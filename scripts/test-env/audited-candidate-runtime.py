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
    need(set(epoch) == EPOCH_KEYS and epoch["kind"] == "audited-candidate-runtime-epoch" and epoch["version"] == 1 and epoch["status"] == "running_awaiting_live_acceptance" and
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
    need(epoch["calls"] == {"stop": 1, "replace": 1, "start": 1}, "epoch_call_count")
    return epoch


def validate_seed_runtime_binding(binding, epoch_pin, epoch, seed):
    need(set(binding) == BINDING_KEYS and binding["kind"] == "audited-candidate-seed-runtime-binding" and binding["version"] == 1 and
         binding["runtimeEpoch"] == epoch_pin and binding["originalSeed"] == SEED and binding["seedExecutor"] == seed["helper"] and binding["seedExecutor"]["sha256"] == SEED_EXECUTOR and
         binding["seedInput"] == seed["input"] and binding["seedSessionAddendum"] == SEED_ADDENDUM and binding["admission02"] == ADMISSION02 and binding["candidateAdmissionComplete"] is False,
         "seed_epoch_provenance_changed")
    for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources"):
        need(binding[key] == seed[key], "seed_business_binding_changed_" + key)
    need(binding["seedCleanup"] == seed["cleanup"] and len(binding["currentSessions"]) == 3, "seed_epoch_session_binding_changed")
    validate_epoch(epoch)
    return binding


class EpochReader:
    """Read the explicitly selected epoch; no provisioning or users0 assumptions."""
    def __init__(self, epoch, modules, private):
        self.epoch, self.modules = validate_epoch(epoch), modules
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
