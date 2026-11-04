# Resource telemetry and first labeled dataset

The local Go dashboard samples the Kubernetes resource metrics API (`metrics.k8s.io/v1beta1`) every 10 seconds while `corepulse serve` is running. Samples are appended to ignored `data/telemetry.jsonl`. The collector accepts only the `open5gs-lab` context and the `open5gs` namespace. Failed reads are reported in the UI and do not create synthetic rows.

The verified fields in each raw sample are UTC `timestamp`, source, context, namespace, total CPU in millicores, total memory in bytes, reporting pod count, and each pod's CPU/memory usage. These are **Kubernetes pod resource metrics**, not Open5GS protocol KPIs or Prometheus scrape metrics. Scrape annotations remain unverified until their endpoints are probed independently.

`corepulse signals` exposes three read-only lab observations: registration and PDU-session success events in the current ready UE pod's recent logs, and a current UE-to-UPF gateway packet probe. The log booleans indicate that those transitions appeared in the pod log; they are not continuous success rates or proof of present session state. The gateway probe gives the current packet result. No PFCP, GTP-U, or AMF protocol counter has been verified as a live metric.

After a dashboard pod experiment, CorePulse writes the run JSON record and rebuilds ignored `data/dataset.csv`. It can also be rebuilt manually:

```bash
go run ./cmd/corepulse dataset --telemetry data/telemetry.jsonl --runs-dir runs --output data/dataset.csv
```

The CSV contains timestamp, `run_id`, label, target, provenance, pod count, aggregate CPU/memory, and AMF/UPF CPU/memory. It now includes only samples inside a run marked `quality: clean` with a matching verified status and lab provenance. Samples outside such intervals are excluded, **not** labeled normal. The summary reports `excluded`. Existing first-capture samples and old fault runs remain in the raw JSONL but do not enter this curated export. An experiment interval label still does not prove that every sample was degraded. Group future model train/test splits by `run_id`.

`corepulse baseline --runs 3 --samples 4 --interval 10s` writes three separate `normal` run records. Before collection it requires the exact lab context, one ready AMF, SMF, UDR, UPF, gNB, and UE pod, and a healthy UE-to-UPF gateway probe. It captures resource metrics and repeats the probe, pod identity, and metric coverage checks at every sample. A failed check leaves an `unverified` record with `quality: excluded` for diagnosis. The probe result is a gateway packet check, not a general Internet reachability guarantee.

New pod-crash runs require a healthy pre-fault probe and ready essential workloads, sample after each fault step, and record a post-recovery probe. They receive `quality: clean` if deployment recovery, at least one captured sample, and an observed post-run path outcome all succeed. The post-run path may be degraded: that is a valid outcome of a controlled fault, not a data-quality failure. Otherwise the run record remains useful for diagnosis but its samples are excluded from the curated CSV. This is an experiment-quality assertion; it does not claim every interval sample reflects degraded 5G service.

On 2 November 2026, the local lab produced three verified normal runs (four samples each) and one reviewed UPF fault run (three samples). The curated CSV contains 12 normal and 3 fault rows; 560 historical or unscoped raw rows were excluded. These local files are Git-ignored. Four runs are far too few for a trustworthy model evaluation, and the fault interval does not include the full period of degraded service after deployment readiness.

The metrics API may lag pod creation or deletion, and missing pods do not mean zero resource use. Samples remain raw and append-only; the CSV is reproducible from raw samples and run records. No model accuracy claim should be made from the first demonstration run.

## Missing component metrics in CSV

`amf_cpu_millicores`, `amf_memory_bytes`, `upf_cpu_millicores` and `upf_memory_bytes`
are nullable numeric fields. When a sample has no reporting pod for a component, both of its
fields are empty. A reporting pod with zero usage produces `0.000` for CPU and `0` for memory.
Usage is summed across reporting pod names with the component prefix, including replacement pods.
Presence refers to the metrics sample and can lag the actual pod lifecycle.

CSV consumers should parse an empty cell as missing. Consumers that require numeric values must
choose an explicit missing-data policy before training or aggregation. Older exports wrote zeros
for missing components, so rebuild them from the raw JSONL and run records to recover the
distinction. The export uses the existing column names and order and continues to group rows by
`run_id` within clean, provenance-checked run intervals.
