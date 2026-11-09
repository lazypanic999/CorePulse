package dashboard

import (
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
	return evaluation.ValidateSnapshot(r) == nil
}
