package neural

import (
	"math"
	"testing"

	"github.com/lazypanic999/CorePulse/internal/inference"
)

func TestDerivedMSEBudgetDoesNotRelaxRawOutputOrClassParity(t *testing.T) {
	ref := referenceCase{Inputs: []float64{30, 30}, Outputs: []float64{.1, .2}, Score: 898, Predicted: "fault"}
	got := inference.Result{Outputs: append([]float64(nil), ref.Outputs...), Score: 898.0000610351562, Predicted: "fault"}
	threshold := 10.
	if err := verifyNumerics("autoencoder", got, ref, &threshold); err != nil {
		t.Fatal("legitimate float32 score rounding refused:", err)
	}
	got.Outputs[0] += .00002
	if err := verifyNumerics("autoencoder", got, ref, &threshold); err == nil {
		t.Fatal("raw absolute output tolerance was relaxed")
	}
	got.Outputs[0] = ref.Outputs[0]
	got.Score = 898.01
	if err := verifyNumerics("autoencoder", got, ref, &threshold); err == nil {
		t.Fatal("large score corruption accepted")
	}
	got.Score = ref.Score
	got.Predicted = "normal"
	if err := verifyNumerics("autoencoder", got, ref, &threshold); err == nil {
		t.Fatal("classification mismatch accepted")
	}
	got.Predicted = "fault"
	got.Score = ref.Score + .00002
	if err := verifyNumerics("mlp", got, ref, nil); err == nil {
		t.Fatal("MLP score tolerance changed")
	}
}

// A held-out window identical to the calibration window that set the threshold
// scores exactly at it in PyTorch (normal: equality is normal) and one float32
// ULP above it in Go (fault). Only that boundary case may differ in class.
func TestAutoencoderClassMayDifferOnlyAtTheThreshold(t *testing.T) {
	threshold := 898.
	ref := referenceCase{Inputs: []float64{30, 30}, Outputs: []float64{.1, .2}, Score: threshold, Predicted: "normal"}
	above := float64(math.Nextafter32(float32(threshold), math.MaxFloat32))
	got := inference.Result{Outputs: append([]float64(nil), ref.Outputs...), Score: above, Predicted: "fault"}
	if err := verifyNumerics("autoencoder", got, ref, &threshold); err != nil {
		t.Fatal("one-ULP threshold tie refused:", err)
	}
	if err := verifyNumerics("autoencoder", got, ref, nil); err == nil {
		t.Fatal("class mismatch accepted without a threshold")
	}
	far := 100.
	if err := verifyNumerics("autoencoder", got, ref, &far); err == nil {
		t.Fatal("class mismatch accepted away from the threshold")
	}
	if err := verifyNumerics("mlp", got, ref, &threshold); err == nil {
		t.Fatal("MLP class mismatch accepted")
	}
}
