package collection

import (
	"errors"
	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"testing"
	"time"
)

func TestRecoveryRequiresConsecutiveHealthyChecks(t *testing.T) {
	states := []string{"healthy", "degraded", "healthy", "healthy"}
	i, captures := 0, 0
	r := watchRecovery(func() (corepulse.ServiceProbe, error) {
		p := corepulse.ServiceProbe{Status: states[i]}
		i++
		return p, nil
	}, func(time.Duration) {}, func() { captures++ })
	if r.Status != "service_recovered" || i != 4 || captures != 4 {
		t.Fatalf("unexpected recovery: %+v, captures %d", r, captures)
	}
}

func TestRecoveryExhaustionRetainsEvidence(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		r := watchRecovery(func() (corepulse.ServiceProbe, error) {
			if unavailable {
				return corepulse.ServiceProbe{}, errors.New("lab offline")
			}
			return corepulse.ServiceProbe{Status: "degraded"}, nil
		}, func(time.Duration) {}, func() {})
		want := "service_degraded"
		if unavailable {
			want = "service_unverified"
		}
		if r.Status != want || len(r.Probes) != 6 {
			t.Fatalf("unexpected recovery: %+v", r)
		}
	}
}

func TestFailedDeploymentDoesNotStartServiceObservation(t *testing.T) {
	result := map[string]any{"status": "recovery_unverified", "ended_at": "original"}
	FinishFault(nil, result, 0, func() error { t.Fatal("unexpected sample"); return nil })
	if result["quality"] != "excluded" || result["ended_at"] != "original" {
		t.Fatalf("unexpected result: %v", result)
	}
}
