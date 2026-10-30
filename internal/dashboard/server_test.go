package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

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
	for _, path := range []string{"/", "/styles.css", "/app.js", "/icon.svg", "/api/plan?target=amf"} {
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
