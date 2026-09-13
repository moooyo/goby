#!/usr/bin/env python3
"""Explicit runtime-epoch contracts and reusable metadata-only candidate readers."""
from __future__ import annotations
import hashlib
import io
import json
import os
from pathlib import Path
from pathlib import PurePosixPath
import re
import stat
import subprocess
import time
import types
import tarfile
from datetime import datetime, timezone, timedelta

C = Path("/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b")
R = Path("/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14")
F = Path("/opt/goby-test/audit-fixes-20260913-20260913T083107Z-fb6c70468fa3")
ARCHIVE = "b7120b6f323ace203fe7b49c56cd669ce5b9ff3ef8f3ceddb4359c2951aa658b"
PROVISION = {"path": str(C / "private/manifest.json"), "sha256": "022ca1e48aac6159750df72157dcddff3738e67962012f556825e26b0f0dfb09"}
SEED = {"path": str(R / "candidate-core-seed-reconciliation-02/private/manifest.json"), "sha256": "1c4e3b5b0005281b33eaaad3e5b7819f6a3bb93db8559bf91713d73ac1bf50e1"}
ADMISSION02 = {"path": str(R / "candidate-live-admission-02/private/report.json"), "sha256": "7df6fd2587697c4ef9e10e6f0adf7b11cea975523ddd2eb4f29cb0394398ded1"}
SEED_ADDENDUM = {"path": str(R / "seed-session-binding-verification-01/verification.json"), "sha256": "f17c3b5d8885d4002e8dc7d2f39961f85a40c578951d38f8661664efb9859dd4"}
OLD_BINARY = "a9b25b6b3e9f04b528ca77cd0a0dd548ae4c2715c06a1a56a6def23c6e00e2d7"
SEED_EXECUTOR = "365363509c9e9b71c2b5ae323ddc9c4a39605a5f6f8596a84a5693d933e1ced7"
GATEWAY_SOURCE_SHA = "8c8e962b8d55e1143972e720e55aa99a89dcb49610b729040bbc773b4bab0c2a"
LIMITS = {"maximumSeconds": 900, "stopSeconds": 60, "readySeconds": 60, "maximumPublicRequests": 10, "stopCalls": 1, "replaceCalls": 1, "startCalls": 1}
HELPERS = {"seed", "provision", "gateway", "admission", "reconcile"}
CATALOG_RELATIVE = "internal/backuppg/catalogs/schema-28-postgresql-17.json"
TABLES = set("activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users".split())
TREE_ROOTS = tuple(str(C / part) for part in ("data/recovery", "data/operations", "data/backups", "data/cache", "data/media", "install/admin"))
PROTECTED = ("goby-client-m3e.service", "goby-foundation-test.service", "postgresql@17-main.service", "goby-core-av-original-client-01.service")
INPUT_KEYS = {"kind", "version", "output", "originalProvision", "seedManifest", "seedSessionAddendum", "admission02", "newFullReport", "newSourceManifest", "newBinary", "compiledCatalog", "frontendReport", "helpers", "reviewedStateContract", "budgets"}
EPOCH_KEYS = {"kind", "version", "status", "transitionInput", "transitionHelper", "runtimeHelper", "originalProvision", "seedProvenance", "currentSource", "candidate", "candidateProcess", "postgresProcess", "lease", "before", "after", "preservation", "calls", "helpers", "candidateAdmissionComplete"}
BINDING_KEYS = {"kind", "version", "runtimeEpoch", "originalSeed", "seedExecutor", "seedInput", "seedSessionAddendum", "admission02", "serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources", "seedCleanup", "currentSessions", "candidateAdmissionComplete"}
PREVIOUS_BINARY_EPOCH = {"path": str(R / "candidate-cancellation-transition-01/private/runtime-epoch.json"), "sha256": "7bcdbc529fd1ba3f6a62f66585e6788cc9efa1aac22a4accc8d339d69ccf6ae2"}
ADMISSION03 = {"path": str(R / "candidate-live-admission-03/private/report.json"), "sha256": "a59638abe364b89e9c44482986698d0b1d310147178cbbe2ab4a0c1412c29f97"}
ADMISSION03_STATE = {"path": str(R / "candidate-admission03-closeout-01/private/state.json"), "sha256": "9c2a6660fda9f2da5c33df8c93fbbc4786c397d158755d1de3712eda6f5e01d0"}
ADMISSION03_CLOSEOUT = {"path": str(R / "candidate-admission03-failure-closeout.json"), "sha256": "090d04421c8fb847822695143483b153f096e2707571c14a01d4860d48233e2a"}
BACKUP_ADDITIONS = {"GOBY_BACKUP_MAX_OBJECT_BYTES": "67108864", "GOBY_BACKUP_MAX_TOTAL_BYTES": "268435456"}
ENV_LIMITS = {"maximumSeconds": 900, "stopSeconds": 60, "readySeconds": 60, "maximumPublicRequests": 10, "stopCalls": 1, "replaceEnvironmentCalls": 1, "startCalls": 1}
REQUIRED_BACKUP_FREE = 2 * (64 << 20) + (64 << 20) + (32 << 20) + (64 << 10)
ENV_INPUT_KEYS = {"kind", "version", "output", "previousEpoch", "previousSeedBinding", "admission03", "failureCloseout", "closedState", "runtimeHelper", "additions", "budgets"}
ENV_EPOCH_KEYS = EPOCH_KEYS | {"previousEpoch", "productInput", "operationKind", "configurationChange"}
ENV_BINDING_KEYS = BINDING_KEYS | {"previousBinding", "admission03", "failureCloseout", "closedState"}
CURRENT_ENV_EPOCH = {"path": str(R / "candidate-backup-limits-revision-01/private/runtime-epoch.json"), "sha256": "72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e"}
CURRENT_ENV_BINDING = {"path": str(R / "candidate-backup-limits-revision-01/private/seed-runtime-binding.json"), "sha256": "92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f"}
SUCCESSOR_STATE = {"path": str(R / "candidate-tv-parent-transition-state-review-01/private/state.json"), "sha256": "82cabde1a8d8dcf73a0e19da5b7c680fd977cace56d0d597bac0c59513b170a6"}
SUCCESSOR_SUMMARY = {"path": str(R / "candidate-tv-parent-transition-state-review-01/summary.json"), "sha256": "be2bd9a3081c9f84fb71ed617d2d3a025415ee2faf762d89e512e26bcd0dff18"}
SUCCESSOR_PRIOR_SOURCE = {"path": str(R / "candidate-core-client-tv-browse-01/private/source-after.json"), "sha256": "541dc489d1592612485aa4e18955cec3adac3cbb8070b1405d4af8f6818a92a6"}
SUCCESSOR_PRIOR_CLOSEOUT = {"path": str(R / "candidate-core-tv-browse01-owned-state-closeout.json"), "sha256": "b9cbc7ad57f7381c1c6c8d267827cb24a907376a17944fe5b3bd5d8dcdd3f4a0"}
ADMISSION04 = {"path": str(R / "candidate-live-admission-04/private/report.json"), "sha256": "05083c7cc5c65c62e30018136b6a7d383c144c9742d96eaecc52e9c2cfc19653"}
INACTIVE_STAGE = {"path": str(R / "candidate-live-admission-04/private/inactive-cancelled.json"), "sha256": "12dac8ef138874cfca6dd6803213854b716ec3a6639b0321954f68ee02e9eb73"}
NEW_F = Path("/opt/goby-test/audit-fixes-20260913-20260913T141732Z-a393812c3356")
NEW_ARCHIVE = "b363afdcf707471c3a95288d04441bb7be89699010b09ca89c4c783e10436177"
NEW_SOURCE_MANIFEST = {"path": str(NEW_F / "source-manifest.json"), "sha256": "bce4d22a4c51dacca4660a6c8e8e3fac816141cd612a7b32b87367799e495cff"}
CURRENT_BINARY = "477d26adced672371707fdf9bb2b0b5e54014487dd2c962d145506887420cd9f"
SUCCESSOR_INPUT_KEYS = {"kind", "version", "output", "previousEpoch", "previousBinding", "reviewedSummary", "reviewedState", "priorCloseout", "priorSource", "newFullReport", "newSourceManifest", "newBinary", "compiledCatalog", "frontendReport", "helpers", "budgets"}
SUCCESSOR_EPOCH_KEYS = EPOCH_KEYS | {"previousEpoch", "productInput", "configurationInput", "operationKind", "reviewedState", "reviewedSummary"}
SUCCESSOR_BINDING_KEYS = BINDING_KEYS | {"previousBinding", "reviewedSummary", "reviewedState", "priorCloseout", "priorSource"}


class ContractError(ValueError):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


