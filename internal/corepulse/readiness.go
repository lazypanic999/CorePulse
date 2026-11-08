package corepulse

import (
	"fmt"
	"time"

	"github.com/lazypanic999/CorePulse/internal/logevents"
)

// ProtocolCheck records a read-only precondition, not an active protocol probe.
// Sparse setup events can be old; their latest observed transition in the current
// pod's bounded log is the evidence. Unknown never qualifies as healthy.
type ProtocolCheck struct {
	CheckedAt        time.Time         `json:"checked_at"`
	Links            LinkStatus        `json:"links"`
	Evidence         []LinkEvidence    `json:"evidence"`
	PFCPPeerMismatch *PFCPPeerMismatch `json:"pfcp_peer_mismatch,omitempty"`
}

type LinkEvidence struct {
	Deployment    string         `json:"deployment"`
	Pod           string         `json:"pod,omitempty"`
	LastEventAt   *time.Time     `json:"last_event_at,omitempty"`
	LastEventKind logevents.Kind `json:"last_event_kind,omitempty"`
	Error         string         `json:"error,omitempty"`
}

// CheckProtocolReadiness refuses mixed-fault starting conditions even when an
// existing UE session still forwards packets. It re-reads logs; it uses no UI cache.
func CheckProtocolReadiness(client Client) (ProtocolCheck, error) {
	check := ProtocolCheck{CheckedAt: time.Now().UTC(), Links: LinkStatus{N2: LinkUnknown, N4: LinkUnknown}, Evidence: []LinkEvidence{}}
	contextName, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		return check, err
	}
	if contextName != LabContext {
		return check, fmt.Errorf("current context %q differs from lab context %q", contextName, LabContext)
	}
	components := make([]ComponentEvents, 0, 3)
	for _, source := range protocolSources {
		if source.component == "ue" {
			continue
		}
		c := collectComponent(client, source)
		components = append(components, c)
		evidence := LinkEvidence{Deployment: c.Deployment, Pod: c.Pod, Error: c.Error}
		for _, event := range c.Events {
			if relevantLinkEvent(c.Deployment, event.Kind) {
				at := event.Timestamp
				evidence.LastEventAt, evidence.LastEventKind = &at, event.Kind
			}
		}
		check.Evidence = append(check.Evidence, evidence)
	}
	check.Links = Links(components)
	check.PFCPPeerMismatch = DetectPFCPPeerMismatch(components)
	if check.Links.N2 != LinkUp || check.Links.N4 != LinkUp {
		return check, fmt.Errorf("protocol readiness requires N2=up and N4=up; observed N2=%s, N4=%s; inspect corepulse signals and reset the isolated lab with scripts/recover-service.sh", check.Links.N2, check.Links.N4)
	}
	return check, nil
}

func relevantLinkEvent(deployment string, kind logevents.Kind) bool {
	if deployment == "ueransim-gnb" {
		return kind == logevents.NGSetupSuccess || kind == logevents.N2AssociationTerminated
	}
	if deployment == "open5gs-smf" {
		switch kind {
		case logevents.PFCPAssociated, logevents.PFCPDeassociated, logevents.PFCPHeartbeatTimeout, logevents.PFCPRetryFailed, logevents.PFCPNoUPF:
			return true
		}
	}
	return false
}
