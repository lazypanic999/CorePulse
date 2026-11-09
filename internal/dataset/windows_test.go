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

func windowRun(t *testing.T, dir, id, label, target string, start time.Time, length time.Duration, verified bool) {
	t.Helper()
	status := "recovered"
	if label == "normal" {
		status = "verified"
	}
	r := map[string]any{"run_id": id, "fault": label, "target": target, "context": corepulse.LabContext, "namespace": corepulse.LabNamespace, "status": status, "quality": "clean", "started_at": start, "ended_at": start.Add(length)}
	if verified {
		r["pre_protocol_check"] = corepulse.ProtocolCheck{CheckedAt: start.Add(-time.Second), Links: corepulse.LinkStatus{N2: corepulse.LinkUp, N4: corepulse.LinkUp}}
	}
	if err := runrecord.Save(filepath.Join(dir, id+".json"), r); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsRetainMissingnessAndRunBoundaries(t *testing.T) {
	dir := t.TempDir()
	raw, runs, out := filepath.Join(dir, "raw.jsonl"), filepath.Join(dir, "runs"), filepath.Join(dir, "windows.csv")
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	windowRun(t, runs, "new", "normal", "", start, 65*time.Second, true)
	windowRun(t, runs, "legacy", "pod_crash", "upf", start.Add(time.Hour), time.Minute, false)
	var lines []byte
	// Deliberately unordered input; zero is observed, not a missing value.
	for _, offset := range []int{62, 35, 5, 30, 15, 3601} {
		pods := []telemetry.PodUsage{{Name: "open5gs-amf-pod", CPUMilli: float64(offset), MemoryBytes: 10}}
		if offset == 15 {
			pods = nil
		}
		if offset == 30 {
			pods[0].CPUMilli = 0
		}
		s := telemetry.Sample{Timestamp: start.Add(time.Duration(offset) * time.Second), Source: telemetry.Source, Context: corepulse.LabContext, Namespace: corepulse.LabNamespace, PodCount: len(pods), CPUMilli: float64(offset), MemoryBytes: 100, Pods: pods}
		b, _ := json.Marshal(s)
		lines = append(lines, append(b, '\n')...)
	}
	if err := os.WriteFile(raw, lines, 0644); err != nil {
		t.Fatal(err)
	}
	summary, err := BuildWindows(raw, runs, out, WindowOptions{Duration: 30 * time.Second, RequireProtocol: true})
	if err != nil || summary.Windows != 3 || summary.Samples != 5 || summary.ExcludedSamples != 1 || summary.ExcludedLegacyRuns != 1 || summary.PartialWindows != 1 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
	bytes, _ := os.ReadFile(out)
	rows, err := csv.NewReader(strings.NewReader(string(bytes))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := func(name string) int { return slices.Index(rows[0], name) }
	if rows[1][col("amf_cpu_millicores_mean")] != "5" || rows[1][col("amf_cpu_millicores_missing_ratio")] != "0.5" || rows[1][col("amf_cpu_millicores_delta")] != "" {
		t.Fatalf("missing observation was imputed: %v", rows[1])
	}
	if rows[2][col("amf_cpu_millicores_mean")] != "17.5" || rows[2][col("amf_cpu_millicores_delta")] != "35" {
		t.Fatalf("zero or boundary was lost: %v", rows[2])
	}
	if rows[3][col("partial")] != "true" || rows[3][col("observed_span_seconds")] != "0" {
		t.Fatalf("partial single-sample window hidden: %v", rows[3])
	}
	parsed, err := ReadWindows(out)
	if err != nil || len(parsed.Rows) != 3 {
		t.Fatalf("cannot read own export: %+v %v", parsed, err)
	}
	after, _ := os.ReadFile(raw)
	if string(after) != string(lines) {
		t.Fatal("raw telemetry changed")
	}
	// Invalid input must preserve the previous export.
	if err := os.WriteFile(raw, append(lines, lines...), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildWindows(raw, runs, out, WindowOptions{Duration: 30 * time.Second, RequireProtocol: true}); err == nil {
		t.Fatal("duplicate captures accepted")
	}
	preserved, _ := os.ReadFile(out)
	if string(preserved) != string(bytes) {
		t.Fatal("failed rebuild replaced previous output")
	}
}

func TestWindowsNeverOverwriteTheirInputs(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw.jsonl")
	if err := os.WriteFile(raw, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildWindows(raw, filepath.Join(dir, "runs"), raw, WindowOptions{Duration: time.Second}); err == nil {
		t.Fatal("input/output alias accepted")
	}
	if _, err := BuildWindows(raw, filepath.Join(dir, "runs"), filepath.Join(dir, "runs", "record.json"), WindowOptions{Duration: time.Second}); err == nil {
		t.Fatal("run record output accepted")
	}
}

func TestWindowsNeverMixAdjacentRuns(t *testing.T) {
	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	raw := filepath.Join(dir, "raw.jsonl")
	out := filepath.Join(dir, "windows.csv")
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	windowRun(t, runs, "first", "normal", "", start, 20*time.Second, true)
	windowRun(t, runs, "second", "pod_crash", "amf", start.Add(20*time.Second), 20*time.Second, true)
	var data []byte
	for _, second := range []int{5, 20, 25, 40} {
		s := telemetry.Sample{Timestamp: start.Add(time.Duration(second) * time.Second), Source: telemetry.Source, Context: corepulse.LabContext, Namespace: corepulse.LabNamespace, CPUMilli: float64(second)}
		line, _ := json.Marshal(s)
		data = append(data, append(line, '\n')...)
	}
	if err := os.WriteFile(raw, data, 0644); err != nil {
		t.Fatal(err)
	}
	summary, err := BuildWindows(raw, runs, out, WindowOptions{Duration: 30 * time.Second, RequireProtocol: true})
	if err != nil || summary.Windows != 2 || summary.Runs != 2 || summary.Samples != 3 || summary.ExcludedSamples != 1 {
		t.Fatalf("runs mixed: %+v %v", summary, err)
	}
	result, err := ReadWindows(out)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[0].RunID != "first" || result.Rows[1].RunID != "second" || result.Rows[0].Features["cpu_millicores_mean"] != 5 || result.Rows[1].Features["cpu_millicores_mean"] != 22.5 {
		t.Fatalf("independent intervals merged: %+v", result.Rows)
	}
}

func splitFixture(t *testing.T, dir string) string {
	t.Helper()
	header := windowHeader()
	rows := [][]string{header}
	for _, group := range []struct{ prefix, label, target string }{{"n", "normal", ""}, {"a", "pod_crash", "amf"}} {
		for _, id := range []string{"1", "2", "3"} {
			for _, at := range []string{"00", "30"} {
				row := make([]string, len(header))
				row[0], row[1], row[2] = group.prefix+id, group.label, group.target
				start, _ := time.Parse(time.RFC3339, "2026-11-09T00:00:"+at+"Z")
				row[3], row[4], row[5], row[6] = start.Format(time.RFC3339), start.Add(30*time.Second).Format(time.RFC3339), start.Add(time.Second).Format(time.RFC3339), start.Add(5*time.Second).Format(time.RFC3339)
				row[7], row[8], row[9], row[10] = "2", "4", "false", "true"
				for i, name := range header {
					if strings.HasSuffix(name, "_missing_ratio") {
						row[i] = "1"
					}
				}
				for name, value := range map[string]string{"cpu_millicores_mean": "5", "cpu_millicores_max": "7", "cpu_millicores_delta": "4", "cpu_millicores_missing_ratio": "0"} {
					row[slices.Index(header, name)] = value
				}
				rows = append(rows, row)
			}
		}
	}
	path := filepath.Join(dir, "windows.csv")
	if err := writeCSV(path, rows); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSplitIsDeterministicAndNeverSharesRuns(t *testing.T) {
	dir := t.TempDir()
	input := splitFixture(t, dir)
	output := filepath.Join(dir, "split.json")
	opts := SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true}
	m, err := Split(input, output, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.TrainRuns) != 4 || len(m.TestRuns) != 2 || len(m.Classes) != 2 {
		t.Fatalf("bad stratification: %+v", m)
	}
	for _, id := range m.TestRuns {
		if slices.Contains(m.TrainRuns, id) {
			t.Fatal("run crossed split")
		}
	}
	before, _ := os.ReadFile(output)
	if _, err := Split(input, output, opts); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(output)
	if string(before) != string(after) {
		t.Fatal("same input and seed produced different split")
	}
	for _, name := range m.FeatureNames {
		if name == "target" || name == "run_id" || name == "label" || strings.Contains(name, "recovery") {
			t.Fatalf("label metadata leaked into features: %s", name)
		}
	}
}

func TestSplitRefusesUnverifiedOrInsufficientRuns(t *testing.T) {
	for _, mode := range []string{"legacy", "singleton", "nan"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			input := splitFixture(t, dir)
			data, _ := os.ReadFile(input)
			rows, _ := csv.NewReader(strings.NewReader(string(data))).ReadAll()
			switch mode {
			case "legacy":
				rows[1][10] = "false"
			case "singleton":
				rows = append(rows[:3], rows[7:]...)
			case "nan":
				rows[1][11] = "NaN"
			}
			if err := writeCSV(input, rows); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "split.json")
			if err := os.WriteFile(output, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := Split(input, output, SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true}); err == nil {
				t.Fatal("invalid evaluation input accepted")
			}
			after, _ := os.ReadFile(output)
			if string(after) != "keep" {
				t.Fatal("failure replaced previous manifest")
			}
		})
	}
}

func TestTemporalEmbargoPurgesWholeNearbyTrainingRuns(t *testing.T) {
	dir := t.TempDir()
	input := splitFixture(t, dir)
	out := filepath.Join(dir, "split.json")
	base, err := Split(input, out, SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(input)
	rows, _ := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	starts := map[string]time.Time{}
	for _, id := range base.TestRuns {
		offset := time.Duration(0)
		if strings.HasPrefix(id, "a") {
			offset = 3 * time.Hour
		}
		starts[id] = start.Add(offset)
	}
	nearAssigned := map[string]bool{}
	for _, id := range base.TrainRuns {
		prefix := id[:1]
		offset := time.Hour
		if prefix == "a" {
			offset = 4 * time.Hour
		}
		if !nearAssigned[prefix] {
			offset = 90 * time.Second
			if prefix == "a" {
				offset += 3 * time.Hour
			}
			nearAssigned[prefix] = true
		}
		starts[id] = start.Add(offset)
	}
	indices := map[string]int{}
	for _, row := range rows[1:] {
		at := starts[row[0]].Add(time.Duration(indices[row[0]]) * 30 * time.Second)
		indices[row[0]]++
		row[3], row[4], row[5], row[6] = at.Format(time.RFC3339), at.Add(30*time.Second).Format(time.RFC3339), at.Add(time.Second).Format(time.RFC3339), at.Add(5*time.Second).Format(time.RFC3339)
	}
	if err := writeCSV(input, rows); err != nil {
		t.Fatal(err)
	}
	m, err := Split(input, out, SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true, Embargo: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.PurgedRuns) != 2 || len(m.TrainRuns) != 2 || !slices.Equal(base.TestRuns, m.TestRuns) || m.EmbargoSeconds != 120 {
		t.Fatalf("bad purged assignment: %+v", m)
	}
	parsed, err := ReadWindows(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyTemporalSeparation(parsed, m.TrainRuns, m.TestRuns, 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := VerifyTemporalSeparation(parsed, append(m.TrainRuns, m.PurgedRuns...), m.TestRuns, 2*time.Minute); err == nil {
		t.Fatal("nearby training runs passed embargo verification")
	}
}

func TestEmbargoFailurePreservesManifest(t *testing.T) {
	dir := t.TempDir()
	input := splitFixture(t, dir)
	out := filepath.Join(dir, "split.json")
	if err := os.WriteFile(out, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Split(input, out, SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true, Embargo: time.Minute}); err == nil {
		t.Fatal("purging all training support should fail")
	}
	saved, _ := os.ReadFile(out)
	if string(saved) != "keep" {
		t.Fatal("failed temporal split replaced output")
	}
}
