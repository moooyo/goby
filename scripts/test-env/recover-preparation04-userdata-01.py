#!/usr/bin/env python3
"""Recover one stopped owned episode's exact UserData in at most six attempts."""

from __future__ import annotations

import argparse
import base64
from copy import deepcopy
from datetime import datetime, timezone
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
from types import SimpleNamespace
from urllib.parse import urlencode

TRANSPORT_SHA = "d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1"
BUDGETS = {"normalRequests": 4, "cleanupRequests": 2, "totalRequests": 6, "requestSeconds": 10,
    "normalSeconds": 60, "cleanupSeconds": 30, "requestBytes": 32768, "responseBytes": 262144,
    "totalResponseBytes": 2097152, "cleanupResponseBytes": 524290}
CLIENT = "Goby Owned UserData Recovery"


def require(value, message):
    if not value:
        raise ValueError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def instant(value):
    require(isinstance(value, str), "An observed timestamp is required.")
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(result.utcoffset() is not None, "An observed timestamp requires a timezone.")
    return result


def utc_now():
    return datetime.now(timezone.utc).isoformat()


def strict_json(raw):
    def pairs(values):
        result = {}
        for key, value in values:
            require(key not in result, "Duplicate JSON member.")
            result[key] = value
        return result
    def invalid(value):
        raise ValueError("Nonfinite JSON number.")
    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_constant=invalid)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def protected(path, *, directory=False, private=True):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts, "An exact absolute authority path is required.")
    for current in (path, *path.parents):
        info = current.lstat()
        require(not stat.S_ISLNK(info.st_mode), "Authority paths cannot contain symlinks.")
        if current == path:
            require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)) and info.st_uid == 0 and
                    not info.st_mode & (0o077 if private else 0o022) and (directory or info.st_nlink == 1),
                    "An authority path lost its exact type, root owner, private mode, or independent file identity.")
            result = info
        else:
            require(stat.S_ISDIR(info.st_mode) and (not info.st_mode & 0o022 or info.st_mode & stat.S_ISVTX), "An authority ancestor is writable.")
    return result


def read_owned(path, *, private=True):
    info = protected(path, private=private)
    require(info.st_size <= 16 * 1024 * 1024, "An owned record exceeded its byte bound.")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as handle:
        require(identity(os.fstat(handle.fileno())) == identity(info), "An owned input changed while opening.")
        raw = handle.read(16 * 1024 * 1024 + 1)
        require(identity(os.fstat(handle.fileno())) == identity(info), "An owned input changed while reading.")
    require(len(raw) <= 16 * 1024 * 1024, "An owned record grew beyond its bound.")
    return raw


def decode(raw, headers, *, required_json):
    lengths = [value for key, value in headers if key.lower() == "content-length"]
    transfers = [value for key, value in headers if key.lower() == "transfer-encoding"]
    types = [value for key, value in headers if key.lower() == "content-type"]
    require(all(isinstance(key, str) and isinstance(value, str) and "\r" not in key + value and "\n" not in key + value
                for key, value in headers) and len(set(types)) <= 1, "Ambiguous or malformed response metadata.")
    if lengths:
        require(len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(raw) and not transfers,
                "The completed HTTP framing differs from the actual bytes.")
    if types and "charset=" in types[0].lower():
        require(types[0].lower().split("charset=", 1)[1].split(";", 1)[0].strip().strip('"') in ("utf-8", "utf8"),
                "Only UTF-8 response text is supported.")
    if not raw: return None
    if required_json: return strict_json(raw)
    return raw.decode("utf-8", errors="strict")


def request_token(request):
    found = [value for key, value in request["headers"] if key.lower() == "x-emby-token"]
    require(len(found) == 1 and isinstance(found[0], str) and found[0], "One exact recorded token context is required.")
    return found[0]


