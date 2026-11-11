import copy
import hashlib
import json
from dataclasses import replace
from pathlib import Path

import pytest
import torch
from torch import nn

from corepulse_ml.data import load_windows, timestamp_ns
from corepulse_ml.export import (
    artifact_for,
    ensure_artifact_destination,
    validate_artifact,
    write_artifact,
)

from .artifact_helpers import hand_method
from .helpers import read_csv, rehash, write_csv
from .test_time_precision import shift_run


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


def test_export_requires_the_verified_scenario_labels_and_full_mlp_fit_identities(tmp_path):
    dataset, method = hand_method(tmp_path)
    incomplete_fit = replace(
        method,
        preprocessor=replace(method.preprocessor, fit_run_ids=method.preprocessor.fit_run_ids[:-1]),
    )
    with pytest.raises(ValueError, match="all outer training"):
        artifact_for(incomplete_fit, dataset, synthetic=True)
    # A two-class dense network is a valid standalone shape, but cannot silently
    # drop a scenario from this verified three-class cohort.
    model = copy.deepcopy(method.model)
    model[-1] = nn.Linear(2, 2, device="cpu", dtype=torch.float32)
    with torch.no_grad():
        model[-1].weight.zero_()
        model[-1].bias.zero_()
    missing_class = replace(method, model=model, labels=("normal", "pod_crash:amf"))
    with pytest.raises(ValueError, match="labels differ"):
        artifact_for(missing_class, dataset, synthetic=True)


@pytest.mark.parametrize("role", ["fit", "calibration"])
def test_autoencoder_export_rejects_fault_run_ids_in_normal_only_provenance(tmp_path, role):
    dataset, method = hand_method(tmp_path, "autoencoder")
    fault_run = next(row.run_id for row in dataset.partition("train") if row.scenario != "normal")
    if role == "fit":
        method = replace(
            method, preprocessor=replace(method.preprocessor, fit_run_ids=(fault_run,))
        )
    else:
        method = replace(method, training={"normal_validation_run_ids": [fault_run]})
    with pytest.raises(ValueError, match="normal outer training"):
        artifact_for(method, dataset, synthetic=True)


@pytest.mark.parametrize("mode", ["session", "embargo"])
def test_autoencoder_export_rechecks_source_groups_and_inner_temporal_separation(tmp_path, mode):
    dataset, method = hand_method(tmp_path, "autoencoder")
    windows, split = tmp_path / "windows.csv", tmp_path / "split.json"
    rows = read_csv(windows)
    manifest = json.loads(split.read_text())
    fit_run = method.preprocessor.fit_run_ids[0]
    calibration_run = method.training["normal_validation_run_ids"][0]
    if mode == "session":
        session = manifest["session_assignments"][calibration_run]
        manifest["session_assignments"][fit_run] = session
        column = rows[0].index("session_id")
        for row in rows[1:]:
            if row[0] == fit_run:
                row[column] = session
    else:
        calibration_start = min(
            timestamp_ns(row[3]) for row in rows[1:] if row[0] == calibration_run
        )
        # A 60s fit run ends only 60s before calibration, inside the fixed 120s margin.
        shift_run(rows, fit_run, calibration_start - 120_000_000_000)
    write_csv(windows, rows)
    split.write_text(json.dumps(manifest))
    rehash(windows, split)
    changed = load_windows(windows, split, features=dataset.features)
    method = replace(
        method,
        split_sha256=changed.split_sha256,
        preprocessor=replace(method.preprocessor, windows_sha256=changed.windows_sha256),
    )
    with pytest.raises(ValueError, match="whole-session|embargo"):
        artifact_for(method, changed, synthetic=True)
