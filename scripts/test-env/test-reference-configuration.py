#!/usr/bin/env python3
"""Exercise configuration capture guards using only synthetic in-memory data.

Run through authorized root SSH with one configuration recorder source path.
Only the suite and four recorder sources are read before the effect fence.
Nested recorder imports use those cached bytes; no recorder is initialized and
no live credential, configuration, service, database, or capture is accessed.
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
from urllib.parse import parse_qs, quote, unquote, urlencode, urlsplit

sys.dont_write_bytecode = True
MODULE: types.ModuleType
SOURCE_LINES: dict[str, list[str]] = {}
SOURCE_HASHES: dict[str, str] = {}


@contextlib.contextmanager
def memory_tracebacks():
    """Keep source lookup in memory until every cleanup error is formatted."""
    with patch.object(linecache, "checkcache", lambda filename=None: None), \
            patch.object(linecache, "lazycache", lambda filename, module_globals: False), \
            patch.object(linecache, "getlines", lambda filename, module_globals=None:
                         list(SOURCE_LINES.get(str(filename), ()))):
        yield


class EffectFence(contextlib.ExitStack):
    """Reject external effects before any operator import or object creation."""

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

        # Python checks sys.modules before meta_path. The explicit recorder
        # MemoryLoader bypasses finders; every other dependency must already
        # be loaded before a finder can probe the real filesystem.
        self.enter_context(patch.object(sys, "meta_path", [RejectUncachedImports()]))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")), (_io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (signal, ("signal", "setitimer", "pthread_sigmask")), (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "stat", "lstat", "readlink", "scandir", "listdir", "walk", "write", "fsync",
                  "umask", "kill", "killpg", "system", "popen", "fork", "posix_spawn", "posix_spawnp", "execve", "execvp",
                  "replace", "rename", "mkdir", "makedirs", "remove", "unlink", "rmdir", "chmod", "chown", "link")),
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


class ConfigurationGuards(unittest.TestCase):
    def setUp(self) -> None:
        fence = EffectFence()
        fence.__enter__()
        self.addCleanup(fence.__exit__, None, None, None)
        self.output = io.StringIO()
        redirect = contextlib.redirect_stdout(self.output)
        redirect.__enter__()
        self.addCleanup(redirect.__exit__, None, None, None)

    def replace(self, owner: object, name: str, value: object) -> object:
        replacement = patch.object(owner, name, value)
        replacement.start()
        self.addCleanup(replacement.stop)
        return value

    def recorder(self) -> object:
        value = MODULE.Recorder.__new__(MODULE.Recorder)
        value.logins = {"control": {"AccessToken": "synthetic-owned-admin-token"},
                        "viewer": {"AccessToken": "synthetic-owned-viewer-token"}}
        value.login_attempts = {"control", "viewer"}
        value.forbidden_tokens = {"synthetic-stored-old-token"}
        value.account_credentials = {"admin": {"REFERENCE_USERNAME": "synthetic-admin", "REFERENCE_PASSWORD": "synthetic-admin-password"},
                                     "viewer": {"REFERENCE_USERNAME": "synthetic-viewer", "REFERENCE_PASSWORD": "synthetic-viewer-password"}}
        value.secrets = {"synthetic-known-secret"}
        value.read_ids, value.task_ids = {"15"}, {"synthetic-observed-task"}
        value._secret_signature, value._secret_regex = None, None
        value.configuration_baseline, value.configuration_observations, value.configuration_changes = {}, [], []
        value.configuration_complete = False
        value.wire_records, value.redaction_audits = {}, []
        value.checks = {}
        value.persistence_failures, value.cleanup_errors = [], []
        value.invalid_tokens, value.logged_out = set(), set()
        value.invalid_login_proofs, value.logout_statuses = {}, {}
        return value

    def install_wire(self, recorder: object, response: FakeResponse | list[FakeResponse]) -> types.SimpleNamespace:
        recorder.record_count = recorder.charged_bytes = recorder.total = recorder.incomplete_count = 0
        recorder.finishing, recorder.deadline = False, 1000.0
        recorder.mutations, recorder.cleanup_errors = [], []
        recorder.login_statuses = {}
        responses = response if isinstance(response, list) else [response]
        connections = [FakeConnection(item) for item in responses]
        fixture = types.SimpleNamespace(saved={}, connection=connections[0], connections=connections, opened=0)

        def save(path: Path, value: object) -> None:
            self.assertTrue(path.is_relative_to(MODULE.base.PRIVATE) or path.is_relative_to(MODULE.base.EXPORT))
            self.assertNotIn(path, fixture.saved)
            fixture.saved[path] = copy.deepcopy(value)

        def connect(host: str, port: int, *, timeout: float) -> FakeConnection:
            self.assertEqual((host, port, timeout), ("127.0.0.1", 18097, 5))
            self.assertLess(fixture.opened, len(fixture.connections), "Unexpected synthetic HTTP attempt")
            result = fixture.connections[fixture.opened]
            fixture.opened += 1
            return result

        self.replace(MODULE.base, "save", save)
        self.replace(MODULE.base, "digest", lambda path: hashlib.sha256(json.dumps(fixture.saved[path], sort_keys=True).encode()).hexdigest())
        self.replace(time, "monotonic", lambda: 100.0)
        self.replace(signal, "setitimer", lambda *args: None)
        self.replace(http.client, "HTTPConnection", connect)
        return fixture

    def test_fixed_capture_and_main_identity_pins_match_the_accepted_stage(self) -> None:
        self.assertEqual((MODULE.REFERENCE_PID, MODULE.MAIN_PID, MODULE.MAIN_START_TICKS), (3131777, 3570491, "24600634"))
        self.assertEqual(MODULE.MAIN_BINARY_SHA256, "2993870cce6f4e0ae2c3630b645985cab664ce5e5e18123fdc920fb241bb2abf")
        self.assertEqual(MODULE.OLD_RECORDS, 1965)
        self.assertEqual(MODULE.READ_SOURCE_SHA256, SOURCE_HASHES["reference-scheduled-tasks.py"])
        self.assertEqual(MODULE.DEVICE_SOURCE_SHA256, SOURCE_HASHES["reference-devices.py"])
        self.assertEqual(MODULE.BASE_SOURCE_SHA256, SOURCE_HASHES["reference-api-keys.py"])
        self.assertEqual(set(MODULE.NAMED_KEYS), {"encoding", "devices", "dlna"})
        self.assertEqual(set(MODULE.CONFIGURATION_ROUTES), {"total", "encoding", "devices", "dlna", "unknown"})
        self.assertEqual(MODULE.base.ROOT, Path("/opt/goby-test/exec-scratch/configuration-m5g"))
        self.assertTrue(MODULE.UNKNOWN_CONFIGURATION.startswith(MODULE.device.DEVICE_PREFIX))

    def test_main_process_pid_owner_start_ticks_and_executable_hash_are_required(self) -> None:
        state = {"pid": "3570491", "uid": 995, "ticks": "24600634", "digest": MODULE.MAIN_BINARY_SHA256}
        self.replace(subprocess, "check_output", lambda *args, **kwargs:
                     "MainPID=" + state["pid"] + "\nActiveState=active\nUser=goby\nWorkingDirectory=/var/lib/goby-test\nExecStart=/opt/goby-dev/goby\n")
        self.replace(Path, "stat", lambda path: types.SimpleNamespace(st_uid=state["uid"]))
        self.replace(Path, "read_text", lambda path: "3570491 (synthetic goby) " + " ".join(["S", *["0"] * 18, state["ticks"]]))
        self.replace(os, "readlink", lambda path: "/opt/goby-dev/goby" if Path(path).name == "exe" else "net:[synthetic-main]")
        self.replace(MODULE.base, "digest", lambda path: state["digest"])
        self.assertEqual(MODULE.main_identity()["binarySha256"], MODULE.MAIN_BINARY_SHA256)
        for field, replacement in (("pid", "3570492"), ("uid", 0), ("ticks", "24600635"), ("digest", "f" * 64)):
            with self.subTest(field=field):
                original = state[field]
                state[field] = replacement
                with self.assertRaisesRegex(RuntimeError, "protected M5f"):
                    MODULE.main_identity()
                state[field] = original

    def test_snapshot_requires_1965_pairs_240_originals_and_proven_removed_owned_sources(self) -> None:
        recorder = self.recorder()
        old_private = Path("/synthetic-configuration-history/private")
        old_raw = [old_private / "raw" / (str(index) + ".json") for index in range(1811)]
        old_export = [old_private.parent / "export" / path.name for path in old_raw]
        new_raw = [MODULE.PRIOR_CAPTURE / "private/raw" / (str(index) + ".json") for index in range(153)]
        audit_path = MODULE.PRIOR_CAPTURE / "private/raw/scheduled-tasks-fresh-m5f-audit.json"
        new_raw.append(audit_path)
        new_export = [MODULE.PRIOR_CAPTURE / "export" / path.name for path in new_raw]
        media = [Path("/synthetic-configuration-history/media") / (str(index) + ".mp4") for index in range(240)]
        owned = {str(MODULE.REMOVED_SOURCE / "batch-a" / (str(index) + ".mp4")): "a" * 64 for index in range(512)}
        prior = {"records": {str(path): "a" * 64 for path in [*old_raw, *old_export]},
                 "media": {str(path): "a" * 64 for path in media},
                 "historicalRemovedPaths": [MODULE.HISTORICAL_REMOVED_MARKER], "ownedSourceFiles": owned,
                 "ownedSourceSnapshot": {"root": str(MODULE.REMOVED_SOURCE), "fileCount": 512,
                                          "files": {path: {"sha256": digest} for path, digest in owned.items()}}}
        records = {MODULE.PRIOR_CAPTURE / "private/baseline.json": prior,
                   audit_path: {"cleanupPassed": True, "captureFailureType": None, "httpAttempts": 153, "incompleteHTTP": 0},
                   MODULE.ORIGINAL_DEVICE_FINAL: {"response": {"status": 200, "body": {"Items": [{"Id": str(index)} for index in range(22)]}}}}
        existing = set()
        self.replace(MODULE.base, "private_file", lambda path: None)
        self.replace(Path, "read_text", lambda path: json.dumps(records[path]))
        self.replace(Path, "glob", lambda path, pattern: iter(new_raw if path.name == "raw" else new_export))
        self.replace(Path, "rglob", lambda path, pattern: iter(old_raw if path == old_private else new_raw))
        self.replace(Path, "resolve", lambda path, **kwargs: path)
        self.replace(Path, "exists", lambda path: path in existing)
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "is_file", lambda path: True)
        self.replace(Path, "lstat", lambda path: types.SimpleNamespace(st_mode=stat.S_IFREG | 0o600))
        self.replace(Path, "stat", lambda path: types.SimpleNamespace(st_size=1))
        digest = Mock(return_value="a" * 64)
        self.replace(MODULE.base, "digest", digest)
        snapshot = recorder.snapshot()
        self.assertEqual((len(snapshot["records"]), len(snapshot["media"])), (3930, 240))
        self.assertEqual(len(snapshot["retiredOwnedSources"]["files"]), 512)
        self.assertTrue(snapshot["retiredOwnedSources"]["removedAfterCompletedStudy"])
        self.assertEqual(len(recorder.previous_device_ids), 22)
        name, value = prior["records"].popitem()
        with self.assertRaisesRegex(RuntimeError, "1965 preceding raw/export pairs"):
            recorder.snapshot()
        prior["records"][name] = value
        name, value = prior["media"].popitem()
        with self.assertRaisesRegex(RuntimeError, "240 original source files"):
            recorder.snapshot()
        prior["media"][name] = value
        first_owned = next(iter(owned))
        prior["ownedSourceSnapshot"]["files"][first_owned]["sha256"] = "f" * 64
        with self.assertRaisesRegex(RuntimeError, "retired owned source provenance differs"):
            recorder.snapshot()
        prior["ownedSourceSnapshot"]["files"][first_owned]["sha256"] = "a" * 64
        existing.add(MODULE.REMOVED_SOURCE)
        with self.assertRaisesRegex(RuntimeError, "disposable task fixture has not been removed"):
            recorder.snapshot()
        existing.clear()
        digest.side_effect = lambda path: "f" * 64 if path == media[0] else "a" * 64
        with self.assertRaisesRegex(RuntimeError, "retained prior evidence or source file changed"):
            recorder.snapshot()

    def test_configuration_reads_are_bounded_to_total_and_documented_named_paths(self) -> None:
        recorder = self.recorder()
        for token in ("", "synthetic-owned-admin-token", "synthetic-owned-viewer-token"):
            for route in ("/emby/System/Configuration", "/emby/System/Configuration/encoding", "/emby/System/Configuration/devices",
                          "/emby/System/Configuration/dlna", "/emby/System/Configuration/" + MODULE.UNKNOWN_CONFIGURATION):
                with self.subTest(token=token, route=route):
                    recorder.authorize("GET", route, token, MODULE.MISSING, "")
        for route in ("/emby/System/Configuration/unobserved", "/emby/System/Configuration?api_key=synthetic-known-secret",
                      "/emby/System/Configuration/encoding?arbitrary=true", "/emby/System/Configuration//encoding",
                      "/emby/System/Configuration/%2e%2e/encoding", "/emby/System/Configuration/encoding#fragment",
                      "https://example.invalid/emby/System/Configuration", "/emby/Auth/Keys", "/emby/Items"):
            with self.subTest(route=route), self.assertRaises((RuntimeError, ValueError)):
                recorder.authorize("GET", route, "synthetic-owned-admin-token", MODULE.MISSING, "")
        for token in ("synthetic-stored-old-token", "unacknowledged-token"):
            with self.subTest(token=token), self.assertRaises(RuntimeError):
                recorder.authorize("GET", "/emby/System/Configuration", token, MODULE.MISSING, "")
        for body, identity in ((None, ""), ({}, ""), (MODULE.MISSING, "control")):
            with self.subTest(body_present=body is not MODULE.MISSING, identity=identity), self.assertRaises(RuntimeError):
                recorder.authorize("GET", "/emby/System/Configuration", "synthetic-owned-admin-token", body, identity)

    def test_configuration_task_device_user_key_and_media_mutations_are_denied(self) -> None:
        recorder = self.recorder()
        routes = ("/emby/System/Configuration", "/emby/System/Configuration/encoding", "/emby/System/Configuration/devices", "/emby/System/Configuration/dlna",
                  "/emby/ScheduledTasks/Running/synthetic-observed-task", "/emby/ScheduledTasks/synthetic-observed-task/Triggers",
                  "/emby/Devices?Id=15", "/emby/Devices/Options?Id=15", "/emby/Users/synthetic-user/Policy",
                  "/emby/Users/New", "/emby/Auth/Keys", "/emby/Library/Refresh", "/emby/Items/synthetic-item/Refresh",
                  "/emby/Sessions/Playing", "/emby/System/Restart", "/emby/System/Shutdown")
        for method in ("POST", "PUT", "PATCH", "DELETE"):
            for route in routes:
                with self.subTest(method=method, route=route), self.assertRaises(RuntimeError):
                    recorder.authorize(method, route, "synthetic-owned-admin-token", {"Enabled": False}, "")

    def test_only_two_reserved_fresh_logins_and_owned_logouts_can_mutate(self) -> None:
        recorder = self.recorder()
        self.assertEqual(set(MODULE.device.IDENTITIES), {"control", "viewer"})
        for identity, account in (("control", "admin"), ("viewer", "viewer")):
            credentials = recorder.account_credentials[account]
            body = {"Username": credentials["REFERENCE_USERNAME"], "Pw": credentials["REFERENCE_PASSWORD"]}
            original_login = recorder.logins.pop(identity)
            recorder.login_attempts.discard(identity)
            recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", body, identity)
            with self.assertRaises(RuntimeError):
                recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", {**body, "Pw": "different-password"}, identity)
            recorder.login_attempts.add(identity)
            with self.assertRaises(RuntimeError):
                recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", body, identity)
            recorder.logins[identity] = original_login
        with self.assertRaises(RuntimeError):
            recorder.authorize("POST", "/emby/Users/AuthenticateByName", "", {"Username": "other", "Pw": "other"}, "unreserved")
        for token in ("synthetic-owned-admin-token", "synthetic-owned-viewer-token"):
            recorder.authorize("POST", "/emby/Sessions/Logout", token, MODULE.MISSING, "")
        for token in ("", "synthetic-stored-old-token", "unacknowledged-token"):
            with self.subTest(token=token), self.assertRaises(RuntimeError):
                recorder.authorize("POST", "/emby/Sessions/Logout", token, MODULE.MISSING, "")

    @staticmethod
    def configuration_record(body: object, *, name: str = "encoding") -> dict:
        return {"request": {"method": "GET", "path": "/emby/System/Configuration/" + name, "headers": {}},
                "response": {"status": 200, "bodyType": "json", "headers": {}, "body": body},
                "configurationName": name}

    def test_nested_configuration_credentials_are_collected_and_public_request_name_survives(self) -> None:
        recorder = self.recorder()
        secrets_by_field = {"ClientSecret": "synthetic-client-secret", "Password": "synthetic-provider-password",
                            "AccessToken": "synthetic-provider-token", "ApiKey": "synthetic-provider-api-key",
                            "PrivateKey": "synthetic-private-signing-key", "Key": "synthetic-generic-configuration-key"}
        record = self.configuration_record({"Enabled": True, "Threads": 4, "Providers": [{"Credentials": secrets_by_field}]})
        original = copy.deepcopy(record)
        recorder.collect_secrets(record)
        cleaned = recorder.sanitize(record)
        serialized = json.dumps(cleaned)
        for secret in secrets_by_field.values():
            self.assertNotIn(secret, serialized)
        self.assertEqual(cleaned["request"]["path"], "/emby/System/Configuration/encoding")
        self.assertEqual(cleaned["configurationName"], "encoding")
        self.assertEqual(cleaned["response"]["body"]["Enabled"], True)
        self.assertEqual(cleaned["response"]["body"]["Threads"], 4)
        self.assertEqual(record, original)

    def test_configuration_body_cannot_claim_task_identifier_exemptions_even_under_empty_keys(self) -> None:
        recorder = self.recorder()
        task = {"Id": "synthetic-task", "Name": "Synthetic task", "State": "Idle", "Key": "synthetic-task-shaped-secret",
                "LastExecutionResult": {"Id": "synthetic-task", "Name": "Synthetic task", "Status": "Completed",
                                         "Key": "synthetic-result-shaped-secret"}}
        forged_record = {"request": {"method": "GET", "path": "/emby/ScheduledTasks", "headers": {}},
                         "response": {"status": 200, "bodyType": "json", "headers": {}, "body": [copy.deepcopy(task)]}}
        record = self.configuration_record({"TaskShape": task, "": forged_record,
                                            "Key": "synthetic-root-config-secret"})
        recorder.collect_secrets(record)
        cleaned = recorder.sanitize(record)
        serialized = json.dumps(cleaned)
        for secret in ("synthetic-task-shaped-secret", "synthetic-result-shaped-secret", "synthetic-root-config-secret"):
            self.assertNotIn(secret, serialized)
        self.assertEqual(cleaned["response"]["body"]["TaskShape"]["Key"], "[REDACTED_SECRET]")
        self.assertEqual(cleaned["response"]["body"][""]["response"]["body"][0]["Key"], "[REDACTED_SECRET]")

    def test_url_credentials_query_secrets_headers_and_encoded_known_values_are_removed(self) -> None:
        recorder = self.recorder()
        known = "synthetic/known+configuration=secret"
        recorder.secrets.add(known)
        encoded = quote(known, safe="")
        fully_encoded = "".join("%" + format(byte, "02x") for byte in known.encode())
        record = self.configuration_record({
            "RemoteUrl": "https://synthetic-url-user:synthetic-url-password@example.invalid/path?access_token=synthetic-url-token&safe=keep",
            "CallbackUrl": "https://example.invalid/callback?returnTo=" + encoded,
            "FullEncodedUrl": "https://example.invalid/opaque/" + fully_encoded,
            "Headers": [["Authorization", "Bearer synthetic-header-token"], ["X-Api-Key", "synthetic-header-api-key"], ["X-Public", "keep"]],
            "diagnostic-" + known: {"Value": "before " + known + " after"},
        })
        record["request"]["headers"] = {"X-Emby-Token": "synthetic-request-token"}
        record["response"]["headers"] = [["Set-Cookie", "session=synthetic-response-cookie; HttpOnly"]]
        original = copy.deepcopy(record)
        recorder.collect_secrets(record)
        cleaned = recorder.sanitize(record)
        serialized = json.dumps(cleaned)
        for secret in (known, encoded, fully_encoded, "synthetic-url-user", "synthetic-url-password", "synthetic-url-token",
                       "synthetic-header-token", "synthetic-header-api-key", "synthetic-request-token", "synthetic-response-cookie"):
            self.assertNotIn(secret, serialized)
        self.assertIn("example.invalid/path", serialized)
        self.assertIn("safe=[REDACTED_QUERY]", serialized)
        self.assertIn(["X-Public", "keep"], cleaned["response"]["body"]["Headers"])
        self.assertEqual(record, original)

    def test_sanitized_dictionary_and_header_key_collisions_are_rejected(self) -> None:
        first, second = "synthetic/first-secret", "synthetic/second-secret"
        for value in ({first: "one", second: "two"}, {first: "one", quote(first, safe=""): "two"},
                      {"headers": {first: "one", second: "two"}}):
            recorder = self.recorder()
            recorder.secrets.update({first, second})
            with self.subTest(value=value), self.assertRaisesRegex(RuntimeError, "[Cc]olli|duplicate"):
                recorder.sanitize(value)

    def test_unseeded_relative_protocol_relative_and_encoded_url_credentials_are_removed(self) -> None:
        absolute = "https://synthetic-encoded-user:synthetic-encoded-password@example.invalid/path?code=unknown-encoded-code#private-encoded-fragment"
        cases = (
            ("/callback?code=unknown-relative-code#private-relative-fragment", ("unknown-relative-code", "private-relative-fragment")),
            ("//synthetic-proxy-user:synthetic-proxy-password@example.invalid/path?code=unknown-protocol-code#private-protocol-fragment",
             ("synthetic-proxy-user", "synthetic-proxy-password", "unknown-protocol-code", "private-protocol-fragment")),
            (quote(absolute, safe=""), ("synthetic-encoded-user", "synthetic-encoded-password", "unknown-encoded-code", "private-encoded-fragment")),
            (quote(quote(quote(absolute, safe=""), safe=""), safe=""),
             ("synthetic-encoded-user", "synthetic-encoded-password", "unknown-encoded-code", "private-encoded-fragment")),
        )
        for url, credentials in cases:
            with self.subTest(url=url):
                recorder = self.recorder()
                recorder.secrets.clear()
                record = self.configuration_record({"Url": url})
                recorder.collect_secrets(record)
                exported = json.dumps(recorder.sanitize(record))
                for _ in range(3):
                    for credential in credentials:
                        self.assertNotIn(credential, exported)
                    exported = unquote(exported)

    def test_header_dict_pairs_and_inherited_credentials_redact_unseeded_echoes(self) -> None:
        headers = {"Proxy-Authorization": "Basic unknown-proxy-header-secret",
                   "X-Emby-Authorization": 'Emby Token="unknown-emby-header-secret"'}
        for representation in (headers, [list(pair) for pair in headers.items()]):
            with self.subTest(representation=type(representation).__name__):
                recorder = self.recorder()
                recorder.secrets.clear()
                record = self.configuration_record({"Headers": representation, "Diagnostic": "echo: " + " / ".join(headers.values())})
                recorder.collect_secrets(record)
                cleaned = recorder.sanitize(record)
                serialized = json.dumps(cleaned)
                self.assertNotIn("unknown-proxy-header-secret", serialized)
                self.assertNotIn("unknown-emby-header-secret", serialized)
                exported_headers = cleaned["response"]["body"]["Headers"]
                self.assertEqual(dict(exported_headers), {name: "[REDACTED_SECRET]" for name in headers})
        recorder = self.recorder()
        recorder.secrets.clear()
        record = self.configuration_record({"Credentials": {"Headers": [["X-Public", "unknown-inner-secret"]]},
                                            "Diagnostic": "unknown-inner-secret"})
        recorder.collect_secrets(record)
        self.assertIn("unknown-inner-secret", recorder.secrets)
        self.assertNotIn("unknown-inner-secret", json.dumps(recorder.sanitize(record)))

    def test_documented_password_flags_remain_boolean_except_inside_sensitive_containers(self) -> None:
        recorder = self.recorder()
        flags = {"HasPassword": True, "HasConfiguredPassword": False, "EnableLocalPassword": True}
        record = self.configuration_record({**flags, "Credentials": copy.deepcopy(flags),
                                            "NonBoolean": {"HasPassword": "synthetic-flag-shaped-secret"}})
        recorder.collect_secrets(record)
        cleaned = recorder.sanitize(record)["response"]["body"]
        for field, value in flags.items():
            self.assertIs(cleaned[field], value)
            self.assertEqual(cleaned["Credentials"][field], "[REDACTED_SECRET]")
        self.assertEqual(cleaned["NonBoolean"]["HasPassword"], "[REDACTED_SECRET]")

    def test_configuration_comparison_distinguishes_absent_null_and_scalar_types(self) -> None:
        recorder = self.recorder()
        recorder.request = Mock(return_value=(200, {"Enabled": True, "Threads": 1}))
        recorder.capture_configuration("baseline", "encoding", "synthetic-owned-admin-token", baseline=True)
        recorder.request.return_value = (200, {"Enabled": 1, "Threads": 1.0, "Optional": None})
        recorder.capture_configuration("final", "encoding", "synthetic-owned-admin-token", final=True)
        self.assertEqual(recorder.configuration_changes[0]["changedTopLevelProperties"], ["Enabled", "Optional", "Threads"])
        self.assertTrue(MODULE.same_value({"a": [None, 1, True]}, {"a": [None, 1, True]}))
        self.assertFalse(MODULE.same_value({"a": None}, {}))
        self.assertFalse(MODULE.same_value(True, 1))
        self.assertFalse(MODULE.same_value(1, 1.0))

    def test_non_utf8_configuration_stays_in_private_wire_and_cannot_be_exported_as_base64(self) -> None:
        recorder = self.recorder()
        body = b"\xffsynthetic-known-secret"
        fixture = self.install_wire(recorder, FakeResponse(body))
        with self.assertRaisesRegex(RuntimeError, "complete bounded UTF-8 capture"):
            recorder.request("binary-configuration", "GET", "/emby/System/Configuration", token="synthetic-owned-admin-token")
        wire_path = MODULE.base.PRIVATE / "wire" / (MODULE.base.PREFIX + "binary-configuration.json")
        self.assertEqual(base64.b64decode(fixture.saved[wire_path]["responseBodyBase64"]), body)
        raw_path = MODULE.base.RAW / (MODULE.base.PREFIX + "binary-configuration.json")
        self.assertEqual(fixture.saved[raw_path]["response"]["bodyType"], "private-binary")
        self.assertIsNone(fixture.saved[raw_path]["response"]["body"])
        exported = fixture.saved[MODULE.base.EXPORT / raw_path.name]
        self.assertNotIn("synthetic-known-secret", exported)
        self.assertNotIn(base64.b64encode(body).decode(), exported)
        self.assertEqual(recorder.incomplete_count, 0)
        self.assertFalse(fixture.saved[raw_path]["observation"]["bodyExportable"])
        self.assertTrue(fixture.connection.closed)

    def test_incomplete_login_acknowledges_new_token_before_raising_capture_failure(self) -> None:
        recorder = self.recorder()
        recorder.logins.pop("control")
        recorder.login_attempts.remove("control")
        token = "synthetic-issued-during-incomplete-login"
        body = json.dumps({"AccessToken": token, "User": {"Id": "synthetic-user", "Name": "synthetic-admin"}}).encode()
        fixture = self.install_wire(recorder, FakeResponse(body, declared_length=len(body) + 1))
        with self.assertRaisesRegex(RuntimeError, "complete bounded UTF-8 capture"):
            recorder.request("partial-login", "POST", "/emby/Users/AuthenticateByName", identity="control",
                             body={"Username": "synthetic-admin", "Pw": "synthetic-admin-password"})
        self.assertEqual(recorder.logins["control"]["AccessToken"], token)
        self.assertTrue(recorder.mutations[0]["acknowledgedLogin"])
        self.assertEqual(fixture.saved[MODULE.base.PRIVATE / "control-login-response.json"]["AccessToken"], token)
        exported = fixture.saved[MODULE.base.EXPORT / (MODULE.base.PREFIX + "partial-login.json")]
        self.assertNotIn(token, exported)
        self.assertEqual(recorder.incomplete_count, 1)
        self.assertTrue(fixture.connection.closed)

    def test_login_acknowledgement_survives_failure_of_every_post_response_write(self) -> None:
        recorder = self.recorder()
        recorder.logins.pop("control")
        recorder.login_attempts.remove("control")
        token = "synthetic-issued-before-storage-failure"
        body = json.dumps({"AccessToken": token, "User": {"Id": "synthetic-user", "Name": "synthetic-admin"}}).encode()
        fixture = self.install_wire(recorder, FakeResponse(body))

        def fail_after_intent(path: Path, value: object) -> None:
            if path.parent == MODULE.base.PRIVATE / "mutations" and path.name == "001-intent.json":
                fixture.saved[path] = copy.deepcopy(value)
                return
            self.assertEqual(recorder.logins["control"]["AccessToken"], token,
                             "A received credential must be acknowledged before any fallible response persistence")
            raise OSError("Synthetic response storage failure")

        self.replace(MODULE.base, "save", fail_after_intent)
        with self.assertRaisesRegex(OSError, "response storage failure"):
            recorder.request("storage-failed-login", "POST", "/emby/Users/AuthenticateByName", identity="control",
                             body={"Username": "synthetic-admin", "Pw": "synthetic-admin-password"})
        self.assertEqual(recorder.logins["control"]["AccessToken"], token)
        self.assertTrue(recorder.mutations[0]["acknowledgedLogin"])
        self.assertIn(token, recorder.secrets)
        self.assertEqual(fixture.opened, 1)
        self.assertTrue(fixture.connection.closed)

    def test_owned_logout_and_invalidity_proofs_continue_when_all_persistence_fails(self) -> None:
        recorder = self.recorder()
        fixture = self.install_wire(recorder, [FakeResponse(b"", status=204), FakeResponse(b"", status=401),
                                               FakeResponse(b"", status=204), FakeResponse(b"", status=401)])
        recorder.finishing = True

        def fail_write(path: Path, value: object) -> None:
            raise OSError("Synthetic total capture storage failure")

        def fail_prior_check() -> None:
            raise RuntimeError("Synthetic earlier audit failure")

        self.replace(MODULE.base, "save", fail_write)
        self.assertIsNone(recorder.cleanup_step("earlier-audit", fail_prior_check))
        recorder.logout("viewer")
        recorder.logout("control")
        self.assertEqual(recorder.logged_out, {"viewer", "control"})
        self.assertEqual({name: proof["status"] for name, proof in recorder.invalid_login_proofs.items()}, {"viewer": 401, "control": 401})
        self.assertEqual(recorder.logout_statuses, {"viewer": 204, "control": 204})
        self.assertTrue(recorder.persistence_failures and recorder.cleanup_errors)
        self.assertTrue(any(item["stage"] == "cleanup-diagnostic-earlier-audit" for item in recorder.persistence_failures))
        self.assertEqual(fixture.saved, {})
        self.assertEqual(fixture.opened, 4)
        self.assertTrue(all(connection.closed for connection in fixture.connections))
        self.assertEqual([(connection.requests[0][0], connection.requests[0][1]) for connection in fixture.connections],
                         [("POST", "/emby/Sessions/Logout"), ("GET", "/emby/Sessions"),
                          ("POST", "/emby/Sessions/Logout"), ("GET", "/emby/Sessions")])


def main() -> None:
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "configuration-read-only-guards", "result": "blocked",
                          "reason": "Authorized root SSH and one configuration recorder source are required"}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    paths = [source, *[source.with_name(name) for name in
                      ("reference-scheduled-tasks.py", "reference-devices.py", "reference-api-keys.py")]]
    sources = {str(path): path.read_bytes() for path in paths}
    suite_bytes = Path(__file__).read_bytes()
    for filename, content in sources.items():
        SOURCE_LINES[filename] = content.decode().splitlines(keepends=True)
        SOURCE_HASHES[Path(filename).name] = hashlib.sha256(content).hexdigest()
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    code = {filename: compile(content, filename, "exec") for filename, content in sources.items()}

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

    def specification(name: str, filename: object, **kwargs: object) -> object:
        if kwargs:
            raise AssertionError("Unexpected recorder import options")
        return importlib.util.spec_from_loader(name, MemoryLoader(str(filename)), origin=str(filename))

    global MODULE
    MODULE = types.ModuleType("synthetic_configuration_recorder")
    MODULE.__file__ = str(source)
    result = unittest.TestResult()
    import_failure = None
    with memory_tracebacks():
        try:
            with EffectFence(), patch.object(importlib.util, "spec_from_file_location", specification), \
                    patch.object(os, "environ", {"GOBY_DEVICE_REFERENCE_RUN": "2"}):
                exec(code[str(source)], MODULE.__dict__)
        except BaseException as error:
            import_failure = {"type": type(error).__name__, "message": str(error)[:1000]}
        else:
            unittest.defaultTestLoader.loadTestsFromTestCase(ConfigurationGuards).run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in failures[:8]]
    passed = import_failure is None and result.wasSuccessful() and result.testsRun > 0
    print(json.dumps({"suite": "configuration-read-only-guards", "result": "passed" if passed else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "recorderSha256": hashlib.sha256(sources[str(source)]).hexdigest(),
                      "testSha256": hashlib.sha256(suite_bytes).hexdigest(),
                      "sourceSha256": {Path(filename).name: hashlib.sha256(content).hexdigest() for filename, content in sources.items()},
                      "httpRequests": 0, "captureWrites": 0, "fixtures": "synthetic-memory-only",
                      "importFailure": import_failure,
                      "failureSummaries": summaries, "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