def need(value, code):
    if not value:
        raise ContractError(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def descriptor(value):
    need(isinstance(value, dict) and set(value) == {"path", "sha256"} and isinstance(value["path"], str) and
         Path(value["path"]).is_absolute() and ".." not in Path(value["path"]).parts and re.fullmatch(r"[0-9a-f]{64}", value["sha256"] or ""), "descriptor_invalid")
    return value


def read_bootstrap(pin):
    descriptor(pin)
    path = Path(pin["path"])
    need(path.is_relative_to(R), "helper_authority_escaped")
    for node in (path, *path.parents):
        info = node.lstat()
        need(info.st_uid == 0 and not info.st_mode & 0o022 and not stat.S_ISLNK(info.st_mode), "helper_authority_unsafe")
    before = path.lstat()
    signature = lambda st: (st.st_dev, st.st_ino, st.st_mode, st.st_uid, st.st_gid, st.st_nlink, st.st_size, st.st_mtime_ns, st.st_ctime_ns)
    need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= 4 << 20, "helper_file_invalid")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        raw = stream.read((4 << 20) + 1)
        need(signature(os.fstat(stream.fileno())) == signature(before), "helper_changed")
    need(signature(path.lstat()) == signature(before) and digest(raw) == pin["sha256"], "helper_digest_changed")
    return raw


def load_helper(name, pin):
    module = types.ModuleType("transition_" + name)
    module.__file__ = pin["path"]
    exec(compile(read_bootstrap(pin), module.__file__, "exec"), module.__dict__)
    return module


def validate_transition_input(value):
    if type(value.get("version")) is int and value["version"] == 2:
        return validate_binary_successor_input(value)
    need(set(value) == INPUT_KEYS and value["kind"] == "audited-candidate-transition-input" and type(value["version"]) is int and value["version"] == 1, "transition_input_schema")
    for key in INPUT_KEYS - {"kind", "version", "output", "helpers", "budgets"}:
        descriptor(value[key])
    need(value["originalProvision"] == PROVISION and value["seedManifest"] == SEED and value["admission02"] == ADMISSION02 and value["seedSessionAddendum"] == SEED_ADDENDUM, "transition_provenance_changed")
    need(set(value["helpers"]) == HELPERS and all(descriptor(pin) for pin in value["helpers"].values()) and
         value["helpers"]["gateway"]["sha256"] == GATEWAY_SOURCE_SHA, "transition_helpers_invalid")
    output = Path(value["output"])
    need(output.parent == R and re.fullmatch(r"candidate-cancellation-transition-[0-9]{2}", output.name) and value["budgets"] == LIMITS and
         all(type(number) is int for number in value["budgets"].values()), "transition_scope_or_budget_invalid")
    need(value["newFullReport"]["path"] == str(F / "report.json") and value["newSourceManifest"]["path"] == str(F / "source-manifest.json") and
         value["newBinary"]["path"] == str(F / "bin/goby-linux-amd64") and value["compiledCatalog"]["path"] == str(F / "source" / CATALOG_RELATIVE), "transition_product_scope_invalid")
    return value


def validate_binary_successor_input(value):
    need(set(value) == SUCCESSOR_INPUT_KEYS and value["kind"] == "audited-candidate-transition-input" and type(value["version"]) is int and value["version"] == 2, "binary_successor_input_schema")
    for key in SUCCESSOR_INPUT_KEYS - {"kind", "version", "output", "helpers", "budgets"}:
        descriptor(value[key])
    expected = {"previousEpoch": CURRENT_ENV_EPOCH, "previousBinding": CURRENT_ENV_BINDING, "reviewedState": SUCCESSOR_STATE,
                "reviewedSummary": SUCCESSOR_SUMMARY, "priorSource": SUCCESSOR_PRIOR_SOURCE, "priorCloseout": SUCCESSOR_PRIOR_CLOSEOUT,
                "newSourceManifest": NEW_SOURCE_MANIFEST}
    need(all(value[key] == pin for key, pin in expected.items()), "binary_successor_authority_changed")
    need(set(value["helpers"]) == HELPERS and all(descriptor(pin) for pin in value["helpers"].values()) and
         value["helpers"]["gateway"]["sha256"] == GATEWAY_SOURCE_SHA, "binary_successor_helpers_invalid")
    output = Path(value["output"])
    need(output.parent == R and re.fullmatch(r"candidate-tv-parent-transition-[0-9]{2}", output.name) and value["budgets"] == LIMITS and
         all(type(number) is int for number in value["budgets"].values()), "binary_successor_scope_or_budget_invalid")
    need(value["newFullReport"]["path"] == str(NEW_F / "report.json") and value["newBinary"]["path"] == str(NEW_F / "bin/goby-linux-amd64") and
         value["compiledCatalog"]["path"] == str(NEW_F / "source" / CATALOG_RELATIVE) and value["newBinary"]["sha256"] not in (OLD_BINARY, CURRENT_BINARY), "binary_successor_product_scope_invalid")
    return value


def validate_successor_full_report(report, worker, value):
    need(report["status"] == worker["status"] == "passed" and report["mode"] == worker["mode"] == "full" and report["archive_sha256"] == NEW_ARCHIVE and
         report["unit_exit_code"] == 0 and report["recursive_cgroup_empty"] is True and report["existing_services_modified"] is False and report["worker"] == worker,
         "full_product_verification_not_complete")
    cleanup = {"only_worker_process_remains", "owned_postgres_stopped", "private_bind_removed", "source_unchanged"}
    need(set(worker["cleanup"]) == cleanup and all(worker["cleanup"][key] is True for key in cleanup), "full_product_cleanup_incomplete")
    expected, actual = worker["expected_packages"], worker["packages"]
    need(len(expected) == len(set(expected)) == len(actual) == 25 and {row["package"] for row in actual} == set(expected) and
         all(row["result"] == "pass" and row["exit_code"] == row["failed"] == row["skipped"] == 0 for row in actual) and
         worker["test_counts"]["failed"] == worker["test_counts"]["skipped"] == 0 and worker["test_counts"]["passed"] > 0, "full_package_coverage_incomplete")
    need(report["scope"] == worker["scope"] == str(NEW_F) and worker["binary"]["path"] == "bin/goby-linux-amd64" and
         worker["binary"]["sha256"] == value["newBinary"]["sha256"] and worker["binary"]["sha256"] not in (OLD_BINARY, CURRENT_BINARY) and
         type(worker["binary"]["bytes"]) is int and worker["binary"]["bytes"] > 0, "new_binary_not_verified")


def successor_sessions(source):
    """Publish only normalized, deterministically ordered revoked credentials."""
    rows = source["tables"]["sessions"]
    need(len(rows) == 15, "successor_session_count")
    result = []
    for row in rows:
        need(isinstance(row, dict) and {"kind", "id", "user_id", "token_hash", "revoked_at"} <= set(row) and
             all(isinstance(row[key], str) for key in ("kind", "id", "user_id", "token_hash", "revoked_at")), "successor_revoked_session_type")
        need(row["kind"] in ("admin", "emby") and re.fullmatch(r"[0-9a-f]{32}", row["id"]) and re.fullmatch(r"[0-9a-f]{32}", row["user_id"]) and
             re.fullmatch(r"\\x[0-9a-f]{64}", row["token_hash"]) and isinstance(row["revoked_at"], str), "successor_revoked_session_invalid")
        need(datetime.fromisoformat(row["revoked_at"].replace("Z", "+00:00")).tzinfo is not None, "successor_revoked_time_invalid")
        result.append({"kind": row["kind"], "credentialId": row["id"], "tokenSha256": row["token_hash"][2:], "userId": row["user_id"], "revokedAt": row["revoked_at"]})
    need(len({row["credentialId"] for row in result}) == len({row["tokenSha256"] for row in result}) == 15, "successor_session_collision")
    return sorted(result, key=lambda row: row["credentialId"])


