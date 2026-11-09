// Package evaluation provides deliberately simple offline baselines with strict
// whole-run holdouts. It does not deploy a model or infer live service health.
package evaluation

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

type Options struct {
	Threshold   float64
	AllowLegacy bool
}
type Prediction struct {
	RunID       string    `json:"run_id"`
	WindowStart time.Time `json:"window_start"`
	Truth       string    `json:"truth"`
	Predicted   string    `json:"predicted"`
	Score       *float64  `json:"score,omitempty"`
}
type ModelReport struct {
	Task          string               `json:"task"`
	WindowMetrics Metrics              `json:"window_metrics"`
	RunMetrics    Metrics              `json:"run_metrics"`
	Predictions   []Prediction         `json:"predictions"`
	Transformer   *Transformer         `json:"transformer,omitempty"`
	Centroids     map[string][]float64 `json:"centroids,omitempty"`
}
type Report struct {
	PurgedWindows         int                    `json:"purged_windows"`
	SchemaVersion         int                    `json:"schema_version"`
	WindowsSHA256         string                 `json:"windows_sha256"`
	Split                 dataset.SplitManifest  `json:"split"`
	TrainWindows          int                    `json:"train_windows"`
	TestWindows           int                    `json:"test_windows"`
	NormalTrainingWindows int                    `json:"normal_training_windows"`
	ZScoreThreshold       float64                `json:"zscore_threshold"`
	Models                map[string]ModelReport `json:"models"`
	Limitations           []string               `json:"limitations"`
}

func Evaluate(windowsPath, splitPath, outputPath string, options Options) (Report, error) {
	r := Report{SchemaVersion: 1, ZScoreThreshold: options.Threshold, Models: map[string]ModelReport{}, Limitations: []string{
		"Ground truth labels describe controlled experiment intervals, not continuous service degradation.",
		"Single-node, single-UE local lab; held-out runs from this environment do not establish generalization to another network.",
		"Resource sample timestamps are capture times; original metric timestamps and averaging windows are not yet retained. Nearby runs may share underlying CPU measurement intervals. A capture-time embargo reduces proximity, but cannot certify measurement-time independence.",
		"Training uses equally weighted windows; long runs contribute more windows. Both window and majority-vote run metrics are reported.",
		"Sparse and partial windows remain included; missing resource values use training-only medians and missing-ratio features.",
		"Probe rule abstains when evidence is absent. Protocol event counts are excluded from fitted models because their availability depends on post-run log collection.",
		"Hyperparameters and split seed must be selected before examining held-out scores. These are offline baselines, not live dashboard predictions.",
	}}
	if options.Threshold <= 0 || math.IsNaN(options.Threshold) || math.IsInf(options.Threshold, 0) {
		return r, fmt.Errorf("z-score threshold must be finite and positive")
	}
	for _, input := range []string{windowsPath, splitPath} {
		if err := separateOutput(input, outputPath); err != nil {
			return r, err
		}
	}
	data, err := dataset.ReadWindows(windowsPath)
	if err != nil {
		return r, err
	}
	r.WindowsSHA256 = data.SHA256
	bytes, err := runrecord.Read(splitPath)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(bytes, &r.Split); err != nil {
		return r, err
	}
	train, test, labels, err := validateSplit(data, r.Split, options.AllowLegacy)
	if err != nil {
		return r, err
	}
	r.TrainWindows, r.TestWindows = len(train), len(test)
	r.PurgedWindows = len(data.Rows) - len(train) - len(test)
	normal := []dataset.WindowRow{}
	for _, row := range train {
		if row.Label == "normal" {
			normal = append(normal, row)
		}
	}
	r.NormalTrainingWindows = len(normal)
	if len(normal) < 2 || len(labels) < 2 {
		return r, fmt.Errorf("evaluation needs at least two normal training windows and both normal and fault scenarios")
	}
	t, err := fitTransform(train, false)
	if err != nil {
		return r, err
	}
	normalT, err := fitTransform(normal, true)
	if err != nil {
		return r, err
	}
	centres, err := centroids(train, t)
	if err != nil {
		return r, err
	}
	counts := map[string]int{}
	for _, row := range train {
		counts[classOf(row)]++
	}
	majority := ""
	largest := -1
	for _, label := range labels {
		if counts[label] > largest {
			largest, majority = counts[label], label
		}
	}
	for _, name := range []string{"nearest_centroid", "majority_scenario", "normal_zscore", "probe_rule"} {
		modelLabels, task := labels, "scenario_classification"
		if name == "normal_zscore" || name == "probe_rule" {
			modelLabels, task = []string{"normal", "fault"}, "normal_vs_fault_interval"
		}
		model := ModelReport{Task: task, Predictions: []Prediction{}}
		observations := []observation{}
		if name == "nearest_centroid" {
			model.Transformer, model.Centroids = &t, centres
		}
		if name == "normal_zscore" {
			model.Transformer = &normalT
		}
		for _, row := range test {
			truth, prediction := classOf(row), majority
			var score *float64
			switch name {
			case "nearest_centroid":
				x, err := t.apply(row)
				if err != nil {
					return r, err
				}
				prediction, err = closest(x, centres, labels)
				if err != nil {
					return r, err
				}
			case "normal_zscore":
				truth = binaryTruth(row)
				x, err := normalT.apply(row)
				if err != nil {
					return r, err
				}
				maximum := 0.0
				for _, v := range x {
					maximum = math.Max(maximum, math.Abs(v))
				}
				score = &maximum
				prediction = "normal"
				if maximum > options.Threshold {
					prediction = "fault"
				}
			case "probe_rule":
				truth = binaryTruth(row)
				sent, hasSent := row.Features["probe_sent_mean"]
				received, hasReceived := row.Features["probe_received_mean"]
				prediction = ""
				if hasSent && hasReceived && sent > 0 {
					prediction = "normal"
					if received < sent {
						prediction = "fault"
					}
				}
			}
			model.Predictions = append(model.Predictions, Prediction{RunID: row.RunID, WindowStart: row.WindowStart, Truth: truth, Predicted: prediction, Score: score})
			observations = append(observations, observation{runID: row.RunID, truth: truth, prediction: prediction})
		}
		model.WindowMetrics = measure(modelLabels, observations)
		model.RunMetrics = measure(modelLabels, byRun(modelLabels, observations))
		r.Models[name] = model
	}
	if !r.Split.RequireProtocol {
		r.Limitations = append(r.Limitations, "Legacy protocol-start evidence was explicitly allowed; mixed starting conditions may contaminate labels.")
	}
	if r.Split.EmbargoSeconds == 0 {
		r.Limitations = append(r.Limitations, "No temporal embargo was applied; disjoint run IDs alone do not exclude shared metric averaging intervals.")
	}
	if err := runrecord.Save(outputPath, r); err != nil {
		return r, err
	}
	return r, nil
}

