#!/usr/bin/env python3
"""Manifest-bound transport for the pure NextUp matrix; live CLI is disabled.

No fixture preparation, discovery, automatic retry, or journal resume is provided.
The injectable interfaces exist for remote fake-transport verification. A live
operator requires a separately reviewed preparation receipt producer and entry.
"""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime, timezone
import base64
import fcntl
import hashlib
import http.client
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import signal
import stat
import sys
import time
from urllib.parse import unquote, urlencode

sys.dont_write_bytecode = True
RECEIPT_LIMIT = 4 * 1024 * 1024
HEADER_BYTES = 32768
HEADER_COUNT = 100
REQUEST_META_BYTES = 32768
SECRET_KEYS = {"accesstoken", "access_token", "token", "password", "pw", "authorization", "cookie", "set-cookie",
               "x-emby-token", "x-mediabrowser-token", "api_key", "apikey", "sessionid", "playsessionid", "mediasourceid", "deviceid", "path", "nativepath"}
URL_SECRET = re.compile(r"(?i)([?&](?:" + "|".join(re.escape(key) for key in sorted(SECRET_KEYS)) + r")=)([^&\s\"'<>]+)")
RECEIPT_NAMES = {"preparation", "coordination", "catalog", "cleanup", "policy-P", "policy-Q", "media-LA", "media-LB"}
CALIBRATIONS = (("P", "A1", "partial"), ("P", "A1", "complete"),
                ("Q", "B1", "partial"), ("Q", "B1", "complete"))
CALIBRATION_EVENTS = ("beforeZero", "playbackInfo", "started", "progress", "stopped", "beforeDelete", "delete", "afterDelete")
CALIBRATION_HEADERS = {"accept", "authorization", "x-emby-token", "host", "accept-encoding",
                       "connection", "content-length", "user-agent", "content-type"}


class TransportError(ValueError):
    """A frozen authority, persistence, transport, or consumption guard failed."""


def require(condition, message):
    if not condition:
        raise TransportError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False)


def sha_bytes(value):
    return hashlib.sha256(value).hexdigest()


def valid_sha(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def strict_json(raw):
    def pairs(values):
        result = {}
        for key, value in values:
            require(key not in result, "Duplicate JSON keys are forbidden.")
            result[key] = value
        return result

    def constant(value):
        raise TransportError("Nonfinite JSON constants are forbidden.")

    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_constant=constant)


def utc_now():
    return datetime.now(timezone.utc).isoformat()


def timestamp(value):
    require(isinstance(value, str), "A completed UTC timestamp is required.")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.utcoffset() is not None, "A completed timestamp must include its timezone.")
    return parsed


def protected(path, *, directory=False, uid=None):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts, "Authority paths must be absolute and normalized.")
    for entry in (path, *path.parents):
        info = entry.lstat()
        require(not stat.S_ISLNK(info.st_mode), "Authority paths cannot contain symlinks.")
        if entry == path:
            require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "Wrong authority path type.")
            require(uid is None or info.st_uid == uid, "Authority ownership changed.")
            require(not info.st_mode & 0o022, "Authority paths cannot be group/world writable.")
            require(directory or info.st_nlink == 1, "Authority files cannot have hard links.")
        else:
            require(stat.S_ISDIR(info.st_mode), "An authority ancestor is not a directory.")
            require(not info.st_mode & 0o022 or bool(info.st_mode & stat.S_ISVTX), "An authority ancestor is writable.")
    return path.lstat()


def contained(path, root):
    value, base = Path(path), Path(root)
    require(value.is_absolute() and base.is_absolute() and ".." not in value.parts and ".." not in base.parts,
            "Scoped paths must be absolute and normalized.")
    require(value != base and base in value.parents, "A path escaped its frozen scope.")
    return value


def read_file(path, *, uid, maximum=RECEIPT_LIMIT):
    info = protected(path, uid=uid)
    require(info.st_size <= maximum, "A private input exceeds its frozen size bound.")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as handle:
        opened = os.fstat(handle.fileno())
        require((opened.st_dev, opened.st_ino) == (info.st_dev, info.st_ino), "A private input changed during open.")
        value = handle.read(maximum + 1)
    require(len(value) <= maximum, "A private input grew beyond its bound.")
    return value


def file_sha(path, *, uid, maximum=128 * 1024 * 1024):
    return sha_bytes(read_file(path, uid=uid, maximum=maximum))


def load_planner(descriptor):
    name = "nextup_matrix_" + descriptor["sha256"]
    spec = importlib.util.spec_from_file_location(name, descriptor["path"])
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def _process_metadata(expected):
    """Read process metadata only; never read an application executable's bytes."""
    process = Path("/proc") / str(expected["pid"])
    raw = (process / "stat").read_text()
    fields = raw[raw.rfind(")") + 2:].split()
    executable = os.readlink(process / "exe")
    executable_info = (process / "exe").stat()
    return {"pid": expected["pid"], "startTicks": fields[19],
            "bootId": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
            "uid": process.stat().st_uid, "exe": executable,
            "exeDevice": executable_info.st_dev, "exeInode": executable_info.st_ino,
            "cmdline": [part.decode("utf-8", errors="strict") for part in (process / "cmdline").read_bytes().split(b"\0") if part],
            "networkNamespace": os.readlink(process / "ns/net"), "cgroup": (process / "cgroup").read_text()}


def process_identity(expected):
    """Bind the application metadata separately from its existing owned proxy."""
    application = _process_metadata(expected["application"])
    endpoint = _process_metadata(expected["endpoint"])
    process = Path("/proc") / str(expected["endpoint"]["pid"])
    listener = expected["endpoint"]["listener"]
    address = "0100007F:" + format(listener["port"], "04X")
    sockets = [line.split() for line in (process / "net/tcp").read_text().splitlines()[1:]]
    require(any(row[1] == address and row[3] == "0A" and row[9] == listener["socketInode"] for row in sockets),
            "The exact namespace loopback listener changed.")
    owned_listener = False
    for entry in (process / "fd").iterdir():
        try:
            owned_listener = owned_listener or os.readlink(entry) == "socket:[" + listener["socketInode"] + "]"
        except FileNotFoundError:
            continue
    require(owned_listener,
            "The bound target process does not own the listener socket.")
    endpoint["listener"] = dict(listener)
    return {"application": application, "endpoint": endpoint, "workerNetworkNamespace": os.readlink("/proc/self/ns/net")}


