package collection

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

type interruptedClient struct{ labClient }

func (c interruptedClient) Run(timeout time.Duration, args ...string) (string, error) {
	if args[0] == "wait" {
		// Simulate process termination after a successful fake delete, without
		// running defers. No Kubernetes command is ever executed by this client.
		os.Exit(23)
	}
	return c.labClient.Run(timeout, args...)
}

func TestFaultProcessInterruptionRetainsCheckpoint(t *testing.T) {
	if dir := os.Getenv("COREPULSE_CRASH_TEST_DIR"); dir != "" {
		_, err := RunFault(interruptedClient{}, "amf", dir, corepulse.ServiceProbe{Status: "healthy"}, nil)
		t.Fatalf("expected child termination, got %v", err)
	}
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	cmd := exec.Command(os.Args[0], "-test.run=^TestFaultProcessInterruptionRetainsCheckpoint$")
	cmd.Env = append(os.Environ(), "COREPULSE_CRASH_TEST_DIR="+runsDir)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("child did not reach fake wait: %v %s", err, output)
	}
	entries, err := os.ReadDir(runsDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("checkpoint missing: %v %v", entries, err)
	}
	data, err := runrecord.Read(filepath.Join(runsDir, entries[0].Name()))
	var r map[string]any
	if err != nil || json.Unmarshal(data, &r) != nil || r["phase"] != "delete_requested" || r["status"] != "recovery_pending" || r["quality"] != "excluded" || r["ended_at"] != nil {
		t.Fatalf("incomplete diagnostic snapshot: %s %v", data, err)
	}
	raw := filepath.Join(dir, "raw.jsonl")
	if err := os.WriteFile(raw, nil, 0644); err != nil {
		t.Fatal(err)
	}
	summary, err := dataset.Build(raw, runsDir, filepath.Join(dir, "dataset.csv"))
	if err != nil || summary.Runs != 0 {
		t.Fatalf("interrupted fault included in dataset: %+v %v", summary, err)
	}
}

func TestServiceObservationCheckpointsRemainExcludedUntilComplete(t *testing.T) {
	r := map[string]any{"status": "recovered", "ended_at": "deployment-time"}
	checkpoints := 0
	err := finishFaultWithWait(labClient{probe: "2 packets transmitted, 2 received"}, r, 1, nil, func() error {
		checkpoints++
		if r["phase"] != "completed" && (r["quality"] != "excluded" || r["ended_at"] != nil) {
			t.Fatalf("pending service observation qualified: %v", r)
		}
		return nil
	}, func(time.Duration) {})
	// 5 during observation, 1 for the completed run before the N2 settle
	// wait, and 1 after the protocol events are attached.
	if err != nil || checkpoints != 7 || r["quality"] != "clean" || r["phase"] != "completed" || r["service_recovery"].(Recovery).Status != "service_recovered" {
		t.Fatalf("unexpected completion: %v checkpoints=%d err=%v", r, checkpoints, err)
	}
	// Without a parseable start time the N2 result must not guess.
	if r["control_plane_recovery"].(ControlPlaneRecovery).Status != "n2_unobserved" {
		t.Fatalf("unexpected completion: %v checkpoints=%d err=%v", r, checkpoints, err)
	}
}

func TestServiceCheckpointFailureStopsFurtherProbes(t *testing.T) {
	probes, captures := 0, 0
	r, err := watchRecoveryWithCheckpoint(func() (corepulse.ServiceProbe, error) {
		probes++
		return corepulse.ServiceProbe{Status: "healthy"}, nil
	}, func(time.Duration) { t.Fatal("wait after failed checkpoint") }, func() { captures++ }, func(Recovery) error {
		return errors.New("disk full")
	})
	if err == nil || probes != 1 || captures != 0 || len(r.Probes) != 1 {
		t.Fatalf("lost probe or continued after failure: %+v %v", r, err)
	}
}
