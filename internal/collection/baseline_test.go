package collection

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
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
	record, err := baseline(context.Background(), labClient{probe: "2 packets transmitted, 2 received"}, store, filepath.Join(dir, "runs"), 2, time.Second, func(context.Context, time.Duration) error { return nil })
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

func TestBaselineCancellationRetainsExcludedEvidence(t *testing.T) {
	dir := t.TempDir()
	runsDir := filepath.Join(dir, "runs")
	rawPath := filepath.Join(dir, "telemetry.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rawBefore []byte
	record, err := baseline(ctx, labClient{probe: "2 packets transmitted, 2 received"}, telemetry.NewStore(rawPath), runsDir, 4, time.Hour, func(ctx context.Context, d time.Duration) error {
		entries, err := os.ReadDir(runsDir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("checkpoint absent during wait: %v %v", entries, err)
		}
		data, _ := os.ReadFile(filepath.Join(runsDir, entries[0].Name()))
		var pending BaselineRecord
		if json.Unmarshal(data, &pending) != nil || pending.Samples != 1 || pending.Quality != "excluded" {
			t.Fatalf("bad pending checkpoint: %s", data)
		}
		rawBefore, _ = os.ReadFile(rawPath)
		cancel()
		return waitContext(ctx, d)
	})
	if !errors.Is(err, context.Canceled) || record.Samples != 1 || record.EndedAt.IsZero() || record.Status != "unverified" || record.Quality != "excluded" || record.Error == "" {
		t.Fatalf("unexpected interrupted interval: %+v err=%v", record, err)
	}
	data, readErr := os.ReadFile(filepath.Join(runsDir, record.RunID+".json"))
	var saved BaselineRecord
	if readErr != nil || json.Unmarshal(data, &saved) != nil || saved.Error != "context canceled" || saved.EndedAt.IsZero() {
		t.Fatalf("interrupted record not saved: %s %v", data, readErr)
	}
	rawAfter, _ := os.ReadFile(rawPath)
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("cancellation started another capture or changed raw data")
	}
	summary, err := dataset.Build(rawPath, runsDir, filepath.Join(dir, "dataset.csv"))
	if err != nil || summary.Samples != 0 || summary.Excluded != 1 || summary.Runs != 0 {
		t.Fatalf("unfinished interval qualified as normal: %+v %v", summary, err)
	}
}
