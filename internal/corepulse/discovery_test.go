package corepulse

import "testing"

func TestAnalyzeSnapshot(t *testing.T) {
	snapshot := map[string]any{"items": []any{
		map[string]any{"kind": "Deployment", "metadata": map[string]any{"name": "open5gs-amf"}, "spec": map[string]any{"replicas": float64(1)}, "status": map[string]any{"readyReplicas": float64(1)}},
		map[string]any{"kind": "Service", "metadata": map[string]any{"name": "sample-metrics", "annotations": map[string]any{"prometheus.io/scrape": "true", "prometheus.io/port": "9090"}}, "spec": map[string]any{"ports": []any{map[string]any{"port": float64(9090)}}}},
	}}
	result, err := AnalyzeSnapshot(snapshot, LabContext)
	if err != nil {
		t.Fatal(err)
	}
	expected := result["expected_deployments"].(map[string]bool)
	if !expected["open5gs-amf"] || expected["open5gs-upf"] {
		t.Fatalf("unexpected deployments: %v", expected)
	}
	if len(result["metric_names_verified"].([]string)) != 0 {
		t.Fatal("metrics were incorrectly verified")
	}
	candidate := result["prometheus_annotation_candidates"].([]map[string]any)[0]
	if candidate["port"] != "9090" || candidate["verified"] != false {
		t.Fatalf("unexpected candidate: %v", candidate)
	}
}

func TestAnalyzeSnapshotRejectsInvalidList(t *testing.T) {
	if _, err := AnalyzeSnapshot(map[string]any{"items": "wrong"}, LabContext); err == nil {
		t.Fatal("expected an error")
	}
}
