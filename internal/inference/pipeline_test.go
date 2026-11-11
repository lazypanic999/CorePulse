package inference

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

// All coefficients and windows for this check are produced in pytest's temporary
// directory from invented inputs. No trained artifacts are stored in the repo.
func TestSyntheticPipelineExport(t *testing.T) {
	path := os.Getenv("COREPULSE_SYNTHETIC_PIPELINE_FIXTURE")
	if path == "" {
		t.Skip("temporary synthetic training/export fixture is supplied by pytest")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Kind      string  `json:"fixture_kind"`
		Tolerance float64 `json:"tolerance"`
		Source    source  `json:"source"`
		Artifacts []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Kind   string `json:"kind"`
			Task   string `json:"task"`
			Cases  []struct {
				Values    map[string]*float64 `json:"values"`
				Predicted string              `json:"predicted"`
				Score     float64             `json:"score"`
			} `json:"cases"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Kind != "synthetic_trained_temporary" || fixture.Tolerance != 1e-5 || len(fixture.Artifacts) != 4 {
		t.Fatal("expected four temporary synthetic models at tolerance 1e-5")
	}
	checked := 0
	for _, item := range fixture.Artifacts {
		data, err := os.ReadFile(item.Path)
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != item.SHA256 {
			t.Fatal("artifact bytes disagree with report hash")
		}
		model, err := Load(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		meta := model.Metadata()
		if meta.Kind != item.Kind || meta.Task != item.Task || !meta.Synthetic || meta.WindowsSHA256 != fixture.Source.WindowsSHA256 || meta.SplitSHA256 != fixture.Source.SplitSHA256 || len(item.Cases) != 6 {
			t.Fatal("export lost source/task identity or held-out support")
		}
		for index, c := range item.Cases {
			result, err := model.Predict(c.Values)
			if err != nil {
				t.Fatal(err)
			}
			if !finite(c.Score) || math.Abs(result.Score-c.Score) > fixture.Tolerance || result.Predicted != c.Predicted {
				t.Fatalf("%s case%d: exported Go=%s/%g, trained PyTorch=%s/%g", item.Kind, index, result.Predicted, result.Score, c.Predicted, c.Score)
			}
			checked++
		}
	}
	t.Logf("SYNTHETIC_PIPELINE_CHECKED %d artifacts %d held-out predictions", len(fixture.Artifacts), checked)
}
