#!/usr/bin/env python3
"""External, fail-closed controller for the Phase 3 owned-guest fault matrix.

Source delivery is not an execution or acceptance record. This controller never
accepts arbitrary shell commands. Pinned adapters implement the finite RPC
contract in the companion document; the infrastructure owner supplies them.
It must run outside the tested guest. An explicitly owned PVE task directory is
an admitted controller location; faults still target only the declared guest.
"""

from __future__ import annotations

import argparse
from contextlib import contextmanager
import decimal
import fcntl
import hashlib
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import selectors
import signal
import stat
import subprocess
import sys
import time
import types


VERSION = 1
MAX_JSON = 8 << 20
MAX_EVENT = 16 << 20
MAX_ADAPTER_BYTES = 2 << 20
MAX_EVENTS = 2048
MAX_SCENARIOS = 1
# This finite provenance-inventory ceiling can describe an already authorized
# guest disk. It grants no resize permission: exact owner, volume, UUID and
# observed size must still match. Fault-volume and resource budgets are separate.
MAX_GUEST_DISK_INVENTORY_BYTES = 128 << 30
SHA = re.compile(r"[0-9a-f]{64}\Z")
REVISION = re.compile(r"[0-9a-f]{40}\Z")
ID = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}\Z")
UUID = re.compile(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}\Z")
MACHINE = re.compile(r"[0-9a-f]{32}\Z")
FAULTS = frozenset({"blocked_read", "blocked_metadata", "mount_loss",
                    "changed_root_mount", "changed_nested_mount", "permission_failure",
                    "enospc", "postgres_disconnect", "postgres_lock_wait",
                    "process_crash", "postgres_restart", "guest_reboot", "guest_reset"})
STORAGE = frozenset({"blocked_read", "blocked_metadata", "mount_loss",
                     "changed_root_mount", "changed_nested_mount", "permission_failure", "enospc"})
MUTATING_FAULT_OBSERVATIONS = STORAGE | {"postgres_lock_wait"}
REBOOTS = frozenset({"guest_reboot", "guest_reset"})
RESTARTS = REBOOTS | {"process_crash", "postgres_restart"}
REPLACEMENTS = frozenset({"changed_root_mount", "changed_nested_mount"})
LANES = frozenset({"scan", "search", "playback", "intro_analysis", "preview"})
STATE_HASHES = ("catalog_identity_sha256", "root_binding_sha256", "user_state_sha256",
                "playback_progress_sha256", "settings_sha256")
ACK_KINDS = frozenset({"metadata", "user_state", "playback_progress", "settings"})
CANCELLED = False


class Invalid(RuntimeError):
    """An invalid input or a failed acceptance assertion, without private data."""


class ObservationTimeout(Invalid):
    """An unknown remote disposition, never permission to repeat a mutation."""


def need(condition, code):
    if not condition:
        raise Invalid(code)


def fields(value, required, optional=()):
    need(isinstance(value, dict), "expected_object")
    need(set(required) <= value.keys() <= set(required) | set(optional), "unexpected_fields")
    return value


def integer(value, low=0, high=(1 << 63) - 1):
    need(type(value) is int and low <= value <= high, "invalid_integer")
    return value


def number(value, low=0, high=3600):
    need(type(value) in (int, float, decimal.Decimal) and math.isfinite(value) and low <= value <= high,
         "invalid_number")
    return value


def text(value, maximum=4096):
    need(isinstance(value, str) and 0 < len(value) <= maximum and
         not any(ord(char) < 32 for char in value), "invalid_text")
    return value


def identifier(value):
    need(isinstance(value, str) and ID.fullmatch(value), "invalid_identifier")
    return value


def digest(value):
    need(isinstance(value, str) and SHA.fullmatch(value), "invalid_digest")
    return value


def canonical(value):
    if isinstance(value, dict):
        return b"{" + b",".join(json.dumps(key, ensure_ascii=True).encode("ascii") + b":" + canonical(value[key])
                                  for key in sorted(value)) + b"}"
    if isinstance(value, (list, tuple)):
        return b"[" + b",".join(canonical(item) for item in value) + b"]"
    if isinstance(value, decimal.Decimal):
        need(value.is_finite() and -1000 <= value.adjusted() <= 1000 and len(value.as_tuple().digits) <= 10000,
             "decimal_serialization_bound")
        return format(value, "f").encode("ascii")
    return json.dumps(value, sort_keys=True, separators=(",", ":"),
                      ensure_ascii=True, allow_nan=False).encode("ascii")


def pairs_no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        need(key not in result, "duplicate_json_key")
        result[key] = value
    return result


def decode(raw, preserve_decimals=False):
    need(len(raw) <= MAX_JSON, "json_size_limit")
    try:
        return json.loads(raw, object_pairs_hook=pairs_no_duplicates,
                          parse_float=decimal.Decimal if preserve_decimals else float,
                          parse_constant=lambda _: (_ for _ in ()).throw(Invalid("nonfinite_json")))
    except (ValueError, UnicodeError, RecursionError) as error:
        raise Invalid("invalid_json") from error


def posix_path(value):
    raw = text(value)
    path = PurePosixPath(raw)
    need(path.is_absolute() and str(path) == raw and ".." not in path.parts and
         path != PurePosixPath("/"), "noncanonical_absolute_path")
    return path


def beneath(value, parent):
    path, root = posix_path(value), posix_path(parent)
    need(path != root and root in path.parents, "path_outside_owned_root")
    return path


def local_path(value, private=True, directory=False):
    path = Path(str(posix_path(value)))
    need(path.resolve(strict=True) == path, "symlinked_path")
    info = path.lstat()
    need((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)), "wrong_file_type")
    need(info.st_uid == os.geteuid() and (directory or info.st_nlink == 1), "wrong_file_owner")
    if private:
        need(stat.S_IMODE(info.st_mode) & 0o077 == 0, "private_file_permissions")
    return path


