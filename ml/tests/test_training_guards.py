import pytest
import torch
import yaml
from torch import nn

from corepulse_ml import experiment
from corepulse_ml.experiment import run_experiment
from corepulse_ml.training import optimize_step

from .test_mlp import options
from .test_reporting import config_file


@pytest.mark.parametrize("mode", ["loss", "gradient", "parameter"])
def test_nonfinite_training_stops_at_the_relevant_guard(mode):
    model = nn.Linear(1, 1, bias=False, device="cpu", dtype=torch.float32)
    with torch.no_grad():
        model.weight.fill_(0 if mode == "gradient" else 1)
    before = model.weight.detach().clone()
    optimizer = torch.optim.SGD(model.parameters(), lr=3e38 if mode == "parameter" else 0.1)
    if mode == "loss":
        loss = model.weight.sum() * float("nan")
    elif mode == "gradient":
        loss = torch.sqrt(model.weight).sum()  # finite zero loss, infinite derivative
    else:
        loss = torch.square(model.weight).sum()  # finite loss/gradient, overflowing update
    with pytest.raises(ValueError, match=mode):
        optimize_step(model, optimizer, loss)
    if mode != "parameter":
        assert torch.equal(before, model.weight), "Invalid loss/gradient must not update parameters"


def test_later_seed_training_failure_preserves_previous_report_and_every_artifact(
    tmp_path, monkeypatch
):
    path, config = config_file(tmp_path)
    config["methods"] = [options()]
    path.write_text(yaml.safe_dump(config))
    directory = tmp_path / "temporary-models"
    run_experiment(path, export_dir=directory)
    paths = [tmp_path / "report.json", *sorted(directory.glob("*.json"))]
    before = {p: p.read_bytes() for p in paths}
    actual_training = experiment.train_mlp

    def fail_later(dataset, folds, method, seed):
        if seed == 23:
            raise ValueError("injected later-seed training failure")
        return actual_training(dataset, folds, method, seed)

    monkeypatch.setattr(experiment, "train_mlp", fail_later)
    with pytest.raises(ValueError, match="later-seed"):
        run_experiment(path, export_dir=directory)
    assert {p: p.read_bytes() for p in paths} == before
    assert sorted(directory.glob("*.json")) == paths[1:]
