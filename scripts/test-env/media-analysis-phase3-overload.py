#!/usr/bin/env python3
"""Source-bound native original-stream quota acceptance, separate from capacity.

This finite helper starts no services, creates no media, mutates no catalog rows
and performs no decoding. It uses two existing viewers and a real licensed
source, holds eight actual original leases, checks the ninth rejection, then
checks another user's allowance and recovery after release. Importing does no
network, process or media work.
"""

import argparse
import copy
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import socket
import stat
import sys
import threading
import time
from urllib.parse import urlencode, urlsplit


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("phase3_overload_workload", HERE / "media-analysis-phase3-workload.py")
WORK = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WORK)
need, exact, integer = WORK.need, WORK.exact, WORK.integer
digest, json_bytes, strict_json = WORK.digest, WORK.json_bytes, WORK.strict_json
OWNER_LIMIT = 8
RECEIVE_BUFFER = 32768
PREFIX_BYTES = 32
MIN_SOURCE_BYTES = 16 << 20


def pinned(reference, maximum):
    exact(reference, "path sha256", "overload_reference_fields")
    raw = WORK.read_private(reference["path"], maximum)
    need(digest(raw) == reference["sha256"], "overload_reference_changed")
    return raw


def validate_binding(value):
    exact(value, "schema_version run_id owner_id source_revision tier profile_id manifest context fault_fixture source limits", "overload_binding_fields")
    need(type(value["schema_version"]) is int and value["schema_version"] == 1, "overload_version")
    for name in ("run_id", "owner_id", "profile_id"):
        need(type(value[name]) is str and WORK.SAFE.fullmatch(value[name]), "overload_identifier")
    need(type(value["source_revision"]) is str and re.fullmatch(r"[0-9a-f]{40}", value["source_revision"]), "overload_source_revision")
    need(value["tier"] in (10000, 100000), "overload_tier")
    exact(value["source"], "item_id media_source_id library_id root_id path bytes sha256", "overload_source_fields")
    for name in ("item_id", "media_source_id", "library_id", "root_id"):
        need(type(value["source"][name]) is str and re.fullmatch(r"[A-Za-z0-9_-]{1,128}", value["source"][name]), "overload_source_identifier")
    need(type(value["source"]["path"]) is str and len(value["source"]["path"]) <= 4096
         and re.fullmatch(r"[0-9a-f]{64}", value["source"]["sha256"]), "overload_source_path_or_digest")
    exact(value["limits"], "admission_seconds active_seconds cleanup_seconds request_seconds max_reject_ms max_recover_ms max_source_bytes max_output_bytes", "overload_limits_fields")
    bounds = {"admission_seconds": (30, 300), "active_seconds": (5, 20), "cleanup_seconds": (5, 30), "request_seconds": (1, 5),
        "max_reject_ms": (1, 5000), "max_recover_ms": (1, 5000), "max_source_bytes": (MIN_SOURCE_BYTES, 2 << 30),
        "max_output_bytes": (4 << 20, 32 << 20)}
    for name, (lower, upper) in bounds.items():
        integer(value["limits"][name], lower, upper, "overload_limit_value")
    integer(value["source"]["bytes"], MIN_SOURCE_BYTES, value["limits"]["max_source_bytes"], "overload_real_source_size")
    return value


def decimal(value):
    need(type(value) is str and re.fullmatch(r"0|[1-9][0-9]{0,19}", value), "original_counter_wire")
    return integer(int(value), 0, (1 << 64) - 1, "original_counter_range")


