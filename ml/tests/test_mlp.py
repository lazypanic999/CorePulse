import copy
from dataclasses import replace

import pytest
import torch

from corepulse_ml.data import load_windows
from corepulse_ml.mlp import read_options, train_mlp
from corepulse_ml.reporting import validate_method
from corepulse_ml.validation import grouped_folds

from .helpers import invented_windows, read_csv, rehash, write_csv


def options():
    return {
        "kind": "mlp",
        "hidden": [4],
        "epochs": [1, 3],
        "learning_rate": 0.01,
        "weight_decay": 0.0,
    }


def test_mlp_is_repeatable_cpu_and_keeps_every_heldout_prediction(tmp_path):
    data = load_windows(*invented_windows(tmp_path))
    folds = grouped_folds(data)
    state = torch.get_rng_state().clone()
    deterministic = torch.are_deterministic_algorithms_enabled()
    first, second = (train_mlp(data, folds, options(), 7) for _ in range(2))
    assert torch.equal(state, torch.get_rng_state())
    assert deterministic == torch.are_deterministic_algorithms_enabled()
    assert first.report == second.report
    assert first.training["selected_epochs"] in (1, 3)
    for a, b in zip(first.model.parameters(), second.model.parameters(), strict=True):
        assert a.device.type == "cpu"
        assert torch.equal(a, b)
    validate_method(data, "mlp_seed_7", first.report)
    assert len(first.report["predictions"]) == len(data.test_indices)
    for fold in first.training["folds"]:
        assert set(fold["fit_run_ids"]).isdisjoint(fold["validation_run_ids"])
        assert set(fold["fit_run_ids"]).isdisjoint(data.manifest["test_runs"])


def test_mlp_test_values_cannot_change_selection_preprocessing_or_weights(tmp_path):
    windows, split = invented_windows(tmp_path)
    data = load_windows(windows, split)
    before = train_mlp(data, grouped_folds(data), options(), 23)
    rows = read_csv(windows)
    column = rows[0].index("cpu_millicores_mean")
    for row in rows[1:]:
        if row[0] in data.manifest["test_runs"]:
            row[column] = "999999"
    write_csv(windows, rows)
    rehash(windows, split)
    changed = load_windows(windows, split)
    after = train_mlp(changed, grouped_folds(changed), options(), 23)
    assert before.training == after.training
    assert before.preprocessor.medians == after.preprocessor.medians
    assert before.preprocessor.means == after.preprocessor.means
    assert before.preprocessor.scales == after.preprocessor.scales
    for a, b in zip(before.model.parameters(), after.model.parameters(), strict=True):
        assert torch.equal(a, b)


def test_mlp_rejects_leaking_or_repeated_inner_folds(tmp_path):
    data = load_windows(*invented_windows(tmp_path))
    folds = grouped_folds(data)
    leaking = replace(folds[0], fit_indices=(*folds[0].fit_indices, data.test_indices[0]))
    for invalid in ((), (folds[0],), (folds[0], folds[0]), (leaking, folds[1])):
        with pytest.raises(ValueError):
            train_mlp(data, invalid, options(), 7)


@pytest.mark.parametrize(
    "field,value",
    [
        ("hidden", []),
        ("hidden", [True]),
        ("hidden", [257]),
        ("epochs", [3, 1]),
        ("epochs", [1, 1]),
        ("epochs", [0]),
        ("epochs", [1001]),
        ("learning_rate", float("nan")),
        ("learning_rate", 0),
        ("weight_decay", -1),
        ("kind", "unknown"),
        ("unexpected", 1),
    ],
)
def test_mlp_configuration_refuses_ambiguous_or_nonfinite_options(field, value):
    configuration = copy.deepcopy(options())
    configuration[field] = value
    with pytest.raises(ValueError):
        read_options(configuration)


def test_mlp_bad_seed_refuses_before_training(tmp_path):
    data = load_windows(*invented_windows(tmp_path))
    for seed in (True, -1, 2**63):
        with pytest.raises(ValueError, match="seed"):
            train_mlp(data, grouped_folds(data), options(), seed)
