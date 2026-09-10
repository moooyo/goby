#!/usr/bin/env python3
"""Test read-only task guards with synthetic values and no live side effects.

Run only through authorized root SSH. The tests neither initialize a recorder
nor contact a server, create evidence, change task state, or read credentials.
"""

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

sys.dont_write_bytecode = True
if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
    print(json.dumps({"result": "blocked", "reason": "Authorized root SSH and one scheduled-task recorder source are required"}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SOURCE_HASH = hashlib.sha256(SOURCE.read_bytes()).hexdigest()
TEST_HASH = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
SPEC = importlib.util.spec_from_file_location("scheduled_tasks_recorder_under_test", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class ScheduledTaskGuards(unittest.TestCase):
    def recorder(self):
        value = MODULE.Recorder.__new__(MODULE.Recorder)
        value.logins = {"control": {"AccessToken": "synthetic-fresh-control"}, "viewer": {"AccessToken": "synthetic-fresh-viewer"}}
        value.login_attempts = {"control", "viewer"}
        value.forbidden_tokens = {"synthetic-stored-old-token"}
        value.account_credentials = {"admin": {"REFERENCE_USERNAME": "synthetic-admin", "REFERENCE_PASSWORD": "synthetic-password"},
                                     "viewer": {"REFERENCE_USERNAME": "synthetic-viewer", "REFERENCE_PASSWORD": "synthetic-viewer-password"}}
        value.secrets = {"synthetic-known-credential"}
        value.task_ids, value.read_ids = {"observed-task"}, {"15"}
        value.task_baseline, value.task_observations = {}, []
        value.task_configuration_changes, value.task_runtime_changes = [], []
        value.task_bound_exceeded = False
        return value

    def test_read_only_routes_require_observed_ids_and_canonical_boolean_filters(self) -> None:
        recorder = self.recorder()
        for path in ("/emby/ScheduledTasks", "/emby/ScheduledTasks?IsHidden=true&IsEnabled=false",
                     "/emby/ScheduledTasks/observed-task", "/emby/ScheduledTasks/" + MODULE.UNKNOWN_TASK_ID):
            recorder.authorize("GET", path, "synthetic-fresh-control", MODULE.MISSING, "")
        for path in ("/emby/ScheduledTasks/unobserved-task", "/emby/ScheduledTasks?IsHidden=maybe",
                     "/emby/ScheduledTasks?IsHidden=true&IsHidden=false", "/emby/ScheduledTasks?api_key=arbitrary",
                     "/emby/ScheduledTasks/observed-task/Triggers", "/emby/Auth/Keys"):
            with self.subTest(path=path), self.assertRaises(RuntimeError):
                recorder.authorize("GET", path, "synthetic-fresh-control", MODULE.MISSING, "")
        with self.assertRaises(RuntimeError):
            recorder.authorize("GET", "/emby/ScheduledTasks", "synthetic-stored-old-token", MODULE.MISSING, "")

    def test_all_task_and_device_mutations_remain_denied(self) -> None:
        recorder = self.recorder()
        paths = ("/emby/ScheduledTasks/observed-task/Triggers", "/emby/ScheduledTasks/Running/observed-task",
                 "/emby/ScheduledTasks/Running/observed-task/Delete", "/emby/Devices?Id=15", "/emby/Devices/Options?Id=15")
        for method in ("POST", "DELETE", "PUT", "PATCH"):
            for path in paths:
                with self.subTest(method=method, path=path), self.assertRaises(RuntimeError):
                    recorder.authorize(method, path, "synthetic-fresh-control", MODULE.MISSING, "")
        recorder.authorize("POST", "/emby/Sessions/Logout", "synthetic-fresh-viewer", MODULE.MISSING, "")

    def test_public_task_keys_are_preserved_only_in_the_declared_dto_positions(self) -> None:
        recorder = self.recorder()
        secret = "synthetic-known-credential"
        record = {"request": {"method": "GET", "path": "/emby/ScheduledTasks/observed-task", "headers": {"X-Emby-Token": secret}},
                  "response": {"status": 200, "bodyType": "json", "body": {"Id": "observed-task", "Name": "Synthetic task", "State": "Idle",
                      "Key": "RefreshLibrary", "LastExecutionResult": {"Key": "RefreshLibrary", "Status": "Completed"},
                      "Description": "https://example.invalid/item?api_key=" + secret, "Extra": {secret: "kept"}}}}
        before = copy.deepcopy(record)
        recorder.collect_secrets(record)
        cleaned = recorder.sanitize(record)
        self.assertNotIn("RefreshLibrary", recorder.secrets)
        self.assertEqual(cleaned["response"]["body"]["Key"], "RefreshLibrary")
        self.assertEqual(cleaned["response"]["body"]["LastExecutionResult"]["Key"], "RefreshLibrary")
        self.assertNotIn(secret, json.dumps(cleaned))
        self.assertEqual(record, before)
        known = copy.deepcopy(record)
        known["response"]["body"]["Key"] = secret
        known["response"]["body"]["LastExecutionResult"]["Key"] = secret
        recorder.collect_secrets(known)
        self.assertNotIn(secret, json.dumps(recorder.sanitize(known)))
        unrelated = copy.deepcopy(record)
        unrelated["request"]["path"] = "/emby/Users"
        unrelated["response"]["body"]["Key"] = "synthetic-unrecognized-secret"
        other = self.recorder()
        other.collect_secrets(unrelated)
        self.assertIn("synthetic-unrecognized-secret", other.secrets)
        self.assertNotIn("synthetic-unrecognized-secret", json.dumps(other.sanitize(unrelated)))

    def test_oversized_definition_response_is_retained_as_a_bounded_partial_for_any_actor(self) -> None:
        recorder = self.recorder()
        definitions = [{"Id": "task-" + str(index), "Name": "Synthetic task", "State": "Idle"} for index in range(65)]
        with patch.object(MODULE.device.Recorder, "request", return_value=(200, definitions)):
            with self.assertRaises(RuntimeError):
                recorder.request("viewer-list", "GET", "/emby/ScheduledTasks", token="synthetic-fresh-viewer")
        self.assertTrue(recorder.task_bound_exceeded)
        self.assertFalse(recorder.task_observations[-1]["completeDefinitionSample"])
        self.assertEqual(recorder.task_observations[-1]["itemsCount"], 65)
        self.assertEqual(recorder.task_ids, {"observed-task"})

    def test_configuration_and_runtime_comparison_distinguish_absent_from_null(self) -> None:
        recorder = self.recorder()
        before = {"Id": "observed-task", "Name": "Synthetic task", "State": "Idle"}
        recorder.task_baseline = {"observed-task": before}
        after = {**before, "IsHidden": None, "CurrentProgressPercentage": None}
        recorder.request = Mock(return_value=(200, after))
        recorder.task_detail("final-detail", "observed-task", final=True)
        self.assertEqual(recorder.task_configuration_changes[0]["changedFields"], ["IsHidden"])
        self.assertEqual(recorder.task_configuration_changes[0]["presenceChanges"], ["IsHidden"])
        self.assertEqual(recorder.task_runtime_changes[0]["changedFields"], ["CurrentProgressPercentage"])
        self.assertEqual(recorder.task_runtime_changes[0]["presenceChanges"], ["CurrentProgressPercentage"])


def main() -> None:
    output = io.StringIO()
    forbidden = RuntimeError("Scheduled-task guard tests forbid live side effects")
    with contextlib.ExitStack() as guards:
        for owner, name in ((socket, "socket"), (socket, "create_connection"), (http.client.HTTPConnection, "__init__"),
                            (subprocess, "Popen"), (subprocess, "run"), (subprocess, "check_output"), (os, "open"), (os, "execvp"),
                            (Path, "mkdir"), (Path, "read_text"), (Path, "read_bytes"), (Path, "write_text"), (Path, "write_bytes"),
                            (MODULE.signal, "setitimer"), (MODULE.time, "sleep"), (MODULE.base, "save"), (MODULE.Recorder, "__init__")):
            guards.enter_context(patch.object(owner, name, side_effect=forbidden))
        result = unittest.TextTestRunner(stream=output).run(unittest.defaultTestLoader.loadTestsFromTestCase(ScheduledTaskGuards))
    print(json.dumps({"suite": "scheduled-task-read-only-guards", "result": "passed" if result.wasSuccessful() else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "recorderSha256": SOURCE_HASH, "testSha256": TEST_HASH, "httpRequests": 0, "captureWrites": 0}))
    if not result.wasSuccessful():
        print(output.getvalue(), file=sys.stderr)
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
