#!/usr/bin/env python3
"""Exercise M5j deployment contracts with memory dependencies only.

Run through authorized root SSH with deploy-backup-recovery.py as the sole argument.
The source files are read before the effect fence is installed. The operator
is then compiled from those bytes; its import and every test are fenced.
Passing this suite is not acceptance of a real database restore or deployment.
"""

from __future__ import annotations

import sys

sys.dont_write_bytecode = True

import argparse
import base64
import builtins
import contextlib
import copy
from datetime import datetime, timezone
import fcntl
import hashlib
import hmac
import http.client
import io
import json
import linecache
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import selectors
import shlex
import shutil
import signal
import socket
import stat
import subprocess
import tarfile
import time
import types
import unittest
from unittest.mock import Mock, patch
from urllib.parse import unquote, urlsplit

OPERATOR: types.ModuleType
SOURCE_LINES: dict[str, list[str]] = {}
ACTIVE_FENCES: list[Fence] = []
HISTORICAL_TASK_TABLES = {"task_definitions", "task_triggers", "task_runs", "task_run_requests", "task_run_children", "task_occurrences"}
SCHEMA22_TABLES = set("schema_migrations server_settings users sessions libraries library_roots items scan_jobs "
                      "catalog_entities item_entities item_images user_item_data play_sessions item_subtitles "
                      "encoding_jobs client_playback_references item_metadata_state application_keys application_key_clients "
                      "devices application_key_devices managed_settings activity_entries".split()) | HISTORICAL_TASK_TABLES
MANAGED_FIELDS = {"id", "revision", "server_name", "max_bitrate", "max_width", "max_height",
                  "max_audio_channels", "created_at", "updated_at", "server_name_mode", "compatibility_max_width"}


def deny_audited_effect(event: str, _arguments: tuple) -> None:
    """Catch imported aliases and import-loader reads missed by API patches."""
    filesystem_events = {"open", "os.listdir", "os.scandir", "os.walk", "os.remove", "os.rmdir",
                         "os.mkdir", "os.rename", "os.chmod", "os.chown", "os.link", "os.symlink",
                         "os.truncate", "os.utime", "os.mkfifo", "os.mknod", "os.chdir", "os.fchdir", "shutil.copyfile", "shutil.copymode",
                         "shutil.copystat", "shutil.copytree", "shutil.move", "shutil.rmtree"}
    if ACTIVE_FENCES and (event in filesystem_events or event.startswith(("socket.", "subprocess.",
                                                                         "os.exec", "os.spawn", "os.fork", "fcntl.")) or
                          event == "os.system"):
        ACTIVE_FENCES[-1].denied.append(event)
        raise AssertionError("Audited external effect: " + event)


class Fence(contextlib.ExitStack):
    """Deny any external effect that a test has not explicitly replaced."""

    def __init__(self) -> None:
        super().__init__()
        self.denied: list[str] = []

    def __enter__(self) -> Fence:
        super().__enter__()
        ACTIVE_FENCES.append(self)
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "lazycache", lambda filename, module_globals: False))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       list(SOURCE_LINES.get(str(filename), ()))))
        targets = (
            (builtins, ("open",)),
            (io, ("open",)),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (fcntl, ("flock",)),
            (secrets, ("token_bytes", "token_hex", "token_urlsafe")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (signal, ("signal", "setitimer")),
            (time, ("sleep",)),
            (os, ("open", "fdopen", "stat", "lstat", "fstat", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "close", "lseek", "fsync", "fchmod", "fchown", "ftruncate", "umask", "kill", "killpg", "system", "popen", "fork",
                  "posix_spawn", "posix_spawnp", "execve", "replace", "rename", "mkdir", "makedirs",
                  "remove", "unlink", "rmdir", "chmod", "chown", "link", "symlink", "truncate",
                  "utime", "mkfifo", "mknod", "access", "chdir", "fchdir", "chroot")),
            (Path, ("open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat",
                    "read_bytes", "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink",
                    "chmod", "touch", "rename", "replace", "glob", "rglob", "iterdir")),
        )
        for owner, names in targets:
            for name in names:
                if not hasattr(owner, name):
                    continue
                label = getattr(owner, "__name__", str(owner)) + "." + name

                def deny(*_args: object, _label: str = label, **_kwargs: object) -> object:
                    self.denied.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, deny))
        return self

    def __exit__(self, *arguments: object) -> None:
        try:
            super().__exit__(*arguments)
        finally:
            if ACTIVE_FENCES and ACTIVE_FENCES[-1] is self:
                ACTIVE_FENCES.pop()
        if self.denied:
            raise AssertionError("External effects were attempted: " + ", ".join(self.denied))


