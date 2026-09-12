#!/usr/bin/env python3
"""Read one complete reference baseline through 34 bounded authentication/API calls.

The only POST routes are existing-administrator authentication and exact-token
logout. Existing failed-preparation evidence is read, never resumed or repaired.
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
import stat
import subprocess
import time
from types import SimpleNamespace
from urllib.parse import urlencode

TRANSPORT_SHA256 = "d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1"
FIELDS = "Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear"
CLIENT, DEVICE_NAME, VERSION = "Goby NextUp Baseline Observer", "Linux Baseline Recorder", "1.0"
NORMAL_LIMIT, CLEANUP_LIMIT, TOTAL_LIMIT = 32, 2, 34


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
    require(isinstance(value, dict) and set(value) == {"schemaVersion", "runId", "server", "endpoint", "process", "lock", "sources", "inputs",
            "scope", "sealedRoots", "forbiddenOriginalRoots", "admin", "preservation", "budgets", "closedObservers"}, "The exact observer manifest is required.")
    value = deepcopy(value)
    require(type(value["schemaVersion"]) is int and value["schemaVersion"] == 2 and identifier(value["runId"]), "The version-two observer manifest and run ID are required.")
    require(isinstance(value["server"], dict) and set(value["server"]) == {"id", "version"} and identifier(value["server"]["id"]) and
            isinstance(value["server"]["version"], str) and value["server"]["version"], "A public server binding is required.")
    require(value["endpoint"] == {"scheme": "http", "host": "127.0.0.1", "port": 18197}, "Only the existing host proxy on port 18197 is allowed.")
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
    require(isinstance(value["lock"], dict) and set(value["lock"]) == {"path", "device", "inode"} and
            all(type(value["lock"][key]) is int and value["lock"][key] > 0 for key in ("device", "inode")), "The existing lock identity is required.")
    require(set(value["sources"]) == {"observer", "transport", "proxy"} and set(value["inputs"]) == {"credentials", "publicBaseline", "predecessor"},
            "The exact owned sources and actual private evidence inputs are required.")
    for row in (*value["sources"].values(), *value["inputs"].values()): descriptor(row)
    require(isinstance(value["closedObservers"], list) and len(value["closedObservers"]) <= 8, "An explicit bounded ordered closed-observer list is required.")
    for row in value["closedObservers"]: descriptor(row)
    require(len({row["path"] for row in value["closedObservers"]}) == len(value["closedObservers"]) and
            len({row["sha256"] for row in value["closedObservers"]}) == len(value["closedObservers"]), "A closed observer recovery cannot be consumed twice.")
    require(Path(value["sources"]["observer"]["path"]).name == "observe-nextup-global-reference-baseline.py" and
            Path(value["sources"]["transport"]["path"]).name == "nextup-global-transport.py" and
            value["sources"]["transport"]["sha256"] == TRANSPORT_SHA256, "Only the reviewed observer and transport may be loaded.")
    require(set(value["scope"]) == {"fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot", "outputRoot"}, "All source and output roots must be explicit.")
    for entry in value["scope"].values(): absolute(entry)
    output = Path(value["scope"]["outputRoot"])
    require(output.parent == Path(value["scope"]["fixtureRoot"]), "The new evidence root must be a direct fixture-root child.")
    require(Path(value["scope"]["fixtureRoot"]) in absolute(value["lock"]["path"]).parents, "The lock escaped its fixture root.")
    for name, rows in (("sealedRoots", value["sealedRoots"]), ("forbiddenOriginalRoots", value["forbiddenOriginalRoots"])):
        require(isinstance(rows, list) and rows and len(set(rows)) == len(rows), "Explicit unique exclusion roots are required: " + name)
        for entry in rows:
            root = absolute(entry)
            require(output != root and root not in output.parents and output not in root.parents, "The fresh output overlaps an excluded scope.")
    forbidden = [Path(root) for root in value["forbiddenOriginalRoots"]]
    require(any(Path(process["application"]["exe"]) == root or root in Path(process["application"]["exe"]).parents for root in forbidden),
            "The original executable must be excluded from byte-reading authority.")
    command = process["application"]["cmdline"]
    require(command.count("-programdata") == 1 and command.index("-programdata") + 1 < len(command), "The original data-root argument is required.")
    data = absolute(command[command.index("-programdata") + 1])
    require(any(data == root or root in data.parents for root in forbidden), "The original database root must be excluded.")
    for name, row in value["sources"].items():
        root = Path(value["scope"]["proxySourceRoot" if name == "proxy" else "sourceRoot"])
        require(root in Path(row["path"]).parents, "An owned source escaped its explicit root.")
    allowed_inputs = [Path(value["scope"]["inputRoot"]), *map(Path, value["sealedRoots"])]
    for row in [*value["inputs"].values(), *value["closedObservers"]]:
        require(any(root in Path(row["path"]).parents for root in allowed_inputs), "An input escaped its owned input or sealed root.")
    for path in [*value["scope"].values(), value["lock"]["path"], *(row["path"] for row in (*value["sources"].values(), *value["inputs"].values(), *value["closedObservers"]))]:
        require(all(Path(path) != root and root not in Path(path).parents for root in forbidden), "A byte-reading or writing path entered original implementation/data authority.")
    admin = value["admin"]
    require(isinstance(admin, dict) and set(admin) == {"userId", "username", "credentialRef", "deviceId"} and
            all(identifier(admin[key]) for key in ("userId", "credentialRef", "deviceId")) and isinstance(admin["username"], str) and admin["username"],
            "One existing administrator and one new observer device must be frozen.")
    scope = value["preservation"]
    require(isinstance(scope, dict) and set(scope) == {"userIds", "libraryIds", "detailRoutes"} and
            isinstance(scope["userIds"], list) and len(set(scope["userIds"])) == len(scope["userIds"]) == 6 and
            isinstance(scope["libraryIds"], list) and len(set(scope["libraryIds"])) == len(scope["libraryIds"]) == 8 and
            all(identifier(key) for key in scope["userIds"] + scope["libraryIds"]) and admin["userId"] in scope["userIds"] and
            isinstance(scope["detailRoutes"], list) and len(scope["detailRoutes"]) == 6, "The exact six users, eight libraries, and six details must be frozen.")
    for row in scope["detailRoutes"]:
        require(isinstance(row, dict) and set(row) == {"group", "userId", "itemId"} and identifier(row["group"]) and
                row["userId"] in scope["userIds"] and identifier(row["itemId"]), "A detail witness escaped its explicit scope.")
    require(len({(row["group"], row["itemId"]) for row in scope["detailRoutes"]}) == 6, "Detail witnesses must be unique.")
    budgets = value["budgets"]
    limits = {"requestSeconds": (1, 15), "normalSeconds": (1, 600), "cleanupSeconds": (1, 120), "requestBytes": (1024, 32768),
              "responseBytes": (1024, 1048576), "totalResponseBytes": (4096, 40 * 1024 * 1024), "cleanupResponseBytes": (1024, 3 * 1024 * 1024)}
    require(isinstance(budgets, dict) and set(budgets) == set(limits) and all(type(budgets[key]) is int and low <= budgets[key] <= high for key, (low, high) in limits.items()) and
            budgets["cleanupResponseBytes"] >= 2 * (budgets["responseBytes"] + 1) and
            budgets["totalResponseBytes"] > budgets["cleanupResponseBytes"], "The finite request/time/byte budgets and cleanup reserve are required.")
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


def validate_baseline(value, manifest):
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
            admin["deviceId"] not in {row["ReportedDeviceId"] for row in devices.values()}, "The existing administrator or fresh observer device is unproven.")
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
    require(len(rows) == 31 and len(set(rows)) == 31, "The complete read plan must have exactly 31 unique reads.")
    return rows


def frozen_plan(manifest, baseline):
    return {"schemaVersion": 1, "runId": manifest["runId"], "manifestSha256": digest(canonical(manifest).encode()),
            "requests": [{"label": "login", "method": "POST", "route": "/emby/Users/AuthenticateByName"}] +
                [{"label": label, "method": "GET", "route": route} for label, route in read_plan(manifest, baseline)] +
                [{"label": "logout", "method": "POST", "route": "/emby/Sessions/Logout"}, {"label": "rejection", "method": "GET", "route": "/emby/Sessions"}],
            "normalLimit": NORMAL_LIMIT, "cleanupLimit": CLEANUP_LIMIT, "totalLimit": TOTAL_LIMIT,
            "createsMediaLibrariesUsersOrPlayback": False, "automaticRetryOrResume": False, "independentAttestationRequired": True}


def decode_wire(wire):
    require(isinstance(wire, dict) and wire.get("completeHttp") is True and wire.get("failure") is None and wire.get("retainedRawTruncated") is False and
            type(wire.get("status")) is int and 100 <= wire["status"] <= 599 and isinstance(wire.get("headers"), list), "The retained wire response is incomplete.")
    raw = base64.b64decode(wire["rawBase64"], validate=True)
    require(len(raw) == wire.get("observedRawBytes") and len(raw) <= 1048576, "The retained raw wire length differs.")
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


class Authority:
    """Pin metadata, actual closed predecessor evidence, inputs, sources, and lock."""

    def __init__(self, manifest, manifest_descriptor):
        self.manifest = validate_manifest(manifest)
        self.frozen = canonical(self.manifest)
        self.lock_fd, self.closed, self.pins = None, False, {}
        require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
                "Observation requires root SSH and Python -I -B.")
        require(Path(self.manifest["sources"]["observer"]["path"]) == Path(__file__).absolute(), "The source pin must bind this observer file.")
        self._record(manifest_descriptor, input_manifest=True)
        for row in self.manifest["sources"].values():
            require(digest(read_owned(row["path"])) == row["sha256"], "An owned frozen source changed.")
        self.support = load_transport(self.manifest["sources"]["transport"])
        self.baseline = validate_baseline(self._record(self.manifest["inputs"]["publicBaseline"]), self.manifest)
        record = self._record(self.manifest["inputs"]["credentials"])
        admin = self.manifest["admin"]
        require(isinstance(record, dict) and set(record) == {"schemaVersion", "runId", "admin"} and record["schemaVersion"] == 1 and record["runId"] == self.manifest["runId"] and
                isinstance(record["admin"], dict) and set(record["admin"]) == {"userId", "username", "credentialRef", "password"} and
                all(record["admin"][key] == admin[key] for key in ("userId", "username", "credentialRef")) and
                isinstance(record["admin"]["password"], str) and len(record["admin"]["password"]) >= 32, "The one existing administrator credential input differs.")
        self.credentials = record["admin"]
        self.predecessor = self._record(self.manifest["inputs"]["predecessor"])
        self._predecessor()
        self._closed_observers()
        self.units_frozen = canonical(self.units)
        for name in ("fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot"):
            protected(self.manifest["scope"][name], directory=True)
        require(not os.path.lexists(self.manifest["scope"]["outputRoot"]), "An existing observer output can never resume.")

    def _record(self, row, *, input_manifest=False):
        descriptor(row)
        path = Path(row["path"])
        roots = [Path(self.manifest["scope"]["inputRoot"]), *map(Path, self.manifest["sealedRoots"])]
        require(path.suffix == ".json" and any(root in path.parents for root in roots) and all(Path(root) != path and Path(root) not in path.parents for root in self.manifest["forbiddenOriginalRoots"]),
                "An actual JSON proof escaped its explicit owned evidence roots.")
        raw = read_owned(path, private=True)
        require(digest(raw) == row["sha256"], "An exact private proof changed.")
        previous = self.pins.setdefault(str(path), row["sha256"])
        require(previous == row["sha256"], "One evidence path has conflicting authority pins.")
        value = strict_json(raw)
        require(not input_manifest or same(value, self.manifest), "The input file differs from the frozen observer manifest.")
        return value

    def _predecessor(self):
        terminal, admin = self.predecessor, self.manifest["admin"]
        require(isinstance(terminal, dict) and terminal.get("kind") == "nextup-global-failed-preparation-terminal" and
                terminal.get("status") == "failed_preparation_independently_sealed_after_known_cleanup" and
                terminal.get("requestCount") == 33 and terminal.get("normalRequestCount") == 31 and terminal.get("cleanupRequestCount") == 2 and
                all(terminal.get(key) is True for key in ("noLibraryCreation", "noUserCreation", "noPlayback", "noMetadataMutation", "old109RootsPreserved",
                    "allWireRequestsComplete", "knownAdministratorClosed", "recursiveCgroupEmpty", "originalApplicationAndProxyMetadataUnchanged")) and
                terminal.get("configurationObserved") is False and terminal.get("completeReferencePreservationClaimed") is False and
                terminal.get("originalImplementationBytesRead") is False and terminal.get("referenceDatabaseRead") is False,
                "The independent failed-preparation terminal does not establish the exact completed read-only attempt.")
        instant(terminal["capturedAt"])
        previous = self._record(terminal["manifest"])
        state, producer = self._record(terminal["state"]), self._record(terminal["producerTerminal"])
        closure = self._record(terminal["authenticationClosure"])
        require(previous.get("runId") == terminal["runId"] == state.get("runId") == producer.get("runId") == closure.get("runId") and
                same(previous.get("server"), self.manifest["server"]) and same(previous.get("process"), self.manifest["process"]) and
                same(previous.get("lock"), self.manifest["lock"]), "The predecessor identifies another run, process, server, or lock.")
        require(state.get("requestCount") == 33 and state.get("normalRequestCount") == 31 and state.get("cleanupRequestCount") == 2 and
                state.get("uncertain") is False and state.get("pending") is None and state.get("ownershipPending") is None and state.get("revoked") == ["admin"] and
                state.get("userIds") == {"admin": admin["userId"]} and all(not state.get(key) for key in ("libraries", "items", "plays", "touched", "calibrations")),
                "The retained producer state contains unresolved or out-of-scope resource ownership.")
        require(producer.get("status") == "stopped_with_known_cleanup" and producer.get("cleanupComplete") is True and producer.get("uncertain") is False and
                producer.get("pending") is None and producer.get("ownershipPending") is None, "The producer did not finish with known exact-token cleanup.")
        metadata = previous["actors"]["admin"]
        require(metadata["userId"] == admin["userId"] and metadata["username"] == admin["username"] and metadata["deviceId"] != admin["deviceId"] and
                closure.get("userId") == admin["userId"] and closure.get("username") == admin["username"] and closure.get("reportedDeviceId") == metadata["deviceId"] and
                closure.get("exactTokenClosed") is True and sha(closure.get("tokenSha256")), "The closure lacks one distinct acknowledged prior administrator device.")
        require(instant(self.baseline["captured_at"]) <= instant(closure["from"]) <= instant(closure["through"]) <= instant(terminal["capturedAt"]), "The predecessor closure window is inconsistent.")
        expected = {"login": (1, "POST", "/emby/Users/AuthenticateByName", 200), "deviceObservation": (31, "GET", "/emby/Devices", 200),
                    "logout": (32, "POST", "/emby/Sessions/Logout", 204), "rejection": (33, "GET", "/emby/Sessions", 401)}
        observed, previous_time, response_pins = {}, None, set()
        for name, (ordinal, method, route, status) in expected.items():
            proof = closure[name]
            require(isinstance(proof, dict) and set(proof) == {"intent", "response"}, "A closure step requires its actual intent and wire response.")
            intent, response = self._record(proof["intent"]), self._record(proof["response"])
            require(response.get("ordinal") == intent.get("ordinal") == ordinal and response.get("actor") == intent.get("actor") == "admin" and
                    response.get("status") == status and same(intent.get("request"), response.get("request")) and
                    intent.get("planSha256") == state.get("planSha256") and response["request"].get("method") == method and response["request"].get("route") == route,
                    "A predecessor wire proof differs from its actual ordered intent.")
            require(proof["response"]["sha256"] not in response_pins, "Distinct closure responses cannot reuse a receipt.")
            response_pins.add(proof["response"]["sha256"])
            body = decode_wire(response)
            completed = instant(response["completedAt"])
            require(instant(closure["from"]) <= completed <= instant(closure["through"]) and (previous_time is None or previous_time <= completed), "Closure responses have inconsistent completion times.")
            previous_time = completed
            headers = response["request"]["headers"]
            require(isinstance(headers, list) and all(isinstance(pair, list) and len(pair) == 2 for pair in headers) and len({key.lower() for key, _ in headers}) == len(headers),
                    "The predecessor request must retain distinct actual header pairs.")
            actual_headers = {key.lower(): value for key, value in headers}
            authorization = 'Emby Client="Goby NextUp Preparation", Device="Linux Fixture Recorder", DeviceId="' + metadata["deviceId"] + '", Version="1.0"'
            require(actual_headers.get("authorization") == authorization, "The predecessor wire client/device metadata differs.")
            require(set(actual_headers) == {"accept", "authorization", "content-type" if name == "login" else "x-emby-token"} and
                    actual_headers["accept"] == "application/json", "The predecessor request contains another identity or unreviewed header.")
            require(response.get("payloadBase64") == intent.get("payloadBase64"), "The predecessor raw request payload differs from its intent.")
            if name == "login":
                token, session = verify_login(body, admin=metadata, server_id=self.manifest["server"]["id"], client="Goby NextUp Preparation", device_name="Linux Fixture Recorder")
                require(digest(token.encode()) == closure["tokenSha256"] and state.get("tokens") == {"admin": token} and state.get("sessions") == {"admin": session} and
                        "x-emby-token" not in actual_headers and intent.get("tokenSha256") is None, "The actual retained login does not own the closed token/session.")
                expected_body = {"Username": metadata["username"], "Pw": self.credentials["password"]}
                require(response["request"].get("body") == expected_body and
                        response.get("payloadBase64") == base64.b64encode(urlencode(expected_body).encode("utf-8")).decode() and
                        actual_headers["content-type"] == "application/x-www-form-urlencoded; charset=utf-8", "The historical login differs from its exact retained administrator form.")
            else:
                require(actual_headers.get("x-emby-token") == token and intent.get("tokenSha256") == closure["tokenSha256"] and response["request"].get("body") is None,
                        "The predecessor device/logout/rejection proof substituted its exact token or request body.")
                require(response.get("payloadBase64") is None, "A historical read or logout had an unexpected request payload.")
            observed[name] = {"body": body, "wire": response}
        require(observed["logout"]["body"] is None and observed["rejection"]["wire"]["completedAt"] == closure["through"], "The prior closure must end at its actual same-token 401.")
        devices = page(observed["deviceObservation"]["body"], devices=True)
        old = self.baseline["devices"]
        require(len(old) == 85 and len(devices) == 86 and set(old) <= set(devices) and all(same(row, devices[key]) for key, row in old.items()),
                "The predecessor did not retain all 85 original device rows exactly.")
        new = set(devices) - set(old)
        require(len(new) == 1 and same(devices[next(iter(new))], closure.get("ownedDevice")), "The prior attempt has another new device population.")
        owned_device(closure["ownedDevice"], metadata, "Goby NextUp Preparation", "Linux Fixture Recorder")
        require(str(observed["login"]["body"]["SessionInfo"]["InternalDeviceId"]) == closure["ownedDevice"]["Id"],
                "The predecessor login's actual internal device ID differs from its registry row.")
        require(admin["deviceId"] not in {row["ReportedDeviceId"] for row in devices.values()}, "The new observer device already exists.")
        require(instant(closure["from"]) <= instant(closure["ownedDevice"]["DateLastActivity"]) <= instant(observed["deviceObservation"]["wire"]["completedAt"]), "The prior device activity escaped its actual observation interval.")
        self.closure, self.previous_devices = closure, devices
        self.closed_device_windows = {closure["ownedDevice"]["Id"]: {"from": closure["from"], "through": closure["through"],
            "userId": closure["userId"], "reportedDeviceId": closure["reportedDeviceId"]}}
        self.closed_token_hashes = {closure["tokenSha256"]}
        self.closed_session_ids = {observed["login"]["body"]["SessionInfo"]["Id"]}
        self.unit = validate_closed_unit(terminal["unit"], failed=True)
        self.units = [deepcopy(self.unit)]

    def _history_step(self, proof, *, ordinal, method, route, status, token, admin, plan_sha=None, login=False):
        require(isinstance(proof, dict) and set(proof) == {"intent", "response"}, "A recovered request needs distinct actual intent and wire descriptors.")
        intent, wire = self._record(proof["intent"]), self._record(proof["response"])
        require(type(intent.get("ordinal")) is int and type(wire.get("ordinal")) is int and intent.get("ordinal") == wire.get("ordinal") == ordinal and intent.get("actor") == wire.get("actor") == "admin" and
                identifier(intent.get("label")) and intent["label"] == wire.get("label") and wire.get("status") == status and
                same(intent.get("request"), wire.get("request")) and wire["request"].get("method") == method and wire["request"].get("route") == route and
                wire.get("payloadBase64") == intent.get("payloadBase64"), "An actual recovered response differs from its ordered request intent.")
        if plan_sha is not None: require(intent.get("planSha256") == plan_sha, "A retained observer login differs from its frozen plan.")
        rows = wire["request"]["headers"]
        require(isinstance(rows, list) and all(isinstance(pair, list) and len(pair) == 2 and all(isinstance(part, str) and "\r" not in part and "\n" not in part for part in pair)
                for pair in rows) and len({key.lower() for key, _ in rows}) == len(rows), "Recovered requests must retain distinct actual header pairs.")
        headers = {key.lower(): value for key, value in rows}
        authorization = 'Emby Client="' + CLIENT + '", Device="' + DEVICE_NAME + '", DeviceId="' + admin["deviceId"] + '", Version="' + VERSION + '"'
        require(set(headers) == {"accept", "authorization", "content-type" if login else "x-emby-token"} and
                headers["accept"] == "application/json" and headers["authorization"] == authorization,
                "A recovered request substituted the original observer client/device metadata.")
        if login:
            body = {"Username": admin["username"], "Pw": self.credentials["password"]}
            require(intent.get("tokenSha256") is None and wire["request"].get("body") == body and
                    wire.get("payloadBase64") == base64.b64encode(urlencode(body).encode("utf-8")).decode() and
                    headers["content-type"] == "application/x-www-form-urlencoded; charset=utf-8", "The retained observer login is not its actual existing-administrator form.")
        else:
            require(headers["x-emby-token"] == token and intent.get("tokenSha256") == digest(token.encode()) and
                    wire["request"].get("body") is None and wire.get("payloadBase64") is None,
                    "The recovery request did not use the original exact token with an empty body.")
        decoded = decode_wire(wire)
        require(precise_timestamp(intent.get("createdAt"))[0] <= precise_timestamp(wire["completedAt"])[0],
                "An original or recovery response precedes its actual request intent.")
        return {"intent": intent, "wire": wire, "body": decoded, "responseDescriptor": proof["response"]}

    def _closed_observers(self):
        """Advance only through independently closed failed-login recoveries."""
        accepted, seen_runs = [], {self.manifest["runId"], self.predecessor["runId"]}
        previous_completed = precise_timestamp(self.predecessor["capturedAt"])[0]
        for descriptor_row in self.manifest["closedObservers"]:
            record = self._record(descriptor_row)
            require(isinstance(record, dict) and type(record.get("schemaVersion")) is int and record.get("schemaVersion") == 1 and record.get("kind") == "nextup-baseline-observer-recovery-terminal" and
                    record.get("status") == "observer_login_independently_recovered_and_closed" and identifier(record.get("runId")) and
                    identifier(record.get("observerRunId")) and record["runId"] != record["observerRunId"] and
                    record["runId"] not in seen_runs and record["observerRunId"] not in seen_runs and
                    type(record.get("requestCount")) is int and record.get("requestCount") == 3 and type(record.get("observerRequestCount")) is int and record.get("observerRequestCount") == 1 and
                    all(record.get(key) is True for key in ("exactTokenClosed", "recursiveCgroupsEmpty", "noNewLogin", "noMetadataMutation", "noLibraryCreation", "noUserCreation", "noPlayback")) and
                    record.get("referenceDatabaseRead") is False and record.get("originalImplementationBytesRead") is False,
                    "A recovered observer lacks its exact independently closed three-request terminal.")
            observer, recovery = record.get("observer"), record.get("recovery")
            require(isinstance(observer, dict) and set(observer) == {"manifest", "state", "terminal", "login", "unit"} and isinstance(recovery, dict) and
                    set(recovery) == {"manifest", "deviceObservation", "logout", "rejection", "unit"}, "The original failed observer and independent recovery evidence must be explicit.")
            previous, state, terminal = self._record(observer["manifest"]), self._record(observer["state"]), self._record(observer["terminal"])
            recovered = self._record(recovery["manifest"])
            require(previous.get("runId") == state.get("runId") == terminal.get("runId") == record["observerRunId"] and
                    type(previous.get("schemaVersion")) is int and previous.get("schemaVersion") in (1, 2) and same(previous.get("server"), self.manifest["server"]) and
                    same(previous.get("process"), self.manifest["process"]) and same(previous.get("lock"), self.manifest["lock"]) and
                    previous.get("inputs", {}).get("publicBaseline") == self.manifest["inputs"]["publicBaseline"] and
                    previous.get("inputs", {}).get("predecessor") == self.manifest["inputs"]["predecessor"] and
                    previous.get("sources", {}).get("transport", {}).get("sha256") == TRANSPORT_SHA256 and
                    previous.get("sources", {}).get("proxy") == self.manifest["sources"]["proxy"], "A recovered observer identifies another baseline, process, server, proxy, or lock.")
            if previous["schemaVersion"] == 2:
                require(previous.get("closedObservers") == accepted, "A recovered version-two observer skipped or substituted its closed predecessor chain.")
            admin = previous.get("admin")
            require(isinstance(admin, dict) and set(admin) == {"userId", "username", "credentialRef", "deviceId"} and
                    admin["userId"] == self.manifest["admin"]["userId"] == record.get("userId") and
                    admin["username"] == self.manifest["admin"]["username"] == record.get("username") and identifier(admin["deviceId"]) and
                    admin["deviceId"] == record.get("reportedDeviceId") and admin["deviceId"] != self.manifest["admin"]["deviceId"] and
                    admin["deviceId"] not in {row["ReportedDeviceId"] for row in self.previous_devices.values()}, "A recovered observer device is not one unique existing-administrator login.")
            require(recovered.get("runId") == record["runId"] and recovered.get("observerRunId") == record["observerRunId"] and
                    same(recovered.get("server"), self.manifest["server"]) and same(recovered.get("process"), self.manifest["process"]) and
                    same(recovered.get("lock"), self.manifest["lock"]) and isinstance(recovered.get("admin"), dict) and
                    all(recovered["admin"].get(key) == admin[key] for key in ("userId", "username", "deviceId")) and
                    same(recovered.get("observer"), observer) and recovered.get("preparationAnchor") == self.manifest["inputs"]["predecessor"],
                    "The actual recovery manifest does not bind the failed observer's exact authority and original evidence.")
            expected_plan = digest(canonical(frozen_plan(previous, self.baseline)).encode())
            require(state.get("manifestSha256") == digest(canonical(previous).encode()) and state.get("planSha256") == expected_plan and
                    all(type(state.get(key)) is int for key in ("requestCount", "normalRequestCount", "cleanupRequestCount", "readIndex")) and
                    state.get("requestCount") == state.get("normalRequestCount") == 1 and state.get("cleanupRequestCount") == state.get("readIndex") == 0 and
                    state.get("uncertain") is True and state.get("token") is None and state.get("session") is None and state.get("closedToken") is False and
                    isinstance(state.get("pending"), dict) and isinstance(state.get("ownershipPending"), dict), "The original failed observer's unknown-login responsibility must remain unmodified.")
            require(terminal.get("status") == "recovery_required" and all(type(terminal.get(key)) is int for key in ("requestCount", "normalRequestCount", "cleanupRequestCount")) and terminal.get("requestCount") == terminal.get("normalRequestCount") == 1 and
                    terminal.get("cleanupRequestCount") == 0 and terminal.get("uncertain") is True and terminal.get("cleanupComplete") is False and
                    terminal.get("completeSnapshotObserved") is False and terminal.get("baselineReleased") is False and terminal.get("outputs") == {},
                    "The original observer terminal was changed into a success or contains another completed scope.")
            login = self._history_step(observer["login"], ordinal=1, method="POST", route="/emby/Users/AuthenticateByName", status=200,
                    token=None, admin=admin, plan_sha=expected_plan, login=True)
            token, session = verify_login(login["body"], admin=admin, server_id=self.manifest["server"]["id"], client=CLIENT, device_name=DEVICE_NAME)
            token_sha = digest(token.encode())
            require(token_sha == record.get("tokenSha256") and token_sha not in self.closed_token_hashes and session not in self.closed_session_ids and
                    state["pending"].get("ordinal") == state["ownershipPending"].get("ordinal") == 1 and state["pending"].get("label") == "login" and
                    state["pending"].get("intentSha256") == observer["login"]["intent"]["sha256"] and same(state["pending"].get("request"), login["intent"]["request"]) and
                    state["ownershipPending"].get("responseReceiptSha256") == observer["login"]["response"]["sha256"] and
                    state["ownershipPending"].get("stage") == "response-awaiting-owner", "The independent recovery does not claim the exact retained failed-login response.")
            require(record.get("from") == login["intent"].get("createdAt") and previous_completed <= precise_timestamp(record["from"])[0] <= precise_timestamp(login["wire"]["completedAt"])[0],
                    "The recovery interval must begin at the original actual login intent.")
            steps = {"login": login}
            for name, ordinal, method, route, status in (("deviceObservation", 1, "GET", "/emby/Devices", 200),
                    ("logout", 2, "POST", "/emby/Sessions/Logout", 204), ("rejection", 3, "GET", "/emby/Sessions", 401)):
                steps[name] = self._history_step(recovery[name], ordinal=ordinal, method=method, route=route, status=status, token=token, admin=admin,
                        plan_sha=digest(canonical(recovered).encode()))
                require(steps[name]["intent"].get("manifestSha256") == recovery["manifest"]["sha256"], "A recovery request is not bound to its exact actual input file.")
            pins = [step["responseDescriptor"]["sha256"] for step in steps.values()]
            require(len(set(pins)) == 4, "Four distinct original/recovery responses are required.")
            times = [previous_completed, *(precise_timestamp(value)[0] for step in steps.values() for value in (step["intent"]["createdAt"], step["wire"]["completedAt"])),
                     precise_timestamp(record["capturedAt"])[0]]
            require(times == sorted(times) and record.get("through") == steps["rejection"]["wire"]["completedAt"] and steps["logout"]["body"] is None,
                    "The completed recovery must end at its exact ordered logout 204 and same-token 401.")
            devices = page(steps["deviceObservation"]["body"], devices=True)
            require(len(devices) == len(self.previous_devices) + 1, "A recovery snapshot must add exactly its one original failed-login device.")
            preserve_closed_devices(self.previous_devices, devices, self.closed_device_windows)
            new = set(devices) - set(self.previous_devices)
            require(len(new) == 1, "The recovery Devices snapshot contains an unowned new population.")
            row = devices[next(iter(new))]
            require(same(row, record.get("ownedDevice")) and str(login["body"]["SessionInfo"]["InternalDeviceId"]) == row["Id"],
                    "The recovery's actual owned device differs from its original login internal device identity.")
            owned_device(row, admin, CLIENT, DEVICE_NAME)
            device_time_within(row["DateLastActivity"], record["from"], steps["deviceObservation"]["wire"]["completedAt"])
            require(self.manifest["admin"]["deviceId"] not in {item["ReportedDeviceId"] for item in devices.values()}, "The new observer device already exists in its recovered predecessor chain.")
            for unit, failed in ((observer["unit"], True), (recovery["unit"], False)):
                validated = validate_closed_unit(unit, failed=failed)
                require(validated["name"] not in {item["name"] for item in self.units} and validated["invocationId"] not in {item["invocationId"] for item in self.units},
                        "An observer/recovery unit cannot substitute a prior unit or invocation.")
                self.units.append(validated)
            self.previous_devices = devices
            self.closed_device_windows[row["Id"]] = {"from": record["from"], "through": record["through"], "userId": admin["userId"], "reportedDeviceId": admin["deviceId"]}
            self.closed_token_hashes.add(token_sha); self.closed_session_ids.add(session)
            accepted.append(descriptor_row); seen_runs.update((record["observerRunId"], record["runId"]))
            previous_completed = precise_timestamp(record["capturedAt"])[0]

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
            require(digest(read_owned(path, private=True)) == expected, "A pinned input or sealed proof changed during observation.")
        require(same(self.support.process_identity(self.manifest["process"]), self.manifest["process"]), "Application, proxy, namespace, or listener metadata changed.")
        for unit in self.units:
            keys = tuple(unit["properties"])
            require(keys and all(re.fullmatch(r"[A-Za-z][A-Za-z0-9]*", key) for key in keys), "Invalid systemd property metadata.")
            result = subprocess.run(["systemctl", "show", unit["name"], *["--property=" + key for key in keys]], check=True, capture_output=True, text=True, timeout=10)
            observed = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
            require(observed == unit["properties"], "A closed predecessor invocation/state changed.")
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
                    "A read escaped its exact ordered 31-route plan.")

    def _dispatch(self, label, method, route, body=None):
        require(self.journal is not None and not self.journal_failed and not self.uncertain and self.pending is None and self.ownership_pending is None and not self.completed,
                "A closed or unresolved observation cannot dispatch again.")
        require(canonical(self.manifest) == self.frozen and digest(canonical(self.plan).encode()) == self.plan_sha256, "The frozen observation plan changed.")
        require(same(self.reads, read_plan(self.manifest, self.authority.baseline)), "The exact retained read sequence changed.")
        self._guard(label, method, route, body)
        cleanup = self.cleanup_started is not None
        require(self.count < TOTAL_LIMIT and (self.cleanup_count < CLEANUP_LIMIT if cleanup else self.normal_count < NORMAL_LIMIT), "The exact HTTP budget is exhausted.")
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
        roster_rows = values["users"]
        users, libraries = sorted(self.manifest["preservation"]["userIds"]), sorted(self.manifest["preservation"]["libraryIds"])
        require(isinstance(roster_rows, list) and len(roster_rows) == 6 and all(isinstance(row, dict) and identifier(row.get("Id")) for row in roster_rows) and
                {row["Id"] for row in roster_rows} == set(users), "The exact existing user roster changed.")
        library_rows = page(values["libraries"], "ItemId", 16)
        require(set(library_rows) == set(libraries), "The exact existing library roster changed.")
        details = {}
        for index, row in enumerate(self.manifest["preservation"]["detailRoutes"]):
            details.setdefault(row["group"], {})[row["itemId"]] = values["detail-" + str(index)]
        result = {"marker": self.authority.baseline["marker"], "version": 1, "captured_at": self.utc_now(), "server": values["server"],
                  "roster": {row["Id"]: row for row in roster_rows}, "libraries": library_rows, "configuration": values["configuration"],
                  "catalog_by_library": {library: page(values["catalog-" + str(index)]) for index, library in enumerate(libraries)},
                  "items_by_user": {user: page(values["items-" + str(index)]) for index, user in enumerate(users)},
                  "preferences": {user: values["prefs-" + str(index)] for index, user in enumerate(users)}, "details": details,
                  "devices": page(values["devices"], devices=True), "credential_context": {"channel": "controller_api", "authenticated_user_id": self.manifest["admin"]["userId"],
                  "token_sha256": digest(self.token.encode()), "user_id_semantics": "subject_projection"}}
        self.outputs["publicBaseline"] = {"path": str(self.journal.root / "private" / "public-baseline.json"), "sha256": self._save("public-baseline.json", result)}
        return result

    def _preserve(self, after):
        before, admin = self.authority.baseline, self.manifest["admin"]
        require(instant(before["captured_at"]) <= instant(after["captured_at"]), "The complete snapshot moved backwards in time.")
        for key in ("server", "configuration", "libraries", "catalog_by_library", "items_by_user", "preferences", "details"):
            require(same(before[key], after[key]), "A complete retained document changed: " + key)
        require(set(before["roster"]) == set(after["roster"]), "The retained user population changed.")
        changes = []
        for user, old in before["roster"].items():
            current = after["roster"][user]
            allowed = {"LastLoginDate", "LastActivityDate"} if user == admin["userId"] else set()
            require(same({key: value for key, value in old.items() if key not in allowed}, {key: value for key, value in current.items() if key not in allowed}), "An account changed outside owned administrator authentication dates.")
            for key in allowed:
                if (key in old) == (key in current) and old.get(key) == current.get(key): continue
                require(instant(before["captured_at"]) <= instant(current[key]) <= instant(after["captured_at"]), "An administrator activity date escaped the retained observation interval.")
                changes.append({"kind": "administrator-authentication-time", "field": key})
        devices, previous = after["devices"], self.authority.previous_devices
        require(len(devices) == len(previous) + 1 and set(previous) <= set(devices), "The snapshot must preserve every acknowledged predecessor device and exactly one new observer device.")
        require(all(same(row, devices[key]) for key, row in before["devices"].items()), "An original device row changed or disappeared.")
        changes.extend(preserve_closed_devices(previous, devices, self.authority.closed_device_windows))
        new = set(devices) - set(previous)
        require(len(new) == 1, "An unowned new device appeared.")
        row = devices[next(iter(new))]
        owned_device(row, admin, CLIENT, DEVICE_NAME)
        require(str(self.login["body"]["SessionInfo"]["InternalDeviceId"]) == row["Id"], "The observed device differs from the login's actual internal device ID.")
        device_time_within(row["DateLastActivity"], self.login["requestAt"], self.responses["devices"]["completedAt"])
        self.observed_device = deepcopy(row)
        return {"completePublicDocumentsPreserved": True, "oldDevicesExactlyPreserved": 85,
                "previousClosedDevicesPreserved": len(self.authority.closed_device_windows), "newOwnedObserverDevices": 1,
                "allowedAuthenticationChanges": changes, "referenceDatabaseRead": False, "originalImplementationBytesRead": False,
                "priorBaseline": self.manifest["inputs"]["publicBaseline"], "predecessorTerminal": self.manifest["inputs"]["predecessor"],
                "closedObservers": self.manifest["closedObservers"]}

    def _cleanup(self):
        require(self.cleanup_started is None and not self.uncertain and not self.journal_failed and self.pending is None and self.ownership_pending is None and self.token is not None,
                "Cleanup requires known durable ownership and cannot restart.")
        self.cleanup_started = self._now()
        self._persist()
        logout = self._dispatch("logout", "POST", "/emby/Sessions/Logout")
        require(logout["status"] == 204, "The exact owned token logout was not HTTP 204.")
        rejection = self._dispatch("rejection", "GET", "/emby/Sessions")
        require(rejection["status"] == 401, "The exact logged-out token remains usable.")
        self.closed_token = True
        self._persist()

    def _closure(self, snapshot):
        admin = self.manifest["admin"]
        result = {"userId": admin["userId"], "reportedDeviceId": admin["deviceId"], "tokenSha256": digest(self.token.encode()),
                  "from": snapshot["captured_at"], "through": self.responses["rejection"]["completedAt"]}
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


def main(arguments=None):
    require(sys.flags.isolated and sys.flags.dont_write_bytecode, "Use Python -I -B to exclude ambient module paths.")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("plan", "observe"))
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--plan-sha256")
    args = parser.parse_args(arguments)
    descriptor_row = {"path": args.manifest, "sha256": args.manifest_sha256}
    descriptor(descriptor_row)
    raw = read_owned(args.manifest, private=True)
    require(digest(raw) == args.manifest_sha256, "The explicit input manifest digest differs.")
    manifest = validate_manifest(strict_json(raw))
    baseline_raw = read_owned(manifest["inputs"]["publicBaseline"]["path"], private=True)
    require(digest(baseline_raw) == manifest["inputs"]["publicBaseline"]["sha256"], "The retained complete baseline changed.")
    baseline = validate_baseline(strict_json(baseline_raw), manifest)
    plan = frozen_plan(manifest, baseline)
    plan_sha = digest(canonical(plan).encode())
    if args.mode == "plan":
        print(canonical({"plan": plan, "planSha256": plan_sha}))
        return 0
    require(args.plan_sha256 == plan_sha, "The observe entry point needs its separately reviewed plan pin.")
    authority = Authority(manifest, descriptor_row)
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
