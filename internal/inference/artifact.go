// Package inference evaluates the restricted version-one offline neural JSON
// contract. It uses the Go standard library only and never contacts a collector,
// cluster, model service or external process. Loading does not authenticate a model.
package inference

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

const MaxArtifactBytes = 16 * 1024 * 1024

// This explicit allowlist matches corepulse_ml.data.RESOURCE_FEATURES. All
// identity, truth, packet-probe and post-run event columns are excluded.
var resourceFeatures = []string{
	"pod_count_mean", "pod_count_delta", "cpu_millicores_mean", "cpu_millicores_max",
	"cpu_millicores_delta", "memory_bytes_mean", "memory_bytes_delta",
	"amf_cpu_millicores_mean", "amf_cpu_millicores_missing_ratio",
	"upf_cpu_millicores_mean", "upf_cpu_millicores_missing_ratio",
}

type source struct {
	WindowsSHA256 string `json:"windows_sha256"`
	SplitSHA256   string `json:"split_sha256"`
}
type preprocessing struct {
	FeatureNames    []string  `json:"feature_names"`
	Medians         []float64 `json:"medians"`
	Means           []float64 `json:"means"`
	Scales          []float64 `json:"scales"`
	DroppedFeatures []string  `json:"dropped_features"`
	FitRunIDs       []string  `json:"fit_run_ids"`
}
type layer struct {
	Op      string      `json:"op"`
	Weights [][]float64 `json:"weights,omitempty"`
	Bias    []float64   `json:"bias,omitempty"`
}
type artifact struct {
	SchemaVersion     int           `json:"schema_version"`
	Kind              string        `json:"kind"`
	Dtype             string        `json:"dtype"`
	Synthetic         bool          `json:"synthetic"`
	Source            source        `json:"source"`
	Preprocessing     preprocessing `json:"preprocessing"`
	Layers            []layer       `json:"layers"`
	Labels            []string      `json:"labels"`
	Task              string        `json:"task"`
	Seed              int64         `json:"seed"`
	Threshold         *float64      `json:"threshold,omitempty"`
	CalibrationRunIDs []string      `json:"calibration_run_ids,omitempty"`
}

// Model owns private immutable parameters; Predict is safe for concurrent callers.
type Model struct{ definition artifact }

type Metadata struct {
	Kind          string
	Task          string
	Synthetic     bool
	Seed          int64
	FeatureNames  []string
	Labels        []string
	WindowsSHA256 string
	SplitSHA256   string
}

func (m *Model) Metadata() Metadata {
	a := m.definition
	return Metadata{a.Kind, a.Task, a.Synthetic, a.Seed, append([]string(nil), a.Preprocessing.FeatureNames...), append([]string(nil), a.Labels...), a.Source.WindowsSHA256, a.Source.SplitSHA256}
}

// Load rejects duplicate keys, nulls, unknown/missing fields, trailing JSON and
// unsupported numerical contracts before constructing an immutable model.
func Load(reader io.Reader) (*Model, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, MaxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxArtifactBytes {
		return nil, fmt.Errorf("artifact exceeds 16 MiB")
	}
	if err := checkJSON(raw); err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	var kind string
	if err := json.Unmarshal(root["kind"], &kind); err != nil {
		return nil, fmt.Errorf("missing or invalid artifact kind")
	}
	keys := []string{"schema_version", "kind", "dtype", "synthetic", "source", "preprocessing", "layers", "labels", "task", "seed"}
	if kind == "autoencoder" {
		keys = append(keys, "threshold", "calibration_run_ids")
	} else if kind != "mlp" {
		return nil, fmt.Errorf("unsupported artifact kind")
	}
	if err := exactKeys(root, keys); err != nil {
		return nil, err
	}
	for name, fields := range map[string][]string{"source": {"windows_sha256", "split_sha256"}, "preprocessing": {"feature_names", "medians", "means", "scales", "dropped_features", "fit_run_ids"}} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(root[name], &object); err != nil {
			return nil, err
		}
		if err := exactKeys(object, fields); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	var operators []json.RawMessage
	if err := json.Unmarshal(root["layers"], &operators); err != nil {
		return nil, err
	}
	for _, rawLayer := range operators {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(rawLayer, &object); err != nil {
			return nil, err
		}
		var op string
		if err := json.Unmarshal(object["op"], &op); err != nil {
			return nil, err
		}
		fields := []string{"op"}
		if op == "linear" {
			fields = append(fields, "weights", "bias")
		} else if op != "relu" {
			return nil, fmt.Errorf("unsupported layer operator")
		}
		if err := exactKeys(object, fields); err != nil {
			return nil, err
		}
	}
	var a artifact
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	if err := validate(a); err != nil {
		return nil, err
	}
	// Serialized coefficients are interpreted as float32, as on the exporter.
	for _, l := range a.Layers {
		for _, row := range l.Weights {
			for i, x := range row {
				row[i] = float64(float32(x))
			}
		}
		for i, x := range l.Bias {
			l.Bias[i] = float64(float32(x))
		}
	}
	return &Model{definition: a}, nil
}

