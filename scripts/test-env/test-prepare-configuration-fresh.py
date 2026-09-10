#!/usr/bin/env python3
"""Exercise fresh configuration fixture safety with memory dependencies only.

Run through authorized root SSH with one preparation operator source path.
Only this suite, that operator, and the four pinned recorder sources are read
before the effect fence. Passing proves synthetic contracts, not preparation,
cleanup, configuration experiments, or HTTP acceptance on a live instance.
"""

from __future__ import annotations

import _io
import argparse
import base64
import contextlib
import copy
import datetime
import hashlib
import http.client
import importlib.util
import io
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
from urllib.parse import parse_qs, quote, unquote, unquote_plus, urlencode, urlsplit, urlunsplit

sys.dont_write_bytecode = True
OPERATOR: types.ModuleType
SOURCE_LINES: dict[str, list[str]] = {}
SOURCE_HASHES: dict[str, str] = {}


@contextlib.contextmanager
def memory_tracebacks():
    """Keep source lookup in memory while import and cleanup errors render."""
    with patch.object(linecache, "checkcache", lambda filename=None: None), \
         patch.object(linecache, "lazycache", lambda filename, module_globals: False), \
         patch.object(linecache, "getlines", lambda filename, module_globals=None:
                      list(SOURCE_LINES.get(str(filename), ()))):
        yield


class EffectFence(contextlib.ExitStack):
    """Reject every unfaked filesystem, process, signal, or network effect."""

    def __init__(self) -> None:
        super().__init__()
        self.violations: list[str] = []

    def __enter__(self) -> EffectFence:
        super().__enter__()
        import builtins

        self.enter_context(memory_tracebacks())
        fence = self

        class RejectUncachedImports:
            def find_spec(self, fullname: str, path: object = None, target: object = None) -> object:
                fence.violations.append("import:" + fullname)
                raise AssertionError("Uncached import is outside the memory fixture: " + fullname)

        self.enter_context(patch.object(sys, "meta_path", [RejectUncachedImports()]))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")), (_io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (signal, ("signal", "setitimer", "pthread_sigmask")), (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "stat", "lstat", "fstat", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "fsync", "umask", "kill", "killpg", "system", "popen", "fork", "posix_spawn",
                  "posix_spawnp", "execve", "execvp", "replace", "rename", "mkdir", "makedirs", "remove", "unlink",
                  "rmdir", "chmod", "chown", "link", "symlink", "truncate")),
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

                def denied(*_args: object, _label: str = label, **_kwargs: object) -> object:
                    self.violations.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments: object) -> None:
        super().__exit__(*arguments)
        if self.violations:
            raise AssertionError("External effects were attempted: " + ", ".join(self.violations))


