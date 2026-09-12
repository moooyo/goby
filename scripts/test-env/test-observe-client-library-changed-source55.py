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
import linecache
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
import urllib.parse
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


OPERATOR_PATH = Path(__file__).with_name("observe-client-library-changed-source55.py")
OPERATOR_RAW = OPERATOR_PATH.read_bytes()
GUARD_RAW = Path(__file__).read_bytes()
OPERATOR_SHA256 = hashlib.sha256(OPERATOR_RAW).hexdigest()
GUARD_SHA256 = hashlib.sha256(GUARD_RAW).hexdigest()
ACTIVE_FENCES = []
ORIGINAL_HELP_FORMATTER_INIT = argparse.HelpFormatter.__init__


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


def memory_translation(message):
    return message


def memory_plural_translation(singular, plural, count):
    return singular if count == 1 else plural


def memory_help_formatter_init(self, prog, indent_increment=2, max_help_position=24, width=None):
    return ORIGINAL_HELP_FORMATTER_INIT(self, prog, indent_increment=indent_increment,
        max_help_position=max_help_position, width=78 if width is None else width)


class MemoryTextTestResult(unittest.TextTestResult):
    """Format failures from exception and code objects without source lookup."""

    def _exc_info_to_string(self, error, test):
        exception_type, exception, trace = error
        name = exception_type.__module__ + "." + exception_type.__name__
        arguments = exception.args
        message = arguments[0][:2048] if arguments and type(arguments[0]) is str else ""
        lines = [name + (": " + message if message else ""), "Traceback locations (no source reads):"]
        for _index in range(32):
            if trace is None:
                break
            code = trace.tb_frame.f_code
            filename = code.co_filename.replace("\\", "/").rsplit("/", 1)[-1][:256]
            lines.append("  " + filename + ":" + str(trace.tb_lineno) + " in " + code.co_name[:128])
            trace = trace.tb_next
        if trace is not None:
            lines.append("  [remaining frames omitted]")
        return "\n".join(lines) + "\n"


class GuardTestCase(unittest.TestCase):
    def setUp(self):
        self.enterContext(patch.object(CONTROLLER, "Path", MemoryPath))
        for name, value in list(vars(CONTROLLER).items()):
            if isinstance(value, PurePosixPath):
                self.enterContext(patch.object(CONTROLLER, name, MemoryPath(str(value))))
        # Keep the actual parser and formatter. Fixed width avoids the original
        # formatter's local import and terminal lookup; translations stay local.
        self.enterContext(patch.object(argparse, "_", memory_translation))
        self.enterContext(patch.object(argparse, "ngettext", memory_plural_translation))
        self.enterContext(patch.object(argparse.HelpFormatter, "__init__", memory_help_formatter_init))
        self.enterContext(EffectFence())
        self.enterContext(patch.object(CONTROLLER, "json", CONTROLLER_JSON))

    def reject(self, callback):
        with self.assertRaises(CONTROLLER.ObservationError):
            callback()


class MemoryJSONGuards(GuardTestCase):
    def test_failure_reporting_never_looks_up_source_files(self):
        result = MemoryTextTestResult(io.StringIO(), True, 0)
        with patch.object(linecache, "getline", denied), patch.object(linecache, "getlines", denied), \
                patch.object(linecache, "checkcache", denied):
            for exception in (AssertionError("synthetic assertion failure"), RuntimeError("synthetic unexpected error")):
                try:
                    raise exception
                except (AssertionError, RuntimeError):
                    error = sys.exc_info()
                    if type(exception) is AssertionError:
                        result.addFailure(self, error)
                    else:
                        result.addError(self, error)
        self.assertEqual(len(result.failures), 1)
        self.assertEqual(len(result.errors), 1)
        self.assertIn("synthetic assertion failure", result.failures[0][1])
        self.assertIn("synthetic unexpected error", result.errors[0][1])
        self.assertIn("test_failure_reporting_never_looks_up_source_files", result.errors[0][1])

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
CANDIDATE = {"binary_sha256": "12" * 32, "runtime_sha256": "23" * 32, "state_sha256": "34" * 32,
    "process": {"pid": 210055, "start_ticks": 120055, "boot_id": CONTROLLER.BOOT},
    "invocation_id": "45" * 16, "publication": "56" * 20, "server_id": CONTROLLER.SERVER,
    "base_url": CONTROLLER.BASE_URL, "direct_url": CONTROLLER.DIRECT_URL, "source": str(CONTROLLER.SOURCE),
    "source_manifest_sha256": CONTROLLER.MANIFEST_SHA}
AV = "ce" * 16


def authority_fixture():
    output = CONTROLLER.WORK / "client-schema28-source55-upgrade-20260912_140000_123456abcdef"
    return {"marker": "goby-client-library-changed-source55-authority-v1", "version": 1, "candidate": copy.deepcopy(CANDIDATE),
        "authority": {**{key: {"path": str(path), "sha256": hashlib.sha256(key.encode()).hexdigest()} for key, path in (
            ("upgrade_intent", CONTROLLER.UPGRADE_TOOL / "intent.json"), ("upgrade_report", output / "report.json"),
            ("upgrade_attestation", output / "attestation.json"), ("current_snapshot", output / "after-full.json"))},
            'history': [{'version': 2, **{name: copy.deepcopy(CONTROLLER.PRIOR_PINS[key]) for name, key in CONTROLLER.HISTORY_NAMES.items()}},
                        {'version': 3, **copy.deepcopy(CONTROLLER.HISTORY_V3_PINS)},
                        {'version': 4, **copy.deepcopy(CONTROLLER.HISTORY_V4_PINS)},
                        {'version': 5, **copy.deepcopy(CONTROLLER.HISTORY_V5_PINS)}]}}


def schema_state():
    return {"schema": 28, "runtime_sha256": CANDIDATE["runtime_sha256"], "browser_sha256": CONTROLLER.CREDENTIALS_SHA,
            "admin_id": CONTROLLER.ADMIN, "viewer_id": CONTROLLER.B, "added_viewer": {"user_id": AV}, "server_id": CONTROLLER.SERVER}


def catalog_fixture(snapshot):
    """Build a synthetic descriptor for pure validator cases, not a production catalog."""
    columns = snapshot["database"]["metadata"]["columns"]
    objects = [{"kind": "relation", "name": name, "value": {}} for name in columns]
    objects += [{"kind": "column", "name": name + "." + str(index).zfill(5), "value": {"name": column, "dropped": False}}
                for name, names in columns.items() for index, column in enumerate(names, 1)]
    consumers = {"devices_id_seq": ("devices", "id"), "activity_entries_id_seq": ("activity_entries", "id"),
                 "catalog_entities_id_seq": ("catalog_entities", "id"), "application_keys_id_seq": ("application_keys", "id"),
                 "theme_owner_ids_id_seq": ("theme_owner_ids", "id")}
    return {"version": 28, "postgresql_major": 17, "migrations": [
        {"version": version, "name": "%04d_synthetic.sql" % version, "sha256": hashlib.sha256(str(version).encode()).hexdigest()}
        for version in range(1, 29)], "objects": objects,
        "catalog": {"Schema": "", "Tables": [{"Name": name, "Columns": names, "PrimaryKey": names[:1], "SortKey": names[:1]}
            for name, names in columns.items()], "Sequences": [{"Name": name, "Table": pair[0], "Column": pair[1],
            "MinValue": 1, "MaxValue": 9223372036854775807, "Increment": 1,
            "Consumers": [{"Table": pair[0], "Column": pair[1]}]} for name, pair in consumers.items()],
            "Constraints": [], "SHA256": CONTROLLER.sha(CONTROLLER.canonical(objects))}}


def memory_structure_reader():
    def themes(tables):
        if not all(isinstance(tables[name], list) for name in ("theme_owner_ids", "theme_reserved_paths", "item_theme_resources")):
            raise CONTROLLER.ObservationError("A synthetic theme inventory is malformed.")
    return types.SimpleNamespace(validate_theme_state=themes, added_viewer_credentials=lambda _state: None)


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
        "device_name": "Observed browser" if kind == "emby" else "Web browser", "client_version": "4.9.5.0" if kind == "emby" else "synthetic-source55",
        "created_at": at(created), "expires_at": at(created + (30 if kind == "emby" else 1) * 86400),
        "last_seen_at": at(created), "revoked_at": None, "client_capabilities": {}, "device_registry_id": device_registry_id}


def audit_row(identifier, action, actor, credential, resource_kind, resource, moment, *, source="native", revision=0):
    metadata = action == "metadata.updated"
    return {"id": identifier, "created_at": at(moment), "action": action, "severity": "Info", "source": source,
        "actor_kind": "user", "actor_id": actor, "actor_credential_id": credential, "resource_kind": resource_kind,
        "resource_id": resource, "request_id": "", "revision": revision, "affected_count": 0 if metadata else 1,
        "state": "", "changed_fields": ["Name", "Overrides"] if metadata else [],
        "previous_revision": 0, "observation_fingerprint": ""}


