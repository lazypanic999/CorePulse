package collection

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type OwnerSummary struct {
	Operation string    `json:"operation"`
	Host      string    `json:"host"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// OwnershipState describes a cooperative marker, not process liveness. A killed
// owner's marker remains locked. No token or local filesystem path is exposed.
type OwnershipState struct {
	Available          bool          `json:"available"`
	Locked             bool          `json:"locked"`
	InspectionRequired bool          `json:"inspection_required"`
	Owner              *OwnerSummary `json:"owner,omitempty"`
	Message            string        `json:"message,omitempty"`
}

func ownershipDir() (string, error) {
	dir := os.Getenv("COREPULSE_LOCK_DIR")
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("resolve collection ownership directory: %w", err)
		}
		dir = filepath.Join(cache, "CorePulse", "locks", "open5gs-lab-open5gs")
	}
	// A relative override resolves differently for independent working trees,
	// defeating coordination even when every caller uses the same env value.
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("collection ownership requires an absolute directory; set COREPULSE_LOCK_DIR to one shared absolute path")
	}
	return filepath.Clean(dir), nil
}

// InspectOwnership reads only. It never creates, releases or expires a marker.
func InspectOwnership() OwnershipState {
	state := OwnershipState{Locked: true, InspectionRequired: true}
	dir, err := ownershipDir()
	if err != nil {
		state.Message = err.Error()
		return state
	}
	_, err = os.Lstat(dir)
	if os.IsNotExist(err) {
		return OwnershipState{Available: true}
	}
	if err != nil {
		state.Message = "Collection ownership could not be read. Inspect the shared marker before starting another run."
		return state
	}
	state.Available = true
	owner, err := readOwner(dir)
	if err != nil {
		state.Message = "Collection ownership is incomplete or unreadable. Inspect it before manual recovery."
		return state
	}
	state.InspectionRequired = false
	state.Owner = &OwnerSummary{Operation: owner.Operation, Host: owner.Host, PID: owner.PID, StartedAt: owner.StartedAt}
	return state
}
