package dashboard

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"

	"github.com/lazypanic999/CorePulse/internal/evaluation"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

// evaluation displays a saved offline snapshot only. Reading this endpoint
// never fits a model, contacts Kubernetes or substitutes current probe results.
func (s *Server) evaluation(w http.ResponseWriter, _ *http.Request) {
	unavailable := func(message string) {
		writeJSON(w, http.StatusOK, map[string]any{"available": false, "mode": "offline", "message": message})
	}
	info, err := os.Stat(s.EvaluationPath)
	if os.IsNotExist(err) || s.EvaluationPath == "" {
		unavailable("No offline evaluation report yet. Run windows, split, and evaluate after collecting independent verified runs.")
		return
	}
	if err != nil || info.Size() > 8*1024*1024 {
		unavailable("The offline evaluation report could not be read or exceeds the 8 MiB display limit.")
		return
	}
	data, err := runrecord.Read(s.EvaluationPath)
	if err != nil {
		unavailable("The offline evaluation report could not be read.")
		return
	}
	var report evaluation.Report
	if json.Unmarshal(data, &report) != nil || !displayableReport(report) {
		unavailable("The offline evaluation report is incomplete or unsupported. Rebuild it with corepulse evaluate.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "mode": "offline", "report": report})
}

func displayableReport(r evaluation.Report) bool {
	hash, err := hex.DecodeString(r.WindowsSHA256)
	if err != nil || len(hash) != 32 || r.SchemaVersion != 1 || r.TestWindows < 1 || r.TrainWindows < 1 || len(r.Models) == 0 {
		return false
	}
	if r.Split.SchemaVersion != 1 || r.Split.WindowsSHA256 != r.WindowsSHA256 || len(r.Split.TrainRuns) == 0 || len(r.Split.TestRuns) == 0 {
		return false
	}
	heldOut := map[string]bool{}
	for _, id := range r.Split.TestRuns {
		if heldOut[id] {
			return false
		}
		heldOut[id] = true
	}
	for _, id := range r.Split.TrainRuns {
		if heldOut[id] {
			return false
		}
	}
	for _, m := range r.Models {
		metrics := m.WindowMetrics
		if metrics.Total != r.TestWindows || len(m.Predictions) != r.TestWindows || metrics.Predicted < 0 || metrics.Abstained < 0 || metrics.Total != metrics.Predicted+metrics.Abstained || len(metrics.Labels) == 0 || len(metrics.Confusion) != len(metrics.Labels) {
			return false
		}
		for _, row := range metrics.Confusion {
			if len(row) != len(metrics.Labels) {
				return false
			}
		}
		if metrics.Coverage < 0 || metrics.Coverage > 1 || metrics.Accuracy < 0 || metrics.Accuracy > 1 || metrics.MacroF1 < 0 || metrics.MacroF1 > 1 {
			return false
		}
		if m.RunMetrics.Total != len(heldOut) {
			return false
		}
		for _, prediction := range m.Predictions {
			if !heldOut[prediction.RunID] {
				return false
			}
		}
	}
	return true
}
