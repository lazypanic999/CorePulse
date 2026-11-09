# Inspect the lab before collection

Run the native binary inside the environment that can reach the lab (Ubuntu WSL for the documented Windows/Minikube setup):

```bash
corepulse doctor
```

It prints one JSON report with eight separate checks: shared collection ownership, allowlisted current context, six essential ready deployment-owned pods, UE-to-UPF gateway packet delivery, N2, N4/PFCP, resource-metric coverage for the current essential pod names, and unchanged pod names across the observations. A packet pass cannot hide an N2 failure; unknown packet evidence cannot become measured loss. Zero resource use counts as observed when a reporting pod supplies it, while an absent pod does not.

Exit **0** means every observed prerequisite passed; **2** means blocked, failed, unavailable or skipped checks remain. Invalid command arguments or output errors use **1**. Interruptions and a three-minute overall deadline stop further CLI observations; individual Kubernetes calls retain their shorter timeouts.

The command does not acquire, expire or remove a collection marker, save telemetry, create a run or change workloads. It uses the existing gateway ping as an active diagnostic probe. If ownership is locked/unreadable, it reports the block without starting cluster checks. A different active context stops resource reads. Lab commands remain explicitly pinned to `open5gs-lab`; no kubeconfig setting is changed.

Observations are sequential and can become stale immediately. The final pod-name/ownership recheck catches observed changes, not every possible race. This is **not a reservation or authorization to skip experiment preflight**. Apply entry points still acquire shared ownership and repeat their own guards. Metric coverage does not establish source freshness or measurement independence; N2/PFCP use bounded current-pod log history, and the gateway check does not demonstrate Internet access.

For a blocked owner, follow [conservative ownership inspection](collection-ownership.md). For missing metrics after a restart, allow metrics-server to catch up or use the baseline's bounded `--metrics-wait`; doctor does not wait or repair the lab. For workload/protocol failures, inspect `corepulse signals` and the [lab recovery guide](lab-operations.md). Run a manual reset only outside an active experiment, then diagnose again. A stale marker is never removed automatically.

`doctor` needs live `kubectl` access; it is separate from the [no-cluster synthetic walkthrough](../examples/offline/README.md). Local reports can contain Kubernetes error details: review them before posting externally.