def baseline_snapshot():
    tables = {name: [] for name in TABLES}
    tables["users"] = [
        {"id": CONTROLLER.B, "name": "m3e-client-viewer", "management_revision": 5, "is_administrator": False,
         "is_disabled": False, "has_password": True, "created_at": at(-86400)},
        {"id": CONTROLLER.ADMIN, "name": "m3e-client-administrator", "management_revision": 1, "is_administrator": True,
         "is_disabled": False, "has_password": True, "created_at": at(-86400)},
        {"id": AV, "name": "m3e-client-av", "management_revision": 1, "is_administrator": False,
         "is_disabled": False, "has_password": True, "created_at": at(-86400)}]
    tables["server_settings"] = [{"key": "server_id", "value": CONTROLLER.SERVER}]
    for index in range(75):
        row = session_row(format(index + 1, "032x"), CONTROLLER.B,
            hashlib.sha256(("old-token-" + str(index)).encode()).hexdigest(), "emby", -86400)
        row["revoked_at"] = at(-3600)
        tables["sessions"].append(row)
    for index in range(64):
        tables["devices"].append({"id": index + 2, "reported_device_id": "old-device-" + str(index),
            "reported_name": "Old browser", "custom_name": None, "app_name": "Emby Web", "app_version": "4.9.5.0",
            "last_user_id": CONTROLLER.B, "created_at": at(-86400), "last_seen_at": at(-3600),
            "ip_address": "127.0.0.1", "revision": 1, "deleted_at": None})
    for index in range(167):
        old = tables["sessions"][index % 75]
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
    tables["library_roots"] = [{"id": ROOT_ID if index == 0 else format(9000 + index, "032x"), "library_id": library["id"],
        "path": "/synthetic/root-" + str(index), "binding_revision": 1, "storage_binding": None, "bound_at": None, "bound_by": None}
        for index, library in enumerate(tables["libraries"])]
    tables["play_sessions"] = [{"id": format(3000 + index, "032x"), "state": "Stopped"} for index in range(26)]
    tables["user_item_data"] = [{"user_id": CONTROLLER.B, "item_id": tables["items"][index]["id"], "play_count": 0}
                                for index in range(7)]
    tables["schema_migrations"] = [{"version": version, "name": "%04d_synthetic.sql" % version, "applied_at": at(-86400)} for version in range(1, 29)]
    columns = {name: list(rows[0]) if rows else ["id"] for name, rows in tables.items()}
    snapshot = {"schema": 28, "private_files": {"fixture_sha256": CANDIDATE["state_sha256"], "synthetic_credentials": "8" * 64},
        "runtime_sha256": CANDIDATE["runtime_sha256"], "browser_sha256": CONTROLLER.CREDENTIALS_SHA, "added_viewer_credentials": None,
        "media": {"groups": ["original", "extras", "music"]},
        "database": {"tables": tables, "metadata": {"captured_at": at(), "database": "goby_client_m3e",
            "server_version_num": 170006, "schemas": ["public"], "public_schema": {"oid": 2200, "owner": "pg_database_owner", "acl": None},
            "columns": columns, "relations": {name: {"oid": index + 1000, "owner": "goby_client_m3e", "acl": None, "column_acl": []}
                                               for index, name in enumerate(tables)}},
            "catalog": {"schema": 28, "synthetic_preserved_catalog": True}, "unsupported": False,
            "sequences": {"devices_id_seq": {"last_value": 65, "is_called": True},
                          "activity_entries_id_seq": {"last_value": 167, "is_called": True},
                          "catalog_entities_id_seq": {"last_value": 20, "is_called": True},
                          "application_keys_id_seq": {"last_value": 1, "is_called": False},
                          "theme_owner_ids_id_seq": {"last_value": 1, "is_called": False}}}}
    snapshot["database"]["catalog"] = catalog_fixture(snapshot)["objects"]
    return snapshot


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
    marker = "M3e Client Movie [LC source55 v1]"
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
        device_id=browser["device_id"], device_registry_id=66), copy.deepcopy(administrator)))
    tables["devices"].append({"id": 66, "reported_device_id": browser["device_id"], "reported_name": browser["device_name"],
        "custom_name": None, "app_name": browser["client_name"], "app_version": browser["client_version"],
        "last_user_id": CONTROLLER.B, "created_at": at(0.5), "last_seen_at": at(1),
        "ip_address": "127.0.0.1", "revision": 1, "deleted_at": None})
    tables["activity_entries"].extend((audit_row(168, "session.login", CONTROLLER.B, SESSION, "session", SESSION, 1, source="emby"),
                                      audit_row(169, "session.login", CONTROLLER.ADMIN, NATIVE_SESSION, "session", NATIVE_SESSION, 2)))
    authenticated["database"]["sequences"].update(devices_id_seq={"last_value": 66, "is_called": True},
                                                   activity_entries_id_seq={"last_value": 169, "is_called": True})
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
            rows["activity_entries"].append(audit_row(170 + step, "metadata.updated", CONTROLLER.ADMIN, NATIVE_SESSION,
                "item", CONTROLLER.ITEM, 5 + 2 * step, revision=2 + step))
    capabilities = [{}, {"PlayableMediaTypes": ["Video"]}]
    ordinary = next(row for row in rows["sessions"] if row["id"] == SESSION)
    native = next(row for row in rows["sessions"] if row["id"] == NATIVE_SESSION)
    ordinary.update(last_seen_at=at(8), client_capabilities=copy.deepcopy(capabilities[-1]))
    rows["devices"][-1]["last_seen_at"] = at(8)
    if closed:
        for index, row in enumerate((ordinary, native)):
            row["revoked_at"] = at(9)
            rows["activity_entries"].append(audit_row(170 + steps + index, "session.revoked", row["user_id"], row["id"],
                "session", row["id"], 9, source="emby" if row["kind"] == "emby" else "native"))
    after["database"]["sequences"]["activity_entries_id_seq"] = {"last_value": 169 + steps + (2 if closed else 0), "is_called": True}
    after["database"]["metadata"]["captured_at"] = at(10)
    return {"before": before, "after": after, "profile": profile, "reservation": reservation, "browser": browser,
            "administrator": administrator, "phase": phase, "capabilities": capabilities, "authenticated": authenticated, "closed": closed}


def upgrade_documents_fixture():
    envelope = authority_fixture()
    candidate, authority = envelope["candidate"], envelope["authority"]
    run = "20260912_140000_123456abcdef"
    output = str(CONTROLLER.WORK / ("client-schema28-source55-upgrade-" + run))
    unit = "goby-client-schema28-source55-" + run.replace("_", "-") + ".service"
    binary = {"path": str(CONTROLLER.WORK / "synthetic-source55-binary"), "sha256": candidate["binary_sha256"], "bytes": 80 << 20}
    service = {"MainPID": str(candidate["process"]["pid"]), "InvocationID": candidate["invocation_id"], "ActiveState": "active", "SubState": "running"}
    controller = {"unit": unit, "invocation_id": "67" * 16, "process": {"pid": 200055, "start_ticks": 119055, "boot_id": CONTROLLER.BOOT}}
    intent = {"marker": CONTROLLER.UPGRADE_MARKER, "version": 1, "run_id": run, "tool": str(CONTROLLER.UPGRADE_TOOL),
              "output": output, "controller": {"unit": unit}, "source": {"root": str(CONTROLLER.SOURCE),
              "manifest_sha256": CONTROLLER.MANIFEST_SHA, "binary": binary}}
    common = {"marker": CONTROLLER.UPGRADE_MARKER, "version": 1, "run_id": run,
              "intent_sha256": authority["upgrade_intent"]["sha256"], "schema": 28, "state_sha256": candidate["state_sha256"],
              "binary": binary, "client_acceptance": False}
    report = dict(common, status="awaiting_outer_attestation", controller=controller, candidate_service=service,
        new_process=candidate["process"], publication=candidate["publication"], service_actions=["stop", "start"], http_requests=5,
        preserved_rows_sequences_credentials_recovery=True, primary_unchanged=True, shared_web_unchanged=True,
        rehearsal_removed=True, hba_restored_exactly=True, automatic_retry=False, automatic_rollback=False,
        evidence={"after-full.json": authority["current_snapshot"], "before-state.json": {
            "path": output + "/before-state.json", "sha256": CONTROLLER.PREVIOUS_STATE_SHA}})
    terminal = {"MainPID": "0", "InvocationID": controller["invocation_id"], "ActiveState": "active", "SubState": "exited",
                "Result": "success", "ExecMainStatus": "0", "ControlGroup": "", "Description": CONTROLLER.UPGRADE_MARKER + ":" + run,
                "RemainAfterExit": "yes"}
    attestation = dict(common, status="passed", report_sha256=authority["upgrade_report"]["sha256"], controller=terminal,
        recursive_cgroup_empty=True, rows_sequences_credentials_recovery_preserved=True, primary_unchanged=True,
        rehearsal_removed=True, hba_restored_exactly=True)
    previous = dict(schema_state(), schema=27, marker="goby-m3e-client-acceptance-v1", work=str(CONTROLLER.WORK),
        phase="ready", stage="complete", binary_sha256=CONTROLLER.PREVIOUS_BINARY_SHA, runtime_sha256=CONTROLLER.PREVIOUS_RUNTIME_SHA,
        process=copy.deepcopy(CONTROLLER.PREVIOUS_PROCESS), upgrade={"historical": True}, upgrade_history=[], retained={"precise": Decimal("1.00001")})
    upgrade = {"marker": CONTROLLER.UPGRADE_MARKER, "phase": "complete", "from_schema": 27, "to_schema": 28, "output": output,
        "intent_sha256": authority["upgrade_intent"]["sha256"], "to_sha256": candidate["binary_sha256"], "publication": candidate["publication"],
        "new_process": candidate["process"], "service": service}
    state = dict(previous, schema=28, binary_sha256=candidate["binary_sha256"], runtime_sha256=candidate["runtime_sha256"],
        process=candidate["process"], upgrade=upgrade, upgrade_history=[upgrade], schema28_upgrade=upgrade,
        schema28_source={"marker": "goby-client-schema28-source-v1", "schema": 28, "source": str(CONTROLLER.SOURCE),
            "source_manifest_sha256": CONTROLLER.MANIFEST_SHA, "catalog_sha256": CONTROLLER.CATALOG_SHA, "publication": candidate["publication"]})
    return envelope, intent, report, attestation, state, previous


