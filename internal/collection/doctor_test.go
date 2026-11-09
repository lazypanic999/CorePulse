package collection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type diagnosticClient struct {
	labClient
	t             *testing.T
	mode          string
	resourceCalls int
	metricsSeen   bool
	owner         *Ownership
}

func (c *diagnosticClient) Run(timeout time.Duration, args ...string) (string, error) {
	if args[0] == "config" {
		if c.mode == "wrong-context" {
			return "production", nil
		}
		return c.labClient.Run(timeout, args...)
	}
	c.resourceCalls++
	switch args[0] {
	case "exec":
		if !strings.Contains(strings.Join(args, " "), "-- ping ") {
			c.t.Fatalf("diagnostic exec was not the existing packet probe: %v", args)
		}
		if c.mode == "unavailable-probe" {
			return "ping not available", errors.New("command failed")
		}
	case "get":
		c.metricsSeen = true
		if c.mode == "missing-metrics" {
			return `{"items":[{"metadata":{"name":"open5gs-amf-abc-pod"},"containers":[{"usage":{"cpu":"0","memory":"0"}}]}]}`, nil
		}
		if c.mode == "new-owner" {
			var err error
			c.owner, err = AcquireOwnership("concurrent-experiment")
			if err != nil {
				c.t.Fatal(err)
			}
		}
	case "logs":
		if c.mode == "n2-down" && strings.Contains(args[1], "gnb") {
			return fmt.Sprintf("%s [2026-11-09 01:00:00.000] [ngap] [error] Association terminated for AMF[2]\n", time.Now().UTC().Format(time.RFC3339Nano)), nil
		}
	default:
		c.t.Fatalf("diagnostic attempted unexpected command: %v", args)
	}
	return c.labClient.Run(timeout, args...)
}

func (c *diagnosticClient) GetJSON(args ...string) (map[string]any, error) {
	c.resourceCalls++
	if args[0] != "get" {
		c.t.Fatalf("diagnostic mutation: %v", args)
	}
	if c.mode == "unready" {
		return nil, errors.New("workload is not ready")
	}
	d, err := c.labClient.GetJSON(args...)
	if c.mode == "pod-changed" && c.metricsSeen && args[1] == "pods" {
		for _, item := range d["items"].([]any) {
			meta := item.(map[string]any)["metadata"].(map[string]any)
			meta["name"] = meta["name"].(string) + "-new"
		}
	}
	return d, err
}

func diagnosticStatus(r DiagnosticReport, name string) string {
	for _, check := range r.Checks {
		if check.Name == name {
			return check.Status
		}
	}
	return "absent"
}

func TestDiagnosticSeparatesPacketProtocolMetricsAndIdentityFailures(t *testing.T) {
	for _, tc := range []struct{ mode, gate, status string }{
		{"healthy", "resource_metrics", "pass"},
		{"wrong-context", "context", "fail"},
		{"unready", "workloads", "fail"},
		{"unavailable-probe", "packet_path", "unavailable"},
		{"n2-down", "n2", "fail"},
		{"missing-metrics", "resource_metrics", "fail"},
		{"pod-changed", "stable_pod_identities", "fail"},
		{"new-owner", "ownership", "blocked"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "unused-parent", "ownership")
			t.Setenv("COREPULSE_LOCK_DIR", marker)
			c := &diagnosticClient{t: t, mode: tc.mode, labClient: labClient{probe: "2 packets transmitted, 2 received"}}
			defer func() {
				if c.owner != nil {
					if err := c.owner.Release(); err != nil {
						t.Fatal(err)
					}
				}
			}()
			r := Diagnose(context.Background(), c)
			if r.Ready != (tc.mode == "healthy") || diagnosticStatus(r, tc.gate) != tc.status || len(r.Checks) != 8 || r.FinishedAt.Before(r.StartedAt) {
				t.Fatalf("incorrect diagnostic: %+v", r)
			}
			if tc.mode == "wrong-context" && c.resourceCalls != 0 {
				t.Fatal("wrong context read lab resources")
			}
			if tc.mode == "n2-down" && (diagnosticStatus(r, "packet_path") != "pass" || diagnosticStatus(r, "n4") != "pass") {
				t.Fatal("N2 failure was confused with packet or PFCP health")
			}
			if tc.mode != "new-owner" {
				if _, err := os.Stat(filepath.Dir(marker)); !os.IsNotExist(err) {
					t.Fatalf("diagnostic created ownership output: %v", err)
				}
			}
		})
	}
}

func TestDiagnosticNeverTouchesClusterOrMarkerWhenLockedOrCancelled(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ownership")
	t.Setenv("COREPULSE_LOCK_DIR", marker)
	owner, err := AcquireOwnership("existing-baseline")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(marker, "owner.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := Diagnose(context.Background(), nil)
	if r.Ready || diagnosticStatus(r, "ownership") != "blocked" || diagnosticStatus(r, "context") != "skipped" {
		t.Fatalf("locked report %+v", r)
	}
	after, _ := os.ReadFile(filepath.Join(marker, "owner.json"))
	if string(after) != string(before) {
		t.Fatal("diagnosis changed owner's marker")
	}
	if err := owner.Release(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = Diagnose(ctx, nil)
	if r.Ready || diagnosticStatus(r, "context") != "unavailable" {
		t.Fatalf("cancelled report %+v", r)
	}
}
