"""Safety boundary for all ML tests: offline fixtures and CPU calculations only."""

import os
import socket
import subprocess
from pathlib import Path

import pytest


@pytest.fixture(autouse=True)
def offline_only(monkeypatch):
    original = subprocess.Popen

    def no_connection(*args, **kwargs):
        raise AssertionError("ML tests must not open network connections")

    def guarded_process(args, *positional, **kwargs):
        if isinstance(args, (str, bytes)) or kwargs.get("shell"):
            raise AssertionError("ML tests require explicit offline subprocess argument lists")
        words = [os.fspath(word) for word in args]
        executable = Path(words[0]).name.lower()
        if executable.startswith("kubectl") or "--apply" in words:
            raise AssertionError("cluster mutation/CLI is forbidden in ML tests")
        if len(words) > 1 and words[1] in {
            "serve", "baseline", "pod-crash", "doctor", "signals", "verify-service",
        }:
            raise AssertionError("live CorePulse commands are forbidden in ML tests")
        return original(args, *positional, **kwargs)

    monkeypatch.setattr(socket.socket, "connect", no_connection)
    monkeypatch.setattr(socket, "create_connection", no_connection)
    monkeypatch.setattr(subprocess, "Popen", guarded_process)
