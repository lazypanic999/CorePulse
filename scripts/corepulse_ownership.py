"""Shared collection ownership for mutating Python lab helpers.

Call ``hold(operation)`` before the first Kubernetes mutation. Outside
ownership the helper re-executes itself under ``corepulse hold``, which takes
the same marker as CLI and dashboard collection and refuses while another
owner holds it. Inside ownership it confirms that the marker carries its token.
There is no bypass: without a CorePulse binary the helper refuses.
"""
import os
import shutil
import subprocess
import sys
from pathlib import Path


def binary():
    configured = os.environ.get("COREPULSE_BIN")
    if configured:
        return configured
    found = shutil.which("corepulse")
    if found:
        return found
    local = Path(__file__).resolve().parents[1] / "corepulse"
    return str(local) if local.is_file() and os.access(local, os.X_OK) else ""


def hold(operation):
    path = binary()
    if not path or not os.access(path, os.X_OK):
        raise SystemExit(
            "This helper needs a CorePulse binary to take shared collection ownership. "
            "Build one with `go build -o corepulse ./cmd/corepulse`, or set COREPULSE_BIN."
        )
    probe = subprocess.run([path, "hold"], check=False, capture_output=True, text=True)
    if "unknown command" in probe.stderr:
        raise SystemExit(f"{path} has no hold command; rebuild with `go build -o corepulse ./cmd/corepulse` or set COREPULSE_BIN.")
    if not os.environ.get("COREPULSE_OWNERSHIP_TOKEN"):
        script = os.path.abspath(sys.argv[0])
        os.execv(path, [path, "hold", "--operation", operation, "--", sys.executable, script, *sys.argv[1:]])
    if subprocess.run([path, "ownership-check"], check=False).returncode != 0:
        raise SystemExit("Shared collection ownership is not confirmed for this helper; refusing to change the lab.")
