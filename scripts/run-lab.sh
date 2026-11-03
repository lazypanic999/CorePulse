#!/usr/bin/env bash
set -Eeuo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
if ! minikube -p open5gs-lab status >/dev/null 2>&1; then
  minikube -p open5gs-lab start --driver=docker
fi
bash scripts/repair-populate.sh
echo 'Lab running. Keep this process open while using the lab; Ctrl+C releases the WSL session.'
# systemd services alone do not keep a WSL distribution alive.
exec sleep infinity
