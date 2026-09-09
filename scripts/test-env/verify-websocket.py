#!/usr/bin/env python3
"""Verify deployed Goby WebSocket notifications and remote commands via SSH.

Run only after the WebSocket service has been deployed on Linux test-env. The
standard-library RFC 6455 client adapts the bounded reference recorder without
writing raw messages or credential URLs. Existing owned media is read-only.
New login sessions are revoked, capabilities are reset, and favorite state is
restored. One reusable isolation account is recorded in a root-only state file.
"""

from __future__ import annotations

import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import select
import socket
import struct
import sys
import tempfile
import time
from urllib.parse import quote, urlencode


sys.dont_write_bytecode = True
try:
    spec = importlib.util.spec_from_file_location(
        "goby_direct_smoke", Path(__file__).with_name("verify-direct-playback.py"))
    smoke = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(smoke)
except Exception:
    print(json.dumps({"status": "failed", "failed_stage": "helper import",
                      "error": "The sibling direct-playback helper is unavailable", "cleanup_errors": []}))
    raise SystemExit(1) from None

RECORD = Path("/opt/goby-test/websocket-goby-verification.json")
OWNER = "goby-websocket-verification-v1"
ROUTES = ("/", "/emby", "/emby/", "/embywebsocket", "/emby/socket")
FRAME_LIMIT = 1024 * 1024
EVENT_LIMIT = 1024
EVENT_TYPES = {"GeneralCommand", "Playstate", "Play"}


def check(condition: bool, label: str) -> None:
    smoke.check(condition, label)


class Client(smoke.API):
    def __init__(self, device: str) -> None:
        super().__init__()
        self.device = device
        self.revoked = False
        self.capabilities_changed = False

    def request(self, method: str, path: str, **options):
        if options.get("emby"):
            headers = dict(options.get("headers") or {})
            headers["Authorization"] = (
                'Emby Client="Goby WebSocket Verification", Device="Linux Test", '
                f'DeviceId="{self.device}", Version="0.1.0"'
            )
            options["headers"] = headers
        return super().request(method, path, **options)

    def login(self, name: str, password: str, *, user_id="", administrator=False) -> None:
        result = self.request("POST", "/emby/Users/AuthenticateByName", emby=True,
                              label="Owned WebSocket client login", body={"Username": name, "Pw": password})
        self.token = result.get("AccessToken", "")
        self.user_id = result.get("User", {}).get("Id", "")
        self.session_id = result.get("SessionInfo", {}).get("Id", "")
        check(bool(self.token and self.user_id and self.session_id), "Owned login omitted authentication fields")
        check(not user_id or self.user_id == user_id, "Owned login resolved a different account")
        check(result.get("User", {}).get("Policy", {}).get("IsAdministrator") is administrator,
              "Owned login administrator policy does not match")

    def capabilities(self, enabled: bool) -> None:
        body = {"PlayableMediaTypes": ["Video", "Audio"], "SupportedCommands": ["SetVolume", "Pause"],
                "SupportsMediaControl": True, "SupportsSync": False} if enabled else {}
        # Login always creates a new session with an empty capability snapshot.
        # Only these newly issued sessions are changed or reset by this script.
        self.capabilities_changed = True
        self.request("POST", "/emby/Sessions/Capabilities/Full", emby=True, body=body,
                     expected=(204,), parse=False, label="Owned client capability declaration")
        if not enabled:
            self.capabilities_changed = False

    def logout(self) -> None:
        if self.token and not self.revoked:
            self.request("POST", "/emby/Sessions/Logout", emby=True, parse=False,
                         label="Owned WebSocket client logout")
            self.revoked = True


