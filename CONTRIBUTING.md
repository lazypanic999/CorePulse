# Contributing

## Find a starting point

Browse [good first issues](https://github.com/lazypanic999/CorePulse/labels/good%20first%20issue) for small, scoped contributions or [help wanted](https://github.com/lazypanic999/CorePulse/labels/help%20wanted) for larger tasks. Each issue lists acceptance criteria, relevant files and whether a live lab is required. Comment with the issue you want to work on and a short approach so contributors can coordinate.

- [Resource quantity validation (#1)](https://github.com/lazypanic999/CorePulse/issues/1) and [offline contributor documentation (#2)](https://github.com/lazypanic999/CorePulse/issues/2) can be started without Kubernetes.
- Telecom and observability contributors can help design the [timestamped protocol event parser (#6)](https://github.com/lazypanic999/CorePulse/issues/6).

Fork the repository, create a focused branch and link the issue in your pull request. Keep documentation and issue discussions in English. For the local lab lifecycle and reset procedure, see [lab operations](docs/lab-operations.md).

Check that an issue is still open and read its linked pull requests before starting. Durable checkpoints, cancellable baselines, and missing-metric CSV handling have already been implemented.

## Community and reporting

Follow the [Code of Conduct](CODE_OF_CONDUCT.md) in project spaces. Use the issue templates for reproducible bugs and scoped feature proposals. Report vulnerabilities through the private process in [SECURITY.md](SECURITY.md), rather than including exploit details in a public issue.

## Pull request workflow

Branch from an up-to-date `main` and keep changes focused. Explain the problem, resulting behavior and validation, and link the relevant issue. Changes to exported data or run records should document compatibility and failure behavior. Include regression tests for behavior changes; documentation-only changes do not need new code tests.

`main` requires a pull request, the successful GitHub Actions `go` check against an up-to-date branch, and resolution of review conversations. Force pushes and deletion of `main` are disabled. Another person's approval is not mandatory for this single-maintainer project, but review feedback must still be addressed.

Contributions are made under the repository's [MIT License](LICENSE). Preserve existing notices and identify any third-party material and its license in your pull request.

## Development checks

CorePulse is an early-stage Go project. Keep fault actions scoped to the isolated `open5gs-lab` Kubernetes context and the `open5gs` namespace. Changes to fault injection should include a recovery check and a test for the safety guard.

Before opening a pull request, run:

```bash
gofmt -w cmd internal
go test ./...
go vet ./...
```

Describe what was measured in a live lab and what was only tested with fixtures. Do not commit kubeconfigs, credentials, raw telemetry, run records, or trained models. See [README.md](README.md) for the current scope.
