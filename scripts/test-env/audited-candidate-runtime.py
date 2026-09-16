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
from copy import deepcopy
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
CURRENT_RUNTIME_KEYS = {"kind", "version", "status", "runtimeEpoch", "seedBinding", "admission", "admissionCloseout", "hosting", "recovery", "current", "preserved", "observation", "observationReview"}
CURRENT_RUNTIME_V2_KEYS = CURRENT_RUNTIME_KEYS | {"previousCurrentRuntime"}
CURRENT_RUNTIME_V1_PREDECESSOR = {"path": str(R / "core-current-runtime-20260915T085726Z/private/current-runtime-binding.json"), "sha256": "38b906d090cf1ae6cf1e6679d68e92772e60e4e5f396ef085b232983a35de60c"}
CURRENT_RUNTIME_V2_PREDECESSOR = {"path": "/opt/goby-test/candidate-lease-loss-recovery-20260915/current-runtime-01/private/current-runtime-binding.json", "sha256": "aed2914bc75663b51a9f4d923acc137cfe6a0926a901dd7fa0a7e64f42183de4"}
CURRENT_RUNTIME_SECOND_ROOT = Path("/opt/goby-test/candidate-oom-exit-recovery-20260916/original-restart-r01")
CURRENT_RUNTIME_SECOND_RECOVERY = {
    "execution": {"path": str(CURRENT_RUNTIME_SECOND_ROOT / "restart-execution.json"), "sha256": "cccb62597eb67dc5f54685e2f72ce1b1a0e48bd1632e5f85972d071838fce0dd"},
    "independentReview": {"path": str(CURRENT_RUNTIME_SECOND_ROOT / "restart-independent-review.json"), "sha256": "a2036bace081fbe10a78ae1d507710b25cc2b2ddd55a5ee37f66c0e84a5a701a"},
    "configuration": {"path": "/opt/goby-test/candidate-lease-loss-recovery-20260915/restart-configuration-observation.json", "sha256": "c86600904fe119838fa67d2543ad2fd41356b712ac81b80a78d4f158463d0cef"},
    "selectedStartIntent": {"path": str(CURRENT_RUNTIME_SECOND_ROOT / "restart-01/old/start-intent.json"), "sha256": "19de19dabd8307c446955a19f9fa15705419acf52559b25b086316974a0a4672"},
    "selectedResult": {"path": str(CURRENT_RUNTIME_SECOND_ROOT / "restart-01/old/result.json"), "sha256": "40575e5c875583129afd41dbf8f0bd376389b8c742cad6dfaf1a083c2e8e6677"},
}
CURRENT_IDENTITY_KEYS = {"candidateProcess", "serverProperties", "serverIdentity", "listener", "postgresProcess", "postgresProperties", "lease"}
CURRENT_RECOVERY_KEYS = {"execution", "independentReview", "configuration", "selectedStartIntent", "selectedResult"}
CURRENT_AUTHORITY_KEYS = ("runtimeEpoch", "seedBinding", "admission", "admissionCloseout", "hosting")
CURRENT_LEASE_KEYS = {"backendPid", "user", "database", "application", "clientHost", "clientPort", "backendStart", "mode", "granted", "candidateConnection"}
CURRENT_OBSERVATION_KEYS = {"kind", "version", "runtimeEpoch", "recoveryExecution", "hosting", "source", "verification", "capturedAt", "before", "after", "preserved", "hostingBefore", "hostingAfter", "leaseQueryResult", "calls"}
CURRENT_OBSERVATION_REVIEW_KEYS = {"kind", "version", "status", "observation", "source", "verification", "checks", "calls"}
CURRENT_OBSERVATION_REVIEW_CHECKS = {"recordPins", "sourceAndVerification", "currentIdentity", "recoveryBinding", "preservedHashes", "hostingContinuity", "uniqueLease", "privateEvidence"}
CURRENT_HOST_UNIT_FIELDS = "Id LoadState ActiveState SubState MainPID InvocationID Result ExecMainStatus ControlGroup NRestarts".split()
PROGRAMS_PREVIOUS_EPOCH = {"path": str(R / "candidate-tv-parent-transition-01/private/runtime-epoch.json"), "sha256": "76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac"}
PROGRAMS_PREVIOUS_BINDING = {"path": str(R / "candidate-tv-parent-transition-01/private/seed-runtime-binding.json"), "sha256": "94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43"}
PROGRAMS_CURRENT_RUNTIME = {"path": "/opt/goby-test/candidate-oom-exit-recovery-20260916/current-runtime-01/private/current-runtime-binding.json", "sha256": "db22e3d954febd07c422d143027876a94782d3937e0ab374f80d566ba283c4ef"}
PROGRAMS_PRIOR_SOURCE = {"path": str(R / "candidate-core-client-tv-browse-02/private/source-after.json"), "sha256": "445c35bc17a8716adb036061e16fa29f22d9883727657ebfe1cfc1fdd0db13e4"}
PROGRAMS_PRIOR_CLOSEOUT = {"path": str(R / "candidate-core-client-tv-browse-02/closeout/closeout.json"), "sha256": "5d666c204c4aa122414aa385928d6be6a4a153c8ce0390e4c9af2643fc19f9c3"}
PROGRAMS_SOURCE_ARCHIVE = {"path": str(R / "live-tv-product-source-20260915T102000Z/source-r01.tar.gz"), "sha256": "3c0e7e0de4e769f3bb2667d8b30cae1b62794c58e4d2b219b707cceb917ed251"}
PROGRAMS_PRODUCT_ROOT = Path("/opt/goby-test/livetv-programs-final-20260915")
PROGRAMS_SOURCE_MANIFEST = {"path": "/opt/goby-test/livetv-programs-focused-20260915/private/source-manifest.json", "sha256": "47b4f9130558377897228aaddff15dc02e279552b83c40922550264ffbf01845"}
PROGRAMS_COMPLETE_SOURCE_ROOT = Path("/opt/goby-test/livetv-programs-complete-source-final-20260916")
PROGRAMS_COMPLETE_PRODUCT_ROOT = Path("/opt/goby-test/livetv-programs-complete-source-final-20260916-r02")
PROGRAMS_COMPLETE_ARTIFACT_PINS = {
    "sourceArchive": {"path": str(PROGRAMS_COMPLETE_SOURCE_ROOT / "private/source.tar.gz"), "sha256": "f211b15d5e9675448f5de4d582e3d3898ad645937291edeeab0e658cbb91752b", "bytes": 33333070},
    "sourceManifest": {"path": str(PROGRAMS_COMPLETE_SOURCE_ROOT / "private/source-manifest.json"), "sha256": "fa0b3bf9642fa1a1a3face0bb580e94c901f3a8d6bfc378891d4233ae5dca4cc", "bytes": 1093188},
    "sourceBridge": {"path": str(PROGRAMS_COMPLETE_PRODUCT_ROOT / "private/build-source-bridge.json"), "sha256": "3a263670569054115ef0d6be1247059cfba54357ea3c7dcec8acbddd1200993d", "bytes": 1062677},
    "buildManifest": {"path": str(PROGRAMS_COMPLETE_PRODUCT_ROOT / "artifacts/linux-amd64-systemd/manifest.json"), "sha256": "54868306dce5a5a04348e2606bd047cf13af9d52c8db2a4ad23c5ffdb9ef6ed5", "bytes": 173965},
    "newBinary": {"path": str(PROGRAMS_COMPLETE_PRODUCT_ROOT / "artifacts/linux-amd64-systemd/goby"), "sha256": "ead67c8faaf4cde88f7fe1bf57ffed43705259aba7b29f59c3473747afd732fb", "bytes": 30701500},
    "packageManifest": {"path": str(PROGRAMS_COMPLETE_PRODUCT_ROOT / "artifacts/linux-amd64-systemd/package-manifest.json"), "sha256": "39566a074e499a714ca4190cea3a776458d7a4300396484627f9c69afdab415c", "bytes": 5998},
    "packageArchive": {"path": str(PROGRAMS_COMPLETE_PRODUCT_ROOT / "artifacts/linux-amd64-systemd/goby-linux-amd64-systemd.tar.gz"), "sha256": "c7e5d30bccf105003d9908ff18f18faf898bf83217c5ff2a6c4eda305c504a86", "bytes": 14078580},
}
PROGRAMS_COMPLETE_COUNTS = {"frozenFiles": 5405, "trackedBuildInputs": 5348, "generatedAssetCount": 57, "frozenBytes": 120705443}
PROGRAMS_COMPLETE_TRACKED_SCOPE = "All tracked project inputs, including documentation and fixtures; not a claim of compiled or emitted contribution."
PROGRAMS_COMPLETE_INVENTORY = {
    "boundary": "Snapshot of all regular files in the stated local scope, not a compiled dependency graph.",
    "digestEncoding": "UTF-8 JSON.stringify(files), sorted by repository-relative name.",
    "scope": ["cmd/", "internal/", "web/admin/embedded.go"], "fileCount": 861, "totalBytes": 12109709,
    "sha256": "9065d9e95d850c17e9d496c56a3af57a46f83879e2c23a3eb234867496b91a2a",
}
PROGRAMS_OLD_BINARY = "b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42"
PROGRAMS_E11_BINARY = "7a681218b74b16f60043c02c268f634282b9f94c8be252ecd0739f3a7995a2f1"
PROGRAMS_ENV_SHA = "877ce946814fef63a240171b7a60c4ad6b4505bf2051be815266ed9744627d3d"
PROGRAMS_INPUT_KEYS = SUCCESSOR_INPUT_KEYS | {"operationKind", "currentRuntime", "newSourceArchive", "newArtifactReceipt", "newBuildManifest", "sourceBridge", "recoveryPolicy"}
PROGRAMS_EPOCH_KEYS = SUCCESSOR_EPOCH_KEYS | {"previousCurrentRuntime", "sourceBefore", "sourceAfter"}
PROGRAMS_BINDING_KEYS = SUCCESSOR_BINDING_KEYS | {"previousCurrentRuntime"}
PROGRAMS_SOURCE_KEYS = {"archiveSha256", "sourceManifest", "binary", "fullReport", "schema", "artifactReceipt", "buildManifest", "sourceBridge"}
PROGRAMS_RECOVERY_POLICY = {"mode": "explicit_once_restore_predecessor", "automatic": False, "maximumAttempts": 1,
    "maximumSeconds": 900, "stopSeconds": 60, "readySeconds": 60, "maximumPublicRequests": 10,
    "refreshDefinitionChange": "disable_only_definition_added_by_this_transition"}
PROGRAMS_RECOVERY_INPUT_KEYS = {"kind", "version", "output", "transitionInput", "transitionFailure", "recoveryReview", "helpers", "budgets"}
PROGRAMS_CAPTURE_INPUT_KEYS = {"kind", "version", "operationKind", "output", "previousEpoch", "previousBinding", "currentRuntime", "priorSource", "priorCloseout", "retentionReview", "helpers"}
PROGRAMS_STAT_ONLY = {str(C / "data/master.key"), "/var/lib/goby-test/application-key-vault/master.key"}
PROGRAMS_PROTECTED = set(PROTECTED) | {"goby-audited-20260914T083143Z-9b73ad46f2e6-server.service", "goby-audited-20260914T083143Z-9b73ad46f2e6-postgres.service"}
PROGRAMS_REFRESH_FIELDS = {"key": "library.refresh_media", "emby_key": "", "name": "Refresh media details",
    "description": "Refresh media details in all registered libraries, including unchanged files.", "category": "Library",
    "is_hidden": False, "enabled": True, "revision": 1, "schedule_timezone": "UTC"}
PROGRAMS_REFRESH_KEYS = set(PROGRAMS_REFRESH_FIELDS) | {"id", "created_at", "updated_at"}
PROGRAMS_SEQUENCES = {"activity_entries_id_seq", "application_keys_id_seq", "catalog_entities_id_seq", "devices_id_seq", "theme_owner_ids_id_seq"}
PROGRAMS_STATE_KEYS = {"source", "inactiveStage", "databases", "trees", "controlDocuments", "fixedFiles", "diagnostics", "unitLogs", "loadedUnits", "protected",
    "candidateBefore", "candidateAfter", "postgresBefore", "postgresAfter", "leaseBefore", "leaseAfter", "hostingBefore", "hostingAfter",
    "previousEpoch", "seedBinding", "priorSource", "currentRuntime", "postgresProcess", "capturedAt", "databaseNow"}
PROGRAMS_RETENTION_MEMBERS = {
    "internal/config/observability.go": "4f26e6af24160c6d93ffc52a87725ac3bb88cd3148bdd603df5594c5d71acc7f",
    "internal/server/activity_retention.go": "88a2831005a3013fc4d88162a7e75606d3f6a87ee70d93b5b862c012fbe32180",
    "internal/activity/store.go": "4bbe56850faf3d60d61039a84582004c18a113c09f91b75484afc3de8fc16728"}


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
    if type(value.get("version")) is int and value["version"] == 3:
        return validate_programs_successor_input(value)
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


def programs_complete_source(archive):
    """Select only the two explicitly retained source archives."""
    descriptor(archive)
    complete = archive == receipt_descriptor(PROGRAMS_COMPLETE_ARTIFACT_PINS["sourceArchive"])
    need(complete or archive == PROGRAMS_SOURCE_ARCHIVE, "programs_source_archive_authority")
    return complete


def validate_programs_successor_input(value):
    need(isinstance(value, dict) and set(value) == PROGRAMS_INPUT_KEYS and
         value["kind"] == "audited-candidate-transition-input" and type(value["version"]) is int and value["version"] == 3 and
         value["operationKind"] == "programs_successor", "programs_transition_input_schema")
    for key in PROGRAMS_INPUT_KEYS - {"kind", "version", "operationKind", "output", "helpers", "budgets", "recoveryPolicy"}:
        descriptor(value[key])
    complete = programs_complete_source(value["newSourceArchive"])
    source_manifest = receipt_descriptor(PROGRAMS_COMPLETE_ARTIFACT_PINS["sourceManifest"]) if complete else PROGRAMS_SOURCE_MANIFEST
    expected = {"previousEpoch": PROGRAMS_PREVIOUS_EPOCH, "previousBinding": PROGRAMS_PREVIOUS_BINDING,
        "currentRuntime": PROGRAMS_CURRENT_RUNTIME, "priorSource": PROGRAMS_PRIOR_SOURCE,
        "priorCloseout": PROGRAMS_PRIOR_CLOSEOUT, "newSourceManifest": source_manifest}
    need(all(canonical(value[key]) == canonical(pin) for key, pin in expected.items()), "programs_transition_authority")
    need(set(value["helpers"]) == HELPERS and all(descriptor(pin) for pin in value["helpers"].values()) and
         value["helpers"]["gateway"]["sha256"] == GATEWAY_SOURCE_SHA, "programs_transition_helpers")
    need(canonical(value["budgets"]) == canonical(LIMITS) and canonical(value["recoveryPolicy"]) == canonical(PROGRAMS_RECOVERY_POLICY),
         "programs_transition_budget_or_recovery_policy")
    output = Path(value["output"])
    need(output.parent == R and re.fullmatch(r"candidate-programs-transition-[0-9]{2}", output.name), "programs_transition_scope")
    for key in ("newFullReport", "newBinary", "newArtifactReceipt", "newBuildManifest", "sourceBridge"):
        need(Path(value[key]["path"]).is_relative_to(PROGRAMS_COMPLETE_PRODUCT_ROOT if complete else PROGRAMS_PRODUCT_ROOT), "programs_product_scope")
    if complete:
        for key, target in (("newBinary", "newBinary"), ("newBuildManifest", "buildManifest"), ("sourceBridge", "sourceBridge")):
            need(value[key] == receipt_descriptor(PROGRAMS_COMPLETE_ARTIFACT_PINS[target]), "programs_complete_artifact_input")
    need(Path(value["compiledCatalog"]["path"]).is_relative_to("/opt/goby-test") and
         value["compiledCatalog"]["path"].endswith("/" + CATALOG_RELATIVE), "programs_source_resource_scope")
    need(value["newBinary"]["sha256"] not in {OLD_BINARY, CURRENT_BINARY, PROGRAMS_OLD_BINARY, PROGRAMS_E11_BINARY}, "programs_product_is_historical_binary")
    return value