def validate_successor_state(state, *, now=None):
    """Check the occupied, nonempty startup state without performing IO."""
    now = now or datetime.now(timezone.utc)
    need(now.tzinfo is not None and [state[key] for key in ("runtimeEpoch", "previousEpoch") if key in state] == [CURRENT_ENV_EPOCH] and
         state["seedBinding"] == CURRENT_ENV_BINDING and state["priorSource"] == SUCCESSOR_PRIOR_SOURCE and state["admission04"] == ADMISSION04, "successor_state_authority_changed")
    tables = state["source"]["tables"]
    need(set(tables) == set(state["inactiveStage"]["tables"]) == TABLES and len(tables["users"]) == 8 and len(tables["items"]) == 13 and
         [row["version"] for row in tables["schema_migrations"]] == [row["version"] for row in state["inactiveStage"]["tables"]["schema_migrations"]] == list(range(1, 29)), "successor_source_inventory_changed")
    sessions = successor_sessions(state["source"])
    need(len(tables["play_sessions"]) == 7 and len(tables["user_item_data"]) == 2 and sum(row["counted"] is True for row in tables["play_sessions"]) == 2,
         "successor_play_history_changed")
    prepared = [row for row in tables["play_sessions"] if row["state"] == "Prepared"]
    need(len(prepared) == 2 and all(row["counted"] is False and row["started_at"] is None and row["stopped_at"] is None for row in prepared), "successor_prepared_history_changed")
    need(all(not tables[key] for key in ("client_playback_references", "encoding_jobs", "task_triggers", "task_runs", "task_run_children", "task_run_requests", "task_occurrences")) and
         len(tables["scan_jobs"]) == 3 and all(row["status"] == "Completed" for row in tables["scan_jobs"]), "successor_pending_startup_work")
    definitions = tables["task_definitions"]
    need(len(definitions) == 1 and all(definitions[0][key] == val for key, val in {"key": "library.scan", "emby_key": "RefreshLibrary", "name": "Scan media library",
         "description": "Scan all registered media libraries.", "category": "Library"}.items()), "successor_task_definition_changed")
    need(len(tables["activity_entries"]) == 58 and all(now + timedelta(seconds=LIMITS["maximumSeconds"]) < datetime.fromisoformat(row["created_at"].replace("Z", "+00:00")) + timedelta(days=1)
         for row in tables["activity_entries"]), "successor_activity_retention_deadline")
    need(set(state["trees"]) == set(TREE_ROOTS) and not any(".next-" in name or ".goby-backup-catalog.pending" in name for tree in state["trees"].values() for name in tree), "successor_pending_control_file")
    need(set(state["trees"][str(C / "data/cache")]) == {".", ".goby-transcode-cache", ".goby-transcode-lock"} and
         sum(not row.get("directory") for row in state["trees"][str(C / "data/media")].values()) == 14 and
         sum(not row.get("directory") for row in state["trees"][str(C / "install/admin")].values()) == 57, "successor_media_cache_asset_inventory")
    documents = state["controlDocuments"]
    need("recovery/activation-journal.json" not in documents, "successor_pending_activation")
    marker = json.loads(next(row["value"] for row in tables["server_settings"] if row["key"] == "goby.recovery.binding.v1"))
    payload = documents["operations/current.json"]["payload"]
    operations = payload["operations"]
    need(not payload.get("transition") and len(operations) == 3 and {(row["kind"], row["state"], row["phase"]) for row in operations} ==
         {("create", "failed", "finished"), ("create", "completed", "finished"), ("restore", "cancelled", "finished")} and
         all(row["authorized"] is True and row["applyAuthorized"] is False and row["cancelAuthorized"] is (row["kind"] == "restore") for row in operations), "successor_operation_startup_work")
    source_state = operations[0]["sourceState"]
    need(all(row["sourceState"] == source_state for row in operations) and marker["deploymentId"] == source_state["deploymentId"] and
         marker["slot"] == source_state["databaseSlot"] == "primary" and not marker.get("generationId") and source_state["revision"] == 0, "successor_control_source_binding")
    entries = documents["backups/.goby-backup-catalog.json"]["entries"]
    need(len(entries) == 2 and {(row["metadata"]["state"], row["phase"], row["metadata"]["size"]) for row in entries} == {("failed", "empty", 0), ("ready", "ready", 290550)} and
         all(row["deleting"] is False for row in entries), "successor_backup_startup_work")
    generations = documents["recovery/generation-registry.json"]["generations"]
    slots = {row["slot"]: row for row in payload["slots"]}
    restore = next(row for row in operations if row["kind"] == "restore")
    need(len(generations) == 1 and generations[0]["complete"] is True and set(slots) == {"primary", "recovery"} and
         slots["primary"]["state"] == "active" and slots["recovery"]["state"] == "staged" and
         slots["recovery"]["imageId"] == restore["generationId"] == generations[0]["id"] and slots["recovery"]["operation"] == restore["id"], "successor_inactive_generation_binding")
    registry = state["diagnostics"]["registry"]
    need(len(registry["files"]) <= 14 and sum(row["closed"] is False for row in registry["files"]) == 1 and all(not row.get("deleting") and
         now + timedelta(seconds=LIMITS["maximumSeconds"]) < datetime.fromisoformat(row["created"].replace("Z", "+00:00")) + timedelta(days=7) for row in registry["files"]), "successor_diagnostic_retention_deadline")
    need(canonical(state["candidateBefore"]) == canonical(state["candidateAfter"]) and canonical(state["postgresBefore"]) == canonical(state["postgresAfter"]) and
         canonical(state["leaseBefore"]) == canonical(state["leaseAfter"]) and canonical(state["hostingBefore"]) == canonical(state["hostingAfter"]), "successor_capture_identity_drift")
    return {"ownedTables": 35, "revokedSessions": sessions, "playRows": 7, "userDataRows": 2, "preparedRows": 2, "inactiveStageRetained": True}


def compare_successor_preservation(before, after, *, installed_binary=None, now=None):
    validate_successor_state(before, now=now)
    validate_successor_state(after, now=now)
    for section in ("source", "inactiveStage"):
        need(canonical(before[section]["tables"]) == canonical(after[section]["tables"]) and canonical(before[section]["sequences"]) == canonical(after[section]["sequences"]), "successor_logical_state_changed_" + section)
    for section in ("databases", "trees", "controlDocuments", "loadedUnits", "protected", "postgresBefore", "postgresAfter", "hostingBefore", "hostingAfter"):
        need(canonical(before[section]) == canonical(after[section]), "successor_preservation_changed_" + section)
    old_files, new_files = before["fixedFiles"], after["fixedFiles"]
    need(set(old_files) == set(new_files) and str(C / "data/master.key") in old_files, "successor_fixed_file_inventory")
    binary_path = str(C / "install/goby")
    for name in old_files:
        if installed_binary is None or name != binary_path:
            need(canonical(old_files[name]) == canonical(new_files[name]), "successor_fixed_file_changed")
    if installed_binary is not None:
        descriptor(installed_binary)
        need(installed_binary["path"] == binary_path and new_files[binary_path]["sha256"] == installed_binary["sha256"] and
             all(new_files[binary_path][key] == old_files[binary_path][key] for key in ("dev", "uid", "gid", "mode")), "successor_installed_binary_authority")
    else:
        for key in ("candidateBefore", "candidateAfter", "leaseBefore", "leaseAfter"):
            need(canonical(before[key]) == canonical(after[key]), "successor_before_identity_changed")
    def append(old, new):
        need(all(old[key] == new[key] for key in ("dev", "ino", "uid", "gid", "mode")) and new["bytes"] >= old["bytes"] and
             (new["prefixSha256"] == old["sha256"] if "prefixSha256" in new else
              new["bytes"] == old["bytes"] and new["sha256"] == old["sha256"]), "successor_log_prefix_changed")
    need(set(before["unitLogs"]) == set(after["unitLogs"]) == {"server-unit.log", "postgres-unit.log"}, "successor_unit_log_inventory")
    for name, facts in before["unitLogs"].items():
        append(facts, after["unitLogs"][name])
    if installed_binary is None:
        old, new = before["diagnostics"], after["diagnostics"]
        need(canonical(old["registry"]) == canonical(new["registry"]) and canonical(old["registryFile"]) == canonical(new["registryFile"]) and
             old["lock"] == new["lock"] and old["directory"] == new["directory"] and set(old["files"]) == set(new["files"]), "successor_before_diagnostics_changed")
        for name, facts in old["files"].items():
            append(facts, new["files"][name])
    else:
        compare_diagnostics(before["diagnostics"], after["diagnostics"])
    return {"ownedTablesExact": 35, "sequencesExact": True, "inactiveStageExact": True, "allPriorPlayAndUserDataExact": True,
            "controlMediaAssetsExact": True, "configurationExact": True, "postgresContinuous": True, "hostingContinuous": True}


def validate_full_report(report, worker, value):
    need(report["status"] == worker["status"] == "passed" and report["mode"] == worker["mode"] == "full" and report["archive_sha256"] == ARCHIVE and
         report["unit_exit_code"] == 0 and report["recursive_cgroup_empty"] is True and report["existing_services_modified"] is False and report["worker"] == worker,
         "full_product_verification_not_complete")
    required = {"only_worker_process_remains", "owned_postgres_stopped", "private_bind_removed", "source_unchanged"}
    need(set(worker["cleanup"]) == required and all(worker["cleanup"][key] is True for key in required), "full_product_cleanup_incomplete")
    expected, actual = worker["expected_packages"], worker["packages"]
    need(len(expected) >= 25 and len(expected) == len(set(expected)) == len(actual) and {row["package"] for row in actual} == set(expected) and
         all(row["result"] == "pass" and row["exit_code"] == row["failed"] == row["skipped"] == 0 for row in actual) and worker["test_counts"]["failed"] == worker["test_counts"]["skipped"] == 0 and worker["test_counts"]["passed"] > 0,
         "full_package_coverage_incomplete")
    need(report["scope"] == worker["scope"] == str(F) and worker["binary"]["path"] == "bin/goby-linux-amd64" and
         worker["binary"]["sha256"] == value["newBinary"]["sha256"] and worker["binary"]["sha256"] != OLD_BINARY and worker["binary"]["bytes"] > 0, "new_binary_not_verified")


