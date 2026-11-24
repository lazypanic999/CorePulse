package corepulse

import (
	"encoding/json"
	"strings"
	"testing"
)

// Each case breaks one link of the Pod -> ReplicaSet -> Deployment chain on the
// initial selection. None may reach a delete.
func TestBrokenOwnershipChainRefusesBeforeDelete(t *testing.T) {
	cases := map[string]func(kind string, metadata map[string]any){
		"pod owner without controller flag": func(kind string, m map[string]any) {
			if kind == "pod" {
				r := ref("ReplicaSet", "open5gs-amf-abc", "open5gs-amf-abc-uid")
				delete(r, "controller")
				m["ownerReferences"] = []any{r}
			}
		},
		"pod owner with controller false": func(kind string, m map[string]any) {
			if kind == "pod" {
				r := ref("ReplicaSet", "open5gs-amf-abc", "open5gs-amf-abc-uid")
				r["controller"] = false
				m["ownerReferences"] = []any{r}
			}
		},
		"pod with two controllers": func(kind string, m map[string]any) {
			if kind == "pod" {
				m["ownerReferences"] = append(array(m["ownerReferences"]), ref("ReplicaSet", "open5gs-amf-def", "open5gs-amf-def-uid"))
			}
		},
		"pod controller is not a ReplicaSet": func(kind string, m map[string]any) {
			if kind == "pod" {
				m["ownerReferences"] = []any{ref("StatefulSet", "open5gs-amf-abc", "open5gs-amf-abc-uid")}
			}
		},
		"pod owner UID differs from ReplicaSet": func(kind string, m map[string]any) {
			if kind == "pod" {
				m["ownerReferences"] = []any{ref("ReplicaSet", "open5gs-amf-abc", "stale-uid")}
			}
		},
		"pod owner reference has no UID": func(kind string, m map[string]any) {
			if kind == "pod" {
				m["ownerReferences"] = []any{ref("ReplicaSet", "open5gs-amf-abc", "")}
			}
		},
		"similarly prefixed ReplicaSet of another deployment": func(kind string, m map[string]any) {
			if kind == "replicaset" {
				m["ownerReferences"] = []any{ref("Deployment", "open5gs-amf-canary", "canary-uid")}
			}
		},
		"ReplicaSet controller UID differs": func(kind string, m map[string]any) {
			if kind == "replicaset" {
				m["ownerReferences"] = []any{ref("Deployment", "open5gs-amf", "old-deploy-uid")}
			}
		},
		"ReplicaSet without controller": func(kind string, m map[string]any) {
			if kind == "replicaset" {
				m["ownerReferences"] = []any{}
			}
		},
		"ReplicaSet being deleted": func(kind string, m map[string]any) {
			if kind == "replicaset" {
				m["deletionTimestamp"] = "2026-11-24T00:00:00Z"
			}
		},
		"ReplicaSet in another namespace": func(kind string, m map[string]any) {
			if kind == "replicaset" {
				m["namespace"] = "default"
			}
		},
		"pod without UID": func(kind string, m map[string]any) {
			if kind == "pod" {
				m["uid"] = ""
			}
		},
		"pod being deleted": func(kind string, m map[string]any) {
			if kind == "pod" {
				m["deletionTimestamp"] = "2026-11-24T00:00:00Z"
			}
		},
		"pod name that could escape the API path": func(kind string, m map[string]any) {
			if kind == "pod" {
				m["name"] = "open5gs-amf-abc-123/../../secrets"
			}
		},
		"deployment without UID": func(kind string, m map[string]any) {
			if kind == "deployment" {
				delete(m, "uid")
			}
		},
	}
	for name, breakLink := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFake()
			f.tweak = func(kind string, m map[string]any, _ int) { breakLink(kind, m) }
			if _, err := RunPodCrash(f, "amf"); err == nil {
				t.Fatal("broken chain accepted")
			}
			if containsAction(f.commands, "delete") {
				t.Fatalf("deleted despite a broken chain: %v", f.commands)
			}
		})
	}
}

