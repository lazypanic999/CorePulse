package dataset

import (
	"fmt"
	"math"
	"sort"

	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

func splitSessions(data WindowData, m SplitManifest, options SplitOptions) (SplitManifest, error) {
	m.SchemaVersion, m.Grouping = 2, "session"
	m.SessionAssignments = map[string]string{}
	classes, sessionSet, runClasses := map[string]map[string]bool{}, map[string]bool{}, map[string]string{}
	for _, row := range data.Rows {
		if options.RequireProtocol && !row.ProtocolVerified {
			return m, fmt.Errorf("run %s lacks verified protocol starting conditions", row.RunID)
		}
		if row.SessionID == "" {
			return m, fmt.Errorf("run %s has unknown session provenance; collect explicit sessions rather than guessing legacy identities", row.RunID)
		}
		if _, seen := m.SessionAssignments[row.RunID]; seen {
			continue
		}
		class, err := scenario(row.Label, row.Target)
		if err != nil {
			return m, err
		}
		m.SessionAssignments[row.RunID] = row.SessionID
		runClasses[row.RunID] = class
		sessionSet[row.SessionID] = true
		if classes[class] == nil {
			classes[class] = map[string]bool{}
		}
		classes[class][row.SessionID] = true
	}
	if len(classes) == 0 {
		return m, fmt.Errorf("cannot split an empty dataset")
	}
	for class, sessions := range classes {
		if len(sessions) < 2 {
			return m, fmt.Errorf("class %s needs at least two declared collection sessions, found %d; additional runs in one session do not satisfy this", class, len(sessions))
		}
	}
	sessions := make([]string, 0, len(sessionSet))
	for id := range sessionSet {
		sessions = append(sessions, id)
	}
	sort.Slice(sessions, func(i, j int) bool {
		return splitKey(options.Seed, "session/"+sessions[i]) < splitKey(options.Seed, "session/"+sessions[j])
	})
	n := int(math.Ceil(float64(len(sessions)) * options.TestFraction))
	if n >= len(sessions) {
		n = len(sessions) - 1
	}
	m.TestSessions = append([]string(nil), sessions[:n]...)
	m.TrainSessions = append([]string(nil), sessions[n:]...)
	testSet := map[string]bool{}
	for _, id := range m.TestSessions {
		testSet[id] = true
	}
	for run, session := range m.SessionAssignments {
		if testSet[session] {
			m.TestRuns = append(m.TestRuns, run)
		} else {
			m.TrainRuns = append(m.TrainRuns, run)
		}
	}
	if options.Embargo > 0 {
		ranges := windowRanges(data)
		purgedSessions := map[string]bool{}
		for _, train := range m.TrainRuns {
			for _, test := range m.TestRuns {
				if near(ranges[train], ranges[test], options.Embargo) {
					purgedSessions[m.SessionAssignments[train]] = true
					break
				}
			}
		}
		kept := []string{}
		for _, run := range m.TrainRuns {
			if purgedSessions[m.SessionAssignments[run]] {
				m.PurgedRuns = append(m.PurgedRuns, run)
			} else {
				kept = append(kept, run)
			}
		}
		m.TrainRuns = kept
		kept = []string{}
		for _, id := range m.TrainSessions {
			if purgedSessions[id] {
				m.PurgedSessions = append(m.PurgedSessions, id)
			} else {
				kept = append(kept, id)
			}
		}
		m.TrainSessions = kept
	}
	for class := range classes {
		m.Classes[class] = ClassSplit{}
	}
	for _, run := range m.TrainRuns {
		c := m.Classes[runClasses[run]]
		c.TrainRuns++
		m.Classes[runClasses[run]] = c
	}
	for _, run := range m.TestRuns {
		c := m.Classes[runClasses[run]]
		c.TestRuns++
		m.Classes[runClasses[run]] = c
	}
	for class, c := range m.Classes {
		if c.TrainRuns < 1 || c.TestRuns < 1 {
			return m, fmt.Errorf("session holdout/embargo leaves class %s without train/test support; collect broader separated sessions without tuning against test scores", class)
		}
	}
	for _, ids := range [][]string{m.TrainRuns, m.TestRuns, m.PurgedRuns, m.TrainSessions, m.TestSessions, m.PurgedSessions} {
		sort.Strings(ids)
	}
	if err := VerifySessionAssignments(data, m); err != nil {
		return m, err
	}
	return m, nil
}

// ValidateGrouping checks declared group roles without authenticating the input
// dataset. Schema one is whole-run; schema two explicitly binds whole sessions.
func ValidateGrouping(m SplitManifest) error {
	if m.SchemaVersion == 1 {
		if (m.Grouping != "" && m.Grouping != "run") || len(m.SessionAssignments) != 0 || len(m.TrainSessions)+len(m.TestSessions)+len(m.PurgedSessions) != 0 {
			return fmt.Errorf("run split cannot claim session grouping metadata")
		}
		return nil
	}
	if m.SchemaVersion != 2 || m.Grouping != "session" {
		return fmt.Errorf("unsupported split grouping/schema")
	}
	if len(m.TrainSessions) == 0 || len(m.TestSessions) == 0 {
		return fmt.Errorf("session split requires retained and held-out sessions")
	}
	declared := map[string]string{}
	for role, ids := range map[string][]string{"train": m.TrainSessions, "test": m.TestSessions, "purged": m.PurgedSessions} {
		for _, id := range ids {
			if id == "" || runrecord.ValidateSessionID(id) != nil || declared[id] != "" {
				return fmt.Errorf("duplicate, invalid or overlapping session identity")
			}
			declared[id] = role
		}
	}
	seenRuns, seenSessions := map[string]bool{}, map[string]bool{}
	for role, ids := range map[string][]string{"train": m.TrainRuns, "test": m.TestRuns, "purged": m.PurgedRuns} {
		for _, id := range ids {
			if id == "" || seenRuns[id] {
				return fmt.Errorf("duplicate or overlapping grouped run identity")
			}
			session := m.SessionAssignments[id]
			if session == "" || declared[session] != role {
				return fmt.Errorf("run %s crosses or lacks declared session ownership", id)
			}
			seenRuns[id], seenSessions[session] = true, true
		}
	}
	if len(seenRuns) != len(m.SessionAssignments) || len(seenSessions) != len(declared) {
		return fmt.Errorf("session assignments do not match all split roles")
	}
	return nil
}

// VerifySessionAssignments binds group declarations to actual CSV metadata.
// Unknown legacy sessions never become independent groups by inference.
func VerifySessionAssignments(data WindowData, m SplitManifest) error {
	if err := ValidateGrouping(m); err != nil {
		return err
	}
	if m.SchemaVersion == 1 {
		return nil
	}
	seen := map[string]bool{}
	for _, row := range data.Rows {
		if row.SessionID == "" || m.SessionAssignments[row.RunID] != row.SessionID {
			return fmt.Errorf("session assignment for run %s differs from CSV provenance", row.RunID)
		}
		seen[row.RunID] = true
	}
	if len(seen) != len(m.SessionAssignments) {
		return fmt.Errorf("session assignment coverage differs from CSV")
	}
	return nil
}
