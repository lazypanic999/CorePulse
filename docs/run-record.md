# Fault run record v1

Each applied `pod-crash` experiment writes one UTF-8 JSON file under `runs/`, named by `run_id`. The directory is Git-ignored because local runs may contain environment details. Times use UTC ISO 8601 strings.

| Field | Meaning |
| --- | --- |
| `schema_version` | Integer schema version, currently `1` |
| `run_id` | Random UUID identifying one experiment |
| `fault` | Fault label, currently `pod_crash` |
| `target` | Allowlisted target, `amf` or `upf` |
| `context` | Kubernetes context observed before the change |
| `namespace` | Fixed lab namespace, `open5gs` |
| `pod_deleted` | Exact pod selected from the deployment |
| `replacement_pod` | New ready pod, present only after verified recovery |
| `started_at`, `ended_at` | Experiment bounds |
| `status` | `recovered` or `recovery_unverified` |
| `error` | Failure detail, present only when recovery is unverified |
| `service_probe` | Optional read-only UE-to-UPF gateway check after deployment recovery: status, current UE pod, packet counts, and time. A healthy gateway check does not prove external Internet reachability. |
| `pre_service_probe` | Gateway check immediately before fault injection; must be healthy for new fault runs. |
| `observed_samples` | Number of resource samples captured during the fault interval. |
| `quality` | `clean` when experiment provenance gates pass; otherwise `excluded`. A degraded post-run service path can still be a clean fault observation. |
| `deployment_recovered_at` | Time Kubernetes replacement readiness was verified, before service observation. |
| `service_recovery` | Up to six packet probes, with five seconds between attempts. Two consecutive healthy probes establish `service_recovered`; otherwise the result is `service_degraded` or `service_unverified`. Probe and sampling execution time adds to the interval. |

New fault runs keep `ended_at` open until service observation finishes and sample resource metrics after every probe. The full interval includes recovery observation; its `pod_crash` label describes the experiment rather than an assertion that every sample is anomalous. `service_probe` is retained as the final probe for compatibility. The CLI exits with code 2 when deployment or service recovery remains unverified. Dashboard history shows the service outcome; older records are labeled as deployment recovery only.

The run label records the action that CorePulse applied. It is **not** an ML prediction. `recovered` means the replacement deployment became ready; inspect `service_probe` separately for the observed UE path. New runs also record `pre_service_probe`, `observed_samples`, and `quality`. `clean` requires a healthy path and ready essential workloads before the fault, deployment recovery, an observed post-run path outcome, and at least one captured resource sample. A degraded post-run path is a valid fault outcome. Older run records lack this assertion and are excluded from the curated dataset.

`baseline` writes the same core identity and interval fields with `fault: normal`, `status: verified` and `quality: clean` only after every sample passes the UE-to-UPF probe and AMF/UPF pod identity checks. It also writes `samples` and `service_probes`. Failed intervals retain `status: unverified`, `quality: excluded`, and an error. Train/test splitting must group all windows from one `run_id` together.
