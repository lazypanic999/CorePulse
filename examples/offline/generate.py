"""Invented fixtures only: no Kubernetes, network, subscriber or real lab input."""
import argparse
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path


ROOT = Path(__file__).resolve().parent


def timestamp(value):
    return value.isoformat().replace('+00:00', 'Z')


def encoded(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'))


def fixtures():
    files, samples = {}, []
    epoch = datetime(2026, 11, 9, tzinfo=timezone.utc)
    for scenario_index, scenario in enumerate(('normal', 'amf', 'upf')):
        for repetition in range(1, 4):
            start = epoch + timedelta(hours=3 * scenario_index + repetition - 1)
            run_id = f'synthetic-{scenario}-{repetition}'
            record = {
                'example_only': 'SYNTHETIC — not lab evidence',
                'schema_version': 1, 'run_id': run_id,
                'fault': 'normal' if scenario == 'normal' else 'pod_crash',
                'target': '' if scenario == 'normal' else scenario,
                'context': 'open5gs-lab', 'namespace': 'open5gs',
                'started_at': timestamp(start), 'ended_at': timestamp(start + timedelta(minutes=1)),
                'status': 'verified' if scenario == 'normal' else 'recovered', 'quality': 'clean',
                'pre_protocol_check': {'checked_at': timestamp(start - timedelta(seconds=1)),
                                       'links': {'n2': 'up', 'n4': 'up'}, 'evidence': []},
                # UPF's first window has no probe. Missing is not zero replies.
                'service_probes': [{'checked_at': timestamp(start + timedelta(seconds=30 if scenario == 'upf' else 0)),
                                    'status': 'degraded' if scenario == 'upf' else 'healthy',
                                    'sent': 2, 'received': 0 if scenario == 'upf' else 2}],
            }
            files[f'run-records/{run_id}.json'] = json.dumps(record, indent=2, sort_keys=True, ensure_ascii=False) + '\n'
            for offset in (1, 5, 31, 35):
                at = start + timedelta(seconds=offset)
                measurement = {'timestamp': timestamp(at - timedelta(seconds=1)), 'window': '1m'}
                pods = [{'name': 'open5gs-amf-synthetic', 'cpu_millicores': 0,
                         'memory_bytes': 100, 'measurement': measurement}]
                if scenario != 'upf' or offset >= 30:
                    pods.append({'name': 'open5gs-upf-synthetic',
                                 'cpu_millicores': 20 if scenario == 'upf' else 0,
                                 'memory_bytes': 200 if scenario == 'upf' else 100,
                                 'measurement': measurement})
                samples.append(encoded({
                    'example_only': 'SYNTHETIC — not lab evidence', 'schema_version': 1,
                    'timestamp': timestamp(at), 'source': 'metrics.k8s.io/v1beta1',
                    'context': 'open5gs-lab', 'namespace': 'open5gs', 'pod_count': len(pods),
                    'cpu_millicores': sum(p['cpu_millicores'] for p in pods),
                    'memory_bytes': sum(p['memory_bytes'] for p in pods), 'pods': pods,
                }))
    files['telemetry.jsonl'] = '\n'.join(samples) + '\n'
    return {name: content.encode('utf-8') for name, content in files.items()}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='verify committed fixtures without writing')
    args = parser.parse_args()
    expected = fixtures()
    for name, content in expected.items():
        path = ROOT / name
        if args.check:
            if not path.is_file() or path.read_bytes() != content:
                raise SystemExit(f'Synthetic fixture differs from generator: {name}')
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
    actual = {f'run-records/{p.name}' for p in (ROOT / 'run-records').glob('*.json')}
    if actual != {name for name in expected if name.startswith('run-records/')}:
        raise SystemExit('Unexpected run records in synthetic fixture directory; inspect manually')
    print(f'Synthetic fixtures {"verified" if args.check else "generated"}: 9 invented runs, 36 invented captures')


if __name__ == '__main__':
    main()