def prior_documents_fixture():
    """Model the failed v2 login-only ledger without executing its old controller."""
    value = ledger_fixture("original", True)
    before, after = value["before"], value["after"]
    rows = after["database"]["tables"]
    rows["sessions"] = [row for row in rows["sessions"] if row["id"] != NATIVE_SESSION]
    rows["activity_entries"] = [row for row in rows["activity_entries"] if row["actor_credential_id"] != NATIVE_SESSION]
    appended = [row for row in rows["activity_entries"] if row["actor_credential_id"] == SESSION]
    first = before["database"]["sequences"]["activity_entries_id_seq"]["last_value"] + 1
    for index, row in enumerate(appended):
        row["id"] = first + index
    after["database"]["sequences"]["activity_entries_id_seq"] = {"last_value": first + 1, "is_called": True}
    upgraded = copy.deepcopy(before)
    upgraded["database"]["metadata"]["captured_at"] = at(-1)
    current_authority = authority_fixture()["authority"]
    authority = CONTROLLER.history_entry_authority(current_authority, current_authority['history'][0])
    upgrade_authority = {key: authority[key] for key in CONTROLLER.UPGRADE_AUTHORITY_KEYS}
    outer = {"pid": 310055, "start_ticks": "210055", "boot_id": CONTROLLER.BOOT, "unit": CONTROLLER.PRIOR_CONTROLLER}
    worker = {"pid": 310056, "start_ticks": "210056", "boot_id": CONTROLLER.BOOT, "uid": 0, "gid": 0,
              "cgroup": "/system.slice/" + CONTROLLER.PRIOR_WORKER}
    sources = {str(CONTROLLER.PRIOR_TOOL / name): hashlib.sha256(name.encode()).hexdigest() for name in CONTROLLER.JS_NAMES}
    browser_authority = {**upgrade_authority, "before_snapshot": authority["prior_before_snapshot"]}
    target = profile_fixture(before)["target"]
    prior_input = {"marker": CONTROLLER.INPUT_MARKER, "version": 1, "mode": CONTROLLER.MODE, "root": str(CONTROLLER.PRIOR_ROOT),
        "output": str(CONTROLLER.PRIOR_ROOT / "browser"), "candidate": copy.deepcopy(CANDIDATE), "controller": outer,
        "actor": {"slot": "B", "user_id": CONTROLLER.B, "account_key": "viewer", "source_credentials_sha256": CONTROLLER.CREDENTIALS_SHA,
            "credentials": {"path": str(CONTROLLER.PRIOR_ROOT / "viewer-credentials.json"), "sha256": "ab" * 32}},
        "source_closure": sources, "authority": browser_authority, "target": target,
        "fixture": {key: {"path": str(path), "sha256": sha} for key, (path, sha) in CONTROLLER.FIXTURE.items()},
        "expected_libraries": sorted([{"id": row["id"], "name": row["name"]} for row in before["database"]["tables"]["libraries"]], key=lambda row: row["id"])}
    ledger = {"new_sessions": 1, "new_devices": 1, "new_audits": 2, "metadata_revision_delta": 0,
              "old_rows_sequences_private_preserved": True, "owned_sessions_closed": True}
    capabilities = {"path": str(CONTROLLER.PRIOR_ROOT / "browser/capabilities-private.json"), "sha256": "cd" * 32,
                    "request_count": 1, "last_successful_body_sha256": "de" * 32}
    controller = {"marker": CONTROLLER.MARKER, "version": 1, "mode": CONTROLLER.MODE, "status": "failed", "phase": "discovery",
        "input_sha256": authority["prior_input"]["sha256"], "source_closure_sha256": CONTROLLER.sha(CONTROLLER.canonical(sources)),
        "authority": upgrade_authority, "controller": outer, "candidate_process": CANDIDATE["process"],
        "candidate_invocation": CANDIDATE["invocation_id"], "state_sha256": CANDIDATE["state_sha256"], "node_process": worker,
        "reserved_native_intents": [], "dispatched_native_intents": [], "restoration": "not_required", "restoration_required": False,
        "browser_fallback_used": False, "automatic_retry": False, "sql_business_writes": False, "candidate_or_primary_service_writes": False,
        "worker_chain_ledger_passed": False, "acceptance_ready_for_outer_terminal": False, "library_changed_client_acceptance": False,
        "client_acceptance": False, "full_m3_complete": False, "errors": [{"stage": "execution_discovery", "failure_type": "ObservationError"}],
        "ledger": ledger, "evidence": {name: authority[key] for key, name in (("prior_input", "input.json"),
            ("prior_browser_report", "browser-report.json"), ("prior_before_snapshot", "before-full.json"), ("prior_after_snapshot", "after-full.json"))}}
    controller["evidence"]["browser-capabilities-private.json"] = {key: capabilities[key] for key in ("path", "sha256")}
    proof = ordinary_proof()
    actor = {"slot": "B", "id": CONTROLLER.B, "ordinary_authority_confirmed": True, "closed": True, "cleanup_failures": [],
        "token_fingerprint": TOKEN_SHA, "proxy_login_status": 200, "login": {"status": 200, "request_count": 1},
        "logout": {"status": 204, "login_view_visible": True}, "proxy_logout": {"status": 204, "completed": True, "token_fingerprint": TOKEN_SHA},
        "session_proof": {"outcome": "all_observed_logout_tokens_rejected", "entries": [{"token_fingerprint": TOKEN_SHA,
            "result": "logout_token_rejected", "verification": {"status": 401, "method": "GET", "route": "/emby/System/Info",
                "is_ui_request": False, "eligible_at_request_start": True, "result": "token_rejected"}}]}}
    browser = {"marker": "goby-client-library-changed-report-v1", "version": 1, "mode": CONTROLLER.MODE, "result": "failed", "outcome": "failed",
        "failure": "library_changed_target_card_not_observed", "input_sha256": controller["input_sha256"], "source_closure_sha256": controller["source_closure_sha256"],
        "controller": outer, "node_process": worker, "candidate": copy.deepcopy(CANDIDATE), "authority": browser_authority, "target": target,
        "stages": [], "controls": [], "discovery": None, "armed": None, "forward": None, "restore_armed": None, "restored": None,
        "library_changed_client_acceptance": False, "client_acceptance": False, "full_m3_complete": False, "restoration": "not_required",
        "login_proof": proof, "actor": actor, "capabilities_private": capabilities,
        "closure": {"context_closed": True, "browser_closed": True, "proxy_closed": True, "http_pending": 0, "websocket_pending": 0,
                    "websocket_active": 0, "sockets_remaining": 0, "websocket_opened": 1, "websocket_closed": 1, "cleanup_failures": []}}
    return {"candidate": copy.deepcopy(CANDIDATE), "authority": authority, "prior_input": prior_input, "controller": controller,
            "browser": browser, "baseline": upgraded, "before": before, "after": after}


def prior_terminal_fixture(value, version=2):
    candidate, authority, controller = value['candidate'], value['authority'], value['controller']
    scope, seal = CONTROLLER.history_scope(version), CONTROLLER.history_seal(version)
    def properties(unit, process, invocation, working, live=False):
        return {'ActiveState': 'active' if live else 'failed', 'ControlGroup': '/system.slice/' + unit if live else '',
            'DropInPaths': '', 'ExecMainCode': '0' if live else '1', 'ExecMainStatus': '0' if live else '1',
            'FragmentPath': ('/etc/systemd/system/' if live else '/run/systemd/transient/') + unit,
            'Group': 'goby' if live else 'root', 'Id': unit, 'InvocationID': invocation, 'LoadState': 'loaded',
            'MainPID': str(process['pid']) if live else '0', 'Restart': 'no', 'Result': 'success' if live else 'exit-code',
            'SubState': 'running' if live else 'failed', 'Transient': 'no' if live else 'yes',
            'User': 'goby' if live else 'root', 'WorkingDirectory': working}
    failed = {}
    for unit, source, invocation, working in ((scope['controller'], controller['controller'], seal['controller_invocation'], str(scope['tool'])),
            (scope['worker'], controller['node_process'], seal['worker_invocation'], str(scope['root']))):
        process = {'pid': source['pid'], 'start_ticks': int(source['start_ticks']), 'boot_id': source['boot_id']}
        failed[unit] = {'old_process': process, 'old_process_gone': True,
            'properties': properties(unit, process, invocation, working),
            'recursive_cgroup': {'exists': False, 'files_checked': 0, 'path': '/sys/fs/cgroup/system.slice/' + unit, 'processes': 0}}
    controller['worker_terminal'] = dict(failed[scope['worker']]['properties'], cgroup_empty=True)
    terminal = {'marker': 'goby-source55-failed-ui-terminal-v' + str(version), 'version': 1, 'schema': 28, 'status': 'failed_scope_sealed',
        'observed_run_status': 'failed', 'phase': 'discovery', 'scope': str(scope['root']), 'tool': str(scope['tool']),
        'captured_at': at({2: 12, 3: 27, 4: 42, 5: 57}[version]), 'cleanup': 'not_required', 'restoration': 'not_required', 'cleanup_needed': False, 'cleanup_performed': False,
        'automatic_retry': False, 'client_acceptance': False, 'full_m3_complete': False, 'library_changed_client_acceptance': False,
        'sql_business_writes': False, 'http_requests': 0, 'service_writes': 0, 'reserved_native_intents': [], 'dispatched_native_intents': [],
        'candidate_preserved': True, 'current_matches_prior_after': True, 'exact_owned_additions_retained': True, 'media_preserved': True,
        'old_rows_sequences_private_preserved': True, 'old_v1_scope_preserved': True, 'owned_session_closed': True,
        'primary_preserved': True, 'scope_files_unchanged': True, 'ledger': copy.deepcopy(controller['ledger']),
        'before_snapshot': authority['prior_before_snapshot'], 'prior_after_snapshot': authority['prior_after_snapshot'],
        'input': authority['prior_input'], 'browser_report': authority['prior_browser_report'], 'report': authority['prior_controller_report'],
        'upgrade_authority_snapshot': authority['current_snapshot'], 'independent_snapshot': copy.deepcopy(seal['independent']),
        'media_fact_sha256': '0f21473c43a050ad54f8985ee57e98addc6420e0cf33d6ee115db8cf8c0eff7d',
        'primary_fact_sha256': '0882d96f8b61c5586ce514a4c320a9bc933c2610cf55f24bfbec80237e77da3a',
        'primary_process': copy.deepcopy(CONTROLLER.PRIMARY_PROCESS), 'primary_invocation_id': CONTROLLER.PRIMARY_INVOCATION,
        'candidate': {**{key: copy.deepcopy(candidate[key]) for key in ('binary_sha256', 'invocation_id', 'process')},
            'properties': properties('goby-client-m3e.service', candidate['process'], candidate['invocation_id'], '/var/lib/goby-test/client-m3e', True)},
        'failed_units': failed, 'browser': {'browser_closed': True, 'capabilities_verified': True, 'capability_requests': 1, 'context_closed': True,
            'failure': 'library_changed_target_card_not_observed', 'failure_counters': {'observer_errors': 0, 'page_errors': 0, 'proxy_failed': 0,
                'proxy_rejected': 0, 'websocket_failed': 0}, 'http_pending': 0, 'login_proven': True, 'owned_session_revoked': True,
            'proxy_closed': True, 'result': 'failed', 'sockets_remaining': 0, 'ui_logout_and_token_rejection_proven': True,
            'websocket_active': 0, 'websocket_closed': 1, 'websocket_opened': 1, 'websocket_pending': 0},
        **copy.deepcopy(seal['records'])}
    if version == 3:
        terminal.update(baseline_chain_verified=True, old_v2_scope_preserved=True, prior_baseline=copy.deepcopy(CONTROLLER.PRIOR_INDEPENDENT),
            prior_failure_preservation=copy.deepcopy(controller['prior_failure_preservation']),
            cumulative_totals={'activity_entries': 171, 'devices': 66, 'sessions': 77}, **copy.deepcopy(CONTROLLER.HISTORY_V3_PREDECESSORS))
    elif version == 4:
        terminal.update(baseline_chain_verified=True, old_v2_scope_preserved=True, old_v3_scope_preserved=True,
            predecessor_units_preserved=True, prior_baseline=copy.deepcopy(CONTROLLER.HISTORY_V3_INDEPENDENT),
            history_preservation=copy.deepcopy(controller['history_preservation']), discovery_screenshot_bytes=38695,
            cumulative_totals={'activity_entries': 173, 'devices': 67, 'sessions': 78}, **copy.deepcopy(CONTROLLER.HISTORY_V4_PREDECESSORS))
    elif version == 5:
        terminal.update(baseline_chain_verified=True, old_v2_scope_preserved=True, old_v3_scope_preserved=True,
            old_v4_scope_preserved=True, predecessor_units_preserved=True, guard_evidence_preserved=True,
            prior_baseline=copy.deepcopy(CONTROLLER.HISTORY_V4_INDEPENDENT),
            history_preservation=copy.deepcopy(controller['history_preservation']), discovery_screenshot_bytes=38695,
            cumulative_totals={'activity_entries': 175, 'devices': 68, 'sessions': 79},
            guard_evidence=copy.deepcopy(CONTROLLER.HISTORY_V5_GUARDS), **copy.deepcopy(CONTROLLER.HISTORY_V5_PREDECESSORS))
    independent = copy.deepcopy(value['after'])
    independent['database']['metadata']['captured_at'] = at({2: 11, 3: 26, 4: 41, 5: 56}[version])
    return terminal, independent


