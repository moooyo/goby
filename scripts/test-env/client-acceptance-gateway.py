#!/usr/bin/env python3
"""Bound the original client's HTTP and WebSocket traffic to one owned origin.

The listener serves both origin-form Node requests and Chromium's HTTP proxy
requests. Vendor /web and media/WebSocket payloads pass through without capture.
Only bounded API JSON/form wire bodies are retained privately. Ordinary HTTP
keeps the existing proxy's one-request/Connection: close transport boundary.
maxRequests bounds admitted ledger entries, including a separate cleanup reserve.
Budget refusals are counted without individual receipts and make index.complete
false; they do not stop later requests within the cleanup reserve.
"""

from __future__ import annotations

import argparse
import base64
from copy import deepcopy
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import socket
import socketserver
import stat
import sys
import threading
import time
from urllib.parse import unquote, urlsplit

POLICY_VERSION = "core-av-transparent-gateway-v1"
PROCESS_FIELDS = {"pid", "startTicks", "bootId", "uid", "exe", "exeDevice", "exeInode", "cmdline", "networkNamespace", "cgroup"}
BODY_TYPES = {"application/json", "application/x-www-form-urlencoded"}


class GatewayRejected(ValueError):
    """A fixed gateway policy or evidence failure, never upstream private text."""


class RequestBudgetExceeded(GatewayRejected):
    """A counted admission refusal that leaves the cleanup reserve available."""


def require(condition, reason):
    if not condition:
        raise GatewayRejected(reason)


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False) + "\n").encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def now():
    return datetime.now(timezone.utc).isoformat()


def strict_json(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            require(key not in result, "duplicate_json_key")
            result[key] = value
        return result

    def constant(unused):
        raise GatewayRejected("nonfinite_json_value")

    return json.loads(raw.decode(), object_pairs_hook=pairs, parse_constant=constant)


def read_owned(path, checksum=None):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts, "input_path_invalid")
    for part in (path, *path.parents):
        require(not part.is_symlink(), "input_symlink")
    before = path.stat()
    require(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and before.st_nlink == 1 and
            not before.st_mode & 0o022 and before.st_size <= 8 << 20, "input_metadata_invalid")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        raw = stream.read((8 << 20) + 1)
        after = os.fstat(stream.fileno())
    fields = lambda info: (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns, info.st_mode, info.st_uid)
    require(fields(before) == fields(after) == fields(path.stat()) and len(raw) <= 8 << 20, "input_changed")
    require(checksum is None or sha(raw) == checksum, "input_digest_changed")
    return raw


def load_proxy(descriptor):
    require(set(descriptor) == {"path", "sha256"} and Path(descriptor["path"]).name == "client-acceptance-proxy.py", "proxy_source_descriptor")
    raw = read_owned(descriptor["path"], descriptor["sha256"])
    spec = importlib.util.spec_from_file_location("owned_gateway_transport", descriptor["path"])
    module = importlib.util.module_from_spec(spec)
    exec(compile(raw, descriptor["path"], "exec"), module.__dict__)
    return module


def origin_parts(origin):
    require(isinstance(origin, str) and re.fullmatch(r"http://127\.0\.0\.1:[0-9]{1,5}", origin), "origin_invalid")
    parsed = urlsplit(origin)
    require(1024 <= parsed.port <= 65535, "origin_port_invalid")
    return parsed.netloc, parsed.port


def request_kind(path, websocket=False):
    if websocket:
        return "websocket"
    if path.startswith("/web/"):
        return "web"
    if re.search(r"(?i)(?:^|/)(?:videos|audio)(?:/|$)|/(?:images|subtitles)(?:/|$)|/(?:stream|download)(?:[./]|$)|\.(?:m3u8|ts|mp4|m4s|mp3|flac|ogg|vtt|srt)$", path):
        return "media"
    return "api"


