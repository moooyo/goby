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


if __name__ == "__main__":
    unittest.main()
