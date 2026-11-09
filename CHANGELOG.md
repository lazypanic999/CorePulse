# Changelog

## Unreleased

- Run native Windows/macOS offline regression checks alongside the Linux minimum-version job. Build release archives and the container with the maintained toolchain pinned in `.go-version`; older Go 1.22 Mach-O binaries can fail to launch on current macOS due to missing UUID load commands.

- Record explicit prospective session declarations in baseline/fault/dashboard checkpoints and optional window metadata. Add deterministic, hash-bound whole-session holdouts with schema-2 evaluation, complete-session embargo purging, refusal of unknown/insufficient groups and explicit dashboard grouping. Legacy run-mode data remains compatible; declarations do not certify independence.

## v0.2.2 — 2026-11-09 (preview)

- Treat ping failures without valid positive transmitted-packet counts as unavailable, not observed packet loss. Preserve unknown/legacy 0/0 counters as empty CSV/window features while retaining genuine zero replies; unobserved final probes cannot qualify a fault as clean.

- Show shared collection ownership in the dashboard and disable run/typed-confirmation controls while locked, unavailable or submitting. Add a read-only ownership endpoint without release tokens or paths; reject relative lock overrides that could resolve differently across working trees.

## v0.2.1 — 2026-11-09 (preview)

- Distinguish live capture times from Metrics API source intervals in the resource panel. Show per-pod metadata coverage and averaging windows; historical unknown intervals remain explicit instead of implying fresh measurements.

- Add a deterministic, explicitly synthetic no-cluster walkthrough through windows, splitting and evaluation. A synthetic report marker/warning keeps invented tutorial scores separate from lab measurements; CI checks the complete example pipeline.

- Recompute saved evaluation scores from retained predictions before dashboard display. Reject mismatched confusion matrices, support, run votes, window identities, split roles or omitted comparisons without fitting models or reading the raw dataset.

- Use consistent half-open run intervals in sample and window exports: a capture at adjacent runs' shared boundary belongs only to the next run, and a capture at the final end remains excluded.

- Serialize CLI baseline batches and applied CLI/dashboard faults with shared, inspectable collection ownership before preflight. Forced termination retains ownership for conservative manual recovery; cross-user/WSL callers must configure a common marker directory.

- Preserve optional per-pod Metrics API source timestamps and averaging windows in new telemetry. Reject malformed metadata while retaining unknown legacy intervals; capture-time labels, resource values and fitted features remain unchanged.

## v0.2.0 — 2026-11-09 (preview)

- Service-aware experiment guards: observed N2/PFCP starting conditions, six-workload checks, and explicit lab context selection in Go and the lab helpers.
- Durable run checkpoints before mutation and through recovery; cancellable baselines preserve excluded interrupted evidence. Optional pre-run metrics warmup handles replacement-pod metric lag without relabelling failed intervals.
- Timestamped UE/gNB/SMF/UPF observations, stale PFCP peer detection, and N2 recovery outcomes separate from packet delivery.
- Curated sample CSV with explicit missing-vs-zero fields and protocol/probe evidence. Run-aligned feature windows, source-hash-bound whole-run splits and optional capture-time embargoes prepare offline studies.
- Resource-only nearest-centroid and normal-deviation references with training-only preprocessing, majority/probe comparisons, full held-out predictions, confusion matrices, F1 and abstention coverage.
- Read-only offline evaluation dashboard, distinct from live service checks, with updated real captures and an expandable mistakes table.
- Local reference cohort: 12 verified baselines, five AMF and five UPF faults. Of 60 feature windows, 31 trained the references, 20 were held out and nine were purged. The scenario classifier misses AMF and the binary resource method trails the packet rule; these are exploratory single-session results with explicit metric-timing limits.
- Code of Conduct, private vulnerability reporting, contributor guidance, scoped help issues and offline Go/Bash/Python fixture checks.

The public dataset card and aggregate report describe the evidence and limitations. Raw inputs remain local. This preview does not serve live model predictions and does not claim production readiness or certified measurement-time independence.

## v0.1.0 — 2026-10-31 (preview)

- Go CLI for lab discovery, a guarded AMF/UPF pod crash, and labeled CSV generation.
- Local dashboard with observed topology, live Kubernetes pod CPU/memory charts, and run history.
- Reproducible UERANSIM gNB N3 address correction for the tested Minikube lab.
- First end-to-end lab verification and a recorded AMF recovery experiment.

This preview does not include Open5GS protocol metrics, a trained detection model, or predictions. The first captured dataset includes lab startup and diagnostic activity and is not a clean model training set.
