# Neural demo media

The README's neural screenshot was captured on 12 November 2026 from the actual
dashboard assets and the saved `synthetic-smoke` schema-3 report. The preview
server serves local files only; live observation endpoints are unavailable and
mutation requests are refused. No Kubernetes client or collector is involved.

The capture retains four references, both fixed MLP seeds, both normal-only
autoencoder seeds, wrong predictions, abstentions and the synthetic-data warning.
It is not a real-network accuracy result. Scenario classification and binary
normal/fault-interval detection are different tasks.

The companion social demo uses these real UI captures with edited camera moves,
chapter cards and a cursor cue for opening the held-out prediction table. The
vertical version is a 36-second, 1080×1350, 60 fps H.264 MP4 with English captions
burned in and no audio. The frame rate describes the video, not model throughput
or the original capture frequency. A synthetic-input label stays visible.

Generated video files, contact sheets and temporary browser profiles stay outside
tracked source assets. The README keeps a small PNG and a native SVG workflow
diagram; it does not embed trained coefficients or raw input files.

## What the demo shows

1. Open the saved offline evaluation and retain every reference and seed.
2. Compare MLP scenario classification with normal-only reconstruction error,
   without ranking their different tasks as one leaderboard.
3. Open held-out predictions and inspect incorrect/abstained results. The report
   retains all 48 method/window rows in this invented example.
4. Explain hash-bound inputs, grouped validation, training-only preprocessing,
   versioned JSON coefficients and standard-library-only Go inference.

See the [synthetic smoke instructions](../ml/README.md#reproduce-the-synthetic-experiment),
[artifact contract](neural-artifacts.md) and
[session report](progress/2026-11-11-neural-session.md) for reproducible checks.
Real-network neural performance and live model integration remain unvalidated.
