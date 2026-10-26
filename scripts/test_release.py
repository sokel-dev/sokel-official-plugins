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
    def release_entry(self, export, entry=None):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'demo'
            (source / 'docs').mkdir(parents=True)
            (source / 'docs/demo.md').write_text('# Demo usage\n')
            (source / 'locales').mkdir()
            (source / 'locales/en.json').write_text('{}\n')
            target = root / 'catalog/plugins/sokel/demo/manifest.yml'
            if entry is not None:
                target.parent.mkdir(parents=True)
                target.write_text(yaml.safe_dump(entry, sort_keys=False))
            export_file = root / 'export.yml'
            export_file.write_text(export)
            image = 'ghcr.io/sokel-dev/sokel-plugin-demo:0.3.0@sha256:' + 'a' * 64
            result = subprocess.run([
                sys.executable, str(ROOT / 'scripts/registry-entry.py'),
                '--plugin', 'demo', '--version', 'v0.3.0', '--image', image,
                '--export', str(export_file), '--catalog', str(root / 'catalog'),
            ], cwd=root, text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            text = target.read_text()
            return yaml.safe_load(text), text, image

    def test_first_release_preserves_export_deployment(self):
        deployment = {
            'targets': [
                {'kind': 'binary', 'ref': 'https://example.com/demo/{os}/{arch}'},
                {'kind': 'container', 'ref': 'ghcr.io/example/demo:0.3.0'},
                {'kind': 'container', 'ref': 'ghcr.io/example/demo-alternate:0.3.0'},
            ],
            'env': [{'name': 'SOKEL_DATA_DIR', 'default': '/data/demo', 'required': True,
                     'desc': 'Writable persistent data directory'}],
            'note': 'Use a persistent volume and one replica.\ndeployment: keep this literal note line.',
        }
        before = {
            'plugin': {'name': 'demo', 'label': 'Demo', 'doc': 'docs/demo.md'},
            'credential': {'fields': [{'name': 'secret', 'type': 'secret'}]},
        }
        after = {
            'operations': [{'id': 'send', 'label': 'Send'}],
            'events_common': [],
            'events': [{'id': 'action', 'fields': [{'name': 'event_id', 'type': 'string'}]}],
        }
        for style in ('block', 'flow', 'root_flow'):
            with self.subTest(style=style):
                if style == 'root_flow':
                    export = yaml.safe_dump({**before, 'deployment': deployment, **after},
                                            sort_keys=False, default_flow_style=True)
                else:
                    if style == 'flow':
                        block = 'deployment: ' + yaml.safe_dump(deployment, default_flow_style=True).strip() + '\n'
                    else:
                        block = yaml.safe_dump({'deployment': deployment}, sort_keys=False)
                    export = (yaml.safe_dump(before, sort_keys=False) + block
                              + yaml.safe_dump(after, sort_keys=False))
                entry, text, image = self.release_entry(export)
                expected = json.loads(json.dumps(deployment))
                expected['targets'][1]['ref'] = image
                self.assertEqual(entry['deployment'], expected,
                                 'first release must keep env, note and other deployment targets')
                self.assertEqual(entry['plugin']['org'], 'sokel')
                self.assertEqual(entry['plugin']['version'], 'v0.3.0')
                self.assertEqual(entry['credential'], before['credential'])
                for key, value in after.items():
                    self.assertEqual(entry[key], value, f'{key} was lost while rewriting deployment')
                keys = [key.value for key, _ in yaml.compose(text).value]
                self.assertEqual(keys.count('deployment'), 1, 'duplicate YAML keys hide deployment settings')
                self.assertEqual(keys.count('plugin'), 1)

    def test_release_keeps_catalog_deployment_and_removes_export_block(self):
        deployment = {
            'targets': [{'kind': 'binary', 'ref': 'https://example.com/catalog-demo'},
                        {'kind': 'container', 'ref': 'ghcr.io/example/old:0.2.0'}],
            'env': [{'name': 'SOKEL_DATA_DIR', 'default': '/catalog/data', 'required': True}],
            'note': 'Catalog-maintained placement constraint',
        }
        old = {
            'plugin': {'name': 'demo', 'org': 'sokel', 'version': 'v0.2.0', 'doc': 'README.md'},
            'deployment': deployment,
            'operations': [],
        }
        export = {
            'plugin': {'name': 'demo', 'doc': 'docs/demo.md'},
            'deployment': {'targets': [{'kind': 'container', 'ref': 'source-image:0.3.0'}],
                           'env': [{'name': 'SOURCE_ONLY'}], 'note': 'Source note'},
            'operations': [{'id': 'send', 'label': 'New send'}],
            'events': [{'id': 'action'}],
        }
        for flow in (False, True):
            with self.subTest(flow=flow):
                entry, text, image = self.release_entry(
                    yaml.safe_dump(export, sort_keys=False, default_flow_style=flow), old)
                expected = json.loads(json.dumps(deployment))
                expected['targets'][1]['ref'] = image
                self.assertEqual(entry['deployment'], expected)
                self.assertEqual(entry['operations'], export['operations'])
                self.assertEqual(entry['events'], export['events'])
                keys = [key.value for key, _ in yaml.compose(text).value]
                self.assertEqual(keys.count('deployment'), 1, 'export and catalog deployment must not coexist')

    def test_first_release_adds_container_without_dropping_binary(self):
        deployment = {'targets': [{'kind': 'binary', 'ref': 'https://example.com/demo'}],
                      'env': [{'name': 'SOKEL_DATA_DIR', 'default': '/data'}], 'note': 'One replica'}
        export = {'plugin': {'name': 'demo', 'doc': 'docs/demo.md'},
                  'deployment': deployment, 'operations': []}
        entry, _, image = self.release_entry(yaml.safe_dump(export, sort_keys=False))
        self.assertEqual(entry['deployment']['targets'], deployment['targets'] + [
            {'kind': 'container', 'ref': image}])
        self.assertEqual(entry['deployment']['env'], deployment['env'])
        self.assertEqual(entry['deployment']['note'], deployment['note'])

    def test_release_refreshes_document_and_removes_obsolete_translations(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'youtube'
            (source / 'docs').mkdir(parents=True)
            (source / 'docs/youtube.md').write_text('# New usage\n')
            (source / 'locales').mkdir()
            (source / 'locales/en.json').write_text(json.dumps({'# New usage\n': '# English usage\n', 'New health': 'New check', 'Source-only description': 'New source summary'}))
            target = root / 'catalog/plugins/sokel/youtube'
            (target / 'locales').mkdir(parents=True)
            (target / 'README.md').write_text('# Old usage\n')
            old = {'plugin': {'name': 'youtube', 'org': 'sokel', 'version': 'v0.2.1', 'doc': 'README.md', 'label': 'Catalog label'},
                   'operations': [{'id': 'health_check', 'label': 'Old health'}]}
            (target / 'manifest.yml').write_text(yaml.safe_dump(old))
            (target / 'locales/en.json').write_text(json.dumps({'# Old usage\n': '# Old translated usage\n', 'Old health': 'Old check', 'Catalog label': 'Preserved label'}))
            export = root / 'export.yml'
            export.write_text(yaml.safe_dump({'plugin': {'name': 'youtube', 'doc': 'docs/youtube.md', 'desc': 'Source-only description'}, 'operations': [{'id': 'health_check', 'label': 'New health'}]}))
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
