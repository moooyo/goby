#!/usr/bin/env python3
"""Run bounded synthetic device-recorder safety checks through authorized SSH.

Only the recorder and test sources are read. Recorder initialization is never
called. All HTTP responses, persistence, and capture output are in memory;
network, process, timer, and filesystem mutation entry points are blocked.
"""

from __future__ import annotations

import contextlib
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
    print(json.dumps({"suite": "reference-device-hardening", "result": "blocked",
                      "reason": "Authorized root SSH and one recorder source path are required"}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SOURCE_HASH = hashlib.sha256(SOURCE.read_bytes()).hexdigest()
TEST_HASH = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
SPEC = importlib.util.spec_from_file_location("reference_devices_under_test", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class FakeResponse:
    def __init__(self, status: int, body: object) -> None:
        self.status = status
        self.body = json.dumps(body).encode()
        self.length = 0

    def read(self, limit: int) -> bytes:
        return self.body[:limit]

    def isclosed(self) -> bool:
        return True

    def getheaders(self) -> list[tuple[str, str]]:
        return [("Content-Type", "application/json")]


class FakeConnection:
    def __init__(self, response: FakeResponse) -> None:
        self.response = response
        self.calls: list[tuple] = []
        self.closed = False

    def request(self, *arguments: object) -> None:
        self.calls.append(arguments)

    def getresponse(self) -> FakeResponse:
        return self.response

    def close(self) -> None:
        self.closed = True


class DeviceSafetyTests(unittest.TestCase):
    def recorder(self) -> object:
        recorder = MODULE.Recorder.__new__(MODULE.Recorder)
        recorder.started = MODULE.time.monotonic()
        recorder.deadline = recorder.started + 60
        recorder.finishing = False
        recorder.record_count = 0
        recorder.charged_bytes = 0
        recorder.total = 0
        recorder.incomplete_count = 0
        recorder.login_attempts = set()
        recorder.login_statuses = {}
        recorder.login_delays = []
        recorder.forbidden_tokens = {"synthetic-preexisting-admin-token", "synthetic-preexisting-viewer-token"}
        recorder.logins = {"control": {"AccessToken": "synthetic-new-control-token"}}
        recorder.account_credentials = {
            "admin": {"REFERENCE_USERNAME": "Synthetic Admin", "REFERENCE_PASSWORD": "synthetic-admin-password"},
            "viewer": {"REFERENCE_USERNAME": "Synthetic Viewer", "REFERENCE_PASSWORD": "synthetic-viewer-password"},
        }
        recorder.secrets = set()
        recorder.invalid_tokens = set()
        recorder.invalid_login_proofs = {}
        recorder.logged_out = set()
        recorder.logout_statuses = {}
        recorder.old_devices = {"preexisting-device-id": {
            "Id": "preexisting-device-id", "ReportedDeviceId": "preexisting-reported-id",
            "InternalId": 41, "AppName": "Preexisting App",
        }}
        recorder.control_id = "synthetic-control-route-id"
        recorder.owned_devices = {"synthetic-owned-alpha-id": self.device("synthetic-owned-alpha-id"),
                                  recorder.control_id: self.device(recorder.control_id, "control")}
        recorder.protected_device_aliases = {
            "preexisting-device-id", "preexisting-reported-id", "41", recorder.control_id,
            recorder.owned_devices[recorder.control_id]["ReportedDeviceId"],
        }
        recorder.read_ids = {MODULE.UNKNOWN_ID, *recorder.old_devices, *recorder.owned_devices}
        recorder.ambiguous_ids = set()
        recorder.approved_write_ids = set()
        recorder.acknowledged_device_deletes = {}
        recorder.pending_write_proof = None
        recorder.write_proofs = []
        recorder.device_observations = []
        recorder.mutations = []
        recorder.cleanup_errors = []
        recorder.write = Mock()
        return recorder

    @staticmethod
    def device(device_id: str, identity: str = "alpha-admin") -> dict:
        metadata = MODULE.Recorder.metadata(identity)
        return {"Id": device_id, "ReportedDeviceId": metadata["DeviceId"],
                "InternalId": 100 if identity == "control" else 101,
                "AppName": metadata["Client"], "Name": metadata["Device"]}

    @staticmethod
    def lookup(recorder: object, status: int, info: object) -> Mock:
        def respond(label: str, method: str, path: str, *, token: str = "", body: object = MODULE.MISSING,
                    identity: str = "") -> tuple[int, object]:
            recorder.authorize(method, path, token, body, identity)
            if method == "GET":
                return status, info
            return 204, ""

        recorder.request = Mock(side_effect=respond)
        return recorder.request

    @staticmethod
    def absence_lookup(recorder: object, list_status: int, listing: object, *, info_body: object = "") -> Mock:
        def respond(label: str, method: str, path: str, *, token: str = "", body: object = MODULE.MISSING,
                    identity: str = "") -> tuple[int, object]:
            recorder.authorize(method, path, token, body, identity)
            route = MODULE.urlsplit(path).path
            if method == "GET" and route == "/emby/Devices/Info":
                return 204, info_body
            if method == "GET" and route == "/emby/Devices":
                return list_status, listing
            raise AssertionError("An absence proof attempted an unexpected request")

        recorder.request = Mock(side_effect=respond)
        return recorder.request

    def test_unknown_and_unowned_devices_never_reach_a_mutation_request(self) -> None:
        for target in (MODULE.UNKNOWN_ID, MODULE.UNKNOWN_NUMERIC_ID, "unowned-listed-device",
                       "preexisting-device-id", "synthetic-control-route-id"):
            with self.subTest(target=target):
                recorder = self.recorder()
                recorder.request = Mock(return_value=(200, {"Items": [{
                    "Id": "unowned-listed-device", "ReportedDeviceId": "unowned-reported-id", "AppName": "Other App",
                }], "TotalRecordCount": 1}))
                recorder.devices("synthetic-list")
                self.assertNotIn("unowned-listed-device", recorder.owned_devices)
                recorder.request.reset_mock()
                with self.assertRaises(RuntimeError):
                    recorder.mutate_device("synthetic-delete", "DELETE", target, token=recorder.admin())
                recorder.request.assert_not_called()
                self.assertEqual(recorder.mutations, [])

    def test_owned_list_id_and_historical_approval_do_not_replace_current_route_proof(self) -> None:
        for historically_approved in (False, True):
            with self.subTest(historically_approved=historically_approved):
                recorder = self.recorder()
                target = "synthetic-owned-alpha-id"
                if historically_approved:
                    recorder.approved_write_ids.add(target)
                for method, suffix, body in (("DELETE", "", MODULE.MISSING),
                                             ("POST", "/Delete", MODULE.MISSING),
                                             ("POST", "/Options", {"CustomName": MODULE.CUSTOM_PREFIX + "Rename"})):
                    with self.assertRaises(RuntimeError):
                        recorder.authorize(method, recorder.route(suffix, target), recorder.admin(), body, "")
                self.assertEqual(recorder.write_proofs, [])
                self.assertIsNone(recorder.pending_write_proof)

    def test_positive_info_identity_proof_allows_only_the_proved_target(self) -> None:
        recorder = self.recorder()
        target = "synthetic-owned-alpha-id"
        other = "synthetic-other-owned-id"
        recorder.owned_devices[other] = self.device(other)
        recorder.read_ids.add(other)
        recorder.approved_write_ids.add(other)
        recorder.acknowledged_device_deletes[target] = {"label": "synthetic-earlier-delete", "status": 204}
        self.lookup(recorder, 200, dict(recorder.owned_devices[target]))
        recorder.prove_write("synthetic-proof", target)
        self.assertNotIn(target, recorder.acknowledged_device_deletes)
        self.assertTrue(recorder.pending_write_proof["matchedOwnedIdentity"])
        self.assertEqual(recorder.pending_write_proof["targetId"], target)
        recorder.authorize("POST", recorder.route("/Options", target), recorder.admin(),
                           {"CustomName": MODULE.CUSTOM_PREFIX + "Rename"}, "")
        with self.assertRaises(RuntimeError):
            recorder.authorize("DELETE", recorder.route("", other), recorder.admin(), MODULE.MISSING, "")

    def test_mismatched_route_identity_blocks_the_write_and_later_cleanup_write(self) -> None:
        for field in ("Id", "ReportedDeviceId", "AppName"):
            with self.subTest(field=field):
                recorder = self.recorder()
                target = "synthetic-owned-alpha-id"
                recorder.approved_write_ids.add(target)
                observed = dict(recorder.owned_devices[target])
                observed[field] = "unowned-routed-value"
                requests = self.lookup(recorder, 200, observed)
                with self.assertRaises(RuntimeError):
                    recorder.mutate_device("synthetic-rename", "POST", target, suffix="/Options",
                                           token=recorder.admin(), body={"CustomName": MODULE.CUSTOM_PREFIX + "Rename"})
                self.assertNotIn(target, recorder.approved_write_ids)
                self.assertIn(target, recorder.ambiguous_ids)
                self.assertIsNone(recorder.pending_write_proof)
                self.assertEqual([call.args[1] for call in requests.call_args_list], ["GET"])
                recorder.finishing = True
                with patch.object(MODULE.base, "save") as save:
                    outcome = recorder.cleanup_step("synthetic-cleanup-delete", lambda: recorder.mutate_device(
                        "synthetic-cleanup-delete", "DELETE", target, token=recorder.admin()))
                self.assertIsNone(outcome)
                self.assertEqual(len(recorder.cleanup_errors), 1)
                self.assertEqual(recorder.cleanup_errors[0]["stage"], "synthetic-cleanup-delete")
                save.assert_called_once()
                self.assertEqual([call.args[1] for call in requests.call_args_list], ["GET"])
                self.assertEqual(recorder.mutations, [])

    def test_old_device_alias_collisions_fail_before_lookup_or_direct_write(self) -> None:
        old = {"Id": "preexisting-device-id", "ReportedDeviceId": "preexisting-reported-id", "InternalId": 41}
        for field, alias in old.items():
            with self.subTest(field=field):
                recorder = self.recorder()
                target = str(alias)
                recorder.owned_devices[target] = self.device(target)
                recorder.read_ids.add(target)
                recorder.approved_write_ids.add(target)
                recorder.pending_write_proof = {"targetId": target, "matchedOwnedIdentity": True,
                                                "previouslyProvedTargetMissing": False}
                recorder.request = Mock()
                with self.assertRaises(RuntimeError):
                    recorder.prove_write("synthetic-collision", target)
                recorder.request.assert_not_called()
                recorder.pending_write_proof = {"targetId": target, "matchedOwnedIdentity": True,
                                                "previouslyProvedTargetMissing": False}
                with self.assertRaises(RuntimeError):
                    recorder.authorize("DELETE", recorder.route("", target), recorder.admin(), MODULE.MISSING, "")

    def test_previously_proved_missing_target_allows_repeat_delete_but_never_options(self) -> None:
        recorder = self.recorder()
        target = "synthetic-owned-alpha-id"
        self.lookup(recorder, 200, dict(recorder.owned_devices[target]))
        recorder.prove_write("synthetic-initial-proof", target)
        self.lookup(recorder, 404, {"Error": "Synthetic missing device"})
        recorder.prove_write("synthetic-repeat-proof", target, allow_missing=True)
        self.assertFalse(recorder.pending_write_proof["matchedOwnedIdentity"])
        self.assertTrue(recorder.pending_write_proof["previouslyProvedTargetMissing"])
        for method, suffix in (("DELETE", ""), ("POST", "/Delete")):
            recorder.authorize(method, recorder.route(suffix, target), recorder.admin(), MODULE.MISSING, "")
        for body in ({}, {"CustomName": None}, {"CustomName": ""}, {"CustomName": MODULE.CUSTOM_PREFIX + "Phantom"}):
            with self.subTest(body=body), self.assertRaises(RuntimeError):
                recorder.authorize("POST", recorder.route("/Options", target), recorder.admin(), body, "")
        requests = self.lookup(recorder, 404, {"Error": "Synthetic missing device"})
        with self.assertRaises(RuntimeError):
            recorder.mutate_device("synthetic-options-after-delete", "POST", target, suffix="/Options",
                                   token=recorder.admin(), body={"CustomName": MODULE.CUSTOM_PREFIX + "Phantom"})
        self.assertEqual([call.args[1] for call in requests.call_args_list], ["GET"])

    def test_unproved_missing_or_non_identity_response_never_authorizes_cleanup(self) -> None:
        for status, info in ((404, {}), (204, ""), (200, {}), (200, []), (403, {}), (500, {})):
            with self.subTest(status=status, body_type=type(info).__name__):
                recorder = self.recorder()
                target = "synthetic-owned-alpha-id"
                requests = self.lookup(recorder, status, info)
                with self.assertRaises(RuntimeError):
                    recorder.prove_write("synthetic-invalid-proof", target, allow_missing=True)
                self.assertNotIn(target, recorder.approved_write_ids)
                self.assertIsNone(recorder.pending_write_proof)
                self.assertIn(target, recorder.ambiguous_ids)
                self.assertEqual([call.args[1] for call in requests.call_args_list], ["GET"])

    def test_empty_204_requires_prior_positive_identity_and_a_fresh_complete_absence_list(self) -> None:
        recorder = self.recorder()
        target = "synthetic-owned-alpha-id"
        self.lookup(recorder, 200, dict(recorder.owned_devices[target]))
        recorder.prove_write("synthetic-positive-before-204", target)
        recorder.acknowledged_device_deletes[target] = {"label": "synthetic-acknowledged-delete", "status": 204}
        rows = [*recorder.old_devices.values(), recorder.owned_devices[recorder.control_id]]
        # The observed reference reports zero even for a nonempty complete
        # device list. Preserved membership, not this count, proves completeness.
        requests = self.absence_lookup(recorder, 200, {"Items": rows, "TotalRecordCount": 0})
        recorder.prove_write("synthetic-204-with-absence", target, allow_missing=True)
        self.assertEqual([MODULE.urlsplit(call.args[2]).path for call in requests.call_args_list],
                         ["/emby/Devices/Info", "/emby/Devices"])
        self.assertFalse(recorder.pending_write_proof["matchedOwnedIdentity"])
        self.assertTrue(recorder.pending_write_proof["previouslyProvedTargetMissing"])
        self.assertEqual(recorder.pending_write_proof["status"], 204)
        for method, suffix in (("DELETE", ""), ("POST", "/Delete")):
            recorder.authorize(method, recorder.route(suffix, target), recorder.admin(), MODULE.MISSING, "")
        for body in ({}, {"CustomName": None}, {"CustomName": ""}, {"CustomName": MODULE.CUSTOM_PREFIX + "Phantom"}):
            with self.subTest(body=body), self.assertRaises(RuntimeError):
                recorder.authorize("POST", recorder.route("/Options", target), recorder.admin(), body, "")

    def test_empty_204_never_uses_failed_partial_ambiguous_or_present_device_lists(self) -> None:
        old = {"Id": "preexisting-device-id", "ReportedDeviceId": "preexisting-reported-id",
               "InternalId": 41, "AppName": "Preexisting App"}
        control = self.device("synthetic-control-route-id", "control")
        unexpected = {"Id": "unexplained-device-id", "ReportedDeviceId": "unexplained-reported-id", "AppName": "Other App"}
        cases = [
            (503, {}), (200, []), (200, {"Items": {}, "TotalRecordCount": 0}),
            (200, {"Items": [], "TotalRecordCount": 0}),
            (200, {"Items": [old], "TotalRecordCount": 0}),
            (200, {"Items": [control], "TotalRecordCount": 0}),
            (200, {"Items": [old, old, control], "TotalRecordCount": 0}),
            (200, {"Items": [old, control, unexpected], "TotalRecordCount": 0}),
            (200, {"Items": [old, control, self.device("synthetic-owned-alpha-id")], "TotalRecordCount": 0}),
        ]
        for index, (status, listing) in enumerate(cases):
            with self.subTest(case=index):
                recorder = self.recorder()
                target = "synthetic-owned-alpha-id"
                self.lookup(recorder, 200, dict(recorder.owned_devices[target]))
                recorder.prove_write("synthetic-positive-before-invalid-list", target)
                recorder.acknowledged_device_deletes[target] = {"label": "synthetic-acknowledged-delete", "status": 204}
                requests = self.absence_lookup(recorder, status, listing)
                with self.assertRaises(RuntimeError):
                    recorder.prove_write("synthetic-204-invalid-list", target, allow_missing=True)
                self.assertIsNone(recorder.pending_write_proof)
                self.assertTrue(all(call.args[1] == "GET" for call in requests.call_args_list))
                self.assertEqual(recorder.mutations, [])
                with self.assertRaises(RuntimeError):
                    recorder.authorize("DELETE", recorder.route("", target), recorder.admin(), MODULE.MISSING, "")

    def test_empty_204_cannot_hide_an_owned_device_without_acknowledged_deletion(self) -> None:
        for missing_target in (True, False):
            with self.subTest(missing_target=missing_target):
                recorder = self.recorder()
                target = "synthetic-owned-alpha-id"
                self.lookup(recorder, 200, dict(recorder.owned_devices[target]))
                recorder.prove_write("synthetic-positive-before-unexplained-absence", target)
                if not missing_target:
                    recorder.acknowledged_device_deletes[target] = {"label": "synthetic-acknowledged-delete", "status": 204}
                    other = "synthetic-owned-beta-id"
                    recorder.owned_devices[other] = self.device(other, "beta-viewer")
                    recorder.read_ids.add(other)
                rows = [*recorder.old_devices.values(), recorder.owned_devices[recorder.control_id]]
                requests = self.absence_lookup(recorder, 200, {"Items": rows, "TotalRecordCount": 0})
                with self.assertRaises(RuntimeError):
                    recorder.prove_write("synthetic-204-unexplained-absence", target, allow_missing=True)
                self.assertIsNone(recorder.pending_write_proof)
                self.assertTrue(all(call.args[1] == "GET" for call in requests.call_args_list))

    def test_run_selection_accepts_only_the_two_explicit_reviewed_values(self) -> None:
        self.assertEqual(MODULE.selected_run("1"), 1)
        self.assertEqual(MODULE.selected_run("2"), 2)
        for value in ("", "0", "3", "99", "01", "02", "+1", "-1", "1.0", " 1", "2 ", "2\n", "\n1", "\u0661", "\uff12"):
            with self.subTest(value=value), self.assertRaises(RuntimeError):
                MODULE.selected_run(value)

    def test_run_selection_does_not_probe_existing_paths_or_automatically_advance(self) -> None:
        with patch.object(Path, "exists", return_value=True) as exists, \
                patch.object(Path, "is_symlink", return_value=True) as symlink:
            self.assertEqual(MODULE.selected_run("1"), 1)
            self.assertEqual(MODULE.selected_run("2"), 2)
            self.assertEqual(MODULE.selected_run("1"), 1)
            with self.assertRaises(RuntimeError):
                MODULE.selected_run("3")
        exists.assert_not_called()
        symlink.assert_not_called()

    def test_204_with_body_or_options_intent_cannot_obtain_missing_target_permission(self) -> None:
        for info_body, allow_missing in (({}, True), ("unexpected-body", True), (None, True), ("", False)):
            with self.subTest(body_type=type(info_body).__name__, allow_missing=allow_missing):
                recorder = self.recorder()
                target = "synthetic-owned-alpha-id"
                self.lookup(recorder, 200, dict(recorder.owned_devices[target]))
                recorder.prove_write("synthetic-positive-before-invalid-204", target)
                requests = self.absence_lookup(recorder, 200, {"Items": [], "TotalRecordCount": 0}, info_body=info_body)
                with self.assertRaises(RuntimeError):
                    recorder.prove_write("synthetic-invalid-204", target, allow_missing=allow_missing)
                self.assertIsNone(recorder.pending_write_proof)
                self.assertTrue(all(call.args[1] == "GET" for call in requests.call_args_list))
                self.assertEqual(recorder.mutations, [])

    def test_existing_credential_is_never_accepted_for_http_or_logout(self) -> None:
        for token in ("synthetic-preexisting-admin-token", "synthetic-preexisting-viewer-token"):
            with self.subTest(token=token):
                recorder = self.recorder()
                recorder.logins["alpha-admin"] = {"AccessToken": token}
                for method, path in (("GET", "/emby/Sessions"), ("POST", "/emby/Sessions/Logout")):
                    with self.assertRaisesRegex(RuntimeError, "credential is not acknowledged and owned"):
                        recorder.request("synthetic-old-token", method, path, token=token)
                with self.assertRaisesRegex(RuntimeError, "credential is not acknowledged and owned"):
                    recorder.logout("alpha-admin")
                self.assertEqual(recorder.record_count, 0)
                self.assertEqual(recorder.mutations, [])
                self.assertEqual(recorder.logged_out, set())

    def test_successful_probe_invalidates_stale_cleanup_proofs_for_every_shared_token(self) -> None:
        recorder = self.recorder()
        shared = "synthetic-shared-owned-token"
        unrelated = "synthetic-unrelated-owned-token"
        recorder.logins.update({"alpha-viewer": {"AccessToken": shared}, "alpha-other-client": {"AccessToken": shared},
                               "beta-viewer": {"AccessToken": unrelated}})
        recorder.invalid_tokens = {shared, unrelated}
        recorder.invalid_login_proofs = {identity: {"label": "synthetic-old-invalidity", "status": 401}
                                         for identity in ("alpha-viewer", "alpha-other-client", "beta-viewer")}
        recorder.request = Mock(return_value=(200, []))
        self.assertEqual(recorder.probe_login("synthetic-live-after-relogin", "alpha-viewer"), 200)
        self.assertNotIn(shared, recorder.invalid_tokens)
        self.assertNotIn("alpha-viewer", recorder.invalid_login_proofs)
        self.assertNotIn("alpha-other-client", recorder.invalid_login_proofs)
        self.assertIn(unrelated, recorder.invalid_tokens)
        self.assertIn("beta-viewer", recorder.invalid_login_proofs)
        recorder.request = Mock(return_value=(401, "Synthetic invalid token"))
        recorder.probe_login("synthetic-final-invalidity", "alpha-other-client")
        self.assertIn(shared, recorder.invalid_tokens)
        for identity in ("alpha-viewer", "alpha-other-client"):
            self.assertEqual(recorder.invalid_login_proofs[identity], {"label": "synthetic-final-invalidity", "status": 401})

    def test_returned_existing_login_token_is_not_owned_or_scheduled_for_logout(self) -> None:
        recorder = self.recorder()
        token = "synthetic-preexisting-admin-token"
        connection = FakeConnection(FakeResponse(200, {"AccessToken": token, "User": {"Id": "synthetic-user"}}))
        saved = []
        with patch.object(MODULE.http.client, "HTTPConnection", return_value=connection), \
                patch.object(MODULE.base, "save", side_effect=lambda path, value: saved.append((str(path), value))), \
                patch.object(MODULE.signal, "setitimer", return_value=0), contextlib.redirect_stdout(io.StringIO()):
            recorder.request("synthetic-returned-old-token", "POST", "/emby/Users/AuthenticateByName",
                             identity="alpha-admin", body={"Username": "Synthetic Admin", "Pw": "synthetic-admin-password"})
        self.assertNotIn("alpha-admin", recorder.logins)
        self.assertIn(token, recorder.secrets)
        self.assertFalse(recorder.mutations[0]["acknowledgedLogin"])
        self.assertEqual(recorder.cleanup_errors, [{"stage": "login", "identity": "alpha-admin", "error": "ExistingCredentialReturned"}])
        self.assertTrue(connection.closed)
        self.assertEqual(len(connection.calls), 1)
        self.assertEqual(len(saved), 2)
        recorder.request = Mock()
        recorder.logout("alpha-admin")
        recorder.request.assert_not_called()

    def test_acknowledged_new_login_survives_private_persistence_failure_before_dto_checks(self) -> None:
        for status in (200, 500):
            with self.subTest(status=status):
                recorder = self.recorder()
                token = "synthetic-acknowledged-new-token"
                payload = {"AccessToken": token, "User": "Synthetic invalid later DTO"}
                connection = FakeConnection(FakeResponse(status, payload))
                saved_intents = []

                def save(path: Path, value: object) -> None:
                    if path.name == "alpha-admin-login-response.json":
                        raise OSError("Synthetic persistence failure")
                    saved_intents.append((str(path), value))

                with patch.object(MODULE.http.client, "HTTPConnection", return_value=connection), \
                        patch.object(MODULE.base, "save", side_effect=save), \
                        patch.object(MODULE.signal, "setitimer", return_value=0), \
                        patch.object(MODULE.time, "sleep", return_value=None):
                    with self.assertRaisesRegex(OSError, "Synthetic persistence failure"):
                        recorder.login("alpha-admin")
                self.assertEqual(recorder.logins["alpha-admin"], payload)
                self.assertIn(token, recorder.secrets)
                self.assertEqual(recorder.login_attempts, {"alpha-admin"})
                self.assertEqual(recorder.login_statuses, {"alpha-admin": status})
                self.assertTrue(recorder.mutations[0]["acknowledgedLogin"])
                self.assertEqual(recorder.mutations[0]["responseStatus"], status)
                self.assertEqual(len(saved_intents), 1)
                self.assertEqual(recorder.record_count, 1)
                self.assertTrue(connection.closed)
                self.assertEqual(len(connection.calls), 1)
                recorder.write.assert_not_called()
                recorder.authorize("POST", "/emby/Sessions/Logout", token, MODULE.MISSING, "")

    def test_completed_mutation_consumes_proof_and_cannot_be_replayed_without_lookup(self) -> None:
        recorder = self.recorder()
        target = "synthetic-owned-alpha-id"
        with patch.object(recorder, "request", return_value=(200, dict(recorder.owned_devices[target]))):
            recorder.prove_write("synthetic-proof", target)
        connection = FakeConnection(FakeResponse(204, ""))
        with patch.object(MODULE.http.client, "HTTPConnection", return_value=connection), \
                patch.object(MODULE.base, "save"), patch.object(MODULE.signal, "setitimer", return_value=0), \
                contextlib.redirect_stdout(io.StringIO()):
            recorder.request("synthetic-delete", "DELETE", recorder.route("", target), token=recorder.admin())
            self.assertIsNone(recorder.pending_write_proof)
            with self.assertRaises(RuntimeError):
                recorder.request("synthetic-unproved-repeat", "DELETE", recorder.route("", target), token=recorder.admin())
        self.assertEqual(len(connection.calls), 1)
        self.assertEqual(len(recorder.mutations), 1)
        self.assertEqual(recorder.record_count, 1)
        self.assertEqual(recorder.acknowledged_device_deletes[target], {"label": "synthetic-delete", "status": 204})

    def test_acknowledged_delete_remains_tracked_when_capture_output_fails(self) -> None:
        recorder = self.recorder()
        target = "synthetic-owned-alpha-id"
        with patch.object(recorder, "request", return_value=(200, dict(recorder.owned_devices[target]))):
            recorder.prove_write("synthetic-delete-proof", target)
        connection = FakeConnection(FakeResponse(204, ""))
        with patch.object(MODULE.http.client, "HTTPConnection", return_value=connection), \
                patch.object(MODULE.base, "save"), patch.object(MODULE.signal, "setitimer", return_value=0), \
                patch.object(recorder, "write", side_effect=OSError("Synthetic capture output failure")):
            with self.assertRaisesRegex(OSError, "Synthetic capture output failure"):
                recorder.request("synthetic-delete-before-output-failure", "DELETE", recorder.route("", target), token=recorder.admin())
        self.assertIsNone(recorder.pending_write_proof)
        self.assertEqual(recorder.acknowledged_device_deletes[target],
                         {"label": "synthetic-delete-before-output-failure", "status": 204})
        self.assertEqual(recorder.mutations[0]["responseStatus"], 204)
        self.assertTrue(connection.closed)
        self.assertEqual(len(connection.calls), 1)


def main() -> None:
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(DeviceSafetyTests)
    stream = io.StringIO()
    forbidden = RuntimeError("Synthetic safety checks forbid external side effects")
    with contextlib.ExitStack() as guards:
        targets = [(socket, "socket"), (socket, "create_connection"), (http.client, "HTTPConnection"),
                   (subprocess, "Popen"), (subprocess, "run"), (subprocess, "check_output"),
                   (os, "execvp"), (os, "system"), (os, "open"), (os, "remove"), (os, "unlink"),
                   (os, "rename"), (os, "replace"), (Path, "mkdir"), (Path, "write_text"),
                   (Path, "write_bytes"), (Path, "read_text"), (Path, "read_bytes"),
                   (MODULE.signal, "setitimer"), (MODULE.time, "sleep"), (MODULE.base, "save"), (MODULE.base, "private_file"),
                   (MODULE.Recorder, "__init__")]
        for owner, name in targets:
            guards.enter_context(patch.object(owner, name, side_effect=forbidden))
        result = unittest.TextTestRunner(stream=stream).run(suite)
    print(json.dumps({"suite": "reference-device-hardening", "result": "passed" if result.wasSuccessful() else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "failedTests": [test.id() for test, _ in result.failures + result.errors],
                      "recorderSha256": SOURCE_HASH, "testSha256": TEST_HASH,
                      "liveHTTPRequests": 0, "captureWrites": 0, "processCalls": 0}, sort_keys=True))
    if not result.wasSuccessful():
        print(stream.getvalue(), file=sys.stderr)
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
