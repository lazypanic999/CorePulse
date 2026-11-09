package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type checkpointClient struct {
	deletes      int
	protocolDown bool
}

func (c *checkpointClient) Run(_ time.Duration, args ...string) (string, error) {
	switch args[0] {
	case "config":
		return "open5gs-lab", nil
	case "exec":
		return "2 packets transmitted, 2 received", nil
	case "delete":
		c.deletes++
	case "logs":
		if c.protocolDown {
			return "", nil
		}
		if strings.Contains(args[1], "gnb") {
			return "2026-11-09T01:00:00Z [2026-11-09 01:00:00.000] [ngap] [info] NG Setup procedure is successful\n", nil
		}
		if strings.Contains(args[1], "smf") {
			return "2026-11-09T01:00:00Z 11/09 01:00:00.000: [smf] INFO: PFCP associated [10.244.0.211]:8805\n", nil
		}
	}
	return "", nil
}

func (c *checkpointClient) GetJSON(args ...string) (map[string]any, error) {
	if strings.HasPrefix(args[1], "deployment/") {
		name := strings.TrimPrefix(args[1], "deployment/")
		return map[string]any{"spec": map[string]any{"replicas": 1, "selector": map[string]any{"matchLabels": map[string]any{"app": name}}}, "status": map[string]any{"readyReplicas": 1}}, nil
	}
	selector := args[len(args)-1]
	name := strings.TrimPrefix(selector, "app=")
	if selector == "app=ueransim,component=ue" {
		name = "ueransim-ue"
	}
	return map[string]any{"items": []any{map[string]any{
		"metadata": map[string]any{"name": name + "-abc-pod", "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": name + "-abc"}}},
		"status":   map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	}}}, nil
}

func TestDashboardDoesNotDeleteWhenCheckpointCannotBeSaved(t *testing.T) {
	t.Setenv("COREPULSE_LOCK_DIR", filepath.Join(t.TempDir(), "lock"))
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	c := &checkpointClient{}
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/experiments/pod-crash", strings.NewReader(`{"target":"amf","confirm":"DELETE_ONE_LAB_POD"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	New(c, blocked).ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError || c.deletes != 0 || !strings.Contains(w.Body.String(), "save fault checkpoint") {
		t.Fatalf("status=%d deletes=%d body=%s", w.Code, c.deletes, w.Body.String())
	}
}

func TestDashboardRejectsUnobservedProtocolDespiteHealthyPackets(t *testing.T) {
	t.Setenv("COREPULSE_LOCK_DIR", filepath.Join(t.TempDir(), "lock"))
	c := &checkpointClient{protocolDown: true}
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/experiments/pod-crash", strings.NewReader(`{"target":"amf","confirm":"DELETE_ONE_LAB_POD"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	New(c, t.TempDir()).ServeHTTP(w, r)
	if w.Code != http.StatusConflict || c.deletes != 0 || !strings.Contains(w.Body.String(), "N2=unknown") {
		t.Fatalf("status=%d deletes=%d body=%s", w.Code, c.deletes, w.Body.String())
	}
}

type unavailableClient struct{ actions int }

func (c *unavailableClient) Run(_ time.Duration, args ...string) (string, error) {
	if args[0] == "delete" {
		c.actions++
	}
	return "", errors.New("no Kubernetes context")
}
func (c *unavailableClient) GetJSON(_ ...string) (map[string]any, error) {
	return nil, errors.New("no Kubernetes context")
}

func TestOfflineOverviewIsExplicit(t *testing.T) {
	client := &unavailableClient{}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/overview", nil)
	response := httptest.NewRecorder()
	New(client, t.TempDir()).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status %d", response.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["connected"] != false || payload["mode"] != "preview" || payload["report"] != nil {
		t.Fatalf("unexpected overview: %v", payload)
	}
}

func TestPlanAndStaticDashboard(t *testing.T) {
	handler := New(&unavailableClient{}, t.TempDir())
	for _, path := range []string{"/", "/styles.css", "/app.js", "/icon.svg", "/api/plan?target=amf", "/api/service-check", "/api/protocol"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787"+path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, response.Code)
		}
		if response.Body.Len() == 0 {
			t.Fatalf("%s: empty response", path)
		}
	}
}

func TestPodCrashRequiresExplicitConfirmation(t *testing.T) {
	client := &unavailableClient{}
	handler := New(client, t.TempDir())
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/experiments/pod-crash", strings.NewReader(`{"target":"amf"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || client.actions != 0 {
		t.Fatalf("status %d, actions %d", response.Code, client.actions)
	}
}

func TestPodCrashRejectsCrossOriginRequest(t *testing.T) {
	client := &unavailableClient{}
	handler := New(client, t.TempDir())
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/experiments/pod-crash", strings.NewReader(`{"target":"amf","confirm":"DELETE_ONE_LAB_POD"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://other.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || client.actions != 0 {
		t.Fatalf("status %d, actions %d", response.Code, client.actions)
	}
}

func TestDashboardRejectsNonLoopbackHost(t *testing.T) {
	response := httptest.NewRecorder()
	New(&unavailableClient{}, t.TempDir()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://example.com/api/overview", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status %d", response.Code)
	}
}

type metricsClient struct{}

func (metricsClient) Run(_ time.Duration, args ...string) (string, error) {
	if args[0] == "config" {
		return "open5gs-lab", nil
	}
	return `{"items":[{"metadata":{"name":"open5gs-amf-abc"},"containers":[{"usage":{"cpu":"5m","memory":"20Mi"}}]}]}`, nil
}
func (metricsClient) GetJSON(_ ...string) (map[string]any, error) { return nil, nil }

func TestTelemetryEndpointServesCapturedMetrics(t *testing.T) {
	store := telemetry.NewStore(t.TempDir() + "/telemetry.jsonl")
	if err := store.Capture(metricsClient{}); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	NewWithTelemetry(metricsClient{}, t.TempDir(), store).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/telemetry", nil))
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["available"] != true || payload["source"] != telemetry.Source {
		t.Fatalf("unexpected telemetry: %v", payload)
	}
	latest := payload["latest"].(map[string]any)
	if latest["cpu_millicores"] != float64(5) || latest["memory_bytes"] != float64(20*1024*1024) {
		t.Fatalf("unexpected latest sample: %v", latest)
	}
}
