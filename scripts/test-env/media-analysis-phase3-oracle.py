"""External, evidence-derived Phase 3 recovery adapter.

Use as the recovery controller's executor, observer and workload adapter. This
source does not authorize a run: all native bindings and this context must be
explicitly released. Every successful projection retains its raw input records.
"""
import argparse
import base64
import concurrent.futures
import copy
import decimal
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import sys
import time
import threading
import types


MAX_JSON = 16 << 20
SAFE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}\Z")
SHA = re.compile(r"[0-9a-f]{64}\Z")
MUTATIONS = {"acknowledge", "arm_late_mount", "inject", "recover", "settle_scenario", "close_scenario",
             "rebind_replacement", "rebind_original"}
IDENTITY_FIELDS = ("owner_id", "vmid", "name", "machine_id", "smbios_uuid", "owner_marker_sha256",
                   "boot_id", "btime", "goby", "postgres", "disks", "volumes", "volume_observations", "observed_unix_ns")
VOLUME_FIELDS = ("present", "mounted", "dm_uuid", "major_minor", "loop_backing_file", "size_bytes")
HEALTHY_FIELDS = ("root_id", "operation", "http_status", "duration_ms", "started_unix_ns",
                  "completed_unix_ns", "response_sha256", "bytes_received")
RESUME_FIELDS = ("reconnected", "persisted_ticks", "requested_ticks", "observed_start_ticks",
                 "tolerance_ticks", "http_status", "media_bytes")
ACK_KIND = {"metadata": "metadata", "favorite": "user_state", "user_name": "user_state",
            "preferences": "user_state", "display_preferences": "user_state", "progress": "playback_progress",
            "analysis_configuration": "settings", "managed_configuration": "settings"}


class Refusal(RuntimeError):
    pass


class Pending(Refusal):
    pass


def need(value, code):
    if not value:
        raise Refusal(code)


def canonical(value):
    # Preserve SQL int64/Decimal values through the orchestration boundary.
    if isinstance(value, dict):
        return b"{" + b",".join(json.dumps(key).encode() + b":" + canonical(value[key]) for key in sorted(value)) + b"}"
    if isinstance(value, (list, tuple)):
        return b"[" + b",".join(canonical(item) for item in value) + b"]"
    if isinstance(value, decimal.Decimal):
        need(value.is_finite(), "nonfinite_json")
        return format(value, "f").encode("ascii")
    return json.dumps(value, separators=(",", ":"), ensure_ascii=True, allow_nan=False).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def decode(raw):
    def pairs(values):
        result = {}
        for key, value in values:
            need(key not in result, "duplicate_json_key")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs, parse_float=decimal.Decimal,
                      parse_constant=lambda _: (_ for _ in ()).throw(Refusal("nonfinite_json")))


