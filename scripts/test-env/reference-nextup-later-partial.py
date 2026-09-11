#!/usr/bin/env python3
"""Research later-episode partial playback with a retained, newly owned user.

Only one new ordinary account and its S01E02 playback state are created. The
account and protocol history remain as evidence. This is not client acceptance.
Existing users, library configuration, media, and reference services are intact.
"""

from __future__ import annotations

import fcntl
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import subprocess
import sys
import time
from urllib.parse import parse_qs, urlencode, urlsplit

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
ROOT = WORK / "reference-nextup-later-partial-v1"
ORIGINAL_ROOT = ROOT
PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
MARKER = "goby-m3e-nextup-later-partial-v1"
USERNAME = "m3e-nextup-later-partial"
PID, TICKS, PORT = 332054, "357218", 18097
SERIES, EPISODES, TARGET = "14", ("18", "17", "19"), "17"
MAX_REQUESTS, MAX_BODY = 30, 256 * 1024
POSITION, RUNTIME = 1200000000, 6000000000


class ResearchError(Exception):
    """An ownership, scope, request, or evidence guard failed."""


def require(condition, message):
    if not condition:
        raise ResearchError(message)


def collect_play_secrets(value, secret_values):
    if isinstance(value, dict):
        for key, item in value.items():
            if str(key).lower() == "playsessionid" and isinstance(item, str) and item:
                secret_values.add(item)
            collect_play_secrets(item, secret_values)
    elif isinstance(value, list):
        for item in value:
            collect_play_secrets(item, secret_values)
    elif isinstance(value, str):
        for match in re.finditer(r"(?i)[?&]PlaySessionId=([^&\s\"'<>]+)", value):
            secret_values.add(match.group(1))


def load_support():
    source = Path(__file__).absolute().with_name("reference-client-initialization.py")
    expected = json.loads((WORK / "reference-client-initialization-v1/export/safety-report.json").read_text())["sourceSha256"]
    require(hashlib.sha256(source.read_bytes()).hexdigest() == expected, "The accepted research support source changed.")
    spec = importlib.util.spec_from_file_location("later_partial_support", source)
    support = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(support)
    return support, support.load_operator()


