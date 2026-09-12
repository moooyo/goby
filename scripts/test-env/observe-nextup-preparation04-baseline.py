#!/usr/bin/env python3
"""Capture a fresh full baseline after independently sealed preparation04 recovery.

The only POST routes are existing-administrator authentication and exact-token
logout. Pinned preparation04 and separate recovery evidence are read without resuming either.
The resulting private baseline remains subject to independent attestation.
"""

from __future__ import annotations

import sys

if __name__ == "__main__" and (not sys.flags.isolated or not sys.flags.dont_write_bytecode):
    print("Use Python -I -B for the owned observer entry point.", file=sys.stderr)
    raise SystemExit(2)

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
import shlex
import stat
import subprocess
import time
from types import SimpleNamespace
from urllib.parse import urlencode

TRANSPORT_SHA256 = "d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1"
FIELDS = "Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear"
CLIENT, DEVICE_NAME, VERSION = "Goby NextUp Baseline Observer", "Linux Baseline Recorder", "1.0"
CLEANUP_LIMIT = 2
PARENT_SEAL_SHA256 = "33f40ce85fc0934bc88a12c4e34cbf4cd8a8b9a682e446d9b1e1feb85e28841a"
RECOVERY_SHA256 = "0c2380b1fb0ee0f78b2e6fb8ad21995b779cfaad26c9ef318af93215121ce312"
PARENT_SOURCE_SHA256 = "347d71f310182e3258dc2f0b51142dbf31088292ee6ec5d3689621971deb152a"
RECOVERY_SOURCE_SHA256 = "0fba1ac06517aac5a5b3e7977df564839439b1523c7559ab44be18fe408a81c4"
ZERO_USERDATA = {"IsFavorite": False, "PlayCount": 0, "PlaybackPositionTicks": 0, "Played": False}
EPISODES = ("A1", "A2", "A3", "B1", "B2", "B3")


class ObservationError(ValueError):
    """A frozen authority or an actual observation was rejected."""


def require(value, message):
    if not value:
        raise ObservationError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def same(left, right):
    return canonical(left) == canonical(right)


def identifier(value):
    return isinstance(value, str) and re.fullmatch(r"[A-Za-z0-9_-]{1,128}", value) is not None


def sha(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def instant(value):
    require(isinstance(value, str), "A timezone-qualified timestamp is required.")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.utcoffset() is not None, "A timestamp has no timezone.")
    return parsed


def strict_json(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            require(key not in result, "Duplicate JSON keys are forbidden.")
            result[key] = value
        return result
    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs,
                      parse_constant=lambda value: (_ for _ in ()).throw(ObservationError("Nonfinite JSON is forbidden.")))


def absolute(value):
    require(isinstance(value, str) and value.startswith("/") and ".." not in Path(value).parts and
            not any(character in value for character in ("\x00", "\n", "\r")), "A normalized absolute Linux path is required.")
    return Path(value)


def descriptor(value):
    require(isinstance(value, dict) and set(value) == {"path", "sha256"} and sha(value["sha256"]), "An exact source or input descriptor is required.")
    absolute(value["path"])


def file_identity(info):
    return (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode, info.st_nlink,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def protected(path, *, directory=False, private=False):
    path = absolute(str(path))
    for entry in (path, *path.parents):
        info = entry.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == info.st_gid == 0, "Authority paths must be root-owned and contain no symlinks.")
        require(not info.st_mode & 0o022 or (entry != path and info.st_mode & stat.S_ISVTX), "An authority path is writable by another owner.")
        require(stat.S_ISDIR(info.st_mode) if entry != path or directory else stat.S_ISREG(info.st_mode), "An authority path has the wrong type.")
        if entry == path:
            require(directory or info.st_nlink == 1, "An authority file has unexpected hard links.")
            require(not private or not info.st_mode & 0o077, "A private input must be owner-only.")
    return path.lstat()


def read_owned(path, *, private=False, maximum=16 * 1024 * 1024):
    expected = protected(path, private=private)
    require(expected.st_size <= maximum, "An owned input exceeds its byte bound.")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as stream:
        require(file_identity(os.fstat(stream.fileno())) == file_identity(expected), "An input changed while opening.")
        raw = stream.read(maximum + 1)
        require(len(raw) <= maximum and file_identity(os.fstat(stream.fileno())) == file_identity(expected), "An input changed while reading.")
    require(file_identity(Path(path).lstat()) == file_identity(expected), "An input path changed during reading.")
    return raw


def load_transport(source):
    raw = read_owned(source["path"])
    require(digest(raw) == source["sha256"] == TRANSPORT_SHA256, "The reviewed transport source changed.")
    name = "nextup_observer_transport_" + source["sha256"]
    module = importlib.util.module_from_spec(importlib.util.spec_from_file_location(name, source["path"]))
    sys.modules[name] = module
    exec(compile(raw, source["path"], "exec"), module.__dict__)
    return module


def validate_manifest(value):
    require(isinstance(value, dict) and set(value) == {"schemaVersion", "kind", "runId", "server", "endpoint", "process", "lock", "sources", "inputs",
            "scope", "sealedRoots", "forbiddenOriginalRoots", "admin", "preservation", "budgets"}, "The exact dedicated observer manifest is required.")
    value = deepcopy(value)
    require(type(value["schemaVersion"]) is int and value["schemaVersion"] == 1 and value["kind"] == "nextup-preparation04-baseline-observation" and
            identifier(value["runId"]), "The dedicated schema and unique run ID are required.")
    require(isinstance(value["server"], dict) and set(value["server"]) == {"id", "version"} and identifier(value["server"]["id"]) and
            isinstance(value["server"]["version"], str) and value["server"]["version"], "A public server binding is required.")
    require(value["endpoint"] == {"scheme": "http", "host": "127.0.0.1", "port": 18197}, "Only the unchanged existing proxy on port 18197 is allowed.")
    process = value["process"]
    require(isinstance(process, dict) and set(process) == {"application", "endpoint", "workerNetworkNamespace"}, "Both metadata-only process identities are required.")
    fields = {"pid", "startTicks", "bootId", "uid", "exe", "exeDevice", "exeInode", "cmdline", "networkNamespace", "cgroup"}
    for role in ("application", "endpoint"):
        row = process[role]
        require(isinstance(row, dict) and set(row) == fields | ({"listener"} if role == "endpoint" else set()) and
                type(row["pid"]) is int and row["pid"] > 1 and type(row["uid"]) is int and row["uid"] == 0 and
                isinstance(row["startTicks"], str) and row["startTicks"].isdigit() and type(row["exeDevice"]) is int and
                type(row["exeInode"]) is int and row["exeInode"] > 0 and isinstance(row["cmdline"], list) and row["cmdline"] and
                all(isinstance(part, str) for part in row["cmdline"]), "A complete root process metadata identity is required.")
        absolute(row["exe"])
    require(process["application"]["pid"] != process["endpoint"]["pid"] and
            process["application"]["networkNamespace"] != process["endpoint"]["networkNamespace"] and
            process["workerNetworkNamespace"] == process["endpoint"]["networkNamespace"], "The original process namespace must remain separate.")
    listener = process["endpoint"]["listener"]
    require(isinstance(listener, dict) and set(listener) == {"port", "socketInode"} and listener["port"] == 18197 and
            isinstance(listener["socketInode"], str) and re.fullmatch(r"[1-9][0-9]*", listener["socketInode"]), "The existing listener must be pinned.")
    lock = value["lock"]
    require(isinstance(lock, dict) and set(lock) == {"path", "device", "inode"} and
            all(type(lock[key]) is int and lock[key] > 0 for key in ("device", "inode")), "The existing lock identity is required.")
    require(isinstance(value["sources"], dict) and set(value["sources"]) == {"observer", "transport", "proxy"} and
            isinstance(value["inputs"], dict) and set(value["inputs"]) == {"credentials", "parentSeal", "recovery"}, "The exact source and evidence set is required.")
    for row in (*value["sources"].values(), *value["inputs"].values()): descriptor(row)
    require(Path(value["sources"]["observer"]["path"]).name == "observe-nextup-preparation04-baseline.py" and
            Path(value["sources"]["transport"]["path"]).name == "nextup-global-transport.py" and
            value["sources"]["transport"]["sha256"] == TRANSPORT_SHA256, "The dedicated observer and frozen transport must be explicit.")
    require(value["inputs"]["parentSeal"]["sha256"] == PARENT_SEAL_SHA256 and value["inputs"]["recovery"]["sha256"] == RECOVERY_SHA256,
            "Only the independently accepted preparation04 and separate recovery may anchor this capture.")
    require(isinstance(value["scope"], dict) and set(value["scope"]) == {"fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot", "outputRoot"}, "All owned roots must be explicit.")
    for entry in value["scope"].values(): absolute(entry)
    output, fixture = Path(value["scope"]["outputRoot"]), Path(value["scope"]["fixtureRoot"])
    require(output.parent == fixture and fixture in absolute(lock["path"]).parents, "The new output and old lock escaped the fixture root.")
    for name in ("sealedRoots", "forbiddenOriginalRoots"):
        rows = value[name]
        require(isinstance(rows, list) and rows and len(set(rows)) == len(rows), "Explicit unique authority roots are required.")
        for entry in rows:
            root = absolute(entry)
            require(output != root and root not in output.parents and output not in root.parents, "The new output overlaps an excluded scope.")
    forbidden = list(map(Path, value["forbiddenOriginalRoots"]))
    require(any(Path(process["application"]["exe"]) == root or root in Path(process["application"]["exe"]).parents for root in forbidden), "The original executable is not excluded.")
    command = process["application"]["cmdline"]
    require(command.count("-programdata") == 1 and command.index("-programdata") + 1 < len(command), "The original data-root argument is required.")
    data = absolute(command[command.index("-programdata") + 1])
    require(any(data == root or root in data.parents for root in forbidden), "The original database root is not excluded.")
    for name, row in value["sources"].items():
        require(Path(value["scope"]["proxySourceRoot" if name == "proxy" else "sourceRoot"]) in Path(row["path"]).parents, "A source escaped its declared owned root.")
    allowed = [Path(value["scope"]["inputRoot"]), *map(Path, value["sealedRoots"])]
    for row in value["inputs"].values():
        require(any(root in Path(row["path"]).parents for root in allowed), "An input escaped its owned or sealed root.")
    for path in [*value["scope"].values(), lock["path"], *(row["path"] for row in (*value["sources"].values(), *value["inputs"].values()))]:
        require(all(Path(path) != root and root not in Path(path).parents for root in forbidden), "A readable path entered original implementation or database authority.")
    admin = value["admin"]
    require(isinstance(admin, dict) and set(admin) == {"userId", "username", "credentialRef", "deviceId"} and
            all(identifier(admin[key]) for key in ("userId", "credentialRef", "deviceId")) and isinstance(admin["username"], str) and admin["username"], "One existing administrator and a new device are required.")
    scope = value["preservation"]
    require(isinstance(scope, dict) and set(scope) == {"userIds", "libraryIds", "detailRoutes"}, "The complete actual snapshot scope is required.")
    for name in ("userIds", "libraryIds"):
        require(isinstance(scope[name], list) and 1 <= len(scope[name]) <= 16 and len(set(scope[name])) == len(scope[name]) and all(identifier(key) for key in scope[name]), "The population must be explicit, unique, and bounded.")
    require(admin["userId"] in scope["userIds"] and isinstance(scope["detailRoutes"], list) and 1 <= len(scope["detailRoutes"]) <= 64, "The administrator and complete detail set are required.")
    for row in scope["detailRoutes"]:
        require(isinstance(row, dict) and set(row) == {"group", "userId", "itemId"} and identifier(row["group"]) and
                row["userId"] in scope["userIds"] and identifier(row["itemId"]), "A detail route escaped its declared subject scope.")
    require(len({(row["group"], row["itemId"]) for row in scope["detailRoutes"]}) == len(scope["detailRoutes"]) and
            len({(row["userId"], row["itemId"]) for row in scope["detailRoutes"]}) == len(scope["detailRoutes"]), "Detail routes may not duplicate either output or wire identities.")
    limits = {"requestSeconds": (1, 15), "normalSeconds": (1, 600), "cleanupSeconds": (1, 120), "requestBytes": (1024, 32768),
              "responseBytes": (1024, 1048576), "totalResponseBytes": (4096, 160 * 1024 * 1024), "cleanupResponseBytes": (1024, 3 * 1024 * 1024)}
    budgets = value["budgets"]
    normal = 6 + len(scope["libraryIds"]) + 2 * len(scope["userIds"]) + len(scope["detailRoutes"])
    require(isinstance(budgets, dict) and set(budgets) == set(limits) and all(type(budgets[key]) is int and low <= budgets[key] <= high for key, (low, high) in limits.items()) and
            budgets["cleanupResponseBytes"] >= 2 * (budgets["responseBytes"] + 1) and
            budgets["totalResponseBytes"] >= normal * (budgets["responseBytes"] + 1) + budgets["cleanupResponseBytes"], "The finite aggregate budget must cover every bounded request and its cleanup reserve.")
    return value


