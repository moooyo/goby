#!/usr/bin/env python3
"""Exercise the schema26 main-upgrade operator with fenced memory fixtures.

Run only through authorized root SSH. The sole input is the reviewed new
operator source. Its imports and every test execute behind filesystem, network,
process, and lock fences. The published deployment dependency is never loaded.
These tests do not perform a backup, rehearsal, migration, or deployment.
"""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import datetime as dt
from decimal import Decimal
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
WORK = Path("/opt/goby-test/exec-work-m3e")
OLD_BINARY_SHA = "665df2d3851dc1b4a251012805678559e17e274c08f2548aead560e593a08d2b"
OLD_SOURCE_SHA = "fa46e1547416afcb65c15f97aa5a1b7d3c344579386a424478ec310f0e01660b"
SYSTEM_IDENTIFIER = "7683277964552005578"
BOOT_ID = "6bdfc486-7bc8-412f-82b5-70095a09dde7"


def deny_audited_effect(event, _arguments):
    filesystem = {
        "open", "os.listdir", "os.scandir", "os.remove", "os.rmdir", "os.mkdir",
        "os.rename", "os.chmod", "os.chown", "os.link", "os.symlink", "os.truncate",
        "os.utime", "os.chdir", "os.fchdir", "os.chroot", "shutil.copyfile",
        "shutil.copymode", "shutil.copystat", "shutil.copytree", "shutil.move",
        "shutil.rmtree", "os.kill", "os.killpg",
    }
    if ACTIVE_FENCES and (event in filesystem or event.startswith((
            "socket.", "subprocess.", "os.exec", "os.spawn", "os.fork", "fcntl.")) or
            event == "os.system"):
        ACTIVE_FENCES[-1].violations.append(event)
        raise AssertionError("Audited external effect: " + event)


