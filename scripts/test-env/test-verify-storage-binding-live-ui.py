#!/usr/bin/env python3
"""Exercise the live UI operator through effect-fenced memory boundaries.

Run only through authorized root SSH. The operator source is read once before
the fence; no fixture may touch a database, filesystem, process, or network.
"""

from __future__ import annotations

import argparse
import __future__
import base64
import builtins
import contextlib
import copy
import datetime
import hashlib
import http.client
import http.cookies
import importlib.util
import io
import _io
import json
import linecache
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
from unittest.mock import Mock, patch
import urllib.parse

sys.dont_write_bytecode = True
OP = None
SOURCE_LINES = {}
BLOCKED_EFFECTS = []
WORK = Path("/opt/goby-test/exec-work-m3e")
TOOL = WORK / "storage-binding-live-ui-tool-05"
RUN = "20260912_120000_1a2b3c4d5e6f"
SUFFIX = "202609121200001a2b3c4d5e6f"
OUTPUT = WORK / ("storage-binding-live-ui-" + RUN)
RUNTIME = Path("/opt/goby-binding-ui-runtime-" + RUN)
TOOL_FILES = frozenset((
    "verify-storage-binding-live-ui.py", "package.json", "package-lock.json",
    "playwright.config.ts", "e2e/root-binding-live.spec.ts", "accepted-web-report.json", "dependency-files.json",
))


class EffectBlocked(AssertionError):
    """An unpatched boundary tried to leave the memory fixture."""


class EnvironmentFence(dict):
    def __init__(self, denied):
        super().__init__()
        self.denied = denied

    def reject(self, *_args, **_kwargs):
        self.denied()

    __getitem__ = __setitem__ = __delitem__ = __contains__ = __iter__ = reject
    __len__ = get = copy = keys = items = values = update = clear = reject
    pop = popitem = setdefault = reject


class EffectFence(contextlib.ExitStack):
    """Deny effects and reject a fixture even if production catches the denial."""

    def __enter__(self):
        super().__enter__()
        import fcntl
        import pwd

        self.violations = []

        def denial(label):
            def denied(*_args, **_kwargs):
                self.violations.append(label)
                BLOCKED_EFFECTS.append(label)
                raise EffectBlocked("Unexpected external effect: " + label)
            return denied

        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       SOURCE_LINES.get(str(filename), [])))
        original_import = builtins.__import__
        cached_modules = dict(sys.modules)

        def cached_import(name, globals=None, locals=None, fromlist=(), level=0):
            module = cached_modules.get(name)
            if level != 0 or module is None or any(
                item == "*" or item not in vars(module) for item in fromlist or ()
            ):
                denial("uncached import: " + name)()
            return original_import(name, globals, locals, fromlist, level)

        targets = (
            (builtins, ("open",)),
            (io, ("open", "FileIO")),
            (_io, ("open", "open_code", "FileIO")),
            (os, ("open", "close", "fdopen", "fstat", "stat", "lstat", "mkdir", "chmod", "chown",
                  "fchmod", "fchown", "fsync", "replace", "rename", "unlink", "remove", "rmdir", "umask",
                  "kill", "killpg", "system", "popen", "getenv", "getenvb", "putenv", "unsetenv", "getcwd",
                  "chdir", "getuid", "geteuid", "getgid", "getegid", "getgroups", "getpid", "getppid",
                  "readlink", "scandir", "listdir", "read", "write", "pread", "pwrite", "truncate", "ftruncate",
                  "link", "symlink", "mount", "unshare", "fork", "forkpty", "execv", "execve", "execvp",
                  "execvpe", "execl", "execle", "execlp", "execlpe", "posix_spawn", "posix_spawnp", "spawnv",
                  "spawnve", "spawnvp", "spawnvpe", "urandom", "dup", "lseek", "pidfd_open", "_exit")),
            (Path, ("open", "lstat", "stat", "exists", "resolve", "readlink", "read_bytes", "read_text",
                    "write_bytes", "write_text", "mkdir", "iterdir", "rglob", "glob", "unlink", "is_file",
                    "is_dir", "is_symlink", "rename", "replace", "touch", "rmdir", "chmod", "lchmod", "symlink_to", "hardlink_to")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo", "gethostname", "gethostbyname",
                      "gethostbyname_ex", "gethostbyaddr", "getnameinfo")),
            (http.client.HTTPConnection, ("connect", "request", "getresponse")),
            (pwd, ("getpwnam", "getpwuid")),
            (fcntl, ("flock", "ioctl")),
            (signal, ("signal", "pidfd_send_signal")),
            (time, ("sleep",)),
            (secrets, ("token_bytes", "token_hex", "token_urlsafe")),
            (shutil, ("rmtree", "copyfile", "copy", "copy2", "copytree", "move", "get_terminal_size")),
        )
        for owner, names in targets:
            for name in names:
                if hasattr(owner, name):
                    self.enter_context(patch.object(owner, name, denial(owner.__name__ + "." + name)))
        self.enter_context(patch.object(os, "environ", EnvironmentFence(denial("os.environ"))))
        if hasattr(os, "environb"):
            self.enter_context(patch.object(os, "environb", EnvironmentFence(denial("os.environb"))))
        self.enter_context(patch.object(builtins, "__import__", cached_import))
        return self

    def __exit__(self, *args):
        super().__exit__(*args)
        if self.violations:
            raise EffectBlocked("External effects attempted: " + ", ".join(self.violations))


def intent_fixture():
    return {
        "marker": "goby-storage-binding-live-ui-v1", "version": 1, "run_id": RUN, "nonce": "a" * 64,
        "tool": str(TOOL), "output": str(OUTPUT), "runtime": str(RUNTIME),
        "database": "goby_binding_ui_" + SUFFIX, "role": "goby_binding_ui_r_" + SUFFIX,
        "origin": "http://127.0.0.1:18288",
        "controller_unit": "goby-storage-binding-live-ui-controller-20260912-120000-1a2b3c4d5e6f.service",
        "worker_unit": "goby-storage-binding-live-ui-worker-20260912-120000-1a2b3c4d5e6f.service",
        "host_namespace": "mnt:[4026531841]", "history_sha256": "b" * 64,
        "source_closure": {name: (OP.WEB_REPORT_SHA if name == "accepted-web-report.json" else "c" * 64)
                           for name in TOOL_FILES},
    }


def metadata(*, mode=stat.S_IFREG | 0o600, uid=0, gid=0, size=7, inode=91, device=7, nlink=1):
    return types.SimpleNamespace(st_mode=mode, st_uid=uid, st_gid=gid, st_size=size,
                                 st_ino=inode, st_dev=device, st_nlink=nlink, st_mtime_ns=3, st_ctime_ns=4)


class MemoryStream(io.BytesIO):
    def fileno(self):
        return 713


