package evaluation

import (
	"fmt"
	"math"
	"slices"
	"sort"
)

// RateSpread uses population standard deviation across every declared seed.
type RateSpread struct {
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
}

type SeedSummary struct {
	Task          string     `json:"task"`
	Seeds         []int64    `json:"seeds"`
	Methods       []string   `json:"methods"`
	WindowMacroF1 RateSpread `json:"window_macro_f1"`
	RunMacroF1    RateSpread `json:"run_macro_f1"`
	Coverage      RateSpread `json:"coverage"`
}

func rateSpread(values []float64) RateSpread {
	r := RateSpread{Min: values[0], Max: values[0]}
	for _, v := range values {
		r.Mean += v / float64(len(values))
		r.Min = min(r.Min, v)
		r.Max = max(r.Max, v)
	}
	for _, v := range values {
		r.Stddev += (v - r.Mean) * (v - r.Mean) / float64(len(values))
	}
	r.Stddev = math.Sqrt(r.Stddev)
	return r
}

func equalSpread(a, b RateSpread) bool {
	return sameRate(a.Mean, b.Mean) && sameRate(a.Stddev, b.Stddev) && sameRate(a.Min, b.Min) && sameRate(a.Max, b.Max)
}

func validateSeedSpread(r Report) error {
	groups := map[string][]string{}
	for name, model := range r.Models {
		if model.Family == "" && model.Seed == nil {
			continue
		}
		if !methodName.MatchString(model.Family) || model.Seed == nil || *model.Seed < 0 || name == "nearest_centroid" || name == "majority_scenario" || name == "normal_zscore" || name == "probe_rule" {
			return fmt.Errorf("invalid seed/family for method %s", name)
		}
		groups[model.Family] = append(groups[model.Family], name)
	}
	if len(groups) != len(r.SeedSpread) {
		return fmt.Errorf("seed summaries do not match declared families")
	}
	for family, names := range groups {
		sort.Slice(names, func(i, j int) bool { return *r.Models[names[i]].Seed < *r.Models[names[j]].Seed })
		seeds := make([]int64, 0, len(names))
		windows, runs, coverage := []float64{}, []float64{}, []float64{}
		task := r.Models[names[0]].Task
		for i, name := range names {
			m := r.Models[name]
			if m.Task != task || i > 0 && *m.Seed == seeds[i-1] {
				return fmt.Errorf("mixed tasks or duplicate seed within family %s", family)
			}
			seeds = append(seeds, *m.Seed)
			windows = append(windows, m.WindowMetrics.MacroF1)
			runs = append(runs, m.RunMetrics.MacroF1)
			coverage = append(coverage, m.WindowMetrics.Coverage)
		}
		s, ok := r.SeedSpread[family]
		if !ok || s.Task != task || !slices.Equal(s.Seeds, seeds) || !slices.Equal(s.Methods, names) || !equalSpread(s.WindowMacroF1, rateSpread(windows)) || !equalSpread(s.RunMacroF1, rateSpread(runs)) || !equalSpread(s.Coverage, rateSpread(coverage)) {
			return fmt.Errorf("seed summary disagrees with method results in %s", family)
		}
	}
	return nil
}
