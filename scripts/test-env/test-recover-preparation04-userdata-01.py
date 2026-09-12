#!/usr/bin/env python3
"""Remote synthetic recovery wire and real exclusive-journal persistence guards."""

import argparse
import base64
from copy import deepcopy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SOURCE = TRANSPORT = None
STAMP = "2026-09-13T02:00:00+00:00"


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


class FakeAuthority:
    def __init__(self, root):
        self.support, self.files = TRANSPORT, {}
        self.value = {"runId": "synthetic-recovery", "outputRoot": str(root / "run"), "readonlyRoots": [],
            "endpoint": {"scheme": "http", "host": "127.0.0.1", "port": 18197},
            "actor": {"userId": "owned-P4", "username": "Owned P4", "credentialRef": "owned-P4-credential", "deviceId": "new-recovery-device"},
            "expected": {"ownedPath": "/synthetic/preparation04/media/LA/Series/Season 01/Series S01E01.mp4"}}
        self.raw = SOURCE.canonical(self.value).encode()
        self.credential = {"username": "Owned P4", "password": "synthetic-password-" + "x" * 40, "credentialRef": "owned-P4-credential"}
        self.parent_state = {"tokens": {actor: "old-token-" + actor for actor in ("admin", "P", "Q")},
            "sessions": {actor: "old-session-" + actor for actor in ("admin", "P", "Q")},
            "libraries": {"LA": {"policyFolderId": "a" * 32}, "LB": {"policyFolderId": "b" * 32}}}
        self.prior = {"server": {"id": "owned-server"}}
        self.mapped = {"id": "125", "parentId": "122", "seriesId": "121", "indexNumber": 1, "parentIndexNumber": 1, "runtimeTicks": 6000000000}
        self.zero = {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0, "IsFavorite": False}
        self.partial = {"Played": False, "PlayCount": 1, "PlaybackPositionTicks": 1200000000, "IsFavorite": False,
            "PlayedPercentage": 20, "LastPlayedDate": "2026-09-12T20:52:59.0000000Z"}
        self.checks, self.closed = 0, False

    def body(self, userdata):
        return {"Id": "125", "Type": "Episode", "ParentId": "122", "SeasonId": "122", "SeriesId": "121",
            "IndexNumber": 1, "ParentIndexNumber": 1, "RunTimeTicks": 6000000000,
            "Path": self.value["expected"]["ownedPath"], "UserData": deepcopy(userdata)}

    def detail(self, body):
        return SOURCE.Authority.detail(self, body)

    def acquire(self): self.check()
    def check(self): self.checks += 1
    def close(self): self.closed = True


class FakeWire:
    def __init__(self, authority):
        self.authority, self.requests, self.overrides = authority, [], {}
        self.token, self.session = "new-synthetic-recovery-token", "new-synthetic-recovery-session"
        self.restored = False

    def response(self, status, body=None, *, raw=None, complete=True, headers=None):
        raw = (b"" if body is None else SOURCE.canonical(body).encode()) if raw is None else raw
        return TRANSPORT.WireResponse(status, headers or [["Content-Type", "application/json; charset=utf-8"], ["Content-Length", str(len(raw))]],
            raw, complete, STAMP, None)

    def send(self, request, headers, payload, **budget):
        self.requests.append({"label": request.label, "method": request.method, "route": request.route, "headers": deepcopy(headers), "payload": payload})
        if request.label in self.overrides:
            value = self.overrides[request.label]
            if isinstance(value, BaseException): raise value
            return value(self, request) if callable(value) else value
        if request.label == "login":
            return self.response(200, {"ServerId": "owned-server", "AccessToken": self.token,
                "User": {"Id": "owned-P4", "Name": "Owned P4", "Policy": {"IsAdministrator": False, "IsDisabled": False,
                    "EnableAllFolders": False, "EnabledFolders": ["a" * 32, "b" * 32]}},
                "SessionInfo": {"Id": self.session, "UserId": "owned-P4", "DeviceId": "new-recovery-device"}})
        if request.label in ("before", "after"):
            return self.response(200, self.authority.body(self.authority.zero if self.restored else self.authority.partial))
        if request.label == "delete":
            self.restored = True
            return self.response(200, {"SyntheticActualDeleteBody": ["retained verbatim", 9]})
        if request.label == "logout": return self.response(204)
        if request.label == "rejection": return self.response(401, raw=b"Access token is invalid or expired.", headers=[["Content-Type", "text/plain"], ["Content-Length", "35"]])
        raise AssertionError("No other synthetic recovery operation is permitted.")


