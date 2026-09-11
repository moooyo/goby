#!/usr/bin/env python3
"""Exercise the schema25 deployment operator with fenced memory fixtures only.

Run this suite only through authorized root SSH, with the new operator source
as its sole argument. The historical deployment operator is never loaded.
These guards do not constitute a database restore or deployment rehearsal.
"""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import datetime as dt
import fcntl
import gzip
import hashlib
import http.client
import http.cookies
import io
import json
import linecache
import os
from pathlib import Path
import re
import secrets
import selectors
import shlex
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tarfile
import time
import types
import unittest
from unittest.mock import Mock, patch
import urllib.parse

sys.dont_write_bytecode = True

OPERATOR = None
SOURCE_LINES = {}
ACTIVE_FENCES = []
FULL_PACKAGES = ["github.com/moooyo/goby/" + package for package in (
    "cmd/goby", "internal/activity", "internal/artwork", "internal/backupformat",
    "internal/backuppg", "internal/backupstore", "internal/config", "internal/database",
    "internal/diagnostics", "internal/events", "internal/identity", "internal/library",
    "internal/lifecycle", "internal/media", "internal/metadata", "internal/playback",
    "internal/recovery", "internal/recoverycontrol", "internal/recoverydb", "internal/server",
    "internal/settings", "internal/subtitle", "internal/tasks", "internal/transcode")]
FULL_CLEANUP = ("unit_terminal", "hba_restored_exactly", "goby_backup_m3e_source_removed",
                "goby_backup_m3e_target_removed", "preexisting_catalog_unchanged", "receipt_saved")


def deny_audited_effect(event, _arguments):
    filesystem = {"open", "os.listdir", "os.scandir", "os.remove", "os.rmdir", "os.mkdir",
                  "os.rename", "os.chmod", "os.chown", "os.link", "os.symlink", "os.truncate",
                  "os.utime", "os.chdir", "os.fchdir", "shutil.copyfile", "shutil.copymode",
                  "shutil.copystat", "shutil.copytree", "shutil.move", "shutil.rmtree"}
    if ACTIVE_FENCES and (event in filesystem or event.startswith(("socket.", "subprocess.",
            "os.exec", "os.spawn", "os.fork", "fcntl.")) or event == "os.system"):
        ACTIVE_FENCES[-1].violations.append(event)
        raise AssertionError("Audited external effect: " + event)


class EffectFence(contextlib.ExitStack):
    """Deny real effects, including aliases captured during operator import."""

    def __enter__(self):
        super().__enter__()
        self.violations = []
        ACTIVE_FENCES.append(self)
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "lazycache", lambda filename, module_globals: False))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       list(SOURCE_LINES.get(str(filename), ()))))
        targets = (
            (builtins, ("open",)), (io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (fcntl, ("flock", "lockf", "fcntl", "ioctl")),
            (secrets, ("token_bytes", "token_hex", "token_urlsafe")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (signal, ("signal", "setitimer", "pidfd_send_signal")), (time, ("sleep",)),
            (os, ("open", "fdopen", "stat", "lstat", "fstat", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "close", "lseek", "fsync", "fchmod", "fchown", "ftruncate", "umask",
                  "kill", "killpg", "system", "popen", "fork", "posix_spawn", "posix_spawnp", "execve",
                  "replace", "rename", "mkdir", "makedirs", "remove", "unlink", "rmdir", "chmod", "chown",
                  "link", "symlink", "truncate", "utime", "mkfifo", "mknod", "access", "chdir", "fchdir", "chroot")),
            (Path, ("open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat",
                    "read_bytes", "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink",
                    "chmod", "touch", "rename", "replace", "glob", "rglob", "iterdir")),
        )
        for owner, names in targets:
            for name in names:
                if not hasattr(owner, name):
                    continue
                label = getattr(owner, "__name__", type(owner).__name__) + "." + name

                def deny(*_args, _label=label, **_kwargs):
                    self.violations.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, deny))
        return self

    def __exit__(self, *arguments):
        try:
            result = super().__exit__(*arguments)
        finally:
            if ACTIVE_FENCES and ACTIVE_FENCES[-1] is self:
                ACTIVE_FENCES.pop()
        if self.violations:
            raise AssertionError("External effects attempted: " + ", ".join(self.violations))
        return result


def information(*, size=0, mode=0o644, kind=stat.S_IFREG, uid=0, gid=0, inode=100, links=1):
    return types.SimpleNamespace(st_dev=7, st_ino=inode, st_uid=uid, st_gid=gid,
                                 st_mode=kind | mode, st_size=size, st_nlink=links)


