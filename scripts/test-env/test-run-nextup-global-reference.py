#!/usr/bin/env python3
"""Remote-only synthetic admission, replay, and matrix-entry guards.

Every test owns a new root-only temporary tree. The published preparation
fixture supplies synthetic inputs and wire responses; the actual preparation
source writes the complete private ledger and independently copied media tree.
Admission replays that ledger and the frozen TransportRunner executes an empty
global branch through an in-memory responder. No service, original executable,
database, real HTTP endpoint, or actual process is observed by this suite.
"""

from __future__ import annotations

import argparse
import base64
import builtins
from copy import deepcopy
from datetime import datetime, timedelta, timezone
import hashlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shlex
import socket
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit
import uuid


sys.dont_write_bytecode = True
SOURCE_BYTES = {}
PREPARATION_GUARDS = None
GRANT_WORKER_SHA256 = "70087cdaeae927c3091b5abcfc1df0c9203cdf35e17d28e5222f491bad46a971"
ACTORS = ("P", "Q")
EPISODES = ("A1", "A2", "A3", "B1", "B2", "B3")
RUNTIME_TICKS = 6_000_000_000
STAMP = "2026-09-13T01:00:00Z"
BLOCKED_ATTEMPTS = {"http": 0, "process": 0, "originalRead": 0, "authority": 0}


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"),
                       ensure_ascii=True, allow_nan=False) + "\n").encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def load_source(path, label):
    name = label + "_" + uuid.uuid4().hex
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def read_json(path):
    return json.loads(Path(path).read_bytes())


def write_json(path, value):
    Path(path).write_bytes(encoded(value))
    Path(path).chmod(0o600)
    return descriptor(path)


def descriptor(path):
    return {"path": str(path), "sha256": digest(Path(path).read_bytes())}


def zero_state(item):
    return {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0,
            "IsFavorite": False}


def complete_preparation_inputs(fixture):
    """Retain the full synthetic release schema used by ActualAuthorityGuards."""
    manifest = fixture.manifest
    sealed_root = Path(manifest["sealedRoots"][0])
    closed = {"userId": "old-user-1", "reportedDeviceId": "synthetic-prior-device",
        "tokenSha256": "c" * 64, "from": STAMP, "through": "2026-09-13T01:00:02Z"}
    for name, status in (("logout", 204), ("rejection", 401)):
        path = sealed_root / (name + ".json")
        fixture.write(path, encoded({"complete": True, "status": status, "token_sha256": "c" * 64,
            "completed_at": "2026-09-13T01:00:01Z"}))
        intent = sealed_root / (name + "-intent.json")
        fixture.write(intent, encoded({"channel": "controller_api", "method": "POST" if name == "logout" else "GET",
            "path": "/emby/Sessions/Logout" if name == "logout" else "/emby/Sessions"}))
        closed[name] = {"record": descriptor(path), "intent": descriptor(intent), "status": status,
            "tokenSha256": "c" * 64, "completedAt": "2026-09-13T01:00:01Z"}
    units = []
    for index, role in enumerate(("worker", "controller"), 1):
        name = "synthetic-" + role + ".service"
        group = "/system.slice/" + name
        units.append({"name": name, "invocationId": str(index) * 32,
            "properties": {"ActiveState": "inactive", "SubState": "dead", "MainPID": "0", "Result": "success",
                "ControlGroup": group}, "cgroupPath": "/sys/fs/cgroup" + group})
    inventory_path = sealed_root / "inventory.json"
    fixture.write(inventory_path, encoded({"syntheticPrivateScope": {"closed": True, "caseOwned": True}}))
    inventory_descriptor = descriptor(inventory_path)
    terminal_path = sealed_root / "terminal.json"
    fixture.write(terminal_path, encoded({"status": "protocol_observation_complete_independently_confirmed",
        "captured_at": "2026-09-13T01:00:02Z", "recursive_cgroups_empty": True,
        "full_target_restored_except_etag": True, "administrator_logout204_same_token401_verified": True,
        "viewer_logout204_same_token401_verified": True, "media_unchanged": True, "reference_database_read": False,
        "original_implementation_bytes_read": False, "scope_inventory": inventory_descriptor,
        "systemd": {row["name"]: {"InvocationID": row["invocationId"], **row["properties"]} for row in units}}))
    fixture.release = {"schemaVersion": 1, "kind": "nextup-global-preparation-release", "runId": manifest["runId"],
        "process": deepcopy(manifest["process"]), "lock": deepcopy(manifest["lock"]),
        "sealedRoots": list(manifest["sealedRoots"]), "releasedAt": "2026-09-13T01:00:03Z",
        "sealed": [{"kind": "terminal", "record": descriptor(terminal_path)},
                   {"kind": "inventory", "record": inventory_descriptor}],
        "units": units, "closedAuthentication": [closed]}
    fixture.baseline["devices"]["synthetic-prior-registry"] = {"Id": "synthetic-prior-registry",
        "ReportedDeviceId": "synthetic-prior-device", "LastUserId": "old-user-1"}
    approval = {"schemaVersion": 1, "kind": "nextup-global-preparation-media-approval",
        "source": deepcopy(manifest["media"]["source"]), "ownedRoot": manifest["media"]["ownedRoot"],
        "approvedReceipt": deepcopy(manifest["media"]["approvedReceipt"]),
        "approvedReceiptSha256": manifest["media"]["approvedReceiptSha256"], "durationSeconds": 600,
        "frameRate": 30, "originalImplementationBytesRead": False}
    for name, value in (("release", fixture.release), ("publicBaseline", fixture.baseline), ("mediaApproval", approval)):
        manifest["inputs"][name] = write_json(manifest["inputs"][name]["path"], value)
    PREPARATION_GUARDS.add_grant_verification_fixture(fixture, fixture.release)


class IOBoundary:
    """Fail before network construction, process inspection, or original reads."""

    def __init__(self, testcase):
        self.testcase = testcase
        self.forbidden = set()
        self.attempts = {key: 0 for key in BLOCKED_ATTEMPTS}
        for target, name, category in (
                (socket, "socket", "http"), (socket, "create_connection", "http"),
                (http.client, "HTTPConnection", "http"), (http.client, "HTTPSConnection", "http"),
                (subprocess, "run", "process"), (subprocess, "Popen", "process")):
            self.block(target, name, category)
        for target, name in ((builtins, "open"), (io, "open"), (os, "open"), (os, "readlink")):
            original = getattr(target, name)

            def guarded(path, *args, _original=original, **kwargs):
                if isinstance(path, (str, bytes, os.PathLike)):
                    candidate = Path(os.fsdecode(path))
                    if candidate.is_absolute():
                        if candidate == Path("/proc") or Path("/proc") in candidate.parents or \
                                candidate == Path("/sys/fs/cgroup") or Path("/sys/fs/cgroup") in candidate.parents:
                            self.deny("process")
                        if any(candidate == root or root in candidate.parents for root in self.forbidden):
                            self.deny("originalRead")
                return _original(path, *args, **kwargs)

            active = patch.object(target, name, side_effect=guarded)
            active.start()
            testcase.addCleanup(active.stop)

    def deny(self, category):
        self.attempts[category] += 1
        BLOCKED_ATTEMPTS[category] += 1
        raise AssertionError("Real I/O is forbidden in synthetic operator guards: " + category)

    def block(self, target, name, category):
        active = patch.object(target, name, side_effect=lambda *args, **kwargs: self.deny(category))
        active.start()
        self.testcase.addCleanup(active.stop)

    def module(self, module):
        for name in ("process_identity", "_process_metadata", "read_unit", "empty_cgroup", "runtime_identity"):
            if hasattr(module, name):
                self.block(module, name, "process")
        if hasattr(module, "HTTPTransport"):
            self.block(module.HTTPTransport, "send", "http")


