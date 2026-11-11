import csv
import hashlib
import json
from datetime import UTC, datetime, timedelta

from corepulse_ml.data import METADATA_COLUMNS, NUMERIC_COLUMNS


def write_csv(path, rows):
    with path.open("w", newline="", encoding="utf-8") as stream:
        csv.writer(stream, lineterminator="\n").writerows(rows)


def read_csv(path):
    with path.open(newline="", encoding="utf-8") as stream:
        return list(csv.reader(stream))


def rehash(windows, split):
    manifest = json.loads(split.read_text())
    manifest["windows_sha256"] = hashlib.sha256(windows.read_bytes()).hexdigest()
    split.write_text(json.dumps(manifest, sort_keys=True))


def invented_windows(directory, schema=2, occasions=3):
    """Three invented occasions, each with three scenarios and two windows/run."""
    directory.mkdir(parents=True, exist_ok=True)
    windows, split = directory / "windows.csv", directory / "split.json"
    header = METADATA_COLUMNS + (("session_id",) if schema == 2 else ()) + NUMERIC_COLUMNS
    rows = [header]
    runs, assignments = [], {}
    scenarios = ("normal", "pod_crash:amf", "pod_crash:upf")

    def stamp(value):
        return value.strftime("%Y-%m-%dT%H:%M:%S") + ".000000001Z"

    for occasion in range(occasions):
        for index, scenario in enumerate(scenarios):
            run = f"invented-{occasion}-{index}"
            session = f"invented-session-{occasion}"
            runs.append(run)
            assignments[run] = session
            label, target = ("normal", "") if scenario == "normal" else scenario.split(":")
            for window in range(2):
                start = datetime(2026, 1, 1, tzinfo=UTC) + timedelta(
                    days=occasion,
                    hours=index,
                    seconds=window * 30,
                )
                meta = [
                    run,
                    label,
                    target,
                    stamp(start),
                    stamp(start + timedelta(seconds=30)),
                    stamp(start),
                    stamp(start + timedelta(seconds=5)),
                    "2",
                    "5",
                    "false",
                    "true",
                ]
                if schema == 2:
                    meta.append(session)
                values = [
                    "0" if n.endswith(("_delta", "_missing_ratio")) else str(occasion + 1)
                    for n in NUMERIC_COLUMNS
                ]
                rows.append(meta + values)
    write_csv(windows, rows)
    manifest = {
        "schema_version": schema,
        "windows_sha256": hashlib.sha256(windows.read_bytes()).hexdigest(),
        "seed": 42,
        "test_fraction": 0.25,
        "require_protocol": True,
        "feature_names": list(NUMERIC_COLUMNS),
        "embargo_seconds": 120,
        "purged_runs": [],
        "train_runs": sorted(runs[: (occasions - 1) * 3]),
        "test_runs": sorted(runs[(occasions - 1) * 3 :]),
        "classes": {label: {"train_runs": occasions - 1, "test_runs": 1} for label in scenarios},
    }
    if schema == 2:
        manifest.update(
            grouping="session",
            session_assignments=assignments,
            train_sessions=[f"invented-session-{i}" for i in range(occasions - 1)],
            test_sessions=[f"invented-session-{occasions - 1}"],
        )
    split.write_text(json.dumps(manifest, sort_keys=True))
    return windows, split
