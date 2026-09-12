#!/usr/bin/env python3
"""Remote-only synthetic guards for the bounded reference preparation producer.

Each case owns a fresh temporary scope, copied reviewed source bytes, synthetic
credentials and public state, and an in-memory transport. No reference service,
original implementation or actual process is observed. An explicit optional
audit mode reads pinned completed owned evidence and performs no HTTP.
Run only from an authorized Linux SSH session with explicit source descriptors.
"""

from __future__ import annotations

import argparse
import base64
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlencode, urlsplit
import uuid


sys.dont_write_bytecode = True
PREPARATION_SOURCE = None
TRANSPORT_SOURCE = None
MATRIX_SOURCE = None
ACTORS = ("P", "Q")
EPISODES = ("A1", "A2", "A3", "B1", "B2", "B3")
SUMMARIES = ("A", "AS1", "AS2", "B", "BS1", "BS2")
RUNTIME_TICKS = 6_000_000_000
PARTIAL_TICKS = 1_200_000_000
STAMP = "2026-09-13T01:00:00Z"
PRESERVATION_ORDER_OBSERVATION = None


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n").encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def load_source(path, label):
    name = label + "_" + uuid.uuid4().hex
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def zero_state(item):
    return {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0,
            "LastPlayedDate": None, "IsFavorite": False, "Key": "synthetic-" + item}


class FakeClock:
    """Expose deterministic time and waits without calling the operating clock."""

    def __init__(self):
        self.value = 100.0
        self.waits = []
        self.base = datetime(2026, 9, 13, 1, tzinfo=timezone.utc)

    def __call__(self):
        self.value += 0.001
        return self.value

    def sleep(self, seconds):
        if not (isinstance(seconds, (int, float)) and 0 < seconds <= 5):
            raise AssertionError("A synthetic wait exceeded its bounded interval.")
        self.waits.append(seconds)
        self.value += seconds

    def utc_now(self):
        return (self.base + timedelta(seconds=self.value)).isoformat()


class FakeAuthority:
    """Provide only case-owned synthetic authority and record every checkpoint."""

    def __init__(self, fixture):
        self.fixture = fixture
        self.manifest = deepcopy(fixture.manifest)
        self.support = fixture.support
        self.planner = fixture.planner
        self.credentials = deepcopy(fixture.credentials)
        self.baseline = deepcopy(fixture.baseline)
        self.release = deepcopy(fixture.release)
        self.acquired = False
        self.closed = False
        self.checks = 0
        self.staged = False
        self.failure_at = None

    def acquire(self):
        if self.acquired or self.closed:
            raise AssertionError("Synthetic authority cannot be reacquired or resumed.")
        self.acquired = True
        self.check()

    def check(self):
        if not self.acquired or self.closed:
            raise AssertionError("The producer checked unavailable synthetic authority.")
        self.checks += 1
        if self.failure_at is not None and self.checks >= self.failure_at:
            raise self.fixture.module.PreparationError("Synthetic frozen authority drift.")

    def stage_media(self, journal):
        self.check()
        if self.staged:
            raise AssertionError("Synthetic media was staged more than once.")
        self.staged = True
        for library in ("LA", "LB"):
            for row in self.fixture.media[library]["files"].values():
                path = Path(row["path"])
                path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                self.fixture.write(path, self.fixture.media_bytes)
        return deepcopy(self.fixture.media)

    def close(self):
        self.closed = True


class PreparationFixture:
    """Build fresh public DTOs and authority files without borrowed evidence."""

    def __init__(self, testcase):
        self.temporary = tempfile.TemporaryDirectory(prefix="nextup-preparation-guards-")
        testcase.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.clock = FakeClock()
        self.scope = {}
        for name in ("fixtureRoot", "sourceRoot", "proxySourceRoot", "evidenceParent"):
            path = self.root / name
            path.mkdir(mode=0o700)
            self.scope[name] = str(path)
        self.scope["outputRoot"] = str(Path(self.scope["fixtureRoot"]) / "fresh-preparation")
        self.scope["matrixEvidenceRoot"] = str(Path(self.scope["evidenceParent"]) / "fresh-matrix")
        self.output = Path(self.scope["outputRoot"])
        self.paths = {}
        for name, source in (("preparation", PREPARATION_SOURCE), ("transport", TRANSPORT_SOURCE), ("matrix", MATRIX_SOURCE)):
            path = Path(self.scope["sourceRoot"]) / source.name
            self.write(path, source.read_bytes())
            self.paths[name] = path
        self.paths["proxy"] = Path(self.scope["proxySourceRoot"]) / "synthetic-owned-proxy.py"
        self.write(self.paths["proxy"], b"# Synthetic proxy source; never executed.\n")
        self.module = load_source(self.paths["preparation"], "preparation_guard_subject")
        self.support = load_source(self.paths["transport"], "preparation_guard_transport")
        self.planner = load_source(self.paths["matrix"], "preparation_guard_matrix")
        for module in (self.module, self.support, self.planner):
            testcase.addCleanup(sys.modules.pop, module.__name__, None)
        self.server_id = "synthetic-reference"
        self.actor_ids = {"admin": "old-user-0", "P": "synthetic-user-P", "Q": "synthetic-user-Q"}
        self.old_user_ids = ["old-user-" + str(index) for index in range(8)]
        self.old_library_ids = ["old-library-" + str(index) for index in range(10)]
        self.library_ids = {library: "synthetic-library-" + library for library in ("LA", "LB")}
        self.policy_guids = {"LA": "a123456789abcdef0123456789abcdef", "LB": "b123456789abcdef0123456789abcdef"}
        self.view_ids = {library: "synthetic-view-" + library for library in ("LA", "LB")}
        self.root_ids = {library: "synthetic-root-" + library for library in ("LA", "LB")}
        self.library_names = {library: "Synthetic Library " + library for library in ("LA", "LB")}
        self.series_names = {library: "Synthetic Series " + library for library in ("LA", "LB")}
        self.item_ids = {item: "synthetic-item-" + item for item in SUMMARIES + EPISODES}
        self.credentials = {actor: {"username": "synthetic-" + actor, "password": "private-password-" + actor + "-" + actor[0] * 40,
                                   "credentialRef": "synthetic-credential-" + actor} for actor in ("admin", "P", "Q")}
        self.credentials["admin"]["userId"] = self.actor_ids["admin"]
        self.media_bytes = b"Synthetic owned immutable media bytes; no decoder or generator is used.\n"
        self.media = {}
        for library in ("LA", "LB"):
            root = self.output / "media" / library
            files = {}
            for item in EPISODES:
                if "L" + item[0] == library:
                    season, episode = ((1, 1), (1, 2), (2, 1))[int(item[1]) - 1]
                    stem = "%s S%02dE%02d" % (self.series_names[library], season, episode)
                    files[item] = {"path": str(root / self.series_names[library] / ("Season %02d" % season) / (stem + ".mp4")), "sha256": digest(self.media_bytes),
                                   "sizeBytes": len(self.media_bytes)}
            self.media[library] = {"rootPath": str(root), "files": files}
        executable = self.root / "synthetic-executable"
        self.write(executable, b"Synthetic executable bytes; never executed.\n")
        info = executable.stat()
        application = {"pid": 54321, "startTicks": "123456", "bootId": "synthetic-boot", "uid": 0,
            "exe": str(executable), "exeDevice": info.st_dev, "exeInode": info.st_ino,
            "cmdline": [str(executable), "--synthetic-only"], "networkNamespace": "net:[54321]",
            "cgroup": "0::/synthetic-application\n"}
        endpoint = deepcopy(application)
        endpoint.update(pid=54322, startTicks="123457", networkNamespace="net:[54322]",
            cgroup="0::/synthetic-proxy\n", listener={"port": 18197, "socketInode": "543210"},
            cmdline=[str(executable), str(self.paths["proxy"]), "--reference-pid", "54321",
                     "--reference-start-ticks", "123456"])
        self.process = {"application": application, "endpoint": endpoint, "workerNetworkNamespace": "net:[54322]"}
        lock_path = Path(self.scope["fixtureRoot"]) / "coordination.lock"
        self.write(lock_path, b"Synthetic exclusive coordination lock.\n")
        lock_info = lock_path.stat()
        self.lock = {"path": str(lock_path), "device": lock_info.st_dev, "inode": lock_info.st_ino}
        sealed = self.root / "synthetic-sealed-history"
        sealed.mkdir(mode=0o700)
        self.baseline = self.make_baseline()
        self.release = {"closedAuthentication": [], "releasedAt": STAMP}
        input_root = Path(self.scope["fixtureRoot"]) / "synthetic-inputs"
        input_root.mkdir(mode=0o700)
        self.scope["inputRoot"] = str(input_root)
        inputs = {}
        for name, value in (("release", self.release), ("publicBaseline", self.baseline),
                            ("credentials", {"schemaVersion": 1, "runId": "synthetic-preparation",
                                             "accounts": self.credentials}), ("mediaApproval", {"syntheticOnly": True})):
            path = input_root / (name + ".json")
            self.write(path, encoded(value))
            inputs[name] = self.descriptor(path)
        source = input_root / "owned-synthetic.mp4"
        self.write(source, self.media_bytes)
        source_info = source.stat()
        media_source = {"path": str(source), "sha256": digest(self.media_bytes), "sizeBytes": len(self.media_bytes),
            "device": source_info.st_dev, "inode": source_info.st_ino, "uid": 0, "gid": 0, "mode": 0o600,
            "nlink": source_info.st_nlink, "mtimeNs": source_info.st_mtime_ns, "ctimeNs": source_info.st_ctime_ns}
        approved = input_root / "approved-owned-media.json"
        self.write(approved, encoded({"marker": "goby-client-media-m3e-v1",
            "movieProfile": {"durationSeconds": 600, "fps": 30, "video": "h264", "audio": "aac"},
            "files": {source.name: media_source["sha256"]}}))
        approved_descriptor = self.descriptor(approved)
        actors = {}
        for actor in ("admin", "P", "Q"):
            actors[actor] = {key: value for key, value in self.credentials[actor].items() if key != "password"}
            actors[actor]["deviceId"] = "synthetic-preparation-device-" + actor
            if actor in ACTORS:
                actors[actor]["matrixDeviceId"] = "synthetic-matrix-device-" + actor
        budgets = {"requestSeconds": 5, "normalSeconds": 1200, "cleanupSeconds": 600,
                   "requestBytes": 32768, "responseBytes": 65536,
                   "totalResponseBytes": 24 * 1024 * 1024, "cleanupResponseBytes": 7 * 1024 * 1024}
        self.manifest = {"schemaVersion": 1, "runId": "synthetic-preparation", "target": "reference", "ownerUid": 0,
            "server": {"id": self.server_id, "version": "synthetic-version"},
            "endpoint": {"scheme": "http", "host": "127.0.0.1", "port": 18197}, "process": self.process,
            "lock": self.lock, "sources": {key: self.descriptor(path) for key, path in self.paths.items()},
            "inputs": inputs, "scope": self.scope, "sealedRoots": [str(sealed)], "forbiddenOriginalRoots": [str(executable)],
            "media": {"source": media_source, "ownedRoot": str(input_root), "approvedReceipt": approved_descriptor,
                      "approvedReceiptSha256": approved_descriptor["sha256"],
                      "roots": {library: self.media[library]["rootPath"] for library in ("LA", "LB")}},
            "actors": actors, "libraries": {library: {"name": self.library_names[library],
                                                     "seriesName": self.series_names[library]} for library in ("LA", "LB")},
            "preservation": {"userIds": self.old_user_ids, "libraryIds": self.old_library_ids,
                "detailRoutes": [{"group": group, "userId": "old-user-0" if group == "admin" else "old-user-6" if group == "grant-P" else "old-user-1", "itemId": item}
                                 for group, rows in self.baseline["details"].items() for item in rows]},
            "budgets": budgets, "matrixBudgets": deepcopy(budgets), "lifecycleSeparationSeconds": 2}

    @staticmethod
    def write(path, raw):
        with path.open("xb") as stream:
            stream.write(raw)
        path.chmod(0o600)

    @staticmethod
    def descriptor(path):
        return {"path": str(path), "sha256": digest(path.read_bytes())}

    def profile(self, actor):
        return {"Id": self.actor_ids[actor], "Name": self.credentials[actor]["username"],
            "Policy": {"IsAdministrator": actor == "admin", "EnableAllFolders": actor == "admin",
                       "EnabledFolders": [] if actor == "admin" else list(self.policy_guids.values()), "IsDisabled": False},
            "Configuration": {"Order": ["tv"]}}

    def make_baseline(self):
        roster = {user: {"Id": user, "Name": self.credentials["admin"]["username"] if index == 0 else "Old User " + str(index),
            "Policy": {"IsAdministrator": index == 0, "EnableAllFolders": True, "EnabledFolders": [], "IsDisabled": False},
            "Configuration": {"Order": ["movies"]}} for index, user in enumerate(self.old_user_ids)}
        libraries = {library: {"ItemId": library, "Name": "Old Library " + str(index), "CollectionType": "movies",
            "Locations": [str(self.root / ("old-media-" + str(index)))],
            "LibraryOptions": {"SaveLocalMetadata": False, "MetadataSavers": []}}
                     for index, library in enumerate(self.old_library_ids)}
        items = {"old-item-" + str(index): {"Id": "old-item-" + str(index), "Name": "Old Movie " + str(index),
            "Type": "Movie", "ParentId": library, "IsFolder": False, "UserData": zero_state("old-" + str(index)),
            "Path": str(self.root / ("old-media-" + str(index)) / "old-movie.mp4")}
                 for index, library in enumerate(self.old_library_ids)}
        return {"marker": "synthetic-public-baseline", "version": 1, "captured_at": STAMP,
            "server": {"Id": self.server_id, "Version": "synthetic-version"}, "roster": roster,
            "configuration": {"SyntheticConfiguration": {"preserve": True}}, "libraries": libraries,
            "catalog_by_library": {library: {"old-item-" + str(index): deepcopy(items["old-item-" + str(index)])}
                                   for index, library in enumerate(self.old_library_ids)},
            "items_by_user": {user: deepcopy(items) for user in self.old_user_ids},
            "preferences": {user: {"SyntheticPreference": index} for index, user in enumerate(self.old_user_ids)},
            "details": {"admin": {key: deepcopy(items[key]) for key in list(items)[:4]},
                        "viewer": {key: deepcopy(items[key]) for key in list(items)[:2]},
                        "grant-P": {key: deepcopy(items[key]) for key in list(items)[:6]}},
            "devices": {"old-device-0": {"Id": "old-device-0", "ReportedDeviceId": "old-reported-device-0",
                "LastUserId": "old-user-1", "LastUserName": roster["old-user-1"]["Name"],
                "AppName": "Old Synthetic Client", "AppVersion": "0.1", "Name": "Old Synthetic Device",
                "DateLastActivity": STAMP}},
            "credential_context": {"channel": "controller_api", "authenticated_user_id": self.actor_ids["admin"],
                "token_sha256": "b" * 64, "user_id_semantics": "subject_projection"}}

    def views(self, actor):
        return [{"Id": self.view_ids[library], "Name": self.library_names[library], "Type": "CollectionFolder",
                 "CollectionType": "tvshows", "ParentId": self.root_ids[library], "Path": self.media[library]["rootPath"]}
                for library in ("LA", "LB")]

    def folder_detail(self, library, item_id):
        return {"Id": item_id, "Name": self.library_names[library],
                "Type": "CollectionFolder" if item_id == self.view_ids[library] else "Folder",
                "Path": self.media[library]["rootPath"], "ParentId": self.root_ids[library],
                "CollectionType": "tvshows", "IsFolder": True}

    def detail(self, item, actor, states):
        library = "L" + item[0]
        if item in EPISODES:
            index = int(item[1])
            season, episode = ((1, 1), (1, 2), (2, 1))[index - 1]
            return {"Id": self.item_ids[item], "Name": "Episode " + item, "Type": "Episode", "IsFolder": False,
                "ParentId": self.item_ids[item[0] + "S" + str(season)], "SeriesId": self.item_ids[item[0]],
                "ParentIndexNumber": season, "IndexNumber": episode, "RunTimeTicks": RUNTIME_TICKS,
                "Path": self.media[library]["files"][item]["path"], "MediaSources": [{
                    "Id": "synthetic-source-" + item, "Path": self.media[library]["files"][item]["path"],
                    "RunTimeTicks": RUNTIME_TICKS}],
                "MediaStreams": [{"Type": "Video", "AverageFrameRate": 30, "RealFrameRate": 30}],
                "UserData": deepcopy(states.get(actor, {}).get(item, zero_state(item)))}
        value = {"Id": self.item_ids[item], "Type": "Series" if len(item) == 1 else "Season", "IsFolder": True,
                 "Name": self.series_names[library] if len(item) == 1 else "Season " + item[-1],
                 "ParentId": self.root_ids[library] if len(item) == 1 else self.item_ids[item[0]],
                 "Path": str(Path(self.media[library]["rootPath"]) / self.series_names[library]) if len(item) == 1 else
                         str(Path(self.media[library]["rootPath"]) / self.series_names[library] / ("Season %02d" % int(item[-1]))),
                 "UserData": {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0,
                              "UnplayedItemCount": 3, "IsFavorite": False}}
        if len(item) != 1:
            value.update(SeriesId=self.item_ids[item[0]], IndexNumber=int(item[-1]))
        return value


