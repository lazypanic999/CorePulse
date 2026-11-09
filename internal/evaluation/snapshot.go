package evaluation

import (
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
)

// ValidateSnapshot checks a saved report's internal consistency without fitting
// models or reading source data. It cannot authenticate the claimed measurements,
// fitted parameters, labels, split independence or source CSV hash.
func ValidateSnapshot(r Report) error {
	hash, err := hex.DecodeString(r.WindowsSHA256)
	if err != nil || len(hash) != 32 || r.SchemaVersion != 1 || r.Split.SchemaVersion != 1 || r.Split.WindowsSHA256 != r.WindowsSHA256 {
		return fmt.Errorf("unsupported report schema or inconsistent source hash")
	}
	if r.TrainWindows < 1 || r.TestWindows < 1 || r.NormalTrainingWindows < 2 || r.NormalTrainingWindows > r.TrainWindows || r.PurgedWindows < 0 || !finite(r.ZScoreThreshold) || r.ZScoreThreshold <= 0 {
		return fmt.Errorf("invalid report support or threshold")
	}
	if !slices.Equal(r.Split.FeatureNames, dataset.WindowFeatureNames()) || !finite(r.Split.TestFraction) || r.Split.TestFraction <= 0 || r.Split.TestFraction >= 1 || !finite(r.Split.EmbargoSeconds) || r.Split.EmbargoSeconds < 0 || r.Split.EmbargoSeconds > 86400 {
		return fmt.Errorf("unsupported split features or policy")
	}
	roles := map[string]string{}
	for _, group := range []struct {
		role string
		ids  []string
	}{{"train", r.Split.TrainRuns}, {"test", r.Split.TestRuns}, {"purged", r.Split.PurgedRuns}} {
		for _, id := range group.ids {
			if id == "" || roles[id] != "" {
				return fmt.Errorf("duplicate, overlapping or empty split identity")
			}
			roles[id] = group.role
		}
	}
	if len(r.Split.TrainRuns) == 0 || len(r.Split.TestRuns) == 0 || r.TrainWindows < len(r.Split.TrainRuns) || r.TestWindows < len(r.Split.TestRuns) || r.PurgedWindows < len(r.Split.PurgedRuns) || len(r.Split.PurgedRuns) == 0 && r.PurgedWindows != 0 {
		return fmt.Errorf("inconsistent split support")
	}
	tasks := map[string]string{"nearest_centroid": "scenario_classification", "majority_scenario": "scenario_classification", "normal_zscore": "normal_vs_fault_interval", "probe_rule": "normal_vs_fault_interval"}
	if len(r.Models) != len(tasks) {
		return fmt.Errorf("report must retain all four reference methods")
	}
	// Establish a common scenario identity for every held-out window. Other
	// methods must retain exactly these windows, with compatible binary truth.
	truths, runTruth := map[string]string{}, map[string]string{}
	for _, p := range r.Models["nearest_centroid"].Predictions {
		if p.Truth != "normal" && p.Truth != "pod_crash:amf" && p.Truth != "pod_crash:upf" {
			return fmt.Errorf("unsupported scenario truth")
		}
		if old := runTruth[p.RunID]; old != "" && old != p.Truth {
			return fmt.Errorf("inconsistent truth within a run")
		}
		runTruth[p.RunID] = p.Truth
		truths[predictionKey(p)] = p.Truth
	}
	labels, classRuns := []string{}, map[string]int{}
	for _, truth := range runTruth {
		classRuns[truth]++
	}
	for label := range classRuns {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	if len(labels) < 2 || classRuns["normal"] == 0 || len(classRuns) != len(r.Split.Classes) {
		return fmt.Errorf("missing scenario support")
	}
	trainRuns := 0
	for label, count := range classRuns {
		c, ok := r.Split.Classes[label]
		if !ok || c.TestRuns != count || c.TrainRuns < 1 {
			return fmt.Errorf("scenario split counts do not match predictions")
		}
		trainRuns += c.TrainRuns
	}
	if trainRuns != len(r.Split.TrainRuns) || r.NormalTrainingWindows < r.Split.Classes["normal"].TrainRuns || r.TrainWindows-r.NormalTrainingWindows < trainRuns-r.Split.Classes["normal"].TrainRuns {
		return fmt.Errorf("training support does not match declared classes")
	}
	for name, task := range tasks {
		model, ok := r.Models[name]
		if !ok || model.Task != task || len(model.Predictions) != r.TestWindows {
			return fmt.Errorf("missing or inconsistent method %s", name)
		}
		modelLabels := labels
		if task == "normal_vs_fault_interval" {
			modelLabels = []string{"normal", "fault"}
		}
		seen, runs := map[string]bool{}, map[string]bool{}
		observations := make([]observation, 0, len(model.Predictions))
		for _, p := range model.Predictions {
			key := predictionKey(p)
			truth, exists := truths[key]
			if task == "normal_vs_fault_interval" && truth != "normal" {
				truth = "fault"
			}
			if roles[p.RunID] != "test" || p.WindowStart.IsZero() || seen[key] || !exists || p.Truth != truth || !slices.Contains(modelLabels, p.Truth) || p.Predicted != "" && !slices.Contains(modelLabels, p.Predicted) || p.Score != nil && !finite(*p.Score) {
				return fmt.Errorf("invalid, duplicate or inconsistent prediction in %s", name)
			}
			seen[key], runs[p.RunID] = true, true
			observations = append(observations, observation{runID: p.RunID, truth: p.Truth, prediction: p.Predicted})
		}
		if len(runs) != len(r.Split.TestRuns) || !sameMetrics(model.WindowMetrics, measure(modelLabels, observations)) || !sameMetrics(model.RunMetrics, measure(modelLabels, byRun(modelLabels, observations))) {
			return fmt.Errorf("stored scores or support disagree with predictions in %s", name)
		}
	}
	return nil
}

func predictionKey(p Prediction) string {
	return p.RunID + "/" + p.WindowStart.UTC().Format(time.RFC3339Nano)
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

func sameRate(a, b float64) bool { return finite(a) && a >= 0 && a <= 1 && math.Abs(a-b) <= 1e-12 }

func sameMetrics(a, b Metrics) bool {
	if !slices.Equal(a.Labels, b.Labels) || a.Total != b.Total || a.Predicted != b.Predicted || a.Abstained != b.Abstained || len(a.Confusion) != len(b.Confusion) || len(a.Classes) != len(b.Classes) || !sameRate(a.Coverage, b.Coverage) || !sameRate(a.Accuracy, b.Accuracy) || !sameRate(a.MacroF1, b.MacroF1) {
		return false
	}
	for i := range b.Confusion {
		if !slices.Equal(a.Confusion[i], b.Confusion[i]) {
			return false
		}
	}
	for label, want := range b.Classes {
		got, ok := a.Classes[label]
		if !ok || got.Support != want.Support || got.Predicted != want.Predicted || !sameRate(got.Precision, want.Precision) || !sameRate(got.Recall, want.Recall) || !sameRate(got.F1, want.F1) {
			return false
		}
	}
	return true
}