def history_documents_fixture():
    first = prior_documents_fixture()
    terminal2, independent2 = prior_terminal_fixture(first)
    authority = authority_fixture()['authority']
    second = copy.deepcopy(first)
    second['version'] = 3
    second['authority'] = CONTROLLER.history_entry_authority(authority, authority['history'][1])
    second['baseline'] = copy.deepcopy(first['after'])
    second['before'] = copy.deepcopy(first['after'])
    second['before']['database']['metadata']['captured_at'] = at(15)
    second['after'] = copy.deepcopy(second['before'])
    second['after']['database']['metadata']['captured_at'] = at(25)
    rows, sequences = second['after']['database']['tables'], second['after']['database']['sequences']
    proof = dict(ordinary_proof(), session_id='bc' * 16, token_sha256=hashlib.sha256(b'second synthetic closed session').hexdigest(),
                 device_id='second-synthetic-browser', created_at=at(16))
    device_id = sequences['devices_id_seq']['last_value'] + 1
    session = session_row(proof['session_id'], CONTROLLER.B, proof['token_sha256'], 'emby', 16,
                          device_id=proof['device_id'], device_registry_id=device_id)
    session.update(last_seen_at=at(23), revoked_at=at(24), client_capabilities={'PlayableMediaTypes': ['Video']})
    rows['sessions'].append(session)
    rows['devices'].append(dict(rows['devices'][-1], id=device_id, reported_device_id=proof['device_id'],
                                created_at=at(15.5), last_seen_at=at(23)))
    next_audit = sequences['activity_entries_id_seq']['last_value'] + 1
    for offset, action, moment in ((0, 'session.login', 16), (1, 'session.revoked', 24)):
        rows['activity_entries'].append(audit_row(next_audit + offset, action, CONTROLLER.B, proof['session_id'],
            'session', proof['session_id'], moment, source='emby'))
    sequences['devices_id_seq'] = {'last_value': device_id, 'is_called': True}
    sequences['activity_entries_id_seq'] = {'last_value': next_audit + 1, 'is_called': True}
    scope, flat = CONTROLLER.history_scope(3), second['authority']
    original_authority = {**{key: flat[key] for key in CONTROLLER.UPGRADE_AUTHORITY_KEYS}, **copy.deepcopy(CONTROLLER.PRIOR_PINS)}
    input_authority = dict(original_authority, before_snapshot=flat['prior_before_snapshot'])
    sources = {str(scope['tool'] / name): hashlib.sha256(('v3-' + name).encode()).hexdigest() for name in CONTROLLER.JS_NAMES}
    outer = dict(second['controller']['controller'], pid=310057, start_ticks='210057', unit=scope['controller'])
    worker = dict(second['controller']['node_process'], pid=310058, start_ticks='210058', cgroup='/system.slice/' + scope['worker'])
    source_sha = CONTROLLER.sha(CONTROLLER.canonical(sources))
    second['prior_input'].update(root=str(scope['root']), output=str(scope['root'] / 'browser'), authority=input_authority,
                                  source_closure=sources, controller=outer)
    second['prior_input']['actor']['credentials']['path'] = str(scope['root'] / 'viewer-credentials.json')
    second['controller'].update(input_sha256=flat['prior_input']['sha256'], source_closure_sha256=source_sha, authority=original_authority,
        controller=outer, node_process=worker, prior_failure_preservation=copy.deepcopy(first['controller']['ledger']))
    for name, key in (('input.json', 'prior_input'), ('browser-report.json', 'prior_browser_report'),
                       ('before-full.json', 'prior_before_snapshot'), ('after-full.json', 'prior_after_snapshot')):
        second['controller']['evidence'][name] = flat[key]
    second['browser'].update(input_sha256=flat['prior_input']['sha256'], source_closure_sha256=source_sha, authority=input_authority,
        controller=outer, node_process=worker, login_proof=proof)
    second['browser']['capabilities_private']['path'] = str(scope['root'] / 'browser/capabilities-private.json')
    second['controller']['evidence']['browser-capabilities-private.json']['path'] = second['browser']['capabilities_private']['path']
    actor = second['browser']['actor']
    actor['token_fingerprint'] = actor['proxy_logout']['token_fingerprint'] = actor['session_proof']['entries'][0]['token_fingerprint'] = proof['token_sha256']
    terminal3, independent3 = prior_terminal_fixture(second, 3)
    third = copy.deepcopy(second)
    third['version'] = 4
    third['authority'] = CONTROLLER.history_entry_authority(authority, authority['history'][2])
    third['baseline'] = copy.deepcopy(second['after'])
    third['before'] = copy.deepcopy(second['after'])
    third['before']['database']['metadata']['captured_at'] = at(30)
    third['after'] = copy.deepcopy(third['before'])
    third['after']['database']['metadata']['captured_at'] = at(40)
    rows, sequences = third['after']['database']['tables'], third['after']['database']['sequences']
    proof = dict(ordinary_proof(), session_id='bd' * 16, token_sha256=hashlib.sha256(b'third synthetic closed session').hexdigest(),
                 device_id='third-synthetic-browser', created_at=at(31))
    device_id = sequences['devices_id_seq']['last_value'] + 1
    session = session_row(proof['session_id'], CONTROLLER.B, proof['token_sha256'], 'emby', 31,
                          device_id=proof['device_id'], device_registry_id=device_id)
    session.update(last_seen_at=at(38), revoked_at=at(39), client_capabilities={'PlayableMediaTypes': ['Video']})
    rows['sessions'].append(session)
    rows['devices'].append(dict(rows['devices'][-1], id=device_id, reported_device_id=proof['device_id'], created_at=at(30.5), last_seen_at=at(38)))
    next_audit = sequences['activity_entries_id_seq']['last_value'] + 1
    for offset, action, moment in ((0, 'session.login', 31), (1, 'session.revoked', 39)):
        rows['activity_entries'].append(audit_row(next_audit + offset, action, CONTROLLER.B, proof['session_id'],
            'session', proof['session_id'], moment, source='emby'))
    sequences['devices_id_seq'] = {'last_value': device_id, 'is_called': True}
    sequences['activity_entries_id_seq'] = {'last_value': next_audit + 1, 'is_called': True}
    scope, flat = CONTROLLER.history_scope(4), third['authority']
    original_authority = {**{key: flat[key] for key in CONTROLLER.UPGRADE_AUTHORITY_KEYS}, 'history': copy.deepcopy(authority['history'][:2])}
    input_authority = dict(original_authority, before_snapshot=flat['prior_before_snapshot'])
    sources = {str(scope['tool'] / name): hashlib.sha256(('v4-' + name).encode()).hexdigest() for name in CONTROLLER.JS_NAMES}
    outer = dict(third['controller']['controller'], pid=310059, start_ticks='210059', unit=scope['controller'])
    worker = dict(third['controller']['node_process'], pid=310060, start_ticks='210060', cgroup='/system.slice/' + scope['worker'])
    source_sha = CONTROLLER.sha(CONTROLLER.canonical(sources))
    third['prior_input'].update(root=str(scope['root']), output=str(scope['root'] / 'browser'), authority=input_authority,
                                 source_closure=sources, controller=outer)
    third['prior_input']['actor']['credentials']['path'] = str(scope['root'] / 'viewer-credentials.json')
    third['controller'].update(input_sha256=flat['prior_input']['sha256'], source_closure_sha256=source_sha, authority=original_authority,
        controller=outer, node_process=worker, history_preservation=[{'version': version, 'ledger': copy.deepcopy(first['controller']['ledger']),
            'after_snapshot': CONTROLLER.history_scope(version)['pins']['after_snapshot'],
            'independent_snapshot': CONTROLLER.history_seal(version)['independent']} for version in (2, 3)])
    third['controller'].pop('prior_failure_preservation')
    for name, key in (('input.json', 'prior_input'), ('browser-report.json', 'prior_browser_report'),
                       ('before-full.json', 'prior_before_snapshot'), ('after-full.json', 'prior_after_snapshot')):
        third['controller']['evidence'][name] = flat[key]
    third['browser'].update(input_sha256=flat['prior_input']['sha256'], source_closure_sha256=source_sha, authority=input_authority,
        controller=outer, node_process=worker, login_proof=proof)
    third['browser']['capabilities_private']['path'] = str(scope['root'] / 'browser/capabilities-private.json')
    third['controller']['evidence']['browser-capabilities-private.json']['path'] = third['browser']['capabilities_private']['path']
    actor = third['browser']['actor']
    actor['token_fingerprint'] = actor['proxy_logout']['token_fingerprint'] = actor['session_proof']['entries'][0]['token_fingerprint'] = proof['token_sha256']
    terminal4, independent4 = prior_terminal_fixture(third, 4)
    fourth = copy.deepcopy(third)
    fourth['version'] = 5
    fourth['authority'] = CONTROLLER.history_entry_authority(authority, authority['history'][3])
    fourth['baseline'] = copy.deepcopy(third['after'])
    fourth['before'] = copy.deepcopy(third['after'])
    fourth['before']['database']['metadata']['captured_at'] = at(45)
    fourth['after'] = copy.deepcopy(fourth['before'])
    fourth['after']['database']['metadata']['captured_at'] = at(55)
    rows, sequences = fourth['after']['database']['tables'], fourth['after']['database']['sequences']
    proof = dict(ordinary_proof(), session_id='be' * 16, token_sha256=hashlib.sha256(b'fourth synthetic closed session').hexdigest(),
                 device_id='fourth-synthetic-browser', created_at=at(46))
    device_id = sequences['devices_id_seq']['last_value'] + 1
    session = session_row(proof['session_id'], CONTROLLER.B, proof['token_sha256'], 'emby', 46,
                          device_id=proof['device_id'], device_registry_id=device_id)
    session.update(last_seen_at=at(53), revoked_at=at(54), client_capabilities={'PlayableMediaTypes': ['Video']})
    rows['sessions'].append(session)
    rows['devices'].append(dict(rows['devices'][-1], id=device_id, reported_device_id=proof['device_id'], created_at=at(45.5), last_seen_at=at(53)))
    next_audit = sequences['activity_entries_id_seq']['last_value'] + 1
    for offset, action, moment in ((0, 'session.login', 46), (1, 'session.revoked', 54)):
        rows['activity_entries'].append(audit_row(next_audit + offset, action, CONTROLLER.B, proof['session_id'],
            'session', proof['session_id'], moment, source='emby'))
    sequences['devices_id_seq'] = {'last_value': device_id, 'is_called': True}
    sequences['activity_entries_id_seq'] = {'last_value': next_audit + 1, 'is_called': True}
    scope, flat = CONTROLLER.history_scope(5), fourth['authority']
    original_authority = {**{key: flat[key] for key in CONTROLLER.UPGRADE_AUTHORITY_KEYS}, 'history': copy.deepcopy(authority['history'][:3])}
    input_authority = dict(original_authority, before_snapshot=flat['prior_before_snapshot'])
    sources = {str(scope['tool'] / name): hashlib.sha256(('v5-' + name).encode()).hexdigest() for name in CONTROLLER.JS_NAMES}
    outer = dict(fourth['controller']['controller'], pid=310061, start_ticks='210061', unit=scope['controller'])
    worker = dict(fourth['controller']['node_process'], pid=310062, start_ticks='210062', cgroup='/system.slice/' + scope['worker'])
    source_sha = CONTROLLER.sha(CONTROLLER.canonical(sources))
    fourth['prior_input'].update(root=str(scope['root']), output=str(scope['root'] / 'browser'), authority=input_authority,
                                  source_closure=sources, controller=outer)
    fourth['prior_input']['actor']['credentials']['path'] = str(scope['root'] / 'viewer-credentials.json')
    fourth['controller'].update(input_sha256=flat['prior_input']['sha256'], source_closure_sha256=source_sha, authority=original_authority,
        controller=outer, node_process=worker, history_preservation=[{'version': version, 'ledger': copy.deepcopy(first['controller']['ledger']),
            'after_snapshot': CONTROLLER.history_scope(version)['pins']['after_snapshot'],
            'independent_snapshot': CONTROLLER.history_seal(version)['independent']} for version in (2, 3, 4)])
    for name, key in (('input.json', 'prior_input'), ('browser-report.json', 'prior_browser_report'),
                       ('before-full.json', 'prior_before_snapshot'), ('after-full.json', 'prior_after_snapshot')):
        fourth['controller']['evidence'][name] = flat[key]
    fourth['browser'].update(input_sha256=flat['prior_input']['sha256'], source_closure_sha256=source_sha, authority=input_authority,
        controller=outer, node_process=worker, login_proof=proof)
    fourth['browser']['capabilities_private']['path'] = str(scope['root'] / 'browser/capabilities-private.json')
    fourth['controller']['evidence']['browser-capabilities-private.json']['path'] = fourth['browser']['capabilities_private']['path']
    actor = fourth['browser']['actor']
    actor['token_fingerprint'] = actor['proxy_logout']['token_fingerprint'] = actor['session_proof']['entries'][0]['token_fingerprint'] = proof['token_sha256']
    terminal5, independent5 = prior_terminal_fixture(fourth, 5)
    def bundle(value, terminal, independent):
        return {'input': value['prior_input'], 'browser_report': value['browser'], 'controller_report': value['controller'],
                'terminal': terminal, 'before_snapshot': value['before'], 'after_snapshot': value['after'], 'independent_snapshot': independent}
    return authority, first['baseline'], [bundle(first, terminal2, independent2), bundle(second, terminal3, independent3),
                                         bundle(third, terminal4, independent4), bundle(fourth, terminal5, independent5)]


