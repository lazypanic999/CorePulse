import json
import os
import shutil
import subprocess
from dataclasses import replace
from pathlib import Path

import numpy as np
import pytest
import torch

from corepulse_ml.data import NUMERIC_COLUMNS, RESOURCE_FEATURES
from corepulse_ml.export import artifact_for
from corepulse_ml.training import tensor

from .artifact_helpers import hand_method


@pytest.mark.parametrize("kind", ["mlp", "autoencoder"])
@pytest.mark.parametrize("full_features", [False, True])
def test_go_matches_cpu_pytorch_on_hand_authored_fixtures(tmp_path, kind, full_features):
    features = (
        RESOURCE_FEATURES if full_features else ("cpu_millicores_mean", "cpu_millicores_delta")
    )
    dataset, method = hand_method(tmp_path / "inputs", kind, features)
    artifact = artifact_for(method, dataset, synthetic=True)
    inputs = [
        {},
        dict.fromkeys(features),
        dict.fromkeys(features, 0.0),
        {name: (None if index % 2 else 0.0) for index, name in enumerate(features)},
    ]
    rng = np.random.default_rng(20261111)
    for _ in range(16):
        values = {}
        for name in features:
            if rng.random() < 0.2:
                continue
            low, high = (
                (0, 1)
                if name.endswith("_missing_ratio")
                else ((-4, 4) if name.endswith("_delta") else (0, 4))
            )
            values[name] = float(rng.uniform(low, high))
        inputs.append(values)
    cases = []
    for values in inputs:
        raw_values = list(dataset.rows[0].values)
        for name in features:
            raw_values[NUMERIC_COLUMNS.index(name)] = values.get(name)
        row = replace(dataset.rows[0], values=tuple(raw_values))
        matrix = tensor(method.preprocessor.transform([row]))
        with torch.no_grad():
            output = method.model(matrix)[0]
            if kind == "mlp":
                probabilities = torch.softmax(output, dim=0)
                index = int(torch.argmax(probabilities))
                scores, score, predicted = (
                    probabilities.tolist(),
                    float(probabilities[index]),
                    method.labels[index],
                )
            else:
                scores = []
                score = float(torch.mean((output - matrix[0]) ** 2))
                predicted = "fault" if score > method.threshold else "normal"
        cases.append(
            {
                "values": values,
                "outputs": output.tolist(),
                "scores": scores,
                "score": score,
                "predicted": predicted,
            }
        )
    path = tmp_path / "hand-authored-parity.json"
    path.write_text(
        json.dumps(
            {
                "fixture_kind": "hand_authored_not_trained",
                "tolerance": 1e-5,
                "artifact": artifact,
                "cases": cases,
            },
            allow_nan=False,
        )
    )
    env = os.environ.copy()
    env.update(
        COREPULSE_PYTORCH_FIXTURE=str(path),
        CGO_ENABLED="0",
        GOPROXY="off",
        GOSUMDB="off",
        GOTOOLCHAIN="local",
    )
    binary = env.get("COREPULSE_GO_PARITY_BINARY")
    if binary:
        command = [binary, "-test.run=^TestPyTorchFixtureParity$", "-test.v", "-test.count=1"]
    else:
        go = shutil.which("go")
        if not go:
            pytest.fail("Go is required for parity; install Go or set COREPULSE_GO_PARITY_BINARY")
        command = [
            go,
            "test",
            "./internal/inference",
            "-run",
            "^TestPyTorchFixtureParity$",
            "-v",
            "-count=1",
        ]
    completed = subprocess.run(
        command,
        cwd=Path(__file__).resolve().parents[2],
        env=env,
        capture_output=True,
        text=True,
        timeout=120,
        check=False,
    )
    assert completed.returncode == 0, completed.stdout + completed.stderr
    assert "PYTORCH_PARITY_CHECKED 20" in completed.stdout, completed.stdout + completed.stderr
