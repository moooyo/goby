#!/usr/bin/env python3
"""Exercise the observability browser verifier using synthetic memory only.

Run through authorized root SSH with the new verifier source and the frozen
managed-user core source. Only inert source bytes are read before installing
the effect fence. Passing these guards is not browser acceptance evidence.
"""

from __future__ import annotations

import _io
import argparse
import ast
import base64
import binascii
import builtins
import contextlib
import copy
import datetime
import hashlib
import http.client
import http.cookiejar
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import re
import secrets
import select
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import types
import unittest
from unittest.mock import patch
import urllib.error
import urllib.parse
import urllib.request

if sys.platform == "linux":
    import fcntl
    import pwd

sys.dont_write_bytecode = True
MODULE: types.ModuleType
CORE: types.ModuleType
SOURCE_TREE: ast.Module
SOURCE_TEXT = ""
SOURCE_LINES: dict[str, list[str]] = {}
SOURCE_HASHES: dict[str, str] = {}
EXPECTED_TABLES = tuple(sorted("""activity_entries application_key_clients application_key_devices
application_keys catalog_entities client_playback_references devices encoding_jobs
item_entities item_images item_metadata_state item_subtitles items libraries
library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings
sessions user_item_data users task_definitions task_occurrences task_run_children
task_run_requests task_runs task_triggers""".split()))


@contextlib.contextmanager
def memory_tracebacks():
    """Keep unittest failures from opening source files under the fence."""
    with patch.object(linecache, "checkcache", lambda filename=None: None), \
            patch.object(linecache, "lazycache", lambda filename, module_globals: False), \
            patch.object(linecache, "getlines", lambda filename, module_globals=None:
                         list(SOURCE_LINES.get(str(filename), ()))):
        yield


class EffectFence(contextlib.ExitStack):
    """Reject every unmocked host effect, including during module imports."""

    def __init__(self):
        super().__init__()
        self.violations = []

    def __enter__(self):
        super().__enter__()
        self.enter_context(memory_tracebacks())
        self.enter_context(patch.object(os, "environ", {}))
        fence = self

        class RejectUncachedImports:
            def find_spec(self, fullname, path=None, target=None):
                fence.violations.append("import:" + fullname)
                raise AssertionError("Uncached import: " + fullname)

        self.enter_context(patch.object(sys, "meta_path", [RejectUncachedImports()]))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")),
            (_io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo", "gethostname", "getfqdn", "gethostbyname")),
            (select, ("select", "poll", "epoll", "kqueue")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (urllib.request, ("urlopen", "urlretrieve")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (tarfile, ("open",)),
            (tempfile, ("TemporaryDirectory", "NamedTemporaryFile", "TemporaryFile", "mkstemp", "mkdtemp")),
            (signal, ("signal", "getsignal", "setitimer", "getitimer", "pthread_sigmask", "pidfd_send_signal")),
            (secrets, ("token_bytes", "token_hex", "token_urlsafe")),
            (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "stat", "lstat", "fstat", "statvfs", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "lseek", "fsync", "umask", "kill", "killpg", "system", "popen", "fork", "posix_spawn",
                  "posix_spawnp", "execve", "execvp", "replace", "rename", "mkdir", "makedirs", "remove", "unlink",
                  "rmdir", "chmod", "chown", "link", "symlink", "truncate", "waitpid", "pidfd_open", "access", "urandom")),
            (os.path, ("ismount",)),
            (Path, ("open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat", "read_bytes",
                    "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink", "chmod", "touch",
                    "rename", "replace", "glob", "rglob", "iterdir")),
        )
        for owner, names in targets:
            for name in names:
                if not hasattr(owner, name):
                    continue
                label = owner.__name__ + "." + name

                def denied(*arguments, _label=label, **options):
                    self.violations.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        if sys.platform == "linux":
            for owner, name in ((fcntl, "flock"), (pwd, "getpwnam")):
                label = owner.__name__ + "." + name

                def denied(*arguments, _label=label, **options):
                    self.violations.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments):
        super().__exit__(*arguments)
        if self.violations:
            raise AssertionError("External effects attempted: " + ", ".join(self.violations))


def function_node(name):
    matches = [node for node in ast.walk(SOURCE_TREE)
               if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name == name]
    if len(matches) != 1:
        raise AssertionError("Expected one verifier function: " + name)
    return matches[0]


def called_name(node):
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        return called_name(node.value) + "." + node.attr
    if isinstance(node, ast.Call):
        return called_name(node.func) + "()"
    return ""


