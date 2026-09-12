#!/usr/bin/env python3
"""Guard the new LibraryChanged controller using only memory fixtures.

The sibling controller and this guard are the only project sources read. The
controller contributes definitions only; no historical controller is imported.
Private JSON callbacks are prepared before the effect fence. Every test keeps
filesystem, process, network, import, and dynamic-code operations fenced unless
an explicit memory adapter owns that boundary.
"""

from __future__ import annotations

import argparse
import builtins
import contextlib
import copy
import datetime
from decimal import Decimal
import fcntl
import hashlib
import http.client
import importlib.util
import io
import json
import json.decoder
import json.scanner
import math
import os
from pathlib import Path, PurePosixPath
import re
import signal
import socket
import stat
import subprocess
import sys
import time
import types
import unicodedata
import unittest
from unittest.mock import Mock, call, patch


sys.dont_write_bytecode = True


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("Duplicate JSON object member.")
        result[key] = value
    return result


def invalid_constant(_value):
    raise ValueError("Nonfinite JSON numbers are forbidden.")


def make_memory_json_decoder():
    decoder = json.JSONDecoder(object_pairs_hook=unique_object, parse_float=Decimal,
                               parse_constant=invalid_constant)
    decoder.parse_string = json.decoder.py_scanstring
    object_globals = dict(json.decoder.JSONObject.__globals__)
    object_globals["scanstring"] = json.decoder.py_scanstring
    decoder.parse_object = types.FunctionType(json.decoder.JSONObject.__code__, object_globals,
        "memory_json_object", json.decoder.JSONObject.__defaults__, json.decoder.JSONObject.__closure__)
    decoder.scan_once = json.scanner.py_make_scanner(decoder)
    return decoder


MEMORY_JSON_DECODER = make_memory_json_decoder()


def memory_json_decode(raw):
    if isinstance(raw, bytes):
        raw = raw.decode("utf-8")
    return MEMORY_JSON_DECODER.decode(raw)


class MemoryPath(PurePosixPath):
    """Keep path composition independent from runtime platform imports."""

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


OPERATOR_PATH = Path(__file__).with_name("observe-client-library-changed.py")
OPERATOR_RAW = OPERATOR_PATH.read_bytes()
GUARD_RAW = Path(__file__).read_bytes()
OPERATOR_SHA256 = hashlib.sha256(OPERATOR_RAW).hexdigest()
GUARD_SHA256 = hashlib.sha256(GUARD_RAW).hexdigest()
ACTIVE_FENCES = []


def deny_effect(label):
    for fence in ACTIVE_FENCES:
        fence.violations.append(label)
    raise AssertionError("An unfaked external effect was attempted: " + label)


def denied(*_args, **_kwargs):
    deny_effect("external API")


def audit_effect(event, arguments):
    if not ACTIVE_FENCES:
        return
    fence = ACTIVE_FENCES[-1]
    if event == "exec" and fence.allowed_code is not None and arguments and arguments[0] is fence.allowed_code:
        return
    if event == "import" and fence.allowed_code is not None and arguments and arguments[0] in sys.modules:
        return
    if event in {"open", "import", "compile", "exec", "builtins.input", "builtins.breakpoint"} or event.startswith(
            ("os.", "subprocess.", "socket.", "ctypes.", "fcntl.", "mmap.", "shutil.", "tempfile.", "pty.")):
        deny_effect("audit:" + event)


sys.addaudithook(audit_effect)


class EffectFence(contextlib.ExitStack):
    def __init__(self, allowed_code=None):
        super().__init__()
        self.allowed_code = allowed_code

    def __enter__(self):
        super().__enter__()
        self.violations = []
        ACTIVE_FENCES.append(self)
        surfaces = [
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
        ]
        if self.allowed_code is None:
            surfaces.append((builtins, ("__import__", "compile", "exec", "eval")))
            surfaces.append((importlib, ("import_module", "reload")))
        for owner, names in surfaces:
            for name in names:
                if hasattr(owner, name):
                    self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments):
        try:
            result = super().__exit__(*arguments)
        finally:
            if ACTIVE_FENCES and ACTIVE_FENCES[-1] is self:
                ACTIVE_FENCES.pop()
        if self.violations:
            raise AssertionError("The guard attempted external effects: " + ", ".join(self.violations))
        return result


SPEC = importlib.util.spec_from_file_location("library_changed_controller_under_test", OPERATOR_PATH)
if SPEC is None or SPEC.loader is None:
    raise RuntimeError("The sibling LibraryChanged controller cannot be loaded.")
CONTROLLER = importlib.util.module_from_spec(SPEC)
OPERATOR_CODE = SPEC.loader.source_to_code(OPERATOR_RAW, str(OPERATOR_PATH))
sys.modules[SPEC.name] = CONTROLLER
with EffectFence(allowed_code=OPERATOR_CODE):
    exec(OPERATOR_CODE, CONTROLLER.__dict__)


JSON_LOAD_OPTIONS = {}


def dispatch_object_pairs(pairs):
    callback = JSON_LOAD_OPTIONS.get("object_pairs_hook")
    return callback(pairs) if callback is not None else dict(pairs)


def dispatch_integer(value):
    return (JSON_LOAD_OPTIONS.get("parse_int") or int)(value)


def dispatch_float(value):
    return (JSON_LOAD_OPTIONS.get("parse_float") or float)(value)


def dispatch_constant(value):
    callback = JSON_LOAD_OPTIONS.get("parse_constant")
    if callback is not None:
        return callback(value)
    return {"NaN": float("nan"), "Infinity": float("inf"), "-Infinity": float("-inf")}[value]


def make_controller_json_decoder():
    decoder = json.JSONDecoder(object_pairs_hook=dispatch_object_pairs, parse_int=dispatch_integer,
                               parse_float=dispatch_float, parse_constant=dispatch_constant)
    decoder.parse_string = json.decoder.py_scanstring
    object_globals = dict(json.decoder.JSONObject.__globals__)
    object_globals["scanstring"] = json.decoder.py_scanstring
    decoder.parse_object = types.FunctionType(json.decoder.JSONObject.__code__, object_globals,
        "controller_memory_json_object", json.decoder.JSONObject.__defaults__, json.decoder.JSONObject.__closure__)
    decoder.scan_once = json.scanner.py_make_scanner(decoder)
    return decoder


CONTROLLER_JSON_DECODER = make_controller_json_decoder()


def controller_json_loads(raw, **options):
    if set(options) - {"object_pairs_hook", "parse_int", "parse_float", "parse_constant"}:
        raise AssertionError("An unmodeled JSON decoder option was requested.")
    if JSON_LOAD_OPTIONS:
        raise AssertionError("The memory JSON decoder was entered recursively.")
    JSON_LOAD_OPTIONS.update(options)
    try:
        return CONTROLLER_JSON_DECODER.decode(raw.decode("utf-8") if isinstance(raw, bytes) else raw)
    finally:
        JSON_LOAD_OPTIONS.clear()


CONTROLLER_JSON = types.SimpleNamespace(**vars(json))
CONTROLLER_JSON.loads = controller_json_loads


def file_information(*, inode=100, links=1, mode=0o600, directory=False, size=1, device=7):
    return types.SimpleNamespace(st_dev=device, st_ino=inode, st_uid=0, st_gid=0, st_nlink=links,
        st_mode=(stat.S_IFDIR if directory else stat.S_IFREG) | mode, st_size=size,
        st_mtime_ns=1_700_000_000_000_000_000, st_ctime_ns=1_700_000_000_000_000_000)


class GuardTestCase(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.object(CONTROLLER, "Path", MemoryPath))
        for name, value in list(vars(CONTROLLER).items()):
            if isinstance(value, PurePosixPath):
                self.enterContext(patch.object(CONTROLLER, name, MemoryPath(str(value))))
        self.enterContext(EffectFence())
        self.enterContext(patch.object(CONTROLLER, "json", CONTROLLER_JSON))

    def reject(self, callback):
        with self.assertRaises(CONTROLLER.ObservationError):
            callback()


class MemoryJSONGuards(GuardTestCase):
    def test_private_python_callbacks_decode_nested_values_without_imports(self):
        self.assertIsInstance(MEMORY_JSON_DECODER.scan_once, types.FunctionType)
        self.assertIs(MEMORY_JSON_DECODER.parse_string, json.decoder.py_scanstring)
        self.assertIs(MEMORY_JSON_DECODER.parse_object.__globals__["scanstring"], json.decoder.py_scanstring)
        self.assertEqual(memory_json_decode(b'{"nested":{"values":[1,true,null,"text",0.125]}}'),
                         {"nested": {"values": [1, True, None, "text", Decimal("0.125")]}})

    def test_malformed_keys_values_arrays_duplicates_and_nonfinite_values_are_rejected(self):
        for raw in (b'{malformed', b'{"unterminated}', b'{"key":"bad\\q"}', b'{"key":}',
                    b'{"key":[1,]}', b'{"key":1,"key":2}', b'{"key":NaN}'):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                memory_json_decode(raw)


def automatic_values():
    return {
        "Name": "M3e Client Movie", "SortName": "m3e client movie", "Overview": "Original overview.",
        "OriginalTitle": "", "OfficialRating": "", "ProductionYear": None, "PremiereDate": None,
        "CommunityRating": None, "ProviderIDs": {}, "Genres": [], "Tags": [], "Studios": [],
        "People": [], "IndexNumber": None, "ParentIndexNumber": None,
    }


def expected_public_values():
    return {
        "Name": "M3e Client Movie", "SortName": "m3e client movie", "Overview": "Original overview.",
        "OriginalTitle": "", "OfficialRating": "", "ProductionYear": None, "PremiereDate": None,
        "CommunityRating": None, "ProviderIds": {}, "Genres": [], "Tags": [], "Studios": [],
        "People": [], "IndexNumber": None, "ParentIndexNumber": None,
    }


TABLES = ("schema_migrations", "server_settings", "users", "sessions", "libraries", "library_roots", "items",
    "scan_jobs", "catalog_entities", "item_entities", "item_images", "user_item_data", "play_sessions", "item_subtitles",
    "encoding_jobs", "client_playback_references", "item_metadata_state", "application_keys", "application_key_clients",
    "devices", "application_key_devices", "task_definitions", "task_triggers", "task_runs", "task_run_requests",
    "task_run_children", "task_occurrences", "managed_settings", "activity_entries", "user_settings", "theme_owner_ids",
    "theme_reserved_paths", "item_theme_resources", "extra_reserved_paths", "item_extra_resources")
TOKEN = "A" * 43
TOKEN_SHA = hashlib.sha256(TOKEN.encode()).hexdigest()
SESSION = "ba" * 16
NATIVE_SESSION = "ad" * 16
NATIVE_TOKEN = "N" * 43
NATIVE_TOKEN_SHA = hashlib.sha256(NATIVE_TOKEN.encode()).hexdigest()
ROOT_ID = "ba1501cff993a630b8d6a41f925cd37f"
INPUT_SHA = "f1" * 32


def at(seconds=0):
    return (datetime.datetime(2026, 9, 12, 6, tzinfo=datetime.timezone.utc) +
            datetime.timedelta(seconds=seconds)).isoformat().replace("+00:00", "Z")


def ordinary_proof():
    return {"token_sha256": TOKEN_SHA, "session_id": SESSION, "user_id": CONTROLLER.B,
        "device_id": "library-changed-guard-browser", "server_id": CONTROLLER.SERVER,
        "client_name": "Emby Web", "device_name": "Observed browser", "client_version": "4.9.5.0",
        "created_at": at(1), "created_at_source": "SessionInfo.LastActivityDate", "kind": "emby", "slot": "B",
        "frame_login_finished": True, "physical_login_completed": True, "request_metadata_matches": True}


def session_row(identifier, user, token_sha, kind, created, *, device_id="", device_registry_id=None):
    return {"id": identifier, "user_id": user, "token_hash": "\\x" + token_sha, "kind": kind,
        "client_name": "Emby Web" if kind == "emby" else "Goby Dashboard", "device_id": device_id if kind == "emby" else "goby-dashboard",
        "device_name": "Observed browser" if kind == "emby" else "Web browser", "client_version": "4.9.5.0" if kind == "emby" else "synthetic-source44",
        "created_at": at(created), "expires_at": at(created + (30 if kind == "emby" else 1) * 86400),
        "last_seen_at": at(created), "revoked_at": None, "client_capabilities": {}, "device_registry_id": device_registry_id}


