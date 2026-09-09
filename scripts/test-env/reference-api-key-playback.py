#!/usr/bin/env python3
"""Capture bounded application-key playback contracts on the isolated reference.

Run only through root SSH on test-env, alongside reference-api-keys.py. The
shared module supplies hardened credential redaction and private file handling.
Only one uniquely named new user, one new key, and fresh logins may be mutated.
"""

from __future__ import annotations

import base64
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
from urllib.parse import parse_qs, quote, urlencode, urlsplit

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("key_reference", Path(__file__).with_name("reference-api-keys.py"))
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
base.__file__ = __file__
PREVIOUS = base.ROOT
base.ROOT = Path("/opt/goby-test/exec-scratch/keys-playback-m5d")
base.PRIVATE = base.ROOT / "private"
base.RAW = base.PRIVATE / "raw"
base.EXPORT = base.ROOT / "export"
base.PREFIX = "keys-playback-m5d-"
base.MARKER = "goby-reference-keys-playback-m5d-owned-v1"
base.APP_PREFIX = "Goby Keys Playback M5d 20260910 01 "
base.DEVICE = "goby-keys-playback-m5d-20260910-01"
base.MAX_TOTAL = 4 * 1024 * 1024
USER_NAME = "reference-keys-playback-m5d-20260910-01"


