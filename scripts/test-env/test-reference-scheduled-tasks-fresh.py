#!/usr/bin/env python3
"""Exercise fresh-task boundaries using synthetic state without live effects."""

from __future__ import annotations

import contextlib
import copy
import hashlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import unittest
from unittest.mock import Mock, patch
from urllib.parse import quote

sys.dont_write_bytecode = True
if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
    print(json.dumps({"result": "blocked", "reason": "Authorized root SSH and one fresh-task recorder source are required"}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SOURCE_HASH = hashlib.sha256(SOURCE.read_bytes()).hexdigest()
TEST_HASH = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
SPEC = importlib.util.spec_from_file_location("fresh_tasks_recorder_under_test", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class FreshTaskGuards(unittest.TestCase):
    def recorder(self):
        value = MODULE.Recorder.__new__(MODULE.Recorder)
        value.manifest = {"networkNamespace": "net:[synthetic-fresh]"}
        value.logins = {"control": {"AccessToken": "synthetic-fresh-control"}, "viewer": {"AccessToken": "synthetic-fresh-viewer"}}
        value.forbidden_tokens = {"synthetic-revoked-bootstrap"}
        value.secrets = {"synthetic-known-credential/a+b?"}
        value.login_attempts, value.login_statuses = set(), {}
        value.account_credentials = {"admin": {"REFERENCE_USERNAME": "synthetic-admin", "REFERENCE_PASSWORD": "synthetic-password"},
                                     "viewer": {"REFERENCE_USERNAME": "synthetic-viewer", "REFERENCE_PASSWORD": ""}}
        value.selected_id = "owned-refresh"
        value.initial_triggers = [{"Type": "IntervalTrigger", "IntervalTicks": 432000000000}]
        value.latest_task = {"Id": value.selected_id, "Key": "RefreshLibrary", "Name": "Synthetic task", "State": "Idle", "Triggers": []}
        value.task_ids, value.read_ids = {value.selected_id, "other-task"}, set()
        value.record_count = 7
        value.task_write_proof = {"taskId": value.selected_id, "taskIdentifier": "RefreshLibrary", "state": "Idle", "requestSequence": 7}
        value.latest_library_count, value.library_proof_sequence = 0, 6
        value.libraries_owned, value.library_attempts = {}, set()
        value.unknown_proved, value.finishing = False, False
        value.task_observations, value.experiments, value.poll_observations = [], [], []
        value.task_mutation_acks, value.library_mutation_acks, value.mutations = [], [], []
        value.cleanup_errors, value.other_task_changes, value.checks = [], [], {}
        value.trigger_write_possible, value.run_possible = False, False
        value.current_experiment = "synthetic"
        value.total, value.charged_bytes, value.incomplete_count = 0, 0, 0
        value.deadline = MODULE.time.monotonic() + 60
        value.all_task_baseline = {value.selected_id: {**value.latest_task, "Triggers": copy.deepcopy(value.initial_triggers)}}
        return value

    def test_only_current_positive_owned_task_and_fresh_tokens_authorize_writes(self) -> None:
        recorder = self.recorder()
        for operation in ("triggers", "start", "stop", "stop-alias"):
            method, path = recorder.task_route(operation, recorder.selected_id)
            body = [] if operation == "triggers" else MODULE.MISSING
            recorder.authorize(method, path, recorder.admin(), body, "")
            with self.assertRaises(RuntimeError):
                recorder.authorize(method, path.replace("owned-refresh", "other-task"), recorder.admin(), body, "")
            with self.assertRaises(RuntimeError):
                recorder.authorize(method, path, "synthetic-revoked-bootstrap", body, "")
        method, path = recorder.task_route("start", recorder.selected_id)
        recorder.record_count += 1
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.admin(), MODULE.MISSING, "")
        recorder.task_write_proof["requestSequence"] = recorder.record_count
        recorder.task_write_proof["taskIdentifier"] = "OtherTask"
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.admin(), MODULE.MISSING, "")

    def test_unknown_controls_require_absence_and_media_stops_require_running(self) -> None:
        recorder = self.recorder()
        method, path = recorder.task_route("stop", MODULE.UNKNOWN_ID)
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, "", MODULE.MISSING, "")
        recorder.unknown_proved = True
        recorder.authorize(method, path, "", MODULE.MISSING, "")
        recorder.latest_library_count = 1
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.admin(), MODULE.MISSING, "")
        method, path = recorder.task_route("stop", recorder.selected_id)
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.admin(), MODULE.MISSING, "")
        recorder.task_write_proof["state"] = "Running"
        recorder.authorize(method, path, recorder.admin(), MODULE.MISSING, "")
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.logins["viewer"]["AccessToken"], MODULE.MISSING, "")

    def test_trigger_cases_are_finite_and_cleanup_can_restore_with_owned_libraries(self) -> None:
        recorder = self.recorder()
        method, path = recorder.task_route("triggers", recorder.selected_id)
        for _name, body in MODULE.TRIGGER_CASES:
            recorder.authorize(method, path, recorder.admin(), body, "")
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.admin(), [{"Type": "IntervalTrigger", "IntervalTicks": 1}], "")
        recorder.latest_library_count = 2
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.admin(), recorder.initial_triggers, "")
        recorder.finishing = True
        recorder.authorize(method, path, recorder.admin(), recorder.initial_triggers, "")
        with self.assertRaises(RuntimeError):
            recorder.authorize(method, path, recorder.admin(), MODULE.TRIGGER_CASES[-1][1], "")

    def test_library_creation_requires_exact_owned_body_and_single_attempt(self) -> None:
        recorder = self.recorder()
        path = "/emby/Library/VirtualFolders"
        body = MODULE.library_body("a")
        recorder.authorize("POST", path, recorder.admin(), body, "")
        for field, replacement in (("Paths", ["/tmp/unowned"]), ("RefreshLibrary", True), ("Name", "unowned")):
            bad = copy.deepcopy(body)
            bad[field] = replacement
            with self.assertRaises(RuntimeError):
                recorder.authorize("POST", path, recorder.admin(), bad, "")
        recorder.library_attempts.add("a")
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", path, recorder.admin(), body, "")
        recorder.library_attempts.clear()
        recorder.latest_task["State"] = "Running"
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", path, recorder.admin(), body, "")

    def test_failed_preconditions_clear_transport_authority_before_any_http(self) -> None:
        recorder = self.recorder()
        with patch.object(MODULE, "ACTIVE_NAMESPACE", "net:[synthetic-fresh]"), patch.object(MODULE.sys, "platform", "unsupported"):
            with self.assertRaises(RuntimeError):
                MODULE.preconditions()
            self.assertIsNone(MODULE.ACTIVE_NAMESPACE)
            with patch.object(MODULE.http.client, "HTTPConnection") as connection:
                with self.assertRaises(RuntimeError):
                    recorder.request("synthetic-denied", "GET", "/emby/ScheduledTasks", token=recorder.admin())
                connection.assert_not_called()

    def test_task_public_keys_survive_but_raw_and_encoded_secrets_do_not(self) -> None:
        recorder = self.recorder()
        secret = next(iter(recorder.secrets))
        encoded, double = quote(secret, safe=""), quote(quote(secret, safe=""), safe="")
        record = {"request": {"method": "GET", "path": "/emby/ScheduledTasks/owned-refresh", "headers": {"X-Emby-Token": secret}},
                  "response": {"status": 200, "bodyType": "json", "body": {"Id": "owned-refresh", "Name": "Synthetic task", "State": "Idle",
                      "Key": "RefreshLibrary", "LastExecutionResult": {"Key": "RefreshLibrary", "Status": "Completed"},
                      "Extra": {secret: "raw", encoded: "encoded", double: "double"}}}}
        # Separate encoded names avoid intentionally colliding sanitized dictionary keys.
        record["response"]["body"]["Extra"] = {"raw-" + secret: "https://example.invalid/?value=" + encoded,
                                                        "encoded-" + encoded: double}
        recorder.collect_secrets(record)
        cleaned = recorder.sanitize(record)
        self.assertEqual(cleaned["response"]["body"]["Key"], "RefreshLibrary")
        self.assertEqual(cleaned["response"]["body"]["LastExecutionResult"]["Key"], "RefreshLibrary")
        self.assertNotIn("RefreshLibrary", recorder.secrets)
        for value in (secret, encoded, double):
            self.assertNotIn(value, json.dumps(cleaned))
        collision = {secret: "one", encoded: "two"}
        with self.assertRaises(RuntimeError):
            recorder.sanitize(collision)
        unrelated = {"request": {"method": "POST", "path": "/emby/ScheduledTasks/owned-refresh/Triggers"},
                     "response": {"status": 200, "bodyType": "json", "body": {"Key": "synthetic-not-public"}}}
        recorder.collect_secrets(unrelated)
        self.assertIn("synthetic-not-public", recorder.secrets)

    def test_incomplete_http_is_counted_before_evidence_write_failure(self) -> None:
        for transport_failure in (True, False):
            with self.subTest(transportFailure=transport_failure):
                recorder = self.recorder()
                connection, response = Mock(), Mock()
                if transport_failure:
                    connection.request.side_effect = ConnectionRefusedError("synthetic transport failure")
                else:
                    response.read.return_value = b"[]"
                    response.isclosed.return_value = False
                    response.length, response.status = 1, 200
                    response.getheaders.return_value = []
                    connection.getresponse.return_value = response
                recorder.write = Mock(side_effect=OSError("synthetic evidence failure"))
                with patch.object(MODULE, "ACTIVE_NAMESPACE", "net:[synthetic-fresh]"), patch.object(MODULE.os, "readlink", return_value="net:[synthetic-fresh]"), \
                        patch.object(MODULE.http.client, "HTTPConnection", return_value=connection), patch.object(MODULE.signal, "setitimer"):
                    with self.assertRaises(OSError):
                        recorder.request("synthetic-failure", "GET", "/emby/ScheduledTasks", token=recorder.admin())
                self.assertEqual(recorder.incomplete_count, 1)
                self.assertEqual(recorder.record_count, 8)
                connection.close.assert_called_once()

    def test_target_cleanup_precedes_independent_other_task_audit(self) -> None:
        recorder = self.recorder()
        recorder.finishing, recorder.latest_library_count = True, 2
        recorder.latest_task["State"] = "Running"
        state = copy.deepcopy(recorder.latest_task)
        def request(_label, method, path, *, token="", body=MODULE.MISSING, identity=""):
            recorder.record_count += 1
            if method == "DELETE":
                state["State"] = "Idle"
            elif method == "POST" and path.endswith("/Triggers"):
                state["Triggers"] = copy.deepcopy(body)
            return (200, copy.deepcopy(state)) if method == "GET" else (204, "")
        recorder.request = Mock(side_effect=request)
        recorder.observe_tasks = Mock(side_effect=RuntimeError("Synthetic unrelated task configuration changed"))
        recorder.cleanup_task()
        self.assertTrue(recorder.checks["originalTriggersRestoredAndTaskIdle"])
        self.assertEqual(state["Triggers"], recorder.initial_triggers)
        recorder.observe_tasks.assert_not_called()
        with self.assertRaises(RuntimeError):
            recorder.audit_task_configuration()