def audit_row(identifier, action, actor, credential, resource_kind, resource, moment, *, source="native", revision=0):
    metadata = action == "metadata.updated"
    return {"id": identifier, "created_at": at(moment), "action": action, "severity": "Info", "source": source,
        "actor_kind": "user", "actor_id": actor, "actor_credential_id": credential, "resource_kind": resource_kind,
        "resource_id": resource, "request_id": "", "revision": revision, "affected_count": 0 if metadata else 1,
        "state": "", "changed_fields": ["Name", "Overrides"] if metadata else []}


def baseline_snapshot():
    tables = {name: [] for name in TABLES}
    tables["users"] = [
        {"id": CONTROLLER.B, "name": "m3e-client-viewer", "management_revision": 5, "is_administrator": False,
         "is_disabled": False, "has_password": True, "created_at": at(-86400)},
        {"id": CONTROLLER.ADMIN, "name": "m3e-client-administrator", "management_revision": 1, "is_administrator": True,
         "is_disabled": False, "has_password": True, "created_at": at(-86400)}]
    tables["server_settings"] = [{"key": "server_id", "value": CONTROLLER.SERVER}]
    for index in range(73):
        row = session_row(format(index + 1, "032x"), CONTROLLER.B,
            hashlib.sha256(("old-token-" + str(index)).encode()).hexdigest(), "emby", -86400)
        row["revoked_at"] = at(-3600)
        tables["sessions"].append(row)
    for index in range(62):
        tables["devices"].append({"id": index + 2, "reported_device_id": "old-device-" + str(index),
            "reported_name": "Old browser", "custom_name": None, "app_name": "Emby Web", "app_version": "4.9.5.0",
            "last_user_id": CONTROLLER.B, "created_at": at(-86400), "last_seen_at": at(-3600),
            "ip_address": "127.0.0.1", "revision": 1, "deleted_at": None})
    for index in range(163):
        old = tables["sessions"][index % 73]
        tables["activity_entries"].append(audit_row(index + 1, "session.revoked", CONTROLLER.B, old["id"],
            "session", old["id"], -3600, source="emby"))
    local = {"Kind": "movie", "Name": "M3e Client Movie", "SortName": "", "Overview": "Original overview.",
             "OriginalTitle": "", "OfficialRating": "", "ProductionYear": None, "PremiereDate": None,
             "CommunityRating": None, "ProviderIDs": None, "Genres": None, "Tags": None, "Studios": None, "People": None}
    item = {"id": CONTROLLER.ITEM, "library_id": CONTROLLER.LIBRARY, "root_id": ROOT_ID,
        "parent_id": CONTROLLER.LIBRARY, "name": "M3e Client Movie", "sort_name": "m3e client movie", "type": "Movie",
        "path": "/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4", "relative_path": "M3e Client Movie.mp4",
        "overview": "Original overview.", "is_folder": False, "index_number": 0, "parent_index_number": 0,
        "media": {"synthetic": "preserved"}, "file_identity": "synthetic-media-identity", "file_size": 1024,
        "modified_at": at(-86400), "created_at": at(-86400), "updated_at": at(-86400),
        "local_metadata": local, "local_metadata_hash": "7" * 64, "local_metadata_path": "/synthetic/original.nfo"}
    tables["items"].append(item)
    parent = copy.deepcopy(item)
    parent.update(id=CONTROLLER.LIBRARY, parent_id=None, root_id=None, name="M3e Client Movies", sort_name="m3e client movies",
                  type="CollectionFolder", is_folder=True, path="", relative_path="", local_metadata=None)
    tables["items"].append(parent)
    for index in range(20):
        other = copy.deepcopy(item)
        other.update(id=format(1000 + index, "032x"), name="Other item " + str(index),
                     relative_path="Other item " + str(index) + ".mp4", local_metadata=None)
        tables["items"].append(other)
    for value in tables["items"]:
        tables["item_metadata_state"].append({"item_id": value["id"], "automatic": automatic_values(),
            "source_key": {"Hash": value["local_metadata_hash"], "RootId": value["root_id"], "Path": value["path"]},
            "overrides": {}, "locked_values": {}, "effective": copy.deepcopy(value["local_metadata"]), "revision": 1,
            "last_edited_by": None, "last_edited_at": None, "updated_at": at(-86400), "music_source": {}})
    tables["libraries"] = [{"id": CONTROLLER.LIBRARY, "name": "M3e Client Movies"}] + [
        {"id": format(2000 + index, "032x"), "name": "Other library " + str(index)} for index in range(3)]
    tables["library_roots"] = [{"id": ROOT_ID, "library_id": CONTROLLER.LIBRARY, "path": "/synthetic/Movies"}]
    tables["play_sessions"] = [{"id": format(3000 + index, "032x"), "state": "Stopped"} for index in range(26)]
    tables["user_item_data"] = [{"user_id": CONTROLLER.B, "item_id": tables["items"][index]["id"], "play_count": 0}
                                for index in range(7)]
    columns = {name: list(rows[0]) if rows else [] for name, rows in tables.items()}
    return {"schema": 27, "private_files": {"fixture_sha256": CONTROLLER.STATE_SHA, "synthetic_credentials": "8" * 64},
        "media": {"groups": ["original", "extras", "music"]},
        "database": {"tables": tables, "metadata": {"captured_at": at(), "database": "goby_client_m3e",
            "server_version_num": 170006, "columns": columns, "relations": {"synthetic_owner": "goby_client_m3e"}},
            "catalog": {"schema": 27, "synthetic_preserved_catalog": True}, "unsupported": False,
            "sequences": {"devices_id_seq": {"last_value": 63, "is_called": True},
                          "activity_entries_id_seq": {"last_value": 163, "is_called": True},
                          "catalog_entities_id_seq": {"last_value": 20, "is_called": True}}}}


def profile_fixture(snapshot):
    item = next(row for row in snapshot["database"]["tables"]["items"] if row["id"] == CONTROLLER.ITEM)
    metadata = next(row for row in snapshot["database"]["tables"]["item_metadata_state"] if row["item_id"] == CONTROLLER.ITEM)
    return {"item": copy.deepcopy(item), "metadata": copy.deepcopy(metadata), "parent_name": "M3e Client Movies",
        "public": expected_public_values(), "target": {"id": CONTROLLER.ITEM, "library_id": CONTROLLER.LIBRARY,
            "name": item["name"], "type": "Movie", "parent_id": item["parent_id"], "root_id": item["root_id"],
            "relative_path": item["relative_path"]}}


def metadata_document(profile, phase="original", marker=None, edited_at=None):
    number = profile["metadata"]["revision"] + {"original": 0, "forward": 1, "restored": 2}[phase]
    effective = copy.deepcopy(profile["public"])
    overrides = copy.deepcopy(profile["metadata"]["overrides"])
    if phase == "forward":
        effective["Name"] = marker
        overrides["Name"] = marker
    return {"Item": {"Id": CONTROLLER.ITEM, "LibraryId": CONTROLLER.LIBRARY, "ParentId": profile["item"]["parent_id"],
        "ParentName": profile["parent_name"], "Name": effective["Name"], "Type": "Movie", "Path": profile["item"]["path"], "IsFolder": False},
        "Revision": str(number), "Automatic": expected_public_values(), "Effective": effective, "Overrides": overrides,
        "LockedValues": copy.deepcopy(profile["metadata"]["locked_values"]), "LockedFields": sorted(profile["metadata"]["locked_values"]),
        "EditableFields": list(CONTROLLER.ACTIVE_FIELDS), "InactiveFields": [],
        "LastEditedBy": CONTROLLER.ADMIN if phase != "original" else (profile["metadata"]["last_edited_by"] or ""),
        "LastEditedAt": edited_at if phase != "original" else profile["metadata"]["last_edited_at"]}


def reservation_fixture(profile):
    original = metadata_document(profile)
    marker = "M3e Client Movie [LC source44 v1]"
    forward = {"Revision": original["Revision"], "Overrides": dict(original["Overrides"], Name=marker),
               "LockedFields": list(original["LockedFields"])}
    restore = {"Revision": str(int(original["Revision"]) + 1), "Overrides": copy.deepcopy(original["Overrides"]),
               "LockedFields": list(original["LockedFields"])}
    digest = lambda value: hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    return {"original": original, "forward_body": forward, "restore_body": restore, "marker_name": marker,
        "public": {"target_id": CONTROLLER.ITEM, "library_id": CONTROLLER.LIBRARY, "revision": original["Revision"],
            "original_name": original["Effective"]["Name"], "marker_name": marker,
            "original_controls_sha256": digest({"Overrides": original["Overrides"], "LockedFields": original["LockedFields"],
                                                "LockedValues": original["LockedValues"]}),
            "forward_body_sha256": digest(forward), "restore_body_sha256": digest(restore)}}


def ledger_fixture(phase="restored", closed=True):
    before = baseline_snapshot()
    profile = profile_fixture(before)
    reservation = reservation_fixture(profile)
    browser = ordinary_proof()
    administrator = session_row(NATIVE_SESSION, CONTROLLER.ADMIN, NATIVE_TOKEN_SHA, "admin", 2)
    authenticated = copy.deepcopy(before)
    tables = authenticated["database"]["tables"]
    tables["sessions"].extend((session_row(SESSION, CONTROLLER.B, TOKEN_SHA, "emby", 1,
        device_id=browser["device_id"], device_registry_id=64), copy.deepcopy(administrator)))
    tables["devices"].append({"id": 64, "reported_device_id": browser["device_id"], "reported_name": browser["device_name"],
        "custom_name": None, "app_name": browser["client_name"], "app_version": browser["client_version"],
        "last_user_id": CONTROLLER.B, "created_at": at(0.5), "last_seen_at": at(1),
        "ip_address": "127.0.0.1", "revision": 1, "deleted_at": None})
    tables["activity_entries"].extend((audit_row(164, "session.login", CONTROLLER.B, SESSION, "session", SESSION, 1, source="emby"),
                                      audit_row(165, "session.login", CONTROLLER.ADMIN, NATIVE_SESSION, "session", NATIVE_SESSION, 2)))
    authenticated["database"]["sequences"].update(devices_id_seq={"last_value": 64, "is_called": True},
                                                   activity_entries_id_seq={"last_value": 165, "is_called": True})
    authenticated["database"]["metadata"]["captured_at"] = at(3)
    after = copy.deepcopy(authenticated)
    rows = after["database"]["tables"]
    steps = {"original": 0, "forward": 1, "restored": 2}[phase]
    if steps:
        item = next(row for row in rows["items"] if row["id"] == CONTROLLER.ITEM)
        metadata = next(row for row in rows["item_metadata_state"] if row["item_id"] == CONTROLLER.ITEM)
        marker = reservation["marker_name"] if phase == "forward" else profile["item"]["name"]
        item.update(name=marker, updated_at=at(4 if phase == "forward" else 6))
        metadata.update(overrides={"Name": marker} if phase == "forward" else {}, revision=1 + steps,
                        last_edited_by=CONTROLLER.ADMIN, last_edited_at=at(4 if phase == "forward" else 6),
                        updated_at=at(4 if phase == "forward" else 6))
        metadata["effective"] = copy.deepcopy(profile["metadata"]["effective"])
        if phase == "forward":
            metadata["effective"]["Name"] = marker
        for step in range(steps):
            rows["activity_entries"].append(audit_row(166 + step, "metadata.updated", CONTROLLER.ADMIN, NATIVE_SESSION,
                "item", CONTROLLER.ITEM, 5 + 2 * step, revision=2 + step))
    capabilities = [{}, {"PlayableMediaTypes": ["Video"]}]
    ordinary = next(row for row in rows["sessions"] if row["id"] == SESSION)
    native = next(row for row in rows["sessions"] if row["id"] == NATIVE_SESSION)
    ordinary.update(last_seen_at=at(8), client_capabilities=copy.deepcopy(capabilities[-1]))
    rows["devices"][-1]["last_seen_at"] = at(8)
    if closed:
        for index, row in enumerate((ordinary, native)):
            row["revoked_at"] = at(9)
            rows["activity_entries"].append(audit_row(166 + steps + index, "session.revoked", row["user_id"], row["id"],
                "session", row["id"], 9, source="emby" if row["kind"] == "emby" else "native"))
    after["database"]["sequences"]["activity_entries_id_seq"] = {"last_value": 165 + steps + (2 if closed else 0), "is_called": True}
    after["database"]["metadata"]["captured_at"] = at(10)
    return {"before": before, "after": after, "profile": profile, "reservation": reservation, "browser": browser,
            "administrator": administrator, "phase": phase, "capabilities": capabilities, "authenticated": authenticated, "closed": closed}


