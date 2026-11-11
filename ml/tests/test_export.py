import copy
import hashlib
import json
from dataclasses import replace
from pathlib import Path

import pytest
import torch
from torch import nn

from corepulse_ml.export import (
    artifact_for,
    ensure_artifact_destination,
    validate_artifact,
    write_artifact,
)

from .artifact_helpers import hand_method


@pytest.mark.parametrize("kind", ["mlp", "autoencoder"])
def test_hand_authored_coefficients_round_trip_as_explicit_finite_json(tmp_path, kind):
    dataset, method = hand_method(tmp_path / "inputs", kind)
    artifact = artifact_for(method, dataset, synthetic=True)
    path = tmp_path / "local-output" / "coefficients.json"
    digest = write_artifact(path, artifact)
    assert hashlib.sha256(path.read_bytes()).hexdigest() == digest
    assert json.loads(path.read_text()) == artifact
    assert artifact["source"]["split_sha256"] == dataset.split_sha256
    assert artifact["preprocessing"]["medians"] == list(method.preprocessor.medians)
    assert artifact["layers"][0]["weights"] == method.model[0].weight.detach().tolist()
    assert artifact["synthetic"] is True


@pytest.mark.parametrize(
    "mode",
    [
        "version",
        "feature",
        "zero-scale",
        "nan",
        "float32-overflow",
        "dimension",
        "operator",
        "unknown",
        "labels",
        "source",
        "seed",
        "missing-output",
    ],
)
def test_artifact_refuses_unsupported_or_inconsistent_contracts(tmp_path, mode):
    dataset, method = hand_method(tmp_path)
    artifact = artifact_for(method, dataset, synthetic=True)
    if mode == "version":
        artifact["schema_version"] = 2
    elif mode == "feature":
        artifact["preprocessing"]["feature_names"][0] = "run_id"
    elif mode == "zero-scale":
        artifact["preprocessing"]["scales"][0] = 0
    elif mode == "nan":
        artifact["layers"][0]["weights"][0][0] = float("nan")
    elif mode == "float32-overflow":
        artifact["layers"][0]["bias"][0] = 1e100
    elif mode == "dimension":
        artifact["layers"][0]["weights"][0].pop()
    elif mode == "operator":
        artifact["layers"][1]["op"] = "sigmoid"
    elif mode == "unknown":
        artifact["code"] = "not executable"
    elif mode == "labels":
        artifact["labels"].reverse()
    elif mode == "source":
        artifact["source"]["windows_sha256"] = "bad"
    elif mode == "seed":
        artifact["seed"] = True
    else:
        artifact["layers"].pop()
    with pytest.raises(ValueError):
        validate_artifact(artifact)


def test_export_binds_source_and_rejects_unsupported_torch_modules(tmp_path):
    dataset, method = hand_method(tmp_path)
    for changed in (
        replace(method, split_sha256="0" * 64),
        replace(method, preprocessor=replace(method.preprocessor, windows_sha256="0" * 64)),
        replace(method, model=nn.Sequential(nn.Sigmoid())),
        replace(method, model=copy.deepcopy(method.model).to(torch.float64)),
    ):
        with pytest.raises(ValueError):
            artifact_for(changed, dataset, synthetic=True)


def test_autoencoder_threshold_and_calibration_identities_remain_explicit(tmp_path):
    dataset, method = hand_method(tmp_path, "autoencoder")
    artifact = artifact_for(method, dataset, synthetic=True)
    assert artifact["threshold"] == 0.25
    for mode in ("overlap", "threshold"):
        bad = copy.deepcopy(artifact)
        if mode == "overlap":
            bad["calibration_run_ids"] = bad["preprocessing"]["fit_run_ids"]
        else:
            bad["threshold"] = -1
        with pytest.raises(ValueError):
            validate_artifact(bad)


def test_coefficients_cannot_be_written_into_tracked_source_or_overwrite_inputs(tmp_path):
    root = Path(__file__).resolve().parents[2]
    with pytest.raises(ValueError, match="ignored"):
        ensure_artifact_destination(root / "ml" / "tracked-coefficients.json")
    dataset, method = hand_method(tmp_path / "inputs")
    artifact = artifact_for(method, dataset, synthetic=True)
    path = tmp_path / "previous.json"
    write_artifact(path, artifact)
    before = path.read_bytes()
    bad = copy.deepcopy(artifact)
    bad["preprocessing"]["scales"][0] = 0
    with pytest.raises(ValueError):
        write_artifact(path, bad)
    with pytest.raises(ValueError, match="overwrite"):
        write_artifact(path, artifact, sources=[path])
    assert path.read_bytes() == before
