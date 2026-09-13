#!/usr/bin/env python3
"""Append two candidate-only backup limits and perform one controlled restart."""
import argparse
from copy import deepcopy
import hashlib
import json
import os
from pathlib import Path
import signal
import stat
import time


def build_revision_class(base):
    class EnvironmentRevision(base.Transition):
        def __init__(self, runtime, value, input_pin, source_pin, runtime_pin):
            runtime.validate_environment_revision_input(value)
            self.revision_input, self.runtime_pin = value, runtime_pin
            reader = runtime.load_helper("previous_seed_reader", self.previous_seed_helper(runtime, value))
            self.previous = runtime.validate_epoch(reader.descriptor(value["previousEpoch"]))
            runtime.need(self.previous["version"] == 1 and value["runtimeHelper"] == runtime_pin, "environment_revision_requires_previous_product_epoch")
            base.Transition.initialize(self, runtime, {"output": value["output"], "helpers": self.previous["helpers"]}, input_pin, source_pin, runtime_pin)
            self.need(self.s.descriptor(input_pin) == value, "environment_input_strict_parse_changed")
            self.old = deepcopy(self.previous["candidate"])
            self.seed = self.s.descriptor(runtime.SEED)
            self.previous_binding = self.s.descriptor(value["previousSeedBinding"])
            runtime.validate_seed_runtime_binding(self.previous_binding, value["previousEpoch"], self.previous, self.seed)
            self.admission = self.s.descriptor(value["admission03"])
            self.baseline = self.s.descriptor(value["closedState"])
            closure = self.s.descriptor(value["failureCloseout"])
            self.need(closure["kind"] == "audited-candidate-admission03-failure-closeout" and closure["status"] == "failed_attempt_closed_for_configuration_revision" and closure["closedState"] == value["closedState"], "admission03_closeout_binding_invalid")
            self.sessions = runtime.environment_revision_sessions(self.previous_binding, self.admission, self.baseline)
            self.validate_failed_history(self.baseline)
            lineage = runtime.resolve_epoch_lineage(self.previous, self.s.descriptor)
            product = runtime.validate_transition_input(lineage["productInput"])
            self.catalog = self.s.descriptor(product["compiledCatalog"])
            self.before_environment = self.s.read_checked(self.old["runtime"]["path"], self.old["runtime"]["sha256"])
            self.after_environment = runtime.revised_environment(self.before_environment)
            self.calls = {"stop": 0, "replaceEnvironment": 0, "start": 0}
            self.stage = "environment_preflight"

        @staticmethod
        def previous_seed_helper(runtime, value):
            # This descriptor is already anchored by the exact previous epoch hash.
            epoch = json.loads(runtime.read_bootstrap(value["previousEpoch"]))
            return epoch["helpers"]["seed"]

        def validate_failed_history(self, state):
            self.need(set(state["source"]["tables"]) == self.r.TABLES, "configuration_baseline_table_membership")
            tables = state["source"]["tables"]
            self.need(len(tables["users"]) == 8 and len(tables["libraries"]) == len(tables["library_roots"]) == 3 and len(tables["items"]) == 13 and
                all(not tables[name] for name in ("user_item_data", "play_sessions", "encoding_jobs", "client_playback_references", "task_runs", "task_run_children", "task_occurrences", "task_run_requests", "task_triggers")), "configuration_requires_idle_seeded_state")
            documents = state["controlDocuments"]
            payload = documents["operations/current.json"]["payload"]
            operations = payload["operations"]
            self.need(len(operations) == 1 and not payload.get("transition") and len(payload["slots"]) == 2 and
                {(row["slot"], row["state"]) for row in payload["slots"]} == {("primary", "active"), ("recovery", "unclaimed")}, "configuration_recovery_not_idle")
            op = operations[0]
            self.need(op["kind"] == "create" and op["state"] == "failed" and op["phase"] == "finished" and op["authorized"] is True and
                op["errorCode"] == "capacity_exceeded" and op["id"] == self.admission["operations"]["create"]["Id"], "failed_create_not_stable_for_restart")
            entries = documents["backups/.goby-backup-catalog.json"]["entries"]
            self.need(len(entries) == 1, "failed_backup_inventory_changed")
            row, metadata = entries[0], entries[0]["metadata"]
            self.need(row["phase"] == "empty" and row["deleting"] is False and metadata["kind"] == "generated" and metadata["state"] == "failed" and
                metadata["size"] == 0 and metadata["errorCode"] == "quota_exceeded" and metadata["id"] == op["backupId"], "failed_backup_not_stable_for_restart")
            self.r.environment_revision_sessions(self.previous_binding, self.admission, state)

        def open(self):
            value = {"output": str(self.output), "budgets": {"maximumSeconds": 900, "cleanupSeconds": 60, "maximumRequests": 10, "cleanupRequests": 1}}
            self.io = self.r.EpochIO(value, self.input_pin, self.source_pin, self.revision_input["previousEpoch"], self.revision_input["previousSeedBinding"], self.modules)
            try:
                self.io.open()
            finally:
                self.created = self.io.created
            self.p, self.candidate = self.io.provision, self.io.candidate
            self.epoch = {"candidateProcess": self.g.metadata(self.candidate["serverIdentity"]["pid"])}
            self.pin()

        def pin(self):
            # The controlled transition changes the explicit current expectation;
            # it never mutates the immutable previous EpochReader into a fake epoch.
            result = self.s.CandidateIO.pin(self.io)
            self.need({key: result[key] for key in self.g.PROCESS_FIELDS} == self.g.metadata(result["pid"]), "configuration_process_projection_changed")
            self.g.verify_listener(result["pid"], result["listener"])
            return result

        def capture(self, label):
            process = self.pin()
            source = self.sql_json(self.candidate["database"], self.a.snapshot_sql(self.catalog))
            recovery = self.p.database_facts(self.candidate["processes"]["postgres"], self.candidate["recoveryDatabase"])
            trees, documents, fixed = {}, {}, {}
            for root in self.r.TREE_ROOTS:
                trees[root], found = self.tree(root)
                documents.update(found)
            for location, expected in self.baseline["fixedFiles"].items():
                path = Path(location)
                self.need(path.is_relative_to(self.r.C) or path in [Path(pin["path"]) for pin in self.old["units"].values()], "configuration_fixed_path_escaped")
                if expected.get("absent"):
                    self.need(not os.path.lexists(path), "configuration_missing_file_appeared")
                    fixed[location] = {"absent": True}
                elif expected.get("directory"):
                    info = self.s.safe_path(path, (0, self.s.pwd.getpwnam("goby").pw_uid), True)
                    fixed[location] = {"directory": True, "entries": sorted(node.name for node in path.iterdir()), "dev": info.st_dev, "ino": info.st_ino,
                        "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode), "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}
                else:
                    fixed[location] = self.file(path)[0]
            disk = os.statvfs(self.r.C / "data/backups")
            state = {"source": source, "recovery": recovery, "trees": trees, "controlDocuments": documents, "fixedFiles": fixed,
                "diagnostics": self.diagnostics(), "candidateProcess": process, "postgresProcess": self.g.metadata(self.old["postgresIdentity"]["pid"]),
                "lease": self.lease(), "protected": {unit: self.p.show(unit) for unit in self.r.PROTECTED}, "loadedUnits": self.loaded_units(),
                "disk": {"freeBytes": disk.f_bavail * disk.f_frsize, "totalBytes": disk.f_blocks * disk.f_frsize},
                "previousEpoch": self.revision_input["previousEpoch"], "admission03": self.r.ADMISSION03}
            pin = self.save(label + ".json", state)
            self.validate_failed_history(state)
            self.need(state["disk"]["freeBytes"] >= self.r.REQUIRED_BACKUP_FREE and int(self.r.BACKUP_ADDITIONS["GOBY_BACKUP_MAX_TOTAL_BYTES"]) >= self.r.REQUIRED_BACKUP_FREE, "configuration_revision_capacity_headroom")
            return state, pin

        def preserve(self, before, after, allow_environment=False):
            self.need(before["source"]["tables"] == after["source"]["tables"] and before["source"]["sequences"] == after["source"]["sequences"], "configuration_owned_data_changed")
            for field in ("recovery", "trees", "controlDocuments", "postgresProcess", "protected", "loadedUnits"):
                self.need(before[field] == after[field], "configuration_preservation_changed_" + field)
            old, new = deepcopy(before["fixedFiles"]), deepcopy(after["fixedFiles"])
            if allow_environment:
                previous, current = old.pop(str(self.r.C / "private/runtime.env")), new.pop(str(self.r.C / "private/runtime.env"))
                self.need(previous["uid"] == current["uid"] == 0 and previous["mode"] == current["mode"] == 0o600 and previous["gid"] == current["gid"] and
                    previous["sha256"] == self.old["runtime"]["sha256"] and current["sha256"] == self.r.digest(self.after_environment), "configuration_environment_identity_or_bytes")
            self.need(old == new, "configuration_fixed_files_changed")

        def verify_process_environment(self):
            expected = self.r.environment_values(self.after_environment)
            raw = Path("/proc", str(self.candidate["serverIdentity"]["pid"]), "environ").read_bytes()
            self.need(len(raw) <= 1 << 20, "configuration_process_environment_bound")
            actual = {}
            for part in raw.split(b"\0"):
                if b"=" in part:
                    key, value = part.split(b"=", 1)
                    name = key.decode("ascii")
                    self.need(name not in actual, "configuration_process_environment_duplicate")
                    actual[name] = value
            self.need(all(actual.get(key) == val for key, val in expected.items()), "configuration_not_loaded_by_current_process")
            self.pin()
            self.save("effective-process-environment.json", {"pid": self.candidate["serverIdentity"]["pid"], "managedKeys": sorted(expected),
                "managedEnvironmentSha256": self.r.digest(b"\0".join(key.encode() + b"=" + expected[key] for key in sorted(expected)))})

        def run(self):
            self.need(not os.path.lexists(self.output), "configuration_output_collision")
            self.open()
            self.stage = "fresh_configuration_before"
            self.before, before_pin = self.capture("before")
            self.preserve(self.baseline, self.before)
            self.need(self.before["candidateProcess"] == self.baseline["candidateProcess"] and self.before["lease"] == self.baseline["lease"], "configuration_baseline_runtime_changed")
            before_diag = self.before["diagnostics"]
            self.need(before_diag["registry"] == self.baseline["diagnostics"]["registry"], "configuration_baseline_log_registry_changed")
            diagnostics_before_pin = self.save("diagnostics-before.json", before_diag)
            logs = {name: self.file(self.r.C / "private" / name)[0] for name in ("server-unit.log", "postgres-unit.log")}
            logs_before_pin = self.save("unit-logs-before.json", logs)
            path = self.r.C / "private/runtime.env"
            before_facts, raw = self.file(path)
            self.need(raw == self.before_environment and before_facts["uid"] == 0 and before_facts["mode"] == 0o600, "configuration_old_environment_changed")
            self.save("preserve-environment-intent.json", {"before": self.old["runtime"], "facts": before_facts})
            old_copy = self.s.write_once(self.private / "runtime-before.env", raw)
            staged = path.with_name(".runtime-env-next-" + self.output.name)
            self.need(not os.path.lexists(staged), "configuration_staging_collision")
            self.save("stage-environment-intent.json", {"path": str(staged), "afterSha256": self.r.digest(self.after_environment), "additions": self.r.BACKUP_ADDITIONS})
            self.s.write_once(staged, self.after_environment)
            staged_facts, staged_raw = self.file(staged)
            self.r.validate_environment_append(raw, staged_raw)
            self.need(staged_facts["dev"] == before_facts["dev"] and staged_facts["uid"] == 0 and staged_facts["gid"] == before_facts["gid"] and staged_facts["mode"] == 0o600, "configuration_staged_file_identity")
            old_process = self.g.metadata(self.candidate["serverIdentity"]["pid"])
            self.stage = "configuration_stop"
            self.pin()
            self.save("stop-intent.json", {"unit": self.p.units["server"], "process": old_process})
            self.calls["stop"] += 1
            signal.setitimer(signal.ITIMER_REAL, min(self.remaining(), 60))
            try:
                self.p.command("configuration-stop", ["/usr/bin/systemctl", "stop", self.p.units["server"]])
                self.stopped(old_process)
            finally:
                signal.setitimer(signal.ITIMER_REAL, self.remaining())
            self.stage = "replace_environment"
            self.need(self.file(path)[0] == before_facts and self.file(staged)[0] == staged_facts, "configuration_file_changed_before_replace")
            self.save("replace-environment-intent.json", {"before": self.old["runtime"], "preservedCopy": old_copy, "afterSha256": self.r.digest(self.after_environment)})
            self.calls["replaceEnvironment"] += 1
            os.replace(staged, path)
            self.s.sync_dir(path.parent)
            runtime_file = {"path": str(path), "sha256": self.r.digest(self.after_environment)}
            self.s.read_checked(path, runtime_file["sha256"])
            self.save("replace-environment-result.json", runtime_file)
            self.stage = "configuration_start"
            self.save("start-intent.json", {"unit": self.p.units["server"], "runtime": runtime_file, "unchangedBinary": self.old["binary"]})
            self.calls["start"] += 1
            server = self.p.start("server")
            current = deepcopy(self.old)
            current.update(input=self.input_pin, productInput=self.previous["transitionInput"], runtime=runtime_file)
            current["processes"]["server"] = server
            current["serverIdentity"] = self.p.process("server", server)
            self.candidate = self.io.candidate = current
            self.epoch = {"candidateProcess": self.g.metadata(current["serverIdentity"]["pid"])}
            self.stage = "configuration_readiness"
            deadline = time.monotonic() + 60
            signal.setitimer(signal.ITIMER_REAL, min(self.remaining(), 60))
            try:
                while time.monotonic() < deadline:
                    listener = self.p.listener(server, current["serverIdentity"], allow_absent=True)
                    if listener:
                        current["listener"] = listener
                        if self.public_read("/readyz") == (200, {"Status": "ready"}):
                            break
                    time.sleep(1)
                else:
                    self.need(False, "configuration_readiness_timeout")
            finally:
                signal.setitimer(signal.ITIMER_REAL, self.remaining())
            self.need(self.public_read("/healthz") == (200, {"Status": "ok"}), "configuration_health_failed")
            self.verify_process_environment()
            self.stage = "configuration_after_preservation"
            after, after_pin = self.capture("after")
            self.preserve(self.before, after, True)
            after_diag = self.diagnostics(before_diag)
            diagnostic_proof = self.r.compare_diagnostics(before_diag, after_diag)
            diagnostics_after_pin = self.save("diagnostics-after.json", after_diag)
            logs_after = {}
            for name, previous in logs.items():
                actual = self.file(self.r.C / "private" / name, previous["bytes"])[0]
                self.need(actual["prefixSha256"] == previous["sha256"] and actual["bytes"] - previous["bytes"] <= 1 << 20 and
                    all(actual[key] == previous[key] for key in ("dev", "ino", "uid", "gid", "mode")), "configuration_unit_log_changed")
                logs_after[name] = actual
            logs_after_pin = self.save("unit-logs-after.json", logs_after)
            self.s.read_checked(self.old["binary"]["path"], self.old["binary"]["sha256"])
            self.stage = "publish_configuration_epoch"
            epoch = {**deepcopy(self.previous), "version": 2, "transitionInput": self.input_pin, "transitionHelper": self.source_pin, "runtimeHelper": self.runtime_pin,
                "previousEpoch": self.revision_input["previousEpoch"], "productInput": self.previous["transitionInput"], "operationKind": "environment_revision",
                "configurationChange": {"before": self.old["runtime"], "after": runtime_file, "preservedCopy": old_copy, "additions": self.r.BACKUP_ADDITIONS,
                    "requiredFreeBytes": self.r.REQUIRED_BACKUP_FREE, "observedFreeBytesBefore": self.before["disk"]["freeBytes"]},
                "candidate": current, "candidateProcess": self.epoch["candidateProcess"], "postgresProcess": after["postgresProcess"], "lease": after["lease"],
                "before": before_pin, "after": after_pin, "calls": self.calls,
                "preservation": {"ownedTablesExact": 35, "failedOperationAndBackupExact": True, "unchangedBinary": self.old["binary"], "allOtherFixedFilesExact": True,
                    "diagnostics": diagnostic_proof, "diagnosticsProof": {"before": diagnostics_before_pin, "after": diagnostics_after_pin},
                    "unitLogsProof": {"before": logs_before_pin, "after": logs_after_pin}, "failureCloseout": self.r.ADMISSION03_CLOSEOUT, "closedState": self.r.ADMISSION03_STATE}}
            self.r.validate_epoch(epoch)
            self.r.resolve_epoch_lineage(epoch, self.s.descriptor)
            epoch_pin = self.save("runtime-epoch.json", epoch)
            binding = {**deepcopy(self.previous_binding), "version": 2, "runtimeEpoch": epoch_pin, "previousBinding": self.revision_input["previousSeedBinding"],
                "admission03": self.r.ADMISSION03, "failureCloseout": self.r.ADMISSION03_CLOSEOUT, "closedState": self.r.ADMISSION03_STATE, "currentSessions": self.sessions}
            self.r.validate_seed_runtime_binding(binding, epoch_pin, epoch, self.seed)
            return {"status": "running_awaiting_live_acceptance", "operationKind": "environment_revision", "runtimeEpoch": epoch_pin,
                "seedRuntimeBinding": self.save("seed-runtime-binding.json", binding), "candidateAdmissionComplete": False}
    return EnvironmentRevision


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("input", "input-sha256", "source-sha256", "runtime-helper", "runtime-helper-sha256"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    import importlib.util
    spec = importlib.util.spec_from_file_location("environment_runtime", args.runtime_helper)
    runtime = importlib.util.module_from_spec(spec)
    raw = Path(args.runtime_helper).read_bytes()
    if hashlib.sha256(raw).hexdigest() != args.runtime_helper_sha256:
        raise ValueError("runtime_helper_digest_changed")
    exec(compile(raw, args.runtime_helper, "exec"), runtime.__dict__)
    runtime_pin = {"path": args.runtime_helper, "sha256": args.runtime_helper_sha256}
    runtime.read_bootstrap(runtime_pin)
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    runtime.read_bootstrap(source_pin)
    runtime.need(os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and __import__("sys").flags.isolated and __import__("sys").flags.dont_write_bytecode, "authorized_isolated_root_ssh_required")
    os.umask(0o077)
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    value = json.loads(runtime.read_bootstrap(input_pin))
    previous = json.loads(runtime.read_bootstrap(value["previousEpoch"]))
    base = runtime.load_helper("frozen_binary_transition", previous["transitionHelper"])
    job = build_revision_class(base)(runtime, value, input_pin, source_pin, runtime_pin)
    def expired(unused_signal, unused_frame):
        raise runtime.ContractError("environment_revision_deadline_" + job.stage)
    for number in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
        signal.signal(number, expired)
    signal.setitimer(signal.ITIMER_REAL, 900)
    try:
        print(json.dumps(job.run()))
        return 0
    except Exception as error:
        result = {"status": "environment_revision_failed_resources_retained", "stage": job.stage, "code": getattr(error, "code", "environment_revision_failed"),
            "errorType": type(error).__name__, "calls": job.calls, "automaticRetry": False, "automaticRollback": False, "receipt": None,
            "commandResponsibilities": job.p.command_responsibilities if hasattr(job, "p") else []}
        try:
            if job.created:
                result["receipt"] = job.save("failure.json", result)
        except Exception as receipt_error:
            result["receiptErrorType"] = type(receipt_error).__name__
        print(json.dumps(result))
        return 2
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


if __name__ == "__main__":
    raise SystemExit(main())
