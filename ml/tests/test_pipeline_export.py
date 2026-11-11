import json

import pytest
import yaml

from corepulse_ml.data import NUMERIC_COLUMNS, load_windows
from corepulse_ml.experiment import run_experiment

from .go_runner import run_go_fixture
from .helpers import invented_windows
from .test_autoencoder import options as autoencoder_options
from .test_mlp import options as mlp_options
from .test_reporting import config_file, invented_reference


@pytest.mark.parametrize("schema", [1, 2])
def test_synthetic_training_json_export_go_inference_matches_all_heldout_predictions(
    tmp_path, schema
):
    path, config = config_file(tmp_path)
    windows, split = invented_windows(tmp_path, schema=schema)
    dataset = load_windows(windows, split)
    (tmp_path / "refs.json").write_text(json.dumps(invented_reference(dataset)))
    config["methods"] = [mlp_options(), autoencoder_options()]
    path.write_text(yaml.safe_dump(config))
    directory = tmp_path / "temporary-coefficients"
    report = run_experiment(path, export_dir=directory)
    artifacts = []
    for name, descriptor in report["experiment"]["artifacts"].items():
        artifact_path = directory / descriptor["filename"]
        artifact = json.loads(artifact_path.read_text())
        model = report["models"][name]
        predictions = {(p["run_id"], p["window_start"]): p for p in model["predictions"]}
        cases = []
        for row in dataset.partition("test"):
            prediction = predictions[row.run_id, row.window_start]
            cases.append(
                {
                    "values": {
                        f: row.values[NUMERIC_COLUMNS.index(f)]
                        for f in artifact["preprocessing"]["feature_names"]
                    },
                    "predicted": prediction["predicted"],
                    "score": prediction["score"],
                }
            )
        artifacts.append(
            {
                "path": str(artifact_path),
                "sha256": descriptor["sha256"],
                "kind": artifact["kind"],
                "task": artifact["task"],
                "cases": cases,
            }
        )
    fixture = tmp_path / "synthetic-pipeline.json"
    fixture.write_text(
        json.dumps(
            {
                "fixture_kind": "synthetic_trained_temporary",
                "tolerance": 1e-5,
                "source": {
                    "windows_sha256": dataset.windows_sha256,
                    "split_sha256": dataset.split_sha256,
                },
                "artifacts": artifacts,
            },
            allow_nan=False,
        )
    )
    run_go_fixture(
        "TestSyntheticPipelineExport",
        {"COREPULSE_SYNTHETIC_PIPELINE_FIXTURE": str(fixture)},
        "SYNTHETIC_PIPELINE_CHECKED 4 artifacts 24 held-out predictions",
    )
