#!/usr/bin/env python3
"""Test only the acceptance proxy transport on remote ephemeral loopback ports.

Synthetic HTTP and RFC 6455 peers do not establish Emby/Goby interoperability.
The test never contacts the reference process or any deployed application port.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import importlib.util
import json
import multiprocessing
import os
from pathlib import Path
import socket
import stat
import struct
import threading
import time
import unittest


PROXY_PATH: Path
MEASUREMENTS: dict = {}
CHUNK = bytes(range(256)) * 256


class Reader:
    def __init__(self, connection: socket.socket) -> None:
        self.connection = connection
        self.buffer = bytearray()

    def exact(self, count: int) -> bytes:
        while len(self.buffer) < count:
            data = self.connection.recv(min(65536, count - len(self.buffer)))
            if not data:
                raise EOFError("Synthetic peer ended early")
            self.buffer.extend(data)
        result = bytes(self.buffer[:count])
        del self.buffer[:count]
        return result

    def until(self, delimiter: bytes, maximum: int = 131072) -> bytes:
        while delimiter not in self.buffer:
            data = self.connection.recv(4096)
            if not data:
                raise EOFError("Synthetic peer ended early")
            self.buffer.extend(data)
            if len(self.buffer) > maximum:
                raise ValueError("Synthetic header exceeded its limit")
        end = self.buffer.index(delimiter) + len(delimiter)
        result = bytes(self.buffer[:end])
        del self.buffer[:end]
        return result

    def request(self) -> tuple[bytes, dict[bytes, bytes], bytes, bytes]:
        head = self.until(b"\r\n\r\n")
        lines = head[:-4].split(b"\r\n")
        headers = dict((name.lower(), value.strip()) for name, value in
                       (line.split(b":", 1) for line in lines[1:]))
        if headers.get(b"transfer-encoding") == b"chunked":
            entity, wire = bytearray(), bytearray()
            while True:
                line = self.until(b"\r\n")
                wire.extend(line)
                length = int(line.split(b";", 1)[0].strip(), 16)
                if length == 0:
                    while True:
                        trailer = self.until(b"\r\n")
                        wire.extend(trailer)
                        if trailer == b"\r\n":
                            return lines[0], headers, bytes(entity), bytes(wire)
                entity.extend(self.exact(length))
                wire.extend(entity[-length:])
                suffix = self.exact(2)
                if suffix != b"\r\n":
                    raise ValueError("Synthetic chunk boundary is invalid")
                wire.extend(suffix)
        body = self.exact(int(headers.get(b"content-length", b"0")))
        return lines[0], headers, body, body


def response(body: bytes, status: bytes = b"200 OK", extra: bytes = b"") -> bytes:
    return (b"HTTP/1.1 " + status + b"\r\nContent-Length: " + str(len(body)).encode() +
            b"\r\nConnection: close\r\n" + extra + b"\r\n" + body)


def request(path: bytes = b"/emby/Items", extra: bytes = b"", body: bytes = b"",
            method: bytes = b"GET") -> bytes:
    return (method + b" " + path + b" HTTP/1.1\r\nHost: 127.0.0.1\r\n" + extra +
            (b"Content-Length: " + str(len(body)).encode() + b"\r\n" if body else b"") +
            b"\r\n" + body)


def receive_all(connection: socket.socket, limit: int = 4 * 1024 * 1024) -> bytes:
    result = bytearray()
    while True:
        data = connection.recv(65536)
        if not data:
            return bytes(result)
        result.extend(data)
        if len(result) > limit:
            raise ValueError("Synthetic response exceeded its limit")


class Upstream:
    def __init__(self, handler) -> None:
        self.handler, self.errors = handler, []
        self.count = 0
        self.connections, self.workers = [], []
        self.lock = threading.Lock()
        self.stop = threading.Event()
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", 0))
        self.port = self.listener.getsockname()[1]
        self.listener.listen(128)
        self.listener.settimeout(0.05)
        self.thread = threading.Thread(target=self.accept, daemon=True)
        self.thread.start()

    def accept(self) -> None:
        while not self.stop.is_set():
            try:
                connection, _ = self.listener.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            connection.settimeout(8)
            with self.lock:
                self.count += 1
                self.connections.append(connection)
            worker = threading.Thread(target=self.run, args=(connection,), daemon=True)
            self.workers.append(worker)
            worker.start()

    def run(self, connection: socket.socket) -> None:
        try:
            self.handler(connection)
        except BaseException as error:
            self.errors.append(type(error).__name__)
        finally:
            connection.close()

    def close(self) -> None:
        self.stop.set()
        self.listener.close()
        for connection in self.connections:
            try:
                connection.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            connection.close()
        self.thread.join(1)
        for worker in self.workers:
            worker.join(1)


def proxy_worker(source: str, reference_port: int, goby_port: int, control) -> None:
    sink = os.open(os.devnull, os.O_WRONLY)
    os.dup2(sink, 1)
    os.dup2(sink, 2)
    os.close(sink)
    spec = importlib.util.spec_from_file_location("acceptance_proxy_under_test", source)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)

    class SyntheticReference:
        @staticmethod
        def connect() -> socket.socket:
            return socket.create_connection(("127.0.0.1", reference_port), timeout=3)

    proxy = module.Proxy(0, SyntheticReference(), goby_port, False, 30, threading.BoundedSemaphore(64))
    worker = threading.Thread(target=proxy.serve_forever, kwargs={"poll_interval": 0.05}, daemon=True)
    worker.start()
    control.send({"pid": os.getpid(), "port": proxy.server_address[1]})
    try:
        control.recv()
    finally:
        proxy.shutdown()
        proxy.server_close()
        worker.join(2)
        control.close()


class Rig:
    def __init__(self, goby_handler, reference_handler=None) -> None:
        self.goby = Upstream(goby_handler)
        self.reference = Upstream(reference_handler or goby_handler)
        context = multiprocessing.get_context("spawn")
        self.control, child = context.Pipe()
        self.process = context.Process(target=proxy_worker,
            args=(str(PROXY_PATH), self.reference.port, self.goby.port, child), daemon=True)
        self.process.start()
        child.close()
        if not self.control.poll(5):
            raise RuntimeError("Synthetic proxy did not start")
        self.identity = self.control.recv()
        self.port = self.identity["port"]
        if self.port in (18196, 18197, 18198):
            raise RuntimeError("An ephemeral port collided with a reserved application port")

    def connect(self, receive_buffer: int | None = None) -> socket.socket:
        connection = socket.socket()
        if receive_buffer is not None:
            connection.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, receive_buffer)
        connection.settimeout(8)
        connection.connect(("127.0.0.1", self.port))
        return connection

    def exchange(self, wire: bytes) -> bytes:
        with self.connect() as connection:
            connection.sendall(wire)
            return receive_all(connection)

    def rss(self) -> int:
        for line in Path(f"/proc/{self.identity['pid']}/status").read_text().splitlines():
            if line.startswith("VmRSS:"):
                return int(line.split()[1]) * 1024
        raise RuntimeError("Synthetic proxy RSS is unavailable")

    def close(self) -> None:
        if self.process.is_alive():
            self.control.send("stop")
        self.process.join(3)
        if self.process.is_alive():
            self.process.terminate()
            self.process.join(2)
        self.control.close()
        self.goby.close()
        self.reference.close()


def ws_frame(opcode: int, payload: bytes, masked: bool) -> bytes:
    size = len(payload)
    length = bytes([size]) if size < 126 else b"\x7e" + struct.pack("!H", size) if size < 65536 else b"\x7f" + struct.pack("!Q", size)
    if not masked:
        return bytes([0x80 | opcode]) + length + payload
    key = b"test"
    return bytes([0x80 | opcode, length[0] | 0x80]) + length[1:] + key + bytes(value ^ key[index % 4] for index, value in enumerate(payload))


def ws_read(reader: Reader) -> tuple[int, bytes, bool]:
    first, second = reader.exact(2)
    size = second & 127
    if size == 126:
        size = struct.unpack("!H", reader.exact(2))[0]
    elif size == 127:
        size = struct.unpack("!Q", reader.exact(8))[0]
    if size > 1024 * 1024:
        raise ValueError("Synthetic WebSocket frame exceeded its limit")
    key = reader.exact(4) if second & 128 else None
    payload = reader.exact(size)
    if key:
        payload = bytes(value ^ key[index % 4] for index, value in enumerate(payload))
    return first & 15, payload, key is not None


class TransportTests(unittest.TestCase):
    def rig(self, handler, reference=None) -> Rig:
        rig = Rig(handler, reference)
        self.addCleanup(rig.close)
        return rig

    def test_01_http_status_headers_and_binary_body(self) -> None:
        observed = []
        payload = b"\x00binary\xff\x80\r\nentity"
        expected = response(payload, b"418 Synthetic Test", b"X-Test: unchanged\r\nContent-Type: application/octet-stream\r\n")
        def upstream(connection):
            observed.append(Reader(connection).request())
            connection.sendall(expected)
        rig = self.rig(upstream)
        self.assertEqual(rig.exchange(request(body=payload, method=b"POST")), expected)
        self.assertEqual(observed[0][2], payload)
        self.assertEqual(observed[0][1][b"connection"], b"close")

    def test_02_range_and_conditional_headers(self) -> None:
        observed = []
        body = CHUNK[13:64]
        expected = response(body, b"206 Partial Content", b"Content-Range: bytes 13-63/65536\r\nETag: \"synthetic\"\r\n")
        def upstream(connection):
            observed.append(Reader(connection).request())
            connection.sendall(expected)
        rig = self.rig(upstream)
        self.assertEqual(rig.exchange(request(extra=b"Range: bytes=13-63\r\nIf-Range: \"synthetic\"\r\n")), expected)
        self.assertEqual(observed[0][1][b"range"], b"bytes=13-63")
        self.assertEqual(observed[0][1][b"if-range"], b'"synthetic"')

    def test_03_chunked_upload_and_response(self) -> None:
        wire = b"4;test=one\r\nWiki\r\n5\r\npedia\r\n0\r\nX-Test: trailer\r\n\r\n"
        expected = b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nConnection: close\r\n\r\n" + wire
        observed = []
        def upstream(connection):
            observed.append(Reader(connection).request())
            connection.sendall(expected)
        rig = self.rig(upstream)
        with rig.connect() as connection:
            connection.sendall(request(extra=b"Transfer-Encoding: chunked\r\n", method=b"POST"))
            for byte in wire:
                connection.sendall(bytes([byte]))
            self.assertEqual(receive_all(connection), expected)
        self.assertEqual(observed[0][2], b"Wikipedia")
        self.assertEqual(observed[0][3], wire)

    def test_04_backend_selection_uses_raw_path(self) -> None:
        def make_handler(label):
            def handler(connection):
                Reader(connection).request()
                connection.sendall(response(label))
            return handler
        rig = self.rig(make_handler(b"goby"), make_handler(b"reference"))
        self.assertEqual(rig.exchange(request(b"/web/index.html?target=api")), response(b"reference"))
        self.assertEqual(rig.exchange(request(b"/emby/Items?path=/web/index.html")), response(b"goby"))
        self.assertEqual(rig.exchange(request(b"/%77eb/index.html")), response(b"goby"))

    def test_05_websocket_bidirectional_frames(self) -> None:
        key = base64.b64encode(b"synthetic-key-16!")
        accept = base64.b64encode(hashlib.sha1(key + b"258EAFA5-E914-47DA-95CA-C5AB0DC85B11").digest())
        header = b"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + b"\r\n\r\n"
        payload = CHUNK * 5
        completed = threading.Event()
        def upstream(connection):
            reader = Reader(connection)
            _, headers, _, _ = reader.request()
            self.assertEqual(headers[b"sec-websocket-key"], key)
            connection.sendall(header + ws_frame(1, b"ready", False))
            self.assertEqual(ws_read(reader), (2, payload, True))
            connection.sendall(ws_frame(2, payload[::-1], False) + ws_frame(9, b"ping", False))
            self.assertEqual(ws_read(reader), (10, b"ping", True))
            self.assertEqual(ws_read(reader), (8, b"\x03\xe8", True))
            connection.sendall(ws_frame(8, b"\x03\xe8", False))
            completed.set()
        rig = self.rig(upstream)
        with rig.connect() as connection:
            connection.sendall(request(b"/embywebsocket", extra=b"Connection: keep-alive, UpGrAdE\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + b"\r\n"))
            reader = Reader(connection)
            self.assertEqual(reader.until(b"\r\n\r\n"), header)
            self.assertEqual(ws_read(reader), (1, b"ready", False))
            connection.sendall(ws_frame(2, payload, True))
            self.assertEqual(ws_read(reader), (2, payload[::-1], False))
            self.assertEqual(ws_read(reader), (9, b"ping", False))
            connection.sendall(ws_frame(10, b"ping", True) + ws_frame(8, b"\x03\xe8", True))
            self.assertEqual(ws_read(reader), (8, b"\x03\xe8", False))
        self.assertTrue(completed.wait(2))
        self.assertEqual(rig.goby.errors, [])

    def test_06_request_half_close_preserves_response(self) -> None:
        expected = response(CHUNK * 10)
        saw_half_close = threading.Event()
        def upstream(connection):
            Reader(connection).request()
            self.assertEqual(connection.recv(1), b"")
            saw_half_close.set()
            connection.sendall(expected)
        rig = self.rig(upstream)
        with rig.connect() as connection:
            connection.sendall(request(body=b"completed request", method=b"POST"))
            connection.shutdown(socket.SHUT_WR)
            self.assertEqual(receive_all(connection), expected)
        self.assertTrue(saw_half_close.is_set())
        self.assertEqual(rig.goby.errors, [])

    def cancellation(self, reset: bool) -> None:
        ready, release, cancelled = threading.Event(), threading.Event(), threading.Event()
        observed = {"sent": 0}
        def upstream(connection):
            Reader(connection).request()
            ready.set()
            self.assertTrue(release.wait(3))
            connection.settimeout(3)
            try:
                connection.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: 1073741824\r\nConnection: close\r\n\r\n")
                for _ in range(16384):
                    connection.sendall(CHUNK)
                    observed["sent"] += len(CHUNK)
            except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
                cancelled.set()
        rig = self.rig(upstream)
        connection = rig.connect()
        connection.sendall(request())
        self.assertTrue(ready.wait(2))
        if reset:
            connection.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
        started = time.monotonic()
        connection.close()
        release.set()
        self.assertTrue(cancelled.wait(5), "Active upstream did not observe cancellation before cleanup")
        self.assertLess(observed["sent"], 1073741824)
        MEASUREMENTS["reset_cancel_ms" if reset else "fin_cancel_ms"] = round((time.monotonic() - started) * 1000, 2)
        self.assertEqual(rig.goby.errors, [])

    def test_07_reset_cancels_active_upstream(self) -> None:
        self.cancellation(True)

    def test_08_normal_close_cancels_active_upstream(self) -> None:
        self.cancellation(False)

    def test_09_exact_header_limit_is_accepted(self) -> None:
        def upstream(connection):
            line, headers, _, _ = Reader(connection).request()
            self.assertEqual(line, b"GET /emby/Items HTTP/1.1")
            connection.sendall(response(b"accepted"))
        rig = self.rig(upstream)
        fixed = b"GET /emby/Items HTTP/1.1\r\nHost: 127.0.0.1\r\nX-Padding: "
        wire = fixed + b"x" * (65536 - len(fixed) - 4) + b"\r\n\r\n"
        self.assertEqual(len(wire), 65536)
        self.assertEqual(rig.exchange(wire), response(b"accepted"))

    def test_10_oversize_header_is_rejected_before_connect(self) -> None:
        rig = self.rig(lambda connection: None)
        wire = b"GET /emby/Items HTTP/1.1\r\nHost: 127.0.0.1\r\nX-Padding: " + b"x" * 65536 + b"\r\n\r\n"
        with rig.connect() as connection:
            try:
                connection.sendall(wire)
                result = receive_all(connection)
                self.assertTrue(result.startswith(b"HTTP/1.1 502 "))
            except ConnectionResetError:
                pass
        time.sleep(0.05)
        self.assertEqual(rig.goby.count + rig.reference.count, 0)

    def test_11_conflicting_framing_is_rejected(self) -> None:
        rig = self.rig(lambda connection: None)
        for headers in (b"Content-Length: 1\r\nContent-Length: 1\r\n",
                        b"Content-Length: 1\r\nTransfer-Encoding: chunked\r\n",
                        b"Transfer-Encoding: gzip, chunked\r\n"):
            self.assertTrue(rig.exchange(request(extra=headers)).startswith(b"HTTP/1.1 502 "))
        self.assertEqual(rig.goby.count + rig.reference.count, 0)

    def test_12_invalid_chunk_body_is_not_forwarded(self) -> None:
        rig = self.rig(lambda connection: None)
        self.assertTrue(rig.exchange(request(extra=b"Transfer-Encoding: chunked\r\n", method=b"POST") + b"invalid\r\n").startswith(b"HTTP/1.1 502 "))
        self.assertEqual(rig.goby.count + rig.reference.count, 0)

    def test_13_initial_pipelining_cannot_cross_backend(self) -> None:
        rig = self.rig(lambda connection: None)
        wire = request(b"/web/index.html") + request(b"/emby/Items")
        self.assertTrue(rig.exchange(wire).startswith(b"HTTP/1.1 502 "))
        self.assertEqual(rig.goby.count + rig.reference.count, 0)

    def test_14_late_pipelining_cannot_cross_backend(self) -> None:
        ready, ended = threading.Event(), threading.Event()
        observed = []
        def upstream(connection):
            Reader(connection).request()
            ready.set()
            observed.append(connection.recv(65536))
            ended.set()
        rig = self.rig(upstream)
        with rig.connect() as connection:
            connection.sendall(request(b"/web/index.html"))
            self.assertTrue(ready.wait(2))
            connection.sendall(request(b"/emby/Items"))
            self.assertTrue(ended.wait(2))
        self.assertEqual(observed, [b""])
        self.assertEqual(rig.reference.count, 1)
        self.assertEqual(rig.goby.count, 0)

    def test_15_slow_consumer_bounds_memory_and_recovers(self) -> None:
        total = 64 * 1024 * 1024
        observed = {"sent": 0}
        completed = threading.Event()
        def upstream(connection):
            Reader(connection).request()
            connection.setsockopt(socket.SOL_SOCKET, socket.SO_SNDBUF, 65536)
            connection.sendall(b"HTTP/1.1 200 OK\r\nContent-Length: " + str(total).encode() + b"\r\nConnection: close\r\n\r\n")
            for _ in range(total // len(CHUNK)):
                connection.sendall(CHUNK)
                observed["sent"] += len(CHUNK)
            completed.set()
        rig = self.rig(upstream)
        baseline = rig.rss()
        with rig.connect(receive_buffer=65536) as connection:
            connection.sendall(request())
            reader = Reader(connection)
            reader.until(b"\r\n\r\n")
            memory, sent = [], []
            for _ in range(20):
                memory.append(rig.rss())
                sent.append(observed["sent"])
                time.sleep(0.05)
            self.assertLess(max(memory) - baseline, 12 * 1024 * 1024)
            self.assertEqual(sent[-1], sent[-5], "Producer did not reach bounded backpressure")
            self.assertLess(sent[-1], total // 4)
            digest = hashlib.sha256()
            for index in range(total // len(CHUNK)):
                digest.update(reader.exact(len(CHUNK)))
                if index % 64 == 0:
                    memory.append(rig.rss())
            expected = hashlib.sha256()
            for _ in range(total // len(CHUNK)):
                expected.update(CHUNK)
            self.assertEqual(digest.digest(), expected.digest())
            self.assertTrue(completed.wait(2))
        self.assertLess(max(memory) - baseline, 12 * 1024 * 1024)
        self.assertEqual(rig.goby.errors, [])
        MEASUREMENTS["slow_consumer"] = {"body_bytes": total, "pause_ms": 1000,
            "baseline_rss_bytes": baseline, "peak_rss_bytes": max(memory),
            "rss_growth_limit_bytes": 12 * 1024 * 1024, "producer_bytes_at_pause": sent[-1],
            "producer_plateau_observed": sent[-1] == sent[-5], "complete_entity_sha256": digest.hexdigest(),
            "scope": "Measured process RSS envelope; not an assertion about kernel socket memory or an exact per-connection allocation"}

    def test_16_cancellation_releases_admission_slots(self) -> None:
        ended = threading.Event()
        good = threading.Event()
        def upstream(connection):
            Reader(connection).request()
            if good.is_set():
                connection.sendall(response(b"available"))
                return
            try:
                for _ in range(1024):
                    connection.sendall(CHUNK)
            except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
                ended.set()
        rig = self.rig(upstream)
        for _ in range(72):
            ended.clear()
            connection = rig.connect()
            connection.sendall(request())
            # Wait until the upstream has started so this checks cleanup of an
            # admitted relay rather than rejection before an upstream connect.
            connection.recv(1)
            connection.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
            connection.close()
            self.assertTrue(ended.wait(2))
        good.set()
        self.assertEqual(rig.exchange(request()), response(b"available"))
        self.assertEqual(rig.goby.errors, [])
        MEASUREMENTS["sequential_cancelled_connections"] = 72


class RecordedResult(unittest.TestResult):
    def __init__(self) -> None:
        super().__init__()
        self.records = []
        self.started = {}

    def startTest(self, test) -> None:
        super().startTest(test)
        self.started[test.id()] = time.monotonic()

    def record(self, test, outcome: str, error=None) -> None:
        value = {"test": test.id(), "outcome": outcome,
                 "duration_ms": round((time.monotonic() - self.started[test.id()]) * 1000, 2)}
        if error:
            value["exception_type"] = error[0].__name__
        self.records.append(value)
        print(json.dumps(value), flush=True)

    def addSuccess(self, test) -> None:
        super().addSuccess(test)
        self.record(test, "passed")

    def addFailure(self, test, error) -> None:
        super().addFailure(test, error)
        self.record(test, "failed", error)

    def addError(self, test, error) -> None:
        super().addError(test, error)
        self.record(test, "error", error)


def main() -> int:
    global PROXY_PATH
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--proxy", type=Path, default=Path(__file__).with_name("client-acceptance-proxy.py"))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if os.name != "posix" or not Path("/proc/self/status").exists() or not args.output.is_absolute():
        raise RuntimeError("An authorized Linux environment and absolute report path are required")
    parent = args.output.parent.lstat()
    if (not stat.S_ISDIR(parent.st_mode) or parent.st_uid != os.geteuid() or
            stat.S_IMODE(parent.st_mode) & 0o077 or args.output.exists()):
        raise RuntimeError("The report path must be new and its parent private and owned")
    os.umask(0o077)
    PROXY_PATH = args.proxy.resolve(strict=True)
    before = hashlib.sha256(PROXY_PATH.read_bytes()).hexdigest()
    own_digest = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    result = RecordedResult()
    unittest.defaultTestLoader.loadTestsFromTestCase(TransportTests).run(result)
    unchanged = hashlib.sha256(PROXY_PATH.read_bytes()).hexdigest() == before
    report = {"format": 1, "scope": "Synthetic acceptance-harness transport only; no Emby/Goby client acceptance",
        "test_count": result.testsRun, "passed": sum(x["outcome"] == "passed" for x in result.records),
        "failed": len(result.failures), "errors": len(result.errors), "source_unchanged": unchanged,
        "proxy_source_sha256": before, "test_source_sha256": own_digest,
        "ports": "Automatically allocated ephemeral loopback ports; no reference namespace or application ports used",
        "tests": result.records, "measurements": MEASUREMENTS,
        "limits": ["Ordinary HTTP Connection: close; keepalive is not covered", "The reference process identity and setns integration are outside this synthetic suite"]}
    descriptor = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as output:
        json.dump(report, output, indent=2)
        output.write("\n")
    print(json.dumps({"test_count": report["test_count"], "passed": report["passed"],
                      "failed": report["failed"], "errors": report["errors"], "source_unchanged": unchanged}), flush=True)
    return 0 if result.wasSuccessful() and unchanged else 1


if __name__ == "__main__":
    raise SystemExit(main())
