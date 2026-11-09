#!/usr/bin/env bash
set -Eeuo pipefail

context="$(kubectl config current-context)"
if [[ "${context}" != 'open5gs-lab' ]]; then
  echo "Expected Kubernetes context open5gs-lab, got ${context}" >&2
  exit 1
fi

namespace='open5gs'
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

kubectl --context open5gs-lab get deployment/ueransim-gnb deployment/ueransim-ue -n "${namespace}" >/dev/null

mapfile -t old_ue_pods < <(kubectl --context open5gs-lab get pods -n "${namespace}" -l app=ueransim,component=ue -o name)

kubectl --context open5gs-lab patch deployment/ueransim-gnb -n "${namespace}" --type=strategic \
  --patch-file "${script_dir}/gnb-dynamic-ip-patch.json"
kubectl --context open5gs-lab rollout status deployment/ueransim-gnb -n "${namespace}" --timeout=3m
kubectl --context open5gs-lab rollout restart deployment/ueransim-ue -n "${namespace}"
kubectl --context open5gs-lab rollout status deployment/ueransim-ue -n "${namespace}" --timeout=3m

for pod in "${old_ue_pods[@]}"; do
  kubectl --context open5gs-lab wait -n "${namespace}" --for=delete "${pod}" --timeout=3m
done

echo 'gNB N3 address now follows its pod IP; UE has restarted against the new gNB.'
