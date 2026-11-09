# Prospective collection sessions and whole-session holdouts

An optional `--session-id` declares which collection occasion a new baseline or fault belongs to. Set it **before collection**, consistently across the CLI/dashboard calls in that occasion. It is not a process ID, a new random label per run or proof of independent network conditions. Describe configuration, node/UE, collection conditions and deliberate session boundaries in the dataset card. Do not manufacture historical sessions from timestamps, run names or file modification times.

## Collect explicitly

```bash
corepulse baseline --runs 3 --runs-dir runs/session-study --output data/session-study/samples.csv --session-id lab-occasion-A
corepulse pod-crash --target upf --runs-dir runs/session-study --session-id lab-occasion-A
corepulse serve --addr 127.0.0.1:8790 --runs-dir runs/session-study --evaluation reports/session-study/evaluation.json --session-id lab-occasion-A
```

The pod-crash command above is a dry-run; add `--apply` only for the reviewed isolated-lab experiment after its guards pass. Use the documented service reset between faults. Reuse the declared ID throughout one occasion, including verified restoration steps; choose a new ID for a genuinely separate prospective occasion, not merely to increase the group count. Dashboard requests use the server's startup declaration, never a request-body override. Its ownership view shows which session future runs will declare.

IDs contain 1–64 ASCII letters/digits/`._-`, beginning with a letter or digit. Empty is allowed for backward compatibility and means **unknown**. Invalid declarations refuse before collection/client calls. Initial, interrupted, cancelled and final checkpoints preserve the same declaration. Collection ownership, context/namespace pinning, protocol/workload/packet gates and exclusion rules still apply.

Keep all prospective study records in the same separate `runs/session-study` directory across occasions A/B/C, rather than mixing historical unknown-session records into it. The collectors above use the normal shared raw telemetry file; window export selects only intervals from that study directory and excludes other captures. Existing default run records and saved evaluation are not overwritten by this recipe. Repeat all needed scenarios in genuinely separate occasions with the appropriate declared ID; three baseline runs in occasion A still count as one session.

## Export and split

```bash
corepulse windows --telemetry data/telemetry.jsonl --runs-dir runs/session-study --output data/session-study/windows.csv
corepulse split --input data/session-study/windows.csv --output data/session-study/split.json --group-by session --seed 42 --test-fraction 0.25 --embargo 2m
corepulse evaluate --input data/session-study/windows.csv --split data/session-study/split.json --output reports/session-study/evaluation.json
```

When an eligible run declares a session, window CSV adds `session_id` immediately after `protocol_start_verified`. Unknown rows in mixed inputs have empty session cells. The numerical feature allowlist is unchanged: session, run, target, labels and timestamps are metadata, not fitted inputs. Readers accept the exact legacy and session-aware headers and reject conflicting session declarations within one run.

**Select `--group-by session` explicitly.** The default remains `run`, even when input includes sessions. Run mode stratifies whole runs by scenario and does not establish cross-session validation. Session mode orders whole declared session IDs using the fixed seed, holds out the requested fraction of groups (rounded up, leaving a training group), and never moves individual runs across the boundary. Its fraction counts sessions, not windows or runs; it does not stratify by splitting a multi-scenario session.

Every scenario needs at least two declared sessions and actual retained/test support after selection and purging. Unknown sessions refuse, even with `--allow-legacy`; that flag concerns protocol provenance, not missing session identity. If the selected session holdout omits a scenario or the embargo removes its training support, the command refuses and preserves the prior manifest. Collect broader separated sessions rather than selecting a seed against test scores.

An embargo purges **the whole training session** when any member run is too close to a held-out window range. The other, more distant runs in that training session are also purged. Held-out selection is unchanged. Session IDs never cross retained/test/purged roles.

## Manifest, evaluation and compatibility

Session mode writes schema **2**, with `grouping: session`, the complete run-to-session assignment map and train/test/purged session and run lists. The exact CSV hash binds the metadata and numerical values. Evaluation verifies assignments against the CSV, all group roles, protocol provenance, class counts and temporal separation before fitting. Its report is schema 2 and retains the grouping. Saved-report validation checks declared session consistency without refitting or authenticating source data.

Existing run manifests/reports remain schema **1**. Legacy raw records remain unknown, not retagged. When no eligible run declares a session, window export keeps the exact legacy header/output behavior; the frozen original reference CSV and numerical results remain reproducible. Merely renaming a high-scoring within-session report as session-grouped cannot pass validation if one session straddles both roles.

Group disjointness is only as sound as the declarations and study design. It does not prove independent configurations, causal availability of all features, disjoint underlying CPU averages or generalization to another network. Capture-time embargoes still have the [source-measurement limits](windowed-dataset.md#optional-capture-time-embargo). Training-only preprocessing, all held-out mistakes/abstentions and window/run metrics remain unchanged. The saved dashboard states its grouping explicitly; it does not serve live predictions.

The [synthetic walkthrough](../examples/offline/README.md) contains three invented session blocks and exercises both modes without a cluster. Its scores are not measured lab performance. The original local 22-run study predates session declarations and cannot be converted into cross-session evidence by this feature.
