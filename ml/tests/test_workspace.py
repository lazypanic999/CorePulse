import importlib
import socket
import subprocess

import torch


def test_cpu_synthetic_smoke_has_no_network_or_process_dependencies(monkeypatch):
    def forbidden(*args, **kwargs):
        raise AssertionError("synthetic ML smoke must not use network or subprocesses")

    monkeypatch.setattr(socket, "create_connection", forbidden)
    monkeypatch.setattr(subprocess, "Popen", forbidden)
    module = importlib.import_module("corepulse_ml.smoke")
    assert module.smoke() == {"synthetic": True, "device": "cpu", "row_sums": [1.0, 1.0]}
    assert torch.version.cuda is None