class MetadataProjectionGuards(GuardTestCase):
    def test_name_override_preserves_independent_sort_name_and_other_values(self):
        automatic = automatic_values()
        original = copy.deepcopy(automatic)
        expected = expected_public_values()
        expected["Name"] = "M3e Client Movie [owned marker]"
        actual = CONTROLLER.metadata_value_projection(automatic, {"Name": expected["Name"]}, {})
        self.assertEqual(actual, expected)
        self.assertEqual(automatic, original)

    def test_override_precedence_does_not_rewrite_retained_lock_values(self):
        automatic = automatic_values()
        locked = {"Name": "Locked name", "Overview": "Locked overview."}
        original = copy.deepcopy(locked)
        expected = expected_public_values()
        expected.update(Name="Manual name", Overview="Locked overview.")
        self.assertEqual(CONTROLLER.metadata_value_projection(automatic, {"Name": "Manual name"}, locked), expected)
        self.assertEqual(locked, original)

    def test_null_collections_are_completed_only_in_public_projection(self):
        automatic = automatic_values()
        for name in ("ProviderIDs", "Genres", "Tags", "Studios", "People"):
            automatic[name] = None
        original = copy.deepcopy(automatic)
        self.assertEqual(CONTROLLER.metadata_value_projection(automatic, {}, {}), expected_public_values())
        self.assertEqual(automatic, original)

    def test_nullable_and_empty_object_database_projections_remain_distinct(self):
        effective = expected_public_values()
        self.assertIsNone(CONTROLLER.metadata_db_projection({"local_metadata": None}, {}, {}, effective))
        self.assertEqual(CONTROLLER.metadata_db_projection({"local_metadata": {}}, {}, {}, effective), {})
        changed = dict(effective, Name="Owned marker")
        self.assertEqual(CONTROLLER.metadata_db_projection({"local_metadata": None}, {"Name": "Owned marker"}, {}, changed),
                         {"Name": "Owned marker"})

    def test_name_only_sparse_projection_preserves_nulls_sort_name_and_unknown_numbers(self):
        local = {"Kind": "movie", "Name": "M3e Client Movie", "SortName": "", "Overview": "Original overview.",
                 "ProviderIDs": None, "Genres": None, "Tags": None, "Studios": None, "People": None,
                 "Opaque": {"LargeInteger": 9007199254740993, "Fraction": Decimal("1.000000000000000001"), "Absent": None}}
        item = {"local_metadata": local}
        original = copy.deepcopy(item)
        marker = "M3e Client Movie [owned marker]"
        effective = dict(expected_public_values(), Name=marker)
        expected = copy.deepcopy(local)
        expected["Name"] = marker
        actual = CONTROLLER.metadata_db_projection(item, {"Name": marker}, {}, effective)
        self.assertEqual(actual, expected)
        self.assertEqual(item, original)
        self.assertEqual(CONTROLLER.metadata_db_projection(item, {}, {}, expected_public_values()), local)

    def test_controlled_provider_ids_use_internal_spelling_without_expanding_other_fields(self):
        local = {"Kind": "movie", "ProviderIds": {"Old": "value"}, "Genres": None}
        effective = expected_public_values()
        effective["ProviderIds"] = {"Vendor": "new"}
        self.assertEqual(CONTROLLER.metadata_db_projection({"local_metadata": local},
            {"ProviderIds": {"Vendor": "new"}}, {}, effective),
            {"Kind": "movie", "ProviderIDs": {"Vendor": "new"}, "Genres": None})


class TargetAndReservationGuards(GuardTestCase):
    def test_exact_json_keeps_large_numbers_nulls_and_duplicate_rejection(self):
        value = {"integer": 9007199254740993, "decimal": Decimal("1.000000000000000001"), "null": None, "flag": False}
        self.assertEqual(CONTROLLER.canonical(value),
            b'{"decimal":1.000000000000000001,"flag":false,"integer":9007199254740993,"null":null}')
        self.assertEqual(CONTROLLER.decode(CONTROLLER.canonical(value)), value)
        for raw in (b'{"x":1,"x":2}', b'{"x":NaN}'):
            with self.subTest(raw=raw):
                self.reject(lambda: CONTROLLER.decode(raw))
        with self.assertRaises(ValueError):
            CONTROLLER.decode(b'{malformed')

    def test_revision_is_a_bounded_canonical_string(self):
        self.assertEqual(CONTROLLER.revision("1"), 1)
        self.assertEqual(CONTROLLER.revision("9223372036854775805"), 9223372036854775805)
        self.assertEqual(CONTROLLER.revision("9223372036854775807"), 9223372036854775807)
        for value in (1, True, Decimal("1"), "0", "01", "+1", "1.0", " 1", "9223372036854775808", None):
            with self.subTest(value=value):
                self.reject(lambda: CONTROLLER.revision(value))

    def test_profile_binds_the_complete_original_sparse_target(self):
        before = baseline_snapshot()
        self.assertEqual(len(before["database"]["tables"]), 35)
        original = copy.deepcopy(before)
        self.assertEqual(CONTROLLER.target_profile(before), profile_fixture(before))
        self.assertEqual(before, original)

    def test_profile_rejects_wrong_identity_revision_lock_or_resource_role(self):
        for defect in ("type", "library", "folder", "revision", "name_lock", "entity", "theme", "extra", "duplicate", "projection"):
            with self.subTest(defect=defect):
                before = baseline_snapshot()
                tables = before["database"]["tables"]
                item, metadata = tables["items"][0], tables["item_metadata_state"][0]
                if defect == "type": item["type"] = "Episode"
                elif defect == "library": item["library_id"] = "0" * 32
                elif defect == "folder": item["is_folder"] = True
                elif defect == "revision": metadata["revision"] = True
                elif defect == "name_lock": metadata["locked_values"]["Name"] = item["name"]
                elif defect == "entity": tables["item_entities"].append({"item_id": CONTROLLER.ITEM})
                elif defect in ("theme", "extra"):
                    tables["item_" + defect + "_resources"].append({"owner_item_id": CONTROLLER.ITEM, "resource_item_id": "9" * 32})
                elif defect == "duplicate": tables["items"].append(copy.deepcopy(item))
                else: metadata["effective"]["SortName"] = item["sort_name"]
                self.reject(lambda: CONTROLLER.target_profile(before))

    def test_profile_rejects_all_six_entity_sources_even_when_associations_are_empty(self):
        for key in ("Genres", "Tags", "Studios", "People", "Artists", "AlbumArtists"):
            with self.subTest(field=key):
                before = baseline_snapshot()
                before["database"]["tables"]["items"][0]["local_metadata"][key] = ["Unowned entity"]
                before["database"]["tables"]["item_metadata_state"][0]["effective"][key] = ["Unowned entity"]
                self.reject(lambda: CONTROLLER.target_profile(before))

    def test_public_document_and_reservation_preserve_exact_controls(self):
        profile = profile_fixture(baseline_snapshot())
        document = metadata_document(profile)
        original = copy.deepcopy(document)
        self.assertEqual(CONTROLLER.validate_metadata_document(document, profile), document)
        self.assertEqual(CONTROLLER.reserve_edit(document, profile), reservation_fixture(profile))
        self.assertEqual(document, original)
        profile["metadata"]["overrides"] = {"Overview": "Pinned overview."}
        profile["metadata"]["locked_values"] = {"OfficialRating": "PG"}
        profile["public"].update(Overview="Pinned overview.", OfficialRating="PG")
        profile["item"]["overview"] = "Pinned overview."
        profile["metadata"]["effective"].update(Overview="Pinned overview.", OfficialRating="PG")
        document = metadata_document(profile)
        reserved = CONTROLLER.reserve_edit(document, profile)
        self.assertEqual(reserved["forward_body"]["Overrides"], {"Overview": "Pinned overview.", "Name": reserved["marker_name"]})
        self.assertEqual(reserved["restore_body"], {"Revision": "2", "Overrides": {"Overview": "Pinned overview."}, "LockedFields": ["OfficialRating"]})
        self.assertEqual(document["LockedValues"], {"OfficialRating": "PG"})

    def test_public_document_rejects_wrong_scalar_types_controls_and_identity(self):
        profile = profile_fixture(baseline_snapshot())
        for key, value in (("Revision", 1), ("Revision", "01"), ("Overrides", None), ("LockedFields", None),
                           ("EditableFields", []), ("LastEditedBy", None), ("Unexpected", True)):
            with self.subTest(field=key):
                document = metadata_document(profile)
                document[key] = value
                self.reject(lambda: CONTROLLER.validate_metadata_document(document, profile))
        document = metadata_document(profile)
        document["Item"]["Id"] = "0" * 32
        self.reject(lambda: CONTROLLER.validate_metadata_document(document, profile))


