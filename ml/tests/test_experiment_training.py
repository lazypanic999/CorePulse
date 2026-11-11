import json

import pytest
import yaml

from corepulse_ml import experiment
from corepulse_ml.experiment import read_experiment, run_experiment
from corepulse_ml.reporting import REFERENCES

from .test_mlp import options
from .test_reporting import config_file


def test_yaml_training_reports_all_seeds_and_keeps_references(tmp_path):
    path, config = config_file(tmp_path)
    config["methods"] = [options()]
    path.write_text(yaml.safe_dump(config))
    first = run_experiment(path)
    assert first == run_experiment(path)
    assert set(first["models"]) == set(REFERENCES) | {"mlp_seed_7", "mlp_seed_23"}
    assert first["synthetic"] is True
    assert first["seed_spread"]["mlp"]["seeds"] == [7, 23]
    for seed in config["seeds"]:
        method = first["models"][f"mlp_seed_{seed}"]
        assert len(method["predictions"]) == first["test_windows"]
        assert method["seed"] == seed
        assert method["training"]["selected_epochs"] in options()["epochs"]
    reference = json.loads((tmp_path / "refs.json").read_text())
    for name in REFERENCES:
        assert first["models"][name] == reference["models"][name]


def test_bad_references_refuse_before_training_or_replacing_report(tmp_path, monkeypatch):
    path, config = config_file(tmp_path)
    run_experiment(path)
    output = tmp_path / "report.json"
    before = output.read_bytes()
    config["methods"] = [options()]
    path.write_text(yaml.safe_dump(config))
    reference_path = tmp_path / "refs.json"
    reference = json.loads(reference_path.read_text())
    del reference["models"]["probe_rule"]
    reference_path.write_text(json.dumps(reference))

    def forbidden(*args, **kwargs):
        raise AssertionError("training started before reference validation")

    monkeypatch.setattr(experiment, "train_mlp", forbidden)
    with pytest.raises(ValueError, match="four reference"):
        run_experiment(path)
    assert output.read_bytes() == before


@pytest.mark.parametrize("methods", [[{"kind": "unknown"}], [options(), options()]])
def test_yaml_rejects_unknown_and_repeated_training_kinds(tmp_path, methods):
    path, config = config_file(tmp_path)
    config["methods"] = methods
    path.write_text(yaml.safe_dump(config))
    with pytest.raises(ValueError, match="method kind"):
        read_experiment(path)
