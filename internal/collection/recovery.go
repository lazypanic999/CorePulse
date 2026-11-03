package collection

import (
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
)

type Recovery struct {
	Status string                   `json:"status"`
	Probes []corepulse.ServiceProbe `json:"probes"`
}

// FinishFault keeps the experiment interval open while observing service recovery.
// It never restarts workloads or treats Kubernetes readiness as packet health.
func FinishFault(client corepulse.Client, result map[string]any, samples int, capture func() error) {
	result["quality"] = "excluded"
	result["observed_samples"] = samples
	if result["status"] != "recovered" {
		return
	}
	result["deployment_recovered_at"] = result["ended_at"]
	recovery := watchRecovery(func() (corepulse.ServiceProbe, error) { return corepulse.ProbeService(client) }, time.Sleep, func() {
		if capture != nil && capture() == nil {
			samples++
		}
	})
	result["service_recovery"] = recovery
	last := recovery.Probes[len(recovery.Probes)-1]
	result["service_probe"] = last
	result["ended_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	result["observed_samples"] = samples
	if samples > 0 && (last.Status == "healthy" || last.Status == "degraded") {
		result["quality"] = "clean"
	}
}

func watchRecovery(probe func() (corepulse.ServiceProbe, error), wait func(time.Duration), capture func()) Recovery {
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
		capture()
		if p.Status == "healthy" {
			streak++
		} else {
			streak = 0
		}
		if streak == 2 {
			r.Status = "service_recovered"
			return r
		}
	}
	if r.Probes[len(r.Probes)-1].Status == "degraded" {
		r.Status = "service_degraded"
	}
	return r
}
