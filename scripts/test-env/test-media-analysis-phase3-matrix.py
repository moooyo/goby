#!/usr/bin/env python3
"""Remote-only contract tests for independently composed recovery evidence.

These fixtures establish validation behavior only. They do not inject faults,
run services, connect to a guest, or establish actual matrix acceptance.
"""

from __future__ import annotations

import copy
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch


SPEC = importlib.util.spec_from_file_location(
    "phase3_matrix", Path(__file__).with_name("media-analysis-phase3-matrix.py"))
MATRIX = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MATRIX)

REVISION = "c" * 40
OWNER = "matrix-owner"
DISPATCH_NS = 100
CLAIMS = ["owned_guest_only", "external_acknowledgements", "independent_identity_observations"]


def sha(identity):
    return hashlib.sha256(identity.encode("ascii")).hexdigest()


def reference(identity, root="/external"):
    return {"path": root + "/" + identity + ".json", "sha256": sha(identity)}


def guest_identity():
    return {"vmid": 106, "name": "goby-phase3-matrix", "machine_id": "a" * 32,
            "smbios_uuid": "11111111-2222-3333-4444-555555555555",
            "disks": {"scsi0": {"volume": "local-lvm:vm-106-disk-0", "uuid": "owned-disk",
                                "size_bytes": 20 << 30}}}


def scenario(fault="blocked_read", late_mount=False, scenario_id="case-10000-blocked_read"):
    return {"scenario_id": scenario_id, "fault": fault,
            "volume_id": "media" if fault in {"blocked_read", "blocked_metadata", "mount_loss",
                "changed_root_mount", "changed_nested_mount", "permission_failure", "enospc"} or late_mount else None,
            "replacement_volume_id": "replacement" if fault in {"changed_root_mount", "changed_nested_mount"} else None,
            "relative_path": "nested" if fault == "changed_nested_mount" else ".",
            "late_mount": late_mount, "restart_goby_after": False,
            "require_interrupted_jobs": fault in {"process_crash", "postgres_restart", "guest_reboot", "guest_reset"}}


def case_record(tier, fault, late_mount=False):
    identity = "case-%d-%s%s" % (tier, fault, "-late" if late_mount else "")
    return {"scenario_id": identity, "run_id": "run-%d" % tier, "fault": fault,
            "late_mount": late_mount, "manifest": reference(identity + "-manifest"),
            "journal": {"directory": "/external/" + identity + "-journal",
                        "terminal": reference(identity + "-journal/0010-run-result")},
            "oracle_state": reference(identity + "-oracle"),
            "inputs": [{"original": reference(identity + "-binding", "/owned"),
                        "retained": reference(identity + "-binding")}],
            "reset_evidence": {"intent": reference(identity + "-reset-intent"),
                               "receipt": reference(identity + "-reset-receipt")} if fault == "guest_reset" else None}


def matrix_manifest():
    return {"schema_version": 1, "matrix_id": "full-matrix", "source_revision": REVISION,
            "owner_id": OWNER, "guest": guest_identity(),
            "modules": {name: reference("module-" + name) for name in
                        ("controller", "state", "oracle", "evidence", "workload")},
            "tiers": [{"tier": tier, "profile_id": "profile-%d" % tier,
                       "cases": [case_record(tier, fault) for fault in sorted(MATRIX.FAULTS)] +
                                [case_record(tier, "guest_reboot", True)]}
                      for tier in (10000, 100000)],
            "retained_failures": [reference("retained-failed-execution")]}


def recovery_manifest(fault="blocked_read", late_mount=False):
    return {"schema_version": 1, "run_id": "run-10000", "source_revision": REVISION,
            "owner_id": OWNER, "tier": 10000, "profile_id": "profile-10000",
            "guest": guest_identity(), "scenarios": [scenario(fault, late_mount)]}


