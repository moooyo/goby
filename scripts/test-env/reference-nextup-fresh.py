#!/usr/bin/env python3
"""Research global NextUp on the owned fresh 30 fps, 600-second TV fixture.

Only viewer2 state for its three known episodes and series may change. Public
API restoration is proven before playback controls. No library, metadata,
policy, password, media, old reference, or Goby mutation is permitted. Playback
reports are protocol research and do not claim real-time client acceptance.
"""

from __future__ import annotations

import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import time
from urllib.parse import urlencode, urlsplit, parse_qs

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
ROOT = WORK / "reference-nextup-v1"
PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
MARKER = "goby-m3e-fresh-nextup-v1"
DEVICE = "goby-m3e-nextup-protocol-recorder-v1"
PID, TICKS, PORT = 332054, "357218", 18097
MAX_REQUESTS, MAX_BODY = 80, 256 * 1024
EXPECTED_PREFS = {"genreLimitOnDetails": "1"}


class NextUpError(Exception):
    """A bounded NextUp research or restoration proof failed."""


def require(condition, message):
    if not condition:
        raise NextUpError(message)


def load_support():
    path = Path(__file__).absolute().with_name("reference-client-initialization.py")
    expected = json.loads((WORK / "reference-client-initialization-v1/export/safety-report.json").read_text())["sourceSha256"]
    require(hashlib.sha256(path.read_bytes()).hexdigest() == expected, "The accepted sanitizer support source changed.")
    spec = importlib.util.spec_from_file_location("nextup_initialization_support", path)
    support = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(support)
    return support, support.load_operator()


