#!/usr/bin/env python3
"""Accept a prepared Goby binary and assets through real isolated recovery.

The frozen PostgreSQL pair operator supplies port-15432 ownership, the lock,
exact HBA replacement, protected-service identities, and database disposal.
This wrapper never builds, installs dependencies, or changes a shared service.
Run only through the authorized Linux test-env SSH operator. Browser and CLI
secrets stay in private files; the published report is an explicit projection.
"""

import argparse
import base64
import hashlib
import http.client
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tarfile
import time
import traceback
import urllib.error
import urllib.parse
import urllib.request


MARKER = "goby-backup-recovery-runtime-v1"
GATE_MARKER = "goby-backup-recovery-gate-v1"
SPEC = "backup-recovery-runtime.spec.ts"
NODE_MODULES = Path("/dev/shm/goby-admin-ui/node_modules")
BROWSER_CACHE = Path("/root/.cache/ms-playwright")
BROWSER_FILES = ("package.json", "playwright.config.ts", "tsconfig.json", "e2e/" + SPEC)
RUNTIME_ONLY_SOURCE_PATHS = frozenset(("scripts/test-env/verify-backup-recovery.py",
                                      "web/admin/e2e/backup-recovery-runtime.spec.ts"))
TABLES = tuple(sorted("""activity_entries application_key_clients application_key_devices
application_keys catalog_entities client_playback_references devices encoding_jobs
item_entities item_images item_metadata_state item_subtitles items libraries
library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings
sessions user_item_data users task_definitions task_occurrences task_run_children
task_run_requests task_runs task_triggers""".split()))
REQUIRED_PHASE_CHECKS = {
    "create": {"NativeBrowserPasswordLogin", "NativeCreatePollHEADAndAttachment",
               "NativeImportRemainsUnverified", "RealUIPolling", "DesktopAndMobileWithoutOverflow",
               "CredentialFreeURLsAndNoPageErrors", "DeterministicCreateGateAndRealUIPolling"},
    "restore": {"NativeBrowserPasswordLogin", "NativePlanInspectAndConfirmedApply",
                "OldNativeCookieRejected", "CompletedOperationAndRestoredPasswordLogin",
                "CredentialFreeURLsAndNoPageErrors"},
    "rollback": {"NativeBrowserPasswordLogin", "NativeConfirmedRollback", "OldNativeCookieRejected",
                 "CompletedOperationAndRestoredPasswordLogin", "CredentialFreeURLsAndNoPageErrors"},
}


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def digest_file(path):
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for data in iter(lambda: stream.read(1 << 20), b""):
            result.update(data)
    return result.hexdigest()


class SafeFailure(Exception):
    def __init__(self, code, message):
        super().__init__(message)
        self.code = code


def source_manifest(core, source, expected):
    core.require(source.parent == core.WORK and source.resolve() == source and
                 re.fullmatch(r"backuppg-source-attempt-[1-9][0-9]*", source.name), "The frozen source location changed.")
    raw = core.private_read(source / "source-inputs.json", maximum=4 << 20)
    core.require(hashlib.sha256(raw).hexdigest() == expected, "The frozen source manifest changed.")
    manifest = json.loads(raw)
    core.require(manifest.get("marker") == "goby-backuppg-source-m5j-v1", "The frozen source marker changed.")
    actual = {}
    for path in source.rglob("*"):
        core.require(not path.is_symlink(), "The frozen source contains a symbolic link.")
        if path.is_file() and path.name not in ("source-inputs.json", "OWNER.txt"):
            actual[str(path.relative_to(source))] = digest_file(path)
    core.require(actual == manifest.get("files"), "Frozen source membership or bytes changed.")
    return actual


def verify_inputs(core, fixture):
    files = source_manifest(core, Path(fixture["source"]), fixture["source_manifest_sha256"])
    binary_source = source_manifest(core, Path(fixture["binary_source"]), fixture["binary_source_manifest_sha256"])
    changes = {name: {"expectedHash": binary_source.get(name), "actualHash": files.get(name)}
               for name in sorted(set(binary_source) | set(files)) if binary_source.get(name) != files.get(name)}
    core.require(set(changes) <= RUNTIME_ONLY_SOURCE_PATHS and all(value["actualHash"] is not None for value in changes.values()),
                 "The runtime clone changed a frozen input outside the two explicit harness files.")
    for name, key in (("binary", "binary_sha256"), ("assets", "assets_sha256"),
                      ("core", "core_sha256"), ("wrapper", "wrapper_sha256")):
        core.require(digest_file(Path(fixture[name])) == fixture[key], "A frozen runtime artifact changed.")
    return files, changes


def private_projection(core, path, value):
    if path.exists():
        core.private_read(path)
    temporary = path.with_name(path.name + ".next")
    core.private_write(temporary, canonical(value).encode())
    os.replace(temporary, path)


def diagnostic_inventory(core, private):
    information = private.lstat()
    core.require(stat.S_ISDIR(information.st_mode) and information.st_uid == 0 and
                 stat.S_IMODE(information.st_mode) == 0o700, "The private diagnostic directory identity changed.")
    names = re.compile(r"(?:worker\.log|worker-exception\.txt|application\.log|unit\.(?:stdout|stderr)|create-gate\.(?:stdout|stderr)|(?:create|restore|rollback)-browser\.(?:json|stderr)|(?:command-[0-9]{4}|cli-[0-9a-f]{8})\.(?:stdout|stderr)|runtime-log-[0-9]{3}\.jsonl|runtime-control-[a-z-]+\.json|http-failure-[0-9]{4}\.body)")
    files = []
    for path in sorted(private.iterdir()):
        if names.fullmatch(path.name):
            info = path.lstat()
            core.require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and
                         stat.S_IMODE(info.st_mode) == 0o600, "A retained diagnostic file identity changed.")
            files.append({"name": path.name, "bytes": info.st_size, "sha256": digest_file(path)})
    return files


def load_core(path):
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_backup_runtime_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The owned pair operator could not be loaded.")
    core = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(core)
    return core


def no_secrets(raw, values):
    for value in values:
        if not isinstance(value, str) or not value:
            continue
        data = value.encode()
        variants = (value, json.dumps(value)[1:-1], urllib.parse.quote(value, safe=""),
                    urllib.parse.quote_plus(value), data.hex(), base64.b64encode(data).decode())
        if any(variant.encode() in raw for variant in variants):
            return False
    return True


def owned_remove(core, path, parent, owner, run):
    core.require(path.parent == parent and path.resolve() == path and not path.is_symlink(),
                 "An owned disposable path changed.")
    marker = json.loads(core.private_read(owner))
    core.require(marker == {"marker": MARKER, "run_id": run}, "A runtime disposal marker changed.")
    shutil.rmtree(path)


