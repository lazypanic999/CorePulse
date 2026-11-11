package neural

import (
	"fmt"
	"math"

	"github.com/lazypanic999/CorePulse/internal/inference"
)

type referenceCase struct {
	RunID     string    `json:"run_id"`
	Window    string    `json:"window"`
	Inputs    []float64 `json:"inputs"`
	Outputs   []float64 `json:"outputs"`
	Score     float64   `json:"score"`
	Predicted string    `json:"predicted"`
}
type cpuReferences struct {
	WindowsSHA256 string `json:"windows_sha256"`
	SplitSHA256   string `json:"split_sha256"`
	Models        map[string]struct {
		ArtifactSHA256 string          `json:"artifact_sha256"`
		Cases          []referenceCase `json:"cases"`
	} `json:"models"`
}

// Raw outputs retain absolute 1e-5 parity. A derived float32 MSE cannot have
// universal absolute 1e-5 precision: one ULP already exceeds it around 128.
// Bound score differences by the measured output difference plus the standard
// positive-sum/division float32 rounding budget. Classification must still match.
// Existing fixed-fixture score assertions remain absolute 1e-5 and unchanged.
func verifyNumerics(kind string, got inference.Result, reference referenceCase) error {
	if math.IsNaN(got.Score) || math.IsInf(got.Score, 0) || math.IsNaN(reference.Score) || math.IsInf(reference.Score, 0) {
		return fmt.Errorf("non-finite parity score")
	}
	if len(got.Outputs) != len(reference.Outputs) || len(got.Outputs) == 0 || got.Predicted != reference.Predicted {
		return fmt.Errorf("Go output dimensions/class differ from CPU PyTorch")
	}
	for i, value := range got.Outputs {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.IsNaN(reference.Outputs[i]) || math.IsInf(reference.Outputs[i], 0) || math.Abs(value-reference.Outputs[i]) > 1e-5 {
			return fmt.Errorf("Go raw output exceeds absolute 1e-5 parity")
		}
	}
	tolerance := 1e-5
	if kind == "autoencoder" {
		if len(reference.Inputs) != len(reference.Outputs) {
			return fmt.Errorf("reconstruction input dimensions differ")
		}
		propagated := 0.
		for i, value := range got.Outputs {
			delta := math.Abs(value - reference.Outputs[i])
			propagated += delta * (2*math.Abs(reference.Outputs[i]-reference.Inputs[i]) + delta)
		}
		rounding := (float64(len(got.Outputs)) + 3) * math.Ldexp(1, -23) * math.Max(1, math.Abs(reference.Score))
		tolerance = math.Max(tolerance, propagated/float64(len(got.Outputs))+rounding)
	}
	if math.Abs(got.Score-reference.Score) > tolerance {
		return fmt.Errorf("Go derived score exceeds its numerical error budget")
	}
	return nil
}
