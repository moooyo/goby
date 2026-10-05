#!/usr/bin/env python3
"""Exercise standalone player packaging boundaries without building an image."""

import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


REPOSITORY = Path(__file__).resolve().parents[1]
BUILDER = REPOSITORY / "scripts" / "build-player-oci.py"
SPEC = importlib.util.spec_from_file_location("player_oci", BUILDER)
PLAYER_OCI = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PLAYER_OCI)
NODE_PIN = "node:24-alpine@sha256:" + "a" * 64
NGINX_PIN = "nginxinc/nginx-unprivileged:1.29-alpine@sha256:" + "b" * 64


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / "source"
        self.source.mkdir()
        for name in PLAYER_OCI.SOURCE_FILES:
            (self.source / name).write_text(name + "\n", encoding="utf-8")
        (self.source / "src").mkdir()
        (self.source / "src" / "main.tsx").write_text("export {};\n", encoding="utf-8")

    def prepare(self, output, node_pin=NODE_PIN, nginx_pin=NGINX_PIN):
        return subprocess.run([sys.executable, str(BUILDER), "--source-dir", str(self.source),
            "--output-dir", str(output), "--image", "goby-player:prepared", "--revision", "c" * 40,
            "--node-image", node_pin, "--nginx-image", nginx_pin, "--source-date-epoch", "1700000000", "--prepare-only"],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30, check=False)

    def test_frozen_source_excludes_credentials_tests_and_generated_inputs(self):
        (self.source / "src" / "components.tsx").write_text("export {};\n", encoding="utf-8")
        (self.source / "src" / "components").mkdir()
        (self.source / "src" / "components" / "button.tsx").write_text("export {};\n", encoding="utf-8")
        for directory in ("node_modules", "dist", "notices", "tests", "scripts"):
            (self.source / directory).mkdir()
            (self.source / directory / "ignored.txt").write_text("private", encoding="utf-8")
        (self.source / ".env").write_text("SECRET=private", encoding="utf-8")
        output = self.root / "prepared"
        result = self.prepare(output)
        self.assertEqual(result.returncode, 0, result.stderr)
        receipt = json.loads((output / "build-receipt.json").read_text())
        files = receipt["sourceInventory"]["files"]
        self.assertEqual({item["name"] for item in files}, {*PLAYER_OCI.SOURCE_FILES, "src/main.tsx", "src/components.tsx", "src/components/button.tsx"})
        self.assertEqual(receipt["sourceInventory"]["sha256"], hashlib.sha256(PLAYER_OCI.canonical(files)).hexdigest())
        self.assertEqual(receipt["status"], "prepared")
        self.assertFalse(receipt["runtimeAccepted"])
        self.assertFalse(receipt["embeddedInGo"])
        self.assertFalse((output / "image-id.txt").exists())
        self.assertEqual((output / "context" / "src" / "main.tsx").stat().st_mtime, 1700000000)

    def test_unpinned_images_are_rejected_without_output(self):
        for node, nginx in (("node:24-alpine", NGINX_PIN), (NODE_PIN, "nginxinc/nginx-unprivileged:1.29-alpine")):
            output = self.root / "unpinned"
            result = self.prepare(output, node, nginx)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("digest-pinned", result.stderr)
            self.assertFalse(output.exists())

    def test_existing_output_is_preserved(self):
        output = self.root / "existing"
        output.mkdir()
        sentinel = output / "retained"
        sentinel.write_bytes(b"keep")
        result = self.prepare(output)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sentinel.read_bytes(), b"keep")
        self.assertEqual(list(output.iterdir()), [sentinel])

    def test_output_cannot_overlap_source(self):
        output = self.source / "new" / "nested"
        result = self.prepare(output)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("must not overlap", result.stderr)
        self.assertFalse(output.exists())
        self.assertFalse(output.parent.exists())

    def test_source_symlink_is_rejected(self):
        external = self.root / "external.ts"
        external.write_bytes(b"secret")
        (self.source / "src" / "linked.ts").symlink_to(external)
        output = self.root / "linked"
        result = self.prepare(output)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(output.exists())

    def test_linked_optional_public_directory_is_rejected(self):
        external = self.root / "outside"
        external.mkdir()
        (self.source / "public").symlink_to(external, target_is_directory=True)
        result = self.prepare(self.root / "public-link")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unlinked directory", result.stderr)


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "assets").mkdir()
        (self.root / "assets" / "entry-123.js").write_text("export {};", encoding="utf-8")
        (self.root / "assets" / "entry-123.css").write_text("body {}", encoding="utf-8")

    def html(self, value):
        (self.root / "index.html").write_text(value, encoding="utf-8")

    def test_valid_vite_entry_binds_actual_assets(self):
        self.html('<script type="module" crossorigin src="/assets/entry-123.js"></script>'
                  '<link rel="stylesheet" crossorigin href="/assets/entry-123.css">')
        result = PLAYER_OCI.bundle_inventory(self.root)
        self.assertEqual([item["kind"] for item in result["entryReferences"]], ["module-script", "stylesheet"])
        self.assertEqual(result["entryReferences"][0]["sha256"], hashlib.sha256(b"export {};").hexdigest())

    def test_nonproduction_and_missing_entries_are_rejected(self):
        for url in ("/src/main.tsx", "https://other.invalid/entry.js", "/assets/../entry.js", "/assets/missing.js"):
            with self.subTest(url=url):
                self.html(f'<script type="module" src="{url}"></script>')
                with self.assertRaises(ValueError):
                    PLAYER_OCI.bundle_inventory(self.root)

    def test_empty_or_inline_entry_is_rejected(self):
        for html in ("<html></html>", '<script type="module">window.alert(1)</script>'):
            self.html(html)
            with self.assertRaises(ValueError):
                PLAYER_OCI.bundle_inventory(self.root)


class NoticeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        shutil.copyfile(REPOSITORY / "web/player/build-notices.mjs", self.root / "build-notices.mjs")
        (self.root / "THIRD-PARTY.md").write_text("# Attribution\n", encoding="utf-8")
        self.package = self.root / "node_modules" / "example-font"
        self.package.mkdir(parents=True)
        (self.package / "package.json").write_text(json.dumps({"name": "example-font", "version": "1.0.0"}))
        (self.package / "OFL.txt").write_text("Test OFL text\n", encoding="utf-8")
        self.lock = {"lockfileVersion": 3, "packages": {
            "": {"name": "test"}, "node_modules/example-font": {"version": "1.0.0", "license": "OFL-1.1"},
            "node_modules/build-only": {"version": "1.0.0", "license": "MIT", "dev": True}}}

    def collect(self):
        (self.root / "package-lock.json").write_text(json.dumps(self.lock), encoding="utf-8")
        return subprocess.run(["node", str(self.root / "build-notices.mjs")], stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, text=True, timeout=30, check=False)

    def test_production_font_notices_are_retained_with_lock_binding(self):
        result = self.collect()
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads((self.root / "notices" / "manifest.json").read_text())
        self.assertEqual(len(report["packages"]), 1)
        package = report["packages"][0]
        self.assertEqual(package["license"], "OFL-1.1")
        self.assertEqual(package["files"][0]["sha256"], hashlib.sha256(b"Test OFL text\n").hexdigest())
        self.assertEqual(report["packageLock"]["sha256"], hashlib.sha256((self.root / "package-lock.json").read_bytes()).hexdigest())
        self.assertEqual((self.root / "notices" / "node_modules" / "example-font" / "OFL.txt").read_text(), "Test OFL text\n")

    def test_missing_license_or_version_mismatch_fails(self):
        (self.package / "OFL.txt").unlink()
        result = self.collect()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no retained license", result.stderr)
        (self.package / "OFL.txt").write_bytes(b"license")
        self.lock["packages"]["node_modules/example-font"]["version"] = "2.0.0"
        result = self.collect()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("differs from lockfile", result.stderr)

    def test_traversal_and_linked_license_are_rejected(self):
        self.lock["packages"]["node_modules/../private"] = {"version": "1.0.0"}
        result = self.collect()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Unsupported production package path", result.stderr)
        del self.lock["packages"]["node_modules/../private"]
        (self.package / "OFL.txt").unlink()
        (self.package / "OFL.txt").symlink_to(self.root / "THIRD-PARTY.md")
        result = self.collect()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Linked notice input", result.stderr)

    def test_oversized_license_is_rejected(self):
        with (self.package / "OFL.txt").open("wb") as stream:
            stream.truncate(2 * 1024 * 1024 + 1)
        result = self.collect()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Invalid notice input", result.stderr)


if __name__ == "__main__":
    unittest.main()
