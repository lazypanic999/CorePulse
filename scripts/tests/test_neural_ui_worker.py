import importlib.util
import tempfile
import unittest
import unittest.mock
from pathlib import Path

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
        self.assertNotIn(worker.SMOKE_OUTPUT, text)

    def test_template_without_expected_output_paths_is_rejected(self):
        original = Path.read_text
        renamed = lambda path, *a, **k: original(path, *a, **k).replace(
            worker.SMOKE_OUTPUT, 'reports/elsewhere/')
        with unittest.mock.patch.object(Path, 'read_text', renamed), self.assertRaises(ValueError):
            worker.configuration(ROOT / 'reports/neural-ui/job-fixture')

    def test_progress_file_replaced_with_complete_json(self):
        import json
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            worker.progress(path, 'Preparing')
            worker.progress(path, 'Training')
            self.assertEqual(json.loads((path / 'progress.json').read_text()), {'phase': 'Training'})
            self.assertEqual(list(path.glob('.progress-*')), [])
