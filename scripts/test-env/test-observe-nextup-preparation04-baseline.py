#!/usr/bin/env python3
"""Remote-only synthetic guards for the preparation04 baseline observer.

The fixed external parent seals are tested separately by the operator. These
guards replace only InputEvidence for completed replay. Actual ObserverRunner,
Journal, HTTP parsing, wire records, and completed-evidence replay remain intact.
No original assets, database, business HTTP, or runtime process probes are used.
Failure reports intentionally omit exception messages and traceback contents.
"""

from __future__ import annotations

import argparse
import base64
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shlex
import socket
import stat
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import uuid


SUBJECT = SUPPORT = None
RUNTIME_TICKS = 6_000_000_000
ZERO = {"IsFavorite": False, "PlayCount": 0, "PlaybackPositionTicks": 0, "Played": False}
BASE_TIME = "2026-09-15T00:00:30+00:00"


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False) + "\n").encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def descriptor(path):
    return {"path": str(path), "sha256": digest(path.read_bytes())}


def read_json(path):
    return json.loads(path.read_bytes())


def write_json(path, value):
    path.write_bytes(encoded(value))
    path.chmod(0o600)
    return descriptor(path)


def load(path, label):
    name = label + "_" + uuid.uuid4().hex
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    exec(compile(path.read_bytes(), str(path), "exec"), module.__dict__)
    return module


class Clock:
    def __init__(self):
        self.value = 100.0
        self.base = datetime(2026, 9, 15, tzinfo=timezone.utc)

    def __call__(self):
        self.value += 0.01
        return self.value

    def utc(self):
        return (self.base + timedelta(seconds=self.value)).isoformat()


class FakeAuthority(SimpleNamespace):
    """Keep only the independent historical-input boundary synthetic."""

    def __init__(self, fixture):
        super().__init__(**fixture.evidence_fields())
        self.fixture = fixture
        self.support = fixture.support
        self.credentials = deepcopy(fixture.credentials)
        self.acquired = self.closed = False
        self.checks = 0
        self.failure_at = None

    def acquire(self):
        if self.acquired or self.closed:
            raise AssertionError("Synthetic authority cannot resume.")
        self.acquired = True
        self.check()

    def check(self):
        if not self.acquired or self.closed:
            raise AssertionError("Synthetic authority is closed.")
        self.checks += 1
        if self.checks == self.failure_at:
            raise self.fixture.module.ObservationError("Synthetic authority changed.")

    def close(self):
        self.closed = True


