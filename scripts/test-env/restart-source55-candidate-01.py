#!/usr/bin/env python3
"""Start the failed source55 candidate once after approved client closure.

Run remotely as root with Python -I -B and --input-sha256. The precreated
candidate-source55-restart-01/input.json must bind this source, the completed
independent client closure and its reviewed field values, and the matrix07
service preservation record. This operator never upgrades, resets, stops,
retries a start, changes fixture ownership, or acquires an application DB lease.
"""

from __future__ import annotations

import argparse
import base64
from copy import deepcopy
from datetime import datetime, timezone
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import types

W = Path("/opt/goby-test/exec-work-m3e")
ROOT = W / "candidate-source55-restart-01"
INPUT = ROOT / "input.json"
UNIT = "goby-client-m3e.service"
UNIT_FILE = Path("/etc/systemd/system") / UNIT
BINARY = Path("/opt/goby-client-m3e/goby")
RUNTIME = W / "runtime.env"
FIXTURE = W / "client-fixture.json"
READER = W / "prepare-client-fixture.py"
LOCK = W / "client-fixture.lock"
DATA = Path("/var/lib/goby-test/client-m3e")
MEDIA = Path("/opt/goby-fixtures/client-m3e")
BASELINE = W / "reference-nextup-global-matrix-execution-07/goby-reconciled-after.json"
SERVICES_BASELINE = W / "reference-nextup-global-matrix-execution-07/preservation-after.json"
REFERENCE_EXECUTION = W / "reference-nextup-global-matrix-attestation-07/published-execution.json"
CLIENT_EXECUTION = W / "reference-nextup-client-discovery-execution-01"
CLIENT_UNIT = "goby-nextup-client-discovery-01.service"
PINS = {
    UNIT_FILE: "ee7a5f0c9912fb1a109beebadcaf3e6d0734705831685f83f64160dd2195061c",
    BINARY: "6a8c46cdd0dcff56af28f11084eabcf2497daf5ce11ac072eaad7a5dbf486e81",
    RUNTIME: "1f245cd8f8c19dbe0b96b803dc8c60dd0cd4b7b7e2a8d99ccc2541e9430c4a7b",
    FIXTURE: "bb78a846d2d4b69b7e2550ed9e367549cbe2d0b763e2b69270ea98bed6570d82",
    READER: "84d21e8ac0b48c5dfd3d2c7ae35b0aec0d7f4f811e65658081490aa44947b49c",
    BASELINE: "1bb63300fb52b6c22213e004e370b39f17f97785bfdecc43caeca85879ee3b25",
    REFERENCE_EXECUTION: "586860e8109ee3b9d21f13126a9d5b715f02c2c95d7a6d115c060eab7b234766",
}
OLD_PID = 1458051
OLD_INVOCATION = "d29ea64c63274346a79c7b3a7938f36e"
SERVICE_FIELDS = {"Id", "LoadState", "ActiveState", "SubState", "MainPID", "Result", "ExecMainStatus", "InvocationID", "ControlGroup"}
CANDIDATE_FIELDS = SERVICE_FIELDS | {"ExecMainCode", "FragmentPath", "DropInPaths", "User", "Group", "ExecStart",
    "EnvironmentFiles", "WorkingDirectory", "Restart", "MemoryMax", "TasksMax", "NRestarts"}
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
LEASE_SQL = """SELECT COALESCE(jsonb_agg(jsonb_build_object(
 'pid',l.pid,'granted',l.granted,'mode',l.mode,'usename',a.usename,
 'backend_start',a.backend_start,'client_addr',a.client_addr,'client_port',a.client_port,
 'state',a.state) ORDER BY l.pid,l.granted),'[]'::jsonb)
FROM pg_locks l LEFT JOIN pg_stat_activity a ON a.pid=l.pid
WHERE l.locktype='advisory' AND l.database=(SELECT oid FROM pg_database WHERE datname='goby_client_m3e')
AND l.classid=(4919415424202458201::bigint >> 32)::oid
AND l.objid=(4919415424202458201::bigint & 4294967295)::oid AND l.objsubid=1;"""


