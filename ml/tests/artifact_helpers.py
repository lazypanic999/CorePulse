"""Hand-authored coefficients only; no trained weights are stored as test assets."""

import torch

from corepulse_ml.data import fit_preprocessor, load_windows
from corepulse_ml.training import TrainedMethod, dense_network
from corepulse_ml.validation import grouped_folds

from .helpers import invented_windows


def hand_method(directory, kind="mlp", features=("cpu_millicores_mean", "cpu_millicores_delta")):
    dataset = load_windows(*invented_windows(directory), features=features)
    labels = ("normal", "pod_crash:amf", "pod_crash:upf")
    task, training, threshold = "scenario_classification", {}, None
    fit_indices = dataset.train_indices
    if kind == "autoencoder":
        fold = grouped_folds(dataset)[0]
        fit_indices = tuple(i for i in fold.fit_indices if dataset.rows[i].scenario == "normal")
        labels, task, threshold = ("normal", "fault"), "normal_vs_fault_interval", 0.25
        training = {
            "normal_validation_run_ids": sorted(
                {
                    dataset.rows[i].run_id
                    for i in fold.validation_indices
                    if dataset.rows[i].scenario == "normal"
                }
            )
        }
    preprocessor = fit_preprocessor(dataset, fit_indices)
    dimension = len(preprocessor.feature_names)
    widths = (
        (dimension, min(4, dimension), 3)
        if kind == "mlp"
        else (dimension, min(2, dimension - 1), dimension)
    )
    model = dense_network(widths)
    with torch.no_grad():
        for index, parameter in enumerate(model.parameters()):
            parameter.copy_(
                torch.arange(parameter.numel(), dtype=torch.float32).reshape(parameter.shape)
                / max(16, 8 * dimension)
                - (index + 1) / 8
            )
    model.eval()
    method = TrainedMethod(
        kind,
        model,
        preprocessor,
        labels,
        {"task": task, "family": kind, "seed": 7},
        training,
        threshold,
        dataset.split_sha256,
    )
    return dataset, method
