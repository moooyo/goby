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
        self.old_binary_sha = runtime.OLD_BINARY

    def need(self, value, code):
        self.r.need(value, code)

    def remaining(self):
        left = self.r.LIMITS["maximumSeconds"] - (time.monotonic() - self.started)
        self.need(left > 0, "transition_deadline_expired")
        return left

    def save(self, name, value):
        return self.s.write_json_once(self.private / name, value)

    def product_files(self, scope, archive_sha, validate_report):
        v = self.value
        self.need(not os.path.lexists(self.output), "transition_output_collision")
        report = self.s.descriptor(v["newFullReport"])
        worker = self.s.parse(self.s.read_checked(scope / "worker-report.json", report["worker_report_sha256"]))
        validate_report(report, worker, v)
        archive = self.s.read_checked(scope / "source.tar", archive_sha)
        sources = self.s.descriptor(v["newSourceManifest"])
        self.need(self.r.archive_manifest(archive) == sources, "source_manifest_not_bound_to_verified_archive")
        source_root = scope / "source"
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
        return sources, frontend

    def products(self):
        v = self.value
        sources, frontend = self.product_files(self.r.F, self.r.ARCHIVE, self.r.validate_full_report)
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

    def check_fresh_before(self, reviewed_source):
        self.need(self.r.canonical(self.before["source"]["tables"]) == self.r.canonical(reviewed_source["tables"]) and self.before["source"]["sequences"] == reviewed_source["sequences"], "fresh_state_differs_from_reviewed_contract")
        self.need({key: self.before[key] for key in self.contract["preserved"]} == self.contract["preserved"], "fresh_resources_differ_from_reviewed_contract")
        return self.s.descriptor(self.contract["diagnosticsSnapshot"])

    def adopt_runtime_context(self, candidate, process):
        self.candidate = self.io.candidate = candidate
        self.epoch = {"candidateProcess": process}

    def preserve(self, after, installed):
        return self.r.compare_preservation(self.before, after)

    def run(self):
        reviewed_source = self.products()
        self.open()
        self.stage = "fresh_before_state"
        self.before, before_pin = self.capture("before")
        if self.value["version"] == 3:
            self.before_pin = before_pin
        reviewed_diagnostics = self.check_fresh_before(reviewed_source)
        diagnostics_before = self.diagnostics()
        self.need(diagnostics_before["registry"] == reviewed_diagnostics["registry"] and diagnostics_before["lock"] == reviewed_diagnostics["lock"], "reviewed_diagnostic_identity_changed")
        diagnostics_before_pin = self.save("diagnostics-before.json", diagnostics_before)
        logs = {name: self.file(self.r.C / "private" / name)[0] for name in ("server-unit.log", "postgres-unit.log")}
        logs_before_pin = self.save("unit-logs-before.json", logs)
        lease_before_pin = self.save("lease-before.json", self.lease())
        old_process = self.g.metadata(self.candidate["serverIdentity"]["pid"])
        old_facts, old_binary = self.file(self.r.C / "install/goby")
        self.need(old_facts["sha256"] == self.old_binary_sha, "installed_old_binary_changed")
        self.save("binary-preservation-intent.json", {"facts": old_facts, "source": self.old["binary"]})
        old_copy = self.s.write_once(self.private / "goby-before.bin", old_binary)
        if self.value["version"] == 3:
            self.old_binary_copy = old_copy
        staged = self.r.C / "install" / (".goby-next-" + self.output.name)
        self.need(not os.path.lexists(staged), "staged_binary_collision")
        self.save("stage-binary-intent.json", {"path": str(staged), "source": self.value["newBinary"]})
        self.p.write(staged, self.binary, mode=old_facts["mode"])
        staged_facts, unused = self.file(staged)
        self.need(staged_facts["sha256"] == self.value["newBinary"]["sha256"] and staged_facts["dev"] == old_facts["dev"] and staged_facts["uid"] == old_facts["uid"] == 0, "staged_binary_authority_changed")
        if self.value["version"] in (2, 3):
            self.need(all(staged_facts[key] == old_facts[key] for key in ("gid", "mode")), "successor_staged_binary_permissions_changed")
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
        self.adopt_runtime_context(current, self.g.metadata(current["serverIdentity"]["pid"]))
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
        preservation = self.preserve(after, installed)
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
        return self.publish_epoch(current, installed, before_pin, after, after_pin, preservation, lease)

    def publish_epoch(self, current, installed, before_pin, after, after_pin, preservation, lease):
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


