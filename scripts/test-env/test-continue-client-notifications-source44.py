#!/usr/bin/env python3
"""Exercise the source44 continuation with a fenced, in-memory filesystem.

Read only this guard, its sibling continuation, and the SHA-pinned frozen base
operator before installing the test fixtures. The frozen module contributes
definitions only; its CLI, prepare, and execute entry points are never called.
All filesystem, service, network, and clock effects during the cases are either
explicit memory adapters or forbidden. No real fixture or asset is loaded.
"""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import datetime
import fcntl
import hashlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
from unittest.mock import Mock, call, patch


sys.dont_write_bytecode = True
OPERATOR_PATH = Path(__file__).with_name("continue-client-notifications-source44.py")
OPERATOR_RAW = OPERATOR_PATH.read_bytes()
GUARD_RAW = Path(__file__).read_bytes()
OPERATOR_SHA256 = hashlib.sha256(OPERATOR_RAW).hexdigest()
GUARD_SHA256 = hashlib.sha256(GUARD_RAW).hexdigest()
ACTIVE_FENCES = []


def denied(*_args, **_kwargs):
    for fence in ACTIVE_FENCES:
        fence.violations.append("external_effect")
    raise AssertionError("A continuation guard attempted a real external effect.")


class EffectFence(contextlib.ExitStack):
    """Reject unmodeled effects, including denials caught by the operator."""

    def __enter__(self):
        super().__enter__()
        self.violations = []
        ACTIVE_FENCES.append(self)
        surfaces = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "socketpair", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (fcntl, ("flock", "lockf", "fcntl", "ioctl")),
            (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "read", "write", "stat", "lstat", "fstat",
                  "readlink", "listdir", "scandir", "mkdir", "makedirs", "remove", "unlink",
                  "rename", "replace", "rmdir", "chmod", "chown", "link", "symlink",
                  "truncate", "kill", "killpg", "system", "popen", "fsync", "fdatasync",
                  "fchmod", "fchown", "ftruncate", "fork", "execv", "execve", "posix_spawn")),
            (Path, ("open", "read_bytes", "read_text", "write_bytes", "write_text", "stat",
                    "lstat", "resolve", "exists", "is_file", "is_dir", "is_symlink", "mkdir",
                    "touch", "unlink", "rename", "replace", "rmdir", "chmod", "iterdir",
                    "glob", "rglob")),
        )
        for owner, names in surfaces:
            for name in names:
                if hasattr(owner, name):
                    self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments):
        try:
            result = super().__exit__(*arguments)
        finally:
            if ACTIVE_FENCES and ACTIVE_FENCES[-1] is self:
                ACTIVE_FENCES.pop()
        if self.violations:
            raise AssertionError("A real effect was attempted, even if its denial was caught.")
        return result


def definition_module(name, path, raw):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError("A reviewed tool module has no import loader.")
    module = importlib.util.module_from_spec(spec)
    code = spec.loader.source_to_code(raw, str(path))
    sys.modules[name] = module
    with EffectFence():
        exec(code, module.__dict__)
    return module


CONTINUATION = definition_module("source44_continuation_under_test", OPERATOR_PATH, OPERATOR_RAW)
BASE_RAW = CONTINUATION.BASE_OPERATOR.read_bytes()
if hashlib.sha256(BASE_RAW).hexdigest() != CONTINUATION.BASE_SHA:
    raise RuntimeError("The frozen base operator does not match its reviewed SHA-256.")
BASE = definition_module("frozen_source44_upgrade_definitions", CONTINUATION.BASE_OPERATOR, BASE_RAW)


def encoded(value):
    if isinstance(value, bytes):
        return value
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


class MemoryHandle:
    def __init__(self, memory, descriptor):
        self.memory, self.descriptor, self.closed = memory, descriptor, False

    def __enter__(self):
        return self

    def __exit__(self, *_arguments):
        self.close()

    def fileno(self):
        return self.descriptor

    def read(self, size=-1):
        raw = self.memory.raw(self.memory.handles[self.descriptor])
        return raw if size < 0 else raw[:size]

    def close(self):
        if not self.closed:
            self.memory.close(self.descriptor)
            self.closed = True