def page(value, key="Id", maximum=256, *, devices=False):
    require(isinstance(value, dict) and isinstance(value.get("Items"), list) and type(value.get("TotalRecordCount")) is int,
            "An actual complete list envelope is required.")
    rows = value["Items"]
    require(len(rows) <= maximum and value["TotalRecordCount"] in ({0, len(rows)} if devices else {len(rows)}), "The list count is incomplete or outside its bound.")
    require(all(isinstance(row, dict) and identifier(row.get(key)) for row in rows) and len({row[key] for row in rows}) == len(rows),
            "List item identities must be unique and complete.")
    if devices:
        require(all(identifier(row.get("ReportedDeviceId")) for row in rows) and len({row["ReportedDeviceId"] for row in rows}) == len(rows),
                "Device rows require unique stable and reported identities.")
    return {row[key]: row for row in rows}


def validate_baseline(value, manifest, *, fresh=False):
    keys = {"marker", "version", "captured_at", "server", "roster", "configuration", "libraries", "catalog_by_library", "items_by_user", "preferences", "details", "devices", "credential_context"}
    require(isinstance(value, dict) and set(value) == keys and isinstance(value["marker"], str) and 0 < len(value["marker"]) <= 128 and
            type(value["version"]) is int and value["version"] == 1, "The complete retained baseline schema is required.")
    instant(value["captured_at"])
    require(all(isinstance(value[key], dict) for key in keys - {"marker", "version", "captured_at"}), "A full baseline document is missing.")
    users, libraries = set(manifest["preservation"]["userIds"]), set(manifest["preservation"]["libraryIds"])
    require(set(value["roster"]) == set(value["items_by_user"]) == set(value["preferences"]) == users and
            set(value["libraries"]) == set(value["catalog_by_library"]) == libraries, "The retained baseline has another population.")
    require(value["server"].get("Id") == manifest["server"]["id"] and value["server"].get("Version") == manifest["server"]["version"], "The retained server differs.")
    require(all(isinstance(row, dict) and row.get("Id") == user and isinstance(row.get("Name"), str) and isinstance(row.get("Policy"), dict) and
                isinstance(row.get("Configuration"), dict) for user, row in value["roster"].items()), "A retained user profile is incomplete.")
    require(all(isinstance(row, dict) and row.get("ItemId") == library and isinstance(row.get("Name"), str) and isinstance(row.get("Locations"), list) and
                isinstance(row.get("LibraryOptions"), dict) for library, row in value["libraries"].items()), "A retained library definition is incomplete.")
    catalog = set()
    for rows in value["catalog_by_library"].values():
        require(isinstance(rows, dict) and all(identifier(key) and isinstance(row, dict) and row.get("Id") == key for key, row in rows.items()), "A retained catalog is malformed.")
        catalog.update(rows)
    require(0 < len(catalog) <= 256, "The retained catalog exceeds its bound.")
    require(all(isinstance(rows, dict) and set(rows) <= catalog and all(isinstance(row, dict) and row.get("Id") == key for key, row in rows.items())
                for rows in value["items_by_user"].values()), "A retained subject projection escaped its catalog.")
    expected = {(row["group"], row["itemId"]) for row in manifest["preservation"]["detailRoutes"]}
    require(all(isinstance(rows, dict) for rows in value["details"].values()) and
            {(group, item) for group, rows in value["details"].items() for item in rows} == expected and
            all(isinstance(row, dict) and row.get("Id") == item for rows in value["details"].values() for item, row in rows.items()), "A full detail witness is missing.")
    devices = page({"Items": list(value["devices"].values()), "TotalRecordCount": len(value["devices"])}, devices=True)
    require(set(devices) == set(value["devices"]), "A retained device dictionary key differs.")
    admin, context = manifest["admin"], value["credential_context"]
    require(context == {"channel": "controller_api", "authenticated_user_id": admin["userId"], "token_sha256": context.get("token_sha256"),
                        "user_id_semantics": "subject_projection"} and sha(context.get("token_sha256")), "The actual administrator token attribution is missing.")
    require(value["roster"][admin["userId"]]["Name"] == admin["username"] and
            (fresh or admin["deviceId"] not in {row["ReportedDeviceId"] for row in devices.values()}), "The existing administrator or fresh observer device is unproven.")
    return value


def query(user, *, parent=None, ids=None):
    values = {"Recursive": "true", "Fields": FIELDS, "EnableUserData": "true", "EnableTotalRecordCount": "true", "Limit": "256"}
    if parent is not None: values["ParentId"] = parent
    if ids is not None: values["Ids"] = ",".join(sorted(ids))
    return "/emby/Users/" + user + "/Items?" + urlencode(values)


def read_plan(manifest, baseline):
    admin = manifest["admin"]["userId"]
    ids = {key for rows in baseline["catalog_by_library"].values() for key in rows}
    rows = [("server", "/emby/System/Info/Public"), ("users", "/emby/Users"), ("libraries", "/emby/Library/VirtualFolders/Query")]
    rows += [("catalog-" + str(index), query(admin, parent=library)) for index, library in enumerate(sorted(manifest["preservation"]["libraryIds"]))]
    for index, user in enumerate(sorted(manifest["preservation"]["userIds"])):
        rows += [("items-" + str(index), query(user, ids=ids)), ("prefs-" + str(index), "/emby/UserSettings/" + user)]
    rows += [("detail-" + str(index), "/emby/Users/" + row["userId"] + "/Items/" + row["itemId"]) for index, row in enumerate(manifest["preservation"]["detailRoutes"])]
    rows += [("devices", "/emby/Devices"), ("configuration", "/emby/System/Configuration")]
    require(len(rows) == 5 + len(manifest["preservation"]["libraryIds"]) + 2 * len(manifest["preservation"]["userIds"]) + len(manifest["preservation"]["detailRoutes"]) and len(set(rows)) == len(rows), "The complete read plan must match its derived unique route count.")
    return rows


def frozen_plan(manifest, baseline):
    return {"schemaVersion": 1, "runId": manifest["runId"], "manifestSha256": digest(canonical(manifest).encode()),
            "requests": [{"label": "login", "method": "POST", "route": "/emby/Users/AuthenticateByName"}] +
                [{"label": label, "method": "GET", "route": route} for label, route in read_plan(manifest, baseline)] +
                [{"label": "logout", "method": "POST", "route": "/emby/Sessions/Logout"}, {"label": "rejection", "method": "GET", "route": "/emby/Sessions"}],
            "normalLimit": 1 + len(read_plan(manifest, baseline)), "cleanupLimit": CLEANUP_LIMIT, "totalLimit": 3 + len(read_plan(manifest, baseline)),
            "createsMediaLibrariesUsersOrPlayback": False, "automaticRetryOrResume": False, "independentAttestationRequired": True}


def decode_wire(wire):
    require(isinstance(wire, dict) and wire.get("completeHttp") is True and wire.get("failure") is None and wire.get("retainedRawTruncated") is False and
            type(wire.get("status")) is int and 100 <= wire["status"] <= 599 and isinstance(wire.get("headers"), list), "The retained wire response is incomplete.")
    raw = base64.b64decode(wire["rawBase64"], validate=True)
    require(len(raw) == wire.get("observedRawBytes") and len(raw) <= 1048576, "The retained raw wire length differs.")
    require(wire["status"] != 204 or not raw, "An HTTP 204 response must have zero actual body bytes.")
    instant(wire["completedAt"])
    require(all(isinstance(pair, list) and len(pair) == 2 and all(isinstance(part, str) and "\r" not in part and "\n" not in part for part in pair)
                for pair in wire["headers"]), "Actual response headers must be complete ordered pairs.")
    require(len(wire["headers"]) <= 100 and sum(len((key + ": " + value + "\r\n").encode("utf-8")) for key, value in wire["headers"]) <= 32768,
            "The actual response headers exceed their capture bound.")
    lengths = [value for key, value in wire["headers"] if key.lower() == "content-length"]
    transfers = [value for key, value in wire["headers"] if key.lower() == "transfer-encoding"]
    content_types = [value for key, value in wire["headers"] if key.lower() == "content-type"]
    require(not lengths or len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(raw) and not transfers, "The retained HTTP framing differs.")
    require(len(set(content_types)) <= 1, "The actual response Content-Type values conflict.")
    if content_types and "charset=" in content_types[0].lower():
        charset = content_types[0].lower().split("charset=", 1)[1].split(";", 1)[0].strip().strip('"')
        require(charset in ("utf-8", "utf8"), "The actual response charset is not UTF-8.")
    if not raw: return None
    try:
        return strict_json(raw)
    except ValueError:
        require(wire["status"] == 401 and not any("json" in value.lower() for value in content_types),
                "Only a non-JSON 401 response may contain UTF-8 plaintext.")
        return raw.decode("utf-8", errors="strict")