class OperatorFixture:
    """Produce actual source-bound bytes, then publish only fixtureReleased."""

    def __init__(self, testcase, boundary, *, broad_matrix_parent=False, scan_ready_round=0):
        self.testcase, self.boundary = testcase, boundary
        self.prepared = PREPARATION_GUARDS.PreparationFixture(testcase)
        fixture = self.prepared
        boundary.forbidden.update(Path(path) for path in fixture.manifest["forbiddenOriginalRoots"])
        self.run_parent = fixture.root / "nextup-global-reference-runs-03"
        self.run_parent.mkdir(mode=0o700)
        matrix_parent = fixture.root if broad_matrix_parent else self.run_parent
        run_scope = {"evidenceParent": str(matrix_parent), "matrixEvidenceRoot": str(matrix_parent / "matrix-03")}
        fixture.scope.update(run_scope)
        fixture.manifest["scope"].update(run_scope)
        complete_preparation_inputs(fixture)
        self.root, self.producer_root = fixture.root, fixture.output
        self.private = self.producer_root / "private"
        self.operator_source = Path(fixture.scope["sourceRoot"]) / "run-nextup-global-reference.py"
        fixture.write(self.operator_source, SOURCE_BYTES["operator"])
        self.operator = load_source(self.operator_source, "matrix_entry_guard_subject")
        testcase.addCleanup(sys.modules.pop, self.operator.__name__, None)
        for module in (self.operator, fixture.module, fixture.support):
            boundary.module(module)
        stage_media = fixture.module.Authority.stage_media
        boundary.block(fixture.module, "Authority", "authority")

        class CompleteSyntheticAuthority(PREPARATION_GUARDS.FakeAuthority):
            def stage_media(self, journal):
                if self.staged:
                    raise AssertionError("Synthetic media cannot be staged twice.")
                self.staged = True
                return stage_media(self, journal)

        self.input_manifest = Path(fixture.scope["inputRoot"]) / "cli-manifest.json"
        fixture.write(self.input_manifest, encoded(fixture.manifest))
        self.preparation_authority = CompleteSyntheticAuthority(fixture)
        if type(scan_ready_round) is not int or not 0 <= scan_ready_round <= 10:
            raise ValueError("The synthetic scan must leave two stable rounds within the frozen twelve-round plan.")

        def observed_scan_progress(wire, call, response):
            label = call["request"].label
            if label.startswith("scan-") and label.endswith("-libraries") and int(label.split("-")[1]) < scan_ready_round:
                body = json.loads(response.raw)
                for row in body["Items"]:
                    if row["ItemId"] in fixture.library_ids.values(): row["RefreshStatus"] = "Running"
                return fixture.support.WireResponse(response.status, response.headers, encoded(body), response.complete_http, response.completed_at, response.failure)
            return response

        self.preparation_wire = PREPARATION_GUARDS.FakeWire(fixture, response_hook=observed_scan_progress)
        self.preparation_runner = fixture.module.PreparationRunner(fixture.manifest,
            authority=self.preparation_authority, transport=self.preparation_wire,
            journal_factory=lambda root, uid: fixture.support.Journal(root, uid=uid),
            monotonic=fixture.clock, sleeper=fixture.clock.sleep, utc_now=fixture.clock.utc_now)
        testcase.addCleanup(self.preparation_authority.close)
        self.terminal = self.preparation_runner.run()
        testcase.assertEqual(self.terminal["status"], "awaiting_independent_attestation",
                             "The actual synthetic producer must succeed before admission is exercised.")
        testcase.assertTrue(self.terminal["completed"])
        testcase.assertEqual(self.terminal["requestCount"], len(self.preparation_wire.calls))
        testcase.assertEqual(len(read_json(self.private / "media-tree.json")["files"]), 16)
        self.attestation_root = self.root / "independent-attestation"
        self.operator_parent = self.run_parent
        self.attestation_root.mkdir(mode=0o700)
        self.operator_output = self.operator_parent / "operator-03"
        self.matrix_output = Path(fixture.scope["matrixEvidenceRoot"])
        self.execution_path = self.attestation_root / "published-execution.json"
        self.execution = read_json(self.private / "draft-execution.json")
        self.execution["matrix"]["binding"]["fixtureReleased"] = True
        fixture.write(self.execution_path, encoded(self.execution))
        self.index_path = self.attestation_root / "wire-index.json"
        self.index = {"schemaVersion": 1, "kind": "nextup-global-preparation-wire-index",
            "runId": fixture.manifest["runId"], "producerRoot": str(self.producer_root), "requests": []}
        for path in sorted(self.private.glob("*-intent.json")):
            intent = read_json(path)
            stem = "%04d-%s" % (intent["ordinal"], intent["label"])
            self.index["requests"].append({"ordinal": intent["ordinal"], "label": intent["label"],
                **{kind: descriptor(self.private / (stem + "-" + kind + ".json"))
                   for kind in ("intent", "reserved", "response")}})
        testcase.assertEqual(len(self.index["requests"]), self.terminal["requestCount"])
        fixture.write(self.index_path, encoded(self.index))
        plan_sha = digest(fixture.module.canonical(read_json(self.private / "frozen-plan.json")).encode())
        command = ["/usr/bin/python3", "-I", "-B", str(fixture.paths["preparation"]), "prepare",
            "--manifest", str(self.input_manifest), "--manifest-sha256", descriptor(self.input_manifest)["sha256"],
            "--plan-sha256", plan_sha]
        self.preparation_unit = "synthetic-preparation.service"
        self.matrix_unit = "synthetic-matrix.service"
        shutdown_properties = {"ActiveState": "active", "SubState": "exited", "MainPID": "0",
            "Result": "success", "ExecMainCode": "1", "ExecMainStatus": "0",
            "ControlGroup": "/system.slice/" + self.preparation_unit, "RemainAfterExit": "yes",
            "ExecStart": "{ path=/usr/bin/python3 ; argv[]=" + shlex.join(command) + " ; ignore_errors=no ; }"}
        runtime_properties = {"Type": "oneshot", "RemainAfterExit": "yes", "Restart": "no", "User": "root",
            "UMask": "0077", "NoNewPrivileges": "yes", "ProtectSystem": "strict", "ProtectHome": "yes",
            "PrivateTmp": "yes", "PrivateNetwork": "no", "TimeoutStartUSec": "30min", "MemoryMax": "536870912",
            "TasksMax": "32", "LimitNOFILE": "4096", "ReadWritePaths": self.writable_paths(self.operator_parent)}
        self.current = {"pid": 45678, "invocationId": "c" * 32,
            "cgroup": "0::/system.slice/" + self.matrix_unit + "\n", "ssh": True,
            "isolated": True, "noBytecode": True, "uid": 0}
        self.unit_values = {
            self.preparation_unit: {"InvocationID": "b" * 32, **shutdown_properties},
            self.matrix_unit: {**runtime_properties, "InvocationID": self.current["invocationId"],
                "MainPID": str(self.current["pid"]), "ActiveState": "activating", "SubState": "start",
                "ControlGroup": "/system.slice/" + self.matrix_unit}}
        self.unit_calls, self.cgroup_calls, self.self_calls, self.target_calls = [], [], [], []
        self.cgroup_empty = True
        self.factory_calls = 0
        self.runners, self.matrix_wires = [], []
        self.attestation_path = self.attestation_root / "attestation.json"
        self.attestation = {"schemaVersion": 1, "kind": "nextup-global-reference-matrix-attestation",
            "runId": fixture.manifest["runId"], "attestedAt": "2026-09-13T02:00:00Z",
            "execution": descriptor(self.execution_path),
            "sources": {"operator": descriptor(self.operator_source),
                **{role: descriptor(fixture.paths[role]) for role in ("preparation", "transport", "matrix")}},
            "preparation": {"root": str(self.producer_root), "inputManifest": descriptor(self.input_manifest),
                "wireIndex": descriptor(self.index_path),
                **{key: descriptor(self.private / filename) for key, filename in self.operator.PREPARATION_FILES.items()}},
            "shutdown": {"observedAt": "2026-09-13T01:59:00Z", "units": [{"name": self.preparation_unit,
                "invocationId": "b" * 32, "properties": shutdown_properties,
                "cgroupPath": "/sys/fs/cgroup/system.slice/" + self.preparation_unit}]},
            "runtime": {"unitName": self.matrix_unit, "properties": runtime_properties},
            "scope": {"attestationRoot": str(self.attestation_root), "sourceRoot": fixture.scope["sourceRoot"],
                "operatorEvidenceRoot": str(self.operator_output)},
            "sealedRoots": list(fixture.manifest["sealedRoots"]),
            "forbiddenOriginalRoots": list(fixture.manifest["forbiddenOriginalRoots"])}
        fixture.write(self.attestation_path, encoded(self.attestation))

    def writable_paths(self, operator_parent):
        parents = {Path(operator_parent), Path(self.execution["scope"]["evidenceParent"])}
        writable = {str(parent) for parent in parents}
        lock = Path(self.prepared.lock["path"])
        if not any(parent in lock.parents for parent in parents):
            writable.add(str(lock))
        return shlex.join(sorted(writable))

    def unit_probe(self, name, fields):
        self.testcase.assertIn(name, (self.preparation_unit, self.matrix_unit))
        self.unit_calls.append((name, set(fields)))
        return {key: self.unit_values[name][key] for key in fields}

    def cgroup_probe(self, path):
        self.testcase.assertEqual(path, "/sys/fs/cgroup/system.slice/" + self.preparation_unit)
        self.cgroup_calls.append(path)
        return self.cgroup_empty

    def self_probe(self):
        self.self_calls.append(deepcopy(self.current))
        return deepcopy(self.current)

    def target_probe(self, expected):
        self.testcase.assertEqual(expected, self.prepared.process)
        self.target_calls.append(deepcopy(expected))
        return deepcopy(self.prepared.process)

    def save_attestation(self):
        self.attestation_path.write_bytes(encoded(self.attestation))
        return descriptor(self.attestation_path)["sha256"]

    def save_index(self):
        self.attestation["preparation"]["wireIndex"] = write_json(self.index_path, self.index)

    def replace_document(self, key, value):
        path = self.attestation["preparation"][key]["path"]
        self.attestation["preparation"][key] = write_json(path, value)

    def replace_execution(self, value):
        self.execution = deepcopy(value)
        self.attestation["execution"] = write_json(self.execution_path, value)

    def replace_wire(self, row, kind, value):
        row[kind] = write_json(row[kind]["path"], value)
        self.save_index()

    def admit(self, *, checksum=None):
        actual = self.save_attestation()
        admission = self.operator.Admission(str(self.attestation_path), checksum or actual,
            unit_probe=self.unit_probe, cgroup_probe=self.cgroup_probe, self_probe=self.self_probe)
        for module in (admission.preparation, admission.transport, admission.matrix):
            self.testcase.addCleanup(sys.modules.pop, module.__name__, None)
        self.boundary.module(admission.support)
        return admission

    def actual_factory(self, admission, *, response_hook=None, after_construct=None):
        def factory(execution, *, probe):
            self.factory_calls += 1
            wire = MatrixWire(self, admission.support, response_hook=response_hook)
            clock = PREPARATION_GUARDS.FakeClock()
            runner = admission.support.TransportRunner(execution, transport=wire, probe=probe,
                monotonic=clock, sleeper=clock.sleep)
            wire.runner = runner
            self.runners.append(runner)
            self.matrix_wires.append(wire)
            self.testcase.addCleanup(runner.authority.close)
            self.testcase.addCleanup(runner.journal.close)
            self.testcase.addCleanup(sys.modules.pop, runner.module.__name__, None)
            if after_construct is not None:
                after_construct(runner)
            return runner
        return factory

    def retained_factory(self, admission, **options):
        def factory(execution, *, probe):
            self.factory_calls += 1
            runner = RetainedResultRunner(self, admission.support, execution, probe=probe, **options)
            self.runners.append(runner)
            return runner
        return factory