def save_record(value: dict, *, create=False) -> None:
    if create:
        descriptor = os.open(RECORD, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        return
    smoke.private_file(RECORD)
    descriptor, temporary = tempfile.mkstemp(prefix=".goby-websocket-state-", dir=RECORD.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, RECORD)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def isolation_account(admin: Client, isolated: Client, owned) -> dict:
    name = "Goby WebSocket isolation " + owned.state["nonce"][:16]
    users = admin.request("GET", "/admin/v1/users", admin=True, label="Owned isolation account lookup")["Items"]
    matching = [entry for entry in users if entry.get("Name") == name]
    check(len(matching) <= 1, "Owned isolation account lookup is ambiguous")
    if RECORD.exists() or RECORD.is_symlink():
        record = smoke.bounded_json(RECORD)
        check(record.get("owner") == OWNER and record.get("fixture_nonce") == owned.state["nonce"] and
              record.get("user_name") == name and isinstance(record.get("user_password"), str) and
              len(record["user_password"]) >= 32, "Private WebSocket ownership record does not match")
    else:
        check(not matching, "An unrecorded account already uses the isolation account name")
        record = {"owner": OWNER, "fixture_nonce": owned.state["nonce"], "user_name": name,
                  "user_password": secrets.token_urlsafe(32), "user_id": "", "creation_pending": True}
        save_record(record, create=True)
    if matching:
        check((record.get("user_id") == matching[0].get("Id")) or
              (not record.get("user_id") and record.get("creation_pending") is True),
              "Existing isolation account has no matching ownership record")
        user = matching[0]
    else:
        check(not record.get("user_id") and record.get("creation_pending") is True,
              "Previously recorded isolation account is missing")
        user = admin.request("POST", "/admin/v1/users", admin=True, expected=(201,),
                             label="Owned isolation account creation", body={"Name": record["user_name"],
                             "Password": record["user_password"], "IsAdministrator": False})["User"]
    check(user.get("IsAdministrator") is False and user.get("IsDisabled") is False,
          "Isolation account must be an enabled non-administrator")
    isolated.login(record["user_name"], record["user_password"], user_id=user["Id"])
    record["user_id"], record["creation_pending"] = user["Id"], False
    save_record(record)
    return record


class WebSocket:
    def __init__(self, route: str, token: str | None, device: str, *, expected=101) -> None:
        check(route in ROUTES, "WebSocket target must use an implemented upgrade path")
        self.sock = None
        self.open = False
        self.closed = False
        self.close_received = False
        self.eof = False
        self.close_sent = False
        self.buffer = bytearray()
        self.fragment = bytearray()
        self.fragment_opcode = None
        self.messages = []
        self.pongs = []
        self.received_bytes = 0
        parameters = {"deviceId": device}
        if token is not None:
            parameters["api_key"] = token
        target = route + "?" + urlencode(parameters)
        key = base64.b64encode(secrets.token_bytes(16)).decode("ascii")
        request = (f"GET {target} HTTP/1.1\r\nHost: 127.0.0.1:18096\r\n"
                   "Upgrade: websocket\r\nConnection: Upgrade\r\n"
                   f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n")
        try:
            self.sock = socket.create_connection(("127.0.0.1", 18096), timeout=5)
            self.sock.sendall(request.encode("ascii"))
            deadline = time.monotonic() + 5
            while b"\r\n\r\n" not in self.buffer:
                remaining = deadline - time.monotonic()
                check(remaining > 0, "WebSocket upgrade exceeded its time limit")
                self.sock.settimeout(remaining)
                chunk = self.sock.recv(16_384)
                check(bool(chunk), "WebSocket connection ended before response headers")
                self.buffer.extend(chunk)
                check(len(self.buffer) <= 65_536, "WebSocket response headers exceed their size limit")
            head, rest = bytes(self.buffer).split(b"\r\n\r\n", 1)
            self.buffer = bytearray(rest)
            lines = head.decode("iso-8859-1").split("\r\n")
            status = int(lines[0].split(" ")[1])
            check(status == expected, f"WebSocket upgrade returned unexpected HTTP {status}")
            if status != 101:
                self.sock.close()
                self.closed = True
                return
            headers = {}
            for line in lines[1:]:
                field, separator, value = line.partition(":")
                check(bool(separator), "WebSocket upgrade returned malformed headers")
                headers[field.lower()] = value.strip()
            expected_accept = base64.b64encode(hashlib.sha1(
                (key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode("ascii")).digest()).decode("ascii")
            check(headers.get("sec-websocket-accept") == expected_accept and
                  headers.get("upgrade", "").lower() == "websocket" and
                  "upgrade" in [value.strip() for value in headers.get("connection", "").lower().split(",")],
                  "WebSocket upgrade did not satisfy the RFC 6455 accept contract")
            self.sock.settimeout(5)
            self.open = True
            self.drain()
        except Exception:
            if self.sock is not None:
                self.sock.close()
            self.closed = True
            raise

    def send(self, opcode: int, payload: bytes) -> None:
        check(self.open and not self.closed, "Cannot send on a closed WebSocket")
        check(len(payload) <= FRAME_LIMIT and (opcode < 8 or len(payload) <= 125),
              "Client WebSocket frame exceeds its size limit")
        mask = secrets.token_bytes(4)
        size = len(payload)
        if size < 126:
            head = bytes([0x80 | opcode, 0x80 | size])
        elif size < 65_536:
            head = bytes([0x80 | opcode, 0x80 | 126]) + struct.pack("!H", size)
        else:
            head = bytes([0x80 | opcode, 0x80 | 127]) + struct.pack("!Q", size)
        self.sock.sendall(head + mask + bytes(value ^ mask[index % 4] for index, value in enumerate(payload)))
        if opcode == 8:
            self.close_sent = True

    def application_message(self, payload: bytes) -> None:
        message = json.loads(payload.decode("utf-8"))
        check(isinstance(message, dict), "WebSocket application messages must be JSON objects")
        self.messages.append(message)
        check(len(self.messages) <= EVENT_LIMIT, "WebSocket observation exceeded its message limit")

    def drain(self) -> None:
        while self.open and len(self.buffer) >= 2:
            first, second = self.buffer[:2]
            check(first & 0x70 == 0 and second & 0x80 == 0,
                  "Server WebSocket frames must be unmasked without unnegotiated extensions")
            opcode, final = first & 15, bool(first & 0x80)
            length, offset = second & 127, 2
            if length == 126:
                if len(self.buffer) < 4:
                    return
                length, offset = struct.unpack("!H", self.buffer[2:4])[0], 4
            elif length == 127:
                if len(self.buffer) < 10:
                    return
                length, offset = struct.unpack("!Q", self.buffer[2:10])[0], 10
            check(length <= FRAME_LIMIT and (opcode < 8 or (final and length <= 125)),
                  "Server WebSocket frame exceeded its bound or control-frame contract")
            if len(self.buffer) < offset + length:
                return
            payload = bytes(self.buffer[offset:offset + length])
            del self.buffer[:offset + length]
            if opcode == 9:
                self.send(10, payload)
            elif opcode == 10:
                self.pongs.append(payload)
                check(len(self.pongs) <= EVENT_LIMIT, "WebSocket pong count exceeded its limit")
            elif opcode == 8:
                self.close_received = True
                if not self.close_sent:
                    self.send(8, payload)
                self.open = False
            elif opcode in (1, 2):
                check(self.fragment_opcode is None, "Server started a message before finishing a fragmented message")
                if final:
                    self.application_message(payload)
                else:
                    self.fragment_opcode, self.fragment = opcode, bytearray(payload)
            elif opcode == 0:
                check(self.fragment_opcode is not None, "Server sent an unexpected continuation frame")
                self.fragment.extend(payload)
                check(len(self.fragment) <= FRAME_LIMIT, "Fragmented WebSocket message exceeded its size limit")
                if final:
                    self.application_message(bytes(self.fragment))
                    self.fragment_opcode, self.fragment = None, bytearray()
            else:
                check(False, "Server sent an unsupported WebSocket opcode")

    def receive(self) -> None:
        try:
            chunk = self.sock.recv(65_536)
        except ConnectionResetError:
            self.eof, self.open = True, False
            return
        if not chunk:
            self.eof, self.open = True, False
            return
        self.received_bytes += len(chunk)
        check(self.received_bytes <= 4 * FRAME_LIMIT, "WebSocket observation exceeded its total byte limit")
        self.buffer.extend(chunk)
        self.drain()

    def close(self) -> None:
        if self.closed:
            return
        try:
            if self.open:
                self.send(8, struct.pack("!H", 1000))
                pump([self], 0.2)
        finally:
            self.open, self.closed = False, True
            if self.sock is not None:
                self.sock.close()


def pump(connections: list[WebSocket], seconds: float, until=None) -> None:
    check(0 <= seconds <= 10, "WebSocket observation windows must be bounded to ten seconds")
    deadline = time.monotonic() + seconds
    while True:
        for connection in connections:
            connection.drain()
        if until is not None and until():
            return
        remaining = deadline - time.monotonic()
        active = {connection.sock: connection for connection in connections if connection.open and not connection.closed}
        if remaining <= 0 or not active:
            return
        ready = select.select(list(active), [], [], min(remaining, 0.2))[0]
        for sock in ready:
            active[sock].receive()


def fresh_messages(connections: list[WebSocket]) -> None:
    pump(connections, 0.15)
    for connection in connections:
        connection.messages.clear()


def envelope(connection: WebSocket, message_type: str, predicate) -> dict | None:
    for message in connection.messages:
        if message.get("MessageType") == message_type and predicate(message.get("Data")):
            return message
    return None


def wait_message(connections, target, message_type, predicate, label) -> dict:
    pump(connections, 6, lambda: envelope(target, message_type, predicate) is not None)
    found = envelope(target, message_type, predicate)
    check(found is not None and isinstance(found.get("MessageId"), str) and bool(found["MessageId"]),
          label + ": expected WebSocket envelope did not arrive with a MessageId")
    return found


def state_change(message_data, user_id: str, item_id: str, favorite: bool) -> bool:
    return isinstance(message_data, dict) and message_data.get("UserId") == user_id and any(
        entry.get("ItemId") == item_id and entry.get("IsFavorite") is favorite
        for entry in message_data.get("UserDataList", []) if isinstance(entry, dict))


def favorite(client: Client, item_id: str, value: bool) -> dict:
    return client.request("POST" if value else "DELETE",
                          f"/emby/Users/{quote(client.user_id)}/FavoriteItems/{quote(item_id)}", emby=True,
                          label="Owned fixture favorite mutation")


def user_data(client: Client, item_id: str) -> dict:
    return client.request("GET", f"/emby/Users/{quote(client.user_id)}/Items/{quote(item_id)}",
                          emby=True, label="Owned fixture user state")["UserData"]


def session(client: Client, target: Client) -> dict:
    result = client.request("GET", "/emby/Sessions?" + urlencode({"Id": target.session_id,
                            "ActiveWithinSeconds": 0}), emby=True, label="Owned session DTO")
    check(isinstance(result, list) and len(result) == 1 and result[0].get("Id") == target.session_id and
          result[0].get("UserId") == target.user_id and result[0].get("DeviceId") == target.device,
          "Owned session lookup did not return the expected login and device")
    return result[0]


def remote_state(client: Client, target: Client, enabled: bool) -> dict:
    deadline = time.monotonic() + 5
    while True:
        result = session(client, target)
        if result.get("SupportsRemoteControl") is enabled:
            return result
        check(time.monotonic() < deadline, "Session remote-control capability did not follow live connection state")
        time.sleep(0.1)


def send_command(actor: Client, target: Client, suffix: str, body, *, expected=(204,)) -> None:
    actor.request("POST", f"/emby/Sessions/{quote(target.session_id)}/{suffix}", emby=True,
                  body=body, expected=expected, parse=False, label="Owned remote command")


def verify_command(message: dict, actor: Client, target: Client, *, target_id: bool) -> dict:
    data = message.get("Data")
    check(isinstance(data, dict) and data.get("ControllingUserId") == actor.user_id and
          (data.get("Id") == target.session_id if target_id else "Id" not in data),
          "Remote command envelope did not bind the authenticated controller and expected target")
    return data


def target_only(connections: list[WebSocket], target: WebSocket) -> None:
    pump(connections, 0.2)
    check(not any(message.get("MessageType") in EVENT_TYPES for connection in connections if connection is not target
                  for message in connection.messages), "Remote command leaked to a different target session")


def main() -> int:
    os.umask(0o077)
    owned = smoke.OwnedFixture()
    primary = Client("goby-websocket-primary")
    sibling = Client("goby-websocket-sibling")
    isolated = Client("goby-websocket-isolated")
    admin = Client("goby-websocket-administrator")
    clients = [primary, sibling, isolated, admin]
    connections = []
    restore = {}
    dirty_users = set()
    isolation_record_ready = False
    summary = {"status": "failed", "assertions": [], "cleanup_errors": []}
    item_id = ""
    media_path = None
    original_hash = ""
    stage = "preconditions"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
              "Run as root through SSH only on the authorized Linux test-env host")
        check((smoke.DIRECTORY / smoke.STATE_NAME).is_file(), "The reusable direct-playback fixture is not prepared")
        owned.open()
        check(bool(owned.state.get("user_id") and owned.state.get("library_id")), "Owned fixture account or library is missing")
        media_path = smoke.DIRECTORY / smoke.MEDIA_NAME
        check(media_path.is_file() and not media_path.is_symlink() and media_path.stat().st_size <= smoke.MAX_MEDIA,
              "Owned media is unavailable or exceeds its size limit")
        original_hash = smoke.digest(media_path)
        check(original_hash == owned.state.get("media_sha256"), "Owned media differs from its recorded bytes")
        credentials = smoke.credentials()
        admin.request("GET", "/readyz", label="Deployed Goby readiness")
        admin.admin_login(credentials)
        for client in (primary, sibling):
            client.login(owned.state["user_name"], owned.state["user_password"], user_id=owned.state["user_id"])
        check(primary.session_id != sibling.session_id and primary.token != sibling.token,
              "Sibling viewers must use distinct authentication sessions")
        isolation_account(admin, isolated, owned)
        isolation_record_ready = True
        check(isolated.user_id != primary.user_id, "Isolation control requires a different ordinary account")
        admin.login(credentials["GOBY_SMOKE_NAME"], credentials["GOBY_SMOKE_PASSWORD"], administrator=True)
        libraries = admin.request("GET", "/admin/v1/libraries", admin=True, label="Existing fixture library ownership")["Items"]
        matching = [entry for entry in libraries if entry.get("Id") == owned.state["library_id"]]
        check(len(matching) == 1 and matching[0].get("Name") == owned.state["library_name"] and
              matching[0].get("Paths") == [str(smoke.DIRECTORY)], "Existing fixture library ownership changed")
        query = urlencode({"ParentId": owned.state["library_id"], "Recursive": "true", "IncludeItemTypes": "Movie",
                           "Fields": "Path", "Limit": 10})
        listed = primary.request("GET", f"/emby/Users/{quote(primary.user_id)}/Items?" + query,
                                 emby=True, label="Existing owned movie lookup")
        check(listed.get("TotalRecordCount") == 1 and len(listed.get("Items", [])) == 1 and
              listed["Items"][0].get("Path") == str(media_path), "Existing owned movie lookup did not match")
        item_id = listed["Items"][0]["Id"]
        for client in (primary, isolated):
            before = user_data(client, item_id)
            check(type(before.get("IsFavorite")) is bool, "Owned fixture favorite state is invalid")
            restore[client.user_id] = before

        stage = "RFC 6455 upgrade routes and authentication"
        for route in ROUTES:
            connection = WebSocket(route, primary.token, primary.device)
            connections.append(connection)
            connection.close()
        for token in (None, "invalid-goby-websocket-token"):
            connections.append(WebSocket("/embywebsocket", token, primary.device, expected=401))
        remote_state(primary, sibling, False)
        summary["assertions"].append({"stage": stage, "authenticated_upgrade_paths": len(ROUTES),
                                      "upgrade_status": 101, "missing_and_invalid_token": 401})

        stage = "live sessions and capability declarations"
        first = WebSocket("/embywebsocket", primary.token, primary.device)
        connections.append(first)
        second = WebSocket("/emby/socket", sibling.token, sibling.device)
        connections.append(second)
        outsider = WebSocket("/emby/", isolated.token, isolated.device)
        connections.append(outsider)
        live = [first, second, outsider]
        remote_state(primary, sibling, False)
        sibling.capabilities(True)
        dto = remote_state(primary, sibling, True)
        check(dto.get("SupportedCommands") == ["SetVolume", "Pause"] and
              dto.get("PlayableMediaTypes") == ["Video", "Audio"], "Live session lost its declared capabilities")
        remote_state(primary, primary, False)
        first.send(9, b"goby-websocket-live-ping")
        pump(live, 5, lambda: b"goby-websocket-live-ping" in first.pongs)
        check(b"goby-websocket-live-ping" in first.pongs, "The live WebSocket did not answer a protocol ping")
        summary["assertions"].append({"stage": stage, "live_without_media_control": False,
                                      "live_with_media_control": True, "rfc6455_ping_pong": True})

        stage = "ordinary user state events and account isolation"
        fresh_messages(live)
        new_favorite = not restore[primary.user_id]["IsFavorite"]
        dirty_users.add(primary.user_id)
        changed = favorite(primary, item_id, new_favorite)
        predicate = lambda data: state_change(data, primary.user_id, item_id, new_favorite)
        event = wait_message(live, first, "UserDataChanged", predicate, "Primary state notification")
        duplicate = wait_message(live, second, "UserDataChanged", predicate, "Sibling state notification")
        check(event["MessageId"] == duplicate["MessageId"] and event["Data"] == duplicate["Data"],
              "Same-user WebSocket sessions did not receive the same state event identity and payload")
        row = next(entry for entry in event["Data"]["UserDataList"] if entry.get("ItemId") == item_id)
        check(all(row.get(key) == value for key, value in changed.items()) and user_data(primary, item_id) == changed,
              "UserDataChanged did not contain the newly committed HTTP user state")
        pump(live, 1)
        check(not any(message.get("MessageType") == "UserDataChanged" and
                      message.get("Data", {}).get("UserId") == primary.user_id for message in outsider.messages),
              "A different account received the primary user's state event")
        fresh_messages(live)
        isolated_favorite = not restore[isolated.user_id]["IsFavorite"]
        dirty_users.add(isolated.user_id)
        favorite(isolated, item_id, isolated_favorite)
        wait_message(live, outsider, "UserDataChanged",
                     lambda data: state_change(data, isolated.user_id, item_id, isolated_favorite), "Isolation account state notification")
        pump(live, 1)
        check(not any(message.get("MessageType") == "UserDataChanged" and
                      message.get("Data", {}).get("UserId") == isolated.user_id
                      for connection in (first, second) for message in connection.messages),
              "Primary account connections received another user's state event")
        for client in (primary, isolated):
            favorite(client, item_id, restore[client.user_id]["IsFavorite"])
            check(user_data(client, item_id) == restore[client.user_id], "Owned favorite state did not restore exactly")
            dirty_users.discard(client.user_id)
        summary["assertions"].append({"stage": stage, "ordinary_http_mutation_notified": True,
                                      "committed_state_matches": True, "sibling_message_id_shared": True,
                                      "different_user_isolated_both_directions": True})

        stage = "same-user and administrator remote delivery"
        fresh_messages(live)
        send_command(primary, sibling, "Command", {"Name": "SetVolume", "Arguments": {"Volume": "37"}})
        message = wait_message(live, second, "GeneralCommand",
                               lambda data: isinstance(data, dict) and data.get("Name") == "SetVolume", "SetVolume delivery")
        data = verify_command(message, primary, sibling, target_id=True)
        check(data.get("Arguments") == {"Volume": "37"}, "Full GeneralCommand did not preserve string arguments")
        target_only(live, second)
        fresh_messages(live)
        send_command(primary, sibling, "Command/SetVolume", {"Arguments": {"Volume": "99"}})
        message = wait_message(live, second, "GeneralCommand",
                               lambda data: isinstance(data, dict) and data.get("Name") == "SetVolume", "Named command delivery")
        check(verify_command(message, primary, sibling, target_id=False).get("Arguments") == {},
              "Named GeneralCommand did not retain its empty-arguments contract")
        target_only(live, second)
        fresh_messages(live)
        send_command(admin, sibling, "Playing/Pause", {"Command": "Pause"})
        message = wait_message(live, second, "Playstate",
                               lambda data: isinstance(data, dict) and data.get("Command") == "Pause", "Administrator Pause delivery")
        verify_command(message, admin, sibling, target_id=True)
        target_only(live, second)
        fresh_messages(live)
        send_command(primary, sibling, "Playing", {"ItemIds": [item_id], "PlayCommand": "PlayNow", "StartPositionTicks": 0})
        message = wait_message(live, second, "Play",
                               lambda data: isinstance(data, dict) and data.get("ItemIds") == [item_id], "Play request delivery")
        data = verify_command(message, primary, sibling, target_id=False)
        check(data.get("PlayCommand") == "PlayNow" and data.get("StartPositionTicks") == 0,
              "Play request did not preserve its playback command and starting position")
        target_only(live, second)
        check("NowPlayingItem" not in session(primary, sibling),
              "Receiving a remote command fabricated an unreported playback state")
        summary["assertions"].append({"stage": stage, "full_set_volume": "37", "named_arguments_empty": True,
                                      "administrator_pause_delivered": True, "play_request_delivered": True,
                                      "http_command_status": 204, "target_session_only": True})

        stage = "unauthorized controls and offline delivery"
        fresh_messages(live)
        send_command(isolated, sibling, "Command", {"Name": "SetVolume", "Arguments": {"Volume": "42"}}, expected=(404,))
        pump(live, 0.75)
        check(not any(message.get("MessageType") in EVENT_TYPES for connection in live for message in connection.messages),
              "Unauthorized account command reached a WebSocket")
        second.close()
        remote_state(primary, sibling, False)
        fresh_messages([first, outsider])
        send_command(primary, sibling, "Command", {"Name": "SetVolume", "Arguments": {"Volume": "91"}})
        pump([first, outsider], 0.5)
        check(not any(message.get("MessageType") in EVENT_TYPES for connection in (first, outsider)
                      for message in connection.messages), "Offline target command reached another WebSocket")
        second = WebSocket("/emby/socket", sibling.token, sibling.device)
        connections.append(second)
        live = [first, second, outsider]
        pump(live, 1)
        check(not any(message.get("MessageType") in EVENT_TYPES for message in second.messages),
              "An offline command was replayed after the target reconnected")
        remote_state(primary, sibling, True)
        sibling.capabilities(False)
        remote_state(primary, sibling, False)
        sibling.capabilities(True)
        remote_state(primary, sibling, True)
        summary["assertions"].append({"stage": stage, "other_account_rejected": 404,
                                      "offline_http_status": 204, "offline_capability": False,
                                      "offline_commands_not_replayed": True, "live_capability_toggle_applied": True})

        stage = "logout revokes all matching sockets and token"
        extra = WebSocket("/", primary.token, primary.device)
        connections.append(extra)
        primary.logout()
        pump([first, extra, second, outsider], 6, lambda: not first.open and not extra.open)
        check(not first.open and not extra.open and (first.close_received or first.eof) and
              (extra.close_received or extra.eof), "Logout did not disconnect every socket owned by its authentication session")
        check(second.open and outsider.open, "Logout disconnected another authentication session")
        connections.append(WebSocket("/embywebsocket", primary.token, primary.device, expected=401))
        primary.request("GET", "/emby/Sessions", emby=True, expected=(401,), parse=False,
                        label="Revoked token HTTP authentication")
        revoked = admin.request("GET", "/emby/Sessions?" + urlencode({"Id": primary.session_id, "ActiveWithinSeconds": 0}),
                                emby=True, label="Revoked session visibility")
        check(revoked == [], "Logged-out authentication session remained visible in session DTOs")
        remote_state(sibling, sibling, True)
        second.send(9, b"goby-sibling-still-live")
        pump([second, outsider], 5, lambda: b"goby-sibling-still-live" in second.pongs)
        check(b"goby-sibling-still-live" in second.pongs, "Sibling session did not remain usable after logout")
        summary["assertions"].append({"stage": stage, "same_session_sockets_disconnected": 2,
                                      "revoked_websocket_and_http_token": 401, "revoked_session_hidden": True,
                                      "sibling_session_preserved": True})
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
    finally:
        for user_id in dirty_users:
            try:
                before = restore[user_id]
                client = next(client for client in (sibling, primary, isolated)
                              if client.user_id == user_id and client.token and not client.revoked)
                favorite(client, item_id, before["IsFavorite"])
                check(user_data(client, item_id) == before, "Owned user state did not restore exactly")
            except Exception:
                summary["cleanup_errors"].append("Owned favorite state restoration failed")
        for client in clients:
            if client.token and not client.revoked and client.capabilities_changed:
                try:
                    client.capabilities(False)
                except Exception:
                    summary["cleanup_errors"].append("Owned capability restoration failed")
        for connection in connections:
            try:
                connection.close()
            except Exception:
                summary["cleanup_errors"].append("Owned WebSocket closure failed")
        for client in clients:
            try:
                client.logout()
            except Exception:
                summary["cleanup_errors"].append("Owned login session revocation failed")
        if admin.cookie:
            try:
                admin.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False,
                              label="WebSocket verification administrator logout")
            except Exception:
                summary["cleanup_errors"].append("Administrator cookie session revocation failed")
        if media_path is not None and original_hash:
            try:
                check(smoke.digest(media_path) == original_hash, "Owned media bytes changed")
                summary["owned_media_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned media preservation check failed")
        if isolation_record_ready:
            try:
                smoke.private_file(RECORD)
                summary["private_isolation_account_record_mode"] = "0600"
            except Exception:
                summary["cleanup_errors"].append("Private isolation account record check failed")
        if owned.lock is not None:
            owned.lock.close()
        if restore and not summary["cleanup_errors"]:
            summary["owned_user_states_restored"] = True
            summary["capabilities_reset_and_login_sessions_revoked"] = True
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
