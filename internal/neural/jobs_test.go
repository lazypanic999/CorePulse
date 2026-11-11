package neural

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/evaluation"
)

func TestManagerRefusesConcurrentTrainingAndNeverPublishesFailedScores(t *testing.T) {
	t.Setenv("COREPULSE_LOCK_DIR", filepath.Join(t.TempDir(), "ownership"))
	release := make(chan struct{})
	m := &Manager{root: t.TempDir(), job: Job{State: "idle", Available: true}, run: func(context.Context, string) error { <-release; return fmt.Errorf("fixture worker failure") }}
	first, err := m.Start()
	if err != nil || first.State != "running" || first.ID == "" {
		t.Fatalf("start: %+v %v", first, err)
	}
	if _, err = m.Start(); err == nil {
		t.Fatal("concurrent job accepted")
	}
	if _, _, err = m.Predict(first.ID, "mlp_seed_7", 0); err == nil {
		t.Fatal("unfinished job served inference")
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for m.Snapshot().State == "running" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := m.Snapshot()
	if got.State != "failed" || len(got.Report) > 0 || len(got.Models) > 0 || got.Error == "" {
		t.Fatalf("failed job published results: %+v", got)
	}
	if _, _, err = m.Predict(first.ID, "mlp_seed_7", 0); err == nil {
		t.Fatal("failed job served inference")
	}
}
func TestManagerRequiresExplicitExecutableConfiguration(t *testing.T) {
	if _, err := New(t.TempDir(), "python", "corepulse"); err == nil {
		t.Fatal("relative executable paths accepted")
	}
	if _, err := New(t.TempDir(), filepath.Join(t.TempDir(), "missing-python"), filepath.Join(t.TempDir(), "missing-cli")); err == nil {
		t.Fatal("missing executable accepted")
	}
}
func referenceJob(t *testing.T) (string, evaluation.Report) {
	t.Helper()
	dir := t.TempDir()
	windows := filepath.Join(dir, "windows.csv")
	split := filepath.Join(dir, "split.json")
	examples := filepath.Join("..", "..", "examples", "offline")
	if _, err := dataset.BuildWindows(filepath.Join(examples, "telemetry.jsonl"), filepath.Join(examples, "run-records"), windows, dataset.WindowOptions{Duration: 30 * time.Second, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := dataset.Split(windows, split, dataset.SplitOptions{GroupBy: "session", Seed: 42, TestFraction: .25, Embargo: 2 * time.Minute, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	report, err := evaluation.Evaluate(windows, split, filepath.Join(dir, "evaluation.json"), evaluation.Options{Threshold: 3, Synthetic: true})
	if err != nil {
		t.Fatal(err)
	}
	return dir, report
}
func saveReport(t *testing.T, dir string, r evaluation.Report) {
	t.Helper()
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "evaluation.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestPublishedOutputsRequireSyntheticSchemaAndUnchangedSource(t *testing.T) {
	t.Run("legacy", func(t *testing.T) {
		dir, _ := referenceJob(t)
		if _, _, _, e := loadOutputs(dir); e == nil {
			t.Fatal("legacy reference report published as neural training")
		}
	})
	t.Run("real-data-flag", func(t *testing.T) {
		dir, r := referenceJob(t)
		r.SchemaVersion = 3
		r.Synthetic = false
		saveReport(t, dir, r)
		if _, _, _, e := loadOutputs(dir); e == nil {
			t.Fatal("real-data report accepted by synthetic trainer")
		}
	})
	t.Run("source-hash", func(t *testing.T) {
		dir, r := referenceJob(t)
		r.SchemaVersion = 3
		saveReport(t, dir, r)
		f, e := os.OpenFile(filepath.Join(dir, "windows.csv"), os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			t.Fatal(e)
		}
		f.WriteString("\n")
		f.Close()
		if _, _, _, e = loadOutputs(dir); e == nil || !strings.Contains(e.Error(), "hash") {
			t.Fatalf("changed source not refused: %v", e)
		}
	})
	t.Run("all-artifacts-required", func(t *testing.T) {
		dir, r := referenceJob(t)
		r.SchemaVersion = 3
		r.Experiment = json.RawMessage(`{"artifacts":{}}`)
		saveReport(t, dir, r)
		if _, _, _, e := loadOutputs(dir); e == nil {
			t.Fatal("missing fixed-seed exports accepted")
		}
	})
}
