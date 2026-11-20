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