class Authority:
    def __init__(self, path, checksum):
        self.files, self.roots, self.lock_fd = {}, {}, None
        self.input_descriptor = {"path": str(path), "sha256": checksum}
        self.raw = self.read(self.input_descriptor)
        self.value = strict_json(self.raw)
        value = self.value
        require(set(value) == {"schemaVersion", "kind", "runId", "ownerUid", "source", "transport", "preparationInput", "parent", "parentUnit",
            "actor", "expected", "process", "endpoint", "lock", "outputRoot", "outputParent", "readonlyRoots", "pins", "budgets"} and
            value["schemaVersion"] == 1 and value["kind"] == "nextup-preparation-userdata-recovery-input" and value["ownerUid"] == 0 and
            re.fullmatch(r"[A-Za-z0-9_-]{1,96}", value["runId"]), "The exact one-shot recovery input is required.")
        require(value["budgets"] == BUDGETS and value["endpoint"] == {"scheme": "http", "host": "127.0.0.1", "port": 18197} and
                value["transport"]["sha256"] == TRANSPORT_SHA and Path(value["source"]["path"]) == Path(__file__).absolute(),
                "The endpoint, bounded recovery budget, or owned source differs.")
        self.read(value["source"])
        transport_raw = self.read(value["transport"])
        spec = importlib.util.spec_from_file_location("owned_userdata_recovery_transport", value["transport"]["path"])
        self.support = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = self.support
        exec(compile(transport_raw, value["transport"]["path"], "exec"), self.support.__dict__)
        self.prior = strict_json(self.read(value["preparationInput"]))
        require(value["process"] == self.prior["process"] and value["endpoint"] == self.prior["endpoint"] and value["lock"] == self.prior["lock"],
                "Recovery cannot select another original application, existing proxy, or fixture lock.")
        require(set(value["parent"]) == {"terminal", "state", "wireIndex"}, "The exact parent terminal/state/ledger must be pinned.")
        self.parent_terminal, self.parent_state, index = (strict_json(self.read(value["parent"][key])) for key in ("terminal", "state", "wireIndex"))
        self.parent_root = Path(self.prior["scope"]["outputRoot"])
        for key, filename in (("terminal", "terminal.json"), ("state", "state.json")):
            require(Path(value["parent"][key]["path"]) == self.parent_root / "private" / filename, "A parent record escaped its retained scope.")
        expected = value["expected"]
        require(set(expected) == {"parentRunId", "actor", "item", "itemId", "ownedPath", "zeroResponse", "partialResponses", "stoppedResponse"} and
                expected["actor"] == "P" and expected["item"] == "A1" and expected["itemId"] == "125" and
                expected["parentRunId"] == self.prior["runId"] == self.parent_terminal["runId"] == self.parent_state["runId"],
                "Only the exact retained preparation04 P/A1 responsibility is supported.")
        require(set(value["actor"]) == {"userId", "username", "credentialRef", "deviceId"} and
                value["actor"]["userId"] == self.parent_state["userIds"]["P"] and
                all(value["actor"][key] == self.prior["actors"]["P"][key] for key in ("username", "credentialRef")) and
                re.fullmatch(r"[A-Za-z0-9_-]{1,128}", value["actor"]["deviceId"]), "The one recovery actor differs from the owned parent P.")
        self.credential = strict_json(self.read(self.prior["inputs"]["credentials"]))["accounts"]["P"]
        require(all(self.credential[key] == value["actor"][key] for key in ("username", "credentialRef")) and
                isinstance(self.credential["password"], str) and len(self.credential["password"]) >= 32,
                "The recovery password must be the retained private P credential.")
        self.mapped = self.parent_state["items"]["A1"]
        name = self.prior["libraries"]["LA"]["seriesName"]
        owned = str(Path(self.prior["media"]["roots"]["LA"]) / name / "Season 01" / (name + " S01E01.mp4"))
        require(self.mapped["id"] == "125" and expected["ownedPath"] == owned and self.parent_root in Path(owned).parents,
                "The target detail path is not the exact retained independently copied episode.")
        self._parent(index)
        for row in [*self.prior["sources"].values(), *value["pins"]]: self.read(row)
        proxy = self.prior["sources"]["proxy"]
        require(proxy["path"] in value["process"]["endpoint"]["cmdline"], "The existing proxy command differs from its pinned owned source.")
        output, parent = Path(value["outputRoot"]), Path(value["outputParent"])
        require(output.parent == parent and not os.path.lexists(output), "Recovery requires one fresh output child and cannot resume.")
        protected(parent, directory=True, private=False)
        require(isinstance(value["readonlyRoots"], list) and value["readonlyRoots"] and len(set(value["readonlyRoots"])) == len(value["readonlyRoots"]),
                "All immutable source/input/parent authority roots must be explicit.")
        for root in value["readonlyRoots"]:
            info = protected(root, directory=True, private=False)
            self.roots[root] = (info.st_dev, info.st_ino)
            require(Path(root) != output and Path(root) not in output.parents and output not in Path(root).parents,
                    "The fresh recovery output overlaps retained authority.")
        for filename in self.files:
            require(filename == proxy["path"] or any(Path(root) in Path(filename).parents for root in self.roots), "A pinned authority file is outside its explicit readonly root.")
            require(all(Path(root) != Path(filename) and Path(root) not in Path(filename).parents for root in self.prior["forbiddenOriginalRoots"]),
                    "Original executable, assets, and data contents cannot be read as recovery evidence.")

    def read(self, row):
        require(isinstance(row, dict) and set(row) == {"path", "sha256"} and isinstance(row["sha256"], str) and
            re.fullmatch(r"[0-9a-f]{64}", row["sha256"]), "Every actual authority file needs an exact descriptor.")
        if hasattr(self, "prior"):
            require(all(Path(root) != Path(row["path"]) and Path(root) not in Path(row["path"]).parents
                        for root in self.prior["forbiddenOriginalRoots"]), "Original implementation or data roots cannot supply recovery bytes.")
        private = Path(row["path"]).suffix != ".py"
        raw = read_owned(row["path"], private=private)
        require(digest(raw) == row["sha256"], "Frozen authority bytes changed.")
        self.files[row["path"]] = (identity(protected(row["path"], private=private)), private)
        return raw

    def detail(self, body):
        item = self.mapped
        require(isinstance(body, dict) and body.get("Id") == item["id"] and body.get("Type") == "Episode" and
                body.get("ParentId") == body.get("SeasonId") == item["parentId"] and body.get("SeriesId") == item["seriesId"] and
                type(body.get("IndexNumber")) is int and body["IndexNumber"] == item["indexNumber"] == 1 and
                type(body.get("ParentIndexNumber")) is int and body["ParentIndexNumber"] == item["parentIndexNumber"] == 1 and
                type(body.get("RunTimeTicks")) is int and body["RunTimeTicks"] == item["runtimeTicks"] == 6000000000 and
                body.get("Path") == self.value["expected"]["ownedPath"] and isinstance(body.get("UserData"), dict),
                "The full target detail differs in identity, source path, season, numbering, runtime, or UserData shape.")
        return body["UserData"]

    def _parent(self, index):
        state, terminal = self.parent_state, self.parent_terminal
        require(terminal.get("completed") is False and terminal.get("status") == "recovery_required" and
                all(document.get("uncertain") is False and document.get("pending") is None and document.get("ownershipPending") is None and
                    (document.get("requestCount"), document.get("normalRequestCount"), document.get("cleanupRequestCount")) == (172, 115, 57)
                    for document in (state, terminal)) and state.get("phase") == "cleanup" and
                set(state.get("tokens", {})) == set(state.get("sessions", {})) == set(state.get("revoked", [])) == {"admin", "P", "Q"} and
                state.get("touched") == [["P", "A1"]] and set(state.get("plays", {})) == {"P"} and
                state["plays"]["P"].get("stopped") is True and state["plays"]["P"].get("item") == "A1" and
                state["plays"]["P"].get("position") == state["plays"]["P"].get("target") == 1200000000,
                "The parent has unresolved requests, another mutation responsibility, or live tokens/playback.")
        require(index.get("schemaVersion") == 1 and index.get("kind") == "nextup-global-preparation-wire-index" and
                index.get("runId") == self.prior["runId"] and index.get("producerRoot") == str(self.parent_root) and
                isinstance(index.get("requests"), list) and len(index["requests"]) == 172, "The complete immutable parent ledger is required.")
        expected = self.value["expected"]
        require(len(expected["partialResponses"]) == 3, "Three identical retained partial responses are required.")
        selected = {row["path"]: row for row in [expected["zeroResponse"], expected["stoppedResponse"], *expected["partialResponses"]]}
        require(len(selected) == 5, "Zero, stopped, and three partial proofs must be distinct actual records.")
        found, auth, labels, previous, new_devices, lifecycles = {}, {}, set(), None, set(), []
        expected_names = set()
        for ordinal, row in enumerate(index["requests"], 1):
            require(set(row) == {"ordinal", "label", "intent", "reserved", "response"} and type(row["ordinal"]) is int and row["ordinal"] == ordinal and
                    row["label"] not in labels and re.fullmatch(r"[A-Za-z0-9_-]{1,88}", row["label"]), "The parent ledger ordinal or label differs.")
            labels.add(row["label"])
            records = {}
            for key in ("intent", "reserved", "response"):
                require(row[key]["path"] == str(self.parent_root / "private" / ("%04d-%s-%s.json" % (ordinal, row["label"], key))),
                        "A parent ledger record escaped its original exact path.")
                expected_names.add(Path(row[key]["path"]).name)
                records[key] = strict_json(self.read(row[key]))
            intent, reserved, response = (records[key] for key in ("intent", "reserved", "response"))
            request = intent["request"]
            require(intent.get("ordinal") == response.get("ordinal") == reserved.get("requestCount") == ordinal and
                    intent.get("label") == response.get("label") == row["label"] and intent.get("actor") == response.get("actor") and
                    canonical(request) == canonical(response["request"]) == canonical(reserved["pending"]["request"]) and
                    reserved["pending"]["intentSha256"] == row["intent"]["sha256"] and request["method"] != "DELETE" and
                    response.get("completeHttp") is True and response.get("failure") is None and response.get("retainedRawTruncated") is False,
                    "A parent attempt is unacknowledged, inconsistent, or already deleted UserData.")
            raw = base64.b64decode(response["rawBase64"], validate=True)
            require(len(raw) == response["observedRawBytes"] <= self.prior["budgets"]["responseBytes"] and
                    len(self.support.bounded_header_prefix(response["headers"])) == len(response["headers"]) and
                    type(response.get("status")) is int and 100 <= response["status"] <= 599 and
                    (previous is None or previous <= instant(response["completedAt"])), "The parent wire is incomplete or out of order.")
            previous = instant(response["completedAt"])
            actor = intent["actor"]
            lifecycle = request["route"].endswith("/PlaybackInfo") or request["route"] in (
                "/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped")
            needed = lifecycle or row["response"]["path"] in selected or row["label"] in {prefix + role for prefix in ("login-", "logout-", "invalid-") for role in ("admin", "P", "Q")}
            device_observation = row["label"] in ("before-devices", "after-devices", "cleanup-after-devices")
            body = decode(raw, response["headers"], required_json=(needed or device_observation) and response["status"] == 200)
            if row["label"] == "login-" + actor:
                require(response["status"] == 200 and body["ServerId"] == self.prior["server"]["id"] and
                        body["User"]["Id"] == body["SessionInfo"]["UserId"] == state["userIds"][actor] and
                        body["AccessToken"] == state["tokens"][actor] and body["SessionInfo"]["Id"] == state["sessions"][actor] and
                        body["SessionInfo"]["DeviceId"] == self.prior["actors"][actor]["deviceId"], "A parent login does not bind its retained actor/session/token.")
                auth[actor] = [ordinal]
                new_devices.add(body["SessionInfo"]["DeviceId"])
            else:
                require(actor in auth and request_token(request) == state["tokens"][actor], "A parent request used another actor's token.")
            for prefix, status, method, route in (("logout-", 204, "POST", "/emby/Sessions/Logout"), ("invalid-", 401, "GET", "/emby/Sessions")):
                if row["label"] == prefix + actor:
                    require(response["status"] == status and request["method"] == method and request["route"] == route and
                            len(auth[actor]) == (1 if status == 204 else 2), "The parent exact-token closure is missing or out of order.")
                    auth[actor].append(ordinal)
            if row["response"]["path"] in selected:
                require(row["response"] == selected[row["response"]["path"]] and actor == "P" and request_token(request) == state["tokens"]["P"],
                        "A selected UserData/STOP proof has another actor or descriptor.")
                found[row["response"]["path"]] = {"ordinal": ordinal, "request": request, "status": response["status"], "body": body}
            if lifecycle:
                lifecycles.append({"ordinal": ordinal, "actor": actor, "request": request, "status": response["status"], "body": body})
            if row["label"] in ("before-devices", "after-devices", "cleanup-after-devices") and isinstance(body, dict):
                new_devices.update(item["ReportedDeviceId"] for item in body.get("Items", []) if isinstance(item, dict) and "ReportedDeviceId" in item)
        self.parent_request_names = expected_names
        self.check_parent_membership()
        require(set(auth) == {"admin", "P", "Q"} and all(len(row) == 3 for row in auth.values()) and set(found) == set(selected) and
                self.value["actor"]["deviceId"] not in new_devices and self.value["actor"]["deviceId"] not in
                    {row["matrixDeviceId"] for role, row in self.prior["actors"].items() if role in ("P", "Q")},
                "The parent token closures, selected witnesses, or new recovery device absence are incomplete.")
        zero = found[expected["zeroResponse"]["path"]]
        partials = [found[row["path"]] for row in expected["partialResponses"]]
        stopped = found[expected["stoppedResponse"]["path"]]
        require(zero["ordinal"] == 110 and stopped["ordinal"] == 114 and [row["ordinal"] for row in partials] == [115, 116, 117],
                "Only the explicit pre-delete partial failure witnesses may authorize recovery.")
        detail_route = "/emby/Users/" + self.value["actor"]["userId"] + "/Items/125"
        for row in (zero, *partials):
            require(row["status"] == 200 and row["request"]["method"] == "GET" and row["request"]["route"] == detail_route,
                    "A full UserData witness has a different route or status.")
            self.detail(row["body"])
        self.zero, self.partial = deepcopy(zero["body"]["UserData"]), deepcopy(partials[0]["body"]["UserData"])
        require(canonical(self.zero) == canonical(state["baselines"]["P"]["A1"]) and
                set(self.zero) == {"Played", "PlayCount", "PlaybackPositionTicks", "IsFavorite"} and
                self.zero["Played"] is False and self.zero["IsFavorite"] is False and type(self.zero["PlayCount"]) is int and self.zero["PlayCount"] == 0 and
                type(self.zero["PlaybackPositionTicks"]) is int and self.zero["PlaybackPositionTicks"] == 0 and
                all(canonical(row["body"]["UserData"]) == canonical(self.partial) for row in partials) and
                set(self.partial) == set(self.zero) | {"PlayedPercentage", "LastPlayedDate"} and self.partial["Played"] is False and self.partial["IsFavorite"] is False and
                type(self.partial["PlayCount"]) is int and self.partial["PlayCount"] == 1 and type(self.partial["PlaybackPositionTicks"]) is int and
                self.partial["PlaybackPositionTicks"] == 1200000000 and type(self.partial["PlayedPercentage"]) in (int, float) and
                self.partial["PlayedPercentage"] == 20 and isinstance(self.partial["LastPlayedDate"], str),
                "The selected full zero/partial UserData differs in field presence, type, or history.")
        play = state["plays"]["P"]
        require(set(play) == {"context", "item", "position", "stopped", "target"} and play["context"]["ItemId"] == "125" and
                play["context"]["SessionId"] == state["sessions"]["P"] and stopped["status"] == 204 and
                stopped["request"]["method"] == "POST" and stopped["request"]["route"] == "/emby/Sessions/Playing/Stopped" and
                stopped["request"]["body"] == {**play["context"], "PositionTicks": 1200000000, "Failed": False, "IsAutomated": False},
                "The retained exact playback context lacks its complete acknowledged STOP.")
        require([row["ordinal"] for row in lifecycles] == [111, 112, 113, 114] and
                all(row["actor"] == "P" and row["request"]["method"] == "POST" for row in lifecycles) and
                state["playSessionIds"] == [play["context"]["PlaySessionId"]] and
                set(play["context"]) == {"ItemId", "MediaSourceId", "PlaySessionId", "SessionId"},
                "The parent contains an additional or unclosed playback lifecycle.")
        info, started, progressed, stop = lifecycles
        require(info["status"] == 200 and info["request"]["route"] == "/emby/Items/125/PlaybackInfo" and
                info["request"]["body"] == {"UserId": self.value["actor"]["userId"], "IsPlayback": True} and
                info["body"]["PlaySessionId"] == play["context"]["PlaySessionId"] and len(info["body"]["MediaSources"]) == 1 and
                info["body"]["MediaSources"][0]["Id"] == play["context"]["MediaSourceId"] and
                info["body"]["MediaSources"][0]["RunTimeTicks"] == 6000000000,
                "The one parent playback negotiation differs from its stopped source/session context.")
        start_body = {**play["context"], "RunTimeTicks": 6000000000, "PositionTicks": 0, "CanSeek": True,
            "IsPaused": False, "IsMuted": False, "PlayMethod": "DirectStream", "PlaybackRate": 1}
        require(started["status"] == progressed["status"] == stop["status"] == 204 and
                started["request"]["route"] == "/emby/Sessions/Playing" and started["request"]["body"] == start_body and
                progressed["request"]["route"] == "/emby/Sessions/Playing/Progress" and
                progressed["request"]["body"] == {**start_body, "PositionTicks": 1200000000, "EventName": "TimeUpdate"} and
                stop["request"] == stopped["request"], "The complete parent start/progress/stop cycle does not close one exact partial attempt.")

    def check_parent_membership(self):
        private = self.parent_root / "private"
        protected(private, directory=True)
        actual = {entry.name for entry in private.iterdir() if re.fullmatch(r"[0-9]+-[A-Za-z0-9_-]+-(?:intent|reserved|response)\.json", entry.name)}
        require(actual == self.parent_request_names, "An actual parent request record is missing from or additional to its complete ledger.")

    def check(self):
        require(canonical(self.value) == canonical(strict_json(self.raw)) and self.lock_fd is not None, "Frozen locked recovery authority is unavailable.")
        self.check_parent_membership()
        for path, (expected, private) in self.files.items():
            require(identity(protected(path, private=private)) == expected, "A frozen recovery authority file changed.")
        for path, expected in self.roots.items():
            info = protected(path, directory=True, private=False)
            require((info.st_dev, info.st_ino) == expected, "An immutable authority root was replaced.")
        lock = self.value["lock"]
        named, opened = protected(lock["path"]), os.fstat(self.lock_fd)
        require((named.st_dev, named.st_ino) == (opened.st_dev, opened.st_ino) == (lock["device"], lock["inode"]), "The held existing lock was replaced.")
        require(self.support.process_identity(self.value["process"]) == self.value["process"], "The metadata-only application or existing proxy identity changed.")

    def acquire(self):
        require(self.lock_fd is None, "The existing fixture lock cannot be acquired twice.")
        lock = self.value["lock"]
        info = protected(lock["path"])
        require((info.st_dev, info.st_ino) == (lock["device"], lock["inode"]), "The existing lock identity differs.")
        self.lock_fd = os.open(lock["path"], os.O_RDWR | os.O_NOFOLLOW)
        fcntl.flock(self.lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.check()
        unit = self.value["parentUnit"]
        require(set(unit) == {"name", "invocationId", "properties", "cgroupPath"} and re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", unit["name"]) and
                re.fullmatch(r"[0-9a-f]{32}", unit["invocationId"]) and set(unit["properties"]) ==
                    {"ActiveState", "SubState", "MainPID", "Result", "ExecMainStatus", "ControlGroup"} and
                unit["properties"]["MainPID"] == "0" and unit["properties"]["ExecMainStatus"] == "2" and unit["properties"]["Result"] == "exit-code" and
                (unit["properties"]["ActiveState"], unit["properties"]["SubState"]) in (("failed", "failed"), ("inactive", "dead")) and
                unit["cgroupPath"] == "/sys/fs/cgroup/system.slice/" + unit["name"] and
                unit["properties"]["ControlGroup"] in ("", "/system.slice/" + unit["name"]), "The exact failed parent unit is not closed.")
        names = ("InvocationID", *unit["properties"])
        result = subprocess.run(["/usr/bin/systemctl", "show", unit["name"], *[part for name in names for part in ("-p", name)]],
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False, env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8"})
        require(result.returncode == 0 and len(result.stdout) <= 65536 and
                dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line) == {"InvocationID": unit["invocationId"], **unit["properties"]},
                "The parent unit invocation or terminal state changed.")
        root = Path(unit["cgroupPath"])
        if root.exists():
            pending, count = [root], 0
            while pending:
                current = pending.pop(); count += 1
                require(count <= 128 and not current.is_symlink() and not (current / "cgroup.procs").read_text().strip(), "The parent cgroup is not bounded and empty.")
                for child in current.iterdir():
                    require(not child.is_symlink(), "The parent cgroup contains a symlink.")
                    if child.is_dir(): pending.append(child)

    def close(self):
        if self.lock_fd is not None:
            os.close(self.lock_fd); self.lock_fd = None


class Runner:
    def __init__(self, authority, *, wire=None, journal_factory=None, monotonic=time.monotonic, now=utc_now):
        self.authority, self.value, self.support = authority, authority.value, authority.support
        self.wire = wire or self.support.HTTPTransport(self.value["endpoint"])
        self.journal_factory = journal_factory or (lambda root: self.support.Journal(root, uid=0))
        self.monotonic, self.now = monotonic, now
        self.journal, self.token, self.session, self.pending, self.failure = None, None, None, None, None
        self.used = self.uncertain = self.journal_failed = self.closed = self.zero_restored = self.already_restored = False
        self.delete_attempted = self.delete_acknowledged = self.logout_acknowledged = False
        self.partial_matched, self.before_response_sha = False, None
        self.phase, self.count, self.normal_count, self.cleanup_count, self.charged_bytes = "normal", 0, 0, 0, 0
        self.started = self.cleanup_started = self.last_clock = self.last_completed = None
        self.labels = set()
        self.secrets = {authority.credential["password"], self.value["actor"]["deviceId"], *authority.parent_state["tokens"].values(),
            *authority.parent_state["sessions"].values(), *authority.files.keys(), *self.value["readonlyRoots"]}

    def clock(self):
        value = self.monotonic()
        require(type(value) in (int, float) and math.isfinite(value) and (self.last_clock is None or value >= self.last_clock), "The monotonic clock changed direction or became nonfinite.")
        self.last_clock = value
        return value

    def state(self):
        return {"schemaVersion": 1, "runId": self.value["runId"], "inputSha256": digest(self.authority.raw), "phase": self.phase,
            "requestCount": self.count, "normalRequestCount": self.normal_count, "cleanupRequestCount": self.cleanup_count,
            "chargedResponseBytes": self.charged_bytes, "token": self.token, "sessionId": self.session, "pending": self.pending,
            "uncertain": self.uncertain, "failure": self.failure, "deleteAttempted": self.delete_attempted,
            "deleteAcknowledged": self.delete_acknowledged, "logoutAcknowledged": self.logout_acknowledged,
            "exactPartialObserved": self.partial_matched, "beforeResponseSha256": self.before_response_sha,
            "zeroRestored": self.zero_restored, "alreadyRestored": self.already_restored, "allKnownTokensClosed": self.closed}

    def save(self, name, value, *, export=False):
        try: return self.journal.save(name, value, export=export)
        except BaseException:
            self.journal_failed = True
            raise

    def persist(self):
        try: self.journal.state(self.state())
        except BaseException:
            self.journal_failed = True
            raise

    def dispatch(self, label, method, route, body=None, *, form=False, acknowledge=None):
        require(self.pending is None and not self.uncertain and not self.journal_failed and label not in self.labels,
                "An uncertain, repeated, or closed journal cannot dispatch.")
        user = self.value["actor"]["userId"]
        routes = {"login": ("POST", "/emby/Users/AuthenticateByName"), "before": ("GET", "/emby/Users/" + user + "/Items/125"),
            "delete": ("DELETE", "/emby/Users/" + user + "/PlayedItems/125"), "after": ("GET", "/emby/Users/" + user + "/Items/125"),
            "logout": ("POST", "/emby/Sessions/Logout"), "rejection": ("GET", "/emby/Sessions")}
        require(label in routes and (method, route) == routes[label] and
                (label == "login" and self.token is None and form and body == {"Username": self.authority.credential["username"], "Pw": self.authority.credential["password"]} or
                 label != "login" and self.token is not None and body is None and not form) and
                (self.phase == "cleanup") == (label in ("logout", "rejection")), "A request escaped the exact six-operation recovery plan.")
        require(label != "delete" or self.labels == {"login", "before"} and not self.already_restored and self.partial_matched and
                isinstance(self.before_response_sha, str) and re.fullmatch(r"[0-9a-f]{64}", self.before_response_sha),
                "Only the first exact partial observation may authorize one DELETE.")
        require(label != "after" or self.delete_acknowledged, "The post-delete observation requires its actual acknowledgement.")
        require(label != "rejection" or self.logout_acknowledged, "The same-token rejection proof must follow one acknowledged logout.")
        cleanup = self.phase == "cleanup"
        require(self.count < 6 and (self.cleanup_count < 2 if cleanup else self.normal_count < 4), "The fixed recovery request reserve is exhausted.")
        deadline = (self.cleanup_started + 30 if cleanup else self.started + 60)
        require(self.clock() < deadline and 2097152 - self.charged_bytes - (0 if cleanup else 524290) >= 262145,
                "The normal phase exhausted its finite time or reserved response-byte budget.")
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="' + CLIENT + '", Device="Linux Fixture Recorder", DeviceId="' +
            self.value["actor"]["deviceId"] + '", Version="1.0"'}
        if self.token is not None: headers["X-Emby-Token"] = self.token
        payload = urlencode([("Username", body["Username"]), ("Pw", body["Pw"])]).encode() if form else None
        if payload is not None: headers["Content-Type"] = "application/x-www-form-urlencoded; charset=utf-8"
        require(payload is None or len(payload) <= 32768, "The private credential form exceeds its bound.")
        request = SimpleNamespace(method=method, route=route, label=label, actor="P", cleanup=cleanup)
        self.support.request_metadata_size(request, headers)
        self.authority.check(); self.journal.check()
        ordinal, created = self.count + 1, self.now()
        stem = "%04d-%s" % (ordinal, label)
        captured = {"method": method, "route": route, "headers": [[key, value] for key, value in headers.items()], "body": body}
        checksum = self.save(stem + "-intent.json", {"ordinal": ordinal, "label": label, "createdAt": created, "phase": self.phase,
            "request": captured, "payloadBase64": None if payload is None else base64.b64encode(payload).decode(),
            "tokenSha256": None if self.token is None else digest(self.token.encode())})
        self.pending = {"ordinal": ordinal, "label": label, "intentSha256": checksum}
        self.count += 1; self.normal_count += int(not cleanup); self.cleanup_count += int(cleanup); self.labels.add(label)
        if label == "delete": self.delete_attempted = True
        self.persist()
        try:
            self.authority.check(); self.journal.check()
            remaining = deadline - self.clock()
            require(remaining > 0, "Persistence consumed the dispatch deadline.")
            response = self.wire.send(request, headers, payload, timeout_seconds=min(10, remaining), max_bytes=262145)
            require(isinstance(response.raw, bytes), "Actual raw response bytes are required.")
            raw, response_headers = response.raw[:262145], self.support.bounded_header_prefix(response.headers)
            complete = response.complete_http is True and response.failure is None and len(response.raw) <= 262144 and len(response_headers) == len(response.headers)
            record = {"ordinal": ordinal, "label": label, "request": captured, "status": response.status, "headers": response_headers,
                "rawBase64": base64.b64encode(raw).decode(), "observedRawBytes": len(response.raw), "retainedRawTruncated": len(raw) != len(response.raw),
                "completeHttp": complete, "completedAt": response.completed_at, "failure": response.failure}
            response_sha = self.save(stem + "-response.json", record)
            self.pending["responseReceiptSha256"] = response_sha
            self.charged_bytes += len(raw) if complete else 262145
            self.persist()
            require(complete and type(response.status) is int and 100 <= response.status <= 599 and
                    instant(created) <= instant(response.completed_at) and (self.last_completed is None or self.last_completed <= instant(response.completed_at)),
                    "The one attempt did not produce a complete bounded timed HTTP response.")
            self.last_completed = instant(response.completed_at)
            decoded = decode(raw, response_headers, required_json=response.status == 200 and label in ("login", "before", "after"))
            event = {"status": response.status, "body": decoded, "responseReceiptSha256": response_sha, "completedAt": response.completed_at}
            if acknowledge is not None:
                acknowledge(event)
                self.persist()
            self.save(stem + "-response.json", self.support.sanitized({"event": event, "headers": [
                [key, "[redacted]" if key.lower() in self.support.SECRET_KEYS else value] for key, value in response_headers]}, self.secrets), export=True)
            self.pending = None
            self.persist()
            return event
        except BaseException:
            self.uncertain = True
            if not self.journal_failed: self.persist()
            raise

    def login(self):
        def acknowledge(event):
            body = event["body"]
            require(event["status"] == 200 and isinstance(body, dict) and body.get("ServerId") == self.authority.prior["server"]["id"] and
                    isinstance(body.get("User"), dict) and isinstance(body.get("SessionInfo"), dict), "Login ownership was not acknowledged.")
            user, session, token = body["User"], body["SessionInfo"], body.get("AccessToken")
            require(user.get("Id") == session.get("UserId") == self.value["actor"]["userId"] and user.get("Name") == self.value["actor"]["username"] and
                    user.get("Policy", {}).get("IsAdministrator") is False and user["Policy"].get("EnableAllFolders") is False and
                    user["Policy"].get("IsDisabled") is False and set(user["Policy"].get("EnabledFolders", [])) ==
                    {row["policyFolderId"] for row in self.authority.parent_state["libraries"].values()} and
                    session.get("DeviceId") == self.value["actor"]["deviceId"] and
                    isinstance(token, str) and token and token not in self.authority.parent_state["tokens"].values() and
                    isinstance(session.get("Id"), str) and session["Id"] and session["Id"] not in self.authority.parent_state["sessions"].values(),
                    "Login did not bind a distinct new exact P/device/session/token.")
            self.token, self.session = token, session["Id"]
            self.secrets.update((token, session["Id"]))
        self.dispatch("login", "POST", "/emby/Users/AuthenticateByName",
            {"Username": self.authority.credential["username"], "Pw": self.authority.credential["password"]}, form=True, acknowledge=acknowledge)

    def observe(self, label):
        event = self.dispatch(label, "GET", "/emby/Users/" + self.value["actor"]["userId"] + "/Items/125")
        require(event["status"] == 200, "The complete target read was not HTTP 200.")
        if label == "before": self.before_response_sha = event["responseReceiptSha256"]
        return self.authority.detail(event["body"])

    def close_token(self):
        self.phase, self.cleanup_started = "cleanup", self.clock()
        self.persist()
        def logout(event):
            require(event["status"] == 204, "The exact new token logout was not acknowledged.")
            self.logout_acknowledged = True
        self.dispatch("logout", "POST", "/emby/Sessions/Logout", acknowledge=logout)
        def rejection(event):
            require(event["status"] == 401, "The same new token was not rejected after logout.")
            self.closed = True
        self.dispatch("rejection", "GET", "/emby/Sessions", acknowledge=rejection)

    def run(self):
        require(not self.used, "The recovery worker cannot resume or run twice.")
        self.used = True
        try:
            self.authority.acquire()
            self.journal = self.journal_factory(self.value["outputRoot"])
            self.started = self.clock()
            self.save("manifest.json", self.value)
            self.save("retained-userdata.json", {"zero": self.authority.zero, "partial": self.authority.partial})
            self.persist()
            try:
                self.login()
                before = self.observe("before")
                if canonical(before) == canonical(self.authority.zero):
                    self.already_restored = self.zero_restored = True
                else:
                    require(canonical(before) == canonical(self.authority.partial), "The current full UserData is neither exact retained partial nor exact zero; no DELETE is authorized.")
                    self.partial_matched = True
                    self.persist()
                    def acknowledge_delete(event):
                        require(event["status"] == 200, "The one narrow UserData DELETE was not acknowledged.")
                        self.delete_acknowledged = True
                    self.dispatch("delete", "DELETE", "/emby/Users/" + self.value["actor"]["userId"] + "/PlayedItems/125", acknowledge=acknowledge_delete)
                    require(canonical(self.observe("after")) == canonical(self.authority.zero), "The one DELETE did not restore the exact full zero UserData.")
                    self.zero_restored = True
                self.persist()
            except BaseException as error:
                self.failure = {"stage": "normal", "type": type(error).__name__}
            if self.token is not None and not self.uncertain and self.pending is None and not self.journal_failed:
                try: self.close_token()
                except BaseException as error: self.failure = {"stage": "closure", "type": type(error).__name__}
            if self.journal_failed: return None
            self.persist()
            success = self.zero_restored and self.closed and self.failure is None and not self.uncertain and self.pending is None
            terminal = {"schemaVersion": 1, "kind": "nextup-preparation-userdata-recovery-terminal", "runId": self.value["runId"],
                "status": "awaiting_independent_attestation" if success else "recovery_required" if self.uncertain or self.pending is not None or self.token is not None and not self.closed else "stopped_with_known_token_closed",
                "requestCount": self.count, "normalRequestCount": self.normal_count, "cleanupRequestCount": self.cleanup_count,
                "zeroRestored": self.zero_restored, "alreadyRestored": self.already_restored, "deleteAttempted": self.delete_attempted,
                "deleteAcknowledged": self.delete_acknowledged, "allKnownTokensClosed": self.closed,
                "pendingPresent": self.pending is not None, "uncertain": self.uncertain, "failure": self.failure,
                "inputSha256": digest(self.authority.raw), "parentUnmodified": True, "originalImplementationBytesRead": False,
                "referenceDatabaseRead": False, "matrixInputsUsable": False}
            self.save("terminal.json", terminal)
            self.save("terminal.json", terminal, export=True)
            return terminal
        finally:
            if self.journal is not None: self.journal.close()
            self.authority.close()


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
            "Only the authorized remote Python -I -B environment is supported.")
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("sha256")
    parser.add_argument("--plan", action="store_true")
    args = parser.parse_args()
    authority = Authority(args.input, args.sha256)
    if args.plan:
        print(canonical({"kind": "owned-userdata-recovery-plan", "inputSha256": digest(authority.raw), "maximumRequests": 6,
            "alreadyRestoredRequests": 4, "parentRequestsVerified": 172, "businessHttpRequests": 0}))
        return 0
    terminal = Runner(authority).run()
    print(canonical(terminal if terminal is not None else {"status": "journal_failure_recovery_required"}))
    return 0 if terminal is not None and terminal["status"] == "awaiting_independent_attestation" else 2


if __name__ == "__main__":
    try: raise SystemExit(main())
    except Exception as error:
        print(canonical({"status": "entry_or_persistence_failure", "type": type(error).__name__}), file=sys.stderr)
        raise SystemExit(2)
