package collection

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Owner is diagnostic evidence, not proof that a process is still alive. PIDs
// can be reused or belong to a different host/WSL namespace.
type Owner struct {
	Token     string    `json:"token"`
	Operation string    `json:"operation"`
	Host      string    `json:"host"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

type Ownership struct {
	dir   string
	token string
}

// AcquireOwnership serializes CLI/dashboard experiment sessions. All processes
// controlling one lab must use the same lock directory. It never steals or
// expires a marker, even if the diagnostic owner seems old or dead.
func AcquireOwnership(operation string) (*Ownership, error) {
	if strings.TrimSpace(operation) == "" {
		return nil, fmt.Errorf("collection operation must be named")
	}
	dir, err := ownershipDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0700); err != nil {
		return nil, fmt.Errorf("prepare collection ownership: %w", err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		if os.IsExist(err) {
			owner, readErr := readOwner(dir)
			if readErr == nil {
				return nil, fmt.Errorf("collection locked: %s on %s PID %d since %s; inspect %s; ownership is never automatically expired", owner.Operation, owner.Host, owner.PID, owner.StartedAt.Format(time.RFC3339), dir)
			}
			return nil, fmt.Errorf("collection locked at %s with unreadable or incomplete ownership; inspect before manual recovery: %w", dir, readErr)
		}
		return nil, fmt.Errorf("acquire collection ownership: %w", err)
	}
	// From this point a crash deliberately leaves a marker, including a crash
	// before owner.json is complete. A partial owner must also block collection.
	token, err := runID()
	if err != nil {
		return nil, fmt.Errorf("ownership marker retained at %s: %w", dir, err)
	}
	host, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("ownership marker retained at %s: %w", dir, err)
	}
	owner := Owner{Token: token, Operation: operation, Host: host, PID: os.Getpid(), StartedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(owner, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(dir, "owner.json"), append(data, '\n'), 0600)
	}
	if err != nil {
		return nil, fmt.Errorf("ownership marker retained at %s: %w", dir, err)
	}
	return &Ownership{dir: dir, token: token}, nil
}

func readOwner(dir string) (Owner, error) {
	var owner Owner
	for _, path := range []string{dir, filepath.Join(dir, "owner.json")} {
		info, err := os.Lstat(path)
		if err != nil {
			return owner, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return owner, fmt.Errorf("ownership path is a symbolic link")
		}
		if path == dir && !info.IsDir() || path != dir && (!info.Mode().IsRegular() || info.Size() > 16384) {
			return owner, fmt.Errorf("unexpected ownership file type or size")
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "owner.json"))
	if err != nil {
		return owner, err
	}
	if err := json.Unmarshal(data, &owner); err != nil {
		return owner, err
	}
	if owner.Token == "" || owner.Operation == "" || owner.Host == "" || owner.PID <= 0 || owner.StartedAt.IsZero() {
		return owner, fmt.Errorf("incomplete owner record")
	}
	return owner, nil
}

// Release removes only a matching ownership record and empty marker directory.
// Unexpected content, changed ownership or missing evidence fails closed.
func (o *Ownership) Release() error {
	owner, err := readOwner(o.dir)
	if err != nil {
		return fmt.Errorf("collection ownership retained at %s: %w", o.dir, err)
	}
	if owner.Token != o.token {
		return fmt.Errorf("collection ownership changed at %s; refusing release", o.dir)
	}
	entries, err := os.ReadDir(o.dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "owner.json" {
		return fmt.Errorf("unexpected collection ownership contents at %s; refusing release", o.dir)
	}
	if err := os.Remove(filepath.Join(o.dir, "owner.json")); err != nil {
		return err
	}
	return os.Remove(o.dir)
}

// WithOwnership includes CLI batch warmup, collection and dataset export. The
// callback is never invoked on contention; ordinary errors release ownership.
func WithOwnership(operation string, fn func() (int, error)) (code int, runErr error) {
	owner, err := AcquireOwnership(operation)
	if err != nil {
		return 1, err
	}
	defer func() {
		runErr = errors.Join(runErr, owner.Release())
		if runErr != nil && code == 0 {
			code = 1
		}
	}()
	return fn()
}
