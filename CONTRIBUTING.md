# Contributing

## Find a starting point

Browse [good first issues](https://github.com/lazypanic999/CorePulse/issues?q=is%3Aissue%20is%3Aopen%20label%3A%22good%20first%20issue%22) for small, scoped contributions or [help wanted](https://github.com/lazypanic999/CorePulse/issues?q=is%3Aissue%20is%3Aopen%20label%3A%22help%20wanted%22) for larger tasks. Each issue lists acceptance criteria, relevant files and whether a live lab is required. Comment with the issue you want to work on and a short approach so contributors can coordinate.

- Start with the completed [synthetic offline evaluation walkthrough](examples/offline/README.md): all three pipeline commands work without Kubernetes.
- A current no-lab starter is [source-frame reuse and measurement-age diagnostics (#44)](https://github.com/lazypanic999/CorePulse/issues/44), using JSONL fixtures.
- For larger dataset/evaluation changes, browse the open help-wanted list above and discuss grouping, source-time and missing-data policies before implementation. [Collection ownership](docs/collection-ownership.md) is already implemented and provides subprocess-test examples.

Fork the repository, create a focused branch and link the issue in your pull request. Keep documentation and issue discussions in English. For the local lab lifecycle and reset procedure, see [lab operations](docs/lab-operations.md).

Check that an issue is still open and read its linked pull requests before starting. Durable checkpoints, cancellable baselines, and missing-metric CSV handling have already been implemented.

## Quickstart without a Kubernetes lab

You can build, test, and preview CorePulse without Open5GS, UERANSIM, or a cluster. Live fault experiments are covered separately in [lab operations](docs/lab-operations.md).

### Prerequisites

- The release Go toolchain in `.go-version` is recommended. Linux CI also checks the `go.mod` source minimum (1.22); current macOS needs a linker from Go 1.24 or newer.
- Git.
- `kubectl` is optional. Without it, or without a reachable `open5gs-lab` context, lab commands fail with an explicit error and the dashboard stays in preview mode.

### Build and test

```bash
git clone https://github.com/<your-account>/CorePulse.git
cd CorePulse
go build -o corepulse ./cmd/corepulse
./corepulse version
go test ./...
go vet ./...
```

`go test ./...` uses in-memory fakes and small fixtures only. It never contacts Kubernetes. Builds from source print `dev` for `version`.

### Commands that work offline

```bash
./corepulse pod-crash --target amf
./corepulse discover --snapshot examples/discovery-snapshot.json
./corepulse dataset --telemetry <empty.jsonl> --runs-dir <empty-dir> --output <out.csv>
```

- `pod-crash` without `--apply` only prints a dry-run plan. It never deletes anything.
- `discover --snapshot` analyzes a saved Kubernetes `List` instead of calling `kubectl`. The example file is synthetic. Annotation candidates are reported with `"verified": false`; no metric is claimed.
- `dataset` with an empty telemetry file and empty run directory writes a CSV header and a zero summary. It reports an error if the telemetry file does not exist.

`signals`, `verify-service`, `baseline`, and `pod-crash --apply` need the live lab and exit unsuccessfully without it.

### Dashboard preview

```bash
./corepulse serve
```

Open `http://127.0.0.1:8787`. The server only binds to loopback addresses and refuses others such as `0.0.0.0`. Without a reachable lab, `/api/overview` returns `"mode": "preview"` with the message *Kubernetes lab is unavailable*, the page shows **Lab offline · preview mode**, reference topology is labeled as such, and the run control stays disabled. No telemetry or predictions are invented. The server also periodically tries to sample pod metrics; those attempts fail harmlessly while offline.

### Package map

| Path | Responsibility |
| --- | --- |
| `cmd/corepulse` | CLI entry point and flag parsing for every command |
| `internal/corepulse` | `kubectl` client, lab discovery, signal checks, UE-to-UPF probe, guarded pod-crash fault |
| `internal/collection` | Baseline runs, fault lifecycle, checkpoints, and service recovery observation |
| `internal/telemetry` | `metrics.k8s.io` parsing, resource quantity validation, JSONL sample store |
| `internal/dataset` | Curated CSV built only from verified run intervals |
| `internal/evaluation` | Offline baselines, training-only preprocessing, and held-out metrics |
| `internal/inference` | Standard-library-only evaluation of versioned dense/ReLU JSON artifacts |
| `ml/` | Offline CPU neural experiments, grouped validation, calibration and coefficient export |
| `internal/runrecord` | Atomic run record writes |
| `internal/dashboard` | Loopback HTTP API and the embedded web UI in `web/` |

### Areas you can change with fixtures alone

- **Resource quantity parsing** (`internal/telemetry`): CPU/memory unit handling and validation, tested with string inputs.
- **Dataset labeling and CSV export** (`internal/dataset`): run interval filtering, exclusion counts, and missing-value handling, tested with temporary JSONL and run record files.
- **Discovery analysis** (`internal/corepulse`): `AnalyzeSnapshot` on synthetic Kubernetes lists like `examples/discovery-snapshot.json`.
- **Dashboard API and UI** (`internal/dashboard`): handlers are tested with a fake client; the preview state can be checked in a browser.
- **Protocol log parsing** (`internal/logevents`): a pure parser tested against small anonymized log fixtures.
- **Windowing and evaluation** (`internal/dataset`, `internal/evaluation`): missingness, whole-run holdouts, source hashes, and deterministic metrics tested with synthetic inputs.

Never commit real kubeconfigs, credentials, subscriber identifiers, raw telemetry, or run records as fixtures. Use synthetic or anonymized data.

### Offline neural checks

The [neural workspace](ml/README.md) uses Python 3.12 on Linux x86-64, CPU-only
PyTorch and a hash-locked environment. Install uv 0.13.7 and keep Go available on
the Linux executable path for the parity tests:

```bash
uv sync --project ml --locked --python 3.12
uv run --project ml --locked ruff check ml
uv run --project ml --locked pytest ml
```

The tests construct invented data, keep preprocessing and model selection inside
the training partition, and actually compare Go inference with CPU PyTorch.
They do not launch collectors, a dashboard server or fault commands. When Go is
installed only on Windows, follow the cross-built test-binary instructions in
the [artifact contract](docs/neural-artifacts.md); missing Go must fail parity
rather than silently skipping it.

Changes to neural data loading, reports, training or inference should run these
checks alongside the Go/helper suites below. Preserve every reference method,
held-out prediction, fixed seed and synthetic warning. Generated coefficients,
reports and caches belong in ignored output directories. Include fixture-only
validation and any unperformed live checks in the pull request description.

After the [synthetic smoke workflow](ml/README.md#reproduce-the-synthetic-experiment),
Node 24 can test the real evaluation renderer with DOM fixtures and no network:
`node --test scripts/tests/dashboard-evaluation.test.cjs`.

## Community and reporting

Follow the [Code of Conduct](CODE_OF_CONDUCT.md) in project spaces. Use the issue templates for reproducible bugs and scoped feature proposals. Report vulnerabilities through the private process in [SECURITY.md](SECURITY.md), rather than including exploit details in a public issue.

## Pull request workflow

Branch from an up-to-date `main` and keep changes focused. Explain the problem, resulting behavior and validation, and link the relevant issue. Changes to exported data or run records should document compatibility and failure behavior. Include regression tests for behavior changes; documentation-only changes do not need new code tests.

`main` requires a pull request, the successful GitHub Actions `go` check against an up-to-date branch, and resolution of review conversations. Force pushes and deletion of `main` are disabled. Another person's approval is not mandatory for this single-maintainer project, but review feedback must still be addressed.

Contributions are made under the repository's [MIT License](LICENSE). Preserve existing notices and identify any third-party material and its license in your pull request.

## Development checks

Changes to the Bash/Python lab helpers also have offline fixture tests. Run them in Linux or WSL with Bash and Python 3:

```bash
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
```

The tests substitute a fake `kubectl`, simulate ambient context changes and failures, and never contact a cluster. CI runs them alongside the Go checks.

CorePulse is an early-stage Go project. Keep fault actions scoped to the isolated `open5gs-lab` Kubernetes context and the `open5gs` namespace. Changes to fault injection should include a recovery check and a test for the safety guard.

Before opening a pull request, run:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
```

Describe what was measured in a live lab and what was only tested with fixtures. Do not commit kubeconfigs, credentials, raw telemetry, run records, or trained models. See [README.md](README.md) for the current scope.

To verify the complete offline example after changing exported data or evaluation behavior, build the CLI and run `python3 scripts/check-offline-example.py --binary ./corepulse` (on Windows, use your built `.exe` path). Python is only needed for this checker; the documented Go CLI pipeline itself has no Python dependency.