// A pod whose labels match the selector but that a similarly named, unrelated
// ReplicaSet controls (overlapping selectors) must not qualify.
func TestOverlappingSelectorPodOfAnotherReplicaSetRefuses(t *testing.T) {
	f := newFake()
	f.owner = "open5gs-amf-abcx"
	if _, err := RunPodCrash(f, "amf"); err == nil || containsAction(f.commands, "delete") {
		t.Fatalf("err=%v commands=%v", err, f.commands)
	}
}

func TestDeleteCarriesTheSelectedPodUIDPrecondition(t *testing.T) {
	f := newFake()
	result, err := RunPodCrash(f, "amf")
	if err != nil || result["status"] != "recovered" {
		t.Fatalf("err=%v result=%v", err, result)
	}
	var deleteArgs []string
	for _, command := range f.commands {
		if command[0] == "delete" {
			deleteArgs = command
		}
	}
	if strings.Join(deleteArgs[:3], " ") != "delete --raw /api/v1/namespaces/open5gs/pods/open5gs-amf-abc-123" {
		t.Fatalf("delete was not an exact UID-preconditioned request: %v", deleteArgs)
	}
	if len(f.deleteBodies) != 1 {
		t.Fatalf("delete bodies: %v", f.deleteBodies)
	}
	var options struct {
		Kind          string            `json:"kind"`
		Preconditions map[string]string `json:"preconditions"`
	}
	if err := json.Unmarshal([]byte(f.deleteBodies[0]), &options); err != nil {
		t.Fatal(err)
	}
	if options.Kind != "DeleteOptions" || options.Preconditions["uid"] != "open5gs-amf-abc-123-uid" {
		t.Fatalf("unexpected delete options: %s", f.deleteBodies[0])
	}
	if result["pod_deleted_uid"] != "open5gs-amf-abc-123-uid" || result["deployment_uid"] != "deploy-uid" ||
		result["replica_set_uid"] != "open5gs-amf-abc-uid" || result["replacement_pod_uid"] != "open5gs-amf-def-456-uid" {
		t.Fatalf("identity chain not recorded: %v", result)
	}
}

func TestRejectedPreconditionLeavesRecoveryUnverified(t *testing.T) {
	f := newFake()
	f.failedAction = "delete"
	result, err := RunPodCrash(f, "amf")
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "recovery_unverified" || result["phase"] != "delete_attempted" || containsAction(f.commands, "wait") {
		t.Fatalf("result=%v commands=%v", result, f.commands)
	}
}

func TestDeploymentReplacedDuringRunIsNotRecovered(t *testing.T) {
	f := newFake()
	deploymentReads := 0
	f.tweak = func(kind string, m map[string]any, _ int) {
		if kind == "deployment" {
			deploymentReads++
		}
		if deploymentReads < 2 {
			return
		}
		if kind == "deployment" {
			m["uid"] = "new-deploy-uid"
		}
		if kind == "replicaset" {
			m["ownerReferences"] = []any{ref("Deployment", "open5gs-amf", "new-deploy-uid")}
		}
	}
	result, err := RunPodCrash(f, "amf")
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "recovery_unverified" || !strings.Contains(stringValue(result["error"]), "replaced") {
		t.Fatalf("deployment replacement accepted: %v", result)
	}
}

func TestSamePodAfterRolloutIsNotARecovery(t *testing.T) {
	f := newFake()
	f.tweak = func(kind string, m map[string]any, podReads int) {
		if kind == "pod" && podReads >= 2 {
			m["name"], m["uid"] = "open5gs-amf-abc-123", "open5gs-amf-abc-123-uid"
			m["ownerReferences"] = []any{ref("ReplicaSet", "open5gs-amf-abc", "open5gs-amf-abc-uid")}
		}
	}
	result, err := RunPodCrash(f, "amf")
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "recovery_unverified" {
		t.Fatalf("same pod accepted as replacement: %v", result)
	}
}
