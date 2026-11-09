# Offline detection and classification baselines

The `evaluate` command fits deliberately simple resource baselines on training runs and reports predictions on held-out runs. It also compares two trivial rules. It does not deploy a model or add live dashboard predictions.

```bash
go run ./cmd/corepulse windows --output data/windows.csv
go run ./cmd/corepulse split --input data/windows.csv --output data/split.json --seed 42
go run ./cmd/corepulse evaluate --input data/windows.csv --split data/split.json --output reports/evaluation.json --z-threshold 3
```

The exact window CSV SHA-256 must match the split manifest. The evaluator rejects duplicate/overlapping train/test IDs, missing or extra membership, incorrect class counts, unknown schemas and changed feature allowlists. Each scenario must have independent train and test runs, and at least two normal training windows must exist. Protocol-start evidence is required unless the split and evaluator both explicitly permit legacy exploration. Invalid evaluation input leaves the previous report intact.

## Methods

| Method | Training | Output |
| --- | --- | --- |
| `nearest_centroid` | Training-only median imputation and standardization; one centroid per scenario | `normal`, `pod_crash:amf`, or `pod_crash:upf`, for scenarios present in the split |
| `majority_scenario` | Most frequent training-window scenario | Same scenario for every test window |
| `normal_zscore` | Normal training windows only; median imputation, means and scales | `fault` if any absolute standardized resource deviation exceeds the configured threshold, otherwise `normal` |
| `probe_rule` | No fitting | `fault` if mean received packets are fewer than sent; `normal` otherwise; abstains if either value is absent or no packets were sent |

Fitted resource inputs are pod-count mean/delta, total CPU mean/max/delta, total memory mean/delta, AMF and UPF CPU means, and AMF and UPF CPU missing ratios. Run identity, label, target, window duration, recovery results and timestamps are never fitted inputs. Protocol-event features are intentionally excluded because post-run log collection affects their availability; the current benchmark cannot establish that those features are causal online inputs.

Features entirely absent in the relevant training partition are dropped and listed. Other absent values use that partition's median. The centroid model uses population standard deviation, with scale one for constant training features. The normal-only model uses `max(population_stddev, 0.05 * max(abs(training_mean), 1))` as its scale to avoid dividing by zero or treating any tiny numerical change as an infinite deviation. Threshold three is a fixed initial rule, not a tuned or statistically calibrated probability. All medians, means, scales and centroids are retained in the report.

## Reading the report

If the split declares a capture-time embargo, purged runs are excluded from both fitting and scoring and `purged_windows` accounts for their rows. The evaluator verifies separation from actual window ranges rather than trusting the manifest's declared margin. Scenario and binary F1 use different label sets and should not be ranked as if they were the same task. Disjoint run IDs and a time margin still do not establish cross-session generalization or certified measurement-time independence; see [source timing limits](windowed-dataset.md#optional-capture-time-embargo).

The JSON records the source hash, full split, training/test window support, normal training support, methods, parameters and every held-out prediction. Incorrect predictions remain present. Reports are deterministic for identical input, split and options; no current timestamp is injected into their contents.

Each method has window-level and run-level confusion matrices, accuracy, per-class precision/recall/F1, support, and macro F1. Confusion rows are truth and columns are prediction in the reported label order. Run predictions use the most common non-abstaining window prediction; ties choose the first label in that order. A run with no prediction remains abstained.

Abstentions retain true-class support and count as missed recall. `accuracy_on_predicted` excludes abstentions, so always read it with `coverage`, `predicted`, and `abstained`. A rule that predicts only easy cases should not appear competitive because its missing cases disappeared.

## What these numbers do and do not establish

The truth is the known experiment interval, not an assertion that every instant in it was unhealthy. For example, an AMF experiment can keep forwarding packets while N2 breaks later; the packet rule should miss some AMF labels. That mismatch is visible rather than relabelled away.

Windows from a run never cross partitions. Runs still share one node, one UE, lab configuration and collection session, so this is not an external validation set. Long runs contribute more training windows, partial/sparse windows remain included, and simple models can learn collection effects. More independent sessions, additional fault parameters and a larger predeclared test set are needed before making accuracy or generalization claims. Choose seeds and thresholds before inspecting test results and preserve poor results instead of rerunning seeds for a better score.

The Go implementation has no new model/runtime dependencies. It is an initial reproducible reference; choosing a runtime for more capable models and serving predictions remains separate future work.

## Dashboard snapshot

`corepulse serve` reads `reports/evaluation.json` for a read-only **Offline evaluation** section. Use `--evaluation <path>` to select another saved report. The section shows held-out support, window/run F1, coverage, mistakes and abstentions, plus an expandable table with every prediction and the source CSV hash. It never substitutes current live probes for held-out predictions or fits a model during a request.

Missing, incomplete or unsupported reports show an explicit unavailable state instead of zero scores. The endpoint `/api/evaluation` is read-only, works without Kubernetes and shares the dashboard's loopback-host restrictions. Re-run evaluation to update the snapshot. The section is not live model serving.


The saved snapshot is checked for internal consistency before display: all four reference methods must retain the same held-out window identities and compatible truths. Split roles must be disjoint, class support must match, and window/run confusion matrices, class scores, coverage and abstentions are recomputed from predictions. Inconsistent snapshots show unavailable rather than displaying their stored scores. This check does not refit or authenticate the models, source data, truth labels, claimed hash or measurement-time independence; a self-consistent report can still describe a flawed study. Original frozen results remain reproducible with their pinned release evaluator; updated evaluator limitation text can change report bytes across source versions.

For a no-cluster walkthrough, use [examples/offline](../examples/offline/README.md). Pass `evaluate --synthetic` for invented inputs: the report retains `synthetic: true`, an explicit limitation and a dashboard warning. This declaration does not change fitting and is not automatic provenance verification.
