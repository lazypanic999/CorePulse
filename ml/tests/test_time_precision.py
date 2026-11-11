import json
from datetime import UTC, datetime

import pytest

from corepulse_ml.data import load_windows, timestamp_ns
from corepulse_ml.numeric import duration_ns
from corepulse_ml.validation import grouped_folds

from .helpers import invented_windows, read_csv, rehash, write_csv


def stamp(nanos):
    seconds, fraction = divmod(nanos, 1_000_000_000)
    return datetime.fromtimestamp(seconds, UTC).strftime("%Y-%m-%dT%H:%M:%S") + f".{fraction:09d}Z"


@pytest.mark.parametrize("nanos", [0, 1, 1_000_000_007, 256_238_704_302, 2_103_571_584_039])
def test_go_duration_seconds_recovers_exact_nanoseconds(nanos):
    whole, fraction = divmod(nanos, 1_000_000_000)
    assert duration_ns(whole + fraction / 1_000_000_000) == nanos


@pytest.mark.parametrize("seconds", [1e-10, -1, 86401, True, float("nan")])
def test_nonrepresentable_duration_refuses(seconds):
    with pytest.raises(ValueError):
        duration_ns(seconds)


@pytest.mark.parametrize("zone", ["+01:60", "-01:60", "+00:99", "+24:00"])
def test_invalid_timezone_minutes_are_not_silently_normalized(zone):
    with pytest.raises(ValueError):
        timestamp_ns("2026-01-01T00:00:00.000000001" + zone)


def test_valid_timezone_keeps_the_exact_nanosecond_identity():
    assert timestamp_ns("2026-01-01T03:00:00.000000001+03:00") == timestamp_ns(
        "2026-01-01T00:00:00.000000001Z"
    )


def shift_run(rows, run_id, first_start):
    selected = [row for row in rows[1:] if row[0] == run_id]
    delta = first_start - min(timestamp_ns(row[3]) for row in selected)
    for row in selected:
        for column in range(3, 7):
            row[column] = stamp(timestamp_ns(row[column]) + delta)


def test_outer_embargo_does_not_lose_one_nanosecond_to_float_truncation(tmp_path):
    windows, split = invented_windows(tmp_path)
    rows = read_csv(windows)
    manifest = json.loads(split.read_text())
    manifest["embargo_seconds"] = 1.000000007
    test_start = min(timestamp_ns(row[3]) for row in rows[1:] if row[0] == "invented-2-0")
    # Gap is 1ns SHORTER than the declared embargo, so retaining this run must refuse.
    shift_run(rows, "invented-1-0", test_start - 1_000_000_006 - 60_000_000_000)
    write_csv(windows, rows)
    split.write_text(json.dumps(manifest))
    rehash(windows, split)
    with pytest.raises(ValueError, match="embargo"):
        load_windows(windows, split)


def test_inner_embargo_uses_the_full_nanosecond_margin_and_purges_whole_session(tmp_path):
    windows, split = invented_windows(tmp_path)
    rows = read_csv(windows)
    manifest = json.loads(split.read_text())
    manifest["embargo_seconds"] = 1.000000007
    first_end = max(timestamp_ns(row[4]) for row in rows[1:] if row[0] == "invented-0-0")
    shift_run(rows, "invented-1-0", first_end + 1_000_000_006)
    write_csv(windows, rows)
    split.write_text(json.dumps(manifest))
    rehash(windows, split)
    dataset = load_windows(windows, split)
    with pytest.raises(ValueError, match="empty inner|support"):
        grouped_folds(dataset)
