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

func sequenceFixture(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	raw, runs, out := filepath.Join(dir, "invented.jsonl"), filepath.Join(dir, "runs"), filepath.Join(dir, "sequences.csv")
	start := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	windowRun(t, runs, "normal", "normal", "", start, 12*time.Second, true)
	windowRun(t, runs, "amf", "pod_crash", "amf", start.Add(12*time.Second), 5*time.Second, true)
	windowRun(t, runs, "legacy", "normal", "", start.Add(time.Hour), 5*time.Second, false)
	var lines []byte
	for _, offset := range []int{12, 4, 0, 17, 3601} {
		pods := []telemetry.PodUsage{{Name: "open5gs-amf-invented", CPUMilli: 0, MemoryBytes: 0}}
		if offset == 4 {
			pods = nil
		}
		s := telemetry.Sample{Timestamp: start.Add(time.Duration(offset) * time.Second), Context: corepulse.LabContext, Namespace: corepulse.LabNamespace, Source: telemetry.Source, PodCount: len(pods), CPUMilli: float64(offset), Pods: pods}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, append(b, '\n')...)
	}
	if err := os.WriteFile(raw, lines, 0644); err != nil {
		t.Fatal(err)
	}
	return raw, runs, out
}

func sequenceRows(t *testing.T, path string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestSequencesKeepZeroMissingEmptyStepsAndHalfOpenRunBounds(t *testing.T) {
	raw, runs, out := sequenceFixture(t)
	before, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	s, err := BuildSequences(raw, runs, out, SequenceOptions{Step: 5 * time.Second, RequireProtocol: true})
	if err != nil || s.Steps != 4 || s.Runs != 2 || s.EmptySteps != 2 || s.PartialSteps != 1 || s.Samples != 3 || s.ExcludedSamples != 2 || s.ExcludedLegacyRuns != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	rows := sequenceRows(t, out)
	col := func(name string) int { return slices.Index(rows[0], name) }
	if rows[1][col("amf_cpu_millicores_value")] != "0" || rows[1][col("amf_cpu_millicores_mask")] != "1" || rows[1][col("cpu_millicores_value")] != "2" {
		t.Fatalf("zero/within-step means lost: %v", rows[1])
	}
	for _, i := range []int{2, 3} {
		if rows[i][col("sample_count")] != "0" || rows[i][col("cpu_millicores_value")] != "" || rows[i][col("cpu_millicores_mask")] != "0" {
			t.Fatalf("empty interval interpolated: %v", rows[i])
		}
	}
	if rows[3][col("partial")] != "true" || rows[4][0] != "amf" || rows[4][col("sample_count")] != "1" {
		t.Fatal("partial/adjacent-run attribution changed")
	}
	data, err := ReadSequences(out)
	if err != nil || len(data.Rows) != 4 {
		t.Fatalf("own sequence export rejected: %v", err)
	}
	if _, ok := data.Rows[1].Features["cpu_millicores_value"]; ok {
		t.Fatal("missing value became observed zero")
	}
	if _, err := ReadWindows(out); err == nil {
		t.Fatal("sequence accepted as window features")
	}
	after, _ := os.ReadFile(raw)
	if string(before) != string(after) {
		t.Fatal("source changed")
	}
	first, _ := os.ReadFile(out)
	if _, err := BuildSequences(raw, runs, out, SequenceOptions{Step: 5 * time.Second, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(out)
	if string(first) != string(second) {
		t.Fatal("export is nondeterministic")
	}
}

func TestSequenceErrorsPreservePreviousOutputAndInputs(t *testing.T) {
	for _, mode := range []string{"duplicate", "provenance", "negative", "unsupported-scenario", "overlap", "alias", "run-output", "step", "too-many-steps"} {
		t.Run(mode, func(t *testing.T) {
			raw, runs, out := sequenceFixture(t)
			if _, err := BuildSequences(raw, runs, out, SequenceOptions{Step: 5 * time.Second, RequireProtocol: true}); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(out)
			source, _ := os.ReadFile(raw)
			options := SequenceOptions{Step: 5 * time.Second, RequireProtocol: true}
			target := out
			switch mode {
			case "duplicate":
				os.WriteFile(raw, append(source, source...), 0644)
			case "provenance":
				os.WriteFile(raw, []byte(strings.ReplaceAll(string(source), corepulse.LabContext, "production")), 0644)
			case "negative":
				os.WriteFile(raw, []byte(strings.Replace(string(source), `"cpu_millicores":12`, `"cpu_millicores":-1`, 1)), 0644)
			case "unsupported-scenario":
				windowRun(t, runs, "other", "pod_crash", "smf", time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), time.Minute, true)
			case "overlap":
				windowRun(t, runs, "other", "normal", "", time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC), time.Minute, true)
			case "alias":
				target = raw
			case "run-output":
				target = filepath.Join(runs, "output.csv")
			case "step":
				options.Step = time.Millisecond
			case "too-many-steps":
				windowRun(t, runs, "long", "normal", "", time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), 200000*time.Second, true)
				options.Step = time.Second
			}
			if _, err := BuildSequences(raw, runs, target, options); err == nil {
				t.Fatal("unsafe/invalid export accepted")
			}
			after, _ := os.ReadFile(out)
			if string(before) != string(after) {
				t.Fatal("failed export replaced previous output")
			}
			if mode == "alias" {
				after, _ := os.ReadFile(raw)
				if string(source) != string(after) {
					t.Fatal("source overwritten")
				}
			}
		})
	}
}

