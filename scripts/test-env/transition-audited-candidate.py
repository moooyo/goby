#!/usr/bin/env python3
"""Perform exactly one verified same-schema candidate binary transition."""
from __future__ import annotations
import argparse
from copy import deepcopy
from datetime import datetime, timezone, timedelta
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import signal
import stat
import subprocess
import sys
import time
import types


def load_runtime(pin):
    path = Path(pin["path"])
    if not path.is_absolute() or ".." in path.parts:
        raise ValueError("runtime_helper_path_invalid")
    for node in (path, *path.parents):
        info = node.lstat()
        if info.st_uid != 0 or stat.S_ISLNK(info.st_mode) or info.st_mode & 0o022:
            raise ValueError("runtime_helper_authority_invalid")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        raw = stream.read((4 << 20) + 1)
    if len(raw) > 4 << 20 or hashlib.sha256(raw).hexdigest() != pin["sha256"]:
        raise ValueError("runtime_helper_digest_changed")
    module = types.ModuleType("candidate_runtime_contract")
    module.__file__ = str(path)
    exec(compile(raw, str(path), "exec"), module.__dict__)
    module.read_bootstrap(pin)
    return module


class Transition:
    def __init__(self, runtime, value, input_pin, source_pin, runtime_pin):
        runtime.validate_transition_input(value)
        self.initialize(runtime, value, input_pin, source_pin, runtime_pin)
        self.need(self.s.descriptor(input_pin) == value, "transition_input_strict_parse_changed")

    def initialize(self, runtime, value, input_pin, source_pin, runtime_pin):
        self.r, self.value = runtime, value
        self.input_pin, self.source_pin, self.runtime_pin = input_pin, source_pin, runtime_pin
        self.modules = {key: runtime.load_helper(key, pin) for key, pin in value["helpers"].items()}
        self.s, self.pmod, self.g, self.a = [self.modules[key] for key in ("seed", "provision", "gateway", "admission")]
        self.output, self.private = Path(value["output"]), Path(value["output"]) / "private"
        self.stage, self.calls, self.responsibilities = "product_preflight", {"stop": 0, "replace": 0, "start": 0}, []
        self.started, self.created, self.public_count = time.monotonic(), False, 0

    def need(self, value, code):
        self.r.need(value, code)

    def remaining(self):
        left = self.r.LIMITS["maximumSeconds"] - (time.monotonic() - self.started)
        self.need(left > 0, "transition_deadline_expired")
        return left

    def save(self, name, value):
        return self.s.write_json_once(self.private / name, value)

    def products(self):
        v = self.value
        self.need(not os.path.lexists(self.output), "transition_output_collision")
        report = self.s.descriptor(v["newFullReport"])
        worker = self.s.parse(self.s.read_checked(self.r.F / "worker-report.json", report["worker_report_sha256"]))
        self.r.validate_full_report(report, worker, v)
        archive = self.s.read_checked(self.r.F / "source.tar", self.r.ARCHIVE)
        sources = self.s.descriptor(v["newSourceManifest"])
        self.need(self.r.archive_manifest(archive) == sources, "source_manifest_not_bound_to_verified_archive")
        source_root = self.r.F / "source"
        entries = list(source_root.rglob("*"))
        self.need(isinstance(sources, dict) and 100 <= len(sources) <= 10000 and len(entries) <= 15000 and all(not node.is_symlink() for node in entries) and
                   {str(node.relative_to(source_root)) for node in entries if node.is_file()} == set(sources), "verified_source_membership_changed")
        for name, row in sources.items():
            self.remaining()
            self.pmod.file_spec(row)
            raw = self.s.read_checked(source_root / self.pmod.relative(name), row["sha256"])
            self.need(len(raw) == row["bytes"], "verified_source_bytes_changed")
        self.binary = self.s.read_checked(v["newBinary"]["path"], v["newBinary"]["sha256"])
        self.need(len(self.binary) == worker["binary"]["bytes"], "new_binary_length_changed")
        self.catalog = self.s.descriptor(v["compiledCatalog"])
        self.a.validate_catalog(self.catalog, sources, v["compiledCatalog"])
        frontend = self.s.descriptor(v["frontendReport"])
        self.need(frontend["status"] == "passed" and len(frontend["assets"]) == 57 and frontend["sourceFilesMatched"] == 64, "frontend_receipt_changed")
        self.old = self.s.descriptor(self.r.PROVISION)
        self.need(v["frontendReport"] == self.old["frontendReport"] and self.old["binary"]["sha256"] == self.r.OLD_BINARY, "old_product_identity_changed")
        old_input = self.s.descriptor(self.old["input"])
        old_sources = self.s.descriptor(old_input["sourceManifest"])
        unchanged = [name for name in set(sources) | set(old_sources) if name.startswith("web/admin/") or name.startswith("internal/database/migrations/") or name == self.r.CATALOG_RELATIVE]
        self.need(len([name for name in unchanged if name.startswith("web/admin/")]) == 64 and all(sources[name] == old_sources[name] for name in unchanged), "schema_or_frontend_source_changed")
        for name, row in frontend["assets"].items():
            self.need(not Path(name).is_absolute() and ".." not in Path(name).parts, "frontend_asset_path_invalid")
            self.s.read_checked(self.r.C / "install/admin" / name, row["sha256"])
        self.seed = self.s.descriptor(self.r.SEED)
        self.s.descriptor(self.r.SEED_ADDENDUM)
        self.admission = self.s.descriptor(self.r.ADMISSION02)
        self.need(self.value["helpers"]["admission"] == self.admission["helper"], "snapshot_helper_must_be_frozen_admission02_source")
        self.sessions = self.r.current_session_contract(self.seed, self.admission)
        self.details = self.s.descriptor(self.seed["actualCatalogDtos"])
        self.contract = self.r.validate_reviewed_contract(self.s.descriptor(v["reviewedStateContract"]))
        reviewed_source = self.s.descriptor(self.contract["sourceSnapshot"])
        self.r.validate_seeded_state(reviewed_source, self.seed, self.details, self.admission, self.catalog, self.a)
        self.need(self.r.validate_control_documents(self.s.descriptor(self.contract["controlDocuments"])) == self.contract["preserved"]["generation"], "reviewed_control_documents_do_not_match_generation")
        return reviewed_source

    def check_captured(self):
        """Replay only saved artifacts; never read live state or admit a product."""
        seed, admission = self.s.descriptor(self.r.SEED), self.s.descriptor(self.r.ADMISSION02)
        self.need(self.value["helpers"]["admission"] == admission["helper"], "captured_snapshot_helper_source_changed")
        contract = self.r.validate_reviewed_contract(self.s.descriptor(self.value["reviewedStateContract"]))
        catalog = self.s.descriptor(self.value["compiledCatalog"])
        result = self.r.validate_seeded_state(self.s.descriptor(contract["sourceSnapshot"]), seed, self.s.descriptor(seed["actualCatalogDtos"]), admission, catalog, self.a)
        self.need(self.r.validate_control_documents(self.s.descriptor(contract["controlDocuments"])) == contract["preserved"]["generation"], "captured_control_binding_differs")
        self.need(self.s.descriptor(contract["diagnosticsSnapshot"])["registry"]["version"] == 1, "captured_diagnostic_registry_invalid")
        return {"status": "captured_contract_checked_not_product_admitted", "state": result, "newSqlQueries": 0, "httpRequests": 0, "businessWrites": 0}

    def open(self):
        io_value = {"output": str(self.output), "candidateManifest": self.r.PROVISION, "runtimeInspection": self.s.INSPECTION,
                    "runtimeHelper": self.value["helpers"]["provision"], "budgets": {"maximumSeconds": 900, "cleanupSeconds": 60, "maximumRequests": 10, "cleanupRequests": 1}}
        self.io = self.s.CandidateIO(io_value, self.input_pin, self.source_pin)
        try:
            self.io.open()
        finally:
            self.created = self.io.created
        self.p, self.candidate = self.io.provision, self.io.candidate
        self.epoch = {"candidateProcess": self.g.metadata(self.candidate["serverIdentity"]["pid"])}
        self.pin()

    def pin(self):
        result = self.io.pin()
        pid = result["pid"]
        metadata = self.g.metadata(pid)
        self.need({key: result[key] for key in self.g.PROCESS_FIELDS} == metadata, "candidate_metadata_projection_changed")
        self.g.verify_listener(pid, result["listener"])
        return result

    def sql_json(self, database, query):
        self.need(database in ("postgres", self.candidate["database"], self.candidate["recoveryDatabase"]) and query.startswith("SELECT ") and ";" not in query, "transition_sql_scope_invalid")
        self.need(self.p.cluster(self.candidate["processes"]["postgres"]) == self.candidate["clusterSystemIdentifier"], "postgres_cluster_changed")
        return self.s.parse(self.p.psql("transition-read-only", "BEGIN READ ONLY; " + query + "; COMMIT;", database))

    def lease(self):
        return self.r.EpochReader.deployment_lease(self)

    def file(self, path, prefix=None):
        path = Path(path)
        uid = self.s.pwd.getpwnam("goby").pw_uid
        before = self.s.safe_path(path, (0, uid))
        self.need(before.st_nlink == 1 and before.st_size <= 256 << 20, "preserved_file_bound")
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
            self.need(self.s.file_identity(os.fstat(stream.fileno())) == self.s.file_identity(before), "preserved_file_open_changed")
            raw = stream.read((256 << 20) + 1)
            self.need(self.s.file_identity(os.fstat(stream.fileno())) == self.s.file_identity(before), "preserved_file_read_changed")
        self.need(self.s.file_identity(path.stat()) == self.s.file_identity(before) and len(raw) == before.st_size, "preserved_file_changed")
        facts = {"dev": before.st_dev, "ino": before.st_ino, "uid": before.st_uid, "gid": before.st_gid, "mode": stat.S_IMODE(before.st_mode), "bytes": len(raw), "sha256": self.r.digest(raw), "mtimeNs": before.st_mtime_ns, "ctimeNs": before.st_ctime_ns}
        if prefix is not None:
            self.need(prefix <= len(raw), "preserved_log_truncated")
            facts["prefixSha256"] = self.r.digest(raw[:prefix])
        return facts, raw

    def tree(self, root):
        path = Path(root)
        entries = [path, *path.rglob("*")]
        self.need(len(entries) <= 2048 and all(not node.is_symlink() for node in entries), "owned_tree_inventory_bound")
        result, documents, total = {}, {}, 0
        for node in entries:
            self.remaining()
            relative = str(node.relative_to(path))
            if node.is_dir():
                info = self.s.safe_path(node, (0, self.s.pwd.getpwnam("goby").pw_uid), True)
                result[relative] = {"directory": True, "dev": info.st_dev, "ino": info.st_ino, "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode), "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}
            else:
                result[relative], raw = self.file(node)
                total += len(raw)
                self.need(total <= 512 << 20, "owned_tree_byte_limit")
                if root in self.r.TREE_ROOTS[:3] and node.suffix == ".json":
                    documents[str(node.relative_to(self.r.C / "data"))] = self.s.parse(raw)
        return result, documents

    def loaded_units(self):
        fields = "Type,User,Group,Restart,ProtectSystem,ProtectHome,PrivateTmp,NoNewPrivileges,ReadWritePaths,InaccessiblePaths,FragmentPath,DropInPaths,EnvironmentFiles,ExecStart,TimeoutStopUSec,KillMode"
        result = {}
        for role, unit in self.p.units.items():
            observed = subprocess.run(["/usr/bin/systemctl", "show", unit, "--property=" + fields], capture_output=True, timeout=10, check=True, env=self.pmod.ENV)
            row = dict(line.split("=", 1) for line in observed.stdout.decode().splitlines() if "=" in line)
            argv = self.pmod.shlex.split(row.pop("ExecStart").split("argv[]=", 1)[1].split(";", 1)[0])
            self.need(argv == self.p.argv[role] and row["Type"] == "exec" and row["Restart"] == "no" and not row["DropInPaths"], "loaded_unit_command_or_restart_changed")
            row["argv"] = argv
            result[role] = row
        return result

    def diagnostics(self, before=None):
        root = self.r.C / "data/diagnostics"
        registry_facts, raw = self.file(root / ".goby-diagnostics.json")
        registry = self.s.parse(raw)
        self.need(registry["version"] == 1 and re.fullmatch(r"[0-9a-f]{32}", registry["token"]) and len(registry["files"]) <= 15 and len({row["name"] for row in registry["files"]}) == len(registry["files"]), "diagnostic_registry_inventory_invalid")
        names = {row["name"] for row in registry["files"]}
        self.need({node.name for node in root.iterdir()} == names | {".goby-diagnostics.json", ".goby-diagnostics.lock"}, "unregistered_diagnostic_file")
        files = {}
        for row in registry["files"]:
            self.need(re.fullmatch(r"goby-" + registry["token"] + r"-[0-9a-f]{32}\.jsonl", row["name"]) and not row.get("deleting"), "diagnostic_registry_entry_invalid")
            prefix = before["files"][row["name"]]["bytes"] if before and row["name"] in before["files"] else None
            files[row["name"]] = self.file(root / row["name"], prefix)[0]
            self.need(files[row["name"]]["bytes"] < 4 << 20 and row["identity"] == {"device": files[row["name"]]["dev"], "inode": files[row["name"]]["ino"]}, "diagnostic_file_authority_changed")
            if before is None:
                created = datetime.fromisoformat(row["created"].replace("Z", "+00:00"))
                self.need(created > datetime.now(timezone.utc) + timedelta(seconds=900) - timedelta(days=7), "diagnostic_pruning_would_be_required")
        if before is None:
            self.need(len(files) <= 14 and sum(row["closed"] is False for row in registry["files"]) == 1, "diagnostic_transition_headroom_invalid")
        info = root.stat()
        return {"registry": registry, "registryFile": registry_facts, "files": files, "lock": self.file(root / ".goby-diagnostics.lock")[0],
                "directory": {"dev": info.st_dev, "ino": info.st_ino, "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode)}}

    def capture(self, label):
        self.pin()
        source = self.sql_json(self.candidate["database"], self.a.snapshot_sql(self.catalog))
        self.r.validate_seeded_state(source, self.seed, self.details, self.admission, self.catalog, self.a)
        source_database = self.p.database_facts(self.candidate["processes"]["postgres"], self.candidate["database"])
        self.need(source_database == self.old["databases"]["source"]["afterStart"], "source_database_owner_or_schema_changed")
        recovery = self.p.database_facts(self.candidate["processes"]["postgres"], self.candidate["recoveryDatabase"])
        self.need(recovery == self.old["databases"]["recovery"]["afterStart"], "recovery_target_not_original_empty_database")
        trees, documents = {}, {}
        for root in self.r.TREE_ROOTS:
            trees[root], found = self.tree(root)
            documents.update(found)
        self.save(label + "-control-documents.json", documents)
        self.save(label + "-tree-facts.json", trees)
        self.need(len([row for row in trees[str(self.r.C / "data/media")].values() if not row.get("directory")]) == 14 and
                   len([row for row in trees[str(self.r.C / "install/admin")].values() if not row.get("directory")]) == 57, "media_or_asset_inventory_changed")
        self.need(set(trees[str(self.r.C / "data/cache")]) == {".", ".goby-transcode-cache", ".goby-transcode-lock"}, "cache_job_recovery_would_be_required")
        generation = self.r.validate_control_documents(documents)
        self.control_documents = documents
        fixed = {pin["path"]: self.file(pin["path"])[0] for pin in [self.r.PROVISION, self.old["runtime"], *self.old["units"].values()]}
        data_root = self.r.C / "data"
        children = list(data_root.iterdir())
        self.need(len(children) <= 64 and all(not path.is_symlink() for path in children) and
                   {path.name for path in children if path.is_dir()} == {"recovery", "operations", "backups", "cache", "media", "diagnostics"}, "uncovered_data_root_directory")
        root_stat = self.s.safe_path(data_root, (0, self.s.pwd.getpwnam("goby").pw_uid), True)
        fixed[str(data_root)] = {"directory": True, "entries": sorted(path.name for path in children), "dev": root_stat.st_dev, "ino": root_stat.st_ino,
                                 "uid": root_stat.st_uid, "gid": root_stat.st_gid, "mode": stat.S_IMODE(root_stat.st_mode), "mtimeNs": root_stat.st_mtime_ns, "ctimeNs": root_stat.st_ctime_ns}
        for path in children:
            if not path.is_dir():
                fixed[str(path)] = self.file(path)[0]
        fixed.setdefault(str(data_root / "master.key"), {"absent": True})
        result = {"source": source, "recovery": {"source": source_database, "recovery": recovery}, "trees": trees, "fixedFiles": fixed, "generation": generation,
                  "protected": {unit: self.p.show(unit) for unit in self.r.PROTECTED}, "postgresProcess": self.g.metadata(self.old["postgresIdentity"]["pid"])}
        if getattr(self, "contract", None) is not None:
            self.need(self.loaded_units() == self.contract["loadedUnits"], "loaded_unit_properties_changed")
        self.pin()
        return result, self.save(label + ".json", result)

    def public_read(self, route):
        self.need(route in ("/readyz", "/healthz", "/emby/System/Info/Public") and self.public_count < 10, "public_read_budget_or_route")
        self.pin()
        self.public_count += 1
        prefix = "public-%02d" % self.public_count
        self.save(prefix + "-intent.json", {"method": "GET", "route": route, "process": self.epoch["candidateProcess"]})
        connection = http.client.HTTPConnection("127.0.0.1", self.candidate["ports"]["http"], timeout=min(3, self.remaining()))
        try:
            connection.request("GET", route, headers={"Connection": "close", "Accept-Encoding": "identity"})
            response = connection.getresponse()
            raw = response.read(65537)
            complete = len(raw) <= 65536 and response.length in (None, 0)
            self.save(prefix + "-response.json", {"status": response.status, "headers": response.getheaders(), "bodySha256": self.r.digest(raw), "complete": complete})
            self.need(complete and response.getheader("Set-Cookie") is None and response.status in (200, 503), "public_response_invalid")
            value = self.s.parse(raw)
        finally:
            connection.close()
        self.pin()
        return response.status, value

    def stopped(self, old_process):
        unit = self.p.show(self.p.units["server"])
        self.need(unit["ActiveState"] == "inactive" and unit["MainPID"] == "0" and unit["Result"] == "success" and unit["ExecMainStatus"] == "0", "candidate_not_cleanly_stopped")
        self.need(not Path("/proc", str(old_process["pid"])).exists(), "old_candidate_process_still_present")
        group = Path("/sys/fs/cgroup") / self.old["processes"]["server"]["ControlGroup"].lstrip("/")
        self.need(not group.exists() or all(not path.read_text().strip() for path in [group / "cgroup.procs", *group.rglob("cgroup.procs")] if path.exists()), "old_recursive_cgroup_not_empty")
        for table in ("tcp", "tcp6"):
            rows = [line.split() for line in Path("/proc/net", table).read_text().splitlines()[1:]]
            self.need(not any(row[3] == "0A" and row[1].rsplit(":", 1)[-1] == "%04X" % self.candidate["ports"]["http"] for row in rows), "old_listener_or_competitor_remains")
        key = 4919415424202458201
        count = self.sql_json(self.candidate["database"], "SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND classid=" + str(key >> 32) + "::oid AND objid=" + str(key & 0xffffffff) + "::oid AND objsubid=1 AND database=(SELECT oid FROM pg_database WHERE datname='" + self.candidate["database"] + "')")
        self.need(count == 0 and self.g.metadata(self.old["postgresIdentity"]["pid"]) == self.before["postgresProcess"], "old_lease_not_released_or_postgres_changed")
        self.save("stopped.json", {"unit": unit, "oldProcess": old_process, "leaseCount": count})

    def run(self):
        reviewed_source = self.products()
        self.open()
        self.stage = "fresh_before_state"
        self.before, before_pin = self.capture("before")
        self.need(self.r.canonical(self.before["source"]["tables"]) == self.r.canonical(reviewed_source["tables"]) and self.before["source"]["sequences"] == reviewed_source["sequences"], "fresh_state_differs_from_reviewed_contract")
        self.need({key: self.before[key] for key in self.contract["preserved"]} == self.contract["preserved"], "fresh_resources_differ_from_reviewed_contract")
        reviewed_diagnostics = self.s.descriptor(self.contract["diagnosticsSnapshot"])
        diagnostics_before = self.diagnostics()
        self.need(diagnostics_before["registry"] == reviewed_diagnostics["registry"] and diagnostics_before["lock"] == reviewed_diagnostics["lock"], "reviewed_diagnostic_identity_changed")
        diagnostics_before_pin = self.save("diagnostics-before.json", diagnostics_before)
        logs = {name: self.file(self.r.C / "private" / name)[0] for name in ("server-unit.log", "postgres-unit.log")}
        logs_before_pin = self.save("unit-logs-before.json", logs)
        lease_before_pin = self.save("lease-before.json", self.lease())
        old_process = self.g.metadata(self.candidate["serverIdentity"]["pid"])
        old_facts, old_binary = self.file(self.r.C / "install/goby")
        self.need(old_facts["sha256"] == self.r.OLD_BINARY, "installed_old_binary_changed")
        self.save("binary-preservation-intent.json", {"facts": old_facts, "source": self.old["binary"]})
        old_copy = self.s.write_once(self.private / "goby-before.bin", old_binary)
        staged = self.r.C / "install" / (".goby-next-" + self.output.name)
        self.need(not os.path.lexists(staged), "staged_binary_collision")
        self.save("stage-binary-intent.json", {"path": str(staged), "source": self.value["newBinary"]})
        self.p.write(staged, self.binary, mode=old_facts["mode"])
        staged_facts, unused = self.file(staged)
        self.need(staged_facts["sha256"] == self.value["newBinary"]["sha256"] and staged_facts["dev"] == old_facts["dev"] and staged_facts["uid"] == old_facts["uid"] == 0, "staged_binary_authority_changed")
        self.stage = "stop"
        self.pin()
        self.save("stop-intent.json", {"unit": self.p.units["server"], "process": old_process})
        self.calls["stop"] += 1
        signal.setitimer(signal.ITIMER_REAL, min(self.remaining(), self.r.LIMITS["stopSeconds"]))
        try:
            self.p.command("candidate-stop", ["/usr/bin/systemctl", "stop", self.p.units["server"]])
            self.stopped(old_process)
        finally:
            signal.setitimer(signal.ITIMER_REAL, self.remaining())
        self.stage = "replace"
        self.need(self.file(self.r.C / "install/goby")[0] == old_facts and self.file(staged)[0] == staged_facts, "binary_authority_changed_before_replace")
        self.save("replace-intent.json", {"destination": str(self.r.C / "install/goby"), "staged": str(staged), "oldCopy": old_copy})
        self.calls["replace"] += 1
        os.replace(staged, self.r.C / "install/goby")
        self.s.sync_dir(self.r.C / "install")
        installed = {"path": str(self.r.C / "install/goby"), "sha256": self.value["newBinary"]["sha256"]}
        self.s.read_checked(installed["path"], installed["sha256"])
        self.save("replace-result.json", installed)
        self.stage = "start"
        self.save("start-intent.json", {"unit": self.p.units["server"], "binary": installed})
        self.calls["start"] += 1
        server = self.p.start("server")
        current = deepcopy(self.old)
        current.update(kind="audited-candidate-runtime-state", input=self.input_pin, originalProvision=self.r.PROVISION, seedProvenance=self.r.SEED,
                       currentSourceManifest=self.value["newSourceManifest"], binary=installed, backendReport=self.value["newFullReport"], bootstrapExecuted=True,
                       sourceState={"users": 8, "schema": 28, "migrations": 28})
        current.pop("setupToken", None)
        current["processes"]["server"] = server
        current["serverIdentity"] = self.p.process("server", server)
        self.candidate = self.io.candidate = current
        self.epoch = {"candidateProcess": self.g.metadata(current["serverIdentity"]["pid"])}
        self.stage = "readiness"
        ready_deadline = time.monotonic() + 60
        signal.setitimer(signal.ITIMER_REAL, min(self.remaining(), self.r.LIMITS["readySeconds"]))
        try:
            while time.monotonic() < ready_deadline:
                listener = self.p.listener(server, current["serverIdentity"], allow_absent=True)
                if listener:
                    current["listener"] = listener
                    status, body = self.public_read("/readyz")
                    if status == 200 and body == {"Status": "ready"}:
                        break
                time.sleep(1)
            else:
                raise self.r.ContractError("new_candidate_readiness_expired")
        finally:
            signal.setitimer(signal.ITIMER_REAL, self.remaining())
        self.need(self.public_read("/healthz") == (200, {"Status": "ok"}), "new_candidate_health_failed")
        status, public = self.public_read("/emby/System/Info/Public")
        self.need(status == 200 and public["Id"] == self.seed["serverId"] and public["StartupWizardCompleted"] is True and public["LocalAddress"] == self.old["publicUrl"], "new_candidate_public_identity_changed")
        lease = self.lease()
        self.stage = "after_preservation"
        after, after_pin = self.capture("after")
        preservation = self.r.compare_preservation(self.before, after)
        diagnostics_after = self.diagnostics(diagnostics_before)
        preservation["diagnostics"] = self.r.compare_diagnostics(diagnostics_before, diagnostics_after)
        diagnostics_after_pin = self.save("diagnostics-after.json", diagnostics_after)
        logs_after = {}
        for name, previous in logs.items():
            facts = self.file(self.r.C / "private" / name, previous["bytes"])[0]
            self.need(facts["prefixSha256"] == previous["sha256"] and facts["bytes"] - previous["bytes"] <= 1 << 20 and all(facts[key] == previous[key] for key in ("dev", "ino", "uid", "gid", "mode")), "unit_log_prefix_or_identity_changed")
            logs_after[name] = facts
        preservation.update(oldBinaryCopy=old_copy, oldBinaryFacts=old_facts, previousLease=lease_before_pin,
            diagnosticsProof={"before": diagnostics_before_pin, "after": diagnostics_after_pin},
            unitLogsProof={"before": logs_before_pin, "after": self.save("unit-logs-after.json", logs_after)})
        self.pin()
        self.remaining()
        self.stage = "publish_epoch"
        epoch = {"kind": "audited-candidate-runtime-epoch", "version": 1, "status": "running_awaiting_live_acceptance", "transitionInput": self.input_pin,
                 "transitionHelper": self.source_pin, "runtimeHelper": self.runtime_pin, "originalProvision": self.r.PROVISION, "seedProvenance": self.r.SEED,
                 "currentSource": {"archiveSha256": self.r.ARCHIVE, "sourceManifest": self.value["newSourceManifest"], "binary": installed, "fullReport": self.value["newFullReport"], "schema": 28},
                 "candidate": current, "candidateProcess": self.epoch["candidateProcess"], "postgresProcess": after["postgresProcess"], "lease": lease,
                 "before": before_pin, "after": after_pin, "preservation": preservation, "calls": self.calls, "helpers": self.value["helpers"], "candidateAdmissionComplete": False}
        self.r.validate_epoch(epoch)
        epoch_pin = self.save("runtime-epoch.json", epoch)
        binding = {"kind": "audited-candidate-seed-runtime-binding", "version": 1, "runtimeEpoch": epoch_pin, "originalSeed": self.r.SEED,
                   "seedExecutor": self.seed["helper"], "seedInput": self.seed["input"], "seedSessionAddendum": self.r.SEED_ADDENDUM, "admission02": self.r.ADMISSION02,
                   **{key: self.seed[key] for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources")},
                   "seedCleanup": self.seed["cleanup"], "currentSessions": self.sessions, "candidateAdmissionComplete": False}
        self.r.validate_seed_runtime_binding(binding, epoch_pin, epoch, self.seed)
        return {"status": "running_awaiting_live_acceptance", "runtimeEpoch": epoch_pin, "seedRuntimeBinding": self.save("seed-runtime-binding.json", binding), "candidateAdmissionComplete": False}


def capture_reviewed_state(runtime, helper_pins, output, input_pin, source_pin, runtime_pin):
    """Fresh read-only preparation, independent of any future full/build input."""
    runtime.need(Path(output).parent == runtime.R and re.fullmatch(r"candidate-transition-state-capture-[0-9]{2}", Path(output).name) and
                 set(helper_pins) == runtime.HELPERS and helper_pins["gateway"]["sha256"] == runtime.GATEWAY_SOURCE_SHA, "read_only_capture_scope_invalid")
    job = Transition.__new__(Transition)
    job.initialize(runtime, {"output": output, "helpers": helper_pins}, input_pin, source_pin, runtime_pin)
    runtime.read_bootstrap(source_pin)
    runtime.read_bootstrap(runtime_pin)
    preparation_input = job.s.descriptor(input_pin)
    runtime.need(preparation_input == {"kind": "audited-candidate-state-capture-input", "version": 1, "output": output, "helpers": helper_pins}, "read_only_capture_input_changed")
    job.stage = "read_only_state_preparation"
    job.old = job.s.descriptor(runtime.PROVISION)
    job.seed = job.s.descriptor(runtime.SEED)
    job.s.descriptor(runtime.SEED_ADDENDUM)
    job.admission = job.s.descriptor(runtime.ADMISSION02)
    runtime.need(helper_pins["admission"] == job.admission["helper"], "capture_snapshot_helper_must_be_frozen_admission02_source")
    job.sessions = runtime.current_session_contract(job.seed, job.admission)
    job.details = job.s.descriptor(job.seed["actualCatalogDtos"])
    original_input = job.s.descriptor(job.old["input"])
    source = job.s.descriptor(original_input["sourceManifest"])
    catalog_path = Path(original_input["sourceManifest"]["path"]).parent / "source" / runtime.CATALOG_RELATIVE
    job.catalog = job.s.parse(job.s.read_checked(catalog_path, source[runtime.CATALOG_RELATIVE]["sha256"]))
    job.contract = None
    try:
        job.open()
        captured, unused = job.capture("captured-state")
        diagnostic = job.diagnostics()
        contract = {"kind": "audited-candidate-reviewed-state-contract", "version": 1,
            "sourceSnapshot": job.save("source-snapshot.json", captured["source"]), "controlDocuments": job.save("control-documents.json", job.control_documents),
            "diagnosticsSnapshot": job.save("diagnostics.json", diagnostic), "preserved": {key: captured[key] for key in ("recovery", "trees", "fixedFiles", "protected", "postgresProcess", "generation")},
            "loadedUnits": job.loaded_units(), "diagnosticPolicy": {"maximumFilesBefore": 14, "retentionDays": 7, "maximumLogBytes": 4 << 20, "maximumAppendBytes": 1 << 20, "pruningAllowed": False}}
        runtime.validate_reviewed_contract(contract)
        job.save("capture-source.json", {"input": input_pin, "helper": source_pin, "runtimeHelper": runtime_pin, "helpers": helper_pins, "httpRequests": 0, "businessWrites": 0, "productGateEvaluated": False})
        return {"status": "captured_for_internal_review", "reviewedStateContract": job.save("reviewed-state-contract.json", contract), "productGateEvaluated": False, "httpRequests": 0, "businessWrites": 0}
    except Exception as error:
        result = {"status": "read_only_state_capture_failed", "stage": job.stage, "code": getattr(error, "code", "read_only_capture_failed"), "errorType": type(error).__name__, "httpRequests": 0, "businessWrites": 0, "receipt": None}
        try:
            if job.created:
                result["receipt"] = job.save("failure.json", result)
        except Exception as receipt_error:
            result["receiptErrorType"] = type(receipt_error).__name__
        return result


def main():
    if not (sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode):
        raise ValueError("authorized_isolated_root_ssh_required")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("input", "input-sha256", "source-sha256", "runtime-helper", "runtime-helper-sha256"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--check-captured", action="store_true")
    args = parser.parse_args()
    runtime_pin = {"path": args.runtime_helper, "sha256": args.runtime_helper_sha256}
    r = load_runtime(runtime_pin)
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    r.read_bootstrap(source_pin)
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    value = json.loads(r.read_bootstrap(input_pin))
    job = Transition(r, value, input_pin, source_pin, runtime_pin)
    if args.check_captured:
        try:
            print(json.dumps(job.check_captured()))
            return 0
        except Exception as error:
            print(json.dumps({"status": "captured_contract_rejected", "stage": "captured_replay", "code": getattr(error, "code", "captured_contract_invalid"), "errorType": type(error).__name__, "businessWrites": 0}))
            return 2
    def expired(unused_signal, unused_frame):
        raise r.ContractError("transition_deadline_" + job.stage)
    for number in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
        signal.signal(number, expired)
    signal.setitimer(signal.ITIMER_REAL, r.LIMITS["maximumSeconds"])
    try:
        result = job.run()
        print(json.dumps(result))
        return 0
    except Exception as error:
        value = {"status": "transition_failed_resources_retained", "stage": job.stage, "code": getattr(error, "code", "transition_operation_failed"), "errorType": type(error).__name__,
                 "output": str(job.output), "calls": job.calls, "automaticRetry": False, "automaticRollback": False, "candidateAdmissionComplete": False, "receipt": None,
                 "commandResponsibilities": job.p.command_responsibilities if hasattr(job, "p") else []}
        try:
            if job.created:
                value["receipt"] = job.save("failure.json", value)
        except Exception as receipt_error:
            value["receiptErrorType"] = type(receipt_error).__name__
        print(json.dumps(value))
        return 2
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


if __name__ == "__main__":
    raise SystemExit(main())