def validate_programs_capture_input(value):
    need(isinstance(value, dict) and set(value) == PROGRAMS_CAPTURE_INPUT_KEYS and
         value["kind"] == "audited-candidate-state-capture-input" and type(value["version"]) is int and value["version"] == 2 and
         value["operationKind"] == "programs_successor", "programs_capture_input_schema")
    for key in PROGRAMS_CAPTURE_INPUT_KEYS - {"kind", "version", "operationKind", "output", "helpers"}:
        descriptor(value[key])
    for key, pin in (("previousEpoch", PROGRAMS_PREVIOUS_EPOCH), ("previousBinding", PROGRAMS_PREVIOUS_BINDING),
                     ("currentRuntime", PROGRAMS_CURRENT_RUNTIME), ("priorSource", PROGRAMS_PRIOR_SOURCE), ("priorCloseout", PROGRAMS_PRIOR_CLOSEOUT)):
        need(value[key] == pin, "programs_capture_authority")
    output = Path(value["output"])
    need(output.parent == R and re.fullmatch(r"candidate-programs-state-capture-[0-9]{2}", output.name) and
         set(value["helpers"]) == HELPERS and all(descriptor(pin) for pin in value["helpers"].values()) and
         value["helpers"]["gateway"]["sha256"] == GATEWAY_SOURCE_SHA, "programs_capture_scope")
    return value


def programs_instant(value, code="programs_timestamp"):
    try:
        need(isinstance(value, str), code)
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        need(parsed.tzinfo is not None, code)
        return parsed
    except (TypeError, ValueError) as error:
        raise ContractError(code) from error


def programs_sessions(source):
    rows = source["tables"]["sessions"]
    need(isinstance(rows, list) and 0 < len(rows) <= 64, "programs_session_inventory")
    result = []
    for row in rows:
        need(isinstance(row, dict) and row.get("kind") in ("admin", "emby") and
             all(isinstance(row.get(key), str) and re.fullmatch(r"[0-9a-f]{32}", row[key]) for key in ("id", "user_id")) and
             isinstance(row.get("token_hash"), str) and re.fullmatch(r"\\x[0-9a-f]{64}", row["token_hash"]), "programs_session_identity")
        programs_instant(row.get("revoked_at"), "programs_session_not_revoked")
        result.append({"kind": row["kind"], "credentialId": row["id"], "userId": row["user_id"],
                       "tokenSha256": row["token_hash"][2:], "revokedAt": row["revoked_at"]})
    need(len({row["credentialId"] for row in result}) == len({row["tokenSha256"] for row in result}) == len(result), "programs_session_collision")
    return sorted(result, key=lambda row: row["credentialId"])


def validate_programs_snapshot(source):
    need(isinstance(source, dict) and set(source) == {"capturedAt", "tables", "sequences"} and
         isinstance(source["tables"], dict) and isinstance(source["sequences"], dict) and
         set(source["tables"]) == TABLES and set(source["sequences"]) == PROGRAMS_SEQUENCES and
         all(isinstance(rows, list) for rows in source["tables"].values()), "programs_full_snapshot_inventory")
    programs_instant(source["capturedAt"])
    for sequence in source["sequences"].values():
        need(isinstance(sequence, dict) and set(sequence) == {"lastValue", "isCalled"} and
             isinstance(sequence["lastValue"], str) and re.fullmatch(r"[0-9]+", sequence["lastValue"]) and
             type(sequence["isCalled"]) is bool, "programs_sequence_shape")
    migrations = source["tables"]["schema_migrations"]
    need(all(isinstance(row, dict) and type(row.get("version")) is int for row in migrations), "programs_migration_row_shape")
    need([row["version"] for row in migrations] == list(range(1, 29)), "programs_same_schema_required")
    return source


def validate_programs_retention(review, state, *, seconds):
    need(isinstance(review, dict) and review.get("kind") == "programs-transition-activity-retention-provenance" and
         type(review.get("version")) is int and review["version"] == 1 and
         review.get("status") == "saved_provenance_reviewed_not_live_admission" and
         type(review.get("expectedActivityRetentionDays")) is int and review["expectedActivityRetentionDays"] == 30 and
         review["basis"]["preservedRuntimeEnvironmentSha256"] == PROGRAMS_ENV_SHA and
         review["basis"]["productArchive"] in (PROGRAMS_SOURCE_ARCHIVE, receipt_descriptor(PROGRAMS_COMPLETE_ARTIFACT_PINS["sourceArchive"])) and
         canonical(review["savedActivityProjection"]["snapshot"]) == canonical(PROGRAMS_PRIOR_SOURCE), "programs_retention_provenance")
    generator = review["basis"]["generator"]
    need(generator.get("sha256") == "9b83f402ce539155dc6d1eafc99786c87b593ca47c7834127aeaacd46335ab6a" and
         type(generator.get("activityRetentionEnvironmentKeyOccurrences")) is int and generator["activityRetentionEnvironmentKeyOccurrences"] == 0 and
         len(review["basis"]["productMembers"]) == 3 and
         {row["name"]: row["sha256"] for row in review["basis"]["productMembers"]} == PROGRAMS_RETENTION_MEMBERS,
         "programs_retention_source_proof")
    need(type(seconds) is int and 0 < seconds <= 1800 and state["databaseNow"] == state["source"]["capturedAt"], "programs_retention_database_clock")
    now = programs_instant(state["databaseNow"])
    rows = state["source"]["tables"]["activity_entries"]
    need(all(now + timedelta(seconds=seconds) < programs_instant(row["created_at"]) + timedelta(days=30) for row in rows),
         "programs_activity_retention_deadline")
    environment = state["fixedFiles"][str(C / "private/runtime.env")]
    need(environment.get("sha256") == PROGRAMS_ENV_SHA and environment.get("access") == "hash_only", "programs_retention_environment_changed")
    return now


def compare_programs_logical(before, after, *, phase, window_start=None, window_end=None, added_definition=None, entry_source=None):
    """Preserve every existing row; the only delta is this transition's definition."""
    validate_programs_snapshot(before)
    validate_programs_snapshot(after)
    need(phase in ("unchanged", "startup", "failed_startup", "recovery"), "programs_comparison_phase")
    need(canonical(before["sequences"]) == canonical(after["sequences"]), "programs_sequences_changed")
    for table in TABLES - {"task_definitions"}:
        need(canonical(before["tables"][table]) == canonical(after["tables"][table]), "programs_old_rows_changed_" + table)
    old = before["tables"]["task_definitions"]
    new = after["tables"]["task_definitions"]
    if phase == "unchanged":
        need(canonical(old) == canonical(new), "programs_task_definitions_changed")
        return None
    need(isinstance(old, list) and isinstance(new, list) and all(isinstance(row, dict) and isinstance(row.get("id"), str) for row in old + new) and
         len({row["id"] for row in old}) == len(old) and len({row["id"] for row in new}) == len(new), "programs_definition_identity")
    old_by_id, new_by_id = ({row["id"]: row for row in rows} for rows in (old, new))
    if phase == "recovery":
        if added_definition is not None:
            need(isinstance(added_definition, dict) and set(added_definition) == PROGRAMS_REFRESH_KEYS and
                 isinstance(added_definition.get("id"), str) and re.fullmatch(r"[0-9a-f]{32}", added_definition["id"]) and
                 canonical({key: added_definition[key] for key in PROGRAMS_REFRESH_FIELDS}) == canonical(PROGRAMS_REFRESH_FIELDS),
                 "programs_recovery_requires_exact_refresh_definition")
            need(entry_source is not None, "programs_recovery_requires_entry_proof")
            proved = compare_programs_logical(entry_source, before, phase="failed_startup",
                window_start=entry_source["capturedAt"], window_end=before["capturedAt"])
            need(canonical(proved) == canonical(added_definition), "programs_recovery_addition_not_owned")
        else:
            need(all(row.get("key") != "library.refresh_media" for row in old), "programs_recovery_missing_addition_proof")
        need(set(old_by_id) == set(new_by_id), "programs_recovery_definition_inventory")
        for identifier, row in old_by_id.items():
            recovered = new_by_id[identifier]
            if added_definition is None or identifier != added_definition["id"]:
                need(canonical(row) == canonical(recovered), "programs_recovery_old_definition_changed")
                continue
            need(canonical(row) == canonical(added_definition) and set(recovered) == PROGRAMS_REFRESH_KEYS and
                 canonical({key: recovered[key] for key in PROGRAMS_REFRESH_KEYS - {"enabled", "revision", "updated_at"}}) ==
                 canonical({key: row[key] for key in PROGRAMS_REFRESH_KEYS - {"enabled", "revision", "updated_at"}}) and
                 recovered["enabled"] is False and type(recovered["revision"]) is int and recovered["revision"] == 2,
                 "programs_recovery_refresh_delta")
            need(programs_instant(window_start) <= programs_instant(recovered["updated_at"]) <= programs_instant(window_end), "programs_recovery_refresh_time")
        need(added_definition is None or added_definition["id"] in old_by_id, "programs_recovery_unowned_definition")
        return added_definition
    need(all(row.get("key") != "library.refresh_media" for row in old), "programs_refresh_already_present")
    need(set(old_by_id) <= set(new_by_id) and all(canonical(row) == canonical(new_by_id[key]) for key, row in old_by_id.items()), "programs_old_definition_changed")
    added = [row for row in new if row["id"] not in old_by_id]
    if phase == "failed_startup" and not added:
        return None
    need(len(added) == 1, "programs_refresh_addition_count")
    row = added[0]
    need(set(row) == PROGRAMS_REFRESH_KEYS and re.fullmatch(r"[0-9a-f]{32}", row["id"]) and
         canonical({key: row[key] for key in PROGRAMS_REFRESH_FIELDS}) == canonical(PROGRAMS_REFRESH_FIELDS), "programs_refresh_definition")
    start, end = programs_instant(window_start), programs_instant(window_end)
    need(start <= programs_instant(row["created_at"]) <= end and start <= programs_instant(row["updated_at"]) <= end,
         "programs_refresh_startup_time")
    return deepcopy(row)


def validate_programs_recovery_input(value, transition):
    validate_programs_successor_input(transition)
    need(isinstance(value, dict) and set(value) == PROGRAMS_RECOVERY_INPUT_KEYS and
         value["kind"] == "audited-candidate-programs-recovery-input" and type(value["version"]) is int and value["version"] == 1,
         "programs_recovery_input_schema")
    for key in ("transitionInput", "transitionFailure", "recoveryReview"):
        descriptor(value[key])
    output = Path(value["output"])
    need(output.parent == R and re.fullmatch(r"candidate-programs-recovery-[0-9]{2}", output.name) and
         value["transitionFailure"]["path"] == str(Path(transition["output"]) / "private/failure.json") and
         canonical(value["helpers"]) == canonical(transition["helpers"]) and canonical(value["budgets"]) == canonical(LIMITS), "programs_recovery_scope")
    return value


def validate_programs_state(state):
    need(isinstance(state, dict) and set(state) == PROGRAMS_STATE_KEYS and
         state["previousEpoch"] == PROGRAMS_PREVIOUS_EPOCH and state["seedBinding"] == PROGRAMS_PREVIOUS_BINDING and
         state["priorSource"] == PROGRAMS_PRIOR_SOURCE and state["currentRuntime"] == PROGRAMS_CURRENT_RUNTIME,
         "programs_state_authority")
    validate_programs_snapshot(state["source"])
    validate_programs_snapshot(state["inactiveStage"])
    need(state["databaseNow"] == state["source"]["capturedAt"], "programs_state_database_clock")
    programs_instant(state["capturedAt"])
    tables = state["source"]["tables"]
    sessions = programs_sessions(state["source"])
    need(all(not tables[key] for key in ("encoding_jobs", "task_triggers", "task_runs", "task_run_children", "task_run_requests", "task_occurrences")) and
         all(row.get("status") == "Completed" for row in tables["scan_jobs"]), "programs_pending_startup_work")
    definitions = tables["task_definitions"]
    need(len(definitions) in (1, 2) and {row.get("key") for row in definitions} in ({"library.scan"}, {"library.scan", "library.refresh_media"}),
         "programs_unreviewed_task_definition")
    need(set(state["trees"]) == set(TREE_ROOTS) and
         set(state["trees"][str(C / "data/cache")]) == {".", ".goby-transcode-cache", ".goby-transcode-lock"}, "programs_cache_or_tree_scope")
    need(not any(".next-" in name or ".goby-backup-catalog.pending" in name for tree in state["trees"].values() for name in tree), "programs_pending_control_file")
    documents = state["controlDocuments"]
    need("recovery/activation-journal.json" not in documents and "operations/current.json" in documents and
         "recovery/generation-registry.json" in documents and "backups/.goby-backup-catalog.json" in documents, "programs_control_inventory")
    payload = documents["operations/current.json"]["payload"]
    need(not payload.get("transition") and len(payload["operations"]) == 3 and
         {(row["kind"], row["state"], row["phase"]) for row in payload["operations"]} ==
         {("create", "failed", "finished"), ("create", "completed", "finished"), ("restore", "cancelled", "finished")} and
         all(row.get("authorized") is True and row.get("applyAuthorized") is False and
             row.get("cancelAuthorized") is (row["kind"] == "restore") for row in payload["operations"]), "programs_recovery_work_pending")
    entries = documents["backups/.goby-backup-catalog.json"]["entries"]
    need(len(entries) == 2 and {(row["metadata"]["state"], row["phase"], row["metadata"]["size"]) for row in entries} ==
         {("failed", "empty", 0), ("ready", "ready", 290550)} and all(row["deleting"] is False for row in entries), "programs_backup_work_pending")
    generations = documents["recovery/generation-registry.json"]["generations"]
    slots = {row["slot"]: row for row in payload["slots"]}
    restore = next(row for row in payload["operations"] if row["kind"] == "restore")
    need(len(generations) == 1 and generations[0]["complete"] is True and set(slots) == {"primary", "recovery"} and
         slots["primary"]["state"] == "active" and slots["recovery"]["state"] == "staged" and
         slots["recovery"]["imageId"] == restore["generationId"] == generations[0]["id"] and
         slots["recovery"]["operation"] == restore["id"], "programs_inactive_generation_changed")
    need(PROGRAMS_PROTECTED <= set(state["protected"]) and set(state["unitLogs"]) == {"server-unit.log", "postgres-unit.log"}, "programs_protected_scope")
    for path in PROGRAMS_STAT_ONLY:
        facts = state["fixedFiles"].get(path)
        need(isinstance(facts, dict) and facts.get("access") == "stat_only" and
             (set(facts) == {"access", "absent"} and facts["absent"] is True or
              set(facts) == {"access", "dev", "ino", "uid", "gid", "mode", "bytes", "mtimeNs", "ctimeNs"}),
             "programs_secret_must_be_stat_only")
    environment = state["fixedFiles"][str(C / "private/runtime.env")]
    need(set(environment) == {"access", "dev", "ino", "uid", "gid", "mode", "bytes", "mtimeNs", "ctimeNs", "sha256"} and
         environment["access"] == "hash_only" and environment["sha256"] == PROGRAMS_ENV_SHA, "programs_environment_facts")
    for first, second in (("candidateBefore", "candidateAfter"), ("postgresBefore", "postgresAfter"),
                          ("leaseBefore", "leaseAfter"), ("hostingBefore", "hostingAfter")):
        need(canonical(state[first]) == canonical(state[second]), "programs_capture_identity_drift")
    need(canonical(state["postgresProcess"]) == canonical(state["postgresAfter"]), "programs_postgres_projection")
    return {"ownedTables": 35, "sequences": 5, "revokedSessions": sessions, "playRows": len(tables["play_sessions"]),
            "userDataRows": len(tables["user_item_data"]), "retainedReferences": len(tables["client_playback_references"])}


