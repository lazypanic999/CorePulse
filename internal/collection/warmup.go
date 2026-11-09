package collection

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type MetricsReady struct {
	Checks int      `json:"checks"`
	Pods   []string `json:"pods"`
}

// WaitForMetrics is a read-only setup step before an interval is labelled.
// It never appends telemetry, creates a run, restarts pods or relaxes baseline
// gates. Workloads must already be ready; only metric coverage is retried.
func WaitForMetrics(ctx context.Context, client corepulse.Client, timeout time.Duration) (MetricsReady, error) {
	return waitForMetrics(ctx, client, timeout, waitContext)
}

func waitForMetrics(ctx context.Context, client corepulse.Client, timeout time.Duration, wait func(context.Context, time.Duration) error) (MetricsReady, error) {
	result := MetricsReady{Pods: []string{}}
	if timeout <= 0 || timeout > 5*time.Minute {
		return result, fmt.Errorf("metrics wait must be positive and at most 5m")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	contextName, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		return result, err
	}
	if contextName != corepulse.LabContext {
		return result, fmt.Errorf("metrics warmup requires context %q; current context is %q", corepulse.LabContext, contextName)
	}
	pods, err := corepulse.StableLabPods(client)
	if err != nil {
		return result, fmt.Errorf("metrics warmup requires ready essential workloads: %w", err)
	}
	for _, pod := range pods {
		result.Pods = append(result.Pods, pod)
	}
	sort.Strings(result.Pods)
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	bounded := metricsClient{Client: client, ctx: deadlineCtx}
	lastReason := "no resource sample observed"
	for {
		if err := deadlineCtx.Err(); err != nil {
			return result, fmt.Errorf("metrics warmup stopped (%s): %w", lastReason, err)
		}
		result.Checks++
		sample, err := telemetry.Capture(bounded)
		if err == nil {
			observed := map[string]bool{}
			for _, pod := range sample.Pods {
				observed[pod.Name] = true
			}
			missing := []string{}
			for _, pod := range result.Pods {
				if !observed[pod] {
					missing = append(missing, pod)
				}
			}
			if len(missing) == 0 {
				current, err := corepulse.StableLabPods(client)
				if err != nil {
					return result, err
				}
				for deployment, pod := range pods {
					if current[deployment] != pod {
						return result, fmt.Errorf("%s changed during metrics warmup; retry after the lab settles", deployment)
					}
				}
				if err := deadlineCtx.Err(); err != nil {
					return result, err
				}
				return result, nil
			}
			lastReason = fmt.Sprintf("resource metrics missing %v", missing)
		} else {
			lastReason = err.Error()
		}
		if err := wait(deadlineCtx, 5*time.Second); err != nil {
			return result, fmt.Errorf("metrics warmup stopped (%s): %w", lastReason, err)
		}
	}
}

type metricsClient struct {
	corepulse.Client
	ctx context.Context
}

func (c metricsClient) Run(timeout time.Duration, args ...string) (string, error) {
	if err := c.ctx.Err(); err != nil {
		return "", err
	}
	if deadline, ok := c.ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return "", context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	output, err := c.Client.Run(timeout, args...)
	if c.ctx.Err() != nil {
		return "", c.ctx.Err()
	}
	return output, err
}
