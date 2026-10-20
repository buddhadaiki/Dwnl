#!/usr/bin/env python3
"""Per-user Dwnl installer. No privilege escalation or system package changes."""
import argparse
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import shutil
import stat
import sys
import tempfile
import urllib.request
import uuid
import zipfile

HOST_NAME = "com.dwnl.host"
EXTENSION_ID = "lphdonkmiecidfgjfplhdfnpphmdioeh"
RECEIPT = ".dwnl-runtime.json"
MARKER = "Dwnl Linux runtime v1"


def fail(message):
    raise RuntimeError(message)


def safe_path(path):
    """Refuse links in every existing ancestor, including dangling links."""
    path = Path(os.path.abspath(path))
    for item in [path, *path.parents]:
        if item.is_symlink():
            fail(f"Link simbólico não permitido: {item}")
    return path


def child(root, name):
    relative = PurePosixPath(name)
    if not name or "\\" in name or relative.is_absolute() or any(p in ("", ".", "..") for p in name.split("/")):
        fail(f"Caminho inválido no pacote: {name}")
    return safe_path(root.joinpath(*relative.parts))


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def inventory(root, exclude=()):
    result = {}
    for path in sorted(root.rglob("*")):
        safe_path(path)
        if path.is_dir():
            continue
        if not stat.S_ISREG(path.lstat().st_mode):
            fail(f"Arquivo especial não permitido: {path}")
        name = path.relative_to(root).as_posix()
        if name not in exclude:
            result[name] = {"sha256": digest(path), "sizeBytes": path.stat().st_size}
    return result


def check_files(root, expected, exclude=()):
    for name in expected:
        child(root, name)
    actual = inventory(root, exclude)
    if actual != expected:
        fail(f"Inventário ou SHA-256 divergente em {root}; nenhum arquivo existente foi removido.")


def read_json(path):
    safe_path(path)
    return json.loads(path.read_text(encoding="utf-8-sig"))


def write_json(path, data):
    path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")


def locations():
    home = safe_path(Path.home())
    data_env = os.environ.get("XDG_DATA_HOME")
    if data_env and not Path(data_env).is_absolute():
        fail("XDG_DATA_HOME precisa ser absoluto.")
    data = safe_path(Path(data_env) if data_env else home / ".local/share")
    config_env = os.environ.get("XDG_CONFIG_HOME")
    if config_env and not Path(config_env).is_absolute():
        fail("XDG_CONFIG_HOME precisa ser absoluto.")
    config = safe_path(Path(config_env) if config_env else home / ".config")
    manifest = safe_path(config / "google-chrome/NativeMessagingHosts" / (HOST_NAME + ".json"))
    runtime = safe_path(data / "Dwnl/runtime")
    return runtime, manifest


def expected_manifest(runtime):
    return {"name": HOST_NAME, "description": "Dwnl native download helper", "path": str(runtime / "dwnl-host"), "type": "stdio", "allowed_origins": [f"chrome-extension://{EXTENSION_ID}/"]}


def owned_runtime(runtime):
    safe_path(runtime)
    receipt = read_json(runtime / RECEIPT)
    if receipt.get("owner") != MARKER or receipt.get("manifest") != expected_manifest(runtime):
        fail("Runtime existente sem comprovante de propriedade compatível.")
    check_files(runtime, receipt["files"], (RECEIPT,))
    return receipt


def preflight():
    if sys.platform != "linux" or platform.machine() not in ("x86_64", "amd64"):
        fail("Este instalador requer Ubuntu 24.04+ x86_64 (Linux de 64 bits).")
    if os.geteuid() == 0:
        fail("Execute como seu usuário normal, sem sudo.")
    missing = [str(p) for p in (Path("/usr/bin/ffmpeg"), Path("/usr/bin/ffprobe"), Path("/usr/bin/zenity")) if not p.is_file() or not os.access(p, os.X_OK)]
    if missing:
        fail("Pré-requisitos ausentes: " + ", ".join(missing) + ". Veja LEIA-ME.md para instalar os pacotes Ubuntu.")


