# Validated lab signals (2 November 2026)

For the subsequent successful lab restart, subscriber repair and UPF service reset on 4 November, see [the current integration results](lab-integration.md) and [lab operations](lab-operations.md). The offline state described below is historical.

Read-only checks were run against the `open5gs-lab` context and `open5gs` namespace after the Minikube cluster and essential deployments recovered from a restart. The commands below describe observed sources; they do not imply that Open5GS exposes a Prometheus KPI endpoint.

| Signal | Live observation | Interpretation and limit |
| --- | --- | --- |
| Pod CPU and memory | `metrics.k8s.io/v1beta1` returned a `PodMetricsList` | Resource usage, not protocol health; individual pods may briefly be absent during restarts. |
| UE registration | Current UE pod log contained `Initial Registration is successful` | A registration event in this pod, not a continuous registration success rate. |
| PDU session | Current UE pod log contained `PDU Session establishment is successful` | A session establishment event, not proof that later packets still pass. |
| UE to UPF gateway | `corepulse signals` reported 2/2 replies from `10.45.0.1` | Current packet-path check through `uesimtun0`; this does not prove Internet reachability. |
| External data path | A direct UE ping to `8.8.8.8` returned 2/2 replies | Point-in-time end-to-end packet test. |
| AMF UE event | Current AMF log contained `Number of AMF-UEs is now 1` | Event-derived count; the current log format is not a scrapeable time-series metric. |
| SMF session event | Current SMF log contained `Number of SMF-Sessions is now 1` | Event-derived count; may be stale if log history is truncated. |
| UPF association | Current UPF log contained `PFCP associated` | Association event; no continuous PFCP health counter verified. |
| Prometheus scrape targets | `corepulse discover` found zero scrape annotation candidates and zero verified metric names | No Open5GS protocol KPI or Prometheus endpoint is claimed. |

The useful split for the next feature set is control-plane events (UE registration and PDU session), user-plane checks (UE-to-UPF and external packet probes), and resource metrics. A ready Kubernetes pod alone is insufficient to establish service health. `corepulse signals` implements the first two UE event checks and the gateway probe without conflating historical log events with the current packet result. AMF/SMF/UPF log markers above were inspected manually and are not yet collected into the dataset.

The lab's full `scripts/verify.sh` timed out on `open5gs-populate` after restart. Its init container reported a duplicate subscriber key. At the time of the point-in-time checks above, the six essential workloads used by CorePulse's baseline gate were ready and the UE packet path was healthy. The full verifier needs a separate lab idempotency fix.

## Signals confirmed on 8 November 2026

These log markers were seen in the live lab with `kubectl logs --timestamps`, and are now parsed by `internal/logevents`:

| Component | Marker | When it appeared |
| --- | --- | --- |
| UE | `Initial Registration is successful`, `PDU Session establishment is successful` | Every UE start |
| gNB | `NG Setup procedure is successful` | gNB start, and after `recover-service.sh` |
| gNB | `Association terminated for AMF` | 31 to 46 s after an AMF fault run started; no NG Setup followed |
| SMF, UPF | `PFCP associated` | Lab start and after `recover-service.sh` |
| SMF | `No Heartbeat from UPF`, `No UPF available`, `PFCP de-associated` | About 20 s after a UPF fault run started |
| SMF | `Retry association with peer failed` | Every 11 s afterwards, naming the old UPF pod IP |

Open5GS writes ANSI colour codes inside its timestamp and component fields, and both Open5GS and UERANSIM write UTC that matches the runtime timestamps. The registration and PDU session failure markers have still not been observed.

## Controlled UPF replacement

One guarded UPF pod-crash run started with a healthy 2/2 gateway probe and captured three resource samples inside the recorded interval. The old UPF pod disappeared from the later samples, Kubernetes made a replacement pod ready, but the post-run UE-to-UPF probe was degraded. UPF and SMF logs then showed PFCP heartbeat and session-establishment failures. This is direct evidence that deployment readiness and 5G service recovery are separate outcomes. The run's `quality: clean` means its provenance and observation gates passed; it does **not** mean the service was healthy after the fault.

After the experiment, SMF and UE rollout restarts did not reliably restore the PDU session. A subsequent Minikube restart ended with the WSL lab stopped unexpectedly, so no second UPF fault run was attempted and the lab is currently offline. The cause of the WSL shutdown has not been established. Local crash dumps were preserved on the D drive for diagnosis.