def verify_login(body, *, admin, server_id, client, device_name):
    require(isinstance(body, dict) and isinstance(body.get("User"), dict) and isinstance(body.get("SessionInfo"), dict) and
            isinstance(body["User"].get("Policy"), dict), "A login lacks complete principal/policy/session shapes.")
    user, session = body["User"], body["SessionInfo"]
    require(body.get("ServerId") == server_id and isinstance(body.get("AccessToken"), str) and body["AccessToken"] and
            user.get("Id") == admin["userId"] and user.get("Name") == admin["username"] and user["Policy"].get("IsAdministrator") is True and
            session.get("ServerId") == server_id and session.get("UserId") == admin["userId"] and session.get("UserName") == admin["username"] and
            session.get("DeviceId") == admin["deviceId"] and identifier(session.get("Id")) and
            type(session.get("InternalDeviceId")) is int and session["InternalDeviceId"] > 0,
            "The login acknowledged another administrator, server, device, or session.")
    for key, expected in (("Client", client), ("DeviceName", device_name), ("ApplicationVersion", VERSION)):
        require(session.get(key) == expected, "The acknowledged login client metadata differs.")
    return body["AccessToken"], session["Id"]


def owned_device(row, admin, client, device_name):
    require(isinstance(row, dict) and row.get("ReportedDeviceId") == admin["deviceId"] and row.get("LastUserId") == admin["userId"] and
            row.get("LastUserName") == admin["username"] and row.get("AppName") == client and row.get("AppVersion") == VERSION and row.get("Name") == device_name,
            "An actual device row is not owned by the acknowledged administrator/client.")
    instant(row.get("DateLastActivity"))


def precise_timestamp(text):
    """Return exact 100ns UTC ticks and the retained fractional spelling."""
    require(isinstance(text, str), "An exact timestamp string is required.")
    match = re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.(\d{1,7}))?(?:Z|[+-]\d{2}:\d{2})", text)
    require(match is not None, "An authority timestamp must retain its exact supported precision.")
    fraction = match.group(1) or ""
    delta = instant(text).astimezone(timezone.utc) - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return (delta.days * 86400 + delta.seconds) * 10_000_000 + delta.microseconds * 10 + int((fraction + "0000000")[6]), fraction


def device_time_within(value, request_at, completed_at):
    """Allow only the Devices DTO's observed whole-second quantization."""
    observed, fraction = precise_timestamp(value)
    lower, _ = precise_timestamp(request_at)
    upper, _ = precise_timestamp(completed_at)
    if not fraction or set(fraction) <= {"0"}:
        lower = lower // 10_000_000 * 10_000_000
    require(lower <= observed <= upper, "Device activity escaped its actual request interval and observed second precision.")


def preserve_closed_devices(before, after, windows):
    """Retain complete device rows with only proven closed-token date updates."""
    require(set(before) <= set(after), "A previously observed device disappeared.")
    changes = []
    for key, old in before.items():
        current, window = after[key], windows.get(key)
        if window is None:
            require(same(old, current), "An unowned existing device changed.")
            continue
        require(same({name: value for name, value in old.items() if name != "DateLastActivity"},
                     {name: value for name, value in current.items() if name != "DateLastActivity"}) and
                current.get("LastUserId") == window["userId"] and current.get("ReportedDeviceId") == window["reportedDeviceId"],
                "A prior closed device changed structure or owner.")
        if old.get("DateLastActivity") != current.get("DateLastActivity"):
            require(precise_timestamp(current["DateLastActivity"])[0] >= precise_timestamp(old["DateLastActivity"])[0], "A closed device activity timestamp moved backwards.")
            device_time_within(current["DateLastActivity"], window["from"], window["through"])
            changes.append({"kind": "prior-closed-device-time", "deviceId": key})
    return changes


def validate_closed_unit(unit, *, failed):
    require(isinstance(unit, dict) and set(unit) == {"name", "invocationId", "properties", "cgroupPath"} and
            isinstance(unit["name"], str) and re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", unit["name"]) and
            isinstance(unit["invocationId"], str) and re.fullmatch(r"[0-9a-f]{32}", unit["invocationId"]) and isinstance(unit["properties"], dict),
            "A closed observer/recovery requires its exact concrete unit record.")
    properties = unit["properties"]
    require({"InvocationID", "MainPID", "ActiveState", "SubState", "Result", "ExecMainStatus", "ControlGroup"} <= set(properties) and
            all(isinstance(key, str) and isinstance(value, str) for key, value in properties.items()),
            "Closed units must explicitly include actual ControlGroup and terminal properties.")
    state = (properties.get("ActiveState"), properties.get("SubState"))
    require(properties.get("InvocationID") == unit["invocationId"] and properties.get("MainPID") == "0" and
            properties.get("Result") == ("exit-code" if failed else "success") and properties.get("ExecMainStatus") == ("2" if failed else "0") and
            (state == ("failed", "failed") if failed else state in (("active", "exited"), ("inactive", "dead"))) and
            properties["ControlGroup"] in ("", "/system.slice/" + unit["name"]) and
            unit["cgroupPath"] == "/sys/fs/cgroup/system.slice/" + unit["name"], "A closed observer/recovery unit has another invocation or terminal state.")
    return deepcopy(unit)


class EvidenceReader:
    """Read only descriptor-bound owned bytes, with no writes or runtime probes."""

    def __init__(self, manifest, read_bytes, *, completed=False, extra_roots=()):
        self.manifest, self.read_bytes, self.completed = manifest, read_bytes, completed
        self.extra_roots = tuple(map(Path, extra_roots))
        self.pins = {}

    def raw(self, row):
        descriptor(row)
        path = Path(row["path"])
        roots = [Path(self.manifest["scope"][key]) for key in ("inputRoot", "sourceRoot", "proxySourceRoot")]
        roots += list(map(Path, self.manifest["sealedRoots"]))
        if self.completed: roots += [Path(self.manifest["scope"]["outputRoot"]), *self.extra_roots]
        require(any(root in path.parents for root in roots) and all(Path(root) != path and Path(root) not in path.parents for root in self.manifest["forbiddenOriginalRoots"]),
                "A proof descriptor escaped its explicit owned roots.")
        raw = self.read_bytes(deepcopy(row))
        require(isinstance(raw, bytes) and len(raw) <= 16 * 1024 * 1024 and digest(raw) == row["sha256"], "A descriptor's exact bounded bytes differ.")
        previous = self.pins.setdefault(str(path), row["sha256"])
        require(previous == row["sha256"], "One path has conflicting evidence pins.")
        return raw

    def record(self, row):
        require(Path(row["path"]).suffix == ".json", "A JSON proof requires an explicit JSON path.")
        return strict_json(self.raw(row))


def request_headers(request):
    pairs = request.get("headers")
    require(isinstance(pairs, list) and all(isinstance(pair, list) and len(pair) == 2 and all(isinstance(part, str) for part in pair) for pair in pairs), "The exact ordered request headers are missing.")
    require(len({pair[0].lower() for pair in pairs}) == len(pairs), "Duplicate request header names are forbidden.")
    return {key.lower(): value for key, value in pairs}


def read_ledger(reader, index_descriptor, count, *, reserved):
    index = reader.record(index_descriptor)
    require(index.get("schemaVersion") == 1 and isinstance(index.get("requests"), list) and len(index["requests"]) == count, "The complete bounded wire index is required.")
    events, previous_time = {}, None
    for ordinal, row in enumerate(index["requests"], 1):
        require(isinstance(row, dict) and {"ordinal", "label", "intent", "response"} <= set(row) and row["ordinal"] == ordinal and
                identifier(row["label"]) and row["label"] not in events, "The wire index was reordered, duplicated, or relabelled.")
        intent, wire = reader.record(row["intent"]), reader.record(row["response"])
        require(intent.get("ordinal") == wire.get("ordinal") == ordinal and intent.get("label") == wire.get("label") == row["label"] and
                same(intent.get("request"), wire.get("request")), "The original intent and response request differ.")
        headers = request_headers(intent["request"])
        token = headers.get("x-emby-token")
        require(intent.get("tokenSha256") == (digest(token.encode()) if token is not None else None), "The intent token attribution differs from its actual request.")
        if "payloadBase64" in wire: require(wire["payloadBase64"] == intent.get("payloadBase64"), "The actual request payload attribution differs.")
        if reserved:
            reservation = reader.record(row["reserved"])
            pending = reservation.get("pending")
            require(isinstance(pending, dict) and pending.get("ordinal") == ordinal and pending.get("label") == row["label"] and
                    pending.get("intentSha256") == row["intent"]["sha256"] and same(pending.get("request"), intent["request"]) and
                    reservation.get("requestCount") == ordinal, "The durable reservation does not bind its exact request.")
        body = decode_wire(wire)
        completed = precise_timestamp(wire["completedAt"])[0]
        require(previous_time is None or completed >= previous_time, "The ordered response times moved backwards.")
        if "createdAt" in intent: require(precise_timestamp(intent["createdAt"])[0] <= completed, "A response precedes its actual request.")
        previous_time = completed
        events[row["label"]] = {"row": row, "intent": intent, "wire": wire, "body": body}
    return index, events


def expected_step(event, method, route, status, *, token=None):
    request = event["intent"]["request"]
    require(request.get("method") == method and request.get("route") == route and event["wire"]["status"] == status,
            "A retained request has another method, route, or acknowledged status.")
    require(request_headers(request).get("x-emby-token") == token, "A retained request used another exact token.")
    if method != "POST" or route != "/emby/Users/AuthenticateByName":
        require(request.get("body") is None and event["intent"].get("payloadBase64") is None, "A retained read or closure has an unexpected body.")


def actor_login(event, actor, server, *, administrator):
    expected_step(event, "POST", "/emby/Users/AuthenticateByName", 200)
    body, headers = event["body"], request_headers(event["intent"]["request"])
    require(isinstance(body, dict) and isinstance(body.get("User"), dict) and isinstance(body.get("SessionInfo"), dict) and isinstance(body.get("AccessToken"), str) and body["AccessToken"], "A retained login has no complete principal and session.")
    metadata = re.fullmatch(r'Emby Client="([^"\r\n]+)", Device="([^"\r\n]+)", DeviceId="([^"\r\n]+)", Version="([^"\r\n]+)"', headers.get("authorization", ""))
    require(metadata is not None, "The original login client metadata is not exact.")
    client, device_name, device_id, version = metadata.groups()
    user, session = body["User"], body["SessionInfo"]
    require(body.get("ServerId") == server["id"] and user.get("Id") == actor["userId"] and user.get("Name") == actor["username"] and
            isinstance(user.get("Policy"), dict) and user["Policy"].get("IsAdministrator") is administrator and
            session.get("ServerId") == server["id"] and session.get("UserId") == actor["userId"] and session.get("UserName") == actor["username"] and
            session.get("DeviceId") == actor["deviceId"] == device_id and identifier(session.get("Id")) and
            type(session.get("InternalDeviceId")) is int and session["InternalDeviceId"] > 0 and
            (session.get("Client"), session.get("DeviceName"), session.get("ApplicationVersion")) == (client, device_name, version),
            "The acknowledged historical user, policy, server, device, or client differs.")
    return {"actor": deepcopy(actor), "token": body["AccessToken"], "session": session["Id"], "deviceId": str(session["InternalDeviceId"]),
            "client": client, "deviceName": device_name, "version": version, "authorization": headers["authorization"], "login": event}


