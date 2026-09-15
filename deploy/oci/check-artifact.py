#!/usr/bin/env python3
"""Bind two pinned release files to a narrow Linux amd64 OCI build input."""

import contextlib
import hashlib
import json
import os
import re
import stat
import struct
import sys


BINARY_LIMIT = 128 * 1024 * 1024
MANIFEST_LIMIT = 16 * 1024 * 1024
OUTPUT_NAME = "oci-artifact-binding.json"
TARGET = {"os": "linux", "arch": "amd64", "cgoEnabled": False}


class ArtifactError(Exception):
    """A fixed diagnostic code without file contents or filesystem details."""


def require(condition, code):
    if not condition:
        raise ArtifactError(code)


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink,
            info.st_uid, info.st_gid, info.st_size,
            info.st_mtime_ns, info.st_ctime_ns)


def directory_identity(info):
    # Creating our output changes directory timestamps, but must not change the
    # directory object, type, ownership, or permissions.
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid)


def open_root(path):
    require(isinstance(path, str) and path.startswith("/") and path != "/"
            and len(path) <= 4096 and "\0" not in path
            and os.path.normpath(path) == path, "payload_path_invalid")
    require(all(hasattr(os, name) for name in
                ("O_NOFOLLOW", "O_DIRECTORY", "O_CLOEXEC", "O_NONBLOCK")),
            "platform_unsupported")
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
    descriptor = os.open("/", flags)
    try:
        for component in path.split("/")[1:]:
            require(component not in ("", ".", ".."), "payload_path_invalid")
            child = os.open(component, flags, dir_fd=descriptor)
            parent = descriptor
            descriptor = child
            os.close(parent)
        require(stat.S_ISDIR(os.fstat(descriptor).st_mode), "payload_not_directory")
        return descriptor
    except BaseException:
        os.close(descriptor)
        raise


def assert_root(path, descriptor, before):
    current = open_root(path)
    try:
        require(directory_identity(os.fstat(descriptor)) == before
                and directory_identity(os.fstat(current)) == before,
                "payload_identity_changed")
    finally:
        os.close(current)


def assert_input(root, name, descriptor, before):
    current = os.fstat(descriptor)
    named = os.stat(name, dir_fd=root, follow_symlinks=False)
    require(file_identity(current) == before and file_identity(named) == before,
            "input_identity_changed")


def read_input(root, name, maximum, retain_content):
    # NONBLOCK prevents a substituted FIFO from blocking before fstat rejects
    # it. No pathname reopen supplies the bytes used by this hash.
    descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC
                         | os.O_NONBLOCK, dir_fd=root)
    try:
        info = os.fstat(descriptor)
        require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1
                and 0 < info.st_size <= maximum, "input_file_invalid")
        before = file_identity(info)
        assert_input(root, name, descriptor, before)
        digest = hashlib.sha256()
        chunks = []
        header = bytearray()
        total = 0
        while True:
            block = os.read(descriptor, min(1024 * 1024, maximum + 1 - total))
            if not block:
                break
            total += len(block)
            require(total <= maximum, "input_file_too_large")
            digest.update(block)
            if retain_content:
                chunks.append(block)
            elif len(header) < 64:
                header.extend(block[:64 - len(header)])
        assert_input(root, name, descriptor, before)
        require(total == info.st_size, "input_identity_changed")
        content = b"".join(chunks) if retain_content else bytes(header)
        return descriptor, before, {"name": name, "bytes": total,
                                    "sha256": digest.hexdigest()}, content
    except BaseException:
        os.close(descriptor)
        raise


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "manifest_duplicate_field")
        result[key] = value
    return result


def invalid_constant(_value):
    raise ArtifactError("manifest_json_invalid")


def validate_manifest(content, binary):
    try:
        value = json.loads(content.decode("utf-8"), object_pairs_hook=unique_object,
                           parse_constant=invalid_constant)
    except (UnicodeError, ValueError, RecursionError) as cause:
        raise ArtifactError("manifest_json_invalid") from cause
    require(isinstance(value, dict)
            and value.get("kind") == "goby-linux-embedded-administrator-build"
            and type(value.get("version")) is int and value["version"] == 1,
            "manifest_kind_invalid")
    target = value.get("target")
    require(isinstance(target, dict) and set(target) == set(TARGET)
            and target.get("os") == "linux" and target.get("arch") == "amd64"
            and target.get("cgoEnabled") is False, "manifest_target_invalid")
    recorded = value.get("binary")
    require(isinstance(recorded, dict)
            and set(recorded) == {"name", "bytes", "sha256"}
            and recorded.get("name") == "goby"
            and type(recorded.get("bytes")) is int
            and recorded["bytes"] == binary["bytes"]
            and recorded.get("sha256") == binary["sha256"],
            "manifest_binary_invalid")
    # Other release-manifest fields retain their original meaning. This checker
    # does not validate source provenance, embedding, or a build-command receipt.


