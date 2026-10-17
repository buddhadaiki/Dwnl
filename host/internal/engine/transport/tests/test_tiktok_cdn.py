"""Official anonymous CDN token, exercised without DNS or real sockets."""
import contextlib
import gzip
import io
import json
import time
import unittest
from unittest.mock import MagicMock, patch

from yt_dlp import YoutubeDL
from yt_dlp.downloader.http import HttpFD
from yt_dlp.networking.common import Request

import test_tiktok_session as fixtures
from test_tiktok_session import SOURCE, challenge_page, http_response, public_page
from test_transport import transport, wire_response


MEDIA = 'https://v16-webapp-prime.tiktok.com/fixture.mp4?signature=exact&expiry=99'
TOKEN = 'synthetic-anonymous-cdn-value'
SET_COOKIE = 'tt_chain_token=' + TOKEN + '; Domain=.tiktok.com; Path=/; Max-Age=300; HttpOnly'


class TikTokCdnTests(unittest.TestCase):
    setUp = fixtures.TikTokAnonymousSessionTests.setUp
    operation = fixtures.TikTokAnonymousSessionTests.operation
    extract = fixtures.TikTokAnonymousSessionTests.extract

    def session(self, raw=SET_COOKIE, bind=True):
        transport.STATE.update(platform='tiktok', mode='download')
        session = transport._TikTokCdnSession()
        session.capture(raw, True)
        session.promote({'id': 'fixture', 'video': {'playAddr': MEDIA}, 'author': {'privateAccount': False}}, 0)
        if bind:
            session.bind({'url': MEDIA, 'protocol': 'https'})
        self.addCleanup(session.close)
        return session

    def test_official_token_reaches_only_selected_native_http_download(self):
        page = public_page().replace('https://v16.tiktokcdn.com/synthetic.mp4', MEDIA)
        output = io.StringIO()
        responses = [http_response(challenge_page()), http_response(page, headers='Set-Cookie: ' + SET_COOKIE + '\r\n'),
                     http_response('media')]
        with self.operation(responses) as (downloader, connections, factory, directory), \
                contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            downloader.params['extractor_args'] = {'dwnl': {'mode': ['download'], 'item': ['7123456789012345678']}}
            (directory / 'selection.json').write_text(json.dumps({
                'id': '7123456789012345678', 'format': {'format_id': 'play'}, 'budget_bytes': 1024 * 1024,
            }))
            info = self.extract(downloader)
            self.assertEqual(list(downloader.cookiejar), [])
            self.assertNotIn(TOKEN, json.dumps(info))
            native = HttpFD(downloader, {'noprogress': True, 'nopart': True, 'retries': 0, 'continuedl': False})
            self.assertTrue(native.download(str(directory / 'media.mp4'), dict(info, **info['formats'][0]))[0])
            self.assertEqual((directory / 'media.mp4').read_bytes(), b'media')
            self.assertEqual(factory.call_count, 3)
            self.assertEqual(connections[2].sent_headers.get('Cookie'), 'tt_chain_token=' + TOKEN,
                             'public metadata token was dropped before the selected CDN request')
            self.assertNotIn('Set-Cookie', connections[2].sent_headers)
            self.assertNotIn(TOKEN, (directory / 'metadata.json').read_text())
            self.assertNotIn(TOKEN, output.getvalue())

    def test_only_official_domain_path_and_safe_single_value_are_accepted(self):
        invalid = [
            'tt_chain_token=x; Path=/',
            'tt_chain_token=x; Domain=www.tiktok.com; Path=/',
            'tt_chain_token=x; Domain=.tiktokcdn.com; Path=/',
            'tt_chain_token=x; Domain=.com; Path=/',
            'tt_chain_token=x; Domain=.tiktok.com; Path=/other',
            'tt_chain_token=x; Domain=.tiktok.com',
            'tt_chain_token=x; Domain=.tiktok.com; Path=/; Domain=.tiktok.com',
            'tt_chain_token=x; Domain=.tiktok.com; Path=/; Secure=1',
            'tt_chain_token=x; Domain=.tiktok.com; Path=/; HttpOnly=1',
            'tt_chain_token=x; Domain=.tiktok.com; Path=/; SameSite=invalid',
            'tt_chain_token=x; Domain=.tiktok.com; Path=/; Other=value',
            'tt_chain_token=x; Domain=.tiktok.com; Path=/;',
            'tt_chain_token=x, tt_chain_token=y; Domain=.tiktok.com; Path=/',
            'tt_chain_token="quoted"; Domain=.tiktok.com; Path=/',
            'tt_chain_token=x\\y; Domain=.tiktok.com; Path=/',
            'tt_chain_token=space value; Domain=.tiktok.com; Path=/',
            'tt_chain_token=bad\r\nheader; Domain=.tiktok.com; Path=/',
            'tt_chain_token=' + 'x' * 4097 + '; Domain=.tiktok.com; Path=/',
            'tt_chain_token=; Domain=.tiktok.com; Path=/',
        ]
        for index, raw in enumerate(invalid):
            with self.subTest(case=index):
                session = transport._TikTokCdnSession()
                with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED') as caught:
                    session.capture(raw, True)
                self.assertNotIn(raw, str(caught.exception))
                session.promote({'id': 'fixture', 'video': {'playAddr': MEDIA}}, 0)
                session.bind({'url': MEDIA, 'protocol': 'https'})
                self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_duplicate_cookie_is_terminal_without_replacement(self):
        session = self.session()
        with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
            session.capture(SET_COOKIE, True)
        self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_semantically_valid_quoted_domain_and_path_remain_scoped(self):
        session = self.session('tt_chain_token=' + TOKEN + '; Domain=".tiktok.com"; Path="/"; Secure; SameSite=Lax')
        self.assertEqual(session.request_cookie(MEDIA, 'GET'), 'tt_chain_token=' + TOKEN)

    def test_inconsistent_protocol_cannot_bind_an_http_url(self):
        session = self.session(bind=False)
        with self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
            session.bind({'url': MEDIA.replace('https:', 'http:'), 'protocol': 'https'})
        self.assertIsNone(session.request_cookie(MEDIA.replace('https:', 'http:'), 'GET'))
        session.bind({'url': MEDIA, 'protocol': 'https'})
        self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_other_cookies_and_ineligible_sources_never_provide_a_token(self):
        for raw, eligible in ((SET_COOKIE, False), ('sid_tt=account; Domain=.tiktok.com; Path=/', True),
                              ('_wafchallengeid=value; Domain=.tiktok.com; Path=/', True)):
            with self.subTest(eligible=eligible, name=raw.partition('=')[0]):
                session = transport._TikTokCdnSession()
                session.capture(raw, eligible)
                transport.STATE['mode'] = 'download'
                session.promote({'id': 'fixture', 'video': {'playAddr': MEDIA}}, 0)
                session.bind({'url': MEDIA, 'protocol': 'https'})
                self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_ttl_is_monotonic_capped_and_never_renewed_by_range_gets(self):
        with patch.object(transport.time, 'monotonic', return_value=100):
            session = self.session()
        for moment in (100, 219.999):
            with self.subTest(moment=moment), patch.object(transport.time, 'monotonic', return_value=moment):
                self.assertEqual(session.request_cookie(MEDIA, 'GET'), 'tt_chain_token=' + TOKEN)
        with patch.object(transport.time, 'monotonic', return_value=220):
            self.assertIsNone(session.request_cookie(MEDIA, 'GET'))
        with patch.object(transport.time, 'monotonic', return_value=100):
            self.assertIsNone(session.request_cookie(MEDIA, 'GET'), 'expiry cleanup must be irreversible')

    def test_max_age_and_expires_only_shorten_the_local_ttl(self):
        for extra in ('Max-Age=2', 'Expires=Thu, 01 Jan 1970 00:16:42 GMT',
                      'Max-Age=5; Expires=Thu, 01 Jan 1970 00:16:42 GMT'):
            with self.subTest(extra=extra), patch.object(transport.time, 'time', return_value=1000), \
                    patch.object(transport.time, 'monotonic', return_value=10):
                session = self.session('tt_chain_token=' + TOKEN + '; Domain=tiktok.com; Path=/; ' + extra)
                self.assertEqual(session.request_cookie(MEDIA, 'GET'), 'tt_chain_token=' + TOKEN)
            with patch.object(transport.time, 'monotonic', return_value=12):
                self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_malformed_or_expired_cookie_cannot_be_used(self):
        for extra in ('Max-Age=0', 'Max-Age=-1', 'Max-Age=nan', 'Max-Age=99999999999',
                      'Expires=invalid', 'Expires=Thu, 01 Jan 1970 00:00:00 GMT',
                      'Max-Age=5; Max-Age=6'):
            with self.subTest(extra=extra), self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                self.session('tt_chain_token=x; Domain=.tiktok.com; Path=/; ' + extra)

    def test_public_status_data_and_download_mode_are_required_before_binding(self):
        data = {'id': 'fixture', 'video': {'playAddr': MEDIA}}
        cases = [(data, status, 'download') for status in (-1, 10216, 10222, 10204, True, '0')]
        cases += [({}, 0, 'download'), (data, 0, 'analyze')]
        for key in ('privateItem', 'isPrivate', 'secret', 'isSecret', 'isContentClassified'):
            cases.append((dict(data, **{key: True}), 0, 'download'))
        cases.append((dict(data, author={'privateAccount': True}), 0, 'download'))
        for index, (value, status, mode) in enumerate(cases):
            with self.subTest(case=index):
                session = transport._TikTokCdnSession()
                session.capture(SET_COOKIE, True)
                transport.STATE['mode'] = mode
                session.promote(value, status)
                session.bind({'url': MEDIA, 'protocol': 'https'})
                self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_selected_url_query_method_and_scope_cannot_change(self):
        for url, method in ((MEDIA + '&extra=1', 'GET'), (MEDIA.replace('exact', 'changed'), 'GET'),
                            (MEDIA.replace('v16-webapp-prime', 'v19'), 'GET'),
                            (MEDIA.replace('tiktok.com', 'tiktokcdn.com'), 'GET'),
                            (SOURCE, 'GET'), (MEDIA, 'HEAD'), (MEDIA, 'POST')):
            with self.subTest(url=url, method=method):
                session = self.session()
                self.assertIsNone(session.request_cookie(url, method))
                self.assertIsNone(session.request_cookie(MEDIA, 'GET'), 'mismatch must discard, not postpone, the token')

    def test_manifest_fragment_and_outside_cookie_domain_are_never_bound(self):
        for selected in ({'url': MEDIA.replace('tiktok.com', 'tiktokcdn.com'), 'protocol': 'https'},
                         {'url': SOURCE, 'protocol': 'https'}, {'url': MEDIA + '#fragment', 'protocol': 'https'},
                         {'url': MEDIA, 'protocol': 'm3u8_native'},
                         {'url': MEDIA, 'protocol': 'https', 'manifest_url': 'https://v16.tiktok.com/a.m3u8'},
                         {'url': MEDIA, 'protocol': 'https', 'fragments': [{'url': MEDIA}]},
                         {'url': 'https://v16.tiktok.com/list.m3u8?x=1', 'protocol': 'https'}):
            with self.subTest(selected=selected):
                session = self.session(bind=False)
                session.bind(selected)
                self.assertIsNone(session.request_cookie(selected['url'], 'GET'))

    def test_selected_binding_cannot_be_reassigned_to_another_format(self):
        session = self.session()
        following = MEDIA.replace('fixture.mp4', 'another.mp4')
        with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
            session.bind({'url': following, 'protocol': 'https'})
        self.assertIsNone(session.request_cookie(following, 'GET'))
        self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_binding_is_not_available_before_public_promotion(self):
        transport.STATE['mode'] = 'download'
        session = transport._TikTokCdnSession()
        session.capture(SET_COOKIE, True)
        session.bind({'url': MEDIA, 'protocol': 'https'})
        self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_analyze_does_not_retain_or_expose_the_server_token(self):
        with self.operation([http_response(public_page(), headers='Set-Cookie: ' + SET_COOKIE + '\r\n')]) as (downloader, _, _, directory):
            info = self.extract(downloader)
            session = transport.STATE['tiktok_session']
            transport.STATE['mode'] = 'download'
            session.cdn.bind({'url': MEDIA, 'protocol': 'https'})
            self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))
            self.assertEqual(list(session.jar), [])
            self.assertNotIn(TOKEN, json.dumps(info))
            self.assertNotIn(TOKEN, (directory / 'metadata.json').read_text())

    def test_cdn_cookie_is_separate_from_waf_and_external_headers_are_removed(self):
        session = transport._TikTokSession(None)
        session.cdn = self.session()
        handler = transport.DwnlRH(logger=MagicMock())
        handler.tiktok_session = session
        connection = MagicMock()
        connection.getresponse.return_value = http_response('data', status=206)
        sent_headers = []
        connection.request.side_effect = lambda *args, **kwargs: sent_headers.append(dict(kwargs['headers']))
        with patch.object(transport, '_PinnedConnection', return_value=connection):
            response = handler.send(Request(MEDIA, headers={'Cookie': 'sid_tt=account; tt_chain_token=evil',
                                                          'Authorization': 'Bearer account', 'Range': 'bytes=0-3'}))
            self.assertEqual(response.read(), b'data')
            response.close()
        sent = {key.lower(): value for key, value in sent_headers[0].items()}
        self.assertEqual(sent['cookie'], 'tt_chain_token=' + TOKEN)
        self.assertNotIn('authorization', sent)
        self.assertEqual(sent['range'], 'bytes=0-3')
        self.assertEqual(list(session.jar), [])
        session.purge()
        self.assertEqual(session.cdn.request_cookie(MEDIA, 'GET'), 'tt_chain_token=' + TOKEN)
        handler.close()
        self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))

    def test_cdn_redirect_or_refusal_is_terminal_and_discards_token(self):
        for status in (301, 302, 303, 307, 308, 401, 403, 429):
            with self.subTest(status=status):
                session = transport._TikTokSession(None)
                session.cdn = self.session()
                handler = transport.DwnlRH(logger=MagicMock())
                handler.tiktok_session = session
                connection = MagicMock()
                connection.getresponse.return_value = http_response(status=status, headers='Location: ' + MEDIA + '\r\n')
                with patch.object(transport, '_PinnedConnection', return_value=connection) as factory:
                    with self.assertRaisesRegex(transport.RequestError, 'DWNL_ACCESS_RESTRICTED'):
                        handler.send(Request(MEDIA))
                self.assertEqual(factory.call_count, 1)
                self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))
                connection.close.assert_called()

    def test_response_capture_requires_canonical_get_200_in_download_mode(self):
        cases = [(SOURCE, 'HEAD', 200), (MEDIA, 'GET', 200), (SOURCE, 'GET', 206)]
        for url, method, status in cases:
            with self.subTest(url=url, method=method, status=status):
                transport.STATE.update(platform='tiktok', mode='download')
                session = transport._TikTokSession(None)
                session.start_page(SOURCE)
                handler = transport.DwnlRH(logger=MagicMock())
                handler.tiktok_session = session
                connection = MagicMock()
                connection.getresponse.return_value = http_response(headers='Set-Cookie: ' + SET_COOKIE + '\r\n', status=status)
                with patch.object(transport, '_PinnedConnection', return_value=connection):
                    response = handler.send(Request(url, method=method))
                    response.read()
                    response.close()
                session.cdn.promote({'id': 'fixture', 'video': {'playAddr': MEDIA}}, 0)
                session.cdn.bind({'url': MEDIA, 'protocol': 'https'})
                self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))

    def test_connection_failure_and_cancellation_discard_the_token(self):
        for failure in (OSError('synthetic failure'), KeyboardInterrupt()):
            with self.subTest(failure=type(failure).__name__):
                session = transport._TikTokSession(None)
                session.cdn = self.session()
                handler = transport.DwnlRH(logger=MagicMock())
                handler.tiktok_session = session
                connection = MagicMock()
                connection.getresponse.side_effect = failure
                with patch.object(transport, '_PinnedConnection', return_value=connection):
                    with self.assertRaises(KeyboardInterrupt if isinstance(failure, KeyboardInterrupt) else transport.RequestError):
                        handler.send(Request(MEDIA))
                self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))
                connection.close.assert_called()

    def test_read_error_premature_close_and_budget_error_discard_token(self):
        for failure in (OSError('synthetic response error'), KeyboardInterrupt(), 'close', 'budget'):
            with self.subTest(failure=type(failure).__name__):
                session = self.session()
                reader = io.BytesIO(b'abcd')
                if isinstance(failure, BaseException):
                    reader = MagicMock()
                    reader.read.side_effect = failure
                body = transport._LimitedBody(reader, MagicMock(), 'identity', session)
                if failure == 'close':
                    body.close()
                else:
                    if failure == 'budget':
                        transport.STATE['byte_limit'] = 2
                    with self.assertRaises(KeyboardInterrupt if isinstance(failure, KeyboardInterrupt) else transport.RequestError):
                        body.read(4)
                self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_zero_length_read_is_not_eof_and_premature_close_discards_token(self):
        session = self.session()
        response = http_response('unread-data', status=206)
        body = transport._LimitedBody(response, MagicMock(), 'identity', session)
        self.assertEqual(body.read(0), b'')
        self.assertFalse(response.isclosed())
        body.close()
        self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_gzip_buffered_tail_is_not_complete_when_wire_has_closed(self):
        session = self.session()
        compressed = gzip.compress(b'large-unread-tail' * 100)
        response = wire_response(b'HTTP/1.1 200 OK\r\nContent-Length: ' + str(len(compressed)).encode() + b'\r\n\r\n' + compressed)
        body = transport._LimitedBody(response, MagicMock(), 'gzip', session)
        self.assertEqual(body.read(1), b'l')
        self.assertTrue(response.isclosed(), 'fixture must exercise a fully buffered compressed wire body')
        body.close()
        self.assertIsNone(session.request_cookie(MEDIA, 'GET'))

    def test_complete_gzip_metadata_read_preserves_candidate_for_selection(self):
        transport.STATE.update(platform='tiktok', mode='download')
        session = transport._TikTokCdnSession()
        session.capture(SET_COOKIE, True)
        compressed = gzip.compress(b'public-metadata')
        response = wire_response(b'HTTP/1.1 200 OK\r\nContent-Length: ' + str(len(compressed)).encode() + b'\r\n\r\n' + compressed)
        body = transport._LimitedBody(response, MagicMock(), 'gzip', session)
        self.assertEqual(body.read(), b'public-metadata')
        body.close()
        session.promote({'id': 'fixture', 'video': {'playAddr': MEDIA}}, 0)
        session.bind({'url': MEDIA, 'protocol': 'https'})
        self.assertEqual(session.request_cookie(MEDIA, 'GET'), 'tt_chain_token=' + TOKEN)
        session.close()

    def test_complete_range_close_allows_same_url_next_range_without_renewal(self):
        session = transport._TikTokSession(None)
        with patch.object(transport.time, 'monotonic', return_value=100):
            session.cdn = self.session()
        handler = transport.DwnlRH(logger=MagicMock())
        handler.tiktok_session = session
        connections = [MagicMock(), MagicMock()]
        sent_headers = []
        for connection in connections:
            connection.getresponse.return_value = http_response('data', status=206)
            connection.request.side_effect = lambda *args, **kwargs: sent_headers.append(dict(kwargs['headers']))
        with patch.object(transport, '_PinnedConnection', side_effect=connections), \
                patch.object(transport.time, 'monotonic', return_value=219):
            for offset in (0, 4):
                response = handler.send(Request(MEDIA, headers={'Range': f'bytes={offset}-{offset+3}'}))
                self.assertEqual(response.read(4), b'data')
                response.close()
        for headers in sent_headers:
            self.assertEqual(headers['Cookie'], 'tt_chain_token=' + TOKEN)
        with patch.object(transport.time, 'monotonic', return_value=220):
            self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))

    def test_invalid_selection_purges_before_any_media_request(self):
        session = transport._TikTokSession(None)
        session.cdn = self.session()
        transport.STATE['tiktok_session'] = session
        with self.assertRaisesRegex(Exception, 'DWNL_INVALID_SELECTION'):
            transport.validate_selection({'id': 'fixture', 'formats': [{'format_id': 'actual'}]},
                                         {'id': 'fixture', 'format': {'format_id': 'changed'}, 'budget_bytes': 1024})
        self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))

    def test_real_extraction_private_status_or_changed_selection_discards_candidate(self):
        cases = [(10216, 'play'), (10222, 'play'), (10204, 'play'), (0, 'changed-format')]
        for status, identity in cases:
            with self.subTest(status=status, identity=identity):
                page = public_page(status).replace('https://v16.tiktokcdn.com/synthetic.mp4', MEDIA)
                with self.operation([http_response(page, headers='Set-Cookie: ' + SET_COOKIE + '\r\n')]) as (downloader, _, factory, directory):
                    downloader.params['extractor_args'] = {'dwnl': {'mode': ['download'], 'item': ['7123456789012345678']}}
                    (directory / 'selection.json').write_text(json.dumps({
                        'id': '7123456789012345678', 'format': {'format_id': identity}, 'budget_bytes': 1024,
                    }))
                    with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED|DWNL_INVALID_SELECTION|permission|blocked'):
                        self.extract(downloader)
                    session = transport.STATE['tiktok_session']
                    session.cdn.bind({'url': MEDIA, 'protocol': 'https'})
                    self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))
                    self.assertEqual(factory.call_count, 1)
                    self.assertEqual(list(session.jar), [])

    def test_response_token_validation_error_closes_connection_without_logging_value(self):
        transport.STATE.update(platform='tiktok', mode='download')
        session = transport._TikTokSession(None)
        session.start_page(SOURCE)
        handler = transport.DwnlRH(logger=MagicMock())
        handler.tiktok_session = session
        connection = MagicMock()
        connection.getresponse.return_value = http_response(headers='Set-Cookie: ' + SET_COOKIE + '; Domain=.com\r\n')
        output = io.StringIO()
        with patch.object(transport, '_PinnedConnection', return_value=connection), contextlib.redirect_stderr(output):
            with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED') as caught:
                handler.send(Request(SOURCE))
        self.assertNotIn(TOKEN, str(caught.exception) + output.getvalue())
        connection.close.assert_called()
        self.assertTrue(connection.getresponse.return_value.isclosed())

    def test_policy_validation_failure_clears_previously_bound_token(self):
        session = transport._TikTokSession(None)
        session.cdn = self.session()
        handler = transport.DwnlRH(logger=MagicMock())
        handler.tiktok_session = session
        with self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
            handler.validate(Request(MEDIA, proxies={'https': 'http://127.0.0.1:9999'}))
        self.assertIsNone(session.cdn.request_cookie(MEDIA, 'GET'))

    def test_native_music_mp4_container_is_not_offered_as_clip_audio(self):
        # Pinned upstream infers an extension from mime_type then also uses
        # that extension as acodec. MP4 identifies a container, not its codec.
        data = {'video': {'playAddr': MEDIA, 'duration': 1, 'width': 160, 'height': 90},
                'music': {'playUrl': 'https://v16m.tiktokcdn.com/music?mime_type=video_mp4'}}
        formats = transport.TikTokIE(None)._extract_web_formats(data)
        native_audio = next(fmt for fmt in formats if fmt['format_id'] == 'audio')
        self.assertEqual((native_audio['ext'], native_audio['acodec'], native_audio['vcodec']), ('mp4', 'mp4', 'none'))
        transport.STATE['platform'] = 'tiktok'
        item = transport._public_items({'id': 'fixture', 'duration': 1,
                                        'formats': transport._TikTok(None)._extract_web_formats(data)})[0]
        self.assertEqual([(fmt['format_id'], fmt['url'], fmt['acodec']) for fmt in item['formats']], [('play', MEDIA, 'aac')])

    def test_native_associated_music_is_removed_even_with_a_known_aac_codec(self):
        data = {'music': {'playUrl': 'https://v16m.tiktokcdn.com/music?mime_type=audio_mp4'}}
        audio = transport.TikTokIE(None)._extract_web_formats(data)[0]
        self.assertEqual((audio['format_id'], audio['ext'], audio['acodec'], audio['vcodec']),
                         ('audio', 'm4a', 'aac', 'none'))
        self.assertEqual(transport._TikTok(None)._extract_web_formats(data), [])

    def test_public_projection_keeps_original_codec_metadata_for_all_platforms(self):
        cases = [
            ('youtube', 'none', 'mp4', 'mp4', 'mp4'),
            ('instagram', 'none', 'mp4', 'mp4', 'mp4'),
            ('x', 'none', 'mp4', 'mp4', 'mp4'),
            ('tiktok', 'h264', 'mp4', 'mp4', 'mp4'),
            ('tiktok', 'none', 'aac', 'mp4', 'aac'),
            ('tiktok', 'none', 'mp4a.40.2', 'mp4', 'mp4a.40.2'),
            ('tiktok', 'none', 'opus', 'webm', 'opus'),
            ('tiktok', 'none', 'mp4', 'mp4', 'mp4'),
            ('tiktok', 'none', 'mp4', 'm4a', 'mp4'),
            ('tiktok', 'none', 'unknown', 'mp4', 'unknown'),
        ]
        for platform, video, audio, ext, expected in cases:
            with self.subTest(platform=platform, video=video, audio=audio, ext=ext):
                transport.STATE['platform'] = platform
                record = {'id': 'fixture', 'formats': [{'format_id': 'selected', 'url': MEDIA, 'protocol': 'https',
                                                       'vcodec': video, 'acodec': audio, 'ext': ext}]}
                projected = transport._public_items(record)[0]['formats'][0]
                self.assertEqual(projected['acodec'], expected)
                self.assertEqual((projected['format_id'], projected['url'], projected['vcodec'], projected['ext']),
                                 ('selected', MEDIA, video, ext))

    def test_original_clip_audio_is_consistent_across_analysis_and_selection(self):
        transport.STATE['platform'] = 'tiktok'
        def extraction():
            data = {'video': {'playAddr': MEDIA},
                    'music': {'playUrl': 'https://v16m.tiktokcdn.com/music?mime_type=video_mp4'}}
            return transport._public_items({'id': 'fixture', 'formats': transport._TikTok(None)._extract_web_formats(data)})[0]
        analyzed = extraction()
        guard = {'id': 'fixture', 'format': {'format_id': 'play', 'vcodec': 'h264', 'acodec': 'aac'}, 'budget_bytes': 1024}
        self.assertEqual(analyzed['formats'][0]['acodec'], 'aac')
        selected = transport.validate_selection(extraction(), guard)
        self.assertEqual((selected['format_id'], selected['acodec'], selected['url']),
                         ('play', 'aac', MEDIA))

    def test_native_music_track_never_replaces_known_original_clip_audio(self):
        music_url = 'https://v16m.tiktokcdn.com/music-180-seconds?mime_type=audio_mp4'
        data = {'id': 'fixture', 'desc': 'Six second clip with voice and background music',
                'author': {'uniqueId': 'fixture'},
                'video': {'playAddr': MEDIA, 'duration': 6, 'width': 160, 'height': 90},
                'music': {'playUrl': music_url, 'duration': 180}}
        with YoutubeDL({'quiet': True, 'no_warnings': True, 'js_runtimes': {}}, auto_init=False) as downloader, \
                patch.object(transport.socket, 'socket', side_effect=AssertionError('offline codec test attempted socket')):
            native = transport.TikTokIE(downloader)._parse_aweme_video_web(data, SOURCE, 'fixture')
            owned = transport._TikTok(downloader)._parse_aweme_video_web(data, SOURCE, 'fixture')
        self.assertEqual(native['duration'], 6)
        self.assertEqual([(fmt['format_id'], fmt['url']) for fmt in native['formats']], [('play', MEDIA), ('audio', music_url)])
        transport.STATE['platform'] = 'tiktok'
        projected = transport._public_items(owned)[0]
        self.assertEqual([(fmt['format_id'], fmt['url'], fmt['acodec']) for fmt in projected['formats']],
                         [('play', MEDIA, 'aac')], 'the separate music track must not displace voice/mixed audio in the clip')

    def test_associated_track_is_excluded_regardless_of_video_codec_or_duration(self):
        music_url = 'https://v16m.tiktokcdn.com/library.mp4'
        for video, audio in (('h264', 'aac'), ('h264', 'unknown'), ('h264', 'none'), ('unknown', 'unknown'), ('none', 'none')):
            for music_duration in (6, 180):
                with self.subTest(video=video, audio=audio, music_duration=music_duration):
                    data = {'video': {'duration': 6}, 'music': {'playUrl': music_url, 'duration': music_duration}}
                    native = [{'format_id': 'play', 'url': MEDIA, 'vcodec': video, 'acodec': audio, 'ext': 'mp4'},
                              {'format_id': 'audio', 'url': music_url, 'vcodec': 'none', 'acodec': 'aac', 'ext': 'm4a'}]
                    with patch.object(transport.TikTokIE, '_extract_web_formats', return_value=native):
                        result = transport._TikTok(None)._extract_web_formats(data)
                    self.assertEqual(result, [native[0]])
                    self.assertIs(result[0], native[0], 'original clip format must be preserved intact')

    def test_only_matching_native_music_record_is_removed_and_other_records_keep_order(self):
        music_url = 'https://v16m.tiktokcdn.com/library.mp4?source=one'
        native = [
            {'format_id': 'play', 'url': MEDIA, 'vcodec': 'h264', 'acodec': 'aac', 'ext': 'mp4'},
            {'format_id': 'audio-v2', 'url': music_url, 'vcodec': 'none', 'acodec': 'aac', 'ext': 'm4a'},
            {'format_id': 'audio', 'url': music_url + '&other=1', 'vcodec': 'none', 'acodec': 'aac', 'ext': 'm4a'},
            {'format_id': 'audio', 'url': music_url, 'vcodec': 'h264', 'acodec': 'aac', 'ext': 'mp4'},
            {'format_id': 'audio', 'url': music_url, 'vcodec': 'none', 'acodec': 'mp4', 'ext': 'mp4'},
        ]
        with patch.object(transport.TikTokIE, '_extract_web_formats', return_value=native):
            result = transport._TikTok(None)._extract_web_formats({'music': {'playUrl': music_url}})
        self.assertEqual(result, native[:4])
        self.assertTrue(all(actual is original for actual, original in zip(result, native[:4])))

    def test_protocol_relative_music_provenance_is_normalized_before_comparison(self):
        music_url = '//v16m.tiktokcdn.com/library.mp4?source=one'
        native = [{'format_id': 'audio', 'url': 'https:' + music_url, 'vcodec': 'none', 'acodec': 'aac', 'ext': 'm4a'}]
        with patch.object(transport.TikTokIE, '_extract_web_formats', return_value=native):
            result = transport._TikTok(None)._extract_web_formats({'music': {'playUrl': music_url}})
        self.assertEqual(result, [])

    def test_missing_music_provenance_keeps_all_original_formats(self):
        native = [{'format_id': 'audio', 'url': MEDIA, 'vcodec': 'none', 'acodec': 'aac', 'ext': 'm4a'}]
        for data in ({}, {'music': None}, {'music': 'invalid'}, {'music': {}}, {'music': {'playUrl': ''}}):
            with self.subTest(data=data), patch.object(transport.TikTokIE, '_extract_web_formats', return_value=native):
                result = transport._TikTok(None)._extract_web_formats(data)
                self.assertIs(result, native)


if __name__ == '__main__':
    unittest.main()