class DeploymentContractTests(unittest.TestCase):
    def setUp(self) -> None:
        self.fence = Fence()
        self.fence.__enter__()
        self.addCleanup(self.fence.__exit__, None, None, None)

    def replace(self, owner: object, name: str, value: object) -> None:
        replacement = patch.object(owner, name, value)
        replacement.start()
        self.addCleanup(replacement.stop)

    def candidate(self) -> dict:
        files = {"go.mod": "a" * 64, "go.sum": "a" * 64, "cmd/goby/main.go": "a" * 64}
        files.update({f"internal/database/migrations/{version:04d}_stub.sql": "a" * 64
                      for version in range(1, 19)})
        files["internal/database/migrations/0019_scheduled_tasks.sql"] = "a" * 64
        files["internal/database/migrations/0020_managed_settings.sql"] = "a" * 64
        files["internal/database/migrations/0021_configuration_compatibility.sql"] = "a" * 64
        files["internal/database/migrations/0022_activity_entries.sql"] = "a" * 64
        files["internal/database/migrations/0023_backup_activity.sql"] = "a" * 64
        files["internal/backuppg/catalogs/schema-23-postgresql-17.json"] = "a" * 64
        return {"owner": OPERATOR.CANDIDATE_OWNER,
                "binary": {"path": str(OPERATOR.SCRATCH / "goby-m5j-linux-amd64"), "sha256": "b" * 64, "size": 12345},
                "assets": {"path": str(OPERATOR.SCRATCH / "goby-m5j-admin-assets.tar.gz"), "sha256": "c" * 64, "files": 2},
                "source": {"path": "/opt/goby-test/verify-m5j-core-attempt-5",
                           "resolved_path": "/opt/goby-test/verify-m5j-core-attempt-5",
                           "owner_marker": "goby-m5j-verification", "files": files},
                "gates": {name: {"status": "passed", "evidence_path": str(OPERATOR.ROOT / (name + ".json")),
                                 "sha256": "d" * 64} for name in ("final_go", "full", "browser", "recovery")}}

    def backup(self) -> dict:
        names = ("candidate.json", "candidate-assets.tar.gz", "operator.py", "business-before.json", "media-before.json", "protected-before.json",
                 "goby-m5i", "runtime.env", "unit.service", "dropin.conf", "master.key", "database-schema22.dump",
                 "restore-toc.list", "restore-schema22.sql", "restore-rehearsal.json", "source-before.json", "source-after.json",
                 "recovery-before.json", "recovery-database-before.json", "diagnostics-before.json", "observability-dropin.conf")
        files = {name: "a" * 64 for name in names}
        files["goby-m5i"] = OPERATOR.OLD
        files["candidate.json"] = "d" * 64
        return {"owner": OPERATOR.OWNER, "phase": "backup-complete", "old_binary_sha256": OPERATOR.OLD,
                "candidate_sha256": "b" * 64, "candidate_manifest_sha256": "d" * 64, "files": files,
                "restore_sql_generated": True, "isolated_restore_verified": True, "live_restore_verified": False}

    def row(self, value: dict) -> str:
        return OPERATOR.canonical(value).decode()

    def protected_service_fixture(self):
        properties = ("FragmentPath", "DropInPaths", "User", "Group", "WorkingDirectory", "ProtectSystem", "ReadWritePaths",
                      "EnvironmentFiles", "UMask", "NoNewPrivileges", "StateDirectory", "LogsDirectory", "LogsDirectoryMode")
        original_dropins = {str(OPERATOR.DROPIN), str(OPERATOR.OBSERVABILITY_DROPIN)}
        original_writes = {"/dev/shm/goby-transcodes-test", str(OPERATOR.VAULT)}
        runtime_file = str(OPERATOR.RUNTIME) + " (ignore_errors=no)"
        recovery_file = str(OPERATOR.RECOVERY_ENV) + " (ignore_errors=no)"
        values = {"FragmentPath": str(OPERATOR.UNIT), "DropInPaths": " ".join(sorted(original_dropins)),
                  "User": "goby", "Group": "goby", "WorkingDirectory": "/var/lib/goby-test", "ProtectSystem": "strict",
                  "ReadWritePaths": " ".join(sorted(original_writes)), "UMask": "0077", "NoNewPrivileges": "yes",
                  "StateDirectory": "", "LogsDirectory": "goby-test", "LogsDirectoryMode": "0700"}
        state = {"values": values, "environment_files": [runtime_file], "dropin_exists": False,
                 "extra_lines": [], "omit": set()}

        def information(inode, mode, uid=0, gid=0, size=64):
            return types.SimpleNamespace(st_dev=1, st_ino=inode, st_size=size, st_mtime_ns=1000 + inode,
                                         st_ctime_ns=2000 + inode, st_mode=mode, st_uid=uid, st_gid=gid, st_nlink=1)

        infos = {OPERATOR.RUNTIME: information(1, stat.S_IFREG | 0o600),
                 OPERATOR.MASTER: information(2, stat.S_IFREG | 0o600, 995, 986, 32),
                 OPERATOR.VAULT: information(3, stat.S_IFDIR | 0o700, 995, 986),
                 OPERATOR.UNIT: information(4, stat.S_IFREG | 0o600),
                 OPERATOR.DROPIN: information(5, stat.S_IFREG | 0o644),
                 OPERATOR.OBSERVABILITY_DROPIN: information(6, stat.S_IFREG | 0o644),
                 OPERATOR.LOG_DIRECTORY: information(7, stat.S_IFDIR | 0o700, 995, 986)}

        def observed(arguments, _label, **_kwargs):
            expected = ["/usr/bin/systemctl", "show", OPERATOR.SERVICE]
            for name in properties:
                expected.extend(["-p", name])
            self.assertEqual(arguments, expected)
            lines = []
            for name in properties:
                if name in state["omit"]:
                    continue
                if name == "EnvironmentFiles":
                    lines.extend(name + "=" + value for value in state["environment_files"])
                else:
                    lines.append(name + "=" + state["values"][name])
            lines.extend(state["extra_lines"])
            return ("\n".join(lines) + "\n").encode()

        def dropin_exists(path):
            self.assertEqual(path, OPERATOR.RECOVERY_DROPIN)
            return state["dropin_exists"]

        def no_symlink(path):
            self.assertEqual(path, OPERATOR.RECOVERY_DROPIN)
            return False

        def retained_bytes(path):
            self.assertEqual(path, OPERATOR.OBSERVABILITY_DROPIN)
            return OPERATOR.OBSERVABILITY_BYTES

        self.replace(OPERATOR, "regular", lambda path, *_args, **_kwargs: infos[path])
        self.replace(OPERATOR, "directory", lambda path, *_args, **_kwargs: infos[path])
        self.replace(OPERATOR, "private_values", Mock(return_value={"GOBY_API_KEY_MASTER_KEY_FILE": str(OPERATOR.MASTER)}))
        self.replace(OPERATOR, "command", Mock(side_effect=observed))
        self.replace(OPERATOR, "sha", lambda path: hashlib.sha256(str(path).encode()).hexdigest())
        self.replace(OPERATOR, "reference_state", Mock(return_value={"pid": OPERATOR.REFERENCE_PID, "retained": True}))
        installed = Mock(return_value=[1, 8, 0o644, 0, 0])
        self.replace(OPERATOR, "installed_recovery_dropin_identity", installed)
        self.replace(Path, "exists", dropin_exists)
        self.replace(Path, "is_symlink", no_symlink)
        self.replace(Path, "read_bytes", retained_bytes)
        self.replace(Path, "lstat", lambda path: infos[path])

        def enable_recovery():
            state["dropin_exists"] = True
            state["values"]["DropInPaths"] = " ".join(sorted(original_dropins | {str(OPERATOR.RECOVERY_DROPIN)}))
            state["values"]["ReadWritePaths"] = " ".join(sorted(original_writes | {str(path) for path in OPERATOR.RECOVERY_DIRECTORIES}))
            state["environment_files"] = [runtime_file, recovery_file]

        return state, enable_recovery, installed, runtime_file, recovery_file

    def finalization_fixture(self):
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", True)
        self.replace(OPERATOR, "FINALIZATION_READ_ONLY", False)
        arguments = types.SimpleNamespace(restore=False, finalize=True,
            candidate_manifest=OPERATOR.SCRATCH / "m5j-candidate.json", manifest_sha256=OPERATOR.FINALIZATION_CANDIDATE_SHA,
            remediation_report=OPERATOR.ROOT / "exec-work-m5j/remediation-guards.json", remediation_sha256="e" * 64)
        candidate = self.candidate()
        candidate["binary"] = {"path": str(OPERATOR.SCRATCH / "goby-m5j-linux-amd64"),
                               "sha256": OPERATOR.FINALIZATION_BINARY_SHA, "size": OPERATOR.FINALIZATION_BINARY_BYTES}
        backup = self.backup()
        backup["candidate_sha256"] = OPERATOR.FINALIZATION_BINARY_SHA
        backup["candidate_manifest_sha256"] = OPERATOR.FINALIZATION_CANDIDATE_SHA
        backup["files"]["operator.py"] = OPERATOR.FINALIZATION_ORIGINAL_OPERATOR_SHA
        assets = {"index.html": b"synthetic accepted administrator index"}
        manifest_bytes = OPERATOR.canonical(candidate)
        before, after = self.historical_rows(), self.historical_rows()
        baseline = {"before": before, "protected": {"synthetic": "protected"}, "media": {"synthetic": "media"},
                    "retained": {"index.html": "a" * 64}, "diagnostic_before": {"synthetic": "diagnostics"}}
        process = {"main_pid": OPERATOR.FINALIZATION_PID, "uid": 995, "gid": 986,
                   "start_ticks": OPERATOR.FINALIZATION_START, "binary_sha256": OPERATOR.FINALIZATION_BINARY_SHA}
        contract_names = sorted(name for name in dir(type(self)) if name.startswith("test_"))
        remediation = {"path": str(arguments.remediation_report), "sha256": arguments.remediation_sha256,
                       "operator_sha256": "c" * 64, "suite_path": "/opt/goby-test/exec-work-m5j/test-deploy-backup-recovery.py",
                       "suite_sha256": "d" * 64, "tests": len(contract_names), "contract_tests": contract_names}
        recovery, recovery_database = {"deployment_id": "1" * 32}, {"status": "verified", "public_empty": True}
        report = {"owner": OPERATOR.OWNER, "status": "passed", **process, "schema_version": OPERATOR.TARGET_SCHEMA,
                  "tables_after": OPERATOR.summaries(after), "recovery": recovery, "recovery_database": recovery_database,
                  "deployment_script_sha256": remediation["operator_sha256"]}
        started = datetime(2042, 1, 2, 3, 4, 5, tzinfo=timezone.utc)
        observed = datetime(2042, 1, 2, 3, 4, 6, tzinfo=timezone.utc)
        state = {"events": [], "counts": {}, "publications": [], "existing": set(), "symlinks": set(),
                 "fail_at": None, "publish_race": None, "disk_sha": OPERATOR.FINALIZATION_BINARY_SHA,
                 "disk_size": OPERATOR.FINALIZATION_BINARY_BYTES, "process": process}

        def record(name):
            self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, True)
            state["events"].append(name)
            state["counts"][name] = state["counts"].get(name, 0) + 1
            if state["fail_at"] == (name, state["counts"][name]):
                raise RuntimeError("Synthetic finalization observation failed: " + name)

        def observation(name, value):
            def receive(*_args, **_kwargs):
                record(name)
                return copy.deepcopy(value)
            return receive

        def exists(path):
            self.assertIn(path, (OPERATOR.EVIDENCE, OPERATOR.RECOVERY_EVIDENCE))
            key = "exists:" + str(path)
            state["counts"][key] = state["counts"].get(key, 0) + 1
            return path in state["existing"] or state["publish_race"] == path and state["counts"][key] > 1

        def is_symlink(path):
            self.assertIn(path, (OPERATOR.EVIDENCE, OPERATOR.RECOVERY_EVIDENCE))
            return path in state["symlinks"]

        def regular(path, maximum, mode):
            self.assertEqual((path, maximum, mode), (OPERATOR.LIVE / "goby", 256 * OPERATOR.MIB, 0o755))
            record("installed-file")
            return types.SimpleNamespace(st_size=state["disk_size"])

        def directory(path, mode):
            self.assertEqual((path, mode), (OPERATOR.LIVE, 0o755))
            record("live-directory")

        def target(root, relative):
            self.assertEqual((root, relative), (OPERATOR.LIVE / "admin", "index.html"))
            record("retained-asset-path")
            return root / relative

        def digest(path):
            if path == OPERATOR.LIVE / "goby":
                record("installed-digest")
                return state["disk_sha"]
            self.assertEqual(path, OPERATOR.BACKUP / "backup-manifest.json")
            record("backup-manifest-digest")
            return "f" * 64

        def identify(expected):
            self.assertEqual(expected, OPERATOR.FINALIZATION_BINARY_SHA)
            record("process")
            return copy.deepcopy(state["process"])

        def publish(path, payload):
            self.assertEqual(path, OPERATOR.EVIDENCE)
            record("publish")
            state["publications"].append((path, payload))

        database = types.SimpleNamespace(read=Mock(side_effect=observation("clock", observed.isoformat())))
        dependencies = {
            "load_remediation_evidence": ("remediation", remediation),
            "load_candidate": ("candidate", (candidate, manifest_bytes, assets)), "load_backup": ("backup", backup),
            "finalization_baseline": ("baseline", baseline), "finalization_start_time": ("started", started),
            "http_bytes": ("ready", b"ready"), "source_inventory": ("source", candidate["source"]["files"]),
            "snapshot": ("rows", after), "protected_state": ("protected", baseline["protected"]),
            "media_state": ("media", baseline["media"]), "verify_recovery": ("recovery", recovery),
            "verify_recovery_database": ("recovery-database", recovery_database),
            "preserve_diagnostic_history": ("diagnostics", True), "verify_installed_candidate": ("shared-verification", report)}
        mocks = {}
        for name, (event, value) in dependencies.items():
            mocks[name] = Mock(side_effect=observation(event, value))
            self.replace(OPERATOR, name, mocks[name])
        self.replace(OPERATOR, "Database", lambda: (record("database"), database)[1])
        self.replace(OPERATOR, "regular", regular)
        self.replace(OPERATOR, "directory", directory)
        self.replace(OPERATOR, "target", target)
        self.replace(OPERATOR, "sha", digest)
        self.replace(OPERATOR, "process_identity", identify)
        self.replace(OPERATOR, "publish_exclusive", publish)
        self.replace(Path, "exists", exists)
        self.replace(Path, "is_symlink", is_symlink)
        forbidden = {}
        for name in ("restore", "create_backup", "replace_file", "command", "private", "sync_directory",
                     "provision_recovery_database", "remove_recovery_database"):
            forbidden[name] = Mock(side_effect=AssertionError("A finalization attempted a forbidden mutation: " + name))
            self.replace(OPERATOR, name, forbidden[name])
        return arguments, state, mocks, forbidden, database, candidate, backup, baseline, assets, started

    def recovery_intent(self) -> dict:
        return {"owner": OPERATOR.OWNER, "candidate_sha256": self.backup()["candidate_sha256"],
                "baseline": OPERATOR.recovery_baseline(), "stage": str(OPERATOR.BACKUP / "recovery-dropin.stage"),
                "identity": [1, 7, 0o644, 0, 0],
                "directories": {str(path): [1, 20 + index, 0o700, 995, 986]
                                for index, path in enumerate(OPERATOR.RECOVERY_DIRECTORIES)}}

    def recovery_database_fixture(self):
        catalog = {
            "cluster": {"system_identifier": "100000000001", "postmaster_start": "2042-01-02T03:04:05+00:00",
                        "version_num": 170006, "port": 5432, "user": "postgres", "database": "postgres"},
            "roles": [{"oid": 1, "name": "postgres", "comment": None, "settings": None},
                      {"oid": 10, "name": "goby_test", "comment": None, "settings": None},
                      {"oid": 11, "name": "retained_reader", "comment": {"sha256": "a" * 64}, "settings": None}],
            "databases": [{"oid": 100, "name": "goby_test", "owner_oid": 10, "owner": "goby_test",
                           "comment": {"sha256": "a" * 64}, "acl": "retained database ACL"}],
            "memberships": [{"role_oid": 11, "member_oid": 10, "grantor_oid": 1,
                             "admin": False, "inherit": False, "set": True}],
            "settings": [{"database_oid": 100, "role_oid": 10, "settings": {"sha256": "b" * 64}}],
            "security_labels": [{"object_oid": 100, "class_oid": 1262, "provider": "retained-provider",
                                 "label": {"sha256": "c" * 64}}],
        }
        baseline = {"catalog": catalog}
        intent = {"owner": OPERATOR.OWNER, "candidate_sha256": "b" * 64,
                  "baseline_sha256": OPERATOR._recovery_digest(baseline),
                  "tag": OPERATOR.OWNER + ":recovery:" + "1" * 32, "environment_sha256": "d" * 64}
        role = {"oid": 201, "name": OPERATOR.RECOVERY_ROLE_NAME, "comment": intent["tag"], "login": True,
                "inherit": False, "superuser": False, "createdb": False, "createrole": False,
                "replication": False, "bypassrls": False, "connection_limit": 16, "valid_until": None, "settings": None}
        target = {"oid": 301, "name": OPERATOR.RECOVERY_DATABASE_NAME, "owner_oid": role["oid"],
                  "owner": OPERATOR.RECOVERY_ROLE_NAME, "encoding": "UTF8", "template": False,
                  "allow_connections": True, "connection_limit": 16, "tablespace_oid": 1663,
                  "collation": "C.UTF-8", "ctype": "C.UTF-8", "locale_provider": "c", "collation_version": None,
                  "acl": "synthetic owned ACL", "safe_acl": True, "comment": intent["tag"]}
        schema = {"database": OPERATOR.RECOVERY_DATABASE_NAME, "user": "postgres", "session_user": "postgres",
                  "database_oid": target["oid"], "postmaster_start": catalog["cluster"]["postmaster_start"],
                  "version_num": catalog["cluster"]["version_num"], "encoding": "UTF8", "address": None, "port": None,
                  "empty": True, "public": {"oid": 401, "owner_oid": 6171, "owner": "pg_database_owner",
                                             "safe_owner": True, "safe_acl": True, "comment": "standard public schema", "acl": None}}
        return baseline, intent, role, target, schema

    def historical_rows(self) -> dict:
        # These row samples test preservation contracts, not database imports.
        rows = {table: [self.row({"preserved": "synthetic-old-" + table})] for table in SCHEMA22_TABLES}
        old_time = "2041-01-02T03:04:05+00:00"
        rows["schema_migrations"] = [self.row({"version": version,
                                                "name": (f"{version:04d}_stub.sql" if version < 19 else
                                                         "0019_scheduled_tasks.sql" if version == 19 else
                                                         "0020_managed_settings.sql" if version == 20 else
                                                         "0021_configuration_compatibility.sql" if version == 21 else "0022_activity_entries.sql"),
                                                "applied_at": old_time}) for version in range(1, 23)]
        rows["managed_settings"] = [self.row({"id": 1, "revision": 17, "server_name": None,
            "max_bitrate": 9876543, "max_width": 1920, "max_height": 1080, "max_audio_channels": 6,
            "created_at": "2040-01-02T03:04:05.123456+00:00", "updated_at": old_time,
            "server_name_mode": "deployment", "compatibility_max_width": 1280})]
        rows["server_settings"] = [self.row({"key": "server_name", "value": "Preserved legacy server name",
            "created_at": "2039-01-02T03:04:05+00:00", "updated_at": old_time})]
        rows["activity_entries"] = [self.row({"id": 37, "action": "settings.updated", "actor_id": "old-admin",
            "severity": "Info", "source": "native", "actor_kind": "user", "actor_credential_id": "old-session",
            "resource_kind": "settings", "resource_id": "server", "request_id": "old-request", "revision": 17,
            "affected_count": 1, "state": "", "changed_fields": ["ServerName"], "created_at": old_time})]
        rows["scan_jobs"] = [self.row({"id": "5" * 32, "status": "Completed", "task_child_id": "4" * 32,
                                        "finished_at": old_time, "items_added": 2})]
        rows["encoding_jobs"] = [self.row({"id": "old-encoding", "state": "completed", "updated_at": old_time})]
        rows["task_definitions"] = [self.row({"id": "1" * 32, "key": "library.scan", "emby_key": "RefreshLibrary",
            "name": "Scan media library", "description": "Scan all registered media libraries.", "category": "Library",
            "is_hidden": False, "enabled": True, "revision": 7, "schedule_timezone": "UTC", "created_at": old_time, "updated_at": old_time})]
        rows["task_triggers"] = [self.row({"id": "2" * 32, "task_id": "1" * 32, "schedule_revision": 7, "position": 0,
            "kind": "startup", "interval_ticks": None, "anchor_at": None, "time_of_day_ticks": None, "day_of_week": None,
            "timezone": None, "max_runtime_ticks": None, "next_fire_at": None, "last_due_at": old_time,
            "calculation_error": "", "retired_at": old_time, "created_at": old_time, "updated_at": old_time})]
        rows["task_runs"] = [self.row({"id": "3" * 32, "task_id": "1" * 32, "state": "completed", "source": "startup",
            "task_key": "library.scan", "task_emby_key": "RefreshLibrary", "task_name": "Scan media library",
            "trigger_id": "2" * 32, "trigger_revision": 7, "scheduled_for": old_time, "finished_at": old_time,
            "total_children": 1, "terminal_children": 1, "completed_children": 1})]
        rows["task_run_children"] = [self.row({"id": "4" * 32, "run_id": "3" * 32, "library_id": "old-library",
            "library_name": "Preserved library snapshot", "ordinal": 0, "state": "completed", "scan_job_id": "5" * 32,
            "scanned": 3, "added": 2, "updated": 1, "finished_at": old_time})]
        rows["task_run_requests"] = [self.row({"task_id": "1" * 32, "request_id": "old-request", "run_id": "3" * 32,
                                                "fingerprint": "\\x" + "a" * 64, "created_at": old_time})]
        rows["task_occurrences"] = [self.row({"id": "6" * 32, "task_id": "1" * 32, "trigger_id": "2" * 32,
            "schedule_revision": 7, "due_at": old_time, "last_due_at": None, "occurrence_count": 1,
            "disposition": "admitted", "run_id": "3" * 32, "observed_at": old_time})]
        return rows

    def upgrade_fixture(self, server_name=None, name_mode=None):
        before = self.historical_rows()
        managed = json.loads(before["managed_settings"][0])
        managed["server_name"] = server_name
        managed["server_name_mode"] = name_mode if name_mode is not None else "deployment" if server_name is None else "custom"
        before["managed_settings"] = [self.row(managed)]
        after = copy.deepcopy(before)
        after["schema_migrations"].append(self.row({"version": 23, "name": "0023_backup_activity.sql",
                                                     "applied_at": "2042-01-02T03:04:05+00:00"}))
        marker = {"version": 1, "deploymentId": "1" * 32, "generationId": "", "slot": "primary"}
        after["server_settings"].append(self.row({"key": "goby.recovery.binding.v1", "value": json.dumps(marker, separators=(",", ":")),
            "created_at": "2042-01-02T03:04:05+00:00", "updated_at": "2042-01-02T03:04:05+00:00"}))
        earliest = datetime(2042, 1, 2, 3, 4, 0, tzinfo=timezone.utc)
        latest = datetime(2042, 1, 2, 3, 4, 10, tzinfo=timezone.utc)
        database = types.SimpleNamespace(read=Mock(side_effect=AssertionError("Upgrade comparison must not project away historical fields")))
        return database, before, after, managed, earliest, latest

    def test_candidate_refuses_missing_gates_refused_results_unsafe_paths_and_invalid_digests(self) -> None:
        accepted = self.candidate()
        self.assertEqual(OPERATOR.validate_candidate_document(accepted), accepted)
        extended = copy.deepcopy(accepted)
        extended["source"]["files"].update({f"internal/synthetic/extra_{index}.go": "a" * 64 for index in range(7)})
        self.assertEqual(OPERATOR.validate_candidate_document(extended), extended)
        variants = []
        for name in ("final_go", "full", "browser", "recovery"):
            missing = copy.deepcopy(accepted)
            del missing["gates"][name]
            variants.append(("missing-" + name, missing))
            refused = copy.deepcopy(accepted)
            refused["gates"][name]["status"] = "refused"
            variants.append(("refused-" + name, refused))
        changes = (("binary", "path", "/tmp/unowned-binary"), ("assets", "path", "/tmp/unowned-assets.tar"),
                   ("source", "path", "/tmp/unowned-source"), ("source", "resolved_path", "/tmp/unowned-source"),
                   ("binary", "sha256", "z" * 64), ("assets", "sha256", "short"))
        for section, field, value in changes:
            altered = copy.deepcopy(accepted)
            altered[section][field] = value
            variants.append((section + "-" + field, altered))
        for section, field, value in (("binary", "size", True), ("binary", "size", 0),
                                      ("binary", "size", 256 * OPERATOR.MIB + 1),
                                      ("binary", "sha256", OPERATOR.OLD), ("assets", "files", True),
                                      ("assets", "files", 1), ("assets", "files", 257)):
            altered = copy.deepcopy(accepted)
            altered[section][field] = value
            variants.append(("invalid-bound-" + section + "-" + field + "-" + str(value), altered))
        for label, field, value in (("relative-evidence", "evidence_path", "relative.json"),
                                    ("outside-evidence", "evidence_path", "/tmp/unowned.json"),
                                    ("traversal-evidence", "evidence_path", "/opt/goby-test/../unowned.json"),
                                    ("invalid-evidence-digest", "sha256", "D" * 64)):
            altered = copy.deepcopy(accepted)
            altered["gates"]["full"][field] = value
            variants.append((label, altered))
        bad_source = copy.deepcopy(accepted)
        bad_source["source"]["files"]["cmd/goby/main.go"] = "a" * 63
        variants.append(("invalid-source-digest", bad_source))
        missing_migration = copy.deepcopy(accepted)
        del missing_migration["source"]["files"]["internal/database/migrations/0023_backup_activity.sql"]
        variants.append(("missing-backup-activity-migration", missing_migration))
        wrong_migration = copy.deepcopy(missing_migration)
        wrong_migration["source"]["files"]["internal/database/migrations/0023_unrelated.sql"] = "a" * 64
        variants.append(("incorrect-migration-twenty-three-name", wrong_migration))
        for version in (22,):
            altered = copy.deepcopy(accepted)
            known = next(name for name in altered["source"]["files"]
                         if name.startswith(f"internal/database/migrations/{version:04d}_"))
            del altered["source"]["files"][known]
            altered["source"]["files"][f"internal/database/migrations/{version:04d}_unrelated.sql"] = "a" * 64
            variants.append(("incorrect-prior-migration-name-" + str(version), altered))
        missing_catalog = copy.deepcopy(accepted)
        del missing_catalog["source"]["files"]["internal/backuppg/catalogs/schema-23-postgresql-17.json"]
        variants.append(("missing-embedded-postgresql-catalog", missing_catalog))
        for relative in ("../outside.go", "internal/../outside.go", "/internal/absolute.go",
                         "internal//double.go", "internal/secret.txt", "admin/index.html"):
            altered = copy.deepcopy(accepted)
            altered["source"]["files"][relative] = "a" * 64
            variants.append(("unsafe-source-entry-" + relative, altered))
        for field, value in (("path", "/opt/goby-test/verify-m5j/../outside"),
                             ("resolved_path", "/dev/shm/goby-verify-m5i-unowned"),
                             ("owner_marker", "goby-m5i-verification")):
            altered = copy.deepcopy(accepted)
            altered["source"][field] = value
            variants.append(("source-namespace-" + field, altered))
        for label, document in variants:
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.validate_candidate_document(document)

    def test_m5j_scope_and_dynamic_manifest_arguments_are_bound_to_the_accepted_baseline(self) -> None:
        self.replace(argparse, "_", lambda value: value)
        self.replace(shutil, "get_terminal_size", Mock(return_value=os.terminal_size((80, 24))))
        self.assertEqual((OPERATOR.BASE_SCHEMA, OPERATOR.TARGET_SCHEMA), (22, 23))
        self.assertEqual(OPERATOR.OLD, "1ead2fcaa22df227d3d8b6b607978887ccfee7868139fc38f40523dbe24752ef")
        self.assertEqual((OPERATOR.OLD_PID, OPERATOR.OLD_START), (3668655, "26912384"))
        self.assertEqual(OPERATOR.OLD_SOURCE_COUNT, 470)
        self.assertEqual(OPERATOR.OLD_SOURCE_MANIFEST_SHA, "0d3d597cb5a7369cc735f3728633edf223b70f03487e44ab9c3f839150919594")
        self.assertEqual(OPERATOR.REFERENCE_PID, 3131777)
        self.assertEqual(OPERATOR.REFERENCE_START, "13964831")
        self.assertEqual(OPERATOR.BACKUP, Path("/opt/goby-test/backups/m5j-20260910"))
        self.assertEqual(OPERATOR.OWNER, "goby-m5j-deployment-backup-v1")
        self.assertEqual(OPERATOR.CANDIDATE_OWNER, "goby-m5j-candidate-v1")
        self.assertEqual(OPERATOR.OLD_TABLES, SCHEMA22_TABLES)
        self.assertEqual(OPERATOR.ADDED_TABLES, set())
        self.assertEqual(len(SCHEMA22_TABLES), 29)
        self.assertEqual(OPERATOR.EVIDENCE.parent, OPERATOR.BACKUP)
        self.assertEqual(OPERATOR.RECOVERY_EVIDENCE.parent, OPERATOR.BACKUP)
        self.assertEqual(OPERATOR.INSTALLATION_STARTED.parent, OPERATOR.BACKUP)
        self.assertEqual(OPERATOR.LOG_DIRECTORY, Path("/var/log/goby-test"))
        self.assertEqual(OPERATOR.OBSERVABILITY_DROPIN, Path("/etc/systemd/system/goby-foundation-test.service.d/30-observability.conf"))
        self.assertEqual(OPERATOR.RECOVERY_DROPIN, Path("/etc/systemd/system/goby-foundation-test.service.d/40-backup-recovery.conf"))
        self.assertEqual(OPERATOR.RECOVERY_DIRECTORIES, (Path("/var/lib/goby-test/recovery-m5j"),
                         Path("/var/lib/goby-test/backups-m5j"), Path("/var/lib/goby-test/recovery-operations-m5j")))
        self.assertEqual(OPERATOR.OBSERVABILITY_BYTES, b"[Service]\nEnvironment=GOBY_LOG_DIR=/var/log/goby-test\nLogsDirectory=goby-test\nLogsDirectoryMode=0700\n")
        path = OPERATOR.SCRATCH / "accepted-unique-m5j.json"
        with patch.object(sys, "argv", ["operator", "--candidate-manifest", str(path), "--manifest-sha256", "e" * 64]):
            accepted = OPERATOR.arguments()
        self.assertEqual(accepted.candidate_manifest, path)
        self.assertEqual(accepted.manifest_sha256, "e" * 64)
        self.assertIs(accepted.restore, False)
        with patch.object(sys, "argv", ["operator", "--restore"]):
            self.assertIs(OPERATOR.arguments().restore, True)
        for options in ([], ["--candidate-manifest", str(path)], ["--manifest-sha256", "e" * 64],
                        ["--restore", "--candidate-manifest", str(path), "--manifest-sha256", "e" * 64]):
            with self.subTest(arguments=options), patch.object(sys, "argv", ["operator", *options]):
                with self.assertRaises(RuntimeError):
                    OPERATOR.arguments()

    def test_candidate_loader_binds_dynamic_manifest_bytes_and_all_four_gate_files(self) -> None:
        candidate = self.candidate()
        path = OPERATOR.SCRATCH / "accepted-unique-m5j.json"
        payload = OPERATOR.canonical(candidate)
        digest = hashlib.sha256(payload).hexdigest()
        assets = {"index.html": b"synthetic index", "assets/synthetic.js": b"synthetic script"}
        regular = Mock()
        self.replace(OPERATOR, "regular", regular)
        self.replace(OPERATOR, "directory", Mock())
        self.replace(Path, "read_bytes", lambda current: payload if current == path else self.fail("Unexpected candidate byte read"))
        self.replace(Path, "resolve", lambda current, **options: Path(candidate["source"]["resolved_path"])
                     if current == Path(candidate["source"]["path"]) else self.fail("Unexpected source resolution"))
        self.replace(Path, "read_text", lambda current: candidate["source"]["owner_marker"]
                     if current == Path(candidate["source"]["resolved_path"]) / "OWNER.txt" else self.fail("Unexpected ownership-marker read"))
        self.replace(Path, "stat", lambda current: types.SimpleNamespace(st_mode=stat.S_IFREG | 0o644,
                                                                          st_size=candidate["binary"]["size"]))
        self.replace(OPERATOR, "source_inventory", Mock(return_value=candidate["source"]["files"]))
        self.replace(OPERATOR, "read_assets", Mock(return_value=assets))
        digests = {Path(candidate[section]["path"]): candidate[section]["sha256"] for section in ("binary", "assets")}
        digests.update({Path(gate["evidence_path"]): gate["sha256"] for gate in candidate["gates"].values()})
        self.replace(OPERATOR, "sha", lambda current: digests[current])
        self.assertEqual(OPERATOR.load_candidate(path, digest), (candidate, payload, assets))
        regular.assert_any_call(path, 2 * OPERATOR.MIB, 0o600)
        for name, gate in candidate["gates"].items():
            gate_path = Path(gate["evidence_path"])
            regular.assert_any_call(gate_path, 128 * OPERATOR.MIB)
            with self.subTest(corrupted_gate=name):
                digests[gate_path] = "f" * 64
                with self.assertRaises(RuntimeError):
                    OPERATOR.load_candidate(path, digest)
                digests[gate_path] = gate["sha256"]
        with self.assertRaises(RuntimeError):
            OPERATOR.load_candidate(path, "f" * 64)
        for unsafe in (Path("/tmp/accepted-m5j.json"), OPERATOR.SCRATCH / "nested/accepted.json",
                       OPERATOR.SCRATCH / "accepted.txt"):
            with self.subTest(manifest_path=str(unsafe)):
                with self.assertRaises(RuntimeError):
                    OPERATOR.load_candidate(unsafe, digest)
        with patch.object(Path, "stat", Mock(return_value=types.SimpleNamespace(st_mode=stat.S_IFREG | 0o666,
                                                                               st_size=candidate["binary"]["size"]))):
            with self.assertRaises(RuntimeError):
                OPERATOR.load_candidate(path, digest)
        noncanonical = json.dumps(candidate, indent=2).encode()
        with patch.object(Path, "read_bytes", Mock(return_value=noncanonical)):
            with self.assertRaises(RuntimeError):
                OPERATOR.load_candidate(path, hashlib.sha256(noncanonical).hexdigest())

    def test_source_inventory_binds_the_embedded_catalog_and_refuses_other_json_paths(self) -> None:
        root = Path("/synthetic/source")
        relative = "internal/backuppg/catalogs/schema-23-postgresql-17.json"
        catalog = root / relative
        regular = Mock()
        self.replace(OPERATOR, "regular", regular)
        self.replace(OPERATOR, "sha", Mock(return_value="a" * 64))
        self.replace(Path, "glob", lambda current, pattern: [catalog]
                     if current == root and pattern == "internal/backuppg/catalogs/*.json" else [])
        self.assertEqual(OPERATOR.source_inventory(root), {"go.mod": "a" * 64, "go.sum": "a" * 64, relative: "a" * 64})
        regular.assert_any_call(catalog, 2 * OPERATOR.MIB, 0o644)
        self.assertIs(OPERATOR.source_entry(relative), True)
        for unsafe in ("internal/backuppg/catalogs/../schema-23-postgresql-17.json",
                       "internal/backuppg/schema-23-postgresql-17.json", "internal/secrets.json",
                       "internal/backuppg/catalogs/schema-23-postgresql-17.json/extra",
                       "/internal/backuppg/catalogs/schema-23-postgresql-17.json"):
            with self.subTest(path=unsafe):
                self.assertIs(OPERATOR.source_entry(unsafe), False)

    def test_recovery_preflight_requires_absence_private_parents_and_no_policy_overrides(self) -> None:
        occupied = None
        symlink = False
        runtime = "GOBY_API_KEY_MASTER_KEY_FILE=/retained/master.key\n"
        effective = b"GOBY_LOG_DIR=/var/log/goby-test"
        group = 986
        free = 1024 * OPERATOR.MIB
        directories = Mock(side_effect=lambda current, *_args: types.SimpleNamespace(st_gid=group)
                           if current == OPERATOR.RECOVERY_PARENT else types.SimpleNamespace(st_gid=0))
        self.replace(OPERATOR, "directory", directories)
        self.replace(OPERATOR, "regular", Mock(return_value=types.SimpleNamespace(st_gid=0)))
        self.replace(Path, "exists", lambda current: current == occupied and not symlink)
        self.replace(Path, "is_symlink", lambda current: current == occupied and symlink)
        self.replace(Path, "read_text", lambda current: runtime if current == OPERATOR.RUNTIME else self.fail("Unexpected runtime read"))
        self.replace(OPERATOR, "command", lambda *_args, **_options: effective)
        self.replace(shutil, "disk_usage", lambda current: types.SimpleNamespace(free=free)
                     if current == OPERATOR.RECOVERY_PARENT else self.fail("Unexpected storage query"))
        baseline = OPERATOR.recovery_baseline()
        self.assertEqual(baseline, {"owner": OPERATOR.OWNER, "directories": [str(path) for path in OPERATOR.RECOVERY_DIRECTORIES],
                         "directories_absent": True, "dropin": str(OPERATOR.RECOVERY_DROPIN), "dropin_absent": True,
                         "dropin_sha256": hashlib.sha256(OPERATOR.RECOVERY_BYTES).hexdigest()})
        self.assertEqual(OPERATOR.preflight_recovery(), baseline)
        directories.assert_any_call(OPERATOR.RECOVERY_PARENT, 0o750, 995)
        for occupied in (*OPERATOR.RECOVERY_DIRECTORIES, OPERATOR.RECOVERY_DROPIN, OPERATOR.RECOVERY_ENV):
            for symlink in (False, True):
                with self.subTest(path=str(occupied), symlink=symlink):
                    with self.assertRaises(RuntimeError):
                        OPERATOR.preflight_recovery()
        occupied = None
        for name in ("GOBY_RECOVERY_STATE_DIR", "GOBY_RECOVERY_DATABASE_URL", "GOBY_BACKUP_DIR",
                     "GOBY_BACKUP_MAX_BYTES", "GOBY_PG_DUMP", "GOBY_PG_RESTORE"):
            for origin in ("runtime", "systemd"):
                with self.subTest(policy=name, origin=origin):
                    runtime = "export " + name + "=/unowned" if origin == "runtime" else ""
                    effective = (name + "=/unowned").encode() if origin == "systemd" else b""
                    with self.assertRaises(RuntimeError):
                        OPERATOR.preflight_recovery()
        runtime, effective = "", b""
        group = 995
        with self.assertRaises(RuntimeError):
            OPERATOR.preflight_recovery()
        group, free = 986, 768 * OPERATOR.MIB
        with self.assertRaises(RuntimeError):
            OPERATOR.preflight_recovery()

    def test_recovery_dropin_requires_exact_bytes_mode_owner_type_and_path(self) -> None:
        accepted = dict(st_dev=1, st_ino=7, st_mode=stat.S_IFREG | 0o644, st_uid=0, st_gid=0,
                        st_nlink=1, st_size=len(OPERATOR.RECOVERY_BYTES))
        metadata = dict(accepted)
        payload = OPERATOR.RECOVERY_BYTES
        resolved = OPERATOR.RECOVERY_DROPIN
        self.replace(Path, "lstat", lambda current: types.SimpleNamespace(**metadata)
                     if current == OPERATOR.RECOVERY_DROPIN else self.fail("Unexpected drop-in lstat"))
        self.replace(Path, "resolve", lambda current, **_options: resolved)
        self.replace(Path, "read_bytes", lambda current: payload
                     if current == OPERATOR.RECOVERY_DROPIN else self.fail("Unexpected drop-in byte read"))
        self.assertEqual(OPERATOR.installed_recovery_dropin_identity(), [1, 7, 0o644, 0, 0])
        metadata["st_nlink"] = 2
        self.assertEqual(OPERATOR.installed_recovery_dropin_identity(), [1, 7, 0o644, 0, 0])
        for field, value in (("st_mode", stat.S_IFREG | 0o666), ("st_mode", stat.S_IFLNK | 0o644),
                             ("st_mode", stat.S_IFIFO | 0o644), ("st_uid", 995), ("st_gid", 986),
                             ("st_nlink", 3), ("st_size", 0)):
            with self.subTest(field=field, value=value):
                metadata = {**accepted, field: value}
                with self.assertRaises(RuntimeError):
                    OPERATOR.installed_recovery_dropin_identity()
        metadata = dict(accepted)
        payload = OPERATOR.RECOVERY_BYTES.replace(b"m5j", b"m5x")
        with self.assertRaises(RuntimeError):
            OPERATOR.installed_recovery_dropin_identity()
        payload, resolved = OPERATOR.RECOVERY_BYTES, Path("/synthetic/replaced-dropin")
        with self.assertRaises(RuntimeError):
            OPERATOR.installed_recovery_dropin_identity()

    def test_recovery_intent_binds_candidate_dropin_inode_and_all_private_directory_inodes(self) -> None:
        intent = self.recovery_intent()
        document = intent
        exists = False
        regular = Mock()
        self.replace(OPERATOR, "regular", regular)
        self.replace(Path, "exists", lambda current: exists and current == OPERATOR.RECOVERY_INTENT)
        self.replace(Path, "is_symlink", Mock(return_value=False))
        self.replace(Path, "read_bytes", lambda current: OPERATOR.canonical(document)
                     if current == OPERATOR.RECOVERY_INTENT else self.fail("Unexpected intent read"))
        self.assertIsNone(OPERATOR.load_recovery_intent(self.backup()))
        regular.assert_not_called()
        exists = True
        self.assertEqual(OPERATOR.load_recovery_intent(self.backup()), intent)
        regular.assert_called_once_with(OPERATOR.RECOVERY_INTENT, 8192, 0o600)
        variants = []
        for field, value in (("owner", "unowned"), ("candidate_sha256", "f" * 64),
                             ("stage", "/synthetic/unowned-stage"), ("identity", [1, 7, 0o644, 995, 0]),
                             ("identity", [1, 7, 0o600, 0, 0]), ("identity", [True, 7, 0o644, 0, 0]),
                             ("baseline", {**intent["baseline"], "directories_absent": False})):
            variants.append({**intent, field: value})
        variants.append({**intent, "unrelated": True})
        for field in intent:
            altered = copy.deepcopy(intent)
            del altered[field]
            variants.append(altered)
        for path in OPERATOR.RECOVERY_DIRECTORIES:
            for position, value in ((0, True), (2, 0o755), (3, 0), (4, 995)):
                altered = copy.deepcopy(intent)
                altered["directories"][str(path)][position] = value
                variants.append(altered)
            altered = copy.deepcopy(intent)
            del altered["directories"][str(path)]
            variants.append(altered)
        altered = copy.deepcopy(intent)
        altered["directories"]["/synthetic/unowned"] = [1, 77, 0o700, 995, 986]
        variants.append(altered)
        for document in variants:
            with self.subTest(document=document):
                with self.assertRaises(RuntimeError):
                    OPERATOR.load_recovery_intent(self.backup())

    def test_recovery_install_binds_new_directory_descriptors_and_publishes_intent_before_link(self) -> None:
        backup, intent = self.backup(), self.recovery_intent()
        stage = Path(intent["stage"])
        events, files, directories, descriptors, published = [], {}, {}, {}, {}
        authorized, live_pid, parent_device, collision = True, b"0\n", 1, False
        occupied = None
        stage_info = dict(st_dev=1, st_ino=7, st_mode=stat.S_IFREG | 0o644, st_uid=0, st_gid=0)
        self.replace(OPERATOR, "installation_proven", lambda current: authorized and current == backup)
        self.replace(Path, "read_bytes", lambda current: OPERATOR.canonical(OPERATOR.recovery_baseline())
                     if current == OPERATOR.BACKUP / "recovery-before.json" else self.fail("Unexpected install read"))
        self.replace(Path, "exists", lambda current: current == occupied or current in files or current in directories)
        self.replace(Path, "is_symlink", Mock(return_value=False))

        def directory(current, mode=None, owner=0):
            if current in directories:
                info = directories[current]
                self.assertEqual((stat.S_IMODE(info["st_mode"]), info["st_uid"]), (mode, owner))
                return types.SimpleNamespace(**info)
            self.assertIn(current, {OPERATOR.RECOVERY_PARENT, OPERATOR.RECOVERY_DROPIN.parent})
            return types.SimpleNamespace(st_dev=1, st_ino=3, st_mode=stat.S_IFDIR | mode, st_uid=owner, st_gid=986)

        def mkdir(current, mode):
            self.assertIn(current, OPERATOR.RECOVERY_DIRECTORIES)
            self.assertEqual(mode, 0o700)
            self.assertNotIn(current, directories)
            index = OPERATOR.RECOVERY_DIRECTORIES.index(current)
            directories[current] = dict(st_dev=1, st_ino=20 + index, st_mode=stat.S_IFDIR | mode, st_uid=0, st_gid=0)
            events.append(("mkdir", current))

        def open_descriptor(current, flags):
            if current == stage:
                self.assertEqual(flags, os.O_RDONLY | os.O_NOFOLLOW)
                descriptor = 88
            else:
                self.assertIn(current, directories)
                self.assertEqual(flags, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
                descriptor = 50 + OPERATOR.RECOVERY_DIRECTORIES.index(current)
            descriptors[descriptor] = current
            return descriptor

        def fstat(descriptor):
            current = descriptors[descriptor]
            return types.SimpleNamespace(**(stage_info if current == stage else directories[current]))

        def fchown(descriptor, uid, gid):
            current = descriptors[descriptor]
            self.assertIn(current, directories)
            self.assertEqual((uid, gid), (995, 986))
            directories[current].update(st_uid=uid, st_gid=gid)
            events.append(("chown", current))

        def fchmod(descriptor, mode):
            current = descriptors[descriptor]
            self.assertIn(current, directories)
            self.assertEqual(mode, 0o700)
            directories[current]["st_mode"] = stat.S_IFDIR | mode

        def command(arguments, _label, **_options):
            events.append(("command", arguments))
            if arguments == ["/usr/bin/systemctl", "show", OPERATOR.SERVICE, "-p", "MainPID", "--value"]:
                return live_pid
            self.assertEqual(arguments, ["/usr/bin/systemctl", "daemon-reload"])
            self.assertIn(OPERATOR.RECOVERY_DROPIN, files)
            return b""

        def private(current, payload):
            self.assertEqual((current, payload), (stage, OPERATOR.RECOVERY_BYTES))
            self.assertNotIn(current, files)
            files[current] = payload

        def publish(current, payload):
            self.assertEqual(current, OPERATOR.RECOVERY_INTENT)
            self.assertEqual(json.loads(payload), intent)
            published[current] = json.loads(payload)
            events.append(("intent", current))

        def link(source, destination, **options):
            self.assertEqual((source, destination, options), (stage, OPERATOR.RECOVERY_DROPIN, {"follow_symlinks": False}))
            self.assertEqual(published[OPERATOR.RECOVERY_INTENT], intent)
            if collision:
                raise FileExistsError("Synthetic recovery target appeared before publication")
            files[destination] = files[source]
            events.append(("link", destination))

        def unlink(current):
            self.assertEqual(current, stage, "Provisioning must preserve old configuration and every recovery store")
            del files[current]
            events.append(("unlink", current))

        self.replace(OPERATOR, "directory", directory)
        self.replace(Path, "mkdir", mkdir)
        self.replace(os, "open", open_descriptor)
        self.replace(os, "fstat", fstat)
        self.replace(os, "fchown", fchown)
        self.replace(os, "fchmod", fchmod)
        self.replace(os, "fsync", lambda descriptor: events.append(("fsync", descriptors[descriptor])))
        self.replace(os, "close", lambda descriptor: descriptors.pop(descriptor))
        self.replace(OPERATOR, "command", command)
        self.replace(OPERATOR, "private", private)
        self.replace(OPERATOR, "publish_exclusive", publish)
        self.replace(Path, "chmod", lambda current, mode: self.assertEqual((current, mode), (stage, 0o644)))
        self.replace(Path, "stat", lambda current: types.SimpleNamespace(st_dev=parent_device)
                     if current == OPERATOR.RECOVERY_DROPIN.parent else self.fail("Unexpected parent stat"))
        self.replace(os, "link", link)
        self.replace(OPERATOR, "sync_directory", lambda current: events.append(("sync", current)))
        self.replace(OPERATOR, "installed_recovery_dropin_identity", Mock(return_value=intent["identity"]))
        self.replace(Path, "unlink", unlink)
        OPERATOR.install_recovery(backup)
        self.assertEqual(files, {OPERATOR.RECOVERY_DROPIN: OPERATOR.RECOVERY_BYTES})
        self.assertEqual(set(directories), set(OPERATOR.RECOVERY_DIRECTORIES))
        self.assertEqual(descriptors, {})
        for path in OPERATOR.RECOVERY_DIRECTORIES:
            self.assertLess(events.index(("chown", path)), events.index(("intent", OPERATOR.RECOVERY_INTENT)))
            self.assertLess(events.index(("fsync", path)), events.index(("intent", OPERATOR.RECOVERY_INTENT)))
        self.assertLess(events.index(("fsync", stage)), events.index(("intent", OPERATOR.RECOVERY_INTENT)))
        self.assertLess(events.index(("intent", OPERATOR.RECOVERY_INTENT)), events.index(("link", OPERATOR.RECOVERY_DROPIN)))
        self.assertEqual(events[-1], ("command", ["/usr/bin/systemctl", "daemon-reload"]))
        for failure in ("unauthorized", "running-service", "preexisting-directory", "cross-filesystem", "publication-race"):
            with self.subTest(failure=failure):
                events.clear()
                files.clear()
                directories.clear()
                published.clear()
                authorized = failure != "unauthorized"
                live_pid = b"12345\n" if failure == "running-service" else b"0\n"
                occupied = OPERATOR.RECOVERY_DIRECTORY if failure == "preexisting-directory" else None
                parent_device = 2 if failure == "cross-filesystem" else 1
                collision = failure == "publication-race"
                with self.assertRaises((RuntimeError, FileExistsError)):
                    OPERATOR.install_recovery(backup)
                self.assertNotIn(OPERATOR.RECOVERY_DROPIN, files)
                self.assertNotIn(("command", ["/usr/bin/systemctl", "daemon-reload"]), events)
                self.assertEqual(descriptors, {})
                if failure in {"unauthorized", "running-service", "preexisting-directory"}:
                    self.assertEqual((files, directories), ({}, {}))
                if failure != "publication-race":
                    self.assertEqual(published, {})

    def test_recovery_preserves_private_stores_and_removes_only_owned_configuration(self) -> None:
        backup, intent = self.backup(), self.recovery_intent()
        stage = Path(intent["stage"])
        paths = {OPERATOR.RECOVERY_DROPIN, stage, OPERATOR.LOG_DIRECTORY, *OPERATOR.RECOVERY_DIRECTORIES}
        events = []
        current_intent, installed_identity, live_pid = intent, intent["identity"], b"0\n"
        info = types.SimpleNamespace(st_dev=1, st_ino=7, st_mode=stat.S_IFREG | 0o644, st_uid=0, st_gid=0)

        def command(arguments, _label, **_options):
            events.append(("command", arguments))
            if arguments == ["/usr/bin/systemctl", "show", OPERATOR.SERVICE, "-p", "MainPID", "--value"]:
                return live_pid
            self.assertEqual(arguments, ["/usr/bin/systemctl", "daemon-reload"])
            return b""

        def unlink(current):
            self.assertIn(current, {OPERATOR.RECOVERY_DROPIN, stage}, "Recovery must retain diagnostic and private store bytes")
            paths.remove(current)
            events.append(("unlink", current))

        def directory(current, mode, owner):
            identity = intent["directories"][str(current)]
            self.assertEqual((mode, owner), (0o700, 995))
            return types.SimpleNamespace(st_dev=identity[0], st_ino=identity[1], st_mode=stat.S_IFDIR | identity[2],
                                         st_uid=identity[3], st_gid=identity[4])

        self.replace(OPERATOR, "command", command)
        self.replace(OPERATOR, "load_recovery_intent", lambda current: current_intent if current == backup else self.fail("Unexpected backup"))
        self.replace(OPERATOR, "installed_recovery_dropin_identity", lambda: installed_identity)
        self.replace(OPERATOR, "regular", Mock(return_value=info))
        self.replace(OPERATOR, "directory", directory)
        self.replace(Path, "exists", lambda current: current in paths)
        self.replace(Path, "is_symlink", Mock(return_value=False))
        self.replace(Path, "read_bytes", lambda current: OPERATOR.RECOVERY_BYTES
                     if current == stage else self.fail("Unowned recovery read"))
        self.replace(Path, "unlink", unlink)
        self.replace(OPERATOR, "sync_directory", lambda current: events.append(("sync", current)))
        report = OPERATOR.preserve_recovery(backup)
        self.assertEqual(report, {"directories_deleted": False, "dropin_removed_or_never_installed": True,
                                 "retained_directories": {str(path): "owned_directory_retained" for path in OPERATOR.RECOVERY_DIRECTORIES}})
        self.assertEqual(paths, {OPERATOR.LOG_DIRECTORY, *OPERATOR.RECOVERY_DIRECTORIES})
        self.assertEqual([value for kind, value in events if kind == "unlink"], [OPERATOR.RECOVERY_DROPIN, stage])
        events.clear()
        self.assertEqual(OPERATOR.preserve_recovery(backup), report)
        self.assertEqual(events, [("command", ["/usr/bin/systemctl", "show", OPERATOR.SERVICE, "-p", "MainPID", "--value"]),
                                  ("command", ["/usr/bin/systemctl", "daemon-reload"])])
        current_intent = None
        retained = OPERATOR.preserve_recovery(backup)
        self.assertEqual(retained["retained_directories"], {str(path): "retained_without_inventory" for path in OPERATOR.RECOVERY_DIRECTORIES})
        for refusal in ("running", "unowned", "replaced"):
            with self.subTest(refusal=refusal):
                events.clear()
                paths.add(OPERATOR.RECOVERY_DROPIN)
                live_pid = b"2345\n" if refusal == "running" else b"0\n"
                current_intent = None if refusal == "unowned" else intent
                installed_identity = [1, 88, 0o644, 0, 0] if refusal == "replaced" else intent["identity"]
                with self.assertRaises(RuntimeError):
                    OPERATOR.preserve_recovery(backup)
                self.assertFalse(any(kind == "unlink" for kind, _value in events))
                self.assertNotIn(("command", ["/usr/bin/systemctl", "daemon-reload"]), events)
                self.assertTrue({OPERATOR.LOG_DIRECTORY, *OPERATOR.RECOVERY_DIRECTORIES}.issubset(paths))

    def test_private_store_reader_pins_nofollow_descriptor_and_exact_bounded_bytes(self) -> None:
        path = OPERATOR.RECOVERY_DIRECTORY / "generation-registry.json"
        payload = b'{"version":1}'
        accepted = dict(st_dev=1, st_ino=9, st_mode=stat.S_IFREG | 0o600, st_uid=995, st_gid=986,
                        st_nlink=1, st_size=len(payload), st_mtime_ns=3, st_ctime_ns=4)
        metadata, opened, final, data = dict(accepted), dict(accepted), dict(accepted), payload
        observations = []

        class MemoryFile(io.BytesIO):
            def fileno(self):
                return 17

            def read(self, size=-1):
                observations.append(size)
                return super().read(size)

        self.replace(OPERATOR, "regular", lambda current, maximum, mode, owner: types.SimpleNamespace(**metadata)
                     if (current, maximum, mode, owner) == (path, 16384, 0o600, 995) else self.fail("Unexpected private metadata read"))
        self.replace(os, "open", lambda current, flags: 17
                     if (current, flags) == (path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK) else self.fail("Unowned metadata open"))
        self.replace(os, "fdopen", lambda descriptor, mode: MemoryFile(data)
                     if (descriptor, mode) == (17, "rb") else self.fail("Unexpected metadata descriptor"))
        self.replace(os, "fstat", lambda descriptor: types.SimpleNamespace(**opened)
                     if descriptor == 17 else self.fail("Unowned metadata fstat"))
        self.replace(Path, "lstat", lambda current: types.SimpleNamespace(**final)
                     if current == path else self.fail("Unexpected private metadata lstat"))
        self.assertEqual(OPERATOR.read_private_store_json(path, 16384), {"version": 1})
        self.assertEqual(observations, [len(payload) + 1])
        for label in ("wrong-group", "opened-replacement", "path-replacement", "truncated", "extended"):
            with self.subTest(case=label):
                metadata, opened, final, data = dict(accepted), dict(accepted), dict(accepted), payload
                if label == "wrong-group":
                    metadata["st_gid"] = 995
                elif label == "opened-replacement":
                    opened["st_ino"] = 88
                elif label == "path-replacement":
                    final["st_ino"] = 88
                elif label == "truncated":
                    data = payload[:-1]
                else:
                    data = payload + b" "
                with self.assertRaises(RuntimeError):
                    OPERATOR.read_private_store_json(path, 16384)
        for data in (b'{"version":1,"version":1}', b'{"value":{"key":"first","key":"second"}}'):
            metadata = {**accepted, "st_size": len(data)}
            opened, final = dict(metadata), dict(metadata)
            with self.subTest(duplicate_json=data):
                with self.assertRaises(RuntimeError):
                    OPERATOR.read_private_store_json(path, 16384)

    def test_initial_deployment_identity_requires_the_owned_lifecycle_directory_and_lock(self) -> None:
        intent = self.recovery_intent()
        directory_identity = intent["directories"][str(OPERATOR.RECOVERY_DIRECTORY)]
        observed_identity = list(directory_identity)
        current_intent = intent
        accepted = {"version": 1, "deploymentId": "1" * 32, "lock": {"device": 1, "inode": 80}}
        marker = accepted
        self.replace(OPERATOR, "load_recovery_intent", lambda _backup: current_intent)
        self.replace(OPERATOR, "directory", lambda current, mode, owner: types.SimpleNamespace(
            st_dev=observed_identity[0], st_ino=observed_identity[1], st_mode=stat.S_IFDIR | observed_identity[2],
            st_uid=observed_identity[3], st_gid=observed_identity[4])
            if (current, mode, owner) == (OPERATOR.RECOVERY_DIRECTORY, 0o700, 995) else self.fail("Unexpected lifecycle directory"))
        self.replace(OPERATOR, "read_private_store_json", lambda current, maximum: marker
                     if (current, maximum) == (OPERATOR.RECOVERY_DIRECTORY / ".goby-lifecycle.json", 16384)
                     else self.fail("Unexpected lifecycle metadata"))
        self.replace(OPERATOR, "regular", lambda current, maximum, mode, owner:
                     types.SimpleNamespace(st_dev=1, st_ino=80, st_size=0)
                     if (current, maximum, mode, owner) == (OPERATOR.RECOVERY_DIRECTORY / ".goby-lifecycle.lock", 0, 0o600, 995)
                     else self.fail("Unexpected lifecycle lock"))
        self.assertEqual(OPERATOR.initial_deployment_id(self.backup()), "1" * 32)
        for marker in ({**accepted, "version": True}, {**accepted, "version": 2},
                       {**accepted, "deploymentId": "A" * 32}, {**accepted, "deploymentId": "1" * 31},
                       {**accepted, "lock": {"device": 1, "inode": 81}}, {**accepted, "unrelated": True}):
            with self.subTest(marker=marker):
                with self.assertRaises(RuntimeError):
                    OPERATOR.initial_deployment_id(self.backup())
        marker = accepted
        observed_identity[1] = 88
        with self.assertRaises(RuntimeError):
            OPERATOR.initial_deployment_id(self.backup())
        observed_identity = list(directory_identity)
        current_intent = None
        with self.assertRaises(RuntimeError):
            OPERATOR.initial_deployment_id(self.backup())

    def test_recovery_verification_requires_empty_initial_stores_and_exact_effective_policy(self) -> None:
        intent, deployment, token, store_id = self.recovery_intent(), "1" * 32, "2" * 32, "3" * 32
        active_intent, dropin_identity = intent, list(intent["identity"])
        directory_identities = copy.deepcopy(intent["directories"])
        expected_names = {
            OPERATOR.RECOVERY_DIRECTORY: {".goby-lifecycle.json", ".goby-lifecycle.lock", "generation-registry.json"},
            OPERATOR.BACKUP_DIRECTORY: {".goby-backup-store.json", ".goby-backup-store.lock", ".goby-backup-catalog.json"},
            OPERATOR.OPERATIONS_DIRECTORY: {".goby-recovery-control.json", ".goby-recovery-control.lock", "current.json", "cas-proof.json"},
        }
        names = copy.deepcopy(expected_names)
        metadata = {path / name: dict(st_dev=1, st_ino=100 + index * 10 + offset, st_mode=stat.S_IFREG | 0o600,
                                     st_uid=995, st_gid=986, st_size=0 if name.endswith(".lock") else 40)
                    for index, (path, entries) in enumerate(expected_names.items()) for offset, name in enumerate(sorted(entries))}
        control_lock = metadata[OPERATOR.OPERATIONS_DIRECTORY / ".goby-recovery-control.lock"]
        slots = [{"slot": slot, "state": state, "imageId": "", "name": "", "captured": "0001-01-01T00:00:00Z", "operation": ""}
                 for slot, state in (("primary", "active"), ("recovery", "unclaimed"))]
        accepted = {
            OPERATOR.RECOVERY_DIRECTORY / "generation-registry.json": {"version": 1, "deploymentId": deployment,
                "baselineDigest": hashlib.sha256(b"").hexdigest(), "generations": []},
            OPERATOR.BACKUP_DIRECTORY / ".goby-backup-store.json": {"format": "goby-backupstore-v1", "token": token},
            OPERATOR.BACKUP_DIRECTORY / ".goby-backup-catalog.json": {"version": 1, "token": token, "entries": []},
            OPERATOR.OPERATIONS_DIRECTORY / ".goby-recovery-control.json": {"version": 1, "deploymentId": deployment,
                "storeId": store_id, "lock": {"device": control_lock["st_dev"], "inode": control_lock["st_ino"]}},
            OPERATOR.OPERATIONS_DIRECTORY / "current.json": {"version": 1, "deploymentId": deployment, "storeId": store_id,
                "revision": 1, "previousDigest": "a" * 64, "payload": {"version": 1, "deploymentId": deployment,
                "operations": [], "slots": slots}},
        }
        documents = copy.deepcopy(accepted)
        policy = " ".join(("GOBY_LOG_DIR=/var/log/goby-test", "GOBY_RECOVERY_STATE_DIR=" + str(OPERATOR.RECOVERY_DIRECTORY),
                           "GOBY_BACKUP_DIR=" + str(OPERATOR.BACKUP_DIRECTORY),
                           "GOBY_RECOVERY_OPERATIONS_DIR=" + str(OPERATOR.OPERATIONS_DIRECTORY),
                           "GOBY_PG_DUMP=/usr/lib/postgresql/17/bin/pg_dump", "GOBY_PG_RESTORE=/usr/lib/postgresql/17/bin/pg_restore"))
        effective = policy

        def directory(current, mode, owner):
            parts = directory_identities[str(current)]
            self.assertEqual((mode, owner), (0o700, 995))
            return types.SimpleNamespace(st_dev=parts[0], st_ino=parts[1], st_mode=stat.S_IFDIR | parts[2], st_uid=parts[3], st_gid=parts[4])

        self.replace(OPERATOR, "load_recovery_intent", lambda _backup: active_intent)
        self.replace(OPERATOR, "installed_recovery_dropin_identity", lambda: dropin_identity)
        self.replace(OPERATOR, "initial_deployment_id", Mock(return_value=deployment))
        self.replace(OPERATOR, "command", lambda *_args, **_options: ("Environment=" + effective).encode())
        self.replace(OPERATOR, "directory", directory)
        self.replace(Path, "iterdir", lambda current: [current / name for name in names[current]])
        self.replace(OPERATOR, "regular", lambda current, *_args: types.SimpleNamespace(**metadata[current]))
        self.replace(OPERATOR, "sha", Mock(return_value="a" * 64))
        self.replace(OPERATOR, "read_private_store_json", lambda current: documents[current])
        report = OPERATOR.verify_recovery(self.backup())
        self.assertEqual((report["deployment_id"], report["directory_mode"], report["directory_uid"], report["directory_gid"]),
                         (deployment, "0700", 995, 986))
        self.assertIs(report["initial_primary_generation"], True)
        self.assertEqual((report["backup_objects"], report["operations"]), (0, 0))
        self.assertEqual(set(report["stores"]), {str(path) for path in OPERATOR.RECOVERY_DIRECTORIES})
        variants = []
        for path, field, value in ((OPERATOR.RECOVERY_DIRECTORY / "generation-registry.json", "generations", [{"id": "new-generation"}]),
                                   (OPERATOR.RECOVERY_DIRECTORY / "generation-registry.json", "deploymentId", "f" * 32),
                                   (OPERATOR.RECOVERY_DIRECTORY / "generation-registry.json", "version", True),
                                   (OPERATOR.BACKUP_DIRECTORY / ".goby-backup-catalog.json", "entries", [{"id": "new-backup"}]),
                                   (OPERATOR.BACKUP_DIRECTORY / ".goby-backup-catalog.json", "token", "f" * 32),
                                   (OPERATOR.BACKUP_DIRECTORY / ".goby-backup-catalog.json", "version", True),
                                   (OPERATOR.OPERATIONS_DIRECTORY / ".goby-recovery-control.json", "version", True),
                                   (OPERATOR.OPERATIONS_DIRECTORY / "current.json", "revision", 2),
                                   (OPERATOR.OPERATIONS_DIRECTORY / "current.json", "revision", True),
                                   (OPERATOR.OPERATIONS_DIRECTORY / "current.json", "version", True),
                                   (OPERATOR.OPERATIONS_DIRECTORY / "current.json", "storeId", "f" * 32)):
            changed = copy.deepcopy(accepted)
            changed[path][field] = value
            variants.append((field, changed))
        changed = copy.deepcopy(accepted)
        changed[OPERATOR.OPERATIONS_DIRECTORY / "current.json"]["payload"]["operations"] = [{"id": "new-operation"}]
        variants.append(("startup-operation", changed))
        changed = copy.deepcopy(accepted)
        changed[OPERATOR.OPERATIONS_DIRECTORY / "current.json"]["payload"]["slots"][1]["state"] = "retained"
        variants.append(("claimed-recovery-slot", changed))
        changed = copy.deepcopy(accepted)
        changed[OPERATOR.OPERATIONS_DIRECTORY / "current.json"]["payload"]["version"] = True
        variants.append(("payload-version-is-not-an-integer", changed))
        for label, documents in variants:
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_recovery(self.backup())
        documents = copy.deepcopy(accepted)
        for effective in (policy + " GOBY_BACKUP_MAX_BYTES=1", policy.replace("/recovery-m5j", "/unowned"),
                          policy + " GOBY_PG_DUMP=/usr/lib/postgresql/17/bin/pg_dump"):
            with self.subTest(environment=effective):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_recovery(self.backup())
        effective = policy
        for path in OPERATOR.RECOVERY_DIRECTORIES:
            names[path].add("unowned.json")
            with self.assertRaises(RuntimeError):
                OPERATOR.verify_recovery(self.backup())
            names[path].remove("unowned.json")
        directory_identities[str(OPERATOR.RECOVERY_DIRECTORY)][1] = 88
        with self.assertRaises(RuntimeError):
            OPERATOR.verify_recovery(self.backup())
        directory_identities = copy.deepcopy(intent["directories"])
        metadata[OPERATOR.OPERATIONS_DIRECTORY / "current.json"]["st_gid"] = 995
        with self.assertRaises(RuntimeError):
            OPERATOR.verify_recovery(self.backup())
        metadata[OPERATOR.OPERATIONS_DIRECTORY / "current.json"]["st_gid"] = 986
        dropin_identity = [1, 88, 0o644, 0, 0]
        with self.assertRaises(RuntimeError):
            OPERATOR.verify_recovery(self.backup())
        dropin_identity, active_intent = list(intent["identity"]), None
        with self.assertRaises(RuntimeError):
            OPERATOR.verify_recovery(self.backup())

    def test_recovery_role_requires_exact_low_privilege_flags_name_tag_and_valid_oid(self) -> None:
        _baseline, intent, role, _target, _schema = self.recovery_database_fixture()
        self.assertIsNone(OPERATOR._recovery_expected_role(role, intent))
        variants = []
        for field, value in (("oid", 0), ("oid", -1), ("oid", True), ("oid", "201"), ("oid", 201.0),
                             ("name", "goby_test"), ("comment", "foreign-ownership-tag"), ("login", False),
                             ("login", 1), ("inherit", True), ("connection_limit", -1), ("connection_limit", 17),
                             ("valid_until", "2042-01-02T03:04:05+00:00"), ("settings", [])):
            variants.append((field, {**role, field: value}))
        for field in ("superuser", "createdb", "createrole", "replication", "bypassrls"):
            for value in (True, None, 0):
                variants.append((field, {**role, field: value}))
        for field in role:
            if field in {"valid_until", "settings"}:
                continue
            altered = dict(role)
            del altered[field]
            variants.append(("missing-" + field, altered))
        for label, observed in variants:
            with self.subTest(case=label, role=observed):
                with self.assertRaises(RuntimeError):
                    OPERATOR._recovery_expected_role(observed, intent)

    def test_recovery_empty_namespace_refuses_hidden_objects_unsafe_grants_and_cluster_drift(self) -> None:
        baseline, _intent, _role, target, schema = self.recovery_database_fixture()
        self.assertIsNone(OPERATOR._recovery_check_empty(schema, target, baseline))
        allowed = copy.deepcopy(schema)
        allowed["public"]["comment"] = None
        self.assertIsNone(OPERATOR._recovery_check_empty(allowed, target, baseline))
        variants = []
        for field, value in (("database", "goby_test"), ("user", OPERATOR.RECOVERY_ROLE_NAME),
                             ("session_user", OPERATOR.RECOVERY_ROLE_NAME), ("database_oid", 999),
                             ("postmaster_start", "different-cluster-start"), ("version_num", 170007),
                             ("encoding", "LATIN1"), ("address", "127.0.0.1"), ("port", 5432),
                             ("empty", False), ("empty", 1), ("public", None)):
            variants.append((field, {**schema, field: value}))
        for field, value in (("oid", 0), ("oid", True), ("oid", "401"),
                             ("safe_owner", False), ("safe_owner", 1), ("safe_acl", False),
                             ("safe_acl", 1), ("comment", "foreign-namespace-tag")):
            altered = copy.deepcopy(schema)
            altered["public"][field] = value
            variants.append(("public-" + field, altered))
        for label, observed in variants:
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR._recovery_check_empty(observed, target, baseline)

    def test_recovery_catalog_retains_every_existing_role_database_setting_and_membership(self) -> None:
        baseline, _intent, role, target, _schema = self.recovery_database_fixture()
        self.assertEqual(OPERATOR._recovery_protected_catalog(copy.deepcopy(baseline["catalog"]), baseline), (None, None))
        current = copy.deepcopy(baseline["catalog"])
        current["roles"].append(role)
        current["databases"].append(target)
        self.assertEqual(OPERATOR._recovery_protected_catalog(current, baseline), (role, target))
        role_only = copy.deepcopy(current)
        role_only["databases"].pop()
        self.assertEqual(OPERATOR._recovery_protected_catalog(role_only, baseline), (role, None))
        variants = []
        for section, index, field, value in (("roles", 1, "oid", 999), ("roles", 2, "comment", {"sha256": "f" * 64}),
                                            ("databases", 0, "owner_oid", 1), ("databases", 0, "acl", "changed ACL"),
                                            ("memberships", 0, "admin", True),
                                            ("settings", 0, "settings", {"sha256": "f" * 64}),
                                            ("security_labels", 0, "label", {"sha256": "f" * 64})):
            changed = copy.deepcopy(current)
            changed[section][index][field] = value
            variants.append((section + "-" + field, changed))
        for field, value in (("system_identifier", "200000000001"), ("postmaster_start", "changed-start"),
                             ("version_num", 170007), ("port", 5433)):
            changed = copy.deepcopy(current)
            changed["cluster"][field] = value
            variants.append(("cluster-" + field, changed))
        for section in ("roles", "databases", "memberships", "settings", "security_labels"):
            changed = copy.deepcopy(current)
            changed[section].pop(0)
            variants.append(("missing-existing-" + section, changed))
        for section in ("roles", "databases"):
            changed = copy.deepcopy(current)
            changed[section].append(copy.deepcopy(changed[section][-1]))
            variants.append(("ambiguous-owned-" + section, changed))
        for field, value in (("owner_oid", 999), ("owner", "postgres")):
            changed = copy.deepcopy(current)
            changed["databases"][-1][field] = value
            variants.append(("foreign-target-" + field, changed))
        changed = copy.deepcopy(current)
        changed["roles"].pop()
        variants.append(("target-without-owned-role", changed))
        changed = copy.deepcopy(current)
        changed["memberships"].append({"role_oid": role["oid"], "member_oid": 10, "grantor_oid": 1,
                                        "admin": False, "inherit": False, "set": False})
        variants.append(("new-owned-role-membership", changed))
        for label, observed in variants:
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR._recovery_protected_catalog(observed, baseline)

    def test_recovery_database_verification_rejects_foreign_oids_tags_and_catalog_fingerprints(self) -> None:
        baseline, intent, role, target, schema = self.recovery_database_fixture()
        closed = {**target, "allow_connections": False}
        receipts = {
            OPERATOR.RECOVERY_ROLE_IDENTITY: ("role", role),
            OPERATOR.RECOVERY_DATABASE_IDENTITY: ("database", {"closed": closed, "open": target}),
            OPERATOR.RECOVERY_DATABASE_OPENING: ("transition", {"closed_sha256": OPERATOR._recovery_digest(closed),
                                                                 "open_sha256": OPERATOR._recovery_digest(target)}),
            OPERATOR.RECOVERY_DATABASE_READY: ("ready", {"schema": schema}),
        }
        accepted = copy.deepcopy(baseline["catalog"])
        accepted["roles"].append(role)
        accepted["databases"].append(target)
        observed = copy.deepcopy(accepted)
        database = types.SimpleNamespace(read=Mock(side_effect=AssertionError("The fixture cannot use a real database connection")))

        def receipt(path, expected_intent, field):
            self.assertEqual(expected_intent, intent)
            expected_field, value = receipts[path]
            self.assertEqual(field, expected_field)
            return value

        main_identity, environment = Mock(), Mock(return_value={"synthetic": "private environment"})
        namespace, connection = Mock(return_value=schema), Mock(return_value={"synthetic": "loopback identity"})
        self.replace(OPERATOR, "_recovery_baseline", Mock(return_value=baseline))
        self.replace(OPERATOR, "_recovery_intent", Mock(return_value=intent))
        self.replace(OPERATOR, "_recovery_receipt", receipt)
        self.replace(OPERATOR, "_recovery_present", Mock(return_value=False))
        self.replace(OPERATOR, "_recovery_catalog", lambda: observed)
        self.replace(OPERATOR, "_recovery_main_identity", main_identity)
        self.replace(OPERATOR, "_recovery_environment", environment)
        self.replace(OPERATOR, "_recovery_schema_observation", namespace)
        self.replace(OPERATOR, "_recovery_tcp_identity", connection)
        report = OPERATOR.verify_recovery_database(self.backup(), database)
        self.assertEqual((report["database_oid"], report["role_oid"], report["ownership_tag"]), (target["oid"], role["oid"], intent["tag"]))
        self.assertIs(report["preexisting_catalog_unchanged"], True)
        variants = []
        changed = copy.deepcopy(accepted)
        changed["roles"][-1]["oid"] = 999
        changed["databases"][-1]["owner_oid"] = 999
        variants.append(("foreign-role-oid-with-consistent-owner", changed))
        for section, field, value in (("databases", "oid", 999), ("roles", "comment", "foreign-tag"),
                                      ("databases", "comment", "foreign-tag"), ("roles", "superuser", True),
                                      ("databases", "acl", "changed grant"), ("databases", "allow_connections", False)):
            changed = copy.deepcopy(accepted)
            changed[section][-1][field] = value
            variants.append((section + "-" + field, changed))
        for label, observed in variants:
            for dependency in (main_identity, environment, namespace, connection):
                dependency.reset_mock()
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_recovery_database(self.backup(), database)
                for dependency in (main_identity, environment, namespace, connection):
                    dependency.assert_not_called()
        database.read.assert_not_called()

    def test_recovery_database_publication_barrier_precedes_provisioning_and_cleanup_dependencies(self) -> None:
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", True)
        baseline = Mock(side_effect=AssertionError("Published evidence must be checked before any recovery baseline"))
        catalog = Mock(side_effect=AssertionError("Published evidence must be checked before any catalog query"))
        command = Mock(side_effect=AssertionError("Published evidence must prohibit database commands"))
        entropy = Mock(side_effect=AssertionError("Published evidence must prohibit secret generation"))
        self.replace(OPERATOR, "_recovery_baseline", baseline)
        self.replace(OPERATOR, "_recovery_catalog", catalog)
        self.replace(OPERATOR, "command", command)
        self.replace(secrets, "token_bytes", entropy)
        for destination in (OPERATOR.EVIDENCE, OPERATOR.RECOVERY_EVIDENCE):
            for kind in ("file", "symlink"):
                with self.subTest(evidence=str(destination), kind=kind), \
                     patch.object(Path, "exists", lambda path: path == destination and kind == "file"), \
                     patch.object(Path, "is_symlink", lambda path: path == destination and kind == "symlink"):
                    with self.assertRaises(RuntimeError):
                        OPERATOR._recovery_unpublished()
                    with self.assertRaises(RuntimeError):
                        OPERATOR.provision_recovery_database(self.backup(), object())
                    with self.assertRaises(RuntimeError):
                        OPERATOR.remove_recovery_database(self.backup())
                    baseline.assert_not_called()
                    catalog.assert_not_called()
                    command.assert_not_called()
                    entropy.assert_not_called()

    def test_recovery_database_provisioning_and_cleanup_require_the_lock_before_dependencies(self) -> None:
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", False)
        self.replace(Path, "exists", Mock(return_value=False))
        self.replace(Path, "is_symlink", Mock(return_value=False))
        baseline = Mock(side_effect=AssertionError("An unlocked recovery must not load its baseline"))
        catalog = Mock(side_effect=AssertionError("An unlocked recovery must not query its catalog"))
        authorization = Mock(side_effect=AssertionError("An unlocked recovery must not consume installation proof"))
        self.replace(OPERATOR, "_recovery_baseline", baseline)
        self.replace(OPERATOR, "_recovery_catalog", catalog)
        self.replace(OPERATOR, "installation_proven", authorization)
        database = types.SimpleNamespace(read=Mock(side_effect=AssertionError("An unlocked recovery must not read the live database")))
        with self.assertRaises(RuntimeError):
            OPERATOR.provision_recovery_database(self.backup(), database)
        with self.assertRaises(RuntimeError):
            OPERATOR.remove_recovery_database(self.backup())
        baseline.assert_not_called()
        catalog.assert_not_called()
        authorization.assert_not_called()
        database.read.assert_not_called()

    def test_recovery_documents_require_exact_canonical_object_bytes_without_duplicate_keys(self) -> None:
        path = OPERATOR.RECOVERY_DB_INTENT
        accepted = {"owner": OPERATOR.OWNER, "nested": {"phase": "prepared"}}
        payload = OPERATOR.canonical(accepted)
        reads = Mock(side_effect=lambda current: payload if current == path else self.fail("Unexpected recovery proof path"))
        self.replace(OPERATOR, "_recovery_private_bytes", reads)
        self.assertEqual(OPERATOR._recovery_document(path), accepted)
        reads.assert_called_once_with(path)
        for payload in (json.dumps(accepted, indent=2).encode(), OPERATOR.canonical(accepted) + b"\n",
                        b'{"owner":"first","owner":"second"}', b'{"nested":{"key":1,"key":1}}',
                        b'{"z":1,"a":2}', b'[]', b'null', b'"text"', b'{invalid', b'\xff'):
            with self.subTest(payload=payload):
                with self.assertRaises(RuntimeError):
                    OPERATOR._recovery_document(path)

    def test_deployment_lock_is_exclusive_nofollow_durable_and_released_without_unlink(self) -> None:
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", False)
        exists, busy, replaced, short_write = False, False, False, False
        contents = bytearray()
        events = []
        self.replace(OPERATOR, "directory", lambda current, mode: self.assertEqual((current, mode), (OPERATOR.SCRATCH, 0o700)))

        def info(opened=False):
            return types.SimpleNamespace(st_dev=1, st_ino=88 if opened and replaced else 7,
                                         st_mode=stat.S_IFREG | 0o600, st_uid=0, st_gid=0, st_size=len(contents))

        def open_descriptor(current, flags, *mode):
            nonlocal exists
            self.assertEqual(current, OPERATOR.DEPLOYMENT_LOCK)
            if flags & os.O_CREAT:
                self.assertEqual((flags, mode), (os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, (0o600,)))
                if exists:
                    raise FileExistsError("Synthetic stable lock file exists")
                exists = True
            else:
                self.assertEqual((flags, mode), (os.O_RDWR | os.O_NOFOLLOW, ()))
                self.assertTrue(exists)
            events.append(("open", 17))
            return 17

        def flock(descriptor, operation):
            self.assertEqual((descriptor, operation), (17, fcntl.LOCK_EX | fcntl.LOCK_NB))
            events.append(("flock", 17))
            if busy:
                raise BlockingIOError("Synthetic competing operator owns the lock")

        def write(descriptor, value):
            self.assertEqual((descriptor, value), (17, OPERATOR.DEPLOYMENT_LOCK_OWNER))
            payload = value[:-1] if short_write else value
            contents.extend(payload)
            events.append(("write", bytes(payload)))
            return len(payload)

        self.replace(os, "open", open_descriptor)
        self.replace(os, "fstat", lambda descriptor: info(True) if descriptor == 17 else self.fail("Unowned lock descriptor"))
        self.replace(OPERATOR, "regular", lambda current, maximum, mode: info()
                     if (current, maximum, mode) == (OPERATOR.DEPLOYMENT_LOCK, len(OPERATOR.DEPLOYMENT_LOCK_OWNER), 0o600)
                     else self.fail("Unexpected lock path validation"))
        self.replace(fcntl, "flock", flock)
        self.replace(os, "write", write)
        self.replace(os, "fsync", lambda descriptor: events.append(("fsync", descriptor)))
        self.replace(OPERATOR, "sync_directory", lambda current: events.append(("sync", current)))
        self.replace(os, "lseek", lambda descriptor, offset, origin: self.assertEqual((descriptor, offset, origin), (17, 0, os.SEEK_SET)))
        self.replace(os, "read", lambda descriptor, size: bytes(contents)
                     if (descriptor, size) == (17, len(OPERATOR.DEPLOYMENT_LOCK_OWNER) + 1) else self.fail("Unexpected lock marker read"))
        self.replace(os, "close", lambda descriptor: events.append(("close", descriptor)))
        with OPERATOR.deployment_lock():
            self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, True)
            self.assertEqual(bytes(contents), OPERATOR.DEPLOYMENT_LOCK_OWNER)
            self.assertIn(("fsync", 17), events)
            self.assertIn(("sync", OPERATOR.SCRATCH), events)
            self.assertNotIn(("close", 17), events)
            with self.assertRaises(RuntimeError):
                with OPERATOR.deployment_lock():
                    self.fail("A deployment lock cannot be reentrant")
            self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, True)
        self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, False)
        self.assertEqual(events[-1], ("close", 17))
        self.assertTrue(exists)
        events.clear()
        with self.assertRaisesRegex(RuntimeError, "Synthetic body failure"):
            with OPERATOR.deployment_lock():
                self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, True)
                raise RuntimeError("Synthetic body failure")
        self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, False)
        self.assertEqual(events, [("open", 17), ("flock", 17), ("close", 17)])
        for failure in ("busy", "replaced-inode", "foreign-owner", "short-write"):
            with self.subTest(failure=failure):
                events.clear()
                busy, replaced, short_write = failure == "busy", failure == "replaced-inode", failure == "short-write"
                exists = failure != "short-write"
                contents = bytearray(b"foreign" if failure == "foreign-owner" else OPERATOR.DEPLOYMENT_LOCK_OWNER if exists else b"")
                with self.assertRaises(RuntimeError):
                    with OPERATOR.deployment_lock():
                        self.fail("An unsafe or competing deployment lock must not yield")
                self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, False)
                self.assertEqual(events[-1], ("close", 17))
                self.assertTrue(exists)

    def test_main_keeps_its_lock_held_until_the_deployment_body_exits(self) -> None:
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", False)
        arguments = types.SimpleNamespace(restore=False)
        events = []
        fail_body = False
        self.replace(signal, "signal", Mock())
        self.replace(os, "umask", Mock())
        self.replace(os, "geteuid", Mock(return_value=0))
        self.replace(OPERATOR, "arguments", Mock(return_value=arguments))
        self.replace(OPERATOR, "directory", Mock())

        @contextlib.contextmanager
        def lock():
            self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, False)
            OPERATOR.DEPLOYMENT_LOCK_HELD = True
            events.append("locked")
            try:
                yield
            finally:
                events.append("released")
                OPERATOR.DEPLOYMENT_LOCK_HELD = False

        def body(received):
            self.assertIs(received, arguments)
            self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, True)
            events.append("body")
            if fail_body:
                raise RuntimeError("Synthetic deployment body failed")

        self.replace(OPERATOR, "deployment_lock", lock)
        self.replace(OPERATOR, "locked_main", body)
        with patch.object(sys, "platform", "linux"), patch.dict(os.environ, {"SSH_CONNECTION": "synthetic-root-ssh"}, clear=True):
            OPERATOR.main()
            self.assertEqual(events, ["locked", "body", "released"])
            self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, False)
            events.clear()
            fail_body = True
            with self.assertRaisesRegex(RuntimeError, "Synthetic deployment body failed"):
                OPERATOR.main()
            self.assertEqual(events, ["locked", "body", "released"])
            self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, False)

    def test_unlocked_deployment_and_restore_refuse_before_loading_dependencies(self) -> None:
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", False)
        self.replace(Path, "exists", Mock(return_value=False))
        self.replace(Path, "is_symlink", Mock(return_value=False))
        candidate = Mock(side_effect=AssertionError("An unlocked deployment cannot load its candidate"))
        backup = Mock(side_effect=AssertionError("An unlocked recovery cannot load its backup"))
        self.replace(OPERATOR, "load_candidate", candidate)
        self.replace(OPERATOR, "load_backup", backup)
        with self.assertRaises(RuntimeError):
            OPERATOR.locked_main(types.SimpleNamespace(restore=False))
        with self.assertRaises(RuntimeError):
            OPERATOR.restore()
        candidate.assert_not_called()
        backup.assert_not_called()

    def test_systemd_properties_preserves_requested_repeated_values(self) -> None:
        runtime_file = str(OPERATOR.RUNTIME) + " (ignore_errors=no)"
        recovery_file = str(OPERATOR.RECOVERY_ENV) + " (ignore_errors=no)"
        raw = ("EnvironmentFiles=" + runtime_file + "\nUser=goby\nEnvironmentFiles=" + recovery_file + "\nStateDirectory=\n").encode()
        self.assertEqual(OPERATOR.systemd_properties(raw, ("EnvironmentFiles", "User", "StateDirectory"),
                                                     repeated=("EnvironmentFiles",)),
                         {"EnvironmentFiles": [runtime_file, recovery_file], "User": "goby", "StateDirectory": ""})
        # Parsing preserves the received order; the protected-state contract
        # separately rejects duplicates or an unauthorized order.
        reversed_raw = ("EnvironmentFiles=" + recovery_file + "\nEnvironmentFiles=" + runtime_file + "\n").encode()
        self.assertEqual(OPERATOR.systemd_properties(reversed_raw, ("EnvironmentFiles",), repeated=("EnvironmentFiles",)),
                         {"EnvironmentFiles": [recovery_file, runtime_file]})
        self.assertEqual(OPERATOR.systemd_properties(b"MainPID=123\nActiveState=active\n", ("MainPID", "ActiveState")),
                         {"MainPID": "123", "ActiveState": "active"})

    def test_systemd_properties_rejects_missing_unknown_and_duplicate_singletons(self) -> None:
        accepted = b"User=goby\nStateDirectory=\nEnvironmentFiles=/opt/goby-test/runtime.env (ignore_errors=no)\n"
        variants = (("identical-singleton", accepted + b"User=goby\n"),
                    ("changed-singleton", accepted + b"User=foreign\n"),
                    ("empty-singleton-duplicate", accepted + b"StateDirectory=\n"),
                    ("unknown-property", accepted + b"Group=goby\n"),
                    ("missing-singleton", accepted.replace(b"StateDirectory=\n", b"")),
                    ("missing-repeated-property", b"User=goby\nStateDirectory=\n"),
                    ("malformed-line", accepted + b"unparsed-property\n"),
                    ("invalid-utf8", accepted + b"User=\xff\n"))
        for label, raw in variants:
            with self.subTest(refused=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.systemd_properties(raw, ("User", "StateDirectory", "EnvironmentFiles"),
                                                repeated=("EnvironmentFiles",))

    def test_protected_state_preserves_legacy_evidence_with_real_environment_file_lines(self) -> None:
        _state, enable_recovery, installed, runtime_file, _recovery_file = self.protected_service_fixture()
        baseline = OPERATOR.protected_state()
        self.assertEqual(OPERATOR.protected_state(allow_recovery=True), baseline)
        installed.assert_not_called()
        enable_recovery()
        recovered_configuration = OPERATOR.protected_state(allow_recovery=True)
        self.assertEqual(recovered_configuration, baseline)
        self.assertEqual(recovered_configuration["service"]["EnvironmentFiles"], runtime_file)
        self.assertIsInstance(recovered_configuration["service"]["EnvironmentFiles"], str)
        installed.assert_called_once_with()
        with self.assertRaises(RuntimeError):
            OPERATOR.protected_state()

    def test_protected_state_refuses_missing_reordered_duplicate_and_extra_environment_files(self) -> None:
        state, enable_recovery, _installed, runtime_file, recovery_file = self.protected_service_fixture()
        enable_recovery()
        variants = (("missing-runtime-first-line", [recovery_file]),
                    ("missing-recovery-line", [runtime_file]),
                    ("reversed-files", [recovery_file, runtime_file]),
                    ("duplicate-runtime", [runtime_file, runtime_file, recovery_file]),
                    ("duplicate-recovery", [runtime_file, recovery_file, recovery_file]),
                    ("extra-environment", [runtime_file, recovery_file, "/opt/goby-test/foreign.env (ignore_errors=no)"]),
                    ("combined-single-line", [runtime_file + " " + recovery_file]),
                    ("runtime-ignore-errors", [runtime_file.replace("ignore_errors=no", "ignore_errors=yes"), recovery_file]),
                    ("recovery-ignore-errors", [runtime_file, recovery_file.replace("ignore_errors=no", "ignore_errors=yes")]))
        for label, files in variants:
            with self.subTest(refused=label):
                state["environment_files"] = files
                with self.assertRaises(RuntimeError):
                    OPERATOR.protected_state(allow_recovery=True)
        enable_recovery()
        original_dropins = " ".join(sorted({str(OPERATOR.DROPIN), str(OPERATOR.OBSERVABILITY_DROPIN)}))
        original_writes = " ".join(sorted({"/dev/shm/goby-transcodes-test", str(OPERATOR.VAULT)}))
        for label, property_name, value in (("missing-loaded-recovery-dropin", "DropInPaths", original_dropins),
                                              ("missing-recovery-write-paths", "ReadWritePaths", original_writes)):
            with self.subTest(refused=label):
                enable_recovery()
                state["values"][property_name] = value
                with self.assertRaises(RuntimeError):
                    OPERATOR.protected_state(allow_recovery=True)

    def test_protected_state_refuses_duplicate_singletons_and_incomplete_requested_properties(self) -> None:
        state, enable_recovery, _installed, _runtime_file, _recovery_file = self.protected_service_fixture()
        enable_recovery()
        for label, extra_lines, omitted in (("identical-user", ["User=goby"], set()),
                                             ("changed-user", ["User=foreign"], set()),
                                             ("duplicate-empty-state-directory", ["StateDirectory="], set()),
                                             ("unknown-property", ["UnrequestedSetting=yes"], set()),
                                             ("missing-group", [], {"Group"}),
                                             ("missing-empty-state-directory", [], {"StateDirectory"})):
            with self.subTest(refused=label):
                state["extra_lines"] = extra_lines
                state["omit"] = omitted
                with self.assertRaises(RuntimeError):
                    OPERATOR.protected_state(allow_recovery=True)

    def test_finalize_refuses_unpublished_scope_drift(self) -> None:
        original_baseline = OPERATOR.finalization_baseline
        arguments, state, _mocks, forbidden, _database, candidate, backup, _baseline, assets, _started = self.finalization_fixture()
        for path in (OPERATOR.EVIDENCE, OPERATOR.RECOVERY_EVIDENCE):
            for kind in ("existing", "symlinks"):
                with self.subTest(barrier=str(path), kind=kind):
                    state[kind].add(path)
                    with self.assertRaises(RuntimeError):
                        OPERATOR.finalize_installation(arguments)
                    self.assertEqual(state["events"], [])
                    state[kind].clear()
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", False)
        with self.assertRaises(RuntimeError):
            OPERATOR.finalize_installation(arguments)
        OPERATOR.DEPLOYMENT_LOCK_HELD = True
        for name, value in (("candidate_manifest", OPERATOR.SCRATCH / "different-candidate.json"),
                            ("manifest_sha256", "f" * 64)):
            with self.subTest(argument=name):
                altered = copy.copy(arguments)
                setattr(altered, name, value)
                with self.assertRaises(RuntimeError):
                    OPERATOR.finalize_installation(altered)
                self.assertEqual(state["events"], [])
        for name, value in (("candidate_manifest_sha256", "f" * 64), ("candidate_sha256", "f" * 64),
                            ("operator.py", "f" * 64)):
            with self.subTest(original_proof=name):
                altered = copy.deepcopy(backup)
                (altered["files"] if name == "operator.py" else altered)[name] = value
                with self.assertRaises(RuntimeError):
                    original_baseline(altered, candidate, OPERATOR.canonical(candidate), assets)
        variants = (("disk-digest", "disk_sha", "f" * 64),
                    ("disk-size", "disk_size", OPERATOR.FINALIZATION_BINARY_BYTES + 1),
                    ("process-pid", "process", {**state["process"], "main_pid": OPERATOR.FINALIZATION_PID + 1}),
                    ("process-start", "process", {**state["process"], "start_ticks": "foreign-start"}),
                    ("process-uid", "process", {**state["process"], "uid": 0}),
                    ("process-gid", "process", {**state["process"], "gid": 0}))
        for label, key, value in variants:
            with self.subTest(refused=label):
                saved = state[key]
                state[key] = value
                state["events"].clear()
                state["counts"].clear()
                with self.assertRaises(RuntimeError):
                    OPERATOR.finalize_installation(arguments)
                self.assertEqual(state["publications"], [])
                self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, False)
                state[key] = saved
        for dependency in forbidden.values():
            dependency.assert_not_called()

    def test_finalize_publishes_only_after_final_readonly_revalidation(self) -> None:
        arguments, state, mocks, forbidden, database, candidate, backup, baseline, assets, started = self.finalization_fixture()
        report = OPERATOR.finalize_installation(arguments)
        self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, False)
        self.assertEqual(state["publications"], [(OPERATOR.EVIDENCE, OPERATOR.canonical(report))])
        self.assertEqual(state["events"][-2:], ["process", "publish"])
        self.assertEqual(report["deployment_script_sha256"], OPERATOR.FINALIZATION_ORIGINAL_OPERATOR_SHA)
        self.assertEqual(report["original_operator_sha256"], OPERATOR.FINALIZATION_ORIGINAL_OPERATOR_SHA)
        self.assertEqual(report["remediation_operator_sha256"], "c" * 64)
        self.assertEqual(report["forward_finalization"]["binding_time_lower_bound"], started.isoformat())
        self.assertIs(report["forward_finalization"]["database_write_executed"], False)
        self.assertIs(report["forward_finalization"]["service_stop_or_restart_executed"], False)
        for name in ("candidate", "backup", "remediation", "process", "installed-file", "installed-digest"):
            self.assertEqual(state["counts"][name], 2)
        mocks["verify_installed_candidate"].assert_called_once_with(
            database, candidate, assets, backup, baseline["before"], baseline["protected"], baseline["media"],
            baseline["retained"], baseline["diagnostic_before"], arguments.manifest_sha256,
            started=started, expected_process=OPERATOR.expected_finalization_process())
        for failure in (("shared-verification", 1), ("candidate", 2), ("backup", 2), ("remediation", 2),
                        ("source", 1), ("installed-file", 2), ("installed-digest", 2), ("rows", 1),
                        ("protected", 1), ("media", 1), ("recovery", 1), ("recovery-database", 1),
                        ("diagnostics", 1), ("process", 2), ("publish", 1)):
            with self.subTest(failure=failure):
                state["events"].clear()
                state["counts"].clear()
                state["publications"].clear()
                state["fail_at"] = failure
                with self.assertRaisesRegex(RuntimeError, "Synthetic finalization observation failed"):
                    OPERATOR.finalize_installation(arguments)
                self.assertEqual(state["publications"], [])
                self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, False)
        state["fail_at"] = None
        for barrier in (OPERATOR.EVIDENCE, OPERATOR.RECOVERY_EVIDENCE):
            with self.subTest(late_publication=str(barrier)):
                state["events"].clear()
                state["counts"].clear()
                state["publications"].clear()
                state["publish_race"] = barrier
                with self.assertRaises(RuntimeError):
                    OPERATOR.finalize_installation(arguments)
                self.assertNotIn("publish", state["events"])
                self.assertEqual(state["publications"], [])
                self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, False)
        for dependency in forbidden.values():
            dependency.assert_not_called()

    def test_remediation_report_binds_operator_suite_and_required_contracts(self) -> None:
        operator_path = OPERATOR.ROOT / "exec-work-m5j/forward-operator.py"
        suite_path = operator_path.with_name("test-deploy-backup-recovery.py")
        report_path = OPERATOR.ROOT / "exec-work-m5j/remediation-guards.json"
        operator_bytes, suite_bytes = b"synthetic forward operator\n", b"synthetic supplemental guard suite\n"
        contracts = sorted(name for name in dir(type(self)) if name.startswith("test_"))
        accepted = {"suite": "m5j-deployment-contracts", "status": "passed", "testsRun": len(contracts),
                    "failures": 0, "errors": 0, "skipped": 0, "fixtures": "synthetic-memory-only",
                    "realDatabaseRestoreAcceptance": False, "realDeploymentAcceptance": False,
                    "operatorSha256": hashlib.sha256(operator_bytes).hexdigest(),
                    "suiteSha256": hashlib.sha256(suite_bytes).hexdigest(), "contractTestNames": contracts}
        payloads = {operator_path: operator_bytes, suite_path: suite_bytes,
                    report_path: json.dumps(accepted, sort_keys=True).encode() + b"\n"}
        mode = 0o600
        self.replace(OPERATOR, "__file__", str(operator_path))
        self.replace(OPERATOR, "regular", lambda path, *_args, **_kwargs:
                     types.SimpleNamespace(st_mode=stat.S_IFREG | mode) if path in payloads else self.fail("Unowned remediation metadata"))
        self.replace(Path, "resolve", lambda path, **_kwargs: path)
        self.replace(Path, "read_bytes", lambda path: payloads[path])
        self.replace(OPERATOR, "sha", lambda path: hashlib.sha256(payloads[path]).hexdigest())
        expected_sha = hashlib.sha256(payloads[report_path]).hexdigest()
        expected = {"path": str(report_path), "sha256": expected_sha, "operator_sha256": accepted["operatorSha256"],
                    "suite_path": str(suite_path), "suite_sha256": accepted["suiteSha256"],
                    "tests": len(contracts), "contract_tests": contracts}
        self.assertEqual(OPERATOR.load_remediation_evidence(report_path, expected_sha), expected)
        variants = []
        for key, value in (("operatorSha256", OPERATOR.FINALIZATION_ORIGINAL_OPERATOR_SHA), ("suiteSha256", "f" * 64),
                           ("status", "failed"), ("failures", 1), ("errors", 1), ("skipped", 1),
                           ("realDatabaseRestoreAcceptance", True), ("realDeploymentAcceptance", True),
                           ("fixtures", "external-effects"), ("testsRun", len(contracts) - 1), ("testsRun", True),
                           ("contractTestNames", list(reversed(contracts))),
                           ("contractTestNames", sorted([*contracts, contracts[0]]))):
            altered = copy.deepcopy(accepted)
            altered[key] = value
            variants.append((key + "-" + str(value)[:40], altered))
        missing = copy.deepcopy(accepted)
        missing["contractTestNames"].remove("test_finalize_publishes_only_after_final_readonly_revalidation")
        missing["testsRun"] = len(missing["contractTestNames"])
        variants.append(("missing-required-contract", missing))
        for label, value in variants:
            with self.subTest(refused=label):
                payloads[report_path] = json.dumps(value, sort_keys=True).encode() + b"\n"
                with self.assertRaises(RuntimeError):
                    OPERATOR.load_remediation_evidence(report_path, hashlib.sha256(payloads[report_path]).hexdigest())
        payloads[report_path] = json.dumps(accepted, sort_keys=True).encode() + b"\n"
        for path, digest in ((report_path, "f" * 64), (report_path, "invalid-sha"),
                             (Path("/tmp/unowned-remediation.json"), expected_sha)):
            with self.subTest(path=str(path), digest=digest):
                with self.assertRaises(RuntimeError):
                    OPERATOR.load_remediation_evidence(path, digest)
        mode = 0o666
        with self.assertRaises(RuntimeError):
            OPERATOR.load_remediation_evidence(report_path, expected_sha)

    def test_finalization_start_time_requires_the_pinned_process_and_valid_utc_timestamp(self) -> None:
        values = {"MainPID": str(OPERATOR.FINALIZATION_PID), "ExecMainStartTimestamp": "Fri 2026-09-11 00:17:59 UTC"}
        command = Mock(side_effect=lambda *_args, **_kwargs:
                       ("\n".join(name + "=" + value for name, value in values.items()) + "\n").encode())
        self.replace(OPERATOR, "command", command)
        self.assertEqual(OPERATOR.finalization_start_time(), datetime(2026, 9, 11, 0, 17, 59, tzinfo=timezone.utc))
        command.assert_called_once_with(
            ["/usr/bin/systemctl", "show", OPERATOR.SERVICE, "-p", "MainPID", "-p", "ExecMainStartTimestamp"],
            "Observe the pinned installation service start time", environment={"PATH": "/usr/bin:/bin", "LANG": "C", "TZ": "UTC"})
        values["ExecMainStartTimestamp"] = "Fri 2026-09-11 00:17:59.123456 UTC"
        self.assertEqual(OPERATOR.finalization_start_time(), datetime(2026, 9, 11, 0, 17, 59, 123456, tzinfo=timezone.utc))
        for name, value in (("MainPID", str(OPERATOR.FINALIZATION_PID + 1)), ("ExecMainStartTimestamp", "n/a"),
                            ("ExecMainStartTimestamp", "Fri 2026-09-11 00:17:59 CST"),
                            ("ExecMainStartTimestamp", "Fri 2026-02-30 00:17:59 UTC"),
                            ("ExecMainStartTimestamp", "2026-09-11T00:17:59Z")):
            with self.subTest(refused=value):
                saved = values[name]
                values[name] = value
                with self.assertRaises(RuntimeError):
                    OPERATOR.finalization_start_time()
                values[name] = saved

    def test_forward_read_only_refuses_mutating_commands_and_discards_failure_diagnostics(self) -> None:
        self.replace(OPERATOR, "FINALIZATION_READ_ONLY", False)
        run = Mock(return_value=types.SimpleNamespace(returncode=0, stdout=b"synthetic read-only output", stderr=b""))
        private = Mock(side_effect=AssertionError("Read-only command errors must not write diagnostic files"))
        self.replace(subprocess, "run", run)
        self.replace(OPERATOR, "private", private)
        read_only = {"PGOPTIONS": "-c default_transaction_read_only=on -c statement_timeout=15000"}
        with OPERATOR.forward_read_only():
            self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, True)
            for arguments, environment in ((["/usr/bin/systemctl", "show", OPERATOR.SERVICE, "-p", "MainPID"], None),
                                            (["/usr/bin/psql", "-X"], read_only),
                                            (OPERATOR.peer_argv("postgres"), read_only)):
                run.reset_mock()
                self.assertEqual(OPERATOR.command(arguments, "Synthetic read-only observation", environment=environment, data=b"SELECT 1;"),
                                 b"synthetic read-only output")
                run.assert_called_once()
            for arguments, environment in ((["/usr/bin/systemctl", "stop", OPERATOR.SERVICE], None),
                                            (["/usr/bin/systemctl", "start", OPERATOR.SERVICE], None),
                                            (["/usr/bin/systemctl", "restart", OPERATOR.SERVICE], None),
                                            (["/usr/bin/pg_restore", "--dbname=goby_test"], read_only),
                                            (["/usr/bin/psql", "-X"], None),
                                            (["/usr/bin/psql", "-X"], {"PGOPTIONS": "-c default_transaction_read_only=off"}),
                                            (OPERATOR.peer_argv("postgres"), {"PGOPTIONS": "-c default_transaction_read_only=off"})):
                with self.subTest(refused=arguments, environment=environment):
                    run.reset_mock()
                    with self.assertRaises(RuntimeError):
                        OPERATOR.command(arguments, "Synthetic forbidden mutation", environment=environment)
                    run.assert_not_called()
            with self.assertRaises(RuntimeError):
                with OPERATOR.forward_read_only():
                    self.fail("Read-only finalization must not be reentrant")
            self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, True)
            run.return_value = types.SimpleNamespace(returncode=1, stdout=b"", stderr=b"synthetic private error")
            with self.assertRaises(RuntimeError):
                OPERATOR.command(["/usr/bin/psql", "-X"], "Synthetic failed read", environment=read_only, data=b"SELECT 1;")
            private.assert_not_called()
        self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, False)
        with self.assertRaisesRegex(RuntimeError, "Synthetic observation interruption"):
            with OPERATOR.forward_read_only():
                raise RuntimeError("Synthetic observation interruption")
        self.assertIs(OPERATOR.FINALIZATION_READ_ONLY, False)

    def test_finalize_failure_never_enters_install_or_restore(self) -> None:
        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", True)
        arguments = types.SimpleNamespace(restore=False, finalize=True,
                                          candidate_manifest=OPERATOR.SCRATCH / "m5j-candidate.json",
                                          manifest_sha256=OPERATOR.FINALIZATION_CANDIDATE_SHA,
                                          remediation_report=OPERATOR.ROOT / "exec-work-m5j/remediation-guards.json",
                                          remediation_sha256="e" * 64)
        forbidden = {}
        for name in ("restore", "load_candidate", "create_backup", "command", "replace_file", "publish_exclusive"):
            forbidden[name] = Mock(side_effect=AssertionError("Finalization cannot enter installation or recovery: " + name))
            self.replace(OPERATOR, name, forbidden[name])
        finalizer = Mock()
        self.replace(OPERATOR, "finalize_installation", finalizer)
        for error in (RuntimeError("Synthetic forward-only finalization refusal"), OPERATOR.Interrupted()):
            with self.subTest(error=type(error).__name__):
                finalizer.reset_mock()
                finalizer.side_effect = error
                with self.assertRaises(type(error)):
                    OPERATOR.locked_main(arguments)
                finalizer.assert_called_once_with(arguments)
                for dependency in forbidden.values():
                    dependency.assert_not_called()

    def test_service_identity_requires_all_four_uid_and_gid_values_and_the_expected_binary(self) -> None:
        properties = {"MainPID": str(OPERATOR.OLD_PID), "User": "goby", "ActiveState": "active"}
        status = "Uid:\t995\t995\t995\t995\nGid:\t986\t986\t986\t986\n"
        process = Path("/proc") / str(OPERATOR.OLD_PID)
        process_stat = str(OPERATOR.OLD_PID) + " (synthetic application name) " + " ".join(["0"] * 19 + [OPERATOR.OLD_START])
        digest = OPERATOR.OLD
        self.replace(OPERATOR, "command", lambda *_args, **_options: "\n".join(name + "=" + value for name, value in properties.items()).encode())
        self.replace(Path, "read_text", lambda current: status if current == process / "status" else process_stat
                     if current == process / "stat" else self.fail("Unexpected process identity read"))
        self.replace(OPERATOR, "sha", lambda current: digest if current == process / "exe" else self.fail("Unexpected executable hash"))
        self.assertEqual(OPERATOR.process_identity(OPERATOR.OLD), {"main_pid": OPERATOR.OLD_PID, "uid": 995, "gid": 986,
                         "start_ticks": OPERATOR.OLD_START, "binary_sha256": OPERATOR.OLD})
        accepted = status
        for field, wrong in (("Uid", 0), ("Gid", 995)):
            for position in range(4):
                uid, gid = [995] * 4, [986] * 4
                (uid if field == "Uid" else gid)[position] = wrong
                status = "Uid: " + " ".join(map(str, uid)) + "\nGid: " + " ".join(map(str, gid))
                with self.subTest(field=field, position=position):
                    with self.assertRaises(RuntimeError):
                        OPERATOR.process_identity(OPERATOR.OLD)
        status, digest = accepted, "f" * 64
        with self.assertRaises(RuntimeError):
            OPERATOR.process_identity(OPERATOR.OLD)
        digest = OPERATOR.OLD
        for field, value in (("User", "root"), ("ActiveState", "inactive"), ("MainPID", "1")):
            saved = properties[field]
            properties[field] = value
            with self.subTest(property=field):
                with self.assertRaises(RuntimeError):
                    OPERATOR.process_identity(OPERATOR.OLD)
            properties[field] = saved

    def test_original_reference_identity_is_read_only_and_refuses_pid_reuse(self) -> None:
        observed = {"MainPID": str(OPERATOR.REFERENCE_PID), "ActiveState": "active"}
        command = Mock(side_effect=lambda *_arguments, **_options:
                       "\n".join(name + "=" + value for name, value in observed.items()).encode())
        self.replace(OPERATOR, "command", command)
        process = Path("/proc") / str(OPERATOR.REFERENCE_PID)
        process_stat = str(OPERATOR.REFERENCE_PID) + " (synthetic application name) " + " ".join(["0"] * 19 + [OPERATOR.REFERENCE_START])
        reads = Mock(side_effect=lambda current: process_stat if current == process / "stat" else self.fail("Unexpected process read"))
        digest = Mock(side_effect=lambda current: "f" * 64 if current == process / "exe" else self.fail("Unexpected process hash"))
        self.replace(Path, "read_text", lambda current: reads(current))
        self.replace(OPERATOR, "sha", digest)
        self.assertEqual(OPERATOR.reference_state(), {"pid": OPERATOR.REFERENCE_PID,
            "start_ticks": OPERATOR.REFERENCE_START, "binary_sha256": "f" * 64})
        command.assert_called_once_with(["/usr/bin/systemctl", "show", OPERATOR.REFERENCE_SERVICE,
            "-p", "MainPID", "-p", "ActiveState"], "Original reference process observation")
        for field, value in (("MainPID", "3131778"), ("ActiveState", "inactive")):
            with self.subTest(field=field):
                previous = observed[field]
                observed[field] = value
                reads.reset_mock()
                digest.reset_mock()
                with self.assertRaises(RuntimeError):
                    OPERATOR.reference_state()
                reads.assert_not_called()
                digest.assert_not_called()
                observed[field] = previous
        process_stat = process_stat.rsplit(" ", 1)[0] + " synthetic-reused-start"
        digest.reset_mock()
        with self.assertRaises(RuntimeError):
            OPERATOR.reference_state()
        digest.assert_not_called()

    def test_installation_marker_requires_exact_owner_candidate_and_private_regular_file(self) -> None:
        backup = self.backup()
        regular = Mock()
        self.replace(OPERATOR, "regular", regular)
        self.replace(Path, "exists", lambda current: False)
        self.replace(Path, "is_symlink", lambda current: False)
        self.assertIs(OPERATOR.installation_proven(backup), False)
        regular.assert_not_called()
        value = {"owner": OPERATOR.OWNER, "candidate_sha256": backup["candidate_sha256"]}
        self.replace(Path, "exists", lambda current: current == OPERATOR.INSTALLATION_STARTED)
        self.replace(Path, "read_bytes", Mock(return_value=OPERATOR.canonical(value)))
        self.assertIs(OPERATOR.installation_proven(backup), True)
        regular.assert_called_once_with(OPERATOR.INSTALLATION_STARTED, 1024, 0o600)
        for altered in ({**value, "owner": "unowned"}, {**value, "candidate_sha256": "f" * 64}, {**value, "unrelated": True}):
            with self.subTest(marker=altered), patch.object(Path, "read_bytes", Mock(return_value=OPERATOR.canonical(altered))):
                with self.assertRaises(RuntimeError):
                    OPERATOR.installation_proven(backup)
        for malformed in (b"not-json", b"\xff"):
            with self.subTest(marker=repr(malformed)), patch.object(Path, "read_bytes", Mock(return_value=malformed)):
                self.assertIs(OPERATOR.installation_proven(backup), False)
        with patch.object(OPERATOR, "regular", Mock(side_effect=OPERATOR.Failure("Synthetic unsafe marker file"))):
            with self.assertRaisesRegex(RuntimeError, "Synthetic unsafe marker file"):
                OPERATOR.installation_proven(backup)

    def test_recovery_requires_every_backup_artifact_and_exact_restore_proof_states(self) -> None:
        accepted = self.backup()
        self.assertEqual(OPERATOR.validate_backup_document(accepted), accepted)
        variants = []
        for name in accepted["files"]:
            incomplete = copy.deepcopy(accepted)
            del incomplete["files"][name]
            variants.append(("missing-" + name, incomplete))
        for field, value in (("phase", "preinstall"), ("owner", "unowned-backup"),
                             ("restore_sql_generated", False), ("isolated_restore_verified", False),
                             ("isolated_restore_verified", 1), ("live_restore_verified", True),
                             ("old_binary_sha256", "c" * 64), ("candidate_manifest_sha256", "e" * 64)):
            altered = copy.deepcopy(accepted)
            altered[field] = value
            variants.append((field + "-" + str(value), altered))
        for name, digest in (("goby-m5i", "c" * 64), ("database-schema22.dump", "invalid"), ("../outside", "a" * 64)):
            altered = copy.deepcopy(accepted)
            altered["files"][name] = digest
            variants.append(("invalid-file-" + name, altered))
        for label, document in variants:
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.validate_backup_document(document)

    def test_backup_loader_checks_owned_private_files_and_every_recorded_digest(self) -> None:
        accepted = self.backup()
        regular = Mock()
        directory = Mock()
        digests = {OPERATOR.BACKUP / name: digest for name, digest in accepted["files"].items()}
        marker = OPERATOR.BACKUP / "OWNER.txt"
        manifest = OPERATOR.BACKUP / "backup-manifest.json"
        self.replace(OPERATOR, "regular", regular)
        self.replace(OPERATOR, "directory", directory)
        self.replace(OPERATOR, "sha", lambda path: digests[path])
        self.replace(Path, "read_text", lambda path: OPERATOR.OWNER if path == marker else self.fail("Unexpected backup text read"))
        self.replace(Path, "read_bytes", lambda path: OPERATOR.canonical(accepted)
                     if path == manifest else self.fail("Unexpected backup byte read"))
        self.assertEqual(OPERATOR.load_backup(), accepted)
        directory.assert_called_once_with(OPERATOR.BACKUP, 0o700)
        regular.assert_any_call(marker, 256, 0o600)
        regular.assert_any_call(manifest, 2 * OPERATOR.MIB, 0o600)
        for name in accepted["files"]:
            path = OPERATOR.BACKUP / name
            regular.assert_any_call(path, 256 * OPERATOR.MIB, 0o600)
            with self.subTest(corrupt_file=name):
                saved = digests[path]
                digests[path] = "f" * 64
                with self.assertRaises(RuntimeError):
                    OPERATOR.load_backup()
                digests[path] = saved
        with patch.object(Path, "read_text", Mock(return_value="unowned")):
            with self.assertRaises(RuntimeError):
                OPERATOR.load_backup()

    def test_restore_commands_generate_sql_then_apply_one_transaction(self) -> None:
        self.assertEqual(OPERATOR.UPGRADE_DROP_SQL, b"")
        dump = Path("/synthetic/backup/database.dump")
        toc = Path("/synthetic/backup/restore.list")
        sql = Path("/synthetic/backup/restore.sql")
        generation = OPERATOR.restore_sql_command(dump, toc)
        self.assertIn("--file=-", generation)
        self.assertTrue("--use-list=" + str(toc) in generation or
                        any(value == "--use-list" and index + 1 < len(generation) and generation[index + 1] == str(toc)
                            for index, value in enumerate(generation)))
        self.assertIn(str(dump), generation)
        self.assertFalse(any(value in {"-d", "--dbname"} or value.startswith("--dbname=")
                             for value in generation))
        application = OPERATOR.restore_database_command(sql)
        self.assertIn("--single-transaction", application)
        self.assertTrue("--file=" + str(sql) in application or
                        any(value in {"--file", "-f"} and index + 1 < len(application) and application[index + 1] == str(sql)
                            for index, value in enumerate(application)))

    def test_public_schema_toc_entries_are_excluded_but_old_table_entries_remain(self) -> None:
        raw = (b"; Synthetic archive list\n"
               b"1; 2615 2200 SCHEMA - public postgres\n"
               b"2; 0 0 COMMENT - SCHEMA public postgres\n"
               b"3; 0 0 ACL - SCHEMA public postgres\n"
               b"4; 1259 16403 TABLE public sessions postgres\n"
               b"5; 0 16403 TABLE DATA public sessions postgres\n"
               b"6; 2606 16410 CONSTRAINT public sessions sessions_pkey postgres\n"
               b"7; 1259 16503 TABLE public task_runs postgres\n"
               b"8; 0 16503 TABLE DATA public task_runs postgres\n"
               b"9; 1259 16603 TABLE public activity_entries postgres\n"
               b"10; 0 16603 TABLE DATA public activity_entries postgres\n")
        filtered = OPERATOR.filtered_restore_toc(raw)
        self.assertIsInstance(filtered, bytes)
        active = [line for line in filtered.splitlines() if line and not line.startswith(b";")]
        self.assertEqual(active, raw.splitlines()[4:])
        for invalid in (b"; Empty archive\n", b"x" * (2 * OPERATOR.MIB + 1)):
            with self.subTest(invalid_size=len(invalid)):
                with self.assertRaises(RuntimeError):
                    OPERATOR.filtered_restore_toc(invalid)

    def test_snapshot_reads_every_public_row_without_projection_and_binds_exported_snapshot(self) -> None:
        rows = self.historical_rows()
        rows["items"] = [self.row({"id": "media-" + str(index), "media": {"ProbeVersion": 6}})
                         for index in range(11)]
        rows["items"].extend(self.row({"id": "container-" + str(index), "media": None}) for index in range(10))
        rows["libraries"] = [self.row({"id": "library-" + str(index)}) for index in range(5)]
        rows["activity_entries"].append(self.row({"id": 2, "action": "settings.updated", "unknown_old_field": "preserve me"}))
        rows["activity_entries"].reverse()
        observed = {"inventory": sorted(SCHEMA22_TABLES), "rows": rows}
        database = types.SimpleNamespace(read=Mock(return_value=observed))
        result = OPERATOR.snapshot(database, 22, "00000003-00000004-1")
        self.assertEqual(set(result), SCHEMA22_TABLES)
        self.assertEqual(result["activity_entries"], sorted(rows["activity_entries"]))
        query = database.read.call_args.args[0]
        self.assertIn("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY", query)
        self.assertIn("SET TRANSACTION SNAPSHOT '00000003-00000004-1'", query)
        self.assertIn("to_jsonb(t)::text raw", query)
        for table in SCHEMA22_TABLES:
            self.assertIn('public."' + table + '" t', query)
        self.assertIn("schemaname='public'", query)
        self.assertTrue(query.endswith("COMMIT;"))
        database.read.reset_mock()
        for invalid in ("", "outside", "0-0-1'; DROP TABLE sessions;--"):
            with self.subTest(snapshot=invalid):
                with self.assertRaises(RuntimeError):
                    OPERATOR.snapshot(database, 22, invalid)
                database.read.assert_not_called()

    def test_snapshot_refuses_table_drift_wrong_migrations_and_unbounded_rows(self) -> None:
        rows = self.historical_rows()
        rows["items"] = [self.row({"id": "synthetic-item", "media": None})]
        accepted = {"inventory": sorted(SCHEMA22_TABLES), "rows": rows}
        database = types.SimpleNamespace(read=Mock(return_value=copy.deepcopy(accepted)))
        self.assertEqual(OPERATOR.snapshot(database, 22, check_catalog=False),
                         {table: sorted(values) for table, values in rows.items()})
        variants = []
        for section in ("inventory", "rows"):
            changed = copy.deepcopy(accepted)
            if section == "inventory":
                changed[section].append("unrelated")
            else:
                changed[section]["unrelated"] = []
            variants.append(("extra-" + section, changed))
            missing = copy.deepcopy(accepted)
            if section == "inventory":
                missing[section].remove("activity_entries")
            else:
                del missing[section]["activity_entries"]
            variants.append(("missing-" + section, missing))
        missing_migration = copy.deepcopy(accepted)
        missing_migration["rows"]["schema_migrations"].pop()
        variants.append(("missing-migration", missing_migration))
        duplicate_migration = copy.deepcopy(accepted)
        duplicate_migration["rows"]["schema_migrations"][-1] = duplicate_migration["rows"]["schema_migrations"][0]
        variants.append(("duplicate-migration", duplicate_migration))
        oversized = copy.deepcopy(accepted)
        oversized["rows"]["activity_entries"] = [self.row({"id": 1})] * 10001
        variants.append(("unbounded-rows", oversized))
        for label, observed in variants:
            with self.subTest(case=label), patch.object(database, "read", Mock(return_value=observed)):
                with self.assertRaises(RuntimeError):
                    OPERATOR.snapshot(database, 22, check_catalog=False)

    def test_isolated_rehearsal_dispatches_all_twenty_nine_tables_and_proves_owned_cleanup_in_memory(self) -> None:
        baseline = self.historical_rows()
        name = "goby_m5j_restore_" + "1" * 24
        sql_path = OPERATOR.BACKUP / "restore-schema22.sql"
        payload = b"SET LOCAL ROLE goby_test;\n-- Synthetic recovery SQL stays in memory.\n"
        started = "2042-01-02T03:04:05+00:00"
        database = types.SimpleNamespace(read=Mock(return_value=started))
        identity = {"oid": 123456, "owner": "goby_test"}
        self.replace(secrets, "token_hex", Mock(return_value="1" * 24))
        self.replace(OPERATOR, "private", Mock())
        self.replace(OPERATOR, "sync_directory", Mock())
        self.replace(Path, "read_bytes", lambda current: payload if current == sql_path else self.fail("Unexpected recovery SQL read"))
        commands = []

        def command(arguments: list[str], label: str, **options: object) -> bytes:
            commands.append((list(arguments), options))
            self.assertIn("default_transaction_read_only=off", options["environment"]["PGOPTIONS"])
            self.assertNotIn("PGPASSWORD", options["environment"])
            return b""

        def observed_snapshot(restored_database, version):
            self.assertEqual(restored_database.name, name)
            self.assertEqual(version, 22)
            return copy.deepcopy(baseline)

        self.replace(OPERATOR, "command", command)
        snapshot = Mock(side_effect=observed_snapshot)
        self.replace(OPERATOR, "snapshot", snapshot)
        observation = Mock(side_effect=[{"start": started, "user": "postgres", "exists": False},
                                        identity, identity, started, False])
        self.replace(OPERATOR, "peer_read", observation)
        report = OPERATOR.rehearsal(database, baseline, sql_path)
        self.assertEqual(report["status"], "passed")
        self.assertEqual(report["schema_version"], 22)
        self.assertEqual(report["tables"], OPERATOR.summaries(baseline))
        self.assertEqual(len(report["tables"]), 29)
        self.assertIs(report["isolated_database_import_executed"], True)
        self.assertIs(report["main_database_restore_executed"], False)
        self.assertIs(report["temporary_database_removed"], True)
        self.assertEqual(len(commands), 3)
        self.assertIn("/usr/bin/createdb", commands[0][0])
        self.assertIn("--template=template0", commands[0][0])
        self.assertIn("--owner=goby_test", commands[0][0])
        self.assertEqual(commands[0][0][-1], name)
        self.assertEqual(commands[1][0], OPERATOR.peer_argv(name) + ["--single-transaction", "--file=-"])
        self.assertEqual(commands[1][1]["data"], payload)
        self.assertIn("/usr/bin/dropdb", commands[2][0])
        self.assertEqual(commands[2][0][-1], name)
        database.read.assert_called_once_with("SELECT to_json(pg_postmaster_start_time());")
        snapshot.assert_called_once()
        self.assertEqual(observation.call_count, 5)

        for table in SCHEMA22_TABLES:
            with self.subTest(changed_restored_table=table):
                changed = copy.deepcopy(baseline)
                changed[table] = [self.row({"synthetic_restore_corruption": table})]
                with patch.object(OPERATOR, "snapshot", Mock(return_value=changed)), \
                     patch.object(OPERATOR, "peer_read", Mock(side_effect=[{"start": started, "user": "postgres", "exists": False},
                                                                            identity, identity, started, False])):
                    with self.assertRaises(RuntimeError):
                        OPERATOR.rehearsal(database, baseline, sql_path)

    def test_isolated_rehearsal_refuses_preexisting_names_and_unrelated_database_ownership(self) -> None:
        accepted = "goby_m5j_restore_" + "1" * 24
        self.assertEqual(OPERATOR.RehearsalDatabase(accepted).name, accepted)
        for unsafe in ("goby_test", "postgres", "goby_m5i_restore_" + "1" * 24, accepted + ";DROP DATABASE goby_test"):
            with self.subTest(database=unsafe):
                with self.assertRaises(RuntimeError):
                    OPERATOR.RehearsalDatabase(unsafe)
        database = types.SimpleNamespace(read=Mock(return_value="synthetic-postmaster-start"))
        self.replace(secrets, "token_hex", Mock(return_value="1" * 24))
        commands = Mock(side_effect=AssertionError("An existing or unrelated database cannot authorize creation or import"))
        self.replace(OPERATOR, "command", commands)
        self.replace(OPERATOR, "private", Mock(side_effect=AssertionError("A refused rehearsal cannot claim owned intent")))
        for observed in ({"start": "synthetic-postmaster-start", "user": "postgres", "exists": True},
                         {"start": "different-server", "user": "postgres", "exists": False},
                         {"start": "synthetic-postmaster-start", "user": "goby_test", "exists": False}):
            with self.subTest(observed=observed), patch.object(OPERATOR, "peer_read", Mock(return_value=observed)):
                with self.assertRaises(RuntimeError):
                    OPERATOR.rehearsal(database, self.historical_rows(), OPERATOR.BACKUP / "restore-schema22.sql")
        commands.assert_not_called()

    def test_published_deployment_or_recovery_refuses_restore_before_loading_dependencies(self) -> None:
        load_backup = Mock(side_effect=AssertionError("Published evidence must be checked before the backup"))
        database = Mock(side_effect=AssertionError("Published evidence must be checked before the database"))
        self.replace(OPERATOR, "load_backup", load_backup)
        self.replace(OPERATOR, "Database", database)
        for path in (OPERATOR.EVIDENCE, OPERATOR.RECOVERY_EVIDENCE):
            for kind in ("file", "symlink"):
                with self.subTest(path=str(path), kind=kind):
                    with patch.object(Path, "exists", lambda current: current == path and kind == "file"), \
                         patch.object(Path, "is_symlink", lambda current: current == path and kind == "symlink"):
                        with self.assertRaises(RuntimeError):
                            OPERATOR.restore()
                    load_backup.assert_not_called()
                    database.assert_not_called()

    def test_published_pass_is_never_rolled_back_when_later_terminal_output_fails(self) -> None:
        database, before, after, _managed, earliest, latest = self.upgrade_fixture("Retained custom server")
        database.read = Mock(side_effect=[earliest.isoformat(), latest.isoformat()])
        candidate = self.candidate()
        source_payload, binary_payload = b"synthetic accepted source", b"synthetic accepted executable"
        candidate["source"]["files"] = {name: hashlib.sha256(source_payload).hexdigest() for name in candidate["source"]["files"]}
        candidate["binary"]["sha256"] = hashlib.sha256(binary_payload).hexdigest()
        manifest_bytes = OPERATOR.canonical(candidate)
        manifest_digest = hashlib.sha256(manifest_bytes).hexdigest()
        backup = self.backup()
        backup["candidate_sha256"], backup["candidate_manifest_sha256"] = candidate["binary"]["sha256"], manifest_digest
        backup["files"]["candidate.json"] = manifest_digest
        source, old_sources = Path(candidate["source"]["resolved_path"]), dict(candidate["source"]["files"])
        assets, retained = {"index.html": b"synthetic accepted administrator index"}, {"index.html": "a" * 64}
        protected, media = {"synthetic": "protected"}, {"synthetic": "media"}
        diagnostics = {"directory": str(OPERATOR.LOG_DIRECTORY), "record_count": 2}
        recovery, recovery_database = {"deployment_id": "1" * 32}, {"synthetic": "recovery-database-verified"}
        arguments = types.SimpleNamespace(restore=False, candidate_manifest=OPERATOR.SCRATCH / "dynamic-m5j.json",
                                          manifest_sha256=manifest_digest)
        published, commands = {}, []

        def publication(destination, payload):
            self.assertEqual(destination, OPERATOR.EVIDENCE)
            self.assertIs(OPERATOR.DEPLOYMENT_LOCK_HELD, True)
            self.assertNotIn(destination, published)
            published[destination] = json.loads(payload)

        def command(argv, _label, **_options):
            commands.append(list(argv))
            if argv == ["/usr/bin/systemctl", "show", OPERATOR.SERVICE, "-p", "MainPID", "--value"]:
                return b"0\n"
            self.assertIn(argv, [["/usr/bin/systemctl", "stop", OPERATOR.SERVICE], ["/usr/bin/systemctl", "start", OPERATOR.SERVICE]])
            return b""

        def read(path):
            if path == OPERATOR.BACKUP / "diagnostics-before.json":
                return OPERATOR.canonical(diagnostics)
            if path == Path(candidate["binary"]["path"]):
                return binary_payload
            if path.is_relative_to(source) and path.relative_to(source).as_posix() in candidate["source"]["files"]:
                return source_payload
            self.fail("Published-pass fixture attempted an unexpected file read")

        def process(expected):
            return {"main_pid": OPERATOR.OLD_PID if expected == OPERATOR.OLD else 4000001, "uid": 995, "gid": 986,
                    "start_ticks": OPERATOR.OLD_START if expected == OPERATOR.OLD else "synthetic-new-start", "binary_sha256": expected}

        self.replace(OPERATOR, "DEPLOYMENT_LOCK_HELD", True)
        self.replace(OPERATOR, "load_candidate", Mock(return_value=(candidate, manifest_bytes, assets)))
        self.replace(OPERATOR, "preflight_paths", Mock(return_value=(old_sources, retained)))
        self.replace(OPERATOR, "preflight_recovery", Mock(return_value=OPERATOR.recovery_baseline()))
        self.replace(OPERATOR, "process_identity", process)
        self.replace(OPERATOR, "protected_state", Mock(return_value=protected))
        self.replace(OPERATOR, "Database", Mock(return_value=database))
        self.replace(OPERATOR, "recovery_database_baseline", Mock())
        self.replace(OPERATOR, "provision_recovery_database", Mock())
        verify_database = Mock(return_value=recovery_database)
        self.replace(OPERATOR, "verify_recovery_database", verify_database)
        self.replace(shutil, "disk_usage", Mock(return_value=types.SimpleNamespace(free=10 ** 12)))
        self.replace(Path, "stat", Mock(return_value=types.SimpleNamespace(st_size=1)))
        self.replace(Path, "exists", lambda current: current in published)
        self.replace(Path, "is_symlink", Mock(return_value=False))
        self.replace(Path, "read_bytes", read)
        self.replace(Path, "resolve", lambda current, **_options: current)
        self.replace(OPERATOR, "exported_snapshot", lambda _database: contextlib.nullcontext("AAA-BBB-1"))
        self.replace(OPERATOR, "snapshot", lambda _database, version, *_args, **_options: before if version == 22 else after)
        self.replace(OPERATOR, "quiescent", Mock())
        self.replace(OPERATOR, "media_state", Mock(return_value=media))
        self.replace(OPERATOR, "create_backup", Mock(return_value=backup))
        self.replace(OPERATOR, "load_backup", Mock(return_value=backup))
        self.replace(OPERATOR, "command", command)
        self.replace(OPERATOR, "private", Mock())
        self.replace(OPERATOR, "sync_directory", Mock())
        self.replace(OPERATOR, "target", lambda root, relative: root / relative)
        self.replace(OPERATOR, "parent_for", Mock())
        self.replace(OPERATOR, "replace_file", Mock())
        self.replace(OPERATOR, "ready", Mock())
        install_recovery, verify_recovery = Mock(), Mock(return_value=recovery)
        self.replace(OPERATOR, "install_recovery", install_recovery)
        self.replace(OPERATOR, "verify_recovery", verify_recovery)
        self.replace(OPERATOR, "preserve_diagnostic_history", Mock(return_value=True))
        self.replace(OPERATOR, "diagnostic_snapshot", Mock(return_value=diagnostics))
        self.replace(OPERATOR, "source_inventory", Mock(return_value=candidate["source"]["files"]))
        self.replace(OPERATOR, "check_assets", Mock())
        self.replace(OPERATOR, "sha", Mock(return_value="e" * 64))
        self.replace(OPERATOR, "publish_exclusive", publication)
        rollback = Mock(side_effect=AssertionError("An already published pass must never roll back"))
        self.replace(OPERATOR, "restore", rollback)
        self.replace(builtins, "print", Mock(side_effect=RuntimeError("Synthetic output failure after publication")))
        with self.assertRaisesRegex(RuntimeError, "Synthetic output failure after publication"):
            OPERATOR.locked_main(arguments)
        rollback.assert_not_called()
        self.assertEqual(set(published), {OPERATOR.EVIDENCE})
        report = published[OPERATOR.EVIDENCE]
        self.assertEqual((report["status"], report["schema_version"], report["candidate_manifest_sha256"]), ("passed", 23, manifest_digest))
        self.assertEqual((report["managed_settings_revision_before"], report["managed_settings_revision_after"]), (17, 17))
        self.assertEqual(report["activity_entries_count"], len(before["activity_entries"]))
        self.assertIs(report["activity_history_inferred_or_startup_event_inserted"], False)
        self.assertEqual(report["diagnostics"], diagnostics)
        self.assertEqual(report["recovery"], recovery)
        install_recovery.assert_called_once_with(backup)
        self.assertEqual(verify_recovery.call_args_list, [unittest.mock.call(backup), unittest.mock.call(backup)])
        self.assertEqual(verify_database.call_args_list, [unittest.mock.call(backup, database), unittest.mock.call(backup, database)])
        self.assertEqual(report["isolated_restore_tables_raw_exact"], 29)
        self.assertIs(report["live_database_restore_executed"], False)
        self.assertEqual(commands, [["/usr/bin/systemctl", "stop", OPERATOR.SERVICE],
                                    ["/usr/bin/systemctl", "show", OPERATOR.SERVICE, "-p", "MainPID", "--value"],
                                    ["/usr/bin/systemctl", "start", OPERATOR.SERVICE]])

    def test_evidence_publication_is_atomic_exclusive_and_cleans_its_staging_name(self) -> None:
        destination = Path("/synthetic/backup/evidence.json")
        payload = b'{"status":"passed","fixture":"synthetic"}'
        files: dict[Path, bytes] = {}
        observations: list[tuple] = []
        fail_link = False

        def private(path: Path, value: bytes) -> None:
            self.assertNotEqual(path, destination, "The final name must never receive staged writes")
            self.assertNotIn(path, files)
            files[path] = bytes(value)
            observations.append(("staged", destination in files))

        def link(source: Path, target: Path, **options: object) -> None:
            self.assertEqual(options, {"follow_symlinks": False})
            self.assertEqual(target, destination)
            self.assertIn(source, files)
            if target in files:
                raise FileExistsError("Synthetic evidence already exists")
            if fail_link:
                raise OSError("Synthetic publication failure before the final name appears")
            files[target] = files[source]
            observations.append(("published", files[target]))

        def unlink(path: Path) -> None:
            self.assertNotEqual(path, destination)
            del files[path]

        self.replace(OPERATOR, "private", private)
        self.replace(OPERATOR, "sync_directory", lambda path: observations.append(("synced", path)))
        self.replace(secrets, "token_hex", Mock(side_effect=("1" * 16, "2" * 16, "3" * 16)))
        self.replace(os, "link", link)
        self.replace(Path, "unlink", unlink)
        OPERATOR.publish_exclusive(destination, payload)
        self.assertEqual(files, {destination: payload})
        self.assertEqual(observations[:2], [("staged", False), ("published", payload)])
        with self.assertRaises(FileExistsError):
            OPERATOR.publish_exclusive(destination, b"replacement must not overwrite existing evidence")
        self.assertEqual(files, {destination: payload})
        files.clear()
        fail_link = True
        with self.assertRaises(OSError):
            OPERATOR.publish_exclusive(destination, payload)
        self.assertEqual(files, {})

    def test_upgrade_preserves_all_twenty_nine_old_tables_and_linked_task_history(self) -> None:
        database, before, after, _managed, earliest, latest = self.upgrade_fixture()
        self.assertEqual(OPERATOR.OLD_TABLES, SCHEMA22_TABLES)
        self.assertEqual(OPERATOR.ADDED_TABLES, set())
        self.assertEqual(len(SCHEMA22_TABLES), 29)
        self.assertTrue(all(before[table] for table in HISTORICAL_TASK_TABLES | {"activity_entries"}))
        self.assertIsNotNone(json.loads(before["scan_jobs"][0])["task_child_id"])
        OPERATOR.verify_upgrade(database, before, after, earliest, latest, expected_deployment="1" * 32)
        for table in SCHEMA22_TABLES - {"schema_migrations"}:
            with self.subTest(changed_old_table=table):
                changed = copy.deepcopy(after)
                changed[table] = [self.row({"changed": table})]
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, before, changed, earliest, latest, expected_deployment="1" * 32)
        for origin in ("before", "after"):
            for table in ("activity_entries", "sessions"):
                with self.subTest(missing_inventory=origin + ":" + table):
                    old, new = copy.deepcopy(before), copy.deepcopy(after)
                    del (old if origin == "before" else new)[table]
                    with self.assertRaises(RuntimeError):
                        OPERATOR.verify_upgrade(database, old, new, earliest, latest, expected_deployment="1" * 32)
            with self.subTest(extra_inventory=origin):
                old, new = copy.deepcopy(before), copy.deepcopy(after)
                (old if origin == "before" else new)["unrelated"] = []
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, old, new, earliest, latest, expected_deployment="1" * 32)
        database.read.assert_not_called()

    def test_upgrade_retains_old_name_modes_and_every_managed_field_value_type_and_timestamp(self) -> None:
        for server_name, name_mode in ((None, "deployment"), (None, "unset"), ("", "empty"),
                                       ("Retained custom server", "custom"), ("0", "custom")):
            with self.subTest(server_name=server_name, name_mode=name_mode):
                database, before, after, managed, earliest, latest = self.upgrade_fixture(server_name, name_mode)
                self.assertEqual(set(managed), MANAGED_FIELDS)
                self.assertEqual(after["managed_settings"], before["managed_settings"])
                self.assertEqual((managed["server_name_mode"], managed["compatibility_max_width"], managed["revision"]), (name_mode, 1280, 17))
                OPERATOR.verify_upgrade(database, before, after, earliest, latest, expected_deployment="1" * 32)
                changes = (("id", 2), ("id", True), ("revision", 18), ("revision", 17.0),
                           ("server_name", "Another persisted name"), ("max_bitrate", None),
                           ("max_width", 0), ("max_height", 720), ("max_audio_channels", 2),
                           ("server_name_mode", "invalid"), ("compatibility_max_width", 0),
                           ("compatibility_max_width", 1280.0),
                           ("created_at", "2042-01-02T03:04:05+00:00"), ("updated_at", "2042-01-02T03:04:05+00:00"))
                for field, value in changes:
                    changed = copy.deepcopy(after)
                    changed["managed_settings"] = [self.row({**managed, field: value})]
                    with self.subTest(field=field, value=value):
                        with self.assertRaises(RuntimeError):
                            OPERATOR.verify_upgrade(database, before, changed, earliest, latest, expected_deployment="1" * 32)
                database.read.assert_not_called()

    def test_upgrade_requires_only_migration_twenty_three_and_preserves_old_migration_bytes(self) -> None:
        database, before, after, _managed, earliest, latest = self.upgrade_fixture()
        variants = []
        for migration in ({"version": 23, "name": "0023_unrelated.sql"},
                          {"version": 24, "name": "0023_backup_activity.sql"},
                          {"version": 23.0, "name": "0023_backup_activity.sql"},
                          {"version": True, "name": "0023_backup_activity.sql"}):
            changed = copy.deepcopy(after)
            changed["schema_migrations"][-1] = self.row(migration)
            variants.append(("unexpected-migration", changed))
        for label, migrations in (("missing-new-migration", before["schema_migrations"]),
                                   ("duplicate-new-migration", after["schema_migrations"] + after["schema_migrations"][-1:]),
                                   ("missing-old-migration", after["schema_migrations"][1:]),
                                   ("additional-migration", after["schema_migrations"] + [self.row({"version": 24, "name": "0024_unrelated.sql"})])):
            changed = copy.deepcopy(after)
            changed["schema_migrations"] = migrations
            variants.append((label, changed))
        changed = copy.deepcopy(after)
        original = json.loads(changed["schema_migrations"][0])
        changed["schema_migrations"][0] = self.row({**original, "applied_at": "2042-01-02T03:04:05+00:00"})
        variants.append(("old-migration-timestamp-changed", changed))
        for label, changed in variants:
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, before, changed, earliest, latest, expected_deployment="1" * 32)
        database.read.assert_not_called()

    def test_upgrade_refuses_any_old_activity_mutation_or_new_startup_activity(self) -> None:
        database, before, after, _managed, earliest, latest = self.upgrade_fixture()
        old = json.loads(before["activity_entries"][0])
        variants = []
        for field in old:
            changed = copy.deepcopy(after)
            altered = dict(old)
            del altered[field]
            changed["activity_entries"] = [self.row(altered)]
            variants.append(("missing-old-activity-field-" + field, changed))
        for rows in ([], None, {}, after["activity_entries"] * 2,
                     [self.row({**old, "id": 37.0})], [self.row({**old, "created_at": "2042-01-02T03:04:05+00:00"})],
                     after["activity_entries"] + [self.row({"id": 38, "action": "system.started"})]):
            changed = copy.deepcopy(after)
            changed["activity_entries"] = rows
            variants.append(("changed-activity-membership-or-bytes", changed))
        for label, changed in variants:
            with self.subTest(case=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, before, changed, earliest, latest, expected_deployment="1" * 32)
        before["activity_entries"], after["activity_entries"] = [], []
        OPERATOR.verify_upgrade(database, before, after, earliest, latest, expected_deployment="1" * 32)
        after["activity_entries"] = [self.row({"id": 1, "action": "system.started"})]
        with self.assertRaises(RuntimeError):
            OPERATOR.verify_upgrade(database, before, after, earliest, latest, expected_deployment="1" * 32)
        database.read.assert_not_called()

    def test_initial_binding_is_the_only_new_setting_and_matches_the_local_initial_generation(self) -> None:
        database, before, after, _managed, earliest, latest = self.upgrade_fixture()
        self.assertIs(OPERATOR.verify_initial_binding(before, after, "1" * 32, earliest, latest), True)
        marker_row = json.loads(after["server_settings"][-1])
        marker_value = json.loads(marker_row["value"])
        variants = []
        for field, value in (("key", "unrelated"), ("value", json.dumps(marker_value, indent=2)),
                             ("value", marker_value), ("created_at", "2042-01-02T03:04:04+00:00"),
                             ("updated_at", "2042-01-02T03:04:06+00:00")):
            variants.append((field, {**marker_row, field: value}))
        for field, value in (("deploymentId", "f" * 32), ("generationId", "2" * 32),
                             ("slot", "recovery"), ("version", 2), ("version", True)):
            variants.append(("marker-" + field, {**marker_row, "value": json.dumps({**marker_value, field: value}, separators=(",", ":"))}))
        for timestamp in ("2042-01-02T03:03:59+00:00", "2042-01-02T03:04:11+00:00", "2042-01-02T03:04:05"):
            variants.append(("timestamp", {**marker_row, "created_at": timestamp, "updated_at": timestamp}))
        variants.append(("extra-field", {**marker_row, "unrelated": True}))
        for field in marker_row:
            altered = dict(marker_row)
            del altered[field]
            variants.append(("missing-" + field, altered))
        for label, marker in variants:
            with self.subTest(case=label):
                changed = copy.deepcopy(after)
                changed["server_settings"][-1] = self.row(marker)
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, before, changed, earliest, latest, expected_deployment="1" * 32)
        for expected in (None, "", "A" * 32, "1" * 31, "f" * 32):
            with self.subTest(expected_deployment=expected):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, before, after, earliest, latest, expected_deployment=expected)
        for origin in ("before", "after"):
            changed_before, changed_after = copy.deepcopy(before), copy.deepcopy(after)
            selected = changed_before if origin == "before" else changed_after
            selected["server_settings"].append(after["server_settings"][-1])
            with self.subTest(duplicate_binding=origin):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, changed_before, changed_after, earliest, latest, expected_deployment="1" * 32)
        changed = copy.deepcopy(after)
        old_setting = json.loads(changed["server_settings"][0])
        for field in old_setting:
            changed["server_settings"][0] = self.row({**old_setting, field: "changed-old-field"})
            with self.subTest(old_setting_field=field):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, before, changed, earliest, latest, expected_deployment="1" * 32)
        database.read.assert_not_called()

    def test_unbound_recovery_only_accepts_exact_unchanged_old_settings(self) -> None:
        database, before, after, _managed, earliest, latest = self.upgrade_fixture()
        after["server_settings"] = copy.deepcopy(before["server_settings"])
        with self.assertRaises(RuntimeError):
            OPERATOR.verify_upgrade(database, before, after, earliest, latest, expected_deployment="1" * 32)
        self.assertIs(OPERATOR.verify_initial_binding(before, after, None, allow_unbound=True), False)
        OPERATOR.verify_upgrade(database, before, after, earliest, latest, allow_unbound=True)
        for replacement in ([], after["server_settings"] * 2, [self.row({"key": "server_name", "value": "changed"})]):
            changed = copy.deepcopy(after)
            changed["server_settings"] = replacement
            with self.subTest(settings=replacement):
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_upgrade(database, before, changed, earliest, latest, allow_unbound=True)
        database.read.assert_not_called()

    def test_quiescent_allows_stable_history_but_refuses_restart_mutations_or_new_execution(self) -> None:
        baseline = self.historical_rows()
        database = types.SimpleNamespace(read=Mock(return_value=False))

        def changed_row(rows: dict, table: str, **changes: object) -> dict:
            changed = copy.deepcopy(rows)
            row = json.loads(changed[table][0])
            row.update(changes)
            changed[table][0] = self.row(row)
            return changed

        suspended = changed_row(baseline, "task_triggers", retired_at=None, calculation_error="invalid_schedule", next_fire_at=None)
        disabled = changed_row(baseline, "task_definitions", enabled=False, is_hidden=True, revision=9, schedule_timezone="Asia/Shanghai")
        disabled = changed_row(disabled, "task_triggers", retired_at=None, calculation_error="", next_fire_at=None)
        retired_definition = copy.deepcopy(baseline)
        unknown = json.loads(retired_definition["task_definitions"][0])
        unknown.update({"id": "7" * 32, "key": "retired.task", "enabled": False})
        retired_definition["task_definitions"].append(self.row(unknown))
        for label, rows in (("retired-trigger-history", baseline), ("suspended-trigger", suspended),
                            ("disabled-definition", disabled), ("disabled-unavailable-definition", retired_definition)):
            with self.subTest(allowed=label):
                original = copy.deepcopy(rows)
                OPERATOR.quiescent(database, rows)
                self.assertEqual(rows, original)

        variants = []
        for table, field, states in (("scan_jobs", "status", ("Queued", "Running")),
                                      ("encoding_jobs", "state", ("queued", "running")),
                                      ("task_runs", "state", ("pending", "running", "stopping")),
                                      ("task_run_children", "state", ("waiting", "queued", "running"))):
            for state in states:
                variants.append((table + "-" + state, changed_row(baseline, table, **{field: state})))
        for field in ("emby_key", "name", "description", "category"):
            variants.append(("definition-would-be-reconciled-" + field,
                             changed_row(baseline, "task_definitions", **{field: "Different retained metadata"})))
        unavailable = copy.deepcopy(retired_definition)
        unknown["enabled"] = True
        unavailable["task_definitions"][-1] = self.row(unknown)
        variants.append(("enabled-unavailable-definition", unavailable))
        missing_definition = copy.deepcopy(baseline)
        for table in HISTORICAL_TASK_TABLES:
            missing_definition[table] = []
        variants.append(("definition-would-be-inserted", missing_definition))
        for kind, next_fire_at in (("startup", None), ("interval", "2043-01-02T03:04:05+00:00"), ("daily", None)):
            trigger = changed_row(baseline, "task_triggers", kind=kind, retired_at=None, calculation_error="", next_fire_at=next_fire_at)
            variants.append(("runnable-or-reinitialized-" + kind, trigger))
        for label, rows in variants:
            with self.subTest(refused=label):
                with self.assertRaises(RuntimeError):
                    OPERATOR.quiescent(database, rows)
        database.read.return_value = True
        with self.assertRaises(RuntimeError):
            OPERATOR.quiescent(database, baseline)


