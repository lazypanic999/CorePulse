"""A guarded first fault experiment for the isolated Open5GS lab."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any
from uuid import uuid4

from .discovery import LAB_NAMESPACE
from .kubectl import Kubectl

ALLOWED_TARGETS = {
    "amf": "open5gs-amf",
    "upf": "open5gs-upf",
}
LAB_CONTEXT = "open5gs-lab"


class SafetyError(RuntimeError):
    pass


def plan_pod_crash(target: str) -> dict[str, Any]:
    if target not in ALLOWED_TARGETS:
        raise SafetyError(f"Target must be one of: {', '.join(sorted(ALLOWED_TARGETS))}")
    deployment = ALLOWED_TARGETS[target]
    return {
        "fault": "pod_crash",
        "target": target,
        "namespace": LAB_NAMESPACE,
        "deployment": deployment,
        "effect": "Delete one ready, deployment-owned pod; Kubernetes creates its replacement.",
        "recovery": "Wait for the deployment to become available again.",
    }


def _selected_pod(client: Kubectl, deployment: str) -> str:
    data = client.get_json("get", f"deployment/{deployment}", "--namespace", LAB_NAMESPACE)
    spec = data.get("spec", {})
    selector = spec.get("selector", {})
    labels = selector.get("matchLabels", {})
    if not labels or selector.get("matchExpressions"):
        raise SafetyError("Deployment selector is missing or has unsupported expressions")
    if spec.get("replicas", 1) != 1 or data.get("status", {}).get("readyReplicas") != 1:
        raise SafetyError("Target deployment must have exactly one ready replica")
    label_arg = ",".join(f"{key}={value}" for key, value in sorted(labels.items()))
    pods = client.get_json(
        "get", "pods", "--namespace", LAB_NAMESPACE, "--selector", label_arg
    ).get("items", [])
    if len(pods) != 1:
        raise SafetyError("Expected exactly one matching pod")
    pod = pods[0]
    if pod.get("status", {}).get("phase") != "Running":
        raise SafetyError("Target pod is not running")
    conditions = pod.get("status", {}).get("conditions", [])
    if not any(c.get("type") == "Ready" and c.get("status") == "True" for c in conditions):
        raise SafetyError("Target pod is not ready")
    name = pod.get("metadata", {}).get("name")
    if not name:
        raise SafetyError("Target pod has no name")
    owners = pod.get("metadata", {}).get("ownerReferences", [])
    if not any(
        owner.get("kind") == "ReplicaSet"
        and owner.get("name", "").startswith(f"{deployment}-")
        for owner in owners
    ):
        raise SafetyError("Matching pod is not owned by the expected deployment")
    return name


def run_pod_crash(client: Kubectl, target: str) -> dict[str, Any]:
    plan = plan_pod_crash(target)
    context = client.run("config", "current-context")
    if context != LAB_CONTEXT:
        raise SafetyError(
            f"Current context {context!r} differs from lab context {LAB_CONTEXT!r}"
        )
    deployment = plan["deployment"]
    pod_name = _selected_pod(client, deployment)
    started = datetime.now(timezone.utc)
    run_id = str(uuid4())
    result = {
        "schema_version": 1,
        "run_id": run_id,
        "fault": "pod_crash",
        "target": target,
        "context": context,
        "namespace": LAB_NAMESPACE,
        "pod_deleted": pod_name,
        "started_at": started.isoformat(),
        "ended_at": None,
        "status": "recovery_pending",
    }
    try:
        client.run("delete", f"pod/{pod_name}", "--namespace", LAB_NAMESPACE, "--wait=false")
        client.run(
            "wait", "--for=delete", f"pod/{pod_name}",
            "--namespace", LAB_NAMESPACE, "--timeout=180s", timeout=210,
        )
        client.run(
            "rollout", "status", f"deployment/{deployment}",
            "--namespace", LAB_NAMESPACE, "--timeout=180s", timeout=210,
        )
        replacement = _selected_pod(client, deployment)
        if replacement == pod_name:
            raise SafetyError("No replacement pod was observed")
        result["replacement_pod"] = replacement
        result["status"] = "recovered"
    except Exception as exc:
        result["status"] = "recovery_unverified"
        result["error"] = str(exc)
    finally:
        result["ended_at"] = datetime.now(timezone.utc).isoformat()
    return result