class MatrixWire:
    """Answer the frozen matrix with independent state and empty global lists."""

    def __init__(self, fixture, support, *, response_hook=None):
        self.fixture, self.support, self.response_hook = fixture, support, response_hook
        self.runner = None
        self.calls = []
        self.tokens = {actor: "synthetic-private-matrix-token-" + actor for actor in ACTORS}
        self.sessions = {actor: "synthetic-matrix-session-" + actor for actor in ACTORS}
        self.states = {actor: {item: zero_state(item) for item in EPISODES} for actor in ACTORS}
        self.stop_count = 0

    def wire(self, status, body=None, *, complete=True, failure=None):
        return self.support.WireResponse(status, [("Content-Type", "application/json")],
            b"" if body is None else encoded(body), complete, STAMP, failure)

    def send(self, request, headers, payload, *, timeout_seconds, max_bytes):
        test = self.fixture.testcase
        matrix = self.fixture.execution["matrix"]
        budgets = self.fixture.execution["budgets"]
        test.assertTrue(0 < timeout_seconds <= budgets["requestSeconds"])
        test.assertEqual(max_bytes, budgets["responseBytes"] + 1)
        self.calls.append({"request": request, "headers": deepcopy(headers), "payload": payload})
        ordinal = self.runner.matrix.count
        stem = "%04d-%s" % (ordinal, request.label)
        private = self.fixture.matrix_output / "private"
        for suffix in ("-intent.json", "-reserved.json"):
            test.assertTrue((private / (stem + suffix)).is_file())
        test.assertEqual(read_json(private / "state.json")["matrix"]["pending"]["request"], request.fact())
        actor = matrix["actors"][request.actor]
        if request.login:
            test.assertNotIn("X-Emby-Token", headers)
            credential = self.fixture.prepared.credentials[request.actor]
            test.assertEqual(parse_qs(payload.decode()),
                             {"Username": [credential["username"]], "Pw": [credential["password"]]})
        else:
            test.assertEqual(headers.get("X-Emby-Token"), self.tokens[request.actor])
        if self.response_hook is not None:
            replacement = self.response_hook(self, request)
            if replacement is not None:
                return replacement
        step = self.runner.matrix.queue[0]
        if request.login:
            return self.wire(200, {"AccessToken": self.tokens[request.actor], "ServerId": matrix["serverId"],
                "User": {"Id": actor["userId"], "Name": actor["username"], "Policy": {"IsAdministrator": False}},
                "SessionInfo": {"Id": self.sessions[request.actor], "UserId": actor["userId"], "DeviceId": actor["deviceId"]}})
        if step.kind == "profile":
            return self.wire(200, {"Id": actor["userId"], "Name": actor["username"],
                "Policy": {"IsAdministrator": False, "EnableAllFolders": False,
                    "EnabledFolders": list(actor["allowedFolderIds"])}, "Configuration": {"Order": ["tv"]}})
        if step.kind == "preferences":
            return self.wire(200, {"home": "tv", "syntheticPreference": {"preserve": False}})
        if step.kind == "playback-info":
            return self.wire(200, {"PlaySessionId": "synthetic-matrix-play-" + step.lifecycle,
                "MediaSources": [{"Id": "synthetic-source-" + step.item, "RunTimeTicks": RUNTIME_TICKS,
                    "Path": self.fixture.prepared.media["L" + step.item[0]]["files"][step.item]["path"]}]})
        if step.kind in ("started", "progress", "stopped", "cleanup-stop"):
            if step.kind in ("stopped", "cleanup-stop"):
                self.stop_count += 1
                position = json.loads(payload)["PositionTicks"]
                state = self.states[request.actor][step.item]
                date = datetime(2026, 9, 13, 1, tzinfo=timezone.utc) + timedelta(seconds=self.stop_count)
                state.update(Played=position == RUNTIME_TICKS, PlayCount=state["PlayCount"] + 1,
                    PlaybackPositionTicks=0 if position == RUNTIME_TICKS else position, LastPlayedDate=date.isoformat())
                if 0 < position < RUNTIME_TICKS:
                    state["PlayedPercentage"] = 100 * position / RUNTIME_TICKS
                else:
                    state.pop("PlayedPercentage", None)
            return self.wire(204)
        if step.kind in ("reset", "cleanup-reset"):
            self.states[request.actor][step.item] = zero_state(step.item)
            return self.wire(200, deepcopy(self.states[request.actor][step.item]))
        if step.kind == "logout":
            return self.wire(204)
        if step.kind == "token-invalid":
            return self.wire(401, {"error": "synthetic retired matrix token"})
        if step.kind == "nextup":
            query = parse_qs(urlsplit(request.route).query)
            symbols = []
            if "SeriesId" in query and step.stage != "EXT":
                series = next(name for name in ("A", "B") if matrix["items"][name]["id"] == query["SeriesId"][0])
                active = [index for index in (1, 2, 3) if self.states[request.actor][series + str(index)]["PlayCount"]]
                if active:
                    symbols = [series + str(index) for index in range(max(active), 4)
                               if not self.states[request.actor][series + str(index)]["Played"]]
            return self.wire(200, {"Items": [{"Id": matrix["items"][item]["id"], "Type": "Episode",
                "UserData": deepcopy(self.states[request.actor][item])} for item in symbols], "TotalRecordCount": len(symbols)})
        test.assertIsNotNone(step.item, "An unenumerated matrix request reached the synthetic wire.")
        mapped = matrix["items"][step.item]
        body = {"Id": mapped["id"], "Type": mapped["type"], "ParentId": mapped["parentId"]}
        if step.item in EPISODES:
            body.update(SeriesId=mapped["seriesId"], ParentIndexNumber=mapped["parentIndexNumber"],
                IndexNumber=mapped["indexNumber"], RunTimeTicks=RUNTIME_TICKS,
                UserData=deepcopy(self.states[request.actor][step.item]))
        else:
            body["UserData"] = {"Played": False, "PlayCount": 0, "UnplayedItemCount": 3}
        return self.wire(200, body)


class RetainedResultRunner:
    """Explicit file-output double for the operator's post-run receipt gates.

This double never claims transport coverage. It creates fresh private/state.json
and export/result.json with the same Journal used by the frozen transport, so
each refusal can target one post-run predicate independently of HTTP behavior.
"""

    def __init__(self, fixture, support, execution, *, probe, state_change=None,
                 result_change=None, retained_change=None, omit=None, after_run=None):
        self.fixture, self.support, self.execution, self.probe = fixture, support, deepcopy(execution), probe
        self.state_change, self.result_change, self.retained_change = state_change, result_change, retained_change
        self.omit, self.after_run = omit, after_run
        self.secrets = {"synthetic-private-result-token"}

    def run(self):
        self.probe(self.execution["process"])
        result = {"mode": "closed", "failure": None, "cleanupComplete": True, "evidenceComplete": True,
            "httpAttempts": 0, "liveAcceptanceClaim": False, "privateToken": "synthetic-private-result-token"}
        state = {"blocked": False, "finished": True, "unverifiedLogins": {}, "unresolvedResponses": [],
                 "matrix": {"pending": None, "revokedActors": ["P", "Q"]}}
        if self.state_change is not None:
            self.state_change(state)
        if self.result_change is not None:
            self.result_change(result)
        retained = self.support.sanitized(deepcopy(result), self.secrets)
        if self.retained_change is not None:
            self.retained_change(retained)
        journal = self.support.Journal(self.fixture.matrix_output, uid=0)
        try:
            if self.omit != "state":
                journal.state(state)
            if self.omit != "result":
                journal.save("result.json", retained, export=True)
        finally:
            journal.close()
        if self.after_run is not None:
            self.after_run()
        return deepcopy(result)


class GuardCase(unittest.TestCase):
    fixture_options = {}

    def setUp(self):
        self.boundary = IOBoundary(self)
        self.loaded_entry_modules = {name: module for name, module in sys.modules.items()
                                     if name.startswith(("matrix_entry_", "nextup_matrix_"))}
        self.addCleanup(self.restore_entry_modules)
        self.fixture = OperatorFixture(self, self.boundary, **self.fixture_options)
        self.O = self.fixture.operator

    def restore_entry_modules(self):
        for name in list(sys.modules):
            if name.startswith(("matrix_entry_", "nextup_matrix_")):
                sys.modules.pop(name, None)
        sys.modules.update(self.loaded_entry_modules)

    def tearDown(self):
        self.assertEqual(self.boundary.attempts, {key: 0 for key in BLOCKED_ATTEMPTS},
                         "Even a denied real-I/O attempt invalidates synthetic coverage.")

    def reject(self, *, pattern=None, fresh=True, checksum=None):
        errors = (self.O.OperatorError, OSError, ValueError, TypeError, KeyError)
        if pattern is None:
            with self.assertRaises(errors):
                self.fixture.admit(checksum=checksum)
        else:
            with self.assertRaisesRegex(errors, pattern):
                self.fixture.admit(checksum=checksum)
        self.assertEqual(self.fixture.factory_calls, 0)
        if fresh:
            self.assertFalse(self.fixture.operator_output.exists())
            self.assertFalse(self.fixture.matrix_output.exists())

    def reject_attestation(self, mutate, *, pattern=None):
        original = deepcopy(self.fixture.attestation)
        try:
            mutate(self.fixture.attestation)
            self.reject(pattern=pattern)
        finally:
            self.fixture.attestation = original

    def execute(self, admission, *, actual=False, **options):
        factory = self.fixture.actual_factory(admission, **options) if actual else self.fixture.retained_factory(admission, **options)
        return self.O.execute(admission, target_probe=self.fixture.target_probe, runner_factory=factory)

    def assert_recovery(self, terminal):
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertFalse(terminal["cleanupComplete"])
        self.assertFalse(terminal["clientAcceptanceClaim"])
        self.assertFalse(terminal["resumeOrRetryAllowed"])
        self.assertTrue(terminal["independentRuntimeClosureRequired"])


