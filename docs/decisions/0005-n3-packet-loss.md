# 0005: Scoped N3 downlink packet loss with owned rollback

**Status:** Proposed, for discussion in [#57](https://github.com/lazypanic999/CorePulse/issues/57). Nothing here is implemented.

The fault catalog's next entry is `packet_loss` on the user plane. This record narrows it to one direction and one protocol, states what the lab actually allows, and lists the questions that need an answer before any privileged helper is written. It is a design, not a claim that netem works in this lab.

## Scope

**Downlink N3 GTP-U egress loss on the pinned UPF pod:** a percentage of UDP packets the UPF sends from source port 2152 to the observed gNB N3 address are dropped, for a bounded duration.

Out of scope for the first version:

- uplink loss (gNB → UPF)
- bidirectional loss
- latency, jitter, reordering or corruption
- N2 (SCTP), N4 (PFCP, UDP 8805), DNS, Kubernetes API or kubelet traffic
- any other pod

A run that cannot prove its scope refuses before changing anything. The label is `packet_loss` with its parameters. It describes what was configured; the measured packet outcome is recorded separately and never replaced by the configured rate.

## What the lab shows (read-only, 24 November 2026)

All observations below came from `kubectl get` and from `kubectl exec` reads. No qdisc, filter, capability, image or workload was changed.

| | UPF `open5gs-upf` | gNB `ueransim-gnb` |
| --- | --- | --- |
| Pod network | Pod networking, `eth0` 10.244.1.131/16, default route via `eth0` | Pod networking, `eth0` 10.244.1.133 |
| N3 binding | `upfd` listens on `10.244.1.131:2152` (GTP-U) and `:8805` (PFCP), both on `eth0`; config uses `gtpu.server.dev: eth0` and `pfcp.server.dev: eth0` | Uses its pod address for N3 after `configure-gnb-n3.sh` |
| Container security context | `privileged: true`, `capabilities.add: [NET_ADMIN]` | `capabilities.add: [NET_ADMIN]` |
| Process identity | uid 999 (`open5gs`), PID 1 also uid 999 | root |
| Effective capabilities of an exec'd process | `CapEff` 0 (bounding set full) | `CapEff` 0xa80435fb, which includes `CAP_NET_ADMIN` |
| Tools | `ip`, `tc`, `tcpdump`, `iptables`, `nft`, `ss` | none of these |
| Current egress qdisc on `eth0` | `noqueue`, no filters | not observable without tools |

The UPF container is privileged, but its processes run as a non-root user without ambient capabilities, so neither `upfd` nor an exec'd shell can change qdiscs. `kubectl exec` has no option to run as a different user. The gNB has the capability but no tools. These facts match the 9 November observation in #57 and are specific to this chart and these images.

N3 shares `eth0` with N4 and all Kubernetes traffic, so a root `netem` qdisc on `eth0` would also drop PFCP heartbeats, DNS and API traffic. The change must be a filter that matches only GTP-U downlink.

## Proposed mechanism

On the UPF pod's `eth0` egress:

1. A `prio` root qdisc with a `handle` owned by CorePulse. Unmatched traffic goes through its default FIFO bands. That adds a queue where `eth0` currently has `noqueue`, so the 0 % validation run below must show that this alone does not change probe or PFCP behaviour.
2. A `netem loss <p>%` child on one band.
3. One `u32` (or `flower`) filter steering only `ip protocol udp`, `sport 2152` and `dst <observed gNB pod IP>/32` to that band.

Rollback deletes exactly the root qdisc whose handle and parameters CorePulse recorded. Removing the root also removes its children and filters. If the live state does not match the journal, rollback refuses and reports it instead of deleting someone else's configuration.

The interface is not assumed. It is resolved from the socket that `upfd` actually binds for GTP-U (`ss -uanp` showed `10.244.1.131:2152`) and the route to the gNB address, and the run refuses if they disagree or name more than one interface.

## How a privileged command reaches the pod network namespace

This is the open question. Every option needs `CAP_NET_ADMIN` in the UPF network namespace, and none of them is free.

| Option | What it needs | Rollback and cleanup | Concerns |
| --- | --- | --- | --- |
| A. Ephemeral debug container (`kubectl debug --target=open5gs-upf --profile=netadmin`) with a pinned tool image | Ephemeral containers enabled; a pinned image digest with `tc` | The qdisc can be removed. The ephemeral container cannot: it stays in the pod spec until the pod is replaced | Adds a lasting, visible change to the pod; image provenance; the pull may need network access |
| B. Node-level `nsenter` into the pod's network namespace through `minikube ssh` | Root on the Minikube node, resolving the container's PID through the runtime | No change to the workload; only the qdisc to roll back | Minikube/Docker-driver specific; host-level root, outside Kubernetes RBAC; harder to pin to the exact pod |
| C. Long-lived root sidecar with `NET_ADMIN` | A chart or Deployment change | Changes the workload for every run | Broadens privileges permanently. Rejected: #57 forbids silently broadening workload privileges |
| D. Run the UPF as root | Image or chart change | Same as C | Rejected for the same reason |

**Recommendation for discussion:** A, with a pinned image digest and an explicit typed confirmation that names the lasting ephemeral container, or B as a lab-only path if a lasting pod-spec change is unacceptable. C and D are out.

## Safety model

These reuse the existing pod-crash gates and add network-specific ones.

- **Dry-run first.** `corepulse packet-loss --target upf --direction downlink --loss 5 --duration 60s` prints:
  - direction, protocol and port, percentage, duration
  - the resolved pod, container, interface, gNB address and filter
  - the mechanism and the capability it needs
  - the exact apply and rollback commands

  Invalid values are rejected before any cluster access: loss outside 0–100 (exclusive), duration outside a bounded range, or an unknown direction or target.
- **Read-only preflight** before `--apply`:
  - context and namespace
  - the UID-verified Deployment → ReplicaSet → Pod chain (#65)
  - container name and interface
  - tool availability and the effective capability through the chosen mechanism
  - an empty or exactly recognised root qdisc, with no foreign filters

  Foreign state refuses; it is never replaced. The run also requires shared collection ownership, the protocol starting gate (N2 and N4 up) and a healthy packet probe.
- **Journal before change.** The journal is persisted before the qdisc is created and records:
  - the pinned pod UID
  - the interface
  - the qdisc handle and parameters
  - the mechanism, including the ephemeral container name for option A
  - the intended expiry

  Phases are checkpointed like pod-crash.
- **Rollback** removes only the journalled qdisc, in the same pod UID, and is verified by reading the qdisc state back. It runs on:
  - normal completion
  - cancellation
  - SIGINT, SIGTERM or SIGHUP, through the same forwarding model as `corepulse hold`

  Nothing claims to survive SIGKILL. After a forced kill the ownership marker and the journal remain, and an explicit `corepulse packet-loss --recover <journal>` re-reads live state, removes only a matching qdisc and records the result.
- **Container replacement.** If the pod UID changes during the run, the qdisc went away with the old network namespace. The run records that and does not touch the replacement.
- **Outcome.** Consecutive packet probes after rollback and an N2/N4 check are both required. The run is `excluded` if any step is unknown.

## Validation plan

1. **Fixtures and fake commands only.**
   - dry-run output and parameter rejection
   - preflight refusals: foreign qdisc, wrong interface, missing capability, UID mismatch
   - a journal written before apply
   - rollback of owned state only
   - interruption at each phase, and recovery after a simulated kill
2. **Isolated-lab read-only run of the chosen mechanism.** Show that the capability and tools are available and that the planned filter matches only GTP-U, using counters with a 0 % loss netem, before any loss is applied.
3. **Isolated-lab loss runs.** Low rates first. Measure downlink loss with probe counters and `tc -s` drop counts, confirm N2 and N4 stay up, and require consecutive healthy probes after rollback.

## The same question for the rest of the catalog

The roadmap's other two network faults hit the same obstacle, so one answer to the mechanism question should cover all three.

- **`high_latency`** would reuse this design unchanged: the same CorePulse-owned root, the same GTP-U downlink filter, and `netem delay <d> [<jitter>]` in place of `loss`.
- **`sctp_drop` (N2)** would need a filter on the other side of the N2 link.

Read-only facts for N2 (24 November 2026):

- The AMF pod sets `runAsNonRoot: true` and `runAsUser: 999` without the `privileged` flag or any added capability. `open5gs-amfd` and exec'd processes have `CapEff` 0. The image does include `ip`, `tc`, `ss`, `iptables` and `nft`.
- NGAP listens with SCTP on `10.244.1.126:38412` on `eth0`. The single association is with the gNB pod `10.244.1.133`.
- The AMF's `eth0` has `noqueue` and no filters.
- The gNB side has `CAP_NET_ADMIN` but no tools, as recorded above.

A scoped N2 fault would therefore match `ip protocol sctp` with port 38412 between those two addresses. It faces the same choice between a lasting ephemeral container and node-level `nsenter`. It has one extra risk: a lost N2 association does not come back by itself in this lab (see `docs/lab-integration.md`), so rollback must be followed by the documented reset and an N2 check.

## Questions for the maintainer

1. Is a lasting ephemeral container on the UPF pod (option A) acceptable for a lab experiment, or should the first version use node-level `nsenter` (option B)?
2. Which tool image and digest should be pinned if A is chosen?
3. What loss range and maximum duration should the dry-run accept?
4. Should the dataset label carry the configured rate as a parameter column, so per-rate sweeps can be split without one-value-per-class leakage (roadmap v0.4.0)?
