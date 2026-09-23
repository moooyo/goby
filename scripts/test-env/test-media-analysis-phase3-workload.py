#!/usr/bin/env python3
"""Pure Phase 3 admission/accounting tests; run only in remote verification.

These tests do not exercise Goby and cannot establish capacity, media correctness
or recovery acceptance. In particular, no synthetic event is an acceptance
result. Importing the actor must perform no work.
"""

import copy
import errno
import importlib.util
import io
from pathlib import Path
import threading
import unittest
from unittest import mock


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


class ServiceCPUAccountingTests(unittest.TestCase):
    @staticmethod
    def sample(usage, at):
        return {"cpu_usage_usec": usage, "cpu_sample_start_ns": at - 1,
                "cpu_sample_end_ns": at + 1, "cpu_sample_at_ns": at}

    def test_counter_read_is_bracketed_before_parsing_or_other_resource_reads(self):
        order = []
        clock = iter((101, 106))

        def now():
            order.append("clock")
            return next(clock)

        def read(path):
            order.append("read:" + path.name)
            return "usage_usec 9007199254740993\nuser_usec 7\nsystem_usec 8\n"

        with mock.patch.object(ACTOR.time, "monotonic_ns", side_effect=now), \
                mock.patch.object(ACTOR.Path, "read_text", autospec=True, side_effect=read):
            sample = ACTOR.cpu_usage_sample(Path("/sys/fs/cgroup/goby"))
        self.assertEqual(order, ["clock", "read:cpu.stat", "clock"])
        self.assertEqual(sample, {"cpu_usage_usec": 9007199254740993,
            "cpu_sample_start_ns": 101, "cpu_sample_end_ns": 106, "cpu_sample_at_ns": 103})

    def test_variable_broker_delay_does_not_manufacture_the_reported_cpu_spike(self):
        # The counters/broker clocks reproduce the retained failed run. Counter
        # read times below are modeled observations, not recovered historical
        # evidence and not a reclassification of that run's 222.7% failure.
        broker_times = (4057588696721, 4057967384431)
        broker_ends = (4057624296217, 4058095187338)
        counters = {"goby": [419829372, 420260326], "postgres": [202835368, 203247803]}
        modeled_times = []
        for ended in broker_ends:
            for offset in (1000000, 2000000):
                modeled_times.extend((ended + offset, ended + offset + 200))
        actor = ACTOR.Actor.__new__(ACTOR.Actor)
        actor.c = {"cgroup_path": "/sys/fs/cgroup/goby", "postgres": {"cgroup_path": "/sys/fs/cgroup/postgres"}}
        actor.m = {"budgets": {"poll_seconds": .25}, "owner_id": "owner", "run_id": "run",
                   "profile_id": "profile", "source_revision": "revision", "tier": 10000}
        actor.assert_owned = mock.Mock()
        actor.sample_database_pool = mock.Mock()
        actor.phase, actor.procs, actor.resources, actor.failures = "cold", {}, [], []
        actor.lanes, actor.jobs, actor.lock = {}, {}, threading.RLock()
        actor.stop, actor.e = mock.Mock(), mock.Mock()
        actor.stop.is_set.side_effect = (False, False, True)
        actor.process_sample = mock.Mock(side_effect=[{
            "observed_monotonic_ns": at, "resource": {"scan_spool_generations": 1},
            "processes": [{"pid": 100, "start_ticks": 7, "arguments": ["ffmpeg", "-c:v", "copy"],
                           "source_paths": ["/owned/video.mp4"], "executable": "ffmpeg", "lineage": [],
                           "cpu_ticks": 10 + index}]} for index, at in enumerate(broker_times)])

        def read(path):
            if path.name == "cpu.stat":
                return "usage_usec %d\n" % counters[path.parent.name].pop(0)
            return "8:0 rbytes=10 wbytes=20\n" if path.name == "io.stat" else "1024\n"

        with mock.patch.object(ACTOR.time, "monotonic_ns", side_effect=modeled_times), \
                mock.patch.object(ACTOR.Path, "read_text", autospec=True, side_effect=read):
            actor.observer()
        self.assertEqual(actor.failures, [])
        self.assertEqual(len(actor.resources), 2)
        self.assertIsNone(actor.resources[0]["cpu_percent"])
        legacy = 100000 * 843389 / (broker_times[1] - broker_times[0])
        self.assertAlmostEqual(legacy, 222.7135916293666)
        self.assertAlmostEqual(actor.resources[1]["cpu_percent"], 100000 * 843389 / (broker_ends[1] - broker_ends[0]))
        self.assertLess(actor.resources[1]["cpu_percent"], 205)
        self.assertEqual(actor.resources[1]["cpu_sampling"], "per_service_read_midpoint")
        self.assertEqual(actor.resources[1]["at_ns"], broker_times[1])
        self.assertEqual(actor.procs["100:7"]["work_intervals"], [broker_times])
        for row, ended in zip(actor.resources, broker_ends):
            self.assertGreater(row["services"]["goby"]["cpu_sample_start_ns"], ended)

    def test_services_use_their_own_intervals_and_true_overload_is_not_clamped(self):
        before = {"goby": self.sample(0, 1000000000), "postgres": self.sample(0, 1000000000)}
        staggered = {"goby": self.sample(1000000, 2000000000), "postgres": self.sample(1000000, 3000000000)}
        self.assertEqual(ACTOR.service_cpu_percent(before, staggered), 150)
        overloaded = {"goby": self.sample(1100000, 2000000000), "postgres": self.sample(1000000, 2000000000)}
        self.assertEqual(ACTOR.service_cpu_percent(before, overloaded), 210)
        self.assertGreater(ACTOR.service_cpu_percent(before, overloaded), 205)
        unchanged = {"goby": self.sample(0, 2000000000), "postgres": self.sample(0, 2000000000)}
        self.assertEqual(ACTOR.service_cpu_percent(before, unchanged), 0)
        self.assertIsNone(ACTOR.service_cpu_percent(None, before))

    def test_counter_regression_and_nonpositive_time_are_failures(self):
        before = {"goby": self.sample(10, 1000000000), "postgres": self.sample(20, 1000000000)}
        current = {"goby": self.sample(11, 2000000000), "postgres": self.sample(21, 2000000000)}
        cases = [("cpu_usage_usec", 9, "cpu_counter_regressed"),
                 ("cpu_usage_usec", -1, "cpu_usage_counter"),
                 ("cpu_usage_usec", True, "cpu_usage_counter"),
                 ("cpu_usage_usec", 11.0, "cpu_usage_counter"),
                 ("cpu_sample_end_ns", 1999999998, "cpu_sample_clock"),
                 ("cpu_sample_at_ns", 2000000000.0, "cpu_sample_clock")]
        for field, value, code in cases:
            changed = copy.deepcopy(current)
            changed["goby"][field] = value
            with self.subTest(field=field, value=value), self.assertRaisesRegex(ACTOR.Failure, code):
                ACTOR.service_cpu_percent(before, changed)
        for at in (999999999, 1000000000):
            changed = {**current, "goby": self.sample(11, at)}
            with self.subTest(at=at), self.assertRaisesRegex(ACTOR.Failure, "cpu_sample_interval"):
                ACTOR.service_cpu_percent(before, changed)


