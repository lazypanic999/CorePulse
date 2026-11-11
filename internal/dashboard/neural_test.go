package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type neuralNoClient struct{ calls int }

func (c *neuralNoClient) Run(time.Duration, ...string) (string, error) {
	c.calls++
	return "", fmt.Errorf("fixture must not call the lab")
}
func (c *neuralNoClient) GetJSON(...string) (map[string]any, error) {
	c.calls++
	return nil, fmt.Errorf("fixture must not call the lab")
}

func TestNeuralDisabledAndRequestGuardsNeverContactLab(t *testing.T) {
	client := &neuralNoClient{}
	handler := NewWithSession(client, t.TempDir(), nil, "", "")
	tests := []struct {
		method, path, body, origin, host string
		code                             int
	}{
		{"GET", "/api/neural", "", "", "127.0.0.1:8787", 200},
		{"POST", "/api/neural/start", `{"confirm":"TRAIN_SYNTHETIC"}`, "", "127.0.0.1:8787", 503},
		{"POST", "/api/neural/start", `{"confirm":"TRAIN_SYNTHETIC"}`, "http://example.com", "127.0.0.1:8787", 400},
		{"POST", "/api/neural/start", `{"confirm":"TRAIN_SYNTHETIC"} {}`, "", "127.0.0.1:8787", 400},
		{"POST", "/api/neural/start", `{"confirm":"TRAIN_SYNTHETIC","python":"arbitrary"}`, "", "127.0.0.1:8787", 400},
		{"POST", "/api/neural/start", `{}`, "", "127.0.0.1:8787", 400},
		{"POST", "/api/neural/predict", `{"job_id":"old","model":"mlp_seed_7","sample":0}`, "", "127.0.0.1:8787", 503},
		{"POST", "/api/neural/predict", `{"job_id":"old","model":"mlp_seed_7","sample":null}`, "", "127.0.0.1:8787", 400},
		{"POST", "/api/neural/predict", `{"job_id":"old","model":"mlp_seed_7"}`, "", "127.0.0.1:8787", 400},
		{"GET", "/api/neural", "", "", "example.com", 403},
	}
	for _, tc := range tests {
		t.Run(tc.path+tc.body+tc.origin+tc.host, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://127.0.0.1:8787"+tc.path, strings.NewReader(tc.body))
			r.Host = tc.host
			r.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			if tc.method == http.MethodGet && tc.code == 200 {
				var value map[string]any
				json.Unmarshal(w.Body.Bytes(), &value)
				if value["available"] != false || value["state"] != "disabled" {
					t.Fatal("unconfigured worker invented availability")
				}
			}
		})
	}
	if client.calls != 0 {
		t.Fatal("neural requests contacted the lab")
	}
}
