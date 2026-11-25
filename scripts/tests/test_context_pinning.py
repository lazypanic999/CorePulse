"""Execute lab helpers against a fake kubectl, never a Kubernetes cluster."""
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

FAKE_KUBECTL = r'''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
path=Path(os.environ['FAKE_KUBE_STATE'])
state=json.loads(path.read_text())
a=sys.argv[1:]
if a==['config','current-context']:
    print(state['context'])
    sys.exit(0)
if a[:2]!=['--context','open5gs-lab']:
    state['unsafe']=True
    path.write_text(json.dumps(state))
    sys.exit(9)
a=a[2:]
state['operations'].append(a)
lock=os.environ.get('COREPULSE_LOCK_DIR')
state.setdefault('locked', []).append(bool(lock) and os.path.isdir(lock))
action=a[0]
block=os.environ.get('FAKE_BLOCK_FILE')
if block and action=='scale' and '--replicas=0' in a and not state.get('blocked'):
    state['blocked']=True
    path.write_text(json.dumps(state))
    Path(block+'.reached').touch()
    import time
    for _ in range(500):
        if Path(block).exists(): break
        time.sleep(0.02)
    state=json.loads(path.read_text())
if action in ['apply','patch','scale','rollout','wait','exec'] and os.environ.get('FAKE_CONTEXT_DRIFT'):
    state['context']='production'
if action=='scale' and '--replicas=0' in a and os.environ.get('FAKE_SCALE_FAILURE') and not state.get('failed'):
    state['failed']=True
    path.write_text(json.dumps(state))
    sys.exit(1)
path.write_text(json.dumps(state))
if action=='get':
    resource=a[1]
    if any('jsonpath' in x for x in a): print('1')
    elif resource.startswith('deployment/'):
        name=resource.split('/')[1]
        print(json.dumps({'spec':{'replicas':1,'selector':{'matchLabels':{'app':name}},'template':{'spec':{'containers':[{'image':'docker.io/gradiant/open5gs:2.7.5'}],'initContainers':[{'name':'init','image':'docker.io/gradiant/open5gs-dbctl:0.10.3'}]}}}}))
    elif resource=='service/open5gs-upf-pfcp': print(json.dumps({'spec':{'selector':{'app':'open5gs-upf'}}}))
    elif resource=='configmap/open5gs-smf': print(json.dumps({'data':{'smf.yaml':'- address: open5gs-upf-pfcp'}}))
    elif resource=='services': print(json.dumps({'items':[]}))
    elif resource=='pods' and 'name' in a: print('pod/ueransim-ue-old')
    elif resource=='pods': print(json.dumps({'items':[{'metadata':{'name':'ueransim-ue-demo'},'status':{'conditions':[{'type':'Ready','status':'True'}]}}]}))
    else: print('ok')
elif action=='logs': print('Initial Registration is successful\nPDU Session establishment is successful')
elif action=='exec': print('2 packets transmitted, 2 received')
elif action=='apply':
    value=json.load(sys.stdin)
    assert value['metadata']['namespace']=='open5gs'
    print('applied')
else: print('ok')
'''

def build_corepulse(test_class):
    """Mutating helpers take ownership through the real CorePulse binary."""
    go = shutil.which('go')
    if not go:
        if os.environ.get('CI'):
            raise RuntimeError('Go is required to build corepulse for lab helper tests')
        raise unittest.SkipTest('Go is required to build corepulse for lab helper tests')
    test_class.build = tempfile.TemporaryDirectory(prefix='corepulse-bin-')
    test_class.binary = Path(test_class.build.name) / 'corepulse'
    subprocess.run([go, 'build', '-o', str(test_class.binary), './cmd/corepulse'], cwd=ROOT, check=True, timeout=300)


