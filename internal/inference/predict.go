package inference

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

type Result struct {
	Predicted string    `json:"predicted"`
	Outputs   []float64 `json:"outputs"`          // MLP logits or autoencoder reconstruction.
	Scores    []float64 `json:"scores,omitempty"` // MLP softmax; not calibrated probabilities.
	Score     float64   `json:"score"`            // Maximum softmax or reconstruction MSE.
	Synthetic bool      `json:"synthetic"`
}

// Predict accepts only the model's retained resource features. Omitted keys and
// nil values are missing; a pointer to zero is an observed zero. Inputs are never
// changed. No identity/label/target/probe metadata is accepted.
func (m *Model) Predict(values map[string]*float64) (Result, error) {
	if m == nil || m.definition.SchemaVersion != 1 {
		return Result{}, fmt.Errorf("model must be loaded and validated before inference")
	}
	a := m.definition
	p := a.Preprocessing
	for name, value := range values {
		if !slices.Contains(p.FeatureNames, name) {
			return Result{}, fmt.Errorf("unknown or unselected input feature %s", name)
		}
		if value != nil && (!finite(*value) || *value < 0 && !strings.HasSuffix(name, "_delta") || strings.HasSuffix(name, "_missing_ratio") && (*value < 0 || *value > 1)) {
			return Result{}, fmt.Errorf("invalid observed input %s", name)
		}
	}
	input := make([]float64, len(p.FeatureNames))
	for i, name := range p.FeatureNames {
		value := p.Medians[i]
		if observed := values[name]; observed != nil {
			value = *observed
		}
		x := (value - p.Means[i]) / p.Scales[i]
		x = float64(float32(x))
		if !finite(x) {
			return Result{}, fmt.Errorf("preprocessing overflow")
		}
		input[i] = x
	}
	x := append([]float64(nil), input...)
	for _, l := range a.Layers {
		if l.Op == "relu" {
			for i, v := range x {
				if v < 0 {
					x[i] = 0
				}
			}
			continue
		}
		out := make([]float64, len(l.Bias))
		for j, row := range l.Weights {
			sum := l.Bias[j]
			for i, w := range row {
				sum += w * x[i]
			}
			out[j] = float64(float32(sum))
			if !finite(out[j]) {
				return Result{}, fmt.Errorf("linear inference overflow")
			}
		}
		x = out
	}
	result := Result{Outputs: x, Synthetic: a.Synthetic}
	if a.Kind == "mlp" {
		largest := slices.Max(x)
		scores := make([]float64, len(x))
		sum := 0.
		for i, v := range x {
			scores[i] = math.Exp(v - largest)
			sum += scores[i]
		}
		best := 0
		for i := range scores {
			scores[i] = float64(float32(scores[i] / sum))
			if scores[i] > scores[best] {
				best = i
			}
		}
		result.Predicted, result.Scores, result.Score = a.Labels[best], scores, scores[best]
	} else {
		sum := 0.
		for i, v := range x {
			difference := float64(float32(v - input[i]))
			square := float64(float32(difference * difference))
			if !finite(square) {
				return Result{}, fmt.Errorf("reconstruction error overflow")
			}
			sum += square
		}
		result.Score = float64(float32(sum / float64(len(x))))
		if !finite(result.Score) {
			return Result{}, fmt.Errorf("reconstruction mean overflow")
		}
		result.Predicted = "normal"
		if result.Score > *a.Threshold {
			result.Predicted = "fault"
		}
	}
	return result, nil
}
