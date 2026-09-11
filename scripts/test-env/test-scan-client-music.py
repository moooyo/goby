#!/usr/bin/env python3
"""Exercise the music scan operator guards in memory through authorized SSH.

The rows and credentials below are synthetic unit fixtures, not a PostgreSQL
catalog or evidence from a deployed server. Every external operation is fenced
while the guard cases run. The scan source, frozen fixture source, and this
suite are read before the in-memory cases begin. No production schema catalog
is manufactured or accepted by these synthetic business-row cases.
"""

from __future__ import annotations

import base64
import argparse
import contextlib
import copy
import datetime as dt
from decimal import Decimal
import hashlib
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import socket
import signal
import stat
import subprocess
import sys
import types
import time
import unittest
from unittest.mock import patch

sys.dont_write_bytecode = True
RUNNER = None
FIXTURE = None
SOURCE_LINES = {}


class MemoryFixtureError(Exception):
    """Represent the frozen fixture transport's own failure type."""


class EffectFence(contextlib.ExitStack):
    """Fail a case if the real operator reaches an external side effect."""

    def __enter__(self):
        super().__enter__()
        import builtins
        import fcntl
        self.violations = []
        self.enter_context(patch.object(linecache, "checkcache", lambda filename=None: None))
        self.enter_context(patch.object(linecache, "getlines", lambda filename, module_globals=None:
                                       SOURCE_LINES.get(str(filename), [])))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")),
            (os, ("open", "fdopen", "stat", "lstat", "mkdir", "chmod", "chown", "fchown", "fsync",
                  "replace", "rename", "unlink", "remove", "rmdir", "kill", "system", "popen",
                  "read", "write", "listdir", "scandir", "fork", "execv", "execve", "umask")),
            (Path, ("lstat", "stat", "exists", "resolve", "readlink", "read_bytes", "read_text",
                    "write_bytes", "write_text", "mkdir", "iterdir", "rglob", "unlink")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (fcntl, ("flock", "lockf", "fcntl", "ioctl")),
            (signal, ("signal", "pidfd_send_signal")),
            (time, ("sleep",)),
        )
        for owner, names in targets:
            for name in names:
                label = owner.__name__ + "." + name

                def denied(*_args, _label=label, **_kwargs):
                    self.violations.append(_label)
                    raise AssertionError("Unexpected external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *args):
        result = super().__exit__(*args)
        if self.violations:
            raise AssertionError("External effects attempted: " + ", ".join(self.violations))
        return result


class MemoryNativeAPI:
    """Preserve NativeAPI's response callback and credential-update ordering."""

    harness = None

    def __init__(self, state):
        self.state, self.cookie, self.csrf, self.requests = state, None, None, 0

    def received(self, route, method, code, raw, cookie):
        pass

    def request(self, route, method="GET", body=None, expected=(200,), admin=False):
        if admin:
            RUNNER.require(self.cookie and self.csrf, "Synthetic native request lacks its administrator credential.")
        record = {"route": route, "method": method, "body": copy.deepcopy(body),
                  "expected": expected, "admin": admin, "cookie": self.cookie, "csrf": self.csrf}
        self.harness.calls.append(record)
        self.harness.timeline.append(("send", copy.deepcopy(record)))
        self.requests += 1
        if not self.harness.responses:
            raise AssertionError("The test did not script this transport request.")
        response = self.harness.responses.pop(0)
        if isinstance(response, Exception):
            raise response
        code, data, cookie = response
        raw = data if isinstance(data, bytes) else json.dumps(data).encode() if data is not None else b""
        self.received(route, method, code, raw, cookie)
        if code not in expected:
            raise MemoryFixtureError("Synthetic native response returned an unexpected status.")
        parsed = json.loads(raw) if raw else None
        if route == "/admin/v1/session" and method == "POST":
            RUNNER.require(cookie and cookie.startswith("goby_session=") and parsed.get("CSRFToken"),
                           "Synthetic native login omitted its credential.")
            self.cookie, self.csrf = cookie.split(";", 1)[0], parsed["CSRFToken"]
        return parsed


class MemoryOperator:
    """Supply explicit in-memory collaborators, never permissive Mock fallbacks."""

    ACCOUNTS = {"admin": "m3e-client-administrator", "viewer": "m3e-client-viewer"}
    PUBLIC = "http://127.0.0.1:18198"
    PORT = 18198
    ROLE = "goby_client_m3e"
    WORK = Path("/opt/goby-test/exec-work-m3e")
    MEDIA_ROOT = Path("/opt/goby-fixtures/client-m3e")
    FixtureError = MemoryFixtureError

    def __init__(self):
        self.calls, self.responses, self.timeline, self.validations, self.comparisons = [], [], [], [], []
        self.clock_value = "2026-09-11T10:00:00+00:00"
        self.NativeAPI = type("BoundMemoryNativeAPI", (MemoryNativeAPI,), {"harness": self})
        self.baseline = {"objects": [{"kind": "sequence", "name": name,
            "value": {"increment": 1, "cache": 1, "cycle": False, "max": 9223372036854775807}}
            for name in ("catalog_entities_id_seq", "activity_entries_id_seq")]}

    @staticmethod
    def require(condition, message):
        RUNNER.require(condition, message)

    @staticmethod
    def precise_json(raw):
        def pairs(values):
            result = {}
            for key, value in values:
                if key in result:
                    raise MemoryFixtureError("The synthetic response repeats a JSON key.")
                result[key] = value
            return result

        def invalid(_value):
            raise MemoryFixtureError("The synthetic response contains a non-finite JSON number.")

        return json.loads(raw, object_pairs_hook=pairs, parse_float=Decimal, parse_constant=invalid)

    @staticmethod
    def canonical_json(value):
        def encode(item):
            if item is None:
                return "null"
            if type(item) is bool:
                return "true" if item else "false"
            if type(item) in (int, Decimal):
                return str(item)
            if isinstance(item, str):
                return json.dumps(item, ensure_ascii=False)
            if isinstance(item, list):
                return "[" + ",".join(encode(value) for value in item) + "]"
            if isinstance(item, dict):
                return "{" + ",".join(encode(key) + ":" + encode(item[key]) for key in sorted(item)) + "}"
            raise TypeError("The unit fixture contains a non-JSON value.")
        return encode(value).encode()

    @classmethod
    def equal_json(cls, left, right):
        return cls.canonical_json(left) == cls.canonical_json(right)

    def utc(self):
        return self.clock_value

    def persist(self, *args, **kwargs):
        if args:
            self.receipt["phase"] = args[0]
        self.timeline.append(("persist", copy.deepcopy(self.receipt)))

    def check(self, *args, **kwargs):
        self.timeline.append(("check", None))

    @staticmethod
    def schema25_binding(state):
        return state.get("schema25_source")

    def trusted_schema_baseline(self, version, binding=None):
        RUNNER.require(version == 25, "The unit scenario requested another schema.")
        return copy.deepcopy(self.baseline)

    def validate_preservation_snapshot(self, snapshot, version, state):
        """Replace catalog loading only; the actual preservation comparer stays real."""
        self.validations.append(snapshot)
        RUNNER.require(snapshot.get("schema") == version == state.get("schema") == 25,
                       "The synthetic preservation record selected another schema.")
        RUNNER.require(snapshot.get("runtime_sha256") == state["runtime_sha256"] and
                       snapshot.get("browser_sha256") == state["browser_sha256"],
                       "A private credential fixture changed.")
        database = snapshot["database"]
        RUNNER.require(database.get("unsupported") is False and
                       set(database["tables"]) == set(database["metadata"]["columns"]) == self.table_inventory,
                       "The synthetic snapshot inventory is incomplete.")

    def compare_preservation_snapshots(self, before, after, source_schema, target_schema, state):
        self.comparisons.append((copy.deepcopy(before), copy.deepcopy(after)))
        with patch.object(FIXTURE, "validate_preservation_snapshot", self.validate_preservation_snapshot):
            return FIXTURE.compare_preservation_snapshots(before, after, source_schema, target_schema, state)


class MusicScanGuardTests(unittest.TestCase):
    def setUp(self):
        self.enterContext(EffectFence())
        self.output = self.enterContext(contextlib.redirect_stdout(io.StringIO()))
        self.error_output = self.enterContext(contextlib.redirect_stderr(io.StringIO()))
        self.op = MemoryOperator()
        self.state = {"schema": 25, "phase": "ready", "admin_id": "a" * 32,
                      "viewer_id": "b" * 32, "server_id": "c" * 32,
                      "runtime_sha256": "d" * 64, "browser_sha256": "e" * 64,
                      "libraries": {"movies": {"id": "1" * 32}, "tv": {"id": "2" * 32},
                                    "music": {"id": "3" * 32}}}
        self.args = types.SimpleNamespace(candidate_sha256="a" * 64, operator_sha256="b" * 64, schema=25)
        self.token = base64.urlsafe_b64encode(b"\x07" * 32).rstrip(b"=").decode()
        self.cookie = "goby_session=" + self.token
        self.csrf = hashlib.sha256(("goby:admin:csrf:" + self.token).encode()).hexdigest()
        self.login_body = {"Name": self.op.ACCOUNTS["admin"], "Password": "synthetic-unit-password"}
        self.login_user = {"Id": self.state["admin_id"], "Name": self.op.ACCOUNTS["admin"],
                           "IsAdministrator": True, "IsDisabled": False, "HasPassword": True,
                           "CreatedAt": "2026-09-10T10:00:00+00:00"}
        self.scan_route = "/admin/v1/libraries/" + self.state["libraries"]["music"]["id"] + "/scan"
        self.job = {"Id": "4" * 32, "LibraryId": self.state["libraries"]["music"]["id"],
                    "ForceProbe": False, "Status": "pending", "Error": "", "Scanned": 0,
                    "Added": 0, "Updated": 0, "CreatedAt": "2026-09-11T10:00:03+00:00",
                    "StartedAt": None, "FinishedAt": None}

    def reject(self, callback):
        with self.assertRaises((RUNNER.ScanError, MemoryFixtureError, FIXTURE.FixtureError)):
            callback()

    def tearDown(self):
        output = self.output.getvalue() + self.error_output.getvalue()
        for secret in (self.token, self.csrf, self.login_body["Password"]):
            self.assertFalse(secret in output, "The operator exposed a synthetic credential.")

    def new_api(self, receipt=None):
        self.receipt = {"previous_job_ids": []} if receipt is None else receipt
        self.op.receipt = self.receipt
        return RUNNER.ScopedAPI(self.op, self.state, self.receipt, self.op.persist, self.op.check)

    def login(self, api):
        self.op.responses.append((200, {"User": self.login_user, "CSRFToken": self.csrf},
                                  self.cookie + "; Path=/admin; HttpOnly; SameSite=Strict"))
        api.request("/admin/v1/session", "POST", self.login_body)

    def scan(self, api, job=None):
        self.op.responses.append((202, {"Job": self.job if job is None else job}, None))
        return api.request(self.scan_route, "POST", {}, expected=(202,), admin=True)

    def completed_job(self):
        return dict(self.job, Status="completed", Scanned=2, Updated=2,
                    StartedAt="2026-09-11T10:00:04+00:00", FinishedAt="2026-09-11T10:00:05+00:00")

    def assert_durable_before_send(self, flag):
        send_indexes = [index for index, (kind, value) in enumerate(self.op.timeline)
                        if kind == "send" and value["method"] == "POST" and
                        (value["route"] == self.scan_route if flag == "scan_requested"
                         else value["route"] == "/admin/v1/session")]
        self.assertEqual(len(send_indexes), 1)
        preceding = [value for kind, value in self.op.timeline[:send_indexes[0]] if kind == "persist"]
        self.assertTrue(any(value.get(flag) is True for value in preceding), "The intent was not durable before sending.")

    def snapshot(self):
        """Model business rows only; do not invent a trusted production catalog."""
        old = "2026-09-10T10:00:00+00:00"
        music_id = self.state["libraries"]["music"]["id"]
        album_id, mp3_id, flac_id = "6" * 32, "7" * 32, "8" * 32
        music_root = "9" * 32
        tables = {name: [] for name in ("libraries", "library_roots", "items", "item_metadata_state",
            "catalog_entities", "item_entities", "users", "user_settings", "user_item_data", "sessions",
            "scan_jobs", "activity_entries", "task_definitions", "task_triggers", "task_runs",
            "task_run_children", "task_run_requests", "task_occurrences", "play_sessions", "item_images",
            "item_subtitles", "devices", "application_keys", "application_key_clients", "server_settings",
            "schema_migrations", "encoding_jobs")}
        for key, collection, name, folder in (("movies", "movies", "M3e Client Movies", "Movies"),
                                               ("tv", "tvshows", "M3e Client Television", "TV"),
                                               ("music", "music", "M3e Client Music", "Music")):
            library_id = self.state["libraries"][key]["id"]
            tables["libraries"].append({"id": library_id, "name": name, "collection_type": collection,
                                       "created_at": old, "last_scan_at": old})
            tables["library_roots"].append({"id": music_root if key == "music" else key + "-root",
                "library_id": library_id, "path": str(self.op.MEDIA_ROOT / folder),
                "allowed_path": str(self.op.MEDIA_ROOT), "relative_path": folder})

        def item(identifier, library_id, kind, name, folder, parent=None, path="", relative="", media=None):
            return {"id": identifier, "library_id": library_id,
                    "root_id": music_root if library_id == music_id and identifier != music_id else None,
                    "parent_id": parent, "type": kind, "name": name, "sort_name": name.lower(),
                    "overview": "", "is_folder": folder, "path": path, "relative_path": relative,
                    "index_number": 0, "parent_index_number": 0, "media": media,
                    "file_identity": "" if folder else "synthetic-file-identity-" + identifier,
                    "file_size": 0 if folder else 4096, "modified_at": None if folder else old,
                    "created_at": old, "updated_at": old, "local_metadata": None,
                    "local_metadata_hash": "", "local_metadata_path": ""}

        tables["items"] = [item(music_id, music_id, "CollectionFolder", "M3e Client Music", True),
                           item(album_id, music_id, "MusicAlbum", "Music", True, music_id, relative="//album/root")]
        for identifier, filename, codec in ((mp3_id, "M3e Client Audio.mp3", "mp3"),
                                             (flac_id, "M3e Client Audio.flac", "flac")):
            media = {"Container": codec, "DurationTicks": 1200000000, "Size": 4096, "Bitrate": 128000,
                     "ProbeVersion": 4, "FileChangeTimeNs": 1700000000000000000,
                     "Streams": [{"Index": 0, "Type": "Audio", "Codec": codec, "SampleRate": 48000,
                                  "Channels": 2, "Bitrate": 128000}],
                     "AudioSeekIndex": {"Version": 1, "Entries": [{"Offset": 100, "Ticks": 0}]}}
            tables["items"].append(item(identifier, music_id, "Audio", "M3e Client Audio", False,
                album_id, str(self.op.MEDIA_ROOT / "Music" / filename), filename, media))
        tables["items"].extend((item("movie-item", self.state["libraries"]["movies"]["id"], "Movie", "Keep Movie", False,
                                    media={"Container": "mkv", "Streams": [{"Codec": "h264"}]}),
                                item("episode-item", self.state["libraries"]["tv"]["id"], "Episode", "Keep Episode", False,
                                     media={"Container": "mkv", "Streams": [{"Codec": "hevc"}]})))
        for row in tables["items"]:
            tables["item_metadata_state"].append({"item_id": row["id"], "revision": 1,
                "automatic": {"Name": row["name"], "SortName": row["sort_name"], "Overview": row["overview"]},
                "source_key": {"Hash": "", "NFOPath": "", "Type": row["type"], "IsFolder": row["is_folder"],
                               "ParentId": row["parent_id"], "Path": row["path"], "RootId": row["root_id"],
                               "RelativePath": row["relative_path"], "HasNFO": False},
                "overrides": {}, "locked_values": {}, "effective": None, "last_edited_by": None,
                "last_edited_at": None, "updated_at": old, "music_source": {}})
        tables["users"] = [{"id": self.state[key + "_id"], "name": self.op.ACCOUNTS[key],
            "normalized_name": self.op.ACCOUNTS[key], "password_hash": "synthetic-account-hash-" + key,
            "has_password": True, "is_administrator": key == "admin", "is_disabled": False,
            "policy": {"EnableMediaPlayback": True}, "configuration": {"AudioLanguagePreference": "en"},
            "management_revision": 3, "created_at": old, "updated_at": old} for key in ("admin", "viewer")]
        tables["user_settings"] = [{"user_id": self.state["viewer_id"], "settings": {
            "PlaybackRate": Decimal("1.00000000000000000000001"), "EnableNextUp": True}, "updated_at": old}]
        tables["user_item_data"] = [{"user_id": self.state["viewer_id"], "item_id": mp3_id,
                                "is_favorite": True, "played": True, "play_count": 4, "position_ticks": 120}]
        tables["catalog_entities"] = [{"id": 1, "kind": "Person", "name": "Keep Person", "normalized_name": "keep person",
            "normalized_hash": "\\x" + hashlib.sha256(b"keep person").hexdigest(), "created_at": old}]
        tables["item_entities"] = [{"item_id": "movie-item", "entity_id": 1, "position": 1,
            "display_name": "Keep Person", "role": "Actor", "credit_type": "Actor", "sort_order": 0, "credit_group": 0}]
        tables["sessions"] = [{"id": "old-session", "user_id": self.state["viewer_id"], "kind": "emby",
            "token_hash": "\\x" + "f" * 64, "client_name": "Keep Client", "device_id": "keep-device",
            "device_name": "Keep Device", "client_version": "old", "created_at": old, "last_seen_at": old,
            "expires_at": "2026-10-10T10:00:00+00:00", "revoked_at": None, "client_capabilities": {},
            "device_registry_id": None}]
        tables["scan_jobs"] = [{"id": "old-scan", "library_id": music_id, "status": "Completed", "error": "",
            "scanned": 2, "added": 2, "updated": 0, "force_probe": False, "created_at": old,
            "started_at": old, "finished_at": old, "task_child_id": None, "cancel_requested": False}]
        tables["activity_entries"] = [self.activity_row(20, "session.login", "native", "user",
            self.state["viewer_id"], "old-session", "session", "old-session", old, count=1)]
        tables["server_settings"] = [{"key": "server_id", "value": self.state["server_id"], "created_at": old, "updated_at": old}]
        tables["schema_migrations"] = [{"version": 25, "name": "0025_music_artists.sql", "applied_at": old}]
        columns = {name: list(rows[0]) if rows else [] for name, rows in tables.items()}
        self.op.table_inventory = set(tables)
        metadata = {"captured_at": "2026-09-11T10:00:00+00:00", "database": self.op.ROLE,
            "server_version_num": 170011, "schemas": ["public"],
            "public_schema": {"oid": 2200, "owner": "pg_database_owner", "acl": None}, "columns": columns,
            "relations": {name: {"oid": index + 1000, "owner": self.op.ROLE, "acl": None, "column_acl": []}
                          for index, name in enumerate(tables)}}
        return {"schema": 25, "runtime_sha256": self.state["runtime_sha256"],
            "browser_sha256": self.state["browser_sha256"], "added_viewer_credentials": None,
            "recovery": {"master.key": {"sha256": "f" * 64, "size": 32}},
            "database": {"metadata": metadata, "tables": tables, "unsupported": False,
                         "catalog": [{"kind": "unit_fixture", "name": "synthetic-business-rows", "value": {}}],
                         "sequences": {"catalog_entities_id_seq": {"last_value": 10, "is_called": True},
                                       "activity_entries_id_seq": {"last_value": 20, "is_called": True},
                                       "devices_id_seq": {"last_value": 4, "is_called": True}}}}

    def prepared_history(self, snapshot, second_user=False):
        """Add a complete unstarted playback row and its revoked ordinary owner."""
        tables = snapshot["database"]["tables"]
        user_id = self.state["viewer_id"]
        if second_user:
            user_id = "c" * 32
            tables["users"].append(dict(tables["users"][1], id=user_id,
                                        name="Synthetic AV Viewer", normalized_name="synthetic av viewer"))
        credential_id, playback_id = ("f" * 32, "0" * 32) if second_user else ("d" * 32, "e" * 32)
        credential = dict(tables["sessions"][0], id=credential_id, user_id=user_id,
                          token_hash="\\x" + hashlib.sha256(("prepared-history-" + credential_id).encode()).hexdigest(),
                          revoked_at="2026-09-11T03:32:42+00:00")
        playback = {"id": playback_id, "user_id": user_id, "auth_session_id": credential_id,
            "device_id": credential["device_id"], "item_id": "movie-item", "media_source_id": "movie-item",
            "state": "Prepared", "position_ticks": 0, "duration_ticks": 1200000000, "counted": False,
            "created_at": "2026-09-11T03:30:00+00:00", "updated_at": "2026-09-11T03:30:00+00:00",
            "expires_at": "2026-09-11T11:30:00+00:00", "started_at": None, "stopped_at": None,
            "player_state": {}, "client_correlated": False, "application_client_id": None}
        tables["sessions"].append(credential)
        tables["play_sessions"].append(playback)
        snapshot["database"]["metadata"]["columns"]["play_sessions"] = list(playback)
        return credential, playback

    @staticmethod
    def activity_row(identifier, action, source, actor_kind, actor_id, credential_id,
                     resource_kind, resource_id, created_at, count=0, state=""):
        return {"id": identifier, "created_at": created_at, "action": action, "source": source, "severity": "Info",
                "actor_kind": actor_kind, "actor_id": actor_id, "actor_credential_id": credential_id,
                "resource_kind": resource_kind, "resource_id": resource_id, "request_id": "", "revision": 0,
                "affected_count": count, "state": state, "changed_fields": []}

    def scan_model(self, controls=None):
        before = self.snapshot()
        if controls:
            item = next(row for row in before["database"]["tables"]["items"] if row["id"] == "7" * 32)
            record = next(row for row in before["database"]["tables"]["item_metadata_state"] if row["item_id"] == item["id"])
            record.update(locked_values=copy.deepcopy(controls.get("locked_values", {})),
                          overrides=copy.deepcopy(controls.get("overrides", {})), last_edited_by=self.state["admin_id"],
                          last_edited_at="2026-09-10T12:00:00+00:00")
            effective = dict(record["locked_values"], **record["overrides"])
            record["effective"] = effective or None
            for key, column in (("Name", "name"), ("SortName", "sort_name"), ("Overview", "overview")):
                item[column] = effective.get(key, item[column])
        plan = RUNNER.prepare_music_plan(self.op, before, self.state)
        after = copy.deepcopy(before)
        after["database"]["metadata"]["captured_at"] = "2026-09-11T10:00:09+00:00"
        tables = after["database"]["tables"]
        receipt = {"previous_job_ids": ["old-scan"], "product_version": "synthetic-candidate",
                   "login_status": 200, "scan_status": 202, "logout_status": 204, "token_readback_status": 401,
                   "token_sha256": hashlib.sha256(self.token.encode()).hexdigest(), "job_id": self.job["Id"],
                   "job_result": self.completed_job(),
                   "windows": {"login": {"before": "2026-09-11T10:00:01+00:00", "after": "2026-09-11T10:00:02+00:00"},
                               "scan": {"before": "2026-09-11T10:00:03+00:00", "after": "2026-09-11T10:00:06+00:00"},
                               "logout": {"before": "2026-09-11T10:00:07+00:00", "after": "2026-09-11T10:00:08+00:00"}}}
        session_id = "5" * 32
        tables["sessions"].append({"id": session_id, "user_id": self.state["admin_id"],
            "token_hash": "\\x" + receipt["token_sha256"], "kind": "admin", "client_name": "Goby Dashboard",
            "device_id": "goby-dashboard", "device_name": "Web browser", "client_version": receipt["product_version"],
            "created_at": "2026-09-11T10:00:01+00:00", "last_seen_at": "2026-09-11T10:00:01+00:00",
            "expires_at": "2026-09-12T10:00:01+00:00", "revoked_at": "2026-09-11T10:00:07+00:00",
            "client_capabilities": {}, "device_registry_id": None})
        tables["scan_jobs"].append({"id": self.job["Id"], "library_id": plan["music_id"], "status": "Completed",
            "error": "", "scanned": 2, "added": 0, "updated": 2, "force_probe": False, "cancel_requested": False,
            "task_child_id": None, "created_at": "2026-09-11T10:00:03+00:00",
            "started_at": "2026-09-11T10:00:04+00:00", "finished_at": "2026-09-11T10:00:05+00:00"})
        names = {plan["album_id"]: "M3e Synthetic Album",
                 plan["audio"]["M3e Client Audio.mp3"]: "M3e MP3",
                 plan["audio"]["M3e Client Audio.flac"]: "M3e FLAC"}
        artist, album = "M3e Synthetic Artist", "M3e Synthetic Album"
        records = {row["item_id"]: row for row in tables["item_metadata_state"]}
        for item in tables["items"]:
            if item["id"] not in names:
                continue
            record = records[item["id"]]
            controls = dict(record["locked_values"], **record["overrides"])
            name = names[item["id"]]
            source = {"Version": 1, "Name": name, "Album": album, "Artists": [artist],
                      "AlbumArtists": [artist] if item["type"] == "MusicAlbum" else []}
            item["name"], item["sort_name"] = controls.get("Name", name), controls.get("SortName", name.lower())
            item["updated_at"] = "2026-09-11T10:00:04+00:00"
            if item["type"] == "Audio":
                item["media"]["EmbeddedMusic"] = {"Version": 1, "Title": name, "Album": album, "Artist": artist}
            record["automatic"].update(Name=name, SortName=name.lower(), Album=album,
                                       Artists=[artist], AlbumArtists=source["AlbumArtists"])
            record["source_key"]["MusicSourceHash"] = hashlib.sha256(
                json.dumps(source, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()
            record["effective"] = {key: value for key, value in source.items() if key != "Version"}
            record["effective"].update(controls)
            record["music_source"] = source
            record["revision"] += 1
            record["updated_at"] = "2026-09-11T10:00:04+00:00"
        artist_id = 11
        tables["catalog_entities"].append({"id": artist_id, "kind": "MusicArtist", "name": artist,
            "normalized_name": artist.lower(), "normalized_hash": "\\x" + hashlib.sha256(artist.lower().encode()).hexdigest(),
            "created_at": "2026-09-11T10:00:04+00:00"})
        for item_id in names:
            tables["item_entities"].append({"item_id": item_id, "entity_id": artist_id, "position": 1,
                "display_name": artist, "role": "", "credit_type": "Artist", "sort_order": None, "credit_group": 1})
        tables["item_entities"].append({"item_id": plan["album_id"], "entity_id": artist_id, "position": 1,
            "display_name": artist, "role": "", "credit_type": "AlbumArtist", "sort_order": None, "credit_group": 2})
        next(row for row in tables["libraries"] if row["id"] == plan["music_id"])["last_scan_at"] = "2026-09-11T10:00:05+00:00"
        events = (("session.login", "native", "user", self.state["admin_id"], session_id, "session", session_id, "01", 1, ""),
                  ("scan.requested", "native", "user", self.state["admin_id"], session_id, "scan", self.job["Id"], "03", 0, ""),
                  ("scan.finished", "system", "system", "", "", "scan", self.job["Id"], "05", 0, "completed"),
                  ("session.revoked", "native", "user", self.state["admin_id"], session_id, "session", session_id, "07", 1, ""))
        for index, (action, origin, actor, owner, credential, resource, target, second, count, state) in enumerate(events, 21):
            tables["activity_entries"].append(self.activity_row(index, action, origin, actor, owner, credential,
                resource, target, "2026-09-11T10:00:" + second + ".500000+00:00", count=count, state=state))
        after["database"]["sequences"]["catalog_entities_id_seq"] = {"last_value": 13, "is_called": True}
        after["database"]["sequences"]["activity_entries_id_seq"] = {"last_value": 24, "is_called": True}
        return before, after, receipt, plan

    def compare(self, before, after, receipt, plan):
        return RUNNER.compare_scan_snapshots(self.op, before, after, self.state, receipt, plan)

    def test_only_exact_schema25_and_sha256_arguments_are_accepted(self):
        RUNNER.validate_arguments(self.args)
        for field, values in (("schema", (24, 26, True, "25", None)),
                              ("candidate_sha256", ("", "a" * 63, "a" * 65, "A" * 64, "g" * 64, None)),
                              ("operator_sha256", ("", "b" * 63, "b" * 65, "B" * 64, "z" * 64, None))):
            for value in values:
                changed = copy.copy(self.args)
                setattr(changed, field, value)
                with self.subTest(field=field, value=value):
                    self.reject(lambda: RUNNER.validate_arguments(changed))

    def test_one_normal_scan_has_durable_intents_and_uses_one_native_credential(self):
        api = self.new_api()
        self.login(api)
        self.scan(api)
        self.op.responses.append((200, {"Items": [self.completed_job()], "TotalRecordCount": 1}, None))
        result = api.poll_job(float("inf"))
        self.assertEqual(result["Id"], self.job["Id"])
        self.op.responses.extend(((204, None, "goby_session=; Path=/admin; Max-Age=0"),
                                  (401, {"Error": {"Code": "invalid_credentials"}}, None)))
        api.logout()
        self.assert_durable_before_send("login_requested")
        self.assert_durable_before_send("scan_requested")
        self.assertEqual(self.receipt["job_id"], self.job["Id"])
        self.assertTrue(self.receipt["token_sha256"] == hashlib.sha256(self.token.encode()).hexdigest())
        self.assertEqual((self.receipt["logout_status"], self.receipt["token_readback_status"]), (204, 401))
        self.assertTrue(all(call["cookie"] == self.cookie for call in self.op.calls[1:]))
        self.assertFalse(any(secret in json.dumps(self.receipt) for secret in
                             (self.token, self.csrf, self.login_body["Password"])))

    def test_scope_rejects_foreign_libraries_force_probe_and_unrelated_mutations(self):
        api = self.new_api()
        self.login(api)
        initial = len(self.op.calls)
        cases = (
            ("/admin/v1/libraries/" + self.state["libraries"]["movies"]["id"] + "/scan", "POST", {}, (202,)),
            (self.scan_route, "POST", {"ForceProbe": True}, (202,)),
            (self.scan_route, "POST", {"ForceProbe": False}, (202,)),
            (self.scan_route, "POST", None, (202,)),
            (self.scan_route + "?ForceProbe=false", "POST", {}, (202,)),
            (self.scan_route + "#owned", "POST", {}, (202,)),
            (self.scan_route, "POST", {}, (200, 202)),
            ("/admin/v1/libraries", "POST", {}, (201,)),
            ("/admin/v1/users", "POST", {}, (201,)),
            ("/admin/v1/users/" + self.state["viewer_id"], "PUT", {}, (200,)),
            ("/admin/v1/jobs/" + self.job["Id"] + "/cancel", "POST", {}, (202,)),
            ("/admin/v1/tasks/owned/runs", "POST", {}, (202,)),
            ("/emby/Library/Refresh", "POST", {}, (204,)),
            ("/admin/v1/session", "DELETE", {}, (204,)),
            ("/admin/v1/jobs", "GET", {}, (200,)),
        )
        for route, method, body, expected in cases:
            with self.subTest(route=route, method=method, body=body):
                self.reject(lambda: api.request(route, method, body, expected=expected, admin=True))
        self.assertEqual(len(self.op.calls), initial)
        self.assertNotIn("scan_requested", self.receipt)

    def test_protected_requests_require_the_acknowledged_login(self):
        api = self.new_api()
        for route, method, body, expected in (("/admin/v1/jobs", "GET", None, (200,)),
                                               (self.scan_route, "POST", {}, (202,)),
                                               ("/admin/v1/session", "DELETE", None, (204,))):
            with self.subTest(route=route):
                self.reject(lambda: api.request(route, method, body, expected=expected, admin=True))
        self.assertEqual(len(self.op.calls), 0)

    def test_login_identity_and_request_shape_cannot_be_changed(self):
        for body, admin, expected in ((dict(self.login_body, Name="foreign-user"), False, (200,)),
                                       ({"Name": self.op.ACCOUNTS["admin"]}, False, (200,)),
                                       (dict(self.login_body, IsAdministrator=True), False, (200,)),
                                       (self.login_body, True, (200,)),
                                       (self.login_body, False, (200, 201))):
            api = self.new_api()
            with self.subTest(admin=admin, expected=expected):
                self.reject(lambda: api.request("/admin/v1/session", "POST", body, expected=expected, admin=admin))
        self.assertEqual(len(self.op.calls), 0)
        for field, value in (("Id", "f" * 32), ("Name", "foreign-user"),
                             ("IsAdministrator", False), ("IsDisabled", True)):
            api = self.new_api()
            self.op.responses.append((200, {"User": dict(self.login_user, **{field: value}), "CSRFToken": self.csrf}, self.cookie))
            with self.subTest(field=field):
                self.reject(lambda: api.request("/admin/v1/session", "POST", self.login_body))
            self.assertFalse(api.verified_admin)
            self.assertNotIn("token_sha256", self.receipt)

    def test_login_unknown_outcome_cannot_be_retried(self):
        api = self.new_api()
        self.op.responses.append(TimeoutError("Synthetic response timeout."))
        with self.assertRaises(TimeoutError):
            api.request("/admin/v1/session", "POST", self.login_body)
        self.reject(lambda: api.request("/admin/v1/session", "POST", self.login_body))
        self.assertEqual(len(self.op.calls), 1)
        self.assert_durable_before_send("login_requested")
        self.assertNotIn("token_sha256", self.receipt)

    def test_acknowledged_login_and_scan_cannot_be_repeated(self):
        api = self.new_api()
        self.login(api)
        self.reject(lambda: api.request("/admin/v1/session", "POST", self.login_body))
        self.scan(api)
        self.reject(lambda: api.request(self.scan_route, "POST", {}, expected=(202,), admin=True))
        self.assertEqual(len(self.op.calls), 2)

    def test_scan_unknown_response_is_never_retried_or_adopted_from_a_job_list(self):
        api = self.new_api()
        self.login(api)
        self.op.responses.append(TimeoutError("Synthetic response timeout."))
        with self.assertRaises(TimeoutError):
            api.request(self.scan_route, "POST", {}, expected=(202,), admin=True)
        self.reject(lambda: api.request(self.scan_route, "POST", {}, expected=(202,), admin=True))
        self.op.responses.append((200, {"Items": [self.completed_job()]}, None))
        self.reject(lambda: api.poll_job(float("inf")))
        self.assertEqual(len(self.op.calls), 2)
        self.assertNotIn("job_id", self.receipt)
        self.assert_durable_before_send("scan_requested")

    def test_scan_binds_only_a_real_202_with_a_new_owned_job(self):
        for status, job, previous in ((200, self.job, []), (409, self.job, []),
                                      (202, dict(self.job, Id="invalid"), []),
                                      (202, dict(self.job, LibraryId="f" * 32), []),
                                      (202, dict(self.job, ForceProbe=True), []),
                                      (202, self.job, [self.job["Id"]])):
            api = self.new_api({"previous_job_ids": previous})
            self.login(api)
            self.op.responses.append((status, {"Job": job}, None))
            with self.subTest(status=status, previous=bool(previous), job_id=job["Id"]):
                self.reject(lambda: api.request(self.scan_route, "POST", {}, expected=(202,), admin=True))
                self.reject(lambda: api.request(self.scan_route, "POST", {}, expected=(202,), admin=True))
            self.assertNotIn("job_id", self.receipt)

    def test_polling_never_replaces_the_returned_job_with_another_job(self):
        invalid_lists = ([dict(self.completed_job(), Id="f" * 32)], [],
                         [self.completed_job(), self.completed_job()],
                         [dict(self.completed_job(), LibraryId="f" * 32)],
                         [dict(self.completed_job(), ForceProbe=True)])
        for jobs in invalid_lists:
            api = self.new_api()
            self.login(api)
            self.scan(api)
            self.op.responses.append((200, {"Items": jobs}, None))
            with self.subTest(case_size=len(jobs)):
                self.reject(lambda: api.poll_job(float("inf")))
            self.assertEqual(self.receipt["job_id"], self.job["Id"])
            self.assertNotIn("job_result", self.receipt)

    def test_polling_requires_the_complete_expected_normal_scan_result(self):
        for field, value in (("Status", "failed"), ("Status", "cancelled"), ("Status", "interrupted"),
                             ("Status", "unknown"), ("Error", "Synthetic warning."),
                             ("Scanned", 3), ("Added", 1), ("Added", False), ("Updated", 1)):
            api = self.new_api()
            self.login(api)
            self.scan(api)
            self.op.responses.append((200, {"Items": [dict(self.completed_job(), **{field: value})]}, None))
            with self.subTest(field=field, value=value):
                self.reject(lambda: api.poll_job(float("inf")))
            self.assertNotIn("job_result", self.receipt)

    def test_poll_timeout_does_not_repeat_scan_or_cancel_the_job(self):
        api = self.new_api()
        self.login(api)
        self.scan(api)
        self.op.responses.append((200, {"Items": [self.job]}, None))
        self.reject(lambda: api.poll_job(-1))
        self.assertEqual([call["method"] for call in self.op.calls], ["POST", "POST", "GET"])
        self.assertEqual(self.receipt["job_id"], self.job["Id"])

    def test_logout_replays_the_original_cookie_and_rejects_missing_cookie_denial(self):
        api = self.new_api()
        self.login(api)
        api.transport.cookie, api.transport.csrf = None, None
        self.op.responses.extend(((204, None, "goby_session=; Max-Age=0"),
                                  (401, {"Error": {"Code": "authentication_required"}}, None)))
        self.reject(api.logout)
        self.assertTrue(all(call["cookie"] == self.cookie for call in self.op.calls[-2:]))
        self.assertNotIn("token_readback_status", self.receipt)
        self.reject(api.logout)
        self.assertEqual(len(self.op.calls), 3)

    def test_persistence_failure_prevents_send_and_never_retries_an_acknowledged_scan(self):
        api = self.new_api()
        self.op.receipt = self.receipt

        def unavailable(phase):
            raise MemoryFixtureError("Synthetic receipt persistence is unavailable.")

        api.persist = unavailable
        self.reject(lambda: api.request("/admin/v1/session", "POST", self.login_body))
        self.assertEqual(len(self.op.calls), 0)
        self.reject(lambda: api.request("/admin/v1/session", "POST", self.login_body))
        api = self.new_api()
        self.login(api)

        def acknowledgement_failure(phase):
            if phase == "scan_acknowledged":
                raise MemoryFixtureError("Synthetic acknowledgement could not be saved.")
            self.op.persist(phase)

        api.persist = acknowledgement_failure
        self.op.responses.append((202, {"Job": self.job}, None))
        self.reject(lambda: api.request(self.scan_route, "POST", {}, expected=(202,), admin=True))
        self.reject(lambda: api.request(self.scan_route, "POST", {}, expected=(202,), admin=True))
        self.assertEqual(len(self.op.calls), 2)
        self.assertEqual(self.receipt["job_id"], self.job["Id"])

    def test_plan_binds_only_the_old_music_album_and_two_files(self):
        before = self.snapshot()
        original = copy.deepcopy(before)
        plan = RUNNER.prepare_music_plan(self.op, before, self.state)
        self.assertEqual(plan, {"music_id": "3" * 32, "album_id": "6" * 32,
                               "audio": {"M3e Client Audio.mp3": "7" * 32, "M3e Client Audio.flac": "8" * 32},
                               "music_ids": ["6" * 32, "7" * 32, "8" * 32], "artist_id": 11})
        self.assertTrue(self.op.equal_json(before, original))
        self.assertEqual(len(self.op.calls), 0)

    def test_plan_rejects_wrong_schema_library_root_and_item_scope(self):
        for kind in ("schema", "library-id", "library-name", "collection-type", "root", "extra-root",
                     "missing-track", "extra-track", "track-parent", "track-path", "album-identity"):
            before = self.snapshot()
            tables = before["database"]["tables"]
            state = copy.deepcopy(self.state)
            if kind == "schema":
                before["schema"] = 24
            elif kind == "library-id":
                state["libraries"]["music"]["id"] = state["libraries"]["movies"]["id"]
            elif kind == "library-name":
                tables["libraries"][2]["name"] = "Foreign Music"
            elif kind == "collection-type":
                tables["libraries"][2]["collection_type"] = "mixed"
            elif kind == "root":
                tables["library_roots"][2]["path"] = "/opt/unowned/Music"
            elif kind == "extra-root":
                tables["library_roots"].append(dict(tables["library_roots"][2], id="unowned-root"))
            elif kind == "missing-track":
                tables["items"].pop(2)
            elif kind == "extra-track":
                tables["items"].append(dict(tables["items"][2], id="unowned-track"))
            elif kind == "track-parent":
                tables["items"][2]["parent_id"] = "movie-item"
            elif kind == "track-path":
                tables["items"][2]["path"] += ".other"
            else:
                tables["items"][1]["relative_path"] = "//album/other"
            with self.subTest(kind=kind):
                self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, state))

    def test_plan_rejects_prior_music_facts_and_unsupported_controls(self):
        for kind in ("embedded", "music-source", "music-hash", "nfo", "artist", "old-credit", "projection",
                     "artist-override", "all-fields-lock", "lock-data", "non-string-control"):
            before = self.snapshot()
            tables = before["database"]["tables"]
            item, record = tables["items"][2], tables["item_metadata_state"][2]
            if kind == "embedded":
                item["media"]["EmbeddedMusic"] = {"Version": 1}
            elif kind == "music-source":
                record["music_source"] = {"Version": 1}
            elif kind == "music-hash":
                record["source_key"]["MusicSourceHash"] = "old-source"
            elif kind == "nfo":
                item["local_metadata"] = {}
            elif kind == "artist":
                tables["catalog_entities"][0]["kind"] = "MusicArtist"
            elif kind == "old-credit":
                tables["item_entities"].append(dict(tables["item_entities"][0], item_id=item["id"]))
            elif kind == "projection":
                record["effective"] = {"Artists": ["Unaccounted Artist"]}
            elif kind == "artist-override":
                record["overrides"] = {"Artists": ["Unaccounted Artist"]}
            elif kind == "all-fields-lock":
                record["locked_values"] = {"*": True}
            elif kind == "lock-data":
                record["locked_values"] = {"LockData": True}
            else:
                record["overrides"] = {"Name": False}
            with self.subTest(kind=kind):
                self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, self.state))

    def test_plan_refuses_active_scans_tasks_children_and_playback(self):
        for table, field, values in (("scan_jobs", "status", ("Queued", "Running")),
                                     ("task_runs", "state", ("pending", "running", "stopping")),
                                     ("task_run_children", "state", ("waiting", "queued", "running")),
                                     ("play_sessions", "state", ("Prepared", "Playing", "Paused"))):
            for value in values:
                before = self.snapshot()
                before["database"]["tables"][table].append({"id": "active-operation", field: value})
                with self.subTest(table=table, value=value):
                    self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, self.state))

    def test_plan_accepts_only_revoked_unstarted_ordinary_prepared_history(self):
        before = self.snapshot()
        expected = RUNNER.prepare_music_plan(self.op, before, self.state)
        self.prepared_history(before)
        self.prepared_history(before, second_user=True)
        saved = copy.deepcopy(before)
        self.assertEqual(RUNNER.prepare_music_plan(self.op, before, self.state), expected)
        self.assertTrue(self.op.equal_json(before, saved))
        self.assertEqual(len(before["database"]["tables"]["play_sessions"]), 2)
        self.assertEqual(self.op.timeline, [])

    def test_plan_refuses_prepared_history_with_incomplete_or_foreign_credentials(self):
        for change in ("missing-reference", "null-reference", "empty-reference", "unknown-reference",
                       "missing-user", "null-user", "empty-user", "missing-owner", "duplicate-owner",
                       "different-user", "null-owner-user", "missing-kind", "native-kind", "key-kind",
                       "missing-revocation", "not-revoked", "bad-revocation", "naive-revocation", "future-revocation"):
            before = self.snapshot()
            credential, playback = self.prepared_history(before)
            if change == "missing-reference":
                del playback["auth_session_id"]
            elif change in {"null-reference", "empty-reference", "unknown-reference"}:
                playback["auth_session_id"] = {"null-reference": None, "empty-reference": "",
                                               "unknown-reference": "1" * 32}[change]
            elif change == "missing-user":
                del playback["user_id"]
            elif change in {"null-user", "empty-user"}:
                playback["user_id"] = None if change == "null-user" else ""
            elif change == "missing-owner":
                before["database"]["tables"]["sessions"].remove(credential)
            elif change == "duplicate-owner":
                before["database"]["tables"]["sessions"].append(copy.deepcopy(credential))
            elif change in {"different-user", "null-owner-user"}:
                credential["user_id"] = self.state["admin_id"] if change == "different-user" else None
            elif change == "missing-kind":
                del credential["kind"]
            elif change in {"native-kind", "key-kind"}:
                credential["kind"] = "admin" if change == "native-kind" else "application_key"
            elif change == "missing-revocation":
                del credential["revoked_at"]
            else:
                credential["revoked_at"] = {"not-revoked": None, "bad-revocation": "invalid",
                    "naive-revocation": "2026-09-11T03:32:42", "future-revocation": "2026-09-11T10:00:01+00:00"}[change]
            with self.subTest(change=change):
                self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, self.state))
        self.assertEqual(self.op.timeline, [])

    def test_plan_refuses_started_counted_or_client_key_prepared_history(self):
        for field, values in (("started_at", ("2026-09-11T03:30:01+00:00", "")),
                              ("counted", (True, 0, "false", None)),
                              ("application_client_id", ("1" * 32, "")),
                              ("state", ("Playing", "Paused"))):
            for value in values:
                before = self.snapshot()
                _, playback = self.prepared_history(before)
                playback[field] = value
                with self.subTest(field=field, value=value):
                    self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, self.state))
        for field in ("started_at", "counted", "application_client_id"):
            before = self.snapshot()
            _, playback = self.prepared_history(before)
            del playback[field]
            with self.subTest(missing=field):
                self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, self.state))
        self.assertEqual(self.op.timeline, [])

    def test_prepared_history_is_preserved_in_every_column_through_the_scan(self):
        before, after, receipt, plan = self.scan_model()
        for snapshot in (before, after):
            self.prepared_history(snapshot)
            self.prepared_history(snapshot, second_user=True)
        self.assertEqual(RUNNER.prepare_music_plan(self.op, before, self.state), plan)
        saved = copy.deepcopy((before, after))
        self.compare(before, after, receipt, plan)
        self.assertTrue(all(self.op.equal_json(left, right) for left, right in zip(saved, (before, after))))
        for table in ("play_sessions", "sessions"):
            rows = before["database"]["tables"][table]
            for row in rows:
                for column in row:
                    changed = copy.deepcopy(after)
                    target = next(value for value in changed["database"]["tables"][table] if value["id"] == row["id"])
                    target[column] = "unowned-change"
                    with self.subTest(table=table, row=row["id"], column=column):
                        self.reject(lambda: self.compare(before, changed, receipt, plan))
            changed = copy.deepcopy(after)
            changed["database"]["tables"][table] = [row for row in changed["database"]["tables"][table]
                                                       if row["id"] != rows[-1]["id"]]
            with self.subTest(table=table, deletion=True):
                self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_plan_refuses_unreviewed_sequence_behavior(self):
        for field, value in (("increment", 2), ("cache", 2), ("cycle", True), ("max", 12)):
            before = self.snapshot()
            original = copy.deepcopy(self.op.baseline)
            self.op.baseline["objects"][0]["value"][field] = value
            with self.subTest(field=field):
                self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, self.state))
            self.op.baseline = original
        for value in ({"last_value": True, "is_called": True}, {"last_value": 10, "is_called": 1},
                      {"last_value": "10", "is_called": True}):
            before = self.snapshot()
            before["database"]["sequences"]["catalog_entities_id_seq"] = value
            with self.subTest(sequence=value):
                self.reject(lambda: RUNNER.prepare_music_plan(self.op, before, self.state))

    def test_exact_music_delta_uses_the_real_preservation_comparer_without_mutating_inputs(self):
        before, after, receipt, plan = self.scan_model()
        saved = copy.deepcopy((before, after, receipt, plan))
        report = self.compare(before, after, receipt, plan)
        self.assertEqual(report["job_id"], self.job["Id"])
        self.assertEqual(report["music_role_rows_added"], 4)
        self.assertEqual(len(self.op.comparisons), 1)
        self.assertTrue(all(self.op.equal_json(left, right) for left, right in zip(saved, (before, after, receipt, plan))))
        expected = copy.deepcopy(before)
        expected["database"]["metadata"]["captured_at"] = after["database"]["metadata"]["captured_at"]
        self.assertTrue(self.op.equal_json(self.op.comparisons[0][1], expected))

    def test_existing_name_sort_and_overview_controls_survive_the_scan(self):
        before, after, receipt, plan = self.scan_model({"locked_values": {"Name": "Locked Name", "Overview": "Keep Overview"},
                                                       "overrides": {"Name": "User Name", "SortName": "User Sort"}})
        self.compare(before, after, receipt, plan)
        for field in ("overrides", "locked_values", "last_edited_by", "last_edited_at"):
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["item_metadata_state"][2][field] = "unowned-change"
            with self.subTest(field=field):
                self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_every_user_preference_userdata_and_other_table_value_is_preserved(self):
        before, after, receipt, plan = self.scan_model()
        protected = set(before["database"]["tables"]) - {"items", "item_metadata_state", "catalog_entities",
            "item_entities", "sessions", "scan_jobs", "activity_entries", "libraries"}
        for table in sorted(protected):
            rows = before["database"]["tables"][table]
            if not rows:
                changed = copy.deepcopy(after)
                changed["database"]["tables"][table].append({"unowned": "addition"})
                with self.subTest(table=table, addition=True):
                    self.reject(lambda: self.compare(before, changed, receipt, plan))
                continue
            for index, row in enumerate(rows):
                for column in row:
                    changed = copy.deepcopy(after)
                    changed["database"]["tables"][table][index][column] = {"unowned": "change"}
                    with self.subTest(table=table, row=index, column=column):
                        self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_movie_tv_and_collection_rows_keep_every_column(self):
        before, after, receipt, plan = self.scan_model()
        for table, key in (("items", "id"), ("item_metadata_state", "item_id")):
            for index, row in enumerate(before["database"]["tables"][table]):
                if row[key] in plan["music_ids"]:
                    continue
                for column in row:
                    changed = copy.deepcopy(after)
                    changed["database"]["tables"][table][index][column] = "unowned-change"
                    with self.subTest(table=table, row=index, column=column):
                        self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_music_technical_facts_and_existing_item_identities_are_not_exempted(self):
        before, after, receipt, plan = self.scan_model()
        for index in (1, 2, 3):
            for column in before["database"]["tables"]["items"][index]:
                changed = copy.deepcopy(after)
                changed["database"]["tables"]["items"][index][column] = "unowned-change"
                with self.subTest(row=index, column=column):
                    self.reject(lambda: self.compare(before, changed, receipt, plan))
        for column in before["database"]["tables"]["items"][2]["media"]:
            changed = copy.deepcopy(after)
            changed["database"]["tables"]["items"][2]["media"][column] = {"unowned": "technical-change"}
            with self.subTest(technical=column):
                self.reject(lambda: self.compare(before, changed, receipt, plan))
        changed = copy.deepcopy(after)
        changed["database"]["tables"]["items"][2]["media"]["EmbeddedMusic"]["Artist"] = "Unowned Artist"
        self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_music_source_projection_revision_and_control_fields_are_exact(self):
        before, after, receipt, plan = self.scan_model()
        for column in before["database"]["tables"]["item_metadata_state"][2]:
            changed = copy.deepcopy(after)
            value = 3 if column == "revision" else "unowned-change"
            changed["database"]["tables"]["item_metadata_state"][2][column] = value
            with self.subTest(column=column):
                self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_old_sessions_jobs_entities_and_audit_rows_keep_every_column(self):
        before, after, receipt, plan = self.scan_model()
        for table in ("sessions", "scan_jobs", "catalog_entities", "item_entities", "activity_entries", "libraries"):
            for index, row in enumerate(before["database"]["tables"][table]):
                for column in row:
                    if table == "libraries" and row["id"] == plan["music_id"] and column == "last_scan_at":
                        continue
                    changed = copy.deepcopy(after)
                    changed["database"]["tables"][table][index][column] = "unowned-change"
                    with self.subTest(table=table, row=index, column=column):
                        self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_new_session_and_job_require_exact_token_owner_scope_and_timestamps(self):
        before, after, receipt, plan = self.scan_model()
        for table in ("sessions", "scan_jobs"):
            row = after["database"]["tables"][table][-1]
            for column in row:
                changed = copy.deepcopy(after)
                changed["database"]["tables"][table][-1][column] = "unowned-change"
                with self.subTest(table=table, column=column):
                    self.reject(lambda: self.compare(before, changed, receipt, plan))
            changed = copy.deepcopy(after)
            changed["database"]["tables"][table].append(dict(row, id="additional-unowned-row"))
            with self.subTest(table=table, addition=True):
                self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_artist_and_credit_delta_cannot_relabel_old_entities_or_add_other_associations(self):
        before, after, receipt, plan = self.scan_model()
        for table, start in (("catalog_entities", 1), ("item_entities", 1)):
            for index in range(start, len(after["database"]["tables"][table])):
                for column in after["database"]["tables"][table][index]:
                    changed = copy.deepcopy(after)
                    changed["database"]["tables"][table][index][column] = "unowned-change"
                    with self.subTest(table=table, row=index, column=column):
                        self.reject(lambda: self.compare(before, changed, receipt, plan))
        changed = copy.deepcopy(after)
        changed["database"]["tables"]["item_entities"].append(copy.deepcopy(changed["database"]["tables"]["item_entities"][-1]))
        self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_four_new_audits_have_exact_actor_resource_action_and_window(self):
        before, after, receipt, plan = self.scan_model()
        for index in range(1, 5):
            for column in after["database"]["tables"]["activity_entries"][index]:
                changed = copy.deepcopy(after)
                changed["database"]["tables"]["activity_entries"][index][column] = "unowned-change"
                with self.subTest(event=index, column=column):
                    self.reject(lambda: self.compare(before, changed, receipt, plan))
        changed = copy.deepcopy(after)
        changed["database"]["tables"]["activity_entries"].append(dict(changed["database"]["tables"]["activity_entries"][-1], id=25))
        self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_sequences_require_exact_insert_and_conflict_insert_consumption(self):
        before, after, receipt, plan = self.scan_model()
        for name, sequence in after["database"]["sequences"].items():
            for value in (dict(sequence, last_value=sequence["last_value"] + 1),
                          dict(sequence, last_value=sequence["last_value"] - 1),
                          dict(sequence, is_called=False), dict(sequence, is_called=1)):
                changed = copy.deepcopy(after)
                changed["database"]["sequences"][name] = value
                with self.subTest(sequence=name, value=value):
                    self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_receipt_requires_actual_statuses_terminal_job_and_ordered_database_windows(self):
        before, after, receipt, plan = self.scan_model()
        for field, value in (("login_status", 201), ("scan_status", 200), ("logout_status", 200),
                             ("token_readback_status", 200), ("token_sha256", "f" * 64), ("job_id", "f" * 32),
                             ("product_version", "foreign-version")):
            changed = copy.deepcopy(receipt)
            changed[field] = value
            with self.subTest(field=field):
                self.reject(lambda: self.compare(before, after, changed, plan))
        for field, value in (("Id", "f" * 32), ("LibraryId", "f" * 32), ("Status", "failed"),
                             ("Error", "Synthetic warning."), ("ForceProbe", True), ("Added", False),
                             ("FinishedAt", "2026-09-11T10:00:04+00:00")):
            changed = copy.deepcopy(receipt)
            changed["job_result"][field] = value
            with self.subTest(job_result=field):
                self.reject(lambda: self.compare(before, after, changed, plan))
        for window in ("login", "scan", "logout"):
            changed = copy.deepcopy(receipt)
            changed["windows"][window]["after"] = "2026-09-10T00:00:00+00:00"
            with self.subTest(window=window):
                self.reject(lambda: self.compare(before, after, changed, plan))

    def test_catalog_recovery_credentials_relations_and_table_inventories_are_preserved(self):
        before, after, receipt, plan = self.scan_model()
        for kind in ("catalog", "schema", "unsupported", "runtime", "browser", "recovery", "added-viewer",
                     "database", "relation-oid", "relation-acl", "columns", "extra-table"):
            changed = copy.deepcopy(after)
            database = changed["database"]
            if kind == "catalog":
                database["catalog"][0]["value"] = {"unowned": True}
            elif kind == "schema":
                changed["schema"] = 24
            elif kind == "unsupported":
                database["unsupported"] = True
            elif kind in ("runtime", "browser"):
                changed[kind + "_sha256"] = "f" * 64
            elif kind == "recovery":
                changed["recovery"]["master.key"]["sha256"] = "0" * 64
            elif kind == "added-viewer":
                changed["added_viewer_credentials"] = {"unowned": "credential"}
            elif kind == "database":
                database["metadata"]["database"] = "foreign_database"
            elif kind == "relation-oid":
                database["metadata"]["relations"]["items"]["oid"] += 1
            elif kind == "relation-acl":
                database["metadata"]["relations"]["items"]["acl"] = ["unowned=arwdDxt"]
            elif kind == "columns":
                database["metadata"]["columns"]["items"].append("unowned_column")
            else:
                database["tables"]["unowned_table"] = []
                database["metadata"]["columns"]["unowned_table"] = []
            with self.subTest(kind=kind):
                self.reject(lambda: self.compare(before, changed, receipt, plan))

    def test_operator_loader_checks_the_hash_before_executing_exact_bytes(self):
        raw = ("from pathlib import Path\n"
               "WORK = Path('/opt/goby-test/exec-work-m3e')\n"
               "PG_PORT = 15432\nPORT = 18198\n"
               "MARKER = 'goby-m3e-client-acceptance-v1'\n").encode()
        calls = []

        def read_once():
            calls.append("read")
            if len(calls) > 1:
                raise AssertionError("The pinned operator was read twice.")
            return raw

        with patch.object(RUNNER, "protected_operator_bytes", read_once):
            module = RUNNER.load_operator(hashlib.sha256(raw).hexdigest())
        self.assertEqual((module.PG_PORT, module.PORT), (15432, 18198))
        self.assertEqual(calls, ["read"])
        refused = b"raise AssertionError('Unpinned source executed.')\n"
        with patch.object(RUNNER, "protected_operator_bytes", return_value=refused):
            self.reject(lambda: RUNNER.load_operator("0" * 64))

    def test_operator_loader_rejects_correctly_hashed_foreign_fixture_targets(self):
        for field, value in (("WORK", "Path('/opt/foreign')"), ("PG_PORT", "5432"),
                             ("PORT", "8096"), ("MARKER", "'foreign-fixture'")):
            values = {"WORK": "Path('/opt/goby-test/exec-work-m3e')", "PG_PORT": "15432",
                      "PORT": "18198", "MARKER": "'goby-m3e-client-acceptance-v1'"}
            values[field] = value
            raw = ("from pathlib import Path\n" + "\n".join(name + " = " + entry for name, entry in values.items()) + "\n").encode()
            with self.subTest(field=field), patch.object(RUNNER, "protected_operator_bytes", return_value=raw):
                self.reject(lambda: RUNNER.load_operator(hashlib.sha256(raw).hexdigest()))

    def test_candidate_check_rejects_wrong_pins_schema_process_and_pending_upgrade(self):
        raw = b"Synthetic pinned fixture bytes.\n"
        args = copy.copy(self.args)
        args.operator_sha256 = hashlib.sha256(raw).hexdigest()
        operation = RUNNER.MusicScan(args)
        owner = {"device": 1, "inode": 2}
        state = {"marker": "goby-m3e-client-acceptance-v1", "phase": "ready", "schema": 25,
                 "binary_sha256": args.candidate_sha256, "work": str(RUNNER.WORK), "work_identity": owner,
                 "upgrade": {"phase": "complete"}, "process": {"pid": 123, "start_ticks": 456}}
        operator = types.SimpleNamespace(STATE_FILE=RUNNER.WORK / "synthetic-state.json", MARKER=state["marker"],
            read=lambda _path: b"synthetic-state", directory=lambda *_args: copy.deepcopy(owner),
            verify_fixture_directories=lambda _state: None, verify_database=lambda _state: None,
            verify_service=lambda _state: copy.deepcopy(state["process"]), added_viewer_receipt=lambda _state: {"user_id": "owned-av"})
        operation.op, operation.state, operation.state_bytes = operator, copy.deepcopy(state), b"synthetic-state"
        with patch.object(RUNNER, "protected_operator_bytes", return_value=raw):
            operation.check()
            for field, value in (("schema", 24), ("binary_sha256", "f" * 64), ("work", "/opt/foreign"),
                                 ("marker", "foreign"), ("phase", "pending"), ("start_pending", True),
                                 ("upgrade", {"phase": "pending"}), ("process", {"pid": 999})):
                operation.state = dict(copy.deepcopy(state), **{field: value})
                with self.subTest(field=field):
                    self.reject(operation.check)
            operation.state = copy.deepcopy(state)
            operation.args.operator_sha256 = "f" * 64
            self.reject(operation.check)

    def test_receipt_tampering_and_unrecorded_receipt_prevent_publication(self):
        operation = RUNNER.MusicScan(self.args)
        publications = []
        owner = {"device": 1, "inode": 2}
        operation.receipt = {"phase": "prepared"}
        operation.receipt_bytes = b"owned receipt"
        operation.output_identity = owner
        operation.op = types.SimpleNamespace(read=lambda _path: b"unowned receipt", exists=lambda _path: True,
            directory=lambda *_args: owner, canonical_json=self.op.canonical_json,
            create=lambda *args: publications.append(args), sync=lambda *_args: None)
        self.reject(lambda: operation.persist("scan_requested"))
        self.assertEqual(publications, [])
        self.assertEqual(operation.events, 0)
        self.assertEqual(operation.receipt["phase"], "prepared")
        operation.receipt_bytes = None
        self.reject(lambda: operation.persist("scan_requested"))
        self.assertEqual(publications, [])
        self.assertEqual(operation.events, 0)

    def test_existing_operation_directory_is_never_resumed_or_replaced(self):
        operation = RUNNER.MusicScan(self.args)
        calls = []
        lock_info = types.SimpleNamespace(st_dev=1, st_ino=2)
        operator = types.SimpleNamespace(LOCK=RUNNER.WORK / "synthetic-lock",
            host_inputs=lambda **kwargs: calls.append("host-inputs"), regular=lambda _path: lock_info,
            identity=lambda info: {"device": info.st_dev, "inode": info.st_ino},
            exists=lambda path: path == RUNNER.OUTPUT)
        with contextlib.ExitStack() as stack:
            stack.enter_context(patch.object(RUNNER, "load_operator", return_value=operator))
            stack.enter_context(patch.object(RUNNER.os, "umask", return_value=0o077))
            stack.enter_context(patch.object(RUNNER.os, "open", return_value=123))
            stack.enter_context(patch.object(RUNNER.os, "fstat", return_value=lock_info))
            stack.enter_context(patch.object(RUNNER.fcntl, "flock", return_value=None))
            self.reject(operation.prepare)
        self.assertEqual(calls, ["host-inputs"])
        self.assertIsNone(operation.state)
        self.assertIsNone(operation.receipt)
        self.assertIsNone(operation.api)
        operation.lock = None

    def test_run_finally_retires_original_token_after_an_unknown_scan_without_retry(self):
        api = self.new_api({"previous_job_ids": [], "windows": {}})
        operation = RUNNER.MusicScan(self.args)
        operation.op, operation.state, operation.receipt, operation.api = self.op, self.state, self.receipt, api
        operation.prepare = lambda: None
        operation.clock = lambda: "2026-09-11T10:00:07+00:00"

        def execute():
            api.request("/admin/v1/session", "POST", self.login_body)
            api.request(self.scan_route, "POST", {}, expected=(202,), admin=True)

        operation.execute = execute
        self.op.responses.extend(((200, {"User": self.login_user, "CSRFToken": self.csrf}, self.cookie),
                                  TimeoutError("Synthetic response timeout."), (204, None, "goby_session=; Max-Age=0"),
                                  (401, {"Error": {"Code": "invalid_credentials"}}, None)))
        self.assertEqual(operation.run(), 1)
        self.assertEqual([call["method"] for call in self.op.calls], ["POST", "POST", "DELETE", "GET"])
        self.assertTrue(all(call["cookie"] == self.cookie for call in self.op.calls[1:]))
        self.assertNotIn("job_id", self.receipt)
        self.assertEqual(self.receipt["token_readback_status"], 401)
        self.assertIsNone(api.original_cookie)
        self.assertIsNone(api.transport.cookie)


