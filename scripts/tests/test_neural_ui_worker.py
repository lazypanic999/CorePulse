import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('ui_worker', ROOT / 'scripts/neural-ui-worker.py')
worker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(worker)


class NeuralUIWorkerTests(unittest.TestCase):
    def test_source_directories_and_shared_output_root_are_rejected(self):
        for path in [ROOT, ROOT / 'ml', ROOT / 'reports/neural-ui']:
            with self.assertRaises(ValueError):
                worker.configuration(path)

    def test_configuration_keeps_synthetic_flag_seeds_and_fixed_methods(self):
        text = worker.configuration(ROOT / 'reports/neural-ui/job-fixture')
        self.assertIn('synthetic: true', text)
        self.assertIn('seeds: [7, 23]', text)
        self.assertIn('reports/neural-ui/job-fixture/windows.csv', text)
        self.assertIn('kind: mlp', text)
        self.assertIn('kind: autoencoder', text)

    def test_progress_file_replaced_with_complete_json(self):
        import json
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            worker.progress(path, 'Preparing')
            worker.progress(path, 'Training')
            self.assertEqual(json.loads((path / 'progress.json').read_text()), {'phase': 'Training'})
            self.assertEqual(list(path.glob('.progress-*')), [])