class ConfigurationFixtureSafetyTests(unittest.TestCase):
    def setUp(self) -> None:
        fence = EffectFence()
        fence.__enter__()
        self.addCleanup(fence.__exit__, None, None, None)
        output = contextlib.redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)
        self.saved: dict[Path, object] = {}
        self.replace(OPERATOR, "save", self.memory_save)
        self.replace(OPERATOR, "atomic_save", self.memory_save)
        self.replace(OPERATOR, "utc", lambda: "2042-01-02T03:04:05+00:00")

    def replace(self, owner: object, name: str, value: object) -> object:
        replacement = patch.object(owner, name, value)
        replacement.start()
        self.addCleanup(replacement.stop)
        return value

    def memory_save(self, path: Path, value: object, **_kwargs: object) -> None:
        self.assertNotIn(path, self.saved, "Prior evidence must not be overwritten")
        self.saved[path] = copy.deepcopy(value)

    def memory_removal(self) -> types.SimpleNamespace:
        directories = {OPERATOR.ROOT, OPERATOR.DATA, OPERATOR.DATA / "config"}
        files = {OPERATOR.ROOT / ".goby-managed", OPERATOR.DATA / ".goby-managed", OPERATOR.DATA / "config/system.xml"}
        metadata = {path: types.SimpleNamespace(st_uid=0, st_nlink=1, st_dev=41, st_ino=index,
                                                st_mode=(stat.S_IFDIR | 0o700) if path in directories else stat.S_IFREG | 0o600)
                    for index, path in enumerate(sorted(directories | files), 1)}
        self.saved[OPERATOR.DATA_IDENTITY] = {"path": str(OPERATOR.DATA), "device": metadata[OPERATOR.DATA].st_dev,
                                               "inode": metadata[OPERATOR.DATA].st_ino, "marker": OPERATOR.MARKER}
        fixture = types.SimpleNamespace(metadata=metadata, symlinks=set(), mounts=set(), deleted=[])
        self.replace(OPERATOR, "properties", lambda unit: {"MainPID": "0", "ActiveState": "inactive"})
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(self.saved[path]))
        self.replace(Path, "exists", lambda path: path in metadata or path in self.saved)
        self.replace(Path, "resolve", lambda path, **kwargs: Path("/synthetic/foreign-target") if path in fixture.symlinks else path)
        self.replace(Path, "is_symlink", lambda path: path in fixture.symlinks)
        self.replace(Path, "lstat", lambda path: metadata[path])
        self.replace(Path, "stat", lambda path, **kwargs: metadata[path])
        self.replace(Path, "is_dir", lambda path: stat.S_ISDIR(metadata[path].st_mode))
        self.replace(Path, "read_text", lambda path, **kwargs: OPERATOR.MARKER + "\n"
                     if path in {OPERATOR.ROOT / ".goby-managed", OPERATOR.DATA / ".goby-managed"}
                     else self.fail("Unexpected ownership-marker read"))
        self.replace(Path, "rglob", lambda path, pattern: iter(sorted(entry for entry in metadata if path in entry.parents))
                     if path == OPERATOR.DATA and pattern == "*" else self.fail("Unexpected removal enumeration"))
        self.replace(os.path, "ismount", lambda path: path in fixture.mounts)

        def unlink(path: Path) -> None:
            self.assertIn(OPERATOR.DATA, path.parents)
            self.assertTrue(stat.S_ISREG(metadata[path].st_mode))
            fixture.deleted.append(path)
            del metadata[path]

        def rmdir(path: Path) -> None:
            self.assertTrue(path == OPERATOR.DATA or OPERATOR.DATA in path.parents)
            self.assertFalse(any(path in entry.parents for entry in metadata))
            fixture.deleted.append(path)
            del metadata[path]

        self.replace(Path, "unlink", unlink)
        self.replace(Path, "rmdir", rmdir)
        return fixture

    def bootstrap_for_requests(self):
        identity = {"networkNamespace": "net:[synthetic-fresh]"}
        self.replace(time, "monotonic", lambda: 100.0)
        self.replace(OPERATOR, "same_new_identity", lambda expected: self.assertEqual(expected, identity))
        self.replace(os, "readlink", lambda path: identity["networkNamespace"]
                     if str(path) == "/proc/self/ns/net" else self.fail("Unexpected namespace observation"))
        self.replace(OPERATOR, "configuration_sanitizer", Mock())
        self.replace(OPERATOR.Bootstrap, "sanitize", lambda recorder, value, *args, **kwargs: copy.deepcopy(value))
        recorder = OPERATOR.Bootstrap(identity)
        recorder.server_id = "synthetic-distinct-fresh-server"
        recorder.tokens["admin"] = "synthetic-owned-admin-token"
        return recorder

    def bootstrap_with_real_sanitizer(self):
        def source_info(path: Path, **_options: object) -> types.SimpleNamespace:
            self.assertIn(str(path), SOURCE_HASHES)
            return types.SimpleNamespace(st_uid=0, st_mode=stat.S_IFREG | 0o644)

        self.replace(time, "monotonic", lambda: 100.0)
        self.replace(Path, "resolve", lambda path, **kwargs: path)
        self.replace(OPERATOR, "canonical", source_info)
        self.replace(OPERATOR, "digest", lambda path: SOURCE_HASHES[str(path)])
        sanitizer = OPERATOR.configuration_sanitizer()
        for method in (sanitizer.collect_secrets, sanitizer.sanitize):
            self.assertEqual(Path(method.__func__.__code__.co_filename).name, "reference-configuration.py")
        self.assertEqual(sanitizer.secrets, set())
        self.replace(OPERATOR, "configuration_sanitizer", lambda: sanitizer)
        return OPERATOR.Bootstrap({"networkNamespace": "net:[synthetic-fresh]"}), sanitizer

    def test_fixed_resources_use_only_the_new_configuration_namespace(self) -> None:
        parent = Path("/opt/goby-test/exec-work-m5g")
        self.assertEqual(OPERATOR.ROOT, parent / "emby-configuration-fresh-m5g-20260910-01")
        self.assertEqual(OPERATOR.DATA, parent / "emby-configuration-fresh-data-01")
        self.assertEqual(OPERATOR.UNIT, "goby-emby-configuration-fresh-m5g-20260910-01.service")
        self.assertEqual(OPERATOR.RUNTIME, OPERATOR.ROOT / "runtime")
        self.assertNotEqual(OPERATOR.DATA, OPERATOR.OLD_DATA)
        for name in ("SOURCE", "SOURCE_INPUT", "SOURCE_BATCHES", "prepare_source", "source_snapshot"):
            self.assertFalse(hasattr(OPERATOR, name), "Configuration setup must not retain media source modes: " + name)

    def test_prepare_refuses_existing_roots_symlinks_or_unit_before_creation(self) -> None:
        class AdmissionReached(Exception):
            pass

        present, symlinks = set(), set()
        state = {"LoadState": "not-found"}
        self.replace(OPERATOR, "ensure_work_parent", Mock())
        self.replace(OPERATOR, "common_preconditions", Mock())
        self.replace(OPERATOR, "verify_sanitizer_sources", Mock())
        self.replace(Path, "exists", lambda path: path in present)
        self.replace(Path, "is_symlink", lambda path: path in symlinks)
        self.replace(OPERATOR, "properties", lambda unit: state if unit == OPERATOR.UNIT else self.fail("Unexpected unit"))
        admission = Mock(side_effect=AdmissionReached())
        created = Mock(side_effect=AssertionError("Existing fixture resources cannot be reused"))
        service = Mock(side_effect=AssertionError("Existing fixture resources cannot dispatch a service"))
        self.replace(OPERATOR, "old_services", admission)
        self.replace(OPERATOR, "create_owned_root", created)
        self.replace(OPERATOR, "run", service)
        for root in (OPERATOR.ROOT, OPERATOR.DATA):
            for kind, collection in (("existing", present), ("symlink", symlinks)):
                with self.subTest(root=str(root), kind=kind):
                    collection.add(root)
                    with self.assertRaises(RuntimeError):
                        OPERATOR.prepare()
                    collection.clear()
                    admission.assert_not_called()
        state["LoadState"] = "loaded"
        with self.assertRaises(RuntimeError):
            OPERATOR.prepare()
        admission.assert_not_called()
        state["LoadState"] = "not-found"
        with self.assertRaises(AdmissionReached):
            OPERATOR.prepare()
        created.assert_not_called()
        service.assert_not_called()
        self.assertEqual(self.saved, {})

    def test_bootstrap_refuses_configuration_and_other_experiments_before_http(self) -> None:
        recorder = self.bootstrap_for_requests()
        connection = Mock(side_effect=AssertionError("Forbidden bootstrap routes cannot reach HTTP"))
        self.replace(http.client, "HTTPConnection", connection)
        requests = (("POST", "/emby/System/Configuration"), ("POST", "/emby/System/Configuration/branding"),
                    ("POST", "/emby/System/Configuration/encoding"), ("POST", "/emby/Users/admin/Configuration"),
                    ("POST", "/emby/Users/admin/Policy"), ("POST", "/emby/ScheduledTasks/Running/task"),
                    ("DELETE", "/emby/ScheduledTasks/Running/task"), ("POST", "/emby/Library/VirtualFolders"),
                    ("DELETE", "/emby/Users/admin"), ("POST", "/emby/Startup/User?unexpected=1"),
                    ("POST", "/emby/Users/New?Name=unowned"), ("GET", "https://outside.invalid/emby/Users"))
        for method, route in requests:
            with self.subTest(method=method, route=route):
                with self.assertRaises(RuntimeError):
                    recorder.request("forbidden", method, route, body={}, token=recorder.tokens["admin"])
                connection.assert_not_called()
        self.assertEqual(self.saved, {})

    def test_bootstrap_allows_setup_routes_only_with_fresh_identity_and_owned_tokens(self) -> None:
        recorder = self.bootstrap_for_requests()
        requests = []

        class Response:
            status = 200

            def __init__(self) -> None:
                self.body = io.BytesIO(b"{}")

            def getheaders(self) -> list:
                return [("Content-Length", "2")]

            def read(self, maximum: int) -> bytes:
                return self.body.read(maximum)

        class Connection:
            def request(self, method: str, route: str, **options: object) -> None:
                requests.append((method, route, options))

            def getresponse(self) -> Response:
                return Response()

            def close(self) -> None:
                pass

        connection = Mock(return_value=Connection())
        self.replace(http.client, "HTTPConnection", connection)
        self.replace(signal, "setitimer", Mock())
        allowed = (("GET", "/emby/System/Info/Public", {}),
                   ("GET", "/emby/Startup/User", {"account": "admin"}),
                   ("GET", "/emby/System/Configuration", {"token": recorder.tokens["admin"]}),
                   ("POST", "/emby/Startup/User", {"account": "admin", "form": True,
                                                    "body": {"Name": OPERATOR.ADMIN_NAME, "Password": "synthetic-new-password"}}),
                   ("POST", "/emby/Startup/RemoteAccess", {"account": "admin", "form": True,
                                                            "body": {"EnableAutomaticPortMapping": "false"}}),
                   ("POST", "/emby/Startup/Complete", {"account": "admin"}),
                   ("POST", "/emby/Users/AuthenticateByName", {"account": "admin",
                                                              "body": {"Username": OPERATOR.ADMIN_NAME, "Pw": "synthetic-new-password"}}),
                   ("POST", "/emby/Users/New", {"token": recorder.tokens["admin"], "body": {"Name": OPERATOR.VIEWER_NAME}}),
                   ("POST", "/emby/Sessions/Logout", {"token": recorder.tokens["admin"]}))
        for index, (method, route, options) in enumerate(allowed):
            with self.subTest(method=method, route=route):
                self.assertEqual(recorder.request("allowed-" + str(index), method, route, **options), (200, {}))
        self.assertEqual([(method, route) for method, route, _options in requests], [(method, route) for method, route, _ in allowed])
        self.assertEqual(connection.call_count, len(allowed))
        for server_id in (None, OPERATOR.OLD_SERVER_ID):
            recorder.server_id = server_id
            with self.assertRaises(RuntimeError):
                recorder.request("unproven-server", "POST", "/emby/Startup/Complete", account="admin")
        recorder.server_id = "synthetic-distinct-fresh-server"
        with self.assertRaises(RuntimeError):
            recorder.request("foreign-token", "GET", "/emby/Users", token="synthetic-foreign-token")
        self.assertEqual(connection.call_count, len(allowed))

    def test_real_sanitizer_collects_deep_headers_before_redacting_echoes_and_dictionary_keys(self) -> None:
        recorder, _sanitizer = self.bootstrap_with_real_sanitizer()
        values = ("synthetic-dict-secret", "synthetic-pair-secret", "synthetic-descriptor-secret",
                  "synthetic-key-secret", "synthetic-forged-secret")
        # The public control field must not itself end in a sensitive suffix.
        body = {"Diagnostic": "Bearer " + " | ".join(values),
                "Nested": [{"Layer": {"RequestHeaders": {"Authorization": "Bearer " + values[0], "X-Public": "keep"}}},
                           {"Layer": {"ProxyHeaders": [["X-Api-Key", values[1]], ["X-Public", "keep"]]}},
                           {"Layer": {"OutboundHeaders": [{"Name": "Authorization", "Value": values[2]}]}}],
                "Key": values[3], "diagnostic-" + values[3] + "-echo": "keep",
                "": {"request": {"method": "GET", "path": "/emby/ScheduledTasks"},
                     "response": {"status": 200, "bodyType": "json",
                                  "body": [{"Id": "task", "Name": "Task", "State": "Idle", "Key": values[4]}]}}}
        record = {"request": {"method": "GET", "path": "/emby/System/Configuration"},
                  "response": {"status": 200, "bodyType": "json", "body": body}}
        before = copy.deepcopy(record)
        exported = recorder.sanitize(record)
        cleaned = exported["response"]["body"]
        for secret in values:
            self.assertNotIn(secret, json.dumps(exported))
        self.assertEqual(cleaned["Nested"][0]["Layer"]["RequestHeaders"]["X-Public"], "keep")
        self.assertEqual(cleaned["Nested"][1]["Layer"]["ProxyHeaders"][1], ["X-Public", "keep"])
        self.assertEqual(cleaned["Key"], "[REDACTED_SECRET]")
        self.assertEqual(cleaned[""]["response"]["body"][0]["Key"], "[REDACTED_SECRET]")
        self.assertEqual(cleaned["diagnostic-[REDACTED_SECRET]-echo"], "keep")
        self.assertNotIn("keep", recorder.secrets)
        self.assertEqual(record, before)
        self.assertEqual(self.saved, {})

    def test_real_sanitizer_redacts_encoded_urls_and_refuses_dictionary_key_collisions(self) -> None:
        recorder, sanitizer = self.bootstrap_with_real_sanitizer()
        absolute = ("https://synthetic-user:synthetic-password@example.invalid/path?code=synthetic-code"
                    "&access_token=synthetic-url-secret#synthetic-fragment")
        encoded = quote(absolute, safe="")
        for url in (absolute, encoded, quote(encoded, safe="")):
            with self.subTest(url=url):
                recorder.secrets.clear()
                sanitizer.secrets.clear()
                value = {"Diagnostic": "synthetic-user | synthetic-password | synthetic-url-secret", "Url": url}
                exported = recorder.sanitize(value)
                text = json.dumps(exported)
                for _ in range(3):
                    for secret in ("synthetic-user", "synthetic-password", "synthetic-code", "synthetic-url-secret", "synthetic-fragment"):
                        self.assertNotIn(secret, text)
                    text = unquote(text)
                self.assertIn("example.invalid/path", text)
                self.assertIn("[REDACTED_QUERY]", text)
        first, second = "synthetic/first-secret", "synthetic/second-secret"
        collisions = ({"Password": first, "Token": second, first: "one", second: "two"},
                      {"Password": first, "Nested": {"Headers": {first: "one", quote(first, safe=""): "two"}}})
        for value in collisions:
            with self.subTest(value=value):
                with self.assertRaisesRegex(RuntimeError, "Sanitized dictionary key collision"):
                    recorder.sanitize(value)
        self.assertEqual(self.saved, {})

    def test_tree_removal_deletes_only_data_under_its_fixed_parent(self) -> None:
        fixture = self.memory_removal()
        forbidden = (OPERATOR.ROOT, OPERATOR.PRIVATE, OPERATOR.OLD_DATA, OPERATOR.ROOT / "source",
                     Path("/opt/goby-test/exec-scratch/emby-scheduled-tasks-fresh-m5f-20260910-01/source"),
                     OPERATOR.DATA.parent, OPERATOR.DATA.parent / "unrelated-data")
        for folder in forbidden:
            with self.subTest(folder=str(folder)):
                with self.assertRaises(RuntimeError):
                    OPERATOR.remove_owned_tree(folder)
                self.assertEqual(fixture.deleted, [])
        OPERATOR.remove_owned_tree(OPERATOR.DATA)
        self.assertEqual(fixture.deleted, [OPERATOR.DATA / "config/system.xml", OPERATOR.DATA / "config",
                                           OPERATOR.DATA / ".goby-managed", OPERATOR.DATA])
        self.assertIn(OPERATOR.ROOT, fixture.metadata)
        self.assertIn(OPERATOR.ROOT / ".goby-managed", fixture.metadata)

    def test_tree_removal_refuses_symlinks_mounts_and_changed_root_identity(self) -> None:
        fixture = self.memory_removal()
        for path in (OPERATOR.DATA, OPERATOR.DATA / "config/system.xml"):
            for kind, collection in (("symlink", fixture.symlinks), ("mount", fixture.mounts)):
                with self.subTest(path=str(path), kind=kind):
                    collection.add(path)
                    with self.assertRaises(RuntimeError):
                        OPERATOR.remove_owned_tree(OPERATOR.DATA)
                    collection.clear()
                    self.assertEqual(fixture.deleted, [])
        original_inode = fixture.metadata[OPERATOR.DATA].st_ino
        fixture.metadata[OPERATOR.DATA].st_ino += 1
        with self.assertRaises(RuntimeError):
            OPERATOR.remove_owned_tree(OPERATOR.DATA)
        fixture.metadata[OPERATOR.DATA].st_ino = original_inode
        self.assertEqual(fixture.deleted, [])
        proof = self.saved[OPERATOR.PRIVATE / "data-removal-intent.json"]
        proof["inode"] += 1
        with self.assertRaises(RuntimeError):
            OPERATOR.remove_owned_tree(OPERATOR.DATA)
        self.assertEqual(fixture.deleted, [])

    def test_stop_refuses_replacement_invocation_pid_or_start_ticks(self) -> None:
        state = {"LoadState": "loaded", "MainPID": "12345", "ActiveState": "activating", "InvocationID": "a" * 32}
        current = {"pid": 12345, "startTicks": "123", "uid": 0, "cgroup": "0::/system.slice/" + OPERATOR.UNIT,
                   "exe": "/usr/bin/bash", "cmdline": ["/bin/bash", str(OPERATOR.RUNTIME / "launch.sh")]}
        original_state, original_current = copy.deepcopy(state), copy.deepcopy(current)
        expected = {"pid": 12345, "startTicks": "123"}
        self.saved[OPERATOR.DISPATCH] = {"invocationId": "a" * 32}
        self.replace(OPERATOR, "owned_roots", Mock())
        self.replace(OPERATOR, "properties", lambda unit: copy.deepcopy(state))
        self.replace(OPERATOR, "dispatch_identity", lambda properties: {"invocationId": properties["InvocationID"]})
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(self.saved[path]))
        self.replace(Path, "exists", lambda path: path in self.saved)
        self.replace(OPERATOR, "new_identity", Mock(side_effect=AssertionError("Owned stop must not require application readiness")))
        commands = []

        def process(pid: int) -> dict:
            if state["MainPID"] == "0":
                raise FileNotFoundError("Synthetic owned process exited")
            self.assertEqual(pid, current["pid"])
            return copy.deepcopy(current)

        def stop(arguments: list[str], *, timeout: int) -> None:
            self.assertEqual(arguments, ["systemctl", "stop", OPERATOR.UNIT])
            self.assertEqual(timeout, 35)
            commands.append(arguments)
            state.update({"MainPID": "0", "ActiveState": "inactive"})

        self.replace(OPERATOR, "process_identity", process)
        self.replace(OPERATOR, "run", stop)
        for change in ("invocation", "pid", "startTicks", "uid", "cgroup"):
            with self.subTest(change=change):
                if change == "invocation":
                    state["InvocationID"] = "b" * 32
                elif change == "pid":
                    state["MainPID"], current["pid"] = "12346", 12346
                else:
                    current[change] = {"startTicks": "124", "uid": 995, "cgroup": "0::/system.slice/unrelated.service"}[change]
                with self.assertRaises(RuntimeError):
                    OPERATOR.stop_owned(expected)
                self.assertEqual(commands, [])
                state.clear()
                state.update(copy.deepcopy(original_state))
                current.clear()
                current.update(copy.deepcopy(original_current))
        self.assertTrue(OPERATOR.stop_owned(expected)["stopped"])
        self.assertEqual(len(commands), 1)


