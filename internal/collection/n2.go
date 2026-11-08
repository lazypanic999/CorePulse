package collection

import (
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/logevents"
)

// ControlPlaneRecovery reports what the gNB's log showed about N2 during a
// fault run. It is separate from service_recovery, which only covers the
// UE-to-UPF packet path: existing sessions can keep forwarding while N2 is
// down, so new registrations would fail.
type ControlPlaneRecovery struct {
	// Status is one of:
	//   n2_lost               N2 terminated during the run and was not set up again
	//   n2_restored           N2 terminated and a later NG Setup succeeded
	//   n2_no_change_observed the log covered the whole run and showed no termination
	//   n2_unobserved         the gNB log was not read, or did not cover the run start
	Status        string     `json:"status"`
	TerminatedAt  *time.Time `json:"terminated_at,omitempty"`
	RestoredAt    *time.Time `json:"restored_at,omitempty"`
	GNBPod        string     `json:"gnb_pod,omitempty"`
	ObservedUntil *time.Time `json:"observed_until,omitempty"`
}

// n2SettleTime is how long after an AMF fault starts the gNB log is read. In
// the lab the gNB logged the SCTP termination about 45 s after the AMF pod
// was deleted, so reading earlier could report no change too soon.
const n2SettleTime = 90 * time.Second

// n2SettleDelay returns how much longer to wait before reading the gNB log.
func n2SettleDelay(target string, startedAt any, now time.Time) time.Duration {
	if target != "amf" {
		return 0
	}
	started, err := time.Parse(time.RFC3339Nano, stringOf(startedAt))
	if err != nil {
		return 0
	}
	if remaining := started.Add(n2SettleTime).Sub(now); remaining > 0 {
		return remaining
	}
	return 0
}

// ControlPlaneLost reports whether a fault result recorded N2 as lost.
func ControlPlaneLost(result map[string]any) bool {
	r, ok := result["control_plane_recovery"].(ControlPlaneRecovery)
	return ok && r.Status == "n2_lost"
}

func stringOf(v any) string {
	s, _ := v.(string)
	return s
}

func controlPlaneRecovery(components []corepulse.ComponentEvents, started time.Time) ControlPlaneRecovery {
	r := ControlPlaneRecovery{Status: "n2_unobserved"}
	for _, c := range components {
		if c.Deployment != "ueransim-gnb" {
			continue
		}
		r.GNBPod = c.Pod
		if c.Error != "" || c.Window == nil || c.CollectedAt.IsZero() || c.Window.First.After(started) {
			return r
		}
		until := c.CollectedAt
		r.ObservedUntil = &until
		r.Status = "n2_no_change_observed"
		for _, e := range c.Events {
			if !e.Timestamp.After(started) {
				continue
			}
			ts := e.Timestamp
			switch e.Kind {
			case logevents.N2AssociationTerminated:
				r.Status, r.TerminatedAt, r.RestoredAt = "n2_lost", &ts, nil
			case logevents.NGSetupSuccess:
				if r.TerminatedAt != nil {
					r.Status, r.RestoredAt = "n2_restored", &ts
				}
			}
		}
		return r
	}
	return r
}
