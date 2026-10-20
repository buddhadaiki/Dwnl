"""Create/read back Linux delivery archives from a fresh staging directory."""
import argparse
import hashlib
import json
from pathlib import Path
import tarfile
import zipfile


def sha(data):
    return hashlib.sha256(data).hexdigest()


def files(root):
    paths = sorted(root.rglob("*"))
    if any(p.is_symlink() for p in paths):
        raise ValueError("Links are not permitted in release archives")
    return {p.relative_to(root).as_posix(): p.read_bytes() for p in paths if p.is_file()}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--stage", required=True, type=Path)
    parser.add_argument("--assets", required=True, type=Path)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    payload = args.stage / "installer"
    report = {"schemaVersion": 1, "platform": "linux-x86_64", "version": args.version, "files": {name: {"sha256": sha(data), "sizeBytes": len(data)} for name, data in files(payload).items()}}
    (payload / "package.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    args.assets.mkdir()
    tar_path = args.assets / f"Dwnl-{args.version}-ubuntu-x64.tar.gz"
    tar_files = files(payload)
    with tarfile.open(tar_path, "w:gz", format=tarfile.PAX_FORMAT) as archive:
        for name in tar_files:
            info = archive.gettarinfo(str(payload / name), arcname="Dwnl-ubuntu/" + name)
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            info.mode = 0o755 if name in ("install.sh", "uninstall.sh", "dwnl-host") else 0o644
            with (payload / name).open("rb") as source:
                archive.addfile(info, source)
    with tarfile.open(tar_path) as archive:
        actual = {m.name.removeprefix("Dwnl-ubuntu/"): archive.extractfile(m).read() for m in archive.getmembers() if m.isfile()}
        if actual != tar_files:
            raise ValueError("Tar readback mismatch")
    zip_path = args.assets / f"Dwnl-{args.version}-chrome-ubuntu.zip"
    extension_files = files(args.stage / "chrome")
    with zipfile.ZipFile(zip_path, "w", compression=zipfile.ZIP_DEFLATED) as archive:
        for name, data in extension_files.items():
            archive.writestr(name, data)
    with zipfile.ZipFile(zip_path) as archive:
        if archive.testzip() or {n: archive.read(n) for n in archive.namelist()} != extension_files:
            raise ValueError("Extension ZIP readback mismatch")
    (args.assets / "LEIA-ME.md").write_bytes((payload / "LEIA-ME.md").read_bytes())
    checksums = "".join(f"{sha(p.read_bytes())}  {p.name}\n" for p in sorted(args.assets.iterdir()))
    (args.assets / "SHA256SUMS.txt").write_text(checksums, encoding="utf-8")
    print(f"Linux archives verified: {args.assets}")


if __name__ == "__main__":
    main()
