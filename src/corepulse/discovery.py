"""Read-only inventory of the existing lab and candidate metrics endpoints."""

from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from .kubectl import Kubectl

LAB_NAMESPACE = "open5gs"


def _item(resource: dict[str, Any]) -> dict[str, Any]:
    metadata = resource.get("metadata", {})
    spec = resource.get("spec", {})
    status = resource.get("status", {})
    return {
        "name": metadata.get("name"),
        "labels": metadata.get("labels", {}),
        "annotations": metadata.get("annotations", {}),
        "ports": spec.get("ports", []),
        "phase": status.get("phase"),
        "ready_replicas": status.get("readyReplicas"),
        "replicas": spec.get("replicas"),
    }


def analyze_snapshot(snapshot: dict[str, Any], context: str) -> dict[str, Any]:
    """Transform a kubectl List into an inventory without claiming scrape success."""
    resources = snapshot.get("items")
    if not isinstance(resources, list):
        raise ValueError("Expected a Kubernetes List with an items array")

    inventory: dict[str, list[dict[str, Any]]] = {
        "deployments": [], "pods": [], "services": []
    }
    candidates: list[dict[str, Any]] = []
    for resource in resources:
        if not isinstance(resource, dict):
            continue
        kind = resource.get("kind")
        key = {"Deployment": "deployments", "Pod": "pods", "Service": "services"}.get(kind)
        if key is None:
            continue
        entry = _item(resource)
        inventory[key].append(entry)
        annotations = entry["annotations"]
        if annotations.get("prometheus.io/scrape", "").lower() == "true":
            candidates.append({
                "kind": kind,
                "name": entry["name"],
                "path": annotations.get("prometheus.io/path", "/metrics"),
                "port": annotations.get("prometheus.io/port"),
                "verified": False,
            })

    for values in inventory.values():
        values.sort(key=lambda item: item["name"] or "")
    candidates.sort(key=lambda item: (item["kind"], item["name"] or ""))
    names = {entry["name"] for entry in inventory["deployments"]}
    return {
        "schema_version": 1,
        "generated_at": datetime.now(timezone.utc).isoformat(),
        "context": context,
        "namespace": LAB_NAMESPACE,
        "inventory": inventory,
        "expected_deployments": {
            name: name in names for name in ("open5gs-amf", "open5gs-upf", "ueransim-gnb", "ueransim-ue")
        },
        "prometheus_annotation_candidates": candidates,
        "metric_names_verified": [],
        "note": "Annotations suggest scrape candidates; they do not prove an endpoint or metric is available.",
    }


def discover(client: Kubectl) -> dict[str, Any]:
    context = client.run("config", "current-context")
    snapshot = client.get_json(
        "get", "deployments,pods,services", "--namespace", LAB_NAMESPACE,
        "--request-timeout=10s",
    )
    return analyze_snapshot(snapshot, context)