class Recorder:
    def __init__(self, support, operator, owner, browser, fixture):
        self.support, self.op, self.owner = support, operator, owner
        self.account = browser["accounts"]["viewer2"]
        self.user_id = self.account["userId"]
        self.secrets = {value["password"] for value in browser["accounts"].values()}
        series = [row for row in fixture["items"] if row["Type"] == "Series"]
        episodes = [row for row in fixture["items"] if row["Type"] == "Episode"]
        seasons = [row for row in fixture["items"] if row["Type"] == "Season"]
        libraries = [row for row in fixture["libraries"] if row["CollectionType"] == "tvshows"]
        require(len(series) == 1 and len(episodes) == 3 and len(seasons) == 2 and len(libraries) == 1,
                "The fixed reference does not contain exactly one owned three-episode TV series.")
        self.series_id, self.library_id = series[0]["Id"], libraries[0]["ItemId"]
        self.episodes = sorted(episodes, key=lambda row: (row["ParentIndexNumber"], row["IndexNumber"]))
        require([(row["ParentIndexNumber"], row["IndexNumber"]) for row in self.episodes] == [(1, 1), (1, 2), (2, 1)] and
                all(row.get("SeriesId") == self.series_id for row in self.episodes + seasons), "The owned series relations differ.")
        self.episode_ids = {row["Id"] for row in self.episodes}
        self.mutable_ids = self.episode_ids | {self.series_id}
        self.family_ids = self.mutable_ids | {row["Id"] for row in seasons}
        self.token, self.session_id, self.token_verified = None, None, False
        self.revoked, self.count, self.finishing = False, 0, False
        self.records, self.observations, self.baseline, self.current = {}, [], None, None
        self.play_sessions = {}
        self.restoration_proven = False
        self.restoration_mode = None
        self.mutated = False
        self.baseline_queries = None
        self.profile = None
        self.data_restored = self.prefs_preserved = self.profile_preserved = False
        self.final_queries_match = False
        self.started = time.monotonic()
        self.persist()

    def persist(self):
        state = {"marker": MARKER, "worker": self.op.process_identity(os.getpid()), "serviceIdentity": self.owner["serviceIdentity"],
                 "requests": self.count, "token": self.token, "tokenVerified": self.token_verified, "tokenRevoked": self.revoked,
                 "sessionId": self.session_id, "playSessions": self.play_sessions, "baseline": self.baseline,
                 "current": self.current, "restorationProven": self.restoration_proven, "restorationMode": self.restoration_mode,
                 "mutated": self.mutated, "dataRestored": self.data_restored}
        path = PRIVATE / "state.json"
        if self.op.present(path):
            require(self.op.read_private(path).get("marker") == MARKER, "An unrelated private state cannot be replaced.")
            temporary = PRIVATE / ("state.next-" + secrets.token_hex(10) + ".json")
            self.op.save(temporary, state)
            os.replace(temporary, path)
            self.op.sync_directory(PRIVATE)
        else:
            self.op.save(path, state)

    def identity(self):
        require(self.op.service_identity(self.owner) == self.owner["serviceIdentity"], "The owned reference process changed.")
        identity = self.owner["serviceIdentity"]
        require(identity["pid"] == PID and identity["startTicks"] == TICKS and
                os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "NextUp HTTP escaped the exact fresh namespace.")

    def approve(self, method, route, body):
        parsed, query = urlsplit(route), parse_qs(urlsplit(route).query, keep_blank_values=True)
        require(not parsed.scheme and not parsed.netloc and not parsed.fragment, "An external NextUp route is forbidden.")
        prefix = "/emby/Users/" + self.user_id
        if parsed.path == "/emby/Users/AuthenticateByName":
            require(method == "POST" and body is None and not query, "Unexpected recorder authentication body.")
            return
        require(self.token_verified, "No authenticated observation or mutation is allowed before recorder identity proof.")
        if parsed.path == "/emby/Sessions/Logout":
            require(method == "POST" and body is None and not query, "Only the new recorder session may be logged out.")
            return
        if method == "GET":
            require(body is None, "Read observations cannot contain request bodies.")
            if parsed.path in (prefix, "/emby/usersettings/" + self.user_id, "/emby/Sessions", "/emby/System/Info/Public"):
                require(not query, "A read-only identity control has unexpected parameters.")
            elif parsed.path in {prefix + "/Items/" + item for item in self.mutable_ids}:
                require(not query, "A detail-projection check has unexpected parameters.")
            elif parsed.path == prefix + "/Items":
                require(set(query) <= {"Ids", "Fields", "Limit", "EnableImages"} and
                        set(query.get("Ids", [""])[0].split(",")) == self.family_ids and query.get("Limit") == ["10"],
                        "The catalog observation is not the exact owned series family.")
            elif parsed.path == "/emby/Shows/" + self.series_id + "/Episodes":
                require(query == {"UserId": [self.user_id]}, "The family-membership observation differs.")
            elif parsed.path == "/emby/Shows/NextUp":
                require(set(query) <= {"UserId", "Limit", "SeriesId", "ParentId", "Fields", "EnableImages", "EnableUserData"} and
                        query.get("UserId") == [self.user_id] and query.get("Limit") == ["10"] and
                        ("SeriesId" not in query or query["SeriesId"] == [self.series_id]) and
                        ("ParentId" not in query or query["ParentId"] in ([self.library_id], [self.series_id])),
                        "A NextUp query is outside the owned user and parent controls.")
            else:
                raise NextUpError("The read is outside the exact fresh NextUp study.")
            return
        require(self.baseline is not None, "User-state changes require a durable complete baseline.")
        if parsed.path.startswith(prefix + "/PlayedItems/"):
            require(method in ("POST", "DELETE") and parsed.path.rsplit("/", 1)[1] in self.mutable_ids and body is None and not query,
                    "A watched-state mutation targets an unowned item or user.")
        elif parsed.path.startswith(prefix + "/Items/") and parsed.path.endswith("/UserData"):
            item = parsed.path.split("/")[-2]
            require(method == "POST" and item in self.mutable_ids and isinstance(body, dict) and not query and
                    set(body) <= {"PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "LastPlayedDate", "Rating", "Likes",
                                  "PlayedPercentage", "UnplayedItemCount", "Key", "ItemId", "ServerId"},
                    "A public UserData update is outside the exact owned item data fields.")
        elif parsed.path.startswith("/emby/Items/") and parsed.path.endswith("/PlaybackInfo"):
            item = parsed.path.split("/")[-2]
            require(method == "POST" and item in self.episode_ids and body == {"UserId": self.user_id, "IsPlayback": True} and not query and
                    self.restoration_proven, "Playback negotiation needs a proven restoration path and an owned episode.")
        elif parsed.path in ("/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"):
            require(method == "POST" and isinstance(body, dict) and not query and self.restoration_proven and
                    body.get("ItemId") in self.episode_ids and body.get("PlaySessionId") in self.play_sessions and
                    body.get("SessionId") == self.session_id and 0 <= body.get("PositionTicks", -1) <= 6000000000,
                    "A playback report does not identify an owned negotiated recorder session and finite source position.")
            owned = self.play_sessions[body["PlaySessionId"]]
            require(body.get("ItemId") == owned["ItemId"] and body.get("MediaSourceId") == owned["MediaSourceId"],
                    "A playback report changed its owned source identity.")
        else:
            raise NextUpError("Policy, preferences, libraries, scans, other accounts, and media writes are outside this study.")

    def request(self, label, method, route, body=None, *, login=False):
        self.identity()
        require(self.count < MAX_REQUESTS and time.monotonic() - self.started < (300 if self.finishing else 120),
                "The bounded NextUp request or time budget is exhausted.")
        self.approve(method, route, body)
        number = self.count + 1
        self.count = number
        self.persist()
        self.op.save(PRIVATE / f"{number:03d}-{label}-intent.json", {"marker": MARKER, "method": method, "path": route,
                     "body": body, "authenticationBodyRetained": False, "userDataBefore": self.current, "finishing": self.finishing})
        headers = {"Accept": "application/json", "Authorization":
                   'Emby Client="Goby Fresh NextUp Protocol Research", Device="Linux Protocol Research", DeviceId="' + DEVICE + '", Version="1.0"'}
        if login:
            payload = urlencode({"Username": self.account["username"], "Pw": self.account["password"]}).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        else:
            headers["X-Emby-Token"] = self.token
            payload = json.dumps(body, separators=(",", ":")).encode() if body is not None else None
            if body is not None:
                headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=8)
        try:
            signal.setitimer(signal.ITIMER_REAL, 8)
            connection.request(method, route, body=payload, headers=headers)
            response = connection.getresponse()
            status, response_headers = response.status, dict(response.getheaders())
            raw = response.read(MAX_BODY)
            require(len(raw) < MAX_BODY, "The NextUp response exceeded its bound.")
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        try:
            result = json.loads(raw) if raw else None
        except (ValueError, UnicodeDecodeError):
            result = raw.decode("utf-8", errors="replace")
        if login and isinstance(result, dict) and result.get("AccessToken"):
            self.token = result["AccessToken"]
            self.secrets.add(self.token)
            self.op.save(PRIVATE / "recorder-login.json", result)
            self.persist()
            require(status == 200 and result.get("ServerId") == self.owner["serverId"] and
                    result.get("User", {}).get("Id") == self.user_id and result["User"].get("Name") == self.account["username"] and
                    result["User"].get("Policy", {}).get("IsAdministrator") is False and
                    result.get("SessionInfo", {}).get("DeviceId") == DEVICE,
                    "The acknowledged token is not the exact new ordinary NextUp recorder.")
            self.token_verified = True
            self.session_id = result["SessionInfo"]["Id"]
            self.profile = {key: result["User"].get(key) for key in ("Configuration", "Policy")}
            self.persist()
        self.identity()
        record = {"classification": "protocol research; not client acceptance", "case": label,
                  "request": {"method": method, "path": route, "body": body, "authenticationBodyRetained": False},
                  "response": {"status": status, "headers": response_headers, "body": result, "bytesRead": len(raw)}}
        self.support.collect_secrets(record, self.secrets)
        safe = self.support.sanitize(record, self.secrets)
        self.op.save(PRIVATE / f"{number:03d}-{label}-response.json", safe)
        self.records[label] = safe
        return status, result

    def snapshot(self, label):
        query = urlencode({"Ids": ",".join(sorted(self.family_ids)), "Fields": "Path,ParentId,MediaSources,MediaStreams,DateCreated,PremiereDate,ProductionYear,Overview,UserData",
                           "Limit": 10, "EnableImages": "false"})
        status, result = self.request(label, "GET", "/emby/Users/" + self.user_id + "/Items?" + query)
        require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list) and
                {row.get("Id") for row in result["Items"]} == self.family_ids, "The complete owned family state is unavailable.")
        rows = {row["Id"]: row for row in result["Items"]}
        state = {key: row["UserData"] for key, row in rows.items()}
        self.current = state
        self.persist()
        return rows, state

    def nextup(self, stage):
        group = {}
        for label, extra in (("global", {}), ("series", {"SeriesId": self.series_id}),
                             ("tv-parent", {"ParentId": self.library_id}), ("series-parent", {"ParentId": self.series_id})):
            query = urlencode({"UserId": self.user_id, "Limit": 10, "Fields": "UserData,ParentId,MediaSources", "EnableImages": "false", **extra})
            status, result = self.request(stage + "-nextup-" + label, "GET", "/emby/Shows/NextUp?" + query)
            require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list), "A bounded NextUp query failed.")
            require(all(row.get("Id") in self.family_ids for row in result["Items"]), "NextUp returned an item outside the owned three-episode family.")
            group[label] = {"ids": [row["Id"] for row in result["Items"]], "types": [row.get("Type") for row in result["Items"]],
                            "total": result.get("TotalRecordCount")}
        self.observations.append({"stage": stage, "userData": self.current, "queries": group})
        return group

    def restore_body(self, item):
        body = dict(self.baseline[item])
        body.setdefault("LastPlayedDate", None)
        return body

    def userdata(self, label, item, body):
        self.mutated, self.data_restored = True, False
        self.persist()
        status, _ = self.request(label, "POST", "/emby/Users/" + self.user_id + "/Items/" + item + "/UserData", body)
        require(status in (200, 204), "The owned public UserData update was rejected.")

    def restore(self, label):
        if not self.mutated:
            return
        if self.restoration_mode == "delete-unplayed":
            for item in sorted(self.episode_ids):
                status, _ = self.request(label + "-delete-unplayed-" + item, "DELETE", "/emby/Users/" + self.user_id + "/PlayedItems/" + item)
                require(status == 200, "The episode-only unplayed restoration was rejected.")
        else:
            for item in [*sorted(self.episode_ids), self.series_id]:
                self.userdata(label + "-" + item, item, self.restore_body(item))
        _, state = self.snapshot(label + "-verified")
        require(state == self.baseline, "The complete episode/season/series UserData baseline was not restored.")
        self.data_restored, self.mutated = True, False
        self.persist()

    def playback(self, stage, item, position):
        require(self.count <= 55, "The playback controls must preserve the complete cleanup budget.")
        status, info = self.request(stage + "-info", "POST", "/emby/Items/" + item + "/PlaybackInfo",
                                    {"UserId": self.user_id, "IsPlayback": True})
        require(status == 200 and isinstance(info, dict) and info.get("PlaySessionId") and info.get("MediaSources"),
                "Playback negotiation did not produce an owned play session.")
        source = info["MediaSources"][0]
        require(source.get("RunTimeTicks") == 6000000000 and source.get("Id"), "The negotiated episode is not the fixed 600-second fixture.")
        context = {"ItemId": item, "MediaSourceId": source["Id"], "PlaySessionId": info["PlaySessionId"], "SessionId": self.session_id}
        self.secrets.add(info["PlaySessionId"])
        self.play_sessions[info["PlaySessionId"]] = {**context, "stopped": False, "PositionTicks": 0}
        self.persist()
        self.mutated, self.data_restored = True, False
        self.persist()
        started = {**context, "RunTimeTicks": 6000000000, "PositionTicks": 0, "CanSeek": True, "IsPaused": False,
                   "IsMuted": False, "PlayMethod": "DirectStream", "PlaybackRate": 1}
        status, _ = self.request(stage + "-started", "POST", "/emby/Sessions/Playing", started)
        require(status == 204, "The owned playback start was not acknowledged.")
        status, _ = self.request(stage + "-progress", "POST", "/emby/Sessions/Playing/Progress",
                                {**started, "PositionTicks": position, "EventName": "TimeUpdate"})
        require(status == 204, "The owned playback progress was not acknowledged.")
        self.play_sessions[info["PlaySessionId"]]["PositionTicks"] = position
        self.persist()
        status, _ = self.request(stage + "-stopped", "POST", "/emby/Sessions/Playing/Stopped",
                                {**context, "PositionTicks": position, "Failed": False, "IsAutomated": False})
        require(status == 204, "The owned playback stop was not acknowledged.")
        self.play_sessions[info["PlaySessionId"]]["stopped"] = True
        self.persist()
        self.snapshot(stage + "-state")
        observed = self.current[item]
        self.observations.append({"stage": stage + "-playback-state-proof", "itemId": item, "reportedPositionTicks": position,
                                  "observedUserData": observed, "historyEstablished": observed.get("PlayCount", 0) > 0 and bool(observed.get("LastPlayedDate")),
                                  "requestedPartialStateEstablished": position < 6000000000 and observed.get("Played") is False and observed.get("PlaybackPositionTicks") == position,
                                  "requestedCompletionEstablished": position == 6000000000 and observed.get("Played") is True})
        self.nextup(stage)

    def capture(self):
        status, _ = self.request("recorder-login", "POST", "/emby/Users/AuthenticateByName", login=True)
        require(status == 200 and self.token_verified, "The new NextUp recorder login failed.")
        status, prefs = self.request("prefs-baseline", "GET", "/emby/usersettings/" + self.user_id)
        require(status == 200 and prefs == EXPECTED_PREFS, "The acknowledged viewer2 preferences changed.")
        rows, baseline = self.snapshot("family-baseline")
        status, membership = self.request("series-membership", "GET", "/emby/Shows/" + self.series_id + "/Episodes?" + urlencode({"UserId": self.user_id}))
        require(status == 200 and {row["Id"] for row in membership.get("Items", [])} == self.episode_ids,
                "The series has descendants outside the three explicitly owned episodes.")
        for item in self.episode_ids:
            row = rows[item]
            videos = [stream for source in row.get("MediaSources", []) for stream in source.get("MediaStreams", []) if stream.get("Type") == "Video"]
            require(row.get("RunTimeTicks") == 6000000000 and row.get("MediaSources") and
                    videos and all(stream.get("AverageFrameRate") == 30 and stream.get("RealFrameRate") == 30 for stream in videos),
                    "The current episode DTO differs from the normal 30 fps, 600-second fixture.")
            require(baseline[item].get("Played") is False and baseline[item].get("PlayCount") == 0 and
                    baseline[item].get("PlaybackPositionTicks") == 0 and not baseline[item].get("LastPlayedDate"),
                    "The owned episodes are not in the safely restorable unstarted baseline.")
        self.baseline = baseline
        self.op.save(PRIVATE / "baseline.json", {"userData": baseline, "profile": self.profile, "prefs": prefs,
                                                 "familyDTOs": rows, "media": self.owner["media"]})
        self.persist()
        self.baseline_queries = self.nextup("baseline")
        first = self.episodes[0]["Id"]
        self.userdata("restore-noop", first, self.restore_body(first))
        require(self.snapshot("restore-noop-verified")[1] == self.baseline, "The public UserData no-op changed baseline fields.")
        self.mutated = True
        self.persist()
        status, _ = self.request("restore-control-mark-played", "POST", "/emby/Users/" + self.user_id + "/PlayedItems/" + first)
        require(status == 200 and self.snapshot("restore-control-played-state")[1][first].get("Played") is True,
                "The reversible played-state control was not established.")
        self.userdata("restore-control-update-baseline", first, self.restore_body(first))
        restored = self.snapshot("restore-control-update-result")[1]
        if restored != self.baseline:
            status, _ = self.request("restore-control-delete-unplayed", "DELETE", "/emby/Users/" + self.user_id + "/PlayedItems/" + first)
            require(status == 200 and self.snapshot("restore-control-delete-result")[1] == self.baseline,
                    "Neither public restore route recovered the exact owned baseline.")
            self.restoration_mode = "delete-unplayed"
        else:
            self.restoration_mode = "userdata-baseline"
        self.restoration_proven, self.data_restored, self.mutated = True, True, False
        self.op.save(PRIVATE / "restoration-proof.json", {"mode": self.restoration_mode, "exactFamilyUserDataRestored": True})
        self.persist()
        self.mutated = True
        self.persist()
        status, _ = self.request("manual-first-played", "POST", "/emby/Users/" + self.user_id + "/PlayedItems/" + first)
        _, played = self.snapshot("manual-first-state")
        require(status == 200 and played[first].get("Played") is True, "The manual played-state control was not established.")
        self.nextup("manual-first")
        actual_date = played[first].get("LastPlayedDate")
        if actual_date and self.restoration_mode == "userdata-baseline":
            series_state = dict(self.current[self.series_id], LastPlayedDate=actual_date)
            self.userdata("series-public-date-control", self.series_id, series_state)
            self.snapshot("series-public-date-state")
            self.observations.append({"stage": "series-public-date-application", "requestedLastPlayedDate": actual_date,
                                      "observedSeriesUserData": self.current[self.series_id],
                                      "dateActuallyApplied": self.current[self.series_id].get("LastPlayedDate") == actual_date})
            self.nextup("series-public-date")
        self.restore("pre-playback-baseline")
        self.playback("partial-first-120s", first, 1200000000)
        self.playback("complete-first-600s", first, 6000000000)
        if self.count <= 55:
            self.playback("partial-second-after-first-complete", self.episodes[1]["Id"], 1200000000)

    def finish(self):
        self.finishing = True
        errors = []
        for key, context in self.play_sessions.items():
            if context["stopped"]:
                continue
            try:
                status, _ = self.request("cleanup-stop-" + context["ItemId"], "POST", "/emby/Sessions/Playing/Stopped",
                                        {name: context[name] for name in ("ItemId", "MediaSourceId", "PlaySessionId", "SessionId", "PositionTicks")} |
                                        {"Failed": False, "IsAutomated": False})
                require(status == 204, "A pending owned playback session did not stop.")
                context["stopped"] = True
                self.persist()
            except Exception as error:
                errors.append(type(error).__name__)
        if self.baseline is not None:
            try:
                require(all(row["stopped"] for row in self.play_sessions.values()),
                        "Data restoration cannot race an unconfirmed owned playback stop.")
                self.restore("final-baseline")
                self.final_queries_match = self.nextup("restored") == self.baseline_queries
                status, prefs = self.request("prefs-final", "GET", "/emby/usersettings/" + self.user_id)
                self.prefs_preserved = status == 200 and prefs == EXPECTED_PREFS
                status, dto = self.request("profile-final", "GET", "/emby/Users/" + self.user_id)
                self.profile_preserved = status == 200 and all(dto.get(key) == value for key, value in self.profile.items())
            except Exception as error:
                errors.append(type(error).__name__)
        if self.token_verified:
            try:
                status, _ = self.request("recorder-logout", "POST", "/emby/Sessions/Logout")
                require(status in (204, 401), "The new NextUp recorder logout failed.")
                status, _ = self.request("recorder-token-invalid", "GET", "/emby/Sessions")
                require(status == 401, "The new NextUp recorder token remains usable.")
                self.revoked = True
                self.persist()
            except Exception as error:
                errors.append(type(error).__name__)
        return errors

    def export(self, failure, cleanup_errors):
        media_preserved = self.op.verify_media() == self.owner["media"]
        self.identity()
        report = {"schemaVersion": 1, "marker": MARKER, "classification": "protocol research; not client acceptance",
                  "referencePID": PID, "referenceStartTicks": TICKS, "requests": self.count, "maximumRequests": MAX_REQUESTS,
                  "seriesId": self.series_id, "episodeIdsInOrder": [row["Id"] for row in self.episodes], "libraryId": self.library_id,
                  "observations": self.observations, "restorationMode": self.restoration_mode, "restorationProven": self.restoration_proven,
                  "exactFamilyUserDataRestored": self.data_restored, "finalNextUpQueriesEqualBaseline": self.final_queries_match,
                  "prefsPreserved": self.prefs_preserved, "configurationAndPolicyPreserved": self.profile_preserved,
                  "mediaAndNfoPreserved": media_preserved, "allOwnedPlaybackSessionsStopped": all(row["stopped"] for row in self.play_sessions.values()),
                  "newRecorderRevoked": self.revoked, "failureType": failure, "cleanupErrors": cleanup_errors,
                  "retainedOwnedHistory": ["New recorder login/logout and device history", "Owned playback negotiation/report history", "Owned UserData and played-state audit history"],
                  "oldReferenceRequests": 0, "GobyRequests": 0, "libraryConfigurationPolicyOrMediaMutations": 0,
                  "playbackEvidenceBoundary": "Started/Progress/Stopped are client-reported protocol controls, not real-time playback or client acceptance."}
        self.support.collect_secrets(report, self.secrets)
        for name, value in (*self.records.items(), ("report", report)):
            safe = self.support.sanitize(value, self.secrets)
            encoded = json.dumps(safe, ensure_ascii=False)
            require(not any(secret and secret in encoded for secret in self.secrets), "A recorder secret survived NextUp export.")
            self.op.save(EXPORT / (name + ".json"), safe)
        return report


