#!/usr/bin/env python3
"""Exercise the actual schema28 controller with synthetic, fenced adapters.

Only this guard and its sibling controller are read before the effect fence is
installed. Published helpers, databases, credentials, binaries, and assets are
never loaded. Run only in an authorized remote verification environment.
"""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import datetime
from decimal import Decimal
import errno
import fcntl
import hashlib
import html.parser
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import signal
import shlex
import shutil
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
import urllib.parse
from unittest.mock import Mock, call, patch


sys.dont_write_bytecode = True
OPERATOR_PATH = Path(__file__).with_name("upgrade-client-schema28.py")
OPERATOR_RAW = OPERATOR_PATH.read_bytes()
GUARD_RAW = Path(__file__).read_bytes()
OPERATOR_SHA256 = hashlib.sha256(OPERATOR_RAW).hexdigest()
GUARD_SHA256 = hashlib.sha256(GUARD_RAW).hexdigest()
MASTER_KEY_PATH = Path("/var/lib/goby-test/client-m3e/master.key")
MASTER_KEY_BYTES = b"0123456789abcdef" * 2
MASTER_RUNTIME_LINE = b"GOBY_API_KEY_MASTER_KEY_FILE='/var/lib/goby-test/client-m3e/master.key'\n"
ACTIVE_FENCES = []
OBSERVED_FENCES = []


def denied(*_arguments, **_keywords):
    for fence in ACTIVE_FENCES:
        fence.violations.append("external_effect")
    raise AssertionError("An unmocked effect escaped the schema28 memory adapters.")


class EffectFence(contextlib.ExitStack):
    """Reject real effects even when the controller catches the denial."""

    def __enter__(self):
        super().__enter__()
        self.violations = []
        self.expected_denial = False
        ACTIVE_FENCES.append(self)
        OBSERVED_FENCES.append(self)
        surfaces = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "socketpair", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (shutil, ("copyfile", "copy", "copy2", "copytree", "move", "rmtree", "chown")),
            (fcntl, ("flock", "lockf", "fcntl", "ioctl")),
            (time, ("sleep",)),
            (signal, ("signal", "alarm")),
            (os, ("open", "close", "fdopen", "read", "write", "stat", "lstat", "fstat",
                  "readlink", "listdir", "scandir", "walk", "mkdir", "makedirs", "remove", "unlink",
                  "rename", "replace", "rmdir", "chmod", "chown", "link", "symlink", "truncate",
                  "kill", "killpg", "system", "popen", "fsync", "fdatasync", "fchmod", "fchown",
                  "ftruncate", "fork", "execv", "execve", "posix_spawn", "pipe", "pipe2", "chdir",
                  "fchdir", "utime", "mkfifo", "mknod", "setuid", "setgid", "setgroups", "setpgid",
                  "setsid", "putenv", "unsetenv", "umask")),
            (Path, ("open", "read_bytes", "read_text", "write_bytes", "write_text", "stat", "lstat",
                    "resolve", "exists", "is_file", "is_dir", "is_symlink", "mkdir", "touch", "unlink",
                    "rename", "replace", "rmdir", "chmod", "iterdir", "glob", "rglob", "readlink",
                    "symlink_to", "hardlink_to")),
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
            raise AssertionError("A schema28 guard attempted an unmocked external effect.")
        return result


def definition_module():
    spec = importlib.util.spec_from_file_location("schema28_controller_under_test", OPERATOR_PATH)
    if spec is None or spec.loader is None:
        raise RuntimeError("The sibling schema28 controller cannot be loaded.")
    module = importlib.util.module_from_spec(spec)
    code = spec.loader.source_to_code(OPERATOR_RAW, str(OPERATOR_PATH))
    sys.modules[spec.name] = module
    with EffectFence():
        exec(code, module.__dict__)
    return module


UPGRADE = definition_module()


def encoded(value):
    if isinstance(value, bytes):
        return value
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def synthetic_hash(label):
    return hashlib.sha256(("schema28-synthetic:" + label).encode()).hexdigest()


class MemoryHandle:
    def __init__(self, memory, descriptor, mode="rb", encoding="utf-8"):
        self.memory, self.descriptor, self.mode, self.encoding = memory, descriptor, mode, encoding
        self.closed = False

    def __enter__(self):
        return self

    def __exit__(self, *_arguments):
        self.close()

    def fileno(self):
        return self.descriptor

    def read(self, size=-1):
        handle = self.memory.handle(self.descriptor)
        if size < 0:
            size = len(handle["entry"]["raw"]) - handle["offset"]
        raw = self.memory.read(self.descriptor, size)
        return raw if "b" in self.mode else raw.decode(self.encoding)

    def write(self, value):
        raw = value if isinstance(value, bytes) else value.encode(self.encoding)
        return self.memory.write(self.descriptor, raw)

    def flush(self):
        handle = self.memory.handle(self.descriptor)
        self.memory.events.append(("flush", handle["path"]))

    def close(self):
        if not self.closed:
            self.memory.close(self.descriptor)
            self.closed = True


class MemoryFS:
    """Retain exact bytes, inode identity, and exclusive publication attempts."""

    def __init__(self, output, replace_targets=(), extra_create_files=(), extra_roots=()):
        self.output = Path(output)
        self.replace_targets = {str(Path(value)) for value in replace_targets}
        self.extra_create_files = {str(Path(value)) for value in extra_create_files}
        self.extra_roots = tuple(Path(value) for value in extra_roots)
        self.entries, self.handles, self.events, self.attempts, self.violations = {}, {}, [], [], []
        self.next_inode, self.next_descriptor = 100, 4000
        self.fail_create = self.fail_replace = self.fail_read = None
        self.umask = 0o077
        self.seed_directory(self.output.parent)

    @staticmethod
    def key(path):
        return str(Path(path))

    def violation(self, message):
        self.violations.append(message)
        raise AssertionError(message)

    def owned(self, path):
        selected = Path(path)
        return (selected.is_absolute() and ".." not in selected.parts and "\\" not in str(selected)
                and (selected == self.output or selected.is_relative_to(self.output)
                     or str(selected) in self.extra_create_files
                     or any(selected == root or selected.is_relative_to(root) for root in self.extra_roots)))

    def allocation(self, raw, mode, directory=False):
        self.next_inode += 1
        return {"raw": raw, "mode": (stat.S_IFDIR if directory else stat.S_IFREG) | mode,
                "inode": self.next_inode, "device": 7, "uid": 0, "gid": 0, "links": 1,
                "mtime_ns": 1_800_000_000_000_000_000 + self.next_inode,
                "ctime_ns": 1_800_000_000_000_000_000 + self.next_inode}

    def seed_directory(self, path, mode=0o700):
        selected = Path(path)
        for parent in list(reversed(selected.parents)) + [selected]:
            key = self.key(parent)
            if key not in self.entries:
                self.entries[key] = self.allocation(None, 0o755 if parent.parent == parent else mode, True)

    def seed(self, path, value, mode=0o600):
        self.seed_directory(Path(path).parent)
        self.entries[self.key(path)] = self.allocation(encoded(value), mode)

    def raw(self, path):
        key = self.key(path)
        self.attempts.append(("read", key))
        if self.fail_read == key:
            raise UPGRADE.Failure("Synthetic frozen input read failed.")
        if key not in self.entries or self.entries[key]["raw"] is None:
            self.violation("An undeclared memory file was read.")
        return self.entries[key]["raw"]

    def info(self, path):
        key = self.key(path)
        if key not in self.entries:
            raise FileNotFoundError(key)
        return self.entry_info(self.entries[key])

    @staticmethod
    def entry_info(entry):
        return types.SimpleNamespace(st_dev=entry["device"], st_ino=entry["inode"], st_uid=entry["uid"],
            st_gid=entry["gid"], st_mode=entry["mode"], st_nlink=entry["links"],
            st_size=4096 if entry["raw"] is None else len(entry["raw"]),
            st_mtime_ns=entry["mtime_ns"], st_ctime_ns=entry["ctime_ns"])

    def mkdir(self, path, mode=0o777, parents=False, exist_ok=False):
        key = self.key(path)
        self.attempts.append(("mkdir", key))
        expected_mode = 0o700 if Path(path) == self.output or Path(path).is_relative_to(self.output) else 0o755
        if not self.owned(path) or key in self.entries or mode != expected_mode or parents or exist_ok:
            self.violation("An evidence directory was adopted or created outside the declared scope.")
        parent = self.entries.get(self.key(Path(path).parent))
        if parent is None or not stat.S_ISDIR(parent["mode"]):
            self.violation("An evidence directory lacks its declared parent.")
        self.entries[key] = self.allocation(None, mode & ~self.umask, True)
        self.events.append(("mkdir", key))

    def create(self, path, value, mode=0o600):
        key = self.key(path)
        self.attempts.append(("create", key))
        if self.fail_create == key:
            raise UPGRADE.Failure("Synthetic exclusive publication failed.")
        parent = self.entries.get(self.key(Path(path).parent))
        if not self.owned(path) or key in self.entries or parent is None or not stat.S_ISDIR(parent["mode"]):
            self.violation("A publication was adopted or escaped the new evidence scope.")
        self.entries[key] = self.allocation(encoded(value), mode & ~self.umask)
        self.events.append(("create", key))

    def replace(self, source, target):
        source, target = self.key(source), self.key(target)
        self.attempts.append(("replace", source, target))
        if self.fail_replace == source:
            raise UPGRADE.Failure("Synthetic atomic replacement failed.")
        if not self.owned(source) or source not in self.entries or target not in self.replace_targets:
            self.violation("An atomic replacement escaped its declared candidate target.")
        destination = self.entries.get(target)
        parent = self.entries.get(self.key(Path(target).parent))
        origin = self.entries[source]
        if (destination is None or parent is None or not stat.S_ISDIR(parent["mode"])
                or not stat.S_ISREG(origin["mode"]) or not stat.S_ISREG(destination["mode"])):
            self.violation("An atomic replacement does not name two declared regular files.")
        if origin["device"] != parent["device"]:
            raise OSError(errno.EXDEV, "Synthetic atomic replacement crossed filesystems.")
        destination["links"] -= 1
        self.entries[target] = self.entries.pop(source)
        self.events.append(("replace", source, target))

    def open(self, path, flags, mode=0o777):
        key = self.key(path)
        if flags & os.O_CREAT:
            if not flags & os.O_EXCL:
                self.violation("A memory publication omitted exclusive creation.")
            self.create(path, b"", mode)
        elif flags & (os.O_WRONLY | os.O_RDWR | os.O_TRUNC):
            self.violation("An existing memory file was opened for mutation.")
        if key not in self.entries:
            raise FileNotFoundError(key)
        entry = self.entries[key]
        if flags & os.O_DIRECTORY and not stat.S_ISDIR(entry["mode"]):
            raise OSError(errno.ENOTDIR, "Synthetic directory descriptor targets a file.")
        if flags & os.O_NOFOLLOW and stat.S_ISLNK(entry["mode"]):
            raise OSError(errno.ELOOP, "Synthetic protected file became a symlink.")
        self.next_descriptor += 1
        self.handles[self.next_descriptor] = {"path": key, "entry": entry, "offset": 0, "flags": flags}
        return self.next_descriptor

    def handle(self, descriptor):
        if descriptor not in self.handles:
            self.violation("An undeclared memory descriptor was used.")
        return self.handles[descriptor]

    def read(self, descriptor, size):
        if descriptor not in self.handles or not isinstance(size, int) or size < 0:
            self.violation("An unowned memory handle attempted a read.")
        handle = self.handle(descriptor)
        if (handle["flags"] & os.O_ACCMODE) == os.O_WRONLY or handle["entry"]["raw"] is None:
            self.violation("A memory descriptor is not readable.")
        self.attempts.append(("read_fd", handle["path"]))
        if self.fail_read == handle["path"]:
            raise UPGRADE.Failure("Synthetic frozen input read failed.")
        raw = handle["entry"]["raw"]
        selected = raw[handle["offset"]:handle["offset"] + size]
        handle["offset"] += len(selected)
        return selected

    def write(self, descriptor, raw):
        if descriptor not in self.handles:
            self.violation("An unowned memory handle attempted a write.")
        handle = self.handle(descriptor)
        if (not handle["flags"] & os.O_CREAT or not self.owned(handle["path"])
                or (handle["flags"] & os.O_ACCMODE) not in (os.O_WRONLY, os.O_RDWR)):
            self.violation("An unowned memory handle attempted a write.")
        entry = handle["entry"]
        offset = handle["offset"]
        entry["raw"] = entry["raw"][:offset] + bytes(raw) + entry["raw"][offset + len(raw):]
        handle["offset"] += len(raw)
        return len(raw)

    def close(self, descriptor):
        if descriptor not in self.handles:
            self.violation("An unowned memory handle was closed.")
        del self.handles[descriptor]

    def open_path(self, path, mode="r", encoding=None, **_keywords):
        if mode in ("r", "rb"):
            flags = os.O_RDONLY
        elif mode in ("x", "xb"):
            flags = os.O_CREAT | os.O_EXCL | os.O_WRONLY
        else:
            self.violation("A memory file used an undeclared open mode.")
        return MemoryHandle(self, self.open(path, flags, 0o600), mode, encoding or "utf-8")

    def sync(self, path):
        if self.key(path) not in self.entries:
            self.violation("An undeclared memory directory was synchronized.")
        self.events.append(("sync", self.key(path)))

    def sync_handle(self, descriptor):
        self.events.append(("fsync", self.handle(descriptor)["path"]))

    def fchmod(self, descriptor, mode):
        handle = self.handle(descriptor)
        if not handle["flags"] & os.O_CREAT or not self.owned(handle["path"]):
            self.violation("An existing file received a mode mutation.")
        handle["entry"]["mode"] = stat.S_IFMT(handle["entry"]["mode"]) | mode

    def chmod(self, path, mode):
        key = self.key(path)
        if not self.owned(path) or key not in self.entries:
            self.violation("An unowned path received a mode mutation.")
        self.entries[key]["mode"] = stat.S_IFMT(self.entries[key]["mode"]) | mode
        self.events.append(("chmod", key, mode))

    def set_umask(self, mode):
        previous = self.umask
        self.umask = mode
        return previous

    def children(self, path, recursive=False, pattern="*"):
        selected = Path(path)
        return iter(sorted(Path(key) for key in self.entries if Path(key) != selected
            and (Path(key).is_relative_to(selected) if recursive else Path(key).parent == selected)
            and (pattern == "*" or Path(key).name == pattern)))

    def bind(self, test):
        replacements = (
            (os, "open", self.open), (os, "read", self.read), (os, "write", self.write), (os, "close", self.close),
            (os, "stat", lambda path, **_kwargs: self.info(path)),
            (os, "lstat", lambda path, **_kwargs: self.info(path)),
            (os, "fstat", lambda descriptor: self.entry_info(self.handle(descriptor)["entry"])),
            (os, "fsync", self.sync_handle),
            (os, "fchmod", self.fchmod),
            (os, "chmod", self.chmod),
            (os, "umask", self.set_umask),
            (os, "replace", self.replace),
            (os.path, "lexists", lambda path: self.key(path) in self.entries),
            (os, "fdopen", lambda descriptor, mode="r", **_kwargs: MemoryHandle(self, descriptor, mode)),
            (Path, "open", lambda path, mode="r", **kwargs: self.open_path(path, mode, **kwargs)),
            (Path, "read_bytes", lambda path: self.raw(path)),
            (Path, "read_text", lambda path, encoding=None, **_kwargs: self.raw(path).decode(encoding or "utf-8")),
            (Path, "stat", lambda path, **_kwargs: self.info(path)),
            (Path, "lstat", lambda path, **_kwargs: self.info(path)),
            (Path, "exists", lambda path: self.key(path) in self.entries),
            (Path, "is_file", lambda path: self.key(path) in self.entries and self.entries[self.key(path)]["raw"] is not None),
            (Path, "is_dir", lambda path: self.key(path) in self.entries and self.entries[self.key(path)]["raw"] is None),
            (Path, "is_symlink", lambda path: self.key(path) in self.entries and stat.S_ISLNK(self.entries[self.key(path)]["mode"])),
            (Path, "mkdir", lambda path, mode=0o777, parents=False, exist_ok=False: self.mkdir(path, mode, parents, exist_ok)),
            (Path, "iterdir", lambda path: self.children(path)),
            (Path, "rglob", lambda path, pattern: self.children(path, recursive=True, pattern=pattern)),
        )
        for owner, name, replacement in replacements:
            test.enterContext(patch.object(owner, name, replacement))


