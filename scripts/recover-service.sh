#!/usr/bin/env bash
set -Eeuo pipefail
[[ "$(kubectl config current-context)" == open5gs-lab ]] || { echo 'Requires open5gs-lab context' >&2; exit 1; }
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
names=(ueransim-ue ueransim-gnb open5gs-smf open5gs-upf)
for name in "${names[@]}"; do
  replicas="$(kubectl --context open5gs-lab get deployment/"$name" -n open5gs -o jsonpath='{.spec.replicas}')"
  [[ "$replicas" == 1 ]] || { echo "$name must have exactly one replica" >&2; exit 1; }
done
python3 scripts/configure-pfcp.py
# Stop traffic producers before rebuilding PFCP peers. Do not overlap old/new UE pods.
restore() { for name in "${names[@]}"; do kubectl --context open5gs-lab scale deployment/"$name" -n open5gs --replicas=1 >/dev/null || true; done; }
trap restore ERR
trap 'restore; exit 130' INT
trap 'restore; exit 143' TERM
for name in "${names[@]}"; do
  selector="$(kubectl --context open5gs-lab get deployment/"$name" -n open5gs -o json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(",".join(k+"="+v for k,v in sorted(d["spec"]["selector"]["matchLabels"].items())))')"
  [[ -n "$selector" ]] || { restore; exit 1; }
  kubectl --context open5gs-lab scale deployment/"$name" -n open5gs --replicas=0
  kubectl --context open5gs-lab wait pod -n open5gs -l "$selector" --for=delete --timeout=180s
done
for name in open5gs-upf open5gs-smf ueransim-gnb ueransim-ue; do
  kubectl --context open5gs-lab scale deployment/"$name" -n open5gs --replicas=1
  kubectl --context open5gs-lab rollout status deployment/"$name" -n open5gs --timeout=180s
done
trap - ERR INT TERM
streak=0
for attempt in {1..12}; do
  pod="$(kubectl --context open5gs-lab get pods -n open5gs -l app=ueransim,component=ue -o json | python3 -c 'import json,sys; p=[p for p in json.load(sys.stdin)["items"] if not p["metadata"].get("deletionTimestamp") and any(c["type"]=="Ready" and c["status"]=="True" for c in p["status"].get("conditions",[]))]; assert len(p)==1; print(p[0]["metadata"]["name"])')"
  logs="$(kubectl --context open5gs-lab logs -n open5gs "$pod" --tail=300)"
  if grep -Eqi 'registration is successful|registration accept' <<<"$logs" &&
     grep -Eqi 'pdu session.*successful|pdu session establishment accept' <<<"$logs" &&
     kubectl --context open5gs-lab exec -n open5gs "$pod" -- ping -I uesimtun0 10.45.0.1 -c 2 -W 2; then
    streak=$((streak+1))
    if [[ "$streak" == 2 ]]; then echo 'Service recovery verified by consecutive packet checks.'; exit 0; fi
  else streak=0; fi
  sleep 5
done
echo 'Workloads are ready but service recovery is unverified.' >&2
exit 2
