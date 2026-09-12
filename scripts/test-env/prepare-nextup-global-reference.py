#!/usr/bin/env python3
"""Prepare an owned global NextUp fixture; publication requires independent attestation.

Plan is pure after reading its explicit input. Prepare is a single-use remote
operator with pinned owned sources, metadata-only original process inspection,
the existing proxy, an existing fixture lock, and no automatic recovery.
"""

from __future__ import annotations

import sys

if __name__ == "__main__" and (not sys.flags.isolated or not sys.flags.dont_write_bytecode):
    print("Use Python -I -B for the owned preparation entry point.", file=sys.stderr)
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
from urllib.parse import parse_qsl, urlencode, urlsplit
import uuid

sys.dont_write_bytecode = True
TRANSPORT_SHA256 = "d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1"
EPISODES = ("A1", "A2", "A3", "B1", "B2", "B3")
SUMMARIES = ("A", "AS1", "AS2", "B", "BS1", "BS2")
CALIBRATIONS = (("P", "A1", "partial"), ("P", "A1", "complete"),
                ("Q", "B1", "partial"), ("Q", "B1", "complete"))
FIELDS = "Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear"
RUNTIME, PARTIAL = 6_000_000_000, 1_200_000_000
MAX_REQUESTS, NORMAL_LIMIT, CLEANUP_RESERVE = 380, 280, 100
PHASE_LIMITS = {"before": 44, "libraries": 42, "mapping": 6, "accounts": 8,
                "baseline": 36, "calibration": 44, "zero": 28, "after": 49, "cleanup": 89}
PLAYBACK_FIELDS = {"Played", "PlayCount", "PlaybackPositionTicks", "LastPlayedDate"}


class PreparationError(ValueError):
    """A frozen preparation input or completed observation was rejected."""


def require(value, message):
    if not value:
        raise PreparationError(message)


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
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(result.utcoffset() is not None, "A timestamp has no timezone.")
    return result


def absolute(value):
    require(isinstance(value, str) and value.startswith("/") and ".." not in Path(value).parts and
            "\x00" not in value and "\n" not in value and "\r" not in value,
            "A normalized absolute Linux authority path is required.")
    return Path(value)


def inside(value, root):
    path, parent = absolute(value), absolute(root)
    require(path != parent and parent in path.parents, "A path escaped its explicit authority root.")
    return path


def descriptor(value):
    require(isinstance(value, dict) and set(value) == {"path", "sha256"} and sha(value["sha256"]),
            "An input needs its exact path and SHA-256.")
    absolute(value["path"])


def strict_json(raw):
    def pairs(rows):
        value = {}
        for key, item in rows:
            require(key not in value, "Duplicate JSON input keys are forbidden.")
            value[key] = item
        return value
    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs,
                      parse_constant=lambda value: (_ for _ in ()).throw(PreparationError("Nonfinite JSON is forbidden.")))


def validate_manifest(value):
    """Validate the complete structural authority before any output or HTTP effect."""
    require(isinstance(value, dict), "A preparation manifest is required.")
    value = deepcopy(value)
    required = {"schemaVersion", "runId", "target", "ownerUid", "server", "endpoint", "process", "lock",
                "sources", "inputs", "scope", "sealedRoots", "forbiddenOriginalRoots", "media", "actors", "libraries", "preservation",
                "budgets", "matrixBudgets", "lifecycleSeparationSeconds"}
    require(set(value) == required and type(value["schemaVersion"]) is int and value["schemaVersion"] == 1 and value["target"] == "reference" and
            identifier(value["runId"]) and type(value["ownerUid"]) is int and value["ownerUid"] == 0,
            "The exact version-one root-owned reference preparation manifest is required.")
    require(set(value["server"]) == {"id", "version"} and identifier(value["server"]["id"]) and
            isinstance(value["server"]["version"], str) and value["server"]["version"], "The current public server binding is missing.")
    require(value["endpoint"] == {"scheme": "http", "host": "127.0.0.1", "port": 18197},
            "Preparation uses only the existing owned host proxy on port 18197.")
    require(set(value["process"]) == {"application", "endpoint", "workerNetworkNamespace"},
            "Original application metadata and owned proxy metadata must be separate.")
    process_fields = {"pid", "startTicks", "bootId", "uid", "exe", "exeDevice", "exeInode", "cmdline", "networkNamespace", "cgroup"}
    for role in ("application", "endpoint"):
        row = value["process"][role]
        require(isinstance(row, dict) and set(row) == process_fields | ({"listener"} if role == "endpoint" else set()) and
                type(row["pid"]) is int and row["pid"] > 1 and type(row["uid"]) is int and row["uid"] >= 0 and
                type(row["exeDevice"]) is int and type(row["exeInode"]) is int and row["exeInode"] > 0 and
                isinstance(row["startTicks"], str) and row["startTicks"].isdigit() and
                isinstance(row["cmdline"], list) and row["cmdline"] and all(isinstance(part, str) for part in row["cmdline"]),
                "Each explicit process needs metadata-only identity fields.")
    require(value["process"]["application"]["pid"] != value["process"]["endpoint"]["pid"] and
            value["process"]["application"]["networkNamespace"] != value["process"]["endpoint"]["networkNamespace"] and
            value["process"]["workerNetworkNamespace"] == value["process"]["endpoint"]["networkNamespace"],
            "The original namespace must remain separate from the existing proxy and recorder.")
    listener = value["process"]["endpoint"]["listener"]
    require(set(listener) == {"port", "socketInode"} and listener["port"] == 18197 and
            isinstance(listener["socketInode"], str) and re.fullmatch(r"[1-9][0-9]*", listener["socketInode"]), "The proxy listener identity is missing.")
    require(set(value["lock"]) == {"path", "device", "inode"} and all(type(value["lock"][key]) is int and value["lock"][key] > 0
            for key in ("device", "inode")), "The existing lock needs its exact device/inode identity.")
    require(set(value["sources"]) == {"preparation", "transport", "matrix", "proxy"}, "The four owned source descriptors are required.")
    require(set(value["inputs"]) == {"release", "publicBaseline", "credentials", "mediaApproval"}, "The four actual preparation inputs are required.")
    for row in (*value["sources"].values(), *value["inputs"].values()):
        descriptor(row)
    for role, filename in (("preparation", "prepare-nextup-global-reference.py"), ("transport", "nextup-global-transport.py"), ("matrix", "nextup-global-matrix.py")):
        require(Path(value["sources"][role]["path"]).name == filename, "Only the named owned preparation/transport/planner Python sources may be loaded.")
    require(value["sources"]["transport"]["sha256"] == TRANSPORT_SHA256,
            "The four-calibration transport consumer must match its reviewed source.")
    require(set(value["scope"]) == {"fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot", "evidenceParent", "outputRoot", "matrixEvidenceRoot"},
            "Every source/output/fixture authority root is required.")
    for path in value["scope"].values():
        absolute(path)
    inside(value["lock"]["path"], value["scope"]["fixtureRoot"])
    output = inside(value["scope"]["outputRoot"], value["scope"]["fixtureRoot"])
    future = inside(value["scope"]["matrixEvidenceRoot"], value["scope"]["evidenceParent"])
    require(future.parent == Path(value["scope"]["evidenceParent"]) and output != future and
            output not in future.parents and future not in output.parents, "Preparation and future matrix evidence roots must be separate.")
    require(isinstance(value["sealedRoots"], list) and value["sealedRoots"] and len(set(value["sealedRoots"])) == len(value["sealedRoots"]),
            "The complete sealed-root exclusions must be explicit.")
    for entry in value["sealedRoots"]:
        sealed = absolute(entry)
        require(all(path != sealed and sealed not in path.parents and path not in sealed.parents for path in (output, future)),
                "A new output overlaps a sealed historical root.")
    require(isinstance(value["forbiddenOriginalRoots"], list) and value["forbiddenOriginalRoots"] and
            len(set(value["forbiddenOriginalRoots"])) == len(value["forbiddenOriginalRoots"]), "Original implementation/data exclusions must be explicit.")
    forbidden = [absolute(root) for root in value["forbiddenOriginalRoots"]]
    original_executable = absolute(value["process"]["application"]["exe"])
    require(any(original_executable == root or root in original_executable.parents for root in forbidden), "The original executable must be excluded from all byte-reading scopes.")
    application_command = value["process"]["application"]["cmdline"]
    if "-programdata" in application_command:
        require(application_command.count("-programdata") == 1 and application_command.index("-programdata") + 1 < len(application_command), "The original data-root metadata is ambiguous.")
        data_root = absolute(application_command[application_command.index("-programdata") + 1])
        require(any(data_root == root or root in data_root.parents for root in forbidden), "The original server data root must be excluded from byte reads.")
    def outside_original(path):
        candidate = absolute(path)
        require(all(candidate != root and root not in candidate.parents for root in forbidden), "An owned input selected original implementation or database bytes.")
    for path in value["scope"].values():
        outside_original(path)
    for row in (*value["sources"].values(), *value["inputs"].values()):
        outside_original(row["path"])
    for name, row in value["inputs"].items():
        candidate = Path(row["path"])
        require(candidate.suffix == ".json" and (Path(value["scope"]["inputRoot"]) in candidate.parents or
                name == "publicBaseline" and any(Path(root) in candidate.parents for root in value["sealedRoots"])),
                "An input receipt must be inside the owned input root or the explicitly sealed public baseline root.")
    for role, row in value["sources"].items():
        inside(row["path"], value["scope"]["proxySourceRoot" if role == "proxy" else "sourceRoot"])
    proxy = value["sources"]["proxy"]["path"]
    command = value["process"]["endpoint"]["cmdline"]
    require(proxy.endswith(".py") and proxy in command, "The endpoint must bind the existing owned Python proxy source.")
    for flag, expected in (("--reference-pid", str(value["process"]["application"]["pid"])),
                           ("--reference-start-ticks", value["process"]["application"]["startTicks"])):
        require(command.count(flag) == 1 and command.index(flag) + 1 < len(command) and command[command.index(flag) + 1] == expected,
                "The existing proxy command does not bind the original process.")
    require(set(value["media"]) == {"source", "ownedRoot", "approvedReceipt", "approvedReceiptSha256", "roots"} and sha(value["media"]["approvedReceiptSha256"]),
            "An approved owned synthetic source receipt is required.")
    descriptor(value["media"]["approvedReceipt"])
    require(value["media"]["approvedReceipt"]["sha256"] == value["media"]["approvedReceiptSha256"], "The approved media receipt descriptor and digest differ.")
    inside(value["media"]["approvedReceipt"]["path"], value["media"]["ownedRoot"])
    outside_original(value["media"]["approvedReceipt"]["path"])
    require(Path(value["media"]["approvedReceipt"]["path"]).suffix == ".json", "Only the owned synthetic media JSON manifest may approve copying.")
    source = value["media"]["source"]
    require(set(source) == {"path", "sha256", "sizeBytes", "device", "inode", "uid", "gid", "mode", "nlink", "mtimeNs", "ctimeNs"} and
            sha(source["sha256"]) and all(type(source[key]) is int for key in source if key not in ("path", "sha256")) and
            0 < source["sizeBytes"] <= 128 * 1024 * 1024 and source["uid"] == source["gid"] == 0 and source["nlink"] >= 1,
            "The source requires current owned synthetic byte and file-identity evidence.")
    absolute(source["path"])
    inside(source["path"], value["media"]["ownedRoot"])
    outside_original(source["path"])
    require(Path(source["path"]).suffix.lower() == ".mp4", "Only an explicitly approved owned synthetic MP4 may be copied.")
    require(set(value["media"]["roots"]) == {"LA", "LB"}, "Two independent destination roots are required.")
    for key in ("LA", "LB"):
        require(value["media"]["roots"][key] == str(output / "media" / key), "Only the new exact media child roots may be written.")
    require(set(value["actors"]) == {"admin", "P", "Q"}, "One existing administrator and two new actors are required.")
    for role, row in value["actors"].items():
        require(set(row) == ({"userId", "username", "credentialRef", "deviceId"} if role == "admin" else
                            {"username", "credentialRef", "deviceId", "matrixDeviceId"}), "An actor has an unexpected credential/device shape.")
        require(all(identifier(row[key]) for key in row if key != "username") and isinstance(row["username"], str) and
                re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9 _-]{0,95}", row["username"]), "Actor identity fields must be explicit and bounded.")
    for field in ("username", "credentialRef", "deviceId"):
        require(len({row[field].casefold() for row in value["actors"].values()}) == 3, "Preparation actor identities must be independent.")
    devices = [value["actors"][role]["deviceId"] for role in ("admin", "P", "Q")] + [value["actors"][role]["matrixDeviceId"] for role in ("P", "Q")]
    require(len(set(devices)) == 5, "Preparation and matrix device IDs must be distinct.")
    require(set(value["libraries"]) == {"LA", "LB"}, "Exactly two new TV library descriptions are required.")
    for row in value["libraries"].values():
        require(set(row) == {"name", "seriesName"} and all(isinstance(name, str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9 _-]{0,95}", name)
                for name in row.values()), "Stable safe library and series names must be explicit.")
    for key in ("name", "seriesName"):
        require(value["libraries"]["LA"][key].casefold() != value["libraries"]["LB"][key].casefold(), "Library and series names must be distinct.")
    preserve = value["preservation"]
    require(set(preserve) == {"userIds", "libraryIds", "detailRoutes"} and len(preserve["userIds"]) == 8 and len(preserve["libraryIds"]) == 10 and
            len(set(preserve["userIds"])) == 8 and len(set(preserve["libraryIds"])) == 10 and
            all(identifier(item) for item in preserve["userIds"] + preserve["libraryIds"]) and
            value["actors"]["admin"]["userId"] in preserve["userIds"] and len(preserve["detailRoutes"]) == 12,
            "The frozen eight-user/ten-library and twelve-detail preservation scope is required.")
    for row in preserve["detailRoutes"]:
        require(set(row) == {"group", "userId", "itemId"} and identifier(row["group"]) and row["userId"] in preserve["userIds"] and identifier(row["itemId"]),
                "Each old full-detail route needs its exact actor and item.")
    require(len({(row["group"], row["itemId"]) for row in preserve["detailRoutes"]}) == 12, "Old detail routes cannot repeat.")
    limits = {"requestSeconds": (1, 15), "normalSeconds": (1, 1200), "cleanupSeconds": (1, 600),
              "requestBytes": (1024, 32768), "responseBytes": (1024, 1024 * 1024),
              "totalResponseBytes": (4096, 384 * 1024 * 1024), "cleanupResponseBytes": (1024, 101 * 1024 * 1024)}
    for name in ("budgets", "matrixBudgets"):
        budget = value[name]
        require(isinstance(budget, dict) and set(budget) == set(limits), "Every finite time and byte budget must be explicit.")
        for key, (low, high) in limits.items():
            require(type(budget[key]) is int and low <= budget[key] <= high, "A budget exceeds its bound: " + key)
        reserve = CLEANUP_RESERVE if name == "budgets" else 80
        require(budget["cleanupResponseBytes"] >= reserve * (budget["responseBytes"] + 1) and
                budget["totalResponseBytes"] > budget["cleanupResponseBytes"], "The separate cleanup byte reserve cannot cover its bounded response slots.")
    require(value["matrixBudgets"]["totalResponseBytes"] <= 128 * 1024 * 1024 and
            value["matrixBudgets"]["cleanupResponseBytes"] <= 80 * 1024 * 1024,
            "Matrix transport budgets must satisfy its separate reviewed bounds.")
    require(type(value["lifecycleSeparationSeconds"]) is int and value["lifecycleSeparationSeconds"] in (2, 3, 4, 5), "Playback separation must be two to five seconds.")
    return value


def frozen_plan(manifest):
    value = validate_manifest(manifest)
    return {"schemaVersion": 1, "classification": "planned preparation; no execution", "runId": value["runId"],
            "manifestSha256": digest(canonical(value).encode()), "maximumRequests": MAX_REQUESTS,
            "normalLimit": NORMAL_LIMIT, "cleanupReserve": CLEANUP_RESERVE, "phaseMaximums": deepcopy(PHASE_LIMITS),
            "normalMaximum": 257, "successMaximumIncludingLogout": 263, "cleanupMaximum": 89,
            "calibrations": [list(row) for row in CALIBRATIONS], "scanRounds": 12,
            "terminal": "awaiting_independent_attestation", "catalogDisposition": "retained"}


