#!/usr/bin/env python3
"""Pure transition guards; use a retained full envelope, never candidate IO."""
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import unittest
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location("candidate_epoch", Path(__file__).with_name("audited-candidate-runtime.py"))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)
OP_SPEC = importlib.util.spec_from_file_location("candidate_transition", Path(__file__).with_name("transition-audited-candidate.py"))
OP = importlib.util.module_from_spec(OP_SPEC)
OP_SPEC.loader.exec_module(OP)


class RuntimeGuards(unittest.TestCase):
    def test_failed_product_gate_cannot_create_output_or_reach_replacement(self):
        job = OP.Transition.__new__(OP.Transition)
        job.products = Mock(side_effect=M.ContractError("full_product_verification_not_complete"))
        job.open = Mock()
        job.calls = {"stop": 0, "replace": 0, "start": 0}
        with patch.object(OP.os, "replace") as replace, self.assertRaises(M.ContractError):
            job.run()
        job.open.assert_not_called()
        replace.assert_not_called()
        self.assertEqual(job.calls, {"stop": 0, "replace": 0, "start": 0})

    def test_existing_real_full_envelope_shape_and_failed_gate_rejection(self):
        path = Path("/opt/goby-test/audit-fixes-20260913-20260913T063334Z-935b86b650b6/report.json")
        raw = path.read_bytes()
        self.assertEqual(hashlib.sha256(raw).hexdigest(), "c6b154db95f84a9b220bdc0ede367cba0b17ab6ec0ed261de8bb1f2e88dd397b")
        # Only expected product identifiers change in this pure schema fixture.
        report = json.loads(raw)
        report.update(scope=str(M.F), archive_sha256=M.ARCHIVE)
        report["worker"]["scope"] = str(M.F)
        report["worker"]["binary"]["sha256"] = "f" * 64
        value = {"newBinary": {"sha256": "f" * 64}}
        M.validate_full_report(report, report["worker"], value)
        for field, changed in (("status", "failed"), ("mode", "target"), ("recursive_cgroup_empty", False), ("existing_services_modified", True), ("unit_exit_code", 1)):
            rejected = copy.deepcopy(report)
            rejected[field] = changed
            with self.subTest(field=field), self.assertRaises(M.ContractError):
                M.validate_full_report(rejected, rejected["worker"], value)
        rejected = copy.deepcopy(report)
        rejected["worker"]["packages"].pop()
        with self.assertRaisesRegex(M.ContractError, "full_package_coverage"):
            M.validate_full_report(rejected, rejected["worker"], value)

    def archive(self, entries):
        buffer = io.BytesIO()
        with tarfile.open(fileobj=buffer, mode="w") as archive:
            for name, content, kind in entries:
                entry = tarfile.TarInfo(name)
                entry.type = kind
                if kind == tarfile.REGTYPE:
                    entry.size = len(content)
                    archive.addfile(entry, io.BytesIO(content))
                else:
                    entry.linkname = "other"
                    archive.addfile(entry)
        return buffer.getvalue()

    def test_archive_manifest_comes_from_actual_bytes_and_rejects_aliases(self):
        raw = self.archive([("cmd/main.go", b"new product", tarfile.REGTYPE)])
        result = M.archive_manifest(raw)
        self.assertEqual(result, {"cmd/main.go": {"sha256": M.digest(b"new product"), "bytes": 11}})
        self.assertNotEqual(result["cmd/main.go"]["sha256"], M.digest(b"old product"))
        for entries in ([('../escape', b'x', tarfile.REGTYPE)], [('link', b'', tarfile.SYMTYPE)],
                        [('same', b'x', tarfile.REGTYPE), ('./same', b'y', tarfile.REGTYPE)]):
            with self.assertRaises(M.ContractError):
                M.archive_manifest(self.archive(entries))

    def session_fixture(self):
        seed = {"helper": {"sha256": M.SEED_EXECUTOR}, "cleanup": [
            {"kind": "native", "tokenSha256": "a" * 64, "logoutAcknowledged": True, "sameTokenRejected": True},
            {"kind": "emby", "tokenSha256": "b" * 64, "logoutAcknowledged": True, "sameTokenRejected": True}]}
        admission = {"kind": "audited-candidate-live-admission", "status": "admission_failed_resources_retained", "seed": M.SEED, "candidateManifest": M.PROVISION,
                     "cleanupFailures": [], "operations": {}, "backup": None, "requests": {"normal": 5, "cleanup": 2},
                     "controllerSessions": {"admin": {"credentialId": "d" * 32, "tokenSha256": "c" * 64, "sameTokenRejected": True}}}
        return seed, admission

    def test_third_revoked_session_requires_its_own_admission_receipt(self):
        seed, admission = self.session_fixture()
        result = M.current_session_contract(seed, admission)
        self.assertEqual([(row["kind"], row["tokenSha256"]) for row in result], [("admin", "a" * 64), ("emby", "b" * 64), ("admin", "c" * 64)])
        for mutation in ({"sameTokenRejected": False}, {"tokenSha256": "a" * 64}, {"credentialId": ""}):
            changed = copy.deepcopy(admission)
            changed["controllerSessions"]["admin"].update(mutation)
            with self.assertRaises(M.ContractError):
                M.current_session_contract(seed, changed)

    def test_every_owned_table_sequence_and_missing_key_fact_is_preserved(self):
        value = {"source": {"tables": {name: [] for name in M.TABLES}, "sequences": {"seq": {"lastValue": "1"}}}, "recovery": {"empty": True},
                 "trees": {root: {"file": "hash"} for root in M.TREE_ROOTS}, "fixedFiles": {str(M.C / "data/master.key"): {"absent": True}}, "protected": {unit: "inactive" for unit in M.PROTECTED},
                 "postgresProcess": {"pid": 12}, "generation": {"revision": 0}}
        value["source"]["tables"]["users"] = [{"id": "actor"}]
        M.compare_preservation(value, copy.deepcopy(value))
        for field in ("recovery", "trees", "fixedFiles", "protected", "postgresProcess", "generation"):
            changed = copy.deepcopy(value)
            changed[field] = {}
            with self.subTest(field=field), self.assertRaises(M.ContractError):
                M.compare_preservation(value, changed)
        changed = copy.deepcopy(value)
        changed["source"]["tables"]["users"][0]["id"] = "other"
        with self.assertRaises(M.ContractError):
            M.compare_preservation(value, changed)

    def control_fixture(self):
        deployment = "a" * 32
        return {"recovery/.goby-lifecycle.json": {"version": 1, "deploymentId": deployment},
                "recovery/generation-registry.json": {"generations": []}, "backups/.goby-backup-catalog.json": {"entries": []},
                "operations/current.json": {"deploymentId": deployment, "revision": 1, "payload": {"deploymentId": deployment, "operations": [],
                    "slots": [{"slot": "primary", "state": "active"}, {"slot": "recovery", "state": "unclaimed"}]}}}

    def test_idle_control_must_already_be_initialized(self):
        original = self.control_fixture()
        M.validate_control_documents(original)
        for missing in ("recovery/generation-registry.json", "backups/.goby-backup-catalog.json"):
            changed = copy.deepcopy(original)
            del changed[missing]
            with self.assertRaises(M.ContractError):
                M.validate_control_documents(changed)
        changed = copy.deepcopy(original)
        changed["operations/current.json"].update(revision=0, payload=None)
        with self.assertRaises(M.ContractError):
            M.validate_control_documents(changed)

    def diagnostic_fixture(self):
        name, new = "goby-" + "a" * 32 + "-" + "b" * 32 + ".jsonl", "goby-" + "a" * 32 + "-" + "c" * 32 + ".jsonl"
        facts = {"dev": 1, "ino": 2, "uid": 995, "gid": 995, "mode": 0o600, "bytes": 3, "sha256": M.digest(b"old")}
        entry = {"name": name, "created": "2026-09-13T08:00:00Z", "identity": {"device": 1, "inode": 2}, "closed": False, "size": 0}
        before = {"registry": {"version": 1, "token": "a" * 32, "files": [entry]}, "registryFile": {"uid": 995, "gid": 995, "mode": 0o600},
                  "files": {name: facts}, "lock": {"ino": 9}, "directory": {"ino": 8}}
        after = copy.deepcopy(before)
        after["registry"]["files"][0].update(closed=True, size=4)
        after["registry"]["files"].append({**entry, "name": new, "identity": {"device": 1, "inode": 3}})
        after["files"][name].update(bytes=4, sha256=M.digest(b"old!"), prefixSha256=M.digest(b"old"))
        after["files"][new] = {**facts, "ino": 3, "bytes": 0, "sha256": M.digest(b"")}
        return before, after, name

    def test_diagnostics_allow_only_owned_rotation_and_append(self):
        before, after, name = self.diagnostic_fixture()
        M.compare_diagnostics(before, after)
        for field, value in (("uid", 0), ("gid", 0), ("mode", 0o644)):
            changed = copy.deepcopy(after)
            changed["registryFile"][field] = value
            with self.assertRaisesRegex(M.ContractError, "diagnostic_registry_owner"):
                M.compare_diagnostics(before, changed)
        changed = copy.deepcopy(after)
        changed["files"][name]["prefixSha256"] = "f" * 64
        with self.assertRaises(M.ContractError):
            M.compare_diagnostics(before, changed)


if __name__ == "__main__":
    unittest.main()
