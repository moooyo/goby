#!/usr/bin/env python3
"""Exercise the main schema28 controller behind the accepted memory effect fence.

Only this guard and its sibling main controller are read before fencing. Main
service, database, lifecycle, key, inactive-slot, and release facts are synthetic.
No historical operator, real database, credential, binary, or asset is loaded.
Run only in an authorized remote verification environment.
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
OPERATOR_PATH = Path(__file__).with_name("upgrade-main-schema28.py")
OPERATOR_RAW = OPERATOR_PATH.read_bytes()
GUARD_RAW = Path(__file__).read_bytes()
OPERATOR_SHA256 = hashlib.sha256(OPERATOR_RAW).hexdigest()
GUARD_SHA256 = hashlib.sha256(GUARD_RAW).hexdigest()
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
    spec = importlib.util.spec_from_file_location("main_schema28_controller_under_test", OPERATOR_PATH)
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
        self.directory_modes, self.remove_targets, self.readlinks, self.locked = {}, set(), {}, set()
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
        expected_mode = self.directory_modes.get(key, 0o700 if Path(path) == self.output or Path(path).is_relative_to(self.output) else 0o755)
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
            self.violation("An atomic replacement escaped its declared main target.")
        destination = self.entries.get(target)
        parent = self.entries.get(self.key(Path(target).parent))
        origin = self.entries[source]
        if (parent is None or not stat.S_ISDIR(parent["mode"]) or not stat.S_ISREG(origin["mode"])
                or destination is not None and not stat.S_ISREG(destination["mode"])):
            self.violation("An atomic publication does not name declared regular files.")
        if origin["device"] != parent["device"]:
            raise OSError(errno.EXDEV, "Synthetic atomic replacement crossed filesystems.")
        if destination is not None:
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
        self.events.append(("close", self.handles[descriptor]["path"], descriptor))
        self.locked.discard(descriptor)
        del self.handles[descriptor]

    def unlink(self, path, **_kwargs):
        key = self.key(path)
        if key not in self.remove_targets or key not in self.entries or not self.owned(path):
            self.violation("An unlink escaped its exact owned temporary file.")
        entry = self.entries.pop(key)
        if not stat.S_ISREG(entry["mode"]):
            self.violation("An unlink targeted a nonregular object.")
        entry["links"] -= 1
        self.events.append(("unlink", key))

    def rmdir(self, path):
        key = self.key(path)
        if not self.owned(path) or key not in self.entries or not stat.S_ISDIR(self.entries[key]["mode"]) or list(self.children(path)):
            self.violation("A directory removal escaped its empty owned scope.")
        del self.entries[key]
        self.events.append(("rmdir", key))

    def readlink(self, path):
        key = self.key(path)
        if key not in self.readlinks:
            self.violation("An undeclared symbolic link was inspected.")
        return self.readlinks[key]

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

    def fchown(self, descriptor, uid, gid):
        handle = self.handle(descriptor)
        if not handle["flags"] & os.O_CREAT or not self.owned(handle["path"]):
            self.violation("An existing file received an owner mutation.")
        handle["entry"].update(uid=uid, gid=gid)

    def chmod(self, path, mode):
        key = self.key(path)
        if not self.owned(path) or key not in self.entries:
            self.violation("An unowned path received a mode mutation.")
        self.entries[key]["mode"] = stat.S_IFMT(self.entries[key]["mode"]) | mode
        self.events.append(("chmod", key, mode))

    def chown(self, path, uid, gid):
        key = self.key(path)
        if not self.owned(path) or key not in self.entries:
            self.violation("An unowned path received an owner mutation.")
        self.entries[key].update(uid=uid, gid=gid)
        self.events.append(("chown", key, uid, gid))

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
            (os, "fchown", self.fchown),
            (os, "chmod", self.chmod),
            (os, "chown", self.chown),
            (os, "umask", self.set_umask),
            (os, "replace", self.replace),
            (os, "unlink", self.unlink),
            (os, "rmdir", self.rmdir),
            (os, "readlink", self.readlink),
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
            (Path, "unlink", lambda path, **kwargs: self.unlink(path, **kwargs)),
            (Path, "rmdir", lambda path: self.rmdir(path)),
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

    def test_sibling_definitions_expose_the_actual_main_controller(self):
        for name in ("prepare", "preflight", "run"):
            self.assertTrue(callable(getattr(UPGRADE.Controller, name)))
        self.assertTrue(callable(UPGRADE.attest))


def main_intent_example(*, preparing=False):
    run_id = "20990101_010203_abcdef012345"
    closure = {name: synthetic_hash(name) for name in UPGRADE.TOOL_FILES}
    closure["upgrade-main-schema28.py"] = OPERATOR_SHA256
    closure["test-upgrade-main-schema28.py"] = GUARD_SHA256
    closure["accepted-web-report.json"] = UPGRADE.WEB_REPORT_SHA
    descriptor = lambda name: {"path": str(UPGRADE.TOOL / name), "sha256": closure[name]}
    prepared = UPGRADE.WORK / ("main-schema28-source55-prepared-" + run_id)
    gate = {"path": str(UPGRADE.WORK / "client-library-changed-ui-source55-v1/terminal.json"),
            "sha256": synthetic_hash("client-gate-terminal")}
    protected = [
        {"name": "candidate", "pid": 1458051, "start_ticks": 13067360, "exe": "/opt/goby-client-m3e/goby",
         "sha256": UPGRADE.NEW_BINARY_SHA, "uid": 995, "cgroup": "/system.slice/goby-client-m3e.service"},
        {"name": "proxy", "pid": 334022, "start_ticks": 378464, "exe": "/usr/bin/python3.13",
         "sha256": synthetic_hash("protected-proxy"), "uid": 0, "cgroup": "/system.slice/synthetic-proxy.service"},
        {"name": "workspace", "pid": 29001, "start_ticks": 29002, "exe": "/usr/lib/postgresql/17/bin/postgres",
         "sha256": synthetic_hash("protected-workspace"), "uid": 997, "cgroup": "/system.slice/synthetic-workspace.service"},
    ]
    for process in protected:
        process["boot_id"] = UPGRADE.OLD_PROCESS["boot_id"]
    return {"marker": UPGRADE.MARKER, "version": 1, "run_id": run_id, "tool": str(UPGRADE.TOOL),
        "output": str(UPGRADE.BACKUPS / ("run-" + run_id)), "prepared": str(prepared),
        "source": {"root": str(UPGRADE.SOURCE), "manifest_sha256": UPGRADE.SOURCE_SHA,
            "full_report": {"path": str(UPGRADE.FULL_REPORT), "sha256": UPGRADE.FULL_REPORT_SHA},
            "terminal": {"path": str(UPGRADE.FULL_TERMINAL), "sha256": UPGRADE.FULL_TERMINAL_SHA},
            "binary": {"path": str(UPGRADE.FULL_REPORT.parent / "tmp/goby-linux-amd64"),
                       "sha256": UPGRADE.NEW_BINARY_SHA, "bytes": 29337989},
            "publication": descriptor("publication.json")},
        "helper": {"source": descriptor("migrate-main-schema28.go"), "binary": descriptor("migrate-main-schema28"),
                   "build": descriptor("helper-build.json")},
        "web": {"source": str(UPGRADE.WEB_SOURCE), "report": descriptor("accepted-web-report.json")},
        "guards": descriptor("guards-report.json"), "go_guards": descriptor("helper-go-guards.json"),
        "source_closure": closure,
        "main_baseline": None if preparing else {"path": str(prepared / "baseline.json"), "sha256": synthetic_hash("main-baseline")},
        "client_gate": None if preparing else gate,
        "startup": {"deadline_utc": "2099-01-01T02:00:00Z", "min_remaining_seconds": 900, "retention_days": 30},
        "protected_services": protected,
        "historical": [{"path": str(UPGRADE.WORK / "synthetic-main-provenance" / name), "sha256": synthetic_hash(name)}
                       for name in ("schema27-completed.json", "schema27-preservation.json")]}


# Product input guard section.


def main_field_change(path, value):
    def change(document):
        for key in path[:-1]:
            document = document[key]
        document[path[-1]] = copy.deepcopy(value)
    return change


def main_product_fixture(changes=None, *, preparing=False):
    """Build synthetic signed inputs; no product or Go file is loaded or executed."""
    changes, files, documents = changes or {}, {}, {}
    intent = copy.deepcopy(main_intent_example(preparing=preparing))

    def changed(name, value):
        documents[name] = value
        if name in changes:
            changes[name](value)
        return value

    def put(path, value, mode=0o600):
        raw = encoded(value)
        files[str(path)] = (raw, mode)
        return {"path": str(path), "sha256": UPGRADE.sha(raw)}

    catalog28 = changed("catalog", {"version": 28, "objects": ["synthetic-main-schema28"],
        "migrations": [{"version": 28, "name": "0028_storage_root_bindings.sql"}]})
    contents = {"go.mod": b"module github.com/moooyo/goby\n", "go.sum": b"Synthetic checksums.\n",
        UPGRADE.SELECTED[0]: b"package config\n", UPGRADE.SELECTED[1]: b"package backuppg\n",
        UPGRADE.SELECTED[2]: encoded({"version": 27, "objects": ["synthetic-main-schema27"]}),
        UPGRADE.SELECTED[3]: encoded(catalog28)}
    contents.update({"internal/synthetic/main_%03d.go" % index:
        ("package synthetic\n// Input %d.\n" % index).encode() for index in range(101)})
    manifest_files = {name: put(UPGRADE.SOURCE / name, raw)["sha256"] for name, raw in contents.items()}
    catalog27_sha, catalog28_sha = (manifest_files[name] for name in UPGRADE.SELECTED[2:])
    for index in range(4243 - len(manifest_files)):
        name = "unread-synthetic/main-%04d.txt" % index
        manifest_files[name] = synthetic_hash(name)
    manifest = changed("manifest", {"marker": "goby-client-backup-source-m3e-v1", "files": manifest_files})
    source = intent["source"]
    source["manifest_sha256"] = put(UPGRADE.SOURCE / "backup-source-inputs.json", manifest)["sha256"]
    put(UPGRADE.TOOL / "upgrade-main-schema28.py", OPERATOR_RAW)
    put(UPGRADE.TOOL / "test-upgrade-main-schema28.py", GUARD_RAW)
    intent["helper"]["source"] = put(UPGRADE.TOOL / "migrate-main-schema28.go", b"package main\nfunc main() {}\n")
    helper_test = put(UPGRADE.TOOL / "migrate-main-schema28_test.go", b"package main\n// Synthetic overlay only.\n")
    intent["helper"]["binary"] = put(UPGRADE.TOOL / "migrate-main-schema28", b"Synthetic main migration helper.\n", 0o755)
    helper_bytes = files[str(UPGRADE.TOOL / "migrate-main-schema28")][0]
    put(UPGRADE.VERIFIED_HELPER_TOOL / "migrate-main-schema28", helper_bytes, 0o755)
    binary = b"M" * 29337989
    source["binary"] = put(UPGRADE.FULL_REPORT.parent / "tmp/goby-linux-amd64", binary, 0o755)
    source["binary"]["bytes"] = len(binary)
    changed("binary", source["binary"])
    required_tests = ["TestHTTPLibraryPermissionsCSRFAndLivePolicyRevocation",
        "TestHTTPLibraryScanBrowseFieldsAndDeletePreservesMedia",
        "TestPostgreSQLRootBindingArchiveMigratesSchema27WithoutInferringApproval"]
    report = changed("report", {"marker": "goby-client-backup-pair-m3e-v1", "status": "passed", "mode": "full", "schema": 28,
        "source": str(UPGRADE.SOURCE), "source_manifest_sha256": source["manifest_sha256"], "run_id": UPGRADE.FULL_RUN,
        "catalog_sha256": catalog28_sha, "unit_exit": 0, "binary": copy.deepcopy(source["binary"]),
        "cleanup": {name: True for name in ("unit_terminal", "hba_restored_exactly", "goby_backup_m3e_source_removed",
            "goby_backup_m3e_target_removed", "preexisting_catalog_unchanged", "receipt_saved")},
        "tests": {"top_level_passes": 2173, "failures": 0, "skips": 0, "passed": required_tests +
            ["TestSyntheticMainProduct%04d" % index for index in range(2170)]},
        "packages": ["github.com/moooyo/goby/synthetic/main%02d" % index for index in range(25)]})
    source["full_report"] = put(UPGRADE.FULL_REPORT, report)
    finished = put(UPGRADE.FULL_TERMINAL.with_name("finished-receipt.json"), {"marker": "synthetic-finished", "finished": True})
    terminal = changed("terminal", {"marker": "goby-collection-folder-source55-full-terminal-v1", "status": "passed",
        "run_id": UPGRADE.FULL_RUN, "unit": UPGRADE.FULL_UNIT, "source": str(UPGRADE.SOURCE),
        "source_manifest_sha256": source["manifest_sha256"], "report": str(UPGRADE.FULL_REPORT),
        "report_sha256": source["full_report"]["sha256"], "binary": copy.deepcopy(source["binary"]), "recursive_cgroup_empty": True,
        "state": {"MainPID": "0", "Result": "success", "ExecMainStatus": "0", "SubState": "exited", "InvocationID": UPGRADE.FULL_INVOCATION},
        "tests": {key: report["tests"][key] for key in ("failures", "skips", "top_level_passes")},
        "packages": len(report["packages"]), "cleanup": copy.deepcopy(report["cleanup"]), "finished_receipt_sha256": finished["sha256"]})
    source["terminal"] = put(UPGRADE.FULL_TERMINAL, terminal)
    publication = changed("publication", {"marker": "goby-source55-publication-v1", "commit": "c" * 40,
        "source_manifest_sha256": source["manifest_sha256"], "binary_sha256": source["binary"]["sha256"],
        "full_report_sha256": source["full_report"]["sha256"], "full_terminal_sha256": source["terminal"]["sha256"]})
    source["publication"] = put(UPGRADE.TOOL / "publication.json", publication)
    web_bytes = changed("web_bytes", {"index.html": b'<html><script type="module" src="/admin/assets/main.js"></script>'
        b'<link rel="stylesheet" href="/admin/assets/main.css"></html>', "assets/main.js": b"export const synthetic = true;\n",
        "assets/main.css": b"body { color: black; }\n"})
    web_files = {name: put(UPGRADE.WEB_SOURCE / name, raw)["sha256"] for name, raw in web_bytes.items()}
    web_report = changed("web_report", {"status": "passed", "tests": {"expected": 68, "unexpected": 0, "flaky": 0, "skipped": 0},
        "dist_files": web_files})
    intent["web"]["report"] = put(UPGRADE.TOOL / "accepted-web-report.json", web_report)
    product_files = {name: digest for name, digest in manifest["files"].items()
        if name in ("go.mod", "go.sum") or name.startswith(("cmd/", "internal/"))}
    build_root = UPGRADE.WORK / "main-schema28-source55-build-01"
    build_files = {**product_files, "cmd/main-schema28-migration/main.go": intent["helper"]["source"]["sha256"],
        "cmd/main-schema28-migration/main_test.go": helper_test["sha256"]}
    go = {"path": "/opt/goby-toolchains/go1.27.1/bin/go", "sha256": "30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624"}
    build = {"marker": "goby-main-schema28-helper-build-v1", "source": str(UPGRADE.SOURCE),
        "source_manifest_sha256": source["manifest_sha256"], "build_root": str(build_root), "files": copy.deepcopy(build_files),
        "helper_source_sha256": intent["helper"]["source"]["sha256"], "binary_sha256": intent["helper"]["binary"]["sha256"], "go": go,
        "argv": [go["path"], "build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o",
            str(UPGRADE.VERIFIED_HELPER_TOOL / "migrate-main-schema28"), "./cmd/main-schema28-migration/main.go"],
        "environment": {"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0", "GOWORK": "off", "GOTOOLCHAIN": "local",
            "GOPROXY": "off", "GOSUMDB": "off"}, "exit_code": 0}
    verification = changed("verification", {"marker": "goby-main-schema28-helper-verification-v1", "status": "passed",
        "database": False, "service_mutations": 0, "source": str(UPGRADE.SOURCE), "source_manifest_sha256": source["manifest_sha256"],
        "build_root": str(build_root), "files": copy.deepcopy(build_files), "binary": {"sha256": intent["helper"]["binary"]["sha256"]},
        "formatted_sources": {"migrate-main-schema28.go": intent["helper"]["source"]["sha256"], "migrate-main-schema28_test.go": helper_test["sha256"]},
        "test_counts": {"pass": 15, "fail": 0, "skip": 0}, "checks": [{"name": "build", "exit_code": 0, "argv": copy.deepcopy(build["argv"])}]})
    verification_descriptor = put(UPGRADE.HELPER_VERIFICATION, verification)
    build["verification_report"] = verification_descriptor
    build = changed("build", build)
    intent["helper"]["build"] = put(UPGRADE.TOOL / "helper-build.json", build)
    put(UPGRADE.VERIFIED_HELPER_TOOL / "helper-build.json", build)
    guards = changed("guards", {"suite": "main-schema28-upgrade-guards", "status": "passed", "operator_sha256": OPERATOR_SHA256,
        "guard_sha256": GUARD_SHA256, "test_count": 20, "actual_controller_flow": True, "failures": 0, "errors": 0,
        "skips": 0, "unexpected_effects": 0})
    intent["guards"] = put(UPGRADE.TOOL / "guards-report.json", guards)
    go_guards = changed("go_guards", {"marker": "goby-main-schema28-helper-go-guards-v1", "status": "passed", "exit_code": 0,
        "database_access": False, "failures_or_skips": 0, "helper_source_sha256": intent["helper"]["source"]["sha256"],
        "guard_source_sha256": helper_test["sha256"], "verification_report": verification_descriptor,
        "tests": ["TestSyntheticMainGuard%d" % index for index in range(15)]})
    intent["go_guards"] = put(UPGRADE.TOOL / "helper-go-guards.json", go_guards)
    put(UPGRADE.VERIFIED_HELPER_TOOL / "helper-go-guards.json", go_guards)
    intent["historical"] = [put(Path(item["path"]), {"marker": "synthetic-sealed-provenance", "index": index})
        for index, item in enumerate(intent["historical"])]
    intent["source_closure"] = {name: UPGRADE.sha(files[str(UPGRADE.TOOL / name)][0]) for name in UPGRADE.TOOL_FILES}
    return {"intent": intent, "files": files, "documents": documents,
        "trees": {str(UPGRADE.WEB_SOURCE): copy.deepcopy(web_report["dist_files"]), str(build_root): copy.deepcopy(build_files)},
        "patches": {"__file__": str(UPGRADE.TOOL / "upgrade-main-schema28.py"), "SOURCE_SHA": source["manifest_sha256"],
            "CATALOG27_SHA": catalog27_sha, "CATALOG28_SHA": catalog28_sha, "NEW_BINARY_SHA": source["binary"]["sha256"],
            "FULL_REPORT_SHA": source["full_report"]["sha256"], "FULL_TERMINAL_SHA": source["terminal"]["sha256"],
            "WEB_REPORT_SHA": intent["web"]["report"]["sha256"], "HELPER_VERIFICATION_SHA": verification_descriptor["sha256"],
            "VERIFIED_HELPER_BYTES": len(helper_bytes), "VERIFIED_BUILD_SHA": intent["helper"]["build"]["sha256"],
            "VERIFIED_GO_GUARDS_SHA": intent["go_guards"]["sha256"]}}


class MainIntentGuards(GuardCase):
    def test_prepare_allows_future_client_gate_but_run_requires_gate_and_main_baseline(self):
        prepared = main_intent_example(preparing=True)
        self.assertEqual(UPGRADE.validate_intent(prepared, "prepare"), prepared)
        self.reject(lambda: UPGRADE.validate_intent(prepared, "run"))
        complete = main_intent_example()
        self.assertEqual(UPGRADE.validate_intent(complete, "run"), complete)
        self.assertEqual(UPGRADE.input_core(prepared), UPGRADE.input_core(complete))
        for field in ("main_baseline", "client_gate"):
            changed = copy.deepcopy(complete)
            changed[field] = None
            self.reject(lambda: UPGRADE.validate_intent(changed, "run"))

    def test_main_intent_rejects_candidate_scope_missing_publication_and_closure_gaps(self):
        changes = (lambda value: value.update(tool=str(UPGRADE.WORK / "client-schema28-source55-tool-05")),
            lambda value: value.update(tool=str(UPGRADE.VERIFIED_HELPER_TOOL)),
            lambda value: value.update(tool=str(UPGRADE.WORK / "main-schema28-source55-tool-02")),
            lambda value: value.update(output=str(UPGRADE.WORK / "candidate-output")),
            lambda value: value["source"].pop("publication"), lambda value: value["source"].update(publication=None),
            lambda value: value["source"]["binary"].update(bytes=29337988),
            lambda value: value["source_closure"].pop("migrate-main-schema28_test.go"),
            lambda value: value["source_closure"].pop("helper-go-guards.json"),
            lambda value: value["go_guards"].update(sha256=synthetic_hash("foreign-go-guards")))
        for index, change in enumerate(changes):
            with self.subTest(change=index):
                value = main_intent_example(preparing=True)
                change(value)
                self.reject(lambda: UPGRADE.validate_intent(value, "prepare"))

    def test_main_intent_rejects_untyped_deadlines_and_unbound_protected_processes(self):
        for path, value in ((('version',), True), (('startup', 'min_remaining_seconds'), True),
                (('startup', 'min_remaining_seconds'), 899), (('startup', 'retention_days'), 0),
                (('startup', 'deadline_utc'), 'expired'), (('protected_services', 0, 'uid'), 0),
                (('protected_services', 0, 'exe'), str(UPGRADE.BINARY)), (('protected_services', 1, 'pid'), 334023),
                (('protected_services', 2, 'name'), 'candidate')):
            with self.subTest(path=path):
                intent = main_intent_example()
                main_field_change(path, value)(intent)
                self.reject(lambda: UPGRADE.validate_intent(intent, "run"))


class MainProductInputsGuards(GuardCase):
    def check_product(self, changes=None, *, preparing=False, member=None, damage=None, original_damage=None, helper_size=None):
        fixture, reads = main_product_fixture(changes, preparing=preparing), []
        members = sorted(UPGRADE.TOOL / name for name in UPGRADE.TOOL_FILES)
        metadata = {"st_mode": stat.S_IFREG | 0o600, "st_uid": 0, "st_gid": 0, "st_nlink": 1}
        if member == "missing":
            members.pop()
        elif member == "extra":
            members.append(UPGRADE.TOOL / "intent.json")
        elif member == "symlink":
            metadata["st_mode"] = stat.S_IFLNK | 0o600
        elif member == "hardlink":
            metadata["st_nlink"] = 2
        if damage is not None:
            key = str(UPGRADE.TOOL / damage)
            raw, mode = fixture["files"][key]
            fixture["files"][key] = (raw + b"Synthetic input drift.\n", mode)
        if original_damage is not None:
            key = str(UPGRADE.VERIFIED_HELPER_TOOL / original_damage)
            raw, mode = fixture["files"][key]
            fixture["files"][key] = (raw + b"Synthetic original helper drift.\n", mode)
        if helper_size is not None:
            fixture["patches"]["VERIFIED_HELPER_BYTES"] = helper_size

        def protected(path, expected=None, **kwargs):
            key = str(path)
            reads.append(key)
            if key == "/opt/goby-toolchains/go1.27.1/bin/go":
                self.assertEqual(expected, "30969f97169d7f43fe6a085873d75613adc21e30818a8c61d95bd27275df4624")
                self.assertEqual(kwargs.get("modes"), (0o755,))
                return b"Synthetic authenticated toolchain transport.\n"
            self.assertIn(key, fixture["files"], "An undeclared input was read.")
            raw, mode = fixture["files"][key]
            UPGRADE.require((expected is None or UPGRADE.sha(raw) == expected) and mode in kwargs.get("modes", (0o600, 0o644))
                and len(raw) <= kwargs.get("limit", 128 << 20), "Synthetic input transport rejected a digest, mode or size.")
            return raw

        def directory(path, mode=0o700):
            self.assertIn(path, (UPGRADE.WORK, UPGRADE.TOOL))
            self.assertEqual(mode, 0o700)
            return {"device": 7, "inode": 100, "uid": 0, "gid": 0, "mode": mode}

        def tree(root, expected, **kwargs):
            self.assertIn(str(root), fixture["trees"])
            self.assertEqual(kwargs, {"maximum": 64 << 20} if root == UPGRADE.WEB_SOURCE else {})
            UPGRADE.require(expected == fixture["trees"][str(root)], "Synthetic copied tree differs.")
            return {"root": str(root), "files": copy.deepcopy(expected), "root_identity": {"device": 7, "inode": 200}}

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
            for owner, name, value in ((UPGRADE, "protected", protected), (UPGRADE, "directory", directory),
                    (UPGRADE, "verify_tree", tree), (Path, "iterdir", iterdir), (Path, "lstat", lstat)):
                stack.enter_context(patch.object(owner, name, value))
            result = UPGRADE.verify_inputs(fixture["intent"])
        self.assertEqual(fixture["intent"], original)
        return result, fixture, reads

    def reject_fields(self, name, cases):
        for path, value in cases:
            with self.subTest(document=name, path=path):
                self.reject(lambda: self.check_product({name: main_field_change(path, value)}))

    def test_product_prerequisites_pass_before_future_client_gate_and_use_exact_main_overlays(self):
        result, fixture, reads = self.check_product(preparing=True)
        self.assertIsNone(fixture["intent"]["client_gate"])
        self.assertEqual(len(result["binary"]), 29337989)
        self.assertEqual(result["full_terminal"]["tests"], {"failures": 0, "skips": 0, "top_level_passes": 2173})
        self.assertEqual(result["full_terminal"]["packages"], 25)
        self.assertEqual(result["frontend_routes"], [("/admin/", "index.html"), ("/admin/assets/main.js", "assets/main.js"),
            ("/admin/assets/main.css", "assets/main.css")])
        self.assertEqual(result["build"]["files"]["cmd/main-schema28-migration/main_test.go"],
            fixture["intent"]["source_closure"]["migrate-main-schema28_test.go"])
        self.assertEqual({Path(path).name for path in reads if Path(path).parent == UPGRADE.TOOL}, UPGRADE.TOOL_FILES)
        self.assertFalse(any("unread-synthetic" in path for path in reads))

    def test_product_closure_rejects_missing_extra_special_and_drifted_main_files(self):
        for member in ("missing", "extra", "symlink", "hardlink"):
            with self.subTest(member=member):
                self.reject(lambda: self.check_product(member=member))
        for name in sorted(UPGRADE.TOOL_FILES):
            with self.subTest(damage=name):
                self.reject(lambda: self.check_product(damage=name))

    def test_product_manifest_catalog_and_binary_are_rechecked(self):
        self.reject_fields("manifest", [(('marker',), 'foreign-source'), (('files', UPGRADE.SELECTED[0]), 'invalid')])
        self.reject_fields("catalog", [(('version',), 27)])
        self.reject_fields("binary", [(('bytes',), 29337988), (('path',), str(UPGRADE.BINARY))])

    def test_full_report_requires_complete_typed_success_cleanup_and_test_inventory(self):
        self.reject_fields("report", [(('mode',), 'targeted'), (('schema',), True), (('unit_exit',), False),
            (('run_id',), '20990101_010203_abcdef012345'), (('cleanup', 'receipt_saved'), 1),
            (('tests', 'top_level_passes'), 2172), (('tests', 'skips'), 1), (('tests', 'passed', 0), 'TestUnrelated'),
            (('packages', 0), 'example.invalid/foreign')])

    def test_full_terminal_requires_typed_summary_and_its_own_successful_invocation(self):
        self.reject_fields("terminal", [(('packages',), True), (('packages',), 24), (('tests', 'failures'), False),
            (('tests', 'top_level_passes'), 2174), (('recursive_cgroup_empty',), 1), (('unit',), UPGRADE.UNIT),
            (('state', 'MainPID'), '123'), (('state', 'InvocationID'), UPGRADE.OLD_INVOCATION),
            (('report_sha256',), synthetic_hash('foreign-report')), (('cleanup', 'receipt_saved'), 1)])
        self.reject(lambda: self.check_product({"terminal": lambda value: value["tests"].update(passed=["TestExpanded"])}))

    def test_publication_is_mandatory_and_binds_every_product_receipt(self):
        self.reject_fields("publication", [((field,), synthetic_hash('foreign-' + field)) for field in
            ('source_manifest_sha256', 'binary_sha256', 'full_report_sha256', 'full_terminal_sha256')] +
            [(('commit',), '0' * 40), (('marker',), 'foreign-publication')])
        self.reject(lambda: self.check_product({"publication": lambda value: value.clear()}, preparing=True))

    def test_frontend_requires_real_unique_manifest_owned_entrypoints(self):
        self.reject_fields("web_report", [(('status',), 'failed'), (('tests', 'expected'), 67), (('tests', 'unexpected'), False)])
        for raw in (b'<script type="module" src="/admin/assets/main.js"></script>',
                b'<script type="module" src="/admin/assets/unknown.js"></script><link rel="stylesheet" href="/admin/assets/main.css">'):
            self.reject(lambda: self.check_product({"web_bytes": main_field_change(('index.html',), raw)}))

    def test_helper_build_requires_both_main_overlays_and_offline_main_output(self):
        self.reject_fields("build", [(('marker',), 'goby-client-schema28-helper-build-v1'), (('build_root',), str(UPGRADE.SOURCE)),
            (('exit_code',), False), (('argv', 6), str(UPGRADE.WORK / 'client-helper')),
            (('environment', 'GOTOOLCHAIN'), 'auto'), (('go', 'sha256'), synthetic_hash('foreign-go'))])
        for name in ("cmd/main-schema28-migration/main.go", "cmd/main-schema28-migration/main_test.go"):
            self.reject(lambda: self.check_product({"build": lambda value: value["files"].pop(name)}))

    def test_new_tool_copies_the_original_helper_and_receipts_without_rewriting_build_history(self):
        result, fixture, reads = self.check_product(preparing=True)
        self.assertNotEqual(UPGRADE.TOOL, UPGRADE.VERIFIED_HELPER_TOOL)
        self.assertEqual(result["build"]["argv"][6], str(UPGRADE.VERIFIED_HELPER_TOOL / "migrate-main-schema28"))
        for name in ("migrate-main-schema28", "helper-build.json", "helper-go-guards.json"):
            self.assertIn(str(UPGRADE.VERIFIED_HELPER_TOOL / name), reads)
            self.assertEqual(fixture["files"][str(UPGRADE.VERIFIED_HELPER_TOOL / name)], fixture["files"][str(UPGRADE.TOOL / name)])
            self.reject(lambda: self.check_product(original_damage=name))
        self.reject(lambda: self.check_product(helper_size=18357654))
        self.reject(lambda: self.check_product({"build": main_field_change(("argv", 6), str(UPGRADE.TOOL / "migrate-main-schema28"))}))

    def test_python_and_go_guard_receipts_bind_actual_main_sources_without_database_access(self):
        self.reject_fields("guards", [(('suite',), 'client-schema28-upgrade-guards'), (('operator_sha256',), synthetic_hash('foreign-operator')),
            (('actual_controller_flow',), 1), (('test_count',), True), (('skips',), 1)])
        self.reject_fields("go_guards", [(('marker',), 'foreign-go-guards'), (('status',), 'failed'), (('exit_code',), False),
            (('database_access',), 0), (('database_access',), True), (('failures_or_skips',), 1),
            (('helper_source_sha256',), synthetic_hash('foreign-main-source')), (('guard_source_sha256',), synthetic_hash('foreign-main-tests')),
            (('tests',), ['TestTooFew']), (('tests',), ['TestRepeated'] * 7)])

    def test_derived_helper_receipts_require_actual_immutable_build_and_test_provenance(self):
        self.reject_fields("verification", [(('status',), 'failed'), (('database',), True), (('service_mutations',), True),
            (('test_counts', 'fail'), 1), (('test_counts', 'pass'), 14), (('checks', 0, 'exit_code'), False),
            (('checks', 0, 'argv', 6), str(UPGRADE.BINARY)), (('formatted_sources', 'migrate-main-schema28.go'), synthetic_hash('foreign-main-helper'))])
        self.reject(lambda: self.check_product({"verification": lambda value: value["checks"].append(copy.deepcopy(value["checks"][0]))}))


def main_client_gate_fixture(intent, catalog=None, changes=None):
    """Build a complete synthetic original-client and candidate-upgrade chain."""
    changes, files, aliases = changes or {}, {}, {}
    catalog = ["synthetic-main-catalog28"] if catalog is None else catalog
    scope = UPGRADE.WORK / "client-library-changed-ui-source55-v7"
    unit, invocation = "goby-client-library-changed-ui-source55-controller-v7.service", "a1" * 16
    state = {"MainPID": "0", "InvocationID": invocation, "ActiveState": "active", "SubState": "exited", "ExecMainStatus": "0",
             "Result": "success", "RemainAfterExit": "yes", "ControlGroup": ""}
    def put(name, path, value, fixed=None):
        document = copy.deepcopy(value)
        if name in changes:
            changes[name](document)
        raw = UPGRADE.canonical(document)
        actual = hashlib.sha256(raw).hexdigest()
        digest = fixed or actual
        if fixed is not None:
            aliases[actual] = fixed
        files[str(path)] = (raw, digest)
        return {"path": str(path), "sha256": digest}
    pin = next(value for value in intent["protected_services"] if value["name"] == "candidate")
    process = {key: pin[key] for key in UPGRADE.OLD_PROCESS}
    upgrade_root = UPGRADE.WORK / "client-schema28-source55-upgrade-20990101_000000_abcdef012345"
    upgrade_intent = put("upgrade_intent", UPGRADE.WORK / "client-schema28-source55-tool-05/intent.json",
        {"output": str(upgrade_root), "source": {"root": str(UPGRADE.SOURCE), "manifest_sha256": UPGRADE.SOURCE_SHA}})
    runtime_sha = synthetic_hash("preserved-candidate-runtime")
    state_doc = put("state", UPGRADE.WORK / "client-fixture.json", {"schema": 28, "process": process,
        "binary_sha256": UPGRADE.NEW_BINARY_SHA, "runtime_sha256": runtime_sha,
        "schema28_upgrade": {"intent_sha256": upgrade_intent["sha256"]},
        "schema28_source": {"source_manifest_sha256": UPGRADE.SOURCE_SHA, "catalog_sha256": UPGRADE.CATALOG28_SHA}})
    publication = {"commit": "bc" * 20, "source_manifest_sha256": UPGRADE.SOURCE_SHA}
    snapshot = {"schema": 28, "database": {"catalog": catalog, "unsupported": False,
        "tables": {"sessions": [], "devices": [], "activity_entries": []}, "sequences": {"sessions_id_seq": {"last_value": 41}}}}
    original = put("original", upgrade_root / "after-full.json", snapshot)
    upgraded = put("upgrade_report", upgrade_root / "report.json", {"marker": "goby-client-schema28-source55-upgrade-v1",
        "status": "awaiting_outer_attestation", "intent_sha256": upgrade_intent["sha256"], "new_process": process,
        "state_sha256": state_doc["sha256"], "publication": publication, "binary": {"sha256": UPGRADE.NEW_BINARY_SHA},
        "evidence": {"after-full.json": original}})
    attested = put("upgrade_attestation", upgrade_root / "attestation.json", {"marker": "goby-client-schema28-source55-upgrade-v1",
        "status": "passed", "intent_sha256": upgrade_intent["sha256"], "report_sha256": upgraded["sha256"],
        "state_sha256": state_doc["sha256"], "recursive_cgroup_empty": True}, UPGRADE.CLIENT_UPGRADE_ATTEST_SHA)
    history = []
    for version in range(2, 7):
        prior = UPGRADE.WORK / ("client-library-changed-ui-source55-v%d" % version)
        entry = {"version": version, **{key: {"path": str(prior / filename), "sha256": synthetic_hash("prior-%d-%s" % (version, key))}
            for key, filename in (("input", "input.json"), ("browser_report", "browser/report.json"),
                ("controller_report", "report.json"), ("before_snapshot", "before-full.json"), ("after_snapshot", "after-full.json"))}}
        entry["terminal"] = {"path": str(UPGRADE.WORK / ("client-library-changed-source55-execution-%02d/terminal.json" % version)),
                             "sha256": synthetic_hash("prior-terminal-%d" % version)}
        history.append(entry)
    authority = {"upgrade_intent": upgrade_intent, "upgrade_report": upgraded, "upgrade_attestation": attested,
                 "current_snapshot": original, "history": history}
    before = put("before_snapshot", scope / "before-full.json", snapshot)
    if "before_descriptor" in changes:
        changes["before_descriptor"](before)
    browser_authority = {**copy.deepcopy(authority), "before_snapshot": copy.deepcopy(before)}
    candidate = {"source": str(UPGRADE.SOURCE), "source_manifest_sha256": UPGRADE.SOURCE_SHA,
        "binary_sha256": UPGRADE.NEW_BINARY_SHA, "process": process, "invocation_id": "ca" * 16,
        "base_url": "http://127.0.0.1:18196", "direct_url": "http://127.0.0.1:18198", "state_sha256": state_doc["sha256"],
        "runtime_sha256": runtime_sha, "publication": publication}
    controller, target, node = {"unit": unit}, {"name": "Synthetic Movies"}, {"pid": 220001, "start_ticks": 220002}
    source = put("input", scope / "input.json", {"marker": "goby-client-library-changed-input-v1", "version": 1,
        "mode": UPGRADE.CLIENT_GATE_MODE, "root": str(scope), "output": str(scope / "browser"), "controller": controller,
        "candidate": candidate, "authority": browser_authority, "target": target})
    origin = put("controller_intent", scope / "intent.json", {"marker": "goby-client-library-changed-observation-v1",
        "mode": UPGRADE.CLIENT_GATE_MODE, "root": str(scope), "controller": controller, "controller_invocation": invocation})
    common = {"version": 1, "mode": UPGRADE.CLIENT_GATE_MODE, "input_sha256": source["sha256"], "controller": controller,
              "full_m3_complete": False, "client_acceptance": False}
    browser = put("browser", scope / "browser/report.json", {**common, "marker": "goby-client-library-changed-report-v1",
        "result": "passed", "library_changed_client_acceptance": True, "failure": None, "restoration": "confirmed",
        "candidate": candidate, "authority": browser_authority, "target": target, "node_process": node, "actor": {"page_error_count": 0},
        "closure": {"context_closed": True, "browser_closed": True, "proxy_closed": True}})
    after = put("after_snapshot", scope / "after-full.json", snapshot)
    report = put("report", scope / "report.json", {**common, "marker": "goby-client-library-changed-observation-v1", "status": "passed",
        "worker_chain_ledger_passed": True, "acceptance_ready_for_outer_terminal": True, "outer_controller_terminal_required": True,
        "library_changed_client_acceptance": False, "errors": [], "restoration": "confirmed", "automatic_retry": False,
        "sql_business_writes": False, "candidate_or_primary_service_writes": False, "restoration_required": False, "browser_fallback_used": False,
        "candidate_process": process, "candidate_invocation": candidate["invocation_id"], "state_sha256": state_doc["sha256"],
        "authority": authority, "node_process": node, "evidence": {"intent.json": origin, "browser-report.json": browser,
            "before-full.json": before, "after-full.json": after},
        "ledger": {"new_sessions": 2, "new_devices": 1, "new_audits": 6, "metadata_revision_delta": 2,
                   "old_rows_sequences_private_preserved": True, "owned_sessions_closed": True},
        "worker_terminal": {"MainPID": "0", "ExecMainStatus": "0", "Result": "success", "cgroup_empty": True}})
    terminal = put("terminal", scope / "terminal.json", {"marker": UPGRADE.CLIENT_GATE_MARKER, "version": 1, "status": "passed",
        "mode": UPGRADE.CLIENT_GATE_MODE, "scope": str(scope), "unit": unit, "state": state, "recursive_cgroup_empty": True,
        "library_changed_client_acceptance": True, "full_m3_complete": False,
        "artifacts": {"input": source, "report": report, "browser": browser, "after_snapshot": after}})
    return {"descriptor": terminal, "files": files, "aliases": aliases, "unit": unit, "state": state, "catalog": catalog}


class MainClientGateGuards(GuardCase):
    def gate(self, changes=None, *, damage_before=False):
        intent = main_intent_example()
        fixture = main_client_gate_fixture(intent, changes=changes)
        intent["client_gate"] = fixture["descriptor"]
        controller = object.__new__(UPGRADE.Controller)
        controller.intent, controller.inputs = intent, {"catalog": {"objects": fixture["catalog"]}}
        reads = []
        if damage_before:
            name = str(UPGRADE.WORK / "client-library-changed-ui-source55-v7/before-full.json")
            raw, fixed = fixture["files"][name]
            fixture["files"][name] = (raw + b" ", fixed)
        def digest(raw):
            value = hashlib.sha256(raw).hexdigest()
            return fixture["aliases"].get(value, value)
        def protected(path, expected=None, **_kwargs):
            reads.append(str(path))
            UPGRADE.require(str(path) in fixture["files"], "An undeclared client artifact was requested.")
            raw, fixed = fixture["files"][str(path)]
            UPGRADE.require(digest(raw) == expected == fixed, "Synthetic client receipt digest changed.")
            return raw
        def properties(unit, names):
            self.assertEqual(unit, fixture["unit"])
            return {key: fixture["state"][key] for key in names}
        controller.properties = properties
        with patch.object(UPGRADE, "sha", digest), patch.object(UPGRADE, "protected", protected), patch.object(UPGRADE, "cgroup_empty",
                side_effect=lambda unit: self.assertEqual(unit, fixture["unit"])) as empty:
            result = UPGRADE.Controller.validate_client_gate(controller)
        self.client_gate_reads = reads
        return result, intent, empty.call_count

    def test_future_gate_is_not_invented_and_completed_gate_requires_independent_terminal(self):
        future = main_intent_example(preparing=True)
        UPGRADE.validate_intent(future, "prepare")
        controller = object.__new__(UPGRADE.Controller)
        controller.intent = future
        self.reject(lambda: UPGRADE.Controller.validate_client_gate(controller))
        result, intent, observations = self.gate()
        self.assertEqual(result["terminal"], intent["client_gate"])
        self.assertEqual(set(result["artifacts"]), {"input", "report", "browser", "after_snapshot"})
        self.assertTrue(result["library_changed_client_acceptance"])
        self.assertFalse(result["full_m3_complete"])
        self.assertEqual(observations, 1)

    def test_client_authority_binds_the_distinct_controller_and_browser_shapes_to_fresh_before_bytes(self):
        result, _, _ = self.gate()
        self.assertTrue(result["library_changed_client_acceptance"])
        self.assertIn(str(UPGRADE.WORK / "client-library-changed-ui-source55-v7/before-full.json"), self.client_gate_reads)
        changes = (
            {"input": lambda value: value["authority"].pop("before_snapshot")},
            {"input": lambda value: value["authority"].update(unknown_field=True)},
            {"report": lambda value: value["authority"].update(unknown_field=True)},
            {"report": lambda value: value["authority"].update(before_snapshot=value["evidence"]["before-full.json"])},
            {"report": lambda value: value["authority"].pop("history")},
            {"report": main_field_change(("authority", "history"), {})},
            {"report": lambda value: value["evidence"].pop("before-full.json")},
            {"report": main_field_change(("evidence", "before-full.json", "sha256"), synthetic_hash("other-before"))},
            {"before_descriptor": main_field_change(("path",), str(UPGRADE.WORK / "other-scope/before-full.json"))},
            {"before_snapshot": main_field_change(("schema",), 27)},
        )
        for index, change in enumerate(changes):
            with self.subTest(authority_change=index):
                self.reject(lambda: self.gate(change))
        self.reject(lambda: self.gate(damage_before=True))

    def test_failed_or_resigned_client_receipts_cannot_supply_run_authority(self):
        cases = {"terminal": [(('marker',), 'foreign-terminal'), (('version',), True), (('status',), 'failed'),
                (('scope',), str(UPGRADE.WORK / 'client-library-changed-ui-source44-v1')), (('recursive_cgroup_empty',), 1),
                (('state', 'MainPID'), '123'), (('state', 'InvocationID'), 'f' * 32), (('full_m3_complete',), True)],
            "report": [(('status',), 'awaiting_outer_attestation'), (('worker_chain_ledger_passed',), 1), (('restoration',), 'pending'),
                (('automatic_retry',), True), (('candidate_or_primary_service_writes',), True), (('ledger', 'new_audits'), 5)],
            "browser": [(('result',), 'failed'), (('library_changed_client_acceptance',), 1), (('closure', 'browser_closed'), False)],
            "controller_intent": [(('controller_invocation',), 'f' * 32)],
            "upgrade_report": [(('status',), 'failed'), (('binary', 'sha256'), UPGRADE.OLD_BINARY_SHA)],
            "upgrade_attestation": [(('status',), 'failed'), (('recursive_cgroup_empty',), False)],
            "state": [(('schema',), True), (('runtime_sha256',), synthetic_hash('foreign-runtime'))],
            "after_snapshot": [(('schema',), 27), (('database', 'unsupported'), True), (('database', 'tables'), {})]}
        for name, fields in cases.items():
            for path, value in fields:
                with self.subTest(document=name, path=path):
                    self.reject(lambda: self.gate({name: main_field_change(path, value)}))


# Main controller memory fixture section.

def main_database_example(name, *, empty=False):
    main = name == UPGRADE.MAIN["database"]
    role_oid, database_oid = (16384, 16385) if main else (994943, 994944)
    properties = {"rolname": name, "rolcanlogin": True, "rolsuper": False, "rolcreatedb": False,
        "rolcreaterole": False, "rolreplication": False, "rolbypassrls": False, "rolinherit": False,
        "rolconnlimit": -1, "rolconfig": None, "rolvaliduntil": None}
    binding = {"version": 1, "deploymentId": UPGRADE.DEPLOYMENT_ID, "generationId": "", "slot": "primary" if main else "recovery"}
    tables = {"schema_migrations": [{"version": index, "name": "synthetic-%02d.sql" % index} for index in range(1, 28)],
        "users": [{"id": "main-admin", "is_administrator": True}],
        "sessions": [{"id": "main-session-%d" % index, "kind": "emby", "user_id": "main-admin"} for index in range(3)],
        "devices": [{"id": "main-device-0"}, {"id": "main-device-1"}],
        "activity_entries": [{"id": index, "created_at": "2099-01-01T00:00:00Z", "action": "session.login"} for index in range(2)],
        "application_keys": [{"id": "main-key"}], "application_key_devices": [{"id": "main-key-device"}],
        "library_roots": [{"id": "main-root", "library_id": "main-movies", "path": "/opt/goby-fixtures/main-test/Movies",
                           "allowed_path": "/opt/goby-fixtures/main-test", "relative_path": "Movies"}],
        "libraries": [{"id": "main-movies", "name": "Synthetic main movies"}],
        "server_settings": [{"key": "goby.recovery.binding.v1", "value": json.dumps(binding, separators=(",", ":"))},
                            {"key": "server_id", "value": "synthetic-main-server"}],
        "scan_jobs": [], "task_runs": [], "task_run_children": [], "encoding_jobs": [{"id": "encoding", "state": "completed"}],
        "client_playback_references": [], "play_sessions": [{"id": "main-play", "state": "Stopped"}],
        "task_definitions": [{"id": "main-task", "enabled": False}],
        "task_triggers": [{"task_id": "main-task", "retired_at": None, "calculation_error": ""}]}
    for index in range(35 - len(tables)):
        tables["synthetic_main_table_%02d" % index] = []
    if empty:
        tables = {}
    columns = {table: list(rows[0]) if rows else ["id"] for table, rows in tables.items()}
    return {"version": 0 if empty else 27, "tables": tables,
        "sequences": {} if empty else {"main_sequence_%d" % index: {"last_value": 30 + index, "log_cnt": 0, "is_called": True} for index in range(5)},
        "catalog": [] if empty else ["synthetic-main-catalog27"],
        "metadata": {"captured_at": "2099-01-01T01:00:00Z", "database": name, "server_version_num": 170011,
            "schemas": ["public"], "public_schema": {"oid": 2200, "owner": 6171, "acl": None},
            "role": {"database_oid": database_oid, "owner_oid": role_oid, "database_acl": None, "properties": properties},
            "columns": columns, "relations": {table: {"oid": 10000 + index, "owner": role_oid, "acl": None, "column_acl": []}
                                               for index, table in enumerate(tables)}}}


def migrated_main_database(before):
    value = copy.deepcopy(before)
    value["version"], value["catalog"] = 28, ["synthetic-main-catalog28"]
    value["metadata"]["captured_at"] = "2099-01-01T01:10:00Z"
    for name, columns in UPGRADE.NEW_COLUMNS.items():
        value["metadata"]["columns"][name].extend(columns)
    for row in value["tables"]["library_roots"]:
        row.update(binding_revision=1, storage_binding=None, bound_at=None, bound_by=None)
    for row in value["tables"]["activity_entries"]:
        row.update(previous_revision=0, observation_fingerprint="")
    value["tables"]["schema_migrations"].append({"version": 28, "name": "0028_storage_root_bindings.sql"})
    return value


class MainMemoryCase(GuardCase):
    """Use real main orchestration, SQL projection, file guards, and publications."""

    def setUp(self):
        super().setUp()
        self.intent = main_intent_example()
        self.output, self.prepared = Path(self.intent["output"]), Path(self.intent["prepared"])
        self.input_root = UPGRADE.WORK / ("main-schema28-source55-inputs-" + self.intent["run_id"])
        self.controller_unit = "goby-main-schema28-source55-" + self.intent["run_id"].replace("_", "-") + ".service"
        self.restart_path = Path("/run/systemd/system") / (UPGRADE.UNIT + ".d") / ("95-main-schema28-" + self.intent["run_id"] + ".conf")
        self.hba_path = Path("/etc/postgresql/17/main/pg_hba.conf")
        self.pg_config = Path("/etc/postgresql/17/main/postgresql.conf")
        self.web_bytes = {"assets/main.js": b"'synthetic main module';\n", "assets/main.css": b"body { color: black; }\n",
            "index.html": b'<script type="module" src="/admin/assets/main.js"></script><link rel="stylesheet" href="/admin/assets/main.css">'}
        published = [UPGRADE.BINARY, *[UPGRADE.WEB / name for name in self.web_bytes]]
        staged = [path.with_name(path.name + ".main28-" + self.intent["run_id"]) for path in published]
        hba_staged = [self.hba_path.with_name("pg_hba.conf.main28-" + self.intent["run_id"] + "-" + phase) for phase in ("install", "restore")]
        self.memory = MemoryFS(self.output, (*published, self.hba_path), (*staged, *hba_staged),
                               (self.prepared, UPGRADE.WEB, self.restart_path.parent))
        self.memory.directory_modes[str(self.prepared)] = 0o700
        self.memory.remove_targets.add(str(self.restart_path))
        self.memory.bind(self)
        self.events, self.sql_writes, self.actions, self.requests, self.connections = [], [], [], [], []
        self.snapshot_statements = []
        self.routes, self.helper_changes, self.http_changes = {}, {}, {}
        self.after_rehearsal = self.after_stop = lambda: None
        self.fail_action = self.helper_failure = None
        self.pid, self.observer_pid, self.invocation = 330001, 330002, "ad" * 16
        self.current = None
        self.running, self.main_process, self.main_invocation = True, copy.deepcopy(UPGRADE.OLD_PROCESS), UPGRADE.OLD_INVOCATION
        self.effective_restart = "on-failure"
        self.new_process = {**UPGRADE.OLD_PROCESS, "pid": 1762090, "start_ticks": UPGRADE.OLD_PROCESS["start_ticks"] + 10000}
        self.databases = {UPGRADE.MAIN["database"]: main_database_example(UPGRADE.MAIN["database"]),
                          UPGRADE.RECOVERY: main_database_example(UPGRADE.RECOVERY)}
        self.initial_main = copy.deepcopy(self.databases[UPGRADE.MAIN["database"]])
        self.initial_inactive = copy.deepcopy(self.databases[UPGRADE.RECOVERY])
        self.pair_role = self.pair_database = None
        self.global_before = {"roles": [{"name": name, "oid": oid, "login": True, "super": False, "createdb": False,
                                        "createrole": False, "inherit": True, "replication": False, "bypass": False,
                                        "limit": -1, "config": None, "valid_until": None, "tag": None,
                                        "verifier_sha256": synthetic_hash(name + "-verifier")}
                                       for name, oid in (("goby_test", 16384), (UPGRADE.RECOVERY, 994943))],
                              "databases": [{"name": name, "oid": oid, "owner": owner, "acl": None, "connect": True,
                                            "limit": -1, "template": False, "tag": None,
                                            "stable": {"datname": name, "datdba": owner, "encoding": 6, "datcollate": "C.UTF-8"}}
                                            for name, oid, owner in (("goby_test", 16385, 16384), (UPGRADE.RECOVERY, 994944, 994943))],
                              "memberships": [], "settings": [], "tablespaces": [{"oid": 1663, "spcname": "pg_default", "spcowner": 10,
                                  "spcacl": None, "spcoptions": None, "location": ""}]}
        self.clock_now, self.retention_eligible = "2099-01-01T01:00:00Z", 0
        self.pg_owner = types.SimpleNamespace(pw_uid=997, pw_gid=997)
        self.catalog27 = {"version": 27, "objects": ["synthetic-main-catalog27"]}
        self.catalog28 = {"version": 28, "objects": ["synthetic-main-catalog28"],
                          "migrations": [{"version": 28, "name": "0028_storage_root_bindings.sql"}]}
        self.old_binary, self.new_binary = b"synthetic original main binary", b"synthetic accepted source55 binary"
        self.enterContext(patch.object(UPGRADE, "sha", self.digest))
        self.enterContext(patch.object(UPGRADE, "__file__", str(UPGRADE.TOOL / "upgrade-main-schema28.py")))
        self.enterContext(patch.object(sys, "platform", "linux"))
        self.enterContext(patch.object(os, "geteuid", lambda: 0))
        self.enterContext(patch.object(os, "getegid", lambda: 0))
        self.enterContext(patch.object(os, "getpid", lambda: self.pid))
        self.enterContext(patch.object(os, "environ", {"SSH_CONNECTION": "synthetic-main-connection", "INVOCATION_ID": self.invocation}))
        self.enterContext(patch.object(secrets, "token_hex", lambda count: "a" * (count * 2)))
        self.enterContext(patch.object(pwd, "getpwnam", lambda name: self.pg_owner if name == "postgres" else denied()))
        self.enterContext(patch.object(time, "monotonic", lambda: 1000.0))
        self.enterContext(patch.object(fcntl, "flock", self.flock))
        self.seed_files()
        self.seed_client_gate()
        self.prepare_intent = copy.deepcopy(self.intent)
        self.prepare_intent.update(main_baseline=None, client_gate=None)
        self.seed_input("prepare", self.prepare_intent)
        self.enterContext(patch.object(UPGRADE, "verify_inputs", self.verified_inputs))
        self.enterContext(patch.object(subprocess, "run", self.subprocess))
        self.enterContext(patch.object(http.client, "HTTPConnection", self.connection))
        load_inputs = UPGRADE.Controller.load_inputs
        def load(controller):
            self.current = controller
            return load_inputs(controller)
        self.enterContext(patch.object(UPGRADE.Controller, "load_inputs", load))
        rehearse = UPGRADE.Controller.rehearse
        def rehearsal(controller):
            result = rehearse(controller)
            self.after_rehearsal()
            return result
        self.enterContext(patch.object(UPGRADE.Controller, "rehearse", rehearsal))

    def tearDown(self):
        self.assertEqual(self.memory.violations, [])
        self.assertEqual(self.memory.handles, {})
        self.assertEqual(self.memory.locked, set())
        self.assertTrue(all(connection.closed for connection in self.connections))

    def digest(self, raw):
        value = hashlib.sha256(raw).hexdigest()
        return self.routes.get(value, value)

    def seed(self, path, raw, digest=None, mode=0o600, uid=0, gid=0):
        self.memory.seed(path, raw, mode)
        entry = self.memory.entries[str(path)]
        entry.update(uid=uid, gid=gid)
        if digest is not None:
            actual = hashlib.sha256(entry["raw"]).hexdigest()
            self.assertTrue(actual not in self.routes or self.routes[actual] == digest)
            self.routes[actual] = digest

    def owned_directory(self, path, uid=0, gid=0, mode=0o700):
        self.memory.seed_directory(path, mode)
        self.memory.entries[str(path)].update(uid=uid, gid=gid, mode=stat.S_IFDIR | mode)

    def seed_input(self, mode, value):
        raw = UPGRADE.canonical(value)
        path = self.input_root / ("prepare.json" if mode == "prepare" else "run.json")
        self.seed(path, raw)
        return types.SimpleNamespace(mode=mode, input=path, input_sha256=self.digest(raw))

    def controller(self, mode):
        return UPGRADE.Controller(self.seed_input(mode, self.prepare_intent if mode == "prepare" else self.intent))

    def prepare_baseline(self):
        result = self.controller("prepare").run()
        self.assertEqual(result["status"], "prepared")
        self.intent["main_baseline"] = copy.deepcopy(result["baseline"])
        return result

    def seed_process(self, value, raw, digest, environ=None):
        root = Path("/proc") / str(value["pid"])
        fields = ["S"] + ["0"] * 19
        fields[19] = str(value["start_ticks"])
        self.seed(root / "stat", (str(value["pid"]) + " (synthetic-process) " + " ".join(fields) + "\n").encode())
        self.seed(root / "cgroup", ("0::" + value["cgroup"] + "\n").encode())
        self.seed(root / "exe", raw, digest, 0o755)
        self.memory.entries[str(root)].update(uid=value["uid"], gid=986)
        self.memory.readlinks[str(root / "exe")] = value["exe"]
        if environ is not None:
            self.seed(root / "environ", b"\0".join((key + "=" + item).encode() for key, item in environ.items()) + b"\0")

    def seed_files(self):
        self.owned_directory(UPGRADE.INSTALL, mode=0o755)
        self.seed(UPGRADE.BINARY, self.old_binary, UPGRADE.OLD_BINARY_SHA, 0o755)
        self.seed(UPGRADE.DEPLOYMENT_LOCK, UPGRADE.DEPLOYMENT_LOCK_BYTES)
        for name, digest in self.intent["source_closure"].items():
            self.seed(UPGRADE.TOOL / name, ("synthetic main tool: " + name).encode(), digest,
                      0o755 if name == "migrate-main-schema28" else 0o600)
        for item in self.intent["historical"]:
            self.seed(Path(item["path"]), ("synthetic sealed provenance: " + item["path"]).encode(), item["sha256"])
        self.inputs = {"catalog": self.catalog28, "binary": self.new_binary, "publication": {"commit": "b" * 40},
            "selected": {UPGRADE.SELECTED[0]: b"package config\n",
                UPGRADE.SELECTED[1]: b"const catalogObjectsSQL = `SELECT '[\"synthetic-main-catalog\"]'::text`;\n",
                UPGRADE.SELECTED[2]: UPGRADE.canonical(self.catalog27), UPGRADE.SELECTED[3]: UPGRADE.canonical(self.catalog28)},
            "source_manifest": {"files": {UPGRADE.SELECTED[2]: UPGRADE.CATALOG27_SHA, UPGRADE.SELECTED[3]: UPGRADE.CATALOG28_SHA}},
            "web": {"files": {name: self.digest(raw) for name, raw in self.web_bytes.items()}},
            "full_terminal": {"unit": UPGRADE.FULL_UNIT, "state": {"MainPID": "0", "InvocationID": UPGRADE.FULL_INVOCATION,
                 "Result": "success", "ExecMainStatus": "0", "SubState": "exited", "ControlGroup": ""}}}
        self.routes[hashlib.sha256(self.new_binary).hexdigest()] = UPGRADE.NEW_BINARY_SHA
        self.inputs["frontend_routes"] = UPGRADE.frontend_routes(self.web_bytes["index.html"], self.inputs["web"]["files"])
        for name, raw in self.inputs["selected"].items():
            expected = self.inputs["source_manifest"]["files"].get(name)
            self.seed(UPGRADE.SOURCE / name, raw, expected)
        for name, raw in self.web_bytes.items():
            self.seed(UPGRADE.WEB_SOURCE / name, raw)
        self.owned_directory(UPGRADE.WEB, mode=0o755)
        self.owned_directory(UPGRADE.WEB / "assets", mode=0o755)
        self.seed(UPGRADE.WEB / "index.html", b"synthetic original main index", mode=0o644)
        self.seed(UPGRADE.WEB / "assets/old-hashed.js", b"synthetic retained old asset", mode=0o644)
        self.env = {"GOBY_DATABASE_URL": "postgresql://goby_test:synthetic-main-password@127.0.0.1:5432/goby_test?sslmode=disable",
            "GOBY_RECOVERY_DATABASE_URL": "postgresql://goby_recovery_m5j:synthetic-recovery-password@127.0.0.1:5432/goby_recovery_m5j?sslmode=disable",
            "GOBY_WEB_DIR": str(UPGRADE.WEB), "GOBY_API_KEY_MASTER_KEY_FILE": str(UPGRADE.MASTER),
            "GOBY_RECOVERY_STATE_DIR": str(UPGRADE.LIFECYCLE), "GOBY_BACKUP_DIR": str(UPGRADE.STORES[2]),
            "GOBY_RECOVERY_OPERATIONS_DIR": str(UPGRADE.STORES[3]), "GOBY_ACTIVITY_RETENTION_DAYS": "30"}
        self.runtime = b"\n".join((key + "='" + value + "'").encode() for key, value in self.env.items()) + b"\n"
        self.seed(UPGRADE.RUNTIME, self.runtime)
        self.seed(UPGRADE.RECOVERY_ENV, b"synthetic preserved recovery environment\n")
        self.seed(UPGRADE.BROWSER, b"synthetic preserved main administrator credentials\n")
        self.seed(UPGRADE.CACHE_OWNER, b"synthetic preserved main transcode cache owner\n", UPGRADE.CACHE_OWNER_SHA)
        self.owned_directory(UPGRADE.CACHE, uid=995, gid=986)
        self.seed(UPGRADE.UNIT_FILE, b"synthetic preserved permanent main unit\n", mode=0o644)
        self.dropins = [UPGRADE.UNIT_FILE.parent / (UPGRADE.UNIT + ".d") / name for name in
                        ("20-application-keys.conf", "30-observability.conf", "40-backup-recovery.conf")]
        for path in self.dropins:
            self.seed(path, ("synthetic " + path.name).encode(), mode=0o644)
        self.master_bytes = b"0123456789abcdef" * 2
        for path in (*UPGRADE.STORES, UPGRADE.PAIRING):
            self.owned_directory(path, uid=995, gid=995 if path == UPGRADE.MASTER.parent else 986)
        self.seed(UPGRADE.MASTER, self.master_bytes, uid=995, gid=995)
        self.seed(UPGRADE.LIFECYCLE_LOCK, b"synthetic existing lifecycle lock", uid=995, gid=986)
        lock = UPGRADE.small_identity(self.memory.info(UPGRADE.LIFECYCLE_LOCK))
        self.seed(UPGRADE.LIFECYCLE / ".goby-lifecycle.json", UPGRADE.canonical({"version": 1, "deploymentId": UPGRADE.DEPLOYMENT_ID,
            "lock": {key: lock[key] for key in ("device", "inode")}}), uid=995, gid=986)
        self.seed(UPGRADE.LIFECYCLE / "generation-registry.json", UPGRADE.canonical({"version": 1, "deploymentId": UPGRADE.DEPLOYMENT_ID,
            "baselineDigest": self.digest(b""), "generations": []}), uid=995, gid=986)
        self.seed(UPGRADE.STORES[3] / "current.json", UPGRADE.canonical({"deploymentId": UPGRADE.DEPLOYMENT_ID, "revision": 5,
            "payload": {"deploymentId": UPGRADE.DEPLOYMENT_ID, "transition": None, "operations": [],
                "slots": [{"slot": "primary", "state": "active"}, {"slot": "recovery", "state": "retained"}]}}), uid=995, gid=986)
        archive_id, archive = "a" * 32, b"synthetic preserved encrypted archive"
        self.seed(UPGRADE.STORES[2] / ("object-" + archive_id + ".age"), archive, uid=995, gid=986)
        self.seed(UPGRADE.STORES[2] / ".goby-backup-catalog.json", UPGRADE.canonical({"entries": [{"deleting": False,
            "metadata": {"id": archive_id, "digest": self.digest(archive), "size": len(archive), "state": "ready", "verified": True}}]}), uid=995, gid=986)
        self.seed(UPGRADE.PAIRING / (archive_id + ".passphrase"), b"synthetic preserved archive passphrase", uid=995, gid=986)
        self.seed(UPGRADE.PAIRING / "backup.json", UPGRADE.canonical({"backup_id": archive_id, "passphrase_file": archive_id + ".passphrase"}), uid=995, gid=986)
        self.owned_directory(UPGRADE.DIAGNOSTICS, uid=995, gid=986)
        self.seed(UPGRADE.DIAGNOSTICS / "main.jsonl", b'{"message":"synthetic original main log"}\n', uid=995, gid=986)
        self.owned_directory(Path("/opt/goby-fixtures/main-test/Movies"), mode=0o755)
        self.seed(Path("/opt/goby-fixtures/main-test/Movies/Synthetic.mkv"), b"synthetic unchanged media", mode=0o644)
        self.seed(self.hba_path, b"synthetic preserved main5432 hba\n", mode=0o640, uid=997, gid=997)
        self.seed(self.pg_config, b"synthetic preserved main5432 configuration\n", mode=0o644, uid=997, gid=997)
        self.seed(UPGRADE.PGDATA / "postmaster.pid", ("60001\n" + str(UPGRADE.PGDATA) + "\n123456\n5432\n/var/run/postgresql\nlocalhost\n0\nready\n").encode(), uid=997, gid=997)
        self.seed(Path("/proc/sys/kernel/random/boot_id"), (UPGRADE.OLD_PROCESS["boot_id"] + "\n").encode())
        self.seed(Path("/proc/1/mountinfo"), b"synthetic unchanged host mount table\n")
        self.memory.readlinks.update({"/proc/self/ns/mnt": "mnt:[synthetic-main]", "/proc/1/ns/mnt": "mnt:[synthetic-main]"})
        self.seed_process({**UPGRADE.OLD_PROCESS, "exe": str(UPGRADE.BINARY), "uid": 995, "cgroup": "/system.slice/" + UPGRADE.UNIT},
                          self.old_binary, UPGRADE.OLD_BINARY_SHA, self.env)
        self.pg_process = {"pid": 60001, "start_ticks": 70001, "boot_id": UPGRADE.OLD_PROCESS["boot_id"],
            "exe": str(UPGRADE.PG / "postgres"), "uid": 997, "cgroup": "/system.slice/postgresql@17-main.service"}
        self.seed_process(self.pg_process, b"synthetic main postgres executable", synthetic_hash("main-postgres"))
        for process in self.intent["protected_services"]:
            self.seed_process(process, ("synthetic protected " + process["name"]).encode(), process["sha256"])
        for unit, raw in ((UPGRADE.UNIT, (str(UPGRADE.OLD_PROCESS["pid"]) + "\n").encode()), (UPGRADE.FULL_UNIT, b""),
                          (self.controller_unit, (str(self.pid) + "\n").encode())):
            self.seed(Path("/sys/fs/cgroup/system.slice") / unit / "cgroup.procs", raw)
        self.owned_directory(self.restart_path.parent.parent, mode=0o755)

    def seed_client_gate(self):
        fixture = main_client_gate_fixture(self.intent, self.inputs["catalog"]["objects"])
        self.intent["client_gate"] = fixture["descriptor"]
        self.client_gate_unit, self.client_gate_state = fixture["unit"], fixture["state"]
        for path, (raw, digest) in fixture["files"].items():
            self.seed(Path(path), raw, digest)
        self.seed(Path("/sys/fs/cgroup/system.slice") / self.client_gate_unit / "cgroup.procs", b"")

    def verified_inputs(self, intent):
        for name, digest in intent["source_closure"].items():
            UPGRADE.protected(UPGRADE.TOOL / name, digest, modes=(0o600, 0o644, 0o755))
        return copy.deepcopy(self.inputs)

    def flock(self, descriptor, operation):
        handle = self.memory.handle(descriptor)
        self.assertEqual(operation, fcntl.LOCK_EX | fcntl.LOCK_NB)
        path = handle["path"]
        self.assertIn(path, (str(UPGRADE.DEPLOYMENT_LOCK), str(UPGRADE.LIFECYCLE), str(UPGRADE.LIFECYCLE_LOCK)))
        if path != str(UPGRADE.DEPLOYMENT_LOCK):
            self.assertFalse(self.running)
            self.assertEqual(self.current.phase_name, "stopped")
        self.memory.locked.add(descriptor)
        self.events.append(("flock", path, descriptor))

    def properties(self, unit):
        if unit == UPGRADE.FULL_UNIT:
            return copy.deepcopy(self.inputs["full_terminal"]["state"])
        if unit == self.client_gate_unit:
            return copy.deepcopy(self.client_gate_state)
        if unit == self.controller_unit:
            result = {"MainPID": "330001", "InvocationID": self.invocation, "ActiveState": "active", "SubState": "running",
                "Description": UPGRADE.MARKER + ":" + self.intent["run_id"], "MemoryMax": str(2 << 30), "MemorySwapMax": "0",
                "CPUQuotaPerSecUSec": "1.500000s", "TasksMax": "128", "RuntimeMaxUSec": "30min", "RemainAfterExit": "yes",
                "KillMode": "control-group", "ControlGroup": "/system.slice/" + unit, "ExecMainStatus": "0", "Result": "success"}
            result.update(getattr(self, "outer_terminal", {}))
            return result
        self.assertEqual(unit, UPGRADE.UNIT)
        result = {name: "synthetic-preserved-value" for name in UPGRADE.SERVICE_PROPERTIES}
        result.update(MainPID=str(self.main_process["pid"]) if self.running else "0", InvocationID=self.main_invocation,
            ActiveState="active" if self.running else "inactive", SubState="running" if self.running else "dead",
            FragmentPath=str(UPGRADE.UNIT_FILE), DropInPaths=" ".join(str(path) for path in self.dropins) +
                (" " + str(self.restart_path) if self.effective_restart == "no" else ""),
            User="goby", Group="goby", WorkingDirectory="/var/lib/goby-test", Restart=self.effective_restart,
            RestartForceExitStatus="", KillMode="control-group", NoNewPrivileges="yes", ProtectSystem="strict",
            EnvironmentFiles=[str(UPGRADE.RUNTIME) + " (ignore_errors=no)", str(UPGRADE.RECOVERY_ENV) + " (ignore_errors=no)"],
            ControlGroup="/system.slice/" + UPGRADE.UNIT if self.running else "", TimeoutStopUSec="1min 30s", UMask="0077")
        result.update(getattr(self, "main_property_changes", {}))
        return result

    def subprocess(self, arguments, **kwargs):
        args = list(arguments)
        self.events.append(("command", tuple(args)))
        self.assertIsNotNone(self.current.lock)
        self.assertIn(self.current.lock, self.memory.locked)
        output = b""
        if args[:2] == ["/usr/bin/systemctl", "show"]:
            self.assertEqual(len(args), 5)
            self.assertEqual(args[3], "--no-pager")
            selected = self.properties(args[2])
            lines = []
            for name in args[4].removeprefix("--property=").split(","):
                value = selected[name]
                lines.extend(name + "=" + item for item in value) if name == "EnvironmentFiles" else lines.append(name + "=" + value)
            output = "\n".join(lines).encode()
        elif args == ["/usr/bin/systemctl", "daemon-reload"]:
            install = self.current.fence.stage == "restart_fence_install"
            self.assertIn(self.current.fence.stage, ("restart_fence_install", "restart_fence_restore"))
            action = "reload-install" if install else "reload-restore"
            self.actions.append(action)
            if self.fail_action == action:
                return types.SimpleNamespace(returncode=1, stdout=b"", stderr=b"synthetic private reload error")
            self.assertEqual(str(self.restart_path) in self.memory.entries, install)
            self.effective_restart = "no" if install else "on-failure"
        elif args[:2] in (["/usr/bin/systemctl", "stop"], ["/usr/bin/systemctl", "start"]):
            action = args[1]
            self.assertEqual(args, ["/usr/bin/systemctl", action, UPGRADE.UNIT])
            self.assertEqual(self.current.phase_name, action + "_requested")
            self.assertEqual(self.current.fence.actions[-1], action)
            phase = self.output_json("phase-" + action + "-requested.json")
            self.assertEqual(phase["phase"], action + "_requested")
            self.assertEqual(self.effective_restart, "no")
            self.actions.append(action)
            if self.fail_action == action:
                return types.SimpleNamespace(returncode=1, stdout=b"", stderr=b"synthetic private service error")
            if action == "stop":
                self.assertTrue(self.running)
                self.assertEqual(kwargs["timeout"], 120)
                self.running = False
                old = Path("/proc") / str(self.main_process["pid"])
                for path in [name for name in self.memory.entries if Path(name) == old or Path(name).is_relative_to(old)]:
                    del self.memory.entries[path]
                self.after_stop()
            else:
                self.assertFalse(self.running)
                self.assertEqual(kwargs["timeout"], 60)
                self.assertEqual(self.current.lifecycle_fds, [])
                self.assertTrue(all(self.memory.handle(fd)["path"] == str(UPGRADE.DEPLOYMENT_LOCK) for fd in self.memory.locked))
                self.assertEqual(self.memory.raw(UPGRADE.BINARY), self.new_binary)
                self.running, self.main_process, self.main_invocation = True, copy.deepcopy(self.new_process), "bd" * 16
                self.seed_process({**self.main_process, "exe": str(UPGRADE.BINARY), "uid": 995, "cgroup": "/system.slice/" + UPGRADE.UNIT},
                                  self.new_binary, UPGRADE.NEW_BINARY_SHA, self.env)
            group = Path("/sys/fs/cgroup/system.slice") / UPGRADE.UNIT / "cgroup.procs"
            self.memory.entries[str(group)]["raw"] = (str(self.main_process["pid"]) + "\n").encode() if self.running else b""
        elif args == ["/usr/bin/ss", "-H", "-ltnp", "sport = :18096"]:
            output = ("LISTEN 0 128 127.0.0.1:18096 0.0.0.0:* users:((\"goby\",pid=%d,fd=1))" % self.main_process["pid"]).encode() if self.running else b""
        elif args[0] == str(UPGRADE.TOOL / "migrate-main-schema28"):
            if self.helper_failure is not None:
                return types.SimpleNamespace(returncode=1, stdout=self.helper_failure, stderr=b"synthetic-private-main-password")
            self.helper_command(args, kwargs.get("pass_fds", ()))
        elif args[:5] == ["/usr/sbin/runuser", "-u", "postgres", "--", str(UPGRADE.PG / "psql")]:
            self.assertEqual(args[args.index("-p") + 1], "5432")
            self.assertEqual(args[args.index("-U") + 1], "postgres")
            self.assertEqual(args[args.index("-h") + 1], "/var/run/postgresql")
            database = args[args.index("-d") + 1]
            statement = kwargs["input"].decode().strip()
            write = statement in self.current.allowed_ddl()
            self.assertEqual("default_transaction_read_only=on" in kwargs["env"]["PGOPTIONS"], not write)
            output = self.sql_response(statement, database, write)
        else:
            self.memory.violation("An undeclared main transport command was attempted.")
        return types.SimpleNamespace(returncode=0, stdout=output, stderr=b"")

    def current_global(self):
        value = copy.deepcopy(self.global_before)
        if self.pair_role is not None:
            value["roles"].append(copy.deepcopy(self.pair_role))
        if self.pair_database is not None:
            value["databases"].append({"name": self.current.rehearsal_name, **copy.deepcopy(self.pair_database)})
        return value

    def sql_response(self, statement, database, write):
        self.assertIn(database, ("postgres", UPGRADE.MAIN["database"], UPGRADE.RECOVERY, self.current.rehearsal_name))
        self.events.append(("sql", database, "write" if write else "read"))
        if write:
            self.assertEqual(database, "postgres")
            self.assertNotIn("FORCE", statement)
            self.assertNotIn("CASCADE", statement)
            self.sql_writes.append(statement)
            name = self.current.rehearsal_name
            if statement.startswith("BEGIN; SET LOCAL password_encryption"):
                self.assertIsNone(self.pair_role)
                self.pair_role = {"oid": 88001, "name": name, "login": True, "super": False, "createdb": False, "createrole": False,
                    "replication": False, "bypass": False, "inherit": False, "limit": 12, "config": None, "valid_until": None,
                    "tag": UPGRADE.MARKER + ":" + self.intent["run_id"], "verifier_sha256": synthetic_hash("fresh-main-rehearsal-verifier")}
            elif statement.startswith("CREATE DATABASE "):
                self.assertIsNone(self.pair_database)
                self.pair_database = {"oid": 88002, "owner": 88001, "allow": True, "acl": None, "tag": None, "grants": []}
                value = main_database_example(name, empty=True)
                value["metadata"]["role"].update(database_oid=88002, owner_oid=88001)
                value["metadata"]["public_schema"].update(oid=88003, owner=6171)
                self.databases[name] = value
            elif statement.startswith("COMMENT ON DATABASE "):
                self.pair_database.update(tag=UPGRADE.MARKER + ":" + self.intent["run_id"], acl=[name + "=CTc/" + name],
                    grants=[{"grantor": 88001, "grantee": 88001, "privilege": privilege, "grantable": False}
                            for privilege in ("CREATE", "CONNECT", "TEMPORARY")])
            elif statement.startswith("ALTER DATABASE "):
                self.pair_database["allow"] = False
            elif statement.startswith("DROP DATABASE "):
                self.assertFalse(self.pair_database["allow"])
                del self.databases[name]
                self.pair_database = None
            elif statement.startswith("DROP ROLE "):
                self.assertIsNone(self.pair_database)
                self.pair_role = None
            else:
                self.memory.violation("An unmodeled rehearsal mutation was attempted.")
            return b""
        if statement == UPGRADE.GLOBAL_SQL.strip():
            return UPGRADE.canonical(self.current_global())
        if "'system_identifier'" in statement and "pg_control_system()" in statement:
            return UPGRADE.canonical({"system_identifier": UPGRADE.SYSTEM_IDENTIFIER, "port": 5432, "version": 170011,
                "data": str(UPGRADE.PGDATA), "hba_file": str(self.hba_path), "config_file": str(self.pg_config),
                "socket": "/var/run/postgresql", "start_microseconds": 123456000000})
        if "'has_migrations'" in statement:
            value = self.databases[database]
            return UPGRADE.canonical({"version": value["version"], "has_migrations": value["version"] > 0,
                                     "role_oid": value["metadata"]["role"]["owner_oid"]})
        if statement == "SELECT COALESCE(max(version),0) FROM public.schema_migrations;":
            return str(self.databases[database]["version"]).encode()
        if statement.startswith("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;"):
            value = self.databases[database]
            self.snapshot_statements.append(statement)
            text = UPGRADE.canonical(value["catalog"]).decode().strip()
            expression = "SELECT jsonb_build_object('section','catalog','value',(SELECT '[\"synthetic-main-catalog\"]'::text)::jsonb);"
            # PostgreSQL keeps the text-valued subquery as a JSON string unless
            # the generated snapshot statement casts that value to jsonb.
            catalog_value = UPGRADE.decode(text.encode()) if expression in statement else text
            catalog_value = getattr(self, "catalog_transport_override", catalog_value)
            rows = [{"section": "metadata", "value": value["metadata"]}, {"section": "catalog", "value": catalog_value}]
            rows += [{"section": "table", "name": name, "value": rows} for name, rows in value["tables"].items()]
            rows += [{"section": "sequence", "name": name, "value": rows} for name, rows in value["sequences"].items()]
            return b"".join(UPGRADE.canonical(row) for row in rows)
        if statement.startswith("SELECT EXISTS("):
            return b"f"
        if "'eligible'" in statement:
            return UPGRADE.canonical({"now": self.clock_now, "eligible": self.retention_eligible})
        if statement.startswith("SELECT jsonb_build_object('role',"):
            return UPGRADE.canonical({"role": self.pair_role, "database": self.pair_database})
        if statement == "SELECT pg_reload_conf();":
            self.events.append(("hba-reload",))
            return b"t"
        if statement.startswith("SELECT count(*) FROM pg_hba_file_rules") or statement.startswith("SELECT (SELECT count(*)") or statement.startswith("SELECT count(*) FROM pg_stat_activity"):
            return b"0"
        self.memory.violation("An undeclared read-only main SQL statement was attempted.")

    def helper_command(self, args, pass_fds):
        options = dict(zip(args[1::2], args[2::2]))
        self.assertEqual(len(options) * 2 + 1, len(args))
        mode, output = options["--mode"], Path(options["--output"])
        self.assertEqual(output.parent, self.output / "private")
        raw = self.memory.raw(self.output / "private/helper-intent.json")
        self.assertEqual(options["--intent-sha256"], self.digest(raw))
        helper_intent = UPGRADE.decode(raw)
        self.assertEqual(helper_intent["main"]["database"], "goby_test")
        self.assertEqual(helper_intent["cluster"]["port"], 5432)
        self.events.append(("helper", mode, output.name))
        if mode == "migrate":
            self.assertFalse(self.running)
            self.assertEqual(tuple(self.current.lifecycle_fds), tuple(pass_fds))
            self.assertEqual([self.memory.handle(fd)["path"] for fd in pass_fds], [str(UPGRADE.LIFECYCLE), str(UPGRADE.LIFECYCLE_LOCK)])
            self.assertTrue(all(fd in self.memory.locked for fd in pass_fds))
            self.assertEqual([int(options[key]) for key in ("--lifecycle-directory-fd", "--lifecycle-lock-fd")], list(pass_fds))
            self.databases[UPGRADE.MAIN["database"]] = migrated_main_database(self.initial_main)
        else:
            self.assertEqual(tuple(pass_fds), ())
            self.assertNotIn("--lifecycle-directory-fd", options)
            self.assertNotIn("--lifecycle-lock-fd", options)
            if mode == "rehearse":
                self.assertTrue(self.running)
                value = migrated_main_database(self.initial_main)
                value["metadata"]["database"] = self.current.rehearsal_name
                value["metadata"]["role"].update(database_oid=88002, owner_oid=88001)
                value["metadata"]["role"]["properties"]["rolname"] = self.current.rehearsal_name
                value["metadata"]["public_schema"] = copy.deepcopy(self.databases[self.current.rehearsal_name]["metadata"]["public_schema"])
                for index, relation in enumerate(value["metadata"]["relations"].values()):
                    relation.update(oid=20000 + index, owner=88001)
                self.databases[self.current.rehearsal_name] = value
        value = {"marker": "goby-main-schema28-helper-v1", "version": 1, "mode": mode, "run_id": self.intent["run_id"],
            "intent_sha256": self.digest(raw), "source_manifest_sha256": UPGRADE.SOURCE_SHA, "source55_binary_sha256": UPGRADE.NEW_BINARY_SHA,
            "helper_sha256": self.intent["helper"]["binary"]["sha256"], "source_schema_version": 27, "target_schema_version": 28,
            "status": {"backup": "backed_up", "inspect": "inspected", "rehearse": "rehearsed", "migrate": "committed"}[mode],
            "state_sha256": synthetic_hash("main-exported-backup-state")}
        if mode == "backup":
            data, path = b"PGDMP-synthetic-main-snapshot", self.output / "private/main-schema27.dump"
            UPGRADE.create(path, data)
            value.update(same_exported_snapshot=True, dump={"path": str(path), "sha256": self.digest(data), "bytes": len(data)})
        else:
            self.assertEqual(options["--baseline-sha256"], self.current.backup_sha)
            value["baseline_sha256"] = self.current.backup_sha
            if mode != "inspect":
                value.update(before_state_sha256=synthetic_hash("main-exported-backup-state"), preserved_state_sha256=synthetic_hash("main-exported-backup-state"),
                             root_bindings_no_auto_binding=True, historical_audit_defaults=True)
        if mode == "migrate":
            value["lifecycle_fence_verified"] = True
        change = self.helper_changes.get(output.name) or self.helper_changes.get(mode)
        if change:
            change(value)
        UPGRADE.create(output, UPGRADE.canonical(value))

    def connection(self, host, port, timeout):
        self.assertEqual((host, port, timeout), ("127.0.0.1", 18096, 15))
        connection = types.SimpleNamespace(route=None, closed=False)
        def request(method, route, body=None, headers=None):
            routes = ["/readyz", "/emby/System/Info/Public", "/admin/", "/admin/assets/main.js", "/admin/assets/main.css"]
            self.assertLess(len(self.requests), 5)
            self.assertEqual((method, route, body), ("GET", routes[len(self.requests)], None))
            self.assertEqual(headers, {"Accept": "*/*", "Accept-Encoding": "identity", "Connection": "close"})
            connection.route = route
            self.requests.append(route)
        def response():
            route = connection.route
            raw = b'{"Status":"ready"}' if route == "/readyz" else UPGRADE.canonical({"Id": "synthetic-main-server", "ProductName": "Goby", "Version": "4.9.5.0"}) if route == "/emby/System/Info/Public" else self.web_bytes["index.html" if route == "/admin/" else route.removeprefix("/admin/")]
            changes = self.http_changes.get(route, {})
            raw = changes.get("body", raw)
            return types.SimpleNamespace(status=changes.get("status", 200), read=lambda limit: raw[:limit], getheader=lambda _name: changes.get("cookie"))
        connection.request, connection.getresponse = request, response
        connection.close = lambda: setattr(connection, "closed", True)
        self.connections.append(connection)
        return connection

    def output_json(self, name):
        return UPGRADE.decode(self.memory.raw(self.output / name))

    def prepare_attestation(self):
        self.pid = self.observer_pid
        self.outer_terminal = {"MainPID": "0", "SubState": "exited", "ControlGroup": ""}
        self.memory.entries[str(Path("/sys/fs/cgroup/system.slice") / self.controller_unit / "cgroup.procs")]["raw"] = b""
        return self.seed_input("attest", self.intent)

    def assert_no_migration(self):
        self.assertFalse(any(event[:2] == ("helper", "migrate") for event in self.events))
        self.assertEqual(self.databases[UPGRADE.MAIN["database"]]["version"], 27)
        self.assertNotIn("start", self.actions)

    def mutate_main_sequence(self):
        self.databases[UPGRADE.MAIN["database"]]["sequences"]["main_sequence_0"]["last_value"] += 1

    def complete_run(self):
        self.prepare_baseline()
        result = self.controller("run").run()
        self.assertEqual(result["status"], "awaiting_outer_attestation")
        return self.prepare_attestation()


class MainReadOnlyFlowGuards(MainMemoryCase):
    def test_actual_snapshot_casts_text_catalog_to_json_array_before_complete_comparison(self):
        result = self.controller("prepare").run()
        self.assertEqual(result["status"], "prepared")
        self.assertTrue(self.snapshot_statements)
        for statement in self.snapshot_statements:
            self.assertIn("::text)::jsonb);", statement)
        baseline = UPGRADE.decode(self.memory.raw(self.prepared / "baseline.json"))["captured"]
        self.assertIsInstance(baseline["database"]["catalog"], list)
        self.assertEqual(baseline["database"]["catalog"], self.catalog27["objects"])
        self.assertEqual(self.sql_writes, [])
        self.assertEqual(self.actions, [])

    def test_actual_snapshot_rejects_string_null_object_and_changed_array_catalog(self):
        values = (UPGRADE.canonical(self.catalog27["objects"]).decode().strip(), None, {}, ["changed-catalog-object"])
        for value in values:
            with self.subTest(catalog_type=type(value).__name__):
                self.catalog_transport_override = value
                self.reject(lambda: self.controller("prepare").run())
                self.assertNotIn(str(self.prepared), self.memory.entries)
                self.assertNotIn(str(self.output), self.memory.entries)
        self.assertEqual(self.sql_writes, [])
        self.assertEqual(self.actions, [])

    def test_actual_prepare_has_no_future_gate_and_writes_only_new_private_evidence(self):
        args = self.seed_input("prepare", self.prepare_intent)
        before = copy.deepcopy(self.memory.entries)
        result = UPGRADE.Controller(args).run()
        self.assertEqual(result["client_gate_checked"], False)
        self.assertEqual((result["business_sql_writes"], result["service_writes"], result["hba_writes"]), (0, 0, 0))
        self.assertEqual(self.sql_writes, [])
        self.assertEqual(self.actions, [])
        self.assertEqual(self.requests, [])
        self.assertNotIn(str(self.output), self.memory.entries)
        for path, entry in before.items():
            self.assertEqual(self.memory.entries[path], entry)
        added = set(self.memory.entries) - set(before)
        self.assertEqual(added, {str(self.prepared), str(self.prepared / "baseline.json"), str(self.prepared / "report.json")})
        self.assertEqual(self.events[0][:2], ("flock", str(UPGRADE.DEPLOYMENT_LOCK)))
        baseline = UPGRADE.decode(self.memory.raw(self.prepared / "baseline.json"))["captured"]
        self.assertEqual(len(baseline["database"]["tables"]["sessions"]), 3)
        self.assertEqual(baseline["inactive"]["metadata"]["role"]["database_oid"], 994944)
        self.assertEqual(baseline["private"]["files"][str(UPGRADE.MASTER)]["identity"]["gid"], 995)
        self.assertEqual(baseline["private"]["trees"][str(UPGRADE.LIFECYCLE)]["identity"]["gid"], 986)

    def test_historical_deployment_lock_is_required_before_any_query_or_private_read(self):
        self.memory.entries[str(UPGRADE.DEPLOYMENT_LOCK)]["raw"] = b"foreign deployment lock\n"
        self.reject(lambda: self.controller("prepare").run())
        self.assertEqual(self.events, [])
        reads = {entry[1] for entry in self.memory.attempts if entry[0] in ("read", "read_fd")}
        self.assertNotIn(str(UPGRADE.MASTER), reads)
        self.assertNotIn(str(self.prepared), self.memory.entries)

    def test_actual_preflight_rechecks_passing_gate_and_fresh_main_baseline_without_writes(self):
        self.prepare_baseline()
        before = copy.deepcopy(self.memory.entries)
        args = self.seed_input("preflight", self.intent)
        before[str(args.input)] = copy.deepcopy(self.memory.entries[str(args.input)])
        result = UPGRADE.Controller(args).run()
        self.assertEqual(result["status"], "preflight_passed")
        self.assertTrue(result["client_gate"]["library_changed_client_acceptance"])
        self.assertEqual(self.memory.entries, before)
        self.assertEqual(self.sql_writes, [])
        self.assertEqual(self.actions, [])

    def test_run_without_the_real_client_gate_is_rejected_before_output_or_service(self):
        self.prepare_baseline()
        self.intent["client_gate"] = None
        self.reject(lambda: self.controller("run").run())
        self.assertNotIn(str(self.output), self.memory.entries)
        self.assertEqual(self.sql_writes, [])
        self.assertEqual(self.actions, [])

    def test_prepared_baseline_rejects_dynamic_row_sequence_inactive_and_global_identity_drift(self):
        self.prepare_baseline()
        original_main, original_inactive, original_global = copy.deepcopy(self.initial_main), copy.deepcopy(self.initial_inactive), copy.deepcopy(self.global_before)
        changes = (
            lambda: self.databases[UPGRADE.MAIN["database"]]["tables"]["sessions"].append({"id": "late-main-session", "kind": "emby", "user_id": "main-admin"}),
            self.mutate_main_sequence,
            lambda: self.databases[UPGRADE.RECOVERY]["tables"]["users"][0].update(id="changed-inactive-user"),
            lambda: self.global_before["roles"][0].update(verifier_sha256=synthetic_hash("changed-main-verifier")),
            lambda: self.global_before["tablespaces"][0].update(spcowner=9999),
        )
        for change in changes:
            with self.subTest(change=changes.index(change)):
                change()
                self.reject(lambda: self.controller("preflight").run())
                self.databases[UPGRADE.MAIN["database"]] = copy.deepcopy(original_main)
                self.databases[UPGRADE.RECOVERY] = copy.deepcopy(original_inactive)
                self.global_before = copy.deepcopy(original_global)
        self.assertEqual(self.actions, [])
        self.assertEqual(self.sql_writes, [])

    def test_master_is_mandatory_with_its_distinct_owner_and_stores_keep_their_group(self):
        self.prepare_baseline()
        original = copy.deepcopy(self.memory.entries[str(UPGRADE.MASTER)])
        for field, value in (("gid", 986), ("mode", stat.S_IFREG | 0o4600), ("mode", stat.S_IFREG | 0o644), ("raw", b"short"), ("links", 2)):
            with self.subTest(field=field):
                self.memory.entries[str(UPGRADE.MASTER)] = {**original, field: value}
                self.reject(lambda: self.controller("preflight").run())
        del self.memory.entries[str(UPGRADE.MASTER)]
        with self.assertRaises(FileNotFoundError):
            self.controller("preflight").run()
        self.memory.entries[str(UPGRADE.MASTER)] = original
        self.memory.entries[str(UPGRADE.LIFECYCLE_LOCK)]["gid"] = 995
        self.reject(lambda: self.controller("preflight").run())
        self.assertEqual(self.actions, [])

    def test_actual_service_reader_preserves_both_environment_file_lines_and_rejects_repeats(self):
        self.prepare_baseline()
        correct = self.properties(UPGRADE.UNIT)["EnvironmentFiles"]
        for value in (correct[:1], list(reversed(correct)), correct + correct[:1]):
            with self.subTest(lines=value):
                self.main_property_changes = {"EnvironmentFiles": value}
                self.reject(lambda: self.controller("preflight").run())
        self.assertEqual(self.actions, [])

    def test_startup_deadline_retention_and_active_work_are_actual_read_only_gates(self):
        self.prepare_baseline()
        self.retention_eligible = 1
        self.reject(lambda: self.controller("preflight").run())
        self.retention_eligible, self.clock_now = 0, "2099-01-01T01:50:00Z"
        self.reject(lambda: self.controller("preflight").run())
        self.clock_now = "2099-01-01T01:00:00Z"
        self.databases[UPGRADE.MAIN["database"]]["tables"]["scan_jobs"] = [{"status": "running"}]
        self.reject(lambda: self.controller("preflight").run())
        self.assertEqual(self.sql_writes, [])
        self.assertEqual(self.actions, [])

    def test_generation_registry_and_primary_binding_cannot_switch_during_admission(self):
        self.prepare_baseline()
        registry_path = UPGRADE.LIFECYCLE / "generation-registry.json"
        raw = self.memory.raw(registry_path)
        registry = UPGRADE.decode(raw)
        registry["generations"] = [{"generationId": "unexpected-new-generation"}]
        self.memory.entries[str(registry_path)]["raw"] = UPGRADE.canonical(registry)
        self.reject(lambda: self.controller("preflight").run())
        self.memory.entries[str(registry_path)]["raw"] = raw
        rows = self.databases[UPGRADE.MAIN["database"]]["tables"]["server_settings"]
        binding = next(row for row in rows if row["key"] == "goby.recovery.binding.v1")
        value = json.loads(binding["value"])
        value["slot"] = "recovery"
        binding["value"] = json.dumps(value, separators=(",", ":"))
        self.reject(lambda: self.controller("preflight").run())
        self.assertEqual(self.actions, [])
        self.assertEqual(self.sql_writes, [])


class MainActualRunGuards(MainMemoryCase):
    def test_actual_run_preserves_main_and_inactive_state_with_assets_before_index_before_binary(self):
        self.prepare_baseline()
        old_asset = copy.deepcopy(self.memory.entries[str(UPGRADE.WEB / "assets/old-hashed.js")])
        original_private = {key: copy.deepcopy(value) for key, value in self.memory.entries.items()
                            if any(Path(key) == root or Path(key).is_relative_to(root) for root in (*UPGRADE.STORES, UPGRADE.PAIRING))}
        result = self.controller("run").run()
        self.assertEqual(result["status"], "awaiting_outer_attestation")
        self.assertEqual(self.actions, ["reload-install", "stop", "start", "reload-restore"])
        self.assertEqual(self.databases[UPGRADE.RECOVERY], self.initial_inactive)
        UPGRADE.compare_database(self.initial_main, self.databases[UPGRADE.MAIN["database"]], self.catalog28)
        self.assertEqual(self.current_global(), self.global_before)
        self.assertEqual(self.memory.raw(UPGRADE.RUNTIME), self.runtime)
        self.assertEqual(self.memory.entries[str(UPGRADE.WEB / "assets/old-hashed.js")], old_asset)
        self.assertEqual({key: self.memory.entries[key] for key in original_private}, original_private)
        replacements = [Path(event[2]) for event in self.memory.events if event[0] == "replace" and event[2] != str(self.hba_path)]
        self.assertEqual(replacements, [UPGRADE.WEB / "assets/main.css", UPGRADE.WEB / "assets/main.js", UPGRADE.WEB / "index.html", UPGRADE.BINARY])
        for path in (UPGRADE.WEB, UPGRADE.WEB / "assets"):
            self.assertEqual(stat.S_IMODE(self.memory.info(path).st_mode), 0o755)
        for name in (*self.web_bytes, "assets/old-hashed.js"):
            self.assertEqual(stat.S_IMODE(self.memory.info(UPGRADE.WEB / name).st_mode), 0o644)
        self.assertEqual(stat.S_IMODE(self.memory.info(UPGRADE.BINARY).st_mode), 0o755)
        self.assertEqual(self.memory.raw(self.hba_path), b"synthetic preserved main5432 hba\n")
        self.assertEqual(len(self.sql_writes), 6)
        self.assertTrue(all("FORCE" not in query and "CASCADE" not in query for query in self.sql_writes))
        self.assertEqual(self.pair_role, None)
        self.assertEqual(self.pair_database, None)
        inspections = [event[2] for event in self.events if event[:2] == ("helper", "inspect")]
        self.assertEqual(inspections, ["helper-before-fence.json", "helper-pre-stop.json", "helper-stopped.json"])
        self.assertEqual(len(self.requests), 5)
        self.assertNotIn(str(self.restart_path), self.memory.entries)
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)
        report = self.output_json("report.json")
        self.assertTrue(all(report["preserved"].values()) and all(report["cleanup"].values()))
        self.assertEqual(report["main_process"], self.new_process)
        self.assertFalse(report["main_client_acceptance"])
        self.assertFalse(report["automatic_retry"] or report["automatic_rollback"])
        material = UPGRADE.decode(self.memory.raw(self.output / "private/materials.json"))[str(UPGRADE.MASTER)]
        self.assertEqual(self.memory.raw(material["path"]), self.master_bytes)
        self.assertEqual(material["original_identity"]["gid"], 995)

    def test_post_rehearsal_row_drift_is_rejected_before_restart_fence_or_stop(self):
        self.prepare_baseline()
        self.after_rehearsal = self.mutate_main_sequence
        self.reject(lambda: self.controller("run").run())
        self.assertEqual(self.actions, [])
        self.assert_no_migration()
        self.assertEqual(self.pair_role, None)
        self.assertEqual(self.pair_database, None)
        self.assertFalse(self.output_json("failed.json")["automatic_retry"])

    def test_second_baseline_after_restart_fence_rejects_drift_before_stop(self):
        self.prepare_baseline()
        install = UPGRADE.Controller.install_restart_fence
        def fence(controller):
            result = install(controller)
            self.mutate_main_sequence()
            return result
        with patch.object(UPGRADE.Controller, "install_restart_fence", fence):
            self.reject(lambda: self.controller("run").run())
        self.assertEqual(self.actions, ["reload-install"])
        self.assert_no_migration()
        self.assertTrue(self.output_json("failed.json")["restart_fence_retained"])

    def test_stopped_drift_blocks_migration_and_does_not_start_or_rollback(self):
        self.prepare_baseline()
        self.after_stop = self.mutate_main_sequence
        self.reject(lambda: self.controller("run").run())
        self.assertEqual(self.actions, ["reload-install", "stop"])
        self.assert_no_migration()
        self.assertFalse(self.running)
        self.assertEqual([event for event in self.events if event[0] == "flock" and event[1] != str(UPGRADE.DEPLOYMENT_LOCK)], [])
        failed = self.output_json("failed.json")
        self.assertTrue(failed["restart_fence_retained"])
        self.assertFalse(failed["automatic_rollback"] or failed["automatic_retry"])

    def test_exported_backup_proof_is_checked_by_actual_helper_before_stop(self):
        self.prepare_baseline()
        self.helper_changes["helper-pre-stop.json"] = lambda value: value.update(state_sha256=synthetic_hash("changed-exported-state"))
        self.reject(lambda: self.controller("run").run())
        self.assertEqual(self.actions, ["reload-install"])
        self.assert_no_migration()

    def test_stopped_exported_backup_inspection_is_distinct_and_required(self):
        self.prepare_baseline()
        self.helper_changes["helper-stopped.json"] = lambda value: value.update(baseline_sha256=synthetic_hash("another-backup"))
        self.reject(lambda: self.controller("run").run())
        self.assertEqual(self.actions, ["reload-install", "stop"])
        self.assert_no_migration()

    def test_migration_requires_both_inherited_lifecycle_fences_and_releases_them_on_failure(self):
        self.prepare_baseline()
        self.helper_changes["migrate"] = lambda value: value.update(lifecycle_fence_verified=False)
        self.reject(lambda: self.controller("run").run())
        locks = [event[1] for event in self.events if event[0] == "flock" and event[1] != str(UPGRADE.DEPLOYMENT_LOCK)]
        self.assertEqual(locks, [str(UPGRADE.LIFECYCLE), str(UPGRADE.LIFECYCLE_LOCK)])
        self.assertEqual(self.current.lifecycle_fds, [])
        self.assertEqual(self.actions, ["reload-install", "stop"])
        self.assertEqual(self.memory.raw(UPGRADE.BINARY), self.old_binary)
        self.assertNotIn(str(self.output / "report.json"), self.memory.entries)

    def test_busy_named_lifecycle_lock_releases_the_directory_without_migration_or_start(self):
        self.prepare_baseline()
        original = self.flock
        def busy(descriptor, operation):
            if self.memory.handle(descriptor)["path"] == str(UPGRADE.LIFECYCLE_LOCK):
                raise BlockingIOError(errno.EWOULDBLOCK, "Synthetic lifecycle lock is already held.")
            return original(descriptor, operation)
        with patch.object(fcntl, "flock", busy):
            with self.assertRaises(BlockingIOError):
                self.controller("run").run()
        self.assertEqual(self.current.lifecycle_fds, [])
        self.assertEqual(self.actions, ["reload-install", "stop"])
        self.assert_no_migration()
        self.assertTrue(self.output_json("failed.json")["restart_fence_retained"])

    def test_hba_restore_is_reserved_before_replace_and_never_retried(self):
        self.prepare_baseline()
        restore = self.hba_path.with_name("pg_hba.conf.main28-" + self.intent["run_id"] + "-restore")
        self.memory.fail_replace = str(restore)
        self.reject(lambda: self.controller("run").run())
        attempts = [entry for entry in self.memory.attempts if entry[:2] == ("replace", str(restore))]
        self.assertEqual(len(attempts), 1)
        self.assertTrue(self.current.hba_restore_reserved)
        self.assertTrue(self.current.hba_changed)
        self.assertIsNotNone(self.pair_role)
        self.assertIsNotNone(self.pair_database)
        self.assertEqual(self.actions, [])
        self.assert_no_migration()

    def test_one_stop_failure_is_retained_without_restart_or_service_retry(self):
        self.prepare_baseline()
        self.fail_action = "stop"
        self.reject(lambda: self.controller("run").run())
        self.assertEqual(self.actions, ["reload-install", "stop"])
        self.assert_no_migration()
        failed = self.output_json("failed.json")
        self.assertEqual(failed["error_code"], "command_failed")
        self.assertTrue(failed["restart_fence_retained"])
        self.assertNotIn("synthetic private service error", UPGRADE.canonical(failed).decode())

    def test_wrong_javascript_bytes_stop_at_four_anonymous_requests_and_retain_fence(self):
        self.prepare_baseline()
        self.http_changes["/admin/assets/main.js"] = {"body": b"foreign main javascript"}
        self.reject(lambda: self.controller("run").run())
        self.assertEqual(len(self.requests), 4)
        self.assertEqual(self.actions, ["reload-install", "stop", "start"])
        self.assertTrue(self.output_json("failed.json")["restart_fence_retained"])
        self.assertNotIn(str(self.output / "report.json"), self.memory.entries)

    def test_wrong_stylesheet_bytes_stop_at_five_requests_without_retry(self):
        self.prepare_baseline()
        self.http_changes["/admin/assets/main.css"] = {"body": b"foreign main stylesheet"}
        self.reject(lambda: self.controller("run").run())
        self.assertEqual(len(self.requests), 5)
        self.assertEqual(self.actions, ["reload-install", "stop", "start"])
        self.assertNotIn(str(self.output / "report.json"), self.memory.entries)

    def test_diagnostics_append_is_bounded_and_old_bytes_remain_preserved(self):
        self.prepare_baseline()
        def append():
            self.memory.entries[str(UPGRADE.DIAGNOSTICS / "main.jsonl")]["raw"] += b'{"message":"synthetic new main log"}\n'
        self.after_rehearsal = append
        result = self.controller("run").run()
        self.assertEqual(result["status"], "awaiting_outer_attestation")
        logs = self.output_json("after-full.json")["logs"]
        self.assertGreater(logs["files"]["main.jsonl"]["identity"]["bytes"], len(b'{"message":"synthetic original main log"}\n'))

    def test_helper_unknown_private_error_is_sanitized_in_actual_command_and_run_ledgers(self):
        self.prepare_baseline()
        self.helper_failure = UPGRADE.canonical({"marker": "goby-main-schema28-helper-v1", "status": "failed", "error": "synthetic-private-main-password"})
        self.reject(lambda: self.controller("run").run())
        failed = self.output_json("failed.json")
        command = self.output_json("command-failure-001.json")
        self.assertEqual(failed["error_code"], "helper_failed")
        self.assertEqual(command["code"], "helper_failed")
        self.assertEqual(set(command), {"phase", "code", "exit", "stdout_sha256", "stderr_sha256"})
        self.assertNotIn("synthetic-private-main-password", UPGRADE.canonical({"failed": failed, "command": command}).decode())
        self.assertEqual(self.actions, [])
        self.assertEqual(self.sql_writes, [])


class MainActualAttestationGuards(MainMemoryCase):
    def test_independent_attest_uses_new_pid_and_original_successful_terminal(self):
        args = self.complete_run()
        self.assertNotIn(str(Path("/proc") / str(UPGRADE.OLD_PROCESS["pid"])), self.memory.entries)
        result = UPGRADE.attest(args)
        self.assertEqual(result["status"], "passed")
        self.assertTrue(result["recursive_cgroup_empty"] and result["full_preservation"])
        self.assertEqual(result["main_process"], self.new_process)
        self.assertFalse(result["main_client_acceptance"])
        self.assertEqual(self.output_json("attestation.json"), result)
        self.assertEqual(self.actions, ["reload-install", "stop", "start", "reload-restore"])
        self.assertEqual(len(self.requests), 5)
        self.reject(lambda: UPGRADE.attest(args))

    def test_independent_attest_rejects_wrong_invocation_liveness_exit_and_self_observation(self):
        args = self.complete_run()
        base = copy.deepcopy(self.outer_terminal)
        for name, value in (("MainPID", "330001"), ("InvocationID", "fe" * 16), ("SubState", "running"),
                            ("Result", "exit-code"), ("ExecMainStatus", "1"), ("RemainAfterExit", "no"), ("Description", "foreign-controller")):
            with self.subTest(field=name):
                self.outer_terminal = {**base, name: value}
                self.reject(lambda: UPGRADE.attest(args))
        self.outer_terminal, self.pid = base, 330001
        self.reject(lambda: UPGRADE.attest(args))
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_independent_attest_requires_recursive_empty_original_cgroup(self):
        args = self.complete_run()
        nested = Path("/sys/fs/cgroup/system.slice") / self.controller_unit / "unexpected-child/cgroup.procs"
        self.seed(nested, b"330099\n")
        self.reject(lambda: UPGRADE.attest(args))
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_independent_attest_rejects_post_run_main_master_or_inactive_slot_drift(self):
        args = self.complete_run()
        master = self.memory.entries[str(UPGRADE.MASTER)]["raw"]
        self.memory.entries[str(UPGRADE.MASTER)]["raw"] = b"1" * 32
        self.reject(lambda: UPGRADE.attest(args))
        self.memory.entries[str(UPGRADE.MASTER)]["raw"] = master
        self.databases[UPGRADE.RECOVERY]["sequences"]["main_sequence_0"]["last_value"] += 1
        self.reject(lambda: UPGRADE.attest(args))
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_independent_attest_rejects_re_signed_readiness_asset_receipt(self):
        args = self.complete_run()
        response = self.output_json("http-4-result.json")
        response["sha256"] = synthetic_hash("foreign-response")
        raw = UPGRADE.canonical(response)
        self.memory.entries[str(self.output / "http-4-result.json")]["raw"] = raw
        report = self.output_json("report.json")
        report["evidence"]["http-4-result.json"]["sha256"] = self.digest(raw)
        self.memory.entries[str(self.output / "report.json")]["raw"] = UPGRADE.canonical(report)
        self.reject(lambda: UPGRADE.attest(args))
        self.assertNotIn(str(self.output / "attestation.json"), self.memory.entries)

    def test_independent_attest_rechecks_copied_master_and_native_asset_modes(self):
        args = self.complete_run()
        materials = UPGRADE.decode(self.memory.raw(self.output / "private/materials.json"))
        item = materials[str(UPGRADE.MASTER)]
        raw = self.memory.entries[item["path"]]["raw"]
        self.memory.entries[item["path"]]["raw"] = b"changed copied master"
        self.reject(lambda: UPGRADE.attest(args))
        self.memory.entries[item["path"]]["raw"] = raw
        self.memory.entries[str(UPGRADE.WEB / "assets/main.js")]["mode"] = stat.S_IFREG | 0o600
        self.reject(lambda: UPGRADE.attest(args))


class MainPurePreservationGuards(GuardCase):
    def test_actual_migration_comparison_preserves_all_rows_sequences_mapping_identity_and_defaults(self):
        before = main_database_example(UPGRADE.MAIN["database"])
        after = migrated_main_database(before)
        catalog = {"objects": after["catalog"]}
        UPGRADE.compare_database(before, after, catalog)
        changes = (
            lambda value: value["tables"]["library_roots"][0].update(path="/other/main/path"),
            lambda value: value["tables"]["library_roots"][0].update(storage_binding={"approved": True}),
            lambda value: value["tables"]["library_roots"][0].update(binding_revision=True),
            lambda value: value["tables"]["activity_entries"][0].update(previous_revision=False),
            lambda value: value["tables"]["sessions"].pop(),
            lambda value: value["sequences"]["main_sequence_0"].update(log_cnt=1),
            lambda value: value["metadata"]["role"].update(database_oid=1498215),
            lambda value: value["metadata"]["relations"]["users"].update(acl=["PUBLIC=r/goby_test"]),
        )
        for index, change in enumerate(changes):
            with self.subTest(index=index):
                wrong = copy.deepcopy(after)
                change(wrong)
                self.reject(lambda: UPGRADE.compare_database(before, wrong, catalog))

    def test_actual_service_fence_allows_only_exact_main_stop_start_and_two_ordered_reloads(self):
        fence = UPGRADE.ServiceFence()
        for arguments in (("stop", UPGRADE.UNIT), ("restart", UPGRADE.UNIT), ("stop", "goby-client-m3e.service")):
            self.reject(lambda: fence.approve(["/usr/bin/systemctl", *arguments]))
        sequence = (("restart_fence_install", ["daemon-reload"]), ("stop_requested", ["stop", UPGRADE.UNIT]),
                    ("start_requested", ["start", UPGRADE.UNIT]), ("restart_fence_restore", ["daemon-reload"]))
        for stage, arguments in sequence:
            fence.stage = stage
            fence.approve(["/usr/bin/systemctl", *arguments])
            self.reject(lambda: fence.approve(["/usr/bin/systemctl", *arguments]))
        self.assertEqual(fence.actions, ["reload-install", "stop", "start", "reload-restore"])

    def test_main_helper_codes_are_fixed_and_private_text_never_becomes_a_code(self):
        for code in UPGRADE.HELPER_FAILURES:
            raw = UPGRADE.canonical({"marker": "goby-main-schema28-helper-v1", "status": "failed", "error": code})
            self.assertEqual(UPGRADE.helper_failure_code(raw), "helper_" + code)
        for code in ("main_profile_capture_failed", "role_properties_invalid", "lifecycle_fd_arguments_invalid", "lifecycle_scope_invalid", "lifecycle_fence_invalid"):
            self.assertIn(code, UPGRADE.HELPER_FAILURES)
        for raw in (b"private stderr", b'{"marker":"goby-main-schema28-helper-v1","status":"failed","error":"secret"}',
                    b'{"marker":"goby-main-schema28-helper-v1","status":"failed","error":"migration_failed","message":"secret"}'):
            self.assertEqual(UPGRADE.helper_failure_code(raw), "helper_failed")


class MainActualCliGuards(MainMemoryCase):
    def invoke(self, mode):
        value = self.prepare_intent if mode == "prepare" else self.intent
        args = self.seed_input(mode, value)
        stream = io.StringIO()
        with contextlib.redirect_stdout(stream):
            code = UPGRADE.main([mode, "--input", str(args.input), "--input-sha256", args.input_sha256])
        raw = stream.getvalue()
        self.assertNotIn("synthetic-main-password", raw)
        self.assertNotIn("synthetic private service error", raw)
        return code, UPGRADE.decode(raw.encode())

    def test_actual_cli_dispatches_prepare_preflight_run_and_separate_attest(self):
        code, result = self.invoke("prepare")
        self.assertEqual((code, result["status"]), (0, "prepared"))
        self.intent["main_baseline"] = result["baseline"]
        code, result = self.invoke("preflight")
        self.assertEqual((code, result["status"]), (0, "preflight_passed"))
        code, result = self.invoke("run")
        self.assertEqual((code, result["status"]), (0, "awaiting_outer_attestation"))
        self.prepare_attestation()
        code, result = self.invoke("attest")
        self.assertEqual((code, result["status"]), (0, "passed"))
        self.assertEqual(self.actions, ["reload-install", "stop", "start", "reload-restore"])

    def test_actual_cli_reports_one_failed_stop_without_private_text_or_retry(self):
        self.prepare_baseline()
        self.fail_action = "stop"
        code, result = self.invoke("run")
        self.assertEqual((code, result["status"], result["error_code"]), (1, "failed", "command_failed"))
        self.assertFalse(result["automatic_retry"])
        self.assertEqual(self.actions, ["reload-install", "stop"])


if __name__ == "__main__":
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        sys.stdout.write('{"suite":"main-schema28-upgrade-guards","status":"blocked"}\n')
        raise SystemExit(1)
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(stream=sys.stderr, verbosity=2).run(suite)
    unexpected = sum(len(fence.violations) for fence in OBSERVED_FENCES if not fence.expected_denial)
    passed = result.wasSuccessful() and not result.skipped and unexpected == 0
    report = {"suite": "main-schema28-upgrade-guards", "status": "passed" if passed else "failed",
        "operator_sha256": OPERATOR_SHA256, "guard_sha256": GUARD_SHA256, "test_count": result.testsRun,
        "failures": len(result.failures), "errors": len(result.errors), "skips": len(result.skipped),
        "actual_controller_flow": True, "unexpected_effects": unexpected}
    sys.stdout.write(json.dumps(report, sort_keys=True, separators=(",", ":")) + "\n")
    raise SystemExit(0 if passed else 1)
