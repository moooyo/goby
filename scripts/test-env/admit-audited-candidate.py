#!/usr/bin/env python3
"""Admit one frozen, seeded candidate without playback or generation activation.

The seed helper owns the HTTP recorder and process pinning. This adapter owns
only the fixed admission sequence and its source-data reconciliation. It never
retries a business mutation, adopts another run, or clears the inactive stage.
"""
from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import sys
import time
from urllib.parse import urlencode


MIB = 1 << 20
LIMITS = {"maximumSeconds": 900, "cleanupSeconds": 180,
          "maximumRequests": 120, "cleanupRequests": 10}
CATALOG_RELATIVE = "internal/backuppg/catalogs/schema-28-postgresql-17.json"
SOURCE_MANIFEST_SHA = "6382c327bada158e06a3fda318d6bf85836a7be4457cf523314f87fbb64f8c0b"
INSPECTION_SHA = "2347a80728803612de59d4de34f6a90f7a1005a30f5ed4092813f1915548c5bf"
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
IDENTIFIER = re.compile(r"[a-z][a-z0-9_]{0,62}\Z")
STATUS_FIELDS = set("Available UnavailableReason RestoreAvailable RestoreUnavailableReason Busy "
                    "ActiveOperationId GenerationRevision Limits Storage Rollback".split())
OPERATION_FIELDS = set("Id RequestId Revision Kind State Phase BackupId CreatedAt UpdatedAt ErrorCode "
                       "Source RestoreDefaults ReplaceRollback CanCancel CanApply GenerationRevision".split())
BACKUP_FIELDS = set("Id Kind State CreatedAt UpdatedAt SizeBytes SHA256 Verified ErrorCode Source".split())
SESSION_ROUTE = "/admin/v1/session"
BACKUPS = "/admin/v1/backups"
OPERATIONS = "/admin/v1/backup-operations"
CONFIGURATION = "/emby/System/Configuration"
TERMINAL = {"completed", "failed", "cancelled", "interrupted"}


class AdmissionError(ValueError):
    """Expose fixed diagnostic codes without request values or credentials."""


def need(condition, code):
    if not condition:
        raise AdmissionError(code)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def parse(raw):
    def unique(pairs):
        value = {}
        for key, item in pairs:
            need(key not in value, "duplicate_json_field")
            value[key] = item
        return value
    return json.loads(raw.decode("utf-8"), object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(AdmissionError("nonfinite_json")))


def decimal(value, positive=False):
    need(isinstance(value, str) and re.fullmatch(r"0|[1-9][0-9]*", value) and
         len(value) <= 20 and int(value) <= (1 << 64) - 1 and (not positive or int(value) > 0),
         "invalid_decimal_projection")
    return int(value)


def descriptor(value):
    need(isinstance(value, dict) and set(value) == {"path", "sha256"} and
         isinstance(value["path"], str) and value["path"].startswith("/opt/") and
         ".." not in Path(value["path"]).parts and isinstance(value["sha256"], str) and
         HEX64.fullmatch(value["sha256"]), "invalid_input_descriptor")
    return value


