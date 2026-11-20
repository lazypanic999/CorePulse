package inference

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
)

// These coefficients are hand-authored arithmetic fixtures, never trained weights.
func handArtifact() map[string]any {
	return map[string]any{
		"schema_version": 1, "kind": "mlp", "dtype": "float32", "synthetic": true, "seed": 7,
		"source":        map[string]any{"windows_sha256": strings.Repeat("a", 64), "split_sha256": strings.Repeat("b", 64)},
		"preprocessing": map[string]any{"feature_names": []string{"cpu_millicores_mean", "cpu_millicores_delta"}, "medians": []float64{2, 0}, "means": []float64{1, 0}, "scales": []float64{2, 1}, "dropped_features": []string{}, "fit_run_ids": []string{"invented-fit"}},
		"layers":        []any{map[string]any{"op": "linear", "weights": [][]float64{{1, 0}, {-1, 0}}, "bias": []float64{0, 0}}},
		"labels":        []string{"normal", "pod_crash:amf"}, "task": "scenario_classification",
	}
}

func loadHand(t *testing.T, object map[string]any) *Model {
	t.Helper()
	raw, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func pointer(x float64) *float64 { return &x }

func TestMissingAndObservedZeroRemainDistinctAndInputIsUntouched(t *testing.T) {
	m := loadHand(t, handArtifact())
	missing, err := m.Predict(nil)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]*float64{"cpu_millicores_mean": pointer(0), "cpu_millicores_delta": pointer(-2)}
	zero, err := m.Predict(values)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Predicted != "normal" || zero.Predicted != "pod_crash:amf" || zero.Outputs[0] != -.5 || !zero.Synthetic {
		t.Fatalf("missing=%+v zero=%+v", missing, zero)
	}
	if len(values) != 2 || *values["cpu_millicores_delta"] != -2 || *values["cpu_millicores_mean"] != 0 {
		t.Fatal("input changed")
	}
	missingNil, err := m.Predict(map[string]*float64{"cpu_millicores_mean": nil})
	if err != nil || missingNil.Score != missing.Score {
		t.Fatal("nil and omitted features differ")
	}
	tie, err := m.Predict(map[string]*float64{"cpu_millicores_mean": pointer(1)})
	if err != nil || tie.Predicted != "normal" || tie.Scores[0] != .5 {
		t.Fatalf("tie changed: %+v %v", tie, err)
	}
	metadata := m.Metadata()
	metadata.FeatureNames[0] = "run_id"
	metadata.Labels[0] = "changed"
	if m.Metadata().FeatureNames[0] != "cpu_millicores_mean" || m.Metadata().Labels[0] != "normal" {
		t.Fatal("metadata exposed mutable model state")
	}
}

func TestAutoencoderThresholdEqualityRemainsNormal(t *testing.T) {
	a := handArtifact()
	a["kind"], a["task"], a["labels"] = "autoencoder", "normal_vs_fault_interval", []string{"normal", "fault"}
	a["threshold"], a["calibration_run_ids"] = .125, []string{"invented-calibration"}
	a["layers"] = []any{map[string]any{"op": "linear", "weights": [][]float64{{0, 0}, {0, 0}}, "bias": []float64{0, 0}}}
	m := loadHand(t, a)
	for _, test := range []struct {
		input float64
		want  string
		score float64
	}{{2, "normal", .125}, {3, "fault", .5}, {1, "normal", 0}} {
		r, err := m.Predict(map[string]*float64{"cpu_millicores_mean": pointer(test.input)})
		if err != nil || r.Predicted != test.want || r.Score != test.score || r.Scores != nil {
			t.Fatalf("%+v %v", r, err)
		}
	}
}

