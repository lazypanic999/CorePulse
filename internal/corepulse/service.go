package corepulse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const upfGateway = "10.45.0.1"

var pingSummary = regexp.MustCompile(`(?m)^\s*(\d+) packets transmitted, (\d+) (?:packets )?received`)
var registrationSuccess = regexp.MustCompile(`(?i)registration is successful|registration accept`)
var sessionSuccess = regexp.MustCompile(`(?i)pdu session.*successful|pdu session establishment accept`)

// SignalSnapshot separates UE log events from a current packet-path probe.
// Log events prove that a transition occurred in this pod, not that it persists.
type SignalSnapshot struct {
	UEPod                string       `json:"ue_pod"`
	RegistrationObserved bool         `json:"registration_observed"`
	SessionObserved      bool         `json:"session_observed"`
	LogSource            string       `json:"log_source"`
	ServiceProbe         ServiceProbe `json:"service_probe"`
	// ProtocolEvents are timestamped registration, session and PFCP events
	// from the current UE, SMF and UPF pods. Event history is not current
	// health; ServiceProbe is the current packet result.
	ProtocolEvents []ComponentEvents `json:"protocol_events"`
	Links          LinkStatus        `json:"links"`
	// PFCPPeerMismatch is set when SMF keeps retrying an address that is not
	// the current UPF pod; null otherwise.
	PFCPPeerMismatch *PFCPPeerMismatch `json:"pfcp_peer_mismatch"`
}

func InspectSignals(client Client) (SignalSnapshot, error) {
	contextName, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		return SignalSnapshot{}, err
	}
	if contextName != LabContext {
		return SignalSnapshot{}, fmt.Errorf("current context %q differs from lab context %q", contextName, LabContext)
	}
	pod, err := currentUEPod(client)
	if err != nil {
		return SignalSnapshot{}, err
	}
	logs, err := client.Run(15*time.Second, "logs", "pod/"+pod, "--namespace", LabNamespace, "--tail=300")
	if err != nil {
		return SignalSnapshot{}, err
	}
	probe, err := ProbeService(client)
	if err != nil {
		return SignalSnapshot{}, err
	}
	events, err := CollectProtocolEvents(client)
	if err != nil {
		return SignalSnapshot{}, err
	}
	return SignalSnapshot{UEPod: pod, RegistrationObserved: registrationSuccess.MatchString(logs), SessionObserved: sessionSuccess.MatchString(logs), LogSource: "current UE pod, last 300 log lines", ServiceProbe: probe, ProtocolEvents: events, Links: Links(events), PFCPPeerMismatch: DetectPFCPPeerMismatch(events)}, nil
}

// ServiceProbe reports a real UE-to-UPF gateway check, separate from pod readiness.
type ServiceProbe struct {
	Status    string `json:"status"`
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	UEPod     string `json:"ue_pod,omitempty"`
	Gateway   string `json:"gateway"`
	// Counts are unavailable when Status is unavailable; integer zeros in
	// that JSON shape are not observations of zero transmitted packets.
	Sent      int    `json:"sent"`
	Received  int    `json:"received"`
	CheckedAt string `json:"checked_at"`
	Detail    string `json:"detail,omitempty"`
}

func currentUEPod(client Client) (string, error) {
	pods, err := client.GetJSON("get", "pods", "--namespace", LabNamespace, "--selector", "app=ueransim,component=ue")
	if err != nil {
		return "", err
	}
	selected := ""
	for _, raw := range array(pods["items"]) {
		pod := object(raw)
		metadata, status := object(pod["metadata"]), object(pod["status"])
		if metadata["deletionTimestamp"] != nil || status["phase"] != "Running" {
			continue
		}
		ready := false
		for _, rawCondition := range array(status["conditions"]) {
			condition := object(rawCondition)
			if condition["type"] == "Ready" && condition["status"] == "True" {
				ready = true
			}
		}
		if !ready {
			continue
		}
		owned := false
		for _, rawOwner := range array(metadata["ownerReferences"]) {
			owner := object(rawOwner)
			if owner["kind"] == "ReplicaSet" && strings.HasPrefix(stringValue(owner["name"]), "ueransim-ue-") {
				owned = true
			}
		}
		if !owned {
			continue
		}
		name := stringValue(metadata["name"])
		if name == "" || selected != "" {
			return "", fmt.Errorf("expected exactly one current UE pod")
		}
		selected = name
	}
	if selected == "" {
		return "", fmt.Errorf("no ready deployment-owned UE pod found")
	}
	return selected, nil
}

func ProbeService(client Client) (ServiceProbe, error) {
	probe := ServiceProbe{Status: "unavailable", Namespace: LabNamespace, Gateway: upfGateway, CheckedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	context, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		return probe, err
	}
	probe.Context = context
	if context != LabContext {
		return probe, fmt.Errorf("current context %q differs from lab context %q", context, LabContext)
	}
	pod, err := currentUEPod(client)
	if err != nil {
		return probe, err
	}
	probe.UEPod = pod
	output, err := client.Run(15*time.Second, "exec", "pod/"+pod, "--namespace", LabNamespace, "--", "ping", "-I", "uesimtun0", upfGateway, "-c", "2")
	matches := pingSummary.FindStringSubmatch(output)
	if len(matches) != 3 {
		probe.Detail = "ping returned no recognizable packet summary"
		if err != nil {
			probe.Detail = err.Error()
		}
		return probe, nil
	}
	sent, sentErr := strconv.Atoi(matches[1])
	received, receivedErr := strconv.Atoi(matches[2])
	if sentErr != nil || receivedErr != nil || sent <= 0 || received < 0 || received > sent {
		probe.Detail = "ping returned invalid or unobserved packet counts"
		return probe, nil
	}
	probe.Sent, probe.Received = sent, received
	if err != nil {
		probe.Status, probe.Detail = "degraded", err.Error()
		return probe, nil
	}
	if probe.Sent == 2 && probe.Received == 2 {
		probe.Status = "healthy"
	} else {
		probe.Status = "degraded"
		probe.Detail = "UE-to-UPF gateway check did not confirm both requested replies"
	}
	return probe, nil
}
