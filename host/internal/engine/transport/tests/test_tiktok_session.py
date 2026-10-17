"""Anonymous TikTok verification over synthetic HTTP, never a real site."""
import base64
import contextlib
import hashlib
import http.cookies
import io
import json
import os
import pathlib
import tempfile
import time
import unittest
import urllib.request
from unittest.mock import MagicMock, patch

from yt_dlp import YoutubeDL
from yt_dlp.networking.common import Request
from yt_dlp.cookies import YoutubeDLCookieJar

# Reuse the existing plugin module: importing a second copy would replace the
# global request-handler registry with classes from a different module instance.
from test_transport import transport, wire_response


SOURCE = 'https://www.tiktok.com/@fixture/video/7123456789012345678'


def challenge_page(nonce=2, name='_wafchallengeid', optional=True, seed=b'synthetic-anonymous-verification', digest=None):
    data = {'v': {'a': base64.b64encode(seed).decode(),
                  'c': base64.b64encode(digest if digest is not None else hashlib.sha256(seed + str(nonce).encode()).digest()).decode()}}
    encoded = base64.b64encode(json.dumps(data, separators=(',', ':')).encode()).decode()
    return (f'<html><div id="cs" class="{encoded}"></div><div id="wci" class="{name}"></div>'
            + ('<div id="rci" class="waforiginalreid"></div><div id="rs" class="synthetic-reid"></div>' if optional else '')
            + '<p>Please wait...</p></html>')


def public_page(status=0):
    data = {'__DEFAULT_SCOPE__': {'webapp.video-detail': {'statusCode': status, 'itemInfo': {'itemStruct': {
        'id': '7123456789012345678', 'desc': 'Synthetic public clip', 'author': {'uniqueId': 'fixture'},
        'video': {'playAddr': 'https://v16.tiktokcdn.com/synthetic.mp4', 'duration': 1, 'width': 160, 'height': 90},
    }}}}}
    return '<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">' + json.dumps(data) + '</script>'


def http_response(body='', status=200, headers=''):
    encoded = body.encode()
    return wire_response(f'HTTP/1.1 {status} Synthetic\r\nContent-Length: {len(encoded)}\r\n{headers}\r\n'.encode() + encoded)


