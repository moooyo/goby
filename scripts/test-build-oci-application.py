#!/usr/bin/env python3
"""Exercise application image provenance boundaries without a Docker daemon."""

import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest


spec = importlib.util.spec_from_file_location("builder", Path(__file__).with_name("build-oci-application.py"))
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class SourceBindingTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.revision = "a" * 40
        self.files = {"cmd/goby/main.go": b"package main\n", "internal/feature/feature.go": b"package feature\n",
                      "web/admin/embedded.go": b"package admin\n", "go.mod": b"module example\n",
                      "go.sum": b"module checksum\n", "README.md": b"Source archive fixture.\n"}

    def archive(self, entries=None, comment=None):
        path = self.root / "source.tar"
        with tarfile.open(path, "w", format=tarfile.PAX_FORMAT,
                          pax_headers={"comment": comment or self.revision}) as stream:
            for name, content in entries or self.files.items():
                entry = tarfile.TarInfo(name)
                if content is None:
                    entry.type = tarfile.SYMTYPE
                    entry.linkname = "target"
                    stream.addfile(entry)
                else:
                    entry.size = len(content)
                    stream.addfile(entry, io.BytesIO(content))
        receipt = self.root / "source.json"
        builder.write_json(receipt, {"kind": "goby-source-archive", "version": 1, "revision": self.revision,
                                    "archive": {"name": path.name, **builder.record(path)}})
        return path, receipt

    def manifest(self):
        records = {name: {"bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()}
                   for name, content in self.files.items()}
        manifest = {"sourceInventory": {"files": [{"name": name, **value} for name, value in records.items()
                                                  if name.startswith(("cmd/", "internal/", "web/admin/"))]},
                    "moduleInputs": {name: records[name] for name in ("go.mod", "go.sum")}}
        return manifest

    def test_complete_application_manifest_is_bound_to_commit_archive(self):
        archive, receipt = self.archive()
        source, files = builder.source_archive(archive, receipt, self.revision)
        builder.bind_manifest_source(self.manifest(), files)
        self.assertEqual(source["revision"], self.revision)
        self.assertEqual(set(files), set(self.files))

    def test_archive_bytes_cannot_be_substituted_after_receipt(self):
        archive, receipt = self.archive()
        with archive.open("ab") as stream:
            stream.write(b"changed")
        with self.assertRaisesRegex(ValueError, "binding mismatch"):
            builder.source_archive(archive, receipt, self.revision)

    def test_commit_comment_cannot_be_replaced_by_a_receipt_claim(self):
        archive, receipt = self.archive(comment="b" * 40)
        with self.assertRaisesRegex(ValueError, "commit marker mismatch"):
            builder.source_archive(archive, receipt, self.revision)

    def test_traversal_link_and_duplicate_archive_members_are_rejected(self):
        for entries in (([('../escape', b'bad')]), ([('/absolute', b'bad')]),
                        ([('link', None)]), ([('duplicate', b'one'), ('duplicate', b'two')])):
            with self.subTest(entries=entries):
                archive, receipt = self.archive(entries=entries)
                with self.assertRaises(ValueError):
                    builder.source_archive(archive, receipt, self.revision)

    def test_changed_missing_duplicate_and_extra_manifest_sources_are_rejected(self):
        archive, receipt = self.archive()
        _, files = builder.source_archive(archive, receipt, self.revision)
        manifest = self.manifest()
        variants = []
        changed = copy.deepcopy(manifest)
        changed["sourceInventory"]["files"][0]["sha256"] = "f" * 64
        variants.append(changed)
        missing = copy.deepcopy(manifest)
        missing["sourceInventory"]["files"].pop()
        variants.append(missing)
        duplicate = copy.deepcopy(manifest)
        duplicate["sourceInventory"]["files"].append(duplicate["sourceInventory"]["files"][0])
        variants.append(duplicate)
        extra = copy.deepcopy(manifest)
        extra["sourceInventory"]["files"].append({"name": "README.md", **files["README.md"]})
        variants.append(extra)
        for value in variants:
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    builder.bind_manifest_source(value, files)

    def test_changed_module_file_is_rejected(self):
        archive, receipt = self.archive()
        _, files = builder.source_archive(archive, receipt, self.revision)
        manifest = self.manifest()
        manifest["moduleInputs"]["go.mod"]["sha256"] = "f" * 64
        with self.assertRaisesRegex(ValueError, "module input"):
            builder.bind_manifest_source(manifest, files)

    def test_recipe_cannot_silently_differ_from_pinned_archive(self):
        path = builder.RECIPE / "Dockerfile.application"
        name = path.relative_to(builder.ROOT).as_posix()
        builder.bind_repository_inputs([path], {name: builder.record(path)})
        with self.assertRaisesRegex(ValueError, "differs from source archive"):
            builder.bind_repository_inputs([path], {name: {"bytes": 1, "sha256": "f" * 64}})


class ToolSelectionTests(unittest.TestCase):
    def details(self, profile):
        prefix = "/opt/goby-amd-ffmpeg" if profile == "amd" else "/opt/ffmpeg/9.0.1"
        return {"Config": {"Env": ["GOBY_FFMPEG=" + prefix + "/bin/ffmpeg",
                                  "GOBY_FFPROBE=" + prefix + "/bin/ffprobe",
                                  "GOBY_PG_DUMP=/usr/lib/postgresql/17/bin/pg_dump",
                                  "GOBY_PG_RESTORE=/usr/lib/postgresql/17/bin/pg_restore"]}}

    def test_amd_binds_renderer_and_both_driver_libraries(self):
        paths = builder.tool_paths(self.details("amd"), "amd")
        self.assertIn("/opt/goby-amd-ffmpeg/lib/libplacebo.so.351", paths)
        self.assertIn("/usr/lib/x86_64-linux-gnu/dri/radeonsi_drv_video.so", paths)
        self.assertIn("/usr/lib/x86_64-linux-gnu/libvulkan_radeon.so", paths)

    def test_wrong_profile_or_main_tool_path_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "Unexpected base tool paths"):
            builder.tool_paths(self.details("software"), "amd")
        details = self.details("software")
        details["Config"]["Env"][0] = "GOBY_FFMPEG=/usr/bin/ffmpeg"
        with self.assertRaisesRegex(ValueError, "Unexpected base tool paths"):
            builder.tool_paths(details, "software")

    def test_analysis_configuration_binds_the_independent_fingerprint_tool(self):
        fingerprint = "/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint"
        pins = {fingerprint: "a" * 64}
        configuration = {"enabled": True, "cacheDirectory": "/var/cache/goby/analysis",
                         "fingerprintPath": fingerprint, "fingerprintSHA256": "a" * 64,
                         "introFFmpegPath": "/usr/bin/ffmpeg", "introFFmpegSHA256": builder.ANALYSIS_SHA256}
        builder.bind_analysis_configuration(json.dumps(configuration), pins)
        configuration.pop("introFFmpegPath")
        with self.assertRaisesRegex(ValueError, "configuration does not bind"):
            builder.bind_analysis_configuration(json.dumps(configuration), pins)

    def test_analysis_configuration_rejects_a_stale_tool_hash(self):
        fingerprint = "/opt/goby-intro-fingerprint/bin/goby-intro-fingerprint"
        configuration = {"enabled": True, "cacheDirectory": "/var/cache/goby/analysis",
                         "fingerprintPath": fingerprint, "fingerprintSHA256": "a" * 64,
                         "introFFmpegPath": "/usr/bin/ffmpeg", "introFFmpegSHA256": "b" * 64}
        with self.assertRaisesRegex(ValueError, "configuration does not bind"):
            builder.bind_analysis_configuration(json.dumps(configuration), {fingerprint: "a" * 64})


if __name__ == "__main__":
    unittest.main()
