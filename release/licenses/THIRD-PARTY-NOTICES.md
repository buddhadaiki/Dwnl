# Local media tool notices

The executables in `../bin` are unmodified files downloaded from the upstream release assets identified in `../verified-tools.json`. Archive hashes were checked against published release checksums and GitHub asset digests before extraction. The manifest also records the extracted executables' SHA256 values.

## yt-dlp 2026.08.19

The standalone Windows executable bundles Python 3.10.11 and yt-dlp-ejs 0.8.0. The combined executable is GPL-3.0-or-later because of its bundled components; the upstream yt-dlp source is Unlicense. Preserve both `yt-dlp-LICENSE.txt` and the upstream aggregate `yt-dlp-THIRD_PARTY_LICENSES.txt`, which contains the component notices and license texts.

- Release: https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.19
- Source: https://github.com/yt-dlp/yt-dlp/tree/2026.08.19
- EJS source: https://github.com/yt-dlp/ejs/releases/tag/0.8.0
- Python source: https://github.com/python/cpython/tree/v3.10.11

## Deno 2.9.7

The Deno source is MIT licensed. Preserve `deno-LICENSE.txt` and the additional upstream Node/Undici type and WebGPU license texts captured beside it. Other bundled components retain their respective licenses. The complete upstream source and dependency lock are identified below; these supplemental texts are not represented as an exhaustive inventory of every Deno dependency.

- Release: https://github.com/denoland/deno/releases/tag/v2.9.7
- Source: https://github.com/denoland/deno/tree/v2.9.7
- Dependency lock: https://github.com/denoland/deno/blob/v2.9.7/Cargo.lock

## FFmpeg and ffprobe N-127511-g01cf1a5396-20261013

This static Windows x64 build has `--enable-gpl --enable-version3` and includes `libmp3lame`. Preserve `ffmpeg-LICENSE.txt` (GPL version 3). The builder scripts have a separate license preserved as `FFmpeg-Builds-LICENSE.txt`.

- Publisher: https://github.com/yt-dlp/FFmpeg-Builds
- FFmpeg source: https://github.com/FFmpeg/FFmpeg/tree/01cf1a5396981e179ebfcdc6e6006d4070e71c3b
- Builder source snapshot: https://github.com/yt-dlp/FFmpeg-Builds/tree/bf23782fa09c1ab08a3c13f2f1508d7ef0e620c6
- Build configuration: `../metadata/ffmpeg-version.txt`
- Legal reference: https://ffmpeg.org/legal.html

The publisher's `latest` release URL changes over time. The saved release ID, asset ID, SHA256, upstream source commit, and builder source snapshot pin the exact local files. Complete corresponding source archives for every compiled dependency have not been assembled in this local development tool folder. No public distribution was performed.
