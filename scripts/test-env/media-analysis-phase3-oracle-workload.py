"""Owned RPC control around the existing real Phase 3 Actor.

No workload is started without an independently released, hash-bound binding.
This wrapper does not replace Actor HTTP, SQL, process, or source observations.
"""
import argparse
import concurrent.futures
import copy
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pwd
import re
import signal
import socket
import stat
import subprocess
import sys
import threading
import time


MAX_JSON = 16 << 20
MAX_CONTROL = 65536
SAFE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}\Z")


def need(value, code):
    if not value:
        raise RuntimeError(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def decode(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            need(key not in result, "duplicate_json_key")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs,
                      parse_constant=lambda _: (_ for _ in ()).throw(RuntimeError("nonfinite_json")))


def read(path, maximum=MAX_JSON, uid=0, private=True):
    path = Path(path)
    need(path.is_absolute() and path.resolve(strict=True) == path, "noncanonical_file")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == uid and before.st_nlink == 1
             and (not private or stat.S_IMODE(before.st_mode) == 0o600)
             and 0 < before.st_size <= maximum, "file_identity")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            raw = stream.read(maximum + 1)
        after = os.fstat(fd)
        need((before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) ==
             (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns)
             and len(raw) == before.st_size, "file_changed")
        return raw
    finally:
        os.close(fd)


def pinned(reference, uid=0, private=True):
    need(set(reference) == {"path", "sha256"}, "pin_fields")
    raw = read(reference["path"], uid=uid, private=private)
    need(hashlib.sha256(raw).hexdigest() == reference["sha256"], "pin_changed")
    return raw


def save(path, value, replace=False):
    raw = canonical(value) + b"\n"
    need(len(raw) <= MAX_JSON, "record_budget")
    path = Path(path)
    stage = path.with_name(path.name + ".next") if replace else path
    fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(raw); stream.flush(); os.fsync(stream.fileno())
    if replace:
        os.replace(stage, path)
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    return {"path": str(path), "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}


def process(pid):
    raw = (Path("/proc") / str(pid) / "stat").read_text()
    fields = raw.rsplit(")", 1)[1].split()
    return {"pid": pid, "start_ticks": int(fields[19]),
            "cgroup": (Path("/proc") / str(pid) / "cgroup").read_text().strip()}


def load_binding(path):
    raw = read(path)
    value = decode(raw)
    required = {"version", "released", "run_id", "owner_id", "source_revision", "profile_id", "tier", "scenario_id", "guest",
                "private_directory", "actor_uid", "actor_gid", "actor_groups", "unit", "output_directory", "phase",
                "actor", "manifest", "context", "scan_configuration", "systemd_run", "systemctl", "python", "self_sha256",
                "unit_memory_bytes", "unit_runtime_seconds", "settle_seconds"}
    need(set(value) == required and value["version"] == 1 and value["released"] is True, "workload_not_released")
    for key in ("run_id", "owner_id", "scenario_id"):
        need(SAFE.fullmatch(value[key]), "workload_scope_id")
    need(value["guest"]["vmid"] == 106 and Path("/etc/machine-id").read_text().strip() == value["guest"]["machine_id"]
         and Path("/sys/class/dmi/id/product_uuid").read_text().strip().lower() == value["guest"]["smbios_uuid"],
         "workload_guest_identity")
    need(re.fullmatch(r"goby-phase3-workload-[a-z0-9-]{1,64}\.service", value["unit"]), "workload_unit_scope")
    need(type(value["actor_uid"]) is int and 0 < value["actor_uid"] < 65535 and
         pwd.getpwuid(value["actor_uid"]).pw_gid == value["actor_gid"], "unprivileged_actor_identity")
    need(isinstance(value["actor_groups"], list) and len(value["actor_groups"]) <= 8 and
         all(type(group) is int and 0 < group < 65535 for group in value["actor_groups"]), "actor_supplementary_groups")
    need(value["phase"] == "cached", "fault_workload_phase")
    need(128 << 20 <= value["unit_memory_bytes"] <= 2 << 30 and 30 <= value["unit_runtime_seconds"] <= 7200
         and 5 <= value["settle_seconds"] <= 90, "workload_control_budget")
    for key in ("private_directory", "output_directory"):
        candidate = Path(value[key])
        need(candidate.is_absolute() and ".." not in candidate.parts and
             str(candidate).startswith(("/opt/goby-phase3/", "/var/lib/goby-phase3/")), "workload_directory_scope")
    need(hashlib.sha256(Path(__file__).read_bytes()).hexdigest() == value["self_sha256"], "workload_adapter_changed")
    return value, hashlib.sha256(raw).hexdigest()


def system(binding, arguments):
    pinned(binding["systemctl"], private=False)
    done = subprocess.run([binding["systemctl"]["path"], *arguments], stdin=subprocess.DEVNULL,
                          capture_output=True, timeout=15, check=False)
    need(len(done.stdout) + len(done.stderr) <= MAX_CONTROL and done.returncode == 0, "owned_unit_query_failed")
    return done.stdout.decode()


def unit_state(binding):
    raw = system(binding, ["show", binding["unit"], "--property=ActiveState,SubState,MainPID,ControlGroup,Result"])
    values = dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)
    need(values["ControlGroup"] in ("", "/system.slice/" + binding["unit"]), "workload_cgroup_changed")
    members = []
    if values["ControlGroup"]:
        path = Path("/sys/fs/cgroup") / values["ControlGroup"].lstrip("/") / "cgroup.procs"
        try:
            raw_members = path.read_bytes()
        except FileNotFoundError:
            raw_members = b""
        need(len(raw_members) <= 65536, "workload_cgroup_inventory_bound")
        members = [int(value) for value in raw_members.splitlines()]
        need(len(members) <= 128 and all(value > 1 for value in members), "workload_cgroup_population")
    return {**values, "boot_id": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
            "cgroup_pids": members, "observed_unix_ns": time.time_ns()}