class HTTPExceptionDiagnosticsTests(unittest.TestCase):
    def actor(self):
        actor = ACTOR.Actor.__new__(ACTOR.Actor)
        actor.assert_owned, actor.remaining = mock.Mock(), mock.Mock(return_value=90)
        actor.c = {"origin": "http://127.0.0.1:18108", "emby_token": "test-token", "device_id": "test-device"}
        actor.m = {"budgets": {"max_requests": 1000, "request_seconds": 90,
                              "max_stream_bytes": 4096, "max_response_bytes": 4096}}
        actor.lock, actor.requests, actor.cleanup_mode, actor.phase = threading.RLock(), 0, False, "cold"
        actor.e = mock.Mock()
        artifacts = {}

        def capture(label, data):
            artifacts[label] = data
            return {"name": label, "bytes": len(data), "sha256": ACTOR.digest(data)}

        actor.e.artifact.side_effect = capture
        return actor, artifacts

    def request(self, chunks, *, status=200):
        actor, artifacts = self.actor()
        response = mock.Mock(status=status, length=None, fp=object())
        response.getheaders.return_value = []
        response.read1.side_effect = chunks
        connection = mock.Mock()
        connection.getresponse.return_value = response
        timer = mock.Mock()
        timer.is_alive.return_value = False
        return actor, artifacts, connection, timer

    def test_positive_short_reads_remain_successful(self):
        actor, artifacts, connection, timer = self.request([b"a", b"bc", b""])
        with mock.patch.object(ACTOR.http.client, "HTTPConnection", return_value=connection), \
                mock.patch.object(ACTOR.threading, "Timer", return_value=timer):
            result = actor.http("transcode-start", "GET", "/binary", binary=True)
        self.assertEqual(result[0], b"abc")
        self.assertEqual(artifacts["body.bin"], b"abc")
        self.assertIsNone(ACTOR.strict_json(artifacts["http.json"])["exception_details"])

    def test_transport_failure_keeps_partial_body_and_only_safe_exception_fields(self):
        secret = "https://private.invalid/?api_key=credential-sentinel header-sentinel"
        errors = [(ConnectionResetError(errno.ECONNRESET, secret), "ConnectionResetError", errno.ECONNRESET),
                  (ACTOR.http.client.IncompleteRead(secret.encode(), 99), "IncompleteRead", None),
                  (ACTOR.http.client.HTTPException(secret), "HTTPException", None)]
        for error, kind, number in errors:
            actor, artifacts, connection, timer = self.request([b"a", b"bc", error])
            with self.subTest(kind=kind), \
                    mock.patch.object(ACTOR.http.client, "HTTPConnection", return_value=connection), \
                    mock.patch.object(ACTOR.threading, "Timer", return_value=timer), \
                    self.assertRaisesRegex(ACTOR.Failure, "^http_transport$"):
                actor.http("transcode-start", "GET", "/binary", binary=True)
            record = ACTOR.strict_json(artifacts["http.json"])
            details = {"exception_type": kind, "errno": number}
            self.assertEqual(record["exception_details"], details)
            self.assertEqual(record["error"], "http_transport")
            self.assertEqual(artifacts["body.bin"], b"abc")
            event = actor.e.event.call_args.kwargs
            self.assertEqual(event["exception_details"], details)
            self.assertEqual(event["error"], "http_transport")
            self.assertEqual((event["status"], event["bytes"]), (200, 3))
            self.assertIsNotNone(event["first_byte_ns"])
            self.assertNotIn(secret.encode(), artifacts["http.json"])
            self.assertNotIn("credential-sentinel", repr(event))

    def test_invalid_errno_and_unknown_exception_name_do_not_leak(self):
        for number in (None, True, "credential-sentinel", 1.5, 1 << 40):
            error = OSError("credential-sentinel")
            error.errno = number
            with self.subTest(number=number):
                self.assertEqual(ACTOR.http_exception_details(error), {"exception_type": "OSError", "errno": None})
        unknown = type("CredentialSentinel", (Exception,), {"__module__": "private_module"})
        self.assertEqual(ACTOR.http_exception_details(unknown("credential-sentinel")),
                         {"exception_type": "OtherException", "errno": None})

    def test_existing_http_status_failure_code_is_not_reclassified(self):
        actor, artifacts, connection, timer = self.request([b""], status=503)
        with mock.patch.object(ACTOR.http.client, "HTTPConnection", return_value=connection), \
                mock.patch.object(ACTOR.threading, "Timer", return_value=timer), \
                self.assertRaisesRegex(ACTOR.Failure, "^http_status$"):
            actor.http("transcode-start", "GET", "/binary", binary=True)
        self.assertEqual(ACTOR.strict_json(artifacts["http.json"])["exception_details"],
                         {"exception_type": "Failure", "errno": None})