func TestLoaderRejectsMalformedOrUnsupportedArtifacts(t *testing.T) {
	cases := map[string]func(map[string]any){
		"version":       func(a map[string]any) { a["schema_version"] = 2 },
		"missing flag":  func(a map[string]any) { delete(a, "synthetic") },
		"missing seed":  func(a map[string]any) { delete(a, "seed") },
		"negative seed": func(a map[string]any) { a["seed"] = -1 },
		"unknown field": func(a map[string]any) { a["code"] = "never executed" },
		"wrong kind":    func(a map[string]any) { a["kind"] = "lstm" },
		"dtype":         func(a map[string]any) { a["dtype"] = "float64" },
		"source hash":   func(a map[string]any) { a["source"].(map[string]any)["windows_sha256"] = "bad" },
		"source extra":  func(a map[string]any) { a["source"].(map[string]any)["other"] = 1 },
		"feature": func(a map[string]any) {
			a["preprocessing"].(map[string]any)["feature_names"] = []string{"run_id", "cpu_millicores_delta"}
		},
		"duplicate feature": func(a map[string]any) {
			a["preprocessing"].(map[string]any)["feature_names"] = []string{"cpu_millicores_mean", "cpu_millicores_mean"}
		},
		"dropped overlap": func(a map[string]any) {
			a["preprocessing"].(map[string]any)["dropped_features"] = []string{"cpu_millicores_mean"}
		},
		"zero scale":        func(a map[string]any) { a["preprocessing"].(map[string]any)["scales"] = []float64{0, 1} },
		"wrong scale count": func(a map[string]any) { a["preprocessing"].(map[string]any)["scales"] = []float64{1} },
		"empty fit ids":     func(a map[string]any) { a["preprocessing"].(map[string]any)["fit_run_ids"] = []string{} },
		"dimension":         func(a map[string]any) { a["layers"].([]any)[0].(map[string]any)["weights"] = [][]float64{{1}, {2}} },
		"bias count":        func(a map[string]any) { a["layers"].([]any)[0].(map[string]any)["bias"] = []float64{1} },
		"float32 overflow":  func(a map[string]any) { a["layers"].([]any)[0].(map[string]any)["bias"] = []float64{1e100, 0} },
		"operator":          func(a map[string]any) { a["layers"].([]any)[0].(map[string]any)["op"] = "sigmoid" },
		"relu first":        func(a map[string]any) { a["layers"] = []any{map[string]any{"op": "relu"}} },
		"output dimension": func(a map[string]any) {
			a["layers"].([]any)[0].(map[string]any)["weights"] = [][]float64{{1, 0}}
			a["layers"].([]any)[0].(map[string]any)["bias"] = []float64{0}
		},
		"labels":           func(a map[string]any) { a["labels"] = []string{"normal", "fault"} },
		"task":             func(a map[string]any) { a["task"] = "service_health" },
		"threshold on mlp": func(a map[string]any) { a["threshold"] = 0 },
		"null coefficient": func(a map[string]any) { a["layers"].([]any)[0].(map[string]any)["bias"] = []any{nil, 0} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			a := handArtifact()
			mutate(a)
			raw, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Load(bytes.NewReader(raw)); err == nil {
				t.Fatal("invalid artifact accepted")
			}
		})
	}
	raw, err := json.Marshal(handArtifact())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		string(raw) + " {}",
		strings.Replace(string(raw), `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(string(raw), `"op":"linear"`, `"op":"linear","op":"linear"`, 1),
		strings.Replace(string(raw), `"seed":7`, `"seed":null`, 1),
		strings.Replace(string(raw), `"seed":7`, `"SEED":7`, 1),
		strings.Repeat("[", 40) + strings.Repeat("]", 40),
	} {
		if _, err := Load(strings.NewReader(bad)); err == nil {
			t.Fatal("duplicate/null/trailing/noncanonical/nested JSON accepted")
		}
	}
	if _, err := Load(strings.NewReader(strings.Repeat(" ", MaxArtifactBytes+1))); err == nil {
		t.Fatal("oversized artifact accepted")
	}
}

func TestInvalidInputsAndNumericalOverflowRefuse(t *testing.T) {
	m := loadHand(t, handArtifact())
	for name, values := range map[string]map[string]*float64{
		"metadata": {"run_id": pointer(1)}, "negative observed": {"cpu_millicores_mean": pointer(-1)},
		"nan": {"cpu_millicores_mean": pointer(math.NaN())}, "inf": {"cpu_millicores_mean": pointer(math.Inf(1))},
		"preprocessing overflow": {"cpu_millicores_mean": pointer(1e308)},
	} {
		if _, err := m.Predict(values); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	a := handArtifact()
	a["layers"].([]any)[0].(map[string]any)["weights"] = [][]float64{{math.MaxFloat32, 0}, {0, 0}}
	overflow := loadHand(t, a)
	if _, err := overflow.Predict(map[string]*float64{"cpu_millicores_mean": pointer(100)}); err == nil {
		t.Fatal("linear overflow accepted")
	}
}

func TestConcurrentInferenceOwnsIndependentOutputs(t *testing.T) {
	m := loadHand(t, handArtifact())
	var group sync.WaitGroup
	errors := make(chan error, 16)
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			r, err := m.Predict(nil)
			if err != nil {
				errors <- err
				return
			}
			if r.Predicted != "normal" {
				errors <- fmt.Errorf("prediction changed")
			}
			r.Outputs[0] = 999
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	r, err := m.Predict(nil)
	if err != nil || r.Outputs[0] != .5 {
		t.Fatal("output alias changed the model")
	}
}