class Authority:
    """Pin concrete receipt files, source bytes, process facts, and the existing lock."""

    def __init__(self, execution, *, probe=process_identity):
        self.execution = strict_json(canonical(execution).encode())
        self.frozen = canonical(self.execution)
        self.probe, self.lock_fd, self.planner = probe, None, None
        value = self.execution
        require(value.get("schemaVersion") == 1 and set(value) ==
                {"schemaVersion", "ownerUid", "matrix", "scope", "lock", "process", "endpoint", "sources", "receipts", "credentials", "budgets"},
                "The execution manifest must use the exact version-one transport contract.")
        self.matrix = value["matrix"]
        require(self.matrix.get("target") == "reference", "Goby execution needs a separate source/schema/fixture receipt contract and is not enabled.")
        self.uid = value["ownerUid"]
        require(type(self.uid) is int and self.uid >= 0, "An explicit authority owner is required.")
        require(set(value["scope"]) == {"fixtureRoot", "receiptRoot", "sourceRoot", "proxySourceRoot", "credentialRoot", "evidenceParent"},
                "Every authority scope must be explicit.")
        self.scope_identities = {}
        for name, root in value["scope"].items():
            info = protected(root, directory=True, uid=self.uid)
            self.scope_identities[name] = (info.st_dev, info.st_ino)
        for name in ("receiptRoot", "credentialRoot"):
            require(not protected(value["scope"][name], directory=True, uid=self.uid).st_mode & 0o077,
                    "Private receipt and credential scopes must be owner-only.")
        self.output = contained(self.matrix["binding"]["evidenceRoot"], value["scope"]["evidenceParent"])
        require(self.output.parent == Path(value["scope"]["evidenceParent"]), "Evidence must be one fresh direct child.")
        require(set(value["lock"]) == {"path", "device", "inode"}, "The existing lock needs its exact file identity.")
        contained(value["lock"]["path"], value["scope"]["fixtureRoot"])
        require(set(value["endpoint"]) == {"scheme", "host", "port"} and value["endpoint"]["scheme"] == "http" and
                value["endpoint"]["host"] == "127.0.0.1" and type(value["endpoint"]["port"]) is int and
                1 <= value["endpoint"]["port"] <= 65535, "Only a manifest-bound namespace loopback HTTP endpoint is supported.")
        self.url = "http://127.0.0.1:" + str(value["endpoint"]["port"])
        required_process = {"pid", "startTicks", "bootId", "uid", "exe", "exeDevice", "exeInode", "cmdline", "networkNamespace", "cgroup"}
        require(set(value["process"]) == {"application", "endpoint", "workerNetworkNamespace"}, "Application and endpoint authority must be separate.")
        for name in ("application", "endpoint"):
            process = value["process"][name]
            require(set(process) == required_process | ({"listener"} if name == "endpoint" else set()) and
                    type(process["pid"]) is int and process["pid"] > 1 and type(process["uid"]) is int and process["uid"] >= 0 and
                    type(process["exeDevice"]) is int and type(process["exeInode"]) is int and process["exeInode"] > 0 and
                    isinstance(process["cmdline"], list) and process["cmdline"] and all(isinstance(part, str) for part in process["cmdline"]),
                    "Concrete metadata is required for each explicitly bound process.")
        require(value["process"]["workerNetworkNamespace"] == value["process"]["endpoint"]["networkNamespace"],
                "The recorder must use the existing proxy's network namespace.")
        listener = value["process"]["endpoint"]["listener"]
        require(isinstance(listener, dict) and set(listener) == {"port", "socketInode"} and
                listener["port"] == value["endpoint"]["port"] and isinstance(listener["socketInode"], str) and
                re.fullmatch(r"[1-9][0-9]*", listener["socketInode"]) is not None, "An exact owned listener socket must bind the endpoint.")
        binding = self.matrix["binding"]
        require(binding["processIdentity"] == value["process"]["application"] and binding["endpoint"] == self.url and
                binding["isolationIdentity"] == value["process"]["application"]["networkNamespace"], "Matrix and application target bindings differ.")
        require(set(value["sources"]) == {"matrix", "transport", "proxy"} and set(value["receipts"]) == RECEIPT_NAMES,
                "The exact source and preparation receipt set is required.")
        for name, descriptor in value["sources"].items():
            self._descriptor(descriptor, "proxySourceRoot" if name == "proxy" else "sourceRoot")
        require(value["sources"]["proxy"]["path"] in value["process"]["endpoint"]["cmdline"], "The endpoint command must identify its pinned owned proxy source.")
        require(Path(value["sources"]["proxy"]["path"]).suffix == ".py" and
                value["process"]["application"]["pid"] != value["process"]["endpoint"]["pid"], "Only the existing separate owned Python proxy contract is supported.")
        command = value["process"]["endpoint"]["cmdline"]
        for flag, expected in (("--reference-pid", str(value["process"]["application"]["pid"])),
                               ("--reference-start-ticks", value["process"]["application"]["startTicks"])):
            require(command.count(flag) == 1 and command.index(flag) + 1 < len(command) and command[command.index(flag) + 1] == expected,
                    "The existing proxy command does not bind the intended application process.")
        require(Path(value["sources"]["transport"]["path"]) == Path(__file__).absolute(), "Transport source authority must bind this file.")
        for descriptor in value["receipts"].values():
            self._descriptor(descriptor, "receiptRoot")
        self._descriptor(value["credentials"], "credentialRoot")
        budgets = value["budgets"]
        require(set(budgets) == {"requestSeconds", "normalSeconds", "cleanupSeconds", "requestBytes", "responseBytes", "totalResponseBytes", "cleanupResponseBytes"},
                "Explicit finite transport budgets are required.")
        bounds = {"requestSeconds": (1, 15), "normalSeconds": (1, 1800), "cleanupSeconds": (1, 600),
                  "requestBytes": (1024, 32768), "responseBytes": (1024, 1024 * 1024),
                  "totalResponseBytes": (4096, 128 * 1024 * 1024), "cleanupResponseBytes": (1024, 80 * 1024 * 1024)}
        for key, (minimum, maximum) in bounds.items():
            require(type(budgets[key]) is int and minimum <= budgets[key] <= maximum, "A transport budget is outside its bound: " + key)
        require(budgets["cleanupResponseBytes"] < budgets["totalResponseBytes"] and
                budgets["cleanupResponseBytes"] >= 80 * (budgets["responseBytes"] + 1), "The byte reserve must cover eighty bounded cleanup responses.")

    def _descriptor(self, descriptor, scope):
        require(isinstance(descriptor, dict) and set(descriptor) == {"path", "sha256"} and valid_sha(descriptor["sha256"]),
                "A private input needs an exact path and SHA-256.")
        contained(descriptor["path"], self.execution["scope"][scope])

    def _read(self, descriptor):
        require(not protected(descriptor["path"], uid=self.uid).st_mode & 0o077, "Private inputs must be owner-only.")
        raw = read_file(descriptor["path"], uid=self.uid)
        require(sha_bytes(raw) == descriptor["sha256"], "A frozen private input changed.")
        return strict_json(raw)

    def acquire(self):
        require(self.lock_fd is None, "Fixture coordination cannot be reacquired.")
        lock = self.execution["lock"]
        info = protected(lock["path"], uid=self.uid)
        require((info.st_dev, info.st_ino) == (lock["device"], lock["inode"]), "The existing lock identity changed.")
        descriptor = os.open(lock["path"], os.O_RDWR | os.O_NOFOLLOW)
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.lock_fd = descriptor
            self.check()
        except BaseException:
            os.close(descriptor)
            self.lock_fd = None
            raise

    def check(self):
        require(canonical(self.execution) == self.frozen and self.lock_fd is not None, "Frozen execution authority is unavailable.")
        value, matrix = self.execution, self.matrix
        for name, root in value["scope"].items():
            info = protected(root, directory=True, uid=self.uid)
            require((info.st_dev, info.st_ino) == self.scope_identities[name], "An authority scope directory was replaced.")
            if name in ("receiptRoot", "credentialRoot"):
                require(not info.st_mode & 0o077, "A private authority scope lost owner-only permissions.")
        lock = value["lock"]
        info = protected(lock["path"], uid=self.uid)
        opened = os.fstat(self.lock_fd)
        require((info.st_dev, info.st_ino) == (opened.st_dev, opened.st_ino) == (lock["device"], lock["inode"]),
                "The held existing fixture lock was replaced.")
        require(self.probe(value["process"]) == value["process"], "The bound target process or namespace changed.")
        for descriptor in value["sources"].values():
            require(file_sha(descriptor["path"], uid=self.uid) == descriptor["sha256"], "A frozen driver source changed.")
        if self.planner is None:
            self.planner = load_planner(value["sources"]["matrix"])
        self.planner.validate_manifest(matrix)
        receipts = {name: self._read(descriptor) for name, descriptor in value["receipts"].items()}
        for name, receipt in receipts.items():
            require(receipt.get("schemaVersion") == 1 and receipt.get("kind") == "nextup-global-" + name and
                    receipt.get("runId") == matrix["runId"] and receipt.get("target") == matrix["target"],
                    "A preparation receipt belongs to a different run, target, or contract.")
        for name, key in (("preparation", "preparationReceiptSha256"), ("coordination", "coordinationReceiptSha256"), ("catalog", "catalogReceiptSha256")):
            require(value["receipts"][name]["sha256"] == matrix[key], "A matrix receipt digest differs.")
        preparation = receipts["preparation"]["facts"]
        require(preparation == {"serverId": matrix["serverId"], "process": value["process"], "endpoint": value["endpoint"],
                "version": matrix["binding"]["version"], "evidenceRoot": matrix["binding"]["evidenceRoot"],
                "credentialStoreSha256": value["credentials"]["sha256"],
                "catalogReceiptSha256": matrix["catalogReceiptSha256"], "actorIds": {key: row["userId"] for key, row in matrix["actors"].items()},
                "retainedArtifacts": ["accounts", "catalog", "media", "protocol-audit"]}, "Preparation facts do not bind the current execution inputs.")
        coordination = receipts["coordination"]["facts"]
        require(set(coordination) == {"lock", "fixtureRoot", "releasedAt", "process"} and coordination["lock"] == lock and
                coordination["fixtureRoot"] == value["scope"]["fixtureRoot"] and coordination["process"] == value["process"],
                "The release receipt does not bind the held fixture lock and target.")
        timestamp(coordination["releasedAt"])
        require(receipts["catalog"]["facts"] == {"libraries": matrix["libraries"], "items": matrix["items"]}, "The prepared catalog mapping differs.")
        credentials = self._read(value["credentials"])
        require(credentials.get("schemaVersion") == 1 and credentials.get("runId") == matrix["runId"] and
                set(credentials.get("actors", {})) == {"P", "Q"}, "A dedicated two-actor credential store is required.")
        for actor in ("P", "Q"):
            row, credential = matrix["actors"][actor], credentials["actors"][actor]
            require(set(credential) == {"credentialRef", "userId", "username", "password"} and
                    all(credential[key] == row[key] for key in ("credentialRef", "userId", "username")) and
                    isinstance(credential["password"], str) and len(credential["password"]) >= 32, "An owned actor credential differs.")
            policy = receipts["policy-" + actor]["facts"]
            require(value["receipts"]["policy-" + actor]["sha256"] == row["policyReceiptSha256"] and
                    set(policy) == {"previousUserIds", "createdUser", "observedProfile"} and isinstance(policy["previousUserIds"], list) and
                    row["userId"] not in policy["previousUserIds"] and policy["createdUser"] == {"Id": row["userId"], "Name": row["username"]},
                    "The account lacks retained absence and creation response facts.")
            profile = policy["observedProfile"]
            require(profile.get("Id") == row["userId"] and profile.get("Name") == row["username"] and
                    profile.get("Policy", {}).get("IsAdministrator") is False and profile["Policy"].get("EnableAllFolders") is False and
                    set(profile["Policy"].get("EnabledFolders", [])) == set(row["allowedFolderIds"]) and isinstance(profile.get("Configuration"), dict),
                    "The prepared ordinary profile or exact folder grants differ.")
        require(credentials["actors"]["P"]["password"] != credentials["actors"]["Q"]["password"], "Actor passwords must be independent.")
        for library in ("LA", "LB"):
            media = receipts["media-" + library]["facts"]
            require(value["receipts"]["media-" + library]["sha256"] == matrix["libraries"][library]["mediaRootReceiptSha256"] and
                    set(media) == {"sourceRootId", "rootPath", "files"} and media["sourceRootId"] == matrix["libraries"][library]["sourceRootId"],
                    "The private media root receipt differs.")
            root = contained(media["rootPath"], value["scope"]["fixtureRoot"])
            protected(root, directory=True, uid=self.uid)
            expected = {name for name in matrix["items"] if name in ("A1", "A2", "A3", "B1", "B2", "B3") and matrix["items"][name]["library"] == library}
            require(set(media["files"]) == expected, "The immutable media set differs.")
            for name, media_file in media["files"].items():
                require(set(media_file) == {"path", "sha256", "sizeBytes"} and media_file["sha256"] == matrix["items"][name]["mediaSha256"] and
                        type(media_file["sizeBytes"]) is int and 0 < media_file["sizeBytes"] <= 128 * 1024 * 1024, "A media identity or bound differs.")
                path = contained(media_file["path"], root)
                require(protected(path, uid=self.uid).st_size == media_file["sizeBytes"] and
                        file_sha(path, uid=self.uid, maximum=media_file["sizeBytes"]) == media_file["sha256"], "Immutable owned media changed.")
        self._cleanup_proof(receipts["cleanup"]["facts"])
        require(value["receipts"]["cleanup"]["sha256"] == matrix["cleanupContract"]["receiptSha256"], "The cleanup receipt digest differs.")
        self.credentials = credentials["actors"]

    def _cleanup_proof(self, facts):
        matrix = self.matrix
        require(isinstance(facts, dict) and set(facts) == {"contractVersion", "process", "serverId", "actors", "calibrations"} and
                type(facts["contractVersion"]) is int and facts["contractVersion"] == 3 and
                facts["process"] == self.execution["process"] and facts["serverId"] == matrix["serverId"],
                "The version-three four-calibration cleanup contract with complete stopped lifecycles is required.")
        require(isinstance(facts["actors"], dict) and set(facts["actors"]) == {"P", "Q"},
                "Cleanup calibration must acknowledge both ordinary preparation actors.")
        actors = {actor: self._calibration_actor(actor, facts["actors"][actor]) for actor in ("P", "Q")}
        for key in ("tokenSha256", "sessionId", "deviceId"):
            require(actors["P"][key] != actors["Q"][key], "Preparation actors must have independent tokens, sessions, and devices.")
        receipts = {facts["actors"][actor]["login"]["responseReceiptSha256"] for actor in ("P", "Q")}
        require(len(receipts) == 2, "Each preparation login must retain its own independent response receipt.")
        rows = facts["calibrations"]
        require(isinstance(rows, list) and len(rows) == len(CALIBRATIONS), "Exactly four owned cleanup calibrations are required.")
        baselines, previous_ordinal, previous_time, play_sessions = {}, 0, None, set()
        for row, expected in zip(rows, CALIBRATIONS):
            require(isinstance(row, dict) and set(row) == {"actor", "item", "mode", *CALIBRATION_EVENTS} and
                    (row["actor"], row["item"], row["mode"]) == expected,
                    "Calibration order must be P/A1 partial, P/A1 complete, Q/B1 partial, Q/B1 complete.")
            actor, item, mode = expected
            details = {}
            for name in CALIBRATION_EVENTS:
                event = row[name]
                body = self._calibration_event(event, actor, item, actors[actor], kind=name)
                require(event["responseReceiptSha256"] not in receipts, "A historical response receipt cannot be reused as another calibration observation.")
                receipts.add(event["responseReceiptSha256"])
                try:
                    completed = timestamp(event["completedAt"])
                except (ValueError, TypeError) as error:
                    raise TransportError("Calibration response completion times must be timezone-qualified timestamps.") from error
                require(event["ordinal"] > previous_ordinal and (previous_time is None or completed >= previous_time),
                        "Completed calibration observations must retain their actual increasing request order.")
                previous_ordinal, previous_time = event["ordinal"], completed
                if name in ("beforeZero", "beforeDelete", "afterDelete"):
                    details[name] = self._calibration_detail(body, item)
            runtime = row["beforeZero"]["response"]["body"]["RunTimeTicks"]
            info = row["playbackInfo"]["response"]["body"]
            require(isinstance(info, dict) and isinstance(info.get("MediaSources"), list) and len(info["MediaSources"]) == 1 and
                    isinstance(info["MediaSources"][0], dict) and type(info["MediaSources"][0].get("RunTimeTicks")) is int and
                    info["MediaSources"][0]["RunTimeTicks"] == runtime, "PlaybackInfo must acknowledge one source with the actual bound Episode runtime.")
            try:
                self.planner.require_id(info.get("PlaySessionId"), "Calibration PlaySessionId")
                self.planner.require_id(info["MediaSources"][0].get("Id"), "Calibration MediaSourceId")
            except (ValueError, TypeError) as error:
                raise TransportError("Calibration playback identifiers must be real acknowledged public identifiers.") from error
            require(info["PlaySessionId"] not in play_sessions, "A calibration cannot reuse a prior actor or mode's play session.")
            play_sessions.add(info["PlaySessionId"])
            context = {"ItemId": matrix["items"][item]["id"], "MediaSourceId": info["MediaSources"][0]["Id"],
                "PlaySessionId": info["PlaySessionId"], "SessionId": actors[actor]["sessionId"]}
            target = self.planner.PARTIAL_TICKS if mode == "partial" else runtime
            started = {**context, "RunTimeTicks": runtime, "PositionTicks": 0, "CanSeek": True, "IsPaused": False,
                "IsMuted": False, "PlayMethod": "DirectStream", "PlaybackRate": 1}
            expected_bodies = {"started": started, "progress": {**started, "PositionTicks": target, "EventName": "TimeUpdate"},
                "stopped": {**context, "PositionTicks": target, "Failed": False, "IsAutomated": False}}
            require(all(canonical(row[name]["request"]["body"]) == canonical(body) for name, body in expected_bodies.items()),
                    "The completed start/progress/STOP receipts do not bind the exact login, source, play session, runtime, and target.")
            baseline, changed, restored = details["beforeZero"], details["beforeDelete"], details["afterDelete"]
            require(self.planner.zero_state(baseline) and self.planner.zero_state(restored) and
                    canonical(baseline) == canonical(restored), "The exact complete zero UserData was not restored after this calibration.")
            if actor in baselines:
                require(canonical(baselines[actor]) == canonical(baseline), "Partial and complete calibrations must share the same measured actor/item baseline.")
            else:
                baselines[actor] = baseline
            value = changed["value"]
            require(value["PlayCount"] > 0, "Calibration playback must persist a positive integer count.")
            try:
                self.planner.parse_date(value.get("LastPlayedDate"))
            except (ValueError, TypeError) as error:
                raise TransportError("Calibration playback must retain an actual timezone-qualified playback date.") from error
            if mode == "partial":
                require(value["Played"] is False and value["PlaybackPositionTicks"] == self.planner.PARTIAL_TICKS,
                        "Partial calibration must prove unplayed state at exactly 120 seconds.")
            else:
                require(value["Played"] is True, "Complete calibration must prove actual Played=true at the authoritative duration.")
            try:
                self.planner.require_playback_userdata_change(baseline["value"], changed["value"], runtime_ticks=runtime)
            except (ValueError, TypeError) as error:
                raise TransportError("Confirmed calibration playback changed unrelated fields or an unproved derived percentage.") from error

    def _calibration_actor(self, actor, value):
        mapped = self.matrix["actors"][actor]
        require(isinstance(value, dict) and set(value) == {"credentialRef", "deviceId", "login"} and
                value["credentialRef"] == mapped["credentialRef"], "The calibration actor credential binding differs.")
        login = value["login"]
        require(isinstance(login, dict) and set(login) == {"status", "body", "responseReceiptSha256"} and
                type(login["status"]) is int and login["status"] == 200 and valid_sha(login["responseReceiptSha256"]),
                "An actual completed preparation login receipt is required.")
        body = login["body"]
        require(isinstance(body, dict) and body.get("ServerId") == self.matrix["serverId"] and
                isinstance(body.get("AccessToken"), str) and body["AccessToken"], "Calibration login did not acknowledge the bound server and token.")
        user, session = body.get("User"), body.get("SessionInfo")
        require(isinstance(user, dict) and isinstance(user.get("Policy"), dict) and isinstance(session, dict) and
                user.get("Id") == mapped["userId"] and user.get("Name") == mapped["username"] and
                user["Policy"].get("IsAdministrator") is False and session.get("UserId") == mapped["userId"] and
                session.get("DeviceId") == value["deviceId"], "Calibration login must identify this ordinary actor and its own preparation device/session.")
        try:
            self.planner.require_id(value["deviceId"], "Preparation device ID")
            self.planner.require_id(session.get("Id"), "Preparation session ID")
        except (ValueError, TypeError) as error:
            raise TransportError("Preparation device and session identifiers must be explicit public identifiers.") from error
        return {"tokenSha256": sha_bytes(body["AccessToken"].encode()), "sessionId": session["Id"], "deviceId": value["deviceId"]}

    def _calibration_event(self, event, actor, item, context, *, kind):
        require(isinstance(event, dict) and set(event) == {"ordinal", "completedAt", "responseReceiptSha256", "request", "response"} and
                type(event["ordinal"]) is int and event["ordinal"] > 0 and valid_sha(event["responseReceiptSha256"]),
                "Every calibration observation needs its actual ordinal and response receipt digest.")
        request, response = event["request"], event["response"]
        user, item_id = self.matrix["actors"][actor]["userId"], self.matrix["items"][item]["id"]
        routes = {"beforeZero": ("GET", "/emby/Users/" + user + "/Items/" + item_id, 200),
            "beforeDelete": ("GET", "/emby/Users/" + user + "/Items/" + item_id, 200),
            "afterDelete": ("GET", "/emby/Users/" + user + "/Items/" + item_id, 200),
            "delete": ("DELETE", "/emby/Users/" + user + "/PlayedItems/" + item_id, 200),
            "playbackInfo": ("POST", "/emby/Items/" + item_id + "/PlaybackInfo", 200),
            "started": ("POST", "/emby/Sessions/Playing", 204), "progress": ("POST", "/emby/Sessions/Playing/Progress", 204),
            "stopped": ("POST", "/emby/Sessions/Playing/Stopped", 204)}
        method, route, status = routes[kind]
        require(isinstance(request, dict) and set(request) == {"method", "route", "headers", "body"} and
                request["method"] == method and request["route"] == route and
                (request["body"] is None if method in ("GET", "DELETE") else isinstance(request["body"], dict)),
                "Calibration operations must use their exact actor/item route and request body shape without extra query flags.")
        if kind == "playbackInfo":
            require(canonical(request["body"]) == canonical({"UserId": user, "IsPlayback": True}),
                    "Calibration negotiation must use the exact ordinary actor and playback flag.")
        headers = request["headers"]
        require(len(bounded_header_prefix(headers)) == len(headers) and
                all(key.lower() in CALIBRATION_HEADERS and "\r" not in key + value and "\n" not in key + value for key, value in headers),
                "Calibration request headers exceed their bound or contain an unreviewed header or line break.")
        expected_length = "0" if request["body"] is None else str(len(canonical(request["body"]).encode()))
        require(all(value == expected_length for key, value in headers if key.lower() == "content-length"),
                "Calibration content length differs from its exact complete request body.")
        tokens = [value for key, value in headers if key.lower() == "x-emby-token"]
        authorization = [value for key, value in headers if key.lower() == "authorization"]
        require(len(tokens) == 1 and sha_bytes(tokens[0].encode()) == context["tokenSha256"] and len(authorization) == 1 and
                not any(key.lower() in {"x-mediabrowser-token", "cookie"} for key, value in headers),
                "Each calibration request must use exactly this actor's acknowledged preparation token.")
        metadata = {}
        text = authorization[0]
        require(text.startswith("Emby "), "Calibration requests must retain their actual Emby device metadata.")
        for entry in text[5:].split(", "):
            match = re.fullmatch(r'([A-Za-z][A-Za-z0-9]*)="([^"\\\r\n]+)"', entry)
            require(match is not None and match.group(1) not in metadata, "Calibration device metadata is malformed or duplicated.")
            metadata[match.group(1)] = match.group(2)
        require(set(metadata) == {"Client", "Device", "DeviceId", "Version"} and metadata["DeviceId"] == context["deviceId"],
                "Calibration requests cannot substitute another actor's device or an additional token source.")
        require(isinstance(response, dict) and set(response) == {"status", "body"} and type(response["status"]) is int and response["status"] == status,
                "Each calibration operation requires its actual expected complete HTTP status.")
        return response["body"]

    def _calibration_detail(self, body, item):
        mapped = self.matrix["items"][item]
        require(isinstance(body, dict) and body.get("Id") == mapped["id"] and body.get("Type") == "Episode" and
                body.get("ParentId") == mapped["parentId"] and body.get("SeriesId") == mapped["seriesId"] and
                type(body.get("ParentIndexNumber")) is int and body["ParentIndexNumber"] == mapped["parentIndexNumber"] and
                type(body.get("IndexNumber")) is int and body["IndexNumber"] == mapped["indexNumber"] and
                type(body.get("RunTimeTicks")) is int and body["RunTimeTicks"] == self.planner.RUNTIME_TICKS,
                "Every calibration full detail must retain its mapped episode identity, relations, numbering, and runtime.")
        try:
            fact = self.planner.userdata_fact(body)
            self.planner.require_episode_percentage(fact["value"], runtime_ticks=body["RunTimeTicks"])
            return fact
        except (ValueError, TypeError) as error:
            raise TransportError("Calibration UserData must preserve the planner's complete field and primitive-type contract.") from error

    def close(self):
        if self.lock_fd is not None:
            os.close(self.lock_fd)
            self.lock_fd = None


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