def report(manifest, manifest_sha256):
    selected = manifest["scenarios"][0]
    return {"schema_version": 1, "run_id": manifest["run_id"], "manifest_sha256": manifest_sha256,
            "source_revision": manifest["source_revision"], "tier": manifest["tier"], "status": "partial",
            "accepted_faults": [selected["fault"]], "pending_faults": sorted(MATRIX.FAULTS - {selected["fault"]}),
            "complete_fault_matrix": False, "late_mount_covered": selected["late_mount"],
            "scenario_count": 1, "physical_power_loss_tested": False,
            "matrix_composition_required": True, "claims": list(CLAIMS)}


def release_fixture(fault="blocked_read", late_mount=False):
    manifest = recovery_manifest(fault, late_mount)
    binding = {"schema_version": 1, "run_id": manifest["run_id"], "owner_id": OWNER,
               "guest": guest_identity()}
    selected = manifest["scenarios"][0]
    binding_sha256 = sha("guest-binding")
    release = {"schema_version": 1, "run_id": manifest["run_id"], "owner_id": OWNER, "vmid": 106,
               "smbios_uuid": manifest["guest"]["smbios_uuid"], "source_revision": REVISION,
               "binding_sha256": binding_sha256, "operations": ["inject", "recover", "close"],
               "scenarios": {selected["scenario_id"]: hashlib.sha256(MATRIX.canonical(selected)).hexdigest()},
               "expires_unix_ns": DISPATCH_NS + 1}
    if late_mount:
        release["operations"].append("arm_late_mount")
    return binding, release, manifest, binding_sha256


