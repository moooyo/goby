#!/usr/bin/env python3
"""Run six post-start operator guard groups using fenced memory fixtures only."""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import datetime as dt
import hashlib
import http.client
import io
import json
import linecache
import os
from pathlib import Path
import re
import secrets
import socket
import stat
import subprocess
import sys
import types
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
OPERATOR = None
SOURCE_LINES = {}
FENCES = []


def audit(event, _arguments):
    if FENCES and (event in {"open", "os.listdir", "os.scandir", "os.remove", "os.rmdir", "os.mkdir", "os.rename",
            "os.chmod", "os.chown", "os.link", "os.symlink", "os.truncate", "os.utime", "os.kill", "os.killpg", "os.system"}
            or event.startswith(("socket.", "subprocess.", "os.exec", "os.spawn", "os.fork", "fcntl."))):
        FENCES[-1].append(event)
        raise AssertionError("Unexpected audited external effect: " + event)


@contextlib.contextmanager
def fenced():
    violations = []
    FENCES.append(violations)
    try:
        with contextlib.ExitStack() as scope:
            scope.enter_context(patch.object(linecache, "checkcache", lambda *_args: None))
            scope.enter_context(patch.object(linecache, "getlines", lambda name, *_args: SOURCE_LINES.get(str(name), [])))
            for owner, names in (
                (builtins, ("open",)), (io, ("open", "open_code", "FileIO")),
                (Path, ("open", "stat", "lstat", "exists", "resolve", "absolute", "is_file", "is_dir", "is_symlink",
                        "read_bytes", "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink", "glob", "rglob", "iterdir")),
                (os, ("open", "fdopen", "stat", "lstat", "fstat", "read", "write", "close", "readlink", "listdir", "scandir",
                      "mkdir", "remove", "unlink", "rename", "replace", "chmod", "chown", "link", "symlink", "system", "popen", "kill")),
                (subprocess, ("Popen", "run", "call", "check_call", "check_output")),
                (socket, ("socket", "create_connection", "getaddrinfo")),
                (http.client, ("HTTPConnection", "HTTPSConnection")), (secrets, ("token_hex", "token_bytes", "token_urlsafe")),
            ):
                for name in names:
                    if not hasattr(owner, name):
                        continue
                    label = getattr(owner, "__name__", type(owner).__name__) + "." + name

                    def deny(*_args, _label=label, **_kwargs):
                        violations.append(_label)
                        raise AssertionError("Unexpected external effect: " + _label)

                    scope.enter_context(patch.object(owner, name, deny))
            yield
    finally:
        FENCES.pop()
        if violations:
            raise AssertionError("External effects attempted: " + ", ".join(violations))


def info(*, mode=0o600, kind=stat.S_IFREG, uid=0, gid=0, links=1, size=0, inode=100):
    return types.SimpleNamespace(st_dev=7, st_ino=inode, st_uid=uid, st_gid=gid, st_nlink=links,
        st_mode=kind | mode, st_size=size, st_mtime_ns=1000000001, st_ctime_ns=1000000002)