class Fixture:
    def __init__(self, test):
        self.test = test
        temporary = tempfile.TemporaryDirectory(prefix="nextup-preparation04-observer-")
        test.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.clock = Clock()
        self.module = load(SUBJECT, "preparation04_observer_guard")
        self.support = load(SUPPORT, "preparation04_transport_guard")
        test.addCleanup(sys.modules.pop, self.module.__name__, None)
        test.addCleanup(sys.modules.pop, self.support.__name__, None)
        self.users = ["user-%02d" % index for index in range(10)]
        self.libraries = ["library-%02d" % index for index in range(12)]
        self.credentials = {"userId": self.users[0], "username": "Synthetic Administrator", "credentialRef": "synthetic-admin-credential",
                            "password": "synthetic-private-guard-password-" + "x" * 48}
        self.admin = {key: value for key, value in self.credentials.items() if key != "password"}
        self.admin["deviceId"] = "fresh-baseline-observer-device"
        scope = {key: str(self.root / key) for key in ("fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot")}
        for path in scope.values():
            Path(path).mkdir(mode=0o700)
        self.sealed = self.root / "sealed"
        self.sealed.mkdir(mode=0o700)
        self.output = Path(scope["fixtureRoot"]) / "fresh-observer"
        scope["outputRoot"] = str(self.output)
        sources = {}
        for role, original in (("observer", SUBJECT), ("transport", SUPPORT)):
            path = Path(scope["sourceRoot"]) / original.name
            path.write_bytes(original.read_bytes())
            path.chmod(0o600)
            sources[role] = descriptor(path)
        proxy_source = Path(scope["proxySourceRoot"]) / "synthetic-proxy.py"
        proxy_source.write_bytes(b"# Synthetic metadata-only fixture; never executed.\n")
        proxy_source.chmod(0o600)
        sources["proxy"] = descriptor(proxy_source)
        process = {"pid": 54321, "startTicks": "1234", "bootId": "synthetic-boot", "uid": 0,
                   "exe": str(self.root / "forbidden-original" / "application"), "exeDevice": 2049, "exeInode": 100,
                   "cmdline": ["synthetic-application", "-programdata", str(self.root / "forbidden-data")],
                   "networkNamespace": "net:[54321]", "cgroup": "0::/synthetic-application\n"}
        proxy = deepcopy(process)
        proxy.update(pid=54322, startTicks="1235", exe=str(self.root / "synthetic-python"), exeInode=101,
                     cmdline=["synthetic-python", sources["proxy"]["path"]], networkNamespace="net:[54322]",
                     cgroup="0::/synthetic-proxy\n", listener={"port": 18197, "socketInode": "54322"})
        lock = Path(scope["fixtureRoot"]) / "reference.lock"
        lock.write_bytes(b"")
        lock.chmod(0o600)
        self.old_routes = [{"group": "legacy-" + actor, "userId": self.users[user_index], "itemId": "old-episode-" + symbol}
                           for actor, user_index in (("P", 6), ("Q", 7)) for symbol in self.module.EPISODES]
        self.items = {}
        for index, symbol in enumerate(self.module.EPISODES):
            self.items[symbol] = {"id": "episode-" + symbol, "type": "Episode", "parentId": "season-" + symbol[0],
                                  "seriesId": "series-" + symbol[0], "indexNumber": index % 3 + 1,
                                  "parentIndexNumber": 1, "runtimeTicks": RUNTIME_TICKS}
        self.new_routes = [{"group": "preparation04-" + actor, "userId": self.users[user_index], "itemId": self.items[symbol]["id"]}
                           for actor, user_index in (("P", 8), ("Q", 9)) for symbol in self.module.EPISODES]
        self.manifest = {"schemaVersion": 1, "kind": "nextup-preparation04-baseline-observation", "runId": "synthetic-preparation04-observer",
            "server": {"id": "synthetic-server", "version": "synthetic-version"}, "endpoint": {"scheme": "http", "host": "127.0.0.1", "port": 18197},
            "process": {"application": process, "endpoint": proxy, "workerNetworkNamespace": proxy["networkNamespace"]},
            "lock": {"path": str(lock), "device": lock.stat().st_dev, "inode": lock.stat().st_ino}, "sources": sources,
            "inputs": {"credentials": {"path": str(Path(scope["inputRoot"]) / "credentials.json"), "sha256": "a" * 64},
                       "parentSeal": {"path": str(self.sealed / "parent-seal.json"), "sha256": self.module.PARENT_SEAL_SHA256},
                       "recovery": {"path": str(self.sealed / "recovery.json"), "sha256": self.module.RECOVERY_SHA256}},
            "scope": scope, "sealedRoots": [str(self.sealed)],
            "forbiddenOriginalRoots": [str(self.root / "forbidden-original"), str(self.root / "forbidden-data")],
            "admin": deepcopy(self.admin), "preservation": {"userIds": self.users, "libraryIds": self.libraries, "detailRoutes": self.old_routes + self.new_routes},
            "budgets": {"requestSeconds": 5, "normalSeconds": 600, "cleanupSeconds": 60, "requestBytes": 32768,
                        "responseBytes": 262144, "totalResponseBytes": 32 * 1024 * 1024, "cleanupResponseBytes": 2 * 262145}}
        self.manifest["inputs"]["credentials"] = write_json(Path(self.manifest["inputs"]["credentials"]["path"]),
            {"schemaVersion": 1, "runId": self.manifest["runId"], "admin": self.credentials})
        self.input_path = Path(scope["inputRoot"]) / "manifest.json"
        self.input_descriptor = write_json(self.input_path, self.manifest)
        self.baseline = self.make_baseline()
        self.baseline_descriptor = write_json(self.sealed / "parent-public.json", self.baseline)
        self.restored_subject, self.restored_item = self.users[8], self.items["A1"]["id"]
        recovery_actor = {"userId": self.restored_subject, "username": self.baseline["roster"][self.restored_subject]["Name"],
                          "credentialRef": "synthetic-P4-credential", "deviceId": "closed-recovery-P4-device"}
        self.recovery_login = {"actor": recovery_actor, "deviceId": "2000", "token": "synthetic-closed-recovery-token",
                               "session": "synthetic-closed-recovery-session", "client": "Synthetic Recovery", "deviceName": "Synthetic Recovery Recorder",
                               "version": "1.0", "from": "2026-09-15T00:00:50.010000+00:00", "through": "2026-09-15T00:00:55.100000+00:00"}
        self.closed_device_windows = {key: {"from": BASE_TIME, "through": "2026-09-15T00:00:40+00:00",
            "userId": self.baseline["devices"][key]["LastUserId"], "reportedDeviceId": self.baseline["devices"][key]["ReportedDeviceId"]}
            for key in ("1093", "1094", "1095")}
        self.user_windows = {self.restored_subject: [{"from": self.recovery_login["from"], "through": self.recovery_login["through"],
                                                   "fields": {"LastLoginDate", "LastActivityDate"}}]}
        self.closed_token_hashes = {digest(("synthetic-closed-token-" + actor).encode()) for actor in ("admin", "P", "Q")}
        self.closed_token_hashes.add(digest(self.recovery_login["token"].encode()))
        self.closed_session_ids = {"synthetic-closed-session-" + actor for actor in ("admin", "P", "Q")}
        self.closed_session_ids.add(self.recovery_login["session"])
        self.not_before = "2026-09-15T00:01:00+00:00"
        self.make_preservation()
        self.calls, self.state_trace = [], []
        self.response_mutator = self.journal_mutator = None
        self.authority = FakeAuthority(self)
        self.runner = None

    def make_baseline(self):
        catalog = {library: {"old-item-" + str(index): {"Id": "old-item-" + str(index), "Name": "Old Item " + str(index)}}
                   for index, library in enumerate(self.libraries)}
        for index, symbol in enumerate(self.module.EPISODES):
            old_id = "old-episode-" + symbol
            catalog[self.libraries[index]][old_id] = {"Id": old_id, "Name": "Legacy " + symbol}
            item_id = self.items[symbol]["id"]
            catalog[self.libraries[10 if symbol.startswith("A") else 11]][item_id] = {"Id": item_id, "Name": "Prepared " + symbol}
        all_items = {key: row for rows in catalog.values() for key, row in rows.items()}
        projections = {user: {key: {**deepcopy(row), "UserData": deepcopy(ZERO), "ProjectionMarker": user}
                              for key, row in all_items.items()} for user in self.users}
        projections[self.users[8]][self.items["A1"]["id"]]["UserData"] = {**deepcopy(ZERO),
            "PlaybackPositionTicks": 1_200_000_000, "PlayedPercentage": 20}
        details = {}
        for route in self.old_routes:
            details.setdefault(route["group"], {})[route["itemId"]] = {"Id": route["itemId"], "Name": "Complete Legacy Detail",
                "UserData": deepcopy(ZERO), "PreservedOpaque": {"subject": route["userId"], "values": [1, False, None]}}
        devices = {}
        for index in range(96):
            key = str(1000 + index)
            owner = self.users[0 if index == 93 else 8 if index == 94 else 9 if index == 95 else 1]
            devices[key] = {"Id": key, "ReportedDeviceId": "old-reported-" + key, "LastUserId": owner,
                            "LastUserName": "Synthetic Administrator" if owner == self.users[0] else "Synthetic User " + owner,
                            "Name": "Old Device", "AppName": "Old Client", "AppVersion": "1.0", "DateLastActivity": BASE_TIME,
                            "PreservedOpaque": index}
        return {"marker": "synthetic-preparation04-parent-public", "version": 1, "captured_at": BASE_TIME,
                "server": {"Id": self.manifest["server"]["id"], "Version": self.manifest["server"]["version"], "FullDocument": [1, 2]},
                "configuration": {"Complete": {"Keep": True}},
                "roster": {user: {"Id": user, "Name": "Synthetic Administrator" if index == 0 else "Synthetic User " + user,
                    "Policy": {"IsAdministrator": index == 0, "Keep": [1, 2]}, "Configuration": {"Keep": True},
                    "LastLoginDate": BASE_TIME, "LastActivityDate": BASE_TIME} for index, user in enumerate(self.users)},
                "libraries": {key: {"ItemId": key, "Name": key, "Locations": ["/synthetic-owned/" + key], "LibraryOptions": {"Keep": True}} for key in self.libraries},
                "catalog_by_library": catalog, "items_by_user": projections, "preferences": {user: {"Keep": user} for user in self.users},
                "details": details, "devices": devices,
                "credential_context": {"channel": "controller_api", "authenticated_user_id": self.users[0], "token_sha256": "b" * 64, "user_id_semantics": "subject_projection"}}

    def make_preservation(self):
        goby = {"database": {"metadata": {"captured_at": "2026-09-15T00:01:00Z", "schema_version": 99},
                              "tables": {"users": [{"id": "synthetic-goby-user", "disabled": False}], "items": []}},
                "services": {"synthetic-goby.service": {"ActiveState": "active"}}, "completeDocument": [1, False, None]}
        anchor_goby = write_json(self.sealed / "goby-anchor.json", goby)
        before_goby = deepcopy(goby)
        before_goby["database"]["metadata"]["captured_at"] = "2026-09-15T00:01:20Z"
        before_goby_descriptor = write_json(self.sealed / "goby-before.json", before_goby)
        after_goby = deepcopy(goby)
        after_goby["database"]["metadata"]["captured_at"] = "2026-09-15T00:03:20Z"
        after_goby_descriptor = write_json(self.sealed / "goby-after.json", after_goby)
        info = self.sealed.stat()
        common = {"roots": {str(self.sealed): {"device": info.st_dev, "inode": info.st_ino, "sealedMarker": "synthetic-owned-state"}},
                  "root_count": 1, "services": {"synthetic-reference.service": {"InvocationID": "a" * 32, "MainPID": "54321"}},
                  "main_files": {"synthetic-owned-source": {"sha256": "d" * 64, "bytes": 16}},
                  "goby_counts": {"users": 1, "items": 0}}
        self.parent_preservation_descriptor = write_json(self.sealed / "parent-preservation.json",
            {**deepcopy(common), "goby_snapshot": anchor_goby, "capturedAt": "2026-09-15T00:01:00Z"})
        self.preservation_before = {**deepcopy(common), "goby_snapshot": before_goby_descriptor, "capturedAt": "2026-09-15T00:01:20Z"}
        self.preservation_after = {**deepcopy(common), "goby_snapshot": after_goby_descriptor, "capturedAt": "2026-09-15T00:03:20Z"}

    def evidence_fields(self):
        return deepcopy({"baseline": self.baseline, "baseline_descriptor": self.baseline_descriptor, "old_routes": self.old_routes,
            "new_details": {(route["group"], route["itemId"]): next(item for item in self.items.values() if item["id"] == route["itemId"]) for route in self.new_routes},
            "restored_subject": self.restored_subject, "restored_item": self.restored_item, "recovery_login": self.recovery_login,
            "closed_device_windows": self.closed_device_windows, "user_windows": self.user_windows, "previous_devices": self.baseline["devices"],
            "closed_token_hashes": self.closed_token_hashes, "closed_session_ids": self.closed_session_ids, "units": [],
            "not_before": self.not_before, "seal": {"afterPreservation": self.parent_preservation_descriptor}})

    def login_body(self):
        return {"ServerId": self.manifest["server"]["id"], "AccessToken": "synthetic-fresh-observer-token",
            "User": {"Id": self.admin["userId"], "Name": self.admin["username"], "Policy": {"IsAdministrator": True}},
            "SessionInfo": {"Id": "synthetic-fresh-observer-session", "InternalDeviceId": 2001, "ServerId": self.manifest["server"]["id"],
                "UserId": self.admin["userId"], "UserName": self.admin["username"], "DeviceId": self.admin["deviceId"],
                "Client": self.module.CLIENT, "DeviceName": self.module.DEVICE_NAME, "ApplicationVersion": self.module.VERSION}}

    @staticmethod
    def envelope(rows, *, devices=False):
        return {"Items": list(rows.values()), "TotalRecordCount": 0 if devices else len(rows)}

    def episode(self, item):
        return {"Id": item["id"], "Type": item["type"], "ParentId": item["parentId"], "SeasonId": item["parentId"], "SeriesId": item["seriesId"],
                "IndexNumber": item["indexNumber"], "ParentIndexNumber": item["parentIndexNumber"], "RunTimeTicks": item["runtimeTicks"],
                "Name": "Prepared Full Episode", "UserData": deepcopy(ZERO), "PreservedExtra": [1, False]}

    def devices(self):
        rows = deepcopy(self.baseline["devices"])
        for key, window in self.closed_device_windows.items():
            rows[key]["DateLastActivity"] = window["through"]
        recovered = self.recovery_login
        actor = recovered["actor"]
        rows[recovered["deviceId"]] = {"Id": recovered["deviceId"], "ReportedDeviceId": actor["deviceId"], "LastUserId": actor["userId"],
            "LastUserName": actor["username"], "Name": recovered["deviceName"], "AppName": recovered["client"], "AppVersion": recovered["version"],
            "DateLastActivity": "2026-09-15T00:00:50+00:00"}
        rows["2001"] = {"Id": "2001", "ReportedDeviceId": self.admin["deviceId"], "LastUserId": self.admin["userId"],
            "LastUserName": self.admin["username"], "Name": self.module.DEVICE_NAME, "AppName": self.module.CLIENT,
            "AppVersion": self.module.VERSION, "DateLastActivity": self.clock.utc()}
        return rows

    def body(self, label):
        if label == "login":
            return 200, self.login_body()
        if label == "logout":
            return 204, None
        if label == "rejection":
            return 401, "Unauthorized"
        if label in ("server", "configuration"):
            return 200, deepcopy(self.baseline[label])
        if label == "users":
            rows = deepcopy(self.baseline["roster"])
            rows[self.admin["userId"]]["LastLoginDate"] = self.clock.utc()
            rows[self.admin["userId"]]["LastActivityDate"] = self.clock.utc()
            for field in ("LastLoginDate", "LastActivityDate"):
                rows[self.restored_subject][field] = "2026-09-15T00:00:52+00:00"
            return 200, list(rows.values())
        if label == "libraries":
            return 200, self.envelope(deepcopy(self.baseline["libraries"]))
        if label == "devices":
            return 200, self.envelope(self.devices(), devices=True)
        prefix, index = label.split("-", 1)
        index = int(index)
        if prefix == "catalog":
            return 200, self.envelope(deepcopy(self.baseline["catalog_by_library"][sorted(self.libraries)[index]]))
        if prefix in ("items", "prefs"):
            user = sorted(self.users)[index]
            if prefix == "prefs":
                return 200, deepcopy(self.baseline["preferences"][user])
            rows = deepcopy(self.baseline["items_by_user"][user])
            if user == self.restored_subject:
                rows[self.restored_item]["UserData"] = deepcopy(ZERO)
            return 200, self.envelope(rows)
        route = self.manifest["preservation"]["detailRoutes"][index]
        if index < len(self.old_routes):
            return 200, deepcopy(self.baseline["details"][route["group"]][route["itemId"]])
        return 200, self.episode(next(item for item in self.items.values() if item["id"] == route["itemId"]))

    def send(self, request, headers, payload, **bounds):
        self.calls.append({"label": request.label, "method": request.method, "route": request.route,
                           "headers": deepcopy(headers), "payload": payload, "bounds": bounds})
        status, body = self.body(request.label)
        raw = b"" if body is None else encoded(body)
        response = {"status": status, "headers": [("Content-Type", "application/json; charset=utf-8"), ("Content-Length", str(len(raw)))],
                    "raw": raw, "complete_http": True, "completed_at": self.clock.utc(), "failure": None}
        if self.response_mutator:
            self.response_mutator(request, response)
        return SimpleNamespace(**response)

    def mutate_body(self, label, mutate):
        def change(request, response):
            if request.label == label:
                body = json.loads(response["raw"])
                mutate(body)
                response["raw"] = encoded(body)
                response["headers"] = [(key, str(len(response["raw"])) if key.lower() == "content-length" else value)
                                       for key, value in response["headers"]]
        self.response_mutator = change

    def make_runner(self):
        def journal(root, uid):
            value = self.support.Journal(root, uid=uid)
            original_state = value.state
            def state(data):
                self.state_trace.append(deepcopy(data))
                return original_state(data)
            value.state = state
            if self.journal_mutator:
                self.journal_mutator(value)
            return value
        self.runner = self.module.ObserverRunner(self.manifest, authority=self.authority, transport=SimpleNamespace(send=self.send),
            journal_factory=journal, monotonic=self.clock, utc_now=self.clock.utc)
        return self.runner

    def run(self):
        return self.make_runner().run()

    def private(self, name):
        return self.output / "private" / name

    def scope_inventory(self):
        entries = {}
        for path in [self.output, *sorted(self.output.rglob("*"))]:
            info = path.lstat()
            name = "." if path == self.output else path.relative_to(self.output).as_posix()
            row = {"device": info.st_dev, "inode": info.st_ino, "uid": info.st_uid, "gid": info.st_gid,
                   "mode": info.st_mode, "links": info.st_nlink}
            if path.is_file():
                raw = path.read_bytes()
                row.update(sha256=digest(raw), bytes=len(raw))
            entries[name] = row
        return {str(self.output): entries}

    @staticmethod
    def exec_start(command):
        return "{ path=/usr/bin/python3 ; argv[]=" + command + " ; ignore_errors=no ; start_time=[Tue 2026-09-15 00:01:40 UTC] ; stop_time=[Tue 2026-09-15 00:02:00 UTC] ; pid=45678 ; code=exited ; status=0 }"

    def completed_evidence(self):
        self.test.assertTrue(self.runner is not None and self.runner.completed)
        independent_root = Path(self.manifest["scope"]["fixtureRoot"]) / "independent"
        independent_root.mkdir(mode=0o700)
        rows = []
        for ordinal, planned in enumerate(self.runner.plan["requests"], 1):
            stem = "%04d-%s" % (ordinal, planned["label"])
            rows.append({"ordinal": ordinal, "label": planned["label"],
                         **{key: descriptor(self.private(stem + "-" + key + ".json")) for key in ("intent", "reserved", "response")}})
        index = {"schemaVersion": 1, "kind": "nextup-preparation04-baseline-wire-index", "runId": self.manifest["runId"], "requests": rows}
        self.index_path = independent_root / "wire-index.json"
        index_descriptor = write_json(self.index_path, index)
        inventory_descriptor = write_json(independent_root / "inventory.json", self.scope_inventory())
        before_descriptor = write_json(independent_root / "before.json", self.preservation_before)
        after_descriptor = write_json(independent_root / "after.json", self.preservation_after)
        unit_name = "synthetic-preparation04-observer.service"
        invocation = "c" * 32
        command = "/usr/bin/python3 -I -B " + self.manifest["sources"]["observer"]["path"] + " observe --manifest " + self.input_descriptor["path"] + " --manifest-sha256 " + self.input_descriptor["sha256"] + " --plan-sha256 " + self.runner.plan_sha256
        self.observe_command = command
        self.independent = {"schemaVersion": 1, "kind": "nextup-preparation04-baseline-independent-terminal", "status": "complete_baseline_independently_accepted",
            "runId": self.manifest["runId"], "source": self.manifest["sources"]["observer"], "manifest": self.input_descriptor,
            "state": descriptor(self.private("state.json")), "observerTerminal": descriptor(self.private("terminal.json")), "wireIndex": index_descriptor,
            "afterPublic": descriptor(self.private("public-baseline.json")), "closedAuthentication": descriptor(self.private("closed-authentication.json")),
            "scopeInventory": inventory_descriptor, "unit": {"name": unit_name, "invocationId": invocation,
                "properties": {"InvocationID": invocation, "MainPID": "0", "ActiveState": "active", "SubState": "exited", "Result": "success", "ExecMainStatus": "0",
                    "ControlGroup": "/system.slice/" + unit_name, "ExecMainPID": "45678", "ExecStart": self.exec_start(command)},
                "cgroupPath": "/sys/fs/cgroup/system.slice/" + unit_name},
            "formerPid": 45678, "formerPidAbsent": True, "cgroupEmpty": True, "requestCount": 64, "normalRequestCount": 62, "cleanupRequestCount": 2,
            "baselineUsableForFreshPreparation": True, "matrixFixtureReleased": False, "preservationBefore": before_descriptor, "preservationAfter": after_descriptor}
        self.independent_path = independent_root / "independent.json"
        self.evidence = {"manifest": self.input_descriptor, "independent": write_json(self.independent_path, self.independent)}
        return deepcopy(self.evidence)

    def reseal_independent(self):
        self.evidence["independent"] = write_json(self.independent_path, self.independent)

    def reseal_index(self, index):
        self.independent["wireIndex"] = write_json(self.index_path, index)
        self.reseal_independent()

    def replace_wire(self, label, kind, mutate):
        index = read_json(self.index_path)
        row = next(row for row in index["requests"] if row["label"] == label)
        path = Path(row[kind]["path"])
        value = read_json(path)
        mutate(value)
        row[kind] = write_json(path, value)
        self.reseal_index(index)

    def replay(self):
        fixture = self
        class SyntheticInputEvidence(SimpleNamespace):
            def __init__(self, manifest, reader):
                fixture.test.assertEqual(manifest, fixture.manifest)
                super().__init__(**fixture.evidence_fields())
                self.baseline = reader.record(self.baseline_descriptor)
        reads = []
        def read_bytes(row):
            path = Path(row["path"])
            self.module.require(self.root in path.parents, "Synthetic replay escaped its owned temporary root.")
            reads.append(str(path))
            return path.read_bytes()
        with patch.object(self.module, "InputEvidence", SyntheticInputEvidence):
            result = self.module.verify_completed_evidence(deepcopy(self.evidence), read_bytes=read_bytes)
        self.replay_reads = reads
        return result