class MatrixManifestTests(unittest.TestCase):
    def test_complete_matrix_has_both_tiers_and_independent_late_mounts(self):
        value = matrix_manifest()
        self.assertEqual(len(MATRIX.FAULTS), 13)
        self.assertEqual(MATRIX.load_manifest(value), value)
        self.assertEqual(sum(len(tier["cases"]) for tier in value["tiers"]), 28)

    def test_distinct_cases_may_share_the_existing_run_and_profile(self):
        value = matrix_manifest()
        for tier in value["tiers"]:
            self.assertEqual(len({case["run_id"] for case in tier["cases"]}), 1)
        MATRIX.load_manifest(value)

    def test_single_tier_and_duplicate_tier_do_not_complete_matrix(self):
        for replacement in ([10000], [100000], [10000, 10000], [10000, 100000, 100000]):
            with self.subTest(tiers=replacement):
                value = matrix_manifest()
                originals = {tier["tier"]: tier for tier in value["tiers"]}
                value["tiers"] = [copy.deepcopy(originals[tier]) for tier in replacement]
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.load_manifest(value)

    def test_every_ordinary_fault_and_separate_late_reboot_are_required(self):
        for tier_index in (0, 1):
            for missing_index in range(14):
                with self.subTest(tier=tier_index, missing=missing_index):
                    value = matrix_manifest()
                    del value["tiers"][tier_index]["cases"][missing_index]
                    with self.assertRaises(MATRIX.Invalid):
                        MATRIX.load_manifest(value)

    def test_late_reboot_cannot_replace_ordinary_reboot_or_use_reset(self):
        for change in ("replace-ordinary", "late-reset"):
            with self.subTest(change=change):
                value = matrix_manifest()
                cases = value["tiers"][0]["cases"]
                if change == "replace-ordinary":
                    next(case for case in cases if case["fault"] == "guest_reboot" and not case["late_mount"])["late_mount"] = True
                else:
                    cases[-1]["fault"] = "guest_reset"
                    cases[-1]["reset_evidence"] = {"intent": reference("late-reset-intent"),
                                                   "receipt": reference("late-reset-receipt")}
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.load_manifest(value)

    def test_duplicate_execution_and_reused_identity_artifacts_are_rejected(self):
        for reused in ("execution", "scenario_id", "manifest", "terminal", "oracle_state"):
            with self.subTest(reused=reused):
                value = matrix_manifest()
                first, second = value["tiers"][0]["cases"][:2]
                if reused == "execution":
                    for name in ("run_id", "scenario_id", "journal"):
                        second[name] = copy.deepcopy(first[name])
                elif reused == "terminal":
                    second["journal"]["terminal"]["sha256"] = first["journal"]["terminal"]["sha256"]
                elif reused in ("manifest", "oracle_state"):
                    second[reused]["sha256"] = first[reused]["sha256"]
                else:
                    second[reused] = first[reused]
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.load_manifest(value)

    def test_scenario_and_evidence_uniqueness_crosses_tier_boundaries(self):
        for name in ("scenario_id", "manifest", "oracle_state"):
            with self.subTest(name=name):
                value = matrix_manifest()
                first = value["tiers"][0]["cases"][0]
                second = value["tiers"][1]["cases"][0]
                second[name] = copy.deepcopy(first[name])
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.load_manifest(value)

    def test_case_requires_retained_inputs_and_only_reset_has_reset_evidence(self):
        for change in ("missing-input", "missing-reset", "unexpected-reset"):
            with self.subTest(change=change):
                value = matrix_manifest()
                cases = value["tiers"][0]["cases"]
                if change == "missing-input":
                    cases[0]["inputs"] = []
                elif change == "missing-reset":
                    next(case for case in cases if case["fault"] == "guest_reset")["reset_evidence"] = None
                else:
                    cases[0]["reset_evidence"] = {"intent": reference("unexpected-intent"),
                                                 "receipt": reference("unexpected-receipt")}
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.load_manifest(value)

    def test_wrong_vm_disk_scope_and_unpinned_source_are_rejected(self):
        for change in ("vmid", "name", "disk", "source", "module"):
            with self.subTest(change=change):
                value = matrix_manifest()
                if change == "vmid":
                    value["guest"]["vmid"] = 101
                elif change == "name":
                    value["guest"]["name"] = "production"
                elif change == "disk":
                    value["guest"]["disks"]["scsi0"]["volume"] = "local-lvm:vm-101-disk-0"
                elif change == "source":
                    value["source_revision"] = "main"
                else:
                    value["modules"]["controller"]["sha256"] = "latest"
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.load_manifest(value)

    def test_strict_schema_rejects_extra_and_missing_fields(self):
        for location in ("manifest", "tier", "case", "reference"):
            for mutation in ("extra", "missing"):
                with self.subTest(location=location, mutation=mutation):
                    value = matrix_manifest()
                    target = {"manifest": value, "tier": value["tiers"][0],
                              "case": value["tiers"][0]["cases"][0],
                              "reference": value["modules"]["controller"]}[location]
                    if mutation == "extra":
                        target["trusted"] = True
                    else:
                        del target[next(iter(target))]
                    with self.assertRaises(MATRIX.Invalid):
                        MATRIX.load_manifest(value)