def compare_programs_preservation(before, after, *, installed_binary=None, recovery=False, added_definition=None, entry_source=None):
    validate_programs_state(before)
    validate_programs_state(after)
    phase = "recovery" if recovery else "startup" if installed_binary is not None else "unchanged"
    definition = compare_programs_logical(before["source"], after["source"], phase=phase,
        window_start=before["databaseNow"], window_end=after["databaseNow"], added_definition=added_definition, entry_source=entry_source)
    compare_programs_logical(before["inactiveStage"], after["inactiveStage"], phase="unchanged")
    for section in ("databases", "trees", "controlDocuments", "loadedUnits", "protected", "postgresBefore", "postgresAfter", "hostingBefore", "hostingAfter"):
        need(canonical(before[section]) == canonical(after[section]), "programs_preservation_changed_" + section)
    old_files, new_files = before["fixedFiles"], after["fixedFiles"]
    need(set(old_files) == set(new_files), "programs_fixed_file_inventory")
    binary = str(C / "install/goby")
    for name, facts in old_files.items():
        if installed_binary is not None and name == binary:
            descriptor(installed_binary)
            need(installed_binary["path"] == binary and new_files[name]["sha256"] == installed_binary["sha256"] and
                 all(canonical(facts[key]) == canonical(new_files[name][key]) for key in ("dev", "uid", "gid", "mode")), "programs_installed_binary_authority")
        else:
            need(canonical(facts) == canonical(new_files[name]), "programs_fixed_file_changed")
    for name, facts in before["unitLogs"].items():
        current = after["unitLogs"][name]
        need(all(canonical(facts[key]) == canonical(current[key]) for key in ("dev", "ino", "uid", "gid", "mode")) and
             current["bytes"] >= facts["bytes"] and current["bytes"] - facts["bytes"] <= 1 << 20 and
             (current.get("prefixSha256") == facts["sha256"] or current["bytes"] == facts["bytes"] and current["sha256"] == facts["sha256"]),
             "programs_unit_log_prefix_changed")
    if installed_binary is None:
        old, new = before["diagnostics"], after["diagnostics"]
        need(set(old) == set(new) and all(canonical(old[key]) == canonical(new[key]) for key in set(old) - {"files"}) and
             set(old["files"]) == set(new["files"]), "programs_before_diagnostics_changed")
        for name, facts in old["files"].items():
            current = new["files"][name]
            need(all(canonical(facts[key]) == canonical(current[key]) for key in ("dev", "ino", "uid", "gid", "mode")) and
                 current["bytes"] >= facts["bytes"] and current["bytes"] - facts["bytes"] <= 1 << 20 and
                 (current.get("prefixSha256") == facts["sha256"] or current["bytes"] == facts["bytes"] and current["sha256"] == facts["sha256"]),
                 "programs_diagnostic_prefix_changed")
        for key in ("candidateBefore", "candidateAfter", "leaseBefore", "leaseAfter"):
            need(canonical(before[key]) == canonical(after[key]), "programs_before_identity_changed")
    else:
        compare_diagnostics(before["diagnostics"], after["diagnostics"])
    return {"existingSourceRowsExact": 35, "sequencesExact": 5, "inactiveStageExact": True, "allPriorPlayAndUserDataExact": True,
            "foreignReferencesExact": True, "configurationExact": True, "postgresContinuous": True, "hostingContinuous": True,
            "refreshDefinition": definition}


def compare_programs_failed_diagnostics(before, after):
    old, new = before["registry"], after["registry"]
    need(type(old["version"]) is int and old["version"] == 1 and canonical({key: old[key] for key in old if key != "files"}) ==
         canonical({key: new[key] for key in new if key != "files"}) and before["lock"] == after["lock"] and before["directory"] == after["directory"],
         "programs_failed_diagnostic_authority")
    previous, current = ({row["name"]: row for row in registry["files"]} for registry in (old, new))
    additions = set(current) - set(previous)
    need(len(previous) == len(old["files"]) and len(current) == len(new["files"]) and set(previous) <= set(current) and len(additions) <= 1 and
         set(before["files"]) == set(previous) and set(after["files"]) == set(current) and
         all(before["registryFile"][key] == after["registryFile"][key] for key in ("uid", "gid", "mode")), "programs_failed_diagnostic_inventory")
    for name, original in previous.items():
        observed = current[name]
        need(type(original["closed"]) is bool and type(observed["closed"]) is bool and not original.get("deleting") and not observed.get("deleting") and
             (not original["closed"] or observed["closed"]) and
             canonical({key: original[key] for key in original if key not in ("size", "closed")}) ==
             canonical({key: observed[key] for key in observed if key not in ("size", "closed")}), "programs_failed_old_diagnostic_changed")
        left, right = before["files"][name], after["files"][name]
        need(all(left[key] == right[key] for key in ("dev", "ino", "uid", "gid", "mode")) and
             0 <= right["bytes"] - left["bytes"] <= 1 << 20 and
             (right.get("prefixSha256") == left["sha256"] or right["bytes"] == left["bytes"] and right["sha256"] == left["sha256"]) and
             (not original["closed"] or right["sha256"] == left["sha256"]), "programs_failed_diagnostic_prefix")
        need(observed["size"] == (right["bytes"] if observed["closed"] else original["size"]), "programs_failed_diagnostic_size")
    for name in additions:
        entry, facts = current[name], after["files"][name]
        need(re.fullmatch(r"goby-" + old["token"] + r"-[0-9a-f]{32}\.jsonl", name) and type(entry["closed"]) is bool and
             not entry.get("deleting") and facts["bytes"] <= 1 << 20 and
             entry["identity"] == {"device": facts["dev"], "inode": facts["ino"]} and
             all(facts[key] == next(iter(before["files"].values()))[key] for key in ("uid", "gid", "mode")), "programs_failed_new_diagnostic")
    need(sum(row["closed"] is False for row in current.values()) <= 1, "programs_failed_diagnostic_writers")


def compare_programs_failed_state(before, failed, transition, *, allow_startup=True):
    validate_programs_state(before)
    validate_programs_state(failed)
    definition = compare_programs_logical(before["source"], failed["source"], phase="failed_startup" if allow_startup else "unchanged",
        window_start=before["databaseNow"], window_end=failed["databaseNow"])
    compare_programs_logical(before["inactiveStage"], failed["inactiveStage"], phase="unchanged")
    for key in ("databases", "trees", "controlDocuments", "loadedUnits", "protected", "postgresBefore", "postgresAfter", "hostingBefore", "hostingAfter"):
        need(canonical(before[key]) == canonical(failed[key]), "programs_failed_native_change_" + key)
    need(set(before["fixedFiles"]) == set(failed["fixedFiles"]), "programs_failed_file_inventory")
    for path, old in before["fixedFiles"].items():
        current = failed["fixedFiles"][path]
        if path == str(C / "install/goby"):
            need(current["sha256"] in {PROGRAMS_OLD_BINARY, transition["newBinary"]["sha256"]} and
                 all(canonical(old[key]) == canonical(current[key]) for key in ("dev", "uid", "gid", "mode")), "programs_failed_binary_unknown")
        else:
            need(canonical(old) == canonical(current), "programs_failed_fixed_file_changed")
    for name, old in before["unitLogs"].items():
        current = failed["unitLogs"][name]
        need(all(old[key] == current[key] for key in ("dev", "ino", "uid", "gid", "mode")) and
             0 <= current["bytes"] - old["bytes"] <= 1 << 20 and
             (current.get("prefixSha256") == old["sha256"] or current["bytes"] == old["bytes"] and current["sha256"] == old["sha256"]),
             "programs_failed_unit_log_changed")
    compare_programs_failed_diagnostics(before["diagnostics"], failed["diagnostics"])
    return definition


def validate_programs_recovery_review(value, transition, review, failure, before, failed, current):
    validate_programs_recovery_input(value, transition)
    keys = {"kind", "version", "status", "transitionInput", "transitionFailure", "before", "failedState", "oldBinaryCopy", "mode", "serverProperties", "serverIdentity"}
    need(isinstance(review, dict) and set(review) == keys and review["kind"] == "audited-programs-transition-failure-review" and
         type(review["version"]) is int and review["version"] == 1 and review["status"] == "failed_transition_state_reviewed" and
         review["transitionInput"] == value["transitionInput"] and review["transitionFailure"] == value["transitionFailure"] and
         review["mode"] in ("start_original_installed", "restore_original"), "programs_recovery_review_schema")
    for key in ("transitionInput", "transitionFailure", "before", "failedState", "oldBinaryCopy"):
        descriptor(review[key])
    need(failure.get("kind") == "audited-programs-transition-failure" and type(failure.get("version")) is int and failure["version"] == 1 and
         failure.get("status") == "transition_failed_resources_retained" and failure.get("input") == value["transitionInput"] and
         failure.get("output") == transition["output"] and failure.get("before") == review["before"] and
         failure.get("oldBinaryCopy") == review["oldBinaryCopy"] and failure.get("automaticRetry") is False and failure.get("automaticRollback") is False,
         "programs_recovery_failure_binding")
    captured = failure.get("failureCapture")
    need(isinstance(captured, dict) and captured.get("status") in ("existing_after_state_retained", "captured_awaiting_independent_review") and
         captured.get("state") == review["failedState"] and canonical(captured.get("serverProperties")) == canonical(review["serverProperties"]) and
         canonical(captured.get("serverIdentity")) == canonical(review["serverIdentity"]), "programs_recovery_failed_state_not_original_capture")
    calls = failure["calls"]
    need(set(calls) == {"stop", "replace", "start"} and all(type(number) is int and number in (0, 1) for number in calls.values()) and
         calls["stop"] == 1 and calls["start"] <= calls["replace"] <= calls["stop"], "programs_recovery_failure_phase")
    responsibilities = failure["commandResponsibilities"]
    need(isinstance(responsibilities, list) and responsibilities and all(isinstance(row, dict) and row.get("processGroupClosed") is True and
         row.get("outcome") == "acknowledged" and row.get("timedOut") is False and row.get("descendantsRemained") is False and
         row.get("sqlOutcome") in (None, "acknowledged") and type(row.get("exitCode")) is int and row["exitCode"] == 0 for row in responsibilities), "programs_recovery_unknown_command_outcome")
    need(review["before"]["path"] == str(Path(transition["output"]) / "private/before.json") and
         review["oldBinaryCopy"] == {"path": str(Path(transition["output"]) / "private/goby-before.bin"), "sha256": PROGRAMS_OLD_BINARY},
         "programs_recovery_old_binary_scope")
    definition = compare_programs_failed_state(before, failed, transition)
    properties, prior_properties = review["serverProperties"], current["current"]["serverProperties"]
    dynamic = {"ActiveState", "SubState", "MainPID", "InvocationID", "Result", "ExecMainStatus"}
    need(isinstance(properties, dict) and set(properties) == set(prior_properties) and
         all(canonical(properties[key]) == canonical(prior_properties[key]) for key in set(properties) - dynamic), "programs_recovery_unit_changed")
    installed = failed["fixedFiles"][str(C / "install/goby")]["sha256"]
    if review["mode"] == "start_original_installed":
        need(installed == PROGRAMS_OLD_BINARY and definition is None and calls["start"] == 0, "programs_recovery_original_not_installed")
    else:
        need(installed == transition["newBinary"]["sha256"] and calls["replace"] == 1, "programs_recovery_successor_not_installed")
    if properties["MainPID"] == "0":
        need(properties["ActiveState"] in ("inactive", "failed") and review["serverIdentity"] is None and
             failed["candidateAfter"] is None and failed["leaseAfter"] is None and
             all(row["closed"] is True for row in failed["diagnostics"]["registry"]["files"]), "programs_recovery_stopped_state")
    else:
        process = failed["candidateAfter"]
        prior = current["current"]["candidateProcess"]
        need(review["mode"] == "restore_original" and properties["ActiveState"] == "active" and properties["SubState"] == "running" and
             isinstance(process, dict) and set(process) == set(prior) | {"listener"} and
             type(process["pid"]) is int and process["pid"] != prior["pid"] and properties["MainPID"] == str(process["pid"]) and
             all(canonical(process[key]) == canonical(prior[key]) for key in set(prior) - {"pid", "startTicks", "exeInode"}) and
             process["exeInode"] == failed["fixedFiles"][str(C / "install/goby")]["ino"] and failed["leaseAfter"] is not None,
             "programs_recovery_running_process")
        identity = review["serverIdentity"]
        need(isinstance(identity, dict) and identity.get("pid") == process["pid"] and identity.get("startTicks") == process["startTicks"] and
             identity.get("executableInode") == process["exeInode"] and identity.get("invocationId") == properties["InvocationID"], "programs_recovery_process_projection")
    return definition


def validate_programs_successor_epoch(epoch):
    need(isinstance(epoch, dict) and set(epoch) == PROGRAMS_EPOCH_KEYS and epoch["kind"] == "audited-candidate-runtime-epoch" and
         type(epoch["version"]) is int and epoch["version"] == 4 and epoch["operationKind"] == "programs_successor" and
         epoch["status"] == "running_awaiting_live_acceptance" and epoch["candidateAdmissionComplete"] is False and
         epoch["previousEpoch"] == PROGRAMS_PREVIOUS_EPOCH and epoch["previousCurrentRuntime"] == PROGRAMS_CURRENT_RUNTIME and
         epoch["originalProvision"] == PROVISION and epoch["seedProvenance"] == SEED, "programs_epoch_schema")
    for key in ("transitionInput", "transitionHelper", "runtimeHelper", "before", "after", "sourceBefore", "sourceAfter", "productInput", "configurationInput", "preservation", "reviewedState", "reviewedSummary"):
        descriptor(epoch[key])
    need(epoch["productInput"] == epoch["transitionInput"] and canonical(epoch["calls"]) == canonical({"stop": 1, "replace": 1, "start": 1}), "programs_epoch_operation")
    source = epoch["currentSource"]
    need(isinstance(source, dict) and set(source) == PROGRAMS_SOURCE_KEYS and source["archiveSha256"] in
         (PROGRAMS_SOURCE_ARCHIVE["sha256"], PROGRAMS_COMPLETE_ARTIFACT_PINS["sourceArchive"]["sha256"]) and
         type(source["schema"]) is int and source["schema"] == 28, "programs_epoch_source")
    for key in PROGRAMS_SOURCE_KEYS - {"archiveSha256", "schema"}:
        descriptor(source[key])
    need(source["binary"]["path"] == str(C / "install/goby") and
         source["binary"]["sha256"] not in {OLD_BINARY, CURRENT_BINARY, PROGRAMS_OLD_BINARY, PROGRAMS_E11_BINARY}, "programs_epoch_binary")
    if source["archiveSha256"] == PROGRAMS_COMPLETE_ARTIFACT_PINS["sourceArchive"]["sha256"]:
        need(source["binary"]["sha256"] == PROGRAMS_COMPLETE_ARTIFACT_PINS["newBinary"]["sha256"] and
             all(source[key] == receipt_descriptor(PROGRAMS_COMPLETE_ARTIFACT_PINS[key]) for key in
                 ("sourceManifest", "buildManifest", "sourceBridge")), "programs_complete_epoch_source")
    candidate = epoch["candidate"]
    need(candidate["bootstrapExecuted"] is True and canonical(candidate["sourceState"]) == canonical({"users": 8, "schema": 28, "migrations": 28}) and
         candidate["originalProvision"] == PROVISION and candidate["seedProvenance"] == SEED and
         candidate["input"] == candidate["productInput"] == epoch["productInput"] and candidate["binary"] == source["binary"] and
         candidate["currentSourceManifest"] == source["sourceManifest"] and candidate["backendReport"] == source["fullReport"], "programs_candidate_binding")
    return epoch


def validate_programs_binding(binding, epoch_pin, epoch, seed):
    validate_programs_successor_epoch(epoch)
    need(isinstance(binding, dict) and set(binding) == PROGRAMS_BINDING_KEYS and binding["kind"] == "audited-candidate-seed-runtime-binding" and
         type(binding["version"]) is int and binding["version"] == 4 and binding["runtimeEpoch"] == epoch_pin and
         binding["previousBinding"] == PROGRAMS_PREVIOUS_BINDING and binding["previousCurrentRuntime"] == PROGRAMS_CURRENT_RUNTIME and
         binding["reviewedState"] == epoch["reviewedState"] and binding["reviewedSummary"] == epoch["reviewedSummary"] and
         binding["priorSource"] == PROGRAMS_PRIOR_SOURCE and binding["priorCloseout"] == PROGRAMS_PRIOR_CLOSEOUT and
         binding["originalSeed"] == SEED and binding["seedExecutor"] == seed["helper"] and binding["seedExecutor"]["sha256"] == SEED_EXECUTOR and
         binding["seedInput"] == seed["input"] and binding["seedSessionAddendum"] == SEED_ADDENDUM and binding["admission02"] == ADMISSION02 and
         binding["candidateAdmissionComplete"] is False, "programs_seed_provenance")
    for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources", "seedCleanup"):
        need(canonical(binding[key]) == canonical(seed["cleanup"] if key == "seedCleanup" else seed[key]), "programs_seed_mapping_changed")
    rows = binding["currentSessions"]
    need(isinstance(rows, list) and all(isinstance(row, dict) and set(row) == {"kind", "credentialId", "tokenSha256", "userId", "revokedAt"} for row in rows), "programs_binding_session_shape")
    projected = {"tables": {"sessions": [{"kind": row["kind"], "id": row["credentialId"], "user_id": row["userId"],
        "token_hash": "\\x" + row["tokenSha256"], "revoked_at": row["revokedAt"]} for row in rows]}}
    need(canonical(rows) == canonical(programs_sessions(projected)), "programs_binding_session_order")
    return binding


