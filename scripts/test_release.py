"""Release behavior shared by Go and manifest-based Python plugins."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1]


class ReleaseTest(unittest.TestCase):
    def test_release_refreshes_document_and_removes_obsolete_translations(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'youtube'
            (source / 'docs').mkdir(parents=True)
            (source / 'docs/youtube.md').write_text('# New usage\n')
            (source / 'locales').mkdir()
            (source / 'locales/en.json').write_text(json.dumps({'# New usage\n': '# English usage\n', 'New health': 'New check'}))
            target = root / 'catalog/plugins/sokel/youtube'
            (target / 'locales').mkdir(parents=True)
            (target / 'README.md').write_text('# Old usage\n')
            old = {'plugin': {'name': 'youtube', 'org': 'sokel', 'version': 'v0.2.1', 'doc': 'README.md', 'label': 'Catalog label'},
                   'operations': [{'id': 'health_check', 'label': 'Old health'}]}
            (target / 'manifest.yml').write_text(yaml.safe_dump(old))
            (target / 'locales/en.json').write_text(json.dumps({'# Old usage\n': '# Old translated usage\n', 'Old health': 'Old check', 'Catalog label': 'Preserved label'}))
            export = root / 'export.yml'
            export.write_text(yaml.safe_dump({'plugin': {'name': 'youtube', 'doc': 'docs/youtube.md'}, 'operations': [{'id': 'health_check', 'label': 'New health'}]}))
            command = [sys.executable, str(ROOT / 'scripts/registry-entry.py'), '--plugin', 'youtube', '--version', 'v0.3.1',
                       '--image', 'ghcr.io/sokel-dev/sokel-plugin-youtube:0.3.1@sha256:' + 'a' * 64,
                       '--export', str(export), '--catalog', str(root / 'catalog')]
            result = subprocess.run(command, cwd=root, text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual((target / 'README.md').read_text(), '# New usage\n')
            translations = json.loads((target / 'locales/en.json').read_text())
            self.assertEqual(translations, {'Catalog label': 'Preserved label', '# New usage\n': '# English usage\n', 'New health': 'New check'})

    def test_images_discover_python_plugin(self):
        workflow = yaml.safe_load((ROOT / '.github/workflows/images.yml').read_text())
        command = next(s['run'] for s in workflow['jobs']['list']['steps'] if s.get('id') == 'l')
        with tempfile.TemporaryDirectory() as tmp:
            base = Path(tmp)
            (base / 'youtube').mkdir()
            (base / 'youtube/main.py').write_text('')
            (base / 'youtube/manifest.yml').write_text('plugin: {name: youtube}\ncodegen: [{lang: python}]\n')
            (base / 'youtube/Dockerfile').write_text('FROM python:3.12-slim\n')
            (base / 'feed').mkdir()
            (base / 'feed/go.mod').write_text('module example.invalid/feed\n')
            (base / 'feed/main.go').write_text('package main\n')
            (base / 'scripts').symlink_to(ROOT / 'scripts', target_is_directory=True)
            output = base / 'output'
            result = subprocess.run(['bash', '-e', '-c', command], cwd=base,
                                    env={**os.environ, 'GITHUB_OUTPUT': str(output)}, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            matrix = json.loads(output.read_text().strip().split('=', 1)[1])
            entries = matrix.get('include', []) if isinstance(matrix, dict) else matrix
            names = [e['plugin'] if isinstance(e, dict) else e for e in entries]
            self.assertIn('youtube', names, 'Image workflow silently skips Python plugins')
            self.assertIn('feed', names)
            by_name = {entry['plugin']: entry for entry in entries}
            self.assertEqual(by_name['youtube'], dict(plugin='youtube', runtime='python', context='youtube', dockerfile='youtube/Dockerfile'))
            self.assertEqual(by_name['feed'], dict(plugin='feed', runtime='go', context='.', dockerfile='Dockerfile'))
            for name in ('youtube', 'feed'):
                result = subprocess.run([sys.executable, str(ROOT / 'scripts/plugin-matrix.py'), '--plugin', name],
                                        cwd=base, text=True, capture_output=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn('runtime=' + by_name[name]['runtime'], result.stdout)
            missing = subprocess.run([sys.executable, str(ROOT / 'scripts/plugin-matrix.py'), '--plugin', '../missing'],
                                     cwd=base, text=True, capture_output=True)
            self.assertNotEqual(missing.returncode, 0)
            (base / 'youtube/Dockerfile').unlink()
            invalid = subprocess.run([sys.executable, str(ROOT / 'scripts/plugin-matrix.py')],
                                     cwd=base, text=True, capture_output=True)
            self.assertNotEqual(invalid.returncode, 0)
            self.assertIn('requires its own Dockerfile', invalid.stderr)
            failed_discovery = subprocess.run(['bash', '-e', '-c', command], cwd=base,
                                              env={**os.environ, 'GITHUB_OUTPUT': str(output)}, capture_output=True)
            self.assertNotEqual(failed_discovery.returncode, 0)

    def test_first_release_creates_catalog_entry_from_export_and_assets(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            assets = root / 'youtube'
            (assets / 'docs').mkdir(parents=True)
            (assets / 'docs/youtube.md').write_text('# Video plugin\n')
            (assets / 'locales').mkdir()
            (assets / 'locales/en.json').write_text('{"视频": "Video"}\n')
            export = root / 'export.yml'
            export.write_text('plugin:\n  name: youtube\n  label: 视频\n  version: 0.2.0\n  doc: docs/youtube.md\noperations: []\n')
            command = [sys.executable, str(ROOT / 'scripts/registry-entry.py'), '--plugin', 'youtube',
                       '--version', 'v0.2.0', '--image', 'ghcr.io/sokel-dev/sokel-plugin-youtube:0.2.0@sha256:' + 'a' * 64,
                       '--export', str(export), '--catalog', str(root / 'catalog')]
            result = subprocess.run(command, cwd=root, text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            entry_dir = root / 'catalog/plugins/sokel/youtube'
            entry = yaml.safe_load((entry_dir / 'manifest.yml').read_text())
            self.assertEqual(entry['plugin']['org'], 'sokel')
            self.assertEqual(entry['plugin']['version'], 'v0.2.0')
            self.assertEqual(entry['plugin']['doc'], 'README.md')
            self.assertEqual((entry_dir / 'README.md').read_text(), '# Video plugin\n')
            self.assertEqual(json.loads((entry_dir / 'locales/en.json').read_text()), {'视频': 'Video'})
            self.assertEqual(entry['deployment']['targets'][0]['ref'], command[command.index('--image') + 1])
            second = subprocess.run(command, cwd=root, text=True, capture_output=True)
            self.assertEqual(second.returncode, 2, 'Releasing the same version must still fail')
            command[command.index('--version') + 1] = 'v0.3.0'
            command[command.index('--image') + 1] = 'ghcr.io/sokel-dev/sokel-plugin-youtube:0.3.0@sha256:' + 'b' * 64
            (assets / 'locales/en.json').write_text('{"视频": "Video", "# Video plugin\\n": "# Usage guide\\n"}\n')
            (entry_dir / 'locales/en.json').write_text('{"视频": "Video", "Catalog-only": "Keep this"}\n')
            upgraded = subprocess.run(command, cwd=root, text=True, capture_output=True)
            self.assertEqual(upgraded.returncode, 0, upgraded.stderr)
            updated = yaml.safe_load((entry_dir / 'manifest.yml').read_text())
            self.assertEqual(updated['plugin']['version'], 'v0.3.0')
            self.assertEqual(updated['deployment']['targets'][0]['ref'], command[command.index('--image') + 1])
            self.assertEqual(updated['plugin']['label'], entry['plugin']['label'])
            translations = json.loads((entry_dir / 'locales/en.json').read_text())
            self.assertEqual(translations['Catalog-only'], 'Keep this')
            self.assertEqual(translations.get('# Video plugin\n'), '# Usage guide\n')
            self.assertEqual((entry_dir / 'README.md').read_text(), '# Video plugin\n')


if __name__ == '__main__':
    unittest.main()
