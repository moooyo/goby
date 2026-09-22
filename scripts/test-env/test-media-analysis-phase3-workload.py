#!/usr/bin/env python3
"""Pure Phase 3 admission/accounting tests; run only in remote verification.

These tests do not exercise Goby and cannot establish capacity, media correctness
or recovery acceptance. In particular, no synthetic event is an acceptance
result. Importing the actor must perform no work.
"""

import copy
import importlib.util
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


class RemuxPlaybackTests(unittest.TestCase):
    def setUp(self):
        self.actor = ACTOR.Actor.__new__(ACTOR.Actor)
        self.actor.lock = threading.RLock()
        self.actor.phase, self.actor.plays, self.actor.intents = "cold", {}, {}
        self.actor.m = {"thresholds": {"min_media_bytes": 64, "frame_mae": 0}}
        self.actor.c = {"user_id": "user", "device_id": "device", "playback": [{"mode": "remux", "item_id": "item",
            "path": "/owned/source.mp4", "body": {"IsPlayback": True}, "seek_ticks": 300000000, "expected_codecs": ["h264", "aac"]}]}
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
        if label == "remux-prepare":
            value = {"PlaySessionId": "play_owned", "MediaSources": [{"Id": "source", "TranscodingUrl":
                "/emby/Videos/item/stream.mp4?VideoCodec=copy&AudioCodec=copy&PlaySessionId=play_owned"}]}
            return value, {}, (1, 2), None
        if label in ("remux-start", "remux-seek"):
            self.seeking = label.endswith("-seek")
            headers = {"X-Goby-Start-Time-Ticks": "240000000" if self.seeking else "0"}
            if self.seeking:
                headers["X-Goby-Seek-Aligned"] = "true"
            return b"m" * 128, headers, (30, 45) if self.seeking else (10, 20), {"name": label + ".mp4"}
        if label == "remux-progress-read":
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
        elif self.seeking:
            target_selected = "-copyts" in arguments and "select=gte(t\\,30.0000000),scale=64:36" in arguments
            position = 30 if target_selected and not self.wrong_frame else 24
        else:
            position = 0
        return bytes([position]) * (64 * 36 * 3), {"name": "frame.bin"}

    def jobs(self, query):
        for binding in ("play_session_id='play_owned'", "item_id='item'", "media_source_id='source'", "device_id='device'", "user_id='user'"):
            self.assertIn(binding, query)
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


if __name__ == "__main__":
    unittest.main()