def archive_manifest(raw):
    """Derive file facts from the pinned archive without extraction or execution."""
    result, total, members = {}, 0, 0
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:*") as archive:
        for member in archive:
            members += 1
            path = PurePosixPath(member.name)
            need(members <= 15000 and not path.is_absolute() and ".." not in path.parts and "\\" not in member.name and
                 not any(char in member.name for char in "\r\n\x00"), "source_archive_member_invalid")
            if member.isdir():
                continue
            name = str(path)
            need(member.isfile() and name not in result and name not in ("", ".") and 0 <= member.size <= 256 << 20, "source_archive_member_type_or_duplicate")
            total += member.size
            need(total <= 512 << 20 and len(result) < 10000, "source_archive_expansion_bound")
            source = archive.extractfile(member)
            checksum, received = hashlib.sha256(), 0
            for chunk in iter(lambda: source.read(1 << 20), b""):
                received += len(chunk)
                need(received <= member.size, "source_archive_member_size")
                checksum.update(chunk)
            source.close()
            need(received == member.size, "source_archive_member_truncated")
            result[name] = {"sha256": checksum.hexdigest(), "bytes": received}
    return result


def current_session_contract(seed, admission):
    need(seed["helper"]["sha256"] == SEED_EXECUTOR and admission["kind"] == "audited-candidate-live-admission" and admission["status"] == "admission_failed_resources_retained" and
         admission["seed"] == SEED and admission["candidateManifest"] == PROVISION and not admission["cleanupFailures"] and admission["operations"] == {} and admission["backup"] is None and
         admission["requests"] == {"normal": 5, "cleanup": 2} and set(admission["controllerSessions"]) == {"admin"}, "consumed_admission_evidence_differs")
    need(len(seed["cleanup"]) == 2 and {row["kind"] for row in seed["cleanup"]} == {"native", "emby"} and
         all(row["logoutAcknowledged"] and row["sameTokenRejected"] for row in seed["cleanup"]), "seed_cleanup_incomplete")
    rows = [{"kind": "admin" if row["kind"] == "native" else "emby", "tokenSha256": row["tokenSha256"], "credentialId": None} for row in seed["cleanup"]]
    extra = admission["controllerSessions"]["admin"]
    need(extra["sameTokenRejected"] is True and re.fullmatch(r"[0-9a-f]{32}", extra["credentialId"]), "admission_admin_cleanup_incomplete")
    rows.append({"kind": "admin", "tokenSha256": extra["tokenSha256"], "credentialId": extra["credentialId"]})
    need(len({row["tokenSha256"] for row in rows}) == 3, "session_provenance_collision")
    return rows


def validate_seeded_state(snapshot, seed, details, admission, catalog, admission_helper):
    """Validate all current stored representations; this function performs no IO."""
    tables = snapshot["tables"]
    need(set(tables) == TABLES and len(catalog["catalog"]["Tables"]) == 35 and {row["Name"] for row in catalog["catalog"]["Tables"]} == TABLES, "owned_table_inventory_changed")
    need([row["version"] for row in tables["schema_migrations"]] == list(range(1, 29)) and len(tables["users"]) == 8 and
         {row["id"] for row in tables["users"]} == set(seed["resources"]["users"]), "seeded_schema_or_users_changed")
    actors = {"admin": seed["admin"], **seed["actors"], "Q": seed["controlQ"]}
    users = {row["id"]: row for row in tables["users"]}
    for role, actor in actors.items():
        row = users[actor["id"]]
        need(row["name"] == actor["username"] and row["is_disabled"] is False and row["is_administrator"] is (role == "admin"), "seeded_actor_changed")
        admission_helper.validate_actor_policy("Q" if role == "Q" else "admin" if role == "admin" else "P", row)
    libraries = seed["libraries"]
    need(len(tables["libraries"]) == len(tables["library_roots"]) == 3, "seeded_roots_changed")
    for library in libraries.values():
        matched = [row for row in tables["libraries"] if row["id"] == library["Id"]]
        need(len(matched) == 1 and matched[0]["name"] == library["Name"] and matched[0]["collection_type"] == library["CollectionType"] and
             [row["path"] for row in tables["library_roots"] if row["library_id"] == library["Id"]] == library["Paths"], "seeded_library_mapping_changed")
    stored = {row["id"]: row for row in tables["items"]}
    library_ids = {row["Id"] for row in libraries.values()}
    need(len(tables["items"]) == len(stored) == 13 and len(details) == 10 and set(stored) == {row["Id"] for row in details} | library_ids, "seeded_item_membership_changed")
    for library in libraries.values():
        root = stored[library["Id"]]
        need(root["library_id"] == root["id"] and root["type"] == "CollectionFolder" and root["name"] == library["Name"] and root["parent_id"] is None and root["path"] == "" and root["is_folder"] is True, "stored_collection_root_changed")
    for dto in details:
        admission_helper.validate_actual_dto(dto, stored[dto["Id"]], seed)
    jobs = tables["scan_jobs"]
    need(len(jobs) == 3 and {(row["id"], row["library_id"]) for row in jobs} == {(row["id"], row["libraryId"]) for row in seed["resources"]["jobs"]} and
         all(row["status"] == "Completed" and row["error"] == "" for row in jobs), "seeded_scan_state_changed")
    need(all(not tables[name] for name in ("user_item_data", "play_sessions", "client_playback_references", "encoding_jobs", "task_runs", "task_run_children", "task_occurrences", "task_run_requests", "task_triggers")), "transition_requires_idle_unscheduled_state")
    definitions = tables["task_definitions"]
    selected = [row for row in definitions if row["key"] == "library.scan"]
    need(len(selected) == 1 and all(row["key"] == "library.scan" or row["enabled"] is False for row in definitions) and
         all(selected[0][key] == expected for key, expected in {"emby_key": "RefreshLibrary", "name": "Scan media library", "description": "Scan all registered media libraries.", "category": "Library"}.items()), "startup_task_reconciliation_would_change_state")
    settings = {row["key"]: row["value"] for row in tables["server_settings"]}
    need(settings["server_id"] == seed["serverId"] and "goby.recovery.binding.v1" in settings, "existing_server_or_recovery_binding_missing")
    wanted = current_session_contract(seed, admission)
    sessions = tables["sessions"]
    need(len(sessions) == 3 and all(row["user_id"] == seed["admin"]["id"] and row["revoked_at"] is not None for row in sessions) and
         {(row["kind"], row["token_hash"]) for row in sessions} == {(row["kind"], "\\x" + row["tokenSha256"]) for row in wanted}, "current_revoked_session_binding_changed")
    need(any(row["id"] == wanted[-1]["credentialId"] and row["token_hash"] == "\\x" + wanted[-1]["tokenSha256"] for row in sessions), "admission02_session_identity_changed")
    return {"ownedTables": 35, "users": 8, "libraries": 3, "storedItems": 13, "publicItems": 10, "revokedSessions": wanted, "historyRows": 0}


def compare_preservation(before, after):
    need(set(before["source"]["tables"]) == set(after["source"]["tables"]) == TABLES and set(before["trees"]) == set(after["trees"]) == set(TREE_ROOTS) and
         set(before["protected"]) == set(after["protected"]) == set(PROTECTED) and str(C / "data/master.key") in before["fixedFiles"] and str(C / "data/master.key") in after["fixedFiles"], "preservation_scope_incomplete")
    need(canonical(before["source"]["tables"]) == canonical(after["source"]["tables"]) and canonical(before["source"]["sequences"]) == canonical(after["source"]["sequences"]), "owned_logical_data_changed")
    for name in ("recovery", "trees", "fixedFiles", "protected", "postgresProcess", "generation"):
        need(canonical(before[name]) == canonical(after[name]), "preservation_changed_" + name)
    return {"ownedTablesExact": 35, "sequencesExact": True, "recoveryExact": True, "controlMediaAssetsExact": True, "protectedUnitsExact": True, "postgresContinuous": True}