def download(supplier, destination):
    url = supplier["url"]
    if not url.startswith("https://github.com/"):
        fail("Fornecedor fora do GitHub oficial fixado no pacote.")
    request = urllib.request.Request(url, headers={"User-Agent": "Dwnl-Linux-Installer/1"})
    with urllib.request.urlopen(request, timeout=60) as response, destination.open("xb") as output:
        if not response.url.startswith("https://"):
            fail("Redirecionamento de download sem HTTPS.")
        size = 0
        while True:
            block = response.read(1024 * 1024)
            if not block:
                break
            size += len(block)
            if size > supplier["sizeBytes"]:
                fail("Download maior que o tamanho fixado no pacote.")
            output.write(block)
    if destination.stat().st_size != supplier["sizeBytes"] or digest(destination) != supplier["sha256"]:
        fail("Download não corresponde ao SHA-256/tamanho fixado: " + supplier["name"])


def install_supplier(supplier, stage, scratch):
    archive = scratch / (supplier["name"] + ".download")
    download(supplier, archive)
    if supplier["archiveType"] == "zip":
        with zipfile.ZipFile(archive) as source:
            entries = source.infolist()
            if len(entries) != len(supplier["files"]) or {e.filename for e in entries} != {f["entry"] for f in supplier["files"]}:
                fail("Inventário inesperado no ZIP do fornecedor.")
            for item in supplier["files"]:
                entry = source.getinfo(item["entry"])
                child(stage, entry.filename)
                if stat.S_ISLNK(entry.external_attr >> 16) or entry.file_size != item["sizeBytes"]:
                    fail("Link ou tamanho inesperado no ZIP do fornecedor.")
                target = child(stage, item["path"])
                target.parent.mkdir(parents=True, exist_ok=True)
                with source.open(entry) as input_file, target.open("xb") as output:
                    shutil.copyfileobj(input_file, output)
    elif supplier["archiveType"] == "executable" and len(supplier["files"]) == 1:
        target = child(stage, supplier["files"][0]["path"])
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(archive, target)
    else:
        fail("Formato de fornecedor não suportado.")
    for item in supplier["files"]:
        target = child(stage, item["path"])
        if target.stat().st_size != item["sizeBytes"] or digest(target) != item["sha256"]:
            fail("Executável extraído não corresponde ao SHA-256 fixado.")
        target.chmod(0o700)
        target.parent.chmod(0o700)


def verify_package(package):
    report = read_json(package / "package.json")
    if report.get("platform") != "linux-x86_64" or report.get("schemaVersion") != 1:
        fail("Pacote de instalação incompatível.")
    check_files(package, report["files"], ("package.json",))
    identity = read_json(package / "extension-identity.json")
    if identity["hostName"] != HOST_NAME or identity["extensionId"] != EXTENSION_ID:
        fail("Identidade da extensão divergente.")
    return report


