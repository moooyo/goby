#!/usr/bin/env python3
"""Run one original-client scenario with owned workers and offline closeout."""
import argparse
import hashlib
import http.client
import io
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

R = Path("/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14")
EPOCH = {"path": str(R / "candidate-backup-limits-revision-01/private/runtime-epoch.json"), "sha256": "72e25f907619fbdf82879070c6fce6178cc8c7881e8015a99991f62e64a2a73e"}
BINDING = {"path": str(R / "candidate-backup-limits-revision-01/private/seed-runtime-binding.json"), "sha256": "92ee92478475390e514f39e554619f322be81062a6d0256820c00bf4c8e0969f"}
ADMISSION = {"path": str(R / "candidate-live-admission-04/private/report.json"), "sha256": "05083c7cc5c65c62e30018136b6a7d383c144c9742d96eaecc52e9c2cfc19653"}
ADMISSION_CLOSEOUT = {"path": str(R / "candidate-live-admission04-closeout.json"), "sha256": "c2aea574a3196b5ef486a2ba2b664d59bb75f5208d3a1a0c53ecaaab81b81eb4"}
HOSTING = {"path": "/opt/goby-test/exec-work-m3e/core-av-original-client-hosting-reconcile-01/hosting.json", "sha256": "2100142b83941e24503838fdf92942aef862bc785bbcfcbe8f93223dd30c53c0"}
AV_VERIFICATION = {"path": str(R / "av-tool-verification-04/verification.json"), "sha256": "1ed8fe0d995026aafc2bdd51aed5b685eec28b66082c15ae29f6e9499ef7f0fa"}
RUNTIME = {"path": str(R / "backup-limits-tool-verification-01/audited-candidate-runtime.py"), "sha256": "1650d6267ab78a07f8c5b77130ad009eeb7212a0bb16251322936792a58dbe66"}
NODE = {"path": "/usr/bin/node", "sha256": "ca0728526aa1cc4e3056decec848ecc6d2c5391cecdd4e21a0ebd221d665c84e"}
SOURCE_FILES = {"closer": "close-audited-candidate-client.mjs", "adapter": "client-browser-audited-candidate.mjs", "gateway": "client-acceptance-gateway.py", "proxy": "client-acceptance-proxy.py",
    "sessionProof": "client-browser-session-proof.mjs", "movie": "client-browser-playback.mjs", "audio": "client-browser-audio-flow.mjs", "subtitles": "client-browser-subtitle-flow.mjs", "tv": "client-browser-tv-flow.mjs"}
FROZEN = {"gateway": "b343f522389bbcb6f704ab3b09d2592ed06573b90961f7b9c8f3eb45b7ab0070", "adapter": "9ff2692ec10254a16f9505ba06c33d81d5980f3421f61a96f8ee917f695b87f6", "closer": "38e8a96e057fee491b4c2324d510779e55890d37e04fdc93d0be71df23cca8ce",
    "proxy": "388965fc772dff82ff13d2a641ecc0e9383e20b9f2a9bc929f48edcf6f394874", "sessionProof": "fea90503a3e279d1ec63723f8785b3421c72d756af4db95f1762fbd67ece2472",
    "movie": "2ddf3ffe7944d92edbb343b4289e0883f01feae09a04d69b902a709566533dac", "audio": "32a828b6c82f3dbd70f3b117bec11e3a6d44192cacb10417e4a75bf781a129e8",
    "subtitles": "e98d3ca5289ba8362450147484bc4cffd13f3d0177d00a266d21edfae0558016", "tv": "5acb53c7fd5852f501e6590d09974913e888b12bd64ac8aef233953dc7cdfb65"}
SCENARIOS = {"movie", "episode", "mp3", "flac", "subtitles", "tv-browse"}
BUDGETS = {"maximumSeconds": 1200, "cleanupSeconds": 240, "clientSeconds": 600, "clientCleanupSeconds": 120,
    "gatewayReadySeconds": 45, "workerGraceSeconds": 30, "unitStopSeconds": 30}
GATEWAY_BUDGETS = {"maxRequests": 1000, "cleanupRequests": 64, "maxApiBodyBytes": 1 << 20, "maxApiTotalBytes": 32 << 20,
    "maxSeconds": 1800, "idleSeconds": 600, "maxConcurrent": 32}
INPUT_KEYS = {"kind", "version", "runId", "scenario", "output", "runtimeEpoch", "seedBinding", "admission", "admissionCloseout", "hosting", "hostingInitialization", "avVerification", "runtimeHelper", "node", "compiledCatalog", "sources", "budgets", "gatewayBudgets"}
HOST_STARTUP_INPUT_KEYS = {"kind", "version", "output", "runtimeEpoch", "seedBinding", "runtimeHelper", "hosting", "gateway", "proxy", "compiledCatalog", "budgets"}
HOST_STARTUP_BUDGETS = {"maximumSeconds": 300, "cleanupSeconds": 60, "normalRequests": 16, "cleanupRequests": 4}
HOST_NETWORK_CONFIGURATION = {"HttpServerPortNumber": 28497, "PublicPort": 28497,
    "ServerName": "Goby Core AV Original Client Host 01", "LocalNetworkAddresses": ["127.0.0.1"], "EnableHttps": False,
    "EnableUPnP": False, "EnableRemoteAccess": False, "EnableAutoUpdate": False, "EnableAutomaticRestart": False,
    "AutoRunWebApp": False}
