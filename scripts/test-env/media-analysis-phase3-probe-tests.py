#!/usr/bin/env python3
"""Remote-only focused regressions for the probe's evidence boundary."""

import hashlib
import http.server
import importlib.util
import json
from pathlib import Path
import threading
import unittest
from urllib.parse import urlsplit


spec = importlib.util.spec_from_file_location("phase3_probe", Path(__file__).with_name("media-analysis-phase3-probe.py"))
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)


class HTTPFixture(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/emby/blocked":
            self.send_response(200)
            self.send_header("Content-Length", "5")
            self.end_headers()
            self.wfile.write(b"x")
            self.wfile.flush()
            self.server.blocked_entered.set()
            self.server.blocked_release.wait(timeout=10)
            return
        payload = b'{"ready":false}' if self.path == "/readyz" else b'{"Items":[]}'
        self.send_response(503 if self.path == "/readyz" else 200)
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("X-Request-Id", "bounded-test-request")
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *_):
        pass


class ProbeBoundaryTests(unittest.TestCase):
    def actor(self):
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), HTTPFixture)
        server.daemon_threads = False
        server.blocked_entered = threading.Event()
        server.blocked_release = threading.Event()
        thread = threading.Thread(target=server.serve_forever)
        thread.start()
        def close():
            server.blocked_release.set()
            server.shutdown()
            thread.join(timeout=3)
            server.server_close()
            self.assertFalse(thread.is_alive())
        self.addCleanup(close)
        actor = object.__new__(probe.Probe)
        actor.origin = urlsplit("http://127.0.0.1:%d" % server.server_port)
        actor.c = {"credentials": {"emby_token": "private-token", "emby_authorization": "private-authorization",
                                  "admin_cookie": "private-cookie", "csrf_token": "private-csrf"}}
        actor.receipts = []
        actor.application = {"pid": 1, "start_ticks": "1", "process_group": 1}
        actor.fixture = server
        return actor

    def test_exact_int64_and_duplicate_json_boundary(self):
        value = 9223372036854775807
        self.assertEqual(probe.decode(probe.encode({"ticks": value}))["ticks"], value)
        for raw in (b'{"ticks":1,"ticks":2}', b'{"ticks":NaN}'):
            with self.assertRaises(probe.Failure):
                probe.decode(raw)

    def test_complete_close_connection_body_has_exact_hash_without_credentials(self):
        actor = self.actor()
        record, data = actor.request("GET", "/emby/ok?api_key=private-query-token")
        self.assertEqual(record["http_status"], 200)
        self.assertFalse(record["client_timed_out"])
        self.assertEqual(record["bytes_received"], len(data))
        self.assertEqual(record["response_sha256"], hashlib.sha256(data).hexdigest())
        self.assertEqual(record["path_sha256"], hashlib.sha256(b"/emby/ok").hexdigest())
        self.assertNotIn("private-", json.dumps(record))

    def test_actual_partial_response_timeout_is_not_a_completed_read(self):
        actor = self.actor()
        record, _ = actor.request("GET", "/emby/blocked", timeout=1)
        self.assertTrue(record["client_timed_out"])
        self.assertEqual(record["http_status"], 200)
        self.assertGreaterEqual(record["duration_ms"], 900)
        self.assertTrue(actor.fixture.blocked_entered.is_set())
        self.assertFalse(actor.fixture.blocked_release.is_set())
        self.assertLess(record["bytes_received"], 5)

    def test_readiness503_is_an_observation_without_inferred_sqlstate(self):
        actor = self.actor()
        result = actor.readiness()
        self.assertEqual(result["ready_status"], 503)
        self.assertNotIn("sqlstate", json.dumps(result).lower())


if __name__ == "__main__":
    unittest.main()
