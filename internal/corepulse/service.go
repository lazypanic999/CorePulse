package corepulse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const upfGateway = "10.45.0.1"

var pingSummary = regexp.MustCompile(`(\d+) packets transmitted, (\d+) received`)

// ServiceProbe reports a real UE-to-UPF gateway check, separate from pod readiness.
type ServiceProbe struct {
	Status    string `json:"status"`
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	UEPod     string `json:"ue_pod,omitempty"`
	Gateway   string `json:"gateway"`
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
	if err != nil {
		probe.Status, probe.Detail = "degraded", err.Error()
		return probe, nil
	}
	matches := pingSummary.FindStringSubmatch(output)
	if len(matches) != 3 {
		probe.Status, probe.Detail = "degraded", "ping returned no recognizable packet summary"
		return probe, nil
	}
	probe.Sent, _ = strconv.Atoi(matches[1])
	probe.Received, _ = strconv.Atoi(matches[2])
	if probe.Sent == 2 && probe.Received == 2 {
		probe.Status = "healthy"
	} else {
		probe.Status = "degraded"
		probe.Detail = "UE-to-UPF gateway packets were lost"
	}
	return probe, nil
}
