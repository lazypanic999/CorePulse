package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lazypanic999/CorePulse/internal/collection"
)

func TestCLICollectionRefusesBeforeClusterOrOutputAccess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COREPULSE_LOCK_DIR", filepath.Join(dir, "shared-lock"))
	owner, err := collection.AcquireOwnership("another-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := owner.Release(); err != nil {
			t.Fatal(err)
		}
	}()
	runs := filepath.Join(dir, "different-run-directory")
	raw := filepath.Join(dir, "raw.jsonl")
	for _, args := range [][]string{
		{"baseline", "--runs", "1", "--samples", "2", "--metrics-wait", "1m", "--runs-dir", runs, "--telemetry", raw},
		{"pod-crash", "--target", "amf", "--apply", "--runs-dir", runs},
	} {
		code, err := run(args)
		if code != 1 || err == nil || !strings.Contains(err.Error(), "another-dashboard") {
			t.Fatalf("CLI bypassed collection ownership: %v => %d %v", args, code, err)
		}
	}
	for _, path := range []string{runs, raw} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("contending collection wrote output: %s %v", path, err)
		}
	}
	if code, err := run([]string{"pod-crash", "--target", "amf"}); code != 0 || err != nil {
		t.Fatalf("read-only dry run was blocked: %d %v", code, err)
	}
}

func TestInvalidSessionFlagsRefuseBeforeCluster(t *testing.T) {
	for _, args := range [][]string{{"baseline", "--session-id", "bad/session"}, {"pod-crash", "--target", "amf", "--apply", "--session-id", "bad session"}, {"serve", "--session-id", "bad session"}} {
		code, err := run(args)
		if code != 1 || err == nil || !strings.Contains(err.Error(), "session-id") {
			t.Fatalf("invalid declaration was not rejected first: %v => %d %v", args, code, err)
		}
	}
}
