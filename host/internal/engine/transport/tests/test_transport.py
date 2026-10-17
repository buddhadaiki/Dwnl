"""Regressions for SSRF, redirect and request boundaries, not site mocks."""
import importlib.util
import pathlib
import socket
import io
import http.client
import unittest
from unittest.mock import patch, MagicMock
from yt_dlp.networking.common import Request
from yt_dlp import YoutubeDL

ADAPTER = pathlib.Path(__file__).resolve().parents[1] / 'yt_dlp_plugins/extractor/dwnl.py'
SPEC = importlib.util.spec_from_file_location('dwnl_transport', ADAPTER)
transport = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(transport)


class URLPolicyTests(unittest.TestCase):
    def setUp(self):
        transport.STATE.update(platform=None,mode='analyze',bytes=0,requests=0,byte_limit=None)
    def test_unauthorized_protocol_credentials_ports_and_domains_are_rejected(self):
        for raw in ('file:///secret', 'http://127.0.0.1/', 'http://www.youtube.com/stream', 'https://localhost/',
                    'https://youtube.com.evil.test/', 'https://user:pass@youtube.com/',
                    'https://www.youtube.com:444/', 'https://www.youtube.com./',
                    'https://www.youtube.com\\@evil.test/', 'https://video.twimg.com/file.mp4'):
            with self.subTest(raw=raw), self.assertRaises(Exception):
                transport.checked_url(raw, 'youtube')

    def test_official_platform_and_cdn_hosts_are_accepted(self):
        for raw, platform in (('https://www.youtube.com/watch?v=public', 'youtube'),
                              ('https://rr1---sn.example.googlevideo.com/videoplayback', 'youtube'),
                              ('https://scontent.cdninstagram.com/media.mp4', 'instagram'),
                              ('https://v16.tiktokcdn.com/media.mp4', 'tiktok'),
                              ('https://video.twimg.com/media.mp4', 'x')):
            self.assertEqual(transport.checked_url(raw, platform).hostname, raw.split('/')[2])

    def test_mixed_dns_and_ipv6_private_answers_are_rejected(self):
        for answers in (['8.8.8.8', '127.0.0.1'], ['10.1.2.3'], ['169.254.169.254'],
                        ['100.64.1.2'], ['::1'], ['fd00::1'], ['fe80::1'], ['::ffff:127.0.0.1']):
            def resolver(host, port, **kwargs):
                return [(socket.AF_INET6 if ':' in address else socket.AF_INET,
                         socket.SOCK_STREAM, socket.IPPROTO_TCP, '', (address, port)) for address in answers]
            with self.subTest(answers=answers), self.assertRaises(Exception):
                transport.public_addresses('www.youtube.com', 443, resolver)

    def test_dns_returns_fixed_public_endpoints(self):
        def resolver(host, port, **kwargs):
            return [(socket.AF_INET, socket.SOCK_STREAM, socket.IPPROTO_TCP, '', ('8.8.8.8', port))]
        self.assertEqual(transport.public_addresses('www.youtube.com', 443, resolver), [('8.8.8.8', 443)])

    def test_youtube_agegate_stops_before_alternative_client(self):
        with self.assertRaises(Exception):
            transport._YouTube._is_agegated({'playabilityStatus': {'status': 'LOGIN_REQUIRED', 'reason': 'Sign in to confirm your age'}})

    def test_tiktok_challenge_is_not_solved(self):
        with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
            transport._TikTok(None)._solve_challenge_and_set_cookies('<html>challenge</html>')

    def test_external_downloader_cannot_receive_a_url(self):
        from yt_dlp.downloader.external import FFmpegFD
        with self.assertRaisesRegex(Exception, 'DWNL_NETWORK_POLICY'):
            FFmpegFD.real_download(None, 'unused', {'url': 'https://www.youtube.com/stream'})

    def test_native_format_protocol_none_is_derived_without_losing_format(self):
        record={'id':'public','duration':2,'formats':[{'format_id':'18','protocol':None,'url':'https://rr1.googlevideo.com/media','vcodec':'avc1.640028','acodec':'mp4a.40.2'}]}
        items=transport._public_items(record)
        self.assertEqual(len(items[0]['formats']),1)
        self.assertEqual(items[0]['formats'][0]['protocol'],'https')

    def test_changed_selection_is_rejected_before_downloader_runs(self):
        guard={'id':'public','duration':2,'format':{'format_id':'v','vcodec':'h264','acodec':'none','width':160,'height':90}}
        item={'id':'public','duration':2,'formats':[{'format_id':'v','vcodec':'h264','acodec':'none','width':160,'height':360}]}
        with self.assertRaisesRegex(Exception,'DWNL_INVALID_SELECTION'):
            transport.validate_selection(item,guard)

    def test_native_missing_and_duplicate_ids_preserve_actual_formats(self):
        record={'id':'public','formats':[{'url':'https://v.tiktokcdn.com/a','format_id':'quality'}, {'url':'https://v.tiktokcdn.com/b','format_id':'quality'}, {'url':'https://v.tiktokcdn.com/c'}]}
        formats=transport._public_items(record)[0]['formats']
        self.assertEqual(len(formats),3)
        self.assertEqual(len({f['format_id'] for f in formats}),3)
        self.assertEqual(formats[2]['acodec'],'unknown')

    def test_alternative_youtube_client_stops_before_native_request(self):
        for client in ('android_vr', 'web_embedded', 'web_embedded.visionos', 'tv', 'tv_downgraded', 'ios', 'all'):
            with self.subTest(client=client), patch.object(transport.YoutubeIE,'_extract_player_response') as native:
                with self.assertRaisesRegex(Exception,'DWNL_ACCESS_RESTRICTED'):
                    transport._YouTube(None)._extract_player_response(client)
                native.assert_not_called()

    def test_current_public_youtube_clients_keep_native_player_response(self):
        response = {'playabilityStatus': {'status': 'OK'}, 'videoDetails': {'videoId': 'public-video'}}
        for client in ('visionos', 'web'):
            with self.subTest(client=client), patch.object(transport.YoutubeIE, '_extract_player_response', return_value=response) as native:
                self.assertIs(transport._YouTube(None)._extract_player_response(client), response)
                native.assert_called_once_with(client)

    def test_restricted_player_response_stops_native_client_iteration(self):
        # Use the pinned upstream client orchestrator, not a mock of its loop.
        # A refusal from the first public client must prevent trying the next.
        for status in ('LOGIN_REQUIRED', 'AGE_VERIFICATION_REQUIRED', 'AGE_CHECK_REQUIRED'):
            response = {'playabilityStatus': {'status': status}, 'videoDetails': {'videoId': 'abcdefghijk'}}
            with self.subTest(status=status), YoutubeDL({'quiet': True, 'no_warnings': True, 'js_runtimes': {}}, auto_init=False) as downloader:
                extractor = transport._YouTube(downloader)
                with patch.object(downloader, 'urlopen', side_effect=AssertionError('offline test attempted networking')) as request, patch.object(transport.YoutubeIE, '_extract_player_response', return_value=response) as native:
                    with self.assertRaisesRegex(Exception, 'DWNL_ACCESS_RESTRICTED'):
                        extractor._extract_player_responses(['visionos', 'web'], 'abcdefghijk', None, 'web', {}, False)
                    self.assertEqual(native.call_count, 1)
                    self.assertEqual(native.call_args.args[0], 'visionos')
                    request.assert_not_called()

    def test_native_user_agent_and_public_headers_survive_without_credentials(self):
        transport.STATE.update(platform='youtube', mode='download', bytes=0, requests=0)
        response = wire_response(b'HTTP/1.1 206 Partial Content\r\nContent-Length: 3\r\n\r\nabc')
        connection = MagicMock(); connection.getresponse.return_value = response
        headers = {'uSeR-aGeNt': 'Upstream public client/1.0', 'Range': 'bytes=0-2', 'Origin': 'https://www.youtube.com', 'Referer': 'https://www.youtube.com/', 'X-YouTube-Client-Name': '101', 'Cookie': 'secret=x', 'Authorization': 'Bearer secret', 'Proxy-Authorization': 'Basic secret', 'Host': 'untrusted.invalid', 'X-Forwarded-For': '127.0.0.1', 'Forwarded': 'for=127.0.0.1', 'Accept-Encoding': 'br', 'Connection': 'keep-alive'}
        with patch.object(transport, '_PinnedConnection', return_value=connection):
            result = transport.DwnlRH(logger=MagicMock()).send(Request('https://rr1.googlevideo.com/media', headers=headers))
            self.assertEqual(result.read(), b'abc'); result.close()
        sent = {key.lower(): value for key, value in connection.request.call_args.kwargs['headers'].items()}
        for key in ('user-agent', 'range', 'origin', 'referer', 'x-youtube-client-name'):
            self.assertEqual(sent[key], {key.lower(): value for key, value in headers.items()}[key])
        for key in ('cookie', 'authorization', 'proxy-authorization', 'host', 'x-forwarded-for', 'forwarded'):
            self.assertNotIn(key, sent)
        self.assertEqual(sent['accept-encoding'], 'identity')
        self.assertEqual(sent['connection'], 'close')

    def test_cross_host_redirect_keeps_user_agent_but_drops_origin_and_referer(self):
        transport.STATE.update(platform='youtube', mode='download', bytes=0, requests=0)
        connections = [MagicMock(), MagicMock()]
        connections[0].getresponse.return_value = wire_response(b'HTTP/1.1 302 Found\r\nLocation: https://rr1.googlevideo.com/media\r\n\r\n')
        connections[1].getresponse.return_value = wire_response(b'HTTP/1.1 200 OK\r\nContent-Length: 3\r\n\r\nabc')
        with patch.object(transport, '_PinnedConnection', side_effect=connections):
            result = transport.DwnlRH(logger=MagicMock()).send(Request('https://www.youtube.com/media', headers={'User-Agent': 'Upstream public client/1.0', 'Origin': 'https://www.youtube.com', 'Referer': 'https://www.youtube.com/'}))
            self.assertEqual(result.read(), b'abc'); result.close()
        sent = {key.lower(): value for key, value in connections[1].request.call_args.kwargs['headers'].items()}
        self.assertEqual(sent['user-agent'], 'Upstream public client/1.0')
        self.assertNotIn('origin', sent); self.assertNotIn('referer', sent)

    def test_tls_connects_only_to_checked_numeric_endpoint_and_original_hostname(self):
        sock=MagicMock();sock.getpeername.return_value=('8.8.8.8',443)
        tls=MagicMock();tls.wrap_socket.return_value=sock
        with patch.object(transport,'public_addresses',return_value=[('8.8.8.8',443)]) as dns, patch.object(transport.socket,'socket',return_value=sock),patch.object(transport.ssl,'create_default_context',return_value=tls):
            self.assertIs(transport.dial_public('www.youtube.com',443,15,True),sock)
            dns.assert_called_once_with('www.youtube.com',443)
            sock.connect.assert_called_once_with(('8.8.8.8',443))
            tls.wrap_socket.assert_called_once_with(sock,server_hostname='www.youtube.com')

    def test_wire_host_header_omits_the_default_port_for_its_scheme(self):
        for secure, port in ((True, 443), (False, 80)):
            with self.subTest(secure=secure, port=port):
                sink = WireSink()
                connection = transport._PinnedConnection('rr1.googlevideo.com', port, 15, secure)
                connection.sock = sink
                with patch.object(transport.socket, 'socket', side_effect=AssertionError('offline wire test attempted socket')):
                    connection.request('GET', '/media?synthetic=value')
                    connection.close()
                hosts = [line for line in sink.wire.split(b'\r\n') if line.lower().startswith(b'host:')]
                self.assertEqual(hosts, [b'Host: rr1.googlevideo.com'])
                self.assertTrue(sink.closed)

    def test_wire_request_replaces_caller_host_with_validated_https_authority(self):
        transport.STATE.update(platform='youtube', mode='download', bytes=0, requests=0)
        sink = WireSink()
        def connect(connection):
            connection.sock = sink
        with patch.object(transport._PinnedConnection, 'connect', connect), \
                patch.object(transport._PinnedConnection, 'getresponse', return_value=wire_response(b'HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n')), \
                patch.object(transport.socket, 'socket', side_effect=AssertionError('offline wire test attempted socket')):
            response = transport.DwnlRH(logger=MagicMock()).send(Request(
                'https://rr1.googlevideo.com/media?synthetic=value', headers={'Host': 'untrusted.invalid'}))
            response.close()
        lines = sink.wire.split(b'\r\n')
        self.assertEqual(lines[0], b'GET /media?synthetic=value HTTP/1.1')
        self.assertEqual([line for line in lines if line.lower().startswith(b'host:')], [b'Host: rr1.googlevideo.com'])
        self.assertNotIn(b'untrusted.invalid', sink.wire)

    def test_http_range_and_streaming_body_are_preserved_without_cookies(self):
        transport.STATE.update(platform='youtube',mode='download',bytes=0,requests=0)
        response=wire_response(b'HTTP/1.1 206 Partial Content\r\nContent-Length: 3\r\nContent-Range: bytes 2-4/9\r\nSet-Cookie: secret=x\r\n\r\nabc')
        connection=MagicMock();connection.getresponse.return_value=response
        with patch.object(transport,'_PinnedConnection',return_value=connection):
            result=transport.DwnlRH(logger=MagicMock()).send(Request('https://rr1.googlevideo.com/media',headers={'Range':'bytes=2-4','Cookie':'secret=x','Authorization':'Bearer secret'}))
            self.assertEqual(result.status,206);self.assertEqual(result.headers['Content-Range'],'bytes 2-4/9')
            self.assertNotIn('Set-Cookie',result.headers)
            self.assertEqual(result.read(2),b'ab');self.assertEqual(result.read(2),b'c');result.close()
            sent=connection.request.call_args.kwargs['headers']
            self.assertEqual(sent['Range'],'bytes=2-4');self.assertFalse(any(k.lower() in ('cookie','authorization') for k in sent))
            connection.close.assert_called()

    def test_redirect_downgrade_and_denied_http_never_reach_second_connection(self):
        for wire in (b'HTTP/1.1 302 Found\r\nLocation: http://www.youtube.com/other\r\n\r\n',b'HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n'):
            transport.STATE.update(platform='youtube',mode='analyze',bytes=0,requests=0)
            connection=MagicMock();connection.getresponse.return_value=wire_response(wire)
            with patch.object(transport,'_PinnedConnection',return_value=connection) as factory:
                with self.assertRaises(Exception):
                    transport.DwnlRH(logger=MagicMock()).send(Request('https://www.youtube.com/watch?v=public'))
                self.assertEqual(factory.call_count,1);connection.close.assert_called()

    def test_cumulative_body_budget_closes_before_returning_excess_bytes(self):
        transport.STATE.update(platform='youtube',mode='analyze',bytes=32*1024*1024-2,requests=0)
        connection=MagicMock();reader=io.BytesIO(b'abc')
        body=transport._LimitedBody(reader,connection,'identity')
        with self.assertRaisesRegex(Exception,'byte budget'):
            body.read(3)
        self.assertTrue(reader.closed);connection.close.assert_called()

    def test_remaining_job_budget_applies_before_media_bytes_are_returned(self):
        transport.STATE.update(platform='youtube',mode='download',bytes=0,requests=0)
        item={'id':'public','formats':[{'format_id':'v','vcodec':'h264','acodec':'none'}]}
        guard={'id':'public','format':item['formats'][0],'budget_bytes':2}
        transport.validate_selection(item,guard)
        body=transport._LimitedBody(io.BytesIO(b'abc'),MagicMock(),'identity')
        with self.assertRaisesRegex(Exception,'byte budget'):
            body.read(3)

class WireSink:
    def __init__(self):
        self.wire = b''
        self.closed = False

    def sendall(self, data):
        self.wire += data

    def close(self):
        self.closed = True


def wire_response(raw):
    class Socket:
        def makefile(self,*args,**kwargs):return io.BytesIO(raw)
    response=http.client.HTTPResponse(Socket());response.begin();return response


if __name__ == '__main__':
    unittest.main()
