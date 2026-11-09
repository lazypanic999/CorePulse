# Windowed features and whole-run splits

`windows` and `split` work offline on existing telemetry and run records. They prepare a reproducible experiment benchmark; they do not train a model or turn experiment labels into proof of service degradation.

```bash
go run ./cmd/corepulse windows --window 30s --output data/windows.csv
go run ./cmd/corepulse split --input data/windows.csv --output data/split.json --test-fraction 0.25 --seed 42
```

## Which runs qualify

The existing curated-data gates still apply: correct lab provenance, `quality: clean`, a verified baseline or recovered deployment, and a completed interval. By default, windows additionally require a nonzero `pre_protocol_check.checked_at` no later than the run start, with both N2 and N4 recorded as up. Older records stay readable, but their starting conditions are not retroactively verified. The summary counts excluded legacy runs separately.

`--allow-legacy` on each command allows exploratory exports/splits that include older clean runs. The output retains `protocol_start_verified: false`, and the split manifest records that the strict gate was disabled. In particular, the mixed starting conditions identified in issue #27 make the older UPF run unsuitable as an independent UPF-only case. Re-collect verified starts for evaluation rather than silently treating missing evidence as healthy.

## Window schema

Windows are aligned to each run's own start and never cross its boundaries. Intervals use `[window_start, window_end)`; a sample exactly on a boundary belongs to the next window. The final interval is clipped to the run end and marked `partial` when shorter than the configured duration. Empty time windows are omitted. Duplicate timestamps within a run, overlapping run attribution, unsupported scenarios, malformed provenance, and negative or non-finite numeric observations are errors. Input and previous output files remain unchanged when validation fails.

Metadata columns are `run_id`, `label`, `target`, window bounds, first/last sample times, sample count, observed span, partial status, and protocol-start verification. These are not model features.

For each numeric resource/probe/event signal, four columns are exported:

| Suffix | Meaning |
| --- | --- |
| `_mean` | Mean of observed values only; blank when none were observed |
| `_max` | Largest observed value; blank when none were observed |
| `_delta` | Last minus first observed value in chronological order; blank with fewer than two observations |
| `_missing_ratio` | Fraction of captured sample rows with that signal absent; one means entirely unobserved |

Observed zero stays numeric zero. No values are interpolated or imputed. Missing ratios describe captured rows, not time coverage: inspect `sample_count`, `observed_span_seconds`, and the bounds to see sparse sampling. A full-duration window with one sample is still sparse; its span is zero and its deltas are blank.

Resource signals are pod count, total CPU/memory, and AMF/UPF CPU/memory. Probe fields carry the latest observation at or before each sample, following the curated CSV semantics; they are not a fresh probe at every resource timestamp. Protocol fields are cumulative counts since run start, only where log coverage proves that interval was observed. Their window deltas are differences between the first/last captured cumulative values, not complete event counts or rates for the whole window. Post-run log collection also affects evidence availability; resource-only and probe-only baselines should be reported separately before claiming protocol features support online inference.

## Split manifest

Splitting groups all windows from one `run_id` together and stratifies by experiment scenario: `normal`, `pod_crash:amf`, and `pod_crash:upf`. A seeded SHA-256 ordering selects held-out runs deterministically. Each scenario must have at least two independent runs; otherwise the command fails instead of splitting windows from the same run. The requested fraction is rounded up per scenario and capped to leave at least one training run. This is a minimum mechanical requirement, not evidence that a tiny test set is representative.

The JSON manifest records the schema version, exact input CSV SHA-256, seed, fraction, protocol requirement, numeric feature allowlist, disjoint train/test run IDs, and per-scenario run counts. Repeating the command on identical input with the same seed gives identical output. Rebuild the manifest if the CSV changes. Choose the seed before examining held-out results.

Future evaluators must fit transformations and models on training runs only, verify the CSV hash and split membership, and keep labels/target/run metadata out of model inputs. Report both window-level and run-level results, class support, missingness, and partial-window policy. Single-node/single-UE measurements and a small number of independent runs remain substantial limits.
