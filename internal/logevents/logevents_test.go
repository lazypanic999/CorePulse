package logevents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func parseFixture(t *testing.T, name string, src Source, opts Options) Result {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	result, err := Parse(f, src, opts)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type want struct {
	line int
	kind Kind
	ts   string
	live bool
}

func checkEvents(t *testing.T, got []Event, expected []want) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(expected), got)
	}
	for i, w := range expected {
		e := got[i]
		ts, _ := time.Parse(time.RFC3339Nano, w.ts)
		if e.Line != w.line || e.Kind != w.kind || !e.Timestamp.Equal(ts) || e.MarkerObservedLive != w.live {
			t.Fatalf("event %d = %+v, want %+v", i, e, w)
		}
		if e.Timestamp.Location() != time.UTC {
			t.Fatalf("event %d timestamp not normalized to UTC: %v", i, e.Timestamp)
		}
	}
}

func TestParseUERANSIMFixture(t *testing.T) {
	istanbul := time.FixedZone("+03", 3*60*60)
	src := Source{Component: "ue", Pod: "ueransim-ue-0", Container: "ue", Format: FormatUERANSIM}
	result := parseFixture(t, "ue.log", src, Options{Location: istanbul})

	checkEvents(t, result.Events, []want{
		{3, RegistrationFailure, "2026-11-02T07:15:30.25Z", false},
		{4, RegistrationSuccess, "2026-11-02T07:15:31.004Z", true},
		{5, RegistrationSuccess, "2026-11-02T07:15:31.004Z", true},
		{6, PDUSessionFailure, "2026-11-02T07:15:31.32Z", false},
		{7, PDUSessionSuccess, "2026-11-02T07:15:32.41Z", true},
		{12, RegistrationSuccess, "2026-11-02T07:20:00Z", true},
		{13, RegistrationSuccess, "2026-11-02T07:21:00Z", true}, // ANSI-colored line
	})
	for _, e := range result.Events {
		if e.Component != "ue" || e.Source != src {
			t.Fatalf("missing provenance: %+v", e)
		}
	}
	skipped := map[int]string{}
	for _, s := range result.Skipped {
		skipped[s.Line] = s.Reason
	}
	if skipped[9] != "invalid timestamp" || skipped[10] != "malformed ueransim line" || len(skipped) != 2 {
		t.Fatalf("unexpected skipped lines: %+v", result.Skipped)
	}
	if result.TimestampedLines != 9 {
		t.Fatalf("timestamped lines = %d, want 9", result.TimestampedLines)
	}
	first, _ := time.Parse(time.RFC3339Nano, "2026-11-02T07:15:30.101Z")
	last, _ := time.Parse(time.RFC3339Nano, "2026-11-02T07:21:00Z")
	if result.Window == nil || !result.Window.First.Equal(first) || !result.Window.Last.Equal(last) {
		t.Fatalf("window = %+v", result.Window)
	}
}

func TestParseOpen5GSFixture(t *testing.T) {
	src := Source{Pod: "open5gs-upf-abc", Format: FormatOpen5GS}
	ref := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	result := parseFixture(t, "upf.log", src, Options{Location: time.UTC, Reference: ref})

	checkEvents(t, result.Events, []want{
		{2, PFCPAssociated, "2026-11-02T10:15:20.5Z", true},
		{3, PFCPHeartbeatTimeout, "2026-11-02T10:18:02.01Z", true},
		{4, PFCPDeassociated, "2026-11-02T10:18:02.011Z", true},
		{5, PFCPAssociated, "2026-11-02T10:18:40.7Z", true},
	})
	for _, e := range result.Events {
		if e.Component != "upf" {
			t.Fatalf("component from log line not used: %+v", e)
		}
	}
	reasons := []string{}
	for _, s := range result.Skipped {
		reasons = append(reasons, s.Reason)
	}
	if strings.Join(reasons, ",") != "invalid timestamp,malformed open5gs line" {
		t.Fatalf("unexpected skipped: %+v", result.Skipped)
	}
	// The AMF line is timestamped but carries no PFCP marker for that component.
	if result.TimestampedLines != 6 {
		t.Fatalf("timestamped lines = %d, want 6", result.TimestampedLines)
	}
}

