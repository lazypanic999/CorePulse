package dataset

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

func TestBuildLabelsByRunInterval(t *testing.T) {
	dir := t.TempDir()
	telemetryPath, runsDir, output := filepath.Join(dir, "telemetry.jsonl"), filepath.Join(dir, "runs"), filepath.Join(dir, "dataset.csv")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	rows := []telemetry.Sample{
		{SchemaVersion: 1, Timestamp: start, Source: telemetry.Source, Context: "open5gs-lab", Namespace: "open5gs", PodCount: 1, CPUMilli: 10, MemoryBytes: 100, Pods: []telemetry.PodUsage{{Name: "open5gs-amf-abc", CPUMilli: 10, MemoryBytes: 100}}},
		{SchemaVersion: 1, Timestamp: start.Add(20 * time.Second), Source: telemetry.Source, Context: "open5gs-lab", Namespace: "open5gs", PodCount: 1, CPUMilli: 20, MemoryBytes: 200, Pods: []telemetry.PodUsage{{Name: "open5gs-amf-def", CPUMilli: 20, MemoryBytes: 200}}},
		{SchemaVersion: 1, Timestamp: start.Add(40 * time.Second), Source: telemetry.Source, Context: "open5gs-lab", Namespace: "open5gs", PodCount: 1},
	}
	var lines []byte
	for _, row := range rows {
		data, _ := json.Marshal(row)
		lines = append(lines, append(data, '\n')...)
	}
	if err := os.WriteFile(telemetryPath, lines, 0644); err != nil {
		t.Fatal(err)
	}
	run := map[string]any{"run_id": "run-1", "fault": "pod_crash", "target": "amf", "context": "open5gs-lab", "namespace": "open5gs", "status": "recovered", "quality": "clean", "started_at": start.Add(10 * time.Second).Format(time.RFC3339Nano), "ended_at": start.Add(30 * time.Second).Format(time.RFC3339Nano)}
	runData, _ := json.Marshal(run)
	if err := os.WriteFile(filepath.Join(runsDir, "run-1.json"), runData, 0644); err != nil {
		t.Fatal(err)
	}
	baseline := map[string]any{"run_id": "baseline-1", "fault": "normal", "context": "open5gs-lab", "namespace": "open5gs", "status": "verified", "quality": "clean", "started_at": start.Add(-time.Second).Format(time.RFC3339Nano), "ended_at": start.Add(time.Second).Format(time.RFC3339Nano)}
	baselineData, _ := json.Marshal(baseline)
	if err := os.WriteFile(filepath.Join(runsDir, "baseline-1.json"), baselineData, 0644); err != nil {
		t.Fatal(err)
	}
	summary, err := Build(telemetryPath, runsDir, output)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Samples != 2 || summary.Normal != 1 || summary.Fault != 1 || summary.Excluded != 1 || summary.Runs != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	csvRows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(csvRows) != 3 || csvRows[1][1] != "baseline-1" || csvRows[1][2] != "normal" || csvRows[2][1] != "run-1" || csvRows[2][2] != "pod_crash" || csvRows[2][10] != "20.000" {
		t.Fatalf("unexpected dataset rows: %v", csvRows)
	}
}

func TestBuildExcludesUnverifiedRun(t *testing.T) {
	dir := t.TempDir()
	telemetryPath, runsDir, output := filepath.Join(dir, "telemetry.jsonl"), filepath.Join(dir, "runs"), filepath.Join(dir, "dataset.csv")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sample := telemetry.Sample{SchemaVersion: 1, Timestamp: stamp, Source: telemetry.Source, Context: "open5gs-lab", Namespace: "open5gs", PodCount: 1}
	data, _ := json.Marshal(sample)
	if err := os.WriteFile(telemetryPath, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	run := map[string]any{"run_id": "bad", "fault": "normal", "status": "unverified", "quality": "excluded", "context": "open5gs-lab", "namespace": "open5gs", "started_at": stamp.Add(-time.Second).Format(time.RFC3339Nano), "ended_at": stamp.Add(time.Second).Format(time.RFC3339Nano)}
	data, _ = json.Marshal(run)
	if err := os.WriteFile(filepath.Join(runsDir, "bad.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	summary, err := Build(telemetryPath, runsDir, output)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Samples != 0 || summary.Excluded != 1 || summary.Runs != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