class ConcurrentMetadataEditTests(unittest.TestCase):
    """Model both owner-transaction orders without accepting stale writes."""

    def setUp(self):
        self.actor = ACTOR.Actor.__new__(ACTOR.Actor)
        self.actor.phase = "cold"
        self.actor.e = mock.Mock()
        self.actor.http = mock.Mock(side_effect=self.http)
        self.settings = {"Revision": "7", "Overrides": {}, "Sorting": {"SortRemoveWords": []}}
        self.update = {**self.settings, "Sorting": {"SortRemoveWords": ["The"]}}
        self.initial = {"Item": {"Id": "witness", "LibraryId": "library", "ParentId": "workload",
                                 "Name": "The Witness", "Type": "Movie", "Path": "/owned/The Witness.mp4", "IsFolder": False},
                        "Revision": "1", "Automatic": {"Name": "The Witness", "SortName": "the witness",
                                                         "Overview": "Automatic overview", "OriginalTitle": "Raw title", "OfficialRating": "PG"},
                        "Effective": {"Name": "The Witness", "SortName": "the witness", "Overview": "Automatic overview",
                                      "OriginalTitle": "Manual title", "OfficialRating": "TV-PG"},
                        "Overrides": {"OriginalTitle": "Manual title"}, "LockedValues": {"OfficialRating": "TV-PG"},
                        "LockedFields": ["OfficialRating"], "EditableFields": ["Overview", "OriginalTitle", "OfficialRating"],
                        "InactiveFields": [], "LastEditedBy": "earlier-admin", "LastEditedAt": "before"}
        self.state = copy.deepcopy(self.initial)
        self.path = "/admin/v1/items/witness/metadata"
        self.order, self.conflict_code = "sorting-first", "revision_conflict"
        self.corrupt_fresh, self.corrupt_final, self.second_conflict = None, None, False
        self.requests, self.receipts = [], []
        self.lock = threading.Lock()
        self.sorting_entered, self.metadata_entered = threading.Event(), threading.Event()
        self.sorting_done, self.metadata_done = threading.Event(), threading.Event()

    def http(self, label, method, path, body=None, admin=False, statuses=(200,), include_status=False, **kwargs):
        with self.lock:
            started = len(self.requests) + 1
            self.requests.append((label, method, path, copy.deepcopy(body)))
        status = 200
        if label == "sorting-rebuild":
            self.sorting_entered.set()
            self.assertTrue(self.metadata_entered.wait(2), "metadata was not submitted concurrently")
            if self.order == "metadata-first":
                self.assertTrue(self.metadata_done.wait(2))
            with self.lock:
                self.state["Automatic"]["SortName"] = "witness"
                self.state["Effective"]["SortName"] = "witness"
                self.state["Revision"] = str(int(self.state["Revision"]) + 1)
                result = {**copy.deepcopy(self.update), "Revision": "8"}
            self.sorting_done.set()
        elif method == "PUT":
            if label == "metadata-edit":
                self.metadata_entered.set()
                self.assertTrue(self.sorting_entered.wait(2), "sorting was not submitted concurrently")
                if self.order == "sorting-first":
                    self.assertTrue(self.sorting_done.wait(2))
            with self.lock:
                if label == "metadata-edit-rebased" and self.second_conflict:
                    self.state["Revision"] = str(int(self.state["Revision"]) + 1)
                if body["Revision"] != self.state["Revision"]:
                    status, result = 409, {"Error": {"Code": self.conflict_code}}
                else:
                    self.state["Overrides"] = copy.deepcopy(body["Overrides"])
                    self.state["Effective"]["Overview"] = body["Overrides"]["Overview"]
                    self.state["Revision"] = str(int(self.state["Revision"]) + 1)
                    self.state["LastEditedBy"], self.state["LastEditedAt"] = "workload-admin", "saved"
                    result = copy.deepcopy(self.state)
            if label == "metadata-edit":
                self.metadata_done.set()
        else:
            self.assertEqual((method, path), ("GET", self.path))
            with self.lock:
                result = copy.deepcopy(self.state)
            corruption = self.corrupt_fresh if label == "metadata-edit-refresh" else self.corrupt_final
            if corruption:
                corruption(result)
        receipt = {"name": label + "-body.json"}
        with self.lock:
            self.receipts.append({"label": label, "status": status, "body": copy.deepcopy(result), "receipt": receipt})
        ACTOR.need(status in statuses, "http_status")
        response = result, {}, (started, started + 10), receipt
        return (*response, status) if include_status else response

    def run_edit(self):
        return self.actor.concurrent_metadata_edit(self.path, copy.deepcopy(self.initial), self.settings, self.update)

    def test_metadata_first_succeeds_and_readback_retains_later_sorting_commit(self):
        self.order = "metadata-first"
        result = self.run_edit()
        first = next(row for row in self.receipts if row["label"] == "metadata-edit")
        self.assertEqual((first["status"], first["body"]["Automatic"]["SortName"]), (200, "the witness"))
        self.assertEqual((result["Revision"], result["Automatic"]["SortName"]), ("3", "witness"))
        self.assertEqual(result["Effective"]["Overview"], "Phase 3 concurrent metadata cold")
        self.assertCountEqual([row[0] for row in self.requests if row[1] == "PUT"], ["sorting-rebuild", "metadata-edit"])
        self.assertFalse(any(row[0] == "metadata-edit-refresh" for row in self.requests))

    def test_sorting_first_rejects_stale_write_and_saves_one_fresh_cas(self):
        result = self.run_edit()
        writes = [row for row in self.requests if row[1] == "PUT" and row[2] == self.path]
        self.assertEqual([(row[0], row[3]["Revision"]) for row in writes], [("metadata-edit", "1"), ("metadata-edit-rebased", "2")])
        self.assertEqual(result["Overrides"], {"OriginalTitle": "Manual title", "Overview": "Phase 3 concurrent metadata cold"})
        self.assertEqual(result["LockedValues"], self.initial["LockedValues"])
        self.assertEqual(result["Automatic"]["Overview"], "Automatic overview")
        self.assertEqual(result["Automatic"]["SortName"], "witness")
        conflict = next(call for call in self.actor.e.event.call_args_list if call.args[0] == "metadata_edit_conflict")
        self.assertEqual(conflict.kwargs["rejected_body"], {"name": "metadata-edit-body.json"})
        self.assertEqual(conflict.kwargs["current_revision"], "2")
        self.assertTrue(conflict.kwargs["manual_state_unchanged"])

    def test_unrelated_conflict_is_not_reloaded_or_accepted(self):
        self.conflict_code = "access_denied"
        with self.assertRaisesRegex(ACTOR.Failure, "metadata_edit_unexpected_response"):
            self.run_edit()
        self.assertFalse(any(row[0] in ("metadata-edit-refresh", "metadata-edit-rebased") for row in self.requests))

    def test_conflict_must_leave_manual_controls_and_edit_history_unchanged(self):
        for field, value in (("Overrides", {"OriginalTitle": "Someone else's edit"}),
                             ("LockedValues", {"OfficialRating": "R"}), ("LastEditedBy", "another-admin")):
            with self.subTest(field=field):
                self.setUp()
                self.corrupt_fresh = lambda detail, field=field, value=value: detail.__setitem__(field, value)
                with self.assertRaises(ACTOR.Failure):
                    self.run_edit()
                self.assertFalse(any(row[0] == "metadata-edit-rebased" for row in self.requests))

    def test_changed_automatic_source_cannot_be_misclassified_as_sorting(self):
        self.corrupt_fresh = lambda detail: detail["Automatic"].__setitem__("Overview", "New source overview")
        with self.assertRaisesRegex(ACTOR.Failure, "metadata_edit_automatic_sort_changed"):
            self.run_edit()
        self.assertFalse(any(row[0] == "metadata-edit-rebased" for row in self.requests))

    def test_second_conflict_fails_without_a_retry_loop(self):
        self.second_conflict = True
        with self.assertRaisesRegex(ACTOR.Failure, "http_status"):
            self.run_edit()
        self.assertEqual(sum(row[0] == "metadata-edit-rebased" for row in self.requests), 1)
        self.assertFalse(any(row[0] == "metadata-edit-readback" for row in self.requests))

    def test_success_response_does_not_replace_final_persistence_readback(self):
        self.corrupt_final = lambda detail: detail["Effective"].__setitem__("Overview", "Lost edit")
        with self.assertRaisesRegex(ACTOR.Failure, "metadata_edit_final_readback"):
            self.run_edit()