func TestOpen5GSYearRollover(t *testing.T) {
	logs := "12/31 23:59:00.000: [smf] INFO: PFCP associated (x.c:1)\n" +
		"01/01 00:30:00.000: [smf] WARNING: No Heartbeat from UPF [10.0.0.1]:8805 (x.c:2)\n"
	ref := time.Date(2027, 1, 1, 1, 0, 0, 0, time.UTC)
	result, err := Parse(strings.NewReader(logs), Source{Format: FormatOpen5GS}, Options{Location: time.UTC, Reference: ref})
	if err != nil {
		t.Fatal(err)
	}
	checkEvents(t, result.Events, []want{
		{1, PFCPAssociated, "2026-12-31T23:59:00Z", true},
		{2, PFCPHeartbeatTimeout, "2027-01-01T00:30:00Z", true},
	})
}

func TestNoEventsIsNotHealth(t *testing.T) {
	for _, input := range []string{"", "banner only\n", "[2026-11-02 10:00:00.000] [rrc] [info] RRC connection established\n"} {
		result, err := Parse(strings.NewReader(input), Source{Component: "ue", Format: FormatUERANSIM}, Options{Location: time.UTC})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Events) != 0 {
			t.Fatalf("%q produced events: %+v", input, result.Events)
		}
		if input != "" && strings.HasPrefix(input, "[") {
			if result.Window == nil || result.TimestampedLines != 1 {
				t.Fatalf("window not reported for timestamped input: %+v", result)
			}
		} else if result.Window != nil {
			t.Fatalf("window reported without timestamped lines: %+v", result.Window)
		}
	}
}

func TestParseRequiresExplicitTimeContext(t *testing.T) {
	cases := []struct {
		name string
		src  Source
		opts Options
		err  string
	}{
		{"location", Source{Component: "ue", Format: FormatUERANSIM}, Options{}, "Location"},
		{"component", Source{Format: FormatUERANSIM}, Options{Location: time.UTC}, "Component"},
		{"reference", Source{Format: FormatOpen5GS}, Options{Location: time.UTC}, "Reference"},
		{"format", Source{Format: "journald"}, Options{Location: time.UTC}, "unsupported"},
	}
	for _, tc := range cases {
		_, err := Parse(strings.NewReader(""), tc.src, tc.opts)
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.err)
		}
	}
}

func TestCRLFLines(t *testing.T) {
	logs := "[2026-11-02 10:00:00.000] [nas] [info] PDU Session establishment is successful PSI[1]\r\n"
	result, err := Parse(strings.NewReader(logs), Source{Component: "ue", Format: FormatUERANSIM}, Options{Location: time.UTC})
	if err != nil || len(result.Events) != 1 || result.Events[0].Kind != PDUSessionSuccess {
		t.Fatalf("CRLF line not parsed: %+v %v", result, err)
	}
}

func TestDedupeOverlappingReads(t *testing.T) {
	src := Source{Component: "ue", Pod: "ue-1", Format: FormatUERANSIM}
	result := parseFixture(t, "ue.log", src, Options{Location: time.UTC})
	// Simulate a second, overlapping tail of the same pod log.
	overlap := append(append([]Event{}, result.Events...), result.Events[4:]...)
	deduped := Dedupe(overlap)
	// Line 5 repeats line 4 with an identical timestamp; it is one occurrence.
	if len(deduped) != 6 {
		t.Fatalf("deduped to %d events, want 6: %+v", len(deduped), deduped)
	}
	if deduped[0].Line != 3 || deduped[len(deduped)-1].Line != 13 {
		t.Fatalf("order not preserved: %+v", deduped)
	}
	// Same timestamp from a different pod is a separate observation.
	other := result.Events[1]
	other.Source.Pod = "ue-2"
	if got := Dedupe([]Event{result.Events[1], other}); len(got) != 2 {
		t.Fatalf("events from different pods merged: %+v", got)
	}
}
