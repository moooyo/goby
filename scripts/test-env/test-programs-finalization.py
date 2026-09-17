#!/usr/bin/env python3
"""Pure finalization guards with finite memory evidence; never candidate IO."""
import copy
from contextlib import ExitStack
import errno
import hashlib
import importlib.util
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch


spec = importlib.util.spec_from_file_location(
    "programs_finalization_guards", Path(__file__).with_name("audited-candidate-runtime.py"))
M = importlib.util.module_from_spec(spec)
spec.loader.exec_module(M)
finalizer_spec = importlib.util.spec_from_file_location(
    "programs_finalizer_components", Path(__file__).with_name("finalize-programs-transition.py"))
F = importlib.util.module_from_spec(finalizer_spec)
finalizer_spec.loader.exec_module(F)


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def pin(path, raw=b"synthetic evidence\n", with_size=False):
    result = {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}
    if with_size:
        result["bytes"] = len(raw)
    return result


class ProgramsFinalizationGuards(unittest.TestCase):
    def setUp(self):
        self.stack = ExitStack()
        self.addCleanup(self.stack.close)
        blocked = lambda: Mock(side_effect=AssertionError("Real system IO is forbidden."))
        self.stack.enter_context(patch.object(M, "os", SimpleNamespace()))
        self.native = SimpleNamespace(Popen=blocked(), run=blocked(), call=blocked(),
                                      check_call=blocked(), check_output=blocked())
        self.stack.enter_context(patch.object(M, "subprocess", self.native))
        self.real_bytes = self.stack.enter_context(patch.object(M, "read_programs_bytes", blocked()))
        self.stack.enter_context(patch.object(M, "read_bootstrap", blocked()))
        self.final_os = SimpleNamespace(getpid=Mock(return_value=5001), fstat=blocked(), killpg=blocked(),
                                       set_blocking=blocked(), read=blocked(), write=blocked())
        self.final_native = SimpleNamespace(Popen=blocked(), PIPE=F.subprocess.PIPE, DEVNULL=F.subprocess.DEVNULL)
        self.final_selectors = SimpleNamespace(DefaultSelector=blocked(), EVENT_READ=1, EVENT_WRITE=2)
        self.stack.enter_context(patch.object(F, "os", self.final_os))
        self.stack.enter_context(patch.object(F, "subprocess", self.final_native))
        self.stack.enter_context(patch.object(F, "selectors", self.final_selectors))
        self.stack.enter_context(patch.object(F, "time", SimpleNamespace(monotonic=Mock(return_value=1000.0), sleep=blocked())))
        self.stack.enter_context(patch.object(F, "signal", SimpleNamespace(SIGTERM=15, SIGKILL=9)))
        self.final_identity = self.stack.enter_context(patch.object(F, "process_identity", blocked()))
        self.final_groups = self.stack.enter_context(patch.object(F, "group_members", blocked()))
        for name in ("open", "read_bytes", "read_text", "write_bytes", "write_text", "stat", "lstat",
                     "iterdir", "rglob", "mkdir", "unlink", "rename", "replace", "exists", "is_file", "is_dir"):
            self.stack.enter_context(patch.object(Path, name, blocked()))

    def rollover_fixture(self):
        # Only source/log byte pins are synthetic. Scope, names, inodes, the old
        # active filename, event times, and the exact 9-to-11 inventory stay fixed.
        self.pins = copy.deepcopy(M.PROGRAMS_FINALIZATION_PINS)
        self.logs = copy.deepcopy(M.PROGRAMS_ROLLOVER_LOGS)
        self.blobs = {}
        source = {}
        for key, member in (("oldMainSource", "cmd/goby/main.go"),
                            ("oldStoreSource", "internal/diagnostics/store_linux.go")):
            raw = ("// Synthetic source fixture for " + member + "\n").encode()
            self.pins[key] = pin(self.pins[key]["path"], raw)
            self.blobs[self.pins[key]["path"]] = raw
            source[member] = {"bytes": len(raw), "sha256": self.pins[key]["sha256"]}
        raw = encoded(source)
        self.pins["oldSourceManifest"] = pin(self.pins["oldSourceManifest"]["path"], raw)
        self.blobs[self.pins["oldSourceManifest"]["path"]] = raw
        shutdown = {"time": "2026-09-17T07:15:40.810585954Z", "level": "INFO",
                    "msg": "server shutdown completed", "event": "server.shutdown.completed", "duration_ms": 2}
        startup = [{"time": "2026-09-17T07:15:41.014000Z", "level": "INFO", "event": "server.starting"},
                   {"time": "2026-09-17T07:15:41.108000Z", "level": "INFO", "event": "server.listening"}]
        for number, route in enumerate(("GET /readyz", "GET /healthz", "GET /emby/System/Info/Public")):
            startup.append({"time": "2026-09-17T07:15:42.%03d000Z" % (74 + 29 * number),
                            "level": "INFO", "event": "request.completed", "route": route,
                            "method": "GET", "status": 200})
        self.review = {"kind": "audited-programs-shutdown-rollover-review", "version": 1,
                       "status": "exact_saved_rollover_reviewed"}
        for key in ("transitionInput", "failure", "before", "after", "oldSourceManifest", "oldMainSource", "oldStoreSource"):
            self.review[key] = copy.deepcopy(self.pins[key])
        for key, rows in (("shutdownLog", [shutdown]), ("startupLog", startup)):
            raw = b"".join(encoded(row) for row in rows)
            content = pin(M.R / "synthetic-finalization-review" / (key + ".jsonl"), raw, True)
            self.review[key] = content
            self.blobs[content["path"]] = raw
            self.logs[key].update(bytes=len(raw), sha256=content["sha256"])
        self.stack.enter_context(patch.object(M, "PROGRAMS_FINALIZATION_PINS", self.pins))
        self.stack.enter_context(patch.object(M, "PROGRAMS_ROLLOVER_LOGS", self.logs))
        self.read_bytes = Mock(side_effect=lambda selected: self.blobs[selected["path"]])
        self.read_descriptor = Mock(side_effect=lambda selected: json.loads(self.blobs[selected["path"]]))

        token = "3618ed602bb9a56c12a2c7b0c34cc443"
        active = "goby-" + token + "-b25e8ca719092ba041930eb7cfa5004a.jsonl"
        diagnostic = {"registry": {"version": 1, "token": token, "files": []}, "files": {},
                      "lock": {"dev": 2049, "ino": 100}, "directory": {"dev": 2049, "ino": 101},
                      "registryFile": {"dev": 2049, "uid": 995, "gid": 986, "mode": 0o600}}
        for number in range(9):
            name = active if number == 8 else "goby-" + token + "-%032x.jsonl" % number
            raw = ("retained old diagnostic %d\n" % number).encode()
            facts = {"dev": 2049, "ino": 200 + number, "uid": 995, "gid": 986, "mode": 0o600,
                     "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}
            diagnostic["files"][name] = facts
            diagnostic["registry"]["files"].append({"name": name, "created": "2026-09-16T10:00:00Z",
                "identity": {"device": 2049, "inode": facts["ino"]}, "closed": number != 8,
                "size": len(raw) if number != 8 else 0})
        self.before = {"databaseNow": "2026-09-17T07:15:39Z", "diagnostics": diagnostic}
        self.after = copy.deepcopy(self.before)
        self.after["databaseNow"] = "2026-09-17T07:15:43Z"
        after = self.after["diagnostics"]
        for row in after["registry"]["files"]:
            row.update(closed=True, size=after["files"][row["name"]]["bytes"])
        for key, expected in self.logs.items():
            closed = key == "shutdownLog"
            after["registry"]["files"].append({"name": expected["name"],
                "created": "2026-09-17T07:15:40.810655594Z" if closed else "2026-09-17T07:15:41.009585878Z",
                "identity": {"device": 2049, "inode": expected["ino"]}, "closed": closed,
                "size": expected["bytes"] if closed else 0})
            after["files"][expected["name"]] = {"dev": 2049, "uid": 995, "gid": 986, "mode": 0o600,
                **{field: expected[field] for field in ("ino", "bytes", "sha256")}}

    def authorize(self, before=None, after=None, review=None):
        selected = self.review if review is None else review
        return M.validate_programs_rollover_review(pin(M.R / "synthetic-rollover-review.json", encoded(selected)),
            selected, self.before if before is None else before, self.after if after is None else after,
            self.read_descriptor, self.read_bytes)

    def test_exact_two_logs_require_validated_authority_and_legacy_stays_strict(self):
        self.rollover_fixture()
        before, after = self.before["diagnostics"], self.after["diagnostics"]
        with self.assertRaisesRegex(M.ContractError, "^diagnostic_file_membership_changed$"):
            M.compare_diagnostics(before, after)
        with self.assertRaisesRegex(M.ContractError, "^programs_failed_diagnostic_inventory$"):
            M.compare_programs_failed_diagnostics(before, after)
        original = M.canonical((self.before, self.after))
        authority = self.authorize()
        expected = {"oldLogPrefixesPreserved": 9, "newActiveLogs": 1, "shutdownRolloverLogs": 1, "prunedLogs": 0}
        self.assertEqual(M.compare_diagnostics(before, after, diagnostic_authority=authority), expected)
        self.assertEqual(M.compare_programs_failed_diagnostics(before, after, diagnostic_authority=authority), expected)
        self.assertEqual(M.canonical((self.before, self.after)), original)
        self.assertEqual({call.args[0]["path"] for call in self.read_bytes.call_args_list}, set(self.blobs))
        self.real_bytes.assert_not_called()
        with self.assertRaisesRegex(M.ContractError, "^programs_rollover_authority_required$"):
            M.compare_diagnostics(before, after, diagnostic_authority={"reviewed": True})

    def test_rollover_rejects_third_log_removed_old_log_and_changed_old_hash(self):
        self.rollover_fixture()
        authority = self.authorize()
        old_name = self.before["diagnostics"]["registry"]["files"][0]["name"]
        for defect, code in (("third_log", "programs_rollover_exact_inventory"),
                             ("removed_old", "programs_rollover_exact_inventory"),
                             ("old_hash", "programs_rollover_old_log_changed")):
            with self.subTest(defect=defect):
                changed = copy.deepcopy(self.after)
                diagnostic = changed["diagnostics"]
                if defect == "third_log":
                    row = copy.deepcopy(diagnostic["registry"]["files"][-1])
                    row["name"] = "goby-3618ed602bb9a56c12a2c7b0c34cc443-" + "f" * 32 + ".jsonl"
                    diagnostic["registry"]["files"].append(row)
                    diagnostic["files"][row["name"]] = copy.deepcopy(diagnostic["files"][self.logs["startupLog"]["name"]])
                elif defect == "removed_old":
                    diagnostic["registry"]["files"] = [row for row in diagnostic["registry"]["files"] if row["name"] != old_name]
                    del diagnostic["files"][old_name]
                else:
                    diagnostic["files"][old_name]["sha256"] = "f" * 64
                with self.assertRaisesRegex(M.ContractError, "^" + code + "$"):
                    self.authorize(after=changed)
                with self.assertRaisesRegex(M.ContractError, "^programs_rollover_authority_required$"):
                    M.compare_diagnostics(self.before["diagnostics"], diagnostic, diagnostic_authority=authority)

    def test_rollover_checks_short_log_bytes_event_and_original_time_window(self):
        self.rollover_fixture()
        changed = copy.deepcopy(self.review)
        changed["shutdownLog"]["sha256"] = "f" * 64
        with self.assertRaisesRegex(M.ContractError, "^programs_rollover_log_content_pin$"):
            self.authorize(review=changed)
        path = self.review["shutdownLog"]["path"]
        original_raw = self.blobs[path]
        self.blobs[path] = original_raw.replace(b"shutdown completed", b"shutdown completeX")
        with self.assertRaisesRegex(M.ContractError, "^programs_rollover_log_content$"):
            self.authorize()
        # Rebind synthetic content pins to reach the independent event guard.
        changed_event = json.loads(original_raw)
        changed_event["event"] = "server.starting"
        event_raw = encoded(changed_event)
        self.blobs[path] = event_raw
        self.review["shutdownLog"] = pin(path, event_raw, True)
        self.logs["shutdownLog"].update(bytes=len(event_raw), sha256=self.review["shutdownLog"]["sha256"])
        with self.assertRaisesRegex(M.ContractError, "^programs_rollover_shutdown_event$"):
            self.authorize()
        self.blobs[path] = original_raw
        self.review["shutdownLog"] = pin(path, original_raw, True)
        self.logs["shutdownLog"].update(bytes=len(original_raw), sha256=self.review["shutdownLog"]["sha256"])
        for boundary in ("before", "after"):
            with self.subTest(boundary=boundary):
                before, after = copy.deepcopy(self.before), copy.deepcopy(self.after)
                (before if boundary == "before" else after)["databaseNow"] = "2026-09-17T07:15:41Z"
                with self.assertRaisesRegex(M.ContractError, "^programs_rollover_event_window$"):
                    self.authorize(before=before, after=after)

    def fresh_fixture(self):
        self.rollover_fixture()
        after = copy.deepcopy(self.after)
        for section in ("source", "inactiveStage", "databases", "trees", "controlDocuments", "loadedUnits",
                        "protected", "postgresBefore", "postgresAfter", "fixedFiles"):
            after[section] = {}
        process = {"pid": 1907978, "startTicks": "34901535", "exeInode": 1580898}
        lease = {"backendPid": 1907986, "candidateConnection": {"pid": process["pid"]}}
        hosting = {"pid": 366598, "startTicks": "506485", "networkNamespace": "net:[4026532544]"}
        for prefix, value in (("candidate", process), ("lease", lease), ("hosting", hosting)):
            after[prefix + "Before"] = copy.deepcopy(value)
            after[prefix + "After"] = copy.deepcopy(value)
        sample = next(iter(after["diagnostics"]["files"].values()))
        after["unitLogs"] = {name: copy.deepcopy(sample) for name in ("server-unit.log", "postgres-unit.log")}
        # Existing full-state and logical-table contracts have their own suite.
        # Keep the actual preservation comparisons and fixed identity guard here.
        self.stack.enter_context(patch.object(M, "validate_programs_state", return_value={}))
        self.stack.enter_context(patch.object(M, "compare_programs_logical", return_value=None))
        return after

    def test_fresh_state_keeps_actual_pid_lease_hosting_and_prior_log_prefixes(self):
        after = self.fresh_fixture()
        fresh = copy.deepcopy(after)
        fresh["databaseNow"] = "2026-09-17T07:15:44Z"
        self.assertTrue(M.compare_programs_finalization_fresh(after, fresh)["hostingContinuous"])
        active = next(row["name"] for row in after["diagnostics"]["registry"]["files"] if not row["closed"])
        appended = copy.deepcopy(fresh)
        facts = appended["diagnostics"]["files"][active]
        facts.update(bytes=facts["bytes"] + 1, prefixSha256=facts["sha256"], sha256="e" * 64)
        self.assertTrue(M.compare_programs_finalization_fresh(after, appended)["hostingContinuous"])
        for defect, code in (("pid", "programs_before_identity_changed"),
                             ("lease", "programs_before_identity_changed"),
                             ("hosting", "programs_preservation_changed_hostingBefore"),
                             ("old_log", "programs_diagnostic_prefix_changed")):
            with self.subTest(defect=defect):
                changed = copy.deepcopy(fresh)
                if defect == "pid":
                    for key in ("candidateBefore", "candidateAfter"):
                        changed[key]["pid"] += 1
                elif defect == "lease":
                    for key in ("leaseBefore", "leaseAfter"):
                        changed[key]["backendPid"] += 1
                elif defect == "hosting":
                    for key in ("hostingBefore", "hostingAfter"):
                        changed[key]["networkNamespace"] = "net:[unreviewed]"
                else:
                    facts = next(iter(changed["diagnostics"]["files"].values()))
                    facts.update(bytes=facts["bytes"] + 1, prefixSha256="f" * 64, sha256="e" * 64)
                with self.assertRaisesRegex(M.ContractError, "^" + code + "$"):
                    M.compare_programs_finalization_fresh(after, changed)
        changed = copy.deepcopy(fresh)
        closed = next(row["name"] for row in after["diagnostics"]["registry"]["files"] if row["closed"])
        facts = changed["diagnostics"]["files"][closed]
        facts.update(bytes=facts["bytes"] + 1, prefixSha256=facts["sha256"], sha256="e" * 64)
        with self.assertRaisesRegex(M.ContractError, "^programs_finalization_closed_log_changed$"):
            M.compare_programs_finalization_fresh(after, changed)
        wrong = copy.deepcopy(after)
        wrong["candidateBefore"]["pid"] = wrong["candidateAfter"]["pid"] = 1907979
        with self.assertRaisesRegex(M.ContractError, "^programs_finalization_fresh_identity$"):
            M.compare_programs_finalization_fresh(wrong, copy.deepcopy(wrong))

    def input_fixture(self):
        value = {"kind": "audited-programs-transition-finalization-input", "version": 1,
                 "output": str(M.PROGRAMS_FINALIZATION_ROOT), "budgets": copy.deepcopy(M.PROGRAMS_FINALIZATION_LIMITS)}
        for key in ("transitionInput", "transitionHelper", "transitionRuntime", "failure", "before", "after", "closureReview", "rolloverReview"):
            value[key] = copy.deepcopy(M.PROGRAMS_FINALIZATION_PINS[key])
        for key, name in (("sourceBefore", "source-before.json"), ("sourceAfter", "source-after.json")):
            value[key] = pin(M.PROGRAMS_FINALIZATION_ORIGINAL / "private" / name)
        return value

    def test_input_cannot_change_original_authorities_or_zero_action_budget(self):
        value = self.input_fixture()
        self.assertIs(M.validate_programs_finalization_input(value), value)
        for key in ("transitionInput", "transitionHelper", "transitionRuntime", "failure", "before", "after", "closureReview", "rolloverReview"):
            with self.subTest(pin=key):
                changed = copy.deepcopy(value)
                changed[key]["sha256"] = "f" * 64
                with self.assertRaisesRegex(M.ContractError, "^programs_finalization_original_authority$"):
                    M.validate_programs_finalization_input(changed)
        for key in M.PROGRAMS_FINALIZATION_LIMITS:
            with self.subTest(budget=key):
                changed = copy.deepcopy(value)
                changed["budgets"][key] += 1
                with self.assertRaisesRegex(M.ContractError, "^programs_finalization_input$"):
                    M.validate_programs_finalization_input(changed)
        changed = copy.deepcopy(value)
        changed["budgets"]["stopCalls"] = False
        with self.assertRaisesRegex(M.ContractError, "^programs_finalization_input$"):
            M.validate_programs_finalization_input(changed)

    def test_fresh_sql_responsibilities_require_exact_labels_and_complete_acknowledgments(self):
        labels = ["cluster-identity", "epoch-read-only", "cluster-identity", "transition-read-only",
                  "cluster-identity", "transition-read-only", "cluster-identity",
                  "database-identity-goby_candidate_ef77f9ffcf0b", "cluster-identity",
                  "database-objects-goby_candidate_ef77f9ffcf0b", "cluster-identity",
                  "database-identity-goby_recovery_ef77f9ffcf0b", "cluster-identity",
                  "database-objects-goby_recovery_ef77f9ffcf0b", "cluster-identity", "epoch-read-only"]
        rows = [{"label": label, "processGroupClosed": True, "outcome": "acknowledged", "timedOut": False,
                 "descendantsRemained": False, "exitCode": 0, "sqlOutcome": "acknowledged",
                 "stdinComplete": True, "outputLimitExceeded": False} for label in labels]
        self.assertIsNone(M._programs_finalization_commands(rows, 16, fresh=True))
        for field, value, code in (("label", "unreviewed-read", "programs_finalization_readonly_commands"),
                                  ("stdinComplete", False, "programs_finalization_readonly_commands"),
                                  ("outputLimitExceeded", True, "programs_finalization_readonly_commands"),
                                  ("sqlOutcome", None, "programs_finalization_readonly_commands"),
                                  ("sqlOutcome", "unknown", "programs_finalization_command_outcome"),
                                  ("outcome", "unknown", "programs_finalization_command_outcome")):
            with self.subTest(field=field, value=value):
                changed = copy.deepcopy(rows)
                changed[1][field] = value
                with self.assertRaisesRegex(M.ContractError, "^" + code + "$"):
                    M._programs_finalization_commands(changed, 16, fresh=True)
        with self.assertRaisesRegex(M.ContractError, "^programs_finalization_command_outcome$"):
            M._programs_finalization_commands(rows[:-1], 16, fresh=True)
        self.native.Popen.assert_not_called()

    def epoch_fixture(self):
        selected = pin(M.R / "synthetic-epoch-input.json")
        epoch = dict.fromkeys(M.PROGRAMS_EPOCH_KEYS)
        epoch.update(kind="audited-candidate-runtime-epoch", version=4, operationKind="programs_successor",
            status="running_awaiting_live_acceptance", candidateAdmissionComplete=False,
            previousEpoch=copy.deepcopy(M.PROGRAMS_PREVIOUS_EPOCH), previousCurrentRuntime=copy.deepcopy(M.PROGRAMS_CURRENT_RUNTIME),
            originalProvision=copy.deepcopy(M.PROVISION), seedProvenance=copy.deepcopy(M.SEED),
            calls={"stop": 1, "replace": 1, "start": 1})
        for key in ("transitionInput", "transitionHelper", "runtimeHelper", "before", "after", "sourceBefore", "sourceAfter",
                    "productInput", "configurationInput", "preservation", "reviewedState", "reviewedSummary"):
            epoch[key] = copy.deepcopy(selected)
        source = {key: copy.deepcopy(selected) for key in M.PROGRAMS_SOURCE_KEYS - {"archiveSha256", "schema"}}
        source.update(archiveSha256=M.PROGRAMS_SOURCE_ARCHIVE["sha256"], schema=28,
                      binary={"path": str(M.C / "install/goby"), "sha256": "2" * 64})
        epoch["currentSource"] = source
        epoch["candidate"] = {"bootstrapExecuted": True, "sourceState": {"users": 8, "schema": 28, "migrations": 28},
            "originalProvision": copy.deepcopy(M.PROVISION), "seedProvenance": copy.deepcopy(M.SEED),
            "input": copy.deepcopy(selected), "productInput": copy.deepcopy(selected), "binary": copy.deepcopy(source["binary"]),
            "currentSourceManifest": copy.deepcopy(source["sourceManifest"]), "backendReport": copy.deepcopy(source["fullReport"])}
        return epoch

    def test_optional_epoch_descriptor_and_legacy_no_extra_read_boundary(self):
        epoch = self.epoch_fixture()
        self.assertIs(M.validate_epoch(epoch), epoch)
        selected = pin(M.PROGRAMS_FINALIZATION_ROOT / "private/antecedent.json")
        finalized = {**copy.deepcopy(epoch), "finalization": selected}
        self.assertIs(M.validate_epoch(finalized), finalized)
        for changed, code in ((None, "descriptor_invalid"), ({**selected, "bytes": 12}, "descriptor_invalid"),
                              (pin(M.PROGRAMS_FINALIZATION_ROOT / "private/runtime-epoch.json"), "programs_finalization_epoch_scope")):
            with self.subTest(descriptor=changed), self.assertRaisesRegex(M.ContractError, "^" + code + "$"):
                M.validate_epoch({**copy.deepcopy(epoch), "finalization": changed})

        class LegacyReadBoundary(Exception):
            pass

        reader = Mock(side_effect=LegacyReadBoundary)
        with patch.object(M, "_finalization_record", side_effect=AssertionError("Unexpected finalization read.")) as record, \
             patch.object(M, "validate_programs_finalization_antecedent", side_effect=AssertionError("Unexpected antecedent.")) as antecedent:
            with self.assertRaises(LegacyReadBoundary):
                M.resolve_programs_successor_lineage(epoch, reader)
            reader.assert_called_once_with(M.PROGRAMS_PREVIOUS_EPOCH)
            record.assert_not_called()
            antecedent.assert_not_called()
        self.real_bytes.assert_not_called()
        reader.reset_mock()
        with patch.object(M, "read_programs_bytes", side_effect=FileNotFoundError("Missing saved antecedent.")) as missing:
            with self.assertRaises(FileNotFoundError):
                M.resolve_programs_successor_lineage(finalized, reader)
            missing.assert_called_once_with(selected)
        reader.assert_not_called()

    def test_antecedent_rejects_self_or_future_epoch_input_before_reading_input(self):
        record = dict.fromkeys(M.PROGRAMS_FINALIZATION_KEYS)
        record.update(kind="audited-programs-transition-finalization", version=1,
            status="fresh_checked_awaiting_live_acceptance", publicationRoot=str(M.PROGRAMS_FINALIZATION_ROOT),
            producer=pin(M.R / "synthetic-finalization-source/producer.py"),
            validator=pin(M.R / "synthetic-finalization-source/validator.py"))
        selected = pin(M.PROGRAMS_FINALIZATION_ROOT / "private/antecedent.json")
        descriptor_reader = Mock(side_effect=AssertionError("Cyclic input must not be read."))
        byte_reader = Mock(side_effect=AssertionError("Cyclic input bytes must not be read."))
        for name in ("antecedent.json", "runtime-epoch.json", "source-fresh.json"):
            with self.subTest(input=name):
                changed = copy.deepcopy(record)
                changed["input"] = pin(M.PROGRAMS_FINALIZATION_ROOT / "private" / name)
                with self.assertRaisesRegex(M.ContractError, "^programs_finalization_new_source_scope$"):
                    M.validate_programs_finalization_antecedent(changed, selected, None, descriptor_reader, byte_reader)
        descriptor_reader.assert_not_called()
        byte_reader.assert_not_called()

    def test_finalizer_capture_queue_has_sixteen_selected_read_only_wires_and_argv(self):
        candidate = {"runId": "20260913T073217Z-ef77f9ffcf0b", "database": "goby_candidate_ef77f9ffcf0b",
                     "recoveryDatabase": "goby_recovery_ef77f9ffcf0b", "ports": {"postgres": 55432}}
        catalog = {"fixture": "prevalidated catalog"}
        snapshot = Mock(return_value="SELECT 'synthetic_snapshot' AS snapshot")
        queue = F.build_capture_sql_queue(candidate, catalog, snapshot)
        labels = ["cluster-identity", "epoch-read-only", "cluster-identity", "transition-read-only",
                  "cluster-identity", "transition-read-only", "cluster-identity",
                  "database-identity-goby_candidate_ef77f9ffcf0b", "cluster-identity",
                  "database-objects-goby_candidate_ef77f9ffcf0b", "cluster-identity",
                  "database-identity-goby_recovery_ef77f9ffcf0b", "cluster-identity",
                  "database-objects-goby_recovery_ef77f9ffcf0b", "cluster-identity", "epoch-read-only"]
        source, recovery = candidate["database"], candidate["recoveryDatabase"]
        databases = ["postgres", source, "postgres", source, "postgres", recovery, "postgres", "postgres",
                     "postgres", source, "postgres", "postgres", "postgres", recovery, "postgres", source]
        self.assertEqual(len(queue), 16)
        self.assertEqual([row["label"] for row in queue], labels)
        snapshot.assert_called_once_with(catalog)
        commands = F.Commands.__new__(F.Commands)
        commands.sql_queue, commands.sql_count = queue, 0
        commands.execute = Mock(return_value=(b"{}\n", b""))
        for number, (row, database) in enumerate(zip(queue, databases), 1):
            with self.subTest(command=number):
                self.assertEqual(row["argv"], ["/usr/sbin/runuser", "-u", "postgres", "--", "/usr/lib/postgresql/17/bin/psql",
                    "-X", "--no-password", "-h", str(F.C / "postgres/socket"), "-p", "55432", "-U", "postgres",
                    "-d", database, "-v", "ON_ERROR_STOP=1", "-Atq"])
                self.assertTrue(row["payload"].startswith((b"SELECT ", b"BEGIN READ ONLY; ")))
                self.assertEqual(commands.sql_command(row["label"], row["argv"], row["payload"]), "{}")
                wire = row["payload"] if row["payload"].startswith(b"BEGIN READ ONLY; ") else b"BEGIN READ ONLY; " + row["payload"] + b" COMMIT;"
                self.assertTrue(wire.startswith(b"BEGIN READ ONLY; ") and wire.endswith(b" COMMIT;"))
                commands.execute.assert_called_with(row["label"], row["argv"], wire, sql=True)
        self.assertEqual(commands.sql_count, 16)
        self.assertEqual(commands.execute.call_count, 16)
        with self.assertRaisesRegex(F.FinalizationError, "^finalization_sql_budget$"):
            commands.sql_command(queue[0]["label"], queue[0]["argv"], queue[0]["payload"])
        self.assertEqual(commands.execute.call_count, 16)
        self.final_native.Popen.assert_not_called()
        self.final_identity.assert_not_called()

    def test_finalizer_unknown_identity_only_closes_the_tracked_popen_leader(self):
        for name, initial_exit, remaining in (("live_empty_group", None, []), ("exited_empty_group", 0, []),
                                             ("unknown_members_remain", None, [{"pid": 6002}])):
            with self.subTest(case=name):
                process = SimpleNamespace(pid=6001, returncode=initial_exit)
                process.kill = Mock(side_effect=lambda: setattr(process, "returncode", -9))
                process.wait = Mock(side_effect=lambda timeout: process.returncode)
                commands = F.Commands.__new__(F.Commands)
                commands.errors = []
                self.final_groups.reset_mock()
                self.final_groups.side_effect = None
                self.final_groups.return_value = copy.deepcopy(remaining)
                self.assertEqual(commands.close_child({"process": process, "identity": None}), not remaining)
                process.wait.assert_called_once_with(timeout=5)
                if initial_exit is None:
                    process.kill.assert_called_once_with()
                else:
                    process.kill.assert_not_called()
                self.final_groups.assert_called_once_with(process.pid)
                self.assertEqual(commands.errors, [])
        self.final_os.killpg.assert_not_called()
        self.final_identity.assert_not_called()
        self.final_native.Popen.assert_not_called()

    def test_finalizer_identity_failure_after_popen_closes_pipes_and_reaps(self):
        files = F.Files(1000.0)
        saved = {}

        def save(name, value):
            self.assertNotIn(name, saved)
            saved[name] = copy.deepcopy(value)
            return pin(F.FINAL / "private" / name, encoded(value))

        files.save = Mock(side_effect=save)
        files.write = Mock(side_effect=lambda name, raw: pin(F.FINAL / "private" / name, raw))
        streams = {}
        for name, fd in (("stdin", 41), ("stdout", 42), ("stderr", 43)):
            stream = SimpleNamespace(closed=False)
            stream.fileno = Mock(return_value=fd)
            stream.close = Mock(side_effect=lambda selected=stream: setattr(selected, "closed", True))
            streams[name] = stream
        process = SimpleNamespace(pid=6001, returncode=None, **streams)
        process.kill = Mock(side_effect=lambda: setattr(process, "returncode", -9))
        process.wait = Mock(side_effect=lambda timeout: process.returncode)
        owner = {"pid": 5001, "cgroup": "0::/system.slice/synthetic-finalization.service\n", "bootId": "synthetic-boot"}
        commands = None

        def identity(pid):
            if pid == 5001:
                return copy.deepcopy(owner)
            self.assertEqual(pid, process.pid)
            self.assertEqual(len(commands.children), 1)
            self.assertIs(commands.children[0]["process"], process)
            raise F.FinalizationError("synthetic_identity_failure")

        def fstat(fd):
            selected = {41: streams["stdin"], 42: streams["stdout"], 43: streams["stderr"]}[fd]
            self.assertTrue(selected.closed)
            raise OSError(errno.EBADF, "Synthetic descriptor is closed.")

        self.final_identity.side_effect = identity
        self.final_native.Popen.side_effect = None
        self.final_native.Popen.return_value = process
        self.final_groups.side_effect = None
        self.final_groups.return_value = []
        self.final_os.fstat.side_effect = fstat
        commands = F.Commands(files, {"LANG": "C"}, [], [])
        argv = ["/usr/lib/postgresql/17/bin/psql", "-X", "-Atq"]
        payload = b"BEGIN READ ONLY; SELECT 1; COMMIT;"
        with self.assertRaisesRegex(F.FinalizationError, "^synthetic_identity_failure$"):
            commands.execute("cluster-identity", argv, payload, sql=True)
        self.final_native.Popen.assert_called_once_with(argv, stdin=self.final_native.PIPE,
            stdout=self.final_native.PIPE, stderr=self.final_native.PIPE,
            env={"LANG": "C", "PGOPTIONS": "-c default_transaction_read_only=on -c statement_timeout=10000 -c lock_timeout=3000"},
            start_new_session=True, close_fds=True)
        for stream in streams.values():
            stream.close.assert_called_once_with()
            self.assertTrue(stream.closed)
        process.kill.assert_called_once_with()
        process.wait.assert_called_once_with(timeout=5)
        self.assertEqual({row["fd"] for row in files.fd_closures}, {41, 42, 43})
        self.assertTrue(all(row["closed"] and row["error"] is None for row in files.fd_closures))
        self.assertEqual(files.critical, 0)
        self.assertFalse(commands.registering or commands.closing)
        self.assertEqual(commands.errors, [])
        result = saved["sql-001-result.json"]
        self.assertEqual((result["exitCode"], result["outcome"], result["sqlOutcome"]), (-9, "unknown", "unknown"))
        self.assertTrue(result["processGroupClosed"])
        self.assertFalse(result["stdinComplete"])
        self.final_selectors.DefaultSelector.assert_not_called()
        self.final_os.killpg.assert_not_called()


if __name__ == "__main__":
    unittest.main()
