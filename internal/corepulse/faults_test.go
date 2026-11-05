package corepulse

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCheckpointFailureBeforeDeleteNeverMutates(t *testing.T) {
	f := newFake()
	_, err := RunPodCrashWithCheckpoint(f, "amf", nil, func(map[string]any) error {
		return errors.New("disk full")
	})
	if err == nil || containsAction(f.commands, "delete") {
		t.Fatalf("err=%v, commands=%v", err, f.commands)
	}
}

func TestFaultCheckpointExistsBeforeEachBlockingStep(t *testing.T) {
	f := newFake()
	lastPhase := ""
	_, err := RunPodCrashWithCheckpoint(f, "amf", nil, func(r map[string]any) error {
		phase := r["phase"].(string)
		if r["quality"] != "excluded" {
			t.Fatalf("unfinished run marked clean: %v", r)
		}
		if phase == "prepared" && containsAction(f.commands, "delete") {
			t.Fatal("initial checkpoint was too late")
		}
		if phase == "delete_requested" {
			if !containsAction(f.commands, "delete") || containsAction(f.commands, "wait") {
				t.Fatal("delete checkpoint was not saved before wait")
			}
		}
		lastPhase = phase
		return nil
	})
	if err != nil || lastPhase != "deployment_recovered" {
		t.Fatalf("phase=%s err=%v", lastPhase, err)
	}
}

func TestCheckpointFailureAfterDeleteStopsLifecycle(t *testing.T) {
	f := newFake()
	var saved map[string]any
	_, err := RunPodCrashWithCheckpoint(f, "amf", nil, func(r map[string]any) error {
		if r["phase"] == "delete_requested" {
			return errors.New("disk full after deletion")
		}
		saved = make(map[string]any)
		for k, v := range r {
			saved[k] = v
		}
		return nil
	})
	if err == nil || !containsAction(f.commands, "delete") || containsAction(f.commands, "wait") {
		t.Fatalf("err=%v commands=%v", err, f.commands)
	}
	if saved["phase"] != "delete_attempted" || saved["status"] != "recovery_pending" || saved["quality"] != "excluded" {
		t.Fatalf("last readable checkpoint was lost: %v", saved)
	}
}

type fakeKubectl struct {
	context      string
	owner        string
	commands     [][]string
	podReads     int
	failedAction string
}

func (f *fakeKubectl) Run(_ time.Duration, args ...string) (string, error) {
	f.commands = append(f.commands, args)
	if args[0] == f.failedAction {
		return "", fmt.Errorf("simulated failure")
	}
	if len(args) == 2 && args[0] == "config" {
		return f.context, nil
	}
	return "ok", nil
}

func (f *fakeKubectl) GetJSON(args ...string) (map[string]any, error) {
	f.commands = append(f.commands, args)
	if args[1] == "deployment/open5gs-amf" {
		return map[string]any{"spec": map[string]any{"replicas": float64(1), "selector": map[string]any{"matchLabels": map[string]any{"app": "amf"}}}, "status": map[string]any{"readyReplicas": float64(1)}}, nil
	}
	f.podReads++
	name, owner := "open5gs-amf-abc-123", f.owner
	if f.podReads > 1 {
		name, owner = "open5gs-amf-def-456", "open5gs-amf-def"
	}
	return map[string]any{"items": []any{map[string]any{
		"metadata": map[string]any{"name": name, "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "name": owner}}},
		"status":   map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	}}}, nil
}

func newFake() *fakeKubectl { return &fakeKubectl{context: LabContext, owner: "open5gs-amf-abc"} }

func containsAction(commands [][]string, action string) bool {
	for _, command := range commands {
		if command[0] == action {
			return true
		}
	}
	return false
}

func TestUnlistedTargetIsRejected(t *testing.T) {
	if _, err := PlanPodCrash("other"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestWrongContextNeverDeletes(t *testing.T) {
	f := newFake()
	f.context = "production"
	if _, err := RunPodCrash(f, "amf"); err == nil {
		t.Fatal("expected an error")
	}
	if containsAction(f.commands, "delete") {
		t.Fatal("deleted outside lab context")
	}
}

func TestUnownedPodNeverDeletes(t *testing.T) {
	f := newFake()
	f.owner = "other-app-abc"
	if _, err := RunPodCrash(f, "amf"); err == nil {
		t.Fatal("expected an error")
	}
	if containsAction(f.commands, "delete") {
		t.Fatal("deleted unowned pod")
	}
}

func TestCrashWaitsForRecovery(t *testing.T) {
	f := newFake()
	result, err := RunPodCrash(f, "amf")
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "recovered" || result["replacement_pod"] != "open5gs-amf-def-456" {
		t.Fatalf("unexpected result: %v", result)
	}
	actions := []string{}
	for _, command := range f.commands {
		actions = append(actions, command[0])
	}
	order := strings.Join(actions, ",")
	deleteAt, waitAt, rolloutAt := strings.Index(order, "delete"), strings.Index(order, "wait"), strings.Index(order, "rollout")
	if deleteAt < 0 || waitAt < 0 || rolloutAt < 0 || deleteAt > waitAt || waitAt > rolloutAt {
		t.Fatalf("wrong order: %s", order)
	}
}

func TestCrashObservesEachFaultStep(t *testing.T) {
	f := newFake()
	observed := 0
	result, err := RunPodCrashWithObserver(f, "amf", func() { observed++ })
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "recovered" || observed != 3 {
		t.Fatalf("observed %d steps, result %v", observed, result)
	}
}

func TestFailedRecoveryIsRecorded(t *testing.T) {
	f := newFake()
	f.failedAction = "rollout"
	result, err := RunPodCrash(f, "amf")
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "recovery_unverified" || result["error"] == nil || result["ended_at"] == nil {
		t.Fatalf("unexpected result: %v", result)
	}
}
