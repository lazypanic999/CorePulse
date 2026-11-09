# Local preview cohort: 9 November 2026

This is a small, single-session exploratory evaluation from the local Open5GS + UERANSIM lab. It is a reference result, not a production accuracy claim or certified measurement-time-independent benchmark. The [machine-readable summary](../evaluation/2026-11-09-reference-summary.json) contains class support, precision/recall/F1 and all confusion matrices without publishing raw telemetry or run identities.

## Collection

The lab used one Minikube node and one UE, Open5GS `2.7.5`, and the existing guarded AMF/UPF pod-crash experiment. Every qualifying run began with ready essential workloads, a healthy UE-to-UPF probe, and observed N2/PFCP readiness. Baselines repeated the packet, pod-identity, metric-coverage and protocol checks. Faults were separated by the ordered service reset.

| Scenario | Qualified runs | Feature windows | Train runs | Test runs | Purged runs |
| --- | ---: | ---: | ---: | ---: | ---: |
| Normal | 12 | 30 | 7 | 3 | 2 |
| AMF pod crash | 5 | 10 | 3 | 2 | 0 |
| UPF pod crash | 5 | 20 | 2 | 2 | 1 |
| Total | 22 | 60 | 12 | 7 | 3 |

There were **316 captured telemetry rows** inside qualifying intervals and **17 partial windows**. Captures come from experiment/baseline samplers and dashboard polling, so sampling density varies. These are captured rows, not 316 distinct underlying CPU measurements. Twenty-four older clean records without protocol-start evidence were excluded. A post-reset baseline that lacked an essential pod's metrics was retained as unverified/excluded; it was not relabelled as normal.

All five AMF experiments recovered the packet path but recorded N2 loss. All five UPF experiments recovered the deployment while the packet path remained degraded, with no N2 change observed. The ordered reset restored the lab after each fault. A separate live refusal test attempted a UPF run after an AMF-induced N2 loss: the request failed before deletion and the UPF pod identity stayed unchanged. The final check observed N2/N4 up and 2/2 UE-to-UPF gateway replies; this does not establish external Internet reachability.

## Evaluation policy

Parameters were fixed before real-data scores were inspected: 30-second run-aligned windows, seed `42`, requested held-out fraction `0.25`, normal-only deviation threshold `3`, and a **120-second capture-time embargo**. The embargo purged whole nearby training runs and left **31 training windows**, **20 held-out windows** and **9 purged windows**. Test support is eight normal, four AMF and eight UPF windows, from three/two/two runs respectively. Training support is seventeen normal, six AMF and eight UPF windows. Preprocessing is fitted on training data only; the deviation rule uses only normal training data.

| Method | Task | Window macro F1 | Run macro F1 | Window accuracy | Coverage |
| --- | --- | ---: | ---: | ---: | ---: |
| Majority scenario | Three scenarios | 0.190 | 0.200 | 0.400 | 1.000 |
| Nearest centroid | Three scenarios | 0.521 | 0.583 | 0.700 | 1.000 |
| Normal-only deviation | Normal / fault interval | 0.642 | 0.571 | 0.650 | 1.000 |
| Packet rule | Normal / fault interval | 0.800 | 0.708 | 0.800 | 1.000 |

The scenario classifier exceeds its majority reference in this cohort but **never predicts AMF**. Its window confusion matrix, with truth rows and prediction columns ordered normal/AMF/UPF, is:

| Truth / prediction | Normal | AMF | UPF |
| --- | ---: | ---: | ---: |
| Normal | 8 | 0 | 0 |
| AMF | 3 | 0 | 1 |
| UPF | 2 | 0 | 6 |

The resource-only deviation method does not beat the simple packet rule on their binary task. Neither result justifies deploying a learned detector. Scenario and binary F1 have different label sets and should not be ranked as if they were the same task. Every held-out prediction, including all mistakes, remains in the local report and dashboard snapshot.

## Timing and generalization limits

The Metrics API exposed approximately a one-minute CPU averaging window during this session (one observed value was `1m0.008s`). Kubernetes documents that CPU is averaged over the reported [`window`](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-metrics-pipeline/). CorePulse currently stores capture times, not the original metric timestamps/windows. Feature windows can therefore repeat or combine overlapping source averages. A two-minute capture-time embargo reduces nearby train/test exposure but cannot certify measurement-time independence. Retaining the missing metadata is [#39](https://github.com/lazypanic999/CorePulse/issues/39).

All runs share one collection session, machine, UE and configuration. Windows are not independent observations; long runs contribute more rows, and sparse/partial windows remain included. Fault labels identify controlled experiment intervals, not continuous degradation. Post-run protocol-event availability is unsuitable as a causal online input, so the fitted methods here use resource features only. [Session-grouped holdouts](https://github.com/lazypanic999/CorePulse/issues/33), richer causal protocol features, source-time validation and new predeclared cohorts are needed before stronger conclusions.

## Reproduction and distribution

Window CSV SHA-256:

```text
fb65a1590bff475f48abf9310514d70b356e9700c82d898f0bc054f8d9549b36
```

On the collection machine, inputs and outputs are frozen under ignored `data/preview-2026-11-09-v0.2/`. A fresh window export, split and evaluation from that snapshot reproduced the **exact report bytes** using the same Go 1.27.3 Linux/amd64 evaluator binary. The full report SHA-256 is in the public summary. The implementation is included in the v0.2.0 preview; [methods](../offline-evaluation.md) and [window semantics](../windowed-dataset.md) describe its behavior.

```bash
corepulse windows --telemetry data/preview-2026-11-09-v0.2/telemetry.jsonl --runs-dir data/preview-2026-11-09-v0.2/runs --output data/windows-rebuilt.csv
corepulse split --input data/windows-rebuilt.csv --output data/split-rebuilt.json --seed 42 --test-fraction 0.25 --embargo 2m
corepulse evaluate --input data/windows-rebuilt.csv --split data/split-rebuilt.json --output reports/evaluation-rebuilt.json --z-threshold 3
```

The raw inputs are **not distributed** with this summary. A fresh clone can inspect the implementation and synthetic regression tests but cannot independently reproduce these particular measurements without the local inputs. This is a documented local run, not an independently auditable public dataset release. Do not select a new seed or threshold merely to improve the displayed test score.
