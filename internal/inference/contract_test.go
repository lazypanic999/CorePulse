package inference

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"testing/iotest"
)

func TestDenseReluArithmeticAndStableSoftmax(t *testing.T) {
	a := handArtifact()
	a["layers"] = []any{
		map[string]any{"op": "linear", "weights": [][]float64{{1, -1}, {-1, 1}}, "bias": []float64{0, 0}},
		map[string]any{"op": "relu"},
		map[string]any{"op": "linear", "weights": [][]float64{{1, 0}, {0, 1}}, "bias": []float64{0, 0}},
	}
	m := loadHand(t, a)
	for _, test := range []struct {
		cpu           float64
		want          string
		first, second float64
	}{{3, "pod_crash:amf", 0, 1}, {7, "normal", 1, 0}} {
		r, err := m.Predict(map[string]*float64{"cpu_millicores_mean": pointer(test.cpu), "cpu_millicores_delta": pointer(2)})
		if err != nil || r.Predicted != test.want || r.Outputs[0] != test.first || r.Outputs[1] != test.second {
			t.Fatalf("relu arithmetic: %+v %v", r, err)
		}
		if math.Abs(r.Scores[0]+r.Scores[1]-1) > 1e-6 {
			t.Fatal("invalid softmax normalization")
		}
	}
	large := loadHand(t, handArtifact())
	r, err := large.Predict(map[string]*float64{"cpu_millicores_mean": pointer(100001)})
	if err != nil || r.Score != 1 || r.Scores[0] != 1 || r.Scores[1] != 0 || r.Predicted != "normal" {
		t.Fatalf("unstable large-logit softmax: %+v %v", r, err)
	}
}

func TestCoefficientLayerAndDocumentBounds(t *testing.T) {
	base := handArtifact()
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	// A valid JSON document followed by whitespace would otherwise load. This
	// specifically exercises the byte bound, rather than invalid JSON rejection.
	if _, err := Load(strings.NewReader(string(raw) + strings.Repeat(" ", MaxArtifactBytes))); err == nil || !strings.Contains(err.Error(), "exceeds 16 MiB") {
		t.Fatalf("document byte limit not enforced: %v", err)
	}
	base["layers"] = make([]any, 33)
	for i := range base["layers"].([]any) {
		base["layers"].([]any)[i] = handArtifact()["layers"].([]any)[0]
	}
	tooMany, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bytes.NewReader(tooMany)); err == nil || !strings.Contains(err.Error(), "layer count") {
		t.Fatalf("operator bound not enforced: %v", err)
	}

	base = handArtifact()
	weights := make([][]float64, 4097)
	for i := range weights {
		weights[i] = []float64{0, 0}
	}
	base["layers"] = []any{
		map[string]any{"op": "linear", "weights": weights, "bias": make([]float64, len(weights))},
		map[string]any{"op": "linear", "weights": [][]float64{make([]float64, 4097), make([]float64, 4097)}, "bias": []float64{0, 0}},
	}
	wide, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bytes.NewReader(wide)); err == nil || !strings.Contains(err.Error(), "output dimension") {
		t.Fatalf("width bound not enforced: %v", err)
	}

	first, second, third := make([][]float64, 1000), make([][]float64, 1000), make([][]float64, 2)
	for i := range first {
		first[i] = make([]float64, 2)
		second[i] = make([]float64, 1000)
	}
	for i := range third {
		third[i] = make([]float64, 1000)
	}
	base = handArtifact()
	base["layers"] = []any{
		map[string]any{"op": "linear", "weights": first, "bias": make([]float64, 1000)},
		map[string]any{"op": "linear", "weights": second, "bias": make([]float64, 1000)},
		map[string]any{"op": "linear", "weights": third, "bias": make([]float64, 2)},
	}
	coefficients, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(coefficients) >= MaxArtifactBytes {
		t.Fatal("coefficient-limit fixture accidentally exceeds document bound")
	}
	if _, err := Load(bytes.NewReader(coefficients)); err == nil || !strings.Contains(err.Error(), "coefficient limit") {
		t.Fatalf("coefficient bound not enforced: %v", err)
	}
}

