# Prospective multi-session cohort: collection protocol

**Status:** Proposed for review in [#62](https://github.com/lazypanic999/CorePulse/issues/62). No occasion has been collected under it. Collection starts only after the maintainer accepts or amends this file, and the accepted version is committed **before occasion A** so nothing below can be tuned against results.

The [9 November cohort](2026-11-09-local-preview.md) is one occasion with unknown session declarations. This protocol collects new evidence for whole-session holdouts ([semantics](../session-holdouts.md)). It does not relabel old runs.

## Occasions

| Session ID | Separation from the previous occasion |
| --- | --- |
| `prospective-2026-A` | first occasion |
| `prospective-2026-B` | a different calendar day; Minikube profile stopped and started again; WSL session restarted |
| `prospective-2026-C` | the same separation again |

An occasion is one continuous collection period on one day, with one declared ID used for every baseline, fault and reset in it. A new process, timestamp or label does not make a new occasion. If an occasion is abandoned part-way, its ID is retired, recorded as abandoned, and not reused.

The separation above changes process state, pod identities and time of day. It does not change the node, UE count, chart, images or configuration, so the sessions are not independent networks. The dataset card must say so.

## Per-occasion plan

All commands use `--runs-dir runs/session-study` and `--session-id <ID>`. The run order is fixed:

1. Start the lab with `.\scripts\start-lab.ps1`. Run `scripts/recover-service.sh` and require exit 0.
2. **Baselines:** `corepulse baseline --runs 4 --samples 6 --interval 10s --metrics-wait 90s`.
3. **AMF fault**, then reset; **UPF fault**, then reset; **AMF fault**, then reset; **UPF fault**, then reset. Each fault is `corepulse pod-crash --target <amf|upf> --apply`. Each reset is `scripts/recover-service.sh` and must exit 0 before the next step.
4. **Closing baselines:** `corepulse baseline --runs 2 --samples 6 --interval 10s --metrics-wait 90s`.
5. Stop the lab with `.\scripts\stop-lab.ps1`.

That gives at most 6 normal, 2 AMF and 2 UPF runs per occasion, and at most 18/6/6 over three occasions. Every gate stays on:

- shared ownership
- context and namespace pinning
- the UID-verified target chain
- healthy workloads and packets
- N2 and N4 up at the start of every run

A refused or excluded run is **not** retried to fill a quota. It is logged, and the plan continues with the next step after the documented reset. If an occasion ends with zero qualifying runs of any scenario, the cohort cannot be evaluated in session mode. The report says so instead of adding a fourth occasion chosen after seeing results.

## Recorded per occasion

In `reports/session-study/<ID>/notes.md` (Git-ignored), written during collection:

- date, start and end times, timezone
- CorePulse commit and binary version
- Open5GS and UERANSIM image tags
- Minikube and kubectl versions
- node CPU and memory
- UE count
- every attempted command with its exit code, run ID, status, quality and exclusion reason
- each reset's exit code
- any manual intervention, verbatim
- the `corepulse doctor` output before the first run and after the last reset

## Fixed analysis parameters

These are fixed now. They match the 9 November reference where possible, so the two cohorts can be compared.

| Parameter | Value |
| --- | --- |
| Window export | `corepulse windows --window 30s` (strict protocol provenance; no `--allow-legacy`) |
| Split | `corepulse split --group-by session --seed 42 --test-fraction 0.25 --embargo 2m` |
| Held-out sessions | `ceil(3 × 0.25) = 1` of three sessions, chosen by the fixed seed |
| Methods | the existing `corepulse evaluate` set: majority scenario, nearest centroid, normal-only deviation (threshold 3), packet rule |
| Features | the existing numerical allowlist; session, run, target, labels and timestamps stay metadata |
| Source timing | `corepulse source-frames` (#44) is run on the raw telemetry and reported. No frame deduplication, freshness cutoff or measurement-time split is applied in this cohort |

If the split refuses (for example because one scenario lacks training support in two sessions), the refusal is the result. The seed, fraction and embargo are not changed to make it pass.

## Reported

A dataset card, `docs/datasets/<date>-prospective-sessions.md`, plus a machine-readable summary, with:

- support per scenario, per session and per role (train, test, purged)
- window and run confusion matrices for every method, coverage, and all mistakes and abstentions
- majority and packet-rule comparisons, with scenario and binary tasks kept separate
- AMF misses stated explicitly, and every method that loses to a simple rule named as such
- source-frame summary per component: unique frames against captures, median age
- every exclusion and abandoned occasion

Raw telemetry, run records and notes stay local. Publishing any raw data is a separate review for private configuration. Public aggregates alone are not an independently reproducible dataset.

## Questions for the maintainer

1. Is a different calendar day plus a Minikube and WSL restart enough separation, or should occasions also differ in something deliberate (time of day, UE restart policy)?
2. Are 6 normal, 2 AMF and 2 UPF runs per occasion the right size, given about 50 minutes of lab time per occasion?
3. Keep `--test-fraction 0.25` (one held-out session), or collect four occasions so two can train?