class LedgerAndRestorationGuards(GuardTestCase):
    def test_complete_success_and_open_checkpoints_have_exact_owned_deltas(self):
        for phase, closed, audits in (("original", False, 2), ("forward", False, 3), ("restored", False, 4), ("restored", True, 6)):
            with self.subTest(phase=phase, closed=closed):
                fixture = ledger_fixture(phase, closed)
                original = copy.deepcopy(fixture)
                result = CONTROLLER.validate_ledger(**fixture)
                self.assertEqual(result["new_sessions"], 2)
                self.assertEqual(result["new_devices"], 1)
                self.assertEqual(result["new_audits"], audits)
                self.assertEqual(result["metadata_revision_delta"], {"original": 0, "forward": 1, "restored": 2}[phase])
                self.assertEqual(result["owned_sessions_closed"], closed)
                self.assertEqual(fixture, original)
        fixture = ledger_fixture()
        tables = fixture["after"]["database"]["tables"]
        self.assertEqual((len(tables["sessions"]), len(tables["devices"]), len(tables["activity_entries"])), (75, 63, 169))

    def test_foreign_old_rows_fail_even_when_all_final_counts_stay_correct(self):
        for table, field, value in (("sessions", "last_seen_at", at(1)), ("devices", "reported_name", "Changed old device"),
                                    ("activity_entries", "request_id", "changed"), ("users", "management_revision", 6),
                                    ("libraries", "name", "Changed library"), ("user_item_data", "play_count", 1)):
            with self.subTest(table=table):
                fixture = ledger_fixture()
                fixture["after"]["database"]["tables"][table][0][field] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_schema28_audit_columns_missing_fields_and_extra_new_columns_are_rejected(self):
        for table, key, value in (("activity_entries", "previous_revision", 0),
                                  ("activity_entries", "observation_fingerprint", ""),
                                  ("sessions", "unreviewed", None), ("devices", "unreviewed", None)):
            with self.subTest(table=table, field=key):
                fixture = ledger_fixture()
                fixture["after"]["database"]["tables"][table][-1][key] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))
        fixture = ledger_fixture()
        del fixture["after"]["database"]["tables"]["activity_entries"][-1]["request_id"]
        self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_metadata_audits_require_count_zero_exact_fields_and_native_credential(self):
        for key, value in (("affected_count", 1), ("affected_count", False), ("revision", Decimal("2")),
                           ("actor_credential_id", "f0" * 16), ("actor_id", CONTROLLER.B), ("source", "emby"),
                           ("request_id", "http-request-id"), ("changed_fields", ["Name"]),
                           ("changed_fields", ["Name", "Overrides", "SortName"])):
            with self.subTest(field=key, value=value):
                fixture = ledger_fixture()
                row = next(row for row in fixture["after"]["database"]["tables"]["activity_entries"] if row["action"] == "metadata.updated")
                row[key] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_sequence_shape_contiguous_ids_and_unrelated_sequences_are_exact(self):
        for name, key, value in (("activity_entries_id_seq", "last_value", 170),
                                 ("activity_entries_id_seq", "last_value", Decimal("169")),
                                 ("devices_id_seq", "is_called", 1), ("catalog_entities_id_seq", "last_value", 21)):
            with self.subTest(sequence=name, field=key):
                fixture = ledger_fixture()
                fixture["after"]["database"]["sequences"][name][key] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))
        for table in ("devices", "activity_entries"):
            with self.subTest(table=table):
                fixture = ledger_fixture()
                fixture["after"]["database"]["tables"][table][-1]["id"] += 100
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_uncalled_sequences_allocate_from_last_value_without_an_extra_increment(self):
        fixture = ledger_fixture()
        for snapshot in (fixture["before"],):
            snapshot["database"]["sequences"]["devices_id_seq"] = {"last_value": 64, "is_called": False}
            snapshot["database"]["sequences"]["activity_entries_id_seq"] = {"last_value": 164, "is_called": False}
        result = CONTROLLER.validate_ledger(**fixture)
        self.assertEqual(result["new_audits"], 6)

    def test_native_session_never_inherits_browser_touch_or_capabilities_allowance(self):
        for key, value in (("last_seen_at", at(8)), ("client_capabilities", {"PlayableMediaTypes": ["Video"]}),
                           ("device_registry_id", 64), ("expires_at", at(2 + 30 * 86400))):
            with self.subTest(field=key):
                fixture = ledger_fixture()
                row = next(row for row in fixture["after"]["database"]["tables"]["sessions"] if row["id"] == NATIVE_SESSION)
                row[key] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_browser_capabilities_device_identity_and_exact_closure_are_bound(self):
        fixture = ledger_fixture()
        fixture["capabilities"] = [{}]
        self.reject(lambda: CONTROLLER.validate_ledger(**fixture))
        for key, value in (("reported_device_id", "old-device-0"), ("revision", True), ("custom_name", "Unowned rename")):
            with self.subTest(field=key):
                fixture = ledger_fixture()
                fixture["after"]["database"]["tables"]["devices"][-1][key] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))
        fixture = ledger_fixture()
        fixture["after"]["database"]["tables"]["sessions"][-1]["revoked_at"] = None
        self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_sparse_metadata_and_all_other_target_fields_are_preserved(self):
        for table, field, value in (("items", "sort_name", "derived from marker"), ("items", "overview", "Unowned edit"),
                                    ("items", "local_metadata_hash", "0" * 64),
                                    ("item_metadata_state", "source_key", {}), ("item_metadata_state", "music_source", {"Name": "Unowned"}),
                                    ("item_metadata_state", "locked_values", {"Name": "Unowned"})):
            with self.subTest(table=table, field=field):
                fixture = ledger_fixture()
                fixture["after"]["database"]["tables"][table][0][field] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))
        fixture = ledger_fixture()
        fixture["after"]["database"]["tables"]["item_metadata_state"][0]["effective"] = expected_public_values()
        self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_missing_ack_original_is_unresolved_and_owned_forward_restores_only_once(self):
        self.assertEqual(CONTROLLER.restore_decision(False, False, False, "original", False, False), "not_required")
        for acknowledged in (False, True):
            self.assertEqual(CONTROLLER.restore_decision(True, acknowledged, False, "original", False, False), "unresolved")
        self.assertEqual(CONTROLLER.restore_decision(True, False, False, "forward", True, False), "restore_once")
        self.assertEqual(CONTROLLER.restore_decision(True, False, True, "forward", True, False), "conflict")
        self.assertEqual(CONTROLLER.restore_decision(True, False, True, "restored", True, True), "restored")
        self.assertEqual(CONTROLLER.restore_decision(True, False, False, "foreign", False, False), "conflict")

    def test_metadata_classification_requires_both_public_values_and_owned_audits(self):
        for phase in ("original", "forward", "restored"):
            with self.subTest(phase=phase):
                fixture = ledger_fixture(phase, False)
                edited = None if phase == "original" else at(4 if phase == "forward" else 6)
                document = metadata_document(fixture["profile"], phase, fixture["reservation"]["marker_name"], edited)
                self.assertEqual(CONTROLLER.classify_metadata(document, fixture["after"], fixture["profile"],
                    fixture["reservation"], NATIVE_SESSION), phase)
        fixture = ledger_fixture("forward", False)
        document = metadata_document(fixture["profile"], "forward", fixture["reservation"]["marker_name"], at(4))
        audit = next(row for row in fixture["after"]["database"]["tables"]["activity_entries"] if row["action"] == "metadata.updated")
        audit["actor_credential_id"] = "f0" * 16
        self.assertEqual(CONTROLLER.classify_metadata(document, fixture["after"], fixture["profile"], fixture["reservation"], NATIVE_SESSION), "foreign")


PAGE_ROUTE = "/synthetic-movies-list"
DOCUMENT = "synthetic-document-1"
CONNECTION = "synthetic-socket-1"
CHILD = {"pid": 2002, "start_ticks": 10002, "boot_id": "a" * 32}
OUTER = {"pid": 2001, "start_ticks": 10001, "boot_id": "a" * 32}


def digest(value):
    return CONTROLLER.sha(CONTROLLER.canonical(value))


def input_fixture():
    return {"source_closure": {str(CONTROLLER.TOOL / name): "7" * 64 for name in CONTROLLER.JS_NAMES},
        "controller": dict(OUTER, unit=CONTROLLER.CONTROLLER_UNIT),
        "candidate": {"server_id": CONTROLLER.SERVER}, "target": profile_fixture(baseline_snapshot())["target"]}


def private_fixture(input_record, input_sha=INPUT_SHA):
    return {"marker": "goby-client-library-home-session-v1", "version": 1, "input_sha256": input_sha,
        "source_closure_sha256": digest(input_record["source_closure"]), "node_process": copy.deepcopy(CHILD),
        "controller": copy.deepcopy(input_record["controller"]), "proof": ordinary_proof(), "token": TOKEN}


def dom_fixture(name, passed=True):
    return {"target_id": CONTROLLER.ITEM, "expected_name": name, "media_inactive": True, "route": PAGE_ROUTE,
        "document_id": DOCUMENT, "passed": passed, "identity_proven": passed, "visible_target_cards": 1,
        "target_title_count": int(passed), "forbidden_title_count": int(not passed)}


def read_fixture(name, start=22120, request_sequence=14, identifier=1, index=0):
    query = [["ParentId", CONTROLLER.LIBRARY]]
    route = "/Users/" + CONTROLLER.B + "/Items"
    shape = digest([route, query])
    target = {"Id": CONTROLLER.ITEM, "Name": name, "Type": "Movie"}
    body = CONTROLLER.canonical({"Items": [target], "TotalRecordCount": 1})
    transfer = {"id": identifier, "kind": "items", "route": route, "query": query, "shape_sha256": shape,
        "token_sha256": TOKEN_SHA, "request_sha256": "1" * 64, "completed": True, "status": 200,
        "terminal_status": 200, "request_elapsed_ms": start + 10, "finished_elapsed_ms": start + 80,
        "projection": {"target": target, "count": 1, "body_sha256": CONTROLLER.sha(body), "body_bytes": len(body)}}
    frame = {"index": index, "token_sha256": TOKEN_SHA, "request_sha256": "1" * 64, "shape_sha256": shape,
        "finished": True, "failed": False, "status": 200, "content_type": "application/json",
        "from_service_worker": False, "source": "page", "main_frame": True, "request_sequence": request_sequence,
        "request_elapsed_ms": start, "finished_elapsed_ms": start + 100, "document_id": DOCUMENT, "page_route": PAGE_ROUTE}
    return {"physical": transfer, "frame": frame, "unambiguous": True, "complete": True}


def boundary_fixture(name="forward"):
    return {"name": name, "route": PAGE_ROUTE, "document_id": DOCUMENT, "connection_id": CONNECTION,
        "token_sha256": TOKEN_SHA, "started_sequence": 30 if name == "forward" else 1000,
        "started_elapsed_ms": 22000 if name == "forward" else 144000, "websocket_seen": 1, "websocket_opened": 1}


def window_fixture(name="forward", commit=None, control_sha="2" * 64):
    reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
    expected = reservation["marker_name" if name == "forward" else "original_name"]
    boundary = boundary_fixture(name)
    start, sequence = boundary["started_elapsed_ms"], boundary["started_sequence"]
    boundary.update(response_completed_elapsed_ms=start + 500, end_elapsed_ms=start + 120500,
                    completed_elapsed_ms=start + 120500, duration_ms=120000)
    data = {key: [CONTROLLER.ITEM] if key == "ItemsUpdated" else [] for key in
            ("ItemsAdded", "ItemsRemoved", "ItemsUpdated", "FoldersAddedTo", "FoldersRemovedFrom", "CollectionFolders")}
    data["IsEmpty"] = False
    message = {"MessageType": "LibraryChanged", "MessageId": "synthetic-change-" + name, "Data": data}
    raw = CONTROLLER.canonical(message)
    message.update(body_sha256=CONTROLLER.sha(raw), body_bytes=len(raw), projection_sha256=digest(message))
    upstream = {"sequence": sequence + 2, "elapsed_ms": start + 100, "connection_id": CONNECTION,
        "token_sha256": TOKEN_SHA, "complete": True, "received": True, "forwarded": True, "message": message}
    received = dict(upstream, sequence=sequence + 3, elapsed_ms=start + 110, document_id=DOCUMENT, route=PAGE_ROUTE)
    pair = read_fixture(expected, start=start + 120, request_sequence=sequence + 4,
                        identifier=1 if name == "forward" else 2)
    samples = [{"sequence": sequence + 1, "started_elapsed_ms": start, "elapsed_ms": start + 10,
                "observation": dom_fixture(expected, False)}]
    samples.extend({"sequence": sequence + 20 + index, "started_elapsed_ms": moment, "elapsed_ms": moment + 10,
        "observation": dom_fixture(expected)} for index, moment in enumerate(range(start + 500, start + 120001, 500)))
    return {"name": name, "control_sha256": control_sha, "boundary": boundary,
        "commit": commit or {"revision": "2" if name == "forward" else "3", "write_completed_at": at(12),
                             "native_result_sha256": "3" * 64, "readback_sha256": "4" * 64},
        "events": {"physical": [upstream], "browser": [received]},
        "http": {"physical": [pair["physical"]], "frames": [pair["frame"]], "pairs": [pair]},
        "dom": samples, "actions": [], "lifecycle": [], "result": "passed",
        "outcome": "automatic_http_and_visible_title_observed",
        "proof": {"message_id": message["MessageId"], "physical_exchange_id": pair["physical"]["id"],
                  "frame_request_index": 0, "first_dom_sequence": sequence + 20,
                  "last_dom_sequence": samples[-1]["sequence"], "identity_bound": True, "ordered": True}}


def stage_observation(name, input_record, control=None):
    original_name = input_record["target"]["name"]
    if name == "discovery":
        pair = read_fixture(original_name, start=100, request_sequence=1)
        return {"home": {"passed": True}, "dom": dom_fixture(original_name), "reads": [pair],
            "query_allowlist": [{key: copy.deepcopy(pair["physical"][key]) for key in ("kind", "route", "query", "shape_sha256")}],
            "socket": {"token_sha256": TOKEN_SHA, "connection_id": CONNECTION, "opened": 1, "seen": 1}}
    if name == "armed":
        samples = [{"sequence": index, "started_elapsed_ms": moment, "elapsed_ms": moment + 10,
            "observation": dom_fixture(original_name)} for index, moment in enumerate(range(1000, 21000, 1000), 1)]
        return {"boundary": boundary_fixture(), "quiet": {"passed": True, "duration_ms": 20000, "catalog_requests": 0,
            "library_changed_messages": 0, "started_elapsed_ms": 0, "completed_elapsed_ms": 20000,
            "samples": samples[:-1] + [dict(samples[-1], started_elapsed_ms=19900, elapsed_ms=20000)]}}
    window_name = "forward" if name == "restore-armed" else "restored"
    window = window_fixture(window_name, commit=copy.deepcopy(control["value"]["commit"]) if control else None,
                            control_sha=control["sha256"] if control else "2" * 64)
    return {"forward": window, "boundary": boundary_fixture("restored")} if name == "restore-armed" else {"restored": window}


