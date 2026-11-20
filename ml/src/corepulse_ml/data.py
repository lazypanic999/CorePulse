"""Strict, hash-bound offline window loading with training-only preprocessing."""

from __future__ import annotations

import calendar
import csv
import hashlib
import io
import json
import math
import re
from collections import Counter
from collections.abc import Iterable
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

import numpy as np

SIGNALS = (
    "pod_count",
    "cpu_millicores",
    "memory_bytes",
    "amf_cpu_millicores",
    "amf_memory_bytes",
    "upf_cpu_millicores",
    "upf_memory_bytes",
    "probe_sent",
    "probe_received",
    "smf_pfcp_failures",
    "gnb_n2_terminations",
    "ue_registrations",
    "ue_pdu_sessions",
)
NUMERIC_COLUMNS = tuple(
    f"{name}_{suffix}" for name in SIGNALS for suffix in ("mean", "max", "delta", "missing_ratio")
)
METADATA_COLUMNS = (
    "run_id",
    "label",
    "target",
    "window_start",
    "window_end",
    "first_sample_at",
    "last_sample_at",
    "sample_count",
    "observed_span_seconds",
    "partial",
    "protocol_start_verified",
)
# Identical to the Go references: no post-run protocol availability, identity or truth inputs.
RESOURCE_FEATURES = (
    "pod_count_mean",
    "pod_count_delta",
    "cpu_millicores_mean",
    "cpu_millicores_max",
    "cpu_millicores_delta",
    "memory_bytes_mean",
    "memory_bytes_delta",
    "amf_cpu_millicores_mean",
    "amf_cpu_millicores_missing_ratio",
    "upf_cpu_millicores_mean",
    "upf_cpu_millicores_missing_ratio",
)
_NUMBER = re.compile(r"[+-]?(?:[0-9]+\.?[0-9]*|\.[0-9]+)(?:[eE][+-]?[0-9]+)?\Z")
_SESSION = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}\Z")
_TIME = re.compile(
    r"([0-9]{4}-[0-9]{2}-[0-9]{2})T([0-9]{2}:[0-9]{2}:[0-9]{2})"
    r"(?:\.([0-9]{1,9}))?(Z|[+-][0-9]{2}:[0-9]{2})\Z"
)


def timestamp_ns(value: str) -> int:
    """Parse RFC3339 without datetime's truncation of Go's nanosecond identities."""
    match = _TIME.fullmatch(value)
    if not match:
        raise ValueError(f"invalid RFC3339 timestamp: {value!r}")
    day, clock, fraction, zone = match.groups()
    try:
        dt = datetime.fromisoformat(f"{day}T{clock}{'+00:00' if zone == 'Z' else zone}")
        seconds = calendar.timegm(dt.utctimetuple())
    except (ValueError, OverflowError) as exc:
        raise ValueError(f"out-of-range RFC3339 timestamp: {value!r}") from exc
    return seconds * 1_000_000_000 + int((fraction or "").ljust(9, "0"))


def strict_json(raw: bytes | str) -> dict:
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError(f"duplicate JSON key: {key}")
            result[key] = value
        return result

    def constant(value):
        raise ValueError(f"non-finite JSON constant: {value}")

    value = json.loads(raw, object_pairs_hook=unique, parse_constant=constant)
    if not isinstance(value, dict):
        raise ValueError("JSON root must be an object")
    return value


def validate_features(features: Iterable[str]) -> tuple[str, ...]:
    try:
        result = tuple(features)
    except TypeError as exc:
        raise ValueError("features must be an iterable of names") from exc
    if (
        not result
        or any(not isinstance(name, str) for name in result)
        or len(set(result)) != len(result)
    ):
        raise ValueError("feature selection must be nonempty and unique")
    if any(name not in RESOURCE_FEATURES for name in result):
        raise ValueError("metadata or unsupported feature requested; use resource allowlist only")
    return result


def _number(value: str, name: str) -> float:
    if not _NUMBER.fullmatch(value):
        raise ValueError(f"invalid numeric {name}")
    result = float(value)
    if not math.isfinite(result):
        raise ValueError(f"non-finite {name}")
    return result