def initial_read(path, checksum):
    """Load only the pinned input and seed module before its reader is available."""
    path = Path(path)
    need(path.is_absolute() and path.resolve(strict=True) == path and HEX64.fullmatch(checksum),
         "unsafe_initial_input")
    for parent in (path, *path.parents):
        info = parent.lstat()
        need(info.st_uid == 0 and not info.st_mode & 0o022 and not stat.S_ISLNK(info.st_mode),
             "unsafe_initial_input_owner")
    before = path.lstat()
    need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= 8 * MIB,
         "unsafe_initial_input_type")
    stable = lambda info: tuple(getattr(info, field) for field in
                               ("st_dev", "st_ino", "st_mode", "st_uid", "st_gid", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns"))
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        opened = os.fstat(stream.fileno())
        need(stable(opened) == stable(before), "initial_input_open_changed")
        raw = stream.read(8 * MIB + 1)
        need(stable(os.fstat(stream.fileno())) == stable(opened), "initial_input_read_changed")
    need(stable(path.lstat()) == stable(before) and len(raw) == before.st_size and sha(raw) == checksum,
         "initial_input_digest_changed")
    return raw


def import_bytes(name, path, raw):
    spec = importlib.util.spec_from_loader(name, loader=None, origin=path)
    module = importlib.util.module_from_spec(spec)
    module.__file__ = path
    sys.modules[name] = module
    exec(compile(raw, path, "exec"), module.__dict__)
    return module


def validate_input(value):
    fields = {"kind", "version", "runId", "candidateManifest", "seedManifest", "runtimeHelper",
              "runtimeInspection", "seedHelper", "admissionHelper", "inspectionHelper",
              "sourceManifest", "compiledCatalog", "output", "budgets"}
    need(isinstance(value, dict) and set(value) == fields and value["kind"] == "audited-candidate-admission-input"
         and value["version"] == 1 and value["budgets"] == LIMITS, "admission_input_contract")
    need(isinstance(value["runId"], str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}", value["runId"]),
         "invalid_admission_run_id")
    for key in fields - {"kind", "version", "runId", "output", "budgets"}:
        descriptor(value[key])
    need(value["sourceManifest"]["sha256"] == SOURCE_MANIFEST_SHA and
         value["inspectionHelper"]["sha256"] == INSPECTION_SHA, "wrong_frozen_source_or_inspector")
    output = Path(value["output"])
    need(output.is_absolute() and str(output).startswith("/opt/goby-test/") and ".." not in output.parts and
         output.name.startswith("candidate-live-admission-") and
         all(not Path(value[key]["path"]).is_relative_to(output) for key in fields
             - {"kind", "version", "runId", "output", "budgets"}), "invalid_admission_output_scope")
    return value


def validate_catalog(catalog, source, pin):
    need(catalog.get("version") == 28 and catalog.get("postgresql_major") == 17 and
         [row.get("version") for row in catalog.get("migrations", [])] == list(range(1, 29)),
         "catalog_not_current_schema28")
    need(source.get(CATALOG_RELATIVE, {}).get("sha256") == pin["sha256"], "catalog_source_cross_binding")
    tables = catalog.get("catalog", {}).get("Tables", [])
    need(1 <= len(tables) <= 64 and len({row["Name"] for row in tables}) == len(tables),
         "catalog_table_inventory")
    for row in tables:
        need(IDENTIFIER.fullmatch(row["Name"]) and row["Columns"] and row["SortKey"] and
             all(IDENTIFIER.fullmatch(name) for name in row["Columns"] + row["SortKey"]) and
             set(row["SortKey"]) <= set(row["Columns"]), "catalog_table_identifier")
    sequences = catalog["catalog"].get("Sequences", [])
    need(all(IDENTIFIER.fullmatch(row["Name"]) and row["Increment"] == 1 for row in sequences),
         "catalog_sequence_inventory")
    return tables


def snapshot_sql(catalog):
    """One SELECT gives a consistent row snapshot; sequences remain separate facts."""
    parts = []
    for row in catalog["catalog"]["Tables"]:
        name = row["Name"]
        order = ",".join('t."' + key + '"' for key in row["SortKey"])
        parts.append("SELECT '" + name + "' AS name,COALESCE(jsonb_agg(to_jsonb(t) ORDER BY " +
                     order + "),'[]'::jsonb) AS rows FROM public.\"" + name + '\" t')
    sequences = []
    for row in catalog["catalog"].get("Sequences", []):
        name = row["Name"]
        sequences.append("SELECT '" + name + "' AS name,jsonb_build_object('lastValue',last_value::text,"
                         "'isCalled',is_called) AS value FROM public.\"" + name + '\"')
    return ("SELECT json_build_object('capturedAt',clock_timestamp(),'tables',"
            "(SELECT jsonb_object_agg(name,rows) FROM (" + " UNION ALL ".join(parts) + ") t),"
            "'sequences',(SELECT jsonb_object_agg(name,value) FROM (" + " UNION ALL ".join(sequences) + ") s))")


def validate_status(value, generation=None, staged=False):
    need(isinstance(value, dict) and set(value) == STATUS_FIELDS and value["Available"] is True and
         value["RestoreAvailable"] is True and value["UnavailableReason"] == value["RestoreUnavailableReason"] == ""
         and value["Busy"] is False and value["ActiveOperationId"] == "", "recovery_not_available_and_idle")
    decimal(value["GenerationRevision"])
    need(generation is None or value["GenerationRevision"] == generation, "active_generation_changed")
    need(value["Rollback"]["Available"] is False and value["Rollback"]["MustReplace"] is staged,
         "inactive_slot_retention_differs")
    decimal(value["Storage"]["Bytes"])
    need(type(value["Storage"]["Objects"]) is int and value["Storage"]["Objects"] >= 0,
         "invalid_backup_storage_projection")
    limits = value["Limits"]
    need(set(limits) == {"MaxBackupBytes", "MaxStoredBytes", "MaxBackups", "MinPassphraseBytes", "MaxPassphraseBytes"} and
         decimal(limits["MaxBackupBytes"], positive=True) >= 64 and decimal(limits["MaxStoredBytes"], positive=True) >= 64 and
         type(limits["MaxBackups"]) is int and limits["MaxBackups"] >= 1 and limits["MinPassphraseBytes"] == 12 and
         limits["MaxPassphraseBytes"] == 1024, "backup_limits_projection")
    return value


def validate_operation(value, kind, request_id, operation_id=None, backup_id=None):
    need(isinstance(value, dict) and set(value) == OPERATION_FIELDS and HEX32.fullmatch(value["Id"])
         and value["Kind"] == kind and value["RequestId"] == request_id and
         (operation_id is None or value["Id"] == operation_id) and
         (backup_id is None or value["BackupId"] == backup_id), "operation_identity_changed")
    decimal(value["Revision"], positive=True)
    decimal(value["GenerationRevision"])
    need(type(value["CanApply"]) is type(value["CanCancel"]) is bool,
         "operation_capability_type")
    return value


def validate_ready(value, generation):
    need(value["Kind"] == "restore" and value["State"] == value["Phase"] == "ready" and
         value["ErrorCode"] == "" and value["CanApply"] is True and value["CanCancel"] is True and
         value["RestoreDefaults"] is False and value["ReplaceRollback"] is False and
         value["GenerationRevision"] == generation, "restore_not_ready")


def validate_cancelled(value):
    need(value["State"] == "cancelled" and value["Phase"] == "finished" and
         value["ErrorCode"] == "operation_cancelled" and value["CanApply"] is False and
         value["CanCancel"] is False, "restore_cancellation_incomplete")


def validate_backup(value, backup_id, server_id, table_names):
    need(isinstance(value, dict) and set(value) == BACKUP_FIELDS and value["Id"] == backup_id and
         value["Kind"] == "generated" and value["State"] == "ready" and value["Verified"] is True and
         value["ErrorCode"] == "" and HEX64.fullmatch(value["SHA256"]), "backup_not_ready_generated")
    size = decimal(value["SizeBytes"], positive=True)
    need(64 <= size <= 64 * MIB, "backup_download_limit")
    source = value["Source"]
    need(source is not None and source["ServerId"] == server_id and source["SchemaVersion"] == "28" and
         len(source["Tables"]) == len(table_names) and {row["Name"] for row in source["Tables"]} == set(table_names),
         "backup_current_source_catalog_differs")
    for row in source["Tables"]:
        decimal(row["Rows"])
    return size


def response_headers(result):
    headers = {}
    for key, value in result["headers"]:
        key = key.lower()
        need(key not in headers, "duplicate_response_header")
        headers[key] = value
    return headers


def validate_download(result, backup, mode):
    headers, size = response_headers(result), int(backup["SizeBytes"])
    need(headers.get("content-type") == "application/octet-stream" and
         headers.get("content-disposition") in ('attachment; filename=' + backup["Id"] + '.age',
                                                'attachment; filename="' + backup["Id"] + '.age"') and
         headers.get("etag") == '"' + backup["SHA256"] + '"' and
         headers.get("accept-ranges") == "bytes" and headers.get("cache-control") == "no-store",
         "backup_attachment_headers")
    count = 64 if mode == "range" else size
    need(headers.get("content-length") == str(count) and len(result["raw"]) == (0 if mode == "head" else count),
         "backup_attachment_incomplete")
    if mode == "range":
        need(result["status"] == 206 and headers.get("content-range") == "bytes 0-63/" + str(size),
             "backup_range_identity")
        raw = result["raw"]
        need(raw.startswith(b"age-encryption.org/v1\n-> scrypt ") and
             raw.splitlines()[1].split()[-1] == b"18", "backup_age_scrypt_header")
    elif mode == "full":
        need(sha(result["raw"]) == backup["SHA256"], "backup_complete_digest")


def bounded_inventory(value):
    need(isinstance(value, dict) and set(value) == {"Items", "TotalRecordCount", "StartIndex", "Limit"} and
         value["StartIndex"] == 0 and value["Limit"] == 25 and
         value["TotalRecordCount"] == len(value["Items"]) <= 23, "incomplete_initial_inventory")
    result = {row["Id"]: row for row in value["Items"]}
    need(len(result) == len(value["Items"]) and all(HEX32.fullmatch(key) for key in result),
         "invalid_inventory_id")
    return result


def indexed(rows, key="id"):
    result = {row[key]: row for row in rows}
    need(len(result) == len(rows), "duplicate_snapshot_identity")
    return result


def unexpected_native_authority(response, helper):
    if response and response.get("status") == 200:
        return helper.response_auth(response, "native")
    return None


def instant(value):
    need(isinstance(value, str), "missing_timestamp")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    need(parsed.tzinfo is not None, "timestamp_without_timezone")
    return parsed


def expected_activity(logins, create_id, backup_id, plan_id):
    result = []
    for login in logins.values():
        row = login["initialSession"]
        source = "native" if row["kind"] == "admin" else "emby"
        for action in ("session.login", "session.revoked"):
            result.append((action, source, "user", row["user_id"], row["id"], "session", row["id"], ""))
    admin = logins["admin"]["initialSession"]
    for action, resource_kind, resource_id, count in (
            ("backup.requested", "backup", create_id, 1),
            ("backup.downloaded", "backup", backup_id, 3),
            ("restore.requested", "restore", plan_id, 1),
            ("restore.cancel_requested", "restore", plan_id, 1)):
        result.extend([(action, "native", "user", admin["user_id"], admin["id"], resource_kind, resource_id, "")] * count)
    result.extend([("backup.finished", "system", "system", "", "", "backup", create_id, "completed"),
                   ("restore.planned", "system", "system", "", "", "restore", plan_id, "")])
    return Counter(result)


def reconcile_source(before, after, logins, create_id, backup_id, plan_id):
    need(set(before["tables"]) == set(after["tables"]), "source_table_inventory_changed")
    allowed = {"sessions", "devices", "activity_entries"}
    for table in set(before["tables"]) - allowed:
        need(before["tables"][table] == after["tables"][table], "unowned_source_table_changed:" + table)
    added = {}
    for table in allowed:
        old, new = indexed(before["tables"][table]), indexed(after["tables"][table])
        need(set(old) <= set(new) and all(new[key] == row for key, row in old.items()),
             "preexisting_source_rows_changed:" + table)
        added[table] = {key: row for key, row in new.items() if key not in old}
    need(set(logins) == {"admin", "P", "Q"} and len(added["sessions"]) == 3 and len(added["devices"]) == 2,
         "unexpected_session_or_device_count")
    expected_devices = set()
    for login in logins.values():
        initial = login["initialSession"]
        row = added["sessions"].get(initial["id"])
        need(row is not None and login.get("logout401") is True and
             row["token_hash"] == "\\x" + sha(login["auth"]["token"].encode()) and
             row["revoked_at"] is not None, "owned_session_not_revoked")
        need(all(value == row[key] for key, value in initial.items() if key not in {"last_seen_at", "revoked_at"}) and
             instant(initial["last_seen_at"]) <= instant(row["last_seen_at"]) <= instant(after["capturedAt"]) and
             instant(before["capturedAt"]) <= instant(row["created_at"]) <= instant(row["revoked_at"]) <= instant(after["capturedAt"]),
             "owned_session_fields_changed")
        if row["kind"] == "admin":
            need(row["device_registry_id"] is None, "native_session_registered_device")
            continue
        device = added["devices"].get(row["device_registry_id"])
        original = login["initialDevice"]
        need(device is not None and original is not None and device["id"] == original["id"] and
             device["reported_device_id"] == login["deviceId"] and device["last_user_id"] == row["user_id"] and
             all(value == device[key] for key, value in original.items() if key != "last_seen_at") and
             instant(original["last_seen_at"]) <= instant(device["last_seen_at"]) <= instant(after["capturedAt"]),
             "owned_device_fields_changed")
        expected_devices.add(device["id"])
    need(expected_devices == set(added["devices"]), "unowned_new_device")
    observed = []
    activity_keys = ("action", "source", "actor_kind", "actor_id", "actor_credential_id", "resource_kind", "resource_id", "state")
    for row in added["activity_entries"].values():
        need(row["severity"] == "Info" and row["request_id"] == row["observation_fingerprint"] == "" and
             row["revision"] == row["previous_revision"] == 0 and
             row["affected_count"] == (1 if row["action"].startswith("session.") else 0) and row["changed_fields"] == [] and
             instant(before["capturedAt"]) <= instant(row["created_at"]) <= instant(after["capturedAt"]),
             "unexpected_activity_fields")
        observed.append(tuple(row[key] for key in activity_keys))
    need(Counter(observed) == expected_activity(logins, create_id, backup_id, plan_id), "unexpected_activity_history")
    need(set(before["sequences"]) == set(after["sequences"]), "sequence_inventory_changed")
    for name, old in before["sequences"].items():
        new = after["sequences"][name]
        if name in {"devices_id_seq", "activity_entries_id_seq"}:
            need(new["isCalled"] is True and decimal(new["lastValue"]) >= decimal(old["lastValue"]),
                 "owned_sequence_rewound")
            table = "devices" if name == "devices_id_seq" else "activity_entries"
            issued = set(added[table])
            first_possible = int(old["lastValue"]) + (1 if old["isCalled"] else 0)
            need(issued and min(issued) >= first_possible and max(issued) == int(new["lastValue"]),
                 "owned_sequence_not_bound_to_issued_ids")
        else:
            need(new == old, "unowned_sequence_changed")
    return {"preservedTables": len(before["tables"]) - 3, "newSessions": 3, "newDevices": 2,
            "newActivityRows": len(observed), "oldRowsExact": True, "allControllerSessionsRevoked": True}


class Admission:
    def __init__(self, io, helper, inspector, seed, catalog):
        self.io, self.helper, self.inspector, self.seed, self.catalog = io, helper, inspector, seed, catalog
        self.phase, self.phase_counts = "preflight", Counter()
        self.logins, self.operations, self.cancel_attempts = {}, {}, set()
        self.samples, self.failures, self.evidence = [], [], {}
        self.started, self.cleanup_end = time.monotonic(), None
        self.lease, self.before, self.staged, self.backup = None, None, None, None
        self.create_request, self.plan_request = secrets.token_hex(16), secrets.token_hex(16)
        self.credentials = {}
        self.sequence = 0
        self.stage_caps = {"authentication": 19, "storage": 13, "create": 22, "plan": 21,
                           "cancel": 7, "final": 6}

    def save(self, name, value):
        return self.helper.write_json_once(self.io.private / (name + ".json"), value)

    def load(self, pin):
        descriptor(pin)
        return parse(self.helper.read_checked(pin["path"], pin["sha256"], limit=8 * MIB))

    def snapshot(self, name, target=False):
        self.io.deadline(self.cleanup_end is not None)
        value = self.inspector.sql_json(self.io.candidate["recoveryDatabase" if target else "database"], snapshot_sql(self.catalog))
        self.evidence[name] = self.save(name, value)
        return value

    def remaining(self, cleanup=False):
        left = self.io.deadline(cleanup)
        if cleanup:
            need(self.cleanup_end is not None and self.cleanup_end > time.monotonic(), "cleanup_deadline_expired")
            left = min(left, self.cleanup_end - time.monotonic())
        return left

    def wait(self, seconds):
        until = time.monotonic() + seconds
        while time.monotonic() < until:
            self.remaining(self.cleanup_end is not None)
            if self.cleanup_end is None:
                self.health_due()
            time.sleep(min(0.25, max(0, until - time.monotonic())))

    def req(self, label, method, route, body=None, role=None, expected=(200,), headers=None,
            maximum=2 * MIB, cleanup=False, health=False):
        self.remaining(cleanup)
        if not cleanup and not health:
            self.health_due()
            need(self.phase in self.stage_caps and self.phase_counts[self.phase] < self.stage_caps[self.phase],
                 "phase_request_budget_exhausted")
            self.phase_counts[self.phase] += 1
        auth = self.logins[role]["auth"] if role else None
        self.sequence += 1
        label = "%03d-%s" % (self.sequence, label)
        result = self.io.request(label, method, route, body=body, auth=auth, expected=expected,
                                 headers=headers, maximum=maximum, cleanup=cleanup)
        need(result.get("complete") is True and result.get("receipt") is not None, "response_not_durably_complete")
        values = response_headers(result)
        if method == "HEAD" or result["status"] == 204:
            need(result["raw"] == b"", "bodyless_response_has_payload")
        elif "content-length" in values:
            need(values["content-length"] == str(len(result["raw"])), "response_content_length_incomplete")
        return result

    def health_due(self):
        if self.lease is None or len(self.samples) == 11:
            return
        due = self.started + len(self.samples) * 60
        if time.monotonic() < due:
            return
        need(time.monotonic() - due <= 20, "stability_sample_deadline_missed")
        if not self.samples:
            self.started = time.monotonic()
        sample = {"index": len(self.samples), "elapsedMilliseconds": round((time.monotonic() - self.started) * 1000), "responses": []}
        for route, wanted in (("/healthz", {"Status": "ok"}), ("/readyz", {"Status": "ready"})):
            response = self.req("stability-" + route[1:], "GET", route, health=True, maximum=65536)
            need(response["body"] == wanted, "candidate_not_healthy_and_ready")
            sample["responses"].append(response["receipt"])
        need(self.inspector.deployment_lease() == self.lease, "candidate_lease_changed")
        sample["process"] = self.io.pin()
        self.samples.append(sample)
        self.save("stability-%02d" % sample["index"], sample)

    def resource_counters(self, name):
        process, unit = self.inspector.process("server", ("NRestarts", "MemoryCurrent", "MemoryPeak", "TasksCurrent"))
        group = Path("/sys/fs/cgroup") / process["cgroup"].lstrip("/")
        events = {}
        for filename in ("memory.events", "pids.events", "cpu.stat"):
            raw = (group / filename).read_bytes()
            need(len(raw) <= 8192, "resource_counter_size")
            events[filename] = dict(line.split() for line in raw.decode().splitlines())
        value = {"process": process, "unit": unit, "events": events}
        self.evidence[name] = self.save(name, value)
        return value

    def preflight(self):
        seed = self.seed
        need(seed["kind"] == "audited-candidate-seed-manifest" and seed["version"] == 1 and
             seed["status"] == "seeded_pending_live_acceptance" and seed["candidateManifest"] == self.io.value["candidateManifest"] and
             seed["runtimeInspection"] == self.io.value["runtimeInspection"] and seed["helper"] == self.io.value["seedHelper"] and
             seed["source"] == {"manifestSha256": SOURCE_MANIFEST_SHA, "binarySha256": self.io.candidate["binary"]["sha256"], "schema": 28} and
             seed["processes"]["candidate"] == self.io.pin() and seed["playbackRequests"] == 0 and
             len(seed["cleanup"]) == 2 and all(row["logoutAcknowledged"] and row["sameTokenRejected"] for row in seed["cleanup"]),
             "seed_candidate_or_cleanup_binding")
        need(self.load(seed["catalogFile"]) == seed["catalog"], "seed_catalog_descriptor_changed")
        source_input = self.load(self.io.candidate["input"])
        need(source_input["sourceManifest"] == self.io.value["sourceManifest"], "candidate_source_manifest_cross_binding")
        self.inspector.assert_target_cluster()
        self.inspector.database_facts("recovery")
        self.lease = self.inspector.deployment_lease()
        self.before = self.snapshot("source-before")
        tables = self.before["tables"]
        need(len(tables["users"]) == 8 and len(tables["libraries"]) == 3 and
             {row["id"] for row in tables["users"]} == set(seed["resources"]["users"]) and
             [row["version"] for row in tables["schema_migrations"]] == list(range(1, 29)), "seed_source_membership_changed")
        need(all(row["status"] == "Completed" for row in tables["scan_jobs"]) and
             all(row["state"] not in {"pending", "running", "stopping"} for row in tables["task_runs"]) and
             all(not tables[name] for name in ("play_sessions", "user_item_data", "client_playback_references", "encoding_jobs")),
             "seed_not_idle_zero_playback")
        self.server_id = seed["serverId"]
        need(HEX32.fullmatch(self.server_id), "invalid_seed_server_id")
        for role, actor in (("admin", seed["admin"]), ("P", seed["actors"]["movie"]), ("Q", seed["controlQ"])):
            credential = self.load(actor["credentials"])
            need(set(credential) == {"actorId", "serverId", "username", "password"} and credential["actorId"] == actor["id"] and
                 credential["serverId"] == self.server_id and credential["username"] == actor["username"], "actor_credential_cross_binding")
            self.credentials[role] = credential
        need(len({row["actorId"] for row in self.credentials.values()}) == 3, "admission_actor_collision")
        users = indexed(tables["users"])
        for role, credential in self.credentials.items():
            user = users[credential["actorId"]]
            need(user["name"] == credential["username"] and user["is_disabled"] is False and
                 user["is_administrator"] is (role == "admin"), "admission_actor_current_authority")
            if role != "admin":
                need(user["policy"]["EnableAllFolders"] is (role == "P") and user["policy"]["EnabledFolders"] == [],
                     "admission_library_policy_changed")
        expected_items = [seed["catalog"][key] for key in ("movie", "series", "mp3", "flac")] + seed["catalog"]["episodes"] + seed["catalog"]["seasons"]
        actual = indexed(tables["items"])
        for item in expected_items:
            row = actual.get(item["id"])
            need(row is not None and row["type"] == item["type"] and
                 ("path" not in item or row["path"] == item["path"]) and
                 ("parentId" not in item or row["parent_id"] == item["parentId"]), "seed_catalog_sql_mapping_changed")
            if "runtimeTicks" in item:
                need(row["media"]["DurationTicks"] == item["runtimeTicks"] and row["media"]["ProbeVersion"] == 6,
                     "seed_media_probe_mapping_changed")
        details = self.load(seed["actualCatalogDtos"])
        need(len(details) == len({row["Id"] for row in details}) == 10, "seed_actual_dto_inventory")
        for dto in details:
            row = actual.get(dto["Id"])
            need(row is not None and dto["Type"] == row["type"] and dto["ServerId"] == self.server_id and
                 dto.get("Path") == row["path"], "seed_actual_dto_source_binding")
            if row["type"] in {"Movie", "Episode", "Audio"}:
                need(dto["RunTimeTicks"] == row["media"]["DurationTicks"] and len(dto["MediaSources"]) == 1 and
                     dto["MediaSources"][0]["ItemId"] == row["id"] and dto["MediaSources"][0]["Path"] == row["path"],
                     "seed_actual_dto_media_binding")
        subtitles = [row for row in tables["item_subtitles"] if row["item_id"] == seed["catalog"]["movie"]["id"] and row["active"]]
        need(len(subtitles) == 2 and {(row["stream_index"], row["codec"], row["language"], row["source_hash"]) for row in subtitles} ==
             {(row["index"], row["codec"], row["language"], row["sha256"]) for row in seed["catalog"]["subtitles"]},
             "seed_subtitle_mapping_changed")
        self.counters_before = self.resource_counters("resources-before")
        self.started = time.monotonic()
        self.health_due()

    def bind_login(self, role):
        login, actor = self.logins[role], self.credentials[role]
        token_hash = sha(login["auth"]["token"].encode())
        rows = self.inspector.sql_json(self.io.candidate["database"],
            "SELECT COALESCE(json_agg(json_build_object('session',to_jsonb(s),'device',"
            "(SELECT to_jsonb(d) FROM devices d WHERE d.id=s.device_registry_id))),'[]'::json) "
            "FROM sessions s WHERE s.token_hash=decode('" + token_hash + "','hex')")
        need(len(rows) == 1, "login_credential_row_not_unique")
        row = rows[0]["session"]
        native = login["auth"]["kind"] == "native"
        need(row["id"] not in indexed(self.before["tables"]["sessions"]) and row["user_id"] == actor["actorId"] and
             row["kind"] == ("admin" if native else "emby") and row["revoked_at"] is None and
             row["token_hash"] == "\\x" + token_hash and row["client_capabilities"] == {}, "login_database_identity")
        if not native:
            need(row["device_id"] == login["deviceId"] and row["client_name"] == "Goby Candidate Admission" and
                 row["client_version"] == "1" and rows[0]["device"]["id"] not in indexed(self.before["tables"]["devices"]),
                 "login_device_scope")
        login.update(initialSession=row, initialDevice=rows[0]["device"])
        self.save("login-" + role.lower() + "-bound", {"actorId": actor["actorId"], "credentialId": row["id"],
                                                       "tokenSha256": token_hash, "session": row, "device": rows[0]["device"]})

    def login(self, role):
        actor = self.credentials[role]
        kind = "native" if role == "admin" else "emby"
        device = "candidate-admission-" + self.io.value["runId"] + "-" + role.lower()
        need(not any(row["reported_device_id"] == device for row in self.before["tables"]["devices"]), "device_id_already_exists")
        headers = None if role == "admin" else {"X-Emby-Authorization":
            'Emby Client="Goby Candidate Admission", Device="Linux", DeviceId="' + device + '", Version="1"'}
        label = "login-" + role.lower()
        route = SESSION_ROUTE if role == "admin" else "/emby/Users/AuthenticateByName"
        body = {"Name": actor["username"], "Password": actor["password"]} if role == "admin" else {"Username": actor["username"], "Pw": actor["password"]}
        attempted = None
        try:
            response = self.req(label, "POST", route, body=body, headers=headers)
        finally:
            attempted = self.io.last_response
            if attempted and attempted.get("label", "").endswith("-" + label) and attempted.get("status") == 200:
                auth = self.helper.response_auth(attempted, kind)
                self.logins[role] = {"auth": auth, "deviceId": device, "response": attempted["receipt"], "logout401": False}
                self.save("login-" + role.lower() + "-authority", self.logins[role])
        need(role in self.logins and response["body"]["User"]["Id"] == actor["actorId"] and
             response["body"]["User"]["Name"] == actor["username"], "login_actor_response")
        if role == "admin":
            need(response["body"]["CSRFToken"] == self.logins[role]["auth"]["csrf"], "native_csrf_not_returned_token")
            values = response_headers(response)
            need("HttpOnly" in values.get("set-cookie", "") and "SameSite=Strict" in values.get("set-cookie", "") and
                 "Path=/admin" in values.get("set-cookie", ""), "native_cookie_policy")
        else:
            need(response["body"]["ServerId"] == self.server_id, "login_server_response")
        self.bind_login(role)

    def authenticate(self):
        self.phase = "authentication"
        self.login("admin")
        self.req("native-session", "GET", SESSION_ROUTE, role="admin")
        self.overview = self.req("overview", "GET", "/admin/v1/overview", role="admin")["body"]
        need(self.overview["Server"]["Id"] == self.server_id and self.overview["Counts"]["Users"] == 8 and
             self.overview["Counts"]["Libraries"] == 3 and self.overview["Database"]["Status"] == "connected" and
             self.overview["Transcoding"] == {"Configured": True, "Available": True, "Reason": ""}, "native_overview_admission")
        info = self.req("public-info", "GET", "/emby/System/Info/Public")["body"]
        need(info["Id"] == self.server_id and info["StartupWizardCompleted"] is True and
             info["LocalAddress"] == self.io.candidate["publicUrl"], "public_candidate_identity")
        self.status_before = validate_status(self.req("backup-status", "GET", BACKUPS + "/status", role="admin")["body"])
        self.generation = self.status_before["GenerationRevision"]
        self.old_backups = bounded_inventory(self.req("backup-list-before", "GET", BACKUPS, role="admin")["body"])
        self.old_operations = bounded_inventory(self.req("operation-list-before", "GET", OPERATIONS, role="admin")["body"])
        actor = self.credentials["P"]
        try:
            denied = self.req("ordinary-native-login-denied", "POST", SESSION_ROUTE,
                              body={"Name": actor["username"], "Password": actor["password"]}, expected=(401,))
        finally:
            last = self.io.last_response
            if last and last.get("label", "").endswith("-ordinary-native-login-denied"):
                auth = unexpected_native_authority(last, self.helper)
                if auth:
                    self.credentials["unexpected-native"] = actor
                    self.logins["unexpected-native"] = {"auth": auth, "deviceId": "goby-dashboard", "response": last["receipt"], "logout401": False}
                    self.save("unexpected-native-authority", self.logins["unexpected-native"])
                    self.bind_login("unexpected-native")
        need(denied["body"]["Error"]["Code"] == "invalid_credentials" and "set-cookie" not in response_headers(denied),
             "ordinary_native_login_not_rejected")
        self.login("P")
        self.login("Q")
        token = self.logins["P"]["auth"]["token"]
        carriers = {"X-Emby-Token": token, "API_KEY": token, "X-Emby-Client": "Goby Candidate Admission",
                    "X-Emby-Client-Version": "1", "X-Emby-Device-Id": self.logins["P"]["deviceId"], "X-Emby-Device-Name": "Linux"}
        for label, route, role in (("ordinary-header", CONFIGURATION, "P"),
                                   ("matching-query", CONFIGURATION + "?" + urlencode(carriers), None)):
            need(self.req(label, "GET", route, role=role)["raw"] == b"{}", "ordinary_projection_broadened")
        denied = self.req("query-management-denied", "GET", "/emby/Devices?" + urlencode(carriers), expected=(403,))
        need(denied["raw"] == ("User " + actor["username"] + " does not have access to ManageServer feature.").encode(),
             "ordinary_management_rejection_differs")
        for label, query in (("conflicting-query-denied", urlencode({**carriers, "api_key": "conflicting-token"})),
                             ("malformed-query-denied", "X-Emby-Token=" + token + "&api_key=%ZZ")):
            denied = self.req(label, "GET", "/emby/Devices?" + query, expected=(401,))
            need(denied["raw"] == b"Access token is invalid or expired.", "carrier_rejection_differs")
        denied = self.req("unknown-query-denied", "GET", CONFIGURATION + "?" + urlencode({**carriers, "X-Emby-IsAdministrator": "true"}), expected=(400,))
        need(denied["raw"] == b"Supply valid supported configuration fields.", "unknown_business_query_rejection_differs")
        denied = self.req("emby-native-backup-denied", "GET", BACKUPS + "/status", role="P", expected=(401,))
        need(denied["body"]["Error"]["Code"] == "authentication_required", "native_cookie_transport_not_required")
        native = self.logins["admin"]["auth"]
        for label, headers, code in (
                ("missing-csrf-denied", {"Cookie": "goby_session=" + native["token"]}, "csrf_invalid"),
                ("foreign-origin-denied", {"Cookie": "goby_session=" + native["token"], "X-CSRF-Token": native["csrf"],
                                           "Origin": "http://foreign.invalid"}, "origin_denied")):
            result = self.req(label, "POST", BACKUPS, body={}, headers=headers, expected=(403,))
            need(result["body"]["Error"]["Code"] == code, "native_mutation_separation_failed")

    def fixture_files(self, label):
        result = []
        for pin in self.seed["resources"]["copiedFiles"]:
            row = self.load(pin)
            path = Path(row["path"])
            need(path.is_relative_to(self.helper.C / "data/media") and row["bytes"] <= 256 * MIB,
                 "fixture_path_escaped_candidate")
            before = self.helper.safe_path(path, (0, self.io.candidate["serverIdentity"]["uid"]))
            need(list(self.helper.file_identity(before)) == row["identity"] and before.st_nlink == 1,
                 "candidate_fixture_metadata_changed")
            digest = hashlib.sha256()
            with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
                need(self.helper.file_identity(os.fstat(stream.fileno())) == self.helper.file_identity(before), "fixture_open_changed")
                while chunk := stream.read(MIB):
                    self.remaining(self.cleanup_end is not None)
                    digest.update(chunk)
                need(self.helper.file_identity(os.fstat(stream.fileno())) == self.helper.file_identity(before), "fixture_read_changed")
            need(digest.hexdigest() == row["sha256"] and self.helper.file_identity(path.lstat()) == self.helper.file_identity(before),
                 "candidate_fixture_bytes_changed")
            result.append({"path": str(path), "sha256": digest.hexdigest(), "identity": row["identity"]})
        need(len(result) == 14 and len({row["path"] for row in result}) == 14, "candidate_fixture_membership")
        self.evidence[label] = self.save(label, result)
        return result

    def storage(self):
        self.phase = "storage"
        storage = self.req("storage-roots", "GET", "/admin/v1/storage/roots", role="admin")["body"]
        need(storage["Configured"] is True and storage["Items"] == [{"Path": str(self.helper.C / "data/media"), "Available": True}],
             "storage_root_not_available")
        libraries = self.helper.items(self.req("libraries", "GET", "/admin/v1/libraries", role="admin")["body"])
        wanted = self.seed["libraries"]
        need({row["Id"] for row in libraries} == {row["Id"] for row in wanted.values()}, "library_seed_membership_changed")
        stored_libraries = indexed(self.before["tables"]["libraries"])
        for actual in libraries:
            original = next(row for row in wanted.values() if row["Id"] == actual["Id"])
            stored = stored_libraries[actual["Id"]]
            need(all(actual[key] == original[key] for key in ("Id", "Name", "CollectionType", "Paths", "CreatedAt")) and
                 actual["LastScanAt"] is not None and instant(actual["LastScanAt"]) == instant(stored["last_scan_at"]),
                 "library_seed_projection_changed")
        jobs = self.helper.items(self.req("scan-jobs", "GET", "/admin/v1/jobs", role="admin")["body"])
        expected_jobs = {row["id"]: row["libraryId"] for row in self.seed["resources"]["jobs"]}
        need(len(jobs) == len(expected_jobs) == 3 and all(row["Id"] in expected_jobs and
             self.helper.completed_job(row, expected_jobs[row["Id"]], row["Id"]) for row in jobs), "seed_scan_completion_changed")
        roots = indexed(self.before["tables"]["library_roots"])
        for name, library in sorted(wanted.items()):
            route = "/admin/v1/libraries/" + library["Id"] + "/roots"
            registered = self.helper.items(self.req("registered-" + name.lower(), "GET", route, role="admin")["body"])
            need(len(registered) == 1 and registered[0]["Id"] in roots, "registered_root_not_owned")
            actual = registered[0]
            source = roots[actual["Id"]]
            need(actual["LibraryId"] == library["Id"] and actual["Path"] == library["Paths"][0] == source["path"] and
                 actual["AllowedPath"] == source["allowed_path"] and actual["RelativePath"] == source["relative_path"] and
                 actual["Revision"] == str(source["binding_revision"]), "registered_root_source_binding")
            binding = self.req("binding-" + name.lower(), "GET", route + "/" + actual["Id"] + "/binding", role="admin")["body"]["Binding"]
            need(all(binding[key] == value for key, value in actual.items()) and
                 binding["Status"] == ("unbound" if source["storage_binding"] is None else "verified") and
                 HEX64.fullmatch(binding.get("ObservedFingerprint", "")), "root_observation_not_available")
        for role in ("P", "Q"):
            rows = self.helper.items(self.req("views-" + role.lower(), "GET", "/emby/Users/" + self.credentials[role]["actorId"] + "/Views", role=role)["body"])
            need({row["Id"] for row in rows} == ({row["Id"] for row in wanted.values()} if role == "P" else set()),
                 "ordinary_library_isolation")
        movie_id = self.seed["catalog"]["movie"]["id"]
        movie = self.req("visible-movie", "GET", "/emby/Users/" + self.credentials["P"]["actorId"] + "/Items/" + movie_id, role="P")["body"]
        need(movie["Id"] == movie_id and movie["Type"] == "Movie" and movie["ServerId"] == self.server_id and
             movie["UserData"]["Played"] is False and movie["UserData"]["PlayCount"] == movie["UserData"]["PlaybackPositionTicks"] == 0,
             "scenario_initial_item_state")
        denied = self.req("hidden-movie", "GET", "/emby/Users/" + self.credentials["Q"]["actorId"] + "/Items/" + movie_id,
                          role="Q", expected=(404,))["body"]
        need(denied["ResponseStatus"]["ErrorCode"] == "not_found", "control_item_not_hidden")

    def observe_operation(self, kind, result):
        request_id = self.create_request if kind == "create" else self.plan_request
        prior = self.operations.get(kind)
        operation = validate_operation(result["body"]["Operation"], kind, request_id,
                                       prior["Id"] if prior else None, self.backup["Id"] if kind == "restore" and self.backup else None)
        need(operation["Id"] not in self.old_operations and
             (not prior or decimal(operation["Revision"]) >= decimal(prior["Revision"])), "operation_revision_or_existing_id")
        self.operations[kind] = operation
        self.save("operation-%s-%03d" % (kind, self.sequence), {"operation": operation, "response": result["receipt"]})
        return operation

    def admit_operation(self, kind, route, body):
        label = "admit-" + kind
        try:
            result = self.req(label, "POST", route, body=body, role="admin", expected=(202,))
        finally:
            last = self.io.last_response
            if last and last.get("label", "").endswith("-" + label) and last.get("status") == 202 and last.get("complete"):
                if last.get("body") is None:
                    last["body"] = parse(last["raw"])
                self.observe_operation(kind, last)
        need(kind in self.operations and result["receipt"] is not None, "operation_admission_unacknowledged")

    def poll_operation(self, kind, target, count, interval=10, cleanup=False):
        operation = self.operations[kind]
        for _ in range(count):
            if operation["State"] == target:
                return operation
            need(operation["State"] not in TERMINAL, "owned_operation_failed_or_interrupted")
            self.wait(interval)
            result = self.req("poll-" + kind, "GET", OPERATIONS + "/" + operation["Id"], role="admin", cleanup=cleanup)
            operation = self.observe_operation(kind, result)
        need(operation["State"] == target, "operation_poll_budget_exhausted")
        return operation

    def create_backup(self):
        self.phase = "create"
        phrase = secrets.token_urlsafe(32)
        self.passphrase = phrase
        self.evidence["backupSecret"] = self.save("backup-passphrase", {"requestId": self.create_request, "passphrase": phrase})
        self.admit_operation("create", BACKUPS, {"RequestId": self.create_request, "Passphrase": phrase})
        operation = self.poll_operation("create", "completed", 16)
        need(operation["Phase"] == "finished" and operation["ErrorCode"] == "" and
             operation["GenerationRevision"] == self.generation and HEX32.fullmatch(operation["BackupId"]) and
             operation["BackupId"] not in self.old_backups, "backup_creation_not_complete")
        self.backup = self.req("backup-detail", "GET", BACKUPS + "/" + operation["BackupId"], role="admin")["body"]["Backup"]
        size = validate_backup(self.backup, operation["BackupId"], self.server_id, self.before["tables"])
        route = BACKUPS + "/" + self.backup["Id"] + "/file"
        prefix = None
        for mode, method, expected, maximum, headers in (
                ("head", "HEAD", (200,), 65536, None),
                ("range", "GET", (206,), 64, {"Range": "bytes=0-63"}),
                ("full", "GET", (200,), size, None)):
            response = self.req("download-" + mode, method, route, role="admin", expected=expected, maximum=maximum, headers=headers)
            validate_download(response, self.backup, mode)
            if mode == "range":
                prefix = response["raw"]
            if mode == "full":
                need(response["raw"][:64] == prefix, "download_range_and_complete_body_differ")
                self.evidence["encryptedArchive"] = self.helper.write_once(self.io.private / (self.backup["Id"] + ".age"), response["raw"])
        status = validate_status(self.req("created-status", "GET", BACKUPS + "/status", role="admin")["body"], self.generation)
        need(status["Storage"]["Objects"] == self.status_before["Storage"]["Objects"] + 1 and
             int(status["Storage"]["Bytes"]) == int(self.status_before["Storage"]["Bytes"]) + size,
             "backup_storage_delta")

    def stage_restore(self):
        self.phase = "plan"
        self.admit_operation("restore", "/admin/v1/restores/plans", {"RequestId": self.plan_request,
            "BackupId": self.backup["Id"], "SHA256": self.backup["SHA256"], "Passphrase": self.passphrase,
            "RestoreDefaults": False, "ReplaceRollback": False, "GenerationRevision": self.generation})
        operation = self.poll_operation("restore", "ready", 20)
        validate_ready(operation, self.generation)
        self.staged = self.snapshot("inactive-ready", target=True)
        source_marker = json.loads(indexed(self.before["tables"]["server_settings"], "key")["goby.recovery.binding.v1"]["value"])
        target_marker = json.loads(indexed(self.staged["tables"]["server_settings"], "key")["goby.recovery.binding.v1"]["value"])
        need(target_marker["version"] == 1 and target_marker["deploymentId"] == source_marker["deploymentId"] and
             target_marker["slot"] == "recovery" and HEX32.fullmatch(target_marker["generationId"]) and
             target_marker["generationId"] != source_marker["generationId"] and
             self.staged["tables"]["users"] == self.before["tables"]["users"] and
             all(row["revoked_at"] is not None for row in self.staged["tables"]["sessions"]) and
             [row["version"] for row in self.staged["tables"]["schema_migrations"]] == list(range(1, 29)),
             "inactive_stage_identity_or_normalization")
        need(self.inspector.deployment_lease() == self.lease, "source_lease_changed_during_stage")

    def cancel_owned(self, kind="restore", cleanup=False):
        operation = self.operations[kind]
        result = self.req("cancel-readback", "GET", OPERATIONS + "/" + operation["Id"], role="admin", cleanup=cleanup)
        operation = self.observe_operation(kind, result)
        if operation["State"] in TERMINAL:
            return
        need(operation["CanCancel"] is True and kind not in self.cancel_attempts, "operation_not_cancellable_or_already_attempted")
        self.cancel_attempts.add(kind)
        self.req("cancel-" + kind, "POST", OPERATIONS + "/" + operation["Id"] + "/cancel",
                 body={"Revision": operation["Revision"]}, role="admin", expected=(202,), cleanup=cleanup)
        self.poll_operation(kind, "cancelled", 2 if cleanup else 4, interval=2, cleanup=cleanup)
        validate_cancelled(self.operations[kind])

    def cancel_ready(self):
        self.phase = "cancel"
        self.cancel_owned()
        status = validate_status(self.req("cancelled-status", "GET", BACKUPS + "/status", role="admin")["body"], self.generation, staged=True)
        need(status["Storage"]["Objects"] == self.status_before["Storage"]["Objects"] + 1 and
             int(status["Storage"]["Bytes"]) == int(self.status_before["Storage"]["Bytes"]) + int(self.backup["SizeBytes"]),
             "cancel_changed_backup_storage")
        retained = self.snapshot("inactive-cancelled", target=True)
        need(retained["tables"] == self.staged["tables"] and retained["sequences"] == self.staged["sequences"],
             "cancel_did_not_retain_exact_inactive_stage")

    def finish_window(self):
        while len(self.samples) < 11:
            self.wait(min(1, max(0.01, self.started + len(self.samples) * 60 - time.monotonic())))
        need(self.samples[-1]["elapsedMilliseconds"] - self.samples[0]["elapsedMilliseconds"] >= 600000,
             "stability_window_short")
        self.phase = "final"
        self.req("same-native-session", "GET", SESSION_ROUTE, role="admin")
        need(self.req("same-ordinary-session", "GET", CONFIGURATION, role="P")["raw"] == b"{}", "ordinary_session_changed")
        q = self.req("same-control-session", "GET", "/emby/Users/" + self.credentials["Q"]["actorId"] + "/Views", role="Q")["body"]
        need(self.helper.items(q) == [], "control_permissions_changed")
        backups = bounded_inventory(self.req("backup-list-after", "GET", BACKUPS, role="admin")["body"])
        operations = bounded_inventory(self.req("operation-list-after", "GET", OPERATIONS, role="admin")["body"])
        need(set(backups) == set(self.old_backups) | {self.backup["Id"]} and
             all(backups[key] == row for key, row in self.old_backups.items()) and
             set(operations) == set(self.old_operations) | {row["Id"] for row in self.operations.values()} and
             all(operations[key] == row for key, row in self.old_operations.items()), "unowned_recovery_inventory_change")
        for operation in self.operations.values():
            need(operations[operation["Id"]] == operation, "final_operation_projection_changed")
        overview = self.req("overview-after", "GET", "/admin/v1/overview", role="admin")["body"]
        need(overview["Server"] == self.overview["Server"] and overview["Database"] == self.overview["Database"] and
             overview["Transcoding"] == self.overview["Transcoding"] and overview["Counts"]["Users"] == 8 and
             overview["Counts"]["Libraries"] == 3 and overview["Counts"]["Items"] == self.overview["Counts"]["Items"],
             "final_runtime_overview_changed")

    def close_sessions(self):
        self.cleanup_end = min(time.monotonic() + 180, self.io.started + 900)
        for kind in reversed(list(self.operations)):
            if self.operations[kind]["State"] not in TERMINAL and kind not in self.cancel_attempts:
                try:
                    self.cancel_owned(kind, cleanup=True)
                except Exception as error:
                    self.failures.append({"stage": "emergency_cancel", "type": type(error).__name__})
        for role in reversed(list(self.logins)):
            login = self.logins[role]
            try:
                if "initialSession" not in login:
                    self.bind_login(role)
                native = login["auth"]["kind"] == "native"
                route, method = (SESSION_ROUTE, "DELETE") if native else ("/emby/Sessions/Logout", "POST")
                if role == "unexpected-native":
                    credential_id = login["initialSession"]["id"]
                    response = self.req("revoke-unexpected-native", "POST", "/admin/v1/sessions/" + credential_id + "/revoke",
                                        body={}, role="admin", expected=(200,), cleanup=True)
                    need(response["body"]["SessionId"] == credential_id and response["body"]["UserId"] == self.credentials[role]["actorId"] and
                         response["body"]["Kind"] == "admin" and response["body"]["CurrentSessionRevoked"] is False,
                         "unexpected_login_revocation_identity")
                else:
                    response = self.req("logout-" + role.lower(), method, route, role=role, expected=(204,), cleanup=True)
                denied = self.req("logout-proof-" + role.lower(), "GET", SESSION_ROUTE if native else "/emby/System/Info",
                                  role=role, expected=(401,), cleanup=True)
                if native:
                    need(denied["body"]["Error"]["Code"] == "invalid_credentials", "native_logout_token_still_usable")
                else:
                    need(denied["raw"] == b"Access token is invalid or expired.", "emby_logout_token_rejection_differs")
                login["logout401"] = True
                self.save("logout-" + role.lower(), {"credentialId": login["initialSession"]["id"],
                    "tokenSha256": sha(login["auth"]["token"].encode()), "logoutResponse": response["receipt"], "sameToken401": denied["receipt"]})
            except Exception as error:
                self.failures.append({"stage": "logout_" + role, "type": type(error).__name__})

    def run(self):
        self.preflight()
        self.fixtures_before = self.fixture_files("fixtures-before")
        self.authenticate()
        self.storage()
        self.create_backup()
        self.stage_restore()
        self.cancel_ready()
        self.finish_window()

    def reconcile(self):
        after = self.snapshot("source-after")
        need(self.before is not None, "source_baseline_unavailable")
        proof = reconcile_source(self.before, after, self.logins, self.operations["create"]["Id"], self.backup["Id"], self.operations["restore"]["Id"])
        need(self.fixture_files("fixtures-after") == self.fixtures_before, "candidate_fixture_closure_changed")
        need(self.inspector.deployment_lease() == self.lease and self.io.pin() == self.seed["processes"]["candidate"], "final_process_or_lease_changed")
        resources = self.resource_counters("resources-after")
        for filename, keys in (("memory.events", ("max", "oom", "oom_kill", "oom_group_kill")), ("pids.events", ("max",))):
            for key in keys:
                need(resources["events"][filename].get(key, "0") == self.counters_before["events"][filename].get(key, "0"),
                     "candidate_resource_limit_event")
        need(resources["unit"]["NRestarts"] == self.counters_before["unit"]["NRestarts"], "candidate_restarted")
        self.helper.read_checked(self.io.candidate["binary"]["path"], self.io.candidate["binary"]["sha256"])
        return proof


def main():
    run_started = time.monotonic()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True)
    parser.add_argument("--input-sha256", required=True)
    args = parser.parse_args()
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
         sys.flags.isolated and sys.flags.dont_write_bytecode, "remote_isolated_root_python_required")
    os.umask(0o077)
    value = validate_input(parse(initial_read(args.input, args.input_sha256)))
    input_pin = {"path": str(Path(args.input)), "sha256": args.input_sha256}
    helper_pin = value["seedHelper"]
    helper = import_bytes("frozen_admission_seed", helper_pin["path"], initial_read(helper_pin["path"], helper_pin["sha256"]))
    need(value["admissionHelper"]["path"] == str(Path(__file__).absolute()), "admission_source_path_differs")
    helper.read_checked(__file__, value["admissionHelper"]["sha256"])
    inspector_pin = value["inspectionHelper"]
    inspector_module = import_bytes("frozen_admission_inspection", inspector_pin["path"], helper.read_checked(inspector_pin["path"], inspector_pin["sha256"]))
    seed = parse(helper.read_checked(**value["seedManifest"]))
    catalog = parse(helper.read_checked(**value["compiledCatalog"]))
    source = parse(helper.read_checked(**value["sourceManifest"]))
    validate_catalog(catalog, source, value["compiledCatalog"])
    io = helper.CandidateIO(value, input_pin, value["admissionHelper"])
    io.started = run_started
    job = Admission(io, helper, inspector_module.CandidateInspection(), seed, catalog)
    def expired(_number, _frame):
        raise AdmissionError("absolute_admission_deadline")
    signal.signal(signal.SIGALRM, expired)
    signal.signal(signal.SIGTERM, expired)
    signal.signal(signal.SIGINT, expired)
    signal.setitimer(signal.ITIMER_REAL, max(0.001, 900 - (time.monotonic() - run_started)))
    failure, proof = None, None
    try:
        io.open()
        job.run()
    except Exception as error:
        failure = {"stage": job.phase, "type": type(error).__name__, "code": str(error) if isinstance(error, AdmissionError) else "admission_operation_failed"}
    finally:
        if io.created:
            job.close_sessions()
    if failure is None and not job.failures:
        try:
            proof = job.reconcile()
        except Exception as error:
            failure = {"stage": "reconciliation", "type": type(error).__name__, "code": str(error) if isinstance(error, AdmissionError) else "admission_reconciliation_failed"}
    successful = failure is None and not job.failures and proof is not None
    result = {"kind": "audited-candidate-live-admission", "version": 1, "status": "admitted_for_core_client" if successful else "admission_failed_resources_retained",
              "input": input_pin, "helper": value["admissionHelper"], "seed": value["seedManifest"], "candidateManifest": value["candidateManifest"],
              "failure": failure, "cleanupFailures": job.failures, "budgets": LIMITS, "requests": io.requests, "phaseRequests": dict(job.phase_counts),
              "elapsedMilliseconds": round((time.monotonic() - io.started) * 1000), "stabilitySamples": job.samples,
              "operations": job.operations, "backup": job.backup, "evidence": job.evidence, "preservation": proof,
              "requestResponsibilities": io.request_states, "controllerSessions": {key: {"credentialId": row.get("initialSession", {}).get("id"),
                  "tokenSha256": sha(row["auth"]["token"].encode()), "sameTokenRejected": row["logout401"]} for key, row in job.logins.items()},
              "candidateAdmissionComplete": successful, "clientAcceptance": False, "playbackRequests": 0, "applyRequests": 0, "rollbackRequests": 0,
              "inactiveStageRetained": successful, "activeGenerationChanged": False if successful else None}
    if io.created:
        summary = {"status": result["status"], "scope": str(io.output), "report": None,
                   "candidateAdmissionComplete": successful, "clientAcceptance": False, "requests": io.requests,
                   "elapsedMilliseconds": result["elapsedMilliseconds"], "failure": failure,
                   "cleanupFailures": job.failures, "inactiveStageRetained": successful, "reportUnavailable": None}
        try:
            summary["report"] = helper.write_json_once(io.private / "report.json", result)
            helper.write_json_once(io.output / "summary.json", summary)
        except Exception as error:
            successful = False
            summary.update(status="admission_report_unavailable_resources_retained", candidateAdmissionComplete=False,
                           reportUnavailable={"type": type(error).__name__, "code": "admission_report_publication_failed"})
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
        print(json.dumps(summary, sort_keys=True))
    else:
        signal.setitimer(signal.ITIMER_REAL, 0)
        print(json.dumps({"status": "admission_not_started", "failure": failure}, sort_keys=True))
    return 0 if successful else 2


if __name__ == "__main__":
    raise SystemExit(main())
