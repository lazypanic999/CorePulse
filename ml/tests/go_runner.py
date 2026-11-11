"""Invoke an actual cgo-free Go test; never substitute or silently skip parity."""

import os
import shutil
import subprocess
from pathlib import Path

import pytest


def run_go_fixture(test_name, variables, marker):
    env = os.environ.copy()
    env.update(CGO_ENABLED="0", GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local", **variables)
    binary = env.get("COREPULSE_GO_PARITY_BINARY")
    if binary:
        command = [binary, f"-test.run=^{test_name}$", "-test.v", "-test.count=1"]
    else:
        go = shutil.which("go")
        if not go:
            pytest.fail("Go is required for parity; install Go or set COREPULSE_GO_PARITY_BINARY")
        command = [go, "test", "./internal/inference", "-run", f"^{test_name}$", "-v", "-count=1"]
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
    assert marker in completed.stdout, completed.stdout + completed.stderr
