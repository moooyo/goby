#!/usr/bin/env python3
"""Remote-only pure wire/closure accounting tests; no sockets or fake server."""

import importlib.util
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("phase3_overload", ROOT / "media-analysis-phase3-overload.py")
OVERLOAD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(OVERLOAD)


def snapshot():
    return {"InstanceId": "a" * 32, "ActiveCount": 0, "CurrentLimit": 64, "CompletionLimit": 128,
            "CurrentCapacityDropped": 0, "CompletionCapacityDropped": "0", "NextLeaseSequence": "1",
            "NextCompletionSequence": "1", "OldestCompletionSequence": "0", "Current": [], "Completed": []}


def lease(active=True):
    return {"LeaseId": "a" * 32 + "-1", "Sequence": "1", "CompletionSequence": "0" if active else "1",
            "ItemId": "item", "MediaSourceId": "source", "StartedUnixNano": "9007199254740993",
            "CompletedUnixNano": "0" if active else "9007199254741000", "Active": active}


class OriginalLeaseAccountingTests(unittest.TestCase):
    def test_idle_is_empty_and_active_is_not_completed(self):
        idle = OVERLOAD.lease_snapshot(snapshot())
        self.assertEqual(idle["current"], {})
        value = snapshot()
        value.update(ActiveCount=1, NextLeaseSequence="2", Current=[lease()])
        actual = OVERLOAD.lease_snapshot(value)
        self.assertEqual(len(actual["current"]), 1)
        self.assertEqual(actual["completed"], {})

    def test_completion_needs_real_nonzero_completion_identity_and_time(self):
        value = snapshot()
        value.update(NextLeaseSequence="2", NextCompletionSequence="2", OldestCompletionSequence="1", Completed=[lease(False)])
        self.assertEqual(len(OVERLOAD.lease_snapshot(value)["completed"]), 1)
        value["Completed"][0]["CompletionSequence"] = "0"
        with self.assertRaisesRegex(OVERLOAD.WORK.Failure, "original_lease_lifetime"):
            OVERLOAD.lease_snapshot(value)

    def test_missing_rows_duplicate_state_or_counter_coercion_are_rejected(self):
        value = snapshot()
        value["ActiveCount"] = 1
        with self.assertRaises(OVERLOAD.WORK.Failure):
            OVERLOAD.lease_snapshot(value)
        value.update(Current=[lease()], Completed=[lease(False)])
        with self.assertRaisesRegex(OVERLOAD.WORK.Failure, "original_lease_both_active_and_completed"):
            OVERLOAD.lease_snapshot(value)
        for counter in (True, 1, "01", "-1", "18446744073709551616"):
            with self.subTest(counter=counter), self.assertRaises(OVERLOAD.WORK.Failure):
                OVERLOAD.decimal(counter)


if __name__ == "__main__":
    unittest.main()
