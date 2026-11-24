//go:build unix

package collection

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func holdLock(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "lock")
	t.Setenv("COREPULSE_LOCK_DIR", dir)
	return dir
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestHoldRefusesBeforeTheHelperStartsWhileACollectorOwns(t *testing.T) {
	holdLock(t)
	owner, err := AcquireOwnership("baseline")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Release()
	ran := filepath.Join(t.TempDir(), "ran")
	code, err := Hold("recover-service", []string{"sh", "-c", "touch " + ran}, io.Discard, io.Discard)
	if code != 1 || err == nil || !strings.Contains(err.Error(), "baseline") || exists(ran) {
		t.Fatalf("code=%d err=%v ran=%v", code, err, exists(ran))
	}
}

func TestCollectorIsRefusedWhileAHeldHelperRuns(t *testing.T) {
	dir := holdLock(t)
	work := t.TempDir()
	started, proceed, token := filepath.Join(work, "started"), filepath.Join(work, "go"), filepath.Join(work, "token")
	script := `printf %s "$COREPULSE_OWNERSHIP_TOKEN" > ` + token + `; touch ` + started + `; while [ ! -f ` + proceed + ` ]; do sleep 0.02; done`
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := Hold("recover-service", []string{"sh", "-c", script}, io.Discard, io.Discard)
		done <- result{code, err}
	}()
	waitForFile(t, started)
	code, err := WithOwnership("dashboard-fault", func() (int, error) {
		t.Error("collection started while a recovery helper held ownership")
		return 0, nil
	})
	if code != 1 || err == nil || !strings.Contains(err.Error(), "recover-service") {
		t.Fatalf("collector not refused: %d %v", code, err)
	}
	data, err := os.ReadFile(token)
	if err != nil {
		t.Fatal(err)
	}
	if owner, err := CheckHeld(string(data)); err != nil || owner.Operation != "recover-service" {
		t.Fatalf("helper token not confirmed: %+v %v", owner, err)
	}
	if _, err := CheckHeld("copied-or-stale-token"); err == nil {
		t.Fatal("foreign token confirmed")
	}
	if err := os.WriteFile(proceed, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.code != 0 || r.err != nil {
		t.Fatalf("hold failed: %+v", r)
	}
	if exists(dir) {
		t.Fatal("ownership kept after the helper finished")
	}
	if _, err := CheckHeld(string(data)); err == nil {
		t.Fatal("released token still confirmed")
	}
}

func TestHoldReturnsHelperFailureAndReleases(t *testing.T) {
	dir := holdLock(t)
	code, err := Hold("recover-service", []string{"sh", "-c", "exit 2"}, io.Discard, io.Discard)
	if code != 2 || err != nil || exists(dir) {
		t.Fatalf("code=%d err=%v marker=%v", code, err, exists(dir))
	}
	code, err = Hold("recover-service", []string{filepath.Join(t.TempDir(), "missing")}, io.Discard, io.Discard)
	if code != 1 || err == nil || exists(dir) {
		t.Fatalf("start failure: code=%d err=%v marker=%v", code, err, exists(dir))
	}
}

func TestHoldRejectsInvalidUse(t *testing.T) {
	dir := holdLock(t)
	for name, call := range map[string]func() (int, error){
		"no command":    func() (int, error) { return Hold("recover-service", nil, io.Discard, io.Discard) },
		"bad operation": func() (int, error) { return Hold("Recover Service", []string{"true"}, io.Discard, io.Discard) },
		"nested under hold": func() (int, error) {
			t.Setenv(HeldTokenEnv, "outer")
			return Hold("recover-service", []string{"true"}, io.Discard, io.Discard)
		},
	} {
		if code, err := call(); code != 1 || err == nil || exists(dir) {
			t.Fatalf("%s: code=%d err=%v marker=%v", name, code, err, exists(dir))
		}
	}
	if _, err := CheckHeld(""); err == nil {
		t.Fatal("empty token confirmed")
	}
}

// TestHoldHelperProcess is the separate CorePulse process that holds ownership
// in the signal tests below.
func TestHoldHelperProcess(t *testing.T) {
	script := os.Getenv("COREPULSE_HOLD_TEST_SCRIPT")
	if script == "" {
		t.Skip("subprocess helper")
	}
	code, err := Hold("recover-service", []string{"sh", "-c", script}, os.Stdout, os.Stderr)
	if err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
	}
	os.Exit(code)
}

func startHold(t *testing.T, script string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHoldHelperProcess$")
	cmd.Env = append(os.Environ(), "COREPULSE_HOLD_TEST_SCRIPT="+script)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return cmd
}

// An interrupt reaches the helper's restore trap; ownership stays held until
// that restoration has finished, then it is released.
func TestInterruptKeepsOwnershipUntilTheHelperFinishesRestoring(t *testing.T) {
	dir := holdLock(t)
	work := t.TempDir()
	started, restoring, restored := filepath.Join(work, "started"), filepath.Join(work, "restoring"), filepath.Join(work, "restored")
	script := `trap 'touch ` + restoring + `; sleep 0.6; touch ` + restored + `; exit 130' INT; touch ` + started + `; while :; do sleep 0.02; done`
	cmd := startHold(t, script)
	waitForFile(t, started)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, restoring)
	if exists(restored) || !exists(dir) {
		t.Fatal("ownership released while the helper was still restoring")
	}
	if _, err := AcquireOwnership("dashboard-fault"); err == nil {
		t.Fatal("collection acquired ownership during restoration")
	}
	err := cmd.Wait()
	if !exists(restored) {
		t.Fatal("helper restoration did not complete")
	}
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 130 {
		t.Fatalf("interrupted helper exit not reported: %v", err)
	}
	if exists(dir) {
		t.Fatal("ownership not released after restoration finished")
	}
}

// Killing the holding process cannot run its release; the marker and owner
// evidence remain while the orphaned helper may still be running.
func TestKilledHoldRetainsInspectableOwnership(t *testing.T) {
	dir := holdLock(t)
	work := t.TempDir()
	started := filepath.Join(work, "started")
	cmd := startHold(t, `echo $$ > `+started+`.pid; touch `+started+`; sleep 30`)
	waitForFile(t, started)
	pid, err := os.ReadFile(started + ".pid")
	if err != nil {
		t.Fatal(err)
	}
	helper, _ := strconv.Atoi(strings.TrimSpace(string(pid)))
	t.Cleanup(func() { _ = syscall.Kill(-helper, syscall.SIGKILL) })
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	owner, err := readOwner(dir)
	if err != nil || owner.Operation != "recover-service" || owner.PID != cmd.Process.Pid {
		t.Fatalf("owner evidence lost: %+v %v", owner, err)
	}
	if _, err := AcquireOwnership("baseline"); err == nil {
		t.Fatal("forced termination released ownership")
	}
}
