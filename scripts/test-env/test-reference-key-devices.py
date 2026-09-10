#!/usr/bin/env python3
"""Exercise key-device recorder safety with bounded, entirely synthetic data.

Run through authorized root SSH. The recorder is imported without initialization;
all responses, persistence, and audit records remain in memory. No reference
credential, capture directory, process, network, or media file is accessed.
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
    print(json.dumps({"suite": "reference-key-device-hardening", "result": "blocked",
                      "reason": "Authorized root SSH and one recorder source path are required"}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SOURCE_HASH = hashlib.sha256(SOURCE.read_bytes()).hexdigest()
TEST_HASH = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
SPEC = importlib.util.spec_from_file_location("reference_key_devices_under_test", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class FakeResponse:
    def __init__(self, status: int, body: object) -> None:
        self.status, self.body, self.length = status, json.dumps(body).encode(), 0

    def read(self, limit: int) -> bytes:
        return self.body[:limit]

    def isclosed(self) -> bool:
        return True

    def getheaders(self) -> list[tuple[str, str]]:
        return [("Content-Type", "application/json")]


class FakeConnection:
    def __init__(self, response: FakeResponse) -> None:
        self.response, self.calls, self.closed = response, [], False

    def request(self, *arguments: object) -> None:
        self.calls.append(arguments)

    def getresponse(self) -> FakeResponse:
        return self.response

    def close(self) -> None:
        self.closed = True


class KeyDeviceSafetyTests(unittest.TestCase):
    def recorder(self) -> object:
        recorder = MODULE.Recorder.__new__(MODULE.Recorder)
        recorder.pid = 123
        recorder.started = MODULE.time.monotonic()
        recorder.deadline = recorder.started + 60
        recorder.finishing, recorder.cleanup_ok = False, False
        recorder.record_count = recorder.charged_bytes = recorder.total = recorder.incomplete_count = 0
        recorder.baseline = {"records": {}, "media": {}, "privateFiles": {}}
        recorder.secrets, recorder.owned, recorder.apps = set(), {}, set(MODULE.KEY_APPS.values())
        recorder.logins = {"control": {"AccessToken": "synthetic-control-token"}}
        recorder.account_credentials = {
            "admin": {"REFERENCE_USERNAME": "Synthetic Admin", "REFERENCE_PASSWORD": "synthetic-password"},
            "viewer": {"REFERENCE_USERNAME": "Synthetic Viewer", "REFERENCE_PASSWORD": "synthetic-viewer-password"},
        }
        recorder.forbidden_tokens = {"synthetic-preexisting-admin-token", "synthetic-preexisting-viewer-token"}
        recorder.login_attempts, recorder.login_statuses, recorder.login_delays = set(), {}, []
        recorder.logged_out, recorder.logout_statuses = set(), {}
        recorder.invalid_tokens, recorder.invalid_login_proofs = set(), {}
        recorder.old_keys, recorder.old_key_audit_complete = [], False
        recorder.device_baseline_complete, recorder.old_key_baseline_complete = True, True
        recorder.server_creation_gate = {"evaluated": True, "allowed": True, "blockedReason": None}
        recorder.key_create_attempts, recorder.key_creation_statuses = set(), {}
        recorder.key_credential_ids, recorder.key_invalidity_proofs = {}, {}
        recorder.key_delete_acknowledgements, recorder.key_probe_observations = [], []
        recorder.key_rows_observations, recorder.key_device_observations = [], []
        recorder.control_id = "20"
        control = MODULE.Recorder.metadata("control")
        recorder.owned_devices = {"20": {"Id": "20", "ReportedDeviceId": control["DeviceId"],
                                           "AppName": control["Client"], "Name": control["Device"]}}
        recorder.old_devices = {"19": {"Id": "19", "ReportedDeviceId": "synthetic-unrelated-reported-id",
                                         "AppName": "Unrelated App", "Name": "Unrelated Device"}}
        recorder.old_options = {"19": {"status": 200, "body": {"CustomName": "Unrelated Custom Name"}}}
        recorder.previous_device_ids = {"19"}
        recorder.protected_device_aliases = {"19", "synthetic-unrelated-reported-id", "20", control["DeviceId"]}
        recorder.protected_hidden_devices, recorder.protected_hidden_options = {}, {}
        recorder.read_ids = {"19", "20", MODULE.PREVIOUS_SERVER_NUMERIC_ID}
        recorder.approved_write_ids, recorder.ambiguous_ids = set(), set()
        recorder.pending_write_proof, recorder.virtual_write_id = None, None
        recorder.acknowledged_device_deletes = {}
        recorder.write_proofs, recorder.device_observations, recorder.mutations = [], [], []
        recorder.cleanup_errors, recorder.checks = [], {}
        recorder.capture_failure, recorder.old_users, recorder.final_devices = None, None, None
        recorder.user_date_changes, recorder.old_device_date_changes = [], []
        recorder.server_public_id = "synthetic-server-public-id"
        recorder.read_ids.add(recorder.server_public_id)
        recorder.server_baseline_info = {
            recorder.server_public_id: {"status": 404, "body": "Synthetic missing reported server"},
            MODULE.PREVIOUS_SERVER_NUMERIC_ID: {"status": 204, "body": ""},
        }
        recorder.server_baseline_list, recorder.server_candidates = [], {}
        recorder.server_branch = {"verified": False, "deleteAttempted": False, "reason": "Synthetic initial state"}
        recorder.header_delete_branch = dict(recorder.server_branch)
        recorder.write = Mock()
        return recorder

    @staticmethod
    def key_row(token: str, name: str = "alpha", *, device_id: int = 44) -> dict:
        return {"AccessToken": token, "AppName": MODULE.KEY_APPS[name], "DeviceId": device_id,
                "ReportedDeviceId": "synthetic-server-public-id"}

    def seed_keys(self, recorder: object, *, duplicate_alpha: bool = False) -> list[dict]:
        recorder.key_create_attempts = set(MODULE.KEY_APPS.values())
        rows = [self.key_row("synthetic-alpha-token"), self.key_row("synthetic-sibling-token", "sibling")]
        if duplicate_alpha:
            rows.append(self.key_row("synthetic-second-alpha-token"))
        recorder.acknowledge_keys(rows)
        return rows

    @staticmethod
    def server_device() -> dict:
        return {"Id": "44", "ReportedDeviceId": "synthetic-server-public-id", "AppName": "Synthetic Server App",
                "Name": "Synthetic Server Device"}

    @staticmethod
    def expected_key_scopes(recorder: object) -> set[str]:
        scopes = set()
        for token, credential_id in recorder.key_credential_ids.items():
            name = recorder.key_name(token)
            scopes.add(credential_id + ":token-only")
            scopes.update(credential_id + ":" + client for client, values in MODULE.KEY_CLIENTS.items() if values[0] == name)
        return scopes

    def finish_audit_in_memory(self, recorder: object) -> None:
        recorder.logins, recorder.invalid_login_proofs = {}, {}
        recorder.old_key_audit_complete = True

        def observed_cleanup(label: str, _action: object) -> object:
            if label == "process-audit":
                return recorder.pid
            if label == "old-file-audit":
                return recorder.baseline
            if label == "private-mutation-journal":
                return None
            raise AssertionError("Unexpected external operation during the synthetic final audit")

        with patch.object(recorder, "cleanup_step", side_effect=observed_cleanup), patch.object(Path, "glob", return_value=[]):
            recorder.finish()

    def test_acknowledgement_requires_an_attempted_reserved_app_and_a_new_token(self) -> None:
        recorder = self.recorder()
        old = self.key_row("synthetic-old-key-token")
        recorder.old_keys = [old]
        rows = [self.key_row("synthetic-new-alpha-token"), self.key_row("synthetic-new-sibling-token", "sibling"), old,
                self.key_row("synthetic-preexisting-admin-token"),
                {"AccessToken": "synthetic-unknown-app-token", "AppName": MODULE.APP_PREFIX + "Not Reserved"}]
        recorder.acknowledge_keys(rows)
        self.assertEqual(recorder.owned, {})
        recorder.key_create_attempts.add(MODULE.KEY_APPS["alpha"])
        recorder.acknowledge_keys(rows)
        self.assertEqual(set(recorder.owned), {"synthetic-new-alpha-token"})
        self.assertEqual(set(recorder.key_credential_ids), set(recorder.owned))
        self.assertTrue({row["AccessToken"] for row in rows} <= recorder.secrets)
        self.assertEqual(recorder.old_keys, [old])

    def test_key_acknowledgement_precedes_create_or_list_output_failure(self) -> None:
        for creation in (True, False):
            with self.subTest(creation=creation):
                recorder = self.recorder()
                if creation:
                    path = "/emby/Auth/Keys?" + MODULE.urlencode({"App": MODULE.KEY_APPS["alpha"]})
                    body = {"AccessToken": "synthetic-created-before-failure"}
                    expected = {body["AccessToken"]}
                else:
                    recorder.key_create_attempts = set(MODULE.KEY_APPS.values())
                    path = "/emby/Auth/Keys"
                    rows = [self.key_row("synthetic-listed-alpha"), self.key_row("synthetic-listed-sibling", "sibling")]
                    body = {"Items": rows, "TotalRecordCount": 0}
                    expected = {row["AccessToken"] for row in rows}
                connection = FakeConnection(FakeResponse(200, body))
                with patch.object(MODULE.http.client, "HTTPConnection", return_value=connection), \
                        patch.object(MODULE.base, "save"), patch.object(MODULE.signal, "setitimer", return_value=0), \
                        patch.object(recorder, "write", side_effect=OSError("Synthetic evidence persistence failure")):
                    with self.assertRaisesRegex(OSError, "Synthetic evidence persistence failure"):
                        recorder.request("synthetic-ack-before-failure", "POST" if creation else "GET", path, token=recorder.admin())
                self.assertEqual(set(recorder.owned), expected)
                self.assertEqual(set(recorder.key_credential_ids), expected)
                self.assertTrue(expected <= recorder.secrets)
                self.assertTrue(connection.closed)
                self.assertEqual(len(connection.calls), 1)
                if creation:
                    self.assertEqual(recorder.key_creation_statuses, {MODULE.KEY_APPS["alpha"]: 200})
                    self.assertEqual(recorder.key_create_attempts, {MODULE.KEY_APPS["alpha"]})

    def test_old_tokens_are_never_http_credentials_or_key_deletion_targets(self) -> None:
        recorder = self.recorder()
        self.seed_keys(recorder)
        old = self.key_row("synthetic-old-unowned-key")
        recorder.old_keys = [old]
        recorder.acknowledge_keys([old])
        for token in (*recorder.forbidden_tokens, old["AccessToken"]):
            with self.subTest(token=token):
                with self.assertRaises(RuntimeError):
                    recorder.request("synthetic-forbidden-token", "GET", "/emby/Sessions", token=token)
                with self.assertRaises(RuntimeError):
                    recorder.request("synthetic-forbidden-delete", "DELETE", "/emby/Auth/Keys/" + MODULE.quote(token, safe=""),
                                     token=recorder.admin())
        self.assertEqual(recorder.record_count, 0)
        self.assertEqual(recorder.mutations, [])

    def test_key_client_metadata_cannot_cross_credentials_or_replace_a_login(self) -> None:
        recorder = self.recorder()
        self.seed_keys(recorder)
        alpha, sibling = recorder.token_for("alpha"), recorder.token_for("sibling")
        for client in ("alpha", "beta"):
            recorder.authorize("GET", "/emby/Sessions", alpha, MODULE.MISSING, "", client)
        recorder.authorize("GET", "/emby/Devices", sibling, MODULE.MISSING, "", "sibling")
        for token, client in ((alpha, "sibling"), (sibling, "alpha"), (sibling, "beta"),
                              (recorder.admin(), "alpha"), (alpha, "unregistered-client")):
            with self.subTest(client=client), self.assertRaises(RuntimeError):
                recorder.authorize("GET", "/emby/Sessions", token, MODULE.MISSING, "", client)

    def test_creation_and_key_metadata_traffic_require_the_complete_absent_server_baseline(self) -> None:
        conditions = ("incomplete-keys", "existing-key", "incomplete-devices", "changed-device-membership", "missing-server-id",
                      "listed-server", "reported-200", "reported-204", "missing-reported", "missing-numeric",
                      "numeric-500", "numeric-204-body", "numeric-200-ambiguous", "numeric-200-same-server", "gate-not-approved")
        for condition in conditions:
            with self.subTest(condition=condition):
                recorder = self.recorder()
                recorder.key_create_attempts.add(MODULE.KEY_APPS["alpha"])
                recorder.acknowledge_keys([self.key_row("synthetic-earlier-owned-alpha")])
                if condition == "incomplete-keys":
                    recorder.old_key_baseline_complete = False
                elif condition == "existing-key":
                    recorder.old_keys = [self.key_row("synthetic-existing-key")]
                elif condition == "incomplete-devices":
                    recorder.device_baseline_complete = False
                elif condition == "changed-device-membership":
                    recorder.previous_device_ids = {"18", "19"}
                elif condition == "missing-server-id":
                    recorder.server_public_id = ""
                elif condition == "listed-server":
                    recorder.server_baseline_list = [self.server_device()]
                elif condition.startswith("reported-"):
                    recorder.server_baseline_info[recorder.server_public_id] = {
                        "status": int(condition.split("-")[1]), "body": self.server_device()}
                elif condition == "missing-reported":
                    recorder.server_baseline_info.pop(recorder.server_public_id)
                elif condition == "missing-numeric":
                    recorder.server_baseline_info.pop(MODULE.PREVIOUS_SERVER_NUMERIC_ID)
                elif condition == "numeric-500":
                    recorder.server_baseline_info[MODULE.PREVIOUS_SERVER_NUMERIC_ID] = {"status": 500, "body": ""}
                elif condition == "numeric-204-body":
                    recorder.server_baseline_info[MODULE.PREVIOUS_SERVER_NUMERIC_ID] = {"status": 204, "body": {}}
                elif condition == "numeric-200-ambiguous":
                    recorder.server_baseline_info[MODULE.PREVIOUS_SERVER_NUMERIC_ID] = {"status": 200, "body": {}}
                elif condition == "numeric-200-same-server":
                    recorder.server_baseline_info[MODULE.PREVIOUS_SERVER_NUMERIC_ID] = {"status": 200, "body": self.server_device()}
                elif condition == "gate-not-approved":
                    recorder.server_creation_gate["allowed"] = False
                path = "/emby/Auth/Keys?" + MODULE.urlencode({"App": MODULE.KEY_APPS["sibling"]})
                with self.assertRaises(RuntimeError):
                    recorder.request("synthetic-gated-create", "POST", path, token=recorder.admin())
                for client in ("", "alpha", "beta"):
                    with self.subTest(client=client), self.assertRaises(RuntimeError):
                        recorder.request("synthetic-gated-key-traffic", "GET", "/emby/Sessions",
                                         token="synthetic-earlier-owned-alpha", client=client)
                self.assertEqual(recorder.record_count, 0)
                self.assertEqual(recorder.mutations, [])

    def test_empty_historical_numeric_lookup_allows_a_proved_new_server_creation(self) -> None:
        recorder = self.recorder()
        recorder.server_creation_gate = {"evaluated": False, "allowed": False, "blockedReason": "Not reached"}
        self.assertIsNone(recorder.server_creation_reason())
        with patch.object(MODULE.base, "save") as save:
            recorder.establish_server_creation_gate()
        self.assertTrue(recorder.server_creation_gate["allowed"])
        self.assertEqual(recorder.server_creation_gate["applicationKeyTrafficBeforeGate"], 0)
        save.assert_called_once()
        recorder.authorize("POST", "/emby/Auth/Keys?" + MODULE.urlencode({"App": MODULE.KEY_APPS["alpha"]}),
                           recorder.admin(), MODULE.MISSING, "")
        self.assertEqual(recorder.key_create_attempts, set())

    def test_duplicate_app_tokens_require_separate_current_invalidity_proofs(self) -> None:
        recorder = self.recorder()
        self.seed_keys(recorder, duplicate_alpha=True)
        with self.assertRaises(RuntimeError):
            recorder.token_for("alpha")

        def response(_label: str, _method: str, _path: str, *, token: str, client: str = "") -> tuple[int, object]:
            return (200, []) if token == "synthetic-second-alpha-token" else (401, "Synthetic invalid token")

        recorder.request = Mock(side_effect=response)
        recorder.probe_keys("synthetic-partial-cleanup")
        expected = self.expected_key_scopes(recorder)
        self.assertEqual(len(expected), 8)
        self.assertEqual(recorder.request.call_count, 8)
        missing = {scope for scope in expected if scope.startswith(recorder.key_credential_ids["synthetic-second-alpha-token"] + ":")}
        self.assertEqual(expected - set(recorder.key_invalidity_proofs), missing)
        with self.assertRaises(RuntimeError):
            self.finish_audit_in_memory(recorder)
        self.assertFalse(recorder.checks["allAcknowledgedKeyContextsInvalid"])
        recorder.request = Mock(return_value=(401, "Synthetic invalid token"))
        recorder.probe_keys("synthetic-complete-cleanup")
        self.assertEqual(set(recorder.key_invalidity_proofs), expected)
        self.finish_audit_in_memory(recorder)
        self.assertTrue(recorder.checks["allAcknowledgedKeyContextsInvalid"])
        self.assertTrue(recorder.cleanup_ok)

    def test_successful_key_probe_clears_only_its_exact_old_scope_proof(self) -> None:
        recorder = self.recorder()
        self.seed_keys(recorder)
        recorder.request = Mock(return_value=(401, "Synthetic invalid token"))
        recorder.probe_keys("synthetic-old-invalidity")
        expected = self.expected_key_scopes(recorder)
        recorder.request = Mock(return_value=(200, []))
        recorder.probe_key("synthetic-live-alpha", "alpha", client="alpha")
        removed = recorder.key_credential_ids[recorder.token_for("alpha")] + ":alpha"
        self.assertEqual(set(recorder.key_invalidity_proofs), expected - {removed})

    def test_zero_unpaginated_count_does_not_hide_members_from_the_paired_page(self) -> None:
        recorder = self.recorder()
        old = self.key_row("synthetic-visible-old-key")
        recorder.old_keys = None
        recorder.request = Mock(side_effect=[(200, {"Items": [old], "TotalRecordCount": 0}),
                                            (200, {"Items": [old], "TotalRecordCount": 1})])
        self.assertEqual(recorder.key_list("synthetic-nonempty-baseline"), [old])
        self.assertIsNone(recorder.old_keys)
        self.assertEqual(recorder.request.call_args_list[1].args[2], "/emby/Auth/Keys?StartIndex=0&Limit=8")
        proof = recorder.key_rows_observations[0]["completeMembershipProof"]
        self.assertEqual(proof["pagedTotalRecordCount"], 1)
        self.assertTrue(proof["uniqueCredentialMembershipEqual"])

    def test_broken_or_inconsistent_key_pages_cannot_establish_empty_old_keys(self) -> None:
        old = self.key_row("synthetic-visible-old-key")
        other = self.key_row("synthetic-other-key", "sibling")
        cases = [
            ({"Items": [], "TotalRecordCount": 0}, 200, {"Items": [old], "TotalRecordCount": 1}),
            ({"Items": [old], "TotalRecordCount": 0}, 200, {"Items": [old], "TotalRecordCount": 0}),
            ({"Items": [old], "TotalRecordCount": 0}, 200, {"Items": [old], "TotalRecordCount": True}),
            ({"Items": [old], "TotalRecordCount": 0}, 200, {"Items": [old], "TotalRecordCount": "1"}),
            ({"Items": [old], "TotalRecordCount": 0}, 200, {"Items": [old], "TotalRecordCount": 9}),
            ({"Items": [old], "TotalRecordCount": 0}, 200, {"Items": [other], "TotalRecordCount": 1}),
            ({"Items": [old, old], "TotalRecordCount": 0}, 200, {"Items": [old, old], "TotalRecordCount": 2}),
            ({"Items": [], "TotalRecordCount": 0}, 503, {}),
            ({"Items": [], "TotalRecordCount": 0}, 200, {"Items": {}, "TotalRecordCount": 0}),
        ]
        for index, (unpaged, status, paged) in enumerate(cases):
            with self.subTest(case=index):
                recorder = self.recorder()
                recorder.old_keys = None
                recorder.request = Mock(side_effect=[(200, unpaged), (status, paged)])
                with self.assertRaises(RuntimeError):
                    recorder.key_list("synthetic-bad-baseline")
                self.assertIsNone(recorder.old_keys)
                self.assertEqual(recorder.owned, {})
                self.assertEqual(recorder.key_rows_observations, [])
                with self.assertRaises(RuntimeError):
                    recorder.authorize("POST", "/emby/Auth/Keys?" + MODULE.urlencode({"App": MODULE.KEY_APPS["alpha"]}),
                                       recorder.admin(), MODULE.MISSING, "")

    def test_server_mapping_requires_both_current_owned_keys_and_both_identity_dimensions(self) -> None:
        recorder = self.recorder()
        rows = self.seed_keys(recorder)
        selected = self.server_device()
        self.assertIsNone(recorder.server_key_mapping_reason(selected, rows))
        variants = [[], rows[:1], rows + [self.key_row("synthetic-third-token")],
                    [rows[0], {**rows[1], "AccessToken": "synthetic-foreign-token"}],
                    [rows[0], {**rows[1], "AppName": MODULE.KEY_APPS["alpha"]}],
                    [rows[0], {**rows[1], "DeviceId": 45}], [rows[0], {**rows[1], "DeviceId": "44"}],
                    [rows[0], {**rows[1], "DeviceId": True}],
                    [rows[0], {**rows[1], "ReportedDeviceId": "different-server"}]]
        for index, variant in enumerate(variants):
            with self.subTest(case=index):
                self.assertIsInstance(recorder.server_key_mapping_reason(selected, variant), str)
        for wrong in ({**selected, "Id": "0"}, {**selected, "Id": "044"}, {**selected, "Id": 44},
                      {**selected, "ReportedDeviceId": "different-server"}):
            with self.subTest(selected=wrong):
                self.assertIsInstance(recorder.server_key_mapping_reason(wrong, rows), str)

    def test_virtual_delete_refuses_missing_baseline_or_current_ownership_requirements(self) -> None:
        for condition in ("old-key", "listed-server", "reported-200", "reported-204", "missing-reported",
                          "no-candidate", "multiple-candidates", "missing-key", "split-key-mapping"):
            with self.subTest(condition=condition):
                recorder = self.recorder()
                rows = self.seed_keys(recorder)
                selected = self.server_device()
                recorder.server_candidates = {selected["Id"]: selected}
                if condition == "old-key":
                    recorder.old_keys = [self.key_row("synthetic-old-key")]
                elif condition == "listed-server":
                    recorder.server_baseline_list = [selected]
                elif condition.startswith("reported-"):
                    recorder.server_baseline_info[recorder.server_public_id]["status"] = int(condition.split("-")[1])
                elif condition == "missing-reported":
                    recorder.server_baseline_info.pop(recorder.server_public_id)
                elif condition == "no-candidate":
                    recorder.server_candidates = {}
                elif condition == "multiple-candidates":
                    recorder.server_candidates["45"] = {**selected, "Id": "45"}
                elif condition == "missing-key":
                    rows = rows[:1]
                elif condition == "split-key-mapping":
                    rows = [rows[0], {**rows[1], "DeviceId": 45}]
                recorder.key_list = Mock(return_value=rows)
                recorder.mutate_device, recorder.request = Mock(), Mock()
                recorder.devices = Mock(return_value=list(recorder.old_devices.values()))
                recorder.read_server_candidates = Mock()
                recorder.maybe_delete_server_device()
                self.assertFalse(recorder.server_branch["deleteAttempted"])
                self.assertIsInstance(recorder.server_branch["reason"], str)
                self.assertIsNone(recorder.virtual_write_id)
                recorder.mutate_device.assert_not_called()
                recorder.request.assert_not_called()

    def test_protected_hidden_device_aliases_cannot_become_owned_write_targets(self) -> None:
        for field in ("Id", "ReportedDeviceId", "InternalId"):
            with self.subTest(field=field):
                recorder = self.recorder()
                rows = self.seed_keys(recorder)
                selected = {**self.server_device(), "InternalId": 44}
                recorder.protected_hidden_devices = {"15": {"Id": "15", "ReportedDeviceId": recorder.server_public_id}}
                recorder.protected_device_aliases.add(str(selected[field]))
                recorder.server_candidates = {selected["Id"]: selected}
                recorder.key_list, recorder.request, recorder.mutate_device = Mock(return_value=rows), Mock(), Mock()
                recorder.devices, recorder.read_server_candidates = Mock(return_value=[]), Mock()
                recorder.maybe_delete_server_device()
                self.assertFalse(recorder.server_branch["deleteAttempted"])
                recorder.mutate_device.assert_not_called()
                recorder.owned_devices[selected["Id"]] = selected
                recorder.approved_write_ids.add(selected["Id"])
                recorder.read_ids.add(selected["Id"])
                with self.assertRaises(RuntimeError):
                    recorder.prove_write("synthetic-hidden-alias-write", selected["Id"])
                recorder.request.assert_not_called()

    def test_historical_empty_numeric_lookup_does_not_block_a_new_exclusively_owned_device(self) -> None:
        recorder = self.recorder()
        rows = self.seed_keys(recorder)
        selected = self.server_device()
        old_snapshot = copy.deepcopy((recorder.old_devices, recorder.old_options))
        recorder.server_candidates = {selected["Id"]: selected}
        deleted, timeline = [], []

        def devices(label: str) -> list[dict]:
            timeline.append((label, "devices", recorder.admin()))
            return [*recorder.old_devices.values(), recorder.owned_devices[recorder.control_id]]

        def keys(label: str) -> list[dict]:
            timeline.append((label, "key-list", recorder.admin()))
            return [] if deleted else rows

        recorder.key_list, recorder.devices = Mock(side_effect=keys), Mock(side_effect=devices)

        def respond(label: str, method: str, path: str, *, token: str = "", body: object = MODULE.MISSING,
                    identity: str = "", client: str = "") -> tuple[int, object]:
            recorder.authorize(method, path, token, body, identity, client)
            route = MODULE.urlsplit(path).path
            timeline.append((label, route, token))
            if method == "GET" and route == "/emby/Devices/Info":
                return (204, "") if deleted else (200, dict(selected))
            if method == "GET" and route == "/emby/Sessions":
                return (401, "Synthetic retired key") if token in recorder.owned else (200, [])
            if method == "DELETE" and route == "/emby/Devices":
                deleted.append(MODULE.parse_qs(MODULE.urlsplit(path).query)["Id"][0])
                recorder.pending_write_proof = None
                recorder.acknowledged_device_deletes[deleted[-1]] = {"label": label, "status": 204}
                return 204, ""
            raise AssertionError("Unexpected virtual-device workflow request")

        recorder.request = Mock(side_effect=respond)
        recorder.maybe_delete_server_device()
        self.assertTrue(recorder.server_branch["deleteAttempted"])
        self.assertTrue(recorder.server_branch["verified"])
        self.assertEqual(deleted, ["44"])
        self.assertEqual(recorder.virtual_write_id, "44")
        self.assertEqual((recorder.old_devices, recorder.old_options), old_snapshot)
        self.assertEqual(recorder.server_baseline_info[MODULE.PREVIOUS_SERVER_NUMERIC_ID], {"status": 204, "body": ""})
        key_probes = [index for index, (_label, route, token) in enumerate(timeline)
                      if route == "/emby/Sessions" and token in recorder.owned]
        control_probes = [index for index, (_label, route, token) in enumerate(timeline)
                          if route == "/emby/Sessions" and token == recorder.admin()]
        after_devices = next(index for index, (label, _route, _token) in enumerate(timeline)
                             if label == "server-delete-devices-after-key-probes")
        self.assertEqual(len(control_probes), 2)
        self.assertLess(control_probes[0], min(key_probes))
        self.assertLess(max(key_probes), after_devices)
        self.assertLess(after_devices, control_probes[1])
        recorder.pending_write_proof = {"targetId": "44", "matchedOwnedIdentity": True,
                                        "previouslyProvedTargetMissing": False}
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", recorder.route("/Options", "44"), recorder.admin(),
                               {"CustomName": MODULE.device.CUSTOM_PREFIX + "Forbidden Virtual Rename"}, "")

    def test_fresh_server_lookup_cannot_reuse_an_earlier_positive_candidate(self) -> None:
        recorder = self.recorder()
        rows = self.seed_keys(recorder)
        recorder.server_candidates = {"44": self.server_device()}
        recorder.key_list = Mock(return_value=rows)
        recorder.request = Mock(return_value=(204, ""))
        recorder.read_server_candidates("synthetic-refresh-without-positive-info", [])
        self.assertEqual(recorder.server_candidates, {})
        self.assertEqual(recorder.request.call_count, 2)

    def test_unknown_header_metadata_is_not_claimed_and_old_devices_remain_unwritable(self) -> None:
        recorder = self.recorder()
        metadata = recorder.client_metadata("alpha")
        known = {"Id": "55", "ReportedDeviceId": metadata["DeviceId"], "AppName": metadata["Client"]}
        self.assertTrue(recorder.owned_pair(known))
        unknown = {**known, "ReportedDeviceId": MODULE.device.DEVICE_PREFIX + "unregistered-client"}
        mismatch = {**known, "AppName": "Unrelated Client"}
        self.assertFalse(recorder.owned_pair(unknown))
        self.assertFalse(recorder.owned_pair(mismatch))
        old_snapshot = copy.deepcopy(recorder.old_devices)
        recorder.request = Mock(return_value=(200, {"Items": [unknown, *recorder.old_devices.values()], "TotalRecordCount": 0}))
        recorder.devices("synthetic-unknown-header-list")
        self.assertNotIn("55", recorder.owned_devices)
        recorder.request.reset_mock()
        for target in ("55", "19"):
            with self.subTest(target=target), self.assertRaises(RuntimeError):
                recorder.mutate_device("synthetic-unowned-delete", "DELETE", target, token=recorder.admin())
        recorder.request.assert_not_called()
        self.assertEqual(recorder.old_devices, old_snapshot)


def main() -> None:
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(KeyDeviceSafetyTests)
    stream = io.StringIO()
    forbidden = RuntimeError("Synthetic safety checks forbid external side effects")
    with contextlib.ExitStack() as guards:
        targets = [(socket, "socket"), (socket, "create_connection"), (http.client, "HTTPConnection"),
                   (subprocess, "Popen"), (subprocess, "run"), (subprocess, "check_output"),
                   (os, "execvp"), (os, "system"), (os, "open"), (os, "remove"), (os, "unlink"),
                   (os, "rename"), (os, "replace"), (Path, "mkdir"), (Path, "write_text"), (Path, "write_bytes"),
                   (Path, "read_text"), (Path, "read_bytes"), (Path, "glob"), (MODULE.signal, "setitimer"),
                   (MODULE.time, "sleep"), (MODULE.base, "save"), (MODULE.base, "private_file"),
                   (MODULE.Recorder, "__init__"), (MODULE.device.Recorder, "__init__"), (MODULE.base.Recorder, "__init__")]
        for owner, name in targets:
            guards.enter_context(patch.object(owner, name, side_effect=forbidden))
        result = unittest.TextTestRunner(stream=stream).run(suite)
    print(json.dumps({"suite": "reference-key-device-hardening", "result": "passed" if result.wasSuccessful() else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "failedTests": [test.id() for test, _ in result.failures + result.errors],
                      "recorderSha256": SOURCE_HASH, "testSha256": TEST_HASH,
                      "liveHTTPRequests": 0, "captureWrites": 0, "processCalls": 0}, sort_keys=True))
    if not result.wasSuccessful():
        print(stream.getvalue(), file=sys.stderr)
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
