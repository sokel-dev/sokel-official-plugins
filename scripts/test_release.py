"""Release behavior shared by Go and manifest-based Python plugins."""
import json
import os
from pathlib import Path
import runpy
import subprocess
import sys
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[1]


class ReleaseTest(unittest.TestCase):
    def release_entry(self, export, entry=None, source_manifest=None, manifest_name='manifest.yml',
                      manifest_symlink=False, expected_error=None, locale_files=None, source_doc="docs/demo.md",
                      source_assets=None, catalog_assets=None, setup_assets=None, check_assets=None, version='v0.3.0'):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / 'demo'
            (source / 'docs').mkdir(parents=True)
            (source / source_doc).write_text('# Demo usage\n')
            (source / 'locales').mkdir()
            for filename, content in (locale_files if locale_files is not None else {'en.json': '{}\n'}).items():
                (source / 'locales' / filename).write_text(content)
            if source_manifest is not None:
                manifest = source / manifest_name
                if manifest_symlink:
                    outside = root / 'outside.yml'
                    outside.write_text(yaml.safe_dump(source_manifest, sort_keys=False))
                    manifest.symlink_to(outside)
                else:
                    manifest.write_text(yaml.safe_dump(source_manifest, sort_keys=False))
            target = root / 'catalog/plugins/sokel/demo/manifest.yml'
            if entry is not None:
                target.parent.mkdir(parents=True)
                target.write_text(yaml.safe_dump(entry, sort_keys=False))
            original_entry = target.read_bytes() if target.exists() else None
            for directory, assets in ((source, source_assets), (target.parent, catalog_assets)):
                for name, content in (assets or {}).items():
                    asset = directory / name
                    asset.parent.mkdir(parents=True, exist_ok=True)
                    asset.write_bytes(content)
            if setup_assets:
                setup_assets(source, target.parent)
            export_file = root / 'export.yml'
            export_file.write_text(export)
            image = 'ghcr.io/sokel-dev/sokel-plugin-demo:0.3.0@sha256:' + 'a' * 64
            result = subprocess.run([
                sys.executable, str(ROOT / 'scripts/registry-entry.py'),
                '--plugin', 'demo', '--version', version, '--image', image,
                '--export', str(export_file), '--catalog', str(root / 'catalog'),
            ], cwd=root, text=True, capture_output=True)
            if check_assets:
                check_assets(source, target.parent)
            if expected_error is not None:
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected_error, result.stderr)
                if original_entry is None:
                    self.assertFalse(target.exists(), 'invalid source must not create a catalog entry')
                else:
                    self.assertEqual(target.read_bytes(), original_entry,
                                     'invalid source must not change a catalog entry')
                return
            self.assertEqual(result.returncode, 0, result.stderr)
            if entry is None:
                for filename, content in (locale_files if locale_files is not None else {'en.json': '{}'}).items():
                    copied = target.parent / 'locales' / filename
                    self.assertEqual(json.loads(copied.read_text()), json.loads(content))
            text = target.read_text()
            return yaml.safe_load(text), text, image

    def test_release_advances_existing_prerelease_version(self):
        old = {'plugin': {'name': 'demo', 'version': 'v0.3.1-docs.1'}}
        entry, _, _ = self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo'}}), old, version='v0.3.2')
        self.assertEqual(entry['plugin']['version'], 'v0.3.2')
        self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo'}}), old,
                           version='v0.3.1-docs.1+build.2', expected_error='does not go up')

    def test_semver_ordering_and_validation(self):
        semver = runpy.run_path(str(ROOT / 'scripts/registry-entry.py'))['semver']
        ordered = ['1.0.0-alpha', '1.0.0-alpha.1', '1.0.0-alpha.beta', '1.0.0-beta',
                   '1.0.0-beta.2', '1.0.0-beta.11', '1.0.0-rc.1', '1.0.0', '1.0.1']
        for older, newer in zip(ordered, ordered[1:]):
            with self.subTest(older=older, newer=newer):
                self.assertLess(semver(older), semver(newer))
        self.assertEqual(semver('v1.2.3+build.01'), semver('1.2.3+another-build'))
        for invalid in ('1.2', '01.2.3', '1.02.3', '1.2.03', '1.2.3-01', '1.2.3-a.01',
                        '1.2.3-', '1.2.3-a..b', '1.2.3+', '1.2.3+a..b', '1.2.3-a_b', '１.2.3'):
            with self.subTest(invalid=invalid):
                with self.assertRaises(SystemExit):
                    semver(invalid)

    def test_first_release_copies_selected_icon_and_optional_license(self):
        # Byte preservation is tested here; SDK catalog validation owns image format and size checks.
        for name, payload in (('icon.svg', b'<svg viewBox="0 0 24 24"/>\n'),
                              ('assets/icon.png', b'\x89PNG\r\n\x1a\nsynthetic binary fixture\x00')):
            for metadata_source in ('export', 'manifest'):
                with self.subTest(name=name, metadata_source=metadata_source):
                    plugin = {'name': 'demo', 'doc': 'docs/demo.md'}
                    local = {'plugin': {'name': 'demo'}}
                    (plugin if metadata_source == 'export' else local['plugin'])['icon'] = name
                    license_text = b'Synthetic test icon. No third-party artwork.\n'
                    def check(source, target):
                        self.assertTrue((target / name).is_file(), 'local icon must ship with its manifest')
                        self.assertEqual((target / name).read_bytes(), payload)
                        self.assertEqual((target / (name + '.license')).read_bytes(), license_text)
                    entry, _, _ = self.release_entry(yaml.safe_dump({'plugin': plugin}),
                        source_manifest=local, source_assets={name: payload, name + '.license': license_text},
                        check_assets=check)
                    self.assertEqual(entry['plugin']['icon'], name)

    def test_existing_entry_refreshes_selected_icon_and_license(self):
        old = {'plugin': {'name': 'demo', 'version': 'v0.2.0', 'icon': 'assets/icon.svg'}}
        export = {'plugin': {'name': 'demo', 'icon': 'other.svg'}}
        assets = {'assets/icon.svg': b'new icon', 'assets/icon.svg.license': b'new license',
                  'other.svg': b'not selected'}
        def check(source, target):
            self.assertEqual((target / 'assets/icon.svg').read_bytes(), b'new icon')
            self.assertEqual((target / 'assets/icon.svg.license').read_bytes(), b'new license')
            self.assertFalse((target / 'other.svg').exists())
        entry, _, _ = self.release_entry(yaml.safe_dump(export), old, source_assets=assets,
            catalog_assets={'assets/icon.svg': b'old icon', 'assets/icon.svg.license': b'old license'},
            check_assets=check)
        self.assertEqual(entry['plugin']['icon'], 'assets/icon.svg')

    def test_existing_entry_keeps_catalog_icon_choice_and_missing_optional_assets(self):
        for icon in ('brand:github', 'catalog.svg'):
            with self.subTest(icon=icon):
                old = {'plugin': {'name': 'demo', 'version': 'v0.2.0'}}
                if icon:
                    old['plugin']['icon'] = icon
                def check(source, target):
                    self.assertEqual((target / 'catalog.svg').read_bytes(), b'catalog icon')
                    self.assertEqual((target / 'catalog.svg.license').read_bytes(), b'catalog license')
                    self.assertFalse((target / 'icon.svg').exists())
                entry, _, _ = self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo', 'icon': 'icon.svg'}}),
                    old, source_assets={'icon.svg': b'source icon', 'icon.svg.license': b'source license'},
                    catalog_assets={'catalog.svg': b'catalog icon', 'catalog.svg.license': b'catalog license'},
                    check_assets=check)
                self.assertEqual(entry['plugin'].get('icon', ''), icon)

        old = {'plugin': {'name': 'demo', 'version': 'v0.2.0', 'icon': 'icon.svg'}}
        def check(source, target):
            self.assertEqual((target / 'icon.svg').read_bytes(), b'new icon')
            self.assertEqual((target / 'icon.svg.license').read_bytes(), b'catalog license')
        self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo'}}), old,
            source_assets={'icon.svg': b'new icon'},
            catalog_assets={'icon.svg': b'old icon', 'icon.svg.license': b'catalog license'}, check_assets=check)

    def test_existing_entry_backfills_empty_icon_and_copies_its_assets(self):
        for declaration in ({}, {'icon': ''}, {'icon': '  '}, {'icon': None}):
            for metadata_source in ('export', 'manifest.yml', 'sokel.yaml'):
                with self.subTest(declaration=declaration, metadata_source=metadata_source):
                    old = {'plugin': {'name': 'demo', 'version': 'v0.2.0', 'label': 'Catalog label',
                                      'desc': 'Catalog description', **declaration}}
                    export = {'plugin': {'name': 'demo', 'icon': ''}}
                    local = {'plugin': {'name': 'demo', 'label': 'Source label', 'desc': 'Source description'}}
                    if metadata_source == 'export':
                        export['plugin']['icon'] = 'icon.svg'
                        local['plugin']['icon'] = 'ignored.svg'
                    else:
                        local['plugin']['icon'] = 'icon.svg'
                    def check(source, target):
                        self.assertTrue((target / 'icon.svg').is_file(), 'a backfilled icon must ship with its entry')
                        self.assertEqual((target / 'icon.svg').read_bytes(), b'new icon')
                        self.assertEqual((target / 'icon.svg.license').read_bytes(), b'new license')
                        self.assertFalse((target / 'ignored.svg').exists())
                    entry, _, _ = self.release_entry(yaml.safe_dump(export), old,
                        source_manifest=local, manifest_name='sokel.yaml' if metadata_source == 'sokel.yaml' else 'manifest.yml',
                        source_assets={'icon.svg': b'new icon', 'icon.svg.license': b'new license'}, check_assets=check)
                    self.assertEqual(entry['plugin']['icon'], 'icon.svg')
                    self.assertEqual(entry['plugin']['label'], 'Catalog label')
                    self.assertEqual(entry['plugin']['desc'], 'Catalog description')

    def test_existing_entry_backfill_requires_safe_local_assets_and_matching_metadata(self):
        old = {'plugin': {'name': 'demo', 'version': 'v0.2.0'}}
        export = yaml.safe_dump({'plugin': {'name': 'demo', 'icon': 'icon.svg'}})
        self.release_entry(export, old, catalog_assets={'icon.svg': b'old unreferenced file'},
                           expected_error='local icon')
        self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo'}}), old,
            source_manifest={'plugin': {'name': 'other', 'icon': 'icon.svg'}},
            expected_error='local manifest is for')
        self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo', 'icon': '../outside.svg'}}), old,
                           expected_error='local icon')
        def setup(source, target):
            outside = source.parent / 'outside.svg'
            outside.write_bytes(b'outside unchanged')
            (source / 'icon.svg').symlink_to(outside)
        self.release_entry(export, old, setup_assets=setup, expected_error='local icon')

        def check(source, target):
            self.assertEqual((target / 'icon.svg').read_bytes(), b'icon without optional license')
            self.assertFalse((target / 'icon.svg.license').exists())
        self.release_entry(export, old, source_assets={'icon.svg': b'icon without optional license'}, check_assets=check)
        unchanged, _, _ = self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo'}}), old)
        self.assertNotIn('icon', unchanged['plugin'], 'legacy entries without any icon declaration remain valid')

    def test_local_icon_paths_reject_urls_and_escapes_before_writing(self):
        for icon in ('../outside.svg', '/tmp/outside.svg', 'assets/../../outside.svg',
                     'https://example.invalid/icon.svg', '//example.invalid/icon.svg',
                     'data:image/svg+xml,content', 'file:icon.svg', 'C:\\icon.svg'):
            with self.subTest(icon=icon):
                self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo', 'icon': icon}}),
                                   expected_error='local icon')

    def test_first_local_icon_requires_a_file_but_no_license(self):
        export = yaml.safe_dump({'plugin': {'name': 'demo', 'icon': 'icon.svg'}})
        self.release_entry(export, expected_error='local icon')
        self.release_entry(export, setup_assets=lambda source, target: (source / 'icon.svg').mkdir(),
                           expected_error='local icon')
        def check(source, target):
            self.assertEqual((target / 'icon.svg').read_bytes(), b'icon')
            self.assertFalse((target / 'icon.svg.license').exists())
        self.release_entry(export, source_assets={'icon.svg': b'icon'}, check_assets=check)

    def test_icon_and_license_reject_unsafe_symlinks_before_any_catalog_writes(self):
        for location in ('source', 'catalog'):
            for filename in ('icon.svg', 'icon.svg.license'):
                with self.subTest(location=location, filename=filename):
                    old = {'plugin': {'name': 'demo', 'version': 'v0.2.0', 'icon': 'icon.svg', 'doc': 'README.md'}}
                    def setup(source, target):
                        outside = source.parent / 'outside'
                        outside.write_bytes(b'outside unchanged')
                        asset = (source if location == 'source' else target) / filename
                        asset.unlink()
                        asset.symlink_to(outside)
                    def check(source, target):
                        self.assertEqual((source.parent / 'outside').read_bytes(), b'outside unchanged')
                        self.assertEqual((target / 'README.md').read_bytes(), b'old documentation')
                    self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo'}}), old,
                        source_assets={'icon.svg': b'new icon', 'icon.svg.license': b'new license'},
                        catalog_assets={'icon.svg': b'old icon', 'icon.svg.license': b'old license',
                                        'README.md': b'old documentation'},
                        setup_assets=setup, check_assets=check, expected_error='local icon')

        def setup(source, target):
            outside = source.parent / 'outside-assets'
            outside.mkdir()
            (outside / 'icon.svg').write_bytes(b'outside unchanged')
            (source / 'assets').symlink_to(outside, target_is_directory=True)
        self.release_entry(yaml.safe_dump({'plugin': {'name': 'demo', 'icon': 'assets/icon.svg'}}),
                           setup_assets=setup, expected_error='local icon')

    def test_first_release_accepts_english_source_with_chinese_locale(self):
        export = yaml.safe_dump({'plugin': {'name': 'demo', 'label': 'Demo', 'doc': 'docs/demo.md'},
                                 'operations': [{'id': 'read', 'label': 'Read items'}]})
        entry, _, _ = self.release_entry(export, locale_files={
            'zh-CN.json': json.dumps({'Demo': '示例', 'Read items': '读取条目',
                                     '# Demo usage\n': '# 示例使用说明\n'}, ensure_ascii=False),
        })
        self.assertEqual(entry['plugin']['label'], 'Demo')
        self.assertEqual(entry['operations'][0]['label'], 'Read items')

    def test_first_release_validates_all_locale_json_before_writing(self):
        export = yaml.safe_dump({'plugin': {'name': 'demo', 'doc': 'docs/demo.md'}})
        for invalid in ('{broken', '[]', '{"Read": 7}', '{"Read": {"nested": "value"}}',
                        '{"": "empty key"}', '{"Read": "One", "Read": "Two"}'):
            with self.subTest(invalid=invalid):
                self.release_entry(export, locale_files={'en.json': '{}', 'zh-CN.json': invalid},
                                   expected_error='invalid locale')

    def test_first_release_requires_a_supported_translation_locale(self):
        export = yaml.safe_dump({'plugin': {'name': 'demo', 'doc': 'docs/demo.md'}})
        self.release_entry(export, locale_files={}, expected_error='en.json or locales/zh-CN.json')

    def test_first_release_fills_missing_schema_export_metadata(self):
        # Real schema exports contain a plugin name but omit distribution metadata.
        export = {'plugin': {'name': 'demo'},
                  'operations': [{'id': 'read', 'label': 'Exported operation'}]}
        local = {'plugin': {'name': 'demo', 'org': 'ignored-org', 'version': '9.9.9',
                            'label': 'Demo service', 'desc': 'Read service data',
                            'icon': 'brand:google', 'doc': 'docs/custom.md'},
                 'operations': [{'id': 'local_only', 'label': 'Never import local contracts'}]}
        for filename in ('manifest.yml', 'sokel.yaml'):
            with self.subTest(filename=filename):
                entry, _, _ = self.release_entry(yaml.safe_dump(export), source_manifest=local,
                                                 manifest_name=filename, source_doc='docs/custom.md', locale_files={
                    'zh-CN.json': json.dumps({'Demo service': '示例服务', 'Read service data': '读取服务数据'}),
                })
                self.assertEqual(entry['plugin'], {'name': 'demo', 'org': 'sokel', 'version': 'v0.3.0',
                    'label': 'Demo service', 'desc': 'Read service data', 'icon': 'brand:google', 'doc': 'README.md'})
                self.assertEqual(entry['operations'], export['operations'])

    def test_first_release_export_metadata_wins(self):
        export = {'plugin': {'name': 'demo', 'label': 'Export label', 'desc': 'Export description',
                             'icon': 'brand:github', 'doc': 'docs/demo.md'}, 'deployment': {'targets': []}}
        local = {'plugin': {'name': 'demo', 'label': 'Local label', 'desc': 'Local description',
                            'icon': 'brand:google', 'doc': 'missing.md'}}
        entry, _, _ = self.release_entry(yaml.safe_dump(export), source_manifest=local)
        self.assertEqual(entry['plugin']['label'], 'Export label')
        self.assertEqual(entry['plugin']['desc'], 'Export description')
        self.assertEqual(entry['plugin']['icon'], 'brand:github')

    def test_first_release_validates_local_metadata_even_with_export_deployment(self):
        export = yaml.safe_dump({'plugin': {'name': 'demo'}, 'deployment': {'targets': []}})
        self.release_entry(export, source_manifest={'plugin': {'name': 'other'}},
                           expected_error='local manifest is for')
        self.release_entry(export, source_manifest={'plugin': {'name': 'demo'}},
                           manifest_symlink=True, expected_error='inside the plugin directory')
        self.release_entry(export, source_manifest={'plugin': {'name': 'demo', 'doc': '../outside.md'}},
                           expected_error='local usage document')
        self.release_entry(export, source_manifest={'plugin': {'name': 'demo', 'label': ['invalid']}},
                           expected_error='local manifest plugin.label must be a string')

    def test_first_release_uses_local_deployment_when_export_omits_it(self):
        deployment = {
            'targets': [{'kind': 'binary', 'ref': 'https://example.com/demo'},
                        {'kind': 'container', 'ref': 'source-image:0.3.0'}],
            'env': [{'name': 'SOKEL_DATA_DIR', 'default': '/data/demo', 'required': True}],
            'note': 'Persistent volume; one replica',
        }
        export = {'plugin': {'name': 'demo', 'doc': 'docs/demo.md'},
                  'operations': [{'id': 'send', 'label': 'Exported operation'}]}
        local = {'plugin': {'name': 'demo', 'org': 'sokel'}, 'deployment': deployment,
                 'operations': [{'id': 'local_only', 'label': 'Do not import this contract'}]}
        for filename in ('manifest.yml', 'sokel.yaml'):
            with self.subTest(filename=filename):
                entry, _, image = self.release_entry(yaml.safe_dump(export), source_manifest=local,
                                                     manifest_name=filename)
                expected = json.loads(json.dumps(deployment))
                expected['targets'][1]['ref'] = image
                self.assertEqual(entry['deployment'], expected)
                self.assertEqual(entry['operations'], export['operations'])

    def test_first_release_prefers_export_deployment_to_local_manifest(self):
        deployment = {'targets': [{'kind': 'container', 'ref': 'export-image:0.3.0'}],
                      'env': [{'name': 'EXPORTED_ENV'}], 'note': 'Exported deployment'}
        export = {'plugin': {'name': 'demo', 'doc': 'docs/demo.md'}, 'deployment': deployment}
        local = {'plugin': {'name': 'demo'},
                 'deployment': {'env': [{'name': 'LOCAL_ENV'}], 'note': 'Local deployment'}}
        entry, _, image = self.release_entry(yaml.safe_dump(export), source_manifest=local)
        expected = json.loads(json.dumps(deployment))
        expected['targets'][0]['ref'] = image
        self.assertEqual(entry['deployment'], expected)

    def test_first_release_rejects_unrelated_or_external_local_manifest(self):
        export = yaml.safe_dump({'plugin': {'name': 'demo', 'doc': 'docs/demo.md'}})
        for name, symlink, error in (
            ('another-plugin', False, 'local manifest is for'),
            ('demo', True, 'local manifest must be inside the plugin directory'),
        ):
            with self.subTest(name=name, symlink=symlink):
                local = {'plugin': {'name': name}, 'deployment': {'note': 'Do not import'}}
                self.release_entry(export, source_manifest=local, manifest_symlink=symlink,
                                   expected_error=error)

    def test_existing_entry_keeps_deployment_when_export_omits_it(self):
        deployment = {'targets': [{'kind': 'container', 'ref': 'catalog-image:0.2.0'}],
                      'env': [{'name': 'CATALOG_ENV'}], 'note': 'Catalog deployment'}
        old = {'plugin': {'name': 'demo', 'version': 'v0.2.0'}, 'deployment': deployment}
        export = {'plugin': {'name': 'demo', 'doc': 'docs/demo.md'}, 'operations': []}
        local = {'plugin': {'name': 'demo'},
                 'deployment': {'env': [{'name': 'LOCAL_ENV'}], 'note': 'Local deployment'}}
        entry, _, image = self.release_entry(yaml.safe_dump(export), old, source_manifest=local)
        expected = json.loads(json.dumps(deployment))
        expected['targets'][0]['ref'] = image
        self.assertEqual(entry['deployment'], expected)

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
