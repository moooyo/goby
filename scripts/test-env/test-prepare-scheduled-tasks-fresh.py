#!/usr/bin/env python3
"""Exercise scheduled-task fixture ownership with entirely synthetic effects.

Run only through authorized root SSH with one operator source path. Only the
operator and suite sources are read before the fence is installed. Files,
services, HTTP, subprocesses, waits, and database access are never real fixtures.
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
OPERATOR: types.ModuleType
SOURCE_LINES: dict[str, list[str]] = {}


class EffectFence(contextlib.ExitStack):
    """Deny effects before imports or constructors can reach a real surface."""

    def __init__(self) -> None:
        super().__init__()
        self.violations: list[str] = []

    def __enter__(self) -> EffectFence:
        super().__enter__()
        import builtins

        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "lazycache", lambda filename, module_globals: False))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       list(SOURCE_LINES.get(str(filename), ()))))
        targets = (
            (builtins, ("open",)),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (signal, ("signal", "setitimer", "pthread_sigmask")), (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "stat", "lstat", "readlink", "scandir", "listdir", "walk", "write", "fsync",
                  "umask", "kill", "killpg", "system", "popen", "fork", "posix_spawn", "posix_spawnp", "execve",
                  "replace", "rename", "mkdir", "makedirs", "remove", "unlink", "rmdir", "chmod", "chown", "link")),
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


class ScheduledFixtureSafetyTests(unittest.TestCase):
    def setUp(self) -> None:
        fence = EffectFence()
        fence.__enter__()
        self.addCleanup(fence.__exit__, None, None, None)
        self.output = io.StringIO()
        redirect = contextlib.redirect_stdout(self.output)
        redirect.__enter__()
        self.addCleanup(redirect.__exit__, None, None, None)
        self.saved: dict[Path, object] = {}
        self.atomic_save = OPERATOR.atomic_save
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

    def install_source_snapshot(self) -> types.SimpleNamespace:
        files = OPERATOR.source_paths()
        directories = set(OPERATOR.SOURCE_BATCHES.values()) | {path.parent for path in files}
        marker = OPERATOR.SOURCE / ".goby-managed"
        entries = [*sorted(directories), *files, marker]
        metadata = {}
        for index, path in enumerate(entries, 1):
            directory = path in directories
            metadata[path] = types.SimpleNamespace(st_uid=0, st_nlink=1, st_dev=41, st_ino=index,
                st_size=0 if directory else OPERATOR.SOURCE_FILE_BYTES,
                st_mode=(stat.S_IFDIR | 0o700) if directory else stat.S_IFREG | 0o600)
        fixture = types.SimpleNamespace(files=files, entries=entries, metadata=metadata,
                                        hashes={path: OPERATOR.SOURCE_SHA256 for path in [*files, OPERATOR.SOURCE_INPUT]},
                                        symlinks=set(), mounts=set())
        fixture.hashes[marker] = "1" * 64
        self.replace(OPERATOR, "owned_root", lambda path: self.assertEqual(path, OPERATOR.SOURCE))
        self.replace(OPERATOR, "digest", lambda path: fixture.hashes[path])
        self.replace(Path, "rglob", lambda path, pattern: iter(fixture.entries) if path == OPERATOR.SOURCE and pattern == "*"
                     else self.fail("Unexpected source enumeration"))
        self.replace(Path, "resolve", lambda path, **kwargs: path)
        self.replace(Path, "is_symlink", lambda path: path in fixture.symlinks)
        self.replace(Path, "lstat", lambda path: fixture.metadata[path])
        self.replace(os.path, "ismount", lambda path: path in fixture.mounts)
        return fixture

    def service_state(self) -> dict:
        return {"Id": OPERATOR.UNIT, "LoadState": "loaded", "ActiveState": "activating", "MainPID": "12345",
                "InvocationID": "a" * 32, "Description": OPERATOR.DESCRIPTION,
                "WorkingDirectory": str(OPERATOR.RUNTIME), "ExecStart": "{ path=" + str(OPERATOR.RUNTIME / "launch.sh") + " ; }",
                "ControlGroup": "/system.slice/" + OPERATOR.UNIT, "MemoryMax": str(512 * OPERATOR.MIB),
                "PrivateNetwork": "yes", "PrivateTmp": "yes", "NoNewPrivileges": "yes", "ProtectSystem": "strict",
                "ProtectHome": "yes", "KillMode": "control-group", "TasksMax": "256", "LimitNOFILE": "65536", "UMask": "0077",
                "ReadWritePaths": str(OPERATOR.DATA) + " " + str(OPERATOR.RUNTIME),
                "ReadOnlyPaths": " ".join(sorted(OPERATOR.READ_ONLY_PATHS))}

    def install_cleanup(self) -> types.SimpleNamespace:
        fixture = types.SimpleNamespace(existing={OPERATOR.ROOT, OPERATOR.PRIVATE, OPERATOR.INTENT, OPERATOR.DATA, OPERATOR.SOURCE}, events=[])
        intent = {"marker": OPERATOR.MARKER, "unit": OPERATOR.UNIT, "programData": str(OPERATOR.DATA),
                  "evidenceRoot": str(OPERATOR.ROOT), "sourceRoot": str(OPERATOR.SOURCE), "oldServices": {}, "oldBaseline": {}}
        for name in ("common_preconditions", "owned_root", "canonical", "recover_atomic_publications",
                     "recover_empty_removal_root", "verify_package", "verify_baseline"):
            self.replace(OPERATOR, name, lambda *args, **kwargs: None)
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(intent) if path == OPERATOR.INTENT else self.fail("Unexpected cleanup input"))
        self.replace(OPERATOR, "properties", lambda unit: {"LoadState": "not-found", "MainPID": "0", "ActiveState": "inactive"})
        self.replace(OPERATOR, "stop_owned", lambda expected: fixture.events.append("stop") or {"stopped": True})
        self.replace(OPERATOR, "check_old_services", lambda expected: fixture.events.append("old-services"))
        self.replace(OPERATOR, "tree_size", lambda path: 0)
        fixture.snapshot = Mock(return_value={"files": {}, "totalBytes": 0, "complete": False})
        self.replace(OPERATOR, "source_snapshot", fixture.snapshot)

        def remove(folder: Path) -> None:
            self.assertIn(folder, {OPERATOR.DATA, OPERATOR.SOURCE})
            fixture.events.append("remove-source" if folder == OPERATOR.SOURCE else "remove-data")
            fixture.existing.remove(folder)

        self.replace(OPERATOR, "remove_owned_tree", remove)
        self.replace(OPERATOR, "remove_owned_data", lambda: remove(OPERATOR.DATA))
        self.replace(Path, "exists", lambda path: path in fixture.existing or path in self.saved)
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "rglob", lambda path, pattern: iter(()) if path == OPERATOR.DATA else self.fail("Unexpected cleanup enumeration"))
        return fixture

    def test_fixed_resources_select_the_new_namespace_and_read_only_sources(self) -> None:
        self.assertEqual(OPERATOR.PORT, 18099)
        self.assertEqual(OPERATOR.OLD_UNITS["goby-foundation-test.service"], 3535438)
        self.assertEqual(OPERATOR.MAIN_START_TICKS, "23604510")
        self.assertEqual(OPERATOR.MAIN_BINARY_SHA256,
                         "394430272da8ad6268534c1bcaa925ccffc92f61ed0b136a8ec6bbc4dae88541")
        self.assertEqual(OPERATOR.SOURCE, OPERATOR.ROOT / "source")
        self.assertEqual(OPERATOR.RUNTIME, OPERATOR.ROOT / "runtime")
        self.assertEqual(OPERATOR.SOURCE_BATCH_COUNT, 256)
        self.assertEqual(set(OPERATOR.SOURCE_BATCHES.values()), {OPERATOR.SOURCE / "batch-a", OPERATOR.SOURCE / "batch-b"})
        self.assertIn(str(OPERATOR.SOURCE), OPERATOR.READ_ONLY_PATHS)
        self.assertIn("/opt/goby-fixtures", OPERATOR.READ_ONLY_PATHS)
        self.assertEqual(OPERATOR.SOURCE_SHA256, "997af268405a91e01685afa52d70a892c767e0e7135d25f6a33087cfb72de1c3")
        self.assertEqual(OPERATOR.SOURCE_FILE_BYTES, 56379)
        for path in (OPERATOR.ROOT, OPERATOR.DATA):
            self.assertIn("scheduled-tasks-fresh-m5f-20260910-01", path.name)
        self.assertNotEqual(OPERATOR.SOURCE, OPERATOR.SOURCE_INPUT.parent)

    def test_old_main_pid_owner_start_ticks_and_binary_are_all_required(self) -> None:
        main = {"pid": 3535438, "uid": 995, "startTicks": "23604510", "exe": "/opt/goby-dev/goby"}
        reference = {"pid": 3131777, "uid": 0, "serviceProperties": {"PrivateNetwork": "yes"}, "networkNamespace": "net:[old]"}
        values = {"goby-foundation-test.service": main, "goby-emby-reference.service": reference}
        self.replace(OPERATOR, "service_identity", lambda unit: copy.deepcopy(values[unit]))
        self.replace(os, "readlink", lambda path: "net:[host]" if str(path) == "/proc/1/ns/net" else self.fail("Unexpected namespace lookup"))
        digest = Mock(return_value=OPERATOR.MAIN_BINARY_SHA256)
        self.replace(OPERATOR, "digest", digest)
        expected = OPERATOR.old_services()
        self.assertEqual(expected["goby-foundation-test.service"]["binarySha256"], OPERATOR.MAIN_BINARY_SHA256)
        for field, altered in (("pid", 3535439), ("uid", 0), ("startTicks", "23604511"), ("exe", "/opt/unrelated/server")):
            with self.subTest(field=field):
                original = main[field]
                main[field] = altered
                with self.assertRaisesRegex(RuntimeError, "protected service process|protected M5e Goby"):
                    OPERATOR.old_services()
                main[field] = original
        digest.return_value = "f" * 64
        with self.assertRaisesRegex(RuntimeError, "protected M5e Goby"):
            OPERATOR.old_services()

    def test_baseline_requires_all_1794_pairs_240_sources_and_immutable_old_hashes(self) -> None:
        old_private = Path("/synthetic-history/private")
        old_raw = [old_private / "raw" / (str(index) + ".json") for index in range(1665)]
        old_export = [Path("/synthetic-history/export") / path.name for path in old_raw]
        new_raw = [OPERATOR.PREVIOUS / "private/raw" / (str(index) + ".json") for index in range(128)]
        audit_path = OPERATOR.PREVIOUS / "private/raw/scheduled-tasks-m5f-audit.json"
        new_raw.append(audit_path)
        new_export = [OPERATOR.PREVIOUS / "export" / path.name for path in new_raw]
        media = [Path("/synthetic-history/media") / (str(index) + ".mp4") for index in range(240)]
        removed = "/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-02/.goby-managed"
        baseline = {"records": {str(path): "a" * 64 for path in [*old_raw, *old_export]},
                    "media": {str(path): "a" * 64 for path in media}, "historicalRemovedPaths": [removed]}
        audit = {"cleanupPassed": True, "captureFailureType": None, "httpAttempts": 128, "incompleteHTTP": 0,
                 "scheduledTaskMutationRequests": 0, "serverId": OPERATOR.OLD_SERVER_ID, "referencePID": 3131777}
        records = {OPERATOR.PREVIOUS / "private/baseline.json": baseline, audit_path: audit}
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(records[path]))
        self.replace(OPERATOR, "canonical", lambda *args, **kwargs: None)
        self.replace(Path, "glob", lambda path, pattern: iter(new_raw if path.name == "raw" else new_export))
        self.replace(Path, "rglob", lambda path, pattern: iter(old_raw if path == old_private else new_raw))
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "is_file", lambda path: True)
        self.replace(Path, "exists", lambda path: False)
        self.replace(Path, "stat", lambda path: types.SimpleNamespace(st_size=1))
        digest = Mock(return_value="a" * 64)
        self.replace(OPERATOR, "digest", digest)
        result = OPERATOR.old_baseline()
        self.assertEqual((len(result["records"]), len(result["media"])), (3588, 240))
        audit["scheduledTaskMutationRequests"] = 1
        with self.assertRaisesRegex(RuntimeError, "study does not have proven cleanup"):
            OPERATOR.old_baseline()
        audit["scheduledTaskMutationRequests"] = 0
        name, value = baseline["records"].popitem()
        with self.assertRaisesRegex(RuntimeError, "1794 old raw/export record pairs"):
            OPERATOR.old_baseline()
        baseline["records"][name] = value
        name, value = baseline["media"].popitem()
        with self.assertRaisesRegex(RuntimeError, "240 old source files"):
            OPERATOR.old_baseline()
        baseline["media"][name] = value
        digest.side_effect = lambda path: "b" * 64 if path == media[0] else "a" * 64
        with self.assertRaisesRegex(RuntimeError, "immutable evidence changed"):
            OPERATOR.old_baseline()

    def test_complete_sources_have_512_independent_files_and_preserve_original_hash(self) -> None:
        fixture = self.install_source_snapshot()
        snapshot = OPERATOR.source_snapshot(complete=True)
        self.assertEqual(snapshot["fileCount"], 512)
        self.assertEqual(snapshot["totalBytes"], 512 * 56379)
        self.assertEqual(len({(row["device"], row["inode"]) for row in snapshot["files"].values()}), 512)
        OPERATOR.verify_source(snapshot)
        fixture.hashes[OPERATOR.SOURCE_INPUT] = "f" * 64
        with self.assertRaisesRegex(RuntimeError, "source or original input changed"):
            OPERATOR.verify_source(snapshot)
        self.assertEqual(self.saved, {})

    def test_source_snapshot_rejects_shared_inodes_hardlinks_mutation_and_missing_files(self) -> None:
        fixture = self.install_source_snapshot()
        first, second = fixture.files[:2]
        original_inode = fixture.metadata[second].st_ino
        fixture.metadata[second].st_ino = fixture.metadata[first].st_ino
        with self.assertRaisesRegex(RuntimeError, "share an inode"):
            OPERATOR.source_snapshot(complete=True)
        fixture.metadata[second].st_ino = original_inode
        fixture.metadata[first].st_nlink = 2
        with self.assertRaisesRegex(RuntimeError, "independent regular files"):
            OPERATOR.source_snapshot(complete=False)
        fixture.metadata[first].st_nlink = 1
        fixture.hashes[first] = "f" * 64
        with self.assertRaisesRegex(RuntimeError, "copy differs"):
            OPERATOR.source_snapshot(complete=True)
        fixture.hashes[first] = OPERATOR.SOURCE_SHA256
        fixture.entries.remove(first)
        self.assertEqual(OPERATOR.source_snapshot(complete=False)["fileCount"], 511)
        with self.assertRaisesRegex(RuntimeError, "both complete 256-file batches"):
            OPERATOR.source_snapshot(complete=True)

    def test_invalid_original_source_refuses_before_any_copy_or_service_action(self) -> None:
        self.replace(OPERATOR, "canonical", lambda *args, **kwargs: None)
        self.replace(Path, "stat", lambda path: types.SimpleNamespace(st_size=56379))
        self.replace(OPERATOR, "digest", lambda path: "f" * 64)
        with self.assertRaisesRegex(RuntimeError, "source media provenance differs"):
            OPERATOR.prepare_source()
        self.assertEqual(self.saved, {})

    def test_prepare_source_writes_512_exclusive_copies_without_http_or_library_creation(self) -> None:
        fixture = self.install_source_snapshot()
        content = b"s" * 56379
        expected_hash = hashlib.sha256(content).hexdigest()
        self.replace(OPERATOR, "SOURCE_SHA256", expected_hash)
        for path in [*fixture.files, OPERATOR.SOURCE_INPUT]:
            fixture.hashes[path] = expected_hash
        original = types.SimpleNamespace(st_size=56379, st_dev=41, st_ino=0)
        self.replace(Path, "stat", lambda path: original if path == OPERATOR.SOURCE_INPUT else self.fail("Unexpected original input stat"))
        self.replace(OPERATOR, "canonical", lambda path, **kwargs: original if path == OPERATOR.SOURCE_INPUT else fixture.metadata[path])
        self.replace(OPERATOR, "create_owned_root", lambda path: self.assertEqual(path, OPERATOR.SOURCE))
        allowed_directories = set(OPERATOR.SOURCE_BATCHES.values()) | {path.parent for path in fixture.files}
        self.replace(Path, "mkdir", lambda path, **kwargs: self.assertIn(path, allowed_directories))
        self.replace(Path, "open", lambda path, mode: io.BytesIO(content) if path == OPERATOR.SOURCE_INPUT and mode == "rb"
                     else self.fail("Unexpected original source open"))
        descriptors, copies = {}, {}
        expected_paths = set(fixture.files)

        def open_copy(path: Path, flags: int, mode: int) -> int:
            self.assertIn(path, expected_paths)
            self.assertNotIn(path, descriptors.values())
            self.assertEqual(flags, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW)
            self.assertEqual(mode, 0o600)
            descriptor = 10000 + len(descriptors)
            descriptors[descriptor] = path
            return descriptor

        class MemoryCopy(io.BytesIO):
            def __init__(self, descriptor: int) -> None:
                super().__init__()
                self.descriptor = descriptor

            def fileno(self) -> int:
                return self.descriptor

            def close(self) -> None:
                if not self.closed:
                    copies[descriptors[self.descriptor]] = self.getvalue()
                super().close()

        self.replace(os, "open", open_copy)
        self.replace(os, "fdopen", lambda descriptor, mode: MemoryCopy(descriptor) if mode == "wb" else self.fail("Unexpected copy stream mode"))
        self.replace(os, "fsync", lambda descriptor: self.assertIn(descriptor, descriptors))
        resources = Mock()
        self.replace(OPERATOR, "resources", resources)
        snapshot = OPERATOR.prepare_source()
        self.assertEqual(len(copies), 512)
        self.assertEqual(set(copies), expected_paths)
        self.assertTrue(all(value == content for value in copies.values()))
        self.assertEqual(resources.call_count, 8)
        self.assertEqual(snapshot["fileCount"], 512)
        self.assertEqual(self.saved, {OPERATOR.PRIVATE / "source-manifest.json": snapshot})

    def test_runtime_sandbox_rejects_writable_source_and_host_namespace(self) -> None:
        state = self.service_state()
        identity = {"pid": 12345, "uid": 0, "exe": str(OPERATOR.APP / "system/EmbyServer"),
                    "cmdline": [str(OPERATOR.APP / "system/EmbyServer"), "-programdata", str(OPERATOR.DATA)],
                    "networkNamespace": "net:[fresh]", "serviceProperties": state}
        self.replace(OPERATOR, "owned_roots", lambda: None)
        self.replace(OPERATOR, "owned_root", lambda path: None)
        self.replace(OPERATOR, "service_identity", lambda unit: copy.deepcopy(identity))
        self.replace(OPERATOR, "process_identity", lambda pid: {"networkNamespace": "net:[old]"})
        self.replace(os, "readlink", lambda path: "net:[host]")
        mounts = {"/": "rw", **{path: "ro" for path in OPERATOR.READ_ONLY_PATHS}, str(OPERATOR.DATA): "rw", str(OPERATOR.RUNTIME): "rw"}
        self.replace(Path, "read_text", lambda path: "\n".join("1 0 0:1 / " + point + " " + options + " - tmpfs tmpfs rw"
                     for point, options in mounts.items()))
        self.assertEqual(OPERATOR.new_identity(), identity)
        mounts[str(OPERATOR.SOURCE)] = "rw"
        with self.assertRaisesRegex(RuntimeError, "Actual fresh mount permissions differ"):
            OPERATOR.new_identity()
        mounts[str(OPERATOR.SOURCE)] = "ro"
        state["WorkingDirectory"] = str(OPERATOR.DATA)
        with self.assertRaisesRegex(RuntimeError, "sandbox or launcher differs"):
            OPERATOR.new_identity()
        state["WorkingDirectory"] = str(OPERATOR.RUNTIME)
        identity["networkNamespace"] = "net:[host]"
        with self.assertRaisesRegex(RuntimeError, "shares a protected network namespace"):
            OPERATOR.new_identity()

    def test_stop_requires_owned_invocation_but_does_not_require_ready_application(self) -> None:
        state = self.service_state()
        witness = {"unit": OPERATOR.UNIT, "invocationId": state["InvocationID"], "controlGroup": state["ControlGroup"],
                   "launcherSha256": "1" * 64, "programData": str(OPERATOR.DATA),
                   "serviceProperties": {name: state.get(name, "") for name in OPERATOR.STATIC_PROPERTIES if name != "ExecStart"}}
        self.saved[OPERATOR.DISPATCH] = witness
        self.replace(OPERATOR, "owned_roots", lambda: None)
        self.replace(OPERATOR, "load", lambda path: self.saved[path] if path == OPERATOR.DISPATCH else
                     {"marker": OPERATOR.MARKER} if path == OPERATOR.INTENT else {"sha256": "1" * 64})
        self.replace(OPERATOR, "digest", lambda path: "1" * 64)
        self.replace(OPERATOR, "properties", lambda unit: copy.deepcopy(state))
        self.replace(Path, "exists", lambda path: path in self.saved)
        self.replace(OPERATOR, "new_identity", lambda: self.fail("Stopping must not require application readiness"))
        commands = []

        def process(pid: int) -> dict:
            self.assertEqual(pid, 12345)
            if state["MainPID"] == "0":
                raise FileNotFoundError("Synthetic owned process exited")
            return {"pid": pid, "startTicks": "123", "uid": 0, "cgroup": "0::/system.slice/" + OPERATOR.UNIT,
                    "exe": "/usr/bin/bash", "cmdline": ["/bin/bash", str(OPERATOR.RUNTIME / "launch.sh")]}

        def stop(arguments: list[str], *, timeout: int) -> None:
            self.assertEqual(arguments, ["systemctl", "stop", OPERATOR.UNIT])
            self.assertEqual(timeout, 35)
            commands.append(arguments)
            state.update({"MainPID": "0", "ActiveState": "inactive"})

        self.replace(OPERATOR, "process_identity", process)
        self.replace(OPERATOR, "run", stop)
        state["InvocationID"] = "b" * 32
        with self.assertRaisesRegex(RuntimeError, "replacement unit invocation"):
            OPERATOR.stop_owned(None)
        self.assertEqual(commands, [])
        state["InvocationID"] = "a" * 32
        self.assertTrue(OPERATOR.stop_owned(None)["stopped"])
        self.assertEqual(len(commands), 1)

    def test_partial_cleanup_stops_before_preservation_failure_and_retains_both_roots(self) -> None:
        fixture = self.install_cleanup()

        def reject(_expected: dict) -> None:
            fixture.events.append("old-files-rejected")
            raise RuntimeError("Synthetic old evidence changed")

        self.replace(OPERATOR, "verify_baseline", reject)
        with self.assertRaisesRegex(RuntimeError, "Cleanup is incomplete"):
            OPERATOR.cleanup()
        self.assertEqual(fixture.events, ["stop", "old-services", "old-files-rejected"])
        self.assertTrue({OPERATOR.DATA, OPERATOR.SOURCE} <= fixture.existing)
        fixture.snapshot.assert_not_called()
        report = self.saved[OPERATOR.PRIVATE / "cleanup-attempt-1.json"]
        self.assertFalse(report["dataRemoved"] or report["sourceRemoved"] or report["oldPreservationVerified"])

    def test_partial_cleanup_removes_only_new_source_and_data(self) -> None:
        fixture = self.install_cleanup()
        OPERATOR.cleanup()
        self.assertEqual(fixture.events, ["stop", "old-services", "remove-source", "remove-data", "old-services"])
        fixture.snapshot.assert_called_once_with(complete=False)
        self.assertTrue({OPERATOR.ROOT, OPERATOR.PRIVATE, OPERATOR.INTENT} <= fixture.existing)
        report = self.saved[OPERATOR.PRIVATE / "cleanup-attempt-1.json"]
        self.assertTrue(report["dataRemoved"] and report["sourceRemoved"] and report["oldPreservationVerified"])

    def test_cleanup_after_deletion_retains_failed_audit_and_can_retry_without_new_deletion(self) -> None:
        fixture = self.install_cleanup()
        checks = []

        def audit(_expected: dict) -> None:
            checks.append(True)
            if len(checks) > 1:
                raise RuntimeError("Synthetic historical audit failed after removal")

        self.replace(OPERATOR, "check_old_services", audit)
        for attempt in (1, 2):
            with self.assertRaisesRegex(RuntimeError, "Cleanup is incomplete"):
                OPERATOR.cleanup()
            report = self.saved[OPERATOR.PRIVATE / ("cleanup-attempt-" + str(attempt) + ".json")]
            self.assertFalse(report["oldPreservationVerified"])
        self.assertEqual(fixture.events.count("stop"), 1)
        self.assertEqual(fixture.events.count("remove-source"), 1)
        self.assertEqual(fixture.events.count("remove-data"), 1)
        self.assertTrue(self.saved[OPERATOR.PRIVATE / "cleanup-attempt-1.json"]["dataRemoved"])

    def test_tree_removal_deletes_only_attested_new_source_or_data(self) -> None:
        roots = {OPERATOR.DATA, OPERATOR.SOURCE}
        deleted = []
        self.replace(OPERATOR, "owned_root", lambda path: None)
        self.replace(OPERATOR, "canonical", lambda path, **kwargs: types.SimpleNamespace(st_dev=41, st_ino=1 if path == OPERATOR.DATA else 2))
        self.replace(OPERATOR, "properties", lambda unit: {"MainPID": "0", "ActiveState": "inactive"})
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(self.saved[path]))
        self.replace(Path, "exists", lambda path: path in self.saved)
        self.replace(Path, "rglob", lambda path, pattern: iter([path / ".goby-managed"]))
        self.replace(Path, "resolve", lambda path, **kwargs: path)
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "lstat", lambda path: types.SimpleNamespace(st_uid=0, st_nlink=1, st_mode=stat.S_IFREG | 0o600))
        self.replace(os.path, "ismount", lambda path: False)

        def unlink(path: Path) -> None:
            self.assertIn(path, {root / ".goby-managed" for root in roots})
            deleted.append(path)

        def rmdir(path: Path) -> None:
            self.assertIn(path, roots)
            deleted.append(path)

        self.replace(Path, "unlink", unlink)
        self.replace(Path, "rmdir", rmdir)
        for root in (OPERATOR.SOURCE, OPERATOR.DATA):
            OPERATOR.remove_owned_tree(root)
        self.assertEqual(deleted, [OPERATOR.SOURCE / ".goby-managed", OPERATOR.SOURCE,
                                   OPERATOR.DATA / ".goby-managed", OPERATOR.DATA])
        for foreign in (OPERATOR.ROOT, OPERATOR.PRIVATE, OPERATOR.SOURCE_INPUT.parent, OPERATOR.OLD_DATA):
            with self.subTest(foreign=str(foreign)):
                with self.assertRaisesRegex(RuntimeError, "outside its fixed parent"):
                    OPERATOR.remove_owned_tree(foreign)
        self.assertEqual(len(deleted), 4)

    def test_empty_removal_recovery_requires_original_inode_device_and_marker(self) -> None:
        entries = {OPERATOR.DATA: types.SimpleNamespace(st_dev=41, st_ino=1),
                   OPERATOR.SOURCE: types.SimpleNamespace(st_dev=41, st_ino=2)}
        proofs = {OPERATOR.PRIVATE / "data-removal-intent.json":
                  {"path": str(OPERATOR.DATA), "device": 41, "inode": 1, "marker": OPERATOR.MARKER},
                  OPERATOR.PRIVATE / "source-removal-intent.json":
                  {"path": str(OPERATOR.SOURCE), "device": 41, "inode": 2, "marker": OPERATOR.SOURCE_MARKER}}
        self.replace(OPERATOR, "canonical", lambda path, **kwargs: entries[path])
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(proofs[path]))
        self.replace(OPERATOR, "properties", lambda unit: {"MainPID": "0", "ActiveState": "inactive"})
        self.replace(Path, "exists", lambda path: path in entries or path in self.saved)
        self.replace(Path, "iterdir", lambda path: iter(()))
        self.replace(os.path, "ismount", lambda path: False)
        proof = proofs[OPERATOR.PRIVATE / "data-removal-intent.json"]
        for field, bad in (("device", 99), ("inode", 99), ("marker", "unrelated-owner")):
            with self.subTest(field=field):
                original = proof[field]
                proof[field] = bad
                with self.assertRaisesRegex(RuntimeError, "previously attested empty removal"):
                    OPERATOR.recover_empty_removal_root()
                proof[field] = original
                self.assertEqual(self.saved, {})
        OPERATOR.recover_empty_removal_root()
        self.assertEqual(self.saved, {OPERATOR.DATA / ".goby-managed": OPERATOR.MARKER + "\n",
                                     OPERATOR.SOURCE / ".goby-managed": OPERATOR.SOURCE_MARKER + "\n"})

    def test_command_failure_diagnostics_are_bounded_private_and_append_only(self) -> None:
        folder = OPERATOR.PRIVATE / "command-failures"
        first = folder / "command-failure-001.json"
        self.saved[first] = {"retained": True}
        self.replace(OPERATOR, "owned_root", lambda path: None)
        self.replace(OPERATOR, "canonical", lambda path, **kwargs: None)
        self.replace(Path, "exists", lambda path: path == folder or path in self.saved)
        self.replace(subprocess, "run", lambda *args, **kwargs:
                     types.SimpleNamespace(returncode=1, stdout=b"synthetic", stderr=b"e" * 70000))
        with self.assertRaisesRegex(RuntimeError, "bounded operator command failed"):
            OPERATOR.run(["systemd-run", "--unit=" + OPERATOR.UNIT])
        report = self.saved[folder / "command-failure-002.json"]
        self.assertEqual(report["stderr"]["bytes"], 70000)
        self.assertEqual(len(report["stderr"]["text"]), 65536)
        self.assertTrue(report["stderr"]["truncated"])
        self.assertEqual(self.saved[first], {"retained": True})

    def test_failed_atomic_publication_retains_pending_and_restores_signal_mask(self) -> None:
        self.replace(OPERATOR, "canonical", lambda path, **kwargs: None)
        self.replace(Path, "exists", lambda path: path in self.saved)
        self.replace(Path, "is_symlink", lambda path: False)
        masks = []

        def mask(how: int, signals: set) -> set:
            masks.append((how, set(signals)))
            if how == signal.SIG_BLOCK:
                self.assertEqual(set(signals), {signal.SIGINT, signal.SIGTERM, signal.SIGHUP})
            else:
                self.assertEqual((how, set(signals)), (signal.SIG_SETMASK, set()))
            return set()

        def link(source: Path, target: Path, *, follow_symlinks: bool) -> None:
            self.assertEqual(source, OPERATOR.PRIVATE / ".prepare-intent.json.pending-1")
            self.assertEqual(target, OPERATOR.INTENT)
            self.assertFalse(follow_symlinks)
            raise OSError("Synthetic publication interruption")

        self.replace(signal, "pthread_sigmask", mask)
        self.replace(os, "link", link)
        with self.assertRaisesRegex(OSError, "publication interruption"):
            self.atomic_save(OPERATOR.INTENT, {"synthetic": "complete pending authority"})
        self.assertEqual(self.saved, {OPERATOR.PRIVATE / ".prepare-intent.json.pending-1":
                                     {"synthetic": "complete pending authority"}})
        self.assertEqual(masks, [(signal.SIG_BLOCK, {signal.SIGINT, signal.SIGTERM, signal.SIGHUP}), (signal.SIG_SETMASK, set())])
        with self.assertRaisesRegex(RuntimeError, "Unexpected atomic authority path"):
            self.atomic_save(OPERATOR.PRIVATE / "unapproved.json", {})

    def test_atomic_recovery_unlinks_only_the_exact_pending_inode(self) -> None:
        final = OPERATOR.INTENT
        matching = OPERATOR.PRIVATE / ".prepare-intent.json.pending-1"
        unrelated = OPERATOR.PRIVATE / ".prepare-intent.json.pending-2"
        metadata = {final: types.SimpleNamespace(st_dev=41, st_ino=1, st_nlink=2, st_uid=0, st_mode=stat.S_IFREG | 0o600),
                    matching: types.SimpleNamespace(st_dev=41, st_ino=1), unrelated: types.SimpleNamespace(st_dev=41, st_ino=2)}
        removed = []
        self.replace(OPERATOR, "canonical", lambda path, **kwargs: None)
        self.replace(Path, "exists", lambda path: path == final)
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "resolve", lambda path, **kwargs: path)
        self.replace(Path, "lstat", lambda path: metadata[path])
        self.replace(Path, "glob", lambda path, pattern: iter([matching, unrelated]) if pattern == ".prepare-intent.json.pending-*"
                     else self.fail("Unexpected authority pending enumeration"))
        self.replace(Path, "unlink", lambda path: removed.append(path))
        OPERATOR.recover_atomic_publications()
        self.assertEqual(removed, [matching])
        removed.clear()
        metadata[matching].st_ino = 99
        with self.assertRaisesRegex(RuntimeError, "lacks its exact temporary link"):
            OPERATOR.recover_atomic_publications()
        self.assertEqual(removed, [])

    def test_sanitizer_preserves_only_public_task_keys_and_removes_secret_names_values_and_urls(self) -> None:
        self.replace(time, "monotonic", lambda: 100.0)
        recorder = OPERATOR.Bootstrap({"networkNamespace": "net:[synthetic]"})
        secret = "synthetic/credential+part=one"
        encoded = quote(secret, safe="")
        recorder.secrets.add(secret)
        task = {"Id": "task-1", "Name": "Scan media", "State": "Idle", "Key": "RefreshLibrary",
                "LastExecutionResult": {"Id": "task-1", "Name": "Scan media", "Key": "RefreshLibrary",
                    "Status": "Completed", "StartTimeUtc": "2042-01-02T03:04:05Z", "EndTimeUtc": "2042-01-02T03:04:06Z",
                    "Debug": {"Key": "synthetic-private-result-key"}},
                "Debug": {"Key": "synthetic-private-task-key"}}
        sensitive_task = copy.deepcopy(task)
        sensitive_task["Id"] = "task-2"
        sensitive_task["Key"] = "task-prefix-" + secret
        sensitive_task["LastExecutionResult"]["Key"] = encoded
        record = {"request": {"method": "GET", "path": "/emby/ScheduledTasks", "headers": []},
                  "response": {"status": 200, "bodyType": "json", "headers": [], "body": [task, sensitive_task]},
                  "Key": "synthetic-private-root-key", "decoy": copy.deepcopy(task),
                  "diagnostic-" + secret: {"value": "before " + secret + " after",
                      "url": "https://fixture.invalid/route?returnTo=" + encoded,
                      "rawUrl": "https://fixture.invalid/route/" + secret + "/next"}}
        exported = recorder.sanitize(record)
        first, second = exported["response"]["body"]
        self.assertEqual(first["Key"], "RefreshLibrary")
        self.assertEqual(first["LastExecutionResult"]["Key"], "RefreshLibrary")
        self.assertEqual(first["Debug"]["Key"], "[REDACTED_SECRET]")
        self.assertEqual(first["LastExecutionResult"]["Debug"]["Key"], "[REDACTED_SECRET]")
        self.assertEqual(exported["decoy"]["Key"], "[REDACTED_SECRET]")
        self.assertEqual(exported["Key"], "[REDACTED_SECRET]")
        self.assertIn("[REDACTED_SECRET]", second["Key"])
        self.assertIn("[REDACTED_SECRET]", second["LastExecutionResult"]["Key"])
        serialized = json.dumps(exported)
        self.assertNotIn(secret, serialized)
        self.assertNotIn(encoded, serialized)
        self.assertNotIn("synthetic-private", serialized)
        self.assertEqual(self.saved, {})

    def test_sanitizer_refuses_dictionary_key_collisions_after_redaction(self) -> None:
        self.replace(time, "monotonic", lambda: 100.0)
        recorder = OPERATOR.Bootstrap({"networkNamespace": "net:[synthetic]"})
        first, second = "synthetic/credential-one", "synthetic/credential-two"
        recorder.secrets.update({first, second})
        for names in ((first, second), (first, quote(first, safe="")), (first, "[REDACTED_SECRET]")):
            with self.subTest(names=names):
                with self.assertRaisesRegex(RuntimeError, "[Cc]ollision|duplicate|same.*key"):
                    recorder.sanitize({names[0]: "first value", names[1]: "second value"})
        self.assertEqual(self.saved, {})


def main() -> None:
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "scheduled-tasks-fresh-safety", "status": "blocked",
                          "reason": "Authorized root SSH and one operator source path are required"}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    source_bytes, suite_bytes = source.read_bytes(), Path(__file__).read_bytes()
    SOURCE_LINES[str(source)] = source_bytes.decode().splitlines(keepends=True)
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    global OPERATOR
    OPERATOR = types.ModuleType("synthetic_scheduled_tasks_fresh_operator")
    OPERATOR.__file__ = str(source)
    with EffectFence():
        exec(compile(source_bytes, str(source), "exec"), OPERATOR.__dict__)
    result = unittest.TestResult()
    unittest.defaultTestLoader.loadTestsFromTestCase(ScheduledFixtureSafetyTests).run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in failures[:8]]
    passed = result.wasSuccessful() and result.testsRun > 0
    print(json.dumps({"suite": "scheduled-tasks-fresh-safety", "status": "passed" if passed else "failed",
                      "testsRun": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "skipped": len(result.skipped), "operatorSha256": hashlib.sha256(source_bytes).hexdigest(),
                      "suiteSha256": hashlib.sha256(suite_bytes).hexdigest(), "fixtures": "synthetic-memory-only",
                      "failureSummaries": summaries, "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
