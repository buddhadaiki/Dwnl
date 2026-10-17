"""Host-embedded adapter. No user plugins, credentials or external downloader.

The CLI selects only DwnlIE. If this module fails to load, no default IE can
take over. Every supported extractor and native media downloader uses the
singleton request director below; TLS connects to an already checked IP.
"""
import atexit
import base64
import email.utils
import gzip
import http.client
import http.cookiejar
import io
import itertools
import ipaddress
import json
import math
import re
import socket
import ssl
import sys
import time
import urllib.parse
from html.parser import HTMLParser

from yt_dlp.cookies import YoutubeDLCookieJar
from yt_dlp.extractor.common import InfoExtractor
from yt_dlp.extractor.youtube import YoutubeIE
from yt_dlp.extractor.instagram import InstagramIE
from yt_dlp.extractor.tiktok import TikTokIE, TikTokVMIE
from yt_dlp.extractor.twitter import TwitterIE
from yt_dlp.networking.common import RequestHandler, Response, _REQUEST_HANDLERS
from yt_dlp.networking.exceptions import RequestError, TransportError, HTTPError
from yt_dlp.utils import ExtractorError

__all__ = ['DwnlIE']
HOSTS = {
    'youtube': ('youtube.com', 'youtu.be', 'googlevideo.com', 'ytimg.com',
                'youtubei.googleapis.com', 'youtube.googleapis.com'),
    'instagram': ('instagram.com', 'cdninstagram.com', 'fbcdn.net'),
    'tiktok': ('tiktok.com', 'tiktokv.com', 'tiktokcdn.com', 'tiktokcdn-us.com',
               'tiktokv.us', 'tiktokv.eu', 'byteoversea.com', 'ibytedtos.com', 'ibyteimg.com'),
    'x': ('x.com', 'twitter.com', 'twimg.com'),
}
INPUT_HOSTS = {
    'youtube': ('youtube.com', 'www.youtube.com', 'm.youtube.com', 'music.youtube.com', 'youtu.be'),
    'instagram': ('instagram.com', 'www.instagram.com'),
    'tiktok': ('tiktok.com', 'www.tiktok.com', 'vm.tiktok.com', 'vt.tiktok.com'),
    'x': ('x.com', 'www.x.com', 'twitter.com', 'www.twitter.com'),
}
FORMAT_ID = re.compile(r'^[A-Za-z0-9_-]{1,128}$')
PROTOCOLS = {'http', 'https', 'm3u8', 'm3u8_native', 'http_dash_segments'}
STATE = {'platform': None, 'mode': 'analyze', 'bytes': 0, 'requests': 0, 'byte_limit': None}


def tiktok_restricted():
    return RequestError('DWNL_ACCESS_RESTRICTED: TikTok anonymous verification did not provide public access')


class _TikTokCookiePolicy(http.cookiejar.DefaultCookiePolicy):
    def __init__(self, session):
        super().__init__()
        self.session = session

    def set_ok(self, cookie, request):
        # No server Set-Cookie is imported. Only the fixed upstream WAF
        # computation may insert the two allowlisted cookies directly.
        return False

    def return_ok(self, cookie, request):
        return (self.session.phase == 'ready'
                and request.get_full_url() == self.session.canonical_url
                and cookie.name in ('_wafchallengeid', 'waforiginalreid')
                and cookie.secure and super().return_ok(cookie, request))


class _TikTokCookieJar(YoutubeDLCookieJar):
    def __init__(self, session):
        super().__init__(filename=None, policy=_TikTokCookiePolicy(session))
        self.session = session

    def set_cookie(self, cookie, *args, **kwargs):
        # CookieJar.set_cookie bypasses CookiePolicy; validate this route too.
        value = cookie.value
        if (self.session.phase != 'solving'
                or cookie.name not in ('_wafchallengeid', 'waforiginalreid')
                or cookie.domain not in ('.tiktok.com', 'tiktok.com', 'www.tiktok.com')
                or cookie.path != '/' or cookie.port is not None
                or not isinstance(value, str) or not 0 < len(value) <= 4096
                or any(ord(c) < 33 or ord(c) > 126 or c in ';,"\\' for c in value)):
            raise policy_error('anonymous cookie is outside its fixed scope')
        now = int(time.time())
        if cookie.expires is not None and (type(cookie.expires) is not int or cookie.expires <= now):
            raise policy_error('anonymous cookie expiry is invalid')
        # Collapse accepted aliases to one key per fixed name: never more
        # than two stored cookies or duplicate names in the outbound header.
        cookie.domain = '.tiktok.com'
        cookie.domain_specified = cookie.domain_initial_dot = True
        cookie.secure, cookie.discard = True, True
        cookie.expires = min(cookie.expires if cookie.expires is not None else now + 120, now + 120)
        return super().set_cookie(cookie, *args, **kwargs)

    def clear(self, *args, **kwargs):
        # Upstream also clears after its GET. Transport cleanup may already
        # have purged these cookies on success, refusal or connection failure.
        try:
            return super().clear(*args, **kwargs)
        except KeyError:
            return None

    def load(self, *args, **kwargs):
        raise policy_error('anonymous cookies cannot be loaded')

    def save(self, *args, **kwargs):
        raise policy_error('anonymous cookies cannot be saved')

    def revert(self, *args, **kwargs):
        raise policy_error('anonymous cookies cannot be loaded')


