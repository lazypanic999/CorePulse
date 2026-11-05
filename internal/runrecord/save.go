// Package runrecord persists complete JSON snapshots without exposing partial writes.
package runrecord

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func Save(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".run-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(0644); err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// A concurrent Windows reader can temporarily prevent replacement. Retry
	// without removing the previous snapshot or exposing the temporary file.
	return retryWindowsSharing(func() error { return os.Rename(f.Name(), path) })
}

// Read tolerates Windows sharing locks during an atomic replacement.
func Read(path string) (data []byte, err error) {
	err = retryWindowsSharing(func() error {
		data, err = os.ReadFile(path)
		return err
	})
	return data, err
}

func retryWindowsSharing(operation func() error) error {
	for attempt := 0; ; attempt++ {
		err := operation()
		var code syscall.Errno
		if err == nil || runtime.GOOS != "windows" || attempt == 25 || !errors.As(err, &code) || (code != 5 && code != 32 && code != 33) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
