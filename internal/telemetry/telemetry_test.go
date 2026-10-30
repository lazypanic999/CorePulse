package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeClient struct {
	context string
	raw     string
}

func (f fakeClient) Run(_ time.Duration, args ...string) (string, error) {
	if args[0] == "config" {
		return f.context, nil
	}
	return f.raw, nil
}
func (f fakeClient) GetJSON(_ ...string) (map[string]any, error) { return nil, nil }

func TestCaptureResourceMetrics(t *testing.T) {
	client := fakeClient{context: "open5gs-lab", raw: `{"items":[{"metadata":{"name":"open5gs-amf-1"},"containers":[{"usage":{"cpu":"125000000n","memory":"32Mi"}}]},{"metadata":{"name":"open5gs-upf-1"},"containers":[{"usage":{"cpu":"25m","memory":"1024Ki"}}]}]}`}
	sample, err := Capture(client)
	if err != nil {
		t.Fatal(err)
	}
	if sample.PodCount != 2 || sample.CPUMilli != 150 || sample.MemoryBytes != 33*1024*1024 {
		t.Fatalf("unexpected sample: %+v", sample)
	}
	if sample.Source != Source || sample.Context != "open5gs-lab" {
		t.Fatalf("unexpected provenance: %+v", sample)
	}
}

func TestWrongContextRejectsMetrics(t *testing.T) {
	if _, err := Capture(fakeClient{context: "production"}); err == nil {
		t.Fatal("expected wrong-context error")
	}
}

func TestStorePersistsOnlyRealSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry.jsonl")
	store := NewStore(path)
	client := fakeClient{context: "open5gs-lab", raw: `{"items":[{"metadata":{"name":"amf"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]}]}`}
	if err := store.Capture(client); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("missing raw sample: %v", err)
	}
	loaded := NewStore(path)
	samples, lastError := loaded.Snapshot()
	if len(samples) != 1 || lastError != "" {
		t.Fatalf("unexpected store: %d, %q", len(samples), lastError)
	}
	if err := loaded.Capture(fakeClient{context: "production"}); err == nil {
		t.Fatal("expected context error")
	}
	samples, _ = loaded.Snapshot()
	if len(samples) != 1 {
		t.Fatal("failed sample should not be persisted")
	}
}
