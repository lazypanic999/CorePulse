# Lab integration: live baseline and remaining checks

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

The lab's `scripts/verify.sh` did **not** pass end to end. During a UE rollout it selected an older terminating pod and missed the new pod's successful registration log. Direct checks of the new UE confirmed registration, PDU session, and tunnel creation, but `ping -I uesimtun0 8.8.8.8` received 0/5 replies. A direct ping to the UPF tunnel gateway `10.45.0.1` also received no replies. The UPF logged GTP-U Error Indications and `Cannot find FAR by Error-Indication` while the ping ran. The cause of the user-plane failure remains open; pod readiness and a successful PDU establishment do not prove packet forwarding.

CorePulse ran one guarded AMF pod-crash experiment from the dashboard. Kubernetes created a replacement AMF pod and the deployment returned to 1/1 ready. The recorded run status was `recovered`, based on deployment readiness; the run interval produced three `pod_crash` and 31 `normal` resource-metric samples in the first CSV. A gNB/UE restart restored UE registration after the AMF replacement. This is control-plane and workload recovery evidence, not proof of user-plane recovery.

## First fault experiment

The `pod-crash` command accepts only `amf` or `upf`. Without `--apply` it prints a plan. With `--apply`, it checks the exact Kubernetes context, requires one ready replica, checks the pod's ReplicaSet owner, deletes that one pod in `open5gs`, waits for deletion, and then waits for deployment recovery. It writes a run JSON record. A recovery failure is reported as `recovery_unverified`; no success is inferred from the delete command alone.

This is an isolated lab experiment, not a production fault injector. Packet loss, latency, and SCTP scenarios are deferred until the running images, privileges, and rollback mechanisms are verified.
