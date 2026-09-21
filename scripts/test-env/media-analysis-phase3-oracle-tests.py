"""Pure evidence contract tests. No live HTTP, SQL, process or fault access."""
import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("oracle_evidence", Path(__file__).with_name("media-analysis-phase3-oracle-evidence.py"))
evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(evidence)


class EvidenceContracts(unittest.TestCase):
    def blocked(self):
        fd = {"fd": 12, "target": "/fault/source.mp4", "mount_id": 70, "inode": 91, "flags": "0100000"}
        task = {"pid": 300, "start_ticks": 400, "tid": 301, "task_start_ticks": 401, "state": "D",
                "syscall": "0 0xc 0x100 0x200 0x0 0x0 0x0 0x300 0x400", "wchan": "io_schedule",
                "architecture": "x86_64", "syscall_first_fd_target": fd["target"], "cgroup": "/goby.service"}
        first = {"goby": {"pid": 300, "start_ticks": 400}, "observed_unix_ns": 3_000_000_000,
                 "blocked_tasks": [task], "task_inventory": [task], "fd_inventory": [fd],
                 "mounts": [{"mount_id": 70, "major_minor": "253:1"}]}
        last = copy.deepcopy(first)
        last["observed_unix_ns"] += 1_000_000_000
        affected = {"outcome": "timeout", "http_method": "GET", "http_status": 0,
                    "http_started_unix_ns": 1_000_000_000, "http_completed_unix_ns": 2_000_000_000,
                    "source_path": fd["target"], "request_sha256": "a" * 64}
        return first, last, affected, {"mountpoint": "/fault", "major_minor": "253:1"}

    def test_bounded_503_is_a_get_boundary_not_an_admission(self):
        first, last, affected, volume = self.blocked()
        affected.update(outcome="bounded_503", http_status=503)
        witness, descriptor = evidence.blocked_witness(first, last, affected, volume, "/goby.service", "read")
        self.assertEqual((witness["tid"], descriptor["inode"]), (301, 91))
        affected.update(http_method="POST", http_status=202)
        with self.assertRaises(evidence.EvidenceError):
            evidence.blocked_witness(first, last, affected, volume, "/goby.service", "read")

    def test_other_device_cannot_borrow_d_state(self):
        first, last, affected, volume = self.blocked()
        last["mounts"][0]["major_minor"] = "253:2"
        with self.assertRaises(evidence.EvidenceError):
            evidence.blocked_witness(first, last, affected, volume, "/goby.service", "read")

    def completion(self, descriptor):
        scan = {key: 0 for key in ("ActivePasses", "RetiringPasses", "CleanupFailures", "ReservedBytes", "ReservedFileDescriptors")}
        return {"kind": "native_storage_observations_drained", "evidence_fd": descriptor,
                "source_path": descriptor["target"], "completed_unix_ns": 5_000_000_000, "terminal_state": "completed",
                "evidence_sha256": "b" * 64, "source_admission_id": "get-1", "operation_id": "get-1",
                "before": {"StorageObservations": {"Active": 1}},
                "after": {"StorageObservations": {"Active": 0}, "ScanEvidence": scan},
                "admissions_stopped": {"sha256": "c" * 64}, "temporary_source_handles": [],
                "spool_generation_handles": [], "owner_backend_references": []}

    def test_thread_absence_without_resource_release_fails(self):
        first, last, affected, volume = self.blocked()
        witness, descriptor = evidence.blocked_witness(first, last, affected, volume, "/goby.service", "read")
        after = copy.deepcopy(last)
        after.update(observed_unix_ns=6_000_000_000, task_inventory=[], fd_inventory=[])
        completion = self.completion(descriptor)
        completion["after"]["StorageObservations"]["Active"] = 1
        with self.assertRaises(evidence.EvidenceError):
            evidence.retired_worker(witness, descriptor, after, completion)

    def test_only_unchanged_prior_anchor_can_remain(self):
        first, last, affected, volume = self.blocked()
        anchor = {**first["fd_inventory"][0], "fd": 8}
        first["fd_inventory"].append(anchor)
        last["fd_inventory"].append(copy.deepcopy(anchor))
        baseline = copy.deepcopy(first)
        witness, descriptor = evidence.blocked_witness(first, last, affected, volume, "/goby.service", "read")
        after = copy.deepcopy(last)
        after.update(observed_unix_ns=6_000_000_000, task_inventory=[], fd_inventory=[anchor])
        self.assertTrue(evidence.retired_worker(witness, descriptor, after, self.completion(descriptor), baseline,
                                               [first, last])["worker_released"])
        after["fd_inventory"][0] = {**anchor, "fd": 9}
        with self.assertRaises(evidence.EvidenceError):
            evidence.retired_worker(witness, descriptor, after, self.completion(descriptor), baseline, [first, last])

    def test_target_lease_can_survive_historical_ring_drops(self):
        active = {"LeaseId": "lease-4", "Sequence": "4", "CompletionSequence": "0", "ItemId": "item",
                  "MediaSourceId": "source", "StartedUnixNano": "100", "CompletedUnixNano": "0", "Active": True}
        before = {"OriginalStreams": {"InstanceId": "instance", "ActiveCount": 1, "CurrentCapacityDropped": 0,
                  "CompletionCapacityDropped": "10", "Current": [active], "Completed": []}}
        done = {**active, "Active": False, "CompletionSequence": "150", "CompletedUnixNano": "400"}
        after = {"OriginalStreams": {"InstanceId": "instance", "ActiveCount": 0, "CurrentCapacityDropped": 0,
                 "CompletionCapacityDropped": "22", "Current": [], "Completed": [done],
                 "OldestCompletionSequence": "23", "NextCompletionSequence": "151"}}
        self.assertEqual(evidence.original_lease_completion(before, after, "item", "source", 90, 300), done)
        after["OriginalStreams"]["Completed"] = []
        with self.assertRaises(evidence.EvidenceError):
            evidence.original_lease_completion(before, after, "item", "source", 90, 300)

    def test_http_failure_cannot_invent_postgres_sqlstate(self):
        owner = {"pid": 44, "backend_start": "2026-09-22T00:00:00+00:00"}
        waiting = {"backends": [{**owner, "wait_event_type": "Lock", "blocking_pids": [45], "query_sha256": "a" * 64}],
                   "observed_unix_ns": 1790035201000000000}
        after = {"owner_backend": owner, "backends": [{**owner, "state": "idle", "xact_start": None, "blocking_pids": []}],
                 "observed_unix_ns": 1790035205000000000}
        with self.assertRaises(evidence.EvidenceError):
            evidence.postgres_lock_facts({"owner_backend": owner}, waiting, after, [], {"catalog": "a"}, {"catalog": "a"},
                                         {"blocker_backend_pids": [45]})


if __name__ == "__main__":
    unittest.main()
