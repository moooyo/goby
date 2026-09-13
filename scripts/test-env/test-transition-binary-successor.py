#!/usr/bin/env python3
"""Pure successor guards and fixed saved-artifact replay; never candidate IO."""
import copy
from datetime import datetime, timedelta
import hashlib
import importlib.util
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
