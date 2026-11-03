"""Use direct UPF pod DNS for the pinned lab's internal PFCP peer."""
import json
import pathlib
import subprocess

def get(*args):
    return json.loads(subprocess.check_output(["kubectl", *args, "-o", "json"]))

def apply(value):
    subprocess.run(["kubectl", "apply", "-f", "-"], input=json.dumps(value).encode(), check=True)

context = subprocess.check_output(["kubectl", "config", "current-context"], text=True).strip()
if context != "open5gs-lab":
    raise SystemExit("Requires open5gs-lab context")
deployment = get("get", "deployment/open5gs-smf", "-n", "open5gs")
if deployment["spec"]["template"]["spec"]["containers"][0]["image"] != "docker.io/gradiant/open5gs:2.7.5":
    raise SystemExit("Unsupported SMF image; inspect before patching")
source = get("get", "service/open5gs-upf-pfcp", "-n", "open5gs")
cm = get("get", "configmap/open5gs-smf", "-n", "open5gs")
config = cm["data"]["smf.yaml"]
old, new = "- address: open5gs-upf-pfcp", "- address: corepulse-upf-pfcp"
if config.count(old) != 1 and config.count(new) != 1:
    raise SystemExit("Unexpected SMF PFCP configuration")
name = "corepulse-upf-pfcp"
existing = get("get", "services", "-n", "open5gs")["items"]
for service in existing:
    if service["metadata"]["name"] == name and service["metadata"].get("labels", {}).get("app.kubernetes.io/managed-by") != "corepulse":
        raise SystemExit("Refusing to replace an unmanaged service")
apply({"apiVersion": "v1", "kind": "Service", "metadata": {"name": name, "namespace": "open5gs", "labels": {"app.kubernetes.io/managed-by": "corepulse"}}, "spec": {"clusterIP": "None", "selector": source["spec"]["selector"], "ports": [{"name": "pfcp", "port": 8805, "targetPort": 8805, "protocol": "UDP"}]}})
if old in config:
    backup = pathlib.Path("reports/smf-before-direct-pfcp.json")
    backup.parent.mkdir(exist_ok=True)
    if not backup.exists():
        backup.write_text(json.dumps(cm, indent=2))
    patch = {"data": {"smf.yaml": config.replace(old, new)}}
    subprocess.run(["kubectl", "patch", "configmap/open5gs-smf", "-n", "open5gs", "--type=merge", "-p", json.dumps(patch)], check=True)
print("PFCP headless peer configured; restart SMF via recover-service.sh to load it.")
