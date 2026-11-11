# Neural JSON artifact contract, version 1

These are local offline inference artifacts, not live-controller configuration.
The synthetic smoke run exports invented-development models only. No committed
fixture contains trained coefficients; parity fixtures use hand-authored numbers
generated into temporary files. Do not commit model JSON, checkpoints, telemetry,
collected run records or credentials.

The artifact root has exact fields:

| Field | Meaning |
| --- | --- |
| `schema_version` | Integer `1`; unknown versions refuse |
| `kind` | `mlp` or `autoencoder` |
| `dtype` | `float32` coefficients and neural layer outputs |
| `synthetic` | Explicit boolean declaration; not an automatic data-origin detector |
| `source` | Lowercase hexadecimal SHA-256 `windows_sha256` and `split_sha256` |
| `preprocessing` | Ordered `feature_names`, `medians`, `means`, `scales`, `dropped_features`, `fit_run_ids` |
| `layers` | Ordered dense/ReLU operators, with explicit coefficients |
| `labels` | Sorted supported scenario classes including normal, or `["normal","fault"]` for an autoencoder |
| `task` | `scenario_classification` or `normal_vs_fault_interval`, matching kind |
| `seed` | Nonnegative signed-int64 model seed |

An autoencoder additionally requires finite nonnegative `threshold` and nonempty
`calibration_run_ids` disjoint from its normal preprocessing fit identities. An MLP
has neither field. Full training/validation policy and predictions live in the
experiment report; source hashes bind declared input identities but are not
signatures or proof that model fitting followed that policy.
The Python exporter additionally checks labels against the verified cohort, full
outer-training preprocessing identities for the final MLP, and normal-only fit/
calibration identities for an autoencoder, including declared session separation
and the inner capture-time embargo. Standalone Go loading has no source
CSV and cannot independently authenticate those declared identities.

Features are a nonempty unique subset of the eleven resource inputs documented in
the ML workspace. Metadata, labels, targets, timestamps, probes and post-run event
counts are never neural inputs. Dropped features are unique, allowlisted and
disjoint from retained features. Every median, mean and scale has exactly one
entry per retained feature. Scales are finite and positive. Missing input uses the
stored training median; observed zero stays zero. Standardization is
`(observed_or_median - mean) / scale`, computed before conversion to float32.

Dense operators are `{op:"linear",weights:[[...]],bias:[...]}`. Weight layout is
**output by input** and computes `output[j] = bias[j] + sum(weights[j][i]*input[i])`.
Every row must match the previous layer width and bias count the output width.
ReLU is `{op:"relu"}` and follows a dense layer; the final layer is dense. No
other fields/operators are supported. Limits are 32 operators, 4,096 outputs per
dense layer, 1,000,000 total coefficients including biases and 16 MiB of JSON.
Coefficients must be finite and representable as float32.

An MLP's final width equals its label count. Stable softmax converts logits to
scores; the first label wins an exact argmax tie. These scores are not calibrated
probabilities. An autoencoder's final width equals its retained input dimension.
Mean squared reconstruction error is computed in standardized feature space;
only `error > threshold` predicts a fault interval, so equality is normal.
Reconstruction error is not an anomaly probability or a service-health proof.

Each exported file is fully validated before atomic replacement. A report records
its exact JSON-byte hash; multi-file publication is not transactional. Consumers
must verify an expected artifact hash when moving files between locations and
check the declared CSV/split hashes against their intended sources. A valid JSON
shape does not authenticate origin, parameters, session declarations or training
independence. There is no pickle, cgo dependency, model service or online
retraining requirement in this restricted format.
Destination-workspace detection keeps tracked-source protection when Python is
used from a wheel or from a different source archive; it does not assume an
editable install. The source-layout fallback retains protection if workspace
markers are absent. Trained outputs still belong in ignored directories.

## Go API and numerical checks

`internal/inference.Load(io.Reader)` validates the whole bounded JSON document
before returning a model. Duplicate keys, nulls, unknown/missing fields and
trailing JSON refuse; numeric null never silently becomes zero. `Metadata()`
returns copies of feature/label lists and declared source hashes. `Predict()`
accepts a `map[string]*float64` containing only retained feature names. Omission
or nil is missing; a pointer to `0.0` is observed zero. Negative deltas are valid,
other observed resource values must be nonnegative, and missing ratios lie in
`[0,1]`. Project the window's feature map before calling; metadata and unselected
features refuse. Neither inputs nor model state are changed by prediction.

`Result.Outputs` contains logits (MLP) or reconstructed standardized inputs
(autoencoder). `Scores` contains MLP softmax values only; `Score` is their maximum
or autoencoder reconstruction MSE. `Synthetic` propagates the artifact's explicit
declaration. The package does not load source CSVs, verify a report's artifact
hash automatically, update the dashboard, contact a cluster or execute commands.
No inference result authorizes fault injection or changes live service status.

Standardization uses float64 followed by float32 conversion. Dense products
accumulate in float64 in Go and each layer output converts to float32. This can
differ slightly from CPU PyTorch's float32 reduction order. Reconstruction
differences/squares and the final mean convert to float32. Non-finite intermediate
outputs refuse. Decisions extremely close to a threshold or a logit tie can be
sensitive to floating-point rounding; parity on bounded fixtures is not a
universal guarantee for arbitrary parameter magnitudes or unstable models.

From the repository root in Linux, with Go and the locked ML environment:

```bash
CGO_ENABLED=0 go test ./internal/inference
uv run --project ml --locked pytest ml/tests/test_go_parity.py -q
```

The Python test generates temporary hand-authored coefficients and computes
expected outputs with CPU PyTorch. It actually invokes the Go test, checks an
execution marker, compares logits/reconstructions, scores and class decisions
at absolute tolerance **1e-5**, and covers MLP/autoencoder with two and all eleven
features, missing inputs, observed zeros and deterministic bounded varied inputs.
It must fail rather than skip if Go is unavailable. Ordinary Go-only tests skip
the external fixture test when no Python fixture was supplied; the pytest parity
check always supplies one and verifies execution. No coefficient fixture is saved
to tracked source files.

When Python runs in WSL and Go is installed only on Windows, cross-build the same
test binary with `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c -o PATH
./internal/inference`, then set `COREPULSE_GO_PARITY_BINARY` to its Linux-visible
absolute path for pytest. Rebuild it after Go changes. Native Linux CI uses its
installed Go compiler with module downloads and toolchain auto-download disabled
inside the parity subprocess.

The full ML suite additionally trains both methods with two fixed seeds on
temporary invented inputs, exports four artifacts, verifies their exact report
hashes in Go and compares all 24 held-out predictions/scores with the original
trained PyTorch report. It repeats this check for run-split schema 1 and
session-split schema 2 (48 predictions in total), with the same 1e-5 numerical
tolerance. These temporary training outputs are distinct from the hand-authored
coefficient parity fixtures and are never committed. This checks export/inference
consistency, not detection quality or live readiness.

For a bounded parser exercise, `CGO_ENABLED=0 go test ./internal/inference -run
'^$' -fuzz '^FuzzLoadArtifact$' -fuzztime=30s -parallel=2` mutates hand-authored
JSON seeds. Fuzz inputs are capped at 64 KiB for workload control; separate unit
tests exercise valid-document byte limits, layer/width/coefficient bounds and
numerical overflow. This is robustness testing, not an exhaustive security proof.