class EffectFence(contextlib.ExitStack):
    """Reject any real external operation, including imported callable aliases."""

    def __enter__(self):
        super().__enter__()
        self.violations = []
        ACTIVE_FENCES.append(self)
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "lazycache", lambda filename, module_globals: False))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       list(SOURCE_LINES.get(str(filename), ()))))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo", "socketpair", "fromfd")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (fcntl, ("flock", "lockf", "fcntl", "ioctl")),
            (secrets, ("token_bytes", "token_hex", "token_urlsafe")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (signal, ("signal", "setitimer", "pidfd_send_signal")),
            (time, ("sleep",)),
            (os, ("open", "fdopen", "stat", "lstat", "fstat", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "pread", "pwrite", "close", "lseek", "fsync", "fdatasync",
                  "fchmod", "fchown", "ftruncate", "umask", "kill", "killpg", "system", "popen",
                  "fork", "forkpty", "posix_spawn", "posix_spawnp", "execve", "execv", "execvp",
                  "replace", "rename", "mkdir", "makedirs", "remove", "unlink", "rmdir", "chmod",
                  "chown", "link", "symlink", "truncate", "utime", "mkfifo", "mknod", "access",
                  "chdir", "fchdir", "chroot", "pidfd_open", "urandom")),
            (Path, ("open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat",
                    "read_bytes", "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink",
                    "chmod", "touch", "rename", "replace", "glob", "rglob", "iterdir", "absolute")),
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


def information(*, size=0, mode=0o600, kind=stat.S_IFREG, uid=0, gid=0,
                inode=100, links=1, mtime_ns=1_700_000_000_000_000_000, ctime_ns=1_700_000_000_000_000_000):
    return types.SimpleNamespace(st_dev=7, st_ino=inode, st_uid=uid, st_gid=gid,
                                 st_mode=kind | mode, st_size=size, st_nlink=links,
                                 st_mtime_ns=mtime_ns, st_ctime_ns=ctime_ns)


class MemoryFiles:
    """An append-only in-memory artifact tree, with explicit failure injection."""

    def __init__(self):
        self.files = {}
        self.directories = set()
        self.events = []
        self.fail_name = None

    def read(self, path, **_kwargs):
        if path not in self.files:
            raise AssertionError("Unmodeled private artifact read: " + str(path))
        return self.files[path]

    def write(self, path, raw, mode=0o600):
        if path.name == self.fail_name:
            raise OSError("Simulated exclusive publication failure.")
        if path in self.files or path in self.directories or mode != 0o600:
            raise AssertionError("A memory artifact was overwritten or published with the wrong mode.")
        self.files[path] = bytes(raw)
        self.events.append(("write", path.name))

    def exists(self, path):
        return path in self.files or path in self.directories

    def mkdir(self, path, mode=0o777, parents=False, exist_ok=False):
        if self.exists(path) or parents or exist_ok or mode != 0o700:
            raise AssertionError("A memory directory was adopted or created outside the exclusive shape.")
        self.directories.add(path)
        self.events.append(("mkdir", str(path)))


class MainSchema26GuardTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())
        self.output = self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.replace(OPERATOR, "LOCK_HELD", False)
        self.replace(OPERATOR, "STARTUP_CONTEXT", None)
        self.replace(OPERATOR, "STARTUP_CHECKING", False)
        self.replace(OPERATOR, "STARTUP_CLEANUP", False)
        self.real_startup_checkpoint = OPERATOR.startup_checkpoint
        # Existing isolated primitive tests do not imply live startup authority.
        # The dedicated startup tests below restore the actual checkpoint.
        self.replace(OPERATOR, "startup_checkpoint", lambda action=None, required=False: {"synthetic_checkpoint": True})

    def replace(self, owner, name, value):
        return self.enterContext(patch.object(owner, name, value))

    def reject(self, action):
        with self.assertRaises(OPERATOR.Failure):
            action()

    def arguments(self):
        artifacts = WORK / "candidate-40"
        args = types.SimpleNamespace(mode="preflight", source=WORK / "source-attempt-40",
            operator=WORK / "tool-build-source18/scripts/test-env/deploy-client-schema25.py",
            deployment_receipt=OPERATOR.PUBLISHED_RUN / "terminal.json", candidate=artifacts / "goby-linux-amd64",
            assets=artifacts / "admin.tar.gz", full_report=artifacts / "full.json", helper=artifacts / "migrate-main-schema26",
            tool_source=WORK / "tool-build-main26", helper_build_report=artifacts / "helper-build.json",
            guard_report=artifacts / "guards.json", service_pin=artifacts / "service.json", release_report=artifacts / "release.json",
            startup_plan=artifacts / "startup-plan.json", failure_report=None, failure_report_sha256=None,
            rehearsal_failure_report=None, rehearsal_failure_report_sha256=None,
            old_pid=539535, old_start_ticks="3115871", old_boot_id=BOOT_ID)
        for index, name in enumerate(("script", "operator", "deployment_receipt", "manifest", "candidate", "assets", "full_report",
                                      "helper", "tool_manifest", "helper_build_report", "guard_report", "service_pin", "release_report", "startup_plan")):
            setattr(args, name + "_sha256", hashlib.sha256(("synthetic-artifact-" + str(index)).encode()).hexdigest())
        args.deployment_receipt_sha256 = OPERATOR.OLD_TERMINAL_SHA
        return args

    def candidate_fixture(self):
        args = self.arguments()
        documents, facts, overrides = {}, {}, {}
        product = {"go.mod": b"module example.invalid/guard\n", "go.sum": b"guard module checksum\n"}
        retention_code = {name: ("// Synthetic unchanged retention input: " + name + "\n").encode()
                          for name in OPERATOR.RETENTION_CODE_SHA256}
        product.update(retention_code)
        self.replace(OPERATOR, "RETENTION_CODE_SHA256", {name: OPERATOR.sha(raw) for name, raw in retention_code.items()})
        migrations = []
        for number in range(1, 27):
            name = f"{number:04d}_guard.sql"
            raw = f"-- Synthetic guarded migration {number}.\n".encode()
            product["internal/database/migrations/" + name] = raw
            migrations.append({"version": number, "name": name, "sha256": OPERATOR.sha(raw)})
        old_tables = [{"Name": f"guard_table_{number:02d}"} for number in range(30)]
        catalogs = {}
        for version in (23, 24, 25, 26):
            tables = old_tables + ([{"Name": name} for name in ("theme_owner_ids", "theme_reserved_paths", "item_theme_resources")]
                                  if version == 26 else [])
            raw = OPERATOR.canonical({"version": version, "migrations": migrations[:version], "catalog": {"Tables": tables}})
            product[f"internal/backuppg/catalogs/schema-{version}-postgresql-17.json"] = raw
            if version < 26:
                catalogs[version] = OPERATOR.sha(raw)
        for number in range(110):
            product[f"internal/guard/member_{number:03d}.go"] = f"// Synthetic product member {number}.\n".encode()
        product_files = {name: OPERATOR.sha(raw) for name, raw in product.items()}
        source_manifest = OPERATOR.canonical({"marker": "goby-client-backup-source-m3e-v1", "files": product_files})
        args.manifest_sha256 = OPERATOR.sha(source_manifest)
        documents.update({args.source / name: raw for name, raw in product.items()})
        documents[args.source / OPERATOR.SOURCE_MANIFEST] = source_manifest
        tools = {**product, OPERATOR.SOURCE_MANIFEST: source_manifest}
        tools.update({name: ("# Synthetic isolated tool " + name + "\n").encode() for name in OPERATOR.TOOL_FILES})
        args.script_sha256 = OPERATOR.sha(tools["scripts/test-env/upgrade-main-schema26.py"])
        tool_files = {name: OPERATOR.sha(raw) for name, raw in tools.items()}
        tool_manifest = OPERATOR.canonical({"marker": "goby-main-schema26-tool-source-v1",
            "product_source_manifest_sha256": args.manifest_sha256, "published_operator_sha256": args.operator_sha256,
            "files": tool_files})
        args.tool_manifest_sha256 = OPERATOR.sha(tool_manifest)
        documents.update({args.tool_source / name: raw for name, raw in tools.items()})
        documents[args.tool_source / OPERATOR.TOOL_MANIFEST] = tool_manifest
        for name in ("candidate", "helper", "assets"):
            facts[getattr(args, name)] = {"sha256": getattr(args, name + "_sha256"), "identity": {"bytes": 2 << 20}}
        packages = {"github.com/moooyo/goby/" + name for name in (
            "cmd/goby", "internal/activity", "internal/artwork", "internal/backupformat", "internal/backuppg",
            "internal/backupstore", "internal/config", "internal/database", "internal/diagnostics", "internal/events",
            "internal/identity", "internal/library", "internal/lifecycle", "internal/media", "internal/metadata",
            "internal/playback", "internal/recovery", "internal/recoverycontrol", "internal/recoverydb", "internal/server",
            "internal/settings", "internal/subtitle", "internal/tasks", "internal/transcode")}
        cleanup = {name: True for name in ("unit_terminal", "hba_restored_exactly", "goby_backup_m3e_source_removed",
                   "goby_backup_m3e_target_removed", "preexisting_catalog_unchanged", "receipt_saved")}
        evidence_path = WORK / "candidate-40/client-observation.json"
        documents[evidence_path] = OPERATOR.canonical({"synthetic": True, "status": "passed"})
        reports = {
            "full_report": {"status": "passed", "mode": "full", "schema": 26, "source": str(args.source),
                "source_manifest_sha256": args.manifest_sha256, "catalog_sha256": product_files["internal/backuppg/catalogs/schema-26-postgresql-17.json"],
                "binary": {"sha256": args.candidate_sha256, "bytes": 2 << 20}, "unit_exit": 0,
                "tests": {"passed": [f"TestGuardMember{number}" for number in range(1701)], "top_level_passes": 1701, "failures": 0, "skips": 0},
                "packages": sorted(packages), "cleanup": cleanup},
            "helper_build_report": {"schema": "goby-main-schema26-helper-build", "version": 1, "status": "passed",
                "product_source_manifest_sha256": args.manifest_sha256, "tool_manifest_sha256": args.tool_manifest_sha256,
                "helper_sha256": args.helper_sha256, "go_version": "go1.27.1", "goos": "linux", "goarch": "amd64", "cgo_enabled": False},
            "guard_report": {"suite": "main-schema26-upgrade-guards", "status": "passed", "failures": 0, "errors": 0, "skips": 0,
                "tests": 10, "cases": [f"test_synthetic_{number}" for number in range(10)], "operator_sha256": args.script_sha256,
                "guard_sha256": tool_files["scripts/test-env/test-upgrade-main-schema26.py"], "fixtures": "synthetic-memory-only",
                "real_backup_acceptance": False, "real_migration_acceptance": False, "real_deployment_acceptance": False},
            "release_report": {"schema": "goby-main-schema26-release-acceptance", "version": 1, "status": "passed", "schema_version": 26,
                "source_manifest_sha256": args.manifest_sha256, "binary_sha256": args.candidate_sha256,
                "candidate_verified": True, "client_verified": True,
                "evidence": [{"path": str(evidence_path), "sha256": OPERATOR.sha(documents[evidence_path])}]},
        }

        def refresh_reports():
            for name in ("full_report", "helper_build_report", "guard_report", "release_report"):
                if name == "release_report":
                    reports[name]["full_report_sha256"] = args.full_report_sha256
                raw = OPERATOR.canonical(reports[name])
                documents[getattr(args, name)] = raw
                setattr(args, name + "_sha256", OPERATOR.sha(raw))

        refresh_reports()

        def read(path, **_kwargs):
            if path not in documents:
                raise AssertionError("Unmodeled candidate input: " + str(path))
            return documents[path]

        def info(path):
            return overrides.get(path, information(size=len(read(path))))

        def walk(path, pattern):
            self.assertIn(path, (args.source, args.tool_source))
            self.assertEqual(pattern, "*")
            return sorted(selected for selected in documents if selected.is_relative_to(path))

        dependency = types.SimpleNamespace(CATALOGS=catalogs, EXPECTED_PACKAGES=packages, FULL_CLEANUP=set(cleanup),
            directory=lambda path: {"path": str(path)}, path_info=info, safe_relative=lambda name: name,
            read_file=read, file_fact=lambda path, **_kwargs: copy.deepcopy(facts[path]),
            read_assets=lambda path: {"index.html": b"synthetic index", "assets/main.js": b"synthetic JavaScript"})
        self.replace(Path, "rglob", walk)
        self.replace(OPERATOR, "LOCK_HELD", True)
        return {"args": args, "op": dependency, "published": {"operator_sha256": args.operator_sha256},
                "documents": documents, "reports": reports, "refresh": refresh_reports,
                "overrides": overrides, "facts": facts, "product_files": product_files, "tool_files": tool_files}

    def evidence_fixture(self):
        memory = MemoryFiles()
        memory.directories.add(OPERATOR.ROOT.parent)

        def directory(path):
            if path not in memory.directories:
                raise AssertionError("Unmodeled evidence directory: " + str(path))
            return {"path": str(path)}

        def glob(path, pattern):
            self.assertEqual(pattern, "intent-*.json")
            return sorted(selected for selected in memory.files if selected.parent == path and selected.name.startswith("intent-"))

        self.replace(os.path, "lexists", memory.exists)
        self.replace(Path, "mkdir", lambda path, mode=0o777, parents=False, exist_ok=False:
                     memory.mkdir(path, mode=mode, parents=parents, exist_ok=exist_ok))
        self.replace(Path, "glob", glob)
        self.replace(OPERATOR, "LOCK_HELD", True)
        dependency = types.SimpleNamespace(directory=directory, write_exclusive=memory.write,
            read_file=memory.read, sync_directory=lambda path: memory.events.append(("sync", str(path))))
        return dependency, memory

    def failure_fixture(self):
        """Model the complete stopped, read-only failure without importing old tools."""
        args = self.arguments()
        args.mode = "repair-baseline"
        args.failure_report = WORK / "candidate-40/baseline-failure.json"
        self.replace(OPERATOR, "ROOT", OPERATOR.REPAIR_ROOT)
        self.replace(OPERATOR, "LOCK_HELD", True)
        documents, overrides = {}, {}
        run = OPERATOR.FAILED_RUN
        directories = {OPERATOR.NORMAL_ROOT, run, run / "materials"}
        old_tool = WORK / "tool-build-main26-failed"
        source_names = ("scripts/test-env/upgrade-main-schema26.py", "scripts/test-env/migrate-main-schema26.go")
        for name in source_names:
            documents[old_tool / name] = ("Synthetic frozen failed input: " + name + "\n").encode()
        self.replace(OPERATOR, "FAILED_OPERATOR_SHA", OPERATOR.sha(documents[old_tool / source_names[0]]))
        self.replace(OPERATOR, "FAILED_HELPER_SOURCE_SHA", OPERATOR.sha(documents[old_tool / source_names[1]]))
        helper_path = WORK / "candidate-40/failed-helper"
        documents[helper_path] = b"Synthetic failed helper binary"

        def fact(path, **_kwargs):
            raw = documents[path]
            return {"identity": {"device": 7, "inode": 100, "uid": 0, "gid": 0, "mode": 0o600, "bytes": len(raw)},
                    "sha256": OPERATOR.sha(raw)}

        def directory(path, *_args, **_kwargs):
            self.assertIn(path, directories)
            return {"device": 7, "inode": 200 + sorted(directories).index(path), "uid": 0, "gid": 0, "mode": 0o700, "bytes": 0}

        def tree(path, uid, gid, **_kwargs):
            self.assertEqual((uid, gid), (0, 0))
            return {"root": directory(path), "files": {
                member.relative_to(path).as_posix(): fact(member)
                for member in sorted(documents) if member.is_relative_to(path)}}

        def walk(path, pattern):
            self.assertIn(path, (OPERATOR.NORMAL_ROOT, OPERATOR.REPAIR_ROOT))
            self.assertEqual(pattern, "*")
            return sorted(member for member in documents.keys() | directories if member != path and member.is_relative_to(path))

        def relative(name):
            self.assertIsInstance(name, str)
            self.assertNotIn("..", Path(name).parts)
            self.assertFalse(Path(name).is_absolute())
            return name

        def write_document(path, value):
            documents[path] = OPERATOR.canonical(value)

        old_process = {"pid": 539535, "start_ticks": "3115871", "boot_id": BOOT_ID}
        service_pin = {"path": str(args.service_pin), "sha256": args.service_pin_sha256}
        inputs = {"asset_members": {"index.html": "1" * 64}, "old_process": old_process, "service_pin": service_pin,
                  "repair_baseline": True}
        old_inputs = {**copy.deepcopy(inputs), "repair_baseline": False, "source_manifest_sha256": args.manifest_sha256,
            "full_report_sha256": args.full_report_sha256, "candidate": {"sha256": args.candidate_sha256},
            "published_deployment_receipt_sha256": OPERATOR.OLD_TERMINAL_SHA,
            "tool_source": {"path": str(old_tool), "manifest_sha256": "2" * 64,
                            "files": {name: OPERATOR.sha(documents[old_tool / name]) for name in source_names}},
            "helper": {"path": str(helper_path), **fact(helper_path)}}
        before = {"runtime": {"sha256": "3" * 64}, "recovery_environment": {"sha256": "4" * 64},
                  "master": {"sha256": "5" * 64}, "diagnostics": {"files": {}}, "stores": {"backups": []}}
        primary = {"process": {"pid": 777, "start_ticks": "100001", "boot_id": BOOT_ID},
                   "sql": {"start_microseconds": 1789100000123456}}
        original = {"phase": "preflight", "inputs_sha256": OPERATOR.sha(OPERATOR.canonical(old_inputs)),
                    "service": {"pid": 539535, "start_ticks": "3115871"}, "primary": primary,
                    "protected": {key: value for key, value in before.items() if key != "diagnostics"}}
        stopped = {"service": {"pid": 0, "binary_sha256": OLD_BINARY_SHA},
                   "database_activity": {"sessions": 0, "prepared": 0, "slots": 0}}
        write_document(OPERATOR.NORMAL_ROOT / "OWNER.json", {"marker": OPERATOR.MARKER, "version": 1,
            "path": str(OPERATOR.NORMAL_ROOT), "run_id": run.name})
        write_document(run / "OWNER.json", {"marker": OPERATOR.MARKER, "version": 1, "run_id": run.name})
        for name, value in (("inputs.json", old_inputs), ("online-preflight.json", original),
                            ("stopped.json", stopped), ("protected-before.json", before)):
            write_document(run / name, value)
        documents[run / "materials/goby-source18"] = b"Synthetic retained original binary"
        documents[run / "materials/runtime.env"] = b"PRIVATE_SYNTHETIC=retained\n"
        material_files = {path.name: OPERATOR.sha(raw) for path, raw in documents.items() if path.parent == run / "materials"}
        write_document(run / "materials.json", {"files": material_files, "old_binary_sha256": OLD_BINARY_SHA,
                                                "automatic_restore_supported": False})
        previous = None
        for sequence, action in enumerate(("stop", "save-materials", "baseline-dump"), 1):
            path = run / f"intent-{sequence:04d}.json"
            write_document(path, {"marker": OPERATOR.MARKER, "run_id": run.name, "sequence": sequence, "action": action,
                "previous_sha256": previous, "inputs_sha256": OPERATOR.sha(documents[run / "inputs.json"])})
            previous = OPERATOR.sha(documents[path])
        write_document(run / "terminal.json", {"marker": OPERATOR.MARKER, "version": 1, "run_id": run.name,
            "status": "failed", "last_completed_phase": "stopped", "last_intent_sha256": previous, "error_type": "Failure",
            "mutation_outcome_must_be_reviewed": True, "automatic_recovery_executed": False,
            "main_database_restored": False, "old_binary_rollback": False, "evidence_retained": True})
        write_document(run / "baseline-cluster.json", {"schema": "goby-main-schema26-cluster-proof", "run_id": run.name,
            **OPERATOR.MAIN, "system_identifier": SYSTEM_IDENTIFIER, "source_manifest_sha256": args.manifest_sha256,
            "tool_manifest_sha256": old_inputs["tool_source"]["manifest_sha256"], "candidate_sha256": args.candidate_sha256,
            "helper_sha256": old_inputs["helper"]["sha256"], "postmaster_start_microseconds": primary["sql"]["start_microseconds"]})
        pending = run / (".main-schema26-report-" + "a" * 32 + ".pending")
        documents[pending] = b""
        documents[run / ".baseline.json.reserved"] = b"goby-main-schema26-migration\nbaseline.json\n"
        for label in ("baseline", "stop-main"):
            documents[run / (label + "-" + "b" * 12 + ".stdout")] = b""
            documents[run / (label + "-" + "b" * 12 + ".stderr")] = (OPERATOR.canonical({
                "schema": "goby-main-schema26-migration", "version": 1, "status": "failed", "code": "column_acl_capture_failed"})
                if label == "baseline" else b"")
        def path_info(path):
            attributes = {"mode": 0o700 if path in directories else 0o600,
                          "kind": stat.S_IFDIR if path in directories else stat.S_IFREG,
                          "size": len(documents[path]) if path in documents else 0}
            attributes.update(overrides.get(path, {}))
            return information(**attributes)

        dependency = types.SimpleNamespace(tree_state=tree, directory=directory, safe_relative=relative,
            path_info=path_info,
            read_file=lambda path, **_kwargs: documents[path], file_fact=fact)
        self.replace(Path, "rglob", walk)
        self.replace(os.path, "lexists", lambda path: path in documents or path in directories)
        report = {"schema": "goby-main-schema26-baseline-failure", "version": 1,
            "failed_root": str(OPERATOR.NORMAL_ROOT), "failed_run": str(run), "source_manifest_sha256": args.manifest_sha256,
            "candidate_sha256": args.candidate_sha256, "full_report_sha256": args.full_report_sha256,
            "service_pin_sha256": args.service_pin_sha256, "deployment_receipt_sha256": OPERATOR.OLD_TERMINAL_SHA,
            "target": dict(OPERATOR.MAIN), "system_identifier": SYSTEM_IDENTIFIER, "old_process": old_process,
            "error_code": "column_acl_capture_failed", "observed_schema": 25, "observed_activity_total": 15}

        observation_path = WORK / "main-schema26-baseline-failure-review-01/failure-observation.json"
        observation = {"root": str(OPERATOR.NORMAL_ROOT), "run": str(run), "failure_code": "column_acl_capture_failed",
            "baseline_absent": True, "pending_empty": True, "intent_actions": ["stop", "save-materials", "baseline-dump"],
            "observed_service": {"MainPID": "0", "ActiveState": "inactive", "SubState": "dead"},
            "main_binary_sha256": OLD_BINARY_SHA}

        def refresh():
            observation["inventory"] = OPERATOR.failure_tree(dependency)
            observation["terminal"] = OPERATOR.decode(documents[run / "terminal.json"])
            write_document(observation_path, observation)
            digest = OPERATOR.sha(documents[observation_path])
            self.replace(OPERATOR, "FAILURE_OBSERVATION_SHA", digest)
            report["observation"] = {"path": str(observation_path), "sha256": digest}
            write_document(args.failure_report, report)
            args.failure_report_sha256 = OPERATOR.sha(documents[args.failure_report])

        refresh()
        return {"args": args, "op": dependency, "inputs": inputs, "documents": documents, "directories": directories,
                "report": report, "observation": observation, "observation_path": observation_path, "overrides": overrides,
                "refresh": refresh, "write_document": write_document,
                "before": before, "original": original, "stopped": stopped, "pending": pending,
                "published": {"protected_before": before}}

    def startup_fixture(self):
        """Build a review attestation over synthetic, independently hashed inputs."""
        args = self.arguments()
        documents = {}
        code = {name: ("// Synthetic unchanged retention input: " + name + "\n").encode() for name in (
            "internal/config/observability.go", "internal/server/activity_retention.go", "internal/activity/store.go")}
        code_hashes = {name: OPERATOR.sha(raw) for name, raw in code.items()}
        self.replace(OPERATOR, "RETENTION_CODE_SHA256", code_hashes)
        unit = Path("/etc/systemd/system") / OPERATOR.SERVICE
        dropins = (unit.with_name(OPERATOR.SERVICE + ".d") / "30-observability.conf",)
        active = {name: 0 for name in ("runnable_triggers", "active_runs", "active_children", "active_scans", "active_encodings")}
        absent = {name: True for name in ("process", "manager", "unit", "runtime_env", "recovery_env", "pass_environment", "unset_environment")}
        observed = {"database_clock": "2026-09-11T12:31:45.045806Z", "activity_total": 15,
                    "earliest_activity": "2026-09-10T10:26:41.150555Z", "legacy_one_day_eligible": 6,
                    "effective_retention_eligible": 0, "active": active}
        plan = {"schema": "goby-main-schema26-startup-plan", "version": 1, "target": dict(OPERATOR.MAIN),
                "system_identifier": SYSTEM_IDENTIFIER, "source_manifest_sha256": args.manifest_sha256,
                "full_report_sha256": args.full_report_sha256, "candidate_sha256": args.candidate_sha256,
                "deployment_receipt_sha256": OPERATOR.OLD_TERMINAL_SHA, "service_pin_sha256": args.service_pin_sha256,
                "old_process": {"pid": 539535, "start_ticks": "3115871", "boot_id": BOOT_ID},
                "unit_sha256": "1" * 64, "dropins_sha256": {str(dropins[0]): "2" * 64},
                "runtime_env_sha256": "2043e72115338d04775485dd63702c6084d36d09331ca5cdac66819152619607",
                "recovery_env_sha256": "6847d34cfd9af7c53a8b16406db6f341f418b6ffdacb83bfe8c1f9bfd36b4c7c",
                "retention": {"environment_key": "GOBY_ACTIVITY_RETENTION_DAYS", "effective_days": 30,
                    "source": "unchanged-code-default", "first_prune_delay_seconds": 60, "prune_interval_seconds": 60,
                    "batch_limit": 1000, "clock": "PostgreSQL clock_timestamp",
                    "comparison": "created_at < clock_timestamp() - retention", "code_sha256": code_hashes,
                    "overrides_absent": absent},
                "observed": observed, "deadline_utc": "2026-09-11T18:31:45.045806Z", "startup_reserve_seconds": 900,
                "evidence": {}}
        for kind, value in (("environment", {"overrides_absent": absent}), ("counts", observed)):
            path = WORK / "candidate-40" / ("startup-" + kind + ".json")
            documents[path] = OPERATOR.canonical(value)
            plan["evidence"][kind] = {"path": str(path), "sha256": OPERATOR.sha(documents[path])}
        for name, raw in code.items():
            documents[args.source / name] = raw
        runtime = Path("/opt/goby-test/runtime.env")
        recovery = Path("/opt/goby-test/recovery-m5j.env")
        documents[runtime] = b"GOBY_DATABASE_URL='private-runtime-sentinel'\n"
        documents[recovery] = b"GOBY_RECOVERY_DATABASE_URL='private-recovery-sentinel'\n"
        environment_hashes = {"runtime_env": OPERATOR.sha(documents[runtime]), "recovery_env": OPERATOR.sha(documents[recovery])}
        self.replace(OPERATOR, "STARTUP_ENV_HASHES", environment_hashes)
        for name, digest in environment_hashes.items():
            plan[name + "_sha256"] = digest

        def refresh():
            documents[args.startup_plan] = OPERATOR.canonical(plan)
            args.startup_plan_sha256 = OPERATOR.sha(documents[args.startup_plan])

        def read(path, **_kwargs):
            if path not in documents:
                raise AssertionError("Unmodeled startup-plan file read: " + str(path))
            return documents[path]

        def legacy_quiescent(*_args, **_kwargs):
            raise AssertionError("The startup gate called the frozen legacy one-day quiescent policy.")

        refresh()
        dependency = types.SimpleNamespace(UNIT=unit, UNIT_SHA="1" * 64, DROPINS=dropins, DROPIN_SHAS=("2" * 64,),
            RUNTIME=runtime, RECOVERY_ENV=recovery,
            read_file=read, quiescent=legacy_quiescent)
        self.replace(OPERATOR, "LOCK_HELD", True)
        return {"args": args, "op": dependency, "plan": plan, "documents": documents,
                "refresh": refresh, "code": code, "code_hashes": code_hashes, "environment_hashes": environment_hashes}

    def startup_gate_fixture(self):
        model = self.startup_fixture()
        current = {**copy.deepcopy(model["plan"]["observed"]), "database_clock": "2026-09-11T13:00:00.000000Z", "deadline_eligible": 0}
        configuration = {"overrides_absent": {key: True for key in OPERATOR.ABSENT_SOURCES},
                         "process_environment_checked": True, "observed_pid": 539535, "effective_retention_days": 30}
        self.replace(OPERATOR, "retention_environment", lambda *_args: copy.deepcopy(configuration))
        self.replace(OPERATOR, "startup_counts", lambda *_args: copy.deepcopy(current))
        model.update(current=current, configuration=configuration)
        return model

    def test_fixed_target_and_published_lifetime_do_not_reuse_candidate_authority(self):
        self.assertEqual(OPERATOR.WORK, WORK)
        self.assertEqual(OPERATOR.ROOT, Path("/opt/goby-test/backups/main-schema26-v1"))
        self.assertEqual(OPERATOR.MARKER, "goby-main-schema26-upgrade-v1")
        self.assertEqual(OPERATOR.SERVICE, "goby-foundation-test.service")
        self.assertEqual(OPERATOR.MAIN, {"database": "goby_test", "database_oid": 16385,
                                        "role": "goby_test", "role_oid": 16384})
        self.assertEqual(OPERATOR.SYSTEM_IDENTIFIER, SYSTEM_IDENTIFIER)
        self.assertEqual((OPERATOR.OLD_PID, OPERATOR.OLD_TICKS), (539535, "3115871"))
        self.assertEqual(OPERATOR.OLD_BINARY_SHA, OLD_BINARY_SHA)

    def test_private_json_rejects_duplicate_keys_and_nonfinite_numbers(self):
        self.assertEqual(OPERATOR.decode(b'{"owner":{"id":16385},"flags":[true,false,null]}'),
                         {"owner": {"id": 16385}, "flags": [True, False, None]})
        self.assertNotEqual(OPERATOR.canonical({"value": True}), OPERATOR.canonical({"value": 1}))
        self.assertNotEqual(OPERATOR.canonical({"value": "16385"}), OPERATOR.canonical({"value": 16385}))
        for raw in (b'{"id":1,"id":2}', b'{"nested":{"value":0,"value":1}}',
                    b'{"n":NaN}', b'{"n":Infinity}', b'{"n":-Infinity}'):
            with self.subTest(raw=raw):
                self.reject(lambda: OPERATOR.decode(raw))
        for raw in (b"{", b"\xff"):
            with self.subTest(raw=raw), self.assertRaises((ValueError, UnicodeDecodeError, OPERATOR.Failure)):
                OPERATOR.decode(raw)
        self.assertEqual(OPERATOR.sha(b"guard fixture"), hashlib.sha256(b"guard fixture").hexdigest())
        class DependencyFailure(Exception):
            pass

        dependency = types.SimpleNamespace(Failure=DependencyFailure)
        self.assertEqual(OPERATOR.failure_reason(dependency, OPERATOR.Failure("Reviewed new guard rejection.")), "Reviewed new guard rejection.")
        self.assertEqual(OPERATOR.failure_reason(dependency, DependencyFailure("Reviewed dependency rejection.")), "Reviewed dependency rejection.")
        for error in (RuntimeError("private credential sentinel"), ValueError("private credential sentinel"),
                      subprocess.CalledProcessError(1, ["private credential sentinel"])):
            self.assertEqual(OPERATOR.failure_reason(dependency, error), type(error).__name__)
            self.assertEqual(OPERATOR.failure_reason(None, error), type(error).__name__)

    def test_unrecognized_operation_mode_is_rejected_before_external_access(self):
        for mode in ("prepare", "deploy", "resume", "restore", "rollback", "restart", "", None):
            with self.subTest(mode=mode):
                self.reject(lambda: OPERATOR.validate_arguments(types.SimpleNamespace(mode=mode)))

    def test_pinned_source_rejects_escape_ownership_modes_and_links_before_open(self):
        path = WORK / "operator-guards/upgrade-main-schema26.py"
        for invalid in (Path("relative.py"), WORK / "../outside.py"):
            with self.subTest(path=str(invalid)):
                self.reject(lambda: OPERATOR.read_pinned_source(invalid, "a" * 64))
        for changed in (
            {"uid": 995}, {"gid": 986}, {"links": 2}, {"kind": stat.S_IFLNK},
            {"kind": stat.S_IFIFO}, {"mode": 0o666}, {"size": 0},
            {"size": OPERATOR.MAX_DOCUMENT + 1},
        ):
            before = information(**{"size": 4, **changed})
            with self.subTest(changed=changed), patch.object(Path, "lstat", lambda selected:
                    before if selected == path else information(kind=stat.S_IFDIR, mode=0o700)):
                self.reject(lambda: OPERATOR.read_pinned_source(path, "a" * 64))

    def test_pinned_source_compares_opened_and_final_identity_and_digest(self):
        path = WORK / "operator-guards/upgrade-main-schema26.py"
        content = b"synthetic operator bytes\n"
        for fault in (None, "digest", "open-inode", "read-time", "path-inode"):
            before = information(size=len(content))
            opened = information(size=len(content), inode=101) if fault == "open-inode" else before
            after_read = information(size=len(content), ctime_ns=before.st_ctime_ns + 1) if fault == "read-time" else before
            final = information(size=len(content), inode=102) if fault == "path-inode" else before
            calls = []
            observations = iter((before, final))
            handle = io.BytesIO(content)
            handle.fileno = lambda: 41

            def lstat(selected):
                return next(observations) if selected == path else information(kind=stat.S_IFDIR, mode=0o700)

            def open_memory(selected, flags):
                self.assertEqual(selected, path)
                self.assertTrue(flags & os.O_NOFOLLOW)
                calls.append((selected, flags))
                return 41

            expected = "0" * 64 if fault == "digest" else hashlib.sha256(content).hexdigest()
            with self.subTest(fault=fault), patch.object(Path, "lstat", lstat), \
                    patch.object(os, "open", open_memory), patch.object(os, "fdopen", return_value=handle), \
                    patch.object(os, "fstat", side_effect=(opened, after_read)):
                if fault is None:
                    self.assertEqual(OPERATOR.read_pinned_source(path, expected), content)
                else:
                    self.reject(lambda: OPERATOR.read_pinned_source(path, expected))
                self.assertEqual(len(calls), 1)

    def test_shared_lock_precedes_authority_and_is_released_after_failure(self):
        events = []

        @contextlib.contextmanager
        def lock(create):
            self.assertIs(create, False)
            self.assertFalse(OPERATOR.LOCK_HELD)
            events.append("lock")
            try:
                yield
            finally:
                events.append("unlock")

        dependency = types.SimpleNamespace(operator_lock=lock)
        with self.assertRaisesRegex(RuntimeError, "Synthetic guarded failure"):
            with OPERATOR.deployment_lock(dependency):
                self.assertTrue(OPERATOR.LOCK_HELD)
                self.reject(lambda: OPERATOR.deployment_lock(dependency).__enter__())
                events.append("work")
                raise RuntimeError("Synthetic guarded failure")
        self.assertEqual(events, ["lock", "work", "unlock"])
        self.assertFalse(OPERATOR.LOCK_HELD)

    def test_locked_primitives_reject_unowned_entry_before_dependency_calls(self):
        dependency = types.SimpleNamespace()
        arguments = types.SimpleNamespace()
        for name, invoke in (
            ("candidate", lambda: OPERATOR.candidate_inputs(dependency, arguments, {})),
            ("service", lambda: OPERATOR.exact_service(dependency, arguments)),
            ("primary", lambda: OPERATOR.primary_proof(dependency, arguments)),
            ("preflight", lambda: OPERATOR.preflight(dependency, arguments, {}, {})),
            ("evidence", lambda: OPERATOR.fresh_evidence(dependency, {}, {}, "run-20260911T080000Z-" + "1" * 24)),
            ("intent", lambda: OPERATOR.append_intent(dependency, OPERATOR.ROOT / "invalid", 1, "stop", None, {})),
        ):
            with self.subTest(primitive=name):
                self.reject(invoke)

    def test_explicit_arguments_bind_every_artifact_target_and_old_process(self):
        args = self.arguments()
        OPERATOR.validate_arguments(args)
        for field, value in (
            ("deployment_receipt", WORK / "foreign/terminal.json"),
            ("deployment_receipt_sha256", "0" * 64), ("old_pid", True), ("old_pid", 539536),
            ("old_start_ticks", "3115872"), ("old_boot_id", "0" * 36),
            ("source", WORK / "source-attempt-18"), ("source", WORK / "nested/source-attempt-40"),
            ("operator", Path("/opt/goby-test/deploy-client-schema25.py")),
            ("operator", WORK / "tool-build-main26/scripts/test-env/foreign.py"),
            ("tool_source", WORK / "nested/tool-build-main26"),
            ("candidate", WORK / "../outside/goby"), ("helper", Path("/tmp/migrate-main-schema26")),
            ("service_pin", Path("relative.json")), ("release_report", Path("/tmp/release.json")),
            ("candidate_sha256", OLD_BINARY_SHA),
        ):
            changed = copy.copy(args)
            setattr(changed, field, value)
            with self.subTest(field=field, value=str(value)):
                self.reject(lambda: OPERATOR.validate_arguments(changed))
        for field in vars(args):
            if field.endswith("_sha256"):
                changed = copy.copy(args)
                setattr(changed, field, "missing-digest")
                with self.subTest(digest=field):
                    self.reject(lambda: OPERATOR.validate_arguments(changed))

    def test_candidate_requires_matching_full_helper_guard_and_release_provenance(self):
        model = self.candidate_fixture()
        args, dependency, published = model["args"], model["op"], model["published"]
        OPERATOR.validate_arguments(args)
        accepted = OPERATOR.candidate_inputs(dependency, args, published)
        self.assertEqual(accepted["source_files"], model["product_files"])
        self.assertEqual(accepted["tool_source"]["files"], model["tool_files"])
        self.assertEqual(accepted["candidate"]["sha256"], args.candidate_sha256)
        baseline = copy.deepcopy(model["reports"])
        faults = (
            ("full_report", ("schema",), 25), ("full_report", ("unit_exit",), False),
            ("full_report", ("tests", "skips"), 1), ("full_report", ("tests", "failures"), 1),
            ("full_report", ("source_manifest_sha256",), "0" * 64),
            ("full_report", ("binary", "sha256"), OLD_BINARY_SHA),
            ("full_report", ("tests", "top_level_passes"), 1700),
            ("helper_build_report", ("tool_manifest_sha256",), "0" * 64),
            ("helper_build_report", ("cgo_enabled",), True),
            ("guard_report", ("guard_sha256",), "0" * 64),
            ("guard_report", ("operator_sha256",), "0" * 64),
            ("guard_report", ("errors",), 1), ("guard_report", ("real_deployment_acceptance",), True),
            ("release_report", ("client_verified",), False),
            ("release_report", ("candidate_verified",), False),
            ("release_report", ("binary_sha256",), OLD_BINARY_SHA),
            ("release_report", ("evidence",), [{"path": "/tmp/foreign.json", "sha256": "1" * 64}]),
        )
        for report, keys, value in faults:
            model["reports"].clear()
            model["reports"].update(copy.deepcopy(baseline))
            current = model["reports"][report]
            for key in keys[:-1]:
                current = current[key]
            current[keys[-1]] = value
            model["refresh"]()
            with self.subTest(report=report, field=".".join(keys)):
                self.reject(lambda: OPERATOR.candidate_inputs(dependency, args, published))

    def test_complete_product_and_tool_inventories_reject_unbound_members(self):
        model = self.candidate_fixture()
        args, dependency, published = model["args"], model["op"], model["published"]
        member = args.source / "internal/guard/member_000.go"
        for change in ({"uid": 995}, {"links": 2}, {"mode": 0o666}, {"kind": stat.S_IFLNK}, {"kind": stat.S_IFIFO}):
            model["overrides"][member] = information(**{"size": len(model["documents"][member]), **change})
            with self.subTest(change=change):
                self.reject(lambda: OPERATOR.candidate_inputs(dependency, args, published))
        model["overrides"].clear()
        changed_retention = dict(OPERATOR.RETENTION_CODE_SHA256)
        changed_retention["internal/activity/store.go"] = "0" * 64
        with patch.object(OPERATOR, "RETENTION_CODE_SHA256", changed_retention):
            self.reject(lambda: OPERATOR.candidate_inputs(dependency, args, published))
        for path in (args.source / "unexpected.go", args.tool_source / "unreviewed-helper.py"):
            model["documents"][path] = b"Unbound member.\n"
            with self.subTest(member=str(path)):
                self.reject(lambda: OPERATOR.candidate_inputs(dependency, args, published))
            del model["documents"][path]
        cloned = args.tool_source / "go.mod"
        original = model["documents"][cloned]
        model["documents"][cloned] = b"Changed product clone.\n"
        self.reject(lambda: OPERATOR.candidate_inputs(dependency, args, published))
        model["documents"][cloned] = original
        path = args.source / "internal/backuppg/catalogs/schema-25-postgresql-17.json"
        model["documents"][path] = b"{}\n"
        self.reject(lambda: OPERATOR.candidate_inputs(dependency, args, published))

    def test_published_receipt_chain_rejects_other_installations_before_dependency_execution(self):
        args = self.arguments()
        raw_dependency = ("from pathlib import Path\n" + "\n".join(
            name + " = Path(" + repr(str(value)) + ")" for name, value in (
                ("WORK", WORK), ("ROOT", OPERATOR.PUBLISHED_ROOT), ("LIVE", OPERATOR.LIVE))) + "\n" +
            "\n".join(name + " = " + repr(value) for name, value in (
                ("SERVICE", OPERATOR.SERVICE), ("SYSTEM_IDENTIFIER", SYSTEM_IDENTIFIER),
                ("DEPLOYMENT_ID", OPERATOR.DEPLOYMENT_ID), ("MAIN", OPERATOR.MAIN))) + "\n").encode()
        args.operator_sha256 = OPERATOR.sha(raw_dependency)
        inputs = {"source_manifest_sha256": OLD_SOURCE_SHA, "candidate": {"sha256": OLD_BINARY_SHA},
                  "source_files": dict(OPERATOR.RETENTION_CODE_SHA256),
                  "tool_source": {"path": str(args.operator.parents[2]), "files": {
                      "scripts/test-env/deploy-client-schema25.py": args.operator_sha256}}, "asset_members": {"index.html": "a" * 64}}
        prepared = {"phase": "prepared", "rehearsal_passed": True, "rehearsal_removed": True,
                    "inputs_sha256": "1" * 64, "files": {"protected-before.json": "2" * 64}}
        terminal = {"status": "passed", "schema_version": 25, "binary_sha256": OLD_BINARY_SHA,
                    "old_archives_retained": True, "main_database_restored": False,
                    "active_service": {"MainPID": "539535", "start_ticks": "3115871"}, "prepared_sha256": "3" * 64}
        documents = {OPERATOR.PUBLISHED_RUN / "inputs.json": inputs, OPERATOR.PUBLISHED_RUN / "prepared.json": prepared,
                     args.deployment_receipt: terminal, OPERATOR.PUBLISHED_RUN / "protected-before.json": {"retained": True}}
        baseline = copy.deepcopy(documents)
        reads = []

        def read(path, expected, **_kwargs):
            reads.append(path)
            if path == Path(OPERATOR.__file__):
                self.assertEqual(expected, args.script_sha256)
                return b"# Synthetic current operator acknowledgement.\n"
            if path == args.operator:
                self.assertEqual(expected, args.operator_sha256)
                return raw_dependency
            self.assertIn(path, documents)
            return OPERATOR.canonical(documents[path])

        self.replace(OPERATOR, "read_pinned_source", read)
        self.replace(Path, "absolute", lambda path: path)
        dependency, published = OPERATOR.load_published_operator(args)
        self.assertEqual(dependency.MAIN, OPERATOR.MAIN)
        self.assertEqual(published["operator_sha256"], args.operator_sha256)
        for path, keys, value in (
            (args.deployment_receipt, ("main_database_restored",), True),
            (args.deployment_receipt, ("active_service", "MainPID"), "539536"),
            (args.deployment_receipt, ("schema_version",), 26),
            (OPERATOR.PUBLISHED_RUN / "prepared.json", ("rehearsal_removed",), False),
            (OPERATOR.PUBLISHED_RUN / "inputs.json", ("source_manifest_sha256",), "0" * 64),
            (OPERATOR.PUBLISHED_RUN / "inputs.json", ("source_files", "internal/activity/store.go"), "0" * 64),
            (OPERATOR.PUBLISHED_RUN / "inputs.json", ("tool_source", "path"), str(WORK / "tool-build-other")),
        ):
            documents.clear()
            documents.update(copy.deepcopy(baseline))
            current = documents[path]
            for key in keys[:-1]:
                current = current[key]
            current[keys[-1]] = value
            with self.subTest(path=path.name, field=".".join(keys)):
                self.reject(lambda: OPERATOR.load_published_operator(args))
        self.assertTrue(reads)

    def test_primary_sql_target_and_independent_start_times_are_exact(self):
        self.replace(OPERATOR, "LOCK_HELD", True)
        args = self.arguments()
        process = {"pid": 777, "start_ticks": "100001", "boot_id": BOOT_ID,
                   "pid_file": ["777", "/var/lib/postgresql/17/main", "1789100000", "5432"]}
        sql = {"system_identifier": SYSTEM_IDENTIFIER, "port": 5432, "version": 170011,
               "data_directory": "/var/lib/postgresql/17/main", "start_microseconds": 1789100000123456}
        calls = []

        def command(arguments, **kwargs):
            self.assertEqual(arguments[:3], ["/usr/sbin/runuser", "-u", "postgres"])
            self.assertEqual(arguments[-2:], ["-p", "5432"])
            self.assertIn("BEGIN READ ONLY", kwargs["input_bytes"].decode())
            calls.append(arguments)
            return OPERATOR.canonical(sql)

        dependency = types.SimpleNamespace(RECOVERY={"database": "goby_recovery_m5j"}, command=command,
                                          read_primary_process=lambda: copy.deepcopy(process))
        proof = OPERATOR.primary_proof(dependency, args)
        self.assertEqual(proof, {"process": process, "sql": sql})
        for key, value in (("system_identifier", "other"), ("port", 15432), ("version", 160000),
                           ("start_microseconds", True), ("data_directory", "/foreign/cluster")):
            original = sql[key]
            sql[key] = value
            with self.subTest(field=key):
                self.reject(lambda: OPERATOR.primary_proof(dependency, args))
            sql[key] = original
        prior_calls = len(calls)
        self.reject(lambda: OPERATOR.pg(dependency, "SELECT 1;", database="goby_client_m3e"))
        environment = {"PGHOST": "127.0.0.1", "PGPORT": "5432", "PGDATABASE": "goby_test", "PGUSER": "goby_test"}
        for key, value in (("PGPORT", "15432"), ("PGHOST", "remote.invalid"), ("PGDATABASE", "postgres"), ("PGUSER", "postgres")):
            changed = dict(environment, **{key: value})
            with self.subTest(environment=key):
                self.reject(lambda: OPERATOR.pg(dependency, "SELECT 1;", database="goby_test", environment=changed))
        self.assertEqual(len(calls), prior_calls)
        run_id = "run-20260911T080000Z-" + "3" * 24
        name = "goby_upgrade26_m3e_" + "4" * 24
        comment = OPERATOR.MARKER + ":" + run_id
        receipt = {"marker": OPERATOR.MARKER, "phase": "created", "run_id": run_id,
            "target": {"database": name, "database_oid": 20001, "role": name, "role_oid": 20002, "owner_comment": comment},
            "start_microseconds": sql["start_microseconds"], "database_acl": None}
        observed = {"system_identifier": SYSTEM_IDENTIFIER, "start_microseconds": sql["start_microseconds"],
            "role": {"name": name, "oid": 20002, "comment": comment, "safe": True, "scram": True, "login": True,
                     "inherit": False, "limit": 4, "config": None, "valid_until": None, "memberships": 0, "settings": 0, "outside_dependencies": 0},
            "database": {"name": name, "oid": 20001, "owner_oid": 20002, "comment": comment, "allow": True,
                         "template": False, "encoding": "UTF8", "acl": None, "clients": 0, "prepared": 0, "slots": 0}}
        OPERATOR.validate_rehearsal(receipt, observed)
        statement = OPERATOR.rehearsal_observation_sql(name)
        self.assertIn("BEGIN READ ONLY", statement)
        self.assertIn("FROM pg_authid r WHERE r.rolname='" + name + "'", statement)
        self.assertIn("'config',(SELECT v.rolconfig FROM pg_roles v WHERE v.oid=r.oid)", statement)
        self.assertNotIn("r.rolconfig", statement)
        self.assertIn("'scram',r.rolpassword LIKE 'SCRAM-SHA-256$%'", statement)
        target_oid = "COALESCE((SELECT oid FROM pg_database WHERE datname='" + name + "'),0)"
        self.assertIn("s.dbid=" + target_oid + " AND s.dbid<>0", statement)
        self.assertIn("s.objid=" + target_oid + " AND s.deptype='o'", statement)
        self.assertEqual(statement.count(target_oid), 2)
        self.assertNotRegex(statement, r"(?i)\b(CREATE|ALTER|DELETE|UPDATE|DROP|TRUNCATE)\b")
        inspection = Mock(return_value=OPERATOR.canonical({"database": None, "role": None}))
        with patch.object(OPERATOR, "pg", inspection):
            self.assertEqual(OPERATOR.observe_rehearsal(dependency, name), {"database": None, "role": None})
            inspection.assert_called_once_with(dependency, statement)
            for invalid in (None, "goby_test", "postgres", name + "'", name + "\n"):
                self.reject(lambda: OPERATOR.rehearsal_observation_sql(invalid))
                self.reject(lambda: OPERATOR.observe_rehearsal(dependency, invalid))
            self.assertEqual(inspection.call_count, 1)
        for section, key, value in (("role", "memberships", 1), ("role", "outside_dependencies", 1),
                                    ("role", "safe", False), ("role", "config", ["search_path=untrusted"]),
                                    ("role", "scram", False), ("database", "clients", 1), ("database", "prepared", 1),
                                    ("database", "slots", 1), ("database", "oid", 16385), ("database", "acl", ["unreviewed"])):
            changed = copy.deepcopy(observed)
            changed[section][key] = value
            with self.subTest(rehearsal=section + "." + key):
                self.reject(lambda: OPERATOR.validate_rehearsal(receipt, changed))
        changed = copy.deepcopy(receipt)
        changed["target"]["database_oid"] = 16385
        self.reject(lambda: OPERATOR.validate_rehearsal(changed, observed))
        journal = types.SimpleNamespace(run=OPERATOR.ROOT / run_id)
        self.reject(lambda: OPERATOR.restore_rehearsal(types.SimpleNamespace(), journal,
                    {"target": dict(OPERATOR.MAIN)}, "never-used-private-url", {}))
        state = {"schema": "goby-main-schema26-migration", "state_sha256": "5" * 64,
                 "state": {"tables": [{"name": "users", "rows": 2, "sha256": "6" * 64}],
                           "sequences": {"existing_sequence": {"last_value": "9007199254740993", "is_called": True}}}}
        OPERATOR.compare_snapshots(state, copy.deepcopy(state))
        for field in ("tables", "sequences"):
            changed = copy.deepcopy(state)
            changed["state"][field] = []
            with self.subTest(preservation=field):
                self.reject(lambda: OPERATOR.compare_snapshots(state, changed))

    def test_fresh_evidence_refuses_adoption_and_retains_partial_publication(self):
        dependency, memory = self.evidence_fixture()
        run_id = "run-20260911T080000Z-" + "1" * 24
        inputs, observed = {"candidate": "synthetic"}, {"quiescent": True}
        memory.directories.add(OPERATOR.ROOT)
        self.reject(lambda: OPERATOR.fresh_evidence(dependency, inputs, observed, run_id))
        self.assertEqual(memory.events, [])
        memory.directories.remove(OPERATOR.ROOT)
        memory.fail_name = "inputs.json"
        with self.assertRaises(OSError):
            OPERATOR.fresh_evidence(dependency, inputs, observed, run_id)
        self.assertIn(OPERATOR.ROOT, memory.directories)
        self.assertIn(OPERATOR.ROOT / run_id / "OWNER.json", memory.files)
        before = copy.deepcopy((memory.files, memory.events))
        self.reject(lambda: OPERATOR.fresh_evidence(dependency, inputs, observed, run_id))
        self.assertEqual((memory.files, memory.events), before)

    def test_intent_chain_is_exclusive_input_bound_and_never_advances_on_failure(self):
        dependency, memory = self.evidence_fixture()
        run_id = "run-20260911T080000Z-" + "2" * 24
        run = OPERATOR.fresh_evidence(dependency, {"candidate": "synthetic"}, {"quiescent": True}, run_id)
        journal = OPERATOR.Journal(dependency, run)
        first = journal.intent("stop", {"pid": 539535})
        self.assertEqual((journal.sequence, journal.previous), (1, first))
        first_raw = memory.files[run / "intent-0001.json"]
        self.reject(lambda: OPERATOR.append_intent(dependency, run, 1, "stop", None, {}))
        self.reject(lambda: OPERATOR.append_intent(dependency, run, 2, "restore-main", first, {}))
        self.reject(lambda: OPERATOR.append_intent(dependency, run, 3, "start", first, {}))
        self.reject(lambda: OPERATOR.append_intent(dependency, run, 2, "start", "0" * 64, {}))
        memory.fail_name = "intent-0002.json"
        with self.assertRaises(OSError):
            journal.intent("baseline-dump", {"database": "goby_test"})
        self.assertEqual((journal.sequence, journal.previous), (1, first))
        memory.fail_name = None
        original_inputs = memory.files[run / "inputs.json"]
        memory.files[run / "inputs.json"] = b'{"candidate":"changed"}\n'
        self.reject(lambda: journal.intent("baseline-dump", {}))
        memory.files[run / "inputs.json"] = original_inputs
        second = journal.intent("baseline-dump", {"database": "goby_test"})
        self.assertEqual((journal.sequence, journal.previous), (2, second))
        self.assertEqual(memory.files[run / "intent-0001.json"], first_raw)
        memory.files[run / "terminal.json"] = OPERATOR.canonical({"status": "failed"})
        self.reject(lambda: journal.intent("start", {}))
        self.assertEqual(journal.sequence, 2)

    def test_upgrade_orders_bounded_phases_retains_failures_and_revokes_only_its_smoke_cookie(self):
        real_smoke = OPERATOR.native_smoke
        real_verify_backup = OPERATOR.verify_backup
        scenarios = [(False, fault) for fault in (
            None, "artifacts", "stop", "materials", "baseline", "dump", "rehearsal", "recheck", "backup-check",
            "migrate", "post-migrate", "install", "start", "ready", "smoke")]
        scenarios += [(True, fault) for fault in (None, "repair-authority", "baseline", "migrate", "start")]
        scenarios += [(2, fault) for fault in (None, "old-baseline-drift", "migrate", "start")]
        for repair_level, fault in scenarios:
            repairing, second = bool(repair_level), repair_level == 2
            with self.subTest(repair=repair_level, phase=fault), contextlib.ExitStack() as scope:
                selected_root = OPERATOR.REHEARSAL_REPAIR_ROOT if second else OPERATOR.REPAIR_ROOT if repairing else OPERATOR.NORMAL_ROOT
                scope.enter_context(patch.object(OPERATOR, "ROOT", selected_root))
                dependency, memory = self.evidence_fixture()
                args = self.arguments()
                args.mode = "rehearsal-repair" if second else "repair-baseline" if repairing else "upgrade"
                args.failure_report_sha256 = "8" * 64 if repairing else None
                args.rehearsal_failure_report_sha256 = "7" * 64 if second else None
                events, exported = [], {"open": False}
                inputs = {"source_manifest_sha256": "a" * 64, "candidate": {"sha256": "b" * 64}, "asset_members": {},
                          "repair_baseline": repairing and not second, "repair_rehearsal": second, "failure_report": None}
                before = {"runtime": {"sha256": "c" * 64}, "stores": {"backups": []}, "assets": {"files": {}}, "diagnostics": {}}
                old_files = {}
                failed_tree = {"synthetic_failed_operation": "immutable"}
                if repairing:
                    before["failed_upgrade"] = copy.deepcopy(failed_tree)
                    old_files = {OPERATOR.NORMAL_ROOT / "OWNER.json": b"retained old root owner",
                                 OPERATOR.FAILED_RUN / "terminal.json": b"retained failed terminal"}
                    memory.files.update(old_files)
                    memory.directories.update({OPERATOR.NORMAL_ROOT, OPERATOR.FAILED_RUN})
                    if second:
                        before["failed_baseline_repair"] = copy.deepcopy(failed_tree)
                        old_files.update({OPERATOR.REPAIR_ROOT / "OWNER.json": b"retained baseline-repair root owner",
                            OPERATOR.REHEARSAL_FAILED_RUN / "terminal.json": b"retained second failed terminal",
                            OPERATOR.REHEARSAL_FAILED_RUN / "baseline.json": b"retained second baseline",
                            OPERATOR.REHEARSAL_FAILED_RUN / "database-schema25.dump": b"retained second dump"})
                        memory.files.update(old_files)
                        memory.directories.update({OPERATOR.REPAIR_ROOT, OPERATOR.REHEARSAL_FAILED_RUN})
                observed = {"service": ({"pid": 0, "binary_sha256": OLD_BINARY_SHA} if repairing else
                                        {"pid": 539535, "start_ticks": "3115871"}),
                            "primary": {"process": {"pid": 777}, "sql": {"start_microseconds": 1789100000123456}},
                            "protected": {key: value for key, value in before.items() if key != "diagnostics"},
                            "failed_tree_sha256": OPERATOR.sha(OPERATOR.canonical(failed_tree))}
                active = {"pid": 900001, "start_ticks": "4000000"}
                baseline = {"schema": "goby-main-schema26-migration", "state_sha256": "d" * 64,
                            "repair_baseline": repairing and not second, "repair_rehearsal": second,
                            "state": {"schema_version": 25, "old_rows": "preserved"}}
                migrated = {"schema": "goby-main-schema26-migration", "state_sha256": "e" * 64,
                            "repair_baseline": repairing and not second, "repair_rehearsal": second,
                            "state": {"schema_version": 26, "old_rows": "preserved"}}
                dump = {"sha256": "f" * 64, "identity": {"bytes": 1024}}
                material_facts = {"goby-source18": {"sha256": OLD_BINARY_SHA}, "runtime.env": {"sha256": "9" * 64}}

                def event(name):
                    events.append(name)
                    if name == fault:
                        raise RuntimeError("private-failure-sentinel")

                def stop(_op, _args, journal, _observed):
                    self.assertFalse(repairing, "Repair attempted to stop or restart the old service again.")
                    journal.intent("stop", {"pid": 539535})
                    event("stop")

                def repair_authority(*_args):
                    event("repair-authority")
                    self.assertEqual({path: memory.files[path] for path in old_files}, old_files)
                    retained = copy.deepcopy(baseline)
                    retained.update(repair_baseline=True, repair_rehearsal=False)
                    if fault == "old-baseline-drift":
                        retained["state"]["old_rows"] = "different retained state"
                    return {"failed_tree": copy.deepcopy(failed_tree), "baseline": retained,
                            "attestation_sha256": args.rehearsal_failure_report_sha256 if second else args.failure_report_sha256}

                def materials(_op, journal, _before):
                    journal.intent("save-materials", {})
                    event("materials")
                    memory.write(journal.run / "materials.json", OPERATOR.canonical({
                        "files": {name: value["sha256"] for name, value in material_facts.items()},
                        "old_binary_sha256": OLD_BINARY_SHA, "automatic_restore_supported": False}))

                def verify_backup(_op, journal, receipt):
                    event("backup-check")
                    return real_verify_backup(_op, journal, receipt)

                def file_fact(path, **_kwargs):
                    self.assertEqual(path.name, "database-schema25.dump")
                    self.assertIn(path, memory.files)
                    return copy.deepcopy(dump)

                def material_tree(path, uid, gid):
                    self.assertEqual((path.name, uid, gid), ("materials", 0, 0))
                    self.assertIn(path.parent / "materials.json", memory.files)
                    return {"files": copy.deepcopy(material_facts)}

                @contextlib.contextmanager
                def snapshot(_environment):
                    event("snapshot-open")
                    exported["open"] = True
                    try:
                        yield "AB-CD-1"
                    finally:
                        exported["open"] = False
                        events.append("snapshot-close")

                def helper(_op, _args, _inputs, journal, target, _url, mode, label, **options):
                    self.assertEqual(target, OPERATOR.MAIN)
                    self.assertIn(mode, ("inspect", "migrate"))
                    phase = {"baseline": "baseline", "main-before-migration": "recheck",
                             "main-migrated": "migrate", "main-after-migration": "post-migrate"}[label]
                    if label == "baseline":
                        self.assertTrue(exported["open"])
                        self.assertEqual(options.get("snapshot"), "AB-CD-1")
                    if mode == "migrate":
                        self.assertEqual(options.get("baseline"), journal.run / "baseline.json")
                        journal.intent("main-migrate", {"target": target})
                    event(phase)
                    result = migrated if phase in ("migrate", "post-migrate") else baseline
                    memory.write(journal.run / (label + ".json"), OPERATOR.canonical(result))
                    return copy.deepcopy(result)

                def dump_main(_op, journal, _environment, snapshot_id):
                    self.assertTrue(exported["open"])
                    self.assertEqual(snapshot_id, "AB-CD-1")
                    event("dump")
                    memory.write(journal.run / "database-schema25.dump", b"private synthetic fresh dump")
                    return copy.deepcopy(dump)

                def rehearsal(_op, _args, _inputs, journal, _dump):
                    self.assertFalse(exported["open"])
                    event("rehearsal")
                    memory.write(journal.run / "rehearsal-disposed.json", OPERATOR.canonical({"status": "disposed"}))
                    return copy.deepcopy(migrated)

                def artifacts(*_args):
                    event("artifacts")
                    return copy.deepcopy(inputs)

                def command(arguments, **_kwargs):
                    self.assertEqual(arguments, ["/usr/bin/systemctl", "start", OPERATOR.SERVICE])
                    event("start")
                    return b""

                dependency.runtime_policy = lambda: "private-memory-only-url"
                dependency.database_environment = lambda *_args, **_kwargs: {"PGPORT": "5432", "PGDATABASE": "goby_test"}
                dependency.exported_snapshot = snapshot
                dependency.cluster_state = lambda: None
                dependency.quiescent = Mock(side_effect=AssertionError("The new upgrade called the frozen legacy quiescent policy."))
                dependency.command = command
                dependency.file_fact = file_fact
                dependency.tree_state = material_tree
                replacements = {
                    "preflight": lambda *_args: copy.deepcopy(observed), "candidate_inputs": artifacts,
                    "repair_preflight": lambda *_args: copy.deepcopy(observed), "read_failure_evidence": repair_authority,
                    "read_rehearsal_failure_evidence": repair_authority,
                    "stop_main": stop, "protected_state": lambda *_args, **_kwargs: copy.deepcopy(before),
                    "save_materials": materials, "invoke_helper": helper, "dump_snapshot": dump_main,
                    "rehearse": rehearsal, "preserved_private": lambda *_args, **_kwargs: copy.deepcopy(before),
                    "verify_backup": verify_backup,
                    "exact_service": lambda *_args, **options: copy.deepcopy(
                        observed["service"] if repairing and options.get("active") is False else active),
                    "install_candidate": lambda *_args: event("install"),
                    "database_activity": lambda *_args: {"sessions": 0, "prepared": 0, "slots": 0},
                    "wait_ready": lambda *_args: (event("ready"), copy.deepcopy(active))[1],
                    "native_smoke": lambda *_args: event("smoke"),
                    "primary_proof": lambda *_args: copy.deepcopy(observed["primary"]),
                }
                for name, replacement in replacements.items():
                    scope.enter_context(patch.object(OPERATOR, name, replacement))
                scope.enter_context(patch.object(secrets, "token_hex", return_value="7" * 24))
                if fault is None:
                    self.assertEqual(OPERATOR.upgrade(dependency, args, inputs, {})["status"], "passed")
                    for earlier, later in ((("repair-authority" if repairing else "stop"), "baseline"), ("baseline", "dump"), ("dump", "rehearsal"),
                                           ("rehearsal", "recheck"), ("recheck", "backup-check"), ("backup-check", "migrate"), ("migrate", "install"),
                                           ("install", "start"), ("start", "ready"), ("ready", "smoke")):
                        self.assertLess(events.index(earlier), events.index(later))
                elif fault == "old-baseline-drift":
                    self.reject(lambda: OPERATOR.upgrade(dependency, args, inputs, {}))
                    self.assertNotIn("dump", events)
                    self.assertNotIn("migrate", events)
                else:
                    with self.assertRaisesRegex(RuntimeError, "private-failure-sentinel"):
                        OPERATOR.upgrade(dependency, args, inputs, {})
                terminals = [OPERATOR.decode(raw) for path, raw in memory.files.items()
                             if path.name == "terminal.json" and path.is_relative_to(OPERATOR.ROOT)]
                self.assertEqual(len(terminals), 1)
                self.assertEqual(terminals[0]["status"], "passed" if fault is None else "failed")
                self.assertIs(terminals[0]["main_database_restored"], False)
                self.assertIs(terminals[0]["old_binary_rollback"], False)
                self.assertNotIn("private-failure-sentinel", OPERATOR.canonical(terminals[0]).decode())
                self.assertLessEqual(events.count("stop"), 1)
                if repairing:
                    self.assertNotIn("stop", events)
                    self.assertEqual({path: memory.files[path] for path in old_files}, old_files)
                    self.assertFalse(any(path.is_relative_to(OPERATOR.NORMAL_ROOT) and path not in old_files for path in memory.files))
                    if second:
                        self.assertFalse(any(path.is_relative_to(OPERATOR.REPAIR_ROOT) and path not in old_files for path in memory.files))
                    if fault is None:
                        self.assertIs(terminals[0]["repair_baseline"], not second)
                        self.assertIs(terminals[0]["repair_rehearsal"], second)
                        intents = [OPERATOR.decode(raw)["action"] for path, raw in memory.files.items()
                                   if path.is_relative_to(OPERATOR.ROOT) and path.name.startswith("intent-")]
                        self.assertEqual(intents[:3], ["repair-rehearsal" if second else "repair-baseline", "save-materials", "baseline-dump"])
                self.assertLessEqual(events.count("migrate"), 1)
                self.assertLessEqual(events.count("start"), 1)
                saved_events = list(events)
                saved_files = dict(memory.files)
                self.reject(lambda: OPERATOR.upgrade(dependency, args, inputs, {}))
                self.assertEqual(events, saved_events)
                self.assertEqual(memory.files, saved_files)

        for fault in (None, "private-ack", "login-body", "wrong-identity", "native-read", "logout-intent", "logout", "revoke-check"):
            with self.subTest(smoke=fault), contextlib.ExitStack() as scope:
                dependency, memory = self.evidence_fixture()
                args = self.arguments()
                run = OPERATOR.fresh_evidence(dependency, {}, {}, "run-20260911T080000Z-" + "8" * 24)
                journal = OPERATOR.Journal(dependency, run)
                active = {"pid": 900001, "start_ticks": "4000000"}
                index = b"<html>Owned synthetic native index</html>"
                inputs = {"candidate": {"sha256": "b" * 64}, "asset_members": {"index.html": OPERATOR.sha(index)}}
                issued = "a" * 48
                csrf = OPERATOR.sha(("goby:admin:csrf:" + issued).encode())
                calls, state = [], {"revoked": False}
                dependency.BROWSER = WORK / "private-browser.env"
                dependency.SESSION = "/admin/v1/session"
                dependency.BACKUPS = "/admin/v1/backups"
                dependency.NATIVE_READS = (dependency.SESSION, dependency.BACKUPS, "/admin/v1/capabilities")
                dependency.private_values = lambda *_args: {"GOBY_SMOKE_NAME": "Owned administrator", "GOBY_SMOKE_PASSWORD": "private-memory-password"}
                dependency.file_fact = lambda *_args: {"sha256": "c" * 64}
                if fault == "private-ack":
                    memory.fail_name = "smoke-session-private.json"
                elif fault == "logout-intent":
                    memory.fail_name = "intent-0002.json"

                class Response:
                    def __init__(self, method, path):
                        self.method, self.path, self.status = method, path, 200
                        user = {"Id": "owned-admin", "Name": "Owned administrator", "IsAdministrator": True}
                        self.raw = OPERATOR.canonical({"User": user, "CSRFToken": csrf}) if path == dependency.SESSION else b"{}"
                        if path == "/admin/":
                            self.raw = index
                        if path == dependency.BACKUPS:
                            self.raw = OPERATOR.canonical({"Items": [{"Id": "old-archive", "SHA256": "d" * 64}]})
                        if method == "POST" and fault == "wrong-identity":
                            self.raw = OPERATOR.canonical({"User": dict(user, Name="Other administrator"), "CSRFToken": csrf})
                        if method == "DELETE":
                            self.status, self.raw = (500 if fault == "logout" else 204), b""
                            state["revoked"] = self.status == 204
                        elif method == "GET" and path == dependency.SESSION:
                            if state["revoked"]:
                                self.status = 200 if fault == "revoke-check" else 401
                                self.raw = b"{}"
                            elif fault == "native-read":
                                self.status = 500

                    def getheader(self, name, default=None):
                        return "goby_session=" + issued + "; Path=/admin" if self.method == "POST" and name == "Set-Cookie" else default

                    def read(self, limit):
                        if self.method == "POST":
                            if run / "smoke-session-private.json" not in memory.files:
                                raise AssertionError("The login body was read before the new cookie acknowledgement was durable.")
                            if fault == "login-body":
                                raise OSError("Synthetic bounded login-body failure.")
                        return self.raw[:limit]

                class Connection:
                    def __init__(self, host, port, **_kwargs):
                        if (host, port) != ("127.0.0.1", OPERATOR.PORT):
                            raise AssertionError("Smoke left its fixed loopback endpoint.")

                    def request(self, method, path, payload=None, headers=None):
                        calls.append((method, path))
                        if method == "POST":
                            if "Cookie" in headers:
                                raise AssertionError("Smoke authentication reused an existing cookie.")
                        elif "Cookie" in headers and headers["Cookie"] != "goby_session=" + issued:
                            raise AssertionError("Smoke used a cookie other than its newly issued one.")
                        self.response = Response(method, path)

                    def getresponse(self):
                        return self.response

                    def close(self):
                        pass

                scope.enter_context(patch.object(OPERATOR, "exact_service", return_value=active))
                scope.enter_context(patch.object(http.client, "HTTPConnection", Connection))
                if fault is None:
                    real_smoke(dependency, args, inputs, journal, active, [{"id": "old-archive", "sha256": "d" * 64}])
                else:
                    with self.assertRaises((OPERATOR.Failure, OSError)):
                        real_smoke(dependency, args, inputs, journal, active, [{"id": "old-archive", "sha256": "d" * 64}])
                self.assertEqual(calls.count(("POST", dependency.SESSION)), 1)
                self.assertEqual(calls.count(("DELETE", dependency.SESSION)), 1,
                                 "Every acknowledged new cookie needs one cleanup attempt, including acknowledgement publication failure.")
                smoke = OPERATOR.decode(memory.files[run / "native-smoke.json"])
                self.assertIs(smoke["new_session_only"], True)
                self.assertIs(smoke["logout_verified"], fault not in ("logout", "revoke-check"))
                self.assertIs(smoke["separate_logout_intent_written"], fault != "logout-intent")
                self.assertEqual(smoke["cleanup_checkpoint_error_type"], "OSError" if fault == "logout-intent" else None)
                self.assertEqual((smoke["backup_create_requests"], smoke["restore_requests"], smoke["archive_delete_requests"]), (0, 0, 0))

    def test_startup_plan_binds_exact_artifacts_configuration_and_observation_types(self):
        model = self.startup_fixture()
        args, dependency, plan = model["args"], model["op"], model["plan"]
        OPERATOR.validate_arguments(args)
        self.assertEqual(OPERATOR.load_startup_plan(dependency, args), plan)
        original = copy.deepcopy(plan)
        for keys, value in (
            (("schema",), "foreign-plan"), (("version",), True), (("target", "database_oid"), 999),
            (("source_manifest_sha256",), "0" * 64), (("full_report_sha256",), "0" * 64),
            (("candidate_sha256",), OLD_BINARY_SHA), (("deployment_receipt_sha256",), "0" * 64),
            (("service_pin_sha256",), "0" * 64), (("old_process", "start_ticks"), "3115872"),
            (("unit_sha256",), "0" * 64), (("runtime_env_sha256",), "0" * 64),
            (("retention", "effective_days"), 1), (("retention", "first_prune_delay_seconds"), True),
            (("retention", "prune_interval_seconds"), 0), (("retention", "batch_limit"), 2000),
            (("retention", "overrides_absent", "manager"), 1),
            (("retention", "code_sha256", "internal/activity/store.go"), "0" * 64),
            (("observed", "activity_total"), True), (("observed", "legacy_one_day_eligible"), 16),
            (("observed", "effective_retention_eligible"), 1),
            (("observed", "active", "active_runs"), False), (("startup_reserve_seconds",), 0),
        ):
            plan.clear()
            plan.update(copy.deepcopy(original))
            current = plan
            for key in keys[:-1]:
                current = current[key]
            current[keys[-1]] = value
            model["refresh"]()
            with self.subTest(field=".".join(keys)):
                self.reject(lambda: OPERATOR.load_startup_plan(dependency, args))
        plan.clear()
        plan.update(copy.deepcopy(original))
        model["refresh"]()
        args.startup_plan_sha256 = "0" * 64
        self.reject(lambda: OPERATOR.load_startup_plan(dependency, args))
        model["refresh"]()
        observation = Path(plan["evidence"]["counts"]["path"])
        model["documents"][observation] = b'{"observation":"changed"}\n'
        self.reject(lambda: OPERATOR.load_startup_plan(dependency, args))

    def test_startup_plan_rejects_ambiguous_json_unbounded_windows_and_early_expiry(self):
        model = self.startup_fixture()
        args, dependency, plan = model["args"], model["op"], model["plan"]
        original = copy.deepcopy(plan)
        for raw in (b'{"schema":"one","schema":"two"}', b'{"observed":{"n":NaN}}', b"{", b"\xff"):
            model["documents"][args.startup_plan] = raw
            args.startup_plan_sha256 = OPERATOR.sha(raw)
            with self.subTest(raw=raw), self.assertRaises((OPERATOR.Failure, ValueError, UnicodeDecodeError)):
                OPERATOR.load_startup_plan(dependency, args)
        for deadline in ("2026-09-11T18:31:45.045807Z", "2026-09-11T12:46:45.045806Z",
                         "2026-09-11T12:30:00.000000Z", "2026-09-11T18:31:45Z",
                         "2026-09-11T18:31:45.045806+00:00", "2026-13-11T18:31:45.045806Z"):
            plan.clear()
            plan.update(copy.deepcopy(original))
            plan["deadline_utc"] = deadline
            model["refresh"]()
            with self.subTest(deadline=deadline):
                self.reject(lambda: OPERATOR.load_startup_plan(dependency, args))
        plan.clear()
        plan.update(copy.deepcopy(original))
        # Not thirty days old at observation, but due before the planned end.
        plan["observed"]["earliest_activity"] = "2026-08-12T15:00:00.000000Z"
        plan["observed"]["effective_retention_eligible"] = 0
        model["refresh"]()
        self.reject(lambda: OPERATOR.load_startup_plan(dependency, args))
        plan.clear()
        plan.update(copy.deepcopy(original))
        plan["observed"].update(activity_total=0, earliest_activity=None, legacy_one_day_eligible=0)
        model["refresh"]()
        self.assertEqual(OPERATOR.load_startup_plan(dependency, args)["observed"]["activity_total"], 0)

    def test_retention_environment_checks_all_override_sources_without_leaking_secrets(self):
        model = self.startup_fixture()
        args, dependency, plan = model["args"], model["op"], model["plan"]
        documents = model["documents"]
        state = {"properties": {"MainPID": "539535", "ActiveState": "active", "SubState": "running",
                 "Environment": 'OTHER="private-unit-sentinel"', "PassEnvironment": "OTHER", "UnsetEnvironment": "UNUSED"},
                 "manager": b"OTHER=private-manager-sentinel\n", "process": b"OTHER=private-process-sentinel\0"}
        baseline = copy.deepcopy(state)
        original_env = {path: documents[path] for path in (dependency.RUNTIME, dependency.RECOVERY_ENV)}
        commands = []

        def command(arguments, **_kwargs):
            commands.append(arguments)
            if arguments == ["/usr/bin/systemctl", "show-environment"]:
                return state["manager"]
            self.assertEqual(arguments[:3], ["/usr/bin/systemctl", "show", OPERATOR.SERVICE])
            return "\n".join(key + "=" + value for key, value in state["properties"].items()).encode()

        def properties(raw, names):
            result = dict(line.split("=", 1) for line in raw.decode().splitlines())
            self.assertEqual(set(result), set(names))
            return result

        def fact(path, **_kwargs):
            if path == dependency.UNIT:
                return {"sha256": dependency.UNIT_SHA}
            if path in dependency.DROPINS:
                return {"sha256": dependency.DROPIN_SHAS[dependency.DROPINS.index(path)]}
            self.assertIn(path, documents)
            return {"sha256": OPERATOR.sha(documents[path])}

        def read_text(path, *_args, **_kwargs):
            if path == Path("/proc/sys/kernel/random/boot_id"):
                return BOOT_ID + "\n"
            self.assertEqual(path, Path("/proc") / state["properties"]["MainPID"] / "cgroup")
            return "0::/system.slice/" + OPERATOR.SERVICE + "\n"

        def open_process(path, mode="r", **_kwargs):
            self.assertEqual((path, mode), (Path("/proc") / state["properties"]["MainPID"] / "environ", "rb"))
            return io.BytesIO(state["process"])

        def service_state(*, active, expected_binary):
            self.assertIs(active, True)
            self.assertEqual(expected_binary, OLD_BINARY_SHA if state["properties"]["MainPID"] == "539535" else args.candidate_sha256)
            return {"MainPID": state["properties"]["MainPID"], "start_ticks": "3115871"}

        dependency.command, dependency.systemd_properties = command, properties
        dependency.file_fact, dependency.service_state = fact, service_state
        self.replace(Path, "read_text", read_text)
        self.replace(Path, "open", open_process)
        self.replace(os, "readlink", lambda path: str(OPERATOR.LIVE / "goby"))
        observed = OPERATOR.retention_environment(dependency, args, plan)
        self.assertTrue(all(observed["overrides_absent"].values()))
        self.assertTrue(observed["process_environment_checked"])
        self.assertNotIn("private-", OPERATOR.canonical(observed).decode())
        for source in OPERATOR.ABSENT_SOURCES:
            state.clear()
            state.update(copy.deepcopy(baseline))
            documents.update(original_env)
            for key, path in (("runtime_env", dependency.RUNTIME), ("recovery_env", dependency.RECOVERY_ENV)):
                plan[key + "_sha256"] = OPERATOR.sha(documents[path])
            if source == "process":
                state["process"] += b"GOBY_ACTIVITY_RETENTION_DAYS=1\0"
            elif source == "manager":
                state["manager"] += b"GOBY_ACTIVITY_RETENTION_DAYS=1\n"
            elif source in ("runtime_env", "recovery_env"):
                path = dependency.RUNTIME if source == "runtime_env" else dependency.RECOVERY_ENV
                documents[path] += b" export GOBY_ACTIVITY_RETENTION_DAYS='1'\n"
                plan[source + "_sha256"] = OPERATOR.sha(documents[path])
            else:
                field = {"unit": "Environment", "pass_environment": "PassEnvironment", "unset_environment": "UnsetEnvironment"}[source]
                state["properties"][field] = "GOBY_ACTIVITY_RETENTION_DAYS=1" if source == "unit" else "GOBY_ACTIVITY_RETENTION_DAYS"
            with self.subTest(source=source):
                self.reject(lambda: OPERATOR.retention_environment(dependency, args, plan))
        state.clear()
        state.update(copy.deepcopy(baseline))
        documents.update(original_env)
        for key, path in (("runtime_env", dependency.RUNTIME), ("recovery_env", dependency.RECOVERY_ENV)):
            plan[key + "_sha256"] = OPERATOR.sha(documents[path])
        state["properties"].update(MainPID="0", ActiveState="inactive", SubState="dead")
        stopped = OPERATOR.retention_environment(dependency, args, plan)
        self.assertFalse(stopped["process_environment_checked"])
        self.assertEqual(stopped["observed_pid"], 0)
        code_path = args.source / "internal/activity/store.go"
        documents[code_path] += b"// Changed retention behavior.\n"
        self.reject(lambda: OPERATOR.retention_environment(dependency, args, plan))
        self.assertTrue(commands)

    def test_startup_gate_preserves_one_day_history_and_allows_legal_append(self):
        model = self.startup_gate_fixture()
        args, dependency, current = model["args"], model["op"], model["current"]
        result = OPERATOR.startup_plan_gate(dependency, args)
        self.assertEqual(result["effective_retention_days"], 30)
        self.assertEqual(result["legacy_one_day_eligible"], 6)
        self.assertEqual(result["observed_legacy_one_day_eligible"], 6)
        self.assertTrue(result["legacy_one_day_rows_are_preserved"])
        self.assertEqual(result["deadline_eligible"], 0)
        current["activity_total"] = 18
        self.assertEqual(OPERATOR.startup_plan_gate(dependency, args)["activity_total"], 18)
        self.assertEqual(current["earliest_activity"], model["plan"]["observed"]["earliest_activity"])

    def test_startup_gate_uses_database_time_and_reserved_start_login_window(self):
        model = self.startup_gate_fixture()
        args, dependency, current = model["args"], model["op"], model["current"]
        for value in ("2026-09-11T12:31:45.045805Z", "2026-09-11T18:31:45.045807Z"):
            current["database_clock"] = value
            with self.subTest(clock=value):
                self.reject(lambda: OPERATOR.startup_plan_gate(dependency, args))
        current["database_clock"] = "2026-09-11T18:16:45.045806Z"
        for action in ("start", "smoke-login"):
            self.assertEqual(OPERATOR.startup_plan_gate(dependency, args, action=action)["database_clock"], current["database_clock"])
        current["database_clock"] = "2026-09-11T18:16:45.045807Z"
        OPERATOR.startup_plan_gate(dependency, args, action="main-migrate")
        for action in ("start", "smoke-login"):
            with self.subTest(action=action):
                self.reject(lambda: OPERATOR.startup_plan_gate(dependency, args, action=action))

    def test_startup_gate_rejects_active_work_lost_history_and_window_expiry(self):
        model = self.startup_gate_fixture()
        args, dependency, current = model["args"], model["op"], model["current"]
        baseline = copy.deepcopy(current)
        for counter in OPERATOR.ACTIVE_COUNTERS:
            for value in (1, False):
                current.clear()
                current.update(copy.deepcopy(baseline))
                current["active"][counter] = value
                with self.subTest(counter=counter, value=value):
                    self.reject(lambda: OPERATOR.startup_plan_gate(dependency, args))
        for key, value in (("activity_total", 14), ("earliest_activity", "2026-09-10T10:26:41.150556Z"),
                           ("effective_retention_eligible", 1), ("deadline_eligible", 1),
                           ("legacy_one_day_eligible", -1), ("activity_total", True)):
            current.clear()
            current.update(copy.deepcopy(baseline))
            current[key] = value
            with self.subTest(field=key):
                self.reject(lambda: OPERATOR.startup_plan_gate(dependency, args))
        # An initially empty population can gain a backdated row. It is not
        # due now, but cannot be accepted if it becomes due inside the window.
        model["plan"]["observed"].update(activity_total=0, earliest_activity=None, legacy_one_day_eligible=0)
        model["refresh"]()
        current.clear()
        current.update(copy.deepcopy(baseline))
        current.update(activity_total=1, earliest_activity="2026-08-12T15:00:00.000000Z", legacy_one_day_eligible=1,
                       effective_retention_eligible=0, deadline_eligible=1)
        self.reject(lambda: OPERATOR.startup_plan_gate(dependency, args))

    def test_startup_context_checks_before_each_intent_without_advancing_on_rejection(self):
        model = self.startup_gate_fixture()
        args, gate_dependency, current = model["args"], model["op"], model["current"]
        journal_dependency, memory = self.evidence_fixture()
        run = OPERATOR.fresh_evidence(journal_dependency, {}, {}, "run-20260911T080000Z-" + "9" * 24)
        journal = OPERATOR.Journal(journal_dependency, run)
        self.replace(OPERATOR, "startup_checkpoint", self.real_startup_checkpoint)
        checks, actions = [], []

        def counts(*_args):
            checks.append("counts")
            self.assertIsNone(self.real_startup_checkpoint(required=True), "The active gate recursively entered itself.")
            return copy.deepcopy(current)

        self.replace(OPERATOR, "startup_counts", counts)
        self.reject(lambda: journal.intent("stop", {}))
        with OPERATOR.startup_plan_checks(gate_dependency, args):
            self.reject(lambda: OPERATOR.startup_plan_checks(gate_dependency, args).__enter__())
            saved = dict(memory.files)
            current["active"]["active_encodings"] = 1
            with self.assertRaises(OPERATOR.Failure):
                journal.intent("stop", {"pid": 539535})
                actions.append("stop")
            self.assertEqual((journal.sequence, journal.previous), (0, None))
            self.assertEqual(memory.files, saved)
            self.assertEqual(actions, [])
            self.assertFalse(OPERATOR.STARTUP_CHECKING)
            current["active"]["active_encodings"] = 0
            first = journal.intent("stop", {"pid": 539535})
            actions.append("stop")
            entry = OPERATOR.decode(memory.files[run / "intent-0001.json"])
            self.assertEqual(entry["binding"]["startup_plan_check"]["startup_plan_sha256"], args.startup_plan_sha256)
            current["database_clock"] = "2026-09-11T18:31:45.045807Z"
            saved = dict(memory.files)
            self.reject(lambda: journal.intent("main-migrate", {}))
            self.assertEqual((journal.sequence, journal.previous), (1, first))
            self.assertEqual(memory.files, saved)
        self.assertIsNone(OPERATOR.STARTUP_CONTEXT)
        self.assertFalse(OPERATOR.STARTUP_CHECKING)
        self.assertEqual(actions, ["stop"])
        self.assertEqual(len(checks), 3)

    def test_repair_authority_is_explicit_and_operation_roots_are_restored(self):
        args = self.arguments()
        report = WORK / "candidate-40/baseline-failure.json"
        digest = "a" * 64
        self.assertEqual(OPERATOR.NORMAL_ROOT, Path("/opt/goby-test/backups/main-schema26-v1"))
        self.assertEqual(OPERATOR.REPAIR_ROOT, Path("/opt/goby-test/backups/main-schema26-baseline-repair-v1"))
        self.assertEqual(OPERATOR.REHEARSAL_REPAIR_ROOT, Path("/opt/goby-test/backups/main-schema26-rehearsal-repair-v1"))
        for mode in ("preflight", "upgrade"):
            args.mode = mode
            OPERATOR.validate_arguments(args)
            for path, value in ((report, None), (None, digest), (report, digest)):
                changed = copy.copy(args)
                changed.failure_report, changed.failure_report_sha256 = path, value
                with self.subTest(normal_mode=mode, path=path, digest=value):
                    self.reject(lambda: OPERATOR.validate_arguments(changed))
            with OPERATOR.operation_scope(args):
                self.assertEqual(OPERATOR.ROOT, OPERATOR.NORMAL_ROOT)
            changed = copy.copy(args)
            changed.rehearsal_failure_report, changed.rehearsal_failure_report_sha256 = report, digest
            self.reject(lambda: OPERATOR.validate_arguments(changed))
        self.assertEqual(OPERATOR.BASELINE_REPAIR_MODES, ("repair-preflight", "repair-baseline"))
        self.assertEqual(OPERATOR.REHEARSAL_REPAIR_MODES, ("rehearsal-repair-preflight", "rehearsal-repair"))
        self.assertEqual(OPERATOR.REPAIR_MODES, (*OPERATOR.BASELINE_REPAIR_MODES, *OPERATOR.REHEARSAL_REPAIR_MODES))
        for mode in OPERATOR.REPAIR_MODES:
            args.mode = mode
            args.failure_report, args.failure_report_sha256 = report, digest
            second = mode in OPERATOR.REHEARSAL_REPAIR_MODES
            args.rehearsal_failure_report = WORK / "candidate-40/rehearsal-failure.json" if second else None
            args.rehearsal_failure_report_sha256 = "b" * 64 if second else None
            OPERATOR.validate_arguments(args)
            with OPERATOR.operation_scope(args):
                self.assertEqual(OPERATOR.ROOT, OPERATOR.REHEARSAL_REPAIR_ROOT if second else OPERATOR.REPAIR_ROOT)
            for path, value in ((None, digest), (report, None), (report, "invalid"),
                                (Path("relative.json"), digest), (Path("/tmp/failure.json"), digest),
                                (WORK / "../outside/failure.json", digest), (WORK / "failure.txt", digest)):
                changed = copy.copy(args)
                changed.failure_report, changed.failure_report_sha256 = path, value
                with self.subTest(repair_mode=mode, repair_path=path, digest=value):
                    self.reject(lambda: OPERATOR.validate_arguments(changed))
            if second:
                for path, value in ((None, "b" * 64), (args.rehearsal_failure_report, None),
                                    (args.rehearsal_failure_report, "invalid"), (Path("/tmp/second.json"), "b" * 64)):
                    changed = copy.copy(args)
                    changed.rehearsal_failure_report, changed.rehearsal_failure_report_sha256 = path, value
                    self.reject(lambda: OPERATOR.validate_arguments(changed))
            else:
                changed = copy.copy(args)
                changed.rehearsal_failure_report, changed.rehearsal_failure_report_sha256 = report, digest
                self.reject(lambda: OPERATOR.validate_arguments(changed))
        args.mode = "repair-baseline"
        args.rehearsal_failure_report, args.rehearsal_failure_report_sha256 = None, None
        with self.assertRaisesRegex(RuntimeError, "synthetic scope failure"):
            with OPERATOR.operation_scope(args):
                self.assertEqual(OPERATOR.ROOT, OPERATOR.REPAIR_ROOT)
                with self.assertRaises(OPERATOR.Failure):
                    with OPERATOR.operation_scope(args):
                        self.fail("A nested repair scope acquired operation authority.")
                raise RuntimeError("synthetic scope failure")
        self.assertEqual(OPERATOR.ROOT, OPERATOR.NORMAL_ROOT)
        self.replace(OPERATOR, "LOCK_HELD", True)
        with self.assertRaises(OPERATOR.Failure):
            with OPERATOR.operation_scope(args):
                self.fail("A root changed after deployment ownership was acquired.")
        self.assertEqual(OPERATOR.ROOT, OPERATOR.NORMAL_ROOT)

    def test_repair_requires_the_complete_immutable_read_only_failure(self):
        model = self.failure_fixture()
        args, dependency, inputs = model["args"], model["op"], model["inputs"]
        documents, report, observation = model["documents"], model["report"], model["observation"]
        run = OPERATOR.FAILED_RUN
        accepted = OPERATOR.read_failure_evidence(dependency, args, inputs)
        self.assertEqual(accepted["failed_tree"], observation["inventory"])
        self.assertEqual(accepted["activity_total"], 15)
        saved_documents, saved_report, saved_observation = copy.deepcopy((documents, report, observation))

        def reset():
            documents.clear()
            documents.update(copy.deepcopy(saved_documents))
            report.clear()
            report.update(copy.deepcopy(saved_report))
            observation.clear()
            observation.update(copy.deepcopy(saved_observation))
            model["overrides"].clear()
            model["refresh"]()

        for keys, value in ((("version",), True), (("failed_root",), str(OPERATOR.REPAIR_ROOT)),
                            (("failed_run",), str(run.with_name("foreign-run"))), (("candidate_sha256",), OLD_BINARY_SHA),
                            (("source_manifest_sha256",), "0" * 64), (("error_code",), "unknown_commit"),
                            (("observed_schema",), 26), (("observed_activity_total",), True),
                            (("observation", "sha256"), "0" * 64), (("observation", "path"), str(WORK / "foreign.json"))):
            reset()
            current = report
            for key in keys[:-1]:
                current = current[key]
            current[keys[-1]] = value
            model["write_document"](args.failure_report, report)
            args.failure_report_sha256 = OPERATOR.sha(documents[args.failure_report])
            with self.subTest(attestation=".".join(keys)):
                self.reject(lambda: OPERATOR.read_failure_evidence(dependency, args, inputs))
        for path in (run / "materials/runtime.env", run / "terminal.json", model["observation_path"]):
            reset()
            documents[path] += b"changed"
            with self.subTest(unreviewed_content=path.name):
                self.reject(lambda: OPERATOR.read_failure_evidence(dependency, args, inputs))
        for attributes in ({"inode": 101}, {"mtime_ns": 1}, {"ctime_ns": 1}, {"links": 2}, {"mode": 0o644}, {"kind": stat.S_IFIFO}):
            reset()
            model["overrides"][run / "materials/runtime.env"] = attributes
            with self.subTest(identity=attributes):
                self.reject(lambda: OPERATOR.read_failure_evidence(dependency, args, inputs))
        for name in ("pending", "baseline", "dump", "later-intent", "terminal", "third-action", "error-code", "reserved"):
            reset()
            if name == "pending":
                documents[model["pending"]] = b"unknown result"
            elif name in ("baseline", "dump", "later-intent"):
                filename = {"baseline": "baseline.json", "dump": "database-schema25.dump", "later-intent": "intent-0004.json"}[name]
                documents[run / filename] = b"unexpected later artifact"
            elif name in ("terminal", "third-action"):
                path = run / ("terminal.json" if name == "terminal" else "intent-0003.json")
                value = OPERATOR.decode(documents[path])
                value["last_completed_phase" if name == "terminal" else "action"] = "migrated" if name == "terminal" else "main-migrate"
                model["write_document"](path, value)
            elif name == "error-code":
                path = run / ("baseline-" + "b" * 12 + ".stderr")
                value = OPERATOR.decode(documents[path])
                value["code"] = "transaction_outcome_unknown"
                model["write_document"](path, value)
            else:
                documents[run / ".baseline.json.reserved"] = b"foreign output reservation"
            # Rebind synthetic review hashes to reach structural checks as well.
            model["refresh"]()
            with self.subTest(failed_boundary=name):
                self.reject(lambda: OPERATOR.read_failure_evidence(dependency, args, inputs))
        reset()
        for raw in (b'{"schema":"one","schema":"two"}', b'{"version":NaN}', b"{"):
            documents[args.failure_report] = raw
            args.failure_report_sha256 = OPERATOR.sha(raw)
            with self.subTest(invalid_json=raw), self.assertRaises((OPERATOR.Failure, ValueError)):
                OPERATOR.read_failure_evidence(dependency, args, inputs)
        reset()
        args.mode = "upgrade"
        self.reject(lambda: OPERATOR.read_failure_evidence(dependency, args, inputs))

    def test_repair_preflight_requires_stopped_schema25_and_an_unused_repair_root(self):
        model = self.failure_fixture()
        args, dependency, inputs = model["args"], model["op"], model["inputs"]
        evidence = OPERATOR.read_failure_evidence(dependency, args, inputs)
        service, primary = copy.deepcopy(model["stopped"]["service"]), copy.deepcopy(model["original"]["primary"])
        state = {**OPERATOR.MAIN, "owner_oid": 16384, "schema": 25, "activity_total": 15, "safe": True, "memberships": 0}
        activity = {"sessions": 0, "prepared": 0, "slots": 0}
        rehearsals = {"databases": 0, "roles": 0}
        current = {**copy.deepcopy(model["before"]), "failed_upgrade": copy.deepcopy(evidence["failed_tree"])}
        old_process = {"exists": False, "ticks": "3115871"}
        queries = []
        dependency.runtime_policy = lambda: "private-synthetic-url"
        dependency.database_environment = lambda *_args, **_kwargs: {"PGPORT": "5432", "PGDATABASE": "goby_test"}
        dependency.cluster_state = lambda: None
        self.replace(OPERATOR, "exact_service", lambda *_args, **options: (self.assertIs(options.get("active"), False), copy.deepcopy(service))[1])
        self.replace(OPERATOR, "primary_proof", lambda *_args: copy.deepcopy(primary))
        self.replace(OPERATOR, "database_activity", lambda *_args: copy.deepcopy(activity))
        self.replace(OPERATOR, "protected_state", lambda *_args, **_kwargs: copy.deepcopy(current))
        self.replace(Path, "exists", lambda path: path == Path("/proc/539535") and old_process["exists"])
        self.replace(Path, "read_text", lambda path: "539535 (goby) " + " ".join(["0"] * 19 + [old_process["ticks"]]))

        def query(_op, statement, **options):
            self.assertIn("BEGIN READ ONLY", statement)
            self.assertNotRegex(statement, r"(?i)\b(DELETE|UPDATE|ALTER|DROP|TRUNCATE)\b")
            queries.append(statement)
            if "public.schema_migrations" in statement:
                self.assertEqual(options["database"], "goby_test")
                return OPERATOR.canonical(state)
            self.assertIn("pg_database", statement)
            return OPERATOR.canonical(rehearsals)

        self.replace(OPERATOR, "pg", query)
        accepted = OPERATOR.repair_preflight(dependency, args, inputs, model["published"])
        self.assertEqual(accepted["phase"], "repair-preflight")
        self.assertEqual(accepted["failed_tree_sha256"], OPERATOR.sha(OPERATOR.canonical(evidence["failed_tree"])))
        for container, key, value in ((service, "pid", 539535), (service, "binary_sha256", args.candidate_sha256),
                                     (primary["process"], "pid", 778), (state, "schema", 26), (state, "database_oid", 999),
                                     (state, "activity_total", 14), (state, "activity_total", 16), (state, "safe", False),
                                     (activity, "sessions", 1), (activity, "prepared", 1), (activity, "slots", 1),
                                     (rehearsals, "databases", 1), (rehearsals, "roles", 1),
                                     (current["runtime"], "sha256", "0" * 64), (old_process, "exists", True)):
            previous = container[key]
            container[key] = value
            with self.subTest(stopped_boundary=key, value=value):
                self.reject(lambda: OPERATOR.repair_preflight(dependency, args, inputs, model["published"]))
            container[key] = previous
        saved_query_count = len(queries)
        model["directories"].add(OPERATOR.REPAIR_ROOT)
        self.reject(lambda: OPERATOR.repair_preflight(dependency, args, inputs, model["published"]))
        self.assertEqual(len(queries), saved_query_count)

    def test_rehearsal_repair_binds_both_failures_and_the_complete_prior_backup(self):
        model = self.failure_fixture()
        args, dependency, inputs = model["args"], model["op"], model["inputs"]
        documents, write = model["documents"], model["write_document"]
        self.replace(OPERATOR, "ROOT", OPERATOR.REHEARSAL_REPAIR_ROOT)
        args.mode = "rehearsal-repair"
        args.rehearsal_failure_report = WORK / "candidate-40/rehearsal-failure.json"
        inputs.update(repair_baseline=False, repair_rehearsal=True,
                      failure_report={"path": str(args.failure_report), "sha256": args.failure_report_sha256})
        first = OPERATOR.read_failure_evidence(dependency, args, inputs)
        run = OPERATOR.REHEARSAL_FAILED_RUN
        model["directories"].update({OPERATOR.REPAIR_ROOT, run, run / "materials"})
        write(OPERATOR.REPAIR_ROOT / "OWNER.json", {"marker": OPERATOR.MARKER, "version": 1,
            "path": str(OPERATOR.REPAIR_ROOT), "run_id": run.name})
        write(run / "OWNER.json", {"marker": OPERATOR.MARKER, "version": 1, "run_id": run.name})
        tool_path = WORK / "tool-build-main26-second-failed"
        source_names = ("scripts/test-env/upgrade-main-schema26.py", "scripts/test-env/migrate-main-schema26.go")
        for name in source_names:
            documents[tool_path / name] = ("Synthetic second failed input: " + name).encode()
        self.replace(OPERATOR, "BASELINE_FAILED_OPERATOR_SHA", OPERATOR.sha(documents[tool_path / source_names[0]]))
        self.replace(OPERATOR, "BASELINE_FAILED_HELPER_SOURCE_SHA", OPERATOR.sha(documents[tool_path / source_names[1]]))
        helper_path = WORK / "candidate-40/second-failed-helper"
        documents[helper_path] = b"Synthetic second failed helper binary"
        old_inputs = OPERATOR.decode(documents[OPERATOR.FAILED_RUN / "inputs.json"])
        old_inputs.update(repair_baseline=True, repair_rehearsal=False, failure_report=copy.deepcopy(inputs["failure_report"]),
            tool_source={"path": str(tool_path), "manifest_sha256": "3" * 64,
                         "files": {name: OPERATOR.sha(documents[tool_path / name]) for name in source_names}},
            helper={"path": str(helper_path), **dependency.file_fact(helper_path)})
        write(run / "inputs.json", old_inputs)
        for path, raw in list(documents.items()):
            if path.parent == OPERATOR.FAILED_RUN / "materials":
                documents[run / "materials" / path.name] = raw
        documents[run / "materials.json"] = documents[OPERATOR.FAILED_RUN / "materials.json"]
        before = {**copy.deepcopy(model["before"]), "failed_upgrade": copy.deepcopy(first["failed_tree"])}
        original = {"phase": "repair-preflight", "inputs_sha256": OPERATOR.sha(documents[run / "inputs.json"]),
            "repair_authority_sha256": args.failure_report_sha256, "service": copy.deepcopy(model["stopped"]["service"]),
            "primary": copy.deepcopy(model["original"]["primary"]),
            "protected": {key: value for key, value in before.items() if key != "diagnostics"}}
        write(run / "online-preflight.json", original)
        write(run / "protected-before.json", before)
        previous = None
        for sequence, action in enumerate(("repair-baseline", "save-materials", "baseline-dump"), 1):
            path = run / f"intent-{sequence:04d}.json"
            write(path, {"marker": OPERATOR.MARKER, "run_id": run.name, "sequence": sequence, "action": action,
                "previous_sha256": previous, "inputs_sha256": OPERATOR.sha(documents[run / "inputs.json"])})
            previous = OPERATOR.sha(documents[path])
        terminal = {"marker": OPERATOR.MARKER, "version": 1, "run_id": run.name, "status": "failed", "error_type": "Failure",
            "last_completed_phase": "backed-up", "last_intent_sha256": previous,
            "automatic_recovery_executed": False, "main_database_restored": False, "old_binary_rollback": False}
        write(run / "terminal.json", terminal)
        proof = {"schema": "goby-main-schema26-cluster-proof", "version": 1, "run_id": run.name,
                 "repair_baseline": True, "postmaster_start_microseconds": original["primary"]["sql"]["start_microseconds"]}
        write(run / "baseline-cluster.json", proof)
        state = {"schema_version": 25, "trusted_catalog_verified": True, "tables": [{"name": f"synthetic_table_{index}"} for index in range(30)]}
        baseline = {"schema": "goby-main-schema26-migration", "version": 1, "mode": "inspect", "status": "inspected",
            "repair_baseline": True, "repair_rehearsal": False, "rehearsal": False, "run_id": run.name,
            "source_schema_version": 25, "target_schema_version": 26, "target": dict(OPERATOR.MAIN),
            "source_manifest_sha256": args.manifest_sha256, "tool_manifest_sha256": old_inputs["tool_source"]["manifest_sha256"],
            "candidate_sha256": args.candidate_sha256, "helper_sha256": old_inputs["helper"]["sha256"],
            "cluster_proof_sha256": OPERATOR.sha(documents[run / "baseline-cluster.json"]),
            "state": state, "state_sha256": OPERATOR.sha(OPERATOR.canonical(state))}
        write(run / "baseline.json", baseline)
        documents[run / ".baseline.json.reserved"] = b"goby-main-schema26-migration\nbaseline.json\n"
        documents[run / "database-schema25.dump"] = b"Synthetic retained prior dump; never used for the main database"
        documents[run / "dump.stderr"] = b""
        write(run / ("baseline-" + "c" * 12 + ".stdout"), {"schema": "goby-main-schema26-migration", "version": 1,
            "mode": "inspect", "status": "inspected", "state_sha256": baseline["state_sha256"]})
        documents[run / ("baseline-" + "c" * 12 + ".stderr")] = b""
        backup = {"source_schema": 25, "same_exported_snapshot": True, "private_materials_retained": True,
            "baseline_sha256": OPERATOR.sha(documents[run / "baseline.json"]), "state_sha256": baseline["state_sha256"],
            "materials_sha256": OPERATOR.sha(documents[run / "materials.json"]), "dump": dependency.file_fact(run / "database-schema25.dump")}
        write(run / "backup-complete.json", backup)
        observation_path = WORK / "main-schema26-repair-failure-review-01/failure-observation.json"
        diagnosis_path = observation_path.with_name("rehearsal-read.stderr")
        documents[diagnosis_path] = b"Synthetic read-only diagnosis: pg_authid.rolconfig does not exist"
        observation = {"root": str(OPERATOR.REPAIR_ROOT), "run": str(run), "failure_code": "pg_authid_rolconfig_missing",
            "rehearsal_artifacts_absent": True, "intent_actions": ["repair-baseline", "save-materials", "baseline-dump"],
            "observed_service": {"MainPID": "0", "ActiveState": "inactive", "SubState": "dead"},
            "main_binary_sha256": OLD_BINARY_SHA,
            "database_observation": {"activity_total": 15, "rehearsal_databases": 0, "rehearsal_roles": 0, "schema": 25, "sessions": 0},
            "diagnosis": {"path": str(diagnosis_path), "sha256": OPERATOR.sha(documents[diagnosis_path])}}
        report = {"schema": "goby-main-schema26-rehearsal-failure", "version": 1, "failed_root": str(OPERATOR.REPAIR_ROOT),
            "failed_run": str(run), "source_manifest_sha256": args.manifest_sha256, "candidate_sha256": args.candidate_sha256,
            "full_report_sha256": args.full_report_sha256, "service_pin_sha256": args.service_pin_sha256,
            "deployment_receipt_sha256": OPERATOR.OLD_TERMINAL_SHA, "baseline_failure_report_sha256": args.failure_report_sha256,
            "target": dict(OPERATOR.MAIN), "system_identifier": SYSTEM_IDENTIFIER, "old_process": inputs["old_process"],
            "error_code": "pg_authid_rolconfig_missing", "observed_schema": 25, "observed_activity_total": 15}

        def refresh():
            observation.update(inventory=OPERATOR.failure_tree(dependency, second=True),
                terminal=OPERATOR.decode(documents[run / "terminal.json"]), backup_complete=OPERATOR.decode(documents[run / "backup-complete.json"]))
            write(observation_path, observation)
            digest = OPERATOR.sha(documents[observation_path])
            self.replace(OPERATOR, "REHEARSAL_FAILURE_OBSERVATION_SHA", digest)
            report["observation"] = {"path": str(observation_path), "sha256": digest}
            write(args.rehearsal_failure_report, report)
            args.rehearsal_failure_report_sha256 = OPERATOR.sha(documents[args.rehearsal_failure_report])

        refresh()
        accepted = OPERATOR.read_rehearsal_failure_evidence(dependency, args, inputs)
        self.assertEqual(accepted["first_failed_tree"], first["failed_tree"])
        self.assertEqual(accepted["baseline"], baseline)
        saved_documents, saved_report, saved_observation = copy.deepcopy((documents, report, observation))

        def reset():
            documents.clear()
            documents.update(copy.deepcopy(saved_documents))
            report.clear()
            report.update(copy.deepcopy(saved_report))
            observation.clear()
            observation.update(copy.deepcopy(saved_observation))
            refresh()

        for path in (OPERATOR.FAILED_RUN / "materials/runtime.env", run / "database-schema25.dump", run / "baseline.json",
                     run / "materials/runtime.env", diagnosis_path, observation_path):
            reset()
            documents[path] += b"unreviewed drift"
            with self.subTest(retained_file=path.name):
                self.reject(lambda: OPERATOR.read_rehearsal_failure_evidence(dependency, args, inputs))
        for field, value in (("baseline_failure_report_sha256", "0" * 64), ("failed_run", str(OPERATOR.FAILED_RUN)),
                             ("observed_schema", 26), ("observed_activity_total", True), ("error_code", "unknown_commit")):
            reset()
            report[field] = value
            refresh()
            with self.subTest(second_attestation=field):
                self.reject(lambda: OPERATOR.read_rehearsal_failure_evidence(dependency, args, inputs))
        for artifact in ("rehearsal-credentials.json", "intent-0004.json", "main-migrated.json", ".unknown.pending"):
            reset()
            documents[run / artifact] = b"unexpected later outcome"
            refresh()
            with self.subTest(later_artifact=artifact):
                self.reject(lambda: OPERATOR.read_rehearsal_failure_evidence(dependency, args, inputs))
        reset()
        changed = OPERATOR.decode(documents[run / "terminal.json"])
        changed["last_completed_phase"] = "rehearsed"
        write(run / "terminal.json", changed)
        refresh()
        self.reject(lambda: OPERATOR.read_rehearsal_failure_evidence(dependency, args, inputs))
        reset()
        args.mode = "repair-baseline"
        self.reject(lambda: OPERATOR.read_rehearsal_failure_evidence(dependency, args, inputs))
        with patch.object(OPERATOR, "ROOT", OPERATOR.REPAIR_ROOT):
            self.reject(lambda: OPERATOR.failure_tree(dependency, second=True))

    def test_startup_counts_reads_one_fixed_database_clock_without_legacy_policy(self):
        model = self.startup_fixture()
        args, dependency, plan = model["args"], model["op"], model["plan"]
        result = {**copy.deepcopy(plan["observed"]), "deadline_eligible": 0}
        queries = []
        dependency.runtime_policy = lambda: "private-runtime-url"
        dependency.database_environment = lambda *_args, **_kwargs: {"PGHOST": "127.0.0.1", "PGPORT": "5432",
                                                                      "PGDATABASE": "goby_test", "PGUSER": "goby_test"}

        def query(_op, statement, **options):
            self.assertEqual(options["database"], "goby_test")
            self.assertEqual(options["environment"]["PGPORT"], "5432")
            self.assertIn("BEGIN READ ONLY", statement)
            self.assertIn("WITH moment AS MATERIALIZED", statement)
            self.assertIn("interval '30 days'", statement)
            self.assertIn("created_at <= '" + plan["deadline_utc"] + "'::timestamptz", statement)
            self.assertNotRegex(statement, r"(?i)\b(DELETE|UPDATE|ALTER|DROP|TRUNCATE)\b")
            queries.append(statement)
            return OPERATOR.canonical(result)

        self.replace(OPERATOR, "pg", query)
        self.assertEqual(OPERATOR.startup_counts(dependency, args, plan), result)
        changed = copy.deepcopy(plan)
        changed["deadline_utc"] = "2026-09-11T18:31:45.045806Z'; DELETE FROM activity_entries; --"
        self.reject(lambda: OPERATOR.startup_counts(dependency, args, changed))
        self.assertEqual(len(queries), 1)


