# Dependency delivery options — evaluation only

Evaluated 2026-10-13 against the current prototype and official primary sources. No bootstrap, replacement dependency, source-gate change, credential request or installation was implemented. Root's current validation build and race fixes remain the immediate integration priority. `completeCorrespondingSource=false` remains accurate.

The exact FFmpeg build's source-cache endpoint returned HTTP 401. We will not acquire credentials or authenticate to bypass that result. The existing 126 source archives, recipe inventories and exact hashes remain useful partial collateral; the cache is not represented as captured.

## Options and tradeoffs

| Option | One installer experience | Source/licensing position | Main tradeoff |
|---|---|---|---|
| Keep the current embedded private binaries | Yes; offline setup | Existing partial source/notice inventory remains incomplete | Lowest integration change; remains validation-only under the approved gate |
| Replace FFmpeg with a reviewed supplier build | Yes; can still embed it | Requires exact binary/source/native-dependency correspondence; a main source link alone does not establish this | Compatibility checks and source review needed; no ready replacement verified here |
| Build a smaller FFmpeg/ffprobe from accessible release sources | Yes; offline setup once built | Fewer dependencies can make exact source/build collateral practical | New isolated build/toolchain work and regression validation; not a quick packaging substitution |
| One Inno bootstrap acquiring pinned private tools directly from public official suppliers | Yes; one setup UI, initial network needed | Changes who supplies the media binaries; does not automatically waive notices or license obligations | Small installer and no binary mirror, but dependency availability, cancellation, rollback and pinning need design/review |

### Trusted prebuilt alternatives

FFmpeg's official download page provides source releases and links to Windows build suppliers including BtbN and Gyan. It currently lists FFmpeg **9.0.2** with source archives and signatures. This supports a bounded source-based alternative, not a finding that any Windows supplier artifact already has complete corresponding source. [Official FFmpeg downloads](https://ffmpeg.org/download.html).

