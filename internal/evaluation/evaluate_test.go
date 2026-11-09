package evaluation

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

func fixture(t *testing.T) (string, string, string, dataset.SplitManifest) {
	t.Helper()
	dir := t.TempDir()
	input, split, out := filepath.Join(dir, "windows.csv"), filepath.Join(dir, "split.json"), filepath.Join(dir, "report.json")
	header := append([]string{"run_id", "label", "target", "window_start", "window_end", "first_sample_at", "last_sample_at", "sample_count", "observed_span_seconds", "partial", "protocol_start_verified"}, dataset.WindowFeatureNames()...)
	rows := [][]string{header}
	for _, group := range []struct {
		prefix, label, target string
		value                 float64
	}{{"n", "normal", "", 0}, {"a", "pod_crash", "amf", 10}} {
		for _, id := range []string{"1", "2"} {
			for _, offset := range []int{0, 30} {
				row := make([]string, len(header))
				row[0], row[1], row[2] = group.prefix+id, group.label, group.target
				start := time.Date(2026, 11, 9, 0, 0, offset, 0, time.UTC)
				row[3], row[4], row[5], row[6] = start.Format(time.RFC3339), start.Add(30*time.Second).Format(time.RFC3339), start.Add(time.Second).Format(time.RFC3339), start.Add(5*time.Second).Format(time.RFC3339)
				row[7], row[8], row[9], row[10] = "2", "4", "false", "true"
				for i, name := range header {
					if strings.HasSuffix(name, "_missing_ratio") {
						row[i] = "1"
					}
				}
				values := map[string]string{"cpu_millicores_mean": strconv.FormatFloat(group.value, 'g', -1, 64), "cpu_millicores_max": strconv.FormatFloat(group.value, 'g', -1, 64), "cpu_millicores_delta": "0", "cpu_millicores_missing_ratio": "0", "probe_sent_mean": "2", "probe_received_mean": "2"}
				if group.label != "normal" {
					values["probe_received_mean"] = "0"
				}
				for name, v := range values {
					row[slices.Index(header, name)] = v
				}
				rows = append(rows, row)
			}
		}
	}
	writeRows(t, input, rows)
	m, err := dataset.Split(input, split, dataset.SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true})
	if err != nil {
		t.Fatal(err)
	}
	return input, split, out, m
}

func writeRows(t *testing.T, path string, rows [][]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
func readRows(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestOfflineEvaluationRetainsHeldOutPredictions(t *testing.T) {
	input, split, out, m := fixture(t)
	r, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.TrainWindows != 4 || r.TestWindows != 4 || len(r.Models) != 4 {
		t.Fatalf("unexpected report: %+v", r)
	}
	for _, name := range []string{"nearest_centroid", "normal_zscore", "probe_rule"} {
		model := r.Models[name]
		if model.WindowMetrics.Accuracy != 1 || model.WindowMetrics.MacroF1 != 1 || model.RunMetrics.Total != 2 {
			t.Fatalf("%s: %+v", name, model.WindowMetrics)
		}
		for _, p := range model.Predictions {
			if slices.Contains(m.TrainRuns, p.RunID) || !slices.Contains(m.TestRuns, p.RunID) {
				t.Fatal("predicted on training data")
			}
		}
	}
	if r.Models["majority_scenario"].WindowMetrics.Accuracy != .5 {
		t.Fatal("majority comparison is missing or wrong")
	}
	before, _ := os.ReadFile(out)
	if _, err := Evaluate(input, split, out, Options{Threshold: 3}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(out)
	if string(before) != string(after) {
		t.Fatal("evaluation is not reproducible")
	}
}

func TestHeldOutValuesCannotChangeTrainingTransforms(t *testing.T) {
	input, split, out, m := fixture(t)
	before, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	rows := readRows(t, input)
	mean := slices.Index(rows[0], "cpu_millicores_mean")
	maximum := slices.Index(rows[0], "cpu_millicores_max")
	for _, row := range rows[1:] {
		if row[1] == "normal" && slices.Contains(m.TestRuns, row[0]) {
			row[mean], row[maximum] = "100", "100"
		}
	}
	writeRows(t, input, rows)
	if _, err := dataset.Split(input, split, dataset.SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	after, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nearest_centroid", "normal_zscore"} {
		if !reflect.DeepEqual(before.Models[name].Transformer, after.Models[name].Transformer) {
			t.Fatalf("test values changed %s training parameters", name)
		}
	}
	if after.Models["nearest_centroid"].WindowMetrics.Accuracy != .5 {
		t.Fatal("wrong held-out predictions were hidden")
	}
}

func TestMissingProbeEvidenceAbstains(t *testing.T) {
	input, split, out, _ := fixture(t)
	rows := readRows(t, input)
	for _, row := range rows[1:] {
		if row[1] == "normal" {
			continue
		}
		for i, name := range rows[0] {
			if strings.HasPrefix(name, "probe_") {
				row[i] = ""
				if strings.HasSuffix(name, "_missing_ratio") {
					row[i] = "1"
				}
			}
		}
	}
	writeRows(t, input, rows)
	if _, err := dataset.Split(input, split, dataset.SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	r, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	metrics := r.Models["probe_rule"].WindowMetrics
	if metrics.Abstained != 2 || metrics.Coverage != .5 || metrics.Accuracy != 1 || metrics.Classes["fault"].Recall != 0 {
		t.Fatalf("missing evidence was treated as healthy: %+v", metrics)
	}
}

func TestEvaluationRejectsChangedOrOverlappingSplits(t *testing.T) {
	for _, mode := range []string{"changed_csv", "overlap", "missing_id", "wrong_classes", "alias"} {
		t.Run(mode, func(t *testing.T) {
			input, split, out, m := fixture(t)
			if err := os.WriteFile(out, []byte("keep"), 0644); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "changed_csv":
				f, err := os.OpenFile(input, os.O_APPEND|os.O_WRONLY, 0644)
				if err != nil {
					t.Fatal(err)
				}
				f.WriteString("\n")
				f.Close()
			case "overlap":
				m.TestRuns = append(m.TestRuns, m.TrainRuns[0])
			case "missing_id":
				m.TrainRuns = m.TrainRuns[1:]
			case "wrong_classes":
				m.Classes["normal"] = dataset.ClassSplit{TrainRuns: 99, TestRuns: 1}
			case "alias":
				out = input
			}
			if mode != "changed_csv" && mode != "alias" {
				if err := runrecord.Save(split, m); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Evaluate(input, split, out, Options{Threshold: 3}); err == nil {
				t.Fatal("invalid evaluation accepted")
			}
			if mode != "alias" {
				saved, _ := os.ReadFile(out)
				if string(saved) != "keep" {
					t.Fatal("failed evaluation replaced report")
				}
			}
		})
	}
}

func TestConfusionMetricsIncludeAbstentions(t *testing.T) {
	m := measure([]string{"normal", "fault"}, []observation{{truth: "normal", prediction: "normal"}, {truth: "normal", prediction: "fault"}, {truth: "fault", prediction: "fault"}, {truth: "fault", prediction: ""}})
	if m.Confusion[0][0] != 1 || m.Confusion[0][1] != 1 || m.Confusion[1][1] != 1 || m.Predicted != 3 || m.Total != 4 || m.Abstained != 1 || m.Classes["fault"].Precision != .5 || m.Classes["fault"].Recall != .5 || m.Classes["fault"].F1 != .5 {
		t.Fatalf("bad confusion metrics: %+v", m)
	}
}