def capture(support, operator, owner):
    fixture = operator.read_private(operator.REPORT)
    require(operator.digest(operator.REPORT) == owner["reportSha256"], "The fixed fixture identity report changed.")
    recorder = Recorder(support, operator, owner, operator.read_private(operator.BROWSER), fixture)
    failure = None
    try:
        recorder.capture()
    except Exception as error:
        failure = type(error).__name__
    cleanup_errors = recorder.finish()
    report = recorder.export(failure, cleanup_errors)
    require(failure is None and not cleanup_errors and recorder.data_restored and recorder.prefs_preserved and
            recorder.profile_preserved and recorder.revoked and report["mediaAndNfoPreserved"] and
            recorder.final_queries_match and report["allOwnedPlaybackSessionsStopped"],
            "The bounded NextUp research or exact restoration is incomplete; inspect retained evidence.")
    print(json.dumps({"result": "complete", "requests": recorder.count, "report": str(EXPORT / "report.json"),
                      "exactUserDataRestored": recorder.data_restored, "newRecorderRevoked": recorder.revoked}), flush=True)


def projection_capture(support, operator, owner):
    original_root = WORK / "reference-nextup-v1"
    prior = operator.read_private(original_root / "export/report.json")
    original = operator.read_private(original_root / "private/baseline.json")
    require(prior.get("requests") == 65 and prior.get("exactFamilyUserDataRestored") is True and
            prior.get("newRecorderRevoked") is True, "The first NextUp study lacks its retained closure proof.")
    recorder = Recorder(support, operator, owner, operator.read_private(operator.BROWSER), operator.read_private(operator.REPORT))
    recorder.count, recorder.baseline = 65, original["userData"]
    recorder.restoration_proven, recorder.restoration_mode = True, prior["restorationMode"]
    recorder.persist()
    details, differences, errors = {}, {}, []
    history_fields = {"PlaybackPositionTicks", "PlayCount", "LastPlayedDate", "Played", "IsFavorite", "Rating"}
    try:
        status, _ = recorder.request("projection-recorder-login", "POST", "/emby/Users/AuthenticateByName", login=True)
        require(status == 200 and recorder.token_verified, "The projection recorder login failed.")
        require(recorder.profile == original["profile"], "The viewer2 configuration or policy differs from the original study baseline.")
        for item in sorted(recorder.mutable_ids):
            status, dto = recorder.request("detail-restored-" + item, "GET", "/emby/Users/" + recorder.user_id + "/Items/" + item)
            require(status == 200 and dto.get("Id") == item and isinstance(dto.get("UserData"), dict), "The full item-detail UserData is unavailable.")
            details[item] = dto["UserData"]
            changed = {key: {"baseline": recorder.baseline[item].get(key), "detail": dto["UserData"].get(key)}
                       for key in history_fields if recorder.baseline[item].get(key) != dto["UserData"].get(key)}
            if changed:
                differences[item] = changed
        _, listed = recorder.snapshot("current-list-projection")
        for item in differences:
            require(recorder.count + 2 <= MAX_REQUESTS - 3, "A detail-level restore must preserve preference and token cleanup budget.")
            recorder.userdata("detail-calibrated-restore-" + item, item, recorder.restore_body(item))
            status, dto = recorder.request("detail-restore-proof-" + item, "GET", "/emby/Users/" + recorder.user_id + "/Items/" + item)
            require(status == 200 and all(dto["UserData"].get(key) == recorder.baseline[item].get(key) for key in history_fields),
                    "The full-detail historical fields did not return to their acknowledged zero baseline.")
            details[item] = dto["UserData"]
        status, prefs = recorder.request("prefs-final", "GET", "/emby/usersettings/" + recorder.user_id)
        require(status == 200 and prefs == EXPECTED_PREFS, "The acknowledged preference baseline changed.")
    except Exception as error:
        errors.append(type(error).__name__)
    finally:
        if recorder.token_verified:
            try:
                status, _ = recorder.request("projection-recorder-logout", "POST", "/emby/Sessions/Logout")
                require(status in (204, 401), "The projection recorder logout failed.")
                status, _ = recorder.request("projection-token-invalid", "GET", "/emby/Sessions")
                require(status == 401, "The projection recorder token invalidity is unproven.")
                recorder.revoked = True
                recorder.persist()
            except Exception as error:
                errors.append(type(error).__name__)
    media_preserved = operator.verify_media() == owner["media"]
    recorder.identity()
    report = {"schemaVersion": 1, "marker": MARKER, "classification": "readback/projection calibration; not client acceptance",
              "priorRequests": 65, "requestsThisCalibration": recorder.count - 65, "totalNextUpStudyRequests": recorder.count,
              "originalListUserDataBaseline": original["userData"], "fullItemDetailUserDataAfterRestoration": details,
              "historicalFieldDifferencesObserved": differences, "mutableHistoryFieldsCompared": sorted(history_fields),
              "fullDetailRestoredToAcknowledgedHistoryBaseline": not errors and len(details) == 4,
              "rawDetailExactlyMatchesOriginalListBaseline": {item: value == original["userData"][item] for item, value in details.items()},
              "newPlaybackOrWatchedExperiments": 0, "onlyMismatchedOwnedUserDataFieldsRestored": list(differences),
              "newRecorderRevoked": recorder.revoked, "mediaPreserved": media_preserved, "errors": errors,
              "evidenceBoundary": "The earlier playback-stage snapshots were list projections; zero/missing count/date there do not prove that internal history did not change. This calibration validates the current restored detail fields, not arbitrary nonzero date/count or remembered-track restoration."}
    for name, value in (*recorder.records.items(), ("report", report)):
        support.collect_secrets(value, recorder.secrets)
        safe = support.sanitize(value, recorder.secrets)
        require(not any(secret and secret in json.dumps(safe) for secret in recorder.secrets), "A recorder secret survived projection export.")
        operator.save(EXPORT / (name + ".json"), safe)
    require(not errors and recorder.revoked and media_preserved, "The projection/restoration calibration is incomplete.")
    print(json.dumps({"result": "complete", "totalNextUpStudyRequests": recorder.count,
                      "historicalFieldDifferences": differences, "report": str(EXPORT / "report.json")}), flush=True)


