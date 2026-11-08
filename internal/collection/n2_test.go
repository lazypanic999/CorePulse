package collection

import (
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/logevents"
)

// Times follow AMF run 141b3adb on 8 November 2026.
var n2Start = time.Date(2026, 11, 8, 0, 10, 8, 0, time.UTC)

func gnb(first, collected time.Time, errText string, events ...logevents.Event) []corepulse.ComponentEvents {
	window := &logevents.Window{First: first, Last: collected}
	if errText != "" {
		window = nil
	}
	return []corepulse.ComponentEvents{
		{Deployment: "open5gs-smf", Window: &logevents.Window{First: first}, CollectedAt: collected},
		{Deployment: "ueransim-gnb", Pod: "ueransim-gnb-1", Window: window, CollectedAt: collected, Error: errText, Events: events},
	}
}

func n2Event(kind logevents.Kind, at time.Time) logevents.Event {
	return logevents.Event{Kind: kind, Timestamp: at, Component: "gnb"}
}

func TestControlPlaneRecoveryStatuses(t *testing.T) {
	setup := n2Start.Add(-51 * time.Second)
	terminated := n2Start.Add(46 * time.Second)
	collected := n2Start.Add(100 * time.Second)
	cases := []struct {
		name   string
		in     []corepulse.ComponentEvents
		status string
	}{
		{"lost, like run 141b3adb", gnb(setup, collected, "", n2Event(logevents.NGSetupSuccess, setup), n2Event(logevents.N2AssociationTerminated, terminated)), "n2_lost"},
		{"restored after termination", gnb(setup, collected, "", n2Event(logevents.N2AssociationTerminated, terminated), n2Event(logevents.NGSetupSuccess, terminated.Add(5*time.Second))), "n2_restored"},
		{"setup before the run does not count as restored", gnb(setup, collected, "", n2Event(logevents.NGSetupSuccess, setup)), "n2_no_change_observed"},
		{"lost again after a restore", gnb(setup, collected, "", n2Event(logevents.N2AssociationTerminated, terminated), n2Event(logevents.NGSetupSuccess, terminated.Add(time.Second)), n2Event(logevents.N2AssociationTerminated, terminated.Add(2*time.Second))), "n2_lost"},
		{"gNB log unreadable", gnb(setup, collected, "target pod is not ready"), "n2_unobserved"},
		{"gNB log starts after the run", gnb(n2Start.Add(time.Second), collected, ""), "n2_unobserved"},
		{"no gNB component", nil, "n2_unobserved"},
	}
	for _, tc := range cases {
		got := controlPlaneRecovery(tc.in, n2Start)
		if got.Status != tc.status {
			t.Errorf("%s: status %q, want %q (%+v)", tc.name, got.Status, tc.status, got)
		}
	}
	lost := controlPlaneRecovery(cases[0].in, n2Start)
	if lost.TerminatedAt == nil || !lost.TerminatedAt.Equal(terminated) || lost.RestoredAt != nil || lost.GNBPod != "ueransim-gnb-1" || lost.ObservedUntil == nil {
		t.Fatalf("lost result lacks evidence: %+v", lost)
	}
}

func TestN2SettleDelayOnlyForAMF(t *testing.T) {
	started := n2Start.Format(time.RFC3339Nano)
	if d := n2SettleDelay("amf", started, n2Start.Add(30*time.Second)); d != 60*time.Second {
		t.Fatalf("amf delay = %v, want 60s", d)
	}
	if d := n2SettleDelay("amf", started, n2Start.Add(2*time.Minute)); d != 0 {
		t.Fatalf("no wait needed after settle time, got %v", d)
	}
	if d := n2SettleDelay("upf", started, n2Start); d != 0 {
		t.Fatalf("upf must not wait, got %v", d)
	}
	if d := n2SettleDelay("amf", "not a time", n2Start); d != 0 {
		t.Fatalf("unparseable start must not wait, got %v", d)
	}
}
