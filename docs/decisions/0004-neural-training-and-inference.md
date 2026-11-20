# 0004: Offline CPU neural training with portable Go inference artifacts

**Status:** Accepted for the synthetic development prototype; live deployment and
real-lab model acceptance remain unapproved.

**Date:** 11 November 2026

## Context

[Decision 0003](0003-offline-baseline-runtime.md) keeps four deliberately small
reference methods in Go. A neural experiment needs gradient training and careful
validation without adding Python, cgo or a model server to the Go controller.
Neither invented example scores nor a saved report establish detection quality.
Run/session identity, targets, collection timing, probes and post-run event
availability can leak labels; larger models do not remove that risk.

## Decision

Train offline using Python 3.12 and CPU-only PyTorch in `ml/`, with exact direct
dependencies, a hash-locked transitive environment, Ruff and pytest. Initially
support Linux x86-64 only, matching the synthetic CI platform. Keep the existing
Go reference algorithms and their guards intact. CI and this development session
use invented fixtures only; no collector, fault runner or Kubernetes access is
part of neural training or inference tests.

Consume source-hash-bound CSV and split manifests. Require the established strict
protocol-start gates, complete disjoint run ownership, verified declared session
ownership for split schema 2, and the existing capture-time embargo. These checks
do not authenticate declarations or prove source-measurement independence.
Model inputs use an explicit resource-feature allowlist; metadata, packet-probe
outcomes and post-run event counters are not neural features. Preserve missing
values separately from observed zero. Imputation, scaling and dropping
all-missing features are fitted on the relevant training partition only.

Use deterministic grouped inner validation inside outer training: whole runs for
split schema 1 and whole declared sessions for schema 2. Apply the capture-time
embargo to inner groups too. Refuse insufficient groups or missing class support;
never silently reduce folds, retune split seeds or inspect outer test scores to
select a configuration. An MLP may refit on all outer training after its epoch
budget is selected using inner validation. Fix all experiment/model seeds before
evaluation and report every seed, rather than publishing the best seed.

A normal-only autoencoder fits its network and preprocessing on normal inner-fit
rows. Calibrate its threshold using a declared percentile of reconstruction
errors on disjoint normal validation groups. Do not refit those validation rows
into the network and then reuse their errors as held-out calibration. Keep
threshold equality on the normal side. No fault labels or outer-test errors may
select that threshold. This is an offline interval detector, not proof of packet
service health or calibrated anomaly probability.

Experiment YAML declares sources, fixed seeds, grouped validation and methods.
Report schema 3 retains all four Go references and every held-out prediction,
including mistakes and abstentions. Its embedded split remains schema 1 or 2.
Report window/run confusion matrices, per-class F1, macro F1, coverage and
per-seed population spread. Spread is descriptive, not a confidence interval.
Snapshot validation checks internal consistency; it cannot certify that training
was honest or that the data is independent. Schemas 1 and 2 retain their original
strict four-reference contract. Synthetic reports retain a prominent dashboard
warning and must not be described as real-lab performance.

For the small feed-forward prototypes, export a versioned JSON artifact with an
explicit feature order, training-only preprocessing parameters, source/split
hashes, supported dense/ReLU layers, task and output interpretation. Implement
evaluation of that restricted artifact using the Go standard library, without
cgo. Validate versions, feature names, finite numbers, dimensions and positive
scales before inference. Unknown architectures/operators require a new supported
contract; never fall back to arbitrary Python execution, pickle or executable
deserialization. Generated weights remain ignored local artifacts. Committed
parity fixtures use hand-authored coefficients, not trained parameters.

Go inference must match CPU PyTorch on invented inputs, missing values and
observed zeros within an absolute tolerance of **1e-5**, with version/dimension/
non-finite rejection tests. Float64 accumulation in Go and float32 PyTorch may
differ slightly; this tolerance is a numerical contract for tested artifacts,
not a performance or universal stability claim. Tensor layout and preprocessing
must be identical before comparing predictions or reconstruction errors.

## Consequences and alternatives

Training has a separate reproducible Python environment; the Go executable keeps
its deployment footprint. A constrained JSON format is easy to inspect and test
but deliberately supports fewer models than ONNX or a dedicated inference
runtime. Consider those only when an actual architecture requires them, with
dependency, portability and parity evidence. A Python model service remains an
alternative, with additional deployment and failure modes that this prototype
does not need. No claim of superior speed or accuracy follows from this choice.

Live inference integration, online retraining, new fault types, real-cohort model
selection and release acceptance are outside this decision. A future live check
must be separately authorized and compare all four references on adequate
independent sessions; it must also evaluate source timing, missingness, class
support, mistakes and service-health semantics. Passing synthetic tests does not
authorize controller actions or establish live readiness.