class Session:
    def __init__(self, binding, binding_sha, actor_module, manifest, context):
        self.b, self.sha, self.mod = binding, binding_sha, actor_module
        self.evidence = actor_module.Evidence(binding["output_directory"], manifest)
        self.actor = actor_module.Actor(manifest, context, self.evidence, request_scope=binding_sha)
        self.actor.phase = binding["phase"]
        self.stop = threading.Event()
        self.phase_errors = []
        self.anchor = {"monotonic_ns": time.monotonic_ns(), "unix_ns": time.time_ns(),
                       "boot_id": Path("/proc/sys/kernel/random/boot_id").read_text().strip()}
        # Read the exact-byte private copy without widening permissions on the
        # deployed root-owned configuration. The broker still verifies its
        # actual deployment path and hash from the unchanged Actor context.
        configuration_raw = pinned(binding["scan_configuration"], uid=binding["actor_uid"])
        need(len(configuration_raw) <= 1 << 20 and
             binding["scan_configuration"]["sha256"] == context["scan_evidence_sha256"],
             "scan_configuration_deployment_hash")
        configuration = actor_module.strict_json(configuration_raw)
        need(configuration["enabled"] is True, "scan_evidence_required")
        self.actor.spool_path = Path(configuration["directory"])
        self.observer = threading.Thread(target=self.actor.observer, name="real-actor-observer")
        self.worker = threading.Thread(target=self.run, name="real-actor-phase")

    def run(self):
        try:
            self.actor.run_phase(self.b["phase"])
        except BaseException as error:
            self.phase_errors.append(type(error).__name__ + ":" + str(error)[:256])
        finally:
            self.stop.set()

    def facts(self):
        # Materialize the Actor's actual HTTP/SQL samples and /proc CPU/FD
        # observations. The controller recomputes overlap from these records.
        with self.actor.lock, self.evidence.lock:
            samples = [{"phase": phase, "kind": kind, "identity": identity, "observations": list(rows)}
                       for (phase, kind, identity), rows in self.actor.samples.items()]
            value = {"version": 1, "binding_sha256": self.sha, "run_id": self.b["run_id"],
                     "owner_id": self.b["owner_id"], "scenario_id": self.b["scenario_id"],
                     "source_revision": self.b["source_revision"], "phase": self.b["phase"],
                     "process": process(os.getpid()), "anchor": self.anchor,
                     "observed_monotonic_ns": time.monotonic_ns(), "observed_unix_ns": time.time_ns(),
                     "lanes": dict(self.actor.lanes), "samples": samples, "process_observations": dict(self.actor.procs),
                     "events": list(self.evidence.events), "resources": list(self.actor.resources),
                     "jobs": [{"id": identity, **record} for identity, record in self.actor.jobs.items()],
                     "plays": list(self.actor.plays), "phase_errors": list(self.phase_errors),
                     "observer_errors": list(self.actor.failures), "admissions_stopped": self.actor.stop.is_set(),
                     "worker_alive": self.worker.is_alive(), "observer_alive": self.observer.is_alive(),
                     "settled": not self.worker.is_alive() and not self.observer.is_alive()}
            value = copy.deepcopy(value)
        return value

    def settle(self):
        # Do not run Actor.cleanup(): restoring acknowledged settings/metadata
        # here would change the very durable state the recovery oracle compares.
        self.actor.stop.set()
        self.worker.join(timeout=self.b["settle_seconds"])
        self.observer.join(timeout=5)
        need(not self.worker.is_alive() and not self.observer.is_alive(), "actor_threads_not_joined")
        # A changed application/PG lifetime cannot inherit the old Actor's
        # credentials-to-process binding. Its old admissions are instead
        # checked in external DB snapshots by the recovery oracle.
        def alive(pid, birth):
            try:
                return process(pid)["start_ticks"] == birth
            except FileNotFoundError:
                return False
        original_alive = alive(self.actor.c["app_pid"], self.actor.c["app_start_ticks"])
        pg = self.actor.c["postgres"]
        original_pg_alive = alive(pg["postmaster_pid"], pg["postmaster_start_ticks"])
        if not original_alive or not original_pg_alive:
            self.evidence.event("settle_changed_service_lifetime", phase=self.b["phase"])
            return self.facts()
        self.actor.assert_owned()
        self.actor.cleanup_mode = True
        self.evidence.closing = True
        self.actor.phase_deadline = None
        self.actor.deadline = time.monotonic() + self.b["settle_seconds"]
        for identity, job in list(self.actor.jobs.items()):
            if job["active"]:
                path = "/admin/v1/jobs/" + identity + "/cancel" if job["kind"] == "scan" else "/admin/v1/task-runs/" + identity + "/cancel"
                self.actor.native(path, "POST", {}, (200, 202, 204, 409))
        self.actor.drain_jobs()
        self.actor.close_playback()
        self.actor.drain_playback()
        need(not self.actor.intents and not self.actor.plays and not self.actor.closed_plays
             and not any(job["active"] for job in self.actor.jobs.values()), "owned_admissions_not_settled")
        return self.facts()

    def serve(self):
        root = self.evidence.directory
        save(root / "session-start.json", {"binding_sha256": self.sha, "anchor": self.anchor,
                                            "process": process(os.getpid())})
        socket_path = root / "control.sock"
        need(len(os.fsencode(socket_path)) < 104 and not socket_path.exists(), "control_socket_scope")
        listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        listener.bind(str(socket_path)); os.chmod(socket_path, 0o600); listener.listen(1); listener.settimeout(.5)
        self.observer.start(); self.worker.start()
        finished = False
        try:
            while not finished and time.monotonic() < self.actor.deadline:
                try:
                    connection, _ = listener.accept()
                except socket.timeout:
                    continue
                with connection:
                    connection.settimeout(self.b["settle_seconds"] + 10)
                    raw = bytearray()
                    while b"\n" not in raw:
                        chunk = connection.recv(4096)
                        need(chunk and len(raw) + len(chunk) <= MAX_CONTROL, "control_request_bound")
                        raw.extend(chunk)
                    request = decode(raw)
                    need(set(request) == {"binding_sha256", "op"} and request["binding_sha256"] == self.sha
                         and request["op"] in {"checkpoint", "settle", "close"}, "control_request_scope")
                    try:
                        value = self.facts() if request["op"] == "checkpoint" else self.settle()
                        response = {"ok": True, "result": value}
                        finished = request["op"] == "close"
                    except BaseException as error:
                        response = {"ok": False, "error": str(error)[:256]}
                    connection.sendall(canonical(response) + b"\n")
        finally:
            self.actor.stop.set()
            self.worker.join(timeout=self.b["settle_seconds"])
            self.observer.join(timeout=5)
            save(root / "session-terminal.json", self.facts())
            listener.close()
            socket_path.unlink()
        need(not self.worker.is_alive() and not self.observer.is_alive(), "actor_exit_with_unjoined_work")


