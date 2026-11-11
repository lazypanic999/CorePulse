// Package neural runs the fixed offline synthetic experiment, separately from live collection.
package neural

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/lazypanic999/CorePulse/internal/collection"
	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/evaluation"
	"github.com/lazypanic999/CorePulse/internal/inference"
)

type Sample struct {
	Index  int    `json:"index"`
	RunID  string `json:"run_id"`
	Window string `json:"window"`
	Truth  string `json:"truth"`
}
type Model struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Seed     int64    `json:"seed"`
	MacroF1  float64  `json:"macro_f1"`
	Coverage float64  `json:"coverage"`
	Samples  []Sample `json:"samples"`
}
type Job struct {
	Available  bool            `json:"available"`
	ID         string          `json:"id,omitempty"`
	State      string          `json:"state"`
	Phase      string          `json:"phase"`
	Synthetic  bool            `json:"synthetic"`
	StartedAt  time.Time       `json:"started_at,omitempty"`
	FinishedAt time.Time       `json:"finished_at,omitempty"`
	Error      string          `json:"error,omitempty"`
	Models     []Model         `json:"models,omitempty"`
	Report     json.RawMessage `json:"report,omitempty"`
}
type loaded struct {
	model   *inference.Model
	samples []Sample
	inputs  []map[string]*float64
}
type Manager struct {
	mu                        sync.Mutex
	root, python, binary, dir string
	job                       Job
	models                    map[string]loaded
	run                       func(context.Context, string) error
}