class FakeWire:
    """Route exact protocol requests through independent synthetic server state."""

    def __init__(self, fixture, *, response_hook=None, send_hook=None):
        self.fixture = fixture
        self.support = fixture.support
        self.response_hook = response_hook
        self.send_hook = send_hook
        self.calls = []
        self.tokens = {actor: "synthetic-private-token-" + actor for actor in ("admin", "P", "Q")}
        self.sessions = {actor: "synthetic-session-" + actor for actor in ("admin", "P", "Q")}
        self.users = deepcopy(fixture.baseline["roster"])
        self.libraries = deepcopy(fixture.baseline["libraries"])
        self.devices = deepcopy(fixture.baseline["devices"])
        self.states = {actor: {item: zero_state(item) for item in EPISODES} for actor in ACTORS}
        self.revoked = set()
        self.started = {}
        self.playback = {}
        self.login_devices = {}
        self.stop_count = 0

    def wire(self, status, body=None, *, raw=None, complete=True, failure=None, headers=None):
        raw = (b"" if body is None else encoded(body)) if raw is None else raw
        return self.support.WireResponse(status,
            [("Content-Type", "application/json")] if headers is None else headers,
            raw, complete, self.fixture.clock.utc_now(), failure)

    def actor(self, headers):
        token = headers.get("X-Emby-Token")
        matches = [actor for actor in self.tokens if self.tokens[actor] == token]
        if len(matches) != 1:
            raise AssertionError("A request used an unacknowledged or substituted token.")
        return matches[0]

    def send(self, request, headers, payload, *, timeout_seconds, max_bytes):
        if not (0 < timeout_seconds <= self.fixture.manifest["budgets"]["requestSeconds"]):
            raise AssertionError("The producer attempted HTTP without a bounded timeout.")
        if max_bytes != self.fixture.manifest["budgets"]["responseBytes"] + 1:
            raise AssertionError("The producer supplied an incorrect response capture limit.")
        call = {"request": deepcopy(request), "headers": deepcopy(headers), "payload": payload,
                "timeoutSeconds": timeout_seconds, "maxBytes": max_bytes}
        self.calls.append(call)
        if self.send_hook is not None:
            self.send_hook(self, call)
        response = self.respond(request, headers, payload)
        if self.response_hook is not None:
            replacement = self.response_hook(self, call, response)
            if replacement is not None:
                response = replacement
        return response

    def respond(self, request, headers, payload):
        fixture = self.fixture
        method = request.method
        parsed = urlsplit(request.route)
        route, query = parsed.path, parse_qs(parsed.query)
        if parsed.scheme or parsed.netloc or not route.startswith("/emby/"):
            raise AssertionError("A request escaped the exact synthetic endpoint.")
        if method == "POST" and route == "/emby/Users/AuthenticateByName":
            if headers.get("X-Emby-Token") is not None:
                raise AssertionError("Login unexpectedly reused a token.")
            body = parse_qs(payload.decode())
            actor = next((actor for actor, credential in fixture.credentials.items()
                          if body == {"Username": [credential["username"]], "Pw": [credential["password"]]}), None)
            if actor is None:
                raise AssertionError("Login used credentials outside the case-owned binding.")
            user_id = fixture.actor_ids[actor]
            if user_id not in self.users:
                raise AssertionError("An ordinary user logged in before creation was acknowledged.")
            authorization = headers["Authorization"]
            device = authorization.split('DeviceId="', 1)[1].split('"', 1)[0]
            self.login_devices[actor] = device
            registry_id = "synthetic-registered-" + actor
            if registry_id in self.devices or any(row["ReportedDeviceId"] == device for row in self.devices.values()):
                raise AssertionError("A synthetic login reused a pre-existing registered device identity.")
            self.devices[registry_id] = {"Id": registry_id, "ReportedDeviceId": device,
                "AppName": "Goby NextUp Preparation", "AppVersion": "1.0", "Name": "Linux Fixture Recorder",
                "LastUserId": user_id, "LastUserName": fixture.credentials[actor]["username"],
                "DateLastActivity": fixture.clock.utc_now()}
            return self.wire(200, {"AccessToken": self.tokens[actor], "ServerId": fixture.server_id,
                "User": deepcopy(self.users[user_id]), "SessionInfo": {"Id": self.sessions[actor],
                    "UserId": user_id, "DeviceId": device}})
        actor = self.actor(headers)
        if route == "/emby/Sessions/Logout" and method == "POST":
            if query or payload is not None:
                raise AssertionError("Logout unexpectedly carried a query or entity body.")
            self.revoked.add(actor)
            return self.wire(204)
        if route == "/emby/Sessions" and method == "GET":
            if actor not in self.revoked:
                raise AssertionError("Rejection proof ran before the owned token was retired.")
            return self.wire(401, {"error": "synthetic retired token"})
        if actor in self.revoked:
            return self.wire(401, {"error": "synthetic retired token"})
        body = None if payload is None else json.loads(payload)
        if route == "/emby/System/Info/Public" and method == "GET":
            return self.wire(200, deepcopy(fixture.baseline["server"]))
        if route == "/emby/System/Configuration" and method == "GET":
            return self.wire(200, deepcopy(fixture.baseline["configuration"]))
        if route == "/emby/Users" and method == "GET":
            return self.wire(200, list(deepcopy(self.users).values()))
        if route == "/emby/Devices" and method == "GET":
            return self.wire(200, {"Items": list(deepcopy(self.devices).values()), "TotalRecordCount": 0})
        if route == "/emby/Library/VirtualFolders/Query" and method == "GET":
            return self.wire(200, {"Items": list(deepcopy(self.libraries).values()),
                                   "TotalRecordCount": len(self.libraries)})
        if route == "/emby/Library/SelectableMediaFolders" and method == "GET":
            if actor != "admin":
                raise AssertionError("Preparation mapping requires the existing administrator token.")
            return self.wire(200, [{"Id": fixture.library_ids[library], "Name": fixture.library_names[library],
                "Guid": fixture.policy_guids[library], "IsUserAccessConfigurable": True,
                "SubFolders": [{"Id": fixture.root_ids[library], "Path": fixture.media[library]["rootPath"],
                    "Name": library, "IsUserAccessConfigurable": True}]} for library in ("LA", "LB")])
        if route == "/emby/Library/VirtualFolders" and method == "POST":
            library = next((name for name in ("LA", "LB")
                            if fixture.library_names[name] == body.get("Name")), None)
            if library is None or actor != "admin":
                raise AssertionError("Creation requested a library outside the frozen owned scope.")
            library_id = fixture.library_ids[library]
            if library_id in self.libraries:
                raise AssertionError("A library creation mutation was replayed.")
            self.libraries[library_id] = {"ItemId": library_id, "Name": body["Name"],
                "CollectionType": body["CollectionType"], "Locations": deepcopy(body["Paths"]),
                "LibraryOptions": deepcopy(body["LibraryOptions"])}
            return self.wire(204)
        if route.endswith("/Refresh") and route.startswith("/emby/Items/") and method == "POST":
            if route.split("/")[3] not in fixture.library_ids.values() or actor != "admin":
                raise AssertionError("Refresh escaped the two acknowledged owned libraries.")
            return self.wire(204)
        if route == "/emby/Users/New" and method == "POST":
            ordinary = next((name for name in ACTORS if fixture.credentials[name]["username"] == body.get("Name")), None)
            if ordinary is None or actor != "admin":
                raise AssertionError("User creation escaped the two frozen ordinary accounts.")
            user_id = fixture.actor_ids[ordinary]
            if user_id in self.users:
                raise AssertionError("User creation was replayed.")
            self.users[user_id] = fixture.profile(ordinary)
            self.users[user_id]["Policy"].update(EnableAllFolders=True, EnabledFolders=[])
            return self.wire(200, deepcopy(self.users[user_id]))
        if route.lower().startswith("/emby/usersettings/") and method == "GET":
            user_id = route.rsplit("/", 1)[1]
            if user_id in fixture.baseline["preferences"]:
                return self.wire(200, deepcopy(fixture.baseline["preferences"][user_id]))
            if user_id not in self.users:
                raise AssertionError("Preferences named an unknown user.")
            return self.wire(200, {"home": "tv", "syntheticPreference": {"preserve": False}})
        if route.startswith("/emby/Users/"):
            parts = route.split("/")
            user_id = parts[3]
            subject = next((name for name, known in fixture.actor_ids.items() if known == user_id), None)
            if len(parts) == 4 and method == "GET":
                return self.wire(200, deepcopy(self.users[user_id]))
            if len(parts) == 5 and parts[4] == "Password" and method == "POST":
                if actor != "admin" or subject not in ACTORS:
                    raise AssertionError("Password mutation escaped acknowledged ordinary ownership.")
                return self.wire(204)
            if len(parts) == 5 and parts[4] == "Policy" and method == "POST":
                if actor != "admin" or subject not in ACTORS:
                    raise AssertionError("Policy mutation escaped acknowledged ordinary ownership.")
                self.users[user_id]["Policy"] = deepcopy(body)
                return self.wire(204)
            if len(parts) == 5 and parts[4] == "Views" and method == "GET":
                rows = fixture.views(actor) if actor == "admin" or set(self.users[user_id]["Policy"].get("EnabledFolders", [])) == set(fixture.policy_guids.values()) else []
                return self.wire(200, {"Items": rows, "TotalRecordCount": len(rows)})
            if len(parts) == 5 and parts[4] == "Items" and method == "GET":
                if "ParentId" in query:
                    parent = query["ParentId"][0]
                    library = next((name for name in ("LA", "LB")
                        if parent in (fixture.library_ids[name], fixture.view_ids[name], fixture.root_ids[name])), None)
                    if library is None:
                        rows = list(deepcopy(fixture.baseline["catalog_by_library"][parent]).values())
                    else:
                        rows = [fixture.detail(item, subject or actor, self.states)
                                for item in SUMMARIES + EPISODES if "L" + item[0] == library]
                elif "Ids" in query:
                    requested = set(query["Ids"][0].split(","))
                    rows = [deepcopy(row) for row in fixture.baseline["items_by_user"].get(user_id, {}).values()
                            if row["Id"] in requested]
                    if subject in ACTORS:
                        rows.extend(fixture.detail(item, subject, self.states)
                                    for item in SUMMARIES + EPISODES if fixture.item_ids[item] in requested)
                else:
                    raise AssertionError("The producer attempted an unbounded catalog query.")
                return self.wire(200, {"Items": rows, "TotalRecordCount": len(rows)})
            if len(parts) == 6 and parts[4] == "Items" and method == "GET":
                item_id = parts[5]
                symbol = next((name for name, known in fixture.item_ids.items() if known == item_id), None)
                if symbol is not None:
                    return self.wire(200, fixture.detail(symbol, subject or actor, self.states))
                for library in ("LA", "LB"):
                    if item_id in (fixture.view_ids[library], fixture.root_ids[library]):
                        return self.wire(200, fixture.folder_detail(library, item_id))
                for group in fixture.baseline["details"].values():
                    if item_id in group:
                        return self.wire(200, deepcopy(group[item_id]))
            if len(parts) == 6 and parts[4] == "PlayedItems" and method == "DELETE":
                symbol = next((name for name, known in fixture.item_ids.items() if known == parts[5]), None)
                if (actor, subject, symbol) not in (("P", "P", "A1"), ("Q", "Q", "B1")) or query or payload is not None:
                    raise AssertionError("DELETE escaped the actor's calibrated episode or carried data.")
                self.states[actor][symbol] = zero_state(symbol)
                return self.wire(200, deepcopy(self.states[actor][symbol]))
        if route.startswith("/emby/Items/") and route.endswith("/PlaybackInfo") and method == "POST":
            symbol = next((name for name, known in fixture.item_ids.items() if known == route.split("/")[3]), None)
            if (actor, symbol) not in (("P", "A1"), ("Q", "B1")) or body != {"UserId": fixture.actor_ids[actor], "IsPlayback": True}:
                raise AssertionError("PlaybackInfo escaped an independently owned calibration pair.")
            play_session = "synthetic-play-" + str(len(self.calls))
            self.playback[actor] = {"ItemId": fixture.item_ids[symbol], "MediaSourceId": "synthetic-source-" + symbol,
                                    "PlaySessionId": play_session, "SessionId": self.sessions[actor]}
            return self.wire(200, {"PlaySessionId": play_session, "MediaSources": [{
                "Id": "synthetic-source-" + symbol, "RunTimeTicks": RUNTIME_TICKS,
                "Path": fixture.media["L" + symbol[0]]["files"][symbol]["path"]}]})
        if route in ("/emby/Sessions/Playing", "/emby/Sessions/Playing/Progress", "/emby/Sessions/Playing/Stopped") and method == "POST":
            context = self.playback.get(actor)
            if not context or any(body.get(key) != value for key, value in context.items()):
                raise AssertionError("Playback changed its acknowledged public identity tuple.")
            symbol = next(name for name, known in fixture.item_ids.items() if known == body["ItemId"])
            if route == "/emby/Sessions/Playing":
                self.started[actor] = deepcopy(context)
            if route.endswith("/Stopped"):
                self.stop_count += 1
                position = body["PositionTicks"]
                self.states[actor][symbol].update(Played=position == RUNTIME_TICKS, PlayCount=1,
                    PlaybackPositionTicks=0 if position == RUNTIME_TICKS else position,
                    LastPlayedDate=fixture.clock.utc_now())
                self.started.pop(actor, None)
            return self.wire(204)
        raise AssertionError("An unenumerated request reached the synthetic transport: " + method + " " + request.route)


class GuardCase(unittest.TestCase):
    """Install hard barriers against real HTTP, subprocesses, and process probes."""

    def setUp(self):
        self.fixture = PreparationFixture(self)
        self.P = self.fixture.module
        self.T = self.fixture.support
        self.clock = self.fixture.clock
        for target, attribute in ((self.P, "Authority"), (self.T.HTTPTransport, "send"),
                                  (self.T, "process_identity"), (socket, "create_connection"),
                                  (socket, "socket"), (self.P.subprocess, "run")):
            blocker = patch.object(target, attribute, side_effect=AssertionError("Real I/O and process inspection are forbidden in synthetic guards."))
            blocker.start()
            self.addCleanup(blocker.stop)

    def runner(self, *, response_hook=None, send_hook=None, journal_factory=None):
        authority = FakeAuthority(self.fixture)
        transport = FakeWire(self.fixture, response_hook=response_hook, send_hook=send_hook)
        factory = journal_factory or (lambda root, uid: self.T.Journal(root, uid=uid))
        runner = self.P.PreparationRunner(self.fixture.manifest, authority=authority, transport=transport,
            journal_factory=factory, monotonic=self.clock, sleeper=self.clock.sleep, utc_now=self.clock.utc_now)
        self.addCleanup(authority.close)
        self.addCleanup(lambda: runner.journal.close() if runner.journal is not None else None)
        return runner, transport, authority

    def assert_no_draft(self):
        if self.fixture.output.exists():
            self.assertEqual(list(self.fixture.output.rglob("draft-execution.json")), [])
            self.assertEqual(list(self.fixture.output.rglob("draft-matrix.json")), [])

    def assert_failed(self, result):
        self.assertFalse(result.get("completed", False))
        self.assertNotEqual(result.get("status"), "awaiting_independent_attestation")
        self.assert_no_draft()

    def assert_no_resume(self, runner, transport):
        count = len(transport.calls)
        with self.assertRaises((self.P.PreparationError, self.T.TransportError)):
            runner.run()
        self.assertEqual(len(transport.calls), count)


