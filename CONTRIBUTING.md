# Contributing

CorePulse is an early-stage Go project. Keep fault actions scoped to the isolated `open5gs-lab` Kubernetes context and the `open5gs` namespace. Changes to fault injection should include a recovery check and a test for the safety guard.

Before opening a pull request, run:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
```

Describe what was measured in a live lab and what was only tested with fixtures. Do not commit kubeconfigs, credentials, raw telemetry, run records, or trained models. See [README.md](README.md) for the current scope.