class Journal:
    """Exclusive receipts and atomic private snapshots; existing runs never resume."""

    def __init__(self, root, *, uid):
        self.root, self.uid = Path(root), uid
        self.directory_fds, self.identities = {}, {}
        parent_info = protected(self.root.parent, directory=True, uid=uid)
        parent_fd = os.open(self.root.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            opened = os.fstat(parent_fd)
            require((opened.st_dev, opened.st_ino) == (parent_info.st_dev, parent_info.st_ino), "The evidence parent changed during open.")
            os.mkdir(self.root.name, mode=0o700, dir_fd=parent_fd)
            os.fsync(parent_fd)
            root_fd = os.open(self.root.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent_fd)
            self.directory_fds["root"] = root_fd
            for name in ("private", "export"):
                os.mkdir(name, mode=0o700, dir_fd=root_fd)
                self.directory_fds[name] = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=root_fd)
            os.fsync(root_fd)
            for name, descriptor in self.directory_fds.items():
                info = os.fstat(descriptor)
                self.identities[name] = (info.st_dev, info.st_ino)
            self.check()
        except BaseException:
            self.close()
            raise
        finally:
            os.close(parent_fd)

    def check(self):
        require(set(self.directory_fds) == {"root", "private", "export"}, "Journal directory authority is closed.")
        for name, descriptor in self.directory_fds.items():
            path = self.root if name == "root" else self.root / name
            info = protected(path, directory=True, uid=self.uid)
            opened = os.fstat(descriptor)
            require(not info.st_mode & 0o077 and
                    (info.st_dev, info.st_ino) == (opened.st_dev, opened.st_ino) == self.identities[name],
                    "A private journal directory changed its identity or owner-only mode.")

    def save(self, name, value, *, export=False):
        self.check()
        require(re.fullmatch(r"[A-Za-z0-9_-]+\.json", name) is not None, "Invalid journal filename.")
        parent_fd = self.directory_fds["export" if export else "private"]
        raw = (canonical(value) + "\n").encode()
        descriptor = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=parent_fd)
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(raw)
            handle.flush()
            os.fsync(handle.fileno())
        os.fsync(parent_fd)
        return sha_bytes(raw)

    def state(self, value):
        self.check()
        parent = self.root / "private"
        path, temporary = parent / "state.json", parent / "state-next.json"
        if path.exists():
            require(not protected(path, uid=self.uid).st_mode & 0o077, "Private state must remain owner-only.")
        self.save(temporary.name, value)
        descriptor = self.directory_fds["private"]
        os.replace(temporary.name, path.name, src_dir_fd=descriptor, dst_dir_fd=descriptor)
        os.fsync(descriptor)

    def close(self):
        for descriptor in self.directory_fds.values():
            os.close(descriptor)
        self.directory_fds.clear()