class GuardCase(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())

    def reject(self, action):
        with self.assertRaises(UPGRADE.Failure):
            action()


class EffectFenceGuards(unittest.TestCase):
    def test_caught_denial_still_rejects_every_effect_family(self):
        actions = (
            ("file", lambda: Path("/synthetic-unowned/file").read_bytes()),
            ("process", lambda: subprocess.run(["synthetic-unowned-command"])),
            ("network", lambda: socket.socket()),
            ("http", lambda: http.client.HTTPConnection("synthetic.invalid")),
            ("lock", lambda: fcntl.flock(1234, fcntl.LOCK_EX)),
            ("clock", lambda: time.sleep(1)),
        )
        for name, action in actions:
            with self.subTest(effect=name):
                with self.assertRaises(AssertionError):
                    with EffectFence() as fence:
                        fence.expected_denial = True
                        try:
                            action()
                        except AssertionError:
                            pass
        self.assertEqual(ACTIVE_FENCES, [])

    def test_sibling_definitions_expose_the_actual_controller_contract(self):
        self.assertTrue(callable(UPGRADE.Controller))
        self.assertTrue(callable(UPGRADE.validate_intent))
        self.assertTrue(callable(UPGRADE.ServiceFence.approve))
        self.assertTrue(callable(UPGRADE.attest))


def intent_example():
    run_id = "20990101_010203_abcdef012345"
    closure = {name: synthetic_hash(name) for name in UPGRADE.TOOL_FILES}
    closure["upgrade-client-schema28.py"] = OPERATOR_SHA256
    closure["test-upgrade-client-schema28.py"] = GUARD_SHA256
    closure["accepted-web-report.json"] = UPGRADE.WEB_REPORT_SHA
    descriptor = lambda name: {"path": str(UPGRADE.TOOL / name), "sha256": closure[name]}
    return {"marker": UPGRADE.MARKER, "version": 1, "run_id": run_id, "tool": str(UPGRADE.TOOL),
        "output": str(UPGRADE.WORK / ("client-schema28-source55-upgrade-" + run_id)),
        "controller": {"unit": "goby-client-schema28-source55-" + run_id.replace("_", "-") + ".service"},
        "candidate": {"state": {"path": str(UPGRADE.STATE), "sha256": UPGRADE.STATE_SHA},
                      "baseline": {"path": str(UPGRADE.BASELINE), "sha256": UPGRADE.BASELINE_SHA},
                      "process": copy.deepcopy(UPGRADE.OLD_PROCESS), "invocation_id": UPGRADE.OLD_INVOCATION},
        "source": {"root": str(UPGRADE.SOURCE), "manifest_sha256": UPGRADE.SOURCE_SHA,
                   "full_report": {"path": str(UPGRADE.FULL_REPORT),
                                   "sha256": synthetic_hash("full-report")},
                   "terminal": {"path": str(UPGRADE.FULL_TERMINAL),
                                "sha256": synthetic_hash("full-terminal")},
                   "binary": {"path": str(UPGRADE.FULL_REPORT.parent / "tmp/goby-linux-amd64"),
                              "sha256": synthetic_hash("source55-binary"), "bytes": (1 << 20) + 1},
                   "publication": descriptor("publication.json")},
        "helper": {"source": descriptor("migrate-client-schema28.go"), "binary": descriptor("migrate-client-schema28"),
                   "build": descriptor("helper-build.json")},
        "web": {"source": str(UPGRADE.WEB_SOURCE), "report": descriptor("accepted-web-report.json")},
        "guards": descriptor("guards-report.json"), "source_closure": closure,
        "history_sha256": synthetic_hash("pair-history")}


def runtime_example():
    return (b"GOBY_DATABASE_URL='postgresql://goby_client_m3e:synthetic-password@127.0.0.1:15432/goby_client_m3e?sslmode=disable'\n"
            + MASTER_RUNTIME_LINE + b"GOBY_WEB_DIR='/opt/goby-dev/admin'\nSYNTHETIC_KEEP='unchanged'\n")


def snapshot_example(master_present=True):
    tables = {name: [{"id": name + "-" + str(index)} for index in range(count)] for name, count in UPGRADE.COUNTS.items()}
    tables["sessions"] = [{"id": "sessions-" + str(index), "kind": "emby", "user_id": "synthetic-viewer",
                            "revoked_at": "2098-12-31T00:00:00Z"} for index in range(75)]
    tables["play_sessions"] = [{"id": "play-" + str(index), "state": "Prepared" if index == 0 else "Stopped",
        "auth_session_id": "sessions-0", "user_id": "synthetic-viewer", "started_at": None, "counted": False,
        "application_client_id": None} for index in range(26)]
    tables["user_item_data"] = [{"id": "user-item-" + str(index)} for index in range(7)]
    tables["library_roots"] = [{"id": "root-" + str(index), "library_id": "library-" + str(index),
        "path": "/synthetic/media/root-" + str(index), "allowed_path": "/synthetic/media/root-" + str(index),
        "relative_path": "."} for index in range(4)]
    tables["schema_migrations"] = [{"version": index, "name": "synthetic-migration-%02d" % index} for index in range(1, 28)]
    tables["users"] = [{"id": "synthetic-viewer", "is_administrator": False, "is_disabled": False}]
    for name in ("scan_jobs", "task_runs", "task_run_children"):
        tables[name] = []
    tables["application_keys"] = []
    tables["application_key_devices"] = []
    for index in range(35 - len(tables)):
        tables["synthetic_table_%02d" % index] = []
    columns = {name: list(rows[0]) if rows else ["id"] for name, rows in tables.items()}
    return {"schema": 27, "runtime_sha256": UPGRADE.RUNTIME_SHA,
        "recovery": {"master.key": {"size": 32, "sha256": hashlib.sha256(MASTER_KEY_BYTES).hexdigest()}} if master_present else {},
        "browser_sha256": synthetic_hash("viewer-credentials"), "added_viewer_credentials": {"sha256": synthetic_hash("av-credentials")},
        "database": {"tables": tables, "sequences": {"sequence_%d" % index: {"last_value": 100 + index, "is_called": True} for index in range(5)},
            "catalog": ["synthetic-schema27-catalog"], "unsupported": False,
            "metadata": {"captured_at": "2099-01-01T01:00:00Z", "database": {"name": "goby_client_m3e", "oid": 100},
                "server_version_num": 170000, "schemas": ["public"], "public_schema": {"oid": 200, "owner": 101},
                "relations": {name: {"oid": 1000 + index} for index, name in enumerate(tables)}, "columns": columns}}}


def migration_example(before=None, runtime_sha256=None):
    before = snapshot_example() if before is None else before
    after = copy.deepcopy(before)
    catalog = {"version": 28, "objects": ["synthetic-schema28-catalog"],
               "migrations": [{"version": 28, "name": "0028_storage_root_bindings.sql"}]}
    after["schema"] = 28
    after["runtime_sha256"] = runtime_sha256 or UPGRADE.RUNTIME_SHA
    after["database"]["catalog"] = copy.deepcopy(catalog["objects"])
    after["database"]["metadata"]["captured_at"] = "2099-01-01T01:10:00Z"
    for name, columns in UPGRADE.NEW_COLUMNS.items():
        after["database"]["metadata"]["columns"][name].extend(columns)
    for row in after["database"]["tables"]["library_roots"]:
        row.update(binding_revision=1, storage_binding=None, bound_at=None, bound_by=None)
    for row in after["database"]["tables"]["activity_entries"]:
        row.update(previous_revision=0, observation_fingerprint="")
    after["database"]["tables"]["schema_migrations"].append(copy.deepcopy(catalog["migrations"][-1]))
    return before, after, catalog


class IntentGuards(GuardCase):
    def test_exact_current_source44_and_new_source55_scope(self):
        value = intent_example()
        self.assertEqual(UPGRADE.validate_intent(value), value)
        self.assertEqual(UPGRADE.OLD_PROCESS, {"pid": 1264063, "start_ticks": 11104222,
            "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"})
        self.assertEqual(UPGRADE.COUNTS["sessions"], 75)
        self.assertEqual(UPGRADE.COUNTS["devices"], 64)
        self.assertEqual(UPGRADE.COUNTS["activity_entries"], 167)
        self.assertEqual(str(UPGRADE.SOURCE), "/opt/goby-test/exec-work-m3e/source-attempt-55")
        self.assertEqual(str(UPGRADE.TOOL), "/opt/goby-test/exec-work-m3e/client-schema28-source55-tool-05")

    def test_foreign_or_stale_scope_is_rejected(self):
        changes = (
            ("version", lambda v: v.update(version=True)),
            ("old-output", lambda v: v.update(output=str(UPGRADE.WORK / "client-notifications-source44-continuation-v1"))),
            ("old-tool", lambda v: v.update(tool=str(UPGRADE.WORK / "client-notifications-source44-tool-01"))),
            ("used-schema28-tool", lambda v: v.update(tool=str(UPGRADE.WORK / "client-schema28-source55-tool-01"))),
            ("used-schema28-tool02", lambda v: v.update(tool=str(UPGRADE.WORK / "client-schema28-source55-tool-02"))),
            ("used-schema28-tool03", lambda v: v.update(tool=str(UPGRADE.WORK / "client-schema28-source55-tool-03"))),
            ("used-schema28-tool04", lambda v: v.update(tool=str(UPGRADE.WORK / "client-schema28-source55-tool-04"))),
            ("old-source", lambda v: v["source"].update(root=str(UPGRADE.WORK / "source-attempt-44"))),
            ("foreign-controller", lambda v: v["controller"].update(unit=UPGRADE.PRIMARY_UNIT)),
            ("candidate-pid", lambda v: v["candidate"]["process"].update(pid=748513)),
            ("candidate-ticks", lambda v: v["candidate"]["process"].update(start_ticks=11104223)),
            ("candidate-invocation", lambda v: v["candidate"].update(invocation_id="f" * 32)),
            ("state-hash", lambda v: v["candidate"]["state"].update(sha256=synthetic_hash("foreign-state"))),
            ("baseline-hash", lambda v: v["candidate"]["baseline"].update(sha256=synthetic_hash("old-baseline"))),
            ("binary-unchanged", lambda v: v["source"]["binary"].update(sha256=UPGRADE.OLD_BINARY_SHA)),
            ("binary-small", lambda v: v["source"]["binary"].update(bytes=1 << 20)),
            ("binary-boolean-size", lambda v: v["source"]["binary"].update(bytes=True)),
            ("web-primary-target", lambda v: v["web"].update(source="/opt/goby-dev/admin")),
            ("helper-path", lambda v: v["helper"]["binary"].update(path=str(UPGRADE.WORK / "other-helper"))),
            ("missing-closure", lambda v: v["source_closure"].pop("migrate-client-schema28.go")),
            ("extra-closure", lambda v: v["source_closure"].update({"unreviewed.py": synthetic_hash("unreviewed")})),
            ("closure-conflict", lambda v: v["source_closure"].update({"migrate-client-schema28": synthetic_hash("foreign-helper")})),
            ("extra-root-key", lambda v: v.update(retry=True)),
        )
        for name, change in changes:
            with self.subTest(change=name):
                value = intent_example()
                change(value)
                self.reject(lambda: UPGRADE.validate_intent(value))

    def test_duplicate_and_escaped_json_fields_are_rejected(self):
        for raw in (b'{"version":1,"version":2}', b'{"process":{"pid":1,"\\u0070id":2}}',
                    b'{"number":NaN}', b'{"number":Infinity}', b'{"items":[' + b'[' * 42 + b'0' + b']' * 42 + b']}'):
            with self.subTest(raw_hash=synthetic_hash(raw.hex())):
                self.reject(lambda: UPGRADE.decode(raw))

    def test_json_round_trip_retains_large_integer_identity(self):
        value = UPGRADE.decode(b'{"first":9007199254740992,"next":9007199254740993,"fraction":1.25}')
        self.assertNotEqual(value["first"], value["next"])
        self.assertIsInstance(value["fraction"], Decimal)
        self.assertEqual(UPGRADE.decode(UPGRADE.canonical(value)), value)


