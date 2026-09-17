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
import traceback
from types import SimpleNamespace
from urllib.parse import urlencode


MIB = 1 << 20
LIMITS = {"maximumSeconds": 900, "cleanupSeconds": 180,
          "maximumRequests": 120, "cleanupRequests": 10}
FRESH_LIMITS = {**LIMITS, "maximumRequests": 129}
FRESH_ROOT = Path("/opt/goby-test/embedded-candidate-20260914")
FRESH_SOURCE_MANIFEST = {
    "path": "/opt/goby-test/full-regression-continuation-20260914/retained/source-manifest.json",
    "sha256": "95fe6a40ecabdfe6b48260dcf200cf8a49d0cf9664c407ca91cd9eaba539d6f6"}
FRESH_BINARY_SHA = "59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312"
FRESH_PRODUCT_HASHES = {
    "sourceArchive": "418f9803237e02e0859f9bef8f6ed646730acb590a3482867a288cd64e8a0fad",
    "sourceManifest": FRESH_SOURCE_MANIFEST["sha256"],
    "embeddedBuildManifest": "a732425002323e0e29a4ca7cf9e0fd517d773c1de027d810488173b75957e683",
    "productionSourceBinding": "823870bb9bcf9e2ca4b35e483d617de34e1c541b273cd6bf02e647a066e920dc",
    "regressionReview": "5b946d4b4bfee2b177b53861c89fad690c08c406b832a26f778d563ed4174b72",
    "regressionClosure": "6cd0b024834315db240d6268c94b578e4beb99977fd1740d51c5494cf4508b1c"}
FRESH_BACKUP_PROFILE = {"name": "bounded-fixture-backup-v1", "environment": {
    "GOBY_BACKUP_MAX_OBJECT_BYTES": "67108864", "GOBY_BACKUP_MAX_TOTAL_BYTES": "268435456"}}
FRESH_INSPECTION_BUDGETS = {"maximumSeconds": 900, "maximumSqlSessions": 64}
CATALOG_RELATIVE = "internal/backuppg/catalogs/schema-28-postgresql-17.json"
SOURCE_MANIFEST_SHA = "6382c327bada158e06a3fda318d6bf85836a7be4457cf523314f87fbb64f8c0b"
INSPECTION_SHA = "2347a80728803612de59d4de34f6a90f7a1005a30f5ed4092813f1915548c5bf"
EPOCH_ARCHIVE_SHA = "b7120b6f323ace203fe7b49c56cd669ce5b9ff3ef8f3ceddb4359c2951aa658b"
TV_LIMITS = {"maximumSeconds": 180, "cleanupSeconds": 60, "maximumRequests": 28, "cleanupRequests": 8}
TV_ROOT = "/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14/"
TV_EPOCH = {"path": TV_ROOT + "candidate-tv-parent-transition-01/private/runtime-epoch.json",
            "sha256": "76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac"}
TV_BINDING = {"path": TV_ROOT + "candidate-tv-parent-transition-01/private/seed-runtime-binding.json",
              "sha256": "94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43"}
TV_CLOSEOUT = {"path": TV_ROOT + "candidate-tv-parent-transition-closeout.json",
               "sha256": "c9e03c008d0d1dbf0b66b8070692738e50a2ca57c29f89cbd40c1fba38d597ba"}
TV_REUSED = {"path": TV_ROOT + "candidate-live-admission-04/private/report.json",
             "sha256": "05083c7cc5c65c62e30018136b6a7d383c144c9742d96eaecc52e9c2cfc19653"}
TV_CHECKS = ("runtimeIdentity", "tvDefaultParents", "tvDetailParents", "ordinaryAuthorization",
             "healthWindow60Seconds", "sourceAndInactivePreserved", "sessionCleanup")
TV_REUSED_CONTRACTS = ["native_authentication_and_query_carriers", "storage_and_library_access",
                       "backup_create_download", "restore_ready_cancel_retained_stage"]
PROGRAMS_REUSED = {"path": TV_ROOT + "candidate-live-admission-05/private/report.json",
                   "sha256": "b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535"}
PROGRAMS_CHECKS = ("runtimeIdentity", "programsEmptyQuery", "programsSeriesQuery", "ordinaryAuthorization",
                   "healthWindow60Seconds", "sourceAndInactivePreserved", "sessionCleanup")
PROGRAMS_REUSED_CONTRACTS = ["tv_parent_projection_and_ordinary_authorization", *TV_REUSED_CONTRACTS]
PROGRAMS_QUERY = {"HasAired": "false", "SortBy": "StartDate", "ImageTypeLimit": "1",
                  "EnableImageTypes": "Primary,Thumb,Backdrop", "EnableUserData": "false",
                  "Fields": "PrimaryImageAspectRatio,ChannelInfo",
                  "Limit": "12", "X-Emby-Language": "en-us"}
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
CONTROL_POLICY = {"EnableAllFolders": False, "EnabledFolders": [], "EnableMediaPlayback": True,
                  "EnablePlaybackRemuxing": True, "EnableAudioPlaybackTranscoding": True,
                  "EnableVideoPlaybackTranscoding": True, "IsAdministrator": False, "IsDisabled": False}
SAFE_ERROR_FIELDS = set("EnableAllFolders EnabledFolders Path ParentId Id Type Name ServerId MediaSources ItemId "
                       "DurationTicks RunTimeTicks ProbeVersion media policy source catalog actualCatalogDtos "
                       "binding_revision storage_binding last_scan_at Revision GenerationRevision Limits Rollback "
                       "Storage CanApply CanCancel Operation Backup AccessToken CSRFToken SessionInfo User Error "
                       "ResponseStatus Code ErrorCode token_hash device_registry_id actor_credential_id".split())


class AdmissionError(ValueError):
    """Expose fixed diagnostic codes without request values or credentials."""


def need(condition, code):
    if not condition:
        raise AdmissionError(code)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def safe_failure(error, stage):
    result = {"stage": stage, "type": type(error).__name__,
              "code": str(error) if isinstance(error, AdmissionError) else "admission_operation_failed"}
    if isinstance(error, KeyError):
        field = error.args[0] if error.args and isinstance(error.args[0], str) else None
        result["field"] = field if field in SAFE_ERROR_FIELDS else "unrecognized_field"
    frames = [frame for frame in traceback.extract_tb(error.__traceback__) if frame.filename == __file__]
    if frames:
        result.update(sourceFile="admit-audited-candidate.py", sourceLine=frames[-1].lineno)
    return result


def validate_actor_policy(role, user):
    need(role in {"admin", "P", "Q"} and isinstance(user.get("policy"), dict) and
         canonical(user["policy"]) == canonical(CONTROL_POLICY if role == "Q" else {}),
         "admission_observed_raw_policy_changed")


def validate_actual_dto(dto, row, seed):
    need(row is not None and dto["Id"] == row["id"] and dto["Type"] == row["type"] and
         dto["ServerId"] == seed["serverId"], "seed_actual_dto_source_binding")
    if dto["Type"] == "MusicAlbum":
        need(dto["Id"] == seed["catalog"]["album"]["id"] and "Path" not in dto and row["path"] == "" and
             dto.get("ParentId") == row["parent_id"] == seed["libraries"]["Music"]["Id"],
             "seed_actual_album_projection_binding")
    else:
        need(dto.get("Path") == row["path"] and dto.get("ParentId") == row["parent_id"],
             "seed_actual_dto_path_or_parent_binding")
    if row["type"] in {"Movie", "Episode", "Audio"}:
        need(dto["RunTimeTicks"] == row["media"]["DurationTicks"] and len(dto["MediaSources"]) == 1 and
             dto["MediaSources"][0]["ItemId"] == row["id"] and dto["MediaSources"][0]["Path"] == row["path"],
             "seed_actual_dto_media_binding")


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
    common = {"kind", "version", "runId", "runtimeHelper", "admissionHelper", "compiledCatalog", "output", "budgets"}
    versions = {1: common | {"candidateManifest", "seedManifest", "runtimeInspection", "seedHelper", "inspectionHelper", "sourceManifest"},
                2: common | {"runtimeEpoch", "seedRuntimeBinding"},
                3: common | {"runtimeEpoch", "seedRuntimeBinding", "admissionKind", "reusedAdmission04", "transitionCloseout"},
                4: common | {"candidateManifest", "seedManifest", "runtimeInspection", "seedHelper", "inspectionHelper", "sourceManifest", "admissionKind"},
                5: common | {"runtimeEpoch", "seedRuntimeBinding", "admissionKind", "reusedAdmission05", "transitionCloseout"}}
    need(isinstance(value, dict) and type(value.get("version")) is int and value["version"] in versions,
         "admission_input_version")
    fields = versions[value["version"]]
    limits = FRESH_LIMITS if value["version"] == 4 else TV_LIMITS if value["version"] in (3, 5) else LIMITS
    same_limits = canonical(value.get("budgets")) == canonical(limits) if value["version"] in (4, 5) else value.get("budgets") == limits
    need(isinstance(value, dict) and set(value) == fields and value["kind"] == "audited-candidate-admission-input"
         and same_limits, "admission_input_contract")
    need(isinstance(value["runId"], str) and re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9._-]{0,79}", value["runId"]),
         "invalid_admission_run_id")
    pins = fields - {"kind", "version", "runId", "output", "budgets", "admissionKind"}
    for key in pins:
        descriptor(value[key])
    if value["version"] == 3:
        need(value["admissionKind"] == "affected_tv_parent" and value["runtimeEpoch"] == TV_EPOCH and
             value["seedRuntimeBinding"] == TV_BINDING and value["reusedAdmission04"] == TV_REUSED and
             value["transitionCloseout"] == TV_CLOSEOUT, "tv_admission_frozen_authority")
    if value["version"] == 5:
        need(value["admissionKind"] == "affected_programs" and value["reusedAdmission05"] == PROGRAMS_REUSED and
             value["runtimeEpoch"] != TV_EPOCH and value["seedRuntimeBinding"] != TV_BINDING and
             value["transitionCloseout"] != TV_CLOSEOUT, "programs_admission_successor_authority")
    if value["version"] == 1:
        need(value["sourceManifest"]["sha256"] == SOURCE_MANIFEST_SHA and
             value["inspectionHelper"]["sha256"] == INSPECTION_SHA, "wrong_frozen_source_or_inspector")
    output = Path(value["output"])
    need(output.is_absolute() and str(output).startswith("/opt/goby-test/") and ".." not in output.parts and
         output.name.startswith("candidate-live-admission-") and
         all(not Path(value[key]["path"]).is_relative_to(output) for key in pins), "invalid_admission_output_scope")
    if value["version"] == 4:
        need(value["admissionKind"] == "fresh_embedded" and output.parent == FRESH_ROOT and
             canonical(value["sourceManifest"]) == canonical(FRESH_SOURCE_MANIFEST), "fresh_admission_source_or_scope")
        for key in ("runtimeHelper", "seedHelper", "admissionHelper", "inspectionHelper", "seedManifest", "runtimeInspection"):
            need(Path(value[key]["path"]).is_relative_to(FRESH_ROOT), "fresh_admission_helper_or_evidence_scope")
    return value