class ObserverGuards(unittest.TestCase):
    def fixture(self):
        return Fixture(self)

    def assert_closed_success(self, f, terminal):
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual((terminal["requestCount"], terminal["normalRequestCount"], terminal["cleanupRequestCount"]), (64, 62, 2))
        self.assertTrue(terminal["cleanupComplete"])
        self.assertFalse(terminal["baselineReleased"])
        self.assertEqual(len(f.calls), 64)
        self.assertTrue(f.authority.closed)

    def test_complete_dynamic_snapshot_has_all_twenty_four_details_and_98_devices(self):
        f = self.fixture()
        self.assert_closed_success(f, f.run())
        after = read_json(f.private("public-baseline.json"))
        proof = read_json(f.private("public-preservation.json"))
        self.assertEqual((len(after["roster"]), len(after["libraries"]), sum(map(len, after["details"].values())), len(after["devices"])), (10, 12, 24, 98))
        self.assertEqual((proof["oldDetailWitnessesPreserved"], proof["newFullZeroDetailWitnesses"], proof["retainedDeviceCount"]), (12, 12, 96))
        for group, rows in f.baseline["details"].items():
            self.assertEqual(after["details"][group], rows)
        for route in f.new_routes:
            self.assertEqual(after["details"][route["group"]][route["itemId"]]["UserData"], ZERO)
        expected = deepcopy(f.baseline["items_by_user"])
        expected[f.restored_subject][f.restored_item]["UserData"] = deepcopy(ZERO)
        self.assertEqual(after["items_by_user"], expected)
        partial_projection = f.baseline["items_by_user"][f.restored_subject][f.restored_item]["UserData"]
        self.assertEqual(set(partial_projection), set(ZERO) | {"PlayedPercentage"})
        self.assertEqual((partial_projection["PlayCount"], partial_projection["PlayedPercentage"]), (0, 20))
        self.assertEqual(sum(call["method"] == "GET" for call in f.calls), 62)
        self.assertEqual([call["label"] for call in f.calls if call["method"] == "POST"], ["login", "logout"])

    def test_plan_derived_route_budget_is_frozen_and_contains_61_read_gets(self):
        f = self.fixture()
        plan = f.module.frozen_plan(f.manifest, f.baseline)
        self.assertEqual((plan["normalLimit"], plan["cleanupLimit"], plan["totalLimit"]), (62, 2, 64))
        self.assertEqual(len(f.module.read_plan(f.manifest, f.baseline)), 61)
        self.assertEqual(len({(row["method"], row["route"]) for row in plan["requests"]}), 64)
        smaller = deepcopy(f.manifest)
        smaller["preservation"]["detailRoutes"] = smaller["preservation"]["detailRoutes"][:-1]
        self.assertEqual(f.module.frozen_plan(smaller, f.baseline)["totalLimit"], 63)

    def test_complete_source_bound_login_and_logout_controller_receipts(self):
        f = self.fixture()
        self.assert_closed_success(f, f.run())
        closure = read_json(f.private("closed-authentication.json"))
        self.assertEqual(closure["login"]["response"], f.runner.responses["login"]["wire"])
        self.assertEqual(closure["deviceObservation"]["response"], f.runner.responses["devices"]["wire"])
        for label, status in (("logout", 204), ("rejection", 401)):
            result = read_json(Path(closure[label]["record"]["path"]))
            wire = read_json(Path(result["wire_response"]["path"]))
            self.assertEqual(result["status"], status)
            self.assertEqual(result["body"], f.module.decode_wire(wire))
            self.assertEqual(result["token_sha256"], digest(f.login_body()["AccessToken"].encode()))

    def test_login_responsibility_remains_pending_until_durable_owner_registration(self):
        f = self.fixture()
        self.assert_closed_success(f, f.run())
        pending = [row for row in f.state_trace if row["ownershipPending"] is not None]
        self.assertEqual({row["ownershipPending"]["stage"] for row in pending}, {"response-awaiting-owner", "owner-registered"})
        unowned = next(row for row in pending if row["ownershipPending"]["stage"] == "response-awaiting-owner")
        self.assertTrue(unowned["uncertain"])
        self.assertIsNone(unowned["token"])
        registered = next(row for row in pending if row["ownershipPending"]["stage"] == "owner-registered")
        self.assertTrue(registered["uncertain"])
        self.assertEqual(registered["ownershipPending"]["tokenSha256"], digest(f.login_body()["AccessToken"].encode()))

    def test_authentication_change_order_is_independent_of_set_iteration(self):
        f = self.fixture()
        self.assert_closed_success(f, f.run())
        proof = read_json(f.private("public-preservation.json"))
        dates = [(row["userId"], row["field"]) for row in proof["allowedAuthenticationChanges"] if row["kind"] == "acknowledged-authentication-date"]
        expected = [(user, field) for user in sorted((f.admin["userId"], f.restored_subject)) for field in ("LastActivityDate", "LastLoginDate")]
        self.assertEqual(dates, expected)
        class ReverseIterationSet(set):
            def __iter__(self):
                return iter(sorted(super().__iter__(), reverse=True))
            def union(self, *others):
                return ReverseIterationSet(super().union(*others))
        snapshot = read_json(f.private("public-baseline.json"))
        f.authority.baseline = f.module.strict_json(encoded(f.baseline))
        with patch.object(f.module, "set", ReverseIterationSet, create=True):
            repeated = f.module.preserve_snapshot(f.manifest, f.authority, snapshot, f.runner.login, f.runner.responses)
        self.assertEqual(repeated, proof)

    def test_fresh_login_cannot_precede_the_independent_closure_bound(self):
        f = self.fixture()
        f.authority.not_before = "2026-09-15T01:00:00Z"
        terminal = f.run()
        self.assertEqual(f.calls, [])
        self.assertFalse(terminal["completeSnapshotObserved"])
        self.assertEqual(terminal["requestCount"], 0)

    def test_nonempty_logout_204_cannot_authorize_rejection_or_known_cleanup(self):
        f = self.fixture()
        def malformed(request, response):
            if request.label == "logout":
                response.update(raw=b"{}", headers=[("Content-Type", "application/json"), ("Content-Length", "2")])
        f.response_mutator = malformed
        terminal = f.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertFalse(terminal["cleanupComplete"])
        self.assertEqual(len(f.calls), 63)
        self.assertEqual(f.calls[-1]["label"], "logout")

    def test_failed_cleanup_status_is_never_retried(self):
        for label, status, count in (("logout", 500, 63), ("rejection", 200, 64)):
            with self.subTest(label=label):
                f = self.fixture()
                f.response_mutator = lambda request, response: response.update(status=status) if request.label == label else None
                terminal = f.run()
                self.assertEqual(terminal["status"], "recovery_required")
                self.assertFalse(terminal["cleanupComplete"])
                self.assertEqual(len(f.calls), count)
                self.assertEqual(sum(row["label"] == label for row in f.calls), 1)

    def test_same_runner_and_existing_output_never_resume(self):
        f = self.fixture()
        f.run()
        with self.assertRaises(f.module.ObservationError):
            f.runner.run()
        self.assertEqual(len(f.calls), 64)
        other = self.fixture()
        other.output.mkdir(mode=0o700)
        result = other.run()
        self.assertEqual(other.calls, [])
        self.assertFalse(result["completeSnapshotObserved"])

    def test_known_get_status_failure_only_performs_two_exact_cleanup_calls(self):
        f = self.fixture()
        f.response_mutator = lambda request, response: response.update(status=503) if request.label == "server" else None
        terminal = f.run()
        self.assertEqual([row["label"] for row in f.calls], ["login", "server", "logout", "rejection"])
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertTrue(terminal["cleanupComplete"])

    def test_lost_transport_response_never_retries_or_infers_cleanup(self):
        f = self.fixture()
        def fail(request, response):
            if request.label == "server":
                raise TimeoutError("Synthetic lost response.")
        f.response_mutator = fail
        terminal = f.run()
        self.assertEqual([row["label"] for row in f.calls], ["login", "server"])
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertFalse(terminal["cleanupComplete"])

    def test_route_mutation_is_rejected_before_any_unplanned_get(self):
        f = self.fixture()
        runner = f.make_runner()
        runner.reads[0] = ("server", "/emby/Users/new-user")
        terminal = runner.run()
        self.assertEqual(f.calls, [])
        self.assertFalse(terminal["completeSnapshotObserved"])

    def test_cleanup_reserve_stops_normal_work_before_its_next_send(self):
        f = self.fixture()
        original = f.send
        def consume(request, headers, payload, **bounds):
            result = original(request, headers, payload, **bounds)
            if request.label == "login":
                budgets = f.manifest["budgets"]
                f.runner.charged_bytes = budgets["totalResponseBytes"] - budgets["cleanupResponseBytes"]
            return result
        runner = f.make_runner()
        runner.transport = SimpleNamespace(send=consume)
        terminal = runner.run()
        self.assertEqual([row["label"] for row in f.calls], ["login", "logout", "rejection"])
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertTrue(terminal["cleanupComplete"])

    def test_whole_second_device_times_are_allowed_only_in_the_observed_bucket(self):
        f = self.fixture()
        for value, accepted in (("2026-09-15T00:00:50Z", True), ("2026-09-15T00:00:50.0000000Z", True),
                                ("2026-09-15T00:00:49Z", False), ("2026-09-15T00:00:50.0000001Z", False)):
            with self.subTest(value=value):
                if accepted:
                    f.module.device_time_within(value, f.recovery_login["from"], f.recovery_login["through"])
                else:
                    with self.assertRaises(f.module.ObservationError):
                        f.module.device_time_within(value, f.recovery_login["from"], f.recovery_login["through"])

    def test_closed_device_rejects_100ns_backwards_activity(self):
        f = self.fixture()
        before = {"1093": deepcopy(f.baseline["devices"]["1093"])}
        before["1093"]["DateLastActivity"] = "2026-09-15T00:00:35.1234567Z"
        after = deepcopy(before)
        after["1093"]["DateLastActivity"] = "2026-09-15T00:00:35.1234561Z"
        with self.assertRaises(f.module.ObservationError):
            f.module.preserve_closed_devices(before, after, f.closed_device_windows)
        after["1093"]["DateLastActivity"] = "2026-09-15T00:00:35.1234568Z"
        self.assertEqual(len(f.module.preserve_closed_devices(before, after, f.closed_device_windows)), 1)

    def test_plaintext_401_requires_a_non_json_content_type(self):
        f = self.fixture()
        def plain(request, response):
            if request.label == "rejection":
                response.update(raw=b"Unauthorized", headers=[("Content-Type", "text/plain; charset=utf-8"), ("Content-Length", "12")])
        f.response_mutator = plain
        self.assert_closed_success(f, f.run())
        self.assertEqual(f.runner.responses["rejection"]["body"], "Unauthorized")

    def test_response_tuple_headers_preserve_order_and_duplicate_names(self):
        f = self.fixture()
        def duplicate(request, response):
            response["headers"] = [("X-Order", "first"), *response["headers"], ("X-Order", "second")]
        f.response_mutator = duplicate
        self.assert_closed_success(f, f.run())
        for ordinal, call in enumerate(f.calls, 1):
            wire = read_json(f.private("%04d-%s-response.json" % (ordinal, call["label"])))
            self.assertEqual((wire["headers"][0], wire["headers"][-1]), (["X-Order", "first"], ["X-Order", "second"]))

    def test_real_frozen_http_response_parser_feeds_all_64_observer_calls(self):
        f = self.fixture()
        runner = f.make_runner()
        runner.transport = f.support.HTTPTransport(f.manifest["endpoint"])
        expected_headers, parsed_headers, connections = [], [], []
        case = self
        class SyntheticSocket:
            def __init__(self, connection, planned):
                self.connection, self.planned = connection, planned
                self.request_bytes = bytearray()
                self.closed = False
            def sendall(self, data):
                self.request_bytes.extend(data)
            def makefile(self, mode):
                case.assertEqual(mode, "rb")
                head, separator, payload = bytes(self.request_bytes).partition(b"\r\n\r\n")
                case.assertEqual(separator, b"\r\n\r\n")
                lines = head.decode("iso-8859-1").split("\r\n")
                method, route, version = lines[0].split(" ", 2)
                case.assertEqual((method, route, version), (self.planned["method"], self.planned["route"], "HTTP/1.1"))
                headers = {key: value.strip() for key, value in (line.split(":", 1) for line in lines[1:])}
                if "Content-Length" in headers:
                    case.assertEqual(int(headers["Content-Length"]), len(payload))
                request = SimpleNamespace(label=self.planned["label"], method=method, route=route, actor="admin", cleanup=self.planned["label"] in ("logout", "rejection"))
                response = f.send(request, headers, payload or None, timeout_seconds=self.connection.timeout, max_bytes=f.manifest["budgets"]["responseBytes"] + 1)
                pairs = [("X-Order", "first"), *response.headers, ("X-Order", "second")]
                expected_headers.append(pairs)
                reason = {200: "OK", 204: "No Content", 401: "Unauthorized"}[response.status]
                raw = ("HTTP/1.1 %d %s\r\n" % (response.status, reason) + "".join(key + ": " + value + "\r\n" for key, value in pairs) + "\r\n").encode("iso-8859-1")
                return io.BytesIO(raw + response.raw)
            def close(self):
                self.closed = True
        class ObservedResponse(f.support.http.client.HTTPResponse):
            def getheaders(self):
                pairs = super().getheaders()
                parsed_headers.append(pairs)
                return pairs
        def connect(connection):
            case.assertLess(len(connections), len(runner.plan["requests"]))
            value = SyntheticSocket(connection, runner.plan["requests"][len(connections)])
            connections.append(value)
            connection.sock = value
        with patch.object(f.support.http.client.HTTPConnection, "connect", connect), patch.object(f.support.http.client.HTTPConnection, "response_class", ObservedResponse), patch.object(f.support, "utc_now", f.clock.utc):
            terminal = runner.run()
        self.assert_closed_success(f, terminal)
        self.assertEqual(len(parsed_headers), 64)
        self.assertEqual(parsed_headers, expected_headers)
        self.assertTrue(all(type(pair) is tuple for pairs in parsed_headers for pair in pairs))
        self.assertTrue(all(value.closed for value in connections))