class AdmissionGuards(GuardCase):
    def test_complete_actual_producer_ledger_replays_every_private_byte(self):
        before = {str(path): path.read_bytes() for path in self.fixture.private.iterdir()}
        admission = self.fixture.admit()
        self.assertEqual(admission.replay_count, self.fixture.terminal["requestCount"])
        self.assertEqual(admission.replay_count, len(self.fixture.index["requests"]))
        self.assertEqual(before, {str(path): path.read_bytes() for path in self.fixture.private.iterdir()})
        self.assertFalse(admission.documents["draftExecution"]["matrix"]["binding"]["fixtureReleased"])
        self.assertTrue(admission.execution["matrix"]["binding"]["fixtureReleased"])
        self.assertTrue(self.fixture.unit_calls and self.fixture.cgroup_calls and self.fixture.self_calls)
        self.assertFalse(self.fixture.target_calls)
        self.assertFalse(self.fixture.operator_output.exists())
        self.assertFalse(self.fixture.matrix_output.exists())
        admission.checkpoint(full=True)

    def test_short_success_uses_source_limits_without_requiring_maximum_count(self):
        admission = self.fixture.admit()
        plan = admission.preparation.frozen_plan(admission.manifest)
        self.assertEqual((admission.producer_normal_maximum, admission.producer_success_maximum),
                         (plan["normalMaximum"], plan["successMaximumIncludingLogout"]))
        self.assertEqual((plan["normalMaximum"], plan["successMaximumIncludingLogout"]), (257, 263))
        self.assertLess(self.fixture.terminal["requestCount"], plan["successMaximumIncludingLogout"])
        self.assertEqual(self.fixture.terminal["requestCount"], self.fixture.terminal["normalRequestCount"] + 6)
        self.assertEqual(admission.replay_count, self.fixture.terminal["requestCount"])
        self.assertEqual((admission.matrix.MAX_REQUESTS, admission.matrix.NORMAL_LIMIT, admission.matrix.CLEANUP_RESERVE), (300, 220, 80))
        cleanup = read_json(admission.documents["draftExecution"]["receipts"]["cleanup"]["path"])["facts"]
        self.assertEqual(cleanup["contractVersion"], 3)
        receipts = {row["login"]["responseReceiptSha256"] for row in cleanup["actors"].values()}
        for row in cleanup["calibrations"]:
            for name in ("beforeZero", "playbackInfo", "started", "progress", "stopped", "beforeDelete", "delete", "afterDelete"):
                receipts.add(row[name]["responseReceiptSha256"])
            self.assertEqual(row["stopped"]["response"]["status"], 204)
            if row["mode"] == "partial":
                self.assertEqual(row["beforeDelete"]["response"]["body"]["UserData"]["PlayedPercentage"], 20)
                self.assertNotIn("PlayedPercentage", row["afterDelete"]["response"]["body"]["UserData"])
        self.assertEqual(len(receipts), 34)

    def test_source_plan_success_limits_require_integer_capacity_and_six_closures(self):
        plan = self.fixture.prepared.module.frozen_plan(self.fixture.prepared.manifest)
        changes = ({"normalMaximum": True}, {"normalMaximum": 0}, {"successMaximumIncludingLogout": "263"},
                   {"normalMaximum": plan["normalLimit"] + 1}, {"cleanupReserve": 5},
                   {"successMaximumIncludingLogout": plan["normalMaximum"] + 5},
                   {"successMaximumIncludingLogout": plan["normalMaximum"] + 7},
                   {"maximumRequests": plan["maximumRequests"] + 1})
        for change in changes:
            with self.subTest(change=change), self.assertRaises(self.O.OperatorError):
                self.O.successful_preparation_limits({**deepcopy(plan), **change})

    def test_updated_descriptor_cannot_forge_source_plan_request_maxima(self):
        original = read_json(self.fixture.private / "frozen-plan.json")
        for change in ({"normalMaximum": original["normalMaximum"] + 1, "successMaximumIncludingLogout": original["successMaximumIncludingLogout"] + 1},
                       {"normalMaximum": 232, "successMaximumIncludingLogout": 238},
                       {"normalMaximum": original["normalMaximum"] - 1, "successMaximumIncludingLogout": original["successMaximumIncludingLogout"] - 1}):
            with self.subTest(change=change):
                self.fixture.replace_document("plan", {**deepcopy(original), **change})
                self.reject(pattern="source-bound actual plan")

    def test_semantically_identical_plan_still_requires_original_producer_bytes(self):
        path = self.fixture.private / "frozen-plan.json"
        path.write_bytes(b" " + path.read_bytes())
        self.fixture.attestation["preparation"]["plan"] = descriptor(path)
        self.reject(pattern="private output cannot be reproduced.*frozen-plan")

    def test_terminal_and_state_cannot_exceed_recomputed_normal_success_maxima(self):
        plan = self.fixture.prepared.module.frozen_plan(self.fixture.prepared.manifest)
        terminal, state = read_json(self.fixture.private / "terminal.json"), read_json(self.fixture.private / "state.json")
        for change in ({"normalRequestCount": plan["normalMaximum"] + 1, "requestCount": plan["successMaximumIncludingLogout"] + 1},
                       {"normalRequestCount": -1, "requestCount": 5}, {"normalRequestCount": True, "requestCount": 7}):
            with self.subTest(change=change):
                self.fixture.replace_document("terminal", {**deepcopy(terminal), **change})
                self.fixture.replace_document("state", {**deepcopy(state), **change})
                self.reject(pattern="request budget does not close")

    def test_successful_cleanup_requires_exactly_six_in_all_recorded_counters(self):
        terminal, state = read_json(self.fixture.private / "terminal.json"), read_json(self.fixture.private / "state.json")
        for cleanup in (5, 7, True, "6", 6.0):
            with self.subTest(cleanup=cleanup):
                candidate = deepcopy(terminal)
                candidate.update(cleanupRequestCount=cleanup, requestCount=candidate["normalRequestCount"] + (cleanup if type(cleanup) is int else 6))
                candidate["phaseRequestCounts"]["cleanup"] = cleanup
                saved = {**deepcopy(state), **{key: candidate[key] for key in ("requestCount", "normalRequestCount", "cleanupRequestCount")}}
                self.fixture.replace_document("terminal", candidate); self.fixture.replace_document("state", saved)
                self.reject(pattern="request budget does not close")

    def test_success_summary_counts_cannot_replace_actual_wire_population(self):
        terminal, state = read_json(self.fixture.private / "terminal.json"), read_json(self.fixture.private / "state.json")
        for delta in (-1, 1):
            with self.subTest(delta=delta):
                change = {"normalRequestCount": terminal["normalRequestCount"] + delta, "requestCount": terminal["requestCount"] + delta}
                self.fixture.replace_document("terminal", {**deepcopy(terminal), **change})
                self.fixture.replace_document("state", {**deepcopy(state), **change})
                self.reject(pattern="complete actual preparation wire index")

    def test_attestation_checksum_and_exact_shape_are_required(self):
        self.reject(checksum="0" * 64, pattern="digest")
        for mutate in (lambda value: value.update(schemaVersion=True),
                       lambda value: value.update(kind="preparation-summary"),
                       lambda value: value.update(runId="different-run"),
                       lambda value: value.update(unreviewed=True),
                       lambda value: value["preparation"].pop("wireIndex")):
            with self.subTest(mutation=mutate.__code__.co_firstlineno):
                self.reject_attestation(mutate)

    def test_failed_producer_terminal_cannot_be_published(self):
        original = read_json(self.fixture.private / "terminal.json")
        for change in ({"completed": False}, {"status": "recovery_required"}, {"cleanupComplete": False},
                       {"uncertain": True}, {"pending": {"label": "unresolved"}},
                       {"ownershipPending": {"kind": "user"}}, {"failure": {"type": "SyntheticFailure"}},
                       {"cleanupErrors": [{"stage": "logout"}]}, {"matrixInputsUsable": True}):
            with self.subTest(change=change):
                candidate = deepcopy(original)
                candidate.update(change)
                self.fixture.replace_document("terminal", candidate)
                self.reject(pattern="complete safely")

    def test_success_summary_forgery_does_not_replace_source_replay(self):
        terminal = read_json(self.fixture.private / "terminal.json")
        terminal["inventedSuccessSummary"] = {"allRecordsVerified": True}
        self.fixture.replace_document("terminal", terminal)
        self.reject(pattern="raw ledger")

    def test_semantically_identical_private_json_still_requires_exact_bytes(self):
        path = self.fixture.private / "before-public.json"
        path.write_bytes(b" " + path.read_bytes())
        self.reject(pattern="private output cannot be reproduced")

    def test_raw_response_tamper_with_updated_descriptor_is_rejected_by_replay(self):
        row = next(row for row in self.fixture.index["requests"]
                   if read_json(row["intent"]["path"])["request"]["route"] == "/emby/System/Configuration")
        response = read_json(row["response"]["path"])
        body = json.loads(base64.b64decode(response["rawBase64"]))
        body["SyntheticConfiguration"]["preserve"] = False
        raw = encoded(body)
        response.update(rawBase64=base64.b64encode(raw).decode(), observedRawBytes=len(raw))
        self.fixture.replace_wire(row, "response", response)
        self.reject(pattern="raw ledger")

    def test_token_substitution_across_all_three_records_cannot_forge_dispatch(self):
        row = next(row for row in self.fixture.index["requests"]
                   if any(pair[0] == "X-Emby-Token" for pair in read_json(row["intent"]["path"])["request"]["headers"]))
        values = {kind: read_json(row[kind]["path"]) for kind in ("intent", "reserved", "response")}
        for request in (values["intent"]["request"], values["reserved"]["pending"]["request"], values["response"]["request"]):
            next(pair for pair in request["headers"] if pair[0] == "X-Emby-Token")[1] = "forged-unowned-token"
        values["intent"]["tokenSha256"] = digest(b"forged-unowned-token")
        self.fixture.replace_wire(row, "intent", values["intent"])
        values["reserved"]["pending"]["intentSha256"] = row["intent"]["sha256"]
        for kind in ("reserved", "response"):
            self.fixture.replace_wire(row, kind, values[kind])
        self.reject(pattern="raw ledger")

    def test_missing_or_extra_wire_index_rows_are_rejected(self):
        original = deepcopy(self.fixture.index)
        for rows in (original["requests"][:-1], original["requests"] + [deepcopy(original["requests"][-1])]):
            with self.subTest(length=len(rows)):
                self.fixture.index = {**deepcopy(original), "requests": rows}
                self.fixture.save_index()
                self.reject(pattern="complete actual preparation wire index")

    def test_wire_index_cannot_hide_an_extra_private_request_record(self):
        self.fixture.prepared.write(self.fixture.private / "9999-unindexed-intent.json", encoded({"unindexed": True}))
        self.reject(pattern="additional to the complete wire index")

    def test_missing_wire_record_is_rejected_before_execution(self):
        Path(self.fixture.index["requests"][0]["reserved"]["path"]).unlink()
        self.reject()

    def test_unaccounted_private_file_is_rejected_after_replay(self):
        self.fixture.prepared.write(self.fixture.private / "unaccounted-summary.json", encoded({"complete": True}))
        self.reject(pattern="unaccounted files")

    def test_forged_private_state_tokens_do_not_replace_replayed_state(self):
        state = read_json(self.fixture.private / "state.json")
        state["tokens"]["P"] = "invented-success-summary-token"
        self.fixture.replace_document("state", state)
        self.reject(pattern="private output cannot be reproduced")

    def test_full_release_and_approved_media_evidence_remain_pinned(self):
        paths = [Path(self.fixture.prepared.release["sealed"][0]["record"]["path"]),
                 Path(self.fixture.prepared.release["closedAuthentication"][0]["rejection"]["record"]["path"]),
                 Path(self.fixture.prepared.manifest["media"]["approvedReceipt"]["path"])]
        for path in paths:
            with self.subTest(record=path.name):
                original = path.read_bytes()
                try:
                    path.write_bytes(original + b" ")
                    self.reject(pattern="digest")
                finally:
                    path.write_bytes(original)

    def test_external_attestation_cannot_omit_producer_sealed_roots(self):
        unrelated = self.fixture.root / "unrelated-sealed-root"
        unrelated.mkdir(mode=0o700)
        self.reject_attestation(lambda value: value.update(sealedRoots=[str(unrelated)]), pattern="omitted a producer sealed-root")

    def test_wire_status_framing_payload_and_time_must_be_complete(self):
        row = self.fixture.index["requests"][0]
        original = read_json(row["response"]["path"])
        for change in ({"completeHttp": False}, {"failure": "synthetic lost response"},
                       {"retainedRawTruncated": True}, {"status": True}, {"observedRawBytes": 0},
                       {"payloadBase64": None}, {"completedAt": "2026-09-13T03:00:00Z"}):
            with self.subTest(change=change):
                candidate = deepcopy(original)
                candidate.update(change)
                self.fixture.replace_wire(row, "response", candidate)
                self.reject()

    def test_only_fixture_released_may_change_from_the_producer_draft(self):
        original = deepcopy(self.fixture.execution)
        changes = (lambda value: value["matrix"]["binding"].update(fixtureReleased=False),
                   lambda value: value["matrix"]["binding"].update(freshEvidenceRoot=False),
                   lambda value: value["budgets"].update(requestSeconds=4),
                   lambda value: value["matrix"]["actors"]["P"].update(deviceId="forged-device"),
                   lambda value: value["endpoint"].update(port=18198),
                   lambda value: value.update(ownerUid=1))
        for mutate in changes:
            with self.subTest(mutation=mutate.__code__.co_firstlineno):
                candidate = deepcopy(original)
                mutate(candidate)
                self.fixture.replace_execution(candidate)
                self.reject(pattern="Only independently attested fixtureReleased")

    def test_private_files_directories_symlinks_and_hardlinks_are_rejected(self):
        for path in (self.fixture.attestation_path, self.fixture.execution_path,
                     self.fixture.private / "terminal.json", self.fixture.private, self.fixture.attestation_root):
            with self.subTest(path=path.name):
                previous = stat.S_IMODE(path.stat().st_mode)
                try:
                    path.chmod(0o750 if path.is_dir() else 0o640)
                    self.reject(pattern="owner-only")
                finally:
                    path.chmod(previous)
        path = self.fixture.private / "before-public.json"
        renamed = self.fixture.attestation_root / "synthetic-held-before.json"
        path.rename(renamed)
        path.symlink_to(renamed)
        self.reject(pattern="symlinks")
        path.unlink()
        renamed.rename(path)
        extra = self.fixture.attestation_root / "linked-before.json"
        os.link(path, extra)
        self.reject(pattern="hard links")

    def test_source_pins_and_operator_path_bind_the_actual_loaded_bytes(self):
        for role in ("operator", "preparation", "transport", "matrix"):
            with self.subTest(role=role):
                self.reject_attestation(lambda value: value["sources"][role].update(sha256="0" * 64), pattern="digest")
        alternate = Path(self.fixture.prepared.scope["sourceRoot"]) / "alternate"
        alternate.mkdir(mode=0o700)
        path = alternate / "run-nextup-global-reference.py"
        self.fixture.prepared.write(path, SOURCE_BYTES["operator"])
        self.reject_attestation(lambda value: value["sources"].update(operator=descriptor(path)), pattern="bind this file")
        for role in ("transport", "matrix"):
            with self.subTest(changed_source=role):
                path = self.fixture.prepared.paths[role]
                old = path.read_bytes()
                try:
                    path.write_bytes(old + b"\n# Synthetic changed dependency.\n")
                    self.reject_attestation(lambda value: value["sources"].update({role: descriptor(path)}), pattern="frozen matrix dependency")
                finally:
                    path.write_bytes(old)

    def test_media_copy_identity_and_metadata_content_are_pinned(self):
        path = Path(self.fixture.prepared.media["LA"]["files"]["A1"]["path"])
        path.write_bytes(b"Different synthetic copied media bytes.\n")
        self.reject(pattern="changed after the producer copy proof")

    def test_fresh_roots_are_distinct_unsealed_and_never_resume(self):
        for key, path in (("operator", self.fixture.operator_output), ("matrix", self.fixture.matrix_output)):
            with self.subTest(existing_root=key):
                path.mkdir(mode=0o700)
                try:
                    self.reject(fresh=False, pattern="fresh execution root")
                finally:
                    path.rmdir()
        self.fixture.operator_output.symlink_to(self.fixture.root / "missing-output-target")
        try:
            self.reject(fresh=False, pattern="fresh execution root")
        finally:
            self.fixture.operator_output.unlink()
        for path in (self.fixture.matrix_output, self.fixture.matrix_output / "nested",
                     self.fixture.producer_root / "nested", self.fixture.attestation_root / "nested",
                     Path(self.fixture.attestation["sealedRoots"][0]) / "nested"):
            with self.subTest(overlap=str(path)):
                self.reject_attestation(lambda value: value["scope"].update(operatorEvidenceRoot=str(path)))