def read_pinned(path, expected=None, maximum=MAX_JSON, private=True):
    target = local_path(path, private=private)
    fd = os.open(target, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        need(before.st_size <= maximum, "file_size_limit")
        raw = bytearray()
        while len(raw) < before.st_size:
            part = os.read(fd, min(1 << 20, before.st_size - len(raw)))
            need(bool(part), "file_truncated")
            raw.extend(part)
        need(not os.read(fd, 1), "file_grew")
        after = os.fstat(fd)
        identity = lambda item: (item.st_dev, item.st_ino, item.st_size, item.st_mtime_ns,
                                 item.st_ctime_ns, item.st_mode, item.st_uid, item.st_nlink)
        need(identity(before) == identity(after) == identity(target.lstat()), "file_changed")
        result = bytes(raw)
        sha = hashlib.sha256(result).hexdigest()
        need(expected is None or digest(expected) == sha, "file_digest_mismatch")
        return result, sha
    finally:
        os.close(fd)


def read_ref(value, root):
    fields(value, ("path", "sha256"))
    beneath(value["path"], str(root))
    return read_pinned(value["path"], digest(value["sha256"]))


def durable_artifact(reference, root):
    """Rehash and fsync the complete external SQLite snapshot without loading it."""
    fields(reference, ("path", "sha256", "bytes", "fsynced"))
    beneath(reference["path"], root)
    path = local_path(reference["path"])
    expected_bytes = integer(reference["bytes"], 1, 1 << 30)
    digest(reference["sha256"])
    fd = os.open(path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        need(before.st_size == expected_bytes, "external_snapshot_size_changed")
        sha, total = hashlib.sha256(), 0
        deadline = time.monotonic() + 120
        while total < expected_bytes:
            check_cancelled()
            need(time.monotonic() < deadline, "external_snapshot_hash_deadline")
            part = os.read(fd, min(1 << 20, expected_bytes - total))
            need(bool(part), "external_snapshot_truncated")
            total += len(part)
            sha.update(part)
        need(not os.read(fd, 1) and sha.hexdigest() == reference["sha256"], "external_snapshot_digest_changed")
        os.fsync(fd)
        after = os.fstat(fd)
        identity = lambda item: (item.st_dev, item.st_ino, item.st_size, item.st_mtime_ns, item.st_ctime_ns)
        need(identity(before) == identity(after) == identity(path.lstat()), "external_snapshot_replaced")
    finally:
        os.close(fd)
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    return {**reference, "fsynced": True}


def cancellation(_signum, _frame):
    global CANCELLED
    CANCELLED = True


def check_cancelled():
    need(not CANCELLED, "controller_cancelled")


def load_manifest(value):
    fields(value, ("schema_version", "run_id", "source_revision", "profile_id", "tier",
                   "owner_id", "controller", "guest", "adapters", "workload_manifest",
                   "volumes", "healthy_roots", "budgets", "scenarios", "state_validator"))
    need(value["schema_version"] == VERSION, "unsupported_schema")
    for name in ("run_id", "profile_id", "owner_id"):
        identifier(value[name])
    need(REVISION.fullmatch(value["source_revision"]), "invalid_source_revision")
    need(value["tier"] in (10000, 100000), "unsupported_catalog_tier")
    controller = fields(value["controller"], ("machine_id", "artifacts_root", "marker"))
    need(MACHINE.fullmatch(controller["machine_id"]), "invalid_controller_machine_id")
    posix_path(controller["artifacts_root"])
    fields(controller["marker"], ("path", "sha256"))
    posix_path(controller["marker"]["path"])
    digest(controller["marker"]["sha256"])
    guest = fields(value["guest"], ("vmid", "name", "machine_id", "smbios_uuid", "owner_marker_sha256",
                                    "owned_root", "disks", "goby_uid", "goby_cgroup", "postgres_cgroup"))
    need(guest["vmid"] == 106 and guest["name"].startswith("goby-phase3-"), "guest_not_owned_scope")
    need(MACHINE.fullmatch(guest["machine_id"]) and guest["machine_id"] != controller["machine_id"],
         "controller_is_tested_guest")
    need(UUID.fullmatch(guest["smbios_uuid"]), "invalid_smbios_uuid")
    digest(guest["owner_marker_sha256"])
    root = str(posix_path(guest["owned_root"]))
    need(root.startswith("/var/lib/goby-phase3/") or root.startswith("/opt/goby-phase3/"),
         "unsafe_owned_root")
    integer(guest["goby_uid"], 1, 65534)
    for key in ("goby_cgroup", "postgres_cgroup"):
        path = posix_path(guest[key])
        need("goby-phase3-" in str(path) and str(path).endswith(".service"), "unowned_service_cgroup")
    need(1 <= len(guest["disks"]) <= 8 and isinstance(guest["disks"], dict), "invalid_disk_inventory")
    for key, disk in guest["disks"].items():
        identifier(key)
        fields(disk, ("volume", "uuid", "size_bytes"))
        need(re.fullmatch(r"local-lvm:vm-106-(?:disk-[0-9]+|cloudinit)", disk["volume"]), "unowned_disk")
        text(disk["uuid"], 128)
        integer(disk["size_bytes"], 1, MAX_GUEST_DISK_INVENTORY_BYTES)
    fields(value["adapters"], ("executor", "observer", "workload", "hypervisor"))
    for adapter in value["adapters"].values():
        fields(adapter, ("path", "sha256", "context"))
        posix_path(adapter["path"])
        digest(adapter["sha256"])
        fields(adapter["context"], ("path", "sha256"))
        posix_path(adapter["context"]["path"])
        digest(adapter["context"]["sha256"])
    fields(value["workload_manifest"], ("path", "sha256"))
    digest(value["workload_manifest"]["sha256"])
    posix_path(value["workload_manifest"]["path"])
    validator = fields(value["state_validator"], ("source", "binding_sha256", "scope"))
    fields(validator["source"], ("path", "sha256"))
    posix_path(validator["source"]["path"])
    digest(validator["source"]["sha256"])
    digest(validator["binding_sha256"])
    scope = fields(validator["scope"], ("user_ids", "item_ids", "library_ids", "root_ids", "fault_root_ids"))
    for key in scope:
        need(isinstance(scope[key], list) and 1 <= len(scope[key]) <= 256 and len(set(scope[key])) == len(scope[key]),
             "invalid_state_validator_scope")
        for item in scope[key]:
            identifier(item)
    need(set(scope["fault_root_ids"]) <= set(scope["root_ids"]), "fault_root_scope_mismatch")
    volumes = value["volumes"]
    need(isinstance(volumes, dict) and 2 <= len(volumes) <= 8, "invalid_fault_volume_count")
    for key, volume in volumes.items():
        identifier(key)
        fields(volume, ("mountpoint", "backing_file", "loop_device", "mapper_name", "dm_uuid",
                         "filesystem_uuid", "major_minor", "size_bytes", "writable", "purpose"))
        beneath(volume["mountpoint"], root)
        beneath(volume["backing_file"], root)
        need(re.fullmatch(r"/dev/loop[0-9]{1,3}", volume["loop_device"]), "invalid_loop_device")
        need(re.fullmatch(r"goby-phase3-[a-z0-9-]{1,64}", volume["mapper_name"]), "invalid_mapper")
        need(volume["dm_uuid"].startswith("GOBY-PHASE3-" + value["owner_id"] + "-"), "unowned_dm_uuid")
        text(volume["filesystem_uuid"], 128)
        need(re.fullmatch(r"[0-9]{1,5}:[0-9]{1,5}", volume["major_minor"]), "invalid_device_number")
        integer(volume["size_bytes"], 16 << 20, 2 << 30)
        need(type(volume["writable"]) is bool and volume["purpose"] in {"media", "derivatives", "replacement"},
             "invalid_volume_role")
    need(len({v["backing_file"] for v in volumes.values()}) == len(volumes) and
         len({v["loop_device"] for v in volumes.values()}) == len(volumes) and
         len({v["mapper_name"] for v in volumes.values()}) == len(volumes), "aliased_fault_volumes")
    healthy = value["healthy_roots"]
    need(isinstance(healthy, list) and 1 <= len(healthy) <= 8 and len(set(healthy)) == len(healthy),
         "invalid_healthy_roots")
    for root_id in healthy:
        identifier(root_id)
    budget = fields(value["budgets"], ("rpc_seconds", "recovery_seconds", "fault_seconds", "poll_seconds",
                                        "max_output_bytes", "healthy_latency_ms", "cleanup_seconds"))
    number(budget["rpc_seconds"], 1, 120)
    number(budget["recovery_seconds"], 10, 900)
    number(budget["fault_seconds"], 5, 120)
    number(budget["poll_seconds"], 0.1, 10)
    number(budget["cleanup_seconds"], 5, 120)
    integer(budget["max_output_bytes"], 4096, MAX_JSON)
    number(budget["healthy_latency_ms"], 1, 60000)
    scenarios = value["scenarios"]
    need(isinstance(scenarios, list) and 1 <= len(scenarios) <= MAX_SCENARIOS, "invalid_scenario_count")
    seen = set()
    for scenario in scenarios:
        fields(scenario, ("scenario_id", "fault", "volume_id", "replacement_volume_id", "relative_path",
                           "late_mount", "restart_goby_after", "require_interrupted_jobs"))
        identifier(scenario["scenario_id"])
        need(scenario["scenario_id"] not in seen, "duplicate_scenario")
        seen.add(scenario["scenario_id"])
        fault = scenario["fault"]
        need(fault in FAULTS, "unsupported_fault")
        need(type(scenario["late_mount"]) is bool and type(scenario["restart_goby_after"]) is bool and
             type(scenario["require_interrupted_jobs"]) is bool, "invalid_scenario_boolean")
        need(not scenario["late_mount"] or fault in REBOOTS, "late_mount_requires_guest_restart")
        need(fault not in RESTARTS or scenario["require_interrupted_jobs"], "restart_requires_interrupted_jobs")
        requires_volume = fault in STORAGE or scenario["late_mount"]
        need((scenario["volume_id"] in volumes) if requires_volume else scenario["volume_id"] is None,
             "invalid_scenario_volume")
        if fault in REPLACEMENTS:
            need(scenario["replacement_volume_id"] in volumes and
                 scenario["replacement_volume_id"] != scenario["volume_id"], "invalid_replacement_volume")
        else:
            need(scenario["replacement_volume_id"] is None, "unexpected_replacement_volume")
        relative = scenario["relative_path"]
        need(isinstance(relative, str) and len(relative) <= 512 and "\x00" not in relative and
             not relative.startswith("/") and ".." not in PurePosixPath(relative).parts,
             "unsafe_relative_path")
        if fault == "changed_nested_mount":
            need(relative not in ("", "."), "nested_mount_requires_descendant")
        if fault == "enospc":
            volume = volumes[scenario["volume_id"]]
            need(volume["writable"] and volume["purpose"] == "derivatives", "enospc_requires_derivative_volume")
        if fault == "permission_failure":
            need(volumes[scenario["volume_id"]]["writable"], "permission_fault_requires_writable_volume")
    return value


def process_identity(value):
    fields(value, ("pid", "start_ticks", "cgroup", "executable_sha256"))
    integer(value["pid"], 2, 1 << 30)
    integer(value["start_ticks"], 1)
    posix_path(value["cgroup"])
    digest(value["executable_sha256"])
    return (value["pid"], value["start_ticks"])


def validate_identity(facts, manifest):
    fields(facts, ("owner_id", "vmid", "name", "machine_id", "smbios_uuid", "owner_marker_sha256",
                   "boot_id", "btime", "goby", "postgres", "disks", "volumes", "volume_observations", "observed_unix_ns"))
    guest = manifest["guest"]
    for key in ("vmid", "name", "machine_id", "smbios_uuid", "owner_marker_sha256", "disks"):
        need(facts[key] == guest[key], "guest_identity_mismatch")
    need(facts["owner_id"] == manifest["owner_id"], "guest_owner_mismatch")
    need(UUID.fullmatch(facts["boot_id"]), "invalid_boot_id")
    integer(facts["btime"], 1)
    integer(facts["observed_unix_ns"], 1)
    for name in ("goby", "postgres"):
        process_identity(facts[name])
        need(facts[name]["cgroup"] == guest[name + "_cgroup"], "service_cgroup_changed")
    need(facts["volumes"] == manifest["volumes"], "fault_volume_identity_changed")
    need(set(facts["volume_observations"]) == set(manifest["volumes"]), "volume_observation_missing")
    for name, observed in facts["volume_observations"].items():
        fields(observed, ("present", "mounted", "dm_uuid", "major_minor", "loop_backing_file", "size_bytes"))
        configured = manifest["volumes"][name]
        need(observed["present"] is True and observed["mounted"] is True and
             observed["loop_backing_file"] == configured["backing_file"] and
             all(observed[key] == configured[key] for key in ("dm_uuid", "major_minor", "size_bytes")),
             "actual_fault_volume_identity_mismatch")
    return facts


def validate_hypervisor(facts, manifest):
    fields(facts, ("owner_id", "vmid", "smbios_uuid", "status", "qemu_pid", "qemu_start_ticks",
                   "config_sha256", "disks", "observed_unix_ns", "observer_machine_id"))
    need(facts["owner_id"] == manifest["owner_id"] and facts["vmid"] == 106 and
         facts["smbios_uuid"] == manifest["guest"]["smbios_uuid"] and
         facts["disks"] == manifest["guest"]["disks"], "hypervisor_identity_mismatch")
    need(facts["status"] == "running", "guest_not_running")
    integer(facts["qemu_pid"], 2)
    integer(facts["qemu_start_ticks"], 1)
    digest(facts["config_sha256"])
    integer(facts["observed_unix_ns"], 1)
    need(MACHINE.fullmatch(facts["observer_machine_id"]) and
         facts["observer_machine_id"] != manifest["guest"]["machine_id"], "hypervisor_observer_is_guest")
    return facts


def validate_state(value):
    fields(value, ("catalog_count", *STATE_HASHES))
    integer(value["catalog_count"], 1, 262144)
    for name in STATE_HASHES:
        digest(value[name])
    return value


def assert_preserved(before, after):
    validate_state(before)
    validate_state(after)
    need(after == before, "acknowledged_state_changed")


def validate_checkpoint(value, manifest):
    fields(value, ("run_id", "owner_id", "profile_id", "source_revision", "tier", "sha256",
                   "active_lanes", "observed_unix_ns", "overlap_windows"))
    for key in ("run_id", "owner_id", "profile_id", "source_revision", "tier"):
        need(value[key] == manifest[key], "workload_checkpoint_scope_mismatch")
    digest(value["sha256"])
    need(isinstance(value["active_lanes"], list) and set(value["active_lanes"]) >= LANES and
         len(value["active_lanes"]) <= 16, "compound_overlap_missing")
    # Intro and preview share one production worker slot. Each needs real
    # overlap with the foreground lanes; simultaneous analysis workers would
    # contradict the admitted capacity and is not an acceptance requirement.
    windows = value["overlap_windows"]
    need(isinstance(windows, list) and 2 <= len(windows) <= 64, "overlap_window_budget")
    observed = integer(value["observed_unix_ns"], 1)
    found = set()
    for window in windows:
        fields(window, ("analysis_lane", "lanes", "start_unix_ns", "end_unix_ns", "process_source_fd_sha256"))
        need(window["analysis_lane"] in {"intro_analysis", "preview"} and
             set(window["lanes"]) >= {"scan", "search", "playback", window["analysis_lane"]}, "analysis_overlap_missing")
        start = integer(window["start_unix_ns"], 1)
        integer(window["end_unix_ns"], start + 1, observed)
        digest(window["process_source_fd_sha256"])
        found.add(window["analysis_lane"])
    need(found == {"intro_analysis", "preview"}, "analysis_overlap_population_missing")


def validate_ack(value, manifest, scenario):
    fields(value, ("acknowledged_state", "acknowledgements", "snapshot", "checkpoint", "active_jobs",
                   "resources", "snapshot_transaction", "observed_unix_ns"), ("native_state_acks", "native_snapshot", "dispatches"))
    need(value.get("dispatches", 1) == 1, "ack_dispatch_count_unknown")
    validate_state(value["acknowledged_state"])
    need(value["acknowledged_state"]["catalog_count"] >= manifest["tier"], "catalog_tier_not_established")
    validate_checkpoint(value["checkpoint"], manifest)
    snapshot = fields(value["snapshot"], ("path", "sha256", "bytes", "fsynced"))
    posix_path(snapshot["path"])
    digest(snapshot["sha256"])
    integer(snapshot["bytes"], 1, 1 << 30)
    need(snapshot["fsynced"] is True, "external_snapshot_not_durable")
    transaction = fields(value["snapshot_transaction"], ("isolation", "read_only", "database_identity_sha256",
                                                           "synchronous_commit", "fsync", "full_page_writes"))
    need(transaction["isolation"] == "repeatable read" and transaction["read_only"] is True,
         "snapshot_not_consistent")
    need(transaction["synchronous_commit"] == "on" and transaction["fsync"] is True and
         transaction["full_page_writes"] is True, "durability_configuration_not_admitted")
    digest(transaction["database_identity_sha256"])
    acks = value["acknowledgements"]
    need(isinstance(acks, list) and 4 <= len(acks) <= 32, "missing_durable_acknowledgements")
    found = set()
    for ack in acks:
        fields(ack, ("kind", "request_id", "http_status", "response_sha256", "expected_sha256",
                     "observed_sha256", "completed_unix_ns"))
        need(ack["kind"] in ACK_KINDS, "unknown_ack_kind")
        found.add(ack["kind"])
        identifier(ack["request_id"])
        integer(ack["http_status"], 200, 299)
        digest(ack["response_sha256"])
        need(digest(ack["expected_sha256"]) == digest(ack["observed_sha256"]), "ack_not_read_back")
        need(integer(ack["completed_unix_ns"], 1) <= value["observed_unix_ns"], "ack_after_snapshot")
    need(found == ACK_KINDS, "acknowledged_state_kinds_missing")
    jobs = value["active_jobs"]
    need(isinstance(jobs, list) and len(jobs) <= 64, "job_budget")
    kinds = set()
    ids = set()
    for job in jobs:
        fields(job, ("id_sha256", "kind", "status"))
        need(digest(job["id_sha256"]) not in ids, "duplicate_active_job")
        ids.add(job["id_sha256"])
        need(job["kind"] in {"scan", "intro_analysis", "preview", "transcode"} and
             job["status"] in {"running", "queued"}, "job_not_admitted")
        kinds.add(job["kind"])
    if scenario["require_interrupted_jobs"]:
        need({"scan", "intro_analysis", "preview"} <= kinds, "interruption_population_missing")
        need(any(job["kind"] in {"intro_analysis", "preview"} and job["status"] == "running" for job in jobs),
             "no_analysis_job_running_at_fault")
    validate_resources(value["resources"])
    integer(value["observed_unix_ns"], 1)


def validate_resources(value):
    fields(value, ("goby_children", "temporary_bytes", "temporary_inodes", "open_fds", "owner_backends",
                   "spool_generations", "partial_publications"))
    for name in ("temporary_bytes", "temporary_inodes", "open_fds", "owner_backends", "partial_publications"):
        integer(value[name])
    need(isinstance(value["goby_children"], list) and len(value["goby_children"]) <= 128, "child_budget")
    for child in value["goby_children"]:
        process_identity(child)
    need(isinstance(value["spool_generations"], list) and len(value["spool_generations"]) <= 64,
         "spool_generation_budget")
    for generation in value["spool_generations"]:
        digest(generation)


def healthy_probes(value, manifest, earliest):
    need(isinstance(value, list) and len(value) <= 128, "healthy_probe_budget")
    found = set()
    for probe in value:
        fields(probe, ("root_id", "operation", "http_status", "duration_ms", "started_unix_ns",
                       "completed_unix_ns", "response_sha256", "bytes_received"))
        need(probe["root_id"] in manifest["healthy_roots"], "unexpected_healthy_root")
        need(probe["operation"] in {"search", "playback", "scan"}, "unexpected_healthy_operation")
        integer(probe["http_status"], 200, 299)
        number(probe["duration_ms"], 0, manifest["budgets"]["healthy_latency_ms"])
        integer(probe["started_unix_ns"], earliest)
        integer(probe["completed_unix_ns"], probe["started_unix_ns"])
        digest(probe["response_sha256"])
        integer(probe["bytes_received"], 1 if probe["operation"] == "playback" else 0)
        found.add((probe["root_id"], probe["operation"]))
    need(found >= {(root, op) for root in manifest["healthy_roots"] for op in ("search", "playback", "scan")},
         "healthy_root_service_missing")


def validate_fault_observation(value, scenario, manifest, before):
    fields(value, ("fault", "mechanism", "observed_unix_ns", "facts", "state", "healthy_probes"), ("dispatches",))
    fault = scenario["fault"]
    need(value["fault"] == fault, "fault_observation_scope_mismatch")
    need(fault not in MUTATING_FAULT_OBSERVATIONS or value.get("dispatches") == 1,
         "fault_observation_dispatch_count_unknown")
    integer(value["observed_unix_ns"], 1)
    facts = value["facts"]
    need(isinstance(facts, dict), "missing_fault_facts")
    if fault in STORAGE:
        assert_preserved(before["acknowledged_state"], value["state"])
        healthy_probes(value["healthy_probes"], manifest, facts["injected_unix_ns"])
    if fault.startswith("blocked_"):
        need(value["mechanism"] == "dm_suspend" and facts.get("suspended") is True,
             "io_not_suspended")
        need(facts.get("dm_uuid") == manifest["volumes"][scenario["volume_id"]]["dm_uuid"], "wrong_suspended_device")
        witness = fields(facts.get("blocked_task"), ("pid", "start_ticks", "tid", "task_start_ticks",
                                                    "state", "syscall", "wchan", "operation", "cgroup",
                                                    "request_sha256", "first_seen_unix_ns", "last_seen_unix_ns"))
        integer(witness["pid"], 2)
        integer(witness["start_ticks"], 1)
        integer(witness["tid"], 2)
        integer(witness["task_start_ticks"], 1)
        need(witness["state"] == "D" and witness["cgroup"] == manifest["guest"]["goby_cgroup"],
             "blocked_goby_task_not_observed")
        need(witness["operation"] == ("read" if fault == "blocked_read" else "metadata"),
             "wrong_blocked_operation")
        text(witness["syscall"], 512)
        text(witness["wchan"], 128)
        digest(witness["request_sha256"])
        need(integer(witness["last_seen_unix_ns"], 1) - integer(witness["first_seen_unix_ns"], 1) >= 1_000_000_000,
             "blocked_task_not_sustained")
        timed_out, bounded_return = facts.get("caller_timed_out"), facts.get("bounded_http_return")
        need(type(timed_out) is bool and type(bounded_return) is bool and timed_out != bounded_return and
             facts.get("http_method") == "GET" and (not bounded_return or facts.get("http_status") == 503) and
             facts.get("task_alive_after_caller_return") is True, "caller_return_lifetime_not_observed")
    elif fault == "mount_loss":
        need(value["mechanism"] == "owned_unmount" and facts.get("mounted") is False and
             facts.get("affected_read_success") is False, "mount_loss_not_observed")
    elif fault in REPLACEMENTS:
        need(value["mechanism"] == "owned_bind_mount" and facts.get("before_mount_id") != facts.get("after_mount_id")
             and facts.get("replacement_visible") is True and facts.get("binding_status") == "changed"
             and facts.get("explicit_rebind_required") is True and facts.get("automatic_rebind") is False,
             "replacement_not_fenced")
        digest(facts.get("denied_request_sha256"))
    elif fault == "permission_failure":
        need(value["mechanism"] == "owned_chmod" and facts.get("errno") == "EACCES" and
             facts.get("probe_uid") == manifest["guest"]["goby_uid"], "permission_failure_not_observed")
    elif fault == "enospc":
        need(value["mechanism"] == "owned_volume_fill" and facts.get("errno") == "ENOSPC" and
             facts.get("probe_uid") == manifest["guest"]["goby_uid"], "enospc_not_observed")
        integer(facts.get("filler_bytes"), 1, manifest["volumes"][scenario["volume_id"]]["size_bytes"])
        need(facts.get("product_write_failed") is True, "enospc_product_path_missing")
    elif fault == "postgres_disconnect":
        need(value["mechanism"] == "terminate_owned_backend" and facts.get("terminated") is True
             and facts.get("old_backend_exists") is False and facts.get("ready_status") == 503,
             "postgres_disconnect_not_observed")
        integer(facts.get("backend_pid"), 2)
        text(facts.get("backend_start"), 128)
    elif fault == "postgres_lock_wait":
        need(value["mechanism"] == "owned_row_lock" and facts.get("wait_event_type") == "Lock" and
             facts.get("statement_sqlstate") == "57014" and facts.get("transaction_rolled_back") is True
             and facts.get("same_owner_backend_survived") is True, "postgres_lock_rollback_not_observed")
        digest(facts.get("before_rows_sha256"))
        need(facts.get("before_rows_sha256") == facts.get("after_rows_sha256"), "lock_timeout_partial_write")
    elif fault in RESTARTS:
        mechanisms = {"process_crash": "sigkill_owned_process", "postgres_restart": "restart_owned_postgres",
                      "guest_reboot": "guest_systemctl_reboot", "guest_reset": "pve_owned_guest_reset"}
        need(value["mechanism"] == mechanisms[fault] and facts.get("dispatches") == 1,
             "restart_kind_not_established")
        if fault == "guest_reset":
            need(facts.get("vmid") == 106 and facts.get("smbios_uuid") == manifest["guest"]["smbios_uuid"]
                 and facts.get("pve_operation") == "reset", "forced_reset_not_established")
            text(facts.get("pve_task_id"), 512)
    return value


def validate_recovery(value, manifest, scenario, ack, before, after, fault_facts, expected_state=None):
    fields(value, ("state", "healthy_probes", "ready_status", "affected_root_available", "interrupted_jobs",
                   "resources", "closure", "playback_resume", "post_resume", "original_storage_restored", "observed_unix_ns"),
           ("rebind_chain", "dispatches"))
    need(value.get("dispatches", 1) == 1, "recovery_dispatch_count_unknown")
    assert_preserved(expected_state or ack["acknowledged_state"], value["state"])
    need(scenario["fault"] in REPLACEMENTS or "rebind_chain" not in value, "unexpected_rebind_chain")
    need(value["ready_status"] == 200 and value["affected_root_available"] is True,
         "service_not_recovered")
    healthy_probes(value["healthy_probes"], manifest, after["observed_unix_ns"])
    if scenario["fault"] in STORAGE or scenario["late_mount"]:
        need(value["original_storage_restored"] is True, "original_storage_not_restored")
    fault = scenario["fault"]
    old_goby, new_goby = process_identity(before["goby"]), process_identity(after["goby"])
    old_pg, new_pg = process_identity(before["postgres"]), process_identity(after["postgres"])
    if fault in REBOOTS:
        need(before["boot_id"] != after["boot_id"] and after["btime"] >= before["btime"],
             "guest_os_boot_did_not_change")
        # PID and start_ticks are boot-relative and may legitimately repeat.
        # Their complete identity includes the independently observed boot ID.
        need((before["boot_id"], old_goby) != (after["boot_id"], new_goby) and
             (before["boot_id"], old_pg) != (after["boot_id"], new_pg), "guest_process_identity_not_changed")
    else:
        need(before["boot_id"] == after["boot_id"], "unexpected_guest_reboot")
        if fault == "process_crash" or scenario["restart_goby_after"]:
            need(old_goby != new_goby, "goby_process_did_not_change")
        if fault == "postgres_restart":
            need(old_pg != new_pg, "postgres_process_did_not_change")
        else:
            need(old_pg == new_pg, "unexpected_postgres_restart")
    jobs = value["interrupted_jobs"]
    need(isinstance(jobs, list) and len(jobs) <= 64, "interrupted_job_budget")
    if scenario["require_interrupted_jobs"]:
        expected = {job["id_sha256"] for job in ack["active_jobs"]}
        need({job.get("id_sha256") for job in jobs} == expected, "interrupted_job_population_changed")
        for job in jobs:
            fields(job, ("id_sha256", "status", "duplicate_execution", "partial_published"))
            need(job["status"] in {"interrupted", "failed", "cancelled", "completed", "rescheduled"}
                 and job["duplicate_execution"] is False and job["partial_published"] is False,
                 "invalid_interrupted_job_recovery")
    validate_resources(value["resources"])
    closure = fields(value["closure"], ("retired_workers", "orphan_children", "orphan_temporary_bytes",
                                        "orphan_spool_generations", "partial_publications", "owned_backends_released"))
    need(closure["orphan_children"] == [] and closure["orphan_spool_generations"] == [] and
         closure["orphan_temporary_bytes"] == 0 and closure["partial_publications"] == 0 and
         closure["owned_backends_released"] is True, "resource_closure_incomplete")
    if fault.startswith("blocked_"):
        witness = fault_facts["facts"]["blocked_task"]
        retired = closure["retired_workers"]
        need(isinstance(retired, list) and any(
            row.get("pid") == witness["pid"] and row.get("start_ticks") == witness["start_ticks"] and
            row.get("tid") == witness["tid"] and row.get("task_start_ticks") == witness["task_start_ticks"] and
            row.get("syscall_returned") is True and row.get("worker_released") is True and
            row.get("evidence_handles_closed") is True for row in retired), "blocked_worker_not_closed")
    resume = fields(value["playback_resume"], ("reconnected", "persisted_ticks", "requested_ticks",
                                               "observed_start_ticks", "tolerance_ticks", "http_status", "media_bytes"))
    need(resume["reconnected"] is True, "playback_not_reconnected")
    integer(resume["persisted_ticks"], 1)
    need(resume["requested_ticks"] == resume["persisted_ticks"], "resume_not_from_persisted_progress")
    tolerance = integer(resume["tolerance_ticks"], 0, 30_000_000)
    need(abs(integer(resume["observed_start_ticks"]) - resume["persisted_ticks"]) <= tolerance,
         "playback_resume_position_mismatch")
    integer(resume["http_status"], 200, 299)
    integer(resume["media_bytes"], 1)
    integer(value["observed_unix_ns"], after["observed_unix_ns"])


def validate_rebind_chain(first, second):
    previous = {(row["table"], canonical(row["key"])): row for row in first["native_ack"]["postimages"]}
    current = {(row["table"], canonical(row["key"])): row for row in second["native_ack"]["postimages"]}
    need(previous.keys() == current.keys() and all(current[key]["before_row"] == previous[key]["row"] for key in previous),
         "rebind_postimage_chain_discontinuous")
    for key, original in previous.items():
        if key[0] == "library_roots":
            need(current[key]["row"]["storage_binding"] == original["before_row"]["storage_binding"],
                 "explicit_original_rebind_changed_storage_identity")


def validate_resume_position(before_ticks, after_ticks, requested_ticks, acknowledged_ticks):
    need(integer(before_ticks, 1) == integer(after_ticks, 1) == integer(requested_ticks, 1) ==
         integer(acknowledged_ticks, 1), "resume_did_not_use_acknowledged_position")


def link_ack_references(acknowledgements, native_references):
    projected = {item["request_id"]: item for item in acknowledgements}
    native_ids = [reference["mutation_id"] for reference in native_references]
    need(len(projected) == len(acknowledgements) == len(native_ids) == len(set(native_ids)) and
         set(projected) == set(native_ids), "acknowledgement_reference_bijection")
    return projected


class Journal:
    """One immutable, fsynced event per file, with a hash chain and no overwrite."""

    def __init__(self, root):
        self.root = local_path(str(root), directory=True)
        self.sequence = 0
        self.previous = "0" * 64

    def write(self, kind, data):
        need(self.sequence < MAX_EVENTS, "journal_event_budget")
        identifier(kind)
        event = {"schema_version": VERSION, "sequence": self.sequence, "previous_sha256": self.previous,
                 "kind": kind, "controller_unix_ns": time.time_ns(), "controller_monotonic_ns": time.monotonic_ns(),
                 "data": data}
        raw = canonical(event) + b"\n"
        need(len(raw) <= MAX_EVENT, "journal_event_size_limit")
        path = self.root / (f"{self.sequence:04d}-" + kind + ".json")
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
        try:
            view = memoryview(raw)
            while view:
                written = os.write(fd, view)
                need(written > 0, "journal_short_write")
                view = view[written:]
            os.fsync(fd)
        finally:
            os.close(fd)
        directory = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        self.previous = hashlib.sha256(raw).hexdigest()
        self.sequence += 1
        return {"path": str(path), "sha256": self.previous}


def run_bounded(argv, request, timeout, maximum, preserve_external=False, started=None):
    """Bound local adapter output and lifetime; never infer remote disposition."""
    process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.PIPE, start_new_session=True,
                               env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "PYTHONDONTWRITEBYTECODE": "1"})
    output, diagnostics = bytearray(), bytearray()
    deadline = time.monotonic() + timeout
    error = None
    try:
        if started:
            started(process.pid)
        body = canonical(request) + b"\n"
        need(len(body) <= MAX_JSON, "rpc_request_budget")
        pending = memoryview(body)
        with selectors.DefaultSelector() as ready:
            for pipe in (process.stdin, process.stdout, process.stderr):
                os.set_blocking(pipe.fileno(), False)
            ready.register(process.stdin, selectors.EVENT_WRITE, "stdin")
            ready.register(process.stdout, selectors.EVENT_READ, "stdout")
            ready.register(process.stderr, selectors.EVENT_READ, "stderr")
            while ready.get_map():
                check_cancelled()
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise ObservationTimeout("adapter_observation_timeout")
                for key, _events in ready.select(min(remaining, 0.25)):
                    if key.data == "stdin":
                        written = os.write(key.fileobj.fileno(), pending)
                        pending = pending[written:]
                        if not pending:
                            ready.unregister(key.fileobj)
                            key.fileobj.close()
                        continue
                    chunk = os.read(key.fileobj.fileno(), 65536)
                    if not chunk:
                        ready.unregister(key.fileobj)
                        key.fileobj.close()
                    else:
                        destination = output if key.data == "stdout" else diagnostics
                        destination.extend(chunk)
                        need(len(output) + len(diagnostics) <= maximum, "adapter_output_budget")
            remaining = deadline - time.monotonic()
            need(remaining > 0, "adapter_deadline_expired")
            process.wait(timeout=remaining)
            need(process.returncode == 0, "adapter_failed")
            need(len(diagnostics) == 0, "adapter_unstructured_diagnostics")
    except BaseException as caught:
        error = caught
    finally:
        # Only the controller-owned adapter process group is affected. Killing
        # ssh or a broker does not imply that a guest operation was cancelled.
        if preserve_external and error is not None:
            # PVE may still own an accepted reset operation. Its adapter writes
            # an independent durable receipt; retain the controller-owned
            # group for read-only disposition and closure, never signal it.
            pass
        elif process.poll() is None or error is not None:
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            def group_exists():
                try:
                    os.killpg(process.pid, 0)
                    return True
                except ProcessLookupError:
                    return False
            grace = time.monotonic() + 2
            while group_exists() and time.monotonic() < grace:
                process.poll()
                time.sleep(0.05)
            if group_exists():
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            closure = time.monotonic() + 3
            while group_exists() and time.monotonic() < closure:
                process.poll()
                time.sleep(0.05)
            if group_exists():
                error = Invalid("adapter_process_closure_unproven")
            try:
                process.wait(timeout=max(0.01, closure - time.monotonic()))
            except subprocess.TimeoutExpired:
                error = Invalid("adapter_process_closure_unproven")
        for pipe in (process.stdin, process.stdout, process.stderr):
            if pipe and not pipe.closed:
                pipe.close()
    if error:
        raise error
    return decode(bytes(output), preserve_decimals=True)


