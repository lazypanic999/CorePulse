from dataclasses import replace
from datetime import datetime, timedelta

import pytest

from corepulse_ml.data import load_windows
from corepulse_ml.validation import fit_inner_preprocessor, group_for, grouped_folds, validate_fold

from .helpers import invented_windows, read_csv, rehash, write_csv


@pytest.mark.parametrize("schema,grouping", [(1, "run"), (2, "session")])
def test_whole_group_inner_folds_never_include_outer_test(tmp_path, schema, grouping):
    data = load_windows(*invented_windows(tmp_path, schema))
    folds = grouped_folds(data)
    assert folds == grouped_folds(data)
    assert len(folds) == 2
    validated = set()
    for fold in folds:
        assert fold.grouping == grouping
        assert set(fold.fit_indices) | set(fold.validation_indices) == set(data.train_indices)
        assert set(fold.fit_indices).isdisjoint(fold.validation_indices)
        assert set(fold.fit_indices).isdisjoint(data.test_indices)
        assert set(fold.validation_indices).isdisjoint(data.test_indices)
        assert set(fold.fit_groups).isdisjoint(fold.validation_groups)
        validated.update(fold.validation_indices)
        fit_inner_preprocessor(data, fold)
    assert validated == set(data.train_indices)


@pytest.mark.parametrize("schema", [1, 2])
def test_insufficient_independent_groups_refuse_without_reducing_fold_count(tmp_path, schema):
    data = load_windows(*invented_windows(tmp_path, schema, occasions=2))
    with pytest.raises(ValueError, match="insufficient"):
        grouped_folds(data, n_splits=2)


def test_inner_validation_values_do_not_enter_preprocessing(tmp_path):
    windows, split = invented_windows(tmp_path)
    data = load_windows(windows, split)
    fold = grouped_folds(data)[0]
    before = fit_inner_preprocessor(data, fold)
    records = read_csv(windows)
    for index in fold.validation_indices:
        records[index + 1][records[0].index("cpu_millicores_mean")] = "1000000"
    write_csv(windows, records)
    rehash(windows, split)
    changed = load_windows(windows, split)
    after_fold = grouped_folds(changed)[0]
    assert after_fold.fit_indices == fold.fit_indices
    after = fit_inner_preprocessor(changed, after_fold)
    assert after.medians == before.medians
    assert after.means == before.means
    assert after.scales == before.scales
    with pytest.raises(ValueError, match="hash"):
        validate_fold(changed, fold)


def test_injected_test_rows_or_split_run_cannot_enter_inner_fit(tmp_path):
    data = load_windows(*invented_windows(tmp_path))
    fold = grouped_folds(data)[0]
    with pytest.raises(ValueError, match="outer test"):
        validate_fold(data, replace(fold, fit_indices=fold.fit_indices + (data.test_indices[0],)))
    moved = fold.fit_indices[0]
    with pytest.raises(ValueError, match="group|declared"):
        validate_fold(
            data,
            replace(
                fold,
                fit_indices=fold.fit_indices[1:],
                validation_indices=fold.validation_indices + (moved,),
            ),
        )


def test_inner_embargo_purges_whole_sessions_without_changing_validation_selection(tmp_path):
    windows, split = invented_windows(tmp_path, occasions=5)
    data = load_windows(windows, split)
    original = grouped_folds(data)
    fit_session = original[0].fit_groups[0]
    valid_session = original[0].validation_groups[0]
    fit_run = next(
        r.run_id for r in data.rows if r.session_id == fit_session and r.scenario == "normal"
    )
    valid_run = next(
        r.run_id for r in data.rows if r.session_id == valid_session and r.scenario == "normal"
    )
    records = read_csv(windows)
    fit_start = datetime.fromisoformat(next(r[3] for r in records[1:] if r[0] == fit_run))
    valid_start = datetime.fromisoformat(next(r[3] for r in records[1:] if r[0] == valid_run))
    offset = valid_start + timedelta(seconds=60) - fit_start
    for record in records[1:]:
        if record[0] == fit_run:
            for column in range(3, 7):
                value = datetime.fromisoformat(record[column]) + offset
                record[column] = value.strftime("%Y-%m-%dT%H:%M:%S") + ".000000001Z"
    write_csv(windows, records)
    rehash(windows, split)
    changed = load_windows(windows, split)
    folds = grouped_folds(changed)
    assert [f.validation_groups for f in folds] == [f.validation_groups for f in original]
    assert any(f.purged_groups for f in folds)
    for fold in folds:
        for group in fold.purged_groups:
            members = {i for i in changed.train_indices if group_for(changed, i) == group}
            assert members <= set(fold.purged_indices)
            assert members.isdisjoint(fold.fit_indices)
        validate_fold(changed, fold)
