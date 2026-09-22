#!/usr/bin/env python3
"""Remote-only preparation identity, inventory, and budget test source."""

import importlib.util
import copy
from pathlib import Path
import stat
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("phase3_prepare", ROOT / "media-analysis-phase3-prepare.py")
PREPARE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PREPARE)


def case(identity, episode, variant, content):
    return {"case_id": identity, "series_id": "series", "season_id": "season", "episode_id": episode,
            "variant_group": variant, "source": {"sha256": content}}


class SourceIdentityTests(unittest.TestCase):
    def test_episode_variants_never_inflate_independent_sources(self):
        values = [case("episode-original", "one", "one-original", "hash-one"),
                  case("episode-dub", "one", "one-dub", "hash-two"),
                  case("episode-two", "two", "two-original", "hash-three")]
        result = PREPARE.source_components(values)
        self.assertEqual(result["episode-original"], result["episode-dub"])
        self.assertNotEqual(result["episode-original"], result["episode-two"])

    def test_transitive_content_and_variant_aliases_remain_one_group(self):
        values = [case("a", "one", "shared-variant", "hash-one"),
                  case("b", "two", "shared-variant", "hash-two"),
                  case("c", "three", "other-variant", "hash-two")]
        result = PREPARE.source_components(values)
        self.assertEqual(len(set(result.values())), 1)
        self.assertEqual(result, PREPARE.source_components(list(reversed(values))))

    def test_budget_is_rejected_before_a_copy_or_directory_mutation(self):
        preparer = PREPARE.Preparer.__new__(PREPARE.Preparer)
        preparer.operator = {"max_fixture_allocated_bytes": 8192}
        preparer.generated_bytes = 4096
        with self.assertRaisesRegex(PREPARE.WORK.Failure, "preparation_media_byte_budget"):
            preparer.reserve(8192)
        self.assertEqual(preparer.generated_bytes, 4096)


class InventoryWriteTests(unittest.TestCase):
    expected = (
        b'{"bytes":17,"origin":"licensed","original_id":"licensed-b","relative_path":"Zed\\u7535\\u5f71.mp4","root_id":"root-b","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}\n'
        b'{"bytes":11,"origin":"generated","original_id":"short-template","relative_path":"Folder/First.mp4","root_id":"root-a","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n'
    )

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.workspace = Path(temporary.name).resolve()
        (self.workspace / "private").mkdir()
        self.inventory = self.workspace / "private" / "inventory.jsonl"
        self.preparer = PREPARE.Preparer.__new__(PREPARE.Preparer)
        self.preparer.workspace = self.workspace
        self.preparer.operator = {"max_fixture_allocated_bytes": 8192}
        self.preparer.generated_bytes = 0
        self.preparer.root_bindings = [{"id": "root-a", "path": str(self.workspace / "media" / "a")},
                                       {"id": "root-b", "path": str(self.workspace / "media" / "b")}]
        self.preparer.inventory_rows = [
            {"path": str(self.workspace / "media" / "b" / "Zed\u7535\u5f71.mp4"), "sha256": "b" * 64,
             "bytes": 17, "origin": "licensed", "original_id": "licensed-b"},
            {"path": str(self.workspace / "media" / "a" / "Folder" / "First.mp4"), "sha256": "a" * 64,
             "bytes": 11, "origin": "generated", "original_id": "short-template"}]

    def test_exact_ordered_jsonl_is_private_and_flushed_before_sync(self):
        retained_rows = self.preparer.inventory_rows
        synchronized = []
        real_fsync = PREPARE.os.fsync

        def observe_sync(fd):
            synchronized.append(self.inventory.read_bytes())
            real_fsync(fd)

        with mock.patch.object(PREPARE.os, "fsync", side_effect=observe_sync):
            path, count = self.preparer.write_inventory()
        self.assertEqual((path, count), (self.inventory, 2))
        self.assertEqual(self.inventory.read_bytes(), self.expected)
        self.assertEqual(synchronized, [self.expected])
        self.assertEqual(stat.S_IMODE(self.inventory.stat().st_mode), 0o600)
        self.assertEqual(self.preparer.generated_bytes, 8192)
        self.assertEqual(retained_rows, [])

    def test_exact_page_budget_is_checked_before_file_creation(self):
        prefix = b'{"bytes":11,"origin":"generated","original_id":"'
        suffix = b'","relative_path":"Only.mp4","root_id":"root-a","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}\n'
        padding = "x" * (4096 - len(prefix) - len(suffix))
        self.preparer.inventory_rows = [{"path": str(self.workspace / "media" / "a" / "Only.mp4"),
            "sha256": "a" * 64, "bytes": 11, "origin": "generated", "original_id": padding}]
        self.preparer.operator["max_fixture_allocated_bytes"] = 8191
        with mock.patch.object(PREPARE.os, "open") as opened:
            with self.assertRaisesRegex(PREPARE.WORK.Failure, "preparation_media_byte_budget"):
                self.preparer.write_inventory()
        opened.assert_not_called()
        self.assertFalse(self.inventory.exists())
        self.assertEqual(self.preparer.generated_bytes, 0)
        self.preparer.operator["max_fixture_allocated_bytes"] = 8192
        self.assertEqual(self.preparer.write_inventory(), (self.inventory, 1))
        self.assertEqual(self.inventory.read_bytes(), prefix + padding.encode() + suffix)
        self.assertEqual(self.preparer.generated_bytes, 8192)

    def test_existing_inventory_is_not_overwritten(self):
        self.inventory.write_bytes(b"existing inventory\n")
        with self.assertRaises(FileExistsError):
            self.preparer.write_inventory()
        self.assertEqual(self.inventory.read_bytes(), b"existing inventory\n")
        self.assertEqual(len(self.preparer.inventory_rows), 2)

    def test_inventory_symlink_is_not_followed(self):
        original = self.workspace / "private" / "original.jsonl"
        original.write_bytes(b"original inventory\n")
        self.inventory.symlink_to(original)
        with self.assertRaises(FileExistsError):
            self.preparer.write_inventory()
        self.assertTrue(self.inventory.is_symlink())
        self.assertEqual(original.read_bytes(), b"original inventory\n")
        self.assertEqual(len(self.preparer.inventory_rows), 2)

    def test_sync_failure_closes_stream_and_retains_inventory_rows(self):
        retained_rows = self.preparer.inventory_rows
        streams = []
        real_fdopen = PREPARE.os.fdopen

        def observe_stream(*args, **kwargs):
            stream = real_fdopen(*args, **kwargs)
            streams.append(stream)
            return stream

        with mock.patch.object(PREPARE.os, "fdopen", side_effect=observe_stream), \
             mock.patch.object(PREPARE.os, "fsync", side_effect=OSError("injected sync failure")):
            with self.assertRaisesRegex(OSError, "injected sync failure"):
                self.preparer.write_inventory()
        self.assertEqual(len(streams), 1)
        self.assertTrue(streams[0].closed)
        self.assertIs(self.preparer.inventory_rows, retained_rows)
        self.assertEqual(len(retained_rows), 2)