class _TikTokChallengeElements(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.elements = {}

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        identity = attrs.get('id')
        if identity in ('cs', 'wci', 'rci', 'rs'):
            if identity in self.elements:
                raise tiktok_restricted()
            self.elements[identity] = attrs.get('class', '')


def _validate_tiktok_challenge(webpage):
    if (not isinstance(webpage, str) or len(webpage.encode('utf-8')) > 65536
            or re.search(r'LOGIN_REQUIRED|AGE_VERIFICATION_REQUIRED|AGE_CHECK_REQUIRED|captcha|sign in|log in', webpage, re.I)):
        raise tiktok_restricted()
    try:
        parser = _TikTokChallengeElements()
        parser.feed(webpage)
        fields = parser.elements
        if (fields.get('wci') != '_wafchallengeid'
                or (fields.get('rs') and fields.get('rci') != 'waforiginalreid')
                or fields.get('rci') not in (None, 'waforiginalreid')
                or not 0 < len(fields.get('cs', '')) <= 16384):
            raise ValueError('unsupported structure')
        encoded = fields['cs']
        data = json.loads(base64.b64decode(encoded + '=' * (-len(encoded) % 4), validate=True))
        seed, digest = data['v']['a'], data['v']['c']
        if not isinstance(seed, str) or not isinstance(digest, str) or len(seed) > 8192 or len(digest) > 64:
            raise ValueError('unsupported digest input')
        decode = lambda value: base64.b64decode(value + '=' * (-len(value) % 4), validate=True)
        if not 0 < len(decode(seed)) <= 4096 or len(decode(digest)) != 32:
            raise ValueError('unsupported digest size')
        return {'_wafchallengeid'} | ({'waforiginalreid'} if fields.get('rs') else set())
    except Exception:
        raise tiktok_restricted() from None


class _TikTokCdnSession:
    """One official anonymous token; never a cookiejar or extractor field."""
    def __init__(self):
        self._value = None
        self._expires = 0
        self._seen = False
        self._public = False
        self._url = None
        self._bound = False

    def capture(self, raw, eligible):
        if not re.match(r'^\s*tt_chain_token\s*=', raw) or not eligible:
            return
        try:
            if (self._seen or len(raw) > 8192
                    or any(ord(c) < 32 or ord(c) > 126 for c in raw)):
                raise ValueError()
            self._seen = True
            first, *segments = raw.split(';')
            name, _, value = first.partition('=')
            if (name.strip() != 'tt_chain_token' or not 0 < len(value) <= 4096
                    or any(ord(c) < 33 or ord(c) > 126 or c in ';,"\\' for c in value)):
                raise ValueError()
            attrs = {}
            for segment in segments:
                key, separator, attr = segment.strip().partition('=')
                key, attr = key.lower(), attr.strip()
                if (key in attrs or key not in ('domain', 'path', 'secure', 'httponly', 'samesite', 'expires', 'max-age')
                        or (key in ('secure', 'httponly') and separator)
                        or (key not in ('secure', 'httponly') and (not separator or not attr))):
                    raise ValueError()
                if attr.startswith('"'):
                    if not attr.endswith('"') or '"' in attr[1:-1] or '\\' in attr:
                        raise ValueError()
                    attr = attr[1:-1]
                attrs[key] = attr
            if (attrs.get('domain', '').lower() not in ('tiktok.com', '.tiktok.com')
                    or attrs.get('path') != '/'
                    or ('samesite' in attrs and attrs['samesite'].lower() not in ('none', 'lax', 'strict'))):
                raise ValueError()
            ttl = 120.0
            if 'max-age' in attrs:
                if not re.fullmatch(r'[0-9]{1,10}', attrs['max-age']):
                    raise ValueError()
                ttl = min(ttl, int(attrs['max-age']))
            if 'expires' in attrs:
                expiry = email.utils.parsedate_to_datetime(attrs['expires'])
                if expiry is None or expiry.tzinfo is None:
                    raise ValueError()
                ttl = min(ttl, expiry.timestamp() - time.time())
            if not 0 < ttl <= 120:
                raise ValueError()
            # Secure is imposed locally: even a server cookie without the
            # Secure attribute can only reach the exact bound HTTPS URL.
            self._value, self._expires = value, time.monotonic() + ttl
        except (ValueError, TypeError, OverflowError):
            self.close()
            raise tiktok_restricted() from None

    def promote(self, data, status):
        author = data.get('author') if isinstance(data, dict) else None
        self._public = (type(status) is int and status == 0 and isinstance(data, dict)
                        and bool(data.get('id')) and isinstance(data.get('video'), dict) and bool(data['video'])
                        and not any(data.get(key) for key in ('privateItem', 'isPrivate', 'secret', 'isSecret', 'isContentClassified'))
                        and not (isinstance(author, dict) and (author.get('privateAccount') or author.get('isPrivateAccount'))))
        if not self._public or STATE['mode'] != 'download':
            self.close()

    def bind(self, selected):
        if self._bound:
            self.close()
            raise tiktok_restricted()
        if STATE['mode'] != 'download' or not self._public or self._value is None:
            self.close()
            return
        url = selected.get('url')
        try:
            parsed = checked_url(url, 'tiktok')
        except BaseException:
            self.close()
            raise
        if (parsed.scheme != 'https' or parsed.fragment or parsed.hostname in ('tiktok.com', 'www.tiktok.com')
                or not parsed.hostname.endswith('.tiktok.com')
                or selected.get('protocol') != 'https' or selected.get('fragments') or selected.get('manifest_url')
                or parsed.path.lower().endswith(('.m3u8', '.mpd'))):
            self.close()
            return
        self._url, self._bound = url, True

    def request_cookie(self, url, method):
        if self._url is None or self._value is None:
            return None
        if (STATE['mode'] != 'download' or url != self._url or method != 'GET'
                or time.monotonic() >= self._expires):
            self.close()
            return None
        return 'tt_chain_token=' + self._value

    def close(self):
        self._value, self._expires, self._url, self._public = None, 0, None, False


class _TikTokSession:
    def __init__(self, downloader):
        self.downloader = downloader
        self.jar = _TikTokCookieJar(self)
        self.canonical_url = None
        self.phase = 'new'
        self.attempted = False
        self.source_ok = False
        self.cdn = _TikTokCdnSession()

    def start_page(self, url):
        parsed = checked_url(url, 'tiktok')
        if (self.phase != 'new' or parsed.hostname != 'www.tiktok.com'
                or parsed.query or parsed.fragment
                or not re.fullmatch(r'/@[^/]+/(?:video|photo)/[0-9]+', parsed.path)):
            raise tiktok_restricted()
        self.canonical_url, self.phase = url, 'page'

    def record_response(self, url, method, status):
        self.source_ok = url == self.canonical_url and method == 'GET' and status == 200

    def request_cookie(self, url, method):
        if self.phase != 'ready':
            return None
        if url != self.canonical_url or method != 'GET':
            self.purge()
            raise tiktok_restricted()
        header = self.jar.get_cookie_header(url)
        if not header:
            self.purge()
            raise tiktok_restricted()
        self.phase = 'sending'
        return header

    def finish_request(self):
        if self.phase == 'sending':
            self.purge()

    def purge(self):
        self.jar.clear()
        self.phase = 'done'

    def close(self):
        self.purge()
        self.cdn.close()
        self.phase = 'closed'


def _check_tiktok_cookie_sources(downloader):
    # Check parameters before touching cached cookiejar/request_director:
    # either cached property may otherwise load a supplied file/browser jar.
    if (downloader is None or downloader.params.get('cookiefile') is not None
            or downloader.params.get('cookiesfrombrowser') is not None):
        raise policy_error('anonymous session cannot import cookies')


def _tiktok_session(downloader):
    _check_tiktok_cookie_sources(downloader)
    session = STATE.get('tiktok_session')
    if session is not None:
        if session.downloader is not downloader or session.phase == 'closed':
            raise policy_error('anonymous session cannot be replaced')
        return session
    handlers = downloader._request_director.handlers
    if len(handlers) != 1 or not isinstance(next(iter(handlers.values())), DwnlRH):
        raise policy_error('private transport is not exclusive')
    handler = next(iter(handlers.values()))
    if list(downloader.cookiejar) or list(handler.cookiejar):
        raise policy_error('anonymous session requires empty cookies')
    session = _TikTokSession(downloader)
    downloader.cookiejar = handler.cookiejar = session.jar
    handler.tiktok_session = session
    STATE['tiktok_session'] = session
    atexit.register(session.close)
    return session


def policy_error(reason):
    return RequestError('DWNL_NETWORK_POLICY: ' + reason)


def checked_url(raw, platform):
    if (not isinstance(raw, str) or len(raw) > 16384 or not raw
            or any(ord(c) < 33 or c == '\\' for c in raw)):
        raise policy_error('ambiguous URL')
    try:
        parsed = urllib.parse.urlsplit(raw)
        host, port = parsed.hostname, parsed.port
    except ValueError as error:
        raise policy_error('invalid URL') from error
    if (parsed.scheme != 'https' or not host or parsed.username is not None
            or parsed.password is not None or host.endswith('.') or not host.isascii()
            or port not in (None, 443)):
        raise policy_error('unsupported URL authority or scheme')
    host = host.lower()
    if not any(host == base or host.endswith('.' + base) for base in HOSTS.get(platform, ())):
        raise policy_error('domain not allowed: ' + host)
    if any(piece in parsed.path.lower() for piece in ('/login', '/accounts/login', '/signin', '/challenge')):
        raise RequestError('DWNL_ACCESS_RESTRICTED: login or challenge destination')
    return parsed


def public_addresses(host, port, resolver=socket.getaddrinfo):
    endpoints = []
    try:
        answers = resolver(host, port, type=socket.SOCK_STREAM)
    except OSError as error:
        raise TransportError('DWNL_NETWORK_ERROR: DNS resolution failed', cause=error) from error
    for row in answers:
        address = ipaddress.ip_address(row[4][0].split('%')[0])
        checked = address.ipv4_mapped if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped else address
        if (not checked.is_global or checked.is_multicast or checked.is_reserved
                or (isinstance(address, ipaddress.IPv6Address) and (address.teredo or address.sixtofour))):
            raise policy_error('DNS returned a non-public address')
        endpoint = (str(address), port)
        if endpoint not in endpoints:
            endpoints.append(endpoint)
    if not endpoints or len(endpoints) > 16:
        raise policy_error('DNS has no bounded public address set')
    return endpoints


def dial_public(host, port, timeout, secure):
    # Resolve once. socket.connect receives a numeric endpoint, so rebinding
    # between validation and connection cannot change the destination.
    endpoints = public_addresses(host, port)
    last_error = None
    for address, fixed_port in endpoints:
        sock = socket.socket(socket.AF_INET6 if ':' in address else socket.AF_INET, socket.SOCK_STREAM)
        try:
            sock.settimeout(timeout)
            sock.connect((address, fixed_port))
            peer = ipaddress.ip_address(sock.getpeername()[0])
            if peer != ipaddress.ip_address(address):
                raise policy_error('connected peer differs from checked address')
            if secure:
                sock = ssl.create_default_context().wrap_socket(sock, server_hostname=host)
            return sock
        except (OSError, ssl.SSLError) as error:
            sock.close()
            last_error = error
        except Exception:
            sock.close()
            raise
    raise TransportError('DWNL_NETWORK_ERROR: DNS/connect/TLS failed', cause=last_error)


class _PinnedConnection(http.client.HTTPConnection):
    def __init__(self, host, port, timeout, secure):
        self.default_port = 443 if secure else 80
        super().__init__(host, port, timeout=timeout)
        self.secure = secure

    def connect(self):
        if self._tunnel_host:
            raise policy_error('proxy tunnels are unavailable')
        self.sock = dial_public(self.host, self.port, self.timeout, self.secure)


class _LimitedBody(io.RawIOBase):
    def __init__(self, response, connection, encoding, cdn=None):
        self.response, self.connection = response, connection
        self.cdn, self.eof = cdn, False
        if encoding in ('', 'identity'):
            self.reader = response
        elif encoding == 'gzip':
            self.reader = gzip.GzipFile(fileobj=response)
        else:
            if self.cdn is not None:
                self.cdn.close()
            response.close()
            connection.close()
            raise TransportError('DWNL_NETWORK_ERROR: unsupported content encoding')

    def readable(self):
        return True

    def read(self, size=-1):
        try:
            return self._read(size)
        except BaseException:
            if self.cdn is not None:
                self.cdn.close()
            self.close()
            raise

    def _read(self, size=-1):
        limit = STATE.get('byte_limit') or (32 * 1024 * 1024 if STATE['mode'] == 'analyze' else 5 * 1024 * 1024 * 1024)
        remaining = limit - STATE['bytes']
        if remaining < 0:
            raise policy_error('request byte budget exceeded')
        # Native HLS reads manifests without a size. Bound this metadata path;
        # native media downloads use positive, incremental read sizes.
        unbounded = size is None or size < 0
        amount = min(remaining + 1, size if not unbounded else 32 * 1024 * 1024 + 1)
        try:
            result = self.reader.read(amount)
        except (OSError, http.client.HTTPException) as error:
            raise TransportError('DWNL_NETWORK_ERROR: interrupted response', cause=error) from error
        # read(0) is not EOF. A gzip reader can buffer a decoded tail after
        # its compressed HTTP body closes, so only identity may use wire EOF.
        if amount > 0 and (len(result) < amount or (self.reader is self.response
                and getattr(self.response, 'isclosed', lambda: False)())):
            self.eof = True
        STATE['bytes'] += len(result)
        if unbounded and len(result) > 32 * 1024 * 1024:
            self.close()
            raise policy_error('metadata response exceeds limit')
        if STATE['bytes'] > limit:
            self.close()
            raise policy_error('request byte budget exceeded')
        return result

    def close(self):
        # A complete Range body may be followed by another GET of the exact
        # same selection. Premature close/cancellation drops the token.
        if self.cdn is not None and not self.eof:
            self.cdn.close()
        try:
            if hasattr(self, 'reader'):
                self.reader.close()
            self.response.close()
        except BaseException:
            if self.cdn is not None:
                self.cdn.close()
            raise
        finally:
            self.connection.close()
            super().close()


class DwnlRH(RequestHandler):
    _SUPPORTED_URL_SCHEMES = ('http', 'https')

    def close(self):
        session = getattr(self, 'tiktok_session', None)
        if session is not None:
            session.close()

    def _validate(self, request):
        try:
            return self._validate_owned(request)
        except BaseException:
            session = getattr(self, 'tiktok_session', None)
            if session is not None:
                session.cdn.close()
            raise

    def _validate_owned(self, request):
        checked_url(request.url, STATE['platform'])
        if any(value for value in self._get_proxies(request).values()):
            raise policy_error('proxies are unavailable')
        if request.method not in ('GET', 'HEAD', 'POST') or (request.data is not None and not isinstance(request.data, bytes)):
            raise policy_error('unsupported HTTP operation')
        if request.data is not None and len(request.data) > 2 * 1024 * 1024:
            raise policy_error('request body exceeds limit')
        if not self.verify or self._client_cert:
            raise policy_error('TLS policy cannot be changed')
        if any(key not in ('timeout', 'cookiejar', 'keep_header_casing', 'impersonate') for key in request.extensions):
            raise policy_error('unsupported transport extension')
        session = getattr(self, 'tiktok_session', None)
        if session is not None and (STATE['platform'] != 'tiktok'
                or request.extensions.get('cookiejar') not in (None, session.jar)):
            raise policy_error('anonymous cookiejar cannot be substituted or shared')

    def _send(self, request):
        try:
            return self._send_owned(request)
        except BaseException:
            session = getattr(self, 'tiktok_session', None)
            if session is not None:
                session.cdn.close()
            raise

    def _send_owned(self, request):
        self._validate(request)
        url, method, data = request.url, request.method, request.data
        headers = self._get_headers(request)
        forbidden = {'cookie', 'authorization', 'proxy-authorization', 'host', 'x-forwarded-for', 'forwarded',
                     'accept-encoding', 'connection'}
        headers = {key: value for key, value in headers.items() if key.lower() not in forbidden}
        # The pinned extractor supplies a UA consistent with its public client
        # and selected media headers. No browser credentials are accepted.
        if not any(key.lower() == 'user-agent' for key in headers):
            headers['User-Agent'] = 'Dwnl/0.1 public-media'
        headers.update({'Accept-Encoding': 'identity', 'Connection': 'close'})
        for _ in range(11):
            parsed = checked_url(url, STATE['platform'])
            STATE['requests'] += 1
            if STATE['requests'] > (128 if STATE['mode'] == 'analyze' else 10000):
                raise policy_error('request count budget exceeded')
            session = getattr(self, 'tiktok_session', None)
            cookie = session.request_cookie(url, method) if session is not None else None
            cdn_cookie = session.cdn.request_cookie(url, method) if session is not None else None
            if cdn_cookie:
                if cookie:
                    raise policy_error('anonymous sessions cannot share a request')
                cookie = cdn_cookie
            if cookie:
                headers['Cookie'] = cookie
            connection, response = None, None
            try:
                connection = _PinnedConnection(parsed.hostname, parsed.port or (443 if parsed.scheme == 'https' else 80),
                                               min(15, self._calculate_timeout(request)), parsed.scheme == 'https')
                target = urllib.parse.urlunsplit(('', '', parsed.path or '/', parsed.query, ''))
                connection.request(method, target, body=data, headers=headers)
                response = connection.getresponse()
                if session is not None:
                    session.record_response(url, method, response.status)
                    for key, value in response.getheaders():
                        if key.lower() == 'set-cookie':
                            session.cdn.capture(value, STATE['mode'] == 'download' and session.source_ok)
            except (OSError, http.client.HTTPException) as error:
                if connection is not None:
                    connection.close()
                raise TransportError('DWNL_NETWORK_ERROR: HTTP connection failed', cause=error) from error
            except BaseException:
                if response is not None:
                    response.close()
                if connection is not None:
                    connection.close()
                raise
            finally:
                if session is not None:
                    session.finish_request()
                headers.pop('Cookie', None)
            print('DWNL_NET ' + parsed.hostname + ' ' + str(response.status), file=sys.stderr)
            if cookie and response.status not in ((200, 206) if cdn_cookie else (200,)):
                response.close()
                connection.close()
                raise tiktok_restricted()
            if response.status in (301, 302, 303, 307, 308):
                location = response.getheader('Location')
                response.close()
                connection.close()
                if not location:
                    raise TransportError('DWNL_NETWORK_ERROR: redirect has no target')
                following = urllib.parse.urljoin(url, location)
                next_url = checked_url(following, STATE['platform'])
                if parsed.scheme == 'https' and next_url.scheme != 'https':
                    raise policy_error('HTTPS downgrade redirect')
                if next_url.hostname != parsed.hostname:
                    headers = {key: value for key, value in headers.items() if key.lower() not in ('origin', 'referer')}
                if response.status == 303 or (response.status in (301, 302) and method == 'POST'):
                    method, data = 'GET', None
                    headers = {key: value for key, value in headers.items() if key.lower() not in ('content-type', 'content-length')}
                url = following
                continue
            if response.status in (401, 403, 429):
                status = response.status
                response.close()
                connection.close()
                raise RequestError('DWNL_ACCESS_RESTRICTED: server HTTP ' + str(status) + '; no credentials or bypass used')
            encoding = response.getheader('Content-Encoding', '').lower()
            dropped = {'set-cookie'} | ({'content-length','content-encoding'} if encoding == 'gzip' else set())
            wrapped = Response(_LimitedBody(response, connection, encoding, session.cdn if session is not None else None),
                               url, {key: value for key, value in response.getheaders() if key.lower() not in dropped},
                               response.status, response.reason)
            if response.status >= 400:
                raise HTTPError(wrapped)
            return wrapped
        raise policy_error('redirect limit exceeded')


class _CookieFreeMixin:
    def _set_cookie(self, *args, **kwargs):
        return None

    def _get_cookies(self, url):
        return {}

    def _solve_challenge_and_set_cookies(self, webpage):
        raise ExtractorError('DWNL_ACCESS_RESTRICTED: anti-bot challenge; no solver is used', expected=True)


class _YouTube(_CookieFreeMixin, YoutubeIE):
    @classmethod
    def ie_key(cls):
        return YoutubeIE.ie_key()

    @staticmethod
    def _is_agegated(player_response):
        _check_player_access(player_response)
        return False

    def _search_json(self, *args, **kwargs):
        result = super()._search_json(*args, **kwargs)
        _check_player_access(result)
        return result

    def _extract_player_response(self, client, *args, **kwargs):
        # Match the pinned upstream anonymous defaults explicitly. Its retired
        # android_vr client returns 403; embedded/authenticated fallbacks stay off.
        if client not in ('visionos', 'web'):
            raise RequestError('DWNL_ACCESS_RESTRICTED: alternative player client is unavailable')
        result = super()._extract_player_response(client, *args, **kwargs)
        _check_player_access(result)
        return result

    def fetch_po_token(self, *args, **kwargs):
        # No proof-of-origin provider or challenge service is invoked.
        return None


def _check_player_access(response):
    status = response.get('playabilityStatus', {}) if isinstance(response, dict) else {}
    reason = str(status.get('reason', '')).lower()
    if (status.get('status') in ('LOGIN_REQUIRED', 'AGE_VERIFICATION_REQUIRED', 'AGE_CHECK_REQUIRED')
            or status.get('desktopLegacyAgeGateReason')
            or any(text in reason for text in ('confirm your age', 'age-restricted', 'not a bot', 'sign in', 'login', 'country'))):
        raise RequestError('DWNL_ACCESS_RESTRICTED: player requires restricted access; no alternative client bypass')


class _Instagram(_CookieFreeMixin, InstagramIE):
    @classmethod
    def ie_key(cls):
        return InstagramIE.ie_key()


class _TikTok(_CookieFreeMixin, TikTokIE):
    @classmethod
    def ie_key(cls):
        return TikTokIE.ie_key()

    def _extract_web_formats(self, aweme_detail):
        formats = super()._extract_web_formats(aweme_detail)
        music = aweme_detail.get('music')
        music_url = music.get('playUrl') if isinstance(music, dict) else None
        if not isinstance(music_url, str) or not music_url:
            return formats
        music_url = self._proto_relative_url(music_url)
        # The associated library track is not the clip's original audio mix.
        # Preserve every video format and any independently sourced format.
        return [fmt for fmt in formats if not (fmt.get('format_id') == 'audio'
                and fmt.get('vcodec') == 'none' and fmt.get('url') == music_url)]

    def _set_cookie(self, *args, **kwargs):
        session = STATE.get('tiktok_session')
        if session is None or session.downloader is not self._downloader:
            raise policy_error('anonymous cookie has no owned session')
        return InfoExtractor._set_cookie(self, *args, **kwargs)

    def _solve_challenge_and_set_cookies(self, webpage):
        session = STATE.get('tiktok_session')
        if (session is None or session.downloader is not self._downloader
                or session.phase != 'page' or session.attempted or not session.source_ok):
            raise tiktok_restricted()
        # Reserve before validation so failures, redirects and another IE
        # instance cannot restart the one permitted verification computation.
        session.attempted = True
        succeeded = False
        try:
            expected_names = _validate_tiktok_challenge(webpage)
            session.phase = 'solving'
            # Pinned upstream is a finite native Python computation (at most
            # 1,000,001 candidates), with no JS execution or external provider.
            names = TikTokIE._solve_challenge_and_set_cookies(self, webpage)
            if any(name and name not in ('_wafchallengeid', 'waforiginalreid') for name in names):
                raise tiktok_restricted()
            # Upstream returns the optional name even when its value is empty
            # and no optional cookie was inserted. Cleanup only real cookies.
            names = tuple(name for name in names if name in expected_names)
            if set(names) != expected_names or {cookie.name for cookie in session.jar} != expected_names:
                raise tiktok_restricted()
            session.phase = 'ready'
            succeeded = True
            return names
        except Exception:
            raise tiktok_restricted() from None
        finally:
            if not succeeded:
                session.purge()

    def _extract_web_data_and_status(self, url, video_id, fatal=True):
        session = _tiktok_session(self._downloader)
        try:
            session.start_page(url)
            data, status = super()._extract_web_data_and_status(url, video_id, fatal=fatal)
            session.cdn.promote(data, status)
            return data, status
        except ExtractorError:
            session.cdn.close()
            if session.attempted:
                raise tiktok_restricted() from None
            raise
        except BaseException:
            session.cdn.close()
            raise
        finally:
            session.purge()


class _TikTokShort(_CookieFreeMixin, TikTokVMIE):
    @classmethod
    def ie_key(cls):
        return TikTokVMIE.ie_key()


class _Twitter(_CookieFreeMixin, TwitterIE):
    @classmethod
    def ie_key(cls):
        return TwitterIE.ie_key()


def _deny_external_download(*args, **kwargs):
    raise ExtractorError('DWNL_NETWORK_POLICY: external downloader fallback is disabled', expected=True)


def _public_items(info, depth=0):
    if depth > 4 or not isinstance(info, dict):
        raise ExtractorError('DWNL_INVALID_SELECTION: unsupported item structure', expected=True)
    entries = info.get('entries')
    if entries is not None:
        entries = list(itertools.islice(entries, 65))
        if len(entries) > 64:
            raise ExtractorError('DWNL_INVALID_SELECTION: too many post items', expected=True)
        result = []
        for entry in entries:
            if entry is not None:
                result.extend(_public_items(entry, depth + 1))
        return result
    if (info.get('is_live') or info.get('live_status') in ('is_live', 'is_upcoming')
            or info.get('has_drm') or info.get('availability') in ('private', 'premium_only', 'subscriber_only', 'needs_auth')):
        raise ExtractorError('DWNL_ACCESS_RESTRICTED: restricted, live or DRM media', expected=True)
    duration = info.get('duration')
    if duration is not None and (not isinstance(duration, (int, float)) or not math.isfinite(duration) or not 0 < duration <= 21600):
        raise ExtractorError('DWNL_INVALID_SELECTION: supported duration is up to six hours', expected=True)
    formats = info.get('formats') or ([info] if info.get('url') else [])
    print('DWNL_FORMATS count=' + str(len(formats)) + ' ' + repr([(str(f.get('format_id'))[:128], f.get('protocol'), bool(f.get('has_drm'))) for f in formats[:5]]), file=sys.stderr)
    if len(formats) > 4096:
        raise ExtractorError('DWNL_INVALID_SELECTION: format count exceeds limit', expected=True)
    identities = set()
    for index, item in enumerate(formats):
        original = str(item.get('format_id') or 'direct')
        base = re.sub(r'[^A-Za-z0-9_-]', '_', original)[:100] or 'direct'
        identity = base
        if not item.get('format_id') or identity in identities:
            identity = base + '-dwnl-' + str(index)
        while identity in identities:
            identity += '_'
        identities.add(identity)
        item['format_id'] = identity
        # Native IE.extract runs before YoutubeDL fills these derived fields.
        if not item.get('protocol'):
            item['protocol'] = urllib.parse.urlsplit(item.get('url', '')).scheme
        # Missing native codec data is unknown, never evidence of silence.
        # Preserve the distinction for the UI and confirm from local ffprobe.
        for field in ('vcodec', 'acodec'):
            if item.get(field) is None:
                item[field] = 'unknown'
    formats = [item for item in formats if not item.get('has_drm') and item.get('protocol', 'https') in PROTOCOLS
               and FORMAT_ID.fullmatch(str(item.get('format_id', '')))]
    if not formats:
        raise ExtractorError('DWNL_INVALID_SELECTION: no supported public formats', expected=True)
    info['formats'] = formats
    return [info]


def validate_selection(item, guard):
    try:
        return _validate_selection(item, guard)
    except BaseException:
        session = STATE.get('tiktok_session')
        if session is not None:
            session.cdn.close()
        raise


def _validate_selection(item, guard):
    invalid = lambda: ExtractorError('DWNL_INVALID_SELECTION: public source selection changed; analyze again', expected=True)
    if not isinstance(guard, dict) or str(item.get('id')) != guard.get('id'):
        raise invalid()
    expected = guard.get('format', {})
    matches = [fmt for fmt in item['formats'] if fmt.get('format_id') == expected.get('format_id')]
    if len(matches) != 1:
        raise invalid()
    for field in ('vcodec', 'acodec', 'width', 'height'):
        value = expected.get(field)
        if value not in (None, '', 0) and matches[0].get(field) != value:
            raise invalid()
    duration = guard.get('duration')
    actual = item.get('duration')
    if duration and (not actual or abs(actual - duration) > max(.5, duration * .01)):
        raise invalid()
    budget = guard.get('budget_bytes')
    if type(budget) is not int or not 0 < budget <= 5 * 1024 * 1024 * 1024:
        raise invalid()
    STATE['byte_limit'] = budget
    return matches[0]


class DwnlIE(InfoExtractor):
    IE_NAME = 'Dwnl'
    _VALID_URL = r'https://(?:[^/]+)/(?:.+)'
    _GEO_BYPASS = False

    def _real_extract(self, url):
        try:
            return self._extract_owned(url)
        except BaseException:
            session = STATE.get('tiktok_session')
            if session is not None:
                session.cdn.close()
            raise

    def _extract_owned(self, url):
        platform = next((key for key, hosts in INPUT_HOSTS.items() if urllib.parse.urlsplit(url).hostname in hosts), None)
        if not platform:
            raise ExtractorError('DWNL_NETWORK_POLICY: unsupported source host', expected=True)
        checked_url(url, platform)
        if platform == 'tiktok':
            _check_tiktok_cookie_sources(self._downloader)
        director = self._downloader._request_director
        if len(director.handlers) != 1 or not isinstance(next(iter(director.handlers.values())), DwnlRH):
            raise RequestError('DWNL_NETWORK_POLICY: private transport is not exclusive')
        mode = self._configuration_arg('mode', ['analyze'])[0]
        if mode == 'probe':
            if url != 'https://www.youtube.com/watch?v=dwnl-private-self-test':
                raise policy_error('invalid adapter probe')
            self.to_screen('DWNL_ADAPTER_READY')
            return {'id': 'dwnl-private-self-test', 'title': 'Private adapter self test', 'url': url, 'ext': 'mp4'}
        if mode not in ('analyze', 'download'):
            raise ExtractorError('DWNL_INVALID_SELECTION: unsupported host operation', expected=True)
        STATE.update(platform=platform, mode=mode, bytes=0, requests=0, byte_limit=32 * 1024 * 1024)
        if platform == 'tiktok':
            _tiktok_session(self._downloader)
        mapping = {'youtube': (_YouTube,), 'instagram': (_Instagram,),
                   'tiktok': (_TikTokShort, _TikTok), 'x': (_Twitter,)}
        for _ in range(5):
            cls = next((candidate for candidate in mapping[platform] if candidate.suitable(url)), None)
            if cls is None:
                raise ExtractorError('DWNL_INVALID_SELECTION: public video/post URL required', expected=True)
            info = cls(self._downloader).extract(url)
            if info.get('_type') not in ('url', 'url_transparent'):
                break
            url = info.get('url', '')
            checked_url(url, platform)
            if urllib.parse.urlsplit(url).hostname not in INPUT_HOSTS[platform]:
                raise ExtractorError('DWNL_NETWORK_POLICY: extractor delegation is outside its platform', expected=True)
        else:
            raise ExtractorError('DWNL_INVALID_SELECTION: extractor delegation limit', expected=True)
        items = _public_items(info)
        fields = ('format_id', 'ext', 'vcodec', 'acodec', 'width', 'height', 'filesize', 'filesize_approx', 'abr', 'tbr', 'has_drm', 'protocol')
        metadata = {'entries': [{key: item.get(key) for key in ('id', 'title', 'duration', 'availability', 'has_drm')}
                                | {'formats': [{key: fmt.get(key) for key in fields} for fmt in item['formats']]}
                                for item in items]}
        encoded = json.dumps(metadata, ensure_ascii=False, allow_nan=False).encode('utf-8')
        if len(encoded) > 2 * 1024 * 1024:
            raise ExtractorError('DWNL_INVALID_SELECTION: metadata exceeds limit', expected=True)
        with open('metadata.json', 'xb') as report:
            report.write(encoded)
        if mode == 'download':
            selected = self._configuration_arg('item', [''], casesense=True)[0]
            matches = [item for item in items if str(item.get('id')) == selected]
            if len(matches) != 1:
                raise ExtractorError('DWNL_INVALID_SELECTION: selected item disappeared', expected=True)
            with open('selection.json', 'rb') as selection:
                raw = selection.read(2 * 1024 * 1024 + 1)
            if len(raw) > 2 * 1024 * 1024:
                raise ExtractorError('DWNL_INVALID_SELECTION: selection guard exceeds limit', expected=True)
            selected_format = validate_selection(matches[0], json.loads(raw))
            if platform == 'tiktok':
                STATE['tiktok_session'].cdn.bind(selected_format)
            return matches[0]
        return info


# Pinned upstream paths all use this shared director. Default handlers are
# removed, not merely given a lower preference. Policy errors are RequestError
# and therefore cannot cause fallback. External downloaders are disabled before
# native HLS can attempt to delegate unsupported features to FFmpeg.
_REQUEST_HANDLERS.clear()
_REQUEST_HANDLERS[DwnlRH.RH_KEY] = DwnlRH
from yt_dlp.downloader.external import ExternalFD
ExternalFD.real_download = _deny_external_download
