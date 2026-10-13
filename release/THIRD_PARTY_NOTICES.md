# Dwnl private prototype dependency notices

This collateral records the exact files prepared for this local Windows prototype. The build is private validation only. Complete corresponding source archives have not been assembled, so the normal usable release package is blocked. Source links are precise references; they do not substitute for a complete corresponding source bundle or establish readiness for public redistribution.

The lock records SHA256, size, version, PE architecture, official release assets, archive digests, source commits/tags, component references, license text hashes and local capability evidence. The local staging step rechecks binary/collateral hashes and actual runtime versions and exercises the MP3 encoder. No platform media request is made.

Dwnl uses an unmodified Inno Setup 7.1.0 installer engine; its license is preserved as `licenses/Inno-Setup-LICENSE.txt`. Source: https://github.com/jrsoftware/issrc/tree/is-7_1_0. Compiler executable SHA256: d06ebd38f38e3cee60a3c50cc45bd449d77e0bc6a5cabc607ea9886808e4de1a. The compiler itself is not included in the installed payload.

## Remaining source collateral

- yt-dlp combined Windows executable: upstream yt-dlp 2026.08.19, bundled Python 3.10.11, EJS 0.8.0 and third-party aggregate license texts are identified, but complete corresponding source/build inputs for the combined binary are not assembled.
- Deno 2.9.7: MIT source references, pinned Cargo.lock and supplemental license texts are recorded. A complete third-party inventory and exact source/build inputs for all compiled dependencies are not assembled.
- FFmpeg and ffprobe: exact upstream commit 01cf1a5396981e179ebfcdc6e6006d4070e71c3b and builder commit bf23782fa09c1ab08a3c13f2f1508d7ef0e620c6 are pinned. Complete corresponding source for this GPLv3 build, its compiled external libraries and build inputs is not assembled.
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


Partial official source archives are included with validation payloads. Family attribution does not establish complete corresponding source; see dependencies.lock.json. Normal package generation remains blocked while completeCorrespondingSource is false.

## Native host static dependencies

The Windows helper was compiled with Go 1.27.2 and statically includes the Go runtime/standard library and golang.org/x/sys v0.38.0. The compiler's `go version -m dist/helper/dwnl-host.exe` readback identifies Go 1.27.2, x/sys v0.38.0, Windows amd64 and CGO_ENABLED=0. No Go or x/sys runtime is installed globally.

Both components use the BSD 3-Clause license. Binary redistribution must retain the copyright notice, conditions and disclaimer. Their actual unmodified local license texts are preserved as `licenses/Go-1.27.2-LICENSE.txt` and `licenses/golang.org-x-sys-v0.38.0-LICENSE.txt`, with exact SHA256 recorded in dependencies.lock.json. These permissive notice additions do not establish completeness of GPL media corresponding source or change the package's validation-only status.

- Go 1.27.2 source/license: https://cs.opensource.google/go/go/+/refs/tags/go1.27.2:LICENSE
- golang.org/x/sys v0.38.0 source/license: https://cs.opensource.google/go/x/sys/+/refs/tags/v0.38.0:LICENSE
