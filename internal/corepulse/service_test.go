package corepulse

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type probeClient struct {
	context string
	execPod string
	output  string
	logs    string
	execErr error
}

func (p *probeClient) Run(_ time.Duration, args ...string) (string, error) {
	if args[0] == "config" {
		return p.context, nil
	}
	if args[0] == "exec" {
		p.execPod = args[1]
		return p.output, p.execErr
	}
	if args[0] == "logs" {
		return p.logs, nil
	}
	return "", errors.New("unexpected kubectl command")
}

func (p *probeClient) GetJSON(_ ...string) (map[string]any, error) {
	makePod := func(name string, deleting bool) map[string]any {
		metadata := map[string]any{"name": name, "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": "ueransim-ue-abc"}}}
		if deleting {
			metadata["deletionTimestamp"] = "2026-10-31T00:00:00Z"
		}
		return map[string]any{"metadata": metadata, "status": map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}
	}
	return map[string]any{"items": []any{makePod("ueransim-ue-old", true), makePod("ueransim-ue-current", false)}}, nil
}

func TestProbeServiceUsesCurrentUEAndObservedPackets(t *testing.T) {
	client := &probeClient{context: LabContext, output: "2 packets transmitted, 2 received, 0% packet loss"}
	probe, err := ProbeService(client)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Status != "healthy" || probe.Received != 2 || client.execPod != "pod/ueransim-ue-current" {
		t.Fatalf("unexpected probe: %+v, exec pod: %s", probe, client.execPod)
	}
}

func TestProbeServiceNeverExecutesOutsideLab(t *testing.T) {
	client := &probeClient{context: "production"}
	if _, err := ProbeService(client); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("expected context error, got %v", err)
	}
	if client.execPod != "" {
		t.Fatal("executed probe outside lab")
	}
}

func TestProbeServiceReportsPacketFailure(t *testing.T) {
	client := &probeClient{context: LabContext, output: "2 packets transmitted, 0 received, 100% packet loss", execErr: errors.New("ping lost packets")}
	probe, err := ProbeService(client)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Status != "degraded" || probe.Detail == "" || probe.Sent != 2 || probe.Received != 0 {
		t.Fatalf("unexpected probe: %+v", probe)
	}
}

func TestProbeDoesNotInventPacketLossWithoutCounts(t *testing.T) {
	for _, output := range []string{"", "ping: SO_BINDTODEVICE: Invalid argument", "ping timed out before a packet summary", "0 packets transmitted, 0 received", "2 packets transmitted, 3 received", "9999999999999999999999999 packets transmitted, 0 received", "-1 packets transmitted, 0 received"} {
		t.Run(output, func(t *testing.T) {
			probe, err := ProbeService(&probeClient{context: LabContext, output: output, execErr: errors.New("probe command failed")})
			if err != nil || probe.Status != "unavailable" || probe.Sent != 0 || probe.Received != 0 || probe.Detail == "" {
				t.Fatalf("unobserved/invalid counts became packet loss: %+v %v", probe, err)
			}
		})
	}
}

func TestProbeAcceptsLinuxAndPacketsReceivedSummaries(t *testing.T) {
	for _, output := range []string{"PING example\n--- statistics ---\n2 packets transmitted, 2 received, 0% packet loss\n", "2 packets transmitted, 2 packets received, 0% packet loss"} {
		probe, err := ProbeService(&probeClient{context: LabContext, output: output})
		if err != nil || probe.Status != "healthy" || probe.Sent != 2 || probe.Received != 2 {
			t.Fatalf("valid packet counts lost: %+v %v", probe, err)
		}
	}
}

func TestMissingSummaryIsUnavailableEvenWithoutCommandError(t *testing.T) {
	probe, err := ProbeService(&probeClient{context: LabContext, output: "no packet summary"})
	if err != nil || probe.Status != "unavailable" || probe.Detail == "" {
		t.Fatalf("summary absence became observed loss: %+v %v", probe, err)
	}
}

func TestInspectSignalsDistinguishesLogEventsFromCurrentProbe(t *testing.T) {
	client := &probeClient{context: LabContext, logs: "Initial Registration is successful\nPDU Session establishment is successful", output: "2 packets transmitted, 0 received, 100% packet loss"}
	snapshot, err := InspectSignals(client)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.RegistrationObserved || !snapshot.SessionObserved || snapshot.ServiceProbe.Status != "degraded" {
		t.Fatalf("unexpected signal snapshot: %+v", snapshot)
	}
	if snapshot.Links.N2 != LinkUnknown || snapshot.Links.N4 != LinkUnknown {
		t.Fatalf("unreadable protocol evidence was treated as up: %+v", snapshot.Links)
	}
}
