# Staged UPF recovery observations — 9 November 2026

Two sequential diagnostics in the existing single-node, single-UE lab separated a SMF-only restart from a later UE restart after a guarded UPF pod crash. The [sanitized summary](2026-11-09-upf-recovery-summary.json) contains every stage's packet counts, observed links and hashes of the local diagnostic records. Raw records are not distributed.

## Conditions and sequence

The images were Open5GS `docker.io/gradiant/open5gs:2.7.5` for SMF/UPF and UERANSIM UE `towards5gs/ueransim-ue:v3.2.6`. The previously configured direct/headless PFCP peer remained in place. Neither experiment changed ConfigMaps, services, CNI settings or peer addresses manually.

Each attempt acquired shared collection ownership before reading fresh healthy N2/N4, six ready deployment-owned workloads and a healthy gateway probe. The ordinary guarded UPF fault then completed its deployment/service observations. Subsequent restarts and probes were recorded separately under ignored `reports/`, with the diagnostic explicitly excluded from training. The original completed fault outcome was not rewritten after manual recovery, and neither diagnostic enters the frozen 22-run reference cohort.

SMF and UE were restarted in order, each by scaling its single replica to zero, waiting for the selected old pod's deletion, then scaling to one and waiting for rollout. The UE step was used only after two SMF-only packet checks remained degraded. gNB, AMF, UDR and the replacement UPF pod names stayed unchanged through these recovery stages. A reviewed full ordered reset was available on error but was not used in either attempt.

## Observed in both attempts

| Stage | N2 | N4 | Gateway observation |
| --- | --- | --- | --- |
| Before UPF fault | Up | Up | 2/2 replies |
| UPF replacement ready | Up | Stale peer | 0/2 replies |
| SMF restart, first check | Up | Up | 0/2 replies |
| SMF restart, follow-up | Up | Up | 0/2 replies |
| Early checks after UE restart | Up | Up | No transmitted-packet summary; recorded 0/0 |
| Later checks after UE restart | Up | Up | Two consecutive 2/2 replies |

The early 0/0 records are **unobserved packet counts**, not measured packet loss. The v0.2.1 collector used for these diagnostics recorded these startup command failures as `degraded`; they must not be interpreted as an observed zero-packet experiment. Updated source builds classify missing/invalid packet summaries as `unavailable` and export unknown counters as empty features. The summary retains the original recorded statuses instead of rewriting the evidence.

This gives a narrower observed recovery sequence than resetting all four service workloads: SMF association returned first, while the packet path remained down during the two checks; a later UE restart was followed by a working packet path without another UPF or gNB restart. There was no time-matched untreated control, so this does not prove that the UE restart was necessary or that this sequence always suffices. Restarting SMF changes both peer discovery and SMF process/session state, which are not isolated here. The older failed restart attempts in the historical lab notes used different starting conditions and are not contradicted by these two observations.

## Source review

The upstream v2.7.5 [PFCP configuration parser](https://github.com/open5gs/open5gs/blob/v2.7.5/lib/pfcp/context.c) resolves configured peer addresses into a PFCP node and retains its address list. The [SMF association timer case](https://github.com/open5gs/open5gs/blob/v2.7.5/src/smf/pfcp-sm.c) retries association using that node; the examined case has no resolver call. The [PFCP send path](https://github.com/open5gs/open5gs/blob/v2.7.5/lib/pfcp/path.c) sends through the node's existing address list. These paths are consistent with the observed stale-IP retries and fresh association after restarting SMF. This is an explanation of the reviewed path, not a proof of every possible peer-update mechanism or of behavior in other Open5GS versions.

The earlier report remains tracked in [#22](https://github.com/lazypanic999/CorePulse/issues/22) and the [upstream discussion](https://github.com/open5gs/open5gs/discussions/4978). No upstream patch or DNS-resilience claim follows from this diagnostic.

## Operational limits

Keep `scripts/recover-service.sh` as the reviewed default reset. Do not apply this two-workload observation to an AMF fault with lost N2: that case was not tested here. A reusable narrower recovery command needs explicit readiness/image/scope gates, durable phase records, shared ownership, cancellation and fallback tests before it replaces the default.

The observations concern the UE-to-UPF gateway, not Internet access. Two repetitions on one configuration do not establish production reliability, cross-session generalization or improved model accuracy. The separate offline reference study and its poor AMF results remain unchanged.
