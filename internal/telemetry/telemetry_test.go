package telemetry

import (
	"math"
	"os"
	"path/filepath"
	"strings"
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

func TestCPUMilliRejectsInvalidQuantities(t *testing.T) {
	cases := []struct {
		in      string
		wantErr string
	}{
		{"-1", "negative"},
		{"-25m", "negative"},
		{"NaN", "non-finite"},
		{"Inf", "non-finite"},
		{"+Inf", "non-finite"},
		{"-Inf", "non-finite"},
	}
	for _, tc := range cases {
		_, err := cpuMilli(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("cpuMilli(%q) = %v, want error containing %q", tc.in, err, tc.wantErr)
		}
	}
}

func TestMemoryBytesRejectsInvalidQuantities(t *testing.T) {
	cases := []struct {
		in      string
		wantErr string
	}{
		{"-1Ki", "negative"},
		{"-32Mi", "negative"},
		{"NaNMi", "non-finite"},
		{"InfGi", "non-finite"},
		// Finite but larger than MaxInt64 after scaling.
		{"1e20Ti", "overflow"},
		{"9000000000000Mi", "overflow"},
		// Exactly 2^63 bytes: equal to float64(math.MaxInt64) but not representable.
		{"8388608Ti", "overflow"},
	}
	for _, tc := range cases {
		_, err := memoryBytes(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("memoryBytes(%q) = %v, want error containing %q", tc.in, err, tc.wantErr)
		}
	}
}

func TestResourceQuantitiesAcceptValidUnits(t *testing.T) {
	cpuCases := []struct {
		in   string
		want float64
	}{
		{"125000000n", 125}, // n → millicores via 1e-6
		{"25m", 25},
		{"1", 1000},
		{"0", 0},
		{"0.5", 500},
	}
	for _, tc := range cpuCases {
		got, err := cpuMilli(tc.in)
		if err != nil {
			t.Fatalf("cpuMilli(%q): %v", tc.in, err)
		}
		if math.Abs(got-tc.want) > 1e-9 {
			t.Fatalf("cpuMilli(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	memCases := []struct {
		in   string
		want int64
	}{
		{"32Mi", 32 * 1024 * 1024},
		{"1024Ki", 1024 * 1024},
		{"1Gi", 1024 * 1024 * 1024},
		{"1000", 1000},
	}
	for _, tc := range memCases {
		got, err := memoryBytes(tc.in)
		if err != nil {
			t.Fatalf("memoryBytes(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("memoryBytes(%q) = %v, want %v", tc.in, got, tc.want)
		}
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