class SingleCaseReportTests(unittest.TestCase):
    def test_genuine_partial_controller_report_is_accepted_only_as_one_case(self):
        for fault, late_mount in [(fault, False) for fault in MATRIX.FAULTS] + [("guest_reboot", True)]:
            with self.subTest(fault=fault, late_mount=late_mount):
                manifest = recovery_manifest(fault, late_mount)
                expected_sha = sha("case-manifest")
                MATRIX.validate_report(report(manifest, expected_sha), manifest, expected_sha)

    def test_admission_or_promoted_single_case_never_counts_as_completed_evidence(self):
        changes = [{"status": "admitted"}, {"status": "passed"}, {"complete_fault_matrix": True},
                   {"matrix_composition_required": False}, {"scenario_count": 0}, {"scenario_count": 14},
                   {"physical_power_loss_tested": True}, {"accepted_faults": sorted(MATRIX.FAULTS)},
                   {"pending_faults": []}, {"claims": []}]
        manifest = recovery_manifest()
        expected_sha = sha("case-manifest")
        for change in changes:
            with self.subTest(change=change), self.assertRaises(MATRIX.Invalid):
                MATRIX.validate_report({**report(manifest, expected_sha), **change}, manifest, expected_sha)

    def test_report_is_bound_to_manifest_source_tier_run_and_late_mount(self):
        changes = [{"run_id": "another-run"}, {"source_revision": "d" * 40}, {"tier": 100000},
                   {"manifest_sha256": sha("another-manifest")}, {"late_mount_covered": True},
                   {"accepted_faults": ["permission_failure"]}]
        manifest = recovery_manifest()
        expected_sha = sha("case-manifest")
        for change in changes:
            with self.subTest(change=change), self.assertRaises(MATRIX.Invalid):
                MATRIX.validate_report({**report(manifest, expected_sha), **change}, manifest, expected_sha)

    def test_json_scalar_types_cannot_be_substituted_by_python_equal_values(self):
        changes = [{"scenario_count": True}, {"scenario_count": 1.0}, {"schema_version": True},
                   {"tier": 10000.0}, {"complete_fault_matrix": 0}, {"late_mount_covered": 0},
                   {"physical_power_loss_tested": 0}, {"matrix_composition_required": 1}, {"status": True}]
        manifest = recovery_manifest()
        expected_sha = sha("case-manifest")
        for change in changes:
            with self.subTest(change=change), self.assertRaises(MATRIX.Invalid):
                MATRIX.validate_report({**report(manifest, expected_sha), **change}, manifest, expected_sha)


class GuestReleaseTests(unittest.TestCase):
    def test_release_is_evaluated_at_recorded_dispatch_not_composition_time(self):
        binding, release, manifest, binding_sha256 = release_fixture()
        MATRIX.validate_guest_release(binding, release, manifest, binding_sha256, DISPATCH_NS)

    def test_release_expired_at_dispatch_is_rejected_including_equal_boundary(self):
        for expires in (0, DISPATCH_NS - 1, DISPATCH_NS):
            with self.subTest(expires=expires):
                binding, release, manifest, binding_sha256 = release_fixture()
                release["expires_unix_ns"] = expires
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.validate_guest_release(binding, release, manifest, binding_sha256, DISPATCH_NS)

    def test_release_is_bound_to_run_owner_vm_source_binding_and_scenario(self):
        changes = [{"run_id": "another-run"}, {"owner_id": "another-owner"}, {"vmid": 101},
                   {"smbios_uuid": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}, {"source_revision": "d" * 40},
                   {"binding_sha256": sha("another-binding")}, {"scenarios": {}},
                   {"scenarios": {"case-10000-blocked_read": sha("changed-scenario")}}]
        for change in changes:
            with self.subTest(change=change):
                binding, release, manifest, binding_sha256 = release_fixture()
                release.update(change)
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.validate_guest_release(binding, release, manifest, binding_sha256, DISPATCH_NS)

    def test_binding_identity_cannot_be_replayed_for_another_guest_or_disk(self):
        for field in ("run_id", "owner_id", "vmid", "name", "machine_id", "smbios_uuid", "disks"):
            with self.subTest(field=field):
                binding, release, manifest, binding_sha256 = release_fixture()
                if field in ("run_id", "owner_id"):
                    binding[field] = "another-owner-or-run"
                elif field == "vmid":
                    binding["guest"][field] = 101
                elif field == "disks":
                    binding["guest"][field]["scsi0"]["size_bytes"] += 512
                else:
                    binding["guest"][field] = "another-guest"
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.validate_guest_release(binding, release, manifest, binding_sha256, DISPATCH_NS)

    def test_all_required_mutations_must_have_been_released(self):
        for operation in ("inject", "recover", "close", "arm_late_mount"):
            with self.subTest(operation=operation):
                binding, release, manifest, binding_sha256 = release_fixture("guest_reboot", True)
                MATRIX.validate_guest_release(binding, release, manifest, binding_sha256, DISPATCH_NS)
                release["operations"].remove(operation)
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.validate_guest_release(binding, release, manifest, binding_sha256, DISPATCH_NS)