func TestTypedJSONErrorsAndReadFailuresRefuse(t *testing.T) {
	readError := errors.New("invented reader failure")
	if _, err := Load(iotest.ErrReader(readError)); !errors.Is(err, readError) {
		t.Fatalf("read error lost: %v", err)
	}
	for _, raw := range []string{`{`, `[]`, `1`, `"text"`, `{"kind":1}`, `{"kind":"mlp"}`, `{"kind":"mlp","kind":"autoencoder"}`} {
		if _, err := Load(strings.NewReader(raw)); err == nil {
			t.Fatalf("invalid root accepted: %s", raw)
		}
	}
	for name, mutate := range map[string]func(map[string]any){
		"typed source":        func(a map[string]any) { a["source"] = false },
		"typed preprocessing": func(a map[string]any) { a["preprocessing"] = []any{} },
		"typed layers":        func(a map[string]any) { a["layers"] = 1 },
		"typed layer":         func(a map[string]any) { a["layers"] = []any{1} },
		"typed op":            func(a map[string]any) { a["layers"].([]any)[0].(map[string]any)["op"] = 1 },
		"typed flag":          func(a map[string]any) { a["synthetic"] = "true" },
		"duplicate fit":       func(a map[string]any) { a["preprocessing"].(map[string]any)["fit_run_ids"] = []string{"same", "same"} },
		"blank fit":           func(a map[string]any) { a["preprocessing"].(map[string]any)["fit_run_ids"] = []string{" "} },
		"relu extra fields":   func(a map[string]any) { a["layers"] = []any{map[string]any{"op": "relu", "bias": []float64{0}}} },
	} {
		t.Run(name, func(t *testing.T) {
			a := handArtifact()
			mutate(a)
			raw, err := json.Marshal(a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Load(bytes.NewReader(raw)); err == nil {
				t.Fatal("invalid typed contract accepted")
			}
		})
	}
}

func TestMissingRatioBoundsZeroModelsAndReconstructionOverflow(t *testing.T) {
	a := handArtifact()
	p := a["preprocessing"].(map[string]any)
	p["feature_names"], p["medians"], p["means"], p["scales"] = []string{"amf_cpu_millicores_missing_ratio", "cpu_millicores_delta"}, []float64{.5, 0}, []float64{.5, 0}, []float64{1, 1}
	m := loadHand(t, a)
	for _, ratio := range []float64{-.001, 1.001} {
		if _, err := m.Predict(map[string]*float64{"amf_cpu_millicores_missing_ratio": pointer(ratio)}); err == nil {
			t.Fatal("invalid missing ratio accepted")
		}
	}
	for _, ratio := range []float64{0, 1} {
		if _, err := m.Predict(map[string]*float64{"amf_cpu_millicores_missing_ratio": pointer(ratio)}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Predict(map[string]*float64{"run_id": nil}); err == nil {
		t.Fatal("nil metadata input ignored")
	}
	var zero Model
	var missing *Model
	for _, model := range []*Model{&zero, missing} {
		if _, err := model.Predict(nil); err == nil {
			t.Fatal("unloaded model accepted")
		}
	}
	a = handArtifact()
	a["kind"], a["task"], a["labels"] = "autoencoder", "normal_vs_fault_interval", []string{"normal", "fault"}
	a["threshold"], a["calibration_run_ids"] = 0, []string{"invented-calibration"}
	a["layers"] = []any{map[string]any{"op": "linear", "weights": [][]float64{{0, 0}, {0, 0}}, "bias": []float64{0, 0}}}
	autoencoder := loadHand(t, a)
	if _, err := autoencoder.Predict(map[string]*float64{"cpu_millicores_mean": pointer(1e22)}); err == nil {
		t.Fatal("reconstruction square overflow accepted")
	}
}

func FuzzLoadArtifact(f *testing.F) {
	raw, err := json.Marshal(handArtifact())
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{raw, []byte(`null`), []byte(`{"kind":"mlp"}`), []byte(`{"x":1,"x":2}`)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64*1024 {
			return
		} // Keep the fuzz workload bounded; large valid documents have unit tests above.
		model, err := Load(bytes.NewReader(data))
		if err != nil {
			return
		}
		result, err := model.Predict(nil)
		if err != nil {
			return
		} // Numerically unstable coefficients may validly refuse inference.
		if !finite(result.Score) {
			t.Fatal("successful inference returned nonfinite score")
		}
		for _, x := range append(result.Outputs, result.Scores...) {
			if !finite(x) {
				t.Fatal("successful inference returned nonfinite output")
			}
		}
	})
}
