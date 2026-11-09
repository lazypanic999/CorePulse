package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/evaluation"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

func evaluationFixture() evaluation.Report {
	hash := strings.Repeat("a", 64)
	report := evaluation.Report{SchemaVersion: 1, WindowsSHA256: hash, TrainWindows: 4, TestWindows: 2, NormalTrainingWindows: 2, ZScoreThreshold: 3,
		Split:  dataset.SplitManifest{SchemaVersion: 1, WindowsSHA256: hash, TestFraction: .25, FeatureNames: dataset.WindowFeatureNames(), TrainRuns: []string{"train-n", "train-a"}, TestRuns: []string{"test-n", "test-a"}, Classes: map[string]dataset.ClassSplit{"normal": {TrainRuns: 1, TestRuns: 1}, "pod_crash:amf": {TrainRuns: 1, TestRuns: 1}}},
		Models: map[string]evaluation.ModelReport{}}
	for _, name := range []string{"nearest_centroid", "majority_scenario", "normal_zscore", "probe_rule"} {
		labels, task := []string{"normal", "pod_crash:amf"}, "scenario_classification"
		if name == "normal_zscore" || name == "probe_rule" {
			labels, task = []string{"normal", "fault"}, "normal_vs_fault_interval"
		}
		classes := map[string]evaluation.ClassMetrics{}
		for _, label := range labels {
			classes[label] = evaluation.ClassMetrics{Precision: 1, Recall: 1, F1: 1, Support: 1, Predicted: 1}
		}
		metrics := evaluation.Metrics{Labels: labels, Confusion: [][]int{{1, 0}, {0, 1}}, Classes: classes, Total: 2, Predicted: 2, Coverage: 1, Accuracy: 1, MacroF1: 1}
		report.Models[name] = evaluation.ModelReport{Task: task, WindowMetrics: metrics, RunMetrics: metrics, Predictions: []evaluation.Prediction{
			{RunID: "test-n", WindowStart: time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC), Truth: labels[0], Predicted: labels[0]},
			{RunID: "test-a", WindowStart: time.Date(2026, 11, 9, 0, 1, 0, 0, time.UTC), Truth: labels[1], Predicted: labels[1]},
		}}
	}
	return report
}
func TestOfflineEvaluationEndpointNeedsNoLab(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evaluation.json")
	report := evaluationFixture()
	if err := runrecord.Save(path, report); err != nil {
		t.Fatal(err)
	}
	client := &lockSpyClient{}
	handler := NewWithEvaluation(client, filepath.Join(dir, "runs"), nil, path)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/evaluation", nil))
	var payload struct {
		Available bool              `json:"available"`
		Mode      string            `json:"mode"`
		Report    evaluation.Report `json:"report"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !payload.Available || payload.Mode != "offline" || payload.Report.TestWindows != 2 || client.calls != 0 {
		t.Fatalf("unexpected snapshot: %+v status=%d", payload, response.Code)
	}
}

func TestMissingOrInvalidEvaluationDoesNotInventScores(t *testing.T) {
	for _, mode := range []string{"missing", "invalid_json", "incomplete", "wrong_hash", "training_prediction", "inflated_score", "duplicate_prediction", "omitted_comparison", "purged_overlap"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "evaluation.json")
			switch mode {
			case "invalid_json":
				os.WriteFile(path, []byte("{"), 0644)
			case "incomplete":
				runrecord.Save(path, map[string]int{"schema_version": 1})
			case "wrong_hash":
				r := evaluationFixture()
				r.Split.WindowsSHA256 = "wrong"
				runrecord.Save(path, r)
			case "training_prediction":
				r := evaluationFixture()
				m := r.Models["nearest_centroid"]
				m.Predictions[0].RunID = "train-n"
				r.Models["nearest_centroid"] = m
				runrecord.Save(path, r)
			case "inflated_score", "duplicate_prediction", "omitted_comparison", "purged_overlap":
				r := evaluationFixture()
				switch mode {
				case "inflated_score":
					m := r.Models["nearest_centroid"]
					m.WindowMetrics.Accuracy = .5
					r.Models["nearest_centroid"] = m
				case "duplicate_prediction":
					r.Models["nearest_centroid"].Predictions[0] = r.Models["nearest_centroid"].Predictions[1]
				case "omitted_comparison":
					delete(r.Models, "probe_rule")
				case "purged_overlap":
					r.Split.PurgedRuns = []string{"test-n"}
					r.PurgedWindows = 1
				}
				runrecord.Save(path, r)
			}
			response := httptest.NewRecorder()
			NewWithEvaluation(&unavailableClient{}, dir, nil, path).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/evaluation", nil))
			var payload map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["available"] != false || payload["report"] != nil || payload["mode"] != "offline" {
				t.Fatalf("invented report: %v", payload)
			}
		})
	}
}