def body_failure(label, mutate):
    def test(self):
        f = self.fixture()
        f.mutate_body(label, mutate)
        terminal = f.run()
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertTrue(terminal["cleanupComplete"])
        self.assertEqual([row["label"] for row in f.calls[-2:]], ["logout", "rejection"])
        self.assertLessEqual(len(f.calls), 64)
        self.assertFalse(terminal["completeSnapshotObserved"])
    return test


BODY_FAILURES = {
    "old_detail_extra_changed": ("detail-0", lambda body: body.update(PreservedOpaque={"changed": True})),
    "new_detail_wrong_identity": ("detail-12", lambda body: body.update(Id="other-episode")),
    "new_detail_wrong_parent": ("detail-12", lambda body: body.update(ParentId="other-season")),
    "new_detail_wrong_season": ("detail-12", lambda body: body.update(SeasonId="other-season")),
    "new_detail_bool_index": ("detail-12", lambda body: body.update(IndexNumber=True)),
    "new_detail_bool_parent_index": ("detail-12", lambda body: body.update(ParentIndexNumber=True)),
    "new_detail_wrong_series": ("detail-12", lambda body: body.update(SeriesId="other-series")),
    "new_detail_wrong_runtime": ("detail-12", lambda body: body.update(RunTimeTicks=RUNTIME_TICKS + 1)),
    "new_detail_float_runtime": ("detail-12", lambda body: body.update(RunTimeTicks=float(RUNTIME_TICKS))),
    "new_detail_extra_zero_percentage": ("detail-12", lambda body: body["UserData"].update(PlayedPercentage=0)),
    "new_detail_extra_null_date": ("detail-12", lambda body: body["UserData"].update(LastPlayedDate=None)),
    "new_detail_nonzero_count": ("detail-12", lambda body: body["UserData"].update(PlayCount=1)),
    "new_detail_favorite_changed": ("detail-12", lambda body: body["UserData"].update(IsFavorite=True)),
    "new_detail_missing_zero_field": ("detail-12", lambda body: body["UserData"].pop("IsFavorite")),
    "new_detail_bool_count": ("detail-12", lambda body: body["UserData"].update(PlayCount=False)),
    "projection_wrong_target_metadata": ("items-8", lambda body: next(row for row in body["Items"] if row["Id"] == "episode-A1").update(ProjectionMarker="changed")),
    "projection_wrong_target_state": ("items-8", lambda body: next(row for row in body["Items"] if row["Id"] == "episode-A1")["UserData"].update(PlayedPercentage=0)),
    "projection_other_item_changed": ("items-8", lambda body: next(row for row in body["Items"] if row["Id"] == "episode-A2")["UserData"].update(PlayCount=1)),
    "projection_other_subject_changed": ("items-9", lambda body: next(row for row in body["Items"] if row["Id"] == "episode-A1")["UserData"].update(PlayCount=1)),
    "unknown_device_added": ("devices", lambda body: body["Items"].append({"Id": "9999", "ReportedDeviceId": "unowned-device"})),
    "old_device_removed": ("devices", lambda body: body["Items"].pop(0)),
    "old_device_structure_changed": ("devices", lambda body: body["Items"][0].update(PreservedOpaque="changed")),
    "old_device_date_changed_without_window": ("devices", lambda body: body["Items"][0].update(DateLastActivity="2026-09-15T00:00:31Z")),
    "closed_device_structure_changed": ("devices", lambda body: next(row for row in body["Items"] if row["Id"] == "1093").update(Name="changed")),
    "closed_device_after_close": ("devices", lambda body: next(row for row in body["Items"] if row["Id"] == "1093").update(DateLastActivity="2026-09-15T00:00:41Z")),
    "recovery_device_wrong_owner": ("devices", lambda body: next(row for row in body["Items"] if row["Id"] == "2000").update(LastUserId="user-09")),
    "recovery_device_after_close": ("devices", lambda body: next(row for row in body["Items"] if row["Id"] == "2000").update(DateLastActivity="2026-09-15T00:00:56Z")),
    "observer_device_wrong_client": ("devices", lambda body: next(row for row in body["Items"] if row["Id"] == "2001").update(AppName="another-client")),
    "observer_device_future": ("devices", lambda body: next(row for row in body["Items"] if row["Id"] == "2001").update(DateLastActivity="2099-01-01T00:00:00Z")),
    "duplicate_reported_device": ("devices", lambda body: body["Items"][-1].update(ReportedDeviceId=body["Items"][0]["ReportedDeviceId"])),
    "devices_incomplete_count": ("devices", lambda body: body.update(TotalRecordCount=1)),
    "unowned_account_date": ("users", lambda body: body[1].update(LastActivityDate="2026-09-15T00:00:52Z")),
    "recovery_account_after_window": ("users", lambda body: body[8].update(LastLoginDate="2026-09-15T00:00:56Z")),
    "recovery_account_backwards": ("users", lambda body: body[8].update(LastLoginDate="2026-09-15T00:00:29Z")),
    "recovery_account_policy_changed": ("users", lambda body: body[8]["Policy"].update(Keep=[])),
    "configuration_changed": ("configuration", lambda body: body.update(Unexpected=True)),
    "catalog_changed": ("catalog-0", lambda body: body["Items"][0].update(Name="changed")),
    "preferences_changed": ("prefs-0", lambda body: body.update(Unexpected=True)),
    "user_missing": ("users", lambda body: body.pop()),
    "library_missing": ("libraries", lambda body: body["Items"].pop()),
}
for name, (label, mutate) in BODY_FAILURES.items():
    setattr(ObserverGuards, "test_reject_" + name, body_failure(label, mutate))


