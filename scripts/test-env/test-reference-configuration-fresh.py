#!/usr/bin/env python3
"""Exercise fresh configuration capture guards using synthetic memory only.

Run through authorized root SSH with one fresh recorder source path. Only this
suite, that recorder, the preparation operator, and four pinned recorder sources
are read before the effect fence. A pass establishes synthetic safety contracts;
it does not establish live capture, restoration, cleanup, or HTTP acceptance.
"""

from __future__ import annotations

import _io
import argparse
import base64
import contextlib
import copy
import datetime
import hashlib
import http.client
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
from unittest.mock import Mock, patch
from urllib.parse import parse_qs, quote, unquote, unquote_plus, urlencode, urlsplit, urlunsplit

sys.dont_write_bytecode = True
MODULE: types.ModuleType
SOURCE_LINES: dict[str, list[str]] = {}
SOURCE_HASHES: dict[str, str] = {}


@contextlib.contextmanager
def memory_tracebacks():
    """Keep source lookup in memory while import and cleanup errors render."""
    with patch.object(linecache, "checkcache", lambda filename=None: None), \
         patch.object(linecache, "lazycache", lambda filename, module_globals: False), \
         patch.object(linecache, "getlines", lambda filename, module_globals=None:
                      list(SOURCE_LINES.get(str(filename), ()))):
        yield


class EffectFence(contextlib.ExitStack):
    """Reject every unfaked filesystem, process, signal, or network effect."""

    def __init__(self) -> None:
        super().__init__()
        self.violations: list[str] = []

    def __enter__(self) -> EffectFence:
        super().__enter__()
        import builtins

        self.enter_context(memory_tracebacks())
        fence = self

        class RejectUncachedImports:
            def find_spec(self, fullname: str, path: object = None, target: object = None) -> object:
                fence.violations.append("import:" + fullname)
                raise AssertionError("Uncached import is outside the memory fixture: " + fullname)

        self.enter_context(patch.object(sys, "meta_path", [RejectUncachedImports()]))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")), (_io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (signal, ("signal", "setitimer", "pthread_sigmask")), (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "stat", "lstat", "fstat", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "fsync", "umask", "kill", "killpg", "system", "popen", "fork", "posix_spawn",
                  "posix_spawnp", "execve", "execvp", "replace", "rename", "mkdir", "makedirs", "remove", "unlink",
                  "rmdir", "chmod", "chown", "link", "symlink", "truncate")),
            (os.path, ("ismount",)),
            (Path, ("open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat", "read_bytes",
                    "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink", "chmod", "touch",
                    "rename", "replace", "glob", "rglob", "iterdir")),
        )
        for owner, names in targets:
            for name in names:
                if not hasattr(owner, name):
                    continue
                label = owner.__name__ + "." + name

                def denied(*_args: object, _label: str = label, **_kwargs: object) -> object:
                    self.violations.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments: object) -> None:
        super().__exit__(*arguments)
        if self.violations:
            raise AssertionError("External effects were attempted: " + ", ".join(self.violations))


class FakeResponse:
    def __init__(self, body: bytes, *, declared_length: int | None = None, status: int = 200) -> None:
        self.body, self.offset = body, 0
        self.status, self.reason, self.version = status, "Synthetic response", 11
        self.declared_length = len(body) if declared_length is None else declared_length

    @property
    def length(self) -> int:
        return len(self.body) - self.offset

    def getheaders(self) -> list[tuple[str, str]]:
        return [("Content-Length", str(self.declared_length))]

    def read(self, limit: int) -> bytes:
        value = self.body[self.offset:self.offset + limit]
        self.offset += len(value)
        return value

    def isclosed(self) -> bool:
        return self.length == 0


class FakeConnection:
    def __init__(self, response: FakeResponse) -> None:
        self.response, self.requests, self.closed = response, [], False

    def request(self, method: str, path: str, body: object, headers: dict) -> None:
        self.requests.append((method, path, body, copy.deepcopy(headers)))

    def getresponse(self) -> FakeResponse:
        return self.response

    def close(self) -> None:
        self.closed = True