class FullScanBudgetGuards(GuardCase):
    fixture_options = {"scan_ready_round": 10}

    def test_source_bound_success_maximum_replays_actual_twelve_round_ledger(self):
        admission = self.fixture.admit()
        plan, terminal = admission.documents["plan"], self.fixture.terminal
        self.assertEqual(terminal["normalRequestCount"], plan["normalMaximum"])
        self.assertEqual(terminal["requestCount"], plan["successMaximumIncludingLogout"])
        self.assertGreater(terminal["normalRequestCount"], 232)
        self.assertGreater(terminal["requestCount"], 238)
        self.assertEqual(terminal["cleanupRequestCount"], 6)
        self.assertEqual(admission.replay_count, len(self.fixture.index["requests"]))
        self.assertEqual(admission.replay_count, terminal["requestCount"])
        self.assertEqual((admission.matrix.MAX_REQUESTS, admission.matrix.NORMAL_LIMIT, admission.matrix.CLEANUP_RESERVE), (300, 220, 80))
        self.assertFalse(self.fixture.operator_output.exists())
        self.assertFalse(self.fixture.matrix_output.exists())


class MediaMembershipGuards(GuardCase):
    """Reject changes to the exact copied media population without resealing it."""

    def extra_entry(self, kind):
        root = self.fixture.producer_root / "media"
        names = {"mp4": "unrecorded.mp4", "nfo": "unrecorded.nfo", "directory": "unrecorded-directory",
                 "symlink": "unrecorded-link", "fifo": "unrecorded-fifo"}
        path = root / names[kind]
        if kind == "directory":
            path.mkdir(mode=0o755)
        elif kind == "symlink":
            path.symlink_to(self.fixture.root / "missing-synthetic-symlink-target")
        elif kind == "fifo":
            os.mkfifo(path, mode=0o600)
        else:
            self.fixture.prepared.write(path, b"Unrecorded synthetic media-tree entry.\n")
        return path

    @staticmethod
    def remove_extra(path, kind):
        if kind == "directory":
            path.rmdir()
        else:
            path.unlink()

    def episode_path(self):
        return Path(self.fixture.prepared.media["LA"]["files"]["A1"]["path"])

    def required_directory(self):
        return Path(self.fixture.prepared.media["LA"]["files"]["A3"]["path"]).parent

    def replace_episode_with_equal_bytes(self):
        path = self.episode_path()
        old = path.lstat()
        raw = path.read_bytes()
        held = self.fixture.root / "held-original-media.mp4"
        path.rename(held)
        self.fixture.prepared.write(path, raw)
        path.chmod(stat.S_IMODE(old.st_mode))
        os.utime(path, ns=(old.st_atime_ns, old.st_mtime_ns))
        self.assertEqual(digest(path.read_bytes()), digest(held.read_bytes()))
        self.assertNotEqual((path.stat().st_dev, path.stat().st_ino), (held.stat().st_dev, held.stat().st_ino))

    def test_admission_rejects_extra_media_files_directories_symlinks_and_special_entries(self):
        original = (self.fixture.private / "media-tree.json").read_bytes()
        for kind in ("mp4", "nfo", "directory", "symlink", "fifo"):
            with self.subTest(kind=kind):
                path = self.extra_entry(kind)
                try:
                    with self.assertRaisesRegex(self.O.OperatorError, "real media tree contains an additional"):
                        self.fixture.admit()
                finally:
                    self.remove_extra(path, kind)
                self.assertEqual((self.fixture.private / "media-tree.json").read_bytes(), original)
                self.assertEqual(self.fixture.factory_calls, 0)
                self.assertFalse(self.fixture.operator_output.exists())
                self.assertFalse(self.fixture.matrix_output.exists())

    def test_checkpoint_rejects_extra_media_files_directories_symlinks_and_special_entries(self):
        for kind in ("mp4", "nfo", "directory", "symlink", "fifo"):
            with self.subTest(kind=kind):
                admission = self.fixture.admit()
                path = self.extra_entry(kind)
                try:
                    with self.assertRaisesRegex(self.O.OperatorError, "real media tree contains an additional"):
                        admission.checkpoint()
                finally:
                    self.remove_extra(path, kind)
                self.assertEqual(self.fixture.factory_calls, 0)

    def test_admission_rejects_a_missing_recorded_media_file(self):
        self.episode_path().rename(self.fixture.root / "held-missing-media.mp4")
        self.reject()

    def test_checkpoint_rejects_a_missing_recorded_media_file(self):
        admission = self.fixture.admit()
        self.episode_path().rename(self.fixture.root / "held-missing-media.mp4")
        with self.assertRaises((self.O.OperatorError, OSError)):
            admission.checkpoint()

    def test_admission_rejects_same_byte_media_file_replacement(self):
        self.replace_episode_with_equal_bytes()
        self.reject(pattern="changed after the producer copy proof")

    def test_checkpoint_rejects_same_byte_media_file_replacement(self):
        admission = self.fixture.admit()
        self.replace_episode_with_equal_bytes()
        with self.assertRaisesRegex(self.O.OperatorError, "pinned admission source or evidence record changed"):
            admission.checkpoint()

    def test_admission_rejects_a_missing_required_media_directory(self):
        self.required_directory().rename(self.fixture.root / "held-missing-media-directory")
        self.reject()

    def test_checkpoint_rejects_a_missing_required_media_directory(self):
        admission = self.fixture.admit()
        self.required_directory().rename(self.fixture.root / "held-missing-media-directory")
        with self.assertRaises((self.O.OperatorError, OSError)):
            admission.checkpoint()

    def test_admission_rejects_a_required_directory_replaced_by_a_symlink(self):
        path = self.required_directory()
        held = self.fixture.root / "held-symlink-media-directory"
        path.rename(held)
        path.symlink_to(held, target_is_directory=True)
        self.reject()

    def test_checkpoint_rejects_directory_replacement_with_unchanged_members_and_files(self):
        admission = self.fixture.admit()
        root = self.fixture.producer_root / "media"
        files = read_json(self.fixture.private / "media-tree.json")["files"]
        identities = {path: self.O.identity(Path(path).lstat()) for path in files}
        previous = root.lstat()
        held = self.fixture.root / "held-replaced-media-root"
        root.rename(held)
        root.mkdir(mode=stat.S_IMODE(previous.st_mode))
        for child in list(held.iterdir()):
            child.rename(root / child.name)
        self.assertNotEqual((root.stat().st_dev, root.stat().st_ino), (held.stat().st_dev, held.stat().st_ino))
        self.assertEqual({path: self.O.identity(Path(path).lstat()) for path in files}, identities)
        with self.assertRaisesRegex(self.O.OperatorError, "previously admitted media directory was replaced or modified"):
            admission.checkpoint()