def compound_fixture():
    manifest = {"run_id": "run", "owner_id": "owner", "source_revision": "a" * 40, "profile_id": "profile", "tier": 10000,
                "guest": {"vmid": 106, "machine_id": "b" * 32}, "thresholds": {"min_all_lane_overlap_ms": 100},
                "concurrency": {"query_workers": 2, "playback_workers": 3}, "budgets": {"max_events": 100}}
    context = {"manifest_sha256": "c" * 64, "driver_sha256": "d" * 64, "catalog_expected": {"initial": 9000, "cold": 10000, "cached": 10000, "incremental": 10000},
               "scan_libraries": [{"id": "library", "expected": {"cold": {"Scanned": 6000, "Added": 1000, "Updated": 5000},
                    "cached": {"Scanned": 6000, "Added": 0, "Updated": 0}, "incremental": {"Scanned": 6000, "Added": 1, "Updated": 1}}}]}
    result = {key: copy.deepcopy(manifest[key]) for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier", "guest", "thresholds", "concurrency")}
    result.update(schema_version=1, accepted=True, execution_complete=True, failure_codes=[], cleanup_errors=[],
                  checks={"cleanup": True, "no_observer_failures": True, "http_failures": True},
                  manifest_sha256=context["manifest_sha256"], driver_sha256=context["driver_sha256"], scan_work={})
    events = []
    for phase in ("cold", "cached", "incremental"):
        scan = {"kind": "scan_completed", "phase": phase, "reference": phase, "force_probe": phase == "cold", "counters": copy.deepcopy(context["scan_libraries"][0]["expected"][phase])}
        events += [scan, {"kind": "catalog_exact_count", "phase": phase, "count": 10000},
                   {"kind": "compound_overlap", "phase": phase, "analysis_admission_overlapped": True, "productive_overlap_ms": {"intro": 120, "previews": 130}}]
        result["scan_work"][phase] = {"completed_scans": [copy.deepcopy(scan)]}
    events.append({"kind": "increment_verified", "phase": "incremental", "deleted": 1, "added": 1,
                   "stable_cross_root_identity": True, "user_state_preserved": True})
    return manifest, context, result, events


def transition_fixture():
    context = {"catalog_expected": {"initial": 9000, "incremental": 10000},
               "mutation": {"move_from": "/owned/a/move.mp4", "move_to": "/owned/b/move.mp4", "delete_path": "/owned/a/delete.mp4"}}
    old = {"move": {"id": "moved", "root_id": "root-a", "file_identity": "1:2"}, "delete_id": "removed-original"}
    before = {"catalog_count": 10000, "move": {"id": "moved", "path": context["mutation"]["move_to"], "root_id": "root-b", "file_identity": "1:2"},
              "addition": {"id": "added-incremental"}, "restored": None, "old_deleted_count": 0,
              "move_userdata": [{"item_id": "moved", "is_favorite": True}], "unaffected_count": 9998,
              "unaffected_sha256": "e" * 64, "userdata_sha256": "f" * 64, "settings_sha256": "a" * 64, "metadata_sha256": "b" * 64}
    after = copy.deepcopy(before)
    after.update(addition=None, restored={"id": "new-restored", "path": context["mutation"]["delete_path"]}, restored_userdata_count=0)
    after["move"].update(path=context["mutation"]["move_from"], root_id="root-a")
    return context, old, before, after


class AfterCompoundReceiptTests(unittest.TestCase):
    def test_complete_capacity_receipt_freezes_reverse_scan_before_dispatch(self):
        manifest, context, result, events = compound_fixture()
        self.assertEqual(PREPARE.accepted_compound(result, events, manifest, context), {"Scanned": 6000, "Added": 1, "Updated": 1})
        self.assertEqual(context["catalog_expected"]["initial"], 9000)
        self.assertTrue(result["accepted"])

    def test_partial_or_cleanup_failed_result_cannot_authorize_reconciliation(self):
        for change in ({"execution_complete": False}, {"accepted": False}, {"cleanup_errors": ["unresolved_admission"]}, {"failure_codes": ["overlap"]}):
            manifest, context, result, events = compound_fixture()
            result.update(change)
            with self.subTest(change=change), self.assertRaisesRegex(PREPARE.WORK.Failure, "after_compound_result_not_accepted"):
                PREPARE.accepted_compound(result, events, manifest, context)

    def test_missing_phase_changed_counter_or_wrong_driver_is_rejected(self):
        for mutation in ("phase", "counter", "driver"):
            manifest, context, result, events = compound_fixture()
            if mutation == "phase":
                events = [event for event in events if event.get("phase") != "cached"]
            elif mutation == "counter":
                events[0]["counters"]["Updated"] = 0
            else:
                result["driver_sha256"] = "0" * 64
            with self.subTest(mutation=mutation), self.assertRaises(PREPARE.WORK.Failure):
                PREPARE.accepted_compound(result, events, manifest, context)

    def test_inverse_scan_preserves_move_but_does_not_revive_deleted_item(self):
        context, old, before, after = transition_fixture()
        result = PREPARE.reconciled_transition(before, after, old, context)
        self.assertEqual(result["stable_moved_item_id"], "moved")
        self.assertEqual(result["recreated_item_id"], "new-restored")
        self.assertFalse(result["old_deleted_identity_revived"])
        self.assertFalse(result["old_deleted_user_state_revived"])

    def test_same_count_is_insufficient_without_identity_state_and_key_proof(self):
        for mutation in ("old-id", "userdata", "unrelated", "settings", "seed-count", "added-retained"):
            context, old, before, after = transition_fixture()
            if mutation == "old-id":
                after["restored"]["id"] = old["delete_id"]
            elif mutation == "userdata":
                after["move_userdata"][0]["is_favorite"] = False
            elif mutation == "unrelated":
                after["unaffected_sha256"] = "0" * 64
            elif mutation == "settings":
                after["settings_sha256"] = "0" * 64
            elif mutation == "seed-count":
                before["catalog_count"] = context["catalog_expected"]["initial"]
            else:
                after["addition"] = copy.deepcopy(before["addition"])
            with self.subTest(mutation=mutation), self.assertRaises(PREPARE.WORK.Failure):
                PREPARE.reconciled_transition(before, after, old, context)


class ReconciliationPopulationTests(unittest.TestCase):
    def test_full_population_boundaries_are_accepted_without_dropping_rows(self):
        PREPARE.reconciliation_population(100000, 100000, 200000, 4096, 4096)
        PREPARE.reconciliation_population(10000, 10000, 1, 1, 1)

    def test_one_excess_row_or_changed_witness_is_rejected(self):
        cases = ((100000, 100001, 1, 1, 1), (100000, 100000, 200001, 1, 1),
                 (100000, 100000, 2, 2, 1), (100000, 100000, 4097, 4097, 4097),
                 (100000, True, 1, 1, 1), (100000, 100000, 1, True, 1))
        for values in cases:
            with self.subTest(values=values), self.assertRaises(PREPARE.WORK.Failure):
                PREPARE.reconciliation_population(*values)


if __name__ == "__main__":
    unittest.main()
