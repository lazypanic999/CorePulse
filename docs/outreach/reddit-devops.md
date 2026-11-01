Disclosure: I maintain CorePulse, and an AI coding assistant helped draft this comment.

CorePulse is an early-stage open-source Go project for controlled fault experiments in a local Open5GS + UERANSIM Kubernetes lab.

The first working slice can plan or execute a guarded AMF/UPF pod deletion in an exact `open5gs-lab` context, record the fault interval, and wait for deployment recovery. A loopback dashboard shows live pod CPU/memory, run history, and a separate UE-to-UPF gateway probe. I separated the probe because a ready replacement pod does not necessarily mean the service path is healthy.

There is no trained detector yet. The first dataset includes setup/diagnostic noise, so the next step is repeated clean runs and evaluation split by run. The repo has screenshots, setup notes, tests, and preview binaries: https://github.com/lazypanic999/CorePulse

For people who run fault experiments: what additional signal would you collect first to distinguish AMF from UPF failures? I am considering protocol-level metrics and a stronger end-to-end reachability check.

Published at: https://www.reddit.com/r/devops/comments/1xfq7qc/comment/19rzowqvsxmx0/
