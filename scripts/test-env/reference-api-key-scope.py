#!/usr/bin/env python3
"""Capture target-library ACLs and independent key playback policy flags.

Run only through root SSH alongside the preceding API-key recorder modules.
Only one fresh ordinary user and key may be changed. Profile negotiation never
follows its returned media URLs. The complete study is bounded to 26 requests.
"""

from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import stat
import sys
from urllib.parse import urlencode

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("key_context_reference", Path(__file__).with_name("reference-api-key-context.py"))
context = importlib.util.module_from_spec(spec)
spec.loader.exec_module(context)
base = context.base
playback = context.playback
PREVIOUS = base.ROOT
base.__file__ = __file__
base.ROOT = Path("/opt/goby-test/exec-scratch/keys-scope-m5d")
base.PRIVATE = base.ROOT / "private"
base.RAW = base.PRIVATE / "raw"
base.EXPORT = base.ROOT / "export"
base.PREFIX = "keys-scope-m5d-"
base.MARKER = "goby-reference-keys-scope-m5d-owned-v1"
base.APP_PREFIX = "Goby Keys Scope M5d 20260910 01 "
base.DEVICE = "goby-keys-scope-m5d-20260910-01"
base.MAX_TOTAL = 2 * 1024 * 1024
playback.USER_NAME = "reference-keys-scope-m5d-20260910-01"


class Recorder(context.Recorder):
    def snapshot(self) -> dict:
        prior_path = PREVIOUS / "private/baseline.json"
        base.private_file(prior_path)
        prior = json.loads(prior_path.read_text())
        paths = [Path(path) for path in prior["records"]]
        for folder in (PREVIOUS / "private/raw", PREVIOUS / "export"):
            paths.extend(folder.glob("*.json"))
        base.require(len(paths) == 2202 and len(set(paths)) == 2202,
                     "Expected all 1101 preceding raw/export pairs")
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

    def request(self, label: str, method: str, path: str, **kwargs) -> tuple[int, object]:
        base.require(self.record_count < 26, "HTTP request count exceeds this study's bound")
        base.require(not kwargs.get("binary"), "Media delivery is outside this negotiation study")
        return super().request(label, method, path, **kwargs)

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
        policy = user.get("Policy", {})
        base.require(policy.get("IsAdministrator") is False and policy.get("IsDisabled") is False,
                     "New user is not ordinary and enabled")
        base.require(all(policy.get(name) is True for name in ("EnableAllFolders", "EnableMediaPlayback",
                         "EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding", "EnablePlaybackRemuxing")),
                     "Initial policy does not meet the independent-control baseline")
        self.apps.add(base.APP_PREFIX + "Alpha")
        self.request("key-create", "POST", "/emby/Auth/Keys?" + urlencode({"App": base.APP_PREFIX + "Alpha"}), token=admin)
        self.key_list("keys-created")
        base.require(len(self.owned) == 1, "Expected one newly owned key")
        key = next(iter(self.owned))
        user_route = "/emby/Users/" + user["Id"]
        self.request("folders-initial-views", "GET", user_route + "/Views", token=key)
        self.request("folders-initial-items", "GET", user_route + "/Items?Recursive=true&Limit=2", token=key)
        self.request("profile-initial-no-user", "POST", self.video_route + "/PlaybackInfo", token=key,
                     body=self.conversion_profile())
        self.request("profile-initial-explicit-user", "POST", self.video_route + "/PlaybackInfo", token=key,
                     body={**self.conversion_profile(), "UserId": user["Id"]})
        denied_policy = {**policy, "EnableAllFolders": False, "EnabledFolders": []}
        status, _ = self.request("folders-denied-set-policy", "POST", user_route + "/Policy", token=admin, body=denied_policy)
        base.require(status in {200, 204}, "Owned folder policy update failed")
        self.request("folders-denied-views", "GET", user_route + "/Views", token=key)
        self.request("folders-denied-items", "GET", user_route + "/Items?Recursive=true&Limit=2", token=key)
        self.request("folders-denied-no-user-items", "GET", "/emby/Items?Recursive=true&Limit=2", token=key)
        for label, flag in (("user-disabled-only", "IsDisabled"), ("playback-disabled-only", "EnableMediaPlayback")):
            changed = {**policy, flag: flag == "IsDisabled"}
            status, _ = self.request(label + "-set-policy", "POST", user_route + "/Policy", token=admin, body=changed)
            base.require(status in {200, 204}, "Owned independent policy update failed")
            self.request(label + "-user", "GET", user_route, token=key)
            self.request(label + "-profile", "POST", self.video_route + "/PlaybackInfo", token=key,
                         body={**self.conversion_profile(), "UserId": user["Id"]})


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