class PlanGuards(GuardCase):
    def test_plan_is_pure_bounded_and_deeply_copied(self):
        original = deepcopy(self.fixture.manifest)
        observed = self.P.validate_manifest(original)
        plan = self.P.frozen_plan(original)
        self.assertEqual(original, self.fixture.manifest)
        observed["actors"]["P"]["username"] = "Changed Only In Returned Copy"
        self.assertNotEqual(observed, original)
        self.assertFalse(self.fixture.output.exists())
        self.assertEqual(plan["maximumRequests"], 380)
        self.assertEqual(plan["normalLimit"], 280)
        self.assertEqual(plan["cleanupReserve"], 100)
        self.assertEqual(plan["normalMaximum"], 257)
        self.assertEqual(plan["successMaximumIncludingLogout"], 263)
        self.assertEqual(plan["cleanupMaximum"], 89)
        self.assertEqual(plan["calibrations"], [["P", "A1", "partial"], ["P", "A1", "complete"],
                                                ["Q", "B1", "partial"], ["Q", "B1", "complete"]])

    def reject(self, mutate):
        candidate = deepcopy(self.fixture.manifest)
        mutate(candidate)
        with self.assertRaises((self.P.PreparationError, ValueError, TypeError)):
            self.P.validate_manifest(candidate)
        self.assertFalse(self.fixture.output.exists())

    def test_unsafe_endpoint_target_and_protocol_are_rejected_before_output(self):
        for field, value in (("scheme", "https"), ("host", "localhost"), ("host", "192.0.2.1"),
                             ("port", 8096), ("port", True)):
            with self.subTest(field=field, value=value):
                self.reject(lambda manifest: manifest["endpoint"].update({field: value}))
        self.reject(lambda manifest: manifest.update(target="goby"))
        self.reject(lambda manifest: manifest.update(unreviewedRoute="/emby/Library/Refresh"))

    def test_source_and_output_paths_cannot_escape_or_overlap_sealed_scope(self):
        for path in ("relative/file.py", "/tmp/../outside.py", "C:\\synthetic\\file.py", "/tmp/unsafe\nfile.py", "/tmp/unsafe\x00file.py"):
            with self.subTest(path=path):
                self.reject(lambda manifest: manifest["sources"]["preparation"].update(path=path))
        self.reject(lambda manifest: manifest["scope"].update(outputRoot=manifest["scope"]["matrixEvidenceRoot"]))
        self.reject(lambda manifest: manifest["sealedRoots"].append(manifest["scope"]["outputRoot"]))
        self.reject(lambda manifest: manifest["media"]["roots"].update(LA=manifest["scope"]["fixtureRoot"]))
        self.reject(lambda manifest: manifest["sources"]["proxy"].update(path=manifest["sources"]["transport"]["path"]))

    def test_actor_identity_device_and_names_cannot_cross_or_inject_routes(self):
        for field in ("username", "credentialRef", "deviceId"):
            with self.subTest(field=field):
                self.reject(lambda manifest: manifest["actors"]["Q"].update({field: manifest["actors"]["P"][field]}))
        self.reject(lambda manifest: manifest["actors"]["P"].update(matrixDeviceId=manifest["actors"]["P"]["deviceId"]))
        for bad in ("../other", "name?admin=true", "name\r\nX-Token: secret", "<xml>"):
            with self.subTest(bad=bad):
                self.reject(lambda manifest: manifest["actors"]["P"].update(username=bad))
                self.reject(lambda manifest: manifest["libraries"]["LA"].update(seriesName=bad))

    def test_process_proxy_and_lock_bindings_cannot_be_substituted(self):
        self.reject(lambda manifest: manifest["process"]["endpoint"].update(pid=manifest["process"]["application"]["pid"]))
        self.reject(lambda manifest: manifest["process"]["endpoint"].update(networkNamespace=manifest["process"]["application"]["networkNamespace"]))
        self.reject(lambda manifest: manifest["process"].update(workerNetworkNamespace="net:[unknown]"))
        self.reject(lambda manifest: manifest["process"]["endpoint"]["listener"].update(port=8096))
        self.reject(lambda manifest: manifest["process"]["endpoint"]["cmdline"].extend(["--reference-pid", "another"]))
        self.reject(lambda manifest: manifest["lock"].update(path="/unowned/coordination.lock"))
        self.reject(lambda manifest: manifest["lock"].update(inode=True))
        self.reject(lambda manifest: manifest["process"]["endpoint"]["listener"].update(socketInode="0"))

    def test_contract_versions_and_lifecycle_delay_require_exact_integer_types(self):
        for value in (True, 1.0, "1"):
            with self.subTest(schemaVersion=value):
                self.reject(lambda manifest: manifest.update(schemaVersion=value))
        for value in (2.0, "2", True):
            with self.subTest(lifecycleSeparationSeconds=value):
                self.reject(lambda manifest: manifest.update(lifecycleSeparationSeconds=value))

    def test_population_and_detail_routes_are_frozen(self):
        self.reject(lambda manifest: manifest["preservation"]["userIds"].append("old-user-extra"))
        self.reject(lambda manifest: manifest["preservation"]["libraryIds"].pop())
        self.reject(lambda manifest: manifest["preservation"]["detailRoutes"].pop())
        self.reject(lambda manifest: manifest["preservation"]["detailRoutes"].__setitem__(1, deepcopy(manifest["preservation"]["detailRoutes"][0])))

    def test_budget_types_bounds_and_independent_matrix_reserve_are_required(self):
        for family in ("budgets", "matrixBudgets"):
            for key in self.fixture.manifest[family]:
                for value in (True, 0, -1, float("inf")):
                    with self.subTest(family=family, key=key, value=value):
                        self.reject(lambda manifest: manifest[family].update({key: value}))
        self.reject(lambda manifest: manifest["budgets"].update(responseBytes=1024 * 1024 + 1))
        self.reject(lambda manifest: manifest["budgets"].update(cleanupResponseBytes=100 * manifest["budgets"]["responseBytes"]))
        self.reject(lambda manifest: manifest["matrixBudgets"].update(totalResponseBytes=384 * 1024 * 1024))
        self.reject(lambda manifest: manifest["matrixBudgets"].update(cleanupResponseBytes=81 * 1024 * 1024))

    def test_legacy_transport_and_unapproved_media_inputs_are_rejected(self):
        self.reject(lambda manifest: manifest["sources"]["transport"].update(sha256="0" * 64))
        self.reject(lambda manifest: manifest["media"].update(approvedReceiptSha256="not-a-digest"))
        self.reject(lambda manifest: manifest["media"]["source"].update(uid=1000))
        self.reject(lambda manifest: manifest["media"]["source"].update(sizeBytes=128 * 1024 * 1024 + 1))
        self.reject(lambda manifest: manifest["media"]["source"].update(nlink=0))

    def test_json_duplicate_keys_nonfinite_values_and_invalid_utf8_are_rejected(self):
        for raw in (b'{"same":1,"same":2}', b'{"value":NaN}', b'{"value":Infinity}', b'{"value":"\xff"}'):
            with self.subTest(raw=raw):
                with self.assertRaises((self.P.PreparationError, ValueError, UnicodeError)):
                    self.P.strict_json(raw)


class PipelineGuards(GuardCase):
    def test_complete_pipeline_retains_four_calibrations_and_waits_for_attestation(self):
        runner, wire, authority = self.runner()
        result = runner.run()
        self.assertTrue(result["completed"], result)
        self.assertTrue(result["cleanupComplete"], result)
        self.assertEqual(result["status"], "awaiting_independent_attestation")
        self.assertIsNone(result["ownershipPending"])
        self.assertFalse(result["uncertain"])
        self.assertLessEqual(result["requestCount"], 263)
        self.assertLessEqual(result["normalRequestCount"], 257)
        self.assertEqual(result["cleanupRequestCount"], 6)
        self.assertEqual(len(wire.calls), result["requestCount"])
        self.assertEqual(wire.revoked, {"admin", "P", "Q"})
        self.assertEqual(wire.stop_count, 4)
        self.assertEqual(wire.states, {actor: {item: zero_state(item) for item in EPISODES} for actor in ACTORS})
        self.assertTrue(authority.staged)
        self.assertGreaterEqual(authority.checks, 2 * len(wire.calls))
        self.assertFalse(Path(self.fixture.scope["matrixEvidenceRoot"]).exists())
        state = json.loads((self.fixture.output / "private" / "state.json").read_text())
        self.assertIsNone(state["ownershipPending"])
        self.assertFalse(state["uncertain"])
        self.assertEqual(len(state["playSessionIds"]), 4)
        cleanup_paths = [path for path in self.fixture.output.rglob("*.json")
                         if json.loads(path.read_text()).get("kind") == "nextup-global-cleanup"]
        self.assertEqual(len(cleanup_paths), 1)
        cleanup = json.loads(cleanup_paths[0].read_text())
        facts = cleanup["facts"]
        self.assertEqual(facts["contractVersion"], 2)
        self.assertEqual([(row["actor"], row["item"], row["mode"]) for row in facts["calibrations"]],
                         [("P", "A1", "partial"), ("P", "A1", "complete"), ("Q", "B1", "partial"), ("Q", "B1", "complete")])
        self.assertEqual(cleanup["attestationState"], "pending")
        for row in facts["calibrations"]:
            for event_name in ("beforeZero", "beforeDelete", "delete", "afterDelete"):
                event = row[event_name]
                self.assertIn(["X-Emby-Token", wire.tokens[row["actor"]]], event["request"]["headers"])
                self.assertEqual(event["response"]["status"], 200)
                self.assertIsNone(event["request"]["body"])
        self.assert_no_resume(runner, wire)

    def test_draft_is_rejected_then_synthetic_independent_attestation_satisfies_actual_consumer(self):
        runner, wire, unused_authority = self.runner()
        result = runner.run()
        self.assertTrue(result["completed"], result)
        paths = list(self.fixture.output.rglob("draft-execution.json"))
        self.assertEqual(len(paths), 1)
        execution = json.loads(paths[0].read_text())
        self.assertIs(execution["matrix"]["binding"]["fixtureReleased"], False)
        consumer = self.T.Authority(execution, probe=lambda expected: deepcopy(expected))
        try:
            with self.assertRaises(ValueError):
                consumer.acquire()
        finally:
            consumer.close()
        independently_attested = deepcopy(execution)
        independently_attested["matrix"]["binding"]["fixtureReleased"] = True
        accepted = self.T.Authority(independently_attested, probe=lambda expected: deepcopy(expected))
        try:
            accepted.acquire()
            accepted.check()
        finally:
            if accepted.planner is not None:
                sys.modules.pop(accepted.planner.__name__, None)
            accepted.close()
        self.assertEqual(len(wire.calls), result["requestCount"])

    def test_http_requires_durable_intent_reservation_and_pending_state(self):
        def require_write_ahead(wire, call):
            request = call["request"]
            prefix = "%04d-%s" % (len(wire.calls), request.label)
            root = self.fixture.output / "private"
            for suffix in ("-intent.json", "-reserved.json"):
                self.assertTrue((root / (prefix + suffix)).is_file(), prefix + suffix)
            state = json.loads((root / "state.json").read_text())
            self.assertIsNotNone(state["pending"])
        runner, wire, unused_authority = self.runner(send_hook=require_write_ahead)
        result = runner.run()
        self.assertTrue(result["completed"], result)
        self.assertEqual(len({call["request"].label for call in wire.calls}), len(wire.calls))

    def test_calibration_mutations_keep_exact_actor_session_source_and_position(self):
        runner, wire, unused_authority = self.runner()
        result = runner.run()
        self.assertTrue(result["completed"], result)
        started = [call for call in wire.calls if call["request"].route == "/emby/Sessions/Playing"]
        progress = [call for call in wire.calls if call["request"].route == "/emby/Sessions/Playing/Progress"]
        stopped = [call for call in wire.calls if call["request"].route == "/emby/Sessions/Playing/Stopped"]
        self.assertEqual(len(started), 4)
        self.assertEqual(len(progress), 4)
        self.assertEqual(len(stopped), 4)
        for index, position in enumerate((PARTIAL_TICKS, RUNTIME_TICKS, PARTIAL_TICKS, RUNTIME_TICKS)):
            bodies = [json.loads(rows[index]["payload"]) for rows in (started, progress, stopped)]
            self.assertEqual([body["PositionTicks"] for body in bodies], [0, position, position])
            self.assertEqual(bodies[1]["EventName"], "TimeUpdate")
            self.assertIs(bodies[2]["Failed"], False)
            self.assertIs(bodies[2]["IsAutomated"], False)
            for field in ("ItemId", "MediaSourceId", "PlaySessionId", "SessionId"):
                self.assertEqual(len({body[field] for body in bodies}), 1)
        self.assertTrue(self.clock.waits)

    def test_restricted_accounts_observe_every_full_detail_with_their_own_token(self):
        runner, wire, unused_authority = self.runner()
        result = runner.run()
        self.assertTrue(result["completed"], result)
        for actor in ACTORS:
            for symbol in SUMMARIES + EPISODES:
                route = "/emby/Users/" + self.fixture.actor_ids[actor] + "/Items/" + self.fixture.item_ids[symbol]
                calls = [call for call in wire.calls if call["request"].route == route and call["request"].method == "GET"]
                self.assertGreaterEqual(len(calls), 2)
                self.assertTrue(all(call["headers"]["X-Emby-Token"] == wire.tokens[actor] for call in calls))
            profile = wire.users[self.fixture.actor_ids[actor]]
            self.assertIs(profile["Policy"]["EnableAllFolders"], False)
            self.assertEqual(set(profile["Policy"]["EnabledFolders"]), set(self.fixture.policy_guids.values()))

    def test_logout_and_rejection_preserve_each_exact_original_token(self):
        runner, wire, unused_authority = self.runner()
        result = runner.run()
        self.assertTrue(result["completed"], result)
        logout = [call for call in wire.calls if call["request"].route == "/emby/Sessions/Logout"]
        rejection = [call for call in wire.calls if call["request"].route == "/emby/Sessions"]
        self.assertEqual(len(logout), 3)
        self.assertEqual(len(rejection), 3)
        self.assertEqual({call["headers"]["X-Emby-Token"] for call in logout}, set(wire.tokens.values()))
        self.assertEqual({call["headers"]["X-Emby-Token"] for call in rejection}, set(wire.tokens.values()))
        self.assertTrue(all(call["payload"] is None for call in logout + rejection))

    def test_private_responses_keep_secrets_while_export_removes_them(self):
        runner, wire, unused_authority = self.runner()
        result = runner.run()
        self.assertTrue(result["completed"], result)
        private = "\n".join(path.read_text() for path in (self.fixture.output / "private").rglob("*.json"))
        exported = "\n".join(path.read_text() for path in (self.fixture.output / "export").rglob("*.json"))
        self.assertTrue(exported)
        for secret in (*wire.tokens.values(), *wire.sessions.values(),
                       *(row["password"] for row in self.fixture.credentials.values()),
                       *(row["deviceId"] for row in self.fixture.manifest["actors"].values()),
                       *(row["path"] for library in self.fixture.media.values() for row in library["files"].values())):
            self.assertNotIn(secret, exported)
        self.assertTrue(any(token in private for token in wire.tokens.values()))


class FailureGuards(GuardCase):
    def lost_route(self, method, path):
        interrupted = []
        def lose(wire, call, response):
            request = call["request"]
            if not interrupted and request.method == method and urlsplit(request.route).path == path:
                interrupted.append(request.label)
                return wire.wire(response.status, raw=response.raw[:max(1, len(response.raw) // 2)],
                                 complete=False, failure="synthetic lost response")
            return None
        runner, wire, unused_authority = self.runner(response_hook=lose)
        result = runner.run()
        self.assertEqual(len(interrupted), 1, result)
        self.assert_failed(result)
        self.assertFalse(result.get("cleanupComplete", False))
        self.assertEqual(sum(call["request"].method == method and urlsplit(call["request"].route).path == path
                             for call in wire.calls), 1)
        interrupted_index = next(index for index, call in enumerate(wire.calls) if call["request"].label == interrupted[0])
        self.assertTrue(all(call["request"].cleanup for call in wire.calls[interrupted_index + 1:]))
        self.assert_no_resume(runner, wire)

    def test_lost_administrator_login_response_blocks_ownership_and_replay(self):
        self.lost_route("POST", "/emby/Users/AuthenticateByName")

    def test_lost_library_creation_response_never_adopts_or_replays_library(self):
        self.lost_route("POST", "/emby/Library/VirtualFolders")

    def test_lost_account_creation_response_never_adopts_or_replays_account(self):
        self.lost_route("POST", "/emby/Users/New")

    def test_lost_playback_info_response_never_infers_session_ownership(self):
        self.lost_route("POST", "/emby/Items/" + self.fixture.item_ids["A1"] + "/PlaybackInfo")

    def test_lost_playing_response_never_replays_start(self):
        self.lost_route("POST", "/emby/Sessions/Playing")

    def test_lost_progress_response_never_replays_progress(self):
        self.lost_route("POST", "/emby/Sessions/Playing/Progress")

    def test_lost_stopped_response_never_replays_stop(self):
        self.lost_route("POST", "/emby/Sessions/Playing/Stopped")

    def test_lost_delete_response_never_replays_mutation(self):
        self.lost_route("DELETE", "/emby/Users/" + self.fixture.actor_ids["P"] + "/PlayedItems/" + self.fixture.item_ids["A1"])

    def test_malformed_login_retains_unverified_token_without_using_it(self):
        def corrupt(wire, call, response):
            if call["request"].route == "/emby/Users/AuthenticateByName":
                body = json.loads(response.raw)
                body["User"]["Id"] = "unrelated-user"
                return wire.wire(200, body)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(wire.calls), 1)
        self.assertEqual(wire.revoked, set())

    def test_duplicate_json_keys_are_retained_without_success_or_followup_http(self):
        def corrupt(wire, call, response):
            if len(wire.calls) == 1:
                return wire.wire(200, raw=b'{"AccessToken":"private-first","AccessToken":"private-second"}')
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(wire.calls), 1)
        responses = list((self.fixture.output / "private").glob("*-response.json"))
        self.assertEqual(len(responses), 1)
        private = json.loads(responses[0].read_text())
        self.assertIn(base64.b64encode(b'{"AccessToken":"private-first","AccessToken":"private-second"}').decode(),
                      json.dumps(private))

    def test_oversized_response_blocks_before_consumption_and_preserves_capture_bound(self):
        def corrupt(wire, call, response):
            if len(wire.calls) == 1:
                return wire.wire(200, raw=b"x" * (self.fixture.manifest["budgets"]["responseBytes"] + 10))
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(wire.calls), 1)
        for path in (self.fixture.output / "private").glob("*-response.json"):
            value = json.loads(path.read_text())
            body_keys = [key for key in value if key == "rawBase64" or key.lower().endswith("base64") and "response" in key.lower()]
            self.assertTrue(body_keys)
            for key in body_keys:
                self.assertLessEqual(len(base64.b64decode(value[key])), self.fixture.manifest["budgets"]["responseBytes"] + 1)

    def test_own_token_visibility_failure_cannot_fall_back_to_administrator(self):
        target = "/emby/Users/" + self.fixture.actor_ids["P"] + "/Items/" + self.fixture.item_ids["A1"]
        def corrupt(wire, call, response):
            if call["request"].route == target and not call["request"].cleanup:
                return wire.wire(403, {"error": "synthetic restricted visibility unavailable"})
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        attempted = [call for call in wire.calls if call["request"].route == target]
        self.assertTrue(attempted)
        self.assertTrue(all(call["headers"]["X-Emby-Token"] == wire.tokens["P"] for call in attempted))
        self.assertFalse(any(call["request"].route == "/emby/Sessions/Playing" for call in wire.calls))

    def test_scan_with_extra_playable_media_cannot_freeze_catalog(self):
        def corrupt(wire, call, response):
            request = call["request"]
            query = parse_qs(urlsplit(request.route).query)
            if request.method == "GET" and query.get("ParentId", [None])[0] == self.fixture.library_ids["LA"]:
                body = json.loads(response.raw)
                body["Items"].append({"Id": "synthetic-extra-playable", "Type": "Movie", "Name": "Unplanned Movie"})
                body["TotalRecordCount"] += 1
                return wire.wire(200, body)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        self.assertFalse(any(call["request"].route == "/emby/Users/New" for call in wire.calls))

    def test_missing_view_source_root_relation_stops_before_account_creation(self):
        target = "/emby/Users/" + self.fixture.actor_ids["admin"] + "/Items/" + self.fixture.view_ids["LA"]
        def corrupt(wire, call, response):
            if call["request"].route == target:
                body = json.loads(response.raw)
                body.pop("Path", None)
                body.pop("ParentId", None)
                return wire.wire(200, body)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        self.assertFalse(any(call["request"].route == "/emby/Users/New" for call in wire.calls))

    def test_wrong_runtime_or_frame_rate_cannot_establish_owned_episode_mapping(self):
        def corrupt(wire, call, response):
            query = parse_qs(urlsplit(call["request"].route).query)
            if query.get("ParentId", [None])[0] == self.fixture.library_ids["LA"]:
                body = json.loads(response.raw)
                for item in body["Items"]:
                    if item.get("Type") == "Episode":
                        item["RunTimeTicks"] = 120_000_000
                        item["MediaStreams"][0]["AverageFrameRate"] = 24
                return wire.wire(200, body)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)

    def test_unrelated_userdata_drift_during_playback_prevents_cleanup_attestation(self):
        target = "/emby/Users/" + self.fixture.actor_ids["P"] + "/Items/" + self.fixture.item_ids["A1"]
        def corrupt(wire, call, response):
            if call["request"].route == target and wire.states["P"]["A1"]["PlayCount"]:
                body = json.loads(response.raw)
                body["UserData"]["IsFavorite"] = True
                return wire.wire(200, body)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)

    def test_nonrestored_full_userdata_after_delete_cannot_publish_draft(self):
        def corrupt(wire, call, response):
            if call["request"].method == "DELETE":
                wire.states["P"]["A1"]["LastPlayedDate"] = self.clock.utc_now()
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)

    def test_old_public_configuration_drift_cannot_publish_draft(self):
        seen = []
        def corrupt(wire, call, response):
            if call["request"].route == "/emby/System/Configuration":
                seen.append(call["request"].label)
                if len(seen) > 1:
                    body = json.loads(response.raw)
                    body["UnexplainedOldConfiguration"] = True
                    return wire.wire(200, body)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        self.assertGreaterEqual(len(seen), 2)

    def test_known_progress_rejection_only_cleans_acknowledged_owned_responsibilities(self):
        rejected = []
        def reject(wire, call, response):
            if call["request"].route == "/emby/Sessions/Playing/Progress" and not rejected:
                rejected.append(call["request"].label)
                return wire.wire(500, {"error": "synthetic acknowledged rejection"})
        runner, wire, unused_authority = self.runner(response_hook=reject)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(rejected), 1)
        index = next(index for index, call in enumerate(wire.calls) if call["request"].label == rejected[0])
        self.assertTrue(all(call["request"].cleanup for call in wire.calls[index + 1:]))
        stops = [call for call in wire.calls if call["request"].route == "/emby/Sessions/Playing/Stopped"]
        self.assertEqual(len(stops), 1)
        self.assertEqual(stops[0]["headers"]["X-Emby-Token"], wire.tokens["P"])
        self.assertTrue(result["cleanupComplete"], result)
        self.assertEqual(wire.states["P"]["A1"], zero_state("A1"))