class RetainedJournalTests(unittest.TestCase):
    def journal_fixture(self, changes=None):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name).resolve()
        records = []
        previous = "0" * 64
        for index, kind in enumerate(("run-admitted", "scenario-start", "run-result")):
            event = {"schema_version": 1, "sequence": index, "previous_sha256": previous, "kind": kind,
                     "controller_unix_ns": 100 + index, "controller_monotonic_ns": 10 + index,
                     "data": {"status": "partial"} if kind == "run-result" else {"scenario_id": "case"}}
            if changes and index in changes:
                event.update(changes[index])
            raw = MATRIX.canonical(event) + b"\n"
            path = root / ("%04d-%s.json" % (index, event["kind"]))
            path.write_bytes(raw)
            path.chmod(0o600)
            previous = hashlib.sha256(raw).hexdigest()
            records.append({"event": event, "reference": {"path": str(path), "sha256": previous}})
        return {"directory": str(root), "terminal": records[-1]["reference"]}, records

    def test_frozen_terminal_links_every_retained_event(self):
        journal, records = self.journal_fixture()
        self.assertEqual(MATRIX.read_journal(journal), records)

    def test_altered_early_event_cannot_be_hidden_by_unchanged_terminal(self):
        journal, records = self.journal_fixture()
        records[0]["event"]["data"]["scenario_id"] = "another-case"
        Path(records[0]["reference"]["path"]).write_bytes(MATRIX.canonical(records[0]["event"]) + b"\n")
        with self.assertRaises(MATRIX.Invalid):
            MATRIX.read_journal(journal)

    def test_rehashed_terminal_cannot_replace_the_frozen_terminal_pin(self):
        journal, records = self.journal_fixture()
        records[-1]["event"]["data"]["status"] = "passed"
        Path(records[-1]["reference"]["path"]).write_bytes(MATRIX.canonical(records[-1]["event"]) + b"\n")
        with self.assertRaises(MATRIX.Invalid):
            MATRIX.read_journal(journal)

    def test_missing_event_cannot_be_accepted_as_a_shorter_execution(self):
        journal, records = self.journal_fixture()
        Path(records[1]["reference"]["path"]).unlink()
        with self.assertRaises(MATRIX.Invalid):
            MATRIX.read_journal(journal)

    def test_admission_terminal_and_clock_regression_do_not_prove_execution(self):
        for change in ({2: {"kind": "admission"}}, {1: {"controller_monotonic_ns": 1}}):
            with self.subTest(change=change):
                journal, _records = self.journal_fixture(change)
                with self.assertRaises(MATRIX.Invalid):
                    MATRIX.read_journal(journal)

    def test_extra_unaccounted_file_prevents_journal_composition(self):
        journal, _records = self.journal_fixture()
        (Path(journal["directory"]) / "unaccounted.json").write_text("{}", encoding="ascii")
        with self.assertRaises(MATRIX.Invalid):
            MATRIX.read_journal(journal)