@dataclass(frozen=True)
class WireResponse:
    status: int | None
    headers: list
    raw: bytes
    complete_http: bool
    completed_at: str
    failure: str | None = None


class HTTPTransport:
    """A single bounded request with no cookies, redirects, or implicit retry."""

    def __init__(self, endpoint):
        self.endpoint = dict(endpoint)
        self.frozen_endpoint = canonical(self.endpoint)

    def send(self, request, headers, payload, *, timeout_seconds, max_bytes):
        require(canonical(self.endpoint) == self.frozen_endpoint, "The HTTP transport endpoint changed.")
        require(isinstance(timeout_seconds, (int, float)) and not isinstance(timeout_seconds, bool) and math.isfinite(timeout_seconds) and
                0 < timeout_seconds <= 15 and type(max_bytes) is int and 0 < max_bytes <= 1024 * 1024 + 1,
                "HTTP dispatch requires finite time and response bounds.")
        request_metadata_size(request, headers)
        connection = http.client.HTTPConnection(self.endpoint["host"], self.endpoint["port"], timeout=timeout_seconds)
        status, response_headers, raw, complete, failure = None, [], b"", False, None
        old_handler = signal.getsignal(signal.SIGALRM)

        def timeout_handler(signum, frame):
            raise TimeoutError("The bounded HTTP deadline expired.")

        signal.signal(signal.SIGALRM, timeout_handler)
        signal.setitimer(signal.ITIMER_REAL, timeout_seconds)
        try:
            connection.request(request.method, request.route, body=payload, headers=headers)
            response = connection.getresponse()
            status = response.status
            actual_headers = response.getheaders()
            response_headers = bounded_header_prefix(actual_headers)
            require(len(response_headers) == len(actual_headers), "Response headers exceeded their fixed capture bound.")
            chunks = bytearray()
            while len(chunks) < max_bytes:
                if response.length == 0:
                    complete = True
                    break
                chunk = response.read(min(65536, max_bytes - len(chunks)))
                chunks.extend(chunk)
                raw = bytes(chunks)
                if not chunk or response.isclosed():
                    complete = response.length in (None, 0)
                    break
                if response.length == 0:
                    complete = True
                    break
            declared = [value for key, value in response_headers if key.lower() == "content-length"]
            if declared:
                complete = complete and len(set(declared)) == 1 and declared[0].isdigit() and int(declared[0]) == len(raw)
            if declared and any(key.lower() == "transfer-encoding" for key, value in response_headers):
                complete = False
        except Exception as error:
            failure = type(error).__name__
            if isinstance(error, http.client.IncompleteRead):
                raw = (raw + error.partial)[:max_bytes]
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, old_handler)
            connection.close()
        return WireResponse(status, response_headers, raw, complete, utc_now(), failure)