def validate_control_documents(documents):
    """Validate the idle, previously initialized representations defined by source."""
    marker = documents["recovery/.goby-lifecycle.json"]
    need(marker["version"] == 1 and re.fullmatch(r"[0-9a-f]{32}", marker["deploymentId"]), "lifecycle_marker_missing_or_invalid")
    need("recovery/activation-journal.json" not in documents and "backups/.goby-backup-catalog.pending" not in documents, "pending_control_work")
    if "recovery/active-generation.json" in documents:
        active = documents["recovery/active-generation.json"]
        need(active["deploymentId"] == marker["deploymentId"] and active["databaseSlot"] == "primary" and active["master"] == "default" and not active["generationId"] and active["revision"] == 0, "unexpected_active_generation")
    need("recovery/generation-registry.json" in documents and documents["recovery/generation-registry.json"]["generations"] in (None, []), "generation_registry_missing_or_nonempty")
    record = documents["operations/current.json"]
    need(record["deploymentId"] == marker["deploymentId"], "recovery_control_deployment_changed")
    payload = record["payload"]
    need(isinstance(payload, dict) and record["revision"] > 0 and payload["deploymentId"] == marker["deploymentId"] and payload["operations"] == [] and not payload.get("transition") and
         len(payload["slots"]) == 2 and {(row["slot"], row["state"]) for row in payload["slots"]} == {("primary", "active"), ("recovery", "unclaimed")}, "recovery_control_not_initialized_and_idle")
    need("backups/.goby-backup-catalog.json" in documents and documents["backups/.goby-backup-catalog.json"]["entries"] in (None, []), "backup_catalog_missing_or_nonempty")
    return {"deploymentId": marker["deploymentId"], "controlRevision": record["revision"]}


def compare_diagnostics(before, after):
    old, new = before["registry"], after["registry"]
    need(old["version"] == new["version"] == 1 and old["token"] == new["token"] and re.fullmatch(r"[0-9a-f]{32}", old["token"]), "diagnostic_registry_authority_changed")
    need(all(before["registryFile"][key] == after["registryFile"][key] for key in ("uid", "gid", "mode")), "diagnostic_registry_owner_or_mode_changed")
    previous, current = {row["name"]: row for row in old["files"]}, {row["name"]: row for row in new["files"]}
    need(len(previous) == len(old["files"]) and len(current) == len(new["files"]) and len(set(current) - set(previous)) == 1 and set(previous) <= set(current), "diagnostic_file_membership_changed")
    need(before["lock"] == after["lock"] and before["directory"] == after["directory"], "diagnostic_lock_or_directory_changed")
    for name, entry in previous.items():
        observed = current[name]
        need(not entry.get("deleting") and not observed.get("deleting") and observed["closed"] is True and
             {key: val for key, val in observed.items() if key not in ("size", "closed")} == {key: val for key, val in entry.items() if key not in ("size", "closed")}, "diagnostic_old_entry_changed")
        left, right = before["files"][name], after["files"][name]
        need(right["prefixSha256"] == left["sha256"] and all(left[key] == right[key] for key in ("dev", "ino", "uid", "gid", "mode")) and
             0 <= right["bytes"] - left["bytes"] <= 1 << 20 and observed["size"] == right["bytes"], "diagnostic_prefix_or_identity_changed")
        if entry["closed"]:
            need(left["sha256"] == right["sha256"], "closed_diagnostic_log_changed")
    name = next(iter(set(current) - set(previous)))
    need(re.fullmatch(r"goby-" + old["token"] + r"-[0-9a-f]{32}\.jsonl", name) and current[name]["closed"] is False and not current[name].get("deleting") and
         after["files"][name]["bytes"] <= 1 << 20 and current[name]["identity"] == {"device": after["files"][name]["dev"], "inode": after["files"][name]["ino"]} and
         all(after["files"][name][key] == next(iter(before["files"].values()))[key] for key in ("uid", "gid", "mode")), "new_diagnostic_log_invalid")
    return {"oldLogPrefixesPreserved": len(previous), "newActiveLogs": 1, "prunedLogs": 0}


def validate_reviewed_contract(value):
    need(set(value) == {"kind", "version", "sourceSnapshot", "controlDocuments", "diagnosticsSnapshot", "preserved", "loadedUnits", "diagnosticPolicy"} and value["kind"] == "audited-candidate-reviewed-state-contract" and value["version"] == 1, "reviewed_state_contract_schema")
    for key in ("sourceSnapshot", "controlDocuments", "diagnosticsSnapshot"):
        descriptor(value[key])
    need(set(value["preserved"]) == {"recovery", "trees", "fixedFiles", "protected", "postgresProcess", "generation"} and
         set(value["preserved"]["trees"]) == set(TREE_ROOTS), "reviewed_preservation_scope_invalid")
    need(value["diagnosticPolicy"] == {"maximumFilesBefore": 14, "retentionDays": 7, "maximumLogBytes": 4 << 20, "maximumAppendBytes": 1 << 20, "pruningAllowed": False}, "reviewed_diagnostic_policy_invalid")
    return value


def validate_epoch(epoch):
    version = epoch["version"]
    if type(version) is int and version == 3:
        return validate_binary_successor_epoch(epoch)
    need(type(version) is int and version in (1, 2) and set(epoch) == (EPOCH_KEYS if version == 1 else ENV_EPOCH_KEYS) and epoch["kind"] == "audited-candidate-runtime-epoch" and epoch["status"] == "running_awaiting_live_acceptance" and
         epoch["originalProvision"] == PROVISION and epoch["seedProvenance"] == SEED and epoch["candidateAdmissionComplete"] is False, "runtime_epoch_schema")
    for key in ("transitionInput", "transitionHelper", "runtimeHelper", "before", "after"):
        descriptor(epoch[key])
    source = epoch["currentSource"]
    need(set(source) == {"archiveSha256", "sourceManifest", "binary", "fullReport", "schema"} and source["archiveSha256"] == ARCHIVE and source["schema"] == 28 and source["binary"]["sha256"] != OLD_BINARY, "runtime_epoch_source")
    for key in ("sourceManifest", "binary", "fullReport"):
        descriptor(source[key])
    candidate = epoch["candidate"]
    need(candidate["bootstrapExecuted"] is True and candidate["sourceState"] == {"users": 8, "schema": 28, "migrations": 28} and candidate["binary"] == source["binary"] and
         candidate["originalProvision"] == PROVISION and candidate["seedProvenance"] == SEED and candidate["currentSourceManifest"] == source["sourceManifest"], "epoch_must_not_impersonate_empty_provision")
    if version == 1:
        need(epoch["calls"] == {"stop": 1, "replace": 1, "start": 1}, "epoch_call_count")
    else:
        need(epoch["operationKind"] == "environment_revision" and epoch["previousEpoch"] == PREVIOUS_BINARY_EPOCH and
             epoch["calls"] == {"stop": 1, "replaceEnvironment": 1, "start": 1}, "environment_epoch_operation_invalid")
        descriptor(epoch["productInput"])
        change = epoch["configurationChange"]
        need(set(change) == {"before", "after", "preservedCopy", "additions", "requiredFreeBytes", "observedFreeBytesBefore"} and
             change["additions"] == BACKUP_ADDITIONS and change["after"] == candidate["runtime"] and change["requiredFreeBytes"] == REQUIRED_BACKUP_FREE and
             type(change["observedFreeBytesBefore"]) is int and change["observedFreeBytesBefore"] >= REQUIRED_BACKUP_FREE and
             candidate["input"] == epoch["transitionInput"] and candidate["productInput"] == epoch["productInput"], "environment_epoch_configuration_invalid")
        for key in ("before", "after", "preservedCopy"):
            descriptor(change[key])
        need(change["before"]["path"] == change["after"]["path"] == str(C / "private/runtime.env") and change["preservedCopy"]["sha256"] == change["before"]["sha256"], "environment_epoch_preserved_copy_invalid")
    return epoch


def validate_binary_successor_epoch(epoch):
    need(set(epoch) == SUCCESSOR_EPOCH_KEYS and epoch["kind"] == "audited-candidate-runtime-epoch" and type(epoch["version"]) is int and epoch["version"] == 3 and
         epoch["status"] == "running_awaiting_live_acceptance" and epoch["operationKind"] == "binary_successor" and
         epoch["previousEpoch"] == CURRENT_ENV_EPOCH and epoch["reviewedState"] == SUCCESSOR_STATE and epoch["reviewedSummary"] == SUCCESSOR_SUMMARY and
         epoch["originalProvision"] == PROVISION and epoch["seedProvenance"] == SEED and epoch["candidateAdmissionComplete"] is False, "binary_successor_epoch_schema")
    for key in ("transitionInput", "transitionHelper", "runtimeHelper", "before", "after", "productInput", "configurationInput", "preservation"):
        descriptor(epoch[key])
    need(epoch["productInput"] == epoch["transitionInput"] and epoch["calls"] == {"stop": 1, "replace": 1, "start": 1}, "binary_successor_epoch_operation")
    source = epoch["currentSource"]
    need(set(source) == {"archiveSha256", "sourceManifest", "binary", "fullReport", "schema"} and source["archiveSha256"] == NEW_ARCHIVE and
         source["sourceManifest"] == NEW_SOURCE_MANIFEST and source["schema"] == 28, "binary_successor_epoch_source")
    for key in ("sourceManifest", "binary", "fullReport"):
        descriptor(source[key])
    need(source["binary"]["path"] == str(C / "install/goby") and source["binary"]["sha256"] not in (OLD_BINARY, CURRENT_BINARY) and
         source["fullReport"]["path"] == str(NEW_F / "report.json"), "binary_successor_epoch_product_path")
    candidate = epoch["candidate"]
    need(candidate["bootstrapExecuted"] is True and candidate["sourceState"] == {"users": 8, "schema": 28, "migrations": 28} and
         candidate["originalProvision"] == PROVISION and candidate["seedProvenance"] == SEED and candidate["binary"] == source["binary"] and
         candidate["currentSourceManifest"] == source["sourceManifest"] and candidate["backendReport"] == source["fullReport"] and
         candidate["input"] == candidate["productInput"] == epoch["productInput"], "binary_successor_candidate_binding")
    return epoch


