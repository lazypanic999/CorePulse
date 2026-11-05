package runrecord

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestReplacementAlwaysExposesCompleteJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runs", "run.json")
	if err := Save(path, map[string]int{"sample": 0}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := Read(path)
			if err != nil || !json.Valid(data) {
				t.Errorf("reader saw incomplete snapshot: %s, %v", data, err)
				return
			}
		}
	}()
	for i := 1; i <= 20; i++ {
		if err := Save(path, map[string]int{"sample": i}); err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	wg.Wait()
	before, _ := os.ReadFile(path)
	if err := Save(path, math.NaN()); err == nil {
		t.Fatal("expected encoding failure")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed write replaced the last good snapshot")
	}
}