class LiveUIGuards(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())
        self.stdout = self.enterContext(contextlib.redirect_stdout(io.StringIO()))

    def reject(self, function, *args, **kwargs):
        with self.assertRaises(OP.Failure):
            function(*args, **kwargs)

    def test_product_and_scope_pins_are_independent_of_any_intent(self):
        self.assertEqual(OP.MARKER, "goby-storage-binding-live-ui-v1")
        self.assertEqual(OP.TOOL, TOOL)
        self.assertEqual(OP.TOOL_FILES, TOOL_FILES)
        self.assertEqual(OP.PORT, 18288)
        self.assertEqual(OP.ORIGIN, "http://127.0.0.1:18288")
        self.assertEqual(OP.SOURCE, WORK / "source-attempt-54")
        self.assertEqual(OP.BINARY, WORK / "client-backup-run-20260912_053517_9cb0074fc731/tmp/goby-linux-amd64")
        self.assertEqual(OP.BINARY_SHA, "5f88c432d96825d5c3f8c2faccc64be673dab85849670707571c20fddd71594e")
        self.assertEqual(OP.WEB, WORK / "storage-binding-workflow-web-03/dist")
        self.assertEqual(OP.WEB_REPORT_SHA, "2b33e2805afb7422eeb8486db9c4f531cd074a636e4dcddc08c91c8d0808a9d7")
        self.assertEqual(OP.CATALOG_SHA, "8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b")
        self.assertEqual(OP.NODE, WORK / "client-library-changed-source44-tool-01/node")
        self.assertEqual(OP.NODE_SHA, "3517c2df0b2f8cd7f422b4b8450ef81c6889f08eb03e281d6de9079b15e6a327")
        self.assertEqual(OP.NODE_MODULES, Path("/opt/goby-test/inactive-dependencies-m5h/node_modules"))
        self.assertEqual(OP.HISTORY, Path("/opt/goby-test/postgres-workspace-v1/client-backup-pair.json"))
        self.assertEqual(OP.DATA, Path("/var/lib/postgresql/goby-workspace-v1/data"))
        self.assertEqual(OP.SOCKET, Path("/var/lib/postgresql/goby-workspace-v1/socket"))
        self.assertEqual(OP.SOURCE_FILES, (
            "scripts/test-env/prepare-postgres-workspace.py", "internal/backuppg/catalog.go",
            "internal/backuppg/catalogs/schema-28-postgresql-17.json",
        ))

    def test_real_intent_accepts_the_complete_fresh_scope(self):
        value = intent_fixture()
        self.assertIs(OP.validate_intent(value), value)

    def test_real_intent_rejects_missing_extra_and_wrong_document_shapes(self):
        for value in (None, True, 1, "intent", [], [intent_fixture()]):
            with self.subTest(shape=type(value).__name__):
                self.reject(OP.validate_intent, value)
        for field in intent_fixture():
            value = intent_fixture()
            del value[field]
            with self.subTest(missing=field):
                self.reject(OP.validate_intent, value)
        value = intent_fixture()
        value["resume"] = True
        self.reject(OP.validate_intent, value)

    def test_real_intent_rejects_malformed_identity_types_and_truthy_versions(self):
        for field in ("run_id", "nonce", "host_namespace", "history_sha256"):
            for value in (None, True, 7, [], {}, "", "../escape", "A" * 64):
                changed = intent_fixture()
                changed[field] = value
                with self.subTest(field=field, value=value):
                    self.reject(OP.validate_intent, changed)
        for value in (True, "1", 1.0, 0, 2):
            changed = intent_fixture()
            changed["version"] = value
            with self.subTest(version=value):
                self.reject(OP.validate_intent, changed)

    def test_real_intent_rejects_all_foreign_paths_units_pairs_and_application_ports(self):
        changes = {
            "tool": [str(WORK / "source-attempt-54"), str(WORK / "storage-binding-live-ui-tool-01"),
                     str(WORK / "storage-binding-live-ui-tool-02"),
                     str(WORK / "storage-binding-live-ui-tool-03"),
                     str(WORK / "storage-binding-live-ui-tool-04"),
                     str(TOOL) + "/../storage-binding-live-ui-tool-01"],
            "output": [str(WORK / "client-backup-run-20260912_053517_9cb0074fc731"), str(OUTPUT) + "/other"],
            "runtime": [str(OUTPUT / "runtime"), "/opt/goby-binding-ui-runtime-foreign", str(RUNTIME) + "/../foreign"],
            "database": ["goby_backup_m3e_source", "goby_backup_m3e_target", "goby_binding_ui_other", "postgres"],
            "role": ["goby_backup_m3e_source", "goby_binding_ui_r_other", "postgres"],
            "controller_unit": ["goby-client-m3e.service", intent_fixture()["worker_unit"]],
            "worker_unit": ["goby-foundation-test.service", intent_fixture()["controller_unit"]],
            "origin": ["http://127.0.0.1:" + str(port) for port in
                       (5432, 15432, 18096, 18097, 18196, 18197, 18198, 18289)] +
                      ["http://localhost:18288", "http://0.0.0.0:18288", "http://127.0.0.1:18288/"]
        }
        for field, values in changes.items():
            for value in values:
                changed = intent_fixture()
                changed[field] = value
                with self.subTest(field=field, value=value):
                    self.reject(OP.validate_intent, changed)

    def test_real_intent_rejects_incomplete_extra_and_malformed_static_closure(self):
        for value in (None, True, [], "files", {}):
            changed = intent_fixture()
            changed["source_closure"] = value
            with self.subTest(shape=type(value).__name__):
                self.reject(OP.validate_intent, changed)
        for name in TOOL_FILES:
            changed = intent_fixture()
            del changed["source_closure"][name]
            with self.subTest(missing=name):
                self.reject(OP.validate_intent, changed)
        for name in ("../escape.py", "upgrade-main-schema25.py", "client-backup-pair.json", "unlisted.py"):
            changed = intent_fixture()
            changed["source_closure"][name] = "c" * 64
            with self.subTest(extra=name):
                self.reject(OP.validate_intent, changed)
        for value in (None, True, 4, "C" * 64, "d" * 63, "e" * 65):
            changed = intent_fixture()
            changed["source_closure"]["verify-storage-binding-live-ui.py"] = value
            with self.subTest(digest=value):
                self.reject(OP.validate_intent, changed)
        changed = intent_fixture()
        changed["source_closure"]["accepted-web-report.json"] = "d" * 64
        self.reject(OP.validate_intent, changed)

    def test_real_decoder_rejects_duplicate_nonfinite_oversize_and_deep_json(self):
        invalid = (b'{"nonce":"a","nonce":"b"}', b'{"x":NaN}', b'{"x":Infinity}', b"\xff",
                   b"[" * 34 + b"0" + b"]" * 34, b"[" + b"0," * 20000 + b"0]", b"{")
        for raw in invalid:
            with self.subTest(raw=raw[:40]):
                self.reject(OP.decode, raw)
        for raw in (bytearray(b"{}"), "{}", None, b" " * 65537):
            with self.subTest(type=type(raw).__name__):
                self.reject(OP.decode, raw, 65536)

    def test_real_intent_reader_rejects_foreign_path_or_digest_before_reading(self):
        with patch.object(OP, "read_file") as reader:
            for path in (OUTPUT / "intent.json", OP.HISTORY, TOOL / "../intent.json"):
                with self.subTest(path=str(path)):
                    self.reject(OP.load_intent, path, "a" * 64)
            for digest in (None, True, 1, "A" * 64, "a" * 63, ""):
                with self.subTest(digest=digest):
                    self.reject(OP.load_intent, TOOL / "intent.json", digest)
            reader.assert_not_called()

    def test_real_intent_reader_binds_exact_private_bytes_and_real_validation(self):
        raw = OP.canonical(intent_fixture())
        with patch.object(OP, "read_file", return_value=raw) as reader:
            self.assertEqual(OP.load_intent(TOOL / "intent.json", OP.sha(raw)), intent_fixture())
            reader.assert_called_once_with(TOOL / "intent.json", modes=(0o600,), limit=65536)
            self.reject(OP.load_intent, TOOL / "intent.json", "0" * 64)
        for raw in (b'{"marker":1}', b'{"run_id":1,"run_id":2}', b" " * 65537):
            with self.subTest(raw=raw[:40]), patch.object(OP, "read_file", return_value=raw):
                self.reject(OP.load_intent, TOOL / "intent.json", OP.sha(raw))

    def input_fixture(self):
        catalog = OP.canonical({"version": 28})
        selected = {OP.SOURCE_FILES[0]: b"# Workspace reader fixture.\n",
                    OP.SOURCE_FILES[1]: b"package backuppg\n", OP.SOURCE_FILES[2]: catalog}
        source = OP.canonical({"marker": "goby-client-backup-source-m3e-v1",
                               "files": {name: OP.sha(raw) for name, raw in selected.items()}})
        web = {"index.html": b"<!doctype html><title>Fixture</title>\n", "assets/app.js": b"void 0;\n"}
        report = OP.canonical({"status": "passed", "tests": {"expected": 68, "unexpected": 0, "flaky": 0, "skipped": 0},
                               "dist_files": {name: OP.sha(raw) for name, raw in web.items()}})
        files = {TOOL / name: (report if name == "accepted-web-report.json" else ("Fixture " + name).encode())
                 for name in TOOL_FILES}
        intent = intent_fixture()
        intent["source_closure"] = {name: OP.sha(files[TOOL / name]) for name in TOOL_FILES}
        files.update({OP.SOURCE / name: raw for name, raw in selected.items()})
        files.update({OP.WEB / name: raw for name, raw in web.items()})
        files.update({OP.SOURCE / "backup-source-inputs.json": source,
                      OP.BINARY: b"Frozen executable fixture.\n", OP.NODE: b"Frozen Node fixture.\n",
                      OP.HISTORY: b"Historical receipt fixture.\n"})
        intent["history_sha256"] = OP.sha(files[OP.HISTORY])
        files[TOOL / "intent.json"] = OP.canonical(intent)
        return types.SimpleNamespace(intent=intent, files=files, selected=selected, web=web, report=report,
                                     source=source, catalog=catalog, events=[], wrong_metadata={}, extra=[], nonfiles=set())

    @contextlib.contextmanager
    def input_effects(self, fixture):
        def reader(path, **options):
            fixture.events.append(("read", path, options))
            OP.require(path in fixture.files, "An in-memory input is absent.")
            return fixture.files[path]

        def inventory(path, pattern):
            self.assertEqual(pattern, "*")
            self.assertIn(path, (TOOL, OP.WEB))
            return iter([name for name in fixture.files if path in name.parents] +
                        [name for name in fixture.extra if path in name.parents])

        def info(path):
            regular = path in fixture.files or path in fixture.extra
            return fixture.wrong_metadata.get(path, metadata(
                size=len(fixture.files.get(path, b"")), mode=(stat.S_IFREG | 0o600) if regular else (stat.S_IFDIR | 0o700)))

        with contextlib.ExitStack() as patches:
            patches.enter_context(patch.object(OP, "__file__", str(TOOL / "verify-storage-binding-live-ui.py")))
            patches.enter_context(patch.object(OP, "WEB_REPORT_SHA", OP.sha(fixture.report)))
            patches.enter_context(patch.object(OP, "SOURCE_MANIFEST_SHA", OP.sha(fixture.source)))
            patches.enter_context(patch.object(OP, "CATALOG_SHA", OP.sha(fixture.catalog)))
            patches.enter_context(patch.object(OP, "BINARY_SHA", OP.sha(b"Frozen executable fixture.\n")))
            patches.enter_context(patch.object(OP, "NODE_SHA", OP.sha(b"Frozen Node fixture.\n")))
            patches.enter_context(patch.object(OP, "read_file", side_effect=reader))
            patches.enter_context(patch.object(OP, "directory", side_effect=lambda path, **kwargs:
                                              fixture.events.append(("directory", path, kwargs)) or
                                              {"device": 1, "inode": 2, "uid": 0, "gid": 0, "mode": 0o700}))
            patches.enter_context(patch.object(OP, "path_info", side_effect=info))
            patches.enter_context(patch.object(OP, "verify_dependencies", side_effect=lambda intent:
                                              fixture.events.append(("dependencies",)) or {"memory": "verified"}))
            patches.enter_context(patch.object(Path, "rglob", inventory))
            patches.enter_context(patch.object(Path, "is_file", lambda path:
                                              path not in fixture.nonfiles and
                                              (path in fixture.files or path in fixture.extra)))
            yield

    def test_real_input_reader_verifies_complete_product_and_selected_source_bytes(self):
        fixture = self.input_fixture()
        with self.input_effects(fixture):
            result = OP.verify_inputs(fixture.intent)
        self.assertEqual(result, {"selected": fixture.selected, "catalog": {"version": 28},
                                  "web": {name: OP.sha(raw) for name, raw in fixture.web.items()},
                                  "dependencies": {"memory": "verified"}})
        reads = [event[1] for event in fixture.events if event[0] == "read"]
        self.assertIn(OP.HISTORY, reads)
        self.assertEqual(set(reads), set(fixture.files) - {TOOL / "intent.json"})
        self.assertFalse(any("upgrade-main-schema25" in str(path) for path in reads))

    def test_real_input_reader_rejects_each_changed_file_without_any_mutation(self):
        seed = self.input_fixture()
        for path in set(seed.files) - {TOOL / "intent.json"}:
            fixture = self.input_fixture()
            fixture.files[path] += b"changed"
            with self.subTest(path=str(path)), self.input_effects(fixture):
                self.reject(OP.verify_inputs, fixture.intent)
            self.assertTrue(all(event[0] in ("read", "directory", "dependencies") for event in fixture.events))

    def test_real_input_reader_rejects_tool_and_frontend_membership_drift(self):
        for scope, defect in ((TOOL, "extra"), (OP.WEB, "extra"), (OP.WEB, "missing")):
            fixture = self.input_fixture()
            if defect == "missing":
                del fixture.files[OP.WEB / "assets/app.js"]
            else:
                fixture.extra.append(scope / "unlisted.js")
                fixture.files[scope / "unlisted.js"] = b"Extra artifact.\n"
            with self.subTest(scope=str(scope), defect=defect), self.input_effects(fixture):
                self.reject(OP.verify_inputs, fixture.intent)

    def test_real_input_reader_rejects_untrusted_tool_or_frontend_metadata(self):
        for path in (TOOL / "package.json", OP.WEB / "index.html"):
            for change in ({"uid": 995}, {"gid": 995}, {"mode": stat.S_IFREG | 0o622},
                           {"mode": stat.S_IFLNK | 0o777}):
                fixture = self.input_fixture()
                fixture.wrong_metadata[path] = metadata(**change)
                with self.subTest(path=str(path), change=change), self.input_effects(fixture):
                    self.reject(OP.verify_inputs, fixture.intent)

    def test_real_input_reader_rejects_a_relocated_entrypoint(self):
        fixture = self.input_fixture()
        with self.input_effects(fixture), patch.object(OP, "__file__", str(OUTPUT / "worker.py")):
            self.reject(OP.verify_inputs, fixture.intent)

    def test_real_input_reader_rejects_unlisted_special_tool_entries(self):
        for kind in (stat.S_IFIFO, stat.S_IFSOCK, stat.S_IFCHR, stat.S_IFBLK):
            fixture = self.input_fixture()
            path = TOOL / "special-entry"
            fixture.extra.append(path)
            fixture.nonfiles.add(path)
            fixture.wrong_metadata[path] = metadata(mode=kind | 0o600)
            with self.subTest(kind=kind), self.input_effects(fixture):
                self.reject(OP.verify_inputs, fixture.intent)

    def test_real_input_reader_rejects_malformed_selected_source_manifest(self):
        for value in (None, [], {"marker": "wrong", "files": {}},
                      {"marker": "goby-client-backup-source-m3e-v1", "files": None},
                      {"marker": "goby-client-backup-source-m3e-v1", "files": []},
                      {"marker": "goby-client-backup-source-m3e-v1", "files": {}},
                      {"marker": "goby-client-backup-source-m3e-v1", "files": {}, "extra": True}):
            fixture = self.input_fixture()
            fixture.source = OP.canonical(value)
            fixture.files[OP.SOURCE / "backup-source-inputs.json"] = fixture.source
            with self.subTest(value=value), self.input_effects(fixture):
                self.reject(OP.verify_inputs, fixture.intent)

    def test_real_input_reader_rejects_unaccepted_or_malformed_frontend_report(self):
        for value in (None, [], {}, {"status": "failed"},
                      {"status": "passed", "tests": None},
                      {"status": "passed", "tests": {"expected": 68, "unexpected": False}, "dist_files": {}},
                      {"status": "passed", "tests": {"expected": 68, "unexpected": 0}, "dist_files": []},
                      {"status": "passed", "tests": {"expected": 68, "unexpected": 0}, "dist_files": {}}):
            fixture = self.input_fixture()
            fixture.report = OP.canonical(value)
            fixture.files[TOOL / "accepted-web-report.json"] = fixture.report
            fixture.intent["source_closure"]["accepted-web-report.json"] = OP.sha(fixture.report)
            with self.subTest(value=value), self.input_effects(fixture):
                self.reject(OP.verify_inputs, fixture.intent)

    def test_real_input_reader_rejects_truthy_frontend_counters_with_otherwise_valid_inventory(self):
        for field, value in (("expected", 68.0), ("unexpected", False), ("unexpected", 0.0)):
            fixture = self.input_fixture()
            report = OP.decode(fixture.report)
            report["tests"][field] = value
            fixture.report = OP.canonical(report)
            fixture.files[TOOL / "accepted-web-report.json"] = fixture.report
            fixture.intent["source_closure"]["accepted-web-report.json"] = OP.sha(fixture.report)
            with self.subTest(field=field, value=value), self.input_effects(fixture):
                self.reject(OP.verify_inputs, fixture.intent)

    def test_real_dependency_reader_rejects_malformed_or_foreign_manifest_before_tree_reads(self):
        base = {"marker": "goby-storage-binding-live-ui-dependencies-v1", "root": str(OP.NODE_MODULES),
                "root_identity": {}, "files": {}, "directories": {}, "browser": {}}
        invalid = [None, [], {}, dict(base, marker="historical"), dict(base, root="/dev/shm/goby-admin-ui/node_modules"),
                   dict(base, resume=True)]
        for value in invalid:
            with self.subTest(value=value), patch.object(OP, "read_file", return_value=OP.canonical(value)), \
                    patch.object(OP, "path_info") as info, patch.object(Path, "rglob") as inventory:
                self.reject(OP.verify_dependencies, intent_fixture())
                info.assert_not_called()
                inventory.assert_not_called()

    def test_real_dependency_tree_rejects_membership_mode_hash_or_link_drift_before_lockfile(self):
        root = OP.NODE_MODULES
        root_info = metadata(mode=stat.S_IFDIR | 0o755)
        directories = ("@playwright", "@playwright/test", "playwright", "playwright-core")
        names = ("@playwright/test/package.json", "playwright/package.json", "playwright-core/package.json")
        for defect in ("missing", "extra", "mode", "hash", "hardlink", "special"):
            files = {root / name: ("Package fixture " + name).encode() for name in names}
            expected = {name: {"sha256": OP.sha(files[root / name]), "mode": 0o644} for name in names}
            manifest = {"marker": "goby-storage-binding-live-ui-dependencies-v1", "root": str(root),
                        "root_identity": OP.identity(root_info), "files": expected,
                        "directories": {name: 0o755 for name in directories}, "browser": {}}
            listed = set(files)
            changed_path = root / names[0]
            if defect == "missing":
                listed.remove(changed_path)
            elif defect == "extra":
                files[root / "playwright/unlisted.js"] = b"Extra dependency.\n"
                listed.add(root / "playwright/unlisted.js")
            elif defect == "hash":
                files[changed_path] += b"changed"
            reads = []

            def reader(path, **options):
                reads.append(path)
                return OP.canonical(manifest) if path == TOOL / "dependency-files.json" else files[path]

            def info(path):
                if path not in files:
                    return root_info
                mode = stat.S_IFREG | (0o664 if defect == "mode" and path == changed_path else 0o644)
                if defect == "special" and path == changed_path:
                    mode = stat.S_IFIFO | 0o644
                return metadata(mode=mode, nlink=2 if defect == "hardlink" and path == changed_path else 1,
                                size=len(files[path]))

            with self.subTest(defect=defect), patch.object(OP, "read_file", side_effect=reader), \
                    patch.object(OP, "hash_file", side_effect=lambda path, **options: OP.sha(files[path])), \
                    patch.object(OP, "path_info", side_effect=info), \
                    patch.object(Path, "rglob", lambda base, pattern: iter(path for path in listed if base in path.parents)):
                self.reject(OP.verify_dependencies, intent_fixture())
            self.assertNotIn(TOOL / "package-lock.json", reads)

    def test_real_source_helper_reader_rechecks_exact_bytes_before_import(self):
        selected = {OP.SOURCE_FILES[0]: b"Selected reviewed helper.\n"}
        with patch.object(OP, "read_file", return_value=b"Replacement helper.\n"), \
                patch.object(builtins, "compile") as compiler:
            self.reject(OP.load_workspace, selected)
        compiler.assert_not_called()

    def test_real_source_helper_loads_only_verified_bytes_and_rejects_another_cluster(self):
        for foreign in (False, True):
            raw = ("from pathlib import Path\n" +
                   "CONTROL = Path('/opt/goby-test/postgres-workspace-v1')\n" +
                   "DATA = Path('/var/lib/postgresql/goby-workspace-v1/data')\n" +
                   "SOCKET = Path('/var/lib/postgresql/goby-workspace-v1/socket')\n" +
                   "PORT = " + ("5432" if foreign else "15432") + "\n").encode()
            with self.subTest(foreign=foreign), patch.object(OP, "read_file", return_value=raw) as reader:
                if foreign:
                    self.reject(OP.load_workspace, {OP.SOURCE_FILES[0]: raw})
                else:
                    loaded = OP.load_workspace({OP.SOURCE_FILES[0]: raw})
                    self.assertEqual((loaded.CONTROL, loaded.DATA, loaded.SOCKET, loaded.PORT),
                                     (OP.CONTROL, OP.DATA, OP.SOCKET, 15432))
                reader.assert_called_once_with(OP.SOURCE / OP.SOURCE_FILES[0])

    def test_real_file_reader_rejects_wrong_metadata_before_opening(self):
        path = TOOL / "intent.json"
        changes = ({"mode": stat.S_IFDIR | 0o600}, {"mode": stat.S_IFLNK | 0o600},
                   {"mode": stat.S_IFREG | 0o666}, {"uid": 995}, {"gid": 995}, {"nlink": 2}, {"size": 65})
        for change in changes:
            with self.subTest(change=change), patch.object(OP, "path_info", return_value=metadata(**change)), \
                    patch.object(os, "open") as opened:
                self.reject(OP.read_file, path, modes=(0o600,), limit=64)
                opened.assert_not_called()

    def test_real_file_reader_uses_nofollow_and_rejects_identity_or_read_races(self):
        path = TOOL / "intent.json"
        before = metadata(size=7)
        for defect in (None, "opened inode", "final inode", "mtime", "oversize"):
            opened_info = metadata(size=7, inode=92 if defect == "opened inode" else 91)
            final_info = metadata(size=7, inode=92 if defect == "final inode" else 91)
            after = metadata(size=7)
            if defect == "mtime":
                after.st_mtime_ns = 99
            stream = MemoryStream(b"fixture" if defect != "oversize" else b"fixture too long")
            with self.subTest(defect=defect), patch.object(OP, "path_info", return_value=before), \
                    patch.object(os, "open", return_value=713) as opened, \
                    patch.object(os, "fdopen", return_value=stream), \
                    patch.object(os, "fstat", side_effect=[opened_info, after]), \
                    patch.object(Path, "lstat", return_value=final_info):
                if defect is None:
                    self.assertEqual(OP.read_file(path, limit=7), b"fixture")
                else:
                    self.reject(OP.read_file, path, limit=7)
                opened.assert_called_once_with(path, os.O_RDONLY | os.O_NOFOLLOW)

    def test_real_dependency_hasher_streams_nofollow_bytes_with_complete_size_proof(self):
        path = OP.NODE_MODULES / "playwright-core/large-fixture.js"
        raw = b"a" * ((1 << 20) + 7)
        info = metadata(mode=stat.S_IFREG | 0o644, size=len(raw))
        reads = []

        class BoundedStream(MemoryStream):
            def read(self, size=-1):
                reads.append(size)
                return super().read(size)

        with patch.object(OP, "path_info", return_value=info), patch.object(os, "open", return_value=713) as opened, \
                patch.object(os, "fdopen", return_value=BoundedStream(raw)), \
                patch.object(os, "fstat", return_value=info), patch.object(Path, "lstat", return_value=info):
            self.assertEqual(OP.hash_file(path, limit=len(raw)), hashlib.sha256(raw).hexdigest())
        opened.assert_called_once_with(path, os.O_RDONLY | os.O_NOFOLLOW)
        self.assertEqual(reads, [1 << 20, 1 << 20, 1 << 20])

    def test_real_dependency_hasher_rejects_open_read_size_and_identity_races(self):
        path = OP.NODE_MODULES / "playwright-core/fixture.js"
        for defect in ("opened inode", "final inode", "mtime", "size", "oversize"):
            before = metadata(mode=stat.S_IFREG | 0o644, size=7)
            opened = metadata(mode=stat.S_IFREG | 0o644, size=7, inode=92 if defect == "opened inode" else 91)
            final = metadata(mode=stat.S_IFREG | 0o644, size=7, inode=92 if defect == "final inode" else 91)
            after = metadata(mode=stat.S_IFREG | 0o644, size=7)
            if defect == "mtime":
                after.st_mtime_ns = 99
            raw = b"fixture extra" if defect == "oversize" else b"short" if defect == "size" else b"fixture"
            with self.subTest(defect=defect), patch.object(OP, "path_info", return_value=before), \
                    patch.object(os, "open", return_value=713), patch.object(os, "fdopen", return_value=MemoryStream(raw)), \
                    patch.object(os, "fstat", side_effect=[opened, after]), patch.object(Path, "lstat", return_value=final):
                self.reject(OP.hash_file, path, limit=7)

    def test_real_path_reader_rejects_a_symlink_in_every_ancestor(self):
        path = TOOL / "e2e/root-binding-live.spec.ts"
        for rejected in (path, *path.parents):
            seen = []

            def observe(candidate):
                seen.append(candidate)
                return metadata(mode=(stat.S_IFLNK if candidate == rejected else stat.S_IFDIR) | 0o700)

            with self.subTest(ancestor=str(rejected)), patch.object(Path, "lstat", observe):
                self.reject(OP.path_info, path)
            self.assertEqual(seen[-1], rejected)

    def test_real_file_creator_uses_exclusive_nofollow_and_exact_private_metadata(self):
        path = OUTPUT / "proposal.json"
        events = []

        class Sink:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def fileno(self):
                return 713

            def write(self, raw):
                events.append(("write", raw))

            def flush(self):
                events.append(("flush",))

        with patch.object(OP, "path_info", side_effect=lambda path: events.append(("parent", path))), \
                patch.object(os, "open", return_value=713) as opened, \
                patch.object(os, "fdopen", return_value=Sink()), \
                patch.object(os, "fchown", side_effect=lambda *args: events.append(("chown", *args))), \
                patch.object(os, "fchmod", side_effect=lambda *args: events.append(("chmod", *args))), \
                patch.object(os, "fsync", side_effect=lambda *args: events.append(("sync", *args))):
            OP.create_file(path, b"proposal")
        opened.assert_called_once_with(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        self.assertEqual(events, [("parent", OUTPUT), ("chown", 713, 0, 0), ("chmod", 713, 0o600),
                                  ("write", b"proposal"), ("flush",), ("sync", 713)])

    def test_real_file_creator_rejects_nonbytes_before_any_filesystem_boundary(self):
        with patch.object(OP, "path_info") as parent, patch.object(os, "open") as opened:
            for value in (None, "proposal", bytearray(b"proposal"), []):
                with self.subTest(type=type(value).__name__):
                    self.reject(OP.create_file, OUTPUT / "proposal.json", value)
            parent.assert_not_called()
            opened.assert_not_called()

    def test_real_file_replacement_refuses_a_second_read_race_without_replace(self):
        path = OP.HBA
        with patch.object(OP, "read_file", side_effect=[b"original", b"foreign"]), \
                patch.object(OP, "create_file") as created, \
                patch.object(secrets, "token_hex", return_value="1" * 16), \
                patch.object(os, "replace") as replaced:
            self.reject(OP.replace_file, path, b"original", b"temporary")
        created.assert_called_once_with(path.with_name(path.name + ".binding-ui-" + "1" * 16),
                                        b"temporary", uid=0, gid=0)
        replaced.assert_not_called()

    def test_real_file_replacement_refuses_unknown_original_before_temporary_write(self):
        with patch.object(OP, "read_file", return_value=b"foreign"), \
                patch.object(OP, "create_file") as created, patch.object(os, "replace") as replaced:
            self.reject(OP.replace_file, OP.HBA, b"original", b"temporary")
        created.assert_not_called()
        replaced.assert_not_called()

    def database_fixture(self):
        db = OP.Database(intent_fixture(), {"selected": {}, "catalog": {}}, "d" * 64)
        role = {"oid": 17001, "tag": db.tag, "login": True, "super": False, "createdb": False,
                "createrole": False, "replication": False, "bypass": False, "inherit": False,
                "limit": 12, "config": None, "valid_until": None, "scram": True}
        database = {"oid": 18001, "owner_oid": 17001, "tag": db.tag, "allow": True, "template": False,
                    "limit": -1, "encoding": "UTF8", "acl": [
                        {"grantor": 17001, "grantee": 17001, "privilege_type": privilege, "is_grantable": False}
                        for privilege in ("CREATE", "CONNECT", "TEMPORARY")]}
        public = {"oid": 19001, "owner": "pg_database_owner", "comment": "standard public schema",
                  "acl": ["pg_database_owner=UC/pg_database_owner", "=U/pg_database_owner"]}
        db.pair = {"role_oid": 17001, "database_oid": 18001, "phase": "owned",
                   "database": copy.deepcopy(database), "public": copy.deepcopy(public), "casts": []}
        return types.SimpleNamespace(db=db, role=role, database=database, public=public,
                                     rows={"role": role, "database": database}, casts=[], events=[])

    def test_real_database_creation_records_partial_ddl_before_each_next_mutation(self):
        for fail_at, expected_phase, expected_role, expected_database in (
            (1, "role_pending", None, None), (2, "database_pending", 17001, None),
            (3, "database_tag_pending", 17001, 18001),
        ):
            fixture = self.database_fixture()
            db = fixture.db
            db.pair = {"role_oid": None, "database_oid": None, "phase": "absent"}
            observations = [{"role": None, "database": None}, {"role": fixture.role, "database": None},
                            {"role": fixture.role, "database": dict(fixture.database, tag=None)}]
            sql_calls = []

            def mutation(sql):
                sql_calls.append(sql)
                fixture.events.append(("sql", sql))
                if len(sql_calls) == fail_at:
                    raise OP.Failure("An owned DDL acknowledgement was lost.")
                return b""

            def journal():
                fixture.events.append(("receipt", copy.deepcopy(db.pair)))

            with self.subTest(fail_at=fail_at), patch.object(db, "rows", side_effect=observations), \
                    patch.object(db, "maintenance", side_effect=mutation), patch.object(db, "journal", side_effect=journal):
                self.reject(db.create)
            self.assertEqual((db.pair["phase"], db.pair["role_oid"], db.pair["database_oid"]),
                             (expected_phase, expected_role, expected_database))
            self.assertEqual(len(sql_calls), fail_at)
            for index, event in enumerate(fixture.events):
                if event[0] == "sql":
                    self.assertEqual(fixture.events[index - 1][0], "receipt")
            self.assertFalse(any("DROP " in sql or "CASCADE" in sql or "FORCE" in sql for sql in sql_calls))

    def test_real_database_creation_refuses_existing_names_without_journal_or_ddl(self):
        fixture = self.database_fixture()
        for rows in ({"role": fixture.role, "database": None}, {"role": None, "database": fixture.database},
                     fixture.rows):
            with self.subTest(rows=rows), patch.object(fixture.db, "rows", return_value=rows), \
                    patch.object(fixture.db, "journal") as journal, patch.object(fixture.db, "maintenance") as ddl:
                self.reject(fixture.db.create)
                journal.assert_not_called()
                ddl.assert_not_called()

    def test_real_database_creation_requires_public_and_empty_catalog_before_owned_receipt(self):
        for defect in (None, "public", "objects"):
            fixture = self.database_fixture()
            db = fixture.db
            db.pair = {"role_oid": None, "database_oid": None, "phase": "absent"}
            observations = [{"role": None, "database": None}, {"role": fixture.role, "database": None},
                            {"role": fixture.role, "database": dict(fixture.database, tag=None)},
                            copy.deepcopy(fixture.rows), copy.deepcopy(fixture.rows)]
            if defect == "public":
                fixture.public["owner"] = "foreign"
            phases = []
            commands = []

            def maintenance(sql):
                commands.append(sql)
                return b"0" if sql.startswith("SELECT ") else b""

            def query(sql, **options):
                self.assertIs(options.get("administrative"), True)
                return OP.canonical(fixture.public) if "FROM pg_namespace" in sql else b"[]"

            with self.subTest(defect=defect), patch.object(db, "rows", side_effect=observations), \
                    patch.object(db, "maintenance", side_effect=maintenance), patch.object(db, "query", side_effect=query), \
                    patch.object(db, "objects", return_value=[{"unowned": True}] if defect == "objects" else []), \
                    patch.object(db, "journal", side_effect=lambda: phases.append(db.pair["phase"])):
                if defect is None:
                    db.create()
                else:
                    self.reject(db.create)
            self.assertEqual(phases[:3], ["role_pending", "database_pending", "database_tag_pending"])
            self.assertEqual(db.pair["phase"], "owned" if defect is None else "database_tag_pending")
            self.assertEqual(phases.count("owned"), int(defect is None))
            self.assertFalse(any(sql.startswith("DROP ") for sql in commands))

    def test_real_database_ownership_rejects_drift_before_dependency_queries(self):
        for scope, field, value in (("role", "oid", 17002), ("role", "tag", "historical"),
                                    ("role", "super", True), ("role", "inherit", True), ("role", "config", []),
                                    ("database", "oid", 18002), ("database", "owner_oid", 17002),
                                    ("database", "tag", "historical"), ("database", "allow", False)):
            fixture = self.database_fixture()
            fixture.rows[scope][field] = value
            with self.subTest(scope=scope, field=field), patch.object(fixture.db, "rows", return_value=fixture.rows), \
                    patch.object(fixture.db, "maintenance") as query:
                self.reject(fixture.db.verify_owned)
                query.assert_not_called()

    def test_real_database_ownership_rejects_external_memberships_and_settings(self):
        fixture = self.database_fixture()
        with patch.object(fixture.db, "rows", return_value=fixture.rows), \
                patch.object(fixture.db, "maintenance", return_value=b"1") as query:
            self.reject(fixture.db.verify_owned)
        query.assert_called_once()
        sql = query.call_args.args[0]
        self.assertTrue(sql.startswith("SELECT "))
        for relation in ("pg_auth_members", "pg_db_role_setting", "pg_shdepend", "pg_shseclabel"):
            self.assertIn(relation, sql)

    @contextlib.contextmanager
    def disposal_effects(self, fixture, *, initial_busy=False, racing=False, dependency=False):
        db = fixture.db
        state = copy.deepcopy(fixture.rows)
        snapshot = {"rows": {"users": []}, "sequences": {}}
        fixture.snapshot = snapshot

        def maintenance(sql):
            fixture.events.append(("sql", sql))
            if sql.startswith("ALTER DATABASE "):
                self.assertEqual(sql, "ALTER DATABASE " + db.name + " ALLOW_CONNECTIONS false;")
                state["database"]["allow"] = False
                return b""
            if sql.startswith("DROP DATABASE "):
                self.assertEqual(sql, "DROP DATABASE " + db.name + ";")
                state["database"] = None
                return b""
            if sql.startswith("DROP ROLE "):
                self.assertEqual(sql, "DROP ROLE " + db.role + ";")
                state["role"] = None
                return b""
            self.assertTrue(sql.startswith("SELECT "))
            if "pg_prepared_xacts" in sql:
                return b"1" if initial_busy else b"0"
            if sql.startswith("SELECT count(*) FROM pg_stat_activity"):
                return b"1" if racing else b"0"
            if state["database"] is None and "pg_shdepend" in sql:
                return b"1" if dependency else b"0"
            return b"0"

        def query(sql, **options):
            fixture.events.append(("query", sql, options))
            self.assertIs(options.get("administrative"), True)
            if "FROM pg_namespace" in sql:
                return OP.canonical(fixture.public)
            self.assertIn("FROM pg_cast", sql)
            return OP.canonical(fixture.casts)

        with patch.object(db, "rows", side_effect=lambda: copy.deepcopy(state)), \
                patch.object(db, "maintenance", side_effect=maintenance), \
                patch.object(db, "query", side_effect=query), patch.object(db, "snapshot", return_value=snapshot), \
                patch.object(db, "journal", side_effect=lambda: fixture.events.append(("receipt", db.pair["phase"]))):
            yield

    def test_real_database_disposal_closes_then_drops_only_the_new_pair(self):
        fixture = self.database_fixture()
        with self.disposal_effects(fixture):
            fixture.db.dispose(fixture.snapshot)
        commands = [event[1] for event in fixture.events if event[0] == "sql" and not event[1].startswith("SELECT ")]
        self.assertEqual(commands, ["ALTER DATABASE " + fixture.db.name + " ALLOW_CONNECTIONS false;",
                                    "DROP DATABASE " + fixture.db.name + ";", "DROP ROLE " + fixture.db.role + ";"])
        self.assertEqual(fixture.db.pair["phase"], "removed")
        self.assertFalse(any(any(token in command for token in ("FORCE", "CASCADE", "pg_terminate_backend"))
                             for command in commands))

    def test_real_database_disposal_retains_partial_ddl_without_any_query_or_drop(self):
        for phase in ("absent", "role_pending", "database_pending", "database_tag_pending", "closed", "database_removed"):
            fixture = self.database_fixture()
            fixture.db.pair["phase"] = phase
            with self.subTest(phase=phase), patch.object(fixture.db, "rows") as rows, \
                    patch.object(fixture.db, "query") as query, patch.object(fixture.db, "maintenance") as ddl:
                self.reject(fixture.db.dispose, {})
                rows.assert_not_called()
                query.assert_not_called()
                ddl.assert_not_called()

    def test_real_database_disposal_refuses_busy_racing_or_dependency_work(self):
        for defect in ("busy", "race", "dependency"):
            fixture = self.database_fixture()
            with self.subTest(defect=defect), self.disposal_effects(
                    fixture, initial_busy=defect == "busy", racing=defect == "race", dependency=defect == "dependency"):
                self.reject(fixture.db.dispose, fixture.snapshot)
            commands = [event[1] for event in fixture.events if event[0] == "sql" and not event[1].startswith("SELECT ")]
            self.assertFalse(any(command.startswith("DROP ROLE ") for command in commands))
            if defect in ("busy", "race"):
                self.assertFalse(any(command.startswith("DROP DATABASE ") for command in commands))
            if defect == "busy":
                self.assertEqual(commands, [])
            self.assertEqual(fixture.db.pair["phase"],
                             {"busy": "owned", "race": "closed", "dependency": "database_removed"}[defect])

    def test_real_database_disposal_refuses_changed_final_rows_namespace_or_casts_before_close(self):
        for defect in ("rows", "public", "casts"):
            fixture = self.database_fixture()
            if defect == "public":
                fixture.public["oid"] = 19002
            elif defect == "casts":
                fixture.casts.append({"unowned_cast": True})
            with self.subTest(defect=defect), self.disposal_effects(fixture):
                self.reject(fixture.db.dispose, {} if defect == "rows" else fixture.snapshot)
            commands = [event[1] for event in fixture.events if event[0] == "sql" and not event[1].startswith("SELECT ")]
            self.assertEqual(commands, [])
            self.assertEqual(fixture.db.pair["phase"], "owned")

    def test_real_database_snapshot_projects_sequence_fields_without_a_composite_row(self):
        fixture = self.database_fixture()
        tables = """activity_entries application_key_clients application_key_devices application_keys catalog_entities
client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images
item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions
scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests
task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users""".split()
        sequences = ("activity_entries_id_seq", "application_keys_id_seq", "theme_owner_ids_id_seq")
        fixture.db.inputs["catalog"] = {"objects": [], "catalog": {
            "Tables": [{"Name": name} for name in tables], "Sequences": [{"Name": name} for name in sequences]}}
        expected = {"rows": {name: [] for name in tables}, "sequences": {
            name: {"last_value": 1, "log_cnt": 0, "is_called": False} for name in sequences}}
        statements = []

        def command(arguments, *, stdin, **options):
            arguments = [str(value) for value in arguments]
            sql = stdin.decode()
            statements.append(sql)
            self.assertEqual(arguments[arguments.index("-d") + 1], fixture.db.name)
            self.assertEqual(arguments[arguments.index("-p") + 1], "15432")
            self.assertIn("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;", sql)
            if len(statements) == 1:
                self.assertIn("SELECT (SELECT count(*) FROM public.activity_entries)", sql)
                return b"0"
            self.assertEqual(len(statements), 2)
            self.assertNotIn("to_jsonb(s)", sql)
            for name in sequences:
                self.assertIn(f"SELECT '{name}' AS name,jsonb_build_object('last_value',s.last_value,"
                              f"'log_cnt',s.log_cnt,'is_called',s.is_called) AS value FROM public.{name} s", sql)
            return OP.canonical(expected)

        with patch.object(fixture.db, "objects", return_value=[]), patch.object(OP, "command", side_effect=command):
            self.assertEqual(fixture.db.snapshot(), expected)
        self.assertEqual(len(statements), 2)

    def final_database_fixture(self):
        administrator, library = "1" * 32, "2" * 32
        root_ids = {"A": "3" * 32, "B": "4" * 32, "U": "5" * 32}
        rows = {table: [] for table in (
            "scan_jobs", "devices", "application_keys", "application_key_clients", "application_key_devices",
            "play_sessions", "encoding_jobs", "user_item_data", "item_entities", "item_images", "item_subtitles",
            "item_extra_resources", "item_theme_resources", "catalog_entities", "task_runs", "task_run_requests",
            "task_run_children", "task_occurrences",
        )}
        rows.update(users=[{"id": administrator, "is_administrator": True, "is_disabled": False}],
                    libraries=[{"id": library}], items=[{"id": library, "type": "CollectionFolder"}],
                    library_roots=[], activity_entries=[], sessions=[],
                    server_settings=[{"key": "server_id", "value": "owned-server"},
                                     {"key": "setup_completed", "value": "true"}],
                    item_metadata_state=[{"item_id": library, "revision": 1, "overrides": {}, "locked_values": {},
                                          "last_edited_by": None, "last_edited_at": None}],
                    theme_owner_ids=[{"id": 1, "virtual_root": True, "item_id": None},
                                     {"id": 2, "virtual_root": False, "item_id": library}])
        bindings = {}
        timestamp = "2026-09-12T12:00:00+00:00"
        anchor = {"version": 1, "profile": "linux-fsuuid-filehandle-v1", "filesystem_uuid": "a" * 32,
                  "handle_type": 1, "handle": "AQ=="}
        for key, revision in (("A", 3), ("B", 1), ("U", 2)):
            path = str(RUNTIME / "app/media" / key)
            document = {"version": 1, "mapping": {"approved_path": str(RUNTIME / "app/media"), "registered_path": path},
                        "anchor": copy.deepcopy(anchor), "registered_root": dict(anchor, handle={"A": "Ag==", "B": "Aw==", "U": "BA=="}[key]),
                        "boundaries": [{"relative_path": "archive", "identity": dict(anchor, handle="BQ==")}] if key == "A" else []}
            fingerprint, topology = OP.stored_topology(document)
            row = {"id": root_ids[key], "library_id": library, "path": path, "allowed_path": str(RUNTIME / "app/media"),
                   "relative_path": key, "binding_revision": revision, "storage_binding": document,
                   "bound_at": timestamp, "bound_by": administrator}
            rows["library_roots"].append(row)
            bindings[key] = {"Id": root_ids[key], "LibraryId": library, "Path": path, "AllowedPath": row["allowed_path"],
                             "RelativePath": key, "Revision": str(revision), "Status": "verified", "BoundAt": timestamp,
                             "BoundBy": administrator, "ApprovedFingerprint": fingerprint, "ObservedFingerprint": fingerprint,
                             "Approved": topology, "Observed": copy.deepcopy(topology)}
        proof = {"administrator_id": administrator, "library_id": library, "root_ids": root_ids, "bindings": bindings,
                 "baseline_b": copy.deepcopy(rows["library_roots"][1]), "controller_fingerprint": "e" * 64}
        for key, previous, revision, fingerprint in (
            ("A", 1, 2, proof["controller_fingerprint"]), ("A", 2, 3, bindings["A"]["ApprovedFingerprint"]),
            ("U", 1, 2, bindings["U"]["ApprovedFingerprint"]),
        ):
            rows["activity_entries"].append({"action": "library.root_binding.updated", "resource_kind": "library_root",
                "resource_id": root_ids[key], "previous_revision": previous, "revision": revision,
                "observation_fingerprint": fingerprint, "source": "native", "actor_kind": "user", "actor_id": administrator})
        sessions = {}
        for session_id, token_hash in (("6" * 32, "a" * 64), ("7" * 32, "b" * 64)):
            sessions[session_id] = {"user_id": administrator, "token_sha256": token_hash}
            rows["sessions"].append({"id": session_id, "kind": "admin", "user_id": administrator,
                                     "revoked_at": timestamp, "token_hash": "\\x" + token_hash})
            for action in ("session.login", "session.revoked"):
                rows["activity_entries"].append({"action": action, "resource_kind": "session", "resource_id": session_id,
                                                 "source": "native", "actor_kind": "user", "actor_id": administrator})
        for action, resource, identifier in (("user.created", "user", administrator), ("library.created", "library", library)):
            rows["activity_entries"].append({"action": action, "resource_kind": resource, "resource_id": identifier,
                                             "source": "native", "actor_kind": "system" if resource == "user" else "user",
                                             "actor_id": "" if resource == "user" else administrator})
        for index, entry in enumerate(rows["activity_entries"], start=1):
            entry["id"] = index
        snapshot = {"rows": rows, "sequences": {"theme_owner_ids_id_seq": {"last_value": 2, "is_called": True},
                                                   "activity_entries_id_seq": {"last_value": 9, "is_called": True}}}
        return types.SimpleNamespace(snapshot=snapshot, proof=proof, sessions=sessions)

    def test_real_final_database_accepts_the_single_unedited_collection_trigger_rows(self):
        fixture = self.final_database_fixture()
        OP.verify_final_database(fixture.snapshot, fixture.proof, fixture.sessions)

    def test_real_final_database_rejects_duplicate_missing_or_changed_binding_transitions(self):
        for defect in ("duplicate", "missing", "wrong previous", "wrong fingerprint"):
            fixture = self.final_database_fixture()
            events = fixture.snapshot["rows"]["activity_entries"]
            if defect == "duplicate":
                events[2] = copy.deepcopy(events[0])
            elif defect == "missing":
                del events[2]
            elif defect == "wrong previous":
                events[2]["previous_revision"] = 2
            else:
                events[2]["observation_fingerprint"] = "f" * 64
            with self.subTest(defect=defect):
                self.reject(OP.verify_final_database, fixture.snapshot, fixture.proof, fixture.sessions)

    def test_real_final_database_rejects_edited_metadata_or_nonunique_collection_owners(self):
        for defect in ("missing metadata", "extra metadata", "revision", "override", "editor", "owner missing", "owner duplicate", "owner changed"):
            fixture = self.final_database_fixture()
            rows = fixture.snapshot["rows"]
            if defect == "missing metadata":
                rows["item_metadata_state"] = []
            elif defect == "extra metadata":
                rows["item_metadata_state"].append(copy.deepcopy(rows["item_metadata_state"][0]))
            elif defect == "revision":
                rows["item_metadata_state"][0]["revision"] = 2
            elif defect == "override":
                rows["item_metadata_state"][0]["overrides"] = {"Name": "edited"}
            elif defect == "editor":
                rows["item_metadata_state"][0]["last_edited_by"] = fixture.proof["administrator_id"]
            elif defect == "owner missing":
                rows["theme_owner_ids"].pop()
            elif defect == "owner duplicate":
                rows["theme_owner_ids"][1]["id"] = rows["theme_owner_ids"][0]["id"]
            else:
                rows["theme_owner_ids"][1]["item_id"] = "f" * 32
            with self.subTest(defect=defect):
                self.reject(OP.verify_final_database, fixture.snapshot, fixture.proof, fixture.sessions)

    def test_real_collection_defaults_use_shared_sql_functions_inside_the_owned_readonly_transaction(self):
        fixture = self.database_fixture()
        library = "2" * 32
        with patch.object(OP, "command", return_value=b"t") as command:
            fixture.db.verify_library_defaults(library)
        command.assert_called_once()
        arguments = [str(item) for item in command.call_args.args[0]]
        sql = command.call_args.kwargs["stdin"].decode()
        self.assertEqual(arguments[:4], ["/usr/sbin/runuser", "--user", "postgres", "--"])
        self.assertEqual(arguments[arguments.index("-p") + 1], "15432")
        self.assertEqual(arguments[arguments.index("-d") + 1], fixture.db.name)
        self.assertTrue(sql.startswith("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;"))
        self.assertTrue(sql.endswith("COMMIT;\n"))
        for marker in ("catalog_metadata_automatic_values", "catalog_metadata_source_key", "m.effective IS NOT DISTINCT FROM i.local_metadata",
                       "m.updated_at=i.created_at", "WHERE i.id='" + library + "'", str(fixture.db.pair["database_oid"]),
                       str(fixture.db.pair["role_oid"]), fixture.db.tag):
            self.assertIn(marker, sql)
        self.assertNotIn("PGPASSWORD", command.call_args.kwargs["env"])

    def test_real_collection_defaults_reject_wrong_ids_and_nontrue_observations(self):
        fixture = self.database_fixture()
        with patch.object(fixture.db, "query") as query:
            for value in (None, True, "", "goby_backup_m3e_source", "2" * 32 + "'; DROP DATABASE postgres; --"):
                with self.subTest(value=value):
                    self.reject(fixture.db.verify_library_defaults, value)
            query.assert_not_called()
        for value in (b"f", b"", b"true", None):
            with self.subTest(value=value), patch.object(fixture.db, "query", return_value=value) as query:
                self.reject(fixture.db.verify_library_defaults, "2" * 32)
                self.assertIs(query.call_args.kwargs["administrative"], True)

    def test_real_worker_final_checks_allow_only_one_theme_owner_sequence_increment(self):
        for defect in (None, "sequence", "registration metadata", "initial owner", "defaults"):
            worker = self.worker_fixture()
            fixture = self.final_database_fixture()
            worker.library_id = fixture.proof["library_id"]
            worker.binding_proof = fixture.proof
            worker.tokens = {session_id: dict(proof, session_id=session_id) for session_id, proof in fixture.sessions.items()}
            worker.registration = copy.deepcopy(fixture.snapshot)
            worker.initial = copy.deepcopy(fixture.snapshot)
            for table in ("users", "sessions", "libraries", "library_roots", "items", "activity_entries", "item_metadata_state"):
                worker.initial["rows"][table] = []
            worker.initial["rows"]["theme_owner_ids"] = [copy.deepcopy(fixture.snapshot["rows"]["theme_owner_ids"][0])]
            worker.initial["rows"]["server_settings"] = [copy.deepcopy(fixture.snapshot["rows"]["server_settings"][0])]
            worker.initial["sequences"]["theme_owner_ids_id_seq"] = {"last_value": 1, "is_called": True}
            worker.initial["sequences"]["activity_entries_id_seq"] = {"last_value": 1, "is_called": False}
            if defect == "sequence":
                fixture.snapshot["sequences"]["theme_owner_ids_id_seq"]["last_value"] = 3
            elif defect == "registration metadata":
                fixture.snapshot["rows"]["item_metadata_state"][0]["automatic"] = {"unexplained": True}
            elif defect == "initial owner":
                worker.initial["rows"]["theme_owner_ids"][0]["id"] = 99

            def cleanup():
                worker.report["cleanup"] = {name: True for name in ("browser_terminal", "sessions_revoked", "backend_terminal", "mounts_removed")}

            with self.subTest(defect=defect), patch.object(worker, "admit"), patch.object(worker, "fixtures"), \
                    patch.object(worker, "start_backend"), patch.object(worker, "start_browser"), \
                    patch.object(worker, "workflow", side_effect=lambda: worker.stages.extend(OP.STAGES)), \
                    patch.object(worker, "cleanup", side_effect=cleanup), \
                    patch.object(worker.db, "snapshot", return_value=fixture.snapshot), \
                    patch.object(worker.db, "query", return_value=b"f" if defect == "defaults" else b"t"), \
                    patch.object(worker, "assert_worker_alone") as alone, patch.object(OP, "create_file") as published:
                self.assertEqual(worker.run_all(), 0 if defect is None else 1)
            self.assertEqual(worker.report["status"], "workflow_complete" if defect is None else "failed")
            self.assertEqual([call.args[0] for call in published.call_args_list],
                             ([worker.private / "final-database.json"] if defect is None else []) + [worker.private / "worker-result.json"])
            self.assertEqual(alone.call_count, int(defect is None))

    def controller_fixture(self):
        with patch.object(secrets, "token_hex", return_value="d" * 64):
            controller = OP.Controller(intent_fixture(), "e" * 64, {"selected": {}, "catalog": {}, "web": {}})
        controller.postgres = types.SimpleNamespace(pw_uid=113, pw_gid=117)
        controller.goby = types.SimpleNamespace(pw_uid=995, pw_gid=995)
        controller.hba_before = b"Exact prior HBA bytes.\n"
        controller.hba_after = b"Exact temporary HBA bytes.\n"
        controller.hba_intent = True
        return controller

    def test_real_hba_restore_accepts_only_the_recorded_before_or_after_bytes(self):
        for current in (b"foreign", b"Exact temporary HBA bytes.\nchanged"):
            controller = self.controller_fixture()
            with self.subTest(current=current), patch.object(OP, "read_file", return_value=current), \
                    patch.object(controller, "check_cluster") as cluster, patch.object(OP, "replace_file") as replaced, \
                    patch.object(controller, "reload_hba") as reloaded:
                self.reject(controller.restore_hba)
                cluster.assert_not_called()
                replaced.assert_not_called()
                reloaded.assert_not_called()
        for restore in (True, False):
            controller = self.controller_fixture()
            current = controller.hba_after if restore else controller.hba_before
            events = []
            with self.subTest(restore=restore), patch.object(OP, "read_file", return_value=current), \
                    patch.object(controller, "check_cluster", side_effect=lambda value: events.append(("cluster", value))), \
                    patch.object(OP, "replace_file", side_effect=lambda *args, **kwargs: events.append(("replace", args, kwargs))), \
                    patch.object(controller, "reload_hba", side_effect=lambda value: events.append(("reload", value))):
                controller.restore_hba()
            self.assertEqual(events[0], ("cluster", current))
            self.assertEqual(events[-1], ("reload", controller.hba_before))
            self.assertEqual(sum(event[0] == "replace" for event in events), int(restore))

    def test_real_worker_stop_refuses_an_unknown_invocation_or_process_identity(self):
        for defect in ("invocation", "unobserved process", "process changed"):
            controller = self.controller_fixture()
            controller.worker_started = True
            controller.worker_invocation = "a" * 32
            controller.worker_process = None if defect == "unobserved process" else {"pid": 301, "start_ticks": 10}
            state = {"LoadState": "loaded", "Description": controller.tag,
                     "ControlGroup": "/system.slice/" + controller.intent["worker_unit"],
                     "InvocationID": "b" * 32 if defect == "invocation" else "a" * 32,
                     "MainPID": "301", "ActiveState": "active"}
            with self.subTest(defect=defect), patch.object(OP, "unit_state", return_value=state), \
                    patch.object(OP, "process_fact", return_value={"pid": 301, "start_ticks": 11}), \
                    patch.object(OP, "command") as command:
                self.reject(controller.stop_worker)
                command.assert_not_called()

    def test_real_worker_dispatch_retains_the_exact_unit_after_its_main_process_exits(self):
        controller = self.controller_fixture()
        controller.output_identity = {"device": 1, "inode": 2}
        controller.worker_input_sha = "f" * 64
        with patch.object(OP, "directory", return_value=controller.output_identity), patch.object(OP, "verify_inputs"), \
                patch.object(controller, "check_cluster"), patch.object(controller.db, "verify_owned"), \
                patch.object(OP, "listener"), \
                patch.object(OP, "command", side_effect=OP.Failure("Stop at the in-memory launch boundary.")) as command:
            self.reject(controller.dispatch)
        command.assert_called_once()
        arguments = command.call_args.args[0]
        self.assertEqual(arguments.count("--remain-after-exit"), 1)
        self.assertIn("--service-type=exec", arguments)
        self.assertIn("--unit=" + controller.intent["worker_unit"], arguments)
        unshare = arguments.index("/usr/bin/unshare")
        self.assertEqual(arguments[unshare:unshare + 6],
                         ["/usr/bin/unshare", "--mount", "--fork", "--kill-child", "--propagation", "private"])

    def test_real_known_worker_emergency_stop_cannot_become_success(self):
        controller = self.controller_fixture()
        controller.worker_started = True
        controller.worker_invocation = "a" * 32
        controller.worker_process = {"pid": 301, "start_ticks": 10}
        state = {"LoadState": "loaded", "Description": controller.tag,
                 "ControlGroup": "/system.slice/" + controller.intent["worker_unit"],
                 "InvocationID": "a" * 32, "MainPID": "301", "ActiveState": "active"}
        with patch.object(OP, "unit_state", return_value=state), \
                patch.object(OP, "process_fact", return_value=controller.worker_process), patch.object(OP, "command") as command:
            self.reject(controller.stop_worker)
        command.assert_called_once_with(["/usr/bin/systemctl", "stop", controller.intent["worker_unit"]], timeout=40)

    def test_real_cleanup_restores_hba_despite_unknown_worker_and_retains_pair(self):
        controller = self.controller_fixture()
        controller.db.pair["phase"] = "database_tag_pending"
        events = []
        with patch.object(controller, "stop_worker", side_effect=OP.Failure("Unknown worker identity.")), \
                patch.object(controller, "restore_hba", side_effect=lambda: events.append("restore_hba")), \
                patch.object(controller.db, "dispose") as dispose:
            controller.cleanup()
        self.assertEqual(events, ["restore_hba"])
        self.assertEqual(controller.report["cleanup"], {"worker_terminal": False, "hba_restored": True})
        self.assertIs(controller.report["pair_retained"], True)
        dispose.assert_not_called()

    def test_real_cleanup_hba_failure_blocks_disposal_and_remains_visible(self):
        controller = self.controller_fixture()
        controller.db.pair["phase"] = "owned"
        controller.report["workflow_verified"] = True
        with patch.object(controller, "stop_worker"), \
                patch.object(controller, "restore_hba", side_effect=OP.Failure("Exact HBA restoration failed.")), \
                patch.object(controller.db, "dispose") as dispose:
            controller.cleanup()
        self.assertIs(controller.report["cleanup"]["hba_restored"], False)
        self.assertEqual(controller.report["cleanup_errors"]["hba_restored"], "Exact HBA restoration failed.")
        dispose.assert_not_called()

    def test_real_cleanup_preserves_historical_receipt_and_publishes_only_new_receipts(self):
        for changed in (False, True):
            controller = self.controller_fixture()
            controller.created = True
            controller.output_identity = {"device": 1, "inode": 2, "uid": 0, "gid": 0, "mode": 0o700}
            controller.db.pair["phase"] = "database_pending"
            controller.report["protected_before"] = [{"identity": "protected"}]
            controller.report["host_before"] = {"namespace": controller.intent["host_namespace"]}
            history = b"Immutable historical receipt.\n"
            controller.intent["history_sha256"] = OP.sha(history)
            writes = []
            with self.subTest(changed=changed), patch.object(controller, "stop_worker"), \
                    patch.object(controller, "restore_hba"), \
                    patch.object(OP, "read_file", return_value=history + (b"changed" if changed else b"")) as reader, \
                    patch.object(OP, "protected_facts", return_value=controller.report["protected_before"]), \
                    patch.object(OP, "host_fact", return_value=controller.report["host_before"]), \
                    patch.object(OP, "listener"), patch.object(OP, "verify_inputs"), \
                    patch.object(OP, "create_file", side_effect=lambda path, raw, **kwargs: writes.append((path, raw))), \
                    patch.object(OP, "replace_file") as replaced, patch.object(controller.db, "dispose") as dispose:
                controller.cleanup()
            reader.assert_called_once_with(OP.HISTORY, modes=(0o600,))
            self.assertIs(controller.report["cleanup"]["historical_receipt_unchanged"], not changed)
            self.assertEqual([path for path, _ in writes], [OUTPUT / "receipt-001.json"])
            self.assertEqual(OP.decode(writes[0][1])["pair"]["phase"], "database_pending")
            self.assertEqual(history, b"Immutable historical receipt.\n")
            replaced.assert_not_called()
            dispose.assert_not_called()

    def test_real_cleanup_disposes_only_after_worker_and_hba_proofs_then_rechecks_preservation(self):
        controller = self.controller_fixture()
        controller.created = True
        controller.output_identity = {"device": 1, "inode": 2, "uid": 0, "gid": 0, "mode": 0o700}
        controller.db.pair["phase"] = "owned"
        controller.report.update(workflow_verified=True, protected_before=[{"identity": "protected"}],
                                 host_before={"namespace": controller.intent["host_namespace"]})
        controller.before_catalog = {}
        controller.final_snapshot = {"rows": {}, "sequences": {}}
        history = b"Immutable historical receipt.\n"
        controller.intent["history_sha256"] = OP.sha(history)
        events = []

        def dispose(snapshot):
            self.assertIs(snapshot, controller.final_snapshot)
            events.append("dispose")
            controller.db.pair["phase"] = "removed"

        with patch.object(controller, "stop_worker", side_effect=lambda: events.append("worker")), \
                patch.object(controller, "restore_hba", side_effect=lambda: events.append("hba")), \
                patch.object(controller.db, "dispose", side_effect=dispose), \
                patch.object(controller.db, "maintenance", side_effect=lambda sql: events.append("catalog") or b"{}"), \
                patch.object(controller, "remove_runtime", side_effect=lambda: events.append("runtime")), \
                patch.object(OP, "read_file", return_value=history), \
                patch.object(OP, "protected_facts", return_value=controller.report["protected_before"]), \
                patch.object(OP, "host_fact", return_value=controller.report["host_before"]), \
                patch.object(OP, "listener", side_effect=lambda: events.append("port")), \
                patch.object(OP, "verify_inputs", side_effect=lambda intent: events.append("inputs")), \
                patch.object(OP, "create_file", side_effect=lambda path, raw, **kwargs: events.append(("write", path))):
            controller.cleanup()
        self.assertEqual(events[:4], ["worker", "hba", "dispose", "catalog"])
        self.assertIn(("write", OUTPUT / "catalog-after.json"), events)
        self.assertEqual(events[-1], ("write", OUTPUT / "receipt-001.json"))
        self.assertEqual(set(controller.report["cleanup"]), {
            "worker_terminal", "hba_restored", "pair_removed", "global_catalog_unchanged", "runtime_removed",
            "historical_receipt_unchanged", "protected_unchanged", "host_unchanged", "port_closed", "inputs_unchanged",
        })
        self.assertTrue(all(value is True for value in controller.report["cleanup"].values()))

    def test_real_controller_success_remains_a_proposal_for_outer_attestation(self):
        controller = self.controller_fixture()
        controller.created = True
        required = {"worker_terminal", "hba_restored", "pair_removed", "global_catalog_unchanged", "runtime_removed",
                    "historical_receipt_unchanged", "protected_unchanged", "host_unchanged", "port_closed", "inputs_unchanged"}

        def cleanup():
            controller.report["cleanup"] = {key: True for key in required}

        with patch.object(controller, "admit"), patch.object(controller, "provision"), \
                patch.object(controller, "dispatch"), patch.object(controller, "cleanup", side_effect=cleanup), \
                patch.object(OP, "create_file") as published:
            self.assertEqual(controller.run_all(), 0)
        self.assertEqual(controller.report["status"], "awaiting_outer_attestation")
        published.assert_called_once_with(OUTPUT / "report.json", OP.canonical(controller.report))

    def test_real_controller_failure_runs_cleanup_once_without_replay(self):
        for failing_stage in ("admit", "provision", "dispatch"):
            controller = self.controller_fixture()
            events = []

            def stage(name):
                def invoke():
                    events.append(name)
                    if name == failing_stage:
                        raise OP.Failure("The selected boundary rejected this run.")
                return invoke

            with self.subTest(stage=failing_stage), patch.object(controller, "admit", side_effect=stage("admit")), \
                    patch.object(controller, "provision", side_effect=stage("provision")), \
                    patch.object(controller, "dispatch", side_effect=stage("dispatch")), \
                    patch.object(controller, "cleanup", side_effect=lambda: events.append("cleanup")):
                self.assertEqual(controller.run_all(), 1)
            expected = ["admit", "provision", "dispatch"][:["admit", "provision", "dispatch"].index(failing_stage) + 1]
            self.assertEqual(events, expected + ["cleanup"])
            self.assertEqual(controller.report["status"], "failed")

    def admission_fixture(self):
        controller = self.controller_fixture()
        owner = {"process": {"pid": 701, "start_ticks": 99}, "system_identifier": "123456789"}
        raw = OP.canonical(owner)
        state = {"Description": controller.tag, "ControlGroup": "/system.slice/" + controller.intent["controller_unit"],
                 "InvocationID": "a" * 32, "MainPID": "501", "ActiveState": "active", "MemoryMax": str(2 << 30),
                 "MemorySwapMax": "0", "CPUQuotaPerSecUSec": "1.500000s", "KillMode": "control-group", "RemainAfterExit": "yes"}
        module = types.SimpleNamespace(
            OWNER=OP.CONTROL / "owner.json", HBA=controller.hba_before.decode(), CONF="Exact PostgreSQL configuration.\n",
            verify_parents=Mock(), acquire_lock=Mock(return_value=811), binaries=Mock(return_value={}),
            validate_owner=Mock(), expected_owner=Mock(return_value={}), directory=Mock(return_value={}),
            verify_directories=Mock(), verify_configuration=Mock(), verify_process=Mock(return_value=owner["process"]),
            cluster_identifier=Mock(return_value=owner["system_identifier"]), verify_server=Mock(),
        )
        module.read_file = Mock(side_effect=lambda path, **kwargs:
                                module.CONF if path == OP.DATA / "postgresql.conf" else "# Empty override.\n")
        return types.SimpleNamespace(controller=controller, owner=owner, owner_raw=raw, state=state, module=module,
                                     host={"namespace": controller.intent["host_namespace"], "mountinfo_sha256": "c" * 64},
                                     process={"pid": 501, "start_ticks": 11,
                                              "cgroup": "/system.slice/" + controller.intent["controller_unit"]},
                                     occupied=set(), worker_state={"LoadState": "not-found"}, events=[],
                                     port_error=False, rows={"role": None, "database": None})

    @contextlib.contextmanager
    def admission_effects(self, fixture):
        import pwd
        controller = fixture.controller

        def reader(path, **options):
            fixture.events.append(("read", path))
            if path == fixture.module.OWNER:
                return fixture.owner_raw
            self.assertEqual(path, OP.HBA)
            return controller.hba_before

        def listen():
            fixture.events.append(("listener",))
            if fixture.port_error:
                raise OP.Failure("The fixed port is occupied.")

        with contextlib.ExitStack() as patches:
            patches.enter_context(patch.object(sys, "platform", "linux"))
            patches.enter_context(patch.object(os, "geteuid", return_value=0))
            patches.enter_context(patch.object(os, "getpid", return_value=501))
            patches.enter_context(patch.object(os, "environ", {"SSH_CONNECTION": "authorized-memory-fixture"}))
            patches.enter_context(patch.object(OP, "host_fact", side_effect=lambda: copy.deepcopy(fixture.host)))
            patches.enter_context(patch.object(OP, "unit_state", side_effect=lambda unit:
                                              fixture.state if unit == controller.intent["controller_unit"] else fixture.worker_state))
            patches.enter_context(patch.object(OP, "process_fact", return_value=fixture.process))
            patches.enter_context(patch.object(OP, "directory", side_effect=lambda path, **kwargs:
                                              fixture.events.append(("directory", path, kwargs)) or {}))
            patches.enter_context(patch.object(OP, "present", side_effect=lambda path: path in fixture.occupied))
            patches.enter_context(patch.object(OP, "listener", side_effect=listen))
            patches.enter_context(patch.object(OP, "protected_facts", return_value=[{"protected": "unchanged"}]))
            patches.enter_context(patch.object(OP, "load_workspace", side_effect=lambda selected:
                                              fixture.events.append(("workspace",)) or fixture.module))
            patches.enter_context(patch.object(pwd, "getpwnam", side_effect=lambda name:
                                              controller.postgres if name == "postgres" else controller.goby))
            patches.enter_context(patch.object(OP, "read_file", side_effect=reader))
            patches.enter_context(patch.object(controller.db, "rows", side_effect=lambda: fixture.rows))
            patches.enter_context(patch.object(controller.db, "maintenance", return_value=b"{}"))
            yield

    def test_real_controller_admission_completes_read_checks_without_new_artifacts(self):
        fixture = self.admission_fixture()
        with self.admission_effects(fixture), patch.object(OP, "create_file") as created, \
                patch.object(OP, "replace_file") as replaced, patch.object(Path, "mkdir") as mkdir:
            fixture.controller.admit()
            created.assert_not_called()
            replaced.assert_not_called()
            mkdir.assert_not_called()
        self.assertEqual(fixture.controller.report["controller"]["invocation_id"], "a" * 32)
        self.assertIn(("directory", Path("/opt"), {"mode": 0o755}), fixture.events)
        fixture.module.acquire_lock.assert_called_once()
        self.assertEqual(fixture.controller.before_catalog, {})

    def test_real_controller_admission_refuses_collision_or_host_unit_drift_before_workspace(self):
        for defect in ("output", "runtime", "worker", "port", "namespace", "pid", "tag", "memory", "retention", "cgroup"):
            fixture = self.admission_fixture()
            if defect in ("output", "runtime"):
                fixture.occupied.add(getattr(fixture.controller, defect))
            elif defect == "worker":
                fixture.worker_state = {"LoadState": "loaded"}
            elif defect == "port":
                fixture.port_error = True
            elif defect == "namespace":
                fixture.host["namespace"] = "mnt:[999]"
            elif defect == "pid":
                fixture.state["MainPID"] = "502"
            elif defect == "tag":
                fixture.state["Description"] = "historical-controller"
            elif defect == "memory":
                fixture.state["MemoryMax"] = "infinity"
            elif defect == "retention":
                fixture.state["RemainAfterExit"] = "no"
            else:
                fixture.process["cgroup"] = "/system.slice/foreign.service"
            with self.subTest(defect=defect), self.admission_effects(fixture), \
                    patch.object(OP, "create_file") as created, patch.object(OP, "replace_file") as replaced, \
                    patch.object(Path, "mkdir") as mkdir:
                self.reject(fixture.controller.admit)
                created.assert_not_called()
                replaced.assert_not_called()
                mkdir.assert_not_called()
            self.assertNotIn(("workspace",), fixture.events)
            fixture.module.acquire_lock.assert_not_called()

    def test_real_controller_admission_refuses_new_pair_collision_after_cluster_check(self):
        fixture = self.admission_fixture()
        fixture.rows = {"role": {"oid": 77, "tag": "historical"}, "database": None}
        with self.admission_effects(fixture), patch.object(OP, "create_file") as created, \
                patch.object(Path, "mkdir") as mkdir:
            self.reject(fixture.controller.admit)
            created.assert_not_called()
            mkdir.assert_not_called()
        fixture.module.verify_server.assert_called_once()

    def test_real_unit_state_rejects_historical_units_before_systemctl(self):
        with patch.object(OP, "command") as command:
            for unit in ("goby-client-m3e.service", "goby-foundation-test.service", "goby-client-backup-old.service",
                         "goby-storage-binding-live-ui-worker-foreign.service"):
                with self.subTest(unit=unit):
                    self.reject(OP.unit_state, unit)
            command.assert_not_called()

    def test_real_terminal_reader_requires_independent_invocation_and_empty_cgroup(self):
        controller = self.controller_fixture()
        unit = controller.intent["controller_unit"]
        baseline = {"Description": controller.tag, "ControlGroup": "/system.slice/" + unit, "InvocationID": "a" * 32,
                    "MainPID": "0", "RemainAfterExit": "yes", "ActiveState": "active", "SubState": "exited",
                    "Result": "success", "ExecMainStatus": "0"}
        for field, value in (("InvocationID", "b" * 32), ("MainPID", "501"), ("ActiveState", "inactive"),
                             ("SubState", "running"), ("SubState", "dead"), ("RemainAfterExit", "no"),
                             ("Result", "failed"), ("ExecMainStatus", "1")):
            state = dict(baseline, **{field: value})
            with self.subTest(field=field), patch.object(OP, "unit_state", return_value=state), \
                    patch.object(OP, "cgroup_empty") as cgroup:
                self.reject(OP.unit_terminal, unit, controller.tag, "a" * 32)
                cgroup.assert_not_called()
        with patch.object(OP, "unit_state", return_value=baseline), \
                patch.object(OP, "cgroup_empty", side_effect=OP.Failure("A descendant still contains a process.")):
            self.reject(OP.unit_terminal, unit, controller.tag, "a" * 32)
        with patch.object(OP, "unit_state", return_value=baseline), patch.object(OP, "cgroup_empty") as cgroup:
            self.assertEqual(OP.unit_terminal(unit, controller.tag, "a" * 32), baseline)
            cgroup.assert_called_once_with(unit)

    def test_real_cgroup_reader_checks_root_and_every_nested_process_file(self):
        unit = intent_fixture()["worker_unit"]
        root = Path("/sys/fs/cgroup/system.slice") / unit
        files = [root / "cgroup.procs", root / "browser/cgroup.procs", root / "browser/renderer/cgroup.procs"]
        reads = []
        with patch.object(OP, "present", return_value=True), patch.object(Path, "is_dir", return_value=True), \
                patch.object(Path, "is_symlink", return_value=False), patch.object(Path, "rglob", return_value=files) as inventory, \
                patch.object(Path, "read_text", side_effect=lambda *args, **kwargs: reads.append(True) or ""):
            OP.cgroup_empty(unit)
        inventory.assert_called_once_with("cgroup.procs")
        self.assertEqual(len(reads), 3)
        with patch.object(OP, "present", return_value=False), patch.object(Path, "rglob") as inventory:
            OP.cgroup_empty(unit)
            inventory.assert_not_called()

    def test_real_cgroup_reader_rejects_nested_processes_links_and_incomplete_inventory(self):
        unit = intent_fixture()["worker_unit"]
        root = Path("/sys/fs/cgroup/system.slice") / unit
        leaf = root / "browser/renderer/cgroup.procs"
        for defect in ("root type", "root link", "leaf link", "process", "empty", "oversize"):
            files = [] if defect == "empty" else [root / str(index) / "cgroup.procs" for index in range(513)] if defect == "oversize" else [root / "cgroup.procs", leaf]

            def linked(path):
                return (defect == "root link" and path == root) or (defect == "leaf link" and path == leaf)

            def read(path, *args, **kwargs):
                return "771\n" if defect == "process" and path == leaf else ""

            with self.subTest(defect=defect), patch.object(OP, "present", return_value=True), \
                    patch.object(Path, "is_dir", return_value=defect != "root type"), \
                    patch.object(Path, "is_symlink", linked), patch.object(Path, "rglob", return_value=files), \
                    patch.object(Path, "read_text", read):
                self.reject(OP.cgroup_empty, unit)

    def worker_request_fixture(self):
        database = self.database_fixture()
        return {"marker": OP.MARKER, "run_id": RUN, "intent_sha256": "e" * 64, "pair": database.db.pair,
                "password": "d" * 64, "goby_gid": 995,
                "runtime_identity": {"device": 7, "inode": 22, "uid": 0, "gid": 995, "mode": 0o750},
                "output_identity": {"device": 7, "inode": 23, "uid": 0, "gid": 0, "mode": 0o700},
                "controller": {"unit": intent_fixture()["controller_unit"], "invocation_id": "a" * 32,
                               "process": {"pid": 501, "start_ticks": 11}},
                "cluster": {"pid": 701, "start_ticks": 99}}

    def worker_fixture(self, request=None):
        request = self.worker_request_fixture() if request is None else request
        raw = OP.canonical(request)
        with patch.object(OP, "read_file", return_value=raw), patch.object(secrets, "token_hex", return_value="d" * 64):
            return OP.Worker(intent_fixture(), "e" * 64, {"selected": {}, "catalog": {}, "web": {}}, OP.sha(raw))

    def test_real_worker_constructor_rejects_unpinned_or_foreign_requests_before_allocating_secrets(self):
        baseline = self.worker_request_fixture()
        changes = [("marker", "goby-client-backup-pair-m3e-v1"), ("run_id", "20260912_053517_9cb0074fc731"),
                   ("intent_sha256", "f" * 64), ("password", "unsafe"), ("password", None)]
        for field, value in changes:
            request = copy.deepcopy(baseline)
            request[field] = value
            raw = OP.canonical(request)
            with self.subTest(field=field, value=value), patch.object(OP, "read_file", return_value=raw), \
                    patch.object(secrets, "token_hex") as secret:
                self.reject(OP.Worker, intent_fixture(), "e" * 64, {}, OP.sha(raw))
                secret.assert_not_called()
        raw = OP.canonical(baseline)
        with patch.object(OP, "read_file", return_value=raw), patch.object(secrets, "token_hex") as secret:
            self.reject(OP.Worker, intent_fixture(), "e" * 64, {}, "0" * 64)
            secret.assert_not_called()

    def test_real_worker_constructor_rejects_missing_extra_or_historical_document_shape(self):
        baseline = self.worker_request_fixture()
        changes = [None, [], {"marker": "goby-client-backup-pair-m3e-v1", "phase": "finished"},
                   dict(baseline, resume=True)]
        for field in baseline:
            changed = copy.deepcopy(baseline)
            del changed[field]
            changes.append(changed)
        for request in changes:
            raw = OP.canonical(request)
            with self.subTest(request=request), patch.object(OP, "read_file", return_value=raw), \
                    patch.object(secrets, "token_hex") as secret:
                self.reject(OP.Worker, intent_fixture(), "e" * 64, {}, OP.sha(raw))
                secret.assert_not_called()

    def test_real_worker_constructor_rejects_retained_partial_or_malformed_owned_pair(self):
        changes = [("phase", phase) for phase in ("retained", "finished", "removed", "closed", "database_pending")]
        changes += [("role_oid", value) for value in (None, True, 0, "17001")]
        changes += [("database_oid", value) for value in (None, True, 0, "18001")]
        for field, value in changes:
            request = self.worker_request_fixture()
            request["pair"][field] = value
            raw = OP.canonical(request)
            with self.subTest(field=field, value=value), patch.object(OP, "read_file", return_value=raw), \
                    patch.object(secrets, "token_hex") as secret:
                self.reject(OP.Worker, intent_fixture(), "e" * 64, {}, OP.sha(raw))
                secret.assert_not_called()
        for gid in (None, True, 0, "995"):
            request = self.worker_request_fixture()
            request["goby_gid"] = gid
            raw = OP.canonical(request)
            with self.subTest(gid=gid), patch.object(OP, "read_file", return_value=raw), \
                    patch.object(secrets, "token_hex") as secret:
                self.reject(OP.Worker, intent_fixture(), "e" * 64, {}, OP.sha(raw))
                secret.assert_not_called()

    def test_real_worker_environment_contains_only_the_new_application_scope(self):
        worker = self.worker_fixture()
        with patch.object(os, "environ", {"PGHOST": "foreign", "PGPASSWORD": "maintenance-secret",
                                         "GOBY_DATABASE_URL": "foreign", "GOBY_RECOVERY_DATABASE_URL": "foreign",
                                         "GOBY_UNLISTED_SETTING": "foreign"}):
            environment = worker.environment()
        self.assertEqual(environment["GOBY_LISTEN"], "127.0.0.1:18288")
        self.assertEqual(environment["GOBY_PUBLIC_URL"], "http://127.0.0.1:18288")
        self.assertEqual(environment["GOBY_DATABASE_URL"],
                         "postgresql://goby_binding_ui_r_" + SUFFIX + ":" + "d" * 64 +
                         "@127.0.0.1:15432/goby_binding_ui_" + SUFFIX + "?sslmode=disable")
        self.assertEqual(environment["GOBY_RECOVERY_DATABASE_URL"], "")
        self.assertEqual(environment["GOBY_TRUSTED_PROXIES"], "")
        self.assertEqual(environment["GOBY_WEB_DIR"], str(RUNTIME / "web"))
        self.assertFalse(any(key.startswith("PG") for key in environment))
        self.assertNotIn("GOBY_UNLISTED_SETTING", environment)
        for name, suffix in (("GOBY_MEDIA_ROOTS", "media"), ("GOBY_API_KEY_MASTER_KEY_FILE", "master.key"),
                             ("GOBY_RECOVERY_STATE_DIR", "recovery"), ("GOBY_RECOVERY_OPERATIONS_DIR", "operations"),
                             ("GOBY_BACKUP_DIR", "backups"), ("GOBY_LOG_DIR", "diagnostics"),
                             ("GOBY_TRANSCODE_CACHE", "cache"), ("TMPDIR", "tmp")):
            self.assertEqual(environment[name], str(RUNTIME / "app" / suffix))
        self.assertNotIn("maintenance-secret", OP.canonical(environment).decode())

    def test_real_worker_admission_rejects_shared_namespace_changed_root_or_unit_before_mkdir(self):
        import pwd
        for defect in ("namespace", "cgroup", "output", "runtime", "unit", "budget", "retention"):
            worker = self.worker_fixture()
            state = {"Description": OP.MARKER + ":" + RUN, "InvocationID": "a" * 32,
                     "ControlGroup": "/system.slice/" + worker.intent["worker_unit"], "MemoryMax": str(2 << 30),
                     "MemorySwapMax": "0", "CPUQuotaPerSecUSec": "1.500000s", "KillMode": "control-group", "RemainAfterExit": "yes"}
            process = {"namespace": "mnt:[4026532999]", "cgroup": "/system.slice/" + worker.intent["worker_unit"]}
            if defect == "namespace":
                process["namespace"] = worker.intent["host_namespace"]
            elif defect == "cgroup":
                process["cgroup"] = "/system.slice/foreign.service"
            elif defect == "unit":
                state["Description"] = "historical-unit"
            elif defect == "budget":
                state["MemoryMax"] = "infinity"
            elif defect == "retention":
                state["RemainAfterExit"] = "no"

            def directory(path, *args):
                name = "output" if path == OUTPUT else "runtime"
                if name == "runtime":
                    self.assertEqual(args, (0, 995, 0o750))
                return {} if defect == name else worker.request[name + "_identity"]

            with self.subTest(defect=defect), patch.object(sys, "platform", "linux"), \
                    patch.object(os, "geteuid", return_value=0), patch.object(os, "getpid", return_value=601), \
                    patch.object(pwd, "getpwnam", return_value=types.SimpleNamespace(pw_uid=995, pw_gid=995)), \
                    patch.object(OP, "directory", side_effect=directory), patch.object(OP, "process_fact", return_value=process), \
                    patch.object(OP, "unit_state", return_value=state), patch.object(worker.db, "verify_owned") as database, \
                    patch.object(OP, "listener") as listener, patch.object(Path, "mkdir") as mkdir:
                self.reject(worker.admit)
                database.assert_not_called()
                listener.assert_not_called()
                mkdir.assert_not_called()

    def test_real_worker_ramfs_mount_uses_only_the_empty_readonly_owned_root(self):
        worker = self.worker_fixture()
        worker.gid = 986
        target = Path(worker.paths["U"])
        before = metadata(mode=stat.S_IFDIR | 0o550, gid=986)
        worker.directory_ids[str(target)] = OP.identity(before)
        observed = {"id": "42", "target": str(target), "filesystem": "ramfs"}
        state = {"mounted": False, "uid": 0, "gid": 0, "mode": 0o755}
        events = []

        def command(arguments, **options):
            events.append(("mount", arguments, options))
            state["mounted"] = True

        def chown(path, uid, gid):
            self.assertTrue(state["mounted"])
            events.append(("chown", path, uid, gid))
            state.update(uid=uid, gid=gid)

        def chmod(path, mode):
            events.append(("chmod", path, mode))
            state["mode"] = mode

        def path_info(path):
            self.assertEqual(path, target)
            return metadata(mode=stat.S_IFDIR | state["mode"], uid=state["uid"], gid=state["gid"], inode=92, device=8)

        with patch.object(worker, "mount_info", side_effect=lambda _path: observed if state["mounted"] else None), \
                patch.object(Path, "lstat", return_value=before), patch.object(Path, "iterdir", return_value=iter(())), \
                patch.object(OP, "command", side_effect=command), patch.object(os, "chown", side_effect=chown), \
                patch.object(os, "chmod", side_effect=chmod), patch.object(OP, "path_info", side_effect=path_info):
            worker.mount(target)
        self.assertEqual(events, [
            ("mount", ["/usr/bin/mount", "-t", "ramfs", "-o", "nosuid,nodev,noexec", "binding-ui-" + RUN, target], {"timeout": 5}),
            ("chown", target, 0, 986), ("chmod", target, 0o550)])
        self.assertEqual(worker.mounts, {str(target): observed})
        self.assertEqual(worker.directory_ids[str(target)], OP.identity(before))

    def test_real_worker_ramfs_mount_rejects_unowned_failed_or_nonempty_observations(self):
        for defect in ("target", "command", "filesystem", "metadata", "contents"):
            worker = self.worker_fixture()
            target = Path(worker.paths["A"] if defect == "target" else worker.paths["U"])
            before = metadata(mode=stat.S_IFDIR | 0o550, gid=worker.gid)
            worker.directory_ids[str(target)] = OP.identity(before)
            observed = {"id": "42", "target": str(target), "filesystem": "tmpfs" if defect == "filesystem" else "ramfs"}
            after = metadata(mode=stat.S_IFDIR | (0o750 if defect == "metadata" else 0o550), gid=worker.gid, inode=92, device=8)
            children = (target / "unexpected",) if defect == "contents" else ()
            with self.subTest(defect=defect), patch.object(worker, "mount_info", side_effect=[None, observed]), \
                    patch.object(Path, "lstat", return_value=before), patch.object(Path, "iterdir", return_value=iter(children)), \
                    patch.object(OP, "path_info", return_value=after), patch.object(OP, "command", \
                    side_effect=OP.Failure("Mount failed.") if defect == "command" else None) as command, \
                    patch.object(os, "chown") as chown, patch.object(os, "chmod") as chmod:
                self.reject(worker.mount, target)
            self.assertEqual(worker.mounts, {})
            if defect == "target":
                command.assert_not_called()
            if defect in ("target", "command", "filesystem"):
                chown.assert_not_called()
                chmod.assert_not_called()

    def test_real_worker_unmount_refuses_an_unknown_or_changed_mount_without_command(self):
        for defect in ("unrecorded", "changed"):
            worker = self.worker_fixture()
            target = RUNTIME / "app/media/A/archive"
            if defect == "changed":
                worker.mounts[str(target)] = {"id": "41", "target": str(target)}
            with self.subTest(defect=defect), patch.object(worker, "mount_info", return_value={"id": "42"}), \
                    patch.object(OP, "command") as command:
                self.reject(worker.unmount, target)
                command.assert_not_called()

    def test_real_ipc_reader_refuses_unknown_hardlinks_and_bounds_published_bytes(self):
        path = OUTPUT / "private/ipc/001-browser_ready.json"
        with patch.object(OP, "present", return_value=True), patch.object(Path, "lstat", return_value=metadata(nlink=1)), \
                patch.object(OP, "read_file", return_value=b"{}") as reader:
            self.assertEqual(OP.read_ipc(path), b"{}")
            reader.assert_called_once_with(path, modes=(0o600,), limit=512 << 10)
        with patch.object(OP, "present", side_effect=[True, False]), \
                patch.object(Path, "lstat", return_value=metadata(nlink=2)), patch.object(OP, "read_file") as reader:
            self.reject(OP.read_ipc, path)
            reader.assert_not_called()
        with patch.object(OP, "present", return_value=False), patch.object(OP, "read_file") as reader:
            self.assertIsNone(OP.read_ipc(path))
            reader.assert_not_called()

    def test_real_workflow_missing_request_fails_without_stage_or_ack_replay(self):
        worker = self.worker_fixture()
        worker.browser = Mock()
        worker.browser.poll.return_value = None
        with patch.object(time, "monotonic", side_effect=[0, 0, 241]), patch.object(time, "sleep"), \
                patch.object(OP, "read_ipc", return_value=None), patch.object(worker, "stage") as stage, \
                patch.object(OP, "write_ipc") as write:
            self.reject(worker.workflow)
        stage.assert_not_called()
        write.assert_not_called()
        self.assertEqual(worker.stages, [])

    def test_real_workflow_ack_publication_failure_never_replays_a_completed_control_action(self):
        worker = self.worker_fixture()
        events = []
        requests = {worker.private / "ipc" / ("%02d-%s.request.json" % (index + 1, action)):
                    OP.canonical({"marker": OP.IPC_MARKER, "run_id": RUN, "nonce": worker.intent["nonce"],
                                  "sequence": index + 1, "action": action, "payload": {}})
                    for index, action in enumerate(OP.STAGES)}

        def stage(index, payload):
            events.append(("stage", index))
            return {"owned_observation": index}

        def write(path, value):
            events.append(("ack", value["sequence"]))
            if value["sequence"] == 5:
                raise OP.Failure("The acknowledgement could not be committed.")

        with patch.object(time, "monotonic", return_value=0), \
                patch.object(OP, "read_ipc", side_effect=lambda path: requests[path]), \
                patch.object(worker, "stage", side_effect=stage), patch.object(worker, "phase_alive"), \
                patch.object(OP, "write_ipc", side_effect=write):
            self.reject(worker.workflow)
        self.assertEqual(events, [event for index in range(5) for event in (("stage", index), ("ack", index + 1))])
        self.assertEqual(worker.stages, list(OP.STAGES[:4]))

    def test_real_workflow_rejects_stale_wrong_nonce_or_reordered_request_before_stage(self):
        for field, value in (("marker", "historical"), ("run_id", "20260912_053517_9cb0074fc731"),
                             ("nonce", "f" * 64), ("sequence", True), ("sequence", 2), ("action", OP.STAGES[1])):
            worker = self.worker_fixture()
            request = {"marker": OP.IPC_MARKER, "run_id": RUN, "nonce": worker.intent["nonce"],
                       "sequence": 1, "action": OP.STAGES[0], "payload": {}}
            request[field] = value
            with self.subTest(field=field), patch.object(time, "monotonic", return_value=0), \
                    patch.object(OP, "read_ipc", return_value=OP.canonical(request)), \
                    patch.object(worker, "stage") as stage, patch.object(OP, "write_ipc") as write:
                self.reject(worker.workflow)
            stage.assert_not_called()
            write.assert_not_called()
            self.assertEqual(worker.stages, [])

    def test_real_http_rejects_non_native_routes_and_queries_before_connection(self):
        worker = self.worker_fixture()
        worker.library_id = "1" * 32
        worker.root_ids = {"A": "2" * 32, "B": "3" * 32, "U": "4" * 32}
        prefix = "/admin/v1/libraries/" + worker.library_id
        routes = ("http://127.0.0.1:15432/", "/admin/v1/backup", "/admin/v1/session?x=1",
                  prefix + "/roots-other", prefix + "/roots/" + "9" * 32 + "/binding",
                  worker.binding_route("A") + "?revision=1", worker.binding_route("A") + "/extra")
        with patch.object(http.client, "HTTPConnection") as connection, patch.object(worker, "pin_backend") as pin:
            for route in routes:
                with self.subTest(route=route):
                    self.reject(worker.http, "GET", route)
            connection.assert_not_called()
            pin.assert_not_called()

    def test_real_http_uncertain_put_response_cannot_replay_approval(self):
        worker = self.worker_fixture()
        worker.library_id = "1" * 32
        worker.root_ids = {"A": "2" * 32, "B": "3" * 32, "U": "4" * 32}
        token = base64.urlsafe_b64encode(b"t" * 32).rstrip(b"=").decode()
        worker.controller_token = token
        worker.stages = list(OP.STAGES[:4])
        connection = Mock()
        connection.getresponse.side_effect = OSError("The response was lost.")
        body = {"Revision": "1", "ObservedFingerprint": "f" * 64, "AcknowledgeMissingRemoval": True}
        with patch.object(worker, "pin_backend"), patch.object(http.client, "HTTPConnection", return_value=connection):
            with self.assertRaises(OSError):
                worker.http("PUT", worker.binding_route("A"), body=body, token=token)
            self.reject(worker.http, "PUT", worker.binding_route("A"), body=body, token=token)
        self.assertIs(worker.put_attempted, True)
        connection.request.assert_called_once()
        connection.close.assert_called_once()
        self.assertEqual(connection.request.call_args.kwargs["body"], OP.canonical(body))
        self.assertEqual(connection.request.call_args.kwargs["headers"]["Origin"], OP.ORIGIN)
        self.assertEqual(connection.request.call_args.kwargs["headers"]["X-CSRF-Token"], OP.csrf(token))

    def test_real_http_uncertain_login_preserves_provisional_cookie_and_cannot_replay(self):
        worker = self.worker_fixture()
        token = base64.urlsafe_b64encode(b"t" * 32).rstrip(b"=").decode()
        response = Mock(status=200)
        response.getheaders.return_value = [("Set-Cookie", "goby_session=" + token + "; Path=/admin; HttpOnly; SameSite=Strict")]
        response.read.side_effect = OSError("The login response body was lost.")
        connection = Mock()
        connection.getresponse.return_value = response
        with patch.object(worker, "pin_backend"), patch.object(http.client, "HTTPConnection", return_value=connection), \
                patch.object(OP, "create_file") as created:
            with self.assertRaises(OSError):
                worker.http("POST", "/admin/v1/session", body={"Name": "new-admin", "Password": "private"}, login=True)
            self.reject(worker.http, "POST", "/admin/v1/session", body={"Name": "new-admin", "Password": "private"}, login=True)
        self.assertIs(worker.login_attempted, True)
        self.assertEqual(worker.controller_token, token)
        created.assert_called_once_with(worker.private / "controller-session-provisional.json", OP.canonical({"token": token}))
        connection.request.assert_called_once()
        connection.close.assert_called_once()

    def test_real_session_cleanup_missing_delete_ack_still_checks_exact_cookie_rejection(self):
        worker = self.worker_fixture()
        token = base64.urlsafe_b64encode(b"t" * 32).rstrip(b"=").decode()
        worker.browser_token = token
        worker.backend = Mock()
        row = {"token_hash": "\\x" + OP.sha(token.encode()), "revoked_at": None, "kind": "admin"}
        events = []

        def http(method, route, **options):
            events.append((method, route, options))
            if method == "DELETE":
                raise OP.Failure("A revocation acknowledgement was lost.")
            return 401, None

        with patch.object(worker, "pin_backend"), patch.object(worker.db, "snapshot", return_value={"rows": {"sessions": [row]}}), \
                patch.object(worker, "prove_session") as proof, patch.object(worker, "http", side_effect=http):
            self.reject(worker.revoke_sessions)
        self.assertEqual([event[0] for event in events], ["DELETE", "GET"])
        self.assertEqual(events[-1], ("GET", "/admin/v1/session", {"token": token, "expected": (401,)}))
        self.assertEqual(proof.call_args_list[-1].kwargs, {"revoked": True})

    def test_real_worker_cleanup_unknown_backend_retains_mounts_and_reports_failure(self):
        worker = self.worker_fixture()
        worker.backend = Mock(name="backend")
        worker.browser = Mock(name="browser")
        worker.mounts[str(RUNTIME / "app/media/U")] = {"id": "41"}
        events = []

        def stop(child, fact):
            events.append("browser" if child is worker.browser else "backend")
            if child is worker.backend:
                raise OP.Failure("An unknown backend cannot be stopped.")

        with patch.object(worker, "stop_child", side_effect=stop), \
                patch.object(worker, "revoke_sessions", side_effect=lambda: events.append("sessions")), \
                patch.object(worker, "restore_permissions", side_effect=lambda: events.append("permissions")), \
                patch.object(worker, "unmount") as unmount:
            worker.cleanup()
        self.assertEqual(events, ["browser", "sessions", "permissions", "backend"])
        self.assertIs(worker.report["cleanup"]["backend_terminal"], False)
        self.assertIs(worker.report["cleanup"]["mounts_removed"], False)
        unmount.assert_not_called()

    def test_real_child_stop_refuses_changed_process_witness_before_opening_pidfd(self):
        for field, value in (("pid", 602), ("start_ticks", 12), ("boot_id", "changed"),
                             ("cgroup", "/system.slice/foreign.service"), ("exe", "/foreign"), ("sha256", "f" * 64)):
            worker = self.worker_fixture()
            child = Mock(pid=601, returncode=0)
            child.poll.return_value = None
            fact = {"pid": 601, "start_ticks": "11", "boot_id": "owned-boot", "sha256": "a" * 64,
                    "exe": str(RUNTIME / "goby"), "cgroup": "/system.slice/" + worker.intent["worker_unit"]}
            current = dict(fact, start_ticks=11, **({} if field == "start_ticks" else {field: value}))
            if field == "start_ticks":
                current[field] = value
            with self.subTest(field=field), patch.object(OP, "process_fact", return_value=current), \
                    patch.object(os, "pidfd_open") as pidfd, patch.object(signal, "pidfd_send_signal") as signal_child:
                self.reject(worker.stop_child, child, fact)
                pidfd.assert_not_called()
                signal_child.assert_not_called()

    @contextlib.contextmanager
    def main_effects(self):
        with patch.object(sys, "platform", "linux"), patch.object(os, "geteuid", return_value=0), \
                patch.object(os, "environ", {"SSH_CONNECTION": "authorized-memory-fixture"}), \
                patch.object(os, "umask", return_value=0o077), \
                patch.object(signal, "signal"), \
                patch.object(shutil, "get_terminal_size", return_value=os.terminal_size((80, 24))):
            yield

    def main_arguments(self, fixture, mode="run", worker_sha=None):
        args = ["--mode", mode, "--intent", str(TOOL / "intent.json"),
                "--intent-sha256", OP.sha(fixture.files[TOOL / "intent.json"])]
        return args + ([] if worker_sha is None else ["--worker-input-sha256", worker_sha])

    def test_real_main_dispatches_only_after_real_intent_and_complete_input_admission(self):
        for mode in ("run", "worker", "attest"):
            fixture = self.input_fixture()
            runner = Mock()
            runner.run_all.return_value = 0
            with self.subTest(mode=mode), self.input_effects(fixture), self.main_effects(), \
                    patch.object(OP, "Controller", side_effect=lambda *args: fixture.events.append(("controller", args)) or runner) as controller, \
                    patch.object(OP, "Worker", side_effect=lambda *args: fixture.events.append(("worker", args)) or runner) as worker, \
                    patch.object(OP, "attest", side_effect=lambda *args: fixture.events.append(("attest", args)) or 0) as attest:
                args = self.main_arguments(fixture, mode, "f" * 64 if mode == "worker" else None)
                self.assertEqual(OP.main(args), 0)
            last = fixture.events[-1]
            self.assertEqual(last[0], {"run": "controller", "worker": "worker", "attest": "attest"}[mode])
            self.assertEqual(last[1][0], fixture.intent)
            self.assertEqual(last[1][2]["dependencies"], {"memory": "verified"})
            self.assertLess(fixture.events.index(("dependencies",)), len(fixture.events) - 1)
            self.assertEqual((controller.call_count, worker.call_count, attest.call_count),
                             {"run": (1, 0, 0), "worker": (0, 1, 0), "attest": (0, 0, 1)}[mode])

    def test_real_main_malformed_intent_or_artifact_stops_before_any_lifecycle(self):
        for defect in ("intent", "duplicate", "binary", "tool", "history", "dependency"):
            fixture = self.input_fixture()
            if defect == "intent":
                changed = copy.deepcopy(fixture.intent)
                changed["database"] = "goby_backup_m3e_source"
                fixture.files[TOOL / "intent.json"] = OP.canonical(changed)
            elif defect == "duplicate":
                fixture.files[TOOL / "intent.json"] = b'{"marker":"one","marker":"two"}'
            else:
                path = {"binary": OP.BINARY, "tool": TOOL / "playwright.config.ts", "history": OP.HISTORY,
                        "dependency": TOOL / "dependency-files.json"}[defect]
                fixture.files[path] += b"changed"
            with self.subTest(defect=defect), self.input_effects(fixture), self.main_effects(), \
                    patch.object(OP, "Controller") as controller, patch.object(OP, "Worker") as worker, \
                    patch.object(OP, "attest") as attest, patch.object(OP, "create_file") as created, \
                    patch.object(OP, "replace_file") as replaced, patch.object(Path, "mkdir") as mkdir:
                self.assertEqual(OP.main(self.main_arguments(fixture)), 1)
                controller.assert_not_called()
                worker.assert_not_called()
                attest.assert_not_called()
                created.assert_not_called()
                replaced.assert_not_called()
                mkdir.assert_not_called()

    def test_real_main_rejects_cross_mode_worker_arguments_before_reading_intent(self):
        fixture = self.input_fixture()
        for mode, worker_sha in (("run", "f" * 64), ("attest", "f" * 64), ("worker", None), ("worker", "invalid")):
            with self.subTest(mode=mode, worker_sha=worker_sha), self.main_effects(), \
                    patch.object(OP, "load_intent") as load, patch.object(OP, "verify_inputs") as inputs, \
                    patch.object(OP, "Controller") as controller, patch.object(OP, "Worker") as worker:
                self.assertEqual(OP.main(self.main_arguments(fixture, mode, worker_sha)), 1)
                load.assert_not_called()
                inputs.assert_not_called()
                controller.assert_not_called()
                worker.assert_not_called()

    def test_real_main_sanitizes_unexpected_exceptions_without_swallowing_interrupts(self):
        fixture = self.input_fixture()
        with self.main_effects(), patch.object(OP, "load_intent", return_value=fixture.intent), \
                patch.object(OP, "verify_inputs", side_effect=RuntimeError("private-cookie-password")), \
                patch.object(OP, "Controller") as controller:
            self.assertEqual(OP.main(self.main_arguments(fixture)), 1)
        controller.assert_not_called()
        result = OP.decode(self.stdout.getvalue().encode())
        self.assertEqual(result, {"status": "failed", "error": "RuntimeError"})
        self.assertNotIn("private-cookie-password", self.stdout.getvalue())
        with self.main_effects(), patch.object(OP, "load_intent", return_value=fixture.intent), \
                patch.object(OP, "verify_inputs", side_effect=KeyboardInterrupt), \
                patch.object(OP, "Controller") as controller:
            with self.assertRaises(KeyboardInterrupt):
                OP.main(self.main_arguments(fixture))
        controller.assert_not_called()

    def attestation_fixture(self):
        required = {"worker_terminal", "hba_restored", "pair_removed", "global_catalog_unchanged", "runtime_removed",
                    "historical_receipt_unchanged", "protected_unchanged", "host_unchanged", "port_closed", "inputs_unchanged"}
        worker_raw = b'{"status":"workflow_complete"}\n'
        final_raw = b'{"rows":{},"sequences":{}}\n'
        report = {"marker": OP.MARKER, "run_id": RUN, "intent_sha256": "e" * 64,
                  "status": "awaiting_outer_attestation", "workflow_verified": True,
                  "cleanup": {name: True for name in required},
                  "controller": {"unit": intent_fixture()["controller_unit"], "invocation_id": "a" * 32,
                                 "process": {"pid": 501, "start_ticks": 11}},
                  "worker_terminal": {"InvocationID": "b" * 32},
                  "host_before": {"namespace": intent_fixture()["host_namespace"], "mountinfo_sha256": "c" * 64},
                  "protected_before": [{"identity": "protected"}], "worker_result_sha256": OP.sha(worker_raw),
                  "final_database_sha256": OP.sha(final_raw), "binding_proof": {}, "session_proof": {}}
        module = types.SimpleNamespace(OWNER=OP.CONTROL / "owner.json", HBA="Exact HBA.\n",
                                       acquire_lock=Mock(return_value=811), validate_owner=Mock(),
                                       expected_owner=Mock(return_value={}), binaries=Mock(return_value={}),
                                       directory=Mock(return_value={}))
        files = {OUTPUT / "report.json": OP.canonical(report), OUTPUT / "private/worker-result.json": worker_raw,
                 OUTPUT / "private/controller-final-database.json": final_raw,
                 OUTPUT / "catalog-before.json": b"{}", OUTPUT / "catalog-after.json": b"{}",
                 module.OWNER: b"{}"}
        inspector = types.SimpleNamespace(db=types.SimpleNamespace(rows=Mock(return_value={"role": None, "database": None}),
                                                                   maintenance=Mock(return_value=b"{}")), check_cluster=Mock())
        return types.SimpleNamespace(intent=intent_fixture(), report=report, module=module, files=files,
                                     inspector=inspector, events=[], occupied=set(), current_pid=901,
                                     host=copy.deepcopy(report["host_before"]), protected=copy.deepcopy(report["protected_before"]),
                                     unit_error=None, port_error=False, input_error=False, final_error=False)

    @contextlib.contextmanager
    def attestation_effects(self, fixture):
        import fcntl
        import pwd

        def reader(path, **options):
            fixture.events.append(("read", path))
            self.assertIn(path, fixture.files)
            return fixture.files[path]

        def terminal(unit, tag, invocation):
            fixture.events.append(("terminal", unit, invocation))
            if fixture.unit_error == unit:
                raise OP.Failure("A current unit witness is not terminal.")
            return {"InvocationID": invocation, "MainPID": "0", "observed_unit": unit}

        def listener():
            fixture.events.append(("port",))
            if fixture.port_error:
                raise OP.Failure("The owned port is still occupied.")

        def inputs(intent):
            fixture.events.append(("inputs",))
            if fixture.input_error:
                raise OP.Failure("An accepted input changed.")

        def final(*args):
            fixture.events.append(("final_database",))
            if fixture.final_error:
                raise OP.Failure("The independently retained final data proof is invalid.")

        def publish(path, raw, **options):
            fixture.events.append(("publish", path, raw))
            self.assertEqual(path, OUTPUT / "terminal.json")
            OP.require(path not in fixture.files, "An immutable memory receipt already exists.")
            fixture.files[path] = raw

        with contextlib.ExitStack() as patches:
            patches.enter_context(patch.object(sys, "platform", "linux"))
            patches.enter_context(patch.object(os, "geteuid", return_value=0))
            patches.enter_context(patch.object(os, "getpid", side_effect=lambda: fixture.current_pid))
            patches.enter_context(patch.object(os, "environ", {"SSH_CONNECTION": "authorized-memory-fixture"}))
            patches.enter_context(patch.object(OP, "directory", return_value={}))
            patches.enter_context(patch.object(OP, "present", side_effect=lambda path: path in fixture.occupied))
            patches.enter_context(patch.object(OP, "read_file", side_effect=reader))
            patches.enter_context(patch.object(OP, "unit_terminal", side_effect=terminal))
            patches.enter_context(patch.object(OP, "host_fact", side_effect=lambda: fixture.host))
            patches.enter_context(patch.object(OP, "protected_facts", side_effect=lambda: fixture.protected))
            patches.enter_context(patch.object(OP, "listener", side_effect=listener))
            patches.enter_context(patch.object(OP, "verify_inputs", side_effect=inputs))
            patches.enter_context(patch.object(OP, "verify_final_database", side_effect=final))
            patches.enter_context(patch.object(OP, "Controller", return_value=fixture.inspector))
            patches.enter_context(patch.object(OP, "load_workspace", return_value=fixture.module))
            patches.enter_context(patch.object(pwd, "getpwnam", return_value=types.SimpleNamespace(pw_uid=113, pw_gid=117)))
            patches.enter_context(patch.object(fcntl, "flock", side_effect=lambda *args: fixture.events.append(("unlock", *args))))
            patches.enter_context(patch.object(os, "close", side_effect=lambda descriptor: fixture.events.append(("close", descriptor))))
            patches.enter_context(patch.object(OP, "process_fact", side_effect=lambda pid:
                                              fixture.events.append(("observer", pid)) or {"pid": pid, "start_ticks": 21}))
            patches.enter_context(patch.object(OP, "create_file", side_effect=publish))
            yield

    def test_real_outer_attestation_rechecks_current_state_and_commits_terminal_last(self):
        fixture = self.attestation_fixture()
        with self.attestation_effects(fixture):
            self.assertEqual(OP.attest(fixture.intent, "e" * 64, {"selected": {}}), 0)
        terminal = OP.decode(fixture.files[OUTPUT / "terminal.json"])
        self.assertEqual(terminal["status"], "passed")
        self.assertEqual(terminal["observed_by"]["pid"], 901)
        self.assertEqual([event[1] for event in fixture.events if event[0] == "terminal"],
                         [fixture.intent["controller_unit"], fixture.intent["worker_unit"]])
        self.assertEqual(fixture.events[-1][0:2], ("publish", OUTPUT / "terminal.json"))
        self.assertEqual(fixture.events[-2], ("observer", 901))
        self.assertIn(("close", 811), fixture.events)
        fixture.inspector.check_cluster.assert_called_once_with(fixture.module.HBA.encode())
        fixture.inspector.db.rows.assert_called_once()
        fixture.inspector.db.maintenance.assert_called_once_with(OP.GLOBAL_SQL)

    def test_real_outer_attestation_refuses_self_certification_or_terminal_replay(self):
        for defect in ("same process", "existing terminal"):
            fixture = self.attestation_fixture()
            if defect == "same process":
                fixture.current_pid = 501
            else:
                fixture.occupied.add(OUTPUT / "terminal.json")
            with self.subTest(defect=defect), self.attestation_effects(fixture):
                self.reject(OP.attest, fixture.intent, "e" * 64, {"selected": {}})
            self.assertFalse(any(event[0] in ("terminal", "publish") for event in fixture.events))

    def test_real_outer_attestation_refuses_worker_claims_missing_cleanup_or_truthy_flags(self):
        for defect in ("worker pass", "workflow false", "missing cleanup", "truthy cleanup", "wrong run", "wrong intent"):
            fixture = self.attestation_fixture()
            if defect == "worker pass":
                fixture.report["status"] = "passed"
            elif defect == "workflow false":
                fixture.report["workflow_verified"] = 1
            elif defect == "missing cleanup":
                del fixture.report["cleanup"]["pair_removed"]
            elif defect == "truthy cleanup":
                fixture.report["cleanup"]["pair_removed"] = 1
            elif defect == "wrong run":
                fixture.report["run_id"] = "20260912_053517_9cb0074fc731"
            else:
                fixture.report["intent_sha256"] = "f" * 64
            fixture.files[OUTPUT / "report.json"] = OP.canonical(fixture.report)
            with self.subTest(defect=defect), self.attestation_effects(fixture):
                self.reject(OP.attest, fixture.intent, "e" * 64, {"selected": {}})
            self.assertFalse(any(event[0] in ("terminal", "publish") for event in fixture.events))

    def test_real_outer_attestation_refuses_each_changed_current_proof_without_publication(self):
        for defect in ("controller", "worker", "host", "protected", "port", "runtime", "inputs", "worker bytes",
                       "snapshot bytes", "final proof", "pair", "catalog", "cluster"):
            fixture = self.attestation_fixture()
            if defect in ("controller", "worker"):
                fixture.unit_error = fixture.intent[defect + "_unit"]
            elif defect == "host":
                fixture.host["mountinfo_sha256"] = "f" * 64
            elif defect == "protected":
                fixture.protected = [{"identity": "changed"}]
            elif defect == "port":
                fixture.port_error = True
            elif defect == "runtime":
                fixture.occupied.add(RUNTIME)
            elif defect == "inputs":
                fixture.input_error = True
            elif defect == "worker bytes":
                fixture.files[OUTPUT / "private/worker-result.json"] += b"changed"
            elif defect == "snapshot bytes":
                fixture.files[OUTPUT / "private/controller-final-database.json"] += b"changed"
            elif defect == "final proof":
                fixture.final_error = True
            elif defect == "pair":
                fixture.inspector.db.rows.return_value = {"role": {"oid": 17001}, "database": None}
            elif defect == "catalog":
                fixture.inspector.db.maintenance.return_value = b'{"changed":true}'
            else:
                fixture.inspector.check_cluster.side_effect = OP.Failure("The live cluster identity changed.")
            with self.subTest(defect=defect), self.attestation_effects(fixture):
                self.reject(OP.attest, fixture.intent, "e" * 64, {"selected": {}})
            self.assertNotIn(OUTPUT / "terminal.json", fixture.files)
            self.assertFalse(any(event[0] == "publish" for event in fixture.events))


def memory_error(error, test_id):
    frames = []
    current = error[2]
    while current is not None:
        code = current.tb_frame.f_code
        frames.append({"function": code.co_name, "file": code.co_filename, "line": current.tb_lineno})
        current = current.tb_next
    return json.dumps({"test": test_id, "error_type": error[0].__name__,
                       "message": str(error[1]), "frames": frames}, sort_keys=True)


class MemoryResult(unittest.TestResult):
    def _exc_info_to_string(self, error, test):
        return memory_error(error, test.id())


def main():
    global OP
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run memory-only guards only through authorized root SSH.")
    if len(sys.argv) != 2:
        raise SystemExit("Usage: test-verify-storage-binding-live-ui.py OPERATOR_SOURCE")
    source = Path(sys.argv[1])
    if source.name != "verify-storage-binding-live-ui.py":
        raise SystemExit("Only the reviewed live binding UI operator is supported.")
    import fcntl
    import pwd
    raw = source.read_bytes()
    SOURCE_LINES[str(source)] = raw.decode().splitlines(keepends=True)
    OP = types.ModuleType("storage_binding_live_ui_operator")
    OP.__file__ = str(source)
    try:
        with EffectFence():
            exec(compile(raw, str(source), "exec"), OP.__dict__)
    except Exception:
        print(json.dumps({
            "suite": "storage-binding-live-ui-guards", "status": "failed", "tests": 0,
            "failures": 0, "errors": 1, "skips": 0, "operator_sha256": hashlib.sha256(raw).hexdigest(),
            "fixtures": "one-source-read-then-effect-fenced-memory-only", "initial_source_reads": 1,
            "database_commands": 0, "filesystem_mutations": 0, "blocked_effects": BLOCKED_EFFECTS,
            "summaries": [memory_error(sys.exc_info(), "operator_import")],
        }, sort_keys=True))
        return 1
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(LiveUIGuards)
    result = MemoryResult()
    suite.run(result)
    print(json.dumps({
        "suite": "storage-binding-live-ui-guards", "status": "passed" if result.wasSuccessful() else "failed",
        "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
        "skips": len(result.skipped), "operator_sha256": hashlib.sha256(raw).hexdigest(),
        "fixtures": "one-source-read-then-effect-fenced-memory-only", "initial_source_reads": 1,
        "database_commands": 0, "filesystem_mutations": 0, "blocked_effects": BLOCKED_EFFECTS,
        "summaries": [detail for _, detail in [*result.failures, *result.errors]],
    }, sort_keys=True))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
