package collection

import (
	"context"
	"errors"
	"testing"
	"time"
)

type warmingClient struct {
	labClient
	captures, missingUntil int
}

func (c *warmingClient) Run(timeout time.Duration, args ...string) (string, error) {
	if args[0] == "get" {
		c.captures++
		if c.captures <= c.missingUntil {
			return `{"items":[{"metadata":{"name":"open5gs-amf-abc-pod"},"containers":[{"usage":{"cpu":"1m","memory":"1Mi"}}]}]}`, nil
		}
	}
	return c.labClient.Run(timeout, args...)
}

func TestMetricsWarmupWaitsForCurrentPodCoverage(t *testing.T) {
	c := &warmingClient{missingUntil: 2}
	waits := 0
	r, err := waitForMetrics(context.Background(), c, time.Minute, func(context.Context, time.Duration) error { waits++; return nil })
	if err != nil || r.Checks != 3 || waits != 2 || len(r.Pods) != 6 {
		t.Fatalf("warmup=%+v waits=%d err=%v", r, waits, err)
	}
}

func TestMetricsWarmupCancellationNeverStartsAnotherCapture(t *testing.T) {
	c := &warmingClient{missingUntil: 100}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := waitForMetrics(ctx, c, time.Minute, func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() })
	if !errors.Is(err, context.Canceled) || r.Checks != 1 || c.captures != 1 {
		t.Fatalf("warmup=%+v captures=%d err=%v", r, c.captures, err)
	}
}

func TestMetricsWarmupDeadlineDoesNotTurnMissingIntoReady(t *testing.T) {
	c := &warmingClient{missingUntil: 100}
	_, err := waitForMetrics(context.Background(), c, time.Nanosecond, func(ctx context.Context, _ time.Duration) error { <-ctx.Done(); return ctx.Err() })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost deadline: %v", err)
	}
}
