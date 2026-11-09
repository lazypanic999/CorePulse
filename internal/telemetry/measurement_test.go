package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func metricFixture(fields string) fakeClient {
	return fakeClient{context: "open5gs-lab", raw: `{"items":[{"metadata":{"name":"open5gs-amf-one"},` + fields + `"containers":[{"usage":{"cpu":"3m","memory":"1Mi"}}]}]}`}
}

func TestCapturePreservesPerPodMeasurement(t *testing.T) {
	client := fakeClient{context: "open5gs-lab", raw: `{"items":[
		{"metadata":{"name":"open5gs-upf-one"},"timestamp":"2026-11-09T00:00:20Z","window":"30s","containers":[{"usage":{"cpu":"2m","memory":"1Mi"}}]},
		{"metadata":{"name":"open5gs-amf-one"},"timestamp":"2026-11-09T00:00:00.123Z","window":"1m0.008s","containers":[{"usage":{"cpu":"3m","memory":"1Mi"}}]}
	]}`}
	sample, err := Capture(client)
	if err != nil {
		t.Fatal(err)
	}
	if sample.CPUMilli != 5 || sample.MemoryBytes != 2*1024*1024 || sample.SchemaVersion != 1 {
		t.Fatalf("additive metadata changed values: %+v", sample)
	}
	wantTimes := []string{"2026-11-09T00:00:00.123Z", "2026-11-09T00:00:20Z"}
	wantWindows := []string{"1m0.008s", "30s"}
	for i, pod := range sample.Pods {
		if pod.Measurement == nil || pod.Measurement.Timestamp.Format(time.RFC3339Nano) != wantTimes[i] || pod.Measurement.Window != wantWindows[i] {
			t.Fatalf("lost per-pod metadata: %+v", pod)
		}
		if !sample.Timestamp.After(pod.Measurement.Timestamp) {
			t.Fatal("capture timestamp was replaced with source time")
		}
	}
	// Repeated source frames stay repeated. Capturing them does not fabricate
	// distinct measurements or silently deduplicate the historical row stream.
	second, err := Capture(client)
	if err != nil || *second.Pods[0].Measurement != *sample.Pods[0].Measurement {
		t.Fatalf("source frame changed across captures: %+v, %v", second, err)
	}
}

func TestCaptureMeasurementMissingOrMalformed(t *testing.T) {
	legacy, err := Capture(metricFixture(""))
	if err != nil || legacy.Pods[0].Measurement != nil {
		t.Fatalf("missing metadata must remain unknown: %+v, %v", legacy, err)
	}
	for _, fields := range []string{
		`"timestamp":"2026-11-09T00:00:00Z",`,
		`"window":"1m",`,
		`"timestamp":"broken","window":"1m",`,
		`"timestamp":"0001-01-01T00:00:00Z","window":"1m",`,
		`"timestamp":null,"window":"1m",`,
		`"timestamp":7,"window":"1m",`,
		`"timestamp":"2026-11-09T00:00:00Z","window":null,`,
		`"timestamp":"2026-11-09T00:00:00Z","window":60,`,
		`"timestamp":"2026-11-09T00:00:00Z","window":"",`,
		`"timestamp":"2026-11-09T00:00:00Z","window":"0s",`,
		`"timestamp":"2026-11-09T00:00:00Z","window":"-1s",`,
		`"timestamp":"2026-11-09T00:00:00Z","window":"forever",`,
		`"timestamp":"2026-11-09T00:00:00Z","window":"999999999999999999h",`,
	} {
		t.Run(fields, func(t *testing.T) {
			store := NewStore(filepath.Join(t.TempDir(), "raw.jsonl"))
			if err := store.Capture(metricFixture(fields)); err == nil || !strings.Contains(err.Error(), "measurement") {
				t.Fatalf("malformed metadata accepted: %v", err)
			}
			rows, lastError := store.Snapshot()
			if len(rows) != 0 || lastError == "" {
				t.Fatal("bad measurement became an accepted sample")
			}
			if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
				t.Fatal("bad measurement was persisted")
			}
		})
	}
}

func TestMeasurementJSONCompatibility(t *testing.T) {
	for _, raw := range []string{
		`{"name":"amf","cpu_millicores":0,"memory_bytes":0}`,
		`{"name":"amf","cpu_millicores":0,"memory_bytes":0,"measurement":{"timestamp":"2026-11-09T00:00:00.123Z","window":"1m0.008s"}}`,
	} {
		var pod PodUsage
		if err := json.Unmarshal([]byte(raw), &pod); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(pod)
		if err != nil || string(encoded) != raw {
			t.Fatalf("round trip changed metadata/legacy bytes: %s, %v", encoded, err)
		}
	}
	for _, metadata := range []string{
		`{}`, `{"timestamp":"2026-11-09T00:00:00Z"}`, `{"window":"1m"}`,
		`{"timestamp":"invalid","window":"1m"}`,
		`{"timestamp":"2026-11-09T00:00:00Z","window":"0s"}`,
		`{"timestamp":"2026-11-09T00:00:00Z","window":false}`,
	} {
		var pod PodUsage
		if err := json.Unmarshal([]byte(`{"name":"amf","measurement":`+metadata+`}`), &pod); err == nil {
			t.Fatalf("stored malformed measurement accepted: %s", metadata)
		}
	}
}

func TestStoreReloadPreservesMeasurement(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "raw.jsonl"))
	if err := store.Capture(metricFixture(`"timestamp":"2026-11-09T00:00:00Z","window":"1m0.008s",`)); err != nil {
		t.Fatal(err)
	}
	rows, lastError := NewStore(store.Path()).Snapshot()
	if len(rows) != 1 || lastError != "" || rows[0].Pods[0].Measurement == nil || rows[0].Pods[0].Measurement.Window != "1m0.008s" {
		t.Fatalf("stored measurement did not survive reload: %+v, %s", rows, lastError)
	}
}