def stage_fixture(name, input_record=None, input_sha=INPUT_SHA, previous=None, session_record=None, control=None):
    input_record = input_record or input_fixture()
    return {"marker": "goby-client-library-changed-stage-v1", "version": 1, "input_sha256": input_sha,
        "source_closure_sha256": digest(input_record["source_closure"]), "controller": copy.deepcopy(input_record["controller"]),
        "node_process": copy.deepcopy(CHILD), "name": name, "token_sha256": TOKEN_SHA,
        "session_private": session_record or {"path": str(CONTROLLER.BROWSER_ROOT / "session-private.json"), "sha256": "5" * 64},
        "previous_control_sha256": previous, "observation": stage_observation(name, input_record, control)}


class LoginAndStageGuards(GuardTestCase):
    def test_one_browser_login_and_private_token_require_exact_new_process_and_rows(self):
        fixture = ledger_fixture("original", False)
        value, private = input_fixture(), private_fixture(input_fixture())
        CONTROLLER.validate_login(ordinary_proof())
        token, row = CONTROLLER.validate_private_session(private, value, INPUT_SHA, CHILD, fixture["before"], fixture["after"])
        self.assertEqual((token, row["id"]), (TOKEN, SESSION))
        for key, replacement in (("version", True), ("token", NATIVE_TOKEN), ("input_sha256", "0" * 64),
                                  ("node_process", dict(CHILD, pid=3000)), ("controller", {})):
            changed = copy.deepcopy(private)
            changed[key] = replacement
            self.reject(lambda: CONTROLLER.validate_private_session(changed, value, INPUT_SHA, CHILD, fixture["before"], fixture["after"]))
        old = copy.deepcopy(fixture["before"])
        old["database"]["tables"]["sessions"][0]["token_hash"] = "\\x" + TOKEN_SHA
        self.reject(lambda: CONTROLLER.owned_session(old, fixture["after"], ordinary_proof()))

    def test_all_four_stage_shapes_bind_actor_control_process_and_seven_sources(self):
        value = input_fixture()
        for name in CONTROLLER.STAGES:
            stage = stage_fixture(name, value, previous="6" * 64)
            self.assertEqual(CONTROLLER.validate_stage(stage, value, INPUT_SHA, CHILD, name, "6" * 64, ordinary_proof()), stage["observation"])
            for key, replacement in (("version", True), ("input_sha256", "0" * 64), ("token_sha256", NATIVE_TOKEN_SHA),
                                     ("previous_control_sha256", None), ("node_process", dict(CHILD, pid=3000))):
                changed = copy.deepcopy(stage)
                changed[key] = replacement
                self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, name, "6" * 64, ordinary_proof()))
        manifest = {"marker": "goby-client-library-changed-sources-v1", "files": value["source_closure"]}
        self.assertEqual(CONTROLLER.validate_source_manifest(manifest, CONTROLLER.TOOL / CONTROLLER.JS_NAMES[0]), value["source_closure"])
        manifest["files"] = dict(manifest["files"], **{str(CONTROLLER.TOOL / "extra.py"): "8" * 64})
        self.reject(lambda: CONTROLLER.validate_source_manifest(manifest, CONTROLLER.TOOL / CONTROLLER.JS_NAMES[0]))

    def test_discovery_allowlist_is_exactly_observed_and_quiet_requires_full_coverage(self):
        value = input_fixture()
        changed = stage_fixture("discovery", value)
        shape = changed["observation"]["query_allowlist"][0]
        shape["query"].append(["Limit", "20"])
        shape["shape_sha256"] = digest([shape["route"], shape["query"]])
        self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, "discovery", None, ordinary_proof()))
        for key, replacement in (("duration_ms", 19999), ("catalog_requests", True), ("library_changed_messages", 1),
                                 ("samples", []), ("completed_elapsed_ms", 19000)):
            changed = stage_fixture("armed", value)
            changed["observation"]["quiet"][key] = replacement
            self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, "armed", None, ordinary_proof()))


class WindowAndIntentGuards(GuardTestCase):
    def test_complete_windows_accept_delivery_before_ack_and_zero_frame_index(self):
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        for name in ("forward", "restored"):
            window = window_fixture(name)
            result = CONTROLLER.window_evidence(window, input_fixture(), reservation)
            self.assertEqual(result, {key: window[key] for key in ("result", "outcome", "proof")})
            self.assertLess(window["events"]["browser"][0]["elapsed_ms"], window["boundary"]["response_completed_elapsed_ms"])

    def test_catalog_pairing_rejects_credentials_unbounded_projection_and_ambiguous_frames(self):
        pair = read_fixture("M3e Client Movie")
        self.assertEqual(CONTROLLER.pair_reads([pair["physical"]], [pair["frame"]]), [pair])
        for query in ([["ParentId", CONTROLLER.LIBRARY], ["Api_Key", "secret"]], [["Ids", CONTROLLER.ITEM], ["IDS", CONTROLLER.ITEM]],
                      [["ParentId", CONTROLLER.LIBRARY], ["Limit", "129"]], [["UserId", CONTROLLER.ADMIN], ["Ids", CONTROLLER.ITEM]]):
            changed = copy.deepcopy(pair["physical"])
            changed["query"] = query
            changed["shape_sha256"] = digest([changed["route"], query])
            self.reject(lambda: CONTROLLER.pair_reads([changed], [pair["frame"]]))
        for key, value in (("count", True), ("count", 129), ("body_bytes", 0), ("body_bytes", 2 ** 21 + 1), ("body_sha256", "bad")):
            changed = copy.deepcopy(pair["physical"])
            changed["projection"][key] = value
            self.reject(lambda: CONTROLLER.pair_reads([changed], [pair["frame"]]))
        frames = [pair["frame"], dict(pair["frame"], index=1)]
        self.assertTrue(all(not row["complete"] and row["physical"] is None for row in CONTROLLER.pair_reads([pair["physical"]], frames)))
        for failed in (None, 0, True):
            self.assertEqual(CONTROLLER.pair_reads([pair["physical"]], [dict(pair["frame"], failed=failed)]), [])

    def test_missing_websocket_http_or_persistent_dom_never_passes(self):
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        for layer, outcome in (("websocket", "websocket_not_observed_within_window"),
                               ("http", "automatic_http_not_observed_within_window"), ("dom", "dom_update_not_observed_within_window")):
            window = window_fixture()
            if layer == "websocket":
                window["events"]["browser"] = []
            elif layer == "http":
                window["http"]["frames"][0]["request_sequence"] = window["events"]["browser"][0]["sequence"]
            else:
                window["dom"][-1]["observation"]["passed"] = False
            self.assertEqual(CONTROLLER.window_evidence(window, input_fixture(), reservation),
                             {"result": "not_observed_within_window", "outcome": outcome, "proof": None})

    def test_full_window_rejects_intervention_wrong_public_payload_gaps_and_short_duration(self):
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        for change in (lambda w: w.update(actions=[{"name": "reload"}]),
                       lambda w: w["boundary"].update(duration_ms=119999),
                       lambda w: w["events"]["browser"][0]["message"].update(MessageType="UserDataChanged"),
                       lambda w: w["events"]["physical"].append(copy.deepcopy(w["events"]["physical"][0])),
                       lambda w: w["dom"].__delitem__(slice(2, 12)),
                       lambda w: w["http"]["pairs"][0].update(complete=False)):
            window = window_fixture()
            change(window)
            self.reject(lambda: CONTROLLER.window_evidence(window, input_fixture(), reservation))

    def test_native_intents_bind_methods_routes_order_and_never_replay(self):
        fence = CONTROLLER.NativeFence()
        self.reject(lambda: fence.reserve("forward", "PUT", fence.PATH))
        self.reject(lambda: fence.reserve("login", "POST", "/emby/Users/AuthenticateByName"))
        for label in ("login", "original", "forward", "forward-readback", "pre-restore", "restore", "restore-readback",
                      "reconcile-one", "reconcile-two", "logout", "exact401"):
            fence.reserve(label, *fence.REQUESTS[label])
            self.reject(lambda: fence.reserve(label, *fence.REQUESTS[label]))
        self.assertEqual(len(fence.reserved), 11)
        self.assertEqual(sum(fence.REQUESTS[name][0] == "PUT" for name in fence.reserved), 2)
        fresh = CONTROLLER.NativeFence()
        fresh.reserve("login", *fresh.REQUESTS["login"])
        self.reject(lambda: fresh.reserve("restore", *fresh.REQUESTS["restore"]))
        self.reject(lambda: fresh.reserve("exact401", *fresh.REQUESTS["exact401"]))
        fresh.reserve("logout", *fresh.REQUESTS["logout"])
        self.reject(lambda: fresh.reserve("original", *fresh.REQUESTS["original"]))
        fresh.reserved = ["synthetic-consumed"] * 11 + ["login"]
        self.reject(lambda: fresh.reserve("original", *fresh.REQUESTS["original"]))


class MemoryStream:
    def __init__(self, filesystem, descriptor, mode):
        self.fs, self.descriptor, self.mode, self.position = filesystem, descriptor, mode, 0

    def __enter__(self):
        return self

    def __exit__(self, *_arguments):
        self.fs.close(self.descriptor)

    def fileno(self):
        return self.descriptor

    def flush(self):
        self.fs.events.append(("flush", self.fs.descriptors[self.descriptor]))

    def write(self, raw):
        if self.mode != "wb" or not isinstance(raw, bytes):
            raise AssertionError("The memory writer received an unsupported operation.")
        entry = self.fs.entry_for_fd(self.descriptor)
        entry["raw"] = entry["raw"][:self.position] + raw + entry["raw"][self.position + len(raw):]
        self.position += len(raw)
        return len(raw)

    def read(self, limit):
        if self.mode != "rb" or type(limit) is not int or limit < 0:
            raise AssertionError("The memory reader requires a bounded binary read.")
        raw = self.fs.entry_for_fd(self.descriptor)["raw"][self.position:self.position + limit]
        self.position += len(raw)
        return raw