def file_identity(info):
    return {"sizeBytes": info.st_size, "device": info.st_dev, "inode": info.st_ino, "uid": info.st_uid,
            "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode), "nlink": info.st_nlink,
            "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}


def protected(path, *, directory=False, private=False, links=False):
    path = absolute(str(path))
    for parent in (path, *path.parents):
        info = parent.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == info.st_gid == 0,
                "An authority path contains a symlink or is not root-owned.")
        require(not info.st_mode & 0o022 or (parent != path and info.st_mode & stat.S_ISVTX), "An authority path is writable by another owner.")
        if parent == path:
            require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "An authority path has the wrong type.")
            require(not private or not info.st_mode & 0o077, "A private authority input is not owner-only.")
            require(directory or links or info.st_nlink == 1, "An authority file has unexpected hard links.")
        else:
            require(stat.S_ISDIR(info.st_mode), "An authority ancestor is not a directory.")
    return path.lstat()


def read_owned(path, *, private=False, links=False, maximum=16 * 1024 * 1024):
    expected = protected(path, private=private, links=links)
    require(expected.st_size <= maximum, "An owned input exceeds its bound.")
    descriptor_fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor_fd, "rb") as stream:
        require(file_identity(os.fstat(stream.fileno())) == file_identity(expected), "An input changed while opening.")
        raw = stream.read(maximum + 1)
        require(len(raw) <= maximum and file_identity(os.fstat(stream.fileno())) == file_identity(expected), "An input changed while reading.")
    require(file_identity(Path(path).lstat()) == file_identity(expected), "An input path changed during reading.")
    return raw


def load_owned(source, label):
    raw = read_owned(source["path"])
    require(digest(raw) == source["sha256"], "A reviewed owned source digest differs.")
    name = label + "_" + source["sha256"]
    module = importlib.util.module_from_spec(importlib.util.spec_from_file_location(name, source["path"]))
    sys.modules[name] = module
    exec(compile(raw, source["path"], "exec"), module.__dict__)
    return module


def validate_public_baseline(value, manifest):
    keys = {"marker", "version", "captured_at", "server", "roster", "configuration", "libraries", "catalog_by_library",
            "items_by_user", "preferences", "details", "devices", "credential_context"}
    require(isinstance(value, dict) and set(value) == keys and isinstance(value["marker"], str) and 0 < len(value["marker"]) <= 128 and
            type(value["version"]) is int and value["version"] == 1, "The complete retained public baseline schema is required before output or HTTP.")
    instant(value["captured_at"])
    for key in keys - {"marker", "version", "captured_at"}:
        require(isinstance(value[key], dict), "A complete public baseline document is missing: " + key)
    users, libraries = set(manifest["preservation"]["userIds"]), set(manifest["preservation"]["libraryIds"])
    require(set(value["roster"]) == set(value["items_by_user"]) == set(value["preferences"]) == users and
            set(value["libraries"]) == set(value["catalog_by_library"]) == libraries, "The retained public baseline has another frozen population.")
    require(value["server"].get("Id") == manifest["server"]["id"] and value["server"].get("Version") == manifest["server"]["version"],
            "The retained full public baseline identifies another server.")
    for user, row in value["roster"].items():
        require(isinstance(row, dict) and row.get("Id") == user and isinstance(row.get("Name"), str) and isinstance(row.get("Policy"), dict) and
                isinstance(row.get("Configuration"), dict), "The retained account profile is incomplete.")
    for library, row in value["libraries"].items():
        require(isinstance(row, dict) and row.get("ItemId") == library and isinstance(row.get("Name"), str) and
                isinstance(row.get("Locations"), list) and isinstance(row.get("LibraryOptions"), dict), "The retained library definition is incomplete.")
    catalog_ids = set()
    for rows in value["catalog_by_library"].values():
        require(isinstance(rows, dict) and all(identifier(key) and isinstance(row, dict) and row.get("Id") == key for key, row in rows.items()),
                "A retained old catalog projection is malformed.")
        catalog_ids.update(rows)
    require(0 < len(catalog_ids) <= 256, "The retained complete old catalog exceeds its bound.")
    for rows in value["items_by_user"].values():
        require(isinstance(rows, dict) and set(rows) <= catalog_ids and all(isinstance(row, dict) and row.get("Id") == key for key, row in rows.items()),
                "A retained per-user projection is incomplete or escaped its item scope.")
    detail_keys = {(group, item) for group, rows in value["details"].items() if isinstance(rows, dict) for item in rows}
    expected = {(row["group"], row["itemId"]) for row in manifest["preservation"]["detailRoutes"]}
    require(detail_keys == expected and all(isinstance(rows, dict) and all(isinstance(row, dict) and row.get("Id") == item for item, row in rows.items())
            for rows in value["details"].values()), "All twelve retained explicit detail witnesses are required.")
    require(len(value["devices"]) <= 256 and all(identifier(key) and isinstance(row, dict) and row.get("Id") == key and
            isinstance(row.get("ReportedDeviceId"), str) and 0 < len(row["ReportedDeviceId"]) <= 512 for key, row in value["devices"].items()) and
            len({row["ReportedDeviceId"] for row in value["devices"].values()}) == len(value["devices"]),
            "The retained device registry is malformed or repeats a reported identity.")
    context = value["credential_context"]
    require(set(context) == {"channel", "authenticated_user_id", "token_sha256", "user_id_semantics"} and context["channel"] == "controller_api" and
            context["authenticated_user_id"] == manifest["actors"]["admin"]["userId"] and context["user_id_semantics"] == "subject_projection" and sha(context["token_sha256"]),
            "The retained baseline must preserve its own original administrator-token attribution.")
    reported_devices = {row["ReportedDeviceId"] for row in value["devices"].values()}
    new_devices = {row["deviceId"] for row in manifest["actors"].values()} | {manifest["actors"][actor]["matrixDeviceId"] for actor in ("P", "Q")}
    require(not reported_devices.intersection(new_devices), "Preparation and matrix devices must be new to the retained public registry.")
    require(value["roster"][manifest["actors"]["admin"]["userId"]]["Name"] == manifest["actors"]["admin"]["username"] and
            all(row["Name"].casefold() not in {manifest["actors"][actor]["username"].casefold() for actor in ("P", "Q")} for row in value["roster"].values()),
            "The retained roster does not establish the existing admin and two absent new account names.")
    return value


