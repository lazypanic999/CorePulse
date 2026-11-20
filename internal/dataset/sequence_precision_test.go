package dataset

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSequenceFractionalStepsRoundTripWithoutLosingNanoseconds(t *testing.T) {
	for _, step := range []time.Duration{time.Second + 7*time.Nanosecond, 256238704302, 2103571584039} {
		t.Run(step.String(), func(t *testing.T) {
			raw, runs, out := sequenceFixture(t)
			if _, err := BuildSequences(raw, runs, out, SequenceOptions{Step: step, RequireProtocol: true}); err != nil {
				t.Fatal(err)
			}
			if _, err := ReadSequences(out); err != nil {
				t.Fatalf("valid %s export rejected by its reader: %v", step, err)
			}
		})
	}
}

func TestSequenceSplitRejectsSubNanosecondEmbargo(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join("..", "..", "examples", "offline", "telemetry.jsonl")
	runs := filepath.Join("..", "..", "examples", "offline", "run-records")
	out := filepath.Join(dir, "sequences.csv")
	if _, err := BuildSequences(raw, runs, out, SequenceOptions{Step: 5 * time.Second, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	data, err := ReadSequences(out)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Split(out, filepath.Join(dir, "split.json"), SplitOptions{GroupBy: "session", TestFraction: .25, Seed: 42, RequireProtocol: true})
	if err != nil {
		t.Fatal(err)
	}
	m.EmbargoSeconds = 1.000000007
	if err := ValidateSequenceSplit(data, m, false); err != nil {
		t.Fatal(err)
	}
	m.EmbargoSeconds = 1e-10
	if ValidateSequenceSplit(data, m, false) == nil {
		t.Fatal("sub-nanosecond embargo silently became zero")
	}
}