class RPC:
    def __init__(self, manifest, journal):
        self.manifest, self.journal = manifest, journal
        self.counter = 0

    def call(self, role, operation, scenario, payload=None, mutation=False, timeout=None):
        check_cancelled()
        if operation == "hypervisor_identity":
            role = "hypervisor"
        if operation == "inject" and scenario["fault"] == "guest_reset":
            role, operation = "hypervisor", "forced_reset"
            payload = {"durable_ack": payload["durable_ack"]}
        adapter = self.manifest["adapters"][role]
        read_pinned(adapter["path"], adapter["sha256"], MAX_ADAPTER_BYTES, private=False)
        read_pinned(adapter["context"]["path"], adapter["context"]["sha256"])
        self.counter += 1
        request_id = self.manifest["run_id"] + ":" + str(self.counter)
        request = {"schema_version": VERSION, "request_id": request_id, "operation": operation,
                   "run_id": self.manifest["run_id"], "owner_id": self.manifest["owner_id"],
                   "source_revision": self.manifest["source_revision"], "tier": self.manifest["tier"],
                   "profile_id": self.manifest["profile_id"], "guest": self.manifest["guest"],
                   "volumes": self.manifest["volumes"], "scenario": scenario, "payload": payload or {},
                   "artifacts_root": self.manifest["controller"]["artifacts_root"],
                   "context_sha256": adapter["context"]["sha256"]}
        intent = self.journal.write("mutation-intent" if mutation else "observation-intent",
                                    {"role": role, "request": request, "adapter_sha256": adapter["sha256"]})
        try:
            def record_owner(pid):
                raw = (Path("/proc") / str(pid) / "stat").read_text()
                ticks = int(raw[raw.rfind(")") + 2:].split()[19])
                cgroup = (Path("/proc") / str(pid) / "cgroup").read_text().strip()
                self.journal.write("adapter-process-owner", {"request_id": request_id, "pid": pid,
                                   "start_ticks": ticks, "cgroup": cgroup,
                                   "preserve_on_observation_timeout": operation == "forced_reset"})
            result = run_bounded([sys.executable, "-I", "-B", adapter["path"], "--context", adapter["context"]["path"]],
                                 request, timeout or self.manifest["budgets"]["rpc_seconds"],
                                 self.manifest["budgets"]["max_output_bytes"], operation == "forced_reset", record_owner)
            fields(result, ("schema_version", "request_id", "operation", "owner_id", "vmid",
                            "status", "data"))
            need(result["schema_version"] == VERSION and result["request_id"] == request_id and
                 result["operation"] == operation and result["owner_id"] == self.manifest["owner_id"] and
                 result["vmid"] == 106 and result["status"] in {"ok", "pending", "failed"}, "rpc_receipt_mismatch")
            self.journal.write("rpc-receipt", {"intent": intent, "receipt": result})
            need(result["status"] != "failed", "adapter_reported_failure")
            if mutation:
                need(result["status"] == "ok", "mutation_disposition_unknown")
                need(isinstance(result["data"], dict) and result["data"].get("dispatches") == 1,
                     "mutation_dispatch_count_unknown")
            return result
        except BaseException as error:
            self.journal.write("rpc-incomplete", {"intent": intent, "error": type(error).__name__,
                                                   "automatic_retry": False, "remote_disposition": "unknown"})
            raise


