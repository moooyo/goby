#!/usr/bin/env python3
"""Remote-only synthetic guards for the one-shot reference baseline observer.

All HTTP, process metadata, and systemd observations are synthetic. Every case
uses a new owned temporary journal. No original implementation or database is read.
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
import socket
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import urlencode
import uuid

SUBJECT = SUPPORT = None


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n").encode()


def load(path, label):
    name = label + uuid.uuid4().hex
    module = importlib.util.module_from_spec(importlib.util.spec_from_file_location(name, path))
    sys.modules[name] = module
    exec(compile(path.read_bytes(), str(path), "exec"), module.__dict__)
    return module


class Clock:
    def __init__(self):
        self.value = 100.0
        self.base = datetime(2026, 9, 13, tzinfo=timezone.utc)

    def __call__(self):
        self.value += 0.01
        return self.value

    def utc(self):
        return (self.base + timedelta(seconds=self.value)).isoformat()


class FakeAuthority:
    def __init__(self, fixture):
        self.fixture, self.support = fixture, fixture.support
        self.baseline, self.credentials = deepcopy(fixture.baseline), deepcopy(fixture.credentials)
        self.closure, self.previous_devices = deepcopy(fixture.closure), deepcopy(fixture.previous_devices)
        self.closed_device_windows = deepcopy(fixture.closed_device_windows)
        self.closed_token_hashes = set(fixture.closed_token_hashes)
        self.closed_session_ids = set(fixture.closed_session_ids)
        self.checks, self.failure_at = 0, None
        self.acquired = self.closed = False

    def acquire(self):
        if self.acquired or self.closed: raise AssertionError("Synthetic authority cannot resume.")
        self.acquired = True
        self.check()

    def check(self):
        if not self.acquired or self.closed: raise AssertionError("Synthetic authority is closed.")
        self.checks += 1
        if self.checks == self.failure_at: raise self.fixture.module.ObservationError("Synthetic authority drift.")

    def close(self):
        self.closed = True


class Fixture:
    def __init__(self, test):
        temporary = tempfile.TemporaryDirectory(prefix="nextup-observer-guard-")
        test.addCleanup(temporary.cleanup)
        self.root, self.clock = Path(temporary.name), Clock()
        self.module, self.support = load(SUBJECT, "observer_guard_"), load(SUPPORT, "observer_support_")
        test.addCleanup(sys.modules.pop, self.module.__name__, None)
        test.addCleanup(sys.modules.pop, self.support.__name__, None)
        self.users = ["user-" + str(index) for index in range(6)]
        self.libraries = ["library-" + str(index) for index in range(8)]
        self.credentials = {"userId": self.users[0], "username": "Owned Admin", "credentialRef": "owned-admin-secret", "password": "secret-" + "x" * 64}
        self.admin = {key: value for key, value in self.credentials.items() if key != "password"}
        self.admin["deviceId"] = "fresh-observer-device"
        scope = {key: str(self.root / key) for key in ("fixtureRoot", "inputRoot", "sourceRoot", "proxySourceRoot")}
        for path in scope.values(): Path(path).mkdir(mode=0o700)
        scope["outputRoot"] = str(Path(scope["fixtureRoot"]) / "fresh-observation")
        self.output = Path(scope["outputRoot"])
        self.sealed = self.root / "sealed"
        self.sealed.mkdir(mode=0o700)
        source = {}
        for role, original in (("observer", SUBJECT), ("transport", SUPPORT)):
            path = Path(scope["sourceRoot"]) / original.name
            path.write_bytes(original.read_bytes()); path.chmod(0o600)
            source[role] = {"path": str(path), "sha256": self.module.digest(path.read_bytes())}
        path = Path(scope["proxySourceRoot"]) / "synthetic-owned-proxy.py"
        path.write_bytes(b"# Synthetic owned proxy; never executed.\n"); path.chmod(0o600)
        source["proxy"] = {"path": str(path), "sha256": self.module.digest(path.read_bytes())}
        app = {"pid": 54321, "startTicks": "1234", "bootId": "synthetic-boot", "uid": 0, "exe": str(self.root / "forbidden" / "original"),
               "exeDevice": 2049, "exeInode": 54321, "cmdline": ["synthetic-executable", "-programdata", str(self.root / "forbidden-data")],
               "networkNamespace": "net:[54321]", "cgroup": "0::/synthetic-application\n"}
        proxy = deepcopy(app)
        proxy.update(pid=54322, startTicks="1235", exe=str(self.root / "synthetic-python"), networkNamespace="net:[54322]",
                     cmdline=["synthetic-python", source["proxy"]["path"]], cgroup="0::/synthetic-proxy\n", listener={"port": 18197, "socketInode": "54322"})
        lock = Path(scope["fixtureRoot"]) / "reference.lock"
        lock.write_bytes(b""); lock.chmod(0o600)
        self.manifest = {"schemaVersion": 2, "runId": "synthetic-observation", "server": {"id": "synthetic-server", "version": "synthetic-v1"}, "closedObservers": [],
            "endpoint": {"scheme": "http", "host": "127.0.0.1", "port": 18197},
            "process": {"application": app, "endpoint": proxy, "workerNetworkNamespace": "net:[54322]"},
            "lock": {"path": str(lock), "device": lock.stat().st_dev, "inode": lock.stat().st_ino}, "sources": source,
            "inputs": {key: {"path": str(Path(scope["inputRoot"]) / (key + ".json")), "sha256": "a" * 64} for key in ("credentials", "publicBaseline", "predecessor")},
            "scope": scope, "sealedRoots": [str(self.sealed)], "forbiddenOriginalRoots": [str(self.root / "forbidden"), str(self.root / "forbidden-data")],
            "admin": deepcopy(self.admin), "preservation": {"userIds": self.users, "libraryIds": self.libraries,
                "detailRoutes": [{"group": "admin", "userId": self.users[0], "itemId": "item-" + str(index)} for index in range(6)]},
            "budgets": {"requestSeconds": 5, "normalSeconds": 600, "cleanupSeconds": 60, "requestBytes": 32768, "responseBytes": 262144,
                        "totalResponseBytes": 16 * 1024 * 1024, "cleanupResponseBytes": 2 * 262145}}
        stamp = "2026-09-13T00:00:00+00:00"
        self.baseline = {"marker": "synthetic-reference-public-baseline", "version": 1, "captured_at": stamp,
            "server": {"Id": "synthetic-server", "Version": "synthetic-v1", "FullDocument": [1, 2, 3]}, "configuration": {"Complete": "configuration"},
            "roster": {user: {"Id": user, "Name": "Owned Admin" if index == 0 else "Old User " + str(index), "Policy": {"IsAdministrator": index == 0},
                               "Configuration": {"Keep": True}, "LastLoginDate": stamp, "LastActivityDate": stamp} for index, user in enumerate(self.users)},
            "libraries": {library: {"ItemId": library, "Name": library, "Locations": ["/owned/" + library], "LibraryOptions": {"Keep": True}} for library in self.libraries},
            "catalog_by_library": {library: {"item-" + str(index): {"Id": "item-" + str(index), "Name": "Old Item " + str(index)}} for index, library in enumerate(self.libraries)},
            "items_by_user": {user: {"item-" + str(index): {"Id": "item-" + str(index), "Name": "Old Item " + str(index)}} for index, user in enumerate(self.users)},
            "preferences": {user: {"Keep": user} for user in self.users},
            "details": {"admin": {"item-" + str(index): {"Id": "item-" + str(index), "Name": "Old Detail " + str(index)} for index in range(6)}},
            "devices": {"old-device-" + str(index): {"Id": "old-device-" + str(index), "ReportedDeviceId": "old-reported-" + str(index), "DateLastActivity": stamp,
                                                    "OldCompleteDocument": index} for index in range(85)},
            "credential_context": {"channel": "controller_api", "authenticated_user_id": self.users[0], "token_sha256": "b" * 64, "user_id_semantics": "subject_projection"}}
        old_admin = {**self.admin, "deviceId": "previous-preparation-device"}
        previous = self.device(old_admin, "986", "Goby NextUp Preparation", "Linux Fixture Recorder", "2026-09-13T00:00:30+00:00")
        self.previous_devices = {**deepcopy(self.baseline["devices"]), previous["Id"]: previous}
        self.closure = {"from": "2026-09-13T00:00:10+00:00", "through": "2026-09-13T00:00:40+00:00", "tokenSha256": self.module.digest(b"previous-token"), "ownedDevice": previous}
        self.closed_device_windows = {"986": {"from": self.closure["from"], "through": self.closure["through"], "userId": self.admin["userId"], "reportedDeviceId": old_admin["deviceId"]}}
        self.closed_token_hashes, self.closed_session_ids = {self.closure["tokenSha256"]}, {"previous-session"}
        if not hasattr(self.module.Authority, "_closed_observers"):
            self.manifest["schemaVersion"] = 1
            self.manifest.pop("closedObservers")
        self.authority = FakeAuthority(self)
        self.calls, self.response_mutator, self.journal_mutator = [], None, None
        self.bodies = self.make_bodies()

    def device(self, admin, key, client, name, stamp):
        return {"Id": key, "ReportedDeviceId": admin["deviceId"], "LastUserId": admin["userId"], "LastUserName": admin["username"],
                "AppName": client, "AppVersion": "1.0", "Name": name, "DateLastActivity": stamp}

    def envelope(self, rows):
        return {"Items": list(rows.values()), "TotalRecordCount": len(rows)}

    def make_bodies(self):
        bodies = {"server": deepcopy(self.baseline["server"]), "users": list(deepcopy(self.baseline["roster"]).values()),
                  "libraries": self.envelope(self.baseline["libraries"]), "configuration": deepcopy(self.baseline["configuration"])}
        for index, library in enumerate(sorted(self.libraries)): bodies["catalog-" + str(index)] = self.envelope(self.baseline["catalog_by_library"][library])
        for index, user in enumerate(sorted(self.users)):
            bodies["items-" + str(index)] = self.envelope(self.baseline["items_by_user"][user])
            bodies["prefs-" + str(index)] = deepcopy(self.baseline["preferences"][user])
        for index in range(6): bodies["detail-" + str(index)] = deepcopy(self.baseline["details"]["admin"]["item-" + str(index)])
        return bodies

    def login(self):
        return {"ServerId": self.manifest["server"]["id"], "AccessToken": "new-synthetic-observer-token", "User": {"Id": self.admin["userId"], "Name": self.admin["username"], "Policy": {"IsAdministrator": True}},
                "SessionInfo": {"Id": "new-synthetic-observer-session", "UserId": self.admin["userId"], "DeviceId": self.admin["deviceId"],
                                "ServerId": self.manifest["server"]["id"], "UserName": self.admin["username"], "InternalDeviceId": 987,
                                "Client": self.module.CLIENT, "DeviceName": self.module.DEVICE_NAME, "ApplicationVersion": "1.0"}}

    def send(self, request, headers, payload, **bounds):
        self.calls.append({"label": request.label, "method": request.method, "route": request.route, "headers": deepcopy(headers), "payload": payload, "bounds": bounds})
        status, body = 200, None
        if request.label == "login": body = self.login()
        elif request.label == "logout": status = 204
        elif request.label == "rejection": status, body = 401, "Unauthorized"
        elif request.label == "devices":
            devices = deepcopy(self.previous_devices)
            devices["986"]["DateLastActivity"] = self.closure["through"]
            devices["987"] = self.device(self.admin, "987", self.module.CLIENT, self.module.DEVICE_NAME, self.clock.utc())
            body = {"Items": list(devices.values()), "TotalRecordCount": 0}
        else:
            body = deepcopy(self.bodies[request.label])
            if request.label == "users":
                body[0]["LastLoginDate"] = body[0]["LastActivityDate"] = self.clock.utc()
        raw = b"" if body is None else encoded(body)
        response = {"status": status, "headers": [("Content-Type", "application/json; charset=utf-8"), ("Content-Length", str(len(raw)))], "raw": raw,
                    "complete_http": True, "completed_at": self.clock.utc(), "failure": None}
        if self.response_mutator: self.response_mutator(request, response)
        return SimpleNamespace(**response)

    def make_runner(self):
        def journal(root, uid):
            value = self.support.Journal(root, uid=uid)
            if self.journal_mutator: self.journal_mutator(value)
            return value
        self.runner = self.module.ObserverRunner(self.manifest, authority=self.authority, transport=SimpleNamespace(send=self.send), journal_factory=journal,
                                                 monotonic=self.clock, utc_now=self.clock.utc)
        return self.runner

    def run(self):
        return self.make_runner().run()

    def mutate_body(self, label, mutate):
        def change(request, response):
            if request.label == label:
                value = json.loads(response["raw"])
                mutate(value)
                response["raw"] = encoded(value)
                response["headers"][-1] = (response["headers"][-1][0], str(len(response["raw"])))
        self.response_mutator = change

    def history(self):
        """Write fresh synthetic counterparts of actual predecessor proof shapes."""
        def write(name, value):
            path = self.sealed / (name + ".json")
            raw = encoded(value); path.write_bytes(raw); path.chmod(0o600)
            return {"path": str(path), "sha256": self.module.digest(raw)}
        prior_admin = {**self.admin, "deviceId": "previous-preparation-device"}
        prior = {"runId": "synthetic-prior", "server": self.manifest["server"], "process": self.manifest["process"], "lock": self.manifest["lock"], "actors": {"admin": prior_admin}}
        token, session_id, plan_sha = "previous-token", "previous-session", "c" * 64
        state = {"runId": prior["runId"], "requestCount": 33, "normalRequestCount": 31, "cleanupRequestCount": 2, "uncertain": False,
                 "pending": None, "ownershipPending": None, "revoked": ["admin"], "userIds": {"admin": self.admin["userId"]},
                 "libraries": {}, "items": {}, "plays": {}, "touched": [], "calibrations": [], "tokens": {"admin": token}, "sessions": {"admin": session_id}, "planSha256": plan_sha}
        producer = {"runId": prior["runId"], "status": "stopped_with_known_cleanup", "cleanupComplete": True, "uncertain": False, "pending": None, "ownershipPending": None}
        closure = {**deepcopy(self.closure), "schemaVersion": 1, "kind": "synthetic-administrator-closure", "runId": prior["runId"],
                   "userId": self.admin["userId"], "username": self.admin["username"], "reportedDeviceId": prior_admin["deviceId"], "exactTokenClosed": True}
        login = self.login()
        login.update(AccessToken=token)
        login["SessionInfo"].update(Id=session_id, DeviceId=prior_admin["deviceId"], Client="Goby NextUp Preparation", DeviceName="Linux Fixture Recorder", InternalDeviceId=986)
        specifications = {"login": (1, "POST", "/emby/Users/AuthenticateByName", 200, login, "2026-09-13T00:00:20+00:00"),
            "deviceObservation": (31, "GET", "/emby/Devices", 200, {"Items": list(self.previous_devices.values()), "TotalRecordCount": 0}, "2026-09-13T00:00:31+00:00"),
            "logout": (32, "POST", "/emby/Sessions/Logout", 204, None, "2026-09-13T00:00:39+00:00"),
            "rejection": (33, "GET", "/emby/Sessions", 401, "Unauthorized", closure["through"])}
        for name, (ordinal, method, route, status, body, completed) in specifications.items():
            headers = {"Accept": "application/json", "Authorization": 'Emby Client="Goby NextUp Preparation", Device="Linux Fixture Recorder", DeviceId="' + prior_admin["deviceId"] + '", Version="1.0"'}
            request_body = None
            if name == "login":
                headers["Content-Type"] = "application/x-www-form-urlencoded; charset=utf-8"
                request_body = {"Username": self.admin["username"], "Pw": self.credentials["password"]}
            else: headers["X-Emby-Token"] = token
            request = {"method": method, "route": route, "headers": [[key, value] for key, value in headers.items()], "body": request_body}
            payload = None if request_body is None else base64.b64encode(urlencode(request_body).encode()).decode()
            intent = {"ordinal": ordinal, "label": name, "actor": "admin", "planSha256": plan_sha, "request": request,
                      "payloadBase64": payload, "tokenSha256": None if name == "login" else closure["tokenSha256"]}
            raw = b"" if body is None else encoded(body)
            wire = {"ordinal": ordinal, "label": name, "actor": "admin", "completedAt": completed, "request": request, "payloadBase64": payload,
                    "status": status, "headers": [["Content-Type", "application/json"], ["Content-Length", str(len(raw))]], "rawBase64": base64.b64encode(raw).decode(),
                    "completeHttp": True, "failure": None, "observedRawBytes": len(raw), "retainedRawTruncated": False}
            closure[name] = {"intent": write(name + "-intent", intent), "response": write(name + "-response", wire)}
        unit = {"name": "goby-synthetic-closed-preparation.service", "invocationId": "d" * 32,
                "properties": {"InvocationID": "d" * 32, "MainPID": "0", "ActiveState": "failed", "SubState": "failed", "Result": "exit-code", "ExecMainStatus": "2", "ControlGroup": ""},
                "cgroupPath": "/sys/fs/cgroup/system.slice/goby-synthetic-closed-preparation.service"}
        terminal = {"schemaVersion": 1, "kind": "nextup-global-failed-preparation-terminal", "status": "failed_preparation_independently_sealed_after_known_cleanup",
            "runId": prior["runId"], "capturedAt": "2026-09-13T00:00:50+00:00", "unit": unit,
            "manifest": write("prior-manifest", prior), "state": write("prior-state", state), "producerTerminal": write("prior-producer-terminal", producer),
            "authenticationClosure": write("prior-closure", closure), "requestCount": 33, "normalRequestCount": 31, "cleanupRequestCount": 2,
            "noLibraryCreation": True, "noUserCreation": True, "noPlayback": True, "noMetadataMutation": True, "old109RootsPreserved": True,
            "allWireRequestsComplete": True, "knownAdministratorClosed": True, "recursiveCgroupEmpty": True, "originalApplicationAndProxyMetadataUnchanged": True,
            "configurationObserved": False, "completeReferencePreservationClaimed": False, "originalImplementationBytesRead": False, "referenceDatabaseRead": False}
        self.manifest["inputs"]["predecessor"] = write("independent-terminal", terminal)
        self.manifest["inputs"]["publicBaseline"] = write("baseline", self.baseline)
        self.manifest["inputs"]["credentials"] = write("credentials", {"schemaVersion": 1, "runId": self.manifest["runId"], "admin": self.credentials})
        input_path = Path(self.manifest["scope"]["inputRoot"]) / "manifest.json"
        raw = encoded(self.manifest); input_path.write_bytes(raw); input_path.chmod(0o600)
        self.input_descriptor = {"path": str(input_path), "sha256": self.module.digest(raw)}
        self.history_records = {str(path): json.loads(path.read_bytes()) for path in self.sealed.glob("*.json")}
        self.history_terminal, self.history_closure = terminal, closure
        return self.input_descriptor

    def historical_authority(self):
        authority = object.__new__(self.module.Authority)
        authority.manifest, authority.baseline, authority.credentials = deepcopy(self.manifest), deepcopy(self.baseline), deepcopy(self.credentials)
        authority.predecessor = deepcopy(self.history_terminal)
        authority._record = lambda row: deepcopy(self.history_records[row["path"]])
        return authority

    def recovery_history(self, count=1):
        """Append private synthetic failed-login and exact-token recovery chains."""
        self.history()
        accumulated = deepcopy(self.previous_devices)
        windows = deepcopy(self.closed_device_windows)
        self.recovery_terminals, self.recovery_descriptors = [], []

        def write(name, value):
            path = self.sealed / (name + ".json")
            raw = encoded(value); path.write_bytes(raw); path.chmod(0o600)
            self.history_records[str(path)] = deepcopy(value)
            return {"path": str(path), "sha256": self.module.digest(raw)}

        for index in range(count):
            prefix = "recovered-" + str(index)
            second = 60 + index * 12
            base = datetime(2026, 9, 13, tzinfo=timezone.utc)
            stamp = lambda offset, micros=0: (base + timedelta(seconds=second + offset, microseconds=micros)).isoformat()
            metadata = {**self.admin, "deviceId": "recovered-observer-device-" + str(index)}
            token, session, internal_id = "recovered-token-" + str(index), "recovered-session-" + str(index), 990 + index
            previous = deepcopy(self.manifest)
            previous.update(runId=prefix + "-observer", admin=metadata)
            if index == 0:
                previous["schemaVersion"] = 1; previous.pop("closedObservers")
            else: previous["closedObservers"] = deepcopy(self.recovery_descriptors)
            previous["scope"]["outputRoot"] = str(self.sealed / (prefix + "-output"))
            plan_sha = self.module.digest(self.module.canonical(self.module.frozen_plan(previous, self.baseline)).encode())
            login = self.login()
            login.update(AccessToken=token)
            login["SessionInfo"].update(Id=session, DeviceId=metadata["deviceId"], InternalDeviceId=internal_id)
            authorization = 'Emby Client="' + self.module.CLIENT + '", Device="' + self.module.DEVICE_NAME + '", DeviceId="' + metadata["deviceId"] + '", Version="1.0"'

            def step(name, ordinal, method, route, status, body, completed, created, *, is_login=False):
                headers = {"Accept": "application/json", "Authorization": authorization}
                request_body = None
                if is_login:
                    headers["Content-Type"] = "application/x-www-form-urlencoded; charset=utf-8"
                    request_body = {"Username": metadata["username"], "Pw": self.credentials["password"]}
                else: headers["X-Emby-Token"] = token
                request = {"method": method, "route": route, "headers": [[key, value] for key, value in headers.items()], "body": request_body}
                payload = None if request_body is None else base64.b64encode(urlencode(request_body).encode()).decode()
                intent = {"ordinal": ordinal, "label": name, "actor": "admin", "planSha256": plan_sha, "request": request, "createdAt": created,
                          "payloadBase64": payload, "tokenSha256": None if is_login else self.module.digest(token.encode())}
                if not is_login:
                    intent.update(planSha256=recovery_plan_sha, manifestSha256=recovery_manifest_descriptor["sha256"])
                raw = b"" if body is None else encoded(body)
                wire = {"ordinal": ordinal, "label": name, "actor": "admin", "completedAt": completed, "request": request, "payloadBase64": payload, "status": status,
                        "headers": [["Content-Type", "application/json"], ["Content-Length", str(len(raw))]], "rawBase64": base64.b64encode(raw).decode(),
                        "completeHttp": True, "failure": None, "observedRawBytes": len(raw), "retainedRawTruncated": False}
                return {"intent": write(prefix + "-" + name + "-intent", intent), "response": write(prefix + "-" + name + "-response", wire)}, intent

            login_proof, login_intent = step("login", 1, "POST", "/emby/Users/AuthenticateByName", 200, login, stamp(0, 28000), stamp(0, 10000), is_login=True)
            pending = {"ordinal": 1, "label": "login", "intentSha256": login_proof["intent"]["sha256"], "request": login_intent["request"]}
            state = {"schemaVersion": 1, "runId": previous["runId"], "manifestSha256": self.module.digest(self.module.canonical(previous).encode()), "planSha256": plan_sha,
                     "requestCount": 1, "normalRequestCount": 1, "cleanupRequestCount": 0, "readIndex": 0, "uncertain": True, "token": None, "session": None, "closedToken": False,
                     "pending": pending, "ownershipPending": {"ordinal": 1, "responseReceiptSha256": login_proof["response"]["sha256"], "stage": "response-awaiting-owner"}}
            failed = {"runId": previous["runId"], "status": "recovery_required", "requestCount": 1, "normalRequestCount": 1, "cleanupRequestCount": 0, "uncertain": True,
                      "cleanupComplete": False, "completeSnapshotObserved": False, "baselineReleased": False, "outputs": {}}
            def unit(name, failed):
                invocation = (str(index + 1) + ("a" if failed else "b")) * 16
                return {"name": name, "invocationId": invocation, "properties": {"InvocationID": invocation, "MainPID": "0", "ActiveState": "failed" if failed else "active",
                        "SubState": "failed" if failed else "exited", "Result": "exit-code" if failed else "success", "ExecMainStatus": "2" if failed else "0", "ControlGroup": ""},
                        "cgroupPath": "/sys/fs/cgroup/system.slice/" + name}
            observer = {"manifest": write(prefix + "-manifest", previous), "state": write(prefix + "-state", state), "terminal": write(prefix + "-terminal", failed),
                        "login": login_proof, "unit": unit(prefix + "-observer.service", True)}
            recovery_manifest = {"schemaVersion": 1, "runId": prefix + "-recovery", "observerRunId": previous["runId"], "server": previous["server"],
                                 "process": previous["process"], "lock": previous["lock"], "admin": metadata, "observer": observer,
                                 "preparationAnchor": self.manifest["inputs"]["predecessor"]}
            recovery_manifest_descriptor = write(prefix + "-recovery-manifest", recovery_manifest)
            recovery_plan_sha = self.module.digest(self.module.canonical(recovery_manifest).encode())
            for device_id, window in windows.items():
                accumulated[device_id]["DateLastActivity"] = self.module.instant(window["through"]).replace(microsecond=0).isoformat()
            owned = self.device(metadata, str(internal_id), self.module.CLIENT, self.module.DEVICE_NAME, stamp(0))
            accumulated[owned["Id"]] = owned
            device_proof, _ = step("devices", 1, "GET", "/emby/Devices", 200, {"Items": list(deepcopy(accumulated).values()), "TotalRecordCount": 0}, stamp(5, 100000), stamp(5, 10000))
            logout_proof, _ = step("logout", 2, "POST", "/emby/Sessions/Logout", 204, None, stamp(6, 100000), stamp(6, 10000))
            rejection_proof, _ = step("rejection", 3, "GET", "/emby/Sessions", 401, "Unauthorized", stamp(7, 100000), stamp(7, 10000))
            recovery = {"manifest": recovery_manifest_descriptor, "deviceObservation": device_proof, "logout": logout_proof,
                        "rejection": rejection_proof, "unit": unit(prefix + "-recovery.service", False)}
            record = {"schemaVersion": 1, "kind": "nextup-baseline-observer-recovery-terminal", "status": "observer_login_independently_recovered_and_closed",
                "runId": recovery_manifest["runId"], "observerRunId": previous["runId"], "capturedAt": stamp(8), "observer": observer, "recovery": recovery,
                "userId": metadata["userId"], "username": metadata["username"], "reportedDeviceId": metadata["deviceId"], "tokenSha256": self.module.digest(token.encode()),
                "from": stamp(0, 10000), "through": stamp(7, 100000), "ownedDevice": deepcopy(owned), "requestCount": 3, "observerRequestCount": 1,
                "exactTokenClosed": True, "recursiveCgroupsEmpty": True, "referenceDatabaseRead": False, "originalImplementationBytesRead": False,
                "noNewLogin": True, "noMetadataMutation": True, "noLibraryCreation": True, "noUserCreation": True, "noPlayback": True}
            desc = write(prefix + "-independent-recovery", record)
            self.recovery_descriptors.append(desc); self.recovery_terminals.append(record)
            windows[owned["Id"]] = {"from": record["from"], "through": record["through"], "userId": metadata["userId"], "reportedDeviceId": metadata["deviceId"]}
        self.manifest["closedObservers"] = deepcopy(self.recovery_descriptors)
        input_path = Path(self.input_descriptor["path"])
        raw = encoded(self.manifest); input_path.write_bytes(raw)
        self.input_descriptor["sha256"] = self.module.digest(raw)
        return self.recovery_descriptors

    def recovered_authority(self):
        authority = self.historical_authority()
        authority._predecessor(); authority._closed_observers()
        return authority

    def use_recovered_authority(self, authority):
        self.previous_devices = deepcopy(authority.previous_devices)
        self.closed_device_windows = deepcopy(authority.closed_device_windows)
        self.closed_token_hashes, self.closed_session_ids = set(authority.closed_token_hashes), set(authority.closed_session_ids)
        self.authority = FakeAuthority(self)


class ObserverGuards(unittest.TestCase):
    def fixture(self): return Fixture(self)

    def test_complete_34_call_observation_and_actual_controller_adapters(self):
        f = self.fixture(); terminal = f.run()
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual((terminal["requestCount"], terminal["normalRequestCount"], terminal["cleanupRequestCount"]), (34, 32, 2))
        self.assertTrue(terminal["cleanupComplete"]); self.assertFalse(terminal["baselineReleased"])
        baseline = json.loads((f.output / "private/public-baseline.json").read_bytes())
        self.assertEqual(len(baseline["devices"]), 87)
        self.assertEqual(baseline["configuration"], f.baseline["configuration"])
        self.assertEqual(baseline["credential_context"]["token_sha256"], f.module.digest(f.login()["AccessToken"].encode()))
        closure = json.loads((f.output / "private/closed-authentication.json").read_bytes())
        for label, status in (("logout", 204), ("rejection", 401)):
            proof = closure[label]
            result = json.loads(Path(proof["record"]["path"]).read_bytes())
            intent = json.loads(Path(proof["intent"]["path"]).read_bytes())
            wire = json.loads(Path(result["wire_response"]["path"]).read_bytes())
            self.assertEqual(f.module.digest(Path(result["wire_response"]["path"]).read_bytes()), result["wire_response"]["sha256"])
            self.assertEqual(result["body"], f.module.decode_wire(wire)); self.assertEqual(result["status"], status)
            self.assertEqual(result["completed_at"], wire["completedAt"])
            self.assertEqual(result["token_sha256"], closure["tokenSha256"])
            self.assertEqual(intent["path"], wire["request"]["route"])
        self.assertEqual(closure["from"], baseline["captured_at"])
        self.assertEqual(closure["through"], f.runner.responses["rejection"]["completedAt"])
        self.assertEqual([call["method"] for call in f.calls].count("POST"), 2)
        self.assertTrue(f.authority.closed)

    def test_frozen_transport_tuple_headers_are_normalized_without_loss(self):
        f = self.fixture()
        runner = f.make_runner()
        runner.transport = f.support.HTTPTransport(f.manifest["endpoint"])
        connections, expected_headers, parsed_headers = [], [], []
        plan, case = runner.plan["requests"], self

        class ResponseSocket:
            def __init__(self, connection, planned):
                self.connection, self.planned = connection, planned
                self.request_bytes, self.closed = bytearray(), False

            def sendall(self, data):
                self.request_bytes.extend(data)

            def makefile(self, mode):
                case.assertEqual(mode, "rb")
                head, separator, payload = bytes(self.request_bytes).partition(b"\r\n\r\n")
                case.assertEqual(separator, b"\r\n\r\n")
                lines = head.decode("iso-8859-1").split("\r\n")
                method, route, version = lines[0].split(" ", 2)
                case.assertEqual(version, "HTTP/1.1")
                case.assertEqual((method, route), (self.planned["method"], self.planned["route"]))
                headers = {}
                for line in lines[1:]:
                    key, separator, value = line.partition(":")
                    case.assertEqual(separator, ":")
                    headers[key] = value.strip()
                if "Content-Length" in headers: case.assertEqual(int(headers["Content-Length"]), len(payload))
                request = SimpleNamespace(method=method, route=route, label=self.planned["label"], actor="admin", cleanup=self.planned["label"] in ("logout", "rejection"))
                response = f.send(request, headers, payload or None, timeout_seconds=self.connection.timeout, max_bytes=f.manifest["budgets"]["responseBytes"] + 1)
                pairs = [("X-Observer-Order", "first"), *(tuple(pair) for pair in response.headers), ("X-Observer-Order", "second")]
                expected_headers.append(pairs)
                reason = {200: "OK", 204: "No Content", 401: "Unauthorized"}[response.status]
                response_head = f"HTTP/1.1 {response.status} {reason}\r\n" + "".join(f"{key}: {value}\r\n" for key, value in pairs) + "\r\n"
                return io.BytesIO(response_head.encode("iso-8859-1") + response.raw)

            def close(self):
                self.closed = True

        class ObservedHTTPResponse(f.support.http.client.HTTPResponse):
            def getheaders(self):
                pairs = super().getheaders()
                parsed_headers.append(pairs)
                return pairs

        def connect(connection):
            case.assertLess(len(connections), len(plan))
            synthetic_socket = ResponseSocket(connection, plan[len(connections)])
            connections.append(synthetic_socket)
            connection.sock = synthetic_socket

        with patch.object(socket.socket, "connect", side_effect=AssertionError("Real HTTP is forbidden.")), \
             patch.object(f.support.http.client.HTTPConnection, "connect", connect), \
             patch.object(f.support.http.client.HTTPConnection, "response_class", ObservedHTTPResponse), \
             patch.object(f.support, "utc_now", f.clock.utc):
            terminal = runner.run()
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual((terminal["requestCount"], terminal["normalRequestCount"], terminal["cleanupRequestCount"]), (34, 32, 2))
        self.assertTrue(terminal["cleanupComplete"]); self.assertFalse(terminal["baselineReleased"])
        self.assertEqual(len(connections), 34); self.assertEqual(len(parsed_headers), 34)
        self.assertEqual(parsed_headers, expected_headers)
        self.assertTrue(all(connection.closed for connection in connections))
        for ordinal, (planned, pairs) in enumerate(zip(plan, parsed_headers), start=1):
            self.assertIs(type(pairs), list)
            self.assertTrue(all(type(pair) is tuple for pair in pairs))
            self.assertEqual(pairs[0], ("X-Observer-Order", "first")); self.assertEqual(pairs[-1], ("X-Observer-Order", "second"))
            label = planned["label"]
            wire = json.loads((f.output / "private" / f"{ordinal:04d}-{label}-response.json").read_bytes())
            self.assertEqual(wire["headers"], [[key, value] for key, value in pairs])
            self.assertEqual(f.module.decode_wire(wire), runner.responses[label]["body"])

    def test_same_runner_cannot_resume(self):
        f = self.fixture(); f.run()
        with self.assertRaises(f.module.ObservationError): f.runner.run()
        self.assertEqual(len(f.calls), 34)

    def test_existing_output_is_not_reused(self):
        f = self.fixture(); f.output.mkdir(mode=0o700)
        result = f.run()
        self.assertEqual(len(f.calls), 0); self.assertFalse(result["baselineReleased"])

    def test_known_get_failure_only_logs_out_and_rejects(self):
        f = self.fixture()
        def mutate(request, response):
            if request.label == "server": response["status"] = 503
        f.response_mutator = mutate
        result = f.run()
        self.assertEqual([row["label"] for row in f.calls], ["login", "server", "logout", "rejection"])
        self.assertEqual(result["status"], "stopped_with_known_cleanup")
        self.assertTrue(result["cleanupComplete"])

    def test_unacknowledged_login_is_unknown_and_never_logs_out(self):
        f = self.fixture()
        f.response_mutator = lambda request, response: response.update(status=401) if request.label == "login" else None
        result = f.run()
        self.assertEqual(result["status"], "recovery_required"); self.assertEqual(len(f.calls), 1)
        self.assertIsNotNone(f.runner.ownership_pending)

    def test_transport_exception_never_retries_or_cleans_up(self):
        f = self.fixture()
        def fail(request, response): raise TimeoutError("Synthetic lost response.")
        f.response_mutator = fail
        result = f.run()
        self.assertEqual(result["status"], "recovery_required"); self.assertEqual(len(f.calls), 1)
        self.assertIsNotNone(f.runner.pending)

    def test_logout_non204_is_not_retried(self):
        f = self.fixture()
        f.response_mutator = lambda request, response: response.update(status=500) if request.label == "logout" else None
        result = f.run()
        self.assertEqual(len(f.calls), 33); self.assertEqual(result["status"], "recovery_required")
        self.assertEqual(f.calls[-1]["label"], "logout")

    def test_same_token_rejection_is_required(self):
        f = self.fixture()
        f.response_mutator = lambda request, response: response.update(status=200) if request.label == "rejection" else None
        result = f.run()
        self.assertEqual(len(f.calls), 34); self.assertEqual(result["status"], "recovery_required")
        self.assertFalse(result["cleanupComplete"])

    def test_route_mutation_is_rejected_before_dispatch(self):
        f = self.fixture(); runner = f.make_runner()
        runner.reads[0] = ("server", "/emby/Library/Refresh")
        result = runner.run()
        self.assertNotEqual(result["status"], "awaiting_independent_attestation")
        self.assertNotIn("/emby/Library/Refresh", [row["route"] for row in f.calls])

    def test_missing_configuration_never_becomes_complete_snapshot(self):
        f = self.fixture(); f.bodies["configuration"] = None
        result = f.run()
        self.assertEqual(result["status"], "stopped_with_known_cleanup")
        self.assertFalse(result["completeSnapshotObserved"])

    def test_response_byte_reserve_stops_normal_work_before_cleanup(self):
        f = self.fixture(); f.manifest["budgets"]["totalResponseBytes"] = f.manifest["budgets"]["cleanupResponseBytes"] + f.manifest["budgets"]["responseBytes"] + 1
        result = f.run()
        self.assertLess(len(f.calls), 34); self.assertTrue(result["cleanupComplete"])

    def test_public_plan_is_exact_and_bounded(self):
        f = self.fixture(); plan = f.module.frozen_plan(f.manifest, f.baseline)
        self.assertEqual(len(plan["requests"]), 34)
        self.assertEqual(sum(row["method"] == "GET" for row in plan["requests"]), 32)
        self.assertEqual(plan["requests"][-3]["route"], "/emby/System/Configuration")
        self.assertFalse(plan["createsMediaLibrariesUsersOrPlayback"])

    def test_devices_zero_count_is_specific_to_devices(self):
        f = self.fixture(); envelope = {"Items": [{"Id": "old-device", "ReportedDeviceId": "old-reported"}], "TotalRecordCount": 0}
        self.assertEqual(set(f.module.page(envelope, devices=True)), {"old-device"})
        with self.assertRaises(f.module.ObservationError): f.module.page(envelope)

    def test_device_count_and_duplicate_shapes_rejected(self):
        f = self.fixture()
        for envelope in ({"Items": [], "TotalRecordCount": True}, {"Items": [{"Id": "d", "ReportedDeviceId": "r"}], "TotalRecordCount": 2},
                         {"Items": [{"Id": "d", "ReportedDeviceId": "r"}, {"Id": "e", "ReportedDeviceId": "r"}], "TotalRecordCount": 2},
                         {"Items": [None], "TotalRecordCount": 0}):
            with self.subTest(envelope=envelope), self.assertRaises(f.module.ObservationError): f.module.page(envelope, devices=True)

    def test_device_whole_second_can_precede_login_acknowledgment(self):
        f = self.fixture()
        f.mutate_body("devices", lambda body: body["Items"][-1].update(DateLastActivity="2026-09-13T00:01:40.0000000+00:00"))
        terminal = f.run()
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertLess(f.module.instant("2026-09-13T00:01:40Z"), f.module.instant(f.runner.login["completedAt"]))

    def test_device_time_uses_only_observed_whole_second_quantization(self):
        f = self.fixture()
        cases = [("2026-09-13T00:00:40.0000000Z", True), ("2026-09-13T00:00:39.0000000Z", False),
                 ("2026-09-13T00:00:40.0100000Z", False), ("2026-09-13T00:00:40.0500000Z", True),
                 ("2026-09-13T00:00:41.1000001Z", False), ("2026-09-13T00:00:42Z", False)]
        for value, valid in cases:
            with self.subTest(value=value):
                if valid: f.module.device_time_within(value, "2026-09-13T00:00:40.020000Z", "2026-09-13T00:00:41.100000Z")
                else:
                    with self.assertRaises(f.module.ObservationError): f.module.device_time_within(value, "2026-09-13T00:00:40.020000Z", "2026-09-13T00:00:41.100000Z")

    def test_actual_authority_reads_and_pins_complete_synthetic_predecessor(self):
        f = self.fixture(); descriptor = f.history()
        with patch.object(f.module, "__file__", f.manifest["sources"]["observer"]["path"]):
            authority = f.module.Authority(f.manifest, descriptor)
        self.assertEqual(len(authority.previous_devices), 86)
        self.assertGreaterEqual(len(authority.pins), 15)
        with patch.object(authority.support, "process_identity", return_value=f.manifest["process"]), patch.object(f.module.subprocess, "run") as run:
            run.return_value = SimpleNamespace(stdout="".join(key + "=" + value + "\n" for key, value in authority.unit["properties"].items()))
            authority.acquire(); authority.check()
            path = Path(f.manifest["inputs"]["credentials"]["path"])
            path.write_bytes(path.read_bytes() + b" ")
            with self.assertRaises(f.module.ObservationError): authority.check()
        authority.close()

    def test_actual_authority_rejects_nonprivate_input(self):
        f = self.fixture(); descriptor = f.history()
        Path(f.manifest["inputs"]["credentials"]["path"]).chmod(0o644)
        with patch.object(f.module, "__file__", f.manifest["sources"]["observer"]["path"]), self.assertRaises(f.module.ObservationError):
            f.module.Authority(f.manifest, descriptor)

    def test_actual_authority_rejects_conflicting_manifest_pin(self):
        f = self.fixture(); descriptor = f.history(); descriptor["sha256"] = "0" * 64
        with patch.object(f.module, "__file__", f.manifest["sources"]["observer"]["path"]), self.assertRaises(f.module.ObservationError):
            f.module.Authority(f.manifest, descriptor)

    def test_actual_authority_rejects_replaced_lock(self):
        f = self.fixture(); descriptor = f.history()
        with patch.object(f.module, "__file__", f.manifest["sources"]["observer"]["path"]): authority = f.module.Authority(f.manifest, descriptor)
        lock = Path(f.manifest["lock"]["path"])
        lock.rename(lock.with_name("old-lock")); lock.write_bytes(b""); lock.chmod(0o600)
        with self.assertRaises(f.module.ObservationError): authority.acquire()
        authority.close()

    def test_actual_authority_rejects_changed_source(self):
        f = self.fixture(); descriptor = f.history()
        source = Path(f.manifest["sources"]["proxy"]["path"])
        source.write_bytes(source.read_bytes() + b"# Changed owned source.\n")
        with patch.object(f.module, "__file__", f.manifest["sources"]["observer"]["path"]), self.assertRaises(f.module.ObservationError):
            f.module.Authority(f.manifest, descriptor)

    def test_historical_plaintext_401_only_when_not_declared_json(self):
        f = self.fixture(); f.history()
        wire = deepcopy(f.history_records[f.history_closure["rejection"]["response"]["path"]])
        raw = b"Unauthorized"
        wire.update(rawBase64=base64.b64encode(raw).decode(), observedRawBytes=len(raw), headers=[["Content-Type", "text/plain; charset=utf-8"], ["Content-Length", str(len(raw))]])
        self.assertEqual(f.module.decode_wire(wire), "Unauthorized")
        wire["headers"][0][1] = "application/json"
        with self.assertRaises(f.module.ObservationError): f.module.decode_wire(wire)

    def test_historical_response_header_boundary(self):
        f = self.fixture(); f.history()
        original = f.history_records[f.history_closure["deviceObservation"]["response"]["path"]]
        for headers in ([*[deepcopy(pair) for pair in original["headers"]], ["Content-Type", "text/plain"]],
                        [["Content-Type", "application/json; charset=utf-16"], *original["headers"][1:]]):
            wire = deepcopy(original); wire["headers"] = headers
            with self.subTest(headers=headers), self.assertRaises(f.module.ObservationError): f.module.decode_wire(wire)

    def test_default_fake_wire_uses_real_transport_tuple_pairs(self):
        f = self.fixture()
        response = f.send(SimpleNamespace(label="login", method="POST", route="/emby/Users/AuthenticateByName"), {}, None)
        self.assertTrue(all(type(pair) is tuple for pair in response.headers))

    def test_one_recovered_observer_yields_88_actual_devices_with_34_requests(self):
        f = self.fixture(); f.recovery_history(1)
        authority = f.recovered_authority()
        self.assertEqual(len(authority.previous_devices), 87)
        self.assertEqual(len(authority.closed_device_windows), 2)
        self.assertEqual(len(authority.units), 3)
        f.use_recovered_authority(authority)
        terminal = f.run()
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual(terminal["requestCount"], 34)
        baseline = json.loads((f.output / "private/public-baseline.json").read_bytes())
        self.assertEqual(len(baseline["devices"]), 88)
        self.assertEqual(baseline["devices"]["990"]["ReportedDeviceId"], "recovered-observer-device-0")
        self.assertFalse(terminal["baselineReleased"])

    def test_two_recovered_observers_preserve_ordered_device_chain(self):
        f = self.fixture(); f.recovery_history(2)
        authority = f.recovered_authority()
        self.assertEqual(len(authority.previous_devices), 88)
        self.assertEqual(len(authority.closed_device_windows), 3)
        self.assertEqual(len(authority.units), 5)
        f.use_recovered_authority(authority)
        terminal = f.run()
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual(terminal["requestCount"], 34)
        self.assertEqual(len(json.loads((f.output / "private/public-baseline.json").read_bytes())["devices"]), 89)

    def test_recovery_chain_rejects_reordered_records(self):
        f = self.fixture(); f.recovery_history(2)
        f.manifest["closedObservers"].reverse()
        with self.assertRaises(f.module.ObservationError): f.recovered_authority()

    def test_recovery_chain_rejects_duplicate_manifest_records(self):
        f = self.fixture(); f.recovery_history(1)
        f.manifest["closedObservers"] *= 2
        with self.assertRaises(f.module.ObservationError): f.module.validate_manifest(f.manifest)

    def test_recovery_chain_retains_original_failed_state(self):
        f = self.fixture(); f.recovery_history(1)
        source = f.recovery_terminals[0]["observer"]["state"]["path"]
        retained = Path(source).read_bytes()
        f.recovered_authority()
        self.assertEqual(Path(source).read_bytes(), retained)
        self.assertIsNone(json.loads(retained)["token"])
        self.assertTrue(json.loads(retained)["uncertain"])

    def test_recovered_observer_timestamp_uses_original_request_second(self):
        f = self.fixture(); f.recovery_history(1)
        authority = f.recovered_authority()
        row = authority.previous_devices["990"]
        self.assertLess(f.module.instant(row["DateLastActivity"]), f.module.instant(authority.closed_device_windows["990"]["from"]))

    def test_recovered_device_followup_activity_cannot_exceed_proven_close(self):
        f = self.fixture(); f.recovery_history(1); f.use_recovered_authority(f.recovered_authority())
        f.mutate_body("devices", lambda body: next(row for row in body["Items"] if row["Id"] == "990").update(DateLastActivity="2026-09-13T00:01:39Z"))
        terminal = f.run()
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertTrue(terminal["cleanupComplete"])

    def test_recovered_token_cannot_be_reused_by_new_observer(self):
        f = self.fixture(); f.recovery_history(1); f.use_recovered_authority(f.recovered_authority())
        f.mutate_body("login", lambda body: body.update(AccessToken="recovered-token-0"))
        terminal = f.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(terminal["requestCount"], 1)

    def test_recovered_session_cannot_be_reused_by_new_observer(self):
        f = self.fixture(); f.recovery_history(1); f.use_recovered_authority(f.recovered_authority())
        f.mutate_body("login", lambda body: body["SessionInfo"].update(Id="recovered-session-0"))
        terminal = f.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(terminal["requestCount"], 1)

    def test_actual_authority_pins_all_recovery_inputs_and_both_closed_units(self):
        f = self.fixture(); f.recovery_history(1)
        with patch.object(f.module, "__file__", f.manifest["sources"]["observer"]["path"]):
            authority = f.module.Authority(f.manifest, f.input_descriptor)
        self.assertEqual(len(authority.units), 3)
        self.assertEqual(len(authority.previous_devices), 87)
        expected_units = {unit["name"]: unit for unit in authority.units}
        def show(arguments, **kwargs):
            self.assertEqual(arguments[:2], ["systemctl", "show"])
            return SimpleNamespace(stdout="".join(key + "=" + value + "\n" for key, value in expected_units[arguments[2]]["properties"].items()))
        with patch.object(authority.support, "process_identity", return_value=f.manifest["process"]), patch.object(f.module.subprocess, "run", side_effect=show) as run:
            authority.acquire()
            self.assertEqual(run.call_count, 3)
            record = f.recovery_terminals[0]["recovery"]["rejection"]["response"]
            path = Path(record["path"]); path.write_bytes(path.read_bytes() + b" ")
            with self.assertRaises(f.module.ObservationError): authority.check()
        authority.close()

    def test_closed_unit_requires_explicit_live_control_group(self):
        f = self.fixture(); f.recovery_history(1)
        for failed, source in ((True, f.history_terminal["unit"]), (True, f.recovery_terminals[0]["observer"]["unit"]),
                               (False, f.recovery_terminals[0]["recovery"]["unit"])):
            unit = deepcopy(source); unit["properties"].pop("ControlGroup")
            with self.subTest(failed=failed, name=unit["name"]), self.assertRaises(f.module.ObservationError):
                f.module.validate_closed_unit(unit, failed=failed)

    def test_closed_unit_rejects_another_slice(self):
        f = self.fixture(); f.recovery_history(1)
        unit = deepcopy(f.recovery_terminals[0]["recovery"]["unit"])
        unit["properties"]["ControlGroup"] = "/another.slice/" + unit["name"]
        with self.assertRaises(f.module.ObservationError): f.module.validate_closed_unit(unit, failed=False)

    def test_closed_device_rejects_submicrosecond_activity_rollback(self):
        f = self.fixture()
        old = {"device": {"Id": "device", "ReportedDeviceId": "reported", "LastUserId": "admin", "DateLastActivity": "2026-09-13T00:00:45.1234567Z"}}
        current = deepcopy(old); current["device"]["DateLastActivity"] = "2026-09-13T00:00:45.1234561Z"
        windows = {"device": {"userId": "admin", "reportedDeviceId": "reported", "from": "2026-09-13T00:00:40Z", "through": "2026-09-13T00:00:50Z"}}
        with self.assertRaises(f.module.ObservationError): f.module.preserve_closed_devices(old, current, windows)

    def test_closed_device_accepts_exact_submicrosecond_forward_activity(self):
        f = self.fixture()
        old = {"device": {"Id": "device", "ReportedDeviceId": "reported", "LastUserId": "admin", "DateLastActivity": "2026-09-13T00:00:45.1234561Z"}}
        current = deepcopy(old); current["device"]["DateLastActivity"] = "2026-09-13T00:00:45.1234567Z"
        windows = {"device": {"userId": "admin", "reportedDeviceId": "reported", "from": "2026-09-13T00:00:40Z", "through": "2026-09-13T00:00:50Z"}}
        self.assertEqual(len(f.module.preserve_closed_devices(old, current, windows)), 1)

    def test_recovery_chain_cannot_precede_predecessor_seal(self):
        f = self.fixture(); f.recovery_history(1)
        f.history_terminal["capturedAt"] = "2026-09-13T00:01:01Z"
        with self.assertRaises(f.module.ObservationError): f.recovered_authority()

    def test_recovery_request_cannot_precede_previous_response(self):
        f = self.fixture(); f.recovery_history(1)
        desc = f.recovery_terminals[0]["recovery"]["logout"]["intent"]
        f.history_records[desc["path"]]["createdAt"] = "2026-09-13T00:01:05.000000Z"
        with self.assertRaises(f.module.ObservationError): f.recovered_authority()

    def test_actual_authority_queries_control_group_and_rejects_live_drift(self):
        f = self.fixture(); descriptor = f.history()
        with patch.object(f.module, "__file__", f.manifest["sources"]["observer"]["path"]):
            authority = f.module.Authority(f.manifest, descriptor)
        properties = deepcopy(authority.unit["properties"])
        properties["ControlGroup"] = "/another.slice/" + authority.unit["name"]
        def show(arguments, **kwargs):
            self.assertIn("--property=ControlGroup", arguments)
            return SimpleNamespace(stdout="".join(key + "=" + value + "\n" for key, value in properties.items()))
        with patch.object(authority.support, "process_identity", return_value=f.manifest["process"]), patch.object(f.module.subprocess, "run", side_effect=show):
            with self.assertRaises(f.module.ObservationError): authority.acquire()
        authority.close()


def body_mutation_test(label, mutate, expected_calls=34, uncertain=False):
    def test(self):
        f = self.fixture(); f.mutate_body(label, mutate); terminal = f.run()
        self.assertNotEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual(len(f.calls), expected_calls)
        self.assertEqual(terminal["status"] == "recovery_required", uncertain)
        if uncertain: self.assertFalse(terminal["cleanupComplete"])
        else: self.assertTrue(terminal["cleanupComplete"])
    return test


LOGIN_MUTATIONS = {
    "null_user": lambda body: body.update(User=None),
    "null_policy": lambda body: body["User"].update(Policy=None),
    "list_session": lambda body: body.update(SessionInfo=[]),
    "wrong_server": lambda body: body.update(ServerId="another-server"),
    "wrong_user": lambda body: body["User"].update(Id="another-user"),
    "wrong_username": lambda body: body["User"].update(Name="Another Admin"),
    "wrong_device": lambda body: body["SessionInfo"].update(DeviceId="another-device"),
    "wrong_session_user": lambda body: body["SessionInfo"].update(UserId="another-user"),
    "empty_token": lambda body: body.update(AccessToken=""),
    "nonadmin": lambda body: body["User"]["Policy"].update(IsAdministrator=False),
    "wrong_client": lambda body: body["SessionInfo"].update(Client="Another Client"),
    "missing_client": lambda body: body["SessionInfo"].pop("Client"),
    "wrong_session_server": lambda body: body["SessionInfo"].update(ServerId="another-server"),
    "wrong_session_username": lambda body: body["SessionInfo"].update(UserName="Another Admin"),
    "missing_internal_device": lambda body: body["SessionInfo"].pop("InternalDeviceId"),
    "reused_closed_token": lambda body: body.update(AccessToken="previous-token"),
}
for name, mutate in LOGIN_MUTATIONS.items():
    setattr(ObserverGuards, "test_login_" + name, body_mutation_test("login", mutate, 1, True))

PRESERVATION_MUTATIONS = {
    "server": ("server", lambda body: body.update(FullDocument=[3, 2, 1])),
    "configuration": ("configuration", lambda body: body.update(Complete="changed")),
    "library": ("libraries", lambda body: body["Items"][0].update(Name="changed")),
    "catalog": ("catalog-0", lambda body: body["Items"][0].update(Name="changed")),
    "catalog_wrong_count": ("catalog-0", lambda body: body.update(TotalRecordCount=0)),
    "subject": ("items-0", lambda body: body["Items"][0].update(Name="changed")),
    "preferences": ("prefs-0", lambda body: body.update(Keep="changed")),
    "detail": ("detail-0", lambda body: body.update(Name="changed")),
    "old_user": ("users", lambda body: body[1]["Configuration"].update(Keep=False)),
    "admin_policy": ("users", lambda body: body[0]["Policy"].update(IsAdministrator=False)),
    "admin_date_outside": ("users", lambda body: body[0].update(LastActivityDate="2099-01-01T00:00:00Z")),
    "old_device": ("devices", lambda body: body["Items"][0].update(OldCompleteDocument="changed")),
    "prior_device_structure": ("devices", lambda body: body["Items"][-2].update(Name="changed")),
    "prior_device_future": ("devices", lambda body: body["Items"][-2].update(DateLastActivity="2099-01-01T00:00:00Z")),
    "new_device_owner": ("devices", lambda body: body["Items"][-1].update(LastUserId="another-user")),
    "new_device_client": ("devices", lambda body: body["Items"][-1].update(AppName="Another Client")),
    "new_device_future": ("devices", lambda body: body["Items"][-1].update(DateLastActivity="2099-01-01T00:00:00Z")),
    "new_device_previous_second": ("devices", lambda body: body["Items"][-1].update(DateLastActivity="2026-09-13T00:01:39.0000000Z")),
    "new_device_internal_id": ("devices", lambda body: body["Items"][-1].update(Id="988")),
    "missing_device": ("devices", lambda body: body["Items"].pop()),
    "extra_device": ("devices", lambda body: body["Items"].append({"Id": "unowned", "ReportedDeviceId": "unowned-reported"})),
}
for name, (label, mutate) in PRESERVATION_MUTATIONS.items():
    setattr(ObserverGuards, "test_preservation_" + name, body_mutation_test(label, mutate))


def historical_mutation_test(target, mutate):
    def test(self):
        f = self.fixture(); f.history()
        authority = f.historical_authority()
        if target == "terminal": mutate(authority.predecessor)
        elif target in ("state", "producerTerminal", "manifest", "authenticationClosure"):
            mutate(f.history_records[f.history_terminal[target]["path"]])
        else:
            step, kind = target.split("/")
            mutate(f.history_records[f.history_closure[step][kind]["path"]])
        with self.assertRaises((f.module.ObservationError, KeyError, TypeError, ValueError)):
            authority._predecessor()
    return test


def mutate_historical_body(wire, mutate):
    body = json.loads(base64.b64decode(wire["rawBase64"]))
    mutate(body)
    raw = encoded(body)
    wire.update(rawBase64=base64.b64encode(raw).decode(), observedRawBytes=len(raw))
    for pair in wire["headers"]:
        if pair[0].lower() == "content-length": pair[1] = str(len(raw))


HISTORICAL_MUTATIONS = {
    "configuration_unproven": ("terminal", lambda row: row.update(configurationObserved=True)),
    "wrong_request_count": ("terminal", lambda row: row.update(requestCount=34)),
    "not_closed": ("terminal", lambda row: row.update(knownAdministratorClosed=False)),
    "no_empty_cgroup": ("terminal", lambda row: row.update(recursiveCgroupEmpty=False)),
    "mutation": ("terminal", lambda row: row.update(noMetadataMutation=False)),
    "state_pending": ("state", lambda row: row.update(pending={"ordinal": 1})),
    "state_owner_pending": ("state", lambda row: row.update(ownershipPending={"stage": "response-awaiting-owner"})),
    "state_new_library": ("state", lambda row: row.update(libraries={"unowned": {}})),
    "state_unrevoked": ("state", lambda row: row.update(revoked=[])),
    "state_other_session": ("state", lambda row: row.update(sessions={"admin": "other-session"})),
    "producer_not_clean": ("producerTerminal", lambda row: row.update(cleanupComplete=False)),
    "manifest_other_process": ("manifest", lambda row: row["process"]["application"].update(pid=55555)),
    "window_before_login": ("authenticationClosure", lambda row: row.update(through="2026-09-13T00:00:15Z")),
    "logout_wrong_status": ("logout/response", lambda row: row.update(status=200)),
    "logout_other_token": ("logout/response", lambda row: row["request"]["headers"][-1].__setitem__(1, "another-token")),
    "logout_incomplete": ("logout/response", lambda row: row.update(completeHttp=False)),
    "rejection_wrong_route": ("rejection/intent", lambda row: row["request"].update(route="/emby/Users")),
    "rejection_no_401": ("rejection/response", lambda row: row.update(status=200)),
    "login_wrong_server": ("login/response", lambda row: mutate_historical_body(row, lambda body: body.update(ServerId="another-server"))),
    "login_wrong_internal_device": ("login/response", lambda row: mutate_historical_body(row, lambda body: body["SessionInfo"].update(InternalDeviceId=999))),
    "devices_old_field": ("deviceObservation/response", lambda row: mutate_historical_body(row, lambda body: body["Items"][0].update(OldCompleteDocument="changed"))),
    "devices_unowned": ("deviceObservation/response", lambda row: mutate_historical_body(row, lambda body: body["Items"].append({"Id": "unowned", "ReportedDeviceId": "unowned-reported"}))),
    "devices_incomplete": ("deviceObservation/response", lambda row: mutate_historical_body(row, lambda body: body["Items"].pop())),
}
for name, (target, mutate) in HISTORICAL_MUTATIONS.items():
    setattr(ObserverGuards, "test_predecessor_" + name, historical_mutation_test(target, mutate))


def recovered_mutation_test(target, mutate):
    def test(self):
        f = self.fixture(); f.recovery_history(1)
        record = f.history_records[f.recovery_descriptors[0]["path"]]
        if target == "record": mutate(record)
        elif target.startswith("observer/"):
            keys = target.split("/")[1:]
            descriptor = record["observer"]
            for key in keys: descriptor = descriptor[key]
            mutate(f.history_records[descriptor["path"]])
        else:
            keys = target.split("/")[1:]
            descriptor = record["recovery"]
            for key in keys: descriptor = descriptor[key]
            mutate(f.history_records[descriptor["path"]])
        with self.assertRaises((f.module.ObservationError, KeyError, TypeError, ValueError)):
            f.recovered_authority()
    return test


RECOVERED_MUTATIONS = {
    "not_closed": ("record", lambda row: row.update(exactTokenClosed=False)),
    "wrong_request_budget": ("record", lambda row: row.update(requestCount=4)),
    "bool_observer_count": ("record", lambda row: row.update(observerRequestCount=True)),
    "new_login_allowed": ("record", lambda row: row.update(noNewLogin=False)),
    "wrong_reported_device": ("record", lambda row: row.update(reportedDeviceId="another-device")),
    "wrong_token_hash": ("record", lambda row: row.update(tokenSha256="f" * 64)),
    "wrong_start": ("record", lambda row: row.update(**{"from": "2026-09-13T00:00:45Z"})),
    "wrong_end": ("record", lambda row: row.update(through="2026-09-13T00:00:51Z")),
    "wrong_original_unit": ("record", lambda row: row["observer"]["unit"]["properties"].update(MainPID="123")),
    "wrong_recovery_unit": ("record", lambda row: row["recovery"]["unit"]["properties"].update(ExecMainStatus="1")),
    "duplicate_unit": ("record", lambda row: row["recovery"].update(unit=deepcopy(row["observer"]["unit"]))),
    "old_state_marked_clean": ("observer/state", lambda row: row.update(uncertain=False)),
    "old_state_changed_owner": ("observer/state", lambda row: row.update(token="claimed-token")),
    "old_state_lost_pending": ("observer/state", lambda row: row.update(pending=None)),
    "old_state_wrong_plan": ("observer/state", lambda row: row.update(planSha256="e" * 64)),
    "old_state_bool_count": ("observer/state", lambda row: row.update(requestCount=True)),
    "old_terminal_changed_success": ("observer/terminal", lambda row: row.update(status="awaiting_independent_attestation")),
    "old_manifest_other_proxy": ("observer/manifest", lambda row: row["sources"]["proxy"].update(sha256="f" * 64)),
    "old_login_wrong_principal": ("observer/login/response", lambda row: mutate_historical_body(row, lambda body: body["User"].update(Id="another-user"))),
    "old_login_wrong_internal_device": ("observer/login/response", lambda row: mutate_historical_body(row, lambda body: body["SessionInfo"].update(InternalDeviceId=999))),
    "recovery_manifest_other_actor": ("recovery/manifest", lambda row: row["admin"].update(deviceId="another-device")),
    "recovery_manifest_other_observer": ("recovery/manifest", lambda row: row["observer"]["state"].update(sha256="e" * 64)),
    "recovery_manifest_other_anchor": ("recovery/manifest", lambda row: row["preparationAnchor"].update(sha256="e" * 64)),
    "recovery_manifest_other_process": ("recovery/manifest", lambda row: row["process"]["application"].update(pid=55555)),
    "recovery_devices_other_token": ("recovery/deviceObservation/response", lambda row: row["request"]["headers"][-1].__setitem__(1, "another-token")),
    "recovery_old_device_changed": ("recovery/deviceObservation/response", lambda row: mutate_historical_body(row, lambda body: body["Items"][0].update(OldCompleteDocument="changed"))),
    "recovery_old_device_missing": ("recovery/deviceObservation/response", lambda row: mutate_historical_body(row, lambda body: body["Items"].pop(0))),
    "recovery_extra_device": ("recovery/deviceObservation/response", lambda row: mutate_historical_body(row, lambda body: body["Items"].append({"Id": "unowned", "ReportedDeviceId": "unowned-reported"}))),
    "recovery_owned_device_future": ("recovery/deviceObservation/response", lambda row: mutate_historical_body(row, lambda body: body["Items"][-1].update(DateLastActivity="2099-01-01T00:00:00Z"))),
    "recovery_logout_failed": ("recovery/logout/response", lambda row: row.update(status=500)),
    "recovery_logout_intent_after_response": ("recovery/logout/intent", lambda row: row.update(createdAt="2026-09-13T00:01:07Z")),
    "recovery_rejection_wrong_input_pin": ("recovery/rejection/intent", lambda row: row.update(manifestSha256="e" * 64)),
    "recovery_rejection_wrong_plan_pin": ("recovery/rejection/intent", lambda row: row.update(planSha256="e" * 64)),
    "recovery_rejection_not401": ("recovery/rejection/response", lambda row: row.update(status=200)),
    "recovery_rejection_incomplete": ("recovery/rejection/response", lambda row: row.update(completeHttp=False)),
}
for name, (target, mutate) in RECOVERED_MUTATIONS.items():
    setattr(ObserverGuards, "test_recovered_chain_" + name, recovered_mutation_test(target, mutate))


def persistence_failure_test(stage):
    def test(self):
        f = self.fixture()
        def alter(journal):
            original_save, original_state = journal.save, journal.state
            def save(name, value, **kwargs):
                if stage == "wire" and name == "0001-login-response.json": raise OSError("Synthetic durable wire write failure.")
                if stage == "reserved" and name == "0001-login-reserved.json": raise OSError("Synthetic reservation write failure.")
                if stage == "adapter" and name == "0033-logout-controller-result.json": raise OSError("Synthetic controller adapter write failure.")
                return original_save(name, value, **kwargs)
            def state(value):
                pending = value.get("ownershipPending")
                if stage == "before_owner" and pending and pending["stage"] == "response-awaiting-owner": raise OSError("Synthetic owner boundary failure.")
                if stage == "owner_registered" and pending and pending["stage"] == "owner-registered": raise OSError("Synthetic owner commit failure.")
                if stage == "owner_clear" and value.get("token") and pending is None and value["requestCount"] == 1: raise OSError("Synthetic owner-clear failure.")
                return original_state(value)
            journal.save, journal.state = save, state
        f.journal_mutator = alter
        terminal = f.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertFalse(terminal["cleanupComplete"])
        self.assertEqual(len(f.calls), 0 if stage == "reserved" else 33 if stage == "adapter" else 1)
    return test


for stage in ("reserved", "wire", "before_owner", "owner_registered", "owner_clear", "adapter"):
    setattr(ObserverGuards, "test_durable_failure_" + stage, persistence_failure_test(stage))


def transport_failure_test(name, mutate):
    def test(self):
        f = self.fixture(); f.response_mutator = lambda request, response: mutate(response) if request.label == "server" else None
        terminal = f.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual([call["label"] for call in f.calls], ["login", "server"])
        self.assertFalse(terminal["cleanupComplete"])
    return test


for name, mutate in {
    "incomplete": lambda response: response.update(complete_http=False),
    "failure": lambda response: response.update(failure="TimeoutError"),
    "framing": lambda response: response["headers"].__setitem__(-1, (response["headers"][-1][0], "1")),
    "oversized": lambda response: response.update(raw=b"x" * 262145),
    "malformed_json": lambda response: response.update(raw=b"{", headers=[["Content-Length", "1"]]),
    "backwards_time": lambda response: response.update(completed_at="2026-09-12T00:00:00Z"),
}.items():
    setattr(ObserverGuards, "test_response_" + name, transport_failure_test(name, mutate))


def main():
    global SUBJECT, SUPPORT
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--observer", required=True); parser.add_argument("--observer-sha256", required=True)
    parser.add_argument("--transport", required=True); parser.add_argument("--transport-sha256", required=True)
    parser.add_argument("--report", required=True)
    args = parser.parse_args()
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or not sys.flags.isolated or not sys.flags.dont_write_bytecode:
        raise SystemExit("Guards require authorized root SSH with Python -I -B.")
    SUBJECT, SUPPORT = Path(args.observer).resolve(), Path(args.transport).resolve()
    for path, expected in ((SUBJECT, args.observer_sha256), (SUPPORT, args.transport_sha256)):
        if hashlib.sha256(path.read_bytes()).hexdigest() != expected: raise SystemExit("A guard source pin differs.")
    report = Path(args.report)
    if report.exists(): raise SystemExit("A guard report cannot be replayed.")
    with patch.object(socket.socket, "connect", side_effect=AssertionError("Real HTTP is forbidden in synthetic guards.")):
        result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(ObserverGuards))
    value = {"schemaVersion": 1, "kind": "nextup-global-reference-baseline-observer-guards", "tests": result.testsRun,
             "failures": len(result.failures), "errors": len(result.errors), "skipped": len(result.skipped), "passed": result.wasSuccessful(),
             "realBusinessHttp": 0, "realProcessProbes": 0, "originalImplementationBytesRead": False,
             "observerSha256": args.observer_sha256, "transportSha256": args.transport_sha256}
    with report.open("xb") as handle: handle.write(encoded(value)); handle.flush(); os.fsync(handle.fileno())
    report.chmod(0o600)
    print(json.dumps(value, sort_keys=True))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
