"""Evidence and request contracts with temporary artifacts and no live faults."""
import copy
import importlib.util
from pathlib import Path
import sys
import tempfile
import threading
import time
import types
import unittest
from unittest import mock

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


@unittest.skipUnless(sys.platform == "linux", "Immutable intent contracts require Linux filesystem primitives")
class NativeRequestContracts(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        def load(name, filename):
            module_spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
            module = importlib.util.module_from_spec(module_spec)
            module_spec.loader.exec_module(module)
            return module

        cls.oracle_module = load("oracle_native_contract", "media-analysis-phase3-oracle.py")
        cls.guest_module = load("oracle_guest_contract", "media-analysis-phase3-recovery-guest.py")

    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="goby-oracle-contract-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.fixture_number = 0
        self.requests = []
        self.binding_sha256 = "a" * 64
        self.binding = {"run_id": "shared-run", "owner_id": "test-owner", "volumes": {"media": {}}}

        # Fixture construction avoids infrastructure discovery. Guest.run,
        # scenario validation and exclusive, fsynced intent writes stay real.
        self.native_guest = self.guest_module.Guest.__new__(self.guest_module.Guest)
        self.native_guest.binding = self.binding
        self.native_guest.binding_sha256 = self.binding_sha256
        self.native_guest.root = self.root / "guest-owned"
        self.native_guest.operations = self.native_guest.root / "recovery-operations"
        self.native_guest.operations.mkdir(parents=True, mode=0o700)
        self.native_guest.guard = mock.Mock()
        self.native_guest.release = mock.Mock()
        self.native_guest.inject = mock.Mock(side_effect=lambda scenario: {"scenario_id": scenario["scenario_id"]})
        self.native_guest.recover = mock.Mock(side_effect=lambda scenario, injection: {"recovered": True})

        def call(role, request, timeout):
            self.assertEqual(role, "guest")
            self.assertGreater(timeout, 0)
            wire_request = self.guest_module.decode(self.oracle_module.canonical(request))
            self.requests.append(wire_request)
            return self.native_guest.run(wire_request)

        self.transport = types.SimpleNamespace(call=mock.Mock(side_effect=call))

    def make_oracle(self, case_id="case-a", context_sha256="b" * 64,
                    request_id="recover-0001", artifacts_root=None):
        self.fixture_number += 1
        root = artifacts_root or self.root / ("external-" + str(self.fixture_number))
        root.mkdir(mode=0o700, exist_ok=True)
        value = self.oracle_module.Oracle.__new__(self.oracle_module.Oracle)
        value.c = {"run_id": self.binding["run_id"], "owner_id": self.binding["owner_id"],
                   "source_revision": "c" * 40,
                   "transport": {"remote": {"guest": {"binding": {"sha256": self.binding_sha256}}}},
                   "budgets": {"external_bytes": 16 << 20, "external_files": 1024, "free_floor_bytes": 0}}
        value.q = {"request_id": request_id, "context_sha256": context_sha256, "operation": "recover", "payload": {}}
        value.serial = 0
        value.io_lock = threading.RLock()
        value.case_id = case_id
        value.scenario = {"scenario_id": case_id, "fault": "blocked_read", "volume_id": "media",
                          "replacement_volume_id": None, "relative_path": "source.mp4", "late_mount": False,
                          "restart_goby_after": False, "require_interrupted_jobs": False}
        value.root = root
        value.state_path = root / "oracle-state.json"
        value.saved = (self.oracle_module.decode(self.oracle_module.read_file(value.state_path))
                       if value.state_path.exists() else {"version": 1, "scenario_id": case_id, "records": []})
        value.record_refs = []
        value.deadline = time.monotonic() + 120
        value.transport = self.transport
        value.modules = {}
        return value

    def operation_bytes(self):
        return {path.name: path.read_bytes() for path in self.native_guest.operations.iterdir()}

    def test_cases_share_guest_operations_without_native_intent_collision(self):
        first = self.make_oracle(case_id="t10-01-block-read")
        second = self.make_oracle(case_id="t10-02-block-meta")
        second.scenario["fault"] = "blocked_metadata"
        self.assertEqual((first.c["run_id"], first.q, first.serial),
                         (second.c["run_id"], second.q, second.serial))
        first.guest("inject")
        previous = self.operation_bytes()
        second.guest("inject")

        self.assertNotEqual(self.requests[0]["request_id"], self.requests[1]["request_id"])
        intents = sorted(self.native_guest.operations.glob("*-intent.json"))
        self.assertEqual(len(intents), 2)
        self.assertCountEqual([self.guest_module.decode(path.read_bytes()) for path in intents], self.requests)
        expected_fields = {"schema_version", "request_id", "op", "run_id", "owner_id", "source_revision",
                           "binding_sha256", "scenario", "payload"}
        for request in self.requests:
            self.assertEqual(set(request), expected_fields)
            self.assertRegex(request["request_id"], r"oracle-[0-9a-f]{40}\Z")
        for name, raw in previous.items():
            self.assertEqual((self.native_guest.operations / name).read_bytes(), raw)
        self.assertEqual(len(list(self.native_guest.operations.glob("*-result.json"))), 2)
        self.assertEqual(self.native_guest.inject.call_count, 2)
        self.assertEqual((len(first.record_refs), len(second.record_refs)), (1, 1))

    def test_reconstructed_request_hits_real_exclusive_intent_guard(self):
        first = self.make_oracle()
        first.guest("inject")
        first.persist()
        original_operations = self.operation_bytes()
        original_state = first.state_path.read_bytes()

        reconstructed = self.make_oracle(artifacts_root=first.root)
        with self.assertRaises(FileExistsError):
            reconstructed.guest("inject")
        self.assertEqual(self.requests[0]["request_id"], self.requests[1]["request_id"])
        self.assertEqual(self.operation_bytes(), original_operations)
        self.assertEqual(first.state_path.read_bytes(), original_state)
        self.native_guest.inject.assert_called_once()
        self.assertEqual(reconstructed.record_refs, [])

    def test_refrozen_context_cannot_retry_persisted_oracle_mutation(self):
        first = self.make_oracle()
        first.saved["injection"] = {"mechanism": "fixture"}
        self.assertEqual(first.dispatch(), {"recovered": True, "dispatches": 1})
        first.persist()
        original_state = first.state_path.read_bytes()
        original_operations = self.operation_bytes()
        self.assertEqual(first.saved["mutation_intents"]["recover"]["request_id"], first.q["request_id"])

        for context_sha256 in ("b" * 64, "d" * 64):
            with self.subTest(context_sha256=context_sha256):
                first.q["context_sha256"] = context_sha256
                with self.assertRaisesRegex(self.oracle_module.Refusal, "mutation_already_attempted_no_automatic_retry"):
                    first.dispatch()
        reconstructed = self.make_oracle(context_sha256="d" * 64, request_id="recover-0002", artifacts_root=first.root)
        with self.assertRaisesRegex(self.oracle_module.Refusal, "mutation_already_attempted_no_automatic_retry"):
            reconstructed.dispatch()
        self.transport.call.assert_called_once()
        self.native_guest.recover.assert_called_once()
        self.assertEqual(first.state_path.read_bytes(), original_state)
        self.assertEqual(self.operation_bytes(), original_operations)

    def test_native_request_namespace_changes_each_scope_dimension(self):
        baseline = self.make_oracle().native_id("guest", "inject")
        self.assertEqual(self.make_oracle().native_id("guest", "inject"), baseline)
        variants = (
            ("scenario", {"case_id": "case-b"}, "guest", "inject", 0),
            ("context", {"context_sha256": "d" * 64}, "guest", "inject", 0),
            ("parent", {"request_id": "recover-0002"}, "guest", "inject", 0),
            ("role", {}, "state", "inject", 0),
            ("operation", {}, "guest", "recover", 0),
            ("serial", {}, "guest", "inject", 1),
        )
        identifiers = {baseline}
        for dimension, arguments, role, operation, serial in variants:
            with self.subTest(dimension=dimension):
                candidate = self.make_oracle(**arguments)
                candidate.serial = serial
                identifier = candidate.native_id(role, operation)
                self.assertRegex(identifier, r"oracle-[0-9a-f]{40}\Z")
                self.assertNotIn(identifier, identifiers)
                identifiers.add(identifier)


if __name__ == "__main__":
    unittest.main()