class RuntimeReadonlyGuards(GuardCase):
    """Keep broad writable parents from covering existing read-only authority."""

    def test_dedicated_sibling_outputs_and_exact_lock_file_are_admitted(self):
        admission = self.fixture.admit()
        self.assertEqual(self.fixture.matrix_output.parent, self.fixture.run_parent)
        self.assertEqual(self.fixture.operator_output.parent, self.fixture.run_parent)
        self.assertEqual(self.fixture.matrix_output.name, "matrix-03")
        self.assertEqual(self.fixture.operator_output.name, "operator-03")
        lock = Path(self.fixture.prepared.lock["path"])
        writable = set(shlex.split(admission.value["runtime"]["properties"]["ReadWritePaths"]))
        self.assertEqual(writable, {str(self.fixture.run_parent), str(lock)})
        self.assertNotIn(str(lock.parent), writable)
        readonly = (self.fixture.producer_root, self.fixture.attestation_root,
                    Path(self.fixture.prepared.scope["sourceRoot"]), Path(self.fixture.prepared.scope["inputRoot"]),
                    *[Path(path) for path in self.fixture.attestation["sealedRoots"]],
                    *[Path(path) for path in self.fixture.attestation["forbiddenOriginalRoots"]])
        for path in readonly:
            self.assertNotEqual(self.fixture.run_parent, path)
            self.assertNotIn(self.fixture.run_parent, path.parents)
        self.assertFalse(self.fixture.operator_output.exists())
        self.assertFalse(self.fixture.matrix_output.exists())

    def test_disjoint_operator_output_does_not_authorize_unsafe_writable_parents(self):
        fixture = self.fixture
        parents = {"workspace": fixture.root, "sources": Path(fixture.prepared.scope["sourceRoot"]),
            "proxy-sources": Path(fixture.prepared.scope["proxySourceRoot"]),
            "inputs": Path(fixture.prepared.scope["inputRoot"]), "preparation-parent": fixture.producer_root.parent}
        old_roots = (fixture.producer_root, fixture.attestation_root,
                     *[Path(path) for path in fixture.attestation["sealedRoots"]])
        for label, parent in parents.items():
            with self.subTest(parent=label):
                original = deepcopy(fixture.attestation)
                old_unit = deepcopy(fixture.unit_values[fixture.matrix_unit])
                child = parent / ("unsafe-operator-" + label)
                for root in old_roots:
                    self.assertNotEqual(child, root)
                    self.assertNotIn(root, child.parents)
                    self.assertNotIn(child, root.parents)
                try:
                    writable = fixture.writable_paths(parent)
                    fixture.attestation["scope"]["operatorEvidenceRoot"] = str(child)
                    fixture.attestation["runtime"]["properties"]["ReadWritePaths"] = writable
                    fixture.unit_values[fixture.matrix_unit]["ReadWritePaths"] = writable
                    self.reject(pattern="writable evidence parent would cover a read-only")
                    self.assertFalse(child.exists())
                finally:
                    fixture.attestation = original
                    fixture.unit_values[fixture.matrix_unit] = old_unit

    def test_lock_file_exception_cannot_expand_to_the_fixture_directory(self):
        writable = shlex.join([str(self.fixture.run_parent), str(Path(self.fixture.prepared.lock["path"]).parent)])
        self.fixture.attestation["runtime"]["properties"]["ReadWritePaths"] = writable
        self.fixture.unit_values[self.fixture.matrix_unit]["ReadWritePaths"] = writable
        self.reject(pattern="exactly the evidence parents")


class UnsafeMatrixRuntimeGuards(GuardCase):
    fixture_options = {"broad_matrix_parent": True}

    def test_actual_producer_draft_with_unsafe_matrix_parent_is_rejected(self):
        fixture = self.fixture
        manifest = read_json(fixture.private / "manifest.json")
        draft = read_json(fixture.private / "draft-execution.json")
        self.assertEqual(fixture.terminal["status"], "awaiting_independent_attestation")
        self.assertEqual(manifest["scope"]["evidenceParent"], str(fixture.root))
        self.assertEqual(draft["scope"]["evidenceParent"], str(fixture.root))
        self.assertEqual(fixture.matrix_output, fixture.root / "matrix-03")
        published = deepcopy(draft)
        published["matrix"]["binding"]["fixtureReleased"] = True
        self.assertEqual(published, fixture.execution)
        for root in (fixture.producer_root, fixture.attestation_root,
                     *[Path(path) for path in fixture.attestation["sealedRoots"]]):
            self.assertNotEqual(fixture.matrix_output, root)
            self.assertNotIn(root, fixture.matrix_output.parents)
            self.assertNotIn(fixture.matrix_output, root.parents)
        self.reject(pattern="writable evidence parent would cover a read-only")


