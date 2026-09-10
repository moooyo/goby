#!/usr/bin/env python3
"""Exercise fresh-fixture operator safety using bounded in-memory substitutes.

Run only through authorized root SSH with the operator source path as the only
argument. Source and suite bytes are read once for import and provenance. Every
test fences process, network, signal, filesystem, and waiting entry points before
constructing operator objects. No real fixture, service, credential, or media is
read or changed; all boundary observations after import are synthetic.
"""

from __future__ import annotations

import argparse
import base64
import contextlib
import copy
import datetime
import hashlib
import http.client
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
from urllib.parse import quote, urlencode

sys.dont_write_bytecode = True
MODULE: types.ModuleType
OPERATOR_CODE: types.CodeType
SOURCE_LINES: dict[str, list[str]] = {}


class EffectFence:
    """Deny external effects, including constructors, unless explicitly faked."""

    def __init__(self) -> None:
        self.stack = contextlib.ExitStack()
        self.boundaries: dict[str, Mock] = {}
        self.violations: list[str] = []

    def install(self, owner: object, name: str, label: str, *, bound: bool = False) -> None:
        if not hasattr(owner, name):
            return

        def denied(*_args: object, **_kwargs: object) -> object:
            self.violations.append(label)
            raise AssertionError("Unexpected external effect: " + label)

        boundary = Mock(name=label, side_effect=denied)
        self.boundaries[label] = boundary
        if bound:
            def forwarding(*args: object, **kwargs: object) -> object:
                return boundary(*args, **kwargs)
            replacement = forwarding
        else:
            replacement = boundary
        self.stack.enter_context(patch.object(owner, name, replacement))

    def __enter__(self) -> EffectFence:
        import builtins

        # unittest formats failures before addCleanup restores this fence.
        # Keep traceback source lookup entirely in memory, including frames
        # from libraries whose source was intentionally never read by the suite.
        self.stack.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.stack.enter_context(patch.object(linecache, "lazycache", lambda filename, module_globals: False))
        self.stack.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                             list(SOURCE_LINES.get(str(filename), ()))))
        self.install(builtins, "open", "builtins.open")
        for name in ("open", "fdopen", "write", "fsync", "readlink", "walk", "listdir", "scandir", "stat", "lstat",
                     "mkdir", "makedirs", "unlink", "remove", "rmdir", "rename", "replace", "chmod", "chown",
                     "truncate", "umask", "kill", "killpg", "system", "popen", "fork", "forkpty", "posix_spawn",
                     "posix_spawnp", "execl", "execle", "execlp", "execlpe", "execv", "execve", "execvp", "execvpe",
                     "spawnl", "spawnle", "spawnlp", "spawnlpe", "spawnv", "spawnve", "spawnvp", "spawnvpe"):
            self.install(os, name, "os." + name)
        self.install(os.path, "ismount", "os.path.ismount")
        for name in ("run", "Popen", "call", "check_call", "check_output"):
            self.install(subprocess, name, "subprocess." + name)
        for name in ("socket", "create_connection", "getaddrinfo"):
            self.install(socket, name, "socket." + name)
        for name in ("HTTPConnection", "HTTPSConnection"):
            self.install(http.client, name, "http.client." + name)
        for name in ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage"):
            self.install(shutil, name, "shutil." + name)
        for name in ("signal", "setitimer"):
            self.install(signal, name, "signal." + name)
        self.install(time, "sleep", "time.sleep")
        for name in ("exists", "is_file", "is_dir", "is_symlink", "stat", "lstat", "resolve", "read_text",
                     "read_bytes", "glob", "rglob", "iterdir", "open", "mkdir", "unlink", "rmdir", "chmod",
                     "touch", "rename", "replace", "write_text", "write_bytes"):
            self.install(Path, name, "Path." + name, bound=True)
        return self

    def allow(self, label: str, implementation: object = None) -> Mock:
        boundary = self.boundaries[label]
        boundary.side_effect = implementation if callable(implementation) else None
        boundary.return_value = implementation if not callable(implementation) else None
        return boundary

    def __exit__(self, *arguments: object) -> None:
        self.stack.__exit__(*arguments)
        if self.violations:
            raise AssertionError("Unfaked external effects: " + ", ".join(self.violations))


class FakeResponse:
    def __init__(self, status: int, body: object = None, *, headers: list[tuple[str, str]] | None = None,
                 incomplete: bool = False) -> None:
        self.status = status
        self.body = b"" if body is None else body if isinstance(body, bytes) else json.dumps(body).encode()
        self.headers = headers if headers is not None else [("Content-Length", str(len(self.body)))]
        self.incomplete, self.offset = incomplete, 0

    def getheaders(self) -> list[tuple[str, str]]:
        return list(self.headers)

    def read(self, limit: int) -> bytes:
        if self.incomplete:
            raise http.client.IncompleteRead(self.body, len(self.body) + 1)
        value = self.body[self.offset:self.offset + limit]
        self.offset += len(value)
        return value


class FakeConnection:
    def __init__(self, response: FakeResponse) -> None:
        self.response, self.requests, self.closed = response, [], False

    def request(self, method: str, route: str, *, body: bytes | None, headers: dict) -> None:
        self.requests.append({"method": method, "route": route, "body": body, "headers": copy.deepcopy(headers)})

    def getresponse(self) -> FakeResponse:
        return self.response

    def close(self) -> None:
        self.closed = True


class FreshOperatorSafetyTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fence = EffectFence()
        self.fence.__enter__()
        self.addCleanup(self.fence.__exit__, None, None, None)
        self.output = io.StringIO()
        self.redirect = contextlib.redirect_stdout(self.output)
        self.redirect.__enter__()
        self.addCleanup(self.redirect.__exit__, None, None, None)
        self.saved: dict[Path, object] = {}
        self.save_order: list[Path] = []
        self.replace("save", self.memory_save)
        self.replace("utc", "2042-01-02T03:04:05+00:00")

    def replace(self, name: str, implementation: object = None) -> Mock:
        substitute = Mock(name=name, side_effect=implementation if callable(implementation) else None,
                          return_value=implementation if not callable(implementation) else None)
        replacement = patch.object(MODULE, name, substitute)
        replacement.start()
        self.addCleanup(replacement.stop)
        return substitute

    def memory_save(self, path: Path, value: object, **_kwargs: object) -> None:
        self.assertNotIn(path, self.saved, "Evidence must not be overwritten")
        self.saved[path] = copy.deepcopy(value)
        self.save_order.append(path)

    def intent(self) -> dict:
        return {"referenceRun": MODULE.REFERENCE_RUN, "marker": MODULE.MARKER, "unit": MODULE.UNIT, "programData": str(MODULE.DATA),
                "evidenceRoot": str(MODULE.ROOT), "oldServices": {"synthetic": "unchanged"},
                "oldBaseline": {"synthetic": "preserved"},
                "previousFailureProvenance": None if MODULE.REFERENCE_RUN == 1 else {"synthetic": "first-attempt-preserved"}}

    def state(self) -> dict:
        return {"LoadState": "loaded", "ActiveState": "activating", "SubState": "start", "MainPID": "12345",
                "InvocationID": "a" * 32, "Id": MODULE.UNIT, "Description": MODULE.DESCRIPTION,
                "WorkingDirectory": str(MODULE.RUNTIME),
                "ExecStart": "{ path=" + str(MODULE.RUNTIME / "launch.sh") + " ; argv[]=owned-launcher ; }",
                "ControlGroup": "/system.slice/" + MODULE.UNIT, "MemoryMax": str(512 * MODULE.MIB),
                "PrivateNetwork": "yes", "PrivateTmp": "yes", "NoNewPrivileges": "yes", "ProtectSystem": "strict",
                "ProtectHome": "yes", "KillMode": "control-group", "TasksMax": "256", "LimitNOFILE": "65536",
                "UMask": "0077", "ReadWritePaths": str(MODULE.DATA) + " " + str(MODULE.RUNTIME),
                "ReadOnlyPaths": " ".join(sorted(MODULE.READ_ONLY_PATHS))}

    def launcher_process(self) -> dict:
        return {"pid": 12345, "startTicks": "900001", "uid": 0, "exe": "/usr/bin/bash",
                "cmdline": ["/bin/bash", str(MODULE.RUNTIME / "launch.sh")],
                "cgroup": "0::/system.slice/" + MODULE.UNIT + "\n", "networkNamespace": "net:[fresh]"}

    def install_stop_fixture(self, *, dispatch_exists: bool = True, main_pid: int = 12345,
                             final_state: dict | None = None) -> types.SimpleNamespace:
        fixture = types.SimpleNamespace(state=self.state(), process=self.launcher_process(), stopped=False, commands=[])
        fixture.state["MainPID"] = str(main_pid)
        witness = {"unit": MODULE.UNIT, "invocationId": "a" * 32, "controlGroup": "/system.slice/" + MODULE.UNIT,
                   "launcherSha256": "1" * 64, "programData": str(MODULE.DATA),
                   "serviceProperties": {name: fixture.state.get(name, "") for name in MODULE.STATIC_PROPERTIES if name != "ExecStart"}}
        records = {MODULE.INTENT: self.intent(), MODULE.PRIVATE / "launcher-manifest.json": {"sha256": "1" * 64},
                   MODULE.DISPATCH: witness}
        self.replace("owned_roots")
        self.replace("load", lambda path: copy.deepcopy(records[path]))
        self.replace("digest", lambda path: "1" * 64 if path == MODULE.RUNTIME / "launch.sh" else self.fail("Unexpected digest path"))
        self.replace("new_identity", lambda: self.fail("Stopping must not require application readiness"))
        self.fence.allow("Path.exists", lambda path: path == MODULE.DISPATCH and dispatch_exists or path in self.saved)

        def properties(unit: str) -> dict:
            self.assertEqual(unit, MODULE.UNIT)
            if fixture.stopped:
                return copy.deepcopy(final_state or {"LoadState": "not-found", "MainPID": "0", "ActiveState": "inactive"})
            return copy.deepcopy(fixture.state)

        def process_identity(pid: int) -> dict:
            self.assertEqual(pid, 12345, "A protected or unrelated process must never be inspected for stopping")
            if fixture.stopped:
                raise FileNotFoundError("Synthetic owned process exited")
            return copy.deepcopy(fixture.process)

        def run(arguments: list[str], *, timeout: int) -> str:
            self.assertEqual(arguments, ["systemctl", "stop", MODULE.UNIT])
            self.assertEqual(timeout, 35)
            fixture.commands.append(list(arguments))
            fixture.stopped = True
            return ""

        self.replace("properties", properties)
        fixture.process_mock = self.replace("process_identity", process_identity)
        self.replace("run", run)
        return fixture

    def install_cleanup_fixture(self) -> types.SimpleNamespace:
        fixture = types.SimpleNamespace(data_exists=True, events=[])
        self.replace("common_preconditions")
        self.replace("owned_root")
        self.replace("owned_roots")
        self.replace("canonical")
        self.replace("recover_empty_removal_root")
        self.replace("load", lambda path: self.intent() if path == MODULE.INTENT else self.fail("Unexpected cleanup input"))
        self.fence.allow("Path.exists", lambda path: fixture.data_exists if path == MODULE.DATA else
                         path in {MODULE.ROOT, MODULE.PRIVATE, MODULE.INTENT} or path in self.saved)
        self.fence.allow("Path.is_symlink", False)
        self.replace("properties", {"LoadState": "not-found", "MainPID": "0", "ActiveState": "inactive"})
        fixture.stop = self.replace("stop_owned", lambda expected: fixture.events.append("stop") or {"stopped": True})
        fixture.remove = self.replace("remove_owned_data", lambda: self.fail("Unexpected data removal"))
        self.replace("verify_baseline")
        self.replace("verify_package")
        self.replace("verify_previous_failure")
        return fixture

    def install_network(self, responses: list[FakeResponse]) -> types.SimpleNamespace:
        fixture = types.SimpleNamespace(connections=[], responses=list(responses), now=100.0)
        self.replace("same_new_identity")
        self.fence.allow("os.readlink", lambda path: "net:[fresh]" if str(path) == "/proc/self/ns/net" else self.fail("Unexpected namespace read"))
        self.fence.allow("signal.setitimer")
        self.fence.allow("time.sleep")
        timer = patch.object(MODULE.time, "monotonic", lambda: fixture.now)
        timer.start()
        self.addCleanup(timer.stop)

        def connection(host: str, port: int, *, timeout: int) -> FakeConnection:
            self.assertEqual((host, port, timeout), ("127.0.0.1", MODULE.PORT, 5))
            self.assertTrue(fixture.responses, "Unexpected bootstrap HTTP attempt")
            result = FakeConnection(fixture.responses.pop(0))
            fixture.connections.append(result)
            return result

        self.fence.allow("http.client.HTTPConnection", connection)
        fixture.recorder = MODULE.Bootstrap({"networkNamespace": "net:[fresh]"})
        return fixture

    def install_identity(self, *, mount_overrides: dict[str, str] | None = None) -> types.SimpleNamespace:
        settings = self.state()
        identity = self.launcher_process()
        identity.update({"exe": str(MODULE.APP / "system/EmbyServer"),
                         "cmdline": [str(MODULE.APP / "system/EmbyServer"), "-programdata", str(MODULE.DATA)],
                         "serviceProperties": settings})
        fixture = types.SimpleNamespace(identity=identity)
        self.replace("owned_roots")
        self.replace("service_identity", lambda unit: copy.deepcopy(fixture.identity) if unit == MODULE.UNIT else self.fail("Unexpected identity unit"))
        self.replace("process_identity", lambda pid: {"networkNamespace": "net:[old]"}
                     if pid == MODULE.OLD_UNITS["goby-emby-reference.service"] else self.fail("Unexpected protected process"))
        self.fence.allow("os.readlink", lambda path: "net:[host]" if str(path) == "/proc/1/ns/net" else self.fail("Unexpected host namespace read"))
        mounts = {"/": "rw", **{path: "ro" for path in MODULE.READ_ONLY_PATHS},
                  str(MODULE.DATA): "rw", str(MODULE.RUNTIME): "rw", **(mount_overrides or {})}
        text = "\n".join("1 0 0:1 / " + path + " " + options + " - tmpfs tmpfs rw" for path, options in mounts.items())
        self.fence.allow("Path.read_text", lambda path: text if path == Path("/proc/12345/mountinfo") else self.fail("Unexpected mount table path"))
        return fixture

    def install_removal_recovery(self, *, proof_changes: dict | None = None) -> types.SimpleNamespace:
        entry = types.SimpleNamespace(st_dev=41, st_ino=123, st_uid=0, st_mode=stat.S_IFDIR | 0o700)
        proof = {"programData": str(MODULE.DATA), "device": entry.st_dev, "inode": entry.st_ino,
                 "marker": MODULE.MARKER, **(proof_changes or {})}
        fixture = types.SimpleNamespace(entry=entry, proof=proof)
        self.replace("canonical")
        self.replace("load", lambda path: copy.deepcopy(fixture.proof) if path == MODULE.PRIVATE / "data-removal-intent.json"
                     else self.fail("Unexpected recovery proof"))
        self.replace("properties", {"MainPID": "0", "ActiveState": "inactive"})
        self.fence.allow("Path.exists", lambda path: path == MODULE.DATA or path in self.saved)
        self.fence.allow("Path.stat", lambda path: entry if path == MODULE.DATA else self.fail("Unexpected recovery stat"))
        self.fence.allow("Path.iterdir", lambda path: iter(()) if path == MODULE.DATA else self.fail("Unexpected recovery membership"))
        self.fence.allow("os.path.ismount", False)
        return fixture

    def install_previous_failure(self) -> types.SimpleNamespace:
        prior_private = MODULE.FIRST_ROOT / "private"
        source = prior_private / "operator-source-attempt-1.py"
        cleanup_path = prior_private / "cleanup-attempt-1.json"
        marker_path = MODULE.FIRST_ROOT / ".goby-managed"
        console = prior_private / "operator-console.log"
        records = {
            prior_private / "prepare-intent.json": {
                "operatorSha256": MODULE.FIRST_OPERATOR_SHA256, "unit": MODULE.FIRST_UNIT,
                "programData": str(MODULE.FIRST_DATA), "marker": MODULE.FIRST_MARKER,
            },
            prior_private / "prepare-failure.json": {"cleanup": {"stopped": True}, "preservationErrors": []},
            cleanup_path: {"unit": MODULE.FIRST_UNIT, "programData": str(MODULE.FIRST_DATA),
                           "dataRemoved": True, "oldPreservationVerified": True, "evidenceRetained": True,
                           "stop": {"stopped": True}},
        }
        files = {marker_path, source, console, *records}
        directories = {prior_private, prior_private / "raw", MODULE.FIRST_ROOT / "export", MODULE.FIRST_ROOT / "runtime"}
        fixture = types.SimpleNamespace(records=records, files=files, directories=directories,
                                        entries=sorted(files | directories), cleanup_paths=[cleanup_path],
                                        exists=set(files | directories), symlinks=set(), http=[],
                                        state={"LoadState": "not-found"}, source=source, console=console,
                                        cleanup_path=cleanup_path, prior_private=prior_private,
                                        hashes={path: hashlib.sha256(str(path).encode()).hexdigest() for path in files})
        fixture.hashes[source] = MODULE.FIRST_OPERATOR_SHA256
        selection = patch.object(MODULE, "REFERENCE_RUN", 2)
        selection.start()
        self.addCleanup(selection.stop)
        self.replace("canonical")
        self.replace("load", lambda path: copy.deepcopy(fixture.records[path]))
        self.replace("properties", lambda unit: copy.deepcopy(fixture.state) if unit == MODULE.FIRST_UNIT
                     else self.fail("Unexpected prior fixture unit"))
        self.replace("digest", lambda path: fixture.hashes[path])
        self.fence.allow("Path.read_text", lambda path: MODULE.FIRST_MARKER + "\n" if path == marker_path
                         else self.fail("Unexpected prior fixture content read"))
        self.fence.allow("Path.exists", lambda path: path in fixture.exists)
        self.fence.allow("Path.is_symlink", lambda path: path in fixture.symlinks)
        self.fence.allow("Path.iterdir", lambda path: iter(fixture.http) if path in
                         {prior_private / "raw", MODULE.FIRST_ROOT / "export"} else self.fail("Unexpected HTTP membership read"))
        self.fence.allow("Path.glob", lambda path, pattern: iter(fixture.cleanup_paths) if path == prior_private and
                         pattern == "cleanup-attempt-*.json" else self.fail("Unexpected prior cleanup enumeration"))
        self.fence.allow("Path.rglob", lambda path, pattern: iter(fixture.entries) if path == MODULE.FIRST_ROOT and pattern == "*"
                         else self.fail("Unexpected prior evidence enumeration"))
        self.fence.allow("Path.resolve", lambda path, **_kwargs: path)
        self.fence.allow("Path.lstat", lambda path: types.SimpleNamespace(st_uid=0, st_nlink=1,
                         st_mode=(stat.S_IFDIR | 0o700) if path in fixture.directories else stat.S_IFREG | 0o600))
        self.fence.allow("Path.stat", lambda path: types.SimpleNamespace(st_size=17) if path in fixture.files
                         else self.fail("Unexpected prior file stat"))
        return fixture

    def test_stop_owned_accepts_activating_launcher_without_ready_identity(self) -> None:
        fixture = self.install_stop_fixture(dispatch_exists=False)
        result = MODULE.stop_owned(None)
        self.assertEqual(len(fixture.commands), 1)
        self.assertTrue(result["stopped"] and result["ownedProcessGone"])
        self.assertIn(MODULE.DISPATCH, self.saved)
        self.assertNotIn(MODULE.IDENTITY, self.saved)
        self.assertEqual(fixture.process_mock.call_count, 2)

    def test_stop_owned_accepts_owned_activating_unit_before_main_pid_exists(self) -> None:
        fixture = self.install_stop_fixture(main_pid=0)
        self.assertTrue(MODULE.stop_owned(None)["stopped"])
        self.assertEqual(len(fixture.commands), 1)
        fixture.process_mock.assert_not_called()

    def test_stop_owned_refuses_changed_invocation(self) -> None:
        fixture = self.install_stop_fixture()
        fixture.state["InvocationID"] = "b" * 32
        with self.assertRaisesRegex(RuntimeError, "replacement unit invocation"):
            MODULE.stop_owned(None)
        self.assertEqual(fixture.commands, [])

    def test_stop_owned_refuses_replaced_exec_start(self) -> None:
        fixture = self.install_stop_fixture()
        fixture.state["ExecStart"] = "{ path=/usr/bin/unrelated ; argv[]=unrelated ; }"
        with self.assertRaisesRegex(RuntimeError, "launcher and cgroup"):
            MODULE.stop_owned(None)
        self.assertEqual(fixture.commands, [])

    def test_stop_owned_refuses_unit_and_process_cgroup_mismatches(self) -> None:
        fixture = self.install_stop_fixture()
        fixture.state["ControlGroup"] = "/system.slice/unrelated.service"
        with self.assertRaisesRegex(RuntimeError, "launcher and cgroup"):
            MODULE.stop_owned(None)
        fixture.state["ControlGroup"] = "/system.slice/" + MODULE.UNIT
        fixture.process["cgroup"] = "0::/system.slice/unrelated.service\n"
        with self.assertRaisesRegex(RuntimeError, "outside the owned unit cgroup"):
            MODULE.stop_owned(None)
        self.assertEqual(fixture.commands, [])

    def test_stop_owned_refuses_reused_pid_start_ticks(self) -> None:
        fixture = self.install_stop_fixture()
        expected = {"pid": 12345, "startTicks": "older-incarnation"}
        with self.assertRaisesRegex(RuntimeError, "replacement process"):
            MODULE.stop_owned(expected)
        self.assertEqual(fixture.commands, [])

    def test_stop_owned_requires_the_attested_process_to_disappear(self) -> None:
        fixture = self.install_stop_fixture()
        fixture.process_mock.side_effect = lambda pid: copy.deepcopy(fixture.process)
        with self.assertRaisesRegex(RuntimeError, "process did not exit"):
            MODULE.stop_owned(None)
        self.assertEqual(len(fixture.commands), 1)

    def test_stop_owned_rejects_unfinished_final_unit_states(self) -> None:
        final_state = {"MainPID": "0", "ActiveState": "deactivating"}
        fixture = self.install_stop_fixture(final_state=final_state)
        for active_state in ("deactivating", "activating"):
            with self.subTest(active_state=active_state):
                fixture.stopped = False
                final_state["ActiveState"] = active_state
                with self.assertRaisesRegex(RuntimeError, "remains active|did not stop|not stopped"):
                    MODULE.stop_owned(None)
        self.assertEqual(len(fixture.commands), 2)

    def test_cleanup_stops_owned_service_before_failed_preservation_denies_deletion(self) -> None:
        fixture = self.install_cleanup_fixture()

        def failed_preservation(_expected: dict) -> None:
            fixture.events.append("preservation")
            raise RuntimeError("Synthetic protected baseline changed")

        self.replace("check_old_services", failed_preservation)
        with self.assertRaisesRegex(RuntimeError, "Cleanup is incomplete"):
            MODULE.cleanup()
        self.assertEqual(fixture.events, ["stop", "preservation"])
        fixture.remove.assert_not_called()
        report = self.saved[MODULE.PRIVATE / "cleanup-attempt-1.json"]
        self.assertTrue(report["stop"]["stopped"] and report["evidenceRetained"])
        self.assertFalse(report["dataRemoved"] or report["oldPreservationVerified"])

    def test_cleanup_retry_after_removed_data_retains_failed_preservation_history(self) -> None:
        fixture = self.install_cleanup_fixture()
        checks = []

        def preservation(_expected: dict) -> None:
            checks.append("checked")
            if len(checks) > 1:
                raise RuntimeError("Synthetic post-removal baseline failure")

        def remove() -> None:
            fixture.data_exists = False

        self.replace("check_old_services", preservation)
        fixture.remove.side_effect = remove
        self.replace("tree_size", 17)
        self.fence.allow("Path.rglob", lambda path, pattern: iter(()) if path == MODULE.DATA and pattern == "*"
                         else self.fail("Unexpected cleanup inventory"))
        with self.assertRaisesRegex(RuntimeError, "Cleanup is incomplete"):
            MODULE.cleanup()
        first = copy.deepcopy(self.saved[MODULE.PRIVATE / "cleanup-attempt-1.json"])
        self.assertTrue(first["dataRemoved"])
        self.assertFalse(first["oldPreservationVerified"])
        with self.assertRaisesRegex(RuntimeError, "cannot prove.*preservation baseline"):
            MODULE.cleanup()
        retry = self.saved[MODULE.PRIVATE / "cleanup-absent-data-1.json"]
        self.assertTrue(retry["dataAbsent"] and retry["serviceAbsent"] and retry["evidenceRetained"])
        self.assertFalse(retry["dataRemoved"] or retry["oldPreservationVerified"])
        self.assertEqual(retry["preservationFailureType"], "RuntimeError")
        self.assertEqual(first, self.saved[MODULE.PRIVATE / "cleanup-attempt-1.json"])
        self.assertEqual(fixture.stop.call_count, 1)
        self.assertEqual(fixture.remove.call_count, 1)

    def test_marker_recovery_requires_exact_empty_attested_data_root(self) -> None:
        self.install_removal_recovery()
        MODULE.recover_empty_removal_root()
        self.assertEqual(self.saved, {MODULE.DATA / ".goby-managed": MODULE.MARKER + "\n"})
        marker = MODULE.DATA / ".goby-managed"
        deleted: list[Path] = []
        self.replace("owned_roots")
        self.fence.allow("Path.exists", lambda path: path == MODULE.DATA or
                         path == MODULE.PRIVATE / "data-removal-intent.json" or path in self.saved)
        self.fence.allow("Path.rglob", lambda path, pattern: iter([marker]) if path == MODULE.DATA and pattern == "*"
                         else self.fail("Unexpected recovered-root inventory"))
        self.fence.allow("Path.resolve", lambda path, **_kwargs: path)
        self.fence.allow("Path.is_symlink", False)
        self.fence.allow("Path.lstat", lambda path: types.SimpleNamespace(st_uid=0, st_nlink=1, st_mode=stat.S_IFREG | 0o600)
                         if path == marker else self.fail("Unexpected recovered-root member"))

        def unlink(path: Path) -> None:
            self.assertEqual(path, marker)
            deleted.append(path)
            del self.saved[path]

        def rmdir(path: Path) -> None:
            self.assertEqual(path, MODULE.DATA)
            deleted.append(path)

        self.fence.allow("Path.unlink", unlink)
        self.fence.allow("Path.rmdir", rmdir)
        MODULE.remove_owned_data()
        self.assertEqual(deleted, [marker, MODULE.DATA])

    def test_marker_recovery_refuses_device_inode_path_or_marker_changes(self) -> None:
        fixture = self.install_removal_recovery()
        expected = copy.deepcopy(fixture.proof)
        for field, replacement in (("device", 42), ("inode", 124), ("programData", "/dev/shm/unrelated"),
                                   ("marker", "unrelated-owner")):
            with self.subTest(field=field):
                fixture.proof = {**expected, field: replacement}
                with self.assertRaisesRegex(RuntimeError, "previously attested empty removal root"):
                    MODULE.recover_empty_removal_root()
                self.assertEqual(self.saved, {})

    def test_removal_rejects_unowned_hardlinked_symlink_and_mounted_members(self) -> None:
        self.replace("owned_roots")
        root_entry = types.SimpleNamespace(st_dev=41, st_ino=123)
        path = MODULE.DATA / "unexpected-member"
        fixture = types.SimpleNamespace(owner=0, links=1, symlink=False, mounted=False)
        self.fence.allow("Path.exists", lambda target: target in self.saved)
        self.fence.allow("Path.stat", lambda target: root_entry if target == MODULE.DATA else self.fail("Unexpected removal stat"))
        self.fence.allow("Path.rglob", lambda target, pattern: iter([path]) if target == MODULE.DATA and pattern == "*"
                         else self.fail("Unexpected removal inventory"))
        self.fence.allow("Path.resolve", lambda target, **_kwargs: target)
        self.fence.allow("Path.is_symlink", lambda target: fixture.symlink if target == path else False)
        self.fence.allow("Path.lstat", lambda target: types.SimpleNamespace(st_uid=fixture.owner, st_nlink=fixture.links,
                         st_mode=stat.S_IFREG | 0o600) if target == path else self.fail("Unexpected member stat"))
        self.fence.allow("os.path.ismount", lambda target: fixture.mounted if target == path else False)
        self.replace("load", lambda target: copy.deepcopy(self.saved[target]))
        for field, value in (("owner", 995), ("links", 2), ("symlink", True), ("mounted", True)):
            with self.subTest(field=field):
                fixture.owner, fixture.links, fixture.symlink, fixture.mounted = 0, 1, False, False
                setattr(fixture, field, value)
                with self.assertRaisesRegex(RuntimeError, "unexpected owner|unexpected hard link|unsafe removal target"):
                    MODULE.remove_owned_data()
                self.fence.boundaries["Path.unlink"].assert_not_called()
                self.fence.boundaries["Path.rmdir"].assert_not_called()

    def test_short_content_length_never_establishes_ready_server_identity(self) -> None:
        public = {"Version": "4.9.5.0", "ServerName": MODULE.SERVER_NAME, "Id": "synthetic-new-server"}
        encoded = json.dumps(public).encode()
        fixture = self.install_network([FakeResponse(200, encoded, headers=[("Content-Length", str(len(encoded) + 1))])])
        with self.assertRaisesRegex(RuntimeError, "public identity is wrong"):
            fixture.recorder.capture()
        self.assertIsNone(fixture.recorder.server_id)
        self.assertEqual((fixture.recorder.count, fixture.recorder.complete_count, fixture.recorder.incomplete_count), (1, 0, 1))
        self.assertEqual(fixture.connections[0].requests[0]["method"], "GET")
        self.assertTrue(fixture.connections[0].closed)
        self.assertFalse(any(path.name.endswith("credentials.env") for path in self.saved))
        raw = next(value for path, value in self.saved.items() if path.parent == MODULE.RAW)
        self.assertFalse(raw["observation"]["completeHTTP"])

    def test_interrupted_response_cannot_be_returned_as_complete_http(self) -> None:
        fixture = self.install_network([FakeResponse(200, {"Id": "partial"}, incomplete=True)])
        with self.assertRaisesRegex(RuntimeError, "HTTP response was incomplete"):
            fixture.recorder.request("partial", "GET", "/emby/System/Info/Public")
        self.assertEqual((fixture.recorder.complete_count, fixture.recorder.incomplete_count), (0, 1))
        raw = next(value for path, value in self.saved.items() if path.parent == MODULE.RAW)
        self.assertEqual(raw["observation"]["failureType"], "IncompleteRead")
        self.assertEqual(fixture.recorder.tokens, {})

    def test_acknowledged_login_survives_later_dto_failure_with_redacted_export(self) -> None:
        token = "synthetic-owned-bootstrap-token"
        fixture = self.install_network([
            FakeResponse(200, {"Version": "4.9.5.0", "ServerName": MODULE.SERVER_NAME, "Id": "synthetic-new-server"}),
            FakeResponse(200, {"Name": ""}), FakeResponse(200, {}), FakeResponse(204), FakeResponse(204),
            FakeResponse(200, {"AccessToken": token, "ServerId": "wrong-server", "User": {"Name": MODULE.ADMIN_NAME}}),
        ])
        token_factory = patch.object(MODULE.secrets, "token_hex", return_value="synthetic-password")
        token_factory.start()
        self.addCleanup(token_factory.stop)
        with self.assertRaisesRegex(RuntimeError, "administrator login or role differs"):
            fixture.recorder.capture()
        self.assertEqual(fixture.recorder.tokens, {"admin": token})
        self.assertIn(token, fixture.recorder.secrets)
        acknowledgement = MODULE.PRIVATE / "admin-bootstrap-login.json"
        self.assertEqual(self.saved[acknowledgement]["AccessToken"], token)
        login_raw = next(path for path in self.saved if path.parent == MODULE.RAW and "admin-login" in path.name)
        self.assertLess(self.save_order.index(acknowledgement), self.save_order.index(login_raw))
        exported = self.saved[MODULE.EXPORT / login_raw.name]
        self.assertNotIn(token, json.dumps(exported))
        self.assertEqual(fixture.recorder.count, 6)
        self.assertTrue(all(connection.closed for connection in fixture.connections))

    def test_bootstrap_mutations_and_http_require_fresh_server_and_namespace(self) -> None:
        fixture = self.install_network([])
        for identity in (None, MODULE.OLD_SERVER_ID):
            with self.subTest(server_identity=identity):
                fixture.recorder.server_id = identity
                with self.assertRaisesRegex(RuntimeError, "distinct proven fresh server identity"):
                    fixture.recorder.request("forbidden-write", "POST", "/emby/Startup/Complete")
        fixture.recorder.identity["networkNamespace"] = "net:[other]"
        with self.assertRaisesRegex(RuntimeError, "outside the fresh namespace"):
            fixture.recorder.request("wrong-namespace", "GET", "/emby/System/Info/Public")
        self.assertEqual(fixture.recorder.count, 0)
        self.assertEqual(fixture.connections, [])
        self.assertEqual(self.saved, {})

    def test_actual_mount_attestation_rejects_writable_shared_paths(self) -> None:
        fixture = self.install_identity()
        self.assertEqual(MODULE.new_identity(), fixture.identity)
        boundary = self.fence.boundaries["Path.read_text"]
        good_reader = boundary.side_effect
        for target in sorted(MODULE.READ_ONLY_PATHS):
            with self.subTest(path=target):
                boundary.side_effect = lambda path, target=target: good_reader(path).replace(" " + target + " ro -", " " + target + " rw -")
                with self.assertRaisesRegex(RuntimeError, "Actual fresh mount permissions"):
                    MODULE.new_identity()

    def test_identity_refuses_both_host_and_old_reference_network_namespaces(self) -> None:
        fixture = self.install_identity()
        for namespace in ("net:[host]", "net:[old]"):
            with self.subTest(namespace=namespace):
                fixture.identity["networkNamespace"] = namespace
                with self.assertRaisesRegex(RuntimeError, "shares a protected network namespace"):
                    MODULE.new_identity()

    def test_default_request_count_time_and_response_bounds_are_enforced(self) -> None:
        fixture = self.install_network([])
        self.assertEqual((MODULE.MAX_REQUESTS, MODULE.MAX_BODY, MODULE.MAX_WIRE), (128, 256 * 1024, 8 * 1024 * 1024))
        self.assertEqual(fixture.recorder.deadline - fixture.now, 180)
        fixture.recorder.count = MODULE.MAX_REQUESTS
        with self.assertRaisesRegex(RuntimeError, "request budget exhausted"):
            fixture.recorder.request("count-limit", "GET", "/emby/System/Info/Public")
        fixture.recorder.count = 0
        fixture.now = fixture.recorder.deadline
        with self.assertRaisesRegex(RuntimeError, "request budget exhausted"):
            fixture.recorder.request("time-limit", "GET", "/emby/System/Info/Public")
        self.assertEqual(fixture.connections, [])
        fixture.now = 100.0
        fixture.responses.append(FakeResponse(200, b"x" * (MODULE.MAX_BODY + 1)))
        with self.assertRaisesRegex(RuntimeError, "HTTP response was incomplete"):
            fixture.recorder.request("body-limit", "GET", "/emby/System/Info/Public")
        self.assertEqual((fixture.recorder.complete_count, fixture.recorder.incomplete_count), (0, 1))

    def test_run_selector_accepts_only_canonical_string_one_or_two(self) -> None:
        self.assertEqual(MODULE.selected_run("1"), 1)
        self.assertEqual(MODULE.selected_run("2"), 2)
        for value in (None, True, False, 1, 2, b"1", "", "0", "3", "01", "02", "+1", " 1", "2 ", "2\n", [], {}):
            with self.subTest(value=repr(value)):
                with self.assertRaisesRegex(ValueError, "canonical 1 or 2"):
                    MODULE.selected_run(value)

    def test_default_and_second_run_use_distinct_owned_resources_and_prefixes(self) -> None:
        variants = []
        for value in (None, "1", "2"):
            variant = types.ModuleType("synthetic_fresh_selection_" + str(value))
            variant.__file__ = MODULE.__file__
            environment = {} if value is None else {"GOBY_FRESH_KEY_DEVICES_RUN": value}
            with patch.object(MODULE.os, "environ", environment):
                exec(OPERATOR_CODE, variant.__dict__)
            variants.append(variant)
        default, first, second = variants
        for field in ("REFERENCE_RUN", "ROOT", "DATA", "UNIT", "MARKER", "PREFIX"):
            self.assertEqual(getattr(default, field), getattr(first, field))
        self.assertEqual((first.REFERENCE_RUN, second.REFERENCE_RUN), (1, 2))
        self.assertEqual(first.ROOT, second.FIRST_ROOT)
        self.assertEqual(first.DATA, second.FIRST_DATA)
        self.assertEqual(first.UNIT, second.FIRST_UNIT)
        for field in ("ROOT", "DATA", "UNIT", "MARKER", "ADMIN_NAME", "VIEWER_NAME", "ADMIN_DEVICE", "VIEWER_DEVICE"):
            self.assertNotEqual(getattr(first, field), getattr(second, field), field)
        self.assertTrue(second.ROOT.name.endswith("-02") and second.DATA.name.endswith("-02"))
        self.assertTrue(second.UNIT.endswith("-02.service"))
        self.assertEqual(first.PREFIX, "key-devices-fresh-setup-m5e-")
        self.assertEqual(second.PREFIX, "key-devices-fresh-setup-m5e-2-")
        self.assertEqual((first.PORT, second.PORT), (18098, 18098))
        self.assertEqual(second.RUNTIME, second.ROOT / "runtime")
        self.assertIsNone(first.previous_failure_provenance())

    def test_runtime_working_directory_is_required_for_start_dispatch_and_identity(self) -> None:
        runtime_argument = "--property=WorkingDirectory=" + str(MODULE.RUNTIME)
        calls = []
        self.replace("run", lambda arguments: calls.append(arguments) or "")
        MODULE.start_service()
        self.assertEqual(len(calls), 1)
        self.assertIn(runtime_argument, calls[0])
        self.assertNotIn("--property=WorkingDirectory=" + str(MODULE.DATA), calls[0])
        identity_fixture = self.install_identity()
        identity_fixture.identity["serviceProperties"]["WorkingDirectory"] = str(MODULE.DATA)
        with self.assertRaisesRegex(RuntimeError, "sandbox or launcher differs"):
            MODULE.new_identity()
        identity_fixture.identity["serviceProperties"]["WorkingDirectory"] = str(MODULE.RUNTIME)
        self.assertEqual(MODULE.new_identity(), identity_fixture.identity)
        stop_fixture = self.install_stop_fixture()
        stop_fixture.state["WorkingDirectory"] = str(MODULE.DATA)
        with self.assertRaisesRegex(RuntimeError, "launcher and cgroup"):
            MODULE.stop_owned(None)
        self.assertEqual(stop_fixture.commands, [])

    def test_prior_failure_provenance_covers_every_retained_file_and_executed_source(self) -> None:
        fixture = self.install_previous_failure()
        self.assertEqual(MODULE.FIRST_OPERATOR_SHA256,
                         "8f18f8f7603c935db626df2b62a8d5a4ea0ce9b9a3646346f9f954775703dd35")
        result = MODULE.previous_failure_provenance()
        self.assertEqual(result["referenceRun"], 1)
        self.assertEqual(result["outcome"], "initialization-failed-before-http")
        self.assertEqual(result["setupRecordCount"], 0)
        self.assertEqual(result["operatorSha256"], MODULE.FIRST_OPERATOR_SHA256)
        self.assertEqual(result["operatorSource"], str(fixture.source))
        self.assertEqual(result["cleanupRecord"], str(fixture.cleanup_path))
        self.assertEqual(set(result["files"]), {str(path) for path in fixture.files})
        self.assertEqual(result["fileCount"], len(fixture.files))
        self.assertEqual(result["totalBytes"], len(fixture.files) * 17)
        MODULE.verify_previous_failure(result)
        self.assertEqual(self.saved, {})

    def test_prior_failure_requires_source_identity_and_unique_successful_owned_cleanup(self) -> None:
        fixture = self.install_previous_failure()
        intent = fixture.records[fixture.prior_private / "prepare-intent.json"]
        failure = fixture.records[fixture.prior_private / "prepare-failure.json"]
        cleanup = fixture.records[fixture.cleanup_path]
        cases = [
            (intent, "operatorSha256", "9" * 64, "executed operator and fixed resources"),
            (intent, "unit", "unrelated.service", "executed operator and fixed resources"),
            (failure, "cleanup", {"stopped": False}, "proven service cleanup"),
            (failure, "preservationErrors", [{"check": "oldFiles"}], "proven service cleanup"),
            (cleanup, "dataRemoved", False, "unique successful cleanup report"),
            (cleanup, "oldPreservationVerified", False, "unique successful cleanup report"),
            (cleanup, "evidenceRetained", False, "unique successful cleanup report"),
            (cleanup, "stop", {"stopped": False}, "ambiguous ownership"),
            (cleanup, "unit", "unrelated.service", "ambiguous ownership"),
        ]
        for record, field, replacement, message in cases:
            with self.subTest(field=field, replacement=replacement):
                original = copy.deepcopy(record[field])
                record[field] = replacement
                with self.assertRaisesRegex(RuntimeError, message):
                    MODULE.previous_failure_provenance()
                record[field] = original
        fixture.hashes[fixture.source] = "7" * 64
        with self.assertRaisesRegex(RuntimeError, "operator source is missing or differs"):
            MODULE.previous_failure_provenance()
        fixture.hashes[fixture.source] = MODULE.FIRST_OPERATOR_SHA256
        fixture.entries.remove(fixture.source)
        with self.assertRaisesRegex(RuntimeError, "operator source is missing or differs"):
            MODULE.previous_failure_provenance()
        fixture.entries.append(fixture.source)
        second_cleanup = fixture.prior_private / "cleanup-attempt-2.json"
        fixture.cleanup_paths.append(second_cleanup)
        fixture.records[second_cleanup] = copy.deepcopy(cleanup)
        with self.assertRaisesRegex(RuntimeError, "unique successful cleanup report"):
            MODULE.previous_failure_provenance()
        self.assertEqual(self.saved, {})

    def test_prior_evidence_hash_changes_deletions_and_additions_are_rejected(self) -> None:
        fixture = self.install_previous_failure()
        expected = MODULE.previous_failure_provenance()
        previous_hash = fixture.hashes[fixture.console]
        fixture.hashes[fixture.console] = "8" * 64
        with self.assertRaisesRegex(RuntimeError, "first fresh attempt evidence changed"):
            MODULE.verify_previous_failure(expected)
        fixture.hashes[fixture.console] = previous_hash
        fixture.entries.remove(fixture.console)
        with self.assertRaisesRegex(RuntimeError, "first fresh attempt evidence changed"):
            MODULE.verify_previous_failure(expected)
        fixture.entries.append(fixture.console)
        extra = fixture.prior_private / "unexpected-new-evidence.json"
        fixture.entries.append(extra)
        fixture.files.add(extra)
        fixture.hashes[extra] = "6" * 64
        with self.assertRaisesRegex(RuntimeError, "first fresh attempt evidence changed"):
            MODULE.verify_previous_failure(expected)
        self.assertEqual(self.saved, {})

    def test_prior_attempt_requires_absent_data_unit_identity_and_http(self) -> None:
        fixture = self.install_previous_failure()
        fixture.exists.add(MODULE.FIRST_DATA)
        with self.assertRaisesRegex(RuntimeError, "first fresh data and unit to be absent"):
            MODULE.previous_failure_provenance()
        fixture.exists.remove(MODULE.FIRST_DATA)
        fixture.symlinks.add(MODULE.FIRST_DATA)
        with self.assertRaisesRegex(RuntimeError, "first fresh data and unit to be absent"):
            MODULE.previous_failure_provenance()
        fixture.symlinks.clear()
        fixture.state["LoadState"] = "loaded"
        with self.assertRaisesRegex(RuntimeError, "first fresh data and unit to be absent"):
            MODULE.previous_failure_provenance()
        fixture.state["LoadState"] = "not-found"
        fixture.http.append(fixture.prior_private / "raw/unexpected.json")
        with self.assertRaisesRegex(RuntimeError, "unexpectedly contains HTTP capture evidence"):
            MODULE.previous_failure_provenance()
        fixture.http.clear()
        for name in ("manifest.json", "service-identity.json", "bootstrap-result.json", "fresh-public-identity.json"):
            with self.subTest(name=name):
                path = fixture.prior_private / name
                fixture.exists.add(path)
                with self.assertRaisesRegex(RuntimeError, "process identity or bootstrap unexpectedly"):
                    MODULE.previous_failure_provenance()
                fixture.exists.remove(path)
        self.assertEqual(self.saved, {})

    def test_command_failures_retain_bounded_private_diagnostics_without_overwrite(self) -> None:
        self.replace("owned_root")
        self.replace("canonical")
        folder = MODULE.PRIVATE / "command-failures"
        first = folder / "command-failure-001.json"
        self.saved[first] = {"retained": "prior diagnostic"}
        self.fence.allow("Path.exists", lambda path: path == folder or path in self.saved)
        arguments = ["systemd-run", "--unit=" + MODULE.UNIT, str(MODULE.RUNTIME / "launch.sh")]
        oversized = b"e" * 70000
        response = types.SimpleNamespace(returncode=1, stdout=b"synthetic output", stderr=oversized)
        command = self.fence.allow("subprocess.run", lambda *args, **kwargs: response)
        with self.assertRaisesRegex(RuntimeError, "bounded operator command failed: systemd-run"):
            MODULE.run(arguments)
        command.assert_called_once_with(arguments, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False)
        second = self.saved[folder / "command-failure-002.json"]
        self.assertEqual(second["failureType"], "NonzeroExit")
        self.assertEqual(second["arguments"], arguments)
        self.assertEqual(second["returnCode"], 1)
        self.assertEqual(second["stderr"]["bytes"], 70000)
        self.assertEqual(len(second["stderr"]["text"]), 65536)
        self.assertTrue(second["stderr"]["truncated"])
        self.assertEqual(second["referenceRun"], MODULE.REFERENCE_RUN)
        command.side_effect = subprocess.TimeoutExpired(arguments, 15, output=b"partial", stderr=b"timeout detail")
        with self.assertRaisesRegex(RuntimeError, "bounded operator command timed out: systemd-run"):
            MODULE.run(arguments)
        third = self.saved[folder / "command-failure-003.json"]
        self.assertEqual(third["failureType"], "TimeoutExpired")
        self.assertIsNone(third["returnCode"])
        self.assertEqual(third["stdout"]["text"], "partial")
        self.assertEqual(self.saved[first], {"retained": "prior diagnostic"})
        self.fence.boundaries["Path.mkdir"].assert_not_called()