class RestartError(ValueError):
    """A named admission, start, readiness, or preservation check failed."""


def require(condition, check):
    if not condition:
        raise RestartError(check)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def safe_json(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False) + "\n").encode()


def canonical_path(path, *, directory=False):
    require(path.is_absolute() and ".." not in path.parts, "absolute-normalized-path")
    for item in (path, *path.parents):
        info = item.lstat()
        require(not stat.S_ISLNK(info.st_mode), "path-symlink")
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "path-type")
    return info


def read(path, checksum=None, maximum=256 << 20):
    info = canonical_path(path)
    require(info.st_size <= maximum and not info.st_mode & 0o022, "input-size-mode")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(identity(os.fstat(stream.fileno())) == identity(info), "input-open-race")
        raw = stream.read(maximum + 1)
        require(identity(os.fstat(stream.fileno())) == identity(info), "input-read-race")
    require(identity(path.lstat()) == identity(info) and len(raw) <= maximum, "input-after-read-race")
    require(checksum is None or digest(raw) == checksum, "input-sha256")
    return raw


def publish(name, value, encoder=safe_json):
    require(re.fullmatch(r"[a-z0-9-]+\.json", name) is not None, "output-name")
    path, raw = ROOT / name, encoder(value)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    parent = os.open(ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)
    return {"path": str(path), "sha256": digest(raw)}


def command(argv, *, text=None, timeout=10, postgres=False):
    environment = dict(ENV)
    if postgres:
        environment["PGOPTIONS"] = "-c default_transaction_read_only=on -c statement_timeout=20000 -c lock_timeout=5000"
    result = subprocess.run(argv, input=text, text=True, capture_output=True, check=False, env=environment, timeout=timeout)
    require(result.returncode == 0, "bounded-command-failed")
    return result.stdout.strip()


def show(unit, fields=SERVICE_FIELDS, *, timeout=10):
    require(unit in (UNIT, CLIENT_UNIT, "goby-foundation-test.service", "goby-emby-client-m3e.service"), "unit-query-scope")
    output = command(["/usr/bin/systemctl", "show", unit, "--no-pager", "--property=" + ",".join(sorted(fields))], timeout=timeout)
    value = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
    require(set(value) == fields, "unit-property-membership")
    return value


def empty_cgroup(unit):
    root = Path("/sys/fs/cgroup/system.slice") / unit
    if not root.exists():
        return True
    paths = [root, *[path for path in root.rglob("*") if path.is_dir()]]
    require(len(paths) <= 64, "cgroup-bound")
    return all(not (path / "cgroup.procs").read_text().strip() for path in paths)


def metadata(pid):
    require(type(pid) is int and pid > 1, "process-pid")
    root = Path("/proc") / str(pid)
    raw = (root / "stat").read_text()
    fields = raw[raw.rfind(")") + 2:].split()
    exe = (root / "exe").stat()
    value = {"pid": pid, "startTicks": fields[19], "bootId": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
             "uid": root.stat().st_uid, "exe": os.readlink(root / "exe"), "exeDevice": exe.st_dev, "exeInode": exe.st_ino,
             "cmdline": [part.decode() for part in (root / "cmdline").read_bytes().split(b"\0") if part],
             "networkNamespace": os.readlink(root / "ns/net"), "cgroup": (root / "cgroup").read_text()}
    after = (root / "stat").read_text()
    require(after[after.rfind(")") + 2:].split()[19] == value["startTicks"], "process-start-race")
    return value


