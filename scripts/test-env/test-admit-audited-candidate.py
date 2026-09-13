#!/usr/bin/env python3
"""Pure admission guards; no process, database, filesystem fixture, or HTTP IO."""
import copy
import importlib.util
from pathlib import Path
import unittest
from types import SimpleNamespace

spec = importlib.util.spec_from_file_location("admission_guards", Path(__file__).with_name("admit-audited-candidate.py"))
admission = importlib.util.module_from_spec(spec)
spec.loader.exec_module(admission)


def pin(name="source"):
    return {"path": "/opt/goby-test/" + name, "sha256": "a" * 64}


def input_value():
    value = {key: pin(key) for key in ("candidateManifest", "seedManifest", "runtimeHelper", "runtimeInspection",
                                     "seedHelper", "admissionHelper", "inspectionHelper", "sourceManifest", "compiledCatalog")}
    value.update(kind="audited-candidate-admission-input", version=1, runId="admission-01",
                 output="/opt/goby-test/work/candidate-live-admission-01", budgets=dict(admission.LIMITS))
    value["sourceManifest"]["sha256"] = admission.SOURCE_MANIFEST_SHA
    value["inspectionHelper"]["sha256"] = admission.INSPECTION_SHA
    return value


def epoch_fixture():
    value = {key: pin(key) for key in ("runtimeEpoch", "seedRuntimeBinding", "runtimeHelper", "admissionHelper", "compiledCatalog")}
    value.update(kind="audited-candidate-admission-input", version=2, runId="epoch-admission-01",
                 output="/opt/goby-test/work/candidate-live-admission-03", budgets=dict(admission.LIMITS))
    business = {key: {} for key in ("admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources")}
    seed = {**business, "serverId": "synthetic-server", "helper": pin("original-seed-executor"), "input": pin("original-seed-input"),
            "candidateManifest": pin("original-provision"), "cleanup": [],
            "source": {"manifestSha256": "1" * 64, "binarySha256": "2" * 64}, "processes": {"candidate": {"pid": 101}}}
    current = {"archiveSha256": admission.EPOCH_ARCHIVE_SHA, "schema": 28, "fullReport": pin("current-full-report"),
               "sourceManifest": {"path": "/opt/goby-test/current-source-manifest", "sha256": "3" * 64},
               "binary": {"path": "/opt/goby-test/installed-binary", "sha256": "4" * 64}}
    helpers = {key: pin("runtime-" + key) for key in ("seed", "provision", "gateway", "admission", "reconcile")}
    epoch = {"version": 1, "runtimeHelper": copy.deepcopy(value["runtimeHelper"]), "seedProvenance": pin("original-seed"),
             "originalProvision": copy.deepcopy(seed["candidateManifest"]), "currentSource": copy.deepcopy(current),
             "transitionInput": pin("transition-input"), "helpers": copy.deepcopy(helpers),
             "candidateProcess": {"pid": 202, "startTicks": "222"},
             "candidate": {"backendReport": copy.deepcopy(current["fullReport"]), "currentSourceManifest": copy.deepcopy(current["sourceManifest"]),
                           "binary": copy.deepcopy(current["binary"]), "input": pin("transition-input"),
                           "serverIdentity": {"pid": 202, "startTicks": "222"},
                           "listener": {"pid": 202, "port": 28498, "socketInode": "333"}, "ports": {"http": 28498}}}
    binding = {**copy.deepcopy(business), "serverId": seed["serverId"], "runtimeEpoch": copy.deepcopy(value["runtimeEpoch"]),
               "originalSeed": copy.deepcopy(epoch["seedProvenance"]), "seedExecutor": copy.deepcopy(seed["helper"]),
               "seedInput": copy.deepcopy(seed["input"]), "seedCleanup": copy.deepcopy(seed["cleanup"])}
    full = {"archive_sha256": admission.EPOCH_ARCHIVE_SHA, "worker": {"binary": {"sha256": "4" * 64}}}
    transition = {"newFullReport": copy.deepcopy(current["fullReport"]), "newSourceManifest": copy.deepcopy(current["sourceManifest"]),
                  "newBinary": {"path": "/opt/goby-test/build-binary", "sha256": "4" * 64},
                  "compiledCatalog": copy.deepcopy(value["compiledCatalog"]), "helpers": copy.deepcopy(helpers)}
    return value, epoch, binding, seed, full, transition