class PreservationGuards(GuardCase):
    def test_schema28_preserves_all_original_rows_and_neutral_defaults(self):
        before, after, catalog = migration_example()
        UPGRADE.validate_population(before)
        UPGRADE.compare_migrated(before, after, catalog, runtime_sha256=UPGRADE.RUNTIME_SHA)

    def test_only_snapshot_capture_time_may_change_before_stop(self):
        before = snapshot_example()
        after = copy.deepcopy(before)
        after["database"]["metadata"]["captured_at"] = "2099-01-01T02:00:00Z"
        UPGRADE.compare_baseline(before, after)
        for section, key in (("tables", "sessions"), ("sequences", "sequence_0"), ("metadata", "public_schema")):
            with self.subTest(section=section):
                changed = copy.deepcopy(after)
                changed["database"][section][key] = [] if section == "tables" else {"changed": True}
                self.reject(lambda: UPGRADE.compare_baseline(before, changed))

    def test_latest_population_and_quiescence_are_exact(self):
        for name in ("sessions", "devices", "activity_entries", "library_roots"):
            with self.subTest(table=name):
                value = snapshot_example()
                value["database"]["tables"][name].pop()
                self.reject(lambda: UPGRADE.validate_population(value))
        for state in ("queued", "running", "Queued", "Running"):
            with self.subTest(state=state):
                value = snapshot_example()
                value["database"]["tables"]["scan_jobs"] = [{"id": "scan", "status": state}]
                self.reject(lambda: UPGRADE.validate_population(value))

    def test_prepared_playback_requires_a_revoked_matching_ordinary_owner(self):
        value = snapshot_example()
        value["database"]["tables"]["sessions"][0]["revoked_at"] = None
        self.reject(lambda: UPGRADE.validate_population(value))
        value = snapshot_example()
        value["database"]["tables"]["play_sessions"][0]["state"] = "Playing"
        self.reject(lambda: UPGRADE.validate_population(value))

    def test_unrelated_rows_sequences_acl_and_private_data_cannot_change(self):
        changes = (
            ("row", lambda v: v["database"]["tables"]["sessions"][0].update(id="foreign")),
            ("sequence", lambda v: v["database"]["sequences"]["sequence_0"].update(last_value=999)),
            ("namespace", lambda v: v["database"]["metadata"]["public_schema"].update(owner=999)),
            ("catalog", lambda v: v["database"].update(catalog=["foreign-catalog"])),
            ("column-order", lambda v: v["database"]["metadata"]["columns"]["library_roots"].reverse()),
            ("root-binding", lambda v: v["database"]["tables"]["library_roots"][0].update(storage_binding={"approved": True})),
            ("root-revision", lambda v: v["database"]["tables"]["library_roots"][0].update(binding_revision=2)),
            ("root-bound-by", lambda v: v["database"]["tables"]["library_roots"][0].update(bound_by="admin")),
            ("audit-revision", lambda v: v["database"]["tables"]["activity_entries"][0].update(previous_revision=1)),
            ("audit-observation", lambda v: v["database"]["tables"]["activity_entries"][0].update(observation_fingerprint="foreign")),
            ("credential", lambda v: v.update(browser_sha256=synthetic_hash("foreign-credential"))),
            ("recovery", lambda v: v["recovery"]["master.key"].update(sha256=synthetic_hash("foreign-key"))),
            ("runtime", lambda v: v.update(runtime_sha256=synthetic_hash("foreign-runtime"))),
        )
        for name, change in changes:
            with self.subTest(change=name):
                before, after, catalog = migration_example()
                change(after)
                self.reject(lambda: UPGRADE.compare_migrated(before, after, catalog, runtime_sha256=UPGRADE.RUNTIME_SHA))

    def test_runtime_changes_only_one_candidate_web_directory_line(self):
        raw = b"KEEP_BEFORE='literal'\nGOBY_WEB_DIR='/opt/goby-dev/admin'\nKEEP_AFTER='literal'\n"
        expected = raw.replace(b"GOBY_WEB_DIR='/opt/goby-dev/admin'", b"GOBY_WEB_DIR='/opt/goby-client-m3e/admin'")
        self.assertEqual(UPGRADE.patch_runtime(raw), expected)
        for changed in (raw + b"GOBY_WEB_DIR='/other'\n", raw * 2, raw.replace(b"\nGOBY_WEB_DIR", b"\n#GOBY_WEB_DIR"),
                        expected, raw.replace(b"admin'\n", b"admin'\r\n")):
            self.reject(lambda: UPGRADE.patch_runtime(changed))


class ServiceFenceGuards(GuardCase):
    def test_reserves_only_one_exact_candidate_stop_then_start(self):
        fence = UPGRADE.ServiceFence()
        fence.stage = "stop_requested"
        fence.approve(["/usr/bin/systemctl", "stop", UPGRADE.UNIT])
        fence.stage = "start_requested"
        fence.approve(["/usr/bin/systemctl", "start", UPGRADE.UNIT])
        self.assertEqual(fence.reserved, {"stop", "start"})
        self.reject(lambda: fence.approve(["/usr/bin/systemctl", "start", UPGRADE.UNIT]))
        fence.stage = "stop_requested"
        self.reject(lambda: fence.approve(["/usr/bin/systemctl", "stop", UPGRADE.UNIT]))

    def test_unowned_units_restarts_and_noncanonical_commands_are_rejected(self):
        for command in (["/usr/bin/systemctl", "stop", UPGRADE.PRIMARY_UNIT],
                        ["/usr/bin/systemctl", "restart", UPGRADE.UNIT], ["systemctl", "stop", UPGRADE.UNIT],
                        ["/usr/bin/systemctl", "stop", UPGRADE.UNIT, "--no-block"]):
            with self.subTest(command=command):
                fence = UPGRADE.ServiceFence()
                fence.stage = "stop_requested"
                self.reject(lambda: fence.approve(command))
                self.assertEqual(fence.reserved, set())


    def test_start_without_stop_or_before_durable_phase_is_rejected(self):
        for stage in ("preflight", "rehearsed", "stop_requested", "stopped", "start_requested"):
            with self.subTest(stage=stage):
                fence = UPGRADE.ServiceFence()
                fence.stage = stage
                self.reject(lambda: fence.approve(["/usr/bin/systemctl", "start", UPGRADE.UNIT]))
                self.assertEqual(fence.reserved, set())


# Synthetic verification input evidence guards.
def verify_inputs_fixture(changes=None):
    """Sign synthetic evidence in dependency order without reading published inputs."""
    changes, files, documents = changes or {}, {}, {}

    def changed(name, value):
        documents[name] = value
        if name in changes:
            changes[name](value)
        return value

    def put(path, value, mode=0o600):
        raw = encoded(value)
        files[str(path)] = (raw, mode)
        return {"path": str(path), "sha256": UPGRADE.sha(raw)}

    intent = copy.deepcopy(intent_example())
    source = intent["source"]
    catalog28 = changed("catalog28", {"version": 28, "objects": ["synthetic-schema28"],
        "migrations": [{"version": 28, "name": "0028_storage_root_bindings.sql"}]})
    contents = {"go.mod": b"module github.com/moooyo/goby\n", "go.sum": b"Synthetic checksums.\n",
        UPGRADE.SELECTED[0]: b"# Synthetic workspace helper definitions.\n",
        UPGRADE.SELECTED[1]: b"package backuppg\n",
        UPGRADE.SELECTED[2]: encoded({"version": 27, "objects": ["synthetic-schema27"]}),
        UPGRADE.SELECTED[3]: encoded(catalog28)}
    contents.update({"internal/synthetic/input_%03d.go" % index:
        ("package synthetic\n// Input %d.\n" % index).encode() for index in range(101)})
    manifest_files = {name: put(UPGRADE.SOURCE / name, raw)["sha256"] for name, raw in contents.items()}
    catalog27_sha, catalog28_sha = (manifest_files[name] for name in UPGRADE.SELECTED[2:])
    for index in range(4243 - len(manifest_files)):
        name = "unread-synthetic/input-%04d.txt" % index
        manifest_files[name] = synthetic_hash(name)
    manifest = changed("manifest", {"marker": "goby-client-backup-source-m3e-v1", "files": manifest_files})
    manifest_sha = put(UPGRADE.SOURCE / "backup-source-inputs.json", manifest)["sha256"]
    source["manifest_sha256"] = manifest_sha
    put(UPGRADE.TOOL / "upgrade-client-schema28.py", OPERATOR_RAW)
    put(UPGRADE.TOOL / "test-upgrade-client-schema28.py", GUARD_RAW)
    intent["helper"]["source"] = put(UPGRADE.TOOL / "migrate-client-schema28.go", b"package main\nfunc main() {}\n")
    helper_test = put(UPGRADE.TOOL / "migrate-client-schema28_test.go", b"package main\n// Synthetic source-closure fixture only.\n")
    intent["helper"]["binary"] = put(UPGRADE.TOOL / "migrate-client-schema28", b"Synthetic migration helper.\n", 0o755)
    binary = b"S" * ((1 << 20) + 1)
    source["binary"] = put(UPGRADE.FULL_REPORT.parent / "tmp/goby-linux-amd64", binary, 0o755)
    source["binary"]["bytes"] = len(binary)
    changed("binary", source["binary"])
    required_tests = ["TestHTTPLibraryPermissionsCSRFAndLivePolicyRevocation",
        "TestHTTPLibraryScanBrowseFieldsAndDeletePreservesMedia",
        "TestPostgreSQLRootBindingArchiveMigratesSchema27WithoutInferringApproval"]
    report = changed("report", {"marker": "goby-client-backup-pair-m3e-v1", "status": "passed", "mode": "full",
        "schema": 28, "source": str(UPGRADE.SOURCE), "source_manifest_sha256": manifest_sha,
        "run_id": UPGRADE.FULL_RUN, "catalog_sha256": catalog28_sha, "unit_exit": 0,
        "cleanup": {name: True for name in ("unit_terminal", "hba_restored_exactly", "goby_backup_m3e_source_removed",
            "goby_backup_m3e_target_removed", "preexisting_catalog_unchanged", "receipt_saved")},
        "tests": {"top_level_passes": 2173, "failures": 0, "skips": 0, "passed": required_tests +
            ["TestSyntheticSource55Pass%04d" % index for index in range(2170)]},
        "packages": ["github.com/moooyo/goby/synthetic/package%02d" % index for index in range(25)],
        "binary": copy.deepcopy(source["binary"])})
    source["full_report"] = put(UPGRADE.FULL_REPORT, report)
    finished = put(UPGRADE.FULL_TERMINAL.with_name("finished-receipt.json"), {"marker": "synthetic-finished", "finished": True})
    terminal = changed("terminal", {"marker": "goby-collection-folder-source55-full-terminal-v1", "status": "passed",
        "run_id": UPGRADE.FULL_RUN, "unit": UPGRADE.FULL_UNIT, "source": str(UPGRADE.SOURCE),
        "source_manifest_sha256": manifest_sha, "report": str(UPGRADE.FULL_REPORT),
        "report_sha256": source["full_report"]["sha256"], "binary": copy.deepcopy(source["binary"]),
        "recursive_cgroup_empty": True, "state": {"MainPID": "0", "Result": "success", "ExecMainStatus": "0",
            "SubState": "exited", "InvocationID": UPGRADE.FULL_INVOCATION},
        "tests": {key: report["tests"][key] for key in ("failures", "skips", "top_level_passes")},
        "cleanup": copy.deepcopy(report["cleanup"]), "packages": len(report["packages"]),
        "finished_receipt_sha256": finished["sha256"]})
    source["terminal"] = put(UPGRADE.FULL_TERMINAL, terminal)
    publication = changed("publication", {"marker": "goby-source55-publication-v1", "commit": "a" * 40,
        "source_manifest_sha256": manifest_sha, "binary_sha256": source["binary"]["sha256"],
        "full_report_sha256": source["full_report"]["sha256"], "full_terminal_sha256": source["terminal"]["sha256"]})
    source["publication"] = put(UPGRADE.TOOL / "publication.json", publication)
    web_bytes = changed("web_bytes", {"index.html": b'<!doctype html><html><head><link rel="stylesheet" href="/admin/assets/app.css">'
        b'</head><body><script type="module" src="/admin/assets/app.js"></script></body></html>',
        "assets/app.js": b"export const synthetic = true;\n", "assets/app.css": b"body { color: black; }\n"})
    web_files = {name: put(UPGRADE.WEB_SOURCE / name, raw)["sha256"] for name, raw in web_bytes.items()}
    web_report = changed("web_report", {"status": "passed", "tests": {"expected": 68, "unexpected": 0, "flaky": 0, "skipped": 0},
        "dist_files": web_files})
    intent["web"]["report"] = put(UPGRADE.TOOL / "accepted-web-report.json", web_report)
    product_files = {name: digest for name, digest in manifest["files"].items()
        if name in ("go.mod", "go.sum") or name.startswith(("cmd/", "internal/"))}
    build_root = UPGRADE.WORK / ("client-schema28-source55-build-" + intent["run_id"])
    helper_source_sha = intent["helper"]["source"]["sha256"]
    build_files = {**product_files, "cmd/client-schema28-migration/main.go": helper_source_sha,
                   "cmd/client-schema28-migration/main_test.go": helper_test["sha256"]}
    go = {"path": "/opt/goby-toolchains/go1.27.1/bin/go",
        "sha256": "30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624"}
    build = changed("build", {"marker": "goby-client-schema28-helper-build-v1", "source": str(UPGRADE.SOURCE),
        "source_manifest_sha256": manifest_sha, "build_root": str(build_root), "files": copy.deepcopy(build_files),
        "helper_source_sha256": helper_source_sha, "binary_sha256": intent["helper"]["binary"]["sha256"], "go": go,
        "argv": [go["path"], "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o",
            str(UPGRADE.TOOL / "migrate-client-schema28"), "./cmd/client-schema28-migration/main.go"],
        "environment": {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0", "GOWORK": "off",
            "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off"}, "exit_code": 0})
    intent["helper"]["build"] = put(UPGRADE.TOOL / "helper-build.json", build)
    guards = changed("guards", {"suite": "client-schema28-upgrade-guards", "status": "passed",
        "operator_sha256": OPERATOR_SHA256, "guard_sha256": GUARD_SHA256, "test_count": 20,
        "actual_controller_flow": True, "failures": 0, "errors": 0, "skips": 0, "unexpected_effects": 0})
    intent["guards"] = put(UPGRADE.TOOL / "guards-report.json", guards)
    intent["history_sha256"] = put(UPGRADE.HISTORY, {"marker": "synthetic-preserved-history"})["sha256"]
    intent["source_closure"] = {name: UPGRADE.sha(files[str(UPGRADE.TOOL / name)][0]) for name in UPGRADE.TOOL_FILES}
    put(UPGRADE.TOOL / "intent.json", intent)
    return {"intent": intent, "files": files, "documents": documents,
        "trees": {str(UPGRADE.WEB_SOURCE): copy.deepcopy(web_report["dist_files"]), str(build_root): copy.deepcopy(build_files)},
        "patches": {"__file__": str(UPGRADE.TOOL / "upgrade-client-schema28.py"), "SOURCE_SHA": manifest_sha,
            "CATALOG27_SHA": catalog27_sha, "CATALOG28_SHA": catalog28_sha, "WEB_REPORT_SHA": intent["web"]["report"]["sha256"]}}


