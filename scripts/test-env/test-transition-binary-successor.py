#!/usr/bin/env python3
"""Pure successor guards and fixed saved-artifact replay; never candidate IO."""
import copy
from datetime import datetime, timedelta
import hashlib
import importlib.util
import io
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


M = module("successor_runtime_guards", "audited-candidate-runtime.py")
OP = module("successor_transition_guards", "transition-audited-candidate.py")
FULL = {"path": str(M.NEW_F / "report.json"), "sha256": "2dc580db7e44bc2b01f6dc843f147d70588fa381bfd706560fbec2f68b228bfc"}
WORKER = {"path": str(M.NEW_F / "worker-report.json"), "sha256": "4d65de4801e261de49ed41024a0506d23a521957ee4b312494a12b5031bf99f5"}
BINARY = {"path": str(M.NEW_F / "bin/goby-linux-amd64"), "sha256": "b0d6769cadc525b12d2970a206d8e141a39431ee72bb4f7be77bbeecf873ea42"}
OUTPUT = M.R / "candidate-tv-parent-transition-99"


def identity(pin):
    return pin["path"], pin["sha256"]


def instant(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def memory_pin(path, value):
    return {"path": str(path), "sha256": hashlib.sha256(M.canonical(value)).hexdigest()}


class SuccessorGuards(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.documents = {}

        def frozen(pin):
            raw = Path(pin["path"]).read_bytes()
            if hashlib.sha256(raw).hexdigest() != pin["sha256"]:
                raise AssertionError("saved_fixture_digest_changed")
            value = json.loads(raw)
            cls.documents[identity(pin)] = value
            return value

        cls.state = frozen(M.SUCCESSOR_STATE)
        cls.summary = frozen(M.SUCCESSOR_SUMMARY)
        cls.prior_source = frozen(M.SUCCESSOR_PRIOR_SOURCE)
        cls.prior_closeout = frozen(M.SUCCESSOR_PRIOR_CLOSEOUT)
        cls.parent = frozen(M.CURRENT_ENV_EPOCH)
        cls.parent_binding = frozen(M.CURRENT_ENV_BINDING)
        cls.original = frozen(M.PREVIOUS_BINARY_EPOCH)
        cls.configuration = frozen(cls.parent["transitionInput"])
        cls.original_input = frozen(cls.original["transitionInput"])
        cls.seed = frozen(M.SEED)
        cls.admission = frozen(M.ADMISSION04)
        cls.inactive = frozen(M.INACTIVE_STAGE)
        cls.full = frozen(FULL)
        cls.worker = frozen(WORKER)
        cls.sources = frozen(M.NEW_SOURCE_MANIFEST)
        cls.now = instant(cls.state["capturedAt"])

    def input_fixture(self):
        return {"kind": "audited-candidate-transition-input", "version": 2, "output": str(OUTPUT),
                "previousEpoch": copy.deepcopy(M.CURRENT_ENV_EPOCH), "previousBinding": copy.deepcopy(M.CURRENT_ENV_BINDING),
                "reviewedSummary": copy.deepcopy(M.SUCCESSOR_SUMMARY), "reviewedState": copy.deepcopy(M.SUCCESSOR_STATE),
                "priorCloseout": copy.deepcopy(M.SUCCESSOR_PRIOR_CLOSEOUT), "priorSource": copy.deepcopy(M.SUCCESSOR_PRIOR_SOURCE),
                "newFullReport": copy.deepcopy(FULL), "newSourceManifest": copy.deepcopy(M.NEW_SOURCE_MANIFEST),
                "newBinary": copy.deepcopy(BINARY), "compiledCatalog": {"path": str(M.NEW_F / "source" / M.CATALOG_RELATIVE),
                    "sha256": self.sources[M.CATALOG_RELATIVE]["sha256"]},
                "frontendReport": copy.deepcopy(self.parent["candidate"]["frontendReport"]),
                "helpers": copy.deepcopy(self.parent["helpers"]), "budgets": copy.deepcopy(M.LIMITS)}

    def programs_input_fixture(self):
        value = self.input_fixture()
        value.update(version=3, operationKind="programs_successor", output=str(M.R / "candidate-programs-transition-99"),
            previousEpoch=copy.deepcopy(M.PROGRAMS_PREVIOUS_EPOCH), previousBinding=copy.deepcopy(M.PROGRAMS_PREVIOUS_BINDING),
            currentRuntime=copy.deepcopy(M.PROGRAMS_CURRENT_RUNTIME), priorSource=copy.deepcopy(M.PROGRAMS_PRIOR_SOURCE),
            priorCloseout=copy.deepcopy(M.PROGRAMS_PRIOR_CLOSEOUT), newSourceArchive=copy.deepcopy(M.PROGRAMS_SOURCE_ARCHIVE),
            newSourceManifest=copy.deepcopy(M.PROGRAMS_SOURCE_MANIFEST), recoveryPolicy=copy.deepcopy(M.PROGRAMS_RECOVERY_POLICY))
        for key, name in (("newFullReport", "execution.json"), ("newArtifactReceipt", "artifact-receipt.json"),
                          ("newBuildManifest", "artifacts/linux-amd64-systemd/manifest.json"),
                          ("newBinary", "artifacts/linux-amd64-systemd/goby"), ("sourceBridge", "private/build-source-bridge.json")):
            value[key] = {"path": str(M.PROGRAMS_PRODUCT_ROOT / name), "sha256": M.digest(("synthetic-" + name).encode())}
        return value

    def test_programs_input_keeps_historical_authorities_and_binds_the_new_product(self):
        value = self.programs_input_fixture()
        self.assertIs(M.validate_transition_input(value), value)
        for mutate in (lambda row: row.update(previousEpoch=M.CURRENT_ENV_EPOCH),
                       lambda row: row.update(currentRuntime={"path": "elsewhere", "sha256": "0" * 64}),
                       lambda row: row.update(priorSource=M.SUCCESSOR_PRIOR_SOURCE),
                       lambda row: row["newSourceArchive"].update(sha256="0" * 64),
                       lambda row: row["newSourceManifest"].update(sha256="0" * 64),
                       lambda row: row["newBinary"].update(sha256=M.PROGRAMS_E11_BINARY),
                       lambda row: row["newBinary"].update(sha256=M.PROGRAMS_OLD_BINARY),
                       lambda row: row["recoveryPolicy"].update(automatic=True),
                       lambda row: row["budgets"].update(stopCalls=True),
                       lambda row: row.update(version=True)):
            changed = copy.deepcopy(value)
            mutate(changed)
            with self.assertRaises(M.ContractError):
                M.validate_transition_input(changed)
        self.assertEqual(M.validate_transition_input(self.input_fixture())["version"], 2)

    def test_complete_programs_input_cannot_mix_profiles_or_use_the_ordinary_binary(self):
        value = self.programs_input_fixture()
        for key, target in (("newSourceArchive", "sourceArchive"), ("newSourceManifest", "sourceManifest"),
                            ("newBinary", "newBinary"), ("newBuildManifest", "buildManifest"), ("sourceBridge", "sourceBridge")):
            value[key] = M.receipt_descriptor(M.PROGRAMS_COMPLETE_ARTIFACT_PINS[target])
        for key, name in (("newFullReport", "execution.json"), ("newArtifactReceipt", "artifact-receipt.json")):
            value[key] = {"path": str(M.PROGRAMS_COMPLETE_PRODUCT_ROOT / name), "sha256": M.digest(("synthetic-" + name).encode())}
        self.assertIs(M.validate_transition_input(value), value)
        mutations = [lambda row: row.update(newSourceManifest=copy.deepcopy(M.PROGRAMS_SOURCE_MANIFEST)),
            lambda row: row.update(newSourceArchive=copy.deepcopy(M.PROGRAMS_SOURCE_ARCHIVE)),
            lambda row: row["newBinary"].update(sha256="c4f46ee9a9127df502eac3145f64202aed503c2289cf50ef79fab16e6ba84c5d"),
            lambda row: row["sourceBridge"].update(path=str(M.PROGRAMS_PRODUCT_ROOT / "private/build-source-bridge.json")),
            lambda row: row["newBuildManifest"].update(sha256="f" * 64)]
        for mutation in mutations:
            changed = copy.deepcopy(value)
            mutation(changed)
            with self.assertRaises(M.ContractError):
                M.validate_transition_input(changed)
        self.assertEqual(M.validate_transition_input(self.programs_input_fixture())["currentRuntime"], M.PROGRAMS_CURRENT_RUNTIME)

    def test_programs_v4_epoch_and_binding_use_the_existing_public_validators(self):
        epoch, unused, unused_records = self.epoch_fixture()
        value = self.programs_input_fixture()
        selected = memory_pin(Path(value["output"]).with_name("programs-synthetic-input.json"), value)
        epoch.update(version=4, operationKind="programs_successor", previousEpoch=M.PROGRAMS_PREVIOUS_EPOCH,
            previousCurrentRuntime=M.PROGRAMS_CURRENT_RUNTIME, transitionInput=selected, productInput=selected,
            sourceBefore=memory_pin(Path(value["output"]) / "private/source-before.json", self.prior_source),
            sourceAfter=memory_pin(Path(value["output"]) / "private/source-after.json", self.prior_source))
        epoch["currentSource"].update(archiveSha256=M.PROGRAMS_SOURCE_ARCHIVE["sha256"], sourceManifest=value["newSourceManifest"],
            binary={"path": str(M.C / "install/goby"), "sha256": value["newBinary"]["sha256"]}, fullReport=value["newFullReport"],
            artifactReceipt=value["newArtifactReceipt"], buildManifest=value["newBuildManifest"], sourceBridge=value["sourceBridge"])
        epoch["candidate"].update(input=selected, productInput=selected, binary=copy.deepcopy(epoch["currentSource"]["binary"]),
            currentSourceManifest=value["newSourceManifest"], backendReport=value["newFullReport"])
        self.assertIs(M.validate_epoch(epoch), epoch)
        epoch_pin = memory_pin(Path(value["output"]) / "private/runtime-epoch.json", epoch)
        binding = {"kind": "audited-candidate-seed-runtime-binding", "version": 4, "runtimeEpoch": epoch_pin, "originalSeed": M.SEED,
            "seedExecutor": self.seed["helper"], "seedInput": self.seed["input"], "seedSessionAddendum": M.SEED_ADDENDUM, "admission02": M.ADMISSION02,
            **{key: self.seed[key] for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources")},
            "seedCleanup": self.seed["cleanup"], "currentSessions": M.programs_sessions(self.prior_source),
            "previousBinding": M.PROGRAMS_PREVIOUS_BINDING, "previousCurrentRuntime": M.PROGRAMS_CURRENT_RUNTIME,
            "reviewedState": epoch["reviewedState"], "reviewedSummary": epoch["reviewedSummary"], "priorCloseout": M.PROGRAMS_PRIOR_CLOSEOUT,
            "priorSource": M.PROGRAMS_PRIOR_SOURCE, "candidateAdmissionComplete": False}
        self.assertIs(M.validate_seed_runtime_binding(binding, epoch_pin, epoch, self.seed), binding)
        for mutate in (lambda row: row.update(version=3), lambda row: row.pop("sourceAfter"),
                       lambda row: row["currentSource"].pop("artifactReceipt"),
                       lambda row: row["currentSource"].update(schema=True),
                       lambda row: row.update(previousCurrentRuntime=M.CURRENT_ENV_EPOCH)):
            changed = copy.deepcopy(epoch)
            mutate(changed)
            with self.assertRaises(M.ContractError):
                M.validate_epoch(changed)
        changed = copy.deepcopy(binding)
        changed["currentSessions"][0]["revokedAt"] = None
        with self.assertRaises(M.ContractError):
            M.validate_seed_runtime_binding(changed, epoch_pin, epoch, self.seed)

    def test_programs_artifact_pins_do_not_relax_legacy_descriptors(self):
        pin = {"path": str(M.PROGRAMS_PRODUCT_ROOT / "artifact-receipt.json"), "sha256": "a" * 64, "bytes": 123}
        self.assertEqual(M.programs_artifact_pin(pin), {"path": pin["path"], "sha256": pin["sha256"]})
        with self.assertRaises(M.ContractError):
            M.descriptor(pin)
        for changed in ({**pin, "bytes": True}, {**pin, "bytes": 0}, {**pin, "bytes": -1}, {**pin, "extra": "ignored"}):
            with self.assertRaises(M.ContractError):
                M.programs_artifact_pin(changed)
        member = {"archive": pin, "member": "ram/synthetic/worker-report.json", "sha256": "b" * 64, "bytes": 12}
        self.assertIs(M.programs_archive_member(member), member)
        for name in ("../worker-report.json", "/worker-report.json", "ram//worker-report.json", "ram/./worker-report.json", "ram\\worker-report.json"):
            with self.assertRaises(M.ContractError):
                M.programs_archive_member({**member, "member": name})

    def test_programs_missing_actual_artifact_cannot_open_or_replace(self):
        job = OP.ProgramsSuccessor.__new__(OP.ProgramsSuccessor)
        job.r, job.value, job.output = M, self.programs_input_fixture(), M.R / "candidate-programs-transition-99"
        job.s, job.open = SimpleNamespace(descriptor=Mock()), Mock()
        job.calls = {"stop": 0, "replace": 0, "start": 0}
        with patch.object(OP.os.path, "lexists", return_value=False), patch.object(M, "read_programs_bytes", side_effect=FileNotFoundError("actual receipt absent")), \
                patch.object(OP.os, "replace") as replace, self.assertRaises(FileNotFoundError):
            job.run()
        job.open.assert_not_called()
        replace.assert_not_called()
        self.assertEqual(job.calls, {"stop": 0, "replace": 0, "start": 0})

    def test_programs_secret_files_are_stat_only_and_unknown_keys_are_rejected(self):
        job = OP.ProgramsSuccessor.__new__(OP.ProgramsSuccessor)
        job.r = M
        info = SimpleNamespace(st_mode=0o100600, st_nlink=1, st_dev=1, st_ino=2, st_uid=995, st_gid=995, st_size=32, st_mtime_ns=3, st_ctime_ns=4)
        job.s = SimpleNamespace(safe_path=Mock(return_value=info), pwd=SimpleNamespace(getpwnam=lambda unused: SimpleNamespace(pw_uid=995)))
        for path in M.PROGRAMS_STAT_ONLY:
            with patch.object(OP.os.path, "lexists", return_value=True), patch.object(OP.os, "open", side_effect=AssertionError("secret_open_forbidden")):
                facts, content = job.file(path)
            self.assertIsNone(content)
            self.assertEqual(facts["access"], "stat_only")
            self.assertNotIn("sha256", facts)
        with patch.object(OP.os, "open", side_effect=AssertionError("secret_open_forbidden")), self.assertRaises(M.ContractError):
            job.file(M.C / "data/unreviewed.key")
        with patch.object(OP.os.path, "lexists", return_value=False):
            facts, content = job.file(M.C / "data/master.key")
        self.assertEqual(facts, {"access": "stat_only", "absent": True})
        self.assertIsNone(content)

    def test_programs_recovery_input_is_explicit_and_bound_to_one_failed_attempt(self):
        transition = self.programs_input_fixture()
        value = {"kind": "audited-candidate-programs-recovery-input", "version": 1, "output": str(M.R / "candidate-programs-recovery-99"),
            "transitionInput": memory_pin(M.R / "programs-input.json", transition),
            "transitionFailure": {"path": str(Path(transition["output"]) / "private/failure.json"), "sha256": "a" * 64},
            "recoveryReview": {"path": str(M.R / "programs-recovery-review.json"), "sha256": "b" * 64},
            "helpers": transition["helpers"], "budgets": copy.deepcopy(M.LIMITS)}
        self.assertIs(M.validate_programs_recovery_input(value, transition), value)
        for mutate in (lambda row: row.update(version=True), lambda row: row.update(output=transition["output"]),
                       lambda row: row["transitionFailure"].update(path=str(M.R / "another/failure.json")),
                       lambda row: row["budgets"].update(startCalls=2)):
            changed = copy.deepcopy(value)
            mutate(changed)
            with self.assertRaises(M.ContractError):
                M.validate_programs_recovery_input(changed, transition)

    def programs_state_fixture(self, restarted=False):
        captured = self.restarted_state()[0] if restarted else copy.deepcopy(self.state)
        state = {key: copy.deepcopy(captured[key]) for key in M.PROGRAMS_STATE_KEYS if key in captured}
        state.update(previousEpoch=M.PROGRAMS_PREVIOUS_EPOCH, seedBinding=M.PROGRAMS_PREVIOUS_BINDING,
            priorSource=M.PROGRAMS_PRIOR_SOURCE, currentRuntime=M.PROGRAMS_CURRENT_RUNTIME,
            databaseNow=state["source"]["capturedAt"], postgresProcess=copy.deepcopy(state["postgresBefore"]))
        state["fixedFiles"][str(M.C / "private/runtime.env")].update(access="hash_only", sha256=M.PROGRAMS_ENV_SHA)
        state["fixedFiles"][str(M.C / "private/runtime.env")].pop("prefixSha256", None)
        for path in M.PROGRAMS_STAT_ONLY:
            facts = state["fixedFiles"].setdefault(path, {"absent": True})
            facts.pop("sha256", None)
            facts.pop("prefixSha256", None)
            facts["access"] = "stat_only"
        for unit in M.PROGRAMS_PROTECTED:
            state["protected"].setdefault(unit, {"Id": unit, "MainPID": "123", "InvocationID": "f" * 32})
        state["fixedFiles"][str(M.C / "install/goby")]["sha256"] = self.programs_input_fixture()["newBinary"]["sha256"] if restarted else M.PROGRAMS_OLD_BINARY
        return state

    def programs_recovery_fixture(self, running=True):
        transition = self.programs_input_fixture()
        entry = self.programs_state_fixture()
        failed = self.programs_state_fixture(restarted=running)
        transition_pin = memory_pin(M.R / "programs-synthetic-input.json", transition)
        before_pin = memory_pin(Path(transition["output"]) / "private/before.json", entry)
        copy_pin = {"path": str(Path(transition["output"]) / "private/goby-before.bin"), "sha256": M.PROGRAMS_OLD_BINARY}
        properties = copy.deepcopy(self.parent["candidate"]["processes"]["server"])
        prior_properties = copy.deepcopy(properties)
        server_identity = copy.deepcopy(self.parent["candidate"]["serverIdentity"])
        original_process = {key: value for key, value in entry["candidateAfter"].items() if key != "listener"}
        if running:
            process = failed["candidateAfter"]
            properties.update(MainPID=str(process["pid"]), InvocationID="e" * 32)
            server_identity.update(pid=process["pid"], startTicks=process["startTicks"], executableInode=process["exeInode"], invocationId="e" * 32)
            created = (instant(entry["databaseNow"]) + timedelta(seconds=1)).isoformat()
            failed["source"]["tables"]["task_definitions"].append({**M.PROGRAMS_REFRESH_FIELDS, "id": "d" * 32, "created_at": created, "updated_at": created})
        else:
            properties.update(MainPID="0", ActiveState="inactive", SubState="dead")
            server_identity = None
            failed["candidateBefore"] = failed["candidateAfter"] = failed["leaseBefore"] = failed["leaseAfter"] = None
            for row in failed["diagnostics"]["registry"]["files"]:
                row.update(closed=True, size=failed["diagnostics"]["files"][row["name"]]["bytes"])
        failed_pin = memory_pin(M.R / "programs-synthetic-failed-state.json", failed)
        failure = {"kind": "audited-programs-transition-failure", "version": 1, "status": "transition_failed_resources_retained",
            "input": transition_pin, "output": transition["output"], "before": before_pin, "oldBinaryCopy": copy_pin,
            "automaticRetry": False, "automaticRollback": False, "calls": {"stop": 1, "replace": 1 if running else 0, "start": 1 if running else 0},
            "commandResponsibilities": [{"processGroupClosed": True, "outcome": "acknowledged", "timedOut": False, "descendantsRemained": False, "exitCode": 0, "sqlOutcome": None}],
            "failureCapture": {"status": "captured_awaiting_independent_review", "state": failed_pin,
                               "serverProperties": copy.deepcopy(properties), "serverIdentity": copy.deepcopy(server_identity)}}
        failure_pin = memory_pin(Path(transition["output"]) / "private/failure.json", failure)
        review = {"kind": "audited-programs-transition-failure-review", "version": 1, "status": "failed_transition_state_reviewed",
            "transitionInput": transition_pin, "transitionFailure": failure_pin, "before": before_pin,
            "failedState": failed_pin, "oldBinaryCopy": copy_pin,
            "mode": "restore_original" if running else "start_original_installed", "serverProperties": properties, "serverIdentity": server_identity}
        recovery = {"kind": "audited-candidate-programs-recovery-input", "version": 1, "output": str(M.R / "candidate-programs-recovery-99"),
            "transitionInput": transition_pin, "transitionFailure": failure_pin, "recoveryReview": memory_pin(M.R / "programs-synthetic-recovery-review.json", review),
            "helpers": transition["helpers"], "budgets": copy.deepcopy(M.LIMITS)}
        current = {"current": {"candidateProcess": original_process, "serverProperties": prior_properties}}
        return tuple(copy.deepcopy(part) for part in (recovery, transition, review, failure, entry, failed, current))

    def test_programs_full_state_and_two_recovery_entry_modes_have_explicit_proofs(self):
        for running in (False, True):
            values = self.programs_recovery_fixture(running)
            self.assertEqual(set(values[4]), M.PROGRAMS_STATE_KEYS)
            self.assertEqual(set(values[5]), M.PROGRAMS_STATE_KEYS)
            M.validate_programs_state(values[4])
            M.validate_programs_state(values[5])
            addition = M.validate_programs_recovery_review(*values)
            self.assertEqual(addition is not None, running)
            missing = copy.deepcopy(values[4])
            del missing["postgresProcess"]
            with self.assertRaisesRegex(M.ContractError, "programs_state_authority"):
                M.validate_programs_state(missing)
            changed = copy.deepcopy(values[4])
            changed["postgresProcess"]["pid"] += 1
            with self.assertRaisesRegex(M.ContractError, "programs_postgres_projection"):
                M.validate_programs_state(changed)

    def test_programs_recovery_rejects_unknown_commands_unrelated_changes_and_false_identity(self):
        for mutate in (lambda row: row[3]["commandResponsibilities"][0].update(outcome="unknown"),
                       lambda row: row[3]["commandResponsibilities"][0].update(sqlOutcome="unknown"),
                       lambda row: row[3]["commandResponsibilities"][0].update(processGroupClosed=False),
                       lambda row: row[3]["failureCapture"].update(status="incomplete_capture_retained"),
                       lambda row: row[2]["failedState"].update(sha256="0" * 64),
                       lambda row: row[3]["calls"].update(stop=0),
                       lambda row: row[2].update(mode="retry_successor"),
                       lambda row: row[2]["oldBinaryCopy"].update(sha256="0" * 64),
                       lambda row: row[5]["source"]["tables"]["users"][0].update(name="different"),
                       lambda row: row[5]["source"]["tables"]["task_definitions"][0].update(enabled=False),
                       lambda row: row[2]["serverProperties"].update(MainPID="1"),
                       lambda row: row[2]["serverIdentity"].update(executableInode=0)):
            values = copy.deepcopy(self.programs_recovery_fixture())
            mutate(values)
            with self.assertRaises(M.ContractError):
                M.validate_programs_recovery_review(*values)

    def test_programs_recovery_rechecks_shutdown_before_starting_the_old_binary(self):
        values = self.programs_recovery_fixture()
        running = values[5]
        stopped = copy.deepcopy(running)
        stopped["candidateBefore"] = stopped["candidateAfter"] = stopped["leaseBefore"] = stopped["leaseAfter"] = None
        for row in stopped["diagnostics"]["registry"]["files"]:
            row.update(closed=True, size=stopped["diagnostics"]["files"][row["name"]]["bytes"])
        M.compare_programs_failed_state(running, stopped, values[1], allow_startup=False)
        stopped["source"]["tables"]["play_sessions"][0]["state"] = "Expired"
        with self.assertRaisesRegex(M.ContractError, "old_rows_changed_play_sessions"):
            M.compare_programs_failed_state(running, stopped, values[1], allow_startup=False)

    def test_programs_recovery_claim_cannot_be_reused_from_another_output(self):
        job = OP.ProgramsRecovery.__new__(OP.ProgramsRecovery)
        job.r, job.value = M, self.programs_input_fixture()
        job.s = SimpleNamespace(write_json_once=Mock())
        with patch.object(OP.os.path, "lexists", return_value=True), self.assertRaisesRegex(M.ContractError, "already_consumed"):
            job.claim_recovery()
        job.s.write_json_once.assert_not_called()
        job.input_pin = {"path": str(M.R / "recovery-input.json"), "sha256": "a" * 64}
        job.source_pin = {"path": str(M.R / "transition.py"), "sha256": "b" * 64}
        job.transition_input_pin = {"path": str(M.R / "original-input.json"), "sha256": "c" * 64}
        job.recovery_value = {"transitionFailure": {"path": str(M.R / "failure.json"), "sha256": "d" * 64}}
        job.recovery_review = {"mode": "restore_original"}
        with patch.object(OP.os.path, "lexists", return_value=False):
            job.claim_recovery()
        self.assertEqual(job.s.write_json_once.call_args.args[0], Path(job.value["output"]) / "private/recovery-claim.json")
        self.assertIs(job.s.write_json_once.call_args.args[1]["automatic"], False)

    def test_programs_capture_is_not_a_partial_transition_or_fake_tv_input(self):
        transition = self.programs_input_fixture()
        capture = {key: copy.deepcopy(transition[key]) for key in M.PROGRAMS_CAPTURE_INPUT_KEYS if key in transition}
        capture.update(kind="audited-candidate-state-capture-input", version=2, output=str(M.R / "candidate-programs-state-capture-99"),
                       retentionReview={"path": str(M.R / "retention-review.json"), "sha256": "a" * 64})
        self.assertIs(M.validate_programs_capture_input(capture), capture)
        with self.assertRaises(M.ContractError):
            M.validate_programs_successor_input(capture)
        for changed in ({**capture, "scenario": "tv-browse"}, {**capture, "version": 5}, {**capture, "newBinary": transition["newBinary"]}):
            with self.assertRaises(M.ContractError):
                M.validate_programs_capture_input(changed)

    def test_programs_schema_resources_must_match_individually_not_only_by_schema_number(self):
        previous = copy.deepcopy(self.sources)
        current = copy.deepcopy(previous)
        for row in current.values():
            row["mode"] = 0o644
        M.validate_programs_source_compatibility(current, previous)
        selected = [name for name in previous if name.startswith("internal/database/migrations/") or name.startswith("internal/backuppg/catalogs/") and name.endswith(".json")]
        self.assertEqual(len(selected), 34)
        for name in selected:
            changed = copy.deepcopy(current)
            changed[name]["sha256"] = "0" * 64
            with self.subTest(member=name), self.assertRaisesRegex(M.ContractError, "same_schema_resources"):
                M.validate_programs_source_compatibility(changed, previous)

    def test_programs_environment_is_hashed_without_decoding_or_returning_content(self):
        class Stream(io.BytesIO):
            def fileno(self):
                return 17
        content = b"SYNTHETIC_SETTING=retained\n"
        info = SimpleNamespace(st_mode=0o100600, st_nlink=1, st_dev=1, st_ino=2, st_uid=0, st_gid=0,
                               st_size=len(content), st_mtime_ns=3, st_ctime_ns=4)
        job = OP.ProgramsSuccessor.__new__(OP.ProgramsSuccessor)
        job.r = M
        job.s = SimpleNamespace(safe_path=Mock(return_value=info), file_identity=lambda row: (row.st_dev, row.st_ino, row.st_size, row.st_mtime_ns, row.st_ctime_ns),
            pwd=SimpleNamespace(getpwnam=lambda unused: SimpleNamespace(pw_uid=995)), parse=Mock(side_effect=AssertionError("environment_decode_forbidden")))
        with patch.object(M, "PROGRAMS_ENV_SHA", M.digest(content)), patch.object(OP.os, "open", return_value=17), \
                patch.object(OP.os, "fdopen", return_value=Stream(content)), patch.object(OP.os, "fstat", return_value=info), \
                patch.object(Path, "lstat", return_value=info):
            facts, returned = job.file(M.C / "private/runtime.env")
        self.assertEqual(facts["sha256"], M.digest(content))
        self.assertEqual(facts["access"], "hash_only")
        self.assertIsNone(returned)
        job.s.parse.assert_not_called()

    def test_programs_recovery_stops_on_shutdown_drift_before_replacement_or_start(self):
        values = self.programs_recovery_fixture()
        job = OP.ProgramsRecovery.__new__(OP.ProgramsRecovery)
        job.r, job.recovery_value, job.value, job.recovery_review = M, values[0], values[1], values[2]
        job.failed_state = values[5]
        job.failed_process = {key: value for key, value in values[5]["candidateAfter"].items() if key != "listener"}
        job.output, job.private = Path(values[0]["output"]), Path(values[0]["output"]) / "private"
        job.input_pin, job.source_pin, job.transition_input_pin = values[0]["recoveryReview"], values[0]["transitionFailure"], values[0]["transitionInput"]
        job.calls, job.retention_review = {"stop": 0, "replace": 0, "start": 0}, {}
        job.s = SimpleNamespace(write_json_once=Mock(return_value={"path": "synthetic", "sha256": "a" * 64}), sync_dir=Mock())
        job.p = SimpleNamespace(units={"server": self.parent["candidate"]["processes"]["server"]["Id"]}, command=Mock(), start=Mock())
        job.recovery_saved, job.recovery_context, job.pin, job.assert_stopped = Mock(), Mock(), Mock(), Mock()
        job.remaining, job.save = Mock(return_value=900), Mock()
        job.capture = Mock(side_effect=[(copy.deepcopy(values[5]), values[2]["failedState"]), (copy.deepcopy(values[5]), values[2]["failedState"])])
        with patch.object(OP.os.path, "lexists", return_value=False), patch.object(OP.os, "mkdir"), \
                patch.object(OP.os, "replace") as replace, patch.object(OP.signal, "setitimer"), \
                patch.object(M, "validate_programs_retention"), \
                patch.object(M, "compare_programs_failed_state", side_effect=M.ContractError("shutdown_state_drift")), \
                self.assertRaisesRegex(M.ContractError, "shutdown_state_drift"):
            job.run()
        self.assertEqual(job.calls, {"stop": 1, "replace": 0, "start": 0})
        job.p.command.assert_called_once()
        replace.assert_not_called()
        job.p.start.assert_not_called()

    def test_programs_recovery_rejects_unreviewed_state_before_output_creation(self):
        job = OP.ProgramsRecovery.__new__(OP.ProgramsRecovery)
        job.recovery_saved = Mock(side_effect=M.ContractError("unreviewed_failed_state"))
        with patch.object(OP.os, "mkdir") as mkdir, patch.object(OP.os, "replace") as replace, \
                self.assertRaisesRegex(M.ContractError, "unreviewed_failed_state"):
            job.run()
        mkdir.assert_not_called()
        replace.assert_not_called()

    def saved_reader(self, additional=None):
        documents = copy.deepcopy(self.documents)
        documents.update(additional or {})
        seen = []

        def read(pin):
            key = identity(pin)
            if key not in documents:
                raise AssertionError("unexpected_saved_descriptor")
            seen.append(key)
            return copy.deepcopy(documents[key])

        return read, documents, seen

    def restarted_state(self):
        """Simulate only the permitted process, binary, lease, and log changes."""
        after = copy.deepcopy(self.state)
        after["capturedAt"] = (self.now + timedelta(seconds=2)).isoformat()
        after["previousEpoch"] = after.pop("runtimeEpoch")
        for section in ("source", "inactiveStage"):
            after[section]["capturedAt"] = after["capturedAt"]
        process = copy.deepcopy(self.parent["candidateProcess"])
        process.update(pid=process["pid"] + 100000, startTicks=str(int(process["startTicks"]) + 100000),
                       exeInode=process["exeInode"] + 1)
        listener = copy.deepcopy(after["candidateAfter"]["listener"])
        listener["socketInode"] = str(int(listener["socketInode"]) + 1)
        after["candidateBefore"] = after["candidateAfter"] = {**process, "listener": listener}
        lease = copy.deepcopy(after["leaseAfter"])
        lease["backendPid"] += 100000
        lease["candidateConnection"]["pid"] = process["pid"]
        after["leaseBefore"] = copy.deepcopy(lease)
        after["leaseAfter"] = copy.deepcopy(lease)
        installed = {"path": str(M.C / "install/goby"), "sha256": BINARY["sha256"]}
        facts = after["fixedFiles"][installed["path"]]
        facts.update(sha256=installed["sha256"], bytes=self.worker["binary"]["bytes"], ino=facts["ino"] + 1,
                     mtimeNs=facts["mtimeNs"] + 1, ctimeNs=facts["ctimeNs"] + 1)
        for name, facts in after["unitLogs"].items():
            facts["prefixSha256"] = facts["sha256"]
            facts["bytes"] += 1
            facts["sha256"] = M.digest((name + "-synthetic-append").encode())
        diagnostics = after["diagnostics"]
        previous_active = next(row for row in diagnostics["registry"]["files"] if row["closed"] is False)
        new_entry = copy.deepcopy(previous_active)
        for entry in diagnostics["registry"]["files"]:
            entry["closed"] = True
            facts = diagnostics["files"][entry["name"]]
            facts["prefixSha256"] = facts["sha256"]
            entry["size"] = facts["bytes"]
        name = "goby-" + diagnostics["registry"]["token"] + "-" + "f" * 32 + ".jsonl"
        self.assertNotIn(name, diagnostics["files"])
        new_facts = copy.deepcopy(diagnostics["files"][previous_active["name"]])
        new_facts.update(ino=max(row["ino"] for row in diagnostics["files"].values()) + 1, bytes=0,
                         sha256=M.digest(b""))
        new_facts.pop("prefixSha256", None)
        new_entry.update(name=name, closed=False, size=0, created=after["capturedAt"],
                         identity={"device": new_facts["dev"], "inode": new_facts["ino"]})
        diagnostics["registry"]["files"].append(new_entry)
        diagnostics["files"][name] = new_facts
        registry_raw = M.canonical(diagnostics["registry"])
        registry_facts = diagnostics["registryFile"]
        registry_facts.update(bytes=len(registry_raw), sha256=M.digest(registry_raw), ino=registry_facts["ino"] + 1,
                              mtimeNs=registry_facts["mtimeNs"] + 1, ctimeNs=registry_facts["ctimeNs"] + 1)
        return after, installed, process

    def epoch_fixture(self):
        value = self.input_fixture()
        input_pin = memory_pin(OUTPUT.with_name(OUTPUT.name + "-input.json"), value)
        after, installed, process = self.restarted_state()
        before_pin = memory_pin(OUTPUT / "private/before.json", self.state)
        after_pin = memory_pin(OUTPUT / "private/after.json", after)
        proof = {"before": before_pin, "after": after_pin, "reviewedState": M.SUCCESSOR_STATE, "installedBinary": installed}
        proof_pin = memory_pin(OUTPUT / "private/preservation.json", proof)
        candidate = copy.deepcopy(self.parent["candidate"])
        candidate.update(input=input_pin, productInput=input_pin, binary=installed, currentSourceManifest=M.NEW_SOURCE_MANIFEST,
                         backendReport=FULL)
        candidate["processes"]["server"]["MainPID"] = str(process["pid"])
        candidate["processes"]["server"]["InvocationID"] = "e" * 32
        candidate["serverIdentity"].update(pid=process["pid"], startTicks=process["startTicks"],
            executableInode=process["exeInode"], invocationId="e" * 32)
        candidate["listener"].update(pid=process["pid"], socketInode=after["candidateAfter"]["listener"]["socketInode"])
        for slot in ("source", "recovery"):
            candidate["databases"][slot]["afterStart"] = copy.deepcopy(after["databases"][slot])
        epoch = {key: copy.deepcopy(self.parent[key]) for key in M.EPOCH_KEYS}
        epoch.update(version=3, operationKind="binary_successor", previousEpoch=M.CURRENT_ENV_EPOCH,
                     productInput=input_pin, configurationInput=self.parent["transitionInput"], transitionInput=input_pin,
                     reviewedState=M.SUCCESSOR_STATE, reviewedSummary=M.SUCCESSOR_SUMMARY,
                     currentSource={"archiveSha256": M.NEW_ARCHIVE, "sourceManifest": M.NEW_SOURCE_MANIFEST,
                                    "binary": installed, "fullReport": FULL, "schema": 28},
                     candidate=candidate, candidateProcess=process, lease=after["leaseAfter"], before=before_pin,
                     after=after_pin, preservation=proof_pin, calls={"stop": 1, "replace": 1, "start": 1})
        additional = {identity(pin): document for pin, document in ((input_pin, value), (before_pin, self.state),
                                                                    (after_pin, after), (proof_pin, proof))}
        return epoch, value, additional

    def test_saved_review_accepts_complete_nonempty_terminal_history(self):
        original = M.canonical(self.state)
        facts = M.validate_successor_review(self.summary, self.state, self.prior_closeout, self.prior_source, now=self.now)
        self.assertEqual((facts["ownedTables"], facts["playRows"], facts["userDataRows"], facts["preparedRows"]), (35, 7, 2, 2))
        self.assertEqual(len(facts["revokedSessions"]), 15)
        operations = self.state["controlDocuments"]["operations/current.json"]["payload"]["operations"]
        self.assertEqual({(row["kind"], row["state"], row["phase"]) for row in operations},
                         {("create", "failed", "finished"), ("create", "completed", "finished"), ("restore", "cancelled", "finished")})
        self.assertEqual(M.canonical(self.state), original)

    def test_input_rejects_parent_product_scope_and_budget_drift(self):
        value = self.input_fixture()
        self.assertEqual(len(value), 16)
        self.assertIs(M.validate_binary_successor_input(value), value)
        for name, mutate in (
            ("binary_parent", lambda row: row.update(previousEpoch=M.PREVIOUS_BINARY_EPOCH)),
            ("binding", lambda row: row["previousBinding"].update(sha256="a" * 64)),
            ("review", lambda row: row["reviewedState"].update(sha256="b" * 64)),
            ("old_binary", lambda row: row["newBinary"].update(sha256=M.CURRENT_BINARY)),
            ("old_manifest", lambda row: row.update(newSourceManifest=self.parent["currentSource"]["sourceManifest"])),
            ("scope", lambda row: row.update(output=str(M.R / "candidate-cancellation-transition-99"))),
            ("budget", lambda row: row["budgets"].update(maximumSeconds=901)),
            ("boolean_version", lambda row: row.update(version=True)),
            ("extra", lambda row: row.update(originalProvision=M.PROVISION)),
        ):
            changed = copy.deepcopy(value)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.validate_binary_successor_input(changed)

    def test_actual_full_gate_and_failed_gate_cannot_enter_runtime(self):
        value = self.input_fixture()
        M.validate_successor_full_report(self.full, self.worker, value)
        for name, mutate in (
            ("failed", lambda row: row.update(status="failed")),
            ("target", lambda row: row.update(mode="target")),
            ("cleanup", lambda row: row["worker"]["cleanup"].update(source_unchanged=False)),
            ("package", lambda row: row["worker"]["packages"].pop()),
            ("old_binary", lambda row: row["worker"]["binary"].update(sha256=M.CURRENT_BINARY)),
        ):
            report = copy.deepcopy(self.full)
            mutate(report)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.validate_successor_full_report(report, report["worker"], value)
        job = OP.BinarySuccessor.__new__(OP.BinarySuccessor)
        job.products = Mock(side_effect=M.ContractError("full_product_verification_not_complete"))
        job.open = Mock(side_effect=AssertionError("runtime_must_not_open"))
        job.calls = {"stop": 0, "replace": 0, "start": 0}
        with patch.object(OP.os, "replace") as replace, self.assertRaises(M.ContractError):
            job.run()
        job.open.assert_not_called()
        replace.assert_not_called()
        self.assertEqual(job.calls, {"stop": 0, "replace": 0, "start": 0})

    def test_revoked_sessions_are_exact_typed_unique_and_sorted(self):
        source = copy.deepcopy(self.prior_source)
        source["tables"]["sessions"].reverse()
        rows = M.successor_sessions(source)
        self.assertEqual([row["credentialId"] for row in rows], sorted(row["id"] for row in source["tables"]["sessions"]))
        self.assertTrue(all(set(row) == {"kind", "credentialId", "tokenSha256", "userId", "revokedAt"} for row in rows))
        for name, mutate in (
            ("count", lambda entries: entries.pop()),
            ("unrevoked", lambda entries: entries[0].update(revoked_at=None)),
            ("kind", lambda entries: entries[0].update(kind="application_key")),
            ("identity", lambda entries: entries[0].update(id="not-an-id")),
            ("hash_type", lambda entries: entries[0].update(token_hash=123)),
            ("hash_prefix", lambda entries: entries[0].update(token_hash=entries[0]["token_hash"][2:])),
            ("naive_time", lambda entries: entries[0].update(revoked_at="2026-09-13T14:00:00")),
            ("duplicate_hash", lambda entries: entries[0].update(token_hash=entries[1]["token_hash"])),
            ("duplicate_id", lambda entries: entries[0].update(id=entries[1]["id"])),
        ):
            changed = copy.deepcopy(source)
            mutate(changed["tables"]["sessions"])
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.successor_sessions(changed)

    def test_state_rejects_pending_control_stage_and_startup_work(self):
        def operation(state):
            return state["controlDocuments"]["operations/current.json"]["payload"]["operations"][0]
        for name, mutate in (
            ("trigger", lambda row: row["source"]["tables"]["task_triggers"].append({})),
            ("encoding", lambda row: row["source"]["tables"]["encoding_jobs"].append({})),
            ("scan", lambda row: row["source"]["tables"]["scan_jobs"][0].update(status="Running")),
            ("authorization", lambda row: operation(row).update(authorized=False)),
            ("apply", lambda row: operation(row).update(applyAuthorized=True)),
            ("publication", lambda row: operation(row).update(phase="publication")),
            ("transition", lambda row: row["controlDocuments"]["operations/current.json"]["payload"].update(transition={"phase": "requested"})),
            ("activation", lambda row: row["controlDocuments"].update({"recovery/activation-journal.json": {}})),
            ("pending_file", lambda row: row["trees"][str(M.C / "data/backups")].update({".goby-backup-catalog.pending": {"bytes": 1}})),
            ("deletion", lambda row: row["controlDocuments"]["backups/.goby-backup-catalog.json"]["entries"][0].update(deleting=True)),
            ("generation", lambda row: row["controlDocuments"]["recovery/generation-registry.json"]["generations"][0].update(complete=False)),
            ("both_anchors", lambda row: row.update(previousEpoch=M.CURRENT_ENV_EPOCH)),
        ):
            changed = copy.deepcopy(self.state)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.validate_successor_state(changed, now=self.now)

    def test_review_rejects_same_counts_with_changed_prior_rows_or_sequences(self):
        for name, mutate in (
            ("play", lambda row: row["tables"]["play_sessions"][0].update(position_ticks=row["tables"]["play_sessions"][0]["position_ticks"] + 1)),
            ("userdata", lambda row: row["tables"]["user_item_data"][0].update(play_count=99)),
            ("sequence", lambda row: row["sequences"][next(iter(row["sequences"]))].update(lastValue="999999")),
            ("missing_table", lambda row: row["tables"].pop("user_settings")),
        ):
            prior = copy.deepcopy(self.prior_source)
            mutate(prior)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.validate_successor_review(self.summary, self.state, self.prior_closeout, prior, now=self.now)

    def test_fresh_before_preserves_both_databases_and_all_old_history(self):
        fresh = copy.deepcopy(self.state)
        fresh["capturedAt"] = (self.now + timedelta(seconds=1)).isoformat()
        fresh["previousEpoch"] = fresh.pop("runtimeEpoch")
        for section in ("source", "inactiveStage"):
            fresh[section]["capturedAt"] = fresh["capturedAt"]
        M.compare_successor_preservation(self.state, fresh, now=self.now)
        for name, mutate in (
            ("prepared_expired", lambda row: next(play for play in row["source"]["tables"]["play_sessions"] if play["state"] == "Prepared").update(state="Expired")),
            ("userdata", lambda row: row["source"]["tables"]["user_item_data"][0].update(play_count=3)),
            ("inactive_user", lambda row: row["inactiveStage"]["tables"]["users"][0].update(name="changed")),
            ("inactive_sequence", lambda row: row["inactiveStage"]["sequences"][next(iter(row["inactiveStage"]["sequences"]))].update(lastValue="999999")),
            ("database_facts", lambda row: row["databases"]["source"].update(extra=True)),
            ("environment", lambda row: row["fixedFiles"][str(M.C / "private/runtime.env")].update(sha256="a" * 64)),
            ("log_rewrite", lambda row: row["unitLogs"]["server-unit.log"].update(sha256="b" * 64)),
        ):
            changed = copy.deepcopy(fresh)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.compare_successor_preservation(self.state, changed, now=self.now)

    def test_restart_allows_only_installed_binary_and_owned_log_rotation(self):
        after, installed, _process = self.restarted_state()
        proof = M.compare_successor_preservation(self.state, after, installed_binary=installed, now=self.now)
        self.assertTrue(proof["allPriorPlayAndUserDataExact"])
        for field in ("dev", "uid", "gid", "mode"):
            changed = copy.deepcopy(after)
            changed["fixedFiles"][installed["path"]][field] += 1
            with self.subTest(field=field), self.assertRaises(M.ContractError):
                M.compare_successor_preservation(self.state, changed, installed_binary=installed, now=self.now)
        for name, mutate in (
            ("wrong_binary", lambda row: row["fixedFiles"][installed["path"]].update(sha256=M.CURRENT_BINARY)),
            ("old_prepared", lambda row: next(play for play in row["source"]["tables"]["play_sessions"] if play["state"] == "Prepared").update(stopped_at=row["capturedAt"])),
            ("control_revision", lambda row: row["controlDocuments"]["operations/current.json"].update(revision=999)),
            ("missing_key_fact", lambda row: row["fixedFiles"].pop(str(M.C / "data/master.key"))),
            ("log_prefix", lambda row: row["unitLogs"]["server-unit.log"].update(
                bytes=self.state["unitLogs"]["server-unit.log"]["bytes"],
                sha256=self.state["unitLogs"]["server-unit.log"]["sha256"], prefixSha256="c" * 64)),
        ):
            changed = copy.deepcopy(after)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.compare_successor_preservation(self.state, changed, installed_binary=installed, now=self.now)

    def test_retention_guard_rechecks_time_without_expiring_prepared_rows(self):
        prepared = [row for row in self.state["source"]["tables"]["play_sessions"] if row["state"] == "Prepared"]
        self.assertTrue(all(instant(row["expires_at"]) < self.now for row in prepared))
        M.validate_successor_state(self.state, now=self.now)
        oldest = min(instant(row["created_at"]) for row in self.state["source"]["tables"]["activity_entries"])
        boundary = oldest + timedelta(days=1, seconds=-M.LIMITS["maximumSeconds"])
        with self.assertRaisesRegex(M.ContractError, "activity_retention_deadline"):
            M.validate_successor_state(self.state, now=boundary)
        changed = copy.deepcopy(self.state)
        changed["diagnostics"]["registry"]["files"][0]["created"] = (self.now - timedelta(days=7)).isoformat()
        with self.assertRaisesRegex(M.ContractError, "diagnostic_retention_deadline"):
            M.validate_successor_state(changed, now=self.now)

    def test_epoch_lineage_and_binding_keep_configuration_and_descriptor_proofs(self):
        epoch, value, additional = self.epoch_fixture()
        read, _documents, _seen = self.saved_reader(additional)
        lineage = M.resolve_epoch_lineage(epoch, read)
        self.assertEqual(set(lineage), {"productEpoch", "productInput", "configurationInput"})
        self.assertEqual(lineage, {"productEpoch": epoch, "productInput": value, "configurationInput": self.configuration})
        self.assertEqual(M.resolve_epoch_lineage(self.original, read)["configurationInput"], None)
        self.assertEqual(M.resolve_epoch_lineage(self.parent, read)["productEpoch"], self.original)
        self.assertEqual(epoch["candidate"]["runtime"], self.parent["candidate"]["runtime"])
        for name, mutate in (
            ("skip_configuration", lambda row: row.update(previousEpoch=M.PREVIOUS_BINARY_EPOCH)),
            ("wrong_configuration", lambda row: row.update(configurationInput=row["productInput"])),
            ("environment_changed", lambda row: row["candidate"]["runtime"].update(sha256="1" * 64)),
            ("inline_nanoseconds", lambda row: row.update(preservation={"oldBinaryFacts": {"mtimeNs": 1789310000000000001}})),
            ("inline_extra", lambda row: row["preservation"].update(ctimeNs=1789310000000000001)),
            ("reused_binary", lambda row: row["currentSource"]["binary"].update(sha256=M.CURRENT_BINARY)),
        ):
            changed = copy.deepcopy(epoch)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.resolve_epoch_lineage(changed, read)
        epoch_pin = memory_pin(OUTPUT / "private/runtime-epoch.json", epoch)
        binding = {key: copy.deepcopy(self.parent_binding[key]) for key in M.BINDING_KEYS}
        binding.update(version=3, runtimeEpoch=epoch_pin, previousBinding=M.CURRENT_ENV_BINDING,
                       reviewedSummary=M.SUCCESSOR_SUMMARY, reviewedState=M.SUCCESSOR_STATE,
                       priorCloseout=M.SUCCESSOR_PRIOR_CLOSEOUT, priorSource=M.SUCCESSOR_PRIOR_SOURCE,
                       currentSessions=M.successor_sessions(self.state["source"]))
        M.validate_seed_runtime_binding(binding, epoch_pin, epoch, self.seed)
        for name, mutate in (
            ("order", lambda row: row["currentSessions"].reverse()),
            ("extra_field", lambda row: row["currentSessions"][0].update(extra=True)),
            ("seed_mapping", lambda row: row.update(catalog={})),
        ):
            changed = copy.deepcopy(binding)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises(M.ContractError):
                M.validate_seed_runtime_binding(changed, epoch_pin, epoch, self.seed)

    def test_adopt_context_updates_every_reader_without_mutating_parent_epoch(self):
        previous = copy.deepcopy(self.parent)
        original = M.canonical(previous)
        job = OP.BinarySuccessor.__new__(OP.BinarySuccessor)
        job.previous_epoch = previous
        job.input_pin = memory_pin(OUTPUT / "input.json", self.input_fixture())
        job.epoch = previous
        job.io = SimpleNamespace(epoch=previous, candidate=previous["candidate"],
                                 reader=SimpleNamespace(epoch=previous, candidate=previous["candidate"], manifest=previous["candidate"]))
        after, _installed, process = self.restarted_state()
        candidate = copy.deepcopy(previous["candidate"])
        candidate["serverIdentity"]["pid"] = process["pid"]
        job.adopt_runtime_context(candidate, process)
        for current in (job.candidate, job.io.candidate, job.io.reader.candidate, job.io.reader.manifest):
            self.assertIs(current, candidate)
            self.assertEqual(current["productInput"], job.input_pin)
        for context in (job.epoch, job.io.epoch, job.io.reader.epoch):
            self.assertEqual(set(context), {"candidateProcess", "postgresProcess"})
            self.assertEqual(context["candidateProcess"], process)
            self.assertEqual(context["postgresProcess"], previous["postgresProcess"])
            self.assertIsNot(context, previous)
        job.epoch["candidateProcess"]["pid"] += 1
        self.assertEqual(job.io.reader.epoch["candidateProcess"], process)
        self.assertEqual(M.canonical(previous), original)
        self.assertEqual(after["candidateAfter"]["pid"], process["pid"])

    def test_check_captured_replays_saved_inputs_without_any_live_operation(self):
        read, documents, seen = self.saved_reader()
        job = OP.BinarySuccessor.__new__(OP.BinarySuccessor)
        job.r, job.value, job.s = M, self.input_fixture(), SimpleNamespace(descriptor=read)
        for name in ("open", "capture", "public_read", "sql_json", "hosting", "run"):
            setattr(job, name, Mock(side_effect=AssertionError("live_operation_forbidden")))
        with patch.object(M, "EpochReader", side_effect=AssertionError("reader_forbidden")), \
                patch.object(M, "EpochIO", side_effect=AssertionError("io_forbidden")), \
                patch.object(OP.subprocess, "run", side_effect=AssertionError("subprocess_forbidden")), \
                patch.object(OP.os, "replace", side_effect=AssertionError("replace_forbidden")):
            result = job.check_captured()
            self.assertEqual(result["status"], "captured_contract_checked_not_product_admitted")
            self.assertFalse(result["newProductGateEvaluated"])
            self.assertFalse(result["freshRuntimeChecked"])
            self.assertEqual((result["newSqlQueries"], result["httpRequests"], result["businessWrites"]), (0, 0, 0))
            documents[identity(M.INACTIVE_STAGE)]["tables"]["users"][0]["name"] = "different-stage"
            with self.assertRaisesRegex(M.ContractError, "inactive_stage_changed"):
                job.check_captured()
        self.assertIn(identity(M.SUCCESSOR_STATE), seen)
        self.assertIn(identity(M.INACTIVE_STAGE), seen)
        for name in ("open", "capture", "public_read", "sql_json", "hosting", "run"):
            getattr(job, name).assert_not_called()


if __name__ == "__main__":
    unittest.main()
