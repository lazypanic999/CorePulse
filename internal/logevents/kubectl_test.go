package logevents

import (
	"strings"
	"testing"
	"time"
)

// Fixtures in testdata/kubectl are real "kubectl logs --timestamps" output
// from the CorePulse lab on 7-8 November 2026, with ANSI colours intact and
// subscriber identity lines removed.

func runtimeOpts() Options { return Options{RuntimeTimestamps: true} }

func TestKubectlSMFLogAfterUPFReplacement(t *testing.T) {
	result := parseFixture(t, "kubectl/smf-upf-crash.log", Source{Pod: "open5gs-smf", Format: FormatOpen5GS}, runtimeOpts())
	checkEvents(t, result.Events, []want{
		{3, PFCPAssociated, "2026-11-07T23:59:29.45476942Z", true},
		{6, PFCPHeartbeatTimeout, "2026-11-08T00:04:56.030163616Z", true},
		{7, PFCPNoUPF, "2026-11-08T00:04:56.030168167Z", true},
		{8, PFCPDeassociated, "2026-11-08T00:04:56.030171535Z", true},
		{10, PFCPRetryFailed, "2026-11-08T00:05:07.031463107Z", true},
		{11, PFCPRetryFailed, "2026-11-08T00:05:18.034947335Z", true},
	})
	for _, e := range result.Events {
		if e.TimestampSource != "runtime" || e.Component != "smf" {
			t.Fatalf("unexpected provenance: %+v", e)
		}
	}
	// PFCP lines keep only the peer address; "No UPF available" names none.
	peers := []string{}
	for _, e := range result.Events {
		peers = append(peers, e.Peer)
	}
	if strings.Join(peers, ",") != "10.244.0.209:8805,10.244.0.209:8805,,10.244.0.209:8805,10.244.0.209:8805,10.244.0.209:8805" {
		t.Fatalf("unexpected peers: %q", peers)
	}
	// The [pfcp] library lines are timestamped records without an SMF marker.
	if result.TimestampedLines != 10 || len(result.Skipped) != 0 {
		t.Fatalf("timestamped=%d skipped=%+v", result.TimestampedLines, result.Skipped)
	}
}

func TestKubectlReplacementUPFNeverAssociates(t *testing.T) {
	result := parseFixture(t, "kubectl/upf-replacement.log", Source{Pod: "open5gs-upf", Format: FormatOpen5GS}, runtimeOpts())
	if len(result.Events) != 0 {
		t.Fatalf("replacement UPF logged no PFCP association, got %+v", result.Events)
	}
	// The window shows what was read, so "no association in this window" is
	// distinguishable from "log not read".
	if result.Window == nil || result.TimestampedLines != 4 {
		t.Fatalf("window=%+v timestamped=%d", result.Window, result.TimestampedLines)
	}
}

func TestKubectlUERegistration(t *testing.T) {
	result := parseFixture(t, "kubectl/ue-registered.log", Source{Component: "ue", Pod: "ueransim-ue", Format: FormatUERANSIM}, runtimeOpts())
	if len(result.Events) != 2 || result.Events[0].Kind != RegistrationSuccess || result.Events[1].Kind != PDUSessionSuccess {
		t.Fatalf("unexpected UE events: %+v", result.Events)
	}
	if got := result.Events[0].Timestamp.Format(time.RFC3339Nano); got != "2026-11-07T23:59:33.362510876Z" {
		t.Fatalf("runtime timestamp not used: %s", got)
	}
	// "Registration accept received" is a debug line before the success line;
	// it must not count as a second registration.
	if result.TimestampedLines != 27 {
		t.Fatalf("timestamped lines = %d", result.TimestampedLines)
	}
}

func TestKubectlGNBLosesN2AfterAMFReplacement(t *testing.T) {
	result := parseFixture(t, "kubectl/gnb-amf-crash.log", Source{Component: "gnb", Pod: "ueransim-gnb", Format: FormatUERANSIM}, runtimeOpts())
	checkEvents(t, result.Events, []want{
		{7, NGSetupSuccess, "2026-11-08T00:09:17.921914637Z", true},
		{21, N2AssociationTerminated, "2026-11-08T00:10:54.555786247Z", true},
	})
	// No second NG Setup follows in the captured window: the gNB stayed
	// without N2 even though the UE's packets kept reaching the UPF.
	last, _ := time.Parse(time.RFC3339Nano, "2026-11-08T00:10:54.555794659Z")
	if result.Window == nil || !result.Window.Last.Equal(last) {
		t.Fatalf("window = %+v", result.Window)
	}
}

func TestUEMarkersIgnoreGNBComponent(t *testing.T) {
	line := "2026-11-08T00:10:54.555786247Z [2026-11-08 00:10:54.555] [ngap] [error] Association terminated for AMF[2]\n"
	result, err := Parse(strings.NewReader(line), Source{Component: "ue", Format: FormatUERANSIM}, runtimeOpts())
	if err != nil || len(result.Events) != 0 {
		t.Fatalf("gNB marker matched a UE source: %+v %v", result.Events, err)
	}
}

func TestRuntimeModeRejectsLinesWithoutPrefix(t *testing.T) {
	logs := "[2026-11-07 23:59:33.362] [nas] [info] Initial Registration is successful\n" +
		"2026-11-07T23:59:33.599300021Z [2026-11-07 23:59:33.599] [nas] [info] PDU Session establishment is successful PSI[1]\n" +
		"2026-13-07T23:59:33Z [2026-11-07 23:59:33.599] [nas] [info] PDU Session establishment is successful PSI[1]\n" +
		"\n"
	result, err := Parse(strings.NewReader(logs), Source{Component: "ue", Format: FormatUERANSIM}, runtimeOpts())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.Events[0].Line != 2 {
		t.Fatalf("unexpected events: %+v", result.Events)
	}
	if len(result.Skipped) != 2 || result.Skipped[0].Reason != "missing runtime timestamp" || result.Skipped[1].Reason != "invalid runtime timestamp" {
		t.Fatalf("unexpected skipped: %+v", result.Skipped)
	}
}

func TestRuntimeModeNeedsNoZoneOrReference(t *testing.T) {
	if _, err := Parse(strings.NewReader(""), Source{Format: FormatOpen5GS}, runtimeOpts()); err != nil {
		t.Fatalf("runtime mode should not need Location or Reference: %v", err)
	}
}
