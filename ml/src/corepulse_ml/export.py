"""Version-one JSON coefficients; no pickle, checkpoint loader or executable state."""

from __future__ import annotations

import hashlib
import json
import re
from pathlib import Path

import torch
from torch import nn

from corepulse_ml.data import WindowDataset, near, run_ranges, validate_features
from corepulse_ml.numeric import duration_ns, is_finite_number
from corepulse_ml.reporting import labels_for, write_report
from corepulse_ml.training import TrainedMethod

MAX_ARTIFACT_BYTES = 16 * 1024 * 1024
_HASH = re.compile(r"[0-9a-f]{64}\Z")
_FLOAT32_MAX = float(torch.finfo(torch.float32).max)


def _keys(value, expected, label):
    if not isinstance(value, dict) or value.keys() != set(expected):
        raise ValueError(f"unsupported {label} fields")


def _numbers(values, length, label, *, positive=False, float32=False):
    if (
        not isinstance(values, list)
        or len(values) != length
        or any(
            not is_finite_number(x) or positive and x <= 0 or float32 and abs(x) > _FLOAT32_MAX
            for x in values
        )
    ):
        raise ValueError(f"invalid {label} dimensions or numbers")


def _ids(values, label):
    if (
        not isinstance(values, list)
        or not values
        or any(not isinstance(v, str) or not v.strip() for v in values)
        or len(set(values)) != len(values)
    ):
        raise ValueError(f"invalid {label}")


def validate_artifact(artifact: dict) -> None:
    kind = artifact.get("kind") if isinstance(artifact, dict) else None
    if kind not in ("mlp", "autoencoder"):
        raise ValueError("unsupported artifact kind")
    fields = {
        "schema_version",
        "kind",
        "dtype",
        "synthetic",
        "source",
        "preprocessing",
        "layers",
        "labels",
        "task",
        "seed",
    }
    if kind == "autoencoder":
        fields |= {"threshold", "calibration_run_ids"}
    _keys(artifact, fields, "artifact")
    if type(artifact["schema_version"]) is not int or artifact["schema_version"] != 1:
        raise ValueError("unsupported artifact schema version")
    if artifact["dtype"] != "float32" or type(artifact["synthetic"]) is not bool:
        raise ValueError("artifact requires float32 and an explicit synthetic declaration")
    if type(artifact["seed"]) is not int or not 0 <= artifact["seed"] < 2**63:
        raise ValueError("invalid artifact seed")
    source = artifact["source"]
    _keys(source, {"windows_sha256", "split_sha256"}, "source")
    if any(not isinstance(h, str) or not _HASH.fullmatch(h) for h in source.values()):
        raise ValueError("invalid artifact source hash")
    pre = artifact["preprocessing"]
    _keys(
        pre,
        {"feature_names", "medians", "means", "scales", "dropped_features", "fit_run_ids"},
        "preprocessing",
    )
    features = validate_features(pre["feature_names"])
    if not isinstance(pre["feature_names"], list):
        raise ValueError("feature_names must be an ordered list")
    dropped = pre["dropped_features"]
    if not isinstance(dropped, list):
        raise ValueError("invalid dropped features")
    if dropped:
        validate_features(dropped)
    if set(features) & set(dropped):
        raise ValueError("retained and dropped features overlap")
    _ids(pre["fit_run_ids"], "preprocessing fit identities")
    dimension = len(features)
    _numbers(pre["medians"], dimension, "medians")
    _numbers(pre["means"], dimension, "means")
    _numbers(pre["scales"], dimension, "scales", positive=True)
    labels = artifact["labels"]
    if kind == "mlp":
        if (
            artifact["task"] != "scenario_classification"
            or not isinstance(labels, list)
            or not 2 <= len(labels) <= 3
            or any(label not in ("normal", "pod_crash:amf", "pod_crash:upf") for label in labels)
            or labels != sorted(set(labels))
            or "normal" not in labels
        ):
            raise ValueError("invalid scenario labels/task")
    else:
        if artifact["task"] != "normal_vs_fault_interval" or labels != ["normal", "fault"]:
            raise ValueError("invalid autoencoder labels/task")
        threshold = artifact["threshold"]
        if not is_finite_number(threshold) or threshold < 0:
            raise ValueError("invalid autoencoder threshold")
        _ids(artifact["calibration_run_ids"], "normal calibration identities")
        if set(artifact["calibration_run_ids"]) & set(pre["fit_run_ids"]):
            raise ValueError("calibration identities overlap normal fit identities")
    layers = artifact["layers"]
    if not isinstance(layers, list) or not 1 <= len(layers) <= 32:
        raise ValueError("invalid layer count")
    previous, parameters = "", 0
    for index, layer in enumerate(layers):
        if not isinstance(layer, dict):
            raise ValueError("invalid layer object")
        if layer.get("op") == "relu":
            _keys(layer, {"op"}, "relu")
            if previous != "linear" or index == len(layers) - 1:
                raise ValueError("relu must follow a linear layer and cannot be the output")
        elif layer.get("op") == "linear":
            _keys(layer, {"op", "weights", "bias"}, "linear")
            weights = layer["weights"]
            if not isinstance(weights, list) or not 1 <= len(weights) <= 4096:
                raise ValueError("invalid linear output dimension")
            for row in weights:
                _numbers(row, dimension, "linear weights", float32=True)
            _numbers(layer["bias"], len(weights), "linear bias", float32=True)
            parameters += len(weights) * (dimension + 1)
            if parameters > 1_000_000:
                raise ValueError("artifact exceeds coefficient limit")
            dimension = len(weights)
        else:
            raise ValueError("unsupported layer operator")
        previous = layer["op"]
    expected = len(labels) if kind == "mlp" else len(features)
    if previous != "linear" or dimension != expected:
        raise ValueError("network output dimension does not match task")


