# 0002: Use Go for the control plane

**Status:** Accepted

CorePulse's CLI and future API use Go. The first Python discovery and pod experiment were migrated to a dependency-free Go module. The command names, JSON schemas, lab context checks, target allowlist, and recovery reporting remain the same.

Go provides a single executable for the lab controller and a straightforward path to a long-running API. The ML model runtime is still an open decision; model experiments should be evaluated on their results before choosing how inference is hosted. The implementation does not claim that Go itself improves fault detection accuracy.