def _ids(value, name: str) -> tuple[str, ...]:
    if value is None:
        value = []
    if not isinstance(value, list) or any(not isinstance(x, str) or not x.strip() for x in value):
        raise ValueError(f"invalid {name} identities")
    if len(set(value)) != len(value):
        raise ValueError(f"duplicate {name} identity")
    return tuple(value)


@dataclass(frozen=True)
class WindowRow:
    run_id: str
    scenario: str
    session_id: str
    window_start: str
    start_ns: int
    end_ns: int
    values: tuple[float | None, ...]


@dataclass(frozen=True)
class WindowDataset:
    rows: tuple[WindowRow, ...]
    train_indices: tuple[int, ...]
    test_indices: tuple[int, ...]
    purged_indices: tuple[int, ...]
    windows_sha256: str
    split_sha256: str
    _manifest_json: str
    features: tuple[str, ...]
    schema_version: int

    @property
    def manifest(self) -> dict:
        # Return a fresh object; callers cannot mutate validated membership in-place.
        return json.loads(self._manifest_json)

    def partition(self, role: str) -> tuple[WindowRow, ...]:
        if role not in ("train", "test", "purged"):
            raise ValueError("unknown partition")
        return tuple(self.rows[i] for i in getattr(self, role + "_indices"))


def _read_rows(raw: bytes) -> tuple[WindowRow, ...]:
    records = list(csv.reader(io.StringIO(raw.decode("utf-8"), newline=""), strict=True))
    if not records:
        raise ValueError("missing window CSV header")
    header = tuple(records[0])
    legacy = METADATA_COLUMNS + NUMERIC_COLUMNS
    session = METADATA_COLUMNS + ("session_id",) + NUMERIC_COLUMNS
    if header not in (legacy, session):
        raise ValueError("missing columns or unsupported window CSV header")
    offset = len(METADATA_COLUMNS) + (header == session)
    result, seen, declarations = [], set(), {}
    for record in records[1:]:
        if len(record) != len(header):
            raise ValueError("incorrect window CSV field count")
        run, label, target = record[:3]
        scenario = "normal" if label == "normal" and not target else f"{label}:{target}"
        if not run.strip() or scenario not in ("normal", "pod_crash:amf", "pod_crash:upf"):
            raise ValueError("missing run identity or unsupported scenario")
        start, end, first, last = map(timestamp_ns, record[3:7])
        if not start < end or not start <= first <= last < end:
            raise ValueError("inconsistent window time bounds")
        if (run, start) in seen:
            raise ValueError("duplicate window identity")
        seen.add((run, start))
        if not re.fullmatch(r"[+]?[0-9]+", record[7]) or int(record[7]) < 1:
            raise ValueError("invalid sample count")
        if _number(record[8], "observed span") < 0:
            raise ValueError("negative observed span")
        if record[9] not in ("true", "false") or record[10] != "true":
            raise ValueError("invalid partial flag or unverified protocol start")
        session_id = record[11] if header == session else ""
        if session_id and not _SESSION.fullmatch(session_id):
            raise ValueError("invalid session declaration")
        declaration = (scenario, session_id)
        if run in declarations and declarations[run] != declaration:
            raise ValueError("conflicting run labels/session declarations")
        declarations[run] = declaration
        values = []
        for name, cell in zip(NUMERIC_COLUMNS, record[offset:], strict=True):
            if cell == "" and not name.endswith("_missing_ratio"):
                values.append(None)
                continue
            number = _number(cell, name)
            if number < 0 and not name.endswith("_delta"):
                raise ValueError(f"negative {name}")
            if name.endswith("_missing_ratio") and not 0 <= number <= 1:
                raise ValueError("invalid missing ratio")
            values.append(number)
        result.append(WindowRow(run, scenario, session_id, record[3], start, end, tuple(values)))
    return tuple(result)


def run_ranges(rows: Iterable[WindowRow]) -> dict[str, tuple[int, int]]:
    result = {}
    for row in rows:
        start, end = result.get(row.run_id, (row.start_ns, row.end_ns))
        result[row.run_id] = min(start, row.start_ns), max(end, row.end_ns)
    return result


