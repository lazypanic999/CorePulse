package collection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type labClient struct{ probe string }

func (f labClient) Run(_ time.Duration, args ...string) (string, error) {
	switch args[0] {
	case "config":
		return "open5gs-lab", nil
	case "get":
		return `{"items":[{"metadata":{"name":"open5gs-amf-abc-pod"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]},{"metadata":{"name":"open5gs-smf-abc-pod"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]},{"metadata":{"name":"open5gs-udr-abc-pod"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]},{"metadata":{"name":"open5gs-upf-abc-pod"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]},{"metadata":{"name":"ueransim-gnb-abc-pod"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]},{"metadata":{"name":"ueransim-ue-abc-pod"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]}]}`, nil
	case "exec":
		return f.probe, nil
	}
	return "", nil
}

func (f labClient) GetJSON(args ...string) (map[string]any, error) {
	if strings.HasPrefix(args[1], "deployment/") {
		name := strings.TrimPrefix(args[1], "deployment/")
		return map[string]any{"spec": map[string]any{"replicas": float64(1), "selector": map[string]any{"matchLabels": map[string]any{"app": name}}}, "status": map[string]any{"readyReplicas": float64(1)}}, nil
	}
	selector := args[len(args)-1]
	component := strings.TrimPrefix(selector, "app=")
	if strings.Contains(selector, "app=ueransim,component=ue") {
		component = "ueransim-ue"
	}
	name, owner := component+"-abc-pod", component+"-abc"
	pod := map[string]any{"metadata": map[string]any{"name": name, "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": owner}}}, "status": map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}}}
	return map[string]any{"items": []any{pod}}, nil
}

func TestBaselineRecordsVerifiedInterval(t *testing.T) {
	dir := t.TempDir()
	store := telemetry.NewStore(filepath.Join(dir, "telemetry.jsonl"))
	record, err := Baseline(labClient{probe: "2 packets transmitted, 2 received"}, store, filepath.Join(dir, "runs"), 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "verified" || record.Quality != "clean" || record.Samples != 2 || len(record.ServiceProbes) != 2 {
		t.Fatalf("unexpected baseline: %+v", record)
	}
	data, err := os.ReadFile(filepath.Join(dir, "runs", record.RunID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved BaselineRecord
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.RunID != record.RunID || saved.Quality != "clean" {
		t.Fatalf("unexpected saved baseline: %+v", saved)
	}
}
