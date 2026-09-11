#!/usr/bin/env python3
"""Expose unchanged reference/client traffic on owned Linux loopback ports.

Run only on the authorized remote test environment. This is an acceptance
transport, not an Emby implementation: upstream status lines, response headers,
entity bodies, and WebSocket frames pass through without interpretation.
Ordinary HTTP uses Connection: close and does not establish keepalive coverage.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import select
import signal
import socket
import socketserver
import stat
import threading
import time


MAX_HEADER = 65536
MAX_BUFFER = 262144
CLONE_NEWNET = 0x40000000
EXPECTED_EXE = "/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer"
FIELD_NAME = re.compile(rb"^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")


class Rejected(Exception):
    """Carry only a fixed, non-sensitive failure category."""


def start_ticks(pid: int) -> str:
    value = Path(f"/proc/{pid}/stat").read_text()
    fields = value[value.rfind(")") + 2:].split()
    if len(fields) < 20 or fields[0] == "Z":
        raise Rejected("reference_process_unavailable")
    return fields[19]


class Reference:
    def __init__(self, pid: int, ticks: str, digest: str, port: int) -> None:
        self.pid, self.ticks, self.digest, self.port = pid, ticks, digest, port
        self.verify()
        self.namespace = os.open(f"/proc/{pid}/ns/net", os.O_RDONLY | os.O_CLOEXEC)
        self.namespace_identity = os.fstat(self.namespace)
        self.verify()

    def verify(self) -> None:
        if start_ticks(self.pid) != self.ticks:
            raise Rejected("reference_process_changed")
        executable = Path(f"/proc/{self.pid}/exe")
        if os.readlink(executable) != EXPECTED_EXE:
            raise Rejected("reference_executable_path_changed")
        with executable.open("rb") as source:
            actual = hashlib.file_digest(source, "sha256").hexdigest()
        if actual != self.digest or start_ticks(self.pid) != self.ticks:
            raise Rejected("reference_executable_identity_changed")
        if hasattr(self, "namespace_identity"):
            current = os.stat(f"/proc/{self.pid}/ns/net")
            if (current.st_dev, current.st_ino) != (
                    self.namespace_identity.st_dev, self.namespace_identity.st_ino):
                raise Rejected("reference_namespace_changed")

    def connect(self) -> socket.socket:
        self.verify()
        original = os.open("/proc/thread-self/ns/net", os.O_RDONLY | os.O_CLOEXEC)
        upstream = None
        try:
            os.setns(self.namespace, CLONE_NEWNET)
            upstream = socket.create_connection(("127.0.0.1", self.port), timeout=10)
        finally:
            try:
                os.setns(original, CLONE_NEWNET)
            except OSError:
                # Continuing in the reference namespace could misroute Goby
                # connections. Terminate the owned helper instead of guessing.
                os._exit(70)
            finally:
                os.close(original)
        try:
            self.verify()
        except Exception:
            if upstream is not None:
                upstream.close()
            raise
        return upstream


class BodyFraming:
    """Validate one request boundary without changing entity or chunk bytes."""

    def __init__(self, length: int, chunked: bool) -> None:
        self.remaining = length
        self.state = "size" if chunked else "fixed"
        self.line = bytearray()
        self.trailer_bytes = 0

    def consume(self, data: bytes) -> None:
        offset = 0
        while offset < len(data):
            if self.state in ("fixed", "data"):
                count = min(self.remaining, len(data) - offset)
                self.remaining -= count
                offset += count
                if self.remaining == 0:
                    self.state = "done" if self.state == "fixed" else "crlf"
                continue
            if self.state == "done":
                raise Rejected("http_pipelining_not_supported")
            self.line.append(data[offset])
            offset += 1
            if len(self.line) > 8192:
                raise Rejected("request_chunk_line_too_large")
            if self.state == "crlf":
                if bytes(self.line) not in (b"\r", b"\r\n"):
                    raise Rejected("request_chunk_boundary_invalid")
                if self.line == b"\r\n":
                    self.line.clear()
                    self.state = "size"
                continue
            if not self.line.endswith(b"\r\n"):
                continue
            line = bytes(self.line[:-2])
            self.line.clear()
            if self.state == "size":
                size = line.split(b";", 1)[0]
                if not re.fullmatch(rb"[0-9a-fA-F]{1,16}", size):
                    raise Rejected("request_chunk_size_invalid")
                self.remaining = int(size, 16)
                self.state = "data" if self.remaining else "trailer"
            else:
                self.trailer_bytes += len(line) + 2
                if self.trailer_bytes > MAX_HEADER:
                    raise Rejected("request_trailers_too_large")
                if not line:
                    self.state = "done"
                else:
                    name, separator, value = line.partition(b":")
                    if (not separator or not FIELD_NAME.fullmatch(name) or
                            any(c < 32 and c != 9 or c == 127 for c in value)):
                        raise Rejected("request_trailer_invalid")


def read_request(client: socket.socket) -> tuple[bytes, bytes, str, BodyFraming | None]:
    data = bytearray()
    client.settimeout(15)
    while b"\r\n\r\n" not in data:
        remaining = MAX_HEADER + 1 - len(data)
        if remaining <= 0:
            raise Rejected("request_headers_too_large")
        chunk = client.recv(min(16384, remaining))
        if not chunk:
            raise Rejected("request_disconnected")
        data.extend(chunk)
    head, body = bytes(data).split(b"\r\n\r\n", 1)
    if len(head) + 4 > MAX_HEADER:
        raise Rejected("request_headers_too_large")
    lines = head.split(b"\r\n")
    parts = lines[0].split(b" ")
    if (len(parts) != 3 or not re.fullmatch(rb"[A-Z]{1,20}", parts[0]) or
            parts[2] not in (b"HTTP/1.0", b"HTTP/1.1") or
            not parts[1].startswith(b"/") or parts[1].startswith(b"//") or
            any(c < 33 or c == 127 for c in parts[1])):
        raise Rejected("request_line_invalid")
    headers = []
    values: dict[bytes, list[bytes]] = {}
    for line in lines[1:]:
        name, separator, value = line.partition(b":")
        if (not separator or not FIELD_NAME.fullmatch(name) or
                any(c < 32 and c != 9 or c == 127 for c in value)):
            raise Rejected("request_header_invalid")
        name = name.lower()
        headers.append((name, line))
        values.setdefault(name, []).append(value.strip())
    if len(headers) > 100 or len(values.get(b"host", [])) != 1:
        raise Rejected("request_headers_invalid")
    lengths = values.get(b"content-length", [])
    encodings = values.get(b"transfer-encoding", [])
    if (len(lengths) > 1 or (lengths and encodings) or
            lengths and not re.fullmatch(rb"[0-9]{1,19}", lengths[0]) or
            encodings and (len(encodings) != 1 or encodings[0].lower() != b"chunked")):
        raise Rejected("request_framing_invalid")
    connection = {part.strip().lower() for value in values.get(b"connection", [])
                  for part in value.split(b",")}
    websocket = (values.get(b"upgrade", [b""])[0].lower() == b"websocket" and
                 b"upgrade" in connection)
    if b"upgrade" in values and not websocket:
        raise Rejected("unsupported_protocol_upgrade")
    if websocket and (parts[0] != b"GET" or encodings or lengths and int(lengths[0]) != 0):
        raise Rejected("websocket_request_invalid")
    # Preserve entity framing headers and their body bytes. Remove conventional
    # proxy transport headers; force ordinary requests to one upstream per TCP
    # connection so a later /web request cannot inherit an earlier API route.
    omitted = {b"connection", b"proxy-connection", b"proxy-authorization", b"keep-alive"}
    protected = {b"host", b"content-length", b"transfer-encoding"}
    if connection & protected:
        raise Rejected("connection_header_framing_conflict")
    omitted |= connection - {b"upgrade"}
    if not websocket:
        omitted.add(b"upgrade")
    forwarded = [lines[0], *(line for name, line in headers if name not in omitted)]
    forwarded.append(b"Connection: Upgrade" if websocket else b"Connection: close")
    path = parts[1].split(b"?", 1)[0].decode("ascii", errors="strict")
    framing = None if websocket else BodyFraming(int(lengths[0]) if lengths else 0, bool(encodings))
    if framing is not None:
        framing.consume(body)
    return b"\r\n".join(forwarded) + b"\r\n\r\n", body, path, framing


def relay(client: socket.socket, upstream: socket.socket, initial: bytes,
          idle_seconds: int, framing: BodyFraming | None) -> None:
    endpoints = (client, upstream)
    other = {client: upstream, upstream: client}
    pending = {client: bytearray(), upstream: bytearray(initial)}
    eof = {client: False, upstream: False}
    shut = {client: False, upstream: False}
    for endpoint in endpoints:
        endpoint.setblocking(False)
    last_activity = time.monotonic()
    while True:
        for endpoint in endpoints:
            if eof[other[endpoint]] and not pending[endpoint] and not shut[endpoint]:
                try:
                    endpoint.shutdown(socket.SHUT_WR)
                except OSError:
                    pass
                shut[endpoint] = True
        if all(eof.values()) and not any(pending.values()):
            return
        readable = [s for s in endpoints if not eof[s] and len(pending[other[s]]) < MAX_BUFFER]
        writable = [s for s in endpoints if pending[s]]
        remaining = idle_seconds - (time.monotonic() - last_activity)
        if remaining <= 0:
            raise Rejected("transport_idle_timeout")
        ready_read, ready_write, _ = select.select(readable, writable, [], min(remaining, 1))
        for endpoint in ready_write:
            try:
                count = endpoint.send(pending[endpoint])
            except BlockingIOError:
                continue
            if count <= 0:
                raise Rejected("transport_disconnected")
            del pending[endpoint][:count]
            last_activity = time.monotonic()
        for endpoint in ready_read:
            try:
                chunk = endpoint.recv(min(65536, MAX_BUFFER - len(pending[other[endpoint]])))
            except BlockingIOError:
                continue
            if chunk:
                if endpoint is client and framing is not None:
                    framing.consume(chunk)
                pending[other[endpoint]].extend(chunk)
                last_activity = time.monotonic()
            else:
                eof[endpoint] = True
        if eof[upstream] and not pending[client]:
            return


class Proxy(socketserver.ThreadingTCPServer):
    allow_reuse_address = False
    daemon_threads = True
    block_on_close = False

    def __init__(self, port: int, reference: Reference, goby_port: int,
                 reference_only: bool, idle: int, slots: threading.BoundedSemaphore) -> None:
        self.reference, self.goby_port = reference, goby_port
        self.reference_only, self.idle, self.slots = reference_only, idle, slots
        super().__init__(("127.0.0.1", port), Handler)

    def process_request(self, request: socket.socket, client_address: tuple) -> None:
        if not self.slots.acquire(blocking=False):
            request.close()
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self.slots.release()
            raise

    def process_request_thread(self, request: socket.socket, client_address: tuple) -> None:
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.slots.release()

    def handle_error(self, request: socket.socket, client_address: tuple) -> None:
        print(json.dumps({"event": "proxy_error", "category": "unexpected_handler_failure"}), flush=True)


class Handler(socketserver.BaseRequestHandler):
    def handle(self) -> None:
        upstream = None
        started = False
        target = "undetermined"
        try:
            head, body, path, framing = read_request(self.request)
            target = "reference" if self.server.reference_only or path.startswith("/web/") else "goby"
            if target == "reference":
                upstream = self.server.reference.connect()
            else:
                upstream = socket.create_connection(("127.0.0.1", self.server.goby_port), timeout=10)
            started = True
            relay(self.request, upstream, head + body, self.server.idle, framing)
        except Exception as error:
            category = str(error) if isinstance(error, Rejected) else "transport_unavailable"
            print(json.dumps({"event": "proxy_error", "target": target, "category": category}), flush=True)
            if not started:
                # This is an explicit transport failure, never a fabricated
                # successful response for an unavailable application endpoint.
                try:
                    self.request.sendall(b"HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
                except OSError:
                    pass
        finally:
            if upstream is not None:
                upstream.close()


def private_report(path: Path, value: dict) -> None:
    parent = path.parent
    info = parent.lstat()
    if (not path.is_absolute() or not stat.S_ISDIR(info.st_mode) or
            info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) & 0o077):
        raise Rejected("report_directory_not_private")
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w") as output:
        json.dump(value, output, indent=2)
        output.write("\n")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reference-pid", type=int, required=True)
    parser.add_argument("--reference-start-ticks", required=True)
    parser.add_argument("--reference-sha256", required=True)
    parser.add_argument("--reference-port", type=int, default=18097)
    parser.add_argument("--reference-listen", type=int, default=18197)
    parser.add_argument("--goby-listen", type=int, default=18196)
    parser.add_argument("--goby-port", type=int, default=18198)
    parser.add_argument("--reference-only", action="store_true")
    parser.add_argument("--idle-seconds", type=int, default=300)
    parser.add_argument("--status-file", type=Path, required=True)
    args = parser.parse_args()
    if (os.geteuid() != 0 or not hasattr(os, "setns") or
            not re.fullmatch(r"[0-9a-f]{64}", args.reference_sha256) or
            not args.reference_start_ticks.isdecimal() or args.reference_pid <= 1 or
            not 30 <= args.idle_seconds <= 3600 or
            any(not 1024 <= port <= 65535 for port in (
                args.reference_port, args.reference_listen, args.goby_listen, args.goby_port))):
        raise Rejected("invalid_remote_configuration")
    os.umask(0o077)
    reference = Reference(args.reference_pid, args.reference_start_ticks,
                          args.reference_sha256, args.reference_port)
    slots = threading.BoundedSemaphore(64)
    servers = []
    running = []
    try:
        servers.append(Proxy(args.reference_listen, reference, args.goby_port, True, args.idle_seconds, slots))
        if not args.reference_only:
            servers.append(Proxy(args.goby_listen, reference, args.goby_port, False, args.idle_seconds, slots))
        private_report(args.status_file, {
            "pid": os.getpid(), "start_ticks": start_ticks(os.getpid()),
            "reference_pid": reference.pid, "reference_start_ticks": reference.ticks,
            "reference_executable_sha256": reference.digest,
            "reference_namespace_inode": reference.namespace_identity.st_ino,
            "listen": [f"127.0.0.1:{server.server_address[1]}" for server in servers],
            "reference_only": args.reference_only,
            "response_policy": "Unchanged upstream status, headers, entity bytes, and WebSocket frames",
            "http_connection_policy": "One ordinary HTTP request per connection; upstream Connection: close; keepalive is not verified",
        })
        stopped = threading.Event()
        signal.signal(signal.SIGTERM, lambda *_: stopped.set())
        signal.signal(signal.SIGINT, lambda *_: stopped.set())
        for server in servers:
            threading.Thread(target=server.serve_forever, daemon=True).start()
            running.append(server)
        print(json.dumps({"event": "proxy_started", "listeners": len(servers)}), flush=True)
        while not stopped.wait(1):
            reference.verify()
    finally:
        for server in servers:
            if server in running:
                server.shutdown()
            server.server_close()
        os.close(reference.namespace)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        category = str(error) if isinstance(error, Rejected) else "startup_or_reference_identity_failure"
        print(json.dumps({"event": "proxy_stopped", "category": category}), flush=True)
        raise SystemExit(1) from None