def near(a: tuple[int, int], b: tuple[int, int], embargo_ns: int) -> bool:
    return a[0] < b[1] + embargo_ns and a[1] > b[0] - embargo_ns


def _grouping(manifest: dict, rows: tuple[WindowRow, ...], roles: dict[str, str]) -> None:
    if manifest.get("input_kind", "") != "":
        raise ValueError("window loader cannot consume a sequence split")
    schema = manifest.get("schema_version")
    if type(schema) is not int or schema not in (1, 2):
        raise ValueError("unsupported split schema")
    session_roles = {}
    for role in ("train", "test", "purged"):
        for session in _ids(manifest.get(role + "_sessions"), role + " sessions"):
            if not _SESSION.fullmatch(session) or session in session_roles:
                raise ValueError("duplicate/overlapping session groups")
            session_roles[session] = role
    assignments = manifest.get("session_assignments")
    if assignments is None:
        assignments = {}
    if not isinstance(assignments, dict):
        raise ValueError("invalid session assignments")
    if schema == 1:
        if manifest.get("grouping", "") not in ("", "run") or session_roles or assignments:
            raise ValueError("run schema cannot claim session grouping")
        return
    if manifest.get("grouping") != "session" or set(assignments) != set(roles):
        raise ValueError("session assignment coverage mismatch")
    if not all(role in session_roles.values() for role in ("train", "test")):
        raise ValueError("empty session partition")
    observed = set()
    for row in rows:
        if not row.session_id or assignments.get(row.run_id) != row.session_id:
            raise ValueError("session assignment differs from CSV provenance")
        if session_roles.get(row.session_id) != roles[row.run_id]:
            raise ValueError("session crosses split roles")
        observed.add(row.session_id)
    if observed != set(session_roles):
        raise ValueError("unknown session group")


def load_windows(
    windows: str | Path, split: str | Path, features: Iterable[str] = RESOURCE_FEATURES
) -> WindowDataset:
    selected = validate_features(features)
    raw, split_raw = Path(windows).read_bytes(), Path(split).read_bytes()
    digest = hashlib.sha256(raw).hexdigest()
    manifest = strict_json(split_raw)
    if manifest.get("windows_sha256") != digest:
        raise ValueError("CSV hash mismatch")
    if manifest.get("feature_names") != list(NUMERIC_COLUMNS):
        raise ValueError("split feature allowlist mismatch")
    if manifest.get("require_protocol") is not True:
        raise ValueError("neural experiments require strict protocol provenance")
    seed = manifest.get("seed")
    if type(seed) is not int or not -(2**63) <= seed < 2**63:
        raise ValueError("invalid split seed")
    for key, low, high, inclusive in (
        ("test_fraction", 0, 1, False),
        ("embargo_seconds", 0, 86400, True),
    ):
        value = manifest.get(key, 0 if key == "embargo_seconds" else None)
        if (
            type(value) not in (int, float)
            or not math.isfinite(value)
            or not (low <= value <= high if inclusive else low < value < high)
        ):
            raise ValueError(f"invalid split {key}")
    roles = {}
    for role in ("train", "test", "purged"):
        ids = _ids(manifest.get(role + "_runs"), role)
        if role != "purged" and not ids:
            raise ValueError("empty train/test partition")
        for run in ids:
            if run in roles:
                raise ValueError("duplicate/overlapping split membership")
            roles[run] = role
    rows = _read_rows(raw)
    if {row.run_id for row in rows} != set(roles):
        raise ValueError("split membership differs from CSV runs")
    _grouping(manifest, rows, roles)
    counts = {role: Counter() for role in ("train", "test")}
    seen = set()
    for row in rows:
        if row.run_id not in seen and roles[row.run_id] != "purged":
            counts[roles[row.run_id]][row.scenario] += 1
        seen.add(row.run_id)
    labels = set(counts["train"]) | set(counts["test"])
    classes = manifest.get("classes")
    if (
        not isinstance(classes, dict)
        or set(classes) != labels
        or "normal" not in labels
        or len(labels) < 2
    ):
        raise ValueError("class membership mismatch or missing normal support")
    for label in labels:
        expected = {"train_runs": counts["train"][label], "test_runs": counts["test"][label]}
        actual = classes[label]
        if (
            not isinstance(actual, dict)
            or actual != expected
            or any(type(actual.get(k)) is not int or actual[k] < 1 for k in expected)
        ):
            raise ValueError("missing class partition support or incorrect class counts")
    margin = int(manifest.get("embargo_seconds", 0) * 1_000_000_000)
    ranges = run_ranges(rows)
    if margin:
        for train, role in roles.items():
            if role == "train" and any(
                near(ranges[train], ranges[test], margin) for test in manifest["test_runs"]
            ):
                raise ValueError("retained training run violates capture-time embargo")
    indices = {
        role: tuple(i for i, row in enumerate(rows) if roles[row.run_id] == role)
        for role in ("train", "test", "purged")
    }
    return WindowDataset(
        rows,
        indices["train"],
        indices["test"],
        indices["purged"],
        digest,
        hashlib.sha256(split_raw).hexdigest(),
        json.dumps(manifest, sort_keys=True),
        selected,
        manifest["schema_version"],
    )