def main() -> None:
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "m5j-deployment-contracts", "status": "blocked",
                          "reason": "Authorized root SSH and one operator source path are required",
                          "realDatabaseRestoreAcceptance": False, "realDeploymentAcceptance": False}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    source_bytes, suite_bytes = source.read_bytes(), Path(__file__).read_bytes()
    SOURCE_LINES[str(source)] = source_bytes.decode().splitlines(keepends=True)
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    global OPERATOR
    OPERATOR = types.ModuleType("synthetic_m5j_deployment_operator")
    OPERATOR.__file__ = str(source)
    sys.addaudithook(deny_audited_effect)
    try:
        with Fence():
            exec(compile(source_bytes, str(source), "exec"), OPERATOR.__dict__)
    except BaseException as error:
        print(json.dumps({"suite": "m5j-deployment-contracts", "status": "failed", "testsRun": 0,
                          "reason": "Fenced operator import failed", "errorType": type(error).__name__,
                          "fixtures": "synthetic-memory-only", "realDatabaseRestoreAcceptance": False,
                          "realDeploymentAcceptance": False,
                          "operatorSha256": hashlib.sha256(source_bytes).hexdigest(),
                          "suiteSha256": hashlib.sha256(suite_bytes).hexdigest()}))
        raise SystemExit(1)
    result = unittest.TestResult()
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(DeploymentContractTests)
    contract_names = sorted(test._testMethodName for test in suite)
    suite.run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]}
                 for test, detail in failures[:8]]
    print(json.dumps({"suite": "m5j-deployment-contracts", "status": "passed" if result.wasSuccessful() else "failed",
                      "testsRun": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "skipped": len(result.skipped), "operatorSha256": hashlib.sha256(source_bytes).hexdigest(),
                      "suiteSha256": hashlib.sha256(suite_bytes).hexdigest(), "fixtures": "synthetic-memory-only",
                      "contractTestNames": contract_names,
                      "realDatabaseRestoreAcceptance": False, "realDeploymentAcceptance": False,
                      "failureSummaries": summaries, "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
