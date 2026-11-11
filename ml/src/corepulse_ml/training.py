"""Shared CPU-only building blocks; fitted networks stay in memory/local outputs."""

from __future__ import annotations

import math
from contextlib import contextmanager
from dataclasses import dataclass

import numpy as np
import torch
from torch import nn

from corepulse_ml.data import Preprocessor


@dataclass(frozen=True)
class TrainedMethod:
    kind: str
    model: nn.Sequential
    preprocessor: Preprocessor
    labels: tuple[str, ...]
    report: dict
    training: dict
    threshold: float | None = None


@contextmanager
def seeded_cpu(seed: int):
    if type(seed) is not int or not 0 <= seed < 2**63:
        raise ValueError("training seed must be a nonnegative int64")
    previous = torch.are_deterministic_algorithms_enabled()
    previous_warn = torch.is_deterministic_algorithms_warn_only_enabled()
    with torch.random.fork_rng(devices=[]):
        torch.manual_seed(seed)
        torch.use_deterministic_algorithms(True)
        try:
            yield
        finally:
            torch.use_deterministic_algorithms(previous, warn_only=previous_warn)


def dense_network(widths: tuple[int, ...]) -> nn.Sequential:
    if len(widths) < 2 or any(type(width) is not int or not 1 <= width <= 4096 for width in widths):
        raise ValueError("invalid dense network dimensions")
    layers = []
    for index, (inputs, outputs) in enumerate(zip(widths[:-1], widths[1:], strict=True)):
        layers.append(nn.Linear(inputs, outputs, device="cpu", dtype=torch.float32))
        if index < len(widths) - 2:
            layers.append(nn.ReLU())
    return nn.Sequential(*layers)


def tensor(matrix: np.ndarray) -> torch.Tensor:
    result = torch.as_tensor(matrix, dtype=torch.float32, device="cpu")
    if result.ndim != 2 or not result.shape[0] or not torch.isfinite(result).all():
        raise ValueError("training requires a nonempty finite matrix")
    return result


def optimize_step(model: nn.Module, optimizer, loss: torch.Tensor) -> float:
    value = float(loss.detach())
    if not math.isfinite(value):
        raise ValueError("non-finite training loss")
    optimizer.zero_grad(set_to_none=True)
    loss.backward()
    if any(p.grad is not None and not torch.isfinite(p.grad).all() for p in model.parameters()):
        raise ValueError("non-finite training gradient")
    optimizer.step()
    if any(not torch.isfinite(p).all() for p in model.parameters()):
        raise ValueError("non-finite trained parameter")
    return value


def positive_number(value, label: str, *, zero: bool = False) -> float:
    if (
        type(value) not in (int, float)
        or not math.isfinite(value)
        or value < 0
        or not zero
        and value == 0
    ):
        raise ValueError(f"invalid {label}")
    return float(value)