@dataclass(frozen=True)
class Preprocessor:
    feature_names: tuple[str, ...]
    medians: tuple[float, ...]
    means: tuple[float, ...]
    scales: tuple[float, ...]
    dropped_features: tuple[str, ...]
    fit_run_ids: tuple[str, ...]
    windows_sha256: str

    def transform(self, rows: Iterable[WindowRow]) -> np.ndarray:
        rows = tuple(rows)
        if not rows:
            raise ValueError("cannot transform an empty partition")
        positions = [NUMERIC_COLUMNS.index(name) for name in self.feature_names]
        matrix = np.array(
            [
                [np.nan if row.values[i] is None else row.values[i] for i in positions]
                for row in rows
            ],
            dtype=np.float64,
        )
        with np.errstate(over="raise", invalid="raise", divide="raise"):
            try:
                matrix = (
                    np.where(np.isnan(matrix), self.medians, matrix) - self.means
                ) / self.scales
                result = matrix.astype(np.float32)
            except FloatingPointError as exc:
                raise ValueError("preprocessing numeric overflow") from exc
        if not np.isfinite(result).all():
            raise ValueError("non-finite preprocessed value")
        return result


def fit_preprocessor(dataset: WindowDataset, indices: Iterable[int] | None = None) -> Preprocessor:
    selected = tuple(dataset.train_indices if indices is None else indices)
    if (
        not selected
        or len(set(selected)) != len(selected)
        or any(type(i) is not int or i not in dataset.train_indices for i in selected)
    ):
        raise ValueError("preprocessing fit must use a nonempty subset of outer training rows")
    positions = [NUMERIC_COLUMNS.index(name) for name in dataset.features]
    matrix = np.array(
        [
            [
                np.nan if dataset.rows[i].values[j] is None else dataset.rows[i].values[j]
                for j in positions
            ]
            for i in selected
        ],
        dtype=np.float64,
    )
    keep = ~np.isnan(matrix).all(axis=0)
    features = tuple(name for name, retain in zip(dataset.features, keep, strict=True) if retain)
    dropped = tuple(name for name, retain in zip(dataset.features, keep, strict=True) if not retain)
    if not any(not name.endswith("_missing_ratio") for name in features):
        raise ValueError("no observed training resource features")
    with np.errstate(over="raise", invalid="raise", divide="raise"):
        try:
            matrix = matrix[:, keep]
            medians = np.nanmedian(matrix, axis=0)
            matrix = np.where(np.isnan(matrix), medians, matrix)
            means, scales = matrix.mean(axis=0), matrix.std(axis=0, ddof=0)
            scales = np.where(scales == 0, 1.0, scales)
        except FloatingPointError as exc:
            raise ValueError("training numeric overflow") from exc
    if not all(np.isfinite(x).all() for x in (medians, means, scales)):
        raise ValueError("non-finite training parameters")
    return Preprocessor(
        features,
        tuple(map(float, medians)),
        tuple(map(float, means)),
        tuple(map(float, scales)),
        dropped,
        tuple(sorted({dataset.rows[i].run_id for i in selected})),
        dataset.windows_sha256,
    )