def main() -> None:
    output = io.StringIO()
    forbidden = RuntimeError("Fresh task guard tests forbid live side effects")
    with contextlib.ExitStack() as guards:
        for owner, name in ((socket, "socket"), (socket, "create_connection"), (http.client.HTTPConnection, "__init__"),
                            (subprocess, "Popen"), (subprocess, "run"), (subprocess, "check_output"), (os, "open"), (os, "execvp"),
                            (Path, "mkdir"), (Path, "read_text"), (Path, "read_bytes"), (Path, "write_text"), (Path, "write_bytes"),
                            (MODULE.signal, "setitimer"), (MODULE.time, "sleep"), (MODULE.base, "save"), (MODULE.Recorder, "__init__")):
            guards.enter_context(patch.object(owner, name, side_effect=forbidden))
        result = unittest.TextTestRunner(stream=output).run(unittest.defaultTestLoader.loadTestsFromTestCase(FreshTaskGuards))
    print(json.dumps({"suite": "fresh-scheduled-task-guards", "result": "passed" if result.wasSuccessful() else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "recorderSha256": SOURCE_HASH, "testSha256": TEST_HASH, "httpRequests": 0, "captureWrites": 0}))
    if not result.wasSuccessful():
        print(output.getvalue(), file=sys.stderr)
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