class VerifyInputsGuards(GuardCase):
    def check_inputs(self, changes=None, *, tool_case=None, damaged_file=None):
        fixture, reads, trees_seen = verify_inputs_fixture(changes), [], []
        members = sorted(UPGRADE.TOOL / name for name in UPGRADE.TOOL_FILES | {"intent.json"})
        metadata = {"st_mode": stat.S_IFREG | 0o600, "st_uid": 0, "st_gid": 0, "st_nlink": 1}
        if tool_case == "missing":
            members.pop()
        elif tool_case == "extra":
            members.append(UPGRADE.TOOL / "unreviewed.py")
        elif tool_case is not None:
            field, value = {"symlink": ("st_mode", stat.S_IFLNK | 0o600), "owner": ("st_uid", 1000),
                "group": ("st_gid", 1000), "hardlink": ("st_nlink", 2)}[tool_case]
            metadata[field] = value
        if damaged_file is not None:
            key = str(UPGRADE.TOOL / damaged_file)
            raw, mode = fixture["files"][key]
            fixture["files"][key] = (raw + b"Synthetic drift.\n", mode)

        def protected(path, expected=None, **kwargs):
            key = str(path)
            reads.append(key)
            if key == "/opt/goby-toolchains/go1.27.1/bin/go":
                # The transport models a toolchain already authenticated at its fixed digest.
                self.assertEqual(expected, "30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624")
                self.assertEqual(kwargs.get("modes"), (0o755,))
                return b"Synthetic authenticated Go toolchain.\n"
            self.assertIn(key, fixture["files"], "An undeclared input was read.")
            raw, mode = fixture["files"][key]
            UPGRADE.require(expected is None or UPGRADE.sha(raw) == expected, "Synthetic protected input digest differs.")
            UPGRADE.require(mode in kwargs.get("modes", (0o600, 0o644)) and len(raw) <= kwargs.get("limit", 128 << 20),
                "Synthetic protected input mode or size differs.")
            return raw

        def directory(path, mode=0o700):
            self.assertIn(Path(path), (UPGRADE.WORK, UPGRADE.TOOL))
            self.assertEqual(mode, 0o700)
            return {"device": 7, "inode": 100, "uid": 0, "gid": 0, "mode": mode}

        def verify_tree(root, expected, **kwargs):
            key = str(root)
            self.assertIn(key, fixture["trees"], "An undeclared tree was read.")
            self.assertEqual(kwargs, {"maximum": 64 << 20} if root == UPGRADE.WEB_SOURCE else {})
            UPGRADE.require(expected == fixture["trees"][key], "Synthetic tree inventory differs.")
            trees_seen.append(key)
            return {"root": key, "files": copy.deepcopy(expected), "root_identity": {"device": 7, "inode": 200, "mode": 0o700}}

        def iterdir(path):
            self.assertEqual(path, UPGRADE.TOOL)
            return iter(members)

        def lstat(path):
            self.assertIn(path, members)
            return types.SimpleNamespace(**metadata)

        original = copy.deepcopy(fixture["intent"])
        with contextlib.ExitStack() as stack:
            for name, value in fixture["patches"].items():
                stack.enter_context(patch.object(UPGRADE, name, value))
            for owner, name, replacement in ((UPGRADE, "protected", protected), (UPGRADE, "directory", directory),
                    (UPGRADE, "verify_tree", verify_tree), (Path, "iterdir", iterdir), (Path, "lstat", lstat)):
                stack.enter_context(patch.object(owner, name, replacement))
            result = UPGRADE.verify_inputs(fixture["intent"])
        self.assertEqual(fixture["intent"], original)
        return result, fixture, reads, trees_seen

    def reject_document(self, name, change):
        self.reject(lambda: self.check_inputs({name: change}))

    def reject_fields(self, name, cases):
        for path, value in cases:
            with self.subTest(document=name, path=path, value=value):
                def change(document):
                    for key in path[:-1]:
                        document = document[key]
                    document[path[-1]] = copy.deepcopy(value)
                self.reject_document(name, change)

    def test_accepts_complete_inputs_and_reads_only_selected_source_members(self):
        result, fixture, reads, trees_seen = self.check_inputs()
        for output, document in (("catalog", "catalog28"), ("full_terminal", "terminal"), ("publication", "publication"), ("build", "build")):
            self.assertEqual(result[output], fixture["documents"][document])
        self.assertEqual(len(result["source_manifest"]["files"]), 4243)
        self.assertEqual(set(result["selected"]), set(UPGRADE.SELECTED))
        self.assertEqual(set(trees_seen), set(fixture["trees"]))
        self.assertEqual(result["frontend_routes"], [("/admin/", "index.html"), ("/admin/assets/app.js", "assets/app.js"),
            ("/admin/assets/app.css", "assets/app.css")])
        selected = {name for name in fixture["documents"]["manifest"]["files"]
            if name in ("go.mod", "go.sum") or name.startswith(("cmd/", "internal/"))} | set(UPGRADE.SELECTED) | {"backup-source-inputs.json"}
        self.assertEqual({path for path in reads if Path(path).is_relative_to(UPGRADE.SOURCE)},
            {str(UPGRADE.SOURCE / name) for name in selected})
        self.assertEqual(result["full_terminal"]["tests"], {"failures": 0, "skips": 0, "top_level_passes": 2173})
        self.assertEqual(result["full_terminal"]["packages"], 25)
        self.assertEqual(len(fixture["documents"]["report"]["tests"]["passed"]), 2173)
        self.assertEqual(len(fixture["documents"]["report"]["packages"]), 25)

    def test_rejects_frozen_closure_drift_and_untrusted_tool_membership(self):
        for tool_case in ("missing", "extra", "symlink", "owner", "group", "hardlink"):
            with self.subTest(tool_case=tool_case):
                self.reject(lambda: self.check_inputs(tool_case=tool_case))
        for name in sorted(UPGRADE.TOOL_FILES):
            with self.subTest(damaged_file=name):
                self.reject(lambda: self.check_inputs(damaged_file=name))

    def test_rejects_manifest_selected_paths_incomplete_product_and_wrong_catalog(self):
        def escape(document):
            files = document["files"]
            name = next(name for name in files if name.startswith("internal/synthetic/"))
            files["internal/../synthetic-escape.go"] = files.pop(name)
        def incomplete(document):
            document["files"] = {("unread-synthetic/" + name if name.startswith("internal/synthetic/") else name): digest
                for name, digest in document["files"].items()}
        for index, change in enumerate((lambda value: value.update(marker="foreign-source"),
                lambda value: value["files"].pop(UPGRADE.SELECTED[0]),
                lambda value: value["files"].update({UPGRADE.SELECTED[0]: "invalid"}), escape, incomplete)):
            with self.subTest(change=index):
                self.reject_document("manifest", change)
        self.reject_document("catalog28", lambda value: value.update(version=27))

    def test_rejects_full_report_scope_and_success_type_changes(self):
        self.reject_fields("report", [((name,), value) for name, value in (("marker", "foreign-report"), ("status", "failed"),
            ("mode", "targeted"), ("schema", True), ("schema", 27), ("unit_exit", False), ("unit_exit", 1),
            ("source", str(UPGRADE.WORK / "source-attempt-44")), ("source_manifest_sha256", synthetic_hash("foreign-manifest")),
            ("catalog_sha256", synthetic_hash("foreign-catalog")), ("run_id", "20990101_010203_abcdef012345"))])

    def test_rejects_incomplete_cleanup_missing_tests_duplicates_and_packages(self):
        self.reject_fields("report", [(('cleanup', 'receipt_saved'), False), (('cleanup', 'unit_terminal'), 1),
            (('tests', 'top_level_passes'), True), (('tests', 'top_level_passes'), 2172), (('tests', 'failures'), 1),
            (('tests', 'skips'), False), (('tests', 'passed', 0), 'TestUnrelatedReplacement'),
            (('packages', 0), 'example.invalid/foreign/package')])
        for index, change in enumerate((lambda value: value["cleanup"].pop("hba_restored_exactly"),
                lambda value: value["cleanup"].update(extra=True),
                lambda value: value["tests"]["passed"].__setitem__(0, value["tests"]["passed"][1]),
                lambda value: value["packages"].pop(), lambda value: value["packages"].__setitem__(0, value["packages"][1]))):
            with self.subTest(change=index):
                self.reject_document("report", change)

    def test_rejects_binary_receipt_output_path_and_actual_length_disagreement(self):
        self.reject_fields("report", [(('binary', 'sha256'), synthetic_hash('foreign-binary'))])
        self.reject_document("binary", lambda value: value.update(bytes=value["bytes"] + 1))
        self.reject_document("binary", lambda value: value.update(path=str(UPGRADE.WORK / "unowned-binary")))

    def test_rejects_terminal_replay_liveness_and_evidence_disagreement(self):
        self.reject_fields("terminal", [(('marker',), 'foreign-terminal'), (('status',), 'failed'),
            (('run_id',), '20990101_010203_abcdef012345'), (('unit',), UPGRADE.PRIMARY_UNIT),
            (('source_manifest_sha256',), synthetic_hash('foreign-source')), (('report_sha256',), synthetic_hash('foreign-report')),
            (('recursive_cgroup_empty',), 1), (('state', 'MainPID'), '123'), (('state', 'Result'), 'exit-code'),
            (('state', 'ExecMainStatus'), '1'), (('state', 'SubState'), 'running'), (('state', 'InvocationID'), 'f' * 32),
            (('tests', 'top_level_passes'), 2174), (('cleanup', 'receipt_saved'), False), (('finished_receipt_sha256',), 'invalid')])
        self.reject_document("terminal", lambda value: value.update(packages=24))

    def test_rejects_terminal_summary_count_drift_types_and_extra_fields_through_verify_inputs(self):
        self.reject_fields("terminal", [(('tests', 'failures'), 1), (('tests', 'failures'), False),
            (('tests', 'skips'), 1), (('tests', 'skips'), False), (('tests', 'top_level_passes'), 2172),
            (('tests', 'top_level_passes'), True), (('tests', 'top_level_passes'), "2173"),
            (('packages',), 26), (('packages',), True), (('packages',), False), (('packages',), "25"),
            (('packages',), ["synthetic-package"] * 25)])
        for change in (lambda value: value["tests"].pop("failures"),
                       lambda value: value["tests"].update(extra=0),
                       lambda value: value["tests"].update(passed=["TestUnexpectedExpandedSummary"]),
                       lambda value: value["cleanup"].update(receipt_saved=1)):
            self.reject_document("terminal", change)

    def test_rejects_publication_that_does_not_bind_every_accepted_artifact(self):
        self.reject_fields("publication", [((field,), synthetic_hash('foreign-' + field)) for field in
            ('source_manifest_sha256', 'binary_sha256', 'full_report_sha256', 'full_terminal_sha256')] +
            [(('commit',), '0' * 40), (('commit',), 'A' * 40), (('marker',), 'foreign-publication')])
        self.reject_document("publication", lambda value: value.update(extra=True))
        self.reject_document("publication", lambda value: value.pop("full_terminal_sha256"))

    def test_rejects_helper_build_outside_exact_source_copy(self):
        self.reject_fields("build", [(('source',), str(UPGRADE.WORK / 'source-attempt-44')),
            (('source_manifest_sha256',), synthetic_hash('foreign-source')), (('build_root',), str(UPGRADE.SOURCE)),
            (('helper_source_sha256',), synthetic_hash('foreign-helper-source')), (('binary_sha256',), synthetic_hash('foreign-helper-binary')),
            (('exit_code',), False), (('exit_code',), 1)])
        self.reject_document("build", lambda value: value["files"].pop("go.mod"))
        self.reject_document("build", lambda value: value["files"].pop("cmd/client-schema28-migration/main_test.go"))
        self.reject_document("build", lambda value: value["files"].update({"unreviewed.go": synthetic_hash("extra")}))

    def test_rejects_helper_toolchain_command_and_offline_environment_drift(self):
        self.reject_fields("build", [(('go', 'path'), '/usr/bin/go'), (('go', 'sha256'), synthetic_hash('foreign-go')),
            (('argv', 6), str(UPGRADE.SOURCE / 'unowned-helper')), (('environment', 'GOTOOLCHAIN'), 'auto'),
            (('environment', 'GOPROXY'), 'https://example.invalid'), (('environment', 'GOFLAGS'), '-mod=mod')])
        self.reject_document("build", lambda value: value["argv"].remove("-mod=readonly"))

    def test_rejects_stale_incomplete_or_unproven_controller_guards(self):
        self.reject_fields("guards", [((name,), value) for name, value in (("suite", "foreign-guards"), ("status", "failed"),
            ("operator_sha256", synthetic_hash("foreign-controller")), ("guard_sha256", synthetic_hash("foreign-guard")),
            ("test_count", True), ("test_count", 19), ("actual_controller_flow", 1), ("failures", False),
            ("errors", 1), ("skips", 1), ("unexpected_effects", 1))])

    def test_rejects_unaccepted_frontend_or_unbound_entrypoint_routes(self):
        self.reject_fields("web_report", [(('status',), 'failed'), (('tests', 'expected'), 67), (('tests', 'unexpected'), False),
            (('tests', 'flaky'), 1), (('tests', 'skipped'), 1)])
        self.reject_document("web_report", lambda value: value["dist_files"].pop("index.html"))
        for index, raw in enumerate((b'<script type="module" src="/admin/assets/app.js"></script>',
                b'<script type="module" src="https://example.invalid/app.js"></script><link rel="stylesheet" href="/admin/assets/app.css">',
                b'<script type="module" src="/admin/assets/missing.js"></script><link rel="stylesheet" href="/admin/assets/app.css">')):
            with self.subTest(document='index.html', change=index):
                self.reject_document("web_bytes", lambda value: value.update({"index.html": raw}))