class FreshConfigurationGuards(unittest.TestCase):
    def setUp(self) -> None:
        fence = EffectFence()
        fence.__enter__()
        self.addCleanup(fence.__exit__, None, None, None)
        output = contextlib.redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)

    def replace(self, owner: object, name: str, value: object) -> object:
        replacement = patch.object(owner, name, value)
        replacement.start()
        self.addCleanup(replacement.stop)
        return value

    def recorder(self) -> object:
        value = self.sanitizer()
        value.manifest = {"serverId": "synthetic-fresh-server", "oldServices": {
            "reference": {"networkNamespace": "net:[synthetic-old-reference]"},
            "main": {"networkNamespace": "net:[synthetic-old-main]"}}}
        value.authority = {"pid": 43210, "startTicks": "123456", "networkNamespace": "net:[synthetic-fresh]"}
        value.pid = value.authority["pid"]
        value.started, value.deadline, value.finishing = 100.0, 1000.0, False
        value.logins = {"control": {"AccessToken": "synthetic-owned-admin-token"},
                        "viewer": {"AccessToken": "synthetic-owned-viewer-token"}}
        value.login_attempts = {"control", "viewer"}
        value.forbidden_tokens = {"synthetic-old-bootstrap-token"}
        value.account_credentials = {
            "admin": {"REFERENCE_USERNAME": "synthetic-admin", "REFERENCE_PASSWORD": "synthetic-admin-password"},
            "viewer": {"REFERENCE_USERNAME": "synthetic-viewer", "REFERENCE_PASSWORD": "synthetic-viewer-password"}}
        value.invalid_tokens, value.logged_out = set(), set()
        value.login_statuses, value.invalid_login_proofs, value.logout_statuses = {}, {}, {}
        value.record_count = value.total = value.charged_bytes = value.incomplete_count = 0
        value.mutations, value.persistence_failures, value.cleanup_errors, value.observations = [], [], [], []
        value.wire_records, value.checks = {}, {}
        value.capture_failure, value.cleanup_ok = None, False
        value.pending, value.writes_blocked, value.block_reasons = None, False, []
        value.baselines = {
            "total": {"status": 200, "body": {
                "ServerName": "Synthetic baseline server",
                "SortRemoveWords": ["the", "a"], "ImageExtractionTimeoutMs": 10000,
                "HttpServerPortNumber": MODULE.PORT, "HttpsPortNumber": MODULE.HTTPS_PORT,
                "EnableHttps": False, "EnableRemoteAccess": False, "EnableUPnP": False,
                "EnableAutoUpdate": False, "EnableAutomaticRestart": False, "IsInMaintenanceMode": False,
                "CachePath": "/synthetic/cache", "MetadataPath": "/synthetic/metadata",
                "NestedProtected": {"Enabled": True, "Integer": 1, "Ratio": 1.0, "Optional": None, "List": [1, True]}}},
            "encoding": {"status": 200, "body": {
                "TranscodingMaxWidth": 1920, "EnableHardwareEncoding": False,
                "HardwareAccelerationType": "none", "TranscodingTempPath": "/synthetic/transcoding",
                "EncodingThreadCount": 1, "NestedProtected": {"Enabled": True, "Integer": 1, "Optional": None}}},
            "unknown": {"status": 404, "body": "Synthetic unknown configuration"}}
        value.public_baseline = {"Id": value.manifest["serverId"], "Version": "4.9.5.0",
                                 "ServerName": value.baselines["total"]["body"]["ServerName"]}
        value.latest = {**copy.deepcopy(value.baselines), "public": copy.deepcopy(value.public_baseline)}
        value.config_dirty, value.restored, value.current_case = set(), True, None
        value.case_results, value.restoration_results, value.restore_attempts = [], [], {}
        value.key_baseline_empty, value.key_create_attempted, value.owned_key = False, False, None
        value.key_ownership_failed, value.key_invalidity, value.key_revoke_attempted = False, None, False
        value.control_user_id, value.old_users, value.task_baseline, value.device_baseline = None, [], {}, []
        return value

    def install_wire(self, recorder: object, responses: FakeResponse | list[FakeResponse]) -> types.SimpleNamespace:
        items = responses if isinstance(responses, list) else [responses]
        connections = [FakeConnection(item) for item in items]
        fixture = types.SimpleNamespace(saved={}, connections=connections, opened=0,
                                        authority=copy.deepcopy(recorder.authority), namespace=recorder.authority["networkNamespace"])

        def save(path: Path, value: object) -> None:
            self.assertTrue(path.is_relative_to(MODULE.base.PRIVATE) or path.is_relative_to(MODULE.base.EXPORT))
            self.assertNotIn(path, fixture.saved, "Prior evidence must not be overwritten")
            fixture.saved[path] = copy.deepcopy(value)

        def connect(host: str, port: int, *, timeout: float) -> FakeConnection:
            self.assertEqual((host, port, timeout), ("127.0.0.1", 18099, 5))
            self.assertLess(fixture.opened, len(connections), "Unexpected synthetic HTTP attempt")
            result = connections[fixture.opened]
            fixture.opened += 1
            return result

        def same_identity(expected: dict) -> None:
            if expected != fixture.authority:
                raise RuntimeError("Synthetic fresh process identity changed")

        authority = types.SimpleNamespace(same_new_identity=same_identity)
        self.replace(MODULE, "operator_module", lambda: authority)
        self.replace(os, "readlink", lambda path: fixture.namespace if str(path) == "/proc/self/ns/net"
                     else self.fail("Unexpected process namespace observation"))
        self.replace(MODULE.base, "save", save)
        self.replace(MODULE.base, "digest", lambda path: hashlib.sha256(json.dumps(fixture.saved[path], sort_keys=True).encode()).hexdigest())
        self.replace(time, "monotonic", lambda: 100.0)
        self.replace(signal, "setitimer", lambda *args: None)
        self.replace(http.client, "HTTPConnection", connect)
        return fixture

    @staticmethod
    def json_response(value: object, *, status: int = 200) -> FakeResponse:
        return FakeResponse(json.dumps(value).encode(), status=status)

    def read_responses(self, recorder: object) -> list[FakeResponse]:
        return [self.json_response(recorder.baselines["total"]["body"]),
                self.json_response(recorder.baselines["encoding"]["body"]), self.json_response(recorder.public_baseline)]

    def approve(self, recorder: object, area: str, body: dict, actor: str = "control", *, case: str | None = None) -> None:
        if recorder.current_case is None:
            recorder.begin_case(case or {"total": "full-server-name", "partial": "partial-flat-merge",
                                         "encoding": "named-encoding-width", "unknown": "unknown-named-post"}[area])
        recorder.approve_configuration(area, actor, body)

    def test_fixed_fresh_authority_capture_paths_and_source_pins(self) -> None:
        self.assertEqual((MODULE.PORT, MODULE.HTTPS_PORT, MODULE.OLD_RECORDS), (18099, 18499, 2051))
        self.assertEqual((MODULE.MAIN_REQUESTS, MODULE.MAX_REQUESTS), (240, 280))
        self.assertGreaterEqual(MODULE.MAX_REQUESTS - MODULE.MAIN_REQUESTS, 40)
        self.assertEqual(MODULE.OLD_SERVER_ID, "ec69ef1cf84140e88489c30326529308")
        self.assertEqual(MODULE.LIBRARIES_ROUTE, "/emby/Library/VirtualFolders/Query")
        self.assertEqual(MODULE.OPERATOR_SOURCE_SHA256, "6aa2f03e2db5fd0028159c55bf6fc2cd22bb33597a9c4cb7166e40b2b14329d8")
        self.assertEqual(MODULE.OPERATOR_SOURCE_SHA256, SOURCE_HASHES["prepare-configuration-fresh.py"])
        self.assertEqual(MODULE.EVIDENCE, Path("/opt/goby-test/exec-work-m5g/emby-configuration-fresh-m5g-20260910-01"))
        self.assertEqual(MODULE.DATA, Path("/opt/goby-test/exec-work-m5g/emby-configuration-fresh-data-01"))
        self.assertEqual(MODULE.MANIFEST, MODULE.EVIDENCE / "private/manifest.json")
        self.assertEqual(MODULE.base.ROOT, MODULE.EVIDENCE / "runtime/configuration-capture")
        self.assertEqual(set(MODULE.IDENTITIES), {"control", "viewer"})
        self.assertEqual(set(MODULE.CONFIG_ROUTES), {"total", "encoding", "unknown"})
        self.assertTrue(MODULE.UNKNOWN.startswith("goby-configuration-fresh-m5g-20260910-01-"))
        self.assertEqual(MODULE.SANITIZER_SOURCES, {name: SOURCE_HASHES[name] for name in MODULE.SANITIZER_SOURCES})

    def ready_manifest(self) -> dict:
        return {"state": "READY", "marker": MODULE.MARKER, "unit": MODULE.UNIT,
                "port": MODULE.PORT, "httpsPort": MODULE.HTTPS_PORT, "programData": str(MODULE.DATA),
                "evidenceRoot": str(MODULE.EVIDENCE), "operatorSha256": MODULE.OPERATOR_SOURCE_SHA256,
                "sanitizerSources": copy.deepcopy(MODULE.SANITIZER_SOURCES),
                "allFreshProgramDataOwned": True, "oldPreservationVerified": True,
                "bootstrapCredentialsRevoked": True, "bootstrapCredentialsInvalidity": {"admin": 401, "viewer": 401},
                "adminCredentialsFile": str(MODULE.EVIDENCE / "private/admin-credentials.env"),
                "viewerCredentialsFile": str(MODULE.EVIDENCE / "private/viewer-credentials.env"),
                "serverId": "synthetic-fresh-server", "bootstrapLibraries": [],
                "bootstrapLibraryMutationRequests": 0, "bootstrapTaskMutationRequests": 0, "bootstrapApplicationKeyRequests": 0,
                "oldBaseline": {"records": {"/synthetic/protected/record-" + str(index): "a" * 64 for index in range(4102)},
                                "media": {"/synthetic/protected/media-" + str(index): "b" * 64 for index in range(240)},
                                "privateFiles": {"/synthetic/protected/private-secret": "c" * 64},
                                "retainedHistoricalFiles": {"/synthetic/protected/history": "d" * 64},
                                "historicalRemovedPaths": ["/synthetic/protected/removed"],
                                "retiredOwnedSources": {"removedAfterCompletedStudy": True}},
                "setupRecordCount": 2}

    def test_ready_manifest_requires_the_fresh_port_identity_revoked_bootstrap_and_old_corpus(self) -> None:
        manifest = self.ready_manifest()
        original = copy.deepcopy(manifest)
        operator = types.SimpleNamespace(OLD_SERVER_ID="synthetic-old-server", load=lambda path: copy.deepcopy(manifest)
                                         if path == MODULE.MANIFEST else self.fail("Unexpected manifest read"))
        self.replace(MODULE, "operator_module", lambda: operator)
        self.assertEqual(MODULE.load_manifest(), manifest)
        changes = (("state", "PREPARING"), ("marker", "unowned"), ("unit", "old.service"), ("port", 18097),
                   ("httpsPort", 18497), ("programData", "/opt/goby-test/emby-reference-data"), ("evidenceRoot", "/old/private"),
                   ("serverId", operator.OLD_SERVER_ID), ("operatorSha256", "f" * 64), ("sanitizerSources", {}),
                   ("allFreshProgramDataOwned", False), ("oldPreservationVerified", False), ("bootstrapCredentialsRevoked", False),
                   ("bootstrapCredentialsInvalidity", {"admin": 401}), ("adminCredentialsFile", "/old/private/admin.env"),
                   ("viewerCredentialsFile", "/old/private/viewer.env"), ("bootstrapLibraries", [{}]),
                   ("bootstrapLibraryMutationRequests", 1), ("bootstrapTaskMutationRequests", 1), ("bootstrapApplicationKeyRequests", 1))
        for field, value in changes:
            manifest.clear()
            manifest.update(copy.deepcopy(original))
            manifest[field] = value
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                MODULE.load_manifest()
        for field in ("records", "media"):
            manifest.clear()
            manifest.update(copy.deepcopy(original))
            manifest["oldBaseline"][field].popitem()
            with self.subTest(corpus=field), self.assertRaises(RuntimeError):
                MODULE.load_manifest()

    def test_snapshot_preserves_2051_pairs_240_sources_all_old_private_hashes_and_bootstrap_tokens(self) -> None:
        recorder = self.recorder()
        recorder.manifest = self.ready_manifest()
        old = copy.deepcopy(recorder.manifest["oldBaseline"])
        raw = [MODULE.EVIDENCE / "private/raw" / ("synthetic-setup-" + str(index) + ".json") for index in range(2)]
        exported = [MODULE.EVIDENCE / "export" / path.name for path in raw]
        private = [*raw, MODULE.MANIFEST, MODULE.EVIDENCE / "private/admin-credentials.env", MODULE.EVIDENCE / "private/viewer-credentials.env"]
        tokens = {path: "synthetic-retired-bootstrap-" + str(index) for index, path in enumerate(raw)}
        verify = Mock()
        self.replace(MODULE, "operator_module", lambda: types.SimpleNamespace(verify_baseline=verify))
        self.replace(Path, "glob", lambda path, pattern: iter(raw if path == MODULE.EVIDENCE / "private/raw" else exported)
                     if path in {MODULE.EVIDENCE / "private/raw", MODULE.EVIDENCE / "export"} and pattern == "*.json"
                     else self.fail("Unexpected setup record enumeration"))
        self.replace(Path, "rglob", lambda path, pattern: iter(private)
                     if path == MODULE.EVIDENCE / "private" and pattern == "*" else self.fail("Unexpected private enumeration"))
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "is_file", lambda path: path in private)
        self.replace(Path, "stat", lambda path: types.SimpleNamespace(st_size=100))
        self.replace(Path, "read_text", lambda path: json.dumps({"response": {"body": {"AccessToken": tokens[path]}}})
                     if path in tokens else self.fail("Unexpected setup record content read"))
        self.replace(MODULE.base, "private_file", Mock())
        self.replace(MODULE.base, "digest", lambda path: "e" * 64)
        snapshot = recorder.snapshot()
        verify.assert_called_once_with(recorder.manifest["oldBaseline"])
        self.assertEqual(len(snapshot["records"]), 4106)
        self.assertEqual(snapshot["media"], old["media"])
        for path, digest in old["records"].items():
            self.assertEqual(snapshot["records"][path], digest)
        for path, digest in old["privateFiles"].items():
            self.assertEqual(snapshot["privateFiles"][path], digest)
        self.assertEqual(snapshot["retainedHistoricalFiles"], old["retainedHistoricalFiles"])
        self.assertEqual(snapshot["historicalRemovedPaths"], old["historicalRemovedPaths"])
        self.assertEqual(snapshot["retiredOwnedSources"], old["retiredOwnedSources"])
        self.assertTrue(set(tokens.values()) <= recorder.forbidden_tokens)
        self.assertTrue(set(tokens.values()) <= recorder.secrets)
        raw.pop()
        with self.assertRaises(RuntimeError):
            recorder.snapshot()

    def test_each_http_attempt_requires_the_exact_fresh_process_and_namespace(self) -> None:
        recorder = self.recorder()
        fixture = self.install_wire(recorder, self.json_response({}))
        initial = copy.deepcopy(fixture.authority)
        for field, replacement in (("pid", 43211), ("startTicks", "123457"), ("networkNamespace", "net:[replacement]")):
            with self.subTest(field=field):
                fixture.authority[field] = replacement
                with self.assertRaises(RuntimeError):
                    recorder.request("replaced-identity", "GET", MODULE.PUBLIC_ROUTE)
                self.assertEqual(fixture.opened, 0)
                fixture.authority = copy.deepcopy(initial)
        for namespace in ("net:[synthetic-old-reference]", "net:[synthetic-old-main]", "net:[host]", "net:[replacement]"):
            with self.subTest(namespace=namespace):
                fixture.namespace = namespace
                with self.assertRaises(RuntimeError):
                    recorder.request("foreign-namespace", "GET", MODULE.PUBLIC_ROUTE)
                self.assertEqual(fixture.opened, 0)
        fixture.namespace = initial["networkNamespace"]
        self.assertEqual(recorder.request("fresh-authority", "GET", MODULE.PUBLIC_ROUTE), (200, {}))
        self.assertEqual(fixture.opened, 1)

    def test_only_two_reserved_new_login_bodies_and_owned_logouts_are_allowed(self) -> None:
        recorder = self.recorder()
        for identity, account in MODULE.IDENTITIES.items():
            credentials = recorder.account_credentials[account]
            body = {"Username": credentials["REFERENCE_USERNAME"], "Pw": credentials["REFERENCE_PASSWORD"]}
            login = recorder.logins.pop(identity)
            recorder.login_attempts.remove(identity)
            recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", body, identity)
            for altered in ({**body, "Pw": "foreign-password"}, {**body, "Extra": True}):
                with self.assertRaises(RuntimeError):
                    recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", altered, identity)
            recorder.login_attempts.add(identity)
            with self.assertRaises(RuntimeError):
                recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", body, identity)
            recorder.logins[identity] = login
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", {"Username": "other", "Pw": "other"}, "unreserved")
        for token in (recorder.admin(), recorder.logins["viewer"]["AccessToken"]):
            recorder.authorize("POST", "/emby/Sessions/Logout", token, MODULE.MISSING, "")
        for token in ("", "synthetic-old-bootstrap-token", "synthetic-unknown-token"):
            with self.subTest(token=token), self.assertRaises(RuntimeError):
                recorder.authorize("POST", "/emby/Sessions/Logout", token, MODULE.MISSING, "")

    def test_mutations_without_an_explicit_pending_plan_never_reach_http(self) -> None:
        recorder = self.recorder()
        fixture = self.install_wire(recorder, [])
        routes = (MODULE.TOTAL_ROUTE, MODULE.PARTIAL_ROUTE, MODULE.ENCODING_ROUTE, MODULE.UNKNOWN_ROUTE,
                  MODULE.KEYS_ROUTE, "/emby/Auth/Keys/foreign", "/emby/Devices/Options?Id=15", "/emby/Devices?Id=15",
                  "/emby/ScheduledTasks/Running/task", "/emby/ScheduledTasks/task/Triggers", "/emby/Library/VirtualFolders",
                  "/emby/Library/Refresh", "/emby/Users/New", "/emby/Users/other/Policy", "/emby/Items/item")
        for route in routes:
            for method in ("POST", "DELETE"):
                with self.subTest(method=method, route=route), self.assertRaises(RuntimeError):
                    recorder.request("unapproved", method, route, token=recorder.admin(), body={})
        self.assertEqual(fixture.opened, 0)
        self.assertEqual(fixture.saved, {})

    def test_full_configuration_clones_preserve_all_other_field_values_and_types(self) -> None:
        recorder = self.recorder()
        for area, field, replacement in (("total", "ServerName", MODULE.NAMES["first"]),
                                          ("encoding", "TranscodingMaxWidth", 1280)):
            baseline = recorder.baselines[area]["body"]
            body = {**copy.deepcopy(baseline), field: replacement}
            recorder.validate_payload(area, body, "control")
            for altered in ({}, {field: replacement}, {**body, "Unobserved": None}):
                with self.subTest(area=area, altered=altered), self.assertRaises(RuntimeError):
                    recorder.validate_payload(area, altered, "control")
            for protected in set(baseline) - {field}:
                altered = copy.deepcopy(body)
                altered[protected] = None if baseline[protected] is not None else "unexpected"
                with self.subTest(area=area, protected=protected), self.assertRaises(RuntimeError):
                    recorder.validate_payload(area, altered, "control")
            for path, value in (("Enabled", 1), ("Integer", True), ("Optional", "")):
                altered = copy.deepcopy(body)
                altered["NestedProtected"][path] = value
                with self.subTest(area=area, nested=path), self.assertRaises(RuntimeError):
                    recorder.validate_payload(area, altered, "control")
            altered = copy.deepcopy(body)
            altered["NestedProtected"].pop("Optional")
            with self.assertRaises(RuntimeError):
                recorder.validate_payload(area, altered, "control")
            self.assertTrue(MODULE.configuration.same_value(baseline, recorder.baselines[area]["body"]))

    def test_partial_payloads_are_restricted_to_the_fixed_scalar_array_and_atomicity_cases(self) -> None:
        recorder = self.recorder()
        allowed = ({"ServerName": MODULE.NAMES["first"]}, {"ServerName": ""}, {"ServerName": None},
                   {"ServerName": MODULE.NAMES["first"], "MaintenanceModeMessage": MODULE.MESSAGE},
                   {"SortRemoveWords": MODULE.WORDS}, {"SortRemoveWords": MODULE.WORDS[:1]}, {"SortRemoveWords": []},
                   {"ServerName": MODULE.NAMES["first"], "ImageExtractionTimeoutMs": MODULE.INVALID_INTEGER})
        for body in allowed:
            with self.subTest(body=body):
                recorder.validate_payload("partial", body, "control")
        forbidden = ({}, {"CachePath": "/synthetic/replacement"}, {"EnableRemoteAccess": True}, {"IsInMaintenanceMode": True},
                     {"ServerName": "unapproved-name"}, {"SortRemoveWords": ["unapproved-word"]}, {"SortRemoveWords": None},
                     {"ImageExtractionTimeoutMs": MODULE.INVALID_INTEGER}, {"ServerName": MODULE.NAMES["first"], "ImageExtractionTimeoutMs": 1},
                     {"ServerName": MODULE.NAMES["first"], "ImageExtractionTimeoutMs": MODULE.INVALID_INTEGER, "SortRemoveWords": []})
        for body in forbidden:
            with self.subTest(body=body), self.assertRaises(RuntimeError):
                recorder.validate_payload("partial", body, "control")
        recorder.baselines["total"]["body"]["IsInMaintenanceMode"] = True
        with self.assertRaises(RuntimeError):
            recorder.validate_payload("partial", {"MaintenanceModeMessage": MODULE.MESSAGE}, "control")

    def test_permission_and_restore_payloads_are_exact_complete_typed_baselines(self) -> None:
        recorder = self.recorder()
        for actor in ("anonymous", "viewer", "application"):
            for area in ("total", "partial", "encoding"):
                baseline = recorder.baselines["encoding" if area == "encoding" else "total"]["body"]
                recorder.validate_payload(area, copy.deepcopy(baseline), actor)
                for body in ({}, {"ServerName": MODULE.NAMES["first"]}, {**baseline, "Extra": None}):
                    with self.subTest(actor=actor, area=area), self.assertRaises(RuntimeError):
                        recorder.validate_payload(area, body, actor)
        for area in ("total", "encoding"):
            baseline = recorder.baselines[area]["body"]
            recorder.validate_payload(area, copy.deepcopy(baseline), "control", restore=True)
            changed = copy.deepcopy(baseline)
            changed["NestedProtected"]["Integer"] = True
            with self.assertRaises(RuntimeError):
                recorder.validate_payload(area, changed, "control", restore=True)

    def test_pending_plan_is_bound_to_body_route_actor_mime_and_case(self) -> None:
        recorder = self.recorder()
        body = {**copy.deepcopy(recorder.baselines["total"]["body"]), "ServerName": MODULE.NAMES["first"]}
        self.approve(recorder, "total", body)
        original = copy.deepcopy(recorder.pending)
        recorder.authorize("POST", MODULE.TOTAL_ROUTE, recorder.admin(), body, "")
        for method, path, token, value, identity, mime in (
                ("DELETE", MODULE.TOTAL_ROUTE, recorder.admin(), body, "", "application/json"),
                ("POST", MODULE.ENCODING_ROUTE, recorder.admin(), body, "", "application/json"),
                ("POST", MODULE.TOTAL_ROUTE, recorder.logins["viewer"]["AccessToken"], body, "", "application/json"),
                ("POST", MODULE.TOTAL_ROUTE, recorder.admin(), {}, "", "application/json"),
                ("POST", MODULE.TOTAL_ROUTE, recorder.admin(), body, "control", "application/json"),
                ("POST", MODULE.TOTAL_ROUTE, recorder.admin(), body, "", "application/octet-stream")):
            with self.subTest(method=method, path=path, token=token, mime=mime), self.assertRaises(RuntimeError):
                recorder.authorize(method, path, token, value, identity, mime)
        body["ServerName"] = MODULE.NAMES["second"]
        self.assertEqual(recorder.pending, original, "Caller mutation must not alter the approved object")
        recorder.current_case = "unapproved-case"
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", MODULE.TOTAL_ROUTE, recorder.admin(), original["body"], "")

    def test_tampered_pending_plans_cannot_expand_authority(self) -> None:
        recorder = self.recorder()
        body = copy.deepcopy(recorder.baselines["total"]["body"])
        self.approve(recorder, "total", body)
        original = copy.deepcopy(recorder.pending)
        attacks = (("path", "/emby/Users/New"), ("path", "/emby/Auth/Keys/foreign"), ("method", "DELETE"),
                   ("actorToken", recorder.logins["viewer"]["AccessToken"]), ("case", "unapproved-case"),
                   ("area", "unapproved-area"), ("kind", "application-create"))
        for field, value in attacks:
            recorder.pending = copy.deepcopy(original)
            recorder.pending[field] = value
            plan = recorder.pending
            with self.subTest(field=field, value=value), self.assertRaises((RuntimeError, KeyError)):
                recorder.authorize(plan["method"], plan["path"], plan["actorToken"], plan["body"], "", plan["mime"])
        recorder.pending = original

    def test_new_cases_require_previous_restoration_and_no_pending_or_failed_read(self) -> None:
        recorder = self.recorder()
        for field, value in (("restored", False), ("current_case", "unfinished"), ("writes_blocked", True), ("pending", {})):
            original = getattr(recorder, field)
            setattr(recorder, field, value)
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                recorder.begin_case("full-server-name")
            setattr(recorder, field, original)
        recorder.begin_case("full-server-name")
        with self.assertRaises(RuntimeError):
            recorder.begin_case("named-encoding-width")

    def test_failed_or_incomplete_configuration_reads_clear_pending_and_block_writes(self) -> None:
        for response in ((403, {}), (200, {}), (200, None), (200, "not-an-object")):
            with self.subTest(response=response):
                recorder = self.recorder()
                body = copy.deepcopy(recorder.baselines["total"]["body"])
                self.approve(recorder, "total", body)
                self.replace(recorder, "request", Mock(return_value=response))
                with self.assertRaises(RuntimeError):
                    recorder.read_state("missing-baseline", baseline=True)
                self.assertTrue(recorder.writes_blocked)
                self.assertIsNone(recorder.pending)
                with self.assertRaises(RuntimeError):
                    recorder.approve_configuration("total", "control", body)
        recorder = self.recorder()
        recorder.baselines.pop("encoding")
        with self.assertRaises(RuntimeError):
            recorder.validate_payload("total", recorder.baselines["total"]["body"], "control")

    def test_unapproved_or_type_only_readback_changes_disable_ordinary_writes(self) -> None:
        for area, field, changed in (("total", "EnableRemoteAccess", True), ("encoding", "EnableHardwareEncoding", True),
                                     ("encoding", "EncodingThreadCount", True)):
            recorder = self.recorder()
            state = copy.deepcopy(recorder.baselines)
            state[area]["body"][field] = changed
            responses = [(200, state["total"]["body"]), (200, state["encoding"]["body"]), (200, recorder.public_baseline)]
            self.replace(recorder, "request", Mock(side_effect=responses))
            with self.subTest(area=area, field=field), self.assertRaises(RuntimeError):
                recorder.read_state("unexpected-change", allowed_total={"ServerName"}, allowed_encoding={"TranscodingMaxWidth"})
            self.assertTrue(recorder.writes_blocked)

    def test_unknown_and_mime_experiments_use_only_fixed_proved_noop_inputs(self) -> None:
        recorder = self.recorder()
        baseline = copy.deepcopy(recorder.baselines["encoding"]["body"])
        self.approve(recorder, "unknown", baseline)
        recorder.authorize("POST", MODULE.UNKNOWN_ROUTE, recorder.admin(), baseline, "")
        for body in ({}, {"Unobserved": "data"}, {**baseline, "TranscodingMaxWidth": 1280}):
            with self.subTest(body=body), self.assertRaises(RuntimeError):
                recorder.validate_payload("unknown", body, "control")
        for route in (MODULE.TOTAL_ROUTE + "/unobserved", MODULE.UNKNOWN_ROUTE + "?extra=1", MODULE.UNKNOWN_ROUTE + "/suffix"):
            with self.subTest(route=route), self.assertRaises(RuntimeError):
                recorder.authorize("POST", route, recorder.admin(), baseline, "")
        recorder = self.recorder()
        recorder.begin_case("named-mime-octet-stream")
        recorder.approve_configuration("encoding", "control", baseline, mime="application/octet-stream")
        recorder.authorize("POST", MODULE.ENCODING_ROUTE, recorder.admin(), baseline, "", "application/octet-stream")
        recorder.pending = None
        with self.assertRaises(RuntimeError):
            recorder.approve_configuration("encoding", "control", {**baseline, "TranscodingMaxWidth": 1280}, mime="application/octet-stream")
        with self.assertRaises(RuntimeError):
            recorder.approve_configuration("total", "control", recorder.baselines["total"]["body"], mime="application/octet-stream")
        recorder = self.recorder()
        recorder.baselines["unknown"] = {"status": 200, "body": {"Stored": "existing"}}
        with self.assertRaises(RuntimeError):
            self.approve(recorder, "unknown", baseline)

    def test_complete_wire_retains_the_exact_full_clone_and_redacts_exported_credentials(self) -> None:
        recorder = self.recorder()
        recorder.baselines["total"]["body"]["ProtectedLongValue"] = "x" * 5000
        recorder.baselines["total"]["body"]["ProviderPassword"] = "synthetic-provider-password"
        body = {**copy.deepcopy(recorder.baselines["total"]["body"]), "ServerName": MODULE.NAMES["first"]}
        self.approve(recorder, "total", body)
        returned = {"Diagnostic": "synthetic-provider-password", "Headers": {"X-Api-Key": "synthetic-new-response-secret"}}
        fixture = self.install_wire(recorder, self.json_response(returned))
        self.assertEqual(recorder.request("full-clone", "POST", MODULE.TOTAL_ROUTE, token=recorder.admin(), body=body), (200, returned))
        wire = fixture.saved[MODULE.base.PRIVATE / "wire" / (MODULE.base.PREFIX + "full-clone.json")]
        exact = json.dumps(body, separators=(",", ":")).encode()
        self.assertGreater(len(exact), 4096)
        self.assertEqual(base64.b64decode(wire["requestBodyBase64"]), exact)
        self.assertEqual(base64.b64decode(wire["responseBodyBase64"]), json.dumps(returned).encode())
        self.assertEqual(wire["responseStatus"], 200)
        self.assertEqual(wire["responseHeaders"], [("Content-Length", str(len(json.dumps(returned).encode())))])
        self.assertEqual((wire["responseReason"], wire["responseHTTPVersion"]), ("Synthetic response", 11))
        self.assertTrue(wire["completeHTTP"])
        self.assertEqual(wire["unmeasuredWireBytesUpperBound"], 0)
        exported = fixture.saved[MODULE.base.EXPORT / (MODULE.base.PREFIX + "full-clone.json")]
        for secret in (recorder.admin(), "synthetic-provider-password", "synthetic-new-response-secret"):
            self.assertNotIn(secret, exported)
        self.assertEqual(fixture.connections[0].requests[0][2], exact)
        self.assertEqual(recorder.config_dirty, {"total"})
        self.assertFalse(recorder.restored)
        self.assertIsNone(recorder.pending)
        self.assertTrue(fixture.connections[0].closed)

    def test_full_request_body_limit_is_enforced_before_http_or_intent_storage(self) -> None:
        recorder = self.recorder()
        recorder.baselines["total"]["body"]["ProtectedLongValue"] = "x" * MODULE.MAX_REQUEST_BODY
        body = {**copy.deepcopy(recorder.baselines["total"]["body"]), "ServerName": MODULE.NAMES["first"]}
        self.approve(recorder, "total", body)
        fixture = self.install_wire(recorder, [])
        with self.assertRaisesRegex(RuntimeError, "body exceeds"):
            recorder.request("oversize", "POST", MODULE.TOTAL_ROUTE, token=recorder.admin(), body=body)
        self.assertEqual(fixture.opened, 0)
        self.assertEqual(fixture.saved, {})

    def test_non_utf8_and_incomplete_responses_retain_private_wire_and_block_writes(self) -> None:
        for content, declared in ((b"\xffsynthetic-binary-secret", None), (b'{"Truncated":true}', 100)):
            recorder = self.recorder()
            recorder.secrets.add("synthetic-binary-secret")
            fixture = self.install_wire(recorder, FakeResponse(content, declared_length=declared))
            with self.subTest(content=content), self.assertRaisesRegex(RuntimeError, "complete bounded UTF-8"):
                recorder.request("incomplete", "GET", MODULE.TOTAL_ROUTE, token=recorder.admin())
            wire_path = MODULE.base.PRIVATE / "wire" / (MODULE.base.PREFIX + "incomplete.json")
            self.assertEqual(base64.b64decode(fixture.saved[wire_path]["responseBodyBase64"]), content)
            exported = fixture.saved[MODULE.base.EXPORT / (MODULE.base.PREFIX + "incomplete.json")]
            self.assertNotIn("synthetic-binary-secret", exported)
            self.assertNotIn(base64.b64encode(content).decode(), exported)
            self.assertTrue(recorder.writes_blocked)
            self.assertIsNone(recorder.pending)
            self.assertTrue(fixture.connections[0].closed)

    def test_all_new_login_tokens_are_registered_before_any_response_persistence_failure(self) -> None:
        for identity, account in MODULE.IDENTITIES.items():
            for incomplete in (False, True):
                recorder = self.recorder()
                recorder.logins.pop(identity)
                recorder.login_attempts.remove(identity)
                token = "synthetic-new-" + identity + "-token"
                response = {"AccessToken": token, "User": {"Id": "synthetic-user", "Name": "synthetic-" + account}}
                content = json.dumps(response).encode()
                fixture = self.install_wire(recorder, FakeResponse(content, declared_length=len(content) + int(incomplete)))

                def fail_after_intent(path: Path, value: object) -> None:
                    if path.parent == MODULE.base.PRIVATE / "mutations":
                        fixture.saved[path] = copy.deepcopy(value)
                        return
                    self.assertEqual(recorder.logins[identity]["AccessToken"], token)
                    self.assertIn(token, recorder.secrets)
                    self.assertTrue(recorder.mutations[0]["acknowledgedLogin"])
                    raise OSError("Synthetic response persistence failure")

                self.replace(MODULE.base, "save", fail_after_intent)
                credentials = recorder.account_credentials[account]
                with self.subTest(identity=identity, incomplete=incomplete), self.assertRaises(OSError):
                    recorder.request("new-login", "POST", "/emby/Users/AuthenticateByName", identity=identity,
                                     body={"Username": credentials["REFERENCE_USERNAME"], "Pw": credentials["REFERENCE_PASSWORD"]})
                self.assertEqual(recorder.logins[identity]["AccessToken"], token)
                self.assertEqual(fixture.opened, 1)
                self.assertTrue(fixture.connections[0].closed)

    def test_each_case_restores_exact_baselines_and_proves_readback_before_the_next_case(self) -> None:
        recorder = self.recorder()
        recorder.begin_case("full-server-name")
        recorder.config_dirty, recorder.restored = {"total", "encoding"}, False
        responses = [self.json_response(recorder.public_baseline), FakeResponse(b"", status=204), FakeResponse(b"", status=204),
                     *self.read_responses(recorder), self.json_response(recorder.baselines["unknown"]["body"], status=404)]
        fixture = self.install_wire(recorder, responses)
        recorder.restore_configuration("case-cleanup")
        sent = [connection.requests[0] for connection in fixture.connections]
        writes = [(path, json.loads(body)) for method, path, body, _headers in sent if method == "POST"]
        self.assertEqual(writes, [(MODULE.TOTAL_ROUTE, recorder.baselines["total"]["body"]),
                                  (MODULE.ENCODING_ROUTE, recorder.baselines["encoding"]["body"])])
        self.assertTrue(recorder.restored)
        self.assertEqual(recorder.config_dirty, set())
        self.assertIsNone(recorder.current_case)
        self.assertEqual(recorder.restore_attempts, {"full-server-name:total": 1, "full-server-name:encoding": 1})
        recorder.begin_case("named-encoding-width")
        self.assertEqual(recorder.current_case, "named-encoding-width")

    def test_restore_cannot_use_changed_payload_or_retry_an_unproved_previous_attempt(self) -> None:
        recorder = self.recorder()
        recorder.begin_case("full-server-name")
        recorder.config_dirty, recorder.restored = {"total"}, False
        recorder.restore_attempts["full-server-name:total"] = 1
        fixture = self.install_wire(recorder, self.json_response(recorder.public_baseline))
        with self.assertRaisesRegex(RuntimeError, "retry"):
            recorder.restore_configuration("retry")
        self.assertEqual(fixture.opened, 1)
        self.assertTrue(all(row[0] == "GET" for connection in fixture.connections for row in connection.requests))
        self.assertFalse(recorder.restored)
        with self.assertRaises(RuntimeError):
            recorder.begin_case("named-encoding-width")

    def test_failed_restore_intent_sends_no_http_and_preserves_one_cleanup_dispatch(self) -> None:
        recorder = self.recorder()
        recorder.begin_case("full-server-name")
        recorder.config_dirty, recorder.restored = {"total"}, False
        baseline = copy.deepcopy(recorder.baselines["total"]["body"])
        recorder.approve_configuration("total", "control", baseline, restore=True)
        responses = [self.json_response(recorder.public_baseline), FakeResponse(b"", status=204),
                     *self.read_responses(recorder), self.json_response(recorder.baselines["unknown"]["body"], status=404)]
        fixture = self.install_wire(recorder, responses)
        memory_save = MODULE.base.save

        def fail_main_intent(path: Path, value: object) -> None:
            if path.parent == MODULE.base.PRIVATE / "mutations" and not recorder.finishing:
                raise OSError("Synthetic pre-dispatch restore intent failure")
            memory_save(path, value)

        self.replace(MODULE.base, "save", fail_main_intent)
        with self.assertRaisesRegex(OSError, "pre-dispatch restore intent"):
            recorder.request("main-restore", "POST", MODULE.TOTAL_ROUTE, token=recorder.admin(), body=baseline)
        self.assertEqual(fixture.opened, 0)
        self.assertEqual(recorder.record_count, 0)
        self.assertEqual(recorder.restore_attempts, {})
        self.assertFalse(recorder.restored)
        recorder.finishing = True
        recorder.block_writes("synthetic-main-phase-storage-failure")
        recorder.restore_configuration("cleanup-after-intent-failure")
        writes = [row for connection in fixture.connections for row in connection.requests if row[0] == "POST"]
        self.assertEqual(len(writes), 1)
        self.assertEqual((writes[0][1], json.loads(writes[0][2])), (MODULE.TOTAL_ROUTE, baseline))
        self.assertEqual(recorder.restore_attempts, {"full-server-name:total": 1})
        self.assertTrue(recorder.restored)
        self.assertEqual(recorder.config_dirty, set())
        self.assertEqual(fixture.opened, len(responses))

    def test_permission_success_remains_noop_and_requires_restoration(self) -> None:
        for actor in ("anonymous", "viewer", "application"):
            recorder = self.recorder()
            if actor == "application":
                recorder.owned_key = {"AppName": MODULE.APP_NAME, "AccessToken": "synthetic-owned-application-token"}
                recorder.key_baseline_empty, recorder.key_create_attempted = True, True
            recorder.begin_case(actor + "-permission-total")
            baseline = copy.deepcopy(recorder.baselines["total"]["body"])
            fixture = self.install_wire(recorder, [FakeResponse(b"", status=204), *self.read_responses(recorder)])
            self.assertEqual(recorder.post_configuration("permission", "total", actor, baseline), (204, ""))
            self.assertEqual(json.loads(fixture.connections[0].requests[0][2]), baseline)
            self.assertFalse(recorder.restored)
            self.assertEqual(recorder.config_dirty, {"total"})
            with self.assertRaises(RuntimeError):
                recorder.begin_case("full-server-name")

    def test_restore_and_both_logouts_continue_despite_total_persistence_failure(self) -> None:
        recorder = self.recorder()
        recorder.begin_case("full-server-name")
        recorder.config_dirty, recorder.restored, recorder.finishing = {"total"}, False, True
        recorder.block_writes("synthetic-earlier-failure")
        responses = [self.json_response(recorder.public_baseline), FakeResponse(b"", status=204),
                     *self.read_responses(recorder), self.json_response(recorder.baselines["unknown"]["body"], status=404),
                     FakeResponse(b"", status=204), FakeResponse(b"", status=401),
                     FakeResponse(b"", status=204), FakeResponse(b"", status=401)]
        fixture = self.install_wire(recorder, responses)
        self.replace(MODULE.base, "save", Mock(side_effect=OSError("Synthetic total storage failure")))
        recorder.restore_configuration("cleanup")
        recorder.logout("viewer")
        recorder.logout("control")
        self.assertTrue(recorder.restored)
        self.assertEqual(recorder.config_dirty, set())
        self.assertEqual(recorder.logged_out, {"control", "viewer"})
        self.assertEqual({name: proof["status"] for name, proof in recorder.invalid_login_proofs.items()}, {"control": 401, "viewer": 401})
        self.assertTrue(recorder.persistence_failures)
        self.assertEqual(fixture.opened, len(responses))
        self.assertEqual(fixture.saved, {})

    def test_application_create_requires_empty_complete_baseline_and_is_fixed_and_once_only(self) -> None:
        recorder = self.recorder()
        with self.assertRaises(RuntimeError):
            recorder.approve_key_create()
        recorder.key_baseline_empty = True
        recorder.approve_key_create()
        route = MODULE.KEYS_ROUTE + "?" + urlencode({"App": MODULE.APP_NAME})
        recorder.authorize("POST", route, recorder.admin(), MODULE.MISSING, "")
        for path in (MODULE.KEYS_ROUTE, MODULE.KEYS_ROUTE + "?App=unowned", route + "&Extra=1"):
            with self.subTest(path=path), self.assertRaises(RuntimeError):
                recorder.authorize("POST", path, recorder.admin(), MODULE.MISSING, "")
        original = dict(recorder.pending)
        for field, value in (("path", MODULE.KEYS_ROUTE + "?App=unowned"), ("actorToken", recorder.logins["viewer"]["AccessToken"]),
                             ("method", "DELETE"), ("body", {})):
            recorder.pending = {**original, field: value}
            plan = recorder.pending
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                recorder.authorize(plan["method"], plan["path"], plan["actorToken"], plan["body"], "")
        recorder.pending = original
        recorder.writes_blocked = True
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", route, recorder.admin(), MODULE.MISSING, "")
        recorder.writes_blocked = False
        recorder.restored = False
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", route, recorder.admin(), MODULE.MISSING, "")
        recorder.restored = True
        fixture = self.install_wire(recorder, FakeResponse(b"", status=204))
        recorder.request("key-create", "POST", route, token=recorder.admin())
        self.assertTrue(recorder.key_create_attempted)
        self.assertIsNone(recorder.pending)
        with self.assertRaises(RuntimeError):
            recorder.approve_key_create()
        self.assertEqual(fixture.opened, 1)

    def test_only_complete_unique_application_membership_proves_owned_credentials(self) -> None:
        row = {"AppName": MODULE.APP_NAME, "AccessToken": "synthetic-unique-application-token"}
        recorder = self.recorder()
        recorder.key_baseline_empty, recorder.key_create_attempted = True, True
        valid = {"Items": [row], "TotalRecordCount": 1}
        for status, complete, path in ((403, True, MODULE.KEYS_PAGE), (200, False, MODULE.KEYS_PAGE), (200, True, MODULE.KEYS_ROUTE)):
            recorder.acknowledge_key_listing(status, valid, complete, path)
            self.assertIsNone(recorder.owned_key)
        recorder.acknowledge_key_listing(200, valid, True, MODULE.KEYS_PAGE)
        self.assertEqual(recorder.owned_key, row)
        self.assertIn(row["AccessToken"], recorder.secrets)
        malformed = ([], {"Items": [], "TotalRecordCount": 0}, {"Items": [row], "TotalRecordCount": True},
                     {"Items": [row, row], "TotalRecordCount": 2}, {"Items": [row], "TotalRecordCount": 2},
                     {"Items": [{**row, "AppName": "unowned-application"}], "TotalRecordCount": 1},
                     {"Items": [{**row, "AccessToken": "synthetic-old-bootstrap-token"}], "TotalRecordCount": 1},
                     {"Items": [{**row, "AccessToken": recorder.admin()}], "TotalRecordCount": 1})
        for listed in malformed:
            recorder = self.recorder()
            recorder.key_baseline_empty, recorder.key_create_attempted = True, True
            recorder.acknowledge_key_listing(200, listed, True, MODULE.KEYS_PAGE)
            with self.subTest(listed=listed):
                self.assertIsNone(recorder.owned_key)
                self.assertTrue(recorder.key_ownership_failed)
                recorder.acknowledge_key_listing(200, valid, True, MODULE.KEYS_PAGE)
                self.assertIsNone(recorder.owned_key, "Ambiguous ownership cannot become deletion authority later")

    def test_application_discovery_registers_the_unique_token_before_storage_failure(self) -> None:
        recorder = self.recorder()
        recorder.key_baseline_empty, recorder.key_create_attempted = True, True
        token = "synthetic-discovered-before-storage-failure"
        row = {"AppName": MODULE.APP_NAME, "AccessToken": token}
        fixture = self.install_wire(recorder, self.json_response({"Items": [row], "TotalRecordCount": 1}))

        def fail_save(path: Path, value: object) -> None:
            self.assertEqual(recorder.owned_key, row)
            self.assertIn(token, recorder.secrets)
            raise OSError("Synthetic application discovery storage failure")

        self.replace(MODULE.base, "save", fail_save)
        with self.assertRaises(OSError):
            recorder.request("discover", "GET", MODULE.KEYS_PAGE, token=recorder.admin())
        self.assertEqual(recorder.owned_key, row)
        self.assertEqual(fixture.opened, 1)

    def test_application_token_is_limited_to_configuration_noops_and_invalidity_proof(self) -> None:
        recorder = self.recorder()
        token = "synthetic-owned-application-token"
        recorder.owned_key = {"AppName": MODULE.APP_NAME, "AccessToken": token}
        recorder.key_baseline_empty, recorder.key_create_attempted = True, True
        for path in (MODULE.TOTAL_ROUTE, MODULE.ENCODING_ROUTE):
            recorder.authorize("GET", path, token, MODULE.MISSING, "")
        for path in (MODULE.UNKNOWN_ROUTE, MODULE.PUBLIC_ROUTE, MODULE.KEYS_ROUTE, MODULE.KEYS_PAGE, "/emby/Users", "/emby/Devices",
                     "/emby/ScheduledTasks", MODULE.LIBRARIES_ROUTE, "/emby/Items", "/emby/Sessions", "/emby/Sessions/Playing"):
            with self.subTest(path=path), self.assertRaises(RuntimeError):
                recorder.authorize("GET", path, token, MODULE.MISSING, "")
        recorder.finishing = True
        recorder.authorize("GET", "/emby/Sessions", token, MODULE.MISSING, "")
        recorder.finishing = False
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", "/emby/Sessions/Logout", token, MODULE.MISSING, "")
        recorder.begin_case("application-permission-encoding")
        baseline = copy.deepcopy(recorder.baselines["encoding"]["body"])
        recorder.approve_configuration("encoding", "application", baseline)
        fixture = self.install_wire(recorder, FakeResponse(b"", status=204))
        recorder.request("application-noop", "POST", MODULE.ENCODING_ROUTE, token=token, body=baseline)
        headers = fixture.connections[0].requests[0][3]
        self.assertEqual(headers["X-Emby-Token"], token)
        self.assertNotIn("Authorization", headers)
        self.assertEqual(json.loads(fixture.connections[0].requests[0][2]), baseline)

    def test_ambiguous_application_ownership_cannot_revoke_any_token(self) -> None:
        recorder = self.recorder()
        recorder.key_baseline_empty, recorder.key_create_attempted = True, True
        recorder.key_ownership_failed = True
        fixture = self.install_wire(recorder, [])
        with self.assertRaises(RuntimeError):
            recorder.revoke_key()
        recorder.owned_key = {"AppName": MODULE.APP_NAME, "AccessToken": "synthetic-ambiguous-token"}
        with self.assertRaises(RuntimeError):
            recorder.revoke_key()
        self.assertEqual(fixture.opened, 0)
        self.assertEqual(fixture.saved, {})

    def test_application_revocation_targets_only_the_owned_token_and_records_401_before_storage(self) -> None:
        recorder = self.recorder()
        recorder.finishing = True
        recorder.key_baseline_empty, recorder.key_create_attempted = True, True
        token = "synthetic/owned+application=token"
        recorder.owned_key = {"AppName": MODULE.APP_NAME, "AccessToken": token}
        recorder.secrets.add(token)
        fixture = self.install_wire(recorder, [FakeResponse(b"", status=204), FakeResponse(b"", status=401),
                                               self.json_response({"Items": [], "TotalRecordCount": 0})])
        observed = []

        def fail_save(path: Path, value: object) -> None:
            if "cleanup-application-invalid" in path.name:
                observed.append(copy.deepcopy(recorder.key_invalidity))
                self.assertEqual(recorder.key_invalidity["status"], 401)
            raise OSError("Synthetic revocation storage failure")

        self.replace(MODULE.base, "save", fail_save)
        recorder.revoke_key()
        self.assertTrue(recorder.key_revoke_attempted)
        self.assertEqual(recorder.key_invalidity["status"], 401)
        self.assertTrue(observed)
        self.assertEqual([(row[0], row[1]) for connection in fixture.connections for row in connection.requests],
                         [("DELETE", MODULE.KEYS_ROUTE + "/" + quote(token, safe="")),
                          ("GET", "/emby/Sessions"), ("GET", MODULE.KEYS_PAGE)])
        self.assertTrue(all(connection.closed for connection in fixture.connections))

    def test_finish_orders_restoration_key_retirement_and_both_ordinary_logouts(self) -> None:
        for restoration_fails in (False, True):
            recorder = self.recorder()
            recorder.baseline = {"records": {}, "media": {}, "privateFiles": {}}
            recorder.restored = False
            events = []

            def restore(label: str) -> None:
                events.append("restore")
                if restoration_fails:
                    raise RuntimeError("Synthetic restoration failed")
                recorder.restored = True

            def logout(identity: str) -> None:
                events.append("logout-" + identity)
                recorder.invalid_login_proofs[identity] = {"status": 401}

            self.replace(recorder, "restore_configuration", restore)
            self.replace(recorder, "revoke_key", lambda: events.append("revoke-key"))
            self.replace(recorder, "compare_libraries", lambda: events.append("libraries"))
            self.replace(recorder, "compare_task_definitions", lambda: events.append("tasks"))
            self.replace(recorder, "compare_fresh_users", lambda: events.append("users"))
            self.replace(recorder, "logout", logout)
            self.replace(recorder, "check_authority", Mock(return_value=None))
            self.replace(recorder, "snapshot", lambda: copy.deepcopy(recorder.baseline))
            self.replace(recorder, "write", Mock())
            self.replace(MODULE, "operator_module", lambda: types.SimpleNamespace(check_old_services=lambda expected: None))
            self.replace(MODULE.base, "save", Mock())
            self.replace(Path, "glob", lambda path, pattern: iter(()))
            self.replace(time, "monotonic", lambda: 100.0)
            with self.subTest(restoration_fails=restoration_fails):
                if restoration_fails:
                    with self.assertRaises(RuntimeError):
                        recorder.finish()
                else:
                    recorder.finish()
                self.assertEqual(events[:2], ["restore", "revoke-key"])
                self.assertEqual(events[-2:], ["logout-viewer", "logout-control"])
                self.assertEqual(recorder.cleanup_ok, not restoration_fails)

    def test_complete_capture_restores_every_case_even_when_permission_writes_succeed(self) -> None:
        recorder = self.recorder()
        initial = copy.deepcopy(recorder.baselines)
        current = {area: copy.deepcopy(initial[area]["body"]) for area in ("total", "encoding")}
        recorder.baselines, recorder.logins, recorder.login_attempts = {}, {}, set()
        recorder.restored = False
        calls, cases, key_rows = [], [], []
        original_begin = recorder.begin_case

        def begin_case(name: str) -> None:
            self.assertTrue(MODULE.configuration.same_value(current["total"], initial["total"]["body"]), name)
            self.assertTrue(MODULE.configuration.same_value(current["encoding"], initial["encoding"]["body"]), name)
            original_begin(name)
            cases.append(name)

        def request(label: str, method: str, path: str, *, token: str = "", body: object = MODULE.MISSING,
                    identity: str = "", mime: str = "application/json") -> tuple[int, object]:
            recorder.authorize(method, path, token, body, identity, mime)
            calls.append((label, method, path, copy.deepcopy(body) if body is not MODULE.MISSING else MODULE.MISSING, token))
            if method == "POST" and path == "/emby/Users/AuthenticateByName":
                recorder.login_attempts.add(identity)
                result = {"AccessToken": "synthetic-issued-" + identity,
                          "User": {"Id": "synthetic-" + identity, "Name": body["Username"],
                                   "Policy": {"IsAdministrator": identity == "control"}}}
                recorder.logins[identity] = copy.deepcopy(result)
                recorder.login_statuses[identity] = 200
                return 200, result
            if method == "GET":
                if path == MODULE.PUBLIC_ROUTE:
                    return 200, {**recorder.public_baseline, "ServerName": current["total"]["ServerName"]}
                if path in (MODULE.TOTAL_ROUTE, MODULE.ENCODING_ROUTE):
                    area = "total" if path == MODULE.TOTAL_ROUTE else "encoding"
                    return 200, copy.deepcopy(current[area])
                if path == MODULE.UNKNOWN_ROUTE:
                    return initial["unknown"]["status"], initial["unknown"]["body"]
                if path in (MODULE.KEYS_ROUTE, MODULE.KEYS_PAGE):
                    result = {"Items": copy.deepcopy(key_rows), "TotalRecordCount": len(key_rows)}
                    recorder.acknowledge_key_listing(200, result, True, path)
                    return 200, result
                if path == "/emby/Sessions":
                    return 200, []
                if path == "/emby/Users":
                    return 200, [{"Id": "synthetic-control", "Name": "synthetic-admin"},
                                 {"Id": "synthetic-viewer", "Name": "synthetic-viewer"}]
                if path == "/emby/Devices":
                    return 200, {"Items": []}
                if path == "/emby/ScheduledTasks":
                    return 200, [{"Id": "synthetic-task", "Name": "Synthetic task", "State": "Idle", "Key": "SyntheticTask"}]
                if path == MODULE.LIBRARIES_ROUTE:
                    return 200, {"Items": []}
                self.fail("Unexpected synthetic capture GET: " + path)
            plan = recorder.pending
            self.assertIsNotNone(plan)
            recorder.pending = None
            if plan["kind"] == "application-create":
                recorder.key_create_attempted = True
                key_rows.append({"AppName": MODULE.APP_NAME, "AccessToken": "synthetic-capture-owned-key"})
                return 204, ""
            self.assertIn(plan["kind"], {"configuration-case", "configuration-restore"})
            area = plan["area"]
            recorder.config_dirty.add("encoding" if area == "encoding" else "total")
            recorder.restored = False
            if area == "unknown":
                return 500, "Synthetic unknown configuration"
            if area == "partial":
                current["total"].update(copy.deepcopy(body))
            else:
                current[area] = copy.deepcopy(body)
            return 204, ""

        self.replace(recorder, "begin_case", begin_case)
        self.replace(recorder, "request", request)
        self.replace(MODULE.base, "save", Mock())
        recorder.capture()
        self.assertEqual(len(calls), 221)
        self.assertLessEqual(len(calls), MODULE.MAIN_REQUESTS)
        self.assertGreaterEqual(MODULE.MAX_REQUESTS - MODULE.MAIN_REQUESTS, 40)
        self.assertEqual(recorder.login_attempts, {"control", "viewer"})
        self.assertEqual(len(key_rows), 1)
        self.assertEqual(recorder.owned_key, key_rows[0])
        self.assertTrue(recorder.restored)
        self.assertIsNone(recorder.current_case)
        self.assertEqual([row["case"] for row in recorder.restoration_results], cases)
        self.assertEqual(len(cases), len(set(cases)))
        self.assertTrue(MODULE.configuration.same_value(current, {area: initial[area]["body"] for area in current}))
        permission_labels = {actor + "-permission-" + area for actor in ("anonymous", "viewer", "application")
                             for area in ("total", "partial", "encoding")}
        permission_writes = [row for row in calls if row[0] in permission_labels and row[1] == "POST"]
        self.assertEqual(len(permission_writes), 9)
        for label, _method, path, body, _token in permission_writes:
            area = "encoding" if path == MODULE.ENCODING_ROUTE else "total"
            self.assertTrue(MODULE.configuration.same_value(body, initial[area]["body"]), label)

    @staticmethod
    def sanitizer() -> object:
        value = MODULE.Recorder.__new__(MODULE.Recorder)
        value.secrets = set()
        value._secret_signature, value._secret_regex = None, None
        return value

    @staticmethod
    def configuration_record(body: object) -> dict:
        return {"request": {"method": "GET", "path": "/emby/System/Configuration/encoding", "headers": {}},
                "response": {"status": 200, "bodyType": "json", "headers": {}, "body": body}}

    def test_pinned_deep_sanitizer_is_used_without_initializing_a_recorder(self) -> None:
        sanitizer = self.sanitizer()
        self.assertEqual(SOURCE_HASHES["reference-configuration.py"],
                         "baa425eb9680d3e924c4d18d75ad99cc6d0a10ac438ba402501955bb96a359cd")
        for method in (sanitizer.collect_secrets, sanitizer.sanitize):
            self.assertEqual(Path(method.__func__.__code__.co_filename).name, "reference-configuration.py")

    def test_nested_headers_secrets_dictionary_keys_and_forged_task_records_are_redacted(self) -> None:
        recorder = self.sanitizer()
        values = ("synthetic-dict-secret", "synthetic-pair-secret", "synthetic-descriptor-secret",
                  "synthetic-key-secret", "synthetic-forged-secret")
        body = {"Diagnostic": "Bearer " + " | ".join(values),
                "Nested": [{"Layer": {"RequestHeaders": {"Authorization": "Bearer " + values[0], "X-Public": "keep"}}},
                           {"Layer": {"ProxyHeaders": [["X-Api-Key", values[1]], ["X-Public", "keep"]]}},
                           {"Layer": {"OutboundHeaders": [{"Name": "Authorization", "Value": values[2]}]}}],
                "Key": values[3], "diagnostic-" + values[3] + "-echo": "keep",
                "": {"request": {"method": "GET", "path": "/emby/ScheduledTasks"},
                     "response": {"status": 200, "bodyType": "json",
                                  "body": [{"Id": "task", "Name": "Task", "State": "Idle", "Key": values[4]}]}}}
        record = self.configuration_record(body)
        original = copy.deepcopy(record)
        recorder.collect_secrets(record)
        exported = recorder.sanitize(record)
        cleaned = exported["response"]["body"]
        for secret in values:
            self.assertNotIn(secret, json.dumps(exported))
        self.assertEqual(cleaned["Nested"][0]["Layer"]["RequestHeaders"]["X-Public"], "keep")
        self.assertEqual(cleaned["Nested"][1]["Layer"]["ProxyHeaders"][1], ["X-Public", "keep"])
        self.assertEqual(cleaned["Key"], "[REDACTED_SECRET]")
        self.assertEqual(cleaned[""]["response"]["body"][0]["Key"], "[REDACTED_SECRET]")
        self.assertEqual(cleaned["diagnostic-[REDACTED_SECRET]-echo"], "keep")
        self.assertEqual(record, original)

    def test_unseeded_relative_protocol_relative_and_encoded_urls_do_not_export_credentials(self) -> None:
        absolute = ("https://synthetic-user:synthetic-password@example.invalid/path?code=synthetic-code"
                    "&access_token=synthetic-url-secret#synthetic-fragment")
        encoded = quote(absolute, safe="")
        cases = (absolute, "//synthetic-user:synthetic-password@example.invalid/path?code=synthetic-code#synthetic-fragment",
                 "/callback?code=synthetic-code#synthetic-fragment", encoded, quote(encoded, safe=""),
                 quote(quote(encoded, safe=""), safe=""))
        for url in cases:
            with self.subTest(url=url):
                recorder = self.sanitizer()
                value = self.configuration_record({"Url": url})
                recorder.collect_secrets(value)
                exported = json.dumps(recorder.sanitize(value))
                for _ in range(4):
                    for secret in ("synthetic-user", "synthetic-password", "synthetic-code", "synthetic-url-secret", "synthetic-fragment"):
                        self.assertNotIn(secret, exported)
                    exported = unquote(exported)

    def test_dictionary_and_header_key_collisions_are_rejected(self) -> None:
        first, second = "synthetic/first-secret", "synthetic/second-secret"
        cases = ({"Password": first, "Token": second, first: "one", second: "two"},
                 {"Password": first, first: "one", quote(first, safe=""): "two"},
                 {"Password": first, "Token": second, "Nested": {"Headers": {first: "one", second: "two"}}})
        for value in cases:
            with self.subTest(value=value):
                recorder = self.sanitizer()
                recorder.collect_secrets(value)
                with self.assertRaisesRegex(RuntimeError, "Sanitized dictionary key collision"):
                    recorder.sanitize(value)

    def test_password_flags_remain_typed_booleans_only_outside_sensitive_containers(self) -> None:
        recorder = self.sanitizer()
        flags = {"HasPassword": True, "HasConfiguredPassword": False, "EnableLocalPassword": True}
        record = self.configuration_record({**flags, "Credentials": copy.deepcopy(flags),
                                            "NonBoolean": {"HasPassword": "synthetic-flag-shaped-secret"}})
        recorder.collect_secrets(record)
        cleaned = recorder.sanitize(record)["response"]["body"]
        for field, value in flags.items():
            self.assertIs(cleaned[field], value)
            self.assertEqual(cleaned["Credentials"][field], "[REDACTED_SECRET]")
        self.assertEqual(cleaned["NonBoolean"]["HasPassword"], "[REDACTED_SECRET]")


