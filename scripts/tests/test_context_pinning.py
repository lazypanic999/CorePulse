"""Execute lab helpers against a fake kubectl, never a Kubernetes cluster."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

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
action=a[0]
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

@unittest.skipUnless(os.name != 'nt' and shutil.which('bash'), 'lab helpers target Linux/WSL')
class ContextPinning(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='corepulse-scripts-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.scripts = self.root / 'scripts'
        self.scripts.mkdir()
        source = Path(__file__).resolve().parents[1]
        for name in ['recover-service.sh', 'repair-populate.sh', 'configure-gnb-n3.sh', 'configure-pfcp.py', 'populate-subscriber.sh', 'gnb-dynamic-ip-patch.json']:
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
        self.env = dict(os.environ, PATH=str(tools) + os.pathsep + os.environ['PATH'], FAKE_KUBE_STATE=str(self.state), FAKE_CONTEXT_DRIFT='1')

    def run_script(self, name, context='open5gs-lab', fail_scale=False):
        self.state.write_text(json.dumps({'context': context, 'operations': [], 'unsafe': False}))
        env = dict(self.env)
        if fail_scale:
            env['FAKE_SCALE_FAILURE'] = '1'
        command = [sys.executable if name.endswith('.py') else 'bash', str(self.scripts / name)]
        result = subprocess.run(command, cwd=self.root, env=env, capture_output=True, text=True, timeout=20)
        return result, json.loads(self.state.read_text())

    def test_all_helpers_remain_pinned_after_ambient_drift(self):
        for name in ['configure-pfcp.py', 'repair-populate.sh', 'configure-gnb-n3.sh', 'recover-service.sh']:
            with self.subTest(script=name):
                result, state = self.run_script(name)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertTrue(state['operations'])
                self.assertEqual(state['context'], 'production')
                self.assertFalse(state['unsafe'])

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

if __name__ == '__main__':
    unittest.main()