AAC_PACKET_SHA256 = ACTOR.digest(b"owned raw AAC access unit")


def copy_seek_plan(actual=240000000, requested=300000000, timestamps=True):
    audio = {"stream_index": 1, "codec": "aac", "time_base_numerator": 1, "time_base_denominator": 48000,
             "sample_rate": 48000, "channels": 1, "pts": actual * 48000 // 10000000,
             "duration": 1024, "packet_sha256": AAC_PACKET_SHA256}
    candidate = {"version": 1, "requested_start_ticks": actual,
        "index": {"stream_index": 0, "time_base_numerator": 1, "time_base_denominator": 12288,
                  "entries": [{"pts": actual * 12288 // 10000000, "dts": actual * 12288 // 10000000,
                               "audio": [copy.deepcopy(audio)]}]}, "audio": audio}
    if actual != requested:
        candidate["original_requested_start_ticks"] = requested
    if timestamps:
        candidate["copy_timestamps"] = True
    return {"StartTicks": actual, "CopyTimestamps": timestamps, "VideoCodec": "copy", "AudioCodec": "copy",
        "VideoStreamIndex": 0, "AudioStreamIndex": 1, "OutputMode": "progressive", "Container": "mp4",
        "VideoCopySeekCandidate": ACTOR.json_bytes(candidate).decode()}


