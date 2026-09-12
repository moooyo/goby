#!/usr/bin/env python3
"""Exercise backup runner guards in memory, only through authorized root SSH."""

from __future__ import annotations

import contextlib
import copy
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import signal
import socket
import stat
import subprocess
import sys
import types
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
RUNNER = None
SOURCE_LINES = {}
BASE_HBA = b"""# Peer administration and explicitly named loopback SCRAM test logins only.
local all postgres peer
local all all reject
host goby_test goby_test 127.0.0.1/32 scram-sha-256
host goby_client_m3e goby_client_m3e 127.0.0.1/32 scram-sha-256
host all all 0.0.0.0/0 reject
host all all ::0/0 reject
"""


class EffectFence(contextlib.ExitStack):
    def __enter__(self):
        super().__enter__()
        import builtins
        import fcntl
        import pwd
        self.violations = []
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None: SOURCE_LINES.get(str(filename), [])))
        targets = ((builtins, ("open",)), (io, ("open", "FileIO")),
            (os, ("open", "close", "fdopen", "fstat", "stat", "lstat", "mkdir", "chmod", "chown", "fchown", "fsync",
                  "replace", "unlink", "remove", "rmdir", "umask", "kill", "system", "popen")),
            (Path, ("lstat", "stat", "exists", "resolve", "readlink", "read_bytes", "read_text", "write_bytes", "write_text",
                    "mkdir", "iterdir", "rglob", "unlink")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")), (pwd, ("getpwnam",)),
            (fcntl, ("flock",)), (signal, ("signal", "pidfd_send_signal")))
        for owner, names in targets:
            for name in names:
                label = owner.__name__ + "." + name
                def denied(*_args, _label=label, **_kwargs):
                    self.violations.append(_label)
                    raise AssertionError("Unexpected external effect: " + _label)
                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *args):
        super().__exit__(*args)
        if self.violations:
            raise AssertionError("External effects attempted: " + ", ".join(self.violations))


class RunnerGuardTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())
        self.output = self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.tag = RUNNER.MARKER + ":20260911_010203_abcdef012345"
        self.unit = "goby-client-backup-20260911-010203-abcdef012345.service"
        self.args = types.SimpleNamespace(source=RUNNER.WORK / "source-attempt-05", manifest_sha256="a" * 64,
                                          schema=24, mode="targeted", package=[], run="")
        self.role = {"oid": 17001, "tag": self.tag, "login": True, "super": False, "createdb": False,
            "createrole": False, "replication": False, "bypass": False, "inherit": False, "limit": 12,
            "config": None, "valid_until": None, "scram": True}
        self.database = {"oid": 18001, "owner_oid": 17001, "tag": self.tag, "allow": True, "template": False,
            "limit": -1, "encoding": "UTF8", "acl": [{"grantor": 17001, "grantee": 17001,
                "privilege_type": privilege, "is_grantable": False} for privilege in ("CONNECT", "CREATE", "TEMPORARY")]}
        self.pair = {"name": RUNNER.NAMES[0], "role_oid": 17001, "database_oid": 18001,
                     "database_fingerprint": copy.deepcopy(self.database), "phase": "owned", "public": {"oid": 2200}, "casts": []}
        self.rows = {"role": self.role, "database": self.database}

    def replace(self, owner, name, value):
        return self.enterContext(patch.object(owner, name, value))

    def reject(self, action):
        with self.assertRaises(RUNNER.Failure):
            action()

    def test_arguments_accept_canonical_numbered_full_and_targeted_sources(self):
        for number in ("01", "04", "05", "07", "09", "10", "99", "100", "1000"):
            self.args.source = RUNNER.WORK / ("source-attempt-" + number)
            self.args.mode, self.args.package, self.args.run = "targeted", [], ""
            RUNNER.validate_arguments(self.args)
            self.args.package = ["./internal/backuppg", "./internal/database"]
            self.args.run = "TestPostgreSQL|TestMigrate"
            RUNNER.validate_arguments(self.args)
            self.args.mode, self.args.package, self.args.run = "full", [], ""
            RUNNER.validate_arguments(self.args)

    def test_arguments_reject_noncanonical_source_numbers(self):
        for number in ("", "0", "00", "7", "007", "010", "000100", "-07", "+07", "7.0", "07x", "07 ", "\u0660\u0667"):
            self.args.source = RUNNER.WORK / ("source-attempt-" + number)
            self.reject(lambda: RUNNER.validate_arguments(self.args))

    def test_arguments_reject_path_traversal_external_sources_and_shell_controls(self):
        for field, value in (("source", Path("source-attempt-07")),
                ("source", RUNNER.WORK / "../source-attempt-07"),
                ("source", RUNNER.WORK / "nested/../source-attempt-07"),
                ("source", RUNNER.WORK / "source-attempt-07/.."),
                ("source", RUNNER.WORK / "nested/source-attempt-07"),
                ("source", Path("/tmp/source-attempt-07")),
                ("source", RUNNER.WORK.with_name("exec-work-m3e-other") / "source-attempt-07"),
                ("manifest_sha256", "bad"), ("package", ["./internal/../server"]), ("package", ["./..."]),
                ("package", ["-exec=foreign"]), ("run", "Test\nforeign")):
            arguments = copy.copy(self.args)
            setattr(arguments, field, value)
            self.reject(lambda arguments=arguments: RUNNER.validate_arguments(arguments))

    def test_arguments_require_exact_lowercase_sha256_manifest_digest(self):
        for digest in ("", "a" * 63, "a" * 65, "A" * 64, "g" * 64, "a" * 64 + "\n"):
            self.args.manifest_sha256 = digest
            self.reject(lambda: RUNNER.validate_arguments(self.args))

    def test_arguments_only_accept_explicit_supported_schema_versions(self):
        for schema in (24, 25, 26, 27, 28):
            self.args.schema = schema
            RUNNER.validate_arguments(self.args)
        for schema in (None, True, False, "25", "26", "27", "28", 0, 1, 23, 29, 25.0, 26.0, 27.0, 28.0):
            self.args.schema = schema
            self.reject(lambda: RUNNER.validate_arguments(self.args))

    def test_catalog_arguments_require_explicit_schema25_through28_without_test_selectors(self):
        self.args.mode = "catalog"
        self.reject(lambda: RUNNER.validate_arguments(self.args))
        for schema in (25, 26, 27, 28):
            self.args.schema, self.args.run, self.args.package = schema, "", []
            RUNNER.validate_arguments(self.args)
            self.args.run = "TestOne"
            self.reject(lambda: RUNNER.validate_arguments(self.args))
            self.args.run, self.args.package = "", ["./internal/backuppg"]
            self.reject(lambda: RUNNER.validate_arguments(self.args))

    def test_cli_requires_manifest_digest_before_any_external_effect(self):
        self.replace(RUNNER.argparse, "_", lambda message: message)
        initialize = RUNNER.argparse.HelpFormatter.__init__
        def fixed_width(formatter, *args, **kwargs):
            initialize(formatter, *args, **dict(kwargs, width=80))
        self.replace(RUNNER.argparse.HelpFormatter, "__init__", fixed_width)
        error = self.enterContext(contextlib.redirect_stderr(io.StringIO()))
        with self.assertRaises(SystemExit) as caught:
            RUNNER.main(["--source", str(RUNNER.WORK / "source-attempt-07"), "--mode", "full"])
        self.assertEqual(caught.exception.code, 2)
        self.assertIn("--manifest-sha256", error.getvalue())

    def test_cli_defaults_to_schema24_and_requires_explicit25_through28_for_catalog_mode(self):
        self.replace(RUNNER.argparse, "_", lambda message: message)
        initialize = RUNNER.argparse.HelpFormatter.__init__
        def fixed_width(formatter, *args, **kwargs):
            initialize(formatter, *args, **dict(kwargs, width=80))
        self.replace(RUNNER.argparse.HelpFormatter, "__init__", fixed_width)
        self.replace(signal, "signal", Mock())
        factory = self.replace(RUNNER, "Runner", Mock())
        factory.return_value.run_all.return_value = 0
        base = ["--source", str(self.args.source), "--manifest-sha256", self.args.manifest_sha256]
        self.assertEqual(RUNNER.main(base + ["--mode", "full"]), 0)
        self.assertEqual(factory.call_args.args[0].schema, 24)
        factory.reset_mock()
        self.reject(lambda: RUNNER.main(base + ["--mode", "catalog"]))
        factory.assert_not_called()
        for schema in (25, 26, 27, 28):
            factory.reset_mock()
            self.assertEqual(RUNNER.main(base + ["--mode", "catalog", "--schema", str(schema)]), 0)
            self.assertEqual((factory.call_args.args[0].schema, factory.call_args.args[0].mode), (schema, "catalog"))
        for mode in ("full", "targeted"):
            factory.reset_mock()
            self.assertEqual(RUNNER.main(base + ["--mode", mode, "--schema", "28"]), 0)
            self.assertEqual((factory.call_args.args[0].schema, factory.call_args.args[0].mode), (28, mode))

    def test_cli_rejects_unsupported_and_noninteger_schema_before_runner_construction(self):
        self.replace(RUNNER.argparse, "_", lambda message: message)
        initialize = RUNNER.argparse.HelpFormatter.__init__
        def fixed_width(formatter, *args, **kwargs):
            initialize(formatter, *args, **dict(kwargs, width=80))
        self.replace(RUNNER.argparse.HelpFormatter, "__init__", fixed_width)
        self.enterContext(contextlib.redirect_stderr(io.StringIO()))
        factory = self.replace(RUNNER, "Runner", Mock())
        base = ["--source", str(self.args.source), "--manifest-sha256", self.args.manifest_sha256, "--mode", "catalog"]
        for schema in ("23", "29", "0", "true", "false", "26.0", "27.0", "28.0"):
            with self.assertRaises(SystemExit) as caught:
                RUNNER.main(base + ["--schema", schema])
            self.assertEqual(caught.exception.code, 2)
        factory.assert_not_called()

    def test_historical_catalog_pins_and_reviewed_schema28_identity_are_fixed(self):
        self.assertEqual(RUNNER.HISTORICAL_CATALOG_SHA256, {
            23: "de85f4917dd7409e7e7bed20c7cbe63f0d7afe68f6ed72faff6b5bbb598ed00b",
            24: "6ba8a30d7648f3fdd73f977cc5d2aafac39232704542f28f20f0898d93ef575c",
            25: "e269a7eb6b31d2eb3fff734896ca074a6113761f321f2f23f4b07441e7dc617b",
            26: "e02c46a49dd4200bb67954f70ca0bbe90b3ef99d8821ea56a97e3dcb97e696de",
            27: "1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d",
        })
        self.assertEqual(RUNNER.MIGRATION_26_NAME, "0026_theme_owners.sql")
        self.assertEqual(RUNNER.MIGRATION_27_NAME, "0027_movie_extras.sql")
        self.assertEqual(RUNNER.MIGRATION_28_NAME, "0028_storage_root_bindings.sql")
        self.assertEqual(RUNNER.CATALOG_TABLE_COUNTS, {23: 29, 24: 30, 25: 30, 26: 33, 27: 35, 28: 35})
        self.assertEqual(RUNNER.BOOTSTRAP_MIGRATIONS, {25: RUNNER.MIGRATION_25_NAME,
            26: RUNNER.MIGRATION_26_NAME, 27: RUNNER.MIGRATION_27_NAME, 28: RUNNER.MIGRATION_28_NAME})
        for schema in (23, 24, 25, 26, 27, 28):
            self.assertEqual(RUNNER.catalog_name(schema), f"internal/backuppg/catalogs/schema-{schema}-postgresql-17.json")
        for schema in (None, True, False, "26", "27", "28", 22, 29, 26.0, 27.0, 28.0):
            self.reject(lambda: RUNNER.catalog_name(schema))

    def test_runner_constructor_binds_output_and_report_to_selected_schema(self):
        self.replace(RUNNER.time, "strftime", lambda *args: "20260911_010203")
        self.replace(RUNNER.time, "gmtime", lambda: ())
        self.replace(RUNNER.secrets, "token_hex", lambda length: "abcdef012345")
        for schema in (24, 25, 26, 27):
            self.args.schema = schema
            runner = RUNNER.Runner(self.args)
            self.assertEqual(runner.catalog_output, runner.output / f"schema-{schema}-postgresql-17.json")
            self.assertEqual(runner.report["schema"], schema)
            self.assertIsNone(runner.catalog_artifact)

    def test_full_mode_cannot_narrow_packages_or_cases(self):
        self.args.mode = "full"
        self.args.run = "TestOne"
        self.reject(lambda: RUNNER.validate_arguments(self.args))
        self.args.run, self.args.package = "", ["./internal/backuppg"]
        self.reject(lambda: RUNNER.validate_arguments(self.args))

    def test_duplicate_control_fields_are_rejected(self):
        self.reject(lambda: RUNNER.decode('{"marker":"owned","marker":"foreign"}'))

    def test_hba_adds_only_two_exact_loopback_scram_identities(self):
        actual = RUNNER.temporary_hba(BASE_HBA, self.tag, BASE_HBA)
        self.assertTrue(actual.endswith(BASE_HBA))
        added = actual[:-len(BASE_HBA)].decode().splitlines()
        self.assertEqual([line for line in added if not line.startswith("#")],
                         [f"host {name} {name} 127.0.0.1/32 scram-sha-256" for name in RUNNER.NAMES])
        self.assertEqual(actual.count(b"goby_client_m3e"), 2)
        self.assertNotIn(b"5432", actual)

    def test_hba_refuses_modified_original_missing_newline_and_invalid_tag(self):
        for original in (BASE_HBA + b"# foreign\n", BASE_HBA.replace(b"scram-sha-256", b"trust"), BASE_HBA[:-1]):
            self.reject(lambda original=original: RUNNER.temporary_hba(original, self.tag, BASE_HBA))
        self.reject(lambda: RUNNER.temporary_hba(BASE_HBA, self.tag + "\nhost all all 0/0 trust", BASE_HBA))

    def test_admin_connector_is_pinned_to_15432_and_allowed_databases(self):
        called = self.replace(RUNNER, "command", Mock(return_value="safe"))
        self.assertEqual(RUNNER.pg("SELECT 1", RUNNER.NAMES[0]), "safe")
        arguments = called.call_args.args[0]
        self.assertEqual(arguments[arguments.index("-p") + 1], "15432")
        self.assertEqual(arguments[arguments.index("-h") + 1], RUNNER.SOCKET)
        self.reject(lambda: RUNNER.pg("SELECT 1", "goby_client_m3e"))
        self.reject(lambda: RUNNER.pg("SELECT 1", "goby_test"))
        self.assertEqual(called.call_count, 1)

    def test_pair_requires_exact_both_oids_tags_and_restricted_role(self):
        RUNNER.validate_pair_rows(self.pair, self.rows, self.tag)
        for key, value in (("oid", 17002), ("tag", "foreign"), ("super", True), ("createdb", True),
                ("createrole", True), ("inherit", True), ("replication", True), ("bypass", True),
                ("scram", False), ("config", ["search_path=foreign"]), ("limit", -1)):
            rows = copy.deepcopy(self.rows)
            rows["role"][key] = value
            self.reject(lambda rows=rows: RUNNER.validate_pair_rows(self.pair, rows, self.tag))
        for key, value in (("oid", 18002), ("tag", None), ("owner_oid", 17002), ("allow", False), ("template", True)):
            rows = copy.deepcopy(self.rows)
            rows["database"][key] = value
            self.reject(lambda rows=rows: RUNNER.validate_pair_rows(self.pair, rows, self.tag))

    def test_pair_observation_uses_role_view_for_configuration_and_only_boolean_scram_projection(self):
        query = self.replace(RUNNER, "pg", Mock(return_value='{"role":null,"database":null}'))
        self.assertEqual(RUNNER.pair_rows(RUNNER.NAMES[0]), {"role": None, "database": None})
        sql = query.call_args.args[0]
        self.assertIn("FROM pg_roles r", sql)
        self.assertIn("a.rolpassword LIKE 'SCRAM-SHA-256$%'", sql)
        self.assertNotIn("FROM pg_authid WHERE rolname", sql)
        self.assertIn("'grantor',a.grantor::bigint", sql)
        self.assertIn("'grantee',a.grantee::bigint", sql)

    def test_pair_rejects_public_or_foreign_grants_even_if_receipt_matches(self):
        for key, value in (("grantee", 0), ("grantor", 10), ("is_grantable", True), ("privilege_type", "UNKNOWN")):
            pair, rows = copy.deepcopy(self.pair), copy.deepcopy(self.rows)
            rows["database"]["acl"][0][key] = value
            pair["database_fingerprint"] = copy.deepcopy(rows["database"])
            self.reject(lambda pair=pair, rows=rows: RUNNER.validate_pair_rows(pair, rows, self.tag))

    def test_role_isolation_checks_every_membership_direction_and_external_dependencies(self):
        query = self.replace(RUNNER, "pg", Mock(return_value="0"))
        RUNNER.require_role_isolated(self.pair)
        sql = query.call_args.args[0]
        for fragment in ("member=17001", "roleid=17001", "grantor=17001", "pg_shdepend", "dbid=18001",
                         "pg_db_role_setting", "pg_shseclabel"):
            self.assertIn(fragment, sql)
        query.return_value = "1"
        self.reject(lambda: RUNNER.require_role_isolated(self.pair))

    def test_retained_run_requires_exact_independent_disposal_evidence(self):
        run = "20260911_010203_abcdef012345"
        previous = {"run_id": run, "output": str(RUNNER.WORK / ("client-backup-run-" + run)),
                    "pairs": [self.pair], "tag": self.tag, "cluster": {"system_identifier": "1234567890123456789"}}
        raw = json.dumps(previous, sort_keys=True).encode()
        disposal = {"marker": "goby-client-backup-disposal-m3e-v1", "status": "disposed", "run_id": run,
                    "tag": self.tag, "source_receipt_sha256": RUNNER.sha(raw), "system_identifier": "1234567890123456789",
                    "hba_sha256": RUNNER.sha(BASE_HBA), "report_sha256": "a" * 64,
                    "report_path": str(Path(previous["output"]) / "disposal-report.json"),
                    "pairs": [{"name": RUNNER.NAMES[0], "role_oid": 17001, "database_oid": 18001}]}
        RUNNER.validate_disposal(previous, raw, disposal, RUNNER.sha(BASE_HBA))
        for key, value in (("source_receipt_sha256", "b" * 64), ("system_identifier", "9999999999999999999"),
                           ("status", "pending"), ("pairs", []), ("hba_sha256", "c" * 64),
                           ("report_path", "/tmp/foreign.json"), ("tag", "foreign")):
            changed = dict(disposal, **{key: value})
            self.reject(lambda changed=changed: RUNNER.validate_disposal(previous, raw, changed, RUNNER.sha(BASE_HBA)))

    def test_unknown_object_inventory_includes_hidden_grants_and_hooks(self):
        sql = RUNNER.unsupported_objects_sql(17001)
        for name in ("pg_namespace", "pg_type", "pg_policy", "pg_rewrite", "pg_subscription", "pg_default_acl",
                     "pg_largeobject_metadata", "pg_seclabel", "pg_event_trigger", "pg_foreign_server", "pg_publication",
                     "pg_inherits", "pg_statistic_ext", "pg_transform", "attacl", "proacl", "typacl", "relacl"):
            self.assertIn(name, sql)
        self.assertNotIn("DROP", sql)

    def test_unit_refuses_wrong_description_or_cgroup(self):
        self.reject(lambda: RUNNER.require_unit_owner({"Description": "foreign", "ControlGroup": ""}, self.unit, self.tag))
        self.reject(lambda: RUNNER.require_unit_owner({"Description": self.tag, "ControlGroup": "/system.slice/foreign.service"}, self.unit, self.tag))

    def test_unit_terminal_rejects_active_process_and_unknown_children(self):
        self.replace(RUNNER, "present", lambda path: False)
        RUNNER.require_unit_terminal({"LoadState": "not-found"}, self.unit, self.tag)
        state = {"LoadState": "loaded", "Description": self.tag, "ControlGroup": "/system.slice/" + self.unit,
                 "MainPID": "123", "ActiveState": "active"}
        self.reject(lambda: RUNNER.require_unit_terminal(state, self.unit, self.tag))
        state.update(MainPID="0", ActiveState="inactive")
        self.replace(RUNNER, "present", lambda path: True)
        self.replace(Path, "read_text", lambda *args, **kwargs: "999\n")
        self.reject(lambda: RUNNER.require_unit_terminal(state, self.unit, self.tag))
        self.reject(lambda: RUNNER.require_unit_terminal({"LoadState": "not-found"}, self.unit, self.tag))

    def test_unit_name_cannot_target_existing_product_services(self):
        self.reject(lambda: RUNNER.unit_state("goby-foundation-test.service"))
        self.reject(lambda: RUNNER.unit_state("goby-client-backup-foreign.service"))

    def test_command_failures_never_expose_password_or_url(self):
        secret = "postgresql://synthetic:private-secret@127.0.0.1:15432/private"
        self.replace(subprocess, "run", lambda *args, **kwargs: types.SimpleNamespace(returncode=1, stdout=secret, stderr=secret))
        with self.assertRaises(RUNNER.Failure) as caught:
            RUNNER.command(["synthetic"])
        self.assertNotIn(secret, str(caught.exception))
        self.assertNotIn("private-secret", self.output.getvalue())

    def test_private_read_rejects_foreign_owner_links_permissions_and_special_files(self):
        for overrides in ({"st_uid": 1000}, {"st_gid": 1000}, {"st_nlink": 2},
                          {"st_mode": stat.S_IFREG | 0o644}, {"st_mode": stat.S_IFLNK | 0o600}):
            fields = dict(st_mode=stat.S_IFREG | 0o600, st_uid=0, st_gid=0, st_nlink=1, st_size=1)
            fields.update(overrides)
            self.replace(RUNNER, "canonical", lambda path, fields=fields: types.SimpleNamespace(**fields))
            self.reject(lambda: RUNNER.private_read(Path("/private/control")))

    def test_replace_private_rejects_racing_hba_before_replacement(self):
        reads = iter((b"original", b"foreign"))
        self.replace(RUNNER, "private_read", lambda *args, **kwargs: next(reads))
        self.replace(RUNNER, "create_private", Mock())
        renamed = self.replace(os, "replace", Mock())
        self.reject(lambda: RUNNER.replace_private(Path("/private/hba"), b"original", b"temporary"))
        renamed.assert_not_called()

    def cleanup_fixture(self, actual=None, unit_failure=False):
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        runner.report = {"status": "failed", "cleanup": {}}
        runner.hba_intent = True
        runner.hba_before, runner.hba_after = BASE_HBA, RUNNER.temporary_hba(BASE_HBA, self.tag, BASE_HBA)
        runner.postgres = types.SimpleNamespace(pw_uid=102, pw_gid=105)
        runner.pairs, runner.before_catalog, runner.receipt, runner.lock = [self.pair], None, None, None
        runner.stop_unit = Mock(side_effect=RUNNER.Failure("Unknown unit") if unit_failure else None)
        runner.check_cluster, runner.reload_hba, runner.remove_pair = Mock(), Mock(), Mock()
        self.replace(signal, "signal", Mock())
        self.replace(RUNNER, "private_read", lambda *args, **kwargs: runner.hba_after if actual is None else actual)
        self.replaced = self.replace(RUNNER, "replace_private", Mock())
        return runner

    def test_cleanup_restores_hba_even_when_unit_is_unknown_and_retains_pair(self):
        runner = self.cleanup_fixture(unit_failure=True)
        runner.cleanup()
        self.assertFalse(runner.report["cleanup"]["unit_terminal"])
        self.assertTrue(runner.report["cleanup"]["hba_restored_exactly"])
        self.replaced.assert_called_once_with(RUNNER.HBA, runner.hba_after, BASE_HBA, 102, 105)
        runner.remove_pair.assert_not_called()
        self.assertTrue(runner.report["pair_evidence_retained"])

    def test_cleanup_does_not_overwrite_unrelated_hba(self):
        runner = self.cleanup_fixture(actual=b"foreign")
        runner.cleanup()
        self.replaced.assert_not_called()
        runner.reload_hba.assert_not_called()
        runner.remove_pair.assert_not_called()
        self.assertFalse(runner.report["cleanup"]["hba_restored_exactly"])

    def test_cleanup_does_not_change_hba_after_cluster_identity_loss(self):
        runner = self.cleanup_fixture()
        runner.check_cluster.side_effect = RUNNER.Failure("Changed PID or system identifier")
        runner.cleanup()
        self.replaced.assert_not_called()
        runner.remove_pair.assert_not_called()

    def test_cleanup_retains_failed_database_evidence_after_successful_hba_restoration(self):
        runner = self.cleanup_fixture()
        runner.cleanup()
        self.assertTrue(runner.report["cleanup"]["hba_restored_exactly"])
        runner.remove_pair.assert_not_called()

    def remove_fixture(self, live=False, objects=None, schema=24):
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        runner.tag, runner.hba_before = self.tag, BASE_HBA
        runner.args, runner.output = self.args, RUNNER.WORK / "private-evidence"
        runner.args.schema = schema
        runner.source_identity, runner.source_files = {"device": 1, "inode": 2}, {"fixture": "a" * 64}
        runner.check_cluster, runner.save = Mock(), Mock()
        runner.public_identity = Mock(return_value=self.pair["public"])
        runner.cast_inventory = Mock(return_value=[])
        runner.inspect_objects = Mock(return_value=[] if objects is None else objects)
        self.replace(RUNNER, "pair_rows", lambda name: self.rows)
        self.replace(RUNNER, "require_role_isolated", Mock())
        self.queries = self.replace(RUNNER, "pg", Mock(return_value="1" if live else "0"))
        self.verified_source = self.replace(RUNNER, "verify_source", Mock(return_value=(runner.source_identity, runner.source_files)))
        self.read_catalog = self.replace(RUNNER, "read_source_catalog", Mock(return_value={"objects": [{"known": True}]}))
        self.replace(RUNNER, "create_private", Mock())
        return runner

    def test_cleanup_refuses_live_backends_without_termination_or_drop(self):
        runner = self.remove_fixture(live=True)
        self.reject(lambda: runner.remove_pair(self.pair))
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))
        self.assertFalse(any("pg_terminate_backend" in query.args[0] for query in self.queries.call_args_list))

    def test_cleanup_refuses_unknown_schema_objects_before_database_fence(self):
        runner = self.remove_fixture(objects=[{"foreign": True}])
        self.reject(lambda: runner.remove_pair(self.pair))
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_cleanup_uses_explicit_schema25_and_rechecks_source_before_database_fence(self):
        runner = self.remove_fixture(objects=[{"foreign": True}], schema=25)
        self.reject(lambda: runner.remove_pair(self.pair))
        self.verified_source.assert_called_once_with(self.args.source, self.args.manifest_sha256, 25, catalog_bootstrap=False)
        self.read_catalog.assert_called_once_with(self.args.source, runner.source_files, 25)
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_cleanup_uses_explicit_schema26_before_any_database_fence(self):
        runner = self.remove_fixture(objects=[{"foreign": True}], schema=26)
        self.reject(lambda: runner.remove_pair(self.pair))
        self.verified_source.assert_called_once_with(self.args.source, self.args.manifest_sha256, 26, catalog_bootstrap=False)
        self.read_catalog.assert_called_once_with(self.args.source, runner.source_files, 26)
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_cleanup_uses_explicit_schema27_before_any_database_fence(self):
        runner = self.remove_fixture(objects=[{"foreign": True}], schema=27)
        self.reject(lambda: runner.remove_pair(self.pair))
        self.verified_source.assert_called_once_with(self.args.source, self.args.manifest_sha256, 27, catalog_bootstrap=False)
        self.read_catalog.assert_called_once_with(self.args.source, runner.source_files, 27)
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_cleanup_refuses_changed_frozen_source_before_database_fence(self):
        runner = self.remove_fixture(schema=25)
        self.verified_source.return_value = (runner.source_identity, {"changed": "b" * 64})
        self.reject(lambda: runner.remove_pair(self.pair))
        self.read_catalog.assert_not_called()
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_cleanup_refuses_incomplete_oid_receipts(self):
        runner = self.remove_fixture()
        pair = dict(self.pair, phase="database_pending", database_oid=None)
        self.reject(lambda: runner.remove_pair(pair))
        self.queries.assert_not_called()

    def test_cleanup_rejects_unknown_cast_without_dropping_database(self):
        runner = self.remove_fixture()
        runner.cast_inventory.return_value = [{"oid": 999999, "castsource": 17001}]
        self.reject(lambda: runner.remove_pair(self.pair))
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_cleanup_failure_to_save_final_receipt_cannot_report_success(self):
        runner = self.cleanup_fixture()
        runner.report["status"] = "passed"
        runner.pairs = []
        runner.receipt = {}
        runner.save = Mock(side_effect=RUNNER.Failure("Receipt replacement failed"))
        runner.cleanup()
        self.assertEqual(runner.report["status"], "failed")
        self.assertFalse(runner.report["cleanup"]["receipt_saved"])

    def refresh_source_manifest(self):
        self.source_manifest = {"marker": RUNNER.SOURCE_MARKER,
            "files": {str(path.relative_to(self.args.source)): RUNNER.sha(value) for path, value in self.source_bytes.items()}}
        self.source_raw = json.dumps(self.source_manifest, sort_keys=True).encode()

    def change_catalog(self, version, *, reattest_synthetic=False):
        self.source_bytes[self.args.source / RUNNER.catalog_name(version)] = json.dumps(self.source_catalogs[version], sort_keys=True).encode()
        self.refresh_source_manifest()
        if reattest_synthetic:
            # Descriptor guards must reach validation even though schemas25–27
            # are now historical. Only this explicit in-memory fixture can rebind
            # its synthetic digest; immutable-history guards never use it.
            self.assertIn(version, (25, 26, 27))
            pins = dict(RUNNER.HISTORICAL_CATALOG_SHA256)
            pins[version] = RUNNER.sha(self.source_bytes[self.args.source / RUNNER.catalog_name(version)])
            self.replace(RUNNER, "HISTORICAL_CATALOG_SHA256", pins)

    def source_fixture(self, number="07", schema=24, bootstrap=False):
        self.args.source = source = RUNNER.WORK / ("source-attempt-" + number)
        names = ["go.mod", "go.sum", "scripts/test-env/prepare-postgres-workspace.py",
                 "scripts/test-env/run-backup-full-tests.sh", "internal/backuppg/catalog.go", RUNNER.CATALOG_GENERATOR]
        names.extend(f"fixture/input-{index:03d}.txt" for index in range(101))
        self.source_bytes = {source / name: (name + "\n").encode() for name in names}
        migrations = []
        for version in range(1, schema + 1):
            name = {24: RUNNER.MIGRATION_24_NAME, 25: RUNNER.MIGRATION_25_NAME,
                    26: RUNNER.MIGRATION_26_NAME, 27: RUNNER.MIGRATION_27_NAME,
                    28: RUNNER.MIGRATION_28_NAME}.get(version, f"{version:04d}_fixture.sql")
            content = f"Synthetic migration {version}.\n".encode()
            self.source_bytes[source / (RUNNER.MIGRATION_DIRECTORY + name)] = content
            migrations.append({"version": version, "name": name, "sha256": RUNNER.sha(content)})
        self.source_catalogs = {}
        for version in range(23, schema + 1):
            objects = [{"kind": "relation", "name": f"fixture_relation_{version}", "value": {
                "kind": "r", "persistence": "p", "partition": False, "replica_identity": "d",
                "row_security": False, "force_row_security": False, "options": None}}]
            tables = [{"Name": f"fixture_{index:02d}", "Columns": ["id"], "PrimaryKey": ["id"], "SortKey": ["id"]}
                      for index in range({23: 29, 24: 30, 25: 30, 26: 33, 27: 35, 28: 35}[version])]
            tables[-1]["PrimaryKey"] = []
            if version >= 25:
                tables[0]["Columns"].append("music_source")
            self.source_catalogs[version] = {"version": version, "postgresql_major": 17,
                "migrations": copy.deepcopy(migrations[:version]), "catalog": {"Schema": "", "Tables": tables, "Sequences": [],
                "SHA256": RUNNER.sha(json.dumps(objects, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()),
                "Constraints": []}, "objects": objects}
            self.source_bytes[source / RUNNER.catalog_name(version)] = json.dumps(self.source_catalogs[version], sort_keys=True).encode()
        # These synthetic in-memory catalogs are never trusted production input.
        # The unpatched historical digest constants have their own exact guard.
        self.replace(RUNNER, "HISTORICAL_CATALOG_SHA256", {version: RUNNER.sha(self.source_bytes[source / RUNNER.catalog_name(version)])
            for version in range(23, min(schema, 27) + 1)})
        if bootstrap:
            self.bootstrap_catalog_raw = self.source_bytes.pop(source / RUNNER.catalog_name(schema))
        directory_info = types.SimpleNamespace(st_mode=stat.S_IFDIR | 0o755, st_uid=0, st_gid=0,
                                              st_dev=1, st_ino=1, st_nlink=2, st_size=0)
        self.source_metadata = {
            source: types.SimpleNamespace(**dict(vars(directory_info), st_mode=stat.S_IFDIR | 0o700, st_ino=2)),
            source / "internal": directory_info,
        }
        for index, (path, value) in enumerate(self.source_bytes.items()):
            self.source_metadata[path] = types.SimpleNamespace(st_mode=stat.S_IFREG | 0o644, st_uid=0, st_gid=0,
                st_dev=1, st_ino=index + 3, st_nlink=1, st_size=len(value))
        self.source_entries = [source / "internal", *self.source_bytes]
        self.refresh_source_manifest()
        self.replace(Path, "lstat", lambda path: self.source_metadata.get(path, directory_info))
        self.replace(Path, "rglob", lambda path, pattern: iter(self.source_entries))
        self.replace(Path, "read_bytes", lambda path: self.source_bytes[path])
        self.replace(RUNNER, "private_read", lambda path, **kwargs: self.source_raw if path == source / RUNNER.MANIFEST else self.source_bytes[path])
        return source

    def test_source_manifest_accepts_complete_numbered_source_bytes(self):
        for number in ("05", "07", "100"):
            source = self.source_fixture(number)
            identity, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw))
            self.assertEqual(identity, {"device": 1, "inode": 2})
            self.assertEqual(files, self.source_manifest["files"])

    def test_schema28_bootstrap_and_final_source_have_distinct_catalog_membership(self):
        for bootstrap in (False, True):
            source = self.source_fixture("43", schema=28, bootstrap=bootstrap)
            identity, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 28, catalog_bootstrap=bootstrap)
            self.assertEqual(identity, {"device": 1, "inode": 2})
            self.assertEqual({name for name in files if name.startswith(RUNNER.CATALOG_DIRECTORY)},
                             {RUNNER.catalog_name(version) for version in range(23, 28 if bootstrap else 29)})
            self.assertIn(RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_28_NAME, files)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 28, catalog_bootstrap=not bootstrap))
            for earlier in (24, 25, 26, 27):
                self.reject(lambda earlier=earlier: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), earlier))

    def test_schema28_preserves_every_historical_pin_even_with_a_matching_new_manifest(self):
        for version in range(23, 28):
            source = self.source_fixture(schema=28, bootstrap=True)
            self.source_bytes[source / RUNNER.catalog_name(version)] += b"\n"
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 28, catalog_bootstrap=True))
        for name in (RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_27_NAME,
                     RUNNER.CATALOG_GENERATOR, RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_28_NAME):
            source = self.source_fixture(schema=28, bootstrap=True)
            self.source_entries.remove(source / name)
            del self.source_bytes[source / name]
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 28, catalog_bootstrap=True))

    def test_generated_catalog28_binds_35_tables_and_the_complete_migration_chain(self):
        self.source_fixture(schema=28, bootstrap=True)
        raw, files = self.bootstrap_catalog_raw, copy.deepcopy(self.source_manifest["files"])
        baseline = RUNNER.generated_catalog(raw, files, schema=28)
        self.assertEqual((baseline["version"], len(baseline["catalog"]["Tables"])), (28, 35))
        self.assertEqual(baseline["migrations"][:-1], self.source_catalogs[27]["migrations"])
        for mutate in (lambda value: value["migrations"][-1].update(name="0028_foreign.sql"),
                       lambda value: value["migrations"][-1].update(sha256="f" * 64),
                       lambda value: value["migrations"][26].update(sha256="f" * 64),
                       lambda value: value["catalog"]["Tables"].pop(),
                       lambda value: value.update(version=27), lambda value: value.update(version=True)):
            changed = copy.deepcopy(baseline)
            mutate(changed)
            self.reject(lambda: RUNNER.generated_catalog(json.dumps(changed).encode(), files, schema=28))
        files[RUNNER.MIGRATION_DIRECTORY + "0029_future.sql"] = "f" * 64
        self.reject(lambda: RUNNER.generated_catalog(raw, files, schema=28))

    def test_catalog28_receipt_cannot_adopt_a_relabelled_artifact(self):
        runner = self.generated_artifact_fixture(schema=28)
        runner.inspect_catalog_result(b"Generated trusted schema 28 catalog.\n")
        expected = copy.deepcopy(runner.catalog_artifact)
        for field, value in (("schema", 27), ("schema", True),
                             ("path", str(runner.output / "schema-27-postgresql-17.json")),
                             ("source_manifest_sha256", "f" * 64), ("sha256", "f" * 64)):
            runner.catalog_artifact = dict(expected, **{field: value})
            runner.receipt["catalog_artifact"] = copy.deepcopy(runner.catalog_artifact)
            self.reject(runner.read_generated_catalog)

    def test_schema25_source_binds_new_catalog_to_exact_manifest_and_preserves_30_tables(self):
        source = self.source_fixture("13", schema=25)
        identity, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25)
        baseline = RUNNER.read_source_catalog(source, files, 25)
        self.assertEqual(identity, {"device": 1, "inode": 2})
        self.assertEqual(len(baseline["catalog"]["Tables"]), 30)
        self.assertEqual(baseline["migrations"][-1]["name"], RUNNER.MIGRATION_25_NAME)
        self.assertIn("music_source", baseline["catalog"]["Tables"][0]["Columns"])
        self.assertEqual(baseline["catalog"]["Tables"][-1]["PrimaryKey"], [])

    def test_schema26_source_requires_exact_catalog_history_and_33_tables(self):
        source = self.source_fixture("21", schema=26)
        identity, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26)
        baseline = RUNNER.read_source_catalog(source, files, 26)
        self.assertEqual(identity, {"device": 1, "inode": 2})
        self.assertEqual(baseline["version"], 26)
        self.assertEqual(len(baseline["catalog"]["Tables"]), 33)
        self.assertEqual(baseline["migrations"][-1]["name"], "0026_theme_owners.sql")
        self.assertEqual(baseline["migrations"][:-1], self.source_catalogs[25]["migrations"])
        self.assertEqual({name for name in files if name.startswith(RUNNER.CATALOG_DIRECTORY)},
                         {RUNNER.catalog_name(version) for version in (23, 24, 25, 26)})

    def test_schema27_source_requires_35_tables_and_immutable_schema26_history(self):
        source = self.source_fixture("29", schema=27)
        identity, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27)
        baseline = RUNNER.read_source_catalog(source, files, 27)
        self.assertEqual(identity, {"device": 1, "inode": 2})
        self.assertEqual((baseline["version"], len(baseline["catalog"]["Tables"])), (27, 35))
        self.assertEqual(baseline["migrations"][-1]["name"], "0027_movie_extras.sql")
        self.assertEqual(baseline["migrations"][:-1], self.source_catalogs[26]["migrations"])
        self.assertEqual({name for name in files if name.startswith(RUNNER.CATALOG_DIRECTORY)},
                         {RUNNER.catalog_name(version) for version in (23, 24, 25, 26, 27)})
        for schema in (24, 25, 26):
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), schema))

    def test_schema27_bootstrap_requires_four_historical_catalogs_and_no_current_catalog(self):
        source = self.source_fixture("29", schema=27, bootstrap=True)
        _, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27, catalog_bootstrap=True)
        self.assertEqual({name for name in files if name.startswith(RUNNER.CATALOG_DIRECTORY)},
                         {RUNNER.catalog_name(version) for version in (23, 24, 25, 26)})
        self.assertEqual(len([name for name in files if name.startswith(RUNNER.MIGRATION_DIRECTORY)]), 27)
        self.assertIn(RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_27_NAME, files)
        self.assertNotIn(RUNNER.catalog_name(27), files)
        self.assertIsNone(RUNNER.read_source_catalog(source, files, 27, catalog_bootstrap=True))
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27))
        for schema in (None, True, False, "27", 24, 25, 26, 28, 27.0):
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), schema, catalog_bootstrap=True))
            self.reject(lambda: RUNNER.generated_catalog(self.bootstrap_catalog_raw, files, schema=schema))

    def test_schema27_rejects_missing_or_rewritten_historical26_catalog_even_when_manifest_matches(self):
        for bootstrap in (False, True):
            for changed in ("missing", "bytes"):
                with self.subTest(bootstrap=bootstrap, changed=changed):
                    source = self.source_fixture(schema=27, bootstrap=bootstrap)
                    path = source / RUNNER.catalog_name(26)
                    if changed == "missing":
                        del self.source_bytes[path]
                        self.source_entries.remove(path)
                    else:
                        self.source_bytes[path] += b"\n"
                    self.refresh_source_manifest()
                    self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27, catalog_bootstrap=bootstrap))
            source = self.source_fixture(schema=27, bootstrap=bootstrap)
            self.source_bytes[source / (RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_26_NAME)] += b"Changed historical DDL.\n"
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27, catalog_bootstrap=bootstrap))

    def test_schema27_requires_exact_current_inputs_and_rejects_future_members(self):
        for bootstrap in (False, True):
            for name in (RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_27_NAME,
                         RUNNER.CATALOG_GENERATOR if bootstrap else RUNNER.catalog_name(27)):
                source = self.source_fixture(schema=27, bootstrap=bootstrap)
                del self.source_bytes[source / name]
                self.source_entries.remove(source / name)
                self.refresh_source_manifest()
                self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27, catalog_bootstrap=bootstrap))
            extras = [RUNNER.MIGRATION_DIRECTORY + "0027_unowned.sql", RUNNER.MIGRATION_DIRECTORY + "0028_future.sql",
                      RUNNER.CATALOG_DIRECTORY + "schema-28-postgresql-17.json"]
            if bootstrap:
                extras.append(RUNNER.catalog_name(27))
            for name in extras:
                source = self.source_fixture(schema=27, bootstrap=bootstrap)
                path = source / name
                self.source_bytes[path] = b"Unowned source member.\n"
                self.source_metadata[path] = self.source_metadata[source / "go.mod"]
                self.source_entries.append(path)
                self.refresh_source_manifest()
                self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27, catalog_bootstrap=bootstrap))
            source = self.source_fixture(schema=27, bootstrap=bootstrap)
            self.source_bytes[source / (RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_27_NAME)] += b"Changed current DDL.\n"
            if not bootstrap:
                self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27, catalog_bootstrap=bootstrap))

    def test_schema27_catalog_rejects_non35_tables_and_rewritten_or_untyped_history(self):
        for size in (30, 33, 34, 36):
            source = self.source_fixture(schema=27)
            tables = self.source_catalogs[27]["catalog"]["Tables"]
            if size == 36:
                tables.append({"Name": "unowned", "Columns": ["id"], "PrimaryKey": ["id"], "SortKey": ["id"]})
            else:
                del tables[size:]
            self.change_catalog(27)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27))
        for index, field, value in ((25, "sha256", "f" * 64), (26, "name", "0027_foreign.sql"),
                                   (26, "sha256", "f" * 64), (26, "version", True), (26, "version", 27.0)):
            source = self.source_fixture(schema=27)
            self.source_catalogs[27]["migrations"][index][field] = value
            self.change_catalog(27)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 27))

    def test_schema26_source_and_generated_apis_reject_implicit_or_confused_versions(self):
        source = self.source_fixture(schema=26, bootstrap=True)
        files = self.source_manifest["files"]
        for schema in (None, True, False, "26", 23, 24, 27, 26.0):
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), schema, catalog_bootstrap=True))
            self.reject(lambda: RUNNER.read_source_catalog(source, files, schema, catalog_bootstrap=True))
            self.reject(lambda: RUNNER.generated_catalog(self.bootstrap_catalog_raw, files, schema=schema))
        for bootstrap in (None, 0, 1, "true"):
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=bootstrap))
            self.reject(lambda: RUNNER.read_source_catalog(source, files, 26, catalog_bootstrap=bootstrap))
        self.reject(lambda: RUNNER.generated_catalog(self.bootstrap_catalog_raw, files))

    def test_catalog_bootstrap26_requires_three_historical_catalogs_and_no_current_catalog(self):
        source = self.source_fixture("21", schema=26, bootstrap=True)
        identity, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=True)
        self.assertEqual(identity, {"device": 1, "inode": 2})
        self.assertEqual({name for name in files if name.startswith(RUNNER.CATALOG_DIRECTORY)},
                         {RUNNER.catalog_name(version) for version in (23, 24, 25)})
        self.assertEqual(len([name for name in files if name.startswith(RUNNER.MIGRATION_DIRECTORY)]), 26)
        self.assertIn(RUNNER.MIGRATION_DIRECTORY + "0026_theme_owners.sql", files)
        self.assertNotIn(RUNNER.catalog_name(26), files)
        self.assertIsNone(RUNNER.read_source_catalog(source, files, 26, catalog_bootstrap=True))
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25, catalog_bootstrap=True))

    def test_schema26_source_rejects_missing_inputs_even_with_an_updated_manifest(self):
        for bootstrap in (False, True):
            names = [RUNNER.catalog_name(version) for version in (23, 24, 25)]
            names += [RUNNER.MIGRATION_DIRECTORY + "0026_theme_owners.sql",
                      RUNNER.CATALOG_GENERATOR if bootstrap else RUNNER.catalog_name(26)]
            for name in names:
                with self.subTest(bootstrap=bootstrap, missing=name):
                    source = self.source_fixture(schema=26, bootstrap=bootstrap)
                    self.source_entries.remove(source / name)
                    del self.source_bytes[source / name]
                    self.refresh_source_manifest()
                    self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=bootstrap))

    def test_schema26_source_rejects_extra_historical_future_or_bootstrap_current_catalogs(self):
        for bootstrap, version in ((False, 22), (False, 27), (True, 22), (True, 26), (True, 27)):
            with self.subTest(bootstrap=bootstrap, extra=version):
                source = self.source_fixture(schema=26, bootstrap=bootstrap)
                path = source / (RUNNER.CATALOG_DIRECTORY + f"schema-{version}-postgresql-17.json")
                self.source_bytes[path] = self.bootstrap_catalog_raw if version == 26 else b"{}"
                self.source_metadata[path] = self.source_metadata[source / "go.mod"]
                self.source_entries.append(path)
                self.refresh_source_manifest()
                self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=bootstrap))

    def test_schema26_source_never_retrusts_rewritten_historical_catalogs(self):
        for bootstrap in (False, True):
            for version in (23, 24, 25):
                source = self.source_fixture(schema=26, bootstrap=bootstrap)
                self.source_bytes[source / RUNNER.catalog_name(version)] += b"\n"
                self.refresh_source_manifest()
                self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=bootstrap))
            source = self.source_fixture(schema=26, bootstrap=bootstrap)
            self.source_bytes[source / (RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_25_NAME)] += b"Changed historical DDL.\n"
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=bootstrap))

    def test_schema26_migration_requires_exact_name_membership_and_catalog_digest(self):
        for bootstrap in (False, True):
            for name in ("0026_unowned.sql", "0027_future.sql"):
                source = self.source_fixture(schema=26, bootstrap=bootstrap)
                path = source / (RUNNER.MIGRATION_DIRECTORY + name)
                self.source_bytes[path] = b"Unreviewed migration.\n"
                self.source_metadata[path] = self.source_metadata[source / "go.mod"]
                self.source_entries.append(path)
                self.refresh_source_manifest()
                self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=bootstrap))
        source = self.source_fixture(schema=26)
        self.source_bytes[source / (RUNNER.MIGRATION_DIRECTORY + "0026_theme_owners.sql")] += b"Changed current DDL.\n"
        self.refresh_source_manifest()
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))
        source = self.source_fixture(schema=26, bootstrap=True)
        self.source_bytes[source / (RUNNER.MIGRATION_DIRECTORY + "0026_theme_owners.sql")] += b"Unattested changed DDL.\n"
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26, catalog_bootstrap=True))

    def test_schema26_catalog_rejects_wrong_identity_and_non33_table_counts(self):
        for field, value in (("version", 25), ("version", True), ("version", 26.0),
                             ("postgresql_major", True), ("postgresql_major", 16), ("postgresql_major", 17.0)):
            source = self.source_fixture(schema=26)
            self.source_catalogs[26][field] = value
            self.change_catalog(26, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))
        for size in (0, 30, 32, 34):
            source = self.source_fixture(schema=26)
            tables = self.source_catalogs[26]["catalog"]["Tables"]
            if size == 34:
                tables.append({"Name": "unowned", "Columns": ["id"], "PrimaryKey": ["id"], "SortKey": ["id"]})
            else:
                del tables[size:]
            self.change_catalog(26, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))

    def test_schema26_catalog_rejects_rewritten_or_incomplete_migration_history(self):
        for index, field, value in ((0, "sha256", "f" * 64), (24, "name", "0025_foreign.sql"),
                                   (25, "name", "0026_foreign.sql"), (25, "sha256", "f" * 64),
                                   (25, "version", True), (25, "version", 26.0), (25, "version", 25)):
            source = self.source_fixture(schema=26)
            self.source_catalogs[26]["migrations"][index][field] = value
            self.change_catalog(26, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))
        for extra in (False, True):
            source = self.source_fixture(schema=26)
            history = self.source_catalogs[26]["migrations"]
            if extra:
                history.append({"version": 27, "name": "0027_future.sql", "sha256": "f" * 64})
            else:
                history.pop()
            self.change_catalog(26, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))

    def test_schema26_catalog_rejects_unowned_fields_and_object_fingerprint_drift(self):
        for boundary in ("root", "migration", "catalog", "table", "sequence", "consumer", "constraint", "object", "value"):
            source = self.source_fixture(schema=26)
            baseline = self.source_catalogs[26]
            catalog = baseline["catalog"]
            sequence = {"Name": "fixture_seq", "Table": "fixture_00", "Column": "id", "MinValue": 1, "MaxValue": 10,
                        "Increment": 1, "Consumers": [{"Table": "fixture_00", "Column": "id"}]}
            constraint = {"Table": "fixture_00", "Name": "fixture_fkey", "Definition": "Synthetic constraint"}
            catalog["Sequences"], catalog["Constraints"] = [sequence], [constraint]
            target = {"root": baseline, "migration": baseline["migrations"][-1], "catalog": catalog,
                "table": catalog["Tables"][0], "sequence": sequence, "consumer": sequence["Consumers"][0],
                "constraint": constraint, "object": baseline["objects"][0], "value": baseline["objects"][0]["value"]}[boundary]
            target["unowned"] = True
            self.change_catalog(26, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))
        source = self.source_fixture(schema=26)
        self.source_catalogs[26]["objects"][0]["value"]["row_security"] = True
        self.change_catalog(26, reattest_synthetic=True)
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 26))

    def test_catalog_bootstrap_requires_exact_schema25_migrations_without_a_catalog25_input(self):
        source = self.source_fixture("13", schema=25, bootstrap=True)
        identity, files = RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25, catalog_bootstrap=True)
        self.assertEqual(identity, {"device": 1, "inode": 2})
        self.assertNotIn(RUNNER.catalog_name(25), files)
        self.assertIn(RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_25_NAME, files)
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 24, catalog_bootstrap=True))
        source = self.source_fixture("13", schema=25)
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25, catalog_bootstrap=True))

    def test_catalog_bootstrap_rejects_missing_generator_and_wrong_migration_hash(self):
        for name in (RUNNER.CATALOG_GENERATOR, RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_25_NAME):
            source = self.source_fixture(schema=25, bootstrap=True)
            self.source_entries.remove(source / name)
            del self.source_bytes[source / name]
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25, catalog_bootstrap=True))
        source = self.source_fixture(schema=25, bootstrap=True)
        self.source_bytes[source / (RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_25_NAME)] += b"Changed DDL.\n"
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25, catalog_bootstrap=True))

    def test_generated_catalog_requires_exact_bootstrap_history_and_schema_identity(self):
        self.source_fixture(schema=25, bootstrap=True)
        baseline = RUNNER.generated_catalog(self.bootstrap_catalog_raw, self.source_manifest["files"])
        self.assertEqual(baseline["version"], 25)
        for mutate in (lambda value: value.update(version=24), lambda value: value.update(postgresql_major=16),
                       lambda value: value["migrations"][-1].update(sha256="f" * 64),
                       lambda value: value["migrations"][-1].update(name="0025_foreign.sql"),
                       lambda value: value.update(unowned=True)):
            value = copy.deepcopy(baseline)
            mutate(value)
            self.reject(lambda: RUNNER.generated_catalog(json.dumps(value).encode(), self.source_manifest["files"]))

    def test_generated_catalog26_binds_every_migration_to_the_exact_bootstrap_manifest(self):
        self.source_fixture(schema=26, bootstrap=True)
        files = copy.deepcopy(self.source_manifest["files"])
        raw = self.bootstrap_catalog_raw
        baseline = RUNNER.generated_catalog(raw, files, schema=26)
        self.assertEqual((baseline["version"], len(baseline["catalog"]["Tables"])), (26, 33))
        for mutate in (lambda values: values.pop(RUNNER.MIGRATION_DIRECTORY + "0026_theme_owners.sql"),
                       lambda values: values.update({RUNNER.MIGRATION_DIRECTORY + "0026_theme_owners.sql": "f" * 64}),
                       lambda values: values.update({RUNNER.MIGRATION_DIRECTORY + "0027_future.sql": "f" * 64}),
                       lambda values: values.update({RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_25_NAME: "f" * 64})):
            changed = copy.deepcopy(files)
            mutate(changed)
            self.reject(lambda: RUNNER.generated_catalog(raw, changed, schema=26))
        for mutate in (lambda value: value.update(version=25), lambda value: value.update(version=True),
                       lambda value: value["migrations"][-1].update(name="0026_foreign.sql"),
                       lambda value: value["migrations"][-1].update(sha256="f" * 64),
                       lambda value: value.update(unowned=True)):
            changed = copy.deepcopy(baseline)
            mutate(changed)
            self.reject(lambda: RUNNER.generated_catalog(json.dumps(changed).encode(), files, schema=26))

    def test_generated_catalog26_never_accepts_a_schema25_artifact_or_history(self):
        self.source_fixture(schema=25, bootstrap=True)
        raw25, files25 = self.bootstrap_catalog_raw, copy.deepcopy(self.source_manifest["files"])
        self.assertEqual(RUNNER.generated_catalog(raw25, files25)["version"], 25)
        self.reject(lambda: RUNNER.generated_catalog(raw25, files25, schema=26))
        self.source_fixture(schema=26, bootstrap=True)
        self.reject(lambda: RUNNER.generated_catalog(raw25, self.source_manifest["files"], schema=26))
        self.reject(lambda: RUNNER.generated_catalog(self.bootstrap_catalog_raw, files25, schema=26))

    def test_generated_catalog27_requires_exact_27_history_and_never_accepts_schema26(self):
        self.source_fixture(schema=27, bootstrap=True)
        raw, files = self.bootstrap_catalog_raw, copy.deepcopy(self.source_manifest["files"])
        baseline = RUNNER.generated_catalog(raw, files, schema=27)
        self.assertEqual((baseline["version"], len(baseline["catalog"]["Tables"])), (27, 35))
        for mutate in (
                lambda value: value.pop(RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_27_NAME),
                lambda value: value.update({RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_27_NAME: "f" * 64}),
                lambda value: value.update({RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_26_NAME: "f" * 64}),
                lambda value: value.update({RUNNER.MIGRATION_DIRECTORY + "0028_future.sql": "f" * 64})):
            changed = copy.deepcopy(files)
            mutate(changed)
            self.reject(lambda: RUNNER.generated_catalog(raw, changed, schema=27))
        for mutate in (lambda value: value.update(version=26), lambda value: value.update(version=True),
                       lambda value: value["migrations"][-1].update(name="0027_foreign.sql"),
                       lambda value: value["migrations"].pop(), lambda value: value.update(unowned=True)):
            changed = copy.deepcopy(baseline)
            mutate(changed)
            self.reject(lambda: RUNNER.generated_catalog(json.dumps(changed).encode(), files, schema=27))
        raw26 = json.dumps(self.source_catalogs[26], sort_keys=True).encode()
        self.reject(lambda: RUNNER.generated_catalog(raw26, files, schema=27))
        self.reject(lambda: RUNNER.generated_catalog(raw, files, schema=26))

    def generated_artifact_fixture(self, schema=25):
        self.source_fixture(schema=schema, bootstrap=True)
        self.args.schema, self.args.mode = schema, "catalog"
        self.args.manifest_sha256 = RUNNER.sha(self.source_raw)
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        runner.args = self.args
        runner.output = RUNNER.WORK / "client-backup-run-20260911_010203_abcdef012345"
        runner.output_identity = {"device": 1, "inode": 2}
        runner.catalog_output = runner.output / f"schema-{schema}-postgresql-17.json"
        runner.catalog_artifact = None
        runner.source_files = self.source_manifest["files"]
        runner.report, runner.receipt, runner.receipt_bytes = {}, {}, b"Synthetic receipt.\n"
        runner.save, runner.inspect_catalog_pairs = Mock(), Mock()
        artifact_path, output_path = runner.catalog_output, runner.output
        def artifact_directory(path):
            if path != output_path:
                raise AssertionError("Read outside the exact synthetic output directory.")
            return runner.output_identity
        self.replace(RUNNER, "directory", artifact_directory)
        def artifact_bytes(path, **kwargs):
            if path == RUNNER.RECEIPT:
                return runner.receipt_bytes
            if path == artifact_path:
                return self.bootstrap_catalog_raw
            raise AssertionError("Read outside the exact synthetic artifact and receipt.")
        self.replace(RUNNER, "private_read", artifact_bytes)
        return runner

    def test_catalog_completion_receipts_exact_owned_artifact_and_source_objects_with_empty_target(self):
        runner = self.generated_artifact_fixture()
        runner.inspect_catalog_result(b"Generated trusted schema 25 catalog.\n")
        runner.inspect_catalog_pairs.assert_called_once_with(self.source_catalogs[25]["objects"], [])
        runner.save.assert_called_once()
        self.assertEqual(runner.catalog_artifact, {"path": str(runner.catalog_output), "sha256": RUNNER.sha(self.bootstrap_catalog_raw),
            "schema": 25, "source_manifest_sha256": self.args.manifest_sha256})
        self.assertEqual(RUNNER.generated_catalog(self.bootstrap_catalog_raw, runner.source_files), runner.read_generated_catalog())
        self.reject(lambda: runner.inspect_catalog_result(b"Generated trusted schema 25 catalog.\n"))

    def test_catalog26_completion_binds_log_path_artifact_and_empty_target_to_schema26(self):
        runner = self.generated_artifact_fixture(schema=26)
        runner.inspect_catalog_result(b"Generated trusted schema 26 catalog.\n")
        runner.inspect_catalog_pairs.assert_called_once_with(self.source_catalogs[26]["objects"], [])
        runner.save.assert_called_once()
        expected = {"path": str(runner.output / "schema-26-postgresql-17.json"),
                    "sha256": RUNNER.sha(self.bootstrap_catalog_raw), "schema": 26,
                    "source_manifest_sha256": self.args.manifest_sha256}
        self.assertEqual(runner.catalog_artifact, expected)
        self.assertEqual(runner.receipt["catalog_artifact"], expected)
        self.assertEqual(runner.report["catalog_artifact"], expected)
        self.assertEqual(runner.read_generated_catalog(),
                         RUNNER.generated_catalog(self.bootstrap_catalog_raw, runner.source_files, schema=26))
        self.reject(lambda: runner.inspect_catalog_result(b"Generated trusted schema 26 catalog.\n"))

    def test_catalog27_completion_binds_35_table_artifact_receipt_and_empty_target(self):
        runner = self.generated_artifact_fixture(schema=27)
        runner.inspect_catalog_result(b"Generated trusted schema 27 catalog.\n")
        runner.inspect_catalog_pairs.assert_called_once_with(self.source_catalogs[27]["objects"], [])
        runner.save.assert_called_once()
        expected = {"path": str(runner.output / "schema-27-postgresql-17.json"),
                    "sha256": RUNNER.sha(self.bootstrap_catalog_raw), "schema": 27,
                    "source_manifest_sha256": self.args.manifest_sha256}
        self.assertEqual(runner.catalog_artifact, expected)
        self.assertEqual(runner.receipt["catalog_artifact"], expected)
        self.assertEqual(runner.report["catalog_artifact"], expected)
        self.assertEqual(len(runner.read_generated_catalog()["catalog"]["Tables"]), 35)
        runner.catalog_artifact = dict(expected, schema=26)
        runner.receipt["catalog_artifact"] = copy.deepcopy(runner.catalog_artifact)
        self.reject(runner.read_generated_catalog)

    def test_catalog27_launch_and_completion_reject_schema26_paths_logs_or_bytes(self):
        runner = self.generated_artifact_fixture(schema=27)
        exists = self.replace(RUNNER, "present", Mock(return_value=False))
        runner.check_catalog_launch()
        exists.assert_called_once_with(runner.output / "schema-27-postgresql-17.json")
        runner.inspect_catalog_pairs.assert_called_once_with([], [])
        runner.inspect_catalog_pairs.reset_mock()
        exists.return_value = True
        self.reject(runner.check_catalog_launch)
        runner.inspect_catalog_pairs.assert_not_called()
        exists.return_value = False
        expected_path = runner.catalog_output
        for schema, path in ((27, runner.output / "schema-26-postgresql-17.json"), (26, expected_path),
                             (True, expected_path), (27.0, expected_path), (28, expected_path)):
            runner.args.schema, runner.catalog_output = schema, path
            self.reject(runner.check_catalog_launch)
            runner.inspect_catalog_pairs.assert_not_called()
        for log in (b"Generated trusted schema 26 catalog.\n", b"Generated trusted schema 27 catalog.\n" * 2):
            runner = self.generated_artifact_fixture(schema=27)
            self.reject(lambda: runner.inspect_catalog_result(log))
            runner.save.assert_not_called()
            runner.inspect_catalog_pairs.assert_not_called()
        runner = self.generated_artifact_fixture(schema=27)
        raw26 = json.dumps(self.source_catalogs[26], sort_keys=True).encode()
        self.replace(RUNNER, "private_read", lambda path, **kwargs: raw26 if path == runner.catalog_output else runner.receipt_bytes)
        self.reject(lambda: runner.inspect_catalog_result(b"Generated trusted schema 27 catalog.\n"))
        runner.save.assert_not_called()
        runner.inspect_catalog_pairs.assert_not_called()
        self.assertIsNone(runner.catalog_artifact)

    def test_catalog26_completion_rejects_wrong_schema_log_duplicate_and_unsaved_artifact(self):
        for log in (b"", b"Generated trusted schema 25 catalog.\n", b"Generated trusted schema 27 catalog.\n",
                    b"Generated trusted schema 26 catalog.\n" * 2):
            runner = self.generated_artifact_fixture(schema=26)
            self.reject(lambda: runner.inspect_catalog_result(log))
            runner.save.assert_not_called()
            runner.inspect_catalog_pairs.assert_not_called()
            self.assertIsNone(runner.catalog_artifact)
        runner = self.generated_artifact_fixture(schema=26)
        runner.save.side_effect = RUNNER.Failure("Catalog receipt persistence failed.")
        self.reject(lambda: runner.inspect_catalog_result(b"Generated trusted schema 26 catalog.\n"))
        self.assertIsNone(runner.catalog_artifact)
        self.reject(runner.read_generated_catalog)

    def test_catalog26_artifact_cannot_be_relabelled_or_adopted_after_generation(self):
        runner = self.generated_artifact_fixture(schema=26)
        runner.inspect_catalog_result(b"Generated trusted schema 26 catalog.\n")
        expected = copy.deepcopy(runner.catalog_artifact)
        for field, value in (("schema", 25), ("schema", True), ("schema", "26"), ("schema", 26.0),
                ("path", str(runner.output / "schema-25-postgresql-17.json")),
                ("path", str(RUNNER.WORK / "schema-26-postgresql-17.json")),
                ("source_manifest_sha256", "f" * 64), ("sha256", "f" * 64), ("unowned", True)):
            # Match both in-memory copies so schema/path checks themselves,
            # rather than a trivial receipt inequality, must reject the change.
            runner.catalog_artifact = dict(expected, **{field: value})
            runner.receipt["catalog_artifact"] = copy.deepcopy(runner.catalog_artifact)
            self.reject(runner.read_generated_catalog)
        runner.catalog_artifact = expected
        runner.receipt["catalog_artifact"] = copy.deepcopy(expected)
        runner.catalog_output = runner.output / "schema-25-postgresql-17.json"
        self.reject(runner.read_generated_catalog)

    def test_catalog26_completion_rejects_schema25_bytes_before_receipt_or_pair_adoption(self):
        runner = self.generated_artifact_fixture(schema=26)
        raw25 = json.dumps(self.source_catalogs[25], sort_keys=True).encode()
        self.replace(RUNNER, "private_read", lambda path, **kwargs: raw25 if path == runner.catalog_output else runner.receipt_bytes)
        self.reject(lambda: runner.inspect_catalog_result(b"Generated trusted schema 26 catalog.\n"))
        runner.save.assert_not_called()
        runner.inspect_catalog_pairs.assert_not_called()
        self.assertIsNone(runner.catalog_artifact)

    def test_catalog26_launch_rejects_wrong_path_or_schema_before_pair_or_generator_effects(self):
        runner = self.generated_artifact_fixture(schema=26)
        exists = self.replace(RUNNER, "present", Mock(return_value=False))
        runner.check_catalog_launch()
        exists.assert_called_once_with(runner.output / "schema-26-postgresql-17.json")
        runner.inspect_catalog_pairs.assert_called_once_with([], [])
        runner.inspect_catalog_pairs.reset_mock()
        expected_path = runner.catalog_output
        for schema, path in ((26, runner.output / "schema-25-postgresql-17.json"), (25, expected_path),
                             (True, expected_path), (26.0, expected_path), (27, expected_path)):
            runner.args.schema, runner.catalog_output = schema, path
            self.reject(runner.check_catalog_launch)
            runner.inspect_catalog_pairs.assert_not_called()

    def test_catalog26_environment_targets_only_its_owned_schema26_artifact(self):
        runner = self.generated_artifact_fixture(schema=26)
        runner.run = "20260911_010203_abcdef012345"
        created = self.replace(RUNNER, "create_private", Mock())
        self.replace(Path, "mkdir", Mock())
        self.replace(RUNNER, "command", lambda arguments, **kwargs:
            f"{arguments[arguments.index('-d') + 1]}|{arguments[arguments.index('-d') + 1]}|15432")
        runner.write_environment({name: "synthetic-password" for name in RUNNER.NAMES})
        script = next(call.args[1] for call in created.call_args_list if call.args[0] == runner.output / "run.sh")
        self.assertEqual(script, ("#!/bin/bash\nset -euo pipefail\numask 077\nexec " + str(RUNNER.GO) + " run " +
            RUNNER.CATALOG_GENERATOR + " " + str(runner.output / "schema-26-postgresql-17.json") + "\n").encode())
        self.assertNotIn(b"schema-25-postgresql", script)
        self.assertTrue(all(call.args[0].parent == runner.output for call in created.call_args_list))

    def test_catalog27_environment_targets_only_the_owned_schema27_artifact(self):
        runner = self.generated_artifact_fixture(schema=27)
        runner.run = "20260911_010203_abcdef012345"
        created = self.replace(RUNNER, "create_private", Mock())
        self.replace(Path, "mkdir", Mock())
        self.replace(RUNNER, "command", lambda arguments, **kwargs:
            f"{arguments[arguments.index('-d') + 1]}|{arguments[arguments.index('-d') + 1]}|15432")
        runner.write_environment({name: "synthetic-password" for name in RUNNER.NAMES})
        script = next(call.args[1] for call in created.call_args_list if call.args[0] == runner.output / "run.sh")
        self.assertEqual(script, ("#!/bin/bash\nset -euo pipefail\numask 077\nexec " + str(RUNNER.GO) + " run " +
            RUNNER.CATALOG_GENERATOR + " " + str(runner.output / "schema-27-postgresql-17.json") + "\n").encode())
        self.assertNotIn(b"schema-26-postgresql", script)
        self.assertTrue(all(call.args[0].parent == runner.output for call in created.call_args_list))

    def test_catalog26_cleanup_rejects_missing_or_rewritten_bytes_before_database_fence(self):
        for missing in (False, True):
            runner = self.remove_fixture(objects=[{"known": True}], schema=26)
            runner.args.mode = "catalog"
            runner.catalog_output = runner.output / "schema-26-postgresql-17.json"
            runner.catalog_artifact = {"path": str(runner.catalog_output), "sha256": RUNNER.sha(b"Original artifact.\n"),
                                      "schema": 26, "source_manifest_sha256": self.args.manifest_sha256}
            runner.receipt = {"catalog_artifact": copy.deepcopy(runner.catalog_artifact)}
            runner.receipt_bytes, runner.output_identity = b"Synthetic receipt.\n", {"device": 1, "inode": 2}
            self.replace(RUNNER, "directory", Mock(return_value=runner.output_identity))
            read_paths = []
            def artifact_bytes(path, **kwargs):
                read_paths.append(path)
                if path == RUNNER.RECEIPT:
                    return runner.receipt_bytes
                if path != runner.catalog_output:
                    raise AssertionError("Read outside the exact synthetic artifact and receipt.")
                if missing:
                    raise RUNNER.Failure("Catalog artifact is missing.")
                return b"Changed artifact.\n"
            self.replace(RUNNER, "private_read", artifact_bytes)
            self.reject(lambda: runner.remove_pair(self.pair))
            self.assertEqual(read_paths, [RUNNER.RECEIPT, runner.catalog_output])
            self.read_catalog.assert_not_called()
            self.verified_source.assert_called_once_with(self.args.source, self.args.manifest_sha256, 26, catalog_bootstrap=True)
            self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_catalog27_cleanup_requires_its_receipted_artifact_before_database_fence(self):
        runner = self.remove_fixture(objects=[{"known": True}], schema=27)
        runner.args.mode = "catalog"
        runner.catalog_output = runner.output / "schema-27-postgresql-17.json"
        runner.catalog_artifact = {"path": str(runner.catalog_output), "sha256": RUNNER.sha(b"Original artifact.\n"),
                                  "schema": 27, "source_manifest_sha256": self.args.manifest_sha256}
        runner.receipt = {"catalog_artifact": copy.deepcopy(runner.catalog_artifact)}
        runner.receipt_bytes, runner.output_identity = b"Synthetic receipt.\n", {"device": 1, "inode": 2}
        self.replace(RUNNER, "directory", Mock(return_value=runner.output_identity))
        self.replace(RUNNER, "private_read", lambda path, **kwargs:
            runner.receipt_bytes if path == RUNNER.RECEIPT else b"Changed artifact.\n")
        self.reject(lambda: runner.remove_pair(self.pair))
        self.read_catalog.assert_not_called()
        self.verified_source.assert_called_once_with(self.args.source, self.args.manifest_sha256, 27, catalog_bootstrap=True)
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_catalog_environment_runs_only_the_bound_generator_into_owned_output(self):
        runner = self.generated_artifact_fixture()
        runner.run = "20260911_010203_abcdef012345"
        created = self.replace(RUNNER, "create_private", Mock())
        self.replace(Path, "mkdir", Mock())
        self.replace(RUNNER, "command", lambda arguments, **kwargs:
            f"{arguments[arguments.index('-d') + 1]}|{arguments[arguments.index('-d') + 1]}|15432")
        runner.write_environment({name: "synthetic-password" for name in RUNNER.NAMES})
        script = next(call.args[1] for call in created.call_args_list if call.args[0] == runner.output / "run.sh")
        self.assertEqual(script, ("#!/bin/bash\nset -euo pipefail\numask 077\nexec " + str(RUNNER.GO) + " run " +
            RUNNER.CATALOG_GENERATOR + " " + str(runner.catalog_output) + "\n").encode())
        self.assertTrue(all(call.args[0].parent == runner.output for call in created.call_args_list))

    def test_catalog_launch_rechecks_output_directory_receipt_and_empty_pair_before_effects(self):
        runner = self.generated_artifact_fixture()
        exists = self.replace(RUNNER, "present", Mock(return_value=False))
        runner.check_catalog_launch()
        exists.assert_called_once_with(runner.catalog_output)
        runner.inspect_catalog_pairs.assert_called_once_with([], [])
        runner.inspect_catalog_pairs.reset_mock()
        self.replace(RUNNER, "directory", Mock(return_value={"device": 1, "inode": 99}))
        self.reject(runner.check_catalog_launch)
        runner.inspect_catalog_pairs.assert_not_called()
        self.replace(RUNNER, "directory", Mock(return_value=runner.output_identity))
        self.replace(RUNNER, "private_read", Mock(return_value=b"Foreign receipt.\n"))
        self.reject(runner.check_catalog_launch)
        runner.inspect_catalog_pairs.assert_not_called()
        self.replace(RUNNER, "private_read", Mock(return_value=runner.receipt_bytes))
        exists.return_value = True
        self.reject(runner.check_catalog_launch)
        runner.inspect_catalog_pairs.assert_not_called()

    def test_catalog_completion_requires_unique_completion_and_a_saved_receipt(self):
        for log in (b"", b"Generated trusted schema 24 catalog.\n", b"Generated trusted schema 25 catalog.\n" * 2):
            runner = self.generated_artifact_fixture()
            self.reject(lambda: runner.inspect_catalog_result(log))
            runner.save.assert_not_called()
            self.assertIsNone(runner.catalog_artifact)
        runner = self.generated_artifact_fixture()
        runner.save.side_effect = RUNNER.Failure("Catalog receipt persistence failed.")
        self.reject(lambda: runner.inspect_catalog_result(b"Generated trusted schema 25 catalog.\n"))
        self.assertIsNone(runner.catalog_artifact)
        self.reject(runner.read_generated_catalog)

    def test_generated_catalog_cleanup_rejects_missing_or_changed_artifact_and_foreign_path(self):
        runner = self.generated_artifact_fixture()
        self.reject(runner.read_generated_catalog)
        runner.inspect_catalog_result(b"Generated trusted schema 25 catalog.\n")
        expected = copy.deepcopy(runner.catalog_artifact)
        for field, value in (("path", str(RUNNER.WORK / "schema-25-postgresql-17.json")),
                             ("source_manifest_sha256", "f" * 64), ("sha256", "f" * 64), ("schema", 24), ("unowned", True)):
            runner.catalog_artifact = dict(expected, **{field: value})
            self.reject(runner.read_generated_catalog)
        runner.catalog_artifact = expected
        self.replace(RUNNER, "private_read", lambda path, **kwargs: runner.receipt_bytes if path == RUNNER.RECEIPT else self.bootstrap_catalog_raw + b"\n")
        self.reject(runner.read_generated_catalog)
        def missing_artifact(path, **kwargs):
            if path == runner.catalog_output:
                raise RUNNER.Failure("Catalog artifact is missing.")
            return runner.receipt_bytes
        self.replace(RUNNER, "private_read", missing_artifact)
        self.reject(runner.read_generated_catalog)

    def test_catalog_cleanup_refuses_unreceipted_source_objects_before_any_database_fence(self):
        runner = self.remove_fixture(objects=[{"known": True}], schema=25)
        runner.args.mode, runner.catalog_artifact = "catalog", None
        self.reject(lambda: runner.remove_pair(self.pair))
        self.read_catalog.assert_not_called()
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_catalog_cleanup_refuses_missing_artifact_after_valid_receipt_before_database_fence(self):
        runner = self.remove_fixture(objects=[{"known": True}], schema=25)
        runner.args.mode = "catalog"
        runner.catalog_output = runner.output / "schema-25-postgresql-17.json"
        runner.catalog_artifact = {"path": str(runner.catalog_output), "sha256": "a" * 64, "schema": 25,
                                   "source_manifest_sha256": self.args.manifest_sha256}
        runner.receipt, runner.receipt_bytes = {"catalog_artifact": runner.catalog_artifact}, b"Synthetic receipt.\n"
        runner.output_identity = {"device": 1, "inode": 2}
        self.replace(RUNNER, "directory", Mock(return_value=runner.output_identity))
        read_paths = []
        def missing_artifact(path, **kwargs):
            read_paths.append(path)
            if path == runner.catalog_output:
                raise RUNNER.Failure("Catalog artifact is missing.")
            return runner.receipt_bytes
        self.replace(RUNNER, "private_read", missing_artifact)
        self.reject(lambda: runner.remove_pair(self.pair))
        self.assertEqual(read_paths, [RUNNER.RECEIPT, runner.catalog_output])
        self.assertTrue(all(query.args[0].startswith("SELECT") for query in self.queries.call_args_list))

    def test_catalog_pair_check_keeps_cast_and_unknown_object_guards_before_empty_acceptance(self):
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        runner.tag = self.tag
        runner.pairs = [copy.deepcopy(self.pair), dict(copy.deepcopy(self.pair), name=RUNNER.NAMES[1])]
        runner.public_identity = Mock(return_value=self.pair["public"])
        runner.cast_inventory = Mock(return_value=[])
        runner.inspect_objects = Mock(return_value=[])
        self.replace(RUNNER, "pair_rows", lambda name: self.rows)
        self.replace(RUNNER, "require_role_isolated", Mock())
        runner.inspect_catalog_pairs([], [])
        runner.cast_inventory.return_value = [{"unowned": True}]
        runner.inspect_objects.reset_mock()
        self.reject(lambda: runner.inspect_catalog_pairs([], []))
        runner.inspect_objects.assert_not_called()
        runner.cast_inventory.return_value = []
        runner.inspect_objects.side_effect = RUNNER.Failure("Unknown grants or hooks.")
        self.reject(lambda: runner.inspect_catalog_pairs([], []))

    def test_schema24_rejects_schema25_source_before_workspace_or_database_effects(self):
        source = self.source_fixture("13", schema=25)
        self.args.manifest_sha256 = RUNNER.sha(self.source_raw)
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        runner.args = self.args
        self.replace(sys, "platform", "linux")
        self.replace(os, "geteuid", lambda: 0)
        self.replace(os, "umask", Mock())
        self.enterContext(patch.dict(os.environ, {"SSH_CONNECTION": "synthetic"}))
        self.replace(RUNNER, "directory", Mock(return_value={"device": 1, "inode": 2}))
        workspace = self.replace(RUNNER, "load_workspace", Mock())
        with self.assertRaisesRegex(RUNNER.Failure, "catalog membership"):
            runner.prepare()
        workspace.assert_not_called()
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_schema24_and25_reject_schema26_source_before_workspace_or_database_effects(self):
        source = self.source_fixture("21", schema=26)
        self.args.manifest_sha256 = RUNNER.sha(self.source_raw)
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        runner.args = self.args
        self.replace(sys, "platform", "linux")
        self.replace(os, "geteuid", lambda: 0)
        self.replace(os, "umask", Mock())
        self.enterContext(patch.dict(os.environ, {"SSH_CONNECTION": "synthetic"}))
        self.replace(RUNNER, "directory", Mock(return_value={"device": 1, "inode": 2}))
        workspace = self.replace(RUNNER, "load_workspace", Mock())
        for schema in (24, 25):
            self.args.schema = schema
            self.reject(runner.prepare)
            workspace.assert_not_called()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), schema))

    def test_schema25_source_requires_catalog_and_migration_even_with_matching_manifest(self):
        for name in (RUNNER.catalog_name(25), RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_25_NAME):
            source = self.source_fixture("13", schema=25)
            del self.source_bytes[source / name]
            self.source_entries.remove(source / name)
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_source_rejects_future_or_duplicate_migrations_even_with_matching_manifest(self):
        for schema, name in ((24, RUNNER.MIGRATION_25_NAME), (25, "0026_future.sql"), (25, "0025_unowned.sql")):
            source = self.source_fixture(schema=schema)
            path = source / (RUNNER.MIGRATION_DIRECTORY + name)
            self.source_bytes[path] = b"Unreviewed migration.\n"
            self.source_metadata[path] = self.source_metadata[source / "go.mod"]
            self.source_entries.append(path)
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), schema))

    def test_source_rejects_future_catalogs_even_with_matching_manifest(self):
        source = self.source_fixture(schema=25)
        path = source / (RUNNER.CATALOG_DIRECTORY + "schema-26-postgresql-17.json")
        self.source_bytes[path] = b"{}"
        self.source_metadata[path] = self.source_metadata[source / "go.mod"]
        self.source_entries.append(path)
        self.refresh_source_manifest()
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_schema25_source_rejects_wrong_catalog_identity_and_migration_digest(self):
        for field, value in (("version", 24), ("version", 25.0), ("postgresql_major", 16)):
            source = self.source_fixture(schema=25)
            self.source_catalogs[25][field] = value
            self.change_catalog(25, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))
        source = self.source_fixture(schema=25)
        self.source_catalogs[25]["migrations"][-1]["sha256"] = "f" * 64
        self.change_catalog(25, reattest_synthetic=True)
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_source_rejects_rewritten_historical_catalog_and_migration_bytes(self):
        for version in (23, 24):
            source = self.source_fixture(schema=25)
            path = source / RUNNER.catalog_name(version)
            self.source_bytes[path] += b"\n"
            self.refresh_source_manifest()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))
        source = self.source_fixture(schema=25)
        path = source / (RUNNER.MIGRATION_DIRECTORY + RUNNER.MIGRATION_24_NAME)
        self.source_bytes[path] += b"Changed historical DDL.\n"
        self.refresh_source_manifest()
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_schema25_catalog_rejects_rewritten_history_and_wrong_migration_name(self):
        for index, field, value in ((0, "sha256", "f" * 64), (23, "name", "0024_foreign.sql"),
                                   (24, "name", "0025_foreign.sql"), (24, "version", 26), (24, "version", 25.0)):
            source = self.source_fixture(schema=25)
            self.source_catalogs[25]["migrations"][index][field] = value
            self.change_catalog(25, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_schema25_catalog_rejects_missing_and_extra_history(self):
        for extra in (False, True):
            source = self.source_fixture(schema=25)
            history = self.source_catalogs[25]["migrations"]
            if extra:
                history.append({"version": 26, "name": "0026_future.sql", "sha256": "f" * 64})
            else:
                history.pop()
            self.change_catalog(25, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_schema25_catalog_rejects_unowned_fields_at_every_descriptor_boundary(self):
        for boundary in ("root", "migration", "catalog", "table", "sequence", "consumer", "constraint", "object", "value"):
            source = self.source_fixture(schema=25)
            baseline = self.source_catalogs[25]
            catalog = baseline["catalog"]
            sequence = {"Name": "fixture_seq", "Table": "fixture_00", "Column": "id", "MinValue": 1, "MaxValue": 10,
                        "Increment": 1, "Consumers": [{"Table": "fixture_00", "Column": "id"}]}
            constraint = {"Table": "fixture_00", "Name": "fixture_fkey", "Definition": "Synthetic constraint"}
            catalog["Sequences"], catalog["Constraints"] = [sequence], [constraint]
            target = {"root": baseline, "migration": baseline["migrations"][-1], "catalog": catalog,
                "table": catalog["Tables"][0], "sequence": sequence, "consumer": sequence["Consumers"][0],
                "constraint": constraint, "object": baseline["objects"][0], "value": baseline["objects"][0]["value"]}[boundary]
            target["unowned"] = True
            self.change_catalog(25, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_schema25_catalog_rejects_fingerprint_drift_or_an_extra_table(self):
        for extra_table in (False, True):
            source = self.source_fixture(schema=25)
            baseline = self.source_catalogs[25]
            if extra_table:
                baseline["catalog"]["Tables"].append({"Name": "unowned", "Columns": ["id"], "PrimaryKey": ["id"], "SortKey": ["id"]})
            else:
                baseline["objects"][0]["value"]["row_security"] = True
            self.change_catalog(25, reattest_synthetic=True)
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_schema25_catalog_reread_requires_exact_manifest_bound_bytes(self):
        source = self.source_fixture(schema=25)
        catalog_path = source / RUNNER.catalog_name(25)
        self.replace(RUNNER, "private_read", lambda path, **kwargs: self.source_raw if path == source / RUNNER.MANIFEST else
            (self.source_bytes[path] + b"\n" if path == catalog_path else self.source_bytes[path]))
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw), 25))

    def test_source_root_requires_root_ownership_and_exact_0700_directory(self):
        source = self.source_fixture()
        original = self.source_metadata[source]
        for overrides in ({"st_uid": 1000}, {"st_gid": 1000},
                          {"st_mode": stat.S_IFDIR | 0o755}, {"st_mode": stat.S_IFDIR | 0o770},
                          {"st_mode": stat.S_IFREG | 0o700}):
            self.source_metadata[source] = types.SimpleNamespace(**dict(vars(original), **overrides))
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_source_rejects_symlink_at_root_or_any_parent(self):
        source = self.source_fixture()
        for path in (source, *source.parents):
            with self.subTest(path=str(path)):
                original = self.source_metadata.get(path)
                self.source_metadata[path] = types.SimpleNamespace(st_mode=stat.S_IFLNK | 0o777)
                self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))
                if original is None:
                    del self.source_metadata[path]
                else:
                    self.source_metadata[path] = original

    def test_source_files_require_root_ownership_safe_permissions_and_single_regular_link(self):
        source = self.source_fixture()
        path = source / "go.mod"
        original = self.source_metadata[path]
        for overrides in ({"st_uid": 1000}, {"st_gid": 1000}, {"st_nlink": 2},
                          {"st_mode": stat.S_IFREG | 0o664}, {"st_mode": stat.S_IFREG | 0o646},
                          {"st_mode": stat.S_IFLNK | 0o644}, {"st_mode": stat.S_IFIFO | 0o600}):
            self.source_metadata[path] = types.SimpleNamespace(**dict(vars(original), **overrides))
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_source_directories_require_root_ownership_and_safe_permissions(self):
        source = self.source_fixture()
        path = source / "internal"
        original = self.source_metadata[path]
        for overrides in ({"st_uid": 1000}, {"st_gid": 1000},
                          {"st_mode": stat.S_IFDIR | 0o775}, {"st_mode": stat.S_IFDIR | 0o757}):
            self.source_metadata[path] = types.SimpleNamespace(**dict(vars(original), **overrides))
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_source_manifest_rejects_changed_file_bytes(self):
        source = self.source_fixture()
        self.source_bytes[source / "go.mod"] += b"changed\n"
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_source_manifest_rejects_missing_and_extra_files(self):
        source = self.source_fixture()
        path = source / "go.mod"
        self.source_entries.remove(path)
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))
        self.source_entries.append(path)
        extra = source / "extra.txt"
        self.source_bytes[extra] = b"unlisted\n"
        self.source_metadata[extra] = self.source_metadata[path]
        self.source_entries.append(extra)
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_source_manifest_requires_historical_and_current_backup_inputs(self):
        for name in ("internal/backuppg/catalogs/schema-23-postgresql-17.json",
                     "internal/backuppg/catalogs/schema-24-postgresql-17.json",
                     "internal/database/migrations/0024_user_settings.sql"):
            source = self.source_fixture()
            self.source_entries.remove(source / name)
            del self.source_manifest["files"][name]
            self.source_raw = json.dumps(self.source_manifest, sort_keys=True).encode()
            self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_source_manifest_requires_more_than_100_files(self):
        source = self.source_fixture()
        self.source_entries = self.source_entries[:101]
        self.source_manifest["files"] = {str(path.relative_to(source)): RUNNER.sha(self.source_bytes[path])
            for path in self.source_entries if path in self.source_bytes}
        self.source_raw = json.dumps(self.source_manifest, sort_keys=True).encode()
        self.reject(lambda: RUNNER.verify_source(source, RUNNER.sha(self.source_raw)))

    def test_source_manifest_rejects_changed_digest_before_tree_walk(self):
        self.replace(RUNNER, "directory", Mock(return_value={"device": 1, "inode": 2}))
        read = self.replace(RUNNER, "private_read", Mock(return_value=b"changed manifest"))
        self.reject(lambda: RUNNER.verify_source(self.args.source, "a" * 64))
        read.assert_called_once_with(self.args.source / RUNNER.MANIFEST, modes=(0o600, 0o644))

    def test_source_manifest_rejects_unknown_marker_and_duplicate_membership(self):
        self.replace(RUNNER, "directory", Mock(return_value={"device": 1, "inode": 2}))
        for raw in (b'{"marker":"foreign","files":{}}',
                    b'{"marker":"goby-client-backup-source-m3e-v1","files":{},"files":{}}',
                    b'{"marker":"goby-client-backup-source-m3e-v1","files":{"go.mod":"a","go.mod":"b"}}',
                    b'{"marker":"goby-client-backup-source-m3e-v1","files":[]}',
                    b'{"marker":"goby-client-backup-source-m3e-v1","files":{},"extra":true}'):
            self.replace(RUNNER, "private_read", Mock(return_value=raw))
            self.reject(lambda raw=raw: RUNNER.verify_source(self.args.source, RUNNER.sha(raw)))

    def cluster_fixture(self):
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        self.process = {"pid": 123, "start_ticks": 34567, "boot_id": "01234567-89ab-cdef-0123-456789abcdef"}
        self.cluster_owner = {"system_identifier": "1234567890123456789", "process": self.process}
        raw = json.dumps(self.cluster_owner)
        runner.cluster_owner_sha = RUNNER.sha(raw.encode())
        runner.postgres = types.SimpleNamespace(pw_uid=102, pw_gid=105)
        runner.binary_identity = {}
        runner.cluster = copy.deepcopy(self.cluster_owner)
        runner.module = types.SimpleNamespace(OWNER=RUNNER.CONTROL / "owner.json", HBA=BASE_HBA.decode(), CONF="trusted configuration\n",
            read_file=Mock(side_effect=lambda path, **kwargs: raw if path.name == "owner.json" else
                ("trusted configuration\n" if path.name == "postgresql.conf" else "# no overrides\n")),
            validate_owner=Mock(), expected_owner=Mock(return_value={}), directory=Mock(return_value={}),
            verify_directories=Mock(), cluster_identifier=Mock(return_value=self.cluster_owner["system_identifier"]),
            verify_process=Mock(return_value=self.process), verify_server=Mock())
        self.replace(RUNNER, "private_read", Mock(return_value=BASE_HBA))
        return runner

    def test_cluster_requires_recorded_process_system_identifier_and_permanent_owner_bytes(self):
        runner = self.cluster_fixture()
        runner.check_cluster()
        runner.module.validate_owner.assert_called_once()
        runner.module.verify_directories.assert_called_once()
        runner.module.verify_server.assert_called_once()
        runner.module.cluster_identifier.return_value = "9999999999999999999"
        self.reject(runner.check_cluster)
        runner.module.cluster_identifier.return_value = self.cluster_owner["system_identifier"]
        for process in (None, dict(self.process, pid=124), dict(self.process, start_ticks=34568),
                        dict(self.process, boot_id="fedcba98-7654-3210-fedc-ba9876543210")):
            runner.module.verify_process.return_value = process
            self.reject(runner.check_cluster)
        runner.module.verify_process.return_value = self.process
        runner.cluster_owner_sha = "f" * 64
        self.reject(runner.check_cluster)

    def test_cluster_rejects_hba_and_override_changes_after_initial_validation(self):
        runner = self.cluster_fixture()
        self.replace(RUNNER, "private_read", Mock(return_value=b"foreign hba"))
        self.reject(runner.check_cluster)
        self.replace(RUNNER, "private_read", Mock(return_value=BASE_HBA))
        original = runner.module.read_file.side_effect
        runner.module.read_file.side_effect = lambda path, **kwargs: "port=5432\n" if path.name == "postgresql.auto.conf" else original(path, **kwargs)
        self.reject(runner.check_cluster)

    def test_prepare_refuses_nonlinux_nonroot_and_nonssh_before_effects(self):
        runner = RUNNER.Runner.__new__(RUNNER.Runner)
        runner.args = self.args
        self.replace(sys, "platform", "win32")
        self.reject(runner.prepare)
        self.replace(sys, "platform", "linux")
        self.replace(os, "geteuid", lambda: 1000)
        self.reject(runner.prepare)
        self.replace(os, "geteuid", lambda: 0)
        self.enterContext(patch.dict(os.environ, {}, clear=True))
        self.reject(runner.prepare)


def main():
    global RUNNER
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run memory-only guard tests through authorized root SSH.")
    if len(sys.argv) != 2:
        raise SystemExit("Usage: test-run-client-backup-tests.py RUNNER_SOURCE")
    source = Path(sys.argv[1])
    for path in (source, Path(__file__)):
        SOURCE_LINES[str(path)] = path.read_text().splitlines(keepends=True)
    spec = importlib.util.spec_from_file_location("client_backup_runner", source)
    RUNNER = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(RUNNER)
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RunnerGuardTests))
    print("Memory-only guards: no PostgreSQL command, cluster mutation, filesystem mutation, or connection was performed.")
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
