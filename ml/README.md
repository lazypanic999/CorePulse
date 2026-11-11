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
