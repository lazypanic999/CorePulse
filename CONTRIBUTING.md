# Contributing

## Find a starting point

Browse [good first issues](https://github.com/lazypanic999/CorePulse/labels/good%20first%20issue) for small, scoped contributions or [help wanted](https://github.com/lazypanic999/CorePulse/labels/help%20wanted) for larger tasks. Each issue lists acceptance criteria, relevant files and whether a live lab is required. Comment with the issue you want to work on and a short approach so contributors can coordinate.

- [Resource quantity validation (#1)](https://github.com/lazypanic999/CorePulse/issues/1) and [offline contributor documentation (#2)](https://github.com/lazypanic999/CorePulse/issues/2) can be started without Kubernetes.
- Go contributors can help with [durable fault checkpoints (#3)](https://github.com/lazypanic999/CorePulse/issues/3), [cancellable baselines (#4)](https://github.com/lazypanic999/CorePulse/issues/4), or [missing metric representation (#5)](https://github.com/lazypanic999/CorePulse/issues/5).
- Telecom and observability contributors can help design the [timestamped protocol event parser (#6)](https://github.com/lazypanic999/CorePulse/issues/6).

Fork the repository, create a focused branch and link the issue in your pull request. Keep documentation and issue discussions in English. For the local lab lifecycle and reset procedure, see [lab operations](docs/lab-operations.md).

CorePulse is an early-stage Go project. Keep fault actions scoped to the isolated `open5gs-lab` Kubernetes context and the `open5gs` namespace. Changes to fault injection should include a recovery check and a test for the safety guard.

Before opening a pull request, run:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
```

Describe what was measured in a live lab and what was only tested with fixtures. Do not commit kubeconfigs, credentials, raw telemetry, run records, or trained models. See [README.md](README.md) for the current scope.
