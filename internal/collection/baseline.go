package collection

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type BaselineRecord struct {
	SchemaVersion    int                       `json:"schema_version"`
	RunID            string                    `json:"run_id"`
	Fault            string                    `json:"fault"`
	Context          string                    `json:"context"`
	Namespace        string                    `json:"namespace"`
	StartedAt        time.Time                 `json:"started_at"`
	EndedAt          time.Time                 `json:"ended_at"`
	Status           string                    `json:"status"`
	Quality          string                    `json:"quality"`
	Samples          int                       `json:"samples"`
	ServiceProbes    []corepulse.ServiceProbe  `json:"service_probes"`
	PreProtocolCheck *corepulse.ProtocolCheck  `json:"pre_protocol_check,omitempty"`
	ProtocolChecks   []corepulse.ProtocolCheck `json:"protocol_checks,omitempty"`
	// ProtocolEvents is read once at the end of the run; see CollectedAt and
	// Window on each component for what it covers.
	ProtocolEvents      []corepulse.ComponentEvents `json:"protocol_events,omitempty"`
	ProtocolEventsError string                      `json:"protocol_events_error,omitempty"`
	Error               string                      `json:"error,omitempty"`
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
// UE-to-UPF probe and the same ready essential lab pods; otherwise it is unverified.
func Baseline(client corepulse.Client, store *telemetry.Store, runsDir string, samples int, interval time.Duration) (BaselineRecord, error) {
	return BaselineContext(context.Background(), client, store, runsDir, samples, interval)
}

// BaselineContext retains an excluded record on cancellation. Clients performing
// blocking I/O should also use ctx (the CLI supplies a context-bound Kubectl).
// The CLI holds shared ownership across its entire baseline batch and export.
func BaselineContext(ctx context.Context, client corepulse.Client, store *telemetry.Store, runsDir string, samples int, interval time.Duration) (record BaselineRecord, runErr error) {
	return baseline(ctx, client, store, runsDir, samples, interval, waitContext)
}

func baseline(ctx context.Context, client corepulse.Client, store *telemetry.Store, runsDir string, samples int, interval time.Duration, wait func(context.Context, time.Duration) error) (record BaselineRecord, runErr error) {
	if err := ctx.Err(); err != nil {
		return record, err
	}
	if samples < 2 || interval < time.Second {
		return BaselineRecord{}, fmt.Errorf("baseline needs at least two samples and an interval of at least one second")
	}
	id, err := runID()
	if err != nil {
		return BaselineRecord{}, err
	}
	record = BaselineRecord{SchemaVersion: 1, RunID: id, Fault: "normal", Context: corepulse.LabContext, Namespace: corepulse.LabNamespace, Status: "unverified", Quality: "excluded", ServiceProbes: []corepulse.ServiceProbe{}}
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
	preProtocol, err := corepulse.CheckProtocolReadiness(client)
	if err != nil {
		return record, fmt.Errorf("baseline preflight: %w", err)
	}
	record.PreProtocolCheck = &preProtocol
	record.StartedAt = time.Now().UTC()
	path := filepath.Join(runsDir, record.RunID+".json")
	if err := runrecord.Save(path, record); err != nil {
		return record, err
	}
	defer func() {
		record.EndedAt = time.Now().UTC()
		if record.Samples > 0 && ctx.Err() == nil {
			record.ProtocolEvents, record.ProtocolEventsError = protocolEvents(client)
		}
		if runErr != nil {
			record.Error = runErr.Error()
			record.Status, record.Quality = "unverified", "excluded"
		}
		runErr = errors.Join(runErr, runrecord.Save(path, record))
	}()
	for i := 0; i < samples; i++ {
		if i > 0 {
			if err := wait(ctx, interval); err != nil {
				return record, err
			}
		}
		if err := ctx.Err(); err != nil {
			return record, err
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
		if err := ctx.Err(); err != nil {
			return record, err
		}
		protocol, err := corepulse.CheckProtocolReadiness(client)
		record.ProtocolChecks = append(record.ProtocolChecks, protocol)
		if err != nil {
			record.Error = err.Error()
			break
		}
		if err := runrecord.Save(path, record); err != nil {
			return record, err
		}
	}
	if err := ctx.Err(); err != nil {
		return record, err
	}
	if record.Error == "" && record.Samples == samples {
		record.Status, record.Quality = "verified", "clean"
	}
	if record.Error != "" {
		return record, fmt.Errorf("baseline unverified: %s", record.Error)
	}
	return record, nil
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
