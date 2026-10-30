# Resource telemetry and first labeled dataset

The local Go dashboard samples the Kubernetes resource metrics API (`metrics.k8s.io/v1beta1`) every 10 seconds while `corepulse serve` is running. Samples are appended to ignored `data/telemetry.jsonl`. The collector accepts only the `open5gs-lab` context and the `open5gs` namespace. Failed reads are reported in the UI and do not create synthetic rows.

The verified fields in each raw sample are UTC `timestamp`, source, context, namespace, total CPU in millicores, total memory in bytes, reporting pod count, and each pod's CPU/memory usage. These are **Kubernetes pod resource metrics**, not Open5GS protocol KPIs or Prometheus scrape metrics. Scrape annotations remain unverified until their endpoints are probed independently.

After a dashboard pod experiment, CorePulse writes the run JSON record and rebuilds ignored `data/dataset.csv`. It can also be rebuilt manually:

```bash
go run ./cmd/corepulse dataset --telemetry data/telemetry.jsonl --runs-dir runs --output data/dataset.csv
```

The CSV contains timestamp, `run_id`, label, target, provenance, pod count, aggregate CPU/memory, and AMF/UPF CPU/memory. Samples inside a run's recorded `[started_at, ended_at]` interval receive its fault label; samples outside all run intervals are `normal`. This labels the **experiment window**, which includes deletion and Kubernetes recovery. It does not prove that every sample in the window reflects degraded 5G service. Likewise, `normal` only means outside a recorded fault interval; the first capture includes lab startup and diagnostic restarts. Do not use it as a clean healthy training set. Group future model train/test splits by `run_id` after collecting clean, separately verified baseline runs.

The metrics API may lag pod creation or deletion, and missing pods do not mean zero resource use. Samples remain raw and append-only; the CSV is reproducible from raw samples and run records. No model accuracy claim should be made from the first demonstration run.
