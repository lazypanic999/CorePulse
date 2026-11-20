# Offline neural workspace

Python **3.12**, Linux x86-64 and an explicitly selected **CPU-only** PyTorch index.
`uv.lock` pins transitive dependencies and wheel hashes. No CUDA libraries, cluster
configuration, live collector or fault runner is needed. Python models remain offline;
the Go control plane and its existing validation are unchanged.

From the repository root, with uv 0.13.7 installed:

```bash
uv sync --project ml --locked --python 3.12
uv run --project ml --locked ruff check ml
uv run --project ml --locked pytest ml
uv run --project ml --locked python -m corepulse_ml.smoke
```

The tensor inputs and results are explicitly invented. These commands do not measure
lab accuracy. Runtime inputs must come from the documented hash-bound window/split
pipeline; do not fit on test rows or use run/session/label metadata as features.
Generated reports, caches and trained artifacts belong outside tracked source files.
Never commit telemetry, collected run records, checkpoints or trained weights.

Linux-only ML support is an initial deliberate scope: a single locked CPU wheel
platform makes the synthetic CI environment explicit. Go inference remains portable;
additional Python platforms require separate lock/install and parity validation.

## Window loading and preprocessing

`corepulse_ml.data.load_windows(csv, split)` accepts the exact legacy or
session-aware Go window schema, verifies the CSV SHA-256 and complete disjoint
run membership, class support, declared session ownership and capture-time embargo.
Protocol-start evidence is required; the neural workspace does not silently opt
into legacy exploration. Session metadata is not proof of real independence.

The default feature selection matches the eleven resource inputs of the Go
references. Metadata, probes and post-run protocol counters cannot be selected as
neural inputs. `fit_preprocessor(dataset)` uses only outer training rows; an
explicit subset must also belong entirely to outer training, for inner validation.
All-missing training features are dropped, remaining gaps use training medians,
and population means/scales are training-only. Observed zero remains distinct from
missing in the loaded rows. Test/validation rows can only be transformed afterward.
Go's nanosecond window identities are retained without datetime truncation.

## Grouped inner validation

`grouped_folds(dataset, n_splits=2, seed=17)` uses only the verified outer training
partition. Schema 1 groups whole runs, stratified by training scenario; schema 2
groups whole declared sessions, including every scenario they contain. Validation
groups are selected deterministically before scores exist. Unknown/insufficient
groups or lost class support refuse; no silent fold reduction or seed retuning.
The outer capture-time margin also purges entire near-validation training groups.
It retains its source-measurement limitations and does not certify independence.

Use `fit_inner_preprocessor(dataset, fold)` to fit medians/scales only on the inner
fit rows. Validation and outer test/purged rows never enter that fit. Fold hashes,
complete group membership and class support are revalidated before preprocessing.

## Reproduce the synthetic experiment

From the repository root in Linux, with Go and the locked workspace installed:

```bash
mkdir -p reports/neural-synthetic
go build -o reports/neural-synthetic/corepulse ./cmd/corepulse
python3 scripts/prepare-neural-synthetic.py --binary reports/neural-synthetic/corepulse
uv run --project ml --locked python -m corepulse_ml.experiment ml/experiments/synthetic-smoke.yaml
```

Preparation accepts only marked invented examples and runs `windows`, a session
`split`, and `evaluate --synthetic`, with an empty executable search path in the
offline child processes. No cluster CLI, server or live input is used. Generated
reports stay under ignored `reports/neural-synthetic/`; source inputs are preserved.

Experiment YAML has strict keys, unique fixed model seeds and a separate inner
validation seed. The report is schema 3 with an unchanged schema-1/2 outer split.
All four Go reference methods remain required. Extra named methods retain every
held-out prediction and recomputed window/run confusion, per-class F1, macro F1,
coverage and abstentions. Seed-family summaries contain mean, population standard
deviation, minimum and maximum; different prediction tasks are never pooled.
Bad input/metrics refuse before replacing a previous report. Until the subsequent
model branches register training methods, the smoke configuration evaluates only
the required references. These scores are invented development results, not lab
performance or evidence of independence.
