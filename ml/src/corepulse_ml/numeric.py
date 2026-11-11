"""Strict JSON-compatible finite numbers, including oversized integer refusal."""

import math


def is_finite_number(value) -> bool:
    if type(value) not in (int, float):
        return False
    try:
        return math.isfinite(value)
    except OverflowError:
        return False