class MemoryFS:
    """Model exclusive publications and inode-preserving atomic replacements."""

    def __init__(self, output, state_file, binary_file):
        self.output, self.state_file, self.binary_file = map(Path, (output, state_file, binary_file))
        self.entries, self.handles, self.events, self.attempts, self.violations = {}, {}, [], [], []
        self.next_inode, self.next_descriptor = 100, 4000
        self.fail_create = self.fail_replace = None
        self.fail_read = None
        self.fail_read_after_replace = self.pending_read_failure = None
        self.seed_directory(self.output.parent)

    def key(self, path):
        return str(Path(path))

    def violate(self, message):
        self.violations.append(message)
        raise AssertionError(message)

    def allocation(self, raw, mode, directory=False):
        self.next_inode += 1
        return {"raw": raw, "mode": (stat.S_IFDIR if directory else stat.S_IFREG) | mode,
                "inode": self.next_inode, "device": 7, "uid": 0, "gid": 0, "links": 1,
                "mtime_ns": 1_700_000_000_000_000_000 + self.next_inode,
                "ctime_ns": 1_700_000_000_000_000_000 + self.next_inode}

    def seed_directory(self, path, mode=0o700):
        path = Path(path)
        for selected in list(reversed(path.parents)) + [path]:
            key = self.key(selected)
            if key not in self.entries:
                self.entries[key] = self.allocation(None, 0o755 if selected.parent == selected else mode, True)

    def seed(self, path, value, mode=0o600):
        self.seed_directory(Path(path).parent)
        self.entries[self.key(path)] = self.allocation(encoded(value), mode)

    def raw(self, path):
        key = self.key(path)
        self.attempts.append(("read", key))
        if self.pending_read_failure == key:
            self.pending_read_failure = None
            raise CONTINUATION.Failure("Synthetic post-replace readback failure.")
        if self.fail_read == key:
            raise CONTINUATION.Failure("Synthetic publication read failure.")
        if key not in self.entries or self.entries[key]["raw"] is None:
            self.violate("An unmodeled memory file was read: " + key)
        return self.entries[key]["raw"]

    def info(self, path):
        key = self.key(path)
        if key not in self.entries:
            raise FileNotFoundError(key)
        entry = self.entries[key]
        return types.SimpleNamespace(
            st_dev=entry["device"], st_ino=entry["inode"], st_uid=entry["uid"], st_gid=entry["gid"],
            st_mode=entry["mode"], st_nlink=entry["links"],
            st_size=4096 if entry["raw"] is None else len(entry["raw"]),
            st_mtime_ns=entry["mtime_ns"], st_ctime_ns=entry["ctime_ns"],
        )

    def mkdir(self, path, mode=0o777, parents=False, exist_ok=False):
        key = self.key(path)
        if Path(path) != self.output or key in self.entries or mode != 0o700 or parents or exist_ok:
            self.violate("A memory evidence directory was adopted or escaped its declared scope.")
        self.entries[key] = self.allocation(None, mode, True)
        self.events.append(("mkdir", key))

    def create(self, path, value, mode=0o600):
        path, key = Path(path), self.key(path)
        self.attempts.append(("create", key))
        if path.parent != self.output or mode not in (0o600, 0o755):
            self.violate("A create escaped the new continuation output.")
        if self.fail_create == key:
            raise CONTINUATION.Failure("Synthetic exclusive publication failure.")
        if key in self.entries or self.key(path.parent) not in self.entries:
            raise CONTINUATION.Failure("A memory artifact was duplicated or lacks its owned parent.")
        self.entries[key] = self.allocation(encoded(value), mode)
        self.events.append(("create", key))

    def open(self, path, flags, mode=0o777):
        key = self.key(path)
        if flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND):
            self.violate("A raw file open attempted a memory mutation.")
        information = self.info(path)
        if flags & os.O_DIRECTORY and not stat.S_ISDIR(information.st_mode):
            self.violate("A memory directory descriptor referred to a file.")
        self.next_descriptor += 1
        self.handles[self.next_descriptor] = key
        return self.next_descriptor

    def fstat(self, descriptor):
        if descriptor not in self.handles:
            self.violate("An unowned descriptor was inspected.")
        return self.info(self.handles[descriptor])

    def fdopen(self, descriptor, mode):
        if descriptor not in self.handles or mode != "rb":
            self.violate("An unowned descriptor or mode reached fdopen.")
        return MemoryHandle(self, descriptor)

    def close(self, descriptor):
        if descriptor not in self.handles:
            self.violate("An unowned or already closed descriptor was closed.")
        del self.handles[descriptor]

    def replace(self, source, destination):
        source, destination = Path(source), Path(destination)
        left, right = self.key(source), self.key(destination)
        self.attempts.append(("replace", left, right))
        if source.parent != self.output or destination not in (self.state_file, self.binary_file):
            self.violate("An atomic replacement escaped the declared candidate files.")
        if self.fail_replace == left:
            raise CONTINUATION.Failure("Synthetic atomic publication failure.")
        if left not in self.entries or right not in self.entries:
            self.violate("An atomic replacement lacks an existing source or target.")
        self.entries[right] = self.entries.pop(left)
        self.events.append(("replace", left, right))
        if self.fail_read_after_replace == left:
            self.pending_read_failure = right

    def sync(self, path):
        if Path(path) not in (self.output.parent, self.binary_file.parent, self.output):
            self.violate("A sync escaped the memory workspace.")
        self.events.append(("sync", self.key(path)))

    def bind(self, test):
        replacements = (
            (os, "open", self.open), (os, "close", self.close), (os, "fdopen", self.fdopen),
            (os, "fstat", self.fstat), (os, "replace", self.replace),
            (Path, "lstat", lambda path: self.info(path)),
            (Path, "stat", lambda path, **_kwargs: self.info(path)),
            (Path, "is_symlink", lambda path: stat.S_ISLNK(self.info(path).st_mode)),
            (Path, "mkdir", lambda path, mode=0o777, parents=False, exist_ok=False:
             self.mkdir(path, mode, parents, exist_ok)),
        )
        for owner, name, replacement in replacements:
            test.enterContext(patch.object(owner, name, replacement))


class GuardTestCase(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())

    def reject(self, callback):
        with self.assertRaises((CONTINUATION.Failure, BASE.Failure)):
            callback()


def codec():
    return types.SimpleNamespace(
        MARKER="synthetic-client-fixture-v1",
        SCHEMA_27_SOURCE_MARKER="synthetic-schema27-source-v1",
        equal_json=lambda left, right: encoded(left) == encoded(right),
        precise_json=json.loads,
        schema27_binding=lambda state: state.get("schema27_source", {}),
    )


