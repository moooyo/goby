"""Derive Phase 3 fault facts from actual collector records, never pass flags.

This module performs no SSH, HTTP, mutation, process launch, or live probing.
Its inputs are retained records from the pinned native guest and probe helpers.
"""
from datetime import datetime, timezone
import hashlib
import json
from pathlib import PurePosixPath
import re


class EvidenceError(RuntimeError):
    """A bounded, non-secret evidence refusal."""


def need(value, code):
    if not value:
        raise EvidenceError(code)


def integer(value, low=0, high=(1 << 63) - 1):
    need(type(value) is int and low <= value <= high, "integer_evidence_required")
    return value


def decimal_integer(value):
    need(isinstance(value, str) and re.fullmatch(r"[0-9]{1,20}", value), "decimal_integer_evidence_required")
    return integer(int(value))


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def inside(path, root):
    need(isinstance(path, str) and isinstance(root, str), "path_evidence_required")
    candidate, parent = PurePosixPath(path), PurePosixPath(root)
    need(candidate.is_absolute() and parent.is_absolute() and ".." not in candidate.parts,
         "absolute_evidence_path_required")
    return candidate == parent or parent in candidate.parents


def process_key(value):
    return integer(value["pid"], 2), integer(value["start_ticks"], 1)


def task_key(value):
    return (*process_key(value), integer(value["tid"], 2), integer(value["task_start_ticks"], 1))


def postgres_time(value):
    need(isinstance(value, str) and len(value) <= 128, "postgres_timestamp_required")
    parsed = datetime.fromisoformat(value.replace(" UTC", "+00:00").replace("Z", "+00:00"))
    need(parsed.tzinfo is not None, "postgres_timestamp_zone_required")
    elapsed = parsed.astimezone(timezone.utc) - datetime(1970, 1, 1, tzinfo=timezone.utc)
    return (elapsed.days * 86400 + elapsed.seconds) * 1_000_000 + elapsed.microseconds


def original_lease_completion(before, after, item_id, source_id, started_unix_ns, completed_unix_ns):
    """Use the native once-only source-lifetime completion record and reject ring loss."""
    left, right = before["OriginalStreams"], after["OriginalStreams"]
    need(left["InstanceId"] == right["InstanceId"], "original_stream_instance_changed")
    for observation in (left, right):
        need(observation["CurrentCapacityDropped"] == 0, "original_stream_projection_incomplete")
        decimal_integer(observation["CompletionCapacityDropped"])
        need(observation["ActiveCount"] == len(observation["Current"]), "original_stream_active_count_mismatch")
    current = [row for row in left["Current"] if row["ItemId"] == item_id and row["MediaSourceId"] == source_id
                and started_unix_ns <= decimal_integer(row["StartedUnixNano"]) <= completed_unix_ns]
    need(len(current) == 1 and current[0]["Active"] is True and current[0]["CompletedUnixNano"] == "0",
         "unique_active_original_lease_missing")
    selected = [row for row in right["Completed"] if row["LeaseId"] == current[0]["LeaseId"]
                and row["Sequence"] == current[0]["Sequence"] and row["ItemId"] == item_id and row["MediaSourceId"] == source_id]
    need(len(selected) == 1, "unique_original_source_completion_missing")
    row = selected[0]
    sequence = decimal_integer(row["CompletionSequence"])
    need(row["StartedUnixNano"] == current[0]["StartedUnixNano"] and row["Active"] is False and sequence > 0
         and decimal_integer(right["OldestCompletionSequence"]) <= sequence < decimal_integer(right["NextCompletionSequence"])
         and decimal_integer(row["CompletedUnixNano"]) >= completed_unix_ns
         and not any(active["LeaseId"] == row["LeaseId"] for active in right["Current"]),
         "original_source_leave_not_completed")
    return row


# These are Linux syscall ABIs, not a guess based on a wait-channel substring.
# Only operations with an observable first-argument FD are admitted here.
SYSCALLS = {
    "x86_64": {"read": {0, 17, 19, 295, 327}, "metadata": {5, 138, 257, 262, 332}},
    "aarch64": {"read": {63, 65, 67, 69, 286}, "metadata": {44, 56, 79, 80, 291}},
}


