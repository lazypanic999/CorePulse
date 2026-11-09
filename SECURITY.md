# Security Policy

## Supported versions

CorePulse is an early-development local lab tool. Security fixes are developed on `main` and included in subsequent preview releases. Use the latest code or preview release when verifying a fix; older previews do not have a separate backport support line. Releases are previews for an isolated lab, not production-ready deployments.

## Report a vulnerability privately

Use [GitHub private vulnerability reporting](https://github.com/lazypanic999/CorePulse/security/advisories/new) to send a report to the repository maintainer. Private vulnerability reporting is enabled for this repository. Please do not disclose exploit details in a public issue or pull request before the maintainer has had an opportunity to investigate and coordinate a fix.

Include, where available:

- The affected commit or version, operating system, and relevant configuration.
- A description of the impact and the permissions an attacker would need.
- Minimal reproduction steps or a proof of concept using an isolated environment.
- Expected and observed behavior, with sanitized logs or screenshots.
- Whether the issue has already been disclosed elsewhere.

Do not include access tokens, kubeconfigs, credentials, subscriber data, or other private lab information. Avoid testing against someone else's infrastructure. The maintainer will use the private report to discuss reproduction, remediation, and disclosure; public advisories can credit the reporter with their consent.

If the private reporting form is temporarily unavailable, open a minimal issue requesting a private security contact, without vulnerability details or sensitive attachments.

## Relevant security boundaries

Reports are particularly useful for bypasses of Kubernetes context, namespace, target allowlist, ownership, or confirmation checks; unauthorized dashboard actions; command injection; unintended file access; credential exposure; and exploitable dependencies or release artifacts.

CorePulse intentionally deletes an allowlisted pod when an explicitly confirmed lab experiment is applied. Expected disruption from that authorized experiment is different from bypassing its safety checks. Ordinary defects without a security impact can use the [bug report template](https://github.com/lazypanic999/CorePulse/issues/new?template=bug_report.yml).

## Deployment expectations

Run experiments only in the isolated `open5gs-lab` context and `open5gs` namespace. Keep the dashboard bound to loopback; it has no multi-user authentication and should not be published to the Internet. Use a dedicated lab kubeconfig with only the permissions needed for the lab. Keep credentials and generated run/telemetry files out of version control. See [lab operations](docs/lab-operations.md) for lifecycle and recovery procedures.