def validate_programs_closeout_runtime(value, current, closeout_runtime):
    """Relate the old TV closeout to the envelope already checked by the loader."""
    if programs_complete_source(value["newSourceArchive"]):
        need(validate_current_runtime_schema(current) == 2 and
             current["previousCurrentRuntime"] == CURRENT_RUNTIME_V2_PREDECESSOR and
             current["recovery"] == CURRENT_RUNTIME_SECOND_RECOVERY and
             current["runtimeEpoch"] == value["previousEpoch"] and current["seedBinding"] == value["previousBinding"] and
             closeout_runtime == CURRENT_RUNTIME_V1_PREDECESSOR, "programs_tv_runtime_ancestor")
    else:
        need(canonical(closeout_runtime) == canonical(value["currentRuntime"]), "programs_tv_evidence_changed")


def validate_programs_startup_review(value, summary, state, closeout, prior_source, current, retention):
    validate_programs_successor_input(value)
    expected = {"kind", "version", "status", "state", "previousEpoch", "seedBinding", "currentRuntime", "priorSource", "priorCloseout",
                "boundary", "independentReview", "retentionReview"}
    need(isinstance(summary, dict) and set(summary) == expected and summary["kind"] == "audited-programs-transition-startup-state-review" and
         type(summary["version"]) is int and summary["version"] == 1 and summary["status"] == "captured_state_supports_bounded_transition_contract",
         "programs_startup_review_schema")
    for key in expected - {"kind", "version", "status"}:
        descriptor(summary[key])
    for target, origin in (("state", "reviewedState"), ("previousEpoch", "previousEpoch"), ("seedBinding", "previousBinding"),
                           ("currentRuntime", "currentRuntime"), ("priorSource", "priorSource"), ("priorCloseout", "priorCloseout")):
        need(canonical(summary[target]) == canonical(value[origin]), "programs_startup_review_binding")
    need(closeout.get("kind") == "audited-candidate-client-closeout" and type(closeout.get("version")) is int and closeout["version"] == 1 and
         closeout.get("status") == "owned_state_closed_diagnostic_result" and closeout.get("clientAcceptance") is False, "programs_tv_closeout_required")
    evidence = closeout["evidence"]
    for key, pin in (("runtimeEpoch", value["previousEpoch"]), ("seedBinding", value["previousBinding"]),
                     ("sourceAfter", value["priorSource"]), ("boundary", summary["boundary"])):
        need(canonical(evidence[key]) == canonical(pin), "programs_tv_evidence_changed")
    validate_programs_closeout_runtime(value, current, evidence["currentRuntime"])
    need(evidence["admission"] == current["admission"], "programs_tv_admission_changed")
    facts = validate_programs_state(state)
    compare_programs_logical(prior_source, state["source"], phase="unchanged")
    validate_programs_retention(retention, state, seconds=LIMITS["maximumSeconds"] + PROGRAMS_RECOVERY_POLICY["maximumSeconds"])
    need(retention["basis"]["productArchive"] == value["newSourceArchive"], "programs_retention_product_binding")
    need(all(row.get("key") != "library.refresh_media" for row in state["source"]["tables"]["task_definitions"]), "programs_review_requires_absent_refresh")
    identity = current["current"]
    candidate = {**identity["candidateProcess"], "listener": {"host": "127.0.0.1", "port": identity["listener"]["port"], "socketInode": identity["listener"]["socketInode"]}}
    need(canonical(state["candidateBefore"]) == canonical(candidate) and canonical(state["postgresBefore"]) == canonical(identity["postgresProcess"]) and
         canonical(state["leaseBefore"]) == canonical(identity["lease"]), "programs_review_current_identity")
    return facts


def validate_programs_runtime_change(previous, epoch, before, after):
    old, new = previous["current"]["candidateProcess"], epoch["candidateProcess"]
    changed = {"pid", "startTicks", "exeInode"}
    need(isinstance(new, dict) and set(new) == set(old) and all(canonical(new[key]) == canonical(old[key]) for key in set(old) - changed) and
         type(new["pid"]) is int and new["pid"] > 1 and new["pid"] != old["pid"] and
         isinstance(new["startTicks"], str) and re.fullmatch(r"[1-9][0-9]*", new["startTicks"]) and
         int(new["startTicks"]) > int(old["startTicks"]), "programs_process_transition")
    candidate = epoch["candidate"]
    binary = after["fixedFiles"][str(C / "install/goby")]
    need(new["exeDevice"] == binary["dev"] and new["exeInode"] == binary["ino"] and
         candidate["serverIdentity"]["pid"] == new["pid"] and candidate["serverIdentity"]["startTicks"] == new["startTicks"] and
         candidate["serverIdentity"]["executableDevice"] == new["exeDevice"] and candidate["serverIdentity"]["executableInode"] == new["exeInode"],
         "programs_executable_identity")
    properties, prior_properties = candidate["processes"]["server"], previous["current"]["serverProperties"]
    identity, prior_identity = candidate["serverIdentity"], previous["current"]["serverIdentity"]
    need(set(identity) == set(prior_identity) and all(canonical(identity[key]) == canonical(prior_identity[key]) for key in set(identity) -
         {"pid", "startTicks", "invocationId", "executableInode"}), "programs_process_identity_projection")
    need(set(properties) == set(prior_properties) and all(canonical(properties[key]) == canonical(prior_properties[key]) for key in set(properties) - {"MainPID", "InvocationID"}) and
         properties["MainPID"] == str(new["pid"]) and isinstance(properties["InvocationID"], str) and
         re.fullmatch(r"[0-9a-f]{32}", properties["InvocationID"]) and properties["InvocationID"] != prior_properties["InvocationID"] and
         candidate["serverIdentity"]["invocationId"] == properties["InvocationID"], "programs_service_identity")
    listener = candidate["listener"]
    prior_listener = previous["current"]["listener"]
    need(set(listener) == set(prior_listener) and listener["pid"] == new["pid"] and
         all(canonical(listener[key]) == canonical(prior_listener[key]) for key in set(listener) - {"pid", "socketInode"}) and
         isinstance(listener["socketInode"], str) and re.fullmatch(r"[1-9][0-9]*", listener["socketInode"]), "programs_listener_transition")
    need(canonical(after["candidateAfter"]) == canonical({**new, "listener": {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]}}) and
         canonical(after["postgresAfter"]) == canonical(epoch["postgresProcess"]) == canonical(previous["current"]["postgresProcess"]) and
         canonical(candidate["processes"]["postgres"]) == canonical(previous["current"]["postgresProperties"]), "programs_runtime_projection")
    lease, old_lease = epoch["lease"], previous["current"]["lease"]
    need(set(lease) == CURRENT_LEASE_KEYS and all(canonical(lease[key]) == canonical(old_lease[key]) for key in CURRENT_LEASE_KEYS -
         {"backendPid", "backendStart", "clientPort", "candidateConnection"}) and type(lease["backendPid"]) is int and lease["backendPid"] > 1 and
         type(lease["clientPort"]) is int and 0 < lease["clientPort"] < 65536, "programs_lease_transition")
    need(programs_instant(before["databaseNow"]) <= programs_instant(lease["backendStart"]) <= programs_instant(after["databaseNow"]), "programs_lease_start_window")
    connection = lease["candidateConnection"]
    need(set(connection) == {"pid", "localPort", "remotePort", "socketInode"} and connection["pid"] == new["pid"] and
         connection["localPort"] == lease["clientPort"] and connection["remotePort"] == candidate["ports"]["postgres"] and
         isinstance(connection["socketInode"], str) and re.fullmatch(r"[1-9][0-9]*", connection["socketInode"]) and
         canonical(after["leaseAfter"]) == canonical(lease), "programs_lease_socket_binding")


def programs_artifact_pin(pin):
    need(isinstance(pin, dict) and set(pin) == {"path", "sha256", "bytes"} and type(pin["bytes"]) is int and 0 < pin["bytes"] <= 2 << 30,
         "programs_artifact_pin")
    return descriptor({key: pin[key] for key in ("path", "sha256")})


