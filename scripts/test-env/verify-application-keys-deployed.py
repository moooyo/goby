#!/usr/bin/env python3
"""Verify deployed M5d application keys once through authorized root SSH.

Set GOBY_APPLICATION_KEYS_EXPECTED_BINARY_SHA256 to the accepted build digest.
GOBY_APPLICATION_KEYS_VERIFICATION_RUN optionally selects one manual attempt:
canonical decimal 1..99, default 1. Attempt 1 retains the original report and
marker names. Attempt N uses m5d-deployed-application-keys-attempt-N.json and
m5d-deployed-application-keys-attempt-N.attempt.json, both under /opt/goby-test.
Only those fixed names are selectable; existing selected files are never
overwritten and no automatic retry occurs within an invocation.
Use only inert utilities from the adjacent metadata and sessions verifiers.
One administrator login, two native key creations, their client capabilities,
their playback reports, and their revocations/logouts are the only writes.
Complete database rows, credentials, URLs, source paths, and vault bytes remain
in memory. Only a sanitized aggregate report and exclusive marker reach disk.
No deployment, restart, SQL write, encoder, scan, or user-state write is issued.
"""

from __future__ import annotations

import base64
from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timezone
import hashlib
import http.client
from http.cookies import SimpleCookie
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import secrets
import selectors
import signal
import socket
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qs, urlencode, urlsplit


sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test")
RESULT = ROOT / "m5d-deployed-application-keys.json"
ATTEMPT = ROOT / "m5d-deployed-application-keys.attempt.json"
VAULT = Path("/var/lib/goby-test/application-key-vault/master.key")
OWNER = "goby-application-keys-deployed-m5d-v1"
ORIGIN = "http://127.0.0.1:18096"
TABLES = set("schema_migrations server_settings users sessions libraries library_roots items scan_jobs "
             "catalog_entities item_entities item_images user_item_data play_sessions item_subtitles "
             "encoding_jobs client_playback_references item_metadata_state application_keys application_key_clients".split())
MUTABLE = {"sessions", "application_keys", "application_key_clients", "play_sessions"}
KEY_FIELDS = set("Id AppName CreatedAt LastUsedAt RevokedAt CreatedBy IPAddress Status".split())
USER_DATA_TYPES = {"Movie", "Series", "Season", "Episode", "Video", "Audio", "MusicAlbum", "MusicArtist"}
ID, HASH, TOKEN, KEY_ID = (re.compile(pattern) for pattern in
                          (r"[0-9a-f]{32}", r"[0-9a-f]{64}", r"[A-Za-z0-9_-]{43}", r"[1-9][0-9]*"))
PLAY_ID = re.compile(r"play_[0-9a-f]{32}")
MAX_BODY = 2 * 1024 * 1024
STARTED = time.monotonic()
CLEANING = False