class PostStartGuards(unittest.TestCase):
    def setUp(self):
        self.enterContext(fenced())
        self.enterContext(contextlib.redirect_stdout(io.StringIO()))

    def replace(self, owner, name, value):
        return self.enterContext(patch.object(owner, name, value))

    def reject(self, action):
        with self.assertRaises(OPERATOR.Failure):
            action()

    def memory(self):
        files, directories, events = {}, {OPERATOR.ROOT.parent}, []
        fault = {"name": None}

        def write(path, raw, mode=0o600):
            if path.name == fault["name"]:
                raise OSError("private publication failure")
            self.assertEqual(mode, 0o600)
            self.assertNotIn(path, files)
            self.assertNotIn(path, directories)
            files[path] = bytes(raw)
            events.append(("write", path))

        def mkdir(path, mode=0o777, **options):
            self.assertEqual((mode, options), (0o700, {}))
            self.assertNotIn(path, files)
            self.assertNotIn(path, directories)
            directories.add(path)
            events.append(("mkdir", path))

        self.replace(os.path, "lexists", lambda path: path in files or path in directories)
        self.replace(Path, "mkdir", mkdir)
        dependency = types.SimpleNamespace(read_file=lambda path, **_kwargs: files[path], write_exclusive=write,
            directory=lambda path, *_args, **_kwargs: self.assertIn(path, directories), sync_directory=lambda *_args: None)
        return dependency, files, directories, events, fault

    def retained(self):
        op, files, directories, events, fault = self.memory()
        main = {"database": "goby_test", "database_oid": 16385, "role": "goby_test", "role_oid": 16384}
        module = types.SimpleNamespace(LOCK_HELD=True, ROOT=OPERATOR.FAILED_ROOT,
            MARKER="goby-main-schema26-upgrade-v1", MAIN=main)

        def compare(left, right):
            OPERATOR.require(left["state_sha256"] == right["state_sha256"] and left["state"] == right["state"], "Retained states differ.")

        module.compare_snapshots = compare
        op.safe_relative = lambda name: name
        source_args = types.SimpleNamespace(startup_plan_sha256="1" * 64)
        inputs = {"source_manifest_sha256": "2" * 64, "candidate": {"sha256": "3" * 64},
            "helper": {"sha256": "4" * 64}, "tool_source": {"manifest_sha256": "5" * 64},
            "asset_members": {f"asset-{index}.js": "6" * 64 for index in range(57)}}
        run = OPERATOR.FAILED_RUN
        directories.update({OPERATOR.FAILED_ROOT, run})
        put = lambda path, value: files.__setitem__(path, OPERATOR.canonical(value))
        put(OPERATOR.FAILED_ROOT / "OWNER.json", {"marker": module.MARKER, "version": 1, "path": str(OPERATOR.FAILED_ROOT), "run_id": run.name})
        put(run / "OWNER.json", {"marker": module.MARKER, "version": 1, "run_id": run.name})
        put(run / "inputs.json", inputs)
        actions = ["repair-rehearsal", "save-materials", "baseline-dump", "rehearsal-role", "rehearsal-database", "rehearsal-mark",
            "rehearsal-restore", "rehearsal-migrate", "rehearsal-close", "rehearsal-drop-database", "rehearsal-drop-role", "main-migrate"]
        actions += ["publish-file"] * 58 + ["start"]
        previous = None
        for number, action in enumerate(actions, 1):
            path = run / f"intent-{number:04d}.json"
            put(path, {"marker": module.MARKER, "run_id": run.name, "sequence": number, "action": action,
                "previous_sha256": previous, "inputs_sha256": OPERATOR.sha(files[run / "inputs.json"])})
            previous = OPERATOR.sha(files[path])
        terminal = {"status": "failed", "last_completed_phase": "start-requested", "last_intent_sha256": previous,
            "reason": "The candidate process has an unexpected effective identity.", "automatic_recovery_executed": False,
            "main_database_restored": False, "old_binary_rollback": False}
        put(run / "terminal.json", terminal)
        reports = {}
        for name, mode, status, version in (("baseline", "inspect", "inspected", 25),
                ("main-migrated", "migrate", "committed", 25), ("main-after-migration", "inspect", "inspected", 26)):
            reports[name] = {"schema": "goby-main-schema26-migration", "mode": mode, "status": status,
                "source_schema_version": version, "target_schema_version": 26, "repair_rehearsal": True,
                "repair_baseline": False, "rehearsal": False, "run_id": run.name, "target": main,
                "source_manifest_sha256": inputs["source_manifest_sha256"], "tool_manifest_sha256": inputs["tool_source"]["manifest_sha256"],
                "candidate_sha256": inputs["candidate"]["sha256"], "helper_sha256": inputs["helper"]["sha256"],
                "state_sha256": "a" * 64 if name == "baseline" else "b" * 64,
                "state": {"schema": 25 if name == "baseline" else 26, "old_rows": "preserved"}}
        put(run / "baseline.json", reports["baseline"])
        reports["main-migrated"].update(before_state_sha256="a" * 64, preserved_state_sha256="a" * 64,
            input_baseline_sha256=OPERATOR.sha(files[run / "baseline.json"]), new_theme_defaults_verified=True)
        for name in ("main-migrated", "main-after-migration"):
            put(run / (name + ".json"), reports[name])
        put(run / "rehearsal-disposed.json", {"status": "disposed", "force_or_backend_termination_used": False,
            "after": {"database": None, "role": None}})
        before = {"stores": {"backups": [{"id": "old-archive", "sha256": "c" * 64}]}, "private": "unchanged",
                  "failed_upgrade": {"first": "retained"}, "failed_baseline_repair": {"second": "retained"}}
        module.read_rehearsal_failure_evidence = lambda *_args: {
            "first_failed_tree": copy.deepcopy(before["failed_upgrade"]), "failed_tree": copy.deepcopy(before["failed_baseline_repair"])}
        put(run / "protected-before.json", before)
        op.path_info = lambda path: info(kind=stat.S_IFDIR if path in directories else stat.S_IFREG,
            mode=0o700 if path in directories else 0o600, size=len(files[path]) if path in files else 0)
        self.replace(Path, "rglob", lambda path, pattern: sorted(member for member in files.keys() | directories
            if member != path and member.is_relative_to(path)))
        active = {"pid": OPERATOR.PID, "start_ticks": OPERATOR.TICKS, "listener": {"inode": OPERATOR.SOCKET}}
        observation = {"root": str(OPERATOR.FAILED_ROOT), "run": str(run), "smoke_artifacts_absent": True,
            "private_state_preserved": True, "post_start_all_table_equality_claimed": False,
            "startup_plan_sha256": source_args.startup_plan_sha256, "intent_actions": actions, "terminal": terminal,
            "active_service": active, "primary": {"pid": 777, "start_ticks": "100001"}}

        def refresh():
            observation["inventory"] = OPERATOR.failed_inventory(op)
            for name, key in (("baseline", "baseline_sha256"), ("main-migrated", "migration_report_sha256"),
                             ("main-after-migration", "post_migration_report_sha256"), ("rehearsal-disposed", "rehearsal_disposed_sha256")):
                observation[key] = OPERATOR.sha(files[run / (name + ".json")])
            observation["terminal"] = OPERATOR.decode(files[run / "terminal.json"])
            put(OPERATOR.OBSERVATION, observation)
            self.replace(OPERATOR, "OBSERVATION_SHA", OPERATOR.sha(files[OPERATOR.OBSERVATION]))

        refresh()
        return types.SimpleNamespace(op=op, module=module, source_args=source_args, inputs=inputs, files=files,
            directories=directories, events=events, fault=fault, observation=observation, reports=reports, put=put, refresh=refresh, before=before)

    def test_input_provenance_modes_json_and_error_privacy(self):
        own = OPERATOR.WORK / "tool-build-closeout/complete-main-schema26.py"
        source = OPERATOR.WORK / "tool-build-frozen/scripts/test-env/upgrade-main-schema26.py"
        args = types.SimpleNamespace(mode="preflight", upgrade_source=source,
            upgrade_arguments=OPERATOR.WORK / "original-arguments.json", guard_report=OPERATOR.WORK / "closeout-guards.json")
        synthetic = ("import types\ndef parser():\n return types.SimpleNamespace(parse_args=lambda argv: "
            "types.SimpleNamespace(mode=argv[1],script_sha256=argv[3]))\n"
            "def validate_arguments(args):\n pass\ndef load_published_operator(args):\n return types.SimpleNamespace(), {'synthetic':True}\n").encode()
        self.replace(OPERATOR, "UPGRADE_SHA", OPERATOR.sha(synthetic))
        documents = {own: b"synthetic reviewed closeout", own.with_name(OPERATOR.GUARD_NAME): b"synthetic reviewed guard", source: synthetic}
        args.script_sha256, args.upgrade_source_sha256 = OPERATOR.sha(documents[own]), OPERATOR.UPGRADE_SHA
        vector = {"schema": "goby-main-schema26-upgrade-arguments", "version": 1,
            "argv": ["--mode", "rehearsal-repair", "--script-sha256", OPERATOR.UPGRADE_SHA, "--padding", *(["synthetic"] * 15)]}
        guards = {"suite": "main-schema26-post-start-guards", "status": "passed", "tests": 6, "errors": 0, "failures": 0, "skips": 0,
            "operator_sha256": args.script_sha256, "guard_sha256": OPERATOR.sha(documents[own.with_name(OPERATOR.GUARD_NAME)]), "fixtures": "synthetic-memory-only"}

        def refresh():
            for name, value in (("upgrade_arguments", vector), ("guard_report", guards)):
                documents[getattr(args, name)] = OPERATOR.canonical(value)
                setattr(args, name + "_sha256", OPERATOR.sha(documents[getattr(args, name)]))

        self.replace(Path, "absolute", lambda path: own)
        self.replace(OPERATOR, "read_control", lambda path, **_kwargs: documents[path])
        refresh()
        self.assertEqual(OPERATOR.load_inputs(args)[2].mode, "rehearsal-repair")
        for mode in ("upgrade", "resume", "restart", "repair-baseline", "", None):
            changed = copy.copy(args)
            changed.mode = mode
            self.reject(lambda: OPERATOR.load_inputs(changed))
        for field, value in (("upgrade_source", Path("/tmp/upgrade-main-schema26.py")), ("upgrade_source_sha256", "0" * 64),
                             ("script_sha256", "0" * 64), ("guard_report_sha256", "0" * 64)):
            changed = copy.copy(args)
            setattr(changed, field, value)
            self.reject(lambda: OPERATOR.load_inputs(changed))
        guards["tests"] = 5
        refresh()
        self.reject(lambda: OPERATOR.load_inputs(args))
        guards["tests"] = 6
        vector["argv"][1] = "repair-baseline"
        refresh()
        self.reject(lambda: OPERATOR.load_inputs(args))
        for raw in (b'{"x":1,"x":2}', b'{"n":NaN}', b'{"n":Infinity}'):
            self.reject(lambda: OPERATOR.decode(raw))
        self.assertEqual(OPERATOR.reason(None, None, RuntimeError("private credential sentinel")), "RuntimeError")

    def test_private_controls_reject_links_modes_and_changed_opened_identity(self):
        path = OPERATOR.WORK / "control.json"
        for attributes in ({"uid": 995}, {"gid": 986}, {"links": 2}, {"mode": 0o644}, {"kind": stat.S_IFLNK}, {"size": 17 << 20}):
            with self.subTest(attributes=attributes), patch.object(Path, "lstat", lambda _path, values=attributes: info(**values)):
                self.reject(lambda: OPERATOR.read_control(path))
        raw = b"private synthetic control"
        before = info(size=len(raw))
        self.replace(Path, "lstat", lambda _path: before)
        self.replace(os, "open", lambda *_args: 99)
        class Handle(io.BytesIO):
            def fileno(self):
                return 99

        handle = Handle(raw)
        self.replace(os, "fdopen", lambda *_args: handle)
        self.replace(os, "fstat", lambda *_args: info(size=len(raw), inode=101))
        self.reject(lambda: OPERATOR.read_control(path))

    def test_retained_history_requires_exact_commit_disposal_and_no_prior_smoke(self):
        model = self.retained()
        read = lambda: OPERATOR.retained_evidence(model.module, model.op, model.source_args, model.inputs)
        evidence = read()
        self.assertEqual(evidence["inputs_sha256"], OPERATOR.sha(model.files[OPERATOR.FAILED_RUN / "inputs.json"]))
        original_files, original_observation = copy.deepcopy((model.files, model.observation))

        def reset():
            model.files.clear()
            model.files.update(copy.deepcopy(original_files))
            model.observation.clear()
            model.observation.update(copy.deepcopy(original_observation))
            model.refresh()

        for name in ("inputs.json", "intent-0071.json", "main-migrated.json", "rehearsal-disposed.json"):
            reset()
            model.files[OPERATOR.FAILED_RUN / name] += b"unreviewed drift"
            self.reject(read)
        for name in ("smoke-session-private.json", "native-smoke.json", "intent-0072.json"):
            reset()
            model.files[OPERATOR.FAILED_RUN / name] = b"unknown previous attempt"
            model.refresh()
            self.reject(read)
        for filename, key, value in (("terminal.json", "last_completed_phase", "ready"),
                ("main-migrated.json", "preserved_state_sha256", "0" * 64),
                ("main-after-migration.json", "state_sha256", "0" * 64),
                ("protected-before.json", "failed_baseline_repair", {"foreign": "evidence"}),
                ("rehearsal-disposed.json", "force_or_backend_termination_used", True)):
            reset()
            path = OPERATOR.FAILED_RUN / filename
            document = OPERATOR.decode(model.files[path])
            document[key] = value
            model.put(path, document)
            model.refresh()
            self.reject(read)
        reset()
        model.module.LOCK_HELD = False
        self.reject(read)

    def test_live_checks_pin_the_running_process_primary_and_schema26(self):
        model = self.retained()
        evidence = OPERATOR.retained_evidence(model.module, model.op, model.source_args, model.inputs)
        active, primary = copy.deepcopy(model.observation["active_service"]), copy.deepcopy(model.observation["primary"])
        current = {**model.module.MAIN, "owner_oid": 16384, "schema": 26, "activity_total": 15,
            "theme_owners": 22, "reserved_paths": 0, "theme_resources": 0, "rehearsal_databases": 0, "rehearsal_roles": 0,
            "safe": True, "memberships": 0}
        model.module.exact_service = lambda *_args, **_kwargs: copy.deepcopy(active)
        model.module.primary_proof = lambda *_args: copy.deepcopy(primary)
        model.module.preserved_private = Mock(return_value={})
        model.module.startup_checkpoint = lambda **_kwargs: {"accepted": True}
        model.op.runtime_policy = lambda: "private synthetic URL"
        model.op.database_environment = lambda *_args, **_kwargs: {"PGPORT": "5432", "PGDATABASE": "goby_test"}

        def query(_op, statement, **options):
            self.assertIn("BEGIN READ ONLY", statement)
            self.assertEqual(options["database"], "goby_test")
            self.assertNotRegex(statement, r"(?i)\b(DELETE|UPDATE|ALTER|CREATE|DROP|TRUNCATE)\b")
            return OPERATOR.canonical(current)

        model.module.pg = query
        check = lambda **options: OPERATOR.live_checks(model.module, model.op, model.source_args, model.inputs, evidence, **options)
        self.assertEqual(check()["active_service"], active)
        for container, key, value in ((active, "pid", 688834), (active, "start_ticks", "5620919"), (active["listener"], "inode", "foreign"),
                (primary, "pid", 778), (current, "schema", 25), (current, "database_oid", 994944), (current, "activity_total", 16),
                (current, "theme_owners", 21), (current, "theme_resources", 1), (current, "reserved_paths", 1),
                (current, "rehearsal_databases", 1), (current, "rehearsal_roles", 1), (current, "safe", False), (current, "memberships", 1)):
            previous = container[key]
            container[key] = value
            self.reject(check)
            container[key] = previous
        current["activity_total"] = 17
        self.assertEqual(check(after_smoke=True)["database"]["activity_total"], 17)
        current["activity_total"] = 14
        self.reject(lambda: check(after_smoke=True))

    def test_smoke_journal_is_exclusive_input_bound_and_limited_to_one_session(self):
        op, files, directories, events, fault = self.memory()
        run = OPERATOR.ROOT / ("run-20260911T140000Z-" + "a" * 24)
        directories.update({OPERATOR.ROOT, run})
        files[run / "OWNER.json"] = OPERATOR.canonical({"marker": OPERATOR.MARKER, "version": 1, "run_id": run.name})
        files[run / "inputs.json"] = OPERATOR.canonical({"synthetic": "reviewed binding"})
        gate = Mock(return_value={"accepted": True})
        module = types.SimpleNamespace(LOCK_HELD=True, startup_checkpoint=gate)
        journal = OPERATOR.SmokeJournal(module, op, run, OPERATOR.sha(files[run / "inputs.json"]))
        module.LOCK_HELD = False
        self.reject(lambda: journal.intent("smoke-login", {}))
        module.LOCK_HELD = True
        for action in ("start", "stop", "main-migrate", "smoke-logout"):
            self.reject(lambda: journal.intent(action, {}))
        self.assertEqual(gate.call_count, 0)
        gate.side_effect = OPERATOR.Failure("startup deadline elapsed")
        self.reject(lambda: journal.intent("smoke-login", {}))
        self.assertEqual((journal.sequence, journal.previous), (0, None))
        gate.side_effect = None
        original_inputs = files[run / "inputs.json"]
        files[run / "inputs.json"] += b"drift"
        self.reject(lambda: journal.intent("smoke-login", {}))
        files[run / "inputs.json"] = original_inputs
        first = journal.intent("smoke-login", {"conditional_cleanup": {"only_this_new_login_cookie": True}})
        fault["name"] = "intent-0002.json"
        with self.assertRaises(OSError):
            journal.intent("smoke-logout", {})
        self.assertEqual((journal.sequence, journal.previous), (1, first))
        fault["name"] = None
        original_intent = files[run / "intent-0001.json"]
        files[run / "intent-0001.json"] += b"drift"
        self.reject(lambda: journal.intent("smoke-logout", {}))
        files[run / "intent-0001.json"] = original_intent
        second = journal.intent("smoke-logout", {"private_session_sha256": "b" * 64})
        self.assertEqual((journal.sequence, journal.previous), (2, second))
        self.reject(lambda: journal.intent("smoke-login", {}))
        self.reject(lambda: journal.intent("smoke-logout", {}))
        old = OPERATOR.SmokeJournal(module, op, OPERATOR.FAILED_RUN, "c" * 64)
        self.reject(lambda: old.intent("smoke-login", {}))

    def test_completion_only_calls_owned_smoke_and_never_retries_failed_evidence(self):
        for failure in (None, "pre-smoke", "smoke", "post-smoke", "logout-proof", "inputs.json"):
            with self.subTest(failure=failure), contextlib.ExitStack() as scope:
                op, files, directories, events, fault = self.memory()
                old_files = {OPERATOR.FAILED_RUN / "terminal.json": b"retained failed terminal",
                             OPERATOR.FAILED_RUN / "database-schema25.dump": b"retained old dump"}
                files.update(old_files)
                args = types.SimpleNamespace(script_sha256="1" * 64, upgrade_arguments_sha256="2" * 64, guard_report_sha256="3" * 64)
                inputs = {"candidate": {"sha256": "4" * 64}}
                observed = {"active_service": {"pid": OPERATOR.PID, "start_ticks": OPERATOR.TICKS}}
                evidence = {"inputs_sha256": "5" * 64, "before": {"stores": {"backups": []}}}
                calls = []
                module = types.SimpleNamespace(LOCK_HELD=True, startup_checkpoint=lambda *_args, **_kwargs: {"accepted": True},
                    failure_reason=lambda _op, error: type(error).__name__)

                def live(*_args, after_smoke=False):
                    phase = "post-smoke" if after_smoke else "pre-smoke"
                    calls.append(phase)
                    if failure == phase:
                        raise RuntimeError("private credential sentinel")
                    return copy.deepcopy(observed)

                def smoke(_op, _source_args, _inputs, journal, active, backups):
                    self.assertEqual((active, backups), (observed["active_service"], []))
                    calls.append("smoke")
                    journal.intent("smoke-login", {"conditional_cleanup": {"only_this_new_login_cookie": True}})
                    if failure == "smoke":
                        raise RuntimeError("private credential sentinel")
                    journal.intent("smoke-logout", {"private_session_sha256": "6" * 64})
                    op.write_exclusive(journal.run / "native-smoke.json", OPERATOR.canonical({
                        "logout_verified": failure != "logout-proof", "new_session_only": True, "cleanup_checkpoint_error_type": None}))

                module.native_smoke = smoke
                for name in ("upgrade", "invoke_helper", "stop_main", "install_candidate", "rehearse"):
                    setattr(module, name, Mock(side_effect=AssertionError("Forbidden deployment action: " + name)))
                op.command = Mock(side_effect=AssertionError("No command is allowed during closeout."))
                scope.enter_context(patch.object(OPERATOR, "live_checks", live))
                scope.enter_context(patch.object(secrets, "token_hex", return_value="d" * 24))
                if failure == "inputs.json":
                    fault["name"] = failure
                execute = lambda: OPERATOR.complete(args, module, op, types.SimpleNamespace(), inputs, evidence, observed)
                if failure is None:
                    self.assertEqual(execute()["status"], "passed")
                    self.assertEqual(calls, ["pre-smoke", "smoke", "post-smoke"])
                else:
                    with self.assertRaises((RuntimeError, OSError, OPERATOR.Failure)):
                        execute()
                terminals = [OPERATOR.decode(raw) for path, raw in files.items() if path.is_relative_to(OPERATOR.ROOT) and path.name == "terminal.json"]
                self.assertEqual(len(terminals), 0 if failure == "inputs.json" else 1)
                if terminals:
                    self.assertEqual(terminals[0]["status"], "passed" if failure is None else "failed")
                    self.assertEqual([terminals[0][key] for key in ("service_mutations", "database_migrations", "database_restores")], [0, 0, 0])
                    self.assertNotIn("private credential sentinel", OPERATOR.canonical(terminals[0]).decode())
                self.assertEqual({path: files[path] for path in old_files}, old_files)
                saved_files, saved_calls = dict(files), list(calls)
                self.reject(execute)
                self.assertEqual((files, calls), (saved_files, saved_calls))
                op.command.assert_not_called()


