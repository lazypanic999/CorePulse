# Offline neural session — 2026-11-11

The requested implementation queue is complete locally, using invented fixtures
only. No result in this session measures real-network accuracy or service health.
Work began around 16:15 UTC (19:15 Europe/Istanbul), from
`0d628fa80b5670251d9b98154c5a7b0328936a11`. The final stacked branch is
`nn/n6-2-go-inference`. This session did not change main or publish remote changes.

## Task status and branches

“Done” means implemented and validated locally. The synthetic GitHub Actions job
is defined but has not run on GitHub. No branch was published and no PR was opened;
the publishing constraint is explained below. Each branch starts from its predecessor.

| Task | Status | Branch | Implementation commit | PR |
| --- | --- | --- | --- | --- |
| N2.1 Python 3.12 CPU workspace, locked dependencies, Ruff, pytest, synthetic CI | Done locally | `nn/n2-1-ml-workspace` | `73207c7` | None |
| N2.2 Hash/split verification, feature allowlist, training-only preprocessing | Done locally | `nn/n2-2-data-loader` | `76334b9` | None |
| N2.3 Inner validation grouped by run/session | Done locally | `nn/n2-3-inner-validation` | `71cb64d` | None |
| N2.4 Strict experiment YAML and complete held-out reports | Done locally | `nn/n2-4-experiment-reports` | `cb693be` | None |
| N2.5 Extensible schema-3 snapshot and offline dashboard display | Done locally | `nn/n2-5-dashboard-schema` | `e12ac73` | None |
| N2.6 Training/inference ADR 0004 | Done locally | `nn/n2-6-neural-adr` | `7d48370` | None |
| N1.5 Masked sequence export and compatible provenance/splits | Done locally | `nn/n1-5-sequence-export` | `746a17d` | None |
| N3.1 Group-validated CPU MLP | Done locally | `nn/n3-1-mlp` | `9f03ebf` | None |
| N3.2 Every fixed MLP seed in the synthetic smoke workflow | Done locally | `nn/n3-2-synthetic-smoke` | `a247850` | None |
| N5.1 Normal-only autoencoder with validation-percentile threshold | Done locally | `nn/n5-1-autoencoder` | `7cc30d3` | None |
| N6.1 Source-bound versioned JSON coefficient export | Done locally | `nn/n6-1-json-export` | `5bfc0a1` | None |
| N6.2 Pure-Go inference and actual CPU PyTorch parity | Done locally | `nn/n6-2-go-inference` | `467ef6c` | None |

Further focused commits on the last branch add numeric/resource limits, output
preflight, trained-export integration, exact nanosecond boundaries, parser/fuzz
coverage, real dashboard-renderer DOM tests, source/group checks at export,
installed-wheel output protection, failure-isolation tests and contributor docs.
These stay within the requested tasks; no new roadmap item was started.

## Decisions made without interrupting the session

- `AGENTS.md` was absent in the repository and its ancestors. The requested
  `docs/NEURAL-ROADMAP.md` was absent locally and in fetched `origin/main`.
  The explicit task queue supplied in the request was therefore authoritative.
  Existing roadmap checkboxes were left unchanged.
- The first Python environment supports Linux x86-64 with Python 3.12 and the
  explicit official CPU PyTorch index. uv 0.13.7 locks 19 packages, including
  PyTorch 2.15.0+cpu, NumPy 2.5.4, PyYAML 6.0.3, pytest 9.2.0 and Ruff 0.17.4.
  The large environment, caches and downloaded compilers were put outside the
  repository on D: to preserve C: space. No global toolchain pin was changed.
- Neural features default to the eleven resource features used by the Go
  references. Run/session identities, labels, protocol/probe evidence and post-run
  event counts cannot be selected as inputs. All-missing training features stay
  dropped even when observations appear in the held-out partition.
- Inner folds use outer-training data only, keep whole runs (schema 1) or whole
  declared sessions (schema 2), and purge near-validation training groups using
  the existing capture-time margin. Impossible folds refuse rather than silently
  reducing their count or retuning seeds. YAML fold counts are bounded to 2..32.
- MLP epochs are selected by mean inner-validation cross entropy, with an earlier
  epoch winning ties; final fitting uses all outer-training rows. Both declared
  seeds, 7 and 23, remain in reports. No outer-test score selects a seed or epoch.
- Autoencoder preprocessing and weights use only normal inner-fit rows. Separate
  normal validation groups calibrate the declared 95th percentile with the
  empirical `higher` rule. Equality remains normal. Calibration data is not
  refitted into parameters; there must be at least two normal windows per side.
