# Local lab lifecycle and service recovery

CorePulse's live kubectl operations explicitly select `--context open5gs-lab`, including operations inside a long experiment. The initial current-context check still refuses a different active context. Discovery follows the same rule; `discover --snapshot` remains available offline. Explicit selection prevents a later ambient context switch from redirecting an already-checked command. Context names and kubeconfig entries must still be configured correctly by the operator.

From PowerShell in the CorePulse checkout:

```powershell
.\scripts\start-lab.ps1
```

This starts a hidden Ubuntu WSL session and the existing `open5gs-lab` Minikube profile. Startup requires at least 3 GiB free on the Windows system drive. The session remains alive until explicitly stopped. Logs and the Windows process ID are saved under ignored `reports/`. Running the command again reuses the tracked session. A healthy running Minikube profile is reused without another start operation. Inspect `reports/lab-session-error.log` if startup fails. The dashboard is a separate process.

Microsoft documents that [systemd services do not keep WSL alive](https://learn.microsoft.com/en-us/windows/wsl/systemd). The previous machine logs showed an orderly systemd shutdown, rather than establishing a kernel crash. Keeping an explicit WSL process running addresses idle lifetime; it does not claim to prevent other shutdown causes.

To stop the Minikube profile and release its tracked WSL session:

```powershell
.\scripts\stop-lab.ps1
```

## Repeatable subscriber initialization

`run-lab.sh` runs `repair-populate.sh`, which patches only the pinned `open5gs-populate` init container in the exact lab context. `populate-subscriber.sh` queries the demonstration subscriber before inserting. Existing records are preserved, missing records are added, and database connection/query errors fail startup. Reapply after a Helm upgrade. This script is specific to the tested `gradiant/open5gs-dbctl:0.10.3` image and public lab credentials.

## Recover after an AMF or UPF fault

Run this from Ubuntu WSL in the CorePulse checkout:

```bash
bash scripts/recover-service.sh
```

This resets the lab's UE, gNB, SMF and UPF session state. It requires one replica for each workload and the exact lab context. It first runs `configure-pfcp.py`, which creates the lab-only headless `corepulse-upf-pfcp` service and changes the pinned SMF ConfigMap peer to that DNS name. This gives SMF a direct UPF pod address for PFCP instead of the chart's virtual service IP. The original ConfigMap is backed up once under ignored `reports/`; no chart service is deleted. Reapply after a Helm upgrade. An [upstream Open5GS 2.7.5 discussion](https://github.com/open5gs/open5gs/discussions/3997) reports related Kubernetes PFCP address/port behavior and a headless service workaround; the match to this lab is an inference tested locally, not a proved upstream root cause.

It then scales down UE, gNB, SMF and UPF, waiting for old pods to disappear, and starts UPF, SMF, gNB and UE in that order. Other core functions and the subscriber database are preserved. A failure during scaling attempts to restore all four replica counts to one. A replacement UPF has a new pod address: run recovery to restart SMF and resolve the current headless endpoint.

Readiness alone does not complete recovery. The script checks the current ready UE pod's registration and PDU-session log events and requires two consecutive 2/2 gateway packet checks. It exits 2 if workloads are ready but service health is not verified. This resets active demo sessions and belongs outside a recorded normal baseline or fault interval.

To roll back direct PFCP routing, replace `corepulse-upf-pfcp` with `open5gs-upf-pfcp` in the `open5gs-smf` ConfigMap, then restart SMF and the UE/gNB sessions. The original chart service remains available. The additive headless service can be removed after no workload uses it.
