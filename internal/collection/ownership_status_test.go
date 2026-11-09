package collection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnershipInspectionIsReadOnlyAndOmitsToken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent", "marker")
	t.Setenv("COREPULSE_LOCK_DIR", dir)
	state := InspectOwnership()
	if !state.Available || state.Locked || state.Owner != nil {
		t.Fatalf("unexpected free state: %+v", state)
	}
	if _, err := os.Stat(filepath.Dir(dir)); !os.IsNotExist(err) {
		t.Fatal("inspection created a directory")
	}
	owner, err := AcquireOwnership("baseline")
	if err != nil {
		t.Fatal(err)
	}
	state = InspectOwnership()
	if !state.Available || !state.Locked || state.InspectionRequired || state.Owner == nil || state.Owner.Operation != "baseline" || state.Owner.PID != os.Getpid() {
		t.Fatalf("missing owner summary: %+v", state)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), owner.token) || strings.Contains(string(encoded), "\"token\"") || strings.Contains(string(encoded), dir) {
		t.Fatalf("ownership secret/path leaked: %s", encoded)
	}
	if err := owner.Release(); err != nil {
		t.Fatal(err)
	}
	if state := InspectOwnership(); !state.Available || state.Locked {
		t.Fatalf("release not reflected: %+v", state)
	}
}

func TestIncompleteOwnershipStatusStaysLocked(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "marker")
	t.Setenv("COREPULSE_LOCK_DIR", dir)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	state := InspectOwnership()
	if !state.Available || !state.Locked || !state.InspectionRequired || state.Owner != nil || state.Message == "" {
		t.Fatalf("incomplete ownership appeared free: %+v", state)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("inspection changed incomplete marker")
	}
}

func TestRelativeOwnershipOverrideIsRejected(t *testing.T) {
	t.Setenv("COREPULSE_LOCK_DIR", "relative-marker")
	if _, err := AcquireOwnership("baseline"); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative override could split ownership across working trees: %v", err)
	}
	if state := InspectOwnership(); state.Available || !state.Locked || state.Message == "" {
		t.Fatalf("invalid configuration appeared free: %+v", state)
	}
}