def request_budget_class(request):
    # This selects a finite reserve, not actor authorization or a successful
    # cleanup claim. The outer review must verify the request and response.
    path = request.get("path", "")
    if request.get("kind") == "api":
        if request.get("method") == "POST" and re.fullmatch(r"/(?:emby/)?Sessions/(?:Playing/Stopped|Logout)/?", path, re.IGNORECASE):
            return "cleanup"
        if request.get("method") == "GET" and re.fullmatch(r"/(?:emby/)?System/Info/?", path, re.IGNORECASE):
            return "cleanup"
    return "normal"


def framing_done(framing):
    return framing is None or framing.state == "done" or framing.state == "fixed" and framing.remaining == 0


def resolve_target(method, target, host, websocket, origin, *, tunnel=False):
    authority, unused_port = origin_parts(origin)
    require(host == authority.encode(), "host_origin_mismatch")
    if method == b"CONNECT":
        require(not tunnel and target == authority.encode() and not websocket, "connect_authority_denied")
        return {"originForm": b"/", "kind": "connect", "method": "CONNECT", "origin": origin, "target": target.decode(), "path": "/"}
    if target.startswith(b"/") and not target.startswith(b"//"):
        origin_form = target
    else:
        value = target.decode("ascii", errors="strict")
        parsed = urlsplit(value)
        require(parsed.scheme in (("http", "ws") if websocket else ("http",)) and parsed.netloc == authority and
                parsed.username is None and parsed.password is None and not parsed.fragment, "external_origin_denied")
        origin_form = (parsed.path or "/").encode() + (("?" + parsed.query).encode() if parsed.query else b"")
    require(origin_form.startswith(b"/") and not origin_form.startswith(b"//") and b"#" not in origin_form and b"\\" not in origin_form and
            not any(value < 33 or value == 127 for value in origin_form), "origin_form_invalid")
    path = origin_form.split(b"?", 1)[0].decode("ascii", errors="strict")
    decoded = path
    for unused in range(4):
        newer = unquote(decoded, errors="strict")
        require("\\" not in newer and not any(part in (".", "..") for part in newer.split("/")), "path_traversal_denied")
        if newer == decoded:
            break
        decoded = newer
    require(unquote(decoded) == decoded and path.startswith("/web/") == decoded.startswith("/web/"), "ambiguous_web_path")
    require(not tunnel or method == b"GET" and websocket, "connect_requires_websocket_upgrade")
    require(not decoded.startswith("/web/") or method in (b"GET", b"HEAD") and not websocket, "web_resources_read_only")
    kind = request_kind(decoded, websocket)
    require(kind != "web" or method in (b"GET", b"HEAD") and not websocket, "web_resources_read_only")
    return {"originForm": origin_form, "kind": kind, "method": method.decode(), "origin": origin,
            "target": target.decode(), "path": path}


def metadata(pid):
    process = Path("/proc") / str(pid)
    raw = (process / "stat").read_text()
    fields = raw[raw.rfind(")") + 2:].split()
    require(len(fields) > 19 and fields[0] != "Z", "process_unavailable")
    executable = (process / "exe").stat()
    return {"pid": pid, "startTicks": fields[19], "bootId": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
            "uid": process.stat().st_uid, "exe": os.readlink(process / "exe"), "exeDevice": executable.st_dev,
            "exeInode": executable.st_ino, "cmdline": [value.decode() for value in (process / "cmdline").read_bytes().split(b"\0") if value],
            "networkNamespace": os.readlink(process / "ns/net"), "cgroup": (process / "cgroup").read_text()}


def verify_listener(pid, listener):
    root = Path("/proc") / str(pid)
    rows = [line.split() for line in (root / "net/tcp").read_text().splitlines()[1:]]
    require(any(row[1] in ("0100007F:" + format(listener["port"], "04X"), "00000000:" + format(listener["port"], "04X")) and
                row[3] == "0A" and row[9] == listener["socketInode"] for row in rows), "listener_changed")
    for path in (root / "fd").iterdir():
        try:
            if os.readlink(path) == "socket:[" + listener["socketInode"] + "]":
                return
        except FileNotFoundError:
            continue
    raise GatewayRejected("listener_owner_changed")


