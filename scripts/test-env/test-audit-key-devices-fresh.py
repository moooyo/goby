#!/usr/bin/env python3
"""Test the offline recovery's redaction regression and raw credential proofs.

Run only through authorized root SSH. Tests use synthetic in-memory records;
they never read a capture, write evidence, start a process, or issue HTTP.
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
from unittest.mock import patch

sys.dont_write_bytecode = True
if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
    print(json.dumps({"result": "blocked", "reason": "Authorized root SSH and one offline analyzer source path are required"}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SOURCE_HASH = hashlib.sha256(SOURCE.read_bytes()).hexdigest()
TEST_HASH = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()


def module_from(path: Path, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


MODULE = module_from(SOURCE, "offline_key_device_audit_under_test")
LEGACY = module_from(SOURCE.with_name("reference-api-keys.py"), "legacy_key_sanitizer_under_test")


class OfflineAuditTests(unittest.TestCase):
    def test_generic_logical_key_labels_reproduce_the_dictionary_key_false_positive(self) -> None:
        result = MODULE.legacy_label_diagnostic(LEGACY)
        self.assertTrue(result["syntheticReproductionPassed"])
        self.assertEqual([row["stringLength"] for row in result["matches"]], [5, 7])
        self.assertTrue(all(row["matchKind"] == "dictionary-key" for row in result["matches"]))
        self.assertNotIn("alpha", json.dumps(result))
        self.assertNotIn("sibling", json.dumps(result))

    def test_only_the_new_audit_sanitizes_dictionary_keys_and_refuses_collisions(self) -> None:
        sanitizer = LEGACY.Recorder.__new__(LEGACY.Recorder)
        sanitizer.secrets = {"synthetic-sensitive-token"}
        original = {"synthetic-sensitive-token-field": {"credentialLabel": "safe", "message": "synthetic-sensitive-token"}}
        preserved = copy.deepcopy(original)
        result = MODULE.sanitize_new_audit(original, sanitizer)
        self.assertEqual(result, {"[REDACTED_SECRET]-field": {"credentialLabel": "safe", "message": "[REDACTED_SECRET]"}})
        self.assertEqual(original, preserved)
        with self.assertRaises(RuntimeError):
            MODULE.sanitize_new_audit({"synthetic-sensitive-token": 1, "[REDACTED_SECRET]": 2}, sanitizer)

    def test_bootstrap_revocation_requires_distinct_raw_tokens_latest_401_and_no_reuse(self) -> None:
        def login(token: str, admin: bool) -> dict:
            return {"request": {"method": "POST", "path": "/emby/Users/AuthenticateByName", "headers": {}},
                    "response": {"status": 200, "body": {"AccessToken": token, "User": {"Policy": {"IsAdministrator": admin}}}},
                    "observation": {"completeHTTP": True, "captureIncomplete": False}}
        def probe(token: str, status: int) -> dict:
            return {"request": {"method": "GET", "path": "/emby/Sessions", "headers": {"X-Emby-Token": token}},
                    "response": {"status": status, "body": ""}, "observation": {"completeHTTP": True, "captureIncomplete": False}}
        bootstrap = [login("synthetic-admin-token", True), login("synthetic-viewer-token", False),
                     probe("synthetic-admin-token", 401), probe("synthetic-viewer-token", 401)]
        study = {"new-login-probe": probe("synthetic-new-token", 401)}
        with patch.object(MODULE, "SETUP_COUNT", 4):
            self.assertTrue(MODULE.verify_bootstrap_credentials(bootstrap, study)["independentRawTokenMappingVerified"])
            with self.assertRaises(RuntimeError):
                MODULE.verify_bootstrap_credentials(bootstrap, {"reused-bootstrap": probe("synthetic-admin-token", 401)})
            changed = copy.deepcopy(bootstrap)
            changed[-1]["response"]["status"] = 200
            with self.assertRaises(RuntimeError):
                MODULE.verify_bootstrap_credentials(changed, study)
            duplicated = copy.deepcopy(bootstrap)
            duplicated[1]["response"]["body"]["AccessToken"] = "synthetic-admin-token"
            with self.assertRaises(RuntimeError):
                MODULE.verify_bootstrap_credentials(duplicated, study)

    def test_only_the_initial_zero_byte_readiness_refusal_preceding_success_is_classified_outside_http(self) -> None:
        request = {"method": "GET", "path": "/emby/System/Info/Public", "headers": [["Accept", "application/json"]], "body": None}
        failure = {"reference": {"capturedAt": "2026-09-09T23:50:17+00:00"}, "request": copy.deepcopy(request),
                   "response": {"status": None, "headers": [], "bodyType": "incomplete", "body": None},
                   "observation": {"completeHTTP": False, "captureIncomplete": True, "failureType": "ConnectionRefusedError",
                                   "wireBytes": 0, "freshFixtureOnly": True}}
        ready = {"reference": {"capturedAt": "2026-09-09T23:50:18+00:00"}, "request": copy.deepcopy(request),
                 "response": {"status": 200, "body": {"Id": MODULE.SERVER_ID, "Version": "4.9.5.0"}},
                 "observation": {"completeHTTP": True, "captureIncomplete": False, "failureType": None}}
        bootstrap = [failure, ready]
        for label, admin in (("admin", True), ("viewer", False)):
            bootstrap.append({"request": {"method": "POST", "path": "/emby/Users/AuthenticateByName", "headers": {}},
                "response": {"status": 200, "body": {"AccessToken": "synthetic-" + label, "User": {"Policy": {"IsAdministrator": admin}}}},
                "observation": {"completeHTTP": True, "captureIncomplete": False}})
        for label in ("admin", "viewer"):
            bootstrap.append({"request": {"method": "GET", "path": "/emby/Sessions", "headers": {"X-Emby-Token": "synthetic-" + label}},
                "response": {"status": 401, "body": ""}, "observation": {"completeHTTP": True, "captureIncomplete": False}})
        with patch.object(MODULE, "SETUP_COUNT", 6):
            result = MODULE.verify_bootstrap_credentials(bootstrap, {})
            self.assertEqual(result["completeHTTPRecords"], 5)
            self.assertEqual(result["setupRecords"], 6)
            self.assertEqual(len(result["nonHTTPReadinessObservations"]), 1)
            self.assertFalse(result["nonHTTPReadinessObservations"][0]["completeHTTP"])
            for condition in ("wrong-path", "different-failure", "response-bytes", "no-next-success", "incomplete-auth", "incomplete-proof", "contradictory-ready"):
                changed = copy.deepcopy(bootstrap)
                if condition == "wrong-path":
                    changed[0]["request"]["path"] = "/emby/Users/AuthenticateByName"
                elif condition == "different-failure":
                    changed[0]["observation"]["failureType"] = "TimeoutError"
                elif condition == "response-bytes":
                    changed[0]["observation"]["wireBytes"] = 1
                elif condition == "no-next-success":
                    changed[1]["response"]["status"] = 503
                elif condition in {"incomplete-auth", "incomplete-proof"}:
                    changed[2 if condition == "incomplete-auth" else -1]["observation"]["completeHTTP"] = False
                else:
                    changed[1]["observation"]["captureIncomplete"] = True
                with self.subTest(condition=condition), self.assertRaises(RuntimeError):
                    MODULE.verify_bootstrap_credentials(changed, {})

    def test_duplicate_credential_carriers_do_not_become_an_ownership_proof(self) -> None:
        record = {"request": {"headers": [["X-Emby-Token", "synthetic-first"], ["x-emby-token", "synthetic-second"]]}}
        with self.assertRaises(RuntimeError):
            MODULE.credential(record)


def main() -> None:
    output = io.StringIO()
    forbidden = RuntimeError("Offline audit tests forbid external side effects")
    with contextlib.ExitStack() as guards:
        for owner, name in ((socket, "socket"), (socket, "create_connection"), (http.client.HTTPConnection, "__init__"),
                            (subprocess, "Popen"), (subprocess, "run"), (subprocess, "check_output"), (os, "open"), (os, "execvp"),
                            (Path, "read_text"), (Path, "read_bytes"), (Path, "mkdir"), (Path, "write_text"), (Path, "write_bytes")):
            guards.enter_context(patch.object(owner, name, side_effect=forbidden))
        result = unittest.TextTestRunner(stream=output).run(unittest.defaultTestLoader.loadTestsFromTestCase(OfflineAuditTests))
    print(json.dumps({"suite": "offline-key-device-recovery", "result": "passed" if result.wasSuccessful() else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "analyzerSha256": SOURCE_HASH, "testSha256": TEST_HASH, "httpRequests": 0, "captureWrites": 0}))
    if not result.wasSuccessful():
        print(output.getvalue(), file=sys.stderr)
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