def validate_seed_runtime_binding(binding, epoch_pin, epoch, seed):
    version = binding["version"]
    if type(version) is int and version == 3:
        need(epoch["version"] == 3 and set(binding) == SUCCESSOR_BINDING_KEYS and binding["kind"] == "audited-candidate-seed-runtime-binding" and
             binding["runtimeEpoch"] == epoch_pin and binding["previousBinding"] == CURRENT_ENV_BINDING and binding["reviewedState"] == SUCCESSOR_STATE and
             binding["reviewedSummary"] == SUCCESSOR_SUMMARY and binding["priorCloseout"] == SUCCESSOR_PRIOR_CLOSEOUT and binding["priorSource"] == SUCCESSOR_PRIOR_SOURCE and
             binding["originalSeed"] == SEED and binding["seedExecutor"] == seed["helper"] and binding["seedExecutor"]["sha256"] == SEED_EXECUTOR and
             binding["seedInput"] == seed["input"] and binding["seedSessionAddendum"] == SEED_ADDENDUM and binding["admission02"] == ADMISSION02 and
             binding["candidateAdmissionComplete"] is False, "binary_successor_seed_provenance")
        for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources", "seedCleanup"):
            need(canonical(binding[key]) == canonical(seed["cleanup"] if key == "seedCleanup" else seed[key]), "binary_successor_seed_mapping_changed")
        rows = binding["currentSessions"]
        need(len(rows) == 15 and all(set(row) == {"kind", "credentialId", "tokenSha256", "userId", "revokedAt"} for row in rows), "binary_successor_binding_session_shape")
        projected = {"tables": {"sessions": [{"kind": row["kind"], "id": row["credentialId"], "token_hash": "\\x" + row["tokenSha256"], "user_id": row["userId"], "revoked_at": row["revokedAt"]} for row in rows]}}
        need(rows == successor_sessions(projected), "binary_successor_binding_session_order")
        validate_epoch(epoch)
        return binding
    need(type(version) is int and version in (1, 2) and version == epoch["version"] and set(binding) == (BINDING_KEYS if version == 1 else ENV_BINDING_KEYS) and binding["kind"] == "audited-candidate-seed-runtime-binding" and
         binding["runtimeEpoch"] == epoch_pin and binding["originalSeed"] == SEED and binding["seedExecutor"] == seed["helper"] and binding["seedExecutor"]["sha256"] == SEED_EXECUTOR and
         binding["seedInput"] == seed["input"] and binding["seedSessionAddendum"] == SEED_ADDENDUM and binding["admission02"] == ADMISSION02 and binding["candidateAdmissionComplete"] is False,
         "seed_epoch_provenance_changed")
    for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources"):
        need(binding[key] == seed[key], "seed_business_binding_changed_" + key)
    need(binding["seedCleanup"] == seed["cleanup"] and len(binding["currentSessions"]) == (3 if version == 1 else 6), "seed_epoch_session_binding_changed")
    if version == 2:
        descriptor(binding["previousBinding"])
        need(binding["admission03"] == ADMISSION03 and binding["failureCloseout"] == ADMISSION03_CLOSEOUT and binding["closedState"] == ADMISSION03_STATE, "environment_binding_history_changed")
    validate_epoch(epoch)
    return binding


def validate_environment_revision_input(value):
    need(set(value) == ENV_INPUT_KEYS and value["kind"] == "audited-candidate-environment-revision-input" and type(value["version"]) is int and value["version"] == 1,
         "environment_input_schema")
    for key in ENV_INPUT_KEYS - {"kind", "version", "output", "additions", "budgets"}:
        descriptor(value[key])
    need(value["previousEpoch"] == PREVIOUS_BINARY_EPOCH and value["admission03"] == ADMISSION03 and value["failureCloseout"] == ADMISSION03_CLOSEOUT and
         value["closedState"] == ADMISSION03_STATE and value["additions"] == BACKUP_ADDITIONS and value["budgets"] == ENV_LIMITS and
         all(type(n) is int for n in value["budgets"].values()), "environment_revision_scope_changed")
    need(Path(value["output"]).parent == R and re.fullmatch(r"candidate-backup-limits-revision-[0-9]{2}", Path(value["output"]).name) and
         value["previousSeedBinding"]["path"] == str(R / "candidate-cancellation-transition-01/private/seed-runtime-binding.json"), "environment_output_or_previous_binding_invalid")
    return value


def environment_values(raw):
    need(isinstance(raw, bytes) and raw.endswith(b"\n") and b"\r" not in raw and b"\x00" not in raw and len(raw) <= 1 << 20, "environment_bytes_invalid")
    values = {}
    for line in raw.splitlines():
        if not line or line.startswith(b"#"):
            continue
        match = re.fullmatch(rb"([A-Z][A-Z0-9_]*)=(.*)", line)
        need(match is not None, "environment_assignment_invalid")
        key = match[1].decode("ascii")
        need(key not in values, "environment_duplicate_key")
        values[key] = match[2]
    return values


def revised_environment(raw):
    values = environment_values(raw)
    need(not set(BACKUP_ADDITIONS) & set(values) and values.get("GOBY_BACKUP_MIN_FREE_BYTES") == b"67108864", "environment_keys_not_missing_or_minfree_changed")
    return raw + b"".join((key + "=" + val + "\n").encode() for key, val in sorted(BACKUP_ADDITIONS.items()))


def validate_environment_append(before, after):
    need(revised_environment(before) == after, "environment_change_is_not_exact_append")
    return {"addedKeys": sorted(BACKUP_ADDITIONS), "minimumFreeBytes": 64 << 20}


def resolve_epoch_lineage(epoch, read_descriptor):
    """Resolve only the three explicitly frozen generations; never an open graph."""
    validate_epoch(epoch)
    if epoch["version"] == 3:
        return resolve_binary_successor_lineage(epoch, read_descriptor)
    if epoch["version"] == 1:
        return {"productEpoch": epoch, "productInput": read_descriptor(epoch["transitionInput"]), "configurationInput": None}
    previous = validate_epoch(read_descriptor(epoch["previousEpoch"]))
    need(previous["version"] == 1 and epoch["productInput"] == previous["transitionInput"] and epoch["currentSource"] == previous["currentSource"] and
         epoch["helpers"] == previous["helpers"] and epoch["candidate"]["binary"] == previous["candidate"]["binary"] and
         epoch["configurationChange"]["before"] == previous["candidate"]["runtime"] and epoch["postgresProcess"] == previous["postgresProcess"], "environment_epoch_product_lineage_changed")
    old_candidate, current_candidate = previous["candidate"], epoch["candidate"]
    changed = {"input", "runtime", "processes", "serverIdentity", "listener"}
    need(set(current_candidate) == set(old_candidate) | {"productInput"} and all(current_candidate[key] == old_candidate[key] for key in set(old_candidate) - changed) and
         current_candidate["processes"]["postgres"] == old_candidate["processes"]["postgres"], "environment_epoch_changed_unrelated_configuration")
    stable_process_fields = {"bootId", "uid", "exe", "exeDevice", "exeInode", "cmdline", "networkNamespace", "cgroup"}
    need(all(epoch["candidateProcess"][key] == previous["candidateProcess"][key] for key in stable_process_fields), "environment_epoch_changed_binary_or_sandbox")
    config_input = validate_environment_revision_input(read_descriptor(epoch["transitionInput"]))
    need(config_input["previousEpoch"] == epoch["previousEpoch"] and config_input["runtimeHelper"] == epoch["runtimeHelper"], "environment_epoch_input_cross_binding")
    return {"productEpoch": previous, "productInput": read_descriptor(previous["transitionInput"]), "configurationInput": config_input}