def lease_snapshot(value):
    """Keep current lease identity separate from completion and TCP closure."""
    exact(value, "InstanceId ActiveCount CurrentLimit CompletionLimit CurrentCapacityDropped CompletionCapacityDropped NextLeaseSequence NextCompletionSequence OldestCompletionSequence Current Completed", "original_snapshot_fields")
    need(type(value["InstanceId"]) is str and re.fullmatch(r"[0-9a-f]{32}", value["InstanceId"]), "original_instance")
    need(value["CurrentLimit"] == 64 and value["CompletionLimit"] == 128, "original_snapshot_bounds_changed")
    integer(value["ActiveCount"], 0, 64, "original_active_count")
    need(type(value["CurrentCapacityDropped"]) is int and value["CurrentCapacityDropped"] == 0 and type(value["Current"]) is list and len(value["Current"]) == value["ActiveCount"]
         and type(value["Completed"]) is list and len(value["Completed"]) <= 128, "original_observation_incomplete")
    for name in ("CompletionCapacityDropped", "NextLeaseSequence", "NextCompletionSequence", "OldestCompletionSequence"):
        decimal(value[name])
    current, completed = {}, {}
    for target, rows, active in ((current, value["Current"], True), (completed, value["Completed"], False)):
        for row in rows:
            exact(row, "LeaseId Sequence CompletionSequence ItemId MediaSourceId StartedUnixNano CompletedUnixNano Active", "original_lease_fields")
            sequence = decimal(row["Sequence"])
            completion = decimal(row["CompletionSequence"])
            started, finished = decimal(row["StartedUnixNano"]), decimal(row["CompletedUnixNano"])
            need(sequence > 0 and row["LeaseId"] == value["InstanceId"] + "-" + str(sequence)
                 and row["LeaseId"] not in target and row["Active"] is active and started > 0, "original_lease_identity")
            need((completion == 0 and finished == 0) if active else (completion > 0 and finished >= started), "original_lease_lifetime")
            need(type(row["ItemId"]) is str and type(row["MediaSourceId"]) is str, "original_lease_source")
            target[row["LeaseId"]] = row
    need(not set(current).intersection(completed), "original_lease_both_active_and_completed")
    need(decimal(value["NextLeaseSequence"]) > max((decimal(row["Sequence"]) for row in [*current.values(), *completed.values()]), default=0)
         and decimal(value["NextCompletionSequence"]) > max((decimal(row["CompletionSequence"]) for row in completed.values()), default=0), "original_next_sequence")
    return {"instance_id": value["InstanceId"], "current": current, "completed": completed,
            "next_lease": decimal(value["NextLeaseSequence"]), "next_completion": decimal(value["NextCompletionSequence"])}


def source_stamp(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class SlowResponse:
    """Own the TCP socket before response parsing so every failure can close it."""

    def __init__(self, actor, source, credential, label, deadline):
        self.actor, self.source, self.credential = actor, source, credential
        self.label, self.deadline = label, deadline
        self.socket = self.connection = self.response = self.watchdog = None
        self.lease_id, self.prefix, self.status, self.headers = None, b"", None, {}
        self.started_ns, self.header_ns, self.prefix_ns, self.closed_ns = time.monotonic_ns(), None, None, None

    def close(self):
        # Closing the transport first avoids HTTPResponse.close draining bytes.
        if self.socket is not None:
            try:
                self.socket.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            self.socket.close()
        if self.response is not None:
            self.response.close()
        if self.connection is not None:
            self.connection.close()
        if self.watchdog is not None:
            self.watchdog.cancel()
            self.watchdog.join(timeout=1)
            need(not self.watchdog.is_alive(), "overload_watchdog_not_joined")
        self.closed_ns = time.monotonic_ns()

    def open(self):
        self.actor.assert_owned()
        origin = urlsplit(self.actor.c["origin"])
        family = socket.AF_INET6 if origin.hostname == "::1" else socket.AF_INET
        self.socket = socket.socket(family, socket.SOCK_STREAM)
        self.socket.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, RECEIVE_BUFFER)
        self.socket.settimeout(max(.001, self.deadline - time.monotonic()))
        self.socket.connect((origin.hostname, origin.port))
        self.connection = http.client.HTTPConnection(origin.hostname, origin.port)
        self.connection.sock = self.socket
        transport = self.socket
        def expire():
            try:
                transport.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
        self.watchdog = threading.Timer(max(.001, self.deadline - time.monotonic()), expire)
        self.watchdog.start()
        self.connection.request("GET", "/emby/Videos/" + self.source["item_id"] + "/original.mp4?" + urlencode({"MediaSourceId": self.source["media_source_id"]}),
            headers={"Connection": "close", "Accept-Encoding": "identity", "X-Emby-Token": self.credential["token"],
                     "X-Emby-Authorization": 'MediaBrowser Client="Phase3Overload", Device="Linux", DeviceId="%s", Version="1"' % self.credential["device_id"]})
        self.response = self.connection.getresponse()
        self.header_ns = time.monotonic_ns()
        self.status, self.headers = self.response.status, dict(self.response.getheaders())
        need(self.status == 200 and self.response.length == self.source["bytes"] and bool(self.headers.get("ETag"))
             and self.headers.get("Content-Type", "").startswith("video/"), "overload_original_response")
        self.prefix = self.response.read(PREFIX_BYTES)
        self.prefix_ns = time.monotonic_ns()
        need(len(self.prefix) == PREFIX_BYTES, "overload_original_prefix_short")
        self.watchdog.cancel()
        self.watchdog.join(timeout=1)
        need(not self.watchdog.is_alive(), "overload_watchdog_not_joined")
        self.socket.settimeout(None)
        return self

    def receipt(self):
        return {"label": self.label, "status": self.status, "headers": self.headers, "lease_id": self.lease_id,
            "start_ns": self.started_ns, "header_ns": self.header_ns, "prefix_ns": self.prefix_ns,
            "closed_ns": self.closed_ns, "prefix_bytes": len(self.prefix), "prefix_sha256": digest(self.prefix),
            "requested_receive_buffer": RECEIVE_BUFFER, "actual_receive_buffer": self.socket.getsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF) if self.socket and self.socket.fileno() >= 0 else None}


