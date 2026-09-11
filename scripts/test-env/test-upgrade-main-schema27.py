#!/usr/bin/env python3
"""Exercise the schema27 main-upgrade operator with fenced memory fixtures.

Run only through authorized root SSH. The sole input is the reviewed new
operator source. Its imports and every test execute behind filesystem, network,
process, and lock fences. The published deployment dependency is never loaded.
These tests do not perform a backup, rehearsal, migration, or deployment.
"""

from __future__ import annotations

import argparse
import ast
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
OLD_BINARY_SHA = "83757e79a1694573e4c1fab83e18c67be5f0c2d8696246daccb91f009ab2efae"
OLD_SOURCE_SHA = "72e9301ba4405c6bddea15697e101dd62b157048f4c3b2e3e307da07ec0eb5df"
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
            (Path, ("readlink", "open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat",
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


class MainSchema27GuardTests(unittest.TestCase):
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
        args = types.SimpleNamespace(mode="preflight", source=OPERATOR.TARGET_SOURCE,
            operator=WORK / "tool-build-source18/scripts/test-env/deploy-client-schema25.py",
            deployment_receipt=OPERATOR.PUBLISHED_RUN / "terminal.json", candidate=artifacts / "goby-linux-amd64",
            assets=artifacts / "admin.tar.gz", full_report=OPERATOR.TARGET_FULL, helper=artifacts / "migrate-main-schema27",
            tool_source=WORK / "tool-build-main27", helper_build_report=artifacts / "helper-build.json",
            guard_report=artifacts / "guards.json", service_pin=artifacts / "service.json", release_report=artifacts / "release.json",
            startup_plan=artifacts / "startup-plan.json", candidate_state_report=artifacts / "current-candidate.json",
            old_pid=688833, old_start_ticks="5620918", old_boot_id=BOOT_ID,
            candidate_pid=900001, candidate_start_ticks="7000000")
        for index, name in enumerate(("script", "operator", "deployment_receipt", "manifest", "candidate", "assets", "full_report",
                "helper", "tool_manifest", "helper_build_report", "guard_report", "service_pin", "release_report", "startup_plan", "candidate_state_report")):
            setattr(args, name + "_sha256", hashlib.sha256(("synthetic-artifact-" + str(index)).encode()).hexdigest())
        args.deployment_receipt_sha256 = OPERATOR.OLD_TERMINAL_SHA
        args.manifest_sha256 = OPERATOR.TARGET_MANIFEST_SHA
        args.candidate_sha256 = OPERATOR.TARGET_BINARY_SHA
        args.full_report_sha256 = OPERATOR.TARGET_FULL_SHA
        return args

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
        plan = {"schema": "goby-main-schema27-startup-plan", "version": 1, "target": dict(OPERATOR.MAIN),
                "system_identifier": SYSTEM_IDENTIFIER, "source_manifest_sha256": args.manifest_sha256,
                "full_report_sha256": args.full_report_sha256, "candidate_sha256": args.candidate_sha256,
                "deployment_receipt_sha256": OPERATOR.OLD_TERMINAL_SHA, "service_pin_sha256": args.service_pin_sha256,
                "old_process": {"pid": 688833, "start_ticks": "5620918", "boot_id": BOOT_ID},
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
                         "process_environment_checked": True, "observed_pid": 688833, "effective_retention_days": 30}
        self.replace(OPERATOR, "retention_environment", lambda *_args: copy.deepcopy(configuration))
        self.replace(OPERATOR, "startup_counts", lambda *_args: copy.deepcopy(current))
        model.update(current=current, configuration=configuration)
        return model

    def test_fixed_target_and_published_lifetime_do_not_reuse_candidate_authority(self):
        self.assertEqual(OPERATOR.WORK, WORK)
        self.assertEqual(OPERATOR.ROOT, Path("/opt/goby-test/backups/main-schema27-v1"))
        self.assertEqual(OPERATOR.MARKER, "goby-main-schema27-upgrade-v1")
        self.assertEqual(OPERATOR.SERVICE, "goby-foundation-test.service")
        self.assertEqual(OPERATOR.MAIN, {"database": "goby_test", "database_oid": 16385,
                                        "role": "goby_test", "role_oid": 16384})
        self.assertEqual(OPERATOR.SYSTEM_IDENTIFIER, SYSTEM_IDENTIFIER)
        self.assertEqual((OPERATOR.OLD_PID, OPERATOR.OLD_TICKS), (688833, "5620918"))
        self.assertEqual(OPERATOR.OLD_BINARY_SHA, OLD_BINARY_SHA)

    def test_actual_root_extension_receipt_digest_is_independently_pinned(self):
        # This expected value comes from the already completed receipt's
        # independently read SHA256, not from a synthetic operator fixture.
        expected = '09bf775ff6582b0a3dbc6c52026b4da7f0ba59066884c08a4c1cea4d3ba1391e'
        tree = ast.parse(''.join(SOURCE_LINES[OPERATOR.__file__]))
        function = next(node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name == 'verify_candidate')
        comparisons = [node for node in ast.walk(function) if isinstance(node, ast.Compare) and
            isinstance(node.left, ast.Call) and isinstance(node.left.func, ast.Attribute) and
            isinstance(node.left.func.value, ast.Name) and node.left.func.value.id == 'extension' and
            node.left.func.attr == 'get' and len(node.left.args) == 1 and
            isinstance(node.left.args[0], ast.Constant) and node.left.args[0].value == 'report_sha256']
        self.assertEqual(len(comparisons), 1)
        self.assertEqual(len(comparisons[0].ops), 1)
        self.assertIsInstance(comparisons[0].ops[0], ast.Eq)
        self.assertIsInstance(comparisons[0].comparators[0], ast.Constant)
        self.assertEqual(comparisons[0].comparators[0].value, expected)

    def continuation_fixture(self):
        original_sha = OPERATOR.sha
        documents, fixed_hashes = {}, {}
        chain = {'marker': OPERATOR.CONTINUATION_MARKER, 'library_id': '57a85c1ca5b6c7ae602c587755250b2f',
            'root_id': '604d2c0f5c78919a6ee360cda2048066', 'creation_admin_session_id': 'a' * 32,
            'new_scan_admin_session_id': 'b' * 32, 'new_viewer_session_id': 'c' * 32, 'scan_job_id': 'd' * 32,
            'creation_requests': 5, 'continuation_library_creates': 0, 'continuation_scan_dispatches': 1,
            'original_failure_preserved': True, 'creation_admin_revoked': True, 'continuation_sessions_revoked': True}
        for key, (fixed, digest) in OPERATOR.CONTINUATION_ORIGIN_PINS.items():
            path = fixed or WORK / 'frozen-original' / (key + '.py')
            raw = ('Synthetic retained artifact: ' + key).encode()
            documents[path], fixed_hashes[raw] = raw, digest
            chain[key] = {'path': str(path), 'sha256': digest}
        evidence_path = OPERATOR.CONTINUATION_ROOT / 'origin-evidence.json'
        documents[evidence_path] = b'{"synthetic":"complete-origin-inventory"}'
        chain['origin_evidence'] = {'path': str(evidence_path), 'sha256': original_sha(documents[evidence_path])}
        self.replace(OPERATOR, 'sha', lambda raw: fixed_hashes.get(raw, original_sha(raw)))
        completed_path = OPERATOR.CONTINUATION_ROOT / 'completed.json'
        current = {'continuation': copy.deepcopy(chain), 'binary_sha256': OPERATOR.TARGET_BINARY_SHA,
            'process': {'pid': 900001, 'start_ticks': 7000000, 'boot_id': BOOT_ID}, 'runtime_sha256': '1' * 64,
            'state_sha256': '2' * 64, 'source': {'owned': 'source32'}, 'upgrade': {'owned': 'historical-upgrade'},
            'extension': {'owned': 'actual-extension'}, 'receipt_path': str(completed_path),
            'profile': {'receipt_path': str(completed_path), 'library_id': chain['library_id'], 'root_id': chain['root_id'],
                        'item_ids': {'positive': 'e' * 32}}}
        for key in ('setup_source', 'profile_source'):
            path = WORK / 'continued-tools' / (key + '.py')
            documents[path] = ('Synthetic continued source: ' + key).encode()
            current[key] = {'path': str(path), 'sha256': original_sha(documents[path])}
        proof = {'creation_admin_session_id': chain['creation_admin_session_id'], 'native_session_id': chain['new_scan_admin_session_id'],
            'viewer_session_id': chain['new_viewer_session_id'], 'item_ids': current['profile']['item_ids'],
            'creation_admin_revoked': True, 'old_rows_preserved': True, 'expected_increment_preserved': True,
            'creation_audit_count': 3, 'continuation_audit_count': 6, 'aggregate_session_count': 3, 'aggregate_audit_count': 9}
        completed = {'marker': 'goby-client-special-features-fixture-v1', 'phase': 'complete', 'schema': 27,
            'profile_version': 2, 'continuation': copy.deepcopy(chain), 'library_id': chain['library_id'], 'root_id': chain['root_id'],
            'job_id': chain['scan_job_id'], 'proof': copy.deepcopy(proof),
            'candidate': {key: current[key] for key in ('binary_sha256', 'process', 'runtime_sha256')},
            **{key: copy.deepcopy(current[key]) for key in ('source', 'upgrade', 'extension', 'setup_source', 'profile_source')}}
        report = {'marker': 'goby-client-special-features-setup-report-v1', 'result': 'passed', 'phase': 'complete',
            'receipt_path': str(completed_path), 'state_sha256': current['state_sha256'], 'candidate': copy.deepcopy(completed['candidate']),
            'continuation': copy.deepcopy(chain), 'proof': copy.deepcopy(proof)}
        def refresh():
            documents[completed_path] = OPERATOR.canonical(completed)
            digest = original_sha(documents[completed_path])
            current['receipt_sha256'] = current['profile']['receipt_sha256'] = report['receipt_sha256'] = digest
            documents[OPERATOR.CONTINUATION_ROOT / 'report.json'] = OPERATOR.canonical(report)
        refresh()
        dependency = types.SimpleNamespace(read_file=lambda path, **_kwargs: documents[path])
        return current, completed, report, documents, dependency, refresh

    def test_continuation_preserves_actual_origin_pins_and_requires_three_artifacts(self):
        self.assertEqual(OPERATOR.CONTINUATION_INSPECTION, WORK / 'client-special-features-continuation-inspection-v1/report.json')
        self.assertEqual(OPERATOR.CONTINUATION_ORIGIN_PINS['origin_failure'][1],
            '9ce73d72d9ce002dce06230a73213294903800b3b49b06e9b9aa33ad69eda2c0')
        self.assertEqual(OPERATOR.CONTINUATION_ORIGIN_PINS['origin_library_ack'][1],
            '998dffd8122cc7921df1a21092a357881e676950c6b858cd0f7332a3b8ecb94d')
        self.assertEqual(OPERATOR.CONTINUATION_ORIGIN_PINS['origin_state'][1],
            '513d971260e18d24ada666a3ec942391d4679bf2a98c5c852742cc70033fccf1')
        current, completed, report, documents, dependency, refresh = self.continuation_fixture()
        accepted = OPERATOR.verify_candidate_continuation(dependency, current)
        self.assertEqual(set(accepted), {str(OPERATOR.CONTINUATION_ROOT / name) for name in
            ('completed.json', 'report.json', 'origin-evidence.json')} | {str(OPERATOR.ORIGIN_FIXTURE / 'failure.json')})
        saved = copy.deepcopy((current, completed, report, documents))
        for key, value in (('continuation_library_creates', 1), ('continuation_scan_dispatches', 2), ('creation_requests', True),
                ('original_failure_preserved', False), ('creation_admin_revoked', False), ('continuation_sessions_revoked', False),
                ('new_scan_admin_session_id', 'a' * 32), ('library_id', 'f' * 32), ('scan_job_id', '')):
            changed = copy.deepcopy(current)
            changed['continuation'][key] = value
            with self.subTest(boundary=key):
                self.reject(lambda: OPERATOR.verify_candidate_continuation(dependency, changed))
        for path in list(documents):
            raw = documents[path]
            documents[path] = raw + b' unreviewed change'
            with self.subTest(artifact=str(path)), self.assertRaises((OPERATOR.Failure, ValueError)):
                OPERATOR.verify_candidate_continuation(dependency, current)
            documents[path] = raw
        self.assertEqual((current, completed, report, documents), saved)
        completed['continuation']['origin_evidence']['sha256'] = '0' * 64
        refresh()
        self.reject(lambda: OPERATOR.verify_candidate_continuation(dependency, current))

    def test_continuation_rejects_old_profile_and_unproven_audit_or_session_reuse(self):
        for defect in ('old-path', 'old-version', 'report-failed', 'seven-audits', 'two-sessions', 'wrong-scan-actor'):
            with self.subTest(defect=defect), contextlib.ExitStack():
                current, completed, report, documents, dependency, refresh = self.continuation_fixture()
                if defect == 'old-path': current['profile']['receipt_path'] = str(OPERATOR.ORIGIN_FIXTURE / 'completed.json')
                elif defect == 'old-version': completed['profile_version'] = 1
                elif defect == 'report-failed': report['result'] = 'failed'
                elif defect == 'seven-audits': completed['proof']['aggregate_audit_count'] = report['proof']['aggregate_audit_count'] = 7
                elif defect == 'two-sessions': completed['proof']['aggregate_session_count'] = report['proof']['aggregate_session_count'] = 2
                elif defect == 'wrong-scan-actor': completed['proof']['native_session_id'] = report['proof']['native_session_id'] = 'a' * 32
                refresh()
                self.reject(lambda: OPERATOR.verify_candidate_continuation(dependency, current))

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
        for mode in ("prepare", "deploy", "resume", "restore", "rollback", "restart", "repair-baseline", "rehearsal-repair", "", None):
            with self.subTest(mode=mode):
                self.reject(lambda: OPERATOR.validate_arguments(types.SimpleNamespace(mode=mode)))

    def test_pinned_source_rejects_escape_ownership_modes_and_links_before_open(self):
        path = WORK / "operator-guards/upgrade-main-schema27.py"
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
        path = WORK / "operator-guards/upgrade-main-schema27.py"
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
            ("candidate", WORK / "../outside/goby"), ("helper", Path("/tmp/migrate-main-schema27")),
            ("service_pin", Path("relative.json")), ("release_report", Path("/tmp/release.json")),
            ("candidate_sha256", OLD_BINARY_SHA),
            ("candidate_pid", True), ("candidate_pid", 688833), ("candidate_start_ticks", "07000000"),
            ("candidate_state_report", Path('/outside/report.json')),
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
        name = "goby_upgrade27_m3e_" + "4" * 24
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
        state = {"schema": "goby-main-schema27-migration", "state_sha256": "5" * 64,
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
        first = journal.intent("stop", {"pid": 688833})
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
        scenarios = (
            None, "artifacts", "stop", "materials", "baseline", "dump", "rehearsal", "recheck", "backup-check",
            "migrate", "post-migrate", "install", "start", "ready", "smoke")
        for fault in scenarios:
            with self.subTest(phase=fault), contextlib.ExitStack() as scope:
                selected_root = OPERATOR.ROOT
                scope.enter_context(patch.object(OPERATOR, "ROOT", selected_root))
                dependency, memory = self.evidence_fixture()
                args = self.arguments()
                args.mode = "upgrade"
                events, exported = [], {"open": False}
                inputs = {"source_manifest_sha256": "a" * 64, "candidate": {"sha256": "b" * 64}, "asset_members": {}}
                before = {"runtime": {"sha256": "c" * 64}, "stores": {"backups": []}, "assets": {"files": {}}, "diagnostics": {}}
                observed = {"service": {"pid": 688833, "start_ticks": "5620918"},
                            "primary": {"process": {"pid": 777}, "sql": {"start_microseconds": 1789100000123456}},
                            "protected": {key: value for key, value in before.items() if key != "diagnostics"}}
                active = {"pid": 900001, "start_ticks": "4000000"}
                baseline = {"schema": "goby-main-schema27-migration", "state_sha256": "d" * 64,
                            "state": {"schema_version": 26, "old_rows": "preserved"}}
                migrated = {"schema": "goby-main-schema27-migration", "state_sha256": "e" * 64,
                            "state": {"schema_version": 27, "old_rows": "preserved"}}
                dump = {"sha256": "f" * 64, "identity": {"bytes": 1024}}
                material_facts = {"goby-source28": {"sha256": OLD_BINARY_SHA}, "runtime.env": {"sha256": "9" * 64}}

                def event(name):
                    events.append(name)
                    if name == fault:
                        raise RuntimeError("private-failure-sentinel")

                def stop(_op, _args, journal, _observed):
                    journal.intent("stop", {"pid": 688833})
                    event("stop")

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
                    self.assertEqual(path.name, "database-schema26.dump")
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
                    memory.write(journal.run / "database-schema26.dump", b"private synthetic fresh dump")
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
                    "stop_main": stop, "protected_state": lambda *_args, **_kwargs: copy.deepcopy(before),
                    "save_materials": materials, "invoke_helper": helper, "dump_snapshot": dump_main,
                    "rehearse": rehearsal, "preserved_private": lambda *_args, **_kwargs: copy.deepcopy(before),
                    "verify_backup": verify_backup,
                    "exact_service": lambda *_args, **options: copy.deepcopy(active),
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
                    for earlier, later in (("stop", "baseline"), ("baseline", "dump"), ("dump", "rehearsal"),
                                           ("rehearsal", "recheck"), ("recheck", "backup-check"), ("backup-check", "migrate"), ("migrate", "install"),
                                           ("install", "start"), ("start", "ready"), ("ready", "smoke")):
                        self.assertLess(events.index(earlier), events.index(later))
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
        state = {"properties": {"MainPID": "688833", "ActiveState": "active", "SubState": "running",
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
            self.assertEqual(expected_binary, OLD_BINARY_SHA if state["properties"]["MainPID"] == "688833" else args.candidate_sha256)
            return {"MainPID": state["properties"]["MainPID"], "start_ticks": "5620918"}

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
                journal.intent("stop", {"pid": 688833})
                actions.append("stop")
            self.assertEqual((journal.sequence, journal.previous), (0, None))
            self.assertEqual(memory.files, saved)
            self.assertEqual(actions, [])
            self.assertFalse(OPERATOR.STARTUP_CHECKING)
            current["active"]["active_encodings"] = 0
            first = journal.intent("stop", {"pid": 688833})
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

    def test_stable_startup_requires_three_exact_samples_and_keeps_identity_failures_bounded(self):
        class DependencyFailure(Exception):
            pass
        for fault in (None, 'transient', 'persistent', 'binary', 'restart'):
            with self.subTest(fault=fault), contextlib.ExitStack() as scope:
                args = self.arguments()
                scope.enter_context(patch.object(OPERATOR, 'LOCK_HELD', True))
                clock, calls, requests = [0.0], [], []
                active = {'pid': 900001, 'start_ticks': '7000000', 'listener': {'inode': '123'}, 'binary_sha256': args.candidate_sha256}
                dependency = types.SimpleNamespace(Failure=DependencyFailure, command=lambda *_args, **_kwargs: b'')
                dependency.systemd_properties = lambda *_args: {'MainPID': '900001', 'ActiveState': 'active', 'SubState': 'running'}
                scope.enter_context(patch.object(time, 'monotonic', lambda: clock[0]))
                scope.enter_context(patch.object(time, 'sleep', lambda duration: clock.__setitem__(0, clock[0] + duration)))
                def stat_text(path, **_kwargs):
                    ticks = '7000001' if fault == 'restart' and calls else '7000000'
                    return '900001 (goby) ' + ' '.join(['0'] * 19 + [ticks])
                scope.enter_context(patch.object(Path, 'read_text', stat_text))
                def exact(*_args, **_kwargs):
                    calls.append(clock[0])
                    if fault == 'persistent' or fault == 'transient' and len(calls) == 1:
                        raise DependencyFailure('The candidate process has an unexpected effective identity.')
                    if fault == 'binary':
                        raise DependencyFailure('The installed main binary differs.')
                    return copy.deepcopy(active)
                scope.enter_context(patch.object(OPERATOR, 'exact_service', exact))
                class Connection:
                    def __init__(self, *_args, **_kwargs):
                        pass
                    def request(self, *values, **_kwargs):
                        requests.append(values)
                    def getresponse(self):
                        return types.SimpleNamespace(status=200, read=lambda _: b'{}')
                    def close(self):
                        pass
                scope.enter_context(patch.object(http.client, 'HTTPConnection', Connection))
                if fault in (None, 'transient'):
                    self.assertEqual(OPERATOR.wait_ready(dependency, args, {'candidate': {'sha256': args.candidate_sha256}}), active)
                    self.assertGreaterEqual(len(calls), 4 if fault is None else 5)
                    self.assertGreaterEqual(clock[0], 0.5)
                    self.assertEqual(len(requests), 1)
                else:
                    with self.assertRaises((OPERATOR.Failure, DependencyFailure)):
                        OPERATOR.wait_ready(dependency, args, {'candidate': {'sha256': args.candidate_sha256}})
                    self.assertEqual(requests, [])
                    self.assertLessEqual(clock[0], 45)

    def test_schema27_backfill_gate_is_read_only_and_rejects_retiring_any_old_theme_link(self):
        args = self.arguments()
        statements = []
        dependency = types.SimpleNamespace(read_file=lambda *_args, **_kwargs: b'const expectedExtraPathsSQL = `SELECT 1`',
            runtime_policy=lambda: 'private-test-url', database_environment=lambda *_args, **_kwargs: {'PGPORT': '5432'})
        result = {'markers': 2, 'affected_theme_links': 0}
        def query(_op, sql, **options):
            statements.append(sql)
            self.assertEqual(options['database'], 'goby_test')
            self.assertIn('BEGIN READ ONLY', sql)
            self.assertNotRegex(sql, r'(?i)\b(UPDATE|DELETE|ALTER|INSERT|DROP|CREATE)\b')
            return OPERATOR.canonical(result)
        self.replace(OPERATOR, 'pg', query)
        self.assertEqual(OPERATOR.extra_transition_preflight(dependency, args, {}), result)
        result['affected_theme_links'] = 1
        self.reject(lambda: OPERATOR.extra_transition_preflight(dependency, args, {}))
        self.assertEqual(len(statements), 2)

    def test_helper_invocation_binds_main_baseline_schema27_and_complete_old_state_proof(self):
        for defect in (None, 'port', 'source', 'tables', 'preservation', 'sequence', 'resources'):
            with self.subTest(defect=defect), contextlib.ExitStack() as scope:
                dependency, memory = self.evidence_fixture()
                args = self.arguments()
                inputs = {'source_manifest_sha256': args.manifest_sha256,
                    'tool_source': {'manifest_sha256': args.tool_manifest_sha256},
                    'candidate': {'sha256': args.candidate_sha256},
                    'helper': {'path': str(args.helper), 'sha256': args.helper_sha256, 'identity': {'bytes': 1000}}}
                proof = {'process': {'pid': 777, 'start_ticks': '1000'}, 'sql': {'start_microseconds': 1789100000123456}}
                run = OPERATOR.fresh_evidence(dependency, inputs, {'primary': proof}, 'run-20260911T080000Z-' + '9' * 24)
                memory.write(run / 'baseline.json', b'{"private":"baseline"}\n')
                journal = OPERATOR.Journal(dependency, run)
                scope.enter_context(patch.object(OPERATOR, 'primary_proof', lambda *_args: copy.deepcopy(proof)))
                dependency.file_fact = lambda *_args, **_kwargs: {key: inputs['helper'][key] for key in ('identity', 'sha256')}
                dependency.ENV = {'LANG': 'C.UTF-8'}
                calls = []
                def command(argv, **options):
                    calls.append(argv)
                    self.assertEqual(argv[argv.index('--expected-port') + 1], '5432')
                    self.assertEqual(argv[argv.index('--expected-schema') + 1], '26')
                    self.assertNotIn('--snapshot-id', argv)
                    self.assertNotIn('--repair-baseline', argv)
                    self.assertNotIn('--repair-rehearsal', argv)
                    self.assertEqual(argv[argv.index('--baseline') + 1], run / 'baseline.json')
                    cluster_raw = memory.files[run / 'main-migrated-cluster.json']
                    cluster = OPERATOR.decode(cluster_raw)
                    self.assertEqual((cluster['port'], cluster['database_oid'], cluster['role_oid']), (5432, 16385, 16384))
                    report = {'schema': 'goby-main-schema27-migration', 'version': 1, 'mode': 'migrate', 'status': 'committed',
                        'source_schema_version': 26, 'target_schema_version': 27, 'rehearsal': False,
                        'state_sha256': '1' * 64, 'run_id': run.name, 'source_manifest_sha256': args.manifest_sha256,
                        'tool_manifest_sha256': args.tool_manifest_sha256, 'candidate_sha256': args.candidate_sha256,
                        'helper_sha256': args.helper_sha256, 'cluster_proof_sha256': OPERATOR.sha(cluster_raw), 'target': dict(OPERATOR.MAIN),
                        'state': {'schema_version': 27, 'trusted_catalog_verified': True, 'tables': [{} for _ in range(35)]},
                        'input_baseline_sha256': OPERATOR.sha(memory.files[run / 'baseline.json']),
                        'before_state_sha256': '2' * 64, 'preserved_state_sha256': '2' * 64, 'new_extra_defaults_verified': True,
                        'extra_transition': {'resource_count': 0, 'old_sequences_unchanged': True,
                            'expected_paths_sha256': '3' * 64, 'reserved_paths_sha256': '3' * 64}}
                    if defect == 'source': report['source_schema_version'] = 25
                    elif defect == 'tables': report['state']['tables'].pop()
                    elif defect == 'preservation': report['preserved_state_sha256'] = '4' * 64
                    elif defect == 'sequence': report['extra_transition']['old_sequences_unchanged'] = False
                    elif defect == 'resources': report['extra_transition']['resource_count'] = 1
                    memory.write(run / 'main-migrated.json', OPERATOR.canonical(report))
                dependency.command = command
                target = dict(OPERATOR.MAIN)
                if defect == 'port': target['database_oid'] = 16398
                invoke = lambda: OPERATOR.invoke_helper(dependency, args, inputs, journal, target, 'private-url',
                    'migrate', 'main-migrated', baseline=run / 'baseline.json')
                if defect is None:
                    self.assertEqual(invoke()['target_schema_version'], 27)
                else:
                    self.reject(invoke)
                self.assertEqual(len(calls), 0 if defect == 'port' else 1)


def main():
    global OPERATOR
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "main-schema27-upgrade-guards", "status": "blocked",
                          "reason": "Authorized root SSH and the new operator source path are required."}))
        return 2
    source = Path(sys.argv[1]).resolve(strict=True)
    if source.name != "upgrade-main-schema27.py":
        raise SystemExit("Only the reviewed new schema27 main-upgrade operator may be loaded.")
    operator_bytes, suite_bytes = source.read_bytes(), Path(__file__).read_bytes()
    SOURCE_LINES[str(source)] = operator_bytes.decode().splitlines(keepends=True)
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    OPERATOR = types.ModuleType("memory_main_schema27_upgrade")
    OPERATOR.__file__ = str(source)
    sys.addaudithook(deny_audited_effect)
    try:
        with EffectFence():
            exec(compile(operator_bytes, str(source), "exec"), OPERATOR.__dict__)
    except BaseException as error:
        print(json.dumps({"suite": "main-schema27-upgrade-guards", "status": "failed", "tests": 0,
                          "error": "Fenced import failed: " + type(error).__name__,
                          "operator_sha256": hashlib.sha256(operator_bytes).hexdigest(),
                          "guard_sha256": hashlib.sha256(suite_bytes).hexdigest()}))
        return 1
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(MainSchema27GuardTests)
    names = sorted(test._testMethodName for test in suite)
    result = unittest.TestResult()
    suite.run(result)
    passed = result.wasSuccessful() and not result.skipped and result.testsRun >= 10
    print(json.dumps({"suite": "main-schema27-upgrade-guards", "status": "passed" if passed else "failed",
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
