package dataset

import (
	"fmt"
	"time"
)

type runRange struct{ start, end time.Time }

func windowRanges(data WindowData) map[string]runRange {
	ranges := map[string]runRange{}
	for _, row := range data.Rows {
		r, exists := ranges[row.RunID]
		if !exists || row.WindowStart.Before(r.start) {
			r.start = row.WindowStart
		}
		if !exists || row.WindowEnd.After(r.end) {
			r.end = row.WindowEnd
		}
		ranges[row.RunID] = r
	}
	return ranges
}

func near(train, test runRange, embargo time.Duration) bool {
	return train.start.Before(test.end.Add(embargo)) && train.end.After(test.start.Add(-embargo))
}

// VerifyTemporalSeparation rechecks actual retained run ranges, rather than
// trusting a declared embargo in a manually edited split manifest.
func VerifyTemporalSeparation(data WindowData, trainIDs, testIDs []string, embargo time.Duration) error {
	if embargo == 0 {
		return nil
	}
	if embargo < 0 || embargo > 24*time.Hour {
		return fmt.Errorf("invalid embargo duration")
	}
	ranges := windowRanges(data)
	for _, train := range trainIDs {
		for _, test := range testIDs {
			a, okA := ranges[train]
			b, okB := ranges[test]
			if !okA || !okB {
				return fmt.Errorf("temporal split references an unknown run")
			}
			if near(a, b, embargo) {
				return fmt.Errorf("training run %s violates the time embargo around held-out run %s", train, test)
			}
		}
	}
	return nil
}
