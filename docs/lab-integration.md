# Lab integration: live baseline and remaining checks

## 8 November 2026: repeated faults and their causes

The lab was started with `scripts/start-lab.ps1` and all 16 deployments became available. Before any fault the UE registered, established its PDU session, and passed 2/2 gateway checks. Over the night these runs were recorded:

| Runs | Outcome |
| --- | --- |
| 9 baselines | All `verified` and `clean`; every probe 2/2 |
| 5 UPF runs (`c5cc300f`, `68acf881`, `4ed364c1`, `93eee07a`, `2a2e78bd`) | Replacement pod ready, packet path `service_degraded` every time (six probes at 0/2) |
| 5 AMF runs (`141b3adb`, `fafacad0`, `54f6392e`, `3abbd583`, `d1776481`) | Replacement pod ready, packets kept flowing (`service_recovered`), but the gNB lost N2 every time |

Counting the earlier runs, all seven clean UPF runs left the user plane down. (One earlier UPF record, `388d36ac`, overlapped lab startup and is excluded.) The SMF log shows why. When the old UPF pod (10.244.0.209) disappeared, SMF logged a missed heartbeat, `No UPF available`, and `PFCP de-associated`, then retried the **old** address every 11 seconds. The replacement UPF (10.244.0.211) never logged an association. This happened with the headless `corepulse-upf-pfcp` service already in place, so SMF appears to resolve the peer once and keep the IP. Restarting SMF through `scripts/recover-service.sh` restored the path every time it was used that night (9 of 9), confirmed by two consecutive 2/2 checks. Issue [#22](https://github.com/lazypanic999/CorePulse/issues/22) tracks the cause; this is one lab with Open5GS 2.7.5, not a general claim.

An AMF pod crash does not interrupt existing sessions: the user plane does not need the AMF. But 31 to 46 seconds after the AMF fault run started, the gNB logged `Association terminated for AMF` and never ran NG Setup with the replacement AMF. New registrations would have failed while every ping passed. `scripts/recover-service.sh` restarts the gNB, which then set up N2 again. Issue [#21](https://github.com/lazypanic999/CorePulse/issues/21) and PR [#24](https://github.com/lazypanic999/CorePulse/pull/24) add a separate N2 result to fault runs.

Both findings came from reading component logs with runtime timestamps (`corepulse signals`, PRs [#19](https://github.com/lazypanic999/CorePulse/pull/19) and [#20](https://github.com/lazypanic999/CorePulse/pull/20)). The packet probe alone would have reported the AMF runs as fully recovered. The local curated dataset grew to 267 rows (104 normal, 163 fault) from 24 clean runs: 12 baselines, 7 UPF and 5 AMF runs; with PR [#23](https://github.com/lazypanic999/CorePulse/pull/23) the newer rows also carry probe and protocol event columns.

## 4 November 2026: lifecycle and service reset

The lab now has a retained Windows-launched WSL session, a system-drive free-space check, and an idempotent patch for the pinned populate init container. On the existing subscriber, the patch and a subsequent populate rollout both succeeded without inserting a duplicate. The hidden launcher was invoked twice and reused its tracked process. Prior WSL logs showed orderly systemd shutdown; Microsoft's documented WSL idle-lifetime behavior is consistent with that evidence, but other shutdown causes have not been ruled out.

The ordered service reset stopped UE, gNB, SMF and UPF, waited for old pods to disappear, then restored them in reverse dependency order. The current UE registered, established its PDU session and passed two consecutive 2/2 gateway checks. The unchanged lab verifier subsequently passed all 16 deployments, registration, PDU session, tunnel interface and 5/5 external ping replies. See [lab operations](lab-operations.md) for the commands and limits. This is a verified local procedure for the pinned lab, not a general production recovery mechanism.

An initial UPF observation overlapped lab startup and captured no resource samples; its record was excluded. A subsequent isolated UPF run (`1e0de561-bb86-4120-95de-18ff3f2a353a`) began with a healthy path, captured nine resource samples, and observed six failed gateway checks after the replacement deployment became ready. The curated dataset now contains 12 normal and 12 fault rows across five qualifying runs; historical/unscoped rows remain excluded. The reset procedure was then applied outside that recorded fault interval.

Ordered reset using the original virtual PFCP service peer did not reliably restore forwarding: a PDU session was created but PFCP session modification timed out. `configure-pfcp.py` then added a separate headless UPF PFCP service and changed only the pinned SMF peer DNS name. After reapplying the ordered reset, registration and PDU session events appeared and two consecutive 2/2 gateway checks passed. The unchanged full lab verifier passed again, including all 16 deployments and 5/5 external ping replies. This final direct-peer configuration is what the recovery script now applies; the lab was left running and healthy. No claim is made that this single workstation validation proves general PFCP resilience.

CorePulse is a separate repository. It does not copy or modify the [Open5GS + UERANSIM lab](https://github.com/lazypanic999/open5gs-ueransim-kubernetes).

## Verified from the lab repository

| Item | Evidence |
| --- | --- |
| Default namespace | `open5gs` in `scripts/deploy.sh` and `scripts/verify.sh` |
| Minikube profile | `open5gs-lab` in `scripts/cluster-up.sh` |
| Helm releases | `open5gs` and `ueransim` in `scripts/deploy.sh` |
| Open5GS chart | Gradiant `open5gs` `2.3.4` |
| UERANSIM chart | Towards5GS `ueransim` `2.0.17` |
| Verification | `scripts/verify.sh` checks deployments, UE registration, PDU session, and UE ping |
| AMF service | `open5gs-amf-ngap:38412` is configured for gNB N2/SCTP traffic |

The lab README names `open5gs-amf`, `open5gs-upf`, `ueransim-gnb`, and `ueransim-ue` deployments. CorePulse checks for these names but does not assume all of them exist in every running cluster.

## Live check on 31 October 2026

The `open5gs-lab` Minikube profile ran in Ubuntu WSL with the lab's Open5GS and UERANSIM Helm releases in namespace `open5gs`. All 16 deployments became available. The first UE started before the gNB and did not register; restarting the UE after the gNB was ready produced `Initial Registration is successful`, `PDU Session establishment is successful`, and tunnel interface `uesimtun0` with a `10.45.0.x` address. The same registration and session messages appeared after a later gNB/UE restart.

The Minikube metrics-server addon exposed `metrics.k8s.io/v1beta1`. CorePulse read real per-pod CPU and memory from it; a connected dashboard sample had 16 reporting pods. The discovery report showed zero Prometheus scrape annotation candidates. No Prometheus endpoint or Open5GS protocol KPI has been verified.

The first `scripts/verify.sh` attempt failed. During a UE rollout it selected an older terminating pod and missed the new pod's successful registration log. The new UE had a tunnel, but pings to both `8.8.8.8` and UPF gateway `10.45.0.1` received no replies. The UPF logged GTP-U Error Indications. The gNB ConfigMap had `gtpIp: 0.0.0.0`, and the UPF logged a GTP peer of `0.0.0.0`; the user plane began working when gNB advertised its actual pod IP.

Run `bash scripts/configure-gnb-n3.sh` from this repository after deploying the lab. It patches only the `ueransim-gnb` deployment in the exact `open5gs-lab` context to copy its ConfigMap into `/tmp`, replace `gtpIp` with the current pod IP from the Kubernetes Downward API, and restart UE after gNB is ready. It waits for old UE pods to leave so the lab verifier reads the current pod's logs. Reapply the patch after a Helm upgrade of UERANSIM. The script is idempotent and is specific to the tested chart/image configuration.

After this patch, a direct ping to `10.45.0.1` returned 3/3 replies and the lab's unchanged `scripts/verify.sh` passed end to end: all deployments available, UE registration, PDU session, tunnel interface, and 5/5 replies from `8.8.8.8`.

CorePulse ran one guarded AMF pod-crash experiment from the dashboard. Kubernetes created a replacement AMF pod and the deployment returned to 1/1 ready. The recorded run status was `recovered`, based on deployment readiness; the run interval produced three `pod_crash` and 31 `normal` resource-metric samples in the first CSV. A gNB/UE restart restored UE registration after the AMF replacement. The end-to-end packet test passed later, after the gNB N3 address correction. The first dataset also contains lab startup and diagnostic restarts outside the fault interval, so its `normal` rows are baseline candidates, not a clean training set.

## First fault experiment

The `pod-crash` command accepts only `amf` or `upf`. Without `--apply` it prints a plan. With `--apply`, it checks the exact Kubernetes context, requires one ready replica, checks the pod's ReplicaSet owner, deletes that one pod in `open5gs`, waits for deletion, and then waits for deployment recovery. It writes a run JSON record. A recovery failure is reported as `recovery_unverified`; no success is inferred from the delete command alone.

This is an isolated lab experiment, not a production fault injector. Packet loss, latency, and SCTP scenarios are deferred until the running images, privileges, and rollback mechanisms are verified.

## 2 November 2026: signal inventory and curated runs

After Minikube recovery, `corepulse signals` observed UE registration and PDU-session success events plus 2/2 UE-to-UPF gateway replies. The metrics API returned pod resource samples, and a direct UE external ping returned 2/2 replies. AMF, SMF, and UPF logs exposed event markers for UE count, session count, and PFCP association; no Prometheus protocol metric endpoint was verified. See [validated signals](validated-signals.md).

Three guarded normal intervals passed the six-workload, pod identity, metrics coverage, and gateway probe gates. The curated dataset contains 12 normal rows. One UPF pod-crash interval yielded three resource samples and a verified replacement pod. The post-run gateway probe was degraded despite Kubernetes deployment readiness; PFCP heartbeat and session-establishment failures appeared in logs. The reviewed run was included as a controlled fault interval, bringing the curated dataset to 12 normal and 3 fault rows. Historical/unscoped rows (560) were excluded.

The full lab verifier timed out because `open5gs-populate` repeatedly failed its init container with a duplicate subscriber key after restart. The six essential workloads and packet path were independently verified before the UPF fault. After the fault, SMF/UE restarts did not reliably restore the PDU session; a later Minikube restart ended with WSL stopping unexpectedly. The lab was left offline, and no second fault run was attempted. The WSL shutdown cause remains unknown.


## Staged UPF recovery observations — 9 November 2026

Two subsequent diagnostics began with verified N2/N4 and gateway readiness. After each UPF replacement, restarting only SMF restored observed PFCP association while two gateway checks stayed at 0/2. A later UE restart was followed by two consecutive 2/2 gateway checks without another gNB/UPF restart. These are limited sequential observations with timing/state confounds; the default ordered reset is unchanged. See the [stage-by-stage evidence and source review](experiments/2026-11-09-upf-recovery.md). Early post-UE-restart 0/0 records had no packet summary and must be treated as unknown, not measured packet loss.
