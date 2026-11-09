package evaluation

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/lazypanic999/CorePulse/internal/dataset"
	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

func declaredFixture(t *testing.T, oneSession bool) (string, string, string) {
	t.Helper()
	input, split, out, _ := fixture(t)
	rows := readRows(t, input)
	for i, row := range rows {
		value := "session_id"
		if i > 0 {
			value = "declared-" + row[0][1:]
			if oneSession {
				value = "only-session"
			}
		}
		updated := append([]string(nil), row[:11]...)
		updated = append(updated, value)
		updated = append(updated, row[11:]...)
		rows[i] = updated
	}
	writeRows(t, input, rows)
	return input, split, out
}

func TestSessionEvaluationVerifiesSourceGroupsAndSavedSnapshot(t *testing.T) {
	input, split, out := declaredFixture(t, false)
	m, err := dataset.Split(input, split, dataset.SplitOptions{GroupBy: "session", TestFraction: .25, Seed: 42, RequireProtocol: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.SchemaVersion != 2 || r.Split.Grouping != "session" || r.TrainWindows != 4 || r.TestWindows != 4 {
		t.Fatalf("wrong grouped report: %+v", r)
	}
	if err := ValidateSnapshot(r); err != nil {
		t.Fatal(err)
	}
	for _, model := range r.Models {
		if model.Transformer != nil && slices.Contains(model.Transformer.FeatureNames, "session_id") {
			t.Fatal("session metadata leaked into fitting")
		}
	}
	bytes, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var loaded Report
	if err := json.Unmarshal(bytes, &loaded); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshot(loaded); err != nil {
		t.Fatalf("grouped saved snapshot rejected: %v", err)
	}
	before, _ := os.ReadFile(out)
	for _, mode := range []string{"overlap", "forged", "missing", "schema"} {
		t.Run(mode, func(t *testing.T) {
			data, _ := json.Marshal(m)
			var changed dataset.SplitManifest
			json.Unmarshal(data, &changed)
			switch mode {
			case "overlap":
				changed.TestSessions = append(changed.TestSessions, changed.TrainSessions[0])
			case "forged":
				changed.SessionAssignments[changed.TrainRuns[0]] = changed.TestSessions[0]
			case "missing":
				delete(changed.SessionAssignments, changed.TrainRuns[0])
			case "schema":
				changed.SchemaVersion = 1
			}
			if err := runrecord.Save(split, changed); err != nil {
				t.Fatal(err)
			}
			if _, err := Evaluate(input, split, out, Options{Threshold: 3}); err == nil {
				t.Fatal("forged grouping accepted")
			}
			if after, _ := os.ReadFile(out); string(after) != string(before) {
				t.Fatal("invalid grouping overwrote valid report")
			}
			copy := r
			copy.Split = changed
			copy.SchemaVersion = changed.SchemaVersion
			if err := ValidateSnapshot(copy); err == nil {
				t.Fatal("forged saved grouping displayed")
			}
		})
	}
}

func TestPerfectWithinSessionScoreCannotClaimCrossSessionValidation(t *testing.T) {
	input, split, out := declaredFixture(t, true)
	m, err := dataset.Split(input, split, dataset.SplitOptions{TestFraction: .25, Seed: 42, RequireProtocol: true})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Evaluate(input, split, out, Options{Threshold: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r.Models["nearest_centroid"].WindowMetrics.Accuracy != 1 || r.SchemaVersion != 1 || r.Split.Grouping != "" {
		t.Fatal("fixture must have perfect within-session run scores, not session validation")
	}
	if _, err := dataset.Split(input, split, dataset.SplitOptions{GroupBy: "session", TestFraction: .25, Seed: 42, RequireProtocol: true}); err == nil || !strings.Contains(err.Error(), "at least two") {
		t.Fatalf("one-session shortcut appeared cross-session: %v", err)
	}
	// Simply relabelling an otherwise high-scoring run report cannot turn it
	// into a valid session study: the session would straddle both roles.
	m.SchemaVersion, m.Grouping = 2, "session"
	m.TrainSessions, m.TestSessions = []string{"only-session"}, []string{"only-session"}
	m.SessionAssignments = map[string]string{}
	for _, id := range append(append([]string(nil), m.TrainRuns...), m.TestRuns...) {
		m.SessionAssignments[id] = "only-session"
	}
	if err := runrecord.Save(split, m); err != nil {
		t.Fatal(err)
	}
	if _, err := Evaluate(input, split, out, Options{Threshold: 3}); err == nil {
		t.Fatal("run score relabelled as cross-session validation")
	}
	r.SchemaVersion, r.Split = 2, m
	if err := ValidateSnapshot(r); err == nil {
		t.Fatal("dashboard accepted a false cross-session claim")
	}
}
