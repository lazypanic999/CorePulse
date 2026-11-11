import copy
import json

import pytest
import yaml

from corepulse_ml.autoencoder import classify_error
from corepulse_ml.data import load_windows, strict_json, timestamp_ns
from corepulse_ml.experiment import read_experiment
from corepulse_ml.export import artifact_for, validate_artifact
from corepulse_ml.numeric import is_finite_number
from corepulse_ml.reporting import compose_report
from corepulse_ml.training import positive_number
from corepulse_ml.validation import grouped_folds

from .artifact_helpers import hand_method
from .helpers import invented_windows
from .test_reporting import config_file, invented_reference


@pytest.mark.parametrize("value", [True, None, "1", 10**1000, float("nan"), float("inf")])
def test_nonfinite_or_unrepresentable_numbers_refuse_without_conversion_traceback(value):
    assert is_finite_number(value) is False
    with pytest.raises(ValueError):
        positive_number(value, "learning rate")
    with pytest.raises(ValueError):
        classify_error(value, 0)


@pytest.mark.parametrize("raw", ['{"x":1e999}', '{"x":-1e999}', '{"x":NaN}', '{"x":Infinity}'])
def test_json_rejects_nonfinite_exponents_and_constants_everywhere(raw):
    with pytest.raises(ValueError, match="non-finite"):
        strict_json(raw)


@pytest.mark.parametrize("field", ["test_fraction", "embargo_seconds"])
def test_manifest_oversized_integer_is_a_validation_error(tmp_path, field):
    windows, split = invented_windows(tmp_path)
    manifest = json.loads(split.read_text())
    manifest[field] = 10**1000
    split.write_text(json.dumps(manifest))
    with pytest.raises(ValueError, match=field):
        load_windows(windows, split)


def test_oversized_report_scores_and_coefficients_refuse(tmp_path):
    dataset, method = hand_method(tmp_path)
    artifact = artifact_for(method, dataset, synthetic=True)
    artifact["layers"][0]["bias"][0] = 10**1000
    with pytest.raises(ValueError):
        validate_artifact(artifact)
    reference = invented_reference(dataset)
    reference["models"]["nearest_centroid"]["predictions"][0]["score"] = 10**1000
    with pytest.raises(ValueError, match="score"):
        compose_report(dataset, reference, {}, {"synthetic": True})
    reference = invented_reference(dataset)
    reference["zscore_threshold"] = 10**1000
    with pytest.raises(ValueError, match="threshold"):
        compose_report(dataset, reference, {}, {"synthetic": True})
    reference = invented_reference(dataset)
    extra = copy.deepcopy(reference["models"]["nearest_centroid"])
    extra["window_metrics"]["coverage"] = 10**1000
    with pytest.raises(ValueError, match="scores"):
        compose_report(dataset, reference, {"invented": extra}, {"synthetic": True})


def test_impossible_fold_counts_refuse_before_allocation(tmp_path):
    path, config = config_file(tmp_path)
    config["inner_validation"]["folds"] = 33
    path.write_text(yaml.safe_dump(config))
    with pytest.raises(ValueError, match="2 and 32"):
        read_experiment(path)
    dataset = load_windows(config["dataset"]["windows"], config["dataset"]["split"])
    # No huge list should be created for a request exceeding available groups.
    with pytest.raises(ValueError, match="insufficient training groups"):
        grouped_folds(dataset, 10**1000)


@pytest.mark.parametrize("value", [None, True, 123, []])
def test_timestamp_type_errors_are_reported_as_validation_errors(value):
    with pytest.raises(ValueError):
        timestamp_ns(value)
