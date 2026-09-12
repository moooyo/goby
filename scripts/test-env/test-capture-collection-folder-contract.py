#!/usr/bin/env python3
"""Exercise CollectionFolder capture guards with memory fixtures only.

The only project sources read are this guard and the explicitly pinned
controller. Execution is restricted to authorized root SSH on test-env. No live
HTTP, database, service, filesystem publication, or historical implementation
is exercised. The JSON result is a memory-guard receipt, not capture evidence.
"""

from __future__ import annotations

import argparse
import ast
import builtins
from collections import Counter
import contextlib
import copy
import datetime
from decimal import Decimal
import fcntl
import hashlib
import http.client
import importlib
import io
import json
import linecache
import ntpath
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import unittest
from unittest.mock import Mock, patch
import urllib.parse
import uuid


sys.dont_write_bytecode = True
TARGET = None
SOURCE = b""
ACTIVE_FENCES = []
SAFE_IMPORTS = frozenset({
    "__future__", "argparse", "collections", "contextlib", "copy", "datetime",
    "decimal", "fcntl", "hashlib", "http", "http.client", "io", "json", "os",
    "pathlib", "re", "secrets", "signal", "socket", "stat", "subprocess", "sys",
    "time", "types", "urllib", "urllib.parse", "uuid",
})
# Python 3.13 pathlib imports this cached standard-library module while building
# path constants. It is allowed only during definition loading, not at runtime.
CACHED_DEFINITION_IMPORTS = SAFE_IMPORTS | {"ntpath"}
PRELOADED_REGEX_MODULES = types.MappingProxyType({"re": re})
# Python 3.13's JSON scanner requests this dotted name with an empty fromlist;
# the cached import returns the already loaded top-level json module.
PRELOADED_JSON_IMPORTS = types.MappingProxyType({"json.decoder": json})
ORIGINAL_IMPORT = builtins.__import__


def deny_effect(label):
    for fence in ACTIVE_FENCES:
        fence.violations.append(label)
    raise AssertionError("An external effect escaped its memory adapter: " + label)


def denied(*_args, **_kwargs):
    deny_effect("external API")


def audit_effect(event, arguments):
    if not ACTIVE_FENCES:
        return
    fence = ACTIVE_FENCES[-1]
    if event == "exec" and arguments and arguments[0] is fence.allowed_code:
        return
    if event in {"open", "import", "compile", "exec", "builtins.input", "builtins.breakpoint"} or event.startswith(
            ("os.", "subprocess.", "socket.", "ctypes.", "fcntl.", "mmap.", "shutil.", "tempfile.", "pty.")):
        deny_effect("audit:" + event)


def definition_import(name, globals=None, locals=None, fromlist=(), level=0):
    if level or name not in CACHED_DEFINITION_IMPORTS or name not in sys.modules:
        deny_effect("unapproved definition import: " + name)
    return ORIGINAL_IMPORT(name, globals, locals, fromlist, level)


class MemoryImportError(ImportError):
    """A requested module is outside the exact pure memory adapters."""


def resolve_regex_import(name, globals=None, locals=None, fromlist=(), level=0):
    empty_fromlist = type(fromlist) is list and not fromlist
    if type(name) is not str or type(level) is not int or level != 0 or not empty_fromlist:
        raise MemoryImportError("The runtime import is outside the exact cached module adapters.")
    if name in PRELOADED_REGEX_MODULES:
        return PRELOADED_REGEX_MODULES[name]
    if name == "json.decoder" and isinstance(globals, dict) and globals.get("__name__") == "json.decoder":
        return PRELOADED_JSON_IMPORTS[name]
    raise MemoryImportError("The runtime import is outside the exact cached module adapters.")


def memory_runtime_import(name, globals=None, locals=None, fromlist=(), level=0):
    try:
        return resolve_regex_import(name, globals, locals, fromlist, level)
    except MemoryImportError:
        deny_effect("unapproved runtime import: " + (name if type(name) is str else "non-string module"))