def main() -> None:
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "configuration-fresh-safety", "status": "blocked",
                          "reason": "Authorized root SSH and one preparation operator source are required",
                          "livePreparationAcceptance": False}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    paths = [source, *[source.with_name(name) for name in
                      ("reference-configuration.py", "reference-scheduled-tasks.py", "reference-devices.py", "reference-api-keys.py")]]
    sources = {str(path): path.read_bytes() for path in paths}
    suite_bytes = Path(__file__).read_bytes()
    for filename, content in sources.items():
        SOURCE_LINES[filename] = content.decode().splitlines(keepends=True)
        SOURCE_HASHES[filename] = hashlib.sha256(content).hexdigest()
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    code = {filename: compile(content, filename, "exec") for filename, content in sources.items()}

    class MemoryLoader:
        def __init__(self, filename: str) -> None:
            if filename not in code:
                raise AssertionError("Unexpected recorder dependency import: " + filename)
            self.filename = filename

        def create_module(self, specification: object) -> None:
            return None

        def exec_module(self, module: types.ModuleType) -> None:
            module.__file__ = self.filename
            exec(code[self.filename], module.__dict__)

    def specification(name: str, filename: object, **options: object) -> object:
        if options:
            raise AssertionError("Unexpected recorder import options")
        return importlib.util.spec_from_loader(name, MemoryLoader(str(filename)), origin=str(filename))

    global OPERATOR
    OPERATOR = types.ModuleType("synthetic_configuration_fresh_operator")
    OPERATOR.__file__ = str(source)
    result = unittest.TestResult()
    import_failure = None
    with memory_tracebacks(), patch.object(importlib.util, "spec_from_file_location", specification), \
         patch.object(os, "environ", {"GOBY_DEVICE_REFERENCE_RUN": "2"}):
        try:
            with EffectFence():
                exec(code[str(source)], OPERATOR.__dict__)
        except BaseException as error:
            import_failure = {"type": type(error).__name__, "message": str(error)[:1000]}
        else:
            unittest.defaultTestLoader.loadTestsFromTestCase(ConfigurationFixtureSafetyTests).run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in failures[:8]]
    passed = import_failure is None and result.wasSuccessful() and result.testsRun > 0
    print(json.dumps({"suite": "configuration-fresh-safety", "status": "passed" if passed else "failed",
                      "testsRun": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "operatorSha256": SOURCE_HASHES[str(source)], "suiteSha256": hashlib.sha256(suite_bytes).hexdigest(),
                      "sourceSha256": {Path(filename).name: digest for filename, digest in SOURCE_HASHES.items()},
                      "realHttpRequests": 0, "realFixtureWrites": 0, "fixtures": "synthetic-memory-only",
                      "livePreparationAcceptance": False, "importFailure": import_failure,
                      "failureSummaries": summaries, "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
