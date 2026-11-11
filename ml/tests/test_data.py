import json
from dataclasses import FrozenInstanceError

import numpy as np
import pytest

from corepulse_ml.data import fit_preprocessor, load_windows, timestamp_ns

from .helpers import invented_windows, read_csv, rehash, write_csv


@pytest.mark.parametrize("schema", [1, 2])
def test_legacy_and_session_membership_is_exact_and_immutable(tmp_path, schema):
    windows, split = invented_windows(tmp_path, schema)
    data = load_windows(windows, split)
    assert len(data.rows) == 18
    assert len(data.train_indices) == 12
    assert len(data.test_indices) == 6
    assert set(data.train_indices).isdisjoint(data.test_indices)
    changed = data.manifest
    changed["train_runs"].clear()
    assert len(data.manifest["train_runs"]) == 6
    with pytest.raises(FrozenInstanceError):
        data.rows[0].run_id = "changed"


def test_window_loader_rejects_a_sequence_manifest(tmp_path):
    windows, split = invented_windows(tmp_path)
    manifest = json.loads(split.read_text())
    manifest["input_kind"] = "sequences_v1"
    split.write_text(json.dumps(manifest))
    with pytest.raises(ValueError, match="sequence"):
        load_windows(windows, split)


def test_hash_mismatch_refuses_before_processing(tmp_path):
    windows, split = invented_windows(tmp_path)
    windows.write_bytes(windows.read_bytes() + b"\n")
    with pytest.raises(ValueError, match="hash mismatch"):
        load_windows(windows, split)


def test_missing_column_is_not_filled_or_inferred(tmp_path):
    windows, split = invented_windows(tmp_path)
    rows = read_csv(windows)
    write_csv(windows, [row[:-1] for row in rows])
    rehash(windows, split)
    with pytest.raises(ValueError, match="missing columns"):
        load_windows(windows, split)


@pytest.mark.parametrize("role", ["train", "test"])
def test_empty_partition_refuses(tmp_path, role):
    windows, split = invented_windows(tmp_path)
    manifest = json.loads(split.read_text())
    manifest[role + "_runs"] = []
    split.write_text(json.dumps(manifest))
    with pytest.raises(ValueError, match="empty.*partition"):
        load_windows(windows, split)


@pytest.mark.parametrize(
    "mode", ["unknown", "overlap", "missing", "class-count", "session", "embargo", "legacy"]
)
def test_forged_split_metadata_refuses(tmp_path, mode):
    windows, split = invented_windows(tmp_path)
    manifest = json.loads(split.read_text())
    if mode == "unknown":
        manifest["train_runs"].append("unknown-run")
    elif mode == "overlap":
        manifest["test_runs"].append(manifest["train_runs"][0])
    elif mode == "missing":
        manifest["train_runs"].pop()
    elif mode == "class-count":
        manifest["classes"]["normal"]["train_runs"] = 3
    elif mode == "session":
        manifest["session_assignments"][manifest["train_runs"][0]] = "invented-session-1"
    elif mode == "embargo":
        manifest["embargo_seconds"] = 86400
    else:
        manifest["require_protocol"] = False
    split.write_text(json.dumps(manifest))
    with pytest.raises(ValueError):
        load_windows(windows, split)


@pytest.mark.parametrize(
    "feature",
    [
        "label",
        "target",
        "run_id",
        "session_id",
        "sample_count",
        "window_start",
        "protocol_start_verified",
        "smf_pfcp_failures_mean",
        "probe_received_mean",
        "unknown",
    ],
)
def test_metadata_and_post_run_inputs_cannot_enter_the_model(tmp_path, feature):
    windows, split = invented_windows(tmp_path)
    with pytest.raises(ValueError, match="metadata or unsupported"):
        load_windows(windows, split, features=[feature])


@pytest.mark.parametrize(
    "name,value",
    [
        ("cpu_millicores_mean", "NaN"),
        ("cpu_millicores_mean", "-1"),
        ("cpu_millicores_missing_ratio", "1.1"),
        ("protocol_start_verified", "false"),
    ],
)
def test_invalid_observations_refuse(tmp_path, name, value):
    windows, split = invented_windows(tmp_path)
    rows = read_csv(windows)
    rows[1][rows[0].index(name)] = value
    write_csv(windows, rows)
    rehash(windows, split)
    with pytest.raises(ValueError):
        load_windows(windows, split)


def test_imputation_scaling_and_feature_dropping_never_learn_from_test_rows(tmp_path):
    windows, split = invented_windows(tmp_path)
    features = ["cpu_millicores_mean", "pod_count_mean"]
    original = load_windows(windows, split, features)
    transform = fit_preprocessor(original)
    assert transform.medians == (1.5, 1.5)
    assert transform.means == (1.5, 1.5)
    assert transform.scales == (0.5, 0.5)
    records = read_csv(windows)
    for index in original.test_indices:
        records[index + 1][records[0].index("cpu_millicores_mean")] = "1000000"
        records[index + 1][records[0].index("pod_count_mean")] = ""
    write_csv(windows, records)
    rehash(windows, split)
    changed = load_windows(windows, split, features)
    fitted = fit_preprocessor(changed)
    assert fitted.medians == transform.medians
    assert fitted.means == transform.means
    assert fitted.scales == transform.scales
    assert fitted.fit_run_ids == transform.fit_run_ids
    x = fitted.transform(changed.partition("test"))
    assert np.isfinite(x).all()
    assert np.all(x[:, 1] == 0)  # Missing test values use the training median.
    with pytest.raises(ValueError, match="outer training"):
        fit_preprocessor(changed, changed.test_indices)


def test_observed_zero_and_unknown_stay_distinct_and_all_missing_train_features_drop(tmp_path):
    windows, split = invented_windows(tmp_path)
    records = read_csv(windows)
    for row in records[1:13]:
        for index, name in enumerate(records[0]):
            if name.startswith("cpu_millicores_"):
                row[index] = "1" if name.endswith("_missing_ratio") else ""
            if name.startswith("pod_count_"):
                row[index] = "0"
    write_csv(windows, records)
    rehash(windows, split)
    data = load_windows(windows, split, ["cpu_millicores_mean", "pod_count_mean"])
    fitted = fit_preprocessor(data)
    assert fitted.dropped_features == ("cpu_millicores_mean",)
    assert fitted.feature_names == ("pod_count_mean",)
    assert fitted.medians == (0.0,)
    assert fitted.scales == (1.0,)
    assert fitted.transform(data.partition("train")).shape == (12, 1)
    assert (fitted.transform(data.partition("train")) == 0).all()


def test_nanosecond_identity_is_not_truncated():
    first = timestamp_ns("2026-01-01T00:00:00.000000001Z")
    second = timestamp_ns("2026-01-01T00:00:00.000000002Z")
    assert second - first == 1
    assert first == timestamp_ns("2026-01-01T01:00:00.000000001+01:00")


def test_duplicate_json_fields_are_not_silently_overridden(tmp_path):
    windows, split = invented_windows(tmp_path)
    split.write_text(split.read_text().replace('"seed": 42', '"seed": 42, "seed": 7'))
    with pytest.raises(ValueError, match="duplicate JSON key"):
        load_windows(windows, split)
