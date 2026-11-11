package dataset

import (
	"fmt"
	"math"
	"slices"
	"time"
)

// ValidateSequenceSplit binds an already read sequence CSV to its saved split.
// The field windows_sha256 retains its historical name, but hashes this exact CSV.
func ValidateSequenceSplit(data WindowData, m SplitManifest, allowLegacy bool) error {
	if data.InputKind != SequenceKind || m.InputKind != SequenceKind || data.SHA256 != m.WindowsSHA256 || !slices.Equal(m.FeatureNames, SequenceFeatureNames()) || !slices.Equal(data.FeatureNames, SequenceFeatureNames()) {
		return fmt.Errorf("sequence kind, feature allowlist or source hash mismatch")
	}
	if math.IsNaN(m.TestFraction) || math.IsInf(m.TestFraction, 0) || m.TestFraction <= 0 || m.TestFraction >= 1 || math.IsNaN(m.EmbargoSeconds) || math.IsInf(m.EmbargoSeconds, 0) || m.EmbargoSeconds < 0 || m.EmbargoSeconds > 86400 {
		return fmt.Errorf("invalid sequence split policy")
	}
	embargo := time.Duration(math.Round(m.EmbargoSeconds * float64(time.Second)))
	if embargo.Seconds() != m.EmbargoSeconds {
		return fmt.Errorf("sequence embargo must resolve to whole nanoseconds")
	}
	if err := VerifySessionAssignments(data, m); err != nil {
		return err
	}
	if !allowLegacy && !m.RequireProtocol {
		return fmt.Errorf("legacy sequence split requires explicit opt-in")
	}
	roles := map[string]string{}
	for role, ids := range map[string][]string{"train": m.TrainRuns, "test": m.TestRuns, "purged": m.PurgedRuns} {
		for _, id := range ids {
			if id == "" || roles[id] != "" {
				return fmt.Errorf("empty, duplicate or overlapping split run")
			}
			roles[id] = role
		}
	}
	if len(m.TrainRuns) == 0 || len(m.TestRuns) == 0 {
		return fmt.Errorf("empty sequence train/test partition")
	}
	seen := map[string]bool{}
	counts := map[string]ClassSplit{}
	for _, row := range data.Rows {
		role := roles[row.RunID]
		if role == "" {
			return fmt.Errorf("sequence run is absent from split")
		}
		if (m.RequireProtocol || !allowLegacy) && !row.ProtocolVerified {
			return fmt.Errorf("sequence run lacks verified protocol start")
		}
		if !seen[row.RunID] && role != "purged" {
			class, err := scenario(row.Label, row.Target)
			if err != nil {
				return err
			}
			c := counts[class]
			if role == "train" {
				c.TrainRuns++
			} else {
				c.TestRuns++
			}
			counts[class] = c
		}
		seen[row.RunID] = true
	}
	if len(seen) != len(roles) || len(counts) != len(m.Classes) || len(counts) < 2 {
		return fmt.Errorf("sequence split membership or class support mismatch")
	}
	for class, c := range counts {
		if c.TrainRuns < 1 || c.TestRuns < 1 || m.Classes[class] != c {
			return fmt.Errorf("sequence class split support mismatch")
		}
	}
	return VerifyTemporalSeparation(data, m.TrainRuns, m.TestRuns, embargo)
}