def wire_failure(name, mutate, *, label="server"):
    def test(self):
        f = self.fixture()
        f.response_mutator = lambda request, response: mutate(response) if request.label == label else None
        terminal = f.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertFalse(terminal["cleanupComplete"])
        self.assertEqual([row["label"] for row in f.calls], ["login"] if label == "login" else ["login", "server"])
        if label == "login":
            self.assertIsNotNone(f.runner.ownership_pending)
            self.assertTrue(f.private("0001-login-response.json").is_file())
    return test


WIRE_FAILURES = {
    "incomplete": lambda response: response.update(complete_http=False),
    "failure": lambda response: response.update(failure="TimeoutError"),
    "framing": lambda response: response.update(headers=[("Content-Type", "application/json"), ("Content-Length", "1")]),
    "conflicting_lengths": lambda response: response["headers"].append(("Content-Length", "1")),
    "oversized": lambda response: response.update(raw=b"x" * 262145),
    "duplicate_json_keys": lambda response: response.update(raw=b'{"Id":1,"Id":2}', headers=[("Content-Type", "application/json")]),
    "nonfinite_json": lambda response: response.update(raw=b'{"Value":NaN}', headers=[("Content-Type", "application/json")]),
    "invalid_utf8": lambda response: response.update(raw=b'"\xff"', headers=[("Content-Type", "application/json")]),
    "json_plaintext": lambda response: response.update(raw=b"Unauthorized", headers=[("Content-Type", "application/json")]),
    "backwards_time": lambda response: response.update(completed_at="2026-09-14T00:00:00Z"),
}
for name, mutate in WIRE_FAILURES.items():
    setattr(ObserverGuards, "test_wire_" + name, wire_failure(name, mutate))
