#!/usr/bin/env python3
"""Pure Phase 3 admission/accounting tests; run only in remote verification.

These tests do not exercise Goby and cannot establish capacity, media correctness
or recovery acceptance. In particular, no synthetic event is an acceptance
result. Importing the actor must perform no work.
"""

import copy
import importlib.util
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("phase3_workload", ROOT / "media-analysis-phase3-workload.py")
ACTOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ACTOR)


class ManifestTests(unittest.TestCase):
    def setUp(self):
        self.profile = ACTOR.strict_json((ROOT / "media-analysis-phase3-workload-profiles.example.json").read_bytes())["profiles"][0]

    def test_candidate_cannot_be_executed(self):
        with self.assertRaisesRegex(ACTOR.Failure, "profile_not_admitted"):
            ACTOR.validate_manifest(self.profile)

    def test_frozen_profile_requires_closed_fields_and_explicit_spool(self):
        self.profile["admission"] = "approved"
        ACTOR.validate_manifest(self.profile)
        changed = copy.deepcopy(self.profile)
        changed["undeclared_threshold"] = 1
        with self.assertRaisesRegex(ACTOR.Failure, "manifest_fields"):
            ACTOR.validate_manifest(changed)
        changed = copy.deepcopy(self.profile)
        changed["scan_evidence"]["enabled"] = False
        with self.assertRaisesRegex(ACTOR.Failure, "scan_evidence_required"):
            ACTOR.validate_manifest(changed)

    def test_integer_limits_do_not_accept_float_or_bool(self):
        self.profile["admission"] = "approved"
        for value in (1000.0, True, 255):
            changed = copy.deepcopy(self.profile)
            changed["budgets"]["max_requests"] = value
            with self.subTest(value=value), self.assertRaises(ACTOR.Failure):
                ACTOR.validate_manifest(changed)

    def test_cannot_inflate_playback_concurrency_with_reused_scopes(self):
        self.profile["admission"] = "approved"
        self.profile["concurrency"]["playback_workers"] = 12
        with self.assertRaisesRegex(ACTOR.Failure, "playback_workers"):
            ACTOR.validate_manifest(self.profile)

    def test_strict_json_rejects_ambiguous_or_lossy_input(self):
        for data in (b'{"a":1,"a":2}', b'{"a":NaN}', b'{"a":"\\ud800"}', b'{"a":"\xff"}'):
            with self.subTest(data=data), self.assertRaises(ACTOR.Failure):
                ACTOR.strict_json(data)
        self.assertEqual(ACTOR.strict_json(b'{"a":"\\ud83d\\ude00"}'), {"a": "\U0001f600"})


class OverlapTests(unittest.TestCase):
    def test_queued_terminal_and_one_sample_are_not_running_intervals(self):
        self.assertEqual(ACTOR.confirmed_intervals([(1, "a", False), (2, "a", True), (3, "a", False)], 10), [])
        self.assertEqual(ACTOR.confirmed_intervals([(2, "a", True)], 10), [])

    def test_identity_changes_and_measurement_gaps_do_not_bridge_work(self):
        samples = [(0, "a", True), (1, "a", True), (2, "b", True), (99, "b", True), (100, "b", True)]
        self.assertEqual(ACTOR.confirmed_intervals(samples, 5), [(0, 1), (99, 100)])

    def test_pairwise_overlap_does_not_imply_compound_overlap(self):
        # Every pair overlaps, but no instant belongs to all three unions.
        groups = [[(0, 4)], [(2, 6)], [(0, 2), (4, 6)]]
        self.assertEqual(ACTOR.all_overlap(groups), 0)
        self.assertGreater(ACTOR.all_overlap(groups[:2]), 0)
        self.assertGreater(ACTOR.all_overlap([groups[0], groups[2]]), 0)
        self.assertGreater(ACTOR.all_overlap(groups[1:]), 0)

    def test_duplicates_and_overlapping_requests_do_not_double_count_time(self):
        self.assertEqual(ACTOR.all_overlap([[(0, 10), (1, 8), (0, 10)], [(2, 4), (3, 7)]]), 5)
        self.assertEqual(ACTOR.union_length([(0, 10), (1, 8), (0, 10)]), 10)

    def test_sparse_observations_remain_finite_and_exact(self):
        intervals = [(index * 4, index * 4 + 2) for index in range(10000)]
        self.assertEqual(ACTOR.all_overlap([intervals, [(1, 40000)], intervals]), 19999)

    def test_no_samples_is_not_zero_latency(self):
        self.assertEqual(ACTOR.distribution([]), {"count": 0, "min": None, "p50": None, "p95": None, "p99": None, "max": None})
        self.assertEqual(ACTOR.percentile(list(range(1, 101)), .95), 95)

    def test_analysis_process_kinds_are_independent_from_parent_run_state(self):
        self.assertEqual(ACTOR.media_process_kind(["ffmpeg", "-f", "s16le"]), "intro")
        self.assertEqual(ACTOR.media_process_kind(["ffmpeg", "-vf", "showinfo@analysis_source,trim=end=15,select=x"]), "intro")
        self.assertEqual(ACTOR.media_process_kind(["ffmpeg", "-vf", "showinfo@analysis_source,select=x"]), "previews")
        self.assertEqual(ACTOR.media_process_kind(["ffmpeg", "-c:v", "copy"]), "playback")


