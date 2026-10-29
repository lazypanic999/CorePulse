"""Command-line entry point for discovery and the first lab fault."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .discovery import analyze_snapshot, discover
from .faults import SafetyError, plan_pod_crash, run_pod_crash
from .kubectl import Kubectl, KubectlError


def _print_json(value: dict) -> None:
    print(json.dumps(value, indent=2, sort_keys=True))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="corepulse")
    commands = parser.add_subparsers(dest="command", required=True)
    discovery = commands.add_parser("discover", help="Read-only lab inventory")
    discovery.add_argument("--snapshot", type=Path, help="Analyze a saved Kubernetes List")
    discovery.add_argument("--output", type=Path, help="Save the JSON report")

    fault = commands.add_parser("pod-crash", help="Guarded AMF/UPF pod restart experiment")
    fault.add_argument("--target", required=True, choices=["amf", "upf"])
    fault.add_argument("--apply", action="store_true", help="Actually delete one lab pod")
    fault.add_argument("--runs-dir", type=Path, default=Path("runs"))

    args = parser.parse_args(argv)
    client = Kubectl()
    try:
        if args.command == "discover":
            if args.snapshot:
                snapshot = json.loads(args.snapshot.read_text(encoding="utf-8"))
                result = analyze_snapshot(snapshot, "offline-snapshot")
            else:
                result = discover(client)
            if args.output:
                args.output.parent.mkdir(parents=True, exist_ok=True)
                args.output.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
            _print_json(result)
            return 0

        if not args.apply:
            _print_json({"mode": "dry-run", **plan_pod_crash(args.target)})
            return 0
        result = run_pod_crash(client, args.target)
        args.runs_dir.mkdir(parents=True, exist_ok=True)
        path = args.runs_dir / f"{result['run_id']}.json"
        path.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
        _print_json({**result, "run_record": str(path)})
        return 0 if result["status"] == "recovered" else 2
    except (SafetyError, KubectlError, OSError, ValueError, json.JSONDecodeError) as exc:
        print(f"corepulse: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