setattr(ObserverGuards, "test_login_raw_failure_keeps_unknown_responsibility", wire_failure("login", WIRE_FAILURES["duplicate_json_keys"], label="login"))


def login_failure(mutate):
    def test(self):
        f = self.fixture()
        f.mutate_body("login", mutate)
        terminal = f.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual([row["label"] for row in f.calls], ["login"])
        self.assertIsNotNone(f.runner.ownership_pending)
        self.assertIsNone(f.runner.token)
        self.assertFalse(terminal["cleanupComplete"])
    return test


for name, mutate in {
    "wrong_principal": lambda body: body["User"].update(Id="user-09"),
    "non_administrator": lambda body: body["User"]["Policy"].update(IsAdministrator=False),
    "wrong_device": lambda body: body["SessionInfo"].update(DeviceId="another-device"),
    "closed_token": lambda body: body.update(AccessToken="synthetic-closed-recovery-token"),
    "closed_session": lambda body: body["SessionInfo"].update(Id="synthetic-closed-recovery-session"),
}.items():
    setattr(ObserverGuards, "test_login_reject_" + name, login_failure(mutate))


def persistence_failure(stage):
    def test(self):
        f = self.fixture()
        triggered = []
        def change(journal):
            original_save, original_state = journal.save, journal.state
            def save(name, value, **kwargs):
                if not triggered and ((stage == "reserved" and name == "0001-login-reserved.json") or
                    (stage == "wire" and name == "0001-login-response.json") or
                    (stage == "adapter" and name == "0063-logout-controller-result.json")):
                    triggered.append(stage)
                    raise OSError("Synthetic durable write failure.")
                return original_save(name, value, **kwargs)
            def state(value):
                owner = value.get("ownershipPending")
                match = (stage == "before_owner" and owner is not None and owner["stage"] == "response-awaiting-owner") or (
                    stage == "owner_registered" and owner is not None and owner["stage"] == "owner-registered") or (
                    stage == "owner_clear" and value.get("token") is not None and owner is None and value["requestCount"] == 1)
                if not triggered and match:
                    triggered.append(stage)
                    raise OSError("Synthetic durable owner failure.")
                return original_state(value)
            journal.save, journal.state = save, state
        f.journal_mutator = change
        terminal = f.run()
        self.assertEqual(triggered, [stage])
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertFalse(terminal["cleanupComplete"])
        self.assertEqual(len(f.calls), 0 if stage == "reserved" else 63 if stage == "adapter" else 1)
        self.assertNotIn("rejection", [row["label"] for row in f.calls])
    return test