class Source55OrderedHistoryGuards(GuardTestCase):
    def test_only_four_ordered_original_history_formats_are_admitted(self):
        value = authority_fixture()
        CONTROLLER.validate_authority_input(value)
        for history in ([], list(reversed(value['authority']['history'])), [value['authority']['history'][0]] * 2,
                        value['authority']['history'] + [value['authority']['history'][1]]):
            changed = copy.deepcopy(value)
            changed['authority']['history'] = history
            with self.subTest(length=len(history)):
                self.reject(lambda: CONTROLLER.validate_authority_input(changed))

    def test_four_complete_failed_ledgers_preserve_the_upgrade_and_all_closed_sessions(self):
        authority, upgraded, documents = history_documents_fixture()
        original = copy.deepcopy((authority, upgraded, documents))
        after, independent, results = CONTROLLER.validate_history_documents(CANDIDATE, authority, documents, upgraded)
        self.assertEqual([value['version'] for value in results], [2, 3, 4, 5])
        self.assertTrue(all(value['ledger']['new_sessions'] == 1 and value['ledger']['metadata_revision_delta'] == 0 for value in results))
        self.assertEqual((authority, upgraded, documents), original)
        fresh = copy.deepcopy(independent)
        fresh['database']['metadata']['captured_at'] = at(58)
        CONTROLLER.compare_fixed_snapshot(after, fresh)
        CONTROLLER.compare_fixed_snapshot(independent, fresh)
        self.reject(lambda: CONTROLLER.compare_fixed_snapshot(upgraded, fresh))

    def test_history_reordering_relabelled_inputs_and_unowned_predecessor_changes_fail(self):
        for mutation in ('relabel', 'old-row', 'prior-baseline', 'predecessor-order', 'before-time'):
            authority, upgraded, documents = history_documents_fixture()
            value = documents[1]
            if mutation == 'relabel':
                value['input']['authority'] = {**authority, 'before_snapshot': authority['history'][1]['before_snapshot']}
            elif mutation == 'old-row':
                value['after_snapshot']['database']['tables']['sessions'][0]['last_seen_at'] = at(20)
            elif mutation == 'prior-baseline':
                value['terminal']['prior_baseline'] = CONTROLLER.HISTORY_V3_INDEPENDENT
            elif mutation == 'predecessor-order':
                value['terminal']['predecessor_terminals'].reverse()
            else:
                value['before_snapshot']['database']['metadata']['captured_at'] = at(9)
            with self.subTest(mutation=mutation):
                self.reject(lambda: CONTROLLER.validate_history_documents(CANDIDATE, authority, documents, upgraded))

    def test_v5_seal_preserves_failed_guards_and_separate_repaired_launch_evidence(self):
        for mutation in ('old-passed', 'old-count', 'old-source', 'repair-path', 'launch-pin', 'missing-key', 'scope-flag', 'guard-flag'):
            authority, upgraded, documents = history_documents_fixture()
            terminal = documents[3]['terminal']
            evidence = terminal['guard_evidence']
            if mutation == 'old-passed':
                evidence['initial_controller_status'] = 'passed'
                evidence['initial_controller_errors'] = 0
            elif mutation == 'old-count':
                evidence['initial_controller_tests'] = 75
            elif mutation == 'old-source':
                evidence['old_guard_source'] = copy.deepcopy(evidence['repaired_guard_source'])
            elif mutation == 'repair-path':
                evidence['repaired_report']['path'] = evidence['initial_report']['path']
            elif mutation == 'launch-pin':
                evidence['launch_prerequisites']['python_preflight']['sha256'] = '98' * 32
            elif mutation == 'missing-key':
                del evidence['old_failed_guard_preserved']
            elif mutation == 'scope-flag':
                terminal['old_v4_scope_preserved'] = False
            else:
                terminal['guard_evidence_preserved'] = False
            with self.subTest(mutation=mutation):
                self.reject(lambda: CONTROLLER.validate_history_documents(CANDIDATE, authority, documents, upgraded))


class Source55PriorFailureGuards(GuardTestCase):
    def test_independent_failed_terminal_requires_exact_closed_lifetimes_and_snapshot(self):
        value = prior_documents_fixture()
        terminal, independent = prior_terminal_fixture(value)
        CONTROLLER.validate_prior_documents(**value)
        CONTROLLER.validate_prior_terminal(value['candidate'], value['authority'], value['controller'], value['browser'], terminal, value['after'], independent)
        for key, replacement in (('status', 'passed'), ('cleanup_performed', True), ('http_requests', 1),
                                 ('current_matches_prior_after', False), ('library_changed_client_acceptance', True)):
            changed = copy.deepcopy(terminal)
            changed[key] = replacement
            with self.subTest(field=key):
                self.reject(lambda: CONTROLLER.validate_prior_terminal(value['candidate'], value['authority'], value['controller'],
                    value['browser'], changed, value['after'], independent))
        for unit in (CONTROLLER.PRIOR_CONTROLLER, CONTROLLER.PRIOR_WORKER):
            changed = copy.deepcopy(terminal)
            changed['failed_units'][unit]['recursive_cgroup']['processes'] = 1
            with self.subTest(unit=unit):
                self.reject(lambda: CONTROLLER.validate_prior_terminal(value['candidate'], value['authority'], value['controller'],
                    value['browser'], changed, value['after'], independent))
        changed = copy.deepcopy(independent)
        changed['database']['tables']['items'][0]['name'] = 'Unowned title'
        self.reject(lambda: CONTROLLER.validate_prior_terminal(value['candidate'], value['authority'], value['controller'],
            value['browser'], terminal, value['after'], changed))

    def test_prior_failure_chain_retains_exact_upgrade_before_and_one_closed_login(self):
        value = prior_documents_fixture()
        original = copy.deepcopy(value)
        result = CONTROLLER.validate_prior_documents(**value)
        self.assertEqual(result, value["controller"]["ledger"])
        self.assertEqual(value, original)
        fresh = copy.deepcopy(value["after"])
        fresh["database"]["metadata"]["captured_at"] = at(12)
        CONTROLLER.compare_fixed_snapshot(value["after"], fresh)
        self.reject(lambda: CONTROLLER.compare_fixed_snapshot(value["baseline"], fresh))

    def test_prior_failure_cannot_be_upgraded_to_ui_success_or_native_activity(self):
        for document, key, replacement in (("controller", "status", "passed"), ("controller", "reserved_native_intents", ["login"]),
                ("controller", "dispatched_native_intents", ["forward"]), ("browser", "result", "passed"),
                ("browser", "discovery", {"passed": True}), ("browser", "library_changed_client_acceptance", True)):
            value = prior_documents_fixture()
            value[document][key] = replacement
            with self.subTest(document=document, field=key):
                self.reject(lambda: CONTROLLER.validate_prior_documents(**value))

    def test_prior_old_rows_metadata_and_capture_chain_are_preserved(self):
        for table, field in (("users", "management_revision"), ("items", "name"), ("item_metadata_state", "revision"),
                              ("sessions", "last_seen_at"), ("activity_entries", "previous_revision"), ("library_roots", "storage_binding")):
            value = prior_documents_fixture()
            value["after"]["database"]["tables"][table][0][field] = "unowned"
            with self.subTest(table=table):
                self.reject(lambda: CONTROLLER.validate_prior_documents(**value))
        value = prior_documents_fixture()
        value["before"]["database"]["metadata"]["captured_at"] = at(-2)
        self.reject(lambda: CONTROLLER.validate_prior_documents(**value))

    def test_prior_token_device_logout_and_exact_sequence_deltas_are_bound(self):
        for mutation in ("token", "device", "logout", "sequence", "audit"):
            value = prior_documents_fixture()
            if mutation == "token":
                value["browser"]["login_proof"]["token_sha256"] = "fe" * 32
            elif mutation == "device":
                value["after"]["database"]["tables"]["devices"][-1]["reported_device_id"] = "another-browser"
            elif mutation == "logout":
                value["browser"]["actor"]["session_proof"]["entries"][0]["verification"]["status"] = 200
            elif mutation == "sequence":
                value["after"]["database"]["sequences"]["activity_entries_id_seq"]["last_value"] += 1
            else:
                value["after"]["database"]["tables"]["activity_entries"][-1]["previous_revision"] = 1
            with self.subTest(mutation=mutation):
                self.reject(lambda: CONTROLLER.validate_prior_documents(**value))