def validate_fresh_embedded_authority(value, candidate, provision_input, seed, inspection):
    """Bind a new provision and seed directly; historical epochs are not actors."""
    need(value.get("version") == 4 and all(isinstance(row, dict) for row in (candidate, provision_input, seed, inspection)),
         "fresh_admission_authority_shape")
    run_id = candidate.get("runId", "")
    need(isinstance(run_id, str) and re.fullmatch(r"[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}", run_id), "fresh_candidate_run_id")
    root = Path("/opt/goby-audited-candidate-" + run_id)
    need(root != Path("/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b") and
         candidate.get("kind") == "audited-candidate-private-manifest" and type(candidate.get("provisionVersion")) is int and
         candidate["provisionVersion"] == 2 and
         candidate.get("status") == "running_awaiting_live_acceptance" and
         candidate.get("bootstrapExecuted") is False and candidate.get("recoveryRestoreExecuted") is False and
         candidate.get("clientAcceptance") is False and candidate.get("candidateAdmissionComplete") is False,
         "fresh_candidate_provision_required")
    need(provision_input.get("kind") == "audited-candidate-provision-input" and type(provision_input.get("version")) is int and
         provision_input["version"] == 2 and provision_input.get("runId") == run_id and
         provision_input.get("dashboardProfile") == "embedded-administrator-v1" and
         provision_input.get("transcodingProfile") == "software-baseline-v1", "fresh_provision_input_profile")
    product = candidate.get("productEvidence", {})
    need(set(product) == set(FRESH_PRODUCT_HASHES) and
         canonical(product) == canonical({key: provision_input.get(key) for key in FRESH_PRODUCT_HASHES}) and
         all(descriptor(product[key])["sha256"] == digest for key, digest in FRESH_PRODUCT_HASHES.items()) and
         canonical(product["sourceManifest"]) == canonical(value["sourceManifest"]), "fresh_product_evidence_binding")
    need(candidate.get("binary") == {"path": str(root / "install/goby"), "sha256": FRESH_BINARY_SHA} and
         candidate.get("dataDirectory") == str(root / "data") and
         value["candidateManifest"]["path"] == str(root / "private/manifest.json"), "fresh_candidate_layout_or_binary")
    dashboard = {"mode": "embedded", "buildManifest": product["embeddedBuildManifest"], "assetCount": 57,
                 "externalDirectoryInstalled": False, "webDirectoryOverridePresent": False}
    need(canonical(candidate.get("dashboard")) == canonical(dashboard) and
         canonical(candidate.get("backupProfile")) == canonical(FRESH_BACKUP_PROFILE) and
         candidate.get("ordinaryRegressionStatus") == "passed_with_explicit_profile_gap" and
         type(candidate.get("ordinaryRegressionPhases")) is int and candidate["ordinaryRegressionPhases"] == 2 and
         candidate.get("taggedFullRegressionClaimed") is False,
         "fresh_embedded_or_backup_profile_changed")
    need(inspection.get("kind") == "audited-candidate-runtime-inspection" and type(inspection.get("version")) is int and inspection["version"] == 3 and
         inspection.get("status") == "ready_pending_seed" and inspection.get("failure") is None and
         canonical(inspection.get("provisionManifest")) == canonical(value["candidateManifest"]) and
         canonical(inspection.get("provisionInput")) == canonical(candidate["input"]) and
         canonical(inspection.get("provisionHelper")) == canonical(value["runtimeHelper"]) and
         canonical(inspection.get("helper")) == canonical(value["inspectionHelper"]) and
         canonical(inspection.get("sourceEvidence")) == canonical(product), "fresh_initial_inspection_binding")
    initial_database, initial_dashboard = inspection.get("database", {}), inspection.get("dashboard", {})
    need(type(initial_database.get("sourceSchemaVersion")) is int and initial_database["sourceSchemaVersion"] == 28 and
         type(initial_database.get("sourceUsers")) is int and initial_database["sourceUsers"] == 0 and
         initial_database.get("recoveryTargetEmpty") is True and type(initial_dashboard.get("assetCount")) is int and initial_dashboard["assetCount"] == 57 and
         initial_dashboard.get("allAssetsMatched") is True and type(initial_dashboard.get("entryReferencesMatched")) is int and
         initial_dashboard["entryReferencesMatched"] == 5, "fresh_initial_inspection_incomplete")
    need(canonical(inspection.get("processes")) == canonical({"server": candidate["serverIdentity"], "postgres": candidate["postgresIdentity"]}) and
         inspection["httpListener"]["socketInode"] == candidate["listener"]["socketInode"], "fresh_initial_process_or_listener_changed")
    need(seed.get("kind") == "audited-candidate-seed-manifest" and type(seed.get("version")) is int and seed["version"] == 2 and
         seed.get("status") == "seeded_pending_live_acceptance" and type(seed.get("playbackRequests")) is int and seed["playbackRequests"] == 0 and
         seed.get("clientAcceptance") is False and seed.get("candidateAdmissionComplete") is False and
         len(seed.get("cleanup", [])) == 2 and
         all(row.get("logoutAcknowledged") is True and row.get("sameTokenRejected") is True for row in seed["cleanup"]),
         "fresh_seed_or_cleanup_incomplete")
    for key, expected in (("candidateManifest", value["candidateManifest"]), ("runtimeInspection", value["runtimeInspection"]),
                          ("helper", value["seedHelper"]), ("provisionInput", candidate["input"]),
                          ("provisionHelper", value["runtimeHelper"]), ("inspectionInput", inspection["input"]),
                          ("inspectionHelper", value["inspectionHelper"]), ("sourceEvidence", product)):
        need(canonical(seed.get(key)) == canonical(expected), "fresh_seed_provenance_changed:" + key)
    need(canonical(seed.get("source")) == canonical({"manifestSha256": FRESH_SOURCE_MANIFEST["sha256"],
         "binarySha256": FRESH_BINARY_SHA, "schema": 28}), "fresh_seed_source_changed")
    return root


def validate_fresh_backup_status(value, backups, operations):
    validate_status(value, generation="0")
    need(value["Limits"]["MaxBackupBytes"] == "67108864" and value["Limits"]["MaxStoredBytes"] == "268435456" and
         value["Storage"] == {"Bytes": "0", "Objects": 0} and not backups and not operations,
         "fresh_backup_limits_or_initial_inventory")


def validate_epoch_admission(value, epoch, binding, seed, full_report, transition, *, product_epoch=None, configuration_input=None):
    """Cross-bind current runtime facts without rewriting original seed provenance."""
    need(value["version"] == 2 and epoch["runtimeHelper"] == value["runtimeHelper"] and
         binding["runtimeEpoch"] == value["runtimeEpoch"] and binding["originalSeed"] == epoch["seedProvenance"] and
         binding["seedExecutor"] == seed["helper"] and binding["seedInput"] == seed["input"] and
         epoch["originalProvision"] == seed["candidateManifest"], "epoch_admission_provenance_cross_binding")
    for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources"):
        need(binding[key] == seed[key], "epoch_original_seed_business_changed:" + key)
    need(binding["seedCleanup"] == seed["cleanup"], "epoch_original_seed_cleanup_changed")
    if epoch.get("version", 1) == 2:
        need(product_epoch is not None and product_epoch["version"] == 1 and configuration_input is not None and
             epoch["operationKind"] == "environment_revision" and epoch["calls"] == {"stop": 1, "replaceEnvironment": 1, "start": 1} and
             epoch["productInput"] == product_epoch["transitionInput"] and
             configuration_input["previousEpoch"] == epoch["previousEpoch"] and
             epoch["currentSource"] == product_epoch["currentSource"] and
             epoch["originalProvision"] == product_epoch["originalProvision"] and epoch["seedProvenance"] == product_epoch["seedProvenance"] and
             epoch["helpers"] == product_epoch["helpers"], "configuration_epoch_must_preserve_product_lineage")
    else:
        need(configuration_input is None and (product_epoch is None or product_epoch == epoch), "unexpected_configuration_lineage")
    source, candidate = epoch["currentSource"], epoch["candidate"]
    need(source["archiveSha256"] == EPOCH_ARCHIVE_SHA and source["schema"] == 28 and
         full_report["archive_sha256"] == EPOCH_ARCHIVE_SHA and
         source["fullReport"] == transition["newFullReport"] == candidate["backendReport"] and
         source["sourceManifest"] == transition["newSourceManifest"] == candidate["currentSourceManifest"] and
         source["binary"] == candidate["binary"] and
         source["binary"]["sha256"] == transition["newBinary"]["sha256"] == full_report["worker"]["binary"]["sha256"] and
         value["compiledCatalog"] == transition["compiledCatalog"] and candidate["input"] == epoch["transitionInput"] and
         epoch["helpers"] == transition["helpers"],
         "epoch_current_source_cross_binding")
    need(source["sourceManifest"]["sha256"] != seed["source"]["manifestSha256"] and
         source["binary"]["sha256"] != seed["source"]["binarySha256"], "epoch_cannot_relabel_original_seed_source")
    process, listener = epoch["candidateProcess"], candidate["listener"]
    need(process["pid"] == candidate["serverIdentity"]["pid"] == listener["pid"] and
         process["startTicks"] == candidate["serverIdentity"]["startTicks"] and
         listener["port"] == candidate["ports"]["http"], "epoch_current_process_cross_binding")
    return {**process, "listener": {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]}}


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


def validate_effective_backup_limits(value, epoch):
    if epoch is None or epoch["version"] == 1:
        return
    need(epoch["version"] == 2 and epoch["operationKind"] == "environment_revision" and
         value["Limits"]["MaxBackupBytes"] == "67108864" and value["Limits"]["MaxStoredBytes"] == "268435456",
         "amended_backup_limits_not_effective")


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