class RuntimeGuards(GuardCase):
    def test_runtime_static_isolation_deadline_and_resources_are_bounded(self):
        for key, wrong in (("Type", "simple"), ("RemainAfterExit", "no"), ("Restart", "always"),
                           ("User", "nobody"), ("UMask", "0022"), ("NoNewPrivileges", "no"),
                           ("ProtectSystem", "full"), ("ProtectHome", "no"), ("PrivateTmp", "no"),
                           ("PrivateNetwork", "yes"), ("TimeoutStartUSec", "infinity"),
                           ("TimeoutStartUSec", "2h"), ("MemoryMax", "infinity"),
                           ("MemoryMax", "2147483649"), ("TasksMax", "0"), ("TasksMax", "257"),
                           ("LimitNOFILE", "65537"), ("ReadWritePaths", "/tmp")):
            with self.subTest(property=key, wrong=wrong):
                self.reject_attestation(lambda value: value["runtime"]["properties"].update({key: wrong}))

    def test_preparation_shutdown_requires_exact_full_cli_and_invocation(self):
        original = deepcopy(self.fixture.attestation["shutdown"]["units"][0])
        for key, wrong in (("ActiveState", "inactive"), ("SubState", "dead"), ("MainPID", "45679"),
                           ("Result", "failed"), ("ExecMainStatus", "1"), ("RemainAfterExit", "no")):
            with self.subTest(property=key):
                self.reject_attestation(lambda value: value["shutdown"]["units"][0]["properties"].update({key: wrong}))
        command = original["properties"]["ExecStart"]
        for wrong in (command.replace(" -I ", " "), command.replace(" -B ", " "),
                      command.replace(" prepare ", " plan "),
                      command.replace("--manifest-sha256", "--unreviewed-sha256"),
                      command.replace("--plan-sha256", "--unreviewed-plan"),
                      command.replace(" ; ignore_errors", " --resume ; ignore_errors")):
            with self.subTest(command=wrong):
                self.reject_attestation(lambda value: value["shutdown"]["units"][0]["properties"].update(ExecStart=wrong), pattern="exact preparation source/input/plan")
        self.fixture.unit_values[self.fixture.preparation_unit]["InvocationID"] = "d" * 32
        self.reject(pattern="current preparation unit differs")

    def test_nonempty_preparation_cgroup_and_invalid_time_chain_are_rejected(self):
        self.fixture.cgroup_empty = False
        self.reject(pattern="cgroup is not empty")
        self.fixture.cgroup_empty = True
        for wrong in ("2026-09-13T00:59:00Z", "2026-09-13T02:01:00Z"):
            with self.subTest(observed=wrong):
                self.reject_attestation(lambda value: value["shutdown"].update(observedAt=wrong), pattern="times are out of order")

    def test_runtime_self_and_unit_identity_must_own_this_operator(self):
        original = deepcopy(self.fixture.current)
        for change in ({"uid": 1}, {"ssh": False}, {"isolated": False}, {"noBytecode": False},
                       {"pid": 1}, {"pid": True}, {"invocationId": "not-an-invocation"},
                       {"cgroup": "0::/system.slice/other.service\n"}):
            with self.subTest(change=change):
                self.fixture.current = {**deepcopy(original), **change}
                self.reject(pattern="exact root SSH isolated systemd invocation")
        self.fixture.current = original
        original_unit = deepcopy(self.fixture.unit_values[self.fixture.matrix_unit])
        for change in ({"MainPID": "45679"}, {"InvocationID": "d" * 32},
                       {"ActiveState": "inactive", "SubState": "dead"},
                       {"ControlGroup": "/system.slice/other.service"}, {"Restart": "always"}):
            with self.subTest(unit=change):
                self.fixture.unit_values[self.fixture.matrix_unit] = {**deepcopy(original_unit), **change}
                self.reject(pattern="does not own this exact operator process")

    def test_checkpoint_rejects_source_and_directory_identity_drift(self):
        admission = self.fixture.admit()
        path = self.fixture.operator_source
        path.write_bytes(path.read_bytes() + b"\n# Synthetic checkpoint drift.\n")
        with self.assertRaisesRegex(self.O.OperatorError, "changed"):
            admission.checkpoint()
        path.write_bytes(SOURCE_BYTES["operator"])
        admission = self.fixture.admit()
        self.fixture.operator_parent.chmod(0o750)
        with self.assertRaisesRegex(self.O.OperatorError, "directory identity changed"):
            admission.checkpoint()

    def test_checkpoint_rejects_later_invocation_or_cgroup_change(self):
        admission = self.fixture.admit()
        original = deepcopy(self.fixture.current)
        self.fixture.current["invocationId"] = "d" * 32
        with self.assertRaisesRegex(self.O.OperatorError, "invocation identity changed"):
            admission.checkpoint()
        self.fixture.current = original
        self.fixture.cgroup_empty = False
        with self.assertRaisesRegex(self.O.OperatorError, "cgroup is not empty"):
            admission.checkpoint()

    def test_original_bytes_are_rejected_before_any_open(self):
        admission = self.fixture.admit()
        forbidden = self.fixture.prepared.manifest["forbiddenOriginalRoots"][0]
        with self.assertRaisesRegex(self.O.OperatorError, "original implementation or database bytes"):
            admission._read(forbidden)
        self.assertEqual(self.boundary.attempts["originalRead"], 0)


