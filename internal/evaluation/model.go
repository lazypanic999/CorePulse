package evaluation

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/lazypanic999/CorePulse/internal/dataset"
)

// Resource-only inputs avoid using post-run protocol-log availability as an
// online feature. Labels, target, duration and IDs are never inputs.
var resourceFeatures = []string{"pod_count_mean", "pod_count_delta", "cpu_millicores_mean", "cpu_millicores_max", "cpu_millicores_delta", "memory_bytes_mean", "memory_bytes_delta", "amf_cpu_millicores_mean", "amf_cpu_millicores_missing_ratio", "upf_cpu_millicores_mean", "upf_cpu_millicores_missing_ratio"}

type Transformer struct {
	FeatureNames    []string  `json:"feature_names"`
	Medians         []float64 `json:"medians"`
	Means           []float64 `json:"means"`
	Scales          []float64 `json:"scales"`
	DroppedFeatures []string  `json:"dropped_features"`
}

// fitTransform derives every imputation and scaling parameter from the supplied
// training rows. The normal-only model supplies only normal training rows.
func fitTransform(rows []dataset.WindowRow, normalScale bool) (Transformer, error) {
	t := Transformer{FeatureNames: []string{}, Medians: []float64{}, Means: []float64{}, Scales: []float64{}, DroppedFeatures: []string{}}
	for _, name := range resourceFeatures {
		values := []float64{}
		for _, row := range rows {
			if v, ok := row.Features[name]; ok {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			t.DroppedFeatures = append(t.DroppedFeatures, name)
			continue
		}
		sort.Float64s(values)
		median := values[len(values)/2]
		if len(values)%2 == 0 {
			median = values[len(values)/2-1]/2 + values[len(values)/2]/2
		}
		mean, m2 := 0.0, 0.0
		for i, row := range rows {
			v, ok := row.Features[name]
			if !ok {
				v = median
			}
			delta := v - mean
			mean += delta / float64(i+1)
			m2 += delta * (v - mean)
		}
		scale := math.Sqrt(math.Max(0, m2/float64(len(rows))))
		if normalScale {
			scale = math.Max(scale, .05*math.Max(math.Abs(mean), 1))
		} else if scale == 0 {
			scale = 1
		}
		if math.IsNaN(mean) || math.IsInf(mean, 0) || math.IsNaN(scale) || math.IsInf(scale, 0) {
			return t, fmt.Errorf("training numeric overflow for %s", name)
		}
		t.FeatureNames = append(t.FeatureNames, name)
		t.Medians = append(t.Medians, median)
		t.Means = append(t.Means, mean)
		t.Scales = append(t.Scales, scale)
	}
	observedResource := false
	for _, name := range t.FeatureNames {
		if !strings.HasSuffix(name, "_missing_ratio") {
			observedResource = true
		}
	}
	if !observedResource {
		return t, fmt.Errorf("no observed resource features in training runs")
	}
	return t, nil
}

func (t Transformer) apply(row dataset.WindowRow) ([]float64, error) {
	x := make([]float64, len(t.FeatureNames))
	for i, name := range t.FeatureNames {
		v, ok := row.Features[name]
		if !ok {
			v = t.Medians[i]
		}
		x[i] = (v - t.Means[i]) / t.Scales[i]
		if math.IsNaN(x[i]) || math.IsInf(x[i], 0) {
			return nil, fmt.Errorf("numeric overflow transforming %s in run %s", name, row.RunID)
		}
	}
	return x, nil
}

func classOf(row dataset.WindowRow) string {
	if row.Label == "normal" {
		return "normal"
	}
	return row.Label + ":" + row.Target
}
func binaryTruth(row dataset.WindowRow) string {
	if row.Label == "normal" {
		return "normal"
	}
	return "fault"
}

func centroids(rows []dataset.WindowRow, t Transformer) (map[string][]float64, error) {
	centres := map[string][]float64{}
	counts := map[string]int{}
	for _, row := range rows {
		x, err := t.apply(row)
		if err != nil {
			return nil, err
		}
		class := classOf(row)
		counts[class]++
		if centres[class] == nil {
			centres[class] = make([]float64, len(x))
		}
		for i, v := range x {
			centres[class][i] += (v - centres[class][i]) / float64(counts[class])
		}
	}
	return centres, nil
}

func closest(x []float64, centres map[string][]float64, labels []string) (string, error) {
	best, prediction := math.Inf(1), ""
	for _, label := range labels {
		distance := 0.0
		for i, v := range x {
			distance = math.Hypot(distance, v-centres[label][i])
		}
		if math.IsInf(distance, 0) || math.IsNaN(distance) {
			return "", fmt.Errorf("numeric overflow calculating centroid distance")
		}
		if distance < best {
			best, prediction = distance, label
		}
	}
	return prediction, nil
}