def projection_main(internal=False):
    global ROOT, PRIVATE, EXPORT, MARKER, DEVICE
    ROOT = WORK / "reference-nextup-projection-v2"
    PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
    MARKER = "goby-m3e-nextup-projection-v2"
    DEVICE = "goby-m3e-nextup-projection-recorder-v2"
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run only through authorized root SSH.")
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_args: (_ for _ in ()).throw(TimeoutError("A projection HTTP request exceeded its bound.")))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    require(operator.service_identity(owner) == owner["serviceIdentity"] and owner["serviceIdentity"]["pid"] == PID and
            owner["serviceIdentity"]["startTicks"] == TICKS, "The fixed fresh reference changed before projection calibration.")
    if internal:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT)}, "Projection evidence ownership differs.")
        projection_capture(support, operator, owner)
        return
    require(not operator.present(ROOT), "Existing projection evidence will not be replaced or rerun.")
    ROOT.mkdir(mode=0o700)
    operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT)})
    PRIVATE.mkdir(mode=0o700)
    EXPORT.mkdir(mode=0o700)
    operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  "serviceIdentity": owner["serviceIdentity"], "priorRequests": 65, "maximumTotalRequests": MAX_REQUESTS,
                  "scope": "Current full item-detail restoration verification only; no new playback or watched-state experiments."})
    descriptor = os.open(f"/proc/{PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(descriptor).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]), "The projection namespace handle differs.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(descriptor), "/usr/bin/python3", "-I", "-B",
                                 str(Path(__file__).absolute()), "_projection-check"], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=150, check=False, pass_fds=(descriptor,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "Projection verification failed; exact private cleanup state is retained.")
        print(result.stdout.decode().strip())
    finally:
        os.close(descriptor)


