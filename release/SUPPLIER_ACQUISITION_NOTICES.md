# Dwnl private supplier acquisition prototype

The setup embeds our native helper and notice texts. It acquires the unmodified media executables directly from the exact public upstream release URLs in suppliers.lock.json during the user's manual setup run. The setup does not embed or redistribute these upstream media binaries. SHA256 is checked before archive extraction and again for every selected installed file. A changed or unavailable supplier artifact fails before application files or registration are modified.

The source completeness flag remains false. Direct supplier acquisition does not establish complete corresponding source coverage or approval for a public bundled distribution. The older validation bundle retains its separate incomplete-source gate.

yt-dlp 2026.08.19's combined Windows executable is GPLv3 or later; upstream source is Unlicense. Its bundled Python 3.10.11, EJS 0.8.0 and other components have their own notices. Upstream release: https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.19 ; source: https://github.com/yt-dlp/yt-dlp/tree/2026.08.19 ; EJS: https://github.com/yt-dlp/ejs/tree/0.8.0 ; Python: https://github.com/python/cpython/tree/v3.10.11 . Preserved upstream license and aggregate component notices are included in licenses/.

Deno 2.9.7 is MIT with third-party components retaining their licenses. Source and dependency lock: https://github.com/denoland/deno/tree/v2.9.7 ; https://github.com/denoland/deno/blob/v2.9.7/Cargo.lock . Supplemental license texts are preserved and are not claimed to be an exhaustive component inventory.

FFmpeg/ffprobe 9.0.2 are acquired from Gyan's public essentials archive. The supplier GPLv3 LICENSE and detailed build README are acquired from that same hash-pinned archive. Supplier documentation: https://www.gyan.dev/ffmpeg/builds/ ; FFmpeg legal documentation: https://ffmpeg.org/legal.html . The local proof confirms libmp3lame and H.264/AAC stream copy. Complete corresponding source/build inputs for all compiled dependencies have not been assembled by Dwnl.

Our helper statically includes Go 1.27.2 runtime/standard library and golang.org/x/sys v0.38.0 under BSD-3-Clause. Their copyright, conditions and disclaimers are preserved in licenses/. Source: https://cs.opensource.google/go/go/+/refs/tags/go1.27.2 ; https://cs.opensource.google/go/x/sys/+/refs/tags/v0.38.0 . Inno Setup 7.1.0 is used without modification; its license text is also preserved. Source: https://github.com/jrsoftware/issrc/tree/is-7_1_0 . No global runtime is installed.

This is a private prototype. No setup has been executed by the build workflow, no browser profile is edited, and no native host is registered as part of compilation.