def exact_closure(login, logout, rejection):
    for event, method, route, status in ((logout, "POST", "/emby/Sessions/Logout", 204), (rejection, "GET", "/emby/Sessions", 401)):
        expected_step(event, method, route, status, token=login["token"])
        require(request_headers(event["intent"]["request"])["authorization"] == login["authorization"], "A closure changed the original authentication metadata.")
    require(logout["body"] is None and precise_timestamp(login["login"]["wire"]["completedAt"])[0] <= precise_timestamp(logout["wire"]["completedAt"])[0] <=
            precise_timestamp(rejection["wire"]["completedAt"])[0], "The exact token closure is out of order or has a nonempty 204.")
    login["through"] = rejection["wire"]["completedAt"]
    return login


def match_device_login(row, login):
    actor = login["actor"]
    require(row.get("Id") == login["deviceId"] and row.get("ReportedDeviceId") == actor["deviceId"] and row.get("LastUserId") == actor["userId"] and
            row.get("LastUserName") == actor["username"] and (row.get("AppName"), row.get("Name"), row.get("AppVersion")) ==
            (login["client"], login["deviceName"], login["version"]), "The complete device row does not belong to its actual acknowledged login.")


def full_episode_fact(value, item):
    require(isinstance(value, dict) and value.get("Id") == item["id"] and value.get("Type") == item["type"] == "Episode" and
            value.get("ParentId") == item["parentId"] and value.get("SeasonId") == item["parentId"] and value.get("SeriesId") == item["seriesId"] and
            type(value.get("IndexNumber")) is int and value["IndexNumber"] == item["indexNumber"] and
            type(value.get("ParentIndexNumber")) is int and value["ParentIndexNumber"] == item["parentIndexNumber"] and
            type(value.get("RunTimeTicks")) is int and value["RunTimeTicks"] == item["runtimeTicks"] > 0 and
            same(value.get("UserData"), ZERO_USERDATA), "A required full episode lost identity, runtime, relations, or exact four-key zero UserData.")


class InputEvidence:
    """Interpret the fixed accepted parent and recovery using their actual raw bytes."""

    def __init__(self, manifest, reader):
        self.manifest, self.reader = manifest, reader
        self.seal = seal = reader.record(manifest["inputs"]["parentSeal"])
        self.recovery = recovery = reader.record(manifest["inputs"]["recovery"])
        require(seal.get("kind") == "nextup-global-failed-preparation-terminal" and seal.get("status") == "failed_preparation_independently_sealed_with_separate_recovery" and
                same(seal.get("independentRecovery"), manifest["inputs"]["recovery"]) and seal.get("source", {}).get("sha256") == PARENT_SOURCE_SHA256 and
                (seal.get("requestCount"), seal.get("normalRequestCount"), seal.get("cleanupRequestCount")) == (172, 115, 57) and
                seal.get("allProducerBytesMatched") is True and seal.get("allRequestAndStateBytesMatched") is True and seal.get("normalizationApplied") is False and
                seal.get("allKnownSessionsClosed") is True and seal.get("closedActors") == ["P", "Q", "admin"] and seal.get("parentDeleteRequests") == 0 and
                seal.get("calibrationsCompleted") == 0 and seal.get("parentStatusUnchanged") == "recovery_required", "The exact accepted failed-parent seal is required.")
        require(recovery.get("kind") == "nextup-preparation-userdata-recovery-independent-terminal" and recovery.get("status") == "owned_userdata_restored_and_exact_token_closed" and
                recovery.get("source", {}).get("sha256") == RECOVERY_SOURCE_SHA256 and (recovery.get("requestCount"), recovery.get("normalRequestCount"), recovery.get("cleanupRequestCount")) == (6, 4, 2) and
                recovery.get("deleteRequests") == 1 and recovery.get("exactFullZeroUserDataRestored") is True and recovery.get("exactTokenClosed") is True and
                same(recovery.get("parentState"), seal["state"]) and same(recovery.get("parentTerminal"), seal["producerTerminal"]), "The independently accepted separate recovery is required.")
        for document, names in ((seal, ("source", "scopeInventory", "exactReplayComparison", "beforePreservation", "afterPreservation", "parentBeforePreservation", "parentAfterPreservation")),
                                (recovery, ("source", "scopeInventory", "preservationBefore", "preservationAfter", "workerState", "workerTerminal"))):
            for name in names: reader.raw(document[name])
        self.parent = parent = reader.record(seal["manifest"])
        self.parent_state = state = reader.record(seal["state"])
        terminal = reader.record(seal["producerTerminal"])
        require(terminal.get("status") == "recovery_required" and (state.get("requestCount"), state.get("normalRequestCount"), state.get("cleanupRequestCount")) == (172, 115, 57) and
                state.get("pending") is None and state.get("ownershipPending") is None and state.get("uncertain") is False and state.get("calibrations") == [] and
                same(parent["server"], manifest["server"]) and same(parent["process"], manifest["process"]) and same(parent["lock"], manifest["lock"]) and
                same(parent["sources"]["proxy"], manifest["sources"]["proxy"]), "The retained parent state or existing endpoint authority differs.")
        reconstruction = reader.record(seal["publicReconstruction"])
        require(reconstruction.get("allProducerBytesMatched") is True and reconstruction.get("allRequestAndStateBytesMatched") is True and
                reconstruction.get("normalizationApplied") is False and same(reconstruction.get("wireIndex"), seal["wireIndex"]) and
                same(reconstruction.get("exactComparison"), seal["exactReplayComparison"]), "The parent complete reconstruction is not exact.")
        self.baseline_descriptor = reconstruction["cleanupPublic"]
        self.baseline = baseline = reader.record(self.baseline_descriptor)
        self.old_routes = deepcopy(parent["preservation"]["detailRoutes"])
        expected_routes = self.old_routes + [{"group": "preparation04-" + actor, "userId": state["userIds"][actor], "itemId": state["items"][symbol]["id"]} for actor in ("P", "Q") for symbol in EPISODES]
        require(manifest["preservation"] == {"userIds": sorted(baseline["roster"]), "libraryIds": sorted(baseline["libraries"]), "detailRoutes": expected_routes},
                "The fresh scope must preserve all old full details and add all twelve actual P/Q episodes in their explicit order.")
        old_manifest = deepcopy(manifest); old_manifest["preservation"]["detailRoutes"] = self.old_routes
        validate_baseline(baseline, old_manifest)
        require(set(baseline["roster"]) == set(parent["preservation"]["userIds"]) | set(state["userIds"].values()) and
                set(baseline["libraries"]) == set(parent["preservation"]["libraryIds"]) | {row["libraryId"] for row in state["libraries"].values()}, "The retained population differs from the acknowledged creations.")
        _, events = read_ledger(reader, seal["wireIndex"], 172, reserved=True)
        self.parent_events = events
        self.logins, self.closed_device_windows, self.user_windows = {}, {}, {}
        for actor in ("P", "Q", "admin"):
            identity = {**parent["actors"][actor], "userId": state["userIds"][actor]}
            login = actor_login(events["login-" + actor], identity, manifest["server"], administrator=actor == "admin")
            exact_closure(login, events["logout-" + actor], events["invalid-" + actor])
            require(state["tokens"].get(actor) == login["token"] and state["sessions"].get(actor) == login["session"], "The final parent ownership differs from its actual login.")
            login["from"] = events["cleanup-after-devices"]["wire"]["completedAt"]
            match_device_login(baseline["devices"][login["deviceId"]], login)
            self.logins[actor] = login
            self.closed_device_windows[login["deviceId"]] = {"from": login["from"], "through": login["through"], "userId": identity["userId"], "reportedDeviceId": identity["deviceId"]}
            self.user_windows.setdefault(identity["userId"], []).append({"from": baseline["captured_at"], "through": login["through"], "fields": {"LastActivityDate"}})
        # Reconstruct every parent cleanup DTO from its exact administrator wire response.
        for label, route in read_plan(old_manifest, baseline):
            expected_step(events["cleanup-after-" + label], "GET", route, 200, token=self.logins["admin"]["token"])
        rebuilt = snapshot_values(old_manifest, baseline, {label: events["cleanup-after-" + label]["body"] for label, _ in read_plan(old_manifest, baseline)},
                                  baseline["captured_at"], self.logins["admin"]["token"])
        require(same(rebuilt, baseline), "The retained complete parent snapshot differs from its actual raw responses.")
        self.new_details = {}
        for actor in ("P", "Q"):
            for symbol in EPISODES:
                item = state["items"][symbol]
                event = events["baseline-detail-" + actor + "-" + symbol]
                expected_step(event, "GET", "/emby/Users/" + state["userIds"][actor] + "/Items/" + item["id"], 200, token=self.logins[actor]["token"])
                full_episode_fact(event["body"], item)
                require(same(state["baselines"][actor][symbol], ZERO_USERDATA), "An owned detail witness was not an exact full zero baseline.")
                self.new_details[("preparation04-" + actor, item["id"])] = deepcopy(item)
        self._recovery()
        self.closed_token_hashes = {digest(row["token"].encode()) for row in self.logins.values()}
        self.closed_session_ids = {row["session"] for row in self.logins.values()}
        require(len(self.closed_token_hashes) == len(self.closed_session_ids) == 4, "A historical login reused a closed token or session.")
        self.previous_devices = deepcopy(baseline["devices"])
        require(manifest["admin"]["deviceId"] not in {row["ReportedDeviceId"] for row in baseline["devices"].values()} | {self.recovery_login["actor"]["deviceId"]}, "The new observer device is not fresh.")
        self.units = []
        for key, failed in (("unit", True), ("recoveryUnit", False)):
            row = deepcopy(seal[key])
            row.setdefault("invocationId", row["properties"]["InvocationID"])
            self.units.append(validate_closed_unit(row, failed=failed))
        require(all(self.units[1]["properties"].get(key) == value for key, value in recovery["unit"].items()) and
                seal.get("recursiveCgroupsEmpty") is True and seal.get("formerProducerPidAbsent") is True and seal.get("formerRecoveryPidAbsent") is True and
                recovery.get("cgroupEmpty") is True and recovery.get("formerPidAbsent") is True, "The exact parent and recovery terminal invocations are not closed.")
        self.not_before = max((seal["capturedAt"], recovery["createdAt"], self.recovery_login["through"]), key=lambda value: precise_timestamp(value)[0])

    def _recovery(self):
        reader, recovery, state = self.reader, self.recovery, self.parent_state
        manifest = reader.record(recovery["input"])
        require(same(manifest["parent"]["state"], self.seal["state"]) and same(manifest["parent"]["terminal"], self.seal["producerTerminal"]), "The separate recovery names another parent.")
        _, events = read_ledger(reader, recovery["wireIndex"], 6, reserved=False)
        require(list(events) == ["login", "before", "delete", "after", "logout", "rejection"], "The recovery is not the accepted six-request sequence.")
        login = actor_login(events["login"], manifest["actor"], self.manifest["server"], administrator=False)
        require(login["actor"]["userId"] == state["userIds"]["P"] and login["deviceId"] not in self.baseline["devices"] and
                login["actor"]["deviceId"] not in {row["ReportedDeviceId"] for row in self.baseline["devices"].values()}, "The recovery login is not the single new owned P device.")
        item = state["items"]["A1"]
        route = "/emby/Users/" + login["actor"]["userId"]
        for label, method, suffix in (("before", "GET", "/Items/"), ("delete", "DELETE", "/PlayedItems/"), ("after", "GET", "/Items/")):
            expected_step(events[label], method, route + suffix + item["id"], 200, token=login["token"])
            require(request_headers(events[label]["intent"]["request"])["authorization"] == login["authorization"], "A recovery request changed its original client metadata.")
        require(same(events["before"]["body"]["UserData"], self.parent_events["cal-P-partial-before-delete"]["body"]["UserData"]), "Recovery did not start from the exact retained full partial state.")
        full_episode_fact(events["after"]["body"], item)
        require(same(recovery["restorationResponse"], events["after"]["row"]["response"]) and same(recovery["logoutResponse"], events["logout"]["row"]["response"]) and
                same(recovery["rejectionResponse"], events["rejection"]["row"]["response"]), "Independent restoration or closure descriptors differ from their raw records.")
        exact_closure(login, events["logout"], events["rejection"])
        login["from"] = events["login"]["intent"]["createdAt"]
        require(precise_timestamp(login["from"])[0] >= precise_timestamp(self.logins["admin"]["through"])[0], "Recovery precedes the parent session closure.")
        self.recovery_login, self.recovery_events = login, events
        self.logins["recovery"] = login
        self.restored_subject, self.restored_item = login["actor"]["userId"], item["id"]
        self.user_windows.setdefault(self.restored_subject, []).append({"from": login["from"], "through": login["through"], "fields": {"LastLoginDate", "LastActivityDate"}})


