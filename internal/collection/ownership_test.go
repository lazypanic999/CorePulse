package collection

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOwnershipSubprocessContention(t *testing.T) {
	if mode := os.Getenv("COREPULSE_OWNER_TEST_HELPER"); mode != "" {
		owner, err := AcquireOwnership("child-baseline")
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("owned")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		if err := owner.Release(); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, mode := range []string{"release", "kill"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "lock")
			t.Setenv("COREPULSE_LOCK_DIR", dir)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnershipSubprocessContention$")
			cmd.Env = append(os.Environ(), "COREPULSE_OWNER_TEST_HELPER="+mode)
			cmd.Stderr = os.Stderr
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			reader := bufio.NewReader(out)
			if line, err := reader.ReadString('\n'); err != nil || strings.TrimSpace(line) != "owned" {
				t.Fatalf("child failed to acquire: %q %v", line, err)
			}
			owner, err := readOwner(dir)
			if err != nil || owner.PID != cmd.Process.Pid || owner.Operation != "child-baseline" {
				t.Fatalf("missing diagnostic owner: %+v %v", owner, err)
			}
			code, err := WithOwnership("dashboard-fault", func() (int, error) {
				t.Fatal("contending caller started preflight/collection")
				return 0, nil
			})
			if code != 1 || err == nil || !strings.Contains(err.Error(), "child-baseline") {
				t.Fatalf("missing contention details: %d %v", code, err)
			}
			if mode == "kill" {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				if _, err := AcquireOwnership("next"); err == nil {
					t.Fatal("forced termination silently stole ownership")
				}
				retained, err := readOwner(dir)
				if err != nil || retained.Token != owner.Token {
					t.Fatalf("forced termination lost owner evidence: %+v %v", retained, err)
				}
			} else {
				_, _ = fmt.Fprintln(in, "release")
				_ = in.Close()
				if err := cmd.Wait(); err != nil {
					t.Fatal(err)
				}
				next, err := AcquireOwnership("next")
				if err != nil {
					t.Fatal(err)
				}
				if err := next.Release(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestOwnershipOrdinaryErrorAndCancellationRelease(t *testing.T) {
	for _, cause := range []error{nil, errors.New("preflight failed"), context.Canceled} {
		dir := filepath.Join(t.TempDir(), "lock")
		t.Setenv("COREPULSE_LOCK_DIR", dir)
		code, err := WithOwnership("baseline", func() (int, error) { return 130, cause })
		if code != 130 || !errors.Is(err, cause) {
			t.Fatalf("return changed: %d %v", code, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("ordinary return left a lock: %v", err)
		}
	}
}

func TestOwnershipNeverRemovesChangedOrUnexpectedEvidence(t *testing.T) {
	for _, changed := range []bool{false, true} {
		dir := filepath.Join(t.TempDir(), "lock")
		t.Setenv("COREPULSE_LOCK_DIR", dir)
		owner, err := AcquireOwnership("baseline")
		if err != nil {
			t.Fatal(err)
		}
		if changed {
			record, err := readOwner(dir)
			if err != nil {
				t.Fatal(err)
			}
			record.Token = "different-owner"
			data, _ := json.Marshal(record)
			if err := os.WriteFile(filepath.Join(dir, "owner.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(filepath.Join(dir, "unexpected"), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := owner.Release(); err == nil {
			t.Fatal("release removed changed/unexpected evidence")
		}
		if _, err := readOwner(dir); err != nil {
			t.Fatalf("release destroyed owner record: %v", err)
		}
	}
}

func TestIncompleteOwnershipNeverExpires(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lock")
	t.Setenv("COREPULSE_LOCK_DIR", dir)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireOwnership("baseline"); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("incomplete marker did not fail closed: %v", err)
	}
}
