#!/usr/bin/env python3
"""Compose the complete Phase 3 fault matrix from retained external evidence.

This offline verifier never dispatches an RPC or changes a case result. Run it
only on the authorized external Linux controller, after freezing all inputs.
"""

from __future__ import annotations

import argparse
import base64
import decimal
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import sys


FAULTS = frozenset({"blocked_read", "blocked_metadata", "mount_loss",
                   "changed_root_mount", "changed_nested_mount", "permission_failure",
                   "enospc", "postgres_disconnect", "postgres_lock_wait",
                   "process_crash", "postgres_restart", "guest_reboot", "guest_reset"})
MODULES = {"controller": "media-analysis-phase3-recovery.py",
           "state": "media-analysis-phase3-state.py",
           "oracle": "media-analysis-phase3-oracle.py",
           "evidence": "media-analysis-phase3-oracle-evidence.py",
           "workload": "media-analysis-phase3-workload.py"}
SCOPE = ("run_id", "owner_id", "source_revision", "profile_id", "tier")
MAX_JSON = 16 << 20
SHA = re.compile(r"[0-9a-f]{64}\Z")
REVISION = re.compile(r"[0-9a-f]{40}\Z")
ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}\Z")
UUID = re.compile(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z")


class Invalid(RuntimeError):
    """A bounded failure code that contains no private evidence."""


def need(value, code):
    if not value:
        raise Invalid(code)


def fields(value, names):
    need(isinstance(value, dict) and set(value) == set(names), "unexpected_fields")


def canonical(value):
    if isinstance(value, dict):
        return b"{" + b",".join(json.dumps(key, ensure_ascii=True).encode("ascii") + b":" + canonical(value[key])
                                  for key in sorted(value)) + b"}"
    if isinstance(value, (list, tuple)):
        return b"[" + b",".join(canonical(item) for item in value) + b"]"
    if isinstance(value, decimal.Decimal):
        need(value.is_finite() and -1000 <= value.adjusted() <= 1000 and len(value.as_tuple().digits) <= 10000,
             "nonfinite_or_unbounded_decimal")
        return format(value, "f").encode("ascii")
    return json.dumps(value, sort_keys=True, separators=(",", ":"),
                      ensure_ascii=True, allow_nan=False).encode("ascii")


def decode(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            need(key not in result, "duplicate_json_key")
            result[key] = value
        return result
    need(len(raw) <= MAX_JSON, "json_size_limit")
    try:
        return json.loads(raw, object_pairs_hook=pairs, parse_float=decimal.Decimal,
                          parse_constant=lambda _: (_ for _ in ()).throw(Invalid("nonfinite_json")))
    except (ValueError, UnicodeError, RecursionError) as error:
        raise Invalid("invalid_json") from error


def identifier(value):
    need(isinstance(value, str) and ID.fullmatch(value), "invalid_identifier")


def digest(value):
    need(isinstance(value, str) and SHA.fullmatch(value), "invalid_digest")


def absolute(value):
    need(isinstance(value, str) and 0 < len(value) <= 4096, "invalid_path")
    path = PurePosixPath(value)
    need(path.is_absolute() and str(path) == value and ".." not in path.parts and
         str(path) != "/" and not any(ord(char) < 32 for char in value), "noncanonical_path")
    return path


def reference(value):
    fields(value, ("path", "sha256"))
    absolute(value["path"])
    digest(value["sha256"])


def load_manifest(value):
    fields(value, ("schema_version", "matrix_id", "source_revision", "owner_id", "guest",
                   "modules", "tiers", "retained_failures"))
    need(value["schema_version"] == 1, "unsupported_schema")
    identifier(value["matrix_id"])
    identifier(value["owner_id"])
    need(isinstance(value["source_revision"], str) and REVISION.fullmatch(value["source_revision"]),
         "invalid_source_revision")
    guest = value["guest"]
    fields(guest, ("vmid", "name", "machine_id", "smbios_uuid", "disks"))
    need(type(guest["vmid"]) is int and guest["vmid"] == 106 and
         isinstance(guest["name"], str) and guest["name"].startswith("goby-phase3-") and
         re.fullmatch(r"[0-9a-f]{32}", guest["machine_id"]) and UUID.fullmatch(guest["smbios_uuid"]),
         "guest_scope_mismatch")
    need(isinstance(guest["disks"], dict) and 1 <= len(guest["disks"]) <= 8, "disk_inventory_missing")
    for label, disk in guest["disks"].items():
        identifier(label)
        fields(disk, ("volume", "uuid", "size_bytes"))
        need(re.fullmatch(r"local-lvm:vm-106-(?:disk-[0-9]+|cloudinit)", disk["volume"]) and
             isinstance(disk["uuid"], str) and bool(disk["uuid"]) and
             type(disk["size_bytes"]) is int and disk["size_bytes"] > 0, "disk_scope_mismatch")
    fields(value["modules"], MODULES)
    for item in value["modules"].values():
        reference(item)
    tiers = value["tiers"]
    need(isinstance(tiers, list) and len(tiers) == 2 and
         {row.get("tier") for row in tiers} == {10000, 100000}, "both_catalog_tiers_required")
    seen = {key: set() for key in ("scenario", "execution", "manifest", "journal", "oracle")}
    for tier in tiers:
        fields(tier, ("tier", "profile_id", "cases"))
        need(type(tier["tier"]) is int, "invalid_tier")
        identifier(tier["profile_id"])
        cases = tier["cases"]
        need(isinstance(cases, list) and len(cases) == 14, "fourteen_cases_per_tier_required")
        coverage = []
        for case in cases:
            fields(case, ("scenario_id", "run_id", "fault", "late_mount", "manifest", "journal",
                          "oracle_state", "inputs", "reset_evidence"))
            identifier(case["scenario_id"])
            identifier(case["run_id"])
            need(case["fault"] in FAULTS and type(case["late_mount"]) is bool, "invalid_case_kind")
            coverage.append((case["fault"], case["late_mount"]))
            reference(case["manifest"])
            reference(case["oracle_state"])
            fields(case["journal"], ("directory", "terminal"))
            absolute(case["journal"]["directory"])
            reference(case["journal"]["terminal"])
            need(PurePosixPath(case["journal"]["terminal"]["path"]).parent ==
                 PurePosixPath(case["journal"]["directory"]), "terminal_outside_journal")
            keys = {"scenario": case["scenario_id"],
                    "execution": (case["run_id"], case["scenario_id"], case["journal"]["directory"]),
                    "manifest": case["manifest"]["sha256"], "journal": case["journal"]["terminal"]["sha256"],
                    "oracle": case["oracle_state"]["sha256"]}
            for kind, key in keys.items():
                need(key not in seen[kind], "reused_" + kind)
                seen[kind].add(key)
            need(isinstance(case["inputs"], list) and 1 <= len(case["inputs"]) <= 32, "retained_inputs_required")
            original_paths = set()
            for item in case["inputs"]:
                fields(item, ("original", "retained"))
                reference(item["original"])
                reference(item["retained"])
                need(item["original"]["sha256"] == item["retained"]["sha256"], "retained_input_bytes_changed")
                key = (item["original"]["path"], item["original"]["sha256"])
                need(key not in original_paths, "duplicate_retained_input")
                original_paths.add(key)
            reset = case["reset_evidence"]
            if case["fault"] == "guest_reset":
                fields(reset, ("intent", "receipt"))
                reference(reset["intent"])
                reference(reset["receipt"])
            else:
                need(reset is None, "unexpected_reset_evidence")
        expected = {(fault, False) for fault in FAULTS} | {("guest_reboot", True)}
        need(len(set(coverage)) == 14 and set(coverage) == expected, "fault_matrix_coverage_incomplete")
    need(isinstance(value["retained_failures"], list) and len(value["retained_failures"]) <= 256,
         "retained_failure_budget")
    for item in value["retained_failures"]:
        reference(item)
    return value


def validate_report(report, manifest, manifest_sha256):
    scenario = manifest["scenarios"][0]
    expected = {"schema_version": 1, "run_id": manifest["run_id"], "manifest_sha256": manifest_sha256,
                "source_revision": manifest["source_revision"], "tier": manifest["tier"], "status": "partial",
                "accepted_faults": [scenario["fault"]], "pending_faults": sorted(FAULTS - {scenario["fault"]}),
                "complete_fault_matrix": False, "late_mount_covered": scenario["late_mount"], "scenario_count": 1,
                "physical_power_loss_tested": False, "matrix_composition_required": True,
                "claims": ["owned_guest_only", "external_acknowledgements", "independent_identity_observations"]}
    need(canonical(report) == canonical(expected), "single_case_report_mismatch")


def validate_guest_release(binding, release, manifest, binding_sha256, dispatch_unix_ns):
    scenario = manifest["scenarios"][0]
    fields(release, ("schema_version", "run_id", "owner_id", "vmid", "smbios_uuid", "source_revision",
                     "binding_sha256", "operations", "scenarios", "expires_unix_ns"))
    for key in ("run_id", "owner_id"):
        need(binding[key] == manifest[key] == release[key], "guest_release_scope_mismatch")
    for key in ("vmid", "name", "machine_id", "smbios_uuid", "disks"):
        need(binding["guest"][key] == manifest["guest"][key], "guest_binding_identity_mismatch")
    need(release["schema_version"] == 1 and release["vmid"] == 106 and
         release["smbios_uuid"] == manifest["guest"]["smbios_uuid"] and
         release["source_revision"] == manifest["source_revision"] and
         release["binding_sha256"] == binding_sha256, "guest_release_pin_mismatch")
    need(release["scenarios"] == {scenario["scenario_id"]: hashlib.sha256(canonical(scenario)).hexdigest()},
         "guest_release_scenario_mismatch")
    operations = release["operations"]
    required = {"recover", "close"}
    if scenario["fault"] != "guest_reset":
        required.add("inject")
    if scenario["late_mount"]:
        required.add("arm_late_mount")
    need(isinstance(operations, list) and len(operations) == len(set(operations)) and
         required <= set(operations), "guest_release_operations_missing")
    need(type(dispatch_unix_ns) is int and dispatch_unix_ns > 0 and
         type(release["expires_unix_ns"]) is int and dispatch_unix_ns < release["expires_unix_ns"],
         "guest_release_expired_at_dispatch")


def read_file(ref, maximum=MAX_JSON, private=True, pinned=True):
    if pinned:
        reference(ref)
    else:
        absolute(ref["path"])
    path = Path(ref["path"])
    need(path.resolve(strict=True) == path, "symlinked_evidence")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and before.st_nlink == 1,
             "evidence_file_owner_or_type")
        need(not private or stat.S_IMODE(before.st_mode) & 0o077 == 0, "private_evidence_permissions")
        need(before.st_size <= maximum, "evidence_size_limit")
        chunks, size = [], 0
        while chunk := os.read(fd, min(1 << 20, maximum + 1 - size)):
            chunks.append(chunk)
            size += len(chunk)
            need(size <= maximum, "evidence_size_limit")
        after = os.fstat(fd)
        identity = lambda row: (row.st_dev, row.st_ino, row.st_size, row.st_mtime_ns, row.st_ctime_ns)
        need(identity(before) == identity(after) == identity(path.lstat()), "evidence_changed_during_read")
        raw = b"".join(chunks)
        need(not pinned or hashlib.sha256(raw).hexdigest() == ref["sha256"], "evidence_digest_mismatch")
        return raw
    finally:
        os.close(fd)


def load_json(ref):
    return decode(read_file(ref))


def read_journal(spec):
    root = Path(spec["directory"])
    need(root.resolve(strict=True) == root and root.is_dir(), "journal_directory_identity")
    paths = sorted(root.iterdir())
    need(1 <= len(paths) <= 2048 and all(re.fullmatch(r"[0-9]{4}-[A-Za-z0-9_.:-]+\.json", row.name)
                                       for row in paths), "journal_file_population")
    records, previous, monotonic = [], "0" * 64, 0
    for index, path in enumerate(paths):
        # Pin the read with the chain from the independently frozen terminal.
        raw = read_file({"path": str(path)}, pinned=False)
        pin = {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}
        event = decode(raw)
        fields(event, ("schema_version", "sequence", "previous_sha256", "kind", "controller_unix_ns",
                       "controller_monotonic_ns", "data"))
        need(raw == canonical(event) + b"\n" and event["schema_version"] == 1 and
             event["sequence"] == index and event["previous_sha256"] == previous and
             path.name == f"{index:04d}-{event['kind']}.json", "journal_hash_chain_mismatch")
        need(type(event["controller_monotonic_ns"]) is int and event["controller_monotonic_ns"] >= monotonic and
             type(event["controller_unix_ns"]) is int and event["controller_unix_ns"] > 0,
             "journal_time_order")
        previous, monotonic = pin["sha256"], event["controller_monotonic_ns"]
        records.append({"event": event, "reference": pin})
    need(records[-1]["reference"] == spec["terminal"] and records[-1]["event"]["kind"] == "run-result",
         "successful_terminal_journal_required")
    return records


class ReplayJournal:
    """Consume an immutable journal; writes only compare original event bytes."""

    def __init__(self, records):
        self.records, self.position, self.calls = records, 0, []

    def take(self, kind):
        need(self.position < len(self.records), "journal_transition_missing")
        row = self.records[self.position]
        need(row["event"]["kind"] == kind, "journal_transition_mismatch")
        self.position += 1
        return row

    def write(self, kind, data):
        row = self.take(kind)
        expected, observed = dict(row["event"]["data"]), dict(data)
        if kind == "scenario-result":
            duration = expected.pop("duration_seconds")
            need(type(duration) in (float, int, decimal.Decimal) and duration >= 0, "invalid_case_duration")
            observed.pop("duration_seconds")
        need(canonical(expected) == canonical(observed), "journal_projection_mismatch")
        return row["reference"]


class ReplayRPC:
    """A finite receipt reader, with no transport or process execution path."""

    def __init__(self, manifest, journal):
        self.manifest, self.journal, self.counter = manifest, journal, 0

    def call(self, role, operation, scenario, payload=None, mutation=False, timeout=None):
        if operation == "hypervisor_identity":
            role = "hypervisor"
        if operation == "inject" and scenario["fault"] == "guest_reset":
            role, operation, payload = "hypervisor", "forced_reset", {"durable_ack": payload["durable_ack"]}
        self.counter += 1
        adapter = self.manifest["adapters"][role]
        request = {"schema_version": 1, "request_id": self.manifest["run_id"] + ":" + str(self.counter),
                   "operation": operation, **{key: self.manifest[key] for key in SCOPE},
                   "guest": self.manifest["guest"], "volumes": self.manifest["volumes"],
                   "scenario": scenario, "payload": payload or {},
                   "artifacts_root": self.manifest["controller"]["artifacts_root"],
                   "context_sha256": adapter["context"]["sha256"]}
        intent = self.journal.take("mutation-intent" if mutation else "observation-intent")
        need(intent["event"]["data"] == {"role": role, "request": request, "adapter_sha256": adapter["sha256"]},
             "rpc_intent_binding_mismatch")
        owner = self.journal.take("adapter-process-owner")["event"]["data"]
        fields(owner, ("request_id", "pid", "start_ticks", "cgroup", "preserve_on_observation_timeout"))
        need(owner["request_id"] == request["request_id"] and type(owner["pid"]) is int and owner["pid"] > 1 and
             type(owner["start_ticks"]) is int and owner["start_ticks"] > 0 and bool(owner["cgroup"]) and
             owner["preserve_on_observation_timeout"] is (operation == "forced_reset"), "adapter_owner_missing")
        receipt_row = self.journal.take("rpc-receipt")
        value = receipt_row["event"]["data"]
        fields(value, ("intent", "receipt"))
        need(value["intent"] == intent["reference"], "rpc_intent_receipt_link_mismatch")
        receipt = value["receipt"]
        fields(receipt, ("schema_version", "request_id", "operation", "owner_id", "vmid", "status", "data"))
        need(receipt["schema_version"] == 1 and receipt["vmid"] == 106 and
             all(receipt[key] == request[key] for key in ("request_id", "operation", "owner_id")) and
             receipt["status"] in {"ok", "pending"}, "rpc_receipt_scope_mismatch")
        need(not mutation or receipt["status"] == "ok" and receipt["data"].get("dispatches") == 1,
             "mutation_disposition_not_established")
        self.journal.calls.append({"request": request, "receipt": receipt, "intent": intent,
                                   "receipt_reference": receipt_row["reference"], "owner": owner})
        return receipt


def replay_case(controller, state, manifest, manifest_sha256, records):
    journal = ReplayJournal(records)
    class OfflineController(controller.Controller):
        def state_module(self):
            return state

        def observe(self, op, scenario, payload=None, timeout=None):
            for _ in range(2048):
                result = self.invoke("observer", op, scenario, payload, timeout=timeout)
                if result["status"] == "ok":
                    return result["data"]
                self.journal.write("pending-observation", {"operation": op, "scenario_id": scenario["scenario_id"],
                                                           "data": result["data"]})
            raise Invalid("observation_receipt_budget")
    runner = OfflineController(manifest, manifest_sha256, journal, ReplayRPC(manifest, journal))
    report = runner.run()
    need(journal.position == len(records), "unconsumed_journal_events")
    validate_report(report, manifest, manifest_sha256)
    return journal.calls


class RawEvidence:
    """Bind retained collector replies to their original external RPC calls."""

    def __init__(self, saved, calls, manifest, modules):
        self.saved, self.calls, self.manifest, self.modules = saved, calls, manifest, modules
        self.by_request, self.exports = {}, {}
        known = {row["request"]["request_id"] for row in calls if row["request"]["operation"] not in
                 {"hypervisor_identity", "forced_reset"}}
        need(saved["version"] == 1 and saved["scenario_id"] == manifest["scenarios"][0]["scenario_id"],
             "oracle_state_case_mismatch")
        refs = saved["records"]
        need(isinstance(refs, list) and 1 <= len(refs) <= 2048, "oracle_records_missing")
        used = set()
        for ref in refs:
            fields(ref, ("path", "bytes", "sha256", "fsynced"))
            need(ref["fsynced"] is True and ref["path"] not in used, "oracle_record_reused")
            used.add(ref["path"])
            raw = read_file({key: ref[key] for key in ("path", "sha256")})
            need(len(raw) == ref["bytes"], "oracle_record_size_mismatch")
            record = decode(raw)
            fields(record, ("kind", "request_id", "data"))
            need(raw == canonical(record) + b"\n" and record["request_id"] in known, "unbound_oracle_record")
            record["reference"] = ref
            self.by_request.setdefault(record["request_id"], []).append(record)
            if record["kind"] == "external-artifact":
                value = record["data"]
                fields(value, ("guest", "external"))
                native, external = value["guest"], value["external"]
                need(native["sha256"] == external["sha256"] and native["bytes"] == external["bytes"] and
                     external["fsynced"] is True, "external_transfer_mismatch")
                modules["controller"].durable_artifact(external, manifest["controller"]["artifacts_root"])
                previous = self.exports.get((native["path"], native["sha256"]))
                need(previous is None or previous == external, "external_transfer_reference_changed")
                self.exports[(native["path"], native["sha256"])] = external
        need(known <= set(self.by_request), "rpc_raw_records_missing")

    def call(self, operation):
        matches = [row for row in self.calls if row["request"]["operation"] == operation and
                   row["receipt"]["status"] == "ok"]
        need(len(matches) == 1, "unique_successful_rpc_required")
        return matches[0]

    def records(self, operation, kind):
        request = self.call(operation)["request"]["request_id"]
        return [row for row in self.by_request[request] if row["kind"] == kind]

    def native(self, operation, kind):
        rows = self.records(operation, kind)
        need(bool(rows), "native_collector_record_missing")
        results = []
        for row in rows:
            reply = row["data"]
            if kind.startswith(("guest-", "probe-")):
                need(reply["status"] == "ok" and reply["owner_id"] == self.manifest["owner_id"],
                     "native_collector_scope_mismatch")
                results.append(reply["data"])
            else:
                need(reply["ok"] is True, "native_collector_failed")
                results.append(reply["result"])
        return results

    def bind(self, operation, kind, value):
        need(any(canonical(value) == canonical(row) for row in self.native(operation, kind)),
             "raw_projection_not_bound")

    def snapshot(self, saved):
        ref = saved["reference"]
        native = saved["guest_reference"]
        external = self.exports.get((native["path"], native["sha256"]))
        need(external is not None and all(external[key] == ref[key] for key in ("path", "sha256", "bytes")),
             "snapshot_transfer_missing")
        config = self.manifest["state_validator"]
        actual = self.modules["state"].inspect_external(ref, config["binding_sha256"], config["scope"])
        need(actual["summary"]["run_id"] == self.manifest["run_id"], "snapshot_run_mismatch")
        return {**saved, "inspection": actual}

    def snapshot_for(self, operation):
        snapshots = self.native(operation, "state-snapshot")
        native = snapshots[-1]["private_artifact"]
        external = self.exports.get((native["path"], native["sha256"]))
        need(external is not None, "closure_sqlite_transfer_missing")
        identifier = Path(native["path"]).name.removesuffix(".sqlite")
        return self.snapshot({"guest_reference": native, "reference": {
            "snapshot_id": identifier, **{key: external[key] for key in ("path", "sha256", "bytes")}}})

    def resources(self, oracle, operation, snapshot, raw):
        self.bind(operation, "guest-observe", raw["guest"])
        self.bind(operation, "guest-observe_postgres", raw["postgres"])
        self.bind(operation, "probe-runtime_resources", raw["native"])
        computed, actual = oracle.resources(snapshot, raw["guest"], raw["native"], raw["postgres"])
        need(actual == raw, "resource_projection_changed")
        need(not snapshot["inspection"]["active_jobs"] and not computed["goby_children"] and
             computed["temporary_bytes"] == computed["temporary_inodes"] == computed["partial_publications"] == 0 and
             not computed["spool_generations"], "product_resources_not_closed")
        native = raw["native"]["resources"]
        need(all(native["ScanEvidence"][key] == 0 for key in
                 ("ActivePasses", "RetiringPasses", "CleanupFailures", "ReservedBytes", "ReservedFileDescriptors")) and
             native["StorageObservations"]["Active"] == native["OriginalStreams"]["ActiveCount"] ==
             native["OriginalStreams"]["CurrentCapacityDropped"] == 0, "native_resources_not_closed")
        need(not any(row["application_name"] == "goby-phase3-fault-controller" for row in raw["postgres"]["backends"]),
             "fault_backend_not_closed")
        return computed

    def closure_inputs(self):
        rows = self.records("closure_observation", "scenario-closure-inputs")
        need(len(rows) == 1, "raw_closure_missing")
        return rows[0]["data"]

    def audit(self, context):
        controller, state, evidence = (self.modules[key] for key in ("controller", "state", "evidence"))
        scenario, saved = self.manifest["scenarios"][0], self.saved
        fault, config = scenario["fault"], self.manifest["state_validator"]
        oracle = self.modules["oracle"].Oracle.__new__(self.modules["oracle"].Oracle)
        oracle.modules, oracle.manifest, oracle.c = self.modules, self.manifest, context
        oracle.scenario, oracle.case_id = scenario, scenario["scenario_id"]
        oracle.plan, oracle.saved = context["scenarios"][scenario["scenario_id"]], dict(saved)
        for call in self.calls:
            if call["request"]["operation"] != "guest_identity" or call["receipt"]["status"] != "ok":
                continue
            identity_rows = [row["data"]["data"] for row in self.by_request[call["request"]["request_id"]]
                             if row["kind"] == "guest-observe" and row["data"]["status"] == "ok"]
            projected = []
            for observation in identity_rows:
                value = {key: observation[key] for key in self.modules["oracle"].IDENTITY_FIELDS}
                value["volume_observations"] = {name: {key: row[key] for key in self.modules["oracle"].VOLUME_FIELDS}
                                                for name, row in observation["volume_observations"].items()}
                projected.append(value)
            need(call["receipt"]["data"] in projected, "guest_identity_raw_projection_changed")
        for operation, kind in (("inject", "guest-inject"), ("recover", "guest-recover")):
            if operation == "inject" and fault == "guest_reset":
                continue
            native = self.native(operation, kind)
            need(len(native) == 1 and self.call(operation)["receipt"]["data"] == {**native[0], "dispatches": 1},
                 "native_mutation_receipt_changed")
            if operation == "inject":
                need(native[0] == saved["injection"], "saved_injection_changed")
        native_close = self.native("close_scenario", "guest-close")
        need(len(native_close) == 1 and native_close[0] == self.call("close_scenario")["receipt"]["data"]["native_close"],
             "native_close_receipt_changed")
        baseline = self.snapshot(saved["baseline"])
        recovered = self.snapshot(saved["resume_intent"]["snapshot"])
        oracle.saved["baseline"] = baseline
        ack = self.call("acknowledge")["receipt"]["data"]
        recovery = self.call("recovery_observation")["receipt"]["data"]
        need(saved["ack"] == ack and saved["recovery"] == recovery and
             set(oracle.plan["ack_mutations"].values()) == {row["request_id"] for row in ack["acknowledgements"]},
             "oracle_saved_projection_mismatch")
        checkpoint_refs = [value for value in self.exports.values() if value["sha256"] == ack["checkpoint"]["sha256"]]
        need(len(checkpoint_refs) == 1, "overlap_workload_artifact_missing")
        checkpoint = decode(read_file({key: checkpoint_refs[0][key] for key in ("path", "sha256")}))
        need(all(checkpoint[key] == self.manifest[key] for key in SCOPE) and
             checkpoint["scenario_id"] == scenario["scenario_id"] and
             checkpoint["binding_sha256"] == oracle.plan["workload_binding"]["sha256"], "overlap_workload_scope")
        need(any(row["private_artifact"]["sha256"] == ack["checkpoint"]["sha256"] for row in
                 self.native("acknowledge", "workload-checkpoint")), "overlap_workload_not_bound")
        workload_manifest = load_json(self.manifest["workload_manifest"])
        need(self.modules["oracle"].workload_checkpoint(checkpoint, self.modules["workload"], workload_manifest,
             ack["checkpoint"]["sha256"]) == ack["checkpoint"], "productive_overlap_projection_changed")
        scope = dict(config["scope"])
        expectation = "interrupted" if scenario["require_interrupted_jobs"] else "preserve"
        if fault in controller.REPLACEMENTS:
            expectation, scope["fault_root_ids"] = "explicit-rebind", [oracle.plan["root_id"]]
        comparison = state.compare_external(baseline["reference"], recovered["reference"],
            [row["reference"] for row in saved["rebind_acks"]], config["binding_sha256"], scope, expectation)
        need(comparison["passed"] is True and comparison["failure_count"] == 0 and comparison["failures"] == [] and
             recovered["inspection"]["controller_state"] == recovery["state"], "recovery_sqlite_comparison_failed")
        need(oracle.interrupted(recovered, comparison) == recovery["interrupted_jobs"], "interrupted_job_projection_changed")
        playback = self.native("recovery_observation", "probe-playback_resume")
        need(len(playback) == 1, "resume_probe_population")
        oracle.media_proof(playback[0]["media_decode"])
        need({key: playback[0][key] for key in self.modules["oracle"].RESUME_FIELDS} == recovery["playback_resume"],
             "resume_native_projection_changed")
        health_operations = ["recovery_observation"]
        if fault in controller.STORAGE:
            health_operations.append("fault_observation")
        if scenario["late_mount"]:
            health_operations.append("late_mount_observation")
        for operation in health_operations:
            native_rows = self.native(operation, "probe-healthy_probes")
            for rows in native_rows:
                for row in rows:
                    need(not row["client_timed_out"] and not row["client_disconnected"], "healthy_probe_incomplete")
                    if row["operation"] == "playback":
                        oracle.media_proof(row["media_decode"])
            projection = self.call(operation)["receipt"]["data"]["healthy_probes"]
            expected = [{key: row[key] for key in self.modules["oracle"].HEALTHY_FIELDS}
                        for rows in native_rows for row in rows]
            need(all(row in expected for row in projection), "healthy_probe_raw_projection_changed")
        fault_value = self.call("fault_observation")["receipt"]["data"]
        need(fault_value == saved["fault_observation"], "fault_projection_changed")
        if fault in controller.STORAGE:
            during = self.snapshot(saved["fault_snapshot"])
            controller.assert_preserved(ack["acknowledged_state"], during["inspection"]["controller_state"])
        before = saved["before_guest"]
        before_operation = "guest_identity" if fault == "guest_reset" else "inject"
        # guest_identity has two successful observations; bind its baseline
        # directly to the first call rather than pretending it is unique.
        before_requests = [row["request"]["request_id"] for row in self.calls if
                           row["request"]["operation"] == before_operation]
        need(any(row["kind"] == "guest-observe" and row["data"].get("data") == before
                 for request in before_requests for row in self.by_request.get(request, [])),
             "original_guest_identity_not_bound")
        fault_guest = self.native("fault_observation", "guest-observe")[-1]
        if scenario["late_mount"]:
            late = self.call("late_mount_observation")["receipt"]["data"]
            guest = self.native("late_mount_observation", "guest-observe")[-1]
            affected = self.native("late_mount_observation", "probe-affected_probe")[-1]
            point = self.manifest["volumes"][scenario["volume_id"]]["mountpoint"]
            need(guest["boot_id"] == late["boot_id"] != before["boot_id"] and
                 not any(row["mountpoint"] == point for row in guest["mounts"]) and
                 affected["http"]["http_status"] not in (200, 206), "late_mount_raw_evidence_missing")
        if fault in controller.STORAGE:
            affected = saved["affected"]
            self.bind("fault_observation", "probe-affected_probe", affected)
            if fault == "mount_loss":
                point = self.manifest["volumes"][scenario["volume_id"]]["mountpoint"]
                need(not any(row["mountpoint"] == point for row in fault_guest["mounts"]) and
                     affected["http"]["http_status"] not in (200, 206), "actual_mount_loss_missing")
            elif fault in controller.REPLACEMENTS:
                binding = affected["admission_or_binding"]["Binding"]
                target = saved["injection"]["target"]
                old = [row for row in before["mounts"] if row["mountpoint"] == target]
                new = [row for row in fault_guest["mounts"] if row["mountpoint"] == target]
                need(len(new) == 1 and (not old or old[-1]["mount_id"] != new[0]["mount_id"]) and
                     binding["Status"] == "mismatch" and binding["ApprovedFingerprint"] != binding["ObservedFingerprint"],
                     "actual_replacement_fence_missing")
            elif fault in {"permission_failure", "enospc"}:
                self.bind("fault_observation", "guest-observe_failure_errno", saved["errno_probe"])
                need(saved["errno_probe"]["probe_uid"] == self.manifest["guest"]["goby_uid"] and
                     saved["errno_probe"]["errno"] == ("EACCES" if fault == "permission_failure" else "ENOSPC"),
                     "actual_unprivileged_failure_missing")
                if fault == "permission_failure":
                    need(affected["http"]["http_status"] not in (200, 206), "actual_permission_read_not_denied")
                else:
                    snapshot = self.snapshot(saved["product_failure"]["snapshot"])
                    admission = affected["admission_or_binding"]["RunId"]
                    tasks = [row for row in oracle.rows(snapshot, "task_runs") if row["id"] == admission]
                    children = [row for row in oracle.rows(snapshot, "task_run_children") if row["run_id"] == admission]
                    failed = [row for row in children if row["state"] == "failed" and row.get("error_code")]
                    need(len(tasks) == 1 and tasks[0]["task_key"] == "media.preview_generation" and
                         tasks[0]["state"] == "failed" and bool(failed), "actual_enospc_product_failure_missing")
                    admitted = [row for row in oracle.rows(snapshot, "analysis_work_sources") if
                                row["child_id"] in {child["id"] for child in failed}]
                    need(admitted and {row["item_id"] for row in admitted} == {affected["operation_item_id"]},
                         "failed_preview_admission_changed")
        elif fault == "postgres_disconnect":
            pg = self.native("fault_observation", "guest-observe_postgres")[-1]
            injection = saved["injection"]
            need(not any(row["pid"] == injection["pid"] and row["backend_start"] == injection["backend_start"]
                         for row in pg["backends"]) and
                 self.native("fault_observation", "probe-readiness")[-1]["ready_status"] == 503,
                 "actual_postgres_disconnect_missing")
        elif fault in controller.RESTARTS:
            if fault in controller.REBOOTS:
                need(fault_guest["boot_id"] != before["boot_id"], "actual_guest_boot_unchanged")
            else:
                role = "postgres" if fault == "postgres_restart" else "goby"
                need(fault_guest[role].get("pid", 0) == 0 or
                     evidence.process_key(fault_guest[role]) != evidence.process_key(before[role]),
                     "original_process_termination_missing")
        if fault.startswith("blocked_"):
            first, last = saved["blocked_samples"]
            self.bind("fault_observation", "guest-observe", first)
            self.bind("fault_observation", "guest-observe", last)
            affected = saved["affected"]
            self.bind("fault_observation", "probe-affected_probe", affected)
            http = affected["http"]
            request = {"outcome": "timeout" if http["client_timed_out"] else "bounded_503", "http_method": http["method"],
                       "http_status": http["http_status"], "http_started_unix_ns": http["started_unix_ns"],
                       "http_completed_unix_ns": http["completed_unix_ns"],
                       "request_sha256": self.modules["oracle"].digest(http),
                       "source_path": affected["expected_source_identity"]["path"]}
            witness, descriptor = evidence.blocked_witness(first, last, request,
                self.manifest["volumes"][scenario["volume_id"]], self.manifest["guest"]["goby_cgroup"],
                "read" if fault == "blocked_read" else "metadata")
            need(witness == fault_value["facts"]["blocked_task"] and descriptor == saved["blocked_descriptor"],
                 "blocked_task_projection_changed")
            rows = self.records("recovery_observation", "concrete-blocked-completion")
            need(len(rows) == 1, "concrete_blocked_completion_missing")
            completion, raw = dict(rows[0]["data"]["completion"]), rows[0]["data"]["raw_resources"]
            self.resources(oracle, "recovery_observation", recovered, raw)
            self.bind("fault_observation", "probe-runtime_resources", saved["blocked_resources"])
            old_resources, new_resources = saved["blocked_resources"]["resources"], raw["native"]["resources"]
            if fault == "blocked_read":
                lease = evidence.original_lease_completion(old_resources, new_resources, affected["item_id"],
                    affected["media_source_id"], http["started_unix_ns"], http["completed_unix_ns"])
                need(completion["lease"] == lease, "original_lease_projection_changed")
            else:
                need(completion["before"] == old_resources and completion["after"] == new_resources and
                     completion["admissions_stopped"] == saved["settled"]["reference"], "metadata_completion_not_bound")
            completion["evidence_sha256"] = rows[0]["reference"]["sha256"]
            final = self.native("recovery_observation", "guest-observe")[-1]
            retired = evidence.retired_worker(witness, descriptor, final, completion, before, [first, last])
            need(recovery["closure"]["retired_workers"] == [retired], "blocked_retirement_projection_changed")
        if fault == "postgres_lock_wait":
            pg = saved["pg_wait_evidence"]
            self.bind("inject", "guest-observe_postgres", saved["pg_before"])
            for value in (pg["waiting"], pg["after"]):
                self.bind("fault_observation", "guest-observe_postgres", value)
            self.bind("fault_observation", "guest-observe_postgres_log", pg["log"])
            log = pg["log"]
            raw = base64.b64decode(log["raw_base64"], validate=True)
            need(len(raw) == log["raw_bytes"] and hashlib.sha256(raw).hexdigest() == log["raw_sha256"], "postgres_log_hash")
            before_state = self.snapshot(saved["pg_before_fault_snapshot"])["inspection"]["controller_state"]
            after_state = self.snapshot(pg["after_snapshot"])["inspection"]["controller_state"]
            blockers = sorted(row["pid"] for row in pg["waiting"]["backends"]
                              if row["application_name"] == "goby-phase3-fault-controller")
            computed = evidence.postgres_lock_facts(saved["pg_before"], pg["waiting"], pg["after"],
                [decode(line) for line in raw.splitlines() if line], before_state, after_state,
                {**saved["injection"], "blocker_backend_pids": blockers})
            need(all(fault_value["facts"][key] == value for key, value in computed.items()), "postgres_lock_projection_changed")
        value = self.closure_inputs()
        closed_snapshot = self.snapshot_for("closure_observation")
        resources = self.resources(oracle, "closure_observation", closed_snapshot, value["resources"])
        raw_guest = value["resources"]["guest"]
        workload = value["workload"]
        workload_raw = read_file({key: workload[key] for key in ("path", "sha256")})
        need(len(workload_raw) == workload["bytes"], "workload_closure_size")
        facts = decode(workload_raw)
        need(all(facts[key] == self.manifest[key] for key in SCOPE) and
             facts["scenario_id"] == scenario["scenario_id"] and
             facts["binding_sha256"] == oracle.plan["workload_binding"]["sha256"], "workload_closure_scope")
        need(any(row["private_artifact"]["sha256"] == workload["sha256"] for row in
                 self.native("closure_observation", "workload-checkpoint")), "workload_closure_not_bound")
        unit = facts["unit_state"]
        need(int(unit["MainPID"]) == 0 and unit["cgroup_pids"] == [] and unit["ActiveState"] in {"inactive", "failed"},
             "workload_unit_not_closed")
        observed = raw_guest["scenario_closure"]
        lock = observed["lock_unit"]
        need(lock is None or lock["main_pid"] == 0 and lock["cgroup_pids"] == [] and
             lock["active_state"] in {"inactive", "failed"}, "fault_lock_not_closed")
        need(observed["filler"] is None or observed["filler"]["exists"] is False, "fault_filler_remains")
        for volume in self.manifest["volumes"]:
            need(raw_guest["volume_observations"][volume]["suspended"] is False, "fault_mapping_suspended")
            evidence.original_mount_restored(before, raw_guest, volume)
        geometry = ("major_minor", "root", "mountpoint", "filesystem", "source")
        project = lambda mounts: [{key: row[key] for key in geometry} for row in mounts]
        need(project(before["scenario_closure"]["mounts_at_target"]) == project(observed["mounts_at_target"]),
             "original_target_mount_not_restored")
        if fault == "permission_failure":
            need(all(observed["target"][key] == saved["injection"]["before_" + key]
                     for key in ("device", "inode", "mode")), "original_target_mode_not_restored")
        need(self.call("closure_observation")["receipt"]["data"]["unclosed_temporary_bytes"] == resources["temporary_bytes"],
             "closure_projection_changed")
        return {"raw_records": len(saved["records"]), "external_artifacts": len(self.exports),
                "native_acknowledgements": len(ack["native_state_acks"])}


def load_modules(spec):
    result = {}
    for name, filename in MODULES.items():
        path = Path(__file__).resolve().with_name(filename)
        need(spec[name]["path"] == str(path), "only_bundled_validator_sources_allowed")
        read_file(spec[name], maximum=2 << 20, private=False)
        module_spec = importlib.util.spec_from_file_location("phase3_matrix_" + name, path)
        module = importlib.util.module_from_spec(module_spec)
        sys.modules[module_spec.name] = module
        module_spec.loader.exec_module(module)
        read_file(spec[name], maximum=2 << 20, private=False)
        result[name] = module
    return result


def validate_case_scope(matrix, tier, case, manifest):
    need(all(manifest[key] == matrix[key] for key in ("source_revision", "owner_id")) and
         manifest["tier"] == tier["tier"] and manifest["profile_id"] == tier["profile_id"] and
         manifest["run_id"] == case["run_id"], "case_scope_mismatch")
    need(all(manifest["guest"][key] == value for key, value in matrix["guest"].items()), "case_guest_scope_mismatch")
    need(len(manifest["scenarios"]) == 1 and all(manifest["scenarios"][0][key] == case[key]
             for key in ("scenario_id", "fault", "late_mount")), "case_scenario_mismatch")
    need(PurePosixPath(manifest["controller"]["artifacts_root"]) in PurePosixPath(case["journal"]["directory"]).parents,
         "journal_outside_case_artifacts")


def retained_input(case, original):
    matches = [row["retained"] for row in case["inputs"] if row["original"] == original]
    need(len(matches) == 1, "required_original_input_not_retained")
    return load_json(matches[0])


def validate_context(matrix, manifest, case, modules):
    adapters = manifest["adapters"]
    need(adapters["executor"] == adapters["observer"] == adapters["workload"], "oracle_adapter_context_split")
    need(adapters["observer"]["sha256"] == matrix["modules"]["oracle"]["sha256"], "oracle_source_pin_mismatch")
    context = load_json(adapters["observer"]["context"])
    need(context["schema_version"] == 1 and context["released"] is True and
         all(context[key] == manifest[key] for key in SCOPE) and
         context["controller_machine_id"] == manifest["controller"]["machine_id"], "oracle_context_scope_mismatch")
    contract = context["contract"]
    required_contract = {"guest", "volumes", "healthy_roots", "budgets"}
    need(required_contract <= set(contract) <= required_contract | {"state_validator"} and
         all(contract[key] == manifest[key] for key in contract) and
         context["state_scope"] == manifest["state_validator"]["scope"] and
         context["state_binding_canonical_sha256"] == manifest["state_validator"]["binding_sha256"] and
         context["workload_manifest"] == manifest["workload_manifest"], "oracle_contract_changed")
    need(set(context["scenarios"]) == {case["scenario_id"]} and
         context["scenarios"][case["scenario_id"]]["scenario"] == manifest["scenarios"][0], "oracle_scenario_changed")
    for name in ("controller", "state", "evidence", "workload"):
        need(context["modules"][name]["sha256"] == matrix["modules"][name]["sha256"], "validator_source_pin_mismatch")
    for ref in (context["modules"]["transport"], {key: adapters["hypervisor"][key] for key in ("path", "sha256")}):
        read_file(ref, maximum=2 << 20, private=False)
    need(manifest["state_validator"]["source"]["sha256"] == matrix["modules"]["state"]["sha256"],
         "state_validator_source_pin_mismatch")
    workload = load_json(manifest["workload_manifest"])
    need(all(workload[key] == manifest[key] for key in SCOPE), "workload_manifest_scope_mismatch")
    transport = context["transport"]
    need(transport["run_id"] == manifest["run_id"] and transport["owner_id"] == manifest["owner_id"] and
         transport["guest"] == matrix["guest"] and
         transport["artifacts_root"] == manifest["controller"]["artifacts_root"], "transport_scope_mismatch")
    guest = transport["remote"]["guest"]
    binding, release = retained_input(case, guest["binding"]), retained_input(case, guest["release"])
    need(binding["volumes"] == manifest["volumes"] and binding["owned_root"] == manifest["guest"]["owned_root"],
         "guest_fault_volume_binding_mismatch")
    state = retained_input(case, transport["remote"]["state"]["binding"])
    need(modules["state"].digest(state) == manifest["state_validator"]["binding_sha256"],
         "native_state_binding_canonical_hash_mismatch")
    for role in ("probe", "workload"):
        native = retained_input(case, transport["remote"][role]["binding"])
        need(all(native[key] == manifest[key] for key in SCOPE), "native_binding_scope_mismatch")
        if role == "workload":
            need(native["released"] is True and native["scenario_id"] == case["scenario_id"] and
                 context["scenarios"][case["scenario_id"]]["workload_binding"] == transport["remote"][role]["binding"],
                 "workload_case_binding_mismatch")
    return context, binding, release, guest["binding"]["sha256"]


def validate_reset(case, call, manifest, hypervisor):
    intent, receipt = (load_json(case["reset_evidence"][key]) for key in ("intent", "receipt"))
    request = call["request"]
    need(intent["marker"] == "goby-phase3-pve106-reset-intent-v1" and
         all(intent[key] == request[key] for key in ("request_id", *SCOPE)) and
         intent["vmid"] == 106 and intent["smbios_uuid"] == manifest["guest"]["smbios_uuid"] and
         intent["context_sha256"] == request["context_sha256"] and intent["automatic_retry"] is False,
         "reset_intent_binding_mismatch")
    need(all(intent["durable_ack"][key] == request["payload"]["durable_ack"][key] for key in ("path", "sha256")),
         "reset_durable_ack_link_mismatch")
    need(receipt["marker"] == "goby-phase3-pve106-reset-receipt-v1" and receipt["complete"] is True and
         receipt["automatic_retry"] is False and receipt["intent_sha256"] == case["reset_evidence"]["intent"]["sha256"] and
         receipt["response"] == call["receipt"], "reset_receipt_binding_mismatch")
    evidence = receipt["submission_evidence"]
    need(evidence["dispatches"] == 1 and evidence["pvesh_exit_code"] == 0 and evidence["task_status"] == "stopped" and
         evidence["task_exitstatus"] == "OK" and evidence["pve_task_id"] == call["receipt"]["data"]["pve_task_id"] and
         evidence["vmid"] == 106 and evidence["pve_operation"] == "reset", "reset_task_not_successful")
    stdout = read_file({key: evidence["stdout"][key] for key in ("path", "sha256")}, maximum=4 << 20)
    stderr = read_file({key: evidence["stderr"][key] for key in ("path", "sha256")}, maximum=4 << 20)
    need(len(stdout) == evidence["stdout"]["bytes"] and len(stderr) == evidence["stderr"]["bytes"] and
         evidence["pve_task_id"].encode("ascii") in stdout, "reset_submission_bytes_missing")
    release = hypervisor["release"]
    need(hypervisor["reset_scenario"] == manifest["scenarios"][0] and release["allow_forced_reset"] is True and
         release["max_reset_dispatches"] == 1 and release["not_before_unix_ns"] <= intent["created_unix_ns"] <=
         release["expires_unix_ns"] and evidence["observed_unix_ns"] >= intent["created_unix_ns"],
         "reset_release_window_mismatch")


def compose(matrix, matrix_sha256, modules):
    reports, native_paths, case_roots = [], set(), set()
    for tier in sorted(matrix["tiers"], key=lambda row: row["tier"]):
        for case in tier["cases"]:
            manifest = modules["controller"].load_manifest(load_json(case["manifest"]))
            validate_case_scope(matrix, tier, case, manifest)
            root = manifest["controller"]["artifacts_root"]
            need(root not in case_roots, "reused_case_artifact_root")
            case_roots.add(root)
            expected_oracle = Path(root) / ("oracle-state-" + hashlib.sha256(case["scenario_id"].encode()).hexdigest()[:24] + ".json")
            need(case["oracle_state"]["path"] == str(expected_oracle), "oracle_state_original_path_required")
            need(manifest["controller"]["machine_id"] == Path("/etc/machine-id").read_text().strip(),
                 "external_controller_machine_mismatch")
            marker = load_json(manifest["controller"]["marker"])
            need(marker == {"schema_version": 1, "owner_id": matrix["owner_id"],
                 "role": "external_recovery_controller", "machine_id": manifest["controller"]["machine_id"]},
                 "controller_owner_marker_mismatch")
            context, binding, release, binding_sha256 = validate_context(matrix, manifest, case, modules)
            records = read_journal(case["journal"])
            validate_report(records[-1]["event"]["data"], manifest, case["manifest"]["sha256"])
            calls = replay_case(modules["controller"], modules["state"], manifest, case["manifest"]["sha256"], records)
            hypervisor = load_json(manifest["adapters"]["hypervisor"]["context"])
            need(hypervisor["ready"] is True and hypervisor["released"] is True and
                 all(hypervisor[key] == manifest[key] for key in SCOPE) and
                 hypervisor["guest"] == manifest["guest"] and
                 hypervisor["volumes"] == manifest["volumes"], "hypervisor_context_scope_mismatch")
            for call in calls:
                if call["request"]["operation"] in {"inject", "recover", "close_scenario", "arm_late_mount", "forced_reset"}:
                    validate_guest_release(binding, release, manifest, binding_sha256,
                                           call["intent"]["event"]["controller_unix_ns"])
                if call["request"]["operation"] == "forced_reset":
                    validate_reset(case, call, manifest, hypervisor)
            saved = load_json(case["oracle_state"])
            if case["fault"] != "guest_reset":
                validate_guest_release(binding, release, manifest, binding_sha256, saved["injection"]["injected_unix_ns"])
            for ref in saved["records"]:
                need(Path(ref["path"]).parent == Path(root), "oracle_record_outside_case_artifacts")
            raw = RawEvidence(saved, calls, manifest, modules)
            for item in saved["records"]:
                need(item["path"] not in native_paths, "native_receipt_reused_between_cases")
                native_paths.add(item["path"])
            counts = raw.audit(context)
            reports.append({"tier": tier["tier"], "profile_id": tier["profile_id"], "scenario_id": case["scenario_id"],
                "run_id": case["run_id"], "fault": case["fault"], "late_mount": case["late_mount"],
                "manifest_sha256": case["manifest"]["sha256"], "journal_terminal_sha256": case["journal"]["terminal"]["sha256"],
                "oracle_state_sha256": case["oracle_state"]["sha256"], "original_status": "partial", **counts})
    for ref in matrix["retained_failures"]:
        read_file(ref)
    return {"schema_version": 1, "matrix_id": matrix["matrix_id"], "matrix_manifest_sha256": matrix_sha256,
            "source_revision": matrix["source_revision"], "status": "passed", "complete_fault_matrix": True,
            "case_count": len(reports), "tiers": [10000, 100000], "cases": reports,
            "retained_failure_sha256": [row["sha256"] for row in matrix["retained_failures"]],
            "physical_power_loss_tested": False, "capacity_acceptance_inferred": False,
            "regression_acceptance_inferred": False, "external_controller_tree_closure_established": False}


def write_new(path, value):
    target = Path(str(absolute(path)))
    need(target.parent.resolve(strict=True) == target.parent, "output_parent_symlink")
    raw = canonical(value) + b"\n"
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(target.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    try:
        need(sys.platform == "linux", "external_linux_controller_required")
        manifest = load_manifest(load_json({"path": args.manifest, "sha256": args.manifest_sha256}))
        modules = load_modules(manifest["modules"])
        result = compose(manifest, args.manifest_sha256, modules)
        write_new(args.output, result)
        print(canonical(result).decode("ascii"), flush=True)
        return 0
    except (Exception, KeyboardInterrupt) as error:
        code = str(error) if isinstance(error, Invalid) else type(error).__name__
        print(canonical({"schema_version": 1, "status": "failed", "complete_fault_matrix": False,
                         "reason": code, "original_evidence_modified": False}).decode("ascii"), flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
