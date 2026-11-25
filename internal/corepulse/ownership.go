package corepulse

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"
)

// podIdentity is the exact Pod -> ReplicaSet -> Deployment chain a mutation
// may act on. Names alone never qualify: every link is matched by UID.
type podIdentity struct {
	Name          string
	UID           string
	ReplicaSet    string
	ReplicaSetUID string
	Deployment    string
	DeploymentUID string
}

// Kubernetes object names (DNS-1123 subdomains) never contain a slash, so a
// validated name cannot escape the fixed API path used for deletion.
var objectName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)

// controllerRef returns the single owner reference marked controller=true.
// Non-controller references are ignored; zero or several controllers refuse.
func controllerRef(metadata map[string]any) (map[string]any, error) {
	var found map[string]any
	for _, raw := range array(metadata["ownerReferences"]) {
		ref := object(raw)
		if ref["controller"] != true {
			continue
		}
		if found != nil {
			return nil, errors.New("several controller owner references")
		}
		found = ref
	}
	if found == nil {
		return nil, errors.New("no controller owner reference")
	}
	return found, nil
}

func liveObject(metadata map[string]any, kind, name string) error {
	if stringValue(metadata["name"]) != name || !objectName.MatchString(name) {
		return fmt.Errorf("%s name is missing or invalid", kind)
	}
	if stringValue(metadata["uid"]) == "" {
		return fmt.Errorf("%s %s has no UID", kind, name)
	}
	if stringValue(metadata["namespace"]) != LabNamespace {
		return fmt.Errorf("%s %s is outside namespace %s", kind, name, LabNamespace)
	}
	if metadata["deletionTimestamp"] != nil {
		return fmt.Errorf("%s %s is being deleted", kind, name)
	}
	return nil
}

// verifiedTargetPod selects the single ready pod of an allowlisted deployment
// and resolves its exact controller chain in the lab namespace: the pod's one
// controller must be a ReplicaSet whose UID matches the reference, and that
// ReplicaSet's one controller must be this Deployment by name and UID. Shared
// name prefixes or matching labels alone do not qualify.
func verifiedTargetPod(client Client, deployment string) (podIdentity, error) {
	deploy, pod, err := selectedPodObjects(client, deployment)
	if err != nil {
		return podIdentity{}, err
	}
	deployMeta, podMeta := object(deploy["metadata"]), object(pod["metadata"])
	if err := liveObject(deployMeta, "deployment", deployment); err != nil {
		return podIdentity{}, err
	}
	id := podIdentity{
		Name: stringValue(podMeta["name"]), UID: stringValue(podMeta["uid"]),
		Deployment: deployment, DeploymentUID: stringValue(deployMeta["uid"]),
	}
	if err := liveObject(podMeta, "pod", id.Name); err != nil {
		return podIdentity{}, err
	}
	podOwner, err := controllerRef(podMeta)
	if err != nil {
		return podIdentity{}, fmt.Errorf("pod %s: %w", id.Name, err)
	}
	id.ReplicaSet, id.ReplicaSetUID = stringValue(podOwner["name"]), stringValue(podOwner["uid"])
	if podOwner["apiVersion"] != "apps/v1" || podOwner["kind"] != "ReplicaSet" || !objectName.MatchString(id.ReplicaSet) || id.ReplicaSetUID == "" {
		return podIdentity{}, fmt.Errorf("pod %s controller is not an apps/v1 ReplicaSet with a UID", id.Name)
	}
	replicaSet, err := client.GetJSON("get", "replicaset/"+id.ReplicaSet, "--namespace", LabNamespace)
	if err != nil {
		return podIdentity{}, err
	}
	rsMeta := object(replicaSet["metadata"])
	if err := liveObject(rsMeta, "replicaset", id.ReplicaSet); err != nil {
		return podIdentity{}, err
	}
	if stringValue(rsMeta["uid"]) != id.ReplicaSetUID {
		return podIdentity{}, fmt.Errorf("replicaset %s UID differs from the pod owner reference", id.ReplicaSet)
	}
	rsOwner, err := controllerRef(rsMeta)
	if err != nil {
		return podIdentity{}, fmt.Errorf("replicaset %s: %w", id.ReplicaSet, err)
	}
	if rsOwner["apiVersion"] != "apps/v1" || rsOwner["kind"] != "Deployment" || rsOwner["name"] != deployment || stringValue(rsOwner["uid"]) != id.DeploymentUID {
		return podIdentity{}, fmt.Errorf("replicaset %s is not controlled by deployment %s with UID %s", id.ReplicaSet, deployment, id.DeploymentUID)
	}
	return id, nil
}

// VerifyFaultTargets resolves the exact UID chain for every allowlisted fault
// target, read-only, so a report can show the same ownership the delete path
// will require. It does not pin anything: apply re-verifies before deleting.
func VerifyFaultTargets(client Client) error {
	for _, target := range []string{"amf", "upf"} {
		if _, err := verifiedTargetPod(client, allowedTargets[target]); err != nil {
			return fmt.Errorf("%s fault target: %w", target, err)
		}
	}
	return nil
}

// deletePodWithUID deletes the pod only if its UID still matches. kubectl has
// no flag for DeleteOptions preconditions, so the request goes through
// `kubectl delete --raw` with a DeleteOptions body; the API server rejects it
// with a conflict when a different object now holds the name.
func deletePodWithUID(client Client, timeout time.Duration, pod podIdentity) error {
	if !objectName.MatchString(pod.Name) || pod.UID == "" {
		return errors.New("refusing to delete a pod without a valid name and UID")
	}
	body, err := json.Marshal(map[string]any{
		"apiVersion": "v1", "kind": "DeleteOptions",
		"preconditions": map[string]any{"uid": pod.UID},
	})
	if err != nil {
		return err
	}
	file, err := os.CreateTemp("", "corepulse-delete-*.json")
	if err != nil {
		return fmt.Errorf("write delete preconditions: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(body); err != nil {
		file.Close()
		return fmt.Errorf("write delete preconditions: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write delete preconditions: %w", err)
	}
	_, err = client.Run(timeout, "delete", "--raw", "/api/v1/namespaces/"+LabNamespace+"/pods/"+pod.Name, "--filename", file.Name())
	return err
}
