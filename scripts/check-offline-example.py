"""Exercise the documented synthetic pipeline with a built binary, no cluster."""
import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parent.parent


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True, help='built CorePulse binary')
    args = parser.parse_args()
    binary = Path(args.binary).resolve()
    subprocess.run([sys.executable, str(ROOT / 'examples/offline/generate.py'), '--check'], check=True)
    with tempfile.TemporaryDirectory(prefix='corepulse-synthetic-') as directory:
        temp = Path(directory)
        windows, split, report = temp / 'windows.csv', temp / 'split.json', temp / 'evaluation.json'
        env = dict(os.environ, PATH=str(temp / 'no-executables'))

        def run(*arguments):
            completed = subprocess.run([str(binary), *map(str, arguments)], cwd=ROOT,
                env=env, capture_output=True, text=True, timeout=30)
            if completed.returncode:
                raise RuntimeError(f'Offline command failed: {arguments}\n{completed.stderr}')

        run('windows', '--telemetry', 'examples/offline/telemetry.jsonl', '--runs-dir',
            'examples/offline/run-records', '--output', windows, '--window', '30s')
        run('split', '--input', windows, '--output', split, '--seed', '42', '--embargo', '2m')
        run('evaluate', '--input', windows, '--split', split, '--output', report,
            '--z-threshold', '3', '--synthetic')
        with windows.open(newline='', encoding='utf-8') as stream:
            rows = list(csv.DictReader(stream))
        result = json.loads(report.read_bytes())
        assert len(rows) == 18
        assert result['synthetic'] is True and result['limitations'][0].startswith('SYNTHETIC EXAMPLE')
        assert result['windows_sha256'] == hashlib.sha256(windows.read_bytes()).hexdigest()
        assert result['train_windows'] == 12 and result['test_windows'] == 6
        assert set(result['split']['train_runs']).isdisjoint(result['split']['test_runs'])
        assert any(r['upf_cpu_millicores_mean'] == '' and r['upf_cpu_millicores_missing_ratio'] == '1' for r in rows)
        assert any(r['upf_cpu_millicores_mean'] == '0' and r['upf_cpu_millicores_missing_ratio'] == '0' for r in rows)
        assert set(result['models']) == {'nearest_centroid', 'majority_scenario', 'normal_zscore', 'probe_rule'}
        assert result['models']['probe_rule']['window_metrics']['abstained'] > 0
        assert any(p['predicted'] and p['predicted'] != p['truth']
                   for m in result['models'].values() for p in m['predictions'])
        session_split, session_report = temp / 'session-split.json', temp / 'session-evaluation.json'
        run('split', '--input', windows, '--output', session_split, '--seed', '42', '--embargo', '2m', '--group-by', 'session')
        run('evaluate', '--input', windows, '--split', session_split, '--output', session_report, '--synthetic')
        grouped = json.loads(session_report.read_bytes())
        policy = grouped['split']
        assert grouped['schema_version'] == 2 and policy['schema_version'] == 2 and policy['grouping'] == 'session'
        assert grouped['synthetic'] and grouped['train_windows'] == 12 and grouped['test_windows'] == 6
        assert len(policy['train_sessions']) == 2 and len(policy['test_sessions']) == 1
        assert set(policy['train_sessions']).isdisjoint(policy['test_sessions'])
        for role in ('train', 'test'):
            assert {policy['session_assignments'][identity] for identity in policy[role + '_runs']} == set(policy[role + '_sessions'])
        assert all(c['train_runs'] == 2 and c['test_runs'] == 1 for c in policy['classes'].values())
    print('Synthetic pipeline passed: windows -> run/session holdouts -> explicitly synthetic evaluation; mistakes and abstentions retained')


if __name__ == '__main__':
    main()
