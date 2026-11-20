import os

import pytest
import yaml

from corepulse_ml import experiment
from corepulse_ml.experiment import run_experiment

from .test_mlp import options
from .test_reporting import config_file


@pytest.mark.parametrize(
    "mode", ["input", "hardlink", "directory", "artifact-collision", "export-file"]
)
def test_output_aliases_and_conflicts_refuse_before_training_or_writing(
    tmp_path, monkeypatch, mode
):
    path, config = config_file(tmp_path)
    windows = tmp_path / "windows.csv"
    original = windows.read_bytes()
    config["methods"] = [options()]
    output = tmp_path / "report.json"
    artifacts = tmp_path / "coefficients"
    if mode == "input":
        config["output"] = str(windows)
    elif mode == "hardlink":
        os.link(windows, output)
    elif mode == "directory":
        output.mkdir()
    elif mode == "artifact-collision":
        config["output"] = str(artifacts / "mlp_seed_7.json")
    elif mode == "export-file":
        artifacts.write_text("preserve this file")
    path.write_text(yaml.safe_dump(config))

    def no_training(*args, **kwargs):
        raise AssertionError("training started before output preflight")

    monkeypatch.setattr(experiment, "train_mlp", no_training)
    with pytest.raises(ValueError):
        run_experiment(path, export_dir=artifacts)
    assert windows.read_bytes() == original
    if mode == "export-file":
        assert artifacts.read_text() == "preserve this file"
    else:
        assert not artifacts.exists()
