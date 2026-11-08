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

Baseline interval waits and CLI kubectl calls are cancellable. `Ctrl+C` stops collection without starting another sample/run and saves the interrupted interval as excluded, with its end time and reason. Fault runs also save excluded checkpoints before deletion and during recovery; pending or interrupted intervals do not qualify for this CSV. Raw JSONL is preserved. See [run checkpoint and cancellation semantics](run-record.md#checkpoints-and-interrupted-processes).

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

## Timestamped protocol log events

`internal/logevents` parses UERANSIM UE and gNB logs and Open5GS SMF and UPF logs into structured events with a UTC timestamp, its source, the component, the event kind, a 1-based line number, and provenance (pod, container, log format).

| Kind | Component | Marker | Observed in this lab |
| --- | --- | --- | --- |
| `registration_success` | UE | `Registration is successful` | Yes |
| `registration_failure` | UE | `Registration failed` / `Registration reject` | Not yet |
| `pdu_session_success` | UE | `PDU Session establishment is successful` | Yes |
| `pdu_session_failure` | UE | `PDU Session Establishment ... reject/fail` | Not yet |
| `ng_setup_success` | gNB | `NG Setup procedure is successful` | Yes |
| `n2_association_terminated` | gNB | `Association terminated for AMF` | Yes, after an AMF pod crash |
| `pfcp_associated` | SMF, UPF | `PFCP associated` | Yes, both |
| `pfcp_heartbeat_timeout` | SMF, UPF | `No Heartbeat from UPF/SMF` | Yes (SMF), after a UPF pod crash |
| `pfcp_no_upf_available` | SMF | `No UPF available` | Yes, after a UPF pod crash |
| `pfcp_deassociated` | SMF, UPF | `PFCP de-associated` | Yes (SMF), after a UPF pod crash |
| `pfcp_association_retry_failed` | SMF, UPF | `Retry association with peer failed` | Yes (SMF), repeating every 11 s |

Each event carries `marker_observed_live`. The two failure markers still marked "not yet" follow upstream log text and must be confirmed against the pinned lab images before they are used as features. The fixtures in `internal/logevents/testdata/kubectl` are real lab output from 7 and 8 November 2026 with colour codes intact and subscriber identity lines removed; the other fixtures are synthetic.

Time handling is explicit. The preferred input is `kubectl logs --timestamps`: every line then starts with an RFC 3339 timestamp from the container runtime, which carries its zone and becomes the event time (`timestamp_source: runtime`). In that mode the application timestamp is only checked for shape. Without it, neither application format includes a zone, so the caller must pass the zone the container wrote in, and Open5GS timestamps also omit the year: the caller passes the collection time, and an event more than one day after it is assigned to the previous year. In the lab both formats are written in UTC and match the runtime timestamps to the millisecond.

ANSI colour escapes and CRLF endings are removed before parsing; Open5GS embeds colour codes inside the timestamp and component fields. Lines that start like a record but do not parse are reported in `skipped` with a reason and no message text, so subscriber identifiers are not copied. In runtime mode a line without the runtime prefix is skipped as `missing runtime timestamp`. Other lines, such as banners, are ignored.

Each matching line is one event, so repeated real events can be counted. `Dedupe` removes events that recur only because two log reads overlapped, using timestamp, component, kind, pod, and container.

The result has no health field. It reports `timestamped_lines` and the covered time `window`; a missing event means only that the marker did not appear in that window. It does not mean the service was healthy or that the failure rate was zero.
