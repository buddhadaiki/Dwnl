"""Short-form routing against the pinned upstream extractors; all tests offline."""
import contextlib
import copy
import json
import pathlib
import tempfile
import unittest
from unittest.mock import patch
from yt_dlp import YoutubeDL

# Reuse the adapter instance that owns the global request-handler registry.
from test_transport import transport

# IDs/URLs come from yt-dlp 2026.08.19 extractor fixture definitions. The tests
# check matching/routing, not whether those public posts remain available today.
YOUTUBE_ID = '18NGQq7p3LY'
YOUTUBE_URLS = (
    'https://www.youtube.com/shorts/' + YOUTUBE_ID,
    'https://m.youtube.com/shorts/' + YOUTUBE_ID,
    'https://www.youtube.com/watch?v=' + YOUTUBE_ID,
    'https://www.youtube.com/embed/' + YOUTUBE_ID,
    'https://youtu.be/' + YOUTUBE_ID,
)
INSTAGRAM_URLS = (
    ('https://www.instagram.com/reel/Chunk8-jurw/', 'Chunk8-jurw'),
    ('https://www.instagram.com/reels/Cop84x6u7CP/', 'Cop84x6u7CP'),
    ('https://www.instagram.com/marvelskies.fc/reel/CWqAgUZgCku/', 'CWqAgUZgCku'),
)


class ShortFormsTests(unittest.TestCase):
    def setUp(self):
        state = patch.dict(transport.STATE, {'platform': None, 'mode': 'analyze', 'bytes': 0, 'requests': 0, 'byte_limit': None}, clear=True)
        state.start()
        self.addCleanup(state.stop)

    def test_short_watch_embed_and_share_links_select_same_youtube_id(self):
        for url in YOUTUBE_URLS:
            with self.subTest(url=url):
                self.assertTrue(transport._YouTube.suitable(url))
                self.assertEqual(transport._YouTube._match_id(url), YOUTUBE_ID)

    def test_instagram_reel_variants_keep_pinned_extractor_and_post_id(self):
        for url, identity in INSTAGRAM_URLS:
            with self.subTest(url=url):
                self.assertTrue(transport._Instagram.suitable(url))
                self.assertEqual(transport._Instagram._match_id(url), identity)

    def test_channel_and_playlist_routes_do_not_select_single_video_extractor(self):
        for url in (
            'https://www.youtube.com/playlist?list=PL-example',
            'https://www.youtube.com/@publicchannel/shorts',
            'https://www.youtube.com/shorts/',
        ):
            with self.subTest(url=url):
                self.assertFalse(transport._YouTube.suitable(url))

    def test_adapter_dispatches_short_forms_without_rewriting_or_losing_portrait_formats(self):
        for extractor, url, identity in (
            (transport._YouTube, YOUTUBE_URLS[0], YOUTUBE_ID),
            (transport._Instagram, INSTAGRAM_URLS[0][0], INSTAGRAM_URLS[0][1]),
            (transport._Instagram, INSTAGRAM_URLS[1][0], INSTAGRAM_URLS[1][1]),
        ):
            record = {'id': identity, 'title': 'Public portrait fixture', 'duration': 15,
                      'formats': [{'format_id': 'portrait', 'url': 'https://rr1.googlevideo.com/media' if extractor is transport._YouTube else 'https://scontent.cdninstagram.com/media.mp4',
                                   'protocol': 'https', 'width': 1080, 'height': 1920,
                                   'vcodec': 'avc1.640028', 'acodec': 'mp4a.40.2'}]}
            with self.subTest(url=url), tempfile.TemporaryDirectory() as folder, contextlib.chdir(folder), \
                    YoutubeDL({'quiet': True, 'no_warnings': True, 'js_runtimes': {}}, auto_init=False) as downloader, \
                    patch.object(extractor, 'extract', return_value=copy.deepcopy(record)) as native, \
                    patch.object(downloader, 'urlopen', side_effect=AssertionError('offline test attempted network')):
                result = transport.DwnlIE(downloader)._extract_owned(url)
                native.assert_called_once_with(url)
                metadata = json.loads(pathlib.Path('metadata.json').read_text(encoding='utf-8'))
                self.assertEqual(result['id'], identity)
                fmt = metadata['entries'][0]['formats'][0]
                self.assertEqual((fmt['width'], fmt['height']), (1080, 1920))
                self.assertEqual(fmt['vcodec'], 'avc1.640028')
                self.assertEqual(fmt['acodec'], 'mp4a.40.2')

    def test_short_form_access_refusal_does_not_try_alternative_route_or_client(self):
        response = {'playabilityStatus': {'status': 'LOGIN_REQUIRED', 'reason': 'Sign in to confirm your age'}}
        with patch.object(transport.YoutubeIE, '_extract_player_response', return_value=response) as upstream:
            with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                transport._YouTube(None)._extract_player_response('web')
            upstream.assert_called_once_with('web')


if __name__ == '__main__':
    unittest.main()
