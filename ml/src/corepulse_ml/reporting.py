"""Schema-3 composition; reference results remain mandatory and independently checked."""

from __future__ import annotations

import copy
import json
import os
import re
import statistics
import tempfile
from pathlib import Path

from corepulse_ml.data import WindowDataset, timestamp_ns
from corepulse_ml.metrics import by_run, measure
from corepulse_ml.numeric import is_finite_number

REFERENCES = {
    "nearest_centroid": "scenario_classification",
    "majority_scenario": "scenario_classification",
    "normal_zscore": "normal_vs_fault_interval",
    "probe_rule": "normal_vs_fault_interval",
}
_METHOD = re.compile(r"[a-z][a-z0-9_]{0,95}\Z")


def labels_for(dataset: WindowDataset, task: str) -> list[str]:
    if task == "scenario_classification":
        return sorted(
            {dataset.rows[i].scenario for i in (*dataset.train_indices, *dataset.test_indices)}
        )
    if task == "normal_vs_fault_interval":
        return ["normal", "fault"]
    raise ValueError("unsupported evaluation task")


def _equivalent(actual, expected) -> bool:
    if isinstance(expected, float):
        return is_finite_number(actual) and abs(actual - expected) <= 1e-12
    if isinstance(expected, dict):
        return (
            isinstance(actual, dict)
            and actual.keys() == expected.keys()
            and all(_equivalent(actual[k], v) for k, v in expected.items())
        )
    if isinstance(expected, list):
        return (
            isinstance(actual, list)
            and len(actual) == len(expected)
            and all(_equivalent(a, b) for a, b in zip(actual, expected, strict=True))
        )
    return type(actual) is type(expected) and actual == expected


def validate_method(dataset: WindowDataset, name: str, model: dict) -> None:
    if not isinstance(name, str) or not _METHOD.fullmatch(name) or not isinstance(model, dict):
        raise ValueError("invalid named method")
    task = model.get("task")
    labels = labels_for(dataset, task)
    if name in REFERENCES and task != REFERENCES[name]:
        raise ValueError("reference method task changed")
    expected = {(row.run_id, row.start_ns): row.scenario for row in dataset.partition("test")}
    predictions = model.get("predictions")
    if not isinstance(predictions, list) or len(predictions) != len(expected):
        raise ValueError("method must retain every held-out prediction")
    seen = set()
    for prediction in predictions:
        if not isinstance(prediction, dict):
            raise ValueError("invalid prediction object")
        try:
            key = prediction["run_id"], timestamp_ns(prediction["window_start"])
            truth = expected[key]
        except (KeyError, TypeError) as exc:
            raise ValueError("prediction identity differs from held-out windows") from exc
        if task == "normal_vs_fault_interval" and truth != "normal":
            truth = "fault"
        if key in seen or prediction.get("truth") != truth:
            raise ValueError("duplicate prediction or inconsistent truth")
        if prediction.get("predicted") not in ["", *labels]:
            raise ValueError("unsupported predicted class")
        score = prediction.get("score")
        if score is not None and not is_finite_number(score):
            raise ValueError("non-finite prediction score")
        seen.add(key)
    expected_window = measure(labels, predictions)
    expected_run = measure(labels, by_run(labels, predictions))
    if not (
        _equivalent(model.get("window_metrics"), expected_window)
        and _equivalent(model.get("run_metrics"), expected_run)
    ):
        raise ValueError("stored scores/confusion/support disagree with predictions")


def spread(values: list[float]) -> dict:
    if not values or not all(is_finite_number(v) for v in values):
        raise ValueError("seed spread requires finite observations")
    return {
        "mean": statistics.mean(values),
        "stddev": statistics.pstdev(values),
        "min": min(values),
        "max": max(values),
    }


