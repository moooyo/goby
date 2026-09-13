#!/usr/bin/env python3
"""Pure environment-revision guards using retained admission evidence only."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


M = module("environment_contract", "audited-candidate-runtime.py")
OP = module("environment_operator", "revise-audited-candidate-backup-limits.py")


class RevisionGuards(unittest.TestCase):
    def frozen(self, pin):
        raw = Path(pin["path"]).read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), pin["sha256"])
        return json.loads(raw)

    def test_only_two_missing_keys_are_appended_without_changing_old_bytes(self):
        before = b"GOBY_BACKUP_MIN_FREE_BYTES=67108864\nUNCHANGED_SECRET=synthetic\n"
        after = M.revised_environment(before)
        self.assertTrue(after.startswith(before))
        self.assertEqual(after[len(before):], b"GOBY_BACKUP_MAX_OBJECT_BYTES=67108864\nGOBY_BACKUP_MAX_TOTAL_BYTES=268435456\n")
        M.validate_environment_append(before, after)
        with self.assertRaises(M.ContractError):
            M.validate_environment_append(before, after.replace(b"synthetic", b"changed"))

    def test_existing_limit_duplicate_key_and_changed_minimum_are_rejected(self):
        base = b"GOBY_BACKUP_MIN_FREE_BYTES=67108864\n"
        for value in (base + b"GOBY_BACKUP_MAX_OBJECT_BYTES=8388608\n", base + b"GOBY_BACKUP_MAX_TOTAL_BYTES=8388608\n",
                      base + b"OTHER=1\nOTHER=2\n", base.replace(b"67108864", b"1"), base.rstrip(b"\n")):
            with self.assertRaises(M.ContractError):
                M.revised_environment(value)

    def test_actual_closed_and_internal_capture_shapes_bind_the_same_six_sessions(self):
        report, state = self.frozen(M.ADMISSION03), self.frozen(M.ADMISSION03_STATE)
        admission_input = self.frozen(report["input"])
        previous = self.frozen(admission_input["seedRuntimeBinding"])
        expected = M.environment_revision_sessions(previous, report, state)
        self.assertEqual(len(expected), 6)
        internal = copy.deepcopy(state)
        internal["previousEpoch"] = internal.pop("runtimeEpoch")
        self.assertEqual(M.environment_revision_sessions(previous, report, internal), expected)
        internal["runtimeEpoch"] = M.PREVIOUS_BINARY_EPOCH
        with self.assertRaises(M.ContractError):
            M.environment_revision_sessions(previous, report, internal)
        changed = copy.deepcopy(state)
        changed["source"]["tables"]["sessions"][0]["revoked_at"] = None
        with self.assertRaises(M.ContractError):
            M.environment_revision_sessions(previous, report, changed)

    def test_retained_binary_epoch_remains_valid_and_capacity_is_conservative(self):
        previous = self.frozen(M.PREVIOUS_BINARY_EPOCH)
        M.validate_epoch(previous)
        self.assertEqual(previous["version"], 1)
        self.assertEqual(M.REQUIRED_BACKUP_FREE, 234946560)
        self.assertGreaterEqual(int(M.BACKUP_ADDITIONS["GOBY_BACKUP_MAX_TOTAL_BYTES"]), M.REQUIRED_BACKUP_FREE)

    def test_restart_pin_uses_current_expectation_not_previous_epoch_reader(self):
        cls = OP.build_revision_class(SimpleNamespace(Transition=object))
        job = cls.__new__(cls)
        process = {"pid": 456, "startTicks": "900", "listener": {"host": "127.0.0.1", "port": 28498, "socketInode": "7"}}
        frozen_pin = Mock(return_value=process)
        stale_reader_pin = Mock(side_effect=AssertionError("The previous epoch reader must not be used after restart."))
        job.io = SimpleNamespace(reader=SimpleNamespace(pin=stale_reader_pin))
        job.s = SimpleNamespace(CandidateIO=SimpleNamespace(pin=frozen_pin))
        job.g = SimpleNamespace(PROCESS_FIELDS={"pid", "startTicks"}, metadata=Mock(return_value={"pid": 456, "startTicks": "900"}), verify_listener=Mock())
        job.need = M.need
        self.assertEqual(job.pin(), process)
        frozen_pin.assert_called_once_with(job.io)
        stale_reader_pin.assert_not_called()


if __name__ == "__main__":
    unittest.main()
