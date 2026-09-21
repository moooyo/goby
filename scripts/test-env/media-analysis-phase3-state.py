#!/usr/bin/env python3
"""Private, guest-side Phase 3 state observer. Importing this file performs no I/O.

One bounded JSON request on stdin, one sanitized JSON response on stdout. Only
frozen production API mutations and fixed read-only PostgreSQL queries exist.
The external controller, not this guest, must fsync each acknowledgement before
injecting a fault. This observer never injects faults or claims power-loss proof.
"""

import argparse
import datetime
import decimal
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import select
import sqlite3
import stat
import subprocess
import sys
import time
import urllib.parse
import uuid


VERSION = 1
MAX_REQUEST = 64 << 10
MAX_ROW = 8 << 20
MAX_SNAPSHOT = 2 << 30
MAX_SECONDS = 240
SAFE_ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}\Z")
SHA256 = re.compile(r"[a-f0-9]{64}\Z")
FAMILIES = frozenset(("user_name", "favorite", "progress", "preferences",
                      "display_preferences", "metadata", "analysis_configuration",
                      "managed_configuration", "root_rebind"))
MUTATION_FIELDS = {
    "user_name": {"user_id", "name"}, "favorite": {"user_id", "item_id", "value"},
    "progress": {"user_id", "item_id", "media_source_id", "position_ticks"},
    "preferences": {"user_id", "configuration"},
    "display_preferences": {"user_id", "preferences_id", "client", "preferences"},
    "metadata": {"item_id", "overrides"}, "analysis_configuration": {"profile"},
    "managed_configuration": {"server_name"},
    "root_rebind": {"library_id", "root_id", "stage", "acknowledge_missing_removal"},
}
# All SQL identifiers, expressions, ordering, and limits are source-owned. The
# private binding and RPC cannot supply SQL, route names, or executable commands.
# JSONB numbers are parsed as arbitrary precision integers/Decimal, never float.
TABLES = {
    "schema_migrations": ("schema_migrations", "to_jsonb(t)-'applied_at'", ("version",), 50),
    "users": ("users", "to_jsonb(t)-'updated_at'", ("id",), 4096),
    "libraries": ("libraries", "to_jsonb(t)-'last_scan_at'", ("id",), 4096),
    "library_roots": ("library_roots", "to_jsonb(t)", ("id",), 4096),
    "items": ("items", "to_jsonb(t)-'updated_at'", ("id",), 200000),
    "catalog_identity": ("items", "jsonb_build_object('id',id,'library_id',library_id,'root_id',root_id,'parent_id',parent_id,'type',type,'relative_path',relative_path)", ("id",), 200000),
    "user_item_data": ("user_item_data", "to_jsonb(t)-'updated_at'", ("user_id", "item_id"), 1000000),
    "display_preferences": ("display_preferences", "to_jsonb(t)-'updated_at'", ("user_id", "client", "preferences_id"), 1000000),
    "item_metadata_state": ("item_metadata_state", "to_jsonb(t)-'updated_at'", ("item_id",), 200000),
    "metadata_admin": ("item_metadata_state", "jsonb_build_object('item_id',item_id,'overrides',overrides,'locked_values',locked_values,'last_edited_by',last_edited_by,'last_edited_at',last_edited_at)", ("item_id",), 200000),
    "item_intro_state": ("item_intro_state", "to_jsonb(t)-'updated_at'", ("item_id",), 200000),
    "managed_settings": ("managed_settings", "to_jsonb(t)-'updated_at'", ("id",), 1),
    "analysis_settings": ("analysis_settings", "to_jsonb(t)-'updated_at'", ("id",), 1),
    "task_runs": ("task_runs", "to_jsonb(t)", ("id",), 100000),
    "task_run_children": ("task_run_children", "to_jsonb(t)", ("id",), 200000),
    "scan_jobs": ("scan_jobs", "to_jsonb(t)", ("id",), 100000),
    "analysis_run_profiles": ("analysis_run_profiles", "to_jsonb(t)", ("run_id",), 100000),
    "analysis_work": ("analysis_work", "to_jsonb(t)", ("child_id",), 200000),
    "analysis_work_sources": ("analysis_work_sources", "to_jsonb(t)", ("child_id", "item_id"), 1000000),
    "analysis_previews": ("analysis_previews", "to_jsonb(t)-'updated_at'-'timeline' || jsonb_build_object('timeline_sha256',encode(sha256(timeline),'hex'))", ("item_id", "width"), 600000),
    "analysis_detections": ("analysis_detections", "to_jsonb(t)-'updated_at'", ("item_id",), 200000),
    "analysis_intro_decisions": ("analysis_intro_decisions", "to_jsonb(t)-'updated_at'", ("item_id",), 200000),
    "analysis_feature_cache": ("analysis_feature_cache", "to_jsonb(t)-'last_used_at'-'payload' || jsonb_build_object('payload_sha256',encode(sha256(payload),'hex'))", ("cache_key",), 200000),
    # These are intentionally separate from durable user-state equivalence.
    "play_sessions": ("play_sessions", "to_jsonb(t)", ("id",), 100000),
    "sessions": ("sessions", "to_jsonb(t)-'token_hash'", ("id",), 100000),
}
STRICT_TABLES = ("schema_migrations", "users", "libraries", "library_roots",
                 "catalog_identity", "user_item_data", "display_preferences",
                 "metadata_admin", "item_intro_state", "analysis_intro_decisions", "managed_settings", "analysis_settings")
TASK_STATES = {
    "task_runs": ({"pending", "running", "stopping"}, {"completed", "failed", "cancelled", "interrupted"}, "state"),
    "task_run_children": ({"waiting", "queued", "running"}, {"completed", "failed", "cancelled", "unavailable", "interrupted"}, "state"),
    "scan_jobs": ({"Queued", "Running"}, {"Completed", "Failed", "Cancelled", "Interrupted"}, "status"),
}
TASK_MUTABLE = frozenset(("state", "status", "started_at", "deadline_at", "stop_requested_at", "stop_reason",
                          "finished_at", "error", "error_code", "error_message", "scanned", "added", "updated",
                          "total_children", "terminal_children", "completed_children", "failed_children",
                          "cancelled_children", "interrupted_children", "unavailable_children", "cancel_requested"))
PROFILE_FIELDS = {
    "AutoPublishIntros": "auto_publish_intros", "PreviewIntervalSeconds": "preview_interval_seconds",
    "PreviewQuality": "preview_quality", "MaxSourceBytes": "max_source_bytes",
    "MaxItemRuntimeSeconds": "max_item_runtime_seconds", "FeatureCacheMaxBytes": "feature_cache_max_bytes",
}
PREFERENCE_FIELDS = frozenset(("AudioLanguagePreference", "SubtitleLanguagePreference", "SubtitleMode",
                              "EnableNextEpisodeAutoPlay", "RememberAudioSelections", "RememberSubtitleSelections",
                              "ResumeRewindSeconds", "HidePlayedInLatest", "IntroSkipMode"))


class StateError(RuntimeError):
    """Only source-owned error codes cross the RPC boundary."""


def need(value, code):
    if not value:
        raise StateError(code)


def exact_object(pairs):
    result = {}
    for key, value in pairs:
        need(key not in result, "duplicate_json_key")
        result[key] = value
    return result


def decode(raw):
    return json.loads(raw, object_pairs_hook=exact_object, parse_float=decimal.Decimal,
                      parse_constant=lambda _: (_ for _ in ()).throw(StateError("nonfinite_json")))


def canonical(value):
    def exact(item):
        if isinstance(item, decimal.Decimal):
            need(item.is_finite(), "nonfinite_json")
            sign, digits, exponent = item.as_tuple()
            coefficient = "".join(str(digit) for digit in digits).lstrip("0") or "0"
            if coefficient == "0":
                return "0"
            while coefficient.endswith("0"):
                coefficient = coefficient[:-1]
                exponent += 1
            need(abs(exponent) <= 1000000 and len(coefficient) <= 1000000, "decimal_representation_bound")
            if exponent >= 0:
                text = coefficient + "0" * exponent
            elif len(coefficient) + exponent > 0:
                point = len(coefficient) + exponent
                text = coefficient[:point] + "." + coefficient[point:]
            else:
                text = "0." + "0" * (-len(coefficient) - exponent) + coefficient
            return ("-" if sign else "") + text
        if isinstance(item, dict):
            need(all(isinstance(key, str) for key in item), "json_object_key")
            return "{" + ",".join(json.dumps(key, ensure_ascii=True) + ":" + exact(item[key]) for key in sorted(item)) + "}"
        if isinstance(item, list):
            return "[" + ",".join(exact(child) for child in item) + "]"
        need(not isinstance(item, float), "binary_float_forbidden")
        return json.dumps(item, ensure_ascii=True, separators=(",", ":"), allow_nan=False)
    return exact(value).encode("ascii")


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def safe_id(value):
    need(isinstance(value, str) and SAFE_ID.fullmatch(value), "invalid_identifier")
    return value


def private_read(path, maximum=MAX_ROW):
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        info = os.fstat(fd)
        need(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and
             info.st_uid == os.getuid() and info.st_nlink == 1 and info.st_size <= maximum, "private_file_contract")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            raw = stream.read(maximum + 1)
        after = os.fstat(fd)
        need(len(raw) == info.st_size and len(raw) <= maximum and
             (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns) ==
             (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns), "private_file_changed")
        return raw
    finally:
        os.close(fd)


def small_read(path, maximum=MAX_ROW):
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        need(stat.S_ISREG(os.fstat(fd).st_mode), "observation_file_type")
        raw = os.read(fd, maximum + 1)
        need(len(raw) <= maximum, "observation_file_bound")
        return raw
    finally:
        os.close(fd)