class SourceCreateGate:
    """Hold one ordinary-role source transaction until real UI polls prove it."""

    def __init__(self, worker):
        self.worker, self.core = worker, worker.core
        self.pair = worker.pairs[0]
        self.paths = {name: worker.private / ("create-gate-" + name + ".json")
                      for name in ("request", "acquired", "release", "released")}
        self.process = self.output_file = self.error_file = None
        self.output = worker.private / "create-gate.stdout"
        self.application_name = worker.fixture["control_name"] + "_gate"
        self.deadline = None
        self.finished = False
        self.proof = {"table": "public.items", "port": 15432, "source_only": True, "acquired": False,
                      "committed": False, "rollback_attempted": False, "backend_closed": False}
        worker.report["create_gate"] = self.proof

    def signal(self, action):
        return {"Marker": GATE_MARKER, "RunId": self.worker.run, "Phase": "create", "Action": action}

    def records(self):
        raw = self.core.private_read(self.output, maximum=8192)
        return [json.loads(line) for line in raw.splitlines() if line.endswith(b"}")]

    def acknowledge(self, action, timeout):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            records = [value for value in self.records() if value.get("Action") == action]
            if records:
                self.worker.require(len(records) == 1, "The source gate emitted an ambiguous acknowledgement.", "create_gate_protocol_invalid")
                return records[0]
            if self.process.poll() is not None:
                # A short COMMIT acknowledgement may be flushed between the
                # first file read and waitpid observing the client's exit.
                records = [value for value in self.records() if value.get("Action") == action]
                self.worker.require(len(records) == 1, "The source gate exited before its acknowledgement.", "create_gate_process_failed")
                return records[0]
            time.sleep(0.05)
        raise SafeFailure("create_gate_acknowledgement_timeout", "The source gate acknowledgement exceeded its deadline.")

    def write(self, sql):
        self.worker.require(self.process is not None and self.process.poll() is None and self.process.stdin is not None,
                            "The source gate connection is no longer live.", "create_gate_connection_lost")
        self.process.stdin.write(sql)
        self.process.stdin.flush()

    def acquire(self):
        self.worker.stage("create_gate_acquisition")
        self.worker.require(json.loads(self.core.private_read(self.paths["request"])) == self.signal("acquire"),
                            "The source gate request identity changed.", "create_gate_signal_invalid")
        # This reuses the catalog/OID/tag check and a separate low-privilege
        # guarded connection before admitting the long-lived transaction.
        self.worker.db(0, "SELECT 1;")
        db, role, oid = self.pair["database"], self.pair["role"], self.pair["oid"]
        self.output_file = self.output.open("xb")
        self.error_file = (self.worker.private / "create-gate.stderr").open("xb")
        os.chmod(self.output, 0o600)
        os.chmod(self.worker.private / "create-gate.stderr", 0o600)
        self.process = subprocess.Popen([str(self.core.PG_BIN / "psql"), "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At",
            "-h", "127.0.0.1", "-p", "15432", "-U", role, "-d", db], stdin=subprocess.PIPE,
            stdout=self.output_file, stderr=self.error_file, text=True, encoding="utf-8",
            env=dict(self.core.BASE_ENV, PGPASSWORD=self.pair["password"], PGCONNECT_TIMEOUT="5", PGAPPNAME=self.application_name),
            start_new_session=True)
        self.worker.require(Path(f"/proc/{self.process.pid}/cgroup").read_bytes() == Path("/proc/self/cgroup").read_bytes(),
                            "The source gate escaped its bounded runtime cgroup.", "create_gate_cgroup_mismatch")
        self.write(f"""BEGIN;
SET LOCAL lock_timeout='10s';
SET LOCAL statement_timeout='15s';
SET LOCAL idle_in_transaction_session_timeout='75s';
DO $guard$ BEGIN
 IF current_database()<>'{db}' OR current_user<>'{role}' OR current_setting('port')<>'15432'
 OR current_setting('application_name')<>'{self.application_name}'
 OR (SELECT oid::bigint FROM pg_database WHERE datname=current_database())<>{oid}
 OR (SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=current_database())<>'{role}'
 OR (SELECT shobj_description(oid,'pg_database') FROM pg_database WHERE datname=current_database()) IS DISTINCT FROM '{self.worker.fixture['tag']}'
 THEN RAISE EXCEPTION 'Unexpected source gate database identity'; END IF;
END $guard$;
LOCK TABLE public.items IN ACCESS EXCLUSIVE MODE;
SELECT jsonb_build_object('Action','acquired','BackendPID',pg_backend_pid(),'Database',current_database(),'Role',current_user,
 'DatabaseOID',(SELECT oid::bigint FROM pg_database WHERE datname=current_database()),
 'Locked',EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND relation='public.items'::regclass AND mode='AccessExclusiveLock' AND granted));
""")
        acknowledgement = self.acknowledge("acquired", 20)
        expected = {"Action": "acquired", "Database": db, "Role": role, "DatabaseOID": oid, "Locked": True}
        self.worker.require(type(acknowledgement.get("BackendPID")) is int and acknowledgement["BackendPID"] > 0 and
                            {key: value for key, value in acknowledgement.items() if key != "BackendPID"} == expected,
                            "The source gate did not prove its exact locked database identity.", "create_gate_lock_unconfirmed")
        self.proof.update({"acquired": True, "backend_pid": acknowledgement["BackendPID"], "same_cgroup": True})
        self.deadline = time.monotonic() + 60
        private_projection(self.core, self.paths["acquired"], self.signal("acquired"))
        self.worker.stage("browser_create_gate_held")

    def release(self):
        self.worker.stage("create_gate_release")
        value = json.loads(self.core.private_read(self.paths["release"]))
        extra = {"OperationId", "FirstResponseSequence", "SecondResponseSequence"}
        self.worker.require(set(value) == set(self.signal("release")) | extra and
                            {key: item for key, item in value.items() if key not in extra} == self.signal("release") and
                            isinstance(value["OperationId"], str) and re.fullmatch(r"[0-9a-f]{32}", value["OperationId"]) and
                            type(value["FirstResponseSequence"]) is int and type(value["SecondResponseSequence"]) is int and
                            0 < value["FirstResponseSequence"] < value["SecondResponseSequence"] <= 10000,
                            "The source gate release signal is invalid.", "create_gate_signal_invalid")
        operation = self.worker.request("/admin/v1/backup-operations/" + value["OperationId"])["Operation"]
        if operation.get("State") in ("failed", "cancelled", "interrupted"):
            self.worker.record_operation_failure(operation)
        self.worker.require(operation["Id"] == value["OperationId"] and operation["Kind"] == "create" and operation["State"] == "running",
                            "The real create job is not running at source gate release.", "create_gate_operation_not_running")
        self.write("COMMIT;\nSELECT jsonb_build_object('Action','released','BackendPID',pg_backend_pid(),"
                   "'Unlocked',NOT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid() AND relation='public.items'::regclass AND mode='AccessExclusiveLock' AND granted));\n\\q\n")
        acknowledgement = self.acknowledge("released", 10)
        self.worker.require(acknowledgement == {"Action": "released", "BackendPID": self.proof["backend_pid"], "Unlocked": True},
                            "The source gate did not acknowledge a committed unlock.", "create_gate_unlock_unconfirmed")
        self.process.wait(timeout=10)
        self.worker.require(self.process.returncode == 0, "The committed source gate exited abnormally.", "create_gate_exit_nonzero")
        self.finished = True
        self.proof.update({"committed": True, "operation_id": value["OperationId"],
                          "first_response_sequence": value["FirstResponseSequence"], "second_response_sequence": value["SecondResponseSequence"]})
        self.close()
        value["Action"] = "released"
        private_projection(self.core, self.paths["released"], value)
        self.worker.stage("browser_create_download_import")

    def poll(self):
        if self.process is None and self.paths["request"].exists():
            self.acquire()
        if self.process is not None and not self.finished:
            self.worker.require(self.process.poll() is None and time.monotonic() < self.deadline,
                                "The source gate lost its connection or exceeded its holding deadline.", "create_gate_hold_timeout")
            if self.paths["release"].exists():
                self.release()

    def close(self):
        if self.process is None or self.proof["backend_closed"]:
            return
        if not self.finished and self.process.poll() is None:
            self.proof["rollback_attempted"] = True
            try:
                self.write("ROLLBACK;\n\\q\n")
                self.process.wait(timeout=10)
            except (OSError, subprocess.TimeoutExpired, SafeFailure):
                if self.process.poll() is None:
                    os.killpg(self.process.pid, signal.SIGKILL)
                    self.process.wait(timeout=5)
        if self.process.stdin is not None and not self.process.stdin.closed:
            try:
                self.process.stdin.close()
            except OSError:
                pass
        for stream in (self.output_file, self.error_file):
            if stream is not None and not stream.closed:
                stream.close()
        # Fence only this run's source/role/application-name triple if a client
        # timeout left its server backend behind. Never select a shared PID.
        db, role = self.pair["database"], self.pair["role"]
        self.core.pg(f"SELECT pg_terminate_backend(pid,5000) FROM pg_stat_activity WHERE datname='{db}' AND usename='{role}' AND application_name='{self.application_name}';")
        remaining = self.core.pg(f"SELECT count(*) FROM pg_stat_activity WHERE datname='{db}' AND usename='{role}' AND application_name='{self.application_name}';")
        self.worker.require(remaining == "0", "A source gate backend remains after its transaction closed.", "create_gate_backend_remains")
        self.proof["backend_closed"] = True
        self.finished = True