@unittest.skipUnless(os.name != 'nt' and shutil.which('bash'), 'lab helpers target Linux/WSL')
class ContextPinning(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        build_corepulse(cls)

    @classmethod
    def tearDownClass(cls):
        cls.build.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='corepulse-scripts-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.scripts = self.root / 'scripts'
        self.scripts.mkdir()
        source = Path(__file__).resolve().parents[1]
        for name in ['recover-service.sh', 'repair-populate.sh', 'configure-gnb-n3.sh', 'configure-pfcp.py', 'populate-subscriber.sh', 'gnb-dynamic-ip-patch.json', 'ownership.sh', 'corepulse_ownership.py']:
            shutil.copy2(source / name, self.scripts / name)
        tools = self.root / 'bin'
        tools.mkdir()
        kubectl = tools / 'kubectl'
        kubectl.write_text(FAKE_KUBECTL)
        kubectl.chmod(0o755)
        sleep = tools / 'sleep'
        sleep.write_text('#!/bin/sh\nexit 0\n')
        sleep.chmod(0o755)
        self.state = self.root / 'state.json'
        self.lock = self.root / 'lock'
        self.env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ['PATH'], FAKE_KUBE_STATE=str(self.state), FAKE_CONTEXT_DRIFT='1',
                        COREPULSE_BIN=str(self.binary), COREPULSE_LOCK_DIR=str(self.lock))
        for name in ['COREPULSE_OWNERSHIP_TOKEN', 'COREPULSE_OWNERSHIP_OPERATION']:
            self.env.pop(name, None)

    def run_script(self, name, context='open5gs-lab', fail_scale=False):
        self.state.write_text(json.dumps({'context': context, 'operations': [], 'unsafe': False}))
        env = dict(self.env)
        if fail_scale:
            env['FAKE_SCALE_FAILURE'] = '1'
        command = [sys.executable if name.endswith('.py') else 'bash', str(self.scripts / name)]
        result = subprocess.run(command, check=False, cwd=self.root, env=env, capture_output=True, text=True, timeout=20)
        return result, json.loads(self.state.read_text())

    def test_all_helpers_remain_pinned_after_ambient_drift(self):
        for name in ['configure-pfcp.py', 'repair-populate.sh', 'configure-gnb-n3.sh', 'recover-service.sh']:
            with self.subTest(script=name):
                result, state = self.run_script(name)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertTrue(state['operations'])
                self.assertEqual(state['context'], 'production')
                self.assertFalse(state['unsafe'])
                # Every lab call ran under shared ownership, released afterwards.
                self.assertTrue(all(state['locked']))
                self.assertFalse(self.lock.exists())

    def test_other_starting_context_never_operates(self):
        for name in ['configure-pfcp.py', 'repair-populate.sh', 'configure-gnb-n3.sh', 'recover-service.sh']:
            with self.subTest(script=name):
                result, state = self.run_script(name, context='production')
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(state['operations'], [])

    def test_error_restore_stays_in_lab_context(self):
        result, state = self.run_script('recover-service.sh', fail_scale=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(state['failed'])
        self.assertFalse(state['unsafe'])
        restores = [a for a in state['operations'] if a[0] == 'scale' and '--replicas=1' in a]
        self.assertEqual(len(restores), 4)
        self.assertTrue(all(state['locked']))
        self.assertFalse(self.lock.exists(), 'failed reset kept ownership')

    def hold_marker(self, operation):
        """Hold ownership from a separate CorePulse process until released."""
        release = self.root / (operation + '.release')
        started = self.root / (operation + '.started')
        env = dict(self.env)
        process = subprocess.Popen([str(self.binary), 'hold', '--operation', operation, '--', 'sh', '-c',
                                    f'touch {started}; while [ ! -f {release} ]; do sleep 0.02; done'], env=env)
        self.addCleanup(lambda: (release.touch(), process.wait(timeout=10)))
        for _ in range(500):
            if started.exists():
                return process, release
            time.sleep(0.02)
        self.fail('holder never started')

    def test_active_collector_blocks_every_helper_before_any_kubectl_call(self):
        self.hold_marker('baseline')
        for name in ['configure-pfcp.py', 'repair-populate.sh', 'configure-gnb-n3.sh', 'recover-service.sh']:
            with self.subTest(script=name):
                result, state = self.run_script(name)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('baseline', result.stderr)
                self.assertEqual(state['operations'], [])

    def test_running_helper_blocks_new_collection(self):
        self.state.write_text(json.dumps({'context': 'open5gs-lab', 'operations': [], 'unsafe': False}))
        block = self.root / 'unblock'
        env = dict(self.env, FAKE_BLOCK_FILE=str(block))
        helper = subprocess.Popen(['bash', str(self.scripts / 'recover-service.sh')], cwd=self.root, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.addCleanup(lambda: (block.touch(), helper.poll() is None and helper.kill(), helper.wait(timeout=10)))
        for _ in range(500):
            if Path(str(block) + '.reached').exists():
                break
            time.sleep(0.02)
        contender = subprocess.run([str(self.binary), 'hold', '--operation', 'baseline', '--', 'true'], check=False, env=self.env, capture_output=True, text=True, timeout=20)
        self.assertNotEqual(contender.returncode, 0)
        self.assertIn('recover-service', contender.stderr)
        block.touch()
        self.assertEqual(helper.wait(timeout=20), 0)
        self.assertFalse(self.lock.exists())

    def test_wrong_context_releases_ownership_without_operations(self):
        result, state = self.run_script('recover-service.sh', context='production')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(state['operations'], [])
        self.assertFalse(self.lock.exists())

    def test_interrupted_restoration_keeps_ownership_until_restored(self):
        self.state.write_text(json.dumps({'context': 'open5gs-lab', 'operations': [], 'unsafe': False}))
        block = self.root / 'unblock'
        env = dict(self.env, FAKE_BLOCK_FILE=str(block))
        helper = subprocess.Popen(['bash', str(self.scripts / 'recover-service.sh')], cwd=self.root, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.addCleanup(lambda: (block.touch(), helper.poll() is None and helper.kill(), helper.wait(timeout=10)))
        for _ in range(500):
            if Path(str(block) + '.reached').exists():
                break
            time.sleep(0.02)
        # bash exec'd into corepulse hold, so this is the ownership holder.
        helper.send_signal(2)
        block.touch()
        self.assertEqual(helper.wait(timeout=20), 130)
        state = json.loads(self.state.read_text())
        restores = [a for a in state['operations'] if a[0] == 'scale' and '--replicas=1' in a]
        self.assertGreaterEqual(len(restores), 4)
        self.assertTrue(all(state['locked']), 'a restore ran without ownership')
        self.assertFalse(state['unsafe'])
        self.assertFalse(self.lock.exists())

    def test_binary_without_hold_refuses_with_rebuild_hint(self):
        self.state.write_text(json.dumps({'context': 'open5gs-lab', 'operations': [], 'unsafe': False}))
        old = self.root / 'old-corepulse'
        old.write_text('#!/bin/sh\necho "corepulse: unknown command \\"$1\\"" >&2\nexit 1\n')
        old.chmod(0o755)
        env = dict(self.env, COREPULSE_BIN=str(old))
        for name in ['configure-pfcp.py', 'recover-service.sh']:
            with self.subTest(script=name):
                command = [sys.executable if name.endswith('.py') else 'bash', str(self.scripts / name)]
                result = subprocess.run(command, check=False, cwd=self.root, env=env, capture_output=True, text=True, timeout=20)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('no hold command', result.stderr)
                self.assertEqual(json.loads(self.state.read_text())['operations'], [])

    def test_missing_binary_refuses_without_operations(self):
        self.state.write_text(json.dumps({'context': 'open5gs-lab', 'operations': [], 'unsafe': False}))
        env = dict(self.env, COREPULSE_BIN=str(self.root / 'missing'))
        for name in ['configure-pfcp.py', 'recover-service.sh']:
            with self.subTest(script=name):
                command = [sys.executable if name.endswith('.py') else 'bash', str(self.scripts / name)]
                result = subprocess.run(command, check=False, cwd=self.root, env=env, capture_output=True, text=True, timeout=20)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('CorePulse binary', result.stderr)
                self.assertEqual(json.loads(self.state.read_text())['operations'], [])

if __name__ == '__main__':
    unittest.main()