class MemoryFS:
    """Model inodes, exclusive creation, dirfds and hard links for actual writers."""

    def __init__(self):
        self.entries, self.descriptors, self.events, self.failures = {}, {}, [], set()
        self.next_inode, self.next_fd = 100, 10
        self.seed_directory(CONTROLLER.WORK)
        self.seed(MemoryPath("/proc/self/cgroup"), ("0::/system.slice/" + CONTROLLER.CONTROLLER_UNIT + "\n").encode())

    def seed_directory(self, path, mode=0o755):
        path = MemoryPath(str(path))
        for parent in reversed((path, *path.parents)):
            if str(parent) not in self.entries:
                self.next_inode += 1
                self.entries[str(parent)] = {"raw": None, "inode": self.next_inode, "mode": mode if parent == path else 0o755}

    def seed(self, path, value, mode=0o600):
        path = MemoryPath(str(path))
        self.seed_directory(path.parent)
        if str(path) in self.entries:
            raise FileExistsError(str(path))
        self.next_inode += 1
        raw = value if isinstance(value, bytes) else CONTROLLER.canonical(value) + b"\n"
        self.entries[str(path)] = {"raw": raw, "inode": self.next_inode, "mode": mode}
        return {"path": str(path), "sha256": CONTROLLER.sha(raw)}

    def absolute(self, path, dir_fd=None):
        path = MemoryPath(str(path))
        if path.is_absolute():
            return str(path)
        if dir_fd not in self.descriptors:
            raise AssertionError("A relative memory operation has no owned directory descriptor.")
        return str(MemoryPath(self.descriptors[dir_fd]) / path)

    def trip(self, operation, path):
        self.events.append((operation, path))
        if (operation, MemoryPath(path).name) in self.failures:
            raise OSError("Synthetic " + operation + " failure.")

    def lstat(self, path):
        name = str(path)
        if name not in self.entries:
            raise FileNotFoundError(name)
        entry = self.entries[name]
        info = file_information(inode=entry["inode"], links=sum(value is entry for value in self.entries.values()),
            mode=entry["mode"], directory=entry["raw"] is None, size=0 if entry["raw"] is None else len(entry["raw"]))
        for key, value in entry.get("stat_overrides", {}).items():
            setattr(info, key, value)
        return info

    def mkdir(self, path, mode=0o777, **options):
        if options or str(path) in self.entries or str(path.parent) not in self.entries:
            raise FileExistsError(str(path))
        self.seed_directory(path, mode)

    def open(self, path, flags, mode=0o777, *, dir_fd=None):
        name = self.absolute(path, dir_fd)
        self.trip("open", name)
        if flags & os.O_CREAT:
            if not flags & os.O_EXCL or not flags & os.O_NOFOLLOW or mode != 0o600:
                raise AssertionError("The actual writer lost exclusive, private, no-follow creation.")
            self.seed(MemoryPath(name), b"", mode)
        if name not in self.entries:
            raise FileNotFoundError(name)
        if flags & os.O_DIRECTORY and self.entries[name]["raw"] is not None:
            raise NotADirectoryError(name)
        self.next_fd += 1
        self.descriptors[self.next_fd] = name
        return self.next_fd

    def entry_for_fd(self, descriptor):
        return self.entries[self.descriptors[descriptor]]

    def close(self, descriptor):
        name = self.descriptors.pop(descriptor)
        self.events.append(("close", name))

    def fdopen(self, descriptor, mode):
        return MemoryStream(self, descriptor, mode)

    def fsync(self, descriptor):
        self.events.append(("fsync", self.descriptors[descriptor]))

    def link(self, source, target, *, src_dir_fd, dst_dir_fd, follow_symlinks):
        if follow_symlinks is not False:
            raise AssertionError("IPC publication must not follow symbolic links.")
        source, target = self.absolute(source, src_dir_fd), self.absolute(target, dst_dir_fd)
        self.trip("link", target)
        if target in self.entries:
            raise FileExistsError(target)
        self.entries[target] = self.entries[source]

    def unlink(self, path, *, dir_fd):
        name = self.absolute(path, dir_fd)
        self.trip("unlink", name)
        del self.entries[name]

    def read_text(self, path, **options):
        if options:
            raise AssertionError("An unexpected text-read option escaped the adapter.")
        return self.entries[str(path)]["raw"].decode("utf-8")

    def json(self, name):
        path = MemoryPath(name) if str(name).startswith("/") else CONTROLLER.ROOT / name
        return memory_json_decode(self.entries[str(path)]["raw"])

    def install(self, case):
        for name, function in (("open", self.open), ("close", self.close), ("fdopen", self.fdopen), ("fsync", self.fsync),
                               ("link", self.link), ("unlink", self.unlink), ("fstat", lambda fd: self.lstat(self.descriptors[fd])),
                               ("umask", lambda _mode: 0o077)):
            case.enterContext(patch.object(CONTROLLER.os, name, function))
        case.enterContext(patch.object(CONTROLLER.os.path, "lexists", lambda path: str(path) in self.entries))
        for name, function in (("lstat", lambda path: self.lstat(path)), ("mkdir", lambda path, **kw: self.mkdir(path, **kw)),
                               ("read_text", lambda path, **kw: self.read_text(path, **kw)),
                               ("exists", lambda path: str(path) in self.entries)):
            case.enterContext(patch.object(MemoryPath, name, function, create=True))


class PrimaryFactGuards(GuardTestCase):
    def test_actual_primary_fact_projects_stat_tuples_to_exact_json_arrays(self):
        filesystem = MemoryFS()
        filesystem.install(self)
        hash_facts, file_bytes = {}, {}
        for filename, expected_hash in CONTROLLER.PRIMARY_FILES.items():
            raw = ("synthetic pinned primary file: " + filename).encode()
            file_bytes[filename] = raw
            hash_facts[raw] = expected_hash
            filesystem.seed(MemoryPath(filename), raw, 0o755 if filename == "/opt/goby-dev/goby" else 0o600)
        process = MemoryPath("/proc") / str(CONTROLLER.PRIMARY_PROCESS["pid"])
        filesystem.seed_directory(process)
        filesystem.entries[str(process)]["stat_overrides"] = {"st_uid": 995}
        filesystem.seed(process / "exe", file_bytes["/opt/goby-dev/goby"], 0o755)
        filesystem.seed(process / "cgroup", ("0::/system.slice/" + CONTROLLER.PRIMARY_UNIT + "\n").encode())
        command, environment = b"synthetic primary command\x00", b"SYNTHETIC_PRIMARY_ENV=private\x00"
        filesystem.seed(process / "cmdline", command)
        filesystem.seed(process / "environ", environment)
        hash_facts.update({
            command: "c2c8b1f234839029e4dcd80ada8d765fa6d4ebcb21be2f4b72665945d9458d6e",
            environment: "cfa0a59530f5e302b8ea23bb011823d7e68126be05e8b19c6360e450cbee9186"})
        original_sha = CONTROLLER.sha
        self.enterContext(patch.object(CONTROLLER, "sha", lambda raw: hash_facts[raw] if raw in hash_facts else original_sha(raw)))
        self.enterContext(patch.object(MemoryPath, "stat", lambda path: filesystem.lstat(path), create=True))
        self.enterContext(patch.object(MemoryPath, "read_bytes", lambda path: filesystem.entries[str(path)]["raw"], create=True))
        def executable_link(path):
            self.assertEqual(path, process / "exe")
            return "/opt/goby-dev/goby"
        readlink = Mock(side_effect=executable_link)
        self.enterContext(patch.object(CONTROLLER.os, "readlink", readlink))
        properties = {"MainPID": str(CONTROLLER.PRIMARY_PROCESS["pid"]), "InvocationID": CONTROLLER.PRIMARY_INVOCATION,
            "ActiveState": "active", "SubState": "running", "ControlGroup": "/system.slice/" + CONTROLLER.PRIMARY_UNIT,
            "User": "goby", "Group": "goby"}
        def service_properties(arguments, **options):
            self.assertEqual(arguments[:4], ["/usr/bin/systemctl", "show", CONTROLLER.PRIMARY_UNIT, "--no-pager"])
            self.assertEqual(len(arguments), 5)
            self.assertTrue(arguments[4].startswith("--property="))
            self.assertEqual(options["timeout"], 10)
            self.assertTrue(options["text"])
            return types.SimpleNamespace(returncode=0, stdout="\n".join(key + "=" + value for key, value in properties.items()), stderr="")
        commands = Mock(side_effect=service_properties)
        self.enterContext(patch.object(CONTROLLER.subprocess, "run", commands))
        process_identity = Mock(side_effect=lambda pid: copy.deepcopy(CONTROLLER.PRIMARY_PROCESS) if
            pid == CONTROLLER.PRIMARY_PROCESS["pid"] else None)
        run = CONTROLLER.Run(types.SimpleNamespace(check_only=True))
        run.op = types.SimpleNamespace(process_identity=process_identity)
        first, second = run.primary_fact(), run.primary_fact()
        self.assertEqual(first["properties"], properties)
        self.assertEqual(first["process"], CONTROLLER.PRIMARY_PROCESS)
        self.assertEqual(set(first["files"]), set(CONTROLLER.PRIMARY_FILES))
        for filename, expected_hash in CONTROLLER.PRIMARY_FILES.items():
            raw_identity = CONTROLLER.identity(filesystem.lstat(MemoryPath(filename)))
            self.assertIs(type(raw_identity), tuple)
            self.assertIs(type(first["files"][filename]["identity"]), list)
            self.assertEqual(first["files"][filename], {"sha256": expected_hash, "identity": list(raw_identity)})
        self.assertEqual(CONTROLLER.decode(CONTROLLER.canonical(first)), first)
        self.assertTrue(CONTROLLER.same(first, second))
        self.reject(lambda: CONTROLLER.canonical(CONTROLLER.identity(filesystem.lstat(MemoryPath("/opt/goby-dev/goby")))))
        self.assertEqual(commands.call_count, 2)
        self.assertEqual(readlink.call_count, 2)
        self.assertEqual(process_identity.call_args_list, [call(CONTROLLER.PRIMARY_PROCESS["pid"])] * 4)
        self.assertEqual(filesystem.descriptors, {})


class PublicationGuards(GuardTestCase):
    def prepare_memory(self):
        filesystem = MemoryFS()
        filesystem.install(self)
        filesystem.seed_directory(CONTROLLER.ROOT, 0o700)
        filesystem.seed_directory(CONTROLLER.BROWSER_ROOT, 0o700)
        run = CONTROLLER.Run(types.SimpleNamespace(check_only=False))
        run.root_fd = filesystem.open(CONTROLLER.ROOT, os.O_RDONLY | os.O_DIRECTORY)
        self.enterContext(patch.object(CONTROLLER.time, "monotonic", lambda: 50))
        return filesystem, run

    def test_actual_safe_filename_writer_and_nonoverwriting_hardlink_publication(self):
        filesystem, run = self.prepare_memory()
        for name in ("control-reserved.json", "control-forward.json", "control-restored.json", "control-close.json"):
            record = run.save(name, {"name": name}, ipc=True)
            self.assertEqual(run.ipc_read(CONTROLLER.ROOT / name), dict(record, value={"name": name}))
            self.assertNotIn(str(CONTROLLER.ROOT / (name + ".pending")), filesystem.entries)
            self.assertEqual(filesystem.lstat(CONTROLLER.ROOT / name).st_nlink, 1)
            with self.assertRaises(FileExistsError):
                run.save(name, {"replacement": True}, ipc=True)
            self.assertEqual(filesystem.json(name), {"name": name})
        for name in ("stop_requested.json", "../escaped.json", "private/cookie.json", "UPPER.json", "control-forward.json.pending"):
            self.reject(lambda: run.save(name, {}))

    def test_actual_ipc_reader_missing_pending_linked_and_completed_states(self):
        filesystem, run = self.prepare_memory()
        path = CONTROLLER.BROWSER_ROOT / "stage-armed.json"
        self.assertIsNone(run.ipc_read(path))
        pending = path.with_name(path.name + ".pending")
        filesystem.seed(pending, {"synthetic": True})
        self.assertIsNone(run.ipc_read(path))
        filesystem.entries[str(path)] = filesystem.entries[str(pending)]
        self.assertIsNone(run.ipc_read(path))
        del filesystem.entries[str(pending)]
        self.assertEqual(run.ipc_read(path)["value"], {"synthetic": True})

    def test_pending_collision_foreign_modes_extra_links_and_expired_publication_are_rejected(self):
        filesystem, run = self.prepare_memory()
        path = CONTROLLER.BROWSER_ROOT / "stage-armed.json"
        pending = path.with_name(path.name + ".pending")
        filesystem.seed(path, {})
        filesystem.seed(pending, {})
        self.reject(lambda: run.ipc_read(path))
        del filesystem.entries[str(path)]
        run.publications[str(path)] = 44
        self.reject(lambda: run.ipc_read(path))
        run.publications.clear()
        for key, value in (("st_uid", 1000), ("st_gid", 1000), ("st_nlink", 3),
                           ("st_mode", stat.S_IFREG | 0o644), ("st_mode", stat.S_IFDIR | 0o600)):
            filesystem.entries[str(pending)]["stat_overrides"] = {key: value}
            self.reject(lambda: run.ipc_read(path))

    def test_control_stage_cannot_advance_when_publication_fails(self):
        filesystem, run = self.prepare_memory()
        run.input, run.input_sha, run.child = input_fixture(), INPUT_SHA, copy.deepcopy(CHILD)
        run.js_sources = run.input["source_closure"]
        run.reservation = reservation_fixture(profile_fixture(baseline_snapshot()))
        run.stages = [{"name": "discovery", "sha256": "6" * 64}]
        filesystem.failures.add(("link", "control-reserved.json"))
        with self.assertRaises(OSError):
            run.publish_control("reserved")
        self.assertEqual(run.controls, [])
        self.assertNotIn("control-reserved.json", run.records)
        self.assertIn(str(CONTROLLER.ROOT / "control-reserved.json.pending"), filesystem.entries)
        self.reject(lambda: run.publish_control("forward"))