class ProgressiveSeekTests(unittest.TestCase):
    def test_alignment_opt_in_preserves_original_request_codecs_and_scope(self):
        path = "/emby/Videos/item/stream.mp4?VideoCodec=copy&AudioCodec=copy&PlaySessionId=play_owned&api_key=token"
        result = ACTOR.progressive_request(path, "remux", 300000000)
        query = dict(ACTOR.parse_qsl(ACTOR.urlsplit(result).query))
        self.assertEqual(query, {"VideoCodec": "copy", "AudioCodec": "copy", "PlaySessionId": "play_owned",
                                "api_key": "token", "StartTimeTicks": "300000000", "AllowVideoSeekAlignment": "true"})
        self.assertNotIn("AllowVideoSeekAlignment", ACTOR.progressive_request(path, "remux", 0))
        self.assertNotIn("AllowVideoSeekAlignment", ACTOR.progressive_request(path, "transcode", 300000000))
        with self.assertRaisesRegex(ACTOR.Failure, "remux_copy_request_required"):
            ACTOR.progressive_request(path.replace("AudioCodec=copy", "AudioCodec=aac"), "remux", 300000000)

    def test_actual_header_is_bounded_preceding_alignment_not_a_new_target(self):
        self.assertEqual(ACTOR.progressive_start({"X-Goby-Start-Time-Ticks": "240000000", "X-Goby-Seek-Aligned": "true"},
                                                300000000, True), 240000000)
        self.assertEqual(ACTOR.progressive_start({"X-Goby-Start-Time-Ticks": "300000000"}, 300000000, False), 300000000)
        for headers, permitted in (({}, True), ({"X-Goby-Start-Time-Ticks": "240000000"}, True),
            ({"X-Goby-Start-Time-Ticks": "320000000", "X-Goby-Seek-Aligned": "true"}, True),
            ({"X-Goby-Start-Time-Ticks": "190000000", "X-Goby-Seek-Aligned": "true"}, True),
            ({"X-Goby-Start-Time-Ticks": "240000000", "X-Goby-Seek-Aligned": "true"}, False)):
            with self.subTest(headers=headers, permitted=permitted), self.assertRaises(ACTOR.Failure):
                ACTOR.progressive_start(headers, 300000000, permitted)

    def test_completed_plan_retains_original_request_and_both_copy_tracks(self):
        self.assertEqual(ACTOR.remux_seek_contract(copy_seek_plan(), 300000000, 240000000), (True, AAC_PACKET_SHA256))
        self.assertEqual(ACTOR.remux_seek_contract(copy_seek_plan(300000000, 300000000, False), 300000000, 300000000),
                         (False, AAC_PACKET_SHA256))
        for change in ("requested", "actual", "clock", "audio-copy", "audio-proof", "video-proof"):
            plan = copy_seek_plan()
            candidate = ACTOR.strict_json(plan["VideoCopySeekCandidate"].encode())
            if change == "requested":
                candidate["original_requested_start_ticks"] = 320000000
            elif change == "actual":
                candidate["requested_start_ticks"] = 300000000
            elif change == "clock":
                plan["CopyTimestamps"] = False
            elif change == "audio-copy":
                plan["AudioCodec"] = "aac"
            elif change == "audio-proof":
                candidate.pop("audio")
            else:
                candidate["index"]["stream_index"] = 2
            plan["VideoCopySeekCandidate"] = ACTOR.json_bytes(candidate).decode()
            with self.subTest(change=change), self.assertRaises(ACTOR.Failure):
                ACTOR.remux_seek_contract(plan, 300000000, 240000000)

    def test_completed_plan_binds_full_audio_proof_to_selected_video_point(self):
        for change, code in (("missing", "remux_seek_audio_binding"), ("different-hash", "remux_seek_audio_binding"),
            ("different-stream", "remux_seek_audio_binding"), ("duplicate", "remux_seek_audio_binding"),
            ("bool-channels", "remux_seek_audio_binding"), ("wrong-clock", "remux_seek_joint_clock"),
            ("invalid-hash", "remux_seek_audio_proof")):
            plan = copy_seek_plan()
            candidate = ACTOR.strict_json(plan["VideoCopySeekCandidate"].encode())
            point = candidate["index"]["entries"][0]
            if change == "missing":
                point.pop("audio")
            elif change == "different-hash":
                point["audio"][0]["packet_sha256"] = "f" * 64
            elif change == "different-stream":
                point["audio"][0]["stream_index"] = 2
            elif change == "duplicate":
                point["audio"].append(copy.deepcopy(candidate["audio"]))
            elif change == "bool-channels":
                point["audio"][0]["channels"] = True
            elif change == "wrong-clock":
                candidate["audio"]["pts"] += 1024
                point["audio"][0] = copy.deepcopy(candidate["audio"])
            else:
                candidate["audio"]["packet_sha256"] = "SHA256:" + AAC_PACKET_SHA256
                point["audio"][0] = copy.deepcopy(candidate["audio"])
            plan["VideoCopySeekCandidate"] = ACTOR.json_bytes(candidate).decode()
            with self.subTest(change=change), self.assertRaisesRegex(ACTOR.Failure, code):
                ACTOR.remux_seek_contract(plan, 300000000, 240000000)

    def test_packet_clock_requires_actual_preserved_start_for_both_tracks(self):
        for kind, index, rate in (("video", 0, 12288), ("audio", 1, 48000)):
            sample = {"streams": [{"index": index, "codec_type": kind, "time_base": "1/" + str(rate)}],
                      "packets": [{"stream_index": index, "pts": 24 * rate, "dts": 24 * rate,
                                   "data_hash": "SHA256:" + AAC_PACKET_SHA256}]}
            expected_hash = AAC_PACKET_SHA256 if kind == "audio" else None
            result = ACTOR.copied_packet_start(sample, 240000000, kind, expected_hash)
            self.assertEqual(result["position_ticks_numerator"], 240000000)
            for position in (0, 30):
                changed = copy.deepcopy(sample)
                changed["packets"][0].update(pts=position * rate, dts=position * rate)
                with self.subTest(kind=kind, position=position), self.assertRaisesRegex(ACTOR.Failure, "copied_packet_source_clock_mismatch"):
                    ACTOR.copied_packet_start(changed, 240000000, kind, expected_hash)
            sample["packets"][0]["dts"] -= 1
            with self.assertRaisesRegex(ACTOR.Failure, "copied_packet_restart_timestamp"):
                ACTOR.copied_packet_start(sample, 240000000, kind, expected_hash)

    def test_audio_packet_payload_hash_cannot_be_replaced_by_clock_or_extradata(self):
        sample = {"streams": [{"index": 1, "codec_type": "audio", "time_base": "1/48000",
                               "extradata_hash": "SHA256:" + AAC_PACKET_SHA256}],
                  "packets": [{"stream_index": 1, "pts": 1152000, "dts": 1152000,
                               "data_hash": "SHA256:" + AAC_PACKET_SHA256}]}
        for payload_hash in (AAC_PACKET_SHA256, AAC_PACKET_SHA256.upper()):
            sample["packets"][0]["data_hash"] = "SHA256:" + payload_hash
            observed = ACTOR.copied_packet_start(sample, 240000000, "audio", AAC_PACKET_SHA256)
            self.assertEqual(observed["packet_sha256"], observed["expected_packet_sha256"])
            self.assertEqual(observed["packet_sha256"], AAC_PACKET_SHA256)
        for payload_hash, code in (("SHA256:" + "f" * 64, "copied_audio_hash_mismatch"),
            (None, "copied_audio_hash_missing"), ("SHA512:" + AAC_PACKET_SHA256, "copied_audio_hash_missing")):
            changed = copy.deepcopy(sample)
            changed["packets"].append(copy.deepcopy(sample["packets"][0]))
            changed["packets"][0]["data_hash"] = payload_hash
            with self.subTest(payload_hash=payload_hash), self.assertRaisesRegex(ACTOR.Failure, code):
                ACTOR.copied_packet_start(changed, 240000000, "audio", AAC_PACKET_SHA256)
        with self.assertRaisesRegex(ACTOR.Failure, "copied_audio_expected_hash"):
            ACTOR.copied_packet_start(sample, 240000000, "audio")
        sample["packets"][0].update(pts=0, dts=0)
        self.assertEqual(ACTOR.copied_packet_start(sample, 0, "audio", AAC_PACKET_SHA256)["packet_sha256"], AAC_PACKET_SHA256)

    def test_consumer_selects_original_source_time_instead_of_aligned_first_frame(self):
        arguments = ACTOR.progressive_frame_arguments("aligned.mp4", 300000000, True)
        self.assertIn("-copyts", arguments)
        self.assertEqual(arguments[arguments.index("-vf") + 1], "select=gte(t\\,30.0000000),scale=64:36")
        self.assertNotIn("-ss", arguments)
        exact = ACTOR.progressive_frame_arguments("rebased.mp4", 300000000, False)
        self.assertNotIn("-copyts", exact)
        self.assertEqual(exact[exact.index("-vf") + 1], "scale=64:36")


def playback_clients():
    return {mode: {"emby_token": mode + "-token", "auth_session_id": mode + "-auth", "device_id": mode + "-device"}
            for mode in ("direct", "remux", "transcode")}


