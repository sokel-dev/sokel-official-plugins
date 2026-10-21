"""Exercise all registered operations through the SDK without external services."""
import json
from pathlib import Path
import re
import unittest
from unittest.mock import patch

from sokel import File
import main
from sokel_gen import CONTRACT


class Files:
    async def store_stream(self, name, mime, src):
        self.data = src.read()
        return File(id='file-test', name=name, mime=mime, size=len(self.data))


class PluginTest(unittest.IsolatedAsyncioTestCase):
    def test_usage_document_has_english_translation(self):
        root = Path(__file__).parent
        source = (root / "docs/youtube.md").read_text()
        translations = json.loads((root / "locales/en.json").read_text())
        translated = translations.get(source, "")
        self.assertIn("## What it does", translated)
        self.assertIn("YTDLP_PROXY", translated)
        self.assertIn("format_id", translated)

    async def call(self, operation, **inputs):
        return await main.p.dispatch_buffered({'operation': operation, 'input': inputs}, files=self.files)

    def setUp(self):
        self.files = Files()

    async def test_metadata_keeps_typed_outputs(self):
        info = dict(id='video', title='Title', upload_date='20260819', duration=21.8,
                    formats=[dict(format_id='a', height=720, filesize=main.MB),
                             dict(format_id='b', height=720), dict(format_id='audio')],
                    subtitles={'en': []}, automatic_captions={'zh': []})
        with patch.object(main, 'extract', return_value=info):
            out = await self.call('video_info', url='video')
        self.assertEqual(out['duration_seconds'], 21)
        self.assertEqual(out['upload_date'], '2026-08-19')
        self.assertEqual(out['formats'][0]['filesize_mb'], 1)
        self.assertEqual(len(out['formats']), 1)
        self.assertEqual(out['subtitle_langs'], ['en', 'zh'])

    async def test_downloads_upload_files_and_cleanup(self):
        for operation, inputs, ext in [('download', {}, 'mp4'),
                                       ('download_audio', {}, 'webm'),
                                       ('download_audio', {'audio_format': 'mp3'}, 'mp3')]:
            with self.subTest(operation=operation, ext=ext):
                paths, options = [], []
                def extract(url, opts=None, download=False, **kwargs):
                    options.append(dict(opts or {}))
                    if download:
                        path = Path(opts['outtmpl']).parent / ('sample.' + ext)
                        path.write_bytes(b'payload')
                        paths.append(path)
                    return dict(title='Title', ext='webm', acodec='opus', duration=2,
                                requested_formats=[{'filesize': 4}, {'filesize': 3}])
                with patch.object(main, 'extract', side_effect=extract):
                    out = await self.call(operation, url='video', filename='saved', **inputs)
                self.assertEqual(self.files.data, b'payload')
                self.assertEqual(out['file']['id'], 'file-test')
                self.assertEqual(out['file']['name'], 'saved.' + ext)
                self.assertFalse(paths[0].parent.exists())
                if operation == 'download':
                    self.assertTrue(out['merged'])
                    self.assertIn('height<=720', options[0]['format'])
                    self.assertEqual(options[1]['max_filesize'], 512 * main.MB)
                else:
                    self.assertEqual(out['transcoded'], ext == 'mp3')
                    self.assertEqual('postprocessors' in options[1], ext == 'mp3')

    async def test_size_limit_refuses_before_downloading(self):
        for operation in ('download', 'download_audio'):
            with self.subTest(operation=operation), patch.object(main, 'extract', return_value={
                'requested_formats': [{'filesize': main.MB}, {'filesize': main.MB}],
            }) as extract:
                with self.assertRaises(ValueError):
                    await self.call(operation, url='video', max_filesize_mb=1)
                self.assertEqual(extract.call_count, 1)

    async def test_empty_download_is_an_error(self):
        for operation in ('download', 'download_audio'):
            with self.subTest(operation=operation), patch.object(main, 'extract', return_value={}):
                with self.assertRaises(RuntimeError):
                    await self.call(operation, url='video')

    async def test_subtitle_text_without_video_download(self):
        for automatic in (False, True):
            with self.subTest(automatic=automatic):
                def extract(url, opts=None, download=False, **kwargs):
                    if download:
                        self.assertTrue(opts['skip_download'])
                        self.assertEqual(opts['writeautomaticsub'], automatic)
                        (Path(opts['outtmpl']).parent / 'sub.en.vtt').write_text(
                            'WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\n<b>Hello</b>\nHello\nWorld\n')
                    return {'title': 'Title', 'automatic_captions' if automatic else 'subtitles': {'en': []}}
                with patch.object(main, 'extract', side_effect=extract):
                    out = await self.call('transcript', url='video', lang='en')
                self.assertEqual(out['text'], 'Hello\nWorld')
                self.assertEqual(out['char_count'], 11)
                self.assertEqual(out['automatic'], automatic)

    async def test_health_reports_dependency_and_network_failures(self):
        for binary, error, ok in [(None, None, False), ('/bin/ffmpeg', OSError('offline'), False),
                                  ('/bin/ffmpeg', None, True)]:
            with self.subTest(binary=binary, error=error), patch.object(main.shutil, 'which', return_value=binary), \
                    patch.object(main, 'probe_network', side_effect=error):
                self.assertEqual((await self.call('health_check'))['ok'], ok)

    def test_subtitle_selection_and_format_modes(self):
        self.assertEqual(main.pick_subtitle({'subtitles': {'zh-Hans': []}}, 'zh'), ('zh-Hans', False))
        self.assertEqual(main.pick_subtitle({'automatic_captions': {'en': []}}, 'zz'), ('en', True))
        with self.assertRaises(ValueError):
            main.pick_subtitle({}, 'en')
        self.assertEqual(main.format_selector('best')['format'], 'bestvideo+bestaudio/best')
        self.assertEqual(main.format_selector('720', '137')['format'], '137+bestaudio/137')

    def test_extractor_uses_deployment_options(self):
        with patch.dict(main.os.environ, {'YTDLP_PROXY': 'http://proxy.invalid', 'YTDLP_COOKIES': '/cookies'}), \
                patch.object(main.yt_dlp, 'YoutubeDL') as ydl:
            ydl.return_value.__enter__.return_value.extract_info.return_value = {'id': 'video'}
            self.assertEqual(main.extract('video'), {'id': 'video'})
            self.assertEqual(ydl.call_args.args[0]['proxy'], 'http://proxy.invalid')
            self.assertEqual(ydl.call_args.args[0]['cookiefile'], '/cookies')

    def test_all_catalog_display_strings_have_english(self):
        translations = json.loads((Path(__file__).parent / 'locales/en.json').read_text())
        def walk(value):
            if isinstance(value, dict):
                for key, item in value.items():
                    if key in ('label', 'desc', 'help', 'placeholder') and isinstance(item, str) and re.search('[\u4e00-\u9fff]', item):
                        self.assertTrue(translations.get(item), item)
                    walk(item)
            elif isinstance(value, list):
                for item in value:
                    walk(item)
        walk(CONTRACT)


if __name__ == '__main__':
    unittest.main()
