#!/usr/bin/env python3
"""Capture bounded key client metadata, filters, and explicit-user policy.

Run only through root SSH alongside the two preceding API-key recorder modules.
The study creates one new ordinary user and application key, and removes both.
Only negotiation and bounded static delivery are permitted; returned conversion
URLs are captured as data and never requested.
"""

from __future__ import annotations

import base64
import hashlib
import http.client
import importlib.util
import json
from pathlib import Path
import stat
import sys
from urllib.parse import urlencode

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("key_playback_reference", Path(__file__).with_name("reference-api-key-playback.py"))
playback = importlib.util.module_from_spec(spec)
spec.loader.exec_module(playback)
base = playback.base
PREVIOUS = base.ROOT
base.__file__ = __file__
base.ROOT = Path("/opt/goby-test/exec-scratch/keys-context-m5d")
base.PRIVATE = base.ROOT / "private"
base.RAW = base.PRIVATE / "raw"
base.EXPORT = base.ROOT / "export"
base.PREFIX = "keys-context-m5d-"
base.MARKER = "goby-reference-keys-context-m5d-owned-v1"
base.APP_PREFIX = "Goby Keys Context M5d 20260910 01 "
base.DEVICE = "goby-keys-context-m5d-20260910-01"
base.MAX_TOTAL = 2 * 1024 * 1024
playback.USER_NAME = "reference-keys-context-m5d-20260910-01"


