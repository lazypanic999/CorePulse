package corepulse

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/logevents"
)

// eventsClient serves one ready pod per deployment unless the deployment is
// listed in notReady, and returns canned logs per pod.
type eventsClient struct {
	context  string
	notReady map[string]bool
	logs     map[string]string
	logArgs  [][]string
}

func (c *eventsClient) Run(_ time.Duration, args ...string) (string, error) {
	switch args[0] {
	case "config":
		return c.context, nil
	case "logs":
		c.logArgs = append(c.logArgs, args)
		return c.logs[strings.TrimPrefix(args[1], "pod/")], nil
	}
	return "", errors.New("unexpected kubectl command")
}

func (c *eventsClient) GetJSON(args ...string) (map[string]any, error) {
	if strings.HasPrefix(args[1], "pod/") {
		return map[string]any{"status": map[string]any{"podIP": "10.244.0.211"}}, nil
	}
	if strings.HasPrefix(args[1], "deployment/") {
		name := strings.TrimPrefix(args[1], "deployment/")
		ready := float64(1)
		if c.notReady[name] {
			ready = 0
		}
		return map[string]any{
			"spec":   map[string]any{"replicas": float64(1), "selector": map[string]any{"matchLabels": map[string]any{"app": name}}},
			"status": map[string]any{"readyReplicas": ready},
		}, nil
	}
	selector := args[len(args)-1]
	name := strings.TrimPrefix(selector, "app=")
	return map[string]any{"items": []any{map[string]any{
		"metadata": map[string]any{"name": name + "-pod", "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": name + "-rs"}}},
		"status":   map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	}}}, nil
}

func TestCollectProtocolEventsReadsEachComponentWithRuntimeTimestamps(t *testing.T) {
	client := &eventsClient{
		context:  LabContext,
		notReady: map[string]bool{"open5gs-upf": true},
		logs: map[string]string{
			"ueransim-ue-pod": "2026-11-07T23:59:33.362510876Z [2026-11-07 23:59:33.362] [nas] [info] Initial Registration is successful\n" +
				"2026-11-07T23:59:33.599300021Z [2026-11-07 23:59:33.599] [nas] [info] PDU Session establishment is successful PSI[1]\n",
			"ueransim-gnb-pod": "2026-11-08T00:10:54.555786247Z [2026-11-08 00:10:54.555] [ngap] [error] Association terminated for AMF[2]\n",
			"open5gs-smf-pod":  "2026-11-07T23:59:29.454769420Z \x1b[32m11/07 23:59:29.454\x1b[0m: [\x1b[33msmf\x1b[0m] \x1b[1;32mINFO\x1b[0m: PFCP associated [10.244.0.209]:8805 (../src/smf/pfcp-sm.c:188)\n",
		},
	}
	results, err := CollectProtocolEvents(client)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("got %d components", len(results))
	}
	ue, gnb, smf, upf := results[0], results[1], results[2], results[3]
	if ue.Pod != "ueransim-ue-pod" || len(ue.Events) != 2 || ue.Events[0].Kind != logevents.RegistrationSuccess || ue.Events[1].Kind != logevents.PDUSessionSuccess {
		t.Fatalf("unexpected UE events: %+v", ue)
	}
	if ue.Events[0].TimestampSource != "runtime" || ue.Events[0].Timestamp.Format(time.RFC3339Nano) != "2026-11-07T23:59:33.362510876Z" {
		t.Fatalf("runtime timestamp not used: %+v", ue.Events[0])
	}
	if len(smf.Events) != 1 || smf.Events[0].Kind != logevents.PFCPAssociated || smf.Events[0].Component != "smf" {
		t.Fatalf("unexpected SMF events: %+v", smf)
	}
	if len(gnb.Events) != 1 || gnb.Events[0].Kind != logevents.N2AssociationTerminated || gnb.Events[0].Component != "gnb" {
		t.Fatalf("unexpected gNB events: %+v", gnb)
	}
	if upf.Error == "" || len(upf.Events) != 0 || upf.Window != nil {
		t.Fatalf("unready UPF must be reported as not observed: %+v", upf)
	}
	for _, args := range client.logArgs {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--timestamps") || !strings.Contains(joined, "--namespace open5gs") || !strings.Contains(joined, "--tail=300") {
			t.Fatalf("log read missing safeguards: %s", joined)
		}
	}
}

func TestCollectProtocolEventsRefusesOtherContexts(t *testing.T) {
	client := &eventsClient{context: "production"}
	if _, err := CollectProtocolEvents(client); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("expected context error, got %v", err)
	}
	if len(client.logArgs) != 0 {
		t.Fatal("read logs outside the lab context")
	}
}