class Authority:
    """Verify actual source, release, closure, lock, and owned-media inputs."""

    def __init__(self, manifest):
        self.manifest = validate_manifest(manifest)
        self.frozen = canonical(self.manifest)
        self.lock_fd = None
        require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
                "Prepare must run through authorized root SSH with Python -I -B.")
        require(Path(self.manifest["sources"]["preparation"]["path"]) == Path(__file__).absolute(), "Preparation source authority does not bind this file.")
        for row in self.manifest["sources"].values():
            require(digest(read_owned(row["path"])) == row["sha256"], "A frozen source changed.")
        self.support = load_owned(self.manifest["sources"]["transport"], "preparation_transport")
        self.planner = load_owned(self.manifest["sources"]["matrix"], "preparation_matrix")
        self.records = {}
        for name, row in self.manifest["inputs"].items():
            raw = read_owned(row["path"], private=True)
            require(digest(raw) == row["sha256"], "An actual preparation input changed.")
            self.records[name] = strict_json(raw)
        self.release = self.records["release"]
        self.baseline = self.records["publicBaseline"]
        self._inputs()
        for key in ("fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot", "evidenceParent"):
            protected(self.manifest["scope"][key], directory=True)
        protected(Path(self.manifest["scope"]["outputRoot"]).parent, directory=True)
        for key in ("outputRoot", "matrixEvidenceRoot"):
            require(not os.path.lexists(self.manifest["scope"][key]), "An evidence root already exists and cannot be resumed.")
        source = self.manifest["media"]["source"]
        require(file_identity(protected(source["path"], links=True)) == {key: val for key, val in source.items() if key not in ("path", "sha256")},
                "The freshly approved synthetic source identity changed.")
        require(digest(read_owned(source["path"], links=True, maximum=128 * 1024 * 1024)) == source["sha256"], "The owned synthetic bytes differ.")

    def _evidence(self, row, *, sealed=False):
        descriptor(row)
        candidate = Path(row["path"])
        roots = self.manifest["sealedRoots"] if sealed else [self.manifest["scope"]["inputRoot"], *self.manifest["sealedRoots"]]
        require(candidate.suffix == ".json" and any(Path(root) in candidate.parents for root in roots) and
                all(candidate != Path(root) and Path(root) not in candidate.parents for root in self.manifest["forbiddenOriginalRoots"]),
                "A historical proof escaped its exact owned JSON evidence roots.")
        raw = read_owned(row["path"], private=True)
        require(digest(raw) == row["sha256"], "A sealed or closed-token proof changed.")
        if not hasattr(self, "evidence_identities"):
            self.evidence_identities = {}
        self.evidence_identities[row["path"]] = file_identity(protected(row["path"], private=True))
        return strict_json(raw)

    def _inputs(self):
        value, release = self.manifest, self.release
        require(set(release) == {"schemaVersion", "kind", "runId", "process", "lock", "sealedRoots", "releasedAt", "sealed", "units", "closedAuthentication", "grantVerification"} and
                release["schemaVersion"] == 2 and release["kind"] == "nextup-global-preparation-release" and release["runId"] == value["runId"] and
                same(release["process"], value["process"]) and same(release["lock"], value["lock"]) and
                same(release["sealedRoots"], value["sealedRoots"]), "The release proof does not bind this exact new run.")
        instant(release["releasedAt"])
        require(isinstance(release["sealed"], list) and len(release["sealed"]) == 2 and isinstance(release["units"], list) and len(release["units"]) == 2,
                "Sealed terminal/inventory and concrete completed worker/controller unit proofs are required.")
        sealed_values, sealed_descriptors = {}, {}
        for row in release["sealed"]:
            require(set(row) == {"kind", "record"} and row["kind"] in ("terminal", "inventory") and row["kind"] not in sealed_values,
                    "One explicit sealed terminal and one inventory are required.")
            sealed_values[row["kind"]] = self._evidence(row["record"], sealed=True)
            sealed_descriptors[row["kind"]] = row["record"]
        terminal, inventory = sealed_values["terminal"], sealed_values["inventory"]
        require(isinstance(terminal, dict) and terminal.get("status") == "protocol_observation_complete_independently_confirmed" and
                all(terminal.get(key) is True for key in ("recursive_cgroups_empty", "full_target_restored_except_etag",
                    "administrator_logout204_same_token401_verified", "viewer_logout204_same_token401_verified", "media_unchanged")) and
                terminal.get("reference_database_read") is False and terminal.get("original_implementation_bytes_read") is False and
                terminal.get("scope_inventory") == sealed_descriptors["inventory"] and isinstance(terminal.get("systemd"), dict) and
                isinstance(inventory, (dict, list)) and inventory, "The actual sealed v4 terminal does not establish its completed preserved inventory.")
        require(instant(terminal.get("captured_at")) <= instant(release["releasedAt"]), "The release receipt predates its sealed terminal observation.")
        require(len({row.get("name") for row in release["units"]}) == 2 and {row.get("name") for row in release["units"]} == set(terminal["systemd"]),
                "The two concrete completed units must match the actual sealed terminal.")
        for row in release["units"]:
            require(set(row) == {"name", "invocationId", "properties", "cgroupPath"} and re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", row["name"]) and
                    re.fullmatch(r"[0-9a-f]{32}", row["invocationId"]) and set(row["properties"]) == {"ActiveState", "SubState", "MainPID", "Result", "ControlGroup"} and
                    row["properties"]["MainPID"] == "0" and row["properties"]["Result"] == "success" and
                    (row["properties"]["ActiveState"], row["properties"]["SubState"]) in (("active", "exited"), ("inactive", "dead")),
                    "A concrete terminal unit fact is missing.")
            previous = terminal["systemd"][row["name"]]
            require(previous.get("InvocationID") == row["invocationId"] and all(previous.get(key) == row["properties"][key] for key in
                    ("ActiveState", "SubState", "MainPID", "Result")), "A unit proof differs from its sealed invocation and terminal state.")
            control_group = row["properties"]["ControlGroup"]
            expected_group = "/system.slice/" + row["name"]
            require(control_group in ("", expected_group) and row["cgroupPath"] == "/sys/fs/cgroup" + expected_group,
                    "An empty-cgroup proof cannot select another unit's cgroup.")
        validate_public_baseline(self.baseline, value)
        require(isinstance(release["closedAuthentication"], list), "Recorded prior logout windows must be explicit.")
        for row in release["closedAuthentication"]:
            require(set(row) in ({"userId", "reportedDeviceId", "tokenSha256", "from", "through", "logout", "rejection"},
                                {"userId", "reportedDeviceId", "tokenSha256", "from", "through", "logout", "rejection", "login"}) and
                    row["userId"] in value["preservation"]["userIds"] and identifier(row["reportedDeviceId"]) and sha(row["tokenSha256"]) and
                    instant(row["from"]) <= instant(row["through"]), "A prior closed authentication window is malformed.")
            if "login" in row:
                continue
            for name, expected_status in (("logout", 204), ("rejection", 401)):
                proof = row[name]
                require(set(proof) == {"record", "intent", "status", "tokenSha256", "completedAt"} and type(proof["status"]) is int and proof["status"] == expected_status and
                        proof["tokenSha256"] == row["tokenSha256"] and instant(row["from"]) <= instant(proof["completedAt"]) <= instant(row["through"]),
                        "A prior closed token lacks its exact logout/rejection facts.")
                response, intent = self._evidence(proof["record"]), self._evidence(proof["intent"])
                require(isinstance(response, dict) and response.get("complete") is True and type(response.get("status")) is int and
                        response["status"] == expected_status and response.get("token_sha256") == row["tokenSha256"] and
                        response.get("completed_at") == proof["completedAt"], "The actual historical response does not prove this completed token/status/time.")
                require(isinstance(intent, dict) and intent.get("channel") == "controller_api" and
                        intent.get("method") == ("POST" if name == "logout" else "GET") and
                        intent.get("path") in (("/emby/Sessions/Logout",) if name == "logout" else ("/emby/System/Info", "/emby/Sessions")),
                        "A historical authentication proof has a different actual method or route.")
            require(instant(row["logout"]["completedAt"]) <= instant(row["rejection"]["completedAt"]) and
                    instant(row["through"]) <= instant(release["releasedAt"]) and
                    row["logout"]["record"]["sha256"] != row["rejection"]["record"]["sha256"] and
                    any(item.get("ReportedDeviceId") == row["reportedDeviceId"] and item.get("LastUserId") == row["userId"] for item in self.baseline["devices"].values()),
                    "A prior closed-token window does not bind one retained device and ordered distinct responses.")
        self._grant_verification()
        credential_record = self.records["credentials"]
        require(set(credential_record) == {"schemaVersion", "runId", "accounts"} and credential_record["schemaVersion"] == 1 and
                credential_record["runId"] == value["runId"] and set(credential_record["accounts"]) == {"admin", "P", "Q"},
                "A private pre-stored three-account credential input is required.")
        self.credentials = credential_record["accounts"]
        for role, record in self.credentials.items():
            fields = {"username", "credentialRef", "password"} | ({"userId"} if role == "admin" else set())
            require(set(record) == fields and all(record[key] == value["actors"][role][key] for key in fields - {"password"}) and
                    isinstance(record["password"], str) and len(record["password"]) >= 32, "A private actor credential differs.")
        require(len({row["password"] for row in self.credentials.values()}) == 3, "All three preparation credentials must be independent.")
        approval = self.records["mediaApproval"]
        require(approval == {"schemaVersion": 1, "kind": "nextup-global-preparation-media-approval", "source": value["media"]["source"],
                "ownedRoot": value["media"]["ownedRoot"], "approvedReceipt": value["media"]["approvedReceipt"],
                "approvedReceiptSha256": value["media"]["approvedReceiptSha256"], "durationSeconds": 600, "frameRate": 30,
                "originalImplementationBytesRead": False}, "The current owned media approval is incomplete or differs.")
        descriptor_row = value["media"]["approvedReceipt"]
        raw = read_owned(descriptor_row["path"])
        require(digest(raw) == descriptor_row["sha256"], "The actual approved synthetic media manifest is missing or changed.")
        media_manifest = strict_json(raw)
        relative = str(Path(value["media"]["source"]["path"]).relative_to(value["media"]["ownedRoot"]))
        require(isinstance(media_manifest, dict) and media_manifest.get("marker") == "goby-client-media-m3e-v1" and
                media_manifest.get("movieProfile") == {"durationSeconds": 600, "fps": 30, "video": "h264", "audio": "aac"} and
                isinstance(media_manifest.get("files"), dict) and media_manifest["files"].get(relative) == value["media"]["source"]["sha256"],
                "The actual approved media manifest does not bind these source bytes and profile.")

    def _grant_verification(self):
        """Reconcile the accepted Guid experiment against all actual raw attempts."""
        independent = self._evidence(self.release["grantVerification"], sealed=True)
        require(independent.get("schemaVersion") == 1 and independent.get("kind") == "nextup-folder-grant-independent-terminal" and
                independent.get("status") == "folder_guid_grants_independently_verified_and_policy_restored" and
                independent.get("referenceCounts") == {"users": 8, "libraries": 10, "devices": 93, "detailWitnesses": 12} and
                independent.get("afterPublic") == self.manifest["inputs"]["publicBaseline"] and
                (independent.get("requestCount"), independent.get("normalRequestCount"), independent.get("cleanupRequestCount")) == (109, 59, 50) and
                instant(independent["capturedAt"]) <= instant(self.release["releasedAt"]),
                "The latest independent Guid terminal does not bind the exact retained population and baseline.")
        require(all(independent.get(key) is True for key in ("allKnownTokensClosed", "originalPolicyRestored", "fullPublicRoundtripPreserved",
                "allPrivateBytesMatched", "allExportBytesMatched", "recursiveCgroupEmpty", "grantSemanticsProven")) and
                independent.get("originalImplementationBytesRead") is False and independent.get("referenceDatabaseRead") is False,
                "The latest Guid observation lacks completed independent preservation and closure.")
        documents = {key: self._evidence(independent[key], sealed=True) for key in (
            "input", "producerTerminal", "wireIndex", "closedAuthentication", "beforePublic", "afterPublic", "originalPolicy", "targetPolicy", "publicPreservation", "scopeInventory")}
        prior, terminal, index, closures = (documents[key] for key in ("input", "producerTerminal", "wireIndex", "closedAuthentication"))
        source = independent["source"]
        descriptor(source)
        require(source == prior["source"] and source["sha256"] == "70087cdaeae927c3091b5abcfc1df0c9203cdf35e17d28e5222f491bad46a971" and
                Path(source["path"]).suffix == ".py" and any(Path(root) in Path(source["path"]).parents for root in self.manifest["sealedRoots"]) and
                all(Path(root) not in Path(source["path"]).parents for root in self.manifest["forbiddenOriginalRoots"]) and
                digest(read_owned(source["path"])) == source["sha256"], "The successful Guid worker source is not its exact reviewed owned implementation.")
        if not hasattr(self, "evidence_identities"):
            self.evidence_identities = {}
        self.evidence_identities[source["path"]] = file_identity(protected(source["path"]))
        require(prior["runId"] == independent["runId"] == terminal["runId"] == index["runId"] == closures["runId"] and
                prior["process"] == self.manifest["process"] and prior["endpoint"] == self.manifest["endpoint"] and prior["lock"] == self.manifest["lock"] and
                prior["preservation"] == self.manifest["preservation"] and same(documents["afterPublic"], self.baseline),
                "The successful Guid experiment and new preparation have different target or preserved-scope identities.")
        require(terminal.get("status") == "awaiting_independent_attestation" and terminal.get("failure") is None and
                terminal.get("pendingPresent") is False and terminal.get("uncertain") is False and
                all(terminal.get(key) is True for key in ("grantSemanticsProven", "originalPolicyRestored", "publicPreserved", "allKnownTokensClosed")) and
                (terminal.get("requestCount"), terminal.get("normalRequestCount"), terminal.get("cleanupRequestCount")) == (109, 59, 50),
                "The original Guid worker did not close its actual attempt sequence.")
        root = Path(prior["outputRoot"])
        require(index.get("schemaVersion") == 1 and index.get("kind") == "nextup-folder-grant-wire-index" and index.get("root") == str(root) and
                isinstance(index.get("requests"), list) and len(index["requests"]) == 109 and
                closures.get("schemaVersion") == 1 and closures.get("kind") == "nextup-folder-grant-authentication-closures" and
                isinstance(closures.get("closures"), list) and [row.get("actor") for row in closures["closures"]] == ["P", "admin"],
                "The complete Guid ledger and two actual authentication closures are required.")
        ledger, tokens, sessions, previous, charges, phase_counts = {}, {}, set(), None, 0, {"normal": 0, "cleanup": 0}
        for ordinal, row in enumerate(index["requests"], 1):
            require(set(row) == {"ordinal", "label", "intent", "response"} and row["ordinal"] == ordinal and
                    isinstance(row["label"], str) and re.fullmatch(r"[A-Za-z0-9_-]{1,88}", row["label"]) and row["label"] not in ledger,
                    "The Guid index must cover each ordinal and label once.")
            for kind in ("intent", "response"):
                require(row[kind]["path"] == str(root / "private" / ("%04d-%s-%s.json" % (ordinal, row["label"], kind))),
                        "A Guid wire descriptor escaped its exact historical attempt path.")
            intent, response = (self._evidence(row[kind], sealed=True) for kind in ("intent", "response"))
            require(intent.get("ordinal") == response.get("ordinal") == ordinal and intent.get("label") == response.get("label") == row["label"] and
                    intent.get("actor") == response.get("actor") and intent["actor"] in ("admin", "P") and
                    intent.get("phase") == ("normal" if ordinal <= 59 else "cleanup") and same(intent.get("request"), response.get("request")) and
                    response.get("completeHttp") is True and response.get("failure") is None and response.get("retainedRawTruncated") is False and
                    type(response.get("status")) is int and 100 <= response["status"] <= 599,
                    "A raw Guid response is incomplete or disagrees with its reserved request.")
            raw = base64.b64decode(response["rawBase64"], validate=True)
            require(len(raw) == response.get("observedRawBytes") <= 262144 and instant(intent["createdAt"]) <= instant(response["completedAt"]) and
                    (previous is None or previous <= instant(intent["createdAt"])) and instant(response["completedAt"]) <= instant(independent["capturedAt"]),
                    "A Guid response exceeded its byte bound or chronological attempt window.")
            headers = response["headers"]
            require(len(self.support.bounded_header_prefix(headers)) == len(headers), "A Guid response header collection exceeded its bound.")
            lengths = [item for key, item in headers if key.lower() == "content-length"]
            transfers = [item for key, item in headers if key.lower() == "transfer-encoding"]
            require(not lengths or len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(raw) and not transfers,
                    "A completed Guid response has inconsistent HTTP framing.")
            required_json = response["status"] == 200 and (intent["request"]["method"] == "GET" or
                            intent["request"]["route"] == "/emby/Users/AuthenticateByName")
            body = (strict_json(raw) if required_json else raw.decode("utf-8", errors="strict")) if raw else None
            actor, request = intent["actor"], intent["request"]
            require(set(request) == {"method", "route", "headers", "body"} and request["method"] in ("GET", "POST") and
                    request["route"].startswith("/emby/") and not any(char in request["route"] for char in ("\r", "\n")) and
                    len({key.lower() for key, item in request["headers"]}) == len(request["headers"]), "A Guid request has an unreviewed shape or repeated context header.")
            payload = None if intent["payloadBase64"] is None else base64.b64decode(intent["payloadBase64"], validate=True)
            if request["body"] is None:
                require(payload is None, "An empty request recorded unexpected wire payload bytes.")
            elif row["label"] == "login-" + actor:
                require(set(request["body"]) == {"Username", "Pw"} and request["body"]["Username"] == prior["actors"][actor]["username"] and
                        isinstance(request["body"]["Pw"], str) and len(request["body"]["Pw"]) >= 32 and
                        payload == urlencode([("Username", request["body"]["Username"]), ("Pw", request["body"]["Pw"])]).encode(),
                        "The actual login form bytes disagree with the owned actor credential intent and fixed field order.")
            else:
                require(payload == canonical(request["body"]).encode(), "The mutation JSON intent differs from its actual encoded wire payload.")
            auth = [item for key, item in request["headers"] if key.lower() == "authorization"]
            selected = [item for key, item in request["headers"] if key.lower() == "x-emby-token"]
            require(auth == ['Emby Client="Goby Folder Grant Verifier", Device="Linux Fixture Recorder", DeviceId="' +
                    prior["actors"][actor]["deviceId"] + '", Version="1.0"'], "A Guid request substituted another device or client context.")
            if row["label"] == "login-" + actor:
                require(not selected and actor not in tokens and response["status"] == 200 and request["method"] == "POST" and
                        request["route"] == "/emby/Users/AuthenticateByName" and body["ServerId"] == self.manifest["server"]["id"] and
                        body["User"]["Id"] == prior["actors"][actor]["userId"] and body["User"]["Name"] == prior["actors"][actor]["username"] and
                        body["User"]["Policy"]["IsAdministrator"] is (actor == "admin") and
                        body["SessionInfo"]["UserId"] == body["User"]["Id"] and body["SessionInfo"]["DeviceId"] == prior["actors"][actor]["deviceId"] and
                        identifier(body["SessionInfo"].get("Id")) and body["SessionInfo"]["Id"] not in sessions and
                        isinstance(body.get("AccessToken"), str) and body["AccessToken"] and body["AccessToken"] not in tokens.values(),
                        "The Guid login does not acknowledge one distinct exact actor/token/session.")
                tokens[actor] = body["AccessToken"]; sessions.add(body["SessionInfo"]["Id"])
            else:
                require(actor in tokens and selected == [tokens[actor]] and intent.get("tokenSha256") == digest(tokens[actor].encode()),
                        "A Guid request used another actor's actual token.")
            phase_counts[intent["phase"]] += 1
            charges += len(raw); previous = instant(response["completedAt"])
            ledger[row["label"]] = {"index": row, "intent": intent, "response": response, "body": body, "raw": raw}
        require(phase_counts == {"normal": 59, "cleanup": 50} and set(tokens) == {"admin", "P"}, "The actual Guid phase/login counts differ.")
        actual_closed = []
        for row in closures["closures"]:
            actor = row["actor"]
            require(row["userId"] == prior["actors"][actor]["userId"] and row["deviceId"] == prior["actors"][actor]["deviceId"] and
                    row["tokenSha256"] == digest(tokens[actor].encode()) and row.get("exactTokenClosed") is True,
                    "A closure summary does not bind its actual acknowledged login.")
            normalized = {"userId": row["userId"], "reportedDeviceId": row["deviceId"], "tokenSha256": row["tokenSha256"],
                          "from": row["from"], "through": row["through"], "login": row["login"]["response"]}
            for name, method, route, status in (("login", "POST", "/emby/Users/AuthenticateByName", 200),
                    ("logout", "POST", "/emby/Sessions/Logout", 204), ("rejection", "GET", "/emby/Sessions", 401)):
                event = ledger[name + "-" + actor]
                require(row[name] == {key: event["index"][key] for key in ("intent", "response")} and
                        event["intent"]["request"]["method"] == method and event["intent"]["request"]["route"] == route and
                        event["response"]["status"] == status and instant(row["from"]) <= instant(event["response"]["completedAt"]) <= instant(row["through"]),
                        "A real closure response does not match the exact indexed token lifecycle.")
                if name != "login":
                    normalized[name] = {"record": row[name]["response"], "intent": row[name]["intent"], "status": status,
                        "tokenSha256": row["tokenSha256"], "completedAt": event["response"]["completedAt"]}
            require(row["from"] == ledger["login-" + actor]["intent"]["createdAt"] and
                    row["through"] == ledger["rejection-" + actor]["response"]["completedAt"] and
                    ledger["logout-" + actor]["index"]["ordinal"] < ledger["rejection-" + actor]["index"]["ordinal"] and
                    instant(row["through"]) <= instant(self.release["releasedAt"]), "The actual closure window is not exact and ordered.")
            actual_closed.append(normalized)
        require([row for row in self.release["closedAuthentication"] if "login" in row] == actual_closed,
                "The new release closures must be the exact indexed Guid worker facts.")
        original, target = documents["originalPolicy"], documents["targetPolicy"]
        require(set(original) == set(target) and [key for key in sorted(original) if not same(original[key], target[key])] == ["EnabledFolders"] and
                target.get("EnableAllFolders") is False and target.get("IsAdministrator") is False and
                target["EnabledFolders"] == [row["guid"] for row in prior["guidFolders"]], "The proven historical policy change was not only the exact Guid pair.")
        writes = [event for event in ledger.values() if event["intent"]["request"]["method"] not in ("GET",) and
                  event["intent"]["request"]["route"] not in ("/emby/Users/AuthenticateByName", "/emby/Sessions/Logout")]
        require([row["index"]["label"] for row in writes] == ["grant-policy", "restore-policy"] and
                all(row["intent"]["actor"] == "admin" and row["intent"]["request"]["method"] == "POST" and row["response"]["status"] in (200, 204) and
                    row["intent"]["request"]["route"] == "/emby/Users/" + prior["actors"]["P"]["userId"] + "/Policy" for row in writes) and
                same(writes[0]["intent"]["request"]["body"], target) and same(writes[1]["intent"]["request"]["body"], original),
                "Actual wire does not show exactly the two complete policy writes and restoration.")
        for label, expected in (("original-P-policy", original), ("granted-P-policy", target), ("own-P-profile", target), ("restored-P-policy", original)):
            event = ledger[label]
            require(event["intent"]["actor"] == ("P" if label == "own-P-profile" else "admin") and event["intent"]["request"]["method"] == "GET" and
                    event["intent"]["request"]["route"] == "/emby/Users/" + prior["actors"]["P"]["userId"] and
                    event["response"]["status"] == 200 and event["body"].get("Id") == prior["actors"]["P"]["userId"] and
                    same(event["body"].get("Policy"), expected), "The complete actual policy readback does not confirm the acknowledged mutation.")
        require(set(page(ledger["own-P-views"]["body"])) == {row["libraryId"] for row in prior["guidFolders"]} and
                page(ledger["restored-own-P-views"]["body"]) == {} and
                all(ledger[label]["response"]["status"] == 200 for label in ("own-P-views", "restored-own-P-views")),
                "Own-token views do not demonstrate the grant and its restoration.")
        old_user = prior["actors"]["P"]["userId"]
        for label in ("own-P-views", "restored-own-P-views"):
            require(ledger[label]["intent"]["actor"] == "P" and ledger[label]["intent"]["request"]["method"] == "GET" and
                    ledger[label]["intent"]["request"]["route"] == "/emby/Users/" + old_user + "/Views", "A Views proof used another actor or route.")
        for symbol, folder in zip(("LA", "LB"), prior["guidFolders"]):
            event = ledger["own-P-catalog-" + symbol]
            require(event["intent"]["actor"] == "P" and event["response"]["status"] == 200 and
                    event["intent"]["request"]["route"] == PreparationRunner._query(None, old_user, parent=folder["libraryId"]) and
                    set(page(event["body"])) == set(documents["beforePublic"]["catalog_by_library"][folder["libraryId"]]),
                    "An own-token catalog did not observe the complete intended library item set.")
        for item in EPISODES:
            event = ledger["own-P-detail-" + item]
            body = event["body"]
            witness = documents["beforePublic"]["details"]["grant-P"].get(body.get("Id"))
            require(event["intent"]["actor"] == "P" and event["response"]["status"] == 200 and body.get("Type") == "Episode" and
                    event["intent"]["request"]["route"] == "/emby/Users/" + old_user + "/Items/" + body["Id"] and
                    isinstance(witness, dict) and all(body.get(key) == witness.get(key) for key in
                        ("Id", "Type", "ParentId", "SeriesId", "SeasonId", "IndexNumber", "ParentIndexNumber", "RunTimeTicks")) and
                    body.get("SeasonId") == body.get("ParentId") and same(body["UserData"], witness["UserData"]) and
                    self.planner.zero_state(self.planner.userdata_fact(body)), "The first own-token episode proof differs from its exact full before witness or zero history.")
        snapshot_labels = {"before": [], "after": []}
        for prefix in ("before", "after"):
            snapshot = documents[prefix + "Public"]
            def get(label, actor, route):
                snapshot_labels[prefix].append(label)
                event = ledger[label]
                require(event["intent"]["actor"] == actor == "admin" and event["intent"]["request"]["method"] == "GET" and
                        event["intent"]["request"]["route"] == route and event["response"]["status"] == 200,
                        "A full snapshot document was not observed under its exact administrator token.")
                return deepcopy(event["body"])
            context = SimpleNamespace(manifest={"server": self.manifest["server"], "preservation": prior["preservation"]},
                user_ids={actor: row["userId"] for actor, row in prior["actors"].items()}, libraries={}, tokens=tokens,
                authority=SimpleNamespace(baseline=snapshot), _get=get, _save=lambda name, value: None, utc_now=lambda: snapshot["captured_at"])
            context._query = lambda user, **kwargs: PreparationRunner._query(context, user, **kwargs)
            context._devices = lambda label: devices_page(get(label, "admin", "/emby/Devices"))
            require(same(PreparationRunner._snapshot(context, prefix), snapshot), "The complete public snapshot is not reproduced by its 43 actual raw GETs.")
        expected_labels = ["login-admin", *snapshot_labels["before"], "original-P-policy", "grant-policy", "granted-P-policy", "login-P",
            "own-P-profile", "own-P-views", "own-P-selectable-folders", "own-P-catalog-LA", "own-P-catalog-LB",
            *["own-P-detail-" + item for item in EPISODES], "restore-policy", "restored-P-policy", "restored-own-P-views",
            *snapshot_labels["after"], "logout-P", "rejection-P", "logout-admin", "rejection-admin"]
        require(list(ledger) == expected_labels, "The raw Guid ledger is not the exact ordered 109-request experiment.")
        selectable = ledger["own-P-selectable-folders"]["intent"]
        require(selectable["actor"] == "P" and selectable["request"]["method"] == "GET" and
                selectable["request"]["route"] == "/emby/Library/SelectableMediaFolders", "The independent selectable observation used another principal or endpoint.")
        before, after, changes = documents["beforePublic"], documents["afterPublic"], []
        require(len(before["devices"]) == 92 and len(after["devices"]) == 93 and
                len(after["roster"]) == 8 and len(after["libraries"]) == 10 and sum(map(len, after["details"].values())) == 12,
                "The actual complete Guid snapshots have another retained population.")
        for key in ("server", "configuration", "libraries", "catalog_by_library", "preferences", "items_by_user", "details"):
            require(same(before[key], after[key]), "The actual Guid roundtrip changed a preserved full public section.")
        windows = {row["userId"]: row for row in closures["closures"]}
        require(set(before["roster"]) == set(after["roster"]), "The Guid roundtrip changed the old account roster.")
        for user in sorted(before["roster"]):
            old, current = before["roster"][user], after["roster"][user]
            allowed = {"LastLoginDate", "LastActivityDate"} if user in windows else set()
            require(same({key: val for key, val in old.items() if key not in allowed}, {key: val for key, val in current.items() if key not in allowed}),
                    "The Guid roundtrip changed a retained full account policy or configuration.")
            for key in sorted(allowed):
                if old.get(key) == current.get(key) and (key in old) == (key in current): continue
                require(instant(windows[user]["from"]) <= instant(current[key]) <= instant(after["captured_at"]), "A Guid authentication date escaped its real login window.")
                changes.append({"kind": "owned-authentication-time", "userId": user, "field": key})
        device_windows = {row["deviceId"]: row for row in closures["closures"]}
        require(set(before["devices"]) <= set(after["devices"]), "An old device disappeared during the Guid roundtrip.")
        for key in sorted(after["devices"]):
            current, old = after["devices"][key], before["devices"].get(key)
            if old is not None:
                require(same({key: val for key, val in old.items() if key != "DateLastActivity"},
                             {key: val for key, val in current.items() if key != "DateLastActivity"}), "A retained Guid-roundtrip device structure changed.")
            if old is None or old.get("DateLastActivity") != current.get("DateLastActivity"):
                window = device_windows.get(current.get("ReportedDeviceId"))
                require(window is not None and current.get("LastUserId") == window["userId"] and
                        instant(window["from"]).replace(microsecond=0) <= instant(current["DateLastActivity"]) <= instant(after["captured_at"]),
                        "An unowned device or out-of-window device date changed during the Guid roundtrip.")
                changes.append({"kind": "owned-device-time", "deviceId": key})
        require(documents["publicPreservation"] == {"preserved": True, "allowedAuthenticationChanges": changes,
                "beforeTokenSha256": before["credential_context"]["token_sha256"], "afterTokenSha256": after["credential_context"]["token_sha256"]},
                "The preservation report is not reproduced by the actual full before/after DTOs.")
        unit = independent["unit"]
        properties = unit["properties"]
        expected_group = "/system.slice/" + unit["name"]
        require(re.fullmatch(r"[A-Za-z0-9_.@-]+\.service", unit["name"]) and unit["name"] not in {row["name"] for row in self.release["units"]} and
                re.fullmatch(r"[0-9a-f]{32}", properties["InvocationID"]) and properties["MainPID"] == "0" and properties["Result"] == "success" and
                properties["ExecMainStatus"] == "0" and properties["RemainAfterExit"] == "yes" and
                (properties["ActiveState"], properties["SubState"]) in (("active", "exited"), ("inactive", "dead")) and
                properties["ControlGroup"] in ("", expected_group) and unit["cgroupPath"] == "/sys/fs/cgroup" + expected_group,
                "The latest Guid worker lacks its exact completed invocation and cgroup proof.")
        require(properties.get("Type") == "oneshot" and properties["ExecStart"].count("argv[]=") == 1 and
                shlex.split(properties["ExecStart"].split("argv[]=", 1)[1].split(";", 1)[0].strip()) ==
                ["/usr/bin/python3", "-I", "-B", source["path"], independent["input"]["path"], independent["input"]["sha256"]],
                "The independently completed unit did not execute the exact recorded owned source and input.")
        self.grant_unit = {"name": unit["name"], "invocationId": properties["InvocationID"], "cgroupPath": unit["cgroupPath"],
            "properties": {key: properties[key] for key in ("ActiveState", "SubState", "MainPID", "Result", "ControlGroup", "ExecMainStatus", "RemainAfterExit", "Type", "ExecStart")}}

    def _units(self):
        for row in [*self.release["units"], self.grant_unit]:
            names = ("InvocationID", *row["properties"])
            result = subprocess.run(["/usr/bin/systemctl", "show", row["name"], *[part for name in names for part in ("-p", name)]],
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False,
                                    env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8"})
            require(result.returncode == 0 and len(result.stdout) <= 65536, "A released controller or worker unit cannot be inspected.")
            current = dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)
            require(current == {"InvocationID": row["invocationId"], **row["properties"]}, "A released unit changed identity or became active.")
            root = Path(row["cgroupPath"])
            if root.exists():
                paths = [root, *(path for path in root.rglob("*") if path.is_dir())]
                require(len(paths) <= 128 and all(not stat.S_ISLNK(path.lstat().st_mode) and not (path / "cgroup.procs").read_text().strip() for path in paths),
                        "A released explicit worker/controller cgroup is not empty.")

    def acquire(self):
        require(self.lock_fd is None, "The existing fixture lock cannot be reacquired.")
        lock = self.manifest["lock"]
        info = protected(lock["path"], private=True)
        require((info.st_dev, info.st_ino) == (lock["device"], lock["inode"]), "The existing coordination lock changed.")
        descriptor_fd = os.open(lock["path"], os.O_RDWR | os.O_NOFOLLOW)
        try:
            fcntl.flock(descriptor_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            self.lock_fd = descriptor_fd
            self._units()
            self.check()
        except BaseException:
            os.close(descriptor_fd)
            self.lock_fd = None
            raise

    def check(self):
        require(self.lock_fd is not None and canonical(self.manifest) == self.frozen, "Frozen preparation authority is unavailable.")
        lock = self.manifest["lock"]
        opened, named = os.fstat(self.lock_fd), protected(lock["path"], private=True)
        require((opened.st_dev, opened.st_ino) == (named.st_dev, named.st_ino) == (lock["device"], lock["inode"]), "The held fixture lock was replaced.")
        require(self.support.process_identity(self.manifest["process"]) == self.manifest["process"], "The original metadata or existing proxy identity changed.")
        for row in (*self.manifest["sources"].values(), *self.manifest["inputs"].values()):
            require(digest(read_owned(row["path"])) == row["sha256"], "A frozen owned input changed during preparation.")
        for path, expected in self.evidence_identities.items():
            require(file_identity(protected(path, private=Path(path).suffix != ".py")) == expected,
                    "A previously verified raw release/provenance file changed during preparation.")
        approved = self.manifest["media"]["approvedReceipt"]
        require(digest(read_owned(approved["path"])) == approved["sha256"], "The approved synthetic media manifest changed during preparation.")
        source = self.manifest["media"]["source"]
        require(file_identity(protected(source["path"], links=True)) == {key: val for key, val in source.items() if key not in ("path", "sha256")},
                "The approved owned synthetic source changed.")

    def stage_media(self, journal):
        self.check()
        root = Path(self.manifest["scope"]["outputRoot"])
        require(root == journal.root, "Media staging is outside the exact new journal root.")
        os.mkdir(root / "media", 0o755)
        source = self.manifest["media"]["source"]
        raw = read_owned(source["path"], links=True, maximum=128 * 1024 * 1024)
        require(digest(raw) == source["sha256"], "The approved source changed before copying.")
        result, tree = {}, {}
        def create(path, content):
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
            with os.fdopen(fd, "wb") as stream:
                stream.write(content); stream.flush(); os.fsync(stream.fileno())
            self.support.sync_directory(path.parent)
            observed = read_owned(str(path), maximum=128 * 1024 * 1024)
            require(observed == content, "A newly copied owned file differs.")
            tree[str(path)] = {"sha256": digest(observed), **file_identity(path.stat())}
        for library in ("LA", "LB"):
            media_root = Path(self.manifest["media"]["roots"][library])
            os.mkdir(media_root, 0o755)
            name = self.manifest["libraries"][library]["seriesName"]
            series = media_root / name
            os.mkdir(series, 0o755)
            create(media_root / ".goby-managed", ("nextup-global-" + self.manifest["runId"] + "-" + library + "\n").encode())
            create(series / "tvshow.nfo", ("<tvshow><title>" + name + "</title></tvshow>\n").encode())
            files = {}
            for index, (season, episode) in enumerate(((1, 1), (1, 2), (2, 1)), 1):
                season_root = series / ("Season %02d" % season)
                if not season_root.exists(): os.mkdir(season_root, 0o755)
                stem = "%s S%02dE%02d" % (name, season, episode)
                media_file = season_root / (stem + ".mp4")
                create(media_file, raw)
                create(season_root / (stem + ".nfo"), ("<episodedetails><title>Episode %d-%d</title><season>%d</season><episode>%d</episode></episodedetails>\n" %
                                                      (season, episode, season, episode)).encode())
                files[library[1] + str(index)] = {"path": str(media_file), "sha256": source["sha256"], "sizeBytes": len(raw)}
            result[library] = {"rootPath": str(media_root), "files": files}
        require(len({(row["device"], row["inode"]) for path, row in tree.items() if path.endswith(".mp4")}) == 6,
                "The six new media copies must be independent files.")
        self.check()
        require(digest(read_owned(source["path"], links=True, maximum=128 * 1024 * 1024)) == source["sha256"], "The source changed while staging.")
        journal.save("media-tree.json", {"source": source, "files": tree, "originalRootsModified": False})
        return result

    def close(self):
        if self.lock_fd is not None:
            os.close(self.lock_fd)
            self.lock_fd = None


def library_body(manifest, symbol):
    root = manifest["media"]["roots"][symbol]
    options = {"PathInfos": [{"Path": root}], "SampleIgnoreSize": 0,
               "EnableRealtimeMonitor": False, "EnableChapterImageExtraction": False,
               "ExtractChapterImagesDuringLibraryScan": False, "EnableMarkerDetection": False,
               "EnableMarkerDetectionDuringLibraryScan": False, "DownloadImagesInAdvance": False,
               "SaveLocalMetadata": False, "SaveLocalThumbnailSets": False, "SaveSubtitlesWithMedia": False,
               "SaveLyricsWithMedia": False, "MetadataSavers": [], "SubtitleDownloadLanguages": [],
               "LyricsDownloadLanguages": [], "AutomaticRefreshIntervalDays": 0, "EnableEmbeddedTitles": True,
               "EnableAutomaticSeriesGrouping": False,
               "TypeOptions": [{"Type": name, "MetadataFetchers": [], "MetadataFetcherOrder": [],
                                "ImageFetchers": [], "ImageFetcherOrder": [], "ImageOptions": []}
                               for name in ("Movie", "Series", "Season", "Episode", "MusicArtist", "MusicAlbum", "Audio")]}
    return {"Name": manifest["libraries"][symbol]["name"], "CollectionType": "tvshows", "RefreshLibrary": False,
            "Paths": [root], "LibraryOptions": options}


def page(value, key="Id", maximum=256):
    require(isinstance(value, dict) and isinstance(value.get("Items"), list) and len(value["Items"]) <= maximum and
            type(value.get("TotalRecordCount", len(value["Items"]))) is int and
            value.get("TotalRecordCount", len(value["Items"])) == len(value["Items"]), "A required public list is incomplete or truncated.")
    result = {row[key]: row for row in value["Items"] if isinstance(row, dict) and identifier(row.get(key))}
    require(len(result) == len(value["Items"]), "A public list has repeated or malformed identities.")
    return result


def devices_page(value):
    """Decode only Devices: retained reference replies use a zero count sentinel."""
    require(isinstance(value, dict) and set(value) == {"Items", "TotalRecordCount"} and
            isinstance(value["Items"], list) and len(value["Items"]) <= 256 and
            type(value["TotalRecordCount"]) is int and value["TotalRecordCount"] in (0, len(value["Items"])),
            "The Devices response is not the exact bounded observed count contract.")
    result, reported = {}, set()
    for row in value["Items"]:
        require(isinstance(row, dict) and identifier(row.get("Id")) and row["Id"] not in result and
                isinstance(row.get("ReportedDeviceId"), str) and 0 < len(row["ReportedDeviceId"]) <= 512 and
                row["ReportedDeviceId"] not in reported,
                "The complete Devices population has a missing or duplicate device identity.")
        result[row["Id"]] = row
        reported.add(row["ReportedDeviceId"])
    return result


class PreparationRunner:
    """Single-use preparation with injected authority, journal, wire, and clock."""

    def __init__(self, manifest, *, authority, transport, journal_factory, monotonic=time.monotonic,
                 sleeper=time.sleep, utc_now=lambda: datetime.now(timezone.utc).isoformat()):
        self.manifest = validate_manifest(manifest)
        self.frozen = canonical(self.manifest)
        self.plan = frozen_plan(self.manifest)
        self.plan_sha256 = digest(canonical(self.plan).encode())
        self.authority, self.transport, self.journal_factory = authority, transport, journal_factory
        self.support, self.planner = authority.support, authority.planner
        self.monotonic, self.sleeper, self.utc_now = monotonic, sleeper, utc_now
        self.journal = None
        self.started = None
        self.last_clock = None
        self.last_completed = None
        self.cleanup_started = None
        self.phase = "before"
        self.count = self.normal_count = self.cleanup_count = self.charged_bytes = 0
        self.phase_counts = {key: 0 for key in PHASE_LIMITS}
        self.labels, self.tokens, self.sessions, self.logins, self.revoked = set(), {}, {}, {}, set()
        self.user_ids = {"admin": self.manifest["actors"]["admin"]["userId"]}
        self.created_users, self.libraries, self.items, self.media, self.profiles, self.preferences = {}, {}, {}, {}, {}, {}
        self.baselines, self.summaries = {"P": {}, "Q": {}}, {"P": {}, "Q": {}}
        self.plays, self.touched, self.calibrations, self.responses = {}, set(), [], {}
        self.play_session_ids = set()
        self.ownership_pending = None
        self.pending, self.failure, self.uncertain, self.journal_failed = None, None, False, False
        self.used, self.completed, self.outputs, self.before = False, False, {}, None
        self.catalog_ids = set()
        self.secrets = {row["password"] for row in authority.credentials.values()}
        self.secrets.update(self.manifest["scope"].values())
        self.secrets.update(self.manifest["sealedRoots"])
        self.secrets.update(self.manifest["forbiddenOriginalRoots"])
        self.secrets.add(self.manifest["media"]["ownedRoot"])
        self.secrets.add(self.manifest["media"]["source"]["path"])
        self.secrets.update(row["path"] for row in (*self.manifest["sources"].values(), *self.manifest["inputs"].values()))
        self.secrets.update(path for row in authority.baseline.get("libraries", {}).values() for path in row.get("Locations", []) if isinstance(path, str))
        self.secrets.update(row["deviceId"] for row in self.manifest["actors"].values())
        self.secrets.update(self.manifest["actors"][role]["matrixDeviceId"] for role in ("P", "Q"))
        self._collect_secrets(authority.baseline)

    def _now(self):
        current = self.monotonic()
        require(isinstance(current, (int, float)) and not isinstance(current, bool) and math.isfinite(current) and
                (self.last_clock is None or current >= self.last_clock) and (self.started is None or current >= self.started),
                "The injected monotonic clock is nonfinite or moved backwards.")
        self.last_clock = current
        return current

    def _state(self):
        return {"schemaVersion": 1, "runId": self.manifest["runId"], "manifestSha256": digest(self.frozen.encode()),
                "planSha256": self.plan_sha256, "phase": self.phase, "requestCount": self.count,
                "normalRequestCount": self.normal_count, "cleanupRequestCount": self.cleanup_count,
                "chargedResponseBytes": self.charged_bytes, "pending": self.pending, "tokens": self.tokens,
                "sessions": self.sessions, "userIds": self.user_ids, "libraries": self.libraries, "items": self.items,
                "baselines": self.baselines, "summaries": self.summaries, "plays": self.plays,
                "playSessionIds": sorted(self.play_session_ids), "ownershipPending": self.ownership_pending,
                "touched": [list(row) for row in sorted(self.touched)], "calibrations": self.calibrations,
                "revoked": sorted(self.revoked), "failure": self.failure, "uncertain": self.uncertain}

    def _save(self, name, value, *, export=False):
        try:
            return self.journal.save(name, value, export=export)
        except BaseException:
            self.journal_failed = True
            raise

    def _persist(self):
        try:
            self.journal.state(self._state())
        except BaseException:
            self.journal_failed = True
            raise

    def _safe(self, value):
        return self.support.sanitized(value, self.secrets)

    def _collect_secrets(self, value):
        if isinstance(value, dict):
            if isinstance(value.get("SessionInfo"), dict) and isinstance(value["SessionInfo"].get("Id"), str):
                self.secrets.add(value["SessionInfo"]["Id"])
            if isinstance(value.get("MediaSources"), list):
                self.secrets.update(row["Id"] for row in value["MediaSources"] if isinstance(row, dict) and isinstance(row.get("Id"), str))
            for key, item in value.items():
                if key.lower() in self.support.SECRET_KEYS and isinstance(item, str) and item:
                    self.secrets.add(item)
                self._collect_secrets(item)
        elif isinstance(value, list):
            for item in value:
                self._collect_secrets(item)

    def _enter(self, phase):
        require(self.pending is None and self.ownership_pending is None and not self.uncertain, "A pending or uncertain request cannot advance a phase.")
        self.phase = phase
        self._persist()

    def _guard_route(self, actor, method, route, body, form):
        require(actor in self.manifest["actors"], "A request selected an unowned actor.")
        parsed = urlsplit(route)
        require(not parsed.scheme and not parsed.netloc and not parsed.fragment and "\r" not in route and "\n" not in route,
                "An API route escaped the existing proxy.")
        query_rows = parse_qsl(parsed.query, keep_blank_values=True)
        require(len(query_rows) == len({key.lower() for key, _ in query_rows}), "Repeated query parameters are forbidden.")
        query = dict(query_rows)
        path = parsed.path
        if path == "/emby/Users/AuthenticateByName":
            require(method == "POST" and form and not query and actor not in self.tokens and body == {
                    "Username": self.authority.credentials[actor]["username"], "Pw": self.authority.credentials[actor]["password"]},
                    "Login must use the one pre-stored actor credential as a UTF-8 form.")
            return
        require(actor in self.tokens and actor not in self.revoked and not form, "A request lacks its verified live actor token.")
        if path == "/emby/Sessions/Logout":
            require(method == "POST" and body is None and not query, "Logout must target only the exact owned token.")
            return
        if path == "/emby/Sessions":
            require(method == "GET" and body is None and not query and "logout-" + actor in self.labels,
                    "The session route is only an exact post-logout rejection proof.")
            return
        old_users = set(self.manifest["preservation"]["userIds"])
        user_ids = old_users | set(self.user_ids.values())
        new_ids = set(self.user_ids.values()) - old_users
        library_ids = set(self.manifest["preservation"]["libraryIds"]) | {row["libraryId"] for row in self.libraries.values()}
        new_library_ids = {row["libraryId"] for row in self.libraries.values()}
        if method == "GET":
            require(body is None, "Read requests cannot send a body.")
            if actor == "admin" and path in ("/emby/System/Info/Public", "/emby/Users", "/emby/Library/VirtualFolders/Query", "/emby/Devices", "/emby/System/Configuration"):
                require(not query, "A public preservation document cannot add query flags.")
                return
            if actor == "admin" and path == "/emby/Library/SelectableMediaFolders":
                require(not query and self.phase == "mapping" and set(self.libraries) == {"LA", "LB"},
                        "Selectable folders are only one explicit new-library mapping observation.")
                return
            for user in user_ids:
                require(actor == "admin" or user != self.user_ids.get(actor) or actor in ("P", "Q"), "An ordinary user binding differs.")
                if actor != "admin" and user != self.user_ids[actor]: continue
                if path in ("/emby/Users/" + user, "/emby/usersettings/" + user, "/emby/UserSettings/" + user, "/emby/Users/" + user + "/Views"):
                    require(not query, "Profile/preferences/views queries must remain exact.")
                    return
                if path == "/emby/Users/" + user + "/Items":
                    base = {"Recursive": "true", "Fields": FIELDS, "EnableUserData": "true", "EnableTotalRecordCount": "true", "Limit": "256"}
                    require(all(query.get(key) == item for key, item in base.items()), "A scoped catalog projection differs.")
                    if set(query) == set(base) | {"ParentId"}:
                        require(query["ParentId"] in (library_ids if actor == "admin" else
                                {row["viewId"] for row in self.libraries.values()}), "A catalog read selected an unowned scope.")
                        return
                    if actor == "admin" and set(query) == set(base) | {"Ids"}:
                        require(set(query["Ids"].split(",")) <= self.catalog_ids, "An old-user projection escaped the frozen catalog IDs.")
                        return
                    raise PreparationError("A catalog query combination is unreviewed.")
                prefix = "/emby/Users/" + user + "/Items/"
                if path.startswith(prefix):
                    item = path[len(prefix):]
                    allowed = {row["id"] for row in self.items.values()} | {value for row in self.libraries.values()
                              for key, value in row.items() if key in ("libraryId", "viewId", "sourceRootId")}
                    old_details = {(row["userId"], row["itemId"]) for row in self.manifest["preservation"]["detailRoutes"]}
                    require(not query and (item in allowed or actor == "admin" and (user, item) in old_details), "A full detail escaped its exact owned item set.")
                    return
        if actor == "admin" and method == "POST":
            if path == "/emby/Users/New":
                require(not query and body in [{"Name": self.manifest["actors"][role]["username"]} for role in ("P", "Q")], "An unowned account creation was requested.")
                return
            if path == "/emby/Library/VirtualFolders":
                require(not query and body in [library_body(self.manifest, key) for key in ("LA", "LB")], "A library creation differs from the fixed safe body.")
                return
            for role in ("P", "Q"):
                user = self.user_ids.get(role)
                if user not in new_ids: continue
                if path == "/emby/Users/" + user + "/Password":
                    require(not query and body == {"Id": user, "NewPw": self.authority.credentials[role]["password"], "ResetPassword": False}, "Only the new owned password may be configured.")
                    return
                if path == "/emby/Users/" + user + "/Policy":
                    require(not query and body == self._policy(role), "The new account policy differs from its complete frozen body.")
                    return
            for library in new_library_ids:
                if path == "/emby/Items/" + library + "/Refresh":
                    require(query == {"Recursive": "true", "MetadataRefreshMode": "FullRefresh", "ImageRefreshMode": "ValidationOnly"} and body == {}, "Only the exact new-library refresh is permitted.")
                    return
        if actor in ("P", "Q"):
            pair = (actor, "A1" if actor == "P" else "B1")
            user, item = self.user_ids[actor], self.items.get(pair[1], {}).get("id")
            if path == "/emby/Items/" + str(item) + "/PlaybackInfo":
                require(method == "POST" and not query and body == {"UserId": user, "IsPlayback": True} and
                        all(len(self.baselines[key]) == 6 for key in ("P", "Q")), "Playback negotiation lacks all twelve zero baselines.")
                return
            if path == "/emby/Users/" + user + "/PlayedItems/" + str(item):
                require(method == "DELETE" and body is None and not query and pair in self.touched and
                        all(play["stopped"] for play in self.plays.values()), "Narrow DELETE requires a touched owned episode and all known sessions stopped.")
                return
            if method == "POST" and path in ("/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped"):
                play = self.plays.get(actor)
                require(not query and play is not None and isinstance(body, dict) and all(body.get(key) == val for key, val in play["context"].items()),
                        "A playback report substituted an unacknowledged identity.")
                context = deepcopy(play["context"])
                if path.endswith("Stopped"):
                    expected = {**context, "PositionTicks": play["position"], "Failed": False, "IsAutomated": False}
                else:
                    expected = {**context, "RunTimeTicks": RUNTIME, "PositionTicks": 0, "CanSeek": True,
                                "IsPaused": False, "IsMuted": False, "PlayMethod": "DirectStream", "PlaybackRate": 1}
                    if path.endswith("Progress"): expected.update(PositionTicks=play["target"], EventName="TimeUpdate")
                require(body == expected, "A playback report differs from its fixed acknowledged shape.")
                return
        raise PreparationError("A route is outside the exact preparation allowlist.")

    def _resource_kind(self, method, route):
        if method != "POST": return None
        kinds = {"/emby/Users/AuthenticateByName": "login", "/emby/Users/New": "account", "/emby/Library/VirtualFolders": "library"}
        return kinds.get(route, "playback" if route.endswith("/PlaybackInfo") else None)

    def _ownership_followup(self, label, actor, method, route, body, form):
        pending = self.ownership_pending
        return bool(pending is not None and pending.get("kind") == "library" and not pending.get("followupUsed") and
                    pending.get("allowedFollowup") == {"label": label, "actor": actor, "method": method, "route": route} and
                    actor == "admin" and method == "GET" and route == "/emby/Library/VirtualFolders/Query" and body is None and not form)

    def _complete_ownership(self, event, *, kind, actor, owner):
        pending = self.ownership_pending
        require(self.uncertain and pending is not None and pending["kind"] == kind and pending["actor"] == actor and
                pending["label"] in self.responses and pending["ordinal"] == event["ordinal"] and
                pending["responseReceiptSha256"] == event["responseReceiptSha256"] and isinstance(owner, dict) and owner,
                "Ownership can only be committed for the exact retained creation response.")
        if kind == "library":
            require(pending.get("followupUsed") is True and isinstance(pending.get("reconciliationResponse"), dict),
                    "A library owner cannot be committed without its one actual inventory reconciliation.")
        pending["stage"] = "owner-registered"
        pending["owner"] = deepcopy(owner)
        self._persist()
        retained = deepcopy(pending)
        self.ownership_pending = None
        self.uncertain = False
        try:
            self._persist()
        except BaseException:
            self.ownership_pending = retained
            self.uncertain = True
            raise

    def _dispatch(self, label, actor, method, route, body=None, *, form=False):
        followup = self._ownership_followup(label, actor, method, route, body, form)
        require(self.journal is not None and not self.journal_failed and self.pending is None and
                ((not self.uncertain and self.ownership_pending is None) or followup) and not self.completed,
                "No further dispatch is permitted for a closed or uncertain recorder.")
        require(canonical(self.manifest) == self.frozen and digest(canonical(self.plan).encode()) == self.plan_sha256,
                "The frozen preparation plan changed.")
        require(re.fullmatch(r"[a-zA-Z0-9_-]{1,88}", label) and label not in self.labels, "A request label cannot repeat or escape its journal.")
        self._guard_route(actor, method, route, body, form)
        cleanup = self.phase == "cleanup"
        require(self.count < MAX_REQUESTS and (cleanup or self.normal_count < NORMAL_LIMIT) and
                (not cleanup or self.cleanup_count < CLEANUP_RESERVE) and self.phase_counts[self.phase] < PHASE_LIMITS[self.phase],
                "The exact phase or preparation request budget is exhausted.")
        self.authority.check(); self.journal.check()
        budgets = self.manifest["budgets"]
        deadline = (self.cleanup_started + budgets["cleanupSeconds"] if cleanup else self.started + budgets["normalSeconds"])
        require(self._now() < deadline, "The bounded preparation phase deadline expired.")
        metadata = self.manifest["actors"][actor]
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="Goby NextUp Preparation", Device="Linux Fixture Recorder", DeviceId="' + metadata["deviceId"] + '", Version="1.0"'}
        if actor in self.tokens: headers["X-Emby-Token"] = self.tokens[actor]
        payload = None
        if body is not None:
            payload = (urlencode(body).encode("utf-8") if form else canonical(body).encode("utf-8"))
            headers["Content-Type"] = "application/x-www-form-urlencoded; charset=utf-8" if form else "application/json"
        require(payload is None or len(payload) <= budgets["requestBytes"], "The preparation request body exceeded its bound.")
        available = budgets["totalResponseBytes"] - (0 if cleanup else budgets["cleanupResponseBytes"]) - self.charged_bytes
        require(available >= budgets["responseBytes"] + 1, "Normal work cannot spend the cleanup response-byte reserve.")
        ordinal = self.count + 1
        stem = "%04d-%s" % (ordinal, label)
        request = {"method": method, "route": route, "headers": [[key, val] for key, val in headers.items()], "body": body}
        intent = {"ordinal": ordinal, "label": label, "actor": actor, "phase": self.phase, "planSha256": self.plan_sha256,
                  "request": request, "payloadBase64": None if payload is None else base64.b64encode(payload).decode(),
                  "tokenSha256": digest(self.tokens[actor].encode()) if actor in self.tokens else None,
                  "credentialRef": metadata["credentialRef"]}
        intent_sha = self._save(stem + "-intent.json", intent)
        resource_kind = self._resource_kind(method, route)
        self.pending = {"ordinal": ordinal, "label": label, "actor": actor, "intentSha256": intent_sha, "request": request,
                        "resourceKind": resource_kind, "ownershipFollowup": followup}
        if followup:
            self.ownership_pending["followupUsed"] = True
        self.count += 1; self.normal_count += int(not cleanup); self.cleanup_count += int(cleanup)
        self.phase_counts[self.phase] += 1; self.labels.add(label)
        self._save(stem + "-reserved.json", self._state())
        self._persist()
        self.authority.check(); self.journal.check()
        remaining = deadline - self._now()
        require(remaining > 0, "Persistence consumed the frozen dispatch deadline.")
        try:
            response = self.transport.send(SimpleNamespace(method=method, route=route, label=label, actor=actor, cleanup=cleanup), headers, payload,
                                           timeout_seconds=min(budgets["requestSeconds"], remaining), max_bytes=budgets["responseBytes"] + 1)
        except BaseException:
            self.uncertain = True
            raise
        raw = response.raw
        require(isinstance(raw, bytes), "The response transport did not retain actual bytes.")
        actual_length = len(raw)
        raw = raw[:budgets["responseBytes"] + 1]
        response_headers = self.support.bounded_header_prefix(response.headers)
        for key, item in response_headers:
            if key.lower() in self.support.SECRET_KEYS and item: self.secrets.add(item)
        complete = response.complete_http is True and response.failure is None and actual_length <= budgets["responseBytes"] and len(response_headers) == len(response.headers)
        self.charged_bytes += len(raw) if complete else budgets["responseBytes"] + 1
        private = {"ordinal": ordinal, "label": label, "actor": actor, "completedAt": response.completed_at, "request": request,
                   "payloadBase64": None if payload is None else base64.b64encode(payload).decode(),
                   "status": response.status, "headers": response_headers, "rawBase64": base64.b64encode(raw).decode(),
                   "completeHttp": response.complete_http, "failure": response.failure, "observedRawBytes": actual_length,
                   "retainedRawTruncated": actual_length != len(raw)}
        response_sha = self._save(stem + "-response.json", private)
        if resource_kind is not None:
            require(self.ownership_pending is None, "A second creation cannot replace unresolved ownership.")
            self.uncertain = True
            self.ownership_pending = {"kind": resource_kind, "actor": actor, "label": label, "ordinal": ordinal,
                "responseReceiptSha256": response_sha, "stage": "response-awaiting-owner"}
            self._persist()
        elif followup:
            self.ownership_pending["reconciliationResponse"] = {"label": label, "ordinal": ordinal, "responseReceiptSha256": response_sha}
            self.ownership_pending["stage"] = "reconciliation-awaiting-owner"
            self._persist()
        lengths = [item for key, item in response_headers if key.lower() == "content-length"]
        transfers = [item for key, item in response_headers if key.lower() == "transfer-encoding"]
        content_types = [item for key, item in response_headers if key.lower() == "content-type"]
        framing = all("\r" not in key + item and "\n" not in key + item for key, item in response_headers)
        if lengths:
            framing = framing and len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == actual_length and not transfers
        framing = framing and len(set(content_types)) <= 1
        if content_types and "charset=" in content_types[0].lower():
            charset = content_types[0].lower().split("charset=", 1)[1].split(";", 1)[0].strip().strip('"')
            framing = framing and charset in ("utf-8", "utf8")
        complete = complete and framing
        if not complete:
            self.uncertain = True
            self._persist()
            raise PreparationError("The one HTTP attempt did not yield a complete bounded response; recovery is required.")
        completed_at = instant(response.completed_at)
        require(type(response.status) is int and 100 <= response.status <= 599 and
                (self.last_completed is None or completed_at >= self.last_completed), "An HTTP status or completed response timestamp is invalid.")
        self.last_completed = completed_at
        self._now()
        text_body = raw.decode("utf-8", errors="strict")
        if not raw:
            decoded = None
        elif content_types and "json" in content_types[0].lower():
            decoded = strict_json(raw)
        else:
            try:
                decoded = strict_json(raw)
            except ValueError:
                decoded = text_body
        self._collect_secrets(decoded)
        event = {"ordinal": ordinal, "completedAt": response.completed_at, "responseReceiptSha256": response_sha,
                 "request": request, "response": {"status": response.status, "body": decoded}}
        self._save(stem + "-response.json", self._safe({"event": event, "responseHeaders": response_headers}), export=True)
        self.pending = None
        self.responses[label] = event
        self._persist()
        return event

    def _get(self, label, actor, route):
        event = self._dispatch(label, actor, "GET", route)
        require(event["response"]["status"] == 200, "A required full public observation was not HTTP 200.")
        return event["response"]["body"]

    def _post(self, label, actor, route, body=None, *, statuses=(200, 204), form=False, uncertain=False):
        event = self._dispatch(label, actor, "POST", route, body, form=form)
        if event["response"]["status"] not in statuses:
            self.uncertain = self.uncertain or uncertain
            raise PreparationError("The one owned mutation was not acknowledged with its expected status.")
        return event

    def _login(self, actor):
        row = self.authority.credentials[actor]
        event = self._post("login-" + actor, actor, "/emby/Users/AuthenticateByName", {"Username": row["username"], "Pw": row["password"]},
                           statuses=(200,), form=True, uncertain=True)
        body = event["response"]["body"]
        require(isinstance(body, dict) and isinstance(body.get("User"), dict) and isinstance(body.get("SessionInfo"), dict) and
                isinstance(body["User"].get("Policy"), dict), "A login response lacks complete user/policy/session object shapes.")
        user, session = body["User"], body["SessionInfo"]
        require(body.get("ServerId") == self.manifest["server"]["id"] and isinstance(body.get("AccessToken"), str) and body["AccessToken"] and
                user.get("Id") == self.user_ids[actor] and user.get("Name") == row["username"] and
                user["Policy"].get("IsAdministrator") is (actor == "admin") and session.get("UserId") == user["Id"] and
                session.get("DeviceId") == self.manifest["actors"][actor]["deviceId"] and identifier(session.get("Id")),
                "A preparation login acknowledged another principal/device/session.")
        require(body["AccessToken"] not in self.tokens.values() and session["Id"] not in self.sessions.values(), "Preparation actors share a token or session.")
        self.tokens[actor], self.sessions[actor], self.logins[actor] = body["AccessToken"], session["Id"], event
        self._complete_ownership(event, kind="login", actor=actor,
                                 owner={"userId": user["Id"], "sessionId": session["Id"], "tokenSha256": digest(body["AccessToken"].encode())})

    def _logout(self, actor):
        require(actor in self.tokens and actor not in self.revoked, "Only a known live preparation token can be retired.")
        self._post("logout-" + actor, actor, "/emby/Sessions/Logout", statuses=(204,))
        event = self._dispatch("invalid-" + actor, actor, "GET", "/emby/Sessions")
        require(event["response"]["status"] == 401, "The exact logged-out preparation token remains usable.")
        self.revoked.add(actor)
        self._persist()

    def _query(self, user, *, parent=None, ids=None):
        query = {"Recursive": "true", "Fields": FIELDS, "EnableUserData": "true", "EnableTotalRecordCount": "true", "Limit": "256"}
        if parent is not None: query["ParentId"] = parent
        if ids is not None: query["Ids"] = ",".join(sorted(ids))
        return "/emby/Users/" + user + "/Items?" + urlencode(query)

    def _devices(self, label):
        devices = devices_page(self._get(label, "admin", "/emby/Devices"))
        baseline = self.authority.baseline["devices"]
        require(set(baseline) <= set(devices) and
                all(devices[key]["ReportedDeviceId"] == row["ReportedDeviceId"] for key, row in baseline.items()),
                "The Devices response omitted or replaced a retained old device identity.")
        require("admin" in self.logins and set(self.logins) == set(self.tokens),
                "Devices population checks require actual acknowledged preparation logins.")
        owned = set()
        for actor, login in self.logins.items():
            metadata = self.manifest["actors"][actor]
            acknowledged = login["response"]["body"]
            require(acknowledged["AccessToken"] == self.tokens[actor] and acknowledged["SessionInfo"]["UserId"] == self.user_ids[actor] and
                    acknowledged["SessionInfo"]["DeviceId"] == metadata["deviceId"],
                    "A device population binding differs from its actual actor login acknowledgement.")
            selected = [row for row in devices.values() if row["ReportedDeviceId"] == metadata["deviceId"]]
            require(len(selected) == 1, "The Devices response lacks one acknowledged owned preparation device.")
            row = selected[0]
            require(row["Id"] not in baseline and row["Id"] not in owned and row.get("LastUserId") == self.user_ids[actor] and
                    row.get("LastUserName") == metadata["username"] and row.get("AppName") == "Goby NextUp Preparation" and
                    row.get("AppVersion") == "1.0" and row.get("Name") == "Linux Fixture Recorder",
                    "A new device does not match its exact owned actor and request client metadata.")
            instant(row.get("DateLastActivity"))
            owned.add(row["Id"])
        require(set(devices) - set(baseline) == owned,
                "The Devices response contains an unowned new device or an incomplete owned population.")
        return devices

    def _snapshot(self, prefix, *, final=False):
        admin = self.user_ids["admin"]
        server = self._get(prefix + "-server", "admin", "/emby/System/Info/Public")
        require(server.get("Id") == self.manifest["server"]["id"] and server.get("Version") == self.manifest["server"]["version"], "The public server identity changed.")
        roster_rows = self._get(prefix + "-users", "admin", "/emby/Users")
        expected_users = set(self.manifest["preservation"]["userIds"]) | (set(self.user_ids.values()) if final else set())
        require(isinstance(roster_rows, list) and len(roster_rows) == len(expected_users), "The frozen user roster changed unexpectedly.")
        roster = {row["Id"]: row for row in roster_rows}
        require(set(roster) == expected_users, "A user appeared or disappeared outside the two acknowledged creations.")
        libraries = page(self._get(prefix + "-libraries", "admin", "/emby/Library/VirtualFolders/Query"), "ItemId", 16)
        expected_libraries = set(self.manifest["preservation"]["libraryIds"]) | ({row["libraryId"] for row in self.libraries.values()} if final else set())
        require(set(libraries) == expected_libraries, "The frozen library roster changed outside the two owned creations.")
        catalogs, all_ids = {}, set()
        for index, library in enumerate(sorted(libraries)):
            catalogs[library] = page(self._get(prefix + "-catalog-" + str(index), "admin", self._query(admin, parent=library)))
            all_ids.update(catalogs[library])
        require(0 < len(all_ids) <= 256, "The complete frozen public catalog exceeds its bound.")
        self.catalog_ids = all_ids
        projections, preferences, details = {}, {}, {}
        for index, user in enumerate(sorted(roster)):
            projections[user] = page(self._get(prefix + "-items-" + str(index), "admin", self._query(user, ids=all_ids)))
            require(set(projections[user]) <= all_ids, "A per-user projection escaped its requested IDs.")
            preferences[user] = self._get(prefix + "-prefs-" + str(index), "admin", "/emby/UserSettings/" + user)
        for index, row in enumerate(self.manifest["preservation"]["detailRoutes"]):
            details.setdefault(row["group"], {})[row["itemId"]] = self._get(prefix + "-detail-" + str(index), "admin",
                    "/emby/Users/" + row["userId"] + "/Items/" + row["itemId"])
        devices = self._devices(prefix + "-devices")
        configuration = self._get(prefix + "-configuration", "admin", "/emby/System/Configuration")
        result = {"marker": self.authority.baseline["marker"], "version": 1, "captured_at": self.utc_now(), "server": server,
                  "roster": roster, "libraries": libraries, "configuration": configuration, "catalog_by_library": catalogs,
                  "items_by_user": projections, "preferences": preferences, "details": details, "devices": devices,
                  "credential_context": {"channel": "controller_api", "authenticated_user_id": admin,
                      "token_sha256": digest(self.tokens["admin"].encode()), "user_id_semantics": "subject_projection"}}
        self._save(prefix + "-public.json", result)
        return result

    def _preserve(self, before, after, *, prior=False):
        require(instant(before["captured_at"]) <= instant(after["captured_at"]), "Public snapshot time moved backwards.")
        changes = []
        for key in ("server", "configuration", "details"):
            require(same(before[key], after[key]), "A preserved complete public document changed: " + key)
        old_ids = {item for rows in before["catalog_by_library"].values() for item in rows}
        for key in ("libraries", "catalog_by_library", "preferences"):
            require(set(before[key]) <= set(after[key]) and all(same(item, after[key][name]) for name, item in before[key].items()),
                    "A preserved old public library/catalog/preference changed: " + key)
        for user, rows in before["items_by_user"].items():
            require(same(rows, {key: val for key, val in after["items_by_user"][user].items() if key in old_ids}), "An old per-user item projection changed.")
        for user, row in before["roster"].items():
            current = after["roster"][user]
            historical = [entry for entry in self.authority.release["closedAuthentication"] if prior and entry["userId"] == user]
            allowed = {"LastLoginDate", "LastActivityDate"} if user == self.user_ids["admin"] or historical else set()
            require(same({key: val for key, val in row.items() if key not in allowed}, {key: val for key, val in current.items() if key not in allowed}),
                    "An old account policy/configuration or unrelated field changed.")
            for key in sorted(allowed):
                if row.get(key) == current.get(key) and (key in row) == (key in current): continue
                require(user == self.user_ids["admin"] and instant(before["captured_at"]) <= instant(current[key]) <= instant(after["captured_at"]) or
                        any(instant(entry["from"]) <= instant(current[key]) <= instant(entry["through"]) for entry in historical),
                        "An authentication date escaped its exact current or already closed login window.")
                changes.append({"kind": "owned-authentication-time", "userId": user, "field": key})
        owned_devices = {row["deviceId"]: role for role, row in self.manifest["actors"].items() if role in self.tokens}
        prior_devices = {row["reportedDeviceId"]: row for row in self.authority.release["closedAuthentication"]} if prior else {}
        require(set(before["devices"]) <= set(after["devices"]), "An old public device disappeared.")
        for key, row in after["devices"].items():
            reported = row.get("ReportedDeviceId")
            old = before["devices"].get(key)
            owned = owned_devices.get(reported)
            closed = prior_devices.get(reported)
            if owned is None and closed is None:
                require(old is not None and same(old, row), "An unowned existing device changed or appeared.")
                continue
            if old is not None:
                require(same({name: val for name, val in old.items() if name != "DateLastActivity"},
                             {name: val for name, val in row.items() if name != "DateLastActivity"}), "An owned or prior closed device changed structural fields.")
            if owned is not None:
                metadata = self.manifest["actors"][owned]
                require(row.get("LastUserId") == self.user_ids[owned] and row.get("LastUserName") == metadata["username"] and
                        row.get("AppName") == "Goby NextUp Preparation" and row.get("AppVersion") == "1.0" and row.get("Name") == "Linux Fixture Recorder",
                        "A new preparation device is not bound to its exact actor metadata.")
                low, high = before["captured_at"], after["captured_at"]
            else:
                require(old is not None and row.get("LastUserId") == closed["userId"], "A historical closed device changed owner or appeared newly.")
                low, high = closed["from"], closed["through"]
            if old is None or old.get("DateLastActivity") != row.get("DateLastActivity"):
                require(instant(low).replace(microsecond=0) <= instant(row["DateLastActivity"]) <= instant(high),
                        "An authentication device update escaped its exact observed whole-second window.")
                changes.append({"kind": "owned-device-time" if owned else "prior-closed-device-time", "deviceId": key})
        return {"preserved": True, "allowedAuthenticationChanges": changes,
                "beforeTokenSha256": before["credential_context"]["token_sha256"], "afterTokenSha256": after["credential_context"]["token_sha256"]}

    def _verify_library(self, row, symbol):
        body = library_body(self.manifest, symbol)
        require(row.get("Name") == body["Name"] and row.get("CollectionType") == "tvshows" and row.get("Locations") == body["Paths"] and
                identifier(row.get("ItemId")), "A new library does not match its exact owned root/name/type.")
        options = row.get("LibraryOptions", {})
        for key, expected in body["LibraryOptions"].items():
            actual = options.get(key)
            if key == "PathInfos":
                require(isinstance(actual, list) and len(actual) == 1 and actual[0].get("Path") == expected[0]["Path"] and not actual[0].get("NetworkPath"),
                        "The new library contains an unexpected path mapping.")
            elif key == "TypeOptions":
                require(isinstance(actual, list) and len(actual) == len(expected) and {item.get("Type") for item in actual} == {item["Type"] for item in expected} and
                        all(item.get(field) == [] for item in actual for field in ("MetadataFetchers", "MetadataFetcherOrder", "ImageFetchers", "ImageFetcherOrder", "ImageOptions")),
                        "The new library enabled an unapproved provider or media type.")
            else:
                require(same(actual, expected), "A new library option differs from its exact safe template: " + key)

    def _catalog_map(self, rows, symbol):
        name, root = self.manifest["libraries"][symbol]["seriesName"], self.manifest["media"]["roots"][symbol]
        series_rows = [row for row in rows.values() if row.get("Type") == "Series"]
        require(len(series_rows) == 1 and series_rows[0].get("Name") == name and series_rows[0].get("Path") == str(Path(root) / name),
                "A new library must contain exactly its one named owned series.")
        series, prefix = series_rows[0], symbol[1]
        require(identifier(series.get("ParentId")), "The new series source-root identity is missing.")
        mapped = {prefix: {"id": series["Id"], "type": "Series", "library": symbol, "name": name, "parentId": series["ParentId"]}}
        season_rows = [row for row in rows.values() if row.get("Type") == "Season"]
        require(len(season_rows) == 2, "The new series must contain exactly two seasons.")
        for season in (1, 2):
            found = [row for row in season_rows if type(row.get("IndexNumber")) is int and row["IndexNumber"] == season]
            require(len(found) == 1 and found[0].get("ParentId") == series["Id"] and found[0].get("SeriesId") == series["Id"], "A new season relation is ambiguous.")
            row = found[0]
            mapped[prefix + "S" + str(season)] = {"id": row["Id"], "type": "Season", "library": symbol,
                "parentId": row["ParentId"], "seriesId": row["SeriesId"], "indexNumber": season}
        episodes = [row for row in rows.values() if row.get("Type") == "Episode"]
        require(len(episodes) == 3, "The new library must contain exactly its three episode copies.")
        for index, (season, episode) in enumerate(((1, 1), (1, 2), (2, 1)), 1):
            item_symbol = prefix + str(index)
            selected = [row for row in episodes if type(row.get("ParentIndexNumber")) is int and type(row.get("IndexNumber")) is int and
                        row["ParentIndexNumber"] == season and row["IndexNumber"] == episode]
            require(len(selected) == 1, "An episode number is missing, duplicated, or not an integer.")
            row = selected[0]
            require(row.get("Path") == self.media[symbol]["files"][item_symbol]["path"] and row.get("ParentId") == mapped[prefix + "S" + str(season)]["id"] and
                    row.get("SeriesId") == series["Id"] and type(row.get("RunTimeTicks")) is int and row["RunTimeTicks"] == RUNTIME,
                    "An episode path/relation/runtime differs from its owned copied bytes.")
            sources = row.get("MediaSources")
            require(isinstance(sources, list) and len(sources) == 1 and sources[0].get("RunTimeTicks") == RUNTIME,
                    "A copied episode lacks one authoritative source or has alternate versions.")
            streams = row.get("MediaStreams", []) + sources[0].get("MediaStreams", [])
            videos = [stream for stream in streams if stream.get("Type") == "Video"]
            require(videos and all(stream.get("AverageFrameRate") == 30 and stream.get("RealFrameRate") == 30 for stream in videos),
                    "The actual indexed video is not the approved 30 fps profile.")
            mapped[item_symbol] = {"id": row["Id"], "type": "Episode", "library": symbol, "parentId": row["ParentId"],
                "seriesId": series["Id"], "parentIndexNumber": season, "indexNumber": episode, "runtimeTicks": RUNTIME,
                "frameRate": 30, "mediaSha256": self.media[symbol]["files"][item_symbol]["sha256"]}
        allowed = {row["id"] for row in mapped.values()}
        extra = [row for key, row in rows.items() if key not in allowed]
        require(len(extra) <= 1 and all(row.get("Type") == "Folder" and row.get("Id") == series["ParentId"] and row.get("Path") == root for row in extra),
                "The new library contains foreign auxiliary content or another series.")
        require(not allowed.intersection(self.catalog_ids), "The new catalog reused an old item identity.")
        return mapped

    def _create_libraries(self):
        for role in ("P", "Q"):
            require(all(row.get("Name", "").casefold() != self.manifest["actors"][role]["username"].casefold() for row in self.before["roster"].values()),
                    "A proposed new account name already exists.")
        for symbol in ("LA", "LB"):
            body = library_body(self.manifest, symbol)
            require(all(row.get("Name", "").casefold() != body["Name"].casefold() and not set(row.get("Locations", [])).intersection(body["Paths"])
                        for row in self.before["libraries"].values()), "A proposed new library name or root already exists.")
            event = self._post("create-" + symbol, "admin", "/emby/Library/VirtualFolders", body, uncertain=True)
            require(event["response"]["body"] in (None, {}), "Library creation returned an unreviewed acknowledgement body.")
            lookup_label = "created-library-lookup-" + symbol
            self.ownership_pending["allowedFollowup"] = {"label": lookup_label, "actor": "admin", "method": "GET", "route": "/emby/Library/VirtualFolders/Query"}
            self._persist()
            current = page(self._get(lookup_label, "admin", "/emby/Library/VirtualFolders/Query"), "ItemId", 16)
            known = set(self.before["libraries"]) | {row["libraryId"] for row in self.libraries.values()}
            require(known <= set(current) and all(same(current[key], row) for key, row in self.before["libraries"].items()),
                    "A pre-existing library changed during new-library creation.")
            for previous_symbol, previous_library in self.libraries.items():
                self._verify_library(current[previous_library["libraryId"]], previous_symbol)
            new = {key: row for key, row in current.items() if key not in known}
            require(len(new) == 1, "This new library cannot be uniquely reconciled from its acknowledged creation.")
            selected = next(iter(new.values()))
            self._verify_library(selected, symbol)
            item = selected["ItemId"]
            self.libraries[symbol] = {"libraryId": item, "collectionType": "tvshows", "owned": True}
            self._complete_ownership(event, kind="library", actor="admin",
                                     owner={"symbol": symbol, "libraryId": item, "rootPath": self.manifest["media"]["roots"][symbol]})
        for symbol in ("LA", "LB"):
            route = "/emby/Items/" + self.libraries[symbol]["libraryId"] + "/Refresh?" + urlencode({
                "Recursive": "true", "MetadataRefreshMode": "FullRefresh", "ImageRefreshMode": "ValidationOnly"})
            event = self._post("refresh-" + symbol, "admin", route, {}, uncertain=True)
            require(event["response"]["body"] in (None, {}), "A scoped refresh returned an unreviewed acknowledgement body.")
        deadline, previous, ready_maps = self._now() + 100, None, None
        for number in range(12):
            require(self._now() < deadline, "The fixed new-library scan observation window expired.")
            current = page(self._get("scan-%02d-libraries" % number, "admin", "/emby/Library/VirtualFolders/Query"), "ItemId", 16)
            require(set(current) == set(self.before["libraries"]) | {row["libraryId"] for row in self.libraries.values()} and
                    all(same(current[key], row) for key, row in self.before["libraries"].items()), "An old library changed during the scoped scan.")
            catalogs, ready = {}, True
            for symbol in ("LA", "LB"):
                row = current[self.libraries[symbol]["libraryId"]]
                self._verify_library(row, symbol)
                ready = ready and not any(key in row for key in ("RefreshStatus", "RefreshProgress"))
                catalogs[symbol] = page(self._get("scan-%02d-%s" % (number, symbol), "admin", self._query(self.user_ids["admin"], parent=self.libraries[symbol]["libraryId"])))
            state = {"libraries": current, "catalogs": catalogs}
            try:
                ready_maps = {key: self._catalog_map(rows, key) for key, rows in catalogs.items()} if ready else None
            except PreparationError:
                ready_maps = None
            if ready_maps is not None and previous is not None and same(state, previous):
                for symbol in ("LA", "LB"):
                    self.items.update(ready_maps[symbol])
                    self.libraries[symbol]["sourceRootId"] = self.items[symbol[1]]["parentId"]
                require(len({row["id"] for row in self.items.values()}) == 12, "The two new libraries overlap semantic item identities.")
                self._save("indexed-catalog.json", state)
                self._persist()
                return
            previous = state if ready_maps is not None else None
            if number < 11:
                require(self._now() + 2 < deadline, "The next scan observation would exceed its frozen window.")
                self.sleeper(2)
        raise PreparationError("The new libraries did not reach two stable complete observations within twelve rounds.")

    def _map_roots(self):
        views = page(self._get("admin-views", "admin", "/emby/Users/" + self.user_ids["admin"] + "/Views"))
        selectable = self._get("admin-selectable-folders", "admin", "/emby/Library/SelectableMediaFolders")
        require(isinstance(selectable, list) and 0 < len(selectable) <= 64 and all(isinstance(row, dict) for row in selectable),
                "A complete bounded selectable-folder observation is required.")
        for symbol in ("LA", "LB"):
            selected = [row for row in views.values() if row.get("Name") == self.manifest["libraries"][symbol]["name"] and row.get("CollectionType") == "tvshows"]
            require(len(selected) == 1, "The new TV library lacks one actual public view.")
            self.libraries[symbol]["viewId"] = self.libraries[symbol]["scopeItemId"] = selected[0]["Id"]
            view = self._get("view-detail-" + symbol, "admin", "/emby/Users/" + self.user_ids["admin"] + "/Items/" + selected[0]["Id"])
            source = self._get("root-detail-" + symbol, "admin", "/emby/Users/" + self.user_ids["admin"] + "/Items/" + self.libraries[symbol]["sourceRootId"])
            root = self.manifest["media"]["roots"][symbol]
            require(source.get("Id") == self.libraries[symbol]["sourceRootId"] and source.get("Type") == "Folder" and source.get("Path") == root,
                    "The series' public parent is not the exact new native source root.")
            require(view.get("Id") == selected[0]["Id"] and (view["Id"] == self.libraries[symbol]["libraryId"] or
                    view.get("Path") == root or view.get("ParentId") == source["Id"]), "The view-to-library/source relation is not established publicly.")
            grant = [row for row in selectable if row.get("Id") == self.libraries[symbol]["libraryId"]]
            require(len(grant) == 1, "The newly owned library lacks one unique selectable identity.")
            grant = grant[0]
            guid = grant.get("Guid")
            require(isinstance(guid, str) and re.fullmatch(r"[0-9a-f]{32}", guid) and uuid.UUID(guid).hex == guid and
                    sum(row.get("Guid") == guid for row in selectable) == 1 and grant.get("Name") == self.manifest["libraries"][symbol]["name"] and
                    grant.get("IsUserAccessConfigurable") is True and isinstance(grant.get("SubFolders"), list) and len(grant["SubFolders"]) == 1,
                    "The selected policy Guid is missing, duplicated, or not the owned configurable library.")
            folder = grant["SubFolders"][0]
            require(isinstance(folder, dict) and folder.get("Id") == source["Id"] and folder.get("Path") == root and
                    folder.get("IsUserAccessConfigurable") is True and guid not in {self.libraries[symbol]["libraryId"], selected[0]["Id"], source["Id"]},
                    "The actual selectable Guid does not bind the new native source root and path.")
            self.libraries[symbol]["policyFolderId"] = guid
        require(all(self.libraries["LA"][key] != self.libraries["LB"][key] for key in ("libraryId", "viewId", "sourceRootId", "policyFolderId")),
                "The two TV libraries must have separate management/view/source/policy identities.")
        self._persist()

    def _policy(self, actor):
        require(actor in self.created_users, "Policy changes require the exact newly created account acknowledgement.")
        policy = deepcopy(self.created_users[actor]["Policy"])
        policy.update(IsAdministrator=False, IsDisabled=False, EnableAllFolders=False,
                      EnabledFolders=sorted(row["policyFolderId"] for row in self.libraries.values()),
                      EnableMediaPlayback=True, EnableAudioPlaybackTranscoding=True, EnableVideoPlaybackTranscoding=True,
                      EnablePlaybackRemuxing=True, EnableContentDeletion=False, EnableContentDownloading=False)
        return policy

    def _profile(self, actor, label):
        profile = self._get(label, actor, "/emby/Users/" + self.user_ids[actor])
        policy = profile.get("Policy", {})
        require(profile.get("Id") == self.user_ids[actor] and profile.get("Name") == self.manifest["actors"][actor]["username"] and
                isinstance(profile.get("Configuration"), dict) and policy.get("IsAdministrator") is False and policy.get("IsDisabled") is False and
                policy.get("EnableAllFolders") is False and policy.get("EnableMediaPlayback") is True and
                isinstance(policy.get("EnabledFolders"), list) and len(policy["EnabledFolders"]) == 2 and
                set(policy["EnabledFolders"]) == {row["policyFolderId"] for row in self.libraries.values()},
                "The ordinary own-token profile lacks exactly the two approved folder grants.")
        return profile

    def _create_accounts(self):
        for actor in ("P", "Q"):
            event = self._post("create-user-" + actor, "admin", "/emby/Users/New", {"Name": self.manifest["actors"][actor]["username"]}, statuses=(200,), uncertain=True)
            user = event["response"]["body"]
            require(isinstance(user, dict) and isinstance(user.get("Policy"), dict) and identifier(user.get("Id")) and
                    user["Id"] not in self.user_ids.values() and user["Id"] not in self.before["roster"] and
                    user.get("Name") == self.manifest["actors"][actor]["username"] and user["Policy"].get("IsAdministrator") is False,
                    "A new user creation did not acknowledge one distinct ordinary owned account.")
            self.created_users[actor], self.user_ids[actor] = user, user["Id"]
            self._complete_ownership(event, kind="account", actor="admin", owner={"actor": actor, "userId": user["Id"], "username": user["Name"]})
            self._post("password-" + actor, "admin", "/emby/Users/" + user["Id"] + "/Password",
                       {"Id": user["Id"], "NewPw": self.authority.credentials[actor]["password"], "ResetPassword": False}, uncertain=True)
            self._post("policy-" + actor, "admin", "/emby/Users/" + user["Id"] + "/Policy", self._policy(actor), uncertain=True)
            profile = self._get("policy-ack-" + actor, "admin", "/emby/Users/" + user["Id"])
            require(profile.get("Id") == user["Id"] and same(profile.get("Policy"), self._policy(actor)), "The complete fresh policy acknowledgement differs.")

    def _detail(self, actor, item, label):
        body = self._get(label, actor, "/emby/Users/" + self.user_ids[actor] + "/Items/" + self.items[item]["id"])
        mapped = self.items[item]
        require(body.get("Id") == mapped["id"] and body.get("Type") == mapped["type"] and body.get("ParentId") == mapped["parentId"] and
                isinstance(body.get("UserData"), dict), "A full own-token detail has another item or parent identity.")
        if item in EPISODES:
            require(body.get("SeriesId") == mapped["seriesId"] and type(body.get("IndexNumber")) is int and body["IndexNumber"] == mapped["indexNumber"] and
                    type(body.get("ParentIndexNumber")) is int and body["ParentIndexNumber"] == mapped["parentIndexNumber"] and
                    type(body.get("RunTimeTicks")) is int and body["RunTimeTicks"] == RUNTIME, "A full episode detail has different numbering, series, or runtime.")
            self.planner.userdata_fact(body)
        elif mapped["type"] == "Season":
            require(body.get("SeriesId") == mapped["seriesId"] and type(body.get("IndexNumber")) is int and body["IndexNumber"] == mapped["indexNumber"],
                    "A full season detail has different series or numbering.")
        return body

    def _baseline(self):
        for actor in ("P", "Q"):
            self._login(actor)
            self.profiles[actor] = self._profile(actor, "baseline-profile-" + actor)
            self.preferences[actor] = self._get("baseline-prefs-" + actor, actor, "/emby/usersettings/" + self.user_ids[actor])
            views = page(self._get("baseline-views-" + actor, actor, "/emby/Users/" + self.user_ids[actor] + "/Views"))
            require(set(views) == {row["viewId"] for row in self.libraries.values()}, "An ordinary actor must see exactly LA and LB under its own token.")
            for symbol in ("LA", "LB"):
                rows = page(self._get("baseline-catalog-" + actor + "-" + symbol, actor,
                                     self._query(self.user_ids[actor], parent=self.libraries[symbol]["viewId"])))
                require(same(self._catalog_map(rows, symbol), {key: row for key, row in self.items.items() if row["library"] == symbol}),
                        "An ordinary actor cannot read the exact prepared TV catalog with its own token.")
            for item in EPISODES:
                body = self._detail(actor, item, "baseline-detail-" + actor + "-" + item)
                require(self.planner.zero_state(self.planner.userdata_fact(body)), "All twelve episode details must initially prove complete zero history.")
                self.baselines[actor][item] = deepcopy(body["UserData"])
            for item in SUMMARIES:
                self.summaries[actor][item] = deepcopy(self._detail(actor, item, "baseline-summary-" + actor + "-" + item)["UserData"])
            self._persist()

    def _summary_proofs(self, actor, prefix):
        for item in SUMMARIES:
            if item not in self.summaries[actor]: continue
            body = self._detail(actor, item, prefix + "-" + actor + "-" + item)
            require(same(body["UserData"], self.summaries[actor][item]), "A series/season UserData summary did not return to the measured baseline.")

    def _calibrate(self, actor, item, mode):
        prefix = "cal-" + actor + "-" + mode
        if self.plays:
            require(all(row["stopped"] for row in self.plays.values()), "A prior owned lifecycle remains unstopped.")
            self.sleeper(self.manifest["lifecycleSeparationSeconds"])
        label = prefix + "-before-zero"
        before = self._detail(actor, item, label)
        require(same(before["UserData"], self.baselines[actor][item]) and self.planner.zero_state(self.planner.userdata_fact(before)), "Calibration requires the exact own-token zero baseline.")
        record = {"actor": actor, "item": item, "mode": mode, "beforeZero": self.responses[label]}
        event = self._post(prefix + "-info", actor, "/emby/Items/" + self.items[item]["id"] + "/PlaybackInfo",
                           {"UserId": self.user_ids[actor], "IsPlayback": True}, statuses=(200,), uncertain=True)
        info = event["response"]["body"]
        require(isinstance(info, dict) and identifier(info.get("PlaySessionId")) and isinstance(info.get("MediaSources"), list) and len(info["MediaSources"]) == 1 and
                isinstance(info["MediaSources"][0], dict) and identifier(info["MediaSources"][0].get("Id")) and
                type(info["MediaSources"][0].get("RunTimeTicks")) is int and info["MediaSources"][0]["RunTimeTicks"] == RUNTIME,
                "PlaybackInfo did not establish one exact owned 600-second source/session.")
        context = {"ItemId": self.items[item]["id"], "MediaSourceId": info["MediaSources"][0]["Id"], "PlaySessionId": info["PlaySessionId"], "SessionId": self.sessions[actor]}
        require(context["PlaySessionId"] not in self.play_session_ids, "A playback negotiation reused a current or historical owned session identity.")
        target = PARTIAL if mode == "partial" else RUNTIME
        self.plays[actor] = {"context": context, "item": item, "target": target, "position": 0, "stopped": False}
        self.play_session_ids.add(context["PlaySessionId"])
        self.touched.add((actor, item))
        self._complete_ownership(event, kind="playback", actor=actor, owner={"item": item, "context": context})
        started = {**context, "RunTimeTicks": RUNTIME, "PositionTicks": 0, "CanSeek": True, "IsPaused": False,
                   "IsMuted": False, "PlayMethod": "DirectStream", "PlaybackRate": 1}
        self._post(prefix + "-started", actor, "/emby/Sessions/Playing", started, statuses=(204,))
        self._post(prefix + "-progress", actor, "/emby/Sessions/Playing/Progress", {**started, "PositionTicks": target, "EventName": "TimeUpdate"}, statuses=(204,))
        self.plays[actor]["position"] = target
        self._persist()
        self._post(prefix + "-stopped", actor, "/emby/Sessions/Playing/Stopped", {**context, "PositionTicks": target, "Failed": False, "IsAutomated": False}, statuses=(204,))
        self.plays[actor]["stopped"] = True
        self._persist()
        label = prefix + "-before-delete"
        changed = self._detail(actor, item, label)
        record["beforeDelete"] = self.responses[label]
        data = changed["UserData"]
        require(data["PlayCount"] > 0 and (data["Played"] is False and data["PlaybackPositionTicks"] == PARTIAL if mode == "partial" else data["Played"] is True),
                "The actual full detail did not establish the required partial/completion state.")
        instant(data.get("LastPlayedDate"))
        require(same({key: value for key, value in self.baselines[actor][item].items() if key not in PLAYBACK_FIELDS},
                     {key: value for key, value in data.items() if key not in PLAYBACK_FIELDS}), "Playback changed unrelated full UserData fields.")
        deletion = self._dispatch(prefix + "-delete", actor, "DELETE", "/emby/Users/" + self.user_ids[actor] + "/PlayedItems/" + self.items[item]["id"])
        require(deletion["response"]["status"] == 200, "The narrow own-token DELETE did not return HTTP 200.")
        record["delete"] = deletion
        label = prefix + "-after-delete"
        after = self._detail(actor, item, label)
        require(same(after["UserData"], self.baselines[actor][item]) and self.planner.zero_state(self.planner.userdata_fact(after)),
                "The complete own-token UserData baseline was not exactly restored by DELETE.")
        record["afterDelete"] = self.responses[label]
        self.calibrations.append(record)
        self._persist()
        if mode == "partial": self._summary_proofs(actor, prefix + "-summaries")

    def _zero_proofs(self, prefix):
        for actor in ("P", "Q"):
            if actor not in self.tokens or actor in self.revoked: continue
            for item in EPISODES:
                if item not in self.baselines[actor]: continue
                body = self._detail(actor, item, prefix + "-episode-" + actor + "-" + item)
                require(same(body["UserData"], self.baselines[actor][item]) and self.planner.zero_state(self.planner.userdata_fact(body)), "A complete episode baseline was not restored.")
            self._summary_proofs(actor, prefix + "-summary")
            if actor in self.profiles:
                profile = self._profile(actor, prefix + "-profile-" + actor)
                require(same(profile["Configuration"], self.profiles[actor]["Configuration"]) and same(profile["Policy"], self.profiles[actor]["Policy"]),
                        "A new actor's complete configuration or policy changed during calibration.")
                prefs = self._get(prefix + "-prefs-" + actor, actor, "/emby/usersettings/" + self.user_ids[actor])
                require(same(prefs, self.preferences[actor]), "A new actor's complete preferences changed during calibration.")

    def _cleanup(self, *, success=False):
        require(self.cleanup_started is None, "Cleanup is single-use and cannot be resumed.")
        self.cleanup_started = self._now()
        self._enter("cleanup")
        errors = []
        if not success:
            for actor, play in self.plays.items():
                if play["stopped"]: continue
                try:
                    self._post("cleanup-stop-" + actor, actor, "/emby/Sessions/Playing/Stopped",
                               {**play["context"], "PositionTicks": play["position"], "Failed": False, "IsAutomated": False}, statuses=(204,))
                    play["stopped"] = True
                    self._persist()
                except BaseException as error:
                    errors.append({"stage": "stop-" + actor, "type": type(error).__name__})
                    if self.uncertain or self.journal_failed: return errors
            for actor, item in sorted(self.touched):
                try:
                    current = self._detail(actor, item, "cleanup-reconcile-" + actor)
                    if same(current["UserData"], self.baselines[actor][item]): continue
                    require(same({key: value for key, value in current["UserData"].items() if key not in PLAYBACK_FIELDS},
                                 {key: value for key, value in self.baselines[actor][item].items() if key not in PLAYBACK_FIELDS}),
                            "Cleanup cannot accept unrelated UserData drift.")
                    event = self._dispatch("cleanup-delete-" + actor, actor, "DELETE", "/emby/Users/" + self.user_ids[actor] + "/PlayedItems/" + self.items[item]["id"])
                    require(event["response"]["status"] == 200, "The cleanup DELETE was not acknowledged.")
                except BaseException as error:
                    errors.append({"stage": "reset-" + actor, "type": type(error).__name__})
                    if self.uncertain or self.journal_failed: return errors
            try:
                self._zero_proofs("cleanup-zero")
            except BaseException as error:
                errors.append({"stage": "zero-proofs", "type": type(error).__name__})
                if self.uncertain or self.journal_failed: return errors
            if "admin" in self.tokens and "admin" not in self.revoked and self.before is not None:
                try:
                    after = self._snapshot("cleanup-after", final=True)
                    self._save("cleanup-preservation.json", self._preserve(self.before, after))
                except BaseException as error:
                    errors.append({"stage": "public-preservation", "type": type(error).__name__})
                    if self.uncertain or self.journal_failed: return errors
        for actor in ("P", "Q", "admin"):
            if actor not in self.tokens or actor in self.revoked: continue
            try:
                self._logout(actor)
            except BaseException as error:
                errors.append({"stage": "logout-" + actor, "type": type(error).__name__})
                if self.uncertain or self.journal_failed: return errors
        return errors

    def _receipt(self, name, facts):
        value = {"schemaVersion": 1, "kind": "nextup-global-" + name, "runId": self.manifest["runId"], "target": "reference",
                 "facts": facts, "attestationState": "pending"}
        path = "draft-" + name + ".json"
        checksum = self._save(path, value)
        result = {"path": str(self.journal.root / "private" / path), "sha256": checksum}
        self.outputs[name] = result
        return result

    def _drafts(self):
        require(self.revoked == {"admin", "P", "Q"} and len(self.calibrations) == 4 and not self.uncertain and not self.failure,
                "Draft inputs require all four calibrations and all exact token rejection proofs.")
        for library in ("LA", "LB"):
            media = {**self.media[library], "sourceRootId": self.libraries[library]["sourceRootId"]}
            descriptor_row = self._receipt("media-" + library, media)
            self.libraries[library]["mediaRootReceiptSha256"] = descriptor_row["sha256"]
        actors = {}
        for actor in ("P", "Q"):
            row = self.manifest["actors"][actor]
            policy = self._receipt("policy-" + actor, {"previousUserIds": sorted(self.before["roster"]),
                "createdUser": {"Id": self.user_ids[actor], "Name": row["username"]}, "observedProfile": self.profiles[actor]})
            actors[actor] = {"userId": self.user_ids[actor], "username": row["username"], "deviceId": row["matrixDeviceId"],
                "credentialRef": row["credentialRef"], "newOwnedOrdinaryAccount": True, "policyReceiptSha256": policy["sha256"],
                "allowedFolderIds": sorted(value["policyFolderId"] for value in self.libraries.values())}
        cleanup_facts = {"contractVersion": 2, "process": self.manifest["process"], "serverId": self.manifest["server"]["id"],
            "actors": {actor: {"credentialRef": self.manifest["actors"][actor]["credentialRef"], "deviceId": self.manifest["actors"][actor]["deviceId"],
                "login": {"status": 200, "body": self.logins[actor]["response"]["body"], "responseReceiptSha256": self.logins[actor]["responseReceiptSha256"]}}
                for actor in ("P", "Q")}, "calibrations": self.calibrations}
        cleanup = self._receipt("cleanup", cleanup_facts)
        coordination = self._receipt("coordination", {"lock": self.manifest["lock"], "fixtureRoot": self.manifest["scope"]["fixtureRoot"],
            "releasedAt": self.authority.release["releasedAt"], "process": self.manifest["process"]})
        catalog = self._receipt("catalog", {"libraries": self.libraries, "items": self.items})
        credentials = {"schemaVersion": 1, "runId": self.manifest["runId"], "actors": {actor: {
            "credentialRef": actors[actor]["credentialRef"], "userId": self.user_ids[actor], "username": actors[actor]["username"],
            "password": self.authority.credentials[actor]["password"]} for actor in ("P", "Q")}}
        credential_sha = self._save("matrix-credentials.json", credentials)
        credential_descriptor = {"path": str(self.journal.root / "private/matrix-credentials.json"), "sha256": credential_sha}
        preparation = self._receipt("preparation", {"serverId": self.manifest["server"]["id"], "process": self.manifest["process"],
            "endpoint": self.manifest["endpoint"], "version": self.manifest["server"]["version"], "evidenceRoot": self.manifest["scope"]["matrixEvidenceRoot"],
            "credentialStoreSha256": credential_sha, "catalogReceiptSha256": catalog["sha256"], "actorIds": dict((actor, self.user_ids[actor]) for actor in ("P", "Q")),
            "retainedArtifacts": ["accounts", "catalog", "media", "protocol-audit"]})
        matrix = {"schemaVersion": 1, "runId": self.manifest["runId"], "target": "reference", "serverId": self.manifest["server"]["id"],
            "preparationReceiptSha256": preparation["sha256"], "coordinationReceiptSha256": coordination["sha256"], "catalogReceiptSha256": catalog["sha256"],
            "binding": {"processIdentity": self.manifest["process"]["application"], "version": self.manifest["server"]["version"], "endpoint": "http://127.0.0.1:18197",
                "isolationIdentity": self.manifest["process"]["application"]["networkNamespace"], "evidenceRoot": self.manifest["scope"]["matrixEvidenceRoot"],
                "fixtureReleased": False, "freshEvidenceRoot": True},
            "actors": actors, "libraries": self.libraries, "items": self.items,
            "cleanupContract": {"mode": "episode-delete-played-items", "zeroBaselineRequired": True, "verifiedForBoundTarget": True, "receiptSha256": cleanup["sha256"]},
            "lifecycleSeparationSeconds": self.manifest["lifecycleSeparationSeconds"]}
        private = str(self.journal.root / "private")
        execution = {"schemaVersion": 1, "ownerUid": self.manifest["ownerUid"], "matrix": matrix,
            "scope": {"fixtureRoot": self.manifest["scope"]["fixtureRoot"], "receiptRoot": private, "credentialRoot": private,
                "sourceRoot": self.manifest["scope"]["sourceRoot"], "proxySourceRoot": self.manifest["scope"]["proxySourceRoot"],
                "evidenceParent": self.manifest["scope"]["evidenceParent"]},
            "lock": self.manifest["lock"], "process": self.manifest["process"], "endpoint": self.manifest["endpoint"],
            "sources": {key: self.manifest["sources"][key] for key in ("matrix", "transport", "proxy")},
            "receipts": deepcopy(self.outputs), "credentials": credential_descriptor, "budgets": self.manifest["matrixBudgets"]}
        validator = object.__new__(self.support.Authority)
        validator.matrix, validator.execution, validator.planner = matrix, execution, self.planner
        validator._cleanup_proof(cleanup_facts)
        testable = deepcopy(matrix); testable["binding"]["fixtureReleased"] = True
        self.planner.validate_manifest(testable)
        for name, value in (("matrix", matrix), ("execution", execution)):
            filename = "draft-" + name + ".json"
            self.outputs[name] = {"path": str(self.journal.root / "private" / filename), "sha256": self._save(filename, value)}
        self._save("calibration-history-disposition.json", {"zeroExposedUserDataRestored": True,
            "authenticationPlaybackAndAuditHistoryRetained": True, "wholeDatabaseEqualityClaimed": False,
            "calibrations": [list(row) for row in CALIBRATIONS], "independentAttestationRequired": True})

    def run(self):
        require(not self.used, "A preparation runner cannot resume or run twice.")
        self.used = True
        cleanup_errors = []
        try:
            self.authority.acquire()
            self.authority.check()
            self.journal = self.journal_factory(self.manifest["scope"]["outputRoot"], self.manifest["ownerUid"])
            self.started = self._now()
            self._save("manifest.json", self.manifest)
            self._save("frozen-plan.json", self.plan)
            self._persist()
            self.media = self.authority.stage_media(self.journal)
            self._login("admin")
            self.before = self._snapshot("before")
            self._save("baseline-preservation.json", self._preserve(self.authority.baseline, self.before, prior=True))
            self._enter("libraries"); self._create_libraries()
            self._enter("mapping"); self._map_roots()
            self._enter("accounts"); self._create_accounts()
            self._enter("baseline"); self._baseline()
            self._enter("calibration")
            for actor, item, mode in CALIBRATIONS:
                self._calibrate(actor, item, mode)
            self._enter("zero"); self._zero_proofs("final-zero")
            self._enter("after")
            after = self._snapshot("after", final=True)
            self._save("final-preservation.json", self._preserve(self.before, after))
            cleanup_errors = self._cleanup(success=True)
            require(not cleanup_errors and self.revoked == {"admin", "P", "Q"}, "All three exact preparation sessions must be retired.")
            self.authority.check()
            self._drafts()
            self.completed = True
        except BaseException as error:
            self.failure = {"phase": self.phase, "type": type(error).__name__, "pendingLabel": self.pending.get("label") if self.pending else None}
            if self.pending is not None or self.ownership_pending is not None: self.uncertain = True
            if self.journal is not None and not self.journal_failed and not self.uncertain and self.cleanup_started is None:
                try:
                    cleanup_errors = self._cleanup()
                except BaseException as cleanup_error:
                    cleanup_errors.append({"stage": "cleanup", "type": type(cleanup_error).__name__})
        finally:
            terminal = {"schemaVersion": 1, "runId": self.manifest["runId"],
                "status": "awaiting_independent_attestation" if self.completed else "recovery_required" if self.uncertain or self.journal_failed or cleanup_errors else "stopped_with_known_cleanup",
                "completed": self.completed, "independentAttestationRequired": True, "matrixInputsUsable": False,
                "requestCount": self.count, "normalRequestCount": self.normal_count, "cleanupRequestCount": self.cleanup_count,
                "phaseRequestCounts": self.phase_counts, "chargedResponseBytes": self.charged_bytes,
                "cleanupComplete": bool(self.tokens) and set(self.tokens) == self.revoked and not cleanup_errors and not self.uncertain,
                "failure": self.failure, "cleanupErrors": cleanup_errors, "pending": self.pending,
                "ownershipPending": self.ownership_pending, "uncertain": self.uncertain,
                "outputs": self.outputs, "retainedArtifacts": ["accounts", "catalog", "media", "protocol-audit"],
                "originalImplementationBytesRead": False, "actualClientAcceptance": False}
            if self.journal is not None and not self.journal_failed:
                try:
                    self._save("terminal.json", terminal)
                    self._save("terminal.json", self._safe(terminal), export=True)
                except BaseException:
                    terminal.update(status="recovery_required", completed=False, cleanupComplete=False, evidenceIncomplete=True)
            if self.journal is not None: self.journal.close()
            self.authority.close()
        return terminal


def main(arguments=None):
    require(sys.flags.isolated and sys.flags.dont_write_bytecode, "Use the owned script through Python -I -B to exclude ambient module paths.")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("plan", "prepare"))
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--plan-sha256")
    args = parser.parse_args(arguments)
    require(sha(args.manifest_sha256), "The manifest file must have an explicit SHA-256 pin.")
    raw = read_owned(args.manifest, private=True)
    require(digest(raw) == args.manifest_sha256, "The explicit manifest digest differs.")
    manifest = validate_manifest(strict_json(raw))
    plan = frozen_plan(manifest)
    plan_digest = digest(canonical(plan).encode())
    if args.mode == "plan":
        print(canonical({"plan": plan, "planSha256": plan_digest}))
        return 0
    require(sha(args.plan_sha256) and args.plan_sha256 == plan_digest, "Prepare requires the separately reviewed frozen plan digest.")
    authority = Authority(manifest)
    support = authority.support
    runner = PreparationRunner(manifest, authority=authority, transport=support.HTTPTransport(manifest["endpoint"]),
        journal_factory=lambda root, uid: support.Journal(root, uid=uid))
    terminal = runner.run()
    print(canonical({"status": terminal["status"], "requestCount": terminal["requestCount"],
                     "cleanupComplete": terminal["cleanupComplete"], "matrixInputsUsable": False,
                     "evidenceRoot": manifest["scope"]["outputRoot"]}))
    return 0 if terminal["status"] == "awaiting_independent_attestation" else 2


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (PreparationError, OSError, ValueError) as error:
        print(canonical({"status": "blocked", "failureType": type(error).__name__, "matrixInputsUsable": False}), file=sys.stderr)
        raise SystemExit(2)
