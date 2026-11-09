package evaluation

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

func TestSnapshotChecksPredictionsWithoutRefitting(t *testing.T) {
	input, split, out, _ := fixture(t)
	report, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(decoded); err != nil {
		t.Fatalf("valid generated report rejected: %v", err)
	}
	cases := map[string]func(*Report){
		"missing comparison": func(r *Report) { delete(r.Models, "probe_rule") },
		"inflated F1": func(r *Report) {
			m := r.Models["majority_scenario"]
			m.WindowMetrics.MacroF1 = 1
			r.Models["majority_scenario"] = m
		},
		"negative confusion": func(r *Report) { r.Models["nearest_centroid"].WindowMetrics.Confusion[0][0] = -1 },
		"wrong support": func(r *Report) {
			m := r.Models["nearest_centroid"]
			c := m.WindowMetrics.Classes["normal"]
			c.Support++
			m.WindowMetrics.Classes["normal"] = c
		},
		"wrong run votes": func(r *Report) {
			m := r.Models["majority_scenario"]
			m.RunMetrics.Accuracy = 1
			r.Models["majority_scenario"] = m
		},
		"duplicate prediction": func(r *Report) {
			r.Models["nearest_centroid"].Predictions[0] = r.Models["nearest_centroid"].Predictions[1]
		},
		"missing window time": func(r *Report) { r.Models["nearest_centroid"].Predictions[0].WindowStart = time.Time{} },
		"different model windows": func(r *Report) {
			p := &r.Models["probe_rule"].Predictions[0]
			p.WindowStart = p.WindowStart.Add(time.Hour)
		},
		"conflicting binary truth": func(r *Report) {
			p := &r.Models["probe_rule"].Predictions[0]
			if p.Truth == "normal" {
				p.Truth = "fault"
			} else {
				p.Truth = "normal"
			}
		},
		"unknown prediction":       func(r *Report) { r.Models["nearest_centroid"].Predictions[0].Predicted = "unknown" },
		"train test overlap":       func(r *Report) { r.Split.TrainRuns[0] = r.Split.TestRuns[0] },
		"purged test overlap":      func(r *Report) { r.Split.PurgedRuns = []string{r.Split.TestRuns[0]}; r.PurgedWindows = 1 },
		"duplicate train identity": func(r *Report) { r.Split.TrainRuns = append(r.Split.TrainRuns, r.Split.TrainRuns[0]) },
		"missing test run":         func(r *Report) { r.Split.TestRuns = append(r.Split.TestRuns, "unrepresented") },
		"wrong class counts":       func(r *Report) { c := r.Split.Classes["normal"]; c.TestRuns++; r.Split.Classes["normal"] = c },
		"changed features":         func(r *Report) { r.Split.FeatureNames[0] = "run_id" },
		"nonfinite metric": func(r *Report) {
			m := r.Models["nearest_centroid"]
			m.WindowMetrics.Coverage = math.NaN()
			r.Models["nearest_centroid"] = m
		},
		"nonfinite score": func(r *Report) { x := math.Inf(1); r.Models["normal_zscore"].Predictions[0].Score = &x },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var r Report
			if err := json.Unmarshal(data, &r); err != nil {
				t.Fatal(err)
			}
			mutate(&r)
			if err := ValidateSnapshot(r); err == nil {
				t.Fatal("inconsistent saved report accepted")
			}
		})
	}
}

func TestSnapshotRetainsValidAbstentions(t *testing.T) {
	input, split, out, manifest := fixture(t)
	rows := readRows(t, input)
	column := slices.Index(rows[0], "probe_received_mean")
	for _, row := range rows[1:] {
		if row[0] == manifest.TestRuns[0] {
			row[column] = ""
		}
	}
	writeRows(t, input, rows)
	data, err := dataset.ReadWindows(input)
	if err != nil {
		t.Fatal(err)
	}
	manifest.WindowsSHA256 = data.SHA256
	if err := runrecord.Save(split, manifest); err != nil {
		t.Fatal(err)
	}
	r, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Models["probe_rule"].WindowMetrics.Abstained != 2 || r.Models["probe_rule"].RunMetrics.Abstained != 1 {
		t.Fatal("fixture did not retain abstentions")
	}
	if err := ValidateSnapshot(r); err != nil {
		t.Fatalf("valid abstentions rejected: %v", err)
	}
}

func TestSyntheticDeclarationDoesNotChangeFitting(t *testing.T) {
	input, split, out, _ := fixture(t)
	ordinary, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	synthetic, err := Evaluate(input, split, out, Options{Threshold: 3, Synthetic: true})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(ordinary.Models)
	b, _ := json.Marshal(synthetic.Models)
	if ordinary.Synthetic || !synthetic.Synthetic || string(a) != string(b) || len(synthetic.Limitations) != len(ordinary.Limitations)+1 || !strings.HasPrefix(synthetic.Limitations[0], "SYNTHETIC EXAMPLE:") {
		t.Fatal("synthetic declaration changed fitting or lost its warning")
	}
	if err := ValidateSnapshot(synthetic); err != nil {
		t.Fatalf("valid labelled synthetic report rejected: %v", err)
	}
}