class Recorder:
    def __init__(self, support, operator, owner, browser, fixture, credential):
        self.support, self.op, self.owner = support, operator, owner
        self.admin, self.credential = browser["accounts"]["admin"], credential
        require(credential.get("marker") == MARKER and credential.get("username") == USERNAME and
                isinstance(credential.get("password"), str) and len(credential["password"]) >= 40,
                "The newly generated, persisted account credential differs.")
        self.secrets = {row["password"] for row in browser["accounts"].values()} | {credential["password"]}
        self.library_ids = {row["ItemId"] for row in fixture["libraries"]}
        require(len(self.library_ids) == 3 and {row["CollectionType"] for row in fixture["libraries"]} ==
                {"movies", "tvshows", "music"}, "The three owned reference libraries differ.")
        episodes = sorted((row for row in fixture["items"] if row["Type"] == "Episode"),
                          key=lambda row: (row["ParentIndexNumber"], row["IndexNumber"]))
        require(tuple(row["Id"] for row in episodes) == EPISODES and all(row["SeriesId"] == SERIES for row in episodes),
                "The owned three-episode series identity differs.")
        self.user_id, self.existing_ids, self.new_policy = None, None, None
        self.tokens, self.verified, self.revoked, self.sessions = {}, set(), set(), {}
        self.login_attempts = set()
        self.records, self.observations, self.baseline, self.final = {}, {}, {}, {}
        self.baseline_projection = "single-item detail"
        self.count, self.finishing, self.started = 0, False, time.monotonic()
        self.play, self.profile, self.profile_preserved = None, None, False
        self.persist()

    def persist(self):
        state = {"marker": MARKER, "worker": self.op.process_identity(os.getpid()), "requests": self.count,
                 "serviceIdentity": self.owner["serviceIdentity"], "newUserId": self.user_id,
                 "existingUserIds": sorted(self.existing_ids) if self.existing_ids is not None else None,
                 "tokens": self.tokens, "verifiedTokens": sorted(self.verified), "revokedTokens": sorted(self.revoked),
                 "attemptedLoginRoles": sorted(self.login_attempts),
                 "sessionIds": self.sessions, "ownedPlayback": self.play, "retainedUserAndHistory": True}
        path = PRIVATE / "state.json"
        if self.op.present(path):
            require(self.op.read_private(path).get("marker") == MARKER, "An unrelated study state cannot be replaced.")
            temporary = PRIVATE / ("state.next-" + secrets.token_hex(10) + ".json")
            self.op.save(temporary, state)
            os.replace(temporary, path)
            self.op.sync_directory(PRIVATE)
        else:
            self.op.save(path, state)

    def identity(self):
        identity = self.owner["serviceIdentity"]
        require(self.op.service_identity(self.owner) == identity and identity["pid"] == PID and identity["startTicks"] == TICKS and
                os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "The exact fresh reference namespace changed.")

    def approve(self, role, method, route, body, login=False):
        parsed = urlsplit(route)
        query = parse_qs(parsed.query, keep_blank_values=True)
        require(role in ("admin", "new") and not parsed.scheme and not parsed.netloc and not parsed.fragment,
                "An external or unknown-account request is forbidden.")
        if login:
            require(method == "POST" and route == "/emby/Users/AuthenticateByName" and body is None and role not in self.tokens and
                    (role == "admin" or self.user_id is not None), "Only one new session per owned study identity may authenticate.")
            return
        require(role in self.verified, "The request lacks a proven newly acknowledged recorder session.")
        if route == "/emby/Sessions/Logout":
            require(method == "POST" and body is None and role not in self.revoked, "Only the owned recorder session may log out.")
            return
        if route == "/emby/Sessions":
            require(method == "GET" and body is None, "The token-invalidity proof must be read-only.")
            return
        require(role not in self.revoked, "A revoked token may only be checked for invalidity.")
        if role == "admin":
            if method == "GET" and route == "/emby/Users":
                require(body is None and self.existing_ids is None, "The account absence check must precede creation exactly once.")
                return
            if method == "POST" and route == "/emby/Users/New":
                require(body == {"Name": USERNAME} and self.existing_ids is not None and self.user_id is None,
                        "Account creation requires an acknowledged absent username and persisted credentials.")
                return
            require(self.user_id is not None and self.user_id not in self.existing_ids,
                    "Account setup cannot target an existing user.")
            if method == "POST" and route == "/emby/Users/" + self.user_id + "/Password":
                require(body == {"Id": self.user_id, "NewPw": self.credential["password"], "ResetPassword": False},
                        "Only the newly created account may receive its persisted password.")
                return
            if method == "POST" and route == "/emby/Users/" + self.user_id + "/Policy":
                require(body == self.new_policy and isinstance(body, dict) and body.get("IsAdministrator") is False and
                        body.get("IsDisabled") is False and body.get("EnableAllFolders") is False and
                        set(body.get("EnabledFolders", [])) == self.library_ids and body.get("EnableContentDeletion") is False,
                        "Only the new ordinary account's exact owned-library policy may be established.")
                return
            raise ResearchError("An administrative operation is outside new-account setup.")
        prefix = "/emby/Users/" + self.user_id
        if method == "GET":
            require(body is None, "A research GET cannot carry a body.")
            if route in {prefix, prefix + "/Views", *(prefix + "/Items/" + item for item in EPISODES)}:
                return
            if parsed.path == "/emby/Shows/NextUp":
                require(set(query) <= {"UserId", "SeriesId", "Limit", "StartIndex", "EnableImages", "Fields"} and
                        query.get("UserId") == [self.user_id] and query.get("Limit") in (["10"], ["1"]) and
                        query.get("EnableImages") == ["false"] and query.get("Fields") == ["UserData,ParentId"] and
                        ("SeriesId" not in query or query["SeriesId"] == [SERIES]) and
                        ("StartIndex" not in query or query["StartIndex"] == ["1"]), "The NextUp query exceeds the fixed series controls.")
                return
        if method == "POST" and route == "/emby/Items/" + TARGET + "/PlaybackInfo":
            require(body == {"UserId": self.user_id, "IsPlayback": True} and set(self.baseline) == set(EPISODES) and self.play is None,
                    "Only the new account's second episode may negotiate playback after a complete zero baseline.")
            return
        if method == "POST" and route in ("/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"):
            require(isinstance(body, dict) and self.play is not None and not self.play["stopped"] and
                    all(body.get(key) == self.play[key] for key in ("ItemId", "MediaSourceId", "PlaySessionId", "SessionId")) and
                    set(body) <= {"ItemId", "MediaSourceId", "PlaySessionId", "SessionId", "PositionTicks", "RunTimeTicks", "CanSeek",
                                  "IsPaused", "IsMuted", "PlayMethod", "PlaybackRate", "EventName", "Failed", "IsAutomated"} and
                    body.get("PositionTicks") in (0, POSITION), "The playback report is outside the single owned partial session.")
            return
        raise ResearchError("Existing users, other media, watched flags, preferences, scans, and library writes are forbidden.")

    def request(self, label, role, method, route, body=None, *, login=False):
        self.identity()
        reserve = 2 if self.play is not None and self.play["stopped"] else 3
        require(self.count < (MAX_REQUESTS if self.finishing else MAX_REQUESTS - reserve) and
                time.monotonic() - self.started < (360 if self.finishing else 240), "The request or cleanup reserve is exhausted.")
        self.approve(role, method, route, body, login)
        self.count += 1
        if login:
            self.login_attempts.add(role)
        self.persist()
        self.op.save(PRIVATE / f"{self.count:03d}-{label}-intent.json",
                     {"marker": MARKER, "role": role, "method": method, "path": route, "body": body,
                      "authenticationBodyRetained": False, "finishing": self.finishing})
        device = MARKER + "-" + role + "-recorder"
        headers = {"Accept": "application/json", "Authorization":
                   'Emby Client="Goby Later Partial Protocol Research", Device="Linux Protocol Research", DeviceId="' + device + '", Version="1.0"'}
        if login:
            account = self.admin if role == "admin" else self.credential
            payload = urlencode({"Username": account["username"], "Pw": account["password"]}).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        else:
            headers["X-Emby-Token"] = self.tokens[role]
            payload = json.dumps(body, separators=(",", ":")).encode() if body is not None else None
            if body is not None:
                headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=8)
        try:
            signal.setitimer(signal.ITIMER_REAL, 8)
            connection.request(method, route, body=payload, headers=headers)
            response = connection.getresponse()
            status, response_headers, raw = response.status, dict(response.getheaders()), response.read(MAX_BODY + 1)
            require(len(raw) <= MAX_BODY, "The bounded reference body exceeded its limit.")
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        try:
            result = json.loads(raw) if raw else None
        except (ValueError, UnicodeDecodeError):
            result = raw.decode("utf-8", errors="replace")
        collect_play_secrets(result, self.secrets)
        self.support.collect_secrets(result, self.secrets)
        if login and isinstance(result, dict) and result.get("AccessToken"):
            self.tokens[role] = result["AccessToken"]
            self.secrets.add(result["AccessToken"])
            self.op.save(PRIVATE / (role + "-login.json"), result)
            self.persist()
            expected_id = self.admin["userId"] if role == "admin" else self.user_id
            expected_name = self.admin["username"] if role == "admin" else USERNAME
            require(status == 200 and result.get("ServerId") == self.owner["serverId"] and
                    result.get("User", {}).get("Id") == expected_id and result["User"].get("Name") == expected_name and
                    result["User"].get("Policy", {}).get("IsAdministrator") is (role == "admin") and
                    result.get("SessionInfo", {}).get("DeviceId") == device and result["SessionInfo"].get("UserId") == expected_id,
                    "The persisted recorder token lacks its exact new session identity proof.")
            self.verified.add(role)
            self.sessions[role] = result["SessionInfo"]["Id"]
            if role == "new":
                self.profile = {key: result["User"].get(key) for key in ("Configuration", "Policy")}
            self.persist()
        record = {"classification": "protocol research; not client acceptance", "case": label,
                  "request": {"role": role, "method": method, "path": route, "body": body, "authenticationBodyRetained": False},
                  "response": {"status": status, "headers": response_headers, "body": result, "bytesRead": len(raw)}}
        self.support.collect_secrets(record, self.secrets)
        self.op.save(PRIVATE / f"{self.count:03d}-{label}-response.json", record)
        self.records[label] = record
        self.identity()
        return status, result

    def logout(self, role):
        status, _ = self.request(role + "-logout", role, "POST", "/emby/Sessions/Logout")
        require(status in (204, 401), "The owned recorder logout failed.")
        status, _ = self.request(role + "-token-invalid", role, "GET", "/emby/Sessions")
        require(status == 401, "The owned recorder token remains usable.")
        self.revoked.add(role)
        self.persist()

    def setup(self):
        status, _ = self.request("admin-login", "admin", "POST", "/emby/Users/AuthenticateByName", login=True)
        require(status == 200 and "admin" in self.verified, "The new administrative recorder login failed.")
        status, users = self.request("users-before", "admin", "GET", "/emby/Users")
        require(status == 200 and isinstance(users, list) and all(isinstance(row.get("Name"), str) for row in users) and
                not any(row["Name"].casefold() == USERNAME.casefold() for row in users), "The proposed owned username is already occupied.")
        self.existing_ids = {row["Id"] for row in users}
        self.persist()
        status, user = self.request("new-user-create", "admin", "POST", "/emby/Users/New", {"Name": USERNAME})
        self.op.save(PRIVATE / "created-user-acknowledgement.json", {"status": status, "user": user})
        require(status == 200 and isinstance(user, dict) and re.fullmatch(r"[0-9a-f]{32}", user.get("Id", "")) and
                user.get("Id") not in self.existing_ids and user.get("Name") == USERNAME and
                user.get("Policy", {}).get("IsAdministrator") is False, "The creation response is not a new ordinary owned user.")
        self.user_id = user["Id"]
        self.persist()
        self.op.save(PRIVATE / "account-owner.json", {"marker": MARKER, "username": USERNAME, "userId": self.user_id,
                     "serverId": self.owner["serverId"], "serviceIdentity": self.owner["serviceIdentity"], "retained": True})
        status, _ = self.request("new-user-password", "admin", "POST", "/emby/Users/" + self.user_id + "/Password",
                                 {"Id": self.user_id, "NewPw": self.credential["password"], "ResetPassword": False})
        require(status in (200, 204), "The new user's persisted password was not accepted.")
        self.new_policy = dict(user["Policy"], IsAdministrator=False, IsDisabled=False, EnableAllFolders=False,
                               EnabledFolders=sorted(self.library_ids), EnableMediaPlayback=True, EnableAudioPlaybackTranscoding=True,
                               EnableVideoPlaybackTranscoding=True, EnablePlaybackRemuxing=True, EnableContentDeletion=False,
                               EnableContentDownloading=False)
        status, _ = self.request("new-user-policy", "admin", "POST", "/emby/Users/" + self.user_id + "/Policy", self.new_policy)
        require(status in (200, 204), "The new ordinary user's fixed three-library policy was not accepted.")
        self.logout("admin")
        status, _ = self.request("new-user-login", "new", "POST", "/emby/Users/AuthenticateByName", login=True)
        require(status == 200 and "new" in self.verified, "The new ordinary recorder login failed.")
        policy = self.profile["Policy"]
        require(policy.get("IsDisabled") is False and policy.get("EnableMediaPlayback") is True and
                policy.get("EnableAllFolders") is False and set(policy.get("EnabledFolders", [])) == self.library_ids,
                "The new ordinary user's effective access is not the exact three owned libraries.")
        status, views = self.request("owned-views", "new", "GET", "/emby/Users/" + self.user_id + "/Views")
        require(status == 200 and {row["Id"] for row in views.get("Items", [])} == self.library_ids,
                "The new account does not see exactly the three owned libraries.")

    def detail(self, stage, item):
        status, dto = self.request(stage + "-detail-" + item, "new", "GET", "/emby/Users/" + self.user_id + "/Items/" + item)
        require(status == 200 and dto.get("Id") == item and dto.get("SeriesId") == SERIES and dto.get("Type") == "Episode" and
                dto.get("RunTimeTicks") == RUNTIME and isinstance(dto.get("UserData"), dict), "The fixed episode detail is unavailable.")
        return dto["UserData"]

    def nextup(self, label, *, series=True, limit=10, start=None):
        query = {"UserId": self.user_id, "Limit": limit, "Fields": "UserData,ParentId", "EnableImages": "false"}
        if series:
            query["SeriesId"] = SERIES
        if start is not None:
            query["StartIndex"] = start
        status, body = self.request(label, "new", "GET", "/emby/Shows/NextUp?" + urlencode(query))
        require(status == 200 and isinstance(body, dict) and isinstance(body.get("Items"), list) and
                all(row.get("Id") in EPISODES for row in body["Items"]), "NextUp returned an unexpected status or unowned episode.")
        self.observations[label] = {"query": query, "ids": [row["Id"] for row in body["Items"]], "total": body.get("TotalRecordCount")}

    def capture(self):
        self.setup()
        for item in EPISODES:
            data = self.detail("baseline", item)
            require(data.get("Played") is False and data.get("PlaybackPositionTicks") == 0 and data.get("PlayCount") == 0 and
                    not data.get("LastPlayedDate"), "The newly created account does not have a complete untouched detail baseline.")
            self.baseline[item] = data
        self.op.save(PRIVATE / "baseline.json", {"userData": self.baseline, "profile": self.profile})
        self.nextup("baseline-series")
        self.nextup("baseline-global", series=False)
        self.playback()
        for item in EPISODES:
            self.final[item] = self.detail("after-partial", item)
            require(self.final[item].get("Played") is False and self.final[item].get("PlaybackPositionTicks") == (POSITION if item == TARGET else 0),
                    "The intended only-second-partial, none-played state was not established.")
            if item != TARGET:
                require(self.final[item] == self.baseline[item], "An untouched episode's full UserData changed.")
        self.nextup("after-partial-series")
        self.nextup("after-partial-series-limit1", limit=1)
        self.nextup("after-partial-series-offset1-limit1", limit=1, start=1)
        self.nextup("after-partial-global", series=False)
        status, dto = self.request("new-profile-final", "new", "GET", "/emby/Users/" + self.user_id)
        self.profile_preserved = status == 200 and all(dto.get(key) == value for key, value in self.profile.items())
        require(self.profile_preserved, "The new account's configuration or policy drifted during playback research.")

    def playback(self):
        status, info = self.request("second-playback-info", "new", "POST", "/emby/Items/" + TARGET + "/PlaybackInfo",
                                    {"UserId": self.user_id, "IsPlayback": True})
        require(status == 200 and info.get("PlaySessionId") and info.get("MediaSources"), "Playback negotiation failed.")
        source = info["MediaSources"][0]
        require(source.get("Id") and source.get("RunTimeTicks") == RUNTIME, "The negotiated source differs from the fixed fixture.")
        self.secrets.add(info["PlaySessionId"])
        self.play = {"ItemId": TARGET, "MediaSourceId": source["Id"], "PlaySessionId": info["PlaySessionId"],
                     "SessionId": self.sessions["new"], "PositionTicks": 0, "stopped": False}
        self.persist()
        context = {key: self.play[key] for key in ("ItemId", "MediaSourceId", "PlaySessionId", "SessionId")}
        started = {**context, "RunTimeTicks": RUNTIME, "PositionTicks": 0, "CanSeek": True, "IsPaused": False,
                   "IsMuted": False, "PlayMethod": "DirectStream", "PlaybackRate": 1}
        status, _ = self.request("second-started", "new", "POST", "/emby/Sessions/Playing", started)
        require(status == 204, "The owned playback start failed.")
        self.play["PositionTicks"] = POSITION
        self.persist()
        status, _ = self.request("second-progress", "new", "POST", "/emby/Sessions/Playing/Progress",
                                 {**started, "PositionTicks": POSITION, "EventName": "TimeUpdate"})
        require(status == 204, "The owned partial playback report failed.")
        self.stop("second-stopped")

    def stop(self, label):
        body = {key: self.play[key] for key in ("ItemId", "MediaSourceId", "PlaySessionId", "SessionId", "PositionTicks")}
        status, _ = self.request(label, "new", "POST", "/emby/Sessions/Playing/Stopped", {**body, "Failed": False, "IsAutomated": False})
        require(status == 204, "The owned playback stop failed.")
        self.play["stopped"] = True
        self.persist()

    def finish(self):
        self.finishing = True
        errors = []
        if self.play and not self.play["stopped"]:
            try:
                self.stop("cleanup-stopped")
            except Exception as error:
                errors.append(type(error).__name__)
        for role in ("new", "admin"):
            if role in self.verified and role not in self.revoked:
                try:
                    self.logout(role)
                except Exception as error:
                    errors.append(type(error).__name__)
        return errors

    def export(self, failure, errors):
        self.identity()
        media_preserved = self.op.verify_media() == self.owner["media"]
        report = {"schemaVersion": 1, "marker": MARKER, "classification": "protocol research; not client acceptance",
                  "referencePID": PID, "referenceStartTicks": TICKS, "requests": self.count, "maximumRequests": MAX_REQUESTS,
                  "newUsername": USERNAME, "newUserId": self.user_id, "seriesId": SERIES, "episodeIdsInOrder": list(EPISODES),
                  "reportedPartialEpisodeId": TARGET, "reportedPositionTicks": POSITION, "baselineUserData": self.baseline,
                  "baselineProjection": self.baseline_projection,
                  "finalItemDetailUserData": self.final, "observations": self.observations, "userAndProtocolHistoryRetained": True,
                  "newConfigurationAndPolicyPreserved": self.profile_preserved, "mediaAndNfoPreserved": media_preserved,
                  "allNewRecorderTokensRevoked": {"admin", "new"} == set(self.tokens) == self.verified == self.revoked,
                  "allAcknowledgedRecorderTokensRevoked": set(self.tokens) == self.verified == self.revoked,
                  "unacknowledgedLoginRoles": sorted(self.login_attempts - set(self.tokens)),
                  "revokedRecorderRoles": sorted(self.revoked), "ownedPlaybackStopped": self.play is None or self.play["stopped"],
                  "existingUserWrites": 0, "libraryConfigurationMutations": 0, "mediaMutations": 0, "oldReferenceRequests": 0,
                  "GobyRequests": 0, "failureType": failure, "cleanupErrors": errors,
                  "evidenceBoundary": "One new user, one three-episode two-season series. The requested experiment only reports S01E02 at 120 of 600 seconds; consult finalItemDetailUserData to determine whether it was reached. No Played=true controls or actual client playback. New owned account, login, and protocol history remain."}
        self.support.collect_secrets(report, self.secrets)
        for label, value in (*self.records.items(), ("report", report)):
            safe = self.support.sanitize(value, self.secrets)
            require(not any(secret and secret in json.dumps(safe, ensure_ascii=False) for secret in self.secrets),
                    "A known recorder secret survived evidence export.")
            self.op.save(EXPORT / (label + ".json"), safe)
        return report