class ObservabilityBrowserGuards(unittest.TestCase):
    def setUp(self):
        fence = EffectFence()
        fence.__enter__()
        self.addCleanup(fence.__exit__, None, None, None)
        output = contextlib.redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)

    def replace(self, owner, name, value):
        replacement = patch.object(owner, name, value)
        replacement.start()
        self.addCleanup(replacement.stop)
        return value

    def runner(self):
        self.replace(CORE, "BASE_ENV", dict(CORE.BASE_ENV))
        self.replace(CORE, "MARKER", CORE.MARKER)
        self.replace(CORE, "RUN_ENV", CORE.RUN_ENV)
        sequence = iter(range(100))
        self.replace(secrets, "token_hex", lambda count: "a" * (count * 2))
        self.replace(secrets, "token_urlsafe", lambda count: "synthetic-secret-" + str(next(sequence)) + "-" + "x" * 43)
        self.replace(time, "strftime", lambda *arguments: "20260910_120000")
        self.replace(time, "gmtime", lambda *arguments: ())
        self.replace(time, "clock_gettime", lambda *arguments: 100.0)
        self.replace(os, "sysconf", lambda name: 100)
        self.replace(urllib.request, "build_opener", lambda *arguments: types.SimpleNamespace())
        arguments = types.SimpleNamespace(snapshot=Path("/synthetic/snapshot"),
            binary=Path("/opt/goby-test/exec-scratch/synthetic-binary"), binary_sha256="1" * 64,
            assets=Path("/synthetic/assets.tar"), assets_sha256="2" * 64,
            core_runner=Path(CORE.__file__))
        value = MODULE.create_runner(CORE, arguments)
        value.database_oid, value.lock = 45123, 81
        value.port, value.origin = 18888, "http://127.0.0.1:18888"
        value.goby = types.SimpleNamespace(pw_uid=995, pw_gid=995)
        value.browser_work = value.output / "browser"
        value.browser_temp = value.output / "browser-temp"
        value.manifest = value.browser_work / "observability-fixture.json"
        value.result_path = value.browser_work / "observability-result.json"
        value.log_directory = value.runtime / "diagnostics"
        value.log_directory_identity = (51, 73)
        return value

    def browser_result(self, *, complete=True):
        cookie = "goby_session=" + "b" * 43
        csrf = "c" * 43
        return {"Marker": MODULE.RESULT_MARKER, "RunId": "20260910_120000_aaaaaaaaaa", "Complete": complete,
                "BrowserSessionId": "d" * 32, "BrowserCookie": cookie, "BrowserCSRF": csrf,
                "BrowserSecrets": [cookie, cookie.split("=", 1)[1], csrf],
                "Checks": {name: True for name in MODULE.REQUIRED_BROWSER_CHECKS} if complete else {}}

    def snapshot(self):
        return {name: [] for name in EXPECTED_TABLES}

    def fake_diagnostics(self, runner, values):
        """Expose synthetic descriptors only after the real ownership checks."""
        files = {runner.log_directory / name: content for name, content in values.items()}
        metadata = {path: types.SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=995,
                    st_nlink=1, st_size=len(content)) for path, content in files.items()}
        directory = types.SimpleNamespace(st_mode=stat.S_IFDIR | 0o700, st_uid=995, st_dev=51, st_ino=73)
        descriptors, reads = {}, []

        def opened(path, flags):
            self.assertIn(path, files)
            self.assertEqual(flags, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
            descriptor = len(descriptors) + 100
            descriptors[descriptor] = path
            return descriptor

        class MemoryFile(io.BytesIO):
            def __init__(self, descriptor):
                self.descriptor = descriptor
                super().__init__(files[descriptors[descriptor]])

            def fileno(self):
                return self.descriptor

            def read(self, limit=-1):
                reads.append((descriptors[self.descriptor], limit))
                return super().read(limit)

        self.replace(Path, "lstat", lambda path: directory if path == runner.log_directory else metadata[path])
        self.replace(Path, "iterdir", lambda path: iter(files) if path == runner.log_directory else self.fail("Unexpected directory"))
        self.replace(os, "open", opened)
        self.replace(os, "fdopen", lambda descriptor, mode: MemoryFile(descriptor) if mode == "rb" else self.fail("Unexpected mode"))
        self.replace(os, "fstat", lambda descriptor: metadata[descriptors[descriptor]])
        return types.SimpleNamespace(files=files, metadata=metadata, directory=directory, reads=reads)

    def test_canonical_projection_is_stable_and_does_not_mutate_inputs(self):
        value = {"z": [{"b": 2, "a": 1}], "a": {"z": None, "a": True}}
        before = copy.deepcopy(value)
        self.assertEqual(MODULE.canonical(value), '{"a":{"a":true,"z":null},"z":[{"a":1,"b":2}]}')
        self.assertEqual(value, before)
        self.assertEqual(MODULE.canonical({"z": 2, "a": 1}), MODULE.canonical({"a": 1, "z": 2}))

    def test_secret_encodings_and_late_bytes_cannot_evade_detection(self):
        secret = 'synthetic /+"private\\value\u00e9'
        raw = secret.encode()
        variants = {secret, json.dumps(secret)[1:-1], urllib.parse.quote(secret, safe=""),
                    urllib.parse.quote_plus(secret), raw.hex(), base64.b64encode(raw).decode(),
                    base64.urlsafe_b64encode(raw).decode().rstrip("=")}
        self.assertTrue(variants <= MODULE.secret_variants([secret]))
        self.assertEqual(MODULE.secret_variants(["", None, 0]), set())
        for variant in variants:
            with self.subTest(encoding=variant):
                self.assertTrue(MODULE.contains_secret(b"safe " * 4096 + variant.encode(), [secret]))
        self.assertFalse(MODULE.contains_secret(b'{"event":"request.completed"}\n', [secret]))

    def test_diagnostics_require_complete_bounded_json_objects_and_scan_every_line(self):
        row = {"event": "request.completed", "request_id": "a" * 32}
        raw = (MODULE.canonical(row) + "\n").encode()
        self.assertEqual(MODULE.diag_bytes_validate(raw, []), [row])
        self.assertEqual(MODULE.diag_bytes_validate(b"", []), [])
        invalid = (raw[:-1], b"[]\n", b"null\n", b'{"event":1}\n', b"{}\n", b"\xff\n",
                   b"\n", raw + b'{"event":"truncated"}', b" " * 8193)
        for content in invalid:
            with self.subTest(content=content[:64]):
                with self.assertRaises((ValueError, UnicodeError)):
                    MODULE.diag_bytes_validate(content, [])
        secret = "synthetic-sensitive-value"
        for variant in MODULE.secret_variants([secret]):
            leaked = raw + (MODULE.canonical({"event": "failure", "detail": variant}) + "\n").encode()
            with self.subTest(encoding=variant), self.assertRaises(ValueError):
                MODULE.diag_bytes_validate(leaked, [secret])

    def test_private_result_requires_exact_run_schema_typed_checks_and_original_credentials(self):
        value = self.browser_result()
        before = copy.deepcopy(value)
        self.assertIs(MODULE.validate_browser_result(value, value["RunId"], complete=True), value)
        self.assertEqual(value, before)
        partial = {"Marker": MODULE.RESULT_MARKER, "RunId": value["RunId"], "Complete": False,
                   "BrowserSecrets": [], "Checks": {}}
        self.assertIs(MODULE.validate_browser_result(partial, value["RunId"]), partial)
        changes = (("Marker", "foreign"), ("RunId", "other-run"), ("Complete", 1), ("Complete", False),
                   ("BrowserSessionId", "D" * 32), ("BrowserCookie", "goby_session=" + "b" * 43 + "; other=secret"),
                   ("BrowserCSRF", ""), ("BrowserSecrets", []), ("BrowserSecrets", ["x"] * 17),
                   ("Checks", {name: 1 for name in MODULE.REQUIRED_BROWSER_CHECKS}), ("Unexpected", "value"))
        for field, bad in changes:
            changed = copy.deepcopy(value)
            changed[field] = bad
            with self.subTest(field=field, bad=bad), self.assertRaises(ValueError):
                MODULE.validate_browser_result(changed, value["RunId"], complete=True)
        for name in MODULE.REQUIRED_BROWSER_CHECKS:
            changed = copy.deepcopy(value)
            changed["Checks"].pop(name)
            with self.subTest(missing=name), self.assertRaises(ValueError):
                MODULE.validate_browser_result(changed, value["RunId"], complete=True)
        for missing in value["BrowserSecrets"]:
            changed = copy.deepcopy(value)
            changed["BrowserSecrets"].remove(missing)
            with self.subTest(missing_credential=missing), self.assertRaises(ValueError):
                MODULE.validate_browser_result(changed, value["RunId"], complete=True)

    def test_constructor_retains_frozen_ownership_and_cleanup_lifecycle(self):
        runner = self.runner()
        self.assertIsInstance(runner, CORE.Runner)
        for name in ("prepare_database", "replace_hba", "stop_tagged_processes", "execute"):
            self.assertIs(getattr(runner, name).__func__, getattr(CORE.Runner, name))
        self.assertEqual(CORE.PG_PORT, 15432)
        self.assertEqual(runner.database, "goby_m5i_observe_" + runner.run_id)
        self.assertEqual(runner.role, "goby_m5i_role_" + runner.run_id)
        self.assertEqual(runner.tag, "goby-observability-browser-v1:" + runner.run_id)
        self.assertEqual(CORE.BASE_ENV["PGAPPNAME"], "goby_m5i_control_" + runner.run_id)
        self.assertEqual(set(runner.sentinels), {"header", "query", "path", "body"})
        self.assertTrue(set(runner.sentinels.values()) <= set(runner.secrets))

    def test_database_reads_require_exact_new_oid_role_tag_and_lock_before_psql(self):
        runner = self.runner()
        identity = {"oid": runner.database_oid, "owner": runner.role, "tag": runner.tag, "role_tag": runner.tag}
        calls = []
        self.replace(CORE, "pg_json", lambda statement: calls.append(("identity", statement)) or copy.deepcopy(identity))
        self.replace(CORE, "command", lambda arguments, **options: calls.append(("psql", arguments, options)) or "synthetic")
        self.assertEqual(runner.owned_database_read("SELECT 1;"), "synthetic")
        self.assertEqual([entry[0] for entry in calls], ["identity", "psql"])
        self.assertIn("WHERE d.datname = '" + runner.database + "'", calls[0][1])
        arguments, options = calls[1][1:]
        self.assertEqual(arguments[arguments.index("-p") + 1], "15432")
        self.assertEqual(arguments[arguments.index("-d") + 1], runner.database)
        self.assertEqual(arguments[arguments.index("-U") + 1], runner.role)
        self.assertEqual(arguments[arguments.index("-h") + 1], "127.0.0.1")
        self.assertIn("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;", options["text"])
        self.assertIn("current_database() <> '" + runner.database + "'", options["text"])
        self.assertIn("current_user <> '" + runner.role + "'", options["text"])
        self.assertEqual(options["environment"]["PGPASSWORD"], runner.role_password)
        for field in identity:
            original, identity[field] = identity[field], "foreign"
            calls.clear()
            with self.subTest(field=field), self.assertRaises(CORE.VerificationError):
                runner.owned_database_read("SELECT 1;")
            self.assertEqual([entry[0] for entry in calls], ["identity"])
            identity[field] = original
        for field, bad in (("database", "goby_test"), ("role", "postgres"), ("lock", None), ("database_oid", None)):
            original = getattr(runner, field)
            setattr(runner, field, bad)
            calls.clear()
            with self.subTest(field=field), self.assertRaises(CORE.VerificationError):
                runner.owned_database_read("SELECT 1;")
            self.assertEqual(calls, [])
            setattr(runner, field, original)

    def test_complete_snapshot_covers_exactly_29_tables_without_publishing_stored_secrets(self):
        runner = self.runner()
        self.assertEqual(MODULE.PUBLIC_TABLES, EXPECTED_TABLES)
        self.assertEqual(len(EXPECTED_TABLES), 29)
        rows = self.snapshot()
        rows["users"] = [{"id": "owned-admin", "password_hash": "synthetic-password-hash"}]
        rows["sessions"] = [{"token_hash": "\\xdeadbeef", "fingerprint": "synthetic-fingerprint"}]
        value = {"Tables": list(EXPECTED_TABLES), "Rows": rows}
        statements = []
        self.replace(runner, "owned_database_read", lambda statement: statements.append(statement) or MODULE.canonical(value))
        self.assertEqual(runner.full_private_snapshot(), rows)
        self.assertEqual(runner.report["snapshot_tables"], list(EXPECTED_TABLES))
        self.assertEqual(set(re.findall(r'FROM public\."([a-z_]+)" r', statements[0])), set(EXPECTED_TABLES))
        self.assertEqual(len(re.findall(r'FROM public\."([a-z_]+)" r', statements[0])), 29)
        self.assertFalse(re.search(r"\b(?:INSERT|UPDATE|DELETE|TRUNCATE|DROP|ALTER|CREATE)\b", statements[0], re.I))
        self.assertTrue({"synthetic-password-hash", "\\xdeadbeef", "deadbeef", "synthetic-fingerprint"} <= set(runner.secrets))
        self.assertNotIn("synthetic-password-hash", MODULE.canonical(runner.report))
        for kind in ("missing-table", "extra-table", "missing-rows", "extra-rows"):
            changed = copy.deepcopy(value)
            if kind == "missing-table":
                changed["Tables"].remove("activity_entries")
            elif kind == "extra-table":
                changed["Tables"].append("foreign_table")
            elif kind == "missing-rows":
                changed["Rows"].pop("activity_entries")
            else:
                changed["Rows"]["foreign_table"] = []
            self.replace(runner, "owned_database_read", lambda statement, changed=changed: MODULE.canonical(changed))
            with self.subTest(kind=kind), self.assertRaises(CORE.VerificationError):
                runner.full_private_snapshot()
        self.replace(runner, "owned_database_read", lambda statement: " " * (8 * 1024 * 1024 + 1))
        with self.assertRaises(CORE.VerificationError):
            runner.full_private_snapshot()

    def test_shared_services_are_inspected_with_show_and_fixed_process_identity_only(self):
        runner = self.runner()
        commands = []

        def command(arguments):
            commands.append(arguments)
            self.assertEqual(arguments[:2], ["systemctl", "show"])
            self.assertEqual(arguments[3:], ["--property=MainPID", "--property=ActiveState"])
            pid = MODULE.SHARED_PID if arguments[2] == "goby-foundation-test.service" else MODULE.REFERENCE_PID
            return "MainPID=" + str(pid) + "\nActiveState=active"

        def status(path):
            self.assertEqual(path.name, "stat")
            ticks = MODULE.SHARED_START_TICKS if path.parent.name == str(MODULE.SHARED_PID) else 12345
            return "1 (synthetic) " + " ".join(["S", *(["0"] * 18), str(ticks)])

        self.replace(CORE, "command", command)
        self.replace(Path, "read_text", status)
        self.replace(CORE, "file_digest", lambda path: MODULE.SHARED_BINARY_SHA256 if path.parent.name == str(MODULE.SHARED_PID) else "3" * 64)
        value = runner.shared_identity()
        self.assertEqual(len(commands), 2)
        self.assertEqual(set(value), {"goby-foundation-test.service", "goby-emby-reference.service"})
        self.replace(CORE, "command", lambda arguments: "MainPID=999\nActiveState=active")
        with self.assertRaises(CORE.VerificationError):
            runner.shared_identity()

    def test_all_retained_files_are_read_completely_and_secret_in_later_file_fails(self):
        runner = self.runner()
        first = "goby-" + "1" * 32 + "-" + "2" * 32 + ".jsonl"
        second = "goby-" + "1" * 32 + "-" + "3" * 32 + ".jsonl"
        values = {first: b'{"event":"request.completed"}\n', second: b'{"event":"safe"}\n'}
        fake = self.fake_diagnostics(runner, values)
        self.assertEqual(runner.diagnostic_files(), values)
        self.assertEqual(fake.reads, [(runner.log_directory / name, 8193) for name in (first, second)])
        path = runner.log_directory / second
        fake.files[path] = (MODULE.canonical({"event": "failure", "detail": runner.sentinels["header"]}) + "\n").encode()
        fake.metadata[path].st_size = len(fake.files[path])
        with self.assertRaises(CORE.VerificationError):
            runner.diagnostic_files()

    def test_diagnostic_ownership_link_mode_and_observed_size_changes_fail_closed(self):
        runner = self.runner()
        name = "goby-" + "1" * 32 + "-" + "2" * 32 + ".jsonl"
        fake = self.fake_diagnostics(runner, {name: b'{"event":"safe"}\n'})
        metadata = fake.metadata[runner.log_directory / name]
        for field, bad in (("st_mode", stat.S_IFLNK | 0o600), ("st_mode", stat.S_IFREG | 0o644),
                           ("st_uid", 0), ("st_nlink", 2), ("st_size", 8193), ("st_size", metadata.st_size + 1)):
            original = getattr(metadata, field)
            setattr(metadata, field, bad)
            with self.subTest(field=field, bad=bad), self.assertRaises(CORE.VerificationError):
                runner.diagnostic_files()
            setattr(metadata, field, original)
        fake.directory.st_ino += 1
        with self.assertRaises(CORE.VerificationError):
            runner.diagnostic_files()

    def test_immediate_evidence_waits_for_the_exact_request_before_recording_all_bytes(self):
        runner = self.runner()
        expected = "f" * 32
        previous = b'{"event":"request.completed","request_id":"' + b"e" * 32 + b'"}\n'
        matching = b'{"event":"request.completed","request_id":"' + expected.encode() + b'"}\n'
        states = [{"first": previous}, {"first": previous, "second": matching}]
        seen = []
        self.replace(runner, "paused_diagnostics", lambda: states.pop(0))

        def wait(predicate, message, timeout):
            self.assertEqual(timeout, 5)
            seen.append(predicate())
            self.assertNotIn("immediate_injection_evidence", runner.report)
            seen.append(predicate())
            self.assertTrue(seen[-1])

        self.replace(CORE, "wait_until", wait)
        runner.immediate_injection_check("header", expected)
        self.assertEqual(seen, [False, True])
        evidence = runner.report["immediate_injection_evidence"]["header"]
        self.assertEqual(evidence["all_retained_files_checked"], 2)
        self.assertEqual(evidence["all_retained_bytes_checked"], len(previous) + len(matching))
        self.assertEqual(evidence["files_sha256"], {"first": hashlib.sha256(previous).hexdigest(), "second": hashlib.sha256(matching).hexdigest()})
        for kind, identity in (("foreign", expected), ("query", "not-a-request-id")):
            with self.subTest(kind=kind), self.assertRaises(CORE.VerificationError):
                runner.immediate_injection_check(kind, identity)

    def test_pause_waits_for_committed_request_and_always_resumes_and_closes_owned_pidfd(self):
        for failure in (False, True):
            runner = self.runner()
            runner.app = types.SimpleNamespace(pid=2468, poll=lambda: None)
            runner.last_request_id = "f" * 32
            calls = []
            self.replace(runner, "wait_for_request_record", lambda identity: calls.append(("committed", identity)))
            self.replace(os, "pidfd_open", lambda pid: calls.append(("pidfd", pid)) or 91)
            self.replace(signal, "pidfd_send_signal", lambda descriptor, signum: calls.append(("signal", descriptor, signum)))
            self.replace(os, "close", lambda descriptor: calls.append(("close", descriptor)))
            self.replace(Path, "read_text", lambda path: "2468 (synthetic child) T 0")

            def wait(predicate, message, timeout):
                self.assertEqual(timeout, 3)
                self.assertTrue(predicate())

            def diagnostics():
                calls.append(("snapshot",))
                if failure:
                    raise CORE.VerificationError("Synthetic unsafe diagnostic bytes")
                return {"owned": b'{"event":"safe"}\n'}

            self.replace(CORE, "wait_until", wait)
            self.replace(runner, "diagnostic_files", diagnostics)
            with self.subTest(failure=failure):
                if failure:
                    with self.assertRaises(CORE.VerificationError):
                        runner.paused_diagnostics()
                else:
                    self.assertEqual(runner.paused_diagnostics(), {"owned": b'{"event":"safe"}\n'})
                self.assertEqual(calls, [("committed", "f" * 32), ("pidfd", 2468), ("signal", 91, signal.SIGSTOP),
                                         ("snapshot",), ("signal", 91, signal.SIGCONT), ("close", 91)])

    def test_fallback_synchronization_requires_complete_matching_completion_line(self):
        runner = self.runner()
        expected = "f" * 32
        complete = (MODULE.canonical({"event": "request.completed", "request_id": expected}) + "\n").encode()
        contents = [complete[:-1], b"synthetic non-JSON startup\n" + complete]
        state = {"raw": contents[0]}
        self.replace(Path, "stat", lambda path: types.SimpleNamespace(st_size=len(state["raw"])))
        self.replace(Path, "read_bytes", lambda path: state["raw"])

        def wait(predicate, message, timeout):
            self.assertEqual(timeout, 5)
            self.assertFalse(predicate())
            state["raw"] = contents[1]
            self.assertTrue(predicate())

        self.replace(CORE, "wait_until", wait)
        runner.wait_for_request_record(expected)

    def bootstrap_fakes(self, runner, *, fail_kind=None):
        events = []
        initial = self.snapshot()
        initial["schema_migrations"] = [{"version": number} for number in range(1, 23)]
        runner.administrator = {"Id": "1" * 32, "Name": runner.admin_name}
        runner.control_session_id = "2" * 32
        seeded = copy.deepcopy(initial)
        seeded["users"] = [{"id": runner.administrator["Id"]}]
        seeded["sessions"] = [{"id": runner.control_session_id}]
        seeded["activity_entries"] = [{"id": 1, "action": "user.created"}, {"id": 2, "action": "session.login"}]
        for index in range(62):
            seeded["activity_entries"].append({"id": index + 3, "action": "settings.updated", "revision": index + 2,
                "source": "native", "actor_kind": "user", "actor_id": runner.administrator["Id"],
                "actor_credential_id": runner.control_session_id, "resource_kind": "settings", "resource_id": "1",
                "affected_count": 1, "severity": "Info",
                "changed_fields": ["ServerName", "ServerNameMode"] if index in (0, 61) else ["ServerName"]})
        snapshots = iter((initial, seeded))
        original = {"Revision": "1", "Overrides": dict.fromkeys(MODULE.SETTINGS_FIELDS),
                    "ServerNameMode": "deployment", "Encoding": {"TranscodingMaxWidth": 0}}
        self.replace(runner, "full_private_snapshot", lambda: events.append(("snapshot",)) or copy.deepcopy(next(snapshots)))
        self.replace(CORE.Runner, "bootstrap", lambda value: events.append(("core-bootstrap",)))
        self.replace(runner, "control_login", lambda: events.append(("control-login",)))
        self.replace(runner, "list_sessions", lambda: [{"Id": runner.control_session_id}])

        def http_json(route, **options):
            if route == "/admin/v1/settings":
                return copy.deepcopy(original)
            self.assertTrue(route.startswith("/admin/v1/activity?"))
            events.append(("activity",))
            return {"Items": [{} for _ in range(62)], "TotalRecordCount": 62, "RetentionDays": 30}

        def request(route, **options):
            kind = "header" if options.get("headers") else "query" if "?" in route else "path"
            events.append(("request", kind))
            runner.last_request_id = str(len(events)).zfill(32)

        def update(overrides, mode, encoding):
            index = sum(entry[0] == "settings-write" for entry in events)
            self.assertEqual(mode, "custom")
            self.assertEqual(encoding, original["Encoding"])
            self.assertEqual(overrides["ServerName"], runner.sentinels["body"] if index == 0 else "Observed change " + str(index))
            events.append(("settings-write", index))
            runner.last_request_id = str(len(events)).zfill(32)
            runner.settings_current_revision = str(index + 2)

        def check(kind, identity):
            self.assertEqual(identity, runner.last_request_id)
            events.append(("immediate-check", kind))
            if kind == fail_kind:
                raise CORE.VerificationError("Synthetic immediate secrecy failure")

        def restore():
            events.append(("restore",))
            runner.settings_restore_required = False

        self.replace(runner, "http_json", http_json)
        self.replace(runner, "request", request)
        self.replace(runner, "update_settings", update)
        self.replace(runner, "immediate_injection_check", check)
        self.replace(runner, "restore_settings", restore)
        self.replace(runner, "paused_diagnostics", lambda: events.append(("rotation",)) or
                     {"first": b'{"event":"safe"}\n' * 12, "second": b'{"event":"safe"}\n'})
        self.replace(CORE, "private_write", lambda path, content: events.append(("manifest", json.loads(content))))
        return events

    def test_four_injections_are_checked_before_later_mutations_rotation_and_manifest(self):
        runner = self.runner()
        events = self.bootstrap_fakes(runner)
        runner.bootstrap()
        for kind in ("header", "query", "path"):
            index = events.index(("request", kind))
            self.assertEqual(events[index + 1], ("immediate-check", kind))
        index = events.index(("settings-write", 0))
        self.assertEqual(events[index + 1], ("immediate-check", "body"))
        self.assertEqual(events[index + 2], ("settings-write", 1))
        self.assertEqual(sum(entry[0] == "settings-write" for entry in events), 61)
        self.assertLess(events.index(("settings-write", 60)), events.index(("restore",)))
        self.assertLess(events.index(("immediate-check", "body")), events.index(("rotation",)))
        self.assertEqual(events[-1][0], "manifest")
        self.assertEqual(runner.report["fixtures"]["settings_audit_entries"], 62)

    def test_each_failed_immediate_check_prevents_later_injections_rotation_and_manifest(self):
        for kind in ("header", "query", "path", "body"):
            runner = self.runner()
            events = self.bootstrap_fakes(runner, fail_kind=kind)
            with self.subTest(kind=kind), self.assertRaises(CORE.VerificationError):
                runner.bootstrap()
            self.assertEqual(events[-1], ("immediate-check", kind))
            self.assertFalse(any(entry[0] in ("rotation", "manifest") for entry in events))
            self.assertEqual(sum(entry[0] == "settings-write" for entry in events), 1 if kind == "body" else 0)

    def test_settings_restore_refuses_unacknowledged_revision_and_recovers_exact_projection(self):
        runner = self.runner()
        fields = ("Defaults", "Overrides", "Effective", "Sources", "ServerNameMode", "Encoding", "Deployment")
        runner.original_settings = {field: {"synthetic": field} for field in fields}
        runner.settings_restore_required, runner.settings_current_revision = True, "9"
        writes = []
        self.replace(runner, "http_json", lambda route, **options: {"Revision": "10"})
        self.replace(runner, "update_settings", lambda *arguments: writes.append(arguments))
        with self.assertRaises(CORE.VerificationError):
            runner.restore_settings()
        self.assertEqual(writes, [])
        self.assertTrue(runner.settings_restore_required)
        self.replace(runner, "http_json", lambda route, **options: {"Revision": "9"})
        restored = {**copy.deepcopy(runner.original_settings), "Revision": "10"}
        self.replace(runner, "update_settings", lambda *arguments: writes.append(arguments) or restored)
        runner.restore_settings()
        self.assertEqual(writes, [(runner.original_settings["Overrides"], runner.original_settings["ServerNameMode"], runner.original_settings["Encoding"])])
        self.assertFalse(runner.settings_restore_required)
        self.assertEqual(runner.restored_settings, restored)

    def test_partial_browser_credentials_are_collected_before_identity_failure(self):
        runner = self.runner()
        value = self.browser_result(complete=False)
        value["RunId"] = "foreign-run"
        self.replace(CORE, "private_file", lambda path: MODULE.canonical(value).encode())
        with self.assertRaises(CORE.VerificationError):
            runner.load_browser_result()
        self.assertTrue(set(value["BrowserSecrets"]) <= set(runner.secrets))
        self.assertIsNone(runner.browser_result)

    def restart_fakes(self, runner, *, corrupt_table=None, corrupt_log=False):
        events, before = [], self.snapshot()
        before["activity_entries"] = [{"id": 1, "action": "settings.updated"}]
        after = copy.deepcopy(before)
        if corrupt_table:
            after[corrupt_table] = [{"unexpected": "write"}]
        snapshots = iter((before, after, before, after))
        retained = {"owned.jsonl": b'{"event":"safe"}\n'}
        recovered = {"owned.jsonl": b'{"event":"rewritten"}\n'} if corrupt_log else retained
        runner.ui_files = {"index.html": "a" * 64}
        self.replace(runner, "stop_app", lambda: events.append("stop"))
        self.replace(runner, "start_app", lambda: events.append("start"))
        self.replace(runner, "full_private_snapshot", lambda: events.append("snapshot") or copy.deepcopy(next(snapshots)))
        self.replace(runner, "diagnostic_files", lambda: events.append("retained") or dict(retained))
        self.replace(runner, "paused_diagnostics", lambda: events.append("recovered") or dict(recovered))

        def http_json(route, **options):
            events.append("http")
            if route.startswith("/admin/v1/logs?"):
                return {"Items": [{"Name": "owned.jsonl"}], "Status": {"Healthy": True}}
            return {"Items": [{} for _ in range(62)], "TotalRecordCount": 62}

        self.replace(runner, "http_json", http_json)
        self.replace(runner, "request", lambda route, **options: events.append("download") or
                     (200, {"Cache-Control": "no-store"}, retained["owned.jsonl"]))
        self.replace(runner, "verify_browser_revoked", lambda: events.append("credentials"))
        self.replace(Path, "rglob", lambda path, pattern: iter([path / "index.html"]))
        self.replace(Path, "is_file", lambda path: True)
        self.replace(CORE, "file_digest", lambda path: "a" * 64)
        self.replace(runner, "assert_private_logs", lambda: events.append("private-logs"))
        return events

    def test_two_restarts_compare_all_rows_before_authenticated_reads_and_recover_exact_bytes(self):
        runner = self.runner()
        events = self.restart_fakes(runner)
        runner.verify_restart()
        expected = ["stop", "snapshot", "retained", "start", "snapshot", "recovered", "http", "download", "credentials", "http"]
        self.assertEqual(events, expected + expected + ["private-logs"])
        evidence = runner.report["restart_evidence"]
        self.assertEqual([entry["all_public_tables_compared"] for entry in evidence], [29, 29])
        self.assertEqual([entry["retained_download_sha256"] for entry in evidence], [hashlib.sha256(b'{"event":"safe"}\n').hexdigest()] * 2)

    def test_restart_rejects_any_changed_table_before_authentication_or_log_recovery(self):
        for table in EXPECTED_TABLES:
            runner = self.runner()
            events = self.restart_fakes(runner, corrupt_table=table)
            with self.subTest(table=table), self.assertRaises(CORE.VerificationError):
                runner.verify_restart()
            self.assertEqual(events, ["stop", "snapshot", "retained", "start", "snapshot"])

    def test_restart_rejects_rewritten_retained_log_before_download(self):
        runner = self.runner()
        events = self.restart_fakes(runner, corrupt_log=True)
        with self.assertRaises(CORE.VerificationError):
            runner.verify_restart()
        self.assertEqual(events, ["stop", "snapshot", "retained", "start", "snapshot", "recovered"])

    def cleanup_fakes(self, runner, *, browser_active=False, fail_browser=False, fail_control=False, fail_restore=False,
                      extra_session=False, unrevoked=False):
        events = []
        runner.report["status"] = "passed"
        runner.session_cleanup_required = True
        runner.app = types.SimpleNamespace(poll=lambda: None)
        runner.control_cookie, runner.control_csrf = "goby_session=" + "a" * 43, "control-csrf-synthetic"
        runner.control_session_id = "e" * 32
        runner.browser_result = self.browser_result()
        runner.secrets.extend([runner.control_cookie, runner.control_csrf, *runner.browser_result["BrowserSecrets"]])
        sessions = [{"Id": runner.control_session_id, "Status": "active", "IsCurrent": True},
                    {"Id": runner.browser_result["BrowserSessionId"], "Status": "active" if browser_active else "revoked", "IsCurrent": False}]
        if extra_session:
            sessions.append({"Id": "f" * 32, "Status": "revoked", "IsCurrent": False})
        rows = self.snapshot()
        rows["sessions"] = [{"id": item["Id"], "revoked_at": "synthetic-revoked"} for item in sessions]
        if unrevoked:
            rows["sessions"][1]["revoked_at"] = None
        rows["activity_entries"] = [{"action": action} for action, count in
            (("user.created", 1), ("session.login", 2), ("settings.updated", 62), ("session.revoked", 2)) for _ in range(count)]

        def http_json(route, **options):
            if route == "/admin/v1/session":
                self.assertNotIn("method", options)
                events.append("original-control")
                if fail_control:
                    raise CORE.VerificationError("Synthetic original control failure")
                return {"CSRFToken": runner.control_csrf}
            self.assertEqual(route, "/admin/v1/sessions/" + runner.browser_result["BrowserSessionId"] + "/revoke")
            self.assertEqual(options, {"method": "POST", "administrator": True, "body": {}})
            events.append("browser-revoke")
            if fail_browser:
                raise CORE.VerificationError("Synthetic lost browser revocation acknowledgement")
            return {"SessionId": runner.browser_result["BrowserSessionId"], "RevokedAt": "synthetic-revoked"}

        def request(route, **options):
            self.assertEqual(route, "/admin/v1/session")
            if options.get("method") == "DELETE":
                self.assertEqual(options, {"method": "DELETE", "administrator": True, "expected": (204,)})
                events.append("control-logout")
            else:
                self.assertEqual(options.get("expected"), (401,))
                cookie = options["headers"]["Cookie"]
                self.assertIn(cookie, (runner.browser_result["BrowserCookie"], runner.control_cookie))
                events.append("browser-401" if cookie == runner.browser_result["BrowserCookie"] else "control-401")
            return 204, {}, b""

        def restore():
            events.append("restore")
            if fail_restore:
                raise CORE.VerificationError("Synthetic unacknowledged settings revision")

        self.replace(Path, "exists", lambda path: False)
        self.replace(runner, "http_json", http_json)
        self.replace(runner, "request", request)
        self.replace(runner, "restore_settings", restore)
        self.replace(runner, "list_sessions", lambda: events.append("sessions") or copy.deepcopy(sessions))
        self.replace(runner, "full_private_snapshot", lambda: events.append("sql-proof") or copy.deepcopy(rows))
        self.replace(runner, "assert_private_logs", lambda: events.append("private-logs"))
        self.replace(CORE.Runner, "cleanup", lambda value: events.append("core-cleanup"))
        return events

    def test_cleanup_revokes_exact_two_credentials_with_control_logout_last_then_frozen_disposal(self):
        for browser_active in (False, True):
            runner = self.runner()
            events = self.cleanup_fakes(runner, browser_active=browser_active)
            runner.cleanup()
            expected = ["original-control", "restore", "sessions"]
            if browser_active:
                expected.append("browser-revoke")
            expected.extend(["browser-401", "control-logout", "control-401", "sql-proof", "private-logs", "core-cleanup"])
            with self.subTest(browser_active=browser_active):
                self.assertEqual(events, expected)
                self.assertEqual(runner.report["status"], "passed")
                self.assertEqual(runner.report["cleanup"]["native_sessions_revoked_count"], 2)
                self.assertIs(runner.report["cleanup"]["credential_invalidation_http_401_and_sql_proven"], True)

    def test_failed_browser_revocation_still_logs_out_control_and_calls_frozen_cleanup(self):
        runner = self.runner()
        events = self.cleanup_fakes(runner, browser_active=True, fail_browser=True)
        runner.cleanup()
        self.assertEqual(events, ["original-control", "restore", "sessions", "browser-revoke", "browser-401",
                                  "control-logout", "control-401", "sql-proof", "private-logs", "core-cleanup"])
        self.assertEqual(runner.report["status"], "failed")
        self.assertFalse(runner.report["cleanup"]["all_issued_credentials_revoked_control_logout_last"])
        self.assertNotIn("credential_invalidation_http_401_and_sql_proven", runner.report["cleanup"])

    def test_cleanup_never_mints_a_replacement_login_and_preserves_disposal_after_restore_failure(self):
        for failing in ("control", "restore"):
            runner = self.runner()
            events = self.cleanup_fakes(runner, fail_control=failing == "control", fail_restore=failing == "restore")
            self.replace(runner, "control_login", lambda: self.fail("Cleanup minted a replacement native credential"))
            runner.cleanup()
            with self.subTest(failure=failing):
                self.assertEqual(events[-1], "core-cleanup")
                self.assertIn("control-logout", events)
                self.assertIn("control-401", events)
                self.assertEqual(runner.report["status"], "failed")
                name = "original_control_available_for_cleanup" if failing == "control" else "acknowledged_settings_chain_restored"
                self.assertFalse(runner.report["cleanup"][name])

    def test_cleanup_rejects_extra_issued_credentials_and_unrevoked_database_rows(self):
        for kind in ("extra-session", "unrevoked"):
            runner = self.runner()
            events = self.cleanup_fakes(runner, extra_session=kind == "extra-session", unrevoked=kind == "unrevoked")
            runner.cleanup()
            with self.subTest(kind=kind):
                self.assertEqual(events[-1], "core-cleanup")
                self.assertEqual(runner.report["status"], "failed")
                self.assertFalse(runner.report["cleanup"]["all_issued_credentials_revoked_control_logout_last"])

    def test_browser_revocation_requires_exact_two_independent_credentials(self):
        runner = self.runner()
        runner.browser_result = self.browser_result()
        runner.control_session_id, runner.control_csrf = "e" * 32, "synthetic-control-csrf"
        runner.administrator = {"Id": "f" * 32}
        sessions = [{"Id": runner.control_session_id, "Kind": "admin", "UserId": runner.administrator["Id"], "Status": "active"},
                    {"Id": runner.browser_result["BrowserSessionId"], "Kind": "admin", "UserId": runner.administrator["Id"], "Status": "revoked"}]
        calls = []
        self.replace(runner, "request", lambda route, **options: calls.append((route, options)))
        self.replace(runner, "http_json", lambda route, **options: {"CSRFToken": runner.control_csrf, "User": runner.administrator})
        self.replace(runner, "list_sessions", lambda: copy.deepcopy(sessions))
        runner.verify_browser_revoked()
        self.assertEqual(calls, [("/admin/v1/session", {"headers": {"Cookie": runner.browser_result["BrowserCookie"]}, "expected": (401,)})])
        for index, field, bad in ((0, "Status", "revoked"), (1, "Status", "active"), (1, "Kind", "application"),
                                  (1, "UserId", "a" * 32), (1, "Id", "a" * 32)):
            original = sessions[index][field]
            sessions[index][field] = bad
            with self.subTest(field=field, index=index), self.assertRaises(CORE.VerificationError):
                runner.verify_browser_revoked()
            sessions[index][field] = original
        sessions.append(copy.deepcopy(sessions[1]))
        with self.assertRaises(CORE.VerificationError):
            runner.verify_browser_revoked()

    def test_report_scrubs_secrets_from_keys_values_and_nested_failure_diagnostics(self):
        runner = self.runner()
        secret = 'synthetic-private/+value"'
        runner.secrets.append(secret)
        runner.report.update({secret: {"nested": [{"failure": variant} for variant in MODULE.secret_variants([secret])]},
                              "status": "failed"})
        runner.sanitize_report()
        exported = MODULE.canonical(runner.report).encode()
        self.assertFalse(MODULE.contains_secret(exported, [secret]))
        self.assertIn("[REDACTED]", runner.report)

    def test_no_redirect_handler_refuses_every_followup_location(self):
        handler = MODULE.NoRedirect()
        for code in (301, 302, 303, 307, 308):
            with self.subTest(code=code):
                self.assertIsNone(handler.redirect_request(None, None, code, "Synthetic redirect", {}, "http://example.invalid/private"))

    def test_wrapper_contains_no_shared_service_mutation_or_shell_execution(self):
        service_commands = []
        for node in ast.walk(SOURCE_TREE):
            if isinstance(node, (ast.List, ast.Tuple)) and node.elts and isinstance(node.elts[0], ast.Constant) and node.elts[0].value == "systemctl":
                self.assertGreaterEqual(len(node.elts), 2)
                self.assertIsInstance(node.elts[1], ast.Constant)
                self.assertEqual(node.elts[1].value, "show")
                service_commands.append(node)
            if isinstance(node, ast.Call):
                self.assertFalse(any(option.arg == "shell" and not (
                    isinstance(option.value, ast.Constant) and option.value.value is False) for option in node.keywords))
        self.assertEqual(len(service_commands), 1)

    def test_entry_point_has_explicit_prepared_inputs_and_a_main_guard(self):
        main = function_node("main")
        options = {argument.value for call in ast.walk(main) if isinstance(call, ast.Call)
                   and called_name(call.func).endswith(".add_argument") for argument in call.args
                   if isinstance(argument, ast.Constant) and isinstance(argument.value, str)}
        self.assertTrue({"--snapshot", "--binary", "--binary-sha256", "--assets", "--assets-sha256", "--core-runner"} <= options)
        guards = [node for node in SOURCE_TREE.body if isinstance(node, ast.If)
                  and isinstance(node.test, ast.Compare)
                  and isinstance(node.test.left, ast.Name) and node.test.left.id == "__name__"
                  and len(node.test.ops) == 1 and isinstance(node.test.ops[0], ast.Eq)
                  and len(node.test.comparators) == 1 and isinstance(node.test.comparators[0], ast.Constant)
                  and node.test.comparators[0].value == "__main__"]
        self.assertEqual(len(guards), 1)
        self.assertTrue(any(isinstance(node, ast.Call) and called_name(node.func) == "main"
                            for node in ast.walk(guards[0])))


def main():
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        print(json.dumps({"suite": "observability-browser-guards", "status": "blocked",
                          "reason": "Authorized root SSH is required", "liveBrowserAcceptance": False}))
        raise SystemExit(2)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("--core-runner", type=Path)
    arguments = parser.parse_args()
    source = arguments.source.resolve(strict=True)
    core_source = (arguments.core_runner or source.with_name("verify-managed-users.py")).resolve(strict=True)
    sources = {str(path): path.read_bytes() for path in (source, core_source)}
    suite_bytes = Path(__file__).read_bytes()
    for filename, content in sources.items():
        SOURCE_LINES[filename] = content.decode().splitlines(keepends=True)
        SOURCE_HASHES[Path(filename).name] = hashlib.sha256(content).hexdigest()
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    code = {}

    class MemoryLoader:
        def __init__(self, filename):
            if filename not in code:
                raise AssertionError("Unexpected verifier dependency: " + filename)
            self.filename = filename

        def create_module(self, specification):
            return None

        def exec_module(self, module):
            module.__file__ = self.filename
            exec(code[self.filename], module.__dict__)

    def specification(name, filename, **options):
        if options:
            raise AssertionError("Unexpected source import options")
        return importlib.util.spec_from_loader(name, MemoryLoader(str(filename)), origin=str(filename))

    global MODULE, CORE, SOURCE_TREE, SOURCE_TEXT
    MODULE = types.ModuleType("synthetic_observability_browser_verifier")
    MODULE.__file__ = str(source)
    result, import_failure = unittest.TestResult(), None
    with memory_tracebacks():
        try:
            code.update({filename: compile(content, filename, "exec") for filename, content in sources.items()})
            SOURCE_TEXT = sources[str(source)].decode()
            SOURCE_TREE = ast.parse(SOURCE_TEXT, filename=str(source))
            with EffectFence(), patch.object(importlib.util, "spec_from_file_location", specification):
                exec(code[str(source)], MODULE.__dict__)
                CORE = MODULE.load_core(core_source)
        except BaseException as error:
            import_failure = {"type": type(error).__name__, "message": str(error)[:1000]}
        else:
            unittest.defaultTestLoader.loadTestsFromTestCase(ObservabilityBrowserGuards).run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in failures[:8]]
    passed = import_failure is None and result.wasSuccessful() and result.testsRun > 0 and not result.skipped
    print(json.dumps({"suite": "observability-browser-guards", "status": "passed" if passed else "failed",
        "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors), "skipped": len(result.skipped),
        "sourceSha256": SOURCE_HASHES, "suiteSha256": hashlib.sha256(suite_bytes).hexdigest(),
        "httpRequests": 0, "subprocesses": 0, "databaseStatements": 0, "captureWrites": 0,
        "fixtures": "synthetic-memory-only", "liveBrowserAcceptance": False,
        "importFailure": import_failure, "failureSummaries": summaries, "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