def socket_owned(pid, local_port, remote_port=None, expected_inode=None):
    root = Path("/proc") / str(pid)
    inodes = set()
    for path in (root / "fd").iterdir():
        try:
            value = os.readlink(path)
        except FileNotFoundError:
            continue
        if value.startswith("socket:["):
            inodes.add(value[8:-1])
    rows = [line.split() for line in (root / "net/tcp").read_text().splitlines()[1:]]
    return any(row[1] == "0100007F:" + format(local_port, "04X") and row[9] in inodes and
               (expected_inode is None or row[9] == expected_inode) and
               (row[3] == "0A" if remote_port is None else row[3] == "01" and row[2] == "0100007F:" + format(remote_port, "04X")) for row in rows)


def candidate_properties(value, *, failed):
    expected = {"Id": UNIT, "LoadState": "loaded", "FragmentPath": str(UNIT_FILE), "DropInPaths": "", "User": "goby", "Group": "goby",
                "EnvironmentFiles": str(RUNTIME) + " (ignore_errors=no)", "WorkingDirectory": str(DATA), "Restart": "no",
                "MemoryMax": "536870912", "TasksMax": "96", "NRestarts": "0"}
    require(all(value.get(key) == item for key, item in expected.items()), "candidate-unit-config")
    require(value["ExecStart"].count("argv[]=") == 1 and
            value["ExecStart"].split("argv[]=", 1)[1].split(";", 1)[0].strip() == str(BINARY), "candidate-execstart")
    if failed:
        require(all(value[key] == item for key, item in {"ActiveState": "failed", "SubState": "failed", "MainPID": "0",
                "Result": "exit-code", "ExecMainStatus": "1", "InvocationID": OLD_INVOCATION}.items()) and
                value["ExecMainCode"] in ("1", "exited") and empty_cgroup(UNIT) and not Path("/proc", str(OLD_PID)).exists(), "candidate-failed-baseline")
    else:
        require(value["ActiveState"] == "active" and value["SubState"] == "running" and int(value["MainPID"]) > 1 and
                value["InvocationID"] != OLD_INVOCATION and re.fullmatch(r"[0-9a-f]{32}", value["InvocationID"]) and
                value["ControlGroup"] == "/system.slice/" + UNIT, "candidate-new-running-state")


def candidate_metadata(service, fixture):
    process = metadata(int(service["MainPID"]))
    binary = canonical_path(BINARY)
    require(process["uid"] == 995 and process["exe"] == str(BINARY) and process["cmdline"] == [str(BINARY)] and
            process["cgroup"] == "0::/system.slice/" + UNIT + "\n" and process["bootId"] == fixture["process"]["boot_id"] and
            (process["exeDevice"], process["exeInode"]) == (binary.st_dev, binary.st_ino), "new-candidate-process-binding")
    return process


def file_tree(root):
    result, total = {}, 0
    paths = [root, *sorted(root.rglob("*"))]
    require(len(paths) <= 4096, "filesystem-entry-bound")
    for path in paths:
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode) and (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)), "filesystem-entry-type")
        value = {"device": info.st_dev, "inode": info.st_ino, "mode": info.st_mode, "uid": info.st_uid, "gid": info.st_gid,
                 "links": info.st_nlink, "sizeBytes": info.st_size, "mtimeNs": info.st_mtime_ns, "ctimeNs": info.st_ctime_ns}
        if stat.S_ISREG(info.st_mode):
            total += info.st_size
            require(total <= 512 << 20, "filesystem-byte-bound")
            value["sha256"] = digest(read(path, maximum=128 << 20))
        require(identity(path.lstat()) == identity(info), "filesystem-entry-race")
        result[str(path.relative_to(root))] = value
    return result