class BoundEndpoint:
    """Reuse the transport's namespace connection path with metadata-only guards."""

    def __init__(self, binding, base):
        self.binding, self.base = deepcopy(binding), base
        self.pid, self.port = binding["process"]["pid"], binding["listener"]["port"]
        self.verify()
        self.namespace = os.open("/proc/%d/ns/net" % self.pid, os.O_RDONLY | os.O_CLOEXEC)
        self.namespace_identity = os.fstat(self.namespace)
        self.verify()

    def verify(self):
        require(metadata(self.pid) == self.binding["process"], "upstream_process_changed")
        verify_listener(self.pid, self.binding["listener"])
        if hasattr(self, "namespace_identity"):
            actual = os.stat("/proc/%d/ns/net" % self.pid)
            require((actual.st_dev, actual.st_ino) == (self.namespace_identity.st_dev, self.namespace_identity.st_ino), "upstream_namespace_changed")

    def connect(self):
        return self.base.Reference.connect(self)

    def close(self):
        os.close(self.namespace)


class Journal:
    def __init__(self, root, budgets):
        self.root, self.budgets = Path(root), budgets
        require(self.root.is_absolute() and ".." not in self.root.parts and
                all(not path.is_symlink() for path in (self.root, *self.root.parents)), "ledger_path_invalid")
        info = self.root.lstat()
        require(stat.S_ISDIR(info.st_mode) and not self.root.is_symlink() and info.st_uid == os.geteuid() and
                stat.S_IMODE(info.st_mode) == 0o700 and not list(self.root.iterdir()), "fresh_private_ledger_required")
        (self.root / "private").mkdir(mode=0o700)
        self.identities = {path: (path.stat().st_dev, path.stat().st_ino) for path in (self.root, self.root / "private")}
        self.lock = threading.Lock()
        self.entries, self.count, self.api_bytes = {}, 0, 0
        self.request_counts = {"normal": 0, "cleanup": 0}
        self.denied_counts = {"normal": 0, "cleanup": 0}
        self.unrecorded_connections = 0
        self.failure = None

    def save(self, name, value, *, private=True):
        require(re.fullmatch(r"[a-z0-9-]+\.json", name), "journal_filename_invalid")
        for path, expected in self.identities.items():
            info = path.lstat()
            require(stat.S_ISDIR(info.st_mode) and not path.is_symlink() and info.st_uid == os.geteuid() and
                    stat.S_IMODE(info.st_mode) == 0o700 and (info.st_dev, info.st_ino) == expected, "journal_directory_changed")
        directory = self.root / "private" if private else self.root
        raw = encoded(value)
        with os.fdopen(os.open(directory / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        return {"path": str(directory / name), "sha256": sha(raw)}

    def begin(self, request):
        with self.lock:
            require(self.failure is None, "journal_unavailable")
            budget_class = request_budget_class(request)
            limit = self.budgets["cleanupRequests"] if budget_class == "cleanup" else self.budgets["maxRequests"] - self.budgets["cleanupRequests"]
            if self.request_counts[budget_class] >= limit:
                # Admission refusals never connect upstream. Aggregate accounting
                # preserves the finite ledger bound but cannot prove full detail.
                self.denied_counts[budget_class] += 1
                self.unrecorded_connections += 1
                raise RequestBudgetExceeded(budget_class + "_request_budget_exhausted")
            self.count += 1
            self.request_counts[budget_class] += 1
            ordinal = self.count
            request["budgetClass"] = budget_class
            self.entries[ordinal] = {"ordinal": ordinal, "budgetClass": budget_class, "intent": None, "result": None}
            value = {"ordinal": ordinal, "budgetClass": budget_class, "startedAt": now(), "startedMonotonicNs": time.monotonic_ns(), "request": request}
            try:
                self.entries[ordinal]["intent"] = self.save("request-%06d-intent.json" % ordinal, value)
            except BaseException:
                self.failure = "intent_persistence_failed"
                raise
            return ordinal

    def unrecorded(self, failure):
        with self.lock:
            self.unrecorded_connections += 1
            self.failure = self.failure or failure

    def capture(self, body, chunk):
        with self.lock:
            available = min(self.budgets["maxApiBodyBytes"] - len(body), self.budgets["maxApiTotalBytes"] - self.api_bytes)
            kept = chunk[:max(0, available)]
            body.extend(kept)
            self.api_bytes += len(kept)
            return len(kept) != len(chunk)

    def finish(self, ordinal, result):
        with self.lock:
            try:
                require(self.entries[ordinal]["result"] is None, "duplicate_request_result")
                self.entries[ordinal]["result"] = self.save("request-%06d-result.json" % ordinal,
                    {"ordinal": ordinal, "budgetClass": self.entries[ordinal]["budgetClass"], "intent": self.entries[ordinal]["intent"], "completedAt": now(),
                     "completedMonotonicNs": time.monotonic_ns(), **result})
            except BaseException:
                self.failure = "result_persistence_failed"
                raise

    def close(self):
        with self.lock:
            value = {"schemaVersion": 1, "kind": "core-av-gateway-ledger-index", "requestCount": self.count,
                     "requestCounts": dict(self.request_counts), "deniedRequestCounts": dict(self.denied_counts),
                     "unrecordedConnectionCount": self.unrecorded_connections,
                     "retainedApiBytes": self.api_bytes, "failure": self.failure,
                     "complete": self.failure is None and self.unrecorded_connections == 0 and all(row["result"] is not None for row in self.entries.values()),
                     "entries": list(self.entries.values()), "webMediaAndWebSocketBodiesRetained": False}
            return self.save("index.json", value, private=False)


class Capture:
    def __init__(self, request, raw_head, base, journal):
        self.request, self.base, self.journal = request, base, journal
        self.header_buffer = bytearray()
        self.response_head, self.status, self.response_headers = None, None, []
        self.interim = []
        self.request_body, self.response_body = bytearray(), bytearray()
        self.request_bytes = self.response_bytes = 0
        self.request_truncated = self.response_truncated = False
        self.response_eof = False
        self.response_framing = None
        self.response_body_expected = True
        self.response_capture = False
        request_headers = [line.split(b":", 1) for line in raw_head.split(b"\r\n")[1:] if b":" in line]
        types = [value.strip().lower().split(b";", 1)[0].decode("ascii") for key, value in request_headers if key.lower() == b"content-type"]
        self.request_capture = request["kind"] == "api" and len(types) == 1 and types[0] in BODY_TYPES

    def observe(self, direction, chunk):
        if direction == "request":
            # Upgrade headers alone do not establish a WebSocket. Refuse bytes
            # before a 101 response so a second HTTP request cannot share this
            # request's ordinal after a delayed or rejected upgrade.
            require(not chunk or self.request["kind"] != "websocket" or self.status == 101,
                    "websocket_payload_before_upgrade_denied")
            self.request_bytes += len(chunk)
            if self.request_capture:
                self.request_truncated |= self.journal.capture(self.request_body, chunk)
            return
        if not chunk:
            self.response_eof = True
            return
        remaining = chunk
        while self.response_head is None:
            self.header_buffer.extend(remaining)
            marker = self.header_buffer.find(b"\r\n\r\n")
            require(marker < self.base.MAX_HEADER and (marker >= 0 or len(self.header_buffer) <= self.base.MAX_HEADER), "response_headers_exceeded")
            if marker < 0:
                return
            head = bytes(self.header_buffer[:marker + 4])
            remaining = bytes(self.header_buffer[marker + 4:])
            self.header_buffer.clear()
            lines = head[:-4].split(b"\r\n")
            status = re.fullmatch(rb"HTTP/1\.[01] ([0-9]{3})(?: [^\r\n]*)?", lines[0])
            require(status is not None and 100 <= int(status[1]) <= 599, "response_status_invalid")
            code = int(status[1])
            headers = []
            for line in lines[1:]:
                key, separator, value = line.partition(b":")
                require(separator and self.base.FIELD_NAME.fullmatch(key) and not any(c < 32 and c != 9 or c == 127 for c in value), "response_header_invalid")
                headers.append([key.decode("ascii"), value.strip().decode("latin-1")])
            require(len(headers) <= 100, "response_header_count")
            if 100 <= code < 200 and code != 101:
                require(len(self.interim) < 4, "response_interim_limit")
                self.interim.append(base64.b64encode(head).decode())
                if not remaining:
                    return
                continue
            self.response_head, self.status, self.response_headers = head, code, headers
            require(code != 101 or self.request["kind"] == "websocket", "unexpected_protocol_upgrade")
            values = lambda name: [value for key, value in headers if key.lower() == name]
            lengths, encodings = values("content-length"), values("transfer-encoding")
            require(len(lengths) <= 1 and len(encodings) <= 1 and not (lengths and encodings) and
                    (not lengths or lengths[0].isdigit()) and (not encodings or encodings[0].lower() == "chunked"), "response_framing_invalid")
            self.response_body_expected = self.request["method"] != "HEAD" and code not in (101, 204, 304)
            if not self.response_body_expected:
                self.response_framing = self.base.BodyFraming(0, False) if code != 101 else None
            elif lengths or encodings:
                self.response_framing = self.base.BodyFraming(int(lengths[0]) if lengths else 0, bool(encodings))
            content_types = [value.split(";", 1)[0].strip().lower() for value in values("content-type")]
            self.response_capture = self.request["kind"] == "api" and len(content_types) == 1 and (content_types[0] == "application/json" or content_types[0].endswith("+json"))
        self.response_bytes += len(remaining)
        if self.response_framing is not None:
            self.response_framing.consume(remaining)
        if self.response_capture:
            self.response_truncated |= self.journal.capture(self.response_body, remaining)

    def result(self, request_framing):
        complete = self.response_head is not None
        if complete and self.status != 101:
            complete = framing_done(self.response_framing) if self.response_framing is not None else self.response_eof
        upgraded = self.status == 101 and self.request["kind"] == "websocket"
        return {"responseStatus": self.status, "responseHeaders": self.response_headers,
                "responseHeadBase64": None if self.response_head is None else base64.b64encode(self.response_head).decode(),
                "interimHeadsBase64": self.interim, "completeHTTP": bool(complete),
                "requestBodyComplete": framing_done(request_framing),
                "requestBodyWireBytes": 0 if upgraded else self.request_bytes, "responseBodyWireBytes": 0 if upgraded else self.response_bytes,
                "webSocketClientBytes": self.request_bytes if upgraded else 0, "webSocketUpstreamBytes": self.response_bytes if upgraded else 0,
                "requestBodyRetained": self.request_capture, "responseBodyRetained": self.response_capture,
                "requestBodyTruncated": self.request_truncated, "responseBodyTruncated": self.response_truncated,
                "requestBodyBase64": base64.b64encode(self.request_body).decode() if self.request_capture else None,
                "responseBodyBase64": base64.b64encode(self.response_body).decode() if self.response_capture else None,
                "bodyStorage": "http-transfer-wire", "bodyEvidenceComplete": self.request["kind"] == "api" and bool(complete) and
                    framing_done(request_framing) and not self.request_truncated and not self.response_truncated and
                    (self.request_capture or self.request_bytes == 0) and (self.response_capture or self.response_bytes == 0),
                "webMediaAndWebSocketBodiesRetained": False}


class GatewayServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = False
    daemon_threads = True
    block_on_close = False

    def __init__(self, port, config, backends, journal, base):
        self.config, self.backends, self.journal, self.base = config, backends, journal, base
        self.slots = threading.BoundedSemaphore(config["budgets"]["maxConcurrent"])
        self.active, self.active_lock = set(), threading.Lock()
        super().__init__(("127.0.0.1", port), GatewayHandler)

    def process_request(self, request, address):
        if not self.slots.acquire(blocking=False):
            self.journal.unrecorded("gateway_concurrency_limit")
            request.close()
            return
        with self.active_lock:
            self.active.add(request)
        try:
            super().process_request(request, address)
        except BaseException:
            with self.active_lock:
                self.active.discard(request)
            self.slots.release()
            raise

    def process_request_thread(self, request, address):
        try:
            super().process_request_thread(request, address)
        finally:
            with self.active_lock:
                self.active.discard(request)
            self.slots.release()

    def handle_error(self, request, address):
        self.journal.failure = "gateway_handler_failed"

    def close_active(self):
        with self.active_lock:
            for connection in self.active:
                try:
                    connection.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass


class GatewayHandler(socketserver.BaseRequestHandler):
    def handle(self):
        server, tunnel, parent = self.server, False, None
        for unused in range(2):
            upstream, ordinal, capture, framing = None, None, None, None
            wire_counts = {"upstreamBytesWritten": 0, "clientBytesWritten": 0}
            forwarded = False
            raw_head = b""
            request = {"kind": "rejected", "parentOrdinal": parent}

            def observe_head(value):
                nonlocal raw_head
                raw_head = value

            def resolve(method, target, host, websocket):
                nonlocal request
                request = resolve_target(method, target, host, websocket, server.config["browserOrigin"], tunnel=tunnel)
                origin_form = request.pop("originForm")
                request["parentOrdinal"] = parent
                return origin_form

            try:
                if tunnel:
                    self.request.settimeout(15)
                    require(self.request.recv(1, socket.MSG_PEEK) == b"G", "connect_requires_websocket_upgrade")
                head, body, path, framing = server.base.read_request(self.request, target_resolver=resolve, request_observer=observe_head)
                if request["kind"] == "websocket":
                    pairs = [line.split(b":", 1) for line in raw_head.split(b"\r\n")[1:] if b":" in line]
                    values = lambda key: [value.strip() for name, value in pairs if name.lower() == key]
                    keys, versions, upgrades = values(b"sec-websocket-key"), values(b"sec-websocket-version"), values(b"upgrade")
                    require(len(keys) == 1 and len(base64.b64decode(keys[0], validate=True)) == 16 and versions == [b"13"] and
                            len(upgrades) == 1 and upgrades[0].lower() == b"websocket", "websocket_handshake_invalid")
                request.update(rawRequestHeadBase64=base64.b64encode(raw_head).decode(),
                    forwardedRequestHeadBase64=None if request["kind"] == "connect" else base64.b64encode(head).decode())
                ordinal = server.journal.begin(request)
                require(request["kind"] != "websocket" or not body, "websocket_initial_payload_denied")
                if request["kind"] == "connect":
                    require(not body and framing is not None and framing_done(framing), "connect_body_denied")
                    connect_head = b"HTTP/1.1 200 Connection Established\r\n\r\n"
                    self.request.sendall(connect_head)
                    server.journal.finish(ordinal, {"request": request, "outcome": "connect_handshake", "transportHandshakeOnly": True,
                        "responseStatus": 200, "responseHeadBase64": base64.b64encode(connect_head).decode(),
                        "upstreamConnected": False, "webMediaAndWebSocketBodiesRetained": False})
                    tunnel, parent = True, ordinal
                    continue
                capture = Capture(request, raw_head, server.base, server.journal)
                capture.observe("request", body)
                backend_name = "reference" if request["kind"] == "web" else "goby"
                backend = server.backends[backend_name]
                backend.verify()
                upstream = backend.connect()
                backend.verify()
                forwarded = True
                server.base.relay(self.request, upstream, head + body, server.config["budgets"]["idleSeconds"], framing, observer=capture.observe, counters=wire_counts)
                backend.verify()
                observation = capture.result(framing)
                response_header_bytes = len(capture.response_head or b"") + sum(len(base64.b64decode(value)) for value in capture.interim)
                server.journal.finish(ordinal, {"request": request, "backend": backend_name, "outcome": "observed",
                    "upstreamConnected": True, **wire_counts, "requestForwardedComplete": framing_done(framing) and
                        wire_counts["upstreamBytesWritten"] == len(head) + capture.request_bytes,
                    "responseForwardedComplete": observation["completeHTTP"] and
                        wire_counts["clientBytesWritten"] == response_header_bytes + capture.response_bytes,
                    **observation})
                return
            except BaseException as error:
                reason = str(error) if isinstance(error, (GatewayRejected, server.base.Rejected)) else "gateway_transport_unavailable"
                if ordinal is None and not isinstance(error, RequestBudgetExceeded):
                    try:
                        ordinal = server.journal.begin({"kind": "rejected", "parentOrdinal": parent,
                            "rawRequestHeadBase64": base64.b64encode(raw_head).decode()})
                    except RequestBudgetExceeded:
                        pass
                    except BaseException:
                        server.journal.unrecorded("unrecorded_connection_failure")
                if ordinal is not None:
                    try:
                        server.journal.finish(ordinal, {"request": request, "outcome": "rejected_or_interrupted", "reason": reason,
                            "upstreamConnected": forwarded, **wire_counts, "requestForwardedComplete": False,
                            "responseForwardedComplete": False,
                            **(capture.result(framing) if capture is not None else {})})
                    except BaseException:
                        server.journal.failure = "request_result_unavailable"
                if not forwarded:
                    try:
                        self.request.sendall(b"HTTP/1.1 403 Forbidden\r\nConnection: close\r\nContent-Length: 0\r\n\r\n")
                    except OSError:
                        pass
                return
            finally:
                if upstream is not None:
                    upstream.close()


def validate_config(value):
    require(set(value) == {"schemaVersion", "runId", "proxyOrigin", "browserOrigin", "directOrigin", "ledgerRoot", "sources", "upstreams", "budgets"} and
            value["schemaVersion"] == 1 and re.fullmatch(r"[A-Za-z0-9_-]{1,100}", value["runId"]), "gateway_input_invalid")
    unused_authority, port = origin_parts(value["proxyOrigin"])
    require(value["proxyOrigin"] == value["browserOrigin"] and value["directOrigin"] != value["browserOrigin"], "gateway_origin_binding")
    unused_authority, goby_port = origin_parts(value["directOrigin"])
    require(set(value["sources"]) == {"gateway", "proxy"} and set(value["upstreams"]) == {"reference", "goby"}, "gateway_source_upstream_membership")
    current_boot = Path("/proc/sys/kernel/random/boot_id").read_text().strip()
    for binding in value["upstreams"].values():
        require(set(binding) == {"process", "listener", "executableSha256"} and set(binding["process"]) == PROCESS_FIELDS and
                re.fullmatch(r"[0-9a-f]{64}", binding["executableSha256"]) and binding["process"]["bootId"] == current_boot and
                type(binding["process"]["pid"]) is int and binding["process"]["pid"] > 1 and binding["process"]["pid"] != os.getpid(), "fresh_upstream_binding")
        process = binding["process"]
        require(all(type(process[key]) is int and process[key] >= 0 for key in ("uid", "exeDevice", "exeInode")) and
                process["exeInode"] > 0 and isinstance(process["startTicks"], str) and process["startTicks"].isdigit() and
                isinstance(process["cmdline"], list) and process["cmdline"] and all(isinstance(part, str) for part in process["cmdline"]) and
                all(isinstance(process[key], str) and process[key] for key in ("exe", "networkNamespace", "cgroup")), "upstream_metadata_types")
        listener = binding["listener"]
        require(set(listener) == {"host", "port", "socketInode"} and listener["host"] == "127.0.0.1" and
                type(listener["port"]) is int and 1024 <= listener["port"] <= 65535 and
                re.fullmatch(r"[1-9][0-9]*", listener["socketInode"]), "upstream_listener_binding")
    require(value["upstreams"]["goby"]["listener"]["port"] == goby_port and
            value["upstreams"]["goby"]["process"]["pid"] != value["upstreams"]["reference"]["process"]["pid"], "distinct_upstreams")
    bounds = {"maxRequests": (2, 10000), "cleanupRequests": (1, 1000), "maxApiBodyBytes": (1024, 1048576), "maxApiTotalBytes": (1024, 67108864),
              "maxSeconds": (30, 7200), "idleSeconds": (10, 600), "maxConcurrent": (1, 64)}
    require(set(value["budgets"]) == set(bounds) and all(type(value["budgets"][key]) is int and lo <= value["budgets"][key] <= hi for key, (lo, hi) in bounds.items()) and
            value["budgets"]["cleanupRequests"] < value["budgets"]["maxRequests"] and
            value["budgets"]["maxApiTotalBytes"] >= value["budgets"]["maxApiBodyBytes"], "gateway_budgets_invalid")
    return port


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode, "remote_isolated_gateway_required")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True)
    parser.add_argument("--input-sha256", required=True)
    args = parser.parse_args()
    value = strict_json(read_owned(args.input, args.input_sha256))
    port = validate_config(value)
    require(value["sources"]["gateway"]["path"] == str(Path(__file__).absolute()), "gateway_source_path")
    read_owned(value["sources"]["gateway"]["path"], value["sources"]["gateway"]["sha256"])
    base = load_proxy(value["sources"]["proxy"])
    backends = {name: BoundEndpoint(binding, base) for name, binding in value["upstreams"].items()}
    journal = Journal(value["ledgerRoot"], value["budgets"])
    server = GatewayServer(port, value, backends, journal, base)
    stopping = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stopping.set())
    signal.signal(signal.SIGINT, lambda *_: stopping.set())
    listener = {"host": "127.0.0.1", "port": port, "socketInode": str(os.fstat(server.socket.fileno()).st_ino)}
    policy = {"version": POLICY_VERSION, "allowedOrigins": [value["browserOrigin"]],
              "connectionPolicy": "one ordinary HTTP request per TCP connection; upstream Connection: close",
              "bodyPolicy": "bounded API JSON/form only; web/media/websocket payloads excluded",
              "connectPolicy": "same-authority plaintext WebSocket handshake only", "upstreamIdentityPolicy": "metadata-only; executable SHA-256 bound by preparation",
              "requestBudgetPolicy": "separate normal and cleanup caps; fixed Stop/Logout/SystemInfo reserve; no actor/body authorization"}
    started_ns = time.monotonic_ns()
    deadline_ns = started_ns + value["budgets"]["maxSeconds"] * 1000000000
    attestation = {"schemaVersion": 1, "kind": "core-av-client-gateway", **{key: value[key] for key in
        ("runId", "proxyOrigin", "browserOrigin", "directOrigin", "ledgerRoot", "sources", "budgets", "upstreams")},
        "inputSha256": args.input_sha256, "policy": policy, "process": metadata(os.getpid()), "listener": listener,
        "indexPath": str(Path(value["ledgerRoot"]) / "index.json"), "createdAt": now(),
        "startedMonotonicNs": str(started_ns), "deadlineMonotonicNs": str(deadline_ns)}
    ready = journal.save("gateway-attestation.json", attestation, private=False)
    worker = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.1}, daemon=True)
    worker.start()
    print(json.dumps({"event": "gateway_ready", "attestation": ready}), flush=True)
    try:
        while not stopping.wait(1):
            require(time.monotonic_ns() < deadline_ns and journal.failure is None, "gateway_runtime_or_evidence_budget_exhausted")
            for backend in backends.values():
                backend.verify()
    except BaseException:
        journal.failure = journal.failure or "gateway_runtime_stopped"
        raise
    finally:
        server.shutdown()
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            with server.active_lock:
                if not server.active:
                    break
            time.sleep(0.05)
        server.close_active()
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline:
            with server.active_lock:
                if not server.active:
                    break
            time.sleep(0.05)
        with server.active_lock:
            if server.active:
                journal.failure = "gateway_shutdown_left_active_requests"
        server.server_close()
        worker.join(2)
        for backend in backends.values():
            backend.close()
        index = journal.close()
        print(json.dumps({"event": "gateway_closed", "index": index}), flush=True)


if __name__ == "__main__":
    try:
        main()
    except BaseException as error:
        print(json.dumps({"event": "gateway_failed", "reason": str(error) if isinstance(error, GatewayRejected) else "gateway_runtime_failed"}), flush=True)
        raise SystemExit(1) from None
