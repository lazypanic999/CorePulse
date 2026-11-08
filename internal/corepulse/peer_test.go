package corepulse

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/logevents"
)

func smfFixtureEvents(t *testing.T) []logevents.Event {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "logevents", "testdata", "kubectl", "smf-upf-crash.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	result, err := logevents.Parse(f, logevents.Source{Pod: "open5gs-smf", Format: logevents.FormatOpen5GS}, logevents.Options{RuntimeTimestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	return result.Events
}

func TestPFCPPeerMismatchAfterUPFReplacement(t *testing.T) {
	events := smfFixtureEvents(t)
	components := []ComponentEvents{
		{Deployment: "open5gs-smf", Events: events},
		{Deployment: "open5gs-upf", PodIP: "10.244.0.211"},
	}
	got := DetectPFCPPeerMismatch(components)
	if got == nil {
		t.Fatal("expected a mismatch: SMF retries 10.244.0.209 while the UPF pod is 10.244.0.211")
	}
	want, _ := time.Parse(time.RFC3339Nano, "2026-11-08T00:05:18.034947335Z")
	if got.RetryingPeer != "10.244.0.209:8805" || got.UPFPodIP != "10.244.0.211" || !got.LastRetryAt.Equal(want) {
		t.Fatalf("unexpected mismatch: %+v", got)
	}
}

func TestPFCPPeerMismatchNotReported(t *testing.T) {
	events := smfFixtureEvents(t)
	reassociated := append(append([]logevents.Event{}, events...), logevents.Event{Kind: logevents.PFCPAssociated, Peer: "10.244.0.211:8805"})
	cases := []struct {
		name       string
		components []ComponentEvents
	}{
		{"retry names the current UPF pod", []ComponentEvents{{Deployment: "open5gs-smf", Events: events}, {Deployment: "open5gs-upf", PodIP: "10.244.0.209"}}},
		{"SMF re-associated after the retries", []ComponentEvents{{Deployment: "open5gs-smf", Events: reassociated}, {Deployment: "open5gs-upf", PodIP: "10.244.0.211"}}},
		{"UPF pod address unknown", []ComponentEvents{{Deployment: "open5gs-smf", Events: events}, {Deployment: "open5gs-upf"}}},
		{"SMF log unreadable", []ComponentEvents{{Deployment: "open5gs-smf", Error: "not ready"}, {Deployment: "open5gs-upf", PodIP: "10.244.0.211"}}},
		{"healthy lab, no retries", []ComponentEvents{{Deployment: "open5gs-smf", Events: events[:1]}, {Deployment: "open5gs-upf", PodIP: "10.244.0.209"}}},
	}
	for _, tc := range cases {
		if got := DetectPFCPPeerMismatch(tc.components); got != nil {
			t.Errorf("%s: unexpected mismatch %+v", tc.name, got)
		}
	}
}

func TestCollectProtocolEventsRecordsPodIP(t *testing.T) {
	client := &eventsClient{context: LabContext, logs: map[string]string{}}
	results, err := CollectProtocolEvents(client)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range results {
		if c.PodIP != "10.244.0.211" {
			t.Fatalf("%s: pod IP not recorded: %+v", c.Deployment, c)
		}
	}
}