class FullTerminalSummaryGuards(GuardCase):
    def test_real_summary_helper_binds_summary_counts_to_the_complete_report(self):
        fixture = verify_inputs_fixture()
        terminal, report = fixture["documents"]["terminal"], fixture["documents"]["report"]
        before = copy.deepcopy((terminal, report))
        UPGRADE.validate_full_terminal_summary(terminal, report)
        self.assertEqual((terminal, report), before)
        self.assertEqual(terminal["tests"], {"failures": 0, "skips": 0, "top_level_passes": 2173})
        self.assertIs(type(terminal["packages"]), int)
        self.assertEqual(terminal["packages"], len(report["packages"]))
        self.assertIn("passed", report["tests"])
        self.assertNotIn("passed", terminal["tests"])

    def test_real_summary_helper_rejects_wrong_counts_and_boolean_masquerades(self):
        fixture = verify_inputs_fixture()
        original, report = fixture["documents"]["terminal"], fixture["documents"]["report"]
        for field in ("failures", "skips", "top_level_passes"):
            for value in (original["tests"][field] + 1, False, True, float(original["tests"][field])):
                with self.subTest(field=field, value=value):
                    terminal = copy.deepcopy(original)
                    terminal["tests"][field] = value
                    self.reject(lambda: UPGRADE.validate_full_terminal_summary(terminal, report))
        for field in ("failures", "skips"):
            terminal, changed_report = copy.deepcopy(original), copy.deepcopy(report)
            terminal["tests"][field] = changed_report["tests"][field] = False
            self.reject(lambda: UPGRADE.validate_full_terminal_summary(terminal, changed_report))

    def test_real_summary_helper_rejects_expanded_summaries_and_noninteger_package_counts(self):
        fixture = verify_inputs_fixture()
        original, report = fixture["documents"]["terminal"], fixture["documents"]["report"]
        for summary in (None, [], {"failures": 0, "skips": 0}, {**original["tests"], "extra": 0}, copy.deepcopy(report["tests"])):
            terminal = copy.deepcopy(original)
            terminal["tests"] = summary
            self.reject(lambda: UPGRADE.validate_full_terminal_summary(terminal, report))
        for count in (None, True, False, 24, 26, 25.0, "25", copy.deepcopy(report["packages"])):
            terminal = copy.deepcopy(original)
            terminal["packages"] = count
            self.reject(lambda: UPGRADE.validate_full_terminal_summary(terminal, report))


class FrontendRouteGuards(GuardCase):
    def test_only_manifest_owned_module_and_stylesheet_routes_are_selected(self):
        manifest = {"index.html": synthetic_hash("index"), "assets/main.js": synthetic_hash("js"), "assets/main.css": synthetic_hash("css")}
        index = b'<script type="module" src="/admin/assets/main.js"></script><link rel="stylesheet" href="/admin/assets/main.css">'
        self.assertEqual(UPGRADE.frontend_routes(index, manifest), [("/admin/", "index.html"),
            ("/admin/assets/main.js", "assets/main.js"), ("/admin/assets/main.css", "assets/main.css")])
        for changed in (index + b'<script type="module" src="/admin/assets/main.js"></script>',
                        index.replace(b"/admin/assets/main.js", b"https://example.invalid/main.js"),
                        index.replace(b"/admin/assets/main.js", b"/admin/assets/../main.js"),
                        index.replace(b"main.js", b"main.js?token=synthetic"),
                        index.replace(b'rel="stylesheet"', b'rel="stylesheet" rel="stylesheet"'),
                        index.replace(b"main.css", b"missing.css")):
            self.reject(lambda: UPGRADE.frontend_routes(changed, manifest))


class MasterBaselineGuards(GuardCase):
    def test_present_and_empty_absent_master_baselines_have_distinct_exact_proofs(self):
        expected = {"marker": "goby-client-schema28-master-state-v1", "path": str(MASTER_KEY_PATH),
            "application_keys": 0, "application_key_devices": 0, "application_key_sessions": 0}
        self.assertEqual(UPGRADE.MASTER, MASTER_KEY_PATH)
        self.assertEqual(UPGRADE.validate_master_baseline(snapshot_example(False), runtime_example()),
            {**expected, "state": "absent", "bytes": 0, "sha256": None})
        self.assertEqual(UPGRADE.validate_master_baseline(snapshot_example(), runtime_example()),
            {**expected, "state": "present", "bytes": 32, "sha256": hashlib.sha256(MASTER_KEY_BYTES).hexdigest()})

    def test_absent_master_rejects_each_nonempty_application_key_population(self):
        for name in ("application_keys", "application_key_devices", "sessions"):
            with self.subTest(population=name):
                snapshot = snapshot_example(False)
                if name == "sessions":
                    snapshot["database"]["tables"][name][1]["kind"] = "application_key"
                else:
                    snapshot["database"]["tables"][name].append({"id": "synthetic-existing-key"})
                with self.assertRaises(UPGRADE.Failure) as raised:
                    UPGRADE.validate_master_baseline(snapshot, runtime_example())
                self.assertEqual(raised.exception.code, "master_required_missing")

    def test_master_descriptor_and_population_shape_are_complete_and_strict(self):
        for entry in ({"size": 31, "sha256": "a" * 64}, {"size": True, "sha256": "a" * 64},
                      {"size": 32, "sha256": "invalid"}, {"size": 32, "sha256": "a" * 64, "extra": True}):
            snapshot = snapshot_example()
            snapshot["recovery"]["master.key"] = entry
            self.reject(lambda: UPGRADE.validate_master_baseline(snapshot, runtime_example()))
        for name in ("application_keys", "application_key_devices", "sessions"):
            snapshot = snapshot_example(False)
            del snapshot["database"]["tables"][name]
            self.reject(lambda: UPGRADE.validate_master_baseline(snapshot, runtime_example()))

    def test_master_runtime_has_one_exact_existing_candidate_path(self):
        original = runtime_example()
        for raw in (original.replace(MASTER_RUNTIME_LINE, b""), original + MASTER_RUNTIME_LINE,
                    original.replace(str(MASTER_KEY_PATH).encode(), b"/tmp/foreign-master.key"),
                    original.replace(MASTER_RUNTIME_LINE, MASTER_RUNTIME_LINE.replace(b"\n", b"\r\n"))):
            with self.assertRaises(UPGRADE.Failure) as raised:
                UPGRADE.validate_master_baseline(snapshot_example(False), raw)
            self.assertEqual(raised.exception.code, "master_runtime_path_invalid")


class SafeFailureGuards(GuardCase):
    def test_helper_failure_envelopes_export_only_fixed_codes(self):
        payload = {"marker": "goby-client-schema28-helper-v1", "status": "failed", "error": "backup_baseline_drift"}
        self.assertEqual(UPGRADE.helper_failure_code(encoded(payload)), "helper_backup_baseline_drift")
        payload["error"] = "role_properties_invalid"
        self.assertEqual(UPGRADE.helper_failure_code(encoded(payload)), "helper_role_properties_invalid")
        payload["error"] = "synthetic-private-error-message"
        payload["status"] = "retained"
        self.assertEqual(UPGRADE.helper_failure_code(encoded(payload)), "helper_retained")
        for raw in (encoded({**payload, "message": "synthetic-private-message"}), b"synthetic-private-stderr",
                    b"x" * 4097, b'{"marker":"goby-client-schema28-helper-v1","status":"failed","error":false}'):
            self.assertEqual(UPGRADE.helper_failure_code(raw), "helper_failed")

    def test_unknown_failure_codes_and_operation_stages_are_sanitized(self):
        controller = UPGRADE.Controller(types.SimpleNamespace())
        controller.operation_stage = "synthetic-private-stage"
        self.assertEqual(controller.safe_operation_stage(), "unknown")
        self.assertEqual(UPGRADE.safe_error_code(UPGRADE.Failure("synthetic-private-message", "synthetic-private-code")), "guard_rejected")
        self.assertEqual(UPGRADE.safe_error_code(RuntimeError("synthetic-private-message")), "external_failure")