for stage in ("reserved", "wire", "before_owner", "owner_registered", "owner_clear", "adapter"):
    setattr(ObserverGuards, "test_persistence_" + stage, persistence_failure(stage))


class CompletedEvidenceGuards(unittest.TestCase):
    """Replay actual fresh worker artifacts with only historical inputs synthetic."""

    fixture = ObserverGuards.fixture
    assert_closed_success = ObserverGuards.assert_closed_success

    def completed_fixture(self):
        f = self.fixture()
        self.assert_closed_success(f, f.run())
        f.completed_evidence()
        return f

    def assert_replay_rejected(self, f):
        with self.assertRaises((f.module.ObservationError, ValueError, KeyError, OSError)):
            f.replay()

    def test_completed_evidence_replays_all_64_actual_wire_and_journal_bytes(self):
        f = self.completed_fixture()
        before = {str(path): path.read_bytes() for path in f.root.rglob("*") if path.is_file()}
        result = f.replay()
        after = {str(path): path.read_bytes() for path in f.root.rglob("*") if path.is_file()}
        self.assertEqual(before, after)
        self.assertEqual((result["requestCount"], result["normalRequestCount"], result["cleanupRequestCount"]), (64, 62, 2))
        self.assertEqual(result["baseline"], read_json(f.private("public-baseline.json")))
        self.assertEqual(len(result["baseline"]["devices"]), 98)
        for ordinal, planned in enumerate(f.runner.plan["requests"], 1):
            for suffix in ("intent", "reserved", "response"):
                self.assertIn(str(f.private("%04d-%s-%s.json" % (ordinal, planned["label"], suffix))), f.replay_reads)
        self.assertEqual(len(f.calls), 64)

    def test_completed_wire_index_rejects_missing_reordered_and_extra_requests(self):
        for mutation in (lambda rows: rows.pop(), lambda rows: rows.reverse(), lambda rows: rows.append(deepcopy(rows[-1]))):
            with self.subTest(mutation_index=str(mutation.__name__)):
                f = self.completed_fixture()
                index = read_json(f.index_path)
                mutation(index["requests"])
                f.reseal_index(index)
                self.assert_replay_rejected(f)

    def test_completed_summary_cannot_replace_request_population(self):
        f = self.completed_fixture()
        f.independent.update(requestCount=65, normalRequestCount=63)
        f.reseal_independent()
        self.assert_replay_rejected(f)

    def test_completed_raw_wire_digest_cannot_be_replaced_by_decoded_body(self):
        f = self.completed_fixture()
        path = f.private("0002-server-response.json")
        path.write_bytes(path.read_bytes() + b" ")
        self.assert_replay_rejected(f)

    def test_completed_semantically_identical_plan_still_requires_exact_bytes(self):
        f = self.completed_fixture()
        path = f.private("frozen-plan.json")
        path.write_bytes(b" " + path.read_bytes())
        self.assert_replay_rejected(f)

    def test_completed_reserved_counter_must_bind_actual_request(self):
        f = self.completed_fixture()
        f.replace_wire("server", "reserved", lambda value: value.update(requestCount=99))
        self.assert_replay_rejected(f)

    def test_completed_resealed_response_cannot_substitute_an_actual_snapshot(self):
        f = self.completed_fixture()
        def mutate(wire):
            body = json.loads(base64.b64decode(wire["rawBase64"]))
            body["FullDocument"] = [99]
            raw = encoded(body)
            wire.update(rawBase64=base64.b64encode(raw).decode(), observedRawBytes=len(raw))
            wire["headers"] = [[key, str(len(raw)) if key.lower() == "content-length" else value] for key, value in wire["headers"]]
        f.replace_wire("server", "response", mutate)
        self.assert_replay_rejected(f)

    def test_completed_final_state_counts_are_reproduced_not_trusted(self):
        f = self.completed_fixture()
        path = f.private("state.json")
        value = read_json(path)
        value["chargedResponseBytes"] += 1
        f.independent["state"] = write_json(path, value)
        f.reseal_independent()
        self.assert_replay_rejected(f)

    def test_completed_state_rejects_an_identical_external_copy_with_or_without_actual_state(self):
        for keep_actual in (True, False):
            with self.subTest(keep_actual=keep_actual):
                f = self.completed_fixture()
                actual = f.private("state.json")
                alias = f.independent_path.parent / "identical-state-copy.json"
                alias.write_bytes(actual.read_bytes())
                alias.chmod(0o600)
                f.independent["state"] = descriptor(alias)
                self.assertEqual(f.independent["state"]["sha256"], descriptor(actual)["sha256"])
                if not keep_actual:
                    actual.unlink()
                f.reseal_independent()
                self.assert_replay_rejected(f)

    def test_completed_state_requires_the_actual_private_file_to_exist(self):
        f = self.completed_fixture()
        f.private("state.json").unlink()
        self.assert_replay_rejected(f)

    def test_completed_closed_token_receipt_cannot_be_substituted(self):
        f = self.completed_fixture()
        path = f.private("closed-authentication.json")
        value = read_json(path)
        value["tokenSha256"] = "f" * 64
        f.independent["closedAuthentication"] = write_json(path, value)
        terminal_path = f.private("terminal.json")
        terminal = read_json(terminal_path)
        terminal["outputs"]["closedAuthentication"] = f.independent["closedAuthentication"]
        f.independent["observerTerminal"] = write_json(terminal_path, terminal)
        f.reseal_independent()
        self.assert_replay_rejected(f)

    def test_completed_unit_requires_actual_control_group_and_manifest_binding(self):
        for field in ("ControlGroup", "ExecStart", "ExecMainPID"):
            with self.subTest(field=field):
                f = self.completed_fixture()
                f.independent["unit"]["properties"].pop(field)
                f.reseal_independent()
                self.assert_replay_rejected(f)

    def test_completed_unit_accepts_exact_plain_and_quoted_observe_argv(self):
        f = self.completed_fixture()
        for command in (f.observe_command, " ".join('"' + part + '"' for part in shlex.split(f.observe_command))):
            with self.subTest(quoted=command.startswith('"')):
                f.independent["unit"]["properties"]["ExecStart"] = command
                f.reseal_independent()
                self.assertEqual(f.replay()["requestCount"], 64)

    def test_completed_unit_rejects_plan_mode_missing_or_wrong_plan_and_extra_arguments(self):
        for mutation in ("plan_mode", "missing_plan", "wrong_plan", "extra_argument", "duplicate_plan"):
            with self.subTest(mutation=mutation):
                f = self.completed_fixture()
                argv = shlex.split(f.observe_command)
                if mutation == "plan_mode": argv[4] = "plan"
                elif mutation == "missing_plan": argv = argv[:-2]
                elif mutation == "wrong_plan": argv[-1] = "f" * 64 if argv[-1] != "f" * 64 else "e" * 64
                elif mutation == "extra_argument": argv += ["--unexpected"]
                else: argv += argv[-2:]
                f.independent["unit"]["properties"]["ExecStart"] = f.exec_start(shlex.join(argv))
                f.reseal_independent()
                self.assert_replay_rejected(f)

    def test_completed_unit_rejects_multiple_or_mismatched_systemctl_commands(self):
        for mutation in ("wrong_executable_path", "second_record", "duplicate_argv"):
            with self.subTest(mutation=mutation):
                f = self.completed_fixture()
                command = f.exec_start(f.observe_command)
                if mutation == "wrong_executable_path": command = command.replace("path=/usr/bin/python3", "path=/usr/bin/false", 1)
                elif mutation == "second_record": command += " " + f.exec_start(f.observe_command)
                else: command = command.replace(" ; ignore_errors=no", " ; argv[]=" + f.observe_command + " ; ignore_errors=no", 1)
                f.independent["unit"]["properties"]["ExecStart"] = command
                f.reseal_independent()
                self.assert_replay_rejected(f)

    def test_completed_scope_inventory_rejects_an_extra_actual_request_file(self):
        f = self.completed_fixture()
        write_json(f.private("0065-unplanned-intent.json"), {"ordinal": 65, "label": "unplanned", "request": {"method": "GET", "route": "/emby/Users"}})
        f.independent["scopeInventory"] = write_json(Path(f.independent["scopeInventory"]["path"]), f.scope_inventory())
        f.reseal_independent()
        self.assert_replay_rejected(f)

    def test_completed_scope_inventory_rejects_missing_entries_and_wrong_types(self):
        for mutation in ("missing", "file_type", "directory_type", "file_bytes", "file_links", "bool_uid"):
            with self.subTest(mutation=mutation):
                f = self.completed_fixture()
                path = Path(f.independent["scopeInventory"]["path"])
                inventory = read_json(path)
                entries = inventory[str(f.output)]
                file_entry = entries["private/0002-server-response.json"]
                if mutation == "missing":
                    entries.pop("private/0002-server-response.json")
                elif mutation == "file_type":
                    file_entry["mode"] = stat.S_IFDIR | 0o600
                elif mutation == "directory_type":
                    entries["private"]["mode"] = stat.S_IFREG | 0o700
                elif mutation == "file_bytes":
                    file_entry["bytes"] += 1
                elif mutation == "file_links":
                    file_entry["links"] = True
                else:
                    file_entry["uid"] = False
                f.independent["scopeInventory"] = write_json(path, inventory)
                f.reseal_independent()
                self.assert_replay_rejected(f)

    def test_completed_preservation_checks_full_goby_document_not_only_counts(self):
        f = self.completed_fixture()
        after_path = Path(f.independent["preservationAfter"]["path"])
        after = read_json(after_path)
        goby_path = Path(after["goby_snapshot"]["path"])
        goby = read_json(goby_path)
        goby["database"]["tables"]["users"][0]["disabled"] = True
        after["goby_snapshot"] = write_json(goby_path, goby)
        f.independent["preservationAfter"] = write_json(after_path, after)
        f.reseal_independent()
        self.assert_replay_rejected(f)

    def test_completed_preservation_rejects_roots_services_files_or_counts_drift(self):
        for section in ("roots", "services", "main_files", "goby_counts"):
            with self.subTest(section=section):
                f = self.completed_fixture()
                path = Path(f.independent["preservationAfter"]["path"])
                after = read_json(path)
                after[section]["unexpected"] = {"changed": True}
                if section == "roots":
                    after["root_count"] = len(after["roots"])
                f.independent["preservationAfter"] = write_json(path, after)
                f.reseal_independent()
                self.assert_replay_rejected(f)

    def test_completed_preservation_must_match_the_retained_parent_anchor(self):
        f = self.completed_fixture()
        for key in ("preservationBefore", "preservationAfter"):
            path = Path(f.independent[key]["path"])
            value = read_json(path)
            value["goby_counts"]["users"] = 2
            f.independent[key] = write_json(path, value)
        f.reseal_independent()
        self.assert_replay_rejected(f)

    def test_completed_preservation_timestamps_must_bracket_the_actual_run(self):
        for key, stamp in (("preservationBefore", "2026-09-15T00:01:50Z"), ("preservationAfter", "2026-09-15T00:01:40Z")):
            with self.subTest(key=key):
                f = self.completed_fixture()
                path = Path(f.independent[key]["path"])
                value = read_json(path)
                value["capturedAt"] = stamp
                f.independent[key] = write_json(path, value)
                f.reseal_independent()
                self.assert_replay_rejected(f)


