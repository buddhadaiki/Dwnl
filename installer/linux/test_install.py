import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

spec = importlib.util.spec_from_file_location("dwnl_install", Path(__file__).with_name("install.py"))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.package = self.root / "package"
        self.package.mkdir()
        self.runtime = self.root / "home/.local/share/Dwnl/runtime"
        self.manifest = self.root / "home/.config/google-chrome/NativeMessagingHosts/com.dwnl.host.json"
        self.bytes = b"fake supplier executable\n"
        self.supplier = {"name": "yt-dlp", "version": "test", "url": "https://github.com/test/fixture", "archiveType": "executable", "sha256": hashlib.sha256(self.bytes).hexdigest(), "sizeBytes": len(self.bytes), "files": [{"entry": "", "path": "bin/yt-dlp", "sha256": hashlib.sha256(self.bytes).hexdigest(), "sizeBytes": len(self.bytes)}]}
        for name in ("dwnl-host", "install.py", "uninstall.sh", "LEIA-ME.md", "NOTICES.md"):
            (self.package / name).write_text(name, encoding="utf-8")
        (self.package / "licenses").mkdir()
        (self.package / "licenses/fixture.txt").write_text("Fixture license")
        installer.write_json(self.package / "extension-identity.json", {"hostName": installer.HOST_NAME, "extensionId": installer.EXTENSION_ID})
        installer.write_json(self.package / "suppliers.linux.lock.json", {"platform": "linux-x86_64", "suppliers": [self.supplier]})
        self.seal()

    def seal(self):
        installer.write_json(self.package / "package.json", {"schemaVersion": 1, "platform": "linux-x86_64", "files": installer.inventory(self.package, ("package.json",))})

    def response(self, _request, **_kwargs):
        stream = io.BytesIO(self.bytes)
        stream.url = "https://release-assets.githubusercontent.com/fixture"
        return stream

    def install(self):
        with patch.object(installer.urllib.request, "urlopen", side_effect=self.response):
            installer.install(self.package, self.runtime, self.manifest)

    def test_install_upgrade_uninstall_preserves_user_state(self):
        self.install()
        expected = installer.expected_manifest(self.runtime)
        self.assertEqual(installer.read_json(self.manifest), expected)
        self.assertEqual((self.runtime / "bin/yt-dlp").read_bytes(), self.bytes)
        self.assertEqual((self.runtime / "bin/ffmpeg").read_text(), '#!/bin/sh\nexec /usr/bin/ffmpeg "$@"\n')
        state = self.runtime.parent / "state/downloads.db"
        state.parent.mkdir()
        state.write_bytes(b"preserve user state")
        (self.package / "dwnl-host").write_bytes(b"updated host")
        self.seal()
        self.install()
        self.assertEqual((self.runtime / "dwnl-host").read_bytes(), b"updated host")
        installer.uninstall(self.runtime, self.manifest)
        self.assertFalse(self.runtime.exists())
        self.assertFalse(self.manifest.exists())
        self.assertEqual(state.read_bytes(), b"preserve user state")

    def test_failed_download_preserves_previous_installation(self):
        self.install()
        before = installer.inventory(self.runtime)
        manifest = self.manifest.read_bytes()
        with patch.object(installer.urllib.request, "urlopen", side_effect=OSError("network unavailable")):
            with self.assertRaisesRegex(OSError, "network unavailable"):
                installer.install(self.package, self.runtime, self.manifest)
        self.assertEqual(installer.inventory(self.runtime), before)
        self.assertEqual(self.manifest.read_bytes(), manifest)
        self.assertTrue((self.runtime.parent / ".dwnl-use.lock").exists())

    def test_failed_manifest_commit_rolls_back_runtime(self):
        self.install()
        before = installer.inventory(self.runtime)
        manifest = self.manifest.read_bytes()
        (self.package / "dwnl-host").write_bytes(b"new host")
        self.seal()
        with patch.object(installer.os, "replace", side_effect=OSError("simulated manifest write failure")):
            with self.assertRaisesRegex(OSError, "simulated"):
                self.install()
        self.assertEqual(installer.inventory(self.runtime), before)
        self.assertEqual(self.manifest.read_bytes(), manifest)

    def test_modified_or_unowned_files_block_uninstall(self):
        self.install()
        (self.runtime / "user-file.txt").write_text("preserve")
        with self.assertRaisesRegex(RuntimeError, "Inventário"):
            installer.uninstall(self.runtime, self.manifest)
        self.assertTrue(self.manifest.exists())
        self.assertEqual((self.runtime / "user-file.txt").read_text(), "preserve")

    def test_foreign_manifest_is_not_overwritten(self):
        self.manifest.parent.mkdir(parents=True)
        self.manifest.write_text('{"name":"other"}')
        with self.assertRaisesRegex(RuntimeError, "outra instalação"):
            self.install()
        self.assertEqual(self.manifest.read_text(), '{"name":"other"}')
        self.assertFalse(self.runtime.exists())

    def test_package_hash_mismatch_aborts_before_network(self):
        (self.package / "dwnl-host").write_bytes(b"tampered")
        with patch.object(installer.urllib.request, "urlopen") as network:
            with self.assertRaisesRegex(RuntimeError, "SHA-256"):
                installer.install(self.package, self.runtime, self.manifest)
            network.assert_not_called()

    def test_supplier_hash_mismatch_does_not_install(self):
        self.bytes = b"wrong bytes of same size!!"
        with self.assertRaisesRegex(RuntimeError, "SHA-256|tamanho"):
            self.install()
        self.assertFalse(self.runtime.exists())
        self.assertFalse(self.manifest.exists())

    def test_path_traversal_is_rejected(self):
        for name in ("../escape", "/absolute", "bin/../../escape", "bin\\escape", "bin//file", "./file"):
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                installer.child(self.root, name)

    def test_symlink_ancestor_is_rejected(self):
        target = self.root / "target"
        target.mkdir()
        link = self.root / "link"
        try:
            link.symlink_to(target, target_is_directory=True)
        except OSError:
            self.skipTest("Symlink creation not available to this Windows user")
        with self.assertRaisesRegex(RuntimeError, "simbólico"):
            installer.safe_path(link / "file")

    def test_zip_symlink_is_rejected(self):
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            entry = zipfile.ZipInfo("deno")
            entry.create_system = 3
            entry.external_attr = 0o120777 << 16
            archive.writestr(entry, b"some-target")
        self.bytes = buffer.getvalue()
        self.supplier.update({"archiveType": "zip", "sizeBytes": len(self.bytes), "sha256": hashlib.sha256(self.bytes).hexdigest(), "files": [{"entry": "deno", "path": "bin/deno", "sizeBytes": 11, "sha256": "unused"}]})
        stage = self.root / "stage"
        stage.mkdir()
        with patch.object(installer.urllib.request, "urlopen", side_effect=self.response):
            with self.assertRaisesRegex(RuntimeError, "Link"):
                installer.install_supplier(self.supplier, stage, self.root)

    def test_lock_prevents_competing_mutation(self):
        with installer.installation_lock(self.runtime):
            with self.assertRaisesRegex(RuntimeError, "outra instalação"):
                self.install()
        self.assertTrue((self.runtime.parent / ".dwnl-use.lock").exists())

    def test_running_host_shared_lock_blocks_install_and_uninstall(self):
        import fcntl

        self.install()
        lock = self.runtime.parent / ".dwnl-use.lock"
        with lock.open("rb") as active_host:
            fcntl.flock(active_host.fileno(), fcntl.LOCK_SH | fcntl.LOCK_NB)
            with self.assertRaisesRegex(RuntimeError, "em uso"):
                self.install()
            with self.assertRaisesRegex(RuntimeError, "em uso"):
                installer.uninstall(self.runtime, self.manifest)
        self.assertTrue(self.manifest.exists())
        self.assertTrue(self.runtime.exists())

    def test_runtime_executable_directories_are_private(self):
        self.install()
        self.assertEqual(self.runtime.stat().st_mode & 0o777, 0o700)
        self.assertEqual((self.runtime / "bin").stat().st_mode & 0o777, 0o700)

    def test_xdg_paths_and_relative_environment(self):
        home = self.root / "home"
        env = {"XDG_DATA_HOME": str(home / "data"), "XDG_CONFIG_HOME": str(home / "config")}
        with patch.object(installer.Path, "home", return_value=home), patch.dict(os.environ, env):
            runtime, manifest = installer.locations()
            self.assertEqual(runtime, home / "data/Dwnl/runtime")
            self.assertEqual(manifest, home / "config/google-chrome/NativeMessagingHosts/com.dwnl.host.json")
        with patch.dict(os.environ, {"XDG_DATA_HOME": "relative"}):
            with self.assertRaisesRegex(RuntimeError, "absoluto"):
                installer.locations()


if __name__ == "__main__":
    unittest.main()
