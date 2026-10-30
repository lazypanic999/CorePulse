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

The first `scripts/verify.sh` attempt failed. During a UE rollout it selected an older terminating pod and missed the new pod's successful registration log. The new UE had a tunnel, but pings to both `8.8.8.8` and UPF gateway `10.45.0.1` received no replies. The UPF logged GTP-U Error Indications. The gNB ConfigMap had `gtpIp: 0.0.0.0`, and the UPF logged a GTP peer of `0.0.0.0`; the user plane began working when gNB advertised its actual pod IP.

Run `bash scripts/configure-gnb-n3.sh` from this repository after deploying the lab. It patches only the `ueransim-gnb` deployment in the exact `open5gs-lab` context to copy its ConfigMap into `/tmp`, replace `gtpIp` with the current pod IP from the Kubernetes Downward API, and restart UE after gNB is ready. It waits for old UE pods to leave so the lab verifier reads the current pod's logs. Reapply the patch after a Helm upgrade of UERANSIM. The script is idempotent and is specific to the tested chart/image configuration.

After this patch, a direct ping to `10.45.0.1` returned 3/3 replies and the lab's unchanged `scripts/verify.sh` passed end to end: all deployments available, UE registration, PDU session, tunnel interface, and 5/5 replies from `8.8.8.8`.

CorePulse ran one guarded AMF pod-crash experiment from the dashboard. Kubernetes created a replacement AMF pod and the deployment returned to 1/1 ready. The recorded run status was `recovered`, based on deployment readiness; the run interval produced three `pod_crash` and 31 `normal` resource-metric samples in the first CSV. A gNB/UE restart restored UE registration after the AMF replacement. The end-to-end packet test passed later, after the gNB N3 address correction. The first dataset also contains lab startup and diagnostic restarts outside the fault interval, so its `normal` rows are baseline candidates, not a clean training set.

## First fault experiment

The `pod-crash` command accepts only `amf` or `upf`. Without `--apply` it prints a plan. With `--apply`, it checks the exact Kubernetes context, requires one ready replica, checks the pod's ReplicaSet owner, deletes that one pod in `open5gs`, waits for deletion, and then waits for deployment recovery. It writes a run JSON record. A recovery failure is reported as `recovery_unverified`; no success is inferred from the delete command alone.

This is an isolated lab experiment, not a production fault injector. Packet loss, latency, and SCTP scenarios are deferred until the running images, privileges, and rollback mechanisms are verified.
