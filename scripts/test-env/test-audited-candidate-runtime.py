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

    def test_programs_product_review_keeps_profile_status_and_exact_schema(self):
        pin = {"path": "/opt/goby-test/synthetic-programs-review/record.json", "sha256": "a" * 64, "bytes": 100}
        review = {"kind": "programs-final-product-independent-review", "version": 1, "status": "verified",
            **{key: copy.deepcopy(pin) for key in ("input", "adapter", "execution", "archive", "closure", "sourceBridge",
                "buildManifest", "newBinary", "packageManifest", "packageArchive", "buildTools")},
            "worker": {"archive": copy.deepcopy(pin), "member": "ram/synthetic/worker-report.json", "sha256": "b" * 64, "bytes": 100},
            "checks": {key: True for key in ("sourceIdentity", "ordinaryFullSuite", "ordinaryBuild", "embeddedBuild", "packageMembers",
                "artifactMaterialization", "toolPins", "budgets", "resourceClosure", "protectedState", "recordsBinding")},
            "limits": ["Synthetic review schema fixture only; no product execution is asserted."]}
        for complete, accepted, rejected in ((False, "verified", "passed"), (True, "passed", "verified")):
            selected = copy.deepcopy(review)
            selected["status"] = accepted
            with self.subTest(complete=complete, status=accepted):
                self.assertEqual(M.validate_programs_product_review(selected, complete=complete), set(selected))
            mutations = (
                ("status", lambda row: row.update(status=rejected)),
                ("kind", lambda row: row.update(kind="livetv-programs-complete-source-test-build-independent-review")),
                ("version", lambda row: row.update(version=True)),
                ("extra_field", lambda row: row.update(unreviewedExtra=True)),
                ("missing_check", lambda row: row["checks"].pop("recordsBinding")),
                ("extra_check", lambda row: row["checks"].update(unreviewedExtra=True)),
                ("non_boolean_check", lambda row: row["checks"].update(ordinaryFullSuite=1)),
                ("limits_type", lambda row: row.update(limits="Synthetic limit")),
                ("limit_item_type", lambda row: row.update(limits=[True])),
            )
            for name, mutate in mutations:
                changed = copy.deepcopy(selected)
                mutate(changed)
                with self.subTest(complete=complete, mutation=name), self.assertRaisesRegex(M.ContractError, "^programs_artifact_independent_review$"):
                    M.validate_programs_product_review(changed, complete=complete)

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