class PlaybackClientBindingTests(unittest.TestCase):
    def test_legacy_private_context_is_rejected_before_any_runtime_checks(self):
        actor = ACTOR.Actor.__new__(ACTOR.Actor)
        fields = "schema_version manifest_sha256 driver_sha256 run_id owner_id origin admin_cookie csrf_token emby_token user_id device_id owner_file app_pid app_start_ticks cgroup_path postgres process_observer pg_env tools roots inventory_path inventory_sha256 scan_libraries catalog_expected queries playback analysis_item_ids mutation scan_evidence_path scan_evidence_sha256"
        actor.c = dict.fromkeys(fields.split())
        actor.c.update(schema_version=1, run_id="run", owner_id="owner", origin="https://outside.invalid")
        actor.m = {"run_id": "run", "owner_id": "owner"}
        for version in (1, 2.0, "2", True):
            actor.c["schema_version"] = version
            with self.subTest(version=version), self.assertRaisesRegex(ACTOR.Failure, "context_binding"):
                actor.validate_context()
        actor.c["schema_version"] = 2
        with self.assertRaisesRegex(ACTOR.Failure, "loopback_origin"):
            actor.validate_context()

    def test_three_fixed_clients_share_the_primary_query_and_direct_identity(self):
        clients = playback_clients()
        original = copy.deepcopy(clients)
        ACTOR.validate_playback_clients(clients, "direct-token", "direct-device")
        for mode, client in clients.items():
            headers = ACTOR.playback_client_headers(client)
            self.assertEqual(headers["X-Emby-Token"], mode + "-token")
            self.assertIn('DeviceId="' + mode + '-device"', headers["X-Emby-Authorization"])
        self.assertEqual(clients, original)

    def test_sharing_any_credential_component_between_lanes_is_rejected(self):
        for key in ("emby_token", "auth_session_id", "device_id"):
            clients = playback_clients()
            clients["transcode"][key] = clients["remux"][key]
            with self.subTest(key=key), self.assertRaisesRegex(ACTOR.Failure, "distinct_playback_clients_required"):
                ACTOR.validate_playback_clients(clients, "direct-token", "direct-device")

    def test_missing_extra_or_unsafe_client_fields_are_rejected(self):
        for key, value in (("emby_token", ""), ("emby_token", "token\r\nInjected: yes"),
                           ("auth_session_id", "bad session"), ("device_id", 'device"')):
            clients = playback_clients()
            clients["remux"][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(ACTOR.Failure):
                ACTOR.validate_playback_clients(clients, "direct-token", "direct-device")
        for mutation in ("missing", "extra", "missing-mode"):
            clients = playback_clients()
            if mutation == "missing":
                del clients["remux"]["auth_session_id"]
            elif mutation == "extra":
                clients["remux"]["user_id"] = "other-user"
            else:
                del clients["transcode"]
            with self.subTest(mutation=mutation), self.assertRaises(ACTOR.Failure):
                ACTOR.validate_playback_clients(clients, "direct-token", "direct-device")

    def test_query_credentials_cannot_diverge_from_the_direct_lane(self):
        for token, device in (("other-token", "direct-device"), ("direct-token", "other-device")):
            with self.subTest(token=token, device=device), self.assertRaisesRegex(ACTOR.Failure, "primary_direct_client_binding"):
                ACTOR.validate_playback_clients(playback_clients(), token, device)

    def test_database_play_session_must_match_every_identity_field(self):
        actor = ACTOR.Actor.__new__(ACTOR.Actor)
        actor.c = {"user_id": "viewer"}
        play = {"auth_session_id": "remux-auth", "device_id": "remux-device", "item_id": "item", "source_id": "source"}
        expected = {"id": "play-owned", "user_id": "viewer", "auth_session_id": "remux-auth",
                    "device_id": "remux-device", "item_id": "item", "media_source_id": "source"}
        actor.sql = mock.Mock(return_value=copy.deepcopy(expected))
        actor.verify_playback_identity("play-owned", play)
        for key in expected:
            actor.sql.return_value = {**expected, key: "unrelated"}
            with self.subTest(key=key), self.assertRaisesRegex(ACTOR.Failure, "playback_authentication_binding"):
                actor.verify_playback_identity("play-owned", play)
        actor.sql.return_value = None
        with self.assertRaisesRegex(ACTOR.Failure, "playback_authentication_binding"):
            actor.verify_playback_identity("play-owned", play)


class PlaybackClientCleanupTests(unittest.TestCase):
    def setUp(self):
        self.actor = ACTOR.Actor.__new__(ACTOR.Actor)
        self.clients = playback_clients()
        self.actor.c = {"emby_token": "direct-token", "device_id": "direct-device", "user_id": "viewer",
                        "playback": [{"mode": mode, "client": client, "seek_ticks": 300000000}
                                     for mode, client in self.clients.items()]}
        self.actor.plays = {"play-" + mode: {"item_id": "item-" + mode, "source_id": "source-" + mode,
            "mode": mode, "auth_session_id": client["auth_session_id"], "device_id": client["device_id"]}
            for mode, client in self.clients.items()}
        self.actor.closed_plays = {}
        self.actor.http = mock.Mock()
        self.actor.sql = mock.Mock(side_effect=AssertionError("Cleanup must still attempt HTTP when SQL is unavailable"))

    def test_cleanup_keeps_each_original_client_without_mutating_query_credentials(self):
        before = copy.deepcopy(self.actor.c)
        barrier = threading.Barrier(3)
        def observed(label, method, path, body=None, **kwargs):
            identity = body["PlaySessionId"] if body else dict(ACTOR.parse_qsl(ACTOR.urlsplit(path).query))["PlaySessionId"]
            mode = identity.removeprefix("play-")
            self.assertEqual(kwargs["headers"], ACTOR.playback_client_headers(self.clients[mode]))
            if label == "playback-stop":
                barrier.wait(timeout=2)
                self.assertEqual(body["ItemId"], "item-" + mode)
            else:
                self.assertEqual(dict(ACTOR.parse_qsl(ACTOR.urlsplit(path).query))["DeviceId"], mode + "-device")
        self.actor.http.side_effect = observed
        with ACTOR.concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
            list(executor.map(self.actor.close_one_playback, list(self.actor.plays)))
        self.assertEqual(self.actor.c, before)
        self.assertEqual(self.actor.plays, {})
        self.assertEqual(set(self.actor.closed_plays), {"play-direct", "play-remux", "play-transcode"})
        self.assertEqual(self.actor.http.call_count, 6)
        self.actor.sql.assert_not_called()

    def test_changed_lane_identity_cannot_stop_a_different_clients_play(self):
        self.actor.plays["play-remux"]["auth_session_id"] = "transcode-auth"
        with self.assertRaisesRegex(ACTOR.Failure, "playback_client_changed"):
            self.actor.close_one_playback("play-remux")
        self.actor.http.assert_not_called()
        self.assertIn("play-remux", self.actor.plays)

    def test_partial_prepare_still_attempts_cleanup_with_its_original_client(self):
        self.actor.plays["play-remux"]["source_id"] = ""
        self.actor.close_one_playback("play-remux")
        self.assertEqual(self.actor.http.call_args_list[0].kwargs["headers"],
                         ACTOR.playback_client_headers(self.clients["remux"]))
        self.assertEqual(self.actor.http.call_count, 2)
        self.actor.sql.assert_not_called()


class RemuxPlaybackTests(unittest.TestCase):
    def setUp(self):
        self.actor = ACTOR.Actor.__new__(ACTOR.Actor)
        self.actor.lock = threading.RLock()
        self.actor.phase, self.actor.plays, self.actor.intents = "cold", {}, {}
        self.actor.m = {"thresholds": {"min_media_bytes": 64, "frame_mae": 0}, "budgets": {"max_stream_bytes": 4096}}
        self.client = {"emby_token": "remux-token", "auth_session_id": "remux-auth", "device_id": "remux-device"}
        self.actor.c = {"user_id": "user", "device_id": "query-device", "emby_token": "query-token", "playback": [{"mode": "remux", "item_id": "item",
            "path": "/owned/source.mp4", "body": {"IsPlayback": True}, "seek_ticks": 300000000,
            "expected_codecs": ["h264", "aac"], "client": self.client}]}
        self.mode = "remux"
        self.events, self.requests = [], []
        self.actor.e = mock.Mock(private=Path("/evidence"))
        self.actor.e.event.side_effect = lambda kind, **fields: self.events.append({"kind": kind, **fields})
        self.actor.admission_intent = mock.Mock(side_effect=self.admission)
        self.actor.http = mock.Mock(side_effect=self.http)
        self.actor.command = mock.Mock(side_effect=self.command)
        self.actor.sql = mock.Mock(side_effect=self.jobs)
        self.actor.close_one_playback = mock.Mock()
        self.actor.drain_playback = mock.Mock()
        self.seeking, self.wrong_frame = False, False
        self.audio_packet_hash, self.unbound_audio = AAC_PACKET_SHA256, False

    def admission(self, kind, value):
        self.actor.intents["intent"] = value
        return "intent"

    def http(self, label, method, path, body=None, **kwargs):
        self.requests.append((label, method, path, copy.deepcopy(body)))
        expected_headers = ACTOR.playback_client_headers(self.client)
        self.assertEqual({key: kwargs.get("headers", {}).get(key) for key in expected_headers}, expected_headers)
        if label == self.mode + "-prepare":
            path_key = "DirectStreamUrl" if self.mode == "direct" else "TranscodingUrl"
            codecs = "VideoCodec=h264&AudioCodec=aac" if self.mode == "transcode" else "VideoCodec=copy&AudioCodec=copy"
            value = {"PlaySessionId": "play_owned", "MediaSources": [{"Id": "source", path_key:
                "/emby/Videos/item/stream.mp4?" + codecs + "&PlaySessionId=play_owned"}]}
            return value, {}, (1, 2), None
        if label in (self.mode + "-start", self.mode + "-seek"):
            self.seeking = label.endswith("-seek")
            if self.mode == "direct":
                offset = 4096 if self.seeking else 0
                self.assertEqual(kwargs["headers"]["Range"], "bytes=%d-%d" % (offset, offset + 4095))
                self.assertEqual(kwargs["statuses"], (206,))
                raw = (b"b" if self.seeking else b"a") * 4096
                return raw, {"Content-Range": "bytes %d-%d/8192" % (offset, offset + 4095)}, (10, 20), None
            ticks = 240000000 if self.mode == "remux" else 300000000
            headers = {"X-Goby-Start-Time-Ticks": str(ticks) if self.seeking else "0"}
            if self.seeking and self.mode == "remux":
                headers["X-Goby-Seek-Aligned"] = "true"
            return b"m" * 128, headers, (30, 45) if self.seeking else (10, 20), {"name": label + ".mp4"}
        if label == self.mode + "-progress-read":
            return {"UserData": {"PlaybackPositionTicks": 300000000}}, {}, (50, 51), None
        return None, {}, (3, 4), None

    def command(self, tool, arguments, **kwargs):
        if tool == "ffprobe":
            if "-show_packets" in arguments:
                video = arguments[arguments.index("-select_streams") + 1] == "v:0"
                index, rate, kind = (0, 12288, "video") if video else (1, 48000, "audio")
                value = {"streams": [{"index": index, "codec_type": kind, "time_base": "1/" + str(rate)}],
                         "packets": [{"stream_index": index, "pts": 24 * rate, "dts": 24 * rate}]}
                if not video:
                    self.assertEqual(arguments[arguments.index("-show_data_hash") + 1], "sha256")
                    self.assertIn("data_hash", arguments[arguments.index("-show_entries") + 1])
                    self.assertEqual(arguments[arguments.index("-read_intervals") + 1], "%+#64")
                    value["packets"][0]["data_hash"] = "SHA256:" + self.audio_packet_hash
            else:
                value = {"streams": [{"codec_type": "video", "codec_name": "h264"}, {"codec_type": "audio", "codec_name": "aac"}]}
            return ACTOR.json_bytes(value), {"name": "probe.json"}
        if "-ss" in arguments:
            position = int(float(arguments[arguments.index("-ss") + 1]))
        elif self.seeking and self.mode == "transcode":
            position = 30
        elif self.seeking:
            target_selected = "-copyts" in arguments and "select=gte(t\\,30.0000000),scale=64:36" in arguments
            position = 30 if target_selected and not self.wrong_frame else 24
        else:
            position = 0
        return bytes([position]) * (64 * 36 * 3), {"name": "frame.bin"}

    def jobs(self, query):
        if "FROM play_sessions" in query:
            self.assertIn("WHERE id='play_owned'", query)
            return {"id": "play_owned", "user_id": "user", "auth_session_id": self.client["auth_session_id"],
                    "device_id": self.client["device_id"], "item_id": "item", "media_source_id": "source"}
        for binding in ("play_session_id='play_owned'", "item_id='item'", "media_source_id='source'",
                        "auth_session_id='" + self.client["auth_session_id"] + "'",
                        "device_id='" + self.client["device_id"] + "'", "user_id='user'"):
            self.assertIn(binding, query)
        if self.mode == "transcode":
            plan = {"StartTicks": 300000000 if self.seeking else 0, "CopyTimestamps": False,
                    "VideoCodec": "h264", "AudioCodec": "aac", "VideoStreamIndex": 0, "AudioStreamIndex": 1,
                    "OutputMode": "progressive", "Container": "mp4"}
            return [{"id": "transcode-job", "state": "completed", "plan": plan, "source_stamp": "owned-source", "bytes": 128}]
        plan = copy_seek_plan() if self.seeking else copy_seek_plan(0, 0, False)
        if self.seeking and self.unbound_audio:
            candidate = ACTOR.strict_json(plan["VideoCopySeekCandidate"].encode())
            candidate["index"]["entries"][0]["audio"][0]["packet_sha256"] = "f" * 64
            plan["VideoCopySeekCandidate"] = ACTOR.json_bytes(candidate).decode()
        return [{"id": "job", "state": "completed", "plan": plan, "source_stamp": "owned-source", "bytes": 128}]

    def test_aligned_seek_keeps_measured_get_original_progress_and_session_cleanup(self):
        self.actor.playback("remux")
        self.assertEqual([row[0] for row in self.requests], ["remux-prepare", "remux-started", "remux-start", "remux-seek", "remux-progress", "remux-progress-read"])
        seek = next(row for row in self.requests if row[0] == "remux-seek")
        query = dict(ACTOR.parse_qsl(ACTOR.urlsplit(seek[2]).query))
        self.assertEqual(query["StartTimeTicks"], "300000000")
        self.assertEqual(query["AllowVideoSeekAlignment"], "true")
        self.assertEqual(query["VideoCodec"], query["AudioCodec"])
        self.assertEqual(query["VideoCodec"], "copy")
        progress = next(row[3] for row in self.requests if row[0] == "remux-progress")
        self.assertEqual(progress["PositionTicks"], 300000000)
        observed = next(event for event in self.events if event["kind"] == "decoded_media" and event["seeking"])
        self.assertEqual((observed["actual_start_ticks"], observed["consumer_target_ticks"], observed["frame_mae"]), (240000000, 300000000, 0))
        span = next(event for event in self.events if event["kind"] == "playback_bytes" and event["seeking"])
        self.assertEqual((span["start_ns"], span["end_ns"]), (30, 45))
        clock = next(event for event in self.events if event["kind"] == "copied_seek_clock")
        self.assertEqual(clock["packet_starts"]["audio"]["packet_sha256"], AAC_PACKET_SHA256)
        self.assertEqual(clock["packet_starts"]["audio"]["expected_packet_sha256"], AAC_PACKET_SHA256)
        self.assertEqual(sum(call.args[0] == "ffprobe" and "-show_packets" in call.args[1]
                             for call in self.actor.command.call_args_list), 2)
        self.actor.close_one_playback.assert_called_once_with("play_owned")
        self.actor.drain_playback.assert_called_once()
        self.assertEqual(self.actor.intents, {})

    def test_aligned_first_picture_cannot_pass_as_requested_seek_picture(self):
        self.wrong_frame = True
        with self.assertRaisesRegex(ACTOR.Failure, "seek_frame_mismatch"):
            self.actor.playback("remux")
        self.assertFalse(any(row[0] == "remux-progress" for row in self.requests))
        self.assertIn("play_owned", self.actor.plays)
        self.actor.close_one_playback.assert_not_called()

    def test_replaced_audio_cannot_pass_with_correct_clock_and_requested_video_frame(self):
        self.audio_packet_hash = "f" * 64
        with self.assertRaisesRegex(ACTOR.Failure, "copied_audio_hash_mismatch"):
            self.actor.playback("remux")
        self.assertFalse(any(row[0] == "remux-progress" for row in self.requests))
        self.assertFalse(any(event["kind"] == "copied_seek_clock" for event in self.events))
        self.assertIn("play_owned", self.actor.plays)

    def test_unbound_audio_proof_cannot_authorize_a_matching_output_packet(self):
        self.unbound_audio = True
        with self.assertRaisesRegex(ACTOR.Failure, "remux_seek_audio_binding"):
            self.actor.playback("remux")
        self.assertFalse(any(row[0] == "remux-progress" for row in self.requests))
        self.assertFalse(any(call.args[0] == "ffprobe" and "-show_packets" in call.args[1]
                             for call in self.actor.command.call_args_list))

    def test_transcode_uses_its_own_client_for_the_entire_playback(self):
        self.mode = self.actor.c["playback"][0]["mode"] = "transcode"
        self.client.update(emby_token="transcode-token", auth_session_id="transcode-auth", device_id="transcode-device")
        before = copy.deepcopy(self.actor.c)
        self.actor.playback("transcode")
        self.assertEqual([row[0] for row in self.requests], ["transcode-prepare", "transcode-started", "transcode-start",
            "transcode-seek", "transcode-progress", "transcode-progress-read"])
        for label, _, path, _ in self.requests:
            if label in ("transcode-start", "transcode-seek"):
                query = dict(ACTOR.parse_qsl(ACTOR.urlsplit(path).query))
                self.assertEqual((query["VideoCodec"], query["AudioCodec"]), ("h264", "aac"))
                self.assertEqual(query["StartTimeTicks"], "300000000" if label.endswith("-seek") else "0")
        self.assertEqual(self.actor.c, before)
        self.actor.close_one_playback.assert_called_once_with("play_owned")

    def test_direct_preserves_range_and_primary_client_on_every_request(self):
        self.mode = self.actor.c["playback"][0]["mode"] = "direct"
        self.client.update(emby_token=self.actor.c["emby_token"], auth_session_id="direct-auth", device_id=self.actor.c["device_id"])
        before = copy.deepcopy(self.actor.c)
        offsets = []
        class Source(io.BytesIO):
            def seek(self, offset, whence=0):
                offsets.append((offset, whence))
                return super().seek(offset, whence)
        def open_source(path, mode):
            self.assertEqual((path, mode), ("/owned/source.mp4", "rb"))
            return Source(b"a" * 4096 + b"b" * 4096)
        with mock.patch.object(ACTOR.Path, "stat", return_value=mock.Mock(st_size=8192)), \
                mock.patch("builtins.open", side_effect=open_source):
            self.actor.playback("direct")
        self.assertEqual([row[0] for row in self.requests], ["direct-prepare", "direct-started", "direct-start",
            "direct-seek", "direct-progress", "direct-progress-read"])
        self.assertEqual(self.actor.c, before)
        self.actor.command.assert_not_called()
        self.assertEqual(self.actor.sql.call_count, 1)
        self.assertEqual(offsets, [(0, 0), (4096, 0)])


if __name__ == "__main__":
    unittest.main()