def validate_successor_review(summary, state, prior_closeout, prior_source, *, now=None):
    need(summary["kind"] == "audited-tv-parent-transition-startup-state-review" and summary["status"] == "captured_state_supports_bounded_transition_contract" and
         summary["state"] == SUCCESSOR_STATE and summary["runtimeEpoch"] == CURRENT_ENV_EPOCH and summary["seedBinding"] == CURRENT_ENV_BINDING and
         summary["priorSource"] == SUCCESSOR_PRIOR_SOURCE and summary["source"]["ownedTablesExactToTvCloseout"] == 35 and summary["source"]["sequencesExact"] is True and
         prior_closeout["status"] == "owned_state_closed_client_acceptance_pending" and prior_closeout["clientAcceptance"] is False and
         prior_closeout["inputEvidence"]["after"] == SUCCESSOR_PRIOR_SOURCE and prior_closeout["inputEvidence"]["epoch"] == CURRENT_ENV_EPOCH, "binary_successor_review_binding")
    need(canonical(state["source"]["tables"]) == canonical(prior_source["tables"]) and canonical(state["source"]["sequences"]) == canonical(prior_source["sequences"]), "binary_successor_review_source_differs")
    return validate_successor_state(state, now=now)


def resolve_binary_successor_lineage(epoch, read_descriptor):
    previous = validate_epoch(read_descriptor(epoch["previousEpoch"]))
    need(previous["version"] == 2, "binary_successor_requires_configuration_parent")
    inherited = resolve_epoch_lineage(previous, read_descriptor)
    value = validate_binary_successor_input(read_descriptor(epoch["transitionInput"]))
    for key, filename in (("before", "before.json"), ("after", "after.json"), ("preservation", "preservation.json")):
        need(epoch[key]["path"] == str(Path(value["output"]) / "private" / filename), "binary_successor_proof_scope_changed")
    need(value["previousEpoch"] == epoch["previousEpoch"] and value["helpers"] == epoch["helpers"] == previous["helpers"] and
         epoch["configurationInput"] == previous["transitionInput"] and epoch["currentSource"]["fullReport"] == value["newFullReport"] and
         epoch["currentSource"]["sourceManifest"] == value["newSourceManifest"] and epoch["currentSource"]["binary"]["sha256"] == value["newBinary"]["sha256"], "binary_successor_product_configuration_lineage")
    old, current = previous["candidate"], epoch["candidate"]
    need(value["frontendReport"] == old["frontendReport"], "binary_successor_frontend_lineage_changed")
    changed = {"input", "productInput", "binary", "currentSourceManifest", "backendReport", "processes", "serverIdentity", "listener", "databases"}
    need(set(current) == set(old) and all(canonical(current[key]) == canonical(old[key]) for key in set(old) - changed) and
         current["processes"]["postgres"] == old["processes"]["postgres"] and epoch["postgresProcess"] == previous["postgresProcess"], "binary_successor_changed_configuration")
    need(all(epoch["candidateProcess"][key] == previous["candidateProcess"][key] for key in ("bootId", "uid", "exe", "cmdline", "networkNamespace", "cgroup")), "binary_successor_changed_process_sandbox")
    report = read_descriptor(value["newFullReport"])
    worker = read_descriptor({"path": str(NEW_F / "worker-report.json"), "sha256": report["worker_report_sha256"]})
    validate_successor_full_report(report, worker, value)
    sources = read_descriptor(value["newSourceManifest"])
    need(isinstance(sources, dict) and CATALOG_RELATIVE in sources and
         value["compiledCatalog"]["sha256"] == sources[CATALOG_RELATIVE]["sha256"], "binary_successor_catalog_source_binding")
    state = read_descriptor(epoch["reviewedState"])
    validate_successor_review(read_descriptor(epoch["reviewedSummary"]), state, read_descriptor(SUCCESSOR_PRIOR_CLOSEOUT), read_descriptor(SUCCESSOR_PRIOR_SOURCE),
                              now=datetime.fromisoformat(state["capturedAt"].replace("Z", "+00:00")))
    before, after = read_descriptor(epoch["before"]), read_descriptor(epoch["after"])
    compare_successor_preservation(state, before, now=datetime.fromisoformat(before["capturedAt"].replace("Z", "+00:00")))
    compare_successor_preservation(before, after, installed_binary=epoch["currentSource"]["binary"], now=datetime.fromisoformat(after["capturedAt"].replace("Z", "+00:00")))
    proof = read_descriptor(epoch["preservation"])
    need(proof["before"] == epoch["before"] and proof["after"] == epoch["after"] and proof["reviewedState"] == SUCCESSOR_STATE and
         proof["installedBinary"] == epoch["currentSource"]["binary"], "binary_successor_preservation_proof_binding")
    need(after["candidateAfter"] == {**epoch["candidateProcess"], "listener": {"host": "127.0.0.1", "port": current["listener"]["port"], "socketInode": current["listener"]["socketInode"]}} and
         after["postgresAfter"] == epoch["postgresProcess"] and after["leaseAfter"] == epoch["lease"], "binary_successor_after_runtime_proof")
    for slot in ("source", "recovery"):
        need(set(current["databases"][slot]) == set(old["databases"][slot]) and all(current["databases"][slot][key] == old["databases"][slot][key]
             for key in old["databases"][slot] if key != "afterStart") and current["databases"][slot]["afterStart"] == after["databases"][slot] == before["databases"][slot] == state["databases"][slot],
             "binary_successor_database_facts_drift")
    return {"productEpoch": epoch, "productInput": value, "configurationInput": inherited["configurationInput"]}


def environment_revision_sessions(previous_binding, report, state):
    anchors = [state[key] for key in ("runtimeEpoch", "previousEpoch") if key in state]
    need(previous_binding["version"] == 1 and len(previous_binding["currentSessions"]) == 3 and report["kind"] == "audited-candidate-live-admission" and
         report["status"] == "admission_failed_resources_retained" and report["requests"] == {"normal": 36, "cleanup": 6} and
         not report["cleanupFailures"] and set(report["operations"]) == {"create"} and report["playbackRequests"] == report["applyRequests"] == report["rollbackRequests"] == 0 and
         set(report["controllerSessions"]) == {"admin", "P", "Q"} and anchors == [PREVIOUS_BINARY_EPOCH] and state["admission03"] == ADMISSION03,
         "admission03_revision_baseline_changed")
    expected = list(previous_binding["currentSessions"])
    users = {"admin": previous_binding["admin"]["id"], "P": previous_binding["actors"]["movie"]["id"], "Q": previous_binding["controlQ"]["id"]}
    for role in ("admin", "P", "Q"):
        row = report["controllerSessions"][role]
        need(row["sameTokenRejected"] is True and re.fullmatch(r"[0-9a-f]{32}", row["credentialId"]) and re.fullmatch(r"[0-9a-f]{64}", row["tokenSha256"]), "admission03_session_not_closed")
        expected.append({"kind": "admin" if role == "admin" else "emby", "tokenSha256": row["tokenSha256"], "credentialId": row["credentialId"]})
    actual = state["source"]["tables"]["sessions"]
    need(len(actual) == 6 and len({row["tokenSha256"] for row in expected}) == 6 and all(row["revoked_at"] is not None for row in actual) and
         {(row["kind"], row["token_hash"]) for row in actual} == {(row["kind"], "\\x" + row["tokenSha256"]) for row in expected}, "six_revoked_sessions_not_exact")
    for role, row in report["controllerSessions"].items():
        matches = [item for item in actual if item["id"] == row["credentialId"]]
        need(len(matches) == 1 and matches[0]["user_id"] == users[role] and matches[0]["token_hash"] == "\\x" + row["tokenSha256"], "admission03_actor_session_mismatch")
    older = {row["tokenSha256"] for row in previous_binding["currentSessions"]}
    need(all(row["user_id"] == users["admin"] for row in actual if row["token_hash"].removeprefix("\\x") in older), "previous_session_actor_changed")
    return expected


def verify_environment_epoch_files(epoch, seed_module):
    if epoch["version"] == 1:
        return
    if epoch["version"] == 3:
        resolve_epoch_lineage(epoch, seed_module.descriptor)
        previous = seed_module.descriptor(epoch["previousEpoch"])
        need(epoch["candidate"]["runtime"] == previous["candidate"]["runtime"], "binary_successor_environment_changed")
        verify_environment_epoch_files(previous, seed_module)
        return
    resolve_epoch_lineage(epoch, seed_module.descriptor)
    change = epoch["configurationChange"]
    before = seed_module.read_checked(change["preservedCopy"]["path"], change["preservedCopy"]["sha256"])
    after = seed_module.read_checked(change["after"]["path"], change["after"]["sha256"])
    validate_environment_append(before, after)