class DetailCleanupRecorder(Recorder):
    def approve(self, method, route, body):
        prefix = "/emby/Users/" + self.user_id
        allowed = {"POST": {"/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"},
                   "DELETE": {prefix + "/PlayedItems/17", prefix + "/PlayedItems/18"},
                   "GET": {prefix + "/Items/17", prefix + "/Items/18", prefix,
                           "/emby/usersettings/" + self.user_id, "/emby/Sessions"}}
        require(body is None and route in allowed.get(method, set()), "Detail cleanup is limited to the two acknowledged episodes and account preservation reads.")
        if route != "/emby/Users/AuthenticateByName":
            require(self.token_verified, "Cleanup requires the exact newly acknowledged recorder token.")


def detail_cleanup_capture(support, operator, owner):
    original = operator.read_private(WORK / "reference-nextup-v1/private/baseline.json")
    calibration = operator.read_private(WORK / "reference-nextup-projection-v2/export/report.json")
    require(calibration.get("totalNextUpStudyRequests") == 75 and calibration.get("newRecorderRevoked") is True and
            set(calibration.get("historicalFieldDifferencesObserved", {})) == {"17", "18"},
            "The precise two-episode detail cleanup responsibility differs.")
    for item in ("17", "18"):
        data = original["userData"][item]
        require("LastPlayedDate" not in data and data.get("Played") is False and data.get("PlaybackPositionTicks") == 0 and
                data.get("PlayCount") == 0, "An acknowledged original episode baseline differs.")
    recorder = DetailCleanupRecorder(support, operator, owner, operator.read_private(operator.BROWSER), operator.read_private(operator.REPORT))
    recorder.count, recorder.baseline = 75, original["userData"]
    recorder.persist()
    results, errors = {}, []
    try:
        status, _ = recorder.request("cleanup-recorder-login", "POST", "/emby/Users/AuthenticateByName", login=True)
        require(status == 200 and recorder.profile == original["profile"], "The cleanup recorder account or profile differs.")
        for item in ("17", "18"):
            route = "/emby/Users/" + recorder.user_id + "/Items/" + item
            status, before = recorder.request("before-delete-" + item, "GET", route)
            require(status == 200 and before.get("Id") == item and before.get("UserData") == calibration["fullItemDetailUserDataAfterRestoration"][item],
                    "The acknowledged lingering-date state changed before cleanup.")
            recorder.mutated = True
            recorder.persist()
            status, deleted = recorder.request("delete-unplayed-" + item, "DELETE", "/emby/Users/" + recorder.user_id + "/PlayedItems/" + item)
            require(status == 200, "The existing episode unplayed-delete endpoint rejected cleanup.")
            status, after = recorder.request("after-delete-detail-" + item, "GET", route)
            results[item] = {"before": before["UserData"], "deleteResponse": deleted, "after": after.get("UserData"),
                             "exactOriginalBaselineRestored": status == 200 and after.get("UserData") == original["userData"][item]}
            require(results[item]["exactOriginalBaselineRestored"], "The first acknowledged episode did not fully restore; no later episode will be changed.")
        status, prefs = recorder.request("prefs-final", "GET", "/emby/usersettings/" + recorder.user_id)
        require(status == 200 and prefs == EXPECTED_PREFS, "PREF1 changed during precise cleanup.")
        status, dto = recorder.request("profile-final", "GET", "/emby/Users/" + recorder.user_id)
        require(status == 200 and all(dto.get(key) == value for key, value in original["profile"].items()), "The original profile changed during cleanup.")
        recorder.data_restored, recorder.mutated = True, False
        recorder.persist()
    except Exception as error:
        errors.append(type(error).__name__)
    finally:
        if recorder.token_verified:
            try:
                status, _ = recorder.request("cleanup-recorder-logout", "POST", "/emby/Sessions/Logout")
                require(status in (204, 401), "The cleanup recorder logout failed.")
                status, _ = recorder.request("cleanup-recorder-token-invalid", "GET", "/emby/Sessions")
                require(status == 401, "The cleanup recorder token remains usable.")
                recorder.revoked = True
                recorder.persist()
            except Exception as error:
                errors.append(type(error).__name__)
    preserved = operator.verify_media() == owner["media"]
    recorder.identity()
    report = {"schemaVersion": 1, "marker": MARKER, "classification": "necessary detail-level cleanup; not client acceptance",
              "originalRequestLimit": 80, "authorizedAdditionalCleanupRequests": 16, "priorRequests": 75,
              "authorizedTotalLimit": 91, "cleanupRequests": recorder.count - 75, "totalNextUpStudyRequests": recorder.count,
              "episodeResults": results, "exactDetailedBaselineRestored": recorder.data_restored,
              "newRecorderRevoked": recorder.revoked, "mediaAndNfoPreserved": preserved, "errors": errors,
              "earlierClaimCorrection": "The initial successful restoration compared Items-list projection only; LastPlayedDate remained in detail and UserData POST null did not remove it. This report is the required stronger cleanup result."}
    for name, value in (*recorder.records.items(), ("report", report)):
        support.collect_secrets(value, recorder.secrets)
        safe = support.sanitize(value, recorder.secrets)
        require(not any(secret and secret in json.dumps(safe) for secret in recorder.secrets), "A cleanup token survived export.")
        operator.save(EXPORT / (name + ".json"), safe)
    require(not errors and recorder.data_restored and recorder.revoked and preserved, "The precise cleanup remains incomplete; retain responsibility and do not start another study.")
    print(json.dumps({"result": "complete", "totalNextUpStudyRequests": recorder.count, "exactDetailedBaselineRestored": True,
                      "report": str(EXPORT / "report.json")}), flush=True)