def main() -> None:
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "fresh-key-device-operator-safety", "result": "blocked",
                          "reason": "Authorized root SSH and one operator source path are required"}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    source_bytes = source.read_bytes()
    source_hash = hashlib.sha256(source_bytes).hexdigest()
    suite_bytes = Path(__file__).read_bytes()
    suite_hash = hashlib.sha256(suite_bytes).hexdigest()
    SOURCE_LINES[str(source)] = source_bytes.decode("utf-8").splitlines(keepends=True)
    SOURCE_LINES[__file__] = suite_bytes.decode("utf-8").splitlines(keepends=True)
    global MODULE, OPERATOR_CODE
    MODULE = types.ModuleType("fresh_key_device_operator_under_test")
    MODULE.__file__ = str(source)
    OPERATOR_CODE = compile(source_bytes, str(source), "exec")
    with EffectFence():
        exec(OPERATOR_CODE, MODULE.__dict__)
    result = unittest.TextTestRunner(verbosity=2, stream=sys.stderr).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(FreshOperatorSafetyTests))
    print(json.dumps({"suite": "fresh-key-device-operator-safety", "result": "passed" if result.wasSuccessful() else "failed",
                      "testsRun": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "skipped": len(result.skipped), "operatorSha256": source_hash, "suiteSha256": suite_hash,
                      "verificationEnvironment": "authorized-root-ssh", "fixtures": "synthetic-memory-only"}))
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