HOST_UNIT_FIELDS = "Id LoadState ActiveState SubState MainPID InvocationID Result ExecMainStatus ControlGroup NRestarts".split()


class RunError(ValueError):
    pass


def need(value, code):
    if not value:
        raise RunError(code)


def descriptor(pin):
    need(isinstance(pin, dict) and set(pin) == {"path", "sha256"} and isinstance(pin["path"], str) and Path(pin["path"]).is_absolute() and
         ".." not in Path(pin["path"]).parts and isinstance(pin["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", pin["sha256"]), "descriptor_invalid")


def validate_input(value):
    need(set(value) == INPUT_KEYS and value["kind"] == "audited-candidate-client-run-input" and type(value["version"]) is int and value["version"] == 1 and
         value["scenario"] in SCENARIOS and re.fullmatch(r"[a-z0-9][a-z0-9-]{1,55}", value["runId"]), "client_run_input_invalid")
    for key in INPUT_KEYS - {"kind", "version", "runId", "scenario", "output", "sources", "budgets", "gatewayBudgets"}:
        descriptor(value[key])
    need(value["runtimeEpoch"] == EPOCH and value["seedBinding"] == BINDING and value["admission"] == ADMISSION and value["admissionCloseout"] == ADMISSION_CLOSEOUT and
         value["hosting"] == HOSTING and value["avVerification"] == AV_VERIFICATION and value["runtimeHelper"] == RUNTIME,
         "current_admission_authority_changed")
    need(Path(value["output"]) == R / ("candidate-core-client-" + value["runId"]) and value["budgets"] == BUDGETS and value["gatewayBudgets"] == GATEWAY_BUDGETS,
         "client_run_scope_or_budget_invalid")
    initialization = Path(value["hostingInitialization"]["path"])
    need(initialization.is_relative_to(R) and not initialization.is_relative_to(value["output"]), "hosting_initialization_scope_invalid")
    need(value["node"] == NODE and set(value["sources"]) == set(SOURCE_FILES), "client_runtime_source_membership")
    directory = Path(value["sources"]["closer"]["path"]).parent
    for key, filename in SOURCE_FILES.items():
        descriptor(value["sources"][key])
        path = Path(value["sources"][key]["path"])
        need(path.name == filename and path.is_relative_to(R) and not path.is_relative_to(value["output"]) and
             (not filename.endswith(".mjs") or path.parent == directory) and value["sources"][key]["sha256"] == FROZEN[key], "client_source_path_or_revision_changed")
    return value


def admitted(report, value, epoch):
    need(report["kind"] == "audited-candidate-live-admission" and report["version"] == 2 and report["status"] == "admitted_for_core_client" and
         report["candidateAdmissionComplete"] is True and report["failure"] is None and report["cleanupFailures"] == [] and
         report["runtimeEpoch"] == value["runtimeEpoch"] and report["seedRuntimeBinding"] == value["seedBinding"] and report["currentSource"] == epoch["currentSource"], "successful_current_admission_required")


def hosting_initialized(report, value, hosting, epoch, read_descriptor, read_bytes, tables):
    """Admit the explicit startup receipt using saved evidence only."""
    need(isinstance(report, dict) and report.get("kind") == "audited-original-client-host-startup" and type(report.get("version")) is int and
         report["version"] == 1 and report.get("status") == "ready_for_core_client" and report.get("failure") is None and
         report.get("cleanupFailures") == [] and report.get("wizardCompleted") is True, "hosting_initialization_not_complete")
    for key in ("input", "helper", "hosting", "runtimeEpoch", "seedBinding", "credentials", "sourceBefore", "sourceAfter"):
        descriptor(report[key])
    initialization = read_descriptor(report["input"])
    need(isinstance(initialization, dict) and set(initialization) == HOST_STARTUP_INPUT_KEYS and
         initialization["kind"] == "audited-original-client-host-startup-input" and type(initialization["version"]) is int and initialization["version"] == 1 and
         initialization["output"] == str(R / "client-host-startup-01") and initialization["budgets"] == HOST_STARTUP_BUDGETS and
         report["budgets"] == HOST_STARTUP_BUDGETS, "hosting_initialization_input_invalid")
    for key in HOST_STARTUP_INPUT_KEYS - {"kind", "version", "output", "budgets"}:
        descriptor(initialization[key])
    for key in ("hosting", "runtimeEpoch", "seedBinding"):
        need(report[key] == initialization[key] == value[key], "hosting_initialization_authority_changed")
    need(initialization["runtimeHelper"] == value["runtimeHelper"] == epoch["runtimeHelper"], "hosting_initialization_runtime_changed")
    for key in ("gateway", "proxy"):
        need(initialization[key]["sha256"] == FROZEN[key], "hosting_initialization_transport_changed")
    need(Path(report["helper"]["path"]).name == "initialize-audited-client-host.py" and
         Path(report["helper"]["path"]).is_relative_to(R), "hosting_initialization_helper_invalid")
    host = {"process": hosting["process"], "listener": hosting["listener"], "unit": {key: hosting["unitProperties"][key] for key in HOST_UNIT_FIELDS},
            "packageSha256": hosting["packageSha256"], "executableSha256": hosting["executableSha256"]}
    need(report["hostingBefore"] == report["hostingAfter"] == host, "hosting_initialization_host_changed")
    candidate = {**epoch["candidateProcess"], "listener": {"host": "127.0.0.1", "port": epoch["candidate"]["listener"]["port"],
                 "socketInode": epoch["candidate"]["listener"]["socketInode"]}}
    need(report["candidateBefore"] == report["candidateAfter"] == candidate and
         report["postgresBefore"] == report["postgresAfter"] == epoch["postgresProcess"] and
         report["leaseBefore"] == report["leaseAfter"] == epoch["lease"], "hosting_initialization_goby_runtime_changed")
    before, after = [read_descriptor(report[key]) for key in ("sourceBefore", "sourceAfter")]
    need(set(before) == set(after) == {"capturedAt", "tables", "sequences"} and len(tables) == 35 and
         set(before["tables"]) == set(after["tables"]) == set(tables) and before["tables"] == after["tables"] and
         before["sequences"] == after["sequences"], "hosting_initialization_goby_state_changed")
    need(report["preservation"] == {"ownedTablesExact": 35, "sequencesExact": True, "candidateContinuous": True,
         "postgresContinuous": True, "leaseExact": True, "hostingContinuous": True}, "hosting_initialization_preservation_incomplete")
    need(report["publicIdentity"] == {"id": hosting["serverId"], "version": hosting["version"], "serverName": hosting["serverName"]} and
         report["networkConfiguration"] == HOST_NETWORK_CONFIGURATION and
         all(type(report["networkConfiguration"][key]) is type(expected) for key, expected in HOST_NETWORK_CONFIGURATION.items()), "hosting_initialization_public_identity_changed")
    users = report["users"]
    need(set(users) == {"count", "adminId", "adminName"} and type(users["count"]) is int and users["count"] == 1 and
         re.fullmatch(r"[0-9a-f]{32}", users["adminId"]) and users["adminName"] == "goby-client-host-admin-01" and
         report["libraries"] == {"count": 0}, "hosting_initialization_admin_or_libraries_changed")
    web = report["webIndex"]
    need(set(web) == {"status", "location", "bodyRead", "response"} and web["status"] == 200 and web["location"] is None and
         web["bodyRead"] is False, "hosting_initialization_index_not_ready")
    response = read_descriptor(web["response"])
    need(set(response) == {"status", "headers", "headersComplete", "bodyRead", "bodyComplete", "rawHeaders"} and
         response["status"] == 200 and response["headersComplete"] is True and response["bodyRead"] is False and response["bodyComplete"] is None,
         "hosting_initialization_index_receipt_invalid")
    descriptor(response["rawHeaders"])
    raw = read_bytes(response["rawHeaders"])
    need(isinstance(raw, bytes) and len(raw) <= 65536 and raw.endswith(b"\r\n\r\n") and raw.count(b"\r\n\r\n") == 1,
         "hosting_initialization_index_headers_incomplete")
    status_line, headers = raw.split(b"\r\n", 1)
    need(re.fullmatch(rb"HTTP/1\.[01] 200(?: [^\r\n]*)?", status_line) and
         [list(row) for row in http.client.parse_headers(io.BytesIO(headers)).items()] == response["headers"] and
         not any(name.lower() == "location" for name, unused in response["headers"]), "hosting_initialization_index_redirect")
    cleanup = report["cleanup"]
    need(isinstance(cleanup, list) and len(cleanup) == 1 and re.fullmatch(r"[0-9a-f]{64}", cleanup[0]["tokenSha256"]), "hosting_initialization_cleanup_invalid")
    for field, expected in (("logoutStatus", 204), ("sameTokenStatus", 401)):
        receipt = read_descriptor(cleanup[0][field + "Response"])
        need(cleanup[0][field] == receipt["status"] == expected and receipt["complete"] is True, "hosting_initialization_token_not_closed")
    need(report["requests"] == {"normal": 11, "cleanup": 2} and type(report["elapsedMilliseconds"]) is int and
         0 <= report["elapsedMilliseconds"] <= 300000 and report["automaticWriteRetry"] is False and
         report["servicesStartedOrStopped"] == report["gobyBusinessHttpRequests"] == 0, "hosting_initialization_scope_exceeded")


def verify_actor_before(snapshot, binding, scenario):
    actor = binding["actors"][scenario]["id"]
    rows = [row for row in snapshot["tables"]["users"] if row["id"] == actor]
    need(len(rows) == 1 and rows[0]["is_administrator"] is False and rows[0]["is_disabled"] is False, "scenario_actor_not_ordinary")
    need(not any(row["user_id"] == actor for row in snapshot["tables"]["play_sessions"]) and
         not any(row["user_id"] == actor for row in snapshot["tables"]["client_playback_references"]), "scenario_actor_already_consumed")


def verify_signal_target(state, row, process, boot_id):
    need(int(row["MainPID"]) == int(row["ExecMainPID"]) == state["pid"] == process["pid"] and process["bootId"] == boot_id and
         process["uid"] == 0 and process["cmdline"] == state["command"] and process["cgroup"] == "0::/system.slice/" + state["unit"] + "\n" and
         (state.get("process") is None or process == state["process"]), "gateway_signal_identity_changed")


def unit_argv(unit, description, command, output, role, ssh_connection):
    properties = {"Description": description, "Type": "exec", "User": "root", "Group": "root", "UMask": "0077", "Restart": "no", "RemainAfterExit": "yes",
        "PrivateNetwork": "yes", "PrivateTmp": "yes", "ProtectSystem": "strict", "ProtectHome": "read-only", "NoNewPrivileges": "yes", "KillMode": "control-group",
        "TimeoutStopSec": "30", "RuntimeMaxSec": "1830" if role == "gateway" else "630", "MemoryMax": "512M" if role == "gateway" else "2G",
        "MemorySwapMax": "0", "TasksMax": "128" if role == "gateway" else "256", "CPUQuota": "100%", "ReadWritePaths": str(output / ("gateway" if role == "gateway" else "browser")),
        "StandardOutput": "append:" + str(output / "private" / (role + "-stdout.log")), "StandardError": "append:" + str(output / "private" / (role + "-stderr.log")),
        "UnsetEnvironment": "DEBUG PWDEBUG"}
    argv = ["/usr/bin/systemd-run", "--unit=" + unit, "--service-type=exec", "--working-directory=" + str(output)]
    argv += ["--property=" + key + "=" + val for key, val in properties.items()]
    argv += ["--setenv=SSH_CONNECTION=" + ssh_connection, "--setenv=PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright", "--setenv=HOME=/root", "--", *command]
    return argv, properties


class ClientRun:
    def __init__(self, runtime, value, input_pin, source_pin):
        self.r, self.value, self.input_pin, self.source_pin = runtime, validate_input(value), input_pin, source_pin
        self.started, self.stage, self.created = time.monotonic(), "input_preflight", False
        self.output, self.private = Path(value["output"]), Path(value["output"]) / "private"
        self.units = {role: "goby-core-client-" + value["runId"] + "-" + role + ".service" for role in ("gateway", "browser")}
        self.workers, self.failures = {}, []

    def remaining(self, cleanup=False):
        left = BUDGETS["maximumSeconds"] - (0 if cleanup else BUDGETS["cleanupSeconds"]) - (time.monotonic() - self.started)
        need(left > 0, "outer_client_deadline")
        return left

    def save(self, name, value):
        return self.s.write_json_once(self.private / name, value)

    def pin_file(self, path):
        raw = self.s.read_checked(path, self.hash_file(path))
        return {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}

    def hash_file(self, path):
        before = self.s.safe_path(path)
        need(before.st_nlink == 1 and before.st_size <= 32 << 20, "client_artifact_file_bound")
        with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
            raw = stream.read((32 << 20) + 1)
            need(self.s.file_identity(os.fstat(stream.fileno())) == self.s.file_identity(before), "client_artifact_changed")
        need(self.s.file_identity(Path(path).stat()) == self.s.file_identity(before), "client_artifact_replaced")
        return hashlib.sha256(raw).hexdigest()

    def preflight(self):
        need(not os.path.lexists(self.output), "client_output_collision")
        epoch = json.loads(self.r.read_bootstrap(self.value["runtimeEpoch"]))
        self.modules = {key: self.r.load_helper(key, pin) for key, pin in epoch["helpers"].items()}
        self.s = self.modules["seed"]
        need(self.s.descriptor(self.input_pin) == self.value and self.value["runtimeHelper"] == epoch["runtimeHelper"], "outer_input_or_runtime_helper_changed")
        self.epoch = self.r.validate_epoch(epoch)
        self.binding = self.s.descriptor(self.value["seedBinding"])
        self.r.validate_seed_runtime_binding(self.binding, self.value["runtimeEpoch"], epoch, self.s.descriptor(self.r.SEED))
        report = self.s.descriptor(self.value["admission"])
        admitted(report, self.value, epoch)
        self.s.descriptor(self.value["admissionCloseout"])
        verification = self.s.descriptor(self.value["avVerification"])
        need(verification["passed"] is True and verification["browserStarted"] is False and verification["businessHttp"] is False and
             verification["adapterCounts"] == {"tests": 28, "pass": 28, "fail": 0, "skipped": 0} and
             verification["closerCounts"] == {"testCount": 12, "passed": 12, "failed": 0, "sourceUnchanged": True} and
             all(verification["sourcePins"][filename] == FROZEN[key] for key, filename in SOURCE_FILES.items() if filename.endswith(".mjs")), "verified_client_sources_differ")
        for pin in [self.value["node"], *self.value["sources"].values()]:
            self.s.read_checked(pin["path"], pin["sha256"])
        self.gateway = self.r.load_helper("browser_gateway", self.value["sources"]["gateway"])
        self.hosting = self.s.descriptor(self.value["hosting"])
        need(self.hosting["kind"] == "core-av-original-client-hosting" and self.hosting["version"] == "4.9.5.0", "original_client_hosting_not_admitted")
        self.verify_hosting()
        hosting_initialized(self.s.descriptor(self.value["hostingInitialization"]), self.value, self.hosting, self.epoch,
                            self.s.descriptor, lambda pin: self.s.read_checked(pin["path"], pin["sha256"]), self.r.TABLES)
        self.catalog = self.s.descriptor(self.value["compiledCatalog"])
        source_manifest = self.s.descriptor(epoch["currentSource"]["sourceManifest"])
        self.modules["admission"].validate_catalog(self.catalog, source_manifest, self.value["compiledCatalog"])
        probe = self.modules["provision"].Provision({"runId": epoch["candidate"]["runId"], "ports": epoch["candidate"]["ports"]}, self.input_pin, epoch["helpers"]["provision"])
        for unit in self.units.values():
            need(probe.show(unit).get("LoadState") == "not-found" and not any(os.path.lexists(Path(root) / unit) for root in ("/run/systemd/transient", "/run/systemd/system", "/etc/systemd/system")), "client_unit_collision")

    def verify_hosting(self):
        host = self.hosting
        need(self.gateway.metadata(host["process"]["pid"]) == host["process"], "original_client_host_process_changed")
        self.gateway.verify_listener(host["process"]["pid"], host["listener"])
        self.s.read_checked(host["process"]["exe"], host["executableSha256"])

    def open(self):
        budget = {"maximumSeconds": 1200, "cleanupSeconds": 240, "maximumRequests": 2, "cleanupRequests": 1}
        self.io = self.r.EpochIO({"output": str(self.output), "budgets": budget}, self.input_pin, self.source_pin, self.value["runtimeEpoch"], self.value["seedBinding"], self.modules)
        try:
            self.io.open()
        finally:
            self.created = self.io.created
        self.p, self.reader = self.io.provision, self.io.reader
        for name in ("gateway", "browser", "closeout"):
            self.save("mkdir-" + name + "-intent.json", {"path": str(self.output / name), "mode": "0700"})
            os.mkdir(self.output / name, 0o700)
            self.s.sync_dir(self.output)
        for role in self.units:
            for stream in ("stdout", "stderr"):
                self.s.write_once(self.private / (role + "-" + stream + ".log"), b"")

    def source_sample(self, label):
        self.remaining(cleanup=label == "after")
        before = self.io.pin()
        postgres = self.modules["gateway"].metadata(self.epoch["postgresProcess"]["pid"])
        lease = self.reader.deployment_lease()
        need(postgres == self.epoch["postgresProcess"] and lease == self.epoch["lease"], "source_sample_epoch_changed")
        self.reader.assert_target_cluster()
        value = self.reader.sql_json(self.epoch["candidate"]["database"], self.modules["admission"].snapshot_sql(self.catalog))
        need(set(value) == {"capturedAt", "tables", "sequences"} and set(value["tables"]) == self.r.TABLES, "source_sample_inventory_invalid")
        need(self.io.pin() == before and self.reader.deployment_lease() == lease and self.modules["gateway"].metadata(postgres["pid"]) == postgres, "source_sample_changed_during_capture")
        pin = self.save("source-" + label + ".json", value)
        self.save("source-" + label + "-runtime.json", {"candidate": before, "postgres": postgres, "lease": lease})
        return value, pin, before, postgres, lease

    def show_worker(self, role):
        fields = "Id,LoadState,ActiveState,SubState,MainPID,ExecMainPID,InvocationID,Result,ExecMainStatus,ExecMainCode,ControlGroup,Description,ExecStart,Transient,User,PrivateNetwork,ProtectHome,Restart,RemainAfterExit"
        result = subprocess.run(["/usr/bin/systemctl", "show", self.units[role], "--property=" + fields], capture_output=True, timeout=10, check=True)
        need(len(result.stdout) <= 65536, "worker_metadata_bound")
        return dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)

    def own_worker(self, role, row):
        state = self.workers[role]
        need(row.get("Id") == self.units[role] and row.get("Transient") == "yes" and row.get("Description") == state["description"] and
             row.get("User") == "root" and row.get("Restart") == "no" and row.get("RemainAfterExit") == "yes" and
             row.get("PrivateNetwork") == "yes" and row.get("ProtectHome") == "read-only", "worker_ownership_changed")
        command = row["ExecStart"]
        need(command.count("argv[]=") == 1 and self.modules["provision"].shlex.split(command.split("argv[]=", 1)[1].split(";", 1)[0]) == state["command"], "worker_command_changed")
        if state.get("invocationId"):
            need(row["InvocationID"] == state["invocationId"], "worker_invocation_changed")

    def start_worker(self, role, command):
        self.remaining()
        description = "Owned core client " + self.input_pin["sha256"] + " " + role
        argv, properties = unit_argv(self.units[role], description, command, self.output, role, os.environ["SSH_CONNECTION"])
        self.workers[role] = {"unit": self.units[role], "description": description, "command": command, "startRequested": True, "stopRequested": False,
                              "startAcknowledged": False, "stopAcknowledged": False, "pid": None, "invocationId": None, "terminal": None, "closure": None}
        self.save(role + "-start-intent.json", {"unit": self.units[role], "argv": argv, "properties": properties})
        self.p.command("start-owned-" + role, argv)
        self.workers[role]["startAcknowledged"] = True
        row = self.show_worker(role)
        self.own_worker(role, row)
        state = self.workers[role]
        state.update(pid=int(row["ExecMainPID"]), invocationId=row["InvocationID"], controlGroup=row["ControlGroup"])
        need(state["pid"] > 1, "worker_pid_not_observed")
        if role == "gateway" and row["MainPID"] != "0":
            process = self.gateway.metadata(state["pid"])
            verify_signal_target(state, row, process, self.epoch["candidateProcess"]["bootId"])
            state["process"] = process
        self.save(role + "-started.json", row)
        return state

    def wait_gateway(self):
        deadline = time.monotonic() + BUDGETS["gatewayReadySeconds"]
        path = self.output / "gateway/gateway-attestation.json"
        while time.monotonic() < deadline:
            self.remaining()
            row = self.show_worker("gateway")
            self.own_worker("gateway", row)
            need(row["ActiveState"] == "active" and int(row["MainPID"]) == self.workers["gateway"]["pid"], "gateway_exited_before_ready")
            if path.exists():
                pin = self.pin_file(path)
                value = self.s.descriptor(pin)
                need(value["kind"] == "core-av-client-gateway" and value["runId"] == self.value["runId"] and value["inputSha256"] == self.gateway_input["sha256"] and
                     value["process"] == self.gateway.metadata(self.workers["gateway"]["pid"]) and value["upstreams"] == self.gateway_config["upstreams"] and value["sources"] == self.gateway_config["sources"], "gateway_attestation_binding")
                self.gateway.verify_listener(value["process"]["pid"], value["listener"])
                need(sorted(node.name for node in (self.output / "gateway").iterdir()) == ["gateway-attestation.json", "private"] and not list((self.output / "gateway/private").iterdir()), "gateway_ledger_not_fresh")
                return value, pin
            time.sleep(0.2)
        raise RunError("gateway_ready_deadline")

    def wait_client(self):
        deadline = time.monotonic() + BUDGETS["clientSeconds"] + BUDGETS["workerGraceSeconds"]
        while time.monotonic() < deadline:
            self.remaining()
            row = self.show_worker("browser")
            self.own_worker("browser", row)
            if row["MainPID"] == "0":
                self.record_terminal("browser", row)
                return
            need(row["ActiveState"] == "active", "browser_worker_state_invalid")
            time.sleep(1)
        raise RunError("browser_worker_deadline")

    def record_terminal(self, role, row):
        state = self.workers[role]
        need(row["MainPID"] == "0" and int(row["ExecMainPID"]) == state["pid"], "worker_terminal_identity_changed")
        if state["terminal"] is None:
            state["terminal"] = row
            self.save(role + "-terminal.json", row)

    def finish_gateway(self, row):
        """Observe natural exit before stopping the collectible transient unit."""
        state = self.workers["gateway"]
        if row["MainPID"] != "0":
            need(int(row["MainPID"]) == state["pid"], "gateway_shutdown_pid_changed")
            handle = os.pidfd_open(state["pid"])
            try:
                fresh = self.show_worker("gateway")
                self.own_worker("gateway", fresh)
                process = self.gateway.metadata(state["pid"])
                verify_signal_target(state, fresh, process, self.epoch["candidateProcess"]["bootId"])
                if hasattr(self, "gateway_attestation"):
                    need(process == self.gateway_attestation["process"], "gateway_shutdown_identity_changed")
                self.save("gateway-signal-intent.json", {"unit": self.units["gateway"], "invocationId": state["invocationId"], "process": process, "signal": "SIGTERM"})
                state["signalRequested"] = True
                signal.pidfd_send_signal(handle, signal.SIGTERM)
                self.save("gateway-signal-result.json", {"signalAcknowledged": True, "workerPid": state["pid"]})
            finally:
                os.close(handle)
            deadline = time.monotonic() + 25
            while time.monotonic() < deadline:
                self.remaining(cleanup=True)
                row = self.show_worker("gateway")
                self.own_worker("gateway", row)
                if row["MainPID"] == "0":
                    break
                time.sleep(0.2)
        self.record_terminal("gateway", row)

    def close_worker(self, role):
        state = self.workers.get(role)
        if not state or state["stopRequested"]:
            return
        self.remaining(cleanup=True)
        row = self.show_worker(role)
        self.own_worker(role, row)
        if not state.get("pid"):
            state.update(pid=int(row["ExecMainPID"]), invocationId=row["InvocationID"], controlGroup=row["ControlGroup"])
        if role == "gateway":
            try:
                self.finish_gateway(row)
            except Exception as error:
                self.failures.append({"stage": "gateway_graceful_exit", "code": str(error) if isinstance(error, RunError) else "gateway_exit_not_observed", "errorType": type(error).__name__})
        elif row["MainPID"] == "0":
            self.record_terminal(role, row)
        self.save(role + "-stop-intent.json", {"unit": self.units[role], "observed": row})
        state["stopRequested"] = True
        self.p.command("stop-owned-" + role, ["/usr/bin/systemctl", "stop", self.units[role]])
        state["stopAcknowledged"] = True
        final = self.show_worker(role)
        if final["LoadState"] != "not-found":
            self.own_worker(role, final)
        need(state["controlGroup"] == "/system.slice/" + self.units[role], "worker_cgroup_changed")
        group = Path("/sys/fs/cgroup") / state["controlGroup"].lstrip("/")
        pids = sorted({int(pid) for path in ([group / "cgroup.procs", *group.rglob("cgroup.procs")] if group.exists() else []) if path.exists() for pid in path.read_text().split()})
        absent = state["pid"] is not None and state["pid"] > 1 and not Path("/proc", str(state["pid"])).exists()
        state["closure"] = {"unit": final, "workerPid": state["pid"], "workerPidAbsent": absent, "recursiveCgroupPids": pids}
        self.save(role + "-closed.json", state["closure"])
        need(final.get("MainPID") == "0" and absent and not pids, "owned_worker_not_fully_closed")

    def gateway_exit(self):
        state = self.workers["gateway"]
        terminal = state["terminal"]
        need(terminal is not None and terminal["LoadState"] != "not-found" and terminal["Result"] == "success" and terminal["ExecMainStatus"] == "0", "gateway_exit_status_not_observed")
        return int(terminal["ExecMainStatus"])

    def run(self):
        self.preflight()
        self.open()
        self.stage = "source_before"
        before, self.before_pin, self.candidate_before, self.postgres_before, self.lease_before = self.source_sample("before")
        verify_actor_before(before, self.binding, self.value["scenario"])
        self.before_ns = str(time.clock_gettime_ns(time.CLOCK_MONOTONIC))
        candidate_process = {key: self.candidate_before[key] for key in self.gateway.PROCESS_FIELDS}
        self.gateway_config = {"schemaVersion": 1, "runId": self.value["runId"], "proxyOrigin": self.epoch["candidate"]["publicUrl"], "browserOrigin": self.epoch["candidate"]["publicUrl"],
            "directOrigin": self.epoch["candidate"]["directUrl"], "ledgerRoot": str(self.output / "gateway"), "sources": {key: self.value["sources"][key] for key in ("gateway", "proxy")},
            "upstreams": {"goby": {"process": candidate_process, "listener": self.candidate_before["listener"], "executableSha256": self.epoch["currentSource"]["binary"]["sha256"]},
                          "reference": {"process": self.hosting["process"], "listener": self.hosting["listener"], "executableSha256": self.hosting["executableSha256"]}}, "budgets": GATEWAY_BUDGETS}
        self.gateway_input = self.save("gateway-input.json", self.gateway_config)
        try:
            self.stage = "gateway_start"
            self.start_worker("gateway", ["/usr/bin/python3", "-I", "-B", self.value["sources"]["gateway"]["path"], "--input", self.gateway_input["path"], "--input-sha256", self.gateway_input["sha256"]])
            self.gateway_attestation, self.gateway_pin = self.wait_gateway()
            actor = self.binding["actors"][self.value["scenario"]]
            source = {"manifestSha256": self.epoch["currentSource"]["sourceManifest"]["sha256"], "binarySha256": self.epoch["currentSource"]["binary"]["sha256"], "schema": 28}
            approval = self.save("internal-admission.json", {"kind": "audited-candidate-client-approval", "runId": self.value["runId"], "scenario": self.value["scenario"],
                "sourceManifestSha256": source["manifestSha256"], "binarySha256": source["binarySha256"], "serverId": self.binding["serverId"], "isolatedCandidate": True,
                "authorizedRunInput": self.input_pin, "admission": self.value["admission"], "admissionCloseout": self.value["admissionCloseout"]})
            manifest = {"kind": "audited-candidate-client-input", "version": 1, "runId": self.value["runId"], "scenario": self.value["scenario"],
                "clientUrl": self.gateway_config["browserOrigin"] + "/web/index.html", "browserOrigin": self.gateway_config["browserOrigin"], "directOrigin": self.gateway_config["directOrigin"],
                "serverId": self.binding["serverId"], "actor": {"id": actor["id"], "username": actor["username"]}, "credentials": actor["credentials"], "source": source,
                "processes": {"candidate": self.candidate_before, "gateway": {**self.gateway_attestation["process"], "listener": self.gateway_attestation["listener"]}},
                "catalog": self.binding["catalog"], "budgets": {"maximumSeconds": 600, "cleanupSeconds": 120}, "output": str(self.output / "browser"), "approval": approval, "gatewayAttestation": self.gateway_pin}
            self.manifest_pin = self.save("adapter-input.json", manifest)
            self.stage = "browser_start"
            need(self.io.pin() == self.candidate_before, "candidate_changed_before_browser")
            self.start_worker("browser", ["/usr/bin/nsenter", "-t", str(self.gateway_attestation["process"]["pid"]), "-n", "--", self.value["node"]["path"],
                self.value["sources"]["adapter"]["path"], "--manifest", self.manifest_pin["path"], "--manifest-sha256", self.manifest_pin["sha256"]])
            self.stage = "browser_running"
            self.wait_client()
        except Exception as error:
            self.failures.append({"stage": self.stage, "code": str(error) if isinstance(error, RunError) else "client_worker_operation_failed", "errorType": type(error).__name__})
        finally:
            for role in ("browser", "gateway"):
                try:
                    self.close_worker(role)
                except Exception as error:
                    self.failures.append({"stage": "close_" + role, "code": str(error) if isinstance(error, RunError) else "worker_close_failed", "errorType": type(error).__name__})
        self.stage = "source_after"
        after, after_pin, candidate_after, postgres_after, lease_after = self.source_sample("after")
        self.verify_hosting()
        self.after_ns = str(time.clock_gettime_ns(time.CLOCK_MONOTONIC))
        need(not self.failures, "worker_responsibility_unclosed")
        client = self.workers["browser"]
        need(client["terminal"]["Result"] == "success" and client["terminal"]["ExecMainStatus"] == "0", "browser_worker_failed")
        gateway_exit = self.gateway_exit()
        index_pin = self.pin_file(self.output / "gateway/index.json")
        index = self.s.descriptor(index_pin)
        need(index["complete"] is True and index["webMediaAndWebSocketBodiesRetained"] is False, "gateway_ledger_incomplete")
        boundary = {"kind": "audited-candidate-client-boundary", "version": 1, "runId": self.value["runId"], "runtimeEpoch": self.value["runtimeEpoch"],
            "sourceBefore": self.before_pin, "sourceAfter": after_pin, "candidateBefore": self.candidate_before, "candidateAfter": candidate_after,
            "postgresBefore": self.postgres_before, "postgresAfter": postgres_after, "leaseBefore": self.lease_before, "leaseAfter": lease_after,
            "database": self.epoch["candidate"]["database"], "beforeMonotonicNs": self.before_ns, "afterMonotonicNs": self.after_ns,
            "clientWorker": {"exitCode": int(client["terminal"]["ExecMainStatus"]), "mainPID": int(client["closure"]["unit"]["MainPID"]), "workerPidAbsent": client["closure"]["workerPidAbsent"], "remainingBrowserPids": client["closure"]["recursiveCgroupPids"]},
            "gatewayWorker": {"exitCode": gateway_exit, "mainPID": int(self.workers["gateway"]["closure"]["unit"]["MainPID"]), "workerPidAbsent": self.workers["gateway"]["closure"]["workerPidAbsent"], "index": index_pin}}
        boundary_pin = self.save("boundary.json", boundary)
        closeout = {"kind": "audited-candidate-client-closeout-input", "version": 1, "manifest": self.manifest_pin,
            "observation": self.pin_file(self.output / "browser/observation.json"), "summary": self.pin_file(self.output / "browser/summary.json"), "gatewayAttestation": self.gateway_pin,
            "gatewayIndex": index_pin, "runtimeEpoch": self.value["runtimeEpoch"], "admission": self.value["admission"], "seedBinding": self.value["seedBinding"],
            "sourceBefore": self.before_pin, "sourceAfter": after_pin, "boundary": boundary_pin, "sources": self.value["sources"], "output": str(self.output / "closeout")}
        closeout_input = self.save("closeout-input.json", closeout)
        self.stage = "offline_closeout"
        self.remaining(cleanup=True)
        self.p.command("offline-client-closeout", ["/usr/bin/env", "SSH_CONNECTION=" + os.environ["SSH_CONNECTION"], "HOME=/root", self.value["node"]["path"],
            self.value["sources"]["closer"]["path"], "--input", closeout_input["path"], "--input-sha256", closeout_input["sha256"]])
        summary_pin = self.pin_file(self.output / "closeout/summary.json")
        summary = self.s.descriptor(summary_pin)
        need(summary["status"] == "core_scenario_closed", "offline_closeout_incomplete")
        return {"status": "core_scenario_closed", "runId": self.value["runId"], "scenario": self.value["scenario"], "closeout": summary_pin, "boundary": boundary_pin,
                "controllerBusinessHttpRequests": 0, "candidateRestarted": False, "postgresRestarted": False}