def snapshot_values(manifest, baseline, values, captured_at, token):
    users, libraries = sorted(manifest["preservation"]["userIds"]), sorted(manifest["preservation"]["libraryIds"])
    rows = values["users"]
    require(isinstance(rows, list) and len(rows) == len(users) and all(isinstance(row, dict) and identifier(row.get("Id")) for row in rows) and {row["Id"] for row in rows} == set(users), "The full current user roster differs.")
    details = {}
    for index, route in enumerate(manifest["preservation"]["detailRoutes"]): details.setdefault(route["group"], {})[route["itemId"]] = values["detail-" + str(index)]
    result = {"marker": baseline["marker"], "version": 1, "captured_at": captured_at, "server": values["server"], "roster": {row["Id"]: row for row in rows},
              "libraries": page(values["libraries"], "ItemId", 16), "configuration": values["configuration"],
              "catalog_by_library": {key: page(values["catalog-" + str(index)]) for index, key in enumerate(libraries)},
              "items_by_user": {key: page(values["items-" + str(index)]) for index, key in enumerate(users)},
              "preferences": {key: values["prefs-" + str(index)] for index, key in enumerate(users)}, "details": details,
              "devices": page(values["devices"], devices=True), "credential_context": {"channel": "controller_api", "authenticated_user_id": manifest["admin"]["userId"],
              "token_sha256": digest(token.encode()), "user_id_semantics": "subject_projection"}}
    validate_baseline(result, manifest, fresh=True)
    return result


def preserve_snapshot(manifest, authority, after, login, responses):
    before, admin = authority.baseline, manifest["admin"]
    require(precise_timestamp(before["captured_at"])[0] <= precise_timestamp(after["captured_at"])[0], "The fresh baseline timestamp moved backwards.")
    for key in ("server", "configuration", "libraries", "catalog_by_library", "preferences"):
        require(same(before[key], after[key]), "A retained complete public document changed: " + key)
    expected_projections = deepcopy(before["items_by_user"])
    expected_projections[authority.restored_subject][authority.restored_item]["UserData"] = deepcopy(ZERO_USERDATA)
    require(same(expected_projections, after["items_by_user"]), "A subject projection changed outside the separately restored exact zero UserData.")
    for group, rows in before["details"].items():
        require(group in after["details"] and all(same(row, after["details"][group].get(item)) for item, row in rows.items()), "An old complete administrator-subject detail changed.")
    for (group, item), witness in authority.new_details.items(): full_episode_fact(after["details"][group][item], witness)
    windows = deepcopy(authority.user_windows)
    windows.setdefault(admin["userId"], []).append({"from": login["requestAt"], "through": responses["users"]["completedAt"], "fields": {"LastLoginDate", "LastActivityDate"}})
    changes = []
    for user, old in before["roster"].items():
        current = after["roster"][user]
        allowed = set().union(*(row["fields"] for row in windows.get(user, [])))
        require(same({key: value for key, value in old.items() if key not in allowed}, {key: value for key, value in current.items() if key not in allowed}), "An account changed outside proven authentication fields.")
        for field in sorted(allowed):
            if field in old and field in current and old[field] == current[field]: continue
            require(field in current and (field not in old or precise_timestamp(current[field])[0] >= precise_timestamp(old[field])[0]) and
                    any(field in window["fields"] and precise_timestamp(window["from"])[0] <= precise_timestamp(current[field])[0] <= precise_timestamp(window["through"])[0] for window in windows[user]),
                    "An account authentication date escaped its actual acknowledged window.")
            changes.append({"kind": "acknowledged-authentication-date", "userId": user, "field": field})
    changes.extend(preserve_closed_devices(before["devices"], after["devices"], authority.closed_device_windows))
    recovered = authority.recovery_login
    fresh_id = str(login["body"]["SessionInfo"]["InternalDeviceId"])
    require(fresh_id != recovered["deviceId"] and set(after["devices"]) - set(before["devices"]) == {recovered["deviceId"], fresh_id}, "The new full device roster must add exactly the closed recovery and current observer devices.")
    recovery_device = after["devices"][recovered["deviceId"]]
    match_device_login(recovery_device, recovered)
    device_time_within(recovery_device["DateLastActivity"], recovered["from"], recovered["through"])
    device = after["devices"][fresh_id]
    owned_device(device, admin, CLIENT, DEVICE_NAME)
    device_time_within(device["DateLastActivity"], login["requestAt"], responses["devices"]["completedAt"])
    return {"completePublicDocumentsPreserved": True, "oldDetailWitnessesPreserved": len(authority.old_routes), "newFullZeroDetailWitnesses": len(authority.new_details),
            "retainedDeviceCount": len(before["devices"]), "newClosedRecoveryDevices": 1, "newOwnedObserverDevices": 1, "allowedAuthenticationChanges": changes,
            "restoredSubjectProjectionVerified": True, "parentSeal": manifest["inputs"]["parentSeal"], "recovery": manifest["inputs"]["recovery"], "priorBaseline": authority.baseline_descriptor}


def normalized_goby(value):
    require(isinstance(value, dict) and isinstance(value.get("database"), dict) and isinstance(value["database"].get("metadata"), dict) and
            "captured_at" in value["database"]["metadata"], "The complete Goby database snapshot metadata is required.")
    value = deepcopy(value)
    instant(value["database"]["metadata"].pop("captured_at"))
    return value


def verify_preservation_pair(reader, before_descriptor, after_descriptor, *, roots, anchor_descriptor=None):
    before, after = reader.record(before_descriptor), reader.record(after_descriptor)
    for value in (before, after):
        require(isinstance(value, dict) and all(isinstance(value.get(key), dict) and value[key] for key in ("roots", "services", "main_files", "goby_counts")) and
                type(value.get("root_count")) is int and value["root_count"] == len(value["roots"]) and set(roots) <= set(value["roots"]), "The complete preserved root, service, file, and Goby scope is required.")
        instant(value["capturedAt"])
    require(precise_timestamp(before["capturedAt"])[0] <= precise_timestamp(after["capturedAt"])[0] and
            all(same(before[key], after[key]) for key in ("roots", "services", "main_files", "goby_counts")), "The independently captured roots, services, files, or Goby counts changed.")
    goby_before, goby_after = reader.record(before["goby_snapshot"]), reader.record(after["goby_snapshot"])
    require(same(normalized_goby(goby_before), normalized_goby(goby_after)), "The complete Goby state changed outside its capture timestamp.")
    if anchor_descriptor is not None:
        anchor = reader.record(anchor_descriptor)
        require(isinstance(anchor.get("roots"), dict) and set(anchor["roots"]) <= set(before["roots"]) and
                all(same(row, before["roots"][path]) for path, row in anchor["roots"].items()) and
                all(same(anchor[key], before[key]) for key in ("services", "main_files", "goby_counts")) and
                same(normalized_goby(reader.record(anchor["goby_snapshot"])), normalized_goby(goby_before)), "The fresh preservation baseline differs from the accepted parent seal.")
    return before, after


def verify_scope_inventory(reader, descriptor_row, root, generated, final_state):
    inventory = reader.record(descriptor_row)
    require(isinstance(inventory, dict) and set(inventory) == {str(root)} and isinstance(inventory[str(root)], dict), "The completed scope inventory must name exactly this output root.")
    expected = {str(Path(path).relative_to(root)): raw for path, raw in generated.items()}
    expected["private/state.json"] = (canonical(final_state) + "\n").encode()
    entries = inventory[str(root)]
    require(set(entries) == set(expected) | {".", "private", "export"}, "The completed scope contains missing or extra files, requests, or directories.")
    for name, row in entries.items():
        require(isinstance(row, dict) and type(row.get("uid")) is int and type(row.get("gid")) is int and row["uid"] == row["gid"] == 0 and type(row.get("mode")) is int and not row["mode"] & 0o077 and
                type(row.get("device")) is int and row["device"] > 0 and type(row.get("inode")) is int and row["inode"] > 0, "A completed scope entry lost its private root-owned identity.")
        if name in expected:
            require(stat.S_ISREG(row["mode"]) and type(row.get("links")) is int and row["links"] == 1 and row.get("bytes") == len(expected[name]) and row.get("sha256") == digest(expected[name]), "The exact inventory file bytes or regular-file identity differ from replay.")
        else:
            require(stat.S_ISDIR(row["mode"]) and "sha256" not in row, "A completed scope directory was replaced by a file.")