def programs_archive_member(value):
    need(isinstance(value, dict) and set(value) == {"archive", "member", "sha256", "bytes"}, "programs_archive_member_schema")
    programs_artifact_pin(value["archive"])
    name = value["member"]
    need(isinstance(name, str) and name and not PurePosixPath(name).is_absolute() and
         all(part not in ("", ".", "..") for part in name.split("/")) and "\\" not in name and
         isinstance(value["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", value["sha256"]) and
         type(value["bytes"]) is int and 0 < value["bytes"] <= 128 << 20, "programs_archive_member_identity")
    return value


def read_programs_bytes(pin):
    """Read one selected owned artifact; configuration and database paths are excluded."""
    descriptor(pin)
    path = Path(pin["path"])
    need(path.is_relative_to("/opt/goby-test") and path.suffix.lower() not in (".env", ".key", ".dump", ".sql"), "programs_artifact_read_scope")
    for node in (path, *path.parents):
        info = node.lstat()
        need(info.st_uid == 0 and not info.st_mode & 0o022 and not stat.S_ISLNK(info.st_mode), "programs_artifact_authority")
    before = path.lstat()
    signature = lambda row: (row.st_dev, row.st_ino, row.st_mode, row.st_uid, row.st_gid, row.st_nlink, row.st_size, row.st_mtime_ns, row.st_ctime_ns)
    need(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and 0 < before.st_size <= 2 << 30, "programs_artifact_bound")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        need(signature(os.fstat(stream.fileno())) == signature(before), "programs_artifact_open_changed")
        raw = stream.read(before.st_size + 1)
        need(signature(os.fstat(stream.fileno())) == signature(before), "programs_artifact_read_changed")
    need(signature(path.lstat()) == signature(before) and len(raw) == before.st_size and digest(raw) == pin["sha256"], "programs_artifact_digest")
    return raw


def programs_json(raw):
    def unique(pairs):
        value = dict(pairs)
        need(len(value) == len(pairs), "programs_duplicate_json_key")
        return value
    need(isinstance(raw, bytes) and len(raw) <= 16 << 20, "programs_json_bound")
    return json.loads(raw, object_pairs_hook=unique, parse_constant=lambda unused: need(False, "programs_nonfinite_json"))


def validate_programs_source_compatibility(sources, previous):
    selected = {name for name in set(sources) | set(previous) if name.startswith("internal/database/migrations/") or
                name.startswith("internal/backuppg/catalogs/") and name.endswith(".json")}
    need(sum(name.startswith("internal/database/migrations/") for name in selected) == 28 and
         sum(name.startswith("internal/backuppg/catalogs/") for name in selected) == 6 and
         all(name in sources and name in previous and all(canonical(sources[name][key]) == canonical(previous[name][key]) for key in ("bytes", "sha256"))
             for name in selected), "programs_same_schema_resources_changed")


def validate_programs_source_bridge_profile(bridge, *, complete):
    """Keep the old bridge shape exact and select one complete-source record."""
    keys = {"kind", "version", "status", "input", "sourceArchive", "sourceManifest", "sourceCheckpoint", "gitCommit", "frozenFiles", "trackedBuildInputs",
            "generatedAssetCount", "frozenBytes", "sourceInventory", "moduleInputs", "administratorAssets", "administratorEntryReferences", "deploymentInputs",
            "runtimeModeProjection", "buildManifest", "packageManifest", "reader", "sourceTreeUnchangedBeforeAfter", "gitAndFrozenModesDeclaredEqual"}
    if complete:
        keys.add("trackedInputCountScope")
    counts = PROGRAMS_COMPLETE_COUNTS if complete else {"frozenFiles": 924, "trackedBuildInputs": 867, "generatedAssetCount": 57, "frozenBytes": 13253765}
    need(isinstance(bridge, dict) and set(bridge) == keys and bridge["kind"] == "goby-frozen-source-build-bridge" and
         type(bridge["version"]) is int and bridge["version"] == 1 and bridge["status"] == "matched" and exact_counts(bridge, counts) and
         (not complete or bridge["gitCommit"] == "3d6b79b36b6a5050e174f7a152f214b9356608c8" and
          bridge["trackedInputCountScope"] == PROGRAMS_COMPLETE_TRACKED_SCOPE) and
         bridge["sourceTreeUnchangedBeforeAfter"] is True and bridge["gitAndFrozenModesDeclaredEqual"] is False, "programs_source_bridge_schema")


def validate_programs_adapter_source(review_adapter, closure_adapter, read_bytes):
    """Bind the complete profile's retained Pin2 to a separately read Pin3 source."""
    selected = programs_artifact_pin(review_adapter)
    descriptor(closure_adapter)
    need(selected == closure_adapter, "programs_adapter_source_pin")
    content = read_bytes(review_adapter)
    need(isinstance(content, bytes) and len(content) == review_adapter["bytes"] and
         digest(content) == review_adapter["sha256"], "programs_adapter_source_bytes")


def load_programs_product(value, read_descriptor, read_bytes=None):
    """Verify the actual full/build worker and only its declared archived artifacts."""
    validate_programs_successor_input(value)
    complete = programs_complete_source(value["newSourceArchive"])
    read_bytes = read_bytes or read_programs_bytes
    cache = {}
    def raw(pin):
        selected = programs_artifact_pin(pin) if "bytes" in pin else descriptor(pin)
        key = (selected["path"], selected["sha256"])
        if key not in cache:
            cache[key] = read_bytes(selected)
        content = cache[key]
        need(isinstance(content, bytes) and digest(content) == selected["sha256"] and
             ("bytes" not in pin or len(content) == pin["bytes"]), "programs_record_bytes")
        return content
    def document(pin):
        parsed = programs_json(raw(pin))
        selected = programs_artifact_pin(pin) if "bytes" in pin else pin
        need(canonical(parsed) == canonical(read_descriptor(selected)), "programs_record_readback")
        return parsed
    def member(pin, *, metadata_only=False):
        programs_archive_member(pin)
        # The archive is hashed once into this cache. All selected member reads
        # use these same bytes, never a path reopened after verification.
        content = raw(pin["archive"])
        with tarfile.open(fileobj=io.BytesIO(content), mode="r:*") as archive:
            matches = [row for row in archive.getmembers() if str(PurePosixPath(row.name)) == pin["member"]]
            need(len(matches) == 1 and matches[0].name == pin["member"] and matches[0].isfile() and
                 not matches[0].sparse and matches[0].size == pin["bytes"], "programs_archive_member_changed")
            if metadata_only:
                return None
            stream = archive.extractfile(matches[0])
            need(stream is not None, "programs_archive_member_missing")
            with stream:
                result = stream.read(pin["bytes"] + 1)
            need(len(result) == pin["bytes"] and digest(result) == pin["sha256"], "programs_archive_member_digest")
            return result
    receipt = document(value["newArtifactReceipt"])
    keys = {"kind", "version", "status", "sourceArchive", "sourceManifest", "sourceBridge", "fullReport", "buildManifest", "newBinary",
            "packageManifest", "packageArchive", "buildTools", "worker", "execution", "archive", "closure", "independentReview"}
    need(isinstance(receipt, dict) and set(receipt) == keys and receipt["kind"] == "goby-internal-amd64-artifact-receipt" and
         type(receipt["version"]) is int and receipt["version"] == 1 and receipt["status"] == "verified_and_closed", "programs_artifact_receipt_schema")
    for key in keys - {"kind", "version", "status", "fullReport", "worker"}:
        programs_artifact_pin(receipt[key])
    if complete:
        need(all(receipt[key] == pin for key, pin in PROGRAMS_COMPLETE_ARTIFACT_PINS.items()), "programs_complete_artifact_pins")
    programs_archive_member(receipt["worker"])
    full = receipt["fullReport"]
    need(isinstance(full, dict) and set(full) == {"execution", "worker", "ordinaryBinary"} and
         canonical(full["execution"]) == canonical(receipt["execution"]) and canonical(full["worker"]) == canonical(receipt["worker"]), "programs_single_worker_binding")
    programs_archive_member(full["ordinaryBinary"])
    for target, origin in (("sourceArchive", "newSourceArchive"), ("sourceManifest", "newSourceManifest"), ("sourceBridge", "sourceBridge"),
                           ("buildManifest", "newBuildManifest"), ("newBinary", "newBinary"), ("execution", "newFullReport")):
        need(programs_artifact_pin(receipt[target]) == value[origin], "programs_artifact_input_binding")
    for key, filename in (("newBinary", "goby"), ("buildManifest", "manifest.json"), ("packageManifest", "package-manifest.json"),
                          ("packageArchive", "goby-linux-amd64-systemd.tar.gz")):
        product_root = PROGRAMS_COMPLETE_PRODUCT_ROOT if complete else PROGRAMS_PRODUCT_ROOT
        need(receipt[key]["path"] == str(product_root / "artifacts/linux-amd64-systemd" / filename), "programs_materialized_product_path")
    need(receipt["worker"]["archive"] == full["ordinaryBinary"]["archive"] == receipt["archive"], "programs_full_archive_binding")
    execution, closure, review = (document(receipt[key]) for key in ("execution", "closure", "independentReview"))
    closure_flags = {"ownedProcessesClosed", "ext4Unmounted", "loopDetached", "ramUnmounted", "resourcesClosed", "lockReleased", "allOwnedCommandsClosed"}
    need(execution.get("status") == "passed_with_explicit_profile_gap" and exact_counts(execution, {"workerExitCode": 0, "runnerAttempts": 1}) and
         execution.get("priorResultsReused") is False and execution.get("singleRunFullSuite") is True and execution.get("cleanupErrors") == [] and
         all(execution.get(key) is True for key in closure_flags), "programs_full_execution_not_closed")
    need(closure.get("kind") == "livetv-programs-final-closure" and type(closure.get("version")) is int and closure["version"] == 1 and
         closure.get("status") == "closed" and all(closure.get(key) is True for key in closure_flags | {"protectedUnchanged"}) and
         canonical(closure["archive"]) == canonical(receipt["archive"]), "programs_artifact_resource_closure")
    review_keys = {"kind", "version", "status", "input", "adapter", "execution", "archive", "closure", "sourceBridge", "buildManifest", "newBinary",
                   "packageManifest", "packageArchive", "buildTools", "worker", "checks", "limits"}
    checks = {"sourceIdentity", "ordinaryFullSuite", "ordinaryBuild", "embeddedBuild", "packageMembers", "artifactMaterialization", "toolPins",
              "budgets", "resourceClosure", "protectedState", "recordsBinding"}
    need(isinstance(review, dict) and set(review) == review_keys and review["kind"] == "programs-final-product-independent-review" and
         type(review["version"]) is int and review["version"] == 1 and review["status"] == "verified" and set(review["checks"]) == checks and
         all(check is True for check in review["checks"].values()) and isinstance(review["limits"], list) and all(isinstance(row, str) for row in review["limits"]),
         "programs_artifact_independent_review")
    for key in review_keys - {"kind", "version", "status", "checks", "limits", "worker"}:
        programs_artifact_pin(review[key])
    for key in ("execution", "archive", "closure", "sourceBridge", "buildManifest", "newBinary", "packageManifest", "packageArchive", "buildTools", "worker"):
        need(canonical(review[key]) == canonical(receipt[key]), "programs_independent_record_binding")
    need(canonical(review["input"]) == canonical(closure["input"]), "programs_independent_execution_source")
    if complete:
        validate_programs_adapter_source(review["adapter"], closure["adapter"], raw)
    else:
        need(canonical(review["adapter"]) == canonical(closure["adapter"]), "programs_independent_execution_source")
    worker = programs_json(member(receipt["worker"]))
    suffixes = "cmd/goby internal/activity internal/artwork internal/backupformat internal/backuppg internal/backupstore internal/config internal/database internal/diagnostics internal/events internal/identity internal/library internal/lifecycle internal/media internal/metadata internal/playback internal/recovery internal/recoverycontrol internal/server internal/settings internal/storagebinding internal/subtitle internal/tasks internal/transcode internal/recoverydb".split()
    expected = ["github.com/moooyo/goby/" + name for name in suffixes]
    skip = {"package": "github.com/moooyo/goby/internal/library", "test": "TestRootBindingFullScanMountNamespaceHelper"}
    need(worker.get("status") == "passed_with_explicit_profile_gap" and worker.get("ordinary_suite_passed") is True and
         exact_counts(worker, {"complete_package_count": 25}) and worker.get("build_executed") is True and
         worker.get("singleRunFullSuite") is True and worker.get("priorResultsReused") is False and worker.get("expected_packages") == expected and
         len(worker["packages"]) == 25 and [row["package"] for row in worker["packages"]] == expected and
         all(row.get("result") == "pass" and exact_counts(row, {"exit_code": 0, "failed": 0}) for row in worker["packages"]) and
         worker["tests"]["failed"] == [] and canonical(worker["tests"]["skipped"]) == canonical([skip]) and
         canonical(worker["declared_profile_skips"]) == canonical([{**skip, "reason": "the full-scan mount scenario requires its explicit reviewed opt-in scope", "profileStatus": "not_executed"}]),
         "programs_full_suite_contract")
    cleanup = {"owned_postgres_stopped", "private_bind_removed", "source_unchanged", "only_worker_process_remains"}
    need(set(worker["cleanup"]) == cleanup and all(worker["cleanup"][key] is True for key in cleanup), "programs_full_worker_cleanup")
    prefix = "ram/" + Path(worker["scope"]).name + "/"
    need(receipt["worker"]["member"] == prefix + "worker-report.json" and full["ordinaryBinary"]["member"] == prefix + "bin/goby-linux-amd64" and
         full["ordinaryBinary"]["sha256"] == worker["binary"]["sha256"] and full["ordinaryBinary"]["bytes"] == worker["binary"]["bytes"], "programs_ordinary_build_binding")
    member(full["ordinaryBinary"], metadata_only=True)
    materialized = execution["artifactMaterialization"]
    need(materialized.get("status") == "read_back_after_resource_closure" and materialized.get("fullComplete") is True and
         materialized.get("embeddedComplete") is True and materialized.get("independentReviewPending") is True, "programs_artifact_materialization")
    artifact_names = ("newBinary", "buildManifest", "packageManifest", "packageArchive")
    need(set(materialized["artifacts"]) == set(artifact_names), "programs_materialized_inventory")
    for key in artifact_names:
        need(canonical(materialized["artifacts"][key]) == canonical(receipt[key]) and materialized["members"][key]["archive"] == receipt["archive"], "programs_materialized_pin")
        need(member(materialized["members"][key]) == raw(receipt[key]), "programs_materialized_bytes")
    for key in ("worker", "ordinaryBinary"):
        need(canonical(materialized["members"][key]) == canonical(receipt["worker"] if key == "worker" else full[key]), "programs_archived_full_binding")
    need(materialized["members"]["supervisor"]["archive"] == receipt["archive"] and materialized["members"]["supervisor"]["member"] == prefix + "report.json", "programs_supervisor_scope")
    programs_json(member(materialized["members"]["supervisor"]))
    build, package, bridge = (document(receipt[key]) for key in ("buildManifest", "packageManifest", "sourceBridge"))
    need(materialized["members"]["sourceBridge"]["archive"] == receipt["archive"] and
         member(materialized["members"]["sourceBridge"]) == raw(receipt["sourceBridge"]), "programs_archived_source_bridge")
    embedded = worker["embeddedSystemdPackage"]
    need(embedded.get("status") == "built_and_read_back" and exact_counts(embedded, {"invocations": 1}) and
         embedded.get("commandLabel") == "embedded-systemd-build" and
         canonical(embedded["recipe"]) == canonical({"arch": "amd64", "package": "systemd", "outputDirectory": "package-amd64"}) and
         embedded.get("toolsBeforeAfterMatched") is True and embedded["build"].get("elfMachine") == 62 and
         embedded["build"].get("nativeRuntimeExecuted") is False and embedded["package"].get("independentMemberReadback") is True and
         exact_counts(embedded["package"], {"regularFiles": 5, "directories": 1}), "programs_actual_embedded_build")
    for group, key, name in (("build", "manifest", "buildManifest"), ("build", "binary", "newBinary"),
                             ("package", "manifest", "packageManifest"), ("package", "archive", "packageArchive")):
        programs_artifact_pin(embedded[group][key])
        original_path = Path(embedded[group][key]["path"])
        need(original_path.is_relative_to(worker["scope"]) and materialized["members"][name]["member"] == prefix + str(original_path.relative_to(worker["scope"])) and
             all(embedded[group][key][field] == receipt[name][field] for field in ("sha256", "bytes")), "programs_worker_materialized_product")
    for key in ("sourceArchive", "sourceManifest", "buildTools"):
        need(canonical(embedded[key]) == canonical(receipt[key]) and canonical(materialized[key]) == canonical(receipt[key]), "programs_worker_source_binding")
    programs_artifact_pin(embedded["sourceBridge"])
    original_bridge = Path(embedded["sourceBridge"]["path"])
    need(original_bridge.is_relative_to(worker["scope"]) and
         materialized["members"]["sourceBridge"]["member"] == prefix + str(original_bridge.relative_to(worker["scope"])) and
         all(embedded["sourceBridge"][key] == receipt["sourceBridge"][key] for key in ("sha256", "bytes")) and
         canonical(materialized["sourceBridge"]) == canonical(receipt["sourceBridge"]), "programs_source_bridge_materialization")
    need(receipt["buildTools"]["sha256"] == "fb5ecd65848e63edd03161ef0654a361efd4200a3ee8f2e8e158da21f23c9181" and
         receipt["buildTools"]["bytes"] == 5175, "programs_build_tool_authority")
    document(receipt["buildTools"])
    need(build.get("kind") == "goby-linux-embedded-administrator-build" and type(build.get("version")) is int and build["version"] == 1 and
         canonical(build["target"]) == canonical({"os": "linux", "arch": "amd64", "cgoEnabled": False}) and
         build.get("nativeRuntimeExecuted") is False and build.get("ociImageBuilt") is False and build["binary"].get("name") == "goby" and
         all(build["binary"][key] == receipt["newBinary"][key] for key in ("sha256", "bytes")) and
         isinstance(build.get("buildMetadata"), str) and re.search(r"(?m)^\s+build\s+-tags=goby_embed_admin\s*$", build["buildMetadata"]), "programs_build_manifest")
    binary = raw(receipt["newBinary"])
    need(len(binary) >= 64 and binary[:6] == b"\x7fELF\x02\x01" and int.from_bytes(binary[18:20], "little") == 62, "programs_embedded_amd64_elf")
    sources = document(receipt["sourceManifest"])
    need(isinstance(sources, dict) and len(sources) == (5405 if complete else 924) and all(isinstance(row, dict) and set(row) == {"bytes", "mode", "sha256"} and
         type(row["bytes"]) is int and row["bytes"] >= 0 and type(row["mode"]) is int and row["mode"] == 0o644 for row in sources.values()), "programs_frozen_source_manifest")
    prior_epoch = validate_epoch(read_descriptor(value["previousEpoch"]))
    validate_programs_source_compatibility(sources, read_descriptor(prior_epoch["currentSource"]["sourceManifest"]))
    with tarfile.open(fileobj=io.BytesIO(raw(receipt["sourceArchive"])), mode="r:*") as archive:
        observed = {}
        for entry in archive:
            name = str(PurePosixPath(entry.name))
            need(not PurePosixPath(entry.name).is_absolute() and ".." not in PurePosixPath(entry.name).parts, "programs_source_archive_path")
            if entry.isdir():
                continue
            need(entry.isfile() and not entry.sparse and name not in observed and name in sources and entry.size == sources[name]["bytes"] and entry.mode == sources[name]["mode"], "programs_source_archive_member")
            stream = archive.extractfile(entry)
            with stream:
                content = stream.read(entry.size + 1)
            need(len(content) == entry.size and digest(content) == sources[name]["sha256"], "programs_source_archive_digest")
            observed[name] = True
        need(set(observed) == set(sources), "programs_source_archive_membership")
    files = build["sourceInventory"]["files"]
    expected_files = {name for name in sources if name.startswith(("cmd/", "internal/")) or name == "web/admin/embedded.go"}
    need(len(files) == len(expected_files) and {row["name"] for row in files} == expected_files and
         all(set(row) == {"name", "sha256", "bytes"} and all(row[key] == sources[row["name"]][key] for key in ("sha256", "bytes")) for row in files), "programs_compiled_source_membership")
    need(build["sourceInventory"]["sha256"] == digest(json.dumps(files, ensure_ascii=False, separators=(",", ":")).encode()) and
         build["sourceInventory"]["fileCount"] == len(files) and build["sourceInventory"]["totalBytes"] == sum(row["bytes"] for row in files), "programs_source_inventory_digest")
    if complete:
        need(set(build["sourceInventory"]) == set(PROGRAMS_COMPLETE_INVENTORY) | {"files"} and
             all(build["sourceInventory"][key] == expected for key, expected in PROGRAMS_COMPLETE_INVENTORY.items()),
             "programs_complete_source_inventory")
    need(set(build["moduleInputs"]) == {"go.mod", "go.sum"} and all(all(build["moduleInputs"][name][key] == sources[name][key] for key in ("bytes", "sha256")) for name in build["moduleInputs"]), "programs_module_inputs")
    assets = build["administratorAssets"]
    expected_assets = {name.removeprefix("web/admin/dist/") for name in sources if name.startswith("web/admin/dist/")}
    need(len(assets) == len(expected_assets) == 57 and {row["name"] for row in assets} == expected_assets and
         all(all(row[key] == sources["web/admin/dist/" + row["name"]][key] for key in ("bytes", "sha256")) for row in assets), "programs_embedded_assets")
    validate_programs_source_bridge_profile(bridge, complete=complete)
    for key in ("sourceArchive", "sourceManifest"):
        need(canonical(bridge[key]) == canonical(receipt[key]), "programs_bridge_source_identity")
    need(canonical(bridge["input"]) == canonical(review["input"]) == canonical(materialized["input"]) and
         canonical(bridge["sourceCheckpoint"]) == canonical(embedded["sourceCheckpoint"]) == canonical(materialized["sourceCheckpoint"]) and
         canonical(bridge["reader"]) == canonical(embedded["reader"]), "programs_bridge_reader_input")
    for key in ("sourceInventory", "moduleInputs", "administratorAssets", "administratorEntryReferences"):
        need(canonical(bridge[key]) == canonical(build[key]), "programs_bridge_build_inputs")
    for key, group in (("buildManifest", "build"), ("packageManifest", "package")):
        need(canonical(bridge[key]) == canonical(embedded[group]["manifest"]), "programs_bridge_original_manifest")
    modes = bridge["runtimeModeProjection"]
    need(set(modes) == {"umask", "rule", "files", "allMatched"} and type(modes["umask"]) is int and modes["umask"] == 0o077 and
         modes["rule"] == "archiveMode & ~0077" and modes["allMatched"] is True and len(modes["files"]) == len(sources) and
         {row["name"] for row in modes["files"]} == set(sources) and all(set(row) == {"name", "archiveMode", "runtimeMode"} and
         type(row["archiveMode"]) is int and type(row["runtimeMode"]) is int and row["archiveMode"] == sources[row["name"]]["mode"] and
         row["runtimeMode"] == row["archiveMode"] & ~0o077 for row in modes["files"]), "programs_source_mode_projection")
    need(package.get("kind") == "goby-linux-systemd-package" and type(package.get("version")) is int and package["version"] == 1 and
         canonical(package["target"]) == canonical(build["target"]) and package.get("distribution") == "internal_candidate_only" and
         all(package.get(key) is True for key in ("completeReadback", "buildInputsUnchanged", "deploymentInputsUnchanged")) and
         all(package.get(key) is False for key in ("nativeRuntimeExecuted", "serviceInstalled", "ociImageBuilt")) and
         canonical(package["deploymentInputs"]) == canonical(bridge["deploymentInputs"]), "programs_package_manifest")
    need(all(package["archive"][key] == receipt["packageArchive"][key] for key in ("bytes", "sha256")) and
         all(package["buildManifest"][key] == receipt["buildManifest"][key] for key in ("bytes", "sha256")), "programs_package_artifact_binding")
    payload = package["members"]
    need(len(payload) == 5 and len({row["name"] for row in payload}) == 5, "programs_package_members")
    with tarfile.open(fileobj=io.BytesIO(raw(receipt["packageArchive"])), mode="r:gz") as archive:
        records = archive.getmembers()
        regular = [row for row in records if row.isfile()]
        need(len(records) == 6 and len(regular) == 5 and sum(row.isdir() for row in records) == 1 and
             {row.name for row in regular} == {row["name"] for row in payload}, "programs_package_archive_inventory")
        for row in regular:
            expected_row = next(item for item in payload if item["name"] == row.name)
            need(not row.sparse and row.size == expected_row["bytes"] and row.mode == expected_row["mode"], "programs_package_member_metadata")
            with archive.extractfile(row) as stream:
                content = stream.read(row.size + 1)
            need(len(content) == row.size and digest(content) == expected_row["sha256"], "programs_package_member_digest")
    catalog_raw = raw(value["compiledCatalog"])
    need(digest(catalog_raw) == sources[CATALOG_RELATIVE]["sha256"] and len(catalog_raw) == sources[CATALOG_RELATIVE]["bytes"], "programs_compiled_catalog_binding")
    frontend = document(value["frontendReport"])
    need(frontend.get("status") == "passed" and len(frontend["assets"]) == 57 and frontend.get("sourceFilesMatched") == 64, "programs_retained_frontend_receipt")
    return {"receipt": receipt, "sourceManifest": sources, "binaryBytes": binary, "catalog": programs_json(catalog_raw), "frontend": frontend}


def resolve_programs_successor_lineage(epoch, read_descriptor):
    validate_programs_successor_epoch(epoch)
    previous = validate_epoch(read_descriptor(epoch["previousEpoch"]))
    need(previous["version"] == 3, "programs_requires_tv_product_parent")
    inherited = resolve_epoch_lineage(previous, read_descriptor)
    value = validate_programs_successor_input(read_descriptor(epoch["transitionInput"]))
    need(value["previousEpoch"] == epoch["previousEpoch"] and value["currentRuntime"] == epoch["previousCurrentRuntime"] and
         canonical(value["helpers"]) == canonical(epoch["helpers"]) == canonical(previous["helpers"]) and
         epoch["configurationInput"] == previous["configurationInput"], "programs_product_configuration_lineage")
    for key, filename in (("before", "before.json"), ("after", "after.json"), ("sourceBefore", "source-before.json"),
                          ("sourceAfter", "source-after.json"), ("preservation", "preservation.json")):
        need(epoch[key]["path"] == str(Path(value["output"]) / "private" / filename), "programs_proof_scope")
    source = epoch["currentSource"]
    for key, input_key in (("sourceManifest", "newSourceManifest"), ("fullReport", "newFullReport"),
                           ("artifactReceipt", "newArtifactReceipt"), ("buildManifest", "newBuildManifest"), ("sourceBridge", "sourceBridge")):
        need(source[key] == value[input_key], "programs_epoch_product_binding")
    need(source["binary"]["sha256"] == value["newBinary"]["sha256"] and
         epoch["reviewedState"] == value["reviewedState"] and epoch["reviewedSummary"] == value["reviewedSummary"], "programs_epoch_review_binding")
    old, candidate = previous["candidate"], epoch["candidate"]
    changed = {"input", "productInput", "binary", "currentSourceManifest", "backendReport", "processes", "serverIdentity", "listener", "databases"}
    need(set(candidate) == set(old) and all(canonical(candidate[key]) == canonical(old[key]) for key in set(old) - changed) and
         canonical(candidate["processes"]["postgres"]) == canonical(old["processes"]["postgres"]) and
         canonical(epoch["postgresProcess"]) == canonical(previous["postgresProcess"]) and value["frontendReport"] == old["frontendReport"], "programs_configuration_changed")
    load_programs_product(value, read_descriptor)
    current = load_programs_previous_runtime(value["currentRuntime"], value, previous, read_descriptor, read_programs_bytes)
    summary, state = read_descriptor(value["reviewedSummary"]), read_descriptor(value["reviewedState"])
    retention = read_descriptor(summary["retentionReview"])
    validate_programs_startup_review(value, summary, state, read_descriptor(value["priorCloseout"]),
                                    read_descriptor(value["priorSource"]), current, retention)
    read_descriptor(summary["boundary"])
    read_descriptor(summary["independentReview"])
    historical_state = read_descriptor(previous["after"])
    need(canonical(state["loadedUnits"]) == canonical(historical_state["loadedUnits"]) and
         canonical(state["controlDocuments"]) == canonical(historical_state["controlDocuments"]), "programs_native_control_lineage")
    compare_programs_logical(historical_state["inactiveStage"], state["inactiveStage"], phase="unchanged")
    before, after = read_descriptor(epoch["before"]), read_descriptor(epoch["after"])
    need(canonical(read_descriptor(epoch["sourceBefore"])) == canonical(before["source"]) and
         canonical(read_descriptor(epoch["sourceAfter"])) == canonical(after["source"]), "programs_source_snapshot_binding")
    compare_programs_preservation(state, before)
    compared = compare_programs_preservation(before, after, installed_binary=source["binary"])
    validate_programs_retention(retention, before, seconds=1800)
    validate_programs_retention(retention, after, seconds=900)
    validate_programs_runtime_change(current, epoch, before, after)
    proof = read_descriptor(epoch["preservation"])
    expected = {"before", "after", "sourceBefore", "sourceAfter", "reviewedState", "installedBinary", *compared,
                "diagnostics", "oldBinaryCopy", "oldBinaryFacts", "previousLease", "diagnosticsProof", "unitLogsProof"}
    need(isinstance(proof, dict) and set(proof) == expected and all(proof[key] == epoch[key] for key in ("before", "after", "sourceBefore", "sourceAfter", "reviewedState")) and
         proof["installedBinary"] == source["binary"] and all(canonical(proof[key]) == canonical(item) for key, item in compared.items()), "programs_preservation_proof")
    need(canonical(proof["diagnostics"]) == canonical(compare_diagnostics(before["diagnostics"], after["diagnostics"])) and
         proof["oldBinaryCopy"]["path"] == str(Path(value["output"]) / "private/goby-before.bin") and
         proof["oldBinaryCopy"]["sha256"] == PROGRAMS_OLD_BINARY and
         canonical(proof["oldBinaryFacts"]) == canonical(before["fixedFiles"][str(C / "install/goby")]), "programs_old_binary_and_diagnostics_proof")
    descriptor(proof["oldBinaryCopy"])
    need(digest(read_programs_bytes(proof["oldBinaryCopy"])) == PROGRAMS_OLD_BINARY, "programs_old_binary_copy_changed")
    need(canonical(read_descriptor(proof["previousLease"])) == canonical(before["leaseBefore"]), "programs_previous_lease_proof")
    for key, section in (("diagnosticsProof", "diagnostics"), ("unitLogsProof", "unitLogs")):
        need(set(proof[key]) == {"before", "after"}, "programs_appended_evidence_scope")
        # The ordinary run writes these independently; both descriptions must
        # retain exactly the same identities and bytes, including log prefixes.
        for label, captured in (("before", before), ("after", after)):
            saved = deepcopy(read_descriptor(proof[key][label]))
            projected = deepcopy(captured[section])
            if label == "before":
                for collection in (saved, projected):
                    for row in (collection if section == "unitLogs" else collection["files"]).values():
                        row.pop("prefixSha256", None)
            need(canonical(saved) == canonical(projected), "programs_appended_evidence_binding")
    for slot in ("source", "recovery"):
        need(candidate["databases"][slot]["afterStart"] == after["databases"][slot] == before["databases"][slot] == state["databases"][slot] and
             set(candidate["databases"][slot]) == set(old["databases"][slot]) and
             all(canonical(candidate["databases"][slot][key]) == canonical(old["databases"][slot][key]) for key in set(old["databases"][slot]) - {"afterStart"}), "programs_database_facts_drift")
    return {"productEpoch": epoch, "productInput": value, "configurationInput": inherited["configurationInput"]}


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
    if type(version) is int and version == 4:
        return validate_programs_successor_epoch(epoch)
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
    if type(version) is int and version == 4:
        return validate_programs_binding(binding, epoch_pin, epoch, seed)
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
    if epoch["version"] == 4:
        return resolve_programs_successor_lineage(epoch, read_descriptor)
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
    if epoch["version"] == 4:
        resolve_programs_successor_lineage(epoch, seed_module.descriptor)
        need(epoch["candidate"]["runtime"]["sha256"] == PROGRAMS_ENV_SHA, "programs_environment_changed")
        seed_module.read_checked(epoch["candidate"]["runtime"]["path"], PROGRAMS_ENV_SHA)
        return
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


def receipt_descriptor(value):
    need(isinstance(value, dict) and set(value) in ({"path", "sha256"}, {"path", "sha256", "bytes"}), "current_runtime_receipt_pin")
    if "bytes" in value:
        need(type(value["bytes"]) is int and value["bytes"] >= 0, "current_runtime_receipt_bytes")
    return descriptor({key: value[key] for key in ("path", "sha256")})


def exact_counts(value, expected):
    return isinstance(value, dict) and all(type(value.get(key)) is int and value[key] == number for key, number in expected.items())


def validate_current_runtime_schema(envelope):
    """Keep both accepted records intact; allow only two fixed recovery edges."""
    need(isinstance(envelope, dict) and type(envelope.get("version")) is int and
         envelope["version"] in (1, 2) and envelope.get("kind") == "audited-candidate-current-runtime-binding" and
         envelope.get("status") == "reviewed_current_runtime" and
         set(envelope) == (CURRENT_RUNTIME_KEYS if envelope["version"] == 1 else CURRENT_RUNTIME_V2_KEYS), "current_runtime_schema")
    if envelope["version"] == 2:
        need(envelope["previousCurrentRuntime"] in (CURRENT_RUNTIME_V1_PREDECESSOR, CURRENT_RUNTIME_V2_PREDECESSOR),
             "current_runtime_previous_pin")
    return envelope["version"]


def validate_lease_loss_configuration(configuration, execution, result, intent, candidate, *, previous_configuration=None):
    """Use the retained hash projection without inventing configuration parsing facts."""
    need(isinstance(configuration, dict) and set(configuration) == {"kind", "version", "status", "candidates", "source", "metadata",
         "independentReview", "environmentDecoded", "masterKeyContentRead", "newFileReads", "requiresFreshHashMatchBeforeStart"} and
         configuration["kind"] == "candidate-lease-loss-restart-configuration-projection" and type(configuration["version"]) is int and
         configuration["version"] == 1 and configuration["status"] == "derived_from_reviewed_readonly_metadata" and
         all(configuration[key] is False for key in ("environmentDecoded", "masterKeyContentRead", "newFileReads")) and
         configuration["requiresFreshHashMatchBeforeStart"] is True, "current_runtime_lease_loss_configuration")
    for key in ("source", "metadata", "independentReview"):
        need(isinstance(configuration[key], dict) and set(configuration[key]) == {"path", "sha256", "bytes"},
             "current_runtime_configuration_source")
        receipt_descriptor(configuration[key])
    pins = execution.get("preservationInputs", {})
    if previous_configuration is None:
        need(receipt_descriptor(configuration["source"]) == pins.get("readonly-execution.json") and
             receipt_descriptor(configuration["independentReview"]) == pins.get("readonly-independent-review.json"),
             "current_runtime_configuration_source")
    else:
        # The accepted predecessor already validates this projection's original
        # provenance. Its bytes are reused, not relabeled as a new SQL product.
        need(canonical(configuration) == canonical(previous_configuration) and
             pins.get("restart-configuration-observation.json") == CURRENT_RUNTIME_SECOND_RECOVERY["configuration"],
             "current_runtime_configuration_lineage")
    cfg = configuration["candidates"]
    need(isinstance(cfg, dict) and set(cfg) == {"old", "fresh"}, "current_runtime_configuration_candidates")
    metadata_keys = {"bytes", "ctimeNs", "device", "gid", "inode", "links", "mode", "mtimeNs", "type", "uid"}
    roots = {"old": C, "fresh": Path("/opt/goby-audited-candidate-20260914T083143Z-9b73ad46f2e6")}
    for label, entries in cfg.items():
        root = roots[label]
        unit = "goby-audited-" + root.name.removeprefix("goby-audited-candidate-")
        paths = {"environment": str(root / "private/runtime.env"), "postgresConfiguration": str(root / "postgres/server.conf"),
                 "unitFile": "/run/systemd/system/" + unit + "-server.service", "postgresUnitFile": "/run/systemd/system/" + unit + "-postgres.service"}
        need(isinstance(entries, dict) and set(entries) == set(paths), "current_runtime_configuration_files")
        for key, row in entries.items():
            need(isinstance(row, dict) and set(row) == {"path", "sha256", "bytes", "metadata"} and row["path"] == paths[key] and
                 type(row["bytes"]) is int and 0 < row["bytes"] <= 65536 and isinstance(row["metadata"], dict) and
                 set(row["metadata"]) == metadata_keys and all(type(number) is int and number >= 0 for number in row["metadata"].values()) and
                 row["metadata"]["bytes"] == row["bytes"] and row["metadata"]["type"] == stat.S_IFREG and
                 row["metadata"]["links"] == 1, "current_runtime_configuration_file_metadata")
            receipt_descriptor({key: row[key] for key in ("path", "sha256", "bytes")})
    before, after = execution["before"], execution["after"]
    expected_files = {str(root / "install/goby") for root in roots.values()} | {
        "/opt/goby-dev/goby", "/var/lib/goby-test/application-key-vault/master.key",
        "/opt/goby-test/exec-work-m3e/main-deployment-schema25.lock"}
    need(isinstance(before.get("files"), dict) and set(before["files"]) == expected_files and
         canonical(before["files"]) == canonical(after["files"]) == canonical(result["after"]["files"]) and
         canonical(before["configurationHashes"]) == canonical(after["configurationHashes"]) ==
             canonical(result["after"]["configurationHashes"]) == canonical(cfg) and
         canonical(before["livePostmasters"]) == canonical(after["livePostmasters"]) == canonical(result["after"]["livePostmasters"]),
         "current_runtime_lease_loss_preservation")
    need(canonical(intent["configuration"]) == canonical(cfg["old"]) and
         canonical(before["units"][intent["unit"]]) == canonical(intent["oldUnit"]) and
         receipt_descriptor({key: cfg["old"]["environment"][key] for key in ("path", "sha256")}) == candidate["runtime"] and
         receipt_descriptor({key: cfg["old"]["unitFile"][key] for key in ("path", "sha256")}) == candidate["units"]["server"] and
         receipt_descriptor({key: cfg["old"]["postgresUnitFile"][key] for key in ("path", "sha256")}) == candidate["units"]["postgres"],
         "current_runtime_recovery_configuration")


def validate_current_identity(current, product_epoch, *, require_lease_start=True):
    """Accept only a replacement application identity, never a product epoch edit."""
    need(isinstance(current, dict) and set(current) == CURRENT_IDENTITY_KEYS, "current_runtime_identity_schema")
    candidate, old = product_epoch["candidate"], product_epoch["candidateProcess"]
    process = current["candidateProcess"]
    need(isinstance(process, dict) and set(process) == set(old) and
         all(canonical(process[key]) == canonical(old[key]) for key in set(old) - {"pid", "startTicks"}), "current_runtime_process_boundary")
    need(type(process["pid"]) is int and process["pid"] > 1 and process["pid"] != old["pid"] and
         isinstance(process["startTicks"], str) and re.fullmatch(r"[1-9][0-9]*", process["startTicks"]) and
         int(process["startTicks"]) > int(old["startTicks"]), "current_runtime_replacement_identity")
    properties = current["serverProperties"]
    previous = candidate["processes"]["server"]
    need(isinstance(properties, dict) and set(properties) == set(previous) and
         all(canonical(properties[key]) == canonical(previous[key]) for key in set(previous) - {"MainPID", "InvocationID"}) and
         properties["MainPID"] == str(process["pid"]) and isinstance(properties["InvocationID"], str) and
         re.fullmatch(r"[0-9a-f]{32}", properties["InvocationID"]) and properties["InvocationID"] != previous["InvocationID"], "current_runtime_server_properties")
    need(canonical(current["serverIdentity"]) == canonical({**candidate["serverIdentity"], "pid": process["pid"],
         "startTicks": process["startTicks"], "invocationId": properties["InvocationID"]}), "current_runtime_server_identity")
    listener = current["listener"]
    need(isinstance(listener, dict) and set(listener) == set(candidate["listener"]) and
         all(canonical(listener[key]) == canonical(candidate["listener"][key]) for key in set(listener) - {"pid", "socketInode"}) and
         listener["pid"] == process["pid"] and isinstance(listener["socketInode"], str) and
         re.fullmatch(r"[1-9][0-9]*", listener["socketInode"]), "current_runtime_listener")
    need(canonical(current["postgresProcess"]) == canonical(product_epoch["postgresProcess"]) and
         canonical(current["postgresProperties"]) == canonical(candidate["processes"]["postgres"]), "current_runtime_postgres_continuity")
    lease = current["lease"]
    lease_keys = CURRENT_LEASE_KEYS if require_lease_start else CURRENT_LEASE_KEYS - {"backendStart"}
    need(isinstance(lease, dict) and set(lease) == lease_keys and
         all(canonical(lease[key]) == canonical(product_epoch["lease"][key]) for key in CURRENT_LEASE_KEYS -
             {"backendPid", "backendStart", "clientPort", "candidateConnection"}) and lease["granted"] is True and
         lease["mode"] == "ExclusiveLock", "current_runtime_lease_schema")
    need(type(lease["backendPid"]) is int and lease["backendPid"] > 1 and type(lease["clientPort"]) is int and
         0 < lease["clientPort"] < 65536, "current_runtime_lease_identity")
    if require_lease_start:
        try:
            need(isinstance(lease["backendStart"], str), "current_runtime_lease_start")
            started = datetime.fromisoformat(lease["backendStart"].replace("Z", "+00:00"))
            need(started.tzinfo is not None, "current_runtime_lease_start")
        except (TypeError, ValueError) as error:
            raise ContractError("current_runtime_lease_start") from error
    connection = lease["candidateConnection"]
    need(isinstance(connection, dict) and set(connection) == {"pid", "localPort", "remotePort", "socketInode"} and
         connection["pid"] == process["pid"] and connection["localPort"] == lease["clientPort"] and
         connection["remotePort"] == candidate["ports"]["postgres"] and isinstance(connection["socketInode"], str) and
         re.fullmatch(r"[1-9][0-9]*", connection["socketInode"]), "current_runtime_lease_socket")
    return current


def validate_current_runtime(envelope, value, product_epoch, records):
    need(type(value.get("version")) is int and value["version"] == 5 and value.get("scenario") == "tv-browse", "current_runtime_tv_input")
    return _validate_current_runtime_records(envelope, value, product_epoch, records)


def validate_second_recovery_review(review, execution, recovery):
    """Read the Sep16 review in its actual shape, without inventing old flags."""
    need(recovery == CURRENT_RUNTIME_SECOND_RECOVERY, "current_runtime_second_recovery_pins")
    keys = {"kind", "version", "status", "applications", "boundedStartup", "candidateEvidence", "closure", "counts", "createdAtUtc",
            "dispatch", "evidenceLimits", "execution", "outerStreamReadback", "postStartReview", "preflight", "preservationInputs",
            "protected", "rawSqlPins", "readbackPinBytes", "readbackPinCount", "reviewMethod", "rows", "scope", "scopeExclusions", "source"}
    need(isinstance(review, dict) and set(review) == keys and
         review["kind"] == "candidate-original-application-restart-independent-review" and type(review["version"]) is int and
         review["version"] == 1 and review["status"] == "passed" and
         receipt_descriptor(review["execution"]) == recovery["execution"] and
         review["source"] == {"path": str(CURRENT_RUNTIME_SECOND_ROOT / "private/candidate-disk-full-restart.py"),
             "sha256": "c5cd36eb66d19f94adb3205929b7164e14e53a8fba2c84e2a73e3dc536f0b60a", "bytes": 39779},
         "current_runtime_second_recovery_review")
    closure_flags = {"allEightSqlBackendsGoneAsCaptured", "allOuterCommandsClosed", "allReaderFrontendGroupsClosed",
                     "allSqlCommitsAcknowledged", "dispatchExitedZero", "dispatchPidAbsentAsCaptured", "dispatchWaited",
                     "lockFdCloseSucceeded", "lockReleased", "lockUnlockSucceeded"}
    need(set(review["closure"]) == closure_flags | {"receiptErrors", "unneededStops"} and
         all(review["closure"][key] is True for key in closure_flags) and review["closure"]["receiptErrors"] == [] and
         exact_counts(review["closure"], {"unneededStops": 0}) and
         canonical(review["reviewMethod"]) == canonical({"savedFileReadsOnly": True, "testsReplayed": False,
             "httpCalls": 0, "nativeBodyReads": 0, "serviceActions": 0, "sqlCalls": 0, "targetModulesExecuted": 0}),
         "current_runtime_second_recovery_closure")
    counts = {"applicationStarts": 2, "databases": 4, "healthResponses": 4, "outerCommands": 78, "outerRawStreamsRead": 156,
              "outerUnitShowCommands": 76, "postgresRestarts": 0, "rawSqlStreamsRead": 8, "readOnlySqlSessions": 8,
              "readerCommands": 28, "readerMetadataCommands": 20, "sequencesPerDatabase": 5, "tablesPerDatabase": 35,
              "uniqueDeploymentLeases": 2}
    protected_flags = {"historicalMainAndDeploymentLockHashesExact", "oldFailedInvocationsPreservedInBefore",
                       "onlyTwoApplicationInvocationsChanged", "referenceUnitAndProcessExact"}
    protected_counts = {"unchangedConfigurationHashes": 8, "unchangedNonApplicationUnits": 5,
                        "unchangedPostmasters": 3, "unchangedProtectedFiles": 5}
    need(set(review["counts"]) == set(counts) and exact_counts(review["counts"], counts) and
         len(execution["commands"]) == counts["outerCommands"] and
         set(review["protected"]) == protected_flags | set(protected_counts) and
         all(review["protected"][key] is True for key in protected_flags) and exact_counts(review["protected"], protected_counts),
         "current_runtime_second_recovery_preservation")
    # This pinned source/review establishes the successful native control path.
    # No new native-body or post-start diagnostics observation is implied here.
    need(set(review["preservationInputs"]) == set(execution["preservationInputs"]) and
         all(receipt_descriptor(pin) == execution["preservationInputs"][name] for name, pin in review["preservationInputs"].items()) and
         {"native-execution.json", "native-independent-review.json", "readonly-execution.json", "readonly-checkpoint.json",
          "auxiliary-execution.json", "auxiliary-independent-review.json", "restart-configuration-observation.json"} <= set(review["preservationInputs"]) and
         review["postStartReview"].get("included") is False and execution["postStartLogReadbackExecuted"] is False,
         "current_runtime_second_preservation_inputs")
    need(set(review["applications"]) == set(review["candidateEvidence"]) == set(execution["candidates"]) == {"old", "fresh"} and
         review["rows"] == {"old_source": 230, "old_recovery": 147, "fresh_source": 141, "fresh_recovery": 131},
         "current_runtime_second_candidate_scope")
    for label, restored in execution["candidates"].items():
        application, evidence = review["applications"][label], review["candidateEvidence"][label]
        process = restored["identity"]["process"]
        need(application == {"pid": process["pid"], "startTicks": process["startTicks"],
             "invocationId": restored["claimedUnit"]["InvocationID"], "binarySha256": restored["startIntent"]["binarySha256"]} and
             restored["label"] == label and restored["status"] == "restored_and_preserved" and restored["startRequested"] is True and
             restored["cleanupErrors"] == restored["readerEvidenceErrors"] == [] and exact_counts(restored, {"sqlSessions": 4}) and
             all(restored[key] is True for key in ("frontendGroupsClosed", "readProcessesClosed", "readerEvidenceSaved", "sqlResultsClosed")) and
             len(restored["health"]) == 2 and all(row["status"] == 200 and row["complete"] is True for row in restored["health"]) and
             set(evidence) == {"commands", "sqlClosure", "snapshots"} and
             all(evidence[key] == restored[key] for key in ("commands", "sqlClosure")), "current_runtime_second_candidate_review")
        need(set(evidence["snapshots"]) == set(restored["snapshots"]) == {"source", "recovery"}, "current_runtime_second_snapshot_scope")
        for slot in ("source", "recovery"):
            snapshot = restored["snapshots"][slot]
            need(snapshot["equal"] is True and exact_counts(snapshot, {"tables": 35, "sequences": 5, "rows": review["rows"][label + "_" + slot]}) and
                 evidence["snapshots"][slot] == {key: snapshot[key] for key in ("before", "after")}, "current_runtime_second_snapshot_review")


def _validate_current_runtime_records(envelope, value, product_epoch, records):
    """Validate saved recovery and a separately recorded current observation only."""
    version = validate_current_runtime_schema(envelope)
    second_recovery = version == 2 and envelope["previousCurrentRuntime"] == CURRENT_RUNTIME_V2_PREDECESSOR
    for key in CURRENT_AUTHORITY_KEYS:
        descriptor(envelope[key])
        need(canonical(envelope[key]) == canonical(value[key]), "current_runtime_historical_authority")
    validate_epoch(product_epoch)
    need(product_epoch["version"] == 3 and canonical(records["runtimeEpoch"]) == canonical(product_epoch), "current_runtime_product_epoch")
    need(canonical(records["seedBinding"]["runtimeEpoch"]) == canonical(envelope["runtimeEpoch"]), "current_runtime_seed_epoch")
    admission = records["admission"]
    need(admission.get("status") == "admitted_for_core_client" and admission.get("candidateAdmissionComplete") is True and
         admission.get("failure", "missing") is None and admission.get("cleanupFailures") == [] and
         canonical(admission.get("runtimeEpoch")) == canonical(envelope["runtimeEpoch"]) and
         canonical(admission.get("seedRuntimeBinding")) == canonical(envelope["seedBinding"]) and
         canonical(admission.get("currentSource")) == canonical(product_epoch["currentSource"]), "current_runtime_prior_admission")
    closeout = records["admissionCloseout"]
    need(closeout.get("status") == "admitted_for_core_client" and closeout.get("candidateAdmissionComplete") is True and
         canonical(closeout.get("admission")) == canonical(envelope["admission"]) and
         canonical(closeout.get("runtimeEpoch")) == canonical(envelope["runtimeEpoch"]) and
         canonical(closeout.get("seedRuntimeBinding")) == canonical(envelope["seedBinding"]) and
         canonical(closeout.get("currentSource")) == canonical(product_epoch["currentSource"]), "current_runtime_prior_closeout")
    candidate = product_epoch["candidate"]
    need(canonical(envelope["preserved"]) == canonical({key: candidate[key] for key in ("binary", "runtime", "units")}), "current_runtime_preserved_descriptors")
    current = validate_current_identity(envelope["current"], product_epoch)
    previous = None
    if version == 2:
        previous = records.get("previousCurrentRuntime")
        need(validate_current_runtime_schema(previous) == (2 if second_recovery else 1) and
             isinstance(records.get("previousRecords"), dict) and
             (not second_recovery or previous["previousCurrentRuntime"] == CURRENT_RUNTIME_V1_PREDECESSOR), "current_runtime_previous_version")
        # The only extra edge is the fixed Sep15 v2, whose parent must be v1.
        _validate_current_runtime_records(previous, value, product_epoch, records["previousRecords"])
        need(all(canonical(previous[key]) == canonical(envelope[key]) for key in (*CURRENT_AUTHORITY_KEYS, "preserved")),
             "current_runtime_previous_authority")
        need(current["candidateProcess"]["pid"] != previous["current"]["candidateProcess"]["pid"] and
             int(current["candidateProcess"]["startTicks"]) > int(previous["current"]["candidateProcess"]["startTicks"]) and
             current["serverProperties"]["InvocationID"] != previous["current"]["serverProperties"]["InvocationID"],
             "current_runtime_previous_identity")
    recovery = envelope["recovery"]
    need(isinstance(recovery, dict) and set(recovery) == CURRENT_RECOVERY_KEYS, "current_runtime_recovery_schema")
    for pin in recovery.values():
        descriptor(pin)
    if version == 2:
        need(all(isinstance(records.get(key), dict) for key in CURRENT_RECOVERY_KEYS), "current_runtime_recovery_records")
    execution, review, configuration, intent, result = (records[key] for key in
        ("execution", "independentReview", "configuration", "selectedStartIntent", "selectedResult"))
    execution_kind = ("candidate-oom-exit-original-application-restart" if second_recovery else
                      "candidate-disk-full-original-application-restart" if version == 1 else "candidate-lease-loss-original-application-restart")
    need(execution.get("kind") == execution_kind and type(execution.get("version")) is int and
         execution["version"] == 1 and execution.get("status") == "both_original_applications_restored" and
         exact_counts(execution, {"applicationStartRequests": 2, "postgresRestartRequests": 0, "binaryOrEnvironmentChanges": 0,
             "businessHttpRequests": 0, "loginRequests": 0}) and
         execution.get("lockAcquired") is True and execution.get("lockReleased") is True and execution.get("receiptErrors") == [] and
         execution.get("createdPaths") == [] and execution.get("ownedUnits") == {} and
         canonical(execution.get("candidates", {}).get("old")) == canonical(result), "current_runtime_recovery_execution")
    need(isinstance(execution.get("commands"), list) and len(execution["commands"]) > 0 and
         all(isinstance(command, dict) and command.get("closed") is True and exact_counts(command, {"exitCode": 0})
             for command in execution["commands"]), "current_runtime_recovery_command_closure")
    if second_recovery:
        validate_second_recovery_review(review, execution, recovery)
        reviewed = review["applications"]["old"]
    else:
        review_kind = "candidate-disk-full-restart-independent-review" if version == 1 else "candidate-lease-loss-restart-independent-review"
        need(review.get("kind") == review_kind and type(review.get("version")) is int and
             review["version"] == 1 and review.get("status") == "passed" and review.get("failure", "missing") is None and
             receipt_descriptor(review["reviewedReceipt"]) == recovery["execution"] and
             all(review.get(key) is True for key in ("allReaderClosuresVerified", "originalPostmastersPreserved", "lockAcquired", "lockReleased",
                 "protectedFileMetadataPreserved", "unrequestedUnitsUnchanged", "firstRestoredApplicationPreservedThroughSecond")) and
             exact_counts(review, {key: 0 for key in ("applicationStopRequests", "postgresRestartRequests", "newHttpRequests", "newSqlSessions", "serviceChanges")}),
             "current_runtime_recovery_review")
        need(all(review.get("nativePreservation", {}).get(key) is True for key in
                 ("mandatoryBeforeAndAfterNativeChecksInSuccessfulPinnedControlFlow", "priorNativeChecksPassed")), "current_runtime_native_preservation")
        reviewed = review["candidates"]["old"]
        need(receipt_descriptor(reviewed["result"]) == recovery["selectedResult"] and
             receipt_descriptor(reviewed["startIntent"]) == recovery["selectedStartIntent"] and
             all(reviewed.get(key) is True for key in ("allQueryIntentResultClosureHashesMatched", "allReaderStreamsCompleteAndGroupsClosed",
                 "allSqlBackendsRecordedGoneAndCommitAcknowledged", "allSqlStdoutAndSavedResultsMatched", "applicationIdentityMatchesFinalUnit",
                 "claimedInvocationMatchesIntent", "healthAndReadinessStatus200", "ownedListenerAndLeaseSocketBound", "uniqueLeaseResponseMatchedOriginalStdout")),
             "current_runtime_selected_review")
    need(result.get("label") == "old" and result.get("status") == "restored_and_preserved" and result.get("startRequested") is True and
         result.get("cleanupErrors") == [] and result.get("readerEvidenceErrors") == [] and
         all(result.get(key) is True for key in ("frontendGroupsClosed", "readProcessesClosed", "readerEvidenceSaved", "sqlResultsClosed")) and
         canonical(result["startIntent"]) == canonical(intent), "current_runtime_selected_result")
    for slot in (() if second_recovery else ("source", "recovery")):
        snapshot, reviewed_snapshot = result["snapshots"][slot], reviewed["snapshots"][slot]
        need(snapshot.get("equal") is True and snapshot.get("tables") == 35 and snapshot.get("sequences") == 5 and
             reviewed_snapshot.get("allLogicalFieldsEqual") is True and reviewed_snapshot.get("excludedFields") == ["capturedAt"] and
             type(snapshot.get("rows")) is int and snapshot["rows"] > 0 and reviewed_snapshot.get("rows") == snapshot["rows"] and
             reviewed_snapshot.get("beforeSha256") == snapshot["before"]["sha256"] and
             reviewed_snapshot.get("afterSha256") == snapshot["after"]["sha256"], "current_runtime_recovery_preservation")
    config = configuration["candidates"]["old"]
    if version == 2:
        validate_lease_loss_configuration(configuration, execution, result, intent, candidate,
            previous_configuration=records["previousRecords"]["configuration"] if second_recovery else None)
        if second_recovery:
            need(recovery["configuration"] == previous["recovery"]["configuration"], "current_runtime_configuration_lineage")
        need(intent["binarySha256"] == candidate["binary"]["sha256"] and intent["unit"] == previous["current"]["serverProperties"]["Id"] and
             intent["oldUnit"]["InvocationID"] == previous["current"]["serverProperties"]["InvocationID"] and
             intent["oldUnit"]["ExecMainPID"] == str(previous["current"]["candidateProcess"]["pid"]) and
             intent["oldUnit"]["MainPID"] == "0" and intent["oldUnit"]["ActiveState"] == "failed", "current_runtime_previous_start_intent")
    else:
        need(configuration.get("kind") == "candidate-restart-configuration-observation" and configuration.get("status") == "observed" and
             configuration.get("configurationContentPublished") is False and canonical(intent["configuration"]) == canonical(config) and
             intent["binarySha256"] == candidate["binary"]["sha256"] and intent["unit"] == candidate["processes"]["server"]["Id"] and
             intent["oldUnit"]["InvocationID"] == candidate["processes"]["server"]["InvocationID"] and
             intent["oldUnit"]["ExecMainPID"] == str(product_epoch["candidateProcess"]["pid"]) and intent["oldUnit"]["MainPID"] == "0" and
             config.get("sameInstalledArgv") is True and config["environment"].get("matchesHistoricalDigest") is True and
             config["environment"].get("parsed") is False and config["environment"].get("rawRetained") is False and
             {key: config["environment"][key] for key in ("path", "sha256")} == candidate["runtime"] and
             {key: config["unitFile"][key] for key in ("path", "sha256")} == candidate["units"]["server"], "current_runtime_recovery_configuration")
    process = current["candidateProcess"]
    recovered_process = result["identity"]["process"]
    need(all(canonical(recovered_process[key]) == canonical(process[key]) for key in set(recovered_process) - {"cgroup"}) and
         recovered_process["cgroup"].rstrip("\n") == process["cgroup"].rstrip("\n") and
         result["identity"]["executableDevice"] == process["exeDevice"] and result["identity"]["executableInode"] == process["exeInode"] and
         all(result["claimedUnit"][key] == actual for key, actual in current["serverProperties"].items()) and
         reviewed["pid" if second_recovery else "applicationPid"] == process["pid"] and reviewed["startTicks"] == process["startTicks"] and
         reviewed["invocationId"] == current["serverProperties"]["InvocationID"] and
         result["listener"] == {"localPort": current["listener"]["port"], "remotePort": None, "socketInode": current["listener"]["socketInode"]},
         "current_runtime_recovery_identity")
    lease = current["lease"]
    need(canonical(result["lease"]["facts"]) == canonical({key: lease[key] for key in CURRENT_LEASE_KEYS - {"backendStart", "candidateConnection"}}) and
         canonical(result["lease"]["applicationSocket"]) == canonical({key: lease["candidateConnection"][key] for key in ("localPort", "remotePort", "socketInode")}) and
         (second_recovery or reviewed["leaseBackendPid"] == lease["backendPid"]) and result["cluster"]["systemIdentifier"] == candidate["clusterSystemIdentifier"] and
         result["cluster"]["port"] == str(candidate["ports"]["postgres"]), "current_runtime_recovery_lease")
    observation = records["observation"]
    need(isinstance(observation, dict) and set(observation) == CURRENT_OBSERVATION_KEYS and
         observation["kind"] == "audited-candidate-current-runtime-observation" and type(observation["version"]) is int and observation["version"] == 1 and
         canonical(observation["runtimeEpoch"]) == canonical(envelope["runtimeEpoch"]) and
         canonical(observation["recoveryExecution"]) == canonical(recovery["execution"]) and canonical(observation["hosting"]) == canonical(envelope["hosting"]) and
         canonical(observation["before"]) == canonical(current) == canonical(observation["after"]) and
         canonical(observation["preserved"]) == canonical(envelope["preserved"]), "current_runtime_observation_binding")
    try:
        captured = datetime.fromisoformat(observation["capturedAt"].replace("Z", "+00:00"))
        started = datetime.fromisoformat(lease["backendStart"].replace("Z", "+00:00"))
        need(captured.tzinfo is not None and started <= captured, "current_runtime_observation_time")
    except (TypeError, ValueError) as error:
        raise ContractError("current_runtime_observation_time") from error
    calls = observation["calls"]
    need(isinstance(calls, dict) and set(calls) == {"sql", "http", "browser", "service", "seed", "admission", "provision"} and
         all(type(number) is int for number in calls.values()) and calls["sql"] == 2 and
         all(number == 0 for key, number in calls.items() if key != "sql"), "current_runtime_observation_calls")
    hosting = records["hosting"]
    host_identity = {"process": hosting["process"], "listener": hosting["listener"],
                     "unit": {key: hosting["unitProperties"][key] for key in CURRENT_HOST_UNIT_FIELDS}}
    need(canonical(observation["hostingBefore"]) == canonical(host_identity) == canonical(observation["hostingAfter"]), "current_runtime_hosting_continuity")
    need(canonical(records["leaseQueryResult"]) == canonical([{key: lease[key] for key in CURRENT_LEASE_KEYS - {"candidateConnection"}}]),
         "current_runtime_unique_lease_result")
    independent = records["observationReview"]
    need(isinstance(independent, dict) and set(independent) == CURRENT_OBSERVATION_REVIEW_KEYS and
         independent["kind"] == "audited-candidate-current-runtime-observation-review" and type(independent["version"]) is int and
         independent["version"] == 1 and independent["status"] == "passed" and
         canonical(independent["observation"]) == canonical(envelope["observation"]), "current_runtime_observation_review")
    for key in ("source", "verification"):
        descriptor(observation[key])
        need(canonical(independent[key]) == canonical(observation[key]), "current_runtime_observation_review_source")
    need(isinstance(independent["checks"], dict) and set(independent["checks"]) == CURRENT_OBSERVATION_REVIEW_CHECKS and
         all(check is True for check in independent["checks"].values()) and
         isinstance(independent["calls"], dict) and set(independent["calls"]) == set(calls) and
         all(type(number) is int and number == 0 for number in independent["calls"].values()), "current_runtime_observation_review_incomplete")
    return envelope


def load_current_runtime(pin, value, product_epoch, read_descriptor, read_bytes):
    return _load_current_runtime(pin, value, product_epoch, read_descriptor, read_bytes, validate_current_runtime)


def load_programs_previous_runtime(pin, value, product_epoch, read_descriptor, read_bytes):
    if value.get("kind") == "audited-candidate-state-capture-input":
        validate_programs_capture_input(value)
    else:
        validate_programs_successor_input(value)
    def validate(envelope, unused, previous, records):
        need(envelope["runtimeEpoch"] == value["previousEpoch"] and envelope["seedBinding"] == value["previousBinding"] and
             previous["currentSource"]["binary"]["sha256"] == PROGRAMS_OLD_BINARY, "programs_previous_runtime_authority")
        authority = {key: envelope[key] for key in CURRENT_AUTHORITY_KEYS}
        result = _validate_current_runtime_records(envelope, authority, previous, records)
        if value.get("newSourceArchive") is not None and programs_complete_source(value["newSourceArchive"]):
            validate_programs_closeout_runtime(value, envelope, CURRENT_RUNTIME_V1_PREDECESSOR)
        return result
    return _load_current_runtime(pin, value, product_epoch, read_descriptor, read_bytes, validate)


def _load_current_runtime(pin, value, product_epoch, read_descriptor, read_bytes, validator):
    """Read hash-bound saved records without a live query or configuration decode."""
    descriptor(pin)
    need(canonical(value.get("currentRuntime")) == canonical(pin), "current_runtime_input_pin")
    def checked(selected, expected_bytes=None):
        descriptor(selected)
        need(Path(selected["path"]).is_relative_to("/opt/goby-test") and Path(selected["path"]).suffix == ".json", "current_runtime_record_scope")
        raw = read_bytes(selected)
        need(isinstance(raw, bytes) and len(raw) <= 4 << 20 and digest(raw) == selected["sha256"], "current_runtime_record_digest")
        if expected_bytes is not None:
            need(type(expected_bytes) is int and len(raw) == expected_bytes, "current_runtime_record_size")
        def unique_pairs(pairs):
            result = dict(pairs)
            need(len(result) == len(pairs), "current_runtime_duplicate_key")
            return result
        parsed = json.loads(raw, object_pairs_hook=unique_pairs)
        need(canonical(parsed) == canonical(read_descriptor(selected)), "current_runtime_record_readback")
        return parsed
    def read_records(selected_envelope):
        selected_version = validate_current_runtime_schema(selected_envelope)
        need(isinstance(selected_envelope["recovery"], dict) and set(selected_envelope["recovery"]) == CURRENT_RECOVERY_KEYS, "current_runtime_schema")
        records = {key: checked(selected_envelope[key]) for key in CURRENT_AUTHORITY_KEYS}
        records.update({key: checked(selected) for key, selected in selected_envelope["recovery"].items()})
        records["observation"] = checked(selected_envelope["observation"])
        records["observationReview"] = checked(selected_envelope["observationReview"])
        records["leaseQueryResult"] = checked(records["observation"]["leaseQueryResult"])
        observation_source = descriptor(records["observation"]["source"])
        need(Path(observation_source["path"]).is_relative_to("/opt/goby-test") and Path(observation_source["path"]).suffix == ".py",
             "current_runtime_observation_source_scope")
        source_bytes = read_bytes(observation_source)
        need(isinstance(source_bytes, bytes) and 0 < len(source_bytes) <= 4 << 20 and digest(source_bytes) == observation_source["sha256"],
             "current_runtime_observation_source_digest")
        checked(records["observation"]["verification"])
        if selected_version == 2:
            for key in ("source", "metadata", "independentReview"):
                proof = records["configuration"][key]
                checked(receipt_descriptor(proof), proof.get("bytes"))
        return records
    try:
        envelope = checked(pin)
        records = read_records(envelope)
        if envelope["version"] == 2:
            previous = checked(envelope["previousCurrentRuntime"])
            second_recovery = envelope["previousCurrentRuntime"] == CURRENT_RUNTIME_V2_PREDECESSOR
            need(validate_current_runtime_schema(previous) == (2 if second_recovery else 1), "current_runtime_previous_version")
            records["previousCurrentRuntime"] = previous
            records["previousRecords"] = read_records(previous)
            if second_recovery:
                need(previous["previousCurrentRuntime"] == CURRENT_RUNTIME_V1_PREDECESSOR, "current_runtime_previous_pin")
                original = checked(CURRENT_RUNTIME_V1_PREDECESSOR)
                need(validate_current_runtime_schema(original) == 1, "current_runtime_previous_version")
                records["previousRecords"]["previousCurrentRuntime"] = original
                records["previousRecords"]["previousRecords"] = read_records(original)
        validator(envelope, value, product_epoch, records)
        return deepcopy(envelope)
    except ContractError:
        raise
    except (KeyError, TypeError, ValueError, OverflowError) as error:
        raise ContractError("current_runtime_record_invalid") from error


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

    def runtime_identity(self):
        return self.epoch

    def pin(self):
        current = self.runtime_identity()
        for role, expected in (("server", current["candidateProcess"]), ("postgres", current["postgresProcess"])):
            observed = self.provision.show(self.provision.units[role])
            need(observed == self.candidate["processes"][role] and self.modules["gateway"].metadata(expected["pid"]) == expected and
                 self.provision.process(role, observed) == self.candidate[role + "Identity"], "runtime_epoch_process_changed")
        listener = self.candidate["listener"]
        self.modules["gateway"].verify_listener(current["candidateProcess"]["pid"], {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]})
        return {**current["candidateProcess"], "listener": {"host": "127.0.0.1", "port": listener["port"], "socketInode": listener["socketInode"]}}

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
        row, pid = rows[0], self.runtime_identity()["candidateProcess"]["pid"]
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