def source_fd(observation, task, volume, operation):
    """Bind a blocked syscall to an actual open FD on the injected device."""
    need(operation in ("read", "metadata") and task["architecture"] in SYSCALLS, "unsupported_syscall_abi")
    tokens = task["syscall"].split()
    need(len(tokens) == 9 and tokens[0].isdigit(), "complete_proc_syscall_required")
    number, descriptor = int(tokens[0]), int(tokens[1], 0)
    need(number in SYSCALLS[task["architecture"]][operation] and descriptor >= 0,
         "syscall_operation_or_fd_unproven")
    rows = [row for row in observation["fd_inventory"] if row["fd"] == descriptor]
    need(len(rows) == 1, "syscall_fd_not_observed")
    row = rows[0]
    need(integer(row["inode"], 1) and integer(row["mount_id"], 1), "fd_identity_missing")
    need(row["target"] == task["syscall_first_fd_target"] and inside(row["target"], volume["mountpoint"]),
         "syscall_fd_outside_fault_volume")
    mounts = [mount for mount in observation["mounts"] if mount["mount_id"] == row["mount_id"]]
    need(len(mounts) == 1 and mounts[0]["major_minor"] == volume["major_minor"], "syscall_fd_mount_unproven")
    return {"fd": descriptor, "target": row["target"], "mount_id": row["mount_id"],
            "inode": row["inode"], "major_minor": mounts[0]["major_minor"], "syscall_number": number}


def blocked_witness(first, last, affected, volume, goby_cgroup, operation):
    """Require a sustained D task after an actual timed-out or bounded GET."""
    need(affected["outcome"] in {"timeout", "bounded_503"} and affected["http_method"] == "GET"
         and (affected["outcome"] != "bounded_503" or affected["http_status"] == 503)
         and affected["http_started_unix_ns"] < affected["http_completed_unix_ns"], "actual_caller_return_missing")
    start = integer(first["observed_unix_ns"], affected["http_completed_unix_ns"])
    end = integer(last["observed_unix_ns"], start + 1_000_000_000)
    need(process_key(first["goby"]) == process_key(last["goby"]), "blocked_process_lifetime_changed")
    candidates = {task_key(row): row for row in first["blocked_tasks"] if row["state"] == "D"}
    matches = []
    for current in last["blocked_tasks"]:
        previous = candidates.get(task_key(current))
        if previous is None or current["state"] != "D":
            continue
        need(previous["cgroup"] == current["cgroup"] == goby_cgroup, "blocked_task_not_owned")
        try:
            before_fd = source_fd(first, previous, volume, operation)
            after_fd = source_fd(last, current, volume, operation)
        except EvidenceError:
            continue
        if before_fd != after_fd or previous["syscall"] != current["syscall"]:
            continue
        if "source_path" in affected:
            need(inside(affected["source_path"], volume["mountpoint"]), "affected_source_outside_fault_volume")
            if not (before_fd["target"] == affected["source_path"] or
                    operation == "metadata" and inside(affected["source_path"], before_fd["target"])):
                continue
        matches.append(({"pid": current["pid"], "start_ticks": current["start_ticks"], "tid": current["tid"],
                "task_start_ticks": current["task_start_ticks"], "state": "D", "syscall": current["syscall"],
                "wchan": current["wchan"], "operation": operation, "cgroup": current["cgroup"],
                "request_sha256": affected["request_sha256"], "first_seen_unix_ns": start,
                "last_seen_unix_ns": end}, before_fd))
    need(len(matches) == 1, "unique_sustained_source_bound_d_task_missing")
    return matches[0]


