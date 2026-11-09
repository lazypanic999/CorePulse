package dataset

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/runrecord"
)

func sessionFixture(t *testing.T, count int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "windows.csv")
	rows := [][]string{sessionWindowHeader()}
	for session := 0; session < count; session++ {
		for class, group := range []struct{ label, target string }{{"normal", ""}, {"pod_crash", "amf"}, {"pod_crash", "upf"}} {
			for _, offset := range []int{0, 30} {
				at := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC).Add(time.Duration(session*10+class)*time.Hour + time.Duration(offset)*time.Second)
				row := make([]string, len(rows[0]))
				row[0] = fmt.Sprintf("run-%d-%d", session, class)
				row[1], row[2] = group.label, group.target
				row[3], row[4], row[5], row[6] = at.Format(time.RFC3339Nano), at.Add(30*time.Second).Format(time.RFC3339Nano), at.Add(time.Second).Format(time.RFC3339Nano), at.Add(5*time.Second).Format(time.RFC3339Nano)
				row[7], row[8], row[9], row[10], row[11] = "2", "4", "false", "true", fmt.Sprintf("session-%d", session)
				for i := 12; i < len(row); i++ {
					row[i] = "0"
				}
				rows = append(rows, row)
			}
		}
	}
	writeSessionCSV(t, path, rows)
	return path
}