class RecoveredEpochReader(EpochReader):
    """Reuse current metadata readers without decoding historical configuration."""
    def __init__(self, product_epoch, modules, private, current_runtime):
        self.currentRuntime = deepcopy(current_runtime)
        validate_current_runtime_schema(self.currentRuntime)
        self._initialize(product_epoch, modules, private, self.currentRuntime["current"], self.currentRuntime["preserved"], observation_only=False)

    @classmethod
    def for_observation(cls, product_epoch, modules, private, current_facts, preserved):
        """Serve the separately admitted one-lease-call observer; never create a ready envelope."""
        reader = cls.__new__(cls)
        reader.currentRuntime = None
        reader._initialize(product_epoch, modules, private, current_facts, preserved, observation_only=True)
        return reader

    def _initialize(self, product_epoch, modules, private, current, preserved, *, observation_only):
        self.productEpoch = self.epoch = validate_epoch(product_epoch)
        self.modules, self.observationOnly = modules, observation_only
        self._observationLeaseQueries = 0
        self.current = deepcopy(validate_current_identity(current, product_epoch, require_lease_start=not observation_only))
        need(canonical(preserved) == canonical({key: product_epoch["candidate"][key] for key in ("binary", "runtime", "units")}),
             "current_runtime_preserved_descriptors")
        # read_checked hashes these bytes. Do not decode the environment or call the old constructor.
        for pin in (preserved["binary"], preserved["runtime"], *preserved["units"].values()):
            modules["seed"].read_checked(pin["path"], pin["sha256"])
        self.manifest = self.candidate = deepcopy(product_epoch["candidate"])
        self.candidate["processes"]["server"] = deepcopy(self.current["serverProperties"])
        self.candidate["serverIdentity"] = deepcopy(self.current["serverIdentity"])
        self.candidate["listener"] = deepcopy(self.current["listener"])
        p = modules["provision"].Provision({"runId": self.candidate["runId"], "ports": self.candidate["ports"]},
                                            product_epoch["transitionInput"], product_epoch["helpers"]["provision"])
        p.private = Path(private)
        p.argv = {"server": [str(C / "install/goby")], "postgres": ["/usr/lib/postgresql/17/bin/postgres", "-D", str(C / "postgres/data"), "-c", "config_file=" + str(C / "postgres/server.conf")]}
        p.pg_identity, p.pg_version = self.candidate["postgresIdentity"], self.candidate["postgresVersionNum"]
        self.provision = p

    def runtime_identity(self):
        return self.current

    def deployment_lease(self):
        # The parent performs a fresh single-row SQL check and actual socket ownership check.
        if self.observationOnly:
            need(self._observationLeaseQueries == 0, "current_runtime_observation_query_consumed")
            self._observationLeaseQueries += 1
        observed = super().deployment_lease()
        compared = {key: value for key, value in observed.items() if key != "backendStart"} if self.observationOnly else observed
        need(canonical(compared) == canonical(self.current["lease"]) and set(observed) == CURRENT_LEASE_KEYS,
             "current_runtime_lease_changed")
        if self.observationOnly:
            validate_current_identity({**self.current, "lease": observed}, self.productEpoch)
        return observed


