"""Small, auditable kubectl adapter. No shell interpolation is used."""

from __future__ import annotations

import json
import subprocess
from typing import Any


class KubectlError(RuntimeError):
    pass


class Kubectl:
    def __init__(self, binary: str = "kubectl") -> None:
        self.binary = binary

    def run(self, *args: str, timeout: int = 30) -> str:
        command = [self.binary, *args]
        try:
            result = subprocess.run(
                command, check=True, capture_output=True, text=True, timeout=timeout
            )
        except FileNotFoundError as exc:
            raise KubectlError("kubectl is not installed or is not on PATH") from exc
        except subprocess.TimeoutExpired as exc:
            raise KubectlError(f"kubectl timed out: {' '.join(args)}") from exc
        except subprocess.CalledProcessError as exc:
            detail = (exc.stderr or exc.stdout or "unknown error").strip()
            raise KubectlError(f"kubectl failed: {detail}") from exc
        return result.stdout.strip()

    def get_json(self, *args: str) -> dict[str, Any]:
        try:
            payload = json.loads(self.run(*args, "-o", "json"))
        except json.JSONDecodeError as exc:
            raise KubectlError("kubectl returned invalid JSON") from exc
        if not isinstance(payload, dict):
            raise KubectlError("kubectl returned an unexpected JSON shape")
        return payload
