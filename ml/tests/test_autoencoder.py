import copy
from dataclasses import replace

import numpy as np
import pytest
import torch
import yaml

from corepulse_ml.autoencoder import classify_error, read_options, train_autoencoder
from corepulse_ml.data import SIGNALS, load_windows
from corepulse_ml.experiment import run_experiment
from corepulse_ml.reporting import validate_method
from corepulse_ml.validation import grouped_folds

from .helpers import invented_windows, read_csv, rehash, write_csv
from .test_reporting import config_file


def options():
    return {
        "kind": "autoencoder",
        "hidden": [4, 2],
        "epochs": 3,
        "learning_rate": 0.01,
        "weight_decay": 0.0,
        "percentile": 95,
        "calibration_fold": 0,
    }


def test_autoencoder_calibrates_on_disjoint_normal_groups_and_keeps_all_predictions(tmp_path):
    data = load_windows(*invented_windows(tmp_path))
    folds = grouped_folds(data)
    first = train_autoencoder(data, folds, options(), 7)
    second = train_autoencoder(data, folds, options(), 7)
    assert first.report == second.report
    normal_runs = {r.run_id for r in data.partition("train") if r.scenario == "normal"}
    metadata = first.training
    assert set(metadata["normal_fit_run_ids"]) < normal_runs
    assert set(metadata["normal_validation_run_ids"]) < normal_runs
    assert set(metadata["normal_fit_run_ids"]).isdisjoint(metadata["normal_validation_run_ids"])
    assert first.threshold == np.percentile(
        metadata["normal_validation_errors"], 95, method="higher"
    )
    assert metadata["normal_validation_windows"] == 2
    assert first.preprocessor.fit_run_ids == tuple(metadata["normal_fit_run_ids"])
    validate_method(data, "autoencoder_seed_7", first.report)
    assert all(p.device.type == "cpu" for p in first.model.parameters())


def test_fault_training_and_outer_test_values_cannot_influence_normal_fit_or_threshold(tmp_path):
    windows, split = invented_windows(tmp_path)
    data = load_windows(windows, split)
    before = train_autoencoder(data, grouped_folds(data), options(), 23)
    rows = read_csv(windows)
    column = rows[0].index("cpu_millicores_mean")
    for row in rows[1:]:
        if row[1] != "normal" or row[0] in data.manifest["test_runs"]:
            row[column] = "999999"
    write_csv(windows, rows)
    rehash(windows, split)
    changed = load_windows(windows, split)
    after = train_autoencoder(changed, grouped_folds(changed), options(), 23)
    assert before.threshold == after.threshold
    assert before.training == after.training
    assert before.preprocessor.medians == after.preprocessor.medians
    assert before.preprocessor.means == after.preprocessor.means
    assert before.preprocessor.scales == after.preprocessor.scales
    for a, b in zip(before.model.parameters(), after.model.parameters(), strict=True):
        assert torch.equal(a, b)


def test_autoencoder_refuses_leaking_or_missing_calibration_groups(tmp_path):
    data = load_windows(*invented_windows(tmp_path))
    folds = grouped_folds(data)
    leaking = replace(folds[0], fit_indices=(*folds[0].fit_indices, data.test_indices[0]))
    for invalid in ((), (folds[0], folds[0]), (leaking, folds[1])):
        with pytest.raises(ValueError):
            train_autoencoder(data, invalid, options(), 7)


def test_autoencoder_refuses_too_few_normal_calibration_windows(tmp_path):
    windows, split = invented_windows(tmp_path)
    rows = read_csv(windows)
    seen = set()
    kept = [rows[0]]
    for row in rows[1:]:
        if row[1] == "normal":
            if row[0] in seen:
                continue
            seen.add(row[0])
        kept.append(row)
    write_csv(windows, kept)
    rehash(windows, split)
    data = load_windows(windows, split)
    with pytest.raises(ValueError, match="two normal"):
        train_autoencoder(data, grouped_folds(data), options(), 7)


def test_threshold_equality_remains_normal_and_nonfinite_values_refuse():
    assert classify_error(0, 0) == "normal"
    assert classify_error(0.5, 0.5) == "normal"
    assert classify_error(0.50001, 0.5) == "fault"
    for error, threshold in ((float("nan"), 0), (0, float("inf")), (-1, 0), (0, -1)):
        with pytest.raises(ValueError):
            classify_error(error, threshold)


@pytest.mark.parametrize(
    "field,value",
    [
        ("epochs", [1, 3]),
        ("epochs", True),
        ("epochs", 0),
        ("hidden", []),
        ("hidden", [False]),
        ("percentile", 0),
        ("percentile", 101),
        ("percentile", float("nan")),
        ("calibration_fold", -1),
        ("calibration_fold", True),
        ("learning_rate", 0),
        ("weight_decay", -1),
        ("unexpected", 1),
    ],
)
def test_autoencoder_requires_fixed_valid_training_and_calibration_options(field, value):
    config = copy.deepcopy(options())
    config[field] = value
    with pytest.raises(ValueError):
        read_options(config)


def test_yaml_autoencoder_reports_every_seed_and_preserves_reference_methods(tmp_path):
    path, config = config_file(tmp_path)
    config["methods"] = [options()]
    path.write_text(yaml.safe_dump(config))
    report = run_experiment(path)
    assert len(report["models"]) == 6
    assert report["seed_spread"]["autoencoder"]["seeds"] == [7, 23]
    assert report["seed_spread"]["autoencoder"]["task"] == "normal_vs_fault_interval"
    assert report["synthetic"] is True


def test_autoencoder_cannot_borrow_fault_values_when_normal_resources_are_all_missing(tmp_path):
    windows, split = invented_windows(tmp_path)
    data = load_windows(windows, split)
    rows = read_csv(windows)
    columns = {name: rows[0].index(name) for name in rows[0]}
    for row in rows[1:]:
        if row[0] in data.manifest["train_runs"] and row[1] == "normal":
            for signal in SIGNALS[:7]:
                for suffix in ("mean", "max", "delta", "missing_ratio"):
                    row[columns[signal + "_" + suffix]] = "1" if suffix == "missing_ratio" else ""
    write_csv(windows, rows)
    rehash(windows, split)
    changed = load_windows(windows, split)
    with pytest.raises(ValueError, match="no observed training resource"):
        train_autoencoder(changed, grouped_folds(changed), options(), 7)
