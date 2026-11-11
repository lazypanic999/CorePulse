package neural

import (
	"github.com/lazypanic999/CorePulse/internal/inference"
	"testing"
)

func TestDerivedMSEBudgetDoesNotRelaxRawOutputOrClassParity(t *testing.T) {
	ref := referenceCase{Inputs: []float64{30, 30}, Outputs: []float64{.1, .2}, Score: 898, Predicted: "fault"}
	got := inference.Result{Outputs: append([]float64(nil), ref.Outputs...), Score: 898.0000610351562, Predicted: "fault"}
	if err := verifyNumerics("autoencoder", got, ref); err != nil {
		t.Fatal("legitimate float32 score rounding refused:", err)
	}
	got.Outputs[0] += .00002
	if err := verifyNumerics("autoencoder", got, ref); err == nil {
		t.Fatal("raw absolute output tolerance was relaxed")
	}
	got.Outputs[0] = ref.Outputs[0]
	got.Score = 898.01
	if err := verifyNumerics("autoencoder", got, ref); err == nil {
		t.Fatal("large score corruption accepted")
	}
	got.Score = ref.Score
	got.Predicted = "normal"
	if err := verifyNumerics("autoencoder", got, ref); err == nil {
		t.Fatal("classification mismatch accepted")
	}
	got.Predicted = "fault"
	got.Score = ref.Score + .00002
	if err := verifyNumerics("mlp", got, ref); err == nil {
		t.Fatal("MLP score tolerance changed")
	}
}
