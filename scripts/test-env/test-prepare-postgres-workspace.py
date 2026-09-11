#!/usr/bin/env python3
"""Run memory-only operator guards through authorized root SSH.

The only filesystem reads precede the effect fence and load the operator.
Tests never start PostgreSQL, touch a cluster, or contact a database.
"""

from __future__ import annotations

import contextlib
import copy
import fcntl
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import pwd
import select
import signal
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


class EffectFence(contextlib.ExitStack):
    """Prevent accidental real filesystem, subprocess, or network effects."""

    def __enter__(self):
        super().__enter__()
        import builtins
        self.violations = []
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       SOURCE_LINES.get(str(filename), [])))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "FileIO")),
            (os, ("open", "close", "fdopen", "fstat", "stat", "lstat", "mkdir", "chmod", "chown", "fchown",
                  "fsync", "replace", "unlink", "remove", "rmdir", "statvfs", "umask", "kill", "system", "popen",
                  "pidfd_open", "dup")),
            (Path, ("lstat", "stat", "exists", "is_symlink", "resolve", "readlink", "read_bytes", "read_text",
                    "write_bytes", "write_text", "mkdir", "iterdir", "rglob", "unlink")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (pwd, ("getpwnam",)),
            (fcntl, ("flock",)),
            (select, ("poll",)),
            (signal, ("pidfd_send_signal",)),
        )
        for owner, names in targets:
            for name in names:
                label = owner.__name__ + "." + name

                def denied(*_arguments, _label=label, **_options):
                    self.violations.append(_label)
                    raise AssertionError("Unexpected external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments):
        super().__exit__(*arguments)
        if self.violations:
            raise AssertionError("External effects attempted: " + ", ".join(self.violations))


class WorkspaceGuardTests(unittest.TestCase):
    def setUp(self):
        self.fence = self.enterContext(EffectFence())
        self.output = self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.pg = types.SimpleNamespace(pw_uid=102, pw_gid=105)
        self.identity = {"device": 41, "inode": 51}
        self.process = {"pid": 321, "start_ticks": 123456,
                        "boot_id": "01234567-89ab-cdef-0123-456789abcdef"}
        self.expected = OPERATOR.expected_owner(self.pg, {"postgres": {"sha256": "synthetic"}})
        self.owner = dict(self.expected, control_identity=self.identity,
                          directories={str(path): self.identity for path in
                                       (OPERATOR.BASE, OPERATOR.DATA, OPERATOR.SOCKET, OPERATOR.LOG)},
                          phase="initialized", system_identifier="1234567890123456789", process=self.process)

    def replace(self, owner, name, value):
        return self.enterContext(patch.object(owner, name, value))

    def fake_stat(self, *, kind=stat.S_IFREG, mode=0o600, uid=0, gid=0, links=1, inode=51):
        return types.SimpleNamespace(st_mode=kind | mode, st_uid=uid, st_gid=gid, st_nlink=links,
                                     st_dev=41, st_ino=inode, st_size=10)

    def reject(self, callback):
        with self.assertRaises(OPERATOR.WorkspaceError):
            callback()

    def test_owner_accepts_complete_exact_identity(self):
        OPERATOR.validate_owner(self.owner, self.expected, self.identity)

    def test_owner_rejects_every_static_identity_change(self):
        for key in self.expected:
            with self.subTest(key=key):
                owner = copy.deepcopy(self.owner)
                owner[key] = "foreign"
                self.reject(lambda: OPERATOR.validate_owner(owner, self.expected, self.identity))

    def test_owner_rejects_unknown_missing_and_duplicate_fields(self):
        self.reject(lambda: OPERATOR.validate_owner(dict(self.owner, extra=True), self.expected, self.identity))
        owner = dict(self.owner)
        del owner["port"]
        self.reject(lambda: OPERATOR.validate_owner(owner, self.expected, self.identity))
        self.reject(lambda: OPERATOR.strict_object([("port", 15432), ("port", 5432)]))

    def test_owner_rejects_incomplete_initialization(self):
        for field, value in (("phase", "initializing"), ("system_identifier", None),
                             ("system_identifier", "foreign"), ("directories", {}),
                             ("control_identity", {"device": 41, "inode": 99})):
            with self.subTest(field=field):
                self.reject(lambda: OPERATOR.validate_owner(dict(self.owner, **{field: value}), self.expected, self.identity))

    def test_owner_rejects_malformed_process(self):
        for value in ({}, dict(self.process, pid=1), dict(self.process, start_ticks=0),
                      dict(self.process, boot_id="foreign"), dict(self.process, unknown=True)):
            self.reject(lambda: OPERATOR.validate_owner(dict(self.owner, process=value), self.expected, self.identity))

    def test_canonical_rejects_symlink_parent(self):
        self.replace(Path, "lstat", lambda path: self.fake_stat(kind=stat.S_IFLNK if path == Path("/var/lib") else stat.S_IFDIR))
        self.reject(lambda: OPERATOR.canonical(OPERATOR.DATA))

    def test_present_includes_dangling_symlink(self):
        self.replace(Path, "lstat", lambda path: self.fake_stat(kind=stat.S_IFLNK))
        self.assertTrue(OPERATOR.present(OPERATOR.BASE))

    def test_private_file_rejects_wrong_permissions_owner_type_and_links(self):
        for options in ({"mode": 0o644}, {"uid": 102}, {"gid": 105}, {"links": 2}, {"kind": stat.S_IFLNK}):
            with self.subTest(options=options):
                self.replace(OPERATOR, "canonical", lambda path, options=options: self.fake_stat(**options))
                self.reject(lambda: OPERATOR.regular(OPERATOR.OWNER))

    def test_private_directory_rejects_wrong_owner_mode_and_type(self):
        for options in ({"kind": stat.S_IFDIR, "mode": 0o755}, {"kind": stat.S_IFDIR, "mode": 0o700, "uid": 102},
                        {"kind": stat.S_IFLNK, "mode": 0o700}):
            self.replace(OPERATOR, "canonical", lambda path, options=options: self.fake_stat(**options))
            self.reject(lambda: OPERATOR.directory(OPERATOR.CONTROL, 0, 0))

    def test_listener_occupation_rejects_before_bind(self):
        self.replace(OPERATOR, "command", lambda *args, **kwargs: "LISTEN 0 128 0.0.0.0:15432 users:foreign")
        self.reject(OPERATOR.port_available)

    def test_bind_failure_does_not_adopt_or_stop_listener(self):
        self.replace(OPERATOR, "command", lambda *args, **kwargs: "")
        probe = Mock()
        probe.__enter__ = Mock(return_value=probe)
        probe.__exit__ = Mock(return_value=False)
        probe.bind.side_effect = OSError("synthetic occupied socket")
        self.replace(socket, "socket", lambda *args, **kwargs: probe)
        self.reject(OPERATOR.port_available)
        probe.bind.assert_called_once_with(("127.0.0.1", 15432))

    def test_directory_identity_change_rejected(self):
        self.replace(OPERATOR, "canonical", lambda path: self.fake_stat())
        self.replace(OPERATOR, "directory", lambda *args: {"device": 41, "inode": 99})
        self.reject(lambda: OPERATOR.verify_directories(self.pg, self.owner))

    def tree_fixture(self, entries):
        self.replace(OPERATOR, "canonical", lambda path: self.fake_stat())
        self.replace(OPERATOR, "directory", lambda *args: self.identity)
        self.replace(Path, "iterdir", lambda path: iter((OPERATOR.DATA, OPERATOR.SOCKET, OPERATOR.LOG)))
        self.replace(Path, "rglob", lambda path, pattern: iter(entries))
        self.replace(Path, "lstat", lambda path: entries[path])

    def test_tree_rejects_symlink_and_hard_link(self):
        path = OPERATOR.DATA / "pg_wal"
        for info in (self.fake_stat(kind=stat.S_IFLNK, uid=102, gid=105),
                     self.fake_stat(uid=102, gid=105, links=2)):
            self.tree_fixture({path: info})
            self.reject(lambda: OPERATOR.verify_directories(self.pg, self.owner))

    def test_tree_rejects_foreign_special_file_and_mount(self):
        info = self.fake_stat(uid=102, gid=105)
        info.st_dev = 99
        for candidate in (info, self.fake_stat(kind=stat.S_IFSOCK, uid=102, gid=105, mode=0o700)):
            self.tree_fixture({OPERATOR.DATA / "foreign": candidate})
            self.reject(lambda: OPERATOR.verify_directories(self.pg, self.owner))

    def test_configuration_rejects_overrides_and_modified_hba(self):
        baseline = {"PG_VERSION": "17\n", "postgresql.conf": OPERATOR.CONF, "pg_hba.conf": OPERATOR.HBA,
                    "postgresql.auto.conf": "# empty\n", "pg_ident.conf": "# empty\n"}
        for name, value in (("pg_hba.conf", "host all all 0.0.0.0/0 trust\n"),
                            ("postgresql.conf", OPERATOR.CONF + "port=5432\n"),
                            ("postgresql.auto.conf", "shared_preload_libraries='foreign'\n"),
                            ("pg_ident.conf", "foreign postgres root\n"), ("PG_VERSION", "16\n")):
            fixture = dict(baseline, **{name: value})
            self.replace(OPERATOR, "read_file", lambda path, fixture=fixture, **kwargs: fixture[path.name])
            self.reject(lambda: OPERATOR.verify_configuration(self.pg))

    def test_credentials_rejects_extra_setting_and_wrong_database(self):
        password = "a" * 64
        url = f"postgresql://goby_test:{password}@127.0.0.1:15432/goby_test?sslmode=disable"
        value = f"GOBY_DATABASE_URL='{url}'\nGOBY_TEST_DATABASE_URL='{url}'\n"
        self.replace(OPERATOR, "present", lambda path: True)
        for text in (value + "PATH=foreign\n", value.replace(":15432", ":5432"),
                     value.replace("/goby_test?", "/postgres?"), value.replace("a" * 64, "$(foreign)")):
            self.replace(OPERATOR, "read_file", lambda *args, text=text, **kwargs: text)
            self.reject(OPERATOR.credentials)
        self.replace(OPERATOR, "read_file", lambda *args, **kwargs: value)
        self.assertEqual(OPERATOR.credentials(), password)
        self.assertNotIn(password, self.output.getvalue())

    def test_missing_credentials_are_not_rotated(self):
        self.replace(OPERATOR, "present", lambda path: False)
        self.reject(OPERATOR.credentials)

    def process_fixture(self):
        self.replace(OPERATOR, "present", lambda path: True)
        self.replace(OPERATOR, "read_file", lambda *args, **kwargs:
                     f"321\n{OPERATOR.DATA}\n1234567890\n15432\n{OPERATOR.SOCKET}\n127.0.0.1\n1 2\nready\n")
        self.replace(OPERATOR, "process_identity", lambda pid: dict(self.process))
        self.replace(Path, "stat", lambda path: self.fake_stat(uid=102))
        self.replace(Path, "readlink", lambda path: OPERATOR.BIN / "postgres")
        self.replace(Path, "read_bytes", lambda path:
                     b"\0".join((str(OPERATOR.BIN / "postgres").encode(), b"-D", str(OPERATOR.DATA).encode())) + b"\0")
        self.replace(OPERATOR, "command", lambda *args, **kwargs:
                     'LISTEN 0 244 127.0.0.1:15432 0.0.0.0:* users:(("postgres",pid=321,fd=6))')
        self.replace(OPERATOR, "canonical", lambda path: self.fake_stat(kind=stat.S_IFSOCK, uid=102, gid=105, mode=0o700))

    def test_process_accepts_exact_recorded_identity(self):
        self.process_fixture()
        self.assertEqual(OPERATOR.verify_process(self.pg, self.owner), self.process)

    def test_process_rejects_pid_reuse_boot_change_and_unrecorded_process(self):
        self.process_fixture()
        for process in (dict(self.process, start_ticks=123455), dict(self.process, boot_id="fedcba98-7654-3210-fedc-ba9876543210"),
                        dict(self.process, pid=322), None):
            self.reject(lambda: OPERATOR.verify_process(self.pg, dict(self.owner, process=process)))

    def test_process_rejects_wrong_command_binary_owner_and_listener(self):
        for owner, name, value in (
                (Path, "readlink", lambda path: Path("/usr/bin/foreign")),
                (Path, "read_bytes", lambda path: b"postgres\0-D\0/var/lib/postgresql/17/main\0"),
                (Path, "stat", lambda path: self.fake_stat(uid=0)),
                (OPERATOR, "command", lambda *args, **kwargs: 'LISTEN 0 244 0.0.0.0:15432 *:* users:(("postgres",pid=321,fd=6))')):
            self.process_fixture()
            self.replace(owner, name, value)
            self.reject(lambda: OPERATOR.verify_process(self.pg, self.owner))

    def test_process_rejects_changed_identity_during_check(self):
        self.process_fixture()
        identities = iter((self.process, dict(self.process, start_ticks=999999)))
        self.replace(OPERATOR, "process_identity", lambda pid: next(identities))
        self.reject(lambda: OPERATOR.verify_process(self.pg, self.owner))

    def test_stopped_guard_rejects_unrecorded_process(self):
        self.replace(OPERATOR, "port_available", Mock())
        self.replace(Path, "iterdir", lambda path: iter((Path("/proc/123"),)))
        self.replace(Path, "read_bytes", lambda path: b"foreign\0" + str(OPERATOR.DATA).encode() + b"\0")
        self.reject(lambda: OPERATOR.require_stopped(self.pg))

    def test_process_start_ticks_are_field_22_even_with_parentheses(self):
        fields = ["S"] + ["0"] * 18 + ["123456"] + ["0"] * 8
        self.replace(Path, "read_text", lambda path, **kwargs:
                     "321 (postgres (worker)) " + " ".join(fields) if path.name == "stat" else self.process["boot_id"])
        self.assertEqual(OPERATOR.process_identity(321), self.process)

    def role_fixture(self, overrides=None):
        responses = {"SELECT count(*) FROM pg_roles": "1", "SELECT rolcanlogin": "t|f|f|f|f|f",
                     "SELECT count(*) FROM pg_auth_members": "0", "SELECT rolpassword": "t",
                     "SELECT pg_get_userbyid": "goby_test"}
        responses.update(overrides or {})
        self.sql = []

        def query(sql):
            self.sql.append(sql)
            for prefix, value in responses.items():
                if sql.startswith(prefix):
                    return value
            self.fail("Unexpected SQL mutation")

        self.replace(OPERATOR, "administrator", query)
        self.replace(OPERATOR, "present", lambda path: True)
        self.replace(OPERATOR, "credentials", lambda **kwargs: "synthetic-secret")
        self.login = self.replace(OPERATOR, "check_tcp_login", Mock())

    def test_role_rejects_privilege_membership_owner_and_non_scram(self):
        for changes in ({"SELECT rolcanlogin": "t|t|f|f|f|f"}, {"SELECT count(*) FROM pg_auth_members": "1"},
                        {"SELECT rolpassword": "f"}, {"SELECT pg_get_userbyid": "postgres"}):
            self.role_fixture(changes)
            self.reject(lambda: OPERATOR.prepare_role(check_only=True))
            self.login.assert_not_called()
            self.assertTrue(all(sql.startswith("SELECT ") for sql in self.sql))

    def test_role_accepts_existing_restricted_login_without_sql_mutation(self):
        self.role_fixture()
        OPERATOR.prepare_role(check_only=True)
        self.login.assert_called_once_with("synthetic-secret")
        self.assertTrue(all(sql.startswith("SELECT ") for sql in self.sql))

    def test_existing_role_without_credentials_refuses_rotation(self):
        self.role_fixture()
        self.replace(OPERATOR, "present", lambda path: False)
        self.reject(OPERATOR.prepare_role)
        self.assertEqual(len(self.sql), 1)

    def main_fixture(self, fresh=False):
        self.replace(sys, "platform", "linux")
        self.replace(os, "geteuid", lambda: 0)
        self.enterContext(patch.dict(os.environ, {"SSH_CONNECTION": "synthetic"}))
        self.replace(os, "umask", Mock())
        self.replace(pwd, "getpwnam", lambda name: self.pg)
        self.replace(OPERATOR, "verify_parents", Mock())
        self.replace(OPERATOR, "binaries", lambda: self.expected["binaries"])
        self.replace(OPERATOR, "present", lambda path: not fresh)
        self.replace(OPERATOR, "directory", lambda *args: self.identity)
        self.replace(OPERATOR, "regular", Mock())
        self.replace(OPERATOR, "acquire_lock", lambda **kwargs: 42)
        self.replace(os, "close", Mock())
        self.replace(OPERATOR, "read_file", lambda path: json.dumps(self.owner))
        for name in ("verify_directories", "verify_configuration", "verify_server", "prepare_role", "require_stopped"):
            self.replace(OPERATOR, name, Mock())
        self.replace(OPERATOR, "cluster_identifier", lambda: self.owner["system_identifier"])
        self.replace(OPERATOR, "verify_process", lambda *args, **kwargs: self.process)
        self.saved = self.replace(OPERATOR, "save_owner", Mock())
        self.started = self.replace(OPERATOR, "postgres_command", Mock())

    def test_main_refuses_local_and_nonroot_execution(self):
        self.replace(sys, "platform", "win32")
        self.reject(lambda: OPERATOR.main([]))
        self.replace(sys, "platform", "linux")
        self.replace(os, "geteuid", lambda: 1000)
        self.reject(lambda: OPERATOR.main([]))

    def test_main_refuses_unknown_arguments_before_effects(self):
        self.reject(lambda: OPERATOR.main(["--reset"]))

    def test_main_refuses_unknown_data_path_before_mkdir(self):
        self.main_fixture(fresh=True)
        self.replace(OPERATOR, "present", lambda path: path == OPERATOR.BASE)
        self.reject(lambda: OPERATOR.main([]))
        self.started.assert_not_called()

    def test_main_does_not_create_workspace_for_check_or_stop(self):
        self.main_fixture(fresh=True)
        self.reject(lambda: OPERATOR.main(["--check"]))
        self.reject(lambda: OPERATOR.main(["--stop"]))

    def test_main_rejects_unknown_owner_before_commands(self):
        self.main_fixture()
        self.replace(OPERATOR, "read_file", lambda path: json.dumps(dict(self.owner, port=5432)))
        self.reject(lambda: OPERATOR.main([]))
        self.started.assert_not_called()
        self.saved.assert_not_called()

    def test_main_rejects_system_identifier_change_before_commands(self):
        self.main_fixture()
        self.replace(OPERATOR, "cluster_identifier", lambda: "9999999999999999999")
        self.reject(lambda: OPERATOR.main([]))
        self.started.assert_not_called()
        self.saved.assert_not_called()

    def test_main_reuses_verified_running_cluster_without_mutation(self):
        self.main_fixture()
        OPERATOR.main(["--check"])
        self.started.assert_not_called()
        self.saved.assert_not_called()
        OPERATOR.prepare_role.assert_called_once_with(check_only=True)

    def test_main_checks_stopped_cluster_without_starting(self):
        self.main_fixture()
        self.replace(OPERATOR, "verify_process", lambda *args, **kwargs: None)
        OPERATOR.main(["--check"])
        self.started.assert_not_called()
        self.saved.assert_not_called()
        OPERATOR.require_stopped.assert_called_once_with(self.pg)

    def test_main_restarts_recorded_cluster_and_saves_new_process(self):
        self.main_fixture()
        new_process = dict(self.process, pid=432, start_ticks=999999)
        processes = iter((None, new_process, new_process))
        self.replace(OPERATOR, "verify_process", lambda *args, **kwargs: next(processes))
        OPERATOR.main([])
        arguments = self.started.call_args.args[0]
        self.assertEqual(arguments, [OPERATOR.BIN / "pg_ctl", "-D", OPERATOR.DATA, "-l",
                                     OPERATOR.LOG / "postgres.log", "-w", "-t", "30", "start"])
        self.assertEqual(self.saved.call_args.args[0]["process"], new_process)

    def test_main_refuses_stop_after_process_identity_changes(self):
        self.main_fixture()
        processes = iter((self.process, dict(self.process, start_ticks=999999)))
        self.replace(OPERATOR, "verify_process", lambda *args, **kwargs: next(processes))
        self.replace(os, "pidfd_open", lambda *args: 43)
        self.reject(lambda: OPERATOR.main(["--stop"]))
        self.started.assert_not_called()
        self.saved.assert_not_called()

    def test_command_failure_does_not_expose_secret_stderr(self):
        self.replace(subprocess, "run", lambda *args, **kwargs:
                     types.SimpleNamespace(returncode=1, stdout="synthetic-secret", stderr="synthetic-secret"))
        with self.assertRaises(OPERATOR.WorkspaceError) as caught:
            OPERATOR.command(["synthetic"])
        self.assertNotIn("synthetic-secret", str(caught.exception))

    def test_initialization_refuses_any_existing_data_path(self):
        self.replace(OPERATOR, "present", lambda path: True)
        self.reject(lambda: OPERATOR.initialize(self.pg, self.owner))

    def test_lock_contention_is_nonblocking_and_closes_handle(self):
        self.replace(OPERATOR, "regular", lambda path: self.fake_stat())
        self.replace(os, "open", lambda *args: 42)
        self.replace(os, "fstat", lambda descriptor: self.fake_stat())
        close = self.replace(os, "close", Mock())
        flock = self.replace(fcntl, "flock", Mock(side_effect=BlockingIOError()))
        self.reject(OPERATOR.acquire_lock)
        flock.assert_called_once_with(42, fcntl.LOCK_EX | fcntl.LOCK_NB)
        close.assert_called_once_with(42)

    def test_lock_replacement_is_rejected_before_flock(self):
        self.replace(OPERATOR, "regular", lambda path: self.fake_stat())
        self.replace(os, "open", lambda *args: 42)
        self.replace(os, "fstat", lambda descriptor: self.fake_stat(inode=99))
        self.replace(os, "close", Mock())
        self.reject(OPERATOR.acquire_lock)

    def stop_fixture(self, results):
        self.open_pidfd = self.replace(os, "pidfd_open", Mock(return_value=43))
        self.replace(OPERATOR, "verify_process", lambda *args: self.process)
        waiter = Mock()
        waiter.poll.side_effect = results
        self.replace(select, "poll", lambda: waiter)
        self.signal = self.replace(signal, "pidfd_send_signal", Mock())
        self.replace(OPERATOR, "present", lambda path: False)
        self.close = self.replace(os, "close", Mock())
        return waiter

    def test_stop_uses_verified_pidfd_without_pg_ctl_or_numeric_signal(self):
        waiter = self.stop_fixture([[], [(43, select.POLLIN)]])
        OPERATOR.stop_process(self.pg, self.owner, self.process)
        self.open_pidfd.assert_called_once_with(321, 0)
        self.signal.assert_called_once_with(43, signal.SIGINT)
        waiter.register.assert_called_once_with(43, select.POLLIN)
        self.close.assert_called_once_with(43)

    def test_stop_does_not_signal_an_already_exited_process(self):
        self.stop_fixture([[(43, select.POLLIN)]])
        self.reject(lambda: OPERATOR.stop_process(self.pg, self.owner, self.process))
        self.signal.assert_not_called()
        self.close.assert_called_once_with(43)

    def test_stop_timeout_never_retargets_another_process(self):
        self.stop_fixture([[], []])
        self.reject(lambda: OPERATOR.stop_process(self.pg, self.owner, self.process))
        self.signal.assert_called_once_with(43, signal.SIGINT)
        self.close.assert_called_once_with(43)

    def test_hba_is_limited_to_explicit_loopback_roles_and_peer_admin(self):
        active = [line for line in OPERATOR.HBA.splitlines() if line and not line.startswith("#")]
        self.assertEqual(active, ["local all postgres peer", "local all all reject",
                                 "host goby_test goby_test 127.0.0.1/32 scram-sha-256",
                                 "host goby_client_m3e goby_client_m3e 127.0.0.1/32 scram-sha-256",
                                 "host all all 0.0.0.0/0 reject", "host all all ::0/0 reject"])


def main():
    global OPERATOR
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run operator guard tests only through authorized root SSH.")
    if len(sys.argv) != 2:
        raise SystemExit("Usage: test-prepare-postgres-workspace.py OPERATOR_SOURCE")
    source = Path(sys.argv[1])
    for path in (source, Path(__file__)):
        SOURCE_LINES[str(path)] = path.read_text(encoding="utf-8").splitlines(keepends=True)
    spec = importlib.util.spec_from_file_location("postgres_workspace_operator", source)
    OPERATOR = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(OPERATOR)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(WorkspaceGuardTests)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print("Memory-only guards: no PostgreSQL command, cluster mutation, or connection was performed.")
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
