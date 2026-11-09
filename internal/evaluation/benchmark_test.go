package evaluation

import (
	"strings"
	"testing"

	"github.com/lazypanic999/CorePulse/internal/dataset"
)

// A fixed synthetic batch measures implementation cost, not model accuracy.
func BenchmarkResourceReferenceBatch(b *testing.B) {
	train := make([]dataset.WindowRow, 1000)
	for i := range train {
		label, target := "normal", ""
		if i%3 == 1 {
			label, target = "pod_crash", "amf"
		}
		if i%3 == 2 {
			label, target = "pod_crash", "upf"
		}
		values := map[string]float64{}
		for j, name := range resourceFeatures {
			values[name] = float64((i + j) % 20)
			if strings.HasSuffix(name, "_missing_ratio") {
				values[name] = float64(i%3) / 2
			}
		}
		train[i] = dataset.WindowRow{Label: label, Target: target, Features: values}
	}
	labels := []string{"normal", "pod_crash:amf", "pod_crash:upf"}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		transform, err := fitTransform(train, false)
		if err != nil {
			b.Fatal(err)
		}
		centres, err := centroids(train, transform)
		if err != nil {
			b.Fatal(err)
		}
		for _, row := range train[:100] {
			x, err := transform.apply(row)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := closest(x, centres, labels); err != nil {
				b.Fatal(err)
			}
		}
	}
}