class MemorySource:
    """Synthetic trusted catalogs exercise guards without forging real baselines."""

    def __init__(self, source):
        self.source = source
        self.members = {"go.mod": b"module example.invalid/guard\n", "go.sum": b"synthetic sum\n"}
        migrations = []
        for version in range(1, 26):
            name = f"{version:04d}_guard.sql"
            raw = f"-- Synthetic migration {version}.\n".encode()
            self.members["internal/database/migrations/" + name] = raw
            migrations.append({"version": version, "name": name, "sha256": OPERATOR.sha(raw)})
        for number in range(110):
            self.members[f"internal/guard/input_{number:03d}.go"] = f"// Synthetic input {number}.\n".encode()
        self.catalogs = {}
        for version in (23, 24, 25):
            raw = OPERATOR.canonical({"version": version, "migrations": migrations[:version]})
            self.members[f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json"] = raw
            self.catalogs[version] = OPERATOR.sha(raw)
        self.overrides = {}
        self.refresh_manifest()

    def refresh_manifest(self):
        self.files = {name: OPERATOR.sha(raw) for name, raw in self.members.items()}
        self.manifest = OPERATOR.canonical({"marker": OPERATOR.SOURCE_MARKER, "files": self.files})
        self.digest = OPERATOR.sha(self.manifest)

    def paths(self, root, pattern):
        if root != self.source or pattern != "*":
            raise AssertionError("The source walker escaped its one memory tree")
        return [self.source / name for name in sorted((*self.members, OPERATOR.SOURCE_MANIFEST))]

    def read(self, path, **_kwargs):
        name = path.relative_to(self.source).as_posix()
        return self.manifest if name == OPERATOR.SOURCE_MANIFEST else self.members[name]

    def info(self, path):
        name = path.relative_to(self.source).as_posix()
        return self.overrides.get(name, information(size=len(self.read(path))))


class DeploymentGuardTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())
        self.output = self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.args = types.SimpleNamespace(mode="preflight", source=OPERATOR.WORK / "source-attempt-16",
            manifest_sha256="a" * 64, candidate=OPERATOR.WORK / "candidate-16/goby-linux-amd64",
            candidate_sha256="b" * 64, assets=OPERATOR.WORK / "candidate-16/admin.tar.gz",
            assets_sha256="c" * 64, full_report=OPERATOR.WORK / "candidate-16/report.json",
            full_report_sha256="d" * 64, helper=OPERATOR.WORK / "candidate-16/migrate-schema25",
            helper_sha256="e" * 64, tool_source=OPERATOR.WORK / "tool-build-16",
            tool_manifest_sha256="f" * 64, helper_build_report=OPERATOR.WORK / "candidate-16/helper-build.json",
            helper_build_report_sha256="1" * 64, guard_report=OPERATOR.WORK / "candidate-16/guards.json",
            guard_report_sha256="2" * 64, prepared=None, prepared_sha256=None)

    def replace(self, owner, name, value):
        return self.enterContext(patch.object(owner, name, value))

    def reject(self, action):
        with self.assertRaises(OPERATOR.Failure):
            action()

    def source_fixture(self):
        source = MemorySource(self.args.source)
        self.replace(OPERATOR, "CATALOGS", source.catalogs)
        self.replace(OPERATOR, "directory", lambda path: {"path": str(path), "inode": 70})
        self.replace(OPERATOR, "read_file", source.read)
        self.replace(OPERATOR, "path_info", source.info)
        self.replace(Path, "rglob", lambda path, pattern: source.paths(path, pattern))
        return source

    def report_fixture(self):
        return {"marker": "goby-client-backup-pair-m3e-v1", "status": "passed", "mode": "full",
                "schema": 25, "source": str(self.args.source),
                "source_manifest_sha256": self.args.manifest_sha256, "unit_exit": 0,
                "binary": {"path": str(self.args.candidate), "sha256": self.args.candidate_sha256, "bytes": 2000000},
                "tests": {"top_level_passes": 1700, "failures": 0, "skips": 0},
                "packages": list(FULL_PACKAGES), "cleanup": {name: True for name in FULL_CLEANUP}}

    def input_fixture(self):
        report = self.report_fixture()
        tool_files = {"scripts/test-env/deploy-client-schema25.py": "7" * 64,
                      "scripts/test-env/test-deploy-client-schema25.py": "8" * 64,
                      "scripts/test-env/migrate-client-schema25.go": "9" * 64}
        self.guard_report = {"suite": "client-schema25-deployment-guards", "status": "passed",
            "failures": 0, "errors": 0, "skips": 0, "tests": 41,
            "cases": [f"synthetic_guard_case_{number:02d}" for number in range(41)],
            "operator_sha256": tool_files["scripts/test-env/deploy-client-schema25.py"],
            "guard_sha256": tool_files["scripts/test-env/test-deploy-client-schema25.py"],
            "fixtures": "synthetic-memory-only", "real_database_restore_acceptance": False,
            "real_deployment_acceptance": False}
        self.helper_report = {"schema": "goby-client-schema25-helper-build", "version": 1, "status": "passed",
            "product_source_manifest_sha256": self.args.manifest_sha256,
            "tool_manifest_sha256": self.args.tool_manifest_sha256, "helper_sha256": self.args.helper_sha256,
            "go_version": "go1.27.1", "goos": "linux", "goarch": "amd64", "cgo_enabled": False}
        facts = {self.args.candidate: {"sha256": self.args.candidate_sha256, "identity": {"inode": 11, "bytes": 2000000}},
                 self.args.helper: {"sha256": self.args.helper_sha256, "identity": {"inode": 12, "bytes": 2000000}},
                 self.args.assets: {"sha256": self.args.assets_sha256, "identity": {"inode": 13, "bytes": 1000}}}
        self.replace(OPERATOR, "verify_source", lambda source, digest:
                     {"identity": {"inode": 10}, "manifest_sha256": digest, "files": {"go.mod": "f" * 64}})
        self.replace(OPERATOR, "verify_tool_source", lambda path, digest, product, manifest:
                     {"path": str(path), "manifest_sha256": digest, "files": {**product, **tool_files}})
        self.replace(OPERATOR, "file_fact", lambda path, **kwargs: copy.deepcopy(facts[path]))

        def read_document(path, **kwargs):
            if path == self.args.guard_report:
                return OPERATOR.canonical(self.guard_report)
            if path == self.args.helper_build_report:
                return OPERATOR.canonical(self.helper_report)
            return OPERATOR.canonical(report)

        self.replace(OPERATOR, "read_file", read_document)
        self.replace(OPERATOR, "read_assets", lambda path: {"index.html": b"index", "assets/main.js": b"entry"})
        self.args.full_report_sha256 = OPERATOR.sha(OPERATOR.canonical(report))
        self.args.helper_build_report_sha256 = OPERATOR.sha(OPERATOR.canonical(self.helper_report))
        self.args.guard_report_sha256 = OPERATOR.sha(OPERATOR.canonical(self.guard_report))
        return report, facts

    def tool_fixture(self):
        product = MemorySource(self.args.source)
        root = self.args.tool_source
        operator_bytes = b"# Synthetic executing operator bytes.\n"
        members = {**product.members, OPERATOR.SOURCE_MANIFEST: product.manifest,
                   "scripts/test-env/deploy-client-schema25.py": operator_bytes,
                   "scripts/test-env/test-deploy-client-schema25.py": b"# Synthetic guard bytes.\n",
                   "scripts/test-env/migrate-client-schema25.go": b"package main\n"}
        fixture = {"members": members, "product": product}

        def refresh():
            fixture["manifest"] = OPERATOR.canonical({"marker": "goby-client-schema25-tool-source-v1",
                "product_source_manifest_sha256": product.digest,
                "files": {name: OPERATOR.sha(raw) for name, raw in members.items()}})
            fixture["digest"] = OPERATOR.sha(fixture["manifest"])

        def read(path, **kwargs):
            name = path.relative_to(root).as_posix()
            return fixture["manifest"] if name == OPERATOR.TOOL_MANIFEST else members[name]

        def executing_bytes(path):
            self.assertEqual(str(path), OPERATOR.__file__, "The tool verifier read outside its executing operator")
            return operator_bytes

        self.replace(OPERATOR, "directory", lambda path: {"inode": 77})
        self.replace(OPERATOR, "read_file", read)
        self.replace(OPERATOR, "path_info", lambda path: information(size=len(read(path))))
        self.replace(Path, "rglob", lambda path, pattern: [root / name for name in sorted((*members, OPERATOR.TOOL_MANIFEST))])
        self.replace(Path, "read_bytes", executing_bytes)
        fixture["refresh"] = refresh
        refresh()
        return fixture

    def test_arguments_accept_only_explicit_phase_and_pinned_prepared_receipt(self):
        for mode in ("preflight", "prepare"):
            self.args.mode = mode
            OPERATOR.validate_arguments(self.args)
        self.args.mode = "deploy"
        self.reject(lambda: OPERATOR.validate_arguments(self.args))
        self.args.prepared = OPERATOR.ROOT / ("run-20260911T123456Z-" + "1" * 24) / "prepared.json"
        self.args.prepared_sha256 = "f" * 64
        OPERATOR.validate_arguments(self.args)
        self.args.mode = "prepare"
        self.reject(lambda: OPERATOR.validate_arguments(self.args))

    def test_arguments_reject_path_escape_missing_digests_and_cross_run_receipts(self):
        original = copy.copy(self.args)
        for field, value in (("source", Path("source-attempt-16")),
                ("source", OPERATOR.WORK / "nested/source-attempt-16"),
                ("source", OPERATOR.WORK / "../source-attempt-16"),
                ("source", OPERATOR.WORK / "source-attempt-016"),
                ("source", OPERATOR.WORK / "source-attempt-00"),
                ("candidate", Path("/opt/goby-dev/goby")),
                ("assets", OPERATOR.WORK / "../admin.tar.gz"),
                ("full_report", Path("relative-report.json")),
                ("helper", OPERATOR.ROOT / "migrate-schema25"),
                ("tool_source", OPERATOR.WORK / "../tool-build-16"),
                ("helper_build_report", Path("/tmp/unbound-build.json")), ("mode", "restore")):
            with self.subTest(field=field, value=str(value)):
                self.args = copy.copy(original)
                setattr(self.args, field, value)
                self.reject(lambda: OPERATOR.validate_arguments(self.args))
        for field in ("manifest_sha256", "candidate_sha256", "assets_sha256", "full_report_sha256", "helper_sha256",
                      "tool_manifest_sha256", "helper_build_report_sha256"):
            for value in (None, "", "F" * 64, "0" * 63, "0" * 65, "a" * 64 + "\n"):
                with self.subTest(field=field, value=value):
                    self.args = copy.copy(original)
                    setattr(self.args, field, value)
                    self.reject(lambda: OPERATOR.validate_arguments(self.args))
        self.args = copy.copy(original)
        self.args.mode, self.args.prepared_sha256 = "deploy", "f" * 64
        for path in (OPERATOR.ROOT / "prepared.json", OPERATOR.WORK / "run-20260911T123456Z-" / "prepared.json",
                     OPERATOR.ROOT / ("run-20260911T123456Z-" + "1" * 24) / "passed.json"):
            self.args.prepared = path
            self.reject(lambda: OPERATOR.validate_arguments(self.args))

    def test_control_json_preserves_exact_values_and_refuses_duplicate_fields(self):
        raw = b'{"revision":9007199254740993,"nested":{"nullable":null,"list":[1,true,"same"]}}'
        value = OPERATOR.decode(raw)
        self.assertEqual(value["revision"], 9007199254740993)
        self.assertIsNone(value["nested"]["nullable"])
        for malformed in (b'{"phase":"prepared","phase":"deployed"}',
                          b'{"files":{"go.mod":"a","go.mod":"b"}}', b"{", b"\xff"):
            self.reject(lambda malformed=malformed: OPERATOR.decode(malformed))

    def test_artifact_member_paths_refuse_traversal_absolute_controls_and_windows_paths(self):
        for valid in ("index.html", "assets/main.js", "internal/database/migrations/0025_music_artists.sql"):
            self.assertEqual(OPERATOR.safe_relative(valid), valid)
        for name in ("", "/etc/passwd", "../runtime.env", "assets/../../runtime.env", "assets//main.js",
                     "./index.html", "assets/./main.js", "assets/..", "assets\\main.js", "asset\x00.js",
                     "asset\n.js", "asset\x7f.js"):
            self.reject(lambda name=name: OPERATOR.safe_relative(name))

    def test_path_inspection_refuses_symlink_in_any_ancestor(self):
        path = OPERATOR.WORK / "candidate-16/goby"
        for link in (path, path.parent, OPERATOR.WORK):
            with self.subTest(link=str(link)), patch.object(Path, "lstat", lambda current:
                    information(kind=stat.S_IFLNK if current == link else stat.S_IFDIR)):
                self.reject(lambda: OPERATOR.path_info(path))
        self.reject(lambda: OPERATOR.path_info(OPERATOR.WORK / "../escape"))

    def test_private_file_reader_rejects_unsafe_owners_links_modes_and_replacement(self):
        path = self.args.full_report
        for options in ({"uid": 995}, {"gid": 986}, {"links": 2}, {"mode": 0o666},
                        {"kind": stat.S_IFDIR}, {"size": 1025}):
            with self.subTest(options=options), patch.object(OPERATOR, "path_info", return_value=information(**{"size": 3, "mode": 0o600, **options})):
                self.reject(lambda: OPERATOR.read_file(path, limit=1024))
        before, changed = information(size=3, mode=0o600), information(size=3, mode=0o600, inode=101)
        handle = io.BytesIO(b"abc")
        handle.fileno = lambda: 17
        with patch.object(OPERATOR, "path_info", side_effect=[before, changed]), \
                patch.object(OPERATOR.os, "open", return_value=17), \
                patch.object(OPERATOR.os, "fdopen", return_value=handle), \
                patch.object(OPERATOR.os, "fstat", return_value=before):
            self.reject(lambda: OPERATOR.read_file(path))

    def test_source_verification_hashes_every_member_and_retains_the_exact_inventory(self):
        source = self.source_fixture()
        observed = OPERATOR.verify_source(self.args.source, source.digest)
        self.assertEqual(observed["files"], source.files)
        self.assertEqual(observed["manifest_sha256"], source.digest)
        self.assertEqual(observed["identity"], {"path": str(self.args.source), "inode": 70})

    def test_source_verification_refuses_changed_bytes_missing_extra_and_renamed_members(self):
        source = self.source_fixture()
        original = dict(source.members)
        mutations = (
            lambda: source.members.__setitem__("internal/guard/input_099.go", b"changed same source path\n"),
            lambda: source.members.pop("internal/guard/input_099.go"),
            lambda: source.members.__setitem__("unreviewed.go", b"package unreviewed\n"),
            lambda: source.members.__setitem__("renamed.go", source.members.pop("internal/guard/input_099.go")),
        )
        for mutate in mutations:
            source.members = dict(original)
            mutate()
            self.reject(lambda: OPERATOR.verify_source(self.args.source, source.digest))

    def test_source_verification_refuses_unsafe_members_and_manifest_shape(self):
        source = self.source_fixture()
        name = "internal/guard/input_000.go"
        for options in ({"uid": 995}, {"gid": 986}, {"mode": 0o666}, {"links": 2},
                        {"kind": stat.S_IFLNK}, {"kind": stat.S_IFIFO}, {"size": OPERATOR.MAX_FILE + 1}):
            source.overrides = {name: information(**options)}
            self.reject(lambda: OPERATOR.verify_source(self.args.source, source.digest))
        source.overrides = {}
        for document in ({"marker": "foreign", "files": source.files},
                         {"marker": OPERATOR.SOURCE_MARKER, "files": []},
                         {"marker": OPERATOR.SOURCE_MARKER, "files": source.files, "extra": True}):
            source.manifest = OPERATOR.canonical(document)
            source.digest = OPERATOR.sha(source.manifest)
            self.reject(lambda: OPERATOR.verify_source(self.args.source, source.digest))

    def test_rehashed_source_cannot_substitute_old_catalogs_migrations_or_required_inputs(self):
        source = self.source_fixture()
        original = dict(source.members)
        for name in ("internal/backuppg/catalogs/schema-23-postgresql-17.json",
                     "internal/backuppg/catalogs/schema-24-postgresql-17.json",
                     "internal/backuppg/catalogs/schema-25-postgresql-17.json",
                     "internal/database/migrations/0004_guard.sql"):
            source.members = dict(original)
            source.members[name] += b" \n"
            source.refresh_manifest()
            self.reject(lambda: OPERATOR.verify_source(self.args.source, source.digest))
        for name in ("go.mod", "go.sum", "internal/database/migrations/0025_guard.sql"):
            source.members = dict(original)
            del source.members[name]
            source.refresh_manifest()
            self.reject(lambda: OPERATOR.verify_source(self.args.source, source.digest))

    def test_frozen_product_refuses_any_inflight_deployment_tool(self):
        source = self.source_fixture()
        original = dict(source.members)
        for name in OPERATOR.TOOL_FILES:
            source.members = {**original, name: b"unfrozen deployment input\n"}
            source.refresh_manifest()
            self.reject(lambda: OPERATOR.verify_source(self.args.source, source.digest))

    def test_tool_source_accepts_only_exact_product_clone_manifest_and_three_tools(self):
        fixture = self.tool_fixture()
        product = fixture["product"]
        result = OPERATOR.verify_tool_source(self.args.tool_source, fixture["digest"], product.files, product.digest)
        self.assertEqual(set(result["files"]), set(product.files) | {OPERATOR.SOURCE_MANIFEST} | OPERATOR.TOOL_FILES)
        self.assertEqual(result["files"][OPERATOR.SOURCE_MANIFEST], product.digest)

    def test_tool_source_refuses_rehashed_product_changes_manifest_substitution_and_membership_drift(self):
        fixture = self.tool_fixture()
        product, members = fixture["product"], fixture["members"]
        original = dict(members)
        changes = [lambda: members.__setitem__("go.mod", b"module unrelated.invalid\n"),
                   lambda: members.__setitem__(OPERATOR.SOURCE_MANIFEST, b'{"marker":"foreign","files":{}}'),
                   lambda: members.__setitem__("unreviewed-tool.py", b"extra tool\n"),
                   lambda: members.pop("scripts/test-env/migrate-client-schema25.go"),
                   lambda: members.__setitem__("scripts/test-env/deploy-client-schema25.py", b"different executing operator\n")]
        for change in changes:
            members.clear()
            members.update(original)
            change()
            fixture["refresh"]()
            self.reject(lambda: OPERATOR.verify_tool_source(self.args.tool_source, fixture["digest"], product.files, product.digest))
        members.clear()
        members.update(original)
        fixture["refresh"]()
        self.reject(lambda: OPERATOR.verify_tool_source(self.args.tool_source, fixture["digest"], product.files, "0" * 64))
        self.reject(lambda: OPERATOR.verify_tool_source(OPERATOR.WORK / "nested/tool-build-16", fixture["digest"], product.files, product.digest))

    def test_source_manifest_digest_is_checked_before_member_reads(self):
        source = self.source_fixture()
        self.replace(Path, "rglob", Mock(side_effect=AssertionError("An untrusted manifest cannot authorize member reads")))
        self.reject(lambda: OPERATOR.verify_source(self.args.source, "0" * 64))

    def asset_archive(self, entries):
        output = io.BytesIO()
        with tarfile.open(fileobj=output, mode="w:gz") as archive:
            for name, data, kind in entries:
                entry = tarfile.TarInfo(name)
                entry.type = kind
                entry.size = len(data) if kind == tarfile.REGTYPE else 0
                entry.linkname = "../../runtime.env" if kind in (tarfile.SYMTYPE, tarfile.LNKTYPE) else ""
                archive.addfile(entry, io.BytesIO(data) if kind == tarfile.REGTYPE else None)
        return output.getvalue()

    def test_assets_accept_normal_root_directory_without_extracting_files(self):
        raw = self.asset_archive(((".", b"", tarfile.DIRTYPE), ("./assets", b"", tarfile.DIRTYPE),
                                  ("./index.html", b"native index", tarfile.REGTYPE),
                                  ("./assets/main.js", b"entry", tarfile.REGTYPE)))
        self.replace(OPERATOR, "read_file", lambda path, **kwargs: raw)
        self.assertEqual(OPERATOR.read_assets(self.args.assets), {"index.html": b"native index", "assets/main.js": b"entry"})

    def test_assets_refuse_escape_links_duplicate_and_missing_application_index(self):
        base = [("index.html", b"index", tarfile.REGTYPE), ("assets/main.js", b"entry", tarfile.REGTYPE)]
        for malicious in (("../runtime.env", b"secret", tarfile.REGTYPE), ("/etc/passwd", b"secret", tarfile.REGTYPE),
                          ("assets/link", b"", tarfile.SYMTYPE), ("assets/hardlink", b"", tarfile.LNKTYPE),
                          ("assets/pipe", b"", tarfile.FIFOTYPE), ("index.html", b"duplicate", tarfile.REGTYPE)):
            raw = self.asset_archive([*base, malicious])
            with self.subTest(member=malicious[0]), patch.object(OPERATOR, "read_file", return_value=raw):
                self.reject(lambda: OPERATOR.read_assets(self.args.assets))
        raw = self.asset_archive(base[1:])
        with patch.object(OPERATOR, "read_file", return_value=raw):
            self.reject(lambda: OPERATOR.read_assets(self.args.assets))

    def test_inputs_bind_full_source_manifest_binary_helper_assets_and_completed_report(self):
        self.input_fixture()
        inputs = OPERATOR.load_inputs(self.args)
        self.assertEqual(inputs["source_manifest_sha256"], self.args.manifest_sha256)
        self.assertEqual(inputs["candidate"]["sha256"], self.args.candidate_sha256)
        self.assertEqual(inputs["helper"]["sha256"], self.args.helper_sha256)
        self.assertEqual(inputs["full_report"]["sha256"], self.args.full_report_sha256)
        self.assertEqual(inputs["asset_members"], {"index.html": OPERATOR.sha(b"index"), "assets/main.js": OPERATOR.sha(b"entry")})

    def test_inputs_refuse_artifact_drift_and_unbound_old_report_summaries(self):
        report, facts = self.input_fixture()
        for path in (self.args.candidate, self.args.helper, self.args.assets):
            old = facts[path]["sha256"]
            facts[path]["sha256"] = "0" * 64
            self.reject(lambda: OPERATOR.load_inputs(self.args))
            facts[path]["sha256"] = old
        self.args.full_report_sha256 = "0" * 64
        self.reject(lambda: OPERATOR.load_inputs(self.args))
        original = copy.deepcopy(report)
        for field, value in (("status", "failed"), ("mode", "targeted"), ("schema", 24),
                             ("source", str(OPERATOR.WORK / "source-attempt-15")),
                             ("source_manifest_sha256", "0" * 64), ("unit_exit", 1)):
            report.clear()
            report.update(copy.deepcopy(original))
            report[field] = value
            self.args.full_report_sha256 = OPERATOR.sha(OPERATOR.canonical(report))
            self.reject(lambda: OPERATOR.load_inputs(self.args))
        report.clear()
        report.update(copy.deepcopy(original))
        report["manifest_sha256"] = report.pop("source_manifest_sha256")
        self.args.full_report_sha256 = OPERATOR.sha(OPERATOR.canonical(report))
        self.reject(lambda: OPERATOR.load_inputs(self.args))

    def test_full_report_requires_complete_unique_packages_no_skips_and_all_cleanup_proofs(self):
        report, _ = self.input_fixture()
        original = copy.deepcopy(report)
        changes = [lambda value: value["packages"].__setitem__(0, value["packages"][1]),
                   lambda value: value["packages"].__setitem__(0, "example.invalid/unrelated"),
                   lambda value: value["tests"].__setitem__("skips", 1),
                   lambda value: value["tests"].__setitem__("failures", 1),
                   lambda value: value["tests"].__setitem__("top_level_passes", 0),
                   lambda value: value["binary"].__setitem__("sha256", "0" * 64),
                   lambda value: value.__setitem__("cleanup", {"pretend": True})]
        changes.extend(lambda value, name=name: value["cleanup"].pop(name) for name in FULL_CLEANUP)
        changes.extend(lambda value, name=name: value["cleanup"].__setitem__(name, False) for name in FULL_CLEANUP)
        for change in changes:
            report.clear()
            report.update(copy.deepcopy(original))
            change(report)
            self.args.full_report_sha256 = OPERATOR.sha(OPERATOR.canonical(report))
            self.reject(lambda: OPERATOR.load_inputs(self.args))

    def test_helper_build_report_binds_the_separate_tool_source_and_exact_binary(self):
        self.input_fixture()
        self.args.helper_build_report_sha256 = "0" * 64
        self.reject(lambda: OPERATOR.load_inputs(self.args))
        original = copy.deepcopy(self.helper_report)
        for field, value in (("status", "failed"), ("product_source_manifest_sha256", "0" * 64),
                             ("tool_manifest_sha256", "0" * 64), ("helper_sha256", "0" * 64),
                             ("goos", "windows"), ("goarch", "arm64"), ("cgo_enabled", True)):
            self.helper_report.clear()
            self.helper_report.update(original)
            self.helper_report[field] = value
            self.args.helper_build_report_sha256 = OPERATOR.sha(OPERATOR.canonical(self.helper_report))
            self.reject(lambda: OPERATOR.load_inputs(self.args))

    def test_guard_report_refuses_wrong_digest_failed_or_skipped_tools(self):
        self.input_fixture()
        self.args.guard_report_sha256 = "0" * 64
        self.reject(lambda: OPERATOR.load_inputs(self.args))
        original = copy.deepcopy(self.guard_report)
        for field, value in (("operator_sha256", "0" * 64), ("guard_sha256", "0" * 64),
                             ("status", "failed"), ("skips", 1)):
            self.guard_report.clear()
            self.guard_report.update(copy.deepcopy(original))
            self.guard_report[field] = value
            self.args.guard_report_sha256 = OPERATOR.sha(OPERATOR.canonical(self.guard_report))
            self.reject(lambda: OPERATOR.load_inputs(self.args))

    def test_systemd_properties_require_exact_fields_but_preserve_repeated_environment_files(self):
        raw = b"MainPID=0\nEnvironmentFiles=first (ignore_errors=no)\nEnvironmentFiles=second (ignore_errors=no)\n"
        observed = OPERATOR.systemd_properties(raw, ("MainPID", "EnvironmentFiles"), ("EnvironmentFiles",))
        self.assertEqual(observed["EnvironmentFiles"], ["first (ignore_errors=no)", "second (ignore_errors=no)"])
        for malformed in (raw + b"MainPID=5\n", raw + b"Unrequested=yes\n", b"MainPID=0\n", b"MainPID\n"):
            self.reject(lambda malformed=malformed: OPERATOR.systemd_properties(malformed,
                        ("MainPID", "EnvironmentFiles"), ("EnvironmentFiles",)))

    def test_database_environment_refuses_foreign_cluster_database_role_and_url_options(self):
        url = "postgres://goby_test:synthetic%2Bpassword@127.0.0.1:5432/goby_test?sslmode=disable"
        environment = OPERATOR.database_environment(url, "goby_test", "goby_test")
        self.assertEqual((environment["PGHOST"], environment["PGPORT"], environment["PGDATABASE"], environment["PGUSER"]),
                         ("127.0.0.1", "5432", "goby_test", "goby_test"))
        self.assertIn("default_transaction_read_only=on", environment["PGOPTIONS"])
        for bad in (url.replace("127.0.0.1", "localhost"), url.replace(":5432", ":15432"),
                    url.replace("/goby_test?", "/postgres?"), url.replace("goby_test:", "postgres:"),
                    url + "&options=-csearch_path=foreign", url + "#fragment", url.replace("synthetic%2Bpassword", "")):
            self.reject(lambda bad=bad: OPERATOR.database_environment(bad, "goby_test", "goby_test"))

    def test_cluster_preflight_binds_both_existing_pairs_and_refuses_clients_or_ownership_drift(self):
        state = {"system_identifier": OPERATOR.SYSTEM_IDENTIFIER, "port": 5432, "version": 170011,
                 "data_directory": "/var/lib/postgresql/17/main", "postmaster_start": "2026-09-08T21:22:34Z",
                 "databases": [{**pair, "comment": OPERATOR.RECOVERY_COMMENT, "role_comment": OPERATOR.RECOVERY_COMMENT,
                                "safe": True, "memberships": 0, "sessions": 0} for pair in (OPERATOR.MAIN, OPERATOR.RECOVERY)]}
        self.replace(OPERATOR, "pg", lambda query: OPERATOR.canonical(state))
        self.assertEqual(OPERATOR.cluster_state(), state)
        original = copy.deepcopy(state)
        changes = [lambda value: value.__setitem__("system_identifier", "foreign"),
                   lambda value: value.__setitem__("port", 15432),
                   lambda value: value.__setitem__("data_directory", "/foreign/cluster"),
                   lambda value: value["databases"].pop()]
        for index in (0, 1):
            for field, entry in (("database_oid", 123), ("role_oid", 456), ("safe", False),
                                 ("memberships", 1), ("sessions", 1)):
                changes.append(lambda value, index=index, field=field, entry=entry:
                               value["databases"][index].__setitem__(field, entry))
        changes.append(lambda value: value["databases"][1].__setitem__("comment", "foreign receipt"))
        for change in changes:
            state.clear()
            state.update(copy.deepcopy(original))
            change(state)
            self.reject(OPERATOR.cluster_state)

    def cluster_proof_fixture(self):
        exact_microseconds = 1789077142072645
        started = dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc) + dt.timedelta(microseconds=exact_microseconds)
        cluster = {"system_identifier": OPERATOR.SYSTEM_IDENTIFIER,
                   "postmaster_start": started.isoformat(timespec="microseconds"),
                   "databases": [dict(OPERATOR.MAIN), dict(OPERATOR.RECOVERY)]}
        process = {"pid": 2119, "directory": "/var/lib/postgresql/17/main", "seconds": "1789077141",
                   "port": "5432", "ticks": "90000", "boot": "11111111-2222-4333-8444-555555555555"}
        direct = {"system_identifier": OPERATOR.SYSTEM_IDENTIFIER, "postmaster_start_microseconds": exact_microseconds}
        state = {"run": OPERATOR.ROOT / ("run-20260911T123456Z-" + "1" * 24),
                 "inputs": {"source_manifest_sha256": "a" * 64, "tool_source": {"manifest_sha256": "f" * 64},
                            "candidate": {"sha256": "b" * 64}, "helper": {"sha256": "e" * 64}}}

        def reset():
            state.update({"clusters": [copy.deepcopy(cluster), copy.deepcopy(cluster)],
                          "processes": [dict(process), dict(process)], "direct": dict(direct),
                          "cluster_reads": 0, "pid_reads": 0, "os_index": -1, "writes": [], "events": []})

        def observed_cluster(allow_snapshot=False):
            index = state["cluster_reads"]
            self.assertLess(index, 2, "The proof exceeded its two cluster observations")
            state["cluster_reads"] += 1
            state["events"].append(("cluster", index))
            return copy.deepcopy(state["clusters"][index])

        def read_text(path, **kwargs):
            if path == Path("/var/lib/postgresql/17/main/postmaster.pid"):
                index = state["pid_reads"]
                self.assertLess(index, 2, "The proof exceeded its two PID-file observations")
                state["pid_reads"] += 1
                state["os_index"] = index
                value = state["processes"][index]
                state["events"].append(("pid-file", index))
                return "\n".join((str(value["pid"]), value["directory"], value["seconds"], value["port"])) + "\n"
            self.assertIn(state["os_index"], (0, 1))
            value = state["processes"][state["os_index"]]
            if path == Path("/proc/sys/kernel/random/boot_id"):
                return value["boot"] + "\n"
            self.assertEqual(path, Path("/proc") / str(value["pid"]) / "stat")
            return str(value["pid"]) + " (postgres) " + " ".join(["S", *(["0"] * 18), value["ticks"]])

        def sql(query):
            self.assertTrue(query.startswith("BEGIN READ ONLY;"))
            state["events"].append(("sql", "exact-microseconds"))
            return OPERATOR.canonical(state["direct"])

        def publish(path, raw):
            state["writes"].append((path, raw))
            state["events"].append(("write", path))

        self.replace(OPERATOR, "cluster_state", observed_cluster)
        self.replace(Path, "read_text", read_text)
        self.replace(OPERATOR, "pg", sql)
        self.replace(OPERATOR, "write_exclusive", publish)
        state["reset"] = reset
        reset()
        return state

    def test_cluster_proof_preserves_exact_sql_microseconds_despite_independent_pid_file_second(self):
        state = self.cluster_proof_fixture()
        path, digest = OPERATOR.cluster_proof(state["run"], state["inputs"], OPERATOR.MAIN, "guard")
        self.assertEqual(len(state["writes"]), 1)
        written_path, raw = state["writes"][0]
        proof = OPERATOR.decode(raw)
        self.assertEqual(path, written_path)
        self.assertEqual(digest, OPERATOR.sha(raw))
        self.assertIs(type(proof["postmaster_start_microseconds"]), int)
        self.assertEqual(proof["postmaster_start_microseconds"], 1789077142072645)
        self.assertEqual(proof["postmaster_start_microseconds"] // 1000000 - int(state["processes"][0]["seconds"]), 1)
        self.assertEqual(proof["postmaster_pid"], 2119)
        self.assertEqual(proof["postmaster_start_ticks"], "90000")
        self.assertEqual((state["cluster_reads"], state["pid_reads"]), (2, 2))
        self.assertEqual(state["events"][-2:], [("cluster", 1), ("write", path)])

    def test_cluster_proof_refuses_sql_microsecond_timestamp_or_identity_drift_before_publication(self):
        state = self.cluster_proof_fixture()
        changes = [lambda value: value["direct"].__setitem__("postmaster_start_microseconds", 1789077142072646),
                   lambda value: value["clusters"][1].__setitem__("postmaster_start", "2026-09-12T00:00:00.000000+00:00"),
                   lambda value: value["clusters"][1]["databases"][0].__setitem__("database_oid", 999999),
                   lambda value: value["direct"].__setitem__("system_identifier", "different-cluster")]
        for change in changes:
            state["reset"]()
            change(state)
            self.reject(lambda: OPERATOR.cluster_proof(state["run"], state["inputs"], OPERATOR.MAIN, "guard"))
            self.assertEqual(state["writes"], [])

    def test_cluster_proof_refuses_second_os_observation_drift_without_writing_proof(self):
        state = self.cluster_proof_fixture()
        for field, changed in (("pid", 2120), ("directory", "/foreign/postgresql/main"), ("port", "5433"),
                               ("seconds", "1789077142"), ("boot", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"), ("ticks", "90001")):
            with self.subTest(field=field):
                state["reset"]()
                state["processes"][1][field] = changed
                self.reject(lambda: OPERATOR.cluster_proof(state["run"], state["inputs"], OPERATOR.MAIN, "guard"))
                self.assertEqual(state["writes"], [])

    def rehearsal_fixture(self):
        run = OPERATOR.ROOT / ("run-20260911T123456Z-" + "1" * 24)
        name = "goby_upgrade_m3e_" + "2" * 24
        target = {"database": name, "database_oid": 810002, "role": name, "role_oid": 810001,
                  "owner_comment": OPERATOR.MARKER + ":" + run.name}
        receipt = {"marker": OPERATOR.MARKER, "phase": "created", "run_id": run.name,
                   "postmaster_start": "2026-09-08T21:22:34Z", "target": target}
        observed = {"system_identifier": OPERATOR.SYSTEM_IDENTIFIER, "postmaster_start": receipt["postmaster_start"],
                    "database": {"name": name, "oid": target["database_oid"], "owner": name,
                                 "owner_oid": target["role_oid"], "comment": target["owner_comment"], "clients": 0, "prepared": 0},
                    "role": {"name": name, "oid": target["role_oid"], "comment": target["owner_comment"],
                             "safe": True, "memberships": 0, "outside_dependencies": 0}}
        return run, receipt, observed

    def test_rehearsal_validation_refuses_same_name_identity_drift_clients_and_dependencies(self):
        _, receipt, observed = self.rehearsal_fixture()
        OPERATOR.validate_rehearsal(receipt, observed)
        changes = [lambda value: value.__setitem__("system_identifier", "different cluster"),
                   lambda value: value.__setitem__("postmaster_start", "2026-09-11T00:00:00Z"),
                   lambda value: value.__setitem__("database", None), lambda value: value.__setitem__("role", None)]
        for key, field, value in (("database", "oid", 999999), ("database", "owner_oid", 999999),
                ("database", "owner", "foreign"), ("database", "comment", "foreign"), ("database", "clients", 1),
                ("database", "prepared", 1), ("role", "oid", 999999), ("role", "safe", False),
                ("role", "comment", "foreign"), ("role", "memberships", 1), ("role", "outside_dependencies", 1)):
            changes.append(lambda row, key=key, field=field, value=value: row[key].__setitem__(field, value))
        for change in changes:
            changed = copy.deepcopy(observed)
            change(changed)
            self.reject(lambda: OPERATOR.validate_rehearsal(receipt, changed))

    def cleanup_fixture(self):
        run, receipt, observed = self.rehearsal_fixture()
        after_database = {**copy.deepcopy(observed), "database": None}
        after_role = {**copy.deepcopy(after_database), "role": None}
        state = {"run": run, "receipt": receipt, "checked": "b" * 64, "events": [],
                 "observations": [copy.deepcopy(observed), copy.deepcopy(observed), after_database, after_role],
                 "documents": {run / "rehearsal-created.json": OPERATOR.canonical(receipt)}}

        def observe(name):
            self.assertEqual(name, receipt["target"]["database"])
            self.assertTrue(state["observations"], "The cleanup made an unplanned observation")
            state["events"].append(("observe", name))
            return state["observations"].pop(0)

        def helper(*args, **kwargs):
            self.assertEqual(args[2], receipt["target"])
            self.assertEqual(args[4:6], ("inspect", "rehearsal-before-cleanup"))
            self.assertEqual(kwargs["expected_schema"], 25)
            state["events"].append(("inspect", state["checked"]))
            return {"state_sha256": state["checked"]}

        def publish(path, raw, **kwargs):
            if path in state["documents"]:
                raise FileExistsError("Synthetic exclusive publication conflict")
            state["events"].append(("write", path.name))
            state["documents"][path] = raw

        def execute(sql, **kwargs):
            state["events"].append(("sql", sql))
            allowed = {'DROP DATABASE "' + receipt["target"]["database"] + '";',
                       'DROP ROLE "' + receipt["target"]["role"] + '";'}
            self.assertIn(sql, allowed, "Cleanup reached a protected database, forced removal, or session termination")
            if state.get("drop_failure"):
                raise OPERATOR.Failure("Synthetic restricted DROP refusal")
            return b""

        self.replace(OPERATOR, "read_file", lambda path, **kwargs: state["documents"][path])
        self.replace(OPERATOR, "invoke_helper", helper)
        self.replace(OPERATOR, "observe_rehearsal", observe)
        self.replace(OPERATOR, "write_exclusive", publish)
        self.replace(OPERATOR, "pg", execute)
        return state

    def run_cleanup(self, state):
        OPERATOR.cleanup_rehearsal(state["run"], {}, state["receipt"], "synthetic-private-url", {"state_sha256": "b" * 64})

    def test_cleanup_rechecks_identity_and_removes_only_the_receipted_rehearsal(self):
        state = self.cleanup_fixture()
        self.run_cleanup(state)
        sql = [value for kind, value in state["events"] if kind == "sql"]
        name = state["receipt"]["target"]["database"]
        self.assertEqual(sql, ['DROP DATABASE "' + name + '";', 'DROP ROLE "' + name + '";'])
        self.assertIn(state["run"] / "rehearsal-created.json", state["documents"])
        proof = OPERATOR.decode(state["documents"][state["run"] / "rehearsal-disposed.json"])
        self.assertIs(proof["evidence_retained"], True)
        kinds = [kind for kind, _ in state["events"]]
        self.assertEqual(kinds[:5], ["inspect", "observe", "write", "observe", "sql"])

    def test_cleanup_refuses_unknown_state_before_any_drop(self):
        state = self.cleanup_fixture()
        state["checked"] = "c" * 64
        self.reject(lambda: self.run_cleanup(state))
        self.assertFalse(any(kind == "sql" for kind, _ in state["events"]))
        self.assertNotIn(state["run"] / "rehearsal-disposed.json", state["documents"])

    def test_cleanup_refuses_late_oid_owner_comment_clients_and_dependency_drift(self):
        state = self.cleanup_fixture()
        original = copy.deepcopy(state["observations"])
        for container, field, value in (("database", "oid", 777), ("database", "owner_oid", 778),
                ("database", "comment", "different run"), ("database", "clients", 1),
                ("role", "oid", 779), ("role", "outside_dependencies", 1)):
            state["events"].clear()
            state["observations"] = copy.deepcopy(original)
            state["observations"][1][container][field] = value
            state["documents"] = {state["run"] / "rehearsal-created.json": OPERATOR.canonical(state["receipt"])}
            self.reject(lambda: self.run_cleanup(state))
            self.assertFalse(any(kind == "sql" for kind, _ in state["events"]))
            self.assertNotIn(state["run"] / "rehearsal-disposed.json", state["documents"])

    def test_cleanup_refuses_cross_run_receipts_and_protected_oids(self):
        state = self.cleanup_fixture()
        original_receipt, original_observed = copy.deepcopy(state["receipt"]), copy.deepcopy(state["observations"])
        cases = [("run", None), ("database_oid", OPERATOR.MAIN["database_oid"]),
                 ("database_oid", OPERATOR.RECOVERY["database_oid"]),
                 ("role_oid", OPERATOR.MAIN["role_oid"]), ("role_oid", OPERATOR.RECOVERY["role_oid"])]
        for field, value in cases:
            state["receipt"].clear()
            state["receipt"].update(copy.deepcopy(original_receipt))
            state["observations"] = copy.deepcopy(original_observed)
            state["events"].clear()
            if field == "run":
                state["receipt"]["run_id"] = "run-20260911T123456Z-" + "9" * 24
                state["receipt"]["target"]["owner_comment"] = OPERATOR.MARKER + ":" + state["receipt"]["run_id"]
                for row in state["observations"]:
                    for kind in ("database", "role"):
                        if row[kind] is not None:
                            row[kind]["comment"] = state["receipt"]["target"]["owner_comment"]
            else:
                state["receipt"]["target"][field] = value
                for row in state["observations"]:
                    if field == "database_oid" and row["database"] is not None:
                        row["database"]["oid"] = value
                    if field == "role_oid":
                        if row["database"] is not None:
                            row["database"]["owner_oid"] = value
                        if row["role"] is not None:
                            row["role"]["oid"] = value
            state["documents"] = {state["run"] / "rehearsal-created.json": OPERATOR.canonical(state["receipt"])}
            self.reject(lambda: self.run_cleanup(state))
            self.assertFalse(any(kind == "sql" for kind, _ in state["events"]))

    def test_cleanup_failure_preserves_receipts_and_never_reports_disposal(self):
        state = self.cleanup_fixture()
        state["drop_failure"] = True
        self.reject(lambda: self.run_cleanup(state))
        self.assertIn(state["run"] / "rehearsal-created.json", state["documents"])
        self.assertIn(state["run"] / "rehearsal-disposal-intent.json", state["documents"])
        self.assertNotIn(state["run"] / "rehearsal-disposed.json", state["documents"])
        self.assertEqual(len([event for event in state["events"] if event[0] == "sql"]), 1)

    def test_rehearsal_creation_refuses_preexisting_objects_before_any_mutation(self):
        run, _, observed = self.rehearsal_fixture()
        self.replace(OPERATOR.secrets, "token_hex", lambda size: "2" * (size * 2))
        self.replace(OPERATOR, "observe_rehearsal", lambda name: observed)
        self.replace(OPERATOR, "write_exclusive", Mock(side_effect=AssertionError("An existing name cannot authorize a receipt")))
        self.replace(OPERATOR, "pg", Mock(side_effect=AssertionError("An existing rehearsal name cannot authorize DDL")))
        self.reject(lambda: OPERATOR.create_rehearsal(run))

    def material_directory_fixture(self):
        run, _, _ = self.rehearsal_fixture()
        directories = {path: information(kind=stat.S_IFDIR, mode=0o700)
                       for path in (run, *run.parents)}
        state = {"run": run, "directories": directories, "files": {}, "mkdir_calls": [], "events": []}

        def lstat(path):
            if path not in directories:
                raise FileNotFoundError(str(path))
            return directories[path]

        def mkdir(path, mode=0o777, parents=False, exist_ok=False):
            state["mkdir_calls"].append((path, mode, parents, exist_ok))
            if path in directories:
                if exist_ok and stat.S_ISDIR(directories[path].st_mode):
                    return
                raise FileExistsError(str(path))
            if path.parent not in directories:
                if not parents:
                    raise FileNotFoundError(str(path.parent))
                # Path.mkdir(parents=True) does not forward the leaf mode.
                # With umask 0022, those implicit intermediates become 0755.
                mkdir(path.parent, parents=True, exist_ok=True)
            directories[path] = information(kind=stat.S_IFDIR, mode=mode & ~0o022)
            state["events"].append(("mkdir", path))

        def write(path, raw, **kwargs):
            OPERATOR.directory(path.parent)
            if path in state["files"]:
                raise FileExistsError(str(path))
            state["files"][path] = raw
            state["events"].append(("write", path))

        self.replace(Path, "lstat", lstat)
        self.replace(Path, "mkdir", mkdir)
        self.replace(OPERATOR, "read_file", lambda path, **kwargs: ("Synthetic source: " + str(path)).encode())
        self.replace(OPERATOR, "write_exclusive", write)
        self.replace(OPERATOR, "sync_directory", lambda path: state["events"].append(("sync", path)))
        return state

    def test_save_materials_creates_every_parent_private_under_umask_0022_before_index_copy(self):
        state = self.material_directory_fixture()
        before = {"assets": {"files": {"assets/main.js": {}, "index.html": {}}},
                  "stores": {"trees": {str(path): {"files": {}} for path in OPERATOR.STORES},
                             "pairing": {"files": {}}}, "diagnostics": {"files": {}}}
        saved = OPERATOR.save_materials(state["run"], before)
        root = state["run"] / "materials"
        script, index = root / "admin/assets/main.js", root / "admin/index.html"
        writes = [path for kind, path in state["events"] if kind == "write"]
        self.assertLess(writes.index(script), writes.index(index))
        self.assertEqual(saved["admin/assets/main.js"], OPERATOR.sha(state["files"][script]))
        self.assertEqual(saved["admin/index.html"], OPERATOR.sha(state["files"][index]))
        for path in (root, root / "admin", root / "admin/assets"):
            self.assertEqual(stat.S_IMODE(state["directories"][path].st_mode), 0o700)
        self.assertTrue(all(mode == 0o700 and parents is False and exist_ok is False
                            for path, mode, parents, exist_ok in state["mkdir_calls"] if path.is_relative_to(root)))
        for path in (root / "admin", root / "admin/assets"):
            position = state["events"].index(("mkdir", path))
            self.assertEqual(state["events"][position + 1], ("sync", path.parent))

    def test_material_directory_refuses_existing_permissive_symlink_or_foreign_parent_without_repair(self):
        state = self.material_directory_fixture()
        root = state["run"] / "materials"
        state["directories"][root] = information(kind=stat.S_IFDIR, mode=0o700)
        parent = root / "admin"
        for changed in ({"mode": 0o755}, {"kind": stat.S_IFLNK}, {"uid": 995}, {"gid": 986}):
            state["directories"][parent] = information(**{"kind": stat.S_IFDIR, "mode": 0o700, **changed})
            original = {path: vars(info).copy() for path, info in state["directories"].items()}
            state["mkdir_calls"].clear()
            state["events"].clear()
            self.reject(lambda: OPERATOR.ensure_material_directory(root, parent / "assets"))
            self.assertEqual({path: vars(info).copy() for path, info in state["directories"].items()}, original)
            self.assertEqual(state["mkdir_calls"], [])
            self.assertEqual(state["events"], [])
            # chmod/chown remain denied by EffectFence, even after rejection.

    def pipeline_fixture(self):
        run, receipt, _ = self.rehearsal_fixture()
        inputs = {"source_manifest_sha256": self.args.manifest_sha256,
                  "candidate": {"path": str(self.args.candidate), "sha256": self.args.candidate_sha256},
                  "helper": {"path": str(self.args.helper), "sha256": self.args.helper_sha256},
                  "tool_source": {"manifest_sha256": self.args.tool_manifest_sha256}, "asset_members": {"index.html": "7" * 64}}
        stores = {"backups": [{"id": "8" * 32, "sha256": "9" * 64, "bytes": 100}],
                  "pairing": {"files": {"password": "retained"}}, "old_operational_backup": {"files": {"old-dump": "retained"}},
                  "trees": {"retained": True}}
        before = {"stores": stores, "assets": {"files": {}}, "service": {"MainPID": "0"}}
        state = {"run": run, "inputs": inputs, "before": before, "events": [], "failure": None,
                 "files": {OPERATOR.OLD_BACKUP / "historical.archive": b"never replace this old archive"},
                 "baseline": {"source_schema_version": 23, "state": {"schema_version": 23}, "state_sha256": "a" * 64}}

        def event(kind, value):
            state["events"].append((kind, value))
            if state["failure"] == value:
                raise OPERATOR.Failure("Synthetic failure at " + str(value))

        def write(path, raw, **kwargs):
            event("write", path.name)
            if path in state["files"]:
                raise FileExistsError("Synthetic exclusive phase publication conflict")
            state["files"][path] = raw

        def create(values):
            self.assertEqual(values, inputs)
            event("phase", "create-run")
            state["files"][run / "OWNER.json"] = OPERATOR.canonical({"marker": OPERATOR.MARKER, "run_id": run.name, "version": 1})
            state["files"][run / "inputs.json"] = OPERATOR.canonical(inputs)
            return run

        def helper(_run, _inputs, target, url, mode, label, **kwargs):
            event("helper", label)
            state["events"].append(("helper-target", (target["database"], mode)))
            result = copy.deepcopy(state["baseline"]) if mode == "inspect" else {
                "rehearsal": target != OPERATOR.MAIN, "target_schema_version": 25, "state_sha256": "b" * 64}
            if label == "main-after-rehearsal" and state.get("source_drift"):
                result["state_sha256"] = "c" * 64
            state["files"][run / (label + ".json")] = OPERATOR.canonical(result)
            return result

        @contextlib.contextmanager
        def snapshot(environment):
            event("phase", "snapshot-open")
            yield "00000001-00000002-1"
            event("phase", "snapshot-close")

        def dump(_run, environment, snapshot_id):
            self.assertEqual(snapshot_id, "00000001-00000002-1")
            event("phase", "dump")
            state["files"][run / "database-schema23.dump"] = b"fresh current schema23 backup"
            return {"sha256": OPERATOR.sha(state["files"][run / "database-schema23.dump"])}

        def rehearse(_run, _inputs):
            event("phase", "rehearse")
            state["files"][run / "rehearsal-created.json"] = OPERATOR.canonical(receipt)
            return receipt, "synthetic-private-url", {"state_sha256": "b" * 64}

        def cleanup(*args):
            event("phase", "cleanup-rehearsal")
            state["files"][run / "rehearsal-disposed.json"] = OPERATOR.canonical({"status": "disposed"})

        def execute(arguments, **kwargs):
            self.assertEqual(list(arguments), ["/usr/bin/systemctl", "start", OPERATOR.SERVICE],
                             "Deployment reached a main restore, old-binary restart, or unrelated command")
            event("command", "start-candidate")
            return b""

        def files(_run):
            return {path.relative_to(run).as_posix(): OPERATOR.sha(raw) for path, raw in state["files"].items()
                    if path.is_relative_to(run) and path.name != "prepared.json"}

        self.replace(OPERATOR, "create_run", create)
        self.replace(OPERATOR, "read_file", lambda path, **kwargs: state["files"][path])
        self.replace(OPERATOR, "write_exclusive", write)
        self.replace(OPERATOR, "directory", lambda path: {"inode": 99})
        self.replace(Path, "exists", lambda path: path in state["files"])
        self.replace(Path, "is_file", lambda path: path in state["files"])
        self.replace(OPERATOR, "protected_state", lambda environment: copy.deepcopy(before))
        self.replace(OPERATOR, "save_materials", lambda *args: event("phase", "save-materials"))
        self.replace(OPERATOR, "exported_snapshot", snapshot)
        self.replace(OPERATOR, "invoke_helper", helper)
        self.replace(OPERATOR, "dump_snapshot", dump)
        self.replace(OPERATOR, "rehearse", rehearse)
        self.replace(OPERATOR, "cleanup_rehearsal", cleanup)
        self.replace(OPERATOR, "prepared_files", files)
        self.replace(OPERATOR, "store_state", lambda: copy.deepcopy(stores))
        self.replace(OPERATOR, "install_candidate", lambda *args: event("phase", "install"))
        self.replace(OPERATOR, "service_state", lambda **kwargs: {"MainPID": "123" if kwargs.get("active") else "0"})
        self.replace(OPERATOR, "quiescent", lambda environment: {})
        self.replace(OPERATOR, "command", execute)
        self.replace(OPERATOR, "native_smoke", lambda *args: event("phase", "native-smoke"))
        self.replace(OPERATOR, "wait_ready", lambda expected: {"MainPID": "123"})
        return state

    def prepare_pipeline(self, state):
        result = OPERATOR.prepare(self.args, state["inputs"], "synthetic-private-url", {})
        self.args.mode = "deploy"
        self.args.prepared = Path(result["receipt"])
        self.args.prepared_sha256 = result["sha256"]
        return result

    def test_prepare_publishes_proof_only_after_fresh_backup_rehearsal_and_source_compare(self):
        state = self.pipeline_fixture()
        self.prepare_pipeline(state)
        labels = [value for kind, value in state["events"] if kind != "helper-target"]
        self.assertLess(labels.index("dump"), labels.index("backup-complete.json"))
        self.assertLess(labels.index("backup-complete.json"), labels.index("rehearse"))
        self.assertLess(labels.index("cleanup-rehearsal"), labels.index("main-after-rehearsal"))
        self.assertLess(labels.index("main-after-rehearsal"), labels.index("prepared.json"))
        self.assertFalse(any(kind == "command" for kind, _ in state["events"]))
        self.assertEqual(state["files"][OPERATOR.OLD_BACKUP / "historical.archive"], b"never replace this old archive")

    def test_prepare_failure_keeps_fresh_evidence_and_never_installs_or_starts(self):
        state = self.pipeline_fixture()
        state["failure"] = "rehearse"
        self.reject(lambda: self.prepare_pipeline(state))
        self.assertIn(state["run"] / "database-schema23.dump", state["files"])
        self.assertIn(state["run"] / "backup-complete.json", state["files"])
        self.assertNotIn(state["run"] / "prepared.json", state["files"])
        terminal = OPERATOR.decode(state["files"][state["run"] / "terminal.json"])
        self.assertEqual(terminal["status"], "failed")
        self.assertIs(terminal["evidence_retained"], True)
        self.assertNotIn(("phase", "install"), state["events"])
        self.assertFalse(any(kind == "command" for kind, _ in state["events"]))

    def test_prepare_refuses_post_backup_main_data_drift(self):
        state = self.pipeline_fixture()
        state["source_drift"] = True
        self.reject(lambda: self.prepare_pipeline(state))
        self.assertNotIn(state["run"] / "prepared.json", state["files"])
        self.assertTrue(all(mode == "inspect" for kind, value in state["events"] if kind == "helper-target"
                            for database, mode in (value,) if database == OPERATOR.MAIN["database"]))

    def test_prepared_receipt_refuses_changed_inputs_phase_bytes_and_added_artifacts(self):
        state = self.pipeline_fixture()
        self.prepare_pipeline(state)
        run = state["run"]
        OPERATOR.verify_prepared(self.args, state["inputs"])
        state["files"][run / "unreviewed.json"] = b"unexpected artifact"
        self.reject(lambda: OPERATOR.verify_prepared(self.args, state["inputs"]))
        del state["files"][run / "unreviewed.json"]
        changed_inputs = {**state["inputs"], "source_manifest_sha256": "0" * 64}
        self.reject(lambda: OPERATOR.verify_prepared(self.args, changed_inputs))
        saved = state["files"].pop(run / "rehearsal-disposed.json")
        self.reject(lambda: OPERATOR.verify_prepared(self.args, state["inputs"]))
        state["files"][run / "rehearsal-disposed.json"] = saved
        state["files"][run / "terminal.json"] = OPERATOR.canonical({"status": "failed"})
        self.reject(lambda: OPERATOR.verify_prepared(self.args, state["inputs"]))

    def test_deploy_requires_durable_intents_before_migration_and_service_start(self):
        state = self.pipeline_fixture()
        self.prepare_pipeline(state)
        state["events"].clear()
        result = OPERATOR.deploy(self.args, state["inputs"], "synthetic-private-url", {})
        self.assertEqual(result["status"], "passed")
        labels = [value for kind, value in state["events"] if kind != "helper-target"]
        self.assertLess(labels.index("deployment-intent.json"), labels.index("main-migrated"))
        self.assertLess(labels.index("main-migrated"), labels.index("install"))
        self.assertLess(labels.index("start-intent.json"), labels.index("start-candidate"))
        self.assertLess(labels.index("native-smoke"), labels.index("terminal.json"))
        terminal = OPERATOR.decode(state["files"][state["run"] / "terminal.json"])
        self.assertIs(terminal["main_database_restored"], False)
        self.assertIs(terminal["old_binary_rollback"], False)

    def test_deploy_uncertain_migration_keeps_evidence_without_old_binary_fallback(self):
        state = self.pipeline_fixture()
        self.prepare_pipeline(state)
        state["events"].clear()
        state["failure"] = "main-migrated"
        self.reject(lambda: OPERATOR.deploy(self.args, state["inputs"], "synthetic-private-url", {}))
        self.assertNotIn(("phase", "install"), state["events"])
        self.assertFalse(any(kind == "command" for kind, _ in state["events"]))
        terminal = OPERATOR.decode(state["files"][state["run"] / "terminal.json"])
        self.assertIs(terminal["forward_repair_required"], True)
        self.assertIs(terminal["main_database_restored"], False)
        self.assertIs(terminal["old_binary_rollback"], False)
        self.assertIn(state["run"] / "database-schema23.dump", state["files"])

    def test_deploy_smoke_failure_never_restores_old_data_or_retries_start(self):
        state = self.pipeline_fixture()
        self.prepare_pipeline(state)
        state["events"].clear()
        state["failure"] = "native-smoke"
        self.reject(lambda: OPERATOR.deploy(self.args, state["inputs"], "synthetic-private-url", {}))
        self.assertEqual([event for event in state["events"] if event[0] == "command"], [("command", "start-candidate")])
        self.assertEqual([value for kind, value in state["events"] if kind == "helper-target"],
                         [(OPERATOR.MAIN["database"], "inspect"), (OPERATOR.MAIN["database"], "migrate")])
        self.assertEqual(state["files"][OPERATOR.OLD_BACKUP / "historical.archive"], b"never replace this old archive")
        terminal = OPERATOR.decode(state["files"][state["run"] / "terminal.json"])
        self.assertIs(terminal["evidence_retained"], True)

    def test_failed_intent_publication_never_crosses_its_mutation_boundary(self):
        state = self.pipeline_fixture()
        self.prepare_pipeline(state)
        state["events"].clear()
        state["failure"] = "deployment-intent.json"
        self.reject(lambda: OPERATOR.deploy(self.args, state["inputs"], "synthetic-private-url", {}))
        self.assertNotIn(("helper", "main-migrated"), state["events"])
        self.assertNotIn(("phase", "install"), state["events"])
        self.assertFalse(any(kind == "command" for kind, _ in state["events"]))

    def helper_fixture(self):
        run, _, _ = self.rehearsal_fixture()
        inputs = {"source_manifest_sha256": self.args.manifest_sha256,
                  "tool_source": {"manifest_sha256": self.args.tool_manifest_sha256},
                  "candidate": {"sha256": self.args.candidate_sha256},
                  "helper": {"path": str(self.args.helper), "sha256": self.args.helper_sha256}}
        baseline = OPERATOR.canonical({"state_sha256": "a" * 64,
                                       "exact_historical_value": 9007199254740993})
        report = {"schema": "goby-client-schema25-migration", "version": 1, "mode": "migrate", "status": "committed",
                  "state_sha256": "b" * 64, "run_id": run.name, "source_manifest_sha256": self.args.manifest_sha256,
                  "tool_manifest_sha256": self.args.tool_manifest_sha256, "candidate_sha256": self.args.candidate_sha256,
                  "helper_sha256": self.args.helper_sha256, "cluster_proof_sha256": "c" * 64,
                  "target": dict(OPERATOR.MAIN), "input_baseline_sha256": OPERATOR.sha(baseline),
                  "before_state_sha256": "a" * 64, "preserved_state_sha256": "a" * 64}
        state = {"run": run, "inputs": inputs, "report": report, "commands": [], "baseline": baseline}
        self.replace(OPERATOR, "cluster_proof", lambda *args: (run / "main-cluster.json", "c" * 64))
        self.replace(OPERATOR, "cluster_state", lambda *args: {"system_identifier": OPERATOR.SYSTEM_IDENTIFIER})
        self.replace(OPERATOR, "read_file", lambda path, **kwargs:
                     baseline if path == run / "baseline.json" else OPERATOR.canonical(report))

        def execute(arguments, **kwargs):
            state["commands"].append([str(argument) for argument in arguments])
            self.assertEqual(kwargs["environment"]["GOBY_DATABASE_URL"], "synthetic-private-url")
            return b""

        self.replace(OPERATOR, "command", execute)
        return state

    def invoke_migration_helper(self, state):
        return OPERATOR.invoke_helper(state["run"], state["inputs"], OPERATOR.MAIN, "synthetic-private-url",
                                      "migrate", "main-migrated", baseline=state["run"] / "baseline.json")

    def test_helper_invocation_binds_baseline_bytes_and_exact_source_tool_database_identity(self):
        state = self.helper_fixture()
        result = self.invoke_migration_helper(state)
        self.assertEqual(result["before_state_sha256"], result["preserved_state_sha256"])
        self.assertEqual(len(state["commands"]), 1)
        arguments = state["commands"][0]
        self.assertEqual(arguments[:2], [str(self.args.helper), "migrate"])
        for option, expected in (("--baseline-sha256", OPERATOR.sha(state["baseline"])),
                ("--expected-database", OPERATOR.MAIN["database"]),
                ("--expected-database-oid", str(OPERATOR.MAIN["database_oid"])),
                ("--source-manifest-sha256", self.args.manifest_sha256),
                ("--tool-manifest-sha256", self.args.tool_manifest_sha256),
                ("--candidate-sha256", self.args.candidate_sha256),
                ("--expected-helper-sha256", self.args.helper_sha256), ("--run-id", state["run"].name)):
            self.assertEqual(arguments[arguments.index(option) + 1], expected)

    def test_helper_report_refuses_wrong_invocation_and_missing_or_changed_old_state_projection(self):
        state = self.helper_fixture()
        original = copy.deepcopy(state["report"])
        changes = [lambda value: value.__setitem__("run_id", "another-run"),
                   lambda value: value.__setitem__("source_manifest_sha256", "0" * 64),
                   lambda value: value.__setitem__("tool_manifest_sha256", "0" * 64),
                   lambda value: value.__setitem__("candidate_sha256", "0" * 64),
                   lambda value: value.__setitem__("helper_sha256", "0" * 64),
                   lambda value: value.__setitem__("cluster_proof_sha256", "0" * 64),
                   lambda value: value["target"].__setitem__("database_oid", OPERATOR.RECOVERY["database_oid"]),
                   lambda value: value.__setitem__("input_baseline_sha256", "0" * 64),
                   lambda value: value.__setitem__("preserved_state_sha256", "0" * 64),
                   lambda value: (value.pop("before_state_sha256"), value.pop("preserved_state_sha256"))]
        for change in changes:
            state["report"].clear()
            state["report"].update(copy.deepcopy(original))
            change(state["report"])
            self.reject(lambda: self.invoke_migration_helper(state))

    def test_successful_publication_survives_a_later_terminal_output_failure(self):
        state = self.pipeline_fixture()
        self.prepare_pipeline(state)
        state["events"].clear()
        self.replace(OPERATOR, "parser", lambda: types.SimpleNamespace(parse_args=lambda argv: self.args))
        self.replace(OPERATOR, "load_inputs", lambda args: state["inputs"])
        self.replace(OPERATOR, "runtime_policy", lambda: "postgres://goby_test:synthetic@127.0.0.1:5432/goby_test")
        self.replace(OPERATOR.os, "geteuid", lambda: 0)
        self.replace(OPERATOR.sys, "platform", "linux")

        @contextlib.contextmanager
        def lock(create):
            self.assertIs(create, False)
            yield

        self.replace(OPERATOR, "operator_lock", lock)
        printer = self.replace(builtins, "print", Mock(side_effect=[BrokenPipeError("Synthetic terminal closure"), None]))
        self.assertEqual(OPERATOR.main([]), 1)
        self.assertEqual(printer.call_count, 2)
        terminal = OPERATOR.decode(state["files"][state["run"] / "terminal.json"])
        self.assertEqual(terminal["status"], "passed")
        self.assertEqual([event for event in state["events"] if event[0] == "command"], [("command", "start-candidate")])
        self.assertEqual([event for event in state["events"] if event == ("write", "terminal.json")], [("write", "terminal.json")])


def main():
    global OPERATOR
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "client-schema25-deployment-guards", "status": "blocked",
                          "reason": "Authorized root SSH and the new operator source path are required."}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    if source.name != "deploy-client-schema25.py":
        raise SystemExit("Only the new schema25 deployment operator may be loaded.")
    operator_bytes, suite_bytes = source.read_bytes(), Path(__file__).read_bytes()
    SOURCE_LINES[str(source)] = operator_bytes.decode().splitlines(keepends=True)
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    OPERATOR = types.ModuleType("memory_client_schema25_deployment")
    OPERATOR.__file__ = str(source)
    sys.addaudithook(deny_audited_effect)
    try:
        with EffectFence():
            exec(compile(operator_bytes, str(source), "exec"), OPERATOR.__dict__)
    except BaseException as error:
        print(json.dumps({"suite": "client-schema25-deployment-guards", "status": "failed", "tests": 0,
                          "error": "Fenced import failed: " + type(error).__name__,
                          "operator_sha256": hashlib.sha256(operator_bytes).hexdigest(),
                          "guard_sha256": hashlib.sha256(suite_bytes).hexdigest()}))
        raise SystemExit(1)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(DeploymentGuardTests)
    names = sorted(test._testMethodName for test in suite)
    result = unittest.TestResult()
    suite.run(result)
    failures = [*result.failures, *result.errors]
    passed = result.wasSuccessful() and not result.skipped
    print(json.dumps({"suite": "client-schema25-deployment-guards", "status": "passed" if passed else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "skips": len(result.skipped), "cases": names,
                      "failure_summaries": [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]}
                                            for test, detail in failures[:8]],
                      "operator_sha256": hashlib.sha256(operator_bytes).hexdigest(),
                      "guard_sha256": hashlib.sha256(suite_bytes).hexdigest(), "fixtures": "synthetic-memory-only",
                      "real_database_restore_acceptance": False, "real_deployment_acceptance": False}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