class ReplayReceiptTests(unittest.TestCase):
    def receipt_fixture(self):
        manifest = recovery_manifest()
        manifest.update({"volumes": {}, "controller": {"artifacts_root": "/external"},
                         "adapters": {"executor": {**reference("executor"), "context": reference("executor-context")}}})
        selected = manifest["scenarios"][0]
        request = {"schema_version": 1, "request_id": manifest["run_id"] + ":1", "operation": "inject",
                   **{key: manifest[key] for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier")},
                   "guest": manifest["guest"], "volumes": {}, "scenario": selected, "payload": {},
                   "artifacts_root": "/external", "context_sha256": manifest["adapters"]["executor"]["context"]["sha256"]}
        intent = {"event": {"kind": "mutation-intent", "data": {"role": "executor", "request": request,
                  "adapter_sha256": manifest["adapters"]["executor"]["sha256"]}}, "reference": reference("intent")}
        owner = {"event": {"kind": "adapter-process-owner", "data": {"request_id": request["request_id"],
                 "pid": 100, "start_ticks": 1000, "cgroup": "/controller", "preserve_on_observation_timeout": False}},
                 "reference": reference("owner")}
        receipt = {"schema_version": 1, "request_id": request["request_id"], "operation": "inject",
                   "owner_id": OWNER, "vmid": 106, "status": "ok", "data": {"dispatches": 1}}
        result = {"event": {"kind": "rpc-receipt", "data": {"intent": intent["reference"], "receipt": receipt}},
                  "reference": reference("receipt")}
        return manifest, selected, [intent, owner, result]

    def invoke(self, manifest, selected, records):
        journal = MATRIX.ReplayJournal(records)
        result = MATRIX.ReplayRPC(manifest, journal).call("executor", "inject", selected, mutation=True)
        self.assertEqual(journal.position, len(records))
        return result

    def test_receipt_requires_matching_intent_and_process_owner(self):
        manifest, selected, records = self.receipt_fixture()
        self.assertEqual(self.invoke(manifest, selected, records)["status"], "ok")
        for missing in (0, 1, 2):
            with self.subTest(missing=missing):
                manifest, selected, records = self.receipt_fixture()
                del records[missing]
                with self.assertRaises(MATRIX.Invalid):
                    self.invoke(manifest, selected, records)

    def test_reused_receipt_cannot_satisfy_another_request_or_guest(self):
        changes = [{"request_id": "another-run:1"}, {"owner_id": "another-owner"},
                   {"vmid": 101}, {"operation": "recover"}]
        for change in changes:
            with self.subTest(change=change):
                manifest, selected, records = self.receipt_fixture()
                records[-1]["event"]["data"]["receipt"].update(change)
                with self.assertRaises(MATRIX.Invalid):
                    self.invoke(manifest, selected, records)

    def test_receipt_cannot_reference_another_intent(self):
        manifest, selected, records = self.receipt_fixture()
        records[-1]["event"]["data"]["intent"] = reference("another-intent")
        with self.assertRaises(MATRIX.Invalid):
            self.invoke(manifest, selected, records)

    def test_pending_failed_or_repeated_mutation_never_establishes_dispatch(self):
        changes = [{"status": "pending"}, {"status": "failed"}, {"data": {"dispatches": 0}},
                   {"data": {"dispatches": 2}}, {"data": {}}]
        for change in changes:
            with self.subTest(change=change):
                manifest, selected, records = self.receipt_fixture()
                records[-1]["event"]["data"]["receipt"].update(change)
                with self.assertRaises(MATRIX.Invalid):
                    self.invoke(manifest, selected, records)


class RawEvidenceTests(unittest.TestCase):
    def raw_fixture(self, external=False):
        manifest = recovery_manifest("process_crash")
        manifest["controller"] = {"artifacts_root": "/external"}
        manifest["state_validator"] = {"binding_sha256": sha("state-binding"), "scope": {"user_ids": ["viewer"]}}
        calls = [{"request": {"request_id": manifest["run_id"] + ":" + str(index), "operation": operation},
                  "receipt": {"status": "ok"}}
                 for index, operation in enumerate(("inject", "closure_observation"), 1)]
        documents = [{"kind": "guest-observe", "request_id": call["request"]["request_id"],
                      "data": {"status": "ok", "owner_id": OWNER, "data": {"observed": True}}}
                     for call in calls]
        if external:
            documents.append({"kind": "external-artifact", "request_id": calls[0]["request"]["request_id"],
                "data": {"guest": {"path": "/guest/snapshot.sqlite", "sha256": sha("sqlite-bytes"), "bytes": 17},
                         "external": {"path": "/external/snapshot.sqlite", "sha256": sha("sqlite-bytes"),
                                      "bytes": 17, "fsynced": True}}})
        saved, raw = self.freeze_documents(documents, manifest)
        modules = {"controller": Mock(), "state": Mock()}
        return saved, calls, manifest, modules, raw

    def freeze_documents(self, documents, manifest):
        references, raw = [], {}
        for index, document in enumerate(documents):
            path = "/external/record-%d.json" % index
            payload = MATRIX.canonical(document) + b"\n"
            raw[path] = payload
            references.append({"path": path, "bytes": len(payload), "sha256": hashlib.sha256(payload).hexdigest(),
                               "fsynced": True})
        return {"version": 1, "scenario_id": manifest["scenarios"][0]["scenario_id"], "records": references}, raw

    def construct(self, fixture):
        saved, calls, manifest, modules, raw = fixture
        with patch.object(MATRIX, "read_file", side_effect=lambda ref: raw[ref["path"]]):
            return MATRIX.RawEvidence(saved, calls, manifest, modules)

    def rewrite_record(self, fixture, index, change):
        saved, _calls, _manifest, _modules, raw = fixture
        ref = saved["records"][index]
        document = MATRIX.decode(raw[ref["path"]])
        change(document)
        payload = MATRIX.canonical(document) + b"\n"
        raw[ref["path"]] = payload
        ref.update({"bytes": len(payload), "sha256": hashlib.sha256(payload).hexdigest()})

    def test_all_successful_guest_rpcs_require_retained_raw_records(self):
        fixture = self.raw_fixture()
        raw = self.construct(fixture)
        self.assertEqual(raw.native("inject", "guest-observe"), [{"observed": True}])
        for missing in ("all", "closure"):
            with self.subTest(missing=missing):
                fixture = self.raw_fixture()
                fixture[0]["records"] = [] if missing == "all" else fixture[0]["records"][:1]
                with self.assertRaises(MATRIX.Invalid):
                    self.construct(fixture)

    def test_reused_unbound_unflushed_and_truncated_raw_records_are_rejected(self):
        for change in ("reused", "unbound", "unflushed", "truncated", "scenario"):
            with self.subTest(change=change):
                fixture = self.raw_fixture()
                if change == "reused":
                    fixture[0]["records"].append(copy.deepcopy(fixture[0]["records"][0]))
                elif change == "unbound":
                    self.rewrite_record(fixture, 0, lambda value: value.update({"request_id": "another-run:1"}))
                elif change == "unflushed":
                    fixture[0]["records"][0]["fsynced"] = False
                elif change == "truncated":
                    fixture[0]["records"][0]["bytes"] += 1
                else:
                    fixture[0]["scenario_id"] = "another-scenario"
                with self.assertRaises(MATRIX.Invalid):
                    self.construct(fixture)

    def test_native_projection_requires_a_real_matching_collector_record(self):
        raw = self.construct(self.raw_fixture())
        with self.assertRaises(MATRIX.Invalid):
            raw.native("inject", "probe-affected_probe")
        with self.assertRaises(MATRIX.Invalid):
            raw.bind("inject", "guest-observe", {"observed": False})
        fixture = self.raw_fixture()
        self.rewrite_record(fixture, 0, lambda value: value["data"].update({"owner_id": "another-owner"}))
        with self.assertRaises(MATRIX.Invalid):
            self.construct(fixture).native("inject", "guest-observe")

    def test_external_transfer_requires_equal_native_bytes_and_external_durability(self):
        fixture = self.raw_fixture(external=True)
        raw = self.construct(fixture)
        transferred = raw.exports[("/guest/snapshot.sqlite", sha("sqlite-bytes"))]
        fixture[3]["controller"].durable_artifact.assert_called_once_with(transferred, "/external")
        for change in ({"sha256": sha("other-bytes")}, {"bytes": 18}, {"fsynced": False}):
            with self.subTest(change=change):
                fixture = self.raw_fixture(external=True)
                self.rewrite_record(fixture, 2, lambda value: value["data"]["external"].update(change))
                with self.assertRaises(MATRIX.Invalid):
                    self.construct(fixture)

    def test_failed_external_durability_cannot_be_promoted_to_retained_snapshot(self):
        fixture = self.raw_fixture(external=True)
        fixture[3]["controller"].durable_artifact.side_effect = OSError("durability unavailable")
        with self.assertRaisesRegex(OSError, "durability unavailable"):
            self.construct(fixture)

    def snapshot_reference(self):
        return {"guest_reference": {"path": "/guest/snapshot.sqlite", "sha256": sha("sqlite-bytes"), "bytes": 17},
                "reference": {"snapshot_id": "snapshot", "path": "/external/snapshot.sqlite",
                              "sha256": sha("sqlite-bytes"), "bytes": 17}}

    def test_snapshot_without_retained_external_sqlite_is_rejected_before_inspection(self):
        fixture = self.raw_fixture()
        raw = self.construct(fixture)
        with self.assertRaises(MATRIX.Invalid):
            raw.snapshot(self.snapshot_reference())
        fixture[3]["state"].inspect_external.assert_not_called()

    def test_sqlite_inspection_is_required_and_its_failure_is_not_swallowed(self):
        fixture = self.raw_fixture(external=True)
        raw = self.construct(fixture)
        inspector = fixture[3]["state"].inspect_external
        inspector.side_effect = ValueError("sqlite integrity failed")
        snapshot = self.snapshot_reference()
        with self.assertRaisesRegex(ValueError, "sqlite integrity failed"):
            raw.snapshot(snapshot)
        inspector.assert_called_once_with(snapshot["reference"], sha("state-binding"), {"user_ids": ["viewer"]})

    def test_snapshot_retains_inspection_and_cannot_adopt_another_run(self):
        fixture = self.raw_fixture(external=True)
        raw = self.construct(fixture)
        inspector = fixture[3]["state"].inspect_external
        inspector.return_value = {"summary": {"run_id": fixture[2]["run_id"]}, "rows_checked": 23}
        self.assertEqual(raw.snapshot(self.snapshot_reference())["inspection"], inspector.return_value)
        inspector.return_value = {"summary": {"run_id": "another-run"}}
        with self.assertRaises(MATRIX.Invalid):
            raw.snapshot(self.snapshot_reference())

    def test_closure_snapshot_requires_its_own_external_transfer(self):
        fixture = self.raw_fixture()
        native = {"path": "/guest/closure.sqlite", "sha256": sha("closure-sqlite"), "bytes": 17}
        self.rewrite_record(fixture, 1, lambda value: value.update({"kind": "state-snapshot",
            "data": {"ok": True, "result": {"private_artifact": native}}}))
        raw = self.construct(fixture)
        with self.assertRaises(MATRIX.Invalid):
            raw.snapshot_for("closure_observation")
        fixture[3]["state"].inspect_external.assert_not_called()

    def test_closure_inputs_requires_exactly_one_retained_raw_record(self):
        raw = self.construct(self.raw_fixture())
        value = {"resources": {"observed": True}, "workload": reference("closure-workload")}
        with patch.object(raw, "records", return_value=[{"data": value}]) as records:
            self.assertEqual(raw.closure_inputs(), value)
        records.assert_called_once_with("closure_observation", "scenario-closure-inputs")
        for population in ([], [{"data": value}, {"data": copy.deepcopy(value)}]):
            with self.subTest(population=len(population)):
                with patch.object(raw, "records", return_value=population) as records:
                    with self.assertRaisesRegex(MATRIX.Invalid, "raw_closure_missing"):
                        raw.closure_inputs()
                records.assert_called_once_with("closure_observation", "scenario-closure-inputs")


class StrictDecodeTests(unittest.TestCase):
    def test_duplicate_keys_and_nonfinite_numbers_are_not_evidence(self):
        for raw in (b'{"tier":10000,"tier":100000}', b'{"duration":NaN}', b'{"duration":Infinity}'):
            with self.subTest(raw=raw), self.assertRaises(MATRIX.Invalid):
                MATRIX.decode(raw)


if __name__ == "__main__":
    unittest.main()
