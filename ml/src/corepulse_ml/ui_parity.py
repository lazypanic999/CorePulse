"""Freeze CPU outputs for checking a generated UI job in the Go runtime."""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path

import numpy as np
import torch
from torch.nn import functional as functional

from corepulse_ml.data import NUMERIC_COLUMNS, load_windows, strict_json
from corepulse_ml.export import ensure_artifact_destination, validate_artifact
from corepulse_ml.reporting import write_report


def write_parity(directory: Path) -> dict:
    directory = directory.resolve()
    report_path = directory / "evaluation.json"
    report = strict_json(report_path.read_bytes())
    if report.get("synthetic") is not True or report.get("schema_version") != 3:
        raise ValueError("Only synthetic schema-three neural jobs can freeze UI references")
    dataset = load_windows(directory / "windows.csv", directory / "split.json")
    required = {"mlp_seed_7", "mlp_seed_23", "autoencoder_seed_7", "autoencoder_seed_23"}
    if set(report["experiment"]["artifacts"]) != required:
        raise ValueError("All fixed-seed exports are required")
    rows = dataset.partition("test")
    output = {
        "windows_sha256": dataset.windows_sha256,
        "split_sha256": dataset.split_sha256,
        "models": {},
    }
    sources = [report_path, directory / "windows.csv", directory / "split.json"]
    for name, descriptor in report["experiment"]["artifacts"].items():
        if descriptor["filename"] != name + ".json" or name not in {
            "mlp_seed_7",
            "mlp_seed_23",
            "autoencoder_seed_7",
            "autoencoder_seed_23",
        }:
            raise ValueError("Unexpected fixed tutorial artifact")
        source = directory / "models" / descriptor["filename"]
        raw = source.read_bytes()
        if hashlib.sha256(raw).hexdigest() != descriptor["sha256"]:
            raise ValueError("Artifact hash mismatch")
        artifact = strict_json(raw)
        validate_artifact(artifact)
        if artifact["source"] != {
            "windows_sha256": dataset.windows_sha256,
            "split_sha256": dataset.split_sha256,
        }:
            raise ValueError("Artifact source differs from the verified cohort")
        pre = artifact["preprocessing"]
        indices = [NUMERIC_COLUMNS.index(name) for name in pre["feature_names"]]
        matrix = np.array(
            [
                [
                    pre["medians"][j] if row.values[i] is None else row.values[i]
                    for j, i in enumerate(indices)
                ]
                for row in rows
            ],
            dtype=np.float64,
        )
        matrix = (matrix - np.array(pre["means"])) / np.array(pre["scales"])
        inputs = torch.tensor(matrix, dtype=torch.float32)
        values = inputs
        with torch.no_grad():
            for layer in artifact["layers"]:
                if layer["op"] == "relu":
                    values = functional.relu(values)
                else:
                    values = functional.linear(
                        values,
                        torch.tensor(layer["weights"], dtype=torch.float32),
                        torch.tensor(layer["bias"], dtype=torch.float32),
                    )
            if artifact["kind"] == "mlp":
                scores, labels = torch.softmax(values, dim=1).max(dim=1)
                predicted = [artifact["labels"][int(index)] for index in labels]
            else:
                scores = torch.mean((values - inputs) ** 2, dim=1)
                predicted = [
                    "fault" if float(score) > artifact["threshold"] else "normal"
                    for score in scores
                ]
        expected = {
            (p["run_id"], p["window_start"]): p for p in report["models"][name]["predictions"]
        }
        cases = []
        for i, row in enumerate(rows):
            p = expected[(row.run_id, row.window_start)]
            if float(scores[i]) != p["score"] or predicted[i] != p["predicted"]:
                raise ValueError(
                    "Reconstructed CPU outputs differ from the original trained report"
                )
            cases.append(
                {
                    "run_id": row.run_id,
                    "window": row.window_start,
                    "inputs": inputs[i].tolist(),
                    "outputs": values[i].tolist(),
                    "score": float(scores[i]),
                    "predicted": predicted[i],
                }
            )
        output["models"][name] = {"artifact_sha256": descriptor["sha256"], "cases": cases}
        sources.append(source)
    target = ensure_artifact_destination(directory / "parity.json")
    write_report(target, output, sources)
    return output


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    write_parity(parser.parse_args().directory)