class BinarySuccessor(Transition):
    """The single reviewed v2-configuration to TV-parent-binary transition."""
    def reviewed(self, historical=False):
        v = self.value
        self.previous_epoch = self.r.validate_epoch(self.s.descriptor(v["previousEpoch"]))
        self.need(self.previous_epoch["version"] == 2 and v["helpers"] == self.previous_epoch["helpers"], "successor_parent_or_helpers_changed")
        self.r.resolve_epoch_lineage(self.previous_epoch, self.s.descriptor)
        self.seed = self.s.descriptor(self.r.SEED)
        self.previous_binding = self.r.validate_seed_runtime_binding(self.s.descriptor(v["previousBinding"]), v["previousEpoch"], self.previous_epoch, self.seed)
        self.old = deepcopy(self.previous_epoch["candidate"])
        self.old_binary_sha = self.r.CURRENT_BINARY
        self.need(self.old["binary"]["sha256"] == self.old_binary_sha, "successor_current_binary_changed")
        self.reviewed_state = self.s.descriptor(v["reviewedState"])
        now = datetime.fromisoformat(self.reviewed_state["capturedAt"].replace("Z", "+00:00")) if historical else None
        facts = self.r.validate_successor_review(self.s.descriptor(v["reviewedSummary"]), self.reviewed_state,
            self.s.descriptor(v["priorCloseout"]), self.s.descriptor(v["priorSource"]), now=now)
        self.sessions = facts["revokedSessions"]
        before = self.reviewed_state
        expected = {**self.previous_epoch["candidateProcess"], "listener": {"host": "127.0.0.1", "port": self.old["listener"]["port"], "socketInode": self.old["listener"]["socketInode"]}}
        self.need(before["candidateBefore"] == before["candidateAfter"] == expected and before["postgresBefore"] == before["postgresAfter"] == self.previous_epoch["postgresProcess"] and
                   before["leaseBefore"] == before["leaseAfter"] == self.previous_epoch["lease"], "successor_review_runtime_changed")
        admission = self.s.descriptor(self.r.ADMISSION04)
        self.need(admission["status"] == "admitted_for_core_client" and admission["runtimeEpoch"] == self.value["previousEpoch"] and
                   admission["seedRuntimeBinding"] == self.value["previousBinding"] and admission["evidence"]["inactive-cancelled"] == self.r.INACTIVE_STAGE, "successor_inactive_admission_binding")
        inactive = self.s.descriptor(self.r.INACTIVE_STAGE)
        self.need(self.r.canonical(before["inactiveStage"]["tables"]) == self.r.canonical(inactive["tables"]) and
                   self.r.canonical(before["inactiveStage"]["sequences"]) == self.r.canonical(inactive["sequences"]), "successor_review_inactive_stage_changed")
        return facts

    def products(self):
        sources, frontend = self.product_files(self.r.NEW_F, self.r.NEW_ARCHIVE, self.r.validate_successor_full_report)
        self.reviewed()
        self.need(self.value["frontendReport"] == self.old["frontendReport"], "successor_frontend_receipt_changed")
        previous_sources = self.s.descriptor(self.previous_epoch["currentSource"]["sourceManifest"])
        unchanged = {name for name in set(sources) | set(previous_sources) if name.startswith("web/admin/") or name.startswith("internal/database/migrations/") or name == self.r.CATALOG_RELATIVE}
        self.need(sum(name.startswith("web/admin/") for name in unchanged) == 64 and all(name in sources and name in previous_sources and sources[name] == previous_sources[name] for name in unchanged), "successor_schema_or_frontend_changed")
        for name, row in frontend["assets"].items():
            self.need(not Path(name).is_absolute() and ".." not in Path(name).parts, "frontend_asset_path_invalid")
            self.s.read_checked(self.r.C / "install/admin" / name, row["sha256"])
        return self.reviewed_state["source"]

    def check_captured(self):
        facts = self.reviewed(historical=True)
        return {"status": "captured_contract_checked_not_product_admitted", "state": facts, "newSqlQueries": 0,
                "httpRequests": 0, "businessWrites": 0, "newProductGateEvaluated": False, "freshRuntimeChecked": False}

    def open(self):
        value = {"output": str(self.output), "budgets": {"maximumSeconds": 900, "cleanupSeconds": 60, "maximumRequests": 10, "cleanupRequests": 1}}
        self.io = self.r.EpochIO(value, self.input_pin, self.source_pin, self.value["previousEpoch"], self.value["previousBinding"], self.modules)
        try:
            self.io.open()
        finally:
            self.created = self.io.created
        self.p, self.candidate = self.io.provision, self.io.candidate
        self.epoch = {"candidateProcess": deepcopy(self.previous_epoch["candidateProcess"])}
        self.pin()

    def hosting(self):
        expected = self.reviewed_state["hostingBefore"]
        fields = list(expected["unit"])
        result = subprocess.run(["/usr/bin/systemctl", "show", expected["unit"]["Id"], "--property=" + ",".join(fields)], capture_output=True, timeout=10, check=True)
        unit = dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)
        self.need(unit == expected["unit"] and self.g.metadata(expected["process"]["pid"]) == expected["process"], "successor_hosting_changed")
        self.g.verify_listener(expected["process"]["pid"], expected["listener"])
        return deepcopy(expected)

    def capture(self, label):
        reference = self.before if label == "after" else self.reviewed_state
        candidate_before = self.pin()
        postgres_before = self.g.metadata(self.old["postgresIdentity"]["pid"])
        lease_before, host_before = self.lease(), self.hosting()
        source = self.sql_json(self.candidate["database"], self.a.snapshot_sql(self.catalog))
        inactive = self.sql_json(self.candidate["recoveryDatabase"], self.a.snapshot_sql(self.catalog))
        databases = {slot: self.p.database_facts(self.candidate["processes"]["postgres"], self.candidate["database" if slot == "source" else "recoveryDatabase"]) for slot in ("source", "recovery")}
        trees, documents = {}, {}
        for root in self.r.TREE_ROOTS:
            trees[root], found = self.tree(root)
            documents.update(found)
        self.save(label + "-control-documents.json", documents)
        self.save(label + "-tree-facts.json", trees)
        fixed = {pin["path"]: self.file(pin["path"])[0] for pin in [self.r.PROVISION, self.candidate["binary"], self.candidate["runtime"], *self.candidate["units"].values()]}
        root = self.r.C / "data"
        children = list(root.iterdir())
        self.need(len(children) <= 64 and all(not path.is_symlink() for path in children) and
                   {path.name for path in children if path.is_dir()} == {"recovery", "operations", "backups", "cache", "media", "diagnostics"}, "uncovered_data_root_directory")
        info = self.s.safe_path(root, (0, self.s.pwd.getpwnam("goby").pw_uid), True)
        fixed[str(root)] = {"directory": True, "entries": sorted(path.name for path in children), "dev": info.st_dev, "ino": info.st_ino, "uid": info.st_uid,
                            "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode), "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}
        for path in children:
            if not path.is_dir():
                fixed[str(path)] = self.file(path)[0]
        fixed.setdefault(str(root / "master.key"), {"absent": True})
        diagnostics = self.diagnostics(reference["diagnostics"])
        logs = {name: self.file(self.r.C / "private" / name, facts["bytes"])[0] for name, facts in reference["unitLogs"].items()}
        result = {"source": source, "inactiveStage": inactive, "databases": databases, "trees": trees, "controlDocuments": documents, "fixedFiles": fixed,
                  "diagnostics": diagnostics, "unitLogs": logs, "loadedUnits": self.loaded_units(), "protected": {unit: self.p.show(unit) for unit in self.r.PROTECTED},
                  "candidateBefore": candidate_before, "candidateAfter": self.pin(), "postgresBefore": postgres_before, "postgresAfter": self.g.metadata(self.old["postgresIdentity"]["pid"]),
                  "leaseBefore": lease_before, "leaseAfter": self.lease(), "hostingBefore": host_before, "hostingAfter": self.hosting(),
                  "previousEpoch": self.value["previousEpoch"], "seedBinding": self.value["previousBinding"], "priorSource": self.value["priorSource"], "admission04": self.r.ADMISSION04,
                  "postgresProcess": postgres_before, "capturedAt": datetime.now(timezone.utc).isoformat()}
        pin = self.save(label + ".json", result)
        self.r.validate_successor_state(result)
        return result, pin

    def check_fresh_before(self, reviewed_source):
        self.r.compare_successor_preservation(self.reviewed_state, self.before)
        self.need(self.r.canonical(self.before["source"]["tables"]) == self.r.canonical(reviewed_source["tables"]) and
                   self.r.canonical(self.before["source"]["sequences"]) == self.r.canonical(reviewed_source["sequences"]), "successor_fresh_source_changed")
        return self.reviewed_state["diagnostics"]

    def adopt_runtime_context(self, candidate, process):
        candidate["productInput"] = self.input_pin
        # This is a private, unversioned pin context, not a rewritten historical epoch.
        context = {"candidateProcess": deepcopy(process), "postgresProcess": deepcopy(self.previous_epoch["postgresProcess"])}
        self.candidate = self.io.candidate = candidate
        self.epoch = deepcopy(context)
        self.io.epoch = deepcopy(context)
        self.io.reader.epoch = deepcopy(context)
        self.io.reader.manifest = self.io.reader.candidate = candidate

    def preserve(self, after, installed):
        return self.r.compare_successor_preservation(self.before, after, installed_binary=installed)

    def publish_epoch(self, current, installed, before_pin, after, after_pin, preservation, lease):
        for slot in ("source", "recovery"):
            self.need(after["databases"][slot] == self.before["databases"][slot] == self.reviewed_state["databases"][slot], "successor_database_facts_drift")
            current["databases"][slot]["afterStart"] = deepcopy(after["databases"][slot])
        proof = {"before": before_pin, "after": after_pin, "reviewedState": self.value["reviewedState"], "installedBinary": installed, **preservation}
        epoch = {"kind": "audited-candidate-runtime-epoch", "version": 3, "status": "running_awaiting_live_acceptance", "operationKind": "binary_successor",
                 "transitionInput": self.input_pin, "productInput": self.input_pin, "configurationInput": self.previous_epoch["transitionInput"], "previousEpoch": self.value["previousEpoch"],
                 "reviewedState": self.value["reviewedState"], "reviewedSummary": self.value["reviewedSummary"], "transitionHelper": self.source_pin, "runtimeHelper": self.runtime_pin,
                 "originalProvision": self.r.PROVISION, "seedProvenance": self.r.SEED, "currentSource": {"archiveSha256": self.r.NEW_ARCHIVE, "sourceManifest": self.value["newSourceManifest"],
                 "binary": installed, "fullReport": self.value["newFullReport"], "schema": 28}, "candidate": current, "candidateProcess": self.epoch["candidateProcess"],
                 "postgresProcess": after["postgresAfter"], "lease": lease, "before": before_pin, "after": after_pin, "preservation": self.save("preservation.json", proof),
                 "calls": self.calls, "helpers": self.value["helpers"], "candidateAdmissionComplete": False}
        self.r.validate_epoch(epoch)
        epoch_pin = self.save("runtime-epoch.json", epoch)
        binding = {"kind": "audited-candidate-seed-runtime-binding", "version": 3, "runtimeEpoch": epoch_pin, "originalSeed": self.r.SEED, "seedExecutor": self.seed["helper"],
                   "seedInput": self.seed["input"], "seedSessionAddendum": self.r.SEED_ADDENDUM, "admission02": self.r.ADMISSION02,
                   **{key: self.seed[key] for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources")},
                   "seedCleanup": self.seed["cleanup"], "currentSessions": self.sessions, "previousBinding": self.value["previousBinding"], "reviewedState": self.value["reviewedState"],
                   "reviewedSummary": self.value["reviewedSummary"], "priorCloseout": self.value["priorCloseout"], "priorSource": self.value["priorSource"], "candidateAdmissionComplete": False}
        self.r.validate_seed_runtime_binding(binding, epoch_pin, epoch, self.seed)
        return {"status": "running_awaiting_live_acceptance", "runtimeEpoch": epoch_pin, "seedRuntimeBinding": self.save("seed-runtime-binding.json", binding), "candidateAdmissionComplete": False}


class ProgramsSuccessor(BinarySuccessor):
    """The reviewed Programs build on the existing, occupied candidate A."""
    def file(self, path, prefix=None):
        path = Path(path)
        if str(path) in self.r.PROGRAMS_STAT_ONLY:
            self.need(prefix is None, "programs_secret_prefix_forbidden")
            if not os.path.lexists(path):
                self.s.safe_path(path.parent, (0, self.s.pwd.getpwnam("goby").pw_uid), True)
                return {"access": "stat_only", "absent": True}, None
            info = self.s.safe_path(path, (0, self.s.pwd.getpwnam("goby").pw_uid))
            self.need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "programs_secret_stat_authority")
            return {"access": "stat_only", "dev": info.st_dev, "ino": info.st_ino, "uid": info.st_uid, "gid": info.st_gid,
                    "mode": stat.S_IMODE(info.st_mode), "bytes": info.st_size, "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}, None
        self.need(path.suffix.lower() != ".key", "programs_unreviewed_key_path")
        if path == self.r.C / "private/runtime.env":
            self.need(prefix is None, "programs_environment_prefix_forbidden")
            info = self.s.safe_path(path, (0, self.s.pwd.getpwnam("goby").pw_uid))
            self.need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= 1 << 20, "programs_environment_file_bound")
            digest, size = hashlib.sha256(), 0
            with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
                self.need(self.s.file_identity(os.fstat(stream.fileno())) == self.s.file_identity(info), "programs_environment_open_changed")
                while chunk := stream.read(65536):
                    size += len(chunk)
                    self.need(size <= 1 << 20, "programs_environment_read_bound")
                    digest.update(chunk)
                self.need(self.s.file_identity(os.fstat(stream.fileno())) == self.s.file_identity(info), "programs_environment_read_changed")
            self.need(self.s.file_identity(path.lstat()) == self.s.file_identity(info) and size == info.st_size and
                      digest.hexdigest() == self.r.PROGRAMS_ENV_SHA, "programs_environment_changed")
            return {"access": "hash_only", "dev": info.st_dev, "ino": info.st_ino, "uid": info.st_uid, "gid": info.st_gid,
                    "mode": stat.S_IMODE(info.st_mode), "bytes": size, "sha256": digest.hexdigest(),
                    "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}, None
        self.need(path.suffix.lower() != ".env", "programs_unreviewed_environment_path")
        return super().file(path, prefix)

    def reviewed(self, historical=False):
        value = self.value
        self.previous_epoch = self.r.validate_epoch(self.s.descriptor(value["previousEpoch"]))
        self.need(self.previous_epoch["version"] == 3 and value["helpers"] == self.previous_epoch["helpers"], "programs_parent_helpers")
        self.r.resolve_epoch_lineage(self.previous_epoch, self.s.descriptor)
        self.seed = self.s.descriptor(self.r.SEED)
        self.previous_binding = self.r.validate_seed_runtime_binding(self.s.descriptor(value["previousBinding"]),
            value["previousEpoch"], self.previous_epoch, self.seed)
        self.current_runtime = self.r.load_programs_previous_runtime(value["currentRuntime"], value, self.previous_epoch,
            self.s.descriptor, lambda pin: self.s.read_checked(pin["path"], pin["sha256"]))
        self.old = deepcopy(self.previous_epoch["candidate"])
        self.old_binary_sha = self.r.PROGRAMS_OLD_BINARY
        self.reviewed_state = self.s.descriptor(value["reviewedState"])
        self.reviewed_summary = self.s.descriptor(value["reviewedSummary"])
        self.retention_review = self.s.descriptor(self.reviewed_summary["retentionReview"])
        facts = self.r.validate_programs_startup_review(value, self.reviewed_summary, self.reviewed_state,
            self.s.descriptor(value["priorCloseout"]), self.s.descriptor(value["priorSource"]), self.current_runtime, self.retention_review)
        self.s.descriptor(self.reviewed_summary["boundary"])
        self.s.descriptor(self.reviewed_summary["independentReview"])
        prior_state = self.s.descriptor(self.previous_epoch["after"])
        self.need(self.r.canonical(self.reviewed_state["loadedUnits"]) == self.r.canonical(prior_state["loadedUnits"]) and
                  self.r.canonical(self.reviewed_state["controlDocuments"]) == self.r.canonical(prior_state["controlDocuments"]), "programs_configuration_or_control_lineage")
        self.r.compare_programs_logical(prior_state["inactiveStage"], self.reviewed_state["inactiveStage"], phase="unchanged")
        self.sessions = facts["revokedSessions"]
        self.runtime_replaced, self.new_lease, self.source_pins = False, None, {}
        return facts

    def products(self):
        self.need(not os.path.lexists(self.output), "transition_output_collision")
        product = self.r.load_programs_product(self.value, self.s.descriptor)
        self.binary, self.catalog = product["binaryBytes"], product["catalog"]
        self.a.validate_catalog(self.catalog, product["sourceManifest"], self.value["compiledCatalog"])
        self.reviewed()
        self.need(self.value["frontendReport"] == self.old["frontendReport"], "programs_frontend_receipt_changed")
        for name, row in product["frontend"]["assets"].items():
            self.need(not Path(name).is_absolute() and ".." not in Path(name).parts, "programs_frontend_path")
            self.s.read_checked(self.r.C / "install/admin" / name, row["sha256"])
        return self.reviewed_state["source"]

    def check_captured(self):
        facts = self.reviewed(historical=True)
        return {"status": "captured_contract_checked_not_product_admitted", "state": facts, "newSqlQueries": 0,
                "httpRequests": 0, "businessWrites": 0, "newProductGateEvaluated": False, "freshRuntimeChecked": False}

    def open(self):
        value = {"output": str(self.output), "budgets": {"maximumSeconds": 900, "cleanupSeconds": 60, "maximumRequests": 10, "cleanupRequests": 1}}
        self.io = self.r.EpochIO(value, self.input_pin, self.source_pin, self.value["previousEpoch"], self.value["previousBinding"], self.modules,
            current_runtime=self.current_runtime, current_runtime_pin=self.value["currentRuntime"])
        try:
            self.io.open()
        finally:
            self.created = self.io.created
        self.p, self.candidate = self.io.provision, self.io.candidate
        self.epoch = {"candidateProcess": deepcopy(self.current_runtime["current"]["candidateProcess"]),
                      "postgresProcess": deepcopy(self.current_runtime["current"]["postgresProcess"])}
        self.pin()

    def lease(self):
        lease = self.r.EpochReader.deployment_lease(self.io.reader)
        expected = self.new_lease if self.runtime_replaced else self.current_runtime["current"]["lease"]
        if expected is not None:
            self.need(self.r.canonical(lease) == self.r.canonical(expected), "programs_lease_changed")
        else:
            self.new_lease = deepcopy(lease)
            self.io.reader.current["lease"] = deepcopy(lease)
        return lease

    def assert_stopped(self, expected=None):
        unit = self.p.show(self.p.units["server"])
        self.need(unit["Id"] == self.p.units["server"] and unit["LoadState"] == "loaded" and unit["MainPID"] == "0" and unit["ActiveState"] in ("inactive", "failed") and
                  (expected is None or self.r.canonical(unit) == self.r.canonical(expected)), "programs_recovery_server_not_stopped")
        identities = [self.current_runtime["current"]["candidateProcess"]]
        if getattr(self, "failed_process", None) is not None:
            identities.append(self.failed_process)
        for process in identities:
            self.need(not Path("/proc", str(process["pid"])).exists(), "programs_recovery_old_process_present")
        group = Path("/sys/fs/cgroup") / self.old["processes"]["server"]["ControlGroup"].lstrip("/")
        self.need(not group.exists() or all(not path.read_text().strip() for path in [group / "cgroup.procs", *group.rglob("cgroup.procs")] if path.exists()),
                  "programs_recovery_recursive_cgroup_live")
        for name in ("tcp", "tcp6"):
            rows = [line.split() for line in Path("/proc/net", name).read_text().splitlines()[1:]]
            self.need(not any(row[3] == "0A" and row[1].rsplit(":", 1)[-1] == "%04X" % self.candidate["ports"]["http"] for row in rows), "programs_recovery_listener_present")
        key = 4919415424202458201
        count = self.sql_json(self.candidate["database"], "SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND classid=" + str(key >> 32) +
            "::oid AND objid=" + str(key & 0xffffffff) + "::oid AND objsubid=1 AND database=(SELECT oid FROM pg_database WHERE datname='" + self.candidate["database"] + "')")
        self.need(type(count) is int and count == 0 and self.g.metadata(self.old["postgresIdentity"]["pid"]) == self.current_runtime["current"]["postgresProcess"],
                  "programs_recovery_lease_or_postgres_changed")
        return unit

    def capture(self, label, *, stopped=False):
        reference = self.before if hasattr(self, "before") else self.reviewed_state
        if stopped:
            self.assert_stopped()
        candidate_before = None if stopped else self.pin()
        postgres_before = self.g.metadata(self.current_runtime["current"]["postgresProcess"]["pid"])
        lease_before, host_before = None if stopped else self.lease(), self.hosting()
        source = self.sql_json(self.candidate["database"], self.a.snapshot_sql(self.catalog))
        inactive = self.sql_json(self.candidate["recoveryDatabase"], self.a.snapshot_sql(self.catalog))
        databases = {slot: self.p.database_facts(self.candidate["processes"]["postgres"], self.candidate["database" if slot == "source" else "recoveryDatabase"]) for slot in ("source", "recovery")}
        trees, documents = {}, {}
        for root in self.r.TREE_ROOTS:
            trees[root], found = self.tree(root)
            documents.update(found)
        self.save(label + "-control-documents.json", documents)
        self.save(label + "-tree-facts.json", trees)
        fixed = {pin["path"]: self.file(pin["path"])[0] for pin in [self.r.PROVISION, self.candidate["binary"], self.candidate["runtime"], *self.candidate["units"].values()]}
        root = self.r.C / "data"
        children = list(root.iterdir())
        self.need(len(children) <= 64 and all(not path.is_symlink() for path in children) and
                  {path.name for path in children if path.is_dir()} == {"recovery", "operations", "backups", "cache", "media", "diagnostics"}, "programs_data_root_scope")
        info = self.s.safe_path(root, (0, self.s.pwd.getpwnam("goby").pw_uid), True)
        fixed[str(root)] = {"directory": True, "entries": sorted(path.name for path in children), "dev": info.st_dev, "ino": info.st_ino,
            "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode), "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}
        for path in children:
            if not path.is_dir():
                fixed[str(path)] = self.file(path)[0]
        for path in self.r.PROGRAMS_STAT_ONLY:
            fixed[path] = self.file(path)[0]
        diagnostics = self.diagnostics(reference["diagnostics"])
        logs = {name: self.file(self.r.C / "private" / name, facts["bytes"])[0] for name, facts in reference["unitLogs"].items()}
        result = {"source": source, "inactiveStage": inactive, "databases": databases, "trees": trees, "controlDocuments": documents,
            "fixedFiles": fixed, "diagnostics": diagnostics, "unitLogs": logs, "loadedUnits": self.loaded_units(),
            "protected": {unit: self.p.show(unit) for unit in reference["protected"]}, "candidateBefore": candidate_before,
            "candidateAfter": None if stopped else self.pin(), "postgresBefore": postgres_before, "postgresAfter": self.g.metadata(postgres_before["pid"]),
            "leaseBefore": lease_before, "leaseAfter": None if stopped else self.lease(), "hostingBefore": host_before, "hostingAfter": self.hosting(),
            "previousEpoch": self.value["previousEpoch"], "seedBinding": self.value["previousBinding"], "priorSource": self.value["priorSource"],
            "currentRuntime": self.value["currentRuntime"], "postgresProcess": postgres_before,
            "capturedAt": datetime.now(timezone.utc).isoformat(), "databaseNow": source["capturedAt"]}
        if stopped:
            self.assert_stopped()
        self.source_pins[label] = self.save("source-" + label + ".json", source)
        result_pin = self.save(label + ".json", result)
        self.last_capture = {"label": label, "state": result_pin, "value": result}
        self.r.validate_programs_state(result)
        self.r.validate_programs_retention(self.retention_review, result, seconds=1800)
        return result, result_pin

    def check_fresh_before(self, reviewed_source):
        self.r.compare_programs_preservation(self.reviewed_state, self.before)
        self.r.compare_programs_logical(reviewed_source, self.before["source"], phase="unchanged")
        return self.reviewed_state["diagnostics"]

    def adopt_runtime_context(self, candidate, process):
        candidate["productInput"] = self.input_pin
        self.candidate = self.io.candidate = candidate
        self.epoch = {"candidateProcess": deepcopy(process), "postgresProcess": deepcopy(self.current_runtime["current"]["postgresProcess"])}
        self.io.reader.manifest = self.io.reader.candidate = candidate
        self.io.reader.current["candidateProcess"] = deepcopy(process)
        self.io.reader.current["serverProperties"] = deepcopy(candidate["processes"]["server"])
        self.io.reader.current["serverIdentity"] = deepcopy(candidate["serverIdentity"])
        self.runtime_replaced, self.new_lease = True, None

    def preserve(self, after, installed):
        return self.r.compare_programs_preservation(self.before, after, installed_binary=installed)

    def capture_failure_evidence(self):
        """Retain one complete failed state without authorizing a recovery."""
        last = getattr(self, "last_capture", None)
        if last is not None and last["label"] == "after":
            return {"status": "existing_after_state_retained", "state": last["state"],
                    "serverProperties": deepcopy(self.candidate["processes"]["server"]),
                    "serverIdentity": deepcopy(self.candidate["serverIdentity"])}
        if self.stage not in ("replace", "start", "readiness") or not hasattr(self, "before_pin") or not hasattr(self, "p"):
            return {"status": "not_captured", "reason": "no_complete_after_state_or_eligible_failure_phase"}
        if not all(row.get("processGroupClosed") is True and row.get("outcome") == "acknowledged" for row in self.p.command_responsibilities):
            return {"status": "not_captured", "reason": "command_outcome_unknown"}
        if self.remaining() < 60:
            return {"status": "not_captured", "reason": "insufficient_original_transition_budget"}
        signal.setitimer(signal.ITIMER_REAL, 60)
        try:
            before_unit = self.p.show(self.p.units["server"])
            stopped = before_unit["MainPID"] == "0"
            state, pin = self.capture("failed", stopped=stopped)
            self.need(self.p.show(self.p.units["server"]) == before_unit, "programs_failure_capture_unit_changed")
            return {"status": "captured_awaiting_independent_review", "state": pin, "serverProperties": before_unit,
                    "serverIdentity": None if stopped else deepcopy(self.candidate["serverIdentity"])}
        finally:
            signal.setitimer(signal.ITIMER_REAL, self.remaining())

    def publish_epoch(self, current, installed, before_pin, after, after_pin, preservation, lease):
        for slot in ("source", "recovery"):
            self.need(after["databases"][slot] == self.before["databases"][slot] == self.reviewed_state["databases"][slot], "programs_database_facts_drift")
            current["databases"][slot]["afterStart"] = deepcopy(after["databases"][slot])
        proof = {"before": before_pin, "after": after_pin, "sourceBefore": self.source_pins["before"], "sourceAfter": self.source_pins["after"],
                 "reviewedState": self.value["reviewedState"], "installedBinary": installed, **preservation}
        source = {"archiveSha256": self.value["newSourceArchive"]["sha256"], "sourceManifest": self.value["newSourceManifest"],
                  "binary": installed, "fullReport": self.value["newFullReport"], "schema": 28,
                  "artifactReceipt": self.value["newArtifactReceipt"], "buildManifest": self.value["newBuildManifest"], "sourceBridge": self.value["sourceBridge"]}
        epoch = {"kind": "audited-candidate-runtime-epoch", "version": 4, "status": "running_awaiting_live_acceptance", "operationKind": "programs_successor",
            "transitionInput": self.input_pin, "productInput": self.input_pin, "configurationInput": self.previous_epoch["configurationInput"],
            "previousEpoch": self.value["previousEpoch"], "previousCurrentRuntime": self.value["currentRuntime"],
            "reviewedState": self.value["reviewedState"], "reviewedSummary": self.value["reviewedSummary"], "transitionHelper": self.source_pin,
            "runtimeHelper": self.runtime_pin, "originalProvision": self.r.PROVISION, "seedProvenance": self.r.SEED, "currentSource": source,
            "candidate": current, "candidateProcess": self.epoch["candidateProcess"], "postgresProcess": after["postgresAfter"], "lease": lease,
            "before": before_pin, "after": after_pin, "sourceBefore": self.source_pins["before"], "sourceAfter": self.source_pins["after"],
            "preservation": self.save("preservation.json", proof), "calls": self.calls, "helpers": self.value["helpers"], "candidateAdmissionComplete": False}
        self.r.validate_epoch(epoch)
        self.r.validate_programs_runtime_change(self.current_runtime, epoch, self.before, after)
        epoch_pin = self.save("runtime-epoch.json", epoch)
        binding = {"kind": "audited-candidate-seed-runtime-binding", "version": 4, "runtimeEpoch": epoch_pin, "originalSeed": self.r.SEED,
            "seedExecutor": self.seed["helper"], "seedInput": self.seed["input"], "seedSessionAddendum": self.r.SEED_ADDENDUM, "admission02": self.r.ADMISSION02,
            **{key: self.seed[key] for key in ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources")},
            "seedCleanup": self.seed["cleanup"], "currentSessions": self.sessions, "previousBinding": self.value["previousBinding"],
            "previousCurrentRuntime": self.value["currentRuntime"], "reviewedState": self.value["reviewedState"], "reviewedSummary": self.value["reviewedSummary"],
            "priorCloseout": self.value["priorCloseout"], "priorSource": self.value["priorSource"], "candidateAdmissionComplete": False}
        self.r.validate_seed_runtime_binding(binding, epoch_pin, epoch, self.seed)
        return {"status": "running_awaiting_live_acceptance", "runtimeEpoch": epoch_pin,
                "seedRuntimeBinding": self.save("seed-runtime-binding.json", binding), "candidateAdmissionComplete": False}


class ProgramsRecovery(ProgramsSuccessor):
    """Explicitly restore this failed attempt's predecessor once; never retry it."""
    def __init__(self, runtime, value, input_pin, source_pin, runtime_pin):
        runtime.need(isinstance(value, dict) and set(value) == runtime.PROGRAMS_RECOVERY_INPUT_KEYS, "programs_recovery_input_schema")
        transition = programs_input = json.loads(runtime.read_bootstrap(value["transitionInput"]))
        runtime.validate_programs_recovery_input(value, transition)
        self.initialize(runtime, programs_input, input_pin, source_pin, runtime_pin)
        self.recovery_value, self.transition_input_pin = value, value["transitionInput"]
        self.output, self.private = Path(value["output"]), Path(value["output"]) / "private"
        self.need(self.s.descriptor(input_pin) == value and self.s.descriptor(self.transition_input_pin) == transition, "programs_recovery_input_changed")

    def recovery_saved(self):
        self.reviewed(historical=True)
        self.failure = self.s.descriptor(self.recovery_value["transitionFailure"])
        self.recovery_review = self.s.descriptor(self.recovery_value["recoveryReview"])
        self.entry_before = self.s.descriptor(self.recovery_review["before"])
        self.failed_state = self.s.descriptor(self.recovery_review["failedState"])
        self.added_definition = self.r.validate_programs_recovery_review(self.recovery_value, self.value, self.recovery_review,
            self.failure, self.entry_before, self.failed_state, self.current_runtime)
        self.r.compare_programs_preservation(self.reviewed_state, self.entry_before)
        product = self.r.load_programs_product(self.value, self.s.descriptor)
        self.catalog = product["catalog"]
        self.old_bytes = self.r.read_programs_bytes(self.recovery_review["oldBinaryCopy"])
        self.need(self.r.digest(self.old_bytes) == self.r.PROGRAMS_OLD_BINARY, "programs_recovery_old_copy_changed")
        process = self.failed_state["candidateAfter"]
        self.failed_process = None if process is None else {key: val for key, val in process.items() if key != "listener"}
        return {"mode": self.recovery_review["mode"], "refreshDefinition": self.added_definition}

    def check_captured(self):
        result = self.recovery_saved()
        return {"status": "captured_recovery_contract_checked_not_executed", **result,
                "httpRequests": 0, "newSqlQueries": 0, "serviceActions": 0, "businessWrites": 0, "candidateAdmissionComplete": False}

    def recovery_context(self):
        self.candidate = deepcopy(self.old)
        self.candidate["binary"] = {"path": str(self.r.C / "install/goby"),
            "sha256": self.failed_state["fixedFiles"][str(self.r.C / "install/goby")]["sha256"]}
        self.candidate["processes"]["server"] = deepcopy(self.recovery_review["serverProperties"])
        if self.failed_process is not None:
            self.candidate["serverIdentity"] = deepcopy(self.recovery_review["serverIdentity"])
            self.candidate["listener"].update(pid=self.failed_process["pid"], socketInode=self.failed_state["candidateAfter"]["listener"]["socketInode"])
        self.p = self.pmod.Provision({"runId": self.candidate["runId"], "ports": self.candidate["ports"]}, self.input_pin, self.value["helpers"]["provision"])
        self.p.private = self.private
        self.p.argv = {"server": [str(self.r.C / "install/goby")], "postgres": ["/usr/lib/postgresql/17/bin/postgres", "-D", str(self.r.C / "postgres/data"),
            "-c", "config_file=" + str(self.r.C / "postgres/server.conf")]}
        self.p.pg_identity, self.p.pg_version = self.candidate["postgresIdentity"], self.candidate["postgresVersionNum"]
        # The historical constructor would require the predecessor executable
        # still installed. This operation-local reader instead pins the fully
        # reviewed failed state and never rewrites either historical envelope.
        reader = self.r.RecoveredEpochReader.__new__(self.r.RecoveredEpochReader)
        reader.productEpoch = reader.epoch = self.previous_epoch
        reader.currentRuntime, reader.current = deepcopy(self.current_runtime), deepcopy(self.current_runtime["current"])
        if self.failed_process is not None:
            reader.current.update(candidateProcess=deepcopy(self.failed_process), serverProperties=deepcopy(self.recovery_review["serverProperties"]),
                serverIdentity=deepcopy(self.recovery_review["serverIdentity"]), listener=deepcopy(self.candidate["listener"]), lease=deepcopy(self.failed_state["leaseAfter"]))
        reader.modules, reader.provision, reader.observationOnly = self.modules, self.p, False
        reader.manifest = reader.candidate = self.candidate
        self.io = types.SimpleNamespace(reader=reader, candidate=self.candidate, provision=self.p, pin=reader.pin)
        self.epoch = {"candidateProcess": deepcopy(reader.current["candidateProcess"]), "postgresProcess": deepcopy(reader.current["postgresProcess"])}
        self.runtime_replaced = self.failed_process is not None
        self.new_lease = deepcopy(self.failed_state["leaseAfter"])

    def claim_recovery(self):
        claim = Path(self.value["output"]) / "private/recovery-claim.json"
        self.need(not os.path.lexists(claim), "programs_recovery_already_consumed")
        return self.s.write_json_once(claim, {"kind": "audited-programs-recovery-claim", "version": 1,
            "input": self.input_pin, "source": self.source_pin, "transitionInput": self.transition_input_pin,
            "transitionFailure": self.recovery_value["transitionFailure"], "mode": self.recovery_review["mode"], "automatic": False})

    def run(self):
        self.recovery_saved()
        self.need(not os.path.lexists(self.output) and not os.path.lexists(Path(self.value["output"]) / "private/recovery-claim.json"), "programs_recovery_scope_consumed")
        self.s.write_json_once(self.output.with_name(self.output.name + "-intent.json"), {"input": self.input_pin, "source": self.source_pin,
            "operation": "explicit-programs-predecessor-recovery", "transitionFailure": self.recovery_value["transitionFailure"]})
        os.mkdir(self.output, 0o700)
        self.created = True
        os.mkdir(self.private, 0o700)
        self.s.sync_dir(self.output)
        self.s.sync_dir(self.output.parent)
        self.recovery_context()
        stopped = self.failed_process is None
        self.stage = "recovery_fresh_before"
        self.before = self.failed_state
        if stopped:
            self.assert_stopped(self.recovery_review["serverProperties"])
        else:
            self.pin()
        fresh, before_pin = self.capture("recovery-before", stopped=stopped)
        self.r.compare_programs_preservation(self.failed_state, fresh)
        self.r.validate_programs_retention(self.retention_review, fresh, seconds=self.r.PROGRAMS_RECOVERY_POLICY["maximumSeconds"])
        self.before = fresh
        claim = self.claim_recovery()
        if not stopped:
            self.stage = "recovery_stop_successor"
            self.pin()
            self.save("recovery-stop-intent.json", {"unit": self.p.units["server"], "process": self.failed_process, "claim": claim})
            self.calls["stop"] += 1
            signal.setitimer(signal.ITIMER_REAL, min(self.remaining(), self.r.PROGRAMS_RECOVERY_POLICY["stopSeconds"]))
            try:
                self.p.command("programs-recovery-stop", ["/usr/bin/systemctl", "stop", self.p.units["server"]])
                self.assert_stopped()
            finally:
                signal.setitimer(signal.ITIMER_REAL, self.remaining())
            stopped_state, stopped_pin = self.capture("recovery-stopped", stopped=True)
            self.r.compare_programs_failed_state(fresh, stopped_state, self.value, allow_startup=False)
            self.save("recovery-stopped-state.json", {"state": stopped_pin, "sourceUnchanged": True})
        else:
            stopped_state = fresh
        if self.recovery_review["mode"] == "restore_original":
            self.stage = "recovery_replace_original"
            staged = self.r.C / "install" / (".goby-recover-" + self.output.name)
            self.need(not os.path.lexists(staged), "programs_recovery_staging_collision")
            old_facts = self.entry_before["fixedFiles"][str(self.r.C / "install/goby")]
            self.save("recovery-stage-intent.json", {"path": str(staged), "oldBinaryCopy": self.recovery_review["oldBinaryCopy"], "claim": claim})
            self.p.write(staged, self.old_bytes, mode=old_facts["mode"])
            staged_facts, unused = self.file(staged)
            self.need(staged_facts["sha256"] == self.r.PROGRAMS_OLD_BINARY and
                      all(staged_facts[key] == old_facts[key] for key in ("dev", "uid", "gid", "mode")), "programs_recovery_staging_authority")
            self.assert_stopped()
            self.need(self.file(self.r.C / "install/goby")[0] == stopped_state["fixedFiles"][str(self.r.C / "install/goby")] and
                      self.file(staged)[0] == staged_facts, "programs_recovery_binary_changed_before_replace")
            self.save("recovery-replace-intent.json", {"destination": str(self.r.C / "install/goby"), "staged": str(staged), "claim": claim})
            self.calls["replace"] += 1
            os.replace(staged, self.r.C / "install/goby")
            self.s.sync_dir(self.r.C / "install")
        original = {"path": str(self.r.C / "install/goby"), "sha256": self.r.PROGRAMS_OLD_BINARY}
        self.s.read_checked(original["path"], original["sha256"])
        self.assert_stopped()
        self.stage = "recovery_start_original"
        self.save("recovery-start-intent.json", {"unit": self.p.units["server"], "binary": original, "claim": claim})
        self.calls["start"] += 1
        server = self.p.start("server")
        candidate = deepcopy(self.old)
        candidate["binary"], candidate["processes"]["server"] = original, server
        candidate["serverIdentity"] = self.p.process("server", server)
        self.adopt_runtime_context(candidate, self.g.metadata(candidate["serverIdentity"]["pid"]))
        self.stage = "recovery_readiness"
        signal.setitimer(signal.ITIMER_REAL, min(self.remaining(), self.r.PROGRAMS_RECOVERY_POLICY["readySeconds"]))
        try:
            deadline = time.monotonic() + self.r.PROGRAMS_RECOVERY_POLICY["readySeconds"]
            while time.monotonic() < deadline:
                listener = self.p.listener(server, candidate["serverIdentity"], allow_absent=True)
                if listener:
                    candidate["listener"] = listener
                    if self.public_read("/readyz") == (200, {"Status": "ready"}):
                        break
                time.sleep(1)
            else:
                raise self.r.ContractError("programs_recovery_readiness_expired")
        finally:
            signal.setitimer(signal.ITIMER_REAL, self.remaining())
        self.need(self.public_read("/healthz") == (200, {"Status": "ok"}), "programs_recovery_health_failed")
        status, public = self.public_read("/emby/System/Info/Public")
        self.need(status == 200 and public["Id"] == self.seed["serverId"] and public["StartupWizardCompleted"] is True and
                  public["LocalAddress"] == self.old["publicUrl"], "programs_recovery_public_identity_changed")
        after, after_pin = self.capture("recovery-after")
        compared = self.r.compare_programs_preservation(fresh, after, installed_binary=original, recovery=True,
            added_definition=self.added_definition, entry_source=self.entry_before["source"])
        context = {"candidate": candidate, "candidateProcess": self.epoch["candidateProcess"], "postgresProcess": after["postgresAfter"], "lease": after["leaseAfter"]}
        self.r.validate_programs_runtime_change(self.current_runtime, context, fresh, after)
        residual = []
        staged_successor = self.r.C / "install" / (".goby-next-" + Path(self.value["output"]).name)
        if os.path.lexists(staged_successor):
            facts, unused = self.file(staged_successor)
            self.need(facts["sha256"] == self.value["newBinary"]["sha256"], "programs_recovery_residual_staging_changed")
            residual.append({"path": str(staged_successor), "facts": facts, "responsibility": "retained_verified_successor_staging"})
        proof = self.save("recovery-preservation.json", {"entryBefore": self.recovery_review["before"], "failedState": self.recovery_review["failedState"],
            "before": before_pin, "after": after_pin, "sourceBefore": self.source_pins["recovery-before"], "sourceAfter": self.source_pins["recovery-after"], **compared})
        expected_calls = {"stop": 0 if stopped else 1, "replace": 1 if self.recovery_review["mode"] == "restore_original" else 0, "start": 1}
        self.need(self.calls == expected_calls and all(row.get("processGroupClosed") is True and row.get("outcome") == "acknowledged"
            for row in self.p.command_responsibilities), "programs_recovery_command_closure")
        self.pin()
        self.remaining()
        result = {"kind": "audited-candidate-programs-recovery-closeout", "version": 1, "status": "original_binary_restored_awaiting_review",
            "input": self.input_pin, "source": self.source_pin, "runtimeHelper": self.runtime_pin, "claim": claim,
            "transitionInput": self.transition_input_pin, "transitionFailure": self.recovery_value["transitionFailure"],
            "originalProductEpoch": self.value["previousEpoch"], "currentSource": self.previous_epoch["currentSource"], "binary": original,
            "before": before_pin, "after": after_pin, "sourceAfter": self.source_pins["recovery-after"], "preservation": proof,
            "candidateProcess": context["candidateProcess"], "serverProperties": candidate["processes"]["server"], "serverIdentity": candidate["serverIdentity"],
            "listener": candidate["listener"], "postgresProcess": context["postgresProcess"], "lease": context["lease"],
            "calls": self.calls, "publicRequests": self.public_count, "residualStaging": residual, "candidateAdmissionComplete": False, "clientAcceptance": False,
            "automaticRetry": False, "automaticRollback": False, "commandResponsibilities": self.p.command_responsibilities}
        return {**result, "receipt": self.save("recovery-result.json", result)}


def capture_programs_state(runtime, value, input_pin, source_pin, runtime_pin):
    runtime.validate_programs_capture_input(value)
    job = ProgramsSuccessor.__new__(ProgramsSuccessor)
    job.initialize(runtime, value, input_pin, source_pin, runtime_pin)
    runtime.need(job.s.descriptor(input_pin) == value, "programs_capture_input_changed")
    job.previous_epoch = runtime.validate_epoch(job.s.descriptor(value["previousEpoch"]))
    runtime.need(job.previous_epoch["version"] == 3 and value["helpers"] == job.previous_epoch["helpers"], "programs_capture_parent")
    lineage = runtime.resolve_epoch_lineage(job.previous_epoch, job.s.descriptor)
    job.seed = job.s.descriptor(runtime.SEED)
    job.previous_binding = runtime.validate_seed_runtime_binding(job.s.descriptor(value["previousBinding"]),
        value["previousEpoch"], job.previous_epoch, job.seed)
    job.current_runtime = runtime.load_programs_previous_runtime(value["currentRuntime"], value, job.previous_epoch,
        job.s.descriptor, lambda pin: job.s.read_checked(pin["path"], pin["sha256"]))
    job.old = deepcopy(job.previous_epoch["candidate"])
    job.old_binary_sha = runtime.PROGRAMS_OLD_BINARY
    job.catalog = job.s.descriptor(lineage["productInput"]["compiledCatalog"])
    job.retention_review = job.s.descriptor(value["retentionReview"])
    historical = job.s.descriptor(job.previous_epoch["after"])
    hosting = job.s.descriptor(job.current_runtime["hosting"])
    job.reviewed_state = {"diagnostics": historical["diagnostics"], "unitLogs": historical["unitLogs"],
        "protected": {unit: None for unit in runtime.PROGRAMS_PROTECTED},
        "hostingBefore": {"process": hosting["process"], "listener": hosting["listener"],
                          "unit": {key: hosting["unitProperties"][key] for key in runtime.CURRENT_HOST_UNIT_FIELDS}}}
    job.runtime_replaced, job.new_lease, job.source_pins = False, None, {}
    job.stage = "read_only_programs_state_capture"
    try:
        job.open()
        state, state_pin = job.capture("reviewed")
        runtime.compare_programs_logical(job.s.descriptor(value["priorSource"]), state["source"], phase="unchanged")
        runtime.compare_programs_logical(historical["inactiveStage"], state["inactiveStage"], phase="unchanged")
        runtime.need(runtime.canonical(state["loadedUnits"]) == runtime.canonical(historical["loadedUnits"]) and
                     runtime.canonical(state["controlDocuments"]) == runtime.canonical(historical["controlDocuments"]), "programs_capture_native_lineage")
        result = {"kind": "audited-programs-state-capture", "version": 1, "status": "captured_awaiting_independent_review",
            "input": input_pin, "source": source_pin, "runtimeHelper": runtime_pin, "state": state_pin,
            "sourceSnapshot": job.source_pins["reviewed"], "previousEpoch": value["previousEpoch"], "seedBinding": value["previousBinding"],
            "currentRuntime": value["currentRuntime"], "priorSource": value["priorSource"], "priorCloseout": value["priorCloseout"],
            "retentionReview": value["retentionReview"], "ownedTablesCompared": 35, "sequencesCompared": 5,
            "httpRequests": 0, "serviceActions": 0, "businessWrites": 0, "candidateAdmissionComplete": False,
            "commandResponsibilities": job.p.command_responsibilities}
        return {**result, "receipt": job.save("capture-result.json", result)}
    except Exception as error:
        result = {"status": "programs_state_capture_failed_resources_retained", "stage": job.stage,
            "code": getattr(error, "code", "programs_capture_failed"), "errorType": type(error).__name__,
            "httpRequests": 0, "serviceActions": 0, "businessWrites": 0, "automaticRetry": False}
        if job.created:
            result["receipt"] = job.save("capture-failure.json", result)
        return result


def capture_reviewed_state(runtime, helper_pins, output, input_pin, source_pin, runtime_pin, *, preparation=None):
    """Fresh read-only preparation, independent of any future full/build input."""
    if preparation is not None:
        runtime.validate_programs_capture_input(preparation)
        runtime.need(preparation["output"] == output and preparation["helpers"] == helper_pins, "programs_capture_arguments_changed")
        return capture_programs_state(runtime, preparation, input_pin, source_pin, runtime_pin)
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
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--check-captured", action="store_true")
    modes.add_argument("--capture-state", action="store_true")
    modes.add_argument("--recover-failed", action="store_true")
    args = parser.parse_args()
    runtime_pin = {"path": args.runtime_helper, "sha256": args.runtime_helper_sha256}
    r = load_runtime(runtime_pin)
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    r.read_bootstrap(source_pin)
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    value = json.loads(r.read_bootstrap(input_pin))
    if args.capture_state:
        r.validate_programs_capture_input(value)
        def capture_expired(unused_signal, unused_frame):
            raise r.ContractError("programs_capture_deadline")
        signal.signal(signal.SIGALRM, capture_expired)
        signal.setitimer(signal.ITIMER_REAL, r.LIMITS["maximumSeconds"])
        try:
            result = capture_reviewed_state(r, value["helpers"], value["output"], input_pin, source_pin, runtime_pin, preparation=value)
            print(json.dumps(result))
            return 0 if result["status"] == "captured_awaiting_independent_review" else 2
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
    recovery = value.get("kind") == "audited-candidate-programs-recovery-input"
    r.need(not args.recover_failed or recovery, "programs_recovery_flag_requires_recovery_input")
    r.need(not recovery or args.recover_failed or args.check_captured, "programs_recovery_requires_explicit_flag")
    job_type = (ProgramsRecovery if recovery else ProgramsSuccessor if type(value.get("version")) is int and value["version"] == 3 else
                BinarySuccessor if type(value.get("version")) is int and value["version"] == 2 else Transition)
    job = job_type(r, value, input_pin, source_pin, runtime_pin)
    if args.check_captured:
        try:
            print(json.dumps(job.check_captured()))
            return 0
        except Exception as error:
            print(json.dumps({"status": "captured_contract_rejected", "stage": "captured_replay", "code": getattr(error, "code", "captured_contract_invalid"), "errorType": type(error).__name__, "businessWrites": 0}))
            return 2
    # The pure-contract checkpoint must not dispatch before its explicit
    # failed-transition recovery entry has been implemented and verified.
    r.need(not isinstance(job, ProgramsSuccessor), "programs_execution_waits_for_recovery_admission")
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
        if isinstance(job, ProgramsRecovery):
            value.update(kind="audited-programs-recovery-failure", version=1, status="programs_recovery_failed_resources_retained",
                         input=input_pin, source=source_pin, runtimeHelper=runtime_pin, transitionInput=job.transition_input_pin,
                         transitionFailure=job.recovery_value["transitionFailure"])
        elif isinstance(job, ProgramsSuccessor):
            value.update(kind="audited-programs-transition-failure", version=1, input=input_pin, source=source_pin, runtimeHelper=runtime_pin,
                         before=getattr(job, "before_pin", None), oldBinaryCopy=getattr(job, "old_binary_copy", None))
            try:
                value["failureCapture"] = job.capture_failure_evidence()
            except Exception as capture_error:
                last = getattr(job, "last_capture", None)
                value["failureCapture"] = {"status": "incomplete_capture_retained", "state": last["state"] if last and last["label"] in ("after", "failed") else None,
                    "code": getattr(capture_error, "code", "programs_failure_capture_incomplete"), "errorType": type(capture_error).__name__}
        try:
            if job.created:
                value["receipt"] = job.save("recovery-failure.json" if isinstance(job, ProgramsRecovery) else "failure.json", value)
        except Exception as receipt_error:
            value["receiptErrorType"] = type(receipt_error).__name__
        print(json.dumps(value))
        return 2
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


if __name__ == "__main__":
    raise SystemExit(main())