class Source55AuthorityGuards(GuardTestCase):
    def test_cli_requires_the_exact_authority_and_new_driver_paths(self):
        self.assertEqual(CONTROLLER.TOOL, CONTROLLER.WORK / "client-library-changed-source55-tool-06b")
        self.assertEqual(CONTROLLER.ROOT, CONTROLLER.WORK / "client-library-changed-ui-source55-v6")
        self.assertEqual(CONTROLLER.WORKER_UNIT, "goby-client-library-changed-ui-source55-v6.service")
        self.assertEqual(CONTROLLER.CONTROLLER_UNIT, "goby-client-library-changed-ui-source55-controller-v6.service")
        values = ["--script-sha256", "12" * 32, "--driver", str(CONTROLLER.TOOL / CONTROLLER.JS_NAMES[0]),
            "--source-closure", str(CONTROLLER.TOOL / "sources.json"), "--source-closure-sha256", "23" * 32,
            "--authority", str(CONTROLLER.TOOL / "authority.json"), "--authority-sha256", "34" * 32,
            "--node", "/usr/bin/node", "--node-sha256", "45" * 32, "--check-only"]
        parsed = CONTROLLER.arguments(values)
        self.assertEqual(parsed.authority, CONTROLLER.TOOL / "authority.json")
        changed = list(values)
        changed[changed.index("--authority") + 1] = str(CONTROLLER.WORK / "authority.json")
        self.reject(lambda: CONTROLLER.arguments(changed))
        for previous in ("client-library-changed-source55-tool-05", "client-library-changed-source55-tool-06"):
            previous_tool = CONTROLLER.WORK / previous
            for option, name in (("--driver", CONTROLLER.JS_NAMES[0]), ("--authority", "authority.json"), ("--source-closure", "sources.json")):
                changed = list(values)
                changed[changed.index(option) + 1] = str(previous_tool / name)
                with self.subTest(previous_scope=previous, option=option):
                    self.reject(lambda: CONTROLLER.arguments(changed))

    def test_future_candidate_requires_every_pin_and_rejects_previous_identity(self):
        value = authority_fixture()
        candidate, authority = CONTROLLER.validate_authority_input(value)
        self.assertEqual(candidate, CANDIDATE)
        self.assertEqual(set(authority), CONTROLLER.AUTHORITY_KEYS)
        for key in candidate:
            changed = copy.deepcopy(value)
            del changed["candidate"][key]
            with self.subTest(missing=key):
                self.reject(lambda: CONTROLLER.validate_authority_input(changed))
        for field, replacement in (("binary_sha256", CONTROLLER.PREVIOUS_BINARY_SHA), ("state_sha256", CONTROLLER.PREVIOUS_STATE_SHA),
                ("runtime_sha256", CONTROLLER.PREVIOUS_RUNTIME_SHA), ("process", CONTROLLER.PREVIOUS_PROCESS),
                ("invocation_id", CONTROLLER.PREVIOUS_INVOCATION), ("publication", "0" * 40), ("binary_sha256", "f" * 64)):
            changed = copy.deepcopy(value)
            changed["candidate"][field] = replacement
            with self.subTest(field=field):
                self.reject(lambda: CONTROLLER.validate_authority_input(changed))

    def test_authority_paths_are_bounded_before_any_artifact_read(self):
        self.assertEqual(CONTROLLER.UPGRADE_TOOL, CONTROLLER.WORK / "client-schema28-source55-tool-05")
        changed = authority_fixture()
        changed["authority"]["upgrade_intent"]["path"] = str(CONTROLLER.WORK / "client-schema28-source55-tool-02" / "intent.json")
        self.reject(lambda: CONTROLLER.validate_authority_input(changed))
        for name in CONTROLLER.UPGRADE_AUTHORITY_KEYS:
            changed = authority_fixture()
            changed["authority"][name]["path"] = str(CONTROLLER.WORK / "foreign" / "input.json")
            with self.subTest(descriptor=name):
                self.reject(lambda: CONTROLLER.validate_authority_input(changed))

    def test_provisional_upgrade_requires_exact_independent_attestation_and_terminal(self):
        envelope, intent, report, attestation, state, previous = upgrade_documents_fixture()
        candidate, authority = CONTROLLER.validate_authority_input(envelope)
        CONTROLLER.validate_upgrade_authority(candidate, authority, intent, report, attestation, state)
        CONTROLLER.validate_historical_state(previous, state)
        for name, key, replacement in (("report", "status", "passed"), ("attestation", "status", "awaiting_outer_attestation"),
                ("report", "http_requests", 2), ("attestation", "recursive_cgroup_empty", False),
                ("attestation", "report_sha256", "98" * 32), ("report", "publication", "98" * 20)):
            changed_report, changed_attestation = copy.deepcopy(report), copy.deepcopy(attestation)
            (changed_report if name == "report" else changed_attestation)[key] = replacement
            with self.subTest(document=name, field=key):
                self.reject(lambda: CONTROLLER.validate_upgrade_authority(candidate, authority, intent, changed_report, changed_attestation, state))
        for key, value in (("SubState", "dead"), ("ActiveState", "inactive"), ("RemainAfterExit", "no"),
                           ("Description", "foreign"), ("InvocationID", "98" * 16), ("MainPID", "123")):
            changed = copy.deepcopy(attestation)
            changed["controller"][key] = value
            with self.subTest(terminal=key):
                self.reject(lambda: CONTROLLER.validate_upgrade_authority(candidate, authority, intent, report, changed, state))

    def test_upgrade_history_and_previous_credentials_remain_exact(self):
        _, _, _, _, state, previous = upgrade_documents_fixture()
        for field in ("retained", "browser_sha256", "added_viewer", "upgrade_history"):
            changed = copy.deepcopy(state)
            changed[field] = None
            with self.subTest(field=field):
                self.reject(lambda: CONTROLLER.validate_historical_state(previous, changed))

    def test_complete_schema28_columns_and_sequence_consumers_are_checked(self):
        snapshot = baseline_snapshot()
        catalog, state, op = catalog_fixture(snapshot), schema_state(), memory_structure_reader()
        CONTROLLER.validate_schema28_snapshot(snapshot, state, catalog, op)
        for table, field, value in (("library_roots", "binding_revision", True), ("library_roots", "storage_binding", {}),
                ("library_roots", "bound_at", at()), ("library_roots", "bound_by", CONTROLLER.ADMIN),
                ("activity_entries", "previous_revision", 1), ("activity_entries", "observation_fingerprint", "12" * 32)):
            changed = copy.deepcopy(snapshot)
            changed["database"]["tables"][table][0][field] = value
            with self.subTest(table=table, field=field):
                self.reject(lambda: CONTROLLER.validate_schema28_snapshot(changed, state, catalog, op))
        changed = copy.deepcopy(snapshot)
        changed["database"]["sequences"]["devices_id_seq"]["last_value"] = 64
        self.reject(lambda: CONTROLLER.validate_schema28_snapshot(changed, state, catalog, op))
        changed = copy.deepcopy(snapshot)
        del changed["database"]["tables"]["activity_entries"][0]["previous_revision"]
        self.reject(lambda: CONTROLLER.validate_schema28_snapshot(changed, state, catalog, op))

    def test_schema28_users_theme_checks_and_private_receipts_remain_required(self):
        snapshot = baseline_snapshot()
        catalog, state = catalog_fixture(snapshot), schema_state()
        for mutation in ("user", "server", "private", "theme"):
            changed = copy.deepcopy(snapshot)
            reader = memory_structure_reader()
            if mutation == "user":
                changed["database"]["tables"]["users"][2]["is_administrator"] = True
            elif mutation == "server":
                changed["database"]["tables"]["server_settings"][0]["value"] = "other"
            elif mutation == "private":
                changed["added_viewer_credentials"] = {}
            else:
                def deny(_tables):
                    raise CONTROLLER.ObservationError("The pinned theme reader rejected its structure.")
                reader.validate_theme_state = deny
            with self.subTest(mutation=mutation):
                self.reject(lambda: CONTROLLER.validate_schema28_snapshot(changed, state, catalog, reader))


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
        self.assertEqual((len(tables["sessions"]), len(tables["devices"]), len(tables["activity_entries"])), (77, 65, 173))

    def test_foreign_old_rows_fail_even_when_all_final_counts_stay_correct(self):
        for table, field, value in (("sessions", "last_seen_at", at(1)), ("devices", "reported_name", "Changed old device"),
                                    ("activity_entries", "request_id", "changed"), ("users", "management_revision", 6),
                                    ("libraries", "name", "Changed library"), ("user_item_data", "play_count", 1)):
            with self.subTest(table=table):
                fixture = ledger_fixture()
                fixture["after"]["database"]["tables"][table][0][field] = value
                self.reject(lambda: CONTROLLER.validate_ledger(**fixture))

    def test_schema28_audit_columns_missing_fields_and_extra_new_columns_are_rejected(self):
        for table, key, value in (("activity_entries", "previous_revision", 1),
                                  ("activity_entries", "observation_fingerprint", "12" * 32),
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
                                 ("activity_entries_id_seq", "last_value", Decimal("173")),
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
            snapshot["database"]["sequences"]["devices_id_seq"] = {"last_value": 66, "is_called": False}
            snapshot["database"]["sequences"]["activity_entries_id_seq"] = {"last_value": 168, "is_called": False}
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


PAGE_ROUTE = '/web/index.html#!/videos?parentId=' + CONTROLLER.LIBRARY + '&serverId=' + CONTROLLER.SERVER
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


def dom_fixture(name, passed=True, wire=None, phase='discovery', message_id=None, started=250):
    wire = wire or read_fixture(name, start=100, request_sequence=1, phase='discovery')
    physical, frame = wire['physical'], wire['frame']
    proof = {'phase': phase, 'physical_exchange_id': physical['id'], 'frame_request_index': frame['index'],
        'body_sha256': physical['projection']['body_sha256'], 'shape_sha256': physical['shape_sha256'],
        'request_sha256': physical['request_sha256'], 'token_sha256': TOKEN_SHA, 'message_id': message_id}
    return {"target_id": CONTROLLER.ITEM if passed else None, "expected_name": name, "media_inactive": True, "route": PAGE_ROUTE,
        "document_id": DOCUMENT, "passed": passed, "identity_proven": passed, "visible_target_cards": 1,
        "target_title_count": int(passed), "forbidden_title_count": int(not passed), 'started_elapsed_ms': started,
        'visible_items_containers': 1, 'visible_card_containers': 1, 'visible_cards': 1, 'visible_title_buttons': 1, 'explicit_identity_consistent': True,
        'observed_title': name if passed else 'Earlier title', 'selector': '.itemsContainer .card',
        'identity_mode': 'singleton-movie-list-wire-and-card' if passed else 'unbound', 'wire_identity': proof if passed else None}


def read_fixture(name, start=22120, request_sequence=14, identifier=1, index=0, phase='discovery'):
    query = [['IncludeItemTypes', 'Movie'], ['Limit', '50'], ["ParentId", CONTROLLER.LIBRARY], ['Recursive', 'true'], ['StartIndex', '0']]
    route = "/Users/" + CONTROLLER.B + "/Items"
    shape = digest([route, query])
    target = {"Id": CONTROLLER.ITEM, "Name": name, "Type": "Movie"}
    body = CONTROLLER.canonical({"Items": [target], "TotalRecordCount": 1})
    transfer = {"id": identifier, "kind": "items", "route": route, "query": query, "shape_sha256": shape,
        'phase': phase, 'method': 'GET', 'terminal': 'completed',
        "token_sha256": TOKEN_SHA, "request_sha256": "1" * 64, "completed": True, "status": 200,
        "request_sequence": request_sequence,
        "terminal_status": 200, "request_elapsed_ms": start + 10, "finished_elapsed_ms": start + 80,
        "projection": {"target": target, "count": 1, "body_sha256": CONTROLLER.sha(body), "body_bytes": len(body)}}
    frame = {"index": index, "token_sha256": TOKEN_SHA, "request_sha256": "1" * 64, "shape_sha256": shape,
        'phase': phase, 'kind': 'items', 'route': route,
        "finished": True, "failed": False, "status": 200, "content_type": "application/json",
        "from_service_worker": False, "source": "page", "main_frame": True, "request_sequence": request_sequence,
        "request_elapsed_ms": start, "finished_elapsed_ms": start + 100, "document_id": DOCUMENT, "page_route": PAGE_ROUTE}
    return {"physical": transfer, "frame": frame, "unambiguous": True, "complete": True}


def collection_folder_fixture():
    pair = read_fixture("M3e Client Movies", start=20, request_sequence=1, identifier=10, index=10)
    route = "/Users/" + CONTROLLER.B + "/Items/" + CONTROLLER.LIBRARY
    physical, frame = pair["physical"], pair["frame"]
    value = {"Id": CONTROLLER.LIBRARY, "Name": "M3e Client Movies", "Type": "CollectionFolder", "Subviews": ["movies", "movies", "folders"]}
    body = CONTROLLER.canonical(value)
    physical.update(kind="collection-folder", method="GET", terminal="completed", route=route, query=[], shape_sha256=digest([route, []]),
        projection={"collection_folder": value, "count": 1, "body_sha256": CONTROLLER.sha(body), "body_bytes": len(body)})
    frame.update(kind="collection-folder", route=route, shape_sha256=physical["shape_sha256"], page_route="/synthetic-home")
    return pair


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
                        identifier=1 if name == "forward" else 2, phase=name)
    samples = [{"sequence": sequence + 1, "started_elapsed_ms": start, "elapsed_ms": start + 10,
                "observation": dom_fixture(expected, False, started=start)}]
    samples.extend({"sequence": sequence + 20 + index, "started_elapsed_ms": moment, "elapsed_ms": moment + 10,
        "observation": dom_fixture(expected, wire=pair, phase=name, message_id=message['MessageId'], started=moment)}
        for index, moment in enumerate(range(start + 500, start + 120001, 500)))
    return {"name": name, "control_sha256": control_sha, "boundary": boundary,
        "commit": commit or {"revision": "2" if name == "forward" else "3", "write_completed_at": at(12),
                             "native_result_sha256": "3" * 64, "readback_sha256": "4" * 64},
        "events": {"physical": [upstream], "browser": [received]},
        "http": {"physical": [pair["physical"]], "frames": [pair["frame"]], "pairs": [
            {"frame_request_index": pair["frame"]["index"], "physical_exchange_id": pair["physical"]["id"],
             "complete": True, "unambiguous": True}]},
        "dom": samples, "actions": [], "lifecycle": [], "result": "passed",
        "outcome": "automatic_http_and_visible_title_observed",
        "proof": {"message_id": message["MessageId"], "physical_exchange_id": pair["physical"]["id"],
                  "frame_request_index": 0, "first_dom_sequence": sequence + 20,
                  "last_dom_sequence": samples[-1]["sequence"], "identity_bound": True, "ordered": True}}


