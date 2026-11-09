#!/usr/bin/env bash
set -Eeuo pipefail
[[ "$(kubectl config current-context)" == open5gs-lab ]] || { echo 'Requires open5gs-lab context' >&2; exit 1; }
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
patch="$(mktemp)"
trap 'rm -f "$patch"' EXIT
python3 - "$root/populate-subscriber.sh" "$patch" <<'PY'
import json, pathlib, subprocess, sys
d=json.loads(subprocess.check_output(['kubectl','--context','open5gs-lab','get','deployment/open5gs-populate','-n','open5gs','-o','json']))
c=next(c for c in d['spec']['template']['spec']['initContainers'] if c['name']=='init')
if c['image'] != 'docker.io/gradiant/open5gs-dbctl:0.10.3':
    raise SystemExit('Unsupported populate image; inspect before patching')
command=pathlib.Path(sys.argv[1]).read_text().replace('\r\n','\n')
pathlib.Path(sys.argv[2]).write_text(json.dumps({'spec':{'template':{'spec':{'initContainers':[{'name':'init','command':['/bin/bash','-c',command]}]}}}}))
PY
kubectl --context open5gs-lab patch deployment/open5gs-populate -n open5gs --type=strategic --patch-file "$patch"
kubectl --context open5gs-lab rollout status deployment/open5gs-populate -n open5gs --timeout=180s