def environment_epoch_fixture():
    values = epoch_fixture()
    value, epoch, binding, _seed, _full, _transition = values
    previous = copy.deepcopy(epoch)
    value["runtimeEpoch"], value["runtimeHelper"] = pin("amended-epoch"), pin("amended-runtime-helper")
    binding["runtimeEpoch"] = copy.deepcopy(value["runtimeEpoch"])
    epoch.update(version=2, operationKind="environment_revision", previousEpoch=pin("previous-epoch"),
                 runtimeHelper=copy.deepcopy(value["runtimeHelper"]), productInput=copy.deepcopy(previous["transitionInput"]),
                 transitionInput=pin("configuration-input"), calls={"stop": 1, "replaceEnvironment": 1, "start": 1})
    epoch["candidate"]["input"] = copy.deepcopy(epoch["transitionInput"])
    epoch["candidateProcess"].update(pid=303, startTicks="333")
    epoch["candidate"]["serverIdentity"].update(pid=303, startTicks="333")
    epoch["candidate"]["listener"]["pid"] = 303
    configuration = {"previousEpoch": copy.deepcopy(epoch["previousEpoch"])}
    return values, previous, configuration


def status(staged=False):
    return {"Available": True, "UnavailableReason": "", "RestoreAvailable": True, "RestoreUnavailableReason": "",
            "Busy": False, "ActiveOperationId": "", "GenerationRevision": "0", "Limits": {
                "MaxBackupBytes": "1024", "MaxStoredBytes": "2048", "MaxBackups": 3,
                "MinPassphraseBytes": 12, "MaxPassphraseBytes": 1024},
            "Storage": {"Bytes": "0", "Objects": 0}, "Rollback": {"Available": False, "MustReplace": staged}}


def operation():
    return {"Id": "1" * 32, "RequestId": "2" * 32, "Revision": "4", "Kind": "restore", "State": "ready", "Phase": "ready",
            "BackupId": "3" * 32, "CreatedAt": "2026-09-13T00:00:00Z", "UpdatedAt": "2026-09-13T00:00:01Z",
            "ErrorCode": "", "Source": None, "RestoreDefaults": False, "ReplaceRollback": False,
            "CanCancel": True, "CanApply": True, "GenerationRevision": "0"}