class Recorder(base.Recorder):
    def __init__(self, pid: int) -> None:
        self.user = None
        self.old_users = None
        self.pending_reports = []
        self.record_count = 0
        self.incomplete_count = 0
        self.cleanup_ok = False
        super().__init__(pid)

    def snapshot(self) -> dict:
        prior = json.loads((PREVIOUS / "private/baseline.json").read_text())
        paths = [Path(path) for path in prior["records"]]
        for folder in (PREVIOUS / "private/raw", PREVIOUS / "export"):
            paths.extend(folder.glob("*.json"))
        base.require(len(paths) == 2012 and len(set(paths)) == 2012,
                     "Expected all 1006 preceding raw/export pairs")
        media = [Path(path) for path in prior["media"]]
        base.require(sum(path.stat().st_size for path in media) < 32 * 1024 * 1024,
                     "Known source audit exceeds its bound")
        for path in [*paths, *media]:
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode),
                         "Preserved path is not a regular file")
        return {"records": {str(path): base.digest(path) for path in paths},
                "media": {str(path): base.digest(path) for path in media}}

    def mutation_allowed(self, method: str, path: str, token: str, body: dict | None, identity: str) -> None:
        if method == "GET":
            return
        parsed = urlsplit(path)
        route = parsed.path
        owned_user = self.user and "/emby/Users/" + self.user["Id"]
        allowed = False
        if route == "/emby/Users/AuthenticateByName":
            allowed = method == "POST" and identity == "admin" and identity not in self.logins
        elif route == "/emby/Users/New":
            allowed = method == "POST" and self.old_users is not None and self.user is None and body == {"Name": USER_NAME}
        elif route == "/emby/Auth/Keys":
            allowed = method == "POST" and parse_qs(parsed.query).get("App") == [base.APP_PREFIX + "Alpha"]
        elif route == "/emby/Sessions/Logout":
            allowed = method == "POST" and any(login["AccessToken"] == token for login in self.logins.values())
        elif route.startswith("/emby/Auth/Keys/"):
            allowed = method == "DELETE" and any(route == "/emby/Auth/Keys/" + quote(key, safe="") for key in self.owned)
        elif owned_user and route == owned_user:
            allowed = method == "DELETE"
        elif owned_user and route == owned_user + "/Policy":
            allowed = method == "POST" and body is not None and body.get("IsAdministrator") is False
        elif route.endswith("/PlaybackInfo"):
            allowed = method == "POST" and route == self.video_route + "/PlaybackInfo" and body is not None and (
                "UserId" not in body or body["UserId"] == self.user["Id"])
        elif route in {"/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"}:
            allowed = method == "POST" and token in self.owned and body is not None and body.get("ItemId") == self.item_id and (
                "UserId" not in body or body["UserId"] == self.user["Id"]) and not body.get("AdditionalUsers")
        elif self.user and route in {owned_user + "/FavoriteItems/" + self.item_id,
                                     "/emby/Users//FavoriteItems/" + self.item_id,
                                     "/emby/FavoriteItems/" + self.item_id}:
            allowed = method in {"POST", "DELETE"} and token in self.owned
        base.require(allowed, "Mutation is outside the owned playback scope")

    def request(self, label: str, method: str, path: str, *, token: str = "", query_token: bool = False,
                body: dict | None = None, identity: str = "", binary: bool = False) -> tuple[int, object]:
        self.mutation_allowed(method, path, token, body, identity)
        headers = {"Accept": "application/json"}
        if identity:
            headers["Authorization"] = ('Emby Client="Goby Keys Playback Recorder", Device="Keys Playback Owned", '
                f'DeviceId="{base.DEVICE}-{identity}", Version="0.1.0"')
        if token:
            self.secrets.add(token)
            if query_token:
                path += ("&" if "?" in path else "?") + urlencode({"api_key": token})
            else:
                headers["X-Emby-Token"] = token
        if binary:
            headers["Range"] = "bytes=0-31"
        wire = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            wire = json.dumps(body, separators=(",", ":")).encode()
        limit = min(4096 if binary else base.MAX_BODY, base.MAX_TOTAL - self.total)
        base.require(limit > 0, "Capture exhausted its total wire bound")
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=5)
        try:
            connection.request(method, path, wire, headers)
            response = connection.getresponse()
            content = response.read(limit)
            complete = response.isclosed() or response.length == 0
            status, response_headers = response.status, response.getheaders()
            content_type = response.getheader("Content-Type", "")
        finally:
            connection.close()
        self.total += len(content)
        if binary and not content_type.startswith(("application/json", "text/")):
            result = {"base64": base64.b64encode(content).decode(), "sha256": hashlib.sha256(content).hexdigest()}
            kind = "binary"
        else:
            text = content.decode("utf-8", errors="replace")
            try:
                result, kind = json.loads(text), "json"
            except json.JSONDecodeError:
                result, kind = text, "text"
        if identity and isinstance(result, dict) and result.get("AccessToken"):
            self.logins[identity] = result
            self.secrets.add(result["AccessToken"])
            base.save(base.PRIVATE / (identity + "-login.json"), result)
        self.write(label, {"request": {"method": method, "path": path, "headers": headers, "body": body},
            "response": {"status": status, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content),
                            "rangeRequested": "bytes=0-31" if binary else None}})
        self.record_count += 1
        self.incomplete_count += int(not complete)
        print(json.dumps({"capture": base.PREFIX + label, "status": status, "bodyType": kind, "bytes": len(content),
                          "completeHTTP": complete}), flush=True)
        base.require(complete or binary, "JSON response exceeds the bounded capture")
        return status, result

    def users(self, label: str) -> list:
        status, result = self.request(label, "GET", "/emby/Users", token=self.logins["admin"]["AccessToken"])
        base.require(status == 200 and isinstance(result, list), "User listing contract differs")
        return result

    def detail(self, label: str, key: str) -> None:
        self.request(label, "GET", "/emby/Users/" + self.user["Id"] + "/Items/" + self.item_id, token=key)

    def capture(self) -> None:
        original = base.DATA / "private/raw/playback-info-post-minimal.json"
        source = json.loads(original.read_text())["response"]["body"]["MediaSources"][0]
        source_path = Path(source["Path"])
        base.require(str(source_path) in self.baseline["media"], "Video is outside the known source baseline")
        self.item_id = source["Id"].removeprefix("mediasource_")
        self.source_id = source["Id"]
        self.video_route = "/emby/Items/" + self.item_id
        self.login("admin")
        admin = self.logins["admin"]["AccessToken"]
        self.old_keys = self.key_list("keys-before")
        self.old_users = self.users("users-before")
        base.require(not any(user.get("Name") == USER_NAME for user in self.old_users), "User name already exists")
        status, user = self.request("user-create", "POST", "/emby/Users/New", token=admin, body={"Name": USER_NAME})
        base.require(status == 200 and isinstance(user, dict) and user.get("Id") and user.get("Name") == USER_NAME,
                     "Owned user creation differs")
        self.user = user
        base.require(user.get("Policy", {}).get("IsAdministrator") is False, "New user is not ordinary")
        self.apps.add(base.APP_PREFIX + "Alpha")
        self.request("key-create", "POST", "/emby/Auth/Keys?" + urlencode({"App": base.APP_PREFIX + "Alpha"}), token=admin)
        self.key_list("keys-created")
        base.require(len(self.owned) == 1, "Expected one newly owned key")
        key = next(iter(self.owned))
        self.request("key-users-me", "GET", "/emby/Users/Me", token=key)
        self.request("sessions-before", "GET", "/emby/Sessions", token=key)
        self.detail("user-item-before", key)
        infos = {}
        for mode in ("no-user", "explicit-user"):
            params = {} if mode == "no-user" else {"UserId": user["Id"]}
            for method in ("GET", "POST"):
                path = self.video_route + "/PlaybackInfo"
                if method == "GET" and params:
                    path += "?" + urlencode(params)
                status, info = self.request(mode + "-info-" + method.lower(), method, path, token=key,
                    body=params if method == "POST" else None)
                if method == "POST" and status == 200 and isinstance(info, dict):
                    infos[mode] = info
            query = {"Static": "true", "MediaSourceId": self.source_id, **params}
            self.request(mode + "-video-range", "GET", "/emby/Videos/" + self.item_id + "/stream?" + urlencode(query),
                         token=key, binary=True)
        for mode, info in infos.items():
            params = {} if mode == "no-user" else {"UserId": user["Id"]}
            report = {"ItemId": self.item_id, "MediaSourceId": self.source_id, "PlaySessionId": info["PlaySessionId"],
                      "PositionTicks": 0, "CanSeek": True, "IsPaused": True, "PlayMethod": "DirectStream", **params}
            self.pending_reports.append((mode, report, key))
            self.request(mode + "-started", "POST", "/emby/Sessions/Playing", token=key, body=report)
            self.request(mode + "-sessions-started", "GET", "/emby/Sessions", token=key)
            self.detail(mode + "-item-started", key)
            progress = {**report, "PositionTicks": 10000000, "EventName": "TimeUpdate"}
            self.request(mode + "-progress", "POST", "/emby/Sessions/Playing/Progress", token=key, body=progress)
            self.request(mode + "-sessions-progress", "GET", "/emby/Sessions", token=key)
            self.detail(mode + "-item-progress", key)
            status, _ = self.request(mode + "-stopped", "POST", "/emby/Sessions/Playing/Stopped", token=key, body=progress)
            if status in {200, 204}:
                self.pending_reports.remove((mode, report, key))
            self.request(mode + "-sessions-stopped", "GET", "/emby/Sessions", token=key)
            self.detail(mode + "-item-stopped", key)
        for label, path in (("favorite-missing-user", "/emby/Users//FavoriteItems/" + self.item_id),
                            ("favorite-global", "/emby/FavoriteItems/" + self.item_id),
                            ("favorite-explicit-user", "/emby/Users/" + user["Id"] + "/FavoriteItems/" + self.item_id)):
            self.request(label, "POST", path, token=key)
        self.detail("item-after-favorites", key)
        self.request("favorite-explicit-remove", "DELETE", "/emby/Users/" + user["Id"] + "/FavoriteItems/" + self.item_id, token=key)
        audio_context = base.RUNTIME / "audio-m4c/private/context.json"
        if audio_context.exists():
            audio = json.loads(audio_context.read_text())["items"]["mp3"]
            path = Path(audio["Path"])
            base.require(str(path) in self.baseline["media"] and path.stat().st_size < 256 * 1024,
                         "Audio is outside the tiny known source scope")
            for mode in ("no-user", "explicit-user"):
                query = {"Static": "true", "MediaSourceId": audio["MediaSourceId"]}
                if mode == "explicit-user":
                    query["UserId"] = user["Id"]
                self.request(mode + "-audio-range", "GET", "/emby/Audio/" + audio["Id"] + "/stream?" + urlencode(query),
                             token=key, binary=True)
        for label, updates in (("playback-disabled", {"EnableMediaPlayback": False}), ("user-disabled", {"IsDisabled": True})):
            policy = {**user["Policy"], **updates}
            status, _ = self.request(label + "-set-policy", "POST", "/emby/Users/" + user["Id"] + "/Policy", token=admin, body=policy)
            base.require(status in {200, 204}, "Owned policy update failed")
            self.request(label + "-user", "GET", "/emby/Users/" + user["Id"], token=key)
            for mode in ("no-user", "explicit-user"):
                params = {} if mode == "no-user" else {"UserId": user["Id"]}
                self.request(label + "-" + mode + "-info", "POST", self.video_route + "/PlaybackInfo", token=key, body=params)
                query = {"Static": "true", "MediaSourceId": self.source_id, **params}
                self.request(label + "-" + mode + "-range", "GET", "/emby/Videos/" + self.item_id + "/stream?" + urlencode(query),
                             token=key, binary=True)

    def finish(self) -> None:
        if "admin" not in self.logins:
            return
        admin = self.logins["admin"]["AccessToken"]
        for index, (_, report, key) in enumerate(self.pending_reports):
            self.request("cleanup-stop-" + str(index), "POST", "/emby/Sessions/Playing/Stopped", token=key, body=report)
        if self.user:
            status, _ = self.request("user-delete", "DELETE", "/emby/Users/" + self.user["Id"], token=admin)
            base.require(status in {200, 204}, "Owned user deletion failed")
        if self.old_users is not None:
            users = self.users("users-final")
            base.require({user["Id"] for user in users} == {user["Id"] for user in self.old_users},
                         "Existing user membership changed or owned user remains")
            old = {user["Id"]: user["Policy"] for user in self.old_users}
            base.require({user["Id"]: user["Policy"] for user in users} == old, "Existing user policy changed")
        if self.old_keys is not None:
            current = self.key_list("cleanup-keys")
            for index, item in enumerate(current):
                if item["AccessToken"] in self.owned:
                    status, _ = self.request("cleanup-key-" + str(index), "DELETE",
                        "/emby/Auth/Keys/" + quote(item["AccessToken"], safe=""), token=admin)
                    base.require(status in {200, 204}, "Owned key deletion failed")
            final = self.key_list("keys-final")
            base.require(sorted(final, key=lambda item: item["AccessToken"]) == sorted(self.old_keys, key=lambda item: item["AccessToken"]),
                         "Existing keys changed or an owned key remains")
        self.logout_owned_logins()
        base.require(base.preconditions() == self.pid, "Reference process changed")
        base.require(self.snapshot() == self.baseline, "Existing evidence or source hashes changed")
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            base.require(self.sanitize(json.loads(path.read_text())) == json.loads((base.EXPORT / path.name).read_text()),
                         "Capture redaction audit differs")
            base.private_file(path)
            base.private_file(base.EXPORT / path.name)
        self.cleanup_ok = True
        self.write("audit", {"kind": "capture-audit-observation", "referencePID": self.pid,
            "preservedOldRecords": 1006, "preservedOldRecordFiles": len(self.baseline["records"]),
            "preservedOldMediaFiles": len(self.baseline["media"]), "oldHashesUnchanged": True,
            "oldUserMembershipAndPoliciesUnchanged": True, "ownedUsersRemaining": 0,
            "oldKeyRecordsUnchanged": True, "ownedKeysRemaining": 0,
            "ownedLoginLogoutStatuses": self.logout_statuses, "wireBytes": self.total,
            "httpRecords": self.record_count, "incompleteHTTP": self.incomplete_count})


def main() -> None:
    recorder = Recorder(base.preconditions())
    failure = None
    try:
        recorder.capture()
    except Exception as error:
        failure = type(error).__name__
        base.save(base.PRIVATE / "failure.txt", failure + ": " + str(error))
    finally:
        try:
            recorder.finish()
        except Exception as error:
            base.save(base.PRIVATE / "cleanup-failure.txt", type(error).__name__ + ": " + str(error))
            print(json.dumps({"capture": base.PREFIX, "cleanup": "failed", "failureType": type(error).__name__}), flush=True)
            raise SystemExit(1)
    print(json.dumps({"capture": base.PREFIX, "result": "complete" if failure is None else "partial",
                      "failureType": failure, "cleanup": recorder.cleanup_ok}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
