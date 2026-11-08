package collection

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

type Recovery struct {
	Status string                   `json:"status"`
	Probes []corepulse.ServiceProbe `json:"probes"`
}

// FinishFault keeps the experiment interval open while observing service recovery.
// It never restarts workloads or treats Kubernetes readiness as packet health.
func FinishFault(client corepulse.Client, result map[string]any, samples int, capture func() error) {
	_ = finishFault(client, result, samples, capture, nil)
}

// RunFault shares the same durable lifecycle between the CLI and dashboard.
// The caller must validate the healthy pre-probe and essential workloads first.
func RunFault(client corepulse.Client, target, runsDir string, preProbe corepulse.ServiceProbe, capture func() error) (map[string]any, error) {
	samples := 0
	checkpoint := func(result map[string]any) error {
		result["pre_service_probe"] = preProbe
		result["quality"] = "excluded"
		result["observed_samples"] = samples
		return runrecord.Save(filepath.Join(runsDir, fmt.Sprint(result["run_id"])+".json"), result)
	}
	result, err := corepulse.RunPodCrashWithCheckpoint(client, target, func() {
		if capture != nil && capture() == nil {
			samples++
		}
	}, checkpoint)
	if err != nil {
		return result, err
	}
	path := filepath.Join(runsDir, fmt.Sprint(result["run_id"])+".json")
	err = finishFault(client, result, samples, capture, func() error { return runrecord.Save(path, result) })
	return result, err
}

func finishFault(client corepulse.Client, result map[string]any, samples int, capture func() error, checkpoint func() error) error {
	return finishFaultWithWait(client, result, samples, capture, checkpoint, time.Sleep)
}

func finishFaultWithWait(client corepulse.Client, result map[string]any, samples int, capture func() error, checkpoint func() error, wait func(time.Duration)) error {
	persist := func() error {
		if checkpoint != nil {
			return checkpoint()
		}
		return nil
	}
	result["quality"] = "excluded"
	result["observed_samples"] = samples
	if result["status"] != "recovered" {
		return persist()
	}
	result["deployment_recovered_at"] = result["ended_at"]
	result["ended_at"] = nil
	result["phase"] = "service_observation"
	result["service_recovery"] = Recovery{Status: "service_pending", Probes: []corepulse.ServiceProbe{}}
	if err := persist(); err != nil {
		return err
	}
	recovery, err := watchRecoveryWithCheckpoint(func() (corepulse.ServiceProbe, error) { return corepulse.ProbeService(client) }, wait, func() {
		if capture != nil && capture() == nil {
			samples++
		}
	}, func(r Recovery) error {
		result["service_recovery"] = r
		result["service_probe"] = r.Probes[len(r.Probes)-1]
		result["observed_samples"] = samples
		return persist()
	})
	if err != nil {
		return err
	}
	result["service_recovery"] = recovery
	last := recovery.Probes[len(recovery.Probes)-1]
	result["service_probe"] = last
	result["ended_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	result["observed_samples"] = samples
	result["phase"] = "completed"
	if samples > 0 && (last.Status == "healthy" || last.Status == "degraded") {
		result["quality"] = "clean"
	}
	// Save the finished run before waiting for N2 evidence, so an
	// interruption during the wait cannot demote it to an excluded record.
	if err := persist(); err != nil {
		return err
	}
	if delay := n2SettleDelay(stringOf(result["target"]), result["started_at"], time.Now().UTC()); delay > 0 {
		wait(delay)
	}
	started, startErr := time.Parse(time.RFC3339Nano, stringOf(result["started_at"]))
	if events, eventsErr := protocolEvents(client); eventsErr != "" {
		result["protocol_events_error"] = eventsErr
		result["control_plane_recovery"] = ControlPlaneRecovery{Status: "n2_unobserved"}
	} else {
		result["protocol_events"] = events
		if startErr != nil {
			result["control_plane_recovery"] = ControlPlaneRecovery{Status: "n2_unobserved"}
		} else {
			result["control_plane_recovery"] = controlPlaneRecovery(events, started)
		}
	}
	return persist()
}

func watchRecovery(probe func() (corepulse.ServiceProbe, error), wait func(time.Duration), capture func()) Recovery {
	r, _ := watchRecoveryWithCheckpoint(probe, wait, capture, func(Recovery) error { return nil })
	return r
}

func watchRecoveryWithCheckpoint(probe func() (corepulse.ServiceProbe, error), wait func(time.Duration), capture func(), checkpoint func(Recovery) error) (Recovery, error) {
	r := Recovery{Status: "service_unverified", Probes: []corepulse.ServiceProbe{}}
	streak := 0
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			wait(5 * time.Second)
		}
		p, err := probe()
		if err != nil {
			p.Status, p.Detail = "unavailable", err.Error()
		}
		r.Probes = append(r.Probes, p)
		// Keep the probe even if the following metrics command is interrupted.
		if err := checkpoint(r); err != nil {
			return r, err
		}
		capture()
		if err := checkpoint(r); err != nil {
			return r, err
		}
		if p.Status == "healthy" {
			streak++
		} else {
			streak = 0
		}
		if streak == 2 {
			r.Status = "service_recovered"
			return r, nil
		}
	}
	if r.Probes[len(r.Probes)-1].Status == "degraded" {
		r.Status = "service_degraded"
	}
	return r, nil
}