def original_state(op):
    state = {
        "marker": op.MARKER, "schema": 27, "phase": "ready", "stage": "complete",
        "binary_sha256": BASE.OLD_BINARY_SHA, "binary_identity": {"device": 7, "inode": 20},
        "runtime_sha256": BASE.OLD_RUNTIME_SHA, "process": copy.deepcopy(BASE.OLD_PROCESS),
        "viewer_id": BASE.B_USER, "server_id": "synthetic-stable-server",
        "database": {"name": "synthetic-database", "role": "synthetic-role"},
        "credentials": {"path": "/synthetic/private.json", "sha256": "1" * 64},
        "schema27_source": {"marker": op.SCHEMA_27_SOURCE_MARKER, "schema": 27,
            "source": str(BASE.WORK / "source-attempt-32"), "source_manifest_sha256": BASE.OLD_MANIFEST_SHA,
            "catalog_sha256": BASE.CATALOG_SHA, "migration_27_sha256": BASE.MIGRATION_SHA},
        "upgrade": {"phase": "complete", "id": "synthetic-source32-upgrade"},
        "upgrade_history": [{"phase": "complete", "id": "synthetic-earlier-one"},
                            {"phase": "complete", "id": "synthetic-earlier-two"}],
        "special_features_profile": {"phase": "complete", "receipt_path": str(BASE.PROFILE),
                                     "receipt_sha256": BASE.PROFILE_SHA},
        "media_root_extension": {"phase": "complete", "preserved_roots": ["synthetic-extras"]},
    }
    state.update({name: False for name in ("binary_pending", "bootstrap_pending", "credentials_pending",
        "database_creation_pending", "role_creation_pending", "unit_pending", "start_pending", "viewer_pending")})
    return state


def failed_state(original):
    failed = copy.deepcopy(original)
    failed.update(phase="upgrading", stage="notifications_staged", notifications_upgrade={
        "marker": BASE.MARKER, "phase": "staged", "evidence_directory": str(CONTINUATION.FAILED_OUTPUT),
        "publication": BASE.PUBLICATION, "binary_sha256": BASE.BINARY_SHA})
    return failed


def failure_receipts():
    report = {"marker": BASE.MARKER, "status": "retained_for_review", "failure_type": "Failure",
        "phase": "stop_requested", "reason": "An evidence filename escaped the owned directory.",
        "reserved_service_actions": [], "automatic_retry": False, "automatic_rollback": False,
        "last_state_sha256": CONTINUATION.FAILED_STATE_SHA, "failure_observation": "retained_without_mutation"}
    service = {"MainPID": str(BASE.OLD_PROCESS["pid"]), "InvocationID": BASE.OLD_INVOCATION,
        "ActiveState": "active", "SubState": "running", "ControlGroup": "/system.slice/goby-client-m3e.service"}
    terminal = {"unit": CONTINUATION.FAILED_UNIT, "state": {"MainPID": "0", "Result": "exit-code",
        "ExecMainStatus": "1", "ControlGroup": "", "SubState": "failed", "InvocationID": CONTINUATION.FAILED_INVOCATION},
        "recursive_cgroup_empty": True, "failed_report_sha256": CONTINUATION.FAILED_REPORT_SHA,
        "last_state_sha256": CONTINUATION.FAILED_STATE_SHA, "failure_phase": "stop_requested", "reserved_service_actions": []}
    return report, service, terminal


def full_snapshot():
    counts = {"sessions": 73, "devices": 62, "activity_entries": 163, "items": 22, "libraries": 4,
              "play_sessions": 26, "user_item_data": 7, "item_extra_resources": 4, "extra_reserved_paths": 3,
              "client_playback_references": 0, "encoding_jobs": 0}
    tables = {name: [{"id": name + "-" + str(index)} for index in range(count)] for name, count in counts.items()}
    tables["users"] = [{"id": BASE.B_USER, "management_revision": 5, "policy": {"synthetic_preserved_policy": True}}]
    return {"schema": 27, "database": {"tables": tables, "sequences": {"synthetic_sequence": 164},
                                      "catalog": {"synthetic_schema": 27}}}


def final_fixture(op):
    original = original_state(op)
    process = {"pid": 900001, "start_ticks": BASE.OLD_PROCESS["start_ticks"] + 1000,
               "boot_id": BASE.OLD_PROCESS["boot_id"]}
    link = CONTINUATION.origin_link(BASE)
    receipt = {"marker": CONTINUATION.MARKER, "phase": "complete", "old_process": copy.deepcopy(original["process"]),
        "new_process": copy.deepcopy(process), "from_sha256": BASE.OLD_BINARY_SHA, "to_sha256": BASE.BINARY_SHA,
        "from_schema": 27, "to_schema": 27, "installed_binary_identity": {"device": 7, "inode": 30},
        "installed_binary_sha256": BASE.BINARY_SHA, "continuation": copy.deepcopy(link)}
    final = copy.deepcopy(original)
    final.update(binary_sha256=BASE.BINARY_SHA, binary_identity={"device": 7, "inode": 30}, process=copy.deepcopy(process),
        schema27_source=BASE.new_binding(op), upgrade=copy.deepcopy(receipt),
        upgrade_history=copy.deepcopy(original["upgrade_history"]) + [copy.deepcopy(receipt)],
        notifications_upgrade={"marker": CONTINUATION.MARKER, "phase": "complete",
            "evidence_directory": str(CONTINUATION.OUTPUT), "publication": BASE.PUBLICATION,
            "binary_sha256": BASE.BINARY_SHA, "continuation": copy.deepcopy(link)})
    return original, final, process, receipt, link


