#!/usr/bin/env python3
"""One owned folder grant experiment; every live invocation is single use."""

from __future__ import annotations

import argparse
import base64
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import fcntl
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import stat
import sys
import time
from types import SimpleNamespace
from urllib.parse import urlencode
import uuid

PREPARATION_SHA = "d5167f51d9e2203693eeab1f65b521342fa3221b231f7183f8a4b817f54afc39"
TRANSPORT_SHA = "d93ed5628d23deddd4619013a61b395c4e809857cf2bdd00d7e98f19e137edd1"
MATRIX_SHA = "da3ed22ce15a3cf82bce81be31db1a1d03a202c00d44e9ac8ef124a93a2d5259"
CLIENT, DEVICE, VERSION = "Goby Folder Grant Verifier", "Linux Fixture Recorder", "1.0"
BUDGETS = {"normalRequests": 70, "cleanupRequests": 60, "totalRequests": 130,
    "requestSeconds": 10, "normalSeconds": 600, "cleanupSeconds": 600,
    "requestBytes": 32768, "responseBytes": 262144,
    "totalResponseBytes": 67108864, "cleanupResponseBytes": 33554432}
EPISODES = ("A1", "A2", "A3", "B1", "B2", "B3")


def require(value, message):
    if not value:
        raise ValueError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False, allow_nan=False)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def strict_json(raw):
    def pairs(values):
        result = {}
        for key, value in values:
            require(key not in result, "Duplicate JSON member.")
            result[key] = value
        return result
    def constant(value):
        raise ValueError("Nonfinite JSON number.")
    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_constant=constant)


def instant(value):
    require(isinstance(value, str), "An observed timestamp is required.")
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(result.utcoffset() is not None, "An observed timestamp needs a timezone.")
    return result


def utc_now():
    return datetime.now(timezone.utc).isoformat()


def read_owned(path, maximum=16 * 1024 * 1024, *, private=True):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts, "An exact absolute path is required.")
    for entry in (path, *path.parents):
        info = entry.lstat()
        require(not stat.S_ISLNK(info.st_mode), "An authority path contains a symlink.")
        if entry == path:
            require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and
                    not info.st_mode & (0o077 if private else 0o022) and info.st_size <= maximum, "An input is not a bounded protected file.")
            target_info = info
        else:
            require(stat.S_ISDIR(info.st_mode) and (not info.st_mode & 0o022 or info.st_mode & stat.S_ISVTX),
                    "An authority ancestor is writable.")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, "rb") as handle:
        opened = os.fstat(handle.fileno())
        current = path.lstat()
        require((opened.st_dev, opened.st_ino) == (current.st_dev, current.st_ino) == (target_info.st_dev, target_info.st_ino),
                "An input changed during open.")
        raw = handle.read(maximum + 1)
    require(len(raw) <= maximum, "An input exceeded its bound.")
    return raw


