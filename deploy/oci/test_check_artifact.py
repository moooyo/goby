"""Pure temporary-file contracts; no product binary or container is executed."""

import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import struct
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock


SPEC = importlib.util.spec_from_file_location("oci_check_artifact", Path(__file__).with_name("check-artifact.py"))
checker = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(checker)


def sha256(content):
    return hashlib.sha256(content).hexdigest()


def elf_fixture(elf_type=2):
    # This is a synthetic header with inert bytes, not an executable or evidence
    # that administrator assets are embedded in a native product build.
    header = bytearray(64)
    header[:7] = b"\x7fELF\x02\x01\x01"
    struct.pack_into("<HHI", header, 16, elf_type, 62, 1)
    struct.pack_into("<H", header, 52, 64)
    return bytes(header) + b"SYNTHETIC NONEXECUTABLE FIXTURE\n"


class CheckArtifactTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="goby-oci-artifact-test-")
        self.addCleanup(self.temporary.cleanup)
        self.scope = Path(self.temporary.name).resolve()
        self.count = 0

    def fixture(self, binary=None, change_manifest=None, manifest_content=None):
        self.count += 1
        payload = self.scope / ("payload-" + str(self.count))
        payload.mkdir()
        binary = elf_fixture() if binary is None else binary
        value = {"kind": "goby-linux-embedded-administrator-build", "version": 1,
                 "target": {"os": "linux", "arch": "amd64", "cgoEnabled": False},
                 "binary": {"name": "goby", "bytes": len(binary), "sha256": sha256(binary)},
                 "nativeRuntimeExecuted": False, "ociImageBuilt": False,
                 "buildMetadata": "synthetic fixture; no build receipt"}
        if change_manifest is not None:
            change_manifest(value)
        manifest = json.dumps(value, separators=(",", ":")).encode("utf-8") if manifest_content is None else manifest_content
        (payload / "goby").write_bytes(binary)
        (payload / "goby").chmod(0o640)
        (payload / "manifest.json").write_bytes(manifest)
        (payload / "manifest.json").chmod(0o600)
        return SimpleNamespace(payload=payload, binary=binary, manifest=manifest,
                               binary_sha256=sha256(binary), manifest_sha256=sha256(manifest),
                               output=payload / checker.OUTPUT_NAME)

    def check(self, case):
        return checker.check_artifact(case.payload, case.binary_sha256, case.manifest_sha256)

    def rejected(self, case, code=None):
        with self.assertRaises(checker.ArtifactError) as raised:
            self.check(case)
        if code is not None:
            self.assertEqual(str(raised.exception), code)
        self.assertFalse(case.output.exists())

    def test_valid_exec_and_pie_bind_only_observed_inputs(self):
        for elf_type, type_name in ((2, "ET_EXEC"), (3, "ET_DYN")):
            with self.subTest(type=type_name):
                case = self.fixture(binary=elf_fixture(elf_type))
                before = {name: checker.file_identity((case.payload / name).stat()) for name in ("goby", "manifest.json")}
                binding = self.check(case)
                self.assertEqual(json.loads(case.output.read_text(encoding="utf-8")), binding)
                self.assertEqual(case.output.stat().st_mode & 0o777, 0o600)
                self.assertEqual(binding["binary"], {"name": "goby", "bytes": len(case.binary), "sha256": case.binary_sha256})
                self.assertEqual(binding["manifest"], {"name": "manifest.json", "bytes": len(case.manifest), "sha256": case.manifest_sha256})
                self.assertEqual(binding["target"], {"os": "linux", "arch": "amd64", "cgoEnabled": False})
                self.assertEqual(binding["elf"], {"class": 64, "endianness": "little", "machine": 62, "type": type_name})
                self.assertIs(binding["runtimeVerified"], False)
                self.assertIs(binding["requiresSuccessfulCheckExit"], True)
                self.assertIn("require independent verification", binding["boundary"])
                self.assertEqual((case.payload / "goby").read_bytes(), case.binary)
                self.assertEqual((case.payload / "manifest.json").read_bytes(), case.manifest)
                self.assertEqual(before, {name: checker.file_identity((case.payload / name).stat()) for name in before})

    def test_mixed_external_pins_and_manifest_binary_binding_are_rejected(self):
        first = self.fixture()
        second = self.fixture(binary=elf_fixture() + b"different release\n")
        for binary_pin, manifest_pin, code in (
                (second.binary_sha256, first.manifest_sha256, "binary_sha256_mismatch"),
                (first.binary_sha256, second.manifest_sha256, "manifest_sha256_mismatch")):
            with self.subTest(code=code):
                with self.assertRaisesRegex(checker.ArtifactError, "^" + code + "$"):
                    checker.check_artifact(first.payload, binary_pin, manifest_pin)
                self.assertFalse(first.output.exists())
        mixed = self.fixture(change_manifest=lambda value: value["binary"].update(sha256=second.binary_sha256))
        self.rejected(mixed, "manifest_binary_invalid")

    def test_expected_pins_require_exact_lowercase_sha256(self):
        case = self.fixture()
        for invalid in ("", "a" * 63, "a" * 65, "A" * 64, "g" * 64, " " + case.binary_sha256):
            for position in (0, 1):
                with self.subTest(value=invalid, position=position):
                    pins = [case.binary_sha256, case.manifest_sha256]
                    pins[position] = invalid
                    with self.assertRaisesRegex(checker.ArtifactError, "^expected_sha256_invalid$"):
                        checker.check_artifact(case.payload, *pins)
                    self.assertFalse(case.output.exists())

    def test_manifest_target_and_required_fields_use_strict_types(self):
        changes = [
            lambda value: value.pop("kind"),
            lambda value: value.update(kind="goby-linux-systemd-package"),
            lambda value: value.update(version=True),
            lambda value: value.pop("target"),
            lambda value: value["target"].update(os="windows"),
            lambda value: value["target"].update(arch="arm64"),
            lambda value: value["target"].update(cgoEnabled=True),
            lambda value: value["target"].update(cgoEnabled=0),
            lambda value: value["target"].update(extra="unsupported"),
            lambda value: value.pop("binary"),
            lambda value: value["binary"].update(name="another-binary"),
            lambda value: value["binary"].update(bytes=str(value["binary"]["bytes"])),
            lambda value: value["binary"].update(bytes=True),
            lambda value: value["binary"].pop("sha256"),
        ]
        for index, change in enumerate(changes):
            with self.subTest(case=index):
                self.rejected(self.fixture(change_manifest=change))

    def test_manifest_rejects_malformed_duplicate_and_non_json_values(self):
        for content in (b"{", b"[]", b"null", b"\xff", b'{"kind":"one","kind":"two"}',
                        b'{"version":NaN}', b'{"version":Infinity}'):
            with self.subTest(content=content):
                self.rejected(self.fixture(manifest_content=content))

    def test_elf_header_must_actually_be_little_endian_amd64(self):
        mutations = [(0, b"NOPE"), (4, b"\x01"), (5, b"\x02"), (6, b"\x00"),
                     (16, struct.pack("<H", 1)), (18, struct.pack("<H", 183)),
                     (20, struct.pack("<I", 0)), (52, struct.pack("<H", 0))]
        for offset, replacement in mutations:
            with self.subTest(offset=offset):
                binary = bytearray(elf_fixture())
                binary[offset:offset + len(replacement)] = replacement
                self.rejected(self.fixture(binary=bytes(binary)))
        self.rejected(self.fixture(binary=elf_fixture()[:63]), "elf_header_invalid")

    def test_symlink_roots_ancestors_and_inputs_are_not_followed(self):
        case = self.fixture()
        alias = self.scope / "payload-link"
        alias.symlink_to(case.payload, target_is_directory=True)
        with self.assertRaises(checker.ArtifactError):
            checker.check_artifact(alias, case.binary_sha256, case.manifest_sha256)
        self.assertFalse(case.output.exists())
        ancestor = self.scope / "ancestor-link"
        ancestor.symlink_to(self.scope, target_is_directory=True)
        with self.assertRaises(checker.ArtifactError):
            checker.check_artifact(ancestor / case.payload.name, case.binary_sha256, case.manifest_sha256)
        self.assertFalse(case.output.exists())
        for name in ("goby", "manifest.json"):
            with self.subTest(input=name):
                linked = self.fixture()
                outside = self.scope / ("external-" + name)
                original = (linked.payload / name).read_bytes()
                outside.write_bytes(original)
                (linked.payload / name).unlink()
                (linked.payload / name).symlink_to(outside)
                self.rejected(linked)
                self.assertEqual(outside.read_bytes(), original)

    def test_single_link_regular_files_and_size_limits_are_enforced(self):
        for name, maximum in (("goby", checker.BINARY_LIMIT), ("manifest.json", checker.MANIFEST_LIMIT)):
            with self.subTest(input=name, failure="hard-link"):
                case = self.fixture()
                os.link(case.payload / name, self.scope / ("hard-link-" + name))
                self.rejected(case, "input_file_invalid")
            with self.subTest(input=name, failure="size"):
                case = self.fixture()
                with (case.payload / name).open("r+b") as output:
                    output.truncate(maximum + 1)
                self.rejected(case, "input_file_invalid")
            with self.subTest(input=name, failure="directory"):
                case = self.fixture()
                (case.payload / name).unlink()
                (case.payload / name).mkdir()
                self.rejected(case, "input_file_invalid")
        empty = self.fixture(binary=b"")
        self.rejected(empty, "input_file_invalid")

    def test_input_mutation_during_fd_read_cannot_publish(self):
        case = self.fixture()
        binary_inode = (case.payload / "goby").stat().st_ino
        original_read = checker.os.read
        changed = False

        def read_then_change(descriptor, length):
            nonlocal changed
            data = original_read(descriptor, length)
            if data and not changed and os.fstat(descriptor).st_ino == binary_inode:
                changed = True
                with (case.payload / "goby").open("ab") as output:
                    output.write(b"changed during read")
            return data

        with mock.patch.object(checker.os, "read", side_effect=read_then_change):
            self.rejected(case, "input_identity_changed")
        self.assertTrue(changed)

    def test_existing_binding_is_never_overwritten_or_followed(self):
        for linked in (False, True):
            with self.subTest(symlink=linked):
                case = self.fixture()
                sentinel = self.scope / ("existing-" + str(linked))
                sentinel.write_bytes(b"existing binding sentinel\n")
                if linked:
                    case.output.symlink_to(sentinel)
                else:
                    case.output.write_bytes(sentinel.read_bytes())
                with self.assertRaisesRegex(checker.ArtifactError, "^binding_already_exists$"):
                    self.check(case)
                self.assertEqual(case.output.read_bytes(), b"existing binding sentinel\n")
                self.assertEqual(sentinel.read_bytes(), b"existing binding sentinel\n")

    def test_failed_output_write_retains_partial_without_unlinking_names(self):
        case = self.fixture()
        original_write = checker.os.write
        writes = 0

        def fail_write(descriptor, data):
            nonlocal writes
            writes += 1
            if writes == 1:
                return original_write(descriptor, data[:8])
            raise OSError("synthetic output failure")

        with mock.patch.object(checker.os, "write", side_effect=fail_write), \
                mock.patch.object(checker.os, "unlink", side_effect=AssertionError("failed checks must not unlink names")):
            with self.assertRaisesRegex(checker.ArtifactError, "^artifact_io_failed$"):
                self.check(case)
        partial = case.output.read_bytes()
        self.assertEqual(len(partial), 8)
        with self.assertRaises(json.JSONDecodeError):
            json.loads(partial)
        with self.assertRaisesRegex(checker.ArtifactError, "^binding_already_exists$"):
            self.check(case)
        self.assertEqual(case.output.read_bytes(), partial)
        self.assertEqual((case.payload / "goby").read_bytes(), case.binary)
        self.assertEqual((case.payload / "manifest.json").read_bytes(), case.manifest)

    def test_cli_argument_and_error_output_are_bounded(self):
        case = self.fixture()
        output, error = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(output), contextlib.redirect_stderr(error):
            self.assertEqual(checker.main([]), 2)
            self.assertEqual(checker.main([str(case.payload), "private-invalid-value", case.manifest_sha256]), 1)
        self.assertEqual(output.getvalue(), "")
        self.assertIn("expected_sha256_invalid", error.getvalue())
        self.assertNotIn("private-invalid-value", error.getvalue())
        self.assertNotIn(str(case.payload), error.getvalue())
        self.assertFalse(case.output.exists())


if __name__ == "__main__":
    unittest.main()
