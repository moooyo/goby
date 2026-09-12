#!/usr/bin/env python3
"""Exercise the source44 candidate-upgrade guards with memory-only fixtures.

Run only in an authorized remote verification environment. The sibling operator
is the only project module loaded. Published helpers, client code, database
state, credentials, binaries, and assets are never loaded by these tests.
Every guard call runs behind filesystem, process, and network fences.
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
import types
import unittest
from unittest.mock import Mock, call, patch


sys.dont_write_bytecode = True
OPERATOR_PATH = Path(__file__).with_name("upgrade-client-notifications-source44.py")
OPERATOR_RAW = OPERATOR_PATH.read_bytes()
GUARD_RAW = Path(__file__).read_bytes()
OPERATOR_SHA256 = hashlib.sha256(OPERATOR_RAW).hexdigest()
GUARD_SHA256 = hashlib.sha256(GUARD_RAW).hexdigest()
SPEC = importlib.util.spec_from_file_location(
    "source44_upgrade_under_test",
    OPERATOR_PATH,
)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("The sibling source44 operator cannot be loaded.")
UPGRADE = importlib.util.module_from_spec(SPEC)
OPERATOR_CODE = SPEC.loader.source_to_code(OPERATOR_RAW, str(OPERATOR_PATH))
ACTIVE_FENCES = []


def denied(*_args, **_kwargs):
    for fence in ACTIVE_FENCES:
        fence.violations.append("external_effect")
    raise AssertionError("A pure source44 guard attempted an external effect.")


class EffectFence(contextlib.ExitStack):
    """Reject real effects while allowing in-memory validation and assertions."""

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
            (os, ("open", "read", "write", "stat", "lstat", "fstat", "readlink",
                  "listdir", "scandir", "mkdir", "makedirs", "remove", "unlink",
                  "rename", "replace", "rmdir", "chmod", "chown", "link",
                  "symlink", "truncate", "kill", "killpg", "system", "popen",
                  "fsync", "fdatasync", "fchmod", "fchown", "ftruncate",
                  "fork", "execv", "execve", "posix_spawn")),
            (Path, ("open", "read_bytes", "read_text", "write_bytes", "write_text",
                    "stat", "lstat", "resolve", "exists", "is_file", "is_dir",
                    "mkdir", "touch", "unlink", "rename", "replace", "rmdir",
                    "chmod", "iterdir", "glob", "rglob")),
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
            raise AssertionError("A guard attempted an external effect, even if it caught the denial.")
        return result


# Standard-library imports are already cached. The reviewed sibling code is
# read once through importlib before its module body executes behind the fence.
sys.modules[SPEC.name] = UPGRADE
with EffectFence():
    exec(OPERATOR_CODE, UPGRADE.__dict__)


def codec():
    def equal_json(left, right):
        return json.dumps(left, sort_keys=True, separators=(",", ":")) == json.dumps(
            right, sort_keys=True, separators=(",", ":")
        )

    return types.SimpleNamespace(
        equal_json=equal_json,
        MARKER="synthetic-client-fixture-v1",
        SCHEMA_27_SOURCE_MARKER="synthetic-schema27-source-v1",
        schema27_binding=lambda state: state.get("schema27_source", {}),
        PRODUCT_PACKAGES={"synthetic/base"} | {"synthetic/package" + str(index) for index in range(23)},
        PRODUCT_REQUIRED_TESTS={"TestRequiredExistingProduct"},
        PRODUCT_27_REQUIRED_TESTS={"TestRequiredSchema27Product"},
    )


def product_report(op):
    required = sorted(op.PRODUCT_REQUIRED_TESTS | op.PRODUCT_27_REQUIRED_TESTS | {
        "TestHTTPLibraryRefreshDurablyQueuesEveryLibraryBeyondScannerCapacity",
        "TestManagerRetriesCapacityAndIndependentScansBeyondTerminalPage",
    })
    passed = required + ["TestSyntheticCoverage" + str(index) for index in range(2002 - len(required))]
    return {
        "marker": "goby-client-backup-pair-m3e-v1",
        "status": "passed",
        "mode": "full",
        "schema": 27,
        "source": str(UPGRADE.SOURCE),
        "source_manifest_sha256": UPGRADE.MANIFEST_SHA,
        "catalog_sha256": UPGRADE.CATALOG_SHA,
        "run_id": UPGRADE.RUN_ID,
        "unit_exit": 0,
        "cleanup": {
            "unit_terminal": True,
            "hba_restored_exactly": True,
            "goby_backup_m3e_source_removed": True,
            "goby_backup_m3e_target_removed": True,
            "preexisting_catalog_unchanged": True,
            "receipt_saved": True,
        },
        "packages": sorted(op.PRODUCT_PACKAGES | {"github.com/moooyo/goby/internal/storagebinding"}),
        "tests": {"top_level_passes": 2002, "passed": passed, "failures": 0, "skips": 0},
        "binary": copy.deepcopy(UPGRADE.BINARY_REPORT),
    }


def terminal_report():
    return {
        "unit": "goby-library-changed-capacity-full-controller-v2.service",
        "state": {
            "MainPID": "0",
            "Result": "success",
            "ExecMainStatus": "0",
            "ControlGroup": "",
            "SubState": "exited",
            "InvocationID": "d18910a201d44bbfa9bbb77cd9cb8c25",
        },
        "recursive_cgroup_empty": True,
        "run_id": "20260912_032033_aeb954fd9c60",
        "report_sha256": "2d82c22f5d335314373cd042de5f8a75f66706e5dae87d7126ec05427c79fe16",
        "binary": {
            "bytes": 28357495,
            "path": "/opt/goby-test/exec-work-m3e/client-backup-run-20260912_032033_aeb954fd9c60/tmp/goby-linux-amd64",
            "sha256": "cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d",
        },
    }


def starting_state(op):
    return {
        "marker": op.MARKER,
        "schema": 27,
        "phase": "ready",
        "stage": "complete",
        "binary_sha256": UPGRADE.OLD_BINARY_SHA,
        "binary_identity": {"device": 7, "inode": 100},
        "runtime_sha256": UPGRADE.OLD_RUNTIME_SHA,
        "process": copy.deepcopy(UPGRADE.OLD_PROCESS),
        "viewer_id": UPGRADE.B_USER,
        "server_id": "synthetic-stable-server",
        "database": {"role": "synthetic-role", "name": "synthetic-database"},
        "credentials": {"path": "/synthetic/private.json", "sha256": "1" * 64},
        "binary_pending": False,
        "bootstrap_pending": False,
        "credentials_pending": False,
        "database_creation_pending": False,
        "role_creation_pending": False,
        "unit_pending": False,
        "start_pending": False,
        "viewer_pending": False,
        "schema27_source": {
            "marker": op.SCHEMA_27_SOURCE_MARKER,
            "schema": 27,
            "source": "/opt/goby-test/exec-work-m3e/source-attempt-32",
            "source_manifest_sha256": UPGRADE.OLD_MANIFEST_SHA,
            "catalog_sha256": UPGRADE.CATALOG_SHA,
            "migration_27_sha256": UPGRADE.MIGRATION_SHA,
        },
        "upgrade": {"phase": "complete", "source": "synthetic-source32-binary"},
        "upgrade_history": [
            {"phase": "complete", "id": "synthetic-previous-upgrade-one"},
            {"phase": "complete", "id": "synthetic-previous-upgrade-two"},
        ],
        "special_features_profile": {
            "phase": "complete",
            "receipt_path": str(UPGRADE.PROFILE),
            "receipt_sha256": UPGRADE.PROFILE_SHA,
            "preserved_profile_metadata": {"version": 3},
        },
        "media_root_extension": {"phase": "complete", "preserved_roots": ["synthetic-extras"]},
    }


def scope_snapshot():
    counts = {"sessions": 73, "devices": 62, "activity_entries": 163, "items": 22,
              "libraries": 4, "play_sessions": 26, "user_item_data": 7,
              "item_extra_resources": 4, "extra_reserved_paths": 3,
              "client_playback_references": 0, "encoding_jobs": 0}
    tables = {name: [{"id": name + "-" + str(index)} for index in range(count)]
              for name, count in counts.items()}
    tables["users"] = [{"id": UPGRADE.B_USER, "management_revision": 5},
                       {"id": "synthetic-other-user", "management_revision": 9}]
    return {"schema": 27, "database": {"tables": tables}}


def state_delta(op):
    before = starting_state(op)
    process = {"pid": 900001, "start_ticks": UPGRADE.OLD_PROCESS["start_ticks"] + 1000,
               "boot_id": UPGRADE.OLD_PROCESS["boot_id"]}
    receipt = {"marker": UPGRADE.MARKER, "id": UPGRADE.MARKER, "phase": "complete",
               "from_schema": 27, "to_schema": 27, "from_sha256": UPGRADE.OLD_BINARY_SHA,
               "to_sha256": UPGRADE.BINARY_SHA, "old_process": copy.deepcopy(before["process"]),
               "new_process": copy.deepcopy(process), "evidence_directory": str(UPGRADE.OUTPUT),
               "installed_binary_identity": {"device": 7, "inode": 200},
               "installed_binary_sha256": UPGRADE.BINARY_SHA}
    after = copy.deepcopy(before)
    after.update(
        phase="ready", stage="complete", binary_sha256=UPGRADE.BINARY_SHA,
        binary_identity={"device": 7, "inode": 200},
        process=copy.deepcopy(process),
        schema27_source={"marker": op.SCHEMA_27_SOURCE_MARKER, "schema": 27,
                         "source": str(UPGRADE.SOURCE), "source_manifest_sha256": UPGRADE.MANIFEST_SHA,
                         "catalog_sha256": UPGRADE.CATALOG_SHA, "migration_27_sha256": UPGRADE.MIGRATION_SHA},
        upgrade=copy.deepcopy(receipt),
        upgrade_history=copy.deepcopy(before["upgrade_history"]) + [copy.deepcopy(receipt)],
        notifications_upgrade={"marker": UPGRADE.MARKER, "phase": "complete",
                               "evidence_directory": str(UPGRADE.OUTPUT), "publication": UPGRADE.PUBLICATION,
                               "binary_sha256": UPGRADE.BINARY_SHA},
    )
    return before, after, process, receipt


class GuardTestCase(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())
        self.op = codec()

    def reject(self, callback):
        with self.assertRaises(UPGRADE.Failure):
            callback()


class ProductReportGuards(GuardTestCase):
    def test_source44_constants_identify_the_independently_reviewed_run(self):
        expected = {
            "SOURCE": "/opt/goby-test/exec-work-m3e/source-attempt-44",
            "MANIFEST_SHA": "c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b",
            "RUN_ID": "20260912_032033_aeb954fd9c60",
            "REPORT_SHA": "2d82c22f5d335314373cd042de5f8a75f66706e5dae87d7126ec05427c79fe16",
            "BINARY_SHA": "cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d",
            "CATALOG_SHA": "1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d",
            "BASELINE_SHA": "12278b4117f352b433c246fec0b53d7879700f37245f1c6999beea00587591fd",
        }
        for name, value in expected.items():
            with self.subTest(constant=name):
                self.assertEqual(str(getattr(UPGRADE, name)), value)
        self.assertEqual(UPGRADE.BINARY_REPORT, terminal_report()["binary"])

    def test_accepts_complete_exact_product_report_without_mutating_input(self):
        report = product_report(self.op)
        original = copy.deepcopy(report)
        UPGRADE.validate_product_report(self.op, report)
        self.assertEqual(report, original)

    def test_rejects_each_wrong_product_binding(self):
        substitutions = {
            "marker": "goby-client-backup-pair-unowned",
            "status": "failed",
            "mode": "focused",
            "schema": 26,
            "source": "/opt/goby-test/exec-work-m3e/source-attempt-32",
            "source_manifest_sha256": "0" * 64,
            "catalog_sha256": "0" * 64,
            "run_id": "20260912_000000_000000000000",
            "unit_exit": 1,
        }
        for key, value in substitutions.items():
            with self.subTest(field=key):
                report = product_report(self.op)
                report[key] = value
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_missing_required_product_fields(self):
        for key in product_report(self.op):
            with self.subTest(field=key):
                report = product_report(self.op)
                del report[key]
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_incomplete_or_truthy_cleanup(self):
        for key in product_report(self.op)["cleanup"]:
            for value in (False, 1, "true", None):
                with self.subTest(field=key, value=value):
                    report = product_report(self.op)
                    report["cleanup"][key] = value
                    self.reject(lambda: UPGRADE.validate_product_report(self.op, report))
            with self.subTest(missing=key):
                report = product_report(self.op)
                del report["cleanup"][key]
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_noninteger_product_and_test_totals(self):
        for key, value in (("schema", 27.0), ("unit_exit", False)):
            with self.subTest(field=key):
                report = product_report(self.op)
                report[key] = value
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))
        for key, values in {
            "top_level_passes": (2001, 2003, 2002.0, "2002", True),
            "failures": (1, -1, 0.0, "0", False),
            "skips": (1, -1, 0.0, "0", False),
        }.items():
            for value in values:
                with self.subTest(field=key, value=value):
                    report = product_report(self.op)
                    report["tests"][key] = value
                    self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_duplicate_or_missing_required_passes(self):
        report = product_report(self.op)
        report["tests"]["passed"][-1] = report["tests"]["passed"][0]
        self.reject(lambda: UPGRADE.validate_product_report(self.op, report))
        required_tests = self.op.PRODUCT_REQUIRED_TESTS | self.op.PRODUCT_27_REQUIRED_TESTS | {
            "TestHTTPLibraryRefreshDurablyQueuesEveryLibraryBeyondScannerCapacity",
            "TestManagerRetriesCapacityAndIndependentScansBeyondTerminalPage",
        }
        for required in required_tests:
            with self.subTest(required=required):
                report = product_report(self.op)
                report["tests"]["passed"].remove(required)
                report["tests"]["passed"].append("TestUnrelatedReplacement")
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_pass_list_length_that_disagrees_with_2002(self):
        for extra in (False, True):
            with self.subTest(extra=extra):
                report = product_report(self.op)
                if extra:
                    report["tests"]["passed"].append("TestUnexpectedAdditionalPass")
                else:
                    report["tests"]["passed"].pop()
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_extra_cleanup_claim_even_when_true(self):
        report = product_report(self.op)
        report["cleanup"]["unreviewed_cleanup"] = True
        self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_empty_or_nonstring_pass_names(self):
        for value in ("", None, 1, {"name": "TestFake", "status": "passed"}):
            with self.subTest(value=value):
                report = product_report(self.op)
                report["tests"]["passed"][-1] = value
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_missing_extra_or_duplicate_packages(self):
        for mode in ("missing_base", "missing_storagebinding", "extra", "duplicate"):
            with self.subTest(mode=mode):
                report = product_report(self.op)
                if mode == "missing_base":
                    report["packages"].remove("synthetic/base")
                elif mode == "missing_storagebinding":
                    report["packages"].remove("github.com/moooyo/goby/internal/storagebinding")
                elif mode == "extra":
                    report["packages"].append("synthetic/unexpected")
                else:
                    report["packages"].append(report["packages"][0])
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))

    def test_rejects_changed_binary_provenance(self):
        for key, value in (("path", "/tmp/goby-linux-amd64"), ("sha256", "0" * 64),
                           ("bytes", 28357494), ("bytes", 28357495.0)):
            with self.subTest(field=key, value=value):
                report = product_report(self.op)
                report["binary"][key] = value
                self.reject(lambda: UPGRADE.validate_product_report(self.op, report))


class TerminalGuards(GuardTestCase):
    def test_accepts_exact_terminal_receipt(self):
        document = terminal_report()
        original = copy.deepcopy(document)
        UPGRADE.validate_terminal(document)
        self.assertEqual(document, original)

    def test_rejects_changed_terminal_identity_or_result(self):
        for key, value in (("unit", "goby-unowned.service"), ("recursive_cgroup_empty", False),
                           ("recursive_cgroup_empty", 1), ("run_id", "wrong-run"),
                           ("report_sha256", "0" * 64)):
            with self.subTest(field=key, value=value):
                document = terminal_report()
                document[key] = value
                self.reject(lambda: UPGRADE.validate_terminal(document))
        for key, value in (("MainPID", "12"), ("Result", "failed"), ("ExecMainStatus", "1"),
                           ("ControlGroup", "/system.slice/unowned.service"), ("SubState", "running"),
                           ("InvocationID", "0" * 32)):
            with self.subTest(state=key):
                document = terminal_report()
                document["state"][key] = value
                self.reject(lambda: UPGRADE.validate_terminal(document))

    def test_rejects_missing_terminal_proof(self):
        for key in terminal_report():
            with self.subTest(field=key):
                document = terminal_report()
                del document[key]
                self.reject(lambda: UPGRADE.validate_terminal(document))
        for key in terminal_report()["state"]:
            with self.subTest(state=key):
                document = terminal_report()
                del document["state"][key]
                self.reject(lambda: UPGRADE.validate_terminal(document))

    def test_rejects_terminal_binary_from_another_build(self):
        for key, value in (("path", "/tmp/goby-linux-amd64"), ("sha256", "0" * 64),
                           ("bytes", 1), ("bytes", 28357495.0)):
            with self.subTest(field=key):
                document = terminal_report()
                document["binary"][key] = value
                self.reject(lambda: UPGRADE.validate_terminal(document))


class StartingStateGuards(GuardTestCase):
    def test_accepts_exact_completed_source32_state_without_mutating_it(self):
        state = starting_state(self.op)
        original = copy.deepcopy(state)
        binding = Mock(wraps=self.op.schema27_binding)
        self.op.schema27_binding = binding
        UPGRADE.validate_starting_state(self.op, state)
        binding.assert_called_once_with(state)
        self.assertEqual(state, original)

    def test_rejects_wrong_starting_identity_or_phase(self):
        substitutions = {"marker": "unowned-fixture", "schema": 26, "phase": "upgrading",
                         "stage": "pending", "binary_sha256": UPGRADE.BINARY_SHA,
                         "runtime_sha256": "0" * 64, "viewer_id": "synthetic-other-user"}
        for key, value in substitutions.items():
            with self.subTest(field=key):
                state = starting_state(self.op)
                state[key] = value
                self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))
        for key in ("marker", "schema", "phase", "stage", "binary_sha256", "runtime_sha256", "process", "viewer_id"):
            with self.subTest(missing=key):
                state = starting_state(self.op)
                del state[key]
                self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))

    def test_rejects_reused_or_noninteger_old_process(self):
        for key, value in (("pid", 1), ("pid", float(UPGRADE.OLD_PROCESS["pid"])),
                           ("start_ticks", UPGRADE.OLD_PROCESS["start_ticks"] + 1),
                           ("start_ticks", float(UPGRADE.OLD_PROCESS["start_ticks"])),
                           ("boot_id", "00000000-0000-0000-0000-000000000000")):
            with self.subTest(field=key, value=value):
                state = starting_state(self.op)
                state["process"][key] = value
                self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))
        state = starting_state(self.op)
        state["schema"] = 27.0
        self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))

    def test_rejects_pending_fields_and_missing_false_reservations(self):
        for key in ("binary_pending", "bootstrap_pending", "credentials_pending", "database_creation_pending",
                    "role_creation_pending", "unit_pending", "start_pending", "viewer_pending"):
            for value in (True, 0, None, "false", {}):
                with self.subTest(field=key, value=value):
                    state = starting_state(self.op)
                    state[key] = value
                    self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))
            with self.subTest(missing=key):
                state = starting_state(self.op)
                del state[key]
                self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))

    def test_rejects_any_prior_notifications_upgrade(self):
        for value in (None, False, {}, {"phase": "complete"}, {"phase": "failed"}):
            with self.subTest(value=value):
                state = starting_state(self.op)
                state["notifications_upgrade"] = value
                self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))

    def test_rejects_incomplete_prior_upgrade_and_positive_profile(self):
        for section, key, value in (
            ("upgrade", "phase", "started"),
            ("special_features_profile", "phase", "pending"),
            ("special_features_profile", "receipt_path", "/synthetic/unowned.json"),
            ("special_features_profile", "receipt_sha256", "0" * 64),
            ("media_root_extension", "phase", "pending"),
            ("schema27_source", "source", str(UPGRADE.SOURCE)),
            ("schema27_source", "source_manifest_sha256", UPGRADE.MANIFEST_SHA),
        ):
            with self.subTest(section=section, field=key):
                state = starting_state(self.op)
                state[section][key] = value
                self.reject(lambda: UPGRADE.validate_starting_state(self.op, state))


class ScopeSnapshotGuards(GuardTestCase):
    def setUp(self):
        super().setUp()
        self.restriction = types.SimpleNamespace(quiescent=Mock(return_value=None))
        self.profile = types.SimpleNamespace(validate_structure=Mock(return_value=None))
        self.state = starting_state(self.op)

    def validate(self, snapshot):
        return UPGRADE.validate_scope_snapshot(self.op, self.restriction, self.profile, snapshot, self.state)

    def test_accepts_latest_population_and_calls_both_prerequisite_guards(self):
        snapshot = scope_snapshot()
        original = copy.deepcopy(snapshot)
        self.validate(snapshot)
        self.profile.validate_structure.assert_called_once_with(self.op, snapshot, self.state)
        self.restriction.quiescent.assert_called_once_with(snapshot)
        self.assertEqual(snapshot, original)

    def test_propagates_positive_structure_and_quiescence_rejection(self):
        for dependency in (self.profile.validate_structure, self.restriction.quiescent):
            with self.subTest(dependency=dependency):
                dependency.side_effect = UPGRADE.Failure("Synthetic prerequisite rejection.")
                self.reject(lambda: self.validate(scope_snapshot()))
                dependency.side_effect = None

    def test_rejects_each_population_increment_and_decrement(self):
        for name, rows in scope_snapshot()["database"]["tables"].items():
            if name == "users":
                continue
            for delta in (1, -1) if rows else (1,):
                with self.subTest(table=name, delta=delta):
                    snapshot = scope_snapshot()
                    if delta > 0:
                        snapshot["database"]["tables"][name].append({"id": "synthetic-extra"})
                    else:
                        snapshot["database"]["tables"][name].pop()
                    self.reject(lambda: self.validate(snapshot))

    def test_rejects_noncurrent_or_noninteger_b_revision(self):
        for value in (3, 4, 6, 5.0, "5", True, None):
            with self.subTest(revision=value):
                snapshot = scope_snapshot()
                snapshot["database"]["tables"]["users"][0]["management_revision"] = value
                self.reject(lambda: self.validate(snapshot))

    def test_rejects_missing_duplicate_or_misattributed_b(self):
        for mode in ("missing", "duplicate", "misattributed"):
            with self.subTest(mode=mode):
                snapshot = scope_snapshot()
                users = snapshot["database"]["tables"]["users"]
                if mode == "missing":
                    users.pop(0)
                elif mode == "duplicate":
                    users.append(copy.deepcopy(users[0]))
                else:
                    users[0]["id"] = "synthetic-not-b"
                    users[1]["management_revision"] = 5
                self.reject(lambda: self.validate(snapshot))


class StateDeltaGuards(GuardTestCase):
    def validate(self, values):
        return UPGRADE.validate_state_delta(self.op, *values)

    def test_accepts_only_declared_same_schema_replacement(self):
        values = state_delta(self.op)
        original = copy.deepcopy(values)
        self.validate(values)
        self.assertEqual(values, original)

    def test_rejects_unrelated_top_level_addition_removal_and_change(self):
        for mode in ("add", "remove", "change"):
            with self.subTest(mode=mode):
                values = state_delta(self.op)
                after = values[1]
                if mode == "add":
                    after["unexpected"] = "synthetic-value"
                elif mode == "remove":
                    del after["server_id"]
                else:
                    after["server_id"] = "synthetic-other-server"
                self.reject(lambda: self.validate(values))

    def test_rejects_unrelated_nested_mutations_and_type_changes(self):
        for section, key, value in (("database", "role", "unowned-role"),
                                    ("credentials", "sha256", "0" * 64),
                                    ("special_features_profile", "receipt_sha256", "0" * 64),
                                    ("media_root_extension", "preserved_roots", ["unowned-root"])):
            with self.subTest(section=section):
                values = state_delta(self.op)
                values[1][section][key] = value
                self.reject(lambda: self.validate(values))
        for key, value in (("schema", 27.0), ("binary_pending", 0), ("runtime_sha256", "0" * 64)):
            with self.subTest(field=key):
                values = state_delta(self.op)
                values[1][key] = value
                self.reject(lambda: self.validate(values))

    def test_rejects_changed_final_phase_binary_and_catalog_binding(self):
        for key, value in (("phase", "upgrading"), ("stage", "started"), ("binary_sha256", UPGRADE.OLD_BINARY_SHA)):
            with self.subTest(field=key):
                values = state_delta(self.op)
                values[1][key] = value
                self.reject(lambda: self.validate(values))
        for key in state_delta(self.op)[1]["schema27_source"]:
            with self.subTest(binding=key):
                values = state_delta(self.op)
                values[1]["schema27_source"][key] = "synthetic-wrong-binding"
                self.reject(lambda: self.validate(values))

    def test_rejects_stale_other_boot_or_invalid_new_process(self):
        for key, value in (("pid", 1), ("pid", 900001.0),
                           ("start_ticks", UPGRADE.OLD_PROCESS["start_ticks"]),
                           ("start_ticks", str(UPGRADE.OLD_PROCESS["start_ticks"] + 1000)),
                           ("boot_id", "00000000-0000-0000-0000-000000000000")):
            with self.subTest(field=key, value=value):
                before, after, process, receipt = state_delta(self.op)
                process[key] = value
                after["process"] = copy.deepcopy(process)
                receipt["new_process"] = copy.deepcopy(process)
                after["upgrade"] = copy.deepcopy(receipt)
                after["upgrade_history"][-1] = copy.deepcopy(receipt)
                self.reject(lambda: self.validate((before, after, process, receipt)))
        before, after, process, receipt = state_delta(self.op)
        after["process"] = copy.deepcopy(before["process"])
        self.reject(lambda: self.validate((before, after, process, receipt)))

    def test_rejects_reused_missing_or_unbound_installed_identity(self):
        for identity in ({"device": 7, "inode": 100}, {"device": 0, "inode": 200},
                         {"device": 7, "inode": 200.0}, {"device": 7, "inode": True},
                         {"device": 7}, {"device": 7, "inode": 200, "bytes": 1}, None):
            with self.subTest(identity=identity):
                values = state_delta(self.op)
                values[1]["binary_identity"] = identity
                self.reject(lambda: self.validate(values))
        values = state_delta(self.op)
        values[1]["binary_identity"]["inode"] = 201
        self.reject(lambda: self.validate(values))

    def test_rejects_rewritten_reordered_or_nonappend_only_history(self):
        for mode in ("rewrite", "reorder", "truncate", "missing_new", "duplicate_new", "wrong_new"):
            with self.subTest(mode=mode):
                values = state_delta(self.op)
                history = values[1]["upgrade_history"]
                if mode == "rewrite":
                    history[0]["phase"] = "rewritten"
                elif mode == "reorder":
                    history[0], history[1] = history[1], history[0]
                elif mode == "truncate":
                    del history[0]
                elif mode == "missing_new":
                    history.pop()
                elif mode == "duplicate_new":
                    history.append(copy.deepcopy(history[-1]))
                else:
                    history[-1]["phase"] = "failed"
                self.reject(lambda: self.validate(values))

    def test_rejects_mismatched_receipt_even_if_history_repeats_it(self):
        for key, value in (("marker", "unowned-upgrade"), ("phase", "failed"),
                           ("from_schema", 26), ("to_schema", 27.0),
                           ("from_sha256", "0" * 64), ("to_sha256", UPGRADE.OLD_BINARY_SHA),
                           ("old_process", {}), ("new_process", {}),
                           ("installed_binary_sha256", UPGRADE.OLD_BINARY_SHA),
                           ("installed_binary_identity", {"device": 7, "inode": 999})):
            with self.subTest(field=key):
                before, after, process, receipt = state_delta(self.op)
                receipt[key] = value
                after["upgrade"] = copy.deepcopy(receipt)
                after["upgrade_history"][-1] = copy.deepcopy(receipt)
                self.reject(lambda: self.validate((before, after, process, receipt)))

    def test_rejects_missing_or_unowned_completion_marker(self):
        values = state_delta(self.op)
        del values[1]["notifications_upgrade"]
        self.reject(lambda: self.validate(values))
        for key in state_delta(self.op)[1]["notifications_upgrade"]:
            with self.subTest(field=key):
                values = state_delta(self.op)
                values[1]["notifications_upgrade"][key] = "synthetic-unowned"
                self.reject(lambda: self.validate(values))


class ServiceFenceGuards(GuardTestCase):
    UNIT = "goby-client-m3e.service"

    def command(self, action, unit=None):
        return ["/usr/bin/systemctl", action, self.UNIT if unit is None else unit]

    def test_allows_one_stop_then_one_start_in_matching_phases(self):
        fence = UPGRADE.ServiceFence()
        fence.stage = "stop_requested"
        fence.approve(self.command("stop"), self.UNIT)
        fence.stage = "start_requested"
        fence.approve(self.command("start"), self.UNIT)

    def test_read_only_show_does_not_reserve_a_mutation(self):
        fence = UPGRADE.ServiceFence()
        initial = copy.deepcopy(fence.reserved)
        for _ in range(3):
            fence.approve(self.command("show"), self.UNIT)
        self.assertEqual(fence.reserved, initial)
        self.assertEqual(fence.stage, "preflight")
        fence.stage = "stop_requested"
        fence.approve(self.command("stop"), self.UNIT)

    def test_rejects_start_before_stop(self):
        fence = UPGRADE.ServiceFence()
        fence.stage = "start_requested"
        self.reject(lambda: fence.approve(self.command("start"), self.UNIT))
        self.assertEqual(fence.reserved, set())

    def test_rejects_duplicate_mutations(self):
        fence = UPGRADE.ServiceFence()
        fence.stage = "stop_requested"
        fence.approve(self.command("stop"), self.UNIT)
        self.reject(lambda: fence.approve(self.command("stop"), self.UNIT))
        fence.stage = "start_requested"
        fence.approve(self.command("start"), self.UNIT)
        self.reject(lambda: fence.approve(self.command("start"), self.UNIT))
        fence.stage = "stop_requested"
        self.reject(lambda: fence.approve(self.command("stop"), self.UNIT))

    def test_rejects_wrong_phase_without_consuming_authority(self):
        for stage in ("preflight", "ready", "stopped", "start_requested", "complete"):
            with self.subTest(stage=stage):
                fence = UPGRADE.ServiceFence()
                fence.stage = stage
                self.reject(lambda: fence.approve(self.command("stop"), self.UNIT))
                self.assertEqual(fence.reserved, set())
                fence.stage = "stop_requested"
                fence.approve(self.command("stop"), self.UNIT)

    def test_rejects_start_in_wrong_phase_after_owned_stop(self):
        for stage in ("preflight", "stop_requested", "stopped", "ready", "complete"):
            with self.subTest(stage=stage):
                fence = UPGRADE.ServiceFence()
                fence.stage = "stop_requested"
                fence.approve(self.command("stop"), self.UNIT)
                fence.stage = stage
                self.reject(lambda: fence.approve(self.command("start"), self.UNIT))
                self.assertEqual(fence.reserved, {"stop"})
                fence.stage = "start_requested"
                fence.approve(self.command("start"), self.UNIT)

    def test_rejects_other_mutations_units_and_command_shapes(self):
        commands = [self.command(action) for action in ("restart", "reload", "enable", "disable", "kill")]
        commands.extend((self.command("stop", "goby-primary.service"),
                         ["systemctl", "stop", self.UNIT],
                         ["/usr/bin/systemctl", "stop", self.UNIT, "--no-block"],
                         ["/usr/bin/systemctl", "stop", self.UNIT, "goby-primary.service"]))
        for command in commands:
            with self.subTest(command=command):
                fence = UPGRADE.ServiceFence()
                fence.stage = "stop_requested"
                self.reject(lambda: fence.approve(command, self.UNIT))
                self.assertEqual(fence.reserved, set())


class ScopedReadinessGuards(GuardTestCase):
    def setUp(self):
        super().setUp()
        self.candidate = state_delta(self.op)[1]
        self.saved = {}
        self.op.verify_service = Mock(return_value=copy.deepcopy(self.candidate["process"]))
        self.op.precise_json = Mock(side_effect=json.loads)
        self.op.create = Mock(side_effect=denied)
        self.op.NativeAPI = Mock(side_effect=denied)

        def save(name, document):
            self.assertNotIn(name, self.saved)
            self.saved[name] = copy.deepcopy(document)
            return {"path": str(UPGRADE.OUTPUT / name)}

        self.run = types.SimpleNamespace(op=self.op, save=Mock(side_effect=save))
        self.http = self.enterContext(patch.object(UPGRADE.http.client, "HTTPConnection"))
        self.reader = UPGRADE.ScopedReadiness(self.run, self.candidate)
        self.responses([(b'{"Status":"ready"}', 200, None),
                        (b'{"Id":"synthetic-stable-server","ProductName":"Goby"}', 200, None)])

    def tearDown(self):
        self.op.create.assert_not_called()
        self.op.NativeAPI.assert_not_called()

    def responses(self, specifications):
        self.connections = []
        self.response_objects = []
        for raw, status, cookie in specifications:
            response = Mock()
            response.status = status
            response.read.return_value = raw
            response.getheader.return_value = cookie
            connection = Mock()
            connection.request.return_value = None
            connection.getresponse.return_value = response
            connection.close.return_value = None
            self.response_objects.append(response)
            self.connections.append(connection)
        self.http.side_effect = list(self.connections)

    def assert_result(self, label, route, raw, status=200, cookie=False):
        self.assertEqual(self.saved["http-" + label + "-result.json"], {
            "method": "GET", "path": route, "status": status,
            "body_sha256": hashlib.sha256(raw).hexdigest(), "body_hex": raw.hex(),
            "bytes": len(raw), "cookie_present": cookie,
        })

    def test_completes_exact_two_public_gets_with_scoped_evidence(self):
        self.assertEqual(self.reader.requests, 0)
        self.assertIsNone(self.reader.cookie)
        self.assertIsNone(self.reader.csrf)
        self.assertEqual(self.reader.request("/readyz"), {"Status": "ready"})
        self.assertEqual(self.reader.request("/emby/System/Info/Public"), {
            "Id": "synthetic-stable-server", "ProductName": "Goby"})
        self.assertEqual(self.reader.requests, 2)
        self.assertIsNone(self.reader.cookie)
        self.assertIsNone(self.reader.csrf)
        self.assertEqual(self.http.call_args_list, [call("127.0.0.1", 18198, timeout=15)] * 2)
        self.assertEqual(self.op.verify_service.call_args_list, [call(self.candidate)] * 4)
        self.assertEqual(list(self.saved), ["http-readyz-intent.json", "http-readyz-result.json",
                                           "http-public-info-intent.json", "http-public-info-result.json"])
        for index, (label, route) in enumerate((("readyz", "/readyz"), ("public-info", "/emby/System/Info/Public"))):
            self.assertEqual(self.saved["http-" + label + "-intent.json"], {
                "method": "GET", "path": route, "request": index + 1, "authenticated": False})
            self.connections[index].request.assert_called_once_with(
                "GET", route, None, {"Accept": "application/json", "Origin": "http://127.0.0.1:18196"})
            self.response_objects[index].read.assert_called_once_with((2 << 20) + 1)
            self.response_objects[index].getheader.assert_called_once_with("Set-Cookie")
            self.connections[index].close.assert_called_once_with()
        self.assert_result("readyz", "/readyz", b'{"Status":"ready"}')
        self.assert_result("public-info", "/emby/System/Info/Public",
                           b'{"Id":"synthetic-stable-server","ProductName":"Goby"}')

    def test_retains_503_body_and_closes_without_using_legacy_writer(self):
        raw = b'{"Error":"temporarily unavailable"}'
        self.responses([(raw, 503, None)])
        self.reject(lambda: self.reader.request("/readyz"))
        self.assertEqual(self.reader.requests, 1)
        self.assert_result("readyz", "/readyz", raw, status=503)
        self.connections[0].close.assert_called_once_with()
        self.op.precise_json.assert_not_called()
        self.assertEqual(self.op.verify_service.call_count, 1)
        self.reject(lambda: self.reader.request("/readyz"))
        self.assertEqual(self.http.call_count, 1)
        self.assertEqual(list(self.saved), ["http-readyz-intent.json", "http-readyz-result.json"])

    def test_rejects_wrong_first_route_before_any_effect(self):
        for route in ("/emby/System/Info/Public", "/readyz?probe=1", "/emby/Users/Me", "http://example.invalid/readyz"):
            with self.subTest(route=route):
                self.reject(lambda: self.reader.request(route))
        self.assertEqual(self.reader.requests, 0)
        self.http.assert_not_called()
        self.run.save.assert_not_called()
        self.op.verify_service.assert_not_called()

    def test_rejects_repeat_and_third_attempt_before_another_connection(self):
        self.reader.request("/readyz")
        self.reject(lambda: self.reader.request("/readyz"))
        self.assertEqual(self.http.call_count, 1)
        self.assertEqual(self.reader.requests, 1)
        self.reader.request("/emby/System/Info/Public")
        for route in ("/readyz", "/emby/System/Info/Public"):
            with self.subTest(route=route):
                self.reject(lambda: self.reader.request(route))
        self.assertEqual(self.http.call_count, 2)
        self.assertEqual(self.reader.requests, 2)
        self.assertEqual(self.run.save.call_count, 4)

    def test_rejects_set_cookie_including_empty_header_and_preserves_body(self):
        raw = b'{"Status":"ready"}'
        for cookie in ("synthetic-cookie=unowned", ""):
            with self.subTest(cookie=cookie):
                self.saved.clear()
                self.reader = UPGRADE.ScopedReadiness(self.run, self.candidate)
                self.responses([(raw, 200, cookie)])
                self.reject(lambda: self.reader.request("/readyz"))
                self.assert_result("readyz", "/readyz", raw, cookie=True)
                self.connections[0].close.assert_called_once_with()
                self.assertIsNone(self.reader.cookie)
                self.assertIsNone(self.reader.csrf)
        self.op.precise_json.assert_not_called()

    def test_rejects_oversize_body_after_retaining_only_the_bounded_read(self):
        raw = b"x" * ((2 << 20) + 1)
        self.responses([(raw, 200, None)])
        self.reject(lambda: self.reader.request("/readyz"))
        self.response_objects[0].read.assert_called_once_with((2 << 20) + 1)
        self.assert_result("readyz", "/readyz", raw)
        self.assertEqual(self.reader.requests, 1)
        self.connections[0].close.assert_called_once_with()
        self.op.precise_json.assert_not_called()

    def test_closes_connection_on_dispatch_response_and_read_failure(self):
        for failure_point in ("request", "getresponse", "read"):
            with self.subTest(failure_point=failure_point):
                self.saved.clear()
                self.reader = UPGRADE.ScopedReadiness(self.run, self.candidate)
                self.responses([(b"", 200, None)])
                target = self.response_objects[0].read if failure_point == "read" else getattr(self.connections[0], failure_point)
                target.side_effect = OSError("Synthetic connection failure.")
                with self.assertRaises(OSError):
                    self.reader.request("/readyz")
                self.assertEqual(self.reader.requests, 1)
                self.connections[0].close.assert_called_once_with()
                self.assertEqual(list(self.saved), ["http-readyz-intent.json"])
        self.op.precise_json.assert_not_called()

    def test_rejects_changed_candidate_before_dispatch_or_after_response(self):
        self.op.verify_service.return_value = {"pid": 1}
        self.reject(lambda: self.reader.request("/readyz"))
        self.http.assert_not_called()
        self.run.save.assert_not_called()
        self.assertEqual(self.reader.requests, 0)
        self.op.verify_service.side_effect = [copy.deepcopy(self.candidate["process"]), {"pid": 1}]
        self.reject(lambda: self.reader.request("/readyz"))
        self.assertEqual(self.reader.requests, 1)
        self.connections[0].close.assert_called_once_with()
        self.assert_result("readyz", "/readyz", b'{"Status":"ready"}')
        self.op.precise_json.assert_not_called()

    def test_journal_failure_consumes_attempt_without_dispatching(self):
        self.run.save.side_effect = UPGRADE.Failure("Synthetic scoped journal failure.")
        self.reject(lambda: self.reader.request("/readyz"))
        self.assertEqual(self.reader.requests, 1)
        self.http.assert_not_called()
        self.reject(lambda: self.reader.request("/readyz"))
        self.http.assert_not_called()


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(stream=sys.stderr, verbosity=2).run(suite)
    passed = result.wasSuccessful() and not result.skipped
    report = {
        "suite": "client-notifications-source44-upgrade-guards",
        "status": "passed" if passed else "failed",
        "operator_sha256": OPERATOR_SHA256,
        "guard_sha256": GUARD_SHA256,
        "test_count": result.testsRun,
        "failures": len(result.failures),
        "errors": len(result.errors),
        "skips": len(result.skipped),
    }
    sys.stdout.write(json.dumps(report, sort_keys=True, separators=(",", ":")) + "\n")
    raise SystemExit(0 if passed else 1)