class Guards(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="owned-userdata-recovery-guard-")
        self.addCleanup(temporary.cleanup)
        self.authority = FakeAuthority(Path(temporary.name))
        self.wire = FakeWire(self.authority)
        self.runner = SOURCE.Runner(self.authority, wire=self.wire, monotonic=lambda: 0.0, now=lambda: STAMP)
        for target, name in ((socket, "socket"), (socket, "create_connection"), (SOURCE.subprocess, "run"), (TRANSPORT, "process_identity")):
            blocker = patch.object(target, name, side_effect=AssertionError("Synthetic guards cannot contact a service or probe a process."))
            blocker.start(); self.addCleanup(blocker.stop)

    def labels(self): return [row["label"] for row in self.wire.requests]

    def state(self): return SOURCE.strict_json((Path(self.authority.value["outputRoot"]) / "private/state.json").read_bytes())

    def test_success_is_exactly_six_requests_and_one_userdata_delete(self):
        terminal = self.runner.run()
        self.assertEqual(self.labels(), ["login", "before", "delete", "after", "logout", "rejection"])
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual((terminal["normalRequestCount"], terminal["cleanupRequestCount"]), (4, 2))
        deletion = self.wire.requests[2]
        self.assertEqual((deletion["method"], deletion["route"], deletion["payload"]), ("DELETE", "/emby/Users/owned-P4/PlayedItems/125", None))
        self.assertEqual(self.wire.requests[-2]["headers"]["X-Emby-Token"], self.wire.requests[-1]["headers"]["X-Emby-Token"])
        self.assertTrue(terminal["zeroRestored"] and terminal["allKnownTokensClosed"])

    def test_exact_zero_uses_four_requests_without_any_delete_or_second_read(self):
        self.wire.restored = True
        terminal = self.runner.run()
        self.assertEqual(self.labels(), ["login", "before", "logout", "rejection"])
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertTrue(terminal["alreadyRestored"])
        self.assertFalse(terminal["deleteAttempted"])

    def test_other_complete_userdata_never_authorizes_delete_but_closes_token(self):
        body = self.authority.body(self.authority.partial)
        body["UserData"]["PlayedPercentage"] = 21
        self.wire.overrides["before"] = self.wire.response(200, body)
        terminal = self.runner.run()
        self.assertEqual(self.labels(), ["login", "before", "logout", "rejection"])
        self.assertFalse(terminal["zeroRestored"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_zero_with_an_extra_percentage_field_is_not_the_retained_zero(self):
        body = self.authority.body(self.authority.zero)
        body["UserData"]["PlayedPercentage"] = 0
        self.wire.overrides["before"] = self.wire.response(200, body)
        self.runner.run()
        self.assertNotIn("delete", self.labels())

    def test_boolean_partial_primitive_cannot_match_an_integer(self):
        body = self.authority.body(self.authority.partial)
        body["UserData"]["PlayCount"] = True
        self.wire.overrides["before"] = self.wire.response(200, body)
        self.runner.run()
        self.assertNotIn("delete", self.labels())

    def test_missing_partial_field_cannot_authorize_delete(self):
        body = self.authority.body(self.authority.partial)
        del body["UserData"]["LastPlayedDate"]
        self.wire.overrides["before"] = self.wire.response(200, body)
        self.runner.run()
        self.assertNotIn("delete", self.labels())

    def test_wrong_path_or_season_cannot_authorize_delete(self):
        body = self.authority.body(self.authority.partial)
        body["SeasonId"] = "another-season"
        self.wire.overrides["before"] = self.wire.response(200, body)
        terminal = self.runner.run()
        self.assertNotIn("delete", self.labels())
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_complete_unacknowledged_delete_keeps_pending_and_stops_all_http(self):
        self.wire.overrides["delete"] = self.wire.response(500, {"Error": "Synthetic known failure"})
        terminal = self.runner.run()
        self.assertEqual(self.labels(), ["login", "before", "delete"])
        self.assertFalse(terminal["zeroRestored"] or terminal["deleteAcknowledged"])
        self.assertFalse(terminal["allKnownTokensClosed"])
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(self.state()["pending"]["label"], "delete")

    def test_nonzero_after_delete_does_not_retry_or_expand_mutations(self):
        self.wire.overrides["after"] = self.wire.response(200, self.authority.body(self.authority.partial))
        terminal = self.runner.run()
        self.assertEqual(self.labels().count("delete"), 1)
        self.assertFalse(terminal["zeroRestored"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_missing_vs_null_after_delete_is_preserved(self):
        body = self.authority.body(self.authority.zero)
        body["UserData"]["LastPlayedDate"] = None
        self.wire.overrides["after"] = self.wire.response(200, body)
        terminal = self.runner.run()
        self.assertFalse(terminal["zeroRestored"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_lost_delete_response_retains_pending_and_sends_no_cleanup(self):
        self.wire.overrides["delete"] = TimeoutError("Synthetic lost response")
        terminal = self.runner.run()
        self.assertEqual(self.labels(), ["login", "before", "delete"])
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(self.state()["pending"]["label"], "delete")

    def test_bad_json_success_read_stops_without_blind_cleanup(self):
        self.wire.overrides["before"] = self.wire.response(200, raw=b'{"UserData":{} ,"UserData":{}}')
        self.runner.run()
        self.assertEqual(self.labels(), ["login", "before"])
        self.assertEqual(self.state()["pending"]["label"], "before")

    def test_incomplete_response_keeps_actual_raw_and_pending(self):
        self.wire.overrides["before"] = self.wire.response(200, self.authority.body(self.authority.partial), complete=False)
        self.runner.run()
        self.assertEqual(self.labels(), ["login", "before"])
        self.assertIn("responseReceiptSha256", self.state()["pending"])

    def test_unknown_login_owner_never_registers_or_closes_a_guessed_token(self):
        self.wire.overrides["login"] = self.wire.response(200, {"AccessToken": "unknown-token", "ServerId": "owned-server", "User": {}, "SessionInfo": {}})
        self.runner.run()
        self.assertIsNone(self.runner.token)
        self.assertEqual(self.labels(), ["login"])

    def test_old_token_reuse_cannot_be_called_a_new_recovery_login(self):
        self.wire.token = self.authority.parent_state["tokens"]["P"]
        self.runner.run()
        self.assertIsNone(self.runner.token)
        self.assertEqual(self.labels(), ["login"])

    def test_closure_401_failure_does_not_retry_logout(self):
        self.wire.overrides["rejection"] = self.wire.response(200, [])
        terminal = self.runner.run()
        self.assertEqual(self.labels().count("logout"), 1)
        self.assertFalse(terminal["allKnownTokensClosed"])
        self.assertEqual(terminal["status"], "recovery_required")

    def test_pending_commit_failure_sends_no_http(self):
        original = TRANSPORT.Journal.state
        def state(journal, value):
            if value["pending"] is not None: raise OSError("Synthetic pending commit failure")
            return original(journal, value)
        with patch.object(TRANSPORT.Journal, "state", state): self.assertIsNone(self.runner.run())
        self.assertEqual(self.labels(), [])

    def test_login_owner_commit_failure_freezes_before_followup(self):
        original = TRANSPORT.Journal.state
        def state(journal, value):
            if value["token"] is not None: raise OSError("Synthetic owner commit failure")
            return original(journal, value)
        with patch.object(TRANSPORT.Journal, "state", state): self.assertIsNone(self.runner.run())
        self.assertEqual(self.labels(), ["login"])
        self.assertEqual(self.state()["pending"]["label"], "login")

    def test_login_pending_clear_commit_failure_freezes_before_followup(self):
        original = TRANSPORT.Journal.state
        def state(journal, value):
            if value["token"] is not None and value["pending"] is None: raise OSError("Synthetic owner release failure")
            return original(journal, value)
        with patch.object(TRANSPORT.Journal, "state", state): self.assertIsNone(self.runner.run())
        self.assertEqual(self.labels(), ["login"])
        self.assertEqual(self.state()["pending"]["label"], "login")

    def test_partial_gate_commit_failure_prevents_delete(self):
        original = TRANSPORT.Journal.state
        def state(journal, value):
            if value["exactPartialObserved"]: raise OSError("Synthetic matched-state commit failure")
            return original(journal, value)
        with patch.object(TRANSPORT.Journal, "state", state): self.assertIsNone(self.runner.run())
        self.assertEqual(self.labels(), ["login", "before"])

    def test_delete_acknowledgement_commit_failure_stops_before_any_followup(self):
        original = TRANSPORT.Journal.state
        def state(journal, value):
            if value["deleteAcknowledged"]: raise OSError("Synthetic DELETE acknowledgement commit failure")
            return original(journal, value)
        with patch.object(TRANSPORT.Journal, "state", state): self.assertIsNone(self.runner.run())
        self.assertEqual(self.labels(), ["login", "before", "delete"])
        self.assertEqual(self.state()["pending"]["label"], "delete")

    def test_raw_delete_receipt_failure_never_closes_or_retries(self):
        original = TRANSPORT.Journal.save
        def save(journal, name, value, *, export=False):
            if name == "0003-delete-response.json" and not export: raise OSError("Synthetic raw receipt failure")
            return original(journal, name, value, export=export)
        with patch.object(TRANSPORT.Journal, "save", save): self.assertIsNone(self.runner.run())
        self.assertEqual(self.labels(), ["login", "before", "delete"])
        self.assertEqual(self.state()["pending"]["label"], "delete")

    def test_exclusive_output_and_same_runner_cannot_resume(self):
        self.runner.run()
        with self.assertRaises(ValueError): self.runner.run()
        other = SOURCE.Runner(self.authority, wire=self.wire, monotonic=lambda: 0.0, now=lambda: STAMP)
        with self.assertRaises(FileExistsError): other.run()

    def test_exports_have_no_raw_new_token_password_or_session(self):
        self.runner.run()
        for path in (Path(self.authority.value["outputRoot"]) / "export").iterdir():
            raw = path.read_text()
            for secret in (self.runner.token, self.runner.session, self.authority.credential["password"]): self.assertNotIn(secret, raw)


def main():
    global SOURCE, TRANSPORT
    if not (sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode):
        raise SystemExit("Only authorized remote Python -I -B verification is supported.")
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    for key in ("source", "transport", "report"): parser.add_argument("--" + key, type=Path, required=True)
    args = parser.parse_args()
    SOURCE, TRANSPORT = load(args.source, "owned_recovery_subject"), load(args.transport, "owned_recovery_transport")
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Guards))
    report = {"kind": "owned-preparation04-userdata-recovery-guards", "passed": result.wasSuccessful(), "testsRun": result.testsRun,
        "failures": len(result.failures), "errors": len(result.errors), "skips": len(result.skipped),
        "actualBusinessHttpRequests": 0, "actualProcessProbes": 0,
        "sourceSha256": hashlib.sha256(args.source.read_bytes()).hexdigest(), "transportSha256": hashlib.sha256(args.transport.read_bytes()).hexdigest(),
        "guardSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    with args.report.open("x", encoding="utf-8") as handle:
        json.dump(report, handle, sort_keys=True, indent=2); handle.write("\n")
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
