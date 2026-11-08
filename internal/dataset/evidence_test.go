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

	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

func TestBuildAddsProbeAndEventEvidence(t *testing.T) {
	dir := t.TempDir()
	telemetryPath, runsDir, output := filepath.Join(dir, "telemetry.jsonl"), filepath.Join(dir, "runs"), filepath.Join(dir, "dataset.csv")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 8, 0, 4, 37, 0, time.UTC)
	at := func(seconds int) string {
		return start.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339Nano)
	}
	var lines []byte
	for _, offset := range []int{5, 25, 45} {
		sample := telemetry.Sample{SchemaVersion: 1, Timestamp: start.Add(time.Duration(offset) * time.Second), Source: telemetry.Source, Context: "open5gs-lab", Namespace: "open5gs", PodCount: 1}
		data, _ := json.Marshal(sample)
		lines = append(lines, append(data, '\n')...)
	}
	if err := os.WriteFile(telemetryPath, lines, 0644); err != nil {
		t.Fatal(err)
	}
	probe := func(seconds int, status string, received int) map[string]any {
		return map[string]any{"status": status, "sent": 2, "received": received, "checked_at": at(seconds)}
	}
	event := func(seconds int, kind string) map[string]any {
		return map[string]any{"timestamp": at(seconds), "kind": kind}
	}
	run := map[string]any{
		"run_id": "upf-1", "fault": "pod_crash", "target": "upf", "context": "open5gs-lab", "namespace": "open5gs",
		"status": "recovered", "quality": "clean", "started_at": at(0), "ended_at": at(60),
		"pre_service_probe": probe(-2, "healthy", 2),
		"service_recovery":  map[string]any{"status": "service_degraded", "probes": []any{probe(20, "degraded", 0), probe(40, "degraded", 0)}},
		"protocol_events": []any{
			// SMF log started before the run and was read after it: counted.
			map[string]any{"deployment": "open5gs-smf", "collected_at": at(70), "window": map[string]any{"first": at(-300), "last": at(55)},
				"events": []any{event(-300, "pfcp_associated"), event(19, "pfcp_heartbeat_timeout"), event(19, "pfcp_no_upf_available"), event(19, "pfcp_deassociated"), event(30, "pfcp_association_retry_failed"), event(41, "pfcp_association_retry_failed")}},
			// gNB log starts after the run began: earlier events unseen, so empty.
			map[string]any{"deployment": "ueransim-gnb", "collected_at": at(70), "window": map[string]any{"first": at(10), "last": at(12)}, "events": []any{}},
			// UE log could not be read: empty.
			map[string]any{"deployment": "ueransim-ue", "collected_at": at(70), "error": "target pod is not ready", "events": []any{}},
		},
	}
	data, _ := json.Marshal(run)
	if err := os.WriteFile(filepath.Join(runsDir, "upf-1.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(telemetryPath, runsDir, output); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rows[0][14:], evidenceColumns) {
		t.Fatalf("evidence columns missing or reordered: %v", rows[0])
	}
	want := [][]string{
		{"healthy", "2", "2", "0", "", "", ""},  // only the pre-fault probe so far
		{"degraded", "2", "0", "3", "", "", ""}, // heartbeat, no UPF, de-associated
		{"degraded", "2", "0", "5", "", "", ""}, // plus the retries at 30 s and 41 s
	}
	for i, w := range want {
		if got := rows[i+1][14:]; !slices.Equal(got, w) {
			t.Errorf("row %d evidence = %q, want %q", i+1, got, w)
		}
	}
}

func TestBuildLeavesEvidenceEmptyForOlderRecords(t *testing.T) {
	dir := t.TempDir()
	telemetryPath, runsDir, output := filepath.Join(dir, "telemetry.jsonl"), filepath.Join(dir, "runs"), filepath.Join(dir, "dataset.csv")
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 2, 19, 52, 0, 0, time.UTC)
	sample := telemetry.Sample{SchemaVersion: 1, Timestamp: start.Add(5 * time.Second), Source: telemetry.Source, Context: "open5gs-lab", Namespace: "open5gs", PodCount: 1}
	line, _ := json.Marshal(sample)
	if err := os.WriteFile(telemetryPath, append(line, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	baseline := map[string]any{"run_id": "b-1", "fault": "normal", "context": "open5gs-lab", "namespace": "open5gs", "status": "verified", "quality": "clean",
		"started_at": start.Format(time.RFC3339Nano), "ended_at": start.Add(time.Minute).Format(time.RFC3339Nano)}
	data, _ := json.Marshal(baseline)
	if err := os.WriteFile(filepath.Join(runsDir, "b-1.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(telemetryPath, runsDir, output); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(output)
	rows, _ := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if got := rows[1][14:]; !slices.Equal(got, []string{"", "", "", "", "", "", ""}) {
		t.Fatalf("older record must leave evidence empty, got %q", got)
	}
}
