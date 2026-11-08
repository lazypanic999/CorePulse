package collection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type protocolClient struct {
	labClient
	failedComponent string
	deletes         int
	matchedLogs     int
	failAfter       int
}

func (c *protocolClient) Run(timeout time.Duration, args ...string) (string, error) {
	if args[0] == "delete" {
		c.deletes++
	}
	if args[0] == "logs" && c.failedComponent != "" && strings.Contains(args[1], c.failedComponent) {
		c.matchedLogs++
		if c.matchedLogs > c.failAfter {
			return "", nil
		}
	}
	return c.labClient.Run(timeout, args...)
}

func TestProtocolDegradationDuringBaselineExcludesInterval(t *testing.T) {
	c := &protocolClient{labClient: labClient{probe: "2 packets transmitted, 2 received"}, failedComponent: "gnb", failAfter: 1}
	dir := t.TempDir()
	r, err := baseline(context.Background(), c, telemetry.NewStore(filepath.Join(dir, "raw.jsonl")), filepath.Join(dir, "runs"), 2, time.Second, func(context.Context, time.Duration) error {
		t.Fatal("started another sample after N2 became unobserved")
		return nil
	})
	if err == nil || r.Quality != "excluded" || r.Status != "unverified" || r.Samples != 1 || len(r.ProtocolChecks) != 1 || r.ProtocolChecks[0].Links.N2 != corepulse.LinkUnknown {
		t.Fatalf("unexpected interval: %+v err=%v", r, err)
	}
}

func TestUnavailableProtocolNeverStartsBaselineOrFault(t *testing.T) {
	for _, component := range []string{"gnb", "smf"} {
		t.Run(component, func(t *testing.T) {
			c := &protocolClient{labClient: labClient{probe: "2 packets transmitted, 2 received"}, failedComponent: component}
			dir := t.TempDir()
			runs := filepath.Join(dir, "runs")
			_, err := baseline(context.Background(), c, telemetry.NewStore(filepath.Join(dir, "raw.jsonl")), runs, 2, time.Second, func(context.Context, time.Duration) error { t.Fatal("baseline started"); return nil })
			if err == nil || !strings.Contains(err.Error(), "protocol readiness") {
				t.Fatalf("baseline: %v", err)
			}
			_, err = RunFault(c, "amf", runs, corepulse.ServiceProbe{Status: "healthy"}, func() error { t.Fatal("fault sampling started"); return nil })
			if err == nil || c.deletes != 0 {
				t.Fatalf("fault err=%v deletes=%d", err, c.deletes)
			}
			if _, err := os.Stat(runs); !os.IsNotExist(err) {
				t.Fatalf("refused run created a run record: %v", err)
			}
		})
	}
}
