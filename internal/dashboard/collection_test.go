package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/collection"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type lockSpyClient struct{ calls int }

func (c *lockSpyClient) Run(_ time.Duration, _ ...string) (string, error) {
	c.calls++
	return "", fmt.Errorf("cluster must not be accessed")
}

func TestOwnershipEndpointReadsMarkerWithoutClusterAccess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "marker")
	t.Setenv("COREPULSE_LOCK_DIR", dir)
	client := &lockSpyClient{}
	handler := New(client, t.TempDir())
	request := func() collection.OwnershipState {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/collection", nil))
		var state collection.OwnershipState
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &state) != nil || client.calls != 0 {
			t.Fatalf("ownership lookup touched cluster or failed: %d calls=%d %s", w.Code, client.calls, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "\"token\"") {
			t.Fatal("private release token exposed")
		}
		return state
	}
	if state := request(); !state.Available || state.Locked {
		t.Fatalf("unexpected initial state: %+v", state)
	}
	owner, err := collection.AcquireOwnership("baseline")
	if err != nil {
		t.Fatal(err)
	}
	if state := request(); !state.Available || !state.Locked || state.Owner == nil || state.Owner.Operation != "baseline" {
		t.Fatalf("owner not visible: %+v", state)
	}
	if err := owner.Release(); err != nil {
		t.Fatal(err)
	}
	if state := request(); !state.Available || state.Locked {
		t.Fatalf("stale view after release: %+v", state)
	}
}
func (c *lockSpyClient) GetJSON(_ ...string) (map[string]any, error) {
	c.calls++
	return nil, fmt.Errorf("cluster must not be accessed")
}

func TestDashboardCollectionRefusesBeforeTelemetryOrPreflight(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COREPULSE_LOCK_DIR", filepath.Join(dir, "shared-lock"))
	owner, err := collection.AcquireOwnership("cli-baseline")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.Release(); err != nil {
			t.Fatal(err)
		}
	}()
	client := &lockSpyClient{}
	handler := NewWithTelemetry(client, filepath.Join(dir, "different-runs"), telemetry.NewStore(filepath.Join(dir, "raw.jsonl")))
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787/api/experiments/pod-crash", strings.NewReader(`{"target":"amf","confirm":"DELETE_ONE_LAB_POD"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusConflict || client.calls != 0 || !strings.Contains(w.Body.String(), "cli-baseline") {
		t.Fatalf("contention not enforced before all I/O: %d calls=%d %s", w.Code, client.calls, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/api/runs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ownership blocked read-only run history: %d", w.Code)
	}
}
