package dataset

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

func TestExportsPreserveMissingProbeCountsAndObservedZeroReplies(t *testing.T) {
	dir := t.TempDir()
	runs, raw, samples, windows := filepath.Join(dir, "runs"), filepath.Join(dir, "raw.jsonl"), filepath.Join(dir, "samples.csv"), filepath.Join(dir, "windows.csv")
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	windowRun(t, runs, "legacy-probe", "pod_crash", "upf", start, time.Minute, true)
	runPath := filepath.Join(runs, "legacy-probe.json")
	data, err := runrecord.Read(runPath)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["service_recovery"] = map[string]any{"probes": []probeJSON{
		{Status: "degraded", Sent: 0, Received: 0, CheckedAt: start.Format(time.RFC3339Nano)},
		{Status: "degraded", Sent: 2, Received: 0, CheckedAt: start.Add(30 * time.Second).Format(time.RFC3339Nano)},
	}}
	if err := runrecord.Save(runPath, record); err != nil {
		t.Fatal(err)
	}
	var lines []byte
	for _, offset := range []int{5, 35} {
		s := telemetry.Sample{Timestamp: start.Add(time.Duration(offset) * time.Second), Source: telemetry.Source, Context: corepulse.LabContext, Namespace: corepulse.LabNamespace}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, append(b, '\n')...)
	}
	if err := os.WriteFile(raw, lines, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(raw, runs, samples); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(samples)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	column := slices.Index(rows[0], "probe_sent")
	if !slices.Equal(rows[1][column:column+2], []string{"", ""}) || !slices.Equal(rows[2][column:column+2], []string{"2", "0"}) {
		t.Fatalf("unknown and observed-zero counters confused: %v", rows)
	}
	if _, err := BuildWindows(raw, runs, windows, WindowOptions{Duration: 30 * time.Second, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadWindows(windows)
	if err != nil {
		t.Fatal(err)
	}
	first, second := parsed.Rows[0].Features, parsed.Rows[1].Features
	if _, ok := first["probe_received_mean"]; ok || first["probe_received_missing_ratio"] != 1 {
		t.Fatalf("unknown counts became zero features: %v", first)
	}
	if second["probe_sent_mean"] != 2 || second["probe_received_mean"] != 0 || second["probe_received_missing_ratio"] != 0 {
		t.Fatalf("real zero replies disappeared: %v", second)
	}
	after, err := os.ReadFile(raw)
	if err != nil || string(after) != string(lines) {
		t.Fatal("exports changed raw telemetry")
	}
}