func validateSplit(data dataset.WindowData, m dataset.SplitManifest, allowLegacy bool) ([]dataset.WindowRow, []dataset.WindowRow, []string, error) {
	if m.EmbargoSeconds < 0 || m.EmbargoSeconds > 86400 || math.IsNaN(m.EmbargoSeconds) || math.IsInf(m.EmbargoSeconds, 0) {
		return nil, nil, nil, fmt.Errorf("invalid temporal embargo in split manifest")
	}
	if m.TestFraction <= 0 || m.TestFraction >= 1 {
		return nil, nil, nil, fmt.Errorf("invalid test fraction in split manifest")
	}
	if m.SchemaVersion != 1 || m.WindowsSHA256 != data.SHA256 || !slices.Equal(m.FeatureNames, dataset.WindowFeatureNames()) {
		return nil, nil, nil, fmt.Errorf("split schema, feature allowlist or CSV hash mismatch; rebuild the split manifest")
	}
	if !allowLegacy && !m.RequireProtocol {
		return nil, nil, nil, fmt.Errorf("legacy split requires explicit allow-legacy")
	}
	roles := map[string]string{}
	for role, ids := range map[string][]string{"train": m.TrainRuns, "test": m.TestRuns, "purged": m.PurgedRuns} {
		for _, id := range ids {
			if _, exists := roles[id]; exists {
				return nil, nil, nil, fmt.Errorf("duplicate or overlapping split run %s", id)
			}
			roles[id] = role
		}
	}
	train, test := []dataset.WindowRow{}, []dataset.WindowRow{}
	seen := map[string]bool{}
	counts := map[string]dataset.ClassSplit{}
	for _, row := range data.Rows {
		role := roles[row.RunID]
		if role == "" {
			return nil, nil, nil, fmt.Errorf("run %s is absent from split", row.RunID)
		}
		if (m.RequireProtocol || !allowLegacy) && !row.ProtocolVerified {
			return nil, nil, nil, fmt.Errorf("run %s has no verified protocol start", row.RunID)
		}
		fresh := !seen[row.RunID]
		seen[row.RunID] = true
		if role == "purged" {
			continue
		}
		if role == "train" {
			train = append(train, row)
		} else {
			test = append(test, row)
		}
		if fresh {
			c := counts[classOf(row)]
			if role == "train" {
				c.TrainRuns++
			} else {
				c.TestRuns++
			}
			counts[classOf(row)] = c
		}
	}
	if len(seen) != len(roles) || len(counts) != len(m.Classes) || len(train) == 0 || len(test) == 0 {
		return nil, nil, nil, fmt.Errorf("split membership does not match the window dataset")
	}
	labels := make([]string, 0, len(counts))
	for class, c := range counts {
		if c.TrainRuns < 1 || c.TestRuns < 1 || m.Classes[class] != c {
			return nil, nil, nil, fmt.Errorf("class %s is missing distinct train/test run support or has inconsistent counts", class)
		}
		labels = append(labels, class)
	}
	sort.Strings(labels)
	if err := dataset.VerifyTemporalSeparation(data, m.TrainRuns, m.TestRuns, time.Duration(m.EmbargoSeconds*float64(time.Second))); err != nil {
		return nil, nil, nil, err
	}
	return train, test, labels, nil
}

func separateOutput(input, output string) error {
	a, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	b, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if strings.EqualFold(a, b) {
		return fmt.Errorf("evaluation output must not overwrite an input")
	}
	fi, e1 := os.Stat(input)
	fo, e2 := os.Stat(output)
	if e1 == nil && e2 == nil && os.SameFile(fi, fo) {
		return fmt.Errorf("evaluation output aliases an input")
	}
	return nil
}