class ControllerMemoryCase(GuardCase):
    """Keep controller policy and publication methods real; adapt only transport."""

    master_present = True

    def setUp(self):
        super().setUp()
        self.intent = intent_example()
        self.output = Path(self.intent["output"])
        staged = UPGRADE.INSTALL / ("goby.schema28-" + self.intent["run_id"])
        self.memory = MemoryFS(self.output, (UPGRADE.STATE, UPGRADE.BINARY, UPGRADE.RUNTIME), (staged,), (UPGRADE.WEB,))
        self.memory.bind(self)
        self.routes, self.events, self.actions, self.requests, self.connections = {}, [], [], [], []
        self.helper_changes, self.helper_failures, self.fail_command = {}, {}, None
        self.http_changes = {}
        self.after_rehearsal = self.after_stop = lambda: None
        self.pid, self.original_controller_pid = 1900001, 1900001
        self.invocation = "ab" * 16
        self.primary, self.media, self.host = {"identity": "synthetic-primary"}, {"groups": ["original", "extras", "music"]}, {"namespace": "synthetic-host"}
        self.global_catalog = {"roles": [], "databases": [], "memberships": []}
        self.live_snapshot = snapshot_example(master_present=self.master_present)
        self.before_snapshot = copy.deepcopy(self.live_snapshot)
        self.exported_sha = synthetic_hash("held-schema27-state")
        self.old_binary = b"Synthetic original source44 binary.\n"
        self.new_binary = b"N" * ((1 << 20) + 1)
        self.intent["source"]["binary"].update(sha256=hashlib.sha256(self.new_binary).hexdigest(), bytes=len(self.new_binary))
        self.runtime = runtime_example()
        self.new_process = {**UPGRADE.OLD_PROCESS, "pid": UPGRADE.OLD_PROCESS["pid"] + 1000,
                            "start_ticks": UPGRADE.OLD_PROCESS["start_ticks"] + 1000}
        self.service_running, self.service_process = True, copy.deepcopy(UPGRADE.OLD_PROCESS)
        self.service_invocation = UPGRADE.OLD_INVOCATION
        self.enterContext(patch.object(UPGRADE, "sha", self.digest))
        self.enterContext(patch.object(UPGRADE, "__file__", str(UPGRADE.TOOL / "upgrade-client-schema28.py")))
        self.enterContext(patch.object(sys, "platform", "linux"))
        self.enterContext(patch.object(os, "geteuid", lambda: 0))
        self.enterContext(patch.object(os, "getegid", lambda: 0))
        self.enterContext(patch.object(os, "getpid", lambda: self.pid))
        self.enterContext(patch.object(os, "environ", {"SSH_CONNECTION": "synthetic-authorized-connection", "INVOCATION_ID": self.invocation}))
        self.enterContext(patch.object(secrets, "token_hex", lambda size: "a" * (size * 2)))
        self.enterContext(patch.object(signal, "signal", lambda *_args: None))
        self.enterContext(patch.object(signal, "alarm", lambda seconds: self.events.append(("alarm", seconds))))
        self.enterContext(patch.object(time, "monotonic", lambda: 1000.0))
        self.enterContext(patch.object(fcntl, "flock", self.flock))
        self.postgres = types.SimpleNamespace(pw_uid=997, pw_gid=997)
        self.enterContext(patch.object(pwd, "getpwnam", lambda name: self.postgres if name == "postgres" else denied()))
        self.seed(UPGRADE.BINARY, self.old_binary, UPGRADE.OLD_BINARY_SHA, 0o755)
        self.memory.entries[str(UPGRADE.INSTALL)]["mode"] = stat.S_IFDIR | 0o755
        self.seed(UPGRADE.RUNTIME, self.runtime, UPGRADE.RUNTIME_SHA)
        self.state = {"marker": "goby-m3e-client-acceptance-v1", "schema": 27, "phase": "ready", "stage": "complete",
            "binary_sha256": UPGRADE.OLD_BINARY_SHA, "binary_identity": self.file_identity(self.memory.info(UPGRADE.BINARY)),
            "runtime_sha256": UPGRADE.RUNTIME_SHA, "process": copy.deepcopy(UPGRADE.OLD_PROCESS), "start_pending": False,
            "notifications_upgrade": {"phase": "complete"}, "schema27_source": {"source": str(UPGRADE.WORK / "source-attempt-44")},
            "server_id": "synthetic-server", "database_oid": 100, "role_oid": 101,
            "upgrade": {"id": "retained-source44"}, "upgrade_history": [{"id": "retained-source44"}],
            "preserved_history": {"synthetic": True}}
        self.seed(UPGRADE.STATE, UPGRADE.canonical(self.state), UPGRADE.STATE_SHA)
        self.seed(UPGRADE.BASELINE, UPGRADE.canonical(self.live_snapshot), UPGRADE.BASELINE_SHA)
        self.seed(UPGRADE.HISTORY, b"synthetic unchanged pair history\n", self.intent["history_sha256"])
        for name, digest in self.intent["source_closure"].items():
            self.seed(UPGRADE.TOOL / name, ("synthetic reviewed tool: " + name).encode(), digest,
                      0o755 if name == "migrate-client-schema28" else 0o600)
        raw = UPGRADE.canonical(self.intent)
        self.input_sha = self.digest(raw)
        self.seed(UPGRADE.TOOL / "intent.json", raw, self.input_sha)
        self.web_bytes = {"index.html": b'<html><script type="module" src="/admin/assets/app.js"></script><link rel="stylesheet" href="/admin/assets/app.css"></html>\n',
            "assets/app.js": b"'synthetic candidate asset';\n", "assets/app.css": b"body { color: black; }\n"}
        for name, raw in self.web_bytes.items():
            self.seed(UPGRADE.WEB_SOURCE / name, raw)
        shared = Path("/opt/goby-dev/admin")
        self.seed(shared / "index.html", b"<html>Synthetic preserved primary</html>\n", mode=0o644)
        self.memory.entries[str(shared)]["mode"] = stat.S_IFDIR | 0o755
        self.full_terminal = {"unit": UPGRADE.FULL_UNIT, "state": {"MainPID": "0", "InvocationID": UPGRADE.FULL_INVOCATION,
            "Result": "success", "ExecMainStatus": "0", "SubState": "exited", "ControlGroup": ""}}
        _, _, catalog = migration_example(self.live_snapshot)
        self.inputs = {"catalog": catalog, "selected": {name: ("synthetic " + name).encode() for name in UPGRADE.SELECTED},
            "web": {"files": {name: self.digest(raw) for name, raw in self.web_bytes.items()}}, "binary": self.new_binary,
            "publication": {"commit": "c" * 40}, "full_terminal": self.full_terminal}
        self.inputs["frontend_routes"] = UPGRADE.frontend_routes(self.web_bytes["index.html"], self.inputs["web"]["files"])
        self.product_packages = {"github.com/moooyo/goby/synthetic%02d" % index for index in range(24)}
        self.seed(UPGRADE.FULL_REPORT, UPGRADE.canonical({"packages": sorted(self.product_packages | {"github.com/moooyo/goby/internal/storagebinding"})}),
                  self.intent["source"]["full_report"]["sha256"])
        self.configure_modules()
        self.enterContext(patch.object(UPGRADE, "verify_inputs", self.verified_inputs))
        self.enterContext(patch.object(UPGRADE, "load_module", self.module))
        self.enterContext(patch.object(subprocess, "run", self.subprocess))
        self.enterContext(patch.object(http.client, "HTTPConnection", self.connection))
        real_prepare = UPGRADE.Controller.prepare
        def prepare(controller):
            self.current = controller
            return real_prepare(controller)
        self.enterContext(patch.object(UPGRADE.Controller, "prepare", prepare))
        self.enterContext(patch.object(UPGRADE.Controller, "rehearse", lambda controller: self.rehearse(controller)))
        self.controller = UPGRADE.Controller(types.SimpleNamespace(mode="run", input=UPGRADE.TOOL / "intent.json", input_sha256=self.input_sha))
        self.current = self.controller
        self.frozen = self.frozen_entries()

    def tearDown(self):
        self.assertEqual(self.memory.violations, [])
        self.assertEqual(self.memory.handles, {})
        self.assertTrue(all(connection.closed for connection in self.connections))

    @staticmethod
    def file_identity(info):
        return {"device": info.st_dev, "inode": info.st_ino}

    def digest(self, raw):
        actual = hashlib.sha256(raw).hexdigest()
        return self.routes.get(actual, actual)

    def seed(self, path, raw, expected=None, mode=0o600, uid=0, gid=0):
        self.memory.seed(path, raw, mode)
        entry = self.memory.entries[str(path)]
        entry.update(uid=uid, gid=gid)
        if expected is not None:
            actual = hashlib.sha256(entry["raw"]).hexdigest()
            self.assertTrue(actual not in self.routes or self.routes[actual] == expected)
            self.routes[actual] = expected

    def frozen_entries(self):
        prefixes = (UPGRADE.TOOL, UPGRADE.SOURCE, UPGRADE.WEB_SOURCE, Path("/opt/goby-dev/admin"))
        return {key: copy.deepcopy(value) for key, value in self.memory.entries.items()
                if any(Path(key) == prefix or Path(key).is_relative_to(prefix) for prefix in prefixes)}

    def flock(self, descriptor, operation):
        self.memory.handle(descriptor)
        self.assertEqual(operation, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.events.append(("lock", descriptor))

    def configure_modules(self):
        self.op = types.SimpleNamespace(UNIT=UPGRADE.UNIT, BINARY=UPGRADE.BINARY, PORT=18198, PG_PORT=15432,
            ROLE="goby_client_m3e", MARKER=self.state["marker"], LOCK=UPGRADE.WORK / "candidate.lock",
            BROWSER=UPGRADE.WORK / "browser.json", AV_BROWSER=UPGRADE.WORK / "goby-av-browser.json",
            AV_ROOT=UPGRADE.WORK / "synthetic-av", UNIT_FILE=Path("/etc/systemd/system/goby-client-m3e.service"),
            DATA=MASTER_KEY_PATH.parent, PRODUCT_PACKAGES=self.product_packages,
            host_inputs=Mock(), schema27_binding=lambda value: value["schema27_source"],
            verify_service=self.verify_service, require_candidate_stopped=self.require_stopped,
            verify_database=Mock(), verify_fixture_directories=Mock(), preservation_snapshot=self.snapshot,
            identity=self.file_identity, regular=lambda path, **_kwargs: self.memory.info(path), postgres=self.postgres_query)
        for path, raw, digest in ((self.op.LOCK, b"synthetic candidate lock", None),
            (self.op.BROWSER, b"synthetic isolated viewer", self.live_snapshot["browser_sha256"]),
            (self.op.AV_BROWSER, b"synthetic av browser", None), (self.op.AV_ROOT / "credentials.json", b"synthetic av credentials", None),
            (self.op.UNIT_FILE, b"synthetic candidate unit", None)):
            self.seed(path, raw, digest)
        self.memory.seed_directory(self.op.DATA)
        if self.master_present:
            self.seed(MASTER_KEY_PATH, MASTER_KEY_BYTES, self.live_snapshot["recovery"]["master.key"]["sha256"], uid=995, gid=986)
        owner_path, workspace_lock = UPGRADE.CONTROL / "owner.json", UPGRADE.CONTROL / "workspace.lock"
        self.owner = {"system_identifier": "synthetic-cluster", "process": {"pid": 9001, "start_ticks": 9002,
            "boot_id": UPGRADE.OLD_PROCESS["boot_id"]}}
        self.seed(owner_path, UPGRADE.canonical(self.owner))
        self.seed(workspace_lock, b"synthetic workspace lock")
        self.workspace = types.SimpleNamespace(OWNER=owner_path, HBA="synthetic baseline hba\n", verify_parents=Mock(),
            acquire_lock=lambda: self.memory.open(workspace_lock, os.O_RDONLY), validate_owner=Mock(), expected_owner=Mock(return_value={}),
            binaries=Mock(return_value={}), directory=Mock(return_value={}), verify_directories=Mock(), verify_configuration=Mock(),
            verify_process=lambda *_args: copy.deepcopy(self.owner["process"]), verify_server=Mock())
        for name in ("pg_hba.conf", "postgresql.conf", "postgresql.auto.conf", "pg_ident.conf"):
            raw = self.workspace.HBA.encode() if name == "pg_hba.conf" else ("synthetic " + name).encode()
            self.seed(UPGRADE.PGDATA / name, raw, uid=self.postgres.pw_uid, gid=self.postgres.pw_gid)
        self.database = types.SimpleNamespace(name="goby_client_s55_rehearsal_" + self.intent["run_id"].replace("_", ""),
            role="goby_client_s55_rehearsal_" + self.intent["run_id"].replace("_", ""), pair={"phase": "new"},
            rows=lambda: {"role": None, "database": None}, maintenance=lambda sql: UPGRADE.canonical(self.global_catalog)
                if sql == "SYNTHETIC_GLOBAL_SQL" else denied())
        self.dbtools = types.SimpleNamespace(Database=lambda *_args: self.database, GLOBAL_SQL="SYNTHETIC_GLOBAL_SQL",
            host_fact=lambda: copy.deepcopy(self.host), process_fact=lambda pid: {"pid": pid, "cgroup": "/system.slice/" + self.intent["controller"]["unit"]},
            cgroup_empty=self.cgroup_empty, replace_file=self.replace_file)
        self.modules = {"op": self.op, "base": types.SimpleNamespace(Upgrade=types.SimpleNamespace(primary_fact=lambda _controller: copy.deepcopy(self.primary))),
            "profile": types.SimpleNamespace(validate_structure=Mock()), "ext": types.SimpleNamespace(media_witness=lambda *_args: copy.deepcopy(self.media)),
            "dbtools": self.dbtools, "workspace": self.workspace, "attest_workspace": self.workspace}
        for unit in (UPGRADE.FULL_UNIT, self.intent["controller"]["unit"]):
            root = Path("/sys/fs/cgroup/system.slice") / unit
            self.seed(root / "cgroup.procs", b"" if unit == UPGRADE.FULL_UNIT else (str(self.pid) + "\n").encode())

    def module(self, path, digest, name):
        if name in UPGRADE.HELPERS:
            self.assertEqual((path, digest), UPGRADE.HELPERS[name])
        else:
            self.assertIn(name, ("workspace", "attest_workspace"))
            self.assertEqual(path, UPGRADE.SOURCE / UPGRADE.SELECTED[0])
            self.assertEqual(digest, self.digest(self.inputs["selected"][UPGRADE.SELECTED[0]]))
        return self.modules[name]

    def verified_inputs(self, intent):
        self.assertEqual(intent, self.intent)
        for name, digest in intent["source_closure"].items():
            UPGRADE.protected(UPGRADE.TOOL / name, digest, modes=(0o600, 0o644, 0o755))
        return copy.deepcopy(self.inputs)

    def verify_service(self, _state, allow_new=False):
        if self.service_running and self.digest(self.memory.raw(UPGRADE.BINARY)) != _state.get("binary_sha256"):
            return None
        return copy.deepcopy(self.service_process) if self.service_running else None

    def require_stopped(self):
        UPGRADE.require(not self.service_running, "Synthetic candidate still runs.")

    def snapshot(self, _state, schema):
        self.events.append(("snapshot", schema))
        self.assertEqual(self.live_snapshot["schema"], schema)
        return copy.deepcopy(self.live_snapshot)

    def postgres_query(self, sql, database=None):
        if "'database_oid'" in sql:
            self.assertEqual(database, "goby_client_m3e")
            return json.dumps({"database_oid": 100, "role_oid": 101, "schema_oid": 200,
                "schema_owner_oid": 101, "recovery_deployment_id": "d" * 32})
        self.assertIn("'postmaster_start_microseconds'", sql)
        return json.dumps({"postmaster_start_microseconds": 123456789, "postgresql_version_num": 170000})

    def cgroup_empty(self, unit):
        self.assertIn(unit, (UPGRADE.FULL_UNIT, self.intent["controller"]["unit"]))
        raw = self.memory.raw(Path("/sys/fs/cgroup/system.slice") / unit / "cgroup.procs")
        UPGRADE.require(not raw.strip(), "Synthetic controller cgroup remains occupied.")

    def replace_file(self, path, before, after, **_kwargs):
        self.assertEqual(path, UPGRADE.RUNTIME)
        self.assertEqual(self.memory.raw(path), before)
        staging = self.output / "private/runtime-replacement"
        UPGRADE.create(staging, after)
        self.memory.replace(staging, path)
        self.live_snapshot["runtime_sha256"] = self.digest(after)

    def service_properties(self, unit):
        if unit == UPGRADE.FULL_UNIT:
            return copy.deepcopy(self.full_terminal["state"])
        if unit == UPGRADE.UNIT:
            return {"MainPID": str(self.service_process["pid"]) if self.service_running else "0", "InvocationID": self.service_invocation,
                "ActiveState": "active" if self.service_running else "inactive", "SubState": "running" if self.service_running else "dead",
                "ControlGroup": "/system.slice/" + UPGRADE.UNIT if self.service_running else ""}
        self.assertEqual(unit, self.intent["controller"]["unit"])
        result = {"MainPID": str(self.original_controller_pid), "InvocationID": self.invocation, "ActiveState": "active", "SubState": "running",
            "Description": UPGRADE.MARKER + ":" + self.intent["run_id"], "ControlGroup": "/system.slice/" + unit,
            "MemoryMax": str(2 << 30), "MemorySwapMax": "0", "CPUQuotaPerSecUSec": "1.500000s", "TasksMax": "128",
            "RuntimeMaxUSec": "20min", "KillMode": "control-group", "RemainAfterExit": "yes", "Result": "success", "ExecMainStatus": "0"}
        result.update(getattr(self, "outer_terminal", {}))
        return result

    def subprocess(self, arguments, **keywords):
        self.assertGreater(keywords["timeout"], 0)
        args = list(arguments)
        self.events.append(("command", tuple(args)))
        output = b""
        if args[:2] == ["/usr/bin/systemctl", "show"]:
            self.assertEqual(len(args), 5)
            self.assertEqual(args[3], "--no-pager")
            names = args[4].removeprefix("--property=").split(",")
            properties = self.service_properties(args[2])
            output = "\n".join(name + "=" + properties[name] for name in names).encode()
        elif args[:2] in (["/usr/bin/systemctl", "stop"], ["/usr/bin/systemctl", "start"]):
            action = args[1]
            self.assertEqual(args, ["/usr/bin/systemctl", action, UPGRADE.UNIT])
            self.assertEqual(self.current.phase_name, action + "_requested")
            self.assertEqual(self.current.fence.stage, action + "_requested")
            durable = UPGRADE.decode(self.memory.raw(UPGRADE.STATE))
            self.assertEqual(durable["schema28_upgrade"]["phase"], action + "_requested")
            self.assertEqual(self.current.state_raw, self.memory.raw(UPGRADE.STATE))
            self.assertIn(action, self.current.fence.reserved)
            self.assertEqual(self.actions, [] if action == "stop" else ["stop"])
            self.actions.append(action)
            if self.fail_command == action:
                return types.SimpleNamespace(returncode=1, stdout=b"synthetic-private-stdout", stderr=b"synthetic-private-stderr")
            if action == "stop":
                self.assertTrue(self.service_running)
                self.service_running = False
                self.after_stop()
            else:
                self.assertFalse(self.service_running)
                self.assertEqual(self.memory.raw(UPGRADE.BINARY), self.new_binary)
                self.service_running, self.service_process, self.service_invocation = True, copy.deepcopy(self.new_process), "cd" * 16
        elif args == ["/usr/bin/ss", "-H", "-ltnp", "sport = :18198"]:
            output = b"synthetic owned listener" if self.service_running else b""
        elif args[0] == str(UPGRADE.TOOL / "migrate-client-schema28"):
            mode = args[args.index("--mode") + 1]
            if mode in self.helper_failures:
                stdout, stderr = self.helper_failures[mode]
                return types.SimpleNamespace(returncode=1, stdout=stdout, stderr=stderr)
            self.helper_command(args)
        else:
            self.memory.violation("An undeclared command reached the schema28 adapter.")
        return types.SimpleNamespace(returncode=0, stdout=output, stderr=b"")

    def helper_command(self, args):
        self.assertEqual(len(args) % 2, 1)
        options = dict(zip(args[1::2], args[2::2]))
        self.assertEqual(len(options) * 2 + 1, len(args))
        self.assertTrue(set(options) <= {"--mode", "--intent", "--intent-sha256", "--credentials", "--output", "--expected-schema",
                                         "--baseline", "--baseline-sha256", "--target-receipt", "--target-receipt-sha256"})
        mode, output = options["--mode"], Path(options["--output"])
        self.assertEqual(output.parent, self.output / "private")
        self.assertEqual(options["--intent"], str(self.output / "private/helper-intent.json"))
        self.assertEqual(options["--credentials"], str(self.output / "private/helper-credentials.json"))
        self.assertEqual(options["--intent-sha256"], self.digest(self.memory.raw(Path(options["--intent"]))))
        self.events.append(("helper", mode, output.name))
        if mode == "migrate":
            self.assertFalse(self.service_running)
            self.assertEqual(self.actions, ["stop"])
            self.assertEqual(self.current.phase_name, "migrate_requested")
            _, self.live_snapshot, _ = migration_example(self.before_snapshot)
        else:
            self.assertIn(mode, ("backup", "rehearse", "inspect"))
            if output.name == "helper-pre-stop.json":
                self.assertTrue(self.service_running)
            if output.name == "helper-stopped.json":
                self.assertFalse(self.service_running)
        value = {"marker": "goby-client-schema28-helper-v1", "version": 1, "mode": mode, "run_id": self.intent["run_id"],
            "intent_sha256": options["--intent-sha256"], "source_manifest_sha256": UPGRADE.SOURCE_SHA,
            "source55_binary_sha256": self.intent["source"]["binary"]["sha256"], "helper_sha256": self.intent["helper"]["binary"]["sha256"],
            "status": {"backup": "backed_up", "rehearse": "rehearsed", "migrate": "committed", "inspect": "inspected"}[mode],
            "source_schema_version": 27, "target_schema_version": 28, "state_sha256": self.exported_sha,
            "state": {"synthetic": "exported state"}}
        if mode == "backup":
            dump_path = self.output / "private/candidate-schema27.dump"
            raw = b"PGDMP-synthetic-held-snapshot"
            UPGRADE.create(dump_path, raw)
            value.update(same_exported_snapshot=True, dump={"path": str(dump_path), "bytes": len(raw), "sha256": self.digest(raw)})
        elif mode != "inspect":
            self.assertEqual(options["--baseline-sha256"], self.current.backup_sha)
            value.update(baseline_sha256=self.current.backup_sha, before_state_sha256=self.exported_sha,
                         preserved_state_sha256=self.exported_sha, root_bindings_no_auto_binding=True, historical_audit_defaults=True)
        change = self.helper_changes.get(output.name) or self.helper_changes.get(mode)
        if change:
            change(value)
        UPGRADE.create(output, UPGRADE.canonical(value))

    def rehearse(self, controller):
        self.assertEqual(controller.phase_name, "backed_up")
        self.assertTrue(self.service_running)
        self.events.append(("rehearse",))
        UPGRADE.create(controller.private / "helper-target.json", UPGRADE.canonical({"synthetic": "owned target"}))
        controller.helper("rehearse", baseline=True, target=True)
        self.database.pair = {"phase": "removed"}
        controller.save("rehearsal-completed.json", {"marker": UPGRADE.MARKER, "run_id": self.intent["run_id"],
            "backup_sha256": controller.backup_sha, "pair": self.database.pair, "hba_restored_exactly": True,
            "global_catalog_preserved": True, "normal_drop_only": True})
        controller.phase("rehearsed")
        self.after_rehearsal()

    def connection(self, host, port, timeout):
        self.assertEqual((host, port, timeout), ("127.0.0.1", 18198, 15))
        connection = types.SimpleNamespace(closed=False, route=None)
        def request(method, route, body=None, headers=None):
            routes = ["/readyz", "/emby/System/Info/Public", "/admin/", "/admin/assets/app.js", "/admin/assets/app.css"]
            self.assertEqual((method, body), ("GET", None))
            self.assertLess(len(self.requests), len(routes))
            self.assertEqual(route, routes[len(self.requests)])
            self.assertEqual(headers, {"Accept": "application/json" if len(self.requests) < 2 else "*/*",
                "Accept-Encoding": "identity", "Origin": "http://127.0.0.1:18196"})
            self.requests.append(route)
            connection.route = route
        def response():
            route = connection.route
            if route == "/readyz":
                raw = b'{"Status":"ready"}'
            elif route == "/emby/System/Info/Public":
                raw = UPGRADE.canonical({"Id": self.state["server_id"], "ProductName": "Goby", "Version": "4.9.5.0"})
            else:
                raw = self.web_bytes["index.html" if route == "/admin/" else route.removeprefix("/admin/")]
            changed = self.http_changes.get(route, {})
            raw = changed.get("body", raw)
            return types.SimpleNamespace(status=changed.get("status", 200), read=lambda limit: raw[:limit],
                getheader=lambda _name: changed.get("cookie"))
        connection.request, connection.getresponse = request, response
        connection.close = lambda: setattr(connection, "closed", True)
        self.connections.append(connection)
        return connection

    def output_json(self, name):
        return UPGRADE.decode(self.memory.raw(self.output / name))

    def prepare_attestation(self):
        self.pid += 1
        self.outer_terminal = {"MainPID": "0", "SubState": "exited", "ControlGroup": ""}
        group = Path("/sys/fs/cgroup/system.slice") / self.intent["controller"]["unit"] / "cgroup.procs"
        self.memory.entries[str(group)]["raw"] = b""
        return types.SimpleNamespace(mode="attest", input=UPGRADE.TOOL / "intent.json", input_sha256=self.input_sha)


class ControllerFlowGuards(ControllerMemoryCase):
    def test_real_prepare_and_execute_complete_only_pending_outer_attestation(self):
        result = self.controller.run()
        self.assertEqual(result["status"], "awaiting_outer_attestation")
        self.assertEqual(self.actions, ["stop", "start"])
        self.assertEqual(self.current.fence.reserved, {"stop", "start"})
        self.assertEqual(self.requests, ["/readyz", "/emby/System/Info/Public", "/admin/", "/admin/assets/app.js", "/admin/assets/app.css"])
        self.assertEqual([(event[1], event[2]) for event in self.events if event[0] == "helper"],
            [("backup", "helper-backup.json"), ("rehearse", "helper-rehearse.json"), ("inspect", "helper-pre-stop.json"),
             ("inspect", "helper-stopped.json"), ("migrate", "helper-migrate.json")])
        self.assertEqual(self.memory.raw(UPGRADE.BINARY), self.new_binary)
        self.assertEqual(self.memory.raw(UPGRADE.RUNTIME), UPGRADE.patch_runtime(self.runtime))
        self.assertEqual(self.frozen_entries(), self.frozen)
        self.assertEqual(self.output_json("report.json")["status"], "awaiting_outer_attestation")
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)
        self.assertEqual(self.memory.raw(self.output / "private/materials/application/master.key"), MASTER_KEY_BYTES)
        self.assertEqual(self.output_json("master-proof.json"), UPGRADE.validate_master_baseline(self.before_snapshot, self.runtime))
        for path in [UPGRADE.WEB, *list(UPGRADE.WEB.rglob("*"))]:
            info = self.memory.info(path)
            self.assertEqual(stat.S_IMODE(info.st_mode), 0o755 if stat.S_ISDIR(info.st_mode) else 0o644)

    def test_real_preflight_has_no_output_service_helper_or_http_mutation(self):
        self.controller.args.mode = "preflight"
        before = copy.deepcopy(self.memory.entries)
        result = self.controller.run()
        self.assertEqual(result["status"], "preflight_passed")
        self.assertEqual(self.memory.entries, before)
        self.assertEqual(self.actions, [])
        self.assertEqual(self.requests, [])
        self.assertFalse(any(event[0] == "helper" for event in self.events))

    def test_drift_after_rehearsal_rejects_before_stop(self):
        self.after_rehearsal = lambda: self.live_snapshot["database"]["tables"]["sessions"][0].update(id="drift")
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])
        self.assertFalse(any(event[:2] == ("helper", "migrate") for event in self.events))
        self.assertEqual(self.output_json("failed.json")["phase"], "rehearsed")

    def test_stopped_drift_never_migrates_restarts_or_rolls_back(self):
        self.after_stop = lambda: self.live_snapshot["database"]["sequences"]["sequence_0"].update(last_value=999)
        self.reject(self.controller.run)
        self.assertEqual(self.actions, ["stop"])
        self.assertFalse(self.service_running)
        self.assertEqual(self.memory.raw(UPGRADE.BINARY), self.old_binary)
        self.assertFalse(any(event[:2] == ("helper", "migrate") for event in self.events))
        report = self.output_json("failed.json")
        self.assertFalse(report["automatic_retry"])
        self.assertFalse(report["automatic_rollback"])

    def test_exported_backup_drift_rejects_before_stop(self):
        self.helper_changes["helper-pre-stop.json"] = lambda value: value.update(state_sha256=synthetic_hash("changed exported state"))
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])

    def test_stopped_exported_backup_drift_never_migrates(self):
        self.helper_changes["helper-stopped.json"] = lambda value: value.update(state_sha256=synthetic_hash("changed exported state"))
        self.reject(self.controller.run)
        self.assertEqual(self.actions, ["stop"])
        self.assertFalse(any(event[:2] == ("helper", "migrate") for event in self.events))

    def test_primary_drift_after_rehearsal_rejects_before_stop(self):
        self.after_rehearsal = lambda: self.primary.update(identity="foreign-primary")
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])

    def test_frozen_input_inode_drift_after_rehearsal_rejects_before_stop(self):
        def change():
            self.memory.entries[str(UPGRADE.STATE)]["inode"] += 1
        self.after_rehearsal = change
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])

    def test_state_publication_failure_does_not_consume_stop(self):
        self.memory.fail_create = str(self.output / "state-stop-requested.json")
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])
        self.assertEqual(self.controller.fence.reserved, set())

    def test_start_publication_failure_keeps_committed_candidate_stopped(self):
        self.memory.fail_create = str(self.output / "phase-start-requested.json")
        self.reject(self.controller.run)
        self.assertEqual(self.actions, ["stop"])
        self.assertEqual(self.live_snapshot["schema"], 28)
        self.assertEqual(self.memory.raw(UPGRADE.BINARY), self.new_binary)
        self.assertFalse(self.service_running)

    def test_failed_stop_dispatch_is_not_retried(self):
        self.fail_command = "stop"
        self.reject(self.controller.run)
        self.assertEqual(self.actions, ["stop"])
        self.assertEqual(sum(event[0] == "command" and event[1][:2] == ("/usr/bin/systemctl", "stop") for event in self.events), 1)
        self.assertNotIn(str(self.output / "report.json"), self.memory.entries)

    def test_wrong_javascript_bytes_rejects_native_readiness(self):
        self.http_changes["/admin/assets/app.js"] = {"body": b"foreign javascript"}
        self.reject(self.controller.run)
        self.assertEqual(len(self.requests), 4)
        self.assertNotIn(str(self.output / "report.json"), self.memory.entries)

    def test_wrong_stylesheet_bytes_rejects_native_readiness(self):
        self.http_changes["/admin/assets/app.css"] = {"body": b"foreign stylesheet"}
        self.reject(self.controller.run)
        self.assertEqual(len(self.requests), 5)
        self.assertNotIn(str(self.output / "report.json"), self.memory.entries)

    def test_redirect_or_cookie_cannot_count_as_asset_readiness(self):
        self.http_changes["/admin/"] = {"status": 302, "cookie": "synthetic-cookie"}
        self.reject(self.controller.run)
        self.assertEqual(len(self.requests), 3)

    def test_reused_output_is_rejected_by_real_prepare_without_mutation(self):
        self.memory.seed_directory(self.output)
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])
        self.assertFalse(any(event[0] == "helper" for event in self.events))

    def test_old_population_is_rejected_by_real_prepare(self):
        self.live_snapshot["database"]["tables"]["sessions"] = self.live_snapshot["database"]["tables"]["sessions"][:73]
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])
        self.assertNotIn(str(self.output), self.memory.entries)

    def test_current_candidate_pid_drift_is_rejected_by_real_prepare(self):
        self.service_process["pid"] += 1
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])

    def test_unshared_backup_snapshot_never_reaches_rehearsal_or_stop(self):
        self.helper_changes["backup"] = lambda value: value.update(same_exported_snapshot=False)
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])
        self.assertFalse(any(event[0] == "rehearse" for event in self.events))

    def test_rehearsal_auto_binding_proof_is_rejected_before_stop(self):
        self.helper_changes["rehearse"] = lambda value: value.update(root_bindings_no_auto_binding=False)
        self.reject(self.controller.run)
        self.assertEqual(self.actions, [])
        self.assertFalse(any(event[:2] == ("helper", "migrate") for event in self.events))

    def test_failed_migration_acknowledgement_does_not_restart_or_undo_commit(self):
        self.helper_changes["migrate"] = lambda value: value.update(historical_audit_defaults=False)
        self.reject(self.controller.run)
        self.assertEqual(self.actions, ["stop"])
        self.assertEqual(self.live_snapshot["schema"], 28)
        self.assertEqual(self.memory.raw(UPGRADE.BINARY), self.old_binary)
        self.assertFalse(self.service_running)

    def test_hba_restore_is_reserved_before_write_and_never_retried_by_run(self):
        attempts = []
        def failing_replace(path, before, after, **kwargs):
            self.assertEqual(path, UPGRADE.HBA)
            self.assertTrue(self.controller.hba_restore_reserved)
            attempts.append((path, before, after))
            raise UPGRADE.Failure("Synthetic HBA restoration transport failure.")
        self.enterContext(patch.object(self.dbtools, "replace_file", failing_replace))
        def restore():
            self.controller.hba_active = True
            self.controller.hba_after = b"X" + self.controller.hba_before[1:]
            self.memory.entries[str(UPGRADE.HBA)]["raw"] = self.controller.hba_after
            self.controller.restore_hba()
        self.after_rehearsal = restore
        self.reject(self.controller.run)
        self.assertEqual(len(attempts), 1)
        self.assertTrue(self.controller.hba_restore_reserved)
        self.assertTrue(self.controller.hba_active)
        self.assertEqual(self.actions, [])
        self.assertFalse(self.output_json("failed.json")["hba_restored_exactly"])

    def test_existing_master_disappearance_is_not_reinterpreted_as_empty_absence(self):
        self.after_rehearsal = lambda: self.memory.entries.pop(str(MASTER_KEY_PATH))
        with self.assertRaises(UPGRADE.Failure) as raised:
            self.controller.run()
        self.assertEqual(raised.exception.code, "master_unexpected_absence")
        self.assertEqual(self.actions, [])
        self.assertEqual(self.output_json("failed.json")["operation_stage"], "pre_stop_baseline")

    def test_nonzero_helper_output_records_only_safe_stage_code_and_digests(self):
        secret = b"synthetic-sensitive-helper-detail"
        stdout = encoded({"marker": "goby-client-schema28-helper-v1", "status": "failed", "error": "backup_baseline_drift",
                          "message": secret.decode()})
        self.helper_failures["backup"] = (stdout, secret)
        with self.assertRaises(UPGRADE.Failure) as raised:
            self.controller.run()
        self.assertEqual(raised.exception.code, "helper_failed")
        failed = self.output_json("failed.json")
        self.assertEqual((failed["operation_stage"], failed["error_code"]), ("helper_backup", "helper_failed"))
        ledgers = [UPGRADE.decode(entry["raw"]) for path, entry in self.memory.entries.items()
                   if Path(path).parent == self.output and Path(path).name.startswith("command-failure-")]
        self.assertEqual(len(ledgers), 1)
        self.assertEqual(ledgers[0], {"exit": 1, "operation_stage": "helper_backup", "error_code": "helper_failed",
            "stdout_sha256": self.digest(stdout), "stderr_sha256": self.digest(secret)})
        self.assertFalse(any(secret in entry["raw"] for path, entry in self.memory.entries.items()
                             if Path(path).is_relative_to(self.output) and entry["raw"] is not None))
        self.assertEqual(self.actions, [])

    def test_failed_run_sanitizes_an_unknown_operation_stage_and_exception(self):
        def fail():
            self.controller.operation_stage = "synthetic-sensitive-stage"
            raise RuntimeError("synthetic-sensitive-message")
        self.after_rehearsal = fail
        with self.assertRaises(RuntimeError):
            self.controller.run()
        report = self.output_json("failed.json")
        self.assertEqual((report["operation_stage"], report["error_code"]), ("unknown", "external_failure"))
        self.assertNotIn(b"synthetic-sensitive", self.memory.raw(self.output / "failed.json"))
        self.assertEqual(self.actions, [])


