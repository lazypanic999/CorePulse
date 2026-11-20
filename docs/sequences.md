# Masked offline sequences

`corepulse sequences --step 5s` exports a regular capture-time grid within each
eligible run. It reads saved files only. The exporter delegates attribution,
resource validation, clean/recovered status and protocol-start gates to the
unchanged window pipeline. It never changes raw telemetry or run records.

```bash
go build -o reports/neural-synthetic/corepulse ./cmd/corepulse
reports/neural-synthetic/corepulse sequences \
  --telemetry examples/offline/telemetry.jsonl \
  --runs-dir examples/offline/run-records \
  --output reports/neural-synthetic/sequences.csv --step 5s
reports/neural-synthetic/corepulse split \
  --input reports/neural-synthetic/sequences.csv \
  --output reports/neural-synthetic/sequence-split.json \
  --group-by session --seed 42 --test-fraction 0.25 --embargo 2m
```

**These example inputs are invented, not lab evidence.** The commands above do
not invoke Kubernetes, collect samples or inject faults. The binary directory
must already exist (`mkdir -p reports/neural-synthetic` in Linux).

## CSV contract: sequences_v1

Metadata columns are `run_id,label,target,step_index,step_start,step_end,`
`sample_count,partial,protocol_start_verified,session_id,step_seconds`.
Every allowlisted window signal then has adjacent `<signal>_value` and
`<signal>_mask` columns, in the existing signal order. A value is the mean of
observed captures **within that step**, not an instantaneous resampled value.
Mask `1` means at least one observed value; `0` requires a blank value. A measured
zero stays `0,1`. Missing observations stay `,0`; there is no forward fill or
interpolation. A mask does not measure completeness or freshness within a step.

Steps are half-open `[start,end)`, aligned to the run's own start, and numbered
from zero. Captures at a boundary enter the following step/run. Captures at the
final run end are excluded. Empty steps, including completely unobserved runs,
remain with sample count zero and every mask zero. The final short step is marked
partial. This deliberately differs from `windows`, which omits empty windows.
The step must be between 1 second and 1 hour; an export is capped at 100,000 steps
to bound in-memory CSV construction. Invalid input preserves previous output.

The reader rejects broken value/mask pairs, non-finite or negative observations,
inconsistent labels/session/protocol/cadence within a run, missing/duplicate grid
indices and invalid partial boundaries. Metadata is never a numeric feature.
Probe and post-run event signal columns remain exported for provenance but must
not enter neural features; the current Python window trainer explicitly rejects
sequence files/manifests. Sequence-model training is not implemented by this task.

## Split and source identity

The existing `split` command detects the exact sequence header and uses the same
whole-run or declared-session allocation, class-support checks, strict protocol
default and capture-time embargo. A sequence manifest declares
`input_kind: sequences_v1` and its exact value/mask feature order. Its split schema
is still 1 (runs) or 2 (sessions). For backward compatibility, `windows_sha256`
is the SHA-256 of the **exact sequence CSV bytes**, despite the historical name.
Window split serialization is unchanged.

Go consumers use `ReadSequences` and `ValidateSequenceSplit` to verify that hash,
feature order, complete disjoint membership, class support, session ownership and
temporal separation before consuming partitions. Rebuild the split after any CSV
change; do not repair hashes manually. `evaluate` and the Python window loader
reject sequence inputs rather than interpreting masks as window statistics.

Declared sessions are not authenticated proof of independence. The grid and
embargo use capture times and do not reconstruct per-pod CPU measurement intervals.
A 5-second step does not create a 5-second measurement cadence. Missing runs and
partial bins remain visible; no detection-quality claim follows from this export.