class EpochIO:
    """The bounded recorder reused with explicit current-epoch authority."""
    def __init__(self, value, input_pin, source_pin, epoch_pin, binding_pin, modules, *, current_runtime=None, current_runtime_pin=None):
        self.value, self.input_pin, self.source_pin, self.modules = value, input_pin, source_pin, modules
        self.seed = modules["seed"]
        self.epoch = validate_epoch(self.seed.descriptor(epoch_pin))
        need((current_runtime is None) == (current_runtime_pin is None), "current_runtime_io_pin_required")
        self.currentRuntime, self.currentRuntimePin = deepcopy(current_runtime), deepcopy(current_runtime_pin)
        if current_runtime is not None:
            validate_current_runtime_schema(current_runtime)
            descriptor(current_runtime_pin)
            need(canonical(self.seed.descriptor(current_runtime_pin)) == canonical(current_runtime) and
                 canonical(current_runtime["runtimeEpoch"]) == canonical(epoch_pin) and canonical(current_runtime["seedBinding"]) == canonical(binding_pin),
                 "current_runtime_io_authority")
            validate_current_identity(current_runtime["current"], self.epoch)
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
        elif self.epoch["version"] == 4:
            need(current_runtime is None, "programs_epoch_does_not_use_recovery_envelope")
            lineage = resolve_programs_successor_lineage(self.epoch, self.seed.descriptor)
            previous = self.seed.descriptor(self.epoch["previousEpoch"])
            prior_binding = self.seed.descriptor(self.binding["previousBinding"])
            validate_seed_runtime_binding(prior_binding, self.epoch["previousEpoch"], previous, original_seed)
            need(canonical(self.binding["currentSessions"]) == canonical(programs_sessions(self.seed.descriptor(self.epoch["sourceAfter"]))) and
                 self.binding["previousBinding"] == lineage["productInput"]["previousBinding"], "programs_binding_sessions_changed")
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
        intent = {"input": self.input_pin, "source": self.source_pin, "runtimeEpoch": self.binding["runtimeEpoch"], "operation": "create-epoch-reader-output"}
        if self.currentRuntimePin is not None:
            intent["currentRuntime"] = self.currentRuntimePin
        self.seed.write_json_once(self.output.with_name(self.output.name + "-intent.json"), intent)
        os.mkdir(self.output, 0o700)
        self.created = True
        os.mkdir(self.private, 0o700)
        self.seed.sync_dir(self.output)
        self.seed.sync_dir(self.output.parent)
        self.reader = (RecoveredEpochReader(self.epoch, self.modules, self.private, self.currentRuntime)
                       if self.currentRuntime is not None else EpochReader(self.epoch, self.modules, self.private))
        if self.currentRuntime is not None:
            self.candidate = self.reader.candidate
        self.provision = self.reader.provision
        self.pin()
        return self

    def pin(self):
        return self.reader.pin()

    def deadline(self, cleanup=False):
        return self.seed.CandidateIO.deadline(self, cleanup)

    def request(self, *args, **kwargs):
        return self.seed.CandidateIO.request(self, *args, **kwargs)