func New(root, python, binary string) (*Manager, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(python) || !filepath.IsAbs(binary) {
		return nil, fmt.Errorf("neural Python and CLI executable paths must be absolute")
	}
	for _, p := range []string{python, binary, filepath.Join(root, "go.mod"), filepath.Join(root, "ml", "pyproject.toml"), filepath.Join(root, "scripts", "neural-ui-worker.py")} {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("neural workspace requires regular file %s", p)
		}
	}
	m := &Manager{root: root, python: python, binary: binary, job: Job{Available: true, State: "idle", Phase: "Ready to train the fixed synthetic cohort", Synthetic: true}}
	m.run = m.execute
	return m, nil
}
func (m *Manager) Snapshot() Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.job
	if j.State == "running" {
		var p struct {
			Phase string `json:"phase"`
		}
		if b, e := os.ReadFile(filepath.Join(m.dir, "progress.json")); e == nil && len(b) < 4096 && json.Unmarshal(b, &p) == nil {
			j.Phase = p.Phase
		}
	}
	j.Report = append(json.RawMessage(nil), j.Report...)
	j.Models = append([]Model(nil), j.Models...)
	for i := range j.Models {
		j.Models[i].Samples = append([]Sample(nil), j.Models[i].Samples...)
	}
	return j
}
func (m *Manager) Start() (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job.State == "running" {
		return Job{}, fmt.Errorf("a neural experiment is already running")
	}
	parent := filepath.Join(m.root, "reports", "neural-ui")
	if err := os.MkdirAll(parent, 0755); err != nil {
		return Job{}, err
	}
	dir, err := os.MkdirTemp(parent, "job-")
	if err != nil {
		return Job{}, err
	}
	m.dir = dir
	m.models = nil
	m.job = Job{Available: true, ID: filepath.Base(dir), State: "running", Phase: "Preparing the synthetic cohort", Synthetic: true, StartedAt: time.Now().UTC()}
	go m.work(dir)
	return m.job, nil
}
func (m *Manager) execute(ctx context.Context, dir string) error {
	log, err := os.OpenFile(filepath.Join(dir, "training.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.CommandContext(ctx, m.python, "-u", filepath.Join(m.root, "scripts", "neural-ui-worker.py"), "--binary", m.binary, "--output-dir", dir)
	cmd.Dir = m.root
	cmd.Env = append(os.Environ(), "CUDA_VISIBLE_DEVICES=", "OMP_NUM_THREADS=1", "MKL_NUM_THREADS=1", "KUBECONFIG="+os.DevNull, "PYTHONPATH="+filepath.Join(m.root, "ml", "src"))
	cmd.Stdout = log
	cmd.Stderr = log
	return cmd.Run()
}
func (m *Manager) work(dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var report json.RawMessage
	var models map[string]loaded
	var summaries []Model
	_, err := collection.WithOwnership("offline-neural-training", func() (int, error) {
		if err := m.run(ctx, dir); err != nil {
			return 1, err
		}
		var err error
		report, models, summaries, err = loadOutputs(dir)
		return 0, err
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	m.job.FinishedAt = time.Now().UTC()
	if err != nil {
		if log, logErr := os.OpenFile(filepath.Join(dir, "training.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600); logErr == nil {
			fmt.Fprintln(log, "Neural job refused:", err)
			log.Close()
		}
		m.job.State = "failed"
		m.job.Phase = "Training or artifact validation failed"
		m.job.Error = "The job was not published. Inspect its local training.log."
		return
	}
	m.models = models
	m.job.State = "complete"
	m.job.Phase = "Training, export and Go parity validation complete"
	m.job.Models = summaries
	m.job.Report = report
}
func readBounded(path string, limit int64) ([]byte, error) {
	info, e := os.Stat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("unsupported output file")
	}
	return os.ReadFile(path)
}
func hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func loadOutputs(dir string) (json.RawMessage, map[string]loaded, []Model, error) {
	raw, err := readBounded(filepath.Join(dir, "evaluation.json"), 8*1024*1024)
	if err != nil {
		return nil, nil, nil, err
	}
	var report evaluation.Report
	if err = json.Unmarshal(raw, &report); err != nil {
		return nil, nil, nil, err
	}
	if err = evaluation.ValidateSnapshot(report); err != nil {
		return nil, nil, nil, err
	}
	if !report.Synthetic || report.SchemaVersion != 3 {
		return nil, nil, nil, fmt.Errorf("neural UI accepts synthetic schema-three reports only")
	}
	if _, err := readBounded(filepath.Join(dir, "windows.csv"), 8*1024*1024); err != nil {
		return nil, nil, nil, err
	}
	windows, err := dataset.ReadWindows(filepath.Join(dir, "windows.csv"))
	if err != nil {
		return nil, nil, nil, err
	}
	if windows.SHA256 != report.WindowsSHA256 {
		return nil, nil, nil, fmt.Errorf("window source hash mismatch")
	}
	split, err := readBounded(filepath.Join(dir, "split.json"), 1024*1024)
	if err != nil {
		return nil, nil, nil, err
	}
	parityRaw, err := readBounded(filepath.Join(dir, "parity.json"), 2*1024*1024)
	if err != nil {
		return nil, nil, nil, err
	}
	var references cpuReferences
	if err := json.Unmarshal(parityRaw, &references); err != nil {
		return nil, nil, nil, err
	}
	if references.WindowsSHA256 != windows.SHA256 || references.SplitSHA256 != hash(split) || len(references.Models) != 4 {
		return nil, nil, nil, fmt.Errorf("CPU reference provenance mismatch")
	}
	var experiment struct {
		Artifacts map[string]struct {
			Filename string `json:"filename"`
			SHA256   string `json:"sha256"`
		} `json:"artifacts"`
	}
	if err = json.Unmarshal(report.Experiment, &experiment); err != nil {
		return nil, nil, nil, err
	}
	names := []string{"autoencoder_seed_7", "autoencoder_seed_23", "mlp_seed_7", "mlp_seed_23"}
	if len(experiment.Artifacts) != len(names) {
		return nil, nil, nil, fmt.Errorf("all four fixed neural methods must be exported")
	}
	models := map[string]loaded{}
	summaries := []Model{}
	for _, name := range names {
		artifact, ok := experiment.Artifacts[name]
		if !ok || artifact.Filename != name+".json" {
			return nil, nil, nil, fmt.Errorf("unexpected artifact descriptor")
		}
		b, err := readBounded(filepath.Join(dir, "models", artifact.Filename), inference.MaxArtifactBytes)
		if err != nil {
			return nil, nil, nil, err
		}
		if hash(b) != artifact.SHA256 {
			return nil, nil, nil, fmt.Errorf("artifact hash mismatch")
		}
		model, err := inference.Load(bytes.NewReader(b))
		if err != nil {
			return nil, nil, nil, err
		}
		meta := model.Metadata()
		reference, exists := references.Models[name]
		if !exists || reference.ArtifactSHA256 != artifact.SHA256 {
			return nil, nil, nil, fmt.Errorf("CPU reference artifact hash mismatch")
		}
		var definition struct {
			Preprocessing struct {
				Medians []float64 `json:"medians"`
				Means   []float64 `json:"means"`
				Scales  []float64 `json:"scales"`
			} `json:"preprocessing"`
		}
		if err := json.Unmarshal(b, &definition); err != nil {
			return nil, nil, nil, err
		}
		method, ok := report.Models[name]
		if !ok || !meta.Synthetic || meta.WindowsSHA256 != windows.SHA256 || meta.SplitSHA256 != hash(split) || meta.Task != method.Task || method.Seed == nil || meta.Seed != *method.Seed || method.Family != meta.Kind {
			return nil, nil, nil, fmt.Errorf("artifact provenance differs from experiment")
		}
		item := loaded{model: model}
		if len(reference.Cases) != len(method.Predictions) {
			return nil, nil, nil, fmt.Errorf("CPU reference held-out support mismatch")
		}
		summary := Model{Name: name, Kind: meta.Kind, Seed: meta.Seed, MacroF1: method.WindowMetrics.MacroF1, Coverage: method.WindowMetrics.Coverage}
		for index, p := range method.Predictions {
			if !slices.Contains(report.Split.TestRuns, p.RunID) {
				return nil, nil, nil, fmt.Errorf("prediction is not held out")
			}
			var row *dataset.WindowRow
			for i := range windows.Rows {
				r := &windows.Rows[i]
				if r.RunID == p.RunID && r.WindowStart.Equal(p.WindowStart) {
					row = r
					break
				}
			}
			if row == nil {
				return nil, nil, nil, fmt.Errorf("held-out window not present")
			}
			truth := row.Label
			if truth != "normal" {
				truth += ":" + row.Target
			}
			if meta.Task == "normal_vs_fault_interval" && truth != "normal" {
				truth = "fault"
			}
			if p.Truth != truth {
				return nil, nil, nil, fmt.Errorf("held-out truth differs from source row")
			}
			input := map[string]*float64{}
			for _, feature := range meta.FeatureNames {
				if v, ok := row.Features[feature]; ok {
					value := v
					input[feature] = &value
				}
			}
			got, err := model.Predict(input)
			if err != nil {
				return nil, nil, nil, err
			}
			if p.Score == nil {
				return nil, nil, nil, fmt.Errorf("missing CPU PyTorch score")
			}
			cpu := reference.Cases[index]
			stamp, stampErr := time.Parse(time.RFC3339Nano, cpu.Window)
			if cpu.RunID != p.RunID || stampErr != nil || !stamp.Equal(p.WindowStart) || cpu.Score != *p.Score || cpu.Predicted != p.Predicted || len(cpu.Inputs) != len(meta.FeatureNames) {
				return nil, nil, nil, fmt.Errorf("CPU reference differs from original held-out report")
			}
			for i, feature := range meta.FeatureNames {
				value := definition.Preprocessing.Medians[i]
				if observed := input[feature]; observed != nil {
					value = *observed
				}
				expected := float64(float32((value - definition.Preprocessing.Means[i]) / definition.Preprocessing.Scales[i]))
				if cpu.Inputs[i] != expected {
					return nil, nil, nil, fmt.Errorf("CPU reference input differs from source preprocessing")
				}
			}
			if err := verifyNumerics(meta.Kind, got, cpu); err != nil {
				return nil, nil, nil, fmt.Errorf("%s/%s: %w", name, p.RunID, err)
			}
			sample := Sample{len(item.samples), row.RunID, row.WindowStart.Format(time.RFC3339Nano), p.Truth}
			item.samples = append(item.samples, sample)
			item.inputs = append(item.inputs, input)
		}
		if len(item.samples) == 0 {
			return nil, nil, nil, fmt.Errorf("empty held-out partition")
		}
		summary.Samples = append([]Sample(nil), item.samples...)
		models[name] = item
		summaries = append(summaries, summary)
	}
	return raw, models, summaries, nil
}
func (m *Manager) Predict(job, name string, index int) (inference.Result, Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job.State != "complete" || m.job.ID != job {
		return inference.Result{}, Sample{}, fmt.Errorf("this completed neural job is not available")
	}
	item, ok := m.models[name]
	if !ok || index < 0 || index >= len(item.samples) {
		return inference.Result{}, Sample{}, fmt.Errorf("unknown model or held-out sample")
	}
	result, err := item.model.Predict(item.inputs[index])
	return result, item.samples[index], err
}