class Failure(Exception):
    """Carry only a fixed, non-secret assertion label."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def remaining(maximum=8):
    available = (177 if CLEANING else 135) - (time.monotonic() - STARTED)
    check(available > 0.1, "The bounded workflow time budget was exhausted")
    return min(maximum, available)


def timeout_signal(_number, _frame):
    raise Failure("The bounded workflow deadline was reached")


def verification_files():
    value = os.environ.get("GOBY_APPLICATION_KEYS_VERIFICATION_RUN", "1")
    check(re.fullmatch(r"[1-9][0-9]?", value), "The verification run must be a canonical decimal integer from one through ninety-nine")
    number = int(value)
    if number == 1:
        return number, RESULT, ATTEMPT
    stem = "m5d-deployed-application-keys-attempt-" + value
    return number, ROOT / (stem + ".json"), ROOT / (stem + ".attempt.json")


def helpers():
    modules = []
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        for name in ("metadata", "sessions"):
            spec = importlib.util.spec_from_file_location(
                "application_keys_" + name, Path(__file__).with_name("verify-" + name + "-deployed.py"))
            check(spec is not None and spec.loader is not None, "Inert snapshot utilities are unavailable")
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            modules.append(module)
    return modules


def database_type(sessions):
    class Database(sessions.Database):
        def __init__(self):
            super().__init__()
            check(self.environment["PGUSER"] == "goby_test", "The deployed database role is outside the authorized scope")

        def read(self, query, label):
            deadline = time.monotonic() + remaining(15)
            process = subprocess.Popen(["/usr/bin/psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"],
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       env=self.environment)
            output, errors = bytearray(), bytearray()
            try:
                process.stdin.write(query.encode("utf-8"))
                process.stdin.close()
                with selectors.DefaultSelector() as selector:
                    for stream, target, limit in ((process.stdout, output, 8 * 1024 * 1024), (process.stderr, errors, 65536)):
                        os.set_blocking(stream.fileno(), False)
                        selector.register(stream, selectors.EVENT_READ, (target, limit))
                    while selector.get_map():
                        check(time.monotonic() < deadline, "A read-only database observation timed out")
                        for key, _ in selector.select(max(0, min(0.25, deadline - time.monotonic()))):
                            block = os.read(key.fd, 65536)
                            if not block:
                                selector.unregister(key.fileobj)
                                continue
                            target, limit = key.data
                            check(len(target) + len(block) <= limit, "A read-only database observation exceeded its byte bound")
                            target.extend(block)
                check(process.wait(timeout=max(0.1, deadline - time.monotonic())) == 0,
                      "A forced read-only database observation failed")
                return json.loads(output)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=1)
                for stream in (process.stdin, process.stdout, process.stderr):
                    stream.close()
    return Database


def rows(value, table):
    return [json.loads(raw) for raw in value[table]]


def index(value, table):
    result = {str(row["id"]): row for row in rows(value, table)}
    check(len(result) == len(value[table]), "A table identity is duplicated")
    return result


def timestamp(value):
    check(isinstance(value, str), "A required UTC timestamp is missing")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    check(parsed.tzinfo is not None and parsed.utcoffset().total_seconds() == 0, "A required timestamp is not UTC")
    return parsed


def snapshot(utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == TABLES and sum(map(len, value.values())) <= 5000, "The complete nineteen-table snapshot escaped its bound")
    migrations = rows(value, "schema_migrations")
    check(len(migrations) == 16 and {row["version"] for row in migrations} == set(range(1, 17)),
          "The deployed schema is not exactly version sixteen")
    media = [row["media"] for row in rows(value, "items") if row["media"] is not None]
    check(len(value["items"]) == 21 and len(media) == 11 and len(value["libraries"]) == 5 and
          all(isinstance(item, dict) and item.get("ProbeVersion") == 6 for item in media),
          "The existing catalog is not twenty-one items and eleven probe-six media items in five libraries")
    return value


def preserve(before, after):
    check(set(before) == set(after) == TABLES, "The public table inventory changed")
    created = {}
    for table in TABLES:
        if table not in MUTABLE:
            check(before[table] == after[table], "A protected catalog, user-state, job, or configuration table changed")
            continue
        old = {str(json.loads(raw)["id"]): raw for raw in before[table]}
        current = {str(json.loads(raw)["id"]): raw for raw in after[table]}
        check(len(current) == len(after[table]) and all(current.get(key) == raw for key, raw in old.items()),
              "A preexisting row changed or disappeared")
        created[table] = {key: json.loads(current[key]) for key in current.keys() - old.keys()}
    return created


def quiescent(value, database):
    check(not any(row["status"] in {"Queued", "Running"} for row in rows(value, "scan_jobs")), "An existing scan is active")
    check(not any(row["state"] in {"queued", "running"} for row in rows(value, "encoding_jobs")), "An existing encoder is active")
    now = timestamp(database.read("SELECT to_json(clock_timestamp());", "Observe playback clock"))
    active = [row for row in rows(value, "play_sessions") if row["state"] in {"Prepared", "Playing", "Paused"}]
    check(len(active) == 1 and active[0]["state"] == "Prepared" and active[0]["user_id"] is not None and
          timestamp(active[0]["expires_at"]) <= now,
          "The baseline must retain exactly one expired normal Prepared row and no fresh playback")


def vault_identity(sessions):
    configured = sessions.private_values(ROOT / "runtime.env", {"GOBY_API_KEY_MASTER_KEY_FILE"})
    check(configured["GOBY_API_KEY_MASTER_KEY_FILE"] == str(VAULT), "The persistent vault path is outside the fixed runtime scope")
    parent = VAULT.parent.lstat()
    check(VAULT.parent.resolve(strict=True) == VAULT.parent and stat.S_ISDIR(parent.st_mode) and parent.st_uid == 995 and
          stat.S_IMODE(parent.st_mode) == 0o700, "The persistent vault directory is not private")
    descriptor = os.open(VAULT, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME)
    with os.fdopen(descriptor, "rb") as source:
        info = os.fstat(source.fileno())
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 995 and stat.S_IMODE(info.st_mode) == 0o600 and
              info.st_nlink == 1 and info.st_size == 32, "The persistent master key is not private and exactly thirty-two bytes")
        digest = hashlib.sha256(source.read(33)).digest()
        check(file_identity(info) == file_identity(os.fstat(source.fileno())) == file_identity(VAULT.lstat()),
              "The persistent master key changed during observation")
    return file_identity(parent), file_identity(info), digest


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns, info.st_mode, info.st_uid, info.st_gid)


def source_identity(item, baseline, configured):
    root = index(baseline, "library_roots").get(item["root_id"], {})
    allowed = [Path(entry) for entry in configured.split(os.pathsep)]
    path, base = Path(item["path"]), Path(root.get("path", ""))
    check(root.get("library_id") == item["library_id"] and Path(root.get("allowed_path", "")) in allowed and
          base.is_absolute() and base == Path(root["allowed_path"]) / root["relative_path"] and
          base.is_relative_to(Path(root["allowed_path"])) and path.is_absolute() and path != base and
          path.is_relative_to(base) and path.resolve(strict=True) == path,
          "The selected existing media path escaped its configured library root")
    descriptor = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        for component in path.parts[1:-1]:
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        source = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME, dir_fd=descriptor)
    finally:
        os.close(descriptor)
    with os.fdopen(source, "rb") as stream:
        info, digest, first = os.fstat(stream.fileno()), hashlib.sha256(), b""
        check(stat.S_ISREG(info.st_mode) and 32 <= info.st_size <= 64 * 1024 * 1024 and info.st_size == item["file_size"],
              "The selected media is not a bounded existing regular file")
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            remaining()
            if not first:
                first = block[:32]
            digest.update(block)
        check(file_identity(info) == file_identity(os.fstat(stream.fileno())) == file_identity(path.lstat()),
              "The existing media file changed during observation")
    return file_identity(info), digest.digest(), first


class Key:
    def __init__(self, name):
        self.name, self.id, self.token, self.parent, self.default = name, "", "", "", ""
        self.attempted = self.revoked = False
        self.contexts = []


class API:
    def __init__(self, keys, item_id):
        self.keys, self.item_id = keys, item_id
        self.source_id = "mediasource_" + item_id
        self.cookie = self.csrf = self.admin_id = ""
        self.login_attempted = self.logout_attempted = False
        self.counts = {"GET": 0, "POST": 0, "DELETE": 0}
        self.plays, self.urls = {}, set()

    def headers(self, key, context=None):
        headers = {"X-Emby-Token": key.token}
        if context:
            headers["Authorization"] = (f'Emby Client="{context["client"]}", DeviceId="{context["device"]}", '
                                        'Device="M5d deployed verifier", Version="m5d-deployed"')
        return headers

    def charge(self, method):
        remaining()
        check(sum(self.counts.values()) < (200 if CLEANING else 165), "The bounded HTTP request budget was exhausted")
        self.counts[method] += 1

    def owned_play(self, identifier, key, context=None):
        if not isinstance(identifier, str) or not PLAY_ID.fullmatch(identifier):
            return False
        play = self.plays.get(identifier)
        return (play is not None and play["key"] is key and play["source"] == self.source_id and
                (context is None or play["context"]["id"] == context.get("id")))

    def request(self, method, path, *, key=None, context=None, body=None, native=False, expected=200,
                raw=False, extra=None, creating=None):
        route = urlsplit(path)
        check(path.startswith("/") and not path.startswith("//") and not route.scheme and not route.netloc and
              not route.fragment and "\r" not in path and "\n" not in path, "An HTTP route escaped its local scope")
        owned = {entry.id for entry in self.keys if entry.id}
        operation = re.fullmatch(r"/admin/v1/api-keys/([1-9][0-9]*)/(revoke|reveal)", route.path)
        playback = "/emby/Items/" + self.item_id + "/PlaybackInfo"
        login = method == "POST" and path == "/admin/v1/session"
        if login:
            check(native and not self.login_attempted, "An additional administrator login was prevented")
            self.login_attempted = True
        elif creating is not None:
            check(method == "POST" and path == "/admin/v1/api-keys" and native and creating in self.keys and
                  not creating.attempted and body == {"AppName": creating.name}, "An additional key creation was prevented")
            creating.attempted = True
        elif method == "GET":
            check(route.path in {"/admin/v1/session", "/admin/v1/api-keys", "/emby/Users", "/emby/Items", "/emby/Sessions", playback}
                  or path in self.urls, "An HTTP read escaped the bounded key and playback scope")
        elif method == "DELETE":
            check(path == "/admin/v1/session" and native and not self.logout_attempted,
                  "An unowned or additional administrator logout was prevented")
            self.logout_attempted = True
        elif operation:
            check(method == "POST" and not route.query and operation[1] in owned and body == {}, "An unowned native key write was prevented")
        else:
            reports = {"/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"}
            allowed = method == "POST" and key in self.keys and key.token and (
                route.path == playback or route.path in {"/emby/Sessions/Capabilities/Full", "/emby/Sessions/Logout"} or
                route.path in reports and isinstance(body, dict) and self.owned_play(body.get("PlaySessionId"), key, context) or
                route.path == "/emby/Sessions/Playing/Ping" and
                self.owned_play(parse_qs(route.query).get("PlaySessionId", [None])[0], key, context))
            check(allowed, "An HTTP write escaped acknowledged owned objects")
        self.charge(method)
        headers = {"Accept": "application/json", "Origin": ORIGIN}
        if native and self.cookie:
            headers.update({"Cookie": "goby_session=" + self.cookie, "X-CSRF-Token": self.csrf})
        if key:
            headers.update(self.headers(key, context))
        headers.update(extra or {})
        payload = None if body is None else json.dumps(body).encode("utf-8")
        check(payload is None or len(payload) <= MAX_BODY, "An HTTP request exceeded its byte bound")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=remaining())
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            cookies = SimpleCookie()
            cookies.load(response.getheader("Set-Cookie", ""))
            if login and "goby_session" in cookies and TOKEN.fullmatch(cookies["goby_session"].value):
                self.cookie = cookies["goby_session"].value
            data = response.read(MAX_BODY + 1)
            check(len(data) <= MAX_BODY, "An HTTP response exceeded its byte bound")
            value = data if raw else json.loads(data) if data else None
            # Retain acknowledged credentials before envelope/status assertions.
            # A lost creation response never authorizes discovering or revoking
            # an unknown key by name, sequence number, or database difference.
            if login and isinstance(value, dict) and isinstance(value.get("CSRFToken"), str) and HASH.fullmatch(value["CSRFToken"]):
                self.csrf = value["CSRFToken"]
            if creating and isinstance(value, dict):
                public, token = value.get("Key", {}), value.get("AccessToken", "")
                if (isinstance(public, dict) and public.get("AppName") == creating.name and isinstance(public.get("Id"), str) and
                        KEY_ID.fullmatch(public["Id"]) and isinstance(token, str) and TOKEN.fullmatch(token)):
                    creating.id, creating.token = public["Id"], token
            if route.path == playback and key in self.keys and context in key.contexts and isinstance(value, dict):
                play_id, sources = value.get("PlaySessionId"), value.get("MediaSources")
                if (isinstance(play_id, str) and PLAY_ID.fullmatch(play_id) and isinstance(sources, list) and
                        len(sources) == 1 and isinstance(sources[0], dict) and sources[0].get("Id") == self.source_id):
                    # A canonical play ID, acknowledged request context, and
                    # exact original source suffice for cleanup registration.
                    # Retain them before later status, envelope, audio-default,
                    # URL, or current-session assertions can fail.
                    self.plays.setdefault(play_id, {"key": key, "context": context, "source": self.source_id, "stopped": False})
                    check(self.owned_play(play_id, key, context), "PlaybackInfo reused another acknowledged client's play session")
            check(response.status == expected, "An HTTP response returned an unexpected status")
            check(response.status != 204 or not data, "An HTTP 204 response included content")
            return value, dict(response.getheaders())
        finally:
            connection.close()


class WebSocket:
    """A bounded standard-library RFC 6455 client; no raw frame logging."""

    def __init__(self, api, key, context):
        self.socket, self.buffer = None, bytearray()
        api.charge("GET")
        self.socket = socket.create_connection(("127.0.0.1", 18096), timeout=remaining(3))
        try:
            nonce = base64.b64encode(secrets.token_bytes(16)).decode("ascii")
            headers = {"Host": "127.0.0.1:18096", "Upgrade": "websocket", "Connection": "Upgrade",
                       "Sec-WebSocket-Key": nonce, "Sec-WebSocket-Version": "13", **api.headers(key, context)}
            wire = "GET /emby/socket HTTP/1.1\r\n" + "".join(f"{name}: {value}\r\n" for name, value in headers.items()) + "\r\n"
            self.socket.sendall(wire.encode("ascii"))
            while b"\r\n\r\n" not in self.buffer:
                block = self.socket.recv(4096)
                check(block and len(self.buffer) + len(block) <= 16384, "A WebSocket handshake failed or exceeded its bound")
                self.buffer.extend(block)
            head, rest = bytes(self.buffer).split(b"\r\n\r\n", 1)
            self.buffer = bytearray(rest)
            lines = head.decode("iso-8859-1").split("\r\n")
            fields = {name.lower(): value.strip() for name, value in (line.split(":", 1) for line in lines[1:])}
            accepted = base64.b64encode(hashlib.sha1((nonce + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
            check(lines[0].split()[1] == "101" and fields.get("sec-websocket-accept") == accepted,
                  "A real WebSocket upgrade was not accepted")
            self.send(9, b"m5d")
            check(self.frame() == (10, b"m5d"), "A new WebSocket did not return its live ping")
        except BaseException:
            self.close()
            raise

    def send(self, opcode, payload):
        mask = secrets.token_bytes(4)
        self.socket.sendall(bytes((0x80 | opcode, 0x80 | len(payload))) + mask +
                            bytes(value ^ mask[offset % 4] for offset, value in enumerate(payload)))

    def take(self, size):
        check(size <= 65536, "A WebSocket frame exceeded its bound")
        while len(self.buffer) < size:
            self.socket.settimeout(remaining(3))
            chunk = self.socket.recv(min(65536 - len(self.buffer), size - len(self.buffer)))
            if not chunk:
                raise EOFError()
            self.buffer.extend(chunk)
        value, self.buffer = bytes(self.buffer[:size]), self.buffer[size:]
        return value

    def frame(self):
        first, second = self.take(2)
        check(first & 0x80 and not first & 0x70 and not second & 0x80, "A WebSocket frame used unexpected fragmentation or masking")
        length = second & 127
        if length in {126, 127}:
            length = int.from_bytes(self.take(2 if length == 126 else 8), "big")
        return first & 15, self.take(length)

    def revoked(self):
        try:
            opcode, payload = self.frame()
            check(opcode == 8 and len(payload) >= 2, "A revoked WebSocket did not receive a close frame")
            self.send(8, payload[:125])
        except (EOFError, ConnectionResetError, BrokenPipeError):
            pass
        finally:
            self.close()

    def close(self):
        if self.socket is not None:
            self.socket.close()
            self.socket = None


def session_dto(api, key, identifier, context=None):
    check(isinstance(identifier, str) and ID.fullmatch(identifier), "A client session identifier is not thirty-two hexadecimal characters")
    value, _ = api.request("GET", "/emby/Sessions?" + urlencode({"Id": identifier}), key=key, context=context)
    check(isinstance(value, list) and len(value) == 1 and value[0].get("Id") == identifier,
          "The session query did not return its real client identity")
    dto = value[0]
    check(not set(dto) & {"UserId", "UserName", "User", "CredentialId", "ApplicationKeyId", "ExpiresAt", "AccessToken", "TokenHash"} and
          isinstance(dto.get("PlayState"), dict), "A userless session exposed private or synthetic user fields")
    return dto


def bind_key(utilities, database, before, key, user_id):
    current = snapshot(utilities, database)
    created = preserve(before, current)
    digest = "\\x" + hashlib.sha256(key.token.encode("ascii")).hexdigest()
    parents = [row for row in created["sessions"].values() if row["token_hash"] == digest]
    check(len(parents) == 1, "An issued application key did not bind to one new credential")
    parent = parents[0]
    check(ID.fullmatch(parent["id"]) and parent["kind"] == "application_key" and parent["user_id"] is None and parent["expires_at"] is None and
          parent["revoked_at"] is None and parent["client_name"] == key.name, "An application credential has an unexpected owner or lifetime")
    key.parent = parent["id"]
    metadata = created["application_keys"].get(key.id, {})
    check(metadata.get("credential_id") == key.parent and metadata.get("created_by") == user_id and
          metadata.get("reported_device_numeric_id") == 1, "The native key metadata did not bind to its acknowledged parent")
    clients = [row for row in created["application_key_clients"].values() if row["credential_id"] == key.parent]
    check(len(clients) == 1 and ID.fullmatch(clients[0]["id"]) and clients[0]["id"] != key.parent and clients[0]["client_name"] == key.name and
          all(clients[0][field] == parent[field] for field in ("device_id", "device_name", "client_version")),
          "The key did not create exactly one independent default client")
    key.default = clients[0]["id"]
    return current


def playback_info(api, key, context, *, method="GET", user_id="", profile=None, current="", url=False):
    parameters = {"UserId": user_id} if user_id else {}
    if current:
        check(api.owned_play(current, key, context), "PlaybackInfo referenced an unacknowledged play session")
        parameters["CurrentPlaySessionId"] = current
    if url:
        parameters["IsPlayback"] = "true"
    body = None
    if method == "POST":
        body, parameters = parameters, {}
        if profile is not None:
            body["DeviceProfile"] = profile
            body["EnableTranscoding"] = False
    path = "/emby/Items/" + api.item_id + "/PlaybackInfo" + ("?" + urlencode(parameters) if parameters else "")
    value, _ = api.request(method, path, key=key, context=context, body=body)
    check(isinstance(value, dict) and isinstance(value.get("MediaSources"), list) and len(value["MediaSources"]) == 1 and
          isinstance(value.get("PlaySessionId"), str) and PLAY_ID.fullmatch(value["PlaySessionId"]),
          "PlaybackInfo did not return one real source and canonical play session")
    source, play_id = value["MediaSources"][0], value["PlaySessionId"]
    check(not current or play_id == current, "PlaybackInfo replaced the acknowledged current play session")
    check(api.owned_play(play_id, key, context) and isinstance(source, dict) and source.get("Id") == api.source_id and
          ("DefaultAudioStreamIndex" in source) == bool(user_id or profile), "PlaybackInfo changed application-key audio default semantics")
    return play_id, source


def report_play(api, play_id, event, position=0, token_only=False):
    check(isinstance(play_id, str) and PLAY_ID.fullmatch(play_id) and play_id in api.plays,
          "A playback report referenced an unacknowledged canonical play session")
    play = api.plays[play_id]
    body = {"PlaySessionId": play_id, "ItemId": api.item_id, "PositionTicks": position}
    if play["source"]:
        body["MediaSourceId"] = play["source"]
    if play["context"]:
        body["SessionId"] = play["context"]["id"]
    path = "/emby/Sessions/Playing" + ("" if event == "Started" else "/" + event)
    api.request("POST", path, key=play["key"], context=None if token_only else play["context"], body=body, expected=204)
    if event == "Stopped":
        play["stopped"] = True


def main():
    global CLEANING
    report = {"owner": OWNER, "status": "failed", "http_retries": 0, "cleanup": {"proven": False, "errors": []}, "snapshots": {},
              "limitations": ["No encoder or profile-transcoding execution", "No service restart; restart durability belongs to the isolated browser verifier"],
              "raw_response_files_written": False, "postgresql_forced_read_only": True,
              "http_request_limit": 200, "http_body_limit_bytes": MAX_BODY, "workflow_limit_seconds": 180}
    utilities = sessions = database = before = api = deployed = vault = media_file = item = configured = parent_identity = None
    sockets, keys, known_admin = [], [], None
    result_path, attempt_path = RESULT, ATTEMPT
    stage = "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without arguments")
        signal.signal(signal.SIGALRM, timeout_signal)
        signal.setitimer(signal.ITIMER_REAL, max(0.1, 135 - (time.monotonic() - STARTED)))
        accepted = os.environ.get("GOBY_APPLICATION_KEYS_EXPECTED_BINARY_SHA256", "")
        check(HASH.fullmatch(accepted), "The accepted M5d executable digest environment variable is required")
        run_number, result_path, attempt_path = verification_files()
        report["verification_run"] = run_number
        report["output_scope"] = {"directory": str(ROOT), "report": result_path.name, "attempt_marker": attempt_path.name}
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed report directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (result_path, attempt_path)),
              "The selected attempt already exists; no retry or overwrite is allowed")
        utilities, sessions = helpers()
        utilities.private_write(attempt_path, {"owner": OWNER, "verification_run": run_number, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                                         "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
        parent_identity = info.st_dev, info.st_ino
        deployed = sessions.deployment(accepted)
        report["deployment"] = {**deployed, "schema_version": 16, "probe_version": 6, "database_port": 5432, "database_role": "goby_test"}
        database = database_type(sessions)()
        before = snapshot(utilities, database)
        report["snapshots"]["before"] = utilities.snapshot_report(before)
        report["baseline_application_keys"] = len(before["application_keys"])
        report["baseline_application_key_clients"] = len(before["application_key_clients"])
        quiescent(before, database)
        vault = vault_identity(sessions)
        candidates = [row for row in rows(before, "items") if row["type"] == "Movie" and isinstance(row["media"], dict) and
                      32 <= row["file_size"] <= 64 * 1024 * 1024 and row["media"].get("DurationTicks", 0) >= 30_000_000 and
                      any(stream.get("CodecType") == "video" and stream.get("Codec") == "h264" for stream in row["media"].get("Streams", [])) and
                      any(stream.get("CodecType") == "audio" and stream.get("Codec") == "aac" for stream in row["media"].get("Streams", []))]
        check(candidates, "No bounded existing H264 and AAC Movie is available")
        item = min(candidates, key=lambda row: (row["file_size"], row["id"]))
        configured = sessions.private_values(ROOT / "runtime.env", {"GOBY_MEDIA_ROOTS"})["GOBY_MEDIA_ROOTS"]
        media_file = source_identity(item, before, configured)
        report["source"] = {"files_hashed": 1, "maximum_file_bytes": 64 * 1024 * 1024,
                            "file_type": "Movie", "video_codec": "h264", "audio_codec": "aac",
                            "read_only_no_follow_no_atime": True}
        credentials = sessions.private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        users = [row for row in rows(before, "users") if row["name"] == credentials["GOBY_SMOKE_NAME"]]
        check(len(users) == 1 and users[0]["is_administrator"] is True and users[0]["is_disabled"] is False,
              "The existing browser credentials do not identify one enabled administrator")
        user_id, run_id = users[0]["id"], secrets.token_hex(16)
        keys = [Key("M5d deployed " + suffix + " " + run_id) for suffix in ("target", "sibling")]
        check(all(row["client_name"] not in {key.name for key in keys} for row in rows(before, "sessions")), "A unique owned key name already exists")
        api = API(keys, item["id"])
        stage = "single native login and two acknowledged keys"
        value, headers = api.request("POST", "/admin/v1/session", native=True,
                                    body={"Name": credentials["GOBY_SMOKE_NAME"], "Password": credentials["GOBY_SMOKE_PASSWORD"]})
        credentials.clear()
        check(api.cookie and api.csrf and value.get("User", {}).get("Id") == user_id, "The native login omitted its owned cookie or CSRF token")
        check(api.csrf == hashlib.sha256(("goby:admin:csrf:" + api.cookie).encode()).hexdigest(),
              "The native CSRF token is not bound to its newly issued cookie")
        cookies = SimpleCookie()
        cookies.load(headers.get("Set-Cookie", ""))
        cookie = cookies.get("goby_session")
        check(cookie is not None and cookie["path"] == "/admin" and cookie["httponly"] and cookie["samesite"].lower() == "strict",
              "The new native cookie lost its expected security attributes")
        logged_in = snapshot(utilities, database)
        api.admin_id = utilities.identify_login(before, logged_in)
        known_admin = index(logged_in, "sessions")[api.admin_id]
        check(known_admin["user_id"] == user_id and known_admin["token_hash"] == "\\x" + hashlib.sha256(api.cookie.encode()).hexdigest(),
              "The native cookie did not bind to the acknowledged new administrator session")
        for key in keys:
            value, headers = api.request("POST", "/admin/v1/api-keys", native=True, creating=key, body={"AppName": key.name}, expected=201)
            check(key.id and key.token and set(value) == {"Key", "AccessToken"} and set(value["Key"]) == KEY_FIELDS and
                  value["Key"]["Status"] == "active" and value["Key"]["RevokedAt"] is None and
                  value["Key"]["CreatedBy"] == user_id and headers.get("Cache-Control") == "no-store", "Native key creation violated its safe contract")
            current = bind_key(utilities, database, before, key, user_id)
            dto = session_dto(api, key, key.default)
            check(dto["Client"] == key.name and dto["DeviceId"] == index(current, "sessions")[key.parent]["device_id"] and
                  "NowPlayingItem" not in dto, "The default key context has unexpected wire metadata")
        check(len({key.token for key in keys} | {api.cookie}) == 3 and keys[0].parent != keys[1].parent, "Independent credentials collapsed together")
        stage = "catalog projection and distinct authorization client contexts"
        target, sibling = keys
        catalog = index(before, "items")
        content_ids = {identifier for identifier, entry in catalog.items() if entry["type"] != "CollectionFolder"}
        root_ids = {identifier for identifier, entry in catalog.items() if entry["type"] == "CollectionFolder" and
                    entry["parent_id"] is None and identifier == entry["library_id"]}
        check(len(root_ids) == 5 and content_ids | root_ids == set(catalog) and not content_ids & root_ids,
              "The complete catalog does not partition into existing content and five library roots")
        root_path = "/emby/Items?" + urlencode({"Recursive": "false", "Limit": "100", "EnableUserData": "true"})
        value, _ = api.request("GET", root_path, key=target)
        check(isinstance(value, dict) and len(value.get("Items", [])) == len(root_ids) and
              value.get("TotalRecordCount") == len(root_ids) and {entry["Id"] for entry in value["Items"]} == root_ids and
              all(entry["Type"] == "CollectionFolder" and "UserData" not in entry for entry in value["Items"]),
              "Nonrecursive catalog access did not return the five real userless library roots")
        report["catalog"] = {"raw_items": len(catalog), "recursive_content_items": len(content_ids), "library_root_items": len(root_ids)}
        catalog_path = "/emby/Items?" + urlencode({"Recursive": "true", "Limit": "100", "EnableUserData": "true"})
        for projected in (False, True):
            value, _ = api.request("GET", catalog_path + ("&UserId=" + user_id if projected else ""), key=target)
            check(isinstance(value, dict) and len(value.get("Items", [])) == len(content_ids) and
                  value.get("TotalRecordCount") == len(content_ids) and {entry["Id"] for entry in value["Items"]} == content_ids,
                  "Recursive catalog access did not return all existing non-collection content")
            check(all(("UserData" in entry) == (projected and entry["Type"] in USER_DATA_TYPES) for entry in value["Items"]),
                  "The optional user-data projection has incorrect ownership or item-type support")
            if projected:
                states = {(row["user_id"], row["item_id"]): row for row in rows(before, "user_item_data")}
                for entry in value["Items"]:
                    if "UserData" not in entry:
                        continue
                    data, old = entry["UserData"], states.get((user_id, entry["Id"]), {})
                    # Folder playback fields are computed from descendants.
                    # Only their own favorite flag is a directly stored scalar.
                    if entry.get("IsFolder"):
                        check(data.get("IsFavorite") == old.get("is_favorite", False), "A folder favorite projection changed")
                        continue
                    for field, column, default in (("PlaybackPositionTicks", "playback_position_ticks", 0), ("PlayCount", "play_count", 0),
                                                    ("IsFavorite", "is_favorite", False), ("Played", "played", False)):
                        check(data.get(field) == old.get(column, default), "Explicit user data did not project the unchanged stored value")
                    check(("LastPlayedDate" in data) == (old.get("last_played_at") is not None), "A last-played date projection changed")
                    if old.get("last_played_at") is not None:
                        check(timestamp(data["LastPlayedDate"]) == timestamp(old["last_played_at"]), "A last-played timestamp projection changed")
        value, _ = api.request("GET", "/emby/Users", key=target)
        check(isinstance(value, list) and {entry["Id"] for entry in value} == set(index(before, "users")), "Users did not return the real bare array")
        device = "m5d-deployed-device-" + run_id
        check(all(row["device_id"] != device for table in ("sessions", "application_key_clients") for row in rows(before, table)),
              "The owned client device identifier already exists")
        for key in keys:
            for suffix in ("Alpha", "Beta"):
                context = {"client": "M5d " + suffix + " " + run_id, "device": device}
                value, _ = api.request("GET", "/emby/Sessions?" + urlencode({"DeviceId": device}), key=key, context=context)
                current = snapshot(utilities, database)
                found = [row for row in preserve(before, current)["application_key_clients"].values() if
                         row["credential_id"] == key.parent and row["client_name"] == context["client"] and row["device_id"] == device]
                check(len(found) == 1 and ID.fullmatch(found[0]["id"]), "Authorization metadata did not create one attributable real client")
                context["id"] = found[0]["id"]
                key.contexts.append(context)
                dto = next((entry for entry in value if entry.get("Id") == context["id"]), {})
                check(dto.get("Client") == context["client"] and dto.get("DeviceId") == device and
                      dto.get("DeviceName") == "M5d deployed verifier" and dto.get("ApplicationVersion") == "m5d-deployed" and
                      not set(dto) & {"UserId", "UserName", "User"}, "Client wire metadata differs from its Authorization identity")
        check(len({key.default for key in keys} | {context["id"] for key in keys for context in key.contexts}) == 6,
              "Default and Alpha/Beta client identities collapsed together")
        for key in keys:
            api.request("GET", "/admin/v1/api-keys", key=key, expected=401, raw=True)
            api.request("GET", "/admin/v1/api-keys", extra={"Cookie": "goby_session=" + key.token}, expected=401, raw=True)
        stage = "userless playback information, original bytes, and report isolation"
        alpha, beta = target.contexts
        plays = []
        for number, context in enumerate((alpha, beta)):
            api.request("POST", "/emby/Sessions/Capabilities/Full", key=target, context=context,
                        body={"PlayableMediaTypes": ["Video"], "SupportedCommands": ["SetVolume" if number == 0 else "DisplayMessage"],
                              "SupportsMediaControl": True}, expected=204)
            play_id, source = playback_info(api, target, context, url=True)
            playback_info(api, target, context, method="POST", current=play_id)
            playback_info(api, target, context, user_id=user_id, current=play_id)
            playback_info(api, target, context, method="POST", user_id=user_id, current=play_id)
            route = source.get("DirectStreamUrl", "")
            parsed, query = urlsplit(route), parse_qs(urlsplit(route).query)
            check(isinstance(source.get("Container"), str) and source["Container"] and
                  parsed.path == "/videos/" + item["id"] + "/original." + source["Container"] and not parsed.scheme and not parsed.netloc and not parsed.fragment and
                  set(query) == {"api_key", "PlaySessionId", "MediaSourceId", "DeviceId"} and
                  query.get("api_key") == [target.token] and query.get("PlaySessionId") == [play_id] and
                  query.get("MediaSourceId") == [source["Id"]] and query.get("DeviceId") == [device], "The returned original stream URL lost its owned credential and context")
            api.urls.add(route)
            raw, headers = api.request("GET", route, expected=206, raw=True, extra={"Range": "bytes=0-31"})
            check(raw == media_file[2] and headers.get("Content-Range") == f"bytes 0-31/{item['file_size']}",
                  "The token-only returned URL did not restore its client context and source bytes")
            report_play(api, play_id, "Started", (number + 1) * 10_000_000)
            report_play(api, play_id, "Progress", (number + 1) * 10_000_000, token_only=True)
            plays.append(play_id)
        profile = {"DirectPlayProfiles": [{"Type": "Video", "Container": source["Container"], "VideoCodec": "h264", "AudioCodec": "aac"}]}
        playback_info(api, target, alpha, method="POST", profile=profile, current=plays[0])
        current = snapshot(utilities, database)
        created = preserve(before, current)
        for number, context in enumerate((alpha, beta)):
            play = created["play_sessions"].get(plays[number], {})
            check(play.get("user_id") is None and play.get("auth_session_id") == target.parent and
                  play.get("application_client_id") == context["id"] and play.get("device_id") == device and play.get("state") == "Playing",
                  "A playback row lost its userless parent and real client ownership")
            dto = session_dto(api, target, context["id"], context)
            check(dto["SupportedCommands"] == ["SetVolume" if number == 0 else "DisplayMessage"] and
                  dto.get("NowPlayingItem", {}).get("Id") == item["id"] and "UserData" not in dto["NowPlayingItem"] and
                  dto["PlayState"].get("PositionTicks") == (number + 1) * 10_000_000, "Client capabilities or NowPlaying state leaked across contexts")
        before_ping = snapshot(utilities, database)
        api.request("POST", "/emby/Sessions/Playing/Ping?" + urlencode({"PlaySessionId": plays[0]}), key=target, expected=204)
        after_ping = snapshot(utilities, database)
        preserve(before, after_ping)
        old_play, new_play = index(before_ping, "play_sessions")[plays[0]], index(after_ping, "play_sessions")[plays[0]]
        check(all(old_play[field] == new_play[field] for field in old_play if field not in {"updated_at", "expires_at"}) and
              timestamp(new_play["updated_at"]) > timestamp(old_play["updated_at"]) and
              timestamp(new_play["expires_at"]) > timestamp(old_play["expires_at"]) and
              abs((timestamp(new_play["expires_at"]) - timestamp(new_play["updated_at"])).total_seconds() - 1800) < 1,
              "Token-only Ping did not naturally extend only its owned playback expiry")
        check(all(row == index(after_ping, "play_sessions")[identifier] for identifier, row in index(before_ping, "play_sessions").items() if identifier != plays[0]),
              "Ping changed another playback row")
        for play_id in plays:
            report_play(api, play_id, "Stopped", token_only=True)
        stage = "real WebSocket parent revocation and independent sibling logout"
        for context in target.contexts:
            sockets.append(WebSocket(api, target, context))
        value, _ = api.request("POST", "/admin/v1/api-keys/" + target.id + "/revoke", native=True, body={})
        check(set(value) == {"Id", "RevokedAt"} and value["Id"] == target.id, "Native revocation returned a different parent")
        revoked_at = value["RevokedAt"]
        timestamp(revoked_at)
        target.revoked = True
        for client in sockets:
            client.revoked()
        for context in target.contexts:
            api.request("GET", "/emby/Sessions", key=target, context=context, expected=401, raw=True)
        api.request("GET", "/emby/Users", key=sibling, context=sibling.contexts[0])
        value, _ = api.request("POST", "/admin/v1/api-keys/" + target.id + "/revoke", native=True, body={})
        check(value == {"Id": target.id, "RevokedAt": revoked_at}, "Repeated native revocation changed its timestamp")
        api.request("POST", "/admin/v1/api-keys/" + target.id + "/reveal", native=True, body={}, expected=409, raw=True)
        value, _ = api.request("POST", "/admin/v1/api-keys/" + sibling.id + "/reveal", native=True, body={})
        check(value == {"Id": sibling.id, "AccessToken": sibling.token}, "The sibling encrypted secret did not decrypt to its issued value")
        api.request("POST", "/emby/Sessions/Logout", key=sibling, context=sibling.contexts[0], expected=204)
        sibling.revoked = True
        for context in sibling.contexts:
            api.request("GET", "/emby/Sessions", key=sibling, context=context, expected=401, raw=True)
        value, _ = api.request("GET", "/admin/v1/api-keys?" + urlencode({"SearchTerm": run_id, "IncludeRevoked": "true", "Limit": "10"}), native=True)
        check(isinstance(value, dict) and value.get("TotalRecordCount") == 2 and len(value.get("Items", [])) == 2 and
              {entry["Id"] for entry in value["Items"]} == {key.id for key in keys} and
              all(set(entry) == KEY_FIELDS and entry["Status"] == "revoked" and entry["CreatedBy"] == user_id for entry in value["Items"]),
              "The native list lost safe metadata or the two owned revoked parents")
        for entry in value["Items"]:
            timestamp(entry["RevokedAt"])
            timestamp(entry["LastUsedAt"])
        check(not any(secret in json.dumps(value) for secret in (api.cookie, api.csrf, *(key.token for key in keys))),
              "A native metadata response exposed a raw authentication secret")
        report["checks"] = {"native_cookie_and_csrf": True, "keys_created": 2, "distinct_client_contexts": 6,
                            "native_list_safe_fields_and_revoked_history": True,
                            "userless_catalog_and_explicit_user_projection": True, "users_bare_array": True,
                            "native_header_and_cookie_denied": True, "playback_get_post_user_defaults": True,
                            "direct_profile_negotiation": True, "real_range_bytes_per_context": 32, "client_capability_and_now_playing_isolation": True,
                            "token_only_progress_ping_stop_context_restore": True, "natural_ping_expiry_extension": True,
                            "real_websockets_closed_by_parent_revoke": 2, "parent_context_denials": 2, "sibling_authorized_after_target_revoke": True,
                            "idempotent_revocation_timestamp": True, "revoked_reveal_status": 409, "sibling_secret_decryption": True,
                            "sibling_logout_status": 204, "sibling_context_denials_after_logout": 2}
        report["status"] = "passed"
    except BaseException as error:
        report["failed_stage"] = stage
        report["error"] = str(error) if isinstance(error, Failure) else "A protected operation failed; exception details were withheld"
    finally:
        CLEANING = True
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, max(0.1, 178 - (time.monotonic() - STARTED)))
        for client in sockets:
            client.close()
        if api is not None:
            for play_id, play in api.plays.items():
                if not play["stopped"] and not play["key"].revoked:
                    try:
                        report_play(api, play_id, "Stopped", token_only=True)
                    except BaseException:
                        report["cleanup"]["errors"].append("An acknowledged owned playback could not be stopped")
            if api.cookie and not api.csrf:
                try:
                    value, _ = api.request("GET", "/admin/v1/session", native=True)
                    api.csrf = value.get("CSRFToken", "")
                    check(HASH.fullmatch(api.csrf), "The owned administrator CSRF token could not be recovered")
                except BaseException:
                    report["cleanup"]["errors"].append("The owned administrator cleanup credential could not be recovered")
            for key in keys:
                if key.id and key.token and not key.revoked and api.cookie and api.csrf:
                    try:
                        api.request("POST", "/admin/v1/api-keys/" + key.id + "/revoke", native=True, body={})
                        key.revoked = True
                    except BaseException:
                        report["cleanup"]["errors"].append("An acknowledged owned parent key could not be revoked")
                if key.attempted and not (key.id and key.token):
                    report["cleanup"]["errors"].append("A key creation response was lost; no unknown key was mutated")
            if api.cookie and api.csrf:
                try:
                    api.request("DELETE", "/admin/v1/session", native=True, expected=204)
                except BaseException:
                    report["cleanup"]["errors"].append("The single owned administrator logout could not be confirmed")
            elif api.login_attempted:
                report["cleanup"]["errors"].append("The administrator login response was lost; no unknown session was mutated")
            report["http_method_counts"] = api.counts
        if before is not None:
            try:
                final = snapshot(utilities, database)
                report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                created = preserve(before, final)
                check(api is not None and api.admin_id and known_admin, "The administrator database identity is unproven")
                parents = {key.parent for key in keys if key.parent}
                check(set(created["sessions"]) == parents | {api.admin_id} and
                      all(row["revoked_at"] is not None for row in created["sessions"].values()), "New credentials are not exclusively owned and revoked")
                admin = dict(created["sessions"][api.admin_id])
                timestamp(admin["revoked_at"])
                admin["revoked_at"] = None
                check(admin == known_admin, "Administrator cleanup changed more than its revocation timestamp")
                check(set(created["application_keys"]) == {key.id for key in keys if key.id} and
                      {row["credential_id"] for row in created["application_keys"].values()} == parents,
                      "New application metadata escaped acknowledged parent keys")
                clients = {key.default for key in keys if key.default} | {context["id"] for key in keys for context in key.contexts}
                check(set(created["application_key_clients"]) == clients and
                      all(row["credential_id"] in parents for row in created["application_key_clients"].values()),
                      "New client contexts escaped acknowledged parent keys")
                check(set(created["play_sessions"]) == set(api.plays) and all(PLAY_ID.fullmatch(identifier) and row["user_id"] is None and
                      row["auth_session_id"] == api.plays[identifier]["key"].parent and row["application_client_id"] in clients and
                      row["item_id"] == api.item_id and row["state"] == "Stopped" and row["stopped_at"] is not None
                      for identifier, row in created["play_sessions"].items()), "New playbacks are not exclusively owned, userless, and stopped")
                if report["status"] == "passed":
                    check(len(parents) == 2 and len(created["sessions"]) == 3 and len(clients) == 6 and len(api.plays) == 2,
                          "The successful run did not retain its exact expected owned additions")
                report["all_preexisting_rows_raw_exact"] = True
                report["expired_prepared_row_raw_exact"] = True
                report["cleanup"].update({"proven": True, "new_credentials_revoked": len(created["sessions"]),
                                           "new_playbacks_stopped": len(api.plays), "old_catalog_metadata_user_state_jobs_unchanged": True})
            except BaseException:
                report["cleanup"]["errors"].append("Complete raw-row preservation or exclusive owned cleanup could not be proven")
        for label, observation in (("deployment", lambda: sessions.deployment(accepted) == deployed),
                                   ("vault", lambda: vault_identity(sessions) == vault),
                                   ("source", lambda: source_identity(item, before, configured) == media_file)):
            baseline = {"deployment": deployed, "vault": vault, "source": media_file}[label]
            if baseline is not None:
                try:
                    remaining()
                    check(observation(), "A final protected identity changed")
                    report[label + "_unchanged"] = True
                except BaseException:
                    report["cleanup"]["errors"].append("A protected deployment, vault, or source identity could not be proven unchanged")
        if vault is not None:
            report["vault"] = {"uid": 995, "mode": "0600", "bytes": 32, "directory_mode": "0700"}
        if not report["cleanup"]["proven"] or report["cleanup"]["errors"]:
            report["status"] = "failed"
        report["elapsed_seconds"] = round(time.monotonic() - STARTED, 3)
        if parent_identity is not None:
            try:
                info = ROOT.lstat()
                check((info.st_dev, info.st_ino) == parent_identity and info.st_uid == 0 and info.st_mode & 0o022 == 0,
                      "The fixed report directory changed")
                utilities.private_write(result_path, report)
            except BaseException:
                report = {"owner": OWNER, "status": "failed", "error": "The exclusive sanitized report could not be saved; no overwrite was attempted"}
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, 0)
        print(json.dumps(report, ensure_ascii=True, sort_keys=True))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