def retired_worker(witness, descriptor, after, completion, baseline=None, blocked_samples=()):
    """Thread absence alone is never a completion or resource-release proof."""
    need(completion["kind"] in {"native_task_terminal", "owned_media_helper_exit", "native_original_leave",
                                "native_storage_observations_drained"}, "concrete_worker_completion_required")
    need(completion["evidence_fd"] == descriptor and inside(completion["source_path"], descriptor["target"]),
         "worker_completion_source_mismatch")
    need(completion["completed_unix_ns"] >= witness["last_seen_unix_ns"] and
         completion["completed_unix_ns"] <= after["observed_unix_ns"], "worker_completion_time_unproven")
    need(completion["terminal_state"] in {"completed", "cancelled", "interrupted", "failed"},
         "worker_not_terminal")
    need(re.fullmatch(r"[0-9a-f]{64}", completion["evidence_sha256"]), "worker_completion_artifact_required")
    need(completion["source_admission_id"] and completion["operation_id"], "worker_completion_admission_required")
    if completion["kind"] == "owned_media_helper_exit":
        need(completion["waited_pid"] == completion["pid"] and type(completion["exit_code"]) is int
             and completion["process_group_members_after"] == [], "helper_exit_not_joined")
    elif completion["kind"] == "native_task_terminal":
        need(completion["before_task_id"] == completion["after_task_id"] == completion["operation_id"]
             and completion["before_admission_sha256"] == completion["after_admission_sha256"],
                 "native_task_completion_not_same_admission")
    elif completion["kind"] == "native_original_leave":
        need(completion["lease"]["LeaseId"] == completion["operation_id"]
             and completion["lease"]["Active"] is False
             and decimal_integer(completion["lease"]["CompletedUnixNano"]) == completion["completed_unix_ns"],
             "native_original_completion_mismatch")
    else:
        need(completion["before"]["StorageObservations"]["Active"] > 0
             and completion["after"]["StorageObservations"]["Active"] == 0
             and completion["admissions_stopped"]["sha256"], "storage_observation_lifetime_not_closed")
        need(all(completion["after"]["ScanEvidence"][key] == 0 for key in
                 ("ActivePasses", "RetiringPasses", "CleanupFailures", "ReservedBytes", "ReservedFileDescriptors")),
             "storage_scan_evidence_not_released")
    remaining = [row for row in after["fd_inventory"] if row["mount_id"] == descriptor["mount_id"]
                 and row["inode"] == descriptor["inode"]]
    for row in remaining:
        # The syscall's descriptor is never exempt. A pre-existing root anchor
        # is allowed only with the same full FD identity in every retained
        # observation of the same application lifetime.
        need(row["fd"] != descriptor["fd"] and baseline is not None and len(blocked_samples) == 2,
             "evidence_source_fd_still_open")
        for observation in (baseline, *blocked_samples):
            need(process_key(observation["goby"]) == process_key(after["goby"])
                 and row in observation["fd_inventory"], "new_or_changed_source_fd_not_anchor")
    need(completion["temporary_source_handles"] == [] and completion["spool_generation_handles"] == []
         and completion["owner_backend_references"] == [], "logical_worker_resources_still_owned")
    same_task = [row for row in after["task_inventory"] if task_key(row) == task_key(witness)]
    need(not same_task or len(same_task) == 1 and same_task[0]["state"] != "D" and
         same_task[0]["syscall"] != witness["syscall"], "original_syscall_still_observed")
    return {key: witness[key] for key in ("pid", "start_ticks", "tid", "task_start_ticks")} | {
        "syscall_returned": True, "worker_released": True, "evidence_handles_closed": True,
        "completion_evidence_sha256": completion["evidence_sha256"], "source_fd": descriptor}


def postgres_lock_facts(before, waiting, after, log_records, before_state, after_state, injection):
    """Correlate real PG lock wait, SQLSTATE and rollback with the same backend."""
    owner = before["owner_backend"]
    need(owner is not None and after["owner_backend"] == owner, "postgres_owner_backend_not_preserved")
    holders = set(injection["blocker_backend_pids"])
    need(holders and all(type(pid) is int and pid > 1 for pid in holders), "owned_lock_backend_evidence_missing")
    waits = [row for row in waiting["backends"] if row["pid"] == owner["pid"] and
             row["backend_start"] == owner["backend_start"] and row["wait_event_type"] == "Lock"
             and set(row["blocking_pids"]) & holders]
    need(len(waits) == 1, "actual_owned_backend_lock_wait_missing")
    wait = waits[0]
    errors = [row for row in log_records if row.get("state_code") == "57014" and row.get("pid") == owner["pid"]
              and postgres_time(row["session_start"]) // 1_000_000 == postgres_time(owner["backend_start"]) // 1_000_000
              and waiting["observed_unix_ns"] // 1000 <= postgres_time(row["timestamp"]) <= after["observed_unix_ns"] // 1000
              and
              hashlib.sha256(row.get("statement", "").encode()).hexdigest() == wait["query_sha256"]]
    need(len(errors) == 1, "actual_statement_timeout_sqlstate_missing")
    need(before_state == after_state, "lock_timeout_partial_durable_change")
    settled = [row for row in after["backends"] if row["pid"] == owner["pid"] and row["backend_start"] == owner["backend_start"]]
    need(len(settled) == 1 and settled[0]["xact_start"] is None and settled[0]["state"] == "idle"
         and settled[0]["blocking_pids"] == [], "owner_transaction_not_rolled_back")
    return {"wait_event_type": "Lock", "statement_sqlstate": "57014", "transaction_rolled_back": True,
            "same_owner_backend_survived": True, "before_rows_sha256": digest(before_state),
            "after_rows_sha256": digest(after_state), "owner_backend": owner,
            "wait_record_sha256": digest(wait), "sqlstate_record_sha256": digest(errors[0])}


def original_mount_restored(before, after, volume_id):
    previous, current = before["volume_observations"][volume_id], after["volume_observations"][volume_id]
    need(current.get("present") is True and current.get("mounted") is True, "original_volume_not_present")
    fields = ("dm_uuid", "major_minor", "loop_backing_file", "size_bytes")
    need(all(current.get(key) == previous.get(key) for key in fields), "original_volume_identity_not_restored")
    return True
