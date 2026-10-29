# Lab integration: verified baseline and remaining checks

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

## What remains unverified

No live `open5gs` cluster was reachable during this repository update. The local Windows `kubectl` had no current context; the Ubuntu WSL context pointed to a stopped Minikube endpoint. Therefore metric endpoints, Prometheus scrape status, metric names, and fault behavior have **not** been verified live.

When the lab is running, first execute its `scripts/verify.sh`, then run `go run ./cmd/corepulse discover --output reports/discovery.json`. Review the report, probe candidate endpoints, and record actual metric names before designing features or training models. The discovery report deliberately labels annotation-derived endpoints as candidates, not verified metrics.

## First fault experiment

The `pod-crash` command accepts only `amf` or `upf`. Without `--apply` it prints a plan. With `--apply`, it checks the exact Kubernetes context, requires one ready replica, checks the pod's ReplicaSet owner, deletes that one pod in `open5gs`, waits for deletion, and then waits for deployment recovery. It writes a run JSON record. A recovery failure is reported as `recovery_unverified`; no success is inferred from the delete command alone.

This is an isolated lab experiment, not a production fault injector. Packet loss, latency, and SCTP scenarios are deferred until the running images, privileges, and rollback mechanisms are verified.