class WireContractGuards(GuardCase):
    def reject_first_response(self, transform):
        def corrupt(wire, call, response):
            if len(wire.calls) == 1:
                return transform(wire, response)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(wire.calls), 1)
        self.assertFalse(result["cleanupComplete"])

    def test_declared_length_mismatch_is_not_a_complete_consumable_response(self):
        self.reject_first_response(lambda wire, response: wire.wire(200, raw=response.raw,
            headers=[("Content-Type", "application/json"), ("Content-Length", str(len(response.raw) + 1))]))

    def test_conflicting_content_lengths_are_rejected_without_next_http(self):
        self.reject_first_response(lambda wire, response: wire.wire(200, raw=response.raw,
            headers=[("Content-Type", "application/json"), ("Content-Length", str(len(response.raw))),
                     ("Content-Length", str(len(response.raw) + 1))]))

    def test_mixed_response_framing_cannot_prove_complete_http(self):
        self.reject_first_response(lambda wire, response: wire.wire(200, raw=response.raw,
            headers=[("Content-Type", "application/json"), ("Content-Length", str(len(response.raw))),
                     ("Transfer-Encoding", "chunked")]))

    def test_response_status_requires_an_actual_integer(self):
        self.reject_first_response(lambda wire, response: wire.wire(200.0, raw=response.raw))

    def test_conflicting_response_media_types_are_not_consumable(self):
        self.reject_first_response(lambda wire, response: wire.wire(200, raw=response.raw,
            headers=[("Content-Type", "application/json"), ("Content-Type", "text/html")]))

    def test_non_utf8_response_is_preserved_without_login_ownership(self):
        self.reject_first_response(lambda wire, response: wire.wire(200, raw=b'{"AccessToken":"\xff"}'))

    def test_nonfinite_json_response_is_not_a_login_acknowledgement(self):
        self.reject_first_response(lambda wire, response: wire.wire(200, raw=b'{"AccessToken":NaN}'))


class DevicesDecoderGuards(GuardCase):
    """Confine the observed zero-count sentinel to the exact Devices endpoint."""

    def rows(self):
        return [deepcopy(self.fixture.baseline["devices"]["old-device-0"]),
                {"Id": "synthetic-device-second", "ReportedDeviceId": "synthetic-reported-second"}]

    def test_devices_zero_count_accepts_nonempty_items_while_catalog_still_rejects_them(self):
        value = {"Items": self.rows(), "TotalRecordCount": 0}
        before = deepcopy(value)
        self.assertEqual(self.P.devices_page(value), {row["Id"]: row for row in value["Items"]})
        self.assertEqual(value, before)
        with self.assertRaises(self.P.PreparationError):
            self.P.page(value)
        with self.assertRaises(self.P.PreparationError):
            self.P.page({"Items": [{"Id": "synthetic-catalog-item", "Type": "Episode"}], "TotalRecordCount": 0})
        self.assertFalse(self.fixture.output.exists())

    def test_devices_exact_count_and_empty_zero_count_are_valid(self):
        rows = self.rows()
        self.assertEqual(self.P.devices_page({"Items": rows, "TotalRecordCount": len(rows)}),
                         {row["Id"]: row for row in rows})
        self.assertEqual(self.P.devices_page({"Items": [], "TotalRecordCount": 0}), {})

    def test_devices_decoder_requires_exact_top_level_shape_and_array_items(self):
        values = (None, [], {}, {"Items": []}, {"TotalRecordCount": 0},
                  {"Items": [], "TotalRecordCount": 0, "StartIndex": 0},
                  {"Items": None, "TotalRecordCount": 0}, {"Items": {}, "TotalRecordCount": 0})
        for value in values:
            with self.subTest(value=value):
                with self.assertRaises((self.P.PreparationError, ValueError)):
                    self.P.devices_page(value)

    def test_devices_count_requires_integer_zero_or_exact_length(self):
        for count in (True, False, 0.0, 2.0, "0", -1, 1, 3, None):
            with self.subTest(count=count):
                with self.assertRaises(self.P.PreparationError):
                    self.P.devices_page({"Items": self.rows(), "TotalRecordCount": count})

    def test_devices_zero_count_does_not_remove_the_256_row_bound(self):
        rows = [{"Id": "synthetic-device-" + str(index), "ReportedDeviceId": "synthetic-reported-" + str(index)}
                for index in range(257)]
        self.assertEqual(len(self.P.devices_page({"Items": rows[:256], "TotalRecordCount": 0})), 256)
        with self.assertRaises(self.P.PreparationError):
            self.P.devices_page({"Items": rows, "TotalRecordCount": 0})

    def test_devices_rows_need_nonempty_string_identities_and_object_shape(self):
        for row in (None, [], {}, {"Id": "synthetic-one"}, {"ReportedDeviceId": "synthetic-one"},
                    {"Id": "", "ReportedDeviceId": "synthetic-one"}, {"Id": "synthetic-one", "ReportedDeviceId": ""},
                    {"Id": True, "ReportedDeviceId": "synthetic-one"}, {"Id": "synthetic-one", "ReportedDeviceId": None}):
            with self.subTest(row=row):
                with self.assertRaises((self.P.PreparationError, ValueError)):
                    self.P.devices_page({"Items": [row], "TotalRecordCount": 0})

    def test_devices_identities_are_independently_unique(self):
        rows = self.rows()
        duplicate_id = {**rows[1], "Id": rows[0]["Id"]}
        duplicate_reported = {**rows[1], "ReportedDeviceId": rows[0]["ReportedDeviceId"]}
        for duplicate in (duplicate_id, duplicate_reported):
            with self.subTest(duplicate=duplicate):
                with self.assertRaises(self.P.PreparationError):
                    self.P.devices_page({"Items": [rows[0], duplicate], "TotalRecordCount": 0})


class DevicesSnapshotGuards(GuardCase):
    """Verify the preserved registry and every acknowledged preparation device."""

    def corrupt_devices(self, transform, *, label="before-devices"):
        before = deepcopy(self.fixture.baseline)
        observed = []
        def corrupt(wire, call, response):
            if call["request"].route == "/emby/Devices" and call["request"].label == label:
                body = transform(deepcopy(json.loads(response.raw)))
                raw = encoded(body)
                observed.append({"ordinal": len(wire.calls), "label": label, "raw": raw})
                return wire.wire(200, raw=raw)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assertEqual(len(observed), 1, result)
        self.assert_failed(result)
        self.assertTrue(result["cleanupComplete"], result)
        self.assertIsNone(result["ownershipPending"])
        self.assertFalse(result["uncertain"])
        self.assertEqual(self.fixture.baseline, before, "Fake login or decoder mutated the frozen baseline.")
        self.assertTrue(all(call["request"].cleanup for call in wire.calls[observed[0]["ordinal"]:]),
                        "A failed device observation must stop normal preparation.")
        if label == "before-devices":
            self.assertFalse(any(call["request"].route in ("/emby/Library/VirtualFolders", "/emby/Users/New", "/emby/Sessions/Playing")
                                 for call in wire.calls))
        record = self.fixture.output / "private" / ("%04d-%s-response.json" % (observed[0]["ordinal"], label))
        self.assertEqual(base64.b64decode(json.loads(record.read_text())["rawBase64"]), observed[0]["raw"])
        self.assert_no_resume(runner, wire)

    def changed_owned(self, field, value, *, actor="admin", label="before-devices"):
        reported = self.fixture.manifest["actors"][actor]["deviceId"]
        def change(body):
            selected = next(row for row in body["Items"] if row["ReportedDeviceId"] == reported)
            selected[field] = value
            return body
        self.corrupt_devices(change, label=label)

    def test_zero_count_devices_support_complete_pipeline_and_independent_login_registry(self):
        before = deepcopy(self.fixture.baseline)
        runner, wire, unused_authority = self.runner()
        result = runner.run()
        self.assertTrue(result["completed"], result)
        self.assertEqual(self.fixture.baseline, before)
        self.assertEqual(set(wire.devices), set(before["devices"]) | {"synthetic-registered-" + actor for actor in ("admin", "P", "Q")})
        for actor in ("admin", "P", "Q"):
            row = wire.devices["synthetic-registered-" + actor]
            self.assertEqual(row["ReportedDeviceId"], self.fixture.manifest["actors"][actor]["deviceId"])
            self.assertEqual(row["LastUserId"], self.fixture.actor_ids[actor])
            self.assertEqual(row["LastUserName"], self.fixture.credentials[actor]["username"])
        initial = json.loads((self.fixture.output / "private" / "before-public.json").read_text())
        final = json.loads((self.fixture.output / "private" / "after-public.json").read_text())
        self.assertEqual(set(initial["devices"]), set(before["devices"]) | {"synthetic-registered-admin"})
        self.assertEqual(set(final["devices"]), set(wire.devices))
        for key, row in before["devices"].items():
            self.assertEqual(initial["devices"][key], row)
            self.assertEqual(final["devices"][key], row)

    def test_exact_device_count_also_supports_the_complete_pipeline(self):
        counts = []
        def exact(wire, call, response):
            if call["request"].route == "/emby/Devices":
                body = json.loads(response.raw)
                body["TotalRecordCount"] = len(body["Items"])
                counts.append(body["TotalRecordCount"])
                return wire.wire(200, body)
        runner, wire, unused_authority = self.runner(response_hook=exact)
        result = runner.run()
        self.assertTrue(result["completed"], result)
        self.assertEqual(counts, [2, 4])

    def test_snapshot_cannot_omit_a_preserved_old_device(self):
        self.corrupt_devices(lambda body: {**body, "Items": [row for row in body["Items"] if row["Id"] != "old-device-0"]})

    def test_snapshot_cannot_reassign_a_preserved_old_reported_device_identity(self):
        def change(body):
            next(row for row in body["Items"] if row["Id"] == "old-device-0")["ReportedDeviceId"] = "changed-old-reported-device"
            return body
        self.corrupt_devices(change)

    def test_snapshot_requires_the_verified_administrator_login_device(self):
        self.corrupt_devices(lambda body: {**body, "Items": [row for row in body["Items"] if row["Id"] != "synthetic-registered-admin"]})

    def test_after_snapshot_requires_the_verified_ordinary_login_device(self):
        self.corrupt_devices(lambda body: {**body, "Items": [row for row in body["Items"] if row["Id"] != "synthetic-registered-P"]},
                             label="after-devices")

    def test_snapshot_rejects_duplicate_registry_ids(self):
        def duplicate(body):
            row = deepcopy(body["Items"][-1])
            row["ReportedDeviceId"] = "duplicate-id-with-another-reported-device"
            body["Items"].append(row)
            return body
        self.corrupt_devices(duplicate)

    def test_snapshot_rejects_duplicate_reported_device_ids(self):
        def duplicate(body):
            row = deepcopy(body["Items"][-1])
            row["Id"] = "another-registry-id-with-duplicate-reported-device"
            body["Items"].append(row)
            return body
        self.corrupt_devices(duplicate)

    def test_snapshot_rejects_an_extra_unowned_device_even_with_valid_client_fields(self):
        def extra(body):
            row = deepcopy(body["Items"][-1])
            row.update(Id="synthetic-unowned-registry", ReportedDeviceId="synthetic-unowned-reported")
            body["Items"].append(row)
            return body
        self.corrupt_devices(extra)

    def test_snapshot_rejects_a_declared_actor_device_before_its_login_is_acknowledged(self):
        def extra(body):
            body["Items"].append({"Id": "synthetic-premature-P-registry",
                "ReportedDeviceId": self.fixture.manifest["actors"]["P"]["deviceId"],
                "AppName": "Goby NextUp Preparation", "AppVersion": "1.0", "Name": "Linux Fixture Recorder",
                "LastUserId": self.fixture.actor_ids["P"], "LastUserName": self.fixture.credentials["P"]["username"],
                "DateLastActivity": self.clock.utc_now()})
            return body
        self.corrupt_devices(extra)

    def test_snapshot_requires_the_exact_login_user_id(self):
        self.changed_owned("LastUserId", "old-user-1")

    def test_snapshot_requires_the_exact_login_username(self):
        self.changed_owned("LastUserName", "Another Synthetic User")

    def test_snapshot_requires_the_exact_preparation_application_name(self):
        self.changed_owned("AppName", "Another Synthetic Client")

    def test_snapshot_requires_the_exact_preparation_application_version(self):
        self.changed_owned("AppVersion", "2.0")

    def test_snapshot_requires_the_exact_preparation_device_name(self):
        self.changed_owned("Name", "Another Synthetic Device")

    def test_after_snapshot_checks_the_other_ordinary_actor_device_owner(self):
        self.changed_owned("LastUserId", self.fixture.actor_ids["P"], actor="Q", label="after-devices")

    def test_snapshot_rejects_an_invalid_owned_device_activity_timestamp(self):
        self.changed_owned("DateLastActivity", "not-a-timestamp")

    def test_snapshot_rejects_an_owned_device_activity_timestamp_without_timezone(self):
        self.changed_owned("DateLastActivity", "2026-09-13T01:00:01")

    def test_snapshot_rejects_nonzero_device_count_mismatch(self):
        self.corrupt_devices(lambda body: {**body, "TotalRecordCount": len(body["Items"]) + 1})


