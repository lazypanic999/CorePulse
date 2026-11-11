#!/usr/bin/env python3
"""Fixed invented inputs only; used by the optional dashboard neural worker."""
import argparse
import importlib.util
import json
import os
import sys
from pathlib import Path
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def progress(directory, phase):
    fd, filename = tempfile.mkstemp(dir=directory, prefix='.progress-')
    with os.fdopen(fd, 'w', encoding='utf-8') as stream:
        json.dump({'phase': phase}, stream)
    os.replace(filename, directory / 'progress.json')


def configuration(directory):
    directory = directory.resolve()
    parent = ROOT / 'reports/neural-ui'
    if not directory.is_relative_to(parent.resolve()) or directory == parent.resolve():
        raise ValueError('Neural jobs must use a dedicated ignored job directory')
    relative = directory.relative_to(ROOT).as_posix()
    source = (ROOT / 'ml/experiments/synthetic-smoke.yaml').read_text(encoding='utf-8')
    return source.replace('reports/neural-synthetic/', relative + '/')


def main():
    if sys.version_info[:2] != (3, 12):
        raise RuntimeError('The neural worker requires the locked Python 3.12 environment')
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, type=Path)
    parser.add_argument('--output-dir', required=True, type=Path)
    args = parser.parse_args()
    directory = args.output_dir.resolve()
    config = configuration(directory)
    directory.mkdir(parents=True, exist_ok=True)
    progress(directory, 'Building fixture windows, whole-session split and four references')
    spec = importlib.util.spec_from_file_location('synthetic_prepare', ROOT / 'scripts/prepare-neural-synthetic.py')
    prepare = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(prepare)
    prepare.prepare(args.binary, directory)
    configuration_path = directory / 'experiment.yaml'
    configuration_path.write_text(config, encoding='utf-8')
    progress(directory, 'Training CPU MLP and normal-only autoencoder; retaining every seed')
    from corepulse_ml.experiment import run_experiment
    run_experiment(configuration_path, export_dir=directory / 'models')
    progress(directory, 'Freezing CPU reference outputs for Go validation')
    from corepulse_ml.ui_parity import write_parity
    write_parity(directory)
    progress(directory, 'Checking exported artifacts and predictions in Go')
    print('SYNTHETIC ONLY: neural UI training and export complete', flush=True)


if __name__ == '__main__':
    main()
