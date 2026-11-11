"""Small resource MLP; epoch selection uses grouped inner validation only."""

from __future__ import annotations

import math
from dataclasses import asdict, dataclass

import torch
from torch import nn

from corepulse_ml.data import WindowDataset, fit_preprocessor
from corepulse_ml.metrics import model_report
from corepulse_ml.reporting import labels_for
from corepulse_ml.training import (
    TrainedMethod,
    dense_network,
    optimize_step,
    positive_number,
    seeded_cpu,
    tensor,
)
from corepulse_ml.validation import GroupFold, fit_inner_preprocessor, validate_fold


@dataclass(frozen=True)
class MLPOptions:
    hidden: tuple[int, ...]
    epochs: tuple[int, ...]
    learning_rate: float
    weight_decay: float


def read_options(config: dict) -> MLPOptions:
    if (
        not isinstance(config, dict)
        or config.keys() != {"kind", "hidden", "epochs", "learning_rate", "weight_decay"}
        or config["kind"] != "mlp"
    ):
        raise ValueError("MLP requires exact kind/hidden/epochs/learning_rate/weight_decay fields")
    hidden, epochs = config["hidden"], config["epochs"]
    if (
        not isinstance(hidden, list)
        or not 1 <= len(hidden) <= 3
        or any(type(n) is not int or not 1 <= n <= 256 for n in hidden)
    ):
        raise ValueError("MLP requires one to three hidden layers with 1..256 units")
    if (
        not isinstance(epochs, list)
        or not epochs
        or len(epochs) > 16
        or any(type(n) is not int or not 1 <= n <= 1000 for n in epochs)
        or epochs != sorted(set(epochs))
    ):
        raise ValueError("MLP epoch candidates must be unique increasing integers in 1..1000")
    return MLPOptions(
        tuple(hidden),
        tuple(epochs),
        positive_number(config["learning_rate"], "learning rate"),
        positive_number(config["weight_decay"], "weight decay", zero=True),
    )


def train_mlp(
    dataset: WindowDataset, folds: tuple[GroupFold, ...], configuration: dict, seed: int
) -> TrainedMethod:
    options = read_options(configuration)
    if type(seed) is not int or not 0 <= seed < 2**63:
        raise ValueError("training seed must be a nonnegative int64")
    if not folds:
        raise ValueError("MLP requires grouped inner validation")
    # Validate all partitions before fitting anything. A repeated fold must not
    # silently count twice in model selection.
    if any(type(f.index) is not int or f.index < 0 for f in folds) or len(
        {fold.index for fold in folds}
    ) != len(folds):
        raise ValueError("duplicate inner fold")
    validation_rows = [i for fold in folds for i in fold.validation_indices]
    if len(validation_rows) != len(set(validation_rows)) or set(validation_rows) != set(
        dataset.train_indices
    ):
        raise ValueError("inner validation must hold out each outer training row exactly once")
    for fold in folds:
        validate_fold(dataset, fold)
    labels = labels_for(dataset, "scenario_classification")
    targets = {label: index for index, label in enumerate(labels)}

    def y(indices):
        return torch.tensor([targets[dataset.rows[i].scenario] for i in indices], dtype=torch.long)

    losses = {epoch: [] for epoch in options.epochs}
    fold_details = []
    for fold in folds:
        preprocessor = fit_inner_preprocessor(dataset, fold)
        fit = tensor(preprocessor.transform(dataset.rows[i] for i in fold.fit_indices))
        validation = tensor(
            preprocessor.transform(dataset.rows[i] for i in fold.validation_indices)
        )
        fit_targets, validation_targets = y(fold.fit_indices), y(fold.validation_indices)
        checkpoints = {}
        # Seed derivation is fixed before scores exist and remains inside CPU RNG scope.
        with seeded_cpu((seed + fold.index + 1) % 2**63):
            model = dense_network((fit.shape[1], *options.hidden, len(labels)))
            optimizer = torch.optim.Adam(
                model.parameters(), lr=options.learning_rate, weight_decay=options.weight_decay
            )
            for epoch in range(1, max(options.epochs) + 1):
                model.train()
                optimize_step(
                    model, optimizer, nn.functional.cross_entropy(model(fit), fit_targets)
                )
                if epoch in losses:
                    model.eval()
                    with torch.no_grad():
                        loss = float(
                            nn.functional.cross_entropy(model(validation), validation_targets)
                        )
                    if not math.isfinite(loss):
                        raise ValueError("non-finite inner validation loss")
                    losses[epoch].append(loss)
                    checkpoints[str(epoch)] = loss
        fold_details.append(
            {
                "fold": fold.index,
                "fit_run_ids": list(preprocessor.fit_run_ids),
                "validation_run_ids": sorted(
                    {dataset.rows[i].run_id for i in fold.validation_indices}
                ),
                "validation_cross_entropy": checkpoints,
            }
        )
    means = {epoch: sum(values) / len(values) for epoch, values in losses.items()}
    selected = min(options.epochs, key=lambda epoch: (means[epoch], epoch))
    preprocessor = fit_preprocessor(dataset)
    fit = tensor(preprocessor.transform(dataset.partition("train")))
    with seeded_cpu(seed):
        model = dense_network((fit.shape[1], *options.hidden, len(labels)))
        optimizer = torch.optim.Adam(
            model.parameters(), lr=options.learning_rate, weight_decay=options.weight_decay
        )
        for _ in range(selected):
            optimize_step(
                model, optimizer, nn.functional.cross_entropy(model(fit), y(dataset.train_indices))
            )
        model.eval()
        with torch.no_grad():
            probabilities = torch.softmax(
                model(tensor(preprocessor.transform(dataset.partition("test")))), dim=1
            )
    if not torch.isfinite(probabilities).all():
        raise ValueError("non-finite held-out prediction")
    predictions = []
    for row, scores in zip(dataset.partition("test"), probabilities, strict=True):
        index = int(torch.argmax(scores))
        predictions.append(
            {
                "run_id": row.run_id,
                "window_start": row.window_start,
                "truth": row.scenario,
                "predicted": labels[index],
                "score": float(scores[index]),
            }
        )
    training = {
        "options": asdict(options),
        "selected_epochs": selected,
        "selection": "mean grouped-inner-validation cross entropy; earlier epoch breaks ties",
        "mean_validation_cross_entropy": {str(epoch): value for epoch, value in means.items()},
        "folds": fold_details,
        "final_fit_run_ids": list(preprocessor.fit_run_ids),
        "score_semantics": "maximum softmax value; not a calibrated probability",
        "development_only": True,
    }
    report = model_report(
        "scenario_classification",
        labels,
        predictions,
        family="mlp",
        seed=seed,
        training=training,
    )
    return TrainedMethod("mlp", model, preprocessor, tuple(labels), report, training)