class RecoveredRuntimeGuards(unittest.TestCase):
    """Replay retained recovery records with explicitly synthetic current observations."""
    @classmethod
    def setUpClass(cls):
        recovery = Path("/opt/goby-test/candidate-disk-full-recovery-20260914")
        selections = {
            "runtimeEpoch": (M.R / "candidate-tv-parent-transition-01/private/runtime-epoch.json", "76d7cc71be87851271272537795255f9ad7a5f5c3920dd6546e573f42d06bfac"),
            "seedBinding": (M.R / "candidate-tv-parent-transition-01/private/seed-runtime-binding.json", "94bd35e5523a56c60a9b712684d02785b05d6924820bb25f60c48ec8d3496c43"),
            "admission": (M.R / "candidate-live-admission-05/private/report.json", "b73a2d30926c68886bd1674a356e6330eab2072afb53fb1fa1f695c5337f8535"),
            "admissionCloseout": (M.R / "candidate-live-admission05-closeout.json", "86b224298601ff922d6fe136c026b87628282c675b925dae4831ce78068f139b"),
            "hosting": (Path("/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-reconcile-01/hosting.json"), "2100142b83941e24503838fdf92942aef862bc785bbcfcbe8f93223dd30c53c0"),
            "execution": (recovery / "restart-execution.json", "3dfb3639001f63a1b7545022e648480db7f777f8d81222e26d49dd8b340c2f8f"),
            "independentReview": (recovery / "restart-independent-review.json", "8c97fe729fbde2694ba1a9354f6b276bfa8f675c87575a05fcb2b18213a954dc"),
            "configuration": (recovery / "restart-configuration-observation.json", "40df2510717645c3fb1c6ea683b3bad55ec9a7096a113d0827af067174d47335"),
            "selectedStartIntent": (recovery / "restart-01/old/start-intent.json", "406eed6d4e05c1bbade24cbd4b5a1712c2299d78381da660cc25180ca9d8a7b6"),
            "selectedResult": (recovery / "restart-01/old/result.json", "a024667a2267c5cb9a93ca906f91456665f50a2d43241b34ececb825cbc6cb55"),
        }
        cls.saved, cls.pins, cls.raws = {}, {}, {}
        for key, (path, expected) in selections.items():
            raw = path.read_bytes()
            if M.digest(raw) != expected:
                raise AssertionError("A retained recovery fixture pin changed: " + key)
            cls.saved[key], cls.pins[key], cls.raws[str(path)] = json.loads(raw), {"path": str(path), "sha256": expected}, raw
        cls.provision_module = M.load_helper("current_runtime_saved_provision", cls.saved["runtimeEpoch"]["helpers"]["provision"])

    def fixture(self):
        records, pins, raws = copy.deepcopy(self.saved), copy.deepcopy(self.pins), dict(self.raws)
        epoch, result = records["runtimeEpoch"], records["selectedResult"]
        candidate, restored = epoch["candidate"], result["identity"]["process"]
        process = {**epoch["candidateProcess"], "pid": restored["pid"], "startTicks": restored["startTicks"]}
        properties = {key: result["claimedUnit"][key] for key in candidate["processes"]["server"]}
        identity = {**candidate["serverIdentity"], "pid": process["pid"], "startTicks": process["startTicks"], "invocationId": properties["InvocationID"]}
        listener = {**candidate["listener"], "pid": process["pid"], "socketInode": result["listener"]["socketInode"]}
        # This timestamp and the whole current observation are fixtures, not a current lease claim.
        lease = {**result["lease"]["facts"], "backendStart": "2026-09-14T15:00:00+00:00",
                 "candidateConnection": {**result["lease"]["applicationSocket"], "pid": process["pid"]}}
        current = {"candidateProcess": process, "serverProperties": properties, "serverIdentity": identity, "listener": listener,
                   "postgresProcess": epoch["postgresProcess"], "postgresProperties": candidate["processes"]["postgres"], "lease": lease}
        preserved = {key: candidate[key] for key in ("binary", "runtime", "units")}
        def add(name, value):
            path = str(M.R / "synthetic-current-runtime-source-checks" / (name + ".json"))
            raw = M.canonical(value)
            raws[path] = raw
            return {"path": path, "sha256": M.digest(raw)}
        source = {"path": str(M.R / "synthetic-current-runtime-source-checks/observer.py"), "sha256": M.digest(b"synthetic observer source")}
        raws[source["path"]] = b"synthetic observer source"
        verification = add("verification", {"status": "synthetic_source_fixture"})
        records["leaseQueryResult"] = [{key: item for key, item in lease.items() if key != "candidateConnection"}]
        lease_pin = add("lease-result", records["leaseQueryResult"])
        host = records["hosting"]
        host_identity = {"process": host["process"], "listener": host["listener"], "unit": {key: host["unitProperties"][key] for key in M.CURRENT_HOST_UNIT_FIELDS}}
        calls = {"sql": 2, "http": 0, "browser": 0, "service": 0, "seed": 0, "admission": 0, "provision": 0}
        records["observation"] = {"kind": "audited-candidate-current-runtime-observation", "version": 1,
            "runtimeEpoch": pins["runtimeEpoch"], "recoveryExecution": pins["execution"], "hosting": pins["hosting"],
            "source": source, "verification": verification, "capturedAt": "2026-09-15T08:00:00+00:00",
            "before": copy.deepcopy(current), "after": copy.deepcopy(current), "preserved": copy.deepcopy(preserved),
            "hostingBefore": copy.deepcopy(host_identity), "hostingAfter": copy.deepcopy(host_identity), "leaseQueryResult": lease_pin, "calls": calls}
        observation_pin = add("observation", records["observation"])
        records["observationReview"] = {"kind": "audited-candidate-current-runtime-observation-review", "version": 1, "status": "passed",
            "observation": observation_pin, "source": source, "verification": verification,
            "checks": {key: True for key in M.CURRENT_OBSERVATION_REVIEW_CHECKS}, "calls": {key: 0 for key in calls}}
        review_pin = add("observation-review", records["observationReview"])
        envelope = {"kind": "audited-candidate-current-runtime-binding", "version": 1, "status": "reviewed_current_runtime",
            **{key: pins[key] for key in M.CURRENT_AUTHORITY_KEYS}, "recovery": {key: pins[key] for key in M.CURRENT_RECOVERY_KEYS},
            "current": copy.deepcopy(current), "preserved": copy.deepcopy(preserved), "observation": observation_pin, "observationReview": review_pin}
        envelope_pin = add("envelope", envelope)
        value = {"version": 5, "scenario": "tv-browse", **{key: pins[key] for key in M.CURRENT_AUTHORITY_KEYS}, "currentRuntime": envelope_pin}
        return epoch, envelope, value, records, raws

    def test_saved_recovery_and_synthetic_observation_have_distinct_authority(self):
        epoch, envelope, value, records, raws = self.fixture()
        original = M.canonical(epoch)
        result = M.load_current_runtime(value["currentRuntime"], value, epoch, lambda pin: json.loads(raws[pin["path"]]), lambda pin: raws[pin["path"]])
        self.assertTrue(result == envelope)
        self.assertTrue(M.canonical(epoch) == original)
        self.assertNotEqual(result["current"]["candidateProcess"]["pid"], epoch["candidateProcess"]["pid"])
        self.assertNotEqual(result["observationReview"], result["recovery"]["independentReview"])

    def test_changed_recovery_or_missing_independent_closure_cannot_bind(self):
        changes = [
            ("execution", lambda row: row.update(binaryOrEnvironmentChanges=1)),
            ("execution", lambda row: row.update(postgresRestartRequests=False)),
            ("independentReview", lambda row: row.update(allReaderClosuresVerified=False)),
            ("independentReview", lambda row: row["nativePreservation"].update(priorNativeChecksPassed=False)),
            ("independentReview", lambda row: row["candidates"]["old"].update(uniqueLeaseResponseMatchedOriginalStdout=False)),
            ("selectedResult", lambda row: row.update(cleanupErrors=["unclosed"])),
            ("selectedStartIntent", lambda row: row.update(binarySha256="f" * 64)),
            ("configuration", lambda row: row["candidates"]["old"]["environment"].update(parsed=True)),
        ]
        for key, change in changes:
            epoch, envelope, value, records, unused = self.fixture()
            change(records[key])
            with self.subTest(record=key), self.assertRaises(M.ContractError):
                M.validate_current_runtime(envelope, value, epoch, records)

    def test_current_identity_cannot_change_product_sandbox_host_or_postgres(self):
        changes = [
            lambda current: current["candidateProcess"].update(networkNamespace="net:[1]"),
            lambda current: current["candidateProcess"].update(exeInode=1),
            lambda current: current["serverProperties"].update(MainPID="1"),
            lambda current: current["serverIdentity"].update(invocationId="f" * 32),
            lambda current: current["listener"].update(port=1),
            lambda current: current["postgresProcess"].update(pid=1),
            lambda current: current["lease"].update(granted=False),
            lambda current: current["lease"].update(backendStart=None),
        ]
        for index, change in enumerate(changes):
            epoch, envelope, unused, unused_records, unused_raws = self.fixture()
            change(envelope["current"])
            with self.subTest(index=index), self.assertRaises(M.ContractError):
                M.validate_current_identity(envelope["current"], epoch)

    def test_observation_requires_actual_single_lease_result_and_separate_review(self):
        changes = [
            lambda records: records.update(leaseQueryResult=[]),
            lambda records: records["leaseQueryResult"].append(copy.deepcopy(records["leaseQueryResult"][0])),
            lambda records: records["observation"]["calls"].update(sql=0),
            lambda records: records["observation"]["calls"].update(service=1),
            lambda records: records["observationReview"]["checks"].update(uniqueLease=False),
            lambda records: records["observationReview"].update(component={"path": "/forbidden", "sha256": "f" * 64}),
            lambda records: records["observationReview"]["calls"].update(sql=1),
            lambda records: records["observation"]["hostingAfter"]["process"].update(pid=1),
        ]
        for index, change in enumerate(changes):
            epoch, envelope, value, records, unused = self.fixture()
            change(records)
            with self.subTest(index=index), self.assertRaises(M.ContractError):
                M.validate_current_runtime(envelope, value, epoch, records)

    def test_loader_rejects_digest_alias_duplicate_keys_and_configuration_paths(self):
        epoch, envelope, value, records, raws = self.fixture()
        read = Mock(side_effect=lambda pin: raws[pin["path"]])
        bad = copy.deepcopy(value["currentRuntime"])
        bad["path"] = str(M.C / "private/runtime.env")
        with self.assertRaisesRegex(M.ContractError, "current_runtime_record_scope"):
            M.load_current_runtime(bad, {**value, "currentRuntime": bad}, epoch, Mock(), read)
        read.assert_not_called()
        with self.assertRaisesRegex(M.ContractError, "current_runtime_record_digest"):
            M.load_current_runtime(value["currentRuntime"], value, epoch, Mock(), lambda pin: b"{}")
        duplicate = b'{"kind":"a","kind":"b"}'
        duplicate_pin = {"path": value["currentRuntime"]["path"], "sha256": M.digest(duplicate)}
        with self.assertRaisesRegex(M.ContractError, "current_runtime_duplicate_key"):
            M.load_current_runtime(duplicate_pin, {**value, "currentRuntime": duplicate_pin}, epoch, Mock(), lambda pin: duplicate)

    def reader(self, epoch, envelope, *, observation=False):
        current, candidate = envelope["current"], epoch["candidate"]
        provision = Mock()
        provision.units = {role: candidate["processes"][role]["Id"] for role in ("server", "postgres")}
        provision.show.side_effect = lambda unit: copy.deepcopy(current["serverProperties"] if unit == provision.units["server"] else current["postgresProperties"])
        provision.process.side_effect = lambda role, properties: copy.deepcopy(current["serverIdentity"] if role == "server" else candidate["postgresIdentity"])
        gateway = Mock()
        gateway.metadata.side_effect = lambda pid: copy.deepcopy(current["candidateProcess"] if pid == current["candidateProcess"]["pid"] else current["postgresProcess"])
        modules = {"seed": Mock(), "gateway": gateway, "provision": Mock()}
        modules["provision"].Provision.return_value = provision
        with patch.object(M, "verify_environment_epoch_files", side_effect=AssertionError("environment decode forbidden")), \
             patch.object(M, "validate_environment_append", side_effect=AssertionError("environment decode forbidden")), \
             patch.object(M.EpochReader, "__init__", side_effect=AssertionError("old constructor forbidden")):
            if observation:
                facts = copy.deepcopy(current)
                del facts["lease"]["backendStart"]
                reader = M.RecoveredEpochReader.for_observation(epoch, modules, M.R / "synthetic-current-reader", facts, envelope["preserved"])
            else:
                reader = M.RecoveredEpochReader(epoch, modules, M.R / "synthetic-current-reader", envelope)
        return reader, modules

    def test_reader_keeps_epoch_immutable_and_hashes_only_selected_files(self):
        epoch, envelope, unused, unused_records, unused_raws = self.fixture()
        before = M.canonical(epoch)
        reader, modules = self.reader(epoch, envelope)
        observed = reader.pin()
        self.assertEqual(observed["pid"], envelope["current"]["candidateProcess"]["pid"])
        self.assertTrue(M.canonical(epoch) == before)
        self.assertIs(reader.productEpoch, epoch)
        self.assertIsNot(reader.candidate, epoch["candidate"])
        allowed = [envelope["preserved"]["binary"], envelope["preserved"]["runtime"], *envelope["preserved"]["units"].values()]
        self.assertEqual([call.args for call in modules["seed"].read_checked.call_args_list], [(pin["path"], pin["sha256"]) for pin in allowed])

    def test_legacy_reader_does_not_silently_accept_the_replacement_process(self):
        epoch, envelope, unused, unused_records, unused_raws = self.fixture()
        recovered, modules = self.reader(epoch, envelope)
        historical = M.EpochReader.__new__(M.EpochReader)
        historical.epoch, historical.candidate = epoch, epoch["candidate"]
        historical.modules, historical.provision = modules, recovered.provision
        self.assertIs(historical.runtime_identity(), epoch)
        with self.assertRaisesRegex(M.ContractError, "runtime_epoch_process_changed"):
            historical.pin()

    def test_epoch_io_requires_envelope_and_pin_together_before_open(self):
        epoch, envelope, value, unused_records, unused_raws = self.fixture()
        seed = Mock()
        seed.descriptor.return_value = epoch
        for current, pin in ((envelope, None), (None, value["currentRuntime"])):
            with self.subTest(has_envelope=current is not None), self.assertRaisesRegex(M.ContractError, "current_runtime_io_pin_required"):
                M.EpochIO({}, {}, {}, value["runtimeEpoch"], value["seedBinding"], {"seed": seed},
                          current_runtime=current, current_runtime_pin=pin)
        seed.write_json_once.assert_not_called()

    def test_observation_reader_performs_one_real_lease_method_call_and_rejects_replay(self):
        epoch, envelope, unused, unused_records, unused_raws = self.fixture()
        reader, modules = self.reader(epoch, envelope, observation=True)
        lease = copy.deepcopy(envelope["current"]["lease"])
        row = {key: value for key, value in lease.items() if key != "candidateConnection"}
        reader.provision.pgroot = M.C / "postgres"
        reader.provision.value = {"ports": epoch["candidate"]["ports"]}
        reader.provision.cluster.side_effect = lambda observed: self.provision_module.Provision.cluster(reader.provision, observed)
        cluster = "|".join([str(M.C / "postgres/data"), str(epoch["candidate"]["ports"]["postgres"]),
                            epoch["candidate"]["clusterSystemIdentifier"], str(epoch["candidate"]["postgresVersionNum"])])
        reader.provision.psql.side_effect = [cluster, json.dumps([row])]
        modules["seed"].parse.side_effect = json.loads
        connection = lease["candidateConnection"]
        tcp = "header\n 0: 0100007F:%04X 0100007F:%04X 01 0 0 0 0 0 %s\n" % (connection["localPort"], connection["remotePort"], connection["socketInode"])
        postmaster = "\n".join([str(epoch["postgresProcess"]["pid"]), str(M.C / "postgres/data"), "0", str(epoch["candidate"]["ports"]["postgres"])])
        with patch.object(M.Path, "read_text", autospec=True, side_effect=lambda path: postmaster if path.name == "postmaster.pid" else tcp), \
             patch.object(M.Path, "iterdir", return_value=iter([Path("/synthetic/fd/1")])), \
             patch.object(M.os, "readlink", return_value="socket:[" + connection["socketInode"] + "]"):
            actual = reader.deployment_lease()
        self.assertTrue(actual == lease)
        self.assertNotIn("backendStart", reader.current["lease"])
        self.assertIsNone(reader.currentRuntime)
        self.assertEqual(reader.provision.psql.call_count, 2)
        query = reader.provision.psql.call_args.args[1]
        self.assertIn("FROM pg_locks", query)
        self.assertEqual(reader.provision.psql.call_args_list[0].args[0], "cluster-identity")
        self.assertEqual(reader.provision.psql.call_args_list[1].args[0], "epoch-read-only")
        self.assertIn("(pg_control_system()).system_identifier", reader.provision.psql.call_args_list[0].args[1])
        with self.assertRaisesRegex(M.ContractError, "current_runtime_observation_query_consumed"):
            reader.deployment_lease()
        self.assertEqual(reader.provision.psql.call_count, 2)

    def test_failed_cluster_check_prevents_lease_query_and_consumes_observation(self):
        epoch, envelope, unused, unused_records, unused_raws = self.fixture()
        reader, unused_modules = self.reader(epoch, envelope, observation=True)
        reader.provision.pgroot = M.C / "postgres"
        reader.provision.value = {"ports": epoch["candidate"]["ports"]}
        reader.provision.cluster.side_effect = lambda observed: self.provision_module.Provision.cluster(reader.provision, observed)
        reader.provision.psql.return_value = "wrong-directory|1|2|170011"
        postmaster = "\n".join([str(epoch["postgresProcess"]["pid"]), str(M.C / "postgres/data"), "0", str(epoch["candidate"]["ports"]["postgres"])])
        with patch.object(M.Path, "read_text", return_value=postmaster), self.assertRaisesRegex(Exception, "SQL connection reached another cluster"):
            reader.deployment_lease()
        self.assertEqual(reader.provision.psql.call_count, 1)
        self.assertEqual(reader.provision.psql.call_args.args[0], "cluster-identity")
        with self.assertRaisesRegex(M.ContractError, "current_runtime_observation_query_consumed"):
            reader.deployment_lease()
        self.assertEqual(reader.provision.psql.call_count, 1)

    def test_socket_presence_cannot_replace_the_unique_lease_query(self):
        epoch, envelope, unused, unused_records, unused_raws = self.fixture()
        for rows in ([], [{}, {}]):
            reader, unused_modules = self.reader(epoch, envelope, observation=True)
            reader.sql_json = Mock(return_value=rows)
            with patch.object(M.os, "readlink") as sockets, self.assertRaises(M.ContractError):
                reader.deployment_lease()
            sockets.assert_not_called()
            self.assertEqual(reader.sql_json.call_count, 1)


