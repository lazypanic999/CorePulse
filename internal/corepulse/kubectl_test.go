package corepulse

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
	"time"
)

// The subprocess is an argument recorder, not kubectl. Intercept before the
// testing package parses flags so real exec argument delivery can be verified.
func TestMain(m *testing.M) {
	if os.Getenv("COREPULSE_ARGUMENT_RECORDER") == "1" {
		data, _ := json.Marshal(os.Args[1:])
		os.Stdout.Write(data)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestKubectlPinsOperationsBeforeExecBoundary(t *testing.T) {
	t.Setenv("COREPULSE_ARGUMENT_RECORDER", "1")
	for _, args := range [][]string{
		{"delete", "pod/open5gs-upf-demo", "--namespace", LabNamespace, "--wait=false"},
		{"get", "--raw", "/apis/metrics.k8s.io/v1beta1/namespaces/open5gs/pods"},
		{"logs", "pod/ueransim-gnb-demo", "--namespace", LabNamespace, "--timestamps"},
		{"exec", "pod/ueransim-ue-demo", "--namespace", LabNamespace, "--", "ping", "-c", "2", "10.45.0.1"},
		{"config", "current-context"},
	} {
		before := append([]string(nil), args...)
		output, err := (Kubectl{Binary: os.Args[0]}).Run(5*time.Second, args...)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		if err := json.Unmarshal([]byte(output), &got); err != nil {
			t.Fatal(err)
		}
		want := append([]string{"--context", LabContext}, args...)
		if args[0] == "config" {
			want = args
		}
		if !slices.Equal(got, want) || !slices.Equal(before, args) {
			t.Fatalf("argument delivery=%v want=%v original=%v", got, want, args)
		}
	}
}

func TestCancelledKubectlDoesNotStartWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Kubectl{Binary: os.Args[0], Context: ctx}).Run(time.Minute, "get", "pods")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestDiscoveryRejectsOtherContextBeforeReadingInventory(t *testing.T) {
	c := newFake()
	c.context = "production"
	if _, err := Discover(c); err == nil {
		t.Fatal("discovery accepted a different context")
	}
	if containsAction(c.commands, "get") {
		t.Fatal("inventory read outside the lab context")
	}
}
