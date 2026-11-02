package collection

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type BaselineRecord struct {
	SchemaVersion int                      `json:"schema_version"`
	RunID         string                   `json:"run_id"`
	Fault         string                   `json:"fault"`
	Context       string                   `json:"context"`
	Namespace     string                   `json:"namespace"`
	StartedAt     time.Time                `json:"started_at"`
	EndedAt       time.Time                `json:"ended_at"`
	Status        string                   `json:"status"`
	Quality       string                   `json:"quality"`
	Samples       int                      `json:"samples"`
	ServiceProbes []corepulse.ServiceProbe `json:"service_probes"`
	Error         string                   `json:"error,omitempty"`
}

func runID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

// Baseline records an untouched lab interval. Every sample must have a healthy
// UE-to-UPF probe and the same ready AMF/UPF pods; otherwise it is unverified.
func Baseline(client corepulse.Client, store *telemetry.Store, runsDir string, samples int, interval time.Duration) (BaselineRecord, error) {
	if samples < 2 || interval < time.Second {
		return BaselineRecord{}, fmt.Errorf("baseline needs at least two samples and an interval of at least one second")
	}
	id, err := runID()
	if err != nil {
		return BaselineRecord{}, err
	}
	record := BaselineRecord{SchemaVersion: 1, RunID: id, Fault: "normal", Context: corepulse.LabContext, Namespace: corepulse.LabNamespace, Status: "unverified", Quality: "excluded", ServiceProbes: []corepulse.ServiceProbe{}}
	contextName, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		return record, err
	}
	if contextName != corepulse.LabContext {
		return record, fmt.Errorf("current context %q differs from lab context %q", contextName, corepulse.LabContext)
	}
	pods, err := corepulse.StableLabPods(client)
	if err != nil {
		return record, err
	}
	probe, err := corepulse.ProbeService(client)
	if err != nil {
		return record, err
	}
	if probe.Status != "healthy" {
		return record, fmt.Errorf("baseline requires a healthy UE-to-UPF probe: %s", probe.Detail)
	}
	record.StartedAt = time.Now().UTC()
	for i := 0; i < samples; i++ {
		if i > 0 {
			time.Sleep(interval)
		}
		if err := store.Capture(client); err != nil {
			record.Error = err.Error()
			break
		}
		record.Samples++
		captured, _ := store.Snapshot()
		latest := captured[len(captured)-1]
		for _, expected := range pods {
			found := false
			for _, usage := range latest.Pods {
				if usage.Name == expected {
					found = true
					break
				}
			}
			if !found {
				record.Error = "resource metrics missing an essential lab pod"
				break
			}
		}
		if record.Error != "" {
			break
		}
		probe, err := corepulse.ProbeService(client)
		if err != nil {
			record.Error = err.Error()
			break
		}
		record.ServiceProbes = append(record.ServiceProbes, probe)
		if probe.Status != "healthy" {
			record.Error = "UE-to-UPF probe degraded during baseline"
			break
		}
		currentPods, err := corepulse.StableLabPods(client)
		if err != nil {
			record.Error = err.Error()
			break
		}
		for name, expected := range pods {
			if currentPods[name] != expected {
				record.Error = name + " pod changed during baseline"
				break
			}
		}
		if record.Error != "" {
			break
		}
	}
	record.EndedAt = time.Now().UTC()
	if record.Error == "" && record.Samples == samples {
		record.Status, record.Quality = "verified", "clean"
	}
	if err := save(filepath.Join(runsDir, record.RunID+".json"), record); err != nil {
		return record, err
	}
	if record.Error != "" {
		return record, fmt.Errorf("baseline unverified: %s", record.Error)
	}
	return record, nil
}

func save(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
