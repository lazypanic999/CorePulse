package corepulse

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strings"
	"time"
)

const LabContext = "open5gs-lab"

var allowedTargets = map[string]string{"amf": "open5gs-amf", "upf": "open5gs-upf"}

func PlanPodCrash(target string) (map[string]any, error) {
	deployment, ok := allowedTargets[target]
	if !ok {
		return nil, fmt.Errorf("target must be one of: amf, upf")
	}
	return map[string]any{
		"fault": "pod_crash", "target": target, "namespace": LabNamespace, "deployment": deployment,
		"effect":   "Delete one ready, deployment-owned pod; Kubernetes creates its replacement.",
		"recovery": "Wait for the deployment to become available again.",
	}, nil
}

func selectedPod(client Client, deployment string) (string, error) {
	data, err := client.GetJSON("get", "deployment/"+deployment, "--namespace", LabNamespace)
	if err != nil {
		return "", err
	}
	spec, status := object(data["spec"]), object(data["status"])
	selector := object(spec["selector"])
	labels := object(selector["matchLabels"])
	if len(labels) == 0 || len(array(selector["matchExpressions"])) > 0 {
		return "", fmt.Errorf("deployment selector is missing or has unsupported expressions")
	}
	replicas := spec["replicas"]
	if replicas == nil {
		replicas = float64(1)
	}
	if replicas != float64(1) && replicas != 1 {
		return "", fmt.Errorf("target deployment must have exactly one ready replica")
	}
	if status["readyReplicas"] != float64(1) && status["readyReplicas"] != 1 {
		return "", fmt.Errorf("target deployment must have exactly one ready replica")
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value, ok := labels[key].(string)
		if !ok || key == "" || value == "" || strings.ContainsAny(key+value, ",= ") {
			return "", fmt.Errorf("deployment selector has invalid labels")
		}
		parts = append(parts, key+"="+value)
	}
	pods, err := client.GetJSON("get", "pods", "--namespace", LabNamespace, "--selector", strings.Join(parts, ","))
	if err != nil {
		return "", err
	}
	items := array(pods["items"])
	if len(items) != 1 {
		return "", fmt.Errorf("expected exactly one matching pod")
	}
	pod := object(items[0])
	podStatus := object(pod["status"])
	if podStatus["phase"] != "Running" {
		return "", fmt.Errorf("target pod is not running")
	}
	ready := false
	for _, raw := range array(podStatus["conditions"]) {
		c := object(raw)
		if c["type"] == "Ready" && c["status"] == "True" {
			ready = true
		}
	}
	if !ready {
		return "", fmt.Errorf("target pod is not ready")
	}
	metadata := object(pod["metadata"])
	name := stringValue(metadata["name"])
	if name == "" {
		return "", fmt.Errorf("target pod has no name")
	}
	owned := false
	for _, raw := range array(metadata["ownerReferences"]) {
		owner := object(raw)
		if owner["kind"] == "ReplicaSet" && strings.HasPrefix(stringValue(owner["name"]), deployment+"-") {
			owned = true
		}
	}
	if !owned {
		return "", fmt.Errorf("matching pod is not owned by the expected deployment")
	}
	return name, nil
}

func newRunID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]), nil
}

// RunPodCrash checks the lab context and workload before any mutation.
func RunPodCrash(client Client, target string) (map[string]any, error) {
	plan, err := PlanPodCrash(target)
	if err != nil {
		return nil, err
	}
	context, err := client.Run(30*time.Second, "config", "current-context")
	if err != nil {
		return nil, err
	}
	if context != LabContext {
		return nil, fmt.Errorf("current context %q differs from lab context %q", context, LabContext)
	}
	deployment := stringValue(plan["deployment"])
	pod, err := selectedPod(client, deployment)
	if err != nil {
		return nil, err
	}
	id, err := newRunID()
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"schema_version": 1, "run_id": id, "fault": "pod_crash", "target": target,
		"context": context, "namespace": LabNamespace, "pod_deleted": pod,
		"started_at": time.Now().UTC().Format(time.RFC3339Nano), "ended_at": nil, "status": "recovery_pending",
	}
	defer func() { result["ended_at"] = time.Now().UTC().Format(time.RFC3339Nano) }()
	// Record any failure after the delete attempt so the caller can persist it.
	steps := []struct {
		timeout time.Duration
		args    []string
	}{
		{30 * time.Second, []string{"delete", "pod/" + pod, "--namespace", LabNamespace, "--wait=false"}},
		{210 * time.Second, []string{"wait", "--for=delete", "pod/" + pod, "--namespace", LabNamespace, "--timeout=180s"}},
		{210 * time.Second, []string{"rollout", "status", "deployment/" + deployment, "--namespace", LabNamespace, "--timeout=180s"}},
	}
	for _, step := range steps {
		if _, err := client.Run(step.timeout, step.args...); err != nil {
			result["status"], result["error"] = "recovery_unverified", err.Error()
			return result, nil
		}
	}
	replacement, err := selectedPod(client, deployment)
	if err == nil && replacement == pod {
		err = fmt.Errorf("no replacement pod was observed")
	}
	if err != nil {
		result["status"], result["error"] = "recovery_unverified", err.Error()
		return result, nil
	}
	result["replacement_pod"], result["status"] = replacement, "recovered"
	return result, nil
}
