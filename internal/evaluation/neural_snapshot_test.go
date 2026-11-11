package evaluation

import (
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
)

func neuralSnapshot(t *testing.T, session bool) Report {
	t.Helper()
	input, split, out, _ := fixture(t)
	if session {
		input, split, out = declaredFixture(t, false)
		if _, err := dataset.Split(input, split, dataset.SplitOptions{GroupBy: "session", TestFraction: .25, Seed: 42, RequireProtocol: true}); err != nil {
			t.Fatal(err)
		}
	}
	r, err := Evaluate(input, split, out, Options{Threshold: 3, Synthetic: true})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func addInventedMethod(r *Report) {
	r.SchemaVersion = 3
	m := r.Models["nearest_centroid"] // Reused predictions are a schema fixture, not a trained network.
	seed := int64(7)
	m.Family, m.Seed = "invented", &seed
	r.Models["invented_seed_7"] = m
	r.SeedSpread = map[string]SeedSummary{"invented": {Task: m.Task, Seeds: []int64{7}, Methods: []string{"invented_seed_7"}, WindowMacroF1: rateSpread([]float64{m.WindowMetrics.MacroF1}), RunMacroF1: rateSpread([]float64{m.RunMetrics.MacroF1}), Coverage: rateSpread([]float64{m.WindowMetrics.Coverage})}}
}

func TestNeuralSnapshotRetainsLegacySchemasAndRejectsLegacyExtras(t *testing.T) {
	for _, session := range []bool{false, true} {
		r := neuralSnapshot(t, session)
		if err := ValidateSnapshot(r); err != nil {
			t.Fatal(err)
		}
		r.Models["invented"] = r.Models["nearest_centroid"]
		if ValidateSnapshot(r) == nil {
			t.Fatal("legacy report accepted an extra method")
		}
		delete(r.Models, "invented")
		addInventedMethod(&r)
		if err := ValidateSnapshot(r); err != nil {
			t.Fatalf("schema3 with split schema%d: %v", r.Split.SchemaVersion, err)
		}
	}
}

func TestNeuralSnapshotValidatesEveryExtraPredictionAndSeedSummary(t *testing.T) {
	r := neuralSnapshot(t, true)
	addInventedMethod(&r)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Report){
		"missing reference": func(r *Report) { delete(r.Models, "probe_rule") },
		"unknown task": func(r *Report) {
			m := r.Models["invented_seed_7"]
			m.Task = "anything"
			r.Models["invented_seed_7"] = m
		},
		"invalid name": func(r *Report) { r.Models["<script>"] = r.Models["invented_seed_7"] },
		"changed time": func(r *Report) {
			r.Models["invented_seed_7"].Predictions[0].WindowStart = r.Models["invented_seed_7"].Predictions[0].WindowStart.Add(time.Second)
		},
		"missing window": func(r *Report) {
			m := r.Models["invented_seed_7"]
			m.Predictions = m.Predictions[1:]
			r.Models["invented_seed_7"] = m
		},
		"changed truth":   func(r *Report) { r.Models["invented_seed_7"].Predictions[0].Truth = "unknown" },
		"nonfinite score": func(r *Report) { n := math.NaN(); r.Models["invented_seed_7"].Predictions[0].Score = &n },
		"inflated metric": func(r *Report) {
			m := r.Models["invented_seed_7"]
			m.WindowMetrics.Coverage = .01
			r.Models["invented_seed_7"] = m
		},
		"missing summary":   func(r *Report) { r.SeedSpread = nil },
		"changed summary":   func(r *Report) { s := r.SeedSpread["invented"]; s.Coverage.Mean = .123; r.SeedSpread["invented"] = s },
		"changed seed":      func(r *Report) { *r.Models["invented_seed_7"].Seed = 23 },
		"duplicate seed":    func(r *Report) { r.Models["invented_seed_8"] = r.Models["invented_seed_7"] },
		"reference family":  func(r *Report) { m := r.Models["invented_seed_7"]; r.Models["nearest_centroid"] = m },
		"unsupported split": func(r *Report) { r.Split.SchemaVersion = 3 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var copy Report
			if err := json.Unmarshal(raw, &copy); err != nil {
				t.Fatal(err)
			}
			mutate(&copy)
			if ValidateSnapshot(copy) == nil {
				t.Fatal("inconsistent neural report accepted")
			}
		})
	}
}

// A Python-generated synthetic report may be supplied for an offline integration
// check. Ordinary Go tests remain self-contained and never invoke Python or a lab.
func TestPythonSyntheticSnapshot(t *testing.T) {
	path := os.Getenv("COREPULSE_SYNTHETIC_REPORT")
	if path == "" {
		t.Skip("optional Python report not supplied")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != 3 || !r.Synthetic {
		t.Fatal("expected explicitly synthetic schema3 report")
	}
	if err := ValidateSnapshot(r); err != nil {
		t.Fatal(err)
	}
}
