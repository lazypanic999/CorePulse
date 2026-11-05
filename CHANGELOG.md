# Changelog

## Unreleased

- Dashboard redesign: the service path diagram shows pod readiness and the UE-to-UPF packet check separately, the run control lists its safety checks, runs appear in a table, and kubectl errors are summarized with the raw output on request. No external assets; the glass theme and marketing hero are removed.
- Shared CLI/dashboard fault checkpoints before mutation and throughout recovery, with complete JSON replacement and excluded pending records.
- Cancellable baseline waits and kubectl commands; interrupted intervals retain their evidence and stay out of curated training data.
- Missing AMF/UPF measurements export as empty CSV fields while observed zero remains numeric (community contribution #8).
- Liquid glass desktop and narrow-screen dashboard with updated live captures.
- Read-only UE-to-UPF gateway probe in the CLI, dashboard, and new experiment records.

## v0.1.0 — 2026-10-31 (preview)

- Go CLI for lab discovery, a guarded AMF/UPF pod crash, and labeled CSV generation.
- Local dashboard with observed topology, live Kubernetes pod CPU/memory charts, and run history.
- Reproducible UERANSIM gNB N3 address correction for the tested Minikube lab.
- First end-to-end lab verification and a recorded AMF recovery experiment.

This preview does not include Open5GS protocol metrics, a trained detection model, or predictions. The first captured dataset includes lab startup and diagnostic activity and is not a clean model training set.
