package dashboard

import (
	"fmt"
	"path/filepath"

	"github.com/lazypanic999/CorePulse/internal/collection"
	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

// collectPodCrash runs only while the request owns the shared collection lock.
func (s *Server) collectPodCrash(target string) (map[string]any, error) {
	if err := runrecord.ValidateSessionID(s.SessionID); err != nil {
		return nil, err
	}
	if s.Telemetry != nil {
		_ = s.Telemetry.Capture(s.Client)
	}
	preProbe, err := corepulse.ProbeService(s.Client)
	if err != nil || preProbe.Status != "healthy" {
		return nil, fmt.Errorf("a healthy UE-to-UPF path is required before a fault run")
	}
	if _, err := corepulse.StableLabPods(s.Client); err != nil {
		return nil, fmt.Errorf("ready essential lab workloads are required before a fault run: %w", err)
	}
	var capture func() error
	if s.Telemetry != nil {
		capture = func() error { return s.Telemetry.Capture(s.Client) }
	}
	result, err := collection.RunFaultSession(s.Client, target, s.RunsDir, preProbe, capture, s.SessionID)
	if err != nil {
		return result, err
	}
	if s.Telemetry != nil {
		_ = s.Telemetry.Capture(s.Client)
		datasetPath := filepath.Join(filepath.Dir(s.Telemetry.Path()), "dataset.csv")
		if summary, err := dataset.Build(s.Telemetry.Path(), s.RunsDir, datasetPath); err == nil {
			result["dataset"] = summary
		} else {
			result["dataset_error"] = err.Error()
		}
	}
	result["run_record"] = filepath.Join(s.RunsDir, fmt.Sprint(result["run_id"])+".json")
	return result, nil
}