class DumpFailureFlowGuards(ControllerMemoryCase):
    """Exercise Python failure publication; the actual Go writer is verified separately."""

    def assert_dump_failure(self, category, expected=None):
        secret = b"synthetic-sensitive-dump-stderr"
        stdout = encoded({"marker": "goby-client-schema28-helper-v1", "status": "failed", "error": category})
        expected = expected or "helper_" + category
        self.helper_failures["backup"] = (stdout, secret)
        with self.assertRaises(UPGRADE.Failure) as raised:
            self.controller.run()
        self.assertEqual(raised.exception.code, expected)
        failed = self.output_json("failed.json")
        self.assertEqual((failed["operation_stage"], failed["error_code"]), ("helper_backup", expected))
        self.assertFalse(failed["automatic_retry"])
        self.assertFalse(failed["automatic_rollback"])
        ledgers = [UPGRADE.decode(entry["raw"]) for path, entry in self.memory.entries.items()
                   if Path(path).parent == self.output and Path(path).name.startswith("command-failure-")]
        self.assertEqual(ledgers, [{"exit": 1, "operation_stage": "helper_backup", "error_code": expected,
                                  "stdout_sha256": self.digest(stdout), "stderr_sha256": self.digest(secret)}])
        for path, entry in self.memory.entries.items():
            if Path(path).is_relative_to(self.output) and entry["raw"] is not None:
                self.assertNotIn(secret, entry["raw"])
                self.assertNotIn(b"synthetic-sensitive-unknown-dump-error", entry["raw"])
        self.assertEqual(self.actions, [])
        self.assertEqual(self.requests, [])
        self.assertEqual(self.controller.fence.reserved, set())
        self.assertEqual(sum(event[0] == "command" and event[1][0] == str(UPGRADE.TOOL / "migrate-client-schema28")
                             for event in self.events), 1)

    def test_dump_configuration_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_configuration")

    def test_dump_database_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_database")

    def test_dump_archive_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_archive")

    def test_dump_limit_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_limit")

    def test_dump_schema_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_schema")

    def test_dump_target_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_target")

    def test_dump_command_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_command")

    def test_dump_unsupported_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_unsupported")

    def test_dump_cancelled_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_cancelled")

    def test_dump_deadline_failure_preserves_its_safe_run_code(self):
        self.assert_dump_failure("snapshot_dump_deadline")

    def test_generic_dump_failure_preserves_the_existing_safe_fallback(self):
        self.assert_dump_failure("snapshot_dump_failed")

    def test_unknown_dump_failure_never_exports_its_private_error_text(self):
        self.assert_dump_failure("synthetic-sensitive-unknown-dump-error", "helper_failed")


