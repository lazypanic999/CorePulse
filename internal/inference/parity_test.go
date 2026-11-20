package inference

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"testing"
)

// Python constructs this temporary fixture from hand-authored coefficients and
// CPU PyTorch outputs. Ordinary Go-only tests do not require a Python install;
// the mandatory ML suite must invoke this test and verify its execution marker.
func TestPyTorchFixtureParity(t *testing.T) {
	path := os.Getenv("COREPULSE_PYTORCH_FIXTURE")
	if path == "" {
		t.Skip("temporary PyTorch fixture is supplied by pytest")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Kind      string          `json:"fixture_kind"`
		Tolerance float64         `json:"tolerance"`
		Artifact  json.RawMessage `json:"artifact"`
		Cases     []struct {
			Values    map[string]*float64 `json:"values"`
			Outputs   []float64           `json:"outputs"`
			Scores    []float64           `json:"scores"`
			Score     float64             `json:"score"`
			Predicted string              `json:"predicted"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Kind != "hand_authored_not_trained" || fixture.Tolerance != 1e-5 || len(fixture.Cases) < 4 {
		t.Fatal("invalid parity fixture contract")
	}
	m, err := Load(bytes.NewReader(fixture.Artifact))
	if err != nil {
		t.Fatal(err)
	}
	compare := func(label string, got, want []float64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s dimensions differ", label)
		}
		for i, x := range got {
			if !finite(x) || !finite(want[i]) || math.Abs(x-want[i]) > fixture.Tolerance {
				t.Fatalf("%s[%d] Go=%g PyTorch=%g exceeds 1e-5", label, i, x, want[i])
			}
		}
	}
	for index, test := range fixture.Cases {
		r, err := m.Predict(test.Values)
		if err != nil {
			t.Fatalf("case%d: %v", index, err)
		}
		compare("outputs", r.Outputs, test.Outputs)
		compare("scores", r.Scores, test.Scores)
		if !finite(r.Score) || !finite(test.Score) || math.Abs(r.Score-test.Score) > fixture.Tolerance || r.Predicted != test.Predicted || !r.Synthetic {
			t.Fatalf("case%d: prediction/score mismatch: %+v want %s/%g", index, r, test.Predicted, test.Score)
		}
	}
	t.Logf("PYTORCH_PARITY_CHECKED %d hand-authored cases at absolute tolerance 1e-5", len(fixture.Cases))
}