def main():
    global OPERATOR
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "main-schema26-upgrade-guards", "status": "blocked",
                          "reason": "Authorized root SSH and the new operator source path are required."}))
        return 2
    source = Path(sys.argv[1]).resolve(strict=True)
    if source.name != "upgrade-main-schema26.py":
        raise SystemExit("Only the reviewed new schema26 main-upgrade operator may be loaded.")
    operator_bytes, suite_bytes = source.read_bytes(), Path(__file__).read_bytes()
    SOURCE_LINES[str(source)] = operator_bytes.decode().splitlines(keepends=True)
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    OPERATOR = types.ModuleType("memory_main_schema26_upgrade")
    OPERATOR.__file__ = str(source)
    sys.addaudithook(deny_audited_effect)
    try:
        with EffectFence():
            exec(compile(operator_bytes, str(source), "exec"), OPERATOR.__dict__)
    except BaseException as error:
        print(json.dumps({"suite": "main-schema26-upgrade-guards", "status": "failed", "tests": 0,
                          "error": "Fenced import failed: " + type(error).__name__,
                          "operator_sha256": hashlib.sha256(operator_bytes).hexdigest(),
                          "guard_sha256": hashlib.sha256(suite_bytes).hexdigest()}))
        return 1
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(MainSchema26GuardTests)
    names = sorted(test._testMethodName for test in suite)
    result = unittest.TestResult()
    suite.run(result)
    passed = result.wasSuccessful() and not result.skipped and result.testsRun >= 10
    print(json.dumps({"suite": "main-schema26-upgrade-guards", "status": "passed" if passed else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "skips": len(result.skipped), "cases": names,
                      "failure_summaries": [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]}
                                            for test, detail in [*result.failures, *result.errors][:8]],
                      "operator_sha256": hashlib.sha256(operator_bytes).hexdigest(),
                      "guard_sha256": hashlib.sha256(suite_bytes).hexdigest(),
                      "fixtures": "synthetic-memory-only", "real_backup_acceptance": False,
                      "real_migration_acceptance": False, "real_deployment_acceptance": False}))
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