def daemon(binding, binding_sha):
    raw = pinned(binding["actor"], private=False)
    module = type(sys)("pinned_phase3_real_actor")
    module.__file__ = binding["actor"]["path"]
    exec(compile(raw, module.__file__, "exec"), module.__dict__)
    manifest = module.validate_manifest(decode(pinned(binding["manifest"], uid=binding["actor_uid"])))
    context = decode(pinned(binding["context"], uid=binding["actor_uid"]))
    need(context["manifest_sha256"] == binding["manifest"]["sha256"] and
         context["driver_sha256"] == binding["actor"]["sha256"], "actor_pins_mismatch")
    need(all(manifest[key] == binding[key] for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier")), "actor_scope_mismatch")
    parent = Path(binding["output_directory"]).parent
    info = parent.lstat()
    need(stat.S_ISDIR(info.st_mode) and info.st_uid == binding["actor_uid"] and stat.S_IMODE(info.st_mode) == 0o700,
         "actor_output_parent_identity")
    os.setgroups(binding["actor_groups"]); os.setgid(binding["actor_gid"]); os.setuid(binding["actor_uid"])
    Session(binding, binding_sha, module, manifest, context).serve()


def request_session(binding, binding_sha, operation):
    root = Path(binding["output_directory"])
    state = unit_state(binding)
    if not (root / "session-start.json").exists() or not (root / "control.sock").exists() and int(state["MainPID"]) != 0:
        need(operation == "checkpoint" and state["ActiveState"] in {"activating", "active"}, "actor_not_ready_for_mutation")
        return {"pending": True, "unit_state": state}
    info = root.lstat()
    need(root.resolve(strict=True) == root and info.st_uid == binding["actor_uid"] and stat.S_IMODE(info.st_mode) == 0o700,
         "actor_output_identity")
    start = decode(read(root / "session-start.json", uid=binding["actor_uid"]))
    if int(state["MainPID"]) == 0:
        # A reboot may kill the actor without a terminal file. That is not a
        # successful join; preserve the distinct, independently observed fact.
        if state["boot_id"] != start["anchor"]["boot_id"]:
            return {"terminated_by_guest_restart": True, "settled": False, "unit_state": state,
                    "original_session_start": start, "process": None}
        return {"unit_state": state, "terminal": decode(read(root / "session-terminal.json", uid=binding["actor_uid"]))}
    need(process(int(state["MainPID"])) == start["process"], "actor_process_lifetime_changed")
    client = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        client.settimeout(binding["settle_seconds"] + 10)
        client.connect(str(root / "control.sock"))
        client.sendall(canonical({"binding_sha256": binding_sha, "op": operation}) + b"\n")
        raw = bytearray()
        while b"\n" not in raw:
            chunk = client.recv(65536)
            need(chunk and len(raw) + len(chunk) <= MAX_JSON, "actor_response_bound")
            raw.extend(chunk)
        reply = decode(raw)
        need(reply.get("ok") is True, "actor_control_failed")
        return {**reply["result"], "unit_state": state}
    finally:
        client.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binding", required=True)
    parser.add_argument("--daemon", action="store_true")
    args = parser.parse_args()
    request_id = None
    try:
        need(sys.platform == "linux" and os.geteuid() == 0, "owned_linux_guest_control_required")
        binding, binding_sha = load_binding(args.binding)
        if args.daemon:
            daemon(binding, binding_sha)
            return 0
        raw = sys.stdin.buffer.read(MAX_CONTROL + 1)
        need(len(raw) <= MAX_CONTROL, "request_bound")
        request = decode(raw)
        need(set(request) == {"version", "request_id", "op", "scenario_id"} and request["version"] == 1
             and request["scenario_id"] == binding["scenario_id"] and SAFE.fullmatch(request["request_id"]), "request_scope")
        request_id = request["request_id"]
        operation = request["op"]
        need(operation in {"start", "checkpoint", "settle", "close"}, "workload_operation")
        private = Path(binding["private_directory"])
        marker = private / ("workload-" + binding["scenario_id"] + "-start-intent.json")
        if operation == "start":
            need(not marker.exists() and not Path(binding["output_directory"]).exists(), "workload_already_dispatched")
            need(int(unit_state(binding)["MainPID"]) == 0, "workload_unit_already_live")
            for key in ("systemd_run", "python"):
                pinned(binding[key], private=False)
            save(marker, {"request_id": request_id, "binding_sha256": binding_sha, "observed_unix_ns": time.time_ns()})
            argv = [binding["systemd_run"]["path"], "--quiet", "--unit", binding["unit"],
                    "--property=MemoryMax=" + str(binding["unit_memory_bytes"]), "--property=MemorySwapMax=0",
                    "--property=CPUQuota=200%", "--property=TasksMax=128", "--property=KillMode=control-group",
                    "--property=NoNewPrivileges=yes",
                    "--property=RuntimeMaxSec=" + str(binding["unit_runtime_seconds"]), "--property=TimeoutStopSec=30",
                    "--", binding["python"]["path"], "-I", "-B", str(Path(__file__).resolve()),
                    "--binding", str(Path(args.binding).resolve()), "--daemon"]
            result = subprocess.run(argv, stdin=subprocess.DEVNULL, capture_output=True, timeout=20, check=False)
            need(result.returncode == 0 and len(result.stdout) + len(result.stderr) <= MAX_CONTROL, "workload_dispatch_failed")
            data = {"dispatches": 1, "unit_state": unit_state(binding), "binding_sha256": binding_sha}
        else:
            data = request_session(binding, binding_sha, operation)
        data.update({key: binding[key] for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier", "scenario_id")})
        data["binding_sha256"] = binding_sha
        artifact = save(private / (hashlib.sha256(request_id.encode()).hexdigest() + "-workload.json"), data)
        reply = {"version": 1, "request_id": request_id, "ok": True, "result": {"private_artifact": artifact,
                  "operation": operation, "dispatches": 1 if operation != "checkpoint" else 0}}
    except BaseException as error:
        reply = {"version": 1, "request_id": request_id, "ok": False, "error": str(error)[:256], "automatic_retry": False}
    sys.stdout.buffer.write(canonical(reply) + b"\n")
    return 0 if reply["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
