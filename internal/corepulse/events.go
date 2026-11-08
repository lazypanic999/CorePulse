package corepulse

import (
	"fmt"
	"strings"
	"time"

	"github.com/lazypanic999/CorePulse/internal/logevents"
)

// protocolLogTail bounds how much history one read covers. Events older than
// this window are simply not observed; that is reported, never inferred.
const protocolLogTail = 300

type protocolSource struct {
	deployment string
	component  string
	format     logevents.Format
}

// Workloads whose logs carry the markers in internal/logevents.
var protocolSources = []protocolSource{
	{deployment: "ueransim-ue", component: "ue", format: logevents.FormatUERANSIM},
	{deployment: "ueransim-gnb", component: "gnb", format: logevents.FormatUERANSIM},
	{deployment: "open5gs-smf", component: "smf", format: logevents.FormatOpen5GS},
	{deployment: "open5gs-upf", component: "upf", format: logevents.FormatOpen5GS},
}

// ComponentEvents is what one workload's recent log showed. A component whose
// pod could not be read has Error set and no events; that means "not
// observed", not "no events happened".
type ComponentEvents struct {
	Deployment string `json:"deployment"`
	Pod        string `json:"pod,omitempty"`
	PodIP      string `json:"pod_ip,omitempty"`
	// CollectedAt is when the log was read. Events between Window.Last and
	// CollectedAt did not happen in this pod's log; events before
	// Window.First were not observed.
	CollectedAt      time.Time         `json:"collected_at"`
	LogTail          int               `json:"log_tail"`
	Events           []logevents.Event `json:"events"`
	TimestampedLines int               `json:"timestamped_lines"`
	Window           *logevents.Window `json:"window"`
	SkippedLines     int               `json:"skipped_lines"`
	Error            string            `json:"error,omitempty"`
}

// CollectProtocolEvents reads the current UE, SMF and UPF pod logs with
// runtime timestamps and returns their protocol events. It only reads.
func CollectProtocolEvents(client Client) ([]ComponentEvents, error) {
	contextName, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		return nil, err
	}
	if contextName != LabContext {
		return nil, fmt.Errorf("current context %q differs from lab context %q", contextName, LabContext)
	}
	results := make([]ComponentEvents, 0, len(protocolSources))
	for _, source := range protocolSources {
		results = append(results, collectComponent(client, source))
	}
	return results, nil
}

func collectComponent(client Client, source protocolSource) ComponentEvents {
	result := ComponentEvents{Deployment: source.deployment, LogTail: protocolLogTail, Events: []logevents.Event{}}
	pod, podIP, err := selectedPodInfo(client, source.deployment)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Pod, result.PodIP = pod, podIP
	result.CollectedAt = time.Now().UTC()
	logs, err := client.Run(15*time.Second, "logs", "pod/"+pod, "--namespace", LabNamespace, "--timestamps", fmt.Sprintf("--tail=%d", protocolLogTail))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	parsed, err := logevents.Parse(strings.NewReader(logs),
		logevents.Source{Component: source.component, Pod: pod, Format: source.format},
		logevents.Options{RuntimeTimestamps: true})
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Events = parsed.Events
	result.TimestampedLines = parsed.TimestampedLines
	result.Window = parsed.Window
	result.SkippedLines = len(parsed.Skipped)
	return result
}

// PFCPPeerMismatch means SMF is still retrying a PFCP peer address that no
// longer belongs to the current UPF pod. In the lab this followed every UPF
// pod replacement and lasted until SMF was restarted (issue #22).
type PFCPPeerMismatch struct {
	RetryingPeer string    `json:"retrying_peer"`
	UPFPodIP     string    `json:"upf_pod_ip"`
	LastRetryAt  time.Time `json:"last_retry_at"`
}

// DetectPFCPPeerMismatch compares SMF's latest association retry with the
// current UPF pod address. It returns nil when SMF's log or the UPF pod
// address is unavailable, when SMF re-associated after its last retry, or
// when the retried address is the current one.
func DetectPFCPPeerMismatch(components []ComponentEvents) *PFCPPeerMismatch {
	var smf, upf *ComponentEvents
	for i := range components {
		switch components[i].Deployment {
		case "open5gs-smf":
			smf = &components[i]
		case "open5gs-upf":
			upf = &components[i]
		}
	}
	if smf == nil || upf == nil || smf.Error != "" || upf.PodIP == "" {
		return nil
	}
	var lastRetry *logevents.Event
	for i := range smf.Events {
		event := &smf.Events[i]
		switch event.Kind {
		case logevents.PFCPAssociated:
			lastRetry = nil
		case logevents.PFCPRetryFailed:
			lastRetry = event
		}
	}
	if lastRetry == nil || lastRetry.Peer == "" {
		return nil
	}
	host := lastRetry.Peer
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	if host == upf.PodIP {
		return nil
	}
	return &PFCPPeerMismatch{RetryingPeer: lastRetry.Peer, UPFPodIP: upf.PodIP, LastRetryAt: lastRetry.Timestamp}
}

// Link states derived from the latest relevant log event. "unknown" means
// the log was not read or showed no event for that link, never "fine".
const (
	LinkUp        = "up"
	LinkDown      = "down"
	LinkStalePeer = "stale_peer"
	LinkUnknown   = "unknown"
)

// LinkStatus summarises N2 (gNB to AMF) and N4 (SMF to UPF, PFCP) from the
// latest events in each component's recent log.
type LinkStatus struct {
	N2 string `json:"n2"`
	N4 string `json:"n4"`
}

// Links derives N2 from the gNB log and N4 from the SMF log. A stale PFCP
// peer takes precedence over a plain "down" for N4.
func Links(components []ComponentEvents) LinkStatus {
	status := LinkStatus{N2: LinkUnknown, N4: LinkUnknown}
	for _, c := range components {
		if c.Error != "" {
			continue
		}
		for _, e := range c.Events {
			switch {
			case c.Deployment == "ueransim-gnb" && e.Kind == logevents.NGSetupSuccess:
				status.N2 = LinkUp
			case c.Deployment == "ueransim-gnb" && e.Kind == logevents.N2AssociationTerminated:
				status.N2 = LinkDown
			case c.Deployment == "open5gs-smf" && e.Kind == logevents.PFCPAssociated:
				status.N4 = LinkUp
			case c.Deployment == "open5gs-smf" && (e.Kind == logevents.PFCPDeassociated || e.Kind == logevents.PFCPHeartbeatTimeout || e.Kind == logevents.PFCPRetryFailed || e.Kind == logevents.PFCPNoUPF):
				status.N4 = LinkDown
			}
		}
	}
	if status.N4 == LinkDown && DetectPFCPPeerMismatch(components) != nil {
		status.N4 = LinkStalePeer
	}
	return status
}