class LeaseLossRuntimeGuards(unittest.TestCase):
    """Use saved restart facts and an explicitly synthetic, unissued observation."""
    @classmethod
    def setUpClass(cls):
        RecoveredRuntimeGuards.setUpClass.__func__(cls)
        def remember(pin):
            raw = Path(pin["path"]).read_bytes()
            if M.digest(raw) != pin["sha256"] or ("bytes" in pin and len(raw) != pin["bytes"]):
                raise AssertionError("A retained lease-loss fixture pin changed")
            cls.raws[pin["path"]] = raw
            return json.loads(raw) if pin["path"].endswith(".json") else raw
        cls.previous = remember(M.CURRENT_RUNTIME_V1_PREDECESSOR)
        cls.previous_records = {key: remember(cls.previous[key]) for key in M.CURRENT_AUTHORITY_KEYS}
        cls.previous_records.update({key: remember(pin) for key, pin in cls.previous["recovery"].items()})
        for key in ("observation", "observationReview"):
            cls.previous_records[key] = remember(cls.previous[key])
        observation = cls.previous_records["observation"]
        cls.previous_records["leaseQueryResult"] = remember(observation["leaseQueryResult"])
        remember(observation["source"])
        remember(observation["verification"])
        recovery = Path("/opt/goby-test/candidate-lease-loss-recovery-20260915")
        selected = {
            "execution": ("restart-execution.json", "545bef39ba1660d118d881e9ca2de43944a98cfdb89a8946fc5fbdaccf1e8a9e"),
            "independentReview": ("restart-independent-review.json", "97e8125e97e1ba8b5cd72070249279350e0ac08340c8bdac428c9737cee7fc01"),
            "configuration": ("restart-configuration-observation.json", "c86600904fe119838fa67d2543ad2fd41356b712ac81b80a78d4f158463d0cef"),
            "selectedResult": ("restart-01/old/result.json", "cbb00c7791170d6ea7e34f3d57100b8d678d64d285adc0842b775e3287f6c835"),
            "selectedStartIntent": ("restart-01/old/start-intent.json", "6aee0d5d49c73cf37ae9c9e17d3b7f11b1001ca359ed20dfe47ee7c03cbe3e8a"),
        }
        for key, (name, sha256) in selected.items():
            cls.pins[key] = {"path": str(recovery / name), "sha256": sha256}
            cls.saved[key] = remember(cls.pins[key])
        for key in ("source", "metadata", "independentReview"):
            remember(cls.saved["configuration"][key])

    def fixture(self):
        epoch, envelope, value, records, raws = RecoveredRuntimeGuards.fixture(self)
        def add(name, row):
            path = str(M.R / "synthetic-lease-loss-runtime-checks" / (name + ".json"))
            raw = M.canonical(row)
            raws[path] = raw
            return {"path": path, "sha256": M.digest(raw)}
        # These times are synthetic and must never be published as a current lease.
        envelope["current"]["lease"]["backendStart"] = "2026-09-15T15:10:00+00:00"
        records["leaseQueryResult"][0]["backendStart"] = envelope["current"]["lease"]["backendStart"]
        observation = records["observation"]
        observation.update(before=copy.deepcopy(envelope["current"]), after=copy.deepcopy(envelope["current"]),
            capturedAt="2026-09-15T15:11:00+00:00", leaseQueryResult=add("lease-result", records["leaseQueryResult"]))
        envelope["observation"] = add("observation", observation)
        records["observationReview"]["observation"] = envelope["observation"]
        envelope["observationReview"] = add("observation-review", records["observationReview"])
        envelope.update(version=2, previousCurrentRuntime=copy.deepcopy(M.CURRENT_RUNTIME_V1_PREDECESSOR))
        value["currentRuntime"] = add("envelope", envelope)
        records["previousCurrentRuntime"] = copy.deepcopy(self.previous)
        records["previousRecords"] = copy.deepcopy(self.previous_records)
        return epoch, envelope, value, records, raws

    def test_saved_restart_accepts_one_v1_predecessor_and_keeps_history(self):
        epoch, envelope, value, records, raws = self.fixture()
        historical = M.canonical(epoch)
        previous = M.canonical(records["previousCurrentRuntime"])
        result = M.load_current_runtime(value["currentRuntime"], value, epoch,
            lambda pin: json.loads(raws[pin["path"]]), lambda pin: raws[pin["path"]])
        self.assertEqual(result, envelope)
        self.assertEqual(M.canonical(epoch), historical)
        self.assertEqual(M.canonical(records["previousCurrentRuntime"]), previous)
        self.assertEqual(len(records["execution"]["after"]["files"]), 5)

    def test_predecessor_is_fixed_v1_and_cannot_form_another_recovery_chain(self):
        for mutation in (lambda row: row["previousCurrentRuntime"].update(sha256="f" * 64),
                         lambda row: row.update(previousCurrentRuntime=copy.deepcopy(row["runtimeEpoch"]))):
            epoch, envelope, value, records, unused = self.fixture()
            mutation(envelope)
            with self.assertRaisesRegex(M.ContractError, "current_runtime_previous_pin"):
                M.validate_current_runtime(envelope, value, epoch, records)
        epoch, envelope, value, records, unused = self.fixture()
        records["previousCurrentRuntime"] = copy.deepcopy(envelope)
        with self.assertRaisesRegex(M.ContractError, "current_runtime_previous_version"):
            M.validate_current_runtime(envelope, value, epoch, records)

    def test_predecessor_must_pass_its_original_observation_and_source_guards(self):
        mutations = [lambda rows: rows["observationReview"]["checks"].update(recoveryBinding=False),
                     lambda rows: rows["runtimeEpoch"]["currentSource"]["binary"].update(sha256="f" * 64),
                     lambda rows: rows["observation"]["after"]["candidateProcess"].update(pid=1)]
        for mutation in mutations:
            epoch, envelope, value, records, unused = self.fixture()
            mutation(records["previousRecords"])
            with self.assertRaises(M.ContractError):
                M.validate_current_runtime(envelope, value, epoch, records)

    def test_start_intent_must_name_the_previous_current_process(self):
        for field in ("ExecMainPID", "InvocationID"):
            epoch, envelope, value, records, unused = self.fixture()
            intent = copy.deepcopy(records["selectedStartIntent"])
            intent["oldUnit"][field] = (str(epoch["candidateProcess"]["pid"]) if field == "ExecMainPID"
                                       else epoch["candidate"]["processes"]["server"][field])
            records["selectedStartIntent"] = intent
            records["selectedResult"]["startIntent"] = copy.deepcopy(intent)
            records["execution"]["candidates"]["old"] = copy.deepcopy(records["selectedResult"])
            records["execution"]["before"]["units"][intent["unit"]] = copy.deepcopy(intent["oldUnit"])
            with self.subTest(field=field), self.assertRaisesRegex(M.ContractError, "current_runtime_previous_start_intent"):
                M.validate_current_runtime(envelope, value, epoch, records)

    def test_both_candidates_keep_all_four_configuration_projections(self):
        for label in ("old", "fresh"):
            for key in ("unitFile", "environment", "postgresUnitFile", "postgresConfiguration"):
                epoch, envelope, value, records, unused = self.fixture()
                records["configuration"]["candidates"][label][key]["sha256"] = "f" * 64
                with self.subTest(label=label, file=key), self.assertRaisesRegex(M.ContractError, "current_runtime_lease_loss_preservation"):
                    M.validate_current_runtime(envelope, value, epoch, records)

    def test_configuration_sources_and_all_five_file_metadata_are_bound(self):
        epoch, envelope, value, records, unused = self.fixture()
        records["configuration"]["source"]["sha256"] = "f" * 64
        with self.assertRaisesRegex(M.ContractError, "current_runtime_configuration_source"):
            M.validate_current_runtime(envelope, value, epoch, records)
        for path in records["execution"]["after"]["files"]:
            epoch, envelope, value, changed, unused = self.fixture()
            changed["execution"]["after"]["files"][path]["inode"] += 1
            with self.subTest(path=path), self.assertRaisesRegex(M.ContractError, "current_runtime_lease_loss_preservation"):
                M.validate_current_runtime(envelope, value, epoch, changed)

    def test_new_recovery_requires_review_and_cannot_masquerade_as_v1(self):
        epoch, envelope, value, records, unused = self.fixture()
        del records["independentReview"]
        with self.assertRaisesRegex(M.ContractError, "current_runtime_recovery_records"):
            M.validate_current_runtime(envelope, value, epoch, records)
        epoch, envelope, value, records, unused = self.fixture()
        envelope["version"] = 1
        del envelope["previousCurrentRuntime"]
        with self.assertRaisesRegex(M.ContractError, "current_runtime_recovery_execution"):
            M.validate_current_runtime(envelope, value, epoch, records)

    def test_new_reader_keeps_historical_epoch_and_hash_only_access(self):
        epoch, envelope, unused, unused_records, unused_raws = self.fixture()
        historical = M.canonical(epoch)
        reader, modules = RecoveredRuntimeGuards.reader(self, epoch, envelope)
        self.assertEqual(reader.pin()["pid"], envelope["current"]["candidateProcess"]["pid"])
        self.assertEqual(M.canonical(epoch), historical)
        self.assertIs(reader.productEpoch, epoch)
        self.assertEqual(modules["seed"].read_checked.call_count, 4)


