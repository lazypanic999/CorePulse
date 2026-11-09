package collection

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type sessionClient struct {
	labClient
	calls int
}

func (c *sessionClient) Run(timeout time.Duration, args ...string) (string, error) {
	c.calls++
	return c.labClient.Run(timeout, args...)
}
func (c *sessionClient) GetJSON(args ...string) (map[string]any, error) {
	c.calls++
	return c.labClient.GetJSON(args...)
}

func TestInvalidSessionRefusesBeforeClientOrOutput(t *testing.T) {
	c := &sessionClient{}
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	if _, err := BaselineSessionContext(context.Background(), c, telemetry.NewStore(filepath.Join(dir, "raw.jsonl")), runs, 2, time.Second, "bad session"); err == nil || !strings.Contains(err.Error(), "session-id") {
		t.Fatalf("baseline accepted invalid session: %v", err)
	}
	if _, err := RunFaultSession(c, "amf", runs, corepulse.ServiceProbe{Status: "healthy"}, nil, "bad/session"); err == nil {
		t.Fatal("fault accepted invalid session")
	}
	if c.calls != 0 {
		t.Fatal("invalid session reached cluster client")
	}
	if _, err := os.Stat(runs); !os.IsNotExist(err) {
		t.Fatal("invalid session created output")
	}
}

func TestBaselineSessionIsInInitialAndFinalCheckpoint(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	seenInitial := false
	r, err := baseline(context.Background(), labClient{probe: "2 packets transmitted, 2 received"}, telemetry.NewStore(filepath.Join(dir, "raw.jsonl")), runs, 2, time.Second, func(context.Context, time.Duration) error {
		entries, err := os.ReadDir(runs)
		if err != nil || len(entries) != 1 {
			t.Fatal("initial checkpoint missing")
		}
		data, err := os.ReadFile(filepath.Join(runs, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		var checkpoint BaselineRecord
		if err := json.Unmarshal(data, &checkpoint); err != nil {
			t.Fatal(err)
		}
		if checkpoint.SessionID != "prospective-a" {
			t.Fatal("initial checkpoint lost session")
		}
		seenInitial = true
		return nil
	}, "prospective-a")
	if err != nil || !seenInitial || r.SessionID != "prospective-a" || r.Status != "verified" {
		t.Fatalf("session baseline failed: %+v %v", r, err)
	}
	data, err := os.ReadFile(filepath.Join(runs, r.RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var final BaselineRecord
	if json.Unmarshal(data, &final) != nil || final.SessionID != r.SessionID || final.Status != "verified" {
		t.Fatal("final checkpoint lost declaration")
	}
}

func TestCancelledBaselineRetainsDeclaredSession(t *testing.T) {
	dir := t.TempDir()
	r, err := baseline(context.Background(), labClient{probe: "2 packets transmitted, 2 received"}, telemetry.NewStore(filepath.Join(dir, "raw.jsonl")), filepath.Join(dir, "runs"), 2, time.Second, func(context.Context, time.Duration) error { return context.Canceled }, "declared-cancelled")
	if err == nil || r.SessionID != "declared-cancelled" || r.Quality != "excluded" || r.EndedAt.IsZero() {
		t.Fatalf("cancelled session evidence lost: %+v %v", r, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "runs", r.RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved BaselineRecord
	if json.Unmarshal(data, &saved) != nil || saved.SessionID != r.SessionID || saved.Quality != "excluded" {
		t.Fatal("cancelled checkpoint lost session")
	}
}

type sessionReplacementClient struct {
	labClient
	deleted bool
}

func (c *sessionReplacementClient) Run(timeout time.Duration, args ...string) (string, error) {
	if args[0] == "delete" {
		c.deleted = true
	}
	return c.labClient.Run(timeout, args...)
}
func (c *sessionReplacementClient) GetJSON(args ...string) (map[string]any, error) {
	value, err := c.labClient.GetJSON(args...)
	if err == nil && c.deleted && args[1] == "pods" && strings.Contains(args[len(args)-1], "app=open5gs-upf") {
		items := value["items"].([]any)
		metadata := items[0].(map[string]any)["metadata"].(map[string]any)
		metadata["name"] = metadata["name"].(string) + "-replacement"
	}
	return value, err
}

func TestCompletedFaultKeepsDeclaredSession(t *testing.T) {
	dir := t.TempDir()
	client := &sessionReplacementClient{labClient: labClient{probe: "2 packets transmitted, 2 received"}}
	r, err := RunFaultSession(client, "upf", dir, corepulse.ServiceProbe{Status: "healthy", Sent: 2, Received: 2}, func() error { return nil }, "declared-complete")
	if err != nil || r["session_id"] != "declared-complete" || r["phase"] != "completed" || r["quality"] != "clean" {
		t.Fatalf("completed session fault failed: %v %v", r, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, r["run_id"].(string)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if json.Unmarshal(data, &saved) != nil || saved["session_id"] != "declared-complete" || saved["phase"] != "completed" {
		t.Fatalf("completed checkpoint lost session: %s", data)
	}
}