class EmptyMasterFlowGuards(ControllerMemoryCase):
    master_present = False

    def test_original_empty_master_absence_survives_actual_run_and_attestation(self):
        result = self.controller.run()
        self.assertEqual(result["status"], "awaiting_outer_attestation")
        expected = UPGRADE.validate_master_baseline(self.before_snapshot, self.runtime)
        self.assertEqual(expected["state"], "absent")
        self.assertEqual(self.output_json("master-proof.json"), expected)
        self.assertEqual(UPGRADE.attest(self.prepare_attestation())["status"], "passed")
        self.assertNotIn(str(MASTER_KEY_PATH), self.memory.entries)
        self.assertNotIn(str(self.output / "private/materials/application/master.key"), self.memory.entries)
        self.assertFalse(any(attempt[0] in ("read", "read_fd", "create") and attempt[1] == str(MASTER_KEY_PATH)
                             for attempt in self.memory.attempts))
        self.assertEqual(self.actions, ["stop", "start"])
        self.assertEqual(len(self.requests), 5)

    def reject_new_key_population(self, name):
        def change():
            if name == "sessions":
                self.live_snapshot["database"]["tables"][name][1]["kind"] = "application_key"
            else:
                self.live_snapshot["database"]["tables"][name].append({"id": "synthetic-existing-key"})
        self.after_rehearsal = change
        with self.assertRaises(UPGRADE.Failure) as raised:
            self.controller.run()
        self.assertEqual(raised.exception.code, "master_required_missing")
        self.assertEqual(self.actions, [])

    def test_empty_master_scope_rejects_application_keys(self):
        self.reject_new_key_population("application_keys")

    def test_empty_master_scope_rejects_application_key_devices(self):
        self.reject_new_key_population("application_key_devices")

    def test_empty_master_scope_rejects_application_key_sessions(self):
        self.reject_new_key_population("sessions")

    def test_master_appearing_after_rehearsal_blocks_stop(self):
        self.after_rehearsal = lambda: self.seed(MASTER_KEY_PATH, MASTER_KEY_BYTES, uid=995, gid=986)
        with self.assertRaises(UPGRADE.Failure) as raised:
            self.controller.run()
        self.assertEqual(raised.exception.code, "master_unexpected_presence")
        self.assertEqual(self.actions, [])

    def test_master_runtime_path_drift_blocks_stop(self):
        def change():
            self.memory.entries[str(UPGRADE.RUNTIME)]["raw"] = self.runtime.replace(str(MASTER_KEY_PATH).encode(), b"/tmp/foreign-master.key")
        self.after_rehearsal = change
        with self.assertRaises(UPGRADE.Failure) as raised:
            self.controller.run()
        self.assertEqual(raised.exception.code, "master_runtime_path_invalid")
        self.assertEqual(self.actions, [])

    def test_snapshot_master_claim_cannot_change_without_its_original_proof(self):
        def change():
            self.live_snapshot["recovery"]["master.key"] = {"size": 32, "sha256": hashlib.sha256(MASTER_KEY_BYTES).hexdigest()}
        self.after_rehearsal = change
        with self.assertRaises(UPGRADE.Failure) as raised:
            self.controller.run()
        self.assertEqual(raised.exception.code, "master_state_changed")
        self.assertEqual(self.actions, [])

    def test_master_appearing_before_attestation_is_rejected(self):
        self.controller.run()
        args = self.prepare_attestation()
        self.seed(MASTER_KEY_PATH, MASTER_KEY_BYTES, uid=995, gid=986)
        with self.assertRaises(UPGRADE.Failure) as raised:
            UPGRADE.attest(args)
        self.assertEqual(raised.exception.code, "master_unexpected_presence")
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_resigned_master_proof_cannot_replace_zero_counts_with_false(self):
        self.controller.run()
        args = self.prepare_attestation()
        proof = self.output_json("master-proof.json")
        proof["application_keys"] = False
        raw = UPGRADE.canonical(proof)
        self.memory.entries[str(self.output / "master-proof.json")]["raw"] = raw
        report = self.output_json("report.json")
        report["evidence"]["master-proof.json"]["sha256"] = self.digest(raw)
        self.memory.entries[str(self.output / "report.json")]["raw"] = UPGRADE.canonical(report)
        with self.assertRaises(UPGRADE.Failure) as raised:
            UPGRADE.attest(args)
        self.assertEqual(raised.exception.code, "master_state_changed")
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)


class AttestationGuards(ControllerMemoryCase):
    def completed_run(self):
        result = self.controller.run()
        self.assertEqual(result["status"], "awaiting_outer_attestation")
        return self.prepare_attestation()

    def test_actual_independent_attest_publishes_only_after_terminal_readback(self):
        args = self.completed_run()
        result = UPGRADE.attest(args)
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["schema"], 28)
        self.assertTrue(result["recursive_cgroup_empty"])
        self.assertEqual(self.output_json("attestation.json"), result)
        self.assertEqual(self.actions, ["stop", "start"])
        self.assertEqual(len(self.requests), 5)
        self.assertEqual(self.frozen_entries(), self.frozen)
        self.reject(lambda: UPGRADE.attest(args))

    def test_terminal_liveness_invocation_and_exit_status_are_not_interchangeable(self):
        args = self.completed_run()
        base = copy.deepcopy(self.outer_terminal)
        for name, value in (("MainPID", "123"), ("SubState", "running"), ("Result", "exit-code"), ("ExecMainStatus", "1"),
                            ("InvocationID", "f" * 32), ("Description", "foreign-controller"), ("RemainAfterExit", "no")):
            with self.subTest(field=name):
                self.outer_terminal = {**base, name: value}
                self.reject(lambda: UPGRADE.attest(args))
                self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_same_controller_cannot_attest_its_own_success(self):
        self.controller.run()
        args = types.SimpleNamespace(mode="attest", input=UPGRADE.TOOL / "intent.json", input_sha256=self.input_sha)
        self.reject(lambda: UPGRADE.attest(args))
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_nonempty_controller_cgroup_blocks_attestation(self):
        args = self.completed_run()
        path = Path("/sys/fs/cgroup/system.slice") / self.intent["controller"]["unit"] / "cgroup.procs"
        self.memory.entries[str(path)]["raw"] = b"1900099\n"
        self.reject(lambda: UPGRADE.attest(args))
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_claiming_passed_without_outer_attestation_is_rejected(self):
        args = self.completed_run()
        value = self.output_json("report.json")
        value["status"] = "passed"
        self.memory.entries[str(self.output / "report.json")]["raw"] = UPGRADE.canonical(value)
        self.reject(lambda: UPGRADE.attest(args))

    def test_resigned_readiness_receipt_still_requires_exact_asset_hash(self):
        args = self.completed_run()
        response = self.output_json("http-4-result.json")
        response["body_sha256"] = synthetic_hash("foreign-javascript-response")
        raw = UPGRADE.canonical(response)
        self.memory.entries[str(self.output / "http-4-result.json")]["raw"] = raw
        report = self.output_json("report.json")
        report["evidence"]["http-4-result.json"]["sha256"] = self.digest(raw)
        self.memory.entries[str(self.output / "report.json")]["raw"] = UPGRADE.canonical(report)
        self.reject(lambda: UPGRADE.attest(args))
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_candidate_frontend_mode_drift_blocks_attestation(self):
        args = self.completed_run()
        self.memory.entries[str(UPGRADE.WEB / "assets/app.js")]["mode"] = stat.S_IFREG | 0o600
        self.reject(lambda: UPGRADE.attest(args))

    def test_candidate_binary_drift_blocks_attestation(self):
        args = self.completed_run()
        self.memory.entries[str(UPGRADE.BINARY)]["raw"] = b"foreign candidate binary"
        self.reject(lambda: UPGRADE.attest(args))


class ActualMainGuards(ControllerMemoryCase):
    def invoke(self, mode, digest=None):
        stream = io.StringIO()
        with contextlib.redirect_stdout(stream):
            status = UPGRADE.main([mode, "--input", str(UPGRADE.TOOL / "intent.json"), "--input-sha256", digest or self.input_sha])
        raw = stream.getvalue()
        self.assertNotIn("synthetic-password", raw)
        self.assertNotIn("synthetic-private-stderr", raw)
        return status, UPGRADE.decode(raw.encode())

    def test_actual_main_preflight_dispatches_read_only_preparation(self):
        status, report = self.invoke("preflight")
        self.assertEqual(status, 0)
        self.assertEqual(report["status"], "preflight_passed")
        self.assertEqual(self.actions, [])
        self.assertNotIn(str(self.output), self.memory.entries)

    def test_actual_main_run_then_separate_attest_preserves_distinct_statuses(self):
        status, report = self.invoke("run")
        self.assertEqual((status, report["status"]), (0, "awaiting_outer_attestation"))
        self.prepare_attestation()
        status, report = self.invoke("attest")
        self.assertEqual((status, report["status"]), (0, "passed"))
        self.assertIn(("alarm", 1100), self.events)
        self.assertIn(("alarm", 0), self.events)
        self.assertEqual(self.actions, ["stop", "start"])

    def test_actual_main_failure_is_sanitized_without_retry_or_rollback(self):
        self.fail_command = "stop"
        status, report = self.invoke("run")
        self.assertEqual(status, 1)
        self.assertEqual(report["status"], "failed")
        self.assertFalse(report["automatic_retry"])
        self.assertFalse(report["automatic_rollback"])
        self.assertEqual(self.actions, ["stop"])


if __name__ == "__main__":
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        sys.stdout.write('{"suite":"client-schema28-upgrade-guards","status":"blocked"}\n')
        raise SystemExit(1)
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(stream=sys.stderr, verbosity=2).run(suite)
    unexpected = sum(len(fence.violations) for fence in OBSERVED_FENCES if not fence.expected_denial)
    passed = result.wasSuccessful() and not result.skipped and unexpected == 0
    report = {"suite": "client-schema28-upgrade-guards", "status": "passed" if passed else "failed",
        "operator_sha256": OPERATOR_SHA256, "guard_sha256": GUARD_SHA256, "test_count": result.testsRun,
        "failures": len(result.failures), "errors": len(result.errors), "skips": len(result.skipped),
        "actual_controller_flow": True, "unexpected_effects": unexpected}
    sys.stdout.write(json.dumps(report, sort_keys=True, separators=(",", ":")) + "\n")
    raise SystemExit(0 if passed else 1)