class Controller:
    """Finite scenario state machine; an ambiguous mutation ends this run."""

    def __init__(self, manifest, manifest_sha256, journal, rpc=None):
        self.manifest, self.manifest_sha256, self.journal = manifest, manifest_sha256, journal
        self.rpc = rpc or RPC(manifest, journal)
        self.results = []
        self._state_module = None
        self._progress_key = None
        self._progress_ticks = None

    def state_module(self):
        configuration = self.manifest["state_validator"]
        reference = configuration["source"]
        source, _sha = read_pinned(reference["path"], reference["sha256"], MAX_ADAPTER_BYTES, private=False)
        if self._state_module is None:
            module = types.ModuleType("phase3_pinned_external_state_validator")
            module.__file__ = reference["path"]
            exec(compile(source, reference["path"], "exec"), module.__dict__)
            self._state_module = module
        return self._state_module

    def snapshot_reference(self, value):
        fields(value, ("snapshot_id", "path", "sha256", "bytes"), ("fsynced",))
        identifier(value["snapshot_id"])
        durable_artifact({key: value[key] for key in ("path", "sha256", "bytes")} | {"fsynced": True},
                         self.manifest["controller"]["artifacts_root"])
        return value

    def native_baseline(self, ack):
        need("native_snapshot" in ack and "native_state_acks" in ack, "native_acknowledgement_evidence_missing")
        baseline_ref = self.snapshot_reference(ack["native_snapshot"])
        need(all(baseline_ref[key] == ack["snapshot"][key] for key in ("path", "sha256", "bytes")),
             "native_baseline_reference_mismatch")
        configuration = self.manifest["state_validator"]
        module = self.state_module()
        inspected = module.inspect_external(baseline_ref, configuration["binding_sha256"], configuration["scope"])
        assert_preserved(inspected["controller_state"], ack["acknowledged_state"])
        need(isinstance(ack["native_state_acks"], list) and 4 <= len(ack["native_state_acks"]) <= 32,
             "native_ack_reference_population")
        progress, native_ids, native_kinds, last_images = [], set(), set(), {}
        high_level = link_ack_references(ack["acknowledgements"], ack["native_state_acks"])
        categories = {"metadata": "metadata", "favorite": "user_state", "user_name": "user_state",
                      "preferences": "user_state", "display_preferences": "user_state", "progress": "playback_progress",
                      "analysis_configuration": "settings", "managed_configuration": "settings"}
        for reference in ack["native_state_acks"]:
            fields(reference, ("mutation_id", "path", "sha256", "bytes"), ("fsynced",))
            durable_artifact({key: reference[key] for key in ("path", "sha256", "bytes")} | {"fsynced": True},
                             self.manifest["controller"]["artifacts_root"])
            raw, _sha = read_pinned(reference["path"], reference["sha256"])
            native = module.decode(raw)
            need(native["run_id"] == self.manifest["run_id"] and native["binding_sha256"] == configuration["binding_sha256"] and
                 native["mutation_id"] == reference["mutation_id"], "baseline_native_ack_scope")
            mutation_id = native["mutation_id"]
            need(mutation_id not in native_ids and mutation_id in high_level and native["kind"] in categories and
                 native["database_verified"] is True and native["http_readback_verified"] is True, "baseline_native_ack_population")
            native_ids.add(mutation_id)
            native_kinds.add(categories[native["kind"]])
            projected = high_level[mutation_id]
            need(projected["kind"] == categories[native["kind"]] and
                 projected["completed_unix_ns"] == int(native["completed_unix_ns"]), "native_ack_projection_scope")
            images = native["postimages"]
            need(isinstance(images, list) and 1 <= len(images) <= 4, "baseline_postimage_budget")
            expected, actual = [], []
            for image in images:
                fields(image, ("table", "key", "before_row", "row", "expected_fields"))
                module.guard_postimage(native["kind"], image["table"], image["before_row"], image["row"], image["expected_fields"])
                expected.append(image["expected_fields"])
                actual.append({key: image["row"][key] for key in image["expected_fields"]})
                identity = (image["table"], module.canonical(image["key"]).decode())
                if identity in last_images:
                    need(last_images[identity]["row"] == image["before_row"], "baseline_postimage_chain_discontinuous")
                last_images[identity] = image
            need(projected["expected_sha256"] == module.digest(expected) == module.digest(actual) == projected["observed_sha256"],
                 "native_ack_projection_values")
            writes = [event for event in native["http_evidence"] if event["method"] != "GET"]
            need(writes and projected["http_status"] == writes[-1]["status"] and
                 projected["response_sha256"] == module.digest(writes[-1]["body"]), "native_ack_http_projection")
            if native["kind"] == "progress":
                need(len(native["postimages"]) == 1 and native["postimages"][0]["table"] == "user_item_data", "baseline_progress_population")
                progress.append(native["postimages"][0]["key"])
        need(native_kinds == ACK_KINDS and native_ids == set(high_level), "native_ack_categories_missing")
        observer = module.external_observer(configuration["binding_sha256"], configuration["scope"],
                                             {baseline_ref["snapshot_id"] + ".sqlite": baseline_ref})
        connection, _summary = observer.open_snapshot(baseline_ref["snapshot_id"])
        try:
            for image in last_images.values():
                need(image["table"] in module.STRICT_TABLES and observer.compared_scope(image["table"], image["row"]) and
                     observer.row(connection, image["table"], image["key"]) == image["row"], "native_ack_not_present_in_durable_baseline")
        finally:
            connection.close()
        need(len(progress) == 1, "baseline_progress_ack_not_unique")
        self._progress_key = progress[0]
        rows = [row for row in inspected["private_progress_rows"] if
                [row["user_id"], row["item_id"]] == self._progress_key]
        need(len(rows) == 1, "baseline_progress_row_not_unique")
        self._progress_ticks = integer(rows[0]["playback_position_ticks"], 1)

    def validate_write_proof(self, proof, expected_before, kind, stage=None, resume_ticks=None):
        """Independently recheck actual external SQLite rows and native ACKs."""
        required = ("expected_state", "native_ack", "external_ack", "comparison", "before_snapshot", "after_snapshot", "observed_unix_ns")
        fields(proof, required + (("dispatches", "stage") if kind == "root_rebind" else ()))
        if kind == "root_rebind":
            need(proof["dispatches"] == 1 and proof["stage"] == stage, "rebind_stage_or_dispatch_mismatch")
        config = self.manifest["state_validator"]
        module = self.state_module()
        before_ref, after_ref = self.snapshot_reference(proof["before_snapshot"]), self.snapshot_reference(proof["after_snapshot"])
        external_ack = durable_artifact(proof["external_ack"], self.manifest["controller"]["artifacts_root"])
        raw, _sha = read_pinned(external_ack["path"], external_ack["sha256"], MAX_JSON)
        native = module.decode(raw)
        need(module.canonical(native) == module.canonical(proof["native_ack"]) and native["run_id"] == self.manifest["run_id"] and
             native["binding_sha256"] == config["binding_sha256"] and native["kind"] == kind, "native_write_ack_scope_mismatch")
        need(native["before_snapshot"] == before_ref["snapshot_id"] and native["after_snapshot"] == after_ref["snapshot_id"],
             "native_write_snapshot_mismatch")
        ack_ref = {key: external_ack[key] for key in ("path", "sha256", "bytes")} | {"mutation_id": native["mutation_id"]}
        images = native["postimages"]
        need(isinstance(images, list) and 1 <= len(images) <= 4, "native_write_postimage_budget")
        roots, libraries = set(), set()
        seen = set()
        for image in images:
            fields(image, ("table", "key", "before_row", "row", "expected_fields"))
            table, old, new = image["table"], image["before_row"], image["row"]
            need(isinstance(old, dict) and isinstance(new, dict), "write_target_not_preexisting")
            identity = (table, module.canonical(image["key"]))
            need(identity not in seen, "duplicate_write_postimage")
            seen.add(identity)
            if kind == "root_rebind" and table == "library_roots":
                allowed = {"binding_revision", "storage_binding", "bound_at", "bound_by"}
                need(new["id"] in config["scope"]["fault_root_ids"] and image["key"] == [new["id"]] and
                     new["binding_revision"] == old["binding_revision"] + 1 and new["storage_binding"] is not None and
                     new["storage_binding"] != old["storage_binding"], "rebind_postimage_revision_or_scope")
                roots.add(new["id"])
                libraries.add(new["library_id"])
            elif kind == "root_rebind" and table == "libraries":
                allowed = {"revision"}
                need(new["id"] in config["scope"]["library_ids"] and image["key"] == [new["id"]] and
                     new["revision"] == old["revision"] + 1, "rebind_library_revision")
            elif kind == "progress" and table == "user_item_data":
                allowed = {"playback_position_ticks", "play_count", "last_played_at"}
                need(image["key"] == [new["user_id"], new["item_id"]] and new["user_id"] in config["scope"]["user_ids"] and
                     new["item_id"] in config["scope"]["item_ids"] and new["playback_position_ticks"] == resume_ticks,
                     "resume_write_scope_or_position")
                need(image["key"] == self._progress_key, "resume_item_differs_from_acknowledged_progress")
                validate_resume_position(old["playback_position_ticks"], new["playback_position_ticks"],
                                         resume_ticks, self._progress_ticks)
                need(new["play_count"] in {old["play_count"], min(old["play_count"] + 1, 2147483647)}, "resume_play_count_delta")
            else:
                raise Invalid("unexpected_durable_write_table")
            need(set(image["expected_fields"]) <= allowed and
                 {key: value for key, value in old.items() if key not in allowed} ==
                 {key: value for key, value in new.items() if key not in allowed}, "write_blesses_unrelated_fields")
        if kind == "root_rebind":
            need(len(roots) == 1 and {image["row"]["id"] for image in images if image["table"] == "libraries"} == libraries,
                 "rebind_root_library_population")
        else:
            need(len(images) == 1, "resume_write_population")
        scope = dict(config["scope"])
        if roots:
            scope["fault_root_ids"] = sorted(roots)
        inspected = module.inspect_ack_external(ack_ref, config["binding_sha256"], before_ref, after_ref, scope)
        need(inspected["record_sha256"] == external_ack["sha256"], "native_ack_validation_digest")
        before = module.inspect_external(before_ref, config["binding_sha256"], scope)
        after = module.inspect_external(after_ref, config["binding_sha256"], scope)
        assert_preserved(expected_before, before["controller_state"])
        assert_preserved(proof["expected_state"], after["controller_state"])
        immutable = {"catalog_count", "catalog_identity_sha256", "settings_sha256"}
        immutable |= {"user_state_sha256", "playback_progress_sha256"} if kind == "root_rebind" else {"root_binding_sha256"}
        need(all(after["controller_state"][key] == expected_before[key] for key in immutable), "write_changed_unrelated_state")
        expectation = "explicit-rebind" if kind == "root_rebind" else "preserve"
        actual = module.compare_external(before_ref, after_ref, [ack_ref], config["binding_sha256"], scope, expectation)
        need(actual == proof["comparison"] and actual["passed"] is True and actual["failure_count"] == 0,
             "independent_native_write_comparison_failed")
        integer(proof["observed_unix_ns"], int(native["completed_unix_ns"]))
        return after["controller_state"]

    def invoke(self, role, op, scenario, payload=None, mutation=False, timeout=None):
        return self.rpc.call(role, op, scenario, payload, mutation, timeout)

    def observe(self, op, scenario, payload=None, timeout=None):
        deadline = time.monotonic() + (timeout or self.manifest["budgets"]["recovery_seconds"])
        while True:
            check_cancelled()
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ObservationTimeout("recovery_observation_deadline")
            result = self.invoke("observer", op, scenario, payload, timeout=min(remaining, self.manifest["budgets"]["rpc_seconds"]))
            if result["status"] == "ok":
                return result["data"]
            self.journal.write("pending-observation", {"operation": op, "scenario_id": scenario["scenario_id"],
                                                       "data": result["data"]})
            time.sleep(min(self.manifest["budgets"]["poll_seconds"], max(0, deadline - time.monotonic())))

    def fault_observation(self, scenario, payload):
        timeout = self.manifest["budgets"]["fault_seconds"]
        if scenario["fault"] in MUTATING_FAULT_OBSERVATIONS:
            # These observations include actual scan/preview admission or a
            # metadata write. Unknown disposition must not repeat those calls.
            return self.invoke("observer", "fault_observation", scenario, payload,
                               mutation=True, timeout=timeout)["data"]
        return self.observe("fault_observation", scenario, payload, timeout=timeout)

    def scenario(self, scenario):
        started = time.monotonic()
        self.journal.write("scenario-start", {"scenario": scenario, "manifest_sha256": self.manifest_sha256})
        before = validate_identity(self.observe("guest_identity", scenario), self.manifest)
        host_before = validate_hypervisor(self.observe("hypervisor_identity", scenario), self.manifest)
        ack_response = self.invoke("workload", "acknowledge", scenario,
                                   {"workload_manifest": self.manifest["workload_manifest"]}, mutation=True)
        need(ack_response["status"] == "ok", "workload_not_fault_ready")
        ack = ack_response["data"]
        validate_ack(ack, self.manifest, scenario)
        self.native_baseline(ack)
        # Copy and fsync the actual read-back snapshot outside the guest. A
        # guest-local checkpoint or hash without these bytes is insufficient.
        snapshot = durable_artifact(ack["snapshot"], self.manifest["controller"]["artifacts_root"])
        durable = self.journal.write("durable-acknowledged-state",
                                     {"ack": ack, "snapshot": snapshot, "before": before,
                                      "hypervisor_before": host_before})
        if scenario["late_mount"]:
            self.invoke("executor", "arm_late_mount", scenario, {"durable_ack": durable}, mutation=True)
        injection = self.invoke("executor", "inject", scenario,
                                {"durable_ack": durable, "before": before, "hypervisor_before": host_before}, mutation=True)
        injected = self.journal.write("fault-dispatched", {"scenario_id": scenario["scenario_id"], "injection": injection})
        fault = validate_fault_observation(self.fault_observation(scenario, {"injection": injection, "ack": ack}),
                                           scenario, self.manifest, ack)
        expected_state, rebind_chain, rebind_proofs = ack["acknowledged_state"], [], []
        if scenario["fault"] in REPLACEMENTS:
            # Once replacement storage is explicitly approved, a new scan may
            # legitimately reconcile its different contents. Settle the fault
            # workload before approving either topology so this experiment
            # isolates binding recovery from intentional content replacement.
            self.invoke("workload", "settle_scenario", scenario, {"ack": ack, "fault": fault}, mutation=True)
            write = self.invoke("workload", "rebind_replacement", scenario, {"ack": ack, "fault": fault}, mutation=True)["data"]
            expected_state = self.validate_write_proof(write, expected_state, "root_rebind", stage="replacement")
            rebind_proofs.append(write)
            rebind_chain.append(self.journal.write("durable-rebind-state", {"scenario_id": scenario["scenario_id"], "proof": write}))
        # Recovery is a new, predeclared action, never a repetition of inject.
        # It is reached only after a definitive injection receipt and observed
        # fault. Unknown disposition instead requires an independent operator.
        if scenario["late_mount"]:
            # Healthy-root scan probes submit real work even while mounts are
            # late. Keep this as one durably registered mutation invocation.
            late = self.invoke("observer", "late_mount_observation", scenario, {"ack": ack},
                               mutation=True, timeout=self.manifest["budgets"]["fault_seconds"])["data"]
            fields(late, ("boot_id", "mount_present", "ready_status", "state", "healthy_probes",
                           "affected_read_success", "observed_unix_ns", "dispatches"))
            need(late["dispatches"] == 1 and late["boot_id"] != before["boot_id"] and late["mount_present"] is False and
                 late["affected_read_success"] is False and late["ready_status"] in (200, 503),
                 "late_mount_not_observed")
            # /readyz reports catalog/task/engine readiness, not availability
            # of every media root. Preserve the actual status without inventing
            # an unavailable-root readiness requirement.
            assert_preserved(ack["acknowledged_state"], late["state"])
            healthy_probes(late["healthy_probes"], self.manifest, late["observed_unix_ns"])
            self.journal.write("late-mount-evidence", late)
        self.invoke("executor", "recover", scenario, {"injection": injection, "fault": fault}, mutation=True)
        if scenario["fault"] in REPLACEMENTS:
            write = self.invoke("workload", "rebind_original", scenario, {"ack": ack, "fault": fault, "rebind_chain": rebind_chain}, mutation=True)["data"]
            expected_state = self.validate_write_proof(write, expected_state, "root_rebind", stage="original")
            validate_rebind_chain(rebind_proofs[0], write)
            rebind_proofs.append(write)
            rebind_chain.append(self.journal.write("durable-rebind-state", {"scenario_id": scenario["scenario_id"], "proof": write}))
        after = validate_identity(self.observe("guest_identity", scenario), self.manifest)
        host_after = validate_hypervisor(self.observe("hypervisor_identity", scenario), self.manifest)
        need(host_after["config_sha256"] == host_before["config_sha256"], "guest_configuration_changed")
        if scenario["fault"] not in REPLACEMENTS:
            self.invoke("workload", "settle_scenario", scenario, {"ack": ack, "fault": fault}, mutation=True)
        # This adapter first observes restored durable state, then performs one
        # real playback reconnect/resume and adopts only its legitimate row
        # changes. It is therefore a mutation with no automatic observation
        # retry, despite the historical operation name.
        recovery = self.invoke("observer", "recovery_observation", scenario,
                               {"ack": ack, "before": before, "after": after, "fault": fault,
                                "rebind_chain": rebind_chain, "expected_state": expected_state}, mutation=True,
                               timeout=self.manifest["budgets"]["recovery_seconds"])["data"]
        validate_recovery(recovery, self.manifest, scenario, ack, before, after, fault, expected_state)
        if scenario["fault"] in REPLACEMENTS:
            need(recovery.get("rebind_chain") == rebind_chain, "durable_rebind_chain_mismatch")
        resume = recovery["post_resume"]
        expected_after_resume = self.validate_write_proof(resume, expected_state, "progress",
                                                          resume_ticks=recovery["playback_resume"]["persisted_ticks"])
        self.journal.write("durable-resume-state", {"scenario_id": scenario["scenario_id"], "proof": resume,
                                                   "state": expected_after_resume})
        closure = self.invoke("workload", "close_scenario", scenario, {"ack": ack, "recovery": recovery}, mutation=True)
        closed = self.observe("closure_observation", scenario, {"closure": closure},
                              timeout=self.manifest["budgets"]["cleanup_seconds"])
        fields(closed, ("owned_processes_remaining", "owned_faults_remaining", "owned_locks_remaining",
                        "unclosed_temporary_bytes", "original_modes_restored", "original_mounts_restored"))
        need(closed["owned_processes_remaining"] == [] and closed["owned_faults_remaining"] == [] and
             closed["owned_locks_remaining"] == [] and closed["unclosed_temporary_bytes"] == 0 and
             closed["original_modes_restored"] is True and closed["original_mounts_restored"] is True,
             "scenario_resource_closure_incomplete")
        result = {"scenario_id": scenario["scenario_id"], "fault": scenario["fault"], "status": "passed",
                  "duration_seconds": time.monotonic() - started, "fault_dispatch": injected,
                  "before_boot_id": before["boot_id"], "after_boot_id": after["boot_id"],
                  "hypervisor_before": host_before, "hypervisor_after": host_after,
                  "recovery": recovery, "closure": closed}
        self.journal.write("scenario-result", result)
        self.results.append(result)

    def run(self):
        self.journal.write("run-admitted", {"manifest_sha256": self.manifest_sha256,
                                            "source_revision": self.manifest["source_revision"],
                                            "tier": self.manifest["tier"]})
        for scenario in self.manifest["scenarios"]:
            self.scenario(scenario)
        present = {item["fault"] for item in self.results}
        # Each case has a separate context and current process-lifetime pins.
        # Matrix acceptance belongs to the independent external composer.
        complete_matrix = False
        report = {"schema_version": VERSION, "run_id": self.manifest["run_id"],
                  "manifest_sha256": self.manifest_sha256, "source_revision": self.manifest["source_revision"],
                  "tier": self.manifest["tier"], "status": "partial",
                  "accepted_faults": sorted(present), "pending_faults": sorted(FAULTS - present),
                  "complete_fault_matrix": complete_matrix, "late_mount_covered": any(s["late_mount"] for s in self.manifest["scenarios"]),
                  "scenario_count": len(self.results), "physical_power_loss_tested": False,
                  "matrix_composition_required": True,
                  "claims": ["owned_guest_only", "external_acknowledgements", "independent_identity_observations"]}
        self.journal.write("run-result", report)
        return report


