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
