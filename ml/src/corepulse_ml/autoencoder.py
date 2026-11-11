"""Normal-only CPU autoencoder with disjoint validation-percentile calibration."""

from __future__ import annotations

import math
from dataclasses import asdict, dataclass

import numpy as np
import torch
from torch import nn

from corepulse_ml.data import WindowDataset, fit_preprocessor
from corepulse_ml.metrics import model_report
from corepulse_ml.numeric import is_finite_number
from corepulse_ml.training import (
    TrainedMethod,
    dense_network,
    optimize_step,
    positive_number,
    seeded_cpu,
    tensor,
)
from corepulse_ml.validation import GroupFold, validate_fold


@dataclass(frozen=True)
class AutoencoderOptions:
    hidden: tuple[int, ...]
    epochs: int
    learning_rate: float
    weight_decay: float
    percentile: float
    calibration_fold: int


def read_options(config: dict) -> AutoencoderOptions:
    if (
        not isinstance(config, dict)
        or config.keys()
        != {
            "kind",
            "hidden",
            "epochs",
            "learning_rate",
            "weight_decay",
            "percentile",
            "calibration_fold",
        }
        or config["kind"] != "autoencoder"
    ):
        raise ValueError(
            "autoencoder requires exact declared architecture, training and calibration fields"
        )
    hidden = config["hidden"]
    if (
        not isinstance(hidden, list)
        or not 1 <= len(hidden) <= 3
        or any(type(n) is not int or not 1 <= n <= 256 for n in hidden)
    ):
        raise ValueError("autoencoder requires one to three encoder layers with 1..256 units")
    epochs, fold = config["epochs"], config["calibration_fold"]
    if type(epochs) is not int or not 1 <= epochs <= 1000:
        raise ValueError("autoencoder epochs must be a fixed integer in 1..1000")
    if type(fold) is not int or fold < 0:
        raise ValueError("calibration fold must be a fixed nonnegative index")
    percentile = positive_number(config["percentile"], "validation percentile")
    if percentile > 100:
        raise ValueError("validation percentile must be in (0,100]")
    return AutoencoderOptions(
        tuple(hidden),
        epochs,
        positive_number(config["learning_rate"], "learning rate"),
        positive_number(config["weight_decay"], "weight decay", zero=True),
        percentile,
        fold,
    )


def reconstruction_errors(model: nn.Module, matrix: torch.Tensor) -> torch.Tensor:
    with torch.no_grad():
        errors = torch.mean((model(matrix) - matrix) ** 2, dim=1)
    if not torch.isfinite(errors).all():
        raise ValueError("non-finite reconstruction error")
    return errors


def classify_error(error: float, threshold: float) -> str:
    if not all(is_finite_number(x) and x >= 0 for x in (error, threshold)):
        raise ValueError("invalid reconstruction error or threshold")
    return "fault" if error > threshold else "normal"


def train_autoencoder(
    dataset: WindowDataset, folds: tuple[GroupFold, ...], configuration: dict, seed: int
) -> TrainedMethod:
    options = read_options(configuration)
    if type(seed) is not int or not 0 <= seed < 2**63:
        raise ValueError("training seed must be a nonnegative int64")
    if any(type(f.index) is not int or f.index < 0 for f in folds) or len(
        {f.index for f in folds}
    ) != len(folds):
        raise ValueError("duplicate calibration fold")
    selected = [f for f in folds if f.index == options.calibration_fold]
    if len(selected) != 1:
        raise ValueError("declared calibration fold is unavailable")
    fold = selected[0]
    validate_fold(dataset, fold)
    fit_indices = tuple(i for i in fold.fit_indices if dataset.rows[i].scenario == "normal")
    validation_indices = tuple(
        i for i in fold.validation_indices if dataset.rows[i].scenario == "normal"
    )
    if len(fit_indices) < 2 or len(validation_indices) < 2:
        raise ValueError("autoencoder needs at least two normal fit and normal calibration windows")
    preprocessor = fit_preprocessor(dataset, fit_indices)
    if options.hidden[-1] >= len(preprocessor.feature_names):
        raise ValueError("autoencoder bottleneck must be smaller than observed input dimension")
    fit = tensor(preprocessor.transform(dataset.rows[i] for i in fit_indices))
    validation = tensor(preprocessor.transform(dataset.rows[i] for i in validation_indices))
    widths = (fit.shape[1], *options.hidden, *reversed(options.hidden[:-1]), fit.shape[1])
    with seeded_cpu(seed):
        model = dense_network(widths)
        optimizer = torch.optim.Adam(
            model.parameters(), lr=options.learning_rate, weight_decay=options.weight_decay
        )
        for _ in range(options.epochs):
            optimize_step(model, optimizer, nn.functional.mse_loss(model(fit), fit))
        model.eval()
        calibration = reconstruction_errors(model, validation).numpy()
        # 'higher' selects an observed order statistic, avoiding implicit
        # interpolation between a tiny number of calibration examples.
        threshold = float(np.percentile(calibration, options.percentile, method="higher"))
        errors = reconstruction_errors(
            model, tensor(preprocessor.transform(dataset.partition("test")))
        )
    if not math.isfinite(threshold) or threshold < 0:
        raise ValueError("invalid calibrated threshold")
    predictions = []
    for row, error in zip(dataset.partition("test"), errors, strict=True):
        score = float(error)
        predictions.append(
            {
                "run_id": row.run_id,
                "window_start": row.window_start,
                "truth": "normal" if row.scenario == "normal" else "fault",
                "predicted": classify_error(score, threshold),
                "score": score,
            }
        )
    training = {
        "options": asdict(options),
        "normal_fit_run_ids": list(preprocessor.fit_run_ids),
        "normal_validation_run_ids": sorted({dataset.rows[i].run_id for i in validation_indices}),
        "normal_fit_windows": len(fit_indices),
        "normal_validation_windows": len(validation_indices),
        "threshold": threshold,
        "percentile_method": "higher",
        "normal_validation_errors": list(map(float, calibration)),
        "selection": (
            "fixed epochs and calibration fold; normal-only disjoint validation percentile"
        ),
        "score_semantics": "standardized-resource reconstruction MSE; not anomaly probability",
        "calibration_limit": (
            "empirical threshold; small calibration support does not guarantee "
            "a false-positive rate"
        ),
        "development_only": True,
    }
    report = model_report(
        "normal_vs_fault_interval",
        ["normal", "fault"],
        predictions,
        family="autoencoder",
        seed=seed,
        training=training,
    )
    return TrainedMethod(
        "autoencoder",
        model,
        preprocessor,
        ("normal", "fault"),
        report,
        training,
        threshold,
        split_sha256=dataset.split_sha256,
    )