class FixedDateTime(datetime.datetime):
    @classmethod
    def now(cls, tz=None):
        return cls(2026, 9, 12, 6, 0, 12, tzinfo=tz)


class MemoryResponse:
    def __init__(self, raw, status=200, headers=None, read_error=None):
        self.raw, self.status, self.headers, self.read_error = raw, status, headers or {}, read_error
        self.read_limits = []

    def getheaders(self):
        return list(self.headers.items())

    def getheader(self, name, default=None):
        return next((value for key, value in self.headers.items() if key.lower() == name.lower()), default)

    def read(self, limit):
        self.read_limits.append(limit)
        if self.read_error:
            raise self.read_error
        return self.raw[:limit]


class MemoryConnection:
    def __init__(self, environment):
        self.environment, self.response, self.closed = environment, None, False

    def request(self, method, path, body, headers):
        self.response = self.environment.exchange(method, path, body, headers)

    def getresponse(self):
        return self.response

    def close(self):
        self.closed = True


class MemoryRunEnvironment:
    """Supply external facts while keeping controller orchestration unmodified."""

    def __init__(self, case, fault=None):
        self.case, self.fault = case, fault
        self.fs = MemoryFS()
        self.fs.install(case)
        self.connections, self.exchanges, self.lifecycle = [], [], []
        self.browser_created = self.native_created = self.browser_revoked = self.native_revoked = False
        self.metadata_phase = "original"
        self.stage_hook = None
        self.args = types.SimpleNamespace(check_only=False, node=MemoryPath("/usr/bin/node"), node_sha256="8" * 64,
            driver=CONTROLLER.TOOL / CONTROLLER.JS_NAMES[0])
        case.enterContext(patch.object(CONTROLLER.time, "monotonic", lambda: 50))
        case.enterContext(patch.object(CONTROLLER, "dt", types.SimpleNamespace(datetime=FixedDateTime,
            timezone=datetime.timezone, timedelta=datetime.timedelta)))
        case.enterContext(patch.object(CONTROLLER, "proc_identity", lambda _pid: copy.deepcopy(OUTER)))
        for name, function in (("getsignal", lambda _signal: 0), ("signal", lambda *_args: 0),
                               ("setitimer", lambda *_args: (0, 0))):
            case.enterContext(patch.object(CONTROLLER.signal, name, function))
        case.enterContext(patch.object(CONTROLLER.http.client, "HTTPConnection", self.connection))
        self.run = CONTROLLER.Run(self.args)
        for name, function in (("load", self.load), ("launch", self.launch), ("worker_live", self.worker_live),
                               ("properties", self.properties), ("primary_fact", lambda: {"synthetic_primary": "unchanged"}),
                               ("await_worker", self.await_worker), ("check", self.check)):
            case.enterContext(patch.object(self.run, name, function))

    def load(self):
        run = self.run
        run.before = baseline_snapshot()
        run.profile = CONTROLLER.target_profile(run.before)
        run.state = {"schema": 27, "synthetic": True}
        run.media, run.primary = {"synthetic_media": "unchanged"}, {"synthetic_primary": "unchanged"}
        run.credentials = {"viewer": {"username": "synthetic-viewer", "password": "synthetic-viewer-password"},
                           "admin": {"username": "synthetic-admin", "password": "synthetic-admin-password"}}
        run.js_sources = input_fixture()["source_closure"]
        run.sources = {str(CONTROLLER.TOOL / "observe-client-library-changed.py"): OPERATOR_SHA256,
                       str(CONTROLLER.TOOL / "memory-reader.py"): "9" * 64}
        run.op = types.SimpleNamespace(preservation_snapshot=lambda _state, schema: self.snapshot(schema))
        run.profile_reader = types.SimpleNamespace(validate_structure=lambda _op, snapshot, state: self.structure(snapshot, state))
        run.media_reader = types.SimpleNamespace(media_witness=lambda _op, _state: copy.deepcopy(run.media))

    def structure(self, snapshot, state):
        if snapshot["schema"] != state["schema"] or len(snapshot["database"]["tables"]) != 35:
            raise AssertionError("The simulated external reader returned another schema.")

    def check(self):
        if self.run.root_fd is not None:
            self.run.check_root()

    def properties(self, unit=None):
        if unit is None and self.run.closed:
            return {"MainPID": "0", "InvocationID": self.run.invocation, "ActiveState": "inactive", "SubState": "dead"}
        if unit != CONTROLLER.CONTROLLER_UNIT:
            raise AssertionError("An unmodeled service lookup escaped the worker boundary.")
        return {"MainPID": str(OUTER["pid"]), "ControlGroup": "/system.slice/" + CONTROLLER.CONTROLLER_UNIT,
                "User": "root", "Group": "root", "InvocationID": "c" * 32}

    def snapshot(self, schema=27):
        if schema != 27:
            raise AssertionError("The controller requested a different schema snapshot.")
        if not self.browser_created:
            return baseline_snapshot()
        fixture = ledger_fixture(self.metadata_phase, False)
        value = fixture["after"]
        tables = value["database"]["tables"]
        next(row for row in tables["sessions"] if row["id"] == SESSION)["client_capabilities"] = {}
        if not self.native_created:
            tables["sessions"] = [row for row in tables["sessions"] if row["id"] != NATIVE_SESSION]
            tables["activity_entries"] = [row for row in tables["activity_entries"] if row["id"] != 165]
        next_id = max(row["id"] for row in tables["activity_entries"]) + 1
        for identifier, revoked in ((NATIVE_SESSION, self.native_revoked), (SESSION, self.browser_revoked)):
            if revoked:
                row = next(row for row in tables["sessions"] if row["id"] == identifier)
                row["revoked_at"] = at(9)
                tables["activity_entries"].append(audit_row(next_id, "session.revoked", row["user_id"], identifier,
                    "session", identifier, 9, source="native" if identifier == NATIVE_SESSION else "emby"))
                next_id += 1
        value["database"]["sequences"]["activity_entries_id_seq"] = {"last_value": next_id - 1, "is_called": True}
        return value

    def launch(self):
        self.lifecycle.append("launch")
        run = self.run
        run.save("launch-intent.json", {"unit": CONTROLLER.WORKER_UNIT, "synthetic_worker": True})
        run.launched, run.child, run.invocation = True, copy.deepcopy(CHILD), "d" * 32
        self.browser_created = True
        self.fs.seed_directory(CONTROLLER.BROWSER_ROOT, 0o700)
        self.private_record = self.fs.seed(CONTROLLER.BROWSER_ROOT / "session-private.json", private_fixture(run.input, run.input_sha))
        run.save("launch-result.json", {"returncode": 0})

    def worker_live(self):
        run = self.run
        name = CONTROLLER.STAGES[len(run.stages)]
        path = CONTROLLER.BROWSER_ROOT / ("stage-" + name + ".json")
        if str(path) in self.fs.entries:
            return
        control = run.controls[-1] if run.controls else None
        stage = stage_fixture(name, run.input, run.input_sha, control["sha256"] if control else None, self.private_record, control)
        if self.stage_hook:
            self.stage_hook(stage)
        self.fs.seed(path, stage)

    def connection(self, host, port, timeout):
        if (host, port) != ("127.0.0.1", 18198) or not 0 < timeout <= 8:
            raise AssertionError("A native exchange escaped its local candidate or deadline.")
        connection = MemoryConnection(self)
        self.connections.append(connection)
        return connection

    def exchange(self, method, path, body, headers):
        run = self.run
        if path.startswith("/emby/"):
            label = "browser-logout" if method == "POST" else "browser-exact401"
        else:
            label = run.dispatched[-1]
        self.exchanges.append({"label": label, "method": method, "path": path, "body": body, "headers": dict(headers)})
        if label.startswith("browser-"):
            if headers.get("X-Emby-Token") != TOKEN or "Cookie" in headers:
                raise AssertionError("The browser cleanup used another credential carrier.")
            if label == "browser-logout":
                self.browser_revoked = True
                if self.fault == "browser-logout-ack-lost":
                    raise OSError("Synthetic B logout ACK loss.")
                return MemoryResponse(b"", 204)
            return MemoryResponse(b'{"error":"unauthorized"}', 401)
        self.case.assertIn("native-" + label + "-intent.json", run.records)
        self.case.assertEqual((method, path), run.fence.REQUESTS[label])
        if label == "login":
            self.native_created = True
            user = next(row for row in run.before["database"]["tables"]["users"] if row["id"] == CONTROLLER.ADMIN)
            payload = {"User": {"Id": CONTROLLER.ADMIN, "Name": user["name"], "IsAdministrator": True,
                "IsDisabled": False, "HasPassword": True, "CreatedAt": user["created_at"]},
                "CSRFToken": CONTROLLER.sha(("goby:admin:csrf:" + NATIVE_TOKEN).encode())}
            raw = CONTROLLER.canonical(payload)
            cookie = "goby_session=" + NATIVE_TOKEN + "; Path=/; HttpOnly; SameSite=Strict"
            response_headers = {"Set-Cookie": cookie, "Content-Type": "application/json", "Content-Length": str(len(raw))}
            if self.fault == "login-malformed":
                raw = b"{malformed"
                response_headers["Content-Length"] = str(len(raw))
            if self.fault == "login-truncated":
                response_headers["Content-Length"] = str(len(raw) + 1)
            return MemoryResponse(raw, headers=response_headers,
                read_error=OSError("Synthetic login body read failure.") if self.fault == "login-read-error" else None)
        self.case.assertEqual(headers.get("Cookie"), "goby_session=" + NATIVE_TOKEN)
        self.case.assertEqual(headers.get("X-CSRF-Token"), CONTROLLER.sha(("goby:admin:csrf:" + NATIVE_TOKEN).encode()))
        if label == "logout":
            self.native_revoked = True
            if self.fault == "native-logout-ack-lost":
                raise OSError("Synthetic native logout ACK loss.")
            return MemoryResponse(b"", 204)
        if label == "exact401":
            return MemoryResponse(b'{"error":"unauthorized"}', 401)
        if label in ("forward", "restore"):
            expected = run.reservation["forward_body" if label == "forward" else "restore_body"]
            self.case.assertEqual(memory_json_decode(body), expected)
            if label == "forward" and self.fault == "forward-unresolved":
                raise OSError("Synthetic dispatch without a commit barrier.")
            self.metadata_phase = "forward" if label == "forward" else "restored"
            if label == "forward" and self.fault == "forward-committed-ack-lost":
                raise OSError("Synthetic committed forward ACK loss.")
            if label == "restore" and self.fault == "restore-committed-ack-lost":
                raise OSError("Synthetic committed restoration ACK loss.")
        document = metadata_document(run.profile, self.metadata_phase,
            run.reservation["marker_name"] if run.reservation else None,
            at(4 if self.metadata_phase == "forward" else 6) if self.metadata_phase != "original" else None)
        return MemoryResponse(CONTROLLER.canonical(document), headers={"Content-Type": "application/json"})

    def await_worker(self):
        self.lifecycle.append("worker-terminal")
        run = self.run
        self.browser_revoked = self.fault != "browser-logout-ack-lost"
        run.closed = True
        run.terminal = {"MainPID": "0", "ExecMainStatus": "0", "cgroup_empty": True, "InvocationID": run.invocation}
        run.save("worker-terminal.json", run.terminal)
        cap = {"marker": "goby-client-library-home-capabilities-v1", "version": 1, "input_sha256": run.input_sha,
            "source_closure_sha256": digest(run.js_sources), "node_process": run.child,
            "session_id": SESSION, "token_sha256": TOKEN_SHA, "entries": []}
        cap_record = self.fs.seed(CONTROLLER.BROWSER_ROOT / "capabilities-private.json", cap)
        self.fs.seed(CONTROLLER.BROWSER_ROOT / "report.json", self.browser_report(cap_record))

    def browser_report(self, cap_record):
        run = self.run
        normal = len(run.stages) == 4 and len(run.controls) == 4 and not run.errors
        actor = {"logout": {"status": 204 if self.browser_revoked else None, "login_view_visible": self.browser_revoked},
            "proxy_logout": {"status": 204, "completed": True, "token_fingerprint": TOKEN_SHA},
            "session_proof": {"outcome": "all_observed_logout_tokens_rejected", "entries": [
                {"result": "logout_token_rejected", "token_fingerprint": TOKEN_SHA}]},
            "login": {"request_count": 1, "status": 200}, "ordinary_authority_confirmed": True, "page_error_count": 0,
            "closed": True, "cleanup_failures": [], "proxy": {"login": 1, "logout": 1, "completed": 2, "admitted": 2,
                "preparation": 0, "failed": 0, "rejected": 0, "active": 0, "capabilities": 0},
            "network": {key: 0 for key in ("forbidden_mutations", "playback_attempts", "observer_errors", "overflow", "guard_errors", "external_blocked")},
            "websocket": {"failed": 0, "control_attempts": 0}}
        frame = {"kind": "login", "finished": True, "failed": False, "status": 200, "from_service_worker": False,
                 "content_type": "application/json", "request_sha256": "a" * 64}
        report = {"marker": "goby-client-library-changed-report-v1", "version": 1, "mode": CONTROLLER.MODE,
            "input_sha256": run.input_sha, "source_closure_sha256": digest(run.js_sources), "controller": run.input["controller"],
            "node_process": run.child, "candidate": run.input["candidate"], "authority": run.input["authority"],
            "target": run.input["target"], "client_acceptance": False, "full_m3_complete": False,
            "login_proof": ordinary_proof(), "session_private": self.private_record,
            "capabilities_private": dict(cap_record, request_count=0, last_successful_body_sha256=None),
            "result": "passed" if normal else "failed", "failure": None if normal else "synthetic-worker-aborted",
            "restoration": run.restoration, "actor": actor, "catalog_observation": {"observer_failure": None},
            "observation": {"frames": [frame], "physical": [{"kind": "login", "completed": True, "terminal_status": 200,
                                                             "request_sha256": frame["request_sha256"]}]},
            "closure": {"context_closed": True, "browser_closed": True, "proxy_closed": True, "http_pending": 0,
                "websocket_pending": 0, "websocket_active": 0, "sockets_remaining": 0, "websocket_opened": 1,
                "websocket_closed": 1, "cleanup_failures": []}}
        if normal:
            report.update(stages=[{key: value[key] for key in ("name", "path", "sha256")} for value in run.stages],
                controls=copy.deepcopy(run.controls[:-1]), control_close={key: run.controls[-1][key] for key in ("path", "sha256", "value")},
                discovery=run.stages[0]["value"]["observation"], armed=run.stages[1]["value"]["observation"],
                forward=run.stages[2]["value"]["observation"]["forward"],
                restore_armed=run.stages[2]["value"]["observation"]["boundary"],
                restored=run.stages[3]["value"]["observation"]["restored"])
        return report