@contextmanager
def installation_lock(runtime):
    import fcntl

    safe_path(runtime)
    runtime.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    lock = safe_path(runtime.parent / ".dwnl-use.lock")
    descriptor = os.open(lock, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        info = os.fstat(descriptor)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_nlink != 1 or stat.S_IMODE(info.st_mode) != 0o600:
            fail(f"Arquivo de controle de uso inseguro: {lock}")
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            fail("Dwnl está em uso ou outra instalação/remoção está ativa. Feche o popup e os downloads Dwnl no Chrome e tente novamente.")
        yield
    finally:
        os.close(descriptor)
    # Keep the inode permanently: unlinking allows two independent flock locks.


def install(package, runtime, manifest):
    with installation_lock(runtime):
        _install(package, runtime, manifest)


def _install(package, runtime, manifest):
    verify_package(package)
    safe_path(runtime)
    safe_path(manifest)
    if runtime.exists():
        owned_runtime(runtime)
    expected = expected_manifest(runtime)
    if manifest.exists() and read_json(manifest) != expected:
        fail("Manifesto Chrome existente pertence a outra instalação; não será sobrescrito.")
    runtime.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    # All network and archive work finishes before replacing any installed file.
    with tempfile.TemporaryDirectory(prefix=".dwnl-install-", dir=runtime.parent) as scratch_name:
        scratch = Path(scratch_name)
        stage = scratch / "runtime"
        stage.mkdir(mode=0o700)
        for name in ("dwnl-host", "install.py", "uninstall.sh", "suppliers.linux.lock.json", "LEIA-ME.md", "NOTICES.md"):
            shutil.copyfile(package / name, stage / name)
        shutil.copytree(package / "licenses", stage / "licenses")
        for name in ("dwnl-host", "uninstall.sh"):
            (stage / name).chmod(0o700)
        suppliers = read_json(package / "suppliers.linux.lock.json")
        if suppliers.get("platform") != "linux-x86_64":
            fail("Lock de fornecedores incompatível.")
        for supplier in suppliers["suppliers"]:
            print(f"Baixando e verificando {supplier['name']} {supplier['version']}...", flush=True)
            install_supplier(supplier, stage, scratch)
        for tool in ("ffmpeg", "ffprobe"):
            wrapper = stage / "bin" / tool
            wrapper.write_text(f'#!/bin/sh\nexec /usr/bin/{tool} "$@"\n', encoding="utf-8")
            wrapper.chmod(0o700)
        write_json(stage / RECEIPT, {"owner": MARKER, "manifest": expected, "files": inventory(stage)})
        # A failed rollback must not let TemporaryDirectory erase the old runtime.
        backup = runtime.parent / (".dwnl-previous-" + uuid.uuid4().hex)
        previous_manifest = manifest.read_bytes() if manifest.exists() else None
        manifest.parent.mkdir(parents=True, exist_ok=True)
        new_manifest = scratch / "native-host.json"
        write_json(new_manifest, expected)
        # Recheck ownership immediately before touching the old installation.
        safe_path(runtime)
        safe_path(manifest)
        if runtime.exists():
            owned_runtime(runtime)
        if manifest.exists() and read_json(manifest) != expected:
            fail("Manifesto foi modificado durante a preparação.")
        had_runtime = runtime.exists()
        if had_runtime:
            runtime.rename(backup)
        published = False
        try:
            stage.rename(runtime)
            published = True
            # Manifest can be on another filesystem, so stage in its own directory.
            with tempfile.NamedTemporaryFile(prefix=".dwnl-", dir=manifest.parent, delete=False) as tmp:
                temp_manifest = Path(tmp.name)
                tmp.write(new_manifest.read_bytes())
            try:
                os.replace(temp_manifest, manifest)
            finally:
                temp_manifest.unlink(missing_ok=True)
            owned_runtime(runtime)
            if read_json(manifest) != expected:
                fail("Falha de leitura após registrar o Native Messaging.")
        except BaseException:
            if published and runtime.exists():
                shutil.rmtree(runtime)
            if had_runtime:
                backup.rename(runtime)
            if previous_manifest is None:
                manifest.unlink(missing_ok=True)
            else:
                manifest.write_bytes(previous_manifest)
            raise
        if had_runtime:
            shutil.rmtree(backup)
    print(f"Dwnl instalado em {runtime}\nNative Messaging registrado para Google Chrome.\nSiga LEIA-ME.md para carregar a extensão.")


def uninstall(runtime, manifest):
    with installation_lock(runtime):
        _uninstall(runtime, manifest)


def _uninstall(runtime, manifest):
    if not runtime.exists():
        fail("Runtime não encontrado; nada foi removido.")
    receipt = owned_runtime(runtime)
    if manifest.exists() and read_json(manifest) != receipt["manifest"]:
        fail("Manifesto modificado/externo; nada foi removido.")
    if manifest.exists():
        manifest.unlink()
    # Only validated owned files are removed. Parent/state/download directories stay.
    for name in receipt["files"]:
        child(runtime, name).unlink()
    (runtime / RECEIPT).unlink()
    for path in sorted(runtime.rglob("*"), key=lambda p: len(p.parts), reverse=True):
        if path.is_dir():
            path.rmdir()
    runtime.rmdir()
    print("Dwnl auxiliar removido. Downloads, estado e extensão foram preservados.")


def main():
    parser = argparse.ArgumentParser(description="Instalação Dwnl por usuário (Ubuntu x86_64)")
    parser.add_argument("--uninstall", action="store_true")
    args = parser.parse_args()
    if args.uninstall:
        if sys.platform != "linux" or os.geteuid() == 0:
            fail("Desinstale no Linux como usuário normal, sem sudo.")
    else:
        preflight()
    runtime, manifest = locations()
    if args.uninstall:
        uninstall(runtime, manifest)
    else:
        install(Path(__file__).resolve().parent, runtime, manifest)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError, KeyError, zipfile.BadZipFile) as error:
        print(f"Dwnl: {error}", file=sys.stderr)
        sys.exit(1)