def validate_elf(header):
    require(len(header) == 64 and header[:4] == b"\x7fELF"
            and header[4:7] == bytes((2, 1, 1)), "elf_header_invalid")
    elf_type, machine, version = struct.unpack_from("<HHI", header, 16)
    header_size, = struct.unpack_from("<H", header, 52)
    require(elf_type in (2, 3) and machine == 62 and version == 1
            and header_size == 64, "elf_target_invalid")
    return {"class": 64, "endianness": "little", "machine": machine,
            "type": "ET_EXEC" if elf_type == 2 else "ET_DYN"}


def publish_binding(root, value, verify_inputs):
    content = (json.dumps(value, sort_keys=True, indent=2) + "\n").encode("utf-8")
    descriptor = None
    try:
        descriptor = os.open(OUTPUT_NAME, os.O_WRONLY | os.O_CREAT | os.O_EXCL
                             | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=root)
        os.fchmod(descriptor, 0o600)
        position = 0
        while position < len(content):
            written = os.write(descriptor, content[position:])
            require(written > 0, "binding_write_failed")
            position += written
        os.fsync(descriptor)
        written_identity = file_identity(os.fstat(descriptor))
        require(written_identity[2] & 0o777 == 0o600
                and written_identity[3] == 1 and written_identity[6] == len(content),
                "binding_write_failed")
        verify_inputs()
        assert_input(root, OUTPUT_NAME, descriptor, written_identity)
    finally:
        # A failure retains its partial output. Comparing a name and then
        # unlinking it cannot atomically bind deletion to this descriptor.
        # A caller must require exit zero and use a fresh directory after failure.
        if descriptor is not None:
            os.close(descriptor)


def check_artifact(payload, expected_binary_sha256, expected_manifest_sha256):
    require(all(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value)
                for value in (expected_binary_sha256, expected_manifest_sha256)),
            "expected_sha256_invalid")
    payload = os.fspath(payload)
    try:
        with contextlib.ExitStack() as owned:
            root = open_root(payload)
            owned.callback(os.close, root)
            root_identity = directory_identity(os.fstat(root))
            binary_fd, binary_identity, binary, header = read_input(root, "goby", BINARY_LIMIT, False)
            owned.callback(os.close, binary_fd)
            require(binary["sha256"] == expected_binary_sha256, "binary_sha256_mismatch")
            manifest_fd, manifest_identity, manifest, content = read_input(root, "manifest.json", MANIFEST_LIMIT, True)
            owned.callback(os.close, manifest_fd)
            require(manifest["sha256"] == expected_manifest_sha256, "manifest_sha256_mismatch")
            validate_manifest(content, binary)
            elf = validate_elf(header)

            def verify_inputs():
                assert_root(payload, root, root_identity)
                assert_input(root, "goby", binary_fd, binary_identity)
                assert_input(root, "manifest.json", manifest_fd, manifest_identity)

            verify_inputs()
            binding = {"kind": "goby-oci-artifact-binding", "version": 1,
                       "binary": binary, "manifest": manifest, "target": dict(TARGET),
                       "elf": elf, "runtimeVerified": False,
                       "requiresSuccessfulCheckExit": True,
                       "boundary": "Checks only the two externally pinned release files, their manifest binding, and the ELF header. Source provenance, embedded administrator content, and runtime behavior require independent verification."}
            publish_binding(root, binding, verify_inputs)
            return binding
    except FileExistsError as cause:
        raise ArtifactError("binding_already_exists") from cause
    except OSError as cause:
        raise ArtifactError("artifact_io_failed") from cause


def main(argv=None):
    arguments = sys.argv[1:] if argv is None else argv
    if len(arguments) != 3:
        print("Usage: python3 check-artifact.py /payload EXPECTED_BINARY_SHA256 EXPECTED_MANIFEST_SHA256", file=sys.stderr)
        return 2
    try:
        check_artifact(*arguments)
    except ArtifactError as cause:
        print("OCI artifact check failed: " + str(cause), file=sys.stderr)
        return 1
    print(json.dumps({"binding": OUTPUT_NAME, "runtimeVerified": False}, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
