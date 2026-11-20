import copy
import json

import pytest
import yaml

from corepulse_ml.data import load_windows
from corepulse_ml.experiment import read_experiment, run_experiment
from corepulse_ml.metrics import by_run, measure, model_report
from corepulse_ml.reporting import REFERENCES, compose_report, labels_for, write_report

from .helpers import invented_windows


def invented_reference(dataset):
    """A hand-authored schema snapshot, not the Go algorithms or measured performance."""
    models = {}
    for name, task in REFERENCES.items():
        rows = []
        for row in dataset.partition("test"):
            truth = row.scenario
            if task == "normal_vs_fault_interval" and truth != "normal":
                truth = "fault"
            rows.append(
                {
                    "run_id": row.run_id,
                    "window_start": row.window_start,
                    "truth": truth,
                    "predicted": "normal",
                }
            )
        models[name] = model_report(task, labels_for(dataset, task), rows)
    return {
        "schema_version": dataset.schema_version,
        "synthetic": True,
        "windows_sha256": dataset.windows_sha256,
        "split": dataset.manifest,
        "train_windows": len(dataset.train_indices),
        "test_windows": len(dataset.test_indices),
        "purged_windows": len(dataset.purged_indices),
        "normal_training_windows": 4,
        "zscore_threshold": 3,
        "models": models,
        "limitations": ["Invented unit fixture only"],
    }


def config_file(tmp_path):
    windows, split = invented_windows(tmp_path)
    dataset = load_windows(windows, split)
    references, output, config = (
        tmp_path / name for name in ("refs.json", "report.json", "run.yaml")
    )
    references.write_text(json.dumps(invented_reference(dataset)))
    value = {
        "schema_version": 1,
        "name": "invented-smoke",
        "synthetic": True,
        "dataset": {"windows": str(windows), "split": str(split)},
        "references": str(references),
        "output": str(output),
        "seeds": [7, 23],
        "inner_validation": {"folds": 2, "seed": 17},
        "methods": [],
    }
    config.write_text(yaml.safe_dump(value))
    return config, value


def test_metrics_keep_abstentions_and_use_go_run_vote_ties():
    rows = [
        {"run_id": "r", "truth": "normal", "predicted": "normal"},
        {"run_id": "r", "truth": "normal", "predicted": "fault"},
        {"run_id": "f", "truth": "fault", "predicted": ""},
        {"run_id": "f", "truth": "fault", "predicted": "fault"},
    ]
    labels = ["normal", "fault"]
    metrics = measure(labels, rows)
    assert metrics["confusion"] == [[1, 1], [0, 1]]
    assert metrics["classes"]["fault"]["support"] == 2
    assert metrics["coverage"] == 0.75
    assert metrics["macro_f1"] == pytest.approx((2 / 3 + 0.5) / 2)
    assert measure(labels, by_run(labels, rows))["macro_f1"] == 1


def test_report_retains_four_references_every_prediction_and_seed_spread(tmp_path):
    data = load_windows(*invented_windows(tmp_path))
    reference = invented_reference(data)
    before = copy.deepcopy(reference)
    extra = {}
    for seed in (7, 23):
        rows = copy.deepcopy(reference["models"]["nearest_centroid"]["predictions"])
        if seed == 23:
            rows[0]["predicted"] = ""
        extra[f"invented_seed_{seed}"] = model_report(
            "scenario_classification",
            labels_for(data, "scenario_classification"),
            rows,
            family="invented",
            seed=seed,
        )
    report = compose_report(data, reference, extra, {"synthetic": True})
    assert report["schema_version"] == 3
    assert report["split"]["schema_version"] == 2
    assert reference == before
    assert set(REFERENCES) <= report["models"].keys()
    assert all(len(model["predictions"]) == 6 for model in report["models"].values())
    summary = report["seed_spread"]["invented"]
    assert summary["seeds"] == [7, 23]
    assert summary["coverage"]["min"] == 5 / 6
    assert summary["coverage"]["max"] == 1
    assert summary["coverage"]["stddev"] > 0


@pytest.mark.parametrize(
    "mode",
    [
        "missing-ref",
        "corrupt-score",
        "dropped-prediction",
        "wrong-time",
        "wrong-truth",
        "nonfinite",
        "replace-ref",
    ],
)
def test_inconsistent_reference_or_extra_method_refuses(tmp_path, mode):
    data = load_windows(*invented_windows(tmp_path))
    ref = invented_reference(data)
    extra = {"invented": copy.deepcopy(ref["models"]["nearest_centroid"])}
    if mode == "missing-ref":
        del ref["models"]["probe_rule"]
    elif mode == "corrupt-score":
        ref["models"]["nearest_centroid"]["window_metrics"]["macro_f1"] = 1
    elif mode == "dropped-prediction":
        extra["invented"]["predictions"].pop()
    elif mode == "wrong-time":
        extra["invented"]["predictions"][0]["window_start"] = "2026-01-01T00:00:00Z"
    elif mode == "wrong-truth":
        extra["invented"]["predictions"][0]["truth"] = "pod_crash:upf"
    elif mode == "nonfinite":
        extra["invented"]["predictions"][0]["score"] = float("nan")
    else:
        extra = {"nearest_centroid": extra["invented"]}
    with pytest.raises(ValueError):
        compose_report(data, ref, extra, {"synthetic": True})


def test_yaml_runner_is_deterministic_and_bad_input_preserves_previous_report(tmp_path):
    path, config = config_file(tmp_path)
    first = run_experiment(path)
    output = tmp_path / "report.json"
    before = output.read_bytes()
    assert run_experiment(path) == first
    assert output.read_bytes() == before
    references = tmp_path / "refs.json"
    ref = json.loads(references.read_text())
    ref["models"]["probe_rule"]["predictions"].pop()
    references.write_text(json.dumps(ref))
    with pytest.raises(ValueError):
        run_experiment(path)
    assert output.read_bytes() == before
    with pytest.raises(ValueError, match="overwrite"):
        write_report(config["dataset"]["windows"], first, [config["dataset"]["windows"]])
    with pytest.raises(ValueError):
        write_report(output, {"invalid": float("nan")})
    assert output.read_bytes() == before


@pytest.mark.parametrize("mode", ["unknown", "duplicate", "real", "seed", "schema", "feature"])
def test_yaml_configuration_is_strict(tmp_path, mode):
    path, config = config_file(tmp_path)
    if mode == "unknown":
        config["typo"] = 1
    elif mode == "real":
        config["synthetic"] = False
    elif mode == "seed":
        config["seeds"] = [7, 7]
    elif mode == "schema":
        config["schema_version"] = True
    elif mode == "feature":
        config["features"] = [["label"]]
    path.write_text(yaml.safe_dump(config))
    if mode == "duplicate":
        path.write_text(path.read_text() + "name: override\n")
    with pytest.raises(ValueError):
        read_experiment(path)
