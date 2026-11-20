# Neural experiments in the dashboard

The optional neural workspace runs the existing CPU MLP and normal-only
autoencoder pipeline from the real dashboard. It trains on the **fixed invented
tutorial cohort only**. Live pod/resource observations, live run records and the
saved live-reference report remain separate. It does not diagnose a live run or
make live model predictions.

In Linux x86-64, install the locked Python 3.12 workspace and build the Go CLI:

```bash
uv sync --project ml --locked --python 3.12
go build -o corepulse ./cmd/corepulse
./corepulse serve --ml-python "$PWD/ml/.venv/bin/python"
```

`--ml-python` must be an absolute path to the locked environment's Python
executable. Start the dashboard from the repository root. Existing lab guards and
loopback restrictions remain in force. Without this flag the neural panel explains
that the worker is disabled; no Python process is started.

## User flow

1. Open **Neural experiments**, acknowledge the fixed synthetic dataset, and
   select **Train neural models**.
2. The worker prepares verified windows, a whole-session split and all four Go
   references, then trains both neural methods for seeds 7 and 23. The progress
   indicator shows actual stages rather than an invented percentage.
3. Results retain every seed. Scenario classification and anomaly detection are
   different tasks; their F1 values are not one leaderboard.
4. Choose a model and held-out sample, then select **Predict with Go**. The input
   contains retained resource features only. Its known interval label is shown
   afterward for comparison, including incorrect predictions.
5. Expand the reference/prediction tables to inspect all eight methods and every
   held-out result. Maximum softmax scores are not calibrated probabilities.

## Execution and output boundaries

Only one neural job can run at a time. It acquires the existing shared collection
ownership marker, so cooperating fault/baseline collectors cannot overlap CPU
training. Release follows the existing ownership rules; no lock is stolen or
expired. The worker has a ten-minute deadline, CPU-only environment settings and
one compute thread. API requests cannot choose executables, YAML, datasets,
artifact paths or arbitrary feature payloads.

Each run uses a new ignored `reports/neural-ui/job-*` directory, retaining its
configuration, progress, private log, source windows/split, report and four JSON
artifacts. Failed jobs publish no model or score. Earlier output directories are
preserved. Generated coefficients and source files must not be committed.

Before publishing a result, Go validates the synthetic schema-3 report, exact
CSV/split/artifact hashes, all fixed artifact identities and held-out membership.
It evaluates every exported model's held-out input in `internal/inference`.
Frozen CPU reference inputs must exactly match the source preprocessing; model
output vectors must agree within absolute `1e-5`, and classifications must match.
MLP score comparisons retain absolute `1e-5`. Derived autoencoder MSE comparisons
also account for propagated output error and a dimension/magnitude-dependent
float32 sum/division rounding budget: at an MSE of 898, a single float32 ULP is
already about `0.000061`. Existing fixed-fixture output and score assertions at
absolute `1e-5` are unchanged. Inference requests bind the completed job ID,
model and sample; stale jobs
and unknown selections refuse. Models and inputs are retained as validated
in-memory snapshots rather than rereading mutable artifact paths per prediction.

The existing synthetic CI workflow supplies freshly trained exports to
`TestPythonExportIntegrity`, including corruption and stale-job checks. HTTP tests
cover disabled workers, loopback/origin checks, explicit confirmation, strict
request fields and missing/null sample rejection without a lab client call.

Real-data training and live inference remain separate future work. Do not label
real measurements as synthetic to use this development workflow.