class AdmissionGuards(unittest.TestCase):
    def test_environment_epoch_reuses_product_with_a_new_process(self):
        values, previous, configuration = environment_epoch_fixture()
        original_seed = copy.deepcopy(values[3])
        current = admission.validate_epoch_admission(*values, product_epoch=previous, configuration_input=configuration)
        self.assertEqual(current["pid"], 303)
        self.assertEqual(values[1]["currentSource"], previous["currentSource"])
        self.assertEqual(values[3], original_seed)

    def test_environment_epoch_cannot_masquerade_as_binary_replacement(self):
        for mutate in (
                lambda values, previous, configuration: values[1].update(calls={"stop": 1, "replace": 1, "start": 1}),
                lambda values, previous, configuration: values[1].update(operationKind="binary_replacement"),
                lambda values, previous, configuration: values[1].update(productInput=pin("wrong-product-input")),
                lambda values, previous, configuration: values[1]["currentSource"]["binary"].update(sha256="b" * 64),
                lambda values, previous, configuration: configuration.update(previousEpoch=pin("wrong-previous-epoch")),
                lambda values, previous, configuration: previous.update(version=2)):
            values, previous, configuration = environment_epoch_fixture()
            mutate(values, previous, configuration)
            with self.assertRaises(admission.AdmissionError):
                admission.validate_epoch_admission(*values, product_epoch=previous, configuration_input=configuration)

    def test_environment_epoch_requires_exact_effective_backup_limits(self):
        epoch = {"version": 2, "operationKind": "environment_revision"}
        value = status()
        value["Limits"].update(MaxBackupBytes="67108864", MaxStoredBytes="268435456")
        admission.validate_effective_backup_limits(value, epoch)
        admission.validate_effective_backup_limits(status(), {"version": 1})
        for field, changed in (("MaxBackupBytes", "8589934592"), ("MaxStoredBytes", "34359738368"),
                               ("MaxBackupBytes", 67108864), ("MaxStoredBytes", 268435456)):
            candidate = copy.deepcopy(value)
            candidate["Limits"][field] = changed
            with self.assertRaises(admission.AdmissionError):
                admission.validate_effective_backup_limits(candidate, epoch)

    def test_v2_input_selects_epoch_without_legacy_runtime_pins(self):
        value, *_ = epoch_fixture()
        admission.validate_input(value)
        legacy = {**value, "inspectionHelper": pin("old-inspector")}
        with self.assertRaises(admission.AdmissionError):
            admission.validate_input(legacy)
        value["budgets"]["maximumRequests"] = 121
        with self.assertRaises(admission.AdmissionError):
            admission.validate_input(value)

    def test_v2_preserves_original_seed_and_selects_current_process(self):
        values = epoch_fixture()
        original = copy.deepcopy(values[3])
        current = admission.validate_epoch_admission(*values)
        self.assertEqual(current, {"pid": 202, "startTicks": "222", "listener": {"host": "127.0.0.1", "port": 28498, "socketInode": "333"}})
        self.assertEqual(values[3], original)
        self.assertEqual(values[3]["processes"]["candidate"]["pid"], 101)

    def test_v2_rejects_cross_epoch_or_original_seed_bindings(self):
        for mutation in (
                lambda values: values[2].update(runtimeEpoch=pin("another-epoch")),
                lambda values: values[2].update(originalSeed=pin("another-seed")),
                lambda values: values[2].update(seedExecutor=pin("another-executor")),
                lambda values: values[2].update(catalog={"unobserved": True}),
                lambda values: values[1].update(runtimeHelper=pin("another-runtime")),
                lambda values: values[1]["candidateProcess"].update(pid=101)):
            values = epoch_fixture()
            mutation(values)
            with self.assertRaises(admission.AdmissionError):
                admission.validate_epoch_admission(*values)

    def test_v2_rejects_full_source_archive_binary_or_catalog_mismatch(self):
        for mutation in (
                lambda values: values[1]["currentSource"].update(archiveSha256="a" * 64),
                lambda values: values[4].update(archive_sha256="a" * 64),
                lambda values: values[4]["worker"]["binary"].update(sha256="a" * 64),
                lambda values: values[1]["candidate"].update(backendReport=pin("another-full-report")),
                lambda values: values[1]["candidate"].update(currentSourceManifest=pin("another-source")),
                lambda values: values[5].update(compiledCatalog=pin("another-catalog")),
                lambda values: values[5]["helpers"].update(seed=pin("another-recorder")),
                lambda values: values[3]["source"].update(manifestSha256="3" * 64)):
            values = epoch_fixture()
            mutation(values)
            with self.assertRaises(admission.AdmissionError):
                admission.validate_epoch_admission(*values)

    def test_native_overview_requires_observed_ready_health(self):
        class ReachedNextApi(Exception):
            pass

        def exercise(health):
            overview = {"Server": {"Id": "synthetic-server"}, "Counts": {"Users": 8, "Libraries": 3},
                        "Database": {"Status": "connected"}, "Transcoding": health}

            def request(_label, _method, route, **_kwargs):
                if route == "/emby/System/Info/Public":
                    raise ReachedNextApi()
                return {"body": overview if route == "/admin/v1/overview" else {}}

            job = SimpleNamespace(server_id="synthetic-server", login=lambda _role: None, req=request)
            admission.Admission.authenticate(job)

        with self.assertRaises(ReachedNextApi):
            exercise({"Configured": True, "Available": True, "Reason": "ready"})
        for health in ({"Configured": True, "Available": True, "Reason": ""},
                       {"Configured": False, "Available": False, "Reason": "disabled"},
                       {"Configured": True, "Available": False, "Reason": "engine_unavailable"},
                       {"Configured": True, "Available": False, "Reason": "cache_unavailable"},
                       {"Configured": True, "Available": False, "Reason": "manager_closed"}):
            with self.assertRaisesRegex(admission.AdmissionError, "native_overview_admission"):
                exercise(health)

    def test_only_captured_raw_actor_policies_are_admitted(self):
        for role in ("admin", "P"):
            admission.validate_actor_policy(role, {"policy": {}})
        admission.validate_actor_policy("Q", {"policy": dict(admission.CONTROL_POLICY)})
        for role, policy in (("P", {"EnableAllFolders": True}), ("P", {"Unknown": False}),
                             ("Q", {}), ("Q", {**admission.CONTROL_POLICY, "Unknown": False}),
                             ("Q", {**admission.CONTROL_POLICY, "EnableAllFolders": True}),
                             ("Q", {**admission.CONTROL_POLICY, "EnableMediaPlayback": 1})):
            with self.assertRaises(admission.AdmissionError):
                admission.validate_actor_policy(role, {"policy": policy})

    def test_captured_album_omits_path_but_binds_empty_storage_path_and_parent(self):
        seed = {"serverId": "server", "catalog": {"album": {"id": "album"}}, "libraries": {"Music": {"Id": "music"}}}
        dto = {"Id": "album", "Type": "MusicAlbum", "ServerId": "server", "ParentId": "music"}
        row = {"id": "album", "type": "MusicAlbum", "path": "", "parent_id": "music"}
        admission.validate_actual_dto(dto, row, seed)
        for changed_dto, changed_row in (({**dto, "Path": ""}, row), (dto, {**row, "path": "/unexpected"}),
                                          ({**dto, "ParentId": "other"}, row), (dto, {**row, "parent_id": "other"}),
                                          ({**dto, "Id": "other"}, {**row, "id": "other"})):
            with self.assertRaises(admission.AdmissionError):
                admission.validate_actual_dto(changed_dto, changed_row, seed)

    def test_other_captured_dtos_still_require_exact_path_and_parent(self):
        seed = {"serverId": "server"}
        row = {"id": "series", "type": "Series", "path": "/series", "parent_id": "television"}
        dto = {"Id": "series", "Type": "Series", "ServerId": "server", "Path": "/series", "ParentId": "television"}
        admission.validate_actual_dto(dto, row, seed)
        for changed in ({key: value for key, value in dto.items() if key != "Path"}, {**dto, "ParentId": "other"}):
            with self.assertRaises(admission.AdmissionError):
                admission.validate_actual_dto(changed, row, seed)

    def test_key_errors_report_only_allowlisted_field_names(self):
        self.assertEqual(admission.safe_failure(KeyError("EnableAllFolders"), "preflight")["field"], "EnableAllFolders")
        result = admission.safe_failure(KeyError("private-token-or-url"), "preflight")
        self.assertEqual(result["field"], "unrecognized_field")
        self.assertNotIn("private-token-or-url", str(result))

    def test_unexpected_native_success_preserves_cleanup_authority(self):
        auth = {"kind": "native", "token": "synthetic-token", "csrf": "synthetic-csrf"}
        helper = SimpleNamespace(response_auth=lambda response, kind: auth)
        self.assertIs(admission.unexpected_native_authority({"status": 200, "complete": False}, helper), auth)
        self.assertIsNone(admission.unexpected_native_authority({"status": 401, "complete": True}, helper))
        self.assertIsNone(admission.unexpected_native_authority(None, helper))

    def test_successful_activity_contract_has_three_download_grants(self):
        logins = {role: {"initialSession": {"id": role + "-session", "user_id": role + "-user",
                                         "kind": "admin" if role == "admin" else "emby"}} for role in ("admin", "P", "Q")}
        rows = admission.expected_activity(logins, "create", "backup", "plan")
        self.assertEqual(sum(rows.values()), 14)
        self.assertEqual(sum(count for row, count in rows.items() if row[0] == "backup.downloaded"), 3)
        self.assertEqual({row[5] for row in rows if row[0].startswith("restore.")}, {"restore"})

    def test_exact_budget_is_required(self):
        admission.validate_input(input_value())
        for key, value in (("maximumRequests", 121), ("cleanupRequests", 9), ("maximumSeconds", 901), ("cleanupSeconds", 179)):
            candidate = input_value()
            candidate["budgets"][key] = value
            with self.assertRaises(admission.AdmissionError):
                admission.validate_input(candidate)

    def test_source_and_inspector_are_frozen(self):
        for key in ("sourceManifest", "inspectionHelper"):
            candidate = input_value()
            candidate[key]["sha256"] = "b" * 64
            with self.assertRaises(admission.AdmissionError):
                admission.validate_input(candidate)

    def test_no_input_within_output(self):
        candidate = input_value()
        candidate["seedManifest"]["path"] = candidate["output"] + "/seed.json"
        with self.assertRaises(admission.AdmissionError):
            admission.validate_input(candidate)

    def test_catalog_cross_binding_and_identifier(self):
        catalog = {"version": 28, "postgresql_major": 17, "migrations": [{"version": value} for value in range(1, 29)],
                   "catalog": {"Tables": [{"Name": "sessions", "Columns": ["id"], "SortKey": ["id"]}],
                               "Sequences": [{"Name": "devices_id_seq", "Increment": 1}]}}
        source = {admission.CATALOG_RELATIVE: {"sha256": "a" * 64}}
        admission.validate_catalog(catalog, source, pin())
        query = admission.snapshot_sql(catalog)
        self.assertTrue(query.startswith("SELECT "))
        self.assertNotIn(";", query)
        with self.assertRaises(admission.AdmissionError):
            admission.validate_catalog(catalog, source, {**pin(), "sha256": "b" * 64})
        catalog["catalog"]["Tables"][0]["Name"] = "sessions;DROP"
        with self.assertRaises(admission.AdmissionError):
            admission.validate_catalog(catalog, source, pin())

    def test_ready_cancel_retains_stage(self):
        admission.validate_status(status(), "0")
        admission.validate_status(status(True), "0", staged=True)
        with self.assertRaises(admission.AdmissionError):
            admission.validate_status(status(), "0", staged=True)
        value = status(True)
        value["Rollback"]["Available"] = True
        with self.assertRaises(admission.AdmissionError):
            admission.validate_status(value, "0", staged=True)

    def test_generation_must_remain_exact(self):
        with self.assertRaises(admission.AdmissionError):
            admission.validate_status(status(), "1")
        value = operation()
        value["GenerationRevision"] = "1"
        with self.assertRaises(admission.AdmissionError):
            admission.validate_ready(value, "0")

    def test_operation_id_request_and_backup_cross_binding(self):
        value = operation()
        admission.validate_operation(value, "restore", "2" * 32, "1" * 32, "3" * 32)
        for args in (("restore", "4" * 32, "1" * 32, "3" * 32),
                     ("restore", "2" * 32, "4" * 32, "3" * 32),
                     ("restore", "2" * 32, "1" * 32, "4" * 32)):
            with self.assertRaises(admission.AdmissionError):
                admission.validate_operation(value, *args)

    def test_cancel_must_be_terminal_without_apply_authority(self):
        value = operation()
        admission.validate_ready(value, "0")
        with self.assertRaises(admission.AdmissionError):
            admission.validate_cancelled(value)
        value.update(State="cancelled", Phase="finished", ErrorCode="operation_cancelled", CanCancel=False, CanApply=False)
        admission.validate_cancelled(value)
        value["CanApply"] = True
        with self.assertRaises(admission.AdmissionError):
            admission.validate_cancelled(value)

    def test_decimal_rejects_noncanonical_or_numeric_revisions(self):
        for value in (0, True, "00", "01", "-1", str(1 << 64)):
            with self.assertRaises(admission.AdmissionError):
                admission.decimal(value)

    def test_download_full_hash_and_declared_length_are_required(self):
        raw = b"age-encryption.org/v1\n-> scrypt salt 18\n" + b"x" * 60
        backup = {"Id": "1" * 32, "SizeBytes": str(len(raw)), "SHA256": admission.sha(raw)}
        response = {"status": 200, "raw": raw, "headers": [("Content-Type", "application/octet-stream"),
            ("Content-Disposition", "attachment; filename=" + backup["Id"] + ".age"), ("ETag", '"' + backup["SHA256"] + '"'),
            ("Accept-Ranges", "bytes"), ("Cache-Control", "no-store"), ("Content-Length", str(len(raw)))]}
        admission.validate_download(response, backup, "full")
        response["raw"] = raw[:-1]
        with self.assertRaises(admission.AdmissionError):
            admission.validate_download(response, backup, "full")
        response["raw"] = raw[:-1] + b"y"
        with self.assertRaises(admission.AdmissionError):
            admission.validate_download(response, backup, "full")

    def test_duplicate_headers_are_not_silently_collapsed(self):
        with self.assertRaises(admission.AdmissionError):
            admission.response_headers({"headers": [("Content-Length", "3"), ("content-length", "4")]})

    def test_old_rows_cannot_hide_behind_login_whitelist(self):
        snapshot = {"capturedAt": "2026-09-13T00:00:00Z", "tables": {"users": [{"id": "old"}],
                    "sessions": [{"id": "old", "last_seen_at": "before"}], "devices": [], "activity_entries": []}, "sequences": {}}
        after = copy.deepcopy(snapshot)
        after["tables"]["sessions"][0]["last_seen_at"] = "after"
        with self.assertRaisesRegex(admission.AdmissionError, "preexisting_source_rows_changed:sessions"):
            admission.reconcile_source(snapshot, after, {}, "a", "b", "c")
        after = copy.deepcopy(snapshot)
        after["tables"]["users"] = []
        with self.assertRaisesRegex(admission.AdmissionError, "unowned_source_table_changed:users"):
            admission.reconcile_source(snapshot, after, {}, "a", "b", "c")


if __name__ == "__main__":
    unittest.main()