def load_descriptor(row):
    require(isinstance(row, dict) and set(row) == {"path", "sha256"} and
            isinstance(row["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", row["sha256"]),
            "An exact source or evidence descriptor is required.")
    raw = read_owned(row["path"], private=Path(row["path"]).suffix != ".py")
    require(digest(raw) == row["sha256"], "Pinned authority bytes changed.")
    return raw


def module_from(row, name):
    raw = load_descriptor(row)
    spec = importlib.util.spec_from_file_location(name, row["path"])
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    exec(compile(raw, row["path"], "exec"), module.__dict__)
    return module


def decode_payload(raw, headers):
    lengths = [value for key, value in headers if key.lower() == "content-length"]
    transfers = [value for key, value in headers if key.lower() == "transfer-encoding"]
    types = [value for key, value in headers if key.lower() == "content-type"]
    require(all(isinstance(key, str) and isinstance(value, str) and "\r" not in key + value and "\n" not in key + value
                for key, value in headers) and len(set(types)) <= 1, "HTTP metadata is malformed or ambiguous.")
    if lengths:
        require(len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(raw) and not transfers,
                "The completed HTTP framing is inconsistent.")
    if types and "charset=" in types[0].lower():
        require(types[0].lower().split("charset=", 1)[1].split(";", 1)[0].strip().strip('"') in ("utf-8", "utf8"),
                "Only UTF-8 response text is supported.")
    if not raw:
        return None
    if types and "json" in types[0].lower():
        return strict_json(raw)
    try:
        return strict_json(raw)
    except ValueError:
        return raw.decode("utf-8", errors="strict")


def decode_record(row, *, expected_status=None):
    record = strict_json(load_descriptor(row))
    require(record.get("completeHttp") is True and record.get("failure") is None and
            type(record.get("status")) is int and (expected_status is None or record["status"] == expected_status),
            "A provenance response is not a completed expected HTTP result.")
    raw = base64.b64decode(record["rawBase64"], validate=True)
    require(len(raw) <= 262144 and record.get("observedRawBytes", len(raw)) == len(raw) and
            record.get("retainedRawTruncated", False) is False, "A provenance response was truncated.")
    body = decode_payload(raw, record["headers"])
    return record, body


def token_from_request(record):
    pairs = record["request"]["headers"]
    found = [value for key, value in pairs if key.lower() == "x-emby-token"]
    require(len(found) == 1 and isinstance(found[0], str) and found[0], "An exact recorded token context is required.")
    return found[0]


class Authority:
    """Pin owned source and evidence bytes, metadata, roots, and an existing lock."""

    def __init__(self, path, checksum):
        self.input_descriptor = {"path": str(path), "sha256": checksum}
        self.raw = load_descriptor(self.input_descriptor)
        self.value = strict_json(self.raw)
        value = self.value
        required = {"schemaVersion", "kind", "runId", "ownerUid", "source", "preparationSource", "transportSource", "matrixSource",
            "preparationInput", "preparationState", "publicBaseline", "folderAuthority", "closedAuthentication", "actors", "process",
            "endpoint", "lock", "outputRoot", "outputParent", "readonlyRoots", "pins", "preservation", "expectedOriginalPolicySha256",
            "guidFolders", "budgets"}
        require(set(value) == required and value["schemaVersion"] == 1 and value["kind"] == "nextup-folder-grant-verification-input" and
                value["ownerUid"] == 0 and re.fullmatch(r"[A-Za-z0-9_-]{1,96}", value["runId"]), "The exact single-use input contract is required.")
        require(value["budgets"] == BUDGETS and value["endpoint"] == {"scheme": "http", "host": "127.0.0.1", "port": 18197},
                "Only the frozen reference endpoint and budgets are supported.")
        require(value["preparationSource"]["sha256"] == PREPARATION_SHA and value["transportSource"]["sha256"] == TRANSPORT_SHA and
                value["matrixSource"]["sha256"] == MATRIX_SHA and Path(value["source"]["path"]) == Path(__file__).absolute(),
                "The reviewed owned sources differ.")
        self.support = module_from(value["transportSource"], "folder_grant_transport")
        self.preparation = module_from(value["preparationSource"], "folder_grant_preparation")
        self.planner = module_from(value["matrixSource"], "folder_grant_matrix")
        require(set(value["folderAuthority"]) == {"input", "independent", "selectableResponse", "virtualFoldersResponse", "devicesResponse"},
                "The completed folder authority observation must be explicit.")
        self.descriptors = [self.input_descriptor, *[value[key] for key in ("source", "preparationSource", "transportSource", "matrixSource",
            "preparationInput", "preparationState", "publicBaseline")], *value["folderAuthority"].values(), *value["pins"]]
        self.prior = strict_json(load_descriptor(value["preparationInput"]))
        self.prior_state = strict_json(load_descriptor(value["preparationState"]))
        self.baseline = strict_json(load_descriptor(value["publicBaseline"]))
        credential_descriptor = self.prior["inputs"]["credentials"]
        self.descriptors.extend((credential_descriptor, self.prior["sources"]["proxy"]))
        credential_store = strict_json(load_descriptor(credential_descriptor))
        require(credential_store["runId"] == self.prior["runId"] == self.prior_state["runId"] and
                credential_store["schemaVersion"] == 1, "Preparation provenance identifies another run.")
        self.credentials = {actor: credential_store["accounts"][actor] for actor in ("admin", "P")}
        require(set(value["actors"]) == {"admin", "P"}, "Only the existing administrator and owned P are supported.")
        for actor, row in value["actors"].items():
            require(set(row) == {"userId", "username", "credentialRef", "deviceId"} and
                    row["userId"] == self.prior_state["userIds"][actor] and
                    all(row[key] == self.credentials[actor][key] for key in ("username", "credentialRef")) and
                    isinstance(self.credentials[actor]["password"], str) and len(self.credentials[actor]["password"]) >= 32 and
                    re.fullmatch(r"[A-Za-z0-9_-]{1,128}", row["deviceId"]), "An actor does not bind its retained credential and identity.")
        require(len({row["deviceId"] for row in value["actors"].values()}) == 2, "The two new device IDs must differ.")
        require(value["process"] == self.prior["process"] and value["endpoint"] == self.prior["endpoint"] and
                value["lock"] == self.prior["lock"], "The existing reference process, proxy, or lock differs.")
        require(self.prior["sources"]["proxy"]["path"] in value["process"]["endpoint"]["cmdline"],
                "The existing endpoint command does not bind its pinned owned proxy source.")
        require(len(value["preservation"]["userIds"]) == 8 and len(value["preservation"]["libraryIds"]) == 10 and
                set(value["preservation"]["userIds"]) == set(self.baseline["roster"]) and
                set(value["preservation"]["libraryIds"]) == set(self.baseline["libraries"]), "The complete retained public population differs.")
        expected_details = self.prior["preservation"]["detailRoutes"] + [
            {"group": "grant-P", "userId": value["actors"]["P"]["userId"], "itemId": self.prior_state["items"][item]["id"]}
            for item in EPISODES]
        require(value["preservation"]["detailRoutes"] == expected_details and len(expected_details) == 12,
                "The twelve explicit administrator-token subject detail witnesses differ.")
        self.closed_windows = []
        self._closure()
        self._folder_authority()
        policy = self.baseline["roster"][value["actors"]["P"]["userId"]]["Policy"]
        require(digest((canonical(policy) + "\n").encode()) == value["expectedOriginalPolicySha256"] and
                policy.get("EnabledFolders") == ["101", "103"] and policy.get("EnableAllFolders") is False and
                policy.get("IsAdministrator") is False and policy.get("IsDisabled") is False,
                "The retained complete P policy is not the reviewed narrow original policy.")
        self.original_policy = deepcopy(policy)
        self.grant_policy = deepcopy(policy)
        self.grant_policy["EnabledFolders"] = [row["guid"] for row in value["guidFolders"]]
        self.roots = {}
        require(isinstance(value["readonlyRoots"], list) and value["readonlyRoots"] and len(set(value["readonlyRoots"])) == len(value["readonlyRoots"]),
                "Frozen authority roots must be explicit and unique.")
        output, parent = Path(value["outputRoot"]), Path(value["outputParent"])
        require(output.parent == parent and not output.exists(), "The output must be one fresh direct child.")
        self.support.protected(parent, directory=True, uid=0)
        for root in value["readonlyRoots"]:
            info = self.support.protected(root, directory=True, uid=0)
            self.roots[root] = (info.st_dev, info.st_ino)
            bound = Path(root)
            require(output != bound and bound not in output.parents and output not in bound.parents,
                    "The new output overlaps a frozen authority root.")
        for original in self.prior["forbiddenOriginalRoots"]:
            require(not any(Path(original) == Path(row["path"]) or Path(original) in Path(row["path"]).parents
                            for row in self.descriptors), "No descriptor may read original implementation or data roots.")
        require(all(row == self.prior["sources"]["proxy"] or
                    any(Path(row["path"]) == Path(root) or Path(root) in Path(row["path"]).parents for root in self.roots)
                    for row in self.descriptors), "Every source and provenance record needs a frozen readonly root.")
        self.lock_fd = None
        self.check_files()

    def _closure(self):
        for row in self.value["closedAuthentication"]:
            require(set(row) == {"userId", "deviceId", "tokenSha256", "login", "logout", "rejection"}, "Closed-token evidence is incomplete.")
            self.descriptors.extend(row[key] for key in ("login", "logout", "rejection"))
            login, body = decode_record(row["login"], expected_status=200)
            logout, unused = decode_record(row["logout"], expected_status=204)
            rejection, unused = decode_record(row["rejection"], expected_status=401)
            require(body["ServerId"] == self.prior["server"]["id"] and body["User"]["Id"] == row["userId"] and
                    body["SessionInfo"]["UserId"] == row["userId"] and body["SessionInfo"]["DeviceId"] == row["deviceId"] and
                    digest(body["AccessToken"].encode()) == row["tokenSha256"] and
                    token_from_request(logout) == token_from_request(rejection) == body["AccessToken"] and
                    login["request"]["method"] == "POST" and login["request"]["route"] == "/emby/Users/AuthenticateByName" and
                    logout["request"]["method"] == "POST" and logout["request"]["route"] == "/emby/Sessions/Logout" and
                    rejection["request"]["method"] == "GET" and rejection["request"]["route"] in ("/emby/Sessions", "/emby/System/Info") and
                    instant(login["completedAt"]) <= instant(logout["completedAt"]) <= instant(rejection["completedAt"]),
                    "The actual ordered responses do not close one exact login token.")
            low = instant(body["User"]["LastLoginDate"])
            require(instant(login["completedAt"]) - timedelta(seconds=15) <= low <= instant(login["completedAt"]),
                    "The prior login date does not fall within its actual bounded login completion.")
            self.closed_windows.append({"userId": row["userId"], "deviceId": row["deviceId"], "from": low.isoformat(),
                "through": rejection["completedAt"]})
        require(self.closed_windows, "The previous owned authentication must be closed with actual records.")

    def _folder_authority(self):
        value = self.value
        authority = value["folderAuthority"]
        self.folder_input = strict_json(load_descriptor(authority["input"]))
        self.folder_independent = strict_json(load_descriptor(authority["independent"]))
        records = {}
        for name, route in (("selectableResponse", "/emby/Library/SelectableMediaFolders"),
                            ("virtualFoldersResponse", "/emby/Library/VirtualFolders/Query"), ("devicesResponse", "/emby/Devices")):
            record, body = decode_record(authority[name], expected_status=200)
            require(record["request"]["method"] == "GET" and record["request"]["route"] == route,
                    "A folder authority wire response has a different route.")
            records[name] = (record, body)
        tokens = {digest(token_from_request(record).encode()) for record, body in records.values()}
        require(len(tokens) == 1 and tokens <= {row["tokenSha256"] for row in value["closedAuthentication"]},
                "Folder observations must use one actually closed owned token.")
        self.old_devices = self.preparation.devices_page(records["devicesResponse"][1])
        require(len(self.old_devices) == 91 and set(self.baseline["devices"]) <= set(self.old_devices) and
                not {row["deviceId"] for row in value["actors"].values()}.intersection(
                    row["ReportedDeviceId"] for row in self.old_devices.values()), "The retained device population or new device absence differs.")
        require([row.get("libraryId") for row in value["guidFolders"]] == ["101", "103"] and
                all(set(row) == {"libraryId", "guid"} and isinstance(row["guid"], str) and uuid.UUID(row["guid"])
                    for row in value["guidFolders"]) and len({row["guid"] for row in value["guidFolders"]}) == 2,
                "Exactly two distinct observed Guid grants are required.")
        independent = self.folder_independent
        require(independent.get("schemaVersion") == 1 and independent.get("kind") == "nextup-folder-authority-independent-terminal" and
                independent.get("status") == "folder_identifiers_independently_observed_and_session_closed" and
                independent.get("input") == authority["input"] and independent.get("runId") == self.folder_input["runId"] and
                independent.get("devicesAfter") == 91 and independent.get("exactTokenClosed") is True and
                independent.get("grantSemanticsProven") is False and independent.get("fullReferencePublicSnapshotClaimed") is False and
                independent.get("recursiveCgroupEmpty") is True and independent.get("requestCount") == 6,
                "The actual independent folder observation is not the completed closed authority.")
        unit = independent["unit"]["properties"]
        require(unit["MainPID"] == "0" and unit["Result"] == "success" and unit["ExecMainStatus"] == "0" and
                (unit["ActiveState"], unit["SubState"]) in (("active", "exited"), ("inactive", "dead")),
                "The folder observation unit was not independently closed.")
        for key in ("authenticationClosure", "preservationAfter", "preservationBefore", "producerTerminal", "scopeInventory", "source", "wireIndex"):
            load_descriptor(independent[key])
            self.descriptors.append(independent[key])
        terminal = strict_json(load_descriptor(independent["producerTerminal"]))
        require(terminal.get("runId") == self.folder_input["runId"] and terminal.get("status") == "awaiting_independent_attestation" and
                terminal.get("requestCount") == 6 and terminal.get("closed") is True and terminal.get("completed") is True and
                terminal.get("uncertain") is False and terminal.get("pendingPresent") is False,
                "The original folder observer did not produce its completed closed terminal.")
        selectable, virtual = records["selectableResponse"][1], self.preparation.page(records["virtualFoldersResponse"][1], "ItemId", 16)
        require(isinstance(selectable, list) and len(selectable) <= 64 and len(virtual) == 10 and
                self.folder_input["process"] == value["process"] and self.folder_input["endpoint"] == value["endpoint"] and
                self.folder_input["lock"] == value["lock"], "Folder observation provenance identifies another target.")
        expected_mapping = []
        for symbol, grant in zip(("LA", "LB"), value["guidFolders"]):
            library = self.prior_state["libraries"][symbol]
            root = self.prior["media"]["roots"][symbol]
            selected = [row for row in selectable if row.get("Id") == grant["libraryId"]]
            require(len(selected) == 1, "An intended selectable library is missing or ambiguous.")
            row = selected[0]
            expected = {"Name": self.prior["libraries"][symbol]["name"], "Id": grant["libraryId"], "Guid": grant["guid"],
                "SubFolders": [{"Name": symbol, "Id": library["sourceRootId"], "Path": root, "IsUserAccessConfigurable": True}],
                "IsUserAccessConfigurable": True}
            require(row == expected and virtual[grant["libraryId"]]["Locations"] == [root] and
                    library["libraryId"] == library["viewId"] == grant["libraryId"],
                    "The raw Guid, native library, source-root, or path relationship differs.")
            expected_mapping.append({"isUserAccessConfigurable": True, "libraryId": grant["libraryId"], "rootPath": root,
                "selectableGuid": grant["guid"], "selectableId": grant["libraryId"], "selectableSubFolderId": library["sourceRootId"],
                "sourceRootId": library["sourceRootId"], "symbol": symbol, "viewId": library["viewId"]})
        require(independent["mapping"] == expected_mapping, "The independent folder mapping differs from its actual raw observations.")
        self.folder_records = records

    def check_files(self):
        require(canonical(self.value) == canonical(strict_json(self.raw)), "The in-memory frozen input changed.")
        for row in self.descriptors:
            load_descriptor(row)
        for root, expected in self.roots.items():
            info = self.support.protected(root, directory=True, uid=0)
            require((info.st_dev, info.st_ino) == expected, "A frozen authority root was replaced.")

    def acquire(self):
        require(self.lock_fd is None, "The existing lock cannot be reacquired.")
        lock = self.value["lock"]
        info = self.support.protected(lock["path"], uid=0)
        require((info.st_dev, info.st_ino) == (lock["device"], lock["inode"]), "The existing fixture lock changed.")
        self.lock_fd = os.open(lock["path"], os.O_RDWR | os.O_NOFOLLOW)
        fcntl.flock(self.lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.check()

    def check(self):
        self.check_files()
        require(self.lock_fd is not None, "The fixture lock is not held.")
        lock = self.value["lock"]
        info, opened = self.support.protected(lock["path"], uid=0), os.fstat(self.lock_fd)
        require((info.st_dev, info.st_ino) == (opened.st_dev, opened.st_ino) == (lock["device"], lock["inode"]),
                "The held fixture lock changed.")
        require(self.support.process_identity(self.value["process"]) == self.value["process"], "The reference metadata or existing proxy listener changed.")

    def close(self):
        if self.lock_fd is not None:
            os.close(self.lock_fd)
            self.lock_fd = None


class Runner:
    """A narrow acknowledged-policy state machine with no replay or recovery entry."""

    def __init__(self, authority, *, wire=None, journal_factory=None, monotonic=time.monotonic, now=utc_now):
        self.authority, self.value = authority, authority.value
        self.support, self.preparation, self.planner = authority.support, authority.preparation, authority.planner
        self.wire = wire or self.support.HTTPTransport(self.value["endpoint"])
        self.journal_factory = journal_factory or (lambda root: self.support.Journal(root, uid=0))
        self.monotonic, self.utc_now = monotonic, now
        self.journal = None
        self.started = self.cleanup_started = self.last_clock = None
        self.phase, self.policy_state = "normal", "original"
        self.count = self.normal_count = self.cleanup_count = self.charged_bytes = 0
        self.pending = self.failure = None
        self.uncertain = self.journal_failed = self.used = self.proven = self.restored = self.preserved = False
        self.tokens, self.sessions, self.logins, self.windows, self.events = {}, {}, {}, {}, {}
        self.revoked, self.labels = set(), set()
        self.user_ids = {actor: row["userId"] for actor, row in self.value["actors"].items()}
        self.items, self.libraries = deepcopy(authority.prior_state["items"]), deepcopy(authority.prior_state["libraries"])
        self.manifest = deepcopy(authority.prior)
        self.manifest["preservation"] = deepcopy(self.value["preservation"])
        self.manifest["actors"] = deepcopy(self.value["actors"])
        self.media, self.catalog_ids = {}, set()
        for symbol in ("LA", "LB"):
            name = self.manifest["libraries"][symbol]["seriesName"]
            root = Path(self.manifest["media"]["roots"][symbol]) / name
            self.media[symbol] = {"files": {}}
            for index, (season, episode) in enumerate(((1, 1), (1, 2), (2, 1)), 1):
                item = symbol[1] + str(index)
                path = root / ("Season %02d" % season) / ("%s S%02dE%02d.mp4" % (name, season, episode))
                self.media[symbol]["files"][item] = {"path": str(path), "sha256": self.items[item]["mediaSha256"]}
        self.secrets = {row["password"] for row in authority.credentials.values()}
        self.secrets.update(row["path"] for row in authority.descriptors)
        self.secrets.update(self.value["readonlyRoots"])
        self.secrets.update(row["deviceId"] for row in self.value["actors"].values())
        self.before = self.after = None

    def _now(self):
        value = self.monotonic()
        require(type(value) in (int, float) and math.isfinite(value) and
                (self.last_clock is None or value >= self.last_clock), "The monotonic clock changed direction or became nonfinite.")
        self.last_clock = value
        return value

    def _state(self):
        return {"schemaVersion": 1, "runId": self.value["runId"], "inputSha256": digest(self.authority.raw), "phase": self.phase,
            "policyState": self.policy_state, "requestCount": self.count, "normalRequestCount": self.normal_count,
            "cleanupRequestCount": self.cleanup_count, "chargedResponseBytes": self.charged_bytes, "pending": self.pending,
            "uncertain": self.uncertain, "tokens": self.tokens, "sessions": self.sessions, "windows": self.windows,
            "revoked": sorted(self.revoked), "failure": self.failure, "grantProven": self.proven,
            "originalPolicyRestored": self.restored, "publicPreserved": self.preserved}

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

    def _query(self, user, *, parent=None, ids=None):
        return self.preparation.PreparationRunner._query(self, user, parent=parent, ids=ids)

    def _snapshot(self, prefix):
        return self.preparation.PreparationRunner._snapshot(self, prefix)

    def _detail(self, actor, item, label):
        body = self.preparation.PreparationRunner._detail(self, actor, item, label)
        require(body.get("SeasonId") == self.items[item]["parentId"], "The full episode SeasonId differs from its actual mapped parent.")
        return body

    def _catalog_map(self, rows, symbol):
        return self.preparation.PreparationRunner._catalog_map(self, rows, symbol)

    def _collect(self, value):
        if isinstance(value, dict):
            for key, item in value.items():
                if key.lower() in self.support.SECRET_KEYS and isinstance(item, str) and item:
                    self.secrets.add(item)
                self._collect(item)
        elif isinstance(value, list):
            for item in value:
                self._collect(item)

    def _guard_request(self, label, actor, method, route, body, form):
        own = self.user_ids["P"]
        if method == "POST":
            if label == "login-" + actor:
                credential = self.authority.credentials[actor]
                require(self.phase == "normal" and actor not in self.tokens and route == "/emby/Users/AuthenticateByName" and form and
                        body == {"Username": credential["username"], "Pw": credential["password"]}, "Only one explicit actor login is allowed.")
            elif label in ("grant-policy", "restore-policy"):
                restoring = label == "restore-policy"
                require(actor == "admin" and actor in self.tokens and route == "/emby/Users/" + own + "/Policy" and not form and
                        self.phase == ("cleanup" if restoring else "normal") and
                        canonical(body) == canonical(self.authority.original_policy if restoring else self.authority.grant_policy),
                        "A policy mutation escaped its exact complete target policy.")
            else:
                require(label == "logout-" + actor and self.phase == "cleanup" and actor in self.tokens and actor not in self.revoked and
                        route == "/emby/Sessions/Logout" and body is None and not form, "No other POST route is authorized.")
            return
        require(method == "GET" and body is None and not form and actor in self.tokens, "Only owned authenticated GETs are authorized.")
        if label == "rejection-" + actor:
            require(self.phase == "cleanup" and route == "/emby/Sessions" and "logout-" + actor in self.events,
                    "The rejection probe must follow this token's one logout.")
            return
        require(actor not in self.revoked and "logout-" + actor not in self.events, "A logged-out token cannot perform another read.")
        if actor == "P":
            allowed = {"/emby/Users/" + own, "/emby/Users/" + own + "/Views"}
            if label == "own-P-selectable-folders" and self.phase == "normal" and "own-P-views" in self.events:
                allowed.add("/emby/Library/SelectableMediaFolders")
            allowed.update(self._query(own, parent=self.libraries[symbol]["viewId"]) for symbol in ("LA", "LB"))
            allowed.update("/emby/Users/" + own + "/Items/" + self.items[item]["id"] for item in EPISODES)
            require(route in allowed, "P cannot read another account, item, or catalog.")
        else:
            allowed = {"/emby/System/Info/Public", "/emby/Users", "/emby/Library/VirtualFolders/Query", "/emby/Devices",
                       "/emby/System/Configuration", "/emby/Users/" + own}
            allowed.update("/emby/UserSettings/" + user for user in self.value["preservation"]["userIds"])
            allowed.update(self._query(self.user_ids["admin"], parent=library) for library in self.value["preservation"]["libraryIds"])
            allowed.update(self._query(user, ids=self.catalog_ids) for user in self.value["preservation"]["userIds"])
            allowed.update("/emby/Users/" + row["userId"] + "/Items/" + row["itemId"] for row in self.value["preservation"]["detailRoutes"])
            require(route in allowed, "An administrator read escaped the exact public snapshot contract.")

    def _dispatch(self, label, actor, method, route, body=None, *, form=False, acknowledge=None):
        require(not self.journal_failed and not self.uncertain and self.pending is None and actor in self.value["actors"] and
                label not in self.labels and re.fullmatch(r"[A-Za-z0-9_-]{1,88}", label), "A closed, repeated, or unresolved request cannot dispatch.")
        self._guard_request(label, actor, method, route, body, form)
        cleanup = self.phase == "cleanup"
        require(self.count < BUDGETS["totalRequests"] and (self.cleanup_count < BUDGETS["cleanupRequests"] if cleanup else
                self.normal_count < BUDGETS["normalRequests"]), "The fixed request reserve is exhausted.")
        deadline = (self.cleanup_started + BUDGETS["cleanupSeconds"] if cleanup else self.started + BUDGETS["normalSeconds"])
        require(self._now() < deadline, "The phase deadline expired.")
        require(BUDGETS["totalResponseBytes"] - self.charged_bytes - (0 if cleanup else BUDGETS["cleanupResponseBytes"]) >=
                BUDGETS["responseBytes"] + 1, "The normal phase cannot spend the cleanup byte reserve.")
        metadata = self.value["actors"][actor]
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="' + CLIENT + '", Device="' + DEVICE +
            '", DeviceId="' + metadata["deviceId"] + '", Version="' + VERSION + '"'}
        if actor in self.tokens:
            headers["X-Emby-Token"] = self.tokens[actor]
        payload = None if body is None else (urlencode(body).encode() if form else canonical(body).encode())
        if payload is not None:
            headers["Content-Type"] = "application/x-www-form-urlencoded; charset=utf-8" if form else "application/json"
        request = SimpleNamespace(method=method, route=route, label=label, actor=actor, cleanup=cleanup)
        self.support.request_metadata_size(request, headers)
        require(payload is None or len(payload) <= BUDGETS["requestBytes"], "The request body exceeds its bound.")
        self.authority.check(); self.journal.check()
        ordinal, created = self.count + 1, self.utc_now()
        stem = "%04d-%s" % (ordinal, label)
        captured_request = {"method": method, "route": route, "headers": [[key, val] for key, val in headers.items()], "body": body}
        intent = {"ordinal": ordinal, "actor": actor, "label": label, "phase": self.phase, "createdAt": created,
            "request": captured_request, "payloadBase64": None if payload is None else base64.b64encode(payload).decode(),
            "tokenSha256": digest(self.tokens[actor].encode()) if actor in self.tokens else None}
        checksum = self._save(stem + "-intent.json", intent)
        self.pending = {"ordinal": ordinal, "label": label, "actor": actor, "intentSha256": checksum}
        self.count += 1; self.normal_count += int(not cleanup); self.cleanup_count += int(cleanup); self.labels.add(label)
        self._persist()
        try:
            self.authority.check(); self.journal.check()
            remaining = deadline - self._now()
            require(remaining > 0, "Persistence consumed the dispatch deadline.")
            response = self.wire.send(request, headers, payload, timeout_seconds=min(BUDGETS["requestSeconds"], remaining),
                                      max_bytes=BUDGETS["responseBytes"] + 1)
            require(isinstance(response.raw, bytes), "The transport did not return actual response bytes.")
            raw = response.raw[:BUDGETS["responseBytes"] + 1]
            response_headers = self.support.bounded_header_prefix(response.headers)
            complete = (response.complete_http is True and response.failure is None and len(response.raw) <= BUDGETS["responseBytes"] and
                        len(response_headers) == len(response.headers))
            record = {"ordinal": ordinal, "actor": actor, "label": label, "request": captured_request, "status": response.status,
                "headers": response_headers, "rawBase64": base64.b64encode(raw).decode(), "observedRawBytes": len(response.raw),
                "retainedRawTruncated": len(raw) != len(response.raw), "completeHttp": complete,
                "completedAt": response.completed_at, "failure": response.failure}
            response_sha = self._save(stem + "-response.json", record)
            self.pending["responseReceiptSha256"] = response_sha
            self.charged_bytes += len(raw) if complete else BUDGETS["responseBytes"] + 1
            self._persist()
            require(complete and type(response.status) is int and 100 <= response.status <= 599 and
                    instant(created) <= instant(response.completed_at), "The attempt did not produce a complete bounded timed HTTP response.")
            decoded = decode_payload(raw, response_headers)
            self._collect(decoded)
            event = {"ordinal": ordinal, "actor": actor, "createdAt": created, "completedAt": response.completed_at,
                "responseReceiptSha256": response_sha, "status": response.status, "body": decoded}
            if acknowledge is not None:
                acknowledge(event)
                self._persist()
            self.events[label] = event
            self._save(stem + "-response.json", self.support.sanitized({"event": event, "headers": [
                [key, "[redacted]" if key.lower() in self.support.SECRET_KEYS else val] for key, val in response_headers]}, self.secrets), export=True)
            self.pending = None
            self._persist()
            return event
        except BaseException:
            self.uncertain = True
            if not self.journal_failed:
                self._persist()
            raise

    def _get(self, label, actor, route):
        event = self._dispatch(label, actor, "GET", route)
        require(event["status"] == 200, "The completed public read was not HTTP 200.")
        return event["body"]

    def _login(self, actor):
        credential = self.authority.credentials[actor]
        def acknowledge(event):
            body = event["body"]
            require(event["status"] == 200 and isinstance(body, dict) and body.get("ServerId") == self.manifest["server"]["id"] and
                    isinstance(body.get("AccessToken"), str) and body["AccessToken"] and isinstance(body.get("User"), dict) and
                    isinstance(body.get("SessionInfo"), dict), "The login did not acknowledge complete token ownership.")
            user, session = body["User"], body["SessionInfo"]
            require(user.get("Id") == self.user_ids[actor] and user.get("Name") == credential["username"] and
                    user.get("Policy", {}).get("IsAdministrator") is (actor == "admin") and
                    session.get("UserId") == self.user_ids[actor] and session.get("DeviceId") == self.value["actors"][actor]["deviceId"] and
                    isinstance(session.get("Id"), str) and session["Id"] and session["Id"] not in self.sessions.values() and
                    body["AccessToken"] not in self.tokens.values(), "The login principal, device, token, or session differs.")
            self.tokens[actor], self.sessions[actor] = body["AccessToken"], session["Id"]
            self.secrets.update((body["AccessToken"], session["Id"]))
            self.logins[actor] = event
            self.windows[actor] = {"from": event["createdAt"], "through": event["completedAt"]}
        return self._dispatch("login-" + actor, actor, "POST", "/emby/Users/AuthenticateByName",
            {"Username": credential["username"], "Pw": credential["password"]}, form=True, acknowledge=acknowledge)

    def _write_policy(self, *, restore):
        require(self.policy_state == ("granted" if restore else "original"), "The complete policy write is not in its one permitted state.")
        expected = self.authority.original_policy if restore else self.authority.grant_policy
        def acknowledge(event):
            require(event["status"] in (200, 204), "The complete policy write lacks a successful acknowledgement.")
            self.policy_state = "restore-acknowledged" if restore else "grant-acknowledged"
        return self._dispatch("restore-policy" if restore else "grant-policy", "admin", "POST",
            "/emby/Users/" + self.user_ids["P"] + "/Policy", deepcopy(expected), acknowledge=acknowledge)

    def _profile(self, label, actor, expected):
        def validate(event):
            body = event["body"]
            require(event["status"] == 200 and isinstance(body, dict) and body.get("Id") == self.user_ids["P"] and
                    body.get("Name") == self.value["actors"]["P"]["username"] and
                    canonical(body.get("Policy")) == canonical(expected), "The complete P policy readback differs.")
        if label in ("granted-P-policy", "restored-P-policy"):
            def acknowledge(event):
                validate(event)
                self.policy_state = "granted" if label == "granted-P-policy" else "restored"
            return self._dispatch(label, actor, "GET", "/emby/Users/" + self.user_ids["P"], acknowledge=acknowledge)["body"]
        event = self._dispatch(label, actor, "GET", "/emby/Users/" + self.user_ids["P"])
        validate(event)
        return event["body"]

    def _logout(self, actor):
        require(actor in self.tokens and actor not in self.revoked, "Only an acknowledged current token may be closed.")
        def logout_ack(event):
            require(event["status"] == 204, "The exact-token logout was not acknowledged.")
        self._dispatch("logout-" + actor, actor, "POST", "/emby/Sessions/Logout", acknowledge=logout_ack)
        def rejection_ack(event):
            require(event["status"] == 401, "The same logged-out token was not rejected.")
            self.revoked.add(actor)
        self._dispatch("rejection-" + actor, actor, "GET", "/emby/Sessions", acknowledge=rejection_ack)

    def _devices(self, label):
        devices = self.preparation.devices_page(self._get(label, "admin", "/emby/Devices"))
        baseline = self.authority.old_devices
        require(set(baseline) <= set(devices), "A retained device disappeared.")
        for key, row in baseline.items():
            current = devices[key]
            require(canonical({name: item for name, item in row.items() if name != "DateLastActivity"}) ==
                    canonical({name: item for name, item in current.items() if name != "DateLastActivity"}), "A retained old device structure changed.")
            if row.get("DateLastActivity") != current.get("DateLastActivity"):
                windows = [window for window in self.authority.closed_windows if window["deviceId"] == row["ReportedDeviceId"]]
                require(any(instant(window["from"]).replace(microsecond=0) <= instant(current["DateLastActivity"]) <= instant(window["through"])
                            for window in windows), "An old device date changed outside its already closed token window.")
        owned = set()
        for actor in self.tokens:
            metadata = self.value["actors"][actor]
            selected = [row for row in devices.values() if row["ReportedDeviceId"] == metadata["deviceId"]]
            require(len(selected) == 1, "The acknowledged new device is absent or duplicated.")
            row = selected[0]
            require(row["Id"] not in baseline and row["Id"] not in owned and row.get("LastUserId") == self.user_ids[actor] and
                    row.get("LastUserName") == metadata["username"] and row.get("AppName") == CLIENT and row.get("Name") == DEVICE and
                    row.get("AppVersion") == VERSION and
                    instant(self.windows[actor]["from"]).replace(microsecond=0) <= instant(row.get("DateLastActivity")) <= instant(self.utc_now()),
                    "A new device is outside its exact login metadata or observed time window.")
            owned.add(row["Id"])
        require(set(devices) - set(baseline) == owned, "The current device population contains an unowned addition.")
        return devices

    def _preserve(self, before, after):
        changes = []
        require(instant(before["captured_at"]) <= instant(after["captured_at"]), "Snapshot time moved backwards.")
        for key in ("server", "configuration", "libraries", "catalog_by_library", "preferences", "items_by_user"):
            require(canonical(before[key]) == canonical(after[key]), "A complete retained public section changed: " + key)
        for group, rows in before["details"].items():
            require(canonical(rows) == canonical(after["details"].get(group)), "A complete retained detail group changed.")
        require(set(before["roster"]) == set(after["roster"]), "The user roster changed.")
        for user in sorted(before["roster"]):
            old, current = before["roster"][user], after["roster"][user]
            allowed = {"LastLoginDate", "LastActivityDate"} if user in self.user_ids.values() else set()
            require(canonical({key: val for key, val in old.items() if key not in allowed}) ==
                    canonical({key: val for key, val in current.items() if key not in allowed}), "A full retained profile changed outside owned authentication dates.")
            for key in sorted(allowed):
                if old.get(key) == current.get(key) and (key in old) == (key in current):
                    continue
                windows = [row for row in self.authority.closed_windows if row["userId"] == user]
                windows += [{"from": self.windows[actor]["from"], "through": after["captured_at"]}
                            for actor in self.windows if self.user_ids[actor] == user]
                require(any(instant(row["from"]) <= instant(current[key]) <= instant(row["through"]) for row in windows),
                        "An authentication date escaped every actual login window.")
                changes.append({"kind": "owned-authentication-time", "userId": user, "field": key})
        require(set(before["devices"]) <= set(after["devices"]), "A retained device disappeared.")
        for key in sorted(after["devices"]):
            row, old = after["devices"][key], before["devices"].get(key)
            reported = row["ReportedDeviceId"]
            windows = [entry for entry in self.authority.closed_windows if entry["deviceId"] == reported]
            windows += [{"from": self.windows[actor]["from"], "through": after["captured_at"]}
                        for actor in self.windows if self.value["actors"][actor]["deviceId"] == reported]
            if old is not None:
                require(canonical({name: val for name, val in old.items() if name != "DateLastActivity"}) ==
                        canonical({name: val for name, val in row.items() if name != "DateLastActivity"}), "A retained device structure changed.")
            if old is None or old.get("DateLastActivity") != row.get("DateLastActivity"):
                require(any(instant(window["from"]).replace(microsecond=0) <= instant(row["DateLastActivity"]) <= instant(window["through"])
                            for window in windows), "A device date escaped every exact whole-second login window.")
                changes.append({"kind": "owned-device-time", "deviceId": key})
        return {"preserved": True, "allowedAuthenticationChanges": changes,
            "beforeTokenSha256": before["credential_context"]["token_sha256"], "afterTokenSha256": after["credential_context"]["token_sha256"]}

    def _normal(self):
        self._login("admin")
        self.before = self._snapshot("before")
        observed_libraries = self.preparation.page(self.authority.folder_records["virtualFoldersResponse"][1], "ItemId", 16)
        require(canonical(self.before["libraries"]) == canonical(self.authority.baseline["libraries"]) == canonical(observed_libraries),
                "The fresh complete library management DTOs differ from the frozen Guid authority and historical identity.")
        for item in EPISODES:
            body = self.before["details"]["grant-P"][self.items[item]["id"]]
            context = SimpleNamespace(user_ids=self.user_ids, items=self.items, planner=self.planner,
                _get=lambda label, actor, route, captured=body: captured)
            self.preparation.PreparationRunner._detail(context, "P", item, "recorded-subject-P-witness")
            require(body.get("SeasonId") == self.items[item]["parentId"] and self.planner.zero_state(self.planner.userdata_fact(body)),
                    "The administrator-token subject-P before witness has another season or nonzero history.")
        self._save("before-context.json", {"historicalSnapshotSha256": self.value["publicBaseline"]["sha256"],
            "latestDeviceResponseSha256": self.value["folderAuthority"]["devicesResponse"]["sha256"],
            "completeBeforeSnapshot": "before-public.json", "historicalSnapshotReplaced": False,
            "newEpisodeWitnessContext": "administrator-token subject-P projection", "historicalOwnTokenBaselineClaimed": False})
        self._profile("original-P-policy", "admin", self.authority.original_policy)
        self._write_policy(restore=False)
        self._profile("granted-P-policy", "admin", self.authority.grant_policy)
        self._login("P")
        self._profile("own-P-profile", "P", self.authority.grant_policy)
        views = self.preparation.page(self._get("own-P-views", "P", "/emby/Users/" + self.user_ids["P"] + "/Views"))
        require(set(views) == {"101", "103"}, "Own-token Views do not prove exactly the two intended libraries.")
        self._dispatch("own-P-selectable-folders", "P", "GET", "/emby/Library/SelectableMediaFolders")
        prepared_ids = {row["id"] for row in self.items.values()}
        self.catalog_ids -= prepared_ids
        for symbol in ("LA", "LB"):
            rows = self.preparation.page(self._get("own-P-catalog-" + symbol, "P", self._query(self.user_ids["P"], parent=self.libraries[symbol]["viewId"])))
            mapped = self._catalog_map(rows, symbol)
            require(canonical(mapped) == canonical({key: row for key, row in self.items.items() if row["library"] == symbol}),
                    "The own-token catalog differs from the twelve retained semantic item mappings.")
        for item in EPISODES:
            body = self._detail("P", item, "own-P-detail-" + item)
            require(self.planner.zero_state(self.planner.userdata_fact(body)), "The first own-token episode detail does not prove complete zero history.")
            witness = self.before["details"]["grant-P"][self.items[item]["id"]]
            require(canonical(body["UserData"]) == canonical(witness["UserData"]), "Own-token episode history differs from the administrator-token subject-P before witness.")
        self.proven = True
        self._persist()

    def _cleanup(self):
        require(self.pending is None and not self.uncertain and not self.journal_failed, "An unresolved attempt cannot trigger blind cleanup.")
        require(self.policy_state in ("original", "granted", "restored"), "A policy lacking complete readback cannot trigger automatic recovery.")
        self.phase, self.cleanup_started = "cleanup", self._now()
        self._persist()
        errors = []
        if self.policy_state == "granted":
            self._write_policy(restore=True)
            self._profile("restored-P-policy", "admin", self.authority.original_policy)
            if "P" in self.tokens:
                try:
                    views = self.preparation.page(self._get("restored-own-P-views", "P", "/emby/Users/" + self.user_ids["P"] + "/Views"))
                    require(not views, "The restored own-token P Views are not empty.")
                except BaseException as error:
                    if self.uncertain or self.pending is not None or self.journal_failed:
                        raise
                    errors.append(error)
            self.restored = True
            self._persist()
        if self.before is not None and "admin" in self.tokens:
            try:
                self.after = self._snapshot("after")
                self._save("public-preservation.json", self._preserve(self.before, self.after))
                self.preserved = True
                self._persist()
            except BaseException as error:
                if self.uncertain or self.pending is not None or self.journal_failed:
                    raise
                errors.append(error)
        for actor in ("P", "admin"):
            if actor in self.tokens and actor not in self.revoked:
                self._logout(actor)
        if errors:
            raise errors[0]

    def run(self):
        require(not self.used, "This worker has no resume or replay operation.")
        self.used = True
        terminal = None
        try:
            self.authority.acquire()
            self.journal = self.journal_factory(self.value["outputRoot"])
            self.started = self._now()
            self._save("manifest.json", self.value)
            self._save("original-policy.json", self.authority.original_policy)
            self._save("target-policy.json", self.authority.grant_policy)
            self._persist()
            try:
                self._normal()
            except BaseException as error:
                self.failure = {"type": type(error).__name__, "stage": "normal"}
            if not self.uncertain and self.pending is None and not self.journal_failed:
                try:
                    self._cleanup()
                except BaseException as error:
                    self.failure = {"type": type(error).__name__, "stage": "cleanup"}
            if not self.journal_failed:
                self._persist()
                closed = set(self.tokens) == self.revoked
                completed = self.proven and self.restored and self.preserved and closed and not self.uncertain and self.pending is None and self.failure is None
                terminal = {"schemaVersion": 1, "kind": "nextup-folder-grant-verification-terminal", "runId": self.value["runId"],
                    "status": "awaiting_independent_attestation" if completed else "recovery_required" if
                        self.uncertain or self.pending is not None or not closed or self.policy_state not in ("original", "restored") else "stopped_with_known_cleanup",
                    "requestCount": self.count, "normalRequestCount": self.normal_count, "cleanupRequestCount": self.cleanup_count,
                    "grantSemanticsProven": self.proven, "originalPolicyRestored": self.restored, "publicPreserved": self.preserved,
                    "allKnownTokensClosed": closed, "pendingPresent": self.pending is not None, "uncertain": self.uncertain,
                    "failure": self.failure, "originalImplementationBytesRead": False, "referenceDatabaseRead": False,
                    "inputSha256": digest(self.authority.raw)}
                self._save("terminal.json", terminal)
                self._save("terminal.json", terminal, export=True)
            return terminal
        finally:
            if self.journal is not None:
                self.journal.close()
            self.authority.close()


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and sys.flags.isolated and sys.flags.dont_write_bytecode and
            os.environ.get("SSH_CONNECTION"), "Only the authorized remote Python -I -B environment is supported.")
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument("input", type=Path)
    parser.add_argument("sha256")
    parser.add_argument("--plan", action="store_true")
    args = parser.parse_args()
    authority = Authority(args.input, args.sha256)
    if args.plan:
        print(canonical({"kind": "nextup-folder-grant-verification-plan", "inputSha256": digest(authority.raw),
            "expectedNormalRequests": 59, "expectedCleanupRequests": 50, "expectedTotalRequests": 109, "businessHttpRequests": 0}))
        return 0
    terminal = Runner(authority).run()
    print(canonical(terminal if terminal is not None else {"status": "journal_failure_recovery_required"}))
    return 0 if terminal is not None and terminal["status"] == "awaiting_independent_attestation" else 2


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(canonical({"status": "entry_or_persistence_failure", "type": type(error).__name__}), file=sys.stderr)
        raise SystemExit(2)