class EpochReader:
    """Read the explicitly selected epoch; no provisioning or users0 assumptions."""
    def __init__(self, epoch, modules, private):
        self.epoch, self.modules = validate_epoch(epoch), modules
        verify_environment_epoch_files(self.epoch, modules["seed"])
        self.manifest = self.candidate = epoch["candidate"]
        p = modules["provision"].Provision({"runId": self.candidate["runId"], "ports": self.candidate["ports"]}, epoch["transitionInput"], epoch["helpers"]["provision"])
        p.private = Path(private)
        p.argv = {"server": [str(C / "install/goby")], "postgres": ["/usr/lib/postgresql/17/bin/postgres", "-D", str(C / "postgres/data"), "-c", "config_file=" + str(C / "postgres/server.conf")]}
        p.pg_identity, p.pg_version = self.candidate["postgresIdentity"], self.candidate["postgresVersionNum"]
        self.provision = p

    def pin(self):
        for role, expected in (("server", self.epoch["candidateProcess"]), ("postgres", self.epoch["postgresProcess"])):
            observed = self.provision.show(self.provision.units[role])
            need(observed == self.candidate["processes"][role] and self.modules["gateway"].metadata(expected["pid"]) == expected and
                 self.provision.process(role, observed) == self.candidate[role + "Identity"], "runtime_epoch_process_changed")
        listener = self.candidate["listener"]
        self.modules["gateway"].verify_listener(self.epoch["candidateProcess"]["pid"], {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]})
        return {**self.epoch["candidateProcess"], "listener": {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]}}

    def assert_target_cluster(self):
        identity = self.provision.cluster(self.candidate["processes"]["postgres"])
        need(identity == self.candidate["clusterSystemIdentifier"], "runtime_epoch_cluster_changed")
        return identity

    def process(self, role, extra=()):
        need(role in ("server", "postgres") and set(extra) <= {"NRestarts", "MemoryCurrent", "MemoryPeak", "TasksCurrent"}, "epoch_process_fields_invalid")
        self.pin()
        observed = self.provision.show(self.provision.units[role])
        if extra:
            result = subprocess.run(["/usr/bin/systemctl", "show", self.provision.units[role], "--property=" + ",".join(extra)], capture_output=True, timeout=10, check=True)
            observed.update(dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line))
        return self.candidate[role + "Identity"], observed

    def database_facts(self, slot):
        need(slot in ("source", "recovery"), "epoch_database_slot_invalid")
        value = self.provision.database_facts(self.candidate["processes"]["postgres"], self.candidate["database" if slot == "source" else "recoveryDatabase"])
        need(value == self.candidate["databases"][slot]["afterStart"], "epoch_database_facts_changed")
        return value

    def sql_json(self, database, single_select):
        need(database in ("postgres", self.candidate["database"], self.candidate["recoveryDatabase"]) and single_select.startswith("SELECT ") and ";" not in single_select, "epoch_read_only_query_scope")
        self.assert_target_cluster()
        return self.modules["seed"].parse(self.provision.psql("epoch-read-only", "BEGIN READ ONLY; " + single_select + "; COMMIT;", database))

    def deployment_lease(self):
        self.pin()
        key, name = 4919415424202458201, self.candidate["database"]
        rows = self.sql_json(name, "SELECT COALESCE(json_agg(json_build_object('backendPid',a.pid,'user',a.usename,'database',a.datname,'application',a.application_name,'clientHost',host(a.client_addr),'clientPort',a.client_port,'backendStart',a.backend_start,'mode',l.mode,'granted',l.granted) ORDER BY a.pid),'[]'::json) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE l.locktype='advisory' AND l.classid=" + str(key >> 32) + "::oid AND l.objid=" + str(key & 0xffffffff) + "::oid AND l.objsubid=1 AND l.database=(SELECT oid FROM pg_database WHERE datname='" + name + "')")
        need(len(rows) == 1 and rows[0]["user"] == rows[0]["database"] == name and rows[0]["application"] == "goby" and rows[0]["clientHost"] == "127.0.0.1" and rows[0]["granted"] is True and rows[0]["mode"] == "ExclusiveLock", "epoch_deployment_lease_not_single")
        row, pid = rows[0], self.epoch["candidateProcess"]["pid"]
        root = Path("/proc") / str(pid)
        matches = [parts for line in (root / "net/tcp").read_text().splitlines()[1:] if len(parts := line.split()) > 9 and parts[1] == "0100007F:%04X" % row["clientPort"] and parts[2] == "0100007F:%04X" % self.candidate["ports"]["postgres"] and parts[3] == "01"]
        links = set()
        for entry in (root / "fd").iterdir():
            try:
                links.add(os.readlink(entry))
            except FileNotFoundError:
                pass
        need(len(matches) == 1 and "socket:[" + matches[0][9] + "]" in links, "epoch_lease_connection_not_owned")
        row["candidateConnection"] = {"pid": pid, "localPort": row["clientPort"], "remotePort": self.candidate["ports"]["postgres"], "socketInode": matches[0][9]}
        self.pin()
        return row


class EpochIO:
    """The bounded recorder reused with explicit current-epoch authority."""
    def __init__(self, value, input_pin, source_pin, epoch_pin, binding_pin, modules):
        self.value, self.input_pin, self.source_pin, self.modules = value, input_pin, source_pin, modules
        self.seed = modules["seed"]
        self.epoch = validate_epoch(self.seed.descriptor(epoch_pin))
        original_seed = self.seed.descriptor(SEED)
        self.binding = validate_seed_runtime_binding(self.seed.descriptor(binding_pin), epoch_pin, self.epoch, original_seed)
        if self.epoch["version"] == 2:
            lineage = resolve_epoch_lineage(self.epoch, self.seed.descriptor)
            need(self.binding["previousBinding"] == lineage["configurationInput"]["previousSeedBinding"], "environment_previous_binding_input_changed")
            prior_binding = self.seed.descriptor(self.binding["previousBinding"])
            validate_seed_runtime_binding(prior_binding, self.epoch["previousEpoch"], lineage["productEpoch"], original_seed)
            closure, state = self.seed.descriptor(ADMISSION03_CLOSEOUT), self.seed.descriptor(ADMISSION03_STATE)
            need(closure["kind"] == "audited-candidate-admission03-failure-closeout" and closure["status"] == "failed_attempt_closed_for_configuration_revision" and closure["closedState"] == ADMISSION03_STATE,
                 "environment_failure_closeout_not_bound")
            need(self.binding["currentSessions"] == environment_revision_sessions(prior_binding, self.seed.descriptor(ADMISSION03), state), "environment_binding_sessions_changed")
        elif self.epoch["version"] == 3:
            lineage = resolve_epoch_lineage(self.epoch, self.seed.descriptor)
            previous = self.seed.descriptor(self.epoch["previousEpoch"])
            prior_binding = self.seed.descriptor(self.binding["previousBinding"])
            validate_seed_runtime_binding(prior_binding, self.epoch["previousEpoch"], previous, original_seed)
            need(self.binding["currentSessions"] == successor_sessions(self.seed.descriptor(self.binding["priorSource"])) and
                 self.binding["previousBinding"] == lineage["productInput"]["previousBinding"], "binary_successor_binding_sessions_changed")
        self.output, self.private = Path(value["output"]), Path(value["output"]) / "private"
        self.budgets = value["budgets"]
        need(self.output.parent == R and re.fullmatch(r"[a-z0-9][a-z0-9-]{1,79}", self.output.name) and set(self.budgets) == set(self.seed.BUDGETS) and
             all(type(n) is int and n > 0 for n in self.budgets.values()) and self.budgets["maximumSeconds"] <= 1200 and
             self.budgets["cleanupSeconds"] < self.budgets["maximumSeconds"] and self.budgets["maximumRequests"] <= 240 and self.budgets["cleanupRequests"] < self.budgets["maximumRequests"], "epoch_io_scope_or_budget_invalid")
        self.started, self.byte_count, self.last_response = time.monotonic(), 0, None
        self.created, self.request_states, self.requests = False, [], {"normal": 0, "cleanup": 0}
        self.candidate = self.epoch["candidate"]
        self.port = self.candidate["ports"]["http"]

    def open(self):
        need(not os.path.lexists(self.output), "epoch_io_output_collision")
        for pin in [self.candidate["binary"], self.candidate["runtime"], *self.candidate["units"].values()]:
            self.seed.read_checked(pin["path"], pin["sha256"])
        self.seed.write_json_once(self.output.with_name(self.output.name + "-intent.json"), {"input": self.input_pin, "source": self.source_pin, "runtimeEpoch": self.binding["runtimeEpoch"], "operation": "create-epoch-reader-output"})
        os.mkdir(self.output, 0o700)
        self.created = True
        os.mkdir(self.private, 0o700)
        self.seed.sync_dir(self.output)
        self.seed.sync_dir(self.output.parent)
        self.reader = EpochReader(self.epoch, self.modules, self.private)
        self.provision = self.reader.provision
        self.pin()
        return self

    def pin(self):
        return self.reader.pin()

    def deadline(self, cleanup=False):
        return self.seed.CandidateIO.deadline(self, cleanup)

    def request(self, *args, **kwargs):
        return self.seed.CandidateIO.request(self, *args, **kwargs)