def validate_tv_authority(value, epoch, binding, seed, transition, previous, reused, closeout):
    """Bind the affected checks to the successor and retain the old admission's scope."""
    need(value["version"] == epoch["version"] == binding["version"] == 3 and
         epoch["operationKind"] == "binary_successor" and epoch["runtimeHelper"] == value["runtimeHelper"] and
         binding["runtimeEpoch"] == value["runtimeEpoch"] and binding["originalSeed"] == epoch["seedProvenance"] and
         binding["seedExecutor"] == seed["helper"] and binding["seedInput"] == seed["input"] and
         epoch["originalProvision"] == seed["candidateManifest"], "tv_current_epoch_provenance")
    for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources"):
        need(canonical(binding[key]) == canonical(seed[key]), "tv_original_seed_mapping_changed")
    need(binding["seedCleanup"] == seed["cleanup"] and value["compiledCatalog"] == transition["compiledCatalog"] and
         transition["previousEpoch"] == epoch["previousEpoch"] and
         transition["previousBinding"] == binding["previousBinding"] and previous["version"] == 2 and
         previous["operationKind"] == "environment_revision", "tv_successor_predecessor_binding")
    need(type(reused["version"]) is int and reused["version"] == 2 and
         reused["kind"] == "audited-candidate-live-admission" and reused["status"] == "admitted_for_core_client" and
         reused["failure"] is None and reused["cleanupFailures"] == [] and reused["candidateAdmissionComplete"] is True and
         reused["clientAcceptance"] is False and reused["runtimeEpoch"] == epoch["previousEpoch"] and
         reused["seedRuntimeBinding"] == binding["previousBinding"] and
         canonical(reused["currentSource"]) == canonical(previous["currentSource"]) and
         reused["inactiveStageRetained"] is True and reused["activeGenerationChanged"] is False and
         all(reused[key] == 0 for key in ("playbackRequests", "applyRequests", "rollbackRequests")) and
         set(reused["controllerSessions"]) == {"admin", "P", "Q"} and
         all(row["sameTokenRejected"] is True for row in reused["controllerSessions"].values()),
         "tv_reused_admission_not_original_success")
    need(epoch["currentSource"]["binary"]["sha256"] != previous["currentSource"]["binary"]["sha256"] and
         epoch["currentSource"]["sourceManifest"]["sha256"] != seed["source"]["manifestSha256"],
         "tv_successor_cannot_relabel_old_source")
    need(closeout["kind"] == "audited-candidate-tv-parent-transition-closeout" and closeout["version"] == 1 and
         closeout["status"] == "successor_running_awaiting_affected_live_admission" and
         closeout["runtimeEpoch"] == value["runtimeEpoch"] and closeout["seedRuntimeBinding"] == value["seedRuntimeBinding"] and
         closeout["candidateAdmissionComplete"] is False and closeout["clientAcceptance"] is False and
         closeout["currentProcessPinned"] is True and closeout["oldProcessAbsent"] is True and
         closeout["stagedBinaryAbsent"] is True and closeout["installedBinaryVerified"] is True,
         "tv_transition_independent_closeout")
    for key in ("currentSource", "previousEpoch", "before", "after", "preservation", "runtimeHelper", "candidateProcess", "postgresProcess", "calls"):
        need(canonical(closeout[key]) == canonical(epoch[key]), "tv_transition_closeout_cross_binding")
    checks = closeout["preservationChecks"]
    need(checks == {"ownedTablesExact": 35, "sequencesExact": True, "inactiveStageExact": True,
                    "allPriorPlayAndUserDataExact": True, "controlMediaAssetsExact": True,
                    "configurationExact": True, "postgresContinuous": True, "hostingContinuous": True},
         "tv_transition_preservation_incomplete")
    listener = epoch["candidate"]["listener"]
    return {**epoch["candidateProcess"], "listener": {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]}}


def validate_programs_authority(value, epoch, binding, seed, transition, previous, reused, older, closeout):
    """Keep accepted admission05/04 evidence on its original source and require the new transition."""
    need(value["version"] == 5 and epoch["version"] == binding["version"] == 4 and
         value["admissionKind"] == "affected_programs" and epoch["operationKind"] == "programs_successor" and
         epoch["runtimeHelper"] == value["runtimeHelper"] and binding["runtimeEpoch"] == value["runtimeEpoch"] and
         binding["originalSeed"] == epoch["seedProvenance"] and binding["seedExecutor"] == seed["helper"] and
         binding["seedInput"] == seed["input"] and epoch["originalProvision"] == seed["candidateManifest"],
         "programs_current_epoch_provenance")
    for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources"):
        need(canonical(binding[key]) == canonical(seed[key]), "programs_original_seed_mapping_changed")
    need(binding["seedCleanup"] == seed["cleanup"] and value["compiledCatalog"] == transition["compiledCatalog"] and
         transition["previousEpoch"] == epoch["previousEpoch"] == TV_EPOCH and
         transition["previousBinding"] == binding["previousBinding"] == TV_BINDING and
         transition["currentRuntime"] == epoch["previousCurrentRuntime"] == binding["previousCurrentRuntime"] and
         previous["version"] == 3 and previous["operationKind"] == "binary_successor",
         "programs_successor_predecessor_binding")
    need(value["reusedAdmission05"] == PROGRAMS_REUSED and type(reused["version"]) is int and reused["version"] == 3 and
         reused["kind"] == "audited-candidate-live-admission" and reused["admissionKind"] == "affected_tv_parent" and
         reused["status"] == "admitted_for_core_client" and reused["failure"] is None and reused["cleanupFailures"] == [] and
         reused["candidateAdmissionComplete"] is True and reused["clientAcceptance"] is False and
         reused["runtimeEpoch"] == epoch["previousEpoch"] and reused["seedRuntimeBinding"] == binding["previousBinding"] and
         canonical(reused["currentSource"]) == canonical(previous["currentSource"]) and
         set(reused["freshChecks"]) == set(TV_CHECKS) and all(row is True for row in reused["freshChecks"].values()) and
         reused["requests"] == {"normal": 20, "cleanup": 4} and reused["inactiveStageRetained"] is True and
         reused["activeGenerationChanged"] is False and set(reused["controllerSessions"]) == {"P", "Q"} and
         all(row["sameTokenRejected"] is True for row in reused["controllerSessions"].values()) and
         all(type(reused[key]) is int and reused[key] == 0 for key in ("playbackRequests", "applyRequests", "rollbackRequests")),
         "programs_reused_admission05_not_original_success")
    need(type(older["version"]) is int and older["version"] == 2 and older["kind"] == "audited-candidate-live-admission" and
         older["status"] == "admitted_for_core_client" and older["failure"] is None and older["cleanupFailures"] == [] and
         older["candidateAdmissionComplete"] is True and older["clientAcceptance"] is False and
         older["inactiveStageRetained"] is True and older["activeGenerationChanged"] is False and
         set(older["controllerSessions"]) == {"admin", "P", "Q"} and
         all(row["sameTokenRejected"] is True for row in older["controllerSessions"].values()) and
         all(type(older[key]) is int and older[key] == 0 for key in ("playbackRequests", "applyRequests", "rollbackRequests")),
         "programs_reused_admission04_not_original_success")
    expected_reuse = {"report": TV_REUSED, "runtimeEpoch": older["runtimeEpoch"],
                      "seedRuntimeBinding": older["seedRuntimeBinding"], "currentSource": older["currentSource"],
                      "contracts": TV_REUSED_CONTRACTS}
    need(canonical(reused["reusedAdmission04"]) == canonical(expected_reuse) and
         older["runtimeEpoch"] == previous["previousEpoch"], "programs_reused_admission_chain_changed")
    need(epoch["currentSource"]["binary"]["sha256"] != previous["currentSource"]["binary"]["sha256"] and
         epoch["currentSource"]["sourceManifest"]["sha256"] != previous["currentSource"]["sourceManifest"]["sha256"],
         "programs_successor_cannot_relabel_old_source")
    need(closeout["kind"] == "audited-candidate-programs-transition-closeout" and type(closeout["version"]) is int and
         closeout["version"] == 1 and closeout["status"] == "successor_running_awaiting_affected_live_admission" and
         closeout["runtimeEpoch"] == value["runtimeEpoch"] and closeout["seedRuntimeBinding"] == value["seedRuntimeBinding"] and
         closeout["candidateAdmissionComplete"] is False and closeout["clientAcceptance"] is False and
         all(closeout[key] is True for key in ("currentProcessPinned", "oldProcessAbsent", "stagedBinaryAbsent", "installedBinaryVerified")),
         "programs_transition_independent_closeout")
    for key in ("currentSource", "previousEpoch", "previousCurrentRuntime", "before", "after", "sourceBefore", "sourceAfter",
                "preservation", "runtimeHelper", "candidateProcess", "postgresProcess", "calls"):
        need(canonical(closeout[key]) == canonical(epoch[key]), "programs_transition_closeout_cross_binding")
    listener = epoch["candidate"]["listener"]
    return {**epoch["candidateProcess"], "listener": {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]}}


def validate_programs_empty(response):
    value = response["body"]
    need(isinstance(value, dict) and set(value) == {"Items", "TotalRecordCount"} and
         isinstance(value["Items"], list) and value["Items"] == [] and
         type(value["TotalRecordCount"]) is int and value["TotalRecordCount"] == 0 and
         response_headers(response).get("content-type") == "application/json; charset=utf-8",
         "programs_empty_query_contract")


def same_snapshot(before, after, code):
    need(canonical(before["tables"]) == canonical(after["tables"]) and
         canonical(before["sequences"]) == canonical(after["sequences"]), code)


def validate_tv_controls(expected, observed):
    keys = {"trees", "controlDocuments", "fixedFiles", "loadedUnits", "protected", "hostingBefore", "hostingAfter"}
    need(set(observed) == keys and all(canonical(observed[key]) == canonical(expected[key]) for key in keys),
         "tv_control_configuration_or_hosting_changed")