class ContinuedRecorder(Recorder):
    """Continue only the acknowledged new account after the empty-Views gate."""

    def __init__(self, *args):
        super().__init__(*args)
        prior = self.op.read_private(ORIGINAL_ROOT / "export/report.json")
        state = self.op.read_private(ORIGINAL_ROOT / "private/state.json")
        acknowledged = self.op.read_private(ORIGINAL_ROOT / "private/account-owner.json")
        original_marker = "goby-m3e-nextup-later-partial-v1"
        require(prior.get("marker") == state.get("marker") == acknowledged.get("marker") == original_marker and
                prior.get("requests") == state.get("requests") == 11 and prior.get("allNewRecorderTokensRevoked") is True and
                prior.get("failureType") == "ResearchError" and not prior.get("baselineItemDetailUserData") and
                state.get("ownedPlayback") is None and acknowledged.get("username") == USERNAME and
                acknowledged.get("userId") == prior.get("newUserId") == state.get("newUserId") and
                acknowledged.get("serviceIdentity") == self.owner["serviceIdentity"], "The exact owned, unplayed continuation responsibility differs.")
        self.count, self.user_id = 11, acknowledged["userId"]
        self.existing_ids = set(state["existingUserIds"])
        require(self.user_id not in self.existing_ids, "The continuing account was not newly created by the acknowledged study.")
        self.active_libraries_verified = False
        self.baseline_projection = "Shows Episodes list; no claim about omitted count/date fields"
        self.profile_preserved = None
        self.persist()

    def approve(self, role, method, route, body, login=False):
        if role == "new" and method == "GET" and route == "/emby/Shows/" + SERIES + "/Episodes?" + urlencode({"UserId": self.user_id}):
            require(body is None and role in self.verified and role not in self.revoked,
                    "The fresh baseline catalog requires its exact new ordinary recorder.")
            return
        if role == "admin" and not login and route not in ("/emby/Sessions/Logout", "/emby/Sessions"):
            require(role in self.verified and role not in self.revoked, "Continuation setup needs its new verified administrative session.")
            if method == "GET" and route == "/emby/Library/VirtualFolders/Query":
                require(body is None and not self.active_libraries_verified, "Only one current owned-library membership check is allowed.")
                return
            require(method == "POST" and route == "/emby/Users/" + self.user_id + "/Policy" and self.active_libraries_verified and
                    body == self.new_policy and isinstance(body, dict) and body.get("IsAdministrator") is False and
                    body.get("IsDisabled") is False and body.get("EnableAllFolders") is True and body.get("EnabledFolders") == [] and
                    body.get("EnableContentDeletion") is False, "Continuation setup is limited to the already owned new account's verified three-library access.")
            return
        super().approve(role, method, route, body, login)

    def setup(self):
        status, _ = self.request("admin-login", "admin", "POST", "/emby/Users/AuthenticateByName", login=True)
        require(status == 200 and "admin" in self.verified, "The new administrative continuation recorder failed.")
        status, libraries = self.request("active-owned-libraries", "admin", "GET", "/emby/Library/VirtualFolders/Query")
        expected = self.op.read_private(self.op.REPORT)["libraries"]
        require(status == 200 and isinstance(libraries, dict) and len(libraries.get("Items", [])) == 3 and
                {row.get("ItemId") for row in libraries["Items"]} == self.library_ids, "The active reference contains unexpected library membership.")
        for row in libraries["Items"]:
            original = next(item for item in expected if item["ItemId"] == row["ItemId"])
            require(all(row.get(key) == original.get(key) for key in ("Locations", "Name", "CollectionType", "LibraryOptions")),
                    "An active owned library differs from the fixed fixture.")
        self.active_libraries_verified = True
        original = self.op.read_private(ORIGINAL_ROOT / "private/created-user-acknowledgement.json")["user"]
        self.new_policy = dict(original["Policy"], IsAdministrator=False, IsDisabled=False, EnableAllFolders=True, EnabledFolders=[],
                               EnableMediaPlayback=True, EnableAudioPlaybackTranscoding=True, EnableVideoPlaybackTranscoding=True,
                               EnablePlaybackRemuxing=True, EnableContentDeletion=False, EnableContentDownloading=False)
        status, _ = self.request("new-user-basic-access-policy", "admin", "POST", "/emby/Users/" + self.user_id + "/Policy", self.new_policy)
        require(status in (200, 204), "The acknowledged new user's basic library access was not accepted.")
        self.logout("admin")
        status, _ = self.request("new-user-login", "new", "POST", "/emby/Users/AuthenticateByName", login=True)
        require(status == 200 and "new" in self.verified and self.profile["Policy"].get("EnableAllFolders") is True and
                self.profile["Policy"].get("EnabledFolders") == [], "The new ordinary user's effective basic access differs.")
        status, views = self.request("owned-views", "new", "GET", "/emby/Users/" + self.user_id + "/Views")
        require(status == 200 and {row["Id"] for row in views.get("Items", [])} == self.library_ids,
                "The acknowledged new user does not see the three verified owned libraries.")

    def capture(self):
        self.setup()
        status, episodes = self.request("baseline-episodes-list", "new", "GET",
                                        "/emby/Shows/" + SERIES + "/Episodes?" + urlencode({"UserId": self.user_id}))
        require(status == 200 and isinstance(episodes, dict) and len(episodes.get("Items", [])) == 3 and
                {row.get("Id") for row in episodes["Items"]} == set(EPISODES), "The fresh baseline is not the exact three owned episodes.")
        for row in episodes["Items"]:
            require(row.get("Type") == "Episode" and row.get("SeriesId") == SERIES and isinstance(row.get("UserData"), dict) and
                    row["UserData"].get("Played") is False and row["UserData"].get("PlaybackPositionTicks") == 0,
                    "The retained account is not still completely unplayed before the later-episode experiment.")
            self.baseline[row["Id"]] = row["UserData"]
        self.op.save(PRIVATE / "baseline.json", {"userData": self.baseline, "projection": self.baseline_projection, "profile": self.profile})
        self.nextup("baseline-series")
        self.playback()
        for item in EPISODES:
            self.final[item] = self.detail("after-partial", item)
            require(self.final[item].get("Played") is False and
                    self.final[item].get("PlaybackPositionTicks") == (POSITION if item == TARGET else 0),
                    "The none-played, only-S01E02-partial state was not established in all three item details.")
        self.nextup("after-partial-series")