def stable_files(value):
    result = deepcopy(value)
    for name in list(result["data"]):
        if name.startswith("diagnostics/"):
            del result["data"][name]
        elif name == "diagnostics":
            for key in ("sizeBytes", "mtimeNs", "ctimeNs", "links"):
                result["data"][name].pop(key)
    return result


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode, "remote-root-isolated-no-bytecode")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-sha256", required=True)
    options = parser.parse_args()
    require(re.fullmatch(r"[0-9a-f]{64}", options.input_sha256), "input-sha256-format")
    root_info = canonical_path(ROOT, directory=True)
    require(root_info.st_uid == 0 and stat.S_IMODE(root_info.st_mode) == 0o700, "private-output-root")
    require({path.name for path in ROOT.iterdir()} == {"input.json", "restart-source55-candidate.py"}, "fresh-output-membership")
    input_value = json.loads(read(INPUT, options.input_sha256))
    require(set(input_value) == {"schemaVersion", "kind", "source", "clientClosure", "clientClosureExpected", "clientCleanupIndependentlyVerified",
            "clientPreservationBefore", "clientPreservationAfter", "servicePreservation"} and
            input_value["schemaVersion"] == 1 and input_value["kind"] == "candidate-source55-restart-input" and
            input_value["clientCleanupIndependentlyVerified"] is True, "approved-input-contract")
    self_path = Path(__file__).absolute()
    require(input_value["source"]["path"] == str(self_path) and self_path == ROOT / "restart-source55-candidate.py", "frozen-helper-path")
    read(self_path, input_value["source"]["sha256"])
    closure_path = Path(input_value["clientClosure"]["path"])
    require(closure_path.parent == CLIENT_EXECUTION and "independent" in closure_path.name, "independent-client-closure-path")
    closure = json.loads(read(closure_path, input_value["clientClosure"]["sha256"]))
    require(closure.get("kind") == "nextup-client-discovery-independent-terminal" and closure.get("version") == 1 and
            closure.get("status") in ("client_closed_global_request_unobserved", "client_discovery_evidence_reconstructed") and
            closure.get("failure") is None and closure.get("clientDiscoveryEvidenceVerified") is True and
            closure.get("uiLoginAndLogoutDualChannelBound") is True and closure.get("exactUIToken401MetadataVerified") is True and
            closure.get("resumeOrRetryAllowed") is False and closure.get("unit", {}).get("name") == CLIENT_UNIT and
            closure["unit"].get("formerPidAbsent") is True and closure["unit"].get("cgroupAbsent") is True and
            not Path("/proc", str(closure["unit"]["formerPid"])).exists(), "client-independent-cleanup-minimum")
    expected_closure = input_value["clientClosureExpected"]
    require(isinstance(expected_closure, dict) and expected_closure and all(closure.get(key) == value for key, value in expected_closure.items()), "approved-client-closure-fields")
    client_preservation = {}
    for phase in ("before", "after"):
        descriptor = input_value["clientPreservation" + phase.title()]
        require(descriptor["path"] == str(CLIENT_EXECUTION / ("preservation-" + phase + ".json")), "client-preservation-path")
        value = json.loads(read(Path(descriptor["path"]), descriptor["sha256"]))
        require(value["kind"] == "nextup-client-preservation" and value["schemaVersion"] == 1 and value["phase"] == phase and
                value["rootCount"] == len(value["roots"]) == 192 and value["historical186RootsEqual"] is True and
                value["completeGobyStateEqualExceptCaptureTime"] is True, "client-preservation-contract")
        client_preservation[phase] = value
    require(all(client_preservation["before"][key] == client_preservation["after"][key] for key in
                ("previous", "clientInput", "clientUnitFile", "roots", "rootCount", "services", "mainFiles", "gobyCounts")), "client-complete-before-after-preservation")
    require(input_value["servicePreservation"]["path"] == str(SERVICES_BASELINE), "protected-service-baseline-path")
    service_baseline = json.loads(read(SERVICES_BASELINE, input_value["servicePreservation"]["sha256"]))
    for path, checksum in PINS.items():
        read(path, checksum)
    op = types.ModuleType("source55_restart_preservation_reader")
    op.__file__ = str(READER)
    exec(compile(read(READER, PINS[READER]), str(READER), "exec"), op.__dict__)

    def readonly_command(arguments, text=None, environment=None, timeout=30):
        argv = [str(item) for item in arguments]
        expected = ["/usr/sbin/runuser", "-u", "postgres", "--", "/usr/lib/postgresql/17/bin/psql", "-X", "--no-password",
                    "-h", "/var/lib/postgresql/goby-workspace-v1/socket", "-p", "15432", "-U", "postgres", "-d"]
        require(argv[:len(expected)] == expected and argv[len(expected)] in ("postgres", "goby_client_m3e") and
                argv[len(expected) + 1:] == ["-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"] and
                isinstance(text, str) and environment is None and 0 < timeout <= 20, "readonly-postgres-command")
        return command(argv, text=text, timeout=timeout, postgres=True)

    op.command = readonly_command
    fixture = op.precise_json(read(FIXTURE, PINS[FIXTURE]))
    require(fixture["schema"] == 28 and fixture["binary_sha256"] == PINS[BINARY] and fixture["phase"] == "ready" and
            fixture["process"]["pid"] == OLD_PID and fixture["upgrade"]["phase"] == "complete" and
            not any(fixture.get(key) for key in ("start_pending", "directory_pending", "credentials_pending")), "fixture-source55-ownership")
    baseline = op.precise_json(read(BASELINE, PINS[BASELINE]))

    def stable(snapshot):
        value = deepcopy(snapshot)
        value["database"]["metadata"].pop("captured_at")
        return op.canonical_json(value)

    for phase, record in client_preservation.items():
        descriptor = record["gobySnapshot"]
        require(descriptor["path"] == str(CLIENT_EXECUTION / ("goby-" + phase + ".json")), "client-goby-snapshot-path")
        require(stable(op.precise_json(read(Path(descriptor["path"]), descriptor["sha256"]))) == stable(baseline), "client-goby-full-snapshot-preservation")

    def snapshot(name):
        value = op.preservation_snapshot(fixture, 28)
        record = publish(name, value, lambda item: op.canonical_json(item) + b"\n")
        return value, record

    reference_execution = json.loads(read(REFERENCE_EXECUTION, PINS[REFERENCE_EXECUTION]))
    protected_units = ("goby-foundation-test.service", "goby-emby-client-m3e.service")

    def protected_runtime():
        services = {unit: show(unit) for unit in protected_units}
        require(all(services[unit] == service_baseline["services"][unit] for unit in protected_units), "protected-services-unchanged")
        process = reference_execution["process"]
        raw = {name: metadata(process[name]["pid"]) for name in ("application", "endpoint")}
        raw["endpoint"]["listener"] = process["endpoint"]["listener"]
        raw["workerNetworkNamespace"] = os.readlink("/proc/self/ns/net")
        bound = deepcopy(raw)
        if bound["endpoint"]["exe"] == process["endpoint"]["exe"] + " (deleted)":
            require(Path("/proc", str(process["endpoint"]["pid"]), "exe").stat().st_nlink == 0, "deleted-proxy-file-object")
            bound["endpoint"]["exe"] = process["endpoint"]["exe"]
        require(bound == process and socket_owned(process["endpoint"]["pid"], reference_execution["endpoint"]["port"],
                expected_inode=process["endpoint"]["listener"]["socketInode"]), "reference-proxy-binding")
        return {"services": services, "primary": metadata(int(services[protected_units[0]]["MainPID"])), "reference": raw}

    lock_info = canonical_path(LOCK)
    require(lock_info.st_uid == 0 and lock_info.st_nlink == 1 and stat.S_IMODE(lock_info.st_mode) == 0o600, "existing-client-lock")
    lock_fd = os.open(LOCK, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    start_attempted, probes, records, stage = False, [], {}, "acquire-client-fixture-lock"
    terminal = {"schemaVersion": 1, "kind": "candidate-source55-restart-terminal", "status": "failed_before_start",
                "inputSha256": options.input_sha256, "source": input_value["source"], "clientClosure": input_value["clientClosure"],
                "clientPreservation": {phase: input_value["clientPreservation" + phase.title()] for phase in ("before", "after")},
                "clientAcceptanceClaim": False, "upgradePerformed": False, "retryAllowed": False}
    try:
        require(identity(os.fstat(lock_fd)) == identity(lock_info), "client-lock-open-race")
        fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        stage = "preconditions"
        client = show(CLIENT_UNIT)
        require(all(client[key] == value for key, value in {"MainPID": "0", "LoadState": "loaded", "ActiveState": "active",
                "SubState": "exited", "Result": "success", "ExecMainStatus": "0", "ControlGroup": "",
                "InvocationID": closure["unit"]["invocationId"]}.items()) and empty_cgroup(CLIENT_UNIT), "client-discovery-unit-closed")
        old_service = show(UNIT, CANDIDATE_FIELDS)
        candidate_properties(old_service, failed=True)
        host_sockets = [line.split() for table in ("tcp", "tcp6")
                        for line in Path("/proc/self/net", table).read_text().splitlines()[1:]]
        require(not any(row[1].endswith(":" + format(18198, "04X")) and row[3] == "0A" for row in host_sockets), "candidate-port-already-owned")
        protected_before = protected_runtime()
        leases_before = op.precise_json(op.postgres(LEASE_SQL))
        require(leases_before == [], "old-candidate-database-lease-present")
        before, records["databaseBefore"] = snapshot("goby-before.json")
        require(stable(before) == stable(baseline), "complete-goby-baseline-differs")
        tables = before["database"]["tables"]
        require(len(tables["scan_jobs"]) == 5 and all(row["status"] == "completed" for row in tables["scan_jobs"]) and
                all(tables[name] == [] for name in ("task_occurrences", "task_run_children", "task_run_requests", "task_runs", "task_triggers", "encoding_jobs")), "no-pending-job-precondition")
        files_before = {"data": file_tree(DATA), "media": file_tree(MEDIA)}
        records["filesBefore"] = publish("files-before.json", files_before)
        stage = "persist-start-intent"
        require(protected_runtime() == protected_before and show(UNIT, CANDIDATE_FIELDS) == old_service and
                identity(os.fstat(lock_fd)) == identity(LOCK.lstat()) == identity(lock_info), "pre-start-runtime-and-lock-stability")
        for path, checksum in PINS.items():
            read(path, checksum)
        records["intent"] = publish("start-intent.json", {"kind": "candidate-source55-single-start-intent", "schemaVersion": 1,
            "createdAt": datetime.now(timezone.utc).isoformat(), "inputSha256": options.input_sha256, "source": input_value["source"],
            "clientClosure": input_value["clientClosure"], "command": ["/usr/bin/systemctl", "--no-block", "start", UNIT],
            "oldService": old_service, "oldFixtureProcess": fixture["process"], "protectedRuntime": protected_before,
            "filePins": {str(path): checksum for path, checksum in PINS.items()}, "records": deepcopy(records),
            "lockIdentity": list(identity(lock_info)), "databaseLeaseBefore": leases_before, "maximumReadySeconds": 60,
            "maximumStartCalls": 1, "fixtureOwnershipFileUpdated": False})
        stage = "single-start"
        deadline = time.monotonic() + 60
        start_attempted = True
        started = subprocess.run(["/usr/bin/systemctl", "--no-block", "start", UNIT], capture_output=True, check=False, timeout=10, env=ENV)
        records["startResult"] = publish("start-result.json", {"returncode": started.returncode, "stdoutSha256": digest(started.stdout), "stderrSha256": digest(started.stderr)})
        require(started.returncode == 0, "single-start-not-acknowledged")
        stage = "bounded-readiness"
        ready = False
        new_anchor = None
        for attempt in range(60):
            if time.monotonic() >= deadline:
                break
            service = show(UNIT, CANDIDATE_FIELDS, timeout=min(2, deadline - time.monotonic()))
            require(service["ActiveState"] != "failed", "candidate-start-failed")
            if service["ActiveState"] == "active" and service["SubState"] == "running":
                candidate_properties(service, failed=False)
                current_process = candidate_metadata(service, fixture)
                anchor = {"process": current_process, "invocationId": service["InvocationID"]}
                require(new_anchor is None or anchor == new_anchor, "single-start-process-anchor-changed")
                new_anchor = anchor
                if not socket_owned(current_process["pid"], 18198):
                    time.sleep(min(1, max(0, deadline - time.monotonic())))
                    continue
                ready = True
                for route in ("/healthz", "/readyz", "/emby/System/Info/Public"):
                    require(time.monotonic() < deadline, "readiness-deadline")
                    require(metadata(current_process["pid"]) == current_process and socket_owned(current_process["pid"], 18198), "pre-http-candidate-binding")
                    connection = http.client.HTTPConnection("127.0.0.1", 18198, timeout=min(2, deadline - time.monotonic()))
                    response, captured = None, False
                    try:
                        connection.request("GET", route, headers={"Accept": "application/json", "Connection": "close"})
                        response = connection.getresponse()
                        raw = response.read(65537)
                        headers = response.getheaders()
                        probes.append({"attempt": attempt + 1, "route": route, "status": response.status, "headers": headers,
                                       "bodyBase64": base64.b64encode(raw).decode(), "completedAt": datetime.now(timezone.utc).isoformat()})
                        captured = True
                        require(len(raw) <= 65536 and not any(key.lower() == "set-cookie" for key, value in headers), "public-probe-bound-cookie")
                        body = json.loads(raw)
                        valid = response.status == 200 and (body == {"Status": "ok"} if route == "/healthz" else
                            body == {"Status": "ready"} if route == "/readyz" else body.get("Id") == fixture["server_id"])
                        ready = ready and valid
                    except (OSError, http.client.HTTPException, json.JSONDecodeError) as error:
                        ready = False
                        if captured:
                            probes[-1]["failure"] = type(error).__name__
                        else:
                            partial = error.partial[:65537] if isinstance(error, http.client.IncompleteRead) else b""
                            probes.append({"attempt": attempt + 1, "route": route, "status": None if response is None else response.status,
                                "headers": [] if response is None else response.getheaders(), "failure": type(error).__name__,
                                "partialBodyBase64": base64.b64encode(partial).decode()})
                    finally:
                        connection.close()
                    require(metadata(current_process["pid"]) == current_process, "post-http-candidate-binding")
                    if not ready:
                        break
                if ready:
                    break
            time.sleep(min(1, max(0, deadline - time.monotonic())))
        records["probes"] = publish("public-probes.json", probes)
        require(ready and time.monotonic() <= deadline, "candidate-ready-timeout")
        stage = "preservation-after-start"
        new_service = show(UNIT, CANDIDATE_FIELDS)
        candidate_properties(new_service, failed=False)
        new_process = candidate_metadata(new_service, fixture)
        require(new_anchor == {"process": new_process, "invocationId": new_service["InvocationID"]}, "ready-candidate-anchor")
        binary_info = canonical_path(BINARY)
        require(new_process["uid"] == 995 and new_process["exe"] == str(BINARY) and new_process["cmdline"] == [str(BINARY)] and
                new_process["cgroup"] == "0::/system.slice/" + UNIT + "\n" and
                (new_process["exeDevice"], new_process["exeInode"]) == (binary_info.st_dev, binary_info.st_ino) and
                socket_owned(new_process["pid"], 18198), "new-candidate-process-and-listener")
        executable = Path("/proc", str(new_process["pid"]), "exe")
        with executable.open("rb") as stream:
            require((os.fstat(stream.fileno()).st_dev, os.fstat(stream.fileno()).st_ino) == (binary_info.st_dev, binary_info.st_ino), "candidate-executable-open")
            new_executable_sha = digest(stream.read((256 << 20) + 1))
        require(new_executable_sha == PINS[BINARY], "new-running-source55-bytes")
        leases_after = op.precise_json(op.postgres(LEASE_SQL))
        require(len(leases_after) == 1 and leases_after[0]["granted"] is True and leases_after[0]["mode"] == "ExclusiveLock" and
                leases_after[0]["usename"] == "goby_client_m3e" and leases_after[0]["client_addr"] == "127.0.0.1" and
                socket_owned(new_process["pid"], leases_after[0]["client_port"], 15432), "new-candidate-owned-database-lease")
        after, records["databaseAfter"] = snapshot("goby-after.json")
        files_after = {"data": file_tree(DATA), "media": file_tree(MEDIA)}
        records["filesAfter"] = publish("files-after.json", files_after)
        require(stable(after) == stable(before) and stable_files(files_after) == stable_files(files_before), "complete-post-start-preservation")
        require(protected_runtime() == protected_before and metadata(new_process["pid"]) == new_process and
                show(UNIT, CANDIDATE_FIELDS) == new_service and identity(os.fstat(lock_fd)) == identity(LOCK.lstat()) == identity(lock_info), "post-start-runtime-and-lock-stability")
        for path, checksum in PINS.items():
            read(path, checksum)
        terminal.update(status="source55_candidate_restored", source55Ready=True, oldService=old_service, newService=new_service,
            oldFixtureProcess=fixture["process"], newProcess={**new_process, "executableSha256": new_executable_sha},
            databaseLeaseBefore=leases_before, databaseLeaseAfter=leases_after, protectedRuntimeUnchanged=True,
            fullDatabasePreservedExceptCaptureTime=True, recoveryBytesPreserved=True, mediaAndDataPreservedExceptDiagnostics=True,
            fixtureOwnershipFileUpdated=False, applicationLeaseAcquiredByHelper=False)
    except BaseException as error:
        terminal.update(status="recovery_required" if start_attempted else "failed_before_start", errorType=type(error).__name__,
                        failedStage=stage, failedCheck=str(error) if isinstance(error, RestartError) else "bounded-operation-failed")
    finally:
        if start_attempted:
            retained_errors = []
            if "probes" not in records:
                try:
                    records["probes"] = publish("public-probes.json", probes)
                except BaseException as error:
                    retained_errors.append({"phase": "retain-probes", "errorType": type(error).__name__})
            if "databaseAfter" not in records:
                try:
                    unused_after, records["databaseAfter"] = snapshot("goby-after.json")
                except BaseException as error:
                    retained_errors.append({"phase": "retain-database-after", "errorType": type(error).__name__})
            if "filesAfter" not in records:
                try:
                    records["filesAfter"] = publish("files-after.json", {"data": file_tree(DATA), "media": file_tree(MEDIA)})
                except BaseException as error:
                    retained_errors.append({"phase": "retain-files-after", "errorType": type(error).__name__})
            if terminal["status"] != "source55_candidate_restored":
                try:
                    terminal["observedServiceAfterFailure"] = show(UNIT, CANDIDATE_FIELDS)
                    terminal["observedProtectedRuntimeAfterFailure"] = protected_runtime()
                except BaseException as error:
                    retained_errors.append({"phase": "retain-runtime-after", "errorType": type(error).__name__})
            terminal["retentionErrors"] = retained_errors
        os.close(lock_fd)
        terminal.update(startCalls=int(start_attempted), publicHttpRequests=len(probes), records=records,
                        completedAt=datetime.now(timezone.utc).isoformat())
        receipt = publish("terminal.json", terminal)
        print(json.dumps({"status": terminal["status"], "startCalls": terminal["startCalls"], "terminal": receipt, "upgradePerformed": False}))
    return 0 if terminal["status"] == "source55_candidate_restored" else 2


if __name__ == "__main__":
    raise SystemExit(main())
