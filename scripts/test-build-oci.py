#!/usr/bin/env python3
"""Exercise OCI preparation boundaries without a Docker daemon or real build."""

import hashlib
import json
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
import uuid


REPOSITORY = Path(__file__).resolve().parents[1]
BUILDER = REPOSITORY / "scripts" / "build-oci.py"


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.release = self.root / "release"
        self.release.mkdir()
        header = bytearray(64)
        header[:7] = b"\x7fELF\x02\x01\x01"
        struct.pack_into("<HHI", header, 16, 2, 62, 1)
        struct.pack_into("<H", header, 52, 64)
        (self.release / "goby").write_bytes(header)
        manifest = {"kind": "goby-linux-embedded-administrator-build", "version": 1,
                    "target": {"os": "linux", "arch": "amd64", "cgoEnabled": False},
                    "binary": {"name": "goby", "bytes": len(header),
                               "sha256": hashlib.sha256(header).hexdigest()}}
        (self.release / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")

    def prepare(self, output, image="goby-test:prepared"):
        return subprocess.run([sys.executable, str(BUILDER), "--release-dir", str(self.release),
                               "--output-dir", str(output), "--image", image,
                               "--revision", "a" * 40, "--prepare-only"],
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
                              timeout=30, check=False)

    def test_complete_context_without_image_build(self):
        output = self.root / "prepared"
        result = self.prepare(output)
        self.assertEqual(result.returncode, 0, result.stderr)
        receipt = json.loads((output / "build-receipt.json").read_text())
        names = {entry["name"] for entry in receipt["contextFiles"]}
        self.assertIn("intro-fingerprint/vendor/SHA256SUMS", names)
        self.assertIn("application-notices/THIRD_PARTY_NOTICES.md", names)
        self.assertIn("toolchain-patches/ffmpeg-decoder-queue-wakeup.patch", names)
        self.assertEqual(receipt["status"], "prepared")
        self.assertFalse(receipt["runtimeAccepted"])
        self.assertFalse(receipt["registryPublished"])
        self.assertFalse((output / "image-id.txt").exists())
        self.assertFalse((output / "context" / "oci-artifact-binding.json").exists())

    def test_changed_binary_is_rejected_before_image_build(self):
        with (self.release / "goby").open("ab") as stream:
            stream.write(b"changed")
        output = self.root / "bad-input"
        result = self.prepare(output)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("artifact-check.log", result.stderr)
        self.assertFalse((output / "image-build.log").exists())

    def test_overlapping_output_does_not_modify_inputs(self):
        for source in (self.release, REPOSITORY / "tools" / "intro-fingerprint",
                       REPOSITORY / "third_party" / "licenses"):
            output = source / ("oci-rejected-" + uuid.uuid4().hex)
            result = self.prepare(output)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("must not overlap", result.stderr)
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


if __name__ == "__main__":
    unittest.main()
