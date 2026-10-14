"""Regressions for SSRF, redirect and request boundaries, not site mocks."""
import importlib.util
import pathlib
import socket
import io
import http.client
import unittest
from unittest.mock import patch, MagicMock
from yt_dlp.networking.common import Request

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
        with patch.object(transport.YoutubeIE,'_extract_player_response') as native:
            with self.assertRaisesRegex(Exception,'DWNL_ACCESS_RESTRICTED'):
                transport._YouTube(None)._extract_player_response('web_embedded')
            native.assert_not_called()

    def test_tls_connects_only_to_checked_numeric_endpoint_and_original_hostname(self):
        sock=MagicMock();sock.getpeername.return_value=('8.8.8.8',443)
        tls=MagicMock();tls.wrap_socket.return_value=sock
        with patch.object(transport,'public_addresses',return_value=[('8.8.8.8',443)]) as dns, patch.object(transport.socket,'socket',return_value=sock),patch.object(transport.ssl,'create_default_context',return_value=tls):
            self.assertIs(transport.dial_public('www.youtube.com',443,15,True),sock)
            dns.assert_called_once_with('www.youtube.com',443)
            sock.connect.assert_called_once_with(('8.8.8.8',443))
            tls.wrap_socket.assert_called_once_with(sock,server_hostname='www.youtube.com')

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

def wire_response(raw):
    class Socket:
        def makefile(self,*args,**kwargs):return io.BytesIO(raw)
    response=http.client.HTTPResponse(Socket());response.begin();return response


if __name__ == '__main__':
    unittest.main()