def stage_observation(name, input_record, control=None):
    original_name = input_record["target"]["name"]
    if name == "discovery":
        pair = read_fixture(original_name, start=100, request_sequence=1)
        collection = collection_folder_fixture()
        return {"home": {"passed": True}, "dom": dom_fixture(original_name, wire=pair), "reads": [pair],
            "collection_folder_reads": [collection],
            "collection_folder": {"id": CONTROLLER.LIBRARY, "type": "CollectionFolder", "subviews": ["movies", "movies", "folders"],
                "physical_exchange_id": collection["physical"]["id"], "frame_request_index": collection["frame"]["index"], "passed": True},
            "navigation": {"before_route": "/synthetic-home", "after_route": PAGE_ROUTE, "before_sequence": 0},
            "query_allowlist": [{key: copy.deepcopy(pair["physical"][key]) for key in ("kind", "route", "query", "shape_sha256")}],
            "socket": {"token_sha256": TOKEN_SHA, "connection_id": CONNECTION, "opened": 1, "seen": 1}}
    if name == "armed":
        samples = [{"sequence": index, "started_elapsed_ms": moment, "elapsed_ms": moment + 10,
            "observation": dom_fixture(original_name, started=moment)} for index, moment in enumerate(range(1000, 21000, 1000), 1)]
        return {"boundary": boundary_fixture(), "quiet": {"passed": True, "duration_ms": 20000, "catalog_requests": 0,
            "library_changed_messages": 0, "started_elapsed_ms": 1000, "completed_elapsed_ms": 21000,
            "samples": samples[:-1] + [dict(samples[-1], started_elapsed_ms=20900, elapsed_ms=21000,
                observation=dom_fixture(original_name, started=20900))]}}
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
    def test_collection_folder_navigation_requires_actual_status_subviews_and_click_boundary(self):
        value = input_fixture()
        stage = stage_fixture("discovery", value)
        proof = CONTROLLER.navigation_evidence(stage["observation"], value, TOKEN_SHA)
        self.assertEqual(proof, stage["observation"]["collection_folder"])
        for field, replacement in (("status", 404), ("terminal_status", 404), ("completed", False),
                                   ("method", "POST"), ("terminal", "failed"), ("request_sequence", 0)):
            changed = copy.deepcopy(stage)
            changed["observation"]["collection_folder_reads"][0]["physical"][field] = replacement
            with self.subTest(physical=field):
                self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, "discovery", None, ordinary_proof()))
        for subviews in (["movies", "folders"], ["folders", "movies", "movies"], []):
            changed = copy.deepcopy(stage)
            changed["observation"]["collection_folder_reads"][0]["physical"]["projection"]["collection_folder"]["Subviews"] = subviews
            with self.subTest(subviews=subviews):
                self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, "discovery", None, ordinary_proof()))

    def test_collection_folder_foreign_or_duplicate_reads_cannot_replace_automatic_http(self):
        value = input_fixture()
        for mutation in ("duplicate", "token", "document", "route", "unchanged-route"):
            changed = stage_fixture("discovery", value)
            pair = changed["observation"]["collection_folder_reads"][0]
            if mutation == "duplicate":
                changed["observation"]["collection_folder_reads"].append(copy.deepcopy(pair))
            elif mutation == "unchanged-route":
                changed["observation"]["navigation"]["before_route"] = PAGE_ROUTE
            else:
                pair["frame"][{"token": "token_sha256", "document": "document_id", "route": "page_route"}[mutation]] = "foreign"
            with self.subTest(mutation=mutation):
                self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, "discovery", None, ordinary_proof()))
        window = window_fixture()
        collection = collection_folder_fixture()
        for key in ("kind", "method", "terminal", "route", "query", "shape_sha256", "projection"):
            window["http"]["physical"][0][key] = copy.deepcopy(collection["physical"][key])
        for key in ("kind", "route", "shape_sha256"):
            window["http"]["frames"][0][key] = copy.deepcopy(collection["frame"][key])
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        self.assertNotEqual(CONTROLLER.window_evidence(window, value, reservation, stage_observation("discovery", value))["result"], "passed")

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
            self.assertEqual(CONTROLLER.validate_stage(stage, value, INPUT_SHA, CHILD, name, "6" * 64, ordinary_proof(), stage_observation("discovery", value)), stage["observation"])
            for key, replacement in (("version", True), ("input_sha256", "0" * 64), ("token_sha256", NATIVE_TOKEN_SHA),
                                     ("previous_control_sha256", None), ("node_process", dict(CHILD, pid=3000))):
                changed = copy.deepcopy(stage)
                changed[key] = replacement
                self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, name, "6" * 64, ordinary_proof(), stage_observation("discovery", value)))
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
            self.reject(lambda: CONTROLLER.validate_stage(changed, value, INPUT_SHA, CHILD, "armed", None, ordinary_proof(), stage_observation("discovery", value)))