def artifact_for(method: TrainedMethod, dataset: WindowDataset, *, synthetic: bool) -> dict:
    pre = method.preprocessor
    if pre.windows_sha256 != dataset.windows_sha256 or method.split_sha256 != dataset.split_sha256:
        raise ValueError("trained method source/split differs from verified dataset")
    if not set(pre.fit_run_ids) <= set(dataset.manifest["train_runs"]):
        raise ValueError("preprocessing fit identities escape outer training")
    if tuple(method.labels) != tuple(labels_for(dataset, method.report.get("task"))):
        raise ValueError("model labels differ from the verified dataset task")
    if method.kind == "mlp" and set(pre.fit_run_ids) != set(dataset.manifest["train_runs"]):
        raise ValueError("final MLP preprocessing must cover all outer training runs")
    if set(pre.feature_names) | set(pre.dropped_features) != set(dataset.features):
        raise ValueError("trained feature selection differs from dataset")
    if method.report.get("family") != method.kind:
        raise ValueError("trained method family differs from artifact kind")
    if type(method.model) is not nn.Sequential:
        raise ValueError("only sequential dense/ReLU networks can be exported")
    layers = []
    for layer in method.model:
        if type(layer) is nn.Linear:
            if layer.bias is None or any(
                p.device.type != "cpu" or p.dtype != torch.float32 for p in layer.parameters()
            ):
                raise ValueError("only CPU float32 linear layers with bias can be exported")
            layers.append(
                {
                    "op": "linear",
                    "weights": layer.weight.detach().tolist(),
                    "bias": layer.bias.detach().tolist(),
                }
            )
        elif type(layer) is nn.ReLU:
            layers.append({"op": "relu"})
        else:
            raise ValueError("unsupported model operator")
    artifact = {
        "schema_version": 1,
        "kind": method.kind,
        "dtype": "float32",
        "synthetic": synthetic,
        "source": {"windows_sha256": dataset.windows_sha256, "split_sha256": dataset.split_sha256},
        "preprocessing": {
            "feature_names": list(pre.feature_names),
            "medians": list(pre.medians),
            "means": list(pre.means),
            "scales": list(pre.scales),
            "dropped_features": list(pre.dropped_features),
            "fit_run_ids": list(pre.fit_run_ids),
        },
        "layers": layers,
        "labels": list(method.labels),
        "task": method.report["task"],
        "seed": method.report["seed"],
    }
    if method.kind == "autoencoder":
        calibration = method.training["normal_validation_run_ids"]
        _ids(calibration, "normal calibration identities")
        normal_runs = {row.run_id for row in dataset.partition("train") if row.scenario == "normal"}
        if not set(pre.fit_run_ids) <= normal_runs or not set(calibration) <= normal_runs:
            raise ValueError(
                "autoencoder fit/calibration identities must be normal outer training runs"
            )
        if dataset.schema_version == 2:
            sessions = dataset.manifest["session_assignments"]
            if {sessions[run] for run in pre.fit_run_ids} & {sessions[run] for run in calibration}:
                raise ValueError("autoencoder calibration crosses a whole-session fit boundary")
        margin = duration_ns(dataset.manifest.get("embargo_seconds", 0))
        if margin:
            ranges = run_ranges(dataset.partition("train"))
            if any(
                near(ranges[fit], ranges[val], margin)
                for fit in pre.fit_run_ids
                for val in calibration
            ):
                raise ValueError("autoencoder calibration violates the inner capture-time embargo")
        artifact.update(threshold=method.threshold, calibration_run_ids=list(calibration))
    validate_artifact(artifact)
    return artifact


def ensure_artifact_destination(path: str | Path) -> Path:
    path = Path(path).resolve()
    root = Path(__file__).resolve().parents[3]
    allowed = [root / name for name in ("reports", "artifacts", "models", "ml/outputs")]
    if path.is_relative_to(root) and not any(path.is_relative_to(p) for p in allowed):
        raise ValueError(
            "trained coefficients must stay in ignored output directories or outside the repository"
        )
    return path


def write_artifact(path: str | Path, artifact: dict, sources=()) -> str:
    path = ensure_artifact_destination(path)
    validate_artifact(artifact)
    raw = (json.dumps(artifact, indent=2, sort_keys=True, allow_nan=False) + "\n").encode()
    if len(raw) > MAX_ARTIFACT_BYTES:
        raise ValueError("artifact exceeds 16 MiB JSON limit")
    write_report(path, artifact, sources)
    return hashlib.sha256(raw).hexdigest()
