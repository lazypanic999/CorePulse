# Fault run record v1

Updated source builds serialize CLI/dashboard experiment entry points with [shared collection ownership](collection-ownership.md). Interrupted markers require conservative manual inspection; the v0.2.0 release predates this guard.

## Protocol starting conditions

New baseline and fault runs require both `N2=up` and `N4=up` in fresh reads of the current gNB and SMF pod logs, in addition to workload readiness and the packet probe. Down, stale-peer, unreadable and unobserved links refuse the run before mutation or capture. The same fault gate is shared by CLI and dashboard; cached dashboard topology cannot authorize a deletion.

`pre_protocol_check` records the observation time, link states, current source pods, latest relevant event timestamps/kinds and component errors. Baselines additionally retain `protocol_checks` after each sample and exclude the whole interval if either link stops qualifying. These are log-derived transitions in a bounded tail, not active N2/PFCP handshake probes or proof that an old setup event remains current. Missing history is unknown and fails closed. Older runs without these fields remain readable; they do not retroactively acquire verified starting conditions. In particular, the mixed-condition UPF run identified in issue #27 should not be used as an independent UPF-only evaluation case.

Each applied `pod-crash` experiment writes one UTF-8 JSON file under `runs/`, named by `run_id`. The directory is Git-ignored because local runs may contain environment details. Times use UTC ISO 8601 strings.

| Field | Meaning |
| --- | --- |
| `schema_version` | Integer schema version, currently `1` |
| `run_id` | Random UUID identifying one experiment |
| `fault` | Fault label, currently `pod_crash` |
| `target` | Allowlisted target, `amf` or `upf` |
| `context` | Kubernetes context observed before the change |
| `namespace` | Fixed lab namespace, `open5gs` |
| `pod_deleted` | Exact pod selected from the deployment; selection alone does not prove deletion succeeded |
| `replacement_pod` | New ready pod, present only after verified recovery |
| `started_at`, `ended_at` | Experiment bounds |
| `status` | `recovery_pending`, `recovered` or `recovery_unverified`; deployment state only |
| `phase` | Last checkpoint: `prepared`, `delete_attempted`, `delete_requested`, `delete_observed`, `deployment_available`, `deployment_recovered`, `service_observation` or `completed` |
| `error` | Failure detail, present only when recovery is unverified |
| `service_probe` | Optional read-only UE-to-UPF gateway check after deployment recovery: status, current UE pod, packet counts, and time. A healthy gateway check does not prove external Internet reachability. |
| `pre_service_probe` | Gateway check immediately before fault injection; must be healthy for new fault runs. |
| `observed_samples` | Number of resource samples captured during the fault interval. |
| `quality` | `clean` when experiment provenance gates pass; otherwise `excluded`. A degraded post-run service path can still be a clean fault observation. |
| `deployment_recovered_at` | Time Kubernetes replacement readiness was verified, before service observation. |
| `service_recovery` | Up to six packet probes, with five seconds between attempts. Two consecutive healthy probes establish `service_recovered`; otherwise the result is `service_degraded` or `service_unverified`. Probe and sampling execution time adds to the interval. |

New fault runs keep `ended_at` open until service observation finishes and sample resource metrics after every probe. The full interval includes recovery observation; its `pod_crash` label describes the experiment rather than an assertion that every sample is anomalous. `service_probe` is retained as the final probe for compatibility. The CLI exits with code 2 when deployment or service recovery remains unverified. Dashboard history shows the service outcome; older records are labeled as deployment recovery only.

`protocol_events` (both fault and baseline runs, from 8 November 2026) holds the UE, gNB, SMF, and UPF log events read once after the run ends. Each component lists its pod, `collected_at`, the log `window`, line counts, and an `error` when it could not be read. A failed read sets `protocol_events_error` and never changes `quality`. A deleted pod's log is gone by then, so after a UPF fault the replacement UPF's window starts after the run start, and the SMF log carries the failure evidence.

`control_plane_recovery` (fault runs, from 8 November 2026) reports N2 between the gNB and the AMF, read from the gNB events in `protocol_events`. It is separate from `service_recovery` because existing sessions can keep forwarding packets while N2 is down. Its `status` is `n2_lost` (the gNB logged `n2_association_terminated` during the run and no later `ng_setup_success`), `n2_restored`, `n2_no_change_observed` (the gNB log covered the whole run with no termination), or `n2_unobserved` (the gNB log was not read or did not reach back to the run start). `terminated_at`, `restored_at`, `gnb_pod`, and `observed_until` carry the evidence. For AMF runs the gNB log is read at least 90 seconds after the fault started, because the SCTP termination appeared about 31 to 46 seconds after the AMF pod was deleted. The CLI exits with code 2 and the dashboard API returns 202 when N2 is lost, even if the packet path recovered.

## Checkpoints and interrupted processes

The CLI and dashboard use the same persistence path. A readable `prepared` record must be saved before any pod deletion. `delete_attempted` is saved immediately before executing the delete command; it means the command may have run, not that deletion was confirmed. After a successful delete request, `delete_requested` is saved before waiting for disappearance. Further checkpoints capture disappearance, deployment readiness, and each service probe. Service observation keeps `ended_at: null` and `quality: excluded`; only its completed result can qualify as clean. A recovered deployment checkpoint is also excluded until service observation completes.

Each write encodes a complete JSON snapshot, syncs a temporary file in the same directory, closes it, and replaces the previous snapshot. Readers ignore `.tmp` files. Windows readers and writers retry temporary sharing locks for a bounded period. A write failure before deletion prevents mutation; a failure afterward returns an error and leaves the last successfully saved checkpoint available. Checkpointing does not restart or automatically resume an interrupted experiment. Re-check lab health before another run. This is process-interruption protection, not a guarantee against disk failure or power loss.

Abrupt termination can leave a pending record without an end time. It remains diagnostic evidence and is excluded by the dataset builder. An ordinary command failure records `recovery_unverified`, an end time and the error when persistence remains available. Existing records without `phase` remain readable.

The run label records the action that CorePulse applied. It is **not** an ML prediction. `recovered` means the replacement deployment became ready; inspect `service_probe` separately for the observed UE path. New runs also record `pre_service_probe`, `observed_samples`, and `quality`. `clean` requires a healthy path and ready essential workloads before the fault, deployment recovery, an observed post-run path outcome, and at least one captured resource sample. A degraded post-run path is a valid fault outcome. Older run records lack this assertion and are excluded from the curated dataset.

`baseline` writes the same core identity and interval fields with `fault: normal`, `status: verified` and `quality: clean` only after every sample passes the UE-to-UPF probe and essential AMF/SMF/UDR/UPF/gNB/UE pod identity checks. It also writes `samples` and `service_probes`. Failed intervals retain `status: unverified`, `quality: excluded`, and an error. Train/test splitting must group all windows from one `run_id` together.

Baseline collection saves an excluded checkpoint before its first capture and after each validated sample. `Ctrl+C` or `SIGTERM` cancels both interval waits and active CLI kubectl commands. The current interval is finalized as `unverified` / `excluded` with an end time, sample/probe evidence and `error: "context canceled"`; the CLI exits 130 and does not start the next run. Previously verified runs stay valid, and raw telemetry remains append-only. Use the built binary for predictable signal delivery rather than relying on how `go run` forwards signals. A forced kill cannot finalize a record, but the last excluded checkpoint remains. Before finalization, baseline's time-valued `ended_at` is the zero timestamp; excluded quality prevents it from being treated as a completed interval.