class ContinuationContractGuards(GuardTestCase):
    def setUp(self):
        super().setUp()
        self.op = codec()

    def test_declares_new_scope_and_exact_frozen_failure_pins(self):
        self.assertEqual(str(CONTINUATION.OUTPUT), "/opt/goby-test/exec-work-m3e/client-notifications-source44-continuation-v1")
        self.assertEqual(str(CONTINUATION.TOOL), "/opt/goby-test/exec-work-m3e/client-notifications-source44-continuation-tool-01")
        self.assertNotEqual(CONTINUATION.OUTPUT, CONTINUATION.FAILED_OUTPUT)
        self.assertNotEqual(CONTINUATION.TOOL, CONTINUATION.BASE_OPERATOR.parent)
        self.assertEqual(CONTINUATION.BASE_SHA, "92da604ba21390d4c861b47a3ff3d7a05e905a646ee3f894392b860ee4a705c1")
        self.assertEqual(CONTINUATION.FAILED_STATE_SHA, "5d528680edffc64c4720d6024de0db3ad3bc0846eede6040302bef63941cc04a")
        self.assertEqual(CONTINUATION.FAILED_OUTPUT_TREE_SHA, "324dd5683a5c9f18452b2985eb5330b2ce07bd38a24e72c11b5b96593f586ec5")
        self.assertEqual(CONTINUATION.FAILED_EXECUTION_TREE_SHA, "9652fc7b15a3a7a899fbf8fb2e5658c46643fb6ee40859b68b2b120d2a9e8733")

    def test_evidence_names_accept_canonical_names_and_exact_length_limit(self):
        for name in ("staged.json", "stop-requested.json", "state-start-requested.json", "a", "a" * 96):
            with self.subTest(name=name):
                self.assertEqual(CONTINUATION.valid_evidence_name(name), name)

    def test_evidence_names_reject_legacy_underscore_and_path_escapes(self):
        for name in ("stop_requested.json", "", ".", "..", "../staged.json", "dir/staged.json", "dir\\staged.json",
                     "/tmp/staged.json", ".hidden", "-phase.json", "Stage.json", "phase name.json", "a" * 97, None, 1):
            with self.subTest(name=name):
                self.reject(lambda: CONTINUATION.valid_evidence_name(name))

    def test_accepts_only_exact_staged_control_delta(self):
        original = original_state(self.op)
        failed = failed_state(original)
        original_copy, failed_copy = copy.deepcopy(original), copy.deepcopy(failed)
        CONTINUATION.validate_failed_state(BASE, self.op, failed, original)
        self.assertEqual(original, original_copy)
        self.assertEqual(failed, failed_copy)

    def test_rejects_failed_state_that_adopts_memory_phase_or_new_scope(self):
        for location, key, value in ((None, "phase", "ready"), (None, "stage", "notifications_stop_requested"),
                                    ("notifications_upgrade", "phase", "stop_requested"),
                                    ("notifications_upgrade", "marker", CONTINUATION.MARKER),
                                    ("notifications_upgrade", "evidence_directory", str(CONTINUATION.OUTPUT))):
            with self.subTest(location=location, field=key):
                original = original_state(self.op)
                failed = failed_state(original)
                (failed if location is None else failed[location])[key] = value
                self.reject(lambda: CONTINUATION.validate_failed_state(BASE, self.op, failed, original))

    def test_rejects_unrelated_failed_state_mutations_or_missing_fields(self):
        for mode in ("credential", "process", "history", "additional", "missing"):
            with self.subTest(mode=mode):
                original = original_state(self.op)
                failed = failed_state(original)
                if mode == "credential":
                    failed["credentials"]["sha256"] = "0" * 64
                elif mode == "process":
                    failed["process"]["pid"] += 1
                elif mode == "history":
                    failed["upgrade_history"].pop()
                elif mode == "additional":
                    failed["unowned_field"] = True
                else:
                    del failed["runtime_sha256"]
                self.reject(lambda: CONTINUATION.validate_failed_state(BASE, self.op, failed, original))

    def test_accepts_exact_failure_receipts(self):
        values = failure_receipts()
        original = copy.deepcopy(values)
        CONTINUATION.validate_failure_receipts(BASE, *values)
        self.assertEqual(values, original)

    def test_rejects_wrong_failed_report_scope_reason_or_service_reservation(self):
        replacements = {"marker": CONTINUATION.MARKER, "status": "passed", "failure_type": "OSError",
            "phase": "staged", "reason": "A different failure.", "reserved_service_actions": ["stop"],
            "automatic_retry": 0, "automatic_rollback": 0, "last_state_sha256": BASE.OLD_STATE_SHA,
            "failure_observation": "not_available"}
        for key, value in replacements.items():
            with self.subTest(field=key):
                values = failure_receipts()
                values[0][key] = value
                self.reject(lambda: CONTINUATION.validate_failure_receipts(BASE, *values))

    def test_rejects_missing_receipt_fields_and_changed_candidate_service(self):
        for index, document in enumerate(failure_receipts()):
            for key in document:
                with self.subTest(document=index, missing=key):
                    values = failure_receipts()
                    del values[index][key]
                    self.reject(lambda: CONTINUATION.validate_failure_receipts(BASE, *values))
        for key, value in (("MainPID", "0"), ("InvocationID", CONTINUATION.FAILED_INVOCATION),
                           ("ActiveState", "inactive"), ("SubState", "dead"), ("ControlGroup", "")):
            with self.subTest(service=key):
                values = failure_receipts()
                values[1][key] = value
                self.reject(lambda: CONTINUATION.validate_failure_receipts(BASE, *values))

    def test_rejects_live_or_unbound_failed_controller_terminal(self):
        for key, value in (("unit", "unowned.service"), ("recursive_cgroup_empty", 1),
                           ("failed_report_sha256", "0" * 64), ("last_state_sha256", BASE.OLD_STATE_SHA),
                           ("failure_phase", "staged"), ("reserved_service_actions", ["stop"])):
            with self.subTest(field=key):
                values = failure_receipts()
                values[2][key] = value
                self.reject(lambda: CONTINUATION.validate_failure_receipts(BASE, *values))
        for key, value in (("MainPID", "123"), ("Result", "success"), ("ExecMainStatus", "0"),
                           ("ControlGroup", "/system.slice/unowned.service"), ("SubState", "running"),
                           ("InvocationID", BASE.OLD_INVOCATION)):
            with self.subTest(state=key):
                values = failure_receipts()
                values[2]["state"][key] = value
                self.reject(lambda: CONTINUATION.validate_failure_receipts(BASE, *values))

    def test_accepts_exact_final_continuation_link_and_single_history_append(self):
        values = final_fixture(self.op)
        original = copy.deepcopy(values)
        CONTINUATION.validate_final_state(BASE, self.op, *values)
        self.assertEqual(values, original)

    def test_rejects_joint_origin_link_tampering(self):
        for key, value in (("failed_output_tree_sha256", "0" * 64),
                           ("failed_execution_tree_sha256", "0" * 64),
                           ("failed_state_sha256", BASE.OLD_STATE_SHA), ("dispatched_service_actions", ["stop"]),
                           ("durable_phase", "stop_requested")):
            with self.subTest(field=key):
                original, final, process, receipt, link = final_fixture(self.op)
                link[key] = value
                receipt["continuation"] = copy.deepcopy(link)
                final["upgrade"] = copy.deepcopy(receipt)
                final["upgrade_history"][-1] = copy.deepcopy(receipt)
                final["notifications_upgrade"]["continuation"] = copy.deepcopy(link)
                self.reject(lambda: CONTINUATION.validate_final_state(BASE, self.op, original, final, process, receipt, link))

    def test_rejects_missing_final_link_and_historical_rewrites(self):
        for mode in ("missing_marker_link", "missing_receipt_link", "rewrite_history", "repeat_receipt", "change_credential"):
            with self.subTest(mode=mode):
                values = final_fixture(self.op)
                original, final, _process, _receipt, _link = values
                if mode == "missing_marker_link":
                    del final["notifications_upgrade"]["continuation"]
                elif mode == "missing_receipt_link":
                    del final["upgrade"]["continuation"]
                elif mode == "rewrite_history":
                    final["upgrade_history"][0]["id"] = "rewritten"
                elif mode == "repeat_receipt":
                    final["upgrade_history"].append(copy.deepcopy(final["upgrade_history"][-1]))
                else:
                    final["credentials"]["sha256"] = "0" * 64
                self.reject(lambda: CONTINUATION.validate_final_state(BASE, self.op, *values))