class Recorder(playback.Recorder):
    def snapshot(self) -> dict:
        prior_path = PREVIOUS / "private/baseline.json"
        base.private_file(prior_path)
        prior = json.loads(prior_path.read_text())
        paths = [Path(path) for path in prior["records"]]
        for folder in (PREVIOUS / "private/raw", PREVIOUS / "export"):
            paths.extend(folder.glob("*.json"))
        base.require(len(paths) == 2130 and len(set(paths)) == 2130,
                     "Expected all 1065 preceding raw/export pairs")
        media = [Path(path) for path in prior["media"]]
        private_roots = {path.parent.parent for path in paths if path.parent.name == "raw"}
        private_paths = sorted({path for folder in private_roots for path in folder.rglob("*") if path.is_file()})
        base.require(sum(path.stat().st_size for path in media) < 32 * 1024 * 1024,
                     "Known source audit exceeds its bound")
        for path in [*paths, *media, *private_paths]:
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode),
                         "Preserved path is not a regular file")
        return {"records": {str(path): base.digest(path) for path in paths},
                "media": {str(path): base.digest(path) for path in media},
                "privateFiles": {str(path): base.digest(path) for path in private_paths}}

    def write(self, label: str, value: dict) -> None:
        if label == "audit":
            value["preservedOldRecords"] = len(self.baseline["records"]) // 2
            value["conversionURLsRequested"] = 0
            value["preservedOldPrivateFiles"] = len(self.baseline["privateFiles"])
        super().write(label, value)

    def request(self, label: str, method: str, path: str, *, token: str = "", query_token: bool = False,
                body: dict | None = None, identity: str = "", binary: bool = False,
                client: dict | None = None) -> tuple[int, object]:
        self.mutation_allowed(method, path, token, body, identity)
        base.require(self.record_count < 35, "HTTP request count exceeds this study's bound")
        base.require("/master.m3u8" not in path and "/main.m3u8" not in path and "/stream.ts" not in path,
                     "Conversion delivery is outside this study")
        if binary:
            base.require("Static=true" in path and path.startswith("/emby/Videos/"), "Only static video may be read")
        headers = {"Accept": "application/json"}
        if identity:
            headers["Authorization"] = ('Emby Client="Goby Keys Context Recorder", Device="Keys Context Owned", '
                f'DeviceId="{base.DEVICE}-{identity}", Version="0.1.0"')
        if client:
            base.require(not identity and set(client) == {"Client", "DeviceId", "Device", "Version"} and
                         all(isinstance(value, str) and value.replace("-", "").replace(".", "").isalnum()
                             for value in client.values()), "Client metadata is outside the bounded shape")
            headers["Authorization"] = "Emby " + ", ".join(name + '="' + value + '"' for name, value in client.items())
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
        self.write(label, {"request": {"method": method, "path": path, "headers": headers, "body": body,
                                      "clientMetadata": client},
            "response": {"status": status, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content),
                            "rangeRequested": "bytes=0-31" if binary else None}})
        self.record_count += 1
        self.incomplete_count += int(not complete)
        print(json.dumps({"capture": base.PREFIX + label, "status": status, "bodyType": kind, "bytes": len(content),
                          "completeHTTP": complete}), flush=True)
        base.require(complete or binary, "JSON response exceeds the bounded capture")
        return status, result

    @staticmethod
    def conversion_profile() -> dict:
        return {"IsPlayback": True, "AutoOpenLiveStream": False, "EnableDirectPlay": False,
                "EnableDirectStream": False, "EnableTranscoding": True,
                "AllowVideoStreamCopy": False, "AllowAudioStreamCopy": False,
                "DeviceProfile": {"Name": "Goby-Key-Context-HLS", "MaxStreamingBitrate": 2000000,
                    "DirectPlayProfiles": [], "TranscodingProfiles": [{"Type": "Video", "Container": "ts",
                        "VideoCodec": "h264", "AudioCodec": "aac", "Protocol": "hls", "Context": "Streaming",
                        "MaxAudioChannels": "2", "MinSegments": 1, "SegmentLength": 3}]}}

    def capture(self) -> None:
        original = base.DATA / "private/raw/playback-info-post-minimal.json"
        source = json.loads(original.read_text())["response"]["body"]["MediaSources"][0]
        base.require(str(Path(source["Path"])) in self.baseline["media"], "Video is outside the known source baseline")
        self.item_id = source["Id"].removeprefix("mediasource_")
        self.source_id = source["Id"]
        self.video_route = "/emby/Items/" + self.item_id
        self.login("admin")
        admin = self.logins["admin"]["AccessToken"]
        self.old_keys = self.key_list("keys-before")
        self.old_users = self.users("users-before")
        base.require(not any(user.get("Name") == playback.USER_NAME for user in self.old_users), "User name already exists")
        status, user = self.request("user-create", "POST", "/emby/Users/New", token=admin, body={"Name": playback.USER_NAME})
        base.require(status == 200 and isinstance(user, dict) and user.get("Id") and user.get("Name") == playback.USER_NAME,
                     "Owned user creation differs")
        self.user = user
        base.require(user.get("Policy", {}).get("IsAdministrator") is False, "New user is not ordinary")
        self.apps.add(base.APP_PREFIX + "Alpha")
        self.request("key-create", "POST", "/emby/Auth/Keys?" + urlencode({"App": base.APP_PREFIX + "Alpha"}), token=admin)
        self.key_list("keys-created")
        base.require(len(self.owned) == 1, "Expected one newly owned key")
        key = next(iter(self.owned))
        self.request("sessions-baseline", "GET", "/emby/Sessions", token=key)
        for variant in ("alpha", "beta"):
            client = {"Client": "GobyContext-" + variant, "DeviceId": "goby-context-client-" + variant,
                      "Device": "ContextDevice-" + variant, "Version": "9.8.7" if variant == "alpha" else "1.2.3"}
            self.request("metadata-" + variant + "-sessions", "GET", "/emby/Sessions", token=key, client=client)
            query = {"DeviceId": "goby-context-query-" + variant}
            method = "GET" if variant == "alpha" else "POST"
            self.request("device-" + variant + "-info", method, self.video_route + "/PlaybackInfo?" + urlencode(query),
                         token=key, body={} if method == "POST" else None, client=client)
            self.request("device-" + variant + "-range", "GET", "/emby/Videos/" + self.item_id + "/stream?" +
                         urlencode({"Static": "true", "MediaSourceId": self.source_id, **query}), token=key,
                         binary=True, client=client)
        for label, query in (("favorites", {"IsFavorite": "true"}), ("played", {"IsPlayed": "true"}),
                             ("unplayed", {"IsPlayed": "false"}), ("resumable", {"Filters": "IsResumable"})):
            self.request("filter-" + label, "GET", "/emby/Items?" + urlencode({"Recursive": "true", "Limit": 5, **query}), token=key)
        self.request("profile-initial-explicit-user", "POST", self.video_route + "/PlaybackInfo", token=key,
                     body={**self.conversion_profile(), "UserId": user["Id"]})
        restrictions = {"EnableAudioPlaybackTranscoding": False, "EnableVideoPlaybackTranscoding": False,
                        "EnablePlaybackRemuxing": False}
        for label, flags in (("playback-disabled", {"EnableMediaPlayback": False}), ("user-disabled", {"IsDisabled": True})):
            policy = {**user["Policy"], **restrictions, **flags}
            status, _ = self.request(label + "-set-policy", "POST", "/emby/Users/" + user["Id"] + "/Policy", token=admin, body=policy)
            base.require(status in {200, 204}, "Owned policy update failed")
            self.request(label + "-user", "GET", "/emby/Users/" + user["Id"], token=key)
            modes = ("explicit-user",) if label == "playback-disabled" else ("no-user", "explicit-user")
            for mode in modes:
                body = self.conversion_profile()
                if mode == "explicit-user":
                    body["UserId"] = user["Id"]
                self.request(label + "-profile-" + mode, "POST", self.video_route + "/PlaybackInfo", token=key, body=body)
            favorite = "/emby/Users/" + user["Id"] + "/FavoriteItems/" + self.item_id
            self.request(label + "-favorite-add", "POST", favorite, token=key)
            self.request(label + "-favorite-remove", "DELETE", favorite, token=key)


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