func exactKeys(object map[string]json.RawMessage, keys []string) error {
	if len(object) != len(keys) {
		return fmt.Errorf("unknown or missing artifact fields")
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return fmt.Errorf("missing artifact field %s", key)
		}
	}
	return nil
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
func validHash(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && value == strings.ToLower(value)
}
func uniqueIDs(values []string) bool {
	if len(values) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if strings.TrimSpace(v) == "" || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}

func validate(a artifact) error {
	if a.SchemaVersion != 1 || (a.Kind != "mlp" && a.Kind != "autoencoder") || a.Dtype != "float32" || a.Seed < 0 || !validHash(a.Source.WindowsSHA256) || !validHash(a.Source.SplitSHA256) {
		return fmt.Errorf("unsupported version/dtype or invalid source/seed")
	}
	p := a.Preprocessing
	n := len(p.FeatureNames)
	if n < 1 || n > len(resourceFeatures) || len(p.Medians) != n || len(p.Means) != n || len(p.Scales) != n || !uniqueIDs(p.FitRunIDs) {
		return fmt.Errorf("invalid preprocessing dimensions/fit identities")
	}
	seen := map[string]bool{}
	for _, name := range append(append([]string(nil), p.FeatureNames...), p.DroppedFeatures...) {
		if !slices.Contains(resourceFeatures, name) || seen[name] {
			return fmt.Errorf("metadata, unsupported or duplicate feature")
		}
		seen[name] = true
	}
	for i := range n {
		if !finite(p.Medians[i]) || !finite(p.Means[i]) || !finite(p.Scales[i]) || p.Scales[i] <= 0 {
			return fmt.Errorf("nonfinite preprocessing parameter or nonpositive scale")
		}
	}
	if a.Kind == "mlp" {
		if a.Task != "scenario_classification" || len(a.Labels) < 2 || len(a.Labels) > 3 || !slices.IsSorted(a.Labels) || !slices.Contains(a.Labels, "normal") {
			return fmt.Errorf("invalid scenario task/labels")
		}
		for i, label := range a.Labels {
			if !slices.Contains([]string{"normal", "pod_crash:amf", "pod_crash:upf"}, label) || i > 0 && label == a.Labels[i-1] {
				return fmt.Errorf("unsupported/duplicate scenario label")
			}
		}
	} else {
		if a.Task != "normal_vs_fault_interval" || !slices.Equal(a.Labels, []string{"normal", "fault"}) || a.Threshold == nil || !finite(*a.Threshold) || *a.Threshold < 0 || !uniqueIDs(a.CalibrationRunIDs) {
			return fmt.Errorf("invalid autoencoder task/calibration")
		}
		for _, id := range a.CalibrationRunIDs {
			if slices.Contains(p.FitRunIDs, id) {
				return fmt.Errorf("normal calibration overlaps fit identities")
			}
		}
	}
	if len(a.Layers) < 1 || len(a.Layers) > 32 {
		return fmt.Errorf("invalid layer count")
	}
	width, parameters, previous := n, 0, ""
	for index, l := range a.Layers {
		if l.Op == "relu" {
			if previous != "linear" || index == len(a.Layers)-1 {
				return fmt.Errorf("invalid relu position")
			}
		} else if l.Op == "linear" {
			outputs := len(l.Weights)
			if outputs < 1 || outputs > 4096 || len(l.Bias) != outputs {
				return fmt.Errorf("invalid linear output dimension")
			}
			parameters += outputs * (width + 1)
			if parameters > 1000000 {
				return fmt.Errorf("coefficient limit exceeded")
			}
			for _, row := range l.Weights {
				if len(row) != width {
					return fmt.Errorf("linear input dimension mismatch")
				}
				for _, x := range row {
					if !finite(x) || math.Abs(x) > math.MaxFloat32 {
						return fmt.Errorf("invalid float32 coefficient")
					}
				}
			}
			for _, x := range l.Bias {
				if !finite(x) || math.Abs(x) > math.MaxFloat32 {
					return fmt.Errorf("invalid float32 bias")
				}
			}
			width = outputs
		} else {
			return fmt.Errorf("unsupported layer operator")
		}
		previous = l.Op
	}
	expected := n
	if a.Kind == "mlp" {
		expected = len(a.Labels)
	}
	if previous != "linear" || width != expected {
		return fmt.Errorf("network output dimension mismatches task")
	}
	return nil
}

// checkJSON also forbids null: null is not a legal coefficient, flag, scalar or
// list in this format, and encoding/json otherwise converts numeric null to zero.
func checkJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return fmt.Errorf("artifact JSON nesting limit exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		if token == nil {
			return fmt.Errorf("null is not supported in artifacts")
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate or invalid JSON key")
				}
				seen[name] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}
