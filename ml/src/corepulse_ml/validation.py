"""Deterministic inner folds, entirely inside the already verified outer train set."""

from __future__ import annotations

import hashlib
from collections import defaultdict
from dataclasses import dataclass

from corepulse_ml.data import Preprocessor, WindowDataset, fit_preprocessor, near, run_ranges
from corepulse_ml.numeric import duration_ns


@dataclass(frozen=True)
class GroupFold:
    index: int
    grouping: str
    seed: int
    windows_sha256: str
    outer_split_sha256: str
    fit_indices: tuple[int, ...]
    validation_indices: tuple[int, ...]
    purged_indices: tuple[int, ...]
    fit_groups: tuple[str, ...]
    validation_groups: tuple[str, ...]
    purged_groups: tuple[str, ...]


def _key(seed: int, value: str) -> str:
    return hashlib.sha256(f"{seed}\0{value}".encode()).hexdigest()


def group_for(dataset: WindowDataset, index: int) -> str:
    row = dataset.rows[index]
    return row.session_id if dataset.schema_version == 2 else row.run_id


def validate_fold(dataset: WindowDataset, fold: GroupFold) -> None:
    if (
        fold.windows_sha256 != dataset.windows_sha256
        or fold.outer_split_sha256 != dataset.split_sha256
    ):
        raise ValueError("inner fold hash differs from the verified outer dataset/split")
    expected_grouping = "session" if dataset.schema_version == 2 else "run"
    if fold.grouping != expected_grouping:
        raise ValueError("inner grouping must follow the outer manifest schema")
    roles, groups = {}, {}
    outer_training = set(dataset.train_indices)
    for role, indices, declared in (
        ("fit", fold.fit_indices, fold.fit_groups),
        ("validation", fold.validation_indices, fold.validation_groups),
        ("purged", fold.purged_indices, fold.purged_groups),
    ):
        if role != "purged" and not indices:
            raise ValueError("empty inner fit/validation partition")
        observed = set()
        for index in indices:
            if type(index) is not int or index not in outer_training or index in roles:
                raise ValueError("inner fold overlaps or includes outer test/purged rows")
            group = group_for(dataset, index)
            if not group or group in groups and groups[group] != role:
                raise ValueError("a whole group crosses inner partition roles")
            roles[index], groups[group] = role, role
            observed.add(group)
        if len(set(declared)) != len(declared) or set(declared) != observed:
            raise ValueError("inner declared groups differ from actual rows")
    if set(roles) != set(dataset.train_indices):
        raise ValueError("inner fold must account for every outer training row")
    labels = {dataset.rows[i].scenario for i in dataset.train_indices}
    for indices in (fold.fit_indices, fold.validation_indices):
        if {dataset.rows[i].scenario for i in indices} != labels:
            raise ValueError("inner holdout/embargo loses class support; collect broader groups")
    margin = duration_ns(dataset.manifest.get("embargo_seconds", 0))
    if margin:
        ranges = run_ranges(dataset.rows[i] for i in dataset.train_indices)
        fit_runs = {dataset.rows[i].run_id for i in fold.fit_indices}
        validation_runs = {dataset.rows[i].run_id for i in fold.validation_indices}
        if any(near(ranges[a], ranges[b], margin) for a in fit_runs for b in validation_runs):
            raise ValueError("inner temporal separation was not retained")


def grouped_folds(
    dataset: WindowDataset, n_splits: int = 2, seed: int = 17
) -> tuple[GroupFold, ...]:
    if type(n_splits) is not int or n_splits < 2:
        raise ValueError("inner validation requires at least two folds")
    if type(seed) is not int or not -(2**63) <= seed < 2**63:
        raise ValueError("invalid inner seed")
    manifest = dataset.manifest
    grouping = "session" if manifest["schema_version"] == 2 else "run"
    members = defaultdict(list)
    for index in dataset.train_indices:
        group = group_for(dataset, index)
        if not group:
            raise ValueError("unknown inner group provenance")
        members[group].append(index)
    # Reject impossible requests before constructing a potentially enormous list.
    if len(members) < n_splits:
        raise ValueError("insufficient training groups for requested inner folds")
    buckets = [set() for _ in range(n_splits)]
    if grouping == "session":
        if len(members) < n_splits:
            raise ValueError("insufficient declared training sessions for inner folds")
        ordered = sorted(members, key=lambda group: _key(seed, "inner-session/" + group))
        for index, group in enumerate(ordered):
            buckets[index % n_splits].add(group)
    else:
        scenarios = defaultdict(list)
        for group, indices in members.items():
            scenarios[dataset.rows[indices[0]].scenario].append(group)
        for label, groups in sorted(scenarios.items()):
            if len(groups) < n_splits:
                raise ValueError(f"class {label} has insufficient training runs for inner folds")
            ordered = sorted(groups, key=lambda group: _key(seed, f"inner-run/{label}/{group}"))
            for index, group in enumerate(ordered):
                buckets[index % n_splits].add(group)
    ranges = run_ranges(dataset.rows[i] for i in dataset.train_indices)
    margin = duration_ns(manifest.get("embargo_seconds", 0))
    result = []
    for number, validation in enumerate(buckets):
        fit = set(members) - validation
        purged = set()
        if margin:
            validation_runs = {dataset.rows[i].run_id for g in validation for i in members[g]}
            for group in fit:
                runs = {dataset.rows[i].run_id for i in members[group]}
                if any(near(ranges[a], ranges[b], margin) for a in runs for b in validation_runs):
                    purged.add(group)
        fit -= purged

        def indices(groups):
            return tuple(sorted(i for group in groups for i in members[group]))

        fold = GroupFold(
            number,
            grouping,
            seed,
            dataset.windows_sha256,
            dataset.split_sha256,
            indices(fit),
            indices(validation),
            indices(purged),
            tuple(sorted(fit)),
            tuple(sorted(validation)),
            tuple(sorted(purged)),
        )
        validate_fold(dataset, fold)
        result.append(fold)
    return tuple(result)


def fit_inner_preprocessor(dataset: WindowDataset, fold: GroupFold) -> Preprocessor:
    validate_fold(dataset, fold)
    return fit_preprocessor(dataset, fold.fit_indices)
