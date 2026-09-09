#!/usr/bin/env python3
"""Capture bounded WebSocket evidence from the isolated official reference server.

Run on test-env in the goby-emby-reference service network namespace. This
standard-library-only extension uses reference-capture.py without modifying it.
Raw messages and credentials stay in the reference private directory.
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import secrets
import select
import socket
import struct
import time
import urllib.parse


SOURCE = Path(__file__).with_name("reference-capture.py")
if not SOURCE.exists():
    SOURCE = Path("/dev/shm/goby-emby-reference/reference-capture.py")
SPEC = importlib.util.spec_from_file_location("reference_capture", SOURCE)
BASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BASE)
PRIVATE = BASE.PRIVATE
EXPORT = BASE.EXPORT
CREDS = PRIVATE / "websocket-m3c-credentials.env"
BASELINE = PRIVATE / "websocket-m3c-baseline-hashes.json"
DEVICE = "goby-websocket-m3c-recorder"
PREFIX = "websocket-m3c-"
ADMIN_PREFIX = "websocket-admin-m3d-"
ADMIN_CREDS = PRIVATE / "websocket-admin-m3d-credentials.env"
ADMIN_BASELINE = PRIVATE / "websocket-admin-m3d-baseline-hashes.json"
ADMIN_DEVICE = "goby-websocket-admin-m3d-recorder"


def timestamp():
    return dt.datetime.now(dt.timezone.utc).isoformat()


class Recorder(BASE.Recorder):
    prefix = PREFIX

    def __init__(self):
        super().__init__()
        for path in PRIVATE.glob("*credentials.env"):
            self.secret_values.update(
                value for key, value in BASE.read_credentials(path).items()
                if key in {"REFERENCE_TOKEN", "REFERENCE_PASSWORD"} and value
            )

    def request(self, name, *args, **kwargs):
        self.unused(name)
        return super().request(name, *args, **kwargs)

    def unused(self, name):
        if not name.startswith(self.prefix):
            raise RuntimeError("Only owned WebSocket fixture names are permitted")
        if any((directory / (name + ".json")).exists() for directory in (PRIVATE / "raw", EXPORT)):
            raise RuntimeError("Refusing to overwrite existing evidence: " + name)

    def own(self):
        self.credentials = BASE.read_credentials(CREDS)
        self.credential_path = CREDS
        self.secret_values.update(value for key, value in self.credentials.items()
                                  if key in {"REFERENCE_TOKEN", "REFERENCE_PASSWORD"} and value)
        self.client_headers = {
            "Accept": "application/json",
            "Authorization": ('Emby Client="Goby WebSocket Recorder", Device="Linux WebSocket Test", '
                              f'DeviceId="{DEVICE}", Version="0.1.0"'),
        }

    def write(self, name, record):
        self.unused(name)
        cleaned = self.sanitize(record)
        self.audit_export(record, cleaned)
        BASE.private_write(PRIVATE / "raw" / (name + ".json"), json.dumps(record, indent=2) + "\n")
        BASE.private_write(EXPORT / (name + ".json"), json.dumps(cleaned, indent=2, ensure_ascii=False) + "\n")
        messages = [event.get("json", {}).get("MessageType") for event in record["events"]
                    if isinstance(event.get("json"), dict)]
        print(f"{name}: HTTP {record['response']['status']}, message types={messages}", flush=True)

    def setup(self):
        if CREDS.exists() or BASELINE.exists():
            raise RuntimeError("Dedicated WebSocket setup already exists")
        baseline = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                    for directory in (PRIVATE / "raw", EXPORT)
                    for path in sorted(directory.glob("*.json"))}
        BASE.private_write(BASELINE, json.dumps(baseline, indent=2) + "\n")
        credentials = {"REFERENCE_USERNAME": "reference-websocket-m3c", "REFERENCE_PASSWORD": secrets.token_hex(32)}
        BASE.save_credentials(credentials, CREDS)
        self.secret_values.add(credentials["REFERENCE_PASSWORD"])
        status, created = self.request(PREFIX + "user-create", "POST", "/emby/Users/New",
                                       body={"Name": credentials["REFERENCE_USERNAME"]}, authenticated=True)
        if status != 200 or not created.get("Id") or created.get("Policy", {}).get("IsAdministrator"):
            raise RuntimeError("Ordinary WebSocket user creation failed")
        credentials["REFERENCE_USER_ID"] = created["Id"]
        BASE.save_credentials(credentials, CREDS)
        status, _ = self.request(PREFIX + "user-password", "POST", f"/emby/Users/{created['Id']}/Password",
                                 body={"Id": created["Id"], "NewPw": credentials["REFERENCE_PASSWORD"], "ResetPassword": False},
                                 authenticated=True)
        if status not in {200, 204}:
            raise RuntimeError("Dedicated password setup failed")
        self.own()
        status, result = self.request(PREFIX + "user-login", "POST", "/emby/Users/AuthenticateByName",
                                      body={"Username": credentials["REFERENCE_USERNAME"], "Pw": credentials["REFERENCE_PASSWORD"]})
        if status != 200:
            raise RuntimeError("Dedicated login failed")
        self.credentials["REFERENCE_SESSION_ID"] = result["SessionInfo"]["Id"]
        BASE.save_credentials(self.credentials, CREDS)
        print(f"Preserved baseline hashes for {len(baseline)} prior raw/export files.", flush=True)

    def path(self, route="/embywebsocket", token="own", device=DEVICE):
        query = {}
        if token is not None:
            query["api_key"] = self.credentials["REFERENCE_TOKEN"] if token == "own" else token
        if device is not None:
            query["deviceId"] = device
        return route + ("?" + urllib.parse.urlencode(query) if query else "")

    def handshakes(self):
        self.own()
        for name, route in (("root", "/"), ("emby", "/emby"), ("emby-slash", "/emby/"),
                            ("sdk", "/embywebsocket"), ("emby-socket", "/emby/socket")):
            with Capture(self, "handshake-" + name, self.path(route)) as capture:
                capture.wait(1)
        for name, token, device in (("no-token", None, DEVICE), ("bad-token", "invalid-websocket-reference-token", DEVICE),
                                    ("no-device", "own", None), ("other-device", "own", DEVICE + "-other"),
                                    ("no-token-no-device", None, None)):
            with Capture(self, "auth-" + name, self.path(token=token, device=device)) as capture:
                capture.wait(1)
        with Capture(self, "initial-and-ping", self.path()) as capture:
            capture.wait(10)
            if capture.open:
                capture.send(9, b"goby-websocket-m3c-ping")
                capture.wait(2)

    def events(self):
        self.own()
        user_id = self.credentials["REFERENCE_USER_ID"]
        context = json.loads((PRIVATE / "playback-m3-context.json").read_text())
        item_id = context["ItemId"]
        detail = f"/emby/Users/{user_id}/Items/{item_id}"
        _, before = self.request(PREFIX + "favorite-before", "GET", detail, authenticated=True)
        if before.get("UserData", {}).get("IsFavorite") is not False:
            raise RuntimeError("Expected a new ordinary user's unmodified synthetic movie")
        captures = [Capture(self, "event-" + name, self.path(token=token, device=device))
                    for name, token, device in (("own-device", "own", DEVICE), ("no-device", "own", None),
                                                ("other-device", "own", DEVICE + "-other"),
                                                ("no-token", None, DEVICE),
                                                ("bad-token", "invalid-websocket-reference-token", DEVICE))]
        try:
            for capture in captures:
                capture.wait(0.2)
            self.request(PREFIX + "favorite-add", "POST", f"/emby/Users/{user_id}/FavoriteItems/{item_id}", authenticated=True)
            pump(captures, 3)
        finally:
            self.request(PREFIX + "favorite-restore", "DELETE", f"/emby/Users/{user_id}/FavoriteItems/{item_id}", authenticated=True)
            pump(captures, 3)
            for capture in captures:
                capture.close()
        self.request(PREFIX + "favorite-after", "GET", detail, authenticated=True)

    def subscriptions(self):
        self.own()
        with Capture(self, "sessions-subscription", self.path()) as capture:
            capture.wait(0.5)
            if capture.open:
                capture.message("SessionsStart", "1000,1000")
                capture.wait(3.5)
                capture.message("SessionsStop", "")
                capture.wait(2)

    def playback(self):
        self.own()
        user_id = self.credentials["REFERENCE_USER_ID"]
        context = json.loads((PRIVATE / "playback-m3-context.json").read_text())
        item_id = context["ItemId"]
        detail = f"/emby/Users/{user_id}/Items/{item_id}"
        session = f"/emby/Sessions?Id={self.credentials['REFERENCE_SESSION_ID']}"
        self.request(PREFIX + "playback-before", "GET", detail, authenticated=True)
        status, info = self.request(PREFIX + "playback-info", "POST", f"/emby/Items/{item_id}/PlaybackInfo",
                                    body={"UserId": user_id, "DeviceProfile": self.m3_profile(), "IsPlayback": True}, authenticated=True)
        if status != 200 or not info.get("MediaSources") or not info.get("PlaySessionId"):
            raise RuntimeError("Owned playback negotiation failed")
        started = {"ItemId": item_id, "MediaSourceId": info["MediaSources"][0]["Id"],
                   "PlaySessionId": info["PlaySessionId"], "SessionId": self.credentials["REFERENCE_SESSION_ID"],
                   "PositionTicks": 0, "CanSeek": True, "IsPaused": False, "IsMuted": False,
                   "VolumeLevel": 100, "PlayMethod": "DirectStream", "SubtitleStreamIndex": -1,
                   "AudioStreamIndex": info["MediaSources"][0].get("DefaultAudioStreamIndex", 1), "PlaybackRate": 1}
        with Capture(self, "playback-report-progress", self.path()) as capture:
            try:
                self.request(PREFIX + "playback-started", "POST", "/emby/Sessions/Playing", body=started, authenticated=True)
                capture.wait(1)
                self.request(PREFIX + "session-before-ws-progress", "GET", session, authenticated=True)
                capture.message("ReportPlaybackProgress", {**started, "PositionTicks": 1200000000, "EventName": "TimeUpdate"})
                capture.wait(2)
                self.request(PREFIX + "session-after-ws-progress", "GET", session, authenticated=True)
                self.request(PREFIX + "detail-after-ws-progress", "GET", detail, authenticated=True)
            finally:
                stopped = {key: started[key] for key in ("ItemId", "MediaSourceId", "PlaySessionId", "SessionId")}
                stopped.update({"PositionTicks": 0, "Failed": False, "IsAutomated": False})
                self.request(PREFIX + "playback-stopped", "POST", "/emby/Sessions/Playing/Stopped", body=stopped, authenticated=True)
                capture.wait(1)
        self.request(PREFIX + "playback-restore-unplayed", "DELETE", f"/emby/Users/{user_id}/PlayedItems/{item_id}", authenticated=True)
        self.request(PREFIX + "playback-after", "GET", detail, authenticated=True)

    def audit(self):
        baseline = json.loads(BASELINE.read_text())
        for name, expected in baseline.items():
            path = Path(name)
            if not path.exists() or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
                raise RuntimeError("Pre-existing evidence changed: " + path.name)
        paths = sorted(EXPORT.glob(PREFIX + "*.json"))
        for path in paths:
            raw = json.loads((PRIVATE / "raw" / path.name).read_text())
            cleaned = json.loads(path.read_text())
            self.audit_export(raw, cleaned)
            text = path.read_text()
            if any(secret in text for secret in self.secret_values):
                raise RuntimeError("Credential survived sanitization: " + path.name)
        print(f"Audited {len(paths)} WebSocket fixtures; preserved all {len(baseline)} original raw/export files.", flush=True)

    def playback_control(self):
        self.own()
        user_id = self.credentials["REFERENCE_USER_ID"]
        item_id = json.loads((PRIVATE / "playback-m3-context.json").read_text())["ItemId"]
        detail = f"/emby/Users/{user_id}/Items/{item_id}"
        session = f"/emby/Sessions?Id={self.credentials['REFERENCE_SESSION_ID']}"
        _, info = self.request(PREFIX + "control-info", "POST", f"/emby/Items/{item_id}/PlaybackInfo",
                               body={"UserId": user_id, "DeviceProfile": self.m3_profile(), "IsPlayback": True}, authenticated=True)
        started = {"ItemId": item_id, "MediaSourceId": info["MediaSources"][0]["Id"],
                   "PlaySessionId": info["PlaySessionId"], "SessionId": self.credentials["REFERENCE_SESSION_ID"],
                   "PositionTicks": 0, "CanSeek": True, "IsPaused": True, "IsMuted": False,
                   "VolumeLevel": 100, "PlayMethod": "DirectStream", "SubtitleStreamIndex": -1,
                   "AudioStreamIndex": 1, "PlaybackRate": 1}
        progress = {**started, "PositionTicks": 1200000000, "EventName": "TimeUpdate"}
        with Capture(self, "playback-paused-control", self.path()) as capture:
            try:
                self.request(PREFIX + "control-started", "POST", "/emby/Sessions/Playing", body=started, authenticated=True)
                capture.wait(0.5)
                self.request(PREFIX + "control-session-before", "GET", session, authenticated=True)
                capture.message("ReportPlaybackProgress", progress)
                capture.wait(1)
                self.request(PREFIX + "control-session-after-text", "GET", session, authenticated=True)
                capture.send(2, json.dumps({"MessageType": "ReportPlaybackProgress", "Data": progress}, separators=(",", ":")).encode())
                capture.wait(1)
                self.request(PREFIX + "control-session-after-binary", "GET", session, authenticated=True)
                self.request(PREFIX + "control-progress-http", "POST", "/emby/Sessions/Playing/Progress", body=progress, authenticated=True)
                capture.wait(0.5)
                self.request(PREFIX + "control-session-after-http", "GET", session, authenticated=True)
                self.request(PREFIX + "control-detail-after-http", "GET", detail, authenticated=True)
                capture.send(2, b'{"MessageType":"SessionsStart","Data":"1000,1000"}')
                capture.wait(3)
                capture.send(2, b'{"MessageType":"SessionsStop","Data":""}')
                capture.wait(0.5)
            finally:
                stopped = {key: started[key] for key in ("ItemId", "MediaSourceId", "PlaySessionId", "SessionId")}
                stopped.update({"PositionTicks": 0, "Failed": False, "IsAutomated": False})
                self.request(PREFIX + "control-stopped", "POST", "/emby/Sessions/Playing/Stopped", body=stopped, authenticated=True)
                capture.wait(0.5)
        self.request(PREFIX + "control-restore-unplayed", "DELETE", f"/emby/Users/{user_id}/PlayedItems/{item_id}", authenticated=True)
        self.request(PREFIX + "control-detail-after", "GET", detail, authenticated=True)


class AdminRecorder(Recorder):
    prefix = ADMIN_PREFIX

    def admin(self):
        self.credentials = BASE.read_credentials(ADMIN_CREDS)
        self.credential_path = ADMIN_CREDS
        self.secret_values.update(value for key, value in self.credentials.items()
                                  if key in {"REFERENCE_TOKEN", "REFERENCE_PASSWORD"} and value)
        self.client_headers = {
            "Accept": "application/json",
            "Authorization": ('Emby Client="Goby Admin WebSocket Recorder", Device="Linux Admin WebSocket Test", '
                              f'DeviceId="{ADMIN_DEVICE}", Version="0.1.0"'),
        }

    def admin_setup(self):
        if ADMIN_CREDS.exists() or ADMIN_BASELINE.exists():
            raise RuntimeError("Administrator WebSocket setup already exists")
        baseline = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                    for directory in (PRIVATE / "raw", EXPORT)
                    for path in sorted(directory.glob("*.json"))}
        BASE.private_write(ADMIN_BASELINE, json.dumps(baseline, indent=2) + "\n")
        original = BASE.read_credentials()
        credentials = {key: original[key] for key in ("REFERENCE_USERNAME", "REFERENCE_PASSWORD")}
        BASE.save_credentials(credentials, ADMIN_CREDS)
        self.admin()
        status, result = self.request(ADMIN_PREFIX + "admin-login", "POST", "/emby/Users/AuthenticateByName",
                                      body={"Username": credentials["REFERENCE_USERNAME"], "Pw": credentials["REFERENCE_PASSWORD"]})
        if status != 200 or result.get("User", {}).get("Policy", {}).get("IsAdministrator") is not True:
            raise RuntimeError("Dedicated administrator device login failed")
        self.credentials["REFERENCE_SESSION_ID"] = result["SessionInfo"]["Id"]
        BASE.save_credentials(self.credentials, ADMIN_CREDS)
        print(f"Preserved baseline hashes for {len(baseline)} prior raw/export files.", flush=True)

    def admin_subscriptions(self):
        self.admin()
        with Capture(self, "sessions-subscription", self.path(device=ADMIN_DEVICE)) as capture:
            capture.wait(0.5)
            capture.message("SessionsStart", "1000,1000")
            capture.wait(3)
            capture.message("SessionsStop", "")
            capture.wait(2)
            capture.send(2, b'{"MessageType":"SessionsStart","Data":"1000,1000"}')
            capture.wait(3)
            capture.send(2, b'{"MessageType":"SessionsStop","Data":""}')
            capture.wait(2)

    def remote_controls(self):
        self.own()
        target_user = self.credentials["REFERENCE_USER_ID"]
        target = self.credentials["REFERENCE_SESSION_ID"]
        query = "?Id=" + target
        status, sessions = self.request(ADMIN_PREFIX + "target-before", "GET", "/emby/Sessions" + query, authenticated=True)
        if status != 200 or len(sessions) != 1 or sessions[0].get("DeviceId") != DEVICE or sessions[0].get("UserId") != target_user:
            raise RuntimeError("Remote commands may only target the owned ordinary recorder device")
        before = sessions[0]
        BASE.private_write(PRIVATE / (ADMIN_PREFIX + "target-before.json"), json.dumps(before, indent=2) + "\n")
        target_socket = Capture(self, "remote-target", self.path())
        try:
            target_socket.wait(0.5)
            self.request(ADMIN_PREFIX + "target-connected", "GET", "/emby/Sessions" + query, authenticated=True)
            self.request(ADMIN_PREFIX + "capabilities-declare", "POST",
                         "/emby/Sessions/Capabilities" + query + "&PlayableMediaTypes=Video,Audio&SupportedCommands=SetVolume&SupportsMediaControl=true&SupportsSync=false",
                         authenticated=True)
            self.request(ADMIN_PREFIX + "target-declared", "GET", "/emby/Sessions" + query, authenticated=True)
            self.request(ADMIN_PREFIX + "controllable-own-user", "GET", "/emby/Sessions" + query + "&ControllableByUserId=" + target_user, authenticated=True)
            self.admin()
            admin_user = self.credentials["REFERENCE_USER_ID"]
            self.request(ADMIN_PREFIX + "controllable-admin", "GET", "/emby/Sessions" + query + "&ControllableByUserId=" + admin_user, authenticated=True)
            self.request(ADMIN_PREFIX + "pause-admin", "POST", f"/emby/Sessions/{target}/Playing/Pause", body={"Command": "Pause"}, authenticated=True)
            target_socket.wait(1)
            self.request(ADMIN_PREFIX + "volume-admin", "POST", f"/emby/Sessions/{target}/Command/SetVolume",
                         body={"Arguments": {"Volume": "37"}}, authenticated=True)
            target_socket.wait(1)
            self.request(ADMIN_PREFIX + "target-after-commands", "GET", "/emby/Sessions" + query, authenticated=True,
                         note="The recorder receives commands but does not apply or report playback state.")
            self.request(ADMIN_PREFIX + "undeclared-command", "POST", f"/emby/Sessions/{target}/Command/VolumeUp", body={}, authenticated=True,
                         note="VolumeUp was not in the target's explicitly declared SupportedCommands list; only the owned recorder is targeted.")
            target_socket.wait(1)
            other = BASE.read_credentials(BASE.SESSION_ENV_FILE)
            self.request(ADMIN_PREFIX + "controllable-other-user", "GET", "/emby/Sessions" + query + "&ControllableByUserId=" + other["REFERENCE_USER_ID"], authenticated=True)
            self.credentials = other
            self.credential_path = BASE.SESSION_ENV_FILE
            self.client_headers = {"Accept": "application/json", "Authorization": 'Emby Client="Goby Reference Recorder", Device="Linux Session Test", DeviceId="goby-session-m3b-recorder", Version="0.1.0"'}
            self.request(ADMIN_PREFIX + "other-user-policy", "GET", "/emby/Users/" + other["REFERENCE_USER_ID"], authenticated=True)
            self.request(ADMIN_PREFIX + "volume-other-user", "POST", f"/emby/Sessions/{target}/Command/SetVolume",
                         body={"Arguments": {"Volume": "38"}}, authenticated=True,
                         note="A distinct pre-existing ordinary test account targets only the dedicated recorder. No user policy is changed.")
            target_socket.wait(1)
            target_socket.close()
            self.admin()
            self.request(ADMIN_PREFIX + "target-disconnected", "GET", "/emby/Sessions" + query, authenticated=True)
            self.request(ADMIN_PREFIX + "pause-disconnected", "POST", f"/emby/Sessions/{target}/Playing/Pause", body={"Command": "Pause"}, authenticated=True,
                         note="The owned target WebSocket has been closed; its advertised capabilities have not yet been restored.")
        finally:
            target_socket.close()
            self.own()
            restore = {
                "Id": target,
                "PlayableMediaTypes": ",".join(before.get("PlayableMediaTypes", [])),
                "SupportedCommands": ",".join(before.get("SupportedCommands", [])),
                "SupportsMediaControl": str(before.get("Capabilities", {}).get("SupportsMediaControl", False)).lower(),
                "SupportsSync": str(before.get("SupportsSync", False)).lower(),
            }
            self.request(ADMIN_PREFIX + "capabilities-restore", "POST", "/emby/Sessions/Capabilities?" + urllib.parse.urlencode(restore), authenticated=True)
        self.admin()
        self.request(ADMIN_PREFIX + "target-restored", "GET", "/emby/Sessions" + query, authenticated=True)

    def admin_audit(self):
        baseline = json.loads(ADMIN_BASELINE.read_text())
        for name, expected in baseline.items():
            path = Path(name)
            if not path.exists() or hashlib.sha256(path.read_bytes()).hexdigest() != expected:
                raise RuntimeError("Pre-existing evidence changed: " + path.name)
        paths = sorted(EXPORT.glob(ADMIN_PREFIX + "*.json"))
        for path in paths:
            raw = json.loads((PRIVATE / "raw" / path.name).read_text())
            cleaned = json.loads(path.read_text())
            self.audit_export(raw, cleaned)
            if any(secret in path.read_text() for secret in self.secret_values):
                raise RuntimeError("Credential survived sanitization: " + path.name)
        print(f"Audited {len(paths)} administrator WebSocket fixtures; preserved all {len(baseline)} original raw/export files.", flush=True)

    def admin_command_body(self):
        self.own()
        target = self.credentials["REFERENCE_SESSION_ID"]
        query = "?Id=" + target
        self.request(ADMIN_PREFIX + "body-capabilities-declare", "POST",
                     "/emby/Sessions/Capabilities" + query + "&PlayableMediaTypes=Video,Audio&SupportedCommands=SetVolume&SupportsMediaControl=true&SupportsSync=false",
                     authenticated=True)
        try:
            with Capture(self, "command-body-target", self.path()) as capture:
                capture.wait(0.5)
                self.admin()
                self.request(ADMIN_PREFIX + "volume-general-body", "POST", f"/emby/Sessions/{target}/Command",
                             body={"Name": "SetVolume", "Arguments": {"Volume": "37"}}, authenticated=True,
                             note="Single complete GeneralCommand body control against the same owned recorder; it does not execute playback.")
                capture.wait(1)
        finally:
            self.own()
            self.request(ADMIN_PREFIX + "body-capabilities-restore", "POST",
                         "/emby/Sessions/Capabilities" + query + "&PlayableMediaTypes=&SupportedCommands=&SupportsMediaControl=false&SupportsSync=false",
                         authenticated=True)
        self.request(ADMIN_PREFIX + "body-target-restored", "GET", "/emby/Sessions" + query, authenticated=True)


class Capture:
    def __init__(self, recorder, name, path):
        self.recorder = recorder
        self.name = recorder.prefix + name
        recorder.unused(self.name)
        self.started = time.monotonic()
        self.buffer = bytearray()
        self.open = False
        self.closed = False
        self.sock = socket.create_connection(("127.0.0.1", 18097), timeout=10)
        key = base64.b64encode(secrets.token_bytes(16)).decode("ascii")
        headers = [("Host", "127.0.0.1:18097"), ("Upgrade", "websocket"), ("Connection", "Upgrade"),
                   ("Sec-WebSocket-Key", key), ("Sec-WebSocket-Version", "13")]
        request = f"GET {path} HTTP/1.1\r\n" + "".join(f"{k}: {v}\r\n" for k, v in headers) + "\r\n"
        self.record = {
            "reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": timestamp()},
            "request": {"method": "GET", "path": path, "headers": headers, "wireHeaders": request},
            "response": {}, "events": [],
            "observation": "Bounded RFC 6455 client. Times are client receipt/send times. Text payload and parsed JSON are both preserved. No compression or subprotocol was requested.",
        }
        self.sock.sendall(request.encode("ascii"))
        while b"\r\n\r\n" not in self.buffer:
            chunk = self.sock.recv(65536)
            if not chunk:
                raise RuntimeError("Server closed before HTTP response headers")
            self.buffer.extend(chunk)
            if len(self.buffer) > 131072:
                raise RuntimeError("Unexpectedly large handshake")
        head, rest = bytes(self.buffer).split(b"\r\n\r\n", 1)
        self.buffer = bytearray(rest)
        lines = head.decode("iso-8859-1").split("\r\n")
        status = int(lines[0].split(" ")[1])
        response_headers = [line.split(": ", 1) if ": " in line else line.split(":", 1) for line in lines[1:]]
        self.record["response"] = {"status": status, "statusLine": lines[0], "headers": response_headers,
                                    "wireHeaders": head.decode("iso-8859-1") + "\r\n\r\n"}
        if status == 101:
            expected = base64.b64encode(hashlib.sha1((key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11").encode()).digest()).decode()
            actual = next((value for field, value in response_headers if field.lower() == "sec-websocket-accept"), None)
            self.record["response"]["acceptMatchesRequestKey"] = actual == expected
            if actual != expected:
                raise RuntimeError("Invalid WebSocket accept header")
            self.open = True
        else:
            length = next((int(value) for field, value in response_headers if field.lower() == "content-length"), None)
            if length is not None:
                while len(self.buffer) < length:
                    chunk = self.sock.recv(min(65536, length - len(self.buffer)))
                    if not chunk:
                        break
                    self.buffer.extend(chunk)
            self.record["response"]["body"] = bytes(self.buffer).decode("utf-8", errors="replace")
            self.buffer.clear()

    def event(self, direction, opcode, payload, fin=True, masked=False):
        item = {"at": timestamp(), "elapsedMs": round((time.monotonic() - self.started) * 1000, 3),
                "direction": direction, "opcode": opcode, "fin": fin, "masked": masked,
                "payloadLength": len(payload)}
        if opcode in (1, 2):
            try:
                item["text"] = payload.decode("utf-8")
                try:
                    item["json"] = json.loads(item["text"])
                except json.JSONDecodeError:
                    pass
            except UnicodeDecodeError:
                item["payloadBase64"] = base64.b64encode(payload).decode()
        elif opcode == 8:
            item["closeCode"] = struct.unpack("!H", payload[:2])[0] if len(payload) >= 2 else None
            item["closeReason"] = payload[2:].decode("utf-8", errors="replace")
        else:
            item["payloadBase64"] = base64.b64encode(payload).decode()
        self.record["events"].append(item)

    def send(self, opcode, payload):
        if not self.open:
            return
        mask = secrets.token_bytes(4)
        size = len(payload)
        if size < 126:
            head = bytes([0x80 | opcode, 0x80 | size])
        elif size < 65536:
            head = bytes([0x80 | opcode, 0x80 | 126]) + struct.pack("!H", size)
        else:
            head = bytes([0x80 | opcode, 0x80 | 127]) + struct.pack("!Q", size)
        self.sock.sendall(head + mask + bytes(value ^ mask[index % 4] for index, value in enumerate(payload)))
        self.event("client-to-server", opcode, payload, masked=True)

    def message(self, name, data):
        self.send(1, json.dumps({"MessageType": name, "Data": data}, separators=(",", ":")).encode())

    def drain(self):
        while len(self.buffer) >= 2:
            first, second = self.buffer[:2]
            length, offset = second & 127, 2
            if length == 126:
                if len(self.buffer) < 4:
                    return
                length, offset = struct.unpack("!H", self.buffer[2:4])[0], 4
            elif length == 127:
                if len(self.buffer) < 10:
                    return
                length, offset = struct.unpack("!Q", self.buffer[2:10])[0], 10
            if length > 4 * 1024 * 1024:
                raise RuntimeError("Frame exceeds bounded capture limit")
            mask = self.buffer[offset:offset + 4] if second & 0x80 else None
            offset += 4 if mask is not None else 0
            if len(self.buffer) < offset + length:
                return
            payload = bytes(self.buffer[offset:offset + length])
            del self.buffer[:offset + length]
            if mask is not None:
                payload = bytes(value ^ mask[index % 4] for index, value in enumerate(payload))
            opcode = first & 15
            self.event("server-to-client", opcode, payload, bool(first & 0x80), mask is not None)
            if opcode == 9:
                self.send(10, payload)
            elif opcode == 8:
                self.send(8, payload)
                self.open = False

    def wait(self, seconds):
        if not 0 <= seconds <= 10:
            raise ValueError("Every observation window must be at most 10 seconds")
        until = time.monotonic() + seconds
        while self.open:
            self.drain()
            remaining = until - time.monotonic()
            if not self.open or remaining <= 0:
                break
            if not select.select([self.sock], [], [], remaining)[0]:
                break
            chunk = self.sock.recv(65536)
            if not chunk:
                self.open = False
                self.record["events"].append({"at": timestamp(), "direction": "server-to-client", "transport": "eof"})
                break
            self.buffer.extend(chunk)
        self.drain()

    def close(self):
        if self.closed:
            return
        if self.open:
            self.send(8, struct.pack("!H", 1000))
            self.wait(0.3)
        self.record["observedDurationMs"] = round((time.monotonic() - self.started) * 1000, 3)
        self.record["unparsedBufferedBytes"] = len(self.buffer)
        self.sock.close()
        self.closed = True
        self.recorder.write(self.name, self.record)

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()


def pump(captures, seconds):
    until = time.monotonic() + seconds
    while time.monotonic() < until:
        for capture in captures:
            capture.wait(min(0.1, max(0, until - time.monotonic())))
        if not any(capture.open for capture in captures):
            break


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("stage", choices=("setup", "handshakes", "events", "subscriptions", "playback", "playback_control", "audit",
                                         "admin_setup", "admin_subscriptions", "remote_controls", "admin_command_body", "admin_audit"))
    stage = parser.parse_args().stage
    recorder = AdminRecorder() if stage.startswith("admin_") or stage == "remote_controls" else Recorder()
    getattr(recorder, stage)()


if __name__ == "__main__":
    main()