def read_file(path, expected=None, private=True, maximum=MAX_JSON):
    path = Path(path)
    need(path.is_absolute() and path.resolve(strict=True) == path, "noncanonical_artifact")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == os.geteuid() and before.st_nlink == 1
             and not before.st_mode & 0o022 and (not private or stat.S_IMODE(before.st_mode) == 0o600)
             and 0 < before.st_size <= maximum, "artifact_identity")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            raw = stream.read(maximum + 1)
        after = os.fstat(fd)
        need((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
             (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)
             and len(raw) == before.st_size, "artifact_changed")
        if expected is not None:
            need(SHA.fullmatch(expected) and hashlib.sha256(raw).hexdigest() == expected, "artifact_hash_mismatch")
        return raw
    finally:
        os.close(fd)


def load_module(reference, name):
    raw = read_file(reference["path"], reference["sha256"], private=False, maximum=2 << 20)
    module = types.ModuleType(name)
    module.__file__ = reference["path"]
    sys.modules[name] = module
    exec(compile(raw, reference["path"], "exec"), module.__dict__)
    return module


def common_overlap(groups):
    result = list(groups[0])
    for group in groups[1:]:
        result = [(max(a, c), min(b, d)) for a, b in result for c, d in group if max(a, c) < min(b, d)]
        need(len(result) <= 100000, "overlap_comparison_bound")
    return sorted(result)


def workload_checkpoint(facts, module, manifest, proof_sha256):
    """Recompute productive overlap; active_lanes alone cannot pass admission."""
    need(facts["phase_errors"] == [] and facts["observer_errors"] == [] and facts["worker_alive"] is True
         and facts["observer_alive"] is True and facts["admissions_stopped"] is False, "workload_not_active_and_clean")
    phase = facts["phase"]
    samples = facts["samples"]
    maximum_gap = int(manifest["budgets"]["poll_seconds"] * 4_000_000_000)
    spans = {}
    for kind in ("scan", "intro", "previews"):
        current = [row["observations"][-1] for row in samples if row["phase"] == phase and row["kind"] == kind
                   and row["observations"]]
        need(any(row[2] is True and facts["observed_monotonic_ns"] - maximum_gap <= row[0] <= facts["observed_monotonic_ns"]
                 for row in current), "workload_current_analysis_or_scan_missing")
        spans[kind] = [span for row in samples if row["phase"] == phase and row["kind"] == kind
                       for span in module.confirmed_intervals(row["observations"], maximum_gap)]
    events = facts["events"]
    queries = [(row["start_ns"], row["end_ns"]) for row in events if row["kind"] == "http"
               and row.get("phase") == phase and row["label"].startswith("query-")
               and not row["error"] and 200 <= row["status"] < 300]
    playback = [(row["start_ns"], row["end_ns"]) for row in events if row["kind"] == "playback_bytes"
                and row.get("phase") == phase and row.get("bytes", 0) > 0]
    windows = []
    anchor = facts["anchor"]
    need(abs((facts["observed_unix_ns"] - anchor["unix_ns"]) -
             (facts["observed_monotonic_ns"] - anchor["monotonic_ns"])) <= 100_000_000,
         "workload_wall_clock_discontinuity")
    for kind, lane in (("intro", "intro_analysis"), ("previews", "preview")):
        processes = [row for row in facts["process_observations"].values() if row["phase"] == phase and row["kind"] == kind
                     and row["source_paths"] and row["lineage"] and Path(row["executable"]).name in {"ffmpeg", "ffprobe"}]
        productive = [span for row in processes for span in row["work_intervals"]]
        overlap = common_overlap([spans["scan"], spans[kind], productive, queries, playback])
        need(overlap and module.union_length(overlap) >= manifest["thresholds"]["min_all_lane_overlap_ms"] * 1_000_000,
             "actual_productive_overlap_missing")
        lower, upper = overlap[-1]
        need(upper >= facts["observed_monotonic_ns"] - maximum_gap, "workload_overlap_stale")
        need(0 <= lower < upper <= facts["observed_monotonic_ns"], "workload_overlap_clock")
        windows.append({"analysis_lane": lane, "lanes": ["scan", "search", "playback", lane],
                        "start_unix_ns": anchor["unix_ns"] + lower - anchor["monotonic_ns"],
                        "end_unix_ns": anchor["unix_ns"] + upper - anchor["monotonic_ns"],
                        "process_source_fd_sha256": digest(processes)})
    return {**{key: manifest[key] for key in ("run_id", "owner_id", "profile_id", "source_revision", "tier")},
            "sha256": proof_sha256, "active_lanes": ["scan", "search", "playback", "intro_analysis", "preview"],
            "observed_unix_ns": facts["observed_unix_ns"], "overlap_windows": windows}


class Oracle:
    def __init__(self, context, request):
        self.c, self.q, self.serial = context, request, 0
        self.io_lock = threading.RLock()
        required = {"schema_version", "released", "controller_machine_id", "run_id", "owner_id", "source_revision",
                    "profile_id", "tier", "contract", "modules", "transport", "state_scope",
                    "state_binding_canonical_sha256", "workload_manifest", "scenarios", "budgets"}
        need(set(context) == required and context["schema_version"] == 1 and context["released"] is True,
             "oracle_not_released")
        need(sys.platform == "linux" and Path("/etc/machine-id").read_text().strip() == context["controller_machine_id"]
             and context["controller_machine_id"] != context["transport"]["guest"]["machine_id"], "external_controller_required")
        for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier"):
            need(request[key] == context[key], "oracle_scope_changed")
        self.scenario = request["scenario"]
        self.case_id = self.scenario["scenario_id"]
        self.plan = context["scenarios"][self.case_id]
        need(len(context["scenarios"]) == 1, "fresh_single_case_context_required")
        need(self.scenario == self.plan["scenario"], "scenario_not_frozen")
        need(request["guest"] == context["contract"]["guest"] and request["volumes"] == context["contract"]["volumes"],
             "guest_or_volume_scope_changed")
        self.root = Path(request["artifacts_root"])
        info = self.root.lstat()
        need(self.root == Path(context["transport"]["artifacts_root"]) and self.root.resolve(strict=True) == self.root
             and info.st_uid == os.geteuid() and stat.S_IMODE(info.st_mode) == 0o700, "external_artifact_root")
        self.modules = {name: load_module(reference, "phase3_oracle_" + name) for name, reference in context["modules"].items()}
        need(set(self.modules) == {"controller", "transport", "state", "evidence", "workload"}, "oracle_module_set")
        config = copy.deepcopy(context["transport"])
        config["remote"]["workload"]["binding"] = self.plan["workload_binding"]
        self.transport = self.modules["transport"].Transport(config)
        self.manifest = {**context["contract"], **{key: context[key] for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier")}}
        self.state_path = self.root / ("oracle-state-" + hashlib.sha256(self.case_id.encode()).hexdigest()[:24] + ".json")
        self.saved = decode(read_file(self.state_path)) if self.state_path.exists() else {"version": 1, "scenario_id": self.case_id, "records": []}
        need(self.saved["scenario_id"] == self.case_id, "oracle_state_scope")
        self.deadline = time.monotonic() + context["budgets"]["operation_seconds"]
        self.record_refs = []

    def remaining(self):
        value = self.deadline - time.monotonic()
        need(value > 0, "oracle_operation_deadline")
        return value

    def budget(self, extra=0):
        total, files = 0, 0
        for path in self.root.iterdir():
            item = path.lstat()
            need(not stat.S_ISLNK(item.st_mode), "external_artifact_symlink")
            if stat.S_ISREG(item.st_mode):
                total += max(item.st_size, item.st_blocks * 512)
                files += 1
        need(total + extra <= self.c["budgets"]["external_bytes"] and files < self.c["budgets"]["external_files"],
             "external_evidence_budget")
        free = os.statvfs(self.root)
        need(free.f_bavail * free.f_frsize >= self.c["budgets"]["free_floor_bytes"] + extra, "external_free_floor")

    def record(self, kind, value):
        with self.io_lock:
            return self._record(kind, value)

    def _record(self, kind, value):
        self.serial += 1
        raw = canonical({"kind": kind, "request_id": self.q["request_id"], "data": value}) + b"\n"
        need(len(raw) <= MAX_JSON, "oracle_record_bound")
        self.budget(len(raw))
        name = hashlib.sha256((self.q["request_id"] + ":" + str(self.serial)).encode()).hexdigest() + ".oracle.json"
        path = self.root / name
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        reference = {"path": str(path), "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest(), "fsynced": True}
        self.record_refs.append(reference)
        return reference

    def persist(self):
        with self.io_lock:
            self._persist()

    def _persist(self):
        self.saved["records"].extend(self.record_refs)
        self.record_refs = []
        need(len(self.saved["records"]) <= 2048, "oracle_record_count_bound")
        raw = canonical(self.saved) + b"\n"
        need(len(raw) <= MAX_JSON, "oracle_state_file_bound")
        self.budget(len(raw))
        stage = self.state_path.with_name(self.state_path.name + "." + hashlib.sha256(self.q["request_id"].encode()).hexdigest()[:12])
        fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        os.replace(stage, self.state_path)
        directory = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)

    def native_id(self, role, operation):
        with self.io_lock:
            self.serial += 1
            return "oracle-" + hashlib.sha256((self.q["request_id"] + ":" + role + ":" + operation + ":" + str(self.serial)).encode()).hexdigest()[:40]

    def guest(self, operation, payload=None):
        request = {"schema_version": 1, "request_id": self.native_id("guest", operation), "op": operation,
                   **{key: self.c[key] for key in ("run_id", "owner_id", "source_revision")},
                   "binding_sha256": self.c["transport"]["remote"]["guest"]["binding"]["sha256"],
                   "scenario": self.scenario, "payload": payload or {}}
        reply = self.transport.call("guest", request, timeout=min(self.remaining(), 120))
        self.record("guest-" + operation, reply)
        need(reply.get("status") == "ok" and reply.get("op") == operation and reply.get("owner_id") == self.c["owner_id"],
             "guest_collector_refused")
        return reply["data"]

    def state_call(self, operation, **values):
        request = {"version": 1, "request_id": self.native_id("state", operation), "op": operation, **values}
        reply = self.transport.call("state", request, timeout=min(self.remaining(), 120))
        self.record("state-" + operation, reply)
        need(reply.get("ok") is True, "state_collector_refused")
        return reply["result"]

    def probe(self, operation, payload):
        request = {"schema_version": 1, "request_id": self.native_id("probe", operation), "operation": operation,
                   **{key: self.c[key] for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier")},
                   "scenario": self.scenario, "payload": payload}
        reply = self.transport.call("probe", request, timeout=min(self.remaining(), 120))
        self.record("probe-" + operation, reply)
        need(reply.get("status") == "ok" and reply.get("operation") == operation and reply.get("owner_id") == self.c["owner_id"],
             "probe_collector_refused")
        return reply["data"]

    def export(self, reference, identifier_key, identifier):
        existing = self.root / (reference["sha256"] + "-" + Path(reference["path"]).name)
        if existing.exists():
            # The post-resume ACK references snapshots already exported before
            # its mutation. Reuse only those exact, immutable original bytes.
            fd = os.open(existing, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                info = os.fstat(fd)
                need(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and info.st_nlink == 1
                     and stat.S_IMODE(info.st_mode) == 0o600 and info.st_size == reference["bytes"], "retained_artifact_identity")
                checksum = hashlib.sha256()
                while chunk := os.read(fd, 1 << 20):
                    checksum.update(chunk)
                after = os.fstat(fd)
                need(checksum.hexdigest() == reference["sha256"] and
                     (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns) ==
                     (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns), "retained_artifact_changed")
                os.fsync(fd)
            finally:
                os.close(fd)
            retained = {"path": str(existing), "sha256": reference["sha256"], "bytes": reference["bytes"], "fsynced": True}
        else:
            self.budget(reference["bytes"])
            retained = self.transport.export_artifact(reference)
        need(retained["fsynced"] is True, "external_artifact_not_fsynced")
        self.record("external-artifact", {"guest": reference, "external": retained})
        return {identifier_key: identifier, **{key: retained[key] for key in ("path", "bytes", "sha256")}}

    def snapshot(self, label, runtime=True):
        snapshot_id = "oracle-" + hashlib.sha256((self.q["request_id"] + label).encode()).hexdigest()[:48]
        value = self.state_call("snapshot" if runtime else "snapshot_database", snapshot_id=snapshot_id)
        reference = self.export(value["private_artifact"], "snapshot_id", snapshot_id)
        inspected = self.modules["state"].inspect_external(reference, self.c["state_binding_canonical_sha256"], self.c["state_scope"])
        need(inspected["summary"]["run_id"] == self.c["run_id"], "snapshot_run_changed")
        return {"reference": reference, "guest_reference": value["private_artifact"], "inspection": inspected}

    def mutate(self, mutation_id):
        value = self.state_call("mutate", mutation_id=mutation_id)
        ack = self.export(value["private_artifact"], "mutation_id", mutation_id)
        snapshots = {}
        for reference in value["private_snapshots"]:
            identifier = Path(reference["path"]).name.removesuffix(".sqlite")
            snapshots[identifier] = self.export(reference, "snapshot_id", identifier)
        before, after = snapshots[value["before_snapshot"]], snapshots[value["after_snapshot"]]
        inspected = self.modules["state"].inspect_ack_external(ack, self.c["state_binding_canonical_sha256"],
                                                               before, after, self.c["state_scope"])
        return {"reference": ack, "guest_reference": value["private_artifact"], "before": before, "after": after,
                "inspection": inspected}

    def workload(self, operation):
        request = {"version": 1, "request_id": self.native_id("workload", operation), "op": operation, "scenario_id": self.case_id}
        reply = self.transport.call("workload", request, timeout=min(self.remaining(), 120))
        self.record("workload-" + operation, reply)
        need(reply.get("ok") is True, "workload_adapter_refused")
        native = reply["result"]["private_artifact"]
        external = self.export(native, "proof_id", request["request_id"])
        facts = decode(read_file(external["path"], external["sha256"]))
        need(facts["binding_sha256"] == self.plan["workload_binding"]["sha256"]
             and facts["scenario_id"] == self.case_id and
             all(facts[key] == self.c[key] for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier")),
             "workload_artifact_scope_mismatch")
        return facts, external

    def media_proof(self, value):
        need(value["source_sha256"] == value["http_media_sha256"] and SHA.fullmatch(value["source_sha256"])
             and value["source_frame_sha256"] == value["response_frame_sha256"] and value["decoded_frames"] >= 1,
             "actual_source_bound_media_proof_missing")
        need(len(value["children"]) == 4, "actual_decode_child_population")
        for child in value["children"]:
            need(type(child["pid"]) is int and int(child["start_ticks"]) > 0 and child["exit_code"] == 0
                 and child["process_group_closed"] is True and child["completed_unix_ns"] >= child["started_unix_ns"]
                 and SHA.fullmatch(child["stdout_sha256"]), "actual_decode_child_not_closed")

    def healthy(self, earliest):
        rows = self.probe("healthy_probes", {"earliest_unix_ns": earliest, "roots": self.manifest["healthy_roots"]})
        for row in rows:
            need(row["client_timed_out"] is False and row["client_disconnected"] is False, "healthy_request_incomplete")
            if row["operation"] == "playback":
                self.media_proof(row["media_decode"])
            if row["operation"] == "scan":
                need(row["scan_admission"], "actual_healthy_scan_admission_missing")
        projected = [{key: row[key] for key in HEALTHY_FIELDS} for row in rows]
        self.modules["controller"].healthy_probes(projected, self.manifest, earliest)
        return projected

    def identity(self):
        raw = self.guest("observe")
        if "ack" in self.saved and (raw["goby"].get("pid", 0) == 0 or raw["postgres"].get("pid", 0) == 0):
            raise Pending("owned_services_still_starting")
        need(all(row.get("suspended") is False for row in raw["volume_observations"].values()), "identity_contains_suspended_volume")
        self.saved.setdefault("before_guest", raw)
        self.saved["latest_guest_identity"] = raw
        value = {key: raw[key] for key in IDENTITY_FIELDS}
        value["volume_observations"] = {name: {key: row[key] for key in VOLUME_FIELDS} for name, row in raw["volume_observations"].items()}
        self.modules["controller"].validate_identity(value, self.manifest)
        return value

    def resources(self, snapshot, guest=None, native=None, postgres=None):
        guest = guest or self.guest("observe")
        native = native or self.probe("runtime_resources", {})
        postgres = postgres or self.guest("observe_postgres")
        runtime = snapshot["inspection"]["runtime_observations"]
        need(runtime is not None, "complete_runtime_snapshot_required")
        cache = runtime["cache"]
        spool = guest["spool_inventory"]
        scan = native["resources"]["ScanEvidence"]
        children = []
        for row in runtime["processes"]:
            if row.get("is_media_worker"):
                need(row.get("attached_to_application") is True, "orphan_media_worker")
                children.append({"pid": int(row["pid"]), "start_ticks": int(row["start_ticks"]),
                                 "cgroup": self.manifest["guest"]["goby_cgroup"], "executable_sha256": row["executable_sha256"]})
        need(spool is not None, "actual_spool_inventory_required")
        value = {"goby_children": children, "temporary_bytes": sum(row["bytes"] for row in spool["generations"]) + cache["temporary_bytes"],
                 "temporary_inodes": sum(1 + len(row["entries"]) for row in spool["generations"]) + cache["temporary_inodes"], "open_fds": len(guest["fd_inventory"]),
                 "owner_backends": len(postgres["backends"]),
                 "spool_generations": [digest(row) for row in spool["generations"]],
                 "partial_publications": cache["counts"]["unsafe"] + cache["counts"]["missing_referenced"]}
        self.modules["controller"].validate_resources(value)
        return value, {"guest": guest, "native": native, "postgres": postgres, "scan": scan, "cache": cache}

    def jobs(self, snapshot):
        result = []
        keys = {"media.intro_analysis": "intro_analysis", "media.preview_generation": "preview"}
        for row in snapshot["inspection"]["active_jobs"]:
            kind = "scan" if row["table"] == "scan_jobs" else keys.get(row["task_key"]) if row["table"] == "task_runs" else None
            if kind is None:
                continue
            result.append({"id_sha256": digest([row["table"], row["id"]]), "kind": kind,
                           "status": "running" if row["state"].lower() in {"running", "stopping"} else "queued"})
        return result

    def compare(self, before, after, acknowledgement_records, expectation):
        # The transport verifies and fsyncs the exact preserved originals on
        # return. Independent external comparison still reads our own copies.
        for snapshot in (before, after):
            self.transport.return_artifact(snapshot["guest_reference"], snapshot["reference"])
        for ack in acknowledgement_records:
            self.transport.return_artifact(ack["guest_reference"], ack["reference"])
        scope = dict(self.c["state_scope"])
        if expectation == "explicit-rebind":
            scope["fault_root_ids"] = [self.plan["root_id"]]
        value = self.modules["state"].compare_external(before["reference"], after["reference"],
            [ack["reference"] for ack in acknowledgement_records], self.c["state_binding_canonical_sha256"],
            scope, expectation)
        self.record("independent-materialized-state-comparison", value)
        need(value["passed"] is True and value["failure_count"] == 0 and value["failures"] == [],
             "materialized_durable_state_changed")
        return value

    def acknowledge(self):
        need("ack" not in self.saved and "workload_start" not in self.saved, "acknowledgement_already_attempted")
        start, reference = self.workload("start")
        self.saved["workload_start"] = {"data": start, "reference": reference}
        workload_manifest = decode(read_file(self.c["workload_manifest"]["path"], self.c["workload_manifest"]["sha256"]))
        while True:
            facts, proof = self.workload("checkpoint")
            if facts.get("pending"):
                time.sleep(min(.25, self.remaining()))
                continue
            need(not facts.get("terminated_by_guest_restart") and "terminal" not in facts, "workload_ended_before_fault")
            try:
                checkpoint = workload_checkpoint(facts, self.modules["workload"], workload_manifest, proof["sha256"])
                need(facts["lanes"].get("editing", {}).get("completed") is True,
                     "workload_metadata_edits_not_settled_before_ack")
                break
            except Refusal:
                need(facts.get("worker_alive") and not facts.get("phase_errors"), "workload_not_fault_ready")
                time.sleep(min(.25, self.remaining()))
        acknowledgements, native_acks = [], []
        need(set(self.plan["ack_mutations"]) == {"metadata", "user_state", "playback_progress", "settings"},
             "acknowledgement_population")
        for expected_kind, mutation_id in self.plan["ack_mutations"].items():
            ack = self.mutate(mutation_id)
            observed = ack["inspection"]
            need(ACK_KIND.get(observed["kind"]) == expected_kind, "acknowledgement_kind_changed")
            raw = self.modules["state"].decode(read_file(ack["reference"]["path"], ack["reference"]["sha256"]))
            expected = [image["expected_fields"] for image in raw["postimages"]]
            actual = [{key: image["row"][key] for key in image["expected_fields"]} for image in raw["postimages"]]
            writes = [row for row in observed["http_responses"] if row["method"] != "GET"]
            need(writes, "acknowledgement_http_write_missing")
            acknowledgements.append({"kind": expected_kind, "request_id": mutation_id,
                "http_status": writes[-1]["status"], "response_sha256": writes[-1]["body_sha256"],
                "expected_sha256": self.modules["state"].digest(expected),
                "observed_sha256": self.modules["state"].digest(actual),
                "completed_unix_ns": int(observed["completed_unix_ns"])})
            native_acks.append(ack)
        snapshot = self.snapshot("acknowledged")
        overlays = {}
        for ack in native_acks:
            native = self.modules["state"].decode(read_file(ack["reference"]["path"], ack["reference"]["sha256"]))
            for row in native["postimages"]:
                key = (row["table"], self.modules["state"].canonical(row["key"]).decode())
                if key in overlays:
                    need(overlays[key] == row["before_row"], "initial_ack_chain_discontinuous")
                overlays[key] = row["row"]
        for (table, key), expected in overlays.items():
            keys = self.modules["state"].TABLES[table][2]
            actual = [row for row in self.rows(snapshot, table) if
                      self.modules["state"].canonical([row[field] for field in keys]).decode() == key]
            need(actual == [expected], "initial_ack_not_present_at_fault_checkpoint")
        self.saved["baseline"] = snapshot
        self.saved["native_acks"] = native_acks
        self.saved["rebind_acks"] = []
        current, current_proof = self.workload("checkpoint")
        need(current.get("worker_alive") and not current.get("phase_errors"), "workload_not_active_at_ack")
        checkpoint = workload_checkpoint(current, self.modules["workload"], workload_manifest, current_proof["sha256"])
        resources, raw_resources = self.resources(snapshot)
        self.saved["ack_resources"] = raw_resources
        transaction = snapshot["inspection"]["summary"]["transaction"]
        result = {"acknowledged_state": snapshot["inspection"]["controller_state"],
            "acknowledgements": acknowledgements,
            "snapshot": {**{key: snapshot["reference"][key] for key in ("path", "sha256", "bytes")}, "fsynced": True},
            "checkpoint": checkpoint, "active_jobs": self.jobs(snapshot), "resources": resources,
            "snapshot_transaction": {"isolation": transaction["isolation"], "read_only": transaction["read_only"],
                "database_identity_sha256": self.modules["state"].digest(transaction["database_identity"]),
                "synchronous_commit": transaction["synchronous_commit"], "fsync": transaction["fsync"],
                "full_page_writes": transaction["full_page_writes"]},
            "observed_unix_ns": raw_resources["guest"]["observed_unix_ns"],
            "native_state_acks": [row["reference"] for row in native_acks], "native_snapshot": snapshot["reference"],
            "dispatches": 1}
        self.modules["controller"].validate_ack(result, self.manifest, self.scenario)
        self.saved["ack"] = result
        return result

    def inject(self):
        need("ack" in self.saved and "injection" not in self.saved, "fault_before_durable_ack_or_repeated")
        self.saved["before_guest"] = self.guest("observe")
        if self.scenario["fault"] == "postgres_lock_wait":
            self.saved["pg_before"] = self.guest("observe_postgres")
            self.saved["pg_log_cursor"] = self.guest("observe_postgres_log", {"cursor": None})["next_cursor"]
        value = self.guest("inject", self.q["payload"])
        self.saved["injection"] = value
        return value

    def affected(self):
        if "affected" in self.saved:
            return self.saved["affected"]
        need("affected_intent" not in self.saved, "affected_operation_disposition_unknown")
        fault = self.scenario["fault"]
        kind = "read" if fault in {"blocked_read", "mount_loss", "permission_failure"} else "metadata"
        if fault == "enospc":
            kind = "preview"
        if fault == "postgres_lock_wait":
            kind = "lock_metadata"
        self.saved["affected_intent"] = {"kind": kind, "request_id": self.q["request_id"]}
        self.persist()
        value = self.probe("affected_probe", {"root_id": self.plan["root_id"], "kind": kind,
                                               "timeout_seconds": self.plan["caller_timeout_seconds"]})
        self.saved["affected"] = value
        return value

    def postgres_fault(self, injection):
        self.saved["pg_before_fault_snapshot"] = self.snapshot("pg-before-statement", runtime=False)
        waiting = None
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
            future = executor.submit(self.affected)
            while self.remaining() > 1:
                sample = self.guest("observe_postgres")
                blockers = {row["pid"] for row in sample["backends"]
                            if row["application_name"] == "goby-phase3-fault-controller"}
                waits = [row for row in sample["backends"] if row["wait_event_type"] == "Lock"
                         and set(row["blocking_pids"]) & blockers]
                if waits:
                    need(len(waits) == 1, "postgres_waiter_ambiguous")
                    waiting = sample
                    injection = {**injection, "blocker_backend_pids": sorted(blockers)}
                    break
                if future.done() and future.exception() is not None:
                    future.result()
                time.sleep(min(.1, self.remaining()))
            affected = future.result(timeout=self.remaining())
        need(waiting is not None, "actual_postgres_lock_wait_not_observed")
        while self.remaining() > 1:
            sample = self.guest("observe_postgres")
            owner = self.saved["pg_before"]["owner_backend"]
            matching = [row for row in sample["backends"] if row["pid"] == owner["pid"] and
                        row["backend_start"] == owner["backend_start"] and row["state"] == "idle" and row["xact_start"] is None]
            if matching:
                break
            time.sleep(min(.1, self.remaining()))
        log = self.guest("observe_postgres_log", {"cursor": self.saved["pg_log_cursor"]})
        raw = base64.b64decode(log["raw_base64"], validate=True)
        need(len(raw) == log["raw_bytes"] and hashlib.sha256(raw).hexdigest() == log["raw_sha256"], "postgres_raw_log_hash")
        records = [decode(line) for line in raw.splitlines() if line]
        after = self.snapshot("pg-after-statement", runtime=False)
        facts = self.modules["evidence"].postgres_lock_facts(self.saved["pg_before"], waiting, sample, records,
            self.saved["pg_before_fault_snapshot"]["inspection"]["controller_state"], after["inspection"]["controller_state"], injection)
        self.saved["pg_wait_evidence"] = {"waiting": waiting, "after": sample, "log": log, "after_snapshot": after}
        return facts, affected, after

    def fault_observation(self):
        fault = self.scenario["fault"]
        injection = self.q["payload"]["injection"]["data"]
        self.saved.setdefault("injection", injection)
        need(digest(self.q["payload"]["ack"]) == digest(self.saved["ack"]), "acknowledgement_projection_changed")
        if fault == "postgres_lock_wait":
            facts, affected, snapshot = self.postgres_fault(injection)
            facts["injected_unix_ns"] = injection["injected_unix_ns"]
            mechanism = "owned_row_lock"
            guest = self.guest("observe")
        else:
            affected = self.affected() if fault in self.modules["controller"].STORAGE else None
            guest = self.guest("observe")
            facts = dict(injection)
            mechanism = facts.pop("mechanism", "pve_owned_guest_reset" if fault == "guest_reset" else None)
            if fault.startswith("blocked_"):
                timed_out = affected["http"]["client_timed_out"] is True
                bounded = not timed_out and affected["http"]["http_status"] == 503
                need(timed_out != bounded and affected["http"]["method"] == "GET", "actual_http_caller_return_missing")
                first = guest
                time.sleep(min(1.05, self.remaining()))
                guest = self.guest("observe")
                actual_request = {"outcome": "timeout" if timed_out else "bounded_503", "http_method": "GET",
                    "http_status": affected["http"]["http_status"], "http_started_unix_ns": affected["http"]["started_unix_ns"],
                    "http_completed_unix_ns": affected["http"]["completed_unix_ns"],
                    "request_sha256": digest(affected["http"]), "source_path": affected["expected_source_identity"]["path"]}
                witness, descriptor = self.modules["evidence"].blocked_witness(first, guest, actual_request,
                    self.manifest["volumes"][self.scenario["volume_id"]], self.manifest["guest"]["goby_cgroup"],
                    "read" if fault == "blocked_read" else "metadata")
                observed_volume = guest["volume_observations"][self.scenario["volume_id"]]
                need(observed_volume["suspended"] is True, "actual_device_not_suspended")
                self.saved["blocked_descriptor"] = descriptor
                self.saved["blocked_samples"] = [first, guest]
                self.saved["blocked_resources"] = self.probe("runtime_resources", {})
                facts.update(blocked_task=witness, caller_timed_out=timed_out, bounded_http_return=bounded,
                             http_method="GET", http_status=affected["http"]["http_status"], task_alive_after_caller_return=True,
                             suspended=observed_volume["suspended"], dm_uuid=observed_volume["dm_uuid"])
            elif fault == "mount_loss":
                point = self.manifest["volumes"][self.scenario["volume_id"]]["mountpoint"]
                mounted = any(row["mountpoint"] == point for row in guest["mounts"])
                need(not mounted and affected["http"]["http_status"] not in (200, 206), "mount_loss_product_read_not_denied")
                facts.update(mounted=mounted, affected_read_success=False)
            elif fault in self.modules["controller"].REPLACEMENTS:
                binding = affected["admission_or_binding"]["Binding"]
                target = injection["target"]
                old = [row for row in self.saved["before_guest"]["mounts"] if row["mountpoint"] == target]
                new = [row for row in guest["mounts"] if row["mountpoint"] == target]
                need(len(new) == 1 and (not old or old[-1]["mount_id"] != new[0]["mount_id"])
                     and binding["Status"] == "mismatch" and binding["ApprovedFingerprint"] != binding["ObservedFingerprint"],
                     "replacement_not_observed_and_fenced")
                self.saved["replacement_observation"] = affected
                facts.update(before_mount_id=old[-1]["mount_id"] if old else 0, after_mount_id=new[0]["mount_id"],
                    replacement_visible=True, binding_status="changed", explicit_rebind_required=True, automatic_rebind=False,
                    denied_request_sha256=digest(affected["http"]))
            elif fault in {"permission_failure", "enospc"}:
                if "errno_probe" not in self.saved:
                    need("errno_probe_intent" not in self.saved, "errno_probe_disposition_unknown")
                    self.saved["errno_probe_intent"] = self.q["request_id"]
                    self.persist()
                    self.saved["errno_probe"] = self.guest("observe_failure_errno", {})
                native_failure = self.saved["errno_probe"]
                expected = "EACCES" if fault == "permission_failure" else "ENOSPC"
                need(native_failure["errno"] == expected and native_failure["probe_uid"] == self.manifest["guest"]["goby_uid"],
                     "actual_unprivileged_errno_missing")
                facts.update(errno=native_failure["errno"], probe_uid=native_failure["probe_uid"])
                if fault == "enospc":
                    facts["product_write_failed"] = self.product_task_failed(affected)
                else:
                    need(affected["http"]["http_status"] not in (200, 206), "permission_product_read_not_denied")
            elif fault == "postgres_disconnect":
                pg = self.guest("observe_postgres")
                old = injection["pid"]
                exists = any(row["pid"] == old and row["backend_start"] == injection["backend_start"] for row in pg["backends"])
                ready = self.probe("readiness", {})
                need(not exists and ready["ready_status"] == 503, "database_disconnect_not_observed")
                facts.update(backend_pid=old, backend_start=injection["backend_start"], old_backend_exists=exists,
                             ready_status=ready["ready_status"])
            elif fault in self.modules["controller"].RESTARTS:
                if fault in self.modules["controller"].REBOOTS:
                    if guest["boot_id"] == self.saved["before_guest"]["boot_id"]:
                        raise Pending("guest_boot_not_yet_observed")
                else:
                    role = "postgres" if fault == "postgres_restart" else "goby"
                    old = self.saved["before_guest"][role]
                    current = guest[role]
                    need(current.get("pid", 0) == 0 or (current["pid"], current["start_ticks"]) != (old["pid"], old["start_ticks"]),
                         "target_process_termination_not_observed")
            snapshot = self.snapshot("during-fault", runtime=False) if fault in self.modules["controller"].STORAGE else None
        state = snapshot["inspection"]["controller_state"] if snapshot else None
        healthy = self.healthy(facts["injected_unix_ns"]) if fault in self.modules["controller"].STORAGE else []
        result = {"fault": fault, "mechanism": mechanism, "observed_unix_ns": guest["observed_unix_ns"],
                  "facts": facts, "state": state, "healthy_probes": healthy}
        if fault in self.modules["controller"].STORAGE or fault == "postgres_lock_wait":
            result["dispatches"] = 1
        self.modules["controller"].validate_fault_observation(result, self.scenario, self.manifest, self.saved["ack"])
        self.saved["fault_observation"] = result
        if snapshot:
            self.saved["fault_snapshot"] = snapshot
        return result

    def rows(self, snapshot, table):
        module = self.modules["state"]
        need(table in module.TABLES, "unapproved_snapshot_table")
        reference = snapshot["reference"]
        name = reference["snapshot_id"] + ".sqlite"
        observer = module.external_observer(self.c["state_binding_canonical_sha256"], self.c["state_scope"], {name: reference})
        connection, _ = observer.open_snapshot(reference["snapshot_id"])
        try:
            return [module.decode(row[0]) for row in connection.execute(
                "SELECT payload FROM rows WHERE table_name=? ORDER BY row_key", (table,))]
        finally:
            connection.close()

    def product_task_failed(self, affected):
        admission = affected["admission_or_binding"]
        need(affected["kind"] == "preview" and isinstance(admission, dict) and admission.get("RunId"),
             "actual_preview_admission_required")
        while True:
            snapshot = self.snapshot("enospc-task-" + str(self.serial), runtime=False)
            tasks = [row for row in self.rows(snapshot, "task_runs") if row["id"] == admission["RunId"]]
            children = [row for row in self.rows(snapshot, "task_run_children") if row["run_id"] == admission["RunId"]]
            need(len(tasks) == 1 and tasks[0]["task_key"] == "media.preview_generation", "preview_task_binding")
            if tasks[0]["state"] not in {"pending", "running", "stopping"}:
                failed = [row for row in children if row["state"] == "failed" and row.get("error_code")]
                need(tasks[0]["state"] == "failed" and failed, "actual_product_preview_failure_missing")
                admitted = [row for row in self.rows(snapshot, "analysis_work_sources") if
                            row["child_id"] in {child["id"] for child in failed}]
                need(admitted and {row["item_id"] for row in admitted} == {affected["operation_item_id"]},
                     "failed_preview_source_admission_mismatch")
                self.saved["product_failure"] = {"snapshot": snapshot, "task": tasks[0], "children": children,
                                                   "errno_probe": self.saved["errno_probe"]}
                return True
            time.sleep(min(.25, self.remaining()))

    def adopt_ack(self, value, mutation_id):
        ack = self.export(value["private_artifact"], "mutation_id", mutation_id)
        snapshots = {}
        for reference in value["private_snapshots"]:
            identifier = Path(reference["path"]).name.removesuffix(".sqlite")
            snapshots[identifier] = self.export(reference, "snapshot_id", identifier)
        before, after = snapshots[value["before_snapshot"]], snapshots[value["after_snapshot"]]
        inspected = self.modules["state"].inspect_ack_external(ack, self.c["state_binding_canonical_sha256"],
                                                               before, after, self.c["state_scope"])
        return {"reference": ack, "guest_reference": value["private_artifact"], "before": before, "after": after,
                "inspection": inspected}

    def write_proof(self, ack, stage=None):
        module = self.modules["state"]
        scope = dict(self.c["state_scope"])
        if stage is not None:
            scope["fault_root_ids"] = [self.plan["root_id"]]
        comparison = module.compare_external(ack["before"], ack["after"], [ack["reference"]],
            self.c["state_binding_canonical_sha256"], scope, "explicit-rebind" if stage is not None else "preserve")
        need(comparison["passed"] is True and comparison["failure_count"] == 0, "write_comparison_failed")
        after = module.inspect_external(ack["after"], self.c["state_binding_canonical_sha256"], scope)
        native = module.decode(read_file(ack["reference"]["path"], ack["reference"]["sha256"]))
        value = {"expected_state": after["controller_state"], "native_ack": native,
                 "external_ack": {key: ack["reference"][key] for key in ("path", "sha256", "bytes")} | {"fsynced": True},
                 "before_snapshot": ack["before"], "after_snapshot": ack["after"],
                 "comparison": comparison, "observed_unix_ns": int(native["completed_unix_ns"])}
        if stage is not None:
            value.update(dispatches=1, stage=stage)
        self.record("actual-write-proof", value)
        return value

    def rebind(self, stage):
        need(self.scenario["fault"] in self.modules["controller"].REPLACEMENTS and "settled" in self.saved,
             "rebind_before_replacement_fence_or_settle")
        key = "rebind_" + stage
        need(key not in self.saved, "rebind_already_completed")
        observation_id = self.native_id("state", "observe_rebind")
        observation = self.state_call("observe_rebind", observation_id=observation_id,
                                       stage=stage, root_id=self.plan["root_id"])
        retained = self.export(observation["private_artifact"], "observation_id", observation_id)
        self.saved[key + "_observation"] = {"guest": observation["private_artifact"], "external": retained}
        self.persist()
        mutation_id = self.plan["rebind_mutations"][stage]
        value = self.state_call("rebind", mutation_id=mutation_id, observation=observation["private_artifact"])
        ack = self.adopt_ack(value, mutation_id)
        proof = self.write_proof(ack, stage)
        self.saved["rebind_acks"].append(ack)
        self.saved[key] = proof
        return proof

    def settle(self):
        need("settled" not in self.saved, "scenario_settle_repeated")
        facts, reference = self.workload("settle")
        # A guest restart is recorded as termination, never a successful join.
        # Its actual old product jobs and FDs must still pass the DB/resource
        # checks below before the recovery can pass.
        terminal = facts.get("terminal", facts)
        need(terminal.get("settled") is True or facts.get("terminated_by_guest_restart") is True,
             "workload_not_settled")
        self.saved["settled"] = {"facts": facts, "reference": reference}
        return {"dispatches": 1, "workload_artifact": reference, "observed_unix_ns": time.time_ns()}

    def recover(self):
        need("recovered" not in self.saved, "recovery_repeated")
        # Native guest recover consumes its own concrete injection data.
        value = self.guest("recover", {"injection": self.saved["injection"]})
        self.saved["recovered"] = value
        return {**value, "dispatches": 1}

    def late_mount(self):
        guest = self.guest("observe")
        volume = self.manifest["volumes"][self.scenario["volume_id"]]
        mounted = any(row["mountpoint"] == volume["mountpoint"] for row in guest["mounts"])
        need(not mounted, "late_mount_already_present")
        ready = self.probe("readiness", {})
        affected = self.probe("affected_probe", {"root_id": self.plan["root_id"], "kind": "read",
                                                "timeout_seconds": self.plan["caller_timeout_seconds"]})
        need(affected["http"]["http_status"] not in (200, 206), "late_mount_source_read_succeeded")
        snapshot = self.snapshot("late-mount", runtime=False)
        return {"boot_id": guest["boot_id"], "mount_present": mounted, "ready_status": ready["ready_status"],
                "state": snapshot["inspection"]["controller_state"], "healthy_probes": self.healthy(guest["observed_unix_ns"]),
                "affected_read_success": False, "observed_unix_ns": guest["observed_unix_ns"], "dispatches": 1}

    def settled_resources(self, snapshot):
        value, raw = self.resources(snapshot)
        scan = raw["native"]["resources"]["ScanEvidence"]
        resources = raw["native"]["resources"]
        need(not value["goby_children"] and value["temporary_bytes"] == 0 and value["partial_publications"] == 0
             and not value["spool_generations"], "product_derivatives_not_settled")
        need(all(scan[key] == 0 for key in ("ActivePasses", "RetiringPasses", "CleanupFailures", "ReservedBytes", "ReservedFileDescriptors"))
             and resources["StorageObservations"]["Active"] == 0
             and resources["OriginalStreams"]["ActiveCount"] == 0
             and resources["OriginalStreams"]["CurrentCapacityDropped"] == 0, "native_resources_not_settled")
        need(not any(row["application_name"] == "goby-phase3-fault-controller" for row in raw["postgres"]["backends"]),
             "owned_fault_backend_remains")
        return value, raw

    def interrupted(self, after, comparison):
        if not self.scenario["require_interrupted_jobs"]:
            return []
        transitions = {(row["table"], row["id"]): row for row in comparison["task_transitions"]}
        result = []
        for row in self.saved["baseline"]["inspection"]["active_jobs"]:
            identifier = digest([row["table"], row["id"]])
            if identifier not in {job["id_sha256"] for job in self.saved["ack"]["active_jobs"]}:
                continue
            change = transitions[(row["table"], row["id"])]
            if row["table"] == "task_runs":
                before_rows = [item for item in self.rows(self.saved["baseline"], "task_runs") if item["id"] == row["id"]]
                old = before_rows[0]
                if old.get("request_id") is not None:
                    matches = [item for item in self.rows(after, "task_runs") if
                               (item["task_id"], item.get("request_id")) == (old["task_id"], old["request_id"])]
                    need(len(matches) == 1 and matches[0]["id"] == old["id"], "duplicate_original_admission")
            result.append({"id_sha256": identifier, "status": change["after"].lower(),
                           "duplicate_execution": False, "partial_published": False})
        return result

    def retirement(self, raw):
        if not self.scenario["fault"].startswith("blocked_"):
            return []
        witness = self.saved["fault_observation"]["facts"]["blocked_task"]
        descriptor, affected = self.saved["blocked_descriptor"], self.saved["affected"]
        before_resources = self.saved["blocked_resources"]["resources"]
        after_resources = raw["native"]["resources"]
        completion = {"evidence_fd": descriptor, "source_path": affected["expected_source_identity"]["path"],
            "completed_unix_ns": raw["native"]["http"]["completed_unix_ns"], "terminal_state": "completed",
            "source_admission_id": affected["http"]["request_id"], "operation_id": affected["http"]["request_id"],
            "temporary_source_handles": [], "spool_generation_handles": [], "owner_backend_references": []}
        if self.scenario["fault"] == "blocked_read":
            lease = self.modules["evidence"].original_lease_completion(before_resources, after_resources,
                affected["item_id"], affected["media_source_id"], affected["http"]["started_unix_ns"],
                affected["http"]["completed_unix_ns"])
            completion.update(kind="native_original_leave", lease=lease, operation_id=lease["LeaseId"],
                              completed_unix_ns=int(lease["CompletedUnixNano"]))
        else:
            need(before_resources["StorageObservations"]["Active"] > 0
                 and after_resources["StorageObservations"]["Active"] == 0, "actual_storage_observations_not_drained")
            completion.update(kind="native_storage_observations_drained", before=before_resources, after=after_resources,
                              admissions_stopped=self.saved["settled"]["reference"])
        proof = self.record("concrete-blocked-completion", {"completion": completion, "raw_resources": raw})
        completion["evidence_sha256"] = proof["sha256"]
        final_guest = self.guest("observe")
        retired = self.modules["evidence"].retired_worker(witness, descriptor, final_guest, completion,
                                                        self.saved["before_guest"], self.saved["blocked_samples"])
        return [retired]

    def recovery_observation(self):
        need("settled" in self.saved and "resume_intent" not in self.saved, "recovery_resume_disposition_unknown")
        expected = self.q["payload"]["expected_state"]
        healthy = self.healthy(self.q["payload"]["after"]["observed_unix_ns"])
        affected_rows = self.probe("healthy_probes", {"earliest_unix_ns": self.q["payload"]["after"]["observed_unix_ns"],
                                                     "roots": [self.plan["root_id"]]})
        need({row["operation"] for row in affected_rows} == {"search", "playback", "scan"} and
             all(row["root_id"] == self.plan["root_id"] and 200 <= row["http_status"] < 300
                 and not row["client_timed_out"] and not row["client_disconnected"] for row in affected_rows),
             "affected_root_service_not_recovered")
        for row in affected_rows:
            if row["operation"] == "playback":
                self.media_proof(row["media_decode"])
        self.saved["affected_recovery"] = affected_rows
        while True:
            snapshot = self.snapshot("recovered-" + str(self.serial))
            try:
                need(not snapshot["inspection"]["active_jobs"], "product_jobs_not_settled")
                resources, raw = self.settled_resources(snapshot)
                break
            except Refusal:
                time.sleep(min(.25, self.remaining()))
        expectation = "interrupted" if self.scenario["require_interrupted_jobs"] else "preserve"
        if self.scenario["fault"] in self.modules["controller"].REPLACEMENTS:
            expectation = "explicit-rebind"
        comparison = self.compare(self.saved["baseline"], snapshot, self.saved["rebind_acks"], expectation)
        need(snapshot["inspection"]["controller_state"] == expected, "recovered_durable_state_mismatch")
        roots = {row["Id"]: row for row in snapshot["inspection"]["runtime_observations"]["roots"]}
        need(roots[self.plan["root_id"]]["Status"] == "verified", "affected_root_unavailable")
        retired = self.retirement(raw)
        original = True
        if self.scenario["fault"] in self.modules["controller"].STORAGE or self.scenario["late_mount"]:
            original = self.modules["evidence"].original_mount_restored(self.saved["before_guest"], raw["guest"], self.scenario["volume_id"])
        resume_plan = self.plan["resume"]
        progress = [row for row in snapshot["inspection"]["private_progress_rows"] if
                    row["user_id"] == resume_plan["user_id"] and row["item_id"] == resume_plan["item_id"]]
        need(len(progress) == 1 and progress[0]["playback_position_ticks"] > 0, "durable_resume_position_missing")
        self.saved["resume_intent"] = {"snapshot": snapshot, "position_ticks": progress[0]["playback_position_ticks"]}
        self.persist()
        playback = self.probe("playback_resume", {"persisted_ticks": progress[0]["playback_position_ticks"],
            "snapshot_sha256": snapshot["reference"]["sha256"], "item_id_ref": resume_plan["item_id_ref"]})
        self.media_proof(playback["media_decode"])
        retained = self.export(playback["private_artifact"], "proof_id", self.native_id("probe", "resume_receipt"))
        after_resume = self.snapshot("after-resume")
        ack = self.adopt_ack(self.state_call("ack_probe_progress", mutation_id=resume_plan["mutation_id"],
            before_snapshot_id=snapshot["reference"]["snapshot_id"], after_snapshot_id=after_resume["reference"]["snapshot_id"],
            probe_receipt=playback["private_artifact"]), resume_plan["mutation_id"])
        post_resume = self.write_proof(ack)
        result = {"state": snapshot["inspection"]["controller_state"], "healthy_probes": healthy,
            "ready_status": snapshot["inspection"]["runtime_observations"]["ready_status"], "affected_root_available": True,
            "interrupted_jobs": self.interrupted(snapshot, comparison), "resources": resources,
            "closure": {"retired_workers": retired, "orphan_children": [], "orphan_temporary_bytes": 0,
                "orphan_spool_generations": [], "partial_publications": 0, "owned_backends_released": True},
            "playback_resume": {key: playback[key] for key in RESUME_FIELDS}, "post_resume": post_resume,
            "original_storage_restored": original, "observed_unix_ns": post_resume["observed_unix_ns"], "dispatches": 1}
        if self.scenario["fault"] in self.modules["controller"].REPLACEMENTS:
            result["rebind_chain"] = self.q["payload"]["rebind_chain"]
        self.saved["recovery"] = result
        self.saved["resume_ack"] = ack
        self.saved["resume_receipt"] = retained
        return result

    def close(self):
        need("recovery" in self.saved, "close_before_recovery")
        facts, reference = self.workload("close")
        self.saved["close_workload"] = {"facts": facts, "reference": reference}
        value = self.guest("close", {})
        return {"dispatches": 1, "workload_artifact": reference, "native_close": value}

    def closure(self):
        while True:
            facts, reference = self.workload("checkpoint")
            if ("unit_state" in facts and int(facts["unit_state"]["MainPID"]) == 0
                    and facts["unit_state"]["cgroup_pids"] == []
                    and facts["unit_state"]["ActiveState"] in {"inactive", "failed"}):
                break
            time.sleep(min(.25, self.remaining()))
        snapshot = self.snapshot("closed-" + str(self.serial))
        resources, raw = self.settled_resources(snapshot)
        need(not snapshot["inspection"]["active_jobs"], "product_tasks_remain_at_close")
        observation = raw["guest"]["scenario_closure"]
        if observation["lock_unit"] is not None:
            lock = observation["lock_unit"]
            need(lock["main_pid"] == 0 and lock["cgroup_pids"] == [] and lock["active_state"] in {"inactive", "failed"},
                 "owned_fault_lock_process_remains")
        if observation["filler"] is not None:
            need(observation["filler"]["exists"] is False, "owned_fault_filler_remains")
        need(all(row.get("suspended") is False for row in raw["guest"]["volume_observations"].values()), "fault_device_still_suspended")
        for volume in self.manifest["volumes"]:
            self.modules["evidence"].original_mount_restored(self.saved["before_guest"], raw["guest"], volume)
        geometry = ("major_minor", "root", "mountpoint", "filesystem", "source")
        previous_mounts = [{key: row[key] for key in geometry}
                           for row in self.saved["before_guest"]["scenario_closure"]["mounts_at_target"]]
        current_mounts = [{key: row[key] for key in geometry} for row in observation["mounts_at_target"]]
        need(current_mounts == previous_mounts, "owned_target_mount_geometry_not_restored")
        if self.scenario["fault"] == "permission_failure":
            current, previous = observation["target"], self.saved["injection"]
            need((current["device"], current["inode"], current["mode"]) ==
                 (previous["before_device"], previous["before_inode"], previous["before_mode"]), "original_mode_not_restored")
        self.record("scenario-closure-inputs", {"workload": reference, "resources": raw})
        return {"owned_processes_remaining": [], "owned_faults_remaining": [], "owned_locks_remaining": [],
                "unclosed_temporary_bytes": resources["temporary_bytes"], "original_modes_restored": True,
                "original_mounts_restored": True}

    def dispatch(self):
        operation = self.q["operation"]
        routes = {"guest_identity": self.identity, "acknowledge": self.acknowledge, "inject": self.inject,
                  "fault_observation": self.fault_observation, "late_mount_observation": self.late_mount,
                  "recover": self.recover, "settle_scenario": self.settle, "recovery_observation": self.recovery_observation,
                  "close_scenario": self.close, "closure_observation": self.closure,
                  "rebind_replacement": lambda: self.rebind("replacement"), "rebind_original": lambda: self.rebind("original"),
                  "arm_late_mount": lambda: {**self.guest("arm_late_mount", self.q["payload"]), "dispatches": 1}}
        need(operation in routes, "unsupported_oracle_operation")
        writes = (operation in MUTATIONS or operation in {"recovery_observation", "late_mount_observation"} or
                  operation == "fault_observation" and
                  (self.scenario["fault"] in self.modules["controller"].STORAGE or self.scenario["fault"] == "postgres_lock_wait"))
        if writes:
            intents = self.saved.setdefault("mutation_intents", {})
            need(operation not in intents, "mutation_already_attempted_no_automatic_retry")
            intents[operation] = {"request_id": self.q["request_id"], "observed_unix_ns": time.time_ns()}
            self.persist()
        return routes[operation]()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context", required=True)
    args = parser.parse_args()
    reply = {"schema_version": 1, "request_id": "invalid", "operation": "invalid", "owner_id": "invalid",
             "vmid": 106, "status": "failed", "data": {}}
    oracle, lock_fd = None, None
    try:
        raw = sys.stdin.buffer.read(MAX_JSON + 1)
        need(len(raw) <= MAX_JSON, "request_budget")
        request = decode(raw)
        required = {"schema_version", "request_id", "operation", "run_id", "owner_id", "source_revision", "tier", "profile_id",
                    "guest", "volumes", "scenario", "payload", "artifacts_root", "context_sha256"}
        need(set(request) == required and request["schema_version"] == 1 and SAFE.fullmatch(request["request_id"]), "request_shape")
        reply.update({key: request[key] for key in ("request_id", "operation", "owner_id")})
        context = decode(read_file(args.context, request["context_sha256"]))
        root = Path(request["artifacts_root"])
        need(root.resolve(strict=True) == root and root == Path(context["transport"]["artifacts_root"]), "lock_root_scope")
        lock_fd = os.open(root / ".oracle.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        oracle = Oracle(context, request)
        result = oracle.dispatch()
        oracle.persist()
        reply.update(status="ok", data=result)
    except BaseException as error:
        restart_read = (oracle is not None and request["operation"] in {"guest_identity", "fault_observation"}
                        and oracle.scenario["fault"] in oracle.modules["controller"].RESTARTS)
        pending = isinstance(error, Pending) or restart_read and str(error) in {
            "ssh_remote_operation_failed", "ssh_observation_timeout_no_retry"}
        if pending:
            reply["status"] = "pending"
        reply["data"] = {"error_code": str(error) if re.fullmatch(r"[a-z0-9_]{1,128}", str(error)) else type(error).__name__,
                         "automatic_retry": False, "resource_closure_established": False}
        if pending:
            reply["data"]["read_only_observation_pending"] = True
        if oracle is not None:
            try:
                oracle.record("oracle-refusal", reply)
                oracle.persist()
            except BaseException:
                reply["data"]["failure_record_persistence_failed"] = True
    finally:
        if lock_fd is not None:
            os.close(lock_fd)
    sys.stdout.buffer.write(canonical(reply) + b"\n")
    return 0 if reply["status"] in {"ok", "pending"} else 1


if __name__ == "__main__":
    raise SystemExit(main())
