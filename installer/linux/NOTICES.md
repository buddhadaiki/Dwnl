# Dwnl Linux component notices

This archive includes the Dwnl native host compiled with the private Go toolchain, plus its installer. It does not include yt-dlp, Deno, FFmpeg or ffprobe binaries. The installer acquires the pinned official yt-dlp and Deno release assets directly and verifies their SHA-256 digests; the Ubuntu package manager supplies FFmpeg/ffprobe and Zenity.

The native host contains Go runtime/standard library code and golang.org/x/sys v0.38.0. Their BSD 3-Clause license texts are retained in `licenses/Go-1.27.2-LICENSE.txt` and `licenses/golang.org-x-sys-v0.38.0-LICENSE.txt`. Toolchain/build versions can be inspected with `go version -m dwnl-host` on a development system.

- Go source/license: https://cs.opensource.google/go/go/+/refs/tags/go1.27.2:LICENSE
- golang.org/x/sys source/license: https://cs.opensource.google/go/x/sys/+/refs/tags/v0.38.0:LICENSE

The combined standalone yt-dlp executable is GPL-3.0-or-later; upstream yt-dlp source is Unlicense. Deno source is MIT, with additional upstream component licenses. Exact asset URLs, versions, hashes and source references are in `suppliers.linux.lock.json`. Source links and acquisition do not represent an exhaustive license/source audit of the downloaded third-party binaries. Do not redistribute an installed runtime as if it were this supplier-acquisition archive.

Ubuntu FFmpeg build features and licensing are those of the installed distribution packages. Package licenses and copyright notices are available under `/usr/share/doc` on the target system.