def seed_spread(models: dict) -> dict:
    groups = {}
    for name, model in models.items():
        family, seed = model.get("family"), model.get("seed")
        if family is None and seed is None:
            continue
        if (
            not isinstance(family, str)
            or not _METHOD.fullmatch(family)
            or type(seed) is not int
            or not 0 <= seed < 2**63
            or name in REFERENCES
        ):
            raise ValueError("invalid seed/family declaration")
        groups.setdefault(family, []).append((seed, name, model))
    result = {}
    for family, members in sorted(groups.items()):
        members.sort()
        if len({seed for seed, _, _ in members}) != len(members):
            raise ValueError("duplicate model seed within a family")
        tasks = {model["task"] for _, _, model in members}
        if len(tasks) != 1:
            raise ValueError("seed spread cannot compare different tasks")
        result[family] = {
            "task": tasks.pop(),
            "seeds": [seed for seed, _, _ in members],
            "methods": [name for _, name, _ in members],
            "window_macro_f1": spread([m["window_metrics"]["macro_f1"] for _, _, m in members]),
            "run_macro_f1": spread([m["run_metrics"]["macro_f1"] for _, _, m in members]),
            "coverage": spread([m["window_metrics"]["coverage"] for _, _, m in members]),
        }
    return result


def compose_report(
    dataset: WindowDataset, reference: dict, methods: dict, experiment: dict
) -> dict:
    if reference.get("synthetic") is not True or experiment.get("synthetic") is not True:
        raise ValueError("this neural prototype requires explicitly synthetic inputs/references")
    if (
        type(reference.get("schema_version")) is not int
        or reference["schema_version"] != dataset.schema_version
    ):
        raise ValueError("reference report/split schema mismatch")
    if reference.get("windows_sha256") != dataset.windows_sha256 or not _equivalent(
        reference.get("split"), dataset.manifest
    ):
        raise ValueError("reference report source hash/split mismatch")
    counts = {
        "train_windows": len(dataset.train_indices),
        "test_windows": len(dataset.test_indices),
        "purged_windows": len(dataset.purged_indices),
        "normal_training_windows": sum(r.scenario == "normal" for r in dataset.partition("train")),
    }
    if any(type(reference.get(k)) is not int or reference[k] != v for k, v in counts.items()):
        raise ValueError("reference report support mismatch")
    if counts["normal_training_windows"] < 2:
        raise ValueError("at least two normal training windows are required")
    limitations = reference.get("limitations")
    if not isinstance(limitations, list) or any(not isinstance(v, str) for v in limitations):
        raise ValueError("invalid reference limitations")
    threshold = reference.get("zscore_threshold")
    if not is_finite_number(threshold) or threshold <= 0:
        raise ValueError("invalid reference threshold")
    original = reference.get("models")
    if not isinstance(original, dict) or set(original) != set(REFERENCES):
        raise ValueError("all four reference methods are required")
    if set(methods) & set(REFERENCES):
        raise ValueError("extra methods cannot replace a reference")
    models = copy.deepcopy(original | methods)
    for name, model in models.items():
        validate_method(dataset, name, model)
    report = copy.deepcopy(reference)
    report.update(
        schema_version=3,
        models=models,
        seed_spread=seed_spread(models),
        experiment=copy.deepcopy(experiment),
    )
    report["limitations"] = [
        *reference.get("limitations", []),
        "SYNTHETIC NEURAL DEVELOPMENT: invented inputs are not real-lab scores.",
        "Inner validation uses outer training groups only; session declarations "
        "and capture-time embargoes do not certify measurement independence.",
    ]
    return report


def write_report(path: str | Path, report: dict, sources=()) -> None:
    output = Path(path).resolve()
    for source in sources:
        source = Path(source).resolve()
        if str(output).casefold() == str(source).casefold() or (
            output.exists() and source.exists() and output.samefile(source)
        ):
            raise ValueError("report output must not overwrite/alias an input")
    # Serialize fully before changing the previous report or creating output directories.
    raw = (json.dumps(report, indent=2, sort_keys=True, allow_nan=False) + "\n").encode()
    output.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=".neural-report-", dir=output.parent)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, output)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
