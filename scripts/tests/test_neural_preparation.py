"""Test preparation argument safety; these tests never execute a cluster CLI."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location('neural_prepare', Path(__file__).parents[1] / 'prepare-neural-synthetic.py')
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class NeuralPreparation(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        examples = self.root / 'examples/offline'
        (examples / 'run-records').mkdir(parents=True)
        (examples / 'run-records/invented.json').write_text(json.dumps({'example_only': MODULE.EXAMPLE_MARKER}))
        (examples / 'telemetry.jsonl').write_text(json.dumps({'example_only': MODULE.EXAMPLE_MARKER}) + '\n')

    def test_only_offline_commands_with_empty_helper_path(self):
        with patch.object(MODULE, 'ROOT', self.root), patch.object(MODULE.subprocess, 'run') as run:
            MODULE.prepare(self.root / 'corepulse', self.root / 'reports/smoke')
        self.assertEqual([call.args[0][1] for call in run.call_args_list], ['windows', 'split', 'evaluate'])
        for call in run.call_args_list:
            self.assertEqual(call.kwargs['env']['PATH'], '')
            self.assertNotIn('--apply', call.args[0])
        self.assertIn('--synthetic', run.call_args_list[-1].args[0])

    def test_missing_example_marker_refuses_before_any_process(self):
        (self.root / 'examples/offline/telemetry.jsonl').write_text('{}\n')
        with patch.object(MODULE, 'ROOT', self.root), patch.object(MODULE.subprocess, 'run') as run:
            with self.assertRaises(ValueError):
                MODULE.prepare(self.root / 'corepulse', self.root / 'reports/smoke')
        run.assert_not_called()

    def test_source_output_refuses_before_any_process(self):
        with patch.object(MODULE, 'ROOT', self.root), patch.object(MODULE.subprocess, 'run') as run:
            with self.assertRaises(ValueError):
                MODULE.prepare(self.root / 'corepulse', self.root / 'ml')
        run.assert_not_called()