def main():
    global RUNNER, FIXTURE
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise SystemExit("Run these operator guards only through authorized root SSH on test-env.")
    import fcntl
    import http.client
    import pwd
    import re
    import secrets
    import traceback
    import urllib.parse
    source = Path(__file__).with_name("scan-client-music.py")
    fixture_source = Path(__file__).with_name("prepare-client-fixture.py")
    source_bytes, fixture_bytes, guard_bytes = source.read_bytes(), fixture_source.read_bytes(), Path(__file__).read_bytes()
    SOURCE_LINES[str(source)] = source_bytes.decode().splitlines(keepends=True)
    SOURCE_LINES[str(fixture_source)] = fixture_bytes.decode().splitlines(keepends=True)
    SOURCE_LINES[str(Path(__file__))] = guard_bytes.decode().splitlines(keepends=True)
    spec = importlib.util.spec_from_file_location("client_music_scan_operator", source)
    RUNNER = importlib.util.module_from_spec(spec)
    fixture_spec = importlib.util.spec_from_file_location("client_music_frozen_fixture", fixture_source)
    FIXTURE = importlib.util.module_from_spec(fixture_spec)
    sys.modules[spec.name], sys.modules[fixture_spec.name] = RUNNER, FIXTURE
    with EffectFence():
        exec(compile(fixture_bytes, str(fixture_source), "exec"), FIXTURE.__dict__)
        exec(compile(source_bytes, str(source), "exec"), RUNNER.__dict__)
    output = io.StringIO()
    result = unittest.TextTestRunner(stream=output, verbosity=2).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(MusicScanGuardTests))
    sys.stderr.write(output.getvalue())
    passed = result.wasSuccessful() and not result.skipped
    print(json.dumps({"tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "skipped": len(result.skipped), "passed": passed,
                      "operator_sha256": hashlib.sha256(source_bytes).hexdigest(),
                      "fixture_operator_sha256": hashlib.sha256(fixture_bytes).hexdigest(),
                      "guard_sha256": hashlib.sha256(guard_bytes).hexdigest(),
                      "database_mutations": 0, "service_actions": 0, "http_requests": 0}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