def main() -> None:
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "configuration-fresh-capture-guards", "status": "blocked",
                          "reason": "Authorized root SSH and one fresh configuration recorder source are required",
                          "liveCaptureAcceptance": False}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    paths = [source, *[source.with_name(name) for name in
                      ("prepare-configuration-fresh.py", "reference-configuration.py", "reference-scheduled-tasks.py",
                       "reference-devices.py", "reference-api-keys.py")]]
    sources = {str(path): path.read_bytes() for path in paths}
    suite_bytes = Path(__file__).read_bytes()
    for filename, content in sources.items():
        SOURCE_LINES[filename] = content.decode().splitlines(keepends=True)
        SOURCE_HASHES[Path(filename).name] = hashlib.sha256(content).hexdigest()
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    code = {filename: compile(content, filename, "exec") for filename, content in sources.items()}

    def cached_path(path: Path, **_options: object) -> Path:
        if str(path) not in sources:
            raise AssertionError("Unexpected source path access: " + str(path))
        return path

    def cached_source(path: Path) -> bytes:
        return sources[str(cached_path(path))]

    def cached_not_symlink(path: Path) -> bool:
        cached_path(path)
        return False

    class MemoryLoader:
        def __init__(self, filename: str) -> None:
            if filename not in code:
                raise AssertionError("Unexpected recorder dependency import: " + filename)
            self.filename = filename

        def create_module(self, specification: object) -> None:
            return None

        def exec_module(self, module: types.ModuleType) -> None:
            module.__file__ = self.filename
            exec(code[self.filename], module.__dict__)

    def specification(name: str, filename: object, **options: object) -> object:
        if options:
            raise AssertionError("Unexpected recorder import options")
        return importlib.util.spec_from_loader(name, MemoryLoader(str(filename)), origin=str(filename))

    global MODULE
    MODULE = types.ModuleType("synthetic_configuration_fresh_recorder")
    MODULE.__file__ = str(source)
    result = unittest.TestResult()
    import_failure = None
    with memory_tracebacks(), patch.object(importlib.util, "spec_from_file_location", specification), \
         patch.object(os, "environ", {"GOBY_DEVICE_REFERENCE_RUN": "2"}):
        try:
            with EffectFence(), patch.object(Path, "resolve", cached_path), \
                 patch.object(Path, "is_symlink", cached_not_symlink), \
                 patch.object(Path, "read_bytes", cached_source):
                exec(code[str(source)], MODULE.__dict__)
        except BaseException as error:
            import_failure = {"type": type(error).__name__, "message": str(error)[:1000]}
        else:
            unittest.defaultTestLoader.loadTestsFromTestCase(FreshConfigurationGuards).run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in failures[:8]]
    passed = import_failure is None and result.wasSuccessful() and result.testsRun > 0
    print(json.dumps({"suite": "configuration-fresh-capture-guards", "status": "passed" if passed else "failed",
                      "testsRun": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "recorderSha256": SOURCE_HASHES[source.name], "suiteSha256": hashlib.sha256(suite_bytes).hexdigest(),
                      "sourceSha256": SOURCE_HASHES,
                      "realHttpRequests": 0, "realCaptureWrites": 0, "fixtures": "synthetic-memory-only",
                      "liveCaptureAcceptance": False, "importFailure": import_failure,
                      "failureSummaries": summaries, "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