def detail_cleanup_main(internal=False):
    global ROOT, PRIVATE, EXPORT, MARKER, DEVICE, MAX_REQUESTS
    ROOT = WORK / "reference-nextup-detail-cleanup-v3"
    PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
    MARKER, DEVICE = "goby-m3e-nextup-detail-cleanup-v3", "goby-m3e-nextup-detail-cleanup-recorder-v3"
    MAX_REQUESTS = 91
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run only through authorized root SSH.")
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_args: (_ for _ in ()).throw(TimeoutError("The cleanup HTTP deadline expired.")))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    require(operator.service_identity(owner) == owner["serviceIdentity"] and owner["serviceIdentity"]["pid"] == PID and
            owner["serviceIdentity"]["startTicks"] == TICKS, "The owned reference process differs before cleanup.")
    if internal:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT)}, "Cleanup ownership differs.")
        detail_cleanup_capture(support, operator, owner)
        return
    require(not operator.present(ROOT), "Existing exact cleanup evidence will not be overwritten or rerun.")
    ROOT.mkdir(mode=0o700)
    operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT)})
    PRIVATE.mkdir(mode=0o700)
    EXPORT.mkdir(mode=0o700)
    operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  "serviceIdentity": owner["serviceIdentity"], "scope": "Only episode 17 then episode 18, stop immediately unless the first detail matches the original baseline.",
                  "priorRequests": 75, "originalLimit": 80, "authorizedAdditionalCleanupRequests": 16, "maximumTotalRequests": 91})
    descriptor = os.open(f"/proc/{PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(descriptor).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]), "The cleanup namespace differs.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(descriptor), "/usr/bin/python3", "-I", "-B",
                                 str(Path(__file__).absolute()), "_cleanup-detail"], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=150, check=False, pass_fds=(descriptor,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "Precise cleanup failed; responsibility and owned evidence remain.")
        print(result.stdout.decode().strip())
    finally:
        os.close(descriptor)


