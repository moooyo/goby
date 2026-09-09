#!/usr/bin/env python3
"""Run synthetic recorder hardening regressions through SSH without live HTTP.

The recorder is imported without initialization. Tests use in-memory outputs,
fake HTTP responses, and blocked network/process entry points. No reference
service, credential file, capture directory, or historical evidence is opened.
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
    print(json.dumps({"suite": "reference-api-key-hardening", "result": "blocked", "reason": "Authorized root SSH and one recorder source path are required"}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SOURCE_HASH = hashlib.sha256(SOURCE.read_bytes()).hexdigest()
TEST_HASH = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
SPEC = importlib.util.spec_from_file_location("reference_api_keys_under_test", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class FakeResponse:
    status = 200

    def __init__(self, headers: list | tuple, body: object) -> None:
        self.headers, self.body = headers, json.dumps(body).encode()

    def read(self, limit: int) -> bytes:
        return self.body[:limit]

    def getheaders(self) -> list | tuple:
        return self.headers


class FakeConnection:
    def __init__(self, response: FakeResponse) -> None:
        self.response, self.calls, self.closed = response, [], False

    def request(self, *arguments: object) -> None:
        self.calls.append(arguments)

    def getresponse(self) -> FakeResponse:
        return self.response

    def close(self) -> None:
        self.closed = True


class HardeningTests(unittest.TestCase):
    def recorder(self) -> object:
        recorder = MODULE.Recorder.__new__(MODULE.Recorder)
        recorder.secrets, recorder.owned, recorder.apps = set(), {}, set()
        recorder.logins = {"admin": {"AccessToken": "synthetic-admin-credential"},
                           "viewer": {"AccessToken": "synthetic-viewer-credential"}}
        recorder.logged_out, recorder.logout_statuses = set(), {}
        recorder.old_keys, recorder.cleanup_count, recorder.total = [], 0, 0
        return recorder

    def test_tuple_headers_register_and_redact_credentials(self) -> None:
        recorder = self.recorder()
        headers = [("Set-Cookie", "session=synthetic-cookie; HttpOnly"),
                   ("X-Future-Token", "synthetic-future-token"), ("Content-Type", "application/json")]
        record = {"response": {"headers": headers}, "echo": "synthetic-future-token"}
        recorder.collect_secrets(record)
        self.assertEqual(recorder.secrets, {headers[0][1], headers[1][1]})
        self.assertEqual(recorder.sanitize(record), {"response": {"headers": [
            ["Set-Cookie", "[REDACTED_SECRET]"], ["X-Future-Token", "[REDACTED_SECRET]"],
            ["Content-Type", "application/json"]]}, "echo": "[REDACTED_SECRET]"})

    def test_serialized_header_pairs_preserve_duplicates_and_order(self) -> None:
        recorder = self.recorder()
        record = {"headers": [["sEt-CoOkIe", "first=one"], ["sEt-CoOkIe", "second=two"],
                              ["X-Api-Key", "synthetic-api-key"], ["X-Trace-Id", "stable-trace"]]}
        recorder.collect_secrets(record)
        self.assertEqual(len(recorder.secrets), 3)
        self.assertEqual(recorder.sanitize(record)["headers"], [
            ["sEt-CoOkIe", "[REDACTED_SECRET]"], ["sEt-CoOkIe", "[REDACTED_SECRET]"],
            ["X-Api-Key", "[REDACTED_SECRET]"], ["X-Trace-Id", "stable-trace"]])

    def test_dictionary_headers_redact_authorization_cookie_and_token(self) -> None:
        recorder = self.recorder()
        record = {"headers": {"Authorization": "Bearer synthetic-auth", "Cookie": "session=synthetic-session",
                              "X-MediaBrowser-Token": "synthetic-media-token", "Accept": "application/json"}}
        recorder.collect_secrets(record)
        self.assertEqual(len(recorder.secrets), 3)
        self.assertEqual(recorder.sanitize(record)["headers"], {"Authorization": "[REDACTED_SECRET]",
            "Cookie": "[REDACTED_SECRET]", "X-MediaBrowser-Token": "[REDACTED_SECRET]", "Accept": "application/json"})

    def test_nested_tuples_collect_secret_fields_and_become_json_arrays(self) -> None:
        recorder = self.recorder()
        record = {"entries": ({"AccessToken": "synthetic-nested-token", "Id": 18, "IsActive": True},),
                  "other": "synthetic-nested-token"}
        recorder.collect_secrets(record)
        self.assertEqual(recorder.sanitize(record), {"entries": [{"AccessToken": "[REDACTED_SECRET]", "Id": 18,
            "IsActive": True}], "other": "[REDACTED_SECRET]"})

    def test_empty_header_values_and_non_sensitive_values_are_preserved(self) -> None:
        recorder = self.recorder()
        record = {"headers": (("Set-Cookie", ""), ("Content-Length", "0"), ("Access-Control-Allow-Headers", "X-Emby-Token"))}
        recorder.collect_secrets(record)
        self.assertEqual(recorder.secrets, set())
        self.assertEqual(recorder.sanitize(record)["headers"], [list(pair) for pair in record["headers"]])

    def test_malformed_headers_fail_closed_before_export(self) -> None:
        for headers in ([('Set-Cookie',)], [("X-Emby-Token", 42)], "unexpected", {"Set-Cookie": None}):
            with self.subTest(headers_type=type(headers).__name__):
                recorder = self.recorder()
                with self.assertRaises(RuntimeError):
                    recorder.collect_secrets({"headers": headers})
                with self.assertRaises(RuntimeError):
                    recorder.sanitize({"headers": headers})

    def test_key_paths_and_query_credentials_remain_redacted(self) -> None:
        recorder = self.recorder()
        self.assertEqual(recorder.sanitize("/emby/Auth/Keys/unknown-synthetic-key/Delete?api_key=unknown-query-value"),
                         "/emby/Auth/Keys/[REDACTED_KEY]/Delete?api_key=[REDACTED_TOKEN]")

    def test_fake_http_exports_unknown_response_tokens_without_network(self) -> None:
        recorder = self.recorder()
        connection = FakeConnection(FakeResponse([("Set-Cookie", "session=synthetic-http-cookie; HttpOnly"),
            ("X-New-Access-Token", "synthetic-http-token"), ("Content-Type", "application/json")],
            {"Echo": "synthetic-http-token", "Count": 2}))
        saved = {}
        with patch.object(MODULE.http.client, "HTTPConnection", return_value=connection), \
                patch.object(MODULE, "save", side_effect=lambda path, value: saved.update({str(path): value})), \
                contextlib.redirect_stdout(io.StringIO()):
            status_code, result = recorder.request("synthetic-http", "GET", "/emby/Auth/Keys")
        self.assertEqual(status_code, 200)
        self.assertEqual(result["Count"], 2)
        self.assertTrue(connection.closed)
        self.assertEqual(len(connection.calls), 1)
        exported = next(value for path, value in saved.items() if "/export/" in path)
        self.assertNotIn("synthetic-http-token", exported)
        self.assertNotIn("synthetic-http-cookie", exported)
        self.assertEqual(json.loads(exported)["response"]["body"]["Echo"], "[REDACTED_SECRET]")

    def test_logout_success_counts_only_confirmed_responses_once(self) -> None:
        recorder = self.recorder()
        recorder.request = Mock(side_effect=[(204, ""), (200, "")])
        recorder.logout_owned_logins()
        recorder.logout_owned_logins()
        self.assertEqual(recorder.logged_out, {"admin", "viewer"})
        self.assertEqual(recorder.logout_statuses, {"viewer": 204, "admin": 200})
        self.assertEqual(recorder.request.call_count, 2)

    def test_failed_viewer_logout_still_attempts_admin_and_blocks_audit(self) -> None:
        recorder = self.recorder()
        recorder.request = Mock(side_effect=[(500, "failure"), (204, "")])
        recorder.key_list, recorder.write = Mock(return_value=[]), Mock()
        with self.assertRaises(RuntimeError):
            recorder.finish()
        self.assertEqual(recorder.logged_out, {"admin"})
        self.assertEqual(recorder.logout_statuses, {"viewer": 500, "admin": 204})
        self.assertEqual(recorder.request.call_count, 2)
        recorder.write.assert_not_called()

    def test_logout_transport_failure_is_not_counted_and_cleanup_continues(self) -> None:
        recorder = self.recorder()
        recorder.request = Mock(side_effect=[TimeoutError("synthetic timeout"), (204, "")])
        with self.assertRaises(RuntimeError):
            recorder.logout_owned_logins()
        self.assertEqual(recorder.logged_out, {"admin"})
        self.assertEqual(recorder.logout_statuses, {"viewer": None, "admin": 204})
        self.assertEqual(recorder.request.call_count, 2)

    def test_admin_failure_does_not_inflate_success_count(self) -> None:
        recorder = self.recorder()
        recorder.request = Mock(side_effect=[(204, ""), (401, "denied")])
        with self.assertRaises(RuntimeError):
            recorder.logout_owned_logins()
        self.assertEqual(recorder.logged_out, {"viewer"})
        self.assertEqual(recorder.logout_statuses, {"viewer": 204, "admin": 401})

    def test_early_cleanup_checks_logout_before_returning(self) -> None:
        recorder = self.recorder()
        recorder.old_keys = None
        recorder.request = Mock(side_effect=[(204, ""), (403, "denied")])
        with self.assertRaises(RuntimeError):
            recorder.finish()
        self.assertEqual(len(recorder.logged_out), 1)
        self.assertEqual(recorder.request.call_count, 2)


def main() -> None:
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(HardeningTests)
    stream = io.StringIO()
    with patch.object(socket, "socket", side_effect=RuntimeError("Synthetic tests forbid network access")), \
            patch.object(http.client, "HTTPConnection", side_effect=RuntimeError("Live HTTP is forbidden")), \
            patch.object(subprocess, "check_output", side_effect=RuntimeError("Synthetic tests forbid process probes")), \
            patch.object(os, "execvp", side_effect=RuntimeError("Synthetic tests forbid namespace entry")):
        result = unittest.TextTestRunner(stream=stream).run(suite)
    print(json.dumps({"suite": "reference-api-key-hardening", "result": "passed" if result.wasSuccessful() else "failed",
        "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
        "failedTests": [test.id() for test, _ in result.failures + result.errors],
        "recorderSha256": SOURCE_HASH, "testSha256": TEST_HASH, "liveHTTPRequests": 0}, sort_keys=True))
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