def capture_tv_controls(runtime, transition_module, io, baseline, remaining, *, programs=False):
    """Borrow only the frozen file/tree/unit readers; never construct or run a transition."""
    readers = transition_module.ProgramsSuccessor if programs else transition_module.Transition
    # ProgramsSuccessor.file uses super(); allocate its type without calling the transition constructor.
    probe = object.__new__(readers) if programs else SimpleNamespace()
    probe.__dict__.update(r=runtime, s=io.modules["seed"], g=io.modules["gateway"], pmod=io.modules["provision"],
                          p=io.provision, reviewed_state=baseline, need=need, remaining=remaining)
    if programs:
        reference_pin = runtime.PROGRAMS_RECOVERY_REFERENCE
        reference = probe.s.descriptor(reference_pin)
        probe.recovery_master = runtime.programs_recovery_master_authority(reference, reference_pin)
    probe.file = lambda path, prefix=None: readers.file(probe, path, prefix)
    need(set(baseline["trees"]) == set(runtime.TREE_ROOTS), "tv_preserved_tree_inventory")
    hosting_before = transition_module.BinarySuccessor.hosting(probe)
    trees, documents, fixed = {}, {}, {}
    for root in runtime.TREE_ROOTS:
        remaining()
        trees[root], found = readers.tree(probe, root)
        documents.update(found)
    for name, facts in baseline["fixedFiles"].items():
        remaining()
        path = Path(name)
        if programs and str(path) in runtime.PROGRAMS_STAT_ONLY:
            fixed[name] = probe.file(path)[0]
        elif facts.get("absent") is True:
            need(not os.path.lexists(path), "tv_absent_fixed_file_created")
            fixed[name] = {"absent": True}
        elif facts.get("directory") is True:
            info = probe.s.safe_path(path, (0, probe.s.pwd.getpwnam("goby").pw_uid), True)
            children = list(path.iterdir())
            need(len(children) <= 64 and all(not child.is_symlink() for child in children), "tv_fixed_directory_inventory")
            fixed[name] = {"directory": True, "entries": sorted(child.name for child in children), "dev": info.st_dev,
                           "ino": info.st_ino, "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode),
                           "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}
        else:
            fixed[name] = probe.file(path)[0]
    observed = {"trees": trees, "controlDocuments": documents, "fixedFiles": fixed,
                "loadedUnits": transition_module.Transition.loaded_units(probe),
                "protected": {unit: probe.p.show(unit) for unit in runtime.PROTECTED}, "hostingBefore": hosting_before,
                "hostingAfter": transition_module.BinarySuccessor.hosting(probe)}
    remaining()
    validate_tv_controls(baseline, observed)
    return observed


def tv_rows(seed, snapshot):
    rows = indexed(snapshot["tables"]["items"])
    catalog = seed["catalog"]
    series = catalog["series"]
    wanted = [series, *catalog["seasons"], *catalog["episodes"]]
    need(len(wanted) == 6 and len(catalog["seasons"]) == 2 and len(catalog["episodes"]) == 3 and
         len({row["id"] for row in wanted}) == 6, "tv_fixture_inventory")
    result = {}
    for item in wanted:
        row = rows.get(item["id"])
        need(row is not None and row["type"] == item["type"] and row["library_id"] == seed["libraries"]["TV"]["Id"] and
             row["is_folder"] is (row["type"] != "Episode") and isinstance(row["name"], str) and row["name"] != "",
             "tv_fixture_row_identity")
        expected_parent = seed["libraries"]["TV"]["Id"] if item["type"] == "Series" else item["parentId"]
        need(row["parent_id"] == expected_parent and ("path" not in item or row["path"] == item["path"]), "tv_fixture_parent_or_path")
        if item["type"] in ("Season", "Episode"):
            need(row["index_number"] == item["indexNumber"] and item["seriesId"] == series["id"], "tv_fixture_index")
        if item["type"] == "Episode":
            need(row["parent_index_number"] == item["parentIndexNumber"] and
                 row["parent_id"] in {season["id"] for season in catalog["seasons"]}, "tv_episode_parent")
        result[item["id"]] = row
    need(all(result[item["id"]]["parent_id"] == series["id"] for item in catalog["seasons"]), "tv_season_series_parent")
    return result


def validate_tv_dto(dto, row, rows, server_id, detail=False):
    need(isinstance(dto, dict) and dto.get("Id") == row["id"] and dto.get("Type") == row["type"] and
         dto.get("Name") == row["name"] and dto.get("ParentId") == row["parent_id"] and
         dto.get("ServerId") == server_id and dto.get("IsFolder") is row["is_folder"], "tv_response_identity")
    if detail:
        need(dto.get("Path") == row["path"], "tv_detail_path")
    else:
        need("Path" not in dto, "tv_default_query_included_path")
    expected = {}
    if row["type"] in ("Season", "Episode"):
        need(type(dto.get("IndexNumber")) is int and dto["IndexNumber"] == row["index_number"], "tv_response_index")
        parent = rows[row["parent_id"]]
        series = parent if row["type"] == "Season" else rows[parent["parent_id"]]
        expected.update(SeriesId=series["id"], SeriesName=series["name"])
        if row["type"] == "Episode":
            need(type(dto.get("ParentIndexNumber")) is int and dto["ParentIndexNumber"] == row["parent_index_number"], "tv_response_parent_index")
            expected.update(SeasonId=parent["id"], SeasonName=parent["name"])
    fields = {key: dto[key] for key in ("SeriesId", "SeriesName", "SeasonId", "SeasonName") if key in dto}
    need(fields == expected, "tv_parent_fields_missing_or_changed")


def validate_tv_inventory(value, wanted, rows, server_id):
    need(isinstance(value, dict) and isinstance(value.get("Items"), list) and
         type(value.get("TotalRecordCount")) is int and value["TotalRecordCount"] == len(wanted) == len(value["Items"]),
         "tv_response_inventory_size")
    actual = indexed(value["Items"], "Id")
    need(set(actual) == set(wanted), "tv_response_inventory_membership")
    for item_id, dto in actual.items():
        validate_tv_dto(dto, rows[item_id], rows, server_id)


def reconcile_tv_source(before, after, logins):
    """Only two owned Emby credentials, their devices, and four audit rows may change."""
    need(set(before["tables"]) == set(after["tables"]) and len(before["tables"]) == 35,
         "tv_source_table_inventory")
    allowed, added = {"sessions", "devices", "activity_entries"}, {}
    for table in set(before["tables"]) - allowed:
        need(canonical(before["tables"][table]) == canonical(after["tables"][table]), "tv_unowned_table_changed:" + table)
    for table in allowed:
        old, new = indexed(before["tables"][table]), indexed(after["tables"][table])
        need(set(old) <= set(new) and all(canonical(new[key]) == canonical(row) for key, row in old.items()), "tv_old_rows_changed:" + table)
        added[table] = {key: row for key, row in new.items() if key not in old}
    need(set(logins) == {"P", "Q"} and len(added["sessions"]) == len(added["devices"]) == 2 and
         len(added["activity_entries"]) == 4, "tv_unexpected_owned_delta")
    devices, expected_activity_rows = set(), []
    for login in logins.values():
        initial = login["initialSession"]
        row = added["sessions"].get(initial["id"])
        need(row is not None and row["kind"] == "emby" and login["logout401"] is True and row["revoked_at"] is not None and
             row["token_hash"] == "\\x" + sha(login["auth"]["token"].encode()) and
             set(initial) == set(row) and all(canonical(row[key]) == canonical(value) for key, value in initial.items() if key not in {"last_seen_at", "revoked_at"}) and
             instant(initial["last_seen_at"]) <= instant(row["last_seen_at"]) <= instant(after["capturedAt"]) and
             instant(before["capturedAt"]) <= instant(row["created_at"]) <= instant(row["revoked_at"]) <= instant(after["capturedAt"]),
             "tv_owned_session_not_exactly_revoked")
        original, device = login["initialDevice"], added["devices"].get(row["device_registry_id"])
        need(original is not None and device is not None and set(original) == set(device) and
             device["reported_device_id"] == login["deviceId"] and device["last_user_id"] == row["user_id"] and
             all(canonical(device[key]) == canonical(value) for key, value in original.items() if key != "last_seen_at") and
             instant(original["last_seen_at"]) <= instant(device["last_seen_at"]) <= instant(after["capturedAt"]),
             "tv_owned_device_changed")
        devices.add(device["id"])
        for action in ("session.login", "session.revoked"):
            expected_activity_rows.append((action, "emby", "user", row["user_id"], row["id"], "session", row["id"], ""))
    need(devices == set(added["devices"]), "tv_unowned_new_device")
    keys = ("action", "source", "actor_kind", "actor_id", "actor_credential_id", "resource_kind", "resource_id", "state")
    observed = []
    for row in added["activity_entries"].values():
        need(row["severity"] == "Info" and row["request_id"] == row["observation_fingerprint"] == "" and
             type(row["revision"]) is type(row["previous_revision"]) is type(row["affected_count"]) is int and
             row["revision"] == row["previous_revision"] == 0 and row["affected_count"] == 1 and row["changed_fields"] == [] and
             instant(before["capturedAt"]) <= instant(row["created_at"]) <= instant(after["capturedAt"]), "tv_activity_fields_changed")
        observed.append(tuple(row[key] for key in keys))
    need(Counter(observed) == Counter(expected_activity_rows), "tv_activity_identity_changed")
    need(set(before["sequences"]) == set(after["sequences"]), "tv_sequence_inventory_changed")
    for name, old in before["sequences"].items():
        new = after["sequences"][name]
        if name in {"devices_id_seq", "activity_entries_id_seq"}:
            table = "devices" if name == "devices_id_seq" else "activity_entries"
            first = decimal(old["lastValue"]) + (1 if old["isCalled"] else 0)
            issued = set(added[table])
            need(issued == set(range(first, first + len(issued))) and canonical(new) == canonical({"lastValue": str(max(issued)), "isCalled": True}),
                 "tv_sequence_not_exactly_owned")
        else:
            need(canonical(new) == canonical(old), "tv_unowned_sequence_changed")
    return {"preservedTables": 32, "oldRowsExact": True, "newSessions": 2, "newDevices": 2, "newActivityRows": 4,
            "sequencesExact": True, "allControllerSessionsRevoked": True}


class Admission:
    def __init__(self, io, helper, inspector, seed, catalog, *, epoch=None, binding=None, expected_process=None):
        self.io, self.helper, self.inspector, self.seed, self.catalog = io, helper, inspector, seed, catalog
        self.epoch, self.binding = epoch, binding
        self.expected_process = expected_process if epoch is not None else seed["processes"]["candidate"]
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

    def seed_authority(self):
        seed = self.seed
        need(seed["kind"] == "audited-candidate-seed-manifest" and seed["version"] == 1 and
             seed["status"] == "seeded_pending_live_acceptance" and seed["playbackRequests"] == 0 and
             len(seed["cleanup"]) == 2 and all(row["logoutAcknowledged"] and row["sameTokenRejected"] for row in seed["cleanup"]),
             "seed_candidate_or_cleanup_binding")
        if self.epoch is None:
            need(seed["candidateManifest"] == self.io.value["candidateManifest"] and
                 seed["runtimeInspection"] == self.io.value["runtimeInspection"] and seed["helper"] == self.io.value["seedHelper"] and
                 seed["source"] == {"manifestSha256": SOURCE_MANIFEST_SHA, "binarySha256": self.io.candidate["binary"]["sha256"], "schema": 28},
                 "seed_candidate_or_cleanup_binding")
            source_input = self.load(self.io.candidate["input"])
            need(source_input["sourceManifest"] == self.io.value["sourceManifest"], "candidate_source_manifest_cross_binding")
        else:
            need(self.io.epoch == self.epoch and self.io.binding == self.binding and self.io.candidate == self.epoch["candidate"],
                  "epoch_runtime_reader_cross_binding")

    def media_root(self):
        return self.helper.C / "data/media"

    def preflight(self):
        seed = self.seed
        self.seed_authority()
        need(self.expected_process == self.io.pin(), "current_candidate_process_changed")
        need(self.load(seed["catalogFile"]) == seed["catalog"], "seed_catalog_descriptor_changed")
        self.inspector.assert_target_cluster()
        self.inspector.database_facts("recovery")
        self.lease = self.inspector.deployment_lease()
        if self.epoch is not None:
            need(self.lease == self.epoch["lease"], "epoch_deployment_lease_changed")
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
            validate_actor_policy(role, user)
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
            validate_actual_dto(dto, row, seed)
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
             self.overview["Transcoding"] == {"Configured": True, "Available": True, "Reason": "ready"}, "native_overview_admission")
        info = self.req("public-info", "GET", "/emby/System/Info/Public")["body"]
        need(info["Id"] == self.server_id and info["StartupWizardCompleted"] is True and
             info["LocalAddress"] == self.io.candidate["publicUrl"], "public_candidate_identity")
        self.status_before = validate_status(self.req("backup-status", "GET", BACKUPS + "/status", role="admin")["body"])
        validate_effective_backup_limits(self.status_before, self.epoch)
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
            need(path.is_relative_to(self.media_root()) and row["bytes"] <= 256 * MIB,
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
        need(storage["Configured"] is True and storage["Items"] == [{"Path": str(self.media_root()), "Available": True}],
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
                    self.failures.append(safe_failure(error, "emergency_cancel"))
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
                self.failures.append(safe_failure(error, "logout_" + role))

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
        need(self.inspector.deployment_lease() == self.lease and self.io.pin() == self.expected_process, "final_process_or_lease_changed")
        resources = self.resource_counters("resources-after")
        for filename, keys in (("memory.events", ("max", "oom", "oom_kill", "oom_group_kill")), ("pids.events", ("max",))):
            for key in keys:
                need(resources["events"][filename].get(key, "0") == self.counters_before["events"][filename].get(key, "0"),
                     "candidate_resource_limit_event")
        need(resources["unit"]["NRestarts"] == self.counters_before["unit"]["NRestarts"], "candidate_restarted")
        self.helper.read_checked(self.io.candidate["binary"]["path"], self.io.candidate["binary"]["sha256"])
        return proof


class FreshEmbeddedAdmission(Admission):
    """Run the full sequence and nine TV reads against the new embedded actor."""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.stage_caps["tvParents"] = 9
        self.fresh_tv_complete = False

    def seed_authority(self):
        self.initial_inspection = self.load(self.io.value["runtimeInspection"])
        self.fresh_root = validate_fresh_embedded_authority(
            self.io.value, self.io.candidate, self.load(self.io.candidate["input"]), self.seed, self.initial_inspection)
        need(self.io.provision.root == self.fresh_root, "fresh_io_root_changed")

    def media_root(self):
        return Path(self.io.candidate["dataDirectory"]) / "media"

    def preflight(self):
        super().preflight()
        self.tv = tv_rows(self.seed, self.before)
        seasons = [row for row in self.seed["catalog"]["seasons"] if row["indexNumber"] == 2]
        episodes = [row for row in self.seed["catalog"]["episodes"] if (row["parentIndexNumber"], row["indexNumber"]) == (2, 1)]
        need(len(seasons) == len(episodes) == 1, "fresh_tv_detail_fixture_membership")
        self.detail_ids = [seasons[0]["id"], episodes[0]["id"]]

    def authenticate(self):
        super().authenticate()
        validate_fresh_backup_status(self.status_before, self.old_backups, self.old_operations)

    def storage(self):
        super().storage()
        self.phase = "tvParents"
        p, q = (self.credentials[role]["actorId"] for role in ("P", "Q"))
        catalog = self.seed["catalog"]
        series = catalog["series"]["id"]
        for label, route, wanted in (
                ("default-seasons", "/emby/Shows/" + series + "/Seasons?" + urlencode({"UserId": p}), [row["id"] for row in catalog["seasons"]]),
                ("default-episodes", "/emby/Shows/" + series + "/Episodes?" + urlencode({"UserId": p}), [row["id"] for row in catalog["episodes"]]),
                ("default-tv", "/emby/Users/" + p + "/Items?" + urlencode({"ParentId": self.seed["libraries"]["TV"]["Id"], "Recursive": "true", "Limit": 100}), list(self.tv))):
            validate_tv_inventory(self.req(label, "GET", route, role="P")["body"], wanted, self.tv, self.server_id)
        for index, item_id in enumerate(self.detail_ids):
            dto = self.req("detail-p-%d" % index, "GET", "/emby/Users/" + p + "/Items/" + item_id, role="P")["body"]
            validate_tv_dto(dto, self.tv[item_id], self.tv, self.server_id, detail=True)
        invisible = self.req("invisible-q-list", "GET", "/emby/Users/" + q + "/Items?" +
                             urlencode({"Ids": ",".join(self.detail_ids), "Limit": 100}), role="Q")["body"]
        need(self.helper.items(invisible) == [], "fresh_tv_control_list_exposed_items")
        for index, item_id in enumerate(self.detail_ids):
            response = self.req("invisible-q-detail-%d" % index, "GET", "/emby/Users/" + q + "/Items/" + item_id,
                                role="Q", expected=(404,))
            need(response["body"]["ResponseStatus"]["ErrorCode"] == "not_found", "fresh_tv_control_detail_error")
        denied = self.req("cross-user-q-detail", "GET", "/emby/Users/" + p + "/Items/" + self.detail_ids[1],
                          role="Q", expected=(403,))
        need(denied["body"]["ResponseStatus"]["ErrorCode"] == "access_denied", "fresh_tv_cross_user_authorization_error")
        need(self.phase_counts["tvParents"] == 9, "fresh_tv_request_count")
        self.fresh_tv_complete = True

    def reconcile(self):
        need(self.fresh_tv_complete, "fresh_tv_checks_incomplete")
        return super().reconcile()

    def bind_reader_evidence(self):
        reader = self.inspector
        need(reader is not None and reader.runtime_context is True and reader.private is None and
             reader.active_command is None and not reader.command_records and not reader.command_failure_raw and
             reader.sql_sessions == 0, "fresh_runtime_reader_already_used")
        self.helper.safe_path(self.io.private, (0,), directory=True)
        directory = self.io.private / "runtime-inspection"
        directory.mkdir(mode=0o700)
        reader.private = directory
        self.helper.sync_dir(self.io.private)

    def close_reader(self, complete):
        if self.inspector is None:
            return
        reader = self.inspector
        close_failure = None
        try:
            reader.close()
        except Exception as error:
            close_failure = safe_failure(error, "runtime_reader_close")
        retention_errors = []

        def retain(name, raw):
            try:
                return self.helper.write_once(self.io.private / name, raw)
            except Exception as error:
                retention_errors.append({"name": name, "type": type(error).__name__, "code": "runtime_reader_retention_failed"})
                return None

        raw_pins = [retain("runtime-sql-%02d.raw" % (index + 1), raw) for index, raw in enumerate(reader.sql_raw)]
        command_raw = []
        for row in reader.command_failure_raw:
            ordinal = row["ordinal"]
            need(type(ordinal) is int and 1 <= ordinal <= 65536, "fresh_runtime_command_ordinal")
            command_raw.append({"ordinal": ordinal,
                "stdout": retain("runtime-command-%04d-stdout.raw" % ordinal, row["stdout"]),
                "stderr": retain("runtime-command-%04d-stderr.raw" % ordinal, row["stderr"])})
        self.evidence["runtimeReader"] = self.save("runtime-reader", {"helper": self.io.value["inspectionHelper"],
            "private": str(reader.private) if reader.private is not None else None,
            "sqlSessions": reader.sql_sessions, "records": reader.sql_records, "raw": raw_pins,
            "commands": reader.command_records, "commandFailureRaw": command_raw,
            "httpRequests": reader.public_requests, "activeCommandClosed": reader.active_command is None,
            "closeFailure": close_failure, "retentionErrors": retention_errors})
        need(close_failure is None and reader.active_command is None, "fresh_runtime_reader_unclosed")
        need(not retention_errors, "fresh_runtime_reader_retention_incomplete")
        if complete:
            need(reader.private == self.io.private / "runtime-inspection" and
                 reader.sql_sessions == len(raw_pins) == len(reader.sql_records) and
                 reader.public_requests == 0 and all(
                     all(row.get(key) is True for key in ("readOnly", "frontendGroupClosed", "backendGone", "commitAcknowledged")) and
                     row["stdoutSha256"] == sha(raw) for row, raw in zip(reader.sql_records, reader.sql_raw)) and
                 not reader.command_failure_raw and reader.command_records and
                 [row.get("ordinal") for row in reader.command_records] == list(range(1, len(reader.command_records) + 1)) and
                 all(row.get("processCreated") is True and type(row.get("pid")) is int and row["pid"] > 1 and
                     row.get("processGroup") == row["pid"] and row.get("stdinComplete") is True and
                     type(row.get("exitCode")) is int and row["exitCode"] == 0 and row.get("frontendGroupClosed") is True and
                     row.get("failure") is None and row.get("cleanupFailure") is None and row.get("retentionErrors") == [] and
                     row["stdout"]["complete"] is True and row["stderr"]["complete"] is True for row in reader.command_records),
                 "fresh_runtime_reader_evidence_incomplete")


class TVParentAdmission(Admission):
    """One affected TV read sequence; no native administrator or recovery mutation."""
    def __init__(self, *args, baseline, control_reader=None, **kwargs):
        super().__init__(*args, **kwargs)
        self.baseline = baseline
        self.control_reader = control_reader
        self.fresh_checks = dict.fromkeys(TV_CHECKS, False)
        self.stage_caps = {"tv": 14}
        original_deadline = self.io.deadline
        def bounded_deadline(cleanup=False):
            left = original_deadline(cleanup)
            if cleanup and self.cleanup_end is not None:
                left = min(left, self.cleanup_end - time.monotonic())
                need(left > 0, "tv_cleanup_deadline_expired")
            return left
        self.io.deadline = bounded_deadline

    def capture_controls(self, label):
        need(self.control_reader is not None, "tv_control_reader_unavailable")
        result = self.control_reader()
        self.evidence[label] = self.save(label, result)
        return result

    def preflight(self):
        seed = self.seed
        need(seed["kind"] == "audited-candidate-seed-manifest" and seed["version"] == 1 and
             seed["status"] == "seeded_pending_live_acceptance" and seed["playbackRequests"] == 0 and
             len(seed["cleanup"]) == 2 and all(row["logoutAcknowledged"] and row["sameTokenRejected"] for row in seed["cleanup"]),
             "tv_original_seed_cleanup")
        need(self.io.epoch == self.epoch and self.io.binding == self.binding and self.io.candidate == self.epoch["candidate"] and
             self.io.pin() == self.expected_process and self.load(seed["catalogFile"]) == seed["catalog"], "tv_current_runtime_binding")
        self.inspector.assert_target_cluster()
        for slot in ("source", "recovery"):
            self.inspector.database_facts(slot)
        self.lease = self.inspector.deployment_lease()
        need(self.lease == self.epoch["lease"], "tv_current_lease_changed")
        self.before = self.snapshot("source-before")
        self.inactive_before = self.snapshot("inactive-before", target=True)
        tables = {row["Name"] for row in self.catalog["catalog"]["Tables"]}
        sequences = {row["Name"] for row in self.catalog["catalog"]["Sequences"]}
        need(len(tables) == 35 and all(set(row["tables"]) == tables and set(row["sequences"]) == sequences for row in
             (self.before, self.inactive_before, self.baseline["source"], self.baseline["inactiveStage"])), "tv_baseline_catalog_inventory")
        same_snapshot(self.baseline["source"], self.before, "tv_source_changed_since_transition")
        same_snapshot(self.baseline["inactiveStage"], self.inactive_before, "tv_inactive_changed_since_transition")
        self.server_id = seed["serverId"]
        need(HEX32.fullmatch(self.server_id), "tv_server_identity")
        users = indexed(self.before["tables"]["users"])
        for role, actor in (("P", seed["actors"]["tv-browse"]), ("Q", seed["controlQ"])):
            credential = self.load(actor["credentials"])
            need(set(credential) == {"actorId", "serverId", "username", "password"} and credential["actorId"] == actor["id"] and
                 credential["serverId"] == self.server_id and credential["username"] == actor["username"], "tv_credential_binding")
            user = users[actor["id"]]
            need(user["name"] == actor["username"] and user["is_disabled"] is False and user["is_administrator"] is False,
                 "tv_ordinary_actor_authority")
            validate_actor_policy(role, user)
            self.credentials[role] = credential
        need(self.credentials["P"]["actorId"] != self.credentials["Q"]["actorId"], "tv_actor_collision")
        self.tv = tv_rows(seed, self.before)
        seasons = [row for row in seed["catalog"]["seasons"] if row["indexNumber"] == 2]
        episodes = [row for row in seed["catalog"]["episodes"] if (row["parentIndexNumber"], row["indexNumber"]) == (2, 1)]
        need(len(seasons) == len(episodes) == 1, "tv_detail_fixture_membership")
        self.detail_ids = [seasons[0]["id"], episodes[0]["id"]]
        self.fixtures_before = self.fixture_files("fixtures-before")
        self.controls_before = self.capture_controls("controls-before")
        self.counters_before = self.resource_counters("resources-before")
        self.fresh_checks["runtimeIdentity"] = True
        self.started = time.monotonic()
        self.health_due()

    def health_due(self):
        if self.lease is None or len(self.samples) == 3:
            return
        due = self.started + len(self.samples) * 30
        if time.monotonic() < due:
            return
        need(time.monotonic() - due <= 10, "tv_health_sample_deadline_missed")
        if not self.samples:
            self.started = time.monotonic()
        sample = {"index": len(self.samples), "elapsedMilliseconds": round((time.monotonic() - self.started) * 1000), "responses": []}
        if not self.samples:
            sample["elapsedMilliseconds"] = 0
        for route, wanted in (("/healthz", {"Status": "ok"}), ("/readyz", {"Status": "ready"})):
            response = self.req("stability-" + route[1:], "GET", route, health=True, maximum=65536)
            need(response["body"] == wanted, "tv_candidate_not_healthy_and_ready")
            sample["responses"].append(response["receipt"])
        need(self.inspector.deployment_lease() == self.lease and self.io.pin() == self.expected_process, "tv_health_runtime_changed")
        self.samples.append(sample)
        self.evidence["stability-%02d" % sample["index"]] = self.save("stability-%02d" % sample["index"], sample)

    def run(self):
        self.preflight()
        self.phase = "tv"
        public = self.req("public-info", "GET", "/emby/System/Info/Public")["body"]
        need(public["Id"] == self.server_id and public["StartupWizardCompleted"] is True and
             public["LocalAddress"] == self.io.candidate["publicUrl"], "tv_public_runtime_identity")
        for role in ("P", "Q"):
            self.login(role)
        p, q = (self.credentials[role]["actorId"] for role in ("P", "Q"))
        for role, actor in (("P", p), ("Q", q)):
            items = self.helper.items(self.req("views-" + role.lower(), "GET", "/emby/Users/" + actor + "/Views", role=role)["body"])
            expected = {row["Id"] for row in self.seed["libraries"].values()} if role == "P" else set()
            need(len(items) == len(expected) and {row["Id"] for row in items} == expected and
                 all(row["Type"] == "CollectionFolder" and row["ServerId"] == self.server_id for row in items), "tv_visible_libraries_changed")
        catalog = self.seed["catalog"]
        series = catalog["series"]["id"]
        for label, route, wanted in (
                ("default-seasons", "/emby/Shows/" + series + "/Seasons?" + urlencode({"UserId": p}), [row["id"] for row in catalog["seasons"]]),
                ("default-episodes", "/emby/Shows/" + series + "/Episodes?" + urlencode({"UserId": p}), [row["id"] for row in catalog["episodes"]]),
                ("default-tv", "/emby/Users/" + p + "/Items?" + urlencode({"ParentId": self.seed["libraries"]["TV"]["Id"], "Recursive": "true", "Limit": 100}), list(self.tv))):
            validate_tv_inventory(self.req(label, "GET", route, role="P")["body"], wanted, self.tv, self.server_id)
        self.fresh_checks["tvDefaultParents"] = True
        details = self.detail_ids
        for index, item_id in enumerate(details):
            dto = self.req("detail-p-%d" % index, "GET", "/emby/Users/" + p + "/Items/" + item_id, role="P")["body"]
            validate_tv_dto(dto, self.tv[item_id], self.tv, self.server_id, detail=True)
        self.fresh_checks["tvDetailParents"] = True
        invisible = self.req("invisible-q-list", "GET", "/emby/Users/" + q + "/Items?" + urlencode({"Ids": ",".join(details), "Limit": 100}), role="Q")["body"]
        need(self.helper.items(invisible) == [], "tv_control_list_exposed_items")
        for index, item_id in enumerate(details):
            response = self.req("invisible-q-detail-%d" % index, "GET", "/emby/Users/" + q + "/Items/" + item_id, role="Q", expected=(404,))
            need(response["body"]["ResponseStatus"]["ErrorCode"] == "not_found", "tv_control_detail_error")
        denied = self.req("cross-user-q-detail", "GET", "/emby/Users/" + p + "/Items/" + details[1], role="Q", expected=(403,))
        need(denied["body"]["ResponseStatus"]["ErrorCode"] == "access_denied", "tv_cross_user_authorization_error")
        self.fresh_checks["ordinaryAuthorization"] = True
        while len(self.samples) < 3:
            self.wait(min(1, max(0.01, self.started + len(self.samples) * 30 - time.monotonic())))
        need(self.samples[-1]["elapsedMilliseconds"] - self.samples[0]["elapsedMilliseconds"] >= 60000 and
             self.io.requests == {"normal": 20, "cleanup": 0}, "tv_health_window_or_request_count")
        self.fresh_checks["healthWindow60Seconds"] = True

    def close_sessions(self):
        self.cleanup_end = min(time.monotonic() + 60, self.io.started + 180)
        for role in reversed(list(self.logins)):
            login = self.logins[role]
            try:
                need(role in {"P", "Q"} and login["auth"]["kind"] == "emby", "tv_cleanup_unowned_authority")
                if "initialSession" not in login:
                    self.bind_login(role)
                response = self.req("logout-" + role.lower(), "POST", "/emby/Sessions/Logout", role=role, expected=(204,), cleanup=True)
                denied = self.req("logout-proof-" + role.lower(), "GET", "/emby/System/Info", role=role, expected=(401,), cleanup=True)
                need(denied["raw"] == b"Access token is invalid or expired.", "tv_same_token_not_rejected")
                login["logout401"] = True
                self.evidence["logout-" + role.lower()] = self.save("logout-" + role.lower(), {
                    "credentialId": login["initialSession"]["id"], "tokenSha256": sha(login["auth"]["token"].encode()),
                    "logoutResponse": response["receipt"], "sameToken401": denied["receipt"]})
            except Exception as error:
                self.failures.append(safe_failure(error, "logout_" + role))
        self.fresh_checks["sessionCleanup"] = set(self.logins) == {"P", "Q"} and all(row["logout401"] for row in self.logins.values()) and not self.failures

    def reconcile(self):
        after = self.snapshot("source-after")
        inactive = self.snapshot("inactive-after", target=True)
        proof = reconcile_tv_source(self.before, after, self.logins)
        same_snapshot(self.inactive_before, inactive, "tv_inactive_stage_changed")
        need(self.fixture_files("fixtures-after") == self.fixtures_before, "tv_fixture_bytes_changed")
        need(self.inspector.deployment_lease() == self.lease and self.io.pin() == self.expected_process, "tv_final_runtime_changed")
        resources = self.resource_counters("resources-after")
        for filename, keys in (("memory.events", ("max", "oom", "oom_kill", "oom_group_kill")), ("pids.events", ("max",))):
            for key in keys:
                need(resources["events"][filename].get(key, "0") == self.counters_before["events"][filename].get(key, "0"), "tv_resource_limit_event")
        need(resources["unit"]["NRestarts"] == self.counters_before["unit"]["NRestarts"], "tv_candidate_restarted")
        self.helper.read_checked(self.io.candidate["binary"]["path"], self.io.candidate["binary"]["sha256"])
        validate_tv_controls(self.controls_before, self.capture_controls("controls-after"))
        self.fresh_checks["sourceAndInactivePreserved"] = True
        return {**proof, "inactiveTablesExact": 35, "inactiveSequencesExact": True, "oldPlaybackAndUserDataExact": True,
                "controlFilesExact": True, "configurationExact": True, "hostingExact": True}


class ProgramsAdmission(TVParentAdmission):
    """Admit the fixed empty-program query without playback or administrator mutations."""
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.fresh_checks = dict.fromkeys(PROGRAMS_CHECKS, False)
        self.stage_caps = {"programs": 10}

    def run(self):
        self.preflight()
        self.phase = "programs"
        public = self.req("public-info", "GET", "/emby/System/Info/Public")["body"]
        need(public["Id"] == self.server_id and public["StartupWizardCompleted"] is True and
             public["LocalAddress"] == self.io.candidate["publicUrl"], "programs_public_runtime_identity")
        for role in ("P", "Q"):
            self.login(role)
        p, q = (self.credentials[role]["actorId"] for role in ("P", "Q"))
        for role, actor in (("P", p), ("Q", q)):
            items = self.helper.items(self.req("views-" + role.lower(), "GET", "/emby/Users/" + actor + "/Views", role=role)["body"])
            expected = {row["Id"] for row in self.seed["libraries"].values()} if role == "P" else set()
            need(len(items) == len(expected) and {row["Id"] for row in items} == expected and
                 all(row["Type"] == "CollectionFolder" and row["ServerId"] == self.server_id for row in items),
                 "programs_visible_libraries_changed")
        series = self.seed["catalog"]["series"]["id"]
        def route(actor, series_id=None):
            query = {"UserId": actor, **PROGRAMS_QUERY}
            if series_id is not None:
                query["LibrarySeriesId"] = series_id
            return "/emby/LiveTv/Programs?" + urlencode(query)
        for label, selected_series, check in (("programs-empty", None, "programsEmptyQuery"),
                                               ("programs-series", series, "programsSeriesQuery")):
            response = self.req(label, "GET", route(p, selected_series), role="P")
            validate_programs_empty(response)
            self.evidence[label] = response["receipt"]
            self.fresh_checks[check] = True
        for label, actor, selected_series, role, status, error_code in (
                ("programs-hidden-series", q, series, "Q", 404, "not_found"),
                ("programs-cross-user", p, series, "Q", 403, "access_denied"),
                ("programs-non-series", p, self.detail_ids[1], "P", 400, "invalid_input")):
            response = self.req(label, "GET", route(actor, selected_series), role=role, expected=(status,))
            need(response["body"]["ResponseStatus"]["ErrorCode"] == error_code, "programs_authorization_or_type_error")
            self.evidence[label] = response["receipt"]
        self.fresh_checks["ordinaryAuthorization"] = True
        while len(self.samples) < 3:
            self.wait(min(1, max(0.01, self.started + len(self.samples) * 30 - time.monotonic())))
        need(self.samples[-1]["elapsedMilliseconds"] - self.samples[0]["elapsedMilliseconds"] >= 60000 and
             self.io.requests == {"normal": 16, "cleanup": 0}, "programs_health_window_or_request_count")
        self.fresh_checks["healthWindow60Seconds"] = True


def run_tv_parent_admission(value, input_pin, run_started, *, programs=False):
    runtime_pin = value["runtimeHelper"]
    runtime = import_bytes("frozen_tv_admission_runtime", runtime_pin["path"], initial_read(runtime_pin["path"], runtime_pin["sha256"]))
    read = lambda pin: parse(initial_read(pin["path"], pin["sha256"]))
    epoch = runtime.validate_epoch(read(value["runtimeEpoch"]))
    lineage = runtime.resolve_epoch_lineage(epoch, read)
    transition = (runtime.validate_programs_successor_input(lineage["productInput"]) if programs else
                  runtime.validate_binary_successor_input(lineage["productInput"]))
    modules = {key: runtime.load_helper(key, pin) for key, pin in epoch["helpers"].items()}
    helper = modules["seed"]
    helper.read_checked(__file__, value["admissionHelper"]["sha256"])
    seed, binding = read(epoch["seedProvenance"]), read(value["seedRuntimeBinding"])
    runtime.validate_seed_runtime_binding(binding, value["runtimeEpoch"], epoch, seed)
    reuse_key = "reusedAdmission05" if programs else "reusedAdmission04"
    reused, previous, closeout = read(value[reuse_key]), read(epoch["previousEpoch"]), read(value["transitionCloseout"])
    if programs:
        # The v4 lineage reader validates the actual artifact and complete transition preservation.
        older = read(TV_REUSED)
        expected = validate_programs_authority(value, epoch, binding, seed, transition, previous, reused, older, closeout)
    else:
        full = read(epoch["currentSource"]["fullReport"])
        runtime.validate_successor_full_report(full, full["worker"], transition)
        expected = validate_tv_authority(value, epoch, binding, seed, transition, previous, reused, closeout)
    catalog = read(value["compiledCatalog"])
    validate_catalog(catalog, read(epoch["currentSource"]["sourceManifest"]), value["compiledCatalog"])
    io = runtime.EpochIO(value, input_pin, value["admissionHelper"], value["runtimeEpoch"], value["seedRuntimeBinding"], modules)
    io.started = run_started
    baseline = read(epoch["after"])
    if programs:
        source_after = read(epoch["sourceAfter"])
        need(canonical(source_after) == canonical(baseline["source"]), "programs_transition_source_after_mismatch")
        baseline = {**baseline, "source": source_after}
    job_type = ProgramsAdmission if programs else TVParentAdmission
    job = job_type(io, helper, None, seed, catalog, epoch=epoch, binding=binding, expected_process=expected, baseline=baseline)
    transition_pin = epoch["transitionHelper"]
    transition_reader = import_bytes("frozen_tv_control_readers", transition_pin["path"], initial_read(transition_pin["path"], transition_pin["sha256"]))
    if programs:
        need(isinstance(getattr(transition_reader, "ProgramsSuccessor", None), type) and
             callable(getattr(getattr(transition_reader, "ProgramsSuccessor", None), "file", None)) and
             callable(getattr(getattr(transition_reader, "ProgramsSuccessor", None), "tree", None)),
             "programs_metadata_control_readers_required")
    job.control_reader = lambda: capture_tv_controls(runtime, transition_reader, io, job.baseline,
                                                    lambda: job.remaining(job.cleanup_end is not None), programs=programs)
    def expired(_number, _frame):
        raise AdmissionError("absolute_tv_admission_deadline")
    for number in ((signal.SIGALRM, signal.SIGTERM, signal.SIGINT, signal.SIGHUP) if programs else
                   (signal.SIGALRM, signal.SIGTERM, signal.SIGINT)):
        signal.signal(number, expired)
    signal.setitimer(signal.ITIMER_REAL, max(0.001, 180 - (time.monotonic() - run_started)))
    failure, proof, cleanup_interrupted = None, None, False
    try:
        io.open()
        job.inspector = io.reader
        job.run()
    except Exception as error:
        failure = safe_failure(error, job.phase)
    finally:
        if io.created:
            if programs:
                try:
                    job.close_sessions()
                except Exception as error:
                    cleanup_interrupted = True
                    cleanup_failure = safe_failure(error, "session_cleanup")
                    job.failures.append(cleanup_failure)
                    if failure is None:
                        failure = cleanup_failure
            else:
                job.close_sessions()
    if failure is None and not job.failures:
        try:
            proof = job.reconcile()
        except Exception as error:
            failure = safe_failure(error, "reconciliation")
    elif io.created and job.inspector is not None and not cleanup_interrupted:
        try:
            job.snapshot("source-after-failure")
            job.snapshot("inactive-after-failure", target=True)
        except Exception as error:
            job.failures.append(safe_failure(error, "failure_state_capture"))
    successful = failure is None and not job.failures and proof is not None and all(job.fresh_checks.values())
    if programs and successful:
        successful = io.requests == {"normal": 16, "cleanup": 4}
        if not successful:
            failure = {"type": "AdmissionError", "stage": "reconciliation", "code": "programs_final_request_count"}
    result = {"kind": "audited-candidate-live-admission", "version": 5 if programs else 3,
              "admissionKind": "affected_programs" if programs else "affected_tv_parent",
              "status": "admitted_for_core_client" if successful else "admission_failed_resources_retained",
              "input": input_pin, "helper": value["admissionHelper"], "runtimeHelper": runtime_pin,
              "runtimeEpoch": value["runtimeEpoch"], "seedRuntimeBinding": value["seedRuntimeBinding"],
              "currentSource": epoch["currentSource"], "originalProvision": epoch["originalProvision"],
              "originalSeed": binding["originalSeed"], "seedExecutor": binding["seedExecutor"],
              "transitionCloseout": value["transitionCloseout"], "freshChecks": job.fresh_checks,
              reuse_key: {"report": value[reuse_key], "runtimeEpoch": reused["runtimeEpoch"],
                  "seedRuntimeBinding": reused["seedRuntimeBinding"], "currentSource": reused["currentSource"],
                  "contracts": PROGRAMS_REUSED_CONTRACTS if programs else TV_REUSED_CONTRACTS},
              "failure": failure, "cleanupFailures": job.failures, "budgets": TV_LIMITS, "requests": io.requests,
              "phaseRequests": dict(job.phase_counts), "elapsedMilliseconds": round((time.monotonic() - run_started) * 1000),
              "stabilitySamples": job.samples, "evidence": job.evidence, "preservation": proof, "requestResponsibilities": io.request_states,
              "controllerSessions": {key: {"credentialId": row.get("initialSession", {}).get("id"),
                  "tokenSha256": sha(row["auth"]["token"].encode()), "sameTokenRejected": row["logout401"]} for key, row in job.logins.items()},
              "candidateAdmissionComplete": successful, "clientAcceptance": False, "playbackRequests": 0,
              "applyRequests": 0, "rollbackRequests": 0, "inactiveStageRetained": successful,
              "activeGenerationChanged": False if successful else None}
    if programs:
        result["reusedAdmission05"].update(freshChecks=reused["freshChecks"], reusedAdmission04=reused["reusedAdmission04"])
        for field, label in (("sourceBefore", "source-before"), ("sourceAfter", "source-after"),
                             ("inactiveBefore", "inactive-before"), ("inactiveAfter", "inactive-after"),
                             ("controlsBefore", "controls-before"), ("controlsAfter", "controls-after")):
            result[field] = job.evidence.get(label)
    summary = {"status": result["status"], "scope": str(io.output), "report": None, "candidateAdmissionComplete": successful,
               "clientAcceptance": False, "requests": io.requests, "elapsedMilliseconds": result["elapsedMilliseconds"],
               "failure": failure, "cleanupFailures": job.failures, "reportUnavailable": None}
    try:
        if io.created:
            summary["report"] = helper.write_json_once(io.private / "report.json", result)
            helper.write_json_once(io.output / "summary.json", summary)
    except Exception as error:
        successful = False
        summary.update(status="admission_report_unavailable_resources_retained", candidateAdmissionComplete=False,
                       reportUnavailable={"type": type(error).__name__, "code": "admission_report_publication_failed"})
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
    print(json.dumps(summary, sort_keys=True))
    return 0 if successful else 2


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
    need(value["admissionHelper"]["path"] == str(Path(__file__).absolute()), "admission_source_path_differs")
    if value["version"] == 5:
        return run_tv_parent_admission(value, input_pin, run_started, programs=True)
    if value["version"] == 3:
        return run_tv_parent_admission(value, input_pin, run_started)
    epoch, binding, expected_process = None, None, None
    if value["version"] in (1, 4):
        helper_pin = value["seedHelper"]
        helper = import_bytes("frozen_admission_seed", helper_pin["path"], initial_read(helper_pin["path"], helper_pin["sha256"]))
        inspector_pin = value["inspectionHelper"]
        inspector_module = import_bytes("frozen_admission_inspection", inspector_pin["path"], helper.read_checked(inspector_pin["path"], inspector_pin["sha256"]))
        seed = parse(helper.read_checked(**value["seedManifest"]))
        source_pin = value["sourceManifest"]
        io = helper.CandidateIO(value, input_pin, value["admissionHelper"], fresh=True) if value["version"] == 4 else helper.CandidateIO(value, input_pin, value["admissionHelper"])
        inspector = None if value["version"] == 4 else inspector_module.CandidateInspection()
        provenance = {"seed": value["seedManifest"], "candidateManifest": value["candidateManifest"]}
        if value["version"] == 4:
            provenance.update(admissionKind="fresh_embedded", runtimeInspection=value["runtimeInspection"],
                              seedExecutor=value["seedHelper"], inspectionHelper=value["inspectionHelper"],
                              runtimeHelper=value["runtimeHelper"], sourceManifest=source_pin,
                              previousLiveAdmissionsReused=False)
    else:
        runtime_pin = value["runtimeHelper"]
        runtime = import_bytes("frozen_admission_epoch", runtime_pin["path"], initial_read(runtime_pin["path"], runtime_pin["sha256"]))
        epoch = runtime.validate_epoch(parse(initial_read(value["runtimeEpoch"]["path"], value["runtimeEpoch"]["sha256"])))
        if epoch["version"] == 1:
            product_epoch = epoch
            transition = runtime.validate_transition_input(parse(initial_read(epoch["transitionInput"]["path"], epoch["transitionInput"]["sha256"])))
            configuration_input = None
        else:
            lineage = runtime.resolve_epoch_lineage(epoch, lambda pin: parse(initial_read(pin["path"], pin["sha256"])))
            product_epoch = lineage["productEpoch"]
            transition = runtime.validate_transition_input(lineage["productInput"])
            configuration_input = runtime.validate_environment_revision_input(lineage["configurationInput"])
        need(epoch["runtimeHelper"] == runtime_pin and epoch["helpers"] == transition["helpers"], "epoch_runtime_helper_cross_binding")
        modules = {key: runtime.load_helper(key, pin) for key, pin in epoch["helpers"].items()}
        helper = modules["seed"]
        binding = parse(helper.read_checked(**value["seedRuntimeBinding"]))
        need(binding["runtimeEpoch"] == value["runtimeEpoch"] and binding["originalSeed"] == epoch["seedProvenance"],
             "epoch_original_seed_descriptor_changed")
        seed = parse(helper.read_checked(**binding["originalSeed"]))
        runtime.validate_seed_runtime_binding(binding, value["runtimeEpoch"], epoch, seed)
        source_pin = epoch["currentSource"]["sourceManifest"]
        full_report = parse(helper.read_checked(**epoch["currentSource"]["fullReport"]))
        runtime.validate_full_report(full_report, full_report["worker"], {"newBinary": epoch["currentSource"]["binary"]})
        expected_process = validate_epoch_admission(value, epoch, binding, seed, full_report, transition,
                                                    product_epoch=product_epoch, configuration_input=configuration_input)
        io = runtime.EpochIO(value, input_pin, value["admissionHelper"], value["runtimeEpoch"], value["seedRuntimeBinding"], modules)
        inspector = None
        provenance = {"runtimeEpoch": value["runtimeEpoch"], "seedRuntimeBinding": value["seedRuntimeBinding"],
                      "runtimeHelper": runtime_pin, "originalProvision": epoch["originalProvision"],
                      "originalSeed": binding["originalSeed"], "seedExecutor": binding["seedExecutor"],
                      "currentSource": epoch["currentSource"]}
        if epoch["version"] == 2:
            provenance.update(previousEpoch=epoch["previousEpoch"], operationKind=epoch["operationKind"],
                              configurationChange=epoch["configurationChange"])
    helper.read_checked(__file__, value["admissionHelper"]["sha256"])
    catalog = parse(helper.read_checked(**value["compiledCatalog"]))
    source = parse(helper.read_checked(**source_pin))
    validate_catalog(catalog, source, value["compiledCatalog"])
    io.started = run_started
    job_type = FreshEmbeddedAdmission if value["version"] == 4 else Admission
    job = job_type(io, helper, inspector, seed, catalog, epoch=epoch, binding=binding, expected_process=expected_process)
    def expired(_number, _frame):
        raise AdmissionError("absolute_admission_deadline")
    signal.signal(signal.SIGALRM, expired)
    signal.signal(signal.SIGTERM, expired)
    signal.signal(signal.SIGINT, expired)
    signal.setitimer(signal.ITIMER_REAL, max(0.001, 900 - (time.monotonic() - run_started)))
    failure, proof = None, None
    try:
        io.open()
        if value["version"] == 4:
            job.seed_authority()
            inspection_input = job.load(seed["inspectionInput"])
            job.inspector = inspector_module.CandidateInspection(
                inspection_input, seed["inspectionInput"], value["inspectionHelper"], provision=io.provision,
                runtime_context=True, caller_deadline=lambda: job.remaining(job.cleanup_end is not None),
                runtime_budgets=dict(FRESH_INSPECTION_BUDGETS))
            job.bind_reader_evidence()
            provenance["currentSource"] = {"sourceManifest": source_pin, "compiledCatalog": value["compiledCatalog"],
                                           "binary": io.candidate["binary"], "productEvidence": io.candidate["productEvidence"],
                                           "archiveSha256": FRESH_PRODUCT_HASHES["sourceArchive"], "schema": 28}
        elif epoch is not None:
            job.inspector = io.reader
        job.run()
    except Exception as error:
        failure = safe_failure(error, job.phase)
    finally:
        if io.created:
            job.close_sessions()
    if failure is None and not job.failures:
        try:
            proof = job.reconcile()
        except Exception as error:
            failure = safe_failure(error, "reconciliation")
    if value["version"] == 4 and io.created:
        try:
            job.close_reader(failure is None and not job.failures and proof is not None)
        except Exception as error:
            job.failures.append(safe_failure(error, "runtime_reader_closeout"))
    successful = failure is None and not job.failures and proof is not None
    result = {"kind": "audited-candidate-live-admission", "version": value["version"], "status": "admitted_for_core_client" if successful else "admission_failed_resources_retained",
              "input": input_pin, "helper": value["admissionHelper"], **provenance,
              "failure": failure, "cleanupFailures": job.failures, "budgets": value["budgets"], "requests": io.requests, "phaseRequests": dict(job.phase_counts),
              "elapsedMilliseconds": round((time.monotonic() - io.started) * 1000), "stabilitySamples": job.samples,
              "operations": job.operations, "backup": job.backup, "evidence": job.evidence, "preservation": proof,
              "requestResponsibilities": io.request_states, "controllerSessions": {key: {"credentialId": row.get("initialSession", {}).get("id"),
                  "tokenSha256": sha(row["auth"]["token"].encode()), "sameTokenRejected": row["logout401"]} for key, row in job.logins.items()},
              "candidateAdmissionComplete": successful, "clientAcceptance": False, "playbackRequests": 0, "applyRequests": 0, "rollbackRequests": 0,
              "inactiveStageRetained": successful, "activeGenerationChanged": False if successful else None}
    if value["version"] == 4:
        result["freshEmbeddedChecks"] = {"tvParents": job.fresh_tv_complete,
            "tvParentReadRequests": job.phase_counts["tvParents"], "embeddedAssetsInspection": value["runtimeInspection"],
            "fullLiveSequenceComplete": successful, "initialInspectionAssetsReplayed": False,
            "independentRecoveryTargetValidated": successful, "reservedPublicGatewayProbed": False}
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
