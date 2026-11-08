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
	Deployment       string            `json:"deployment"`
	Pod              string            `json:"pod,omitempty"`
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
	pod, err := selectedPod(client, source.deployment)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.Pod = pod
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
