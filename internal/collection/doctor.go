package collection

import (
	"context"
	"fmt"
	"time"

	"github.com/lazypanic999/CorePulse/internal/corepulse"
	"github.com/lazypanic999/CorePulse/internal/telemetry"
)

type DiagnosticGate struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type DiagnosticReport struct {
	SchemaVersion int              `json:"schema_version"`
	StartedAt     time.Time        `json:"started_at"`
	FinishedAt    time.Time        `json:"finished_at"`
	Ready         bool             `json:"ready"`
	Context       string           `json:"context"`
	Namespace     string           `json:"namespace"`
	Checks        []DiagnosticGate `json:"checks"`
	Limitations   []string         `json:"limitations"`
}

// Diagnose observes collection prerequisites without acquiring/releasing a
// marker, appending telemetry, creating runs or changing workload state. It is
// a sequential diagnostic snapshot, never permission to bypass apply preflight.
func Diagnose(ctx context.Context, client corepulse.Client) (report DiagnosticReport) {
	report = DiagnosticReport{SchemaVersion: 1, StartedAt: time.Now().UTC(), Context: corepulse.LabContext, Namespace: corepulse.LabNamespace,
		Checks: []DiagnosticGate{}, Limitations: []string{
			"Sequential observations can change immediately; collection repeats its own guards and acquires ownership before mutation.",
			"Protocol observations come from bounded current-pod logs. The gateway probe does not establish Internet access.",
			"Metric coverage does not certify fresh or independent source intervals. No telemetry or run record is written.",
		}}
	for _, name := range []string{"ownership", "context", "workloads", "packet_path", "n2", "n4", "resource_metrics", "stable_pod_identities"} {
		report.Checks = append(report.Checks, DiagnosticGate{Name: name, Status: "skipped", Detail: "An earlier prerequisite prevented this observation."})
	}
	set := func(name, status, detail string) {
		for i := range report.Checks {
			if report.Checks[i].Name == name {
				report.Checks[i].Status, report.Checks[i].Detail = status, detail
			}
		}
	}
	defer func() {
		report.FinishedAt = time.Now().UTC()
		report.Ready = true
		for _, check := range report.Checks {
			if check.Status != "pass" {
				report.Ready = false
			}
		}
	}()
	owner := InspectOwnership()
	if !owner.Available || owner.Locked {
		set("ownership", "blocked", "Shared collection is locked or unavailable; inspect ownership before retrying. No cluster checks were started.")
		return report
	}
	set("ownership", "pass", "No shared marker was observed; no ownership was acquired.")
	if err := ctx.Err(); err != nil {
		set("context", "unavailable", err.Error())
		return report
	}
	current, err := client.Run(10*time.Second, "config", "current-context")
	if err != nil {
		set("context", "unavailable", err.Error())
		return report
	}
	if current != corepulse.LabContext {
		set("context", "fail", fmt.Sprintf("Current context %q is not %q; no lab resources were read.", current, corepulse.LabContext))
		return report
	}
	set("context", "pass", "Active context matches the allowlisted lab; actual lab commands remain context-pinned.")
	pods, err := corepulse.StableLabPods(client)
	if err != nil {
		set("workloads", "fail", err.Error())
		return report
	}
	set("workloads", "pass", "All six essential deployment-owned pods are ready with one replica each.")
	if err := ctx.Err(); err != nil {
		set("packet_path", "unavailable", err.Error())
		return report
	}
	probe, probeErr := corepulse.ProbeService(client)
	if probeErr != nil {
		set("packet_path", "unavailable", probeErr.Error())
	} else if probe.Status == "healthy" {
		set("packet_path", "pass", fmt.Sprintf("%d/%d gateway packets received; this is not an Internet check.", probe.Received, probe.Sent))
	} else if probe.Status == "degraded" {
		set("packet_path", "fail", fmt.Sprintf("%d/%d gateway packets received.", probe.Received, probe.Sent))
	} else {
		set("packet_path", "unavailable", probe.Detail)
	}
	if ctx.Err() != nil {
		return report
	}
	protocol, protocolErr := corepulse.CheckProtocolReadiness(client)
	for name, state := range map[string]string{"n2": protocol.Links.N2, "n4": protocol.Links.N4} {
		status, detail := "fail", "Observed link state: "+state
		if state == corepulse.LinkUp {
			status = "pass"
		} else if state == "" || state == corepulse.LinkUnknown {
			status = "unavailable"
			if protocolErr != nil {
				detail += "; " + protocolErr.Error()
			}
		}
		set(name, status, detail)
	}
	if ctx.Err() != nil {
		return report
	}
	sample, err := telemetry.Capture(client)
	if err != nil {
		set("resource_metrics", "unavailable", err.Error())
	} else {
		observed := map[string]bool{}
		for _, pod := range sample.Pods {
			observed[pod.Name] = true
		}
		missing := 0
		for _, pod := range pods {
			if !observed[pod] {
				missing++
			}
		}
		if missing > 0 {
			set("resource_metrics", "fail", fmt.Sprintf("Metrics are absent for %d of the six current essential pods; wait for metrics-server and retry.", missing))
		} else {
			set("resource_metrics", "pass", "Metrics cover all six current essential pod names; values were not saved.")
		}
	}
	if ctx.Err() != nil {
		return report
	}
	currentPods, err := corepulse.StableLabPods(client)
	if err != nil {
		set("stable_pod_identities", "fail", err.Error())
	} else {
		set("stable_pod_identities", "pass", "Essential pod names stayed unchanged across these observations.")
		for deployment, pod := range pods {
			if currentPods[deployment] != pod {
				set("stable_pod_identities", "fail", "An essential pod changed during inspection; retry after the lab settles.")
				break
			}
		}
	}
	owner = InspectOwnership()
	if !owner.Available || owner.Locked {
		set("ownership", "blocked", "Shared collection became locked or unavailable during inspection; this snapshot cannot qualify a new run.")
	}
	return report
}