class PrivilegedObserverBoundaryTests(unittest.TestCase):
    def reference(self, suffix="process-observer.json"):
        return {"argv": ["/usr/bin/sudo", "-n", "/usr/bin/python3.13", "-I", "-B",
                         "/opt/goby-phase3-runtime-setup-20260922-01/process-observer.py", "--binding",
                         "/var/lib/goby-phase3/tier10k/control/" + suffix, "--binding-sha256", "a" * 64],
                "binding_sha256": "a" * 64, "source_sha256": "b" * 64}

    def test_initial_and_case_specific_frozen_bindings_are_allowed(self):
        for suffix in ("process-observer.json", "cases/pg-restart-01/process-observer.json"):
            with self.subTest(suffix=suffix):
                value = self.reference(suffix)
                self.assertEqual(ACTOR.process_observer_arguments(value), value["argv"])

    def test_privileged_path_traversal_and_command_substitution_are_rejected(self):
        for suffix in ("../process-observer.json", "cases/../process-observer.json", "cases/a/b/process-observer.json"):
            with self.subTest(suffix=suffix), self.assertRaises(ACTOR.Failure):
                ACTOR.process_observer_arguments(self.reference(suffix))
        for index, replacement in ((2, "/bin/sh"), (3, "-c"), (5, "/tmp/process-observer.py"), (9, "c" * 64)):
            value = self.reference()
            value["argv"][index] = replacement
            with self.subTest(index=index), self.assertRaises(ACTOR.Failure):
                ACTOR.process_observer_arguments(value)


class DatabasePoolAccountingTests(unittest.TestCase):
    def values(self, count="9007199254740993", duration="9007199254741001"):
        return {"MaxConns": 16, "TotalConns": 5, "IdleConns": 2, "AcquiredConns": 3, "ConstructingConns": 0,
                "AcquireCount": count, "AcquireDurationNanoseconds": duration, "EmptyAcquireCount": "2",
                "EmptyAcquireWaitNanoseconds": "100000000", "CanceledAcquireCount": "1"}

    def test_decimal_counters_retain_precision_above_javascript_integer_range(self):
        before = ACTOR.database_pool_values(self.values())
        after = ACTOR.database_pool_values(self.values("9007199254740996", "9007199254741010"))
        result = ACTOR.database_pool_summary([{"at_ns": 1, "values": before}, {"at_ns": 11, "values": after}])
        self.assertEqual(result["delta"]["AcquireCount"], "3")
        self.assertEqual(result["delta"]["AcquireDurationNanoseconds"], "9")
        self.assertEqual(result["peaks"]["AcquiredConns"], 3)

    def test_counter_reset_is_not_a_negative_or_zero_wait_success(self):
        with self.assertRaisesRegex(ACTOR.Failure, "database_pool_counter_reset"):
            ACTOR.database_pool_summary([{"at_ns": 1, "values": ACTOR.database_pool_values(self.values("10"))},
                                         {"at_ns": 2, "values": ACTOR.database_pool_values(self.values("9"))}])
        self.assertEqual(ACTOR.database_pool_summary([])["available"], False)
        self.assertIsNone(ACTOR.database_pool_summary([])["delta"])

    def test_pool_wire_shape_and_gauges_fail_closed(self):
        for invalid in (1, 1.0, True, "01", "-1", "1e4", "9223372036854775808"):
            value = self.values()
            value["AcquireCount"] = invalid
            with self.subTest(invalid=invalid), self.assertRaises(ACTOR.Failure):
                ACTOR.database_pool_values(value)
        value = self.values()
        value["AcquiredConns"] = 6
        with self.assertRaisesRegex(ACTOR.Failure, "database_pool_snapshot_bounds"):
            ACTOR.database_pool_values(value)


if __name__ == "__main__":
    unittest.main()
