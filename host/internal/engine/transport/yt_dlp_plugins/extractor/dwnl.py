"""Host-embedded adapter. No user plugins, credentials or external downloader.

The CLI selects only DwnlIE. If this module fails to load, no default IE can
take over. Every supported extractor and native media downloader uses the
singleton request director below; TLS connects to an already checked IP.
"""
import gzip
import http.client
import io
import itertools
import ipaddress
import json
import math
import re
import socket
import ssl
import sys
import urllib.parse

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
        super().__init__(host, port, timeout=timeout)
        self.secure = secure

    def connect(self):
        if self._tunnel_host:
            raise policy_error('proxy tunnels are unavailable')
        self.sock = dial_public(self.host, self.port, self.timeout, self.secure)


class _LimitedBody(io.RawIOBase):
    def __init__(self, response, connection, encoding):
        self.response, self.connection = response, connection
        if encoding in ('', 'identity'):
            self.reader = response
        elif encoding == 'gzip':
            self.reader = gzip.GzipFile(fileobj=response)
        else:
            response.close()
            connection.close()
            raise TransportError('DWNL_NETWORK_ERROR: unsupported content encoding')

    def readable(self):
        return True

    def read(self, size=-1):
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
        STATE['bytes'] += len(result)
        if unbounded and len(result) > 32 * 1024 * 1024:
            self.close()
            raise policy_error('metadata response exceeds limit')
        if STATE['bytes'] > limit:
            self.close()
            raise policy_error('request byte budget exceeded')
        return result

    def close(self):
        if hasattr(self,'reader'):
            self.reader.close()
        self.response.close()
        self.connection.close()
        super().close()


class DwnlRH(RequestHandler):
    _SUPPORTED_URL_SCHEMES = ('http', 'https')

    def _validate(self, request):
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

    def _send(self, request):
        self._validate(request)
        url, method, data = request.url, request.method, request.data
        headers = self._get_headers(request)
        forbidden = {'cookie', 'authorization', 'proxy-authorization', 'host', 'x-forwarded-for', 'forwarded',
                     'accept-encoding', 'user-agent', 'connection'}
        headers = {key: value for key, value in headers.items() if key.lower() not in forbidden}
        headers.update({'User-Agent': 'Dwnl/0.1 public-media', 'Accept-Encoding': 'identity', 'Connection': 'close'})
        for _ in range(11):
            parsed = checked_url(url, STATE['platform'])
            STATE['requests'] += 1
            if STATE['requests'] > (128 if STATE['mode'] == 'analyze' else 10000):
                raise policy_error('request count budget exceeded')
            connection = _PinnedConnection(parsed.hostname, parsed.port or (443 if parsed.scheme == 'https' else 80),
                                           min(15, self._calculate_timeout(request)), parsed.scheme == 'https')
            try:
                target = urllib.parse.urlunsplit(('', '', parsed.path or '/', parsed.query, ''))
                connection.request(method, target, body=data, headers=headers)
                response = connection.getresponse()
            except (OSError, http.client.HTTPException) as error:
                connection.close()
                raise TransportError('DWNL_NETWORK_ERROR: HTTP connection failed', cause=error) from error
            print('DWNL_NET ' + parsed.hostname + ' ' + str(response.status), file=sys.stderr)
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
            wrapped = Response(_LimitedBody(response, connection, encoding),
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
        if client != 'android_vr':
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


class DwnlIE(InfoExtractor):
    IE_NAME = 'Dwnl'
    _VALID_URL = r'https://(?:[^/]+)/(?:.+)'
    _GEO_BYPASS = False

    def _real_extract(self, url):
        platform = next((key for key, hosts in INPUT_HOSTS.items() if urllib.parse.urlsplit(url).hostname in hosts), None)
        if not platform:
            raise ExtractorError('DWNL_NETWORK_POLICY: unsupported source host', expected=True)
        checked_url(url, platform)
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
            validate_selection(matches[0], json.loads(raw))
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
