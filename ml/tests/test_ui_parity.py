import hashlib
import json

import pytest
import yaml

from corepulse_ml.experiment import run_experiment
from corepulse_ml.ui_parity import write_parity

from .test_autoencoder import options as autoencoder_options
from .test_mlp import options as mlp_options
from .test_reporting import config_file


@pytest.fixture
def ui_job(tmp_path):
    path, config = config_file(tmp_path)
    config["output"] = str(tmp_path / "evaluation.json")
    config["methods"] = [mlp_options(), autoencoder_options()]
    path.write_text(yaml.safe_dump(config))
    run_experiment(path, export_dir=tmp_path / "models")
    return tmp_path


def test_frozen_cpu_outputs_match_original_trained_report(ui_job):
    result = write_parity(ui_job)
    assert set(result["models"]) == {
        "mlp_seed_7",
        "mlp_seed_23",
        "autoencoder_seed_7",
        "autoencoder_seed_23",
    }
    report = json.loads((ui_job / "evaluation.json").read_bytes())
    for name, model in result["models"].items():
        assert len(model["cases"]) == report["test_windows"]
        for case, prediction in zip(
            model["cases"], report["models"][name]["predictions"], strict=True
        ):
            assert case["score"] == prediction["score"]
            assert case["predicted"] == prediction["predicted"]


@pytest.mark.parametrize("corruption", ["artifact", "score", "source", "missing_seed"])
def test_corruption_preserves_previous_cpu_reference(ui_job, corruption):
    write_parity(ui_job)
    target = ui_job / "parity.json"
    before = target.read_bytes()
    report_path = ui_job / "evaluation.json"
    report = json.loads(report_path.read_bytes())
    name = "mlp_seed_7"
    artifact_path = ui_job / "models" / (name + ".json")
    if corruption == "artifact":
        artifact_path.write_bytes(artifact_path.read_bytes() + b"\n")
    elif corruption == "score":
        report["models"][name]["predictions"][0]["score"] += 0.1
    elif corruption == "source":
        artifact = json.loads(artifact_path.read_bytes())
        artifact["source"]["windows_sha256"] = "0" * 64
        artifact_path.write_text(json.dumps(artifact))
        report["experiment"]["artifacts"][name]["sha256"] = hashlib.sha256(
            artifact_path.read_bytes()
        ).hexdigest()
    else:
        del report["experiment"]["artifacts"][name]
    report_path.write_text(json.dumps(report))
    with pytest.raises(ValueError):
        write_parity(ui_job)
    assert target.read_bytes() == before
