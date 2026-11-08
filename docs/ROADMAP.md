# CorePulse roadmap

_Last updated: 8 November 2026_

This roadmap turns the phase table in the [README](../README.md#roadmap) into concrete milestones. Each milestone lists what it delivers, how we know it is done, and which open issues feed into it. Dates are intentionally omitted: a milestone ships when its acceptance criteria pass against the live lab, not on a calendar.

## Where we are

| Phase | Status | What exists | What is missing |
| --- | --- | --- | --- |
| 0. Discovery | Done | `discover`, `signals`, [validated signal inventory](validated-signals.md) | Open5GS exposes no verified Prometheus KPI endpoint |
| 1. Injector | Partial | Guarded `pod_crash` for AMF/UPF with checkpoints and recovery observation | `packet_loss`, `high_latency`, `sctp_drop` |
| 2. Dataset | Partial | Verified `baseline` runs, curated `dataset` CSV, run-level quality gates | Protocol/event features, windowing, enough runs per class |
| 3. Models | Not started | — | Anomaly and classification baselines, held-out evaluation |
| 4. Dashboard | Partial | Live topology, resource charts, guarded run control, run history | Prediction view, WebSocket stream |
| 5. Polish | Partial | Release binaries, GHCR image, WSL start/stop scripts | One-command setup on a clean machine, demo recording |

The key finding so far: **a ready Kubernetes pod does not mean a working 5G service.** The UPF experiment recovered at the deployment level while the UE-to-UPF path stayed degraded. Every milestone below keeps that distinction explicit.

## v0.2.0 — Service-aware signals

Goal: collect control-plane and user-plane health alongside resource metrics, so the dataset can tell "pod is back" from "service is back".

- [x] Merge resource quantity validation ([#1](https://github.com/lazypanic999/CorePulse/issues/1), PRs [#7](https://github.com/lazypanic999/CorePulse/pull/7), [#11](https://github.com/lazypanic999/CorePulse/pull/11)).
- [x] Offline contributor quickstart ([#2](https://github.com/lazypanic999/CorePulse/issues/2)).
- [x] Timestamped parser for registration, PDU session, and PFCP log events using fixtures ([#6](https://github.com/lazypanic999/CorePulse/issues/6)).
- [ ] Read real `kubectl logs --timestamps` output and confirm markers against live logs (PR [#19](https://github.com/lazypanic999/CorePulse/pull/19), in review).
- [ ] Report UE, gNB, SMF, and UPF events from `corepulse signals` (PR [#20](https://github.com/lazypanic999/CorePulse/pull/20), in review).
- [ ] Record protocol events in every run and add probe and event columns to the curated CSV, keeping "not observed" distinct from zero (PR [#23](https://github.com/lazypanic999/CorePulse/pull/23), in review). Events are read once per run rather than on the 10 s resource cadence; a continuous event stream can follow if models need it.
- [ ] Report N2 control-plane recovery separately from the packet path ([#21](https://github.com/lazypanic999/CorePulse/issues/21), PR [#24](https://github.com/lazypanic999/CorePulse/pull/24), in review).
- [x] Repeat the UPF pod-crash experiment and document whether `scripts/recover-service.sh` restores the PFCP path. Four of four UPF runs left the user plane down; the reset restored it each time it was used on 8 November. Cause under investigation in [#22](https://github.com/lazypanic999/CorePulse/issues/22).

**Done when:** a fault run record and its CSV rows show resource, event, and packet-path signals side by side, and the degraded UPF case is reproducible. The degraded case is reproducible; the rest lands with PRs #19, #20, #23, and #24.

## v0.3.0 — Fault catalog

Goal: move from one fault type to the four planned in the initial scope, each reversible and labeled.

- [ ] `packet_loss` on the N3/user-plane path (e.g. `tc netem` in the UPF or gNB pod network namespace) with configurable percentage.
- [ ] `high_latency` with configurable delay and jitter.
- [ ] `sctp_drop` on the N2 path between gNB and AMF.
- [ ] Each fault records parameters, start/end time, rollback command, and rollback verification in the [run record](run-record.md); a schema version field covers the change.
- [ ] The same dry-run → `--apply` → typed-confirmation safety model as `pod-crash`, plus an automatic rollback on interruption.
- [ ] Dashboard controls for the new faults behind the existing context and readiness guards.

**Done when:** every fault can be started, stopped, and rolled back from both CLI and dashboard, and an interrupted run leaves the lab in its baseline state.

## v0.4.0 — Training-ready dataset

Goal: a dataset a model can honestly be evaluated on.

- [ ] 30-second window feature extraction (mean, max, delta, missing-ratio per signal).
- [ ] Parameter sweeps per fault (several loss/latency levels) to avoid one-value-per-class leakage.
- [ ] Target size: at least 10 verified baseline runs and 5 clean runs per fault class.
- [ ] Run-level train/test split helper; windows from one run never cross the split.
- [ ] Dataset card in `docs/` describing collection conditions, class balance, and known biases (single node, single UE).
- [ ] Optional Parquet export next to CSV.

**Done when:** `corepulse dataset` produces a windowed, split dataset with a dataset card, reproducible from raw JSONL and run records.

## v0.5.0 — Detection baselines

Goal: measurable, honest first models.

- [ ] Decision record `0003` on model runtime (Go-native vs. separate Python service) after a small benchmark.
- [ ] Normal-vs-anomalous baseline (threshold/z-score first, then an isolation forest or similar).
- [ ] Fault-type classifier on the same features.
- [ ] Precision, recall, F1, and confusion matrix on held-out runs, saved as a versioned report.
- [ ] Compare against a trivial baseline (e.g. "probe failed ⇒ anomaly") so any gain is meaningful.

**Done when:** a reproducible evaluation report exists and its numbers come from runs the model never saw.

## v0.6.0 — Live predictions

Goal: the 30-second demo from the README.

- [ ] Prediction service integrated with the Go API.
- [ ] WebSocket stream for telemetry and predictions in the dashboard.
- [ ] Prediction panel showing the model output next to the known fault label; wrong predictions stay visible.
- [ ] Prediction log saved with the run record.

**Done when:** a user starts a fault, sees live metrics move, compares the prediction with the true label, and resets the lab from one screen.

## v1.0.0 — Reproducible demo

- [ ] One-command lab setup on a clean Ubuntu machine (Minikube, Helm, Open5GS, UERANSIM, gNB N3 patch, subscriber population).
- [ ] Idempotent subscriber population so the lab's `verify.sh` passes after restarts.
- [ ] Root cause or documented workaround for the WSL shutdown seen after Minikube restarts.
- [ ] Recorded demo video/GIF and refreshed dashboard captures.
- [ ] Clean-machine walkthrough tested by someone other than the maintainer.

## Cross-cutting

- **Safety first:** no change may weaken the context, ownership, or readiness guards. New mutations need a dry-run, a rollback path, and tests.
- **No invented data:** preview mode, missing metrics, and unverified runs stay labeled as such in the CLI, CSV, and dashboard.
- **Contributor paths:** keep at least one `good first issue` that needs no Kubernetes lab open at all times.

## How to help

Pick an open issue from the milestone you care about, or open a [feature request](https://github.com/lazypanic999/CorePulse/issues/new/choose) that references a roadmap item. See [CONTRIBUTING.md](../CONTRIBUTING.md) for the workflow and development checks.
