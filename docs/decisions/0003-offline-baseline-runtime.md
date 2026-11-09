# 0003: Keep the first offline reference baselines in Go

**Status:** Accepted for reference evaluation; model-serving runtime remains open

**Date:** 9 November 2026

The first methods are small numerical references: training-only preprocessing, a nearest-centroid classifier, a normal-only deviation rule, and majority/probe comparisons. They run inside `corepulse evaluate` using the Go standard library. This keeps the offline workflow in the existing executable and makes fixtures, source-hash validation, and whole-run split checks part of the same implementation.

This decision does not claim that Go improves detection accuracy or is faster than a Python implementation. It does not select a runtime for a future isolation forest, a larger model, or continuous inference. Those choices should follow measured model requirements and an appropriate comparison.

## Implementation-cost check

```bash
go test ./internal/evaluation -run '^$' -bench BenchmarkResourceReferenceBatch -benchmem -benchtime=1s -count=1
```

One local run on Windows/amd64, Go 1.27.3, Intel Core i5-12500H reported **946,630 ns/op**, **395,418 B/op**, and **1,920 allocs/op**. Each operation fits preprocessing and scenario centroids on 1,000 synthetic windows with 11 resource inputs, then transforms/classifies 100 rows. Fixture construction is outside the timed region. This is about 0.95 ms for that particular batch on that machine; it is not a cross-runtime comparison or a latency guarantee. It excludes file I/O, Kubernetes calls, report serialization and network serving. The benchmark emits no accuracy result, and its prediction rows are not a held-out experiment set.

For the current small offline reference this avoids adding a model service, package manager or model-runtime dependency. Numerical parameters and every held-out prediction are still exported, so alternative implementations can later be compared on identical splits without changing the experiment controller.
