package dataset

import (
	"sort"
	"strconv"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/logevents"
)

// Evidence columns appended after the resource columns. Empty means not
// observed for that sample; zero means observed and absent.
var evidenceColumns = []string{"probe_status", "probe_sent", "probe_received", "smf_pfcp_failures", "gnb_n2_terminations", "ue_registrations", "ue_pdu_sessions"}

type probeJSON struct {
	Status    string `json:"status"`
	Sent      int    `json:"sent"`
	Received  int    `json:"received"`
	CheckedAt string `json:"checked_at"`
}

type probePoint struct {
	at     time.Time
	status string
	sent   int
	recvd  int
}

type evidence struct {
	probes     []probePoint
	components map[string]corepulse.ComponentEvents
}

// eventCount describes one evidence column built from log events.
type eventCount struct {
	deployment string
	kinds      map[logevents.Kind]bool
}

var eventCounts = []eventCount{
	{"open5gs-smf", map[logevents.Kind]bool{logevents.PFCPHeartbeatTimeout: true, logevents.PFCPNoUPF: true, logevents.PFCPDeassociated: true, logevents.PFCPRetryFailed: true}},
	{"ueransim-gnb", map[logevents.Kind]bool{logevents.N2AssociationTerminated: true}},
	{"ueransim-ue", map[logevents.Kind]bool{logevents.RegistrationSuccess: true}},
	{"ueransim-ue", map[logevents.Kind]bool{logevents.PDUSessionSuccess: true}},
}

func newEvidence(probeLists [][]probeJSON, components []corepulse.ComponentEvents) evidence {
	e := evidence{components: map[string]corepulse.ComponentEvents{}}
	for _, list := range probeLists {
		for _, p := range list {
			at, err := time.Parse(time.RFC3339Nano, p.CheckedAt)
			if err != nil || p.Status == "" {
				continue
			}
			e.probes = append(e.probes, probePoint{at: at, status: p.Status, sent: p.Sent, recvd: p.Received})
		}
	}
	sort.Slice(e.probes, func(i, j int) bool { return e.probes[i].at.Before(e.probes[j].at) })
	for _, c := range components {
		e.components[c.Deployment] = c
	}
	return e
}

// fields returns the evidence column values for one sample of run.
func (e evidence) fields(run Run, at time.Time) []string {
	out := make([]string, 0, len(evidenceColumns))
	var latest *probePoint
	for i := range e.probes {
		if e.probes[i].at.After(at) {
			break
		}
		latest = &e.probes[i]
	}
	if latest == nil {
		out = append(out, "", "", "")
	} else {
		// Local command failures and old degraded 0/0 records do not prove
		// any packets were transmitted. Keep their status, not fake zeros.
		if (latest.status == "healthy" || latest.status == "degraded") && latest.sent > 0 && latest.recvd >= 0 && latest.recvd <= latest.sent {
			out = append(out, latest.status, strconv.Itoa(latest.sent), strconv.Itoa(latest.recvd))
		} else {
			out = append(out, latest.status, "", "")
		}
	}
	for _, column := range eventCounts {
		out = append(out, e.count(column, run, at))
	}
	return out
}

// count is empty unless the component's log was read after the sample and
// already covered the run start, so every matching event in (start, at] was
// visible.
func (e evidence) count(column eventCount, run Run, at time.Time) string {
	c, ok := e.components[column.deployment]
	if !ok || c.Error != "" || c.Window == nil || c.CollectedAt.IsZero() {
		return ""
	}
	if c.Window.First.After(run.Started) || c.CollectedAt.Before(at) {
		return ""
	}
	n := 0
	for _, event := range c.Events {
		if column.kinds[event.Kind] && event.Timestamp.After(run.Started) && !event.Timestamp.After(at) {
			n++
		}
	}
	return strconv.Itoa(n)
}