class ActualRunGuards(GuardTestCase):
    def execute_memory(self, fault=None, failures=()):
        environment = MemoryRunEnvironment(self, fault)
        environment.fs.failures.update(failures)
        report = environment.run.run()
        self.assertTrue(all(connection.closed for connection in environment.connections))
        self.assertEqual(environment.fs.descriptors, {})
        return environment, report

    def test_actual_prepare_execute_publication_native_and_final_ledger_complete_in_memory(self):
        environment, report = self.execute_memory()
        run, filesystem = environment.run, environment.fs
        self.assertEqual(report["status"], "passed", report.get("errors"))
        self.assertEqual(report["ledger"], {"new_sessions": 2, "new_devices": 1, "new_audits": 6,
            "metadata_revision_delta": 2, "old_rows_sequences_private_preserved": True, "owned_sessions_closed": True})
        self.assertEqual([row["name"] for row in run.stages], list(CONTROLLER.STAGES))
        self.assertEqual([row["name"] for row in run.controls], list(CONTROLLER.CONTROLS))
        self.assertEqual(run.dispatched, ["login", "original", "forward", "forward-readback", "pre-restore", "restore",
                                         "restore-readback", "logout", "exact401"])
        self.assertEqual(run.fence.reserved, run.dispatched)
        self.assertEqual(sum(row["method"] == "PUT" for row in environment.exchanges), 2)
        self.assertFalse(report["library_changed_client_acceptance"])
        self.assertTrue(report["acceptance_ready_for_outer_terminal"])
        self.assertFalse(any(path.endswith(".pending") for path in filesystem.entries))
        self.assertEqual(filesystem.json("report.json"), report)
        self.assertEqual(filesystem.json("viewer-credentials.json")["viewer"], run.credentials["viewer"])
        self.assertNotIn(run.credentials["admin"]["password"], filesystem.entries[str(CONTROLLER.ROOT / "input.json")]["raw"].decode())
        self.assertNotIn("admin", filesystem.json("viewer-credentials.json"))
        for name in ("input.json", "metadata-reservation-private.json", "native-cookie-received-private.json",
                     "native-session-private.json", "accepted-stage-restore-armed.json", "after-full.json"):
            self.assertIn(name, run.records)
        final = filesystem.json("after-full.json")["database"]["tables"]
        self.assertEqual((len(final["sessions"]), len(final["devices"]), len(final["activity_entries"])), (75, 63, 169))

    def test_native_intent_publication_failure_consumes_intent_without_dispatch_or_restore(self):
        environment, report = self.execute_memory(failures=(("open", "native-forward-intent.json"),))
        self.assertEqual(report["status"], "failed")
        self.assertIn("forward", environment.run.fence.reserved)
        self.assertNotIn("forward", environment.run.dispatched)
        self.assertFalse(any(row["method"] == "PUT" for row in environment.exchanges))
        self.assertEqual(report["restoration"], "not_required")
        self.assertEqual(environment.run.dispatched[-2:], ["logout", "exact401"])
        self.assertNotIn("native-forward-result.json", environment.run.records)

    def test_missing_forward_ack_at_original_revision_remains_unresolved_without_write_retry(self):
        environment, report = self.execute_memory("forward-unresolved")
        self.assertEqual(report["status"], "failed")
        self.assertTrue(report["restoration_required"])
        self.assertEqual(environment.fs.json("reconcile-one-decision.json")["decision"], "unresolved")
        self.assertEqual([row["label"] for row in environment.exchanges if row["method"] == "PUT"], ["forward"])
        self.assertNotIn("restore", environment.run.fence.reserved)
        self.assertNotIn("reconcile-two", environment.run.fence.reserved)
        self.assertFalse(any(row["name"] == "close" for row in environment.run.controls))

    def test_missing_forward_ack_with_owned_revision_restores_exactly_once_and_retains_failure(self):
        environment, report = self.execute_memory("forward-committed-ack-lost")
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["restoration"], "confirmed")
        self.assertEqual(environment.fs.json("reconcile-one-decision.json")["decision"], "restore_once")
        self.assertEqual(environment.fs.json("reconcile-two-decision.json")["decision"], "restored")
        self.assertEqual([row["label"] for row in environment.exchanges if row["method"] == "PUT"], ["forward", "restore"])
        self.assertFalse(environment.run.forward_ack)
        self.assertEqual(report["ledger"]["new_audits"], 6)
        self.assertTrue(environment.run.browser is not None and environment.run.administrator is not None)

    def test_missing_restore_ack_with_two_owned_audits_never_sends_a_second_restore(self):
        environment, report = self.execute_memory("restore-committed-ack-lost")
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["restoration"], "confirmed")
        self.assertEqual(environment.fs.json("reconcile-one-decision.json")["decision"], "restored")
        self.assertEqual(environment.run.dispatched.count("restore"), 1)
        self.assertNotIn("reconcile-two", environment.run.dispatched)
        self.assertFalse(environment.run.restore_ack)

    def test_close_control_publication_failure_still_closes_both_owned_credentials(self):
        environment, report = self.execute_memory(failures=(("link", "control-close.json"),))
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["restoration"], "confirmed")
        self.assertTrue(environment.native_revoked and environment.browser_revoked)
        self.assertIn("worker-terminal", environment.lifecycle)
        self.assertEqual(environment.run.dispatched[-2:], ["logout", "exact401"])
        self.assertTrue(report["ledger"]["owned_sessions_closed"])
        self.assertIn(str(CONTROLLER.ROOT / "control-close.json.pending"), environment.fs.entries)

    def test_incomplete_native_login_retains_cookie_for_owned_cleanup_but_no_metadata_request(self):
        for fault in ("login-malformed", "login-truncated", "login-read-error"):
            with self.subTest(fault=fault):
                environment, report = self.execute_memory(fault)
                run = environment.run
                self.assertEqual(report["status"], "failed")
                self.assertEqual(run.dispatched, ["login", "logout", "exact401"])
                self.assertFalse(run.native_login_validated)
                self.assertTrue(environment.native_revoked and environment.browser_revoked)
                self.assertEqual(run.administrator["id"], NATIVE_SESSION)
                cookie_record = environment.fs.json("native-cookie-received-private.json")
                self.assertFalse(cookie_record["successful_login"])
                self.assertTrue(cookie_record["requires_independent_new_session_ownership"])
                self.assertEqual(cookie_record["token_sha256"], NATIVE_TOKEN_SHA)
                self.assertNotIn("raw_body_utf8", environment.fs.json("native-login-result.json"))
                self.assertNotIn("original", run.fence.reserved)

    def test_native_logout_missing_ack_keeps_one_delete_and_still_observes_exact401(self):
        environment, report = self.execute_memory("native-logout-ack-lost")
        self.assertEqual(report["status"], "failed")
        self.assertEqual(environment.run.dispatched.count("logout"), 1)
        self.assertEqual(environment.run.dispatched.count("exact401"), 1)
        self.assertEqual(environment.fs.json("native-logout-result.json")["failure_type"], "OSError")
        self.assertEqual(environment.fs.json("native-exact401-result.json")["status"], 401)

    def test_b_fallback_missing_ack_consumes_one_post_and_retains_independent_exact401(self):
        environment, report = self.execute_memory("browser-logout-ack-lost")
        self.assertEqual(report["status"], "failed")
        self.assertTrue(report["browser_fallback_used"])
        self.assertEqual([row["label"] for row in environment.exchanges if row["label"].startswith("browser-")],
                         ["browser-logout", "browser-exact401"])
        requests = environment.fs.json("browser-fallback-result.json")["requests"]
        self.assertEqual(requests[0]["failure_type"], "OSError")
        self.assertEqual((requests[1]["status"], requests[1]["complete"]), (401, True))
        self.assertTrue(report["ledger"]["owned_sessions_closed"])

    def test_actual_stage_rejects_self_consistent_window_using_another_armed_token(self):
        environment = MemoryRunEnvironment(self)
        def replace_token(stage):
            if stage["name"] != "restore-armed":
                return
            window = stage["observation"]["forward"]
            window["boundary"]["token_sha256"] = NATIVE_TOKEN_SHA
            for row in window["events"]["physical"] + window["events"]["browser"] + window["http"]["physical"] + window["http"]["frames"]:
                row["token_sha256"] = NATIVE_TOKEN_SHA
        environment.stage_hook = replace_token
        report = environment.run.run()
        self.assertEqual(report["status"], "failed")
        self.assertEqual([row["name"] for row in environment.run.stages], ["discovery", "armed"])
        self.assertTrue(any("previously accepted armed boundary" in row.get("reason", "") for row in report["errors"]))
        self.assertEqual(environment.run.dispatched.count("restore"), 1)
        self.assertEqual(report["restoration"], "confirmed")


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(stream=sys.stderr, verbosity=2).run(suite)
    passed = result.wasSuccessful() and not result.skipped
    report = {"suite": "client-library-changed-controller-guards", "status": "passed" if passed else "failed",
              "operator_sha256": OPERATOR_SHA256, "guard_sha256": GUARD_SHA256, "test_count": result.testsRun,
              "failures": len(result.failures), "errors": len(result.errors), "skips": len(result.skipped)}
    sys.stdout.write(json.dumps(report, sort_keys=True, separators=(",", ":")) + "\n")
    raise SystemExit(0 if passed else 1)