def bounded_header_prefix(headers):
    require(isinstance(headers, list), "Response headers must retain ordered pairs.")
    result, size = [], 0
    for pair in headers:
        require(isinstance(pair, (tuple, list)) and len(pair) == 2 and all(isinstance(part, str) for part in pair),
                "Response header pairs must be strings.")
        addition = len((pair[0] + ": " + pair[1] + "\r\n").encode("utf-8"))
        if len(result) >= HEADER_COUNT or size + addition > HEADER_BYTES:
            break
        result.append(pair)
        size += addition
    return result


def request_metadata_size(request, headers):
    require(isinstance(headers, dict) and len(headers) <= HEADER_COUNT and
            all(isinstance(key, str) and isinstance(value, str) and "\r" not in key + value and "\n" not in key + value
                for key, value in headers.items()), "Request metadata contains invalid or excessive headers.")
    size = len((request.method + " " + request.route + " HTTP/1.1\r\n").encode("utf-8"))
    size += sum(len((key + ": " + value + "\r\n").encode("utf-8")) for key, value in headers.items())
    require(size <= REQUEST_META_BYTES, "The route and request headers exceed their fixed byte bound.")
    return size


def sanitized(value, secrets):
    if isinstance(value, dict):
        result = {}
        for key, item in value.items():
            safe_key = sanitized(str(key), secrets)
            require(safe_key not in result, "Sanitized response keys would collide.")
            result[safe_key] = "[redacted]" if key.lower() in SECRET_KEYS else sanitized(item, secrets)
        return result
    if isinstance(value, list):
        return [sanitized(item, secrets) for item in value]
    if isinstance(value, str):
        for secret in sorted((item for item in secrets if item), key=len, reverse=True):
            value = value.replace(secret, "[redacted]")
        return URL_SECRET.sub(lambda match: match.group(1) + "[redacted]", value)
    return value


