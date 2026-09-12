#!/usr/bin/env python3
"""Exercise the mount verifier's pure guards through authorized root SSH."""

from __future__ import annotations

import argparse
import __future__
import builtins
import contextlib
import copy
import hashlib
import importlib.util
import io
import _io
import json
import linecache
import os
from pathlib import Path
import re
import shlex
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
from unittest.mock import Mock, patch
import urllib.parse

sys.dont_write_bytecode = True
OPERATOR = None
SOURCE_LINES = {}
TEST_NAME = "TestRootBindingScanMountNamespaceHelper"
WORK = Path("/opt/goby-test/exec-work-m3e")
GO = Path("/opt/goby-toolchains/go1.27.1/bin/go")
MANIFEST = "c5a5cf6bf0afa973fbd2c087d300dbe6bd3079f92c7237b5a108d76ef8ae6fc3"
FULL_RUN = "20260912_053517_9cb0074fc731"
FULL_INNER_UNIT = "goby-client-backup-20260912-053517-9cb0074fc731.service"
FULL_REPORT = WORK / ("client-backup-run-" + FULL_RUN) / "report.json"
FULL_RECEIPT = FULL_REPORT.parent / "receipt-3fcb8703a01a924c.json"
FAILED_RUN = "20260912_061505_b2b453a716a9"
FAILED_OUTPUT = WORK / ("client-backup-run-" + FAILED_RUN)
PAIR_NAMES = ("goby_backup_m3e_source", "goby_backup_m3e_target")
BEHAVIOR_MARKER = (
    "original_remount_recovered=true replacement_approval_learned=false "
    "approval_row_unchanged=true sibling_anchor_unchanged=true "
    "old_leases_retained=true fresh_mount_witness=true system_reboot_tested=false"
)
CLEANUP_MARKER = (
    "mount_cleanup_completed=true held_descriptors_closed=true "
    "owned_mounts_remaining=0 fixture_removed=true"
)


class EnvironmentFence(dict):
    """Reject all access to the process environment inside a pure guard."""

    def __init__(self, denied):
        super().__init__()
        self.denied = denied

    def reject(self, *_args, **_kwargs):
        self.denied()

    __getitem__ = reject
    __setitem__ = reject
    __delitem__ = reject
    __contains__ = reject
    __iter__ = reject
    __len__ = reject
    get = reject
    copy = reject
    keys = reject
    items = reject
    values = reject
    update = reject
    clear = reject
    pop = reject
    popitem = reject
    setdefault = reject


class EffectFence(contextlib.ExitStack):
    """Fail even if a guard catches an attempted external effect."""

    def __enter__(self):
        super().__enter__()
        import fcntl
        import pwd

        self.violations = []
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       SOURCE_LINES.get(str(filename), [])))

        def denial(label):
            def denied(*_args, **_kwargs):
                self.violations.append(label)
                raise AssertionError("Unexpected external effect: " + label)
            return denied

        original_import = builtins.__import__
        cached_modules = dict(sys.modules)

        def cached_import(name, globals=None, locals=None, fromlist=(), level=0):
            module = cached_modules.get(name)
            if level != 0 or module is None or any(
                item == "*" or item not in vars(module) for item in fromlist or ()
            ):
                denial("uncached import: " + name)()
            return original_import(name, globals, locals, fromlist, level)

        targets = (
            (builtins, ("open",)),
            (io, ("open", "FileIO")),
            (_io, ("open", "open_code", "FileIO")),
            (os, ("open", "close", "fdopen", "fstat", "stat", "lstat", "mkdir", "chmod", "chown",
                  "fchown", "fsync", "replace", "rename", "unlink", "remove", "rmdir", "umask", "kill", "system",
                  "popen", "getenv", "getenvb", "putenv", "unsetenv", "getcwd", "chdir", "getuid", "geteuid",
                  "getgid", "getegid", "getgroups", "getpid", "getppid", "readlink", "scandir", "listdir",
                  "read", "write", "pread", "pwrite", "truncate", "ftruncate", "link", "symlink", "mount", "unshare",
                  "fork", "forkpty", "execv", "execve", "execvp", "execvpe", "execl", "execle", "execlp", "execlpe",
                  "posix_spawn", "posix_spawnp", "spawnv", "spawnve", "spawnvp", "spawnvpe", "_exit")),
            (Path, ("lstat", "stat", "exists", "resolve", "readlink", "read_bytes", "read_text", "write_bytes",
                    "write_text", "mkdir", "iterdir", "rglob", "glob", "unlink", "is_file", "is_dir", "rename",
                    "replace", "touch", "rmdir", "chmod", "lchmod", "symlink_to", "hardlink_to")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo", "gethostname", "gethostbyname", "gethostbyname_ex",
                      "gethostbyaddr", "getnameinfo")),
            (pwd, ("getpwnam", "getpwuid")),
            (fcntl, ("flock", "ioctl")),
            (signal, ("signal", "pidfd_send_signal")),
            (time, ("sleep",)),
        )
        for owner, names in targets:
            for name in names:
                if hasattr(owner, name):
                    self.enter_context(patch.object(owner, name, denial(owner.__name__ + "." + name)))
        self.enter_context(patch.object(os, "environ", EnvironmentFence(denial("os.environ"))))
        if hasattr(os, "environb"):
            self.enter_context(patch.object(os, "environb", EnvironmentFence(denial("os.environb"))))
        self.enter_context(patch.object(builtins, "__import__", cached_import))
        return self

    def __exit__(self, *args):
        super().__exit__(*args)
        if self.violations:
            raise AssertionError("External effects attempted: " + ", ".join(self.violations))


class MountVerifierGuardTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())
        self.captured_stdout = self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.output = WORK / "client-backup-run-20260912_010203_abcdef012345"
        self.host = "mnt:[123]"
        self.source_url = (
            "postgresql://goby_backup_m3e_source:" + "a" * 64 +
            "@127.0.0.1:15432/goby_backup_m3e_source?sslmode=disable"
        )
        self.environment = {
            "PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8",
            "GOTMPDIR": str(self.output / "tmp/mount-fixtures"),
            "TMPDIR": str(self.output / "tmp/mount-fixtures"),
            "GOMAXPROCS": "2", "GOMEMLIMIT": "384MiB",
            "GOBY_ROOT_BINDING_SCAN_MOUNT_HELPER": "1",
            "GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE": self.host,
            "GOBY_TEST_DATABASE_URL": self.source_url,
        }
        self.host_state = {
            "namespace": self.host, "mountinfo_sha256": "a" * 64, "loops_sha256": "b" * 64,
        }
        self.helper_stdout = (
            "=== RUN   " + TEST_NAME + "\n" +
            "    mount_helper_test.go:123: " + BEHAVIOR_MARKER + "\n" +
            "    mount_helper_test.go:124: " + CLEANUP_MARKER + "\n" +
            "--- PASS: " + TEST_NAME + " (0.01s)\nPASS\n"
        ).encode()
        self.full_run = "20260912_010203_abcdef012345"
        self.worker_unit = "goby-client-backup-" + self.full_run.replace("_", "-") + ".service"
        self.full_report = {
            "status": "passed", "mode": "full", "schema": 28,
            "source": str(WORK / "source-attempt-54"), "source_manifest_sha256": MANIFEST,
            "run_id": FULL_RUN,
            "unit": FULL_INNER_UNIT,
            "tests": {"top_level_passes": 123, "failures": 0, "skips": 0},
            "cleanup": {name: True for name in (
                "unit_terminal", "hba_restored_exactly", "preexisting_catalog_unchanged", "receipt_saved",
                PAIR_NAMES[0] + "_removed", PAIR_NAMES[1] + "_removed",
            )},
        }
        self.full_receipt = {key: self.full_report[key] for key in (
            "run_id", "unit", "source", "source_manifest_sha256", "schema", "mode",
        )}
        self.full_receipt.update(
            marker="goby-client-backup-pair-m3e-v1", phase="finished", cleanup_complete=True,
            output=str(FULL_REPORT.parent), output_identity={"device": 11, "inode": 12},
            pairs=[{"name": name, "phase": "removed"} for name in PAIR_NAMES],
        )
        self.full_terminal = {
            "unit": "goby-scan-reconciliation-full-controller-v1.service", "cgroup_empty": True,
            "properties": {
                "InvocationID": "c8641f6597f0411ca832d052b885499d", "MainPID": "0",
                "ExecMainStatus": "0", "Result": "success", "ActiveState": "active", "SubState": "exited",
                "ControlGroup": "",
            },
        }

    def reject(self, action):
        with self.assertRaises(OPERATOR.Failure):
            action()

    def command(self):
        return OPERATOR.command_for_helper(self.output / "tmp/library.test")

    def check_launch(self, command=None, environment=None, output=None, host=None, source_url=None):
        return OPERATOR.require_exact_launch(
            self.command() if command is None else command,
            self.environment if environment is None else environment,
            self.output if output is None else output,
            self.host if host is None else host,
            self.source_url if source_url is None else source_url,
        )

    def test_operator_identity_is_bound_to_source54_and_the_exact_helper(self):
        self.assertEqual(OPERATOR.SOURCE, WORK / "source-attempt-54")
        self.assertEqual(OPERATOR.TEST_NAME, TEST_NAME)
        self.assertEqual(OPERATOR.MANIFEST, MANIFEST)
        self.assertEqual(OPERATOR.FULL_UNIT, "goby-scan-reconciliation-full-controller-v1.service")
        self.assertEqual(OPERATOR.FULL_INVOCATION, "c8641f6597f0411ca832d052b885499d")
        self.assertEqual(OPERATOR.FULL_RUN, FULL_RUN)
        self.assertEqual(OPERATOR.FULL_INNER_UNIT, FULL_INNER_UNIT)
        self.assertEqual(OPERATOR.FULL_REPORT, FULL_REPORT)
        self.assertEqual(OPERATOR.FULL_RECEIPT, FULL_RECEIPT)
        self.assertEqual(OPERATOR.FULL_RECEIPT_SHA256, "6accf5be1fa133d5d9f82a270ead3a0e2e82e1b5e0685f1cfee4d09843a48479")
        self.assertEqual(OPERATOR.FAILED_RUN, FAILED_RUN)
        self.assertEqual(OPERATOR.FAILED_RECEIPT_SHA256, "190c202ca956a52b1914fabe7363ea821bd691f0a6f47869edc8785bc3fe288f")
        self.assertEqual(OPERATOR.BLOCK_CLASS, Path("/sys/class/block"))

    def test_compile_command_builds_only_the_library_race_test_binary(self):
        self.assertEqual(OPERATOR.compile_command(self.output), [
            str(GO), "test", "-c", "-race", "-p=1", "-o",
            str(self.output / "tmp/library.test"), "./internal/library",
        ])

    def test_helper_command_is_the_exact_forked_private_namespace_launch(self):
        self.assertEqual(self.command(), [
            "/usr/bin/unshare", "--mount", "--fork", "--kill-child", "--propagation", "private",
            str(self.output / "tmp/library.test"), "-test.run=^" + TEST_NAME + "$", "-test.v", "-test.timeout=120s",
        ])

    def test_command_constructors_refuse_unowned_output_and_binary_paths(self):
        for output in (Path("relative"), WORK, WORK / "foreign", self.output / "nested", Path("/tmp") / self.output.name):
            with self.subTest(output=str(output)):
                self.reject(lambda: OPERATOR.compile_command(output))
                self.reject(lambda: OPERATOR.command_for_helper(output / "tmp/library.test"))
        for binary in (self.output / "library.test", self.output / "tmp/foreign.test", self.output / "foreign/library.test"):
            with self.subTest(binary=str(binary)):
                self.reject(lambda: OPERATOR.command_for_helper(binary))

    def test_exact_launch_accepts_only_the_reviewed_fresh_source_environment(self):
        self.check_launch()

    def test_exact_launch_rejects_missing_and_extra_environment_keys(self):
        for key in self.environment:
            with self.subTest(missing=key):
                environment = dict(self.environment)
                del environment[key]
                self.reject(lambda: self.check_launch(environment=environment))
        for key in ("HOME", "PATH_SUFFIX", "PGPASSWORD", "LD_PRELOAD", "GOBY_TEST_BACKUP_TARGET_DATABASE_URL"):
            with self.subTest(extra=key):
                self.reject(lambda: self.check_launch(environment=dict(self.environment, **{key: "foreign"})))

    def test_exact_launch_rejects_changed_environment_values(self):
        for key in self.environment:
            with self.subTest(key=key):
                environment = dict(self.environment, **{key: self.environment[key] + "-foreign"})
                self.reject(lambda: self.check_launch(environment=environment))

    def test_exact_launch_rejects_argument_addition_removal_and_replacement(self):
        original = self.command()
        self.assertIsInstance(original, list)
        self.assertTrue(original)
        self.assertTrue(all(type(argument) is str for argument in original))
        for index in range(len(original)):
            with self.subTest(removed=index):
                self.reject(lambda: self.check_launch(command=original[:index] + original[index + 1:]))
            with self.subTest(replaced=index):
                changed = list(original)
                changed[index] += "-foreign"
                self.reject(lambda: self.check_launch(command=changed))
        self.reject(lambda: self.check_launch(command=original + ["-test.run=."]))
        self.reject(lambda: self.check_launch(command=["/bin/sh", "-c", shlex.join(original)]))
        self.reject(lambda: self.check_launch(command=list(reversed(original))))

    def test_exact_launch_rejects_a_different_binary_or_output_binding(self):
        self.reject(lambda: self.check_launch(command=OPERATOR.command_for_helper(self.output / "tmp/foreign.test")))
        self.reject(lambda: self.check_launch(output=self.output.with_name("client-backup-run-20260912_010204_abcdef012345")))

    def test_exact_launch_rejects_foreign_or_malformed_source_credentials(self):
        invalid_urls = (
            self.source_url.replace("goby_backup_m3e_source", "goby_backup_m3e_target"),
            self.source_url.replace("@127.0.0.1", "@localhost"),
            self.source_url.replace("@127.0.0.1", "@192.0.2.1"),
            self.source_url.replace(":15432/", ":5432/"),
            self.source_url.replace("a" * 64, "a" * 63),
            self.source_url.replace("a" * 64, "a" * 65),
            self.source_url.replace("a" * 64, "A" * 64),
            self.source_url.replace("a" * 64, "g" * 64),
            self.source_url.replace("?sslmode=disable", ""),
            self.source_url.replace("?sslmode=disable", "?sslmode=require"),
            self.source_url + "&application_name=foreign",
            self.source_url + "\n",
        )
        for url in invalid_urls:
            with self.subTest(url_shape=url.replace("a" * 64, "[synthetic-password]")):
                environment = dict(self.environment, GOBY_TEST_DATABASE_URL=url)
                self.reject(lambda: self.check_launch(environment=environment, source_url=url))

    def test_exact_launch_rejects_invalid_or_mismatched_host_namespace(self):
        self.reject(lambda: self.check_launch(host="mnt:[124]"))
        for host in ("", "mnt:123", "mnt:[foreign]", "net:[123]", "mnt:[123]\n"):
            with self.subTest(host=host):
                environment = dict(self.environment, GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE=host)
                self.reject(lambda: self.check_launch(environment=environment, host=host))

    def test_helper_output_requires_complete_behavior_and_cleanup_evidence(self):
        result = OPERATOR.inspect_helper_output(self.helper_stdout, b"", 0)
        self.assertEqual(result, {
            "test": TEST_NAME, "real_helper_completed": True, "recovery_marker": True,
            "fixture_cleanup_marker": True, "system_reboot_tested": False,
            "stdout_sha256": hashlib.sha256(self.helper_stdout).hexdigest(),
            "stderr_sha256": hashlib.sha256(b"").hexdigest(),
        })

    def test_helper_output_rejects_wrong_types_invalid_utf8_and_excessive_output(self):
        for stdout in (None, "PASS\n", bytearray(self.helper_stdout), b"\xff", b"x" * ((8 << 20) + 1)):
            with self.subTest(stdout_type=type(stdout).__name__):
                self.reject(lambda: OPERATOR.inspect_helper_output(stdout, b"", 0))
        for exit_code in (False, True, 0.0, "0", None):
            with self.subTest(exit_code=exit_code):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout, b"", exit_code))

    def test_helper_output_rejects_nonzero_exit_or_any_stderr(self):
        for exit_code in (-9, 1, 2):
            with self.subTest(exit_code=exit_code):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout, b"", exit_code))
        for stderr in (b"warning\n", b"\n", b" "):
            with self.subTest(stderr=stderr):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout, stderr, 0))

    def test_helper_output_rejects_missing_or_duplicate_completion_lines(self):
        lines = (
            "=== RUN   " + TEST_NAME,
            "--- PASS: " + TEST_NAME + " (0.01s)",
            "PASS",
        )
        for line in lines:
            encoded = (line + "\n").encode()
            with self.subTest(missing=line):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout.replace(encoded, b""), b"", 0))
            with self.subTest(duplicate=line):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout + encoded, b"", 0))

    def test_helper_output_rejects_missing_duplicate_or_changed_markers(self):
        for marker in (BEHAVIOR_MARKER, CLEANUP_MARKER):
            encoded = marker.encode()
            with self.subTest(missing=marker):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout.replace(encoded, b""), b"", 0))
            with self.subTest(duplicate=marker):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout + encoded + b"\n", b"", 0))
            for item in marker.split():
                key, value = item.split("=", 1)
                changed = key + "=" + ({"true": "false", "false": "true", "0": "1"}[value])
                with self.subTest(changed=key):
                    stdout = self.helper_stdout.replace(item.encode(), changed.encode())
                    self.reject(lambda: OPERATOR.inspect_helper_output(stdout, b"", 0))

    def test_helper_output_rejects_failed_skipped_or_racy_streams(self):
        for unexpected in (b"--- FAIL: other (0.01s)\n", b"FAIL\n", b"--- SKIP: other (0.01s)\n",
                           b"SKIP\n", b"WARNING: DATA RACE\n", b"DATA RACE\n"):
            with self.subTest(unexpected=unexpected):
                self.reject(lambda: OPERATOR.inspect_helper_output(self.helper_stdout + unexpected, b"", 0))

    def test_helper_output_rejects_wrong_test_name_or_pass_substring(self):
        changed = self.helper_stdout.replace(TEST_NAME.encode(), (TEST_NAME + "Foreign").encode())
        self.reject(lambda: OPERATOR.inspect_helper_output(changed, b"", 0))
        changed = self.helper_stdout.replace(b"\nPASS\n", b"\nNOT_A_STANDALONE_PASS\n")
        self.reject(lambda: OPERATOR.inspect_helper_output(changed, b"", 0))

    def test_host_preservation_accepts_exact_equal_valid_evidence(self):
        OPERATOR.require_host_preservation(self.host_state, dict(self.host_state))

    def test_host_preservation_rejects_any_changed_identity_or_digest(self):
        changes = {"namespace": "mnt:[124]", "mountinfo_sha256": "c" * 64, "loops_sha256": "d" * 64}
        for key, value in changes.items():
            with self.subTest(key=key):
                self.reject(lambda: OPERATOR.require_host_preservation(self.host_state, dict(self.host_state, **{key: value})))

    def test_host_preservation_rejects_missing_extra_or_malformed_evidence_even_if_equal(self):
        malformed = []
        for key in self.host_state:
            value = dict(self.host_state)
            del value[key]
            malformed.append(value)
        malformed.append(dict(self.host_state, foreign="unowned"))
        for namespace in ("", "mnt:123", "net:[123]", "mnt:[foreign]", "mnt:[123]\n", 123):
            malformed.append(dict(self.host_state, namespace=namespace))
        for key in ("mountinfo_sha256", "loops_sha256"):
            for digest in ("", "a" * 63, "a" * 65, "A" * 64, "g" * 64, "a" * 64 + "\n", None):
                malformed.append(dict(self.host_state, **{key: digest}))
        for evidence in malformed:
            with self.subTest(evidence=evidence):
                self.reject(lambda: OPERATOR.require_host_preservation(evidence, copy.deepcopy(evidence)))
                self.reject(lambda: OPERATOR.require_host_preservation(self.host_state, evidence))
                self.reject(lambda: OPERATOR.require_host_preservation(evidence, self.host_state))

    def check_full(self, report=None, receipt=None, terminal=None):
        return OPERATOR.require_full_prerequisite(
            self.full_report if report is None else report,
            self.full_receipt if receipt is None else receipt,
            self.full_terminal if terminal is None else terminal,
        )

    def test_full_prerequisite_accepts_exact_completed_and_cleaned_source54(self):
        self.check_full()
        terminal = copy.deepcopy(self.full_terminal)
        terminal["properties"].update(ActiveState="inactive", SubState="dead")
        self.check_full(terminal=terminal)
        terminal["properties"]["ControlGroup"] = "/system.slice/" + self.full_terminal["unit"]
        self.check_full(terminal=terminal)

    def test_full_prerequisite_rejects_another_source_manifest_mode_schema_or_status(self):
        changes = {
            "status": ("running", "failed", "retained"),
            "mode": ("targeted", "catalog"),
            "schema": (27, 29, "28", 28.0),
            "source": (str(WORK / "source-attempt-53"), str(WORK / "source-attempt-55")),
            "source_manifest_sha256": ("a" * 64, ""),
        }
        for key, values in changes.items():
            for value in values:
                with self.subTest(key=key, value=value):
                    report, receipt = dict(self.full_report), dict(self.full_receipt)
                    report[key] = receipt[key] = value
                    self.reject(lambda: self.check_full(report=report, receipt=receipt))

    def test_full_prerequisite_requires_every_cleanup_boundary_to_be_exactly_true(self):
        for key in self.full_report["cleanup"]:
            for value in (False, None, 1, "true"):
                with self.subTest(key=key, value=value):
                    report = copy.deepcopy(self.full_report)
                    report["cleanup"][key] = value
                    self.reject(lambda: self.check_full(report=report))
            with self.subTest(missing=key):
                report = copy.deepcopy(self.full_report)
                del report["cleanup"][key]
                self.reject(lambda: self.check_full(report=report))
        report = copy.deepcopy(self.full_report)
        report["cleanup"]["additional_boundary"] = False
        self.reject(lambda: self.check_full(report=report))
        for cleanup in ({}, [], None):
            with self.subTest(cleanup=cleanup):
                self.reject(lambda: self.check_full(report=dict(self.full_report, cleanup=cleanup)))

    def test_full_prerequisite_requires_typed_nonempty_pass_evidence_without_failures_or_skips(self):
        for key, values in {
            "top_level_passes": (0, -1, True, "1", 1.0, None),
            "failures": (1, -1, False, "0", 0.0, None),
            "skips": (1, -1, False, "0", 0.0, None),
        }.items():
            for value in values:
                with self.subTest(key=key, value=value):
                    report = copy.deepcopy(self.full_report)
                    report["tests"][key] = value
                    self.reject(lambda: self.check_full(report=report))
            with self.subTest(missing=key):
                report = copy.deepcopy(self.full_report)
                del report["tests"][key]
                self.reject(lambda: self.check_full(report=report))
        for tests in ({}, [], None):
            with self.subTest(tests=tests):
                self.reject(lambda: self.check_full(report=dict(self.full_report, tests=tests)))

    def test_full_prerequisite_rejects_incomplete_receipt_or_mismatched_report_binding(self):
        for key, value in {
            "marker": "foreign", "phase": "retained", "cleanup_complete": False,
            "run_id": "20260912_010204_abcdef012345", "unit": "foreign.service",
            "source": str(WORK / "source-attempt-53"), "source_manifest_sha256": "a" * 64,
            "schema": 27, "mode": "targeted",
        }.items():
            with self.subTest(key=key):
                self.reject(lambda: self.check_full(receipt=dict(self.full_receipt, **{key: value})))
        for value in (1, "true", None):
            with self.subTest(cleanup_complete=value):
                self.reject(lambda: self.check_full(receipt=dict(self.full_receipt, cleanup_complete=value)))
        for key in ("marker", "phase", "cleanup_complete", "run_id", "unit", "source", "source_manifest_sha256", "schema", "mode"):
            with self.subTest(missing=key):
                receipt = dict(self.full_receipt)
                del receipt[key]
                self.reject(lambda: self.check_full(receipt=receipt))

    def test_full_prerequisite_rejects_missing_or_malformed_run_and_unrelated_unit_even_when_both_agree(self):
        for run in ("", "foreign", "20260912_010203_ABCDEF012345", "20260912_010203_abcdef012345\n"):
            with self.subTest(run=run):
                report, receipt = dict(self.full_report, run_id=run), dict(self.full_receipt, run_id=run)
                self.reject(lambda: self.check_full(report=report, receipt=receipt))
        for key in ("run_id", "unit"):
            with self.subTest(missing_from_both=key):
                report, receipt = dict(self.full_report), dict(self.full_receipt)
                del report[key]
                del receipt[key]
                self.reject(lambda: self.check_full(report=report, receipt=receipt))
        for unit in ("foreign.service", "goby-client-backup-20260912-010204-abcdef012345.service"):
            with self.subTest(unit=unit):
                self.reject(lambda: self.check_full(report=dict(self.full_report, unit=unit), receipt=dict(self.full_receipt, unit=unit)))

    def test_full_prerequisite_requires_both_removed_pairs_in_the_fixed_order(self):
        original = self.full_receipt["pairs"]
        invalid_pairs = (
            [], original[:1], original[1:], list(reversed(original)), original + [dict(original[0])],
            [{"name": "foreign", "phase": "removed"}, original[1]],
            [dict(original[0], phase="owned"), original[1]],
            [original[0], dict(original[1], phase="database_removed")],
            [None, original[1]], {"source": original[0], "target": original[1]}, None,
        )
        for pairs in invalid_pairs:
            with self.subTest(pairs=pairs):
                self.reject(lambda: self.check_full(receipt=dict(self.full_receipt, pairs=pairs)))

    def test_full_prerequisite_requires_exact_terminal_controller_and_empty_cgroup(self):
        for key, value in {
            "unit": "foreign.service", "cgroup_empty": False,
        }.items():
            with self.subTest(key=key):
                self.reject(lambda: self.check_full(terminal=dict(self.full_terminal, **{key: value})))
        for value in (1, "true", None):
            with self.subTest(cgroup_empty=value):
                self.reject(lambda: self.check_full(terminal=dict(self.full_terminal, cgroup_empty=value)))
        for key, value in {
            "InvocationID": "f" * 32, "MainPID": "123", "ExecMainStatus": "1", "Result": "exit-code",
            "ActiveState": "failed", "SubState": "running", "ControlGroup": "/system.slice/foreign.service",
        }.items():
            with self.subTest(key=key):
                terminal = copy.deepcopy(self.full_terminal)
                terminal["properties"][key] = value
                self.reject(lambda: self.check_full(terminal=terminal))
        for active, sub in (("active", "running"), ("active", "dead"), ("inactive", "exited"), ("failed", "failed")):
            with self.subTest(active=active, sub=sub):
                terminal = copy.deepcopy(self.full_terminal)
                terminal["properties"].update(ActiveState=active, SubState=sub)
                self.reject(lambda: self.check_full(terminal=terminal))
        for key in self.full_terminal["properties"]:
            with self.subTest(missing=key):
                terminal = copy.deepcopy(self.full_terminal)
                del terminal["properties"][key]
                self.reject(lambda: self.check_full(terminal=terminal))

    def test_full_prerequisite_rejects_malformed_control_document_shapes(self):
        for malformed in (None, [], "foreign", 1):
            with self.subTest(shape=type(malformed).__name__):
                self.reject(lambda: OPERATOR.require_full_prerequisite(malformed, self.full_receipt, self.full_terminal))
                self.reject(lambda: OPERATOR.require_full_prerequisite(self.full_report, malformed, self.full_terminal))
                self.reject(lambda: OPERATOR.require_full_prerequisite(self.full_report, self.full_receipt, malformed))
                self.reject(lambda: self.check_full(terminal=dict(self.full_terminal, properties=malformed)))

    def worker_admission_fixture(self):
        request = {
            "verification": "source54-original-storage-scan-recovery-private-mount-v2",
            "source": str(WORK / "source-attempt-54"), "manifest": MANIFEST,
            "run_id": self.full_run, "unit": self.worker_unit,
            "tag": "goby-client-backup-pair-m3e-v1:" + self.full_run,
            "output": str(self.output), "output_identity": {"device": 1, "inode": 2},
            "pairs": [{"name": name, "phase": "owned", "role_oid": 17001 + index, "database_oid": 18001 + index}
                      for index, name in enumerate(PAIR_NAMES)],
            "worker_sha256": "e" * 64, "host_namespace": self.host,
        }
        request_sha256 = hashlib.sha256(json.dumps(request, sort_keys=True).encode()).hexdigest()
        run_script_sha256 = "f" * 64
        receipt = {key: copy.deepcopy(request[key]) for key in (
            "run_id", "unit", "tag", "output", "output_identity", "pairs", "source",
        )}
        receipt.update(
            marker="goby-client-backup-pair-m3e-v1", phase="running", cleanup_complete=False,
            mode="targeted", schema=28, source_manifest_sha256=MANIFEST,
            bound_scan_mount={
                "verification": request["verification"], "request_sha256": request_sha256,
                "worker_sha256": request["worker_sha256"], "host_namespace": self.host,
                "run_script_sha256": run_script_sha256,
            },
        )
        return receipt, request, request_sha256, run_script_sha256

    def test_worker_admission_accepts_only_new_owned_pair_in_pending_or_running_unit(self):
        receipt, request, request_sha256, run_script_sha256 = self.worker_admission_fixture()
        for phase in ("unit_pending", "running"):
            with self.subTest(phase=phase):
                OPERATOR.require_worker_admission(dict(receipt, phase=phase), request, request_sha256, run_script_sha256)

    def test_worker_admission_rejects_full_receipt_despite_all_other_matching_fields(self):
        receipt, request, request_sha256, run_script_sha256 = self.worker_admission_fixture()
        for mode in ("full", "catalog"):
            with self.subTest(mode=mode):
                self.reject(lambda: OPERATOR.require_worker_admission(
                    dict(receipt, mode=mode), request, request_sha256, run_script_sha256))

    def test_worker_admission_rejects_missing_or_inexact_explicit_admission(self):
        receipt, request, request_sha256, run_script_sha256 = self.worker_admission_fixture()
        missing = dict(receipt)
        del missing["bound_scan_mount"]
        self.reject(lambda: OPERATOR.require_worker_admission(missing, request, request_sha256, run_script_sha256))
        for admission in ({}, None, dict(receipt["bound_scan_mount"], foreign="unowned")):
            with self.subTest(admission=admission):
                self.reject(lambda: OPERATOR.require_worker_admission(
                    dict(receipt, bound_scan_mount=admission), request, request_sha256, run_script_sha256))

    def test_worker_admission_rejects_changed_request_worker_script_or_host_binding(self):
        receipt, request, request_sha256, run_script_sha256 = self.worker_admission_fixture()
        self.reject(lambda: OPERATOR.require_worker_admission(receipt, request, "a" * 64, run_script_sha256))
        self.reject(lambda: OPERATOR.require_worker_admission(receipt, request, request_sha256, "a" * 64))
        for key, value in (("worker_sha256", "a" * 64), ("host_namespace", "mnt:[124]")):
            with self.subTest(key=key):
                self.reject(lambda: OPERATOR.require_worker_admission(
                    receipt, dict(request, **{key: value}), request_sha256, run_script_sha256))
        for key in receipt["bound_scan_mount"]:
            with self.subTest(changed_admission=key):
                changed = copy.deepcopy(receipt)
                changed["bound_scan_mount"][key] += "-foreign"
                self.reject(lambda: OPERATOR.require_worker_admission(changed, request, request_sha256, run_script_sha256))

    def test_worker_admission_rejects_old_finished_retained_or_cleaned_receipts(self):
        receipt, request, request_sha256, run_script_sha256 = self.worker_admission_fixture()
        for phase in ("finished", "retained", "ready", "prepared"):
            with self.subTest(phase=phase):
                self.reject(lambda: OPERATOR.require_worker_admission(
                    dict(receipt, phase=phase), request, request_sha256, run_script_sha256))
        self.reject(lambda: OPERATOR.require_worker_admission(
            dict(receipt, cleanup_complete=True), request, request_sha256, run_script_sha256))

    def test_worker_admission_rejects_removed_or_incomplete_pair_even_when_receipt_and_request_agree(self):
        receipt, request, request_sha256, run_script_sha256 = self.worker_admission_fixture()
        for index in range(len(PAIR_NAMES)):
            for changes in ({"phase": "removed"}, {"phase": "database_removed"}, {"phase": "role_pending"},
                            {"role_oid": None}, {"database_oid": 0}, {"role_oid": True}):
                with self.subTest(pair=index, changes=changes):
                    changed_request, changed_receipt = copy.deepcopy(request), copy.deepcopy(receipt)
                    changed_request["pairs"][index].update(changes)
                    changed_receipt["pairs"] = copy.deepcopy(changed_request["pairs"])
                    changed_hash = hashlib.sha256(json.dumps(changed_request, sort_keys=True).encode()).hexdigest()
                    changed_receipt["bound_scan_mount"]["request_sha256"] = changed_hash
                    self.reject(lambda: OPERATOR.require_worker_admission(
                        changed_receipt, changed_request, changed_hash, run_script_sha256))

    def create_runner_fixture(self, base_failure=False):
        test = self
        operator_path = Path(OPERATOR.__file__)
        operator_bytes = b"# Fixed in-memory operator source.\n"
        files = {operator_path: operator_bytes}
        effects = []
        passwords = {PAIR_NAMES[0]: "a" * 64, PAIR_NAMES[1]: "b" * 64}
        original_script = b"#!/bin/bash\nset -euo pipefail\nexec fixed-original-command\n"
        original_environment = (
            "GOBY_TEST_DATABASE_URL=" + self.source_url + "\n" +
            "GOBY_TEST_BACKUP_SOURCE_DATABASE_URL=" + self.source_url + "\n" +
            "GOBY_BACKUP_PG_RUN_ID=" + self.full_run + "\n"
        ).encode()

        def private_read(path, **options):
            effects.append(("read", path))
            test.assertIn(path, files)
            return files[path]

        def create_private(path, value):
            effects.append(("create", path))
            OPERATOR.require(path not in files, "An in-memory owned file already exists.")
            files[path] = value

        def replace_private(path, expected, replacement):
            effects.append(("replace", path))
            OPERATOR.require(files.get(path) == expected, "An in-memory owned file changed.")
            files[path] = replacement

        class FakeBaseRunner:
            def __init__(self, args):
                self.args = args
                self.output = test.output
                self.output_identity = {"device": 1, "inode": 2}
                self.run = test.full_run
                self.unit = test.worker_unit
                self.tag = "goby-client-backup-pair-m3e-v1:" + self.run
                self.pairs = [
                    {"name": name, "phase": "owned", "role_oid": 17001 + index, "database_oid": 18001 + index}
                    for index, name in enumerate(PAIR_NAMES)
                ]
                self.report = {"status": "running", "cleanup": {}}
                self.receipt = {}

            def protected(self, *_args, **_kwargs):
                raise AssertionError("A protected lifecycle method must not execute in this fixture.")

            prepare = execute = cleanup = stop_unit = remove_pair = create_pair = check_cluster = protected

            def write_environment(self, given_passwords):
                effects.append(("base_environment", self.output))
                test.assertIs(given_passwords, passwords)
                if base_failure:
                    raise OPERATOR.Failure("The original environment guard rejected the run.")
                create_private(self.output / "run.env", original_environment)
                create_private(self.output / "run.sh", original_script)

        core = types.SimpleNamespace(
            Runner=FakeBaseRunner,
            private_read=Mock(side_effect=private_read),
            create_private=Mock(side_effect=create_private),
            replace_private=Mock(side_effect=replace_private),
        )
        args = types.SimpleNamespace(host_namespace=self.host)
        prerequisites = {"full_report_sha256": "c" * 64, "full_receipt_sha256": "d" * 64}
        runner = OPERATOR.create_runner(core, args, prerequisites)
        return types.SimpleNamespace(
            runner=runner, core=core, files=files, effects=effects, passwords=passwords, prerequisites=prerequisites,
            operator_path=operator_path, operator_bytes=operator_bytes,
            original_environment=original_environment, original_script=original_script,
        )

    def test_created_runner_inherits_every_protected_lifecycle_method_unchanged(self):
        fixture = self.create_runner_fixture()
        for method in ("prepare", "execute", "cleanup", "stop_unit", "remove_pair", "create_pair", "check_cluster"):
            with self.subTest(method=method):
                self.assertIs(getattr(type(fixture.runner), method), getattr(fixture.core.Runner, method))
                self.assertNotIn(method, type(fixture.runner).__dict__)
        self.assertEqual(fixture.effects, [("read", fixture.operator_path)])
        fixture.core.private_read.assert_called_once_with(fixture.operator_path, modes=(0o600, 0o644, 0o755))
        fixture.core.create_private.assert_not_called()
        fixture.core.replace_private.assert_not_called()
        self.assertEqual(fixture.runner.receipt, {})

    def test_worker_environment_extends_only_a_successfully_created_original_environment(self):
        fixture = self.create_runner_fixture()
        fixture.effects.clear()
        fixture.runner.write_environment(fixture.passwords)
        self.assertEqual(fixture.effects[0], ("base_environment", self.output))
        self.assertEqual(fixture.files[self.output / "run.env"], fixture.original_environment)
        self.assertEqual(fixture.files[self.output / "mount-worker.py"], fixture.operator_bytes)
        request_raw = fixture.files[self.output / "mount-request.json"]
        request = json.loads(request_raw)
        self.assertEqual(request, {
            "verification": "source54-original-storage-scan-recovery-private-mount-v2",
            "source": str(WORK / "source-attempt-54"), "manifest": MANIFEST,
            "output": str(self.output), "output_identity": fixture.runner.output_identity,
            "run_id": self.full_run, "unit": fixture.runner.unit, "tag": fixture.runner.tag,
            "pairs": fixture.runner.pairs, "host_namespace": self.host,
            "worker_sha256": hashlib.sha256(fixture.operator_bytes).hexdigest(),
            "environment_sha256": hashlib.sha256(fixture.original_environment).hexdigest(),
        })
        expected_script = (
            "#!/bin/bash\nset -euo pipefail\numask 077\nexec " + shlex.join([
                "/usr/bin/python3", "-I", "-B", str(self.output / "mount-worker.py"),
                "--worker", str(self.output / "mount-request.json"),
            ]) + "\n"
        ).encode()
        self.assertEqual(fixture.files[self.output / "run.sh"], expected_script)
        fixture.core.replace_private.assert_called_once_with(self.output / "run.sh", fixture.original_script, expected_script)
        self.assertEqual(fixture.runner.mount_request_sha256, hashlib.sha256(request_raw).hexdigest())
        self.assertEqual(fixture.runner.mount_script_sha256, hashlib.sha256(expected_script).hexdigest())
        self.assertEqual(fixture.runner.receipt, {"bound_scan_mount": {
            "verification": "source54-original-storage-scan-recovery-private-mount-v2",
            "request_sha256": hashlib.sha256(request_raw).hexdigest(),
            "worker_sha256": hashlib.sha256(fixture.operator_bytes).hexdigest(),
            "host_namespace": self.host,
            "run_script_sha256": hashlib.sha256(expected_script).hexdigest(),
        }})
        self.assertEqual(fixture.runner.report["prerequisites"], fixture.prerequisites)
        self.assertEqual(fixture.runner.report["operator_sha256"], hashlib.sha256(fixture.operator_bytes).hexdigest())
        self.assertEqual(fixture.runner.report["status"], "running")
        self.assertEqual(fixture.runner.report["cleanup"], {})
        self.assertTrue(all(path.parent == self.output for action, path in fixture.effects if action in ("create", "replace")))
        self.assertNotIn(fixture.passwords[PAIR_NAMES[0]].encode(), expected_script)
        self.assertNotIn(fixture.passwords[PAIR_NAMES[1]].encode(), request_raw)

    def test_failed_original_environment_guard_prevents_every_worker_artifact_and_script_replacement(self):
        fixture = self.create_runner_fixture(base_failure=True)
        fixture.effects.clear()
        self.reject(lambda: fixture.runner.write_environment(fixture.passwords))
        self.assertEqual(fixture.effects, [("base_environment", self.output)])
        fixture.core.create_private.assert_not_called()
        fixture.core.replace_private.assert_not_called()
        self.assertNotIn("mount_request_sha256", fixture.runner.__dict__)
        self.assertNotIn("mount_script_sha256", fixture.runner.__dict__)
        self.assertEqual(fixture.runner.report, {"status": "running", "cleanup": {}})
        self.assertEqual(fixture.runner.receipt, {})

    def test_existing_worker_evidence_is_never_adopted_or_overwritten(self):
        fixture = self.create_runner_fixture()
        fixture.files[self.output / "mount-worker.py"] = b"Foreign worker evidence.\n"
        self.reject(lambda: fixture.runner.write_environment(fixture.passwords))
        fixture.core.replace_private.assert_not_called()
        self.assertEqual(fixture.files[self.output / "mount-worker.py"], b"Foreign worker evidence.\n")
        self.assertNotIn(self.output / "mount-request.json", fixture.files)
        self.assertEqual(fixture.files[self.output / "run.sh"], fixture.original_script)
        self.assertEqual(fixture.runner.receipt, {})

    def test_failed_request_write_does_not_admit_worker_or_replace_original_script(self):
        fixture = self.create_runner_fixture()
        original_create = fixture.core.create_private.side_effect

        def create_private(path, value):
            if path == self.output / "mount-request.json":
                raise OPERATOR.Failure("The owned request could not be written.")
            original_create(path, value)

        fixture.core.create_private.side_effect = create_private
        self.reject(lambda: fixture.runner.write_environment(fixture.passwords))
        fixture.core.replace_private.assert_not_called()
        self.assertNotIn("mount_request_sha256", fixture.runner.__dict__)
        self.assertNotIn("mount_script_sha256", fixture.runner.__dict__)
        self.assertNotIn(self.output / "mount-request.json", fixture.files)
        self.assertEqual(fixture.files[self.output / "run.sh"], fixture.original_script)
        self.assertEqual(fixture.runner.receipt, {})

    def test_failed_owned_script_replacement_does_not_publish_launch_digests(self):
        fixture = self.create_runner_fixture()
        fixture.core.replace_private.side_effect = OPERATOR.Failure("The owned script changed before replacement.")
        self.reject(lambda: fixture.runner.write_environment(fixture.passwords))
        self.assertNotIn("mount_request_sha256", fixture.runner.__dict__)
        self.assertNotIn("mount_script_sha256", fixture.runner.__dict__)
        self.assertEqual(fixture.files[self.output / "run.sh"], fixture.original_script)
        self.assertEqual(fixture.runner.report["status"], "running")
        self.assertEqual(fixture.runner.report["cleanup"], {})
        self.assertEqual(fixture.runner.receipt, {})

    def test_full_prerequisite_rejects_another_self_consistent_source54_full_run(self):
        other = "20260912_063517_9cb0074fc731"
        unit = "goby-client-backup-" + other.replace("_", "-") + ".service"
        report = dict(self.full_report, run_id=other, unit=unit)
        receipt = dict(self.full_receipt, run_id=other, unit=unit,
                       output=str(WORK / ("client-backup-run-" + other)))
        self.reject(lambda: self.check_full(report=report, receipt=receipt))

    def test_worker_admission_rejects_reuse_of_the_fixed_full_run_identity(self):
        receipt, request, _, script_hash = self.worker_admission_fixture()
        request.update(run_id=FULL_RUN, unit=FULL_INNER_UNIT,
                       tag="goby-client-backup-pair-m3e-v1:" + FULL_RUN, output=str(FULL_REPORT.parent))
        for key in ("run_id", "unit", "tag", "output"):
            receipt[key] = request[key]
        request_hash = hashlib.sha256(json.dumps(request, sort_keys=True).encode()).hexdigest()
        receipt["bound_scan_mount"]["request_sha256"] = request_hash
        self.reject(lambda: OPERATOR.require_worker_admission(receipt, request, request_hash, script_hash))

    def prerequisite_fixture(self):
        files = {}
        receipt_path = Path("/owned/current-pair-receipt.json")
        control = Path("/owned/control")
        report = copy.deepcopy(self.full_report)
        receipt = copy.deepcopy(self.full_receipt)
        report.update(hba_before_sha256="a" * 64, hba_after_sha256="a" * 64, cluster={"system_identifier": "123456789"})
        previous = {
            "marker": "goby-client-backup-pair-m3e-v1", "run_id": FAILED_RUN,
            "unit": "goby-client-backup-" + FAILED_RUN.replace("_", "-") + ".service",
            "tag": "goby-client-backup-pair-m3e-v1:" + FAILED_RUN,
            "output": str(FAILED_OUTPUT), "output_identity": {"device": 31, "inode": 32},
            "phase": "retained", "cleanup_complete": False, "mode": "targeted", "schema": 28,
            "source": str(OPERATOR.SOURCE), "source_manifest_sha256": MANIFEST,
            "cluster": {"system_identifier": "123456789"},
            "pairs": [{"name": name, "role_oid": 101 + index, "database_oid": 201 + index, "phase": "owned"}
                      for index, name in enumerate(PAIR_NAMES)],
        }
        previous_raw = json.dumps(previous, sort_keys=True).encode()
        disposal_report = b'{"status":"disposed","fixed_failure_preserved":true}\n'
        disposal = {"marker": "goby-client-backup-disposal-m3e-v1", "status": "disposed", "run_id": FAILED_RUN,
                    "tag": previous["tag"], "source_receipt_sha256": hashlib.sha256(previous_raw).hexdigest(),
                    "pairs": [{key: pair[key] for key in ("name", "role_oid", "database_oid")} for pair in previous["pairs"]],
                    "system_identifier": "123456789", "hba_sha256": "a" * 64,
                    "report_path": str(FAILED_OUTPUT / "disposal-report.json"), "report_sha256": hashlib.sha256(disposal_report).hexdigest()}
        disposal_path = control / ("client-backup-disposal-" + FAILED_RUN + ".json")
        files[disposal_path] = json.dumps(disposal, sort_keys=True).encode()
        files[FAILED_OUTPUT / "disposal-report.json"] = disposal_report
        prior = json.dumps({"status": "passed", "host_mountinfo_unchanged": True,
                            "host_namespace_unchanged": True, "fixture_directory_empty": True}).encode()
        prior_worker = b"Reviewed namespace worker fixture.\n"
        files[OPERATOR.PRIOR_MOUNT / "report.json"] = prior
        files[OPERATOR.PRIOR_MOUNT / "worker.py"] = prior_worker
        effects = []

        def private_read(path, **_options):
            effects.append(("read", path))
            if path == FULL_REPORT:
                return json.dumps(report, sort_keys=True).encode()
            if path == FULL_RECEIPT:
                return json.dumps(receipt, sort_keys=True).encode()
            if path == receipt_path:
                return json.dumps(previous, sort_keys=True).encode()
            OPERATOR.require(path in files, "Required in-memory evidence is missing.")
            return files[path]

        def directory(path):
            if path == FULL_REPORT.parent:
                return receipt["output_identity"]
            self.assertEqual(path, FAILED_OUTPUT)
            return previous["output_identity"]

        core = types.SimpleNamespace(
            Failure=OPERATOR.Failure, RECEIPT=receipt_path, CONTROL=control, decode=json.loads,
            private_read=Mock(side_effect=private_read), directory=Mock(side_effect=directory), validate_disposal=Mock(),
        )
        args = types.SimpleNamespace(full_report=FULL_REPORT,
                                     full_report_sha256=hashlib.sha256(private_read(FULL_REPORT)).hexdigest(),
                                     host_namespace=self.host, worker=None)
        effects.clear()
        return types.SimpleNamespace(core=core, args=args, report=report, receipt=receipt, files=files, effects=effects,
                                     previous=previous, previous_raw=previous_raw, disposal=disposal, disposal_path=disposal_path,
                                     full_receipt_sha256=hashlib.sha256(json.dumps(receipt, sort_keys=True).encode()).hexdigest(),
                                     failed_receipt_sha256=hashlib.sha256(previous_raw).hexdigest(),
                                     terminal=copy.deepcopy(self.full_terminal), prior=prior, prior_worker=prior_worker)

    @contextlib.contextmanager
    def prerequisite_effects(self, fixture):
        with contextlib.ExitStack() as patches:
            patches.enter_context(patch.object(OPERATOR, "controller_terminal", return_value=fixture.terminal))
            patches.enter_context(patch.object(OPERATOR, "PRIOR_REPORT_SHA256", hashlib.sha256(fixture.prior).hexdigest()))
            patches.enter_context(patch.object(OPERATOR, "PRIOR_WORKER_SHA256", hashlib.sha256(fixture.prior_worker).hexdigest()))
            patches.enter_context(patch.object(OPERATOR, "FULL_RECEIPT_SHA256", fixture.full_receipt_sha256))
            patches.enter_context(patch.object(OPERATOR, "FAILED_RECEIPT_SHA256", fixture.failed_receipt_sha256))
            patches.enter_context(patch.object(os, "readlink", return_value=self.host))
            yield

    @contextlib.contextmanager
    def controller_effects(self, fixture):
        with self.prerequisite_effects(fixture), contextlib.ExitStack() as patches:
            parser = Mock()
            parser.parse_args.return_value = fixture.args
            patches.enter_context(patch.object(argparse, "ArgumentParser", return_value=parser))
            patches.enter_context(patch.object(os, "umask", return_value=0o077))
            patches.enter_context(patch.object(os, "geteuid", return_value=0))
            patches.enter_context(patch.object(os, "environ", {"SSH_CONNECTION": "authorized-memory-fixture"}))
            patches.enter_context(patch.object(signal, "signal"))
            loader = patches.enter_context(patch.object(OPERATOR, "load_core", return_value=fixture.core))
            runner = Mock()
            runner.run_all.return_value = 0
            factory = patches.enter_context(patch.object(OPERATOR, "create_runner", return_value=runner))
            yield loader, factory, runner

    def test_real_prerequisite_reader_rejects_another_report_before_any_read(self):
        fixture = self.prerequisite_fixture()
        fixture.args.full_report = self.output / "report.json"
        with self.prerequisite_effects(fixture):
            self.reject(lambda: OPERATOR.read_prerequisites(fixture.core, fixture.args))
        fixture.core.private_read.assert_not_called()
        fixture.core.directory.assert_not_called()

    def test_real_main_rejects_incomplete_full_before_constructing_or_preparing_a_runner(self):
        for mutation in ("report running", "receipt running", "controller running", "occupied cgroup", "changed digest"):
            with self.subTest(mutation=mutation):
                fixture = self.prerequisite_fixture()
                if mutation == "report running":
                    fixture.report["status"] = "running"
                    fixture.args.full_report_sha256 = hashlib.sha256(json.dumps(fixture.report, sort_keys=True).encode()).hexdigest()
                elif mutation == "receipt running":
                    fixture.receipt.update(phase="running", cleanup_complete=False)
                elif mutation == "controller running":
                    fixture.terminal["properties"].update(ActiveState="active", SubState="running", MainPID="123")
                elif mutation == "occupied cgroup":
                    fixture.terminal["cgroup_empty"] = False
                else:
                    fixture.args.full_report_sha256 = "0" * 64
                with self.controller_effects(fixture) as (_, factory, runner):
                    self.reject(lambda: OPERATOR.main([]))
                factory.assert_not_called()
                runner.run_all.assert_not_called()

    def test_real_main_dispatches_new_targeted_runner_only_after_exact_prerequisites(self):
        fixture = self.prerequisite_fixture()
        with self.controller_effects(fixture) as (loader, factory, runner):
            self.assertEqual(OPERATOR.main([]), 0)
        loader.assert_called_once_with()
        factory.assert_called_once()
        runner.run_all.assert_called_once_with()
        core, args, prerequisites = factory.call_args.args
        self.assertIs(core, fixture.core)
        self.assertEqual((args.source, args.manifest_sha256, args.schema, args.mode, args.package, args.run),
                         (WORK / "source-attempt-54", MANIFEST, 28, "targeted", ["./internal/library"], "^" + TEST_NAME + "$"))
        self.assertEqual(prerequisites["full_run"], FULL_RUN)
        self.assertEqual(prerequisites["full_inner_unit"], FULL_INNER_UNIT)
        self.assertEqual(prerequisites["full_report"], str(FULL_REPORT))
        self.assertIn(("read", fixture.core.RECEIPT), fixture.effects)
        self.assertIn(("read", FULL_RECEIPT), fixture.effects)
        self.assertIn(("read", OPERATOR.PRIOR_MOUNT / "worker.py"), fixture.effects)
        fixture.core.validate_disposal.assert_called_once_with(fixture.previous, fixture.previous_raw, fixture.disposal, "a" * 64)
        self.assertEqual(prerequisites["failed_run_disposal"]["run_id"], FAILED_RUN)

    def test_real_main_uses_the_fixed_historical_full_receipt_and_rejects_retained_full(self):
        fixture = self.prerequisite_fixture()
        fixture.receipt.update(phase="retained", cleanup_complete=False)
        fixture.full_receipt_sha256 = hashlib.sha256(json.dumps(fixture.receipt, sort_keys=True).encode()).hexdigest()
        with self.controller_effects(fixture) as (_, factory, runner):
            self.reject(lambda: OPERATOR.main([]))
        factory.assert_not_called()
        runner.run_all.assert_not_called()
        self.assertIn(("read", FULL_RECEIPT), fixture.effects)
        self.assertNotIn(("read", fixture.core.RECEIPT), fixture.effects)

    def test_real_main_refuses_new_prepare_without_exact_failed_run_disposal(self):
        for mutation in ("current receipt changed", "current receipt is full", "disposal absent", "validator rejected",
                         "report changed", "report foreign", "HBA mismatch", "cluster mismatch"):
            with self.subTest(mutation=mutation):
                fixture = self.prerequisite_fixture()
                if mutation == "current receipt changed":
                    fixture.previous["phase"] = "finished"
                elif mutation == "current receipt is full":
                    fixture.previous.clear()
                    fixture.previous.update(copy.deepcopy(fixture.receipt))
                elif mutation == "disposal absent":
                    del fixture.files[fixture.disposal_path]
                elif mutation == "validator rejected":
                    fixture.core.validate_disposal.side_effect = OPERATOR.Failure("Exact disposal validation rejected the retained run.")
                elif mutation == "report changed":
                    fixture.files[FAILED_OUTPUT / "disposal-report.json"] += b"changed"
                elif mutation == "report foreign":
                    fixture.disposal["report_path"] = str(FULL_REPORT)
                    fixture.files[fixture.disposal_path] = json.dumps(fixture.disposal, sort_keys=True).encode()
                elif mutation == "HBA mismatch":
                    fixture.report["hba_after_sha256"] = "b" * 64
                else:
                    fixture.report["cluster"]["system_identifier"] = "foreign-cluster"
                fixture.args.full_report_sha256 = hashlib.sha256(json.dumps(fixture.report, sort_keys=True).encode()).hexdigest()
                with self.controller_effects(fixture) as (_, factory, runner):
                    self.reject(lambda: OPERATOR.main([]))
                factory.assert_not_called()
                runner.run_all.assert_not_called()

    def test_worker_admission_cannot_replay_the_failed_mount_run(self):
        receipt, request, _, script_hash = self.worker_admission_fixture()
        unit = "goby-client-backup-" + FAILED_RUN.replace("_", "-") + ".service"
        request.update(run_id=FAILED_RUN, unit=unit, tag="goby-client-backup-pair-m3e-v1:" + FAILED_RUN, output=str(FAILED_OUTPUT))
        for key in ("run_id", "unit", "tag", "output"):
            receipt[key] = request[key]
        request_hash = hashlib.sha256(json.dumps(request, sort_keys=True).encode()).hexdigest()
        receipt["bound_scan_mount"]["request_sha256"] = request_hash
        self.reject(lambda: OPERATOR.require_worker_admission(receipt, request, request_hash, script_hash))

    def test_real_main_worker_mode_rejects_controller_arguments_without_dispatch(self):
        fixture = self.prerequisite_fixture()
        fixture.args.worker = self.output / "mount-request.json"
        with self.controller_effects(fixture) as (loader, factory, runner), patch.object(OPERATOR, "worker") as worker:
            self.reject(lambda: OPERATOR.main([]))
        loader.assert_not_called()
        factory.assert_not_called()
        runner.run_all.assert_not_called()
        worker.assert_not_called()

    def test_real_controller_terminal_reads_only_the_fixed_controller_and_cgroup(self):
        core = types.SimpleNamespace(command=Mock(return_value="\n".join(
            key + "=" + value for key, value in self.full_terminal["properties"].items())), present=Mock(return_value=True))
        cgroup = Path("/sys/fs/cgroup/system.slice") / OPERATOR.FULL_UNIT / "cgroup.procs"
        for contents, empty in (("", True), ("123\n", False)):
            with self.subTest(contents=contents):
                def read_text(path):
                    self.assertEqual(path, cgroup)
                    return contents
                with patch.object(Path, "read_text", read_text):
                    observed = OPERATOR.controller_terminal(core)
                self.assertEqual(observed["unit"], OPERATOR.FULL_UNIT)
                self.assertIs(observed["cgroup_empty"], empty)
                if not empty:
                    self.reject(lambda: self.check_full(terminal=observed))
        core.command.assert_called_with(["/usr/bin/systemctl", "show", OPERATOR.FULL_UNIT,
                                         "--property=InvocationID,ActiveState,SubState,MainPID,ExecMainStatus,Result,ControlGroup"])
        core.present.assert_called_with(cgroup)

    def test_real_namespace_observer_requires_exact_argv_cgroup_and_process_start(self):
        process = types.SimpleNamespace(pid=40001)
        directory = Path("/proc/40001")
        command = self.command()
        for mutation in ("valid", "host namespace", "other argv", "other cgroup", "missing start"):
            with self.subTest(mutation=mutation):
                def read_bytes(path):
                    self.assertEqual(path, directory / "cmdline")
                    arguments = command if mutation != "other argv" else command + ["-test.run=."]
                    return b"\0".join(argument.encode() for argument in arguments) + b"\0"
                def read_text(path):
                    if path == directory / "cgroup":
                        return "0::/system.slice/" + (self.worker_unit if mutation != "other cgroup" else "other.service") + "\n"
                    self.assertEqual(path, directory / "stat")
                    fields = ["S"] + ["0"] * 18 + ["300"]
                    return "40001 (unshare) " + " ".join(fields if mutation != "missing start" else fields[:-1])
                def readlink(path):
                    self.assertEqual(path, directory / "ns/mnt")
                    return self.host if mutation == "host namespace" else "mnt:[124]"
                with patch.object(Path, "read_bytes", read_bytes), patch.object(Path, "read_text", read_text), \
                        patch.object(os, "readlink", side_effect=readlink):
                    if mutation in ("other argv", "other cgroup", "missing start"):
                        self.reject(lambda: OPERATOR.observed_namespace(process, command, self.host, self.worker_unit))
                    else:
                        value = OPERATOR.observed_namespace(process, command, self.host, self.worker_unit)
                        if mutation == "host namespace":
                            self.assertIsNone(value)
                        else:
                            self.assertEqual(value, {"namespace": "mnt:[124]", "pid": 40001, "start_ticks": "300",
                                                     "cgroup": "/system.slice/" + self.worker_unit})

    def block_fixture(self, names=None):
        names = ["sda", "sda1", "sda14", "sda15", "sr0", "sr1"] if names is None else names
        fixture = types.SimpleNamespace(names=names, views=[names], scans=0, root_reads=0, attribute_reads={},
                                        missing=False, root_changed=False, wrong_target=False, attributes={})
        numbers = {"sda": "8:0\n", "sda1": "8:1\n", "sda14": "8:14\n", "sda15": "8:15\n", "sr0": "11:0\n", "sr1": "11:1\n"}
        for name in set(names) | set(numbers) | {"loop0"}:
            device = OPERATOR.BLOCK_CLASS / name
            fixture.attributes[device / "dev"] = numbers.get(name, "7:0\n").encode()
            if re.fullmatch(r"loop[0-9]+", name):
                for field, value in {"backing_file": "/owned/loop-image\n", "offset": "0\n", "sizelimit": "0\n", "autoclear": "1\n"}.items():
                    fixture.attributes[device / "loop" / field] = value.encode()
        return fixture

    @contextlib.contextmanager
    def block_effects(self, fixture):
        root = OPERATOR.BLOCK_CLASS
        class MemoryDirectory:
            def __init__(self, names):
                self.names = names
            def __enter__(self):
                return iter(types.SimpleNamespace(name=name) for name in self.names)
            def __exit__(self, *_args):
                return False
        def scandir(path):
            self.assertEqual(path, root)
            view = fixture.views[min(fixture.scans, len(fixture.views) - 1)]
            fixture.scans += 1
            return MemoryDirectory(view)
        def lstat(path):
            if path == root:
                if fixture.missing:
                    raise FileNotFoundError("Required sysfs block class is missing.")
                fixture.root_reads += 1
                return types.SimpleNamespace(st_mode=stat.S_IFDIR | 0o555, st_uid=0, st_gid=0, st_dev=1,
                                             st_ino=99 if fixture.root_changed and fixture.root_reads > 1 else 10)
            self.assertEqual(path.parent, root)
            return types.SimpleNamespace(st_mode=stat.S_IFLNK | 0o777, st_uid=0, st_gid=0, st_dev=1, st_ino=20)
        def resolve(path, *, strict):
            self.assertTrue(strict)
            self.assertEqual(path.parent, root)
            return (Path("/outside") if fixture.wrong_target else Path("/sys/devices/pci0000:00/block")) / path.name
        def open_attribute(path, mode):
            self.assertEqual(mode, "rb")
            if path not in fixture.attributes:
                raise FileNotFoundError("Required sysfs attribute is missing.")
            value = fixture.attributes[path]
            index = fixture.attribute_reads.get(path, 0)
            fixture.attribute_reads[path] = index + 1
            if isinstance(value, list):
                value = value[min(index, len(value) - 1)]
            return io.BytesIO(value)
        with patch.object(os, "scandir", side_effect=scandir), patch.object(Path, "lstat", lstat), \
                patch.object(Path, "resolve", resolve), patch.object(Path, "open", open_attribute):
            yield

    def test_real_host_witness_records_complete_repeated_class_inventory_without_loops(self):
        fixture = self.block_fixture()
        published = {}
        mountinfo = b"Fixed host mount table.\n"
        def read_bytes(path):
            self.assertEqual(path, Path("/proc/1/mountinfo"))
            return mountinfo
        def readlink(path):
            self.assertIn(path, ("/proc/1/ns/mnt", "/proc/self/ns/mnt"))
            return self.host
        def publish(path, value):
            self.assertEqual(path.parent, self.output)
            self.assertNotIn(path, published)
            published[path] = value
        core = types.SimpleNamespace(create_private=Mock(side_effect=publish))
        with self.block_effects(fixture), patch.object(Path, "read_bytes", read_bytes), \
                patch.object(os, "readlink", side_effect=readlink):
            observed = OPERATOR.host_witness(core, self.output, "before")
        self.assertEqual(observed["mountinfo_sha256"], hashlib.sha256(mountinfo).hexdigest())
        loop_raw = published[self.output / "host-loops-before.json"]
        inventory = json.loads(loop_raw)
        self.assertEqual(set(inventory), {"path", "complete", "observations", "device_count", "loop_count", "devices"})
        self.assertEqual(inventory["path"], "/sys/class/block")
        self.assertIs(inventory["complete"], True)
        self.assertEqual(inventory["observations"], 2)
        self.assertEqual((inventory["device_count"], inventory["loop_count"]), (6, 0))
        self.assertEqual(set(inventory["devices"]), set(fixture.names))
        self.assertTrue(all(value["loop"] is None for value in inventory["devices"].values()))
        self.assertEqual(fixture.scans, 4)
        self.assertEqual(observed["loops_sha256"], hashlib.sha256(loop_raw).hexdigest())
        self.assertEqual(set(published), {self.output / "host-mountinfo-before", self.output / "host-loops-before.json"})

    def test_real_host_witness_rejects_missing_class_without_publishing_an_empty_inventory(self):
        fixture = self.block_fixture()
        fixture.missing = True
        core = types.SimpleNamespace(create_private=Mock())
        with self.block_effects(fixture), patch.object(Path, "read_bytes", return_value=b"Host mount table.\n"), \
                patch.object(os, "readlink", return_value=self.host):
            self.reject(lambda: OPERATOR.host_witness(core, self.output, "before"))
        core.create_private.assert_not_called()
        self.assertEqual(fixture.scans, 0)

    def test_block_inventory_rejects_changed_complete_membership_or_device_properties(self):
        for mutation in ("during membership", "repeated membership", "directory replaced", "device changed", "target escaped"):
            with self.subTest(mutation=mutation):
                fixture = self.block_fixture()
                if mutation == "during membership":
                    fixture.views = [fixture.names, fixture.names + ["loop0"]]
                elif mutation == "repeated membership":
                    fixture.views = [fixture.names, fixture.names, fixture.names + ["loop0"], fixture.names + ["loop0"]]
                elif mutation == "directory replaced":
                    fixture.root_changed = True
                elif mutation == "device changed":
                    fixture.attributes[OPERATOR.BLOCK_CLASS / "sda/dev"] = [b"8:0\n", b"8:0\n", b"8:16\n", b"8:16\n"]
                else:
                    fixture.wrong_target = True
                with self.block_effects(fixture):
                    self.reject(OPERATOR.block_class_inventory)

    def test_block_inventory_rejects_count_byte_and_attribute_exhaustion_without_truncation(self):
        for mutation in ("count", "inventory bytes", "attribute bytes", "duplicate"):
            with self.subTest(mutation=mutation):
                fixture = self.block_fixture()
                if mutation == "duplicate":
                    fixture.views = [["sda", "sda"]]
                with self.block_effects(fixture), contextlib.ExitStack() as patches:
                    if mutation == "count":
                        patches.enter_context(patch.object(OPERATOR, "MAX_BLOCK_DEVICES", 2))
                    elif mutation == "inventory bytes":
                        patches.enter_context(patch.object(OPERATOR, "MAX_BLOCK_INVENTORY_BYTES", 1000))
                    elif mutation == "attribute bytes":
                        patches.enter_context(patch.object(OPERATOR, "MAX_BLOCK_ATTRIBUTE_BYTES", 3))
                    self.reject(OPERATOR.block_class_inventory)

    def test_block_inventory_requires_complete_loop_attributes_and_repeats_their_values(self):
        fixture = self.block_fixture(["loop0"])
        with self.block_effects(fixture):
            observed = OPERATOR.block_class_inventory()
        self.assertEqual((observed["device_count"], observed["loop_count"]), (1, 1))
        self.assertEqual(observed["devices"]["loop0"]["loop"],
                         {"backing_file": "/owned/loop-image\n", "offset": "0\n", "sizelimit": "0\n", "autoclear": "1\n"})
        for mutation in ("attribute missing", "attribute changed", "wrong major"):
            with self.subTest(mutation=mutation):
                fixture = self.block_fixture(["loop0"])
                if mutation == "attribute missing":
                    del fixture.attributes[OPERATOR.BLOCK_CLASS / "loop0/loop/backing_file"]
                elif mutation == "attribute changed":
                    fixture.attributes[OPERATOR.BLOCK_CLASS / "loop0/loop/offset"] = [b"0\n", b"4096\n"]
                else:
                    fixture.attributes[OPERATOR.BLOCK_CLASS / "loop0/dev"] = b"8:0\n"
                with self.block_effects(fixture):
                    self.reject(OPERATOR.block_class_inventory)

    def worker_fixture(self):
        receipt, request, _, _ = self.worker_admission_fixture()
        output, effects, files = self.output, [], {}
        request_path = output / "mount-request.json"
        receipt_path = Path("/owned/current-mount-receipt.json")
        worker_bytes, script_bytes = b"Reviewed in-memory worker.\n", b"Reviewed in-memory launch.\n"
        helper_bytes = b"Reviewed in-memory Go helper.\n"
        environment = dict(self.environment, HOME="/root", GOCACHE="/owned/cache/build", GOMODCACHE="/owned/cache/modules",
                           GOTOOLCHAIN="local", GOWORK="off", GOFLAGS="-mod=readonly", GOTMPDIR=str(output / "tmp"),
                           TMPDIR=str(output / "tmp"), GOBY_TEST_BACKUP_SOURCE_DATABASE_URL=self.source_url,
                           GOBY_BACKUP_PG_RUN_ID=self.full_run)
        environment_raw = "".join(key + "=" + value + "\n" for key, value in environment.items()).encode()
        request.update(worker_sha256=hashlib.sha256(worker_bytes).hexdigest(),
                       environment_sha256=hashlib.sha256(environment_raw).hexdigest())
        request_raw = json.dumps(request, sort_keys=True).encode()
        receipt["bound_scan_mount"].update(request_sha256=hashlib.sha256(request_raw).hexdigest(),
                                          worker_sha256=request["worker_sha256"], run_script_sha256=hashlib.sha256(script_bytes).hexdigest())
        files.update({request_path: request_raw, receipt_path: json.dumps(receipt, sort_keys=True).encode(),
                      output / "mount-worker.py": worker_bytes, output / "run.sh": script_bytes,
                      output / "run.env": environment_raw,
                      OPERATOR.SOURCE / "internal/library/root_binding_scan_mount_namespace_test.go": helper_bytes,
                      Path("/usr/bin/unshare"): b"Fixed unshare executable."})
        fixture = types.SimpleNamespace(output=output, effects=effects, files=files, request=request, request_path=request_path,
                                        receipt=receipt, receipt_path=receipt_path, helper_bytes=helper_bytes, environment=environment,
                                        compile_exit=0, timeout=False, residual=False, host_changed=False, kill_failure=False,
                                        wait_failure=False, namespace_error=False, source_failure=False)

        class OwnedChild:
            pid = 40001
            returncode = None

            def poll(self):
                return self.returncode

            def kill(self):
                effects.append(("kill", self.pid))
                if fixture.kill_failure:
                    raise PermissionError("Owned child signal failed.")

            def wait(self, timeout):
                effects.append(("wait", self.pid, timeout))
                if fixture.wait_failure:
                    raise subprocess.TimeoutExpired("owned-unshare", timeout)
                self.returncode = -9
                return self.returncode

        fixture.child = OwnedChild()

        def private_read(path, **_options):
            effects.append(("read", path))
            self.assertIn(path, files)
            return files[path]

        def create_private(path, value):
            effects.append(("publish", path))
            self.assertNotIn(path, files)
            files[path] = value

        def verify_source(*arguments, **_options):
            effects.append(("source", arguments))
            self.assertEqual(arguments, (OPERATOR.SOURCE, MANIFEST, 28))
            if fixture.source_failure:
                raise OPERATOR.Failure("Frozen source changed.")

        fixture.core = types.SimpleNamespace(Failure=OPERATOR.Failure, RECEIPT=receipt_path, decode=json.loads,
                                             private_read=Mock(side_effect=private_read),
                                             create_private=Mock(side_effect=create_private),
                                             directory=Mock(return_value=request["output_identity"]),
                                             present=Mock(return_value=False), verify_source=Mock(side_effect=verify_source))
        return fixture

    @contextlib.contextmanager
    def worker_effects(self, fixture):
        output, files, effects = fixture.output, fixture.files, fixture.effects

        class MemoryOutput(io.BytesIO):
            def __init__(self, path):
                super().__init__()
                self.path = path

            def close(self):
                if not self.closed:
                    files[self.path] = self.getvalue()
                super().close()

        def open_output(path, mode):
            self.assertEqual(mode, "xb")
            self.assertEqual(path.parent, output)
            self.assertIn(path.name, ("compile.stdout", "compile.stderr", "helper.stdout", "helper.stderr"))
            self.assertNotIn(path, files)
            effects.append(("open", path))
            return MemoryOutput(path)

        def compile_binary(command, **options):
            effects.append(("compile", tuple(command)))
            self.assertEqual(command, OPERATOR.compile_command(output))
            self.assertEqual(options["cwd"], OPERATOR.SOURCE)
            self.assertEqual(options["timeout"], 600)
            self.assertEqual(options["env"]["CGO_ENABLED"], "1")
            self.assertEqual((options["env"]["GOPROXY"], options["env"]["GOSUMDB"]), ("off", "off"))
            options["stdout"].write(b"Fixed compilation output.\n")
            return types.SimpleNamespace(returncode=fixture.compile_exit)

        def launch(command, **options):
            effects.append(("launch", tuple(command)))
            self.assertEqual(command, OPERATOR.command_for_helper(output / "tmp/library.test"))
            self.assertEqual(options["env"], self.environment)
            self.assertEqual(options["cwd"], OPERATOR.SOURCE)
            options["stdout"].write(self.helper_stdout)
            return fixture.child

        def witness(_core, path, suffix):
            self.assertIs(_core, fixture.core)
            self.assertEqual(path, output)
            effects.append(("host", suffix))
            value = dict(self.host_state)
            if suffix == "after" and fixture.host_changed:
                value["mountinfo_sha256"] = "c" * 64
            return value

        def observe(process, command, host, unit):
            self.assertIs(process, fixture.child)
            self.assertEqual((host, unit), (self.host, self.worker_unit))
            effects.append(("observe", process.pid))
            if fixture.namespace_error:
                raise OPERATOR.Failure("Owned namespace witness changed.")
            return {"namespace": "mnt:[124]", "pid": process.pid, "start_ticks": "300", "cgroup": "/system.slice/" + unit}

        def pause(_duration):
            fixture.child.returncode = 0

        def read_text(path, *_args, **_kwargs):
            self.assertEqual(path, Path("/proc/self/cgroup"))
            return "0::/system.slice/" + self.worker_unit + "\n"

        def exists(path):
            self.assertEqual(path, Path("/proc", str(fixture.child.pid)))
            return False

        with contextlib.ExitStack() as patches:
            patches.enter_context(patch.object(OPERATOR, "load_core", return_value=fixture.core))
            patches.enter_context(patch.object(OPERATOR, "__file__", str(output / "mount-worker.py")))
            patches.enter_context(patch.object(OPERATOR, "HELPER_SHA256", hashlib.sha256(fixture.helper_bytes).hexdigest()))
            patches.enter_context(patch.object(OPERATOR, "artifact", return_value={"path": str(output / "tmp/library.test"), "sha256": "e" * 64}))
            patches.enter_context(patch.object(OPERATOR, "host_witness", side_effect=witness))
            patches.enter_context(patch.object(OPERATOR, "observed_namespace", side_effect=observe))
            patches.enter_context(patch.object(os, "getuid", return_value=0))
            patches.enter_context(patch.object(os, "geteuid", return_value=0))
            patches.enter_context(patch.object(os, "environ", {"INVOCATION_ID": "a" * 32, "GOBY_TEST_DATABASE_URL": self.source_url}))
            patches.enter_context(patch.object(Path, "read_text", read_text))
            patches.enter_context(patch.object(Path, "open", open_output))
            patches.enter_context(patch.object(Path, "exists", exists))
            patches.enter_context(patch.object(Path, "mkdir", lambda path, **_options: effects.append(("mkdir", path))))
            patches.enter_context(patch.object(Path, "iterdir", lambda path: iter([path / "leftover"] if fixture.residual else [])))
            patches.enter_context(patch.object(subprocess, "run", side_effect=compile_binary))
            patches.enter_context(patch.object(subprocess, "Popen", side_effect=launch))
            patches.enter_context(patch.object(time, "monotonic", side_effect=([0, 151] if fixture.timeout else [0, 0])))
            patches.enter_context(patch.object(time, "sleep", side_effect=pause))
            yield

    def test_real_worker_success_compiles_then_launches_only_the_owned_private_helper(self):
        fixture = self.worker_fixture()
        with self.worker_effects(fixture):
            self.assertEqual(OPERATOR.worker(fixture.request_path), 0)
        report = json.loads(fixture.files[self.output / "mount-report.json"])
        self.assertEqual(report["status"], "passed")
        self.assertEqual(report["child_cleanup"], {"terminal": True, "killed": False})
        self.assertIs(report["host_preserved"], True)
        stages = [event[0] for event in fixture.effects]
        self.assertEqual(stages.count("compile"), 1)
        self.assertEqual(stages.count("launch"), 1)
        self.assertLess(stages.index("compile"), stages.index("launch"))
        self.assertNotIn("kill", stages)
        self.assertNotIn("wait", stages)
        self.assertEqual(fixture.core.verify_source.call_count, 3)

    def test_real_worker_rejects_old_full_or_unadmitted_pair_before_compile_or_launch(self):
        for mutation in ("full mode", "finished", "missing admission", "changed request"):
            with self.subTest(mutation=mutation):
                fixture = self.worker_fixture()
                if mutation == "full mode":
                    fixture.receipt["mode"] = "full"
                elif mutation == "finished":
                    fixture.receipt.update(phase="finished", cleanup_complete=True)
                elif mutation == "missing admission":
                    del fixture.receipt["bound_scan_mount"]
                else:
                    fixture.receipt["bound_scan_mount"]["request_sha256"] = "0" * 64
                fixture.files[fixture.receipt_path] = json.dumps(fixture.receipt, sort_keys=True).encode()
                with self.worker_effects(fixture):
                    self.assertEqual(OPERATOR.worker(fixture.request_path), 1)
                self.assertFalse(any(event[0] in ("compile", "launch", "kill", "wait", "mkdir") for event in fixture.effects))
                self.assertEqual(json.loads(fixture.files[self.output / "mount-report.json"])["status"], "failed")

    def test_real_worker_source_and_compile_failures_cannot_enter_a_namespace(self):
        for mutation in ("source changed", "compile failed"):
            with self.subTest(mutation=mutation):
                fixture = self.worker_fixture()
                fixture.source_failure = mutation == "source changed"
                fixture.compile_exit = 1 if mutation == "compile failed" else 0
                with self.worker_effects(fixture):
                    self.assertEqual(OPERATOR.worker(fixture.request_path), 1)
                self.assertFalse(any(event[0] in ("launch", "kill", "wait", "host") for event in fixture.effects))
                self.assertEqual(json.loads(fixture.files[self.output / "mount-report.json"])["status"], "failed")

    def test_real_worker_timeout_terminates_only_its_retained_child_and_records_failure(self):
        fixture = self.worker_fixture()
        fixture.timeout = True
        with self.worker_effects(fixture):
            self.assertEqual(OPERATOR.worker(fixture.request_path), 1)
        self.assertEqual([event for event in fixture.effects if event[0] in ("kill", "wait")],
                         [("kill", fixture.child.pid), ("wait", fixture.child.pid, 15)])
        report = json.loads(fixture.files[self.output / "mount-report.json"])
        self.assertEqual(report["child_cleanup"], {"terminal": True, "killed": True})
        self.assertEqual(report["status"], "failed")
        self.assertIs(report["host_preserved"], True)

    def test_real_worker_signal_or_wait_failure_still_saves_host_and_failure_evidence(self):
        for mutation in ("signal", "wait"):
            with self.subTest(mutation=mutation):
                fixture = self.worker_fixture()
                fixture.timeout = True
                fixture.kill_failure, fixture.wait_failure = mutation == "signal", mutation == "wait"
                with self.worker_effects(fixture):
                    self.assertEqual(OPERATOR.worker(fixture.request_path), 1)
                report = json.loads(fixture.files[self.output / "mount-report.json"])
                self.assertEqual(report["status"], "failed")
                self.assertEqual(report["child_cleanup"], {"terminal": False})
                self.assertIs(report["host_preserved"], True)
                self.assertEqual([event for event in fixture.effects if event[0] == "kill"], [("kill", fixture.child.pid)])

    def test_real_worker_host_drift_or_residual_fixture_prevents_passing_result(self):
        for mutation in ("host drift", "fixture retained", "namespace changed"):
            with self.subTest(mutation=mutation):
                fixture = self.worker_fixture()
                fixture.host_changed = mutation == "host drift"
                fixture.residual = mutation == "fixture retained"
                fixture.namespace_error = mutation == "namespace changed"
                with self.worker_effects(fixture):
                    self.assertEqual(OPERATOR.worker(fixture.request_path), 1)
                self.assertEqual(json.loads(fixture.files[self.output / "mount-report.json"])["status"], "failed")

    def test_real_worker_changed_environment_or_existing_binary_cannot_compile_or_launch(self):
        for mutation in ("environment changed", "binary exists"):
            with self.subTest(mutation=mutation):
                fixture = self.worker_fixture()
                if mutation == "environment changed":
                    fixture.files[self.output / "run.env"] += b"FOREIGN=1\n"
                else:
                    fixture.core.present.return_value = True
                with self.worker_effects(fixture):
                    self.assertEqual(OPERATOR.worker(fixture.request_path), 1)
                self.assertFalse(any(event[0] in ("compile", "launch", "kill", "wait", "mkdir") for event in fixture.effects))

    def test_real_worker_report_publication_failure_cannot_return_success(self):
        fixture = self.worker_fixture()
        fixture.core.create_private.side_effect = OPERATOR.Failure("Owned result could not be published.")
        with self.worker_effects(fixture):
            self.reject(lambda: OPERATOR.worker(fixture.request_path))
        self.assertNotIn(self.output / "mount-report.json", fixture.files)
        self.assertFalse(any(event[0] in ("kill", "wait") for event in fixture.effects))

    def result_fixture(self):
        fixture = self.create_runner_fixture()
        fixture.runner.write_environment(fixture.passwords)
        fixture.core.RECEIPT = Path("/owned/current-result-receipt.json")
        fixture.core.decode = json.loads
        fixture.runner.receipt_bytes = b"Exact current result receipt.\n"
        fixture.files[fixture.core.RECEIPT] = fixture.runner.receipt_bytes
        fixture.core.directory = Mock(return_value=fixture.runner.output_identity)
        fixture.core.unit_state = Mock(return_value={"LoadState": "loaded", "ActiveState": "inactive", "MainPID": "0"})
        fixture.core.require_unit_terminal = Mock()
        fixture.core.command = Mock(return_value="a" * 32 + "\n")
        mountinfo, loops = b"Fixed host mountinfo.\n", b'{"loop0": {"backing_file": null}}'
        host = {"namespace": self.host, "mountinfo_sha256": hashlib.sha256(mountinfo).hexdigest(),
                "loops_sha256": hashlib.sha256(loops).hexdigest()}
        binary = {"path": str(self.output / "tmp/library.test"), "sha256": "d" * 64, "device": 1, "inode": 3, "bytes": 2000000}
        observed = {"status": "passed", "verification": OPERATOR.VERIFICATION, "source": str(OPERATOR.SOURCE), "manifest": MANIFEST,
                    "schema": 28, "system_reboot_tested": False, "loop_scope": "observed-only-no-loop-allocation",
                    "run_id": fixture.runner.run, "source_unchanged": True, "binary_unchanged": True, "fixture_directory_empty": True,
                    "host_preserved": True, "child_cleanup": {"terminal": True, "killed": False},
                    "helper": OPERATOR.inspect_helper_output(self.helper_stdout, b"", 0), "host_before": dict(host), "host_after": dict(host),
                    "private_namespace": {"namespace": "mnt:[124]", "pid": 40001, "start_ticks": "300",
                                          "cgroup": "/system.slice/" + fixture.runner.unit, "subprocess_terminal": True},
                    "binary": binary, "invocation_id": "a" * 32}
        fixture.files.update({self.output / "helper.stdout": self.helper_stdout, self.output / "helper.stderr": b"",
                              self.output / "go.log": b"Fixed owned unit log.\n"})
        for suffix in ("before", "after"):
            fixture.files[self.output / ("host-mountinfo-" + suffix)] = mountinfo
            fixture.files[self.output / ("host-loops-" + suffix + ".json")] = loops
        fixture.observed, fixture.host, fixture.binary = observed, host, binary
        fixture.residual = False
        return fixture

    @contextlib.contextmanager
    def result_effects(self, fixture):
        fixture.files[self.output / "mount-report.json"] = json.dumps(fixture.observed, sort_keys=True).encode()
        with patch.object(OPERATOR, "artifact", return_value=fixture.binary), \
                patch.object(OPERATOR, "host_witness", return_value=fixture.host), \
                patch.object(Path, "iterdir", lambda path: iter([path / "leftover"] if fixture.residual else [])):
            yield

    def test_real_result_inspection_accepts_only_complete_owned_proofs(self):
        fixture = self.result_fixture()
        with self.result_effects(fixture):
            fixture.runner.inspect_results()
        self.assertEqual(fixture.runner.report["tests"], {"top_level_passes": 1, "passed": [TEST_NAME], "failures": 0, "skips": 0})
        self.assertEqual(fixture.runner.report["mount"], fixture.observed)
        fixture.core.require_unit_terminal.assert_called_once_with(fixture.core.unit_state.return_value, fixture.runner.unit, fixture.runner.tag)
        fixture.core.command.assert_called_once_with(["/usr/bin/systemctl", "show", fixture.runner.unit, "--property=InvocationID", "--value"])

    def test_real_result_inspection_rejects_missing_or_changed_completion_fields(self):
        invalid = {"status": "failed", "verification": "other", "source": "/other", "manifest": "0" * 64,
                   "run_id": FULL_RUN, "schema": True, "system_reboot_tested": True, "loop_scope": "allocated",
                   "source_unchanged": 1, "binary_unchanged": False, "fixture_directory_empty": False, "host_preserved": False,
                   "child_cleanup": {"terminal": 1, "killed": 0}, "invocation_id": "not-an-invocation"}
        for field, value in invalid.items():
            for missing in (False, True):
                with self.subTest(field=field, missing=missing):
                    fixture = self.result_fixture()
                    if missing:
                        del fixture.observed[field]
                    else:
                        fixture.observed[field] = value
                    with self.result_effects(fixture):
                        self.reject(fixture.runner.inspect_results)
                    self.assertNotIn("mount", fixture.runner.report)

    def test_real_result_inspection_rejects_changed_owned_artifacts_or_current_receipt(self):
        for filename in ("mount-request.json", "run.sh", "run.env", "mount-worker.py", "helper.stdout", "helper.stderr",
                         "host-mountinfo-before", "host-mountinfo-after", "host-loops-before.json", "host-loops-after.json", "receipt"):
            with self.subTest(filename=filename):
                fixture = self.result_fixture()
                path = fixture.core.RECEIPT if filename == "receipt" else self.output / filename
                fixture.files[path] += b"changed"
                with self.result_effects(fixture):
                    self.reject(fixture.runner.inspect_results)
                self.assertNotIn("mount", fixture.runner.report)

    def test_real_result_inspection_rejects_incomplete_private_namespace_proofs(self):
        invalid = {"namespace": self.host, "pid": True, "start_ticks": "not-ticks", "cgroup": "/system.slice/foreign.service",
                   "subprocess_terminal": 1}
        for field, value in invalid.items():
            for missing in (False, True):
                with self.subTest(field=field, missing=missing):
                    fixture = self.result_fixture()
                    if missing:
                        del fixture.observed["private_namespace"][field]
                    else:
                        fixture.observed["private_namespace"][field] = value
                    with self.result_effects(fixture):
                        self.reject(fixture.runner.inspect_results)
                    self.assertNotIn("mount", fixture.runner.report)

    def test_real_result_inspection_rejects_missing_or_malformed_nested_proofs(self):
        for field in ("helper", "host_before", "host_after", "binary", "private_namespace", "child_cleanup"):
            for value in (None, {}, []):
                with self.subTest(field=field, value=value):
                    fixture = self.result_fixture()
                    fixture.observed[field] = value
                    with self.result_effects(fixture):
                        self.reject(fixture.runner.inspect_results)
                    self.assertNotIn("mount", fixture.runner.report)

    def test_real_result_inspection_rejects_live_unit_changed_invocation_or_terminal_host_drift(self):
        for mutation in ("live unit", "invocation changed", "host drift", "fixture retained", "binary changed", "output changed"):
            with self.subTest(mutation=mutation):
                fixture = self.result_fixture()
                if mutation == "live unit":
                    fixture.core.require_unit_terminal.side_effect = OPERATOR.Failure("The owned cgroup remains live.")
                elif mutation == "invocation changed":
                    fixture.core.command.return_value = "b" * 32 + "\n"
                elif mutation == "host drift":
                    fixture.host = dict(fixture.host, loops_sha256="e" * 64)
                elif mutation == "fixture retained":
                    fixture.residual = True
                elif mutation == "binary changed":
                    fixture.binary = dict(fixture.binary, inode=999)
                else:
                    fixture.core.directory.return_value = {"device": 1, "inode": 999}
                with self.result_effects(fixture):
                    self.reject(fixture.runner.inspect_results)
                self.assertNotIn("mount", fixture.runner.report)


def main():
    global OPERATOR
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run memory-only guard tests through authorized root SSH.")
    if len(sys.argv) != 2:
        raise SystemExit("Usage: test-verify-bound-scan-root-mount.py OPERATOR_SOURCE")
    source = Path(sys.argv[1])
    raw = source.read_text()
    SOURCE_LINES[str(source)] = raw.splitlines(keepends=True)
    SOURCE_LINES[str(Path(__file__))] = Path(__file__).read_text().splitlines(keepends=True)
    OPERATOR = types.ModuleType("bound_scan_root_mount_operator")
    OPERATOR.__file__ = str(source)
    with EffectFence():
        exec(compile(raw, str(source), "exec"), OPERATOR.__dict__)
    result = unittest.TextTestRunner(verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(MountVerifierGuardTests)
    )
    if result.wasSuccessful():
        print("Memory-only guards: no external effect occurred inside the import and guard fences.")
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