class Authority:
    """Acquire the existing lock and recheck exact owned evidence and process metadata."""

    def __init__(self, manifest, manifest_descriptor):
        self.manifest = validate_manifest(manifest)
        self.frozen = canonical(self.manifest)
        self.lock_fd, self.closed = None, False
        require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
                "Observation requires root SSH and Python -I -B.")
        require(Path(manifest["sources"]["observer"]["path"]) == Path(__file__).absolute(), "The source pin must bind this dedicated observer file.")
        self.reader = EvidenceReader(self.manifest, lambda row: read_owned(row["path"], private=Path(row["path"]).suffix != ".py"))
        require(same(self.reader.record(manifest_descriptor), self.manifest), "The input bytes differ from the frozen manifest.")
        for row in self.manifest["sources"].values(): self.reader.raw(row)
        self.support = load_transport(self.manifest["sources"]["transport"])
        evidence = InputEvidence(self.manifest, self.reader)
        for key, value in vars(evidence).items():
            if key not in {"manifest", "reader"}: setattr(self, key, value)
        record = self.reader.record(self.manifest["inputs"]["credentials"])
        admin = self.manifest["admin"]
        require(isinstance(record, dict) and set(record) == {"schemaVersion", "runId", "admin"} and record["schemaVersion"] == 1 and record["runId"] == self.manifest["runId"] and
                isinstance(record["admin"], dict) and set(record["admin"]) == {"userId", "username", "credentialRef", "password"} and
                all(record["admin"][key] == admin[key] for key in ("userId", "username", "credentialRef")) and
                isinstance(record["admin"]["password"], str) and len(record["admin"]["password"]) >= 32, "The one existing administrator credential input differs.")
        self.credentials, self.pins = record["admin"], self.reader.pins
        self.units_frozen = canonical(self.units)
        for name in ("fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot"):
            protected(self.manifest["scope"][name], directory=True)
        require(not os.path.lexists(self.manifest["scope"]["outputRoot"]), "A consumed output can never resume.")

    def acquire(self):
        require(self.lock_fd is None and not self.closed, "An authority cannot reacquire or resume.")
        row = self.manifest["lock"]
        info = protected(row["path"], private=True)
        require((info.st_dev, info.st_ino) == (row["device"], row["inode"]), "The existing lock changed.")
        self.lock_fd = os.open(row["path"], os.O_RDWR | os.O_NOFOLLOW)
        require((os.fstat(self.lock_fd).st_dev, os.fstat(self.lock_fd).st_ino) == (row["device"], row["inode"]), "The lock changed while opening.")
        fcntl.flock(self.lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.check()

    def check(self):
        require(not self.closed and self.lock_fd is not None and canonical(self.manifest) == self.frozen, "The observer authority is unavailable or changed.")
        require(canonical(self.units) == self.units_frozen, "The complete closed-unit authority changed in memory.")
        lock = self.manifest["lock"]
        info = protected(lock["path"], private=True)
        opened = os.fstat(self.lock_fd)
        require((info.st_dev, info.st_ino) == (opened.st_dev, opened.st_ino) == (lock["device"], lock["inode"]), "The exclusive lock identity changed.")
        for row in self.manifest["sources"].values():
            require(digest(read_owned(row["path"])) == row["sha256"], "An owned source changed during observation.")
        for path, expected in self.pins.items():
            require(digest(read_owned(path, private=Path(path).suffix != ".py")) == expected, "A pinned input or sealed proof changed during observation.")
        require(same(self.support.process_identity(self.manifest["process"]), self.manifest["process"]), "Application, proxy, namespace, or listener metadata changed.")
        for unit in self.units:
            keys = tuple(unit["properties"])
            require(keys and all(re.fullmatch(r"[A-Za-z][A-Za-z0-9]*", key) for key in keys), "Invalid systemd property metadata.")
            result = subprocess.run(["systemctl", "show", unit["name"], *["--property=" + key for key in keys]], check=True, capture_output=True, text=True, timeout=10)
            observed = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
            require(observed == unit["properties"], "A closed predecessor invocation/state changed.")
            former = unit["properties"].get("ExecMainPID", "")
            require(former.isdigit() and int(former) > 1 and not Path("/proc", former).exists(), "A retained parent or recovery process is still present.")
            root = Path(unit["cgroupPath"])
            if root.exists():
                require(all(not entry.read_text().strip() for entry in [root / "cgroup.procs", *root.glob("**/cgroup.procs")]), "A predecessor cgroup is no longer recursively empty.")

    def close(self):
        if self.lock_fd is not None:
            fcntl.flock(self.lock_fd, fcntl.LOCK_UN)
            os.close(self.lock_fd)
            self.lock_fd = None
        self.closed = True


class ObserverRunner:
    """An at-most-once read sequence with durable login responsibility."""

    def __init__(self, manifest, *, authority, transport, journal_factory, monotonic=time.monotonic,
                 utc_now=lambda: datetime.now(timezone.utc).isoformat()):
        self.manifest = validate_manifest(manifest)
        self.frozen, self.authority, self.transport = canonical(self.manifest), authority, transport
        self.support, self.journal_factory, self.monotonic, self.utc_now = authority.support, journal_factory, monotonic, utc_now
        self.plan = frozen_plan(self.manifest, authority.baseline)
        self.plan_sha256 = digest(canonical(self.plan).encode())
        self.reads = read_plan(self.manifest, authority.baseline)
        self.journal = self.started = self.cleanup_started = self.last_clock = self.last_completed = None
        self.count = self.normal_count = self.cleanup_count = self.charged_bytes = self.read_index = 0
        self.pending = self.ownership_pending = self.token = self.session = self.login = self.failure = None
        self.used = self.completed = self.closed_token = self.uncertain = self.journal_failed = False
        self.labels, self.responses, self.outputs = set(), {}, {}

    def _now(self):
        current = self.monotonic()
        require(isinstance(current, (int, float)) and not isinstance(current, bool) and math.isfinite(current) and
                (self.last_clock is None or current >= self.last_clock), "The monotonic clock is nonfinite or moved backwards.")
        self.last_clock = current
        return current

    def _state(self):
        return {"schemaVersion": 1, "runId": self.manifest["runId"], "manifestSha256": digest(self.frozen.encode()), "planSha256": self.plan_sha256,
                "requestCount": self.count, "normalRequestCount": self.normal_count, "cleanupRequestCount": self.cleanup_count,
                "chargedResponseBytes": self.charged_bytes, "readIndex": self.read_index, "pending": self.pending, "ownershipPending": self.ownership_pending,
                "token": self.token, "session": self.session, "closedToken": self.closed_token, "uncertain": self.uncertain, "failure": self.failure}

    def _save(self, name, value, *, export=False):
        try: return self.journal.save(name, value, export=export)
        except BaseException:
            self.journal_failed = True
            self.uncertain = True
            raise

    def _persist(self):
        try: self.journal.state(self._state())
        except BaseException:
            self.journal_failed = True
            self.uncertain = True
            raise

    def _guard(self, label, method, route, body):
        require(label not in self.labels, "A request cannot be dispatched twice.")
        if label == "login":
            require(self.count == 0 and self.token is None and method == "POST" and route == "/emby/Users/AuthenticateByName" and body ==
                    {"Username": self.authority.credentials["username"], "Pw": self.authority.credentials["password"]}, "Login differs from the one frozen existing credential.")
        elif self.cleanup_started is not None:
            expected = [("logout", "POST", "/emby/Sessions/Logout"), ("rejection", "GET", "/emby/Sessions")]
            require(self.token is not None and self.cleanup_count < 2 and (label, method, route) == expected[self.cleanup_count] and body is None,
                    "Cleanup may only logout then reject the exact owned token once.")
            if label == "rejection": require(self.responses["logout"]["status"] == 204, "The rejection proof requires actual logout 204.")
        else:
            require(self.token is not None and self.read_index < len(self.reads) and (label, route) == self.reads[self.read_index] and method == "GET" and body is None,
                    "A read escaped its exact derived ordered read plan.")

    def _dispatch(self, label, method, route, body=None):
        require(self.journal is not None and not self.journal_failed and not self.uncertain and self.pending is None and self.ownership_pending is None and not self.completed,
                "A closed or unresolved observation cannot dispatch again.")
        require(canonical(self.manifest) == self.frozen and digest(canonical(self.plan).encode()) == self.plan_sha256, "The frozen observation plan changed.")
        require(same(self.reads, read_plan(self.manifest, self.authority.baseline)), "The exact retained read sequence changed.")
        self._guard(label, method, route, body)
        cleanup = self.cleanup_started is not None
        require(self.count < self.plan["totalLimit"] and (self.cleanup_count < CLEANUP_LIMIT if cleanup else self.normal_count < self.plan["normalLimit"]), "The exact HTTP budget is exhausted.")
        self.authority.check(); self.journal.check()
        budgets = self.manifest["budgets"]
        deadline = self.cleanup_started + budgets["cleanupSeconds"] if cleanup else self.started + budgets["normalSeconds"]
        require(self._now() < deadline, "The bounded observation deadline expired.")
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="' + CLIENT + '", Device="' + DEVICE_NAME + '", DeviceId="' + self.manifest["admin"]["deviceId"] + '", Version="' + VERSION + '"'}
        if self.token is not None: headers["X-Emby-Token"] = self.token
        payload = None if body is None else urlencode(body).encode("utf-8")
        if body is not None: headers["Content-Type"] = "application/x-www-form-urlencoded; charset=utf-8"
        require(payload is None or len(payload) <= budgets["requestBytes"], "The login payload exceeds its byte bound.")
        require(budgets["totalResponseBytes"] - self.charged_bytes - (0 if cleanup else budgets["cleanupResponseBytes"]) >= budgets["responseBytes"] + 1,
                "Normal work cannot spend the cleanup response reserve.")
        ordinal, stamp = self.count + 1, self.utc_now()
        instant(stamp)
        if label == "login":
            require(precise_timestamp(stamp)[0] >= precise_timestamp(self.authority.not_before)[0], "The fresh login precedes independent parent and recovery closure.")
        stem = "%04d-%s" % (ordinal, label)
        request = {"method": method, "route": route, "headers": [[key, value] for key, value in headers.items()], "body": body}
        token_sha = digest(self.token.encode()) if self.token is not None else None
        intent = {"ordinal": ordinal, "label": label, "actor": "admin", "planSha256": self.plan_sha256, "request": request,
                  "payloadBase64": None if payload is None else base64.b64encode(payload).decode(), "tokenSha256": token_sha, "createdAt": stamp,
                  "credentialRef": self.manifest["admin"]["credentialRef"]}
        intent_sha = self._save(stem + "-intent.json", intent)
        self.pending = {"ordinal": ordinal, "label": label, "intentSha256": intent_sha, "request": request}
        self.count += 1; self.normal_count += int(not cleanup); self.cleanup_count += int(cleanup); self.labels.add(label)
        self._save(stem + "-reserved.json", self._state()); self._persist()
        self.authority.check(); self.journal.check()
        remaining = deadline - self._now()
        require(remaining > 0, "Durable reservation consumed the request deadline.")
        try:
            response = self.transport.send(SimpleNamespace(method=method, route=route, label=label, actor="admin", cleanup=cleanup), headers, payload,
                    timeout_seconds=min(budgets["requestSeconds"], remaining), max_bytes=budgets["responseBytes"] + 1)
            require(isinstance(response.raw, bytes), "The transport did not retain raw response bytes.")
            raw = response.raw[:budgets["responseBytes"] + 1]
            response_headers = [[key, value] for key, value in self.support.bounded_header_prefix(response.headers)]
            wire = {"ordinal": ordinal, "label": label, "actor": "admin", "completedAt": response.completed_at, "request": request,
                    "payloadBase64": intent["payloadBase64"], "status": response.status, "headers": response_headers, "rawBase64": base64.b64encode(raw).decode(),
                    "completeHttp": response.complete_http, "failure": response.failure, "observedRawBytes": len(response.raw), "retainedRawTruncated": len(raw) != len(response.raw)}
            wire_sha = self._save(stem + "-response.json", wire)
            self.charged_bytes += len(raw) if response.complete_http and response.failure is None and len(response.raw) <= budgets["responseBytes"] else budgets["responseBytes"] + 1
            if label == "login":
                self.uncertain = True
                self.ownership_pending = {"ordinal": ordinal, "responseReceiptSha256": wire_sha, "stage": "response-awaiting-owner"}
                self._persist()
            require(len(response.raw) <= budgets["responseBytes"] and len(response_headers) == len(response.headers), "The response exceeds its finite capture bounds.")
            decoded = decode_wire(wire)
            completed = instant(response.completed_at)
            require(completed >= instant(stamp) and (self.last_completed is None or completed >= self.last_completed), "Response time precedes its request or previous response.")
            self.last_completed = completed
            self._now()
            self.authority.check(); self.journal.check()
            event = {"ordinal": ordinal, "status": response.status, "body": decoded, "requestAt": stamp, "completedAt": response.completed_at,
                     "intent": {"path": str(self.journal.root / "private" / (stem + "-intent.json")), "sha256": intent_sha},
                     "wire": {"path": str(self.journal.root / "private" / (stem + "-response.json")), "sha256": wire_sha}}
            self.responses[label] = event
            self.pending = None
            self._persist()
            if label in ("logout", "rejection"):
                adapter_intent = {"channel": "controller_api", "method": method, "path": route, "token_sha256": token_sha,
                                  "wire_intent": event["intent"], "wire_response": event["wire"]}
                adapter_result = {"channel": "controller_api", "status": response.status, "complete": True, "body": decoded,
                                  "token_sha256": token_sha, "completed_at": response.completed_at, "wire_response": event["wire"]}
                intent_name, result_name = stem + "-controller-intent.json", stem + "-controller-result.json"
                event["controllerIntent"] = {"path": str(self.journal.root / "private" / intent_name), "sha256": self._save(intent_name, adapter_intent)}
                event["controllerResult"] = {"path": str(self.journal.root / "private" / result_name), "sha256": self._save(result_name, adapter_result)}
            return event
        except BaseException:
            self.uncertain = True
            raise

    def _login(self):
        event = self._dispatch("login", "POST", "/emby/Users/AuthenticateByName", {"Username": self.authority.credentials["username"], "Pw": self.authority.credentials["password"]})
        require(event["status"] == 200, "The single login attempt was not acknowledged.")
        token, session = verify_login(event["body"], admin=self.manifest["admin"], server_id=self.manifest["server"]["id"], client=CLIENT, device_name=DEVICE_NAME)
        require(digest(token.encode()) not in self.authority.closed_token_hashes and session not in self.authority.closed_session_ids,
                "The new login reused a retained closed predecessor token or session.")
        self.token, self.session, self.login = token, session, event
        self.ownership_pending.update(stage="owner-registered", userId=self.manifest["admin"]["userId"], sessionId=session, tokenSha256=digest(token.encode()))
        self._persist()
        retained = deepcopy(self.ownership_pending)
        self.ownership_pending, self.uncertain = None, False
        try: self._persist()
        except BaseException:
            self.ownership_pending, self.uncertain = retained, True
            raise

    def _read(self, label, route):
        event = self._dispatch(label, "GET", route)
        self.read_index += 1
        self._persist()
        require(event["status"] == 200, "A required complete public observation was not HTTP 200.")
        return event["body"]

    def _snapshot(self):
        values = {label: self._read(label, route) for label, route in self.reads}
        result = snapshot_values(self.manifest, self.authority.baseline, values, self.utc_now(), self.token)
        self.outputs["publicBaseline"] = {"path": str(self.journal.root / "private" / "public-baseline.json"), "sha256": self._save("public-baseline.json", result)}
        return result

    def _preserve(self, after):
        return preserve_snapshot(self.manifest, self.authority, after, self.login, self.responses)

    def _cleanup(self):
        require(self.cleanup_started is None and not self.uncertain and not self.journal_failed and self.pending is None and self.ownership_pending is None and self.token is not None,
                "Cleanup requires known durable ownership and cannot restart.")
        self.cleanup_started = self._now()
        self._persist()
        logout = self._dispatch("logout", "POST", "/emby/Sessions/Logout")
        require(logout["status"] == 204 and logout["body"] is None, "The exact owned token logout was not an empty HTTP 204.")
        rejection = self._dispatch("rejection", "GET", "/emby/Sessions")
        require(rejection["status"] == 401, "The exact logged-out token remains usable.")
        self.closed_token = True
        self._persist()

    def _closure(self, snapshot):
        admin = self.manifest["admin"]
        result = {"userId": admin["userId"], "reportedDeviceId": admin["deviceId"], "tokenSha256": digest(self.token.encode()),
                  "from": snapshot["captured_at"], "through": self.responses["rejection"]["completedAt"]}
        result["login"] = {"intent": self.login["intent"], "response": self.login["wire"]}
        result["deviceObservation"] = {"intent": self.responses["devices"]["intent"], "response": self.responses["devices"]["wire"]}
        for label, status in (("logout", 204), ("rejection", 401)):
            event = self.responses[label]
            result[label] = {"record": event["controllerResult"], "intent": event["controllerIntent"], "status": status,
                             "tokenSha256": result["tokenSha256"], "completedAt": event["completedAt"]}
        return result

    def run(self):
        require(not self.used, "An observer cannot resume or run twice.")
        self.used = True
        try:
            self.authority.acquire(); self.authority.check()
            self.journal = self.journal_factory(self.manifest["scope"]["outputRoot"], 0)
            self.started = self._now()
            self._save("manifest.json", self.manifest); self._save("frozen-plan.json", self.plan); self._persist()
            self._login()
            snapshot = self._snapshot()
            proof = self._preserve(snapshot)
            self.outputs["preservation"] = {"path": str(self.journal.root / "private" / "public-preservation.json"), "sha256": self._save("public-preservation.json", proof)}
            self._cleanup()
            self.authority.check()
            closure = self._closure(snapshot)
            self.outputs["closedAuthentication"] = {"path": str(self.journal.root / "private" / "closed-authentication.json"), "sha256": self._save("closed-authentication.json", closure)}
            self.completed = True
        except BaseException as error:
            self.failure = {"type": type(error).__name__, "pendingLabel": self.pending.get("label") if self.pending else None, "readIndex": self.read_index}
            if self.pending is not None or self.ownership_pending is not None: self.uncertain = True
            if self.journal is not None and not self.journal_failed and not self.uncertain and self.token is not None and self.cleanup_started is None:
                try: self._cleanup()
                except BaseException as cleanup_error:
                    self.failure["cleanupFailureType"] = type(cleanup_error).__name__
        finally:
            terminal = {"schemaVersion": 1, "runId": self.manifest["runId"], "status": "awaiting_independent_attestation" if self.completed else
                        "recovery_required" if self.uncertain or self.journal_failed or self.token is not None and not self.closed_token else "stopped_with_known_cleanup",
                        "requestCount": self.count, "normalRequestCount": self.normal_count, "cleanupRequestCount": self.cleanup_count,
                        "completeSnapshotObserved": self.completed, "cleanupComplete": self.closed_token and not self.uncertain and not self.journal_failed,
                        "uncertain": self.uncertain, "failure": self.failure, "outputs": self.outputs,
                        "baselineReleased": False, "independentAttestationRequired": True, "originalImplementationBytesRead": False, "referenceDatabaseRead": False,
                        "mediaLibraryUserOrPlaybackMutations": 0}
            if self.journal is not None and not self.journal_failed:
                try:
                    self._save("terminal.json", terminal)
                    self._save("terminal.json", {key: value for key, value in terminal.items() if key != "outputs"}, export=True)
                except BaseException:
                    terminal.update(status="recovery_required", cleanupComplete=False, baselineReleased=False, evidenceIncomplete=True)
            if self.journal is not None: self.journal.close()
            self.authority.close()
        return terminal


def completed_exec_argv(value):
    """Decode one actual systemctl ExecStart record or its plain argv spelling."""
    require(isinstance(value, str) and value.strip(), "The actual completed ExecStart is required.")
    value = value.strip()
    if not value.startswith("{"):
        return shlex.split(value, posix=True)
    lexer = shlex.shlex(value, posix=True, punctuation_chars=";{}")
    lexer.whitespace_split, lexer.commenters = True, ""
    tokens = list(lexer)
    require(tokens[:1] == ["{"] and tokens[-1:] == ["}"] and
            all(token not in ("{", "}") and not (set(token) <= set(";{}") and token != ";") for token in tokens[1:-1]),
            "ExecStart must contain exactly one concrete command record.")
    fields = [[]]
    for token in tokens[1:-1]:
        if token == ";": fields.append([])
        else: fields[-1].append(token)
    require(len(fields) >= 2 and len(fields[0]) == 1 and fields[0][0].startswith("path=") and
            fields[1] and fields[1][0].startswith("argv[]=") and
            not any(token.startswith(("path=", "argv[]=")) for field in fields[2:] for token in field),
            "ExecStart must bind one executable path and one complete argv field.")
    argv = [fields[1][0][len("argv[]="):], *fields[1][1:]]
    require(argv[0] and fields[0][0] == "path=" + argv[0], "The ExecStart executable path and argv disagree.")
    return argv


def verify_completed_evidence(evidence, *, read_bytes):
    """Verify completed owned evidence without HTTP, process probes, or file writes.

    The caller loads this module by its frozen source SHA and supplies a descriptor
    reader returning bytes. The returned baseline is rebuilt from all actual wire
    bodies. Every generated journal artifact is compared with retained exact bytes.
    Neither the independent summary nor a caller-provided baseline substitutes for
    actual request replay. Historical parent and recovery scopes remain untouched.
    """
    require(isinstance(evidence, dict) and set(evidence) == {"manifest", "independent"} and callable(read_bytes), "The exact completed evidence pair and descriptor reader are required.")
    for row in evidence.values(): descriptor(row)
    raw = read_bytes(deepcopy(evidence["manifest"]))
    require(isinstance(raw, bytes) and digest(raw) == evidence["manifest"]["sha256"], "The completed manifest bytes differ.")
    manifest = validate_manifest(strict_json(raw))
    attestation_root = Path(evidence["independent"]["path"]).parent
    require(attestation_root.parent == Path(manifest["scope"]["fixtureRoot"]) and attestation_root != Path(manifest["scope"]["outputRoot"]), "The independent receipt needs its own explicit fixture-root child.")
    reader = EvidenceReader(manifest, read_bytes, completed=True, extra_roots=(attestation_root,))
    require(same(reader.record(evidence["manifest"]), manifest), "The input manifest changed between reads.")
    independent = reader.record(evidence["independent"])
    required = {"schemaVersion", "kind", "status", "runId", "source", "manifest", "state", "observerTerminal", "wireIndex", "afterPublic", "closedAuthentication",
                "scopeInventory", "unit", "formerPid", "formerPidAbsent", "cgroupEmpty", "requestCount", "normalRequestCount", "cleanupRequestCount",
                "baselineUsableForFreshPreparation", "matrixFixtureReleased", "preservationBefore", "preservationAfter"}
    require(isinstance(independent, dict) and required <= set(independent) and independent["schemaVersion"] == 1 and
            independent["kind"] == "nextup-preparation04-baseline-independent-terminal" and independent["status"] == "complete_baseline_independently_accepted" and
            independent["runId"] == manifest["runId"] and same(independent["manifest"], evidence["manifest"]) and same(independent["source"], manifest["sources"]["observer"]) and
            independent["baselineUsableForFreshPreparation"] is True and independent["matrixFixtureReleased"] is False,
            "The independent receipt does not bind this exact completed observer scope.")
    for key, name in (("state", "state.json"), ("observerTerminal", "terminal.json"),
                      ("afterPublic", "public-baseline.json"), ("closedAuthentication", "closed-authentication.json")):
        descriptor(independent[key])
        require(independent[key]["path"] == str(Path(manifest["scope"]["outputRoot"]) / "private" / name),
                "A completed output descriptor must name its actual private journal file.")
    unit = validate_closed_unit(independent["unit"], failed=False)
    require(type(independent["formerPid"]) is int and independent["formerPid"] > 1 and unit["properties"].get("ExecMainPID") == str(independent["formerPid"]) and
            independent["formerPidAbsent"] is True and independent["cgroupEmpty"] is True,
            "The actual completed invocation, former process, or worker input binding is missing.")
    reader.raw(independent["source"])
    for row in manifest["sources"].values(): reader.raw(row)
    authority = InputEvidence(manifest, reader)
    preserved_before, preserved_after = verify_preservation_pair(reader, independent["preservationBefore"], independent["preservationAfter"],
                                                                roots=manifest["sealedRoots"], anchor_descriptor=authority.seal["afterPreservation"])
    credentials = reader.record(manifest["inputs"]["credentials"])
    require(credentials.get("schemaVersion") == 1 and credentials.get("runId") == manifest["runId"] and isinstance(credentials.get("admin"), dict) and
            set(credentials["admin"]) == {"userId", "username", "credentialRef", "password"} and
            all(credentials["admin"][key] == manifest["admin"][key] for key in ("userId", "username", "credentialRef")) and
            isinstance(credentials["admin"]["password"], str) and len(credentials["admin"]["password"]) >= 32, "The completed credential authority differs.")
    authority.credentials = credentials["admin"]
    transport_source = manifest["sources"]["transport"]
    support_name = "nextup_completed_observer_support_" + transport_source["sha256"]
    support = importlib.util.module_from_spec(importlib.util.spec_from_file_location(support_name, transport_source["path"]))
    previous_module = sys.modules.get(support_name)
    sys.modules[support_name] = support
    try:
        exec(compile(reader.raw(transport_source), transport_source["path"], "exec"), support.__dict__)
    finally:
        if previous_module is None: sys.modules.pop(support_name, None)
        else: sys.modules[support_name] = previous_module
    authority.support = support
    authority.acquire = authority.check = authority.close = lambda: None
    plan = frozen_plan(manifest, authority.baseline)
    expected_argv = ["/usr/bin/python3", "-I", "-B", manifest["sources"]["observer"]["path"], "observe", "--manifest", evidence["manifest"]["path"],
                     "--manifest-sha256", evidence["manifest"]["sha256"], "--plan-sha256", digest(canonical(plan).encode())]
    require(completed_exec_argv(unit["properties"].get("ExecStart")) == expected_argv,
            "The completed unit must execute the exact observe command and recomputed frozen plan.")
    counts = (plan["totalLimit"], plan["normalLimit"], plan["cleanupLimit"])
    require(tuple(independent[key] for key in ("requestCount", "normalRequestCount", "cleanupRequestCount")) == counts, "The completed receipt does not match the derived request budget.")
    index, events = read_ledger(reader, independent["wireIndex"], plan["totalLimit"], reserved=True)
    require(index.get("runId") == manifest["runId"] and list(events) == [row["label"] for row in plan["requests"]], "The actual request set differs from the complete frozen plan.")
    retained_baseline = reader.record(independent["afterPublic"])
    state, terminal = reader.record(independent["state"]), reader.record(independent["observerTerminal"])
    retained_closure = reader.record(independent["closedAuthentication"])
    require(terminal.get("status") == "awaiting_independent_attestation" and terminal.get("completeSnapshotObserved") is True and terminal.get("cleanupComplete") is True and
            terminal.get("uncertain") is False and terminal.get("baselineReleased") is False and state.get("uncertain") is False and state.get("pending") is None and
            state.get("ownershipPending") is None and state.get("closedToken") is True and state.get("failure") is None,
            "The original worker state did not complete with known exact-token closure.")
    require(same(terminal["outputs"]["publicBaseline"], independent["afterPublic"]) and same(terminal["outputs"]["closedAuthentication"], independent["closedAuthentication"]), "The independent outputs differ from their original worker descriptors.")
    stamps = [events[row["label"]]["intent"]["createdAt"] for row in plan["requests"][:plan["normalLimit"]]]
    stamps += [retained_baseline["captured_at"]] + [events[row["label"]]["intent"]["createdAt"] for row in plan["requests"][plan["normalLimit"]:]]
    require(precise_timestamp(events["configuration"]["wire"]["completedAt"])[0] <= precise_timestamp(retained_baseline["captured_at"])[0] <=
            precise_timestamp(events["logout"]["intent"]["createdAt"])[0], "The retained complete snapshot time is outside its actual capture and closure interval.")
    require(precise_timestamp(preserved_before["capturedAt"])[0] <= precise_timestamp(events["login"]["intent"]["createdAt"])[0] and
            precise_timestamp(preserved_after["capturedAt"])[0] >= precise_timestamp(events["rejection"]["wire"]["completedAt"])[0], "The preservation observations do not bracket the actual complete run.")
    stamp_iterator, generated = iter(stamps), {}

    class MemoryJournal:
        def __init__(self): self.root = Path(manifest["scope"]["outputRoot"])
        def check(self): pass
        def close(self): pass
        def save(self, name, value, *, export=False):
            path = str(self.root / ("export" if export else "private") / name)
            require(path not in generated, "The completed replay attempted to overwrite a receipt.")
            raw = (canonical(value) + "\n").encode()
            expected = {"path": path, "sha256": digest(raw)}
            require(reader.raw(expected) == raw, "A generated journal receipt differs from the retained exact bytes.")
            generated[path] = raw
            return expected["sha256"]
        def state(self, value): self.final_state = deepcopy(value)

    class WireReplay:
        def __init__(self): self.ordinal = 0
        def send(self, request, headers, payload, **bounds):
            self.ordinal += 1
            require(self.ordinal <= len(plan["requests"]), "The completed replay attempted extra HTTP.")
            expected = plan["requests"][self.ordinal - 1]
            event = events[expected["label"]]
            original = event["intent"]["request"]
            require((request.label, request.method, request.route, request.actor, request.cleanup) ==
                    (expected["label"], expected["method"], expected["route"], "admin", self.ordinal > plan["normalLimit"]) and
                    [[key, value] for key, value in headers.items()] == original["headers"] and
                    (None if payload is None else base64.b64encode(payload).decode()) == event["intent"].get("payloadBase64"), "The replay request differs from the original ordered wire intent.")
            wire = event["wire"]
            return SimpleNamespace(status=wire["status"], headers=[tuple(pair) for pair in wire["headers"]], raw=base64.b64decode(wire["rawBase64"], validate=True),
                                   complete_http=wire["completeHttp"], completed_at=wire["completedAt"], failure=wire["failure"])

    journal, wire = MemoryJournal(), WireReplay()
    tick = [0.0]
    def clock():
        tick[0] += 0.001
        return tick[0]
    runner = ObserverRunner(manifest, authority=authority, transport=wire, journal_factory=lambda root, uid: journal,
                            monotonic=clock, utc_now=lambda: next(stamp_iterator))
    replayed = runner.run()
    require(replayed == terminal and wire.ordinal == plan["totalLimit"] and next(stamp_iterator, None) is None and same(journal.final_state, state), "The completed runner state, terminal, or exact sequence did not replay.")
    require(reader.raw(independent["state"]) == (canonical(journal.final_state) + "\n").encode(), "The final private state bytes differ from the complete replay.")
    verify_scope_inventory(reader, independent["scopeInventory"], journal.root, generated, journal.final_state)
    baseline = strict_json(generated[independent["afterPublic"]["path"]])
    require(same(baseline, retained_baseline) and same(runner._closure(baseline), retained_closure), "The complete fresh baseline or raw-bound closure differs from replay.")
    return {"schemaVersion": 1, "kind": "nextup-preparation04-baseline-verified-evidence", "manifest": deepcopy(evidence["manifest"]),
            "independent": deepcopy(evidence["independent"]), "afterPublic": deepcopy(independent["afterPublic"]), "closedAuthentication": deepcopy(independent["closedAuthentication"]),
            "requestCount": plan["totalLimit"], "normalRequestCount": plan["normalLimit"], "cleanupRequestCount": plan["cleanupLimit"], "baseline": baseline}


def main(arguments=None):
    require(sys.flags.isolated and sys.flags.dont_write_bytecode, "Use Python -I -B to exclude ambient module paths.")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("plan", "observe"))
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--plan-sha256")
    args = parser.parse_args(arguments)
    pin = {"path": args.manifest, "sha256": args.manifest_sha256}
    descriptor(pin)
    raw = read_owned(pin["path"], private=True)
    require(digest(raw) == pin["sha256"], "The explicit manifest digest differs.")
    manifest = validate_manifest(strict_json(raw))
    authority = Authority(manifest, pin)
    plan = frozen_plan(manifest, authority.baseline)
    plan_sha = digest(canonical(plan).encode())
    if args.mode == "plan":
        authority.close()
        print(canonical({"plan": plan, "planSha256": plan_sha}))
        return 0
    require(args.plan_sha256 == plan_sha, "The observe entry point requires its independently reviewed exact plan pin.")
    runner = ObserverRunner(manifest, authority=authority, transport=authority.support.HTTPTransport(manifest["endpoint"]),
                            journal_factory=lambda root, uid: authority.support.Journal(root, uid=uid))
    terminal = runner.run()
    print(canonical({key: terminal[key] for key in ("status", "requestCount", "cleanupComplete", "baselineReleased")}))
    return 0 if terminal["status"] == "awaiting_independent_attestation" else 2


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (ObservationError, OSError, ValueError) as error:
        print(canonical({"status": "blocked", "failureType": type(error).__name__, "baselineReleased": False}), file=sys.stderr)
        raise SystemExit(2)