@contextmanager
def controller_lock(root):
    path = root / ".recovery-controller.lock"
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        info = os.fstat(fd)
        need(stat.S_ISREG(info.st_mode) and info.st_uid == os.geteuid() and
             stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1, "unsafe_controller_lock")
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise Invalid("controller_already_running") from error
        yield
    finally:
        os.close(fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--manifest-sha256", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--admit-only", action="store_true")
    args = parser.parse_args()
    journal = None
    try:
        need(sys.platform == "linux", "external_linux_controller_required")
        raw, sha = read_pinned(args.manifest, digest(args.manifest_sha256))
        manifest = load_manifest(decode(raw))
        root = local_path(manifest["controller"]["artifacts_root"], directory=True)
        need(Path("/etc/machine-id").read_text(encoding="ascii").strip() == manifest["controller"]["machine_id"],
             "controller_machine_identity_mismatch")
        marker, _sha = read_pinned(manifest["controller"]["marker"]["path"], manifest["controller"]["marker"]["sha256"])
        marker_value = decode(marker)
        fields(marker_value, ("schema_version", "owner_id", "role", "machine_id"))
        need(marker_value == {"schema_version": VERSION, "owner_id": manifest["owner_id"],
                               "role": "external_recovery_controller", "machine_id": manifest["controller"]["machine_id"]},
             "controller_marker_mismatch")
        output = Path(str(beneath(args.output, str(root))))
        need(output.parent.resolve(strict=True) == output.parent and not output.exists(), "output_already_exists")
        workload_raw, _sha = read_pinned(manifest["workload_manifest"]["path"], manifest["workload_manifest"]["sha256"], private=False)
        workload = decode(workload_raw)
        for name in ("schema_version", "run_id", "source_revision", "profile_id", "tier", "owner_id"):
            need(workload[name] == manifest[name], "workload_manifest_scope_mismatch")
        for adapter in manifest["adapters"].values():
            read_pinned(adapter["path"], adapter["sha256"], MAX_ADAPTER_BYTES, private=False)
            read_pinned(adapter["context"]["path"], adapter["context"]["sha256"])
        read_pinned(manifest["state_validator"]["source"]["path"], manifest["state_validator"]["source"]["sha256"],
                    MAX_ADAPTER_BYTES, private=False)
        with controller_lock(root):
            output.mkdir(mode=0o700)
            journal = Journal(output)
            for sig in (signal.SIGTERM, signal.SIGINT):
                signal.signal(sig, cancellation)
            if args.admit_only:
                report = {"status": "admitted", "manifest_sha256": sha, "execution_performed": False,
                          "guest_readiness_established": False}
                journal.write("admission", report)
            else:
                report = Controller(manifest, sha, journal).run()
            print(canonical(report).decode("ascii"), flush=True)
            return 0 if report["status"] in {"admitted", "passed"} else 2
    except BaseException as error:
        code = str(error) if isinstance(error, Invalid) else type(error).__name__
        report = {"status": "failed", "reason": code, "automatic_mutation_retry": False,
                  "remote_resource_closure": "not_established", "operator_observation_required": True}
        if journal:
            try:
                journal.write("run-failed", report)
            except BaseException:
                pass
        print(canonical(report).decode("ascii"), flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