class OwnershipGuards(GuardCase):
    """Keep complete but unconsumed resource responses as durable responsibilities."""

    def assert_ownership_blocked(self, runner, wire, result, observed):
        self.assert_failed(result)
        self.assertEqual(result["status"], "recovery_required")
        self.assertFalse(result["cleanupComplete"])
        self.assertTrue(result["uncertain"])
        self.assertTrue(result["ownershipPending"])
        self.assertTrue(runner.uncertain)
        self.assertEqual(len(wire.calls), observed["ordinal"], "No HTTP may follow an unconsumed resource identity.")
        self.assertEqual(wire.calls[-1]["request"].label, observed["label"])
        state = json.loads((self.fixture.output / "private" / "state.json").read_text())
        self.assertTrue(state["ownershipPending"])
        self.assertTrue(state["uncertain"])
        record = self.fixture.output / "private" / ("%04d-%s-response.json" % (observed["ordinal"], observed["label"]))
        self.assertTrue(record.is_file())
        private = json.loads(record.read_text())
        self.assertEqual(private["status"], observed.get("status", 200))
        self.assertIs(private["completeHttp"], True)
        self.assertEqual(base64.b64decode(private["rawBase64"]), observed["raw"])
        label = observed["label"]
        owner_label = "create-" + label.removeprefix("created-library-lookup-") if label.startswith("created-library-lookup-") else label
        owned_call = next(call for call in wire.calls if call["request"].label == owner_label)
        owner_ordinal = wire.calls.index(owned_call) + 1
        owner_record = self.fixture.output / "private" / ("%04d-%s-response.json" % (owner_ordinal, owner_label))
        owner_route = owned_call["request"].route
        kind = ("playback" if owner_route.endswith("/PlaybackInfo") else "account" if owner_route == "/emby/Users/New" else
                "library" if owner_route == "/emby/Library/VirtualFolders" else "login")
        expected = {"label": owner_label, "actor": owned_call["request"].actor, "ordinal": owner_ordinal,
                    "responseReceiptSha256": digest(owner_record.read_bytes()), "kind": kind}
        for pending in (result["ownershipPending"], state["ownershipPending"]):
            for key, value in expected.items():
                self.assertEqual(pending[key], value)
            self.assertIn(pending["stage"], ("response-awaiting-owner", "reconciliation-awaiting-owner", "owner-registered"))
            if owner_label != label:
                self.assertEqual(pending["reconciliationResponse"], {"label": label, "ordinal": observed["ordinal"],
                    "responseReceiptSha256": digest(record.read_bytes())})
        self.assert_no_resume(runner, wire)

    def malformed_resource(self, match, transform):
        observed = []
        def corrupt(wire, call, response):
            if not observed and match(call["request"]):
                body = transform(json.loads(response.raw) if response.raw else None)
                raw = encoded(body)
                observed.append({"ordinal": len(wire.calls), "label": call["request"].label, "raw": raw})
                return wire.wire(200, raw=raw)
        runner, wire, unused_authority = self.runner(response_hook=corrupt)
        result = runner.run()
        self.assertEqual(len(observed), 1, result)
        self.assert_ownership_blocked(runner, wire, result, observed[0])

    @staticmethod
    def login(request):
        return request.actor == "admin" and request.route == "/emby/Users/AuthenticateByName"

    @staticmethod
    def account(request):
        return request.route == "/emby/Users/New"

    @staticmethod
    def playback(request):
        return request.route.endswith("/PlaybackInfo")

    def test_complete_login_json_null_remains_an_unconsumed_ownership_response(self):
        self.malformed_resource(self.login, lambda body: None)

    def test_complete_login_json_list_remains_an_unconsumed_ownership_response(self):
        self.malformed_resource(self.login, lambda body: [])

    def test_complete_login_null_user_retains_ownership_responsibility(self):
        self.malformed_resource(self.login, lambda body: {**body, "User": None})

    def test_complete_login_list_user_retains_ownership_responsibility(self):
        self.malformed_resource(self.login, lambda body: {**body, "User": []})

    def test_complete_login_null_policy_cannot_release_ownership_responsibility(self):
        self.malformed_resource(self.login, lambda body: {**body, "User": {**body["User"], "Policy": None}})

    def test_complete_login_list_policy_cannot_release_ownership_responsibility(self):
        self.malformed_resource(self.login, lambda body: {**body, "User": {**body["User"], "Policy": []}})

    def test_complete_login_null_session_info_retains_ownership_responsibility(self):
        self.malformed_resource(self.login, lambda body: {**body, "SessionInfo": None})

    def test_complete_login_list_session_info_retains_ownership_responsibility(self):
        self.malformed_resource(self.login, lambda body: {**body, "SessionInfo": []})

    def test_ordinary_login_malformed_policy_cannot_trigger_administrator_cleanup_http(self):
        self.malformed_resource(lambda request: request.actor == "P" and request.route == "/emby/Users/AuthenticateByName",
            lambda body: {**body, "User": {**body["User"], "Policy": None}})

    def test_complete_new_user_json_null_keeps_creation_responsibility(self):
        self.malformed_resource(self.account, lambda body: None)

    def test_complete_new_user_json_list_keeps_creation_responsibility(self):
        self.malformed_resource(self.account, lambda body: [])

    def test_complete_new_user_null_policy_keeps_creation_responsibility(self):
        self.malformed_resource(self.account, lambda body: {**body, "Policy": None})

    def test_complete_new_user_list_policy_keeps_creation_responsibility(self):
        self.malformed_resource(self.account, lambda body: {**body, "Policy": []})

    def test_complete_library_list_acknowledgement_cannot_advance_to_next_create(self):
        self.malformed_resource(lambda request: request.route == "/emby/Library/VirtualFolders" and request.method == "POST",
                                lambda body: [])

    def test_complete_library_lookup_null_row_preserves_the_unmapped_creation(self):
        self.malformed_resource(lambda request: request.label == "created-library-lookup-LA",
                                lambda body: {"Items": [None], "TotalRecordCount": 1})

    def test_complete_playback_info_json_null_keeps_session_ownership_unresolved(self):
        self.malformed_resource(self.playback, lambda body: None)

    def test_complete_playback_info_json_list_keeps_session_ownership_unresolved(self):
        self.malformed_resource(self.playback, lambda body: [])

    def test_complete_playback_info_null_media_sources_keeps_session_ownership_unresolved(self):
        self.malformed_resource(self.playback, lambda body: {**body, "MediaSources": None})

    def test_complete_playback_info_null_media_source_keeps_session_ownership_unresolved(self):
        self.malformed_resource(self.playback, lambda body: {**body, "MediaSources": [None]})

    def test_complete_playback_info_list_media_source_keeps_session_ownership_unresolved(self):
        self.malformed_resource(self.playback, lambda body: {**body, "MediaSources": [[]]})

    def test_complete_playback_info_object_media_sources_keeps_session_ownership_unresolved(self):
        self.malformed_resource(self.playback, lambda body: {**body, "MediaSources": {}})

    def duplicate_play_session(self, target_actor):
        prior_ids = []
        observed = []
        def duplicate(wire, call, response):
            request = call["request"]
            if not request.route.endswith("/PlaybackInfo"):
                return None
            body = json.loads(response.raw)
            should_repeat = (target_actor == "P" and request.actor == "P" and len(prior_ids) == 1 or
                             target_actor == "Q" and request.actor == "Q" and len(prior_ids) == 2)
            if should_repeat:
                self.assertFalse(wire.started, "The reused identity must belong to an already stopped lifecycle.")
                body["PlaySessionId"] = prior_ids[0]
                raw = encoded(body)
                observed.append({"ordinal": len(wire.calls), "label": request.label, "raw": raw})
                return wire.wire(200, raw=raw)
            prior_ids.append(body["PlaySessionId"])
            return None
        runner, wire, unused_authority = self.runner(response_hook=duplicate)
        result = runner.run()
        self.assertEqual(len(observed), 1, result)
        self.assert_ownership_blocked(runner, wire, result, observed[0])
        state = json.loads((self.fixture.output / "private" / "state.json").read_text())
        self.assertTrue(set(prior_ids) <= set(state["playSessionIds"]))

    def test_same_actor_cannot_reuse_its_already_stopped_partial_play_session(self):
        self.duplicate_play_session("P")

    def test_playback_info_cannot_reuse_an_older_session_displaced_by_a_later_completed_one(self):
        self.duplicate_play_session("Q")

    def fail_ownership_persistence(self, label, *, clearing):
        observed = {}
        failures = []
        registered = []
        def observe(wire, call, response):
            observed.clear()
            observed.update(ordinal=len(wire.calls), label=call["request"].label,
                            raw=response.raw, status=response.status)
        def factory(root, uid):
            journal = self.T.Journal(root, uid=uid)
            original = journal.state
            def state(value):
                pending = value.get("ownershipPending")
                selected = (isinstance(pending, dict) and pending.get("label") == label and
                            pending.get("stage") == "owner-registered")
                if selected:
                    registered.append(deepcopy(pending))
                fail = (selected and not clearing or clearing and registered and pending is None)
                if fail and not failures:
                    failures.append({"state": deepcopy(value), "observed": deepcopy(observed)})
                    raise OSError("Synthetic ownership registration persistence failure.")
                return original(value)
            journal.state = state
            return journal
        runner, wire, unused_authority = self.runner(response_hook=observe, journal_factory=factory)
        result = runner.run()
        self.assertEqual(len(failures), 1, result)
        self.assertTrue(registered, "The intended resource identity never reached its registration boundary.")
        self.assert_ownership_blocked(runner, wire, result, failures[0]["observed"])

    def test_login_owner_registration_persistence_failure_retains_pending_response(self):
        self.fail_ownership_persistence("login-admin", clearing=False)

    def test_login_owner_release_persistence_failure_restores_pending_response(self):
        self.fail_ownership_persistence("login-admin", clearing=True)

    def test_account_owner_registration_persistence_failure_retains_pending_response(self):
        self.fail_ownership_persistence("create-user-P", clearing=False)

    def test_account_owner_release_persistence_failure_restores_pending_response(self):
        self.fail_ownership_persistence("create-user-P", clearing=True)

    def test_library_owner_registration_persistence_failure_retains_pending_response(self):
        self.fail_ownership_persistence("create-LA", clearing=False)

    def test_library_owner_release_persistence_failure_restores_pending_response(self):
        self.fail_ownership_persistence("create-LA", clearing=True)

    def test_playback_owner_registration_persistence_failure_retains_pending_response(self):
        self.fail_ownership_persistence("cal-P-partial-info", clearing=False)

    def test_playback_owner_release_persistence_failure_restores_pending_response(self):
        self.fail_ownership_persistence("cal-P-partial-info", clearing=True)


class PersistenceGuards(GuardCase):
    def fail_save(self, suffix, *, export=False, attempts):
        failures = []
        def factory(root, uid):
            journal = self.T.Journal(root, uid=uid)
            original = journal.save
            def save(name, value, *, export=False):
                if name.endswith(suffix) and export == expected_export:
                    failures.append(name)
                    raise OSError("Synthetic durable storage failure.")
                return original(name, value, export=export)
            expected_export = export
            journal.save = save
            return journal
        runner, wire, unused_authority = self.runner(journal_factory=factory)
        result = runner.run()
        self.assert_failed(result)
        self.assertTrue(failures)
        self.assertEqual(len(wire.calls), attempts)
        self.assert_no_resume(runner, wire)

    def test_intent_persistence_failure_prevents_first_http(self):
        self.fail_save("-intent.json", attempts=0)

    def test_reservation_persistence_failure_prevents_first_http(self):
        self.fail_save("-reserved.json", attempts=0)

    def test_private_response_persistence_failure_prevents_consumption_and_followup(self):
        self.fail_save("-response.json", attempts=1)

    def test_export_response_persistence_failure_prevents_consumption_and_followup(self):
        self.fail_save("-response.json", export=True, attempts=1)

    def test_pending_state_persistence_failure_prevents_http(self):
        def factory(root, uid):
            journal = self.T.Journal(root, uid=uid)
            original = journal.state
            def state(value):
                if value.get("pending") is not None:
                    raise OSError("Synthetic pending state persistence failure.")
                return original(value)
            journal.state = state
            return journal
        runner, wire, unused_authority = self.runner(journal_factory=factory)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(wire.calls), 0)

    def test_existing_output_root_is_rejected_without_adoption_or_overwrite(self):
        self.fixture.output.mkdir(mode=0o700)
        sentinel = self.fixture.output / "owned-sentinel.txt"
        self.fixture.write(sentinel, b"Must remain byte-for-byte unchanged.\n")
        runner, wire, unused_authority = self.runner()
        try:
            result = runner.run()
        except (self.P.PreparationError, self.T.TransportError, FileExistsError):
            result = None
        if result is not None:
            self.assert_failed(result)
        self.assertEqual(len(wire.calls), 0)
        self.assertEqual(sentinel.read_bytes(), b"Must remain byte-for-byte unchanged.\n")
        self.assertEqual(list(self.fixture.output.iterdir()), [sentinel])

    def test_authority_drift_during_pending_state_persistence_prevents_http(self):
        authority_holder = []
        def factory(root, uid):
            journal = self.T.Journal(root, uid=uid)
            original = journal.state
            def state(value):
                result = original(value)
                if value.get("pending") is not None and authority_holder:
                    authority_holder[0].failure_at = authority_holder[0].checks + 1
                return result
            journal.state = state
            return journal
        runner, wire, authority = self.runner(journal_factory=factory)
        authority_holder.append(authority)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(wire.calls), 0)

    def test_monotonic_deadline_consumed_by_pending_persistence_prevents_http(self):
        def factory(root, uid):
            journal = self.T.Journal(root, uid=uid)
            original = journal.state
            def state(value):
                result = original(value)
                if value.get("pending") is not None:
                    self.clock.value += self.fixture.manifest["budgets"]["normalSeconds"] + 1
                return result
            journal.state = state
            return journal
        runner, wire, unused_authority = self.runner(journal_factory=factory)
        result = runner.run()
        self.assert_failed(result)
        self.assertEqual(len(wire.calls), 0)


