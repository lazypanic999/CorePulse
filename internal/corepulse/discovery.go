package corepulse

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const LabNamespace = "open5gs"

func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func array(v any) []any {
	a, _ := v.([]any)
	return a
}

func stringValue(v any) string { s, _ := v.(string); return s }

func item(resource map[string]any) map[string]any {
	metadata, spec, status := object(resource["metadata"]), object(resource["spec"]), object(resource["status"])
	labels, annotations, ports := metadata["labels"], metadata["annotations"], spec["ports"]
	if labels == nil {
		labels = map[string]any{}
	}
	if annotations == nil {
		annotations = map[string]any{}
	}
	if ports == nil {
		ports = []any{}
	}
	return map[string]any{
		"name": metadata["name"], "labels": labels, "annotations": annotations,
		"ports": ports, "phase": status["phase"], "ready_replicas": status["readyReplicas"], "replicas": spec["replicas"],
	}
}

// AnalyzeSnapshot reports candidates only; it does not claim scrape success.
func AnalyzeSnapshot(snapshot map[string]any, context string) (map[string]any, error) {
	resources, ok := snapshot["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("expected a Kubernetes List with an items array")
	}
	inventory := map[string][]map[string]any{"deployments": {}, "pods": {}, "services": {}}
	candidates := []map[string]any{}
	kinds := map[string]string{"Deployment": "deployments", "Pod": "pods", "Service": "services"}
	for _, raw := range resources {
		resource, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind := stringValue(resource["kind"])
		key, ok := kinds[kind]
		if !ok {
			continue
		}
		entry := item(resource)
		inventory[key] = append(inventory[key], entry)
		annotations := object(entry["annotations"])
		if strings.EqualFold(stringValue(annotations["prometheus.io/scrape"]), "true") {
			path := stringValue(annotations["prometheus.io/path"])
			if path == "" {
				path = "/metrics"
			}
			candidates = append(candidates, map[string]any{"kind": kind, "name": entry["name"], "path": path, "port": annotations["prometheus.io/port"], "verified": false})
		}
	}
	for _, entries := range inventory {
		sort.Slice(entries, func(i, j int) bool { return stringValue(entries[i]["name"]) < stringValue(entries[j]["name"]) })
	}
	sort.Slice(candidates, func(i, j int) bool {
		ki, kj := stringValue(candidates[i]["kind"]), stringValue(candidates[j]["kind"])
		if ki != kj {
			return ki < kj
		}
		return stringValue(candidates[i]["name"]) < stringValue(candidates[j]["name"])
	})
	names := map[string]bool{}
	for _, entry := range inventory["deployments"] {
		names[stringValue(entry["name"])] = true
	}
	expected := map[string]bool{}
	for _, name := range []string{"open5gs-amf", "open5gs-upf", "ueransim-gnb", "ueransim-ue"} {
		expected[name] = names[name]
	}
	return map[string]any{
		"schema_version": 1, "generated_at": time.Now().UTC().Format(time.RFC3339Nano), "context": context,
		"namespace": LabNamespace, "inventory": inventory, "expected_deployments": expected,
		"prometheus_annotation_candidates": candidates, "metric_names_verified": []string{},
		"note": "Annotations suggest scrape candidates; they do not prove an endpoint or metric is available.",
	}, nil
}

func Discover(client Client) (map[string]any, error) {
	context, err := client.Run(30*time.Second, "config", "current-context")
	if err != nil {
		return nil, err
	}
	snapshot, err := client.GetJSON("get", "deployments,pods,services", "--namespace", LabNamespace, "--request-timeout=10s")
	if err != nil {
		return nil, err
	}
	return AnalyzeSnapshot(snapshot, context)
}
