"""Append one reset attempt to the local reset log (called by recover-service.sh).

The record describes what the reset script observed. It is written after the
script finished and never changes the reset's exit code. Missing values stay
null rather than being invented.
"""
import json
import os
import re
import socket
from pathlib import Path

SESSION = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$")
OUTCOMES = {0: "service_verified", 2: "service_unverified", 129: "interrupted", 130: "interrupted", 143: "interrupted"}
PHASES = {"preflight", "scaling_down", "scaling_up", "service_check"}


def build(env):
    code = int(env["RESET_CODE"])
    phase = env.get("RESET_PHASE", "")
    record = {
        "schema_version": 1,
        "operation": "recover-service",
        "started_at": env.get("RESET_STARTED") or None,
        "ended_at": env.get("RESET_ENDED") or None,
        "exit_code": code,
        "outcome": OUTCOMES.get(code, "failed"),
        "last_phase": phase if phase in PHASES else None,
        # Workloads the script may have scaled. Empty when it stopped during
        # preflight; otherwise last_phase says how far it got.
        "workloads_in_scope": ["open5gs-upf", "open5gs-smf", "ueransim-gnb", "ueransim-ue"] if phase != "preflight" else [],
        "healthy_packet_checks": int(env.get("RESET_HEALTHY") or 0),
        "host": socket.gethostname(),
        "ownership_operation": env.get("COREPULSE_OWNERSHIP_OPERATION") or None,
        "session_id": None,
    }
    session = env.get("COREPULSE_SESSION_ID", "")
    if session:
        if SESSION.match(session):
            record["session_id"] = session
        else:
            record["session_error"] = "COREPULSE_SESSION_ID is not a valid session ID; not recorded"
    return record


def main():
    env = os.environ
    path = Path(env["RESET_LOG"])
    path.parent.mkdir(parents=True, exist_ok=True)
    line = json.dumps(build(env), sort_keys=True) + "\n"
    # One write on an O_APPEND descriptor keeps lines whole.
    fd = os.open(path, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o600)
    try:
        os.write(fd, line.encode())
    finally:
        os.close(fd)


if __name__ == "__main__":
    main()
