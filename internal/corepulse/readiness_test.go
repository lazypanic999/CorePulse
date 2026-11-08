package corepulse

import (
	"strings"
	"testing"
)

func TestProtocolReadinessRequiresBothLinks(t *testing.T) {
	const setup = "2026-11-09T01:00:00Z [2026-11-09 01:00:00.000] [ngap] [info] NG Setup procedure is successful\n"
	const lost = "2026-11-09T01:01:00Z [2026-11-09 01:01:00.000] [ngap] [error] Association terminated for AMF[2]\n"
	const associated = "2026-11-09T01:00:00Z 11/09 01:00:00.000: [smf] INFO: PFCP associated [10.244.0.211]:8805\n"
	const retry = "2026-11-09T01:01:00Z 11/09 01:01:00.000: [smf] WARNING: Retry association with peer failed [10.244.0.209]:8805\n"
	const deassociated = "2026-11-09T01:01:00Z 11/09 01:01:00.000: [smf] INFO: PFCP de-associated [10.244.0.211]:8805\n"
	for _, tc := range []struct {
		name, gnb, smf, n2, n4 string
		refuse                 bool
	}{
		{"healthy", setup, associated, LinkUp, LinkUp, false},
		{"N2 terminated despite packet forwarding", setup + lost, associated, LinkDown, LinkUp, true},
		{"stale PFCP peer", setup, associated + retry, LinkUp, LinkStalePeer, true},
		{"PFCP down", setup, associated + deassociated, LinkUp, LinkDown, true},
		{"N2 not observed", "", associated, LinkUnknown, LinkUp, true},
		{"PFCP not observed", setup, "", LinkUp, LinkUnknown, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &eventsClient{context: LabContext, logs: map[string]string{"ueransim-gnb-pod": tc.gnb, "open5gs-smf-pod": tc.smf}}
			check, err := CheckProtocolReadiness(client)
			if (err != nil) != tc.refuse || check.Links.N2 != tc.n2 || check.Links.N4 != tc.n4 || len(check.Evidence) != 3 {
				t.Fatalf("check=%+v err=%v", check, err)
			}
			if tc.refuse && !strings.Contains(err.Error(), "recover-service.sh") {
				t.Fatal(err)
			}
			if !tc.refuse && (check.Evidence[0].Pod == "" || check.Evidence[0].LastEventAt == nil) {
				t.Fatalf("missing audit evidence: %+v", check)
			}
		})
	}
}

func TestProtocolReadinessNeverReadsOutsideLab(t *testing.T) {
	c := &eventsClient{context: "production"}
	if _, err := CheckProtocolReadiness(c); err == nil || len(c.logArgs) != 0 {
		t.Fatalf("err=%v reads=%v", err, c.logArgs)
	}
}