func TestSequenceReaderRejectsMalformedMasksAndIncompleteGrids(t *testing.T) {
	for _, mode := range []string{"mask", "missing-mask", "nonfinite", "negative", "gap", "duplicate", "cadence", "partial", "session", "protocol", "count"} {
		t.Run(mode, func(t *testing.T) {
			raw, runs, out := sequenceFixture(t)
			if _, err := BuildSequences(raw, runs, out, SequenceOptions{Step: 5 * time.Second, RequireProtocol: true}); err != nil {
				t.Fatal(err)
			}
			rows := sequenceRows(t, out)
			switch mode {
			case "mask":
				rows[1][12] = "2"
			case "missing-mask":
				rows[2][12] = "1"
			case "nonfinite":
				rows[1][11] = "NaN"
			case "negative":
				rows[1][11] = "-1"
			case "gap":
				rows = append(rows[:2], rows[3:]...)
			case "duplicate":
				rows = append(rows, rows[1])
			case "cadence":
				rows[2][10] = "6"
			case "partial":
				rows[1][7] = "true"
			case "session":
				rows[1][9] = "one-session"
			case "protocol":
				rows[1][8] = "false"
			case "count":
				rows[1][6] = "0"
			}
			if err := writeCSV(out, rows); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadSequences(out); err == nil {
				t.Fatal("malformed sequence accepted")
			}
		})
	}
}

func TestSequenceSplitsRetainHashSessionMembershipAndEmbargoGates(t *testing.T) {
	dir := t.TempDir()
	out, split := filepath.Join(dir, "sequences.csv"), filepath.Join(dir, "split.json")
	raw, runs := filepath.Join("..", "..", "examples", "offline", "telemetry.jsonl"), filepath.Join("..", "..", "examples", "offline", "run-records")
	if _, err := BuildSequences(raw, runs, out, SequenceOptions{Step: 5 * time.Second, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{"run", "session"} {
		t.Run(group, func(t *testing.T) {
			m, err := Split(out, split, SplitOptions{GroupBy: group, TestFraction: .25, Seed: 42, Embargo: 2 * time.Minute, RequireProtocol: true})
			if err != nil {
				t.Fatal(err)
			}
			data, err := ReadSplitInput(out)
			if err != nil {
				t.Fatal(err)
			}
			if m.InputKind != SequenceKind || !slices.Equal(m.FeatureNames, SequenceFeatureNames()) {
				t.Fatal("sequence split lost source kind/features")
			}
			if err := ValidateSequenceSplit(data, m, false); err != nil {
				t.Fatal(err)
			}
			bytes, _ := json.Marshal(m)
			for _, mode := range []string{"hash", "overlap", "missing", "extra", "features", "protocol", "classes", "embargo", "session"} {
				t.Run(mode, func(t *testing.T) {
					var changed SplitManifest
					if err := json.Unmarshal(bytes, &changed); err != nil {
						t.Fatal(err)
					}
					switch mode {
					case "hash":
						changed.WindowsSHA256 = strings.Repeat("0", 64)
					case "overlap":
						changed.TrainRuns[0] = changed.TestRuns[0]
					case "missing":
						changed.TrainRuns = changed.TrainRuns[1:]
					case "extra":
						changed.TrainRuns = append(changed.TrainRuns, "invented-absent")
					case "features":
						changed.FeatureNames[0] = "run_id"
					case "protocol":
						changed.RequireProtocol = false
					case "classes":
						delete(changed.Classes, "normal")
					case "embargo":
						changed.EmbargoSeconds = 86400
					case "session":
						if group == "session" {
							changed.SessionAssignments[changed.TrainRuns[0]] = changed.TestSessions[0]
						} else {
							changed.SchemaVersion = 2
						}
					}
					if ValidateSequenceSplit(data, changed, false) == nil {
						t.Fatal("inconsistent sequence split accepted")
					}
				})
			}
			if err := runrecord.Save(split, m); err != nil {
				t.Fatal(err)
			}
		})
	}
}
