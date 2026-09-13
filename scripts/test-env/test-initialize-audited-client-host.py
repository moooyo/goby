#!/usr/bin/env python3
"""Pure startup guards with no filesystem mutation, HTTP, SQL, or services."""
import copy
import importlib.util
import json
from pathlib import Path
import types
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("host_startup", Path(__file__).with_name("initialize-audited-client-host.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def fixture():
    return {"kind": "audited-original-client-host-startup-input", "version": 1, "output": str(m.OUTPUT),
        "runtimeEpoch": copy.deepcopy(m.EPOCH), "seedBinding": copy.deepcopy(m.BINDING), "runtimeHelper": copy.deepcopy(m.RUNTIME), "hosting": copy.deepcopy(m.HOSTING),
        "gateway": {"path": str(m.R / "sources/client-acceptance-gateway.py"), "sha256": m.SOURCE_SHA["gateway"]},
        "proxy": {"path": str(m.R / "sources/client-acceptance-proxy.py"), "sha256": m.SOURCE_SHA["proxy"]},
        "compiledCatalog": {"path": str(m.R / "catalog.json"), "sha256": "a" * 64}, "budgets": copy.deepcopy(m.BUDGETS)}


def job():
    value = m.Startup(types.SimpleNamespace(), fixture(), {"path": str(m.R / "input.json"), "sha256": "b" * 64},
                      {"path": str(m.R / "initialize-audited-client-host.py"), "sha256": "c" * 64})
    value.s = types.SimpleNamespace(parse=json.loads, encoded=lambda data: json.dumps(data).encode(),
        write_once=Mock(return_value={"path": "private", "sha256": "d" * 64}), response_complete=lambda response, method, raw, limit: response.length == 0 and len(raw) <= limit)
    value.save, value.pin_host = Mock(return_value={"path": "receipt", "sha256": "d" * 64}), Mock()
    value.endpoint = types.SimpleNamespace(connect=Mock(return_value=Mock()), close=Mock())
    value.password = "private-password-01234567890123456789"
    return value


class BytesSocket:
    def __init__(self, raw):
        self.raw, self.offset = raw, 0

    def recv(self, amount):
        result = self.raw[self.offset:self.offset + amount]
        self.offset += len(result)
        return result

    def settimeout(self, unused):
        pass


class Response:
    def __init__(self, raw, status=200, remaining=0):
        self.raw, self.status, self.length, self.fp = raw, status, remaining, None

    def read1(self, unused):
        result, self.raw = self.raw, b""
        return result

    def getheaders(self):
        return [("Content-Type", "application/json")]

    def close(self):
        pass


class Guards(unittest.TestCase):
    def test_fixed_scope_authority_and_budget(self):
        self.assertEqual(m.validate_input(fixture()), fixture())
        for key in ("runtimeEpoch", "seedBinding", "runtimeHelper", "hosting", "gateway", "proxy"):
            changed = fixture()
            changed[key]["sha256"] = "0" * 64
            with self.subTest(key=key), self.assertRaises(m.StartupError):
                m.validate_input(changed)
        changed = fixture()
        changed["budgets"]["normalRequests"] += 1
        with self.assertRaises(m.StartupError):
            m.validate_input(changed)

    def test_collision_precedes_reads_and_mutations(self):
        value = job()
        value.r.read_bootstrap = Mock()
        with patch.object(m.os.path, "lexists", return_value=True), patch.object(m.os, "mkdir") as mkdir, self.assertRaisesRegex(m.StartupError, "startup_output_collision"):
            value.open()
        mkdir.assert_not_called()
        value.r.read_bootstrap.assert_not_called()
        value.endpoint.connect.assert_not_called()

    def test_headers_only_does_not_consume_html(self):
        prefix = b"HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: 8\r\n\r\n"
        sock = BytesSocket(prefix + b"<secret>")
        status, headers, raw = m.headers_only(sock, lambda: 5)
        self.assertEqual(status, 200)
        self.assertIn(("Content-Type", "text/html"), headers)
        self.assertEqual(raw, prefix)
        self.assertEqual(sock.offset, len(prefix))
        with self.assertRaisesRegex(m.StartupError, "web_headers_truncated"):
            m.headers_only(BytesSocket(b"HTTP/1.1 200 OK\r\n"), lambda: 5)

    def test_configuration_uses_exact_known_fields(self):
        valid = {**copy.deepcopy(m.NETWORK), "IsStartupWizardCompleted": True}
        self.assertEqual(m.validate_configuration(valid), m.NETWORK)
        self.assertNotIn("EnableAutomaticPortMapping", valid)
        for key, changed in (("EnableRemoteAccess", True), ("HttpServerPortNumber", 18196), ("LocalNetworkAddresses", ["0.0.0.0"]), ("IsStartupWizardCompleted", False)):
            with self.subTest(key=key), self.assertRaisesRegex(m.StartupError, "hosting_network_configuration_changed"):
                m.validate_configuration({**valid, key: changed})

    def test_preservation_compares_all_rows_and_runtime(self):
        sample = {"source": {"tables": {str(i): [] for i in range(35)}, "sequences": {"seq": {"lastValue": "1"}}},
            "candidate": {"pid": 1}, "postgres": {"pid": 2}, "lease": {"backendPid": 3}, "hosting": {"pid": 4}}
        self.assertEqual(m.compare_samples(sample, copy.deepcopy(sample))["ownedTablesExact"], 35)
        for key in ("candidate", "postgres", "lease", "hosting"):
            after = copy.deepcopy(sample)
            after[key]["changed"] = True
            with self.subTest(key=key), self.assertRaises(m.StartupError):
                m.compare_samples(sample, after)
        after = copy.deepcopy(sample)
        after["source"]["tables"]["34"] = [{"id": "added"}]
        with self.assertRaises(m.StartupError):
            m.compare_samples(sample, after)

    def test_writes_require_public_identity_and_each_label_is_once(self):
        value = job()
        with self.assertRaisesRegex(m.StartupError, "startup_public_identity_required"):
            value.request("startup-complete")
        value.endpoint.connect.assert_not_called()
        value.public_attested = True
        value.states = [{"label": "startup-complete"}]
        with self.assertRaisesRegex(m.StartupError, "startup_action_repeated_or_unknown"):
            value.request("startup-complete")
        value.save.assert_not_called()

    def test_complete_has_no_body_and_never_reconnects(self):
        value = job()
        value.public_attested = True
        connection = Mock()
        connection.getresponse.return_value = Response(b"", status=204)
        with patch.object(m.http.client, "HTTPConnection", return_value=connection):
            value.request("startup-complete")
        self.assertEqual(connection.auto_open, 0)
        args = connection.request.call_args
        self.assertEqual(args.args, ("POST", "/emby/Startup/Complete"))
        self.assertIsNone(args.kwargs["body"])
        self.assertNotIn("Content-Type", args.kwargs["headers"])
        value.endpoint.connect.assert_called_once()

    def test_partial_login_token_is_owned_before_completion_assertion(self):
        value = job()
        value.public_attested = True
        token = "0123456789abcdef0123456789abcdef"
        connection = Mock()
        connection.getresponse.return_value = Response(json.dumps({"AccessToken": token}).encode(), remaining=7)
        with patch.object(m.http.client, "HTTPConnection", return_value=connection), self.assertRaisesRegex(m.StartupError, "startup_response_incomplete"):
            value.request("login", {"Username": m.ADMIN_NAME, "Pw": value.password})
        self.assertEqual(value.token, token)
        self.assertEqual(value.requests, {"normal": 1, "cleanup": 0})

    def test_cleanup_attempts_same_token_check_even_when_logout_unknown(self):
        value = job()
        value.token = "0123456789abcdef0123456789abcdef"
        value.request = Mock(side_effect=[m.StartupError("transport_unknown"), (None, {"status": 401}, {"path": "check", "sha256": "e" * 64})])
        value.cleanup_token()
        self.assertEqual([call.args[0] for call in value.request.call_args_list], ["logout", "logout-check"])
        self.assertTrue(all(call.kwargs["token"] == value.token for call in value.request.call_args_list))
        self.assertIsNone(value.cleanup[0]["logoutStatus"])
        self.assertEqual(value.cleanup[0]["sameTokenStatus"], 401)
        self.assertEqual(len(value.cleanup_failures), 1)

    def test_complete_plain_text_401_is_valid_cleanup_evidence(self):
        value = job()
        value.token = "0123456789abcdef0123456789abcdef"
        connection = Mock()
        response = Response(b"Access token is invalid.", status=401)
        response.getheaders = lambda: [("Content-Type", "text/plain")]
        connection.getresponse.return_value = response
        with patch.object(m.http.client, "HTTPConnection", return_value=connection):
            body, observed, unused_pin = value.request("logout-check", token=value.token)
        self.assertIsNone(body)
        self.assertEqual(observed["status"], 401)
        self.assertTrue(observed["complete"])
        self.assertEqual(observed["bytes"], len(b"Access token is invalid."))

    def test_request_budget_rejects_before_intent_and_socket(self):
        value = job()
        value.requests["normal"] = 16
        with self.assertRaisesRegex(m.StartupError, "startup_request_budget"):
            value.request("public-before")
        value.save.assert_not_called()
        value.endpoint.connect.assert_not_called()


if __name__ == "__main__":
    unittest.main()
