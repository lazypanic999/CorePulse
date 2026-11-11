"""Strict JSON-compatible finite numbers, including oversized integer refusal."""

import math


def is_finite_number(value) -> bool:
    if type(value) not in (int, float):
        return False
    try:
        return math.isfinite(value)
    except OverflowError:
        return False


def duration_ns(seconds: int | float) -> int:
    """Recover a Go Duration.Seconds value without truncating one nanosecond."""
    if not is_finite_number(seconds) or not 0 <= seconds <= 86400:
        raise ValueError("duration must be finite and between zero and 24 hours")
    nanos = round(seconds * 1_000_000_000)
    whole, fraction = divmod(nanos, 1_000_000_000)
    if whole + fraction / 1_000_000_000 != seconds:
        raise ValueError("duration must resolve to whole nanoseconds")
    return nanos