- Report schema 3 extends named methods while retaining all four references.
  Embedded split manifests keep their existing schema 1/2 contracts. Reports
  retain every held-out prediction, mistakes/abstentions, recomputed confusion
  matrices and per-class/macro F1, coverage, and seed-family population spread.
  A spread across two seeds is not a confidence interval.
- Sequence steps use within-bin observed means and per-signal observation masks,
  with no interpolation or forward fill. Entirely empty steps/runs remain. The
  grid is bounded to 100,000 steps, with 1-second to 1-hour cadence. Sequence
  manifests declare `sequences_v1`; the historical `windows_sha256` field hashes
  the exact sequence CSV bytes. Existing split allocation and provenance gates
  remain in force. Current Python window trainers reject sequence inputs.
- Version-1 JSON artifacts support dense/ReLU operations only, with explicit
  float32 coefficients, preprocessing, source/split hashes, fit identities and
  normal calibration provenance. Export rechecks labels, whole-group ownership
  and exact time margins. Destination/input aliases and collisions refuse before
  training. Installed wheels preserve source-tree output protection.
- Go inference is a separate standard-library-only offline package, with no cgo,
  collector, controller or live model endpoint. Unknown operators/fields,
  duplicate JSON keys, null numerics, invalid dimensions, non-finite values and
  bounded-resource violations refuse. Missing values and observed zero differ.
- Each output file is replaced only after validation. Several model files plus
  the report are not a cross-file transaction: following a write failure, verify
  report hashes or rerun the fixed experiment. A training failure before writes
  preserves the previous report and model files.

## Why there are no pushed branches or draft PRs

The existing, unchanged [CI workflow](../../.github/workflows/ci.yml) runs this
container client check on pushes and pull requests:

```text
docker run --rm --network none --read-only --entrypoint kubectl corepulse-ci version --client=true -o json
```

That check is client-only and network-isolated; it does not contact a cluster.
Nevertheless, the session instruction says never to run `kubectl`. I treated
triggering it through publication as outside the authorized scope, so kept all work
in local commits. I did not disable, skip or weaken existing CI to publish.
A later session can publish the stack and open draft PRs once this client-only
check is explicitly allowed. This was an instruction boundary, not an automatic
approval-review rejection or a demonstrated GitHub authentication failure.

## Validation evidence

Before every commit, the requested format, Go test/vet, Python helper, Ruff and
ML test suites were run. The final ML suite contains 174 passing pytest cases;
the Linux standard-library helper suite contains six passing tests. Parity is
required rather than skipped if Go is missing. Validation used an empty
kubeconfig and task-local CLI refusal stubs as an additional safeguard.

- Native Windows Go 1.27.3: full Go suite and vet passed. The new inference
  package reached 96.1% statement coverage. A bounded, two-worker, 30-second JSON
  fuzz run executed 872,309 inputs without a crash; no generated corpus was added.
