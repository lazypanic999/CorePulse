# 0001: Discover the running lab before choosing metrics

**Status:** Accepted for the first implementation slice

The project specification lists candidate telemetry sources, but does not prove that a particular Open5GS image exports Prometheus metrics. The existing lab repository pins charts and documents Kubernetes workloads, yet does not configure a Prometheus deployment or publish a verified metric inventory.

CorePulse therefore starts with a read-only Kubernetes inventory and explicitly separates scrape candidates from verified metric names. Model features and dashboard charts will be selected only after a live lab run supplies real endpoint and metric evidence.

The first fault experiment uses a single deployment-owned pod deletion because Kubernetes already provides a recovery mechanism that can be observed. Netem and SCTP scenarios require additional privilege and network-path checks before implementation.
