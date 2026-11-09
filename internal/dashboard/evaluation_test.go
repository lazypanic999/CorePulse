package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/evaluation"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

func evaluationFixture() evaluation.Report {
	hash := strings.Repeat("a", 64)
	metrics := evaluation.Metrics{Labels: []string{"normal", "pod_crash:amf"}, Confusion: [][]int{{1, 0}, {0, 1}}, Total: 2, Predicted: 2, Coverage: 1, Accuracy: 1, MacroF1: 1}
	return evaluation.Report{SchemaVersion: 1, WindowsSHA256: hash, TrainWindows: 4, TestWindows: 2, Split: dataset.SplitManifest{SchemaVersion: 1, WindowsSHA256: hash, TrainRuns: []string{"train-n", "train-a"}, TestRuns: []string{"test-n", "test-a"}}, Models: map[string]evaluation.ModelReport{"nearest_centroid": {Task: "scenario_classification", WindowMetrics: metrics, RunMetrics: metrics, Predictions: []evaluation.Prediction{{RunID: "test-n", Truth: "normal", Predicted: "normal"}, {RunID: "test-a", Truth: "pod_crash:amf", Predicted: "pod_crash:amf"}}}}}
}

func TestOfflineEvaluationEndpointNeedsNoLab(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evaluation.json")
	report := evaluationFixture()
	if err := runrecord.Save(path, report); err != nil {
		t.Fatal(err)
	}
	client := &unavailableClient{}
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
	if response.Code != http.StatusOK || !payload.Available || payload.Mode != "offline" || payload.Report.TestWindows != 2 || client.actions != 0 {
		t.Fatalf("unexpected snapshot: %+v status=%d", payload, response.Code)
	}
}

func TestMissingOrInvalidEvaluationDoesNotInventScores(t *testing.T) {
	for _, mode := range []string{"missing", "invalid_json", "incomplete", "wrong_hash", "training_prediction"} {
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
