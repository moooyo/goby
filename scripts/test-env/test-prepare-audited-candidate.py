#!/usr/bin/env python3
"""Pure admission guards; never provision, inspect a process, or contact a host."""
import copy
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import Mock, call, patch

SPEC = importlib.util.spec_from_file_location("audited_candidate", Path(__file__).with_name("prepare-audited-candidate.py"))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class Guards(unittest.TestCase):
    def input(self):
        return {"kind": "audited-candidate-provision-input", "version": 1, "runId": "20260913T070000Z-0123456789ab",
                "backendReport": {}, "sourceManifest": {}, "frontendReport": {}, "ports": {"postgres": 25432, "http": 28298},
                "public_url": "http://127.0.0.1:28296", "transcodingProfile": "software-baseline-v1"}

    def test_rejects_existing_service_ports_even_when_other_port_is_fresh(self):
        for role in ("postgres", "http"):
            for port in M.RESERVED:
                value = self.input()
                value["ports"][role] = port
                with self.subTest(role=role, port=port), self.assertRaises(ValueError):
                    M.validate_input(value)

    def test_rejects_shared_boolean_or_privileged_ports(self):
        for ports in ({"postgres": 28298, "http": 28298}, {"postgres": True, "http": 28298}, {"postgres": 80, "http": 28298}):
            value = self.input()
            value["ports"] = ports
            with self.assertRaises(ValueError):
                M.validate_input(value)

    def test_rejects_undeclared_credentials_and_path_injection(self):
        for changed in ({"runId": "../../old-instance"}, {"runId": "20260913T070000Z-0123456789ab\nExecStart=unsafe"}, {"databaseURL": "postgres://secret"}):
            value = self.input()
            value.update(changed)
            with self.assertRaises(ValueError):
                M.validate_input(value)

    def test_rejects_old_or_credential_bearing_gateway_origins(self):
        for origin in ("http://127.0.0.1:18196", "http://127.0.0.1:25432", "http://127.0.0.1:28298", "http://user:secret@127.0.0.1:28296", "http://remote:28296", "http://127.0.0.1:28296/web"):
            value = self.input()
            value["public_url"] = origin
            with self.subTest(origin=origin), self.assertRaises(ValueError):
                M.validate_input(value)

    def test_manifest_cannot_escape_or_read_prohibited_history(self):
        for name in ("../runtime.env", "/etc/passwd", "assets\\escape.js", *M.FORBIDDEN):
            with self.subTest(name=name), self.assertRaises(ValueError):
                M.relative(name)

    def test_missing_hash_or_boolean_size_cannot_disable_file_verification(self):
        for row in ({"sha256": None, "bytes": 1}, {"sha256": "0" * 64, "bytes": True}, {"sha256": "0" * 64, "bytes": -1}):
            with self.assertRaises((ValueError, TypeError)):
                M.file_spec(row)

    def test_candidate_cannot_hide_software_engine_by_disabling_profile(self):
        value = self.input()
        value["transcodingProfile"] = "disabled"
        with self.assertRaises(ValueError):
            M.validate_input(value)

    def test_targeted_or_unclean_backend_cannot_reach_artifact_reads(self):
        full = {"status": "passed", "mode": "full", "archive_sha256": M.ARCHIVE, "unit_exit_code": 0,
                "recursive_cgroup_empty": True, "existing_services_modified": False,
                "worker": {"status": "passed", "mode": "full", "cleanup": {"only_worker_process_remains": True,
                    "owned_postgres_stopped": True, "private_bind_removed": True, "source_unchanged": True}}}
        cases = []
        for field, value in (("mode", "target"), ("status", "failed"), ("recursive_cgroup_empty", False), ("existing_services_modified", True)):
            case = copy.deepcopy(full)
            case[field] = value
            cases.append(case)
        case = copy.deepcopy(full)
        case["worker"]["cleanup"]["owned_postgres_stopped"] = False
        cases.append(case)
        for case in cases:
            with patch.object(M, "descriptor", return_value=case), patch.object(M, "read", side_effect=AssertionError("No artifact read is permitted.")), self.assertRaises(ValueError):
                M.verify_products(self.input())

    def test_run_collisions_refuse_before_any_resource_creation_or_command(self):
        for collision in ("root", "unit-file", "unit-loaded", "postgres-port", "http-port"):
            job = M.Provision(self.input(), {}, {})
            pg_socket, http_socket = Mock(), Mock()
            if collision.endswith("-port"):
                (pg_socket if collision == "postgres-port" else http_socket).bind.side_effect = OSError(98, "Address already in use")
            unit_path = Path("/run/systemd/system") / job.units["postgres"]
            def exists(path):
                return collision == "root" and Path(path) == job.root or collision == "unit-file" and Path(path) == unit_path
            def show(name):
                if name in job.units.values():
                    return {"LoadState": "loaded" if collision == "unit-loaded" else "not-found"}
                return {"ActiveState": "inactive", "MainPID": "0"}
            with self.subTest(collision=collision), patch.object(M, "verify_products", return_value=({}, b"binary", {})) as product, \
                 patch.object(M.os.path, "lexists", side_effect=exists), patch.object(job, "show", side_effect=show), \
                 patch.object(M.socket, "socket", side_effect=[pg_socket, http_socket]), \
                 patch.object(M, "owned", side_effect=AssertionError("No filesystem admission after collision.")), \
                 patch.object(job, "mkdir", side_effect=AssertionError("No directory creation.")) as mkdir, \
                 patch.object(job, "write", side_effect=AssertionError("No file publication.")) as write, \
                 patch.object(job, "command", side_effect=AssertionError("No initdb or command.")) as command, \
                 patch.object(job, "start", side_effect=AssertionError("No service start.")) as start, \
                 patch.object(job, "psql", side_effect=AssertionError("No SQL.")) as sql:
                with self.assertRaises((ValueError, OSError)):
                    job.run()
                product.assert_called_once()
                for action in (mkdir, write, command, start, sql):
                    action.assert_not_called()
                self.assertFalse(job.created)
                self.assertEqual(job.starts, [])
            if collision.endswith("-port"):
                pg_socket.close.assert_called_once()
                if collision == "http-port":
                    http_socket.close.assert_called_once()

    def test_listener_rejects_foreign_socket_even_with_matching_process(self):
        for inode, accepted in (("999", False), ("444", True)):
            job = M.Provision(self.input(), {}, {})
            process = {"pid": 55, "startTicks": "123", "invocationId": "a" * 32}
            observed = {"MainPID": "55"}
            tcp = ("header\n0: 0100007F:%04X 00000000:0000 0A 0:0 00:0 0 995 0 444 1\n" % self.input()["ports"]["http"]).encode()
            with self.subTest(inode=inode), patch.object(job, "show", return_value=observed), \
                 patch.object(job, "process", return_value=process), patch.object(Path, "read_bytes", return_value=tcp), \
                 patch.object(Path, "iterdir", return_value=iter([Path("/proc/55/fd/7")])), \
                 patch.object(M.os, "readlink", return_value="socket:[" + inode + "]"), patch.object(job, "save") as saved:
                if accepted:
                    self.assertEqual(job.listener(observed, process)["socketInode"], "444")
                else:
                    with self.assertRaises(ValueError):
                        job.listener(observed, process)
                saved.assert_called_once()

    def test_sql_timeout_terminates_its_new_session_and_keeps_unknown_outcome(self):
        job = M.Provision(self.input(), {}, {})
        process = Mock(pid=77, returncode=-15)
        process.communicate.side_effect = M.subprocess.TimeoutExpired(["psql"], 45)
        with patch.object(job, "save"), patch.object(job, "write"), patch.object(M.subprocess, "Popen", return_value=process) as spawn, \
             patch.object(M, "stop_command_group", return_value=((b"", b"timeout"), True)) as stopped:
            with self.assertRaises(ValueError):
                job.command("create-owned-role", ["/usr/sbin/runuser", "-u", "postgres", "--", str(M.PG / "psql")], b"private SQL")
            self.assertTrue(spawn.call_args.kwargs["start_new_session"])
            stopped.assert_called_once_with(process)
            state = job.command_responsibilities[0]
            self.assertEqual(state["sqlOutcome"], "unknown")
            self.assertTrue(state["timedOut"])
            self.assertTrue(state["processGroupClosed"])

    def test_command_group_escalates_after_bounded_term_wait(self):
        process = Mock(pid=77)
        process.communicate.side_effect = [M.subprocess.TimeoutExpired(["initdb"], 5), (b"", b"")]
        with patch.object(M.os, "killpg") as kill, patch.object(M, "group_alive", side_effect=[True, True, False, False]), \
             patch.object(M.time, "monotonic", side_effect=[0, 2, 3]), patch.object(M.time, "sleep"):
            self.assertEqual(M.stop_command_group(process), ((b"", b""), True))
        self.assertEqual(kill.call_args_list, [call(77, M.signal.SIGTERM), call(77, M.signal.SIGKILL)])
        self.assertEqual(process.communicate.call_args_list, [call(timeout=5), call(timeout=5)])
        process.wait.assert_called_once_with(timeout=1)

    def test_failure_receipt_write_error_retains_structured_resource_responsibility(self):
        job = M.Provision(self.input(), {}, {})
        job.created, job.stage = True, "recovery_database_setup"
        job.starts = [{"unit": job.units["postgres"], "acknowledged": True}]
        with patch.object(job, "save", side_effect=OSError(28, "No space left")):
            report = M.failure_report(job, ValueError("Uncertain SQL"))
        self.assertEqual(report["receiptErrorType"], "OSError")
        self.assertIsNone(report["receipt"])
        self.assertEqual(report["scope"], str(job.root))
        self.assertEqual(report["startCalls"], job.starts)
        self.assertEqual(report["stage"], "recovery_database_setup")
        self.assertFalse(report["resumeAllowed"])

    def test_units_never_restart_or_receive_a_shared_write_scope(self):
        root = Path("/opt/goby-audited-candidate-20260913T070000Z-0123456789ab")
        for user, directory in (("goby", "data"), ("postgres", "postgres")):
            text = M.unit_text(user, "/safe/executable", root / directory, root / directory, root / "private/unit.log").decode()
            self.assertIn("User=" + user + "\nGroup=" + user, text)
            self.assertIn("Type=exec\n", text)
            self.assertIn("Restart=no\n", text)
            self.assertIn("ProtectSystem=strict\n", text)
            self.assertIn("ReadWritePaths=" + str(root / directory) + "\n", text)
            self.assertIn("InaccessiblePaths=" + " ".join("-" + path for path in M.INACCESSIBLE), text)
            self.assertNotIn("ReadWritePaths=/var/lib/postgresql", text)
            self.assertNotIn("ReadWritePaths=/var/lib/goby-test", text)


if __name__ == "__main__":
    unittest.main()