def main():
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode, "isolated_root_ssh_required")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    for key in ("input", "input-sha256", "source-sha256", "runtime-helper", "runtime-helper-sha256"):
        parser.add_argument("--" + key, required=True)
    args = parser.parse_args()
    runtime_pin = {"path": args.runtime_helper, "sha256": args.runtime_helper_sha256}
    need(runtime_pin == RUNTIME, "runtime_source_not_frozen")
    import importlib.util
    spec = importlib.util.spec_from_file_location("core_client_runtime", args.runtime_helper)
    runtime = importlib.util.module_from_spec(spec)
    raw = Path(args.runtime_helper).read_bytes()
    need(hashlib.sha256(raw).hexdigest() == args.runtime_helper_sha256, "runtime_source_changed")
    exec(compile(raw, args.runtime_helper, "exec"), runtime.__dict__)
    runtime.read_bootstrap(runtime_pin)
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    runtime.read_bootstrap(source_pin)
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    value = json.loads(runtime.read_bootstrap(input_pin))
    need(value["runtimeHelper"] == runtime_pin, "input_runtime_helper_changed")
    job = ClientRun(runtime, value, input_pin, source_pin)
    def expired(unused_signal, unused_frame):
        raise RunError("outer_client_absolute_deadline")
    for number in (signal.SIGALRM, signal.SIGTERM, signal.SIGINT):
        signal.signal(number, expired)
    signal.setitimer(signal.ITIMER_REAL, BUDGETS["maximumSeconds"])
    try:
        result = job.run()
        if job.created:
            result["report"] = job.save("report.json", result)
        print(json.dumps(result))
        return 0
    except Exception as error:
        result = {"status": "core_client_run_incomplete", "stage": job.stage, "code": str(error) if isinstance(error, RunError) else "outer_client_operation_failed",
            "errorType": type(error).__name__, "runId": value["runId"], "scenario": value["scenario"], "failures": job.failures, "workers": job.workers,
            "automaticBusinessRetry": False, "controllerBusinessHttpRequests": 0, "receipt": None}
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