class ContinuationSimulationGuards(GuardTestCase):
    PHASE_FILENAMES = ("staged.json", "stop-requested.json", "stopped.json", "replace-requested.json",
                       "replaced.json", "start-requested.json")

    def setUp(self):
        super().setUp()
        self.old_binary = b"Synthetic preserved source32 binary.\n" * 64
        self.new_binary = b"Synthetic verified source44 binary.\n" * 64
        for name, value in (("OLD_BINARY_SHA", hashlib.sha256(self.old_binary).hexdigest()),
                            ("BINARY_SHA", hashlib.sha256(self.new_binary).hexdigest()),
                            ("BINARY_BYTES", len(self.new_binary))):
            self.enterContext(patch.object(BASE, name, value))
        self.enterContext(patch.object(BASE, "BINARY_REPORT", {
            "path": str(BASE.BINARY), "sha256": BASE.BINARY_SHA, "bytes": len(self.new_binary)}))
        self.op = codec()
        self.op.UNIT = "goby-client-m3e.service"
        self.op.STATE_FILE = BASE.WORK / "client-fixture.json"
        self.op.BINARY = BASE.WORK / "synthetic-candidate/goby"
        self.memory = MemoryFS(CONTINUATION.OUTPUT, self.op.STATE_FILE, self.op.BINARY)
        self.memory.seed(self.op.BINARY, self.old_binary, 0o755)
        self.op.identity = lambda value: {"device": value.st_dev, "inode": value.st_ino}
        self.original = original_state(self.op)
        self.original["binary_identity"] = self.op.identity(self.memory.info(self.op.BINARY))
        self.enterContext(patch.object(BASE, "OLD_STATE_SHA", hashlib.sha256(encoded(self.original)).hexdigest()))
        self.failed = failed_state(self.original)
        self.enterContext(patch.object(CONTINUATION, "FAILED_STATE_SHA", hashlib.sha256(encoded(self.failed)).hexdigest()))
        self.memory.seed(self.op.STATE_FILE, self.failed)
        self.memory.seed(CONTINUATION.FAILED_OUTPUT / "before-state.json", self.original)
        self.memory.seed(CONTINUATION.FAILED_OUTPUT / "rollback.bin", self.old_binary)
        self.memory.seed(CONTINUATION.FAILED_OUTPUT / "candidate.bin", self.new_binary, 0o755)
        self.memory.seed(CONTINUATION.FAILED_OUTPUT / "failed.json", failure_receipts()[0])
        self.memory.seed(CONTINUATION.FAILED_EXECUTION / "terminal.json", failure_receipts()[2])
        self.memory.seed(CONTINUATION.BASE_OPERATOR, BASE_RAW)
        self.frozen_prefixes = (CONTINUATION.FAILED_OUTPUT, CONTINUATION.FAILED_EXECUTION, CONTINUATION.BASE_OPERATOR.parent)
        self.frozen = self.frozen_entries()
        self.memory.bind(self)
        self.business_snapshot = full_snapshot()
        self.media = {"groups": ["synthetic-original", "synthetic-extras", "synthetic-music"]}
        self.primary = {"process": copy.deepcopy(BASE.PRIMARY_PROCESS), "unchanged": True}
        self.new_process = {"pid": 900001, "start_ticks": BASE.OLD_PROCESS["start_ticks"] + 1000,
                            "boot_id": BASE.OLD_PROCESS["boot_id"]}
        self.service_running = True
        self.service_process = copy.deepcopy(BASE.OLD_PROCESS)
        self.service_invocation = BASE.OLD_INVOCATION
        self.service_actions, self.http_requests, self.connections = [], [], []
        self.op.create, self.op.sync = self.memory.create, self.memory.sync
        self.op.regular = self.regular
        self.op.verify_service = self.verify_service
        self.op.require_candidate_stopped = self.require_stopped
        self.op.verify_database = Mock(return_value=None)
        self.op.verify_fixture_directories = Mock(return_value=None)
        self.op.preservation_snapshot = Mock(side_effect=self.snapshot)
        self.op.preservation_summary = lambda value: {"tables": {name: len(rows) for name, rows in value["database"]["tables"].items()}}
        self.op.NativeAPI = Mock(side_effect=denied)
        self.args = types.SimpleNamespace(mode="deploy", script_sha256=OPERATOR_SHA256)
        self.operator = CONTINUATION.Continuation(self.args, BASE)
        self.operator.op = self.op
        self.operator.original = copy.deepcopy(self.original)
        self.operator.failed_original = copy.deepcopy(self.failed)
        self.operator.state = copy.deepcopy(self.failed)
        self.operator.state_raw = encoded(self.failed)
        self.operator.state_identity = CONTINUATION.identity(self.memory.info(self.op.STATE_FILE))
        self.operator.before = copy.deepcopy(self.business_snapshot)
        self.operator.media = copy.deepcopy(self.media)
        self.operator.primary = copy.deepcopy(self.primary)
        self.operator.failed_trees = {"output": {"synthetic_frozen": True}, "execution": {"synthetic_frozen": True}}
        self.operator.source_proof = {"schema_artifacts": BASE.new_binding(self.op), "synthetic_product_proof": True}
        self.operator.binary_content = self.new_binary
        self.operator.failed_intent = {"source": copy.deepcopy(self.operator.source_proof)}
        self.operator.profile = types.SimpleNamespace(validate_structure=Mock(return_value=None))
        self.operator.restriction = types.SimpleNamespace(quiescent=Mock(return_value=None),
            compare_fixed_snapshot=Mock(side_effect=lambda _op, left, right:
                BASE.require(encoded(left) == encoded(right), "Synthetic database preservation changed.")))
        self.operator.ext = types.SimpleNamespace(media_witness=Mock(side_effect=lambda *_args: copy.deepcopy(self.media)))
        self.operator.primary_fact = Mock(side_effect=lambda: copy.deepcopy(self.primary))
        self.operator.product = Mock(side_effect=lambda: (copy.deepcopy(self.operator.source_proof), self.new_binary))
        self.operator.check_origin = Mock(side_effect=self.check_origin)
        self.operator.prepare = Mock(side_effect=lambda:
            CONTINUATION.validate_failed_state(BASE, self.op, self.operator.state, self.operator.original))
        self.enterContext(patch.object(BASE.subprocess, "run", self.subprocess))
        self.enterContext(patch.object(BASE.http.client, "HTTPConnection", self.connection))
        self.enterContext(patch.object(BASE.Upgrade, "prepare", denied))
        self.enterContext(patch.object(BASE.Upgrade, "execute", denied))
        self.enterContext(patch.object(BASE.Upgrade, "run", denied))
        self.enterContext(patch.object(BASE, "load_helper", denied))
        self.enterContext(patch.object(CONTINUATION, "load_base", denied))

    def tearDown(self):
        self.assertEqual(self.memory.violations, [])
        self.assertEqual(self.memory.handles, {})
        self.assertEqual(self.frozen_entries(), self.frozen)
        self.op.NativeAPI.assert_not_called()
        self.assertTrue(all(connection.closed for connection in self.connections))

    def frozen_entries(self):
        return {key: copy.deepcopy(value) for key, value in self.memory.entries.items()
                if any(Path(key).is_relative_to(prefix) for prefix in self.frozen_prefixes)}

    def check_origin(self):
        self.assertEqual(self.frozen_entries(), self.frozen)
        return copy.deepcopy(self.operator.failed_trees)

    def regular(self, path, mode=0o600, limit=64 << 20):
        information = self.memory.info(path)
        self.assertEqual(stat.S_IMODE(information.st_mode), mode)
        self.assertLessEqual(information.st_size, limit)
        return information

    def verify_service(self, _state, allow_new=False):
        return copy.deepcopy(self.service_process) if self.service_running else None

    def require_stopped(self):
        BASE.require(not self.service_running, "The synthetic candidate has not stopped.")

    def snapshot(self, _state, schema):
        self.assertEqual(schema, 27)
        return copy.deepcopy(self.business_snapshot)

    def service_properties(self):
        return {"MainPID": str(self.service_process["pid"]) if self.service_running else "0",
                "InvocationID": self.service_invocation,
                "ActiveState": "active" if self.service_running else "inactive",
                "SubState": "running" if self.service_running else "dead",
                "ControlGroup": "/system.slice/" + self.op.UNIT if self.service_running else ""}

    def subprocess(self, arguments, **_kwargs):
        arguments = list(arguments)
        output = ""
        if arguments[:2] == ["/usr/bin/systemctl", "show"]:
            self.assertEqual(arguments[2], self.op.UNIT)
            names = arguments[4].removeprefix("--property=").split(",")
            values = self.service_properties()
            output = "\n".join(name + "=" + values[name] for name in names)
        elif arguments[:2] in (["/usr/bin/systemctl", "stop"], ["/usr/bin/systemctl", "start"]):
            action = arguments[1]
            self.assertEqual(arguments, ["/usr/bin/systemctl", action, self.op.UNIT])
            self.assertEqual(self.operator.phase, action + "_requested")
            self.assertEqual(self.operator.fence.stage, action + "_requested")
            durable = json.loads(self.memory.raw(self.op.STATE_FILE))
            self.assertEqual(durable["notifications_upgrade"]["phase"], action + "_requested")
            self.assertEqual(durable["notifications_upgrade"]["marker"], CONTINUATION.MARKER)
            self.assertEqual(self.operator.state_raw, encoded(durable))
            self.assertIn(action, self.operator.fence.reserved)
            if action == "stop":
                self.assertEqual(self.service_actions, [])
                self.assertTrue(self.service_running)
                self.service_running = False
            else:
                self.assertEqual(self.service_actions, ["stop"])
                self.assertFalse(self.service_running)
                self.assertEqual(self.memory.raw(self.op.BINARY), self.new_binary)
                self.service_running = True
                self.service_process = copy.deepcopy(self.new_process)
                self.service_invocation = "a1" * 16
            self.service_actions.append(action)
            self.memory.events.append(("service", action))
        elif arguments == ["/usr/bin/ss", "-H", "-ltnp", "sport = :18198"]:
            self.assertTrue(self.service_running)
            output = "Synthetic owned listener"
        else:
            self.memory.violate("An unmodeled command reached the memory subprocess adapter: " + repr(arguments))
        return types.SimpleNamespace(returncode=0, stdout=output, stderr="")

    def connection(self, host, port, timeout):
        self.assertEqual((host, port, timeout), ("127.0.0.1", 18198, 15))
        connection = types.SimpleNamespace(closed=False, route=None)

        def request(method, route, body, headers):
            self.assertEqual(method, "GET")
            self.assertIsNone(body)
            self.assertEqual(headers, {"Accept": "application/json", "Origin": "http://127.0.0.1:18196"})
            expected = ("/readyz", "/emby/System/Info/Public")[len(self.http_requests)]
            self.assertEqual(route, expected)
            connection.route = route
            self.http_requests.append(route)
            self.memory.events.append(("http", route))

        def response():
            value = {"Status": "ready"} if connection.route == "/readyz" else {
                "Id": self.original["server_id"], "ProductName": "Goby", "Version": "4.9.5.0", "GobyVersion": "synthetic-source44"}
            raw = encoded(value)
            return types.SimpleNamespace(status=200, read=lambda limit: raw[:limit], getheader=lambda _name: None)

        def close():
            connection.closed = True

        connection.request, connection.getresponse, connection.close = request, response, close
        self.connections.append(connection)
        return connection

    def read_output(self, name):
        return json.loads(self.memory.raw(CONTINUATION.OUTPUT / name))

    def test_complete_execute_crosses_real_phase_names_and_atomic_publications(self):
        result = self.operator.run()
        self.assertEqual(result["status"], "passed")
        self.assertEqual(self.service_actions, ["stop", "start"])
        self.assertEqual(self.http_requests, ["/readyz", "/emby/System/Info/Public"])
        self.assertEqual(self.operator.fence.reserved, {"stop", "start"})
        self.assertEqual(self.memory.raw(self.op.BINARY), self.new_binary)
        self.assertEqual(self.operator.phase, "complete")
        self.assertEqual(self.operator.state["phase"], "ready")
        self.assertEqual(self.operator.state["stage"], "complete")
        self.assertEqual(self.operator.state["process"], self.new_process)
        self.assertEqual(result["state_sha256"], hashlib.sha256(self.memory.raw(self.op.STATE_FILE)).hexdigest())
        self.assertEqual(self.operator.state["upgrade_history"][:-1], self.original["upgrade_history"])
        self.assertEqual(len(self.operator.state["upgrade_history"]), len(self.original["upgrade_history"]) + 1)
        for name in self.PHASE_FILENAMES:
            self.assertIn(name, self.operator.records)
            self.assertEqual(self.operator.records[name]["path"], str(CONTINUATION.OUTPUT / name))
            self.assertEqual(self.operator.records[name]["sha256"], hashlib.sha256(self.memory.raw(CONTINUATION.OUTPUT / name)).hexdigest())
            phase = name.removesuffix(".json").replace("-", "_")
            self.assertEqual(self.read_output(name)["phase"], phase)
            journal = self.memory.events.index(("create", str(CONTINUATION.OUTPUT / name)))
            state = self.memory.events.index(("replace", str(CONTINUATION.OUTPUT / ("state-" + name)), str(self.op.STATE_FILE)))
            self.assertLess(journal, state)
            if phase in ("stop_requested", "start_requested"):
                dispatch = self.memory.events.index(("service", phase.split("_")[0]))
                self.assertLess(state, dispatch)
        self.assertTrue(all("_" not in name for name in self.operator.records))
        self.assertEqual(self.read_output("before-full.json"), self.business_snapshot)
        self.assertEqual(self.read_output("after-full.json"), self.business_snapshot)
        report = self.read_output("report.json")
        self.assertEqual(report["http_requests"], 2)
        self.assertEqual(report["continuation"], CONTINUATION.origin_link(BASE))
        self.assertIs(report["original_failure_trees_preserved"], True)
        self.assertIs(report["client_acceptance"], False)

    def assert_before_stop_failure(self, persisted_phase="staged"):
        self.reject(self.operator.run)
        self.assertEqual(self.service_actions, [])
        self.assertEqual(self.http_requests, [])
        self.assertEqual(self.operator.phase, "staged")
        self.assertEqual(self.operator.fence.stage, "staged")
        self.assertEqual(self.operator.fence.reserved, set())
        self.assertTrue(self.service_running)
        self.assertEqual(self.service_process, BASE.OLD_PROCESS)
        self.assertEqual(self.memory.raw(self.op.BINARY), self.old_binary)
        durable = json.loads(self.memory.raw(self.op.STATE_FILE))
        self.assertEqual(durable["notifications_upgrade"]["phase"], persisted_phase)
        report = self.read_output("failed.json")
        self.assertEqual(report["phase"], "staged")
        self.assertEqual(report["reserved_service_actions"], [])
        self.assertIs(report["automatic_retry"], False)
        self.assertIs(report["automatic_rollback"], False)
        self.assertIs(report["failure_observed"], True)
        self.assertEqual(report["last_acknowledged_state_sha256"], hashlib.sha256(self.operator.state_raw).hexdigest())
        self.assertEqual(report["observed_state_sha256"], hashlib.sha256(self.memory.raw(self.op.STATE_FILE)).hexdigest())
        self.assertEqual(self.memory.raw(CONTINUATION.OUTPUT / "failure-fixture-state.json"), self.memory.raw(self.op.STATE_FILE))
        return report

    def test_phase_journal_failure_prevents_stop_and_phase_advancement(self):
        target = CONTINUATION.OUTPUT / "stop-requested.json"
        self.memory.fail_create = str(target)
        self.assert_before_stop_failure()
        self.assertNotIn(str(target), self.memory.entries)
        self.assertNotIn(("create", str(CONTINUATION.OUTPUT / "state-stop-requested.json")), self.memory.attempts)

    def test_phase_state_create_failure_keeps_journal_but_prevents_stop(self):
        target = CONTINUATION.OUTPUT / "state-stop-requested.json"
        self.memory.fail_create = str(target)
        self.assert_before_stop_failure()
        self.assertIn("stop-requested.json", self.operator.records)
        self.assertNotIn(str(target), self.memory.entries)
        self.assertNotIn(("replace", str(target), str(self.op.STATE_FILE)), self.memory.attempts)

    def test_phase_atomic_replace_failure_prevents_stop_and_preserves_acknowledged_state(self):
        target = CONTINUATION.OUTPUT / "state-stop-requested.json"
        self.memory.fail_replace = str(target)
        self.assert_before_stop_failure()
        self.assertIn(str(target), self.memory.entries)
        self.assertNotIn(("replace", str(target), str(self.op.STATE_FILE)), self.memory.events)

    def test_post_replace_readback_failure_retains_actual_state_without_service_authority(self):
        target = CONTINUATION.OUTPUT / "state-stop-requested.json"
        self.memory.fail_read_after_replace = str(target)
        report = self.assert_before_stop_failure(persisted_phase="stop_requested")
        self.assertNotEqual(report["last_acknowledged_state_sha256"], report["observed_state_sha256"])
        self.assertIn(("replace", str(target), str(self.op.STATE_FILE)), self.memory.events)
        self.assertEqual(json.loads(self.operator.state_raw)["notifications_upgrade"]["phase"], "staged")

    def test_start_phase_publication_failure_does_not_restart_or_roll_back(self):
        self.memory.fail_create = str(CONTINUATION.OUTPUT / "start-requested.json")
        self.reject(self.operator.run)
        self.assertEqual(self.service_actions, ["stop"])
        self.assertEqual(self.http_requests, [])
        self.assertFalse(self.service_running)
        self.assertEqual(self.memory.raw(self.op.BINARY), self.new_binary)
        self.assertEqual(self.operator.phase, "replaced")
        self.assertEqual(self.operator.fence.stage, "replaced")
        self.assertEqual(self.operator.fence.reserved, {"stop"})
        self.assertEqual(self.read_output("failed.json")["reserved_service_actions"], ["stop"])
        self.assertNotIn(("create", str(CONTINUATION.OUTPUT / "state-start-requested.json")), self.memory.attempts)

    def test_real_save_rejects_legacy_filename_before_any_create(self):
        self.reject(lambda: self.operator.save("stop_requested.json", {"phase": "stop_requested"}))
        self.assertEqual(self.memory.events, [])
        self.assertEqual(self.operator.records, {})
        self.assertEqual(self.operator.phase, "preflight")
        self.assertEqual(self.operator.fence.stage, "preflight")
        self.assertEqual(self.service_actions, [])

    def test_undeclared_phase_is_rejected_before_journal_or_state_publication(self):
        for phase in ("stop-requested", "complete", "../staged", "unknown_phase"):
            with self.subTest(phase=phase):
                self.reject(lambda: self.operator.phase_record(phase))
        self.assertEqual(self.memory.events, [])
        self.assertEqual(self.memory.raw(self.op.STATE_FILE), encoded(self.failed))
        self.assertEqual(self.operator.phase, "preflight")
        self.assertEqual(self.operator.fence.reserved, set())

    def test_preflight_does_not_create_output_or_dispatch_actions(self):
        self.args.mode = "preflight"
        result = self.operator.run()
        self.assertEqual(result["status"], "preflight_passed")
        self.assertEqual(result["new_evidence_writes"], 0)
        self.assertEqual(self.memory.events, [])
        self.assertNotIn(str(CONTINUATION.OUTPUT), self.memory.entries)
        self.assertEqual(self.memory.raw(self.op.STATE_FILE), encoded(self.failed))
        self.assertEqual(self.service_actions, [])
        self.assertEqual(self.http_requests, [])


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(stream=sys.stderr, verbosity=2).run(suite)
    passed = result.wasSuccessful() and not result.skipped
    report = {"suite": "client-notifications-source44-continuation-guards",
              "status": "passed" if passed else "failed", "operator_sha256": OPERATOR_SHA256,
              "guard_sha256": GUARD_SHA256, "test_count": result.testsRun,
              "failures": len(result.failures), "errors": len(result.errors), "skips": len(result.skipped)}
    sys.stdout.write(json.dumps(report, sort_keys=True, separators=(",", ":")) + "\n")
    raise SystemExit(0 if passed else 1)