def create_runner(core, args):
    class RuntimeRunner(core.Runner):
        def __init__(self, arguments):
            super().__init__(arguments)
            self.runtime = Path("/dev/shm") / ("goby-backup-runtime-" + self.run)
            self.private = self.output / "private"
            self.worker_input = self.output / "worker-input.json"
            self.worker_fixture = None
            self.report.update({"scenario": "native_binary_browser_offline_recovery", "runtime_marker": MARKER,
                                "stage": "operator_prepare"})

        def prepare(self):
            core.require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
                         "Use the authorized root test-env SSH operator.")
            super().prepare()
            self.report["stage"] = "artifact_provenance"
            for path, expected, executable in ((args.binary, args.binary_sha256, True),
                                                (args.assets, args.assets_sha256, False)):
                resolved = path.resolve(strict=True)
                info = path.lstat()
                core.require(resolved == path and resolved.is_relative_to(core.CONTROL) and
                             stat.S_ISREG(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022 and
                             (not executable or info.st_mode & 0o001) and digest_file(path) == expected,
                             "A prepared artifact has unexpected ownership, path, permissions, or digest.")
            core.require(all((args.source / "web/admin" / name).is_file() for name in (*BROWSER_FILES, "package-lock.json")) and
                         (NODE_MODULES / "@playwright/test/cli.js").is_file() and BROWSER_CACHE.is_dir(),
                         "The runtime journey or reusable browser dependencies are unavailable.")
            for path, required in ((Path("/dev/shm"), 160 << 20), (core.WORK, 128 << 20)):
                fs = os.statvfs(path)
                core.require(fs.f_bavail * fs.f_frsize >= required, "Insufficient owned scratch headroom.")
            self.private.mkdir(mode=0o700)
            core.private_write(self.private / "owner.json", canonical({"marker": MARKER, "run_id": self.run}).encode())
            (self.output / "artifacts").mkdir(mode=0o700)
            worker = {"marker": MARKER, "run_id": self.run, "output": str(self.output),
                      "runtime": str(self.runtime), "source": str(args.source), "binary": str(args.binary),
                      "binary_sha256": args.binary_sha256, "assets": str(args.assets),
                      "assets_sha256": args.assets_sha256, "pairs": self.pairs, "tag": self.tag,
                      "core": str(args.core_runner.resolve()), "core_sha256": digest_file(args.core_runner),
                      "wrapper": str(Path(__file__).resolve()), "wrapper_sha256": digest_file(Path(__file__).resolve()),
                      "source_manifest_sha256": args.manifest_sha256, "binary_source": str(args.binary_source),
                      "binary_source_manifest_sha256": args.binary_manifest_sha256,
                      "control_name": self.control_name, "unit": self.unit}
            self.worker_fixture = worker
            _, runtime_source_changes = verify_inputs(core, worker)
            core.private_write(self.worker_input, canonical(worker).encode())
            self.report["provenance"] = {"wrapper_sha256": digest_file(Path(__file__).resolve()),
                "core_operator_sha256": digest_file(args.core_runner), "binary_sha256": args.binary_sha256,
                "assets_archive_sha256": args.assets_sha256, "source_manifest_sha256": args.manifest_sha256,
                "binary_source": str(args.binary_source), "binary_source_manifest_sha256": args.binary_manifest_sha256,
                "binary_source_production_members_unchanged": True, "runtimeOnlySourceChanges": runtime_source_changes,
                "artifact_digest_origin": "operator_supplied_build_evidence",
                "browser_inputs_sha256": {name: digest_file(args.source / "web/admin" / name)
                                            for name in (*BROWSER_FILES, "package-lock.json")}}
            self.report["checks"]["prepared_source_binary_assets_provenance"] = True

        def execute(self):
            self.report["stage"] = "runtime_worker"
            verify_inputs(core, self.worker_fixture)
            log = self.private / "worker.log"
            core.private_write(log, b"")
            invocation = ["systemd-run", "--quiet", "--wait", "--unit=" + self.unit,
                "--service-type=exec", "--property=Description=" + self.tag,
                "--property=MemoryMax=1536M", "--property=MemorySwapMax=0", "--property=CPUQuota=150%",
                "--property=KillMode=control-group", "--property=OOMPolicy=kill", "--property=TimeoutStopSec=15",
                "--property=RuntimeMaxSec=1200", "--property=WorkingDirectory=" + str(args.source),
                "--property=StandardOutput=append:" + str(log), "--property=StandardError=append:" + str(log),
                "/usr/bin/python3", str(Path(__file__).resolve()), "--worker", str(self.worker_input)]
            self.unit_intent = True
            wrapper = subprocess.Popen(invocation, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, env=core.BASE_ENV, start_new_session=True)
            proof = None
            deadline = time.monotonic() + 1230
            try:
                while wrapper.poll() is None:
                    core.require(time.monotonic() < deadline, "The runtime acceptance exceeded its outer deadline.")
                    if proof is None:
                        raw = core.command(["systemctl", "show", self.unit, "--property=Description", "--property=MainPID",
                            "--property=MemoryMax", "--property=MemorySwapMax", "--property=CPUQuotaPerSecUSec", "--property=KillMode"])
                        values = dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)
                        if values.get("Description") == self.tag and values.get("MainPID") not in (None, "0"):
                            core.require(values.get("MemoryMax") == str(1536 << 20) and values.get("MemorySwapMax") == "0" and
                                         values.get("CPUQuotaPerSecUSec") == "1.500000s" and values.get("KillMode") == "control-group",
                                         "The real runtime cgroup differs from the reviewed bounds.")
                            proof = values
                    time.sleep(0.2)
                stdout, stderr = wrapper.communicate(timeout=5)
                core.private_write(self.private / "unit.stdout", stdout)
                core.private_write(self.private / "unit.stderr", stderr)
            finally:
                if wrapper.poll() is None:
                    self.stop_unit()
                    os.killpg(wrapper.pid, signal.SIGKILL)
                    wrapper.communicate(timeout=5)
            core.require(proof is not None, "The active runtime cgroup was not observed.")
            self.report["cgroup"] = proof
            self.report["unit_exit"] = wrapper.returncode
            result_path = self.output / "runtime-result.json"
            if result_path.is_file():
                result = json.loads(core.private_read(result_path, maximum=4 << 20))
                core.require(result.get("marker") == MARKER and result.get("run_id") == self.run,
                             "The worker evidence identity changed.")
                self.report["runtime"] = result
            progress_path = self.output / "runtime-progress.json"
            if progress_path.is_file():
                progress = json.loads(core.private_read(progress_path))
                core.require(progress.get("marker") == MARKER and progress.get("run_id") == self.run,
                             "The worker progress identity changed.")
                self.report["runtime_progress"] = progress
            verify_inputs(core, self.worker_fixture)
            self.report["checks"]["source_and_artifact_bytes_unchanged_after_runtime"] = True
            core.require(wrapper.returncode == 0 and self.report.get("runtime", {}).get("status") == "passed",
                         "Real runtime acceptance failed; only the safe worker projection is published.")
            self.report["checks"]["actual_binary_browser_activation_rollback_restart_and_offline_cli"] = True
            self.report["stage"] = "completed"

        def cleanup(self):
            super().cleanup()
            if self.report["cleanup"].get("owned_cgroup_stopped") is not True:
                self.report["private_diagnostics"] = {"directory": str(self.private), "retained": True,
                                                     "unavailable_reason": "owned_process_stop_not_confirmed"}
                return
            if self.worker_fixture is not None:
                try:
                    verify_inputs(core, self.worker_fixture)
                    self.report["checks"]["source_and_artifact_bytes_unchanged_after_runtime"] = True
                except Exception:
                    self.report["status"] = "failed"
                    self.report["failure"] = {"stage": "final_artifact_provenance", "class": "frozen_input_changed"}
            progress_path = self.output / "runtime-progress.json"
            if progress_path.is_file() and "runtime_progress" not in self.report:
                try:
                    progress = json.loads(core.private_read(progress_path))
                    core.require(progress.get("marker") == MARKER and progress.get("run_id") == self.run,
                                 "The final worker progress identity changed.")
                    self.report["runtime_progress"] = progress
                except Exception:
                    self.report["status"] = "failed"
            preserve_runtime = False
            if self.private.exists():
                try:
                    if self.report["status"] != "passed":
                        for name in ("diagnostics", "recovery", "operations", "backups"):
                            directory = self.runtime / "application" / name
                            if directory.exists():
                                info = directory.lstat()
                                core.require(stat.S_ISDIR(info.st_mode) and info.st_uid == 995 and stat.S_IMODE(info.st_mode) == 0o700,
                                             "An owned recovery diagnostic directory identity changed.")
                        controls = {"operations-owner": "operations/.goby-recovery-control.json",
                            "operations-current": "operations/current.json", "operations-proof": "operations/cas-proof.json",
                            "lifecycle-owner": "recovery/.goby-lifecycle.json", "lifecycle-registry": "recovery/generation-registry.json",
                            "lifecycle-active": "recovery/active-generation.json", "lifecycle-transition": "recovery/activation-journal.json",
                            "backups-owner": "backups/.goby-backup-store.json", "backups-catalog": "backups/.goby-backup-catalog.json"}
                        for label, relative in controls.items():
                            path = self.runtime / "application" / relative
                            if path.exists():
                                raw = core.private_read(path, uid=995, maximum=2 << 20)
                                core.private_write(self.private / ("runtime-control-" + label + ".json"), raw)
                    if self.report["status"] != "passed" and (self.runtime / "application/diagnostics").is_dir():
                        logs = sorted((self.runtime / "application/diagnostics").glob("*.jsonl"))
                        core.require(len(logs) <= 4, "The owned runtime diagnostic inventory exceeded its bound.")
                        for index, path in enumerate(logs):
                            raw = core.private_read(path, uid=995, maximum=65536)
                            core.private_write(self.private / ("runtime-log-%03d.jsonl" % index), raw)
                    self.report["private_diagnostics"] = {"directory": str(self.private), "retained": True,
                        "files": diagnostic_inventory(core, self.private)}
                    self.report["cleanup"]["private_diagnostic_projection_recorded"] = True
                except Exception:
                    self.report["cleanup"]["private_diagnostic_projection_recorded"] = False
                    self.report["status"] = "failed"
                    preserve_runtime = self.runtime.exists()
            try:
                if preserve_runtime:
                    core.require(self.runtime.parent == Path("/dev/shm") and self.runtime.resolve() == self.runtime and
                                 json.loads(core.private_read(self.runtime / "owner.json")) == {"marker": MARKER, "run_id": self.run},
                                 "The runtime diagnostic retention identity changed.")
                    self.runtime.chmod(0o700)
                    self.report["private_runtime_diagnostics"] = {"directory": str(self.runtime), "retained": True,
                        "reason": "private_diagnostic_capture_incomplete"}
                    self.report["cleanup"]["failed_runtime_diagnostics_restricted_and_retained"] = True
                elif self.runtime.exists():
                    owned_remove(core, self.runtime, Path("/dev/shm"), self.runtime / "owner.json", self.run)
                self.report["cleanup"]["private_runtime_removed"] = not preserve_runtime
            except Exception:
                self.report["cleanup"]["private_runtime_removed"] = False
                self.report["status"] = "failed"
            self.report["cleanup"]["synthetic_database_credentials_invalidated"] = all(
                self.report["cleanup"].get(pair["database"] + "_removed") is True and
                self.report["cleanup"].get(pair["role"] + "_removed") is True for pair in self.pairs)
            for path in (self.worker_input, self.output / "run.env"):
                if path.exists():
                    try:
                        core.private_read(path)
                        path.unlink()
                    except Exception:
                        self.report["cleanup"]["private_environment_removed"] = False
                        self.report["status"] = "failed"
                        break
            else:
                self.report["cleanup"]["private_environment_removed"] = True
            if self.report["status"] == "passed":
                try:
                    if self.private.exists():
                        owned_remove(core, self.private, self.output, self.private / "owner.json", self.run)
                    self.report["cleanup"]["private_browser_credentials_removed"] = True
                    self.report.get("private_diagnostics", {})["retained"] = False
                except Exception:
                    self.report["cleanup"]["private_browser_credentials_removed"] = False
                    self.report["status"] = "failed"
            if self.report["status"] != "passed":
                retained = False
                if self.private.exists():
                    try:
                        info = self.private.lstat()
                        core.require(self.private.resolve() == self.private and stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                                     json.loads(core.private_read(self.private / "owner.json")) == {"marker": MARKER, "run_id": self.run},
                                     "The failed diagnostic retention identity changed.")
                        self.private.chmod(0o700)
                        retained = True
                        self.report.setdefault("private_diagnostics", {"directory": str(self.private), "retained": True,
                                               "unavailable_reason": "diagnostic_inventory_incomplete"})
                    except Exception:
                        self.report["private_diagnostics"] = {"directory": str(self.private), "retained": False,
                                                             "unavailable_reason": "private_directory_identity_changed"}
                else:
                    self.report["private_diagnostics"] = {"retained": False, "unavailable_reason": "private_directory_not_created"}
                self.report["cleanup"]["failed_private_diagnostics_retained"] = retained
                self.report.setdefault("failure", self.report.get("runtime", {}).get("failure", {
                    "stage": self.report.get("runtime_progress", {}).get("stage", self.report["stage"]),
                    "class": "worker_exit_without_complete_result" if self.unit_intent else "operator_preparation_failed"}))

    return RuntimeRunner(args)