class OomExitRuntimeGuards(unittest.TestCase):
    """Saved Sep16 recovery plus synthetic observations; no current authority is issued."""
    @classmethod
    def setUpClass(cls):
        LeaseLossRuntimeGuards.setUpClass.__func__(cls)
        def remember(pin):
            raw = Path(pin["path"]).read_bytes()
            if M.digest(raw) != pin["sha256"] or ("bytes" in pin and len(raw) != pin["bytes"]):
                raise AssertionError("A retained second-recovery fixture pin changed")
            cls.raws[pin["path"]] = raw
            return json.loads(raw) if pin["path"].endswith(".json") else raw
        original, original_records = cls.previous, cls.previous_records
        cls.previous = remember(M.CURRENT_RUNTIME_V2_PREDECESSOR)
        cls.previous_records = {key: remember(cls.previous[key]) for key in M.CURRENT_AUTHORITY_KEYS}
        cls.previous_records.update({key: remember(pin) for key, pin in cls.previous["recovery"].items()})
        for key in ("observation", "observationReview"):
            cls.previous_records[key] = remember(cls.previous[key])
        observation = cls.previous_records["observation"]
        cls.previous_records["leaseQueryResult"] = remember(observation["leaseQueryResult"])
        remember(observation["source"])
        remember(observation["verification"])
        cls.previous_records.update(previousCurrentRuntime=original, previousRecords=original_records)
        for key, pin in M.CURRENT_RUNTIME_SECOND_RECOVERY.items():
            cls.pins[key] = copy.deepcopy(pin)
            cls.saved[key] = remember(pin)

    def fixture(self):
        epoch, envelope, value, records, raws = RecoveredRuntimeGuards.fixture(self)
        def add(name, row):
            path = str(M.R / "synthetic-second-recovery-runtime-checks" / (name + ".json"))
            raw = M.canonical(row)
            raws[path] = raw
            return {"path": path, "sha256": M.digest(raw)}
        # These times are test data, not reconstructed deployment-lease facts.
        envelope["current"]["lease"]["backendStart"] = "2026-09-16T10:00:00+00:00"
        records["leaseQueryResult"][0]["backendStart"] = envelope["current"]["lease"]["backendStart"]
        records["observation"].update(before=copy.deepcopy(envelope["current"]), after=copy.deepcopy(envelope["current"]),
            capturedAt="2026-09-16T10:01:00+00:00", leaseQueryResult=add("lease-result", records["leaseQueryResult"]))
        envelope["observation"] = add("observation", records["observation"])
        records["observationReview"]["observation"] = envelope["observation"]
        envelope["observationReview"] = add("observation-review", records["observationReview"])
        envelope.update(version=2, previousCurrentRuntime=copy.deepcopy(M.CURRENT_RUNTIME_V2_PREDECESSOR))
        value["currentRuntime"] = add("envelope", envelope)
        records.update(previousCurrentRuntime=copy.deepcopy(self.previous), previousRecords=copy.deepcopy(self.previous_records))
        return epoch, envelope, value, records, raws

    def test_actual_second_review_and_projection_load_without_rewriting_history(self):
        epoch, envelope, value, records, raws = self.fixture()
        before = M.canonical({"epoch": epoch, "previous": records["previousCurrentRuntime"], "records": records["previousRecords"]})
        actual = M.load_current_runtime(value["currentRuntime"], value, epoch,
            lambda pin: json.loads(raws[pin["path"]]), lambda pin: raws[pin["path"]])
        self.assertEqual(actual, envelope)
        self.assertEqual(before, M.canonical({"epoch": epoch, "previous": records["previousCurrentRuntime"], "records": records["previousRecords"]}))
        self.assertNotIn("nativePreservation", records["independentReview"])
        self.assertNotEqual(records["configuration"]["source"]["sha256"], records["execution"]["preservationInputs"]["readonly-execution.json"]["sha256"])

    def test_second_edge_cannot_select_unknown_ancestor_or_repeat_itself(self):
        epoch, envelope, value, records, unused = self.fixture()
        envelope["previousCurrentRuntime"]["sha256"] = "f" * 64
        with self.assertRaisesRegex(M.ContractError, "current_runtime_previous_pin"):
            M.validate_current_runtime(envelope, value, epoch, records)
        epoch, envelope, value, records, unused = self.fixture()
        records["previousCurrentRuntime"] = copy.deepcopy(envelope)
        with self.assertRaisesRegex(M.ContractError, "current_runtime_previous_version"):
            M.validate_current_runtime(envelope, value, epoch, records)
        epoch, envelope, value, records, unused = self.fixture()
        records["previousRecords"]["previousRecords"]["observationReview"]["checks"]["uniqueLease"] = False
        with self.assertRaises(M.ContractError):
            M.validate_current_runtime(envelope, value, epoch, records)

    def test_second_review_rejects_unclosed_or_mixed_candidate_evidence(self):
        mutations = [lambda row: row["closure"].update(lockFdCloseSucceeded=False),
            lambda row: row["counts"].update(uniqueDeploymentLeases=1),
            lambda row: row["protected"].update(unchangedConfigurationHashes=7),
            lambda row: row["candidateEvidence"]["old"]["snapshots"].update(source=copy.deepcopy(row["candidateEvidence"]["fresh"]["snapshots"]["source"])),
            lambda row: row["preservationInputs"]["native-independent-review.json"].update(sha256="f" * 64),
            lambda row: row.update(nativePreservation={"priorNativeChecksPassed": True})]
        for mutation in mutations:
            epoch, envelope, value, records, unused = self.fixture()
            mutation(records["independentReview"])
            with self.assertRaises(M.ContractError):
                M.validate_current_runtime(envelope, value, epoch, records)

    def test_second_configuration_cannot_be_relabeled_as_new_sql_or_drift(self):
        epoch, envelope, value, records, unused = self.fixture()
        records["configuration"]["source"] = {**records["execution"]["preservationInputs"]["readonly-execution.json"], "bytes": 44312}
        with self.assertRaisesRegex(M.ContractError, "current_runtime_configuration_lineage"):
            M.validate_current_runtime(envelope, value, epoch, records)
        for label in ("old", "fresh"):
            epoch, envelope, value, records, unused = self.fixture()
            records["execution"]["after"]["configurationHashes"][label]["environment"]["sha256"] = "f" * 64
            with self.assertRaisesRegex(M.ContractError, "current_runtime_lease_loss_preservation"):
                M.validate_current_runtime(envelope, value, epoch, records)

    def test_second_start_must_reference_the_accepted_v2_process(self):
        epoch, envelope, value, records, unused = self.fixture()
        intent = records["selectedStartIntent"]
        intent["oldUnit"]["ExecMainPID"] = str(records["previousRecords"]["previousCurrentRuntime"]["current"]["candidateProcess"]["pid"])
        records["selectedResult"]["startIntent"] = copy.deepcopy(intent)
        records["execution"]["candidates"]["old"] = copy.deepcopy(records["selectedResult"])
        records["execution"]["before"]["units"][intent["unit"]] = copy.deepcopy(intent["oldUnit"])
        with self.assertRaisesRegex(M.ContractError, "current_runtime_previous_start_intent"):
            M.validate_current_runtime(envelope, value, epoch, records)

    def test_second_recovery_still_requires_a_distinct_two_query_observation(self):
        for mutate in (lambda row: row["observation"]["calls"].update(sql=0),
                       lambda row: row["observationReview"]["checks"].update(uniqueLease=False),
                       lambda row: row["leaseQueryResult"].append(copy.deepcopy(row["leaseQueryResult"][0]))):
            epoch, envelope, value, records, unused = self.fixture()
            mutate(records)
            with self.assertRaises(M.ContractError):
                M.validate_current_runtime(envelope, value, epoch, records)
        epoch, envelope, value, records, unused = self.fixture()
        del envelope["current"]["lease"]["backendStart"]
        with self.assertRaisesRegex(M.ContractError, "current_runtime_lease_schema"):
            M.validate_current_runtime(envelope, value, epoch, records)

    def test_second_reader_keeps_hash_only_access_and_the_product_epoch(self):
        epoch, envelope, unused, unused_records, unused_raws = self.fixture()
        historical = M.canonical(epoch)
        reader, modules = RecoveredRuntimeGuards.reader(self, epoch, envelope)
        self.assertEqual(reader.pin()["pid"], 1648477)
        self.assertEqual(M.canonical(epoch), historical)
        self.assertEqual(modules["seed"].read_checked.call_count, 4)

    def test_verified_second_runtime_keeps_the_original_tv_closeout_ancestor(self):
        epoch, envelope, value, records, raws = self.fixture()
        current = M.load_current_runtime(value["currentRuntime"], value, epoch,
            lambda pin: json.loads(raws[pin["path"]]), lambda pin: raws[pin["path"]])
        transition = {"newSourceArchive": M.receipt_descriptor(M.PROGRAMS_COMPLETE_ARTIFACT_PINS["sourceArchive"]),
            "previousEpoch": envelope["runtimeEpoch"], "previousBinding": envelope["seedBinding"], "currentRuntime": value["currentRuntime"]}
        original = copy.deepcopy(M.CURRENT_RUNTIME_V1_PREDECESSOR)
        M.validate_programs_closeout_runtime(transition, current, original)
        self.assertEqual(original, M.CURRENT_RUNTIME_V1_PREDECESSOR)
        for wrong in (value["currentRuntime"], M.CURRENT_RUNTIME_V2_PREDECESSOR, {**original, "sha256": "f" * 64}):
            with self.assertRaisesRegex(M.ContractError, "programs_tv_runtime_ancestor"):
                M.validate_programs_closeout_runtime(transition, current, wrong)
        for mutate in (lambda row: row.update(previousCurrentRuntime=copy.deepcopy(M.CURRENT_RUNTIME_V1_PREDECESSOR)),
                       lambda row: row["recovery"]["execution"].update(sha256="f" * 64),
                       lambda row: row["runtimeEpoch"].update(sha256="f" * 64)):
            changed = copy.deepcopy(current)
            mutate(changed)
            with self.assertRaisesRegex(M.ContractError, "programs_tv_runtime_ancestor"):
                M.validate_programs_closeout_runtime(transition, changed, original)
        legacy = {**transition, "newSourceArchive": M.PROGRAMS_SOURCE_ARCHIVE, "currentRuntime": original}
        M.validate_programs_closeout_runtime(legacy, records["previousRecords"]["previousCurrentRuntime"], original)
        with self.assertRaisesRegex(M.ContractError, "programs_tv_evidence_changed"):
            M.validate_programs_closeout_runtime(legacy, current, value["currentRuntime"])