def main():
    global OPERATOR
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "main-schema26-post-start-guards", "status": "blocked", "reason": "Authorized root SSH and one reviewed operator source are required."}))
        return 2
    source = Path(sys.argv[1]).resolve(strict=True)
    if source.name != "complete-main-schema26.py":
        raise SystemExit("Only the new post-start completion operator is accepted.")
    raw, own = source.read_bytes(), Path(__file__).read_bytes()
    SOURCE_LINES[str(source)], SOURCE_LINES[__file__] = raw.decode().splitlines(keepends=True), own.decode().splitlines(keepends=True)
    OPERATOR = types.ModuleType("memory_schema26_post_start")
    OPERATOR.__file__ = str(source)
    sys.addaudithook(audit)
    with fenced():
        exec(compile(raw, str(source), "exec"), OPERATOR.__dict__)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(PostStartGuards)
    names = sorted(test._testMethodName for test in suite)
    result = unittest.TestResult()
    suite.run(result)
    passed = result.wasSuccessful() and not result.skipped and result.testsRun == 6
    print(json.dumps({"suite": "main-schema26-post-start-guards", "status": "passed" if passed else "failed",
        "tests": result.testsRun, "errors": len(result.errors), "failures": len(result.failures), "skips": len(result.skipped),
        "cases": names, "operator_sha256": hashlib.sha256(raw).hexdigest(), "guard_sha256": hashlib.sha256(own).hexdigest(),
        "fixtures": "synthetic-memory-only", "real_deployment_acceptance": False,
        "failure_summaries": [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in [*result.errors, *result.failures][:6]]}))
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