BtbN publishes dependency recipes, GPL/LGPL variants and build instructions. Its LGPL variants omit GPL-only libraries such as x264/x265. Its floating latest and retention policy must be considered for durable version pins. Selecting LGPL reduces a class of dependency obligations but does not remove exact-source or relinking obligations for libraries still used. [BtbN's own build documentation](https://github.com/BtbN/FFmpeg-Builds).

Gyan currently offers 64-bit static GPLv3 Windows builds, hashes, FFmpeg source commit links and library lists; its essentials build includes libmp3lame. This is a legitimate supplier candidate. Those published references do **not**, by themselves, establish a complete exact external-library source/build bundle for the binary. No Gyan/BtbN replacement was downloaded or verified here. [Gyan supplier documentation](https://www.gyan.dev/ffmpeg/builds/).

For an offline package, the more controllable fallback is an isolated small build using a specific public FFmpeg release and only the external libraries needed for actual Dwnl behavior, including a pinned public LAME source release for MP3. LAME's official page supplies source tarballs and currently lists release **4.0**; its compatibility with our chosen FFmpeg and output requirements has not been tested. [Official LAME source downloads](https://lame.sourceforge.io/download.php).

**Inference from the prototype's current behavior:** a build supporting the required input decoders/demuxers, stream-copy merge, MP4/MKV muxing, ffprobe and MP3 encoding can potentially omit many unrelated codecs/hardware libraries. That still needs 128/192/320 kbps fixture verification, stream-copy checks, codec/stream/progress/cancel behavior and platform compatibility checks. Replacing FFmpeg alone also does not close the separate yt-dlp native-bundle/Deno notice gaps. [FFmpeg codec documentation](https://ffmpeg.org/ffmpeg-codecs.html#libmp3lame), [FFmpeg's legal guidance on exact binary/source correspondence and external libraries](https://ffmpeg.org/legal.html).

### One installer with direct supplier acquisition

Inno's official `DownloadTemporaryFile` function accepts a required SHA256 and rejects a mismatch. The `[Files]` download mechanism can use a public URL with SHA256 verification; Inno also supports archive extraction. Therefore, a single setup window can acquire dependencies into Dwnl's private per-user directory without sending the user to separate installers or installing global runtimes. This is capability evidence, not an implemented/tested bootstrap. [Inno hash-checked downloads](https://jrsoftware.org/ishelp/topic_isxfunc_downloadtemporaryfile.htm), [Inno file download/archive options](https://jrsoftware.org/ishelp/topic_filessection.htm).

The proposed experience is one setup UI showing the official component acquisition, followed by a complete-ready result. Dwnl's helper and its own retained notices could remain embedded; yt-dlp/Deno/FFmpeg/ffprobe would be obtained directly from their public suppliers. No credentials, user accounts, cookies, supplier installers, services, autostart or PATH edits should be required. Private executable layout, exact extension origin and owned uninstall scope would remain design requirements.

The upstream yt-dlp 2026.08.19 and Deno v2.9.7 pages mark those releases immutable. They are stronger candidates for version-specific public download pins than a changing latest URL. Their suppliers still describe component licensing; direct acquisition must preserve applicable notices. [yt-dlp's exact release](https://github.com/yt-dlp/yt-dlp/releases/tag/2026.08.19), [Deno's exact release](https://github.com/denoland/deno/releases/tag/v2.9.7).

The current FFmpeg archive URL is floating `latest`. A fixed hash would stop installation after that URL changes, which protects integrity but creates an availability failure. A future bootstrap must choose a publicly accessible durable supplier asset/tag and freeze its actual archive hash, extracted executable hashes, architecture and capabilities. It must never silently use whatever a latest URL returns, relax a hash on failure, fall back to credentials, or download different tools to make setup succeed. No replacement FFmpeg pin or new hash is invented here.

Before such a design could replace the present package, review the dependency-acquisition/license model and verify: corrupt/changed/missing downloads fail before registration; archive paths/links cannot escape owned staging; inner hashes and actual MP3/probe capabilities match; cancellation/network failure leaves no usable-looking partial install; an update preserves the prior working installation; setup cannot terminate unrelated processes; and uninstall preserves downloads/extension and removes only owned state. Registration should occur only after all required private files are ready. These are future acceptance criteria, not claimed test results.

## Recommendation and unchanged gate

Finish the current real-payload **validation** artifact and race fixes first. For the next reviewed design, **prefer one Inno bootstrap with public supplier acquisition and fixed hashes** if first-install networking is acceptable: it keeps the requested one-installer experience and avoids maintaining a bundled mirror of the large media binaries. Treat the changed supplying/conveying arrangement as a specific license review question; it is not a declaration that GPL obligations vanish or that the existing source gate may be toggled true.

If fully offline installation or redistribution of a complete binary payload is required, prefer the controlled smaller-build path over swapping to another large prebuilt bundle whose exact source correspondence is also unverified. Proceed only after its source/build/notice inventory and regressions are checked. No alternative reviewed here is ready to replace the current dependencies today.

Neither option repairs the current network capability gate or proves any public platform download works. No bootstrap implementation, compiler/tool installation, source-cache authentication, design approval or normal release-gate change happened in this evaluation.

## Targeted host notice repair completed

The actual helper's `go version -m` readback identifies **Go 1.27.2**, **golang.org/x/sys v0.38.0**, Windows amd64 and CGO_ENABLED=0. Their byte-exact local BSD 3-Clause licenses were copied to `release/licenses/Go-1.27.2-LICENSE.txt` and `release/licenses/golang.org-x-sys-v0.38.0-LICENSE.txt`. Each is **1,453 bytes**, SHA256 **911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad**.

The release lock now has 12 hashed license files with version/use attribution for the host components. The matching notice section and notice SHA256 **5823b3672e2725040a72b48f04a28e9127078481dd6ac51a52f3407560209904** were updated. `Assert-DwnlCollateral` and diff whitespace checks passed. Existing six partial source artifacts and completeness=false were preserved. These BSD binary-notice additions do not resolve the separate media source gaps; no installer/package/shared-root file was edited.
