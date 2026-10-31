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
	client := &probeClient{context: LabContext, execErr: errors.New("ping lost packets")}
	probe, err := ProbeService(client)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Status != "degraded" || probe.Detail == "" {
		t.Fatalf("unexpected probe: %+v", probe)
	}
}
