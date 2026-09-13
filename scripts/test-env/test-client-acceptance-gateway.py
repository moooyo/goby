#!/usr/bin/env python3
"""Exercise the acceptance gateway using remote synthetic loopback peers only.

This suite never starts a browser or connects to a deployed Goby or reference
application. Transport and receipt assertions do not establish client acceptance.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import stat
import sys
import tempfile
import threading
import time
import unittest


sys.dont_write_bytecode = True
GATEWAY = BASE = SUPPORT = None
ORIGIN = "http://127.0.0.1:18250"
AUTHORITY = b"127.0.0.1:18250"
MEASUREMENTS = {}


def load_module(path: Path, name: str):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def request(path=b"/emby/Items", extra=b"", body=b"", method=b"GET", host=AUTHORITY):
    return SUPPORT.request(path, extra, body, method).replace(
        b"Host: 127.0.0.1\r\n", b"Host: " + host + b"\r\n", 1)


class SyntheticBackend:
    def __init__(self, upstream):
        self.upstream = upstream
        self.lock = threading.Lock()
        self.verified = self.connected = 0

    def verify(self):
        with self.lock:
            self.verified += 1

    def connect(self):
        with self.lock:
            self.connected += 1
        return socket.create_connection(("127.0.0.1", self.upstream.port), timeout=3)


class GatewayRig:
    def __init__(self, handler, reference_handler=None):
        self.temporary = tempfile.TemporaryDirectory(prefix="goby-synthetic-gateway-")
        self.parent = Path(self.temporary.name)
        self.parent.chmod(0o700)
        self.root = self.parent / "journal"
        self.goby = SUPPORT.Upstream(handler)
        self.reference = SUPPORT.Upstream(reference_handler or handler)
        self.backends = {"goby": SyntheticBackend(self.goby), "reference": SyntheticBackend(self.reference)}
        self.clients = []
        self.thread = self.server = self.journal = None
        self.stopped = False
        self.journal_closed = False

    def start(self, budgets=None):
        self.budgets = {"maxRequests": 64, "cleanupRequests": 8, "maxApiBodyBytes": 1024, "maxApiTotalBytes": 32768,
                        "maxSeconds": 60, "idleSeconds": 10, "maxConcurrent": 16}
        if budgets is not None:
            self.budgets.update(budgets)
        self.config = {"runId": "synthetic-gateway", "browserOrigin": ORIGIN, "proxyOrigin": ORIGIN,
                       "directOrigin": "http://127.0.0.1:18251", "budgets": self.budgets}
        self.root.mkdir(mode=0o700)
        self.journal = GATEWAY.Journal(self.root, self.budgets)
        self.server = GATEWAY.GatewayServer(0, self.config, self.backends, self.journal, BASE)
        self.port = self.server.server_address[1]
        if self.port in (18196, 18197, 18198, 18250, 18251):
            self.server.server_close()
            self.server = None
            raise RuntimeError("An ephemeral gateway port collided with a reserved authority")
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
        self.thread.start()

    def connect(self):
        connection = socket.create_connection(("127.0.0.1", self.port), timeout=3)
        connection.settimeout(5)
        self.clients.append(connection)
        return connection

    def exchange(self, wire):
        with self.connect() as connection:
            connection.sendall(wire)
            return SUPPORT.receive_all(connection)

    def backend_connections(self):
        return sum(value.connected for value in self.backends.values())

    def finish(self):
        if self.server is not None and not self.stopped:
            if self.thread is not None and self.thread.is_alive():
                self.server.shutdown()
                self.thread.join(2)
            deadline = time.monotonic() + 3
            while True:
                with self.server.active_lock:
                    active = bool(self.server.active)
                if not active:
                    break
                if time.monotonic() >= deadline:
                    raise RuntimeError("Synthetic gateway workers did not finish their receipts")
                time.sleep(0.01)
            self.server.server_close()
            self.stopped = True
        if self.journal is not None and not self.journal_closed:
            self.journal.close()
            self.journal_closed = True

    def receipts(self):
        self.finish()
        index = json.loads((self.root / "index.json").read_bytes())
        results = [json.loads(path.read_bytes()) for path in sorted((self.root / "private").glob("*-result.json"))]
        return index, results

    def close(self):
        for connection in self.clients:
            try:
                connection.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            connection.close()
        if self.server is not None:
            self.server.close_active()
        self.goby.close()
        self.reference.close()
        try:
            self.finish()
        finally:
            self.temporary.cleanup()


class GatewayTests(unittest.TestCase):
    def rig(self, handler, reference_handler=None, *, budgets=None):
        rig = GatewayRig(handler, reference_handler)
        self.addCleanup(rig.close)
        rig.start(budgets)
        return rig

    def assert_rejected(self, rig, wire):
        try:
            received = rig.exchange(wire)
        except ConnectionResetError:
            received = b""
        if received:
            self.assertGreaterEqual(int(received.split(b" ", 2)[1]), 400)
        self.assertEqual(rig.backend_connections(), 0)

    def test_01_absolute_and_origin_forms_require_the_exact_host(self):
        observed = []
        expected = SUPPORT.response(b'{"Items":[]}', extra=b"Content-Type: application/json\r\n")

        def upstream(connection):
            observed.append(SUPPORT.Reader(connection).request())
            connection.sendall(expected)

        rig = self.rig(upstream)
        target = b"/emby/Items?Fields=Overview%2CGenres&StartIndex=0"
        for form in (target, ORIGIN.encode() + target):
            self.assertEqual(rig.exchange(request(form)), expected)
        self.assertEqual([row[0] for row in observed], [b"GET " + target + b" HTTP/1.1"] * 2)
        self.assertTrue(all(row[1][b"host"] == AUTHORITY for row in observed))
        rejected = self.rig(lambda connection: None)
        self.assert_rejected(rejected, request(ORIGIN.encode() + target, host=b"127.0.0.1:18251"))
        self.assert_rejected(rejected, request(target, host=b"127.0.0.1:18251"))
        self.assertEqual(rig.goby.errors, [])

    def test_02_external_origins_and_connect_never_reach_a_backend(self):
        rig = self.rig(lambda connection: None)
        for target, host, method in (
            (b"http://example.invalid/emby/Items", AUTHORITY, b"GET"),
            (b"http://127.0.0.1:18251/emby/Items", b"127.0.0.1:18251", b"GET"),
            (b"https://127.0.0.1:18250/emby/Items", AUTHORITY, b"GET"),
            (b"example.invalid:443", b"example.invalid:443", b"CONNECT"),
            (b"127.0.0.1:18251", b"127.0.0.1:18251", b"CONNECT"),
        ):
            self.assert_rejected(rig, request(target, method=method, host=host))

    def test_03_same_authority_connect_admits_only_a_websocket_upgrade(self):
        nonce = b"synthetic-key-16"
        self.assertEqual(len(nonce), 16)
        key = base64.b64encode(nonce)
        accept = base64.b64encode(hashlib.sha1(key + b"258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest())
        upgrade = (b"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                   b"Sec-WebSocket-Accept: " + accept + b"\r\n\r\n")
        finished = threading.Event()
        payload = b"synthetic-unrecorded-websocket-payload"

        def upstream(connection):
            reader = SUPPORT.Reader(connection)
            line, headers, body, unused_wire = reader.request()
            self.assertEqual(line, b"GET /embywebsocket HTTP/1.1")
            self.assertEqual(headers[b"sec-websocket-key"], key)
            self.assertEqual(body, b"")
            connection.sendall(upgrade)
            self.assertEqual(SUPPORT.ws_read(reader), (2, payload, True))
            connection.sendall(SUPPORT.ws_frame(2, payload[::-1], False))
            self.assertEqual(SUPPORT.ws_read(reader), (8, b"\x03\xe8", True))
            connection.sendall(SUPPORT.ws_frame(8, b"\x03\xe8", False))
            finished.set()

        rig = self.rig(upstream)
        with rig.connect() as connection:
            reader = SUPPORT.Reader(connection)
            connection.sendall(request(AUTHORITY, method=b"CONNECT"))
            self.assertTrue(reader.until(b"\r\n\r\n").startswith(b"HTTP/1.1 200 "))
            self.assertEqual(rig.backend_connections(), 0)
            connection.sendall(request(b"/embywebsocket", extra=(
                b"Connection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\n"
                b"Sec-WebSocket-Key: " + key + b"\r\nOrigin: " + ORIGIN.encode() + b"\r\n")))
            self.assertEqual(reader.until(b"\r\n\r\n"), upgrade)
            connection.sendall(SUPPORT.ws_frame(2, payload, True))
            self.assertEqual(SUPPORT.ws_read(reader), (2, payload[::-1], False))
            connection.sendall(SUPPORT.ws_frame(8, b"\x03\xe8", True))
            self.assertEqual(SUPPORT.ws_read(reader), (8, b"\x03\xe8", False))
        self.assertTrue(finished.wait(2))
        self.assertEqual(rig.backends["goby"].connected, 1)
        self.assertEqual(rig.backends["reference"].connected, 0)
        self.assertEqual(rig.goby.errors, [])
        index, results = rig.receipts()
        self.assertTrue(index["complete"])
        self.assertIsNone(index["failure"])
        self.assertEqual(len(results), 2)
        self.assertEqual(results[0]["outcome"], "connect_handshake")
        self.assertTrue(results[0]["transportHandshakeOnly"])
        self.assertFalse(results[0]["upstreamConnected"])
        self.assertEqual(results[1]["request"]["parentOrdinal"], results[0]["ordinal"])
        self.assertEqual(results[1]["responseStatus"], 101)
        self.assertTrue(results[1]["completeHTTP"])
        self.assertFalse(results[1]["requestBodyRetained"])
        self.assertFalse(results[1]["responseBodyRetained"])
        ledger = b"\n".join(path.read_bytes() for path in sorted(rig.root.rglob("*.json")))
        self.assertNotIn(payload, ledger)
        self.assertNotIn(base64.b64encode(payload), ledger)

    def test_04_connect_rejects_plain_http_and_tls_without_a_backend(self):
        for nested in (request(b"/emby/Users/AuthenticateByName", method=b"POST", body=b"synthetic-form"),
                       b"\x16\x03\x01\x00\x08synthetic-tls"):
            rig = self.rig(lambda connection: None)
            with rig.connect() as connection:
                connection.sendall(request(AUTHORITY, method=b"CONNECT"))
                self.assertTrue(SUPPORT.Reader(connection).until(b"\r\n\r\n").startswith(b"HTTP/1.1 200 "))
                self.assertEqual(rig.backend_connections(), 0)
                connection.sendall(nested)
                connection.shutdown(socket.SHUT_WR)
                try:
                    result = SUPPORT.receive_all(connection)
                except ConnectionResetError:
                    result = b""
                if result:
                    self.assertGreaterEqual(int(result.split(b" ", 2)[1]), 400)
            self.assertEqual(rig.backend_connections(), 0)
            index, results = rig.receipts()
            self.assertTrue(index["complete"])
            self.assertIsNone(index["failure"])
            self.assertEqual(len(results), 2)
            self.assertEqual(results[1]["outcome"], "rejected_or_interrupted")
            self.assertFalse(results[1]["upstreamConnected"])

    def test_05_web_and_media_payloads_never_enter_the_private_ledger(self):
        bodies = {
            b"/web/index.html": b"<html>synthetic-original-web-body-not-for-ledger</html>",
            b"/emby/Videos/synthetic/stream.mp4": b"\x00synthetic-video-body-not-for-ledger\xff",
            b"/emby/Items/synthetic/Images/Primary": b"\x89synthetic-image-body-not-for-ledger\x00",
            b"/emby/Videos/synthetic/Subtitles/0/Stream.vtt": b"WEBVTT\nsynthetic-subtitle-body-not-for-ledger\n",
        }
        observed = []

        def upstream(connection):
            line, headers, body, unused_wire = SUPPORT.Reader(connection).request()
            target = line.split(b" ")[1]
            observed.append((target, body))
            connection.sendall(SUPPORT.response(bodies[target], extra=b"Content-Type: application/json\r\n"))

        rig = self.rig(upstream)
        for target, body in bodies.items():
            self.assertEqual(rig.exchange(request(target)), SUPPORT.response(body, extra=b"Content-Type: application/json\r\n"))
        index, results = rig.receipts()
        self.assertTrue(index["complete"])
        self.assertIsNone(index["failure"])
        self.assertEqual(index["requestCount"], len(bodies))
        self.assertEqual(index["retainedApiBytes"], 0)
        self.assertFalse(index["webMediaAndWebSocketBodiesRetained"])
        self.assertEqual([row["backend"] for row in results], ["reference", "goby", "goby", "goby"])
        for row in results:
            self.assertTrue(row["completeHTTP"])
            self.assertFalse(row["requestBodyRetained"])
            self.assertFalse(row["responseBodyRetained"])
            self.assertIsNone(row["requestBodyBase64"])
            self.assertIsNone(row["responseBodyBase64"])
            self.assertEqual(row["responseBodyWireBytes"], len(bodies[row["request"]["path"].encode()]))
        ledger = b"\n".join(path.read_bytes() for path in sorted(rig.root.rglob("*.json")))
        for body in bodies.values():
            self.assertNotIn(body, ledger)
            self.assertNotIn(base64.b64encode(body), ledger)
        self.assertEqual(len(observed), len(bodies))
        self.assertEqual(rig.goby.errors + rig.reference.errors, [])

    def test_06_api_wire_capture_is_complete_or_explicitly_truncated(self):
        small_request = b'{"synthetic":"request"}'
        small_response = b'{"Items":[{"Id":"synthetic-item"}]}'
        large_request = b'{"request":"' + b"x" * 4096 + b'"}'
        large_response = b'{"response":"' + b"y" * 4096 + b'"}'
        observed = []

        def upstream(connection):
            row = SUPPORT.Reader(connection).request()
            observed.append(row)
            body = small_response if row[0].split(b" ")[1].endswith(b"/small") else large_response
            connection.sendall(SUPPORT.response(body, extra=b"Content-Type: application/json\r\n"))

        rig = self.rig(upstream)
        for suffix, body, expected in ((b"small", small_request, small_response), (b"large", large_request, large_response)):
            self.assertEqual(rig.exchange(request(b"/emby/Items/" + suffix, method=b"POST", body=body,
                extra=b"Content-Type: application/json\r\n")), SUPPORT.response(expected, extra=b"Content-Type: application/json\r\n"))
        index, results = rig.receipts()
        self.assertEqual(len(results), 2)
        self.assertTrue(index["complete"])
        self.assertIsNone(index["failure"])
        self.assertEqual([row[2] for row in observed], [small_request, large_request])
        for row in results:
            self.assertTrue(row["requestBodyComplete"])
            self.assertTrue(row["completeHTTP"])
            self.assertTrue(row["requestBodyRetained"])
            self.assertTrue(row["responseBodyRetained"])
            self.assertEqual(row["bodyStorage"], "http-transfer-wire")
        for direction, body in (("request", small_request), ("response", small_response)):
            self.assertEqual(base64.b64decode(results[0][direction + "BodyBase64"], validate=True), body)
            self.assertEqual(results[0][direction + "BodyWireBytes"], len(body))
            self.assertFalse(results[0][direction + "BodyTruncated"])
        self.assertTrue(results[0]["bodyEvidenceComplete"])
        for direction, body in (("request", large_request), ("response", large_response)):
            self.assertEqual(base64.b64decode(results[1][direction + "BodyBase64"], validate=True), body[:1024])
            self.assertEqual(results[1][direction + "BodyWireBytes"], len(body))
            self.assertTrue(results[1][direction + "BodyTruncated"])
        self.assertFalse(results[1]["bodyEvidenceComplete"])
        self.assertEqual(index["retainedApiBytes"], len(small_request) + len(small_response) + 2048)
        for entry in index["entries"]:
            for key in ("intent", "result"):
                path = Path(entry[key]["path"])
                self.assertEqual(path.parent, rig.root / "private")
                info = path.lstat()
                self.assertTrue(stat.S_ISREG(info.st_mode))
                self.assertEqual(stat.S_IMODE(info.st_mode), 0o600)
                self.assertEqual(info.st_uid, os.geteuid())
                self.assertEqual(hashlib.sha256(path.read_bytes()).hexdigest(), entry[key]["sha256"])
        self.assertEqual(stat.S_IMODE((rig.root / "private").stat().st_mode), 0o700)
        self.assertEqual(rig.goby.errors, [])
        MEASUREMENTS["api_capture"] = {"body_limit": 1024, "large_request_bytes": len(large_request),
            "large_response_bytes": len(large_response), "retained_api_bytes": index["retainedApiBytes"]}

    def test_07_range_status_headers_and_chunk_bytes_are_transparent(self):
        observed = []
        body = bytes(range(13, 64))
        ranged = SUPPORT.response(body, b"206 Partial Content", (
            b"Content-Type: video/mp4\r\nContent-Range: bytes 13-63/65536\r\nETag: \"synthetic\"\r\nX-Case: unchanged\r\n"))
        chunks = b"4;test=one\r\nWiki\r\n5\r\npedia\r\n0\r\nX-Test: trailer\r\n\r\n"
        chunked = b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n" + chunks

        def upstream(connection):
            row = SUPPORT.Reader(connection).request()
            observed.append(row)
            connection.sendall(ranged if row[0].startswith(b"GET ") else chunked)

        rig = self.rig(upstream)
        self.assertEqual(rig.exchange(request(b"/emby/Videos/synthetic/stream.mp4", extra=(
            b"Range: bytes=13-63\r\nIf-Range: \"synthetic\"\r\n"))), ranged)
        self.assertEqual(observed[0][1][b"range"], b"bytes=13-63")
        self.assertEqual(observed[0][1][b"if-range"], b'"synthetic"')
        self.assertEqual(rig.exchange(request(extra=b"Transfer-Encoding: chunked\r\n", method=b"POST") + chunks), chunked)
        self.assertEqual(observed[1][2:], (b"Wikipedia", chunks))
        self.assertEqual(rig.goby.errors, [])

    def test_08_ordinary_http_pipelining_cannot_cross_backends(self):
        class BufferedRequest:
            def __init__(self):
                self.pending = request(b"/web/index.html") + request(b"/emby/Items")

            def settimeout(self, unused):
                pass

            def recv(self, count):
                result, self.pending = self.pending[:count], self.pending[count:]
                return result

        # A fixed initial read proves admission rejection without assuming that
        # TCP delivers two writes, or even one sendall, in a single recv call.
        with self.assertRaisesRegex(BASE.Rejected, "http_pipelining_not_supported"):
            BASE.read_request(BufferedRequest(), target_resolver=lambda method, target, host, websocket:
                GATEWAY.resolve_target(method, target, host, websocket, ORIGIN)["originForm"])
        entered, finished = threading.Event(), threading.Event()
        observed = []

        def upstream(connection):
            observed.append(SUPPORT.Reader(connection).request())
            entered.set()
            observed.append(connection.recv(65536))
            finished.set()

        later = self.rig(upstream)
        with later.connect() as connection:
            connection.sendall(request(b"/web/index.html"))
            self.assertTrue(entered.wait(2))
            connection.sendall(request(b"/emby/Items"))
            self.assertTrue(finished.wait(2))
        self.assertEqual(observed[0][0], b"GET /web/index.html HTTP/1.1")
        self.assertEqual(observed[1], b"")
        self.assertEqual(later.backends["reference"].connected, 1)
        self.assertEqual(later.backends["goby"].connected, 0)
        self.assertEqual(later.reference.errors, [])

    def test_09_normal_budget_denial_preserves_the_cleanup_reserve(self):
        # Prefix lookalikes are ordinary requests. Exact cleanup routes accept
        # the documented case, optional Emby prefix, and trailing slash forms.
        normal = (
            (b"POST", b"/emby/Sessions/Playing/StoppedExtra", b'{"synthetic":"ordinary"}'),
            (b"GET", b"/System/Info/Public", b""),
        )
        cleanup = (
            (b"POST", b"/eMbY/sEsSiOnS/pLaYiNg/sToPpEd/", b'{"synthetic":"stopped"}'),
            (b"POST", b"/Sessions/Logout/", b""),
            (b"GET", b"/EMBY/System/Info/", b""),
        )
        responses = {
            normal[0][1]: SUPPORT.response(b'{"normal":1}', extra=b"Content-Type: application/json\r\n"),
            normal[1][1]: SUPPORT.response(b'{"normal":2}', extra=b"Content-Type: application/json\r\n"),
            cleanup[0][1]: SUPPORT.response(b"", b"204 No Content"),
            cleanup[1][1]: SUPPORT.response(b"", b"204 No Content"),
            cleanup[2][1]: SUPPORT.response(b"Unauthorized", b"401 Unauthorized", b"Content-Type: text/plain\r\n"),
        }
        observed = []

        def upstream(connection):
            line, headers, body, unused_wire = SUPPORT.Reader(connection).request()
            observed.append((line, body))
            connection.sendall(responses[line.split(b" ")[1]])

        rig = self.rig(upstream, budgets={"maxRequests": 5, "cleanupRequests": 3})
        for method, target, body in normal:
            self.assertEqual(rig.exchange(request(target, method=method, body=body,
                extra=b"Content-Type: application/json\r\n" if body else b"")), responses[target])
        self.assertEqual(rig.backend_connections(), 2)
        denied = rig.exchange(request(b"/emby/Items"))
        self.assertTrue(denied.startswith(b"HTTP/1.1 403 "))
        self.assertEqual(rig.backend_connections(), 2)
        for method, target, body in cleanup:
            self.assertEqual(rig.exchange(request(target, method=method, body=body,
                extra=b"Content-Type: application/json\r\n" if body else b"")), responses[target])
        self.assertEqual(observed, [(method + b" " + target + b" HTTP/1.1", body)
                                    for method, target, body in (*normal, *cleanup)])
        self.assertEqual(rig.backends["goby"].connected, 5)
        self.assertEqual(rig.backends["reference"].connected, 0)
        index, results = rig.receipts()
        self.assertEqual(index["requestCount"], 5)
        self.assertEqual(index["requestCounts"], {"normal": 2, "cleanup": 3})
        self.assertEqual(index["deniedRequestCounts"], {"normal": 1, "cleanup": 0})
        self.assertEqual(index["unrecordedConnectionCount"], 1)
        self.assertFalse(index["complete"])
        self.assertIsNone(index["failure"])
        self.assertEqual(len(results), 5)
        expected_classes = ["normal", "normal", "cleanup", "cleanup", "cleanup"]
        self.assertEqual([entry["budgetClass"] for entry in index["entries"]], expected_classes)
        self.assertEqual([row["budgetClass"] for row in results], expected_classes)
        self.assertEqual([row["request"]["budgetClass"] for row in results], expected_classes)
        self.assertEqual([row["responseStatus"] for row in results], [200, 200, 204, 204, 401])
        for entry, row, budget_class in zip(index["entries"], results, expected_classes):
            intent = json.loads(Path(entry["intent"]["path"]).read_bytes())
            self.assertEqual(intent["budgetClass"], budget_class)
            self.assertEqual(intent["request"]["budgetClass"], budget_class)
            self.assertEqual(row["intent"], entry["intent"])
            self.assertEqual(row["outcome"], "observed")
            self.assertTrue(row["upstreamConnected"])
            self.assertTrue(row["completeHTTP"])
            self.assertTrue(row["requestForwardedComplete"])
            self.assertTrue(row["responseForwardedComplete"])
        self.assertEqual(rig.goby.errors, [])
        MEASUREMENTS["cleanup_reserve"] = {"total_requests": 5, "normal_requests": 2, "cleanup_requests": 3,
            "denied_normal_requests": 1, "unrecorded_connections": 1, "journal_complete": False}

    def test_10_websocket_headers_do_not_admit_a_second_http_request(self):
        nonce = b"synthetic-key-16"
        self.assertEqual(len(nonce), 16)
        upgrade_request = request(b"/embywebsocket", extra=(
            b"Connection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\n"
            b"Sec-WebSocket-Key: " + base64.b64encode(nonce) + b"\r\n"))
        second_request = request(b"/emby/Sessions/Playing/Started", method=b"POST", body=b"synthetic-second-post")

        class BufferedClient:
            def __init__(self):
                self.pending = upgrade_request + second_request
                self.sent = bytearray()

            def settimeout(self, unused):
                pass

            def recv(self, count):
                result, self.pending = self.pending[:count], self.pending[count:]
                return result

            def sendall(self, value):
                self.sent.extend(value)

        with self.subTest(phase="initial_read"):
            rig = self.rig(lambda connection: None)
            buffered = BufferedClient()
            # Invoke the real handler with one deterministic header/body read.
            # No network segmentation assumption can hide the appended POST.
            GATEWAY.GatewayHandler(buffered, ("127.0.0.1", 0), rig.server)
            self.assertEqual(bytes(buffered.sent), b"HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
            self.assertEqual(rig.backend_connections(), 0)
            index, results = rig.receipts()
            self.assertTrue(index["complete"])
            self.assertIsNone(index["failure"])
            self.assertEqual(index["requestCount"], 1)
            self.assertEqual(len(results), 1)
            self.assertEqual(results[0]["outcome"], "rejected_or_interrupted")
            self.assertEqual(results[0]["reason"], "websocket_initial_payload_denied")
            self.assertEqual(results[0]["request"]["method"], "GET")
            self.assertEqual(results[0]["request"]["kind"], "websocket")
            self.assertFalse(results[0]["upstreamConnected"])
            self.assertEqual(results[0]["upstreamBytesWritten"], 0)

        for status in (None, b"401 Unauthorized", b"200 OK"):
            with self.subTest(phase="before_upgrade", upstream_status=status):
                entered, finished = threading.Event(), threading.Event()
                observed = []
                response_body = b'{"synthetic":"upgrade-not-accepted"}'
                expected = None if status is None else SUPPORT.response(response_body, status,
                    b"Content-Type: application/json\r\nX-Synthetic: unchanged\r\n")

                def upstream(connection, reply=expected, arrivals=observed, arrived=entered, ended=finished):
                    reader = SUPPORT.Reader(connection)
                    line, headers, body, unused_wire = reader.request()
                    arrivals.append((line, body, bytes(reader.buffer)))
                    if reply is not None:
                        connection.sendall(reply)
                    arrived.set()
                    # Keep the upstream open until the gateway rejects the
                    # additional bytes, even after a complete non-101 reply.
                    arrivals.append(connection.recv(65536))
                    ended.set()

                rig = self.rig(upstream)
                with rig.connect() as connection:
                    connection.sendall(upgrade_request)
                    self.assertTrue(entered.wait(2))
                    if expected is not None:
                        self.assertEqual(SUPPORT.Reader(connection).exact(len(expected)), expected)
                    connection.sendall(second_request)
                    self.assertTrue(finished.wait(2))
                    self.assertEqual(SUPPORT.receive_all(connection), b"")
                self.assertEqual(observed, [(b"GET /embywebsocket HTTP/1.1", b"", b""), b""])
                self.assertEqual(rig.backends["goby"].connected, 1)
                self.assertEqual(rig.backends["reference"].connected, 0)
                self.assertEqual(rig.goby.errors, [])
                index, results = rig.receipts()
                self.assertTrue(index["complete"])
                self.assertIsNone(index["failure"])
                self.assertEqual(index["requestCount"], 1)
                self.assertEqual(index["requestCounts"], {"normal": 1, "cleanup": 0})
                self.assertEqual(index["unrecordedConnectionCount"], 0)
                self.assertEqual(len(results), 1)
                result = results[0]
                self.assertEqual(result["outcome"], "rejected_or_interrupted")
                self.assertEqual(result["reason"], "websocket_payload_before_upgrade_denied")
                self.assertEqual(result["request"]["method"], "GET")
                self.assertEqual(result["request"]["kind"], "websocket")
                self.assertTrue(result["upstreamConnected"])
                forwarded = base64.b64decode(result["request"]["forwardedRequestHeadBase64"], validate=True)
                self.assertEqual(result["upstreamBytesWritten"], len(forwarded))
                self.assertEqual(result["requestBodyWireBytes"], 0)
                self.assertFalse(result["requestForwardedComplete"])
                self.assertFalse(result["responseForwardedComplete"])
                if expected is None:
                    self.assertIsNone(result["responseStatus"])
                    self.assertFalse(result["completeHTTP"])
                    self.assertEqual(result["clientBytesWritten"], 0)
                else:
                    self.assertEqual(result["responseStatus"], int(status.split(b" ", 1)[0]))
                    head = expected.split(b"\r\n\r\n", 1)[0] + b"\r\n\r\n"
                    self.assertEqual(base64.b64decode(result["responseHeadBase64"], validate=True), head)
                    self.assertTrue(result["completeHTTP"])
                    self.assertEqual(result["responseBodyWireBytes"], len(response_body))
                    self.assertEqual(result["clientBytesWritten"], len(expected))

    def test_11_listener_inventory_binds_both_protocol_tables_and_pid_ownership(self):
        listener = {"host": "127.0.0.1", "port": 18250, "socketInode": "7001"}
        pin, foreign = "7001", "7002"

        def row(address, inode):
            return ("0: " + address + ":" + format(listener["port"], "04X") + " " +
                    "0" * len(address) + ":0000 0A 00000000:00000000 00:00000000 00000000 0 0 " +
                    inode + " 1 0000000000000000\n")

        def table(*rows):
            return "sl local_address rem_address st tx_queue tr retrnsmt uid timeout inode\n" + "".join(rows)

        empty = table()
        ipv6_pin = table(row("0" * 32, pin))
        for name, tcp, tcp6 in (
            ("ipv4_loopback", table(row("0100007F", pin)), empty),
            ("ipv4_wildcard", table(row("00000000", pin)), empty),
            ("ipv6_wildcard", empty, ipv6_pin),
        ):
            with self.subTest(listener=name):
                self.assertEqual(GATEWAY.verify_listener_inventory(tcp, tcp6, listener, {pin}), [pin])

        for address in ("0100007F", "00000000"):
            with self.subTest(competing_ipv4_address=address):
                with self.assertRaisesRegex(GATEWAY.GatewayRejected, "^listener_changed$"):
                    GATEWAY.verify_listener_inventory(table(row(address, foreign)), ipv6_pin, listener, {pin})

        with self.subTest(listener="sole_foreign_inode"):
            with self.assertRaisesRegex(GATEWAY.GatewayRejected, "^listener_changed$"):
                GATEWAY.verify_listener_inventory(table(row("0100007F", foreign)), empty, listener, {pin, foreign})
        with self.subTest(listener="pin_without_pid_fd"):
            with self.assertRaisesRegex(GATEWAY.GatewayRejected, "^listener_owner_changed$"):
                GATEWAY.verify_listener_inventory(empty, ipv6_pin, listener, set())
        with self.subTest(listener="unsupported_address_with_valid_pin"):
            with self.assertRaisesRegex(GATEWAY.GatewayRejected, "^listener_address_unsupported$"):
                GATEWAY.verify_listener_inventory(table(row("0100000A", foreign)), ipv6_pin, listener, {pin})

    def test_12_plain_text_json_capture_is_limited_to_exact_playback_posts(self):
        item = "0123456789abcdef0123456789abcdef"
        playback_info = "/emby/Items/" + item + "/PlaybackInfo"
        plain = b"Content-Type: text/plain\r\n"
        payload = b'{ "ItemId": "' + item.encode() + b'", "PositionTicks": 12345 } \r\n'

        class MemoryBudget:
            # Exercise the existing budget implementation without opening a
            # ledger or a socket in this pure capture-policy guard.
            capture = GATEWAY.Journal.capture

            def __init__(self, body_limit=1024, total_limit=4096, charged=0):
                self.lock = threading.Lock()
                self.budgets = {"maxApiBodyBytes": body_limit, "maxApiTotalBytes": total_limit}
                self.api_bytes = charged

        def capture(kind, method, path, content_type, body, budget):
            metadata = {"kind": kind, "method": method, "path": path}
            head = (method.encode() + b" " + path.encode() + b" HTTP/1.1\r\nHost: " + AUTHORITY + b"\r\n" +
                    content_type + b"Content-Length: " + str(len(body)).encode() + b"\r\n\r\n")
            original_metadata, original_head = dict(metadata), bytes(head)
            observer = GATEWAY.Capture(metadata, head, BASE, budget)
            if kind == "websocket":
                # Test retention after the separately tested valid 101 gate.
                observer.status = 101
            observer.observe("request", body[:7])
            observer.observe("request", body[7:])
            self.assertEqual(metadata, original_metadata)
            self.assertEqual(head, original_head)
            self.assertEqual(observer.request_bytes, len(body))
            return observer

        cases = [
            ("playback_info", "api", "POST", playback_info, plain, True),
            ("plain_charset", "api", "POST", playback_info, b"Content-Type: text/plain; charset=UTF-8\r\n", True),
            ("prefixless_case", "api", "POST", "/iTeMs/" + item + "/pLaYbAcKiNfO", plain, True),
            ("started", "api", "POST", "/Sessions/Playing", plain, True),
            ("progress", "api", "POST", "/emby/Sessions/Playing/Progress", plain, True),
            ("stopped_case", "api", "POST", "/EMBY/sessions/playing/stopped", plain, True),
            ("get", "api", "GET", playback_info, plain, False),
            ("put", "api", "PUT", playback_info, plain, False),
            ("web", "web", "POST", playback_info, plain, False),
            ("media", "media", "POST", playback_info, plain, False),
            ("websocket", "websocket", "POST", playback_info, plain, False),
            ("login", "api", "POST", "/emby/Users/AuthenticateByName", plain, False),
            ("logout", "api", "POST", "/emby/Sessions/Logout", plain, False),
            ("ping", "api", "POST", "/emby/Sessions/Playing/Ping", plain, False),
            ("stopped_suffix", "api", "POST", "/emby/Sessions/Playing/StoppedExtra", plain, False),
            ("path_suffix", "api", "POST", playback_info + "/Extra", plain, False),
            ("trailing_slash", "api", "POST", playback_info + "/", plain, False),
            ("missing_id", "api", "POST", "/emby/Items//PlaybackInfo", plain, False),
            ("encoded_separator", "api", "POST", "/emby/Items/a%2fb/PlaybackInfo", plain, False),
            ("other_type", "api", "POST", playback_info, b"Content-Type: application/octet-stream\r\n", False),
            ("missing_type", "api", "POST", playback_info, b"", False),
            ("duplicate_type", "api", "POST", playback_info, plain + plain, False),
            ("existing_json", "api", "POST", "/emby/Users/AuthenticateByName", b"Content-Type: application/json\r\n", True),
            ("existing_form", "api", "POST", "/emby/Users/AuthenticateByName", b"Content-Type: application/x-www-form-urlencoded\r\n", True),
        ]
        for name, kind, method, path, content_type, retained in cases:
            with self.subTest(policy=name):
                budget = MemoryBudget()
                observer = capture(kind, method, path, content_type, payload, budget)
                self.assertEqual(observer.request_capture, retained)
                self.assertEqual(bytes(observer.request_body), payload if retained else b"")
                self.assertFalse(observer.request_truncated)
                self.assertEqual(budget.api_bytes, len(payload) if retained else 0)

        with self.subTest(budget="per_body"):
            body = b'{"profile":"' + b"x" * 2048 + b'"}'
            budget = MemoryBudget()
            observer = capture("api", "POST", playback_info, plain, body, budget)
            self.assertEqual(bytes(observer.request_body), body[:1024])
            self.assertTrue(observer.request_truncated)
            self.assertEqual(budget.api_bytes, 1024)
        with self.subTest(budget="global_remaining"):
            budget = MemoryBudget(total_limit=1024, charged=1020)
            observer = capture("api", "POST", playback_info, plain, payload, budget)
            self.assertEqual(bytes(observer.request_body), payload[:4])
            self.assertTrue(observer.request_truncated)
            self.assertEqual(budget.api_bytes, 1024)


def main():
    global GATEWAY, BASE, SUPPORT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway", type=Path, default=Path(__file__).with_name("client-acceptance-gateway.py"))
    parser.add_argument("--proxy", type=Path, default=Path(__file__).with_name("client-acceptance-proxy.py"))
    parser.add_argument("--proxy-tests", type=Path, default=Path(__file__).with_name("test-client-acceptance-proxy.py"))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if os.name != "posix" or not Path("/proc/self/status").exists() or not args.output.is_absolute():
        raise RuntimeError("An authorized Linux environment and absolute report path are required")
    parent = args.output.parent.lstat()
    if (not stat.S_ISDIR(parent.st_mode) or parent.st_uid != os.geteuid() or
            stat.S_IMODE(parent.st_mode) & 0o077 or os.path.lexists(args.output)):
        raise RuntimeError("The report path must be new and its parent private and owned")
    os.umask(0o077)
    paths = {"gateway": args.gateway.resolve(strict=True), "proxy": args.proxy.resolve(strict=True),
             "proxyTests": args.proxy_tests.resolve(strict=True), "tests": Path(__file__).resolve(strict=True)}
    hashes = {name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in paths.items()}
    SUPPORT = load_module(paths["proxyTests"], "gateway_test_transport_support")
    BASE = load_module(paths["proxy"], "gateway_test_base_proxy")
    GATEWAY = load_module(paths["gateway"], "gateway_under_test")
    result = SUPPORT.RecordedResult()
    unittest.defaultTestLoader.loadTestsFromTestCase(GatewayTests).run(result)
    unchanged = all(hashlib.sha256(path.read_bytes()).hexdigest() == hashes[name] for name, path in paths.items())
    report = {"format": 1, "scope": "Synthetic gateway transport and receipts only; no browser or application acceptance",
              "test_count": result.testsRun, "passed": sum(row["outcome"] == "passed" for row in result.records),
              "failed": len(result.failures), "errors": len(result.errors), "source_unchanged": unchanged,
              "source_sha256": hashes, "tests": result.records, "measurements": MEASUREMENTS,
              "ports": "Ephemeral loopback listeners; logical HTTP authorities are never network destinations"}
    with os.fdopen(os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "w") as stream:
        json.dump(report, stream, indent=2, allow_nan=False)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    print(json.dumps({"test_count": report["test_count"], "passed": report["passed"], "failed": report["failed"],
                      "errors": report["errors"], "source_unchanged": unchanged}), flush=True)
    return 0 if result.testsRun == 12 and result.wasSuccessful() and unchanged else 1


if __name__ == "__main__":
    raise SystemExit(main())