class DispatchBudgetGuards(GuardCase):
    def prepared_dispatch(self):
        runner, wire, authority = self.runner()
        authority.acquire()
        runner.journal = self.T.Journal(self.fixture.output, uid=self.fixture.manifest["ownerUid"])
        runner.started = runner._now()
        runner._persist()
        return runner, wire, authority

    def login_dispatch(self, runner, label="synthetic-budget-login"):
        credential = runner.authority.credentials["admin"]
        return runner._dispatch(label, "admin", "POST", "/emby/Users/AuthenticateByName",
            {"Username": credential["username"], "Pw": credential["password"]}, form=True)

    def test_normal_count_cannot_spend_reserved_cleanup_capacity(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.normal_count = 280
        runner.count = 280
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_total_count_has_independent_hard_limit(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.phase = "cleanup"
        runner.cleanup_started = self.clock()
        runner.count = 380
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_cleanup_count_is_independently_bounded(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.phase = "cleanup"
        runner.cleanup_started = self.clock()
        runner.cleanup_count = 100
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_phase_limit_cannot_use_unused_other_phase_slots(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.phase_counts["before"] = 44
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_normal_response_byte_budget_preserves_all_cleanup_slots(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        budget = self.fixture.manifest["budgets"]
        runner.charged_bytes = budget["totalResponseBytes"] - budget["cleanupResponseBytes"] - budget["responseBytes"]
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_cleanup_response_byte_limit_cannot_overdraw_total(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.phase = "cleanup"
        runner.cleanup_started = self.clock()
        budget = self.fixture.manifest["budgets"]
        runner.charged_bytes = budget["totalResponseBytes"] - budget["responseBytes"]
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_request_body_limit_is_checked_before_reservation_or_http(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.authority.credentials["admin"]["password"] = "X" * 40000
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(runner.count, 0)
        self.assertEqual(len(wire.calls), 0)

    def test_normal_deadline_prevents_dispatch(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        self.clock.value = runner.started + self.fixture.manifest["budgets"]["normalSeconds"] + 1
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_cleanup_deadline_prevents_dispatch(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.phase = "cleanup"
        runner.cleanup_started = self.clock()
        self.clock.value += self.fixture.manifest["budgets"]["cleanupSeconds"] + 1
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_nonfinite_clock_prevents_http(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        self.clock.value = float("nan")
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_clock_regression_cannot_extend_dispatch_authority(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        self.clock.value = runner.started - 10
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 0)

    def test_reused_request_label_cannot_dispatch_twice(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        self.login_dispatch(runner)
        with self.assertRaises(self.P.PreparationError):
            self.login_dispatch(runner)
        self.assertEqual(len(wire.calls), 1)

    def test_route_allowlist_rejects_global_scan_cross_user_and_duplicate_query(self):
        runner, wire, unused_authority = self.prepared_dispatch()
        runner.tokens["admin"] = wire.tokens["admin"]
        cases = [
            ("admin", "POST", "/emby/Library/Refresh", None, False),
            ("admin", "GET", "http://192.0.2.1/emby/Users", None, False),
            ("admin", "GET", "/emby/Users?x=1&X=2", None, False),
            ("admin", "GET", "/emby/Users/unknown-user/Items/unknown-item", None, False),
            ("admin", "POST", "/emby/Users/old-user-1/Policy", {"IsAdministrator": True}, False),
            ("admin", "DELETE", "/emby/Users/old-user-1/PlayedItems/old-item-1", None, False),
        ]
        for actor, method, route, body, form in cases:
            with self.subTest(route=route):
                with self.assertRaises(self.P.PreparationError):
                    runner._guard_route(actor, method, route, body, form)
        self.assertEqual(len(wire.calls), 0)


class GuidMappingGuards(GuardCase):
    def reject_mapping(self, mutate):
        runner, wire, unused = self.runner()
        def response_hook(current, call, response):
            if call["request"].route != "/emby/Library/SelectableMediaFolders": return None
            body = json.loads(response.raw)
            mutate(body)
            return current.wire(200, body)
        wire.response_hook = response_hook
        result = runner.run()
        self.assert_failed(result)
        self.assertFalse(any(call["request"].route == "/emby/Users/New" for call in wire.calls))

    def test_missing_guid_blocks_account_creation(self):
        self.reject_mapping(lambda rows: rows[0].pop("Guid"))

    def test_duplicate_guid_blocks_account_creation(self):
        self.reject_mapping(lambda rows: rows[1].update(Guid=rows[0]["Guid"]))

    def test_wrong_owned_root_blocks_account_creation(self):
        self.reject_mapping(lambda rows: rows[0]["SubFolders"][0].update(Path="/synthetic/foreign-root"))

    def test_numeric_management_ids_are_not_policy_guids(self):
        self.reject_mapping(lambda rows: rows[0].update(Guid=rows[0]["Id"]))

    def test_fake_browse_access_requires_guid_pair_instead_of_policy_echo(self):
        unused, wire, unused_authority = self.runner()
        actor, user = "P", self.fixture.actor_ids["P"]
        wire.users[user] = self.fixture.profile(actor)
        request = SimpleNamespace(method="GET", route="/emby/Users/" + user + "/Views", actor=actor, label="synthetic-own-views")
        wire.users[user]["Policy"]["EnabledFolders"] = list(self.fixture.library_ids.values())
        denied = wire.respond(request, {"X-Emby-Token": wire.tokens[actor]}, None)
        self.assertEqual(self.P.page(json.loads(denied.raw)), {})
        wire.users[user]["Policy"]["EnabledFolders"] = list(self.fixture.policy_guids.values())
        allowed = wire.respond(request, {"X-Emby-Token": wire.tokens[actor]}, None)
        self.assertEqual(set(self.P.page(json.loads(allowed.raw))), set(self.fixture.view_ids.values()))


def add_grant_verification_fixture(fixture, release):
    """Add self-owned synthetic Guid evidence without observing a real service."""
    import base64
    from copy import deepcopy
    from datetime import datetime, timedelta
    import hashlib
    import json
    from pathlib import Path
    import shlex
    from urllib.parse import urlencode
    import uuid

    def encode(value):
        return (json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n").encode()

    def checksum(raw):
        return hashlib.sha256(raw).hexdigest()

    def save(path, value):
        fixture.write(path, encode(value))
        return fixture.descriptor(path)

    manifest = fixture.manifest
    baseline = deepcopy(fixture.baseline)
    preservation = manifest["preservation"]
    assert len(preservation["userIds"]) == 8
    assert len(preservation["libraryIds"]) == 10
    assert len(preservation["detailRoutes"]) == 12
    assert set(baseline["roster"]) == set(preservation["userIds"])
    assert set(baseline["libraries"]) == set(preservation["libraryIds"])
    admin_id = manifest["actors"]["admin"]["userId"]
    viewer_id = "old-user-6"
    assert admin_id == "old-user-0" and viewer_id in baseline["roster"]
    assert not {fixture.actor_ids[actor] for actor in ("P", "Q")}.intersection(baseline["roster"])
    witness_routes = [row for row in preservation["detailRoutes"] if row["group"] == "grant-P"]
    assert len(witness_routes) == 6 and all(row["userId"] == viewer_id for row in witness_routes)
    assert len({row["itemId"] for row in witness_routes}) == 6

    root = Path(manifest["sealedRoots"][0]) / "synthetic-grant-verification"
    assert fixture.root in root.parents
    root.mkdir(mode=0o700)
    private = root / "private"
    private.mkdir(mode=0o700)
    source_bytes = Path(PREPARATION_SOURCE).with_name("verify-folder-grant-01.py").read_bytes()
    assert checksum(source_bytes) == "70087cdaeae927c3091b5abcfc1df0c9203cdf35e17d28e5222f491bad46a971"
    source_path = root / "verify-folder-grant-01.py"
    fixture.write(source_path, source_bytes)
    source_descriptor = fixture.descriptor(source_path)
    run_id = "synthetic-folder-grant-verification"
    base = datetime.fromisoformat(baseline["captured_at"].replace("Z", "+00:00")) - timedelta(minutes=2)

    def created_at(ordinal):
        return (base + timedelta(seconds=ordinal / 2)).isoformat()

    def completed_at(ordinal):
        return (base + timedelta(seconds=ordinal / 2, milliseconds=125)).isoformat()

    def snapshot_at(ordinal):
        return (base + timedelta(seconds=ordinal / 2, milliseconds=250)).isoformat()

    actors = {
        actor: {"userId": user, "username": baseline["roster"][user]["Name"],
                "credentialRef": "synthetic-grant-credential-" + actor,
                "deviceId": "synthetic-grant-device-" + actor}
        for actor, user in (("admin", admin_id), ("P", viewer_id))
    }
    tokens = {actor: "synthetic-grant-token-" + actor + "-" + actor[0] * 40 for actor in actors}
    grants = [
        {"libraryId": preservation["libraryIds"][-2], "guid": "11111111111141118111111111111111"},
        {"libraryId": preservation["libraryIds"][-1], "guid": "22222222222242228222222222222222"},
    ]
    original = deepcopy(baseline["roster"][viewer_id]["Policy"])
    original.update(IsAdministrator=False, EnableAllFolders=False,
                    EnabledFolders=[row["libraryId"] for row in grants], IsDisabled=False)
    target = deepcopy(original)
    target["EnabledFolders"] = [row["guid"] for row in grants]
    baseline["roster"][viewer_id]["Policy"] = deepcopy(original)
    episode_ids = {row["itemId"] for row in witness_routes}
    for rows in baseline["catalog_by_library"].values():
        for item_id in episode_ids:
            rows.pop(item_id, None)
    episode_bodies = {}
    for index, route in enumerate(witness_routes):
        symbol = ("A", "B")[index // 3] + str(index % 3 + 1)
        library = grants[index // 3]["libraryId"]
        season, episode = ((1, 1), (1, 2), (2, 1))[index % 3]
        series_id = "synthetic-grant-series-" + symbol[0]
        season_id = "synthetic-grant-season-" + symbol[0] + str(season)
        media_path = str(Path(baseline["libraries"][library]["Locations"][0]) /
                         ("Synthetic Grant Series " + symbol[0]) / ("Season %02d" % season) /
                         ("Synthetic Grant " + symbol + ".mp4"))
        body = {"Id": route["itemId"], "Name": "Synthetic Grant Episode " + symbol,
            "Type": "Episode", "IsFolder": False, "ParentId": season_id, "SeasonId": season_id,
            "SeriesId": series_id, "IndexNumber": episode, "ParentIndexNumber": season,
            "RunTimeTicks": 6_000_000_000, "Path": media_path,
            "MediaSources": [{"Id": "synthetic-grant-source-" + symbol, "Path": media_path,
                              "RunTimeTicks": 6_000_000_000}],
            "MediaStreams": [{"Type": "Video", "AverageFrameRate": 30, "RealFrameRate": 30}],
            "UserData": {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0,
                         "LastPlayedDate": None, "IsFavorite": False, "Key": "synthetic-grant-" + symbol}}
        episode_bodies[symbol] = deepcopy(body)
        baseline["catalog_by_library"][library][route["itemId"]] = deepcopy(body)
        for rows in baseline["items_by_user"].values():
            rows[route["itemId"]] = deepcopy(body)
        for rows in baseline["details"].values():
            if route["itemId"] in rows:
                rows[route["itemId"]] = deepcopy(body)
        baseline["details"]["grant-P"][route["itemId"]] = deepcopy(body)

    assert len(baseline["devices"]) <= 91
    retained_index = 0
    while len(baseline["devices"]) < 91:
        registry = "synthetic-grant-retained-registry-%03d" % retained_index
        reported = "synthetic-grant-retained-device-%03d" % retained_index
        retained_index += 1
        if registry in baseline["devices"] or any(
                row["ReportedDeviceId"] == reported for row in baseline["devices"].values()):
            continue
        user_id = preservation["userIds"][retained_index % len(preservation["userIds"])]
        baseline["devices"][registry] = {"Id": registry, "ReportedDeviceId": reported,
            "LastUserId": user_id, "LastUserName": baseline["roster"][user_id]["Name"],
            "AppName": "Synthetic Retained Client", "AppVersion": "0.1", "Name": "Synthetic Retained Device",
            "DateLastActivity": (base - timedelta(minutes=1)).isoformat()}

    def device(actor, ordinal):
        return {"Id": "synthetic-grant-registry-" + actor, "ReportedDeviceId": actors[actor]["deviceId"],
            "LastUserId": actors[actor]["userId"], "LastUserName": actors[actor]["username"],
            "AppName": "Goby Folder Grant Verifier", "AppVersion": "1.0", "Name": "Linux Fixture Recorder",
            "DateLastActivity": completed_at(ordinal)}

    before = deepcopy(baseline)
    before["captured_at"] = snapshot_at(44)
    before["credential_context"] = {"channel": "controller_api", "authenticated_user_id": admin_id,
        "token_sha256": checksum(tokens["admin"].encode()), "user_id_semantics": "subject_projection"}
    before["devices"]["synthetic-grant-registry-admin"] = device("admin", 1)
    after = deepcopy(before)
    after["captured_at"] = snapshot_at(105)
    after["devices"]["synthetic-grant-registry-P"] = device("P", 48)
    assert len(before["devices"]) == 92 and len(after["devices"]) == 93

    fields = "Path,ParentId,SortName,MediaSources,MediaStreams,Overview,Genres,Tags,People,Studios,ProviderIds,DateCreated,ProductionYear"

    def query(user_id, *, parent=None, ids=None):
        parameters = {"Recursive": "true", "Fields": fields, "EnableUserData": "true",
                      "EnableTotalRecordCount": "true", "Limit": "256"}
        if parent is not None:
            parameters["ParentId"] = parent
        if ids is not None:
            parameters["Ids"] = ",".join(sorted(ids))
        return "/emby/Users/" + user_id + "/Items?" + urlencode(parameters)

    def page(rows, *, devices=False):
        values = [deepcopy(rows[key]) for key in sorted(rows)]
        return {"Items": values, "TotalRecordCount": 0 if devices else len(values)}

    events = []

    def add(label, actor, method, route, response_body=None, *, body=None, status=200, form=False):
        events.append({"label": label, "actor": actor, "method": method, "route": route,
                       "responseBody": deepcopy(response_body), "body": deepcopy(body), "status": status, "form": form})

    def login(actor, ordinal):
        user = deepcopy(before["roster"][actors[actor]["userId"]])
        user["Policy"] = deepcopy(target if actor == "P" else user["Policy"])
        user["LastLoginDate"] = created_at(ordinal)
        add("login-" + actor, actor, "POST", "/emby/Users/AuthenticateByName",
            {"ServerId": manifest["server"]["id"], "User": user, "AccessToken": tokens[actor],
             "SessionInfo": {"Id": "synthetic-grant-session-" + actor, "UserId": user["Id"],
                             "DeviceId": actors[actor]["deviceId"]}},
            body={"Username": actors[actor]["username"], "Pw": "synthetic-grant-password-" + actor + "-" + "x" * 40}, form=True)

    def snapshot(prefix, value):
        add(prefix + "-server", "admin", "GET", "/emby/System/Info/Public", value["server"])
        add(prefix + "-users", "admin", "GET", "/emby/Users", [value["roster"][key] for key in sorted(value["roster"])])
        add(prefix + "-libraries", "admin", "GET", "/emby/Library/VirtualFolders/Query", page(value["libraries"]))
        all_ids = {item for rows in value["catalog_by_library"].values() for item in rows}
        for index, library in enumerate(sorted(value["libraries"])):
            add(prefix + "-catalog-" + str(index), "admin", "GET", query(admin_id, parent=library),
                page(value["catalog_by_library"][library]))
        for index, user in enumerate(sorted(value["roster"])):
            add(prefix + "-items-" + str(index), "admin", "GET", query(user, ids=all_ids), page(value["items_by_user"][user]))
            add(prefix + "-prefs-" + str(index), "admin", "GET", "/emby/UserSettings/" + user, value["preferences"][user])
        for index, route in enumerate(preservation["detailRoutes"]):
            add(prefix + "-detail-" + str(index), "admin", "GET",
                "/emby/Users/" + route["userId"] + "/Items/" + route["itemId"], value["details"][route["group"]][route["itemId"]])
        add(prefix + "-devices", "admin", "GET", "/emby/Devices", page(value["devices"], devices=True))
        add(prefix + "-configuration", "admin", "GET", "/emby/System/Configuration", value["configuration"])

    def profile(policy):
        value = deepcopy(before["roster"][viewer_id])
        value["Policy"] = deepcopy(policy)
        return value

    login("admin", 1)
    snapshot("before", before)
    assert len(events) == 44
    profile_route = "/emby/Users/" + viewer_id
    policy_route = profile_route + "/Policy"
    add("original-P-policy", "admin", "GET", profile_route, profile(original))
    add("grant-policy", "admin", "POST", policy_route, body=target, status=204)
    add("granted-P-policy", "admin", "GET", profile_route, profile(target))
    login("P", 48)
    add("own-P-profile", "P", "GET", profile_route, profile(target))
    own_views = {row["libraryId"]: {"Id": row["libraryId"], "Name": before["libraries"][row["libraryId"]]["Name"],
                "Type": "CollectionFolder", "CollectionType": "tvshows"} for row in grants}
    add("own-P-views", "P", "GET", profile_route + "/Views", page(own_views))
    selectable = []
    for library in sorted(before["libraries"]):
        matched = next((row for row in grants if row["libraryId"] == library), None)
        selectable.append({"Id": library, "Name": before["libraries"][library]["Name"],
            "Guid": matched["guid"] if matched else uuid.uuid5(uuid.NAMESPACE_URL, "synthetic-grant-" + library).hex,
            "IsUserAccessConfigurable": True,
            "SubFolders": [{"Id": "synthetic-grant-root-" + library, "Name": "Synthetic Grant Root " + library,
                "Path": before["libraries"][library]["Locations"][0], "IsUserAccessConfigurable": True}]})
    add("own-P-selectable-folders", "P", "GET", "/emby/Library/SelectableMediaFolders", selectable)
    for symbol, grant in zip(("LA", "LB"), grants):
        add("own-P-catalog-" + symbol, "P", "GET", query(viewer_id, parent=grant["libraryId"]),
            page(before["catalog_by_library"][grant["libraryId"]]))
    for symbol, body in episode_bodies.items():
        add("own-P-detail-" + symbol, "P", "GET", profile_route + "/Items/" + body["Id"], body)
    assert len(events) == 59
    add("restore-policy", "admin", "POST", policy_route, body=original, status=204)
    add("restored-P-policy", "admin", "GET", profile_route, profile(original))
    add("restored-own-P-views", "P", "GET", profile_route + "/Views", {"Items": [], "TotalRecordCount": 0})
    snapshot("after", after)
    assert len(events) == 105
    for actor in ("P", "admin"):
        add("logout-" + actor, actor, "POST", "/emby/Sessions/Logout", status=204)
        add("rejection-" + actor, actor, "GET", "/emby/Sessions", {"Error": "Synthetic closed session"}, status=401)
    assert len(events) == 109

    requests, records, charged = [], {}, 0
    for ordinal, event in enumerate(events, 1):
        actor, label = event["actor"], event["label"]
        is_login = label == "login-" + actor
        headers = [["Accept", "application/json"], ["Authorization",
            'Emby Client="Goby Folder Grant Verifier", Device="Linux Fixture Recorder", DeviceId="' +
            actors[actor]["deviceId"] + '", Version="1.0"']]
        if not is_login:
            headers.append(["X-Emby-Token", tokens[actor]])
        payload = None
        if event["body"] is not None:
            payload = urlencode([("Username", event["body"]["Username"]), ("Pw", event["body"]["Pw"]) ]).encode() if event["form"] else encode(event["body"]).rstrip(b"\n")
            headers.append(["Content-Type", "application/x-www-form-urlencoded; charset=utf-8" if event["form"] else "application/json"])
        request = {"method": event["method"], "route": event["route"], "headers": headers, "body": event["body"]}
        intent = {"ordinal": ordinal, "actor": actor, "label": label, "phase": "normal" if ordinal <= 59 else "cleanup",
            "createdAt": created_at(ordinal), "request": deepcopy(request),
            "payloadBase64": None if payload is None else base64.b64encode(payload).decode(),
            "tokenSha256": None if is_login else checksum(tokens[actor].encode())}
        raw = b"" if event["status"] == 204 else b"Access token is invalid or expired." if event["status"] == 401 else encode(event["responseBody"])
        charged += len(raw)
        response = {"ordinal": ordinal, "actor": actor, "label": label, "request": deepcopy(request),
            "status": event["status"], "headers": [["Content-Type", "application/json; charset=utf-8"], ["Content-Length", str(len(raw))]],
            "rawBase64": base64.b64encode(raw).decode(), "observedRawBytes": len(raw), "retainedRawTruncated": False,
            "completeHttp": True, "completedAt": completed_at(ordinal), "failure": None}
        stem = "%04d-%s" % (ordinal, label)
        references = {"intent": save(private / (stem + "-intent.json"), intent),
                      "response": save(private / (stem + "-response.json"), response)}
        requests.append({"ordinal": ordinal, "label": label, **deepcopy(references)})
        records[label] = {"intent": intent, "response": response, "descriptors": references}

    closures, normalized = [], []
    for actor in ("P", "admin"):
        row = {"actor": actor, "userId": actors[actor]["userId"], "deviceId": actors[actor]["deviceId"],
            "tokenSha256": checksum(tokens[actor].encode()), "from": records["login-" + actor]["intent"]["createdAt"],
            "through": records["rejection-" + actor]["response"]["completedAt"], "exactTokenClosed": True}
        for name in ("login", "logout", "rejection"):
            row[name] = deepcopy(records[name + "-" + actor]["descriptors"])
        closures.append(row)
        normalized_row = {"userId": row["userId"], "reportedDeviceId": row["deviceId"], "tokenSha256": row["tokenSha256"],
            "from": row["from"], "through": row["through"], "login": deepcopy(row["login"]["response"])}
        for name, status in (("logout", 204), ("rejection", 401)):
            normalized_row[name] = {"record": deepcopy(row[name]["response"]), "intent": deepcopy(row[name]["intent"]),
                "status": status, "tokenSha256": row["tokenSha256"],
                "completedAt": records[name + "-" + actor]["response"]["completedAt"]}
        normalized.append(normalized_row)

    prior = {"schemaVersion": 1, "kind": "nextup-folder-grant-verification-input", "runId": run_id, "source": deepcopy(source_descriptor),
        "process": deepcopy(manifest["process"]), "endpoint": deepcopy(manifest["endpoint"]), "lock": deepcopy(manifest["lock"]),
        "preservation": deepcopy(preservation), "actors": deepcopy(actors), "guidFolders": deepcopy(grants), "outputRoot": str(root)}
    terminal = {"runId": run_id, "status": "awaiting_independent_attestation", "failure": None, "pendingPresent": False,
        "uncertain": False, "grantSemanticsProven": True, "originalPolicyRestored": True, "publicPreserved": True,
        "allKnownTokensClosed": True, "requestCount": 109, "normalRequestCount": 59, "cleanupRequestCount": 50}
    index_document = {"schemaVersion": 1, "kind": "nextup-folder-grant-wire-index", "runId": run_id,
                      "root": str(root), "requests": requests}
    closure_document = {"schemaVersion": 1, "kind": "nextup-folder-grant-authentication-closures", "runId": run_id, "closures": closures}
    preservation_report = {"preserved": True,
        "allowedAuthenticationChanges": [{"kind": "owned-device-time", "deviceId": "synthetic-grant-registry-P"}],
        "beforeTokenSha256": before["credential_context"]["token_sha256"],
        "afterTokenSha256": after["credential_context"]["token_sha256"]}
    documents = {"input": prior, "producerTerminal": terminal, "wireIndex": index_document,
        "closedAuthentication": closure_document, "beforePublic": before, "afterPublic": after,
        "originalPolicy": original, "targetPolicy": target, "publicPreservation": preservation_report}
    names = {"input": "input.json", "producerTerminal": "terminal.json", "wireIndex": "wire-index.json",
        "closedAuthentication": "closed-authentication.json", "beforePublic": "before-public.json", "afterPublic": "after-public.json",
        "originalPolicy": "original-policy.json", "targetPolicy": "target-policy.json", "publicPreservation": "public-preservation.json"}
    descriptors = {name: save(private / names[name], value) for name, value in documents.items()}
    inventory_rows = {}
    for path in [root, *sorted(root.rglob("*"))]:
        info = path.lstat()
        row = {"device": info.st_dev, "inode": info.st_ino, "uid": info.st_uid, "gid": info.st_gid, "mode": info.st_mode,
            "links": info.st_nlink, "bytes": info.st_size, "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns}
        if path.is_file():
            row["sha256"] = fixture.descriptor(path)["sha256"]
        inventory_rows[str(path.relative_to(root))] = row
    documents["scopeInventory"] = {str(root): inventory_rows}
    descriptors["scopeInventory"] = save(root / "completed-scope-files.json", documents["scopeInventory"])
    unit_name = "synthetic-folder-grant-worker.service"
    unit = {"name": unit_name, "cgroupPath": "/sys/fs/cgroup/system.slice/" + unit_name,
        "properties": {"Id": unit_name, "InvocationID": "3" * 32, "ActiveState": "active", "SubState": "exited",
            "MainPID": "0", "Result": "success", "ControlGroup": "", "ExecMainStatus": "0", "RemainAfterExit": "yes", "Type": "oneshot"}}
    command = ["/usr/bin/python3", "-I", "-B", source_descriptor["path"], descriptors["input"]["path"], descriptors["input"]["sha256"]]
    unit["properties"]["ExecStart"] = "{ path=/usr/bin/python3 ; argv[]=" + shlex.join(command) + " ; ignore_errors=no ; }"
    independent = {"schemaVersion": 1, "kind": "nextup-folder-grant-independent-terminal",
        "status": "folder_guid_grants_independently_verified_and_policy_restored", "runId": run_id,
        "capturedAt": snapshot_at(110), "referenceCounts": {"users": 8, "libraries": 10, "devices": 93, "detailWitnesses": 12},
        "requestCount": 109, "normalRequestCount": 59, "cleanupRequestCount": 50,
        "allKnownTokensClosed": True, "originalPolicyRestored": True, "fullPublicRoundtripPreserved": True,
        "allPrivateBytesMatched": True, "allExportBytesMatched": True, "recursiveCgroupEmpty": True, "grantSemanticsProven": True,
        "originalImplementationBytesRead": False, "referenceDatabaseRead": False, "businessHttpByAudit": 0,
        "replayPerformedHttp": False, "unit": unit, "source": deepcopy(source_descriptor), **deepcopy(descriptors)}
    independent_descriptor = save(root / "independent-terminal.json", independent)
    release["schemaVersion"] = 2
    release["grantVerification"] = independent_descriptor
    release["closedAuthentication"].extend(deepcopy(normalized))
    release_time = datetime.fromisoformat(release["releasedAt"].replace("Z", "+00:00"))
    grant_time = datetime.fromisoformat(independent["capturedAt"])
    assert grant_time <= release_time
    fixture.baseline = deepcopy(after)
    fixture.release = release
    manifest["inputs"]["publicBaseline"] = deepcopy(descriptors["afterPublic"])
    manifest["inputs"]["release"] = save(Path(manifest["scope"]["inputRoot"]) / "synthetic-grant-release.json", release)
    fixture.grant_verification = {"root": root, "private": private, "documents": documents, "descriptors": descriptors,
        "independent": independent, "independentDescriptor": independent_descriptor, "records": records, "actors": actors,
        "normalizedClosures": normalized, "chargedResponseBytes": charged}
    return fixture.grant_verification


class ActualAuthorityGuards(unittest.TestCase):
    """Exercise real authority against only self-owned synthetic private files."""

    def setUp(self):
        self.fixture = PreparationFixture(self)
        self.P = self.fixture.module
        self.manifest = self.fixture.manifest
        self.units_checked = []
        self.process_checked = []
        self.network_attempts = []
        self.loaded_modules = set(sys.modules)
        self.addCleanup(self.clean_loaded_modules)
        def forbidden(*args, **kwargs):
            self.network_attempts.append("forbidden-real-io")
            raise AssertionError("Actual process, network, and subprocess access is forbidden in authority guards.")
        for target, name in ((socket, "socket"), (socket, "create_connection"),
                             (self.P.subprocess, "run"), (self.fixture.support.HTTPTransport, "send")):
            blocker = patch.object(target, name, side_effect=forbidden)
            blocker.start()
            self.addCleanup(blocker.stop)
        original_load = self.P.load_owned
        def load_guarded(source, label):
            module = original_load(source, label)
            if hasattr(module, "process_identity"):
                module.process_identity = forbidden
            if hasattr(module, "HTTPTransport"):
                module.HTTPTransport.send = forbidden
            return module
        loader_blocker = patch.object(self.P, "load_owned", side_effect=load_guarded)
        loader_blocker.start()
        self.addCleanup(loader_blocker.stop)
        sealed_root = Path(self.manifest["sealedRoots"][0])
        closed = {"userId": "old-user-1", "reportedDeviceId": "synthetic-prior-device",
                  "tokenSha256": "c" * 64, "from": STAMP, "through": "2026-09-13T01:00:02Z"}
        for name, status in (("logout", 204), ("rejection", 401)):
            path = sealed_root / (name + ".json")
            self.fixture.write(path, encoded({"complete": True, "status": status, "token_sha256": "c" * 64,
                                            "completed_at": "2026-09-13T01:00:01Z"}))
            intent = sealed_root / (name + "-intent.json")
            self.fixture.write(intent, encoded({"channel": "controller_api", "method": "POST" if name == "logout" else "GET",
                "path": "/emby/Sessions/Logout" if name == "logout" else "/emby/Sessions"}))
            closed[name] = {"record": self.fixture.descriptor(path), "intent": self.fixture.descriptor(intent),
                            "status": status, "tokenSha256": "c" * 64,
                            "completedAt": "2026-09-13T01:00:01Z"}
        units = []
        for index, role in enumerate(("worker", "controller"), 1):
            group = "/system.slice/synthetic-" + role + ".service"
            units.append({"name": "synthetic-" + role + ".service", "invocationId": str(index) * 32,
                "properties": {"ActiveState": "inactive", "SubState": "dead", "MainPID": "0", "Result": "success",
                               "ControlGroup": group},
                "cgroupPath": "/sys/fs/cgroup" + group})
        inventory_path = sealed_root / "inventory.json"
        self.fixture.write(inventory_path, encoded({"syntheticPrivateScope": {"closed": True, "caseOwned": True}}))
        inventory_descriptor = self.fixture.descriptor(inventory_path)
        terminal_path = sealed_root / "terminal.json"
        terminal = {"status": "protocol_observation_complete_independently_confirmed", "captured_at": "2026-09-13T01:00:02Z",
            "recursive_cgroups_empty": True,
            "full_target_restored_except_etag": True, "administrator_logout204_same_token401_verified": True,
            "viewer_logout204_same_token401_verified": True, "media_unchanged": True, "reference_database_read": False,
            "original_implementation_bytes_read": False, "scope_inventory": inventory_descriptor,
            "systemd": {row["name"]: {"InvocationID": row["invocationId"], **row["properties"]} for row in units}}
        self.fixture.write(terminal_path, encoded(terminal))
        sealed = [{"kind": "terminal", "record": self.fixture.descriptor(terminal_path)},
                  {"kind": "inventory", "record": inventory_descriptor}]
        self.release = {"schemaVersion": 1, "kind": "nextup-global-preparation-release", "runId": self.manifest["runId"],
            "process": deepcopy(self.manifest["process"]), "lock": deepcopy(self.manifest["lock"]),
            "sealedRoots": deepcopy(self.manifest["sealedRoots"]), "releasedAt": "2026-09-13T01:00:03Z",
            "sealed": sealed, "units": units, "closedAuthentication": [closed]}
        self.replace_input("release", self.release)
        self.fixture.baseline["devices"]["synthetic-prior-registry"] = {"Id": "synthetic-prior-registry",
            "ReportedDeviceId": "synthetic-prior-device", "LastUserId": "old-user-1"}
        self.replace_input("publicBaseline", self.fixture.baseline)
        add_grant_verification_fixture(self.fixture, self.release)
        self.refresh_media_approval()

    def refresh_media_approval(self):
        self.replace_input("mediaApproval", {"schemaVersion": 1, "kind": "nextup-global-preparation-media-approval",
            "source": deepcopy(self.manifest["media"]["source"]),
            "ownedRoot": self.manifest["media"]["ownedRoot"], "approvedReceipt": deepcopy(self.manifest["media"]["approvedReceipt"]),
            "approvedReceiptSha256": self.manifest["media"]["approvedReceiptSha256"], "durationSeconds": 600,
            "frameRate": 30, "originalImplementationBytesRead": False})

    def clean_loaded_modules(self):
        for name in set(sys.modules) - self.loaded_modules:
            if name.startswith(("preparation_transport_", "preparation_matrix_")):
                sys.modules.pop(name, None)

    def replace_input(self, name, value):
        path = Path(self.manifest["inputs"][name]["path"])
        path.write_bytes(encoded(value))
        path.chmod(0o600)
        self.manifest["inputs"][name] = self.fixture.descriptor(path)

    def authority(self):
        authority = self.P.Authority(self.manifest)
        self.addCleanup(authority.close)
        def fake_units():
            self.units_checked.append(deepcopy(authority.release["units"]))
        def fake_process(expected):
            self.process_checked.append(deepcopy(expected))
            return deepcopy(expected)
        authority._units = fake_units
        patcher = patch.object(authority.support, "process_identity", side_effect=fake_process)
        patcher.start()
        self.addCleanup(patcher.stop)
        network_blocker = patch.object(authority.support.HTTPTransport, "send",
            side_effect=AssertionError("Real HTTP is forbidden in actual-authority guards."))
        network_blocker.start()
        self.addCleanup(network_blocker.stop)
        return authority

    def reject_constructor(self):
        with self.assertRaises((self.P.PreparationError, OSError, ValueError)):
            self.authority()
        self.assertFalse(self.fixture.output.exists())
        self.assertFalse(Path(self.manifest["scope"]["matrixEvidenceRoot"]).exists())
        self.assertEqual(self.network_attempts, [])

    def test_actual_authority_accepts_complete_synthetic_inputs_and_stages_independent_media(self):
        authority = self.authority()
        authority.acquire()
        authority.check()
        self.assertFalse(self.fixture.output.exists())
        journal = authority.support.Journal(self.fixture.output, uid=0)
        self.addCleanup(journal.close)
        media = authority.stage_media(journal)
        self.assertEqual(set(media), {"LA", "LB"})
        files = [row for library in media.values() for row in library["files"].values()]
        self.assertEqual(len(files), 6)
        self.assertEqual(len({(Path(row["path"]).stat().st_dev, Path(row["path"]).stat().st_ino) for row in files}), 6)
        self.assertTrue(all(Path(row["path"]).read_bytes() == self.fixture.media_bytes for row in files))
        self.assertTrue(all(row["sha256"] == digest(self.fixture.media_bytes) for row in files))
        self.assertEqual(len(self.units_checked), 1)
        self.assertTrue(self.process_checked)
        self.assertEqual(self.network_attempts, [])
        self.assertFalse(Path(self.manifest["scope"]["matrixEvidenceRoot"]).exists())

    def test_raw_release_accepts_realistic_non_json_401_and_fixed_login_form_order(self):
        for actor in ("P", "admin"):
            record = self.fixture.grant_verification["records"]["rejection-" + actor]["response"]
            raw = base64.b64decode(record["rawBase64"], validate=True)
            self.assertEqual(len(raw), 35)
            with self.assertRaises(ValueError): json.loads(raw)
            intent = self.fixture.grant_verification["records"]["login-" + actor]["intent"]
            self.assertTrue(base64.b64decode(intent["payloadBase64"]).startswith(b"Username="))
        authority = self.authority()
        self.assertEqual(authority.grant_unit["invocationId"], "3" * 32)
        self.assertEqual(self.network_attempts, [])

    def reject_raw_release(self, mutate):
        checker = object.__new__(self.P.Authority)
        checker.manifest, checker.release, checker.baseline = self.manifest, self.release, self.fixture.baseline
        checker.support, checker.planner = self.fixture.support, self.fixture.planner
        def evidence(row, *, sealed=False):
            value = self.P.strict_json(self.P.read_owned(row["path"], private=True))
            mutate(row, value)
            return value
        checker._evidence = evidence
        with self.assertRaises((self.P.PreparationError, ValueError, KeyError, TypeError)):
            checker._grant_verification()
        self.assertFalse(self.fixture.output.exists())
        self.assertEqual(self.network_attempts, [])

    def test_raw_release_rejects_reordered_login_form_bytes(self):
        def mutate(row, value):
            if row["path"].endswith("0001-login-admin-intent.json"):
                body = value["request"]["body"]
                value["payloadBase64"] = base64.b64encode(urlencode([("Pw", body["Pw"]), ("Username", body["Username"])]).encode()).decode()
        self.reject_raw_release(mutate)

    def test_raw_release_rejects_incomplete_actual_response(self):
        def mutate(row, value):
            if row["path"].endswith("0107-rejection-P-response.json"): value["completeHttp"] = False
        self.reject_raw_release(mutate)

    def test_raw_release_rejects_missing_ledger_attempt(self):
        def mutate(row, value):
            if value.get("kind") == "nextup-folder-grant-wire-index": value["requests"].pop()
        self.reject_raw_release(mutate)

    def test_raw_release_rejects_other_actor_token_in_logout_context(self):
        def mutate(row, value):
            if "0106-logout-P-" in row["path"]:
                for header in value["request"]["headers"]:
                    if header[0] == "X-Emby-Token": header[1] = "synthetic-another-actor-token"
        self.reject_raw_release(mutate)

    def test_raw_release_rejects_unindexed_closure_response(self):
        def mutate(row, value):
            if value.get("kind") == "nextup-folder-grant-authentication-closures":
                value["closures"][0]["logout"] = deepcopy(value["closures"][1]["logout"])
        self.reject_raw_release(mutate)

    def test_missing_private_input_is_rejected_without_output_or_http(self):
        Path(self.manifest["inputs"]["publicBaseline"]["path"]).unlink()
        self.reject_constructor()

    def test_private_input_digest_mismatch_is_rejected_without_output_or_http(self):
        self.manifest["inputs"]["release"]["sha256"] = "0" * 64
        self.reject_constructor()

    def test_private_input_permissions_cannot_be_world_readable(self):
        Path(self.manifest["inputs"]["credentials"]["path"]).chmod(0o644)
        self.reject_constructor()

    def test_private_input_owner_must_match_root_authority(self):
        os.chown(self.manifest["inputs"]["credentials"]["path"], 12345, 12345)
        self.reject_constructor()

    def test_full_public_baseline_missing_marker_is_rejected_before_output(self):
        baseline = deepcopy(self.fixture.baseline)
        baseline.pop("marker")
        self.replace_input("publicBaseline", baseline)
        self.reject_constructor()

    def test_actual_approved_media_manifest_must_exist(self):
        Path(self.manifest["media"]["approvedReceipt"]["path"]).unlink()
        self.reject_constructor()

    def test_actual_approved_media_manifest_must_bind_the_exact_source_hash(self):
        path = Path(self.manifest["media"]["approvedReceipt"]["path"])
        approved = json.loads(path.read_text())
        relative = str(Path(self.manifest["media"]["source"]["path"]).relative_to(self.manifest["media"]["ownedRoot"]))
        approved["files"][relative] = "0" * 64
        path.write_bytes(encoded(approved))
        self.manifest["media"]["approvedReceipt"] = self.fixture.descriptor(path)
        self.manifest["media"]["approvedReceiptSha256"] = self.manifest["media"]["approvedReceipt"]["sha256"]
        self.refresh_media_approval()
        self.reject_constructor()

    def test_completed_unit_proof_cannot_select_another_cgroup(self):
        self.release["units"][0]["cgroupPath"] = self.release["units"][1]["cgroupPath"]
        self.replace_input("release", self.release)
        self.reject_constructor()

    def test_actual_closed_response_status_cannot_be_hidden_by_wrapper_claim(self):
        proof = self.release["closedAuthentication"][0]["rejection"]
        path = Path(proof["record"]["path"])
        response = json.loads(path.read_text())
        response["status"] = 200
        path.write_bytes(encoded(response))
        proof["record"] = self.fixture.descriptor(path)
        self.replace_input("release", self.release)
        self.reject_constructor()

    def test_release_requires_both_sealed_records_and_completed_units(self):
        original = deepcopy(self.release)
        for field in ("sealed", "units"):
            with self.subTest(field=field):
                changed = deepcopy(original)
                changed[field] = changed[field][:1]
                self.replace_input("release", changed)
                self.reject_constructor()

    def test_changed_sealed_response_digest_is_rejected_before_acquiring_lock(self):
        self.release["sealed"][0]["record"]["sha256"] = "0" * 64
        self.replace_input("release", self.release)
        self.reject_constructor()

    def test_prior_closed_authentication_requires_matching_token_and_bounded_complete_proofs(self):
        original = deepcopy(self.release)
        mutations = (
            lambda row: row.pop("rejection"),
            lambda row: row["rejection"].update(tokenSha256="d" * 64),
            lambda row: row["logout"].update(completedAt="2026-09-13T02:00:00Z"),
            lambda row: row["rejection"].update(status=200),
        )
        for index, mutate in enumerate(mutations):
            with self.subTest(index=index):
                changed = deepcopy(original)
                mutate(changed["closedAuthentication"][0])
                self.replace_input("release", changed)
                self.reject_constructor()

    def test_replaced_existing_lock_inode_cannot_be_acquired(self):
        authority = self.authority()
        path = Path(self.manifest["lock"]["path"])
        path.rename(path.with_name("synthetic-retained-old-lock"))
        self.fixture.write(path, b"A different synthetic coordination lock.\n")
        with self.assertRaises(self.P.PreparationError):
            authority.acquire()
        self.assertFalse(self.fixture.output.exists())
        self.assertEqual(self.process_checked, [])

    def test_existing_lock_contention_cannot_start_a_second_preparation(self):
        first = self.authority()
        first.acquire()
        second = self.authority()
        with self.assertRaises((BlockingIOError, OSError)):
            second.acquire()
        self.assertFalse(self.fixture.output.exists())
        self.assertEqual(len(self.units_checked), 1)

    def test_frozen_owned_source_change_is_rejected_at_checkpoint(self):
        authority = self.authority()
        authority.acquire()
        source = Path(self.manifest["sources"]["matrix"]["path"])
        source.write_bytes(source.read_bytes() + b"\n# Synthetic source drift.\n")
        with self.assertRaises(self.P.PreparationError):
            authority.check()
        self.assertFalse(self.fixture.output.exists())

    def test_synthetic_media_identity_drift_is_rejected_at_checkpoint(self):
        authority = self.authority()
        authority.acquire()
        source = Path(self.manifest["media"]["source"]["path"])
        source.write_bytes(b"Different synthetic media bytes.\n")
        with self.assertRaises(self.P.PreparationError):
            authority.check()
        self.assertFalse(self.fixture.output.exists())

    def test_executable_source_descriptor_is_rejected_before_any_attempt_to_read_it(self):
        forbidden = Path(self.manifest["scope"]["sourceRoot"]) / "synthetic-original-implementation.exe"
        self.fixture.write(forbidden, b"Synthetic opaque executable bytes; must not be opened by authority.\n")
        self.manifest["sources"]["matrix"] = self.fixture.descriptor(forbidden)
        attempted = []
        original = self.P.read_owned
        def guarded(path, **kwargs):
            attempted.append(str(path))
            if Path(path) == forbidden:
                raise AssertionError("The source gate attempted to read a prohibited executable descriptor.")
            return original(path, **kwargs)
        with patch.object(self.P, "read_owned", side_effect=guarded):
            with self.assertRaises(self.P.PreparationError):
                self.authority()
        self.assertNotIn(str(forbidden), attempted)
        self.assertFalse(self.fixture.output.exists())

    def test_forbidden_original_input_is_rejected_before_reading_a_json_descriptor(self):
        forbidden = self.manifest["inputs"]["release"]["path"]
        self.manifest["forbiddenOriginalRoots"].append(forbidden)
        attempted = []
        original = self.P.read_owned
        def guarded(path, **kwargs):
            attempted.append(str(path))
            if str(path) == forbidden:
                raise AssertionError("The input gate attempted to read forbidden original bytes.")
            return original(path, **kwargs)
        with patch.object(self.P, "read_owned", side_effect=guarded):
            with self.assertRaises(self.P.PreparationError):
                self.authority()
        self.assertNotIn(forbidden, attempted)
        self.assertFalse(self.fixture.output.exists())


def preservation_order_probe(preparation, changed=("LastLoginDate", "LastActivityDate")):
    """Exercise only the real preservation comparator with synthetic date facts."""
    runner = object.__new__(preparation.PreparationRunner)
    runner.user_ids = {"admin": "synthetic-order-admin"}
    runner.manifest = {"actors": {}}
    runner.tokens = {}
    runner.authority = SimpleNamespace(release={"closedAuthentication": []})
    before = {"captured_at": "2026-09-13T01:00:00Z", "server": {}, "configuration": {}, "details": {},
        "catalog_by_library": {}, "libraries": {}, "preferences": {}, "items_by_user": {}, "devices": {},
        "roster": {"synthetic-order-admin": {"Id": "synthetic-order-admin", "LastLoginDate": "2026-09-13T00:59:00Z",
            "LastActivityDate": "2026-09-13T00:59:00Z"}}, "credential_context": {"token_sha256": "a" * 64}}
    after = deepcopy(before)
    after["captured_at"] = "2026-09-13T01:01:00Z"
    for field in changed:
        after["roster"]["synthetic-order-admin"][field] = "2026-09-13T01:00:30Z"
    with patch.object(preparation, "Authority", side_effect=AssertionError("No authority access is allowed in the ordering probe.")), \
         patch.object(preparation, "read_owned", side_effect=AssertionError("No file reads are allowed in the ordering comparator.")), \
         patch.object(preparation.subprocess, "run", side_effect=AssertionError("No subprocess is allowed in the ordering comparator.")), \
         patch.object(socket, "socket", side_effect=AssertionError("No socket is allowed in the ordering comparator.")), \
         patch.object(socket, "create_connection", side_effect=AssertionError("No network is allowed in the ordering comparator.")):
        report = runner._preserve(before, after)
    return {"fields": [row["field"] for row in report["allowedAuthenticationChanges"]],
        "reportSha256": digest((preparation.canonical(report) + "\n").encode())}


def preservation_order_expected(fields):
    report = {"preserved": True, "allowedAuthenticationChanges": [
        {"kind": "owned-authentication-time", "userId": "synthetic-order-admin", "field": field} for field in fields],
        "beforeTokenSha256": "a" * 64, "afterTokenSha256": "a" * 64}
    return digest(encoded(report))


class PreservationOrderingGuards(unittest.TestCase):
    """Check canonical artifact bytes across independently randomized interpreters."""

    def test_preservation_report_has_fixed_order_when_both_admin_dates_change(self):
        preparation = load_source(PREPARATION_SOURCE, "ordered_preservation_subject")
        self.addCleanup(sys.modules.pop, preparation.__name__, None)
        observed = preservation_order_probe(preparation)
        expected = ["LastActivityDate", "LastLoginDate"]
        self.assertEqual(observed["fields"], expected)
        self.assertEqual(observed["reportSha256"], preservation_order_expected(expected))

    def test_unchanged_and_single_date_cases_keep_their_exact_report_meaning(self):
        preparation = load_source(PREPARATION_SOURCE, "single_preservation_subject")
        self.addCleanup(sys.modules.pop, preparation.__name__, None)
        for fields in ([], ["LastActivityDate"], ["LastLoginDate"]):
            with self.subTest(fields=fields):
                observed = preservation_order_probe(preparation, tuple(fields))
                self.assertEqual(observed["fields"], fields)
                self.assertEqual(observed["reportSha256"], preservation_order_expected(fields))

    def test_isolated_workers_witness_both_native_set_orders_but_identical_report_bytes(self):
        global PRESERVATION_ORDER_OBSERVATION
        observed, native_orders = [], set()
        with tempfile.TemporaryDirectory(prefix="nextup-preservation-order-workers-") as temporary:
            root = Path(temporary)
            for index in range(32):
                unused_report = root / ("unused-worker-%02d.json" % index)
                command = ["/usr/bin/python3", "-I", "-B", str(Path(__file__).absolute()),
                    "--preparation-source", str(PREPARATION_SOURCE), "--transport-source", str(TRANSPORT_SOURCE),
                    "--matrix-source", str(MATRIX_SOURCE), "--report-path", str(unused_report), "--preservation-order-worker"]
                environment = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "PYTHONHASHSEED": "0"}
                for name in ("SSH_CONNECTION", "SSH_TTY"):
                    if os.environ.get(name):
                        environment[name] = os.environ[name]
                result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10,
                                        check=False, env=environment)
                self.assertEqual(result.returncode, 0, "The isolated synthetic ordering worker did not complete.")
                self.assertEqual(result.stderr, b"", "The ordering worker emitted unexpected diagnostics.")
                self.assertLessEqual(len(result.stdout), 4096)
                value = json.loads(result.stdout)
                self.assertEqual(set(value), {"kind", "nativeSetOrder", "fields", "reportSha256", "isolated", "noBytecode", "ignoreEnvironment", "hashRandomization"})
                self.assertEqual(value["kind"], "synthetic-preservation-order-observation")
                for flag in ("isolated", "noBytecode", "ignoreEnvironment", "hashRandomization"):
                    self.assertIs(value[flag], True, "The worker did not establish isolated randomized interpreter startup.")
                self.assertFalse(unused_report.exists(), "The synthetic worker must not create a suite report.")
                self.assertEqual(set(value["nativeSetOrder"]), {"LastActivityDate", "LastLoginDate"})
                native_orders.add(tuple(value["nativeSetOrder"]))
                observed.append(value)
                if len(native_orders) == 2 and len(observed) >= 4:
                    break
        PRESERVATION_ORDER_OBSERVATION = {"maximumWorkers": 32, "workersRun": len(observed),
            "nativeOrdersObserved": [list(order) for order in sorted(native_orders)],
            "hashSeedEnvironmentIgnoredByIsolatedWorkers": True,
            "precondition": "both-native-orders-observed" if len(native_orders) == 2 else "inconclusive-native-order-coverage",
            "expectedReportSha256": preservation_order_expected(["LastActivityDate", "LastLoginDate"]),
            "observedReportSha256": sorted({value["reportSha256"] for value in observed})}
        self.assertEqual(len(native_orders), 2,
            "The bounded isolated workers did not witness both native set orders; this determinism experiment is inconclusive.")
        for value in observed:
            self.assertEqual(value["fields"], ["LastActivityDate", "LastLoginDate"])
            self.assertEqual(value["reportSha256"], PRESERVATION_ORDER_OBSERVATION["expectedReportSha256"])


def verify_actual_grant_release(independent_path, independent_sha):
    """Exercise only the new raw-release adapter against explicitly pinned history."""
    preparation = load_source(PREPARATION_SOURCE, "actual_grant_release_preparation")
    support = load_source(TRANSPORT_SOURCE, "actual_grant_release_transport")
    planner = load_source(MATRIX_SOURCE, "actual_grant_release_matrix")
    def read(row):
        preparation.descriptor(row)
        raw = preparation.read_owned(row["path"], private=True)
        preparation.require(digest(raw) == row["sha256"], "An explicitly pinned completed evidence file changed.")
        return preparation.strict_json(raw)
    independent_descriptor = {"path": str(independent_path), "sha256": independent_sha}
    independent = read(independent_descriptor)
    prior = read(independent["input"])
    original_input = read(prior["preparationInput"])
    baseline = read(independent["afterPublic"])
    closures = read(independent["closedAuthentication"])
    normalized = []
    for row in closures["closures"]:
        item = {"userId": row["userId"], "reportedDeviceId": row["deviceId"], "tokenSha256": row["tokenSha256"],
            "from": row["from"], "through": row["through"], "login": row["login"]["response"]}
        for name, status in (("logout", 204), ("rejection", 401)):
            item[name] = {"record": row[name]["response"], "intent": row[name]["intent"], "status": status,
                "tokenSha256": row["tokenSha256"], "completedAt": read(row[name]["response"])["completedAt"]}
        normalized.append(item)
    checker = object.__new__(preparation.Authority)
    checker.manifest = {"inputs": {"publicBaseline": independent["afterPublic"]},
        "server": {"id": baseline["server"]["Id"], "version": baseline["server"]["Version"]},
        "process": prior["process"], "endpoint": prior["endpoint"], "lock": prior["lock"],
        "preservation": prior["preservation"], "sealedRoots": prior["readonlyRoots"] + [str(Path(prior["outputRoot"])), str(independent_path.parent)],
        "forbiddenOriginalRoots": original_input["forbiddenOriginalRoots"], "scope": {"inputRoot": str(independent_path.parent)}}
    checker.release = {"grantVerification": independent_descriptor, "releasedAt": independent["capturedAt"], "units": [],
        "closedAuthentication": normalized}
    checker.baseline, checker.support, checker.planner = baseline, support, planner
    def forbidden(*args, **kwargs):
        raise AssertionError("Completed-evidence audit cannot perform HTTP or process probes.")
    with patch.object(socket, "socket", side_effect=forbidden), patch.object(socket, "create_connection", side_effect=forbidden), \
         patch.object(support.HTTPTransport, "send", side_effect=forbidden), patch.object(support, "process_identity", side_effect=forbidden), \
         patch.object(preparation.subprocess, "run", side_effect=forbidden):
        checker._grant_verification()
    return {"suite": "completed-grant-release-raw-reconstruction", "passed": True, "independentSha256": independent_sha,
        "rawRequestsVerified": 109, "rawResponsesVerified": 109, "snapshotGetsReconstructed": 86, "closedTokensVerified": 2,
        "actualBusinessHttpRequests": 0, "actualProcessProbes": 0, "livePreparationAcceptanceClaim": False}


def main():
    global PREPARATION_SOURCE, TRANSPORT_SOURCE, MATRIX_SOURCE
    if (sys.platform != "linux" or not (os.environ.get("SSH_CONNECTION") or os.environ.get("SSH_TTY")) or
            os.geteuid() != 0 or not sys.flags.isolated or not sys.flags.dont_write_bytecode):
        print(json.dumps({"suite": "prepare-nextup-global-reference-guards", "status": "blocked",
            "reason": "An authorized root Linux SSH session with Python -I -B is required; local verification is forbidden."}))
        return 2
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preparation-source", required=True, type=Path)
    parser.add_argument("--transport-source", required=True, type=Path)
    parser.add_argument("--matrix-source", required=True, type=Path)
    parser.add_argument("--report-path", required=True, type=Path)
    parser.add_argument("--preservation-order-worker", action="store_true",
                        help="Run one bounded synthetic comparator observation; never perform preparation or write a report.")
    parser.add_argument("--verify-grant-independent", type=Path,
                        help="Read only the explicitly pinned completed Guid evidence through the new raw-release adapter.")
    parser.add_argument("--grant-independent-sha256")
    arguments = parser.parse_args()
    PREPARATION_SOURCE = arguments.preparation_source.resolve(strict=True)
    TRANSPORT_SOURCE = arguments.transport_source.resolve(strict=True)
    MATRIX_SOURCE = arguments.matrix_source.resolve(strict=True)
    if arguments.preservation_order_worker:
        preparation = load_source(PREPARATION_SOURCE, "isolated_preservation_order_subject")
        observed = preservation_order_probe(preparation)
        print(json.dumps({"kind": "synthetic-preservation-order-observation", "nativeSetOrder": list({"LastLoginDate", "LastActivityDate"}),
            **observed, "isolated": bool(sys.flags.isolated), "noBytecode": bool(sys.flags.dont_write_bytecode),
            "ignoreEnvironment": bool(sys.flags.ignore_environment), "hashRandomization": bool(sys.flags.hash_randomization)}, sort_keys=True))
        return 0
    report_path = arguments.report_path.absolute()
    source_hashes = {"preparation": digest(PREPARATION_SOURCE.read_bytes()),
                     "transport": digest(TRANSPORT_SOURCE.read_bytes()),
                     "matrix": digest(MATRIX_SOURCE.read_bytes()),
                     "guards": digest(Path(__file__).read_bytes())}
    if arguments.verify_grant_independent is not None:
        try:
            report = verify_actual_grant_release(arguments.verify_grant_independent, arguments.grant_independent_sha256)
        except Exception as error:
            report = {"suite": "completed-grant-release-raw-reconstruction", "passed": False,
                "errorType": type(error).__name__, "reason": str(error), "actualBusinessHttpRequests": 0, "actualProcessProbes": 0}
        report["sourceSha256"] = source_hashes
        with report_path.open("x", encoding="utf-8") as handle:
            json.dump(report, handle, sort_keys=True, indent=2)
            handle.write("\n")
        print(json.dumps(report, sort_keys=True))
        return 0 if report["passed"] else 1
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {"schemaVersion": 1, "suite": "prepare-nextup-global-reference-guards",
        "classification": "synthetic preparation and persistence guards; not reference or client acceptance evidence",
        "sourceSha256": source_hashes, "testsRun": result.testsRun, "failures": len(result.failures),
        "errors": len(result.errors), "skips": len(result.skipped), "passed": result.wasSuccessful(),
        "failureDetails": [{"test": case.id(), "traceback": details} for case, details in result.failures],
        "errorDetails": [{"test": case.id(), "traceback": details} for case, details in result.errors],
        "skipDetails": [{"test": case.id(), "reason": reason} for case, reason in result.skipped],
        "actualBusinessHttpRequests": 0, "actualProcessProbes": 0, "liveAcceptanceClaim": False,
        "preservationOrderProbe": PRESERVATION_ORDER_OBSERVATION}
    report_path.parent.mkdir(parents=True, exist_ok=True)
    with report_path.open("x", encoding="utf-8") as handle:
        json.dump(report, handle, sort_keys=True, indent=2)
        handle.write("\n")
    report_path.chmod(0o600)
    print(json.dumps(report, sort_keys=True))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
