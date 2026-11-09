package dataset

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

func TestSampleExportUsesHalfOpenRunIntervals(t *testing.T) {
	dir := t.TempDir()
	runs, raw, output := filepath.Join(dir, "runs"), filepath.Join(dir, "raw.jsonl"), filepath.Join(dir, "dataset.csv")
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	windowRun(t, runs, "first", "normal", "", start, 20*time.Second, true)
	windowRun(t, runs, "second", "pod_crash", "amf", start.Add(20*time.Second), 20*time.Second, true)
	var lines []byte
	for _, offset := range []int{0, 20, 40} {
		sample := telemetry.Sample{Timestamp: start.Add(time.Duration(offset) * time.Second), Source: telemetry.Source, Context: corepulse.LabContext, Namespace: corepulse.LabNamespace}
		line, err := json.Marshal(sample)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, append(line, '\n')...)
	}
	if err := os.WriteFile(raw, lines, 0600); err != nil {
		t.Fatal(err)
	}
	summary, err := Build(raw, runs, output)
	if err != nil || summary.Samples != 2 || summary.Normal != 1 || summary.Fault != 1 || summary.Excluded != 1 {
		t.Fatalf("adjacent boundary attribution: %+v %v", summary, err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || len(rows) != 3 || rows[1][1] != "first" || rows[2][1] != "second" {
		t.Fatalf("boundary sample assigned to wrong interval: %v %v", rows, err)
	}
}

func TestHalfOpenAttributionStillRejectsActualOverlap(t *testing.T) {
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	runs := []Run{
		{ID: "first", Started: start, Ended: start.Add(30 * time.Second)},
		{ID: "second", Started: start.Add(20 * time.Second), Ended: start.Add(40 * time.Second)},
	}
	if _, _, err := label(telemetry.Sample{Timestamp: start.Add(25 * time.Second)}, runs); err == nil {
		t.Fatal("genuinely overlapping experiment intervals were accepted")
	}
}