class EffectFence(contextlib.ExitStack):
    """Fail even when the controller catches an attempted external effect."""

    def __init__(self, allowed_code=None):
        super().__init__()
        self.allowed_code = allowed_code
        self.violations = []

    def __enter__(self):
        super().__enter__()
        ACTIVE_FENCES.append(self)
        surfaces = (
            (builtins, ("open", "input", "breakpoint")),
            (io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "socketpair", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (fcntl, ("flock", "lockf", "fcntl", "ioctl")),
            (signal, ("signal", "setitimer", "alarm", "pthread_kill")),
            (time, ("sleep",)),
            (os, ("open", "close", "fdopen", "read", "write", "stat", "lstat", "fstat", "readlink",
                  "listdir", "scandir", "mkdir", "makedirs", "remove", "unlink", "rename", "replace",
                  "rmdir", "chmod", "chown", "link", "symlink", "truncate", "kill", "killpg",
                  "system", "popen", "fsync", "fdatasync", "fchmod", "fchown", "ftruncate",
                  "fork", "execv", "execve", "posix_spawn", "umask", "urandom")),
            (Path, ("open", "read_bytes", "read_text", "write_bytes", "write_text", "stat", "lstat",
                    "resolve", "exists", "is_file", "is_dir", "is_symlink", "mkdir", "touch", "unlink",
                    "rename", "replace", "rmdir", "chmod", "iterdir", "glob", "rglob")),
        )
        for owner, names in surfaces:
            for name in names:
                if hasattr(owner, name):
                    self.enter_context(patch.object(owner, name, denied))
        self.enter_context(patch.object(builtins, "__import__", definition_import if self.allowed_code else memory_runtime_import))
        if self.allowed_code is None:
            for name in ("compile", "exec", "eval"):
                self.enter_context(patch.object(builtins, name, denied))
            for name in ("import_module", "reload"):
                self.enter_context(patch.object(importlib, name, denied))
        self.enter_context(patch.object(linecache, "checkcache", lambda *_args, **_kwargs: None))
        self.enter_context(patch.object(linecache, "getlines", lambda *_args, **_kwargs: []))
        return self

    def __exit__(self, *arguments):
        try:
            result = super().__exit__(*arguments)
        finally:
            ACTIVE_FENCES.pop()
        if self.violations:
            raise AssertionError("External effects were attempted: " + ", ".join(self.violations))
        return result


class GuardTestCase(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.object(TARGET, "Path", MemoryPath))
        for name, value in list(vars(TARGET).items()):
            if isinstance(value, PurePosixPath):
                self.enterContext(patch.object(TARGET, name, MemoryPath(str(value))))
        self.enterContext(EffectFence())

    def reject(self, callback):
        with self.assertRaises(TARGET.CaptureError):
            callback()


TABLES = (
    "schema_migrations", "server_settings", "users", "sessions", "libraries", "library_roots", "items",
    "scan_jobs", "catalog_entities", "item_entities", "item_images", "user_item_data", "play_sessions",
    "item_subtitles", "encoding_jobs", "client_playback_references", "item_metadata_state", "application_keys",
    "application_key_clients", "devices", "application_key_devices", "task_definitions", "task_triggers",
    "task_runs", "task_run_requests", "task_run_children", "task_occurrences", "managed_settings",
    "activity_entries", "user_settings", "theme_owner_ids", "theme_reserved_paths", "item_theme_resources",
    "extra_reserved_paths", "item_extra_resources",
)
TOKEN = "A" * 43
TOKEN_SHA256 = hashlib.sha256(TOKEN.encode()).hexdigest()
SESSION_ID = "ba" * 16
DEVICE = {
    "device_id": "collection-folder-guard-fresh-device", "device_name": "CollectionFolder guard device",
    "client_name": "CollectionFolder contract capture", "client_version": "1.0",
}


def at(seconds=0):
    return (datetime.datetime(2026, 9, 12, 6, tzinfo=datetime.timezone.utc) +
            datetime.timedelta(seconds=seconds)).isoformat().replace("+00:00", "Z")


def credential(target="candidate"):
    expected = TARGET.TARGETS[target]
    return {"user_id": expected["user_id"], "username": expected["username"], "password": "memory-only-password"}


def proof_fixture():
    return dict(DEVICE, session_id=SESSION_ID, user_id=TARGET.TARGETS["candidate"]["user_id"],
                server_id=TARGET.TARGETS["candidate"]["server_id"], token_sha256=TOKEN_SHA256)


def login_payload(target="candidate"):
    expected = TARGET.TARGETS[target]
    return {
        "AccessToken": TOKEN, "ServerId": expected["server_id"],
        "User": {"Id": expected["user_id"], "Name": expected["username"],
                 "Policy": {"IsAdministrator": False, "IsDisabled": False}},
        "SessionInfo": {"Id": SESSION_ID, "UserId": expected["user_id"], "DeviceId": DEVICE["device_id"],
                        "DeviceName": DEVICE["device_name"], "Client": DEVICE["client_name"],
                        "ApplicationVersion": DEVICE["client_version"]},
    }


def session_row(identifier, token_sha256, created, device=None, registry_id=None):
    identity = device or dict(DEVICE, device_id="old-device", device_name="Old device")
    return {
        "id": identifier, "user_id": TARGET.TARGETS["candidate"]["user_id"],
        "token_hash": "\\x" + token_sha256, "kind": "emby", "client_name": identity["client_name"],
        "device_id": identity["device_id"], "device_name": identity["device_name"],
        "client_version": identity["client_version"], "created_at": at(created),
        "expires_at": at(created + 30 * 86400), "last_seen_at": at(created), "revoked_at": None,
        "client_capabilities": {}, "device_registry_id": registry_id,
    }


def device_row(identifier, created, device=None):
    identity = device or dict(DEVICE, device_id="old-device", device_name="Old device")
    return {
        "id": identifier, "reported_device_id": identity["device_id"], "reported_name": identity["device_name"],
        "custom_name": None, "app_name": identity["client_name"], "app_version": identity["client_version"],
        "last_user_id": TARGET.TARGETS["candidate"]["user_id"], "created_at": at(created),
        "last_seen_at": at(created), "ip_address": "127.0.0.1", "revision": 1, "deleted_at": None,
    }


def audit_row(identifier, action, session_id, seconds):
    return {
        "id": identifier, "created_at": at(seconds), "action": action, "severity": "Info", "source": "emby",
        "actor_kind": "user", "actor_id": TARGET.TARGETS["candidate"]["user_id"],
        "actor_credential_id": session_id, "resource_kind": "session", "resource_id": session_id,
        "request_id": "", "revision": 0, "affected_count": 1, "state": "", "changed_fields": [],
    }


def baseline_snapshot():
    tables = {name: [] for name in TABLES}
    old_session = session_row("01" * 16, hashlib.sha256(b"old-token").hexdigest(), -86400, registry_id=7)
    old_session["revoked_at"] = at(-3600)
    tables["sessions"] = [old_session]
    tables["devices"] = [device_row(7, -86400)]
    tables["activity_entries"] = [audit_row(10, "session.revoked", old_session["id"], -3600)]
    tables["users"] = [{"id": TARGET.TARGETS["candidate"]["user_id"],
                        "name": TARGET.TARGETS["candidate"]["username"], "is_administrator": False}]
    tables["server_settings"] = [{"key": "server_id", "value": TARGET.TARGETS["candidate"]["server_id"]}]
    tables["libraries"] = [{"id": TARGET.TARGETS["candidate"]["library_id"], "name": "Original Movies"}]
    tables["library_roots"] = [{"id": "02" * 16, "library_id": TARGET.TARGETS["candidate"]["library_id"],
                                "path": "/synthetic/Movies"}]
    tables["items"] = [{"id": "03" * 16, "name": "Original movie", "is_folder": False,
                        "local_metadata": {"Sparse": None, "LargeInteger": 9007199254740993}}]
    columns = {name: list(rows[0]) if rows else ["id", "fixture"] for name, rows in tables.items()}
    return {
        "schema": 27, "private_files": {"credential_sha256": "7" * 64},
        "media": {"original": {"sha256": "8" * 64, "size": 123}},
        "closed_roots": {"attempt-v1": {"device": 3, "inode": 42, "tree_sha256": "9" * 64},
                         "protocol-capture-old": {"device": 3, "inode": 43, "tree_sha256": "a" * 64}},
        "database": {
            "tables": tables, "metadata": {"captured_at": at(), "database": "goby_client_m3e",
                                             "server_version_num": 170006, "columns": columns},
            "catalog": {"schema": 27, "full_catalog": True}, "unsupported": False,
            "sequences": {"devices_id_seq": {"last_value": 7, "is_called": True},
                          "activity_entries_id_seq": {"last_value": 10, "is_called": True},
                          "catalog_entities_id_seq": {"last_value": 4, "is_called": True}},
        },
    }


def ledger_fixture(closed=True):
    before = baseline_snapshot()
    after = copy.deepcopy(before)
    tables = after["database"]["tables"]
    session = session_row(SESSION_ID, TOKEN_SHA256, 2, DEVICE, 8)
    session["last_seen_at"] = at(3)
    session["revoked_at"] = at(4) if closed else None
    tables["sessions"].append(session)
    device = device_row(8, 1, DEVICE)
    device["last_seen_at"] = at(3)
    tables["devices"].append(device)
    tables["activity_entries"].append(audit_row(11, "session.login", SESSION_ID, 2))
    if closed:
        tables["activity_entries"].append(audit_row(12, "session.revoked", SESSION_ID, 4))
    after["database"]["metadata"]["captured_at"] = at(8)
    after["database"]["sequences"].update(
        devices_id_seq={"last_value": 8, "is_called": True},
        activity_entries_id_seq={"last_value": 12 if closed else 11, "is_called": True},
    )
    return before, after, proof_fixture()


class ExactJSONGuards(GuardTestCase):
    def test_complete_row_encoding_preserves_large_numbers_nulls_and_boolean_types(self):
        value = {"integer": 9007199254740993, "fraction": Decimal("1.000000000000000001"),
                 "nullable": None, "empty": [], "flag": False}
        raw = TARGET.canonical(value)
        self.assertEqual(raw, b'{"empty":[],"flag":false,"fraction":1.000000000000000001,"integer":9007199254740993,"nullable":null}')
        restored = TARGET.decode(raw)
        self.assertEqual(restored, value)
        self.assertIs(type(restored["integer"]), int)
        self.assertIs(type(restored["fraction"]), Decimal)
        self.assertFalse(TARGET.same({"value": True}, {"value": 1}))
        self.assertFalse(TARGET.same({"value": None}, {"value": []}))

    def test_duplicate_members_and_nonfinite_numbers_cannot_become_evidence(self):
        for raw in (b'{"x":1,"x":2}', b'{"x":NaN}', b'{"x":Infinity}', b'{"x":-Infinity}', b'{"x":}'):
            with self.subTest(raw=raw), self.assertRaises((ValueError, TARGET.CaptureError)):
                TARGET.decode(raw)
        for value in (Decimal("NaN"), Decimal("Infinity"), Decimal("-Infinity")):
            self.reject(lambda: TARGET.canonical({"value": value}))


class LoginAndRedactionGuards(GuardTestCase):
    def validate(self, payload, **kwargs):
        return TARGET.validate_login(payload, TARGET.TARGETS["candidate"]["server_id"], credential(), DEVICE, **kwargs)

    def test_complete_fresh_ordinary_login_has_exact_identity_proof(self):
        payload = login_payload()
        original = copy.deepcopy(payload)
        self.assertEqual(self.validate(payload), proof_fixture())
        self.assertEqual(payload, original)

    def test_login_rejects_each_foreign_or_missing_identity_component(self):
        mutations = (
            ("AccessToken",), ("ServerId",), ("User", "Id"), ("User", "Name"),
            ("SessionInfo", "Id"), ("SessionInfo", "UserId"), ("SessionInfo", "DeviceId"),
            ("SessionInfo", "DeviceName"), ("SessionInfo", "Client"), ("SessionInfo", "ApplicationVersion"),
        )
        for path in mutations:
            for missing in (False, True):
                with self.subTest(path=path, missing=missing):
                    payload = login_payload()
                    owner = payload
                    for part in path[:-1]:
                        owner = owner[part]
                    if missing:
                        owner.pop(path[-1])
                    else:
                        owner[path[-1]] = "foreign/value" if path != ("AccessToken",) else ""
                    self.reject(lambda: self.validate(payload))

    def test_login_requires_explicit_ordinary_role_and_enabled_account(self):
        for field, values in (("IsAdministrator", (True, 0, None, "false")),
                              ("IsDisabled", (True, 0, None, "false"))):
            for value in values:
                with self.subTest(field=field, value=value):
                    payload = login_payload()
                    payload["User"]["Policy"][field] = value
                    self.reject(lambda: self.validate(payload))
        for field in ("IsAdministrator", "IsDisabled"):
            payload = login_payload()
            del payload["User"]["Policy"][field]
            self.reject(lambda: self.validate(payload))

    def test_login_never_reuses_old_tokens_or_reported_devices(self):
        self.reject(lambda: self.validate(login_payload(), forbidden_tokens=(TOKEN_SHA256,)))
        self.reject(lambda: self.validate(login_payload(), forbidden_devices=(DEVICE["device_id"],)))
        self.assertEqual(self.validate(login_payload(), forbidden_tokens=("old-token",),
                                       forbidden_devices=("old-device",)), proof_fixture())

    def test_redaction_covers_sensitive_keys_nested_strings_and_overlapping_secrets(self):
        secret = "private-secret-long"
        value = {"Password": secret, "AccessToken": TOKEN, "Authorization": "Bearer " + TOKEN,
                 "Cookie": "private-cookie", "ApiKey": "private-key", "Nested": [
                     {"ordinary": "prefix " + secret + " " + TOKEN + " suffix", "Pw": secret}],
                 "empty": [], "false": False, "null": None, "number": 9007199254740993}
        original = copy.deepcopy(value)
        redacted = TARGET.sanitize(value, {secret, "private-secret", TOKEN, "private-cookie", "private-key"})
        rendered = json.dumps(redacted)
        for forbidden in (secret, "private-secret", TOKEN, "private-cookie", "private-key"):
            self.assertNotIn(forbidden, rendered)
        self.assertEqual(value, original)
        for name in ("empty", "false", "null", "number"):
            self.assertEqual(redacted[name], value[name])


class RequestAndBudgetGuards(GuardTestCase):
    def test_only_the_two_fixed_proxy_ports_are_targets(self):
        self.assertEqual(set(TARGET.TARGETS), {"candidate", "reference"})
        self.assertEqual({name: value["port"] for name, value in TARGET.TARGETS.items()},
                         {"candidate": 18198, "reference": 18197})
        for target in ("", "main", "reference-direct", "http://127.0.0.1:18097", "18198"):
            self.assertFalse(TARGET.allowed_request(target, "GET", "/emby/System/Info/Public"))

    def test_request_allowlist_accepts_only_exact_owned_public_reads_and_login(self):
        for target in ("candidate", "reference"):
            expected = TARGET.TARGETS[target]
            user = "/emby/Users/" + expected["user_id"]
            library = expected["library_id"] if target == "candidate" else "93"
            requests = (("GET", "/emby/System/Info/Public"), ("POST", "/emby/Users/AuthenticateByName"),
                        ("GET", user), ("GET", user + "/Views"),
                        ("GET", user + "/Items/" + library), ("GET", user + "/Items/" + library + TARGET.VARIANT))
            for method, path in requests:
                with self.subTest(target=target, method=method, path=path):
                    self.assertTrue(TARGET.allowed_request(target, method, path, library_id=library))
                    for other in {"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} - {method}:
                        self.assertFalse(TARGET.allowed_request(target, other, path, library_id=library))
                    for suffix in ("/", "?api_key=private", "#fragment", "?StartIndex=0", "&EnableImages=true"):
                        self.assertFalse(TARGET.allowed_request(target, method, path + suffix, library_id=library))

    def test_cleanup_allowlist_contains_only_logout_and_protected_same_token_probe(self):
        for target in ("candidate", "reference"):
            for method, path in (("POST", "/emby/Sessions/Logout"), ("GET", "/emby/System/Info")):
                self.assertTrue(TARGET.allowed_request(target, method, path, cleanup=True))
                self.assertFalse(TARGET.allowed_request(target, method, path))
            for method, path in (("GET", "/emby/System/Info/Public"), ("POST", "/emby/Users/AuthenticateByName"),
                                 ("DELETE", "/emby/Sessions/Logout"), ("POST", "/emby/System/Info")):
                self.assertFalse(TARGET.allowed_request(target, method, path, cleanup=True))

    def test_exact_request_labels_cannot_be_repurposed_to_retry_login_or_other_reads(self):
        for target in ("candidate", "reference"):
            user = "/emby/Users/" + TARGET.TARGETS[target]["user_id"]
            library = TARGET.TARGETS[target]["library_id"] or "93"
            expected = {
                "public-info": ("GET", "/emby/System/Info/Public", False),
                "login": ("POST", "/emby/Users/AuthenticateByName", False),
                "user-before": ("GET", user, False), "user-after": ("GET", user, False),
                "views": ("GET", user + "/Views", False),
                "detail-default": ("GET", user + "/Items/" + library, False),
                "detail-switches": ("GET", user + "/Items/" + library + TARGET.VARIANT, False),
                "logout": ("POST", "/emby/Sessions/Logout", True),
                "exact-token": ("GET", "/emby/System/Info", True),
            }
            for label, route in expected.items():
                method, path, cleanup = route
                self.assertTrue(TARGET.allowed_label(target, label, method, path, cleanup, library))
                self.assertFalse(TARGET.allowed_label(target, label + "-retry", method, path, cleanup, library))
                for other_label, other_route in expected.items():
                    if route != other_route:
                        self.assertFalse(TARGET.allowed_label(target, other_label, method, path, cleanup, library))

    def test_foreign_users_libraries_encoded_paths_and_mutation_routes_are_rejected(self):
        target = "candidate"
        user = "/emby/Users/" + TARGET.TARGETS[target]["user_id"]
        library = TARGET.TARGETS[target]["library_id"]
        paths = (
            "/emby/Users/foreign/Views", "/emby/Users/foreign/Items/" + library,
            user + "/Items/foreign", user + "/Items/" + library + "/SpecialFeatures",
            "/emby/Items/" + library, "/emby/Items", "/emby/Library/VirtualFolders", "/emby/Devices",
            "/emby/Users", "/emby/System/Configuration", "/emby/Library/Refresh", "/emby/Items/" + library + "/Refresh",
            "/emby/Users/%2e%2e/Views", "//emby/System/Info/Public", "http://127.0.0.1:18097/emby/System/Info/Public",
        )
        for path in paths:
            for method in ("GET", "POST", "DELETE"):
                with self.subTest(method=method, path=path):
                    self.assertFalse(TARGET.allowed_request(target, method, path, library_id=library))
        reference_user = "/emby/Users/" + TARGET.TARGETS["reference"]["user_id"]
        for invalid in (None, "", "../93", "93?bad=1", "93/other", "93%2fother"):
            self.assertFalse(TARGET.allowed_request("reference", "GET", reference_user + "/Items/93", library_id=invalid))
        self.assertFalse(TARGET.allowed_request("candidate", "GET", user + "/Items/foreign", library_id="foreign"))

    def test_research_requests_stop_at_sixteen_and_reserve_four_exact_cleanup_calls(self):
        budget = TARGET.Budget(clock=lambda: 100.0)
        for number in range(1, 17):
            self.assertEqual(budget.charge_request(), number)
        self.reject(lambda: budget.charge_request())
        self.assertEqual(budget.requests, 16)
        for number in range(17, 21):
            self.assertEqual(budget.charge_request(cleanup=True), number)
        self.reject(lambda: budget.charge_request(cleanup=True))
        self.assertEqual(budget.requests, 20)

    def test_body_and_aggregate_byte_limits_include_exact_boundary_and_empty_body(self):
        budget = TARGET.Budget(clock=lambda: 100.0)
        budget.charge_response(0)
        for _ in range(8):
            budget.charge_response(1 << 20)
        self.assertEqual(budget.total, 8 << 20)
        self.reject(lambda: budget.charge_response(1))
        self.assertEqual(budget.total, 8 << 20)
        for size in (-1, (1 << 20) + 1, True, "1024", None, 1.5):
            with self.subTest(size=size):
                empty = TARGET.Budget(clock=lambda: 100.0)
                self.reject(lambda: empty.charge_response(size))
                self.assertEqual(empty.total, 0)

    def test_http_request_body_aggregate_and_deadline_limits_cannot_be_enlarged(self):
        for option, value in (("max_requests", 21), ("max_response", (1 << 20) + 1),
                              ("max_total", (8 << 20) + 1), ("deadline_seconds", 13)):
            with self.subTest(option=option):
                self.reject(lambda: TARGET.Budget(**{option: value}, clock=lambda: 100.0))
        budget = TARGET.Budget(clock=lambda: 100.0)
        self.assertEqual((budget.max_requests, budget.max_response, budget.max_total, budget.deadline_seconds),
                         (20, 1 << 20, 8 << 20, 12))

    def test_views_requires_one_complete_matching_movies_collection_folder(self):
        for target in ("candidate", "reference"):
            expected = TARGET.TARGETS[target]
            library = expected["library_id"] or "93"
            row = {"Id": library, "Name": expected["library_name"], "ServerId": expected["server_id"],
                   "Type": "CollectionFolder", "IsFolder": True, "CollectionType": "movies"}
            body = {"Items": [row], "TotalRecordCount": 1}
            self.assertEqual(TARGET.select_library(target, body), library)
            for field in ("Id", "Name", "ServerId", "Type", "IsFolder", "CollectionType"):
                with self.subTest(target=target, field=field):
                    changed = copy.deepcopy(body)
                    changed["Items"][0][field] = "foreign/value"
                    self.reject(lambda: TARGET.select_library(target, changed))
            for bad in ({"Items": [row], "TotalRecordCount": 2}, {"Items": [row], "TotalRecordCount": True},
                        {"Items": [row, row], "TotalRecordCount": 2}, {"Items": [], "TotalRecordCount": 0}):
                self.reject(lambda: TARGET.select_library(target, bad))


class LedgerGuards(GuardTestCase):
    def test_closed_candidate_has_only_one_session_one_device_and_two_audits(self):
        before, after, proof = ledger_fixture()
        original = copy.deepcopy((before, after, proof))
        result = TARGET.validate_ledger(before, after, proof)
        self.assertEqual(result, {"new_sessions": 1, "new_devices": 1, "new_audits": 2,
                                  "old_rows_sequences_private_preserved": True, "owned_sessions_closed": True})
        self.assertEqual((before, after, proof), original)

    def test_unclosed_checkpoint_has_only_one_login_audit_and_cannot_pass_closed(self):
        before, after, proof = ledger_fixture(closed=False)
        self.assertIsNot(TARGET.validate_ledger(before, after, proof, closed=False), False)
        self.reject(lambda: TARGET.validate_ledger(before, after, proof, closed=True))

    def test_no_login_proof_permits_only_the_complete_unchanged_snapshot(self):
        before = baseline_snapshot()
        after = copy.deepcopy(before)
        after["database"]["metadata"]["captured_at"] = at(8)
        self.assertIsNot(TARGET.validate_ledger(before, after, None), False)
        _, changed, _ = ledger_fixture()
        self.reject(lambda: TARGET.validate_ledger(before, changed, None))

    def test_old_complete_rows_cannot_change_even_in_permitted_tables(self):
        before, _, _ = ledger_fixture()
        for table, rows in before["database"]["tables"].items():
            for column in rows[0] if rows else ():
                with self.subTest(table=table, column=column):
                    initial, after, proof = ledger_fixture()
                    after["database"]["tables"][table][0][column] = {"unexpected": "change"}
                    self.reject(lambda: TARGET.validate_ledger(initial, after, proof))

    def test_every_unapproved_table_rejects_new_rows(self):
        for table in set(TABLES) - {"sessions", "devices", "activity_entries"}:
            with self.subTest(table=table):
                before, after, proof = ledger_fixture()
                rows = after["database"]["tables"][table]
                rows.append(copy.deepcopy(rows[0]) if rows else {"id": "foreign", "fixture": "new"})
                self.reject(lambda: TARGET.validate_ledger(before, after, proof))

    def test_snapshot_catalog_column_inventory_and_old_closed_roots_are_invariant(self):
        mutations = (
            lambda value: value.update(schema=28),
            lambda value: value["private_files"].update(credential_sha256="0" * 64),
            lambda value: value["media"]["original"].update(size=124),
            lambda value: value["closed_roots"]["attempt-v1"].update(inode=99),
            lambda value: value["closed_roots"]["protocol-capture-old"].update(tree_sha256="0" * 64),
            lambda value: value["database"]["catalog"].update(full_catalog=False),
            lambda value: value["database"].update(unsupported=True),
            lambda value: value["database"]["metadata"].update(database="foreign_database"),
            lambda value: value["database"]["metadata"].update(captured_at=at(-1)),
            lambda value: value["database"]["metadata"]["columns"]["sessions"].append("hidden_column"),
            lambda value: value["database"]["tables"].pop("theme_reserved_paths"),
            lambda value: value["database"]["tables"]["sessions"][0].pop("expires_at"),
        )
        for index, mutate in enumerate(mutations):
            with self.subTest(mutation=index):
                before, after, proof = ledger_fixture()
                mutate(after)
                self.reject(lambda: TARGET.validate_ledger(before, after, proof))

    def test_extra_missing_or_duplicate_owned_rows_are_rejected(self):
        for table in ("sessions", "devices", "activity_entries"):
            for action in ("duplicate", "remove"):
                with self.subTest(table=table, action=action):
                    before, after, proof = ledger_fixture()
                    rows = after["database"]["tables"][table]
                    if action == "duplicate":
                        rows.append(copy.deepcopy(rows[-1]))
                    else:
                        rows.pop()
                    self.reject(lambda: TARGET.validate_ledger(before, after, proof))

    def test_session_and_device_ownership_cannot_be_reassigned(self):
        for table, columns in (
            ("sessions", ("id", "token_hash", "user_id", "kind", "device_id", "device_name", "client_name",
                          "client_version", "device_registry_id", "client_capabilities")),
            ("devices", ("id", "reported_device_id", "reported_name", "app_name", "app_version", "last_user_id",
                         "custom_name", "deleted_at", "revision", "ip_address")),
        ):
            for column in columns:
                with self.subTest(table=table, column=column):
                    before, after, proof = ledger_fixture()
                    after["database"]["tables"][table][-1][column] = "foreign-value"
                    self.reject(lambda: TARGET.validate_ledger(before, after, proof))

    def test_old_token_or_device_reuse_fails_despite_otherwise_valid_allowance(self):
        for reuse in ("token", "device"):
            with self.subTest(reuse=reuse):
                before, after, proof = ledger_fixture()
                if reuse == "token":
                    old_hash = before["database"]["tables"]["sessions"][0]["token_hash"]
                    proof["token_sha256"] = old_hash[2:]
                    after["database"]["tables"]["sessions"][-1]["token_hash"] = old_hash
                else:
                    proof["device_id"] = "old-device"
                    after["database"]["tables"]["sessions"][-1]["device_id"] = "old-device"
                    after["database"]["tables"]["devices"][-1]["reported_device_id"] = "old-device"
                self.reject(lambda: TARGET.validate_ledger(before, after, proof))

    def test_audits_require_exact_actor_action_resource_and_scalar_types(self):
        for column in ("id", "action", "severity", "source", "actor_kind", "actor_id", "actor_credential_id",
                       "resource_kind", "resource_id", "request_id", "revision", "affected_count", "state", "changed_fields"):
            with self.subTest(column=column):
                before, after, proof = ledger_fixture()
                after["database"]["tables"]["activity_entries"][-1][column] = "foreign-value"
                self.reject(lambda: TARGET.validate_ledger(before, after, proof))
        for column in ("id", "revision", "affected_count"):
            before, after, proof = ledger_fixture()
            after["database"]["tables"]["activity_entries"][-1][column] = True
            self.reject(lambda: TARGET.validate_ledger(before, after, proof))

    def test_sequence_advances_are_exact_contiguous_and_never_reset(self):
        for sequence in ("devices_id_seq", "activity_entries_id_seq", "catalog_entities_id_seq"):
            for change in (-1, 1):
                with self.subTest(sequence=sequence, change=change):
                    before, after, proof = ledger_fixture()
                    after["database"]["sequences"][sequence]["last_value"] += change
                    self.reject(lambda: TARGET.validate_ledger(before, after, proof))
            for field, value in (("is_called", 1), ("is_called", False), ("last_value", True), ("last_value", "8")):
                before, after, proof = ledger_fixture()
                after["database"]["sequences"][sequence][field] = value
                self.reject(lambda: TARGET.validate_ledger(before, after, proof))
        before, after, proof = ledger_fixture()
        after["database"]["sequences"]["foreign_seq"] = {"last_value": 1, "is_called": True}
        self.reject(lambda: TARGET.validate_ledger(before, after, proof))


class MemoryHTTP:
    """Record one physical exchange without a socket or a real timer."""

    def __init__(self, *, status=200, raw=b"{}", content_type="application/json", declared_length=None,
                 read_error=None, alarm_on_read=False):
        self.status, self.raw, self.content_type = status, raw, content_type
        self.declared_length = str(len(raw)) if declared_length is None else declared_length
        self.read_error, self.alarm_on_read = read_error, alarm_on_read
        self.connections, self.sent, self.read_limits, self.timers, self.handlers = [], [], [], [], []
        self.closed = 0

    def timer(self, which, seconds):
        self.timers.append((which, seconds))

    def signal(self, which, handler):
        self.handlers.append((which, handler))

    def connect(self, host, port, timeout):
        if self.timers != [(signal.ITIMER_REAL, 12)]:
            raise AssertionError("The absolute timer was not armed before opening the connection.")
        self.connections.append((host, port, timeout))
        return self

    def request(self, method, path, body=None, headers=None):
        self.sent.append((method, path, body, copy.deepcopy(headers)))

    def getresponse(self):
        return self

    def getheader(self, name):
        return {"Content-Type": self.content_type, "Content-Length": self.declared_length}.get(name)

    def read(self, limit):
        self.read_limits.append(limit)
        if self.alarm_on_read:
            self.handlers[0][1](signal.SIGALRM, None)
        if self.read_error is not None:
            raise self.read_error
        return self.raw

    def close(self):
        self.closed += 1


def memory_run(clock=None):
    run = TARGET.Run(types.SimpleNamespace(script_sha256="f" * 64, check_only=False))
    run.budget = TARGET.Budget(clock=clock or (lambda: 100.0))
    run.before = baseline_snapshot()
    run.saved = {}
    run.check = Mock()
    run.cleanup_identity = Mock()

    def save(name, value):
        if name in run.saved:
            raise AssertionError("A memory evidence label was reused.")
        run.saved[name] = copy.deepcopy(value)
        return {"memory_name": name}

    run.save = save
    actor = TARGET.Actor(run, "candidate", credential(), copy.deepcopy(DEVICE))
    actor.token = TOKEN
    actor.proof = proof_fixture()
    actor.owned_proof = proof_fixture()
    actor.library_id = TARGET.TARGETS["candidate"]["library_id"]
    run.actors = [actor]
    return run, actor


class TransportAndCleanupGuards(GuardTestCase):
    def bind_http(self, wire):
        self.enterContext(patch.object(TARGET.http.client, "HTTPConnection", wire.connect))
        self.enterContext(patch.object(TARGET.signal, "getsignal", lambda _which: signal.SIG_DFL))
        self.enterContext(patch.object(TARGET.signal, "signal", wire.signal))
        self.enterContext(patch.object(TARGET.signal, "setitimer", wire.timer))

    def exchange(self, wire, *, clock=None, cleanup=False):
        self.bind_http(wire)
        run, actor = memory_run(clock)
        path = "/emby/System/Info" if cleanup else "/emby/Users/" + credential()["user_id"] + "/Items/" + actor.library_id
        label = "exact-token" if cleanup else "detail-default"
        return run, actor, run.request(actor, label, "GET", path, cleanup=cleanup)

    def test_transport_uses_exact_port_identity_headers_read_bound_and_absolute_timer(self):
        wire = MemoryHTTP(raw=b'{"Items":[]}')
        run, actor, result = self.exchange(wire)
        self.assertTrue(result["complete"])
        self.assertEqual(result["body"], {"Items": []})
        self.assertEqual(wire.connections, [("127.0.0.1", 18198, 8)])
        self.assertEqual(wire.read_limits, [(1 << 20) + 1])
        self.assertEqual(wire.timers, [(signal.ITIMER_REAL, 12), (signal.ITIMER_REAL, 0)])
        self.assertEqual(wire.closed, 1)
        self.assertEqual(wire.sent[0][3]["X-Emby-Token"], TOKEN)
        self.assertIn(DEVICE["device_id"], wire.sent[0][3]["Authorization"])
        self.assertEqual(run.saved["candidate-detail-default-raw.bin"], wire.raw)
        self.assertEqual(result["sha256"], hashlib.sha256(wire.raw).hexdigest())
        self.assertEqual(run.budget.requests, 1)

    def test_absolute_deadline_rejects_slow_complete_body_and_timer_interrupt(self):
        times = iter((100.0, 112.001, 112.001))
        wire = MemoryHTTP()
        _, _, result = self.exchange(wire, clock=lambda: next(times))
        self.assertFalse(result["complete"])
        self.assertEqual(result["failure_type"], "CaptureError")
        self.assertEqual(wire.closed, 1)
        self.assertEqual(wire.timers[-1], (signal.ITIMER_REAL, 0))
        interrupt = MemoryHTTP(alarm_on_read=True)
        _, _, result = self.exchange(interrupt)
        self.assertFalse(result["complete"])
        self.assertEqual(result["failure_type"], "CaptureError")
        self.assertEqual(interrupt.closed, 1)

    def test_response_rejects_excessive_declared_actual_aggregate_and_incomplete_lengths(self):
        cases = (
            MemoryHTTP(declared_length=str((1 << 20) + 1)),
            MemoryHTTP(raw=b"x" * ((1 << 20) + 1), declared_length="0"),
            MemoryHTTP(raw=b"{}", declared_length="3"),
            MemoryHTTP(raw=b"{}", declared_length="-1"),
            MemoryHTTP(raw=b'{"broken":}'),
        )
        for index, wire in enumerate(cases):
            with self.subTest(case=index):
                run, _, result = self.exchange(wire)
                self.assertFalse(result["complete"])
                self.assertEqual(run.budget.requests, 1)
                self.assertEqual(wire.closed, 1)
        wire = MemoryHTTP()
        self.bind_http(wire)
        run, actor = memory_run()
        run.budget.total = 8 << 20
        result = run.request(actor, "user-before", "GET", "/emby/Users/" + credential()["user_id"])
        self.assertFalse(result["complete"])
        self.assertEqual(run.budget.total, 8 << 20)

    def test_non_json_wire_body_is_retained_but_cannot_pass_a_required_dto_read(self):
        wire = MemoryHTTP(raw=b"<html>Unavailable</html>", content_type="text/html")
        self.bind_http(wire)
        run, actor = memory_run()
        self.reject(lambda: run.get(actor, "user-before", "/emby/Users/" + credential()["user_id"]))
        result = run.saved["candidate-user-before-response.json"]
        self.assertTrue(result["complete"])
        self.assertEqual(result["content_type"], "text/html")
        self.assertEqual(run.saved["candidate-user-before-raw.bin"], wire.raw)

    def test_cleanup_exact401_accepts_a_complete_plain_text_unauthorized_body(self):
        wire = MemoryHTTP(status=401, raw=b"Unauthorized", content_type="text/plain")
        _, _, result = self.exchange(wire, cleanup=True)
        self.assertTrue(result["complete"])
        self.assertEqual(result["status"], 401)

    def test_route_rejection_and_duplicate_labels_never_open_a_connection(self):
        wire = MemoryHTTP()
        self.bind_http(wire)
        run, actor = memory_run()
        self.reject(lambda: run.request(actor, "user-before", "GET", "/emby/Users"))
        self.assertEqual(wire.connections, [])
        self.assertEqual(run.budget.requests, 0)
        result = run.request(actor, "user-before", "GET", "/emby/Users/" + credential()["user_id"])
        self.assertTrue(result["complete"])
        self.reject(lambda: run.request(actor, "user-before", "GET", "/emby/Users/" + credential()["user_id"]))
        self.assertEqual(len(wire.connections), 1)
        self.assertEqual(run.budget.requests, 1)

    def cleanup_actor(self, replies, *, owned=True, full_contract=True, token=TOKEN):
        run, actor = memory_run()
        actor.token = token
        actor.proof = proof_fixture() if full_contract else None
        actor.owned_proof = proof_fixture() if owned else None
        pending, calls = list(replies), []

        def request(owner, label, method, path, **kwargs):
            calls.append((owner, owner.token, label, method, path, kwargs))
            if not pending:
                raise AssertionError("A cleanup request was replayed.")
            reply = pending.pop(0)
            if isinstance(reply, Exception):
                raise reply
            return copy.deepcopy(reply)

        run.request = request
        return actor, calls

    def assert_exact_cleanup_pair(self, actor, calls):
        self.assertEqual([(entry[2], entry[3], entry[4], entry[5]) for entry in calls], [
            ("logout", "POST", "/emby/Sessions/Logout", {"cleanup": True}),
            ("exact-token", "GET", "/emby/System/Info", {"cleanup": True}),
        ])
        self.assertTrue(all(entry[0] is actor and entry[1] == TOKEN for entry in calls))
        self.assertTrue(actor.logout_sent)
        self.assertTrue(actor.exact_sent)
        actor.cleanup()
        self.assertEqual(len(calls), 2)

    def test_cleanup_success_requires_logout204_and_the_same_token401_exactly_once(self):
        actor, calls = self.cleanup_actor([{"complete": True, "status": 204}, {"complete": True, "status": 401}])
        actor.cleanup()
        self.assertTrue(actor.closed)
        self.assert_exact_cleanup_pair(actor, calls)

    def test_lost_logout_acknowledgement_still_probes_once_and_cannot_retry(self):
        for failure in (TARGET.CaptureError("Synthetic lost acknowledgement."),
                        {"complete": False, "status": None}, {"complete": True, "status": 500}):
            with self.subTest(failure=type(failure).__name__):
                actor, calls = self.cleanup_actor([failure, {"complete": True, "status": 401}])
                self.reject(actor.cleanup)
                self.assertFalse(actor.closed)
                self.assert_exact_cleanup_pair(actor, calls)

    def test_wrong_or_incomplete_same_token_response_cannot_claim_cleanup(self):
        for reply in ({"complete": True, "status": 200}, {"complete": True, "status": 403},
                      {"complete": False, "status": 401}, TARGET.CaptureError("Synthetic probe loss.")):
            actor, calls = self.cleanup_actor([{"complete": True, "status": 204}, reply])
            self.reject(actor.cleanup)
            self.assertFalse(actor.closed)
            self.assert_exact_cleanup_pair(actor, calls)

    def test_cleanup_uses_owned_identity_when_full_login_contract_failed(self):
        actor, calls = self.cleanup_actor([{"complete": True, "status": 204}, {"complete": True, "status": 401}],
                                          owned=True, full_contract=False)
        actor.cleanup()
        self.assertTrue(actor.closed)
        self.assert_exact_cleanup_pair(actor, calls)

    def test_no_owned_proof_never_sends_an_unproven_token(self):
        actor, calls = self.cleanup_actor([], owned=False, full_contract=False)
        self.reject(actor.cleanup)
        self.assertEqual(calls, [])
        actor.token = None
        actor.cleanup()
        self.assertEqual(calls, [])

    def test_check_only_calls_load_and_never_creates_evidence_or_sends_http(self):
        run = TARGET.Run(types.SimpleNamespace(check_only=True))
        run.load = Mock()
        run.prepare = run.request = run.save = run.execute = denied
        result = run.check_only()
        run.load.assert_called_once_with()
        self.assertEqual(result["status"], "preflight_passed")
        self.assertEqual(result["http_requests"], 0)
        self.assertIs(result["evidence_created"], False)
        self.assertEqual(result["before_authority_sha256"], TARGET.BASELINE_SHA)
        self.assertEqual(run.actors, [])


class ActorLoginGuards(GuardTestCase):
    def actor(self, reply):
        run, actor = memory_run()
        actor.token = actor.proof = actor.owned_proof = None
        calls = []
        pending = [reply, {"complete": True, "status": 204}, {"complete": True, "status": 401}]

        def request(owner, label, method, path, **kwargs):
            calls.append((owner, owner.token, label, method, path, kwargs))
            if not pending:
                raise AssertionError("An actor replayed an exchange.")
            return copy.deepcopy(pending.pop(0))

        run.request = request
        return run, actor, calls

    def test_fresh_login_proves_ownership_and_contract_once_then_cleans_same_token(self):
        run, actor, calls = self.actor({"complete": True, "status": 200, "body": login_payload()})
        actor.login()
        self.assertEqual(actor.owned_proof, proof_fixture())
        self.assertEqual(actor.proof, proof_fixture())
        self.assertEqual(calls[0][2:5], ("login", "POST", "/emby/Users/AuthenticateByName"))
        self.assertEqual(calls[0][5], {"body": {"Username": credential()["username"], "Pw": credential()["password"]}})
        self.assertIn(TOKEN, run.secret_values)
        self.assertIn("candidate-owned-login-proof.json", run.saved)
        self.assertIn("candidate-login-proof.json", run.saved)
        self.reject(actor.login)
        self.assertEqual(len(calls), 1)
        actor.cleanup()
        self.assertTrue(actor.closed)
        self.assertEqual([entry[1] for entry in calls[1:]], [TOKEN, TOKEN])

    def test_complete_owned_login_with_bad_role_keeps_cleanup_proof_and_failed_contract(self):
        for defect in ("role", "disabled", "device-name", "client", "version"):
            with self.subTest(defect=defect):
                payload = login_payload()
                if defect == "role":
                    payload["User"]["Policy"]["IsAdministrator"] = True
                elif defect == "disabled":
                    payload["User"]["Policy"]["IsDisabled"] = True
                else:
                    field = {"device-name": "DeviceName", "client": "Client", "version": "ApplicationVersion"}[defect]
                    payload["SessionInfo"][field] = "unexpected-contract-value"
                run, actor, calls = self.actor({"complete": True, "status": 200, "body": payload})
                self.reject(actor.login)
                self.assertEqual(actor.owned_proof, proof_fixture())
                self.assertIsNone(actor.proof)
                self.assertNotIn("candidate-login-proof.json", run.saved)
                actor.cleanup()
                self.assertTrue(actor.closed)
                self.assertEqual([entry[1] for entry in calls[1:]], [TOKEN, TOKEN])

    def test_incomplete_or_unparsed_login_never_constructs_ownership_from_a_fragment(self):
        for reply in ({"complete": False, "status": 200, "body": login_payload()},
                      {"complete": False, "status": 200, "body": None},
                      {"complete": True, "status": 200, "body": '{"AccessToken":"' + TOKEN + '"'}):
            with self.subTest(complete=reply["complete"], body_type=type(reply["body"]).__name__):
                _, actor, calls = self.actor(reply)
                self.reject(actor.login)
                self.assertIsNone(actor.owned_proof)
                self.assertIsNone(actor.proof)
                self.assertIsNone(actor.token)
                actor.cleanup()
                self.assertEqual(len(calls), 1)

    def test_foreign_identity_or_old_token_never_acquires_cleanup_authority(self):
        for defect in ("server", "user", "session-user", "device", "old-token", "old-device"):
            with self.subTest(defect=defect):
                payload = login_payload()
                if defect == "server":
                    payload["ServerId"] = "foreign"
                elif defect == "user":
                    payload["User"]["Id"] = "foreign"
                elif defect == "session-user":
                    payload["SessionInfo"]["UserId"] = "foreign"
                elif defect == "device":
                    payload["SessionInfo"]["DeviceId"] = "foreign"
                run, actor, calls = self.actor({"complete": True, "status": 200, "body": payload})
                if defect == "old-token":
                    run.before["database"]["tables"]["sessions"][0]["token_hash"] = "\\x" + TOKEN_SHA256
                elif defect == "old-device":
                    run.before["database"]["tables"]["devices"][0]["reported_device_id"] = DEVICE["device_id"]
                self.reject(actor.login)
                self.assertIsNone(actor.owned_proof)
                self.assertIsNone(actor.proof)
                self.reject(actor.cleanup)
                self.assertEqual(len(calls), 1)


class OwnershipPublicationGuards(GuardTestCase):
    def bind_http_sequence(self, wires):
        pending, selected = list(wires), []
        active = [None]

        def get_signal(_which):
            if not pending:
                raise AssertionError("A physical exchange was replayed.")
            active[0] = pending.pop(0)
            selected.append(active[0])
            return signal.SIG_DFL

        self.enterContext(patch.object(TARGET.signal, "getsignal", get_signal))
        self.enterContext(patch.object(TARGET.signal, "signal", lambda *args: active[0].signal(*args)))
        self.enterContext(patch.object(TARGET.signal, "setitimer", lambda *args: active[0].timer(*args)))
        self.enterContext(patch.object(TARGET.http.client, "HTTPConnection", lambda *args, **kwargs:
                                      active[0].connect(*args, **kwargs)))
        return selected

    def test_login_journal_failure_retains_owned_cleanup_and_failed_capture(self):
        for suffix in ("raw.bin", "response.json"):
            with self.subTest(failed_artifact=suffix):
                run, actor = memory_run()
                actor.token = actor.proof = actor.owned_proof = None
                login_wire = MemoryHTTP(raw=TARGET.canonical(login_payload()))
                logout_wire = MemoryHTTP(status=204, raw=b"", content_type="")
                exact_wire = MemoryHTTP(status=401, raw=b"Unauthorized", content_type="text/plain")
                selected = self.bind_http_sequence([login_wire, logout_wire, exact_wire])
                failed_name = "candidate-login-" + suffix
                save_attempts, observed_login_errors = [], []
                ordinary_save = run.save
                failure = TARGET.CaptureError("Synthetic one-shot login journal failure.")

                def save(name, value):
                    save_attempts.append(name)
                    if name == failed_name:
                        self.assertEqual(save_attempts.count(name), 1)
                        self.assertEqual(actor.token, TOKEN)
                        self.assertEqual(actor.owned_proof, proof_fixture())
                        self.assertIsNone(actor.proof)
                        raise failure
                    return ordinary_save(name, value)

                def capture(owner):
                    self.assertIs(owner, actor)
                    try:
                        owner.login()
                    except TARGET.CaptureError as error:
                        observed_login_errors.append(error)
                        raise
                    self.fail("The failed login journal was incorrectly treated as a successful capture.")

                run.save = save
                run.load = Mock()
                run.prepare = Mock()
                run.capture_actor = capture
                run.state = {"synthetic": "owned-state"}
                run.op = types.SimpleNamespace()
                run.primary, run.media = {"synthetic_primary": "preserved"}, {"synthetic_media": "preserved"}
                run.prior = {str(path): {"synthetic_closed_root": str(path)} for path in TARGET.PRIOR_ROOTS}
                run.primary_fact = lambda: copy.deepcopy(run.primary)
                run.media_reader = types.SimpleNamespace(media_witness=lambda *_args: copy.deepcopy(run.media))
                _, after, _ = ledger_fixture()
                run.snapshot = lambda: copy.deepcopy(after)
                self.enterContext(patch.object(TARGET, "preserved_tree", lambda path: copy.deepcopy(run.prior[str(path)])))
                result = run.execute()
                self.assertEqual(observed_login_errors, [failure])
                self.assertEqual(result["status"], "retained_for_review")
                self.assertTrue(actor.closed)
                self.assertEqual(actor.owned_proof, proof_fixture())
                self.assertIsNone(actor.proof)
                self.assertEqual(selected, [login_wire, logout_wire, exact_wire])
                self.assertEqual([wire.sent[0][:2] for wire in selected], [
                    ("POST", "/emby/Users/AuthenticateByName"),
                    ("POST", "/emby/Sessions/Logout"), ("GET", "/emby/System/Info"),
                ])
                self.assertNotIn("X-Emby-Token", login_wire.sent[0][3])
                self.assertEqual([wire.sent[0][3]["X-Emby-Token"] for wire in (logout_wire, exact_wire)], [TOKEN, TOKEN])
                self.assertEqual(run.budget.requests, 3)
                self.assertEqual(save_attempts.count(failed_name), 1)
                self.assertNotIn(failed_name, run.saved)
                self.assertEqual(run.cleanup_identity.call_count, 2)
                report = run.saved["report.json"]
                self.assertEqual(report["status"], "retained_for_review")
                self.assertEqual(report["errors"], [{"stage": "capture", "failure_type": "CaptureError", "reason": str(failure)}])
                self.assertTrue(run.ledger["owned_sessions_closed"])
                self.assertEqual(run.ledger["new_audits"], 2)
                actor.cleanup()
                self.assertEqual(run.budget.requests, 3)
                self.assertEqual(len(selected), 3)

    def test_partial_login_journal_failure_never_grants_cleanup_from_raw_fragments(self):
        complete_raw = TARGET.canonical(login_payload())
        cases = (MemoryHTTP(raw=b'{"AccessToken":"' + TOKEN.encode() + b'"'),
                 MemoryHTTP(raw=complete_raw, declared_length=str(len(complete_raw) + 1)))
        for wire in cases:
            with self.subTest(declared_length=wire.declared_length):
                run, actor = memory_run()
                actor.token = actor.proof = actor.owned_proof = None
                selected = self.bind_http_sequence([wire])
                ordinary_save = run.save
                attempts = []

                def save(name, value):
                    attempts.append(name)
                    if name == "candidate-login-raw.bin":
                        self.assertIsNone(actor.token)
                        self.assertIsNone(actor.owned_proof)
                        raise TARGET.CaptureError("Synthetic partial login journal failure.")
                    return ordinary_save(name, value)

                run.save = save
                self.reject(actor.login)
                self.assertIsNone(actor.token)
                self.assertIsNone(actor.owned_proof)
                self.assertIsNone(actor.proof)
                actor.cleanup()
                self.assertFalse(actor.closed)
                self.assertEqual(run.cleanup_identity.call_count, 0)
                self.assertEqual(run.budget.requests, 1)
                self.assertEqual(selected, [wire])
                self.assertEqual(attempts.count("candidate-login-raw.bin"), 1)


def candidate_cleanup_properties():
    return {"MainPID": str(TARGET.PROCESS["pid"]), "InvocationID": TARGET.INVOCATION,
            "ActiveState": "active", "SubState": "running"}


class CleanupIdentityGuards(GuardTestCase):
    def real_identity_run(self, *, process=None, properties=None):
        run, actor = memory_run()
        del run.cleanup_identity
        run.state = {"synthetic": "owned-candidate-state"}
        run.op = types.SimpleNamespace(verify_service=Mock(return_value=copy.deepcopy(TARGET.PROCESS if process is None else process)))
        run.properties = Mock(return_value=copy.deepcopy(candidate_cleanup_properties() if properties is None else properties))
        run.check = denied
        return run, actor

    def bind_http(self, wire):
        self.enterContext(patch.object(TARGET.http.client, "HTTPConnection", wire.connect))
        self.enterContext(patch.object(TARGET.signal, "getsignal", lambda _which: signal.SIG_DFL))
        self.enterContext(patch.object(TARGET.signal, "signal", wire.signal))
        self.enterContext(patch.object(TARGET.signal, "setitimer", wire.timer))

    def test_actual_candidate_cleanup_identity_uses_only_the_minimal_current_service_check(self):
        run, actor = self.real_identity_run()
        wire = MemoryHTTP(status=401, raw=b"Unauthorized", content_type="text/plain")
        self.bind_http(wire)
        result = run.request(actor, "exact-token", "GET", "/emby/System/Info", cleanup=True)
        self.assertTrue(result["complete"])
        self.assertEqual(result["status"], 401)
        run.op.verify_service.assert_called_once_with(run.state)
        run.properties.assert_called_once_with("goby-client-m3e.service")
        self.assertEqual(wire.connections, [("127.0.0.1", 18198, 8)])
        self.assertEqual(run.budget.requests, 1)

    def test_candidate_cleanup_identity_mismatch_stops_before_budget_intent_and_http(self):
        defects = [("process", key, "foreign-process-value") for key in TARGET.PROCESS]
        defects += [("properties", key, "foreign-property-value") for key in candidate_cleanup_properties()]
        defects.append(("verification", "failure", None))
        for kind, key, wrong in defects:
            with self.subTest(kind=kind, key=key):
                process, properties = copy.deepcopy(TARGET.PROCESS), candidate_cleanup_properties()
                if kind == "process":
                    process[key] = wrong
                elif kind == "properties":
                    properties[key] = wrong
                run, actor = self.real_identity_run(process=process, properties=properties)
                if kind == "verification":
                    run.op.verify_service.side_effect = TARGET.CaptureError("Synthetic candidate pin mismatch.")
                wire = MemoryHTTP(status=204, raw=b"")
                self.bind_http(wire)
                self.reject(lambda: run.request(actor, "logout", "POST", "/emby/Sessions/Logout", cleanup=True))
                self.assertEqual(run.budget.requests, 0)
                self.assertEqual(run.budget.total, 0)
                self.assertEqual(run.labels, set())
                self.assertEqual(run.saved, {})
                self.assertEqual(wire.connections, [])
                self.assertEqual(wire.sent, [])
                self.assertEqual(wire.timers, [])
                run.op.verify_service.assert_called_once_with(run.state)
                self.assertEqual(run.properties.call_count, int(kind == "properties"))

    def test_actual_reference_cleanup_identity_uses_the_existing_proxy_without_vendor_inspection(self):
        run, _ = self.real_identity_run()
        run.op.verify_service = run.properties = denied
        actor = TARGET.Actor(run, "reference", credential("reference"), copy.deepcopy(DEVICE))
        actor.token = TOKEN
        actor.owned_proof = TARGET.validate_login_ownership(login_payload("reference"),
            TARGET.TARGETS["reference"]["server_id"], TARGET.TARGETS["reference"], DEVICE)
        run.actors = [actor]
        wire = MemoryHTTP(status=401, raw=b"Unauthorized", content_type="text/plain")
        self.bind_http(wire)
        result = run.request(actor, "exact-token", "GET", "/emby/System/Info", cleanup=True)
        self.assertTrue(result["complete"])
        self.assertEqual(result["status"], 401)
        self.assertEqual(wire.connections, [("127.0.0.1", 18197, 8)])
        self.assertEqual(run.budget.requests, 1)


class MemoryPath(PurePosixPath):
    """Provide only explicit memory adapters for operations beyond path syntax."""

    def __init__(self, *segments):
        paths = []
        for segment in segments:
            if isinstance(segment, PurePosixPath):
                paths.extend(segment._raw_paths)
            elif type(segment) is str:
                paths.append(segment)
            else:
                raise TypeError("Memory paths accept only strings or trusted POSIX paths.")
        self._raw_paths = paths

    def with_segments(self, *segments):
        return type(self)(*segments)

    def absolute(self):
        if not self.is_absolute():
            raise AssertionError("A memory path must already be absolute.")
        return self

    def lstat(self):
        denied()

    def stat(self):
        return self.lstat()

    def read_bytes(self):
        denied()

    def read_text(self, encoding="utf-8", errors="strict"):
        return self.read_bytes().decode(encoding, errors)


def stat_fixture(*, inode=101, size=1, mode=0o600, uid=0, gid=0, directory=False):
    return types.SimpleNamespace(
        st_dev=7, st_ino=inode, st_nlink=1, st_size=size,
        st_mode=(stat.S_IFDIR if directory else stat.S_IFREG) | mode,
        st_uid=uid, st_gid=gid, st_mtime_ns=1_700_000_000_000_000_003,
        st_ctime_ns=1_700_000_000_000_000_007,
    )


class PrimaryFactGuards(GuardTestCase):
    def assert_json_shape(self, value):
        self.assertNotIsInstance(value, tuple)
        if type(value) is dict:
            for key, child in value.items():
                self.assertIs(type(key), str)
                self.assert_json_shape(child)
        elif type(value) is list:
            for child in value:
                self.assert_json_shape(child)
        else:
            self.assertIn(type(value), (str, int, bool, float, type(None)))

    def test_actual_primary_fact_serializes_real_stat_projection_without_tuples(self):
        process = MemoryPath("/proc") / str(TARGET.PRIMARY_PROCESS["pid"])
        raw_files, statistics, expected_hashes = {}, {}, {}
        for index, (filename, digest) in enumerate(TARGET.PRIMARY_FILES.items()):
            raw = ("Synthetic primary file: " + filename).encode()
            raw_files[filename] = raw
            statistics[filename] = stat_fixture(inode=101 + index, size=len(raw),
                                                mode=0o755 if filename == "/opt/goby-dev/goby" else 0o600)
            expected_hashes[raw] = digest
        raw_files[str(process / "exe")] = raw_files["/opt/goby-dev/goby"]
        raw_files[str(process / "cgroup")] = ("0::/system.slice/" + TARGET.PRIMARY_UNIT + "\n").encode()
        raw_files[str(process / "cmdline")] = b"synthetic primary command\x00"
        raw_files[str(process / "environ")] = b"SYNTHETIC_PRIMARY_ENV=fixture\x00"
        expected_hashes[raw_files[str(process / "cmdline")]] = "c2c8b1f234839029e4dcd80ada8d765fa6d4ebcb21be2f4b72665945d9458d6e"
        expected_hashes[raw_files[str(process / "environ")]] = "cfa0a59530f5e302b8ea23bb011823d7e68126be05e8b19c6360e450cbee9186"
        statistics[str(process)] = stat_fixture(inode=800, uid=995, gid=995, mode=0o555, directory=True)
        protected_calls, link_calls = [], []

        def protected(path, expected=None, *, modes=(0o600,), **options):
            filename = str(path)
            self.assertIn(filename, TARGET.PRIMARY_FILES)
            self.assertEqual(expected, TARGET.PRIMARY_FILES[filename])
            self.assertEqual(modes, (0o600, 0o644, 0o755))
            self.assertEqual(options, {})
            protected_calls.append(filename)
            return raw_files[filename]

        def readlink(path):
            self.assertEqual(path, process / "exe")
            link_calls.append(str(path))
            return "/opt/goby-dev/goby"

        self.enterContext(patch.object(TARGET, "Path", MemoryPath))
        self.enterContext(patch.object(MemoryPath, "lstat", lambda path: statistics[str(path)]))
        self.enterContext(patch.object(MemoryPath, "read_bytes", lambda path: raw_files[str(path)]))
        self.enterContext(patch.object(TARGET, "protected", protected))
        self.enterContext(patch.object(TARGET.os, "readlink", readlink))
        self.enterContext(patch.object(TARGET, "sha", lambda raw: expected_hashes[raw]))
        properties = {"MainPID": str(TARGET.PRIMARY_PROCESS["pid"]), "InvocationID": TARGET.PRIMARY_INVOCATION,
                      "ActiveState": "active", "SubState": "running", "ControlGroup": "/system.slice/" + TARGET.PRIMARY_UNIT,
                      "User": "goby", "Group": "goby"}
        run = TARGET.Run(types.SimpleNamespace(check_only=True))
        run.properties = Mock(side_effect=lambda unit: copy.deepcopy(properties) if unit == TARGET.PRIMARY_UNIT else denied())
        run.op = types.SimpleNamespace(process_identity=Mock(side_effect=lambda pid:
            copy.deepcopy(TARGET.PRIMARY_PROCESS) if pid == TARGET.PRIMARY_PROCESS["pid"] else denied()))
        first, second = run.primary_fact(), run.primary_fact()
        self.assertEqual(first["properties"], properties)
        self.assertEqual(first["process"], TARGET.PRIMARY_PROCESS)
        self.assertEqual(set(first["files"]), set(TARGET.PRIMARY_FILES))
        for filename, digest in TARGET.PRIMARY_FILES.items():
            info = statistics[filename]
            expected_identity = {"device": info.st_dev, "inode": info.st_ino, "links": info.st_nlink,
                                 "bytes": info.st_size, "mode": stat.S_IMODE(info.st_mode),
                                 "uid": info.st_uid, "gid": info.st_gid,
                                 "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns}
            identity = TARGET.file_identity(info)
            self.assertIs(type(identity), dict)
            self.assertEqual(identity, expected_identity)
            self.assertTrue(all(type(value) is int for value in identity.values()))
            self.assertEqual(first["files"][filename], {"sha256": digest, "identity": identity})
        self.assert_json_shape(first)
        self.assertEqual(TARGET.decode(TARGET.canonical(first)), first)
        self.assertTrue(TARGET.same(first, second))
        self.reject(lambda: TARGET.canonical(tuple(TARGET.file_identity(statistics["/opt/goby-dev/goby"]).values())))
        self.assertEqual(protected_calls, list(TARGET.PRIMARY_FILES) * 2)
        self.assertEqual(len(link_calls), 2)
        self.assertEqual(run.properties.call_count, 2)
        self.assertEqual(run.op.process_identity.call_count, 4)


class ReachedMemoryLockBoundary(Exception):
    """Mark the first post-input boundary without opening a real lock file."""


def startup_state():
    return {"schema": 27, "phase": "ready", "stage": "complete", "marker": "goby-m3e-client-acceptance-v1",
            "work": str(TARGET.WORK), "server_id": TARGET.TARGETS["candidate"]["server_id"],
            "process": copy.deepcopy(TARGET.PROCESS), "binary_sha256": TARGET.BINARY_SHA,
            "browser_sha256": TARGET.CREDENTIALS_SHA, "viewer_id": TARGET.TARGETS["candidate"]["user_id"]}


def startup_terminal():
    state = {"recursive_cgroup_empty": True,
             "properties": {"MainPID": "0", "ControlGroup": "", "Result": "exit-code", "ExecMainStatus": "1"}}
    return {"marker": "goby-client-library-changed-failed-terminal-v1", "status": "failed", "phase": "discovery",
            "browser_sessions_closed": True, "normal_ui_logout_and_exact401": True,
            "native_requests": 0, "metadata_writes": 0, "media_preserved": True, "primary_preserved": True,
            "states": [copy.deepcopy(state), copy.deepcopy(state)],
            "evidence_sha256": {"after-full.json": TARGET.BASELINE_SHA},
            "ledger": {"old_rows_sequences_private_preserved": True, "owned_sessions_closed": True}}


class StartupInputGuards(GuardTestCase):
    """Call the real load prefix without importing historical helper modules."""

    def startup(self, state, terminal):
        source_path = str(TARGET.TOOL / "capture-collection-folder-contract.py")
        source_sha256 = hashlib.sha256(SOURCE).hexdigest()
        records = {source_path: (source_sha256, SOURCE, {"modes": (0o600, 0o644), "limit": 2 << 20}),
                   str(TARGET.STATE): (TARGET.STATE_SHA, TARGET.canonical(state), {}),
                   str(TARGET.PRIOR_TERMINAL): (TARGET.PRIOR_TERMINAL_SHA, TARGET.canonical(terminal), {})}
        reads, locks = [], []

        def protected(path, expected=None, **options):
            filename = str(path)
            self.assertIn(filename, records)
            digest, raw, expected_options = records[filename]
            self.assertEqual(expected, digest)
            self.assertEqual(options, expected_options)
            reads.append(filename)
            return raw

        def stop_at_lock(path, flags):
            self.assertEqual(str(path), "/synthetic/startup.lock")
            self.assertEqual(flags, os.O_RDONLY | os.O_NOFOLLOW)
            locks.append(str(path))
            raise ReachedMemoryLockBoundary()

        self.enterContext(patch.object(TARGET, "Path", MemoryPath))
        self.enterContext(patch.object(TARGET, "__file__", source_path))
        self.enterContext(patch.object(TARGET, "HELPERS", {}))
        self.enterContext(patch.object(TARGET, "protected", protected))
        self.enterContext(patch.object(TARGET.sys, "platform", "linux"))
        self.enterContext(patch.object(TARGET.os, "environ", {"SSH_CONNECTION": "memory-only-ssh-context"}))
        for name in ("getuid", "geteuid", "getgid", "getegid"):
            self.enterContext(patch.object(TARGET.os, name, lambda: 0))
        self.enterContext(patch.object(TARGET.os, "open", stop_at_lock))
        run = TARGET.Run(types.SimpleNamespace(check_only=True, script_sha256=source_sha256))
        run.op = types.SimpleNamespace(LOCK=MemoryPath("/synthetic/startup.lock"))
        run.prepare = run.request = run.save = denied
        return run, reads, locks

    def assert_before_publication(self, run, locks):
        self.assertEqual(locks, [])
        self.assertEqual(run.budget.requests, 0)
        self.assertEqual(run.records, {})
        self.assertEqual(run.actors, [])
        self.assertIsNone(run.root_fd)
        self.assertIsNone(run.lock)

    def test_real_check_only_load_accepts_valid_inputs_up_to_the_memory_lock_boundary(self):
        run, reads, locks = self.startup(startup_state(), startup_terminal())
        with self.assertRaises(ReachedMemoryLockBoundary):
            run.check_only()
        self.assertEqual(reads, [str(TARGET.TOOL / "capture-collection-folder-contract.py"),
                                 str(TARGET.STATE), str(TARGET.PRIOR_TERMINAL)])
        self.assertEqual(locks, ["/synthetic/startup.lock"])
        self.assertEqual(run.budget.requests, 0)
        self.assertEqual(run.records, {})
        self.assertEqual(run.actors, [])
        self.assertIsNone(run.root_fd)

    def test_real_load_rejects_each_state_binding_before_terminal_lock_or_http(self):
        for key in startup_state():
            for missing in (False, True):
                with self.subTest(key=key, missing=missing):
                    state = startup_state()
                    if missing:
                        state.pop(key)
                    else:
                        state[key] = "foreign-state-value"
                    run, reads, locks = self.startup(state, startup_terminal())
                    self.reject(run.check_only)
                    self.assertEqual(reads, [str(TARGET.TOOL / "capture-collection-folder-contract.py"), str(TARGET.STATE)])
                    self.assert_before_publication(run, locks)

    def test_real_load_rejects_unclosed_or_unbound_old_terminal_before_lock_or_http(self):
        defects = [
            lambda value: value.update(marker="foreign-terminal"),
            lambda value: value.update(status="passed"),
            lambda value: value.update(phase="capture"),
            lambda value: value.update(browser_sessions_closed=False),
            lambda value: value.update(normal_ui_logout_and_exact401=False),
            lambda value: value.update(native_requests=1),
            lambda value: value.update(metadata_writes=1),
            lambda value: value.update(media_preserved=False),
            lambda value: value.update(primary_preserved=False),
            lambda value: value.update(states=[]),
            lambda value: value["states"].pop(),
            lambda value: value["states"][0].update(recursive_cgroup_empty=False),
            lambda value: value["states"][1].update(recursive_cgroup_empty=False),
            lambda value: value["evidence_sha256"].update({"after-full.json": "0" * 64}),
            lambda value: value["ledger"].update(old_rows_sequences_private_preserved=False),
            lambda value: value["ledger"].update(owned_sessions_closed=False),
        ]
        for index in (0, 1):
            for key, wrong in (("MainPID", "1"), ("ControlGroup", "/live-group"),
                               ("Result", "success"), ("ExecMainStatus", "0")):
                defects.append(lambda value, index=index, key=key, wrong=wrong:
                               value["states"][index]["properties"].update({key: wrong}))
        for key in startup_terminal():
            defects.append(lambda value, key=key: value.pop(key))
        for index, mutate in enumerate(defects):
            with self.subTest(defect=index):
                terminal = startup_terminal()
                mutate(terminal)
                run, reads, locks = self.startup(startup_state(), terminal)
                self.reject(run.check_only)
                self.assertEqual(reads, [str(TARGET.TOOL / "capture-collection-folder-contract.py"),
                                         str(TARGET.STATE), str(TARGET.PRIOR_TERMINAL)])
                self.assert_before_publication(run, locks)


def memory_error(error, test_id):
    frames = []
    current = error[2]
    while current is not None:
        code = current.tb_frame.f_code
        frames.append({"function": code.co_name, "file": code.co_filename, "line": current.tb_lineno})
        current = current.tb_next
    return json.dumps({"test": test_id, "error_type": error[0].__name__,
                       "message": str(error[1]), "frames": frames}, sort_keys=True)


class MemoryResult(unittest.TestResult):
    def _exc_info_to_string(self, error, test):
        return memory_error(error, test.id())


class MemoryInfrastructureGuards(GuardTestCase):
    def test_regex_backreference_uses_only_the_preloaded_memory_module(self):
        self.assertEqual(re.sub(r"(a)", r"\1", "a"), "a")
        self.assertIs(resolve_regex_import("re", fromlist=[]), PRELOADED_REGEX_MODULES["re"])
        self.assertIs(resolve_regex_import("re", {"__name__": "memory_caller"}, fromlist=[]), PRELOADED_REGEX_MODULES["re"])

    def test_regex_adapter_rejects_unknown_relative_and_fromlist_imports(self):
        cases = (
            {"name": "os"}, {"name": "_sre"}, {"name": "re._parser"}, {"name": "unloaded_module"},
            {"name": "re", "level": 1}, {"name": "re", "level": False},
            {"name": "re", "fromlist": ["*"]}, {"name": "re", "fromlist": ["sub"]},
            {"name": "re", "fromlist": ["missing"]}, {"name": "re", "fromlist": ()},
            {"name": "re", "fromlist": None},
        )
        for request in cases:
            with self.subTest(request=request):
                arguments = {"fromlist": [], **request}
                with self.assertRaises(MemoryImportError):
                    resolve_regex_import(**arguments)

    def test_malformed_json_uses_the_exact_cached_decoder_import_shape(self):
        self.assertIs(resolve_regex_import("json.decoder", {"__name__": "json.decoder"}, fromlist=[], level=0),
                      PRELOADED_JSON_IMPORTS["json.decoder"])
        self.assertIs(PRELOADED_JSON_IMPORTS["json.decoder"], json)
        with self.assertRaises(json.JSONDecodeError):
            TARGET.decode(b'{"AccessToken":"' + TOKEN.encode() + b'"')

    def test_decoder_adapter_rejects_other_callers_modules_and_import_shapes(self):
        base = {"name": "json.decoder", "globals": {"__name__": "json.decoder"}, "fromlist": [], "level": 0}
        for changed in ({"name": "json"}, {"name": "json.scanner"}, {"globals": None},
                        {"globals": {"__name__": "foreign"}}, {"level": 1}, {"level": False},
                        {"fromlist": None}, {"fromlist": ()}, {"fromlist": ["*"]},
                        {"fromlist": ["JSONDecodeError"]}):
            with self.subTest(changed=changed):
                with self.assertRaises(MemoryImportError):
                    resolve_regex_import(**{**base, **changed})

    def test_subtest_failure_is_formatted_from_memory_without_linecache_access(self):
        self.enterContext(patch.object(linecache, "checkcache", denied))
        self.enterContext(patch.object(linecache, "getlines", denied))

        class SyntheticFailure(unittest.TestCase):
            def runTest(self):
                with self.subTest(memory_only=True):
                    self.fail("Synthetic failure for the memory result adapter.")

        result = MemoryResult()
        SyntheticFailure().run(result)
        self.assertEqual(result.testsRun, 1)
        self.assertEqual(len(result.failures), 1)
        self.assertEqual(result.errors, [])
        detail = json.loads(result.failures[0][1])
        self.assertEqual(detail["error_type"], "AssertionError")
        self.assertEqual(detail["message"], "Synthetic failure for the memory result adapter.")
        self.assertTrue(detail["frames"])
        self.assertTrue(all(set(frame) == {"function", "file", "line"} for frame in detail["frames"]))


GUARD_GROUPS = (MemoryInfrastructureGuards, ExactJSONGuards, LoginAndRedactionGuards, RequestAndBudgetGuards, LedgerGuards,
                TransportAndCleanupGuards, ActorLoginGuards, OwnershipPublicationGuards, CleanupIdentityGuards,
                PrimaryFactGuards, StartupInputGuards)


def load_target(operator, expected_sha256):
    global TARGET, SOURCE
    if operator.name != "capture-collection-folder-contract.py":
        raise RuntimeError("Only the CollectionFolder capture controller may be loaded.")
    SOURCE = operator.read_bytes()
    observed = hashlib.sha256(SOURCE).hexdigest()
    if not re.fullmatch(r"[0-9a-f]{64}", expected_sha256) or observed != expected_sha256:
        raise RuntimeError("The explicitly pinned controller SHA-256 differs.")
    syntax = ast.parse(SOURCE, filename=str(operator))
    for node in ast.walk(syntax):
        if isinstance(node, ast.Import):
            names = [alias.name for alias in node.names]
        elif isinstance(node, ast.ImportFrom):
            if node.level:
                raise RuntimeError("A project or relative import is not permitted in the capture controller.")
            names = [node.module]
        else:
            continue
        if any(name not in SAFE_IMPORTS for name in names):
            raise RuntimeError("The capture controller imports outside the explicit standard-library allowlist.")
    code = compile(SOURCE, str(operator), "exec")
    TARGET = types.ModuleType("collection_folder_contract_memory_target")
    TARGET.__file__ = str(operator)
    sys.modules[TARGET.__name__] = TARGET
    sys.addaudithook(audit_effect)
    with EffectFence(allowed_code=code):
        exec(code, TARGET.__dict__)
    return observed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--operator", type=Path, required=True)
    parser.add_argument("--operator-sha256", required=True)
    args = parser.parse_args()
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run memory guards only through authorized root SSH on test-env.")
    guard_sha256 = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    operator_sha256, phase = None, "operator_import"
    result = MemoryResult()
    try:
        operator_sha256 = load_target(args.operator, args.operator_sha256)
        suite = unittest.TestSuite()
        for case in GUARD_GROUPS:
            suite.addTests(unittest.defaultTestLoader.loadTestsFromTestCase(case))
        phase = "guard_runner"
        suite.run(result)
    except Exception:
        result.errors.append((None, memory_error(sys.exc_info(), phase)))
        if operator_sha256 is None and SOURCE:
            operator_sha256 = hashlib.sha256(SOURCE).hexdigest()
    passed = result.wasSuccessful() and not result.skipped and result.testsRun > 0
    details = [detail for _, detail in [*result.failures, *result.errors]]
    print(json.dumps({
        "suite": "collection-folder-contract-memory-guards", "passed": passed,
        "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
        "skips": len(result.skipped), "operator_sha256": operator_sha256, "guard_sha256": guard_sha256,
        "live_http_executed": False, "database_commands": 0, "service_actions": 0,
        "evidence_created": False, "failure_details": None if passed else "\n".join(details),
    }, sort_keys=True))
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