def write_private(path, value):
    raw = canonical(value) + b"\n"
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "wb", closefd=False) as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
    finally:
        os.close(fd)
    directory = os.open(Path(path).parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return hashlib.sha256(raw).hexdigest()


def checked_directory(path):
    value = Path(path)
    need(value.is_absolute() and str(value) == os.path.normpath(value), "directory_contract")
    for part in (value, *value.parents):
        info = part.lstat()
        need(stat.S_ISDIR(info.st_mode), "directory_symlink_or_type")
    return value


def load_binding(path):
    need(sys.platform.startswith("linux"), "guest_linux_required")
    binding = decode(private_read(path, 1 << 20))
    need(binding.get("version") == VERSION, "binding_version")
    safe_id(binding["run_id"])
    root = checked_directory(binding["owned_directory"])
    private = checked_directory(binding["private_directory"])
    need(root != private and root in private.parents and stat.S_IMODE(private.stat().st_mode) == 0o700 and
         private.stat().st_uid == os.getuid(), "private_directory_contract")
    marker_path = Path(binding["owner_marker"]["path"])
    need(root in marker_path.parents, "ownership_marker_scope")
    checked_directory(marker_path.parent)
    marker_raw = private_read(marker_path, 4096)
    need(hashlib.sha256(marker_raw).hexdigest() == binding["owner_marker"]["sha256"], "ownership_marker_hash")
    marker = decode(marker_raw)
    for key, wanted in {"schema_version": 1, "owner_id": binding["owner_id"], "vmid": 106,
                        "smbios_uuid": binding["smbios_uuid"], "run_id": binding["run_id"],
                        "machine_id": binding["machine_id"]}.items():
        need(marker.get(key) == wanted, "ownership_marker_mismatch")
    need(small_read("/etc/machine-id", 128).decode().strip() == binding["machine_id"], "guest_identity_mismatch")
    need(small_read("/sys/class/dmi/id/product_uuid", 128).decode().strip().lower() == binding["smbios_uuid"].lower(), "guest_smbios_mismatch")
    db = binding["database"]
    need(db["psql"] == "/usr/lib/postgresql/17/bin/psql" and SHA256.fullmatch(db["psql_sha256"]), "psql_binding")
    need(db["host"] in ("127.0.0.1", "::1") and type(db["port"]) is int and 1024 <= db["port"] <= 65535, "database_endpoint")
    for key in ("name", "role"):
        need(re.fullmatch(r"goby_phase3_[a-z0-9_]{1,48}", db[key]), "database_owned_name")
    for key in ("oid", "role_oid", "system_identifier"):
        need(isinstance(db[key], str) and re.fullmatch(r"[1-9][0-9]{0,19}", db[key]), "database_identity")
    need(root in Path(db["passfile"]).parents, "passfile_scope")
    checked_directory(Path(db["passfile"]).parent)
    private_read(db["passfile"], 4096)
    origin = urllib.parse.urlsplit(binding["http"]["origin"])
    need(origin.scheme == "http" and origin.hostname in ("127.0.0.1", "::1") and origin.port and
         not origin.path and not origin.query and not origin.fragment and not origin.username, "http_loopback_origin")
    scope = binding["scope"]
    for key, maximum in (("user_ids", 128), ("item_ids", 256), ("library_ids", 128), ("root_ids", 128)):
        values = scope[key]
        need(isinstance(values, list) and 0 < len(values) <= maximum and len(values) == len(set(values)), "scope_bound")
        for value in values:
            safe_id(value)
    need(isinstance(scope["fault_root_ids"], list) and scope["fault_root_ids"] and
         set(scope["fault_root_ids"]) <= set(scope["root_ids"]), "fault_root_scope")
    need(len(binding["mutations"]) <= 1024, "mutation_count_bound")
    rebind_roots = binding.get("rebind_roots", {})
    need(type(rebind_roots) is dict and set(rebind_roots) <= set(scope["fault_root_ids"]), "rebind_root_scope")
    for root_id, declaration in rebind_roots.items():
        need(set(declaration) == {"library_id", "target_path", "original", "replacement"}
             and declaration["library_id"] in scope["library_ids"], "rebind_root_declaration")
        target = Path(declaration["target_path"])
        need(target.is_absolute() and str(target) == os.path.normpath(target) and root in target.parents, "rebind_target_scope")
        for stage in ("original", "replacement"):
            volume = declaration[stage]
            need(set(volume) == {"dm_uuid", "major_minor", "filesystem_uuid", "backing_file"}
                 and re.fullmatch(r"[A-Za-z0-9_.-]{1,128}", volume["dm_uuid"])
                 and re.fullmatch(r"[0-9]{1,8}:[0-9]{1,8}", volume["major_minor"])
                 and str(uuid.UUID(volume["filesystem_uuid"])) == volume["filesystem_uuid"], "rebind_volume_declaration")
            backing = Path(volume["backing_file"])
            need(backing.is_absolute() and str(backing) == os.path.normpath(backing) and root in backing.parents, "rebind_backing_scope")
    seen = set()
    for mutation in binding["mutations"]:
        mid = safe_id(mutation["id"])
        need(len(mid) <= 110, "mutation_identifier_bound")
        need(mid not in seen and mutation["kind"] in FAMILIES, "mutation_binding")
        need(set(mutation) == {"id", "kind"} | MUTATION_FIELDS[mutation["kind"]], "mutation_fields")
        seen.add(mid)
        for field, scope_key in (("user_id", "user_ids"), ("item_id", "item_ids"), ("library_id", "library_ids"), ("root_id", "root_ids")):
            if field in mutation:
                need(mutation[field] in scope[scope_key], "mutation_scope")
        if mutation["kind"] == "root_rebind":
            need(mutation["root_id"] in rebind_roots and mutation["library_id"] == rebind_roots[mutation["root_id"]]["library_id"]
                 and mutation["stage"] in {"original", "replacement"} and mutation["acknowledge_missing_removal"] is True,
                 "rebind_mutation_declaration")
    checked_directory(binding["analysis_cache"]["path"])
    need(root in Path(binding["analysis_cache"]["path"]).parents, "cache_scope")
    safe_id(binding["analysis_cache"]["owner"])
    need(re.fullmatch(r"/sys/fs/cgroup/[A-Za-z0-9_./:@-]+", binding["application"]["cgroup"]) and
         ".." not in Path(binding["application"]["cgroup"]).parts, "cgroup_contract")
    return binding


def artifact(path):
    with open(path, "rb") as stream:
        checksum = hashlib.file_digest(stream, "sha256").hexdigest()
    return {"path": str(path), "bytes": path.stat().st_size, "sha256": checksum}


def state_defaults(user, item):
    return {"user_id": user, "item_id": item, "playback_position_ticks": 0, "play_count": 0,
            "is_favorite": False, "played": False, "last_played_at": None, "hide_from_resume": False,
            "rating": None, "likes": None, "remembered_media_source_id": "", "remembered_media_stamp": "",
            "remembered_audio_stream_index": None, "remembered_subtitle_stream_index": None}


def timestamp(value):
    need(isinstance(value, str), "durable_timestamp_missing")
    parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    need(parsed.tzinfo is not None, "durable_timestamp_timezone")
    return parsed


def guard_postimage(kind, table, previous, observed, expected):
    """An acknowledgement cannot bless collateral changes in its target row."""
    baseline = previous
    if baseline is None and table == "user_item_data":
        baseline = state_defaults(observed["user_id"], observed["item_id"])
    if baseline is None and table == "display_preferences":
        need(observed["revision"] == 1, "display_initial_revision")
        return
    need(baseline is not None, "mutation_target_missing")
    allowed = set(expected)
    if kind == "progress":
        allowed.add("last_played_at")
        if observed["play_count"] == baseline["play_count"] and baseline["play_count"] != 2147483647:
            need(observed["last_played_at"] == baseline["last_played_at"], "progress_unexpected_last_played")
        else:
            need(observed["play_count"] == min(baseline["play_count"] + 1, 2147483647), "progress_play_count")
            seen = timestamp(observed["last_played_at"])
            need(baseline["last_played_at"] is None or seen >= timestamp(baseline["last_played_at"]), "progress_last_played_regressed")
    if kind == "metadata":
        allowed.add("last_edited_at")
        seen = timestamp(observed["last_edited_at"])
        need(baseline["last_edited_at"] is None or seen >= timestamp(baseline["last_edited_at"]), "metadata_edit_time_regressed")
    if kind == "root_rebind" and table == "library_roots":
        allowed.update(("storage_binding", "bound_at"))
        need(observed["storage_binding"] is not None and observed["storage_binding"] != baseline["storage_binding"], "rebind_document_unchanged")
        timestamp(observed["bound_at"])
    need({key: value for key, value in baseline.items() if key not in allowed} ==
         {key: value for key, value in observed.items() if key not in allowed}, "mutation_collateral_state_change")


class Observer:
    def __init__(self, binding, *, external_artifacts=None, expected_binding_sha256=None):
        self.binding = binding
        self.private = Path(binding.get("private_directory", "/unavailable-external-private-root"))
        self.binding_sha256 = expected_binding_sha256 or digest(binding)
        self.external_artifacts = external_artifacts
        self.deadline = time.monotonic() + MAX_SECONDS
        self.http_evidence = []

    def remaining(self):
        seconds = self.deadline - time.monotonic()
        need(seconds > 0, "observer_deadline")
        return seconds

    def request(self, method, route, body=None, audience="admin", accepted=(200,)):
        # Only callers below construct routes; RPC never supplies a route.
        need(route.startswith(("/admin/v1/", "/emby/", "/readyz")), "route_allowlist")
        configuration = self.binding["http"]
        origin = urllib.parse.urlsplit(configuration["origin"])
        headers = {"Accept": "application/json", "Connection": "close"}
        if audience == "admin":
            token = configuration["admin_cookie"]
            headers.update({"Cookie": "goby_session=" + token,
                            "X-CSRF-Token": hashlib.sha256(("goby:admin:csrf:" + token).encode()).hexdigest()})
        elif audience == "emby":
            headers["X-Emby-Token"] = configuration["emby_token"]
            device = safe_id(configuration["emby_device_id"])
            headers["X-Emby-Authorization"] = 'MediaBrowser Client="Phase3", Device="Phase3", DeviceId="' + device + '", Version="1"'
        payload = None if body is None else canonical(body)
        if payload is not None:
            need(len(payload) <= MAX_REQUEST, "http_request_bound")
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection(origin.hostname, origin.port, timeout=min(20, self.remaining()))
        started = time.monotonic_ns()
        started_unix = time.time_ns()
        try:
            connection.request(method, route, body=payload, headers=headers)
            response = connection.getresponse()
            raw = response.read(MAX_ROW + 1)
            need(len(raw) <= MAX_ROW, "http_response_bound")
            record = {"method": method, "route": route, "status": response.status,
                      "elapsed_ns": str(time.monotonic_ns() - started), "started_unix_ns": str(started_unix),
                      "completed_unix_ns": str(time.time_ns()), "body": decode(raw) if raw else None}
            self.http_evidence.append(record)
            need(response.status in accepted, "production_http_status")
            return record["body"], response.status
        finally:
            connection.close()

    def http_identity(self):
        value, _ = self.request("GET", "/emby/System/Info/Public", audience="public")
        need(value.get("Id") == self.binding["http"]["server_id"] and value.get("ProductName") == "Goby", "http_instance_mismatch")
        applications = [value for value in self.processes() if value.get("is_application")]
        need(len(applications) == 1, "application_lifetime_ambiguous")
        application = applications[0]
        process = Path("/proc") / application["pid"]
        sockets = set()
        with os.scandir(process / "fd") as descriptors:
            count = 0
            for descriptor in descriptors:
                count += 1
                need(count <= 16384, "application_descriptor_bound")
                try:
                    target = os.readlink(descriptor.path)
                except FileNotFoundError:
                    continue
                match = re.fullmatch(r"socket:\[([0-9]+)\]", target)
                if match:
                    sockets.add(match[1])
        port = urllib.parse.urlsplit(self.binding["http"]["origin"]).port
        listening = set()
        for family in ("tcp", "tcp6"):
            try:
                raw = small_read(process / "net" / family, 4 << 20).decode()
            except FileNotFoundError:
                continue
            for line in raw.splitlines()[1:]:
                fields = line.split()
                need(len(fields) >= 10, "application_tcp_contract")
                if fields[3] == "0A" and int(fields[1].rsplit(":", 1)[1], 16) == port:
                    listening.add(fields[9])
        need(bool(listening & sockets), "http_listener_not_owned")
        current = small_read(process / "stat", 8192).decode().rsplit(") ", 1)[1].split()
        need(current[19] == application["start_ticks"], "http_application_lifetime_changed")

    def sql(self, tables, consume):
        db = self.binding["database"]
        with open(db["psql"], "rb") as executable:
            need(hashlib.file_digest(executable, "sha256").hexdigest() == db["psql_sha256"], "psql_binary_changed")
        identity = """SELECT jsonb_build_object('identity',jsonb_build_object(
          'name',current_database(),'oid',(SELECT oid::text FROM pg_database WHERE datname=current_database()),
          'role',current_user,'role_oid',(SELECT oid::text FROM pg_roles WHERE rolname=current_user),
          'system_identifier',(SELECT system_identifier::text FROM pg_control_system()),
          'server_id',(SELECT value FROM public.server_settings WHERE key='server_id'),
          'transaction_isolation',current_setting('transaction_isolation'),
          'transaction_read_only',current_setting('transaction_read_only'),
          'server_version',current_setting('server_version_num'),'fsync',current_setting('fsync'),
          'synchronous_commit',current_setting('synchronous_commit'),'full_page_writes',current_setting('full_page_writes')));
        """
        pieces = ["BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL statement_timeout='90s'; SET LOCAL lock_timeout='2s'; SET LOCAL idle_in_transaction_session_timeout='120s'; SET LOCAL timezone='UTC'; SET LOCAL search_path=pg_catalog,public;", identity]
        for name in tables:
            table, expression, keys, limit = TABLES[name]
            order = ",".join('t."' + key + '"' for key in keys)
            pieces.append("SELECT jsonb_build_object('table','" + name + "','row'," + expression + ") FROM public." + table + " t ORDER BY " + order + " LIMIT " + str(limit + 1) + ";")
        pieces.extend((identity, "COMMIT; SELECT '{\"complete\":true}'::json;"))
        script = "\n".join(pieces).encode()
        environment = {"PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TZ": "UTC",
                       "PGPASSFILE": db["passfile"], "PGCONNECT_TIMEOUT": "10", "PGAPPNAME": "goby-phase3-state",
                       "PGOPTIONS": "-c default_transaction_read_only=on -c statement_timeout=90000 -c lock_timeout=2000"}
        argv = [db["psql"], "-X", "-q", "-A", "-t", "--no-password", "-v", "ON_ERROR_STOP=1",
                "-h", db["host"], "-p", str(db["port"]), "-U", db["role"], "-d", db["name"]]
        process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment)
        try:
            process.stdin.write(script)
            process.stdin.close()
            streams = {process.stdout.fileno(): "stdout", process.stderr.fileno(): "stderr"}
            pending = bytearray()
            error_size, total, identities, complete = 0, 0, 0, False
            snapshot_identity = None
            while streams:
                ready, _, _ = select.select(list(streams), [], [], min(1, self.remaining()))
                for descriptor in ready:
                    chunk = os.read(descriptor, 65536)
                    if not chunk:
                        del streams[descriptor]
                        continue
                    if streams[descriptor] == "stderr":
                        error_size += len(chunk)
                        need(error_size <= 65536, "sql_error_bound")
                        continue
                    total += len(chunk)
                    need(total <= MAX_SNAPSHOT, "snapshot_byte_bound")
                    pending.extend(chunk)
                    while b"\n" in pending:
                        line, _, rest = pending.partition(b"\n")
                        pending = bytearray(rest)
                        need(len(line) <= MAX_ROW, "snapshot_row_bound")
                        if not line:
                            continue
                        value = decode(line)
                        if "identity" in value:
                            observed = value["identity"]
                            for field in ("name", "oid", "role", "role_oid", "system_identifier"):
                                need(observed[field] == db[field], "database_binding_mismatch")
                            need(observed["server_id"] == self.binding["http"]["server_id"] and
                                 170000 <= int(observed["server_version"]) < 180000, "database_instance_schema_binding")
                            need(observed["fsync"] == "on" and observed["synchronous_commit"] == "on" and
                                 observed["full_page_writes"] == "on", "database_durability_configuration")
                            need(observed["transaction_isolation"] == "repeatable read" and observed["transaction_read_only"] == "on",
                                 "snapshot_transaction_configuration")
                            need(snapshot_identity is None or observed == snapshot_identity, "snapshot_transaction_identity_changed")
                            snapshot_identity = observed
                            identities += 1
                        elif value.get("complete") is True:
                            complete = True
                        else:
                            need(identities == 1 and not complete and value.get("table") in tables, "sql_stream_order")
                            consume(value["table"], value["row"])
                    need(len(pending) <= MAX_ROW, "snapshot_row_bound")
            need(process.wait(timeout=min(5, self.remaining())) == 0 and not pending and identities == 2 and complete,
                 "sql_incomplete")
            self.snapshot_transaction = {"isolation": snapshot_identity["transaction_isolation"],
                "read_only": snapshot_identity["transaction_read_only"] == "on", "database_identity": snapshot_identity,
                "synchronous_commit": snapshot_identity["synchronous_commit"], "fsync": snapshot_identity["fsync"] == "on",
                "full_page_writes": snapshot_identity["full_page_writes"] == "on"}
        finally:
            if process.poll() is None:
                process.kill()
                process.wait(timeout=5)
            for stream in (process.stdin, process.stdout, process.stderr):
                if stream is not None and not stream.closed:
                    stream.close()

    def snapshot_path(self, snapshot_id):
        return self.evidence_path(safe_id(snapshot_id) + ".sqlite")

    def evidence_path(self, name):
        if self.external_artifacts is not None:
            need(name in self.external_artifacts, "external_artifact_missing")
            return Path(self.external_artifacts[name]["path"])
        return self.private / name

    def snapshot(self, snapshot_id, runtime=True):
        snapshot_id = safe_id(snapshot_id)
        path = self.snapshot_path(snapshot_id)
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
        os.close(fd)
        connection = sqlite3.connect(path)
        hashes = {name: hashlib.sha256() for name in TABLES}
        counts = dict.fromkeys(TABLES, 0)
        try:
            connection.execute("PRAGMA journal_mode=DELETE")
            connection.execute("PRAGMA synchronous=FULL")
            connection.execute("CREATE TABLE rows(table_name TEXT NOT NULL,row_key TEXT NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(table_name,row_key)) WITHOUT ROWID")
            connection.execute("CREATE TABLE facts(name TEXT PRIMARY KEY,payload TEXT NOT NULL)")
            def consume(name, row):
                counts[name] += 1
                need(counts[name] <= TABLES[name][3], "table_bound_exceeded")
                key = canonical([row[field] for field in TABLES[name][2]]).decode()
                raw = canonical(row)
                connection.execute("INSERT INTO rows VALUES(?,?,?)", (name, key, raw.decode()))
            self.sql(tuple(TABLES), consume)
            # Canonical order is the stored ASCII JSON primary-key string, not
            # the database's locale-dependent text ordering.
            for name in TABLES:
                for key, payload in connection.execute("SELECT row_key,payload FROM rows WHERE table_name=? ORDER BY row_key", (name,)):
                    self.remaining()
                    hashes[name].update(canonical([key, decode(payload)]) + b"\n")
            versions = [decode(row[0])["version"] for row in connection.execute("SELECT payload FROM rows WHERE table_name='schema_migrations'")]
            need(sorted(versions) == list(range(1, 51)), "schema50_required")
            for field, table in (("user_ids", "users"), ("item_ids", "items"), ("library_ids", "libraries"), ("root_ids", "library_roots")):
                for value in self.binding["scope"][field]:
                    need(connection.execute("SELECT 1 FROM rows WHERE table_name=? AND row_key=?", (table, canonical([value]).decode())).fetchone(), "owned_scope_missing")
            observation = self.runtime(connection) if runtime else None
            summary = {"snapshot_id": snapshot_id, "run_id": self.binding["run_id"], "binding_sha256": self.binding_sha256,
                       "tables": {name: {"count": counts[name], "sha256": hashes[name].hexdigest()} for name in TABLES},
                       "runtime": observation, "transaction": self.snapshot_transaction, "complete": True}
            summary["sha256"] = digest(summary)
            connection.execute("INSERT INTO facts VALUES('summary',?)", (canonical(summary).decode(),))
            connection.execute("INSERT INTO facts VALUES('http_private',?)", (canonical(self.http_evidence).decode(),))
            connection.commit()
        finally:
            connection.close()
        with open(path, "rb") as snapshot_file:
            os.fsync(snapshot_file.fileno())
        parent = os.open(self.private, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(parent)
        finally:
            os.close(parent)
        # This descriptor is private transport metadata. Public reports must
        # project safe hashes/counts instead of publishing this response.
        return dict(summary, private_artifact=artifact(path))

    def open_snapshot(self, snapshot_id):
        path = self.snapshot_path(snapshot_id)
        info = path.lstat()
        need(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.getuid() and info.st_nlink == 1,
             "snapshot_file_contract")
        connection = sqlite3.connect(path.as_uri() + "?mode=ro&immutable=1", uri=True)
        row = connection.execute("SELECT payload FROM facts WHERE name='summary'").fetchone()
        need(row is not None, "snapshot_not_complete")
        summary = decode(row[0])
        need(summary["binding_sha256"] == self.binding_sha256 and summary["complete"], "snapshot_binding_mismatch")
        need(summary["snapshot_id"] == snapshot_id and summary["sha256"] == digest({key: value for key, value in summary.items() if key != "sha256"}), "snapshot_summary_hash")
        if self.external_artifacts is not None:
            need(set(summary["tables"]) == set(TABLES), "external_table_inventory")
            actual_tables = {row[0] for row in connection.execute("SELECT DISTINCT table_name FROM rows")}
            need(actual_tables <= set(TABLES), "external_unknown_table")
            for name, (_, _, keys, limit) in TABLES.items():
                count, checksum = 0, hashlib.sha256()
                for key, payload in connection.execute("SELECT row_key,payload FROM rows WHERE table_name=? ORDER BY row_key", (name,)):
                    self.remaining()
                    need(len(payload.encode()) <= MAX_ROW, "external_row_bound")
                    record = decode(payload)
                    need(key == canonical([record[field] for field in keys]).decode(), "external_row_key")
                    count += 1
                    need(count <= limit, "external_table_bound")
                    checksum.update(canonical([key, record]) + b"\n")
                need(summary["tables"][name] == {"count": count, "sha256": checksum.hexdigest()}, "external_table_digest_mismatch")
        return connection, summary

    def row(self, connection, table, keys):
        value = connection.execute("SELECT payload FROM rows WHERE table_name=? AND row_key=?", (table, canonical(keys).decode())).fetchone()
        return None if value is None else decode(value[0])

    def runtime(self, connection=None):
        self.http_identity()
        ready_body, ready_status = self.request("GET", "/readyz", audience="public", accepted=(200, 503))
        roots = []
        for library_id in self.binding["scope"]["library_ids"]:
            value, _ = self.request("GET", "/admin/v1/libraries/" + urllib.parse.quote(library_id, safe="") + "/roots")
            need(len(value["Items"]) <= 4096 and value["TotalRecordCount"] == len(value["Items"]), "root_list_bound")
            for root in value["Items"]:
                if root["Id"] not in self.binding["scope"]["root_ids"]:
                    continue
                view, _ = self.request("GET", "/admin/v1/libraries/" + urllib.parse.quote(library_id, safe="") + "/roots/" + urllib.parse.quote(root["Id"], safe="") + "/binding")
                binding = view["Binding"]
                need(binding["Status"] in ("unbound", "verified", "mismatch", "unavailable"), "root_status_contract")
                roots.append({key: binding[key] for key in ("Id", "LibraryId", "Revision", "Status", "ApprovedFingerprint", "ObservedFingerprint") if key in binding})
        need(sorted(row["Id"] for row in roots) == sorted(self.binding["scope"]["root_ids"]), "root_scope_incomplete")
        process = self.processes()
        cache = self.cache(connection)
        native_resources, _ = self.request("GET", "/admin/v1/runtime/resources")
        tasks = []
        if connection is not None:
            for name, (active, _, field) in TASK_STATES.items():
                for (payload,) in connection.execute("SELECT payload FROM rows WHERE table_name=? ORDER BY row_key", (name,)):
                    row = decode(payload)
                    if row[field] in active:
                        need(len(tasks) < 4096, "active_task_bound")
                        tasks.append({"table": name, "id": row["id"], "state": row[field],
                                      "task_key": row.get("task_key"), "run_id": row.get("run_id"),
                                      "library_id": row.get("library_id")})
        return {"ready": ready_status == 200 and ready_body == {"Status": "ready"}, "ready_status": ready_status,
                "ready_body_sha256": digest(ready_body), "roots": roots, "processes": process,
                "cache": cache, "native_resources": native_resources, "active_tasks": tasks,
                "boot_id": small_read("/proc/sys/kernel/random/boot_id", 128).decode().strip()}

    def processes(self):
        cgroup = checked_directory(self.binding["application"]["cgroup"])
        processes, directories, visited = [], [cgroup], 0
        while directories:
            self.remaining()
            visited += 1
            need(visited <= 4096, "cgroup_directory_bound")
            directory = directories.pop()
            need(len(processes) <= 4096 and len(directories) <= 4096, "cgroup_bound")
            raw = small_read(directory / "cgroup.procs", 65536).decode()
            for token in raw.splitlines():
                need(re.fullmatch(r"[1-9][0-9]{0,9}", token), "pid_contract")
                proc = Path("/proc") / token
                try:
                    before = small_read(proc / "stat", 8192).decode()
                    fields = before.rsplit(") ", 1)[1].split()
                    executable = os.readlink(proc / "exe")
                    with open(proc / "exe", "rb") as stream:
                        executable_hash = hashlib.file_digest(stream, "sha256").hexdigest()
                    after = small_read(proc / "stat", 8192).decode().rsplit(") ", 1)[1].split()
                    need(fields[19] == after[19], "process_lifetime_changed")
                    processes.append({"pid": token, "parent_pid": fields[1], "start_ticks": fields[19],
                                      "state": fields[0], "executable_sha256": executable_hash,
                                      "is_application": executable_hash == self.binding["application"]["executable_sha256"],
                                      "is_media_worker": Path(executable).name in ("ffmpeg", "ffprobe")})
                except FileNotFoundError:
                    processes.append({"pid": token, "exited_during_observation": True})
            with os.scandir(directory) as entries:
                for entry in entries:
                    if entry.is_dir(follow_symlinks=False):
                        directories.append(Path(entry.path))
        by_pid = {process["pid"]: process for process in processes if not process.get("exited_during_observation")}
        for process in by_pid.values():
            if not process.get("is_media_worker"):
                continue
            seen, parent, attached = set(), process["parent_pid"], False
            while parent in by_pid and parent not in seen:
                seen.add(parent)
                if by_pid[parent].get("is_application"):
                    attached = True
                    break
                parent = by_pid[parent]["parent_pid"]
            process["attached_to_application"] = attached
        return processes

    def cache(self, connection):
        root = checked_directory(self.binding["analysis_cache"]["path"])
        owner = self.binding["analysis_cache"]["owner"]
        counts = {"temporary": 0, "trash": 0, "ready": 0, "unreferenced_ready": 0, "missing_referenced": 0, "unsafe": 0}
        marker = decode(small_read(root / ".goby-analysis-cache", 256))
        need(marker == {"marker": "goby-analysis-cache-v1", "owner": owner}, "analysis_cache_owner_mismatch")
        references = {}
        if connection is not None:
            for (payload,) in connection.execute("SELECT payload FROM rows WHERE table_name='analysis_previews'"):
                row = decode(payload)
                prior = references.setdefault(row["cache_key"], row["seal"])
                need(prior == row["seal"], "preview_seal_disagreement")
        ready_keys = set()
        rows = []
        hash_bytes = 0
        temporary_bytes, temporary_inodes, temporary_entries = 0, 0, []
        with os.scandir(root) as entries:
            for entry in entries:
                self.remaining()
                need(len(rows) < 131074, "cache_entry_bound")
                info = entry.stat(follow_symlinks=False)
                row = {"name": entry.name, "mode": stat.S_IFMT(info.st_mode), "device": str(info.st_dev), "inode": str(info.st_ino)}
                rows.append(row)
                if entry.name in (".goby-analysis-cache", ".writer-lock"):
                    continue
                if not stat.S_ISDIR(info.st_mode):
                    counts["unsafe"] += 1
                elif entry.name.startswith("tmp-" + owner + "-"):
                    counts["temporary"] += 1
                    allocated, inodes = self.cache_temporary_allocation(Path(entry.path))
                    temporary_bytes += allocated
                    temporary_inodes += inodes
                    temporary_entries.append(row)
                elif entry.name.startswith("trash-" + owner + "-"):
                    counts["trash"] += 1
                    allocated, inodes = self.cache_temporary_allocation(Path(entry.path))
                    temporary_bytes += allocated
                    temporary_inodes += inodes
                    temporary_entries.append(row)
                elif re.fullmatch(r"entry-[a-f0-9]{64}", entry.name):
                    counts["ready"] += 1
                    key = entry.name[6:]
                    ready_keys.add(key)
                    counts["unreferenced_ready"] += key not in references
                    try:
                        entry_root = Path(entry.path)
                        owner_record = decode(small_read(entry_root / ".owner.json", 512))
                        need(owner_record.get("marker") == "goby-analysis-entry-v1" and owner_record.get("owner") == owner and
                             owner_record.get("key") == key and re.fullmatch(r"[a-f0-9]{32}", owner_record.get("token", "")), "cache_entry_owner")
                        manifest_raw = small_read(entry_root / ".entry.json", 65536)
                        seal = hashlib.sha256(manifest_raw).hexdigest()
                        need(small_read(entry_root / ".seal", 65) == (seal + "\n").encode(), "cache_entry_seal")
                        need(key not in references or references[key] == seal, "database_preview_seal_mismatch")
                        manifest = decode(manifest_raw)
                        need(manifest.get("version") == 1 and manifest.get("owner") == owner and manifest.get("key") == key and
                             1 <= len(manifest["artifacts"]) <= 4, "cache_entry_manifest")
                        names = {".owner.json", ".entry.json", ".seal"}
                        for payload in manifest["artifacts"]:
                            name = payload["name"]
                            need(name in ("240.bif", "320.bif", "400.bif", "manifest.json") and name not in names and
                                 type(payload["size"]) is int and 0 <= payload["size"] <= 512 << 20 and SHA256.fullmatch(payload["sha256"]), "cache_artifact_contract")
                            names.add(name)
                            fd = os.open(entry_root / name, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
                            try:
                                before = os.fstat(fd)
                                identity = ":".join(str(value) for value in (before.st_dev, before.st_ino,
                                    before.st_mtime_ns // 1000000000, before.st_mtime_ns % 1000000000,
                                    before.st_ctime_ns // 1000000000, before.st_ctime_ns % 1000000000, before.st_size))
                                need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size == payload["size"] and
                                     identity == payload["identity"], "cache_artifact_identity")
                                checksum = hashlib.sha256()
                                while True:
                                    self.remaining()
                                    chunk = os.read(fd, 1 << 20)
                                    if not chunk:
                                        break
                                    hash_bytes += len(chunk)
                                    need(hash_bytes <= 8 << 30, "cache_hash_byte_bound")
                                    checksum.update(chunk)
                                after = os.fstat(fd)
                                stable = ("st_dev", "st_ino", "st_mode", "st_uid", "st_gid", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns")
                                need(all(getattr(before, field) == getattr(after, field) for field in stable) and
                                     checksum.hexdigest() == payload["sha256"], "cache_artifact_content")
                            finally:
                                os.close(fd)
                        actual_names = set()
                        with os.scandir(entry_root) as children:
                            for child in children:
                                need(len(actual_names) < 8, "cache_entry_inventory_bound")
                                actual_names.add(child.name)
                        need(names == actual_names, "cache_entry_inventory")
                        row["seal"] = seal
                    except (OSError, StateError, KeyError, TypeError, ValueError) as error:
                        if isinstance(error, StateError) and ("bound" in str(error) or "deadline" in str(error)):
                            raise
                        counts["unsafe"] += 1
                else:
                    counts["unsafe"] += 1
        counts["missing_referenced"] = len(set(references) - ready_keys)
        # Unreferenced sealed entries can be legitimate cache retention. They are
        # observed explicitly and are never silently equated with corrupt data.
        if connection is not None:
            connection.execute("INSERT INTO facts VALUES('cache_private',?)", (canonical(rows).decode(),))
        return {"counts": counts, "inventory_sha256": digest(sorted(rows, key=lambda row: row["name"])),
                "references_observed": connection is not None, "payload_bytes_hashed": hash_bytes,
                "temporary_bytes": temporary_bytes, "temporary_inodes": temporary_inodes,
                "temporary_entries": sorted(temporary_entries, key=lambda row: row["name"])}

    def cache_temporary_allocation(self, root):
        pending, seen, allocated = [(root, 0)], set(), 0
        while pending:
            self.remaining()
            path, depth = pending.pop()
            need(depth <= 4 and len(seen) < 131072, "cache_temporary_inventory_bound")
            try:
                info = path.lstat()
            except FileNotFoundError:
                continue
            need(stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode), "cache_temporary_special_file")
            key = (info.st_dev, info.st_ino)
            if key in seen:
                continue
            seen.add(key)
            allocated += info.st_blocks * 512
            need(allocated <= 8 << 30, "cache_temporary_byte_bound")
            if stat.S_ISDIR(info.st_mode):
                try:
                    with os.scandir(path) as children:
                        for child in children:
                            need(len(pending) < 131072, "cache_temporary_pending_bound")
                            pending.append((Path(child.path), depth + 1))
                except FileNotFoundError:
                    continue
        return allocated, len(seen)

    def private_reference(self, reference, suffix):
        need(type(reference) is dict and set(reference) == {"path", "bytes", "sha256"}
             and type(reference["bytes"]) is int and 0 < reference["bytes"] <= MAX_ROW
             and SHA256.fullmatch(reference["sha256"]), "private_reference")
        path = Path(reference["path"])
        need(path.parent == self.private and path.name.endswith(suffix), "private_reference_scope")
        safe_id(path.name)
        raw = private_read(path)
        need(len(raw) == reference["bytes"] and hashlib.sha256(raw).hexdigest() == reference["sha256"], "private_reference_hash")
        return decode(raw)

    def rebind_volume(self, root_id, stage):
        declaration = self.binding["rebind_roots"][root_id]
        expected = declaration[stage]
        target = declaration["target_path"]
        def unescape(value):
            return re.sub(r"\\(040|011|012|134)", lambda match: chr(int(match[1], 8)), value)
        mounts = []
        for line in small_read("/proc/self/mountinfo", 4 << 20).decode().splitlines():
            fields = line.split()
            need(len(fields) >= 10 and "-" in fields, "mountinfo_contract")
            mountpoint = unescape(fields[4])
            if Path(target) == Path(mountpoint) or Path(mountpoint) in Path(target).parents:
                separator = fields.index("-")
                mounts.append({"major_minor": fields[2], "filesystem": fields[separator+1], "mount_id": fields[0],
                               "mountpoint": mountpoint, "mount_root": unescape(fields[3])})
        # Stacked bind mounts can have identical mountpoints and depth. Select
        # the actual visible mount from a held target FD, never from the desired
        # volume or an assumed ordering of mount IDs.
        checked_directory(target)
        target_fd = os.open(target, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC | os.O_NOFOLLOW)
        try:
            target_info = os.fstat(target_fd)
            fdinfo = small_read("/proc/self/fdinfo/%d" % target_fd, 4096).decode().splitlines()
            mount_ids = [line.split(":", 1)[1].strip() for line in fdinfo if line.startswith("mnt_id:")]
            need(len(mount_ids) == 1 and re.fullmatch(r"[1-9][0-9]*", mount_ids[0]), "rebind_target_mount_id")
            named = os.stat(target, follow_symlinks=False)
            need((named.st_dev, named.st_ino) == (target_info.st_dev, target_info.st_ino), "rebind_target_changed")
            mounts = [row for row in mounts if row["mount_id"] == mount_ids[0]]
        finally:
            os.close(target_fd)
        need(len(mounts) == 1 and mounts[0]["major_minor"] == expected["major_minor"]
             and mounts[0]["filesystem"] == "ext4", "rebind_target_mount_mismatch")
        need("%d:%d" % (os.major(target_info.st_dev), os.minor(target_info.st_dev)) == expected["major_minor"], "rebind_target_device_mismatch")
        device = Path("/sys/dev/block") / expected["major_minor"]
        name = device.resolve(strict=True).name
        need(re.fullmatch(r"dm-[0-9]{1,8}", name), "rebind_device_mapper_required")
        dm_uuid = small_read(device / "dm/uuid", 256).decode().strip()
        slaves = list((device / "slaves").iterdir())
        need(len(slaves) == 1 and re.fullmatch(r"loop[0-9]{1,8}", slaves[0].name), "rebind_loop_dependency")
        backing = small_read(slaves[0] / "loop/backing_file", 4096).decode().strip()
        need(dm_uuid == expected["dm_uuid"] and backing == expected["backing_file"], "rebind_volume_owner_changed")
        descriptor = os.open("/dev/" + name, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
        try:
            info = os.fstat(descriptor)
            need(stat.S_ISBLK(info.st_mode) and "%d:%d" % (os.major(info.st_rdev), os.minor(info.st_rdev)) == expected["major_minor"], "rebind_block_identity")
            # These declared fault volumes are ext4. Read only its fixed
            # superblock header; never run a caller-supplied device command.
            header = os.pread(descriptor, 1024, 1024)
            need(len(header) == 1024 and header[56:58] == b"\x53\xef", "rebind_ext4_superblock")
            filesystem_uuid = str(uuid.UUID(bytes=header[104:120]))
        finally:
            os.close(descriptor)
        need(filesystem_uuid == expected["filesystem_uuid"], "rebind_filesystem_changed")
        return dict(expected, mount_id=mounts[0]["mount_id"], target_path=target,
                    target_inode=str(target_info.st_ino),
                    mountpoint=mounts[0]["mountpoint"], mount_root=mounts[0]["mount_root"],
                    target_relative_path=Path(target).relative_to(mounts[0]["mountpoint"]).as_posix())

    def observe_rebind(self, observation_id, stage, root_id):
        safe_id(observation_id)
        need(len(observation_id) <= 100 and stage in {"original", "replacement"}
             and root_id in self.binding.get("rebind_roots", {}), "rebind_observation_scope")
        declaration = self.binding["rebind_roots"][root_id]
        self.http_identity()
        volume = self.rebind_volume(root_id, stage)
        route = "/admin/v1/libraries/" + urllib.parse.quote(declaration["library_id"], safe="") + "/roots/" + urllib.parse.quote(root_id, safe="") + "/binding"
        value, _ = self.request("GET", route)
        current = value["Binding"]
        need(current["Status"] == "mismatch" and SHA256.fullmatch(current["ObservedFingerprint"])
             and self.rebind_volume(root_id, stage) == volume, "rebind_observation_changed")
        record = {"version": VERSION, "run_id": self.binding["run_id"], "binding_sha256": self.binding_sha256,
                  "observation_id": observation_id, "root_id": root_id, "library_id": declaration["library_id"],
                  "stage": stage, "volume": volume, "binding": current, "http_evidence": self.http_evidence}
        path = self.private / (observation_id + ".rebind.json")
        write_private(path, record)
        return {"observation_id": observation_id, "root_id": root_id, "stage": stage, "private_artifact": artifact(path)}

    def rebind(self, mutation_id, observation):
        record = self.private_reference(observation, ".rebind.json")
        need(record["version"] == VERSION and record["binding_sha256"] == self.binding_sha256
             and record["run_id"] == self.binding["run_id"], "rebind_observation_binding")
        return self.mutate(mutation_id, record, observation)

    def mutate(self, mutation_id, rebind_observation=None, observation_reference=None):
        mutation = next((value for value in self.binding["mutations"] if value["id"] == mutation_id), None)
        need(mutation is not None, "mutation_not_frozen")
        need((mutation["kind"] == "root_rebind") == (rebind_observation is not None), "rebind_observation_required")
        need(not (self.private / (safe_id(mutation_id) + ".ack.json")).exists(), "mutation_already_acknowledged")
        self.http_identity()
        before = self.snapshot(mutation_id + ".before", runtime=mutation["kind"] == "root_rebind")
        conn, _ = self.open_snapshot(before["snapshot_id"])
        try:
            expectations = self.apply_mutation(mutation, conn, rebind_observation)
        finally:
            conn.close()
        after = self.snapshot(mutation_id + ".after", runtime=mutation["kind"] == "root_rebind")
        extra = {} if observation_reference is None else {"rebind_observation": observation_reference, "rebind_facts": rebind_observation}
        return self.finish_ack(mutation, before, after, expectations, extra)

    def finish_ack(self, mutation, before, after, expectations, extra=None, allow_unchanged=False):
        mutation_id = mutation["id"]
        conn, _ = self.open_snapshot(after["snapshot_id"])
        postimages = []
        old_connection, _ = self.open_snapshot(before["snapshot_id"])
        try:
            for table, keys, expected in expectations:
                observed = self.row(conn, table, keys)
                need(observed is not None and all(observed.get(key) == value for key, value in expected.items()), "mutation_database_readback_mismatch")
                previous = self.row(old_connection, table, keys)
                need(previous != observed or allow_unchanged, "mutation_no_observed_change")
                guard_postimage(mutation["kind"], table, previous, observed, expected)
                postimages.append({"table": table, "key": keys, "before_row": previous, "row": observed, "expected_fields": expected})
        finally:
            conn.close()
            old_connection.close()
        ack = {"version": VERSION, "run_id": self.binding["run_id"], "mutation_id": mutation_id, "kind": mutation["kind"],
               "binding_sha256": digest(self.binding), "before_snapshot": before["snapshot_id"], "after_snapshot": after["snapshot_id"],
               "postimages": postimages, "http_evidence": self.http_evidence,
               "database_verified": True, "http_readback_verified": True, "completed_monotonic_ns": str(time.monotonic_ns()),
               "completed_unix_ns": str(time.time_ns())}
        ack.update(extra or {})
        if allow_unchanged:
            ack["readback_only"] = all(image["before_row"] == image["row"] for image in postimages)
        sha = write_private(self.private / (mutation_id + ".ack.json"), ack)
        return {"mutation_id": mutation_id, "kind": mutation["kind"], "record_sha256": sha,
                "before_snapshot": before["snapshot_id"], "after_snapshot": after["snapshot_id"],
                "postimage_sha256": digest(postimages), "database_verified": True, "http_readback_verified": True,
                "external_fsync_required": True, "private_ack": {
                    "version": VERSION, "run_id": self.binding["run_id"], "mutation_id": mutation_id,
                    "kind": mutation["kind"], "binding_sha256": digest(self.binding), "postimages": postimages,
                    "completed_monotonic_ns": ack["completed_monotonic_ns"],
                    "completed_unix_ns": ack["completed_unix_ns"],
                    "http_responses": [{"method": value["method"], "status": value["status"],
                                        "started_unix_ns": value["started_unix_ns"], "completed_unix_ns": value["completed_unix_ns"],
                                        "route_sha256": digest(value["route"]), "body_sha256": digest(value["body"])}
                                       for value in self.http_evidence]},
                "private_artifact": artifact(self.private / (mutation_id + ".ack.json")),
                "private_snapshots": [before["private_artifact"], after["private_artifact"]]}

    def ack_probe_progress(self, mutation_id, before_id, after_id, probe_reference):
        mutation = next((value for value in self.binding["mutations"] if value["id"] == mutation_id), None)
        need(mutation is not None and mutation["kind"] == "progress", "probe_progress_not_frozen")
        need(not (self.private / (safe_id(mutation_id) + ".ack.json")).exists(), "mutation_already_acknowledged")
        reply = self.private_reference(probe_reference, ".probe.json")
        need(reply["schema_version"] == 1 and reply["vmid"] == 106 and reply["owner_id"] == self.binding["owner_id"]
             and reply["operation"] == "playback_resume" and reply["status"] == "ok", "probe_progress_receipt")
        data = reply["data"]
        user, item, ticks = mutation["user_id"], mutation["item_id"], int(mutation["position_ticks"])
        need(data["user_id"] == user and data["item_id"] == item and data["reconnected"] is True
             and data["persisted_ticks"] == data["requested_ticks"] == ticks, "probe_progress_scope")
        before_path, after_path = self.snapshot_path(before_id), self.snapshot_path(after_id)
        before_reference, after_reference = artifact(before_path), artifact(after_path)
        need(data["snapshot_sha256"] == before_reference["sha256"], "probe_progress_snapshot_hash")
        before, before_summary = self.open_snapshot(before_id)
        after = None
        try:
            after, after_summary = self.open_snapshot(after_id)
            need(before_summary["run_id"] == after_summary["run_id"] == self.binding["run_id"], "probe_progress_snapshot_run")
            previous = self.row(before, "user_item_data", [user, item])
            observed = self.row(after, "user_item_data", [user, item])
            need(previous is not None and observed is not None and previous["playback_position_ticks"] == ticks,
                 "probe_progress_preimage")
            play = safe_id(data["play_session_id"])
            prior_session = self.row(before, "play_sessions", [play])
            session = self.row(after, "play_sessions", [play])
            need(session is not None and session["user_id"] == user and session["item_id"] == item
                 and session["media_source_id"] == data["current_media_source_id"] and session["counted"] is True
                 and session["state"] == "Stopped" and session["position_ticks"] == ticks, "probe_progress_session_postimage")
            if prior_session is not None:
                need(prior_session["user_id"] == user and prior_session["item_id"] == item
                     and prior_session["media_source_id"] == session["media_source_id"], "probe_progress_prior_session")
            increment = prior_session is None or prior_session["counted"] is False
            count = min(previous["play_count"] + int(increment), 2147483647)
            expected = {"playback_position_ticks": ticks, "play_count": count}
            need(all(observed[key] == value for key, value in expected.items()), "probe_progress_database_readback")
            guard_postimage("progress", "user_item_data", previous, observed, expected)
        finally:
            before.close()
            if after is not None:
                after.close()
        receipts = data["http_evidence"]
        need(type(receipts) is list and 5 <= len(receipts) <= 16, "probe_progress_http_inventory")
        routes = {"playing": "/emby/Sessions/Playing", "stopped": "/emby/Sessions/Playing/Stopped"}
        selected = {}
        for label, route in routes.items():
            rows = [row for row in receipts if row["method"] == "POST" and row["path_sha256"] == hashlib.sha256(route.encode()).hexdigest()]
            need(len(rows) == 1 and rows[0]["http_status"] in (200, 204) and rows[0]["client_timed_out"] is False
                 and rows[0]["client_disconnected"] is False, "probe_progress_playing_stopped")
            selected[label] = rows[0]
        need(selected["playing"]["completed_unix_ns"] <= selected["stopped"]["started_unix_ns"], "probe_progress_http_order")
        self.http_identity()
        readback, _ = self.request("GET", "/emby/Users/" + urllib.parse.quote(user, safe="") + "/Items/" + urllib.parse.quote(item, safe=""), audience="emby")
        need(readback["UserData"]["PlaybackPositionTicks"] == ticks and readback["UserData"]["PlayCount"] == count,
             "probe_progress_http_readback")
        for label, row in selected.items():
            self.http_evidence.append({"method": "POST", "route": routes[label], "status": row["http_status"],
                "started_unix_ns": str(row["started_unix_ns"]), "completed_unix_ns": str(row["completed_unix_ns"]),
                "body": {"observed_response_sha256": row["response_sha256"], "bytes_received": row["bytes_received"]},
                "body_is_digest_projection": True, "probe_receipt_sha256": probe_reference["sha256"]})
        before_result = {"snapshot_id": before_id, "private_artifact": before_reference}
        after_result = {"snapshot_id": after_id, "private_artifact": after_reference}
        return self.finish_ack(mutation, before_result, after_result, [("user_item_data", [user, item], expected)],
                               {"probe_receipt": probe_reference, "probe_progress": data}, allow_unchanged=True)

    def apply_mutation(self, mutation, connection, rebind_observation=None):
        kind = mutation["kind"]
        user = mutation.get("user_id")
        item = mutation.get("item_id")
        quote = lambda value: urllib.parse.quote(value, safe="")
        if kind == "user_name":
            route = "/admin/v1/users/" + quote(user)
            current, _ = self.request("GET", route)
            current = current["User"]
            name = mutation["name"]
            need(isinstance(name, str) and re.fullmatch(r"[A-Za-z][A-Za-z0-9_. -]{0,126}[A-Za-z0-9]", name) and name != current["Name"], "user_name_contract")
            body = {key: current[key] for key in ("Revision", "IsAdministrator", "IsDisabled", "Policy")}
            body["Name"] = name
            self.request("PUT", route, body)
            readback, _ = self.request("GET", route)
            need(readback["User"]["Name"] == name and int(readback["User"]["Revision"]) > int(current["Revision"]), "user_http_readback")
            expected_policy = dict(self.row(connection, "users", [user])["policy"], **body["Policy"])
            expected_policy.update(IsAdministrator=body["IsAdministrator"], IsDisabled=body["IsDisabled"])
            need(int(readback["User"]["Revision"]) == int(current["Revision"]) + 1, "user_revision_increment")
            return [("users", [user], {"name": name, "normalized_name": name.lower(), "policy": expected_policy,
                                      "management_revision": int(readback["User"]["Revision"])})]
        if kind == "favorite":
            value = mutation["value"]
            need(type(value) is bool, "favorite_contract")
            old = self.row(connection, "user_item_data", [user, item])
            need(old is None and value or old is not None and old["is_favorite"] != value, "favorite_no_change")
            self.request("POST" if value else "DELETE", "/emby/Users/" + quote(user) + "/FavoriteItems/" + quote(item), audience="emby")
            readback, _ = self.request("GET", "/emby/Users/" + quote(user) + "/Items/" + quote(item), audience="emby")
            need(readback["UserData"]["IsFavorite"] is value, "favorite_http_readback")
            return [("user_item_data", [user, item], {"is_favorite": value})]
        if kind == "progress":
            ticks = mutation["position_ticks"]
            need(isinstance(ticks, str) and re.fullmatch(r"[1-9][0-9]{0,18}", ticks) and int(ticks) < 2**63, "progress_exact_int64")
            previous = self.row(connection, "user_item_data", [user, item])
            need(previous is None or previous["playback_position_ticks"] != int(ticks), "progress_no_change")
            source = mutation["media_source_id"]
            safe_id(source)
            body = {"UserId": user, "MediaSourceId": source, "IsPlayback": True,
                    "EnableDirectPlay": True, "EnableDirectStream": True, "EnableTranscoding": False}
            plan, _ = self.request("POST", "/emby/Items/" + quote(item) + "/PlaybackInfo", body, audience="emby")
            play = safe_id(plan["PlaySessionId"])
            prior_play = self.row(connection, "play_sessions", [play])
            count = (previous or state_defaults(user, item))["play_count"]
            if prior_play is None or not prior_play["counted"]:
                count = min(count + 1, 2147483647)
            report = {"PlaySessionId": play, "ItemId": item, "MediaSourceId": source, "PositionTicks": int(ticks), "IsPaused": False}
            self.request("POST", "/emby/Sessions/Playing", report, audience="emby", accepted=(204,))
            self.request("POST", "/emby/Sessions/Playing/Progress", report, audience="emby", accepted=(204,))
            readback, _ = self.request("GET", "/emby/Users/" + quote(user) + "/Items/" + quote(item), audience="emby")
            need(readback["UserData"]["PlaybackPositionTicks"] == int(ticks), "progress_http_readback")
            return [("user_item_data", [user, item], {"playback_position_ticks": int(ticks), "play_count": count})]
        if kind == "preferences":
            patch = mutation["configuration"]
            need(isinstance(patch, dict) and patch and set(patch) <= PREFERENCE_FIELDS, "preference_allowlist")
            route = "/admin/v1/users/" + quote(user) + "/preferences"
            current, _ = self.request("GET", route)
            need(any(current["Configuration"].get(key) != value for key, value in patch.items()), "preferences_no_change")
            self.request("PUT", route, {"Revision": current["Revision"], "Configuration": patch})
            readback, _ = self.request("GET", route)
            need(all(readback["Configuration"].get(key) == value for key, value in patch.items()), "preferences_http_readback")
            need(int(readback["Revision"]) == int(current["Revision"]) + 1, "preferences_revision_increment")
            original = self.row(connection, "users", [user])["configuration"]
            changed = {key: readback["Configuration"][key] for key, value in patch.items()
                       if current["Configuration"].get(key) != value}
            return [("users", [user], {"configuration": dict(original, **changed), "configuration_revision": int(readback["Revision"])})]
        if kind == "display_preferences":
            preferences_id, client = safe_id(mutation["preferences_id"]), safe_id(mutation["client"])
            route = "/emby/DisplayPreferences/" + quote(preferences_id) + "?" + urllib.parse.urlencode({"UserId": user, "Client": client})
            current, _ = self.request("GET", route, audience="emby")
            patch = mutation["preferences"]
            need(isinstance(patch, dict) and patch and set(patch) <= {"SortBy", "SortOrder", "CustomPrefs"}, "display_preference_allowlist")
            need(any(current.get(key) != value for key, value in patch.items()), "display_preferences_no_change")
            body = dict(patch, Id=preferences_id, Client=client, Revision=current["Revision"])
            self.request("POST", route, body, audience="emby")
            readback, _ = self.request("GET", route, audience="emby")
            need(all(readback.get(key) == value for key, value in patch.items()), "display_preferences_http_readback")
            need(int(readback["Revision"]) == int(current["Revision"]) + 1, "display_revision_increment")
            stored = {key: readback[key] for key in ("Id", "Client", "SortBy", "SortOrder", "CustomPrefs")}
            return [("display_preferences", [user, client, preferences_id], {"preferences": stored, "revision": int(readback["Revision"])})]
        if kind == "metadata":
            route = "/admin/v1/items/" + quote(item) + "/metadata"
            current, _ = self.request("GET", route)
            patch = mutation["overrides"]
            need(isinstance(patch, dict) and patch and set(patch) <= {"Name", "SortName", "Overview", "Tags", "Genres"}, "metadata_field_allowlist")
            overrides = dict(current["Overrides"], **patch)
            need(overrides != current["Overrides"], "metadata_no_change")
            body = {"Revision": current["Revision"], "Overrides": overrides, "LockedFields": current["LockedFields"]}
            self.request("PUT", route, body)
            readback, _ = self.request("GET", route)
            need(readback["Overrides"] == overrides and all(readback["Effective"].get(key) == value for key, value in patch.items()), "metadata_http_readback")
            return [("metadata_admin", [item], {"overrides": overrides, "last_edited_by": readback["LastEditedBy"]})]
        if kind == "managed_configuration":
            name = mutation["server_name"]
            need(type(name) is str and re.fullmatch(r"[A-Za-z][A-Za-z0-9_. -]{0,126}[A-Za-z0-9]", name), "managed_server_name_contract")
            current, _ = self.request("GET", "/admin/v1/settings")
            need(current["ServerNameMode"] != "custom" or current["Overrides"]["ServerName"] != name,
                 "managed_configuration_no_change")
            overrides = dict(current["Overrides"], ServerName=name)
            self.request("PUT", "/admin/v1/settings", {"Revision": current["Revision"], "Overrides": overrides, "ServerNameMode": "custom"})
            readback, _ = self.request("GET", "/admin/v1/settings")
            need(readback["ServerNameMode"] == "custom" and readback["Overrides"] == overrides and readback["Effective"]["ServerName"] == name
                 and int(readback["Revision"]) == int(current["Revision"]) + 1, "managed_configuration_http_readback")
            return [("managed_settings", [1], {"server_name": name, "server_name_mode": "custom", "revision": int(readback["Revision"])})]
        if kind == "analysis_configuration":
            patch = mutation["profile"]
            need(isinstance(patch, dict) and patch and set(patch) <= set(PROFILE_FIELDS), "analysis_profile_allowlist")
            current, _ = self.request("GET", "/admin/v1/media-analysis")
            current = current["Configuration"]
            profile = dict(current["Profile"], **patch)
            need(profile != current["Profile"], "analysis_configuration_no_change")
            self.request("PUT", "/admin/v1/media-analysis/configuration", {"Revision": current["Revision"], "Profile": profile})
            readback, _ = self.request("GET", "/admin/v1/media-analysis")
            readback = readback["Configuration"]
            need(readback["Profile"] == profile, "analysis_configuration_http_readback")
            need(int(readback["Revision"]) == int(current["Revision"]) + 1, "analysis_revision_increment")
            expected = {PROFILE_FIELDS[key]: value for key, value in profile.items()}
            expected["revision"] = int(readback["Revision"])
            return [("analysis_settings", [1], expected)]
        if kind == "root_rebind":
            need(mutation.get("acknowledge_missing_removal") is True, "explicit_rebind_acknowledgement_required")
            need(rebind_observation is not None and all(rebind_observation[key] == mutation[key]
                 for key in ("root_id", "library_id", "stage")), "rebind_observation_scope")
            volume = self.rebind_volume(mutation["root_id"], mutation["stage"])
            need(volume == rebind_observation["volume"], "rebind_reviewed_volume_changed")
            route = "/admin/v1/libraries/" + quote(mutation["library_id"]) + "/roots/" + quote(mutation["root_id"]) + "/binding"
            current, _ = self.request("GET", route)
            current = current["Binding"]
            reviewed = rebind_observation["binding"]
            need(current["Status"] == "mismatch" and current["ObservedFingerprint"] == reviewed["ObservedFingerprint"]
                 and current["Revision"] == reviewed["Revision"], "rebind_reviewed_fingerprint_mismatch")
            self.request("PUT", route, {"Revision": current["Revision"], "ObservedFingerprint": current["ObservedFingerprint"], "AcknowledgeMissingRemoval": True})
            readback, _ = self.request("GET", route)
            readback = readback["Binding"]
            need(readback["Status"] == "verified" and readback["ApprovedFingerprint"] == current["ObservedFingerprint"] and
                 int(readback["Revision"]) == int(current["Revision"]) + 1, "rebind_http_readback")
            need(self.rebind_volume(mutation["root_id"], mutation["stage"]) == volume, "rebind_volume_changed_after_write")
            library_id = mutation["library_id"]
            previous_library = self.row(connection, "libraries", [library_id])
            return [("library_roots", [mutation["root_id"]], {"binding_revision": int(readback["Revision"]), "bound_by": readback["BoundBy"]}),
                    ("libraries", [library_id], {"revision": previous_library["revision"] + 1})]
        raise StateError("mutation_family")

    def compared_scope(self, table, row):
        if row is None:
            return False
        scope = self.binding["scope"]
        if table == "users":
            return row["id"] in scope["user_ids"]
        if table in ("user_item_data", "display_preferences"):
            return row["user_id"] in scope["user_ids"]
        if table in ("metadata_admin", "item_intro_state", "analysis_intro_decisions"):
            return row["item_id"] in scope["item_ids"]
        if table == "libraries":
            return row["id"] in scope["library_ids"]
        if table == "library_roots":
            return row["id"] in scope["root_ids"]
        return True

    def compare(self, before_id, after_id, ack_ids, expectation, artifact_sha256):
        safe_id(before_id)
        safe_id(after_id)
        need(expectation in ("preserve", "interrupted", "replacement-unbound", "restored-original", "explicit-rebind"), "comparison_expectation")
        need(isinstance(ack_ids, list) and len(ack_ids) <= 1024 and len(set(ack_ids)) == len(ack_ids), "ack_list_bound")
        need(set(artifact_sha256) == {before_id + ".sqlite", after_id + ".sqlite"} | {safe_id(value) + ".ack.json" for value in ack_ids}, "comparison_artifact_set")
        for name, expected_hash in artifact_sha256.items():
            need(SHA256.fullmatch(expected_hash) and artifact(self.evidence_path(name))["sha256"] == expected_hash, "external_artifact_hash_mismatch")
        overlays, chains = {}, {}
        rebind_roots = set()
        for ack_id in ack_ids:
            ack = decode(private_read(self.evidence_path(safe_id(ack_id) + ".ack.json"), 16 << 20))
            need(ack["binding_sha256"] == self.binding_sha256 and ack["database_verified"] and ack["http_readback_verified"], "ack_binding_mismatch")
            if ack["kind"] == "root_rebind":
                rebind_roots.update(row["key"][0] for row in ack["postimages"] if row["table"] == "library_roots")
            for row in ack["postimages"]:
                target = (row["table"], canonical(row["key"]).decode())
                chain = chains.setdefault(target, [])
                if chain:
                    need(chain[-1]["row"] == row["before_row"], "ack_postimage_chain_mismatch")
                chain.append(row)
        before, before_summary = self.open_snapshot(before_id)
        after = None
        failures, changes, transitions = [], [], []
        try:
            after, after_summary = self.open_snapshot(after_id)
            for (table, key), chain in chains.items():
                baseline = self.row(before, table, decode(key))
                known_images = [chain[0]["before_row"]] + [record["row"] for record in chain]
                need(any(baseline == image for image in known_images), "ack_does_not_extend_baseline")
                overlays[(table, key)] = chain[-1]["row"]
                # A newly acknowledged row may be absent from both snapshots
                # after rollback. It must not disappear from the merge's key
                # universe merely because neither cursor can enumerate it.
                actual = self.row(after, table, decode(key))
                if actual != chain[-1]["row"]:
                    failures.append({"code": "acknowledged_postimage_lost", "table": table, "key_sha256": digest(key),
                                     "expected_sha256": digest(chain[-1]["row"]), "actual_sha256": digest(actual)})
            active_scan_libraries = set()
            for (payload,) in before.execute("SELECT payload FROM rows WHERE table_name='scan_jobs'"):
                row = decode(payload)
                if row["status"] in TASK_STATES["scan_jobs"][0]:
                    active_scan_libraries.add(row["library_id"])
            allowed_additions = 0
            for table in STRICT_TABLES:
                left = before.execute("SELECT row_key,payload FROM rows WHERE table_name=? ORDER BY row_key", (table,))
                right = after.execute("SELECT row_key,payload FROM rows WHERE table_name=? ORDER BY row_key", (table,))
                a, b = left.fetchone(), right.fetchone()
                count, bad = 0, 0
                while a is not None or b is not None:
                    self.remaining()
                    key = min(row[0] for row in (a, b) if row is not None)
                    av = decode(a[1]) if a is not None and a[0] == key else None
                    bv = decode(b[1]) if b is not None and b[0] == key else None
                    wanted = overlays.get((table, key), av)
                    scoped = self.compared_scope(table, av) or self.compared_scope(table, bv)
                    admitted_addition = (table == "catalog_identity" and av is None and bv is not None and
                                         bv["library_id"] in active_scan_libraries and expectation == "interrupted")
                    if admitted_addition:
                        allowed_additions += 1
                    elif scoped and wanted != bv:
                        bad += 1
                        if len(changes) < 64:
                            changes.append({"table": table, "key_sha256": digest(key), "before_sha256": digest(wanted), "after_sha256": digest(bv)})
                    count += 1
                    if a is not None and a[0] == key:
                        a = left.fetchone()
                    if b is not None and b[0] == key:
                        b = right.fetchone()
                if bad:
                    failures.append({"code": "durable_state_changed", "table": table, "different_rows": bad, "compared_rows": count})
            for table, (active, terminal, field) in TASK_STATES.items():
                for (payload,) in before.execute("SELECT payload FROM rows WHERE table_name=? ORDER BY row_key", (table,)):
                    self.remaining()
                    old = decode(payload)
                    new = self.row(after, table, [old["id"]])
                    if new is None:
                        failures.append({"code": "task_record_lost", "table": table, "id": old["id"]})
                    elif old[field] in active:
                        mutable = set(TASK_MUTABLE)
                        if table == "task_run_children":
                            mutable.update(key for key in ("executor_token", "scan_job_id") if old.get(key) is None)
                        old_immutable = {key: value for key, value in old.items() if key not in mutable}
                        new_immutable = {key: value for key, value in new.items() if key not in mutable}
                        if old_immutable != new_immutable:
                            failures.append({"code": "task_admission_changed", "table": table, "id": old["id"]})
                        if expectation == "interrupted" and new[field] not in terminal:
                            failures.append({"code": "interrupted_work_still_active", "table": table, "id": old["id"]})
                        if new[field] == "interrupted" and table in ("task_runs", "task_run_children"):
                            expected_error = "scan_interrupted" if table == "task_run_children" and new.get("scan_job_id") else "server_interrupted"
                            if new["error_code"] != expected_error:
                                failures.append({"code": "interrupted_task_reason_mismatch", "table": table, "id": old["id"]})
                        transitions.append({"table": table, "id": old["id"], "before": old[field], "after": new[field]})
                    elif old != new:
                        failures.append({"code": "terminal_task_changed", "table": table, "id": old["id"]})
            for table in ("analysis_run_profiles", "analysis_work", "analysis_work_sources"):
                for key, payload in before.execute("SELECT row_key,payload FROM rows WHERE table_name=? ORDER BY row_key", (table,)):
                    self.remaining()
                    new = after.execute("SELECT payload FROM rows WHERE table_name=? AND row_key=?", (table, key)).fetchone()
                    if new is None or new[0] != payload:
                        failures.append({"code": "analysis_admission_changed", "table": table, "key_sha256": digest(key)})
            need(before_summary["runtime"] is not None and after_summary["runtime"] is not None, "runtime_snapshot_required")
            runtime = after_summary["runtime"]
            if not runtime["ready"]:
                failures.append({"code": "service_not_ready"})
            for category in ("unsafe", "missing_referenced"):
                if runtime["cache"]["counts"][category]:
                    failures.append({"code": "derivative_integrity_failure", "category": category,
                                     "count": runtime["cache"]["counts"][category]})
            before_roots = {root["Id"]: root for root in before_summary["runtime"]["roots"]}
            for root in runtime["roots"]:
                old = before_roots[root["Id"]]
                if root["Id"] not in self.binding["scope"]["fault_root_ids"]:
                    if root["Status"] != "verified" or root.get("ApprovedFingerprint") != old.get("ApprovedFingerprint"):
                        failures.append({"code": "healthy_root_unavailable", "id": root["Id"]})
                    continue
                if expectation == "replacement-unbound" and (root["Status"] not in ("mismatch", "unavailable") or root["Revision"] != old["Revision"]):
                    failures.append({"code": "replacement_was_implicitly_rebound", "id": root["Id"]})
                elif expectation == "restored-original" and (root["Status"] != "verified" or root["Revision"] != old["Revision"] or root.get("ApprovedFingerprint") != old.get("ApprovedFingerprint")):
                    failures.append({"code": "original_root_identity_not_restored", "id": root["Id"]})
                elif expectation == "explicit-rebind":
                    if root["Id"] not in rebind_roots or root["Status"] != "verified" or root.get("ApprovedFingerprint") != root.get("ObservedFingerprint"):
                        failures.append({"code": "explicit_rebind_not_observed", "id": root["Id"]})
                elif expectation in ("preserve", "interrupted") and (root["Status"] != "verified" or
                        root.get("ApprovedFingerprint") != old.get("ApprovedFingerprint") or root["Revision"] != old["Revision"]):
                    failures.append({"code": "root_binding_not_preserved", "id": root["Id"]})
            if expectation == "interrupted":
                if not transitions:
                    failures.append({"code": "no_interruptible_work_observed"})
                interruptions = [row for row in transitions if row["after"] in ("interrupted", "Interrupted")]
                if not interruptions:
                    failures.append({"code": "interrupted_task_not_observed"})
                counts = runtime["cache"]["counts"]
                for field in ("temporary", "trash"):
                    if counts[field]:
                        failures.append({"code": "derivative_recovery_incomplete", "category": field, "count": counts[field]})
                old_lifetimes = {(process["pid"], process["start_ticks"]) for process in before_summary["runtime"]["processes"]
                                 if process.get("is_media_worker")}
                same_boot = before_summary["runtime"]["boot_id"] == runtime["boot_id"]
                for process in runtime["processes"]:
                    if process.get("is_media_worker") and (not process["attached_to_application"] or
                            same_boot and (process["pid"], process["start_ticks"]) in old_lifetimes):
                        failures.append({"code": "orphan_or_surviving_prefault_worker", "pid": process["pid"]})
            need(len(failures) <= 100000, "comparison_failure_bound")
            result = {"before_id": before_id, "after_id": after_id, "expectation": expectation,
                      "passed": not failures, "failures": failures[:128], "failure_count": len(failures),
                      "differences": changes, "difference_details_truncated": sum(row.get("different_rows", 0) for row in failures) > len(changes),
                      "task_transitions": transitions[:4096], "task_transition_count": len(transitions),
                      "allowed_inflight_catalog_additions": allowed_additions,
                      "volatile_tables_excluded": ["sessions", "play_sessions"],
                      "derived_tables_observed_separately": ["items", "item_metadata_state", "analysis_previews", "analysis_detections", "analysis_feature_cache"]}
            result["sha256"] = digest(result)
            return result
        finally:
            before.close()
            if after is not None:
                after.close()


def external_reference(reference, kind):
    """Verify a controller-owned copy without following a guest-supplied path."""
    field = "snapshot_id" if kind == "snapshot" else "mutation_id"
    identifier = safe_id(reference[field])
    path = Path(reference["path"])
    checked_directory(path.parent)
    need(path.is_absolute(), "external_path_absolute")
    info = path.lstat()
    maximum = 4 << 30 if kind == "snapshot" else 16 << 20
    need(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.getuid() and
         info.st_nlink == 1 and 0 < info.st_size <= maximum and info.st_size == reference["bytes"], "external_artifact_contract")
    need(SHA256.fullmatch(reference["sha256"]) and artifact(path)["sha256"] == reference["sha256"], "external_artifact_hash_mismatch")
    return identifier + (".sqlite" if kind == "snapshot" else ".ack.json")


def external_observer(binding_sha256, scope, references):
    need(isinstance(binding_sha256, str) and SHA256.fullmatch(binding_sha256), "external_binding_hash")
    for key, maximum in (("user_ids", 128), ("item_ids", 256), ("library_ids", 128), ("root_ids", 128)):
        need(isinstance(scope[key], list) and 0 < len(scope[key]) <= maximum and len(set(scope[key])) == len(scope[key]), "external_scope_bound")
        for value in scope[key]:
            safe_id(value)
    need(scope["fault_root_ids"] and set(scope["fault_root_ids"]) <= set(scope["root_ids"]), "external_fault_root_scope")
    return Observer({"scope": scope}, external_artifacts=references, expected_binding_sha256=binding_sha256)


def compare_external(before_ref, after_ref, ack_refs, binding_sha256, scope, expectation):
    """Read only externally preserved files; do not access guest paths or APIs.

    Refs contain snapshot_id/mutation_id, controller path, bytes, and the hash
    durably retained outside the guest. Export filenames may have hash prefixes;
    this logical-name map avoids creating links or rewriting source evidence.
    """
    need(isinstance(ack_refs, list) and len(ack_refs) <= 1024, "external_ack_bound")
    references = {}
    for reference, kind in [(before_ref, "snapshot"), (after_ref, "snapshot")] + [(value, "ack") for value in ack_refs]:
        name = external_reference(reference, kind)
        need(name not in references, "external_duplicate_reference")
        references[name] = reference
    observer = external_observer(binding_sha256, scope, references)
    return observer.compare(before_ref["snapshot_id"], after_ref["snapshot_id"],
                            [value["mutation_id"] for value in ack_refs], expectation,
                            {name: value["sha256"] for name, value in references.items()})


def inspect_external(snapshot_ref, binding_sha256, scope):
    """Derive controller hashes and job facts from verified external SQLite rows."""
    name = external_reference(snapshot_ref, "snapshot")
    observer = external_observer(binding_sha256, scope, {name: snapshot_ref})
    connection, summary = observer.open_snapshot(snapshot_ref["snapshot_id"])
    try:
        grouped = {}
        groups = {
            "catalog_identity_sha256": ("catalog_identity",),
            "root_binding_sha256": ("library_roots",),
            "user_state_sha256": ("users", "user_item_data", "display_preferences", "metadata_admin", "item_intro_state", "analysis_intro_decisions"),
            "settings_sha256": ("managed_settings", "analysis_settings"),
        }
        for group, names in groups.items():
            entries = {}
            for table in names:
                count, checksum = 0, hashlib.sha256()
                for key, payload in connection.execute("SELECT row_key,payload FROM rows WHERE table_name=? ORDER BY row_key", (table,)):
                    observer.remaining()
                    row = decode(payload)
                    if observer.compared_scope(table, row):
                        checksum.update(canonical([key, row]) + b"\n")
                        count += 1
                entries[table] = {"count": count, "sha256": checksum.hexdigest()}
            grouped[group] = digest(entries)
        progress_fields = ("user_id", "item_id", "playback_position_ticks", "play_count", "played", "last_played_at", "hide_from_resume",
                           "remembered_media_source_id", "remembered_media_stamp", "remembered_audio_stream_index", "remembered_subtitle_stream_index")
        progress_hash, progress_rows = hashlib.sha256(), []
        for key, payload in connection.execute("SELECT row_key,payload FROM rows WHERE table_name='user_item_data' ORDER BY row_key"):
            observer.remaining()
            row = decode(payload)
            if row["user_id"] in scope["user_ids"]:
                projected = {field: row[field] for field in progress_fields}
                progress_hash.update(canonical([key, projected]) + b"\n")
                if row["item_id"] in scope["item_ids"]:
                    need(len(progress_rows) < 32768, "external_progress_scope_bound")
                    progress_rows.append(projected)
        grouped["playback_progress_sha256"] = progress_hash.hexdigest()
        jobs = []
        for table, (active, _, field) in TASK_STATES.items():
            for (payload,) in connection.execute("SELECT payload FROM rows WHERE table_name=? ORDER BY row_key", (table,)):
                row = decode(payload)
                if row[field] in active:
                    need(len(jobs) < 4096, "external_active_job_bound")
                    jobs.append({"table": table, "id": row["id"], "state": row[field], "task_key": row.get("task_key"),
                                 "run_id": row.get("run_id"), "library_id": row.get("library_id")})
        return {"summary": summary, "controller_state": dict(grouped, catalog_count=summary["tables"]["catalog_identity"]["count"]),
                "active_jobs": jobs, "runtime_observations": summary["runtime"], "private_progress_rows": progress_rows}
    finally:
        connection.close()


def inspect_probe_progress_images(observer, before, after, ack):
    data = ack["probe_progress"]
    need(ack["kind"] == "progress" and data["reconnected"] is True and len(ack["postimages"]) == 1,
         "external_probe_ack_family")
    user, item, play = safe_id(data["user_id"]), safe_id(data["item_id"]), safe_id(data["play_session_id"])
    ticks = data["persisted_ticks"]
    need(type(ticks) is int and 0 < ticks == data["requested_ticks"] < 2**63, "external_probe_progress_ticks")
    old = observer.row(before, "user_item_data", [user, item])
    current = observer.row(after, "user_item_data", [user, item])
    prior_session = observer.row(before, "play_sessions", [play])
    session = observer.row(after, "play_sessions", [play])
    need(old is not None and current is not None and old["playback_position_ticks"] == ticks,
         "external_probe_progress_preimage")
    need(session is not None and session["user_id"] == user and session["item_id"] == item and session["counted"] is True
         and session["state"] == "Stopped" and session["position_ticks"] == ticks
         and session["media_source_id"] == data["current_media_source_id"], "external_probe_stopped_session")
    if prior_session is not None:
        need(prior_session["user_id"] == user and prior_session["item_id"] == item and
             prior_session["media_source_id"] == session["media_source_id"], "external_probe_prior_session")
    count = min(old["play_count"] + int(prior_session is None or prior_session["counted"] is False), 2147483647)
    expected = {"playback_position_ticks": ticks, "play_count": count}
    image = ack["postimages"][0]
    need(image["table"] == "user_item_data" and image["key"] == [user, item] and image["expected_fields"] == expected
         and all(current[key] == value for key, value in expected.items()), "external_probe_progress_postimage")
    reference = ack["probe_receipt"]
    need(set(reference) == {"path", "bytes", "sha256"} and SHA256.fullmatch(reference["sha256"])
         and type(reference["bytes"]) is int and 0 < reference["bytes"] <= MAX_ROW, "external_probe_receipt_reference")
    for route in ("/emby/Sessions/Playing", "/emby/Sessions/Playing/Stopped"):
        records = [row for row in data["http_evidence"] if row["method"] == "POST"
                   and row["path_sha256"] == hashlib.sha256(route.encode()).hexdigest()]
        need(len(records) == 1 and records[0]["http_status"] in (200, 204) and records[0]["client_timed_out"] is False
             and records[0]["client_disconnected"] is False, "external_probe_http_evidence")


def inspect_ack_external(ack_ref, binding_sha256, before_ref, after_ref, scope):
    """Recheck an ACK against its actual external before/after SQLite records."""
    references = {}
    for reference, kind in ((ack_ref, "ack"), (before_ref, "snapshot"), (after_ref, "snapshot")):
        name = external_reference(reference, kind)
        need(name not in references, "external_duplicate_reference")
        references[name] = reference
    observer = external_observer(binding_sha256, scope, references)
    ack = decode(private_read(Path(ack_ref["path"]), 16 << 20))
    need(ack["binding_sha256"] == binding_sha256 and ack["mutation_id"] == ack_ref["mutation_id"] and ack["kind"] in FAMILIES and
         ack["before_snapshot"] == before_ref["snapshot_id"] and ack["after_snapshot"] == after_ref["snapshot_id"], "external_ack_binding")
    before, before_summary = observer.open_snapshot(before_ref["snapshot_id"])
    after = None
    try:
        after, after_summary = observer.open_snapshot(after_ref["snapshot_id"])
        need(before_summary["run_id"] == after_summary["run_id"] == ack["run_id"], "external_ack_run")
        if "probe_progress" in ack:
            need(ack["probe_progress"]["snapshot_sha256"] == before_ref["sha256"], "external_probe_snapshot_binding")
            inspect_probe_progress_images(observer, before, after, ack)
        need(0 < len(ack["postimages"]) <= 4, "external_ack_image_count")
        facts = []
        for image in ack["postimages"]:
            table, keys = image["table"], image["key"]
            need(table in STRICT_TABLES and observer.compared_scope(table, image["row"]), "external_ack_scope")
            prior = observer.row(before, table, keys)
            observed = observer.row(after, table, keys)
            unchanged_probe = (ack.get("readback_only") is True and ack["kind"] == "progress"
                               and table == "user_item_data" and type(ack.get("probe_receipt")) is dict
                               and ack.get("probe_progress", {}).get("reconnected") is True and prior == observed)
            need(prior == image["before_row"] and observed == image["row"] and (prior != observed or unchanged_probe) and observed is not None,
                 "external_ack_postimage_mismatch")
            expected = image["expected_fields"]
            need(all(observed.get(field) == value for field, value in expected.items()), "external_ack_expected_fields")
            guard_postimage(ack["kind"], table, prior, observed, expected)
            facts.append({"table": table, "key": keys, "before_sha256": digest(prior), "after_sha256": digest(observed),
                          "expected_fields_sha256": digest(expected)})
        responses = []
        need(0 < len(ack["http_evidence"]) <= 64, "external_ack_http_bound")
        for event in ack["http_evidence"]:
            need(event["method"] in ("GET", "POST", "PUT", "DELETE") and event["status"] in (200, 204), "external_ack_http_status")
            need(re.fullmatch(r"[0-9]{1,20}", event["started_unix_ns"]) and re.fullmatch(r"[0-9]{1,20}", event["completed_unix_ns"]), "external_ack_http_time")
            responses.append({"method": event["method"], "status": event["status"], "route_sha256": digest(event["route"]),
                              "body_sha256": digest(event["body"]), "started_unix_ns": event["started_unix_ns"],
                              "completed_unix_ns": event["completed_unix_ns"]})
        need(any(event["method"] != "GET" for event in responses), "external_ack_missing_write")
        return {"mutation_id": ack["mutation_id"], "kind": ack["kind"], "record_sha256": ack_ref["sha256"],
                "completed_unix_ns": ack["completed_unix_ns"], "completed_monotonic_ns": ack["completed_monotonic_ns"],
                "postimages": facts, "http_responses": responses, "postimage_sha256": digest(ack["postimages"])}
    finally:
        before.close()
        if after is not None:
            after.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binding", required=True)
    arguments = parser.parse_args()
    request_id = None
    lock_fd = None
    try:
        raw = sys.stdin.buffer.read(MAX_REQUEST + 1)
        need(len(raw) <= MAX_REQUEST, "rpc_request_bound")
        request = decode(raw)
        need(request.get("version") == VERSION, "rpc_version")
        request_id = safe_id(request["request_id"])
        observer = Observer(load_binding(arguments.binding))
        lock_fd = os.open(observer.private / ".observer.lock", os.O_RDWR | os.O_CREAT | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
        info = os.fstat(lock_fd)
        need(stat.S_ISREG(info.st_mode) and stat.S_IMODE(info.st_mode) == 0o600 and info.st_uid == os.getuid(), "observer_lock_contract")
        try:
            fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise StateError("observer_busy") from None
        operation = request["op"]
        common = {"version", "request_id", "op"}
        if operation in ("snapshot", "snapshot_database"):
            need(set(request) == common | {"snapshot_id"}, "rpc_snapshot_fields")
            result = observer.snapshot(request["snapshot_id"], runtime=operation == "snapshot")
        elif operation == "mutate":
            need(set(request) == common | {"mutation_id"}, "rpc_mutate_fields")
            result = observer.mutate(safe_id(request["mutation_id"]))
        elif operation == "observe_rebind":
            need(set(request) == common | {"observation_id", "stage", "root_id"}, "rpc_observe_rebind_fields")
            result = observer.observe_rebind(request["observation_id"], request["stage"], request["root_id"])
        elif operation == "rebind":
            need(set(request) == common | {"mutation_id", "observation"}, "rpc_rebind_fields")
            result = observer.rebind(safe_id(request["mutation_id"]), request["observation"])
        elif operation == "ack_probe_progress":
            need(set(request) == common | {"mutation_id", "before_snapshot_id", "after_snapshot_id", "probe_receipt"}, "rpc_probe_progress_fields")
            result = observer.ack_probe_progress(safe_id(request["mutation_id"]), request["before_snapshot_id"],
                                                request["after_snapshot_id"], request["probe_receipt"])
        elif operation == "readiness":
            need(set(request) == common, "rpc_readiness_fields")
            result = observer.runtime()
        elif operation == "compare":
            need(set(request) == common | {"before_id", "after_id", "ack_ids", "expectation", "artifact_sha256"}, "rpc_compare_fields")
            result = observer.compare(request["before_id"], request["after_id"], request["ack_ids"], request["expectation"], request["artifact_sha256"])
        else:
            raise StateError("rpc_operation")
        response = {"version": VERSION, "request_id": request_id, "ok": True, "result": result}
    except Exception as error:
        code = str(error) if isinstance(error, StateError) and re.fullmatch(r"[a-z0-9_]{1,96}", str(error)) else "observer_failed"
        response = {"version": VERSION, "request_id": request_id, "ok": False, "error": code}
    finally:
        if lock_fd is not None:
            os.close(lock_fd)
    sys.stdout.buffer.write(canonical(response) + b"\n")
    return 0 if response["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
