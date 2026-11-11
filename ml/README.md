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
Serialized capture-time margins recover whole nanoseconds without float-to-int
truncation; sub-nanosecond margins and malformed timezone offsets refuse.

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
uv run --project ml --locked python -m corepulse_ml.experiment ml/experiments/synthetic-smoke.yaml --export-dir reports/neural-synthetic/models
```

Preparation accepts only marked invented examples and runs `windows`, a session
`split`, and `evaluate --synthetic`, with an empty executable search path in the
offline child processes. No cluster CLI, server or live input is used. Generated
reports stay under ignored `reports/neural-synthetic/`; source inputs are preserved.

Experiment YAML has strict keys, unique fixed model seeds and a separate inner
validation seed. The report is schema 3 with an unchanged schema-1/2 outer split.
The declared inner-fold count is bounded to 2..32, and impossible group counts
refuse before allocating fold buckets. JSON exponent overflow and oversized
integers in floating-point fields are validation errors, never valid infinities.
All four Go reference methods remain required. Extra named methods retain every
held-out prediction and recomputed window/run confusion, per-class F1, macro F1,
coverage and abstentions. Seed-family summaries contain mean, population standard
deviation, minimum and maximum; different prediction tasks are never pooled.
Bad input/metrics refuse before replacing a previous report. The smoke configuration
adds an MLP for each declared seed to the four references. The YAML method list may
also be empty for a reference-only consistency check. CI creates inputs from the
marked invented example, trains on CPU and passes the saved Python report through
the Go snapshot validator. These are invented development results, not lab
performance or evidence of independence. The `synthetic` field is an explicit
declaration, not an automatic detector of where data came from.

## CPU MLP implementation

`corepulse_ml.mlp.train_mlp(dataset, folds, configuration, seed)` trains a small
dense/ReLU scenario classifier. Configuration declares one to three hidden
layers (at most 256 units each), increasing epoch candidates (at most 1,000),
learning rate and weight decay. Every outer-training row must be held out in
exactly one inner validation fold. Each inner preprocessor fits only that fold's
fit rows. Mean fold validation cross entropy selects the epoch budget; an exact
tie selects the earlier epoch. No outer-test scores select epochs or seeds.

The final network and preprocessor refit all outer training for that fixed epoch
budget, then produce every outer-test prediction. Training equally weights
windows, so longer runs contribute more examples. CPU RNG and deterministic-mode
settings are scoped and restored. Fixed seeds make this environment repeatable;
they do not promise bitwise equality across every future platform/library build.
Reported maximum softmax values are not calibrated probabilities. Training
refuses non-finite loss, gradients, parameters or predictions. Synthetic unit
tests verify repeatability, disjoint groups and that changing only held-out
values cannot change epoch selection, preprocessing or trained parameters.

## Normal-only autoencoder

The smoke YAML also declares a CPU dense/ReLU autoencoder for every seed. Its
encoder widths, fixed epoch count, calibration fold and percentile are declared
before errors exist. Decoder widths mirror the encoder; the final reconstruction
layer is linear. The bottleneck must be smaller than the retained input dimension.

Only normal rows from that inner fold's fit groups enter preprocessing and weight
training. Separate normal validation groups calibrate an empirical reconstruction
MSE threshold with NumPy's `percentile(..., method="higher")`; equality remains
normal and only a strictly larger error predicts a fault interval. Require at least
two normal windows on each side. Calibration rows are never refitted into weights
or preprocessing afterward. Fault-training values and outer-test values cannot
affect parameters or threshold. All held-out normal/fault predictions remain in
the report, with calibration support, normal run identities and threshold policy.

This tiny synthetic calibration demonstrates code paths only. Its empirical
percentile does not guarantee a false-positive rate, independence, a calibrated
anomaly probability or live service-health detection. The fixed calibration fold
uses fewer outer-training rows than the MLP's final refit; do not compare training
support as if the procedures were identical. A different declared configuration
requires a fresh experiment, not tuning against outer-test results.

## JSON coefficient export

`--export-dir` writes one versioned JSON artifact per method/seed and includes
each file's SHA-256 in the report. Only ignored `reports/`, `artifacts/`, `models/`,
`ml/outputs/` or paths outside the repository are accepted. Input aliases refuse;
each file is replaced atomically after complete validation. Replacing several
artifacts and the report is not a cross-file transaction: after an I/O failure,
check the report's hashes or rerun the fixed experiment. Never commit trained
coefficients. CI's generated files use invented data only and are not uploaded.
Report/model output paths, input aliases (including hardlinks), existing directories
and cross-output collisions are checked before training or creating artifacts.

Schema 1 contains CPU float32 dense/ReLU coefficients, explicit feature order,
training-only medians/means/scales, dropped features, fit run IDs, source CSV/split
hashes, task/labels and seed. Autoencoders also retain their calibrated threshold
and disjoint normal calibration run IDs. Unknown operators, dimensions, versions,
metadata features, non-finite values and nonpositive scales refuse. This format
contains no optimizer state or executable deserialization. See the
[artifact contract](../docs/neural-artifacts.md) for limits and interpretation.

The standard-library-only Go package `internal/inference` loads these artifacts
and evaluates them offline with cgo disabled. The ML test suite requires Go (or a
fresh cross-built Linux test binary via `COREPULSE_GO_PARITY_BINARY`) and actually
compares Go outputs with CPU PyTorch on temporary hand-authored fixtures at
absolute tolerance 1e-5. Models stay separate from the live dashboard/controller.

After the synthetic smoke report exists, Node 24 can check the actual dashboard
render functions without a browser, server or network connection:

```bash
node --test scripts/tests/dashboard-evaluation.test.cjs
```

DOM fixtures verify all reference/neural methods and held-out rows, synthetic
warnings, seed spread, mistakes/abstentions, schema-1/2 compatibility and clearing
stale rows. The synthetic CI job runs this after generating and validating the
invented report. This is functional rendering coverage, not a visual browser audit.
