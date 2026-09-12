#!/usr/bin/env python3
"""Remote-only synthetic transport and persistence guards.

Every case owns a fresh temporary receipt tree, copied driver sources, fake
process identity, fake clock, and in-memory HTTP responder. No business service,
real process probe, or existing evidence directory is used. Run only through an
authorized SSH session, supplying the two reviewed driver source files.
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
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import call, patch
from urllib.parse import parse_qs, urlsplit
import uuid


sys.dont_write_bytecode = True
TRANSPORT_SOURCE = None
MATRIX_SOURCE = None
EPISODES = ("A1", "A2", "A3", "B1", "B2", "B3")
ACTORS = ("P", "Q")
RUNTIME_TICKS = 6_000_000_000
STAMP = "2026-09-13T01:00:00Z"


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n").encode()


def load_source(path, label):
    name = label + "_" + uuid.uuid4().hex
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def zero_state(item):
    return {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0,
            "LastPlayedDate": None, "IsFavorite": False, "Key": item}


class FakeClock:
    """Advance a synthetic monotonic timeline without sleeping."""

    def __init__(self):
        self.value = 100.0
        self.waits = []

    def __call__(self):
        self.value += 0.01
        return self.value

    def sleep(self, seconds):
        self.waits.append(seconds)
        self.value += seconds


class ExecutionFixture:
    """Build concrete private preparation receipts with no borrowed authority."""

    def __init__(self, testcase):
        self.temporary = tempfile.TemporaryDirectory(prefix="nextup-transport-guards-")
        testcase.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.scope = {}
        for key in ("fixtureRoot", "receiptRoot", "sourceRoot", "proxySourceRoot", "credentialRoot", "evidenceParent"):
            path = self.root / key
            path.mkdir(mode=0o700)
            self.scope[key] = str(path)
        self.transport_path = Path(self.scope["sourceRoot"]) / "nextup-global-transport.py"
        self.matrix_path = Path(self.scope["sourceRoot"]) / "nextup-global-matrix.py"
        self.write(self.transport_path, TRANSPORT_SOURCE.read_bytes())
        self.write(self.matrix_path, MATRIX_SOURCE.read_bytes())
        self.proxy_path = Path(self.scope["proxySourceRoot"]) / "synthetic-owned-proxy.py"
        self.write(self.proxy_path, b"# Synthetic proxy source; never imported or executed.\n")
        self.module = load_source(self.transport_path, "transport_guard_subject")
        testcase.addCleanup(sys.modules.pop, self.module.__name__, None)
        self.endpoint = {"scheme": "http", "host": "127.0.0.1", "port": 54321}
        executable = Path(self.scope["fixtureRoot"]) / "fake-executable"
        self.write(executable, b"synthetic executable bytes; never executed\n")
        executable_info = executable.stat()
        self.owner_uid = os.getuid()
        application = {"pid": 54321, "startTicks": "123456", "bootId": "synthetic-boot",
            "uid": self.owner_uid, "exe": str(executable), "exeDevice": executable_info.st_dev,
            "exeInode": executable_info.st_ino, "cmdline": [str(executable), "--synthetic-only"],
            "networkNamespace": "net:[54321]", "cgroup": "0::/synthetic-application\n"}
        endpoint = deepcopy(application)
        endpoint.update(pid=54322, startTicks="123457", networkNamespace="net:[54322]",
            cgroup="0::/synthetic-proxy\n", cmdline=[str(executable), str(self.proxy_path),
                "--reference-pid", str(application["pid"]), "--reference-start-ticks", application["startTicks"]],
            listener={"port": self.endpoint["port"], "socketInode": "543210"})
        self.process = {"application": application, "endpoint": endpoint, "workerNetworkNamespace": "net:[54322]"}
        self.probe_calls = []
        self.observed_process = deepcopy(self.process)
        lock_path = Path(self.scope["fixtureRoot"]) / "coordination.lock"
        self.write(lock_path, b"owned synthetic coordination lock\n")
        info = lock_path.stat()
        self.lock = {"path": str(lock_path), "device": info.st_dev, "inode": info.st_ino}
        self.output = Path(self.scope["evidenceParent"]) / "fresh-run"
        self.matrix = self.make_matrix()
        self.receipts = {}
        self.credentials = {"schemaVersion": 1, "runId": self.matrix["runId"], "actors": {}}
        for actor in ACTORS:
            row = self.matrix["actors"][actor]
            self.credentials["actors"][actor] = {key: row[key] for key in ("credentialRef", "userId", "username")}
            self.credentials["actors"][actor]["password"] = "synthetic-password-" + actor + "-" + actor * 40
        self.credential_descriptor = self.json_file(Path(self.scope["credentialRoot"]) / "actors.json", self.credentials)
        for actor in ACTORS:
            row = self.matrix["actors"][actor]
            profile = self.profile(actor)
            profile["Name"] = row["username"]
            receipt = self.receipt("policy-" + actor, {"previousUserIds": ["preexisting-unrelated-user"],
                "createdUser": {"Id": row["userId"], "Name": row["username"]}, "observedProfile": profile})
            row["policyReceiptSha256"] = receipt["sha256"]
        for library in ("LA", "LB"):
            media_root = Path(self.scope["fixtureRoot"]) / ("media-" + library)
            media_root.mkdir(mode=0o700)
            files = {}
            for item in EPISODES:
                if self.matrix["items"][item]["library"] != library:
                    continue
                path = media_root / (item + ".fixture")
                raw = ("synthetic immutable media " + item + "\n").encode()
                self.write(path, raw)
                self.matrix["items"][item]["mediaSha256"] = digest(raw)
                files[item] = {"path": str(path), "sha256": digest(raw), "sizeBytes": len(raw)}
            receipt = self.receipt("media-" + library, {"sourceRootId": self.matrix["libraries"][library]["sourceRootId"],
                "rootPath": str(media_root), "files": files})
            self.matrix["libraries"][library]["mediaRootReceiptSha256"] = receipt["sha256"]
        catalog = self.receipt("catalog", {"libraries": self.matrix["libraries"], "items": self.matrix["items"]})
        self.matrix["catalogReceiptSha256"] = catalog["sha256"]
        self.cleanup_facts = self.make_cleanup_facts()
        cleanup = self.receipt("cleanup", self.cleanup_facts)
        self.matrix["cleanupContract"]["receiptSha256"] = cleanup["sha256"]
        coordination = self.receipt("coordination", {"lock": self.lock,
            "fixtureRoot": self.scope["fixtureRoot"], "releasedAt": STAMP, "process": self.process})
        self.matrix["coordinationReceiptSha256"] = coordination["sha256"]
        preparation = self.receipt("preparation", {"serverId": self.matrix["serverId"], "process": self.process,
            "endpoint": self.endpoint, "version": self.matrix["binding"]["version"], "evidenceRoot": str(self.output),
            "credentialStoreSha256": self.credential_descriptor["sha256"], "catalogReceiptSha256": catalog["sha256"],
            "actorIds": {actor: self.matrix["actors"][actor]["userId"] for actor in ACTORS},
            "retainedArtifacts": ["accounts", "catalog", "media", "protocol-audit"]})
        self.matrix["preparationReceiptSha256"] = preparation["sha256"]
        self.execution = {"schemaVersion": 1, "ownerUid": self.owner_uid, "matrix": self.matrix, "scope": self.scope,
            "lock": self.lock, "process": self.process, "endpoint": self.endpoint,
            "sources": {"transport": self.descriptor(self.transport_path), "matrix": self.descriptor(self.matrix_path),
                        "proxy": self.descriptor(self.proxy_path)},
            "receipts": self.receipts, "credentials": self.credential_descriptor,
            "budgets": {"requestSeconds": 5, "normalSeconds": 1800, "cleanupSeconds": 600,
                "requestBytes": 32768, "responseBytes": 16384,
                "totalResponseBytes": 8 * 1024 * 1024, "cleanupResponseBytes": 2 * 1024 * 1024}}

    @staticmethod
    def write(path, raw):
        with path.open("xb") as handle:
            handle.write(raw)
        path.chmod(0o600)

    @staticmethod
    def descriptor(path):
        return {"path": str(path), "sha256": digest(path.read_bytes())}

    def json_file(self, path, value):
        self.write(path, encoded(value))
        return self.descriptor(path)

    def receipt(self, name, facts):
        value = {"schemaVersion": 1, "kind": "nextup-global-" + name,
                 "runId": self.matrix["runId"], "target": self.matrix["target"], "facts": deepcopy(facts)}
        descriptor = self.json_file(Path(self.scope["receiptRoot"]) / (name + ".json"), value)
        self.receipts[name] = descriptor
        return descriptor

    def probe(self, expected):
        self.probe_calls.append(deepcopy(expected))
        return deepcopy(self.observed_process)

    def profile(self, actor):
        row = self.matrix["actors"][actor]
        return {"Id": row["userId"], "Policy": {"IsAdministrator": False, "EnableAllFolders": False,
                "EnabledFolders": list(row["allowedFolderIds"])}, "Configuration": {"Order": ["tv"]}}

    def full_detail(self, item, userdata):
        mapped = self.matrix["items"][item]
        return {"Id": mapped["id"], "Type": mapped["type"], "ParentId": mapped["parentId"],
            "SeriesId": mapped["seriesId"], "ParentIndexNumber": mapped["parentIndexNumber"],
            "IndexNumber": mapped["indexNumber"], "RunTimeTicks": RUNTIME_TICKS,
            "UserData": deepcopy(userdata)}

    def make_cleanup_facts(self):
        facts = {"contractVersion": 2, "process": deepcopy(self.process), "serverId": self.matrix["serverId"],
                 "actors": {}, "calibrations": []}
        for actor in ACTORS:
            row = self.matrix["actors"][actor]
            device = "preparation-device-" + actor
            body = {"AccessToken": "preparation-token-" + actor, "ServerId": self.matrix["serverId"],
                "User": {"Id": row["userId"], "Name": row["username"], "Policy": {"IsAdministrator": False}},
                "SessionInfo": {"Id": "preparation-session-" + actor, "UserId": row["userId"], "DeviceId": device}}
            login_record = {"kind": "synthetic-preparation-login", "actor": actor, "completedAt": STAMP,
                            "response": {"status": 200, "body": deepcopy(body)}}
            facts["actors"][actor] = {"credentialRef": row["credentialRef"], "deviceId": device,
                "login": {"status": 200, "body": body, "responseReceiptSha256": digest(encoded(login_record))}}
        ordinal = 0
        def event(actor, method, route, body):
            nonlocal ordinal
            ordinal += 1
            prepared = facts["actors"][actor]
            authorization = ('Emby Client="Synthetic Cleanup Calibration", Device="Synthetic Fixture", DeviceId="' +
                             prepared["deviceId"] + '", Version="1.0"')
            value = {"ordinal": ordinal,
                "completedAt": (datetime(2026, 9, 13, 1, tzinfo=timezone.utc) + timedelta(seconds=ordinal)).isoformat(),
                "request": {"method": method, "route": route,
                    "headers": [["Accept", "application/json"], ["Authorization", authorization],
                                ["X-Emby-Token", prepared["login"]["body"]["AccessToken"]]], "body": None},
                "response": {"status": 200, "body": deepcopy(body)}}
            value["responseReceiptSha256"] = digest(encoded(value))
            return value
        for actor, item in (("P", "A1"), ("Q", "B1")):
            user_id = self.matrix["actors"][actor]["userId"]
            item_id = self.matrix["items"][item]["id"]
            detail_route = "/emby/Users/" + user_id + "/Items/" + item_id
            delete_route = "/emby/Users/" + user_id + "/PlayedItems/" + item_id
            for mode in ("partial", "complete"):
                baseline = self.full_detail(item, zero_state(item))
                changed = deepcopy(baseline)
                changed["UserData"].update(Played=mode == "complete", PlayCount=1,
                    PlaybackPositionTicks=1_200_000_000 if mode == "partial" else 300_000_000,
                    LastPlayedDate=(datetime(2026, 9, 13, 1, tzinfo=timezone.utc) + timedelta(seconds=ordinal + 2)).isoformat())
                facts["calibrations"].append({"actor": actor, "item": item, "mode": mode,
                    "beforeZero": event(actor, "GET", detail_route, baseline),
                    "beforeDelete": event(actor, "GET", detail_route, changed),
                    "delete": event(actor, "DELETE", delete_route, None),
                    "afterDelete": event(actor, "GET", detail_route, baseline)})
        return facts

    def replace_cleanup_facts(self, facts):
        path = Path(self.receipts["cleanup"]["path"])
        receipt = json.loads(path.read_text())
        receipt["facts"] = deepcopy(facts)
        path.write_bytes(encoded(receipt))
        self.receipts["cleanup"]["sha256"] = digest(path.read_bytes())
        self.matrix["cleanupContract"]["receiptSha256"] = self.receipts["cleanup"]["sha256"]

    def make_matrix(self):
        marker = "a" * 64
        manifest = {"schemaVersion": 1, "runId": "synthetic-transport-guard", "target": "reference",
            "preparationReceiptSha256": marker, "coordinationReceiptSha256": marker,
            "catalogReceiptSha256": marker, "serverId": "synthetic-reference", "lifecycleSeparationSeconds": 2,
            "binding": {"processIdentity": self.process["application"], "version": "synthetic-version",
                "endpoint": "http://127.0.0.1:54321", "isolationIdentity": self.process["application"]["networkNamespace"],
                "evidenceRoot": str(self.output), "fixtureReleased": True, "freshEvidenceRoot": True},
            "cleanupContract": {"mode": "episode-delete-played-items", "zeroBaselineRequired": True,
                "verifiedForBoundTarget": True, "receiptSha256": marker}, "actors": {}, "libraries": {}, "items": {}}
        for actor in ACTORS:
            manifest["actors"][actor] = {"userId": "user-" + actor, "username": "ordinary-" + actor,
                "deviceId": "device-" + actor, "credentialRef": "credential-" + actor,
                "newOwnedOrdinaryAccount": True, "allowedFolderIds": ["view-LA", "view-LB"],
                "policyReceiptSha256": marker}
        for series in ("A", "B"):
            library = "L" + series
            manifest["libraries"][library] = {"libraryId": "library-" + library, "viewId": "view-" + library,
                "sourceRootId": "root-" + library, "scopeItemId": "view-" + library,
                "policyFolderId": "view-" + library, "owned": True, "collectionType": "tvshows",
                "mediaRootReceiptSha256": marker}
            manifest["items"][series] = {"id": "series-" + series, "type": "Series", "name": "Series " + series,
                "library": library, "parentId": "root-" + library}
            for season in (1, 2):
                item = series + "S" + str(season)
                manifest["items"][item] = {"id": "season-" + item, "type": "Season", "library": library,
                    "parentId": "series-" + series, "seriesId": "series-" + series, "indexNumber": season}
            for index, (season, episode) in enumerate(((1, 1), (1, 2), (2, 1)), 1):
                item = series + str(index)
                manifest["items"][item] = {"id": "episode-" + item, "type": "Episode", "library": library,
                    "parentId": "season-" + series + "S" + str(season), "seriesId": "series-" + series,
                    "parentIndexNumber": season, "indexNumber": episode, "runtimeTicks": RUNTIME_TICKS,
                    "frameRate": 30, "mediaSha256": marker}
        return manifest


class FakeTransport:
    """Observe exact request bytes and supply independent synthetic wire facts."""

    def __init__(self, fixture, *, positive=False, failure=None, logout_status=204):
        self.fixture, self.module, self.positive = fixture, fixture.module, positive
        self.failure, self.logout_status = failure, logout_status
        self.runner = None
        self.calls = []
        self.states = {actor: {item: zero_state(item) for item in EPISODES} for actor in ACTORS}
        self.tokens = {actor: "synthetic-token-" + actor + "-private" for actor in ACTORS}
        self.sessions = {actor: "synthetic-session-" + actor for actor in ACTORS}
        self.native_path = "/synthetic/private/media/native-episode.mkv"
        self.stop_count = 0

    def wire(self, status, body=None, *, raw=None, complete=True, failure=None, headers=None):
        raw = (b"" if body is None else encoded(body)) if raw is None else raw
        return self.module.WireResponse(status, headers or [("Content-Type", "application/json")],
                                        raw, complete, STAMP, failure)

    def send(self, request, headers, payload, *, timeout_seconds, max_bytes):
        call = {"request": request, "headers": deepcopy(headers), "payload": payload,
                "timeoutSeconds": timeout_seconds, "maxBytes": max_bytes}
        self.calls.append(call)
        if not (0 < timeout_seconds <= self.fixture.execution["budgets"]["requestSeconds"]):
            raise AssertionError("The fake transport received an unbounded timeout.")
        if max_bytes != self.fixture.execution["budgets"]["responseBytes"] + 1:
            raise AssertionError("The fake transport received an incorrect response bound.")
        ordinal = self.runner.matrix.count
        prefix = f"{ordinal:04d}-{request.label}"
        private = self.fixture.output / "private"
        for suffix in ("-intent.json", "-reserved.json"):
            if not (private / (prefix + suffix)).is_file():
                raise AssertionError("HTTP was attempted without durable intent and reservation.")
        state = json.loads((private / "state.json").read_text())
        if state["matrix"]["pending"]["request"] != request.fact():
            raise AssertionError("HTTP was attempted without its durable pending state.")
        if request.login:
            if "X-Emby-Token" in headers:
                raise AssertionError("A login unexpectedly reused an actor token.")
            actual = parse_qs(payload.decode())
            credential = self.fixture.credentials["actors"][request.actor]
            if actual != {"Username": [credential["username"]], "Pw": [credential["password"]]}:
                raise AssertionError("A login used credentials outside its frozen actor binding.")
        elif headers.get("X-Emby-Token") != self.tokens[request.actor]:
            raise AssertionError("A request did not reuse its actor's exact original token.")
        if self.failure is not None:
            intercepted = self.failure(self, request)
            if intercepted is not None:
                return intercepted
        step = self.runner.matrix.queue[0]
        actor = self.fixture.matrix["actors"][request.actor]
        if request.login:
            return self.wire(200, {"AccessToken": self.tokens[request.actor], "ServerId": self.fixture.matrix["serverId"],
                "User": {"Id": actor["userId"], "Name": actor["username"], "Policy": {"IsAdministrator": False}},
                "SessionInfo": {"Id": self.sessions[request.actor], "UserId": actor["userId"], "DeviceId": actor["deviceId"]}})
        if step.kind == "profile":
            return self.wire(200, self.fixture.profile(request.actor))
        if step.kind == "preferences":
            return self.wire(200, {"home": "tv", "nested": {"preserve": False}})
        if step.kind == "playback-info":
            return self.wire(200, {"PlaySessionId": "synthetic-play-" + step.lifecycle,
                "MediaSources": [{"Id": "synthetic-source-" + step.item, "RunTimeTicks": RUNTIME_TICKS,
                                  "Path": self.native_path, "NativePath": self.native_path}]})
        if step.kind in ("started", "progress", "stopped", "cleanup-stop"):
            if step.kind in ("stopped", "cleanup-stop"):
                self.stop_count += 1
                position = json.loads(payload)["PositionTicks"]
                value = self.states[request.actor][step.item]
                observed_date = datetime(2026, 9, 13, 1, tzinfo=timezone.utc) + timedelta(seconds=self.stop_count)
                value.update(Played=position == RUNTIME_TICKS, PlayCount=value["PlayCount"] + 1,
                    PlaybackPositionTicks=0 if position == RUNTIME_TICKS else position,
                    LastPlayedDate=observed_date.isoformat())
            return self.wire(204)
        if step.kind in ("reset", "cleanup-reset"):
            self.states[request.actor][step.item] = zero_state(step.item)
            return self.wire(200, deepcopy(self.states[request.actor][step.item]))
        if step.kind == "logout":
            return self.wire(self.logout_status)
        if step.kind == "token-invalid":
            return self.wire(401, {"error": "invalid token"})
        if step.kind == "nextup":
            params = parse_qs(urlsplit(request.route).query)
            symbols = []
            if "SeriesId" in params and step.stage != "EXT":
                series = next(name for name in ("A", "B")
                              if self.fixture.matrix["items"][name]["id"] == params["SeriesId"][0])
                active = [index for index in (1, 2, 3) if self.states[request.actor][series + str(index)]["PlayCount"]]
                if active:
                    symbols = [series + str(index) for index in range(max(active), 4)
                               if not self.states[request.actor][series + str(index)]["Played"]]
            elif self.positive and self.stop_count:
                symbols = ["B3", "A3"]
            return self.wire(200, {"Items": [{"Id": self.fixture.matrix["items"][item]["id"], "Type": "Episode",
                "UserData": deepcopy(self.states[request.actor][item])} for item in symbols], "TotalRecordCount": len(symbols)})
        mapped = self.fixture.matrix["items"][step.item]
        body = {"Id": mapped["id"], "Type": mapped["type"], "ParentId": mapped["parentId"]}
        if step.item in EPISODES:
            body.update(SeriesId=mapped["seriesId"], ParentIndexNumber=mapped["parentIndexNumber"],
                IndexNumber=mapped["indexNumber"], RunTimeTicks=RUNTIME_TICKS,
                UserData=deepcopy(self.states[request.actor][step.item]))
        else:
            body["UserData"] = {"Played": False, "PlayCount": 0, "UnplayedItemCount": 3}
        return self.wire(200, body)


class FakeHTTPResponse:
    """Expose only deterministic read chunks, headers, and EOF metadata."""

    def __init__(self, chunks, *, headers=None, length=None, status=200):
        self.chunks = list(chunks)
        self.headers = [] if headers is None else list(headers)
        self.length, self.status = length, status
        self.read_sizes = []
        self.eof_reads = 0
        self.will_close = length is None
        self.closed = length == 0

    def getheaders(self):
        return list(self.headers)

    def read(self, amount):
        self.read_sizes.append(amount)
        if not isinstance(amount, int) or amount <= 0:
            raise AssertionError("The response reader requested an unbounded or empty read.")
        if not self.chunks:
            self.eof_reads += 1
            self.closed = True
            return b""
        value = self.chunks.pop(0)
        if isinstance(value, BaseException):
            raise value
        result = value[:amount]
        if len(value) > amount:
            self.chunks.insert(0, value[amount:])
        if self.length is not None:
            self.length = max(0, self.length - len(result))
        self.closed = not result or self.length == 0
        return result

    def isclosed(self):
        return self.closed


class FakeHTTPConnection:
    """Record a request without opening a socket or addressing a host."""

    def __init__(self, response):
        self.response = response
        self.requests = []
        self.closed = False

    def request(self, method, route, body=None, headers=None):
        self.requests.append({"method": method, "route": route, "body": body, "headers": deepcopy(headers)})

    def getresponse(self):
        return self.response

    def close(self):
        self.closed = True


class WireTransportGuards(unittest.TestCase):
    def setUp(self):
        self.fixture = ExecutionFixture(self)
        self.T = self.fixture.module

    def send(self, response, *, maximum=64, headers=None, route="/emby/Sessions"):
        connection = FakeHTTPConnection(response)
        request = SimpleNamespace(method="GET", route=route)
        with patch.object(self.T.http.client, "HTTPConnection", return_value=connection) as constructor, \
             patch.object(self.T.signal, "getsignal", return_value=self.T.signal.SIG_DFL), \
             patch.object(self.T.signal, "signal") as handler, \
             patch.object(self.T.signal, "setitimer") as timer:
            result = self.T.HTTPTransport(self.fixture.endpoint).send(request,
                {"Accept": "application/json"} if headers is None else headers, None,
                timeout_seconds=5, max_bytes=maximum)
        self.assertEqual(constructor.call_count, 1)
        self.assertTrue(connection.closed)
        self.assertEqual(len(connection.requests), 1)
        self.assertIn(call(self.T.signal.ITIMER_REAL, 0), timer.call_args_list)
        self.assertEqual(handler.call_args.args, (self.T.signal.SIGALRM, self.T.signal.SIG_DFL))
        self.assertLessEqual(len(result.raw), maximum)
        self.assertTrue(all(0 < amount <= maximum for amount in response.read_sizes))
        return result, connection

    def test_close_delimited_nonempty_body_requires_final_eof_read(self):
        response = FakeHTTPResponse([b'{"ok":', b"true}"])
        result, unused = self.send(response)
        self.assertEqual(result.raw, b'{"ok":true}')
        self.assertTrue(result.complete_http)
        self.assertIsNone(result.failure)
        self.assertGreaterEqual(len(response.read_sizes), 3)
        self.assertEqual(response.eof_reads, 1)

    def test_declared_length_accumulates_multiple_short_reads(self):
        response = FakeHTTPResponse([b"abc", b"def"], headers=[("Content-Length", "6")], length=6)
        result, unused = self.send(response)
        self.assertEqual(result.raw, b"abcdef")
        self.assertTrue(result.complete_http)
        self.assertIsNone(result.failure)

    def test_declared_length_truncation_remains_incomplete(self):
        response = FakeHTTPResponse([b"abc"], headers=[("Content-Length", "6")], length=6)
        result, unused = self.send(response)
        self.assertEqual(result.raw, b"abc")
        self.assertFalse(result.complete_http)

    def test_capture_limit_retains_only_bounded_bytes_without_complete_claim(self):
        response = FakeHTTPResponse([b"0123456789abcdef"])
        result, unused = self.send(response, maximum=9)
        self.assertEqual(result.raw, b"012345678")
        self.assertFalse(result.complete_http)

    def test_incomplete_read_retains_earlier_chunks_and_exception_partial(self):
        response = FakeHTTPResponse([b"ab", self.T.http.client.IncompleteRead(b"cd", 2)],
                                    headers=[("Content-Length", "6")], length=6)
        result, unused = self.send(response)
        self.assertEqual(result.raw, b"abcd")
        self.assertFalse(result.complete_http)
        self.assertEqual(result.failure, "IncompleteRead")

    def test_header_count_limit_rejects_before_reading_body(self):
        response = FakeHTTPResponse([b"{}"], headers=[("X-Synthetic", "value")] * (self.T.HEADER_COUNT + 1))
        result, unused = self.send(response)
        self.assertFalse(result.complete_http)
        self.assertIsNotNone(result.failure)
        self.assertEqual(response.read_sizes, [])

    def test_header_count_boundary_still_accepts_complete_body(self):
        response = FakeHTTPResponse([b"{}"], headers=[("X-Synthetic", "value")] * self.T.HEADER_COUNT)
        result, unused = self.send(response)
        self.assertTrue(result.complete_http)
        self.assertEqual(result.raw, b"{}")

    def test_header_byte_limit_rejects_before_reading_body(self):
        response = FakeHTTPResponse([b"{}"], headers=[("X-Synthetic", "x" * (self.T.HEADER_BYTES + 1))])
        result, unused = self.send(response)
        self.assertFalse(result.complete_http)
        self.assertIsNotNone(result.failure)
        self.assertEqual(response.read_sizes, [])

    def test_conflicting_content_lengths_never_claim_complete_http(self):
        response = FakeHTTPResponse([b"{}"], headers=[("Content-Length", "2"), ("Content-Length", "3")], length=2)
        result, unused = self.send(response)
        self.assertFalse(result.complete_http)

    def test_request_metadata_limit_prevents_connection_dispatch(self):
        response = FakeHTTPResponse([b"{}"])
        connection = FakeHTTPConnection(response)
        request = SimpleNamespace(method="GET", route="/emby/Sessions")
        with patch.object(self.T.http.client, "HTTPConnection", return_value=connection), \
             patch.object(self.T.signal, "getsignal", return_value=self.T.signal.SIG_DFL), \
             patch.object(self.T.signal, "signal"), patch.object(self.T.signal, "setitimer"):
            try:
                result = self.T.HTTPTransport(self.fixture.endpoint).send(request,
                    {"Authorization": "x" * (self.T.REQUEST_META_BYTES + 1)}, None,
                    timeout_seconds=5, max_bytes=64)
            except self.T.TransportError:
                result = None
        self.assertEqual(connection.requests, [])
        if result is not None:
            self.assertFalse(result.complete_http)
            self.assertIsNotNone(result.failure)


class CleanupCalibrationGuards(unittest.TestCase):
    """Reject incomplete or misbound cleanup calibration authority."""

    def setUp(self):
        self.fixture = ExecutionFixture(self)
        self.T = self.fixture.module
        self.authority = self.T.Authority(self.fixture.execution, probe=self.fixture.probe)
        self.authority.acquire()
        self.authority.close()
        self.addCleanup(self.authority.close)
        self.addCleanup(sys.modules.pop, self.authority.planner.__name__, None)

    def reject(self, mutate):
        facts = deepcopy(self.fixture.cleanup_facts)
        mutate(facts)
        with self.assertRaises(self.T.TransportError):
            self.authority._cleanup_proof(facts)

    @staticmethod
    def userdata(facts, index=0, event="beforeDelete"):
        return facts["calibrations"][index][event]["response"]["body"]["UserData"]

    @staticmethod
    def header(facts, name, value, *, index=0, event="beforeDelete"):
        headers = facts["calibrations"][index][event]["request"]["headers"]
        pair = next(pair for pair in headers if pair[0].lower() == name.lower())
        pair[1] = value

    def assert_runner_refuses(self, facts):
        self.fixture.replace_cleanup_facts(facts)
        transport = FakeTransport(self.fixture)
        clock = FakeClock()
        runner = None
        try:
            with self.assertRaises(self.T.TransportError):
                runner = self.T.TransportRunner(self.fixture.execution, transport=transport,
                    probe=self.fixture.probe, monotonic=clock, sleeper=clock.sleep)
        finally:
            if runner is not None:
                runner.journal.close()
                runner.authority.close()
        self.assertEqual(transport.calls, [])
        self.assertFalse(self.fixture.output.exists())

    def test_four_calibrations_allow_independent_preparation_devices_and_bounded_completed_position(self):
        facts = deepcopy(self.fixture.cleanup_facts)
        self.authority._cleanup_proof(facts)
        self.assertEqual([(row["actor"], row["item"], row["mode"]) for row in facts["calibrations"]],
                         [("P", "A1", "partial"), ("P", "A1", "complete"), ("Q", "B1", "partial"), ("Q", "B1", "complete")])
        receipts = [facts["actors"][actor]["login"]["responseReceiptSha256"] for actor in ACTORS]
        receipts += [row[event]["responseReceiptSha256"] for row in facts["calibrations"]
                     for event in ("beforeZero", "beforeDelete", "delete", "afterDelete")]
        self.assertEqual(len(receipts), 18)
        self.assertEqual(len(set(receipts)), 18)
        for actor in ACTORS:
            self.assertNotEqual(facts["actors"][actor]["deviceId"], self.fixture.matrix["actors"][actor]["deviceId"])
        for row in facts["calibrations"]:
            if row["mode"] == "complete":
                self.assertGreater(row["beforeDelete"]["response"]["body"]["UserData"]["PlaybackPositionTicks"], 0)

    def test_cleanup_requires_exact_integer_contract_version_two(self):
        for version in (None, 1, True, 2.0):
            with self.subTest(version=repr(version)):
                self.reject(lambda facts: facts.update(contractVersion=version))
        self.reject(lambda facts: facts.pop("contractVersion"))

    def test_cleanup_requires_both_preparation_actors_and_all_four_modes(self):
        for actor in ACTORS:
            with self.subTest(missing_actor=actor):
                self.reject(lambda facts: facts["actors"].pop(actor))
        for index in range(4):
            with self.subTest(missing_calibration=index):
                self.reject(lambda facts: facts["calibrations"].pop(index))
        self.reject(lambda facts: facts["calibrations"].append(deepcopy(facts["calibrations"][0])))
        self.reject(lambda facts: facts["calibrations"].__setitem__(1, deepcopy(facts["calibrations"][0])))

    def test_cleanup_calibrations_and_event_ordinals_cannot_be_reordered(self):
        self.reject(lambda facts: facts["calibrations"].reverse())
        self.reject(lambda facts: facts["calibrations"][1]["beforeZero"].update(ordinal=4))
        self.reject(lambda facts: facts["calibrations"][0]["beforeDelete"].update(ordinal=1))

    def test_cleanup_event_timestamps_cannot_regress(self):
        self.reject(lambda facts: facts["calibrations"][1]["beforeZero"].update(completedAt=STAMP))
        self.reject(lambda facts: facts["calibrations"][0]["delete"].update(completedAt="2026-09-12T01:00:00Z"))

    def test_cleanup_process_and_server_must_match_bound_execution(self):
        self.reject(lambda facts: facts.update(serverId="foreign-server"))
        self.reject(lambda facts: facts["process"]["application"].update(startTicks="foreign-start"))

    def test_cleanup_actor_item_and_delete_route_cannot_cross_ownership(self):
        self.reject(lambda facts: facts["calibrations"][2].update(actor="P"))
        self.reject(lambda facts: facts["calibrations"][0].update(item="B1"))
        self.reject(lambda facts: facts["calibrations"][0]["delete"]["request"].update(
            route="/emby/Users/user-Q/PlayedItems/episode-A1"))
        self.reject(lambda facts: facts["calibrations"][0]["beforeZero"]["response"]["body"].update(Id="episode-B1"))

    def test_cleanup_tokens_must_match_each_actors_retained_login(self):
        self.reject(lambda facts: self.header(facts, "X-Emby-Token",
            facts["actors"]["P"]["login"]["body"]["AccessToken"], index=2))
        self.reject(lambda facts: self.header(facts, "X-Emby-Token", "unknown-token"))
        self.reject(lambda facts: facts["calibrations"][0]["beforeDelete"]["request"]["headers"].append(
            ["x-emby-token", facts["actors"]["P"]["login"]["body"]["AccessToken"]]))

    def test_cleanup_authorization_device_must_match_preparation_device(self):
        def cross_device(facts):
            prepared = facts["actors"]
            event = facts["calibrations"][0]["beforeDelete"]
            metadata = next(value for key, value in event["request"]["headers"] if key == "Authorization")
            self.header(facts, "Authorization", metadata.replace(prepared["P"]["deviceId"], prepared["Q"]["deviceId"]))
        self.reject(cross_device)
        self.reject(lambda facts: facts["actors"]["P"]["login"]["body"]["SessionInfo"].update(DeviceId="foreign-device"))

    def test_cleanup_authorization_rejects_token_fields_duplicate_keys_and_duplicate_headers(self):
        for suffix in (', Token="unreviewed-token"', ', DeviceId="duplicate-device"'):
            with self.subTest(metadata_suffix=suffix):
                def add_metadata(facts):
                    header = next(pair for pair in facts["calibrations"][0]["beforeDelete"]["request"]["headers"]
                                  if pair[0] == "Authorization")
                    header[1] += suffix
                self.reject(add_metadata)
        def duplicate_authorization(facts):
            headers = facts["calibrations"][0]["beforeDelete"]["request"]["headers"]
            headers.append(deepcopy(next(pair for pair in headers if pair[0] == "Authorization")))
        self.reject(duplicate_authorization)

    def test_cleanup_rejects_parallel_authorization_and_device_headers(self):
        for name, value in (("X-Emby-Authorization", 'Emby Token="parallel-token"'),
                            ("X-Emby-Device-Id", "parallel-device")):
            with self.subTest(header=name):
                self.reject(lambda facts: facts["calibrations"][0]["beforeDelete"]["request"]["headers"].append([name, value]))

    def test_cleanup_logins_cannot_reuse_actor_session_token_or_device(self):
        self.reject(lambda facts: facts["actors"]["Q"]["login"]["body"]["SessionInfo"].update(
            Id=facts["actors"]["P"]["login"]["body"]["SessionInfo"]["Id"]))
        def repeated_token(facts):
            token = facts["actors"]["P"]["login"]["body"]["AccessToken"]
            facts["actors"]["Q"]["login"]["body"]["AccessToken"] = token
            for index in (2, 3):
                for event in ("beforeZero", "beforeDelete", "delete", "afterDelete"):
                    self.header(facts, "X-Emby-Token", token, index=index, event=event)
        self.reject(repeated_token)
        def repeated_device(facts):
            device = facts["actors"]["P"]["deviceId"]
            previous = facts["actors"]["Q"]["deviceId"]
            facts["actors"]["Q"]["deviceId"] = device
            facts["actors"]["Q"]["login"]["body"]["SessionInfo"]["DeviceId"] = device
            for index in (2, 3):
                for event in ("beforeZero", "beforeDelete", "delete", "afterDelete"):
                    headers = facts["calibrations"][index][event]["request"]["headers"]
                    pair = next(pair for pair in headers if pair[0] == "Authorization")
                    pair[1] = pair[1].replace(previous, device)
        self.reject(repeated_device)

    def test_cleanup_login_must_confirm_ordinary_user_server_and_credential(self):
        self.reject(lambda facts: facts["actors"]["P"]["login"]["body"]["User"]["Policy"].update(IsAdministrator=True))
        self.reject(lambda facts: facts["actors"]["P"]["login"]["body"]["User"].update(Id="user-Q"))
        self.reject(lambda facts: facts["actors"]["P"]["login"]["body"].update(ServerId="foreign-server"))
        self.reject(lambda facts: facts["actors"]["P"]["login"]["body"]["SessionInfo"].update(UserId="user-Q"))
        self.reject(lambda facts: facts["actors"]["P"].update(credentialRef="credential-Q"))

    def test_cleanup_requires_actual_partial_and_complete_playback_states(self):
        self.reject(lambda facts: self.userdata(facts).update(Played=True))
        self.reject(lambda facts: self.userdata(facts).update(PlaybackPositionTicks=0))
        self.reject(lambda facts: self.userdata(facts, 1).update(Played=False))
        for index in range(4):
            with self.subTest(calibration=index):
                self.reject(lambda facts: self.userdata(facts, index).update(PlayCount=0))
                self.reject(lambda facts: self.userdata(facts, index).update(LastPlayedDate=None))

    def test_cleanup_requires_zero_before_and_after_each_calibration(self):
        for index in range(4):
            for event in ("beforeZero", "afterDelete"):
                with self.subTest(calibration=index, event=event):
                    self.reject(lambda facts: self.userdata(facts, index, event).update(PlayCount=1))
                    self.reject(lambda facts: self.userdata(facts, index, event).update(PlaybackPositionTicks=1))

    def test_cleanup_repeated_actor_baseline_preserves_full_userdata_and_field_presence(self):
        def changed_second_baseline(facts):
            for event in ("beforeZero", "beforeDelete", "afterDelete"):
                self.userdata(facts, 1, event)["Key"] = "changed-baseline-key"
        self.reject(changed_second_baseline)
        self.reject(lambda facts: self.userdata(facts, 0, "afterDelete").pop("LastPlayedDate"))
        self.reject(lambda facts: self.userdata(facts, 0, "afterDelete").update(UnexpectedField=None))

    def test_cleanup_playback_and_delete_cannot_change_unrelated_userdata(self):
        self.reject(lambda facts: self.userdata(facts).update(IsFavorite=True))
        self.reject(lambda facts: self.userdata(facts, 0, "afterDelete").update(IsFavorite=True))
        self.reject(lambda facts: self.userdata(facts).update(Key="foreign-key"))

    def test_cleanup_integer_fields_reject_boolean_and_floating_point_substitutes(self):
        for index in (0, 1):
            original = self.userdata(self.fixture.cleanup_facts, index)
            mode = self.fixture.cleanup_facts["calibrations"][index]["mode"]
            for key, values in (("PlayCount", (True, float(original["PlayCount"]))),
                                ("PlaybackPositionTicks", (True, float(original["PlaybackPositionTicks"]))),
                                ("Played", (int(original["Played"]), float(original["Played"])))):
                for value in values:
                    with self.subTest(mode=mode, userdata_field=key, value=repr(value)):
                        self.reject(lambda facts: self.userdata(facts, index).update({key: value}))
            ordinal = self.fixture.cleanup_facts["calibrations"][index]["beforeZero"]["ordinal"]
            for value in (True, float(ordinal)):
                with self.subTest(mode=mode, ordinal=repr(value)):
                    self.reject(lambda facts: facts["calibrations"][index]["beforeZero"].update(ordinal=value))
            for value in (True, 200.0):
                with self.subTest(mode=mode, status=repr(value)):
                    self.reject(lambda facts: facts["calibrations"][index]["delete"]["response"].update(status=value))
        for value in (True, 200.0):
            with self.subTest(login_status=repr(value)):
                self.reject(lambda facts: facts["actors"]["P"]["login"].update(status=value))

    def test_cleanup_requires_full_detail_relationships_and_exact_primitive_types(self):
        for index in (0, 1):
            mode = self.fixture.cleanup_facts["calibrations"][index]["mode"]
            for key, value in (("Type", "Movie"), ("ParentId", "foreign-parent"), ("SeriesId", "series-B"),
                               ("ParentIndexNumber", True), ("IndexNumber", 1.0), ("RunTimeTicks", float(RUNTIME_TICKS))):
                with self.subTest(mode=mode, detail_field=key):
                    self.reject(lambda facts: facts["calibrations"][index]["beforeDelete"]["response"]["body"].update({key: value}))
            with self.subTest(mode=mode, missing_field="RunTimeTicks"):
                self.reject(lambda facts: facts["calibrations"][index]["beforeDelete"]["response"]["body"].pop("RunTimeTicks"))

    def test_cleanup_requires_distinct_receipts_for_two_logins_and_sixteen_events(self):
        with self.subTest(reused_records="login-and-login"):
            self.reject(lambda facts: facts["actors"]["Q"]["login"].update(
                responseReceiptSha256=facts["actors"]["P"]["login"]["responseReceiptSha256"]))
        with self.subTest(reused_records="event-and-event"):
            self.reject(lambda facts: facts["calibrations"][0]["beforeDelete"].update(
                responseReceiptSha256=facts["calibrations"][0]["beforeZero"]["responseReceiptSha256"]))
        with self.subTest(reused_records="login-and-event"):
            self.reject(lambda facts: facts["calibrations"][0]["beforeZero"].update(
                responseReceiptSha256=facts["actors"]["P"]["login"]["responseReceiptSha256"]))

    def test_cleanup_events_require_exact_shape_receipt_and_request(self):
        self.reject(lambda facts: facts["calibrations"][0]["delete"].update(unreviewedField=True))
        self.reject(lambda facts: facts["calibrations"][0]["delete"].update(responseReceiptSha256="invalid"))
        self.reject(lambda facts: facts["calibrations"][0]["delete"]["request"].update(method="POST"))
        self.reject(lambda facts: facts["calibrations"][0]["delete"]["request"].update(body={}))
        self.reject(lambda facts: facts["calibrations"][0]["delete"]["response"].update(status=204))

    def test_legacy_single_cleanup_proof_is_rejected_before_runner_http_or_evidence(self):
        baseline = self.fixture.full_detail("A1", zero_state("A1"))
        changed = deepcopy(baseline)
        changed["UserData"].update(Played=True, PlayCount=1, LastPlayedDate=STAMP)
        legacy = {"process": deepcopy(self.fixture.process), "serverId": self.fixture.matrix["serverId"],
            "actor": "P", "item": "A1", "baseline": baseline, "beforeDelete": changed,
            "delete": {"method": "DELETE", "route": "/emby/Users/user-P/PlayedItems/episode-A1", "status": 200},
            "afterDelete": deepcopy(baseline)}
        self.assert_runner_refuses(legacy)

    def test_missing_actor_calibration_is_rejected_before_runner_http_or_evidence(self):
        facts = deepcopy(self.fixture.cleanup_facts)
        facts["calibrations"].pop()
        self.assert_runner_refuses(facts)


class TransportGuards(unittest.TestCase):
    def setUp(self):
        self.fixture = ExecutionFixture(self)
        self.T = self.fixture.module
        self.clock = FakeClock()
        self.real_http_guard = patch.object(self.T.HTTPTransport, "send",
                                           side_effect=AssertionError("Real HTTP is forbidden in synthetic guards."))
        self.real_http_guard.start()
        self.addCleanup(self.real_http_guard.stop)

    def runner(self, **options):
        transport = FakeTransport(self.fixture, **options)
        runner = self.T.TransportRunner(self.fixture.execution, transport=transport,
            probe=self.fixture.probe, monotonic=self.clock, sleeper=self.clock.sleep)
        transport.runner = runner
        self.addCleanup(runner.authority.close)
        self.addCleanup(runner.journal.close)
        self.addCleanup(sys.modules.pop, runner.module.__name__, None)
        return runner, transport

    def advance(self, runner, predicate):
        for unused in range(300):
            if predicate(runner):
                return
            request = runner.matrix.prepare_next(runner.elapsed())
            if isinstance(request, runner.module.WaitRequired):
                self.clock.sleep(request.seconds)
                continue
            self.assertIsNotNone(request, "The synthetic branch ended before the target request.")
            runner._dispatch(request)
            self.assertFalse(runner.blocked, "A synthetic precondition unexpectedly blocked dispatch.")
        self.fail("The synthetic branch exceeded its bounded request count.")

    def assert_no_resume(self, runner, transport):
        count = len(transport.calls)
        with self.assertRaises(self.T.TransportError):
            runner.run()
        self.assertEqual(len(transport.calls), count)

    def assert_blocked(self, runner, transport, result, attempts):
        self.assertTrue(runner.blocked)
        self.assertFalse(runner.finished)
        self.assertEqual(len(transport.calls), attempts)
        self.assertEqual(result["mode"], "recovery-required")
        self.assertFalse(result["cleanupComplete"])
        self.assertFalse(result["liveAcceptanceClaim"])
        self.assert_no_resume(runner, transport)

    def private_state(self):
        return json.loads((self.fixture.output / "private" / "state.json").read_text())

    def test_complete_all_empty_branch_closes_without_positive_playback(self):
        runner, transport = self.runner()
        result = runner.run()
        self.assertEqual(result["mode"], "closed")
        self.assertEqual(result["outcome"], "reference_global_positive_unresolved")
        self.assertEqual(runner.matrix.normal_count, 116)
        self.assertFalse(runner.matrix.r5_r6_started)
        self.assertTrue(result["cleanupComplete"])
        self.assertFalse(result["liveAcceptanceClaim"])
        self.assertEqual(runner.matrix.current, runner.matrix.baseline)
        self.assertEqual(runner.matrix.revoked, set(ACTORS))
        self.assertEqual(len(transport.calls), runner.matrix.count)
        self.assertEqual(len({call["request"].label for call in transport.calls}), len(transport.calls))
        self.assertTrue(self.fixture.probe_calls)
        self.assert_no_resume(runner, transport)

    def test_complete_positive_branch_retains_all_extensions_and_partial_states(self):
        runner, transport = self.runner(positive=True)
        result = runner.run()
        self.assertEqual(result["mode"], "closed")
        self.assertEqual(runner.matrix.normal_count, 193)
        self.assertTrue(runner.matrix.extension_complete)
        self.assertTrue(runner.matrix.r5_r6_started)
        self.assertTrue(result["cleanupComplete"])
        self.assertEqual(runner.matrix.current, runner.matrix.baseline)
        self.assertEqual(sum(call["request"].label.startswith("EXT-") for call in transport.calls), 14)
        self.assertEqual({fact["requestedPositionTicks"] for fact in runner.matrix.facts
                          if fact.get("durableStateEstablished")}, {RUNTIME_TICKS, 1_200_000_000})
        self.assertLessEqual(result["httpAttempts"], 242)
        self.assertTrue(self.clock.waits)
        self.assertTrue(all(0 < seconds <= 5 for seconds in self.clock.waits))

    def test_logout_401_and_rejection_use_exact_original_actor_token(self):
        def fail_observation(server, request):
            if request.label == "R0-P-detail-A1":
                return server.wire(500, {"error": "synthetic observation rejection"})
            return None
        runner, transport = self.runner(failure=fail_observation, logout_status=401)
        result = runner.run()
        self.assertEqual(result["mode"], "closed-with-observation-failure")
        for actor in ACTORS:
            logout = next(call for call in transport.calls if call["request"].label == "CLEAN-" + actor + "-logout")
            rejected = next(call for call in transport.calls if call["request"].label == "CLEAN-" + actor + "-token-invalid")
            self.assertEqual(logout["headers"]["X-Emby-Token"], transport.tokens[actor])
            self.assertEqual(rejected["headers"]["X-Emby-Token"], transport.tokens[actor])
        self.assertEqual(sum(call["request"].login for call in transport.calls), 2)
        self.assertTrue(result["cleanupComplete"])

    def test_complete_plain_text_401_can_prove_owned_logout_and_token_rejection(self):
        def plain_text_rejection(server, request):
            if request.label == "R0-P-detail-A1":
                return server.wire(500, {"error": "synthetic observation rejection"})
            if request.label.endswith(("-logout", "-token-invalid")):
                return server.wire(401, raw=b"Unauthorized", headers=[("Content-Type", "text/plain; charset=utf-8")])
            return None
        runner, transport = self.runner(failure=plain_text_rejection)
        result = runner.run()
        self.assertEqual(result["mode"], "closed-with-observation-failure")
        self.assertTrue(result["cleanupComplete"])
        self.assertEqual(runner.matrix.revoked, set(ACTORS))
        for call in transport.calls:
            if call["request"].label.endswith(("-logout", "-token-invalid")):
                self.assertEqual(call["headers"]["X-Emby-Token"], transport.tokens[call["request"].actor])

    def test_json_claimed_401_rejects_malformed_json_without_next_http(self):
        def invalid_json_rejection(server, request):
            if request.label == "PRE-P-profile":
                return server.wire(500, {"error": "synthetic profile rejection"})
            if request.label.endswith("-logout"):
                return server.wire(401, raw=b"Unauthorized", headers=[("Content-Type", "application/json")])
            return None
        runner, transport = self.runner(failure=invalid_json_rejection)
        result = runner.run()
        self.assertTrue(runner.blocked)
        self.assertFalse(result["cleanupComplete"])
        self.assertEqual(transport.calls[-1]["request"].label, "CLEAN-P-logout")
        self.assert_no_resume(runner, transport)

    def test_plain_text_401_cannot_replace_required_profile_json(self):
        def invalid_profile(server, request):
            if request.label == "PRE-P-profile":
                return server.wire(401, raw=b"Unauthorized", headers=[("Content-Type", "text/plain")])
            return None
        runner, transport = self.runner(failure=invalid_profile)
        result = runner.run()
        self.assert_blocked(runner, transport, result, 2)
        self.assertIsNotNone(runner.matrix.pending)
        self.assertEqual(result["failure"]["kind"], "response-not-consumable")

    def test_known_progress_failure_stops_reconciles_then_deletes(self):
        def reject_progress(server, request):
            return server.wire(400, {"error": "synthetic progress rejection"}) if request.label == "R1-P-progress-A1" else None
        runner, transport = self.runner(failure=reject_progress)
        result = runner.run()
        labels = [call["request"].label for call in transport.calls]
        stop, reconcile, delete = (labels.index(label) for label in
            ("CLEAN-R1-stop", "CLEAN-P-reconcile-A1", "CLEAN-P-delete-A1"))
        self.assertLess(stop, reconcile)
        self.assertLess(reconcile, delete)
        self.assertEqual(sum(call["request"].method == "DELETE" for call in transport.calls), 1)
        self.assertEqual(result["mode"], "closed-with-observation-failure")
        self.assertEqual(result["failure"]["kind"], "observation-failure")
        self.assertTrue(result["cleanupComplete"])
        self.assertEqual(runner.matrix.current, runner.matrix.baseline)
        fact = next(fact for fact in runner.matrix.facts if fact["kind"] == "cleanup-reconcile")
        self.assertTrue(fact["ownedStopStateReconciled"])
        self.assertTrue(fact["cleanupResetRequired"])

    def test_known_detail_failure_reconciles_acknowledged_stop_without_repeating_it(self):
        def reject_detail(server, request):
            return server.wire(500, {"error": "synthetic detail rejection"}) if request.label == "R1-P-after-play-A1" else None
        runner, transport = self.runner(failure=reject_detail)
        result = runner.run()
        self.assertEqual(result["mode"], "closed-with-observation-failure")
        self.assertTrue(result["cleanupComplete"])
        self.assertFalse(any(call["request"].label == "CLEAN-R1-stop" for call in transport.calls))
        self.assertEqual(runner.matrix.current, runner.matrix.baseline)

    def test_malformed_login_preserves_unverified_token_and_blocks_ownership_inference(self):
        def malformed_login(server, request):
            return server.wire(200, {"AccessToken": server.tokens["P"], "ServerId": "foreign-server",
                                     "User": {"Id": "foreign-user"}, "SessionInfo": {"Id": "unknown-session"}})
        runner, transport = self.runner(failure=malformed_login)
        result = runner.run()
        self.assert_blocked(runner, transport, result, 1)
        self.assertEqual(result["unverifiedLoginActors"], ["P"])
        self.assertEqual(runner.tokens, {})
        self.assertEqual(runner.matrix.sessions, {})
        self.assertEqual(self.private_state()["unverifiedLogins"]["P"]["AccessToken"], transport.tokens["P"])
        with self.assertRaises(self.T.TransportError):
            runner._begin_cleanup()

    def bad_wire_case(self, factory):
        runner, transport = self.runner(failure=lambda server, request: factory(server))
        result = runner.run()
        self.assert_blocked(runner, transport, result, 1)
        self.assertEqual(runner.matrix.count, 1)
        self.assertIsNotNone(runner.matrix.pending)
        self.assertEqual(runner.matrix.sessions, {})
        self.assertEqual(len(list((self.fixture.output / "private").glob("*-wire.json"))), 1)
        self.assertEqual(len(list((self.fixture.output / "export").glob("*-response.json"))), 0)
        self.assertTrue(self.private_state()["matrix"]["failure"]["responseLost"])

    def test_partial_http_body_is_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b'{"AccessToken":"partial', complete=False))

    def test_content_length_mismatch_is_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b"{}", headers=[("Content-Length", "3")]))

    def test_conflicting_content_lengths_are_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b"{}", headers=[("Content-Length", "2"), ("content-length", "3")]))

    def test_mixed_framing_is_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b"{}", headers=[("Content-Length", "2"), ("Transfer-Encoding", "chunked")]))

    def test_non_utf8_body_is_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b'{"value":"\xff"}'))

    def test_duplicate_json_keys_are_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b'{"AccessToken":"one","AccessToken":"two"}'))

    def test_oversized_body_is_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b"x" * (self.fixture.execution["budgets"]["responseBytes"] + 1)))

    def test_nonfinite_json_is_retained_without_consumption(self):
        self.bad_wire_case(lambda server: server.wire(200, raw=b'{"value":NaN}'))

    def test_lost_transport_response_preserves_pending_reservation_without_replay(self):
        def lost_response(server, request):
            raise ConnectionError("Synthetic response loss after request dispatch.")
        runner, transport = self.runner(failure=lost_response)
        result = runner.run()
        self.assert_blocked(runner, transport, result, 1)
        self.assertEqual(result["failure"]["kind"], "transport-exception")
        self.assertEqual(runner.matrix.count, 1)
        self.assertIsNotNone(runner.matrix.pending)
        self.assertTrue(self.private_state()["matrix"]["failure"]["responseLost"])
        self.assertFalse(list((self.fixture.output / "private").glob("*-wire.json")))

    def persistence_case(self, suffix, *, attempts, reserved):
        runner, transport = self.runner()
        original = runner.journal.save
        fired = []
        def fail_once(name, value, *, export=False):
            if name.endswith(suffix) and not fired:
                fired.append(name)
                raise OSError("Synthetic journal persistence failure.")
            return original(name, value, export=export)
        with patch.object(runner.journal, "save", side_effect=fail_once):
            result = runner.run()
        self.assertEqual(len(fired), 1)
        self.assert_blocked(runner, transport, result, attempts)
        self.assertEqual(runner.matrix.count, reserved)
        self.assertEqual(len(list((self.fixture.output / "private").glob("*-intent.json"))), int(reserved > 0))

    def test_intent_persistence_failure_prevents_first_http(self):
        self.persistence_case("-intent.json", attempts=0, reserved=0)

    def test_reservation_persistence_failure_prevents_first_http(self):
        self.persistence_case("-reserved.json", attempts=0, reserved=1)

    def test_wire_response_persistence_failure_prevents_consumption_and_replay(self):
        self.persistence_case("-wire.json", attempts=1, reserved=1)

    def test_export_response_persistence_failure_prevents_consumption_and_replay(self):
        self.persistence_case("-response.json", attempts=1, reserved=1)

    def state_failure_case(self, after_acceptance):
        runner, transport = self.runner()
        original = runner.journal.state
        fired = []
        def fail_once(value):
            pending = value["matrix"]["pending"]
            should_fail = (value["matrix"]["requestCount"] == 1 and
                ((pending is None and bool(value["tokens"])) if after_acceptance else
                 (pending is not None and not value["attemptedLabels"])))
            if should_fail and not fired:
                fired.append(True)
                raise OSError("Synthetic state snapshot failure.")
            return original(value)
        with patch.object(runner.journal, "state", side_effect=fail_once):
            result = runner.run()
        self.assertEqual(fired, [True])
        self.assert_blocked(runner, transport, result, int(after_acceptance))
        self.assertEqual(runner.matrix.count, 1)

    def test_pending_state_persistence_failure_prevents_first_http(self):
        self.state_failure_case(False)

    def test_accepted_state_persistence_failure_prevents_next_http(self):
        self.state_failure_case(True)

    def test_slow_pending_state_persistence_cannot_outlive_http_deadline(self):
        runner, transport = self.runner()
        original = runner.journal.state
        delayed = []
        def slow_state(value):
            result = original(value)
            if value["matrix"]["pending"] is not None and not delayed:
                delayed.append(True)
                self.clock.value += runner.execution["budgets"]["normalSeconds"]
            return result
        with patch.object(runner.journal, "state", side_effect=slow_state):
            result = runner.run()
        self.assertEqual(delayed, [True])
        self.assert_blocked(runner, transport, result, 0)

    def persistence_drift_case(self, change):
        runner, transport = self.runner()
        original = runner.journal.state
        changed = []
        def drifting_state(value):
            result = original(value)
            if value["matrix"]["pending"] is not None and not changed:
                changed.append(True)
                change()
            return result
        with patch.object(runner.journal, "state", side_effect=drifting_state):
            result = runner.run()
        self.assertEqual(changed, [True])
        self.assert_blocked(runner, transport, result, 0)
        self.assertEqual(runner.matrix.count, 1)

    def test_process_drift_during_pending_persistence_prevents_first_http(self):
        self.persistence_drift_case(lambda: self.fixture.observed_process["endpoint"].update(startTicks="changed-during-write"))

    def test_credential_drift_during_pending_persistence_prevents_first_http(self):
        def drift():
            path = Path(self.fixture.credential_descriptor["path"])
            path.write_bytes(path.read_bytes() + b" ")
        self.persistence_drift_case(drift)

    def test_source_drift_during_pending_persistence_prevents_first_http(self):
        self.persistence_drift_case(lambda: self.fixture.transport_path.write_bytes(
            self.fixture.transport_path.read_bytes() + b"\n# Synthetic drift during persistence.\n"))

    def test_persistent_storage_failure_keeps_runner_blocked_without_http(self):
        runner, transport = self.runner()
        with patch.object(runner.journal, "state", side_effect=OSError("Synthetic persistent storage failure.")):
            result = runner.run()
        self.assertTrue(runner.blocked)
        self.assertEqual(len(transport.calls), 0)
        self.assertEqual(runner.matrix.count, 1)
        self.assertIsInstance(result, dict)
        self.assertFalse(result["cleanupComplete"])
        self.assertFalse(result["evidenceComplete"])
        self.assert_no_resume(runner, transport)

    def drift_case(self, change):
        runner, transport = self.runner()
        self.advance(runner, lambda value: value.matrix.count == 1)
        change(runner)
        result = runner.run()
        self.assert_blocked(runner, transport, result, 1)
        self.assertEqual(runner.matrix.count, 1)

    def test_replaced_fixture_lock_prevents_next_http(self):
        def replace_lock(runner):
            path = Path(self.fixture.lock["path"])
            replacement = path.with_name("replacement.lock")
            self.fixture.write(replacement, b"replacement lock\n")
            os.replace(replacement, path)
        self.drift_case(replace_lock)

    def test_transport_source_drift_prevents_next_http(self):
        self.drift_case(lambda runner: self.fixture.transport_path.write_bytes(self.fixture.transport_path.read_bytes() + b"\n# Synthetic drift.\n"))

    def test_matrix_source_drift_prevents_next_http(self):
        self.drift_case(lambda runner: self.fixture.matrix_path.write_bytes(self.fixture.matrix_path.read_bytes() + b"\n# Synthetic drift.\n"))

    def test_proxy_source_drift_prevents_next_http(self):
        self.drift_case(lambda runner: self.fixture.proxy_path.write_bytes(self.fixture.proxy_path.read_bytes() + b"\n# Synthetic drift.\n"))

    def test_credential_drift_prevents_next_http(self):
        def change_credential(runner):
            path = Path(self.fixture.credential_descriptor["path"])
            value = json.loads(path.read_text())
            value["actors"]["P"]["password"] += "changed"
            path.write_bytes(encoded(value))
        self.drift_case(change_credential)

    def test_process_identity_drift_prevents_next_http(self):
        self.drift_case(lambda runner: self.fixture.observed_process["application"].update(startTicks="different-start"))

    def test_endpoint_process_identity_drift_prevents_next_http(self):
        self.drift_case(lambda runner: self.fixture.observed_process["endpoint"].update(startTicks="different-start"))

    def test_listener_ownership_drift_prevents_next_http(self):
        self.drift_case(lambda runner: self.fixture.observed_process["endpoint"]["listener"].update(socketInode="999999"))

    def test_receipt_drift_prevents_next_http(self):
        def change_receipt(runner):
            path = Path(self.fixture.receipts["coordination"]["path"])
            path.write_bytes(path.read_bytes() + b" ")
        self.drift_case(change_receipt)

    def test_media_drift_prevents_next_http(self):
        def change_media(runner):
            path = Path(self.fixture.scope["fixtureRoot"]) / "media-LA" / "A1.fixture"
            path.write_bytes(b"changed immutable media\n")
        self.drift_case(change_media)

    def test_evidence_root_change_without_new_preparation_receipt_is_rejected(self):
        execution = deepcopy(self.fixture.execution)
        execution["matrix"]["binding"]["evidenceRoot"] = str(self.fixture.output.with_name("different-run"))
        authority = self.T.Authority(execution, probe=self.fixture.probe)
        try:
            with self.assertRaises(self.T.TransportError):
                authority.acquire()
        finally:
            authority.close()
        self.assertFalse(self.fixture.output.with_name("different-run").exists())

    def replaced_journal_directory_case(self, name):
        runner, transport = self.runner()
        self.advance(runner, lambda value: value.matrix.count == 1)
        path = self.fixture.output / name
        retained = self.fixture.output / (name + "-retained")
        path.rename(retained)
        path.mkdir(mode=0o700)
        result = runner.run()
        self.assert_blocked(runner, transport, result, 1)
        self.assertFalse(list(path.iterdir()))
        self.assertTrue(retained.is_dir())

    def test_replaced_private_journal_directory_prevents_next_http(self):
        self.replaced_journal_directory_case("private")

    def test_replaced_export_journal_directory_prevents_next_http(self):
        self.replaced_journal_directory_case("export")

    def test_world_readable_credential_file_is_rejected(self):
        Path(self.fixture.credential_descriptor["path"]).chmod(0o644)
        with self.assertRaises(self.T.TransportError):
            self.runner()

    def test_world_readable_receipt_file_is_rejected(self):
        Path(self.fixture.receipts["preparation"]["path"]).chmod(0o644)
        with self.assertRaises(self.T.TransportError):
            self.runner()

    def test_existing_evidence_root_is_rejected_without_http_or_overwrite(self):
        self.fixture.output.mkdir(mode=0o700)
        sentinel = self.fixture.output / "sentinel.txt"
        sentinel.write_text("Existing evidence must survive.\n")
        with self.assertRaises(FileExistsError):
            self.runner()
        self.assertEqual(sentinel.read_text(), "Existing evidence must survive.\n")
        self.assertEqual({path.name for path in self.fixture.output.iterdir()}, {"sentinel.txt"})

    def test_request_budget_rejects_oversized_login_without_http(self):
        self.fixture.execution["budgets"]["requestBytes"] = 1024
        credential = self.fixture.credentials["actors"]["P"]
        credential["password"] = "P" * 2048
        path = Path(self.fixture.credential_descriptor["path"])
        path.write_bytes(encoded(self.fixture.credentials))
        self.fixture.credential_descriptor["sha256"] = digest(path.read_bytes())
        receipt_path = Path(self.fixture.receipts["preparation"]["path"])
        preparation = json.loads(receipt_path.read_text())
        preparation["facts"]["credentialStoreSha256"] = self.fixture.credential_descriptor["sha256"]
        receipt_path.write_bytes(encoded(preparation))
        self.fixture.receipts["preparation"]["sha256"] = digest(receipt_path.read_bytes())
        self.fixture.matrix["preparationReceiptSha256"] = self.fixture.receipts["preparation"]["sha256"]
        runner, transport = self.runner()
        result = runner.run()
        self.assert_blocked(runner, transport, result, 0)
        self.assertEqual(runner.matrix.count, 0)

    def test_normal_byte_reserve_prevents_http(self):
        runner, transport = self.runner()
        budgets = runner.execution["budgets"]
        runner.charged_bytes = budgets["totalResponseBytes"] - budgets["cleanupResponseBytes"] - budgets["responseBytes"]
        result = runner.run()
        self.assert_blocked(runner, transport, result, 0)
        self.assertEqual(runner.matrix.count, 0)

    def test_normal_deadline_prevents_http(self):
        runner, transport = self.runner()
        self.clock.value += runner.execution["budgets"]["normalSeconds"]
        result = runner.run()
        self.assert_blocked(runner, transport, result, 0)

    def test_cleanup_deadline_prevents_cleanup_dispatch(self):
        def reject_profile(server, request):
            return server.wire(500, {"error": "synthetic profile rejection"}) if request.label == "PRE-P-profile" else None
        runner, transport = self.runner(failure=reject_profile)
        self.advance(runner, lambda value: value.matrix.mode == "recovery-required")
        runner._begin_cleanup()
        self.clock.value += runner.execution["budgets"]["cleanupSeconds"]
        result = runner.run()
        self.assert_blocked(runner, transport, result, 2)
        self.assertFalse(any(call["request"].cleanup for call in transport.calls))

    def test_cleanup_byte_budget_prevents_cleanup_dispatch(self):
        def reject_profile(server, request):
            return server.wire(500, {"error": "synthetic profile rejection"}) if request.label == "PRE-P-profile" else None
        runner, transport = self.runner(failure=reject_profile)
        self.advance(runner, lambda value: value.matrix.mode == "recovery-required")
        runner._begin_cleanup()
        budgets = runner.execution["budgets"]
        runner.charged_bytes = budgets["totalResponseBytes"] - budgets["responseBytes"]
        result = runner.run()
        self.assert_blocked(runner, transport, result, 2)

    def test_monotonic_clock_regression_prevents_http(self):
        runner, transport = self.runner()
        self.clock.value -= 1
        result = runner.run()
        self.assert_blocked(runner, transport, result, 0)

    def test_nonfinite_monotonic_clock_prevents_http(self):
        runner, transport = self.runner()
        self.clock.value = float("nan")
        result = runner.run()
        self.assert_blocked(runner, transport, result, 0)

    def test_normal_request_count_cannot_spend_cleanup_reserve(self):
        runner, transport = self.runner()
        runner.matrix.count = runner.module.NORMAL_LIMIT
        result = runner.run()
        self.assert_blocked(runner, transport, result, 0)

    def test_invalid_budget_and_execution_fields_are_rejected(self):
        for mutate in (
            lambda value: value["budgets"].update(requestSeconds=True),
            lambda value: value["budgets"].update(cleanupResponseBytes=1024),
            lambda value: value.update(unreviewedField=True),
            lambda value: value["endpoint"].update(host="localhost"),
        ):
            with self.subTest(mutation=mutate.__code__.co_firstlineno):
                execution = deepcopy(self.fixture.execution)
                mutate(execution)
                with self.assertRaises(self.T.TransportError):
                    self.T.Authority(execution, probe=self.fixture.probe)

    def test_actor_token_swap_is_rejected_before_http(self):
        runner, transport = self.runner()
        self.advance(runner, lambda value: set(value.tokens) == set(ACTORS))
        attempts = len(transport.calls)
        runner.tokens["Q"] = runner.tokens["P"]
        result = runner.run()
        self.assert_blocked(runner, transport, result, attempts)

    def test_private_wire_keeps_bytes_while_exports_remove_context_secrets(self):
        runner, transport = self.runner()
        self.advance(runner, lambda value: "R1-P-playback-info-A1" in value.matrix.completed)
        private = self.fixture.output / "private"
        exported = self.fixture.output / "export"
        login_wire = json.loads(next(private.glob("*-PRE-P-login-wire.json")).read_text())
        login_body = json.loads(base64.b64decode(login_wire["responseBodyBase64"]))
        self.assertEqual(login_body["AccessToken"], transport.tokens["P"])
        self.assertEqual(login_body["SessionInfo"]["Id"], transport.sessions["P"])
        playback_wire = json.loads(next(private.glob("*-R1-P-playback-info-A1-wire.json")).read_text())
        playback_body = json.loads(base64.b64decode(playback_wire["responseBodyBase64"]))
        self.assertEqual(playback_body["MediaSources"][0]["Id"], "synthetic-source-A1")
        export_text = "\n".join(path.read_text() for path in exported.glob("*.json"))
        for secret in (*transport.tokens.values(), *transport.sessions.values(), "synthetic-play-R1",
                       "synthetic-source-A1", transport.native_path, *self.fixture.scope.values()):
            with self.subTest(secret_kind=secret.split("-")[0]):
                self.assertNotIn(secret, export_text)
        self.assertIn("[redacted]", export_text)
        self.assertIn("episode-A1", export_text)

    def test_export_preserves_duplicate_header_order_and_redacts_each_cookie(self):
        headers = [("Content-Type", "application/json"), ("X-Synthetic-Observation", "first"),
            ("Set-Cookie", "synthetic-cookie-a=private-cookie-value-a; Path=/; HttpOnly"),
            ("X-Synthetic-Observation", "second"),
            ("set-cookie", "synthetic-cookie-b=private-cookie-value-b; Path=/; HttpOnly")]
        def duplicate_headers(server, request):
            if request.label == "PRE-P-profile":
                return server.wire(200, self.fixture.profile("P"), headers=headers)
            return None
        runner, transport = self.runner(failure=duplicate_headers)
        self.advance(runner, lambda value: "PRE-P-profile" in value.matrix.completed)
        self.assertEqual(len(transport.calls), 2)
        private_path = next((self.fixture.output / "private").glob("*-PRE-P-profile-wire.json"))
        export_path = next((self.fixture.output / "export").glob("*-PRE-P-profile-response.json"))
        private = json.loads(private_path.read_text())
        exported = json.loads(export_path.read_text())
        self.assertEqual(private["responseHeaders"], [list(pair) for pair in headers])
        expected = [[name, "[redacted]" if name.lower() == "set-cookie" else value] for name, value in headers]
        self.assertIsInstance(exported["response"]["headers"], list)
        self.assertEqual(exported["response"]["headers"], expected)
        self.assertNotIn("private-cookie-value-a", export_path.read_text())
        self.assertNotIn("private-cookie-value-b", export_path.read_text())

    def test_sanitizer_removes_nested_secrets_and_native_path_values(self):
        secrets = {"token-value", "session-value", "play-value", "source-value", "C:\\private\\native.mkv"}
        value = {"AccessToken": "token-value", "Nested": [{"SessionId": "session-value", "PlaySessionId": "play-value",
            "MediaSourceId": "source-value", "NativePath": "C:\\private\\native.mkv"}],
            "description": "token-value session-value play-value source-value C:\\private\\native.mkv", "public": "episode-A1"}
        result = self.T.sanitized(value, secrets)
        rendered = json.dumps(result)
        for secret in secrets:
            self.assertNotIn(secret, rendered)
            self.assertNotIn(secret, result["description"])
        self.assertEqual(result["public"], "episode-A1")
        self.assertEqual(value["AccessToken"], "token-value")


def main():
    global TRANSPORT_SOURCE, MATRIX_SOURCE
    if sys.platform != "linux" or not (os.environ.get("SSH_CONNECTION") or os.environ.get("SSH_TTY")):
        print(json.dumps({"suite": "nextup-global-transport-guards", "status": "blocked",
            "reason": "An authorized Linux SSH session is required; local verification is forbidden."}))
        return 2
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--transport-source", required=True, type=Path)
    parser.add_argument("--matrix-source", required=True, type=Path)
    parser.add_argument("--report-path", required=True, type=Path)
    arguments = parser.parse_args()
    TRANSPORT_SOURCE = arguments.transport_source.resolve(strict=True)
    MATRIX_SOURCE = arguments.matrix_source.resolve(strict=True)
    report_path = arguments.report_path.absolute()
    source_hashes = {"transport": digest(TRANSPORT_SOURCE.read_bytes()), "matrix": digest(MATRIX_SOURCE.read_bytes()),
                     "guards": digest(Path(__file__).read_bytes())}
    suite = unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(case)
                               for case in (TransportGuards, WireTransportGuards, CleanupCalibrationGuards))
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {"schemaVersion": 1, "suite": "nextup-global-transport-guards",
        "classification": "synthetic transport and persistence guards; not reference or client acceptance evidence",
        "sourceSha256": source_hashes, "testsRun": result.testsRun, "failures": len(result.failures),
        "errors": len(result.errors), "skips": len(result.skipped), "passed": result.wasSuccessful(),
        "failureDetails": [{"test": case.id(), "traceback": details} for case, details in result.failures],
        "errorDetails": [{"test": case.id(), "traceback": details} for case, details in result.errors],
        "skipDetails": [{"test": case.id(), "reason": reason} for case, reason in result.skipped],
        "actualBusinessHttpRequests": 0, "actualProcessProbes": 0, "liveAcceptanceClaim": False}
    report_path.parent.mkdir(parents=True, exist_ok=True)
    with report_path.open("x", encoding="utf-8") as handle:
        json.dump(report, handle, sort_keys=True, indent=2)
        handle.write("\n")
    report_path.chmod(0o600)
    print(json.dumps(report, sort_keys=True))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