class ExecuteGuards(GuardCase):
    def test_frozen_transport_completes_all_empty_branch_and_retains_terminal(self):
        admission = self.fixture.admit()
        producer_before = {path.name: path.read_bytes() for path in self.fixture.private.iterdir()}
        terminal = self.execute(admission, actual=True)
        self.assertEqual(terminal["status"], "matrix_protocol_complete")
        self.assertTrue(terminal["cleanupComplete"])
        self.assertTrue(terminal["independentRuntimeClosureRequired"])
        self.assertFalse(terminal["clientAcceptanceClaim"])
        self.assertFalse(terminal["resumeOrRetryAllowed"])
        runner, wire = self.fixture.runners[0], self.fixture.matrix_wires[0]
        self.assertEqual(runner.matrix.normal_count, 116)
        self.assertEqual(terminal["transportResult"]["outcome"], "reference_global_positive_unresolved")
        self.assertFalse(runner.matrix.r5_r6_started)
        self.assertEqual(runner.matrix.current, runner.matrix.baseline)
        self.assertEqual(runner.matrix.revoked, set(ACTORS))
        self.assertEqual(len(wire.calls), terminal["transportResult"]["httpAttempts"])
        self.assertEqual(len({call["request"].label for call in wire.calls}), len(wire.calls))
        self.assertEqual(producer_before, {path.name: path.read_bytes() for path in self.fixture.private.iterdir()})
        retained = read_json(self.fixture.operator_output / "private/terminal.json")
        self.assertEqual(retained["status"], "awaiting_operator_commit")
        self.assertEqual(retained["candidateStatus"], terminal["status"])
        self.assertFalse(retained["completionCommitted"])
        self.assertTrue(terminal["completionCommitted"])
        commit_path = self.fixture.operator_output / "private/commit.json"
        commit = read_json(commit_path)
        self.assertEqual(commit["status"], terminal["status"])
        self.assertEqual(commit["terminalPrivateSha256"], descriptor(self.fixture.operator_output / "private/terminal.json")["sha256"])
        self.assertEqual(commit["terminalExportSha256"], descriptor(self.fixture.operator_output / "export/terminal.json")["sha256"])
        self.assertEqual(terminal["commitSha256"], descriptor(commit_path)["sha256"])
        safe = (self.fixture.operator_output / "export/terminal.json").read_bytes()
        self.assertEqual(json.loads(safe), admission.support.sanitized(retained, admission.secrets | runner.secrets))
        for secret in (*wire.tokens.values(), *wire.sessions.values(), str(self.fixture.producer_root)):
            self.assertNotIn(secret.encode(), safe)
        self.assertTrue(self.fixture.target_calls)
        attempts = len(wire.calls)
        with self.assertRaises((self.O.OperatorError, OSError)):
            self.O.execute(admission, target_probe=self.fixture.target_probe,
                           runner_factory=self.fixture.actual_factory(admission))
        self.assertEqual(len(wire.calls), attempts)
        self.assertEqual(self.fixture.factory_calls, 1)
        with self.assertRaises(admission.support.TransportError):
            runner.run()

    def test_frozen_transport_observation_failure_closes_with_distinct_status(self):
        admission = self.fixture.admit()
        def response_hook(wire, request):
            if request.label == "R0-P-detail-A1":
                return wire.wire(500, {"error": "synthetic observation rejected"})
            return None
        terminal = self.execute(admission, actual=True, response_hook=response_hook)
        self.assertEqual(terminal["status"], "matrix_observation_failed_cleanup_complete")
        self.assertTrue(terminal["cleanupComplete"])
        self.assertEqual(terminal["transportResult"]["mode"], "closed-with-observation-failure")
        self.assertFalse(terminal["clientAcceptanceClaim"])

    def test_incomplete_transport_response_cannot_claim_success(self):
        admission = self.fixture.admit()
        def response_hook(wire, request):
            return wire.wire(200, {"partial": True}, complete=False, failure="synthetic truncated reply")
        terminal = self.execute(admission, actual=True, response_hook=response_hook)
        self.assert_recovery(terminal)
        self.assertEqual(len(self.fixture.matrix_wires[0].calls), 1)
        self.assertEqual(terminal["transportResult"]["mode"], "recovery-required")

    def test_transport_result_persistence_failure_cannot_claim_success(self):
        admission = self.fixture.admit()
        original = admission.support.Journal.save
        def save(journal, name, value, *, export=False):
            if journal.root == self.fixture.matrix_output and name == "result.json" and export:
                raise OSError("Synthetic transport result persistence failure.")
            return original(journal, name, value, export=export)
        with patch.object(admission.support.Journal, "save", new=save):
            terminal = self.execute(admission, actual=True)
        self.assert_recovery(terminal)
        self.assertFalse(terminal["transportResult"]["evidenceComplete"])
        self.assertFalse((self.fixture.matrix_output / "export/result.json").exists())
        self.assertGreater(len(self.fixture.matrix_wires[0].calls), 0)

    def test_returned_result_must_match_actual_retained_transport_result(self):
        admission = self.fixture.admit()
        terminal = self.execute(admission, retained_change=lambda value: value.update(httpAttempts=99))
        self.assert_recovery(terminal)
        self.assertIn("does not match", terminal["failure"]["message"])

    def test_missing_retained_transport_result_cannot_claim_success(self):
        terminal = self.execute(self.fixture.admit(), omit="result")
        self.assert_recovery(terminal)
        self.assertEqual(terminal["failure"]["type"], "FileNotFoundError")

    def test_unclosed_private_transport_state_cannot_claim_success(self):
        terminal = self.execute(self.fixture.admit(),
            state_change=lambda value: value["matrix"].update(pending={"request": "unresolved"}, revokedActors=["P"]))
        self.assert_recovery(terminal)

    def test_transport_persistence_flag_overrides_returned_closed_mode(self):
        terminal = self.execute(self.fixture.admit(), result_change=lambda value: value.update(evidenceComplete=False))
        self.assert_recovery(terminal)

    def test_checkpoint_drift_after_runner_return_prevents_completion(self):
        path = self.fixture.operator_source
        terminal = self.execute(self.fixture.admit(),
            after_run=lambda: path.write_bytes(path.read_bytes() + b"\n# Synthetic post-run drift.\n"))
        self.assert_recovery(terminal)
        self.assertIn("changed", terminal["failure"]["message"])

    def test_changed_runtime_property_is_rechecked_inside_target_probe(self):
        admission = self.fixture.admit()
        original_probe = self.fixture.target_probe
        def target_probe(expected):
            observed = original_probe(expected)
            self.fixture.unit_values[self.fixture.matrix_unit]["Restart"] = "always"
            return observed
        terminal = self.O.execute(admission, target_probe=target_probe,
                                 runner_factory=self.fixture.actual_factory(admission))
        self.assert_recovery(terminal)
        self.assertTrue(self.fixture.target_calls)
        self.assertEqual(self.fixture.matrix_wires[0].calls, [])

    def test_outer_admission_persistence_failure_never_constructs_runner(self):
        admission = self.fixture.admit()
        original = admission.support.Journal.save
        def save(journal, name, value, *, export=False):
            if journal.root == self.fixture.operator_output and name == "admission.json":
                raise OSError("Synthetic admission persistence failure.")
            return original(journal, name, value, export=export)
        with patch.object(admission.support.Journal, "save", new=save):
            terminal = self.execute(admission)
        self.assert_recovery(terminal)
        self.assertEqual(self.fixture.factory_calls, 0)
        self.assertFalse(self.fixture.matrix_output.exists())

    def test_outer_private_terminal_persistence_failure_is_not_success(self):
        self.outer_terminal_failure(export=False)

    def test_outer_export_terminal_persistence_failure_is_not_success(self):
        self.outer_terminal_failure(export=True)

    def outer_terminal_failure(self, *, export):
        admission = self.fixture.admit()
        original = admission.support.Journal.save
        captured = []
        def save(journal, name, value, *, export=False):
            if journal.root == self.fixture.operator_output and name == "terminal.json":
                captured.append((export, deepcopy(value)))
                if export == fail_export:
                    raise OSError("Synthetic outer terminal persistence failure.")
            return original(journal, name, value, export=export)
        fail_export = export
        with patch.object(admission.support.Journal, "save", new=save):
            terminal = self.execute(admission)
        self.assert_recovery(terminal)
        self.assertEqual(terminal["terminalPersistenceFailure"], "OSError")
        self.assertTrue(captured)
        self.assertFalse((self.fixture.operator_output / "export/terminal.json").exists())
        self.assertFalse((self.fixture.operator_output / "private/commit.json").exists())
        self.assertFalse(terminal["completionCommitted"])
        for unused_export, value in captured:
            self.assertEqual(value["status"], "awaiting_operator_commit")
            self.assertFalse(value["completionCommitted"])

    def test_outer_commit_persistence_failure_leaves_only_pending_terminals(self):
        admission = self.fixture.admit()
        original = admission.support.Journal.save
        def save(journal, name, value, *, export=False):
            if journal.root == self.fixture.operator_output and name == "commit.json":
                raise OSError("Synthetic final commit persistence failure.")
            return original(journal, name, value, export=export)
        with patch.object(admission.support.Journal, "save", new=save):
            terminal = self.execute(admission)
        self.assert_recovery(terminal)
        self.assertFalse(terminal["completionCommitted"])
        self.assertFalse((self.fixture.operator_output / "private/commit.json").exists())
        for scope in ("private", "export"):
            retained = read_json(self.fixture.operator_output / scope / "terminal.json")
            self.assertEqual(retained["status"], "awaiting_operator_commit")
            self.assertFalse(retained["completionCommitted"])

    def test_outer_journal_close_failure_cannot_return_success(self):
        admission = self.fixture.admit()
        original = admission.support.Journal.close
        def close(journal):
            original(journal)
            if journal.root == self.fixture.operator_output:
                raise OSError("Synthetic outer journal close failure.")
        with patch.object(admission.support.Journal, "close", new=close):
            terminal = self.execute(admission)
        self.assert_recovery(terminal)
        self.assertEqual(terminal["terminalPersistenceFailure"], "OSError")

    def test_new_output_race_after_admission_never_constructs_runner(self):
        admission = self.fixture.admit()
        self.fixture.operator_output.mkdir(mode=0o700)
        with self.assertRaises(OSError):
            self.execute(admission)
        self.assertEqual(self.fixture.factory_calls, 0)
        self.assertFalse(self.fixture.matrix_output.exists())


def source_bytes(path, expected_name):
    path = path.absolute()
    info = path.lstat()
    if (path.name != expected_name or not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode) or
            info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022 or info.st_size > 4 * 1024 * 1024):
        raise ValueError("An explicit root-owned reviewed Python source is required: " + expected_name)
    return path.read_bytes()


def main():
    global SOURCE_BYTES, PREPARATION_GUARDS
    if (sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or
            not sys.flags.isolated or not sys.flags.dont_write_bytecode):
        print(json.dumps({"suite": "run-nextup-global-reference-guards", "status": "blocked",
            "reason": "Authorized root Linux SSH and Python -I -B are required; local verification is forbidden."}))
        return 2
    parser = argparse.ArgumentParser(description=__doc__)
    names = {"operator": "run-nextup-global-reference.py", "preparation": "prepare-nextup-global-reference.py",
        "preparation-guards": "test-prepare-nextup-global-reference.py", "transport": "nextup-global-transport.py",
        "matrix": "nextup-global-matrix.py"}
    for role in names:
        parser.add_argument("--" + role + "-source", required=True, type=Path)
    parser.add_argument("--report-path", required=True, type=Path)
    arguments = parser.parse_args()
    SOURCE_BYTES = {role: source_bytes(getattr(arguments, role.replace("-", "_") + "_source"), name)
                    for role, name in names.items()}
    grant_source = arguments.preparation_source.with_name("verify-folder-grant-01.py")
    SOURCE_BYTES["grant-worker"] = source_bytes(grant_source, "verify-folder-grant-01.py")
    if digest(SOURCE_BYTES["grant-worker"]) != GRANT_WORKER_SHA256:
        raise ValueError("The preparation fixture requires its exact reviewed grant-verification worker source.")
    names["grant-worker"] = "verify-folder-grant-01.py"
    source_hashes = {role: digest(raw) for role, raw in SOURCE_BYTES.items()}
    source_hashes["guards"] = digest(Path(__file__).read_bytes())
    report_path = arguments.report_path.absolute()
    if report_path.exists() or report_path.is_symlink():
        raise ValueError("The guard report must be a new explicit output file.")
    with tempfile.TemporaryDirectory(prefix="nextup-entry-source-snapshot-") as temporary:
        snapshot = Path(temporary)
        snapshots = {}
        for role, name in names.items():
            path = snapshot / name
            with path.open("xb") as handle:
                handle.write(SOURCE_BYTES[role])
            path.chmod(0o600)
            snapshots[role] = path
        PREPARATION_GUARDS = load_source(snapshots["preparation-guards"], "entry_preparation_fixture")
        PREPARATION_GUARDS.PREPARATION_SOURCE = snapshots["preparation"]
        PREPARATION_GUARDS.TRANSPORT_SOURCE = snapshots["transport"]
        PREPARATION_GUARDS.MATRIX_SOURCE = snapshots["matrix"]
        suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
        result = unittest.TextTestRunner(verbosity=2).run(suite)
    report = {"schemaVersion": 1, "suite": "run-nextup-global-reference-guards",
        "classification": "synthetic source-bound admission, replay, transport, and persistence guards; not live acceptance evidence",
        "sourceSha256": source_hashes, "testsRun": result.testsRun, "failures": len(result.failures),
        "errors": len(result.errors), "skips": len(result.skipped), "passed": result.wasSuccessful(),
        "failureDetails": [{"test": case.id(), "traceback": detail} for case, detail in result.failures],
        "errorDetails": [{"test": case.id(), "traceback": detail} for case, detail in result.errors],
        "skipDetails": [{"test": case.id(), "reason": reason} for case, reason in result.skipped],
        "actualBusinessHttpRequests": 0, "actualProcessProbes": 0, "blockedRealIOAttempts": dict(BLOCKED_ATTEMPTS),
        "originalImplementationBytesRead": False, "liveAcceptanceClaim": False}
    report_path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    with report_path.open("x", encoding="utf-8") as handle:
        json.dump(report, handle, sort_keys=True, indent=2)
        handle.write("\n")
    report_path.chmod(0o600)
    print(json.dumps(report, sort_keys=True))
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