def main(arguments=None):
    global ROOT, PRIVATE, EXPORT, MARKER
    arguments = sys.argv[1:] if arguments is None else arguments
    continued = arguments in (["--continue-owned"], ["_continue-owned"])
    if continued:
        ROOT = WORK / "reference-nextup-later-partial-v2"
        PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
        MARKER = "goby-m3e-nextup-later-partial-v2"
        arguments = ["_capture"] if arguments == ["_continue-owned"] else []
    require(arguments in ([], ["_capture"]), "Usage: reference-nextup-later-partial.py")
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run only through authorized root SSH.")
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_args: (_ for _ in ()).throw(TimeoutError("A bounded study request timed out.")))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    require(operator.service_identity(owner) == owner["serviceIdentity"] and owner["serviceIdentity"]["pid"] == PID and
            owner["serviceIdentity"]["startTicks"] == TICKS and operator.verify_media() == owner["media"] and
            operator.digest(operator.REPORT) == owner["reportSha256"], "The fixed fixture preflight differs.")
    closed = operator.read_private(WORK / "reference-nextup-detail-cleanup-v3/export/final-safety-report.json")
    require(closed.get("actualRequests") == 86 and closed.get("episode17And18FullDetailRestored") is True and
            closed.get("series14AndEpisode19FullDetailMatchedInCalibration") is True and
            closed.get("allThreeRecorderTokensRevokedWith401Proof") is True and
            closed.get("prefsConfigurationPolicyAndMediaPreserved") is True,
            "The preceding viewer2 cleanup lacks its complete full-detail and token proof.")
    if arguments == ["_capture"]:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT), "username": USERNAME},
                "The new account research owner marker differs.")
        intent = operator.read_private(PRIVATE / "intent.json")
        require(operator.process_identity(os.getppid()) == intent["launcher"] and intent["sourceSha256"] == operator.digest(Path(__file__).absolute()) and
                not operator.present(PRIVATE / "state.json"), "An interrupted or unrelated study cannot be automatically replayed.")
        recorder_class = ContinuedRecorder if continued else Recorder
        recorder = recorder_class(support, operator, owner, operator.read_private(operator.BROWSER), operator.read_private(operator.REPORT),
                                  operator.read_private(PRIVATE / "credential.json"))
        failure = None
        try:
            recorder.capture()
        except Exception as error:
            failure = type(error).__name__
        errors = recorder.finish()
        report = recorder.export(failure, errors)
        require(failure is None and not errors and report["allNewRecorderTokensRevoked"] and report["ownedPlaybackStopped"] and
                report["mediaAndNfoPreserved"], "The new account study is incomplete; evidence and responsibility are retained.")
        print(json.dumps({"result": "complete", "requests": recorder.count, "report": str(EXPORT / "report.json"),
                          "observations": recorder.observations, "newRecorderTokensRevoked": True}), flush=True)
        return
    require(not operator.present(ROOT), "Existing research evidence, ownership, and account credentials will not be overwritten or replayed.")
    ROOT.mkdir(mode=0o700)
    operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT), "username": USERNAME})
    PRIVATE.mkdir(mode=0o700)
    EXPORT.mkdir(mode=0o700)
    lock = os.open(ROOT / "run.lock", os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    credential = operator.read_private(ORIGINAL_ROOT / "private/credential.json") if continued else {"username": USERNAME, "password": secrets.token_urlsafe(32)}
    operator.save(PRIVATE / "credential.json", dict(credential, marker=MARKER))
    operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": operator.digest(Path(__file__).absolute()),
                  "launcher": operator.process_identity(os.getpid()), "serviceIdentity": owner["serviceIdentity"], "maximumRequests": MAX_REQUESTS,
                  "scope": "Continue the precisely acknowledged owned account; preserve both attempts; only S01E02 partial playback."
                  if continued else "Create and retain a new ordinary user; only S01E02 partial playback; no existing user state mutations."})
    descriptor = os.open(f"/proc/{PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(descriptor).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]), "The retained network namespace handle differs.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(descriptor), "/usr/bin/python3", "-I", "-B",
                                 str(Path(__file__).absolute()), "_continue-owned" if continued else "_capture"], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=420, check=False, pass_fds=(descriptor, lock),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "The new account research failed; inspect its retained private state and sanitized report.")
        print(result.stdout.decode().strip())
    finally:
        os.close(descriptor)
        os.close(lock)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"result": "failed", "failureType": type(error).__name__, "evidenceRetained": True}), file=sys.stderr)
        sys.exit(1)