class TikTokAnonymousSessionTests(unittest.TestCase):
    def setUp(self):
        self.state = patch.dict(transport.STATE, {'platform': None, 'mode': 'analyze', 'bytes': 0,
                                                 'requests': 0, 'byte_limit': None, 'tiktok_session': None}, clear=True)
        self.state.start()
        self.addCleanup(self.state.stop)

    @contextlib.contextmanager
    def operation(self, responses):
        connections = []
        for response in responses:
            connection = MagicMock()
            connection.getresponse.return_value = response
            def capture(*args, _connection=connection, **kwargs):
                _connection.sent_headers = dict(kwargs['headers'])
                session = transport.STATE.get('tiktok_session')
                _connection.cookie_attributes = [(cookie.name, cookie.domain, cookie.path, cookie.secure, cookie.discard, cookie.expires)
                                                 for cookie in session.jar] if session else []
            connection.request.side_effect = capture
            connections.append(connection)
        with tempfile.TemporaryDirectory() as directory, patch.dict(transport.STATE, {'tiktok_session': None}):
            previous = os.getcwd()
            os.chdir(directory)
            try:
                with YoutubeDL({'quiet': True, 'no_warnings': True, 'js_runtimes': {},
                                'extractor_args': {'dwnl': {'mode': ['analyze']}}}, auto_init=False) as downloader:
                    with patch.object(transport, '_PinnedConnection', side_effect=connections) as factory, \
                            patch.object(transport.socket, 'socket', side_effect=AssertionError('offline test attempted socket')):
                        yield downloader, connections, factory, pathlib.Path(directory)
            finally:
                os.chdir(previous)

    def extract(self, downloader):
        return transport.DwnlIE(downloader).extract(SOURCE)

    def test_real_upstream_verification_sends_only_scoped_waf_then_clears(self):
        with self.operation([http_response(challenge_page()), http_response(public_page())]) as (downloader, connections, factory, directory):
            try:
                info = self.extract(downloader)
            except Exception as error:
                self.fail(f'authorized anonymous verification did not reach public metadata: {error}')
            self.assertEqual(info['id'], '7123456789012345678')
            self.assertEqual(factory.call_count, 2)
            first = connections[0].sent_headers
            second = connections[1].sent_headers
            self.assertNotIn('Cookie', first)
            cookies = http.cookies.SimpleCookie(second['Cookie'])
            self.assertEqual(set(cookies), {'_wafchallengeid', 'waforiginalreid'})
            decoded = json.loads(base64.b64decode(cookies['_wafchallengeid'].value))
            self.assertEqual(base64.b64decode(decoded['d']), b'2')
            self.assertEqual(connections[1].request.call_args.args[:2], ('GET', '/@fixture/video/7123456789012345678'))
            handler = next(iter(downloader._request_director.handlers.values()))
            self.assertIs(downloader.cookiejar, handler.cookiejar)
            self.assertEqual(list(downloader.cookiejar), [])
            for _, domain, path, secure, discard, expiry in connections[1].cookie_attributes:
                self.assertEqual((domain, path, secure, discard), ('.tiktok.com', '/', True, True))
                self.assertLessEqual(expiry, int(time.time()) + 120)
            self.assertEqual({file.name for file in directory.iterdir()}, {'metadata.json'})
            metadata = (directory / 'metadata.json').read_text()
            self.assertNotIn('_wafchallengeid', metadata)
            self.assertNotIn('synthetic-reid', metadata)

    def test_repeated_challenge_stops_after_two_gets_with_empty_jar(self):
        with self.operation([http_response(challenge_page()), http_response(challenge_page())]) as (downloader, _, factory, _):
            with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                self.extract(downloader)
            self.assertEqual(factory.call_count, 2)
            self.assertEqual(list(downloader.cookiejar), [])

    def test_empty_optional_cookie_value_is_omitted_from_retry(self):
        page = challenge_page().replace('class="synthetic-reid"', 'class=""')
        with self.operation([http_response(page), http_response(public_page())]) as (downloader, connections, factory, _):
            info = self.extract(downloader)
            self.assertEqual(info['id'], '7123456789012345678')
            self.assertEqual(factory.call_count, 2)
            cookies = http.cookies.SimpleCookie(connections[1].sent_headers['Cookie'])
            self.assertEqual(set(cookies), {'_wafchallengeid'})
            self.assertEqual([cookie[0] for cookie in connections[1].cookie_attributes], ['_wafchallengeid'])
            self.assertEqual(list(downloader.cookiejar), [])

    def test_optional_cookie_value_without_allowed_name_never_retries(self):
        for page in (challenge_page().replace('id="rci" class="waforiginalreid"', 'id="rci" class=""'),
                     challenge_page().replace('id="rci" class="waforiginalreid"', 'id="rci" class="sid_tt"')):
            with self.subTest(pageKind='unsupported-optional-name'), self.operation([http_response(page)]) as (downloader, _, factory, _):
                with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                    self.extract(downloader)
                self.assertEqual(factory.call_count, 1)
                self.assertEqual(list(downloader.cookiejar), [])

    def test_untrusted_cookie_name_never_reaches_retry(self):
        for name in ('sid_tt', 'tt_chain_token', 'other_cookie'):
            with self.subTest(name=name), self.operation([http_response(challenge_page(name=name))]) as (downloader, _, factory, _):
                with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                    self.extract(downloader)
                self.assertEqual(factory.call_count, 1)
                self.assertEqual(list(downloader.cookiejar), [])

    def test_http_refusal_after_verification_is_terminal_and_cleans_jar(self):
        for status in (401, 403, 429):
            with self.subTest(status=status), self.operation([http_response(challenge_page()), http_response(status=status)]) as (downloader, _, factory, _):
                with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                    self.extract(downloader)
                self.assertEqual(factory.call_count, 2)
                self.assertEqual(list(downloader.cookiejar), [])

    def test_verification_redirect_does_not_forward_cookies_or_issue_third_get(self):
        with self.operation([http_response(challenge_page()), http_response(status=302, headers='Location: https://v16.tiktokcdn.com/other\r\n')]) as (downloader, _, factory, _):
            with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                self.extract(downloader)
            self.assertEqual(factory.call_count, 2)
            self.assertEqual(list(downloader.cookiejar), [])

    def test_unknown_markup_is_not_sent_to_upstream_algorithm(self):
        with self.operation([http_response('<html><p>Unexpected public markup</p></html>')]) as (downloader, _, factory, _):
            with patch.object(transport.TikTokIE, '_solve_challenge_and_set_cookies', side_effect=AssertionError('unrecognized algorithm')) as solver:
                with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                    self.extract(downloader)
                solver.assert_not_called()
            self.assertEqual(factory.call_count, 1)

    def test_cookie_sources_are_rejected_before_cached_properties(self):
        class SuppliedCookies:
            @property
            def cookiejar(self):
                raise AssertionError('cookie source was loaded')

            @property
            def _request_director(self):
                raise AssertionError('request director was evaluated before source check')

        for params in ({'cookiefile': 'private.txt'}, {'cookiefile': ''}, {'cookiesfrombrowser': ('chrome',)}):
            with self.subTest(params=params):
                downloader = SuppliedCookies()
                downloader.params = params
                with self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
                    transport.DwnlIE(downloader)._real_extract(SOURCE)

    def test_another_ie_cannot_restart_verification_in_same_process(self):
        with self.operation([http_response(challenge_page()), http_response(public_page())]) as (downloader, _, factory, directory):
            self.extract(downloader)
            metadata = (directory / 'metadata.json').read_bytes()
            with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                self.extract(downloader)
            self.assertEqual(factory.call_count, 2)
            self.assertEqual((directory / 'metadata.json').read_bytes(), metadata)
            self.assertTrue(transport.STATE['tiktok_session'].attempted)

    def test_direct_cookie_insertions_cannot_escape_name_domain_path_or_expiry(self):
        cases = [
            ('.tiktok.com', 'sid_tt', 'value', {}),
            ('.tiktok.com', 'odin_tt', 'value', {}),
            ('.tiktok.com', 'tt_chain_token', 'value', {}),
            ('.com', '_wafchallengeid', 'value', {}),
            ('.tiktokcdn.com', '_wafchallengeid', 'value', {}),
            ('.tiktok.com', '_wafchallengeid', 'value', {'path': '/different'}),
            ('.tiktok.com', '_wafchallengeid', 'bad;cookie=other', {}),
            ('.tiktok.com', '_wafchallengeid', 'x' * 4097, {}),
            ('.tiktok.com', '_wafchallengeid', 'value', {'expire_time': 1}),
        ]
        for domain, name, value, options in cases:
            with self.subTest(domain=domain, name=name, options=options), self.operation([http_response(challenge_page())]) as (downloader, _, factory, _):
                accepted = []
                def unsafe_upstream(extractor, webpage):
                    transport.InfoExtractor._set_cookie(extractor, domain, name, value, **options)
                    accepted.append(name)
                    return ('_wafchallengeid', None)
                with patch.object(transport.TikTokIE, '_solve_challenge_and_set_cookies', unsafe_upstream):
                    with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                        self.extract(downloader)
                self.assertEqual(factory.call_count, 1)
                self.assertEqual(accepted, [], 'unsafe upstream cookie was accepted before failure')
                self.assertEqual(list(downloader.cookiejar), [])

    def test_cookie_policy_and_persistence_remain_closed_after_page(self):
        with self.operation([http_response(public_page(), headers='Set-Cookie: sid_tt=account; Domain=.tiktok.com; Path=/\r\nSet-Cookie: _wafchallengeid=untrusted; Domain=.tiktok.com; Path=/\r\n')]) as (downloader, _, factory, directory):
            self.extract(downloader)
            jar = downloader.cookiejar
            for name in ('sid_tt', '_wafchallengeid', 'tt_chain_token'):
                response = http_response(headers=f'Set-Cookie: {name}=server; Domain=.tiktok.com; Path=/; Secure\r\n')
                jar.extract_cookies(response, urllib.request.Request(SOURCE))
                response.close()
            self.assertEqual(list(jar), [])
            for method in ('load', 'revert', 'save'):
                with self.subTest(method=method), self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
                    getattr(jar, method)(str(directory / 'cookies.txt'))
            with self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
                transport.InfoExtractor._set_cookie(transport._TikTok(downloader), '.tiktok.com', '_wafchallengeid', 'value')
            handler = next(iter(downloader._request_director.handlers.values()))
            with self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
                handler.send(Request(SOURCE, extensions={'cookiejar': YoutubeDLCookieJar()}))
            self.assertEqual(factory.call_count, 1)
            self.assertFalse((directory / 'cookies.txt').exists())

    def test_direct_cookie_domain_aliases_share_only_two_canonical_keys(self):
        session = transport._TikTokSession(None)
        session.phase, session.canonical_url = 'solving', SOURCE
        for domain in ('.tiktok.com', 'tiktok.com', 'www.tiktok.com'):
            for name in ('_wafchallengeid', 'waforiginalreid'):
                cookie = http.cookiejar.Cookie(
                    0, name, 'synthetic', None, False, domain, True, domain.startswith('.'),
                    '/', True, False, None, True, None, None, {})
                session.jar.set_cookie(cookie)
        self.assertEqual(len(session.jar), 2)
        self.assertEqual({cookie.domain for cookie in session.jar}, {'.tiktok.com'})
        self.assertTrue(all(cookie.domain_specified and cookie.domain_initial_dot for cookie in session.jar))
        session.phase = 'ready'
        header = session.jar.get_cookie_header(SOURCE)
        self.assertEqual(len(header.split('; ')), 2)
        self.assertEqual(set(http.cookies.SimpleCookie(header)), {'_wafchallengeid', 'waforiginalreid'})
        session.close()

    def test_direct_cookie_expiry_rejects_noninteger_and_nonfinite_values(self):
        for expiry in (float('nan'), float('inf'), -float('inf'), time.time() + 60, True, 'future'):
            with self.subTest(expiry=repr(expiry)):
                session = transport._TikTokSession(None)
                session.phase = 'solving'
                cookie = http.cookiejar.Cookie(
                    0, '_wafchallengeid', 'synthetic', None, False, '.tiktok.com', True, True,
                    '/', True, False, None, True, None, None, {})
                # The constructor itself coerces expiry to int. Mutate after
                # construction to exercise the direct-insertion boundary.
                cookie.expires = expiry
                with self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
                    session.jar.set_cookie(cookie)
                self.assertEqual(list(session.jar), [])
                session.close()

    def test_short_url_only_sends_verification_to_canonical_video_get(self):
        with self.operation([http_response(status=302, headers=f'Location: {SOURCE}\r\n'), http_response(),
                             http_response(challenge_page()), http_response(public_page())]) as (downloader, connections, factory, _):
            transport.DwnlIE(downloader).extract('https://vm.tiktok.com/ZSynthetic/')
            self.assertEqual(factory.call_count, 4)
            self.assertEqual([connection.request.call_args.args[0] for connection in connections], ['HEAD', 'HEAD', 'GET', 'GET'])
            for connection in connections[:-1]:
                self.assertNotIn('Cookie', connection.sent_headers)
            self.assertIn('Cookie', connections[-1].sent_headers)

    def test_failed_and_interrupted_retry_always_cleans_cookie_memory(self):
        for failure in (OSError('synthetic connection failure'), KeyboardInterrupt()):
            with self.subTest(failure=type(failure).__name__), self.operation([http_response(challenge_page()), http_response()]) as (downloader, connections, factory, _):
                connections[1].getresponse.side_effect = failure
                with self.assertRaises(KeyboardInterrupt if isinstance(failure, KeyboardInterrupt) else transport.RequestError):
                    self.extract(downloader)
                self.assertEqual(factory.call_count, 2)
                self.assertEqual(list(downloader.cookiejar), [])

    def test_input_digest_and_computation_budget_are_finite(self):
        for page in (challenge_page(seed=b''), challenge_page(seed=b'x' * 4097), challenge_page(digest=b'bad'),
                     challenge_page() + 'x' * 65536, challenge_page(nonce=1_000_001)):
            with self.subTest(length=len(page)), self.operation([http_response(page)]) as (downloader, _, factory, _):
                with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                    self.extract(downloader)
                self.assertEqual(factory.call_count, 1)
                self.assertEqual(list(downloader.cookiejar), [])

    def test_no_waf_values_or_html_are_logged(self):
        output = io.StringIO()
        with self.operation([http_response(challenge_page()), http_response(public_page())]) as (downloader, _, _, _), \
                contextlib.redirect_stdout(output), contextlib.redirect_stderr(output):
            self.extract(downloader)
        for secret in ('synthetic-reid', 'synthetic-anonymous-verification', '<html>', '"v":', '_wafchallengeid'):
            self.assertNotIn(secret, output.getvalue())


if __name__ == '__main__':
    unittest.main()