class ProgramsCompleteSourceGuards(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        pin = M.PROGRAMS_COMPLETE_ARTIFACT_PINS["sourceBridge"]
        raw = Path(pin["path"]).read_bytes()
        if M.digest(raw) != pin["sha256"] or len(raw) != pin["bytes"]:
            raise AssertionError("The retained complete-source bridge changed")
        cls.bridge = json.loads(raw)
        closure_raw = (M.PROGRAMS_COMPLETE_PRODUCT_ROOT / "closure.json").read_bytes()
        if len(closure_raw) != 7152 or M.digest(closure_raw) != "7bf2db69fbc55755ae08120971af8345cee98775d7435c8834ef08b5fe924390":
            raise AssertionError("The retained complete-source closure changed")
        cls.closure_adapter = json.loads(closure_raw)["adapter"]
        cls.review_adapter = {"path": str(M.PROGRAMS_COMPLETE_PRODUCT_ROOT / "private/verify-livetv-programs-final.py"),
            "bytes": 58152, "sha256": "7e061ff3db40681f8c28e86ae625bb8cac54b4ddeccade776d51048278a82ac9"}
        cls.adapter_bytes = Path(cls.review_adapter["path"]).read_bytes()
        if len(cls.adapter_bytes) != cls.review_adapter["bytes"] or M.digest(cls.adapter_bytes) != cls.review_adapter["sha256"]:
            raise AssertionError("The retained complete-source adapter changed")

    def test_saved_complete_bridge_preserves_the_real_count_scope(self):
        M.validate_programs_source_bridge_profile(self.bridge, complete=True)
        for mutate in (lambda row: row.update(frozenFiles=924, trackedBuildInputs=867),
                       lambda row: row.update(trackedInputCountScope="compiled dependencies"),
                       lambda row: row.pop("trackedInputCountScope"),
                       lambda row: row.update(gitCommit="74a69abacdd9206e51f4e166df2346b5555b5cd9"),
                       lambda row: row.update(unreviewedExtra=True)):
            changed = copy.deepcopy(self.bridge)
            mutate(changed)
            with self.assertRaisesRegex(M.ContractError, "programs_source_bridge_schema"):
                M.validate_programs_source_bridge_profile(changed, complete=True)
        with self.assertRaisesRegex(M.ContractError, "programs_source_bridge_schema"):
            M.validate_programs_source_bridge_profile(self.bridge, complete=False)

    def test_retained_pin2_adapter_uses_separately_read_source_bytes(self):
        original = M.canonical(self.closure_adapter)
        read = Mock(return_value=self.adapter_bytes)
        M.validate_programs_adapter_source(self.review_adapter, self.closure_adapter, read)
        read.assert_called_once_with(self.review_adapter)
        self.assertEqual(set(self.closure_adapter), {"path", "sha256"})
        self.assertEqual(original, M.canonical(self.closure_adapter))

    def test_adapter_projection_rejects_changed_descriptors_and_source_bytes(self):
        for target, key, value in (("review", "path", self.review_adapter["path"] + ".other"),
                                  ("closure", "path", self.review_adapter["path"] + ".other"),
                                  ("review", "sha256", "f" * 64), ("closure", "sha256", "f" * 64),
                                  ("review", "extra", True), ("closure", "bytes", self.review_adapter["bytes"])):
            review, closure = copy.deepcopy(self.review_adapter), copy.deepcopy(self.closure_adapter)
            (review if target == "review" else closure)[key] = value
            read = Mock(return_value=self.adapter_bytes)
            with self.subTest(target=target, key=key), self.assertRaises(M.ContractError):
                M.validate_programs_adapter_source(review, closure, read)
            read.assert_not_called()
        for review, content in (({**self.review_adapter, "bytes": self.review_adapter["bytes"] + 1}, self.adapter_bytes),
                                (self.review_adapter, self.adapter_bytes[:-1] + bytes([self.adapter_bytes[-1] ^ 1]))):
            read = Mock(return_value=content)
            with self.assertRaisesRegex(M.ContractError, "programs_adapter_source_bytes"):
                M.validate_programs_adapter_source(review, self.closure_adapter, read)
            read.assert_called_once_with(review)


class ProgramsLogicalGuards(unittest.TestCase):
    def snapshot(self):
        tables = {name: [] for name in M.TABLES}
        tables["schema_migrations"] = [{"version": version} for version in range(1, 29)]
        tables["task_definitions"] = [{"id": "a" * 32, "key": "library.scan", "enabled": True, "revision": 1}]
        tables["sessions"] = [{"id": format(number, "032x"), "kind": "emby", "user_id": "f" * 32,
            "token_hash": "\\x" + format(number, "064x"), "revoked_at": "2026-09-15T09:26:00+00:00"} for number in range(1, 23)]
        tables["play_sessions"] = [{"id": "play-" + str(number), "state": "Prepared" if number == 13 else "Stopped", "counted": number != 13} for number in range(14)]
        tables["user_item_data"] = [{"user_id": "user-" + str(number), "position_ticks": 123} for number in range(6)]
        tables["client_playback_references"] = [{"id": "foreign-audio-1"}, {"id": "foreign-audio-2"}]
        tables["activity_entries"] = [{"id": 1, "created_at": "2026-09-13T07:54:27.108239+00:00"}]
        return {"capturedAt": "2026-09-15T10:00:00+00:00", "tables": tables,
                "sequences": {name: {"lastValue": "72", "isCalled": True} for name in M.PROGRAMS_SEQUENCES}}

    def refresh(self):
        return {**M.PROGRAMS_REFRESH_FIELDS, "id": "b" * 32, "created_at": "2026-09-15T10:00:01+00:00", "updated_at": "2026-09-15T10:00:01+00:00"}

    def compare(self, before, after, phase="startup", added=None):
        return M.compare_programs_logical(before, after, phase=phase, window_start="2026-09-15T10:00:00+00:00",
                                         window_end="2026-09-15T10:00:03+00:00", added_definition=added,
                                         entry_source=self.snapshot() if phase == "recovery" else None)

    def test_programs_startup_retains_all_closed_history_and_only_adds_the_definition(self):
        before = self.snapshot()
        after = copy.deepcopy(before)
        after["tables"]["task_definitions"].append(self.refresh())
        original = M.canonical(before)
        self.assertEqual(self.compare(before, after), self.refresh())
        self.assertEqual(len(M.programs_sessions(before)), 22)
        self.assertEqual(M.canonical(before), original)
        self.assertEqual(after["tables"]["client_playback_references"], before["tables"]["client_playback_references"])
        self.assertIsNone(self.compare(before, before, "unchanged"))
        self.assertIsNone(self.compare(before, before, "failed_startup"))

    def test_programs_rejects_every_old_table_and_sequence_change(self):
        before = self.snapshot()
        for table in M.TABLES:
            after = copy.deepcopy(before)
            after["tables"][table].append({"id": "foreign-mutation"})
            with self.subTest(table=table), self.assertRaises(M.ContractError):
                self.compare(before, after, "unchanged")
        for name in M.PROGRAMS_SEQUENCES:
            for key, value in (("lastValue", "73"), ("isCalled", 1)):
                after = copy.deepcopy(before)
                after["sequences"][name][key] = value
                with self.subTest(sequence=name, field=key), self.assertRaises(M.ContractError):
                    self.compare(before, after, "unchanged")
        before["tables"]["schema_migrations"][0]["applied_at"] = "2026-09-13T07:00:00+00:00"
        after = copy.deepcopy(before)
        after["tables"]["schema_migrations"][0]["applied_at"] = "2026-09-15T10:00:00+00:00"
        M.validate_programs_snapshot(after)
        with self.assertRaisesRegex(M.ContractError, "old_rows_changed_schema_migrations"):
            self.compare(before, after, "unchanged")
        for key in ("tables", "sequences"):
            changed = copy.deepcopy(before)
            changed[key] = []
            with self.subTest(container=key), self.assertRaisesRegex(M.ContractError, "snapshot_inventory"):
                M.validate_programs_snapshot(changed)

    def test_programs_rejects_unreviewed_definition_defaults_time_and_identity(self):
        before = self.snapshot()
        for field, value in (("id", "invalid"), ("key", "library.other"), ("enabled", 1), ("revision", True),
                             ("name", "Different name"), ("is_hidden", True), ("schedule_timezone", "Etc/UTC"),
                             ("created_at", "2026-09-15T09:59:59+00:00"), ("updated_at", "2026-09-15T10:00:04+00:00")):
            after = copy.deepcopy(before)
            row = self.refresh()
            row[field] = value
            after["tables"]["task_definitions"].append(row)
            with self.subTest(field=field), self.assertRaises(M.ContractError):
                self.compare(before, after)
        after = copy.deepcopy(before)
        after["tables"]["task_definitions"].extend([self.refresh(), self.refresh()])
        with self.assertRaises(M.ContractError):
            self.compare(before, after)
        with self.assertRaises(M.ContractError):
            self.compare(before, before)

    def test_programs_recovery_can_only_disable_its_own_added_definition(self):
        failed = self.snapshot()
        failed["capturedAt"] = "2026-09-15T10:00:02+00:00"
        failed["tables"]["task_definitions"].append(self.refresh())
        restored = copy.deepcopy(failed)
        restored["tables"]["task_definitions"][-1].update(enabled=False, revision=2, updated_at="2026-09-15T10:00:02+00:00")
        self.assertEqual(self.compare(failed, restored, "recovery", self.refresh()), self.refresh())
        self.assertIsNone(self.compare(self.snapshot(), self.snapshot(), "recovery"))
        for mutate in (
            lambda value: value["tables"]["task_definitions"].pop(),
            lambda value: value["tables"]["task_definitions"][0].update(enabled=False),
            lambda value: value["tables"]["task_definitions"][-1].update(revision=3),
            lambda value: value["tables"]["task_definitions"][-1].update(created_at="2026-09-15T10:00:02+00:00"),
            lambda value: value["tables"]["client_playback_references"].pop(),
            lambda value: value["tables"]["play_sessions"][-1].update(state="Expired"),
            lambda value: value["sequences"]["activity_entries_id_seq"].update(lastValue="0")):
            changed = copy.deepcopy(restored)
            mutate(changed)
            with self.assertRaises(M.ContractError):
                self.compare(failed, changed, "recovery", self.refresh())
        with self.assertRaises(M.ContractError):
            self.compare(failed, restored, "recovery")

    def test_programs_recovery_cannot_designate_an_old_scan_or_unproved_row(self):
        entry = self.snapshot()
        scan = {**self.refresh(), "id": "a" * 32, "key": "library.scan", "emby_key": "RefreshLibrary", "name": "Scan media library"}
        entry["tables"]["task_definitions"] = [scan]
        changed = copy.deepcopy(entry)
        changed["tables"]["task_definitions"][0].update(enabled=False, revision=2, updated_at="2026-09-15T10:00:02+00:00")
        with self.assertRaisesRegex(M.ContractError, "requires_exact_refresh"):
            M.compare_programs_logical(entry, changed, phase="recovery", added_definition=scan, entry_source=entry)
        failed = self.snapshot()
        failed["capturedAt"] = "2026-09-15T10:00:02+00:00"
        failed["tables"]["task_definitions"].append(self.refresh())
        restored = copy.deepcopy(failed)
        restored["tables"]["task_definitions"][-1].update(enabled=False, revision=2, updated_at="2026-09-15T10:00:03+00:00")
        for addition in ({**self.refresh(), "key": "library.scan"}, {**self.refresh(), "revision": 2},
                         {**self.refresh(), "enabled": 1}):
            with self.assertRaisesRegex(M.ContractError, "requires_exact_refresh"):
                M.compare_programs_logical(failed, restored, phase="recovery", added_definition=addition, entry_source=self.snapshot())
        with self.assertRaisesRegex(M.ContractError, "requires_entry_proof"):
            M.compare_programs_logical(failed, restored, phase="recovery", added_definition=self.refresh())
        with self.assertRaisesRegex(M.ContractError, "refresh_already_present"):
            M.compare_programs_logical(failed, restored, phase="recovery", added_definition=self.refresh(), entry_source=failed)

    def test_programs_all_sessions_remain_revoked_without_hash_collisions(self):
        for mutate in (lambda row: row.update(revoked_at=None), lambda row: row.update(revoked_at="2026-09-15T10:00:00"),
                       lambda row: row.update(token_hash="\\x" + format(2, "064x")), lambda row: row.update(kind="unknown")):
            source = self.snapshot()
            mutate(source["tables"]["sessions"][0])
            with self.assertRaises(M.ContractError):
                M.programs_sessions(source)

    def test_programs_retention_uses_the_proven_thirty_days_and_full_budget(self):
        source = self.snapshot()
        state = {"source": source, "databaseNow": source["capturedAt"],
                 "fixedFiles": {str(M.C / "private/runtime.env"): {"access": "hash_only", "sha256": M.PROGRAMS_ENV_SHA}}}
        review = {"kind": "programs-transition-activity-retention-provenance", "version": 1,
            "status": "saved_provenance_reviewed_not_live_admission", "expectedActivityRetentionDays": 30,
            "basis": {"preservedRuntimeEnvironmentSha256": M.PROGRAMS_ENV_SHA, "productArchive": M.PROGRAMS_SOURCE_ARCHIVE,
                "generator": {"sha256": "9b83f402ce539155dc6d1eafc99786c87b593ca47c7834127aeaacd46335ab6a", "activityRetentionEnvironmentKeyOccurrences": 0},
                "productMembers": [{"name": name, "sha256": digest} for name, digest in M.PROGRAMS_RETENTION_MEMBERS.items()]},
            "savedActivityProjection": {"snapshot": M.PROGRAMS_PRIOR_SOURCE}}
        M.validate_programs_retention(review, state, seconds=1800)
        for mutate in (lambda value: value.update(expectedActivityRetentionDays=31),
                       lambda value: value["basis"]["generator"].update(activityRetentionEnvironmentKeyOccurrences=False),
                       lambda value: value["basis"]["productMembers"][0].update(sha256="0" * 64)):
            changed = copy.deepcopy(review)
            mutate(changed)
            with self.assertRaises(M.ContractError):
                M.validate_programs_retention(changed, state, seconds=1800)
        changed = copy.deepcopy(state)
        changed["databaseNow"] = changed["source"]["capturedAt"] = "2026-10-13T07:30:00+00:00"
        with self.assertRaisesRegex(M.ContractError, "retention_deadline"):
            M.validate_programs_retention(review, changed, seconds=1800)
        changed = copy.deepcopy(state)
        changed["fixedFiles"][str(M.C / "private/runtime.env")]["sha256"] = "0" * 64
        with self.assertRaises(M.ContractError):
            M.validate_programs_retention(review, changed, seconds=1800)


if __name__ == "__main__":
    unittest.main()