def main(arguments=None):
    arguments = sys.argv[1:] if arguments is None else arguments
    if arguments in (["--projection-check"], ["_projection-check"]):
        projection_main(internal=arguments == ["_projection-check"])
        return
    if arguments in (["--cleanup-detail"], ["_cleanup-detail"]):
        detail_cleanup_main(internal=arguments == ["_cleanup-detail"])
        return
    require(arguments in ([], ["_capture"]), "Usage: reference-nextup-fresh.py")
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run only through authorized root SSH.")
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_args: (_ for _ in ()).throw(TimeoutError("A NextUp HTTP request exceeded its bound.")))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    require(owner["serviceIdentity"]["pid"] == PID and owner["serviceIdentity"]["startTicks"] == TICKS and
            operator.service_identity(owner) == owner["serviceIdentity"], "The exact fresh reference process differs.")
    if arguments == ["_capture"]:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT)}, "The NextUp research owner marker differs.")
        capture(support, operator, owner)
        return
    require(not operator.present(ROOT), "Existing NextUp research evidence will not be replaced or automatically rerun.")
    ROOT.mkdir(mode=0o700)
    operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT)})
    PRIVATE.mkdir(mode=0o700)
    EXPORT.mkdir(mode=0o700)
    operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  "serviceIdentity": owner["serviceIdentity"], "maximumRequests": MAX_REQUESTS,
                  "scope": "Only viewer2's three existing episodes and series UserData; exact restoration and recorder revocation required."})
    descriptor = os.open(f"/proc/{PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(descriptor).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]), "The owned namespace handle differs.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(descriptor), "/usr/bin/python3", "-I", "-B",
                                 str(Path(__file__).absolute()), "_capture"], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=340, check=False, pass_fds=(descriptor,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "NextUp research failed; its exact restoration and token state are retained privately.")
        print(result.stdout.decode().strip())
    finally:
        os.close(descriptor)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"result": "failed", "failureType": type(error).__name__, "evidenceRetained": True}), file=sys.stderr)
        sys.exit(1)