- Native Linux Go 1.27.3: a clean committed-source archive at `46692ee` passed
  strict formatting, all Go tests uncached, vet, offline lock consistency, six
  helper tests, Ruff, all 174 ML tests using native Go compilation,
  tensor smoke, complete synthetic experiment, Go snapshot validation and wheel
  build. Five DOM-renderer tests also passed against this archive's report.
  The compiler archive
  was verified against its checksum from the [official Go releases](https://go.dev/dl/#go1.27.3).
- Native Linux Go 1.22.12, matching the existing `go.mod` source minimum: all Go
  tests and vet passed on clean source at `1e730b7`, followed by the current parity
  and trained-export integration tests using that actual native compiler. No
  source minimum or repository toolchain pin was changed. Production code is
  unchanged between `1e730b7` and `46692ee`; intervening changes add tests/docs.
- Parity includes 80 hand-authored MLP/autoencoder cases and 96 temporary,
  synthetic-trained held-out comparisons across run/session splits and a feature
  missing throughout training. Outputs and scores agree within absolute 1e-5;
  labels, byte hashes and source fit/calibration identities are checked too.
  Go runs with cgo disabled. No trained coefficients are committed.
- The invented smoke report has eight methods: four references, two MLP seeds
  and two normal-only autoencoder seeds. Python reports pass the actual Go
  schema-3 snapshot reader. Schema-1/2 compatibility tests remain passing.
- The documented 5-second sequence commands produced 108 steps from nine
  invented runs, preserving 72 entirely empty steps. The session split's
  `sequences_v1` declaration and exact CSV byte hash were checked.
- Five Node 24 DOM-fixture tests execute the real dashboard renderer and actual
  HTML identifiers: all methods and 48 held-out rows, synthetic warning, seed
  spread, mistakes/abstentions, legacy transitions and unavailable/error clearing.
  They do not launch a browser/server or perform network requests.
- A wheel built from committed sources passed module/metadata and CPU checks.
  Repeating the complete experiment through that wheel produced byte-identical
  report and four artifact hashes compared with the source run; inputs were
  unchanged. Generated reports and coefficients remain in ignored output paths.

### Observed filesystem limitation

One native Linux full-suite run with temporary files on `/mnt/d` (WSL DrvFS)
failed the pre-existing `TestReplacementAlwaysExposesCompleteJSON`: a concurrent
reader briefly saw `ENOENT` for `run.json` during replacement. No implementation
or assertion was changed. With test temporary storage on Linux ext4 under `/tmp`,
the same unchanged test and the full suite passed. Native Windows runs also
passed. This is an observed environment-specific concurrency failure, not a
resolved atomic-storage guarantee for DrvFS. A maintainer should investigate
mounted-Windows storage before relying on it for concurrent run-record access.

## Reproduce the invented smoke run

Run from the repository root in Linux x86-64 with Python 3.12, uv 0.13.7, Go and
Node 24 on `PATH`. Outputs below are ignored. These commands build and evaluate
local invented files; they do not start services or contact Kubernetes.

```bash
export CUDA_VISIBLE_DEVICES='' OMP_NUM_THREADS=1 MKL_NUM_THREADS=1
export KUBECONFIG=/dev/null
uv sync --project ml --locked --python 3.12
mkdir -p reports/neural-synthetic
go build -o reports/neural-synthetic/corepulse ./cmd/corepulse
python3 scripts/prepare-neural-synthetic.py --binary reports/neural-synthetic/corepulse
uv run --project ml --locked python -m corepulse_ml.experiment \
  ml/experiments/synthetic-smoke.yaml \
  --export-dir reports/neural-synthetic/models
COREPULSE_SYNTHETIC_REPORT="$PWD/reports/neural-synthetic/evaluation.json" \
  go test ./internal/evaluation -run '^TestPythonSyntheticSnapshot$' -count=1
node --test scripts/tests/dashboard-evaluation.test.cjs
uv run --project ml --locked pytest \
  ml/tests/test_go_parity.py ml/tests/test_pipeline_export.py -q
```

To exercise the sequence exporter on the same invented example:

```bash
reports/neural-synthetic/corepulse sequences \
  --telemetry examples/offline/telemetry.jsonl \
  --runs-dir examples/offline/run-records \
  --output reports/neural-synthetic/sequences.csv --step 5s
reports/neural-synthetic/corepulse split \
  --input reports/neural-synthetic/sequences.csv \
  --output reports/neural-synthetic/sequence-split.json \
  --group-by session --seed 42 --test-fraction 0.25 --embargo 2m
```

The required full pre-commit checks, with the locked ML environment active:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
ruff check ml
pytest ml
```

In WSL, use Linux ext4 for Go test temporary files, for example
`mkdir -p /tmp/corepulse-tests && export GOTMPDIR=/tmp/corepulse-tests TMPDIR=/tmp/corepulse-tests`.
Keep the Linux Go compiler on `PATH` for actual parity compilation. If Go exists
only on Windows, the [artifact contract](../neural-artifacts.md) documents the
fresh Linux test-binary alternative; skipping parity is not an alternative.

## Checks and decisions still outside this session

- Publishing the branch stack and running hosted CI need the client-only CLI
  restriction resolved. No merges, release or package publication was performed.
- Real-lab neural quality, independent-session support and live service-health
  behavior remain unmeasured. Any future lab check needs separate authorization
  and adequate capture/protocol provenance; no synthetic score should be used as
  a real-lab claim. No cluster was contacted here.
- Dashboard coverage is functional DOM and HTTP-fixture coverage, not a visual
  browser audit. There is no live neural dashboard/controller integration.
- Additional Python platforms and Go architectures require their own install and
  parity checks. Values extremely close to a threshold or class tie may change a
  discrete decision under floating-point rounding; 1e-5 parity is a tested numeric
  tolerance, not a universal bitwise or boundary-classification guarantee.
- The observed DrvFS replacement behavior needs a storage/portability decision.

`internal/corepulse/faults.go`, `internal/collection/*`, existing safeguards and
tests, and roadmap checkboxes were not changed. No raw telemetry, collected run
records, credentials or trained weights were added to Git. The pre-existing
untracked `.claude/` directory was preserved untouched.
