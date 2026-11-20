package main

import (
	"path/filepath"
	"testing"

	"github.com/lazypanic999/CorePulse/internal/dataset"
)

func TestSequencesCLIOnlyReadsInventedOfflineExample(t *testing.T) {
	out := filepath.Join(t.TempDir(), "sequences.csv")
	args := []string{"sequences", "--telemetry", filepath.Join("..", "..", "examples", "offline", "telemetry.jsonl"), "--runs-dir", filepath.Join("..", "..", "examples", "offline", "run-records"), "--output", out, "--step", "5s"}
	code, err := run(args)
	if err != nil || code != 0 {
		t.Fatalf("offline sequence command: %d %v", code, err)
	}
	data, err := dataset.ReadSequences(out)
	if err != nil || len(data.Rows) != 108 {
		t.Fatalf("unexpected invented grid: %d %v", len(data.Rows), err)
	}
	for _, tail := range [][]string{{"unexpected"}, {"--step", "0s"}, {"--unknown"}} {
		if code, err := run(append(append([]string(nil), args...), tail...)); err == nil || code == 0 {
			t.Fatal("invalid sequence CLI accepted")
		}
	}
}