class Overload:
    def __init__(self, binding, manifest, context, fault, evidence):
        self.b, self.m, self.c, self.f, self.e = binding, manifest, context, fault, evidence
        limits = binding["limits"]
        bounded = copy.deepcopy(manifest)
        bounded["budgets"].update(request_seconds=limits["request_seconds"], max_requests=512, max_events=2048,
            max_artifact_bytes=limits["max_output_bytes"], max_response_bytes=65536, max_process_seconds=20)
        self.actor = WORK.Actor(bounded, context, evidence, request_scope="overload-" + digest(json_bytes(binding))[:32])
        self.actor.phase = "overload-admission"
        self.actor.deadline = time.monotonic() + limits["admission_seconds"]
        self.active_deadline = None
        self.source_fd, self.source_before, self.prefix = None, None, None
        self.opened, self.known_leases, self.errors, self.proof = [], set(), [], {}
        self.baseline = self.last_snapshot = None

    def snapshot(self):
        result = self.actor.native("/admin/v1/runtime/resources")
        need(type(result) is dict and "OriginalStreams" in result and "DatabasePool" in result, "overload_runtime_resources")
        current = lease_snapshot(result["OriginalStreams"])
        WORK.database_pool_values(result["DatabasePool"])
        if self.baseline is not None:
            need(current["instance_id"] == self.baseline["instance_id"], "overload_original_runtime_restarted")
        self.last_snapshot = current
        self.e.event("original_resources", phase=self.actor.phase, active_count=len(current["current"]),
                     next_lease=str(current["next_lease"]), next_completion=str(current["next_completion"]))
        return current

    def expect_current(self, expected, completed=(), deadline=None):
        deadline = min(deadline or self.actor.deadline, self.actor.deadline)
        while time.monotonic() < deadline:
            value = self.snapshot()
            need(set(value["current"]).issubset(set(expected) | set(completed)), "overload_unexpected_original_lease")
            if set(value["current"]) == set(expected) and set(completed).issubset(value["completed"]):
                for identity in expected | set(completed):
                    lease = value["current"].get(identity) or value["completed"].get(identity)
                    need(lease["ItemId"] == self.b["source"]["item_id"] and lease["MediaSourceId"] == self.b["source"]["media_source_id"], "overload_lease_source_changed")
                return value
            time.sleep(.02)
        raise WORK.Failure("overload_lease_lifecycle_deadline")

    def admission(self):
        for key in ("run_id", "owner_id", "source_revision", "tier"):
            need(self.b[key] == self.m[key] and self.f[key] == self.m[key], "overload_input_scope")
        need(self.b["profile_id"] == self.m["profile_id"] and self.c["manifest_sha256"] == self.b["manifest"]["sha256"], "overload_profile_binding")
        need(self.c["driver_sha256"] == digest((HERE / "media-analysis-phase3-workload.py").read_bytes()), "overload_driver_changed")
        need(self.f["prepared"] is True and self.f["sentinel"]["user_id"] != self.c["user_id"], "overload_distinct_existing_viewers")
        for peer, expected_user in ((False, self.c["user_id"]), (True, self.f["sentinel"]["user_id"])):
            credential = self.credential(peer)
            need(all(type(value) is str and 0 < len(value) <= 4096 and not any(character in value for character in "\r\n\x00")
                     for value in credential.values()), "overload_credential_shape")
            actual = self.actor.http("overload-viewer-identity", "GET", "/emby/Users/Me", headers={"X-Emby-Token": credential["token"],
                "X-Emby-Authorization": 'MediaBrowser Client="Phase3Overload", Device="Linux", DeviceId="%s", Version="1"' % credential["device_id"]})[0]
            need(actual.get("Id") == expected_user and actual.get("Policy", {}).get("IsAdministrator") is False, "overload_credential_user_mismatch")
        source = self.b["source"]
        root = next((root for root in self.c["roots"] if root["id"] == source["root_id"]), None)
        need(root is not None and root["library_id"] == source["library_id"], "overload_source_root_binding")
        path = self.actor.owned_media(source["path"])
        relative = path.relative_to(root["path"]).as_posix()
        inventory = pinned({"path": self.c["inventory_path"], "sha256": self.c["inventory_sha256"]}, 128 << 20)
        matches = []
        for raw in inventory.splitlines():
            row = strict_json(raw)
            if row.get("root_id") == source["root_id"] and row.get("relative_path") == relative:
                matches.append(row)
        need(len(matches) == 1 and matches[0]["origin"] == "licensed" and matches[0]["sha256"] == source["sha256"]
             and matches[0]["bytes"] == source["bytes"], "overload_source_not_frozen_licensed_media")
        self.source_fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        self.source_before = os.fstat(self.source_fd)
        need(stat.S_ISREG(self.source_before.st_mode) and self.source_before.st_size == source["bytes"], "overload_source_stat")
        hasher = hashlib.sha256()
        remaining = source["bytes"]
        while remaining:
            self.actor.remaining()
            block = os.read(self.source_fd, min(1 << 20, remaining))
            need(block, "overload_source_shortened")
            hasher.update(block)
            remaining -= len(block)
        need(not os.read(self.source_fd, 1) and hasher.hexdigest() == source["sha256"], "overload_source_digest")
        self.verify_source()
        self.prefix = os.pread(self.source_fd, PREFIX_BYTES, 0)
        item = self.actor.sql("SELECT json_build_object('id',id,'library_id',library_id,'root_id',root_id,'path',path,'size',file_size,'file_identity',file_identity,'media',media) FROM items WHERE id=" + WORK.sql_string(source["item_id"]))
        need(type(item) is dict and all(item[key] == source[key] for key in ("library_id", "root_id", "path"))
             and item["size"] == source["bytes"] and item["media"]["Size"] == source["bytes"] and item["file_identity"], "overload_catalog_source_binding")
        detail = self.actor.native("/admin/v1/media-analysis/items/" + source["item_id"])
        need(detail["Id"] == source["item_id"] and detail["MediaSourceId"] == source["media_source_id"], "overload_media_source_binding")
        self.e.artifact("source-binding.json", json_bytes({"source": source, "stat": source_stamp(self.source_before), "catalog": item,
                                                          "source_revision": detail["SourceRevision"]}))
        self.baseline = self.snapshot()
        need(not self.baseline["current"], "overload_requires_idle_original_runtime")
        self.proof["baseline_instance"] = self.baseline["instance_id"]
        # The independently privileged observer validates normal native process
        # identity and deployment before any intentional socket pressure.
        self.actor.process_sample()

    def verify_source(self):
        need(source_stamp(os.fstat(self.source_fd)) == source_stamp(self.source_before)
             == source_stamp(os.stat(self.b["source"]["path"], follow_symlinks=False)), "overload_source_changed")

    def credential(self, peer=False):
        if peer:
            return {"token": self.f["sentinel"]["token"], "device_id": self.f["sentinel"]["device_id"]}
        return {"token": self.c["emby_token"], "device_id": self.c["device_id"]}

    def open_slow(self, label, expected, peer=False):
        need(time.monotonic() < self.active_deadline, "overload_active_deadline")
        before = self.snapshot()
        need(set(before["current"]) == expected, "overload_prior_slots_changed")
        response = SlowResponse(self.actor, self.b["source"], self.credential(peer), label,
            min(self.active_deadline, time.monotonic() + self.b["limits"]["request_seconds"]))
        self.opened.append(response)
        response.open()
        need(response.prefix == self.prefix, "overload_original_source_bytes")
        after = self.snapshot()
        added = set(after["current"]) - expected
        need(set(after["current"]).issuperset(expected) and len(added) == 1 and after["next_lease"] == before["next_lease"] + 1, "overload_open_not_one_live_lease")
        identity = added.pop()
        lease = after["current"][identity]
        need(lease["ItemId"] == self.b["source"]["item_id"] and lease["MediaSourceId"] == self.b["source"]["media_source_id"], "overload_open_wrong_source")
        response.lease_id = identity
        self.known_leases.add(identity)
        self.e.artifact("slow-original.json", json_bytes(response.receipt()))
        self.e.event("slow_original_open", label=label, peer=peer, start_ns=response.started_ns, end_ns=response.prefix_ns,
                     lease_reference=digest(identity.encode()), prefix_bytes=len(response.prefix))
        return response

    def execute_pressure(self):
        self.actor.phase = "overload"
        self.active_deadline = time.monotonic() + self.b["limits"]["active_seconds"]
        self.actor.deadline = self.active_deadline
        expected = set()
        owners = []
        for index in range(OWNER_LIMIT):
            opened = self.open_slow("owner-%d" % (index + 1), expected)
            owners.append(opened)
            expected.add(opened.lease_id)
        full = self.expect_current(expected)
        need(len(full["current"]) == OWNER_LIMIT and all(opened.socket.fileno() >= 0 and opened.closed_ns is None for opened in owners), "overload_eight_actual_streams_missing")
        response, headers, span, _ = self.actor.http("ninth-original", "GET", "/emby/Videos/" + self.b["source"]["item_id"] + "/original.mp4?" + urlencode({"MediaSourceId": self.b["source"]["media_source_id"]}), statuses=(429,))
        need(type(response) is dict and response.get("ResponseStatus", {}).get("ErrorCode") == "stream_limit"
             and response["ResponseStatus"].get("Message") == "The authenticated owner has reached its active media stream limit."
             and headers.get("Retry-After") == "2" and headers.get("Content-Type", "").startswith("application/json")
             and not headers.get("ETag") and not headers.get("Content-Range"), "overload_rejection_contract")
        reject_ms = (span[1] - span[0]) / 1e6
        need(reject_ms <= self.b["limits"]["max_reject_ms"], "overload_rejection_latency")
        after_reject = self.expect_current(expected)
        need(after_reject["next_lease"] == full["next_lease"], "overload_rejection_admitted_lease")
        self.proof.update(owner_limit_observed=OWNER_LIMIT, ninth_status=429, ninth_code="stream_limit", owner_specific_error_message=True,
                          retry_after="2", rejection_ms=reject_ms)
        peer = self.open_slow("other-viewer", expected, peer=True)
        peer_expected = expected | {peer.lease_id}
        self.expect_current(peer_expected)
        peer.close()
        # The completed lease is actual leave evidence; local close alone is
        # never sufficient to assume the protected source was retired.
        self.expect_current(expected, {peer.lease_id})
        removed = owners[0]
        removed.close()
        expected.remove(removed.lease_id)
        self.expect_current(expected, {peer.lease_id, removed.lease_id})
        recovering = self.open_slow("owner-after-release", expected)
        recover_ms = (recovering.prefix_ns - recovering.started_ns) / 1e6
        need(recover_ms <= self.b["limits"]["max_recover_ms"], "overload_recovery_latency")
        expected.add(recovering.lease_id)
        self.expect_current(expected)
        need(len(self.known_leases) == OWNER_LIMIT + 2 and time.monotonic() <= self.active_deadline, "overload_exchange_budget_or_lease_count")
        self.proof.update(other_viewer_status=peer.status, recovery_status=recovering.status, recovery_ms=recover_ms,
                          source_prefix_sha256=digest(self.prefix), source_bound_prefix_bytes=PREFIX_BYTES)

    def cleanup(self):
        self.actor.phase = "overload-cleanup"
        self.actor.cleanup_mode = True
        self.e.closing = True
        self.actor.deadline = time.monotonic() + self.b["limits"]["cleanup_seconds"]
        errors = []
        for opened in self.opened:
            try:
                opened.close()
                self.e.artifact("closed-original.json", json_bytes(opened.receipt()))
            except Exception as error:
                errors.append(str(error) if isinstance(error, WORK.Failure) else "overload_socket_close")
        if self.baseline is not None:
            try:
                value = self.expect_current(set(self.baseline["current"]), self.known_leases)
                self.proof["all_known_leases_completed"] = self.known_leases.issubset(value["completed"])
                self.proof["returned_to_original_baseline"] = set(value["current"]) == set(self.baseline["current"])
                need(value["next_lease"] == self.baseline["next_lease"] + len(self.known_leases), "overload_unaccounted_original_admission")
            except Exception as error:
                errors.append(str(error) if isinstance(error, WORK.Failure) else "overload_lease_cleanup")
        if self.source_fd is not None:
            try:
                self.verify_source()
            except Exception as error:
                errors.append(str(error) if isinstance(error, WORK.Failure) else "overload_source_cleanup")
            os.close(self.source_fd)
            self.source_fd = None
        self.proof["local_tcp_closed"] = all(opened.socket is None or opened.socket.fileno() == -1 for opened in self.opened)
        return errors

    def run(self):
        complete = False
        try:
            self.admission()
            self.execute_pressure()
            complete = True
        except Exception as error:
            self.errors.append(str(error) if isinstance(error, WORK.Failure) else "overload_failure")
        finally:
            closure_errors = self.cleanup()
        accepted = complete and not self.errors and not closure_errors and self.proof.get("all_known_leases_completed") is True
        result = {key: self.b[key] for key in ("schema_version", "run_id", "owner_id", "source_revision", "tier", "profile_id", "limits")}
        result.update({"accepted": accepted, "capacity_accepted": False, "decoding_accepted": False, "throughput_accepted": False,
            "scope": "Production original-response per-user quota, independent viewer allowance, slot return and TCP/lease cleanup only.",
            "proof": self.proof, "failure_codes": self.errors, "cleanup_errors": closure_errors,
            "binding_sha256": digest(json_bytes(self.b)), "binding_hash_representation": "canonical json_bytes",
            "source_sha256": self.b["source"]["sha256"], "source_bytes": self.b["source"]["bytes"],
            "media_generated_or_sparse_extended": False, "external_process_resource_closure_still_required": True})
        self.e.atomic("overload-result.json", result)
        return accepted


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binding", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args(argv)
    need(sys.platform == "linux" and os.getuid() != 0, "overload_remote_unprivileged_linux_required")
    os.umask(0o077)
    raw = WORK.read_private(args.binding, 2 << 20)
    binding = validate_binding(strict_json(raw))
    manifest = WORK.validate_manifest(strict_json(pinned(binding["manifest"], 1 << 20)))
    context = strict_json(pinned(binding["context"], 16 << 20))
    fault = strict_json(pinned(binding["fault_fixture"], 16 << 20))
    bounded = copy.deepcopy(manifest)
    bounded["budgets"].update(max_artifact_bytes=binding["limits"]["max_output_bytes"], max_events=2048)
    evidence = WORK.Evidence(args.output, bounded)
    evidence.artifact("overload-binding.json", raw)
    return 0 if Overload(binding, manifest, context, fault, evidence).run() else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"accepted": False, "error_code": str(error) if isinstance(error, WORK.Failure) else "overload_admission_failure"}))
        sys.exit(1)