class Worker:
    def __init__(self, core, fixture):
        self.core, self.fixture = core, fixture
        self.run = fixture["run_id"]
        self.output, self.runtime = Path(fixture["output"]), Path(fixture["runtime"])
        self.private = self.output / "private"
        self.app_root = self.runtime / "application"
        self.source, self.binary = Path(fixture["source"]), Path(fixture["binary"])
        self.pairs = fixture["pairs"]
        self.goby = pwd.getpwnam("goby")
        self.secrets = [pair["password"] for pair in self.pairs]
        self.initial_password, self.changed_password = secrets.token_urlsafe(30), secrets.token_urlsafe(30)
        self.passphrase, self.setup_token = "  recovery " + secrets.token_urlsafe(30) + "  ", secrets.token_urlsafe(32)
        self.secrets.extend((self.initial_password, self.changed_password, self.passphrase, self.setup_token))
        self.admin_name = "runtime-admin-" + self.run
        self.app = self.app_log = self.binary_fd = None
        self.csrf = self.user_id = None
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.cookies))
        self.report = {"marker": MARKER, "run_id": self.run, "status": "running", "stage": "worker_initialization",
                       "checks": {}, "browser": {}, "generations": {}}
        self.ids = {name: secrets.token_hex(16) for name in ("item", "play", "encoding", "scan")}
        self.source_disabled = False
        self.command_count = 0
        self.http_count = 0
        self.core.command = self.diagnostic_command

    def require(self, value, message, code="acceptance_assertion_failed"):
        if not value:
            raise SafeFailure(code, message)

    def stage(self, value):
        self.require(re.fullmatch(r"[a-z][a-z0-9_]{0,63}", value), "The fixed progress stage is invalid.")
        self.report["stage"] = value
        private_projection(self.core, self.output / "runtime-progress.json", {
            "marker": MARKER, "run_id": self.run, "stage": value, "status": self.report["status"]})

    def fail(self, error):
        self.report["status"] = "failed"
        # Raw exception context can include SQL or response values. Preserve it
        # only in the root-private diagnostic directory, never in the report.
        exception = "".join(traceback.format_exception(type(error), error, error.__traceback__))
        self.core.private_write(self.private / "worker-exception.txt", exception.encode()[:65536])
        if isinstance(error, SafeFailure):
            classification = error.code
            self.report["safe_failure_message"] = str(error)
        elif isinstance(error, subprocess.TimeoutExpired):
            classification = "controlled_process_timeout"
        elif isinstance(error, self.core.Failure):
            classification = "owned_operator_assertion_failed"
        elif isinstance(error, (json.JSONDecodeError, KeyError, TypeError, ValueError)):
            classification = "private_result_contract_invalid"
        elif isinstance(error, OSError):
            classification = "private_filesystem_or_process_unavailable"
        else:
            classification = "unexpected_worker_exception"
        self.report["failure"] = {"stage": self.report["stage"], "class": classification}

    def diagnostic_command(self, args, text=None, timeout=30, env=None):
        self.command_count += 1
        name = "command-%04d" % self.command_count
        self.require(self.command_count <= 9999, "The controlled command budget was exceeded.", "command_budget_exceeded")
        process = subprocess.Popen([str(arg) for arg in args], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, text=True, encoding="utf-8", errors="replace", env=env or self.core.BASE_ENV,
            start_new_session=True)
        timed_out = False
        try:
            try:
                output, errors = process.communicate(input=text, timeout=timeout)
            except subprocess.TimeoutExpired:
                timed_out = True
                os.killpg(process.pid, signal.SIGKILL)
                output, errors = process.communicate(timeout=5)
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate(timeout=5)
        self.core.private_write(self.private / (name + ".stdout"), output.encode())
        self.core.private_write(self.private / (name + ".stderr"), errors.encode())
        if timed_out or process.returncode != 0:
            executable = Path(str(args[0])).name
            category = {"runuser": "postgresql_control", "psql": "postgresql_fixture", "systemctl": "cgroup_identity",
                        "go": "go_toolchain", "ffmpeg": "media_toolchain", "ffprobe": "media_toolchain"}.get(executable, "controlled_command")
            self.report["failed_command"] = {"diagnostic_prefix": name, "exit_code": process.returncode}
            raise SafeFailure(category + ("_timeout" if timed_out else "_failed"), "A controlled command failed; its private output was retained.")
        return output.strip()

    def prepare(self):
        self.stage("prepare_runtime_identity")
        self.source_files, runtime_source_changes = verify_inputs(self.core, self.fixture)
        self.report["runtimeOnlySourceChanges"] = runtime_source_changes
        self.require(self.goby.pw_uid == 995 and self.fixture["marker"] == MARKER and
                     re.fullmatch(r"[0-9]{8}_[0-9]{6}_[0-9a-f]{8}", self.run), "The isolated runtime identity changed.")
        self.require(self.runtime == Path("/dev/shm") / ("goby-backup-runtime-" + self.run), "The runtime path changed.")
        self.require(self.output == self.core.WORK / ("backuppg-runtime-" + self.run) and len(self.pairs) == 2,
                     "The runtime output or fixture pair identity changed.")
        for pair, side in zip(self.pairs, ("source", "target")):
            suffix = self.run.replace("_", "")
            self.require(pair["database"] == "goby_backup_" + side + "_" + suffix and
                         pair["role"] == "goby_backup_" + side + "_r_" + suffix and re.fullmatch(r"[0-9a-f]{48}", pair["password"]),
                         "A runtime database or role is not the canonical independently owned fixture.")
        unit = self.core.command(["systemctl", "show", self.fixture["unit"], "--property=Description", "--property=MainPID",
                                 "--property=MemoryMax", "--property=MemorySwapMax", "--property=CPUQuotaPerSecUSec"])
        properties = dict(line.split("=", 1) for line in unit.splitlines() if "=" in line)
        self.require(properties == {"Description": self.fixture["tag"], "MainPID": str(os.getpid()),
                     "MemoryMax": str(1536 << 20), "MemorySwapMax": "0", "CPUQuotaPerSecUSec": "1.500000s"},
                     "The runtime worker is not the main process of its bounded owned cgroup.")
        self.runtime.mkdir(mode=0o750)
        os.chown(self.runtime, 0, self.goby.pw_gid)
        self.runtime.chmod(0o750)
        self.core.private_write(self.runtime / "owner.json", canonical({"marker": MARKER, "run_id": self.run}).encode())
        self.app_root.mkdir(mode=0o700)
        os.chown(self.app_root, self.goby.pw_uid, self.goby.pw_gid)
        for name in ("media", "cache", "diagnostics", "recovery", "backups", "operations", "tmp"):
            target = self.app_root / name
            target.mkdir(mode=0o700)
            os.chown(target, self.goby.pw_uid, self.goby.pw_gid)
        assets = self.app_root / "admin"
        assets.mkdir(mode=0o755)
        total = 0
        with tarfile.open(self.fixture["assets"], "r:*") as archive:
            for member in archive.getmembers():
                relative = Path(member.name)
                self.require(not relative.is_absolute() and ".." not in relative.parts and
                             (member.isfile() or member.isdir()), "The prepared assets contain an unsafe member.")
                total += member.size
                self.require(total <= 32 << 20, "The prepared assets exceed the fixture budget.")
                destination = assets / relative
                if member.isdir():
                    destination.mkdir(mode=0o755, parents=True, exist_ok=True)
                else:
                    destination.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
                    with archive.extractfile(member) as source, destination.open("xb") as output:
                        shutil.copyfileobj(source, output)
                    destination.chmod(0o644)
        self.require((assets / "index.html").is_file(), "The prepared dashboard root is missing.")
        for directory in (assets, *(path for path in assets.rglob("*") if path.is_dir())):
            directory.chmod(0o755)
        self.binary_fd = os.open(self.binary, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(os.dup(self.binary_fd), "rb") as source:
            self.require(hashlib.sha256(source.read()).hexdigest() == self.fixture["binary_sha256"], "The executable changed before pinning.")
        os.lseek(self.binary_fd, 0, os.SEEK_SET)
        self.require(digest_file(Path(self.fixture["assets"])) == self.fixture["assets_sha256"], "The assets changed during extraction.")
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            self.port = listener.getsockname()[1]
        self.require(self.port not in (5432, 15432, 18096, 18097), "The temporary application port is protected.")
        self.origin = "http://127.0.0.1:" + str(self.port)
        # libpq/pgx environment defaults are control-plane state. The prepared
        # Go process must resolve only its explicit GOBY database configuration.
        env = {name: value for name, value in self.core.BASE_ENV.items() if not name.upper().startswith("PG")}
        for pair, variable in zip(self.pairs, ("GOBY_DATABASE_URL", "GOBY_RECOVERY_DATABASE_URL")):
            env[variable] = f"postgresql://{pair['role']}:{pair['password']}@127.0.0.1:15432/{pair['database']}?sslmode=disable"
            self.secrets.append(env[variable])
        env.update({"GOBY_LISTEN": "127.0.0.1:" + str(self.port), "GOBY_PUBLIC_URL": self.origin,
            "GOBY_COOKIE_SECURE": "false", "GOBY_SETUP_TOKEN": self.setup_token,
            "GOBY_SERVER_NAME": "Goby recovery runtime source", "GOBY_WEB_DIR": str(assets),
            "GOBY_MEDIA_ROOTS": str(self.app_root / "media"), "GOBY_TRANSCODING_ENABLED": "false",
            "GOBY_TRANSCODE_CACHE": str(self.app_root / "cache"), "GOBY_LOG_DIR": str(self.app_root / "diagnostics"),
            "GOBY_LOG_MAX_FILE_BYTES": "65536", "GOBY_LOG_MAX_FILES": "4", "GOBY_LOG_MIN_FREE_BYTES": str(8 << 20),
            "GOBY_API_KEY_MASTER_KEY_FILE": str(self.app_root / "master.key"),
            "GOBY_RECOVERY_STATE_DIR": str(self.app_root / "recovery"), "GOBY_BACKUP_DIR": str(self.app_root / "backups"),
            "GOBY_RECOVERY_OPERATIONS_DIR": str(self.app_root / "operations"),
            "GOBY_PG_DUMP": str(self.core.PG_BIN / "pg_dump"), "GOBY_PG_RESTORE": str(self.core.PG_BIN / "pg_restore"),
            "GOBY_BACKUP_TIMEOUT": "3m", "GOBY_BACKUP_MAX_OBJECT_BYTES": str(16 << 20),
            "GOBY_BACKUP_MAX_TOTAL_BYTES": str(96 << 20), "GOBY_BACKUP_MAX_OBJECTS": "12",
            "GOBY_BACKUP_MIN_FREE_BYTES": str(16 << 20), "GOBY_STARTUP_TIMEOUT": "30s",
            "GOBY_FFMPEG": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg",
            "GOBY_FFPROBE": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe",
            "GOMAXPROCS": "2", "GOMEMLIMIT": "384MiB", "TMPDIR": str(self.app_root / "tmp"),
            "GOBY_BACKUP_RUNTIME_RUN_ID": self.run})
        self.environment = env
        inherited_pg_names = sorted(name for name in env if name.upper().startswith("PG"))
        self.require(not inherited_pg_names, "The Go runtime inherited PostgreSQL control options.", "go_environment_contains_libpq_options")
        self.report["application_environment"] = {"inheritedPGNames": inherited_pg_names}
        self.report["checks"]["go_application_and_cli_environment_excludes_control_pg_options"] = True
        self.browser_work = self.private / "browser"
        (self.browser_work / "e2e").mkdir(mode=0o700, parents=True)
        for relative in BROWSER_FILES:
            shutil.copyfile(self.source / "web/admin" / relative, self.browser_work / relative)
            self.require(digest_file(self.browser_work / relative) == self.source_files["web/admin/" + relative],
                         "A private browser input differs from the frozen manifest.", "browser_input_digest_mismatch")
        (self.browser_work / "node_modules").symlink_to(NODE_MODULES, target_is_directory=True)
        self.stage("verify_pinned_toolchains")
        self.core.command([self.core.GO, "version"], timeout=10)
        go_version = self.core.command([self.core.GO, "version", "-m", self.binary], timeout=10)
        self.require("go1.27.1" in go_version, "The prepared executable was not built with Go 1.27.1.")
        self.require("17." in self.core.command([self.core.PG_BIN / "pg_dump", "--version"]), "PostgreSQL 17 tools are required.")
        for tool in ("GOBY_FFMPEG", "GOBY_FFPROBE"):
            self.require("version 9.0.1" in self.core.command([env[tool], "-version"]).splitlines()[0], "The pinned FFmpeg 9.0.1 tool identity changed.")
        playwright = json.loads((NODE_MODULES / "@playwright/test/package.json").read_text())["version"]
        locked = json.loads((self.source / "web/admin/package-lock.json").read_text())["packages"]["node_modules/@playwright/test"]["version"]
        self.require(playwright == locked, "The reusable browser driver differs from the supplied source lockfile.")
        self.report["tools"] = {"go": "1.27.1", "postgresql": "17", "ffmpeg": "9.0.1", "playwright": playwright}
        self.report["http_port"] = self.port
        self.report["checks"]["separate_private_state_backup_and_operation_roots"] = True

    def request(self, route, *, method="GET", body=None, expected=(200,), headers=None, response_kind="json"):
        self.require(route.startswith("/") and not route.startswith("//"), "A fixture route left its isolated origin.")
        self.require(response_kind == "json" or response_kind == "emby_unauthorized" and
                     route == "/emby/System/Info" and method == "GET" and expected == (401,),
                     "The explicit response contract is not permitted for this fixture request.")
        self.http_count += 1
        self.require(self.http_count <= 800, "The native control request budget was exceeded.", "native_http_budget_exceeded")
        template = re.sub(r"/[0-9a-f]{32}(?=/|$)|/[1-9][0-9]*(?=/|$)", "/{id}", route.split("?", 1)[0])
        metadata = {"method": method, "route_template": template, "status": None, "bytes": 0,
                    "response_kind": response_kind}

        def retain_failure(raw, classification, error=None):
            name = "http-failure-%04d.body" % self.http_count
            self.core.private_write(self.private / name, raw)
            failure = dict(metadata, classification=classification, bytes=len(raw), diagnostic=name,
                           body_sha256=hashlib.sha256(raw).hexdigest())
            if error is not None:
                failure["exception_type"] = type(error).__name__
                if isinstance(error, json.JSONDecodeError):
                    failure["json_position"] = {"line": error.lineno, "column": error.colno, "offset": error.pos}
            self.report["last_http_failure"] = failure

        values = {"Accept": "application/json", "Origin": self.origin}
        values.update(headers or {})
        data = None if body is None else canonical(body).encode()
        if data is not None:
            values["Content-Type"] = "application/json"
        if method not in ("GET", "HEAD") and self.csrf:
            values["X-CSRF-Token"] = self.csrf
        request = urllib.request.Request(self.origin + route, method=method, data=data, headers=values)
        try:
            response = self.opener.open(request, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        except (OSError, ValueError, http.client.HTTPException) as error:
            retain_failure(b"", "transport_open", error)
            raise SafeFailure("native_http_transport_open_failed", "An isolated HTTP connection could not be opened.") from error
        metadata["status"] = response.status
        content_type = response.headers.get("Content-Type", "")
        metadata["content_type"] = content_type if content_type in ("application/json", "text/plain", "application/json; charset=utf-8") else "other"
        raw = b""
        try:
            with response:
                raw = response.read((4 << 20) + 1)
        except (OSError, ValueError, http.client.HTTPException) as error:
            partial = getattr(error, "partial", raw)
            retain_failure(partial if isinstance(partial, bytes) else raw, "response_read_or_close", error)
            raise SafeFailure("native_http_response_read_failed", "An isolated HTTP response could not be read completely.") from error
        for cookie in self.cookies:
            self.secrets.append(cookie.value)
        if response.status not in expected or len(raw) > 4 << 20:
            retain_failure(raw, "unexpected_status_or_size")
        self.require(response.status in expected and len(raw) <= 4 << 20,
                     "An isolated native HTTP request returned an unexpected status or size.", "native_http_response_rejected")
        if response_kind == "emby_unauthorized":
            # This compatibility endpoint intentionally returns a fixed text
            # error. Keep native administrator endpoints on the JSON contract.
            matches = (response.status == 401 and content_type == "text/plain" and
                       response.headers.get("Content-Length") == "35" and raw == b"Access token is invalid or expired.")
            if not matches:
                retain_failure(raw, "emby_invalid_token_contract")
            self.require(matches, "The revoked application key did not receive the exact Emby text rejection.", "emby_unauthorized_contract_invalid")
            return raw
        if not raw:
            return None
        try:
            return json.loads(raw)
        except ValueError as error:
            retain_failure(raw, "json_decode", error)
            raise SafeFailure("native_http_json_invalid", "An isolated native HTTP response was not valid JSON.") from error

    def login(self, password):
        self.cookies.clear()
        self.csrf = None
        result = self.request("/admin/v1/session", method="POST", body={"Name": self.admin_name, "Password": password})
        self.csrf, self.user_id = result["CSRFToken"], result["User"]["Id"]
        self.secrets.append(self.csrf)
        self.require(result["User"]["Name"] == self.admin_name and result["User"]["IsAdministrator"], "The dedicated native administrator identity changed.")

    def start_app(self):
        self.require(self.app is None, "An isolated application process is already running.")
        self.require(not any(name.upper().startswith("PG") for name in self.environment),
                     "The application environment contains PostgreSQL control options.", "go_environment_contains_libpq_options")
        self.app_log = (self.private / "application.log").open("ab")
        os.chmod(self.private / "application.log", 0o600)
        self.app = subprocess.Popen([str(self.binary)], executable=f"/proc/self/fd/{self.binary_fd}", pass_fds=(self.binary_fd,),
            env=self.environment, cwd=self.app_root, user=self.goby.pw_uid, group=self.goby.pw_gid, extra_groups=[],
            stdin=subprocess.DEVNULL, stdout=self.app_log, stderr=self.app_log, start_new_session=True)
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            self.require(self.app.poll() is None, "The isolated application exited before readiness.", "application_exited_before_readiness")
            try:
                with urllib.request.urlopen(self.origin + "/readyz", timeout=1) as response:
                    if response.status == 200:
                        break
            except OSError:
                pass
            time.sleep(0.2)
        else:
            raise SafeFailure("application_readiness_timeout", "The isolated application did not become ready.")
        process = Path("/proc") / str(self.app.pid)
        self.require(digest_file(process / "exe") == self.fixture["binary_sha256"], "The running executable differs from the prepared binary.")
        uid_line = re.search(r"^Uid:\s+(.+)$", (process / "status").read_text(), re.MULTILINE)
        self.require(uid_line and [int(value) for value in uid_line.group(1).split()] == [995] * 4,
                     "The isolated application is not running as UID 995.")
        self.report.setdefault("application_pids", []).append(self.app.pid)

    def stop_app(self):
        if self.app is not None:
            if self.app.poll() is None:
                os.killpg(self.app.pid, signal.SIGTERM)
                try:
                    self.app.wait(timeout=25)
                except subprocess.TimeoutExpired:
                    os.killpg(self.app.pid, signal.SIGKILL)
                    self.app.wait(timeout=5)
                    raise SafeFailure("application_drain_timeout", "The isolated application failed to drain within its stop budget.") from None
            self.require(self.app.returncode == 0, "The isolated application did not exit cleanly.", "application_exit_nonzero")
            self.app = None
        if self.app_log is not None:
            self.app_log.close()
            self.app_log = None

    def db(self, side, statement):
        pair = self.pairs[side]
        expected = {"oid": pair["oid"], "owner": pair["role"], "tag": self.fixture["tag"]}
        actual = json.loads(self.core.pg(f"SELECT jsonb_build_object('oid',oid::bigint,'owner',pg_get_userbyid(datdba),'tag',shobj_description(oid,'pg_database')) FROM pg_database WHERE datname='{pair['database']}';"))
        self.require(actual == expected, "The independently owned database identity changed.")
        guard = f"""BEGIN; SET LOCAL statement_timeout='10s';
            DO $guard$ BEGIN IF current_database()<>'{pair['database']}' OR current_user<>'{pair['role']}'
            THEN RAISE EXCEPTION 'Unexpected owned fixture identity'; END IF; END $guard$;
            {statement}
            COMMIT;"""
        return self.core.command([self.core.PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At", "-h", "127.0.0.1",
            "-p", "15432", "-U", pair["role"], "-d", pair["database"]], text=guard,
            env=dict(self.core.BASE_ENV, PGPASSWORD=pair["password"]))

    def snapshot(self, side):
        union = " UNION ALL ".join(f"SELECT '{name}' name, COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) FROM public.\"{name}\" r),'[]'::jsonb) rows" for name in TABLES)
        value = json.loads(self.db(side, "SELECT jsonb_build_object('tables',(SELECT jsonb_agg(tablename ORDER BY tablename) FROM pg_tables WHERE schemaname='public'), 'rows',(SELECT jsonb_object_agg(name,rows) FROM (" + union + ") all_rows));"))
        self.require(value["tables"] == list(TABLES), "The persistence snapshot did not cover the complete schema-23 table inventory.")
        self.require({row["version"] for row in value["rows"]["schema_migrations"]} == set(range(1, 24)), "The runtime did not migrate through exact schema 23.")
        return value["rows"]

    def seed(self):
        self.require(self.request("/admin/v1/bootstrap") == {"Initialized": False}, "The owned source was not fresh.")
        self.request("/admin/v1/bootstrap", method="POST", expected=(201,),
                     body={"SetupToken": self.setup_token, "Name": self.admin_name, "Password": self.initial_password})
        self.login(self.initial_password)
        self.keys = []
        for label in ("Retained active key", "Retained revoked key"):
            key = self.request("/admin/v1/api-keys", method="POST", expected=(201,), body={"AppName": label})
            self.keys.append(key)
            self.secrets.append(key["AccessToken"])
        self.request("/admin/v1/api-keys/" + self.keys[1]["Key"]["Id"] + "/revoke", method="POST", body={})
        library = self.request("/admin/v1/libraries", method="POST", expected=(201,), body={
            "Name": "Retained recovery library", "CollectionType": "movies", "Paths": [str(self.app_root / "media")]})
        self.library_id = library["Library"]["Id"]
        session = self.db(0, f"SELECT id FROM sessions WHERE user_id='{self.user_id}' AND kind='admin' AND revoked_at IS NULL;")
        self.require(re.fullmatch(r"[0-9a-f]{32}", session), "The source fixture does not have one native seed session.")
        item, play, encoding, scan = (self.ids[name] for name in ("item", "play", "encoding", "scan"))
        self.db(0, f"""INSERT INTO items(id,library_id,parent_id,name,sort_name,type)
            VALUES('{item}','{self.library_id}','{self.library_id}','Retained recovery movie','retained recovery movie','Movie');
            INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played)
            VALUES('{self.user_id}','{item}',123456789,3,true,false);
            INSERT INTO play_sessions(id,user_id,auth_session_id,device_id,item_id,media_source_id,state,duration_ticks,expires_at)
            VALUES('{play}','{self.user_id}','{session}','runtime-player','{item}','runtime-source','Playing',600000000,clock_timestamp()+interval '1 hour');
            INSERT INTO encoding_jobs(id,user_id,auth_session_id,device_id,play_session_id,item_id,media_source_id,source_stamp,plan,state,created_at,updated_at,last_access_at)
            VALUES('{encoding}','{self.user_id}','{session}','runtime-player','{play}','{item}','runtime-source','fixture-stamp','{{}}','running',now(),now(),now());
            INSERT INTO scan_jobs(id,library_id,status,started_at) VALUES('{scan}','{self.library_id}','Running',clock_timestamp());""")
        self.settings_name("Retained source server")
        self.baseline = self.snapshot(0)
        self.master_digest = digest_file(self.app_root / "master.key")
        self.report["checks"]["native_seed_with_retained_accounts_keys_library_history_and_abandoned_work"] = True

    def settings_name(self, name):
        current = self.request("/admin/v1/settings")
        overrides = current["Overrides"]
        overrides["ServerName"] = name
        self.request("/admin/v1/settings", method="PUT", body={"Revision": current["Revision"],
            "Overrides": overrides, "ServerNameMode": "custom", "Encoding": current["Encoding"]})

    def record_operation_failure(self, operation):
        value = {name: operation.get(name) for name in ("Id", "Kind", "State", "Phase", "ErrorCode")}
        self.require(isinstance(value["Id"], str) and re.fullmatch(r"[0-9a-f]{32}", value["Id"]) and
                     value["Kind"] in ("create", "import", "delete", "restore", "rollback") and
                     value["State"] in ("failed", "cancelled", "interrupted") and
                     isinstance(value["Phase"], str) and re.fullmatch(r"(?:[a-z][a-z0-9_]{0,63})?", value["Phase"]) and
                     isinstance(value["ErrorCode"], str) and re.fullmatch(r"(?:[a-z][a-z0-9_]{0,63})?", value["ErrorCode"]),
                     "The real operation failure projection is invalid.", "native_operation_projection_invalid")
        failures = self.report.setdefault("operation_failures", [])
        self.require(len(failures) < 10, "The real operation failure inventory exceeded its bound.")
        failures.append(value)
        return value

    def admission_fallbacks(self, result):
        values = result.get("AdmissionFallbacks", [])
        self.require(isinstance(values, list) and len(values) <= 5, "The admission fallback evidence is invalid.")
        expected_fields = {"Action", "Reason", "Scope", "RequestId", "OperationId", "Resolved"}
        scopes = {"create": "durable_request_id", "import": "durable_request_id", "plan": "durable_request_id",
                  "apply": "known_plan_id_pending_completion", "rollback": "durable_request_id_after_login"}
        for value in values:
            self.require(isinstance(value, dict) and set(value) == expected_fields and value.get("Action") in scopes and
                         value["Scope"] == scopes[value["Action"]] and type(value["Resolved"]) is bool and
                         value["Reason"] in ("inspector_cache_evicted", "body_unavailable_after_navigation"),
                         "The admission fallback contains an unsupported scope.")
            self.require((value["Action"] == "apply" and value["RequestId"] is None or
                          value["Action"] != "apply" and isinstance(value["RequestId"], str) and re.fullmatch(r"[0-9a-f]{32}", value["RequestId"])) and
                         (value["OperationId"] is None and not value["Resolved"] or
                          isinstance(value["OperationId"], str) and re.fullmatch(r"[0-9a-f]{32}", value["OperationId"])),
                         "The admission fallback did not retain its exact safe request and operation identities.")
        return values

    def browser_phase(self, phase, password, restored_password, backup_id=None):
        gate = SourceCreateGate(self) if phase == "create" else None
        fixture_path, result_path = self.private / (phase + "-fixture.json"), self.private / (phase + "-result.json")
        value = {"Marker": "goby-backup-recovery-fixture-v1", "RunId": self.run, "Phase": phase, "Origin": self.origin,
            "AdminName": self.admin_name, "Password": password, "RestoredPassword": restored_password,
            "Passphrase": self.passphrase, "ArchivePath": str(self.private / "downloaded.age"), "ResultPath": str(result_path),
            "ArtifactDirectory": str(self.output / "artifacts"), "ExpectedTables": list(TABLES), "BackupId": backup_id}
        if gate is not None:
            value.update({"GateRequestPath": str(gate.paths["request"]), "GateAcquiredPath": str(gate.paths["acquired"]),
                          "GateReleasePath": str(gate.paths["release"]), "GateReleasedPath": str(gate.paths["released"])})
        self.core.private_write(fixture_path, canonical(value).encode())
        stdout, stderr = self.private / (phase + "-browser.json"), self.private / (phase + "-browser.stderr")
        environment = dict(self.core.BASE_ENV, PLAYWRIGHT_BROWSERS_PATH=str(BROWSER_CACHE), CI="1", FORCE_COLOR="0",
                           GOBY_SMOKE_BASE_URL=self.origin, GOBY_BACKUP_RECOVERY_FIXTURE=str(fixture_path),
                           GOBY_BACKUP_RUNTIME_RUN_ID=self.run, TMPDIR=str(self.private))
        with stdout.open("xb") as output, stderr.open("xb") as errors:
            os.chmod(stdout, 0o600)
            os.chmod(stderr, 0o600)
            process = subprocess.Popen(["/usr/bin/node", str(NODE_MODULES / "@playwright/test/cli.js"), "test", "e2e/" + SPEC,
                "--config", "playwright.config.ts", "--workers=1", "--retries=0", "--reporter=json", "--trace=off"],
                cwd=self.browser_work, env=environment, stdin=subprocess.DEVNULL, stdout=output, stderr=errors, start_new_session=True)
            try:
                deadline = time.monotonic() + 330
                while process.poll() is None:
                    self.require(time.monotonic() < deadline, "The real browser phase exceeded its deadline.", "browser_process_timeout")
                    if gate is not None:
                        gate.poll()
                    try:
                        process.wait(timeout=0.1)
                    except subprocess.TimeoutExpired:
                        pass
                code = process.returncode
            finally:
                try:
                    if process.poll() is None:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait(timeout=5)
                finally:
                    if gate is not None:
                        gate.close()
        result = json.loads(self.core.private_read(result_path, maximum=1 << 20))
        self.require(result.get("Marker") == "goby-backup-recovery-result-v1" and result.get("RunId") == self.run and
                     result.get("Phase") == phase, "The private real-browser result identity changed.")
        for name in ("BeforeCookie", "AfterCookie"):
            if result.get(name):
                self.secrets.extend((result[name], result[name].split("=", 1)[1]))
        stats = json.loads(self.core.private_read(stdout, maximum=4 << 20)).get("stats", {})
        self.report["browser"][phase] = {"exit_code": code, "checks": result.get("Checks", {}),
            "expected": stats.get("expected"), "unexpected": stats.get("unexpected"), "skipped": stats.get("skipped"), "flaky": stats.get("flaky"),
            "admission_fallbacks": self.admission_fallbacks(result)}
        if result.get("FailureOperation") is not None:
            self.report["browser"][phase]["failure_operation"] = self.record_operation_failure(result["FailureOperation"])
        self.require(code == 0 and result.get("Complete") is True and REQUIRED_PHASE_CHECKS[phase] <= result.get("Checks", {}).keys() and
                     all(value is True for value in result["Checks"].values()) and
                     stats.get("expected") == 1 and stats.get("unexpected") == 0 and stats.get("skipped") == 0 and stats.get("flaky") == 0,
                     "A real browser phase did not pass every required assertion without skips.", "browser_phase_failed")
        if gate is not None:
            self.require(gate.proof["acquired"] and gate.proof["committed"] and gate.proof["backend_closed"],
                         "The real browser did not complete its source-only deterministic create gate.", "create_gate_not_completed")
        return result

    def generation(self, label, side, revision):
        root = self.app_root / "recovery"
        manifest = json.loads(self.core.private_read(root / "active-generation.json", uid=995))
        marker = json.loads(self.core.private_read(root / ".goby-lifecycle.json", uid=995))
        database_marker = json.loads(self.db(side, "SELECT value FROM server_settings WHERE key='goby.recovery.binding.v1';"))
        slot = ("primary", "recovery")[side]
        self.require(manifest["deploymentId"] == marker["deploymentId"] == database_marker["deploymentId"] and
                     manifest["generationId"] == database_marker["generationId"] and manifest["databaseSlot"] == database_marker["slot"] == slot and
                     manifest["revision"] == revision, "The running database and durable generation ownership markers disagree.")
        generation = root / ("generation-" + manifest["generationId"])
        master = generation / "master.key" if manifest["master"] == "generation" else self.app_root / "master.key"
        self.require(digest_file(master) == self.master_digest, "The activated master differs from the exact archived key.")
        descriptor = manifest["config"]
        self.require(descriptor["name"] == "config.json" and digest_file(generation / "config.json") == descriptor["sha256"],
                     "The persisted generation configuration descriptor changed.")
        self.report["generations"][label] = {"revision": str(revision), "slot": slot,
            "generation_id": manifest["generationId"], "active_manifest_sha256": digest_file(root / "active-generation.json"),
            "database_marker_matches": True, "matching_master": True, "matching_config_descriptor": True}
        return manifest

    def retained(self, side, baseline, restored=True):
        current = self.snapshot(side)
        stable_tables = ("users", "application_keys", "application_key_clients", "application_key_devices", "libraries", "library_roots",
                         "items", "user_item_data", "managed_settings", "schema_migrations")
        for name in stable_tables:
            self.require(current[name] == baseline[name], "A retained account, key, catalog, policy, settings, or history table changed.")
        self.require(all(row["revoked_at"] is not None for row in current["sessions"] if row["kind"] == "application_key"),
                     "An archived application credential remains active.")
        for previous in baseline["sessions"]:
            rows = [row for row in current["sessions"] if row["id"] == previous["id"]]
            self.require(len(rows) == 1 and rows[0]["revoked_at"] is not None and
                         (previous["revoked_at"] is None or rows[0]["revoked_at"] == previous["revoked_at"]),
                         "Recovered credential history was deleted, remained active, or lost its prior revocation timestamp.")
        for table, field, active in (("play_sessions", "state", ("Prepared", "Playing", "Paused")),
                                    ("encoding_jobs", "state", ("queued", "running")),
                                    ("scan_jobs", "status", ("Queued", "Running")),
                                    ("task_runs", "state", ("pending", "running", "stopping")),
                                    ("task_run_children", "state", ("waiting", "queued", "running"))):
            self.require(not any(row[field] in active for row in current[table]), "Recovered application state still contains active abandoned work.")
        self.require(current["task_occurrences"] == baseline["task_occurrences"] and
                     {row["id"] for row in current["task_runs"]} == {row["id"] for row in baseline["task_runs"]},
                     "Recovery or restart fabricated a scheduled execution.")
        if restored:
            wanted = {"play_sessions": ("state", "Expired", self.ids["play"]),
                      "encoding_jobs": ("state", "interrupted", self.ids["encoding"]),
                      "scan_jobs": ("status", "Interrupted", self.ids["scan"])}
            for table, (field, expected, identifier) in wanted.items():
                rows = [row for row in current[table] if row["id"] == identifier]
                self.require(len(rows) == 1 and rows[0][field] == expected, "Restoration did not normalize abandoned work to its required terminal state.")
            encoding = next(row for row in current["encoding_jobs"] if row["id"] == self.ids["encoding"])
            self.require(encoding["error_code"] == "backup_restored", "The restored encoding lacks its fixed normalization reason.")
        for key in self.keys:
            self.request("/emby/System/Info", expected=(401,), headers={"X-Emby-Token": key["AccessToken"]},
                         response_kind="emby_unauthorized")
        return current

    def restart(self, label, password, side, revision, baseline):
        self.stage(label + "_restart_stop")
        self.stop_app()
        self.stage(label + "_restart_start")
        self.start_app()
        self.stage(label + "_restart_verify")
        self.login(password)
        self.retained(side, baseline, restored=side == 1)
        self.generation(label + "_restart", side, revision)
        self.report["checks"][label + "_restart_and_native_login"] = True

    def cli(self, arguments):
        self.require(not any(name.upper().startswith("PG") for name in self.environment),
                     "The CLI environment contains PostgreSQL control options.", "go_environment_contains_libpq_options")
        log = self.private / ("cli-" + secrets.token_hex(4) + ".stderr")
        stdout = log.with_suffix(".stdout")
        timed_out = False
        with log.open("xb") as errors, stdout.open("xb") as output_file:
            os.chmod(log, 0o600)
            os.chmod(stdout, 0o600)
            process = subprocess.Popen([str(self.binary), *arguments], executable=f"/proc/self/fd/{self.binary_fd}",
                pass_fds=(self.binary_fd,), env=self.environment, cwd=self.app_root, user=995, group=self.goby.pw_gid,
                extra_groups=[], stdin=subprocess.DEVNULL, stdout=output_file, stderr=errors, start_new_session=True)
            try:
                try:
                    process.wait(timeout=210)
                except subprocess.TimeoutExpired:
                    timed_out = True
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
            finally:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGKILL)
                    process.wait(timeout=5)
        self.report["last_cli"] = {"stage": self.report["stage"], "diagnostic_prefix": log.stem,
                                   "exit_code": process.returncode}
        self.require(not timed_out, "An offline CLI command exceeded its deadline; its private streams were retained.", "offline_cli_timeout")
        output = self.core.private_read(stdout, maximum=1 << 20)
        self.require(process.returncode == 0 and no_secrets(output, self.secrets),
                     "An offline CLI command failed or exposed private content; raw diagnostics are not published.", "offline_cli_failed")
        return json.loads(output)

    def execute(self):
        self.prepare()
        self.stage("start_source_application")
        self.start_app()
        self.stage("seed_native_fixture")
        self.seed()
        self.stage("browser_create_download_import")
        created = self.browser_phase("create", self.initial_password, self.initial_password)
        archive = self.private / "downloaded.age"
        self.require(digest_file(archive) == created["SHA256"], "The privately retained native download changed.")
        # The rollback image must be visibly different from the archived source.
        self.stage("mutate_rollback_fixture")
        managed = self.request("/admin/v1/users/" + self.user_id)["User"]
        self.request("/admin/v1/users/" + self.user_id + "/password", method="POST",
                     body={"Revision": managed["Revision"], "Password": self.changed_password})
        self.login(self.changed_password)
        self.settings_name("Changed server retained for rollback")
        changed = self.snapshot(0)
        self.stage("browser_plan_apply")
        self.browser_phase("restore", self.changed_password, self.initial_password, created["BackupId"])
        self.stage("verify_online_restore")
        self.login(self.initial_password)
        self.retained(1, self.baseline)
        self.generation("online_restore", 1, 1)
        self.require(self.request("/admin/v1/settings")["Effective"]["ServerName"] == "Retained source server", "The active services did not switch to restored settings.")
        self.report["checks"]["full_application_switch_retains_data_and_revokes_credentials"] = True
        self.restart("restored", self.initial_password, 1, 1, self.baseline)
        self.stage("browser_rollback")
        self.browser_phase("rollback", self.initial_password, self.changed_password)
        self.stage("verify_online_rollback")
        self.login(self.changed_password)
        self.retained(0, changed, restored=False)
        self.generation("online_rollback", 0, 2)
        self.require(self.request("/admin/v1/settings")["Effective"]["ServerName"] == "Changed server retained for rollback", "The active services did not switch to rollback settings.")
        self.restart("rollback", self.changed_password, 0, 2, changed)
        self.stage("stop_before_offline_recovery")
        self.stop_app()
        source = self.pairs[0]
        # Make only this explicitly tagged original role unreachable. The real
        # deployment URI and generation state remain unchanged for offline CLI.
        self.stage("make_owned_original_unreachable")
        self.db(0, "SELECT 1;")
        self.core.pg(f"ALTER ROLE {source['role']} NOLOGIN; SELECT pg_terminate_backend(pid,5000) FROM pg_stat_activity WHERE datname='{source['database']}' AND pid<>pg_backend_pid();")
        self.source_disabled = True
        rejected = subprocess.run([self.core.PG_BIN / "psql", "-X", "-q", "-At", "-h", "127.0.0.1", "-p", "15432",
            "-U", source["role"], "-d", source["database"], "-c", "SELECT 1"], env=dict(self.core.BASE_ENV, PGPASSWORD=source["password"]),
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10, check=False)
        self.require(rejected.returncode != 0, "The original isolated database remains reachable before offline recovery.")
        self.stage("offline_cli_status")
        status = self.cli(["recovery", "status"])
        offline_archive = self.app_root / "offline-archive.age"
        self.core.private_write(offline_archive, self.core.private_read(archive, maximum=16 << 20))
        os.chown(offline_archive, 995, self.goby.pw_gid)
        try:
            self.stage("offline_cli_import")
            imported = self.cli(["backup", "import", "--file", str(offline_archive), "--request-id", secrets.token_hex(16)])
        finally:
            offline_archive.unlink()
        self.require(imported["State"] == "completed" and re.fullmatch(r"[0-9a-f]{32}", imported["BackupId"]),
                     "The offline CLI did not import the actual downloaded archive.")
        self.stage("offline_cli_list")
        objects = self.cli(["backup", "list"])["Items"]
        imported_objects = [item for item in objects if item["Id"] == imported["BackupId"]]
        self.require(len(imported_objects) == 1 and imported_objects[0]["SHA256"] == created["SHA256"] and
                     imported_objects[0]["Verified"] is False, "Offline import changed the bytes or prematurely claimed verification.")
        pass_file = self.app_root / "offline-passphrase"
        self.core.private_write(pass_file, self.passphrase.encode())
        os.chown(pass_file, 995, self.goby.pw_gid)
        try:
            self.stage("offline_cli_plan")
            planned = self.cli(["restore", "plan", "--backup-id", imported["BackupId"], "--sha256", created["SHA256"],
                "--request-id", secrets.token_hex(16), "--generation-revision", status["GenerationRevision"],
                "--passphrase-file", str(pass_file), "--restore-defaults", "--replace-rollback"])
        finally:
            pass_file.unlink()
        self.require(planned["State"] == "ready" and planned["CanApply"], "Offline planning did not produce a real ready plan.")
        self.stage("offline_cli_apply")
        applied = self.cli(["restore", "apply", "--id", planned["Id"], "--revision", planned["Revision"],
            "--generation-revision", status["GenerationRevision"], "--accept-no-rollback"])
        self.require(applied["Status"] == "completed" and applied["GenerationRevision"] == "3" and applied["StartService"] is True,
                     "Offline activation did not report a completed startable generation.")
        self.stage("offline_application_start")
        self.start_app()
        self.stage("offline_application_verify")
        self.login(self.initial_password)
        self.retained(1, self.baseline)
        self.generation("offline_cli_restore", 1, 3)
        operation = self.request("/admin/v1/backup-operations/" + planned["Id"])["Operation"]
        self.require(operation["State"] == "completed", "The started application did not retain the offline completion record.")
        self.restart("offline", self.initial_password, 1, 3, self.baseline)
        self.report["checks"]["offline_cli_unreachable_original_private_passphrase_and_restart"] = True
        self.report["table_inventory"] = list(TABLES)
        self.report["retained_table_facts"] = {name: {"rows": len(rows), "sha256": hashlib.sha256(canonical(rows).encode()).hexdigest()}
                                                for name, rows in self.baseline.items()}
        self.report["screenshots"] = {name: digest_file(self.output / "artifacts" / name)
                                      for name in ("backup-recovery-desktop.png", "backup-recovery-mobile.png")}
        self.stage("final_application_stop")
        self.stop_app()
        self.stage("final_artifact_provenance")
        verify_inputs(self.core, self.fixture)
        for relative in BROWSER_FILES:
            self.require(digest_file(self.browser_work / relative) == self.source_files["web/admin/" + relative],
                         "An executed private browser input changed during the journey.", "browser_input_digest_mismatch")
        self.report["checks"]["executed_browser_inputs_and_frozen_artifacts_unchanged"] = True
        self.report["status"] = "passed"
        self.stage("completed")

    def finish(self):
        try:
            self.stop_app()
        except Exception as error:
            if "failure" not in self.report:
                self.fail(error)
            self.report["cleanup_error"] = "The isolated application did not stop cleanly."
        if self.binary_fd is not None:
            os.close(self.binary_fd)
        raw = canonical(self.report).encode()
        self.require(no_secrets(raw, self.secrets), "The worker report contains private values.")
        self.core.private_write(self.output / "runtime-result.json", raw)
        private_projection(self.core, self.output / "runtime-progress.json", {
            "marker": MARKER, "run_id": self.run, "stage": self.report["stage"], "status": self.report["status"]})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--worker", type=Path)
    parser.add_argument("--core-runner", type=Path)
    parser.add_argument("--source", type=Path)
    parser.add_argument("--manifest-sha256")
    parser.add_argument("--binary", type=Path)
    parser.add_argument("--binary-sha256")
    parser.add_argument("--binary-source", type=Path)
    parser.add_argument("--binary-manifest-sha256")
    parser.add_argument("--assets", type=Path)
    parser.add_argument("--assets-sha256")
    args = parser.parse_args()
    if args.worker:
        # Load only the exact root-owned operator named in this private control
        # file. The outer process has already fenced it inside the owned unit.
        information = args.worker.lstat()
        if not stat.S_ISREG(information.st_mode) or information.st_uid != 0 or stat.S_IMODE(information.st_mode) != 0o600:
            raise RuntimeError("The runtime worker control file is not private.")
        fixture = json.loads(args.worker.read_bytes())
        if digest_file(Path(fixture["core"])) != fixture["core_sha256"] or digest_file(Path(__file__).resolve()) != fixture["wrapper_sha256"]:
            raise RuntimeError("The frozen runtime operator changed before execution.")
        core = load_core(Path(fixture["core"]))
        core.BASE_ENV["PGAPPNAME"] = fixture["control_name"]
        worker = Worker(core, fixture)
        try:
            worker.execute()
        except Exception as error:
            worker.fail(error)
        finally:
            worker.finish()
        return 0 if worker.report["status"] == "passed" else 1
    for name in ("core_runner", "source", "binary", "assets", "manifest_sha256", "binary_sha256", "assets_sha256"):
        if getattr(args, name) is None:
            parser.error("--" + name.replace("_", "-") + " is required")
    core = load_core(args.core_runner)
    if (args.binary_source is None) != (args.binary_manifest_sha256 is None):
        parser.error("--binary-source and --binary-manifest-sha256 must be supplied together")
    if args.binary_source is None:
        args.binary_source, args.binary_manifest_sha256 = args.source, args.manifest_sha256
    for value in (args.manifest_sha256, args.binary_sha256, args.assets_sha256, args.binary_manifest_sha256):
        core.require(re.fullmatch(r"[0-9a-f]{64}", value), "A supplied artifact digest is invalid.")
    args.mode = "runtime"
    def interrupted(signum, frame):
        raise core.Failure("The isolated runtime operator was interrupted.")
    for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(sig, interrupted)
    return create_runner(core, args).run_all()


if __name__ == "__main__":
    sys.exit(main())