func writeSessionCSV(t *testing.T, path string, rows [][]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(file)
	if err := w.WriteAll(rows); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
func readSessionCSV(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestWholeSessionSplitBindsGroupsAndCSV(t *testing.T) {
	input := sessionFixture(t, 3)
	output := filepath.Join(t.TempDir(), "split.json")
	options := SplitOptions{GroupBy: "session", Seed: 42, TestFraction: .25, RequireProtocol: true}
	m, err := Split(input, output, options)
	if err != nil {
		t.Fatal(err)
	}
	if m.SchemaVersion != 2 || m.Grouping != "session" || len(m.TrainSessions) != 2 || len(m.TestSessions) != 1 || len(m.TrainRuns) != 6 || len(m.TestRuns) != 3 {
		t.Fatalf("unexpected session support: %+v", m)
	}
	data, err := ReadWindows(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySessionAssignments(data, m); err != nil {
		t.Fatal(err)
	}
	for _, row := range data.Rows {
		if slices.Contains(m.TestRuns, row.RunID) != slices.Contains(m.TestSessions, row.SessionID) {
			t.Fatal("session crossed train/test")
		}
	}
	if slices.Contains(m.FeatureNames, "session_id") {
		t.Fatal("session became a fitted input")
	}
	again, err := Split(input, output, options)
	if err != nil || !reflect.DeepEqual(m, again) {
		t.Fatalf("nondeterministic split: %v", err)
	}
	// Forge another retained session: role-only validation still succeeds,
	// but validation against the exact CSV metadata must reject it.
	run := m.TrainRuns[0]
	for _, id := range m.TrainSessions {
		if id != m.SessionAssignments[run] {
			m.SessionAssignments[run] = id
			break
		}
	}
	if err := ValidateGrouping(m); err != nil {
		t.Fatalf("fixture did not preserve group roles: %v", err)
	}
	if err := VerifySessionAssignments(data, m); err == nil {
		t.Fatal("forged source assignment accepted")
	}
}

func TestUnknownAndInsufficientSessionsNeverOverwriteManifest(t *testing.T) {
	for _, mode := range []string{"unknown", "one-session", "conflicting", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			input := sessionFixture(t, 3)
			rows := readSessionCSV(t, input)
			switch mode {
			case "unknown":
				rows[1][11] = ""
			case "one-session":
				for _, row := range rows[1:] {
					row[11] = "only-one"
				}
			case "conflicting":
				rows[2][11] = "different"
			case "invalid":
				rows[1][11] = "bad session"
			}
			writeSessionCSV(t, input, rows)
			out := filepath.Join(t.TempDir(), "split.json")
			if err := os.WriteFile(out, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Split(input, out, SplitOptions{GroupBy: "session", Seed: 42, TestFraction: .25}); err == nil {
				t.Fatal("unknown/insufficient session provenance accepted")
			}
			if data, _ := os.ReadFile(out); string(data) != "keep" {
				t.Fatal("failed split overwrote previous output")
			}
		})
	}
}

func TestSessionSelectionDoesNotRetuneForMissingClassCoverage(t *testing.T) {
	input := sessionFixture(t, 3)
	out := filepath.Join(t.TempDir(), "split.json")
	options := SplitOptions{GroupBy: "session", Seed: 42, TestFraction: .25, RequireProtocol: true}
	m, err := Split(input, out, options)
	if err != nil {
		t.Fatal(err)
	}
	rows := readSessionCSV(t, input)
	kept := rows[:1]
	for _, row := range rows[1:] {
		if row[11] != m.TestSessions[0] || row[2] != "upf" {
			kept = append(kept, row)
		}
	}
	writeSessionCSV(t, input, kept)
	before, _ := os.ReadFile(out)
	failed, err := Split(input, out, options)
	if err == nil || !strings.Contains(err.Error(), "without train/test support") || !slices.Equal(failed.TestSessions, m.TestSessions) {
		t.Fatalf("session selection was retuned or insufficient support accepted: %+v %v", failed, err)
	}
	if after, _ := os.ReadFile(out); string(after) != string(before) {
		t.Fatal("coverage refusal changed prior manifest")
	}
}

func TestEmbargoPurgesWholeTrainingSession(t *testing.T) {
	for _, count := range []int{2, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			input := sessionFixture(t, count)
			out := filepath.Join(t.TempDir(), "split.json")
			options := SplitOptions{GroupBy: "session", Seed: 42, TestFraction: .25, RequireProtocol: true}
			base, err := Split(input, out, options)
			if err != nil {
				t.Fatal(err)
			}
			rows := readSessionCSV(t, input)
			var testStart time.Time
			for _, row := range rows[1:] {
				if row[0] == base.TestRuns[0] {
					testStart, _ = time.Parse(time.RFC3339Nano, row[3])
					break
				}
			}
			movedRun := base.TrainRuns[0]
			movedSession := base.SessionAssignments[movedRun]
			index := 0
			for _, row := range rows[1:] {
				if row[0] == movedRun {
					at := testStart.Add(-time.Minute + time.Duration(index)*30*time.Second)
					index++
					row[3], row[4], row[5], row[6] = at.Format(time.RFC3339Nano), at.Add(30*time.Second).Format(time.RFC3339Nano), at.Add(time.Second).Format(time.RFC3339Nano), at.Add(5*time.Second).Format(time.RFC3339Nano)
				}
			}
			writeSessionCSV(t, input, rows)
			options.Embargo = 2 * time.Minute
			before, _ := os.ReadFile(out)
			m, err := Split(input, out, options)
			if count == 2 {
				if err == nil {
					t.Fatal("embargo removed all retained sessions but split succeeded")
				}
				if after, _ := os.ReadFile(out); string(after) != string(before) {
					t.Fatal("failed purge overwrote manifest")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(m.TestSessions, base.TestSessions) || !slices.Equal(m.PurgedSessions, []string{movedSession}) || len(m.PurgedRuns) != 3 {
				t.Fatalf("partial-session purge: %+v", m)
			}
			for _, run := range m.TrainRuns {
				if m.SessionAssignments[run] == movedSession {
					t.Fatal("purged session still trains")
				}
			}
			data, err := ReadWindows(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyTemporalSeparation(data, m.TrainRuns, m.TestRuns, options.Embargo); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWindowSessionMetadataIsProspectiveAndLegacyStaysUnknown(t *testing.T) {
	dir := t.TempDir()
	runs, raw, out := filepath.Join(dir, "runs"), filepath.Join(dir, "raw.jsonl"), filepath.Join(dir, "windows.csv")
	start := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	windowRun(t, runs, "new", "normal", "", start, time.Minute, true)
	windowRun(t, runs, "old", "normal", "", start.Add(time.Hour), time.Minute, true)
	path := filepath.Join(runs, "new.json")
	b, err := runrecord.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]any
	if err := json.Unmarshal(b, &record); err != nil {
		t.Fatal(err)
	}
	record["session_id"] = "declared-session"
	if err := runrecord.Save(path, record); err != nil {
		t.Fatal(err)
	}
	for _, stamp := range []time.Time{start.Add(time.Second), start.Add(time.Hour + time.Second)} {
		line := fmt.Sprintf("{\"timestamp\":%q,\"source\":\"metrics.k8s.io/v1beta1\",\"context\":\"open5gs-lab\",\"namespace\":\"open5gs\"}\n", stamp.Format(time.RFC3339Nano))
		file, err := os.OpenFile(raw, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.WriteString(line)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := BuildWindows(raw, runs, out, WindowOptions{Duration: 30 * time.Second, RequireProtocol: true}); err != nil {
		t.Fatal(err)
	}
	data, err := ReadWindows(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Rows) != 2 || data.Rows[0].SessionID != "declared-session" || data.Rows[1].SessionID != "" {
		t.Fatalf("legacy session invented or declaration lost: %+v", data.Rows)
	}
	if slices.Index(readSessionCSV(t, out)[0], "session_id") != 11 {
		t.Fatal("session is not separate metadata")
	}
}