class SafeResult(unittest.TestResult):
    """Never include private fixture values or traceback text in CLI output."""

    @staticmethod
    def failure_name(test):
        return test.id().split(" (", 1)[0]

    def addFailure(self, test, err):
        self.failures.append((self.failure_name(test), type(err[1]).__name__))

    def addError(self, test, err):
        self.errors.append((self.failure_name(test), type(err[1]).__name__))

    def addSubTest(self, test, subtest, err):
        if err is not None:
            if issubclass(err[0], test.failureException):
                self.addFailure(test, err)
            else:
                self.addError(test, err)


def main():
    global SUBJECT, SUPPORT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--subject", required=True)
    parser.add_argument("--transport", required=True)
    parser.add_argument("--report", required=True)
    args = parser.parse_args()
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or not sys.flags.isolated or not sys.flags.dont_write_bytecode:
        raise SystemExit("Guards require authorized root SSH with Python -I -B.")
    SUBJECT, SUPPORT = Path(args.subject).resolve(), Path(args.transport).resolve()
    report = Path(args.report)
    if report.exists():
        raise SystemExit("A guard report cannot be replayed.")
    result = SafeResult()
    loader = unittest.defaultTestLoader
    suite = unittest.TestSuite((loader.loadTestsFromTestCase(ObserverGuards), loader.loadTestsFromTestCase(CompletedEvidenceGuards)))
    with patch.object(socket.socket, "connect", side_effect=AssertionError("Real network use is forbidden.")), \
         patch.object(socket, "create_connection", side_effect=AssertionError("Real network use is forbidden.")), \
         patch.object(subprocess, "Popen", side_effect=AssertionError("Runtime process probes are forbidden.")):
        suite.run(result)
    value = {"schemaVersion": 1, "kind": "nextup-preparation04-baseline-observer-guards", "tests": result.testsRun,
             "failures": len(result.failures), "errors": len(result.errors), "skipped": len(result.skipped), "passed": result.wasSuccessful(),
             "failureTypes": [{"test": name, "type": kind} for name, kind in result.failures + result.errors],
             "realBusinessHttp": 0, "realProcessProbes": 0, "originalImplementationBytesRead": False, "referenceDatabaseRead": False,
             "historicalInputEvidenceSynthetic": True, "subjectSha256": digest(SUBJECT.read_bytes()), "transportSha256": digest(SUPPORT.read_bytes())}
    with report.open("xb") as handle:
        handle.write(encoded(value))
        handle.flush()
        os.fsync(handle.fileno())
    report.chmod(0o600)
    print(json.dumps(value, sort_keys=True, separators=(",", ":")))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
