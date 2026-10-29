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

The run label records the action that CorePulse applied. It is **not** an ML prediction. Future telemetry windows should reference `run_id` and preserve normal periods before and after the fault. Train/test splitting must group all windows from one `run_id` together.