class SingletonWireCardGuards(GuardTestCase):
    def test_empty_visible_containers_preserve_one_owned_card_container(self):
        input_record = input_fixture()
        discovery = stage_observation('discovery', input_record)
        discovery['dom']['visible_items_containers'] = 3
        original = copy.deepcopy(discovery)
        CONTROLLER.discovery_identity(discovery, input_record, TOKEN_SHA)
        self.assertEqual(discovery, original)
        self.assertEqual(discovery['dom']['visible_items_containers'], 3)
        discovery['dom']['visible_items_containers'] = 0
        CONTROLLER.discovery_identity(discovery, input_record, TOKEN_SHA)
        for field in ('visible_card_containers', 'visible_cards', 'visible_title_buttons', 'target_title_count'):
            changed = copy.deepcopy(discovery)
            changed['dom'][field] = 2
            with self.subTest(field=field):
                self.reject(lambda: CONTROLLER.discovery_identity(changed, input_record, TOKEN_SHA))

    def test_movies_identity_rejects_a_consistently_relabelled_detail_or_duplicate_parent_route(self):
        input_record = input_fixture()
        for route in (PAGE_ROUTE.replace('!/videos', '!/item'), PAGE_ROUTE + '&ParentId=' + CONTROLLER.LIBRARY,
                      PAGE_ROUTE.replace(CONTROLLER.SERVER, 'f0' * 16)):
            discovery = stage_observation('discovery', input_record)
            discovery['dom']['route'] = discovery['navigation']['after_route'] = route
            for pair in discovery['reads']:
                pair['frame']['page_route'] = route
            with self.subTest(route=route):
                self.reject(lambda: CONTROLLER.discovery_identity(discovery, input_record, TOKEN_SHA))

    def test_discovery_wire_must_start_after_the_actual_navigation_boundary(self):
        input_record = input_fixture()
        for side in ('physical', 'frame'):
            discovery = stage_observation('discovery', input_record)
            discovery['reads'][0][side]['request_sequence'] = discovery['navigation']['before_sequence']
            with self.subTest(side=side):
                self.reject(lambda: CONTROLLER.discovery_identity(discovery, input_record, TOKEN_SHA))

    def test_preflight_singleton_is_scoped_to_the_owned_movie_library(self):
        snapshot = baseline_snapshot()
        for row in snapshot['database']['tables']['items']:
            if row['id'] not in (CONTROLLER.ITEM, CONTROLLER.LIBRARY):
                row['library_id'] = format(2000, '032x')
        profile = profile_fixture(snapshot)
        CONTROLLER.validate_singleton_movie(snapshot, profile)
        snapshot['database']['tables']['items'][-1]['name'] = profile['item']['name']
        CONTROLLER.validate_singleton_movie(snapshot, profile)
        snapshot['database']['tables']['items'][-1]['library_id'] = CONTROLLER.LIBRARY
        self.reject(lambda: CONTROLLER.validate_singleton_movie(snapshot, profile))

    def test_dom_proof_recomputes_wire_ids_hashes_phase_and_unique_visible_card(self):
        input_record = input_fixture()
        discovery = stage_observation('discovery', input_record)
        CONTROLLER.discovery_identity(discovery, input_record, TOKEN_SHA)
        for field, value in (('physical_exchange_id', 999), ('frame_request_index', 999), ('body_sha256', 'f0' * 32),
                             ('shape_sha256', 'f0' * 32), ('request_sha256', 'f0' * 32), ('phase', 'restored'), ('message_id', 'other')):
            changed = copy.deepcopy(discovery)
            changed['dom']['wire_identity'][field] = value
            with self.subTest(wire_field=field):
                self.reject(lambda: CONTROLLER.discovery_identity(changed, input_record, TOKEN_SHA))
        for field, value in (('visible_items_containers', 33), ('visible_card_containers', 2), ('visible_cards', 2), ('visible_title_buttons', 2),
                             ('target_title_count', 2), ('explicit_identity_consistent', False), ('observed_title', 'Wrong title'),
                             ('started_elapsed_ms', 99)):
            changed = copy.deepcopy(discovery)
            changed['dom'][field] = value
            with self.subTest(dom_field=field):
                self.reject(lambda: CONTROLLER.discovery_identity(changed, input_record, TOKEN_SHA))

    def test_discovery_rejects_ids_subset_wrong_phase_and_more_than_one_response_item(self):
        for mutation in ('ids-subset', 'wrong-phase', 'multiple-items'):
            input_record = input_fixture()
            discovery = stage_observation('discovery', input_record)
            pair = discovery['reads'][0]
            if mutation == 'ids-subset':
                pair['physical']['query'] = [['Ids', CONTROLLER.ITEM]]
                pair['physical']['shape_sha256'] = digest([pair['physical']['route'], pair['physical']['query']])
                pair['frame']['shape_sha256'] = pair['physical']['shape_sha256']
            elif mutation == 'wrong-phase':
                pair['physical']['phase'] = pair['frame']['phase'] = 'restored'
            else:
                pair['physical']['projection']['count'] = 2
            with self.subTest(mutation=mutation):
                self.reject(lambda: CONTROLLER.discovery_identity(discovery, input_record, TOKEN_SHA))

    def test_restoration_samples_cannot_reuse_discovery_or_previous_window_identity(self):
        input_record = input_fixture()
        discovery = stage_observation('discovery', input_record)
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))['public']
        for proof in (discovery['dom']['wire_identity'], window_fixture('forward')['dom'][1]['observation']['wire_identity']):
            window = window_fixture('restored')
            for sample in window['dom'][1:]:
                sample['observation']['wire_identity'] = copy.deepcopy(proof)
            result = CONTROLLER.window_evidence(window, input_record, reservation, discovery)
            self.assertNotEqual(result['result'], 'passed')

    def test_window_keeps_exact_target_get_after_its_own_message(self):
        input_record = input_fixture()
        discovery = stage_observation('discovery', input_record)
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))['public']
        window = window_fixture('forward')
        pair = {'physical': window['http']['physical'][0], 'frame': window['http']['frames'][0],
                'complete': True, 'unambiguous': True}
        route = '/Users/' + CONTROLLER.B + '/Items/' + CONTROLLER.ITEM
        physical, frame = pair['physical'], pair['frame']
        physical.update(kind='target', route=route, query=[], shape_sha256=digest([route, []]))
        frame.update(kind='target', route=route, shape_sha256=physical['shape_sha256'])
        raw = CONTROLLER.canonical(physical['projection']['target'])
        physical['projection'].update(body_sha256=CONTROLLER.sha(raw), body_bytes=len(raw))
        for sample in window['dom'][1:]:
            sample['observation'] = dom_fixture(reservation['marker_name'], wire=pair, phase='forward',
                message_id=window['events']['browser'][0]['message']['MessageId'], started=sample['started_elapsed_ms'])
        self.assertEqual(CONTROLLER.window_evidence(window, input_record, reservation, discovery)['result'], 'passed')


class WindowAndIntentGuards(GuardTestCase):
    def test_complete_windows_accept_delivery_before_ack_and_zero_frame_index(self):
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        for name in ("forward", "restored"):
            window = window_fixture(name)
            original = copy.deepcopy(window)
            result = CONTROLLER.window_evidence(window, input_fixture(), reservation, stage_observation("discovery", input_fixture()))
            self.assertEqual(result, {key: window[key] for key in ("result", "outcome", "proof")})
            self.assertEqual(window, original)
            self.assertEqual(set(window["http"]["pairs"][0]),
                {"frame_request_index", "physical_exchange_id", "complete", "unambiguous"})
            self.assertLess(window["events"]["browser"][0]["elapsed_ms"], window["boundary"]["response_completed_elapsed_ms"])

    def test_compact_http_references_reject_missing_wrong_duplicate_or_invented_links(self):
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        for defect in ("missing", "frame", "physical", "duplicate", "raw-frame", "raw-physical", "flag", "type",
                       "missing-field", "extra-field", "full-pair", "no-websocket"):
            window = window_fixture()
            http, reference = window["http"], window["http"]["pairs"][0]
            if defect == "missing":
                http["pairs"] = []
            elif defect in ("frame", "no-websocket"):
                reference["frame_request_index"] = 99
                if defect == "no-websocket":
                    window["events"]["browser"] = []
            elif defect == "physical":
                reference["physical_exchange_id"] = 99
            elif defect == "duplicate":
                http["pairs"].append(copy.deepcopy(reference))
            elif defect == "raw-frame":
                http["frames"] = []
            elif defect == "raw-physical":
                http["physical"] = []
            elif defect == "flag":
                reference["unambiguous"] = False
            elif defect == "type":
                reference["complete"] = 1
            elif defect == "missing-field":
                del reference["unambiguous"]
            elif defect == "extra-field":
                reference["projection"] = {}
            else:
                http["pairs"] = [{"frame": http["frames"][0], "physical": http["physical"][0],
                                  "complete": True, "unambiguous": True}]
            with self.subTest(defect=defect):
                self.reject(lambda: CONTROLLER.window_evidence(window, input_fixture(), reservation,
                    stage_observation("discovery", input_fixture())))

    def test_ambiguous_raw_transfers_cannot_be_promoted_by_compact_references(self):
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        for inventory in ("frames", "physical"):
            window = window_fixture()
            http = window["http"]
            duplicate = copy.deepcopy(http[inventory][0])
            duplicate["index" if inventory == "frames" else "id"] += 1
            http[inventory].append(duplicate)
            with self.subTest(inventory=inventory):
                self.reject(lambda: CONTROLLER.window_evidence(window, input_fixture(), reservation,
                    stage_observation("discovery", input_fixture())))
                http["pairs"] = [{"frame_request_index": frame["index"], "physical_exchange_id": None,
                                  "complete": False, "unambiguous": False} for frame in http["frames"]]
                self.assertEqual(CONTROLLER.window_evidence(window, input_fixture(), reservation,
                    stage_observation("discovery", input_fixture())), {"result": "not_observed_within_window",
                        "outcome": "automatic_http_not_observed_within_window", "proof": None})
                duplicate["index" if inventory == "frames" else "id"] = http[inventory][0]["index" if inventory == "frames" else "id"]
                self.reject(lambda: CONTROLLER.window_evidence(window, input_fixture(), reservation,
                    stage_observation("discovery", input_fixture())))

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
                sample = window["dom"][-1]
                sample["observation"] = dom_fixture(sample["observation"]["expected_name"], passed=False,
                    started=sample["started_elapsed_ms"])
            self.assertEqual(CONTROLLER.window_evidence(window, input_fixture(), reservation, stage_observation("discovery", input_fixture())),
                             {"result": "not_observed_within_window", "outcome": outcome, "proof": None})

    def test_bound_dom_with_false_passed_is_rejected(self):
        reservation = reservation_fixture(profile_fixture(baseline_snapshot()))["public"]
        window = window_fixture()
        window["dom"][-1]["observation"]["passed"] = False
        self.reject(lambda: CONTROLLER.window_evidence(window, input_fixture(), reservation,
            stage_observation("discovery", input_fixture())))

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
            self.reject(lambda: CONTROLLER.window_evidence(window, input_fixture(), reservation, stage_observation("discovery", input_fixture())))

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
        run.state = schema_state()
        run.candidate = copy.deepcopy(CANDIDATE)
        run.authority = authority_fixture()["authority"]
        run.catalog = catalog_fixture(run.before)
        run.current_authority = copy.deepcopy(run.before)
        run.media, run.primary = {"synthetic_media": "unchanged"}, {"synthetic_primary": "unchanged"}
        run.credentials = {"viewer": {"username": "synthetic-viewer", "password": "synthetic-viewer-password"},
                           "admin": {"username": "synthetic-admin", "password": "synthetic-admin-password"}}
        run.js_sources = input_fixture()["source_closure"]
        run.sources = {str(CONTROLLER.TOOL / "observe-client-library-changed-source55.py"): OPERATOR_SHA256,
                       str(CONTROLLER.TOOL / "memory-reader.py"): "9" * 64}
        run.op = memory_structure_reader()
        run.op.preservation_snapshot = lambda _state, schema: self.snapshot(schema)
        run.profile_reader = types.SimpleNamespace(validate_structure=Mock(side_effect=AssertionError("The schema27 profile validator must never run.")))
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

    def snapshot(self, schema=28):
        if schema != 28:
            raise AssertionError("The controller requested a different schema snapshot.")
        if not self.browser_created:
            return baseline_snapshot()
        fixture = ledger_fixture(self.metadata_phase, False)
        value = fixture["after"]
        tables = value["database"]["tables"]
        next(row for row in tables["sessions"] if row["id"] == SESSION)["client_capabilities"] = {}
        if not self.native_created:
            tables["sessions"] = [row for row in tables["sessions"] if row["id"] != NATIVE_SESSION]
            tables["activity_entries"] = [row for row in tables["activity_entries"] if row["id"] != 169]
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
        self.assertEqual((len(final["sessions"]), len(final["devices"]), len(final["activity_entries"])), (77, 65, 173))

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
    result = unittest.TextTestRunner(stream=sys.stderr, verbosity=2, resultclass=MemoryTextTestResult).run(suite)
    passed = result.wasSuccessful() and not result.skipped
    report = {"suite": "client-library-changed-source55-controller-guards", "status": "passed" if passed else "failed",
              "operator_sha256": OPERATOR_SHA256, "guard_sha256": GUARD_SHA256, "test_count": result.testsRun,
              "failures": len(result.failures), "errors": len(result.errors), "skips": len(result.skipped)}
    sys.stdout.write(json.dumps(report, sort_keys=True, separators=(",", ":")) + "\n")
    raise SystemExit(0 if passed else 1)