class TransportRunner:
    """Drive one fresh matrix while independently retaining every attempted responsibility."""

    def __init__(self, execution, *, transport=None, probe=process_identity, monotonic=time.monotonic, sleeper=time.sleep):
        self.authority = Authority(execution, probe=probe)
        self.authority.acquire()
        try:
            self.execution = self.authority.execution
            module = self.authority.planner
            self.module, self.matrix = module, module.Matrix(self.execution["matrix"])
            self.journal = Journal(self.authority.output, uid=self.authority.uid)
            self.transport = transport or HTTPTransport(self.execution["endpoint"])
            self.monotonic, self.sleeper = monotonic, sleeper
            self.started = self.monotonic()
            self.last_clock = self.started
            require(isinstance(self.started, (int, float)) and not isinstance(self.started, bool) and math.isfinite(self.started), "Invalid monotonic clock.")
            self.cleanup_deadline = None
            self.tokens, self.unverified_logins = {}, {}
            self.attempted, self.charged_bytes = set(), 0
            self.failure, self.blocked, self.finished = None, False, False
            self.failure_events = []
            self.persistence_failure, self.evidence_complete = None, True
            self.unresolved_responses = []
            self.secrets = {row["password"] for row in self.authority.credentials.values()}
            self.secrets.update(row["deviceId"] for row in self.execution["matrix"]["actors"].values())
            self.secrets.update(str(value) for value in self.execution["scope"].values())
            self.secrets.add(str(self.authority.output))
            self.journal.save("execution.json", self.execution)
            self.journal.save("plan.json", self.matrix.frozen_plan)
            self.persist()
        except BaseException:
            if hasattr(self, "journal"):
                self.journal.close()
            self.authority.close()
            raise

    def elapsed(self):
        value = self.monotonic()
        require(isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value) and value >= self.last_clock,
                "The monotonic transport clock moved backwards or became nonfinite.")
        self.last_clock = value
        return value - self.started

    def persist(self):
        self.journal.state({"schemaVersion": 1, "matrix": self.matrix.private_state(), "tokens": self.tokens,
            "unverifiedLogins": self.unverified_logins, "attemptedLabels": sorted(self.attempted), "chargedResponseBytes": self.charged_bytes,
            "failure": self.failure, "blocked": self.blocked, "finished": self.finished,
            "persistenceFailure": self.persistence_failure, "unresolvedResponses": self.unresolved_responses, "failureEvents": self.failure_events})

    def _secrets_from(self, value):
        if isinstance(value, dict):
            session = value.get("SessionInfo")
            if isinstance(session, dict) and isinstance(session.get("Id"), str):
                self.secrets.add(session["Id"])
            sources = value.get("MediaSources")
            if isinstance(sources, list):
                for source in sources:
                    if isinstance(source, dict) and isinstance(source.get("Id"), str):
                        self.secrets.add(source["Id"])
            for key, item in value.items():
                if key.lower() in SECRET_KEYS and isinstance(item, str):
                    self.secrets.add(item)
                self._secrets_from(item)
        elif isinstance(value, list):
            for item in value:
                self._secrets_from(item)
        elif isinstance(value, str):
            for match in URL_SECRET.finditer(value):
                self.secrets.update((match.group(2), unquote(match.group(2))))

    def _failure(self, kind, error, *, lost=False):
        self.failure = {"kind": kind, "errorType": type(error).__name__, "message": str(error)}
        self.failure_events.append(self.failure)
        self.blocked = True
        if lost and self.matrix.pending is not None:
            self.matrix.lost_response(kind)
        try:
            self.persist()
        except BaseException as persistence_error:
            self.evidence_complete = False
            self.persistence_failure = {"errorType": type(persistence_error).__name__, "message": str(persistence_error)}

    def _dispatch(self, request):
        require(not self.blocked and not self.finished and request.label not in self.attempted, "An attempted or failed run cannot dispatch again.")
        self.authority.check()
        self.journal.check()
        if isinstance(self.transport, HTTPTransport):
            require(self.transport.endpoint == self.execution["endpoint"], "The transport no longer targets the approved endpoint.")
        elapsed = self.elapsed()
        budgets = self.execution["budgets"]
        deadline = self.cleanup_deadline if request.cleanup else self.started + budgets["normalSeconds"]
        remaining = deadline - (self.started + elapsed)
        byte_limit = budgets["totalResponseBytes"] - (0 if request.cleanup else budgets["cleanupResponseBytes"]) - self.charged_bytes
        require(remaining > 0 and byte_limit > budgets["responseBytes"], "The phase deadline or response-byte reserve is exhausted.")
        actor = self.execution["matrix"]["actors"][request.actor]
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="Goby Global NextUp Recorder", Device="Linux Protocol Research", DeviceId="' + actor["deviceId"] + '", Version="1.0"'}
        token_sha = None
        if request.login:
            credential = self.authority.credentials[request.actor]
            payload = urlencode({"Username": credential["username"], "Pw": credential["password"]}).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        else:
            token = self.tokens[request.actor]
            token_sha = sha_bytes(token.encode())
            headers["X-Emby-Token"] = token
            payload = request.body_json.encode() if request.body_json is not None else None
            if payload is not None:
                headers["Content-Type"] = "application/json"
        require(payload is None or len(payload) <= budgets["requestBytes"], "The request body exceeds its frozen budget.")
        request_metadata_size(request, headers)
        ordinal = self.matrix.count + 1
        prefix = str(ordinal).zfill(4) + "-" + request.label
        intent = {"ordinal": ordinal, "request": request.fact(), "planSha256": self.matrix.plan_sha256,
                  "executionSha256": sha_bytes(self.authority.frozen.encode()), "actorTokenSha256": token_sha,
                  "matrixState": self.matrix.private_state(), "sourceSha256": self.execution["sources"]["transport"]["sha256"]}
        intent_sha = self.journal.save(prefix + "-intent.json", intent)
        self.matrix.authorize(request, intent_sha, elapsed, actor_token_sha256=token_sha)
        self.journal.save(prefix + "-reserved.json", {"ordinal": ordinal, "intentReceiptSha256": intent_sha, "matrixState": self.matrix.private_state()})
        self.persist()
        self.journal.check()
        self.authority.check()
        remaining = deadline - (self.started + self.elapsed())
        require(remaining > 0, "Write-ahead persistence consumed the remaining phase deadline.")
        self.attempted.add(request.label)
        try:
            response = self.transport.send(request, headers, payload, timeout_seconds=min(budgets["requestSeconds"], remaining), max_bytes=budgets["responseBytes"] + 1)
        except BaseException as error:
            self._failure("transport-exception", error, lost=True)
            return
        try:
            require(isinstance(response, WireResponse) and isinstance(response.raw, bytes), "The transport did not return a bounded wire response.")
            capture_limit = budgets["responseBytes"] + 1
            captured = response.raw[:capture_limit]
            captured_headers = bounded_header_prefix(response.headers)
            self.charged_bytes += len(captured) if response.complete_http else capture_limit
            timestamp(response.completed_at)
            wire = {"ordinal": ordinal, "intentReceiptSha256": intent_sha, "request": request.fact(), "requestHeaders": headers,
                    "requestBodyBase64": base64.b64encode(payload or b"").decode(), "responseStatus": response.status,
                    "responseHeaders": captured_headers, "responseBodyBase64": base64.b64encode(captured).decode(),
                    "completeHTTP": response.complete_http and len(response.raw) <= capture_limit and len(captured_headers) == len(response.headers),
                    "adapterTruncatedCapture": len(response.raw) > capture_limit, "reportedResponseBytes": len(response.raw),
                    "headerCaptureTruncated": len(captured_headers) != len(response.headers),
                    "completedAt": response.completed_at, "transportFailure": response.failure}
            response_sha = self.journal.save(prefix + "-wire.json", wire)
            require(response.complete_http is True and response.failure is None and len(response.raw) <= budgets["responseBytes"] and
                    len(captured_headers) == len(response.headers) and type(response.status) is int and 100 <= response.status <= 599,
                    "The actual HTTP response is incomplete or exceeds its bound.")
            require(isinstance(response.headers, list) and all(isinstance(pair, (tuple, list)) and len(pair) == 2 and
                    all(isinstance(part, str) for part in pair) for pair in response.headers), "The actual response header capture is invalid.")
            lengths = [value for key, value in response.headers if key.lower() == "content-length"]
            if lengths:
                require(len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(response.raw) and
                        not any(key.lower() == "transfer-encoding" for key, value in response.headers), "HTTP framing did not prove a complete response.")
            content_types = [value.split(";", 1)[0].strip().lower() for key, value in response.headers if key.lower() == "content-type"]
            require(len(set(content_types)) <= 1, "Conflicting response media types are not consumable.")
            status_only = request.route in ("/emby/Sessions", "/emby/Sessions/Logout") and response.status == 401
            declared_json = any(value == "application/json" or value.endswith("+json") for value in content_types)
            try:
                body = strict_json(response.raw) if response.raw else None
            except json.JSONDecodeError:
                require(status_only and not declared_json, "A required JSON response was malformed.")
                body = response.raw.decode("utf-8", errors="strict")
            self._secrets_from(body)
            for key, value in response.headers:
                if key.lower() in SECRET_KEYS:
                    self.secrets.add(value)
            if request.login and isinstance(body, dict) and isinstance(body.get("AccessToken"), str) and body["AccessToken"]:
                self.unverified_logins[request.actor] = body
                self.persist()
            self.journal.save(prefix + "-response.json", sanitized({"ordinal": ordinal, "actor": request.actor,
                "request": request.fact(), "response": {"status": response.status,
                    "headers": [[key, "[redacted]" if key.lower() in SECRET_KEYS else sanitized(value, self.secrets)]
                                for key, value in response.headers], "body": body},
                "completedAt": response.completed_at, "completeHTTP": True, "privateWireSha256": response_sha}, self.secrets), export=True)
        except BaseException as error:
            self._failure("response-not-consumable", error, lost=True)
            return
        try:
            self.authority.check()
            self.matrix.accept(response.status, body, response.completed_at, response_sha, self.elapsed())
        except BaseException as error:
            if self.matrix.pending is not None or request.login or request.route.endswith("/PlaybackInfo") or self.matrix.failure is None or request.cleanup:
                self.unresolved_responses.append({"label": request.label, "actor": request.actor, "privateWireSha256": response_sha})
                retained_delete_failure = request.method == "DELETE" and self.matrix.failure is not None and self.matrix.failure.get("responseRetained") is True
                self._failure("acceptance-recovery-required", error, lost=self.matrix.pending is not None and not retained_delete_failure)
            else:
                self.failure = {"kind": "observation-failure", "errorType": type(error).__name__, "message": str(error)}
                self.failure_events.append(self.failure)
                self.persist()
            return
        if request.login:
            self.tokens[request.actor] = body["AccessToken"]
            self.unverified_logins.pop(request.actor, None)
        self.persist()

    def run(self):
        require(not self.finished and not self.blocked, "A completed or blocked runner cannot resume.")
        try:
            while not self.blocked:
                if self.matrix.mode in ("closed", "closed-with-observation-failure"):
                    require(self.matrix.revoked == {"P", "Q"} and not self.unverified_logins,
                            "A closed planner lacks complete owned token rejection proofs.")
                    self.finished = True
                    break
                if self.matrix.mode == "recovery-required" and self.matrix.pending is None:
                    self._begin_cleanup()
                request = self.matrix.prepare_next(self.elapsed())
                if isinstance(request, self.module.WaitRequired):
                    require(0 < request.seconds <= 5, "The requested wait exceeds its bounded lifecycle allowance.")
                    self.sleeper(request.seconds)
                    continue
                if request is None:
                    if self.matrix.mode in ("api-observed", "client-discovery-required"):
                        self._begin_cleanup()
                        continue
                    require(self.matrix.mode in ("closed", "closed-with-observation-failure") and not self.unverified_logins,
                            "An empty queue does not prove a fully closed recorder.")
                    self.finished = True
                    break
                self._dispatch(request)
        except BaseException as error:
            self._failure("runner-stopped", error, lost=self.matrix.pending is not None)
        finally:
            self.authority.close()
        result = {"schemaVersion": 1, "classification": "protocol transport; not client acceptance",
                  "mode": "recovery-required" if self.blocked else self.matrix.mode, "outcome": self.matrix.outcome,
                  "httpAttempts": len(self.attempted), "reservedRequests": self.matrix.count,
                  "chargedResponseBytes": self.charged_bytes, "cleanupComplete": self.finished and self.matrix.revoked == {"P", "Q"},
                  "failure": self.failure, "planSha256": self.matrix.plan_sha256,
                  "sourceSha256": self.execution["sources"]["transport"]["sha256"],
                  "unverifiedLoginActors": sorted(self.unverified_logins), "unresolvedResponses": self.unresolved_responses,
                  "evidenceComplete": self.evidence_complete, "persistenceFailure": self.persistence_failure, "liveAcceptanceClaim": False}
        result["failureEvents"] = list(self.failure_events)
        try:
            self.persist()
            self.journal.save("result.json", sanitized(result, self.secrets), export=True)
        except BaseException as error:
            self.evidence_complete = False
            result.update(evidenceComplete=False, persistenceFailure={"errorType": type(error).__name__, "message": str(error)},
                          mode="recovery-required", cleanupComplete=False)
        finally:
            self.journal.close()
        return result

    def _begin_cleanup(self):
        require(not self.blocked and not self.unverified_logins, "Unknown login ownership requires separate recovery.")
        self.matrix.begin_cleanup()
        self.cleanup_deadline = self.started + self.elapsed() + self.execution["budgets"]["cleanupSeconds"]
        self.persist()


def main():
    print(json.dumps({"status": "blocked", "reason": "No live entry is enabled. A fresh preparation receipt producer and separately reviewed operator are still required."}))
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
