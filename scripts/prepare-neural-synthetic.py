#!/usr/bin/env python3
"""Prepare invented neural smoke inputs with offline CLI commands only."""
import argparse
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parent.parent
EXAMPLE_MARKER = 'SYNTHETIC — not lab evidence'


def prepare(binary, output_dir):
    binary, output = Path(binary).resolve(), Path(output_dir).resolve()
    if output.is_relative_to(ROOT) and not output.is_relative_to(ROOT / 'reports'):
        raise ValueError('Generated reports must not be written into tracked source directories')
    examples = ROOT / 'examples/offline'
    records = sorted((examples / 'run-records').glob('*.json'))
    if not records:
        raise ValueError('No invented run records found')
    for path in records:
        if json.loads(path.read_text(encoding='utf-8')).get('example_only') != EXAMPLE_MARKER:
            raise ValueError('Refusing a run without the invented-example marker')
    lines = (examples / 'telemetry.jsonl').read_text().splitlines()
    if not lines or any(json.loads(line).get('example_only') != EXAMPLE_MARKER for line in lines):
        raise ValueError('Refusing telemetry without invented-example markers')
    output.mkdir(parents=True, exist_ok=True)
    windows, split, references = (output / name for name in
                                  ('windows.csv', 'split.json', 'references.json'))
    env = os.environ.copy()
    # The absolute offline binary needs no helper executables. Even an unintended
    # cluster CLI lookup cannot resolve a real executable in these child processes.
    env['PATH'] = ''
    commands = [
        ['windows', '--telemetry', str(examples / 'telemetry.jsonl'), '--runs-dir',
         str(examples / 'run-records'), '--output', str(windows), '--window', '30s'],
        ['split', '--input', str(windows), '--output', str(split), '--group-by',
         'session', '--seed', '42', '--test-fraction', '0.25', '--embargo', '2m'],
        ['evaluate', '--input', str(windows), '--split', str(split), '--output',
         str(references), '--z-threshold', '3', '--synthetic'],
    ]
    for command in commands:
        subprocess.run([str(binary), *command], cwd=ROOT, env=env, check=True,
                       stdout=subprocess.DEVNULL)
    return references


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--output-dir', type=Path, default=ROOT / 'reports/neural-synthetic')
    args = parser.parse_args()
    references = prepare(args.binary, args.output_dir)
    print(f'SYNTHETIC ONLY: offline neural inputs prepared; references: {references}')


if __name__ == '__main__':
    main()
