#!/usr/bin/env python3
"""Provision one fresh audited candidate; leave bootstrap and acceptance pending."""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import hashlib
import http.client
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shlex
import signal
import socket
import stat
import subprocess
import sys
import time
from urllib.parse import urlsplit

ARCHIVE = "74247d7584cb750645474deb2a4fc564f5944a32cd9875b2644209d65fbc89df"
PG = Path("/usr/lib/postgresql/17/bin")
RESERVED = {5432, 15432, 18096, 18196, 18197, 18198}
FORBIDDEN = {"dispose-source41-resource-full-failed-pair.py", "test-dispose-source41-resource-full-failed-pair.py", "upgrade-main-schema25.py"}
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
PROTECTED = ("goby-client-m3e.service", "goby-foundation-test.service", "postgresql@17-main.service")
INACCESSIBLE = ("/opt/goby-test", "/opt/goby-dev", "/opt/goby-client-m3e", "/opt/goby-fixtures", "/var/lib/goby-test", "/var/lib/postgresql")
SOFTWARE_ENV = {"GOBY_TRANSCODING_ENABLED": "true", "GOBY_HW_DECODER": "software", "GOBY_HW_ENCODER": "software", "GOBY_HW_DEVICE": "",
                "GOBY_TRANSCODE_THREADS": "2", "GOBY_TRANSCODE_MAX_JOBS": "2", "GOBY_TRANSCODE_MAX_USER_JOBS": "1", "GOBY_TRANSCODE_MAX_SESSION_JOBS": "1",
                "GOBY_TRANSCODE_MAX_QUEUE_JOBS": "16", "GOBY_TRANSCODE_MAX_RETAINED_JOBS": "128", "GOBY_TRANSCODE_MAX_CACHE_BYTES": str(2 << 30),
                "GOBY_TRANSCODE_MAX_JOB_BYTES": str(1 << 30), "GOBY_TRANSCODE_MIN_FREE_BYTES": str(512 << 20), "GOBY_TRANSCODE_MAX_BITRATE": "20000000",
                "GOBY_TRANSCODE_MAX_WIDTH": "1920", "GOBY_TRANSCODE_MAX_HEIGHT": "1080", "GOBY_TRANSCODE_MAX_AUDIO_CHANNELS": "8"}


def need(value, message):
    if not value:
        raise ValueError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + "\n").encode()


def parse(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            need(key not in result, "Duplicate JSON key.")
            result[key] = value
        return result
    return json.loads(raw.decode("utf-8"), object_pairs_hook=pairs,
                      parse_constant=lambda value: (_ for _ in ()).throw(ValueError("Nonfinite JSON.")))


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def owned(path, uid=0, directory=False):
    path = Path(path)
    need(path.is_absolute() and ".." not in path.parts and not any(c in str(path) for c in "\n\r\x00"), "Invalid authority path.")
    for node in (path, *path.parents):
        info = node.lstat()
        need(not stat.S_ISLNK(info.st_mode) and not info.st_mode & 0o022, "Unsafe authority ancestry.")
        need(info.st_uid == (uid if node == path else 0), "Unexpected authority owner.")
        need(stat.S_ISDIR(info.st_mode) if node != path or directory else stat.S_ISREG(info.st_mode), "Unexpected authority type.")
    return path.lstat()


def read(path, checksum=None, size=None):
    path = Path(path)
    need(path.name not in FORBIDDEN, "A prohibited historical script cannot be read.")
    before = owned(path)
    need(before.st_nlink == 1 and before.st_size <= 256 << 20, "Invalid authority file bound.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        need(identity(os.fstat(stream.fileno())) == identity(before), "Authority changed during open.")
        raw = stream.read((256 << 20) + 1)
        need(identity(os.fstat(stream.fileno())) == identity(before), "Authority changed during read.")
    need(identity(path.lstat()) == identity(before) and len(raw) == before.st_size, "Authority changed.")
    need(checksum is None or digest(raw) == checksum, "Authority digest differs.")
    need(size is None or type(size) is int and size == len(raw), "Authority byte count differs.")
    return raw


def descriptor(row):
    need(isinstance(row, dict) and set(row) == {"path", "sha256"} and re.fullmatch(r"[0-9a-f]{64}", row["sha256"]), "Invalid descriptor.")
    return parse(read(row["path"], row["sha256"]))


def relative(name):
    need(isinstance(name, str) and name and not Path(name).is_absolute() and ".." not in Path(name).parts and
         "\\" not in name and not any(c in name for c in "\n\r\x00"), "Invalid relative manifest path.")
    need(Path(name).name not in FORBIDDEN, "A prohibited script is present in the source closure.")
    return Path(name)


def file_spec(row):
    need(set(row) == {"sha256", "bytes"} and isinstance(row["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", row["sha256"]) and
         type(row["bytes"]) is int and 0 <= row["bytes"] <= 256 << 20, "Invalid manifest file facts.")
    return row


def validate_input(value):
    need(set(value) == {"kind", "version", "runId", "backendReport", "sourceManifest", "frontendReport", "ports", "public_url", "transcodingProfile"} and
         value["kind"] == "audited-candidate-provision-input" and type(value["version"]) is int and value["version"] == 1,
         "Unexpected provision contract.")
    need(value["transcodingProfile"] == "software-baseline-v1", "The admitted candidate requires its explicit enabled software engine profile.")
    need(re.fullmatch(r"[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}", value["runId"]), "Invalid fresh run ID.")
    need(set(value["ports"]) == {"postgres", "http"} and len(set(value["ports"].values())) == 2 and
         all(type(port) is int and 1024 <= port <= 65535 and port not in RESERVED for port in value["ports"].values()), "Unsafe candidate ports.")
    public = urlsplit(value["public_url"])
    need(public.scheme == "http" and public.hostname == "127.0.0.1" and public.port is not None and
         value["public_url"] == f"http://127.0.0.1:{public.port}" and 1024 <= public.port <= 65535 and
         public.port not in RESERVED | set(value["ports"].values()), "The new public gateway origin must be distinct from both provisioned ports and preserved services.")
    return value


def verify_products(value):
    report = descriptor(value["backendReport"])
    worker = report["worker"]
    need(report["status"] == worker["status"] == "passed" and report["mode"] == worker["mode"] == "full" and
         report["archive_sha256"] == ARCHIVE and report["unit_exit_code"] == 0 and report["recursive_cgroup_empty"] is True and
         report["existing_services_modified"] is False, "Only the completed frozen full backend verification is admitted.")
    cleanup = {"only_worker_process_remains", "owned_postgres_stopped", "private_bind_removed", "source_unchanged"}
    need(set(worker["cleanup"]) == cleanup and all(worker["cleanup"][key] is True for key in cleanup), "Backend cleanup is incomplete.")
    packages = worker["packages"]
    need(len(worker["expected_packages"]) >= 25 and len(set(worker["expected_packages"])) == len(worker["expected_packages"]) and
         len({row["package"] for row in packages}) == len(packages) == len(worker["expected_packages"]) and
         {row["package"] for row in packages} == set(worker["expected_packages"]) and
         all(row["result"] == "pass" and row["exit_code"] == row["failed"] == row["skipped"] == 0 for row in packages) and
         worker["test_counts"]["failed"] == worker["test_counts"]["skipped"] == 0 and worker["test_counts"]["passed"] > 0,
         "Backend package or test completion differs.")
    scope = Path(report["scope"])
    need(scope.parent == Path("/opt/goby-test") and scope.name.startswith("audit-fixes-20260913-") and worker["scope"] == str(scope), "Unexpected verification scope.")
    need(parse(read(scope / "worker-report.json", report["worker_report_sha256"])) == worker, "Worker report binding differs.")
    read(scope / "source.tar", ARCHIVE)
    need(value["sourceManifest"]["path"] == str(scope / "source-manifest.json"), "Source manifest escaped verification scope.")
    sources = descriptor(value["sourceManifest"])
    need(isinstance(sources, dict) and 100 <= len(sources) <= 10000, "Unexpected source closure size.")
    for name, row in sources.items():
        file_spec(row)
        read(scope / "source" / relative(name), row["sha256"], row["bytes"])
    binary = worker["binary"]
    need(binary["path"] == "bin/goby-linux-amd64", "Backend binary escaped verification scope.")
    file_spec({key: binary[key] for key in ("sha256", "bytes")})
    binary_raw = read(scope / binary["path"], binary["sha256"], binary["bytes"])
    frontend = descriptor(value["frontendReport"])
    need(frontend["kind"] == "audited-candidate-frontend-build" and frontend["status"] == "passed" and
         frontend["recursiveCgroupEmpty"] is True and frontend["sourceFilesMatched"] == 64 and frontend["reusedMockedApiTests"] == 41,
         "The matching frontend build is not accepted.")
    asset_root = Path(frontend["assetDirectory"])
    need(asset_root.is_relative_to("/opt/goby-test") and asset_root.name == "dist" and len(frontend["assets"]) == 57, "Unexpected frontend asset closure.")
    web_sources = {name[10:]: row for name, row in sources.items() if name.startswith("web/admin/")}
    need(len(web_sources) == 64, "Backend and frontend source inventories differ.")
    for name, row in web_sources.items():
        read(asset_root.parent / relative(name), row["sha256"], row["bytes"])
    assets = {name: read(asset_root / relative(name), file_spec(row)["sha256"], row["bytes"]) for name, row in frontend["assets"].items()}
    need({str(path.relative_to(asset_root)) for path in asset_root.rglob("*") if path.is_file()} == set(assets), "Frontend asset membership differs.")
    return report, binary_raw, assets


def unit_text(user, command, writable, work, log, environment=None):
    lines = ["[Unit]", "Description=Fresh audited Goby candidate", "[Service]", "Type=exec", "User=" + user, "Group=" + user,
             "UMask=0077", "Restart=no", "NoNewPrivileges=yes", "ProtectSystem=strict", "ProtectHome=yes", "PrivateTmp=yes",
             "KillMode=control-group", "TimeoutStopSec=20", "MemoryMax=" + ("256M" if user == "postgres" else "768M"),
             "MemorySwapMax=0", "TasksMax=128", "CPUQuota=100%", "WorkingDirectory=" + str(work),
             "ReadWritePaths=" + str(writable), "InaccessiblePaths=" + " ".join("-" + path for path in INACCESSIBLE),
             "StandardOutput=append:" + str(log), "StandardError=inherit", "ExecStart=" + command]
    if environment:
        lines.append("EnvironmentFile=" + str(environment))
    if user == "postgres":
        lines.append("KillSignal=SIGINT")
    return ("\n".join(lines) + "\n").encode()


def group_alive(pid):
    try:
        os.killpg(pid, 0)
        return True
    except ProcessLookupError:
        return False


def stop_command_group(process):
    """Bound cleanup of only the new session owned by this command."""
    output = (b"", b"")
    for sig in (signal.SIGTERM, signal.SIGKILL):
        try:
            os.killpg(process.pid, sig)
        except ProcessLookupError:
            pass
        try:
            output = process.communicate(timeout=5)
        except subprocess.TimeoutExpired as error:
            output = (error.output or b"", error.stderr or b"")
        deadline = time.monotonic() + 1
        while group_alive(process.pid) and time.monotonic() < deadline:
            time.sleep(0.05)
        if not group_alive(process.pid):
            process.wait(timeout=1)
            return output, True
    return output, False


class Provision:
    def __init__(self, value, input_pin, source_pin):
        self.value, self.input_pin, self.source_pin = value, input_pin, source_pin
        self.root = Path("/opt") / ("goby-audited-candidate-" + value["runId"])
        self.private, self.install, self.data, self.pgroot = [self.root / name for name in ("private", "install", "data", "postgres")]
        self.units = {role: "goby-audited-" + value["runId"] + "-" + role + ".service" for role in ("postgres", "server")}
        self.number, self.created, self.starts, self.stage, self.argv = 0, False, [], "preflight", {}
        self.pg_identity, self.pg_version = None, None
        self.command_responsibilities, self.listener_count = [], 0

    def mkdir(self, path, user="root", mode=0o700):
        account = pwd.getpwnam(user)
        os.mkdir(path, mode)
        os.chown(path, account.pw_uid, account.pw_gid)
        os.chmod(path, mode)
        self.sync(path.parent)

    def sync(self, path):
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)

    def write(self, path, raw, user="root", mode=0o600):
        account = pwd.getpwnam(user)
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode), "wb") as stream:
            os.fchown(stream.fileno(), account.pw_uid, account.pw_gid)
            os.fchmod(stream.fileno(), mode)
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        self.sync(path.parent)
        return {"path": str(path), "sha256": digest(raw)}

    def save(self, name, value):
        return self.write(self.private / name, encoded(value))

    def command(self, label, argv, payload=None):
        self.number += 1
        self.save("%03d-%s-intent.json" % (self.number, label), {"argv": argv, "stdinSha256": digest(payload) if payload else None})
        process = subprocess.Popen(argv, stdin=subprocess.PIPE if payload is not None else subprocess.DEVNULL,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=ENV, start_new_session=True)
        state = {"label": label, "pid": process.pid, "processGroup": process.pid, "outcome": "unknown",
                 "sqlOutcome": "unknown" if str(PG / "psql") in argv else None, "timedOut": False,
                 "descendantsRemained": False, "processGroupClosed": False}
        self.command_responsibilities.append(state)
        try:
            self.save("%03d-%s-process.json" % (self.number, label), state)
            stdout, stderr = process.communicate(payload, timeout=45)
            state["processGroupClosed"] = not group_alive(process.pid)
            if not state["processGroupClosed"]:
                state["descendantsRemained"] = True
                (_, _), state["processGroupClosed"] = stop_command_group(process)
        except subprocess.TimeoutExpired:
            state["timedOut"] = True
            (stdout, stderr), state["processGroupClosed"] = stop_command_group(process)
        except BaseException:
            (_, _), state["processGroupClosed"] = stop_command_group(process)
            raise
        state["exitCode"] = process.returncode
        if not state["timedOut"] and not state["descendantsRemained"] and process.returncode == 0:
            state["outcome"] = "acknowledged"
            if state["sqlOutcome"] is not None:
                state["sqlOutcome"] = "acknowledged"
        self.save("%03d-%s-result.json" % (self.number, label), state)
        need(len(stdout) <= 1 << 20 and len(stderr) <= 1 << 20, "Command output exceeded its bound.")
        self.write(self.private / ("%03d-%s.stdout" % (self.number, label)), stdout)
        self.write(self.private / ("%03d-%s.stderr" % (self.number, label)), stderr)
        need(not state["timedOut"] and not state["descendantsRemained"] and state["processGroupClosed"] and process.returncode == 0,
             "Owned command failed or timed out; its SQL outcome must not be retried automatically.")
        return stdout.decode().strip()

    def show(self, unit):
        fields = "Id,LoadState,ActiveState,SubState,MainPID,InvocationID,Result,ExecMainStatus,ControlGroup"
        result = subprocess.run(["/usr/bin/systemctl", "show", unit, "--property=" + fields], capture_output=True, timeout=10, env=ENV)
        need(len(result.stdout) <= 65536, "Unit observation exceeded its bound.")
        return dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)

    def start(self, role):
        unit = self.units[role]
        self.stage = role + "_start_requested"
        self.starts.append({"unit": unit, "acknowledged": False})
        self.command(role + "-start", ["/usr/bin/systemctl", "start", unit])
        self.starts[-1]["acknowledged"] = True
        observed = self.show(unit)
        need(observed.get("ActiveState") == "active" and observed.get("SubState") == "running" and int(observed["MainPID"]) > 1,
             "New unit did not establish a running process.")
        self.save(role + "-started.json", observed)
        return observed

    def process(self, role, observed):
        root = Path("/proc") / observed["MainPID"]
        binary = PG / "postgres" if role == "postgres" else self.install / "goby"
        need(root.stat().st_uid == pwd.getpwnam("postgres" if role == "postgres" else "goby").pw_uid and
             os.readlink(root / "exe") == str(binary) and (root / "cgroup").read_text() == "0::" + observed["ControlGroup"] + "\n",
             "Owned process executable, UID or cgroup differs.")
        need((root / "cmdline").read_bytes().decode().rstrip("\0").split("\0") == self.argv[role], "Owned process command differs.")
        raw = (root / "stat").read_text()
        fields = raw[raw.rfind(")") + 2:].split()
        info, source = (root / "exe").stat(), binary.stat()
        need(fields[0] != "Z" and (info.st_dev, info.st_ino) == (source.st_dev, source.st_ino), "Owned process file object differs.")
        return {"pid": int(observed["MainPID"]), "startTicks": fields[19], "bootId": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
                "exe": str(binary), "uid": root.stat().st_uid, "cgroup": observed["ControlGroup"],
                "executableDevice": info.st_dev, "executableInode": info.st_ino, "invocationId": observed["InvocationID"]}

    def listener(self, observed, process_identity, allow_absent=False):
        need(self.show(self.units["server"]) == observed and self.process("server", observed) == process_identity,
             "Candidate process changed before listener observation.")
        root, port = Path("/proc") / observed["MainPID"], self.value["ports"]["http"]
        raw = (root / "net/tcp").read_bytes()
        need(len(raw) <= 1 << 20, "TCP metadata exceeded its bound.")
        address = "0100007F:%04X" % port
        rows = [line.split() for line in raw.decode().splitlines()[1:]]
        matches = [row for row in rows if len(row) > 9 and row[1] == address and row[3] == "0A"]
        entries = list((root / "fd").iterdir())
        need(len(entries) <= 4096, "Candidate descriptor inventory exceeded its bound.")
        links = []
        for entry in entries:
            try:
                links.append(os.readlink(entry))
            except FileNotFoundError:
                pass
        self.listener_count += 1
        sample = {"process": process_identity, "port": port, "listenRows": matches,
                  "ownedListenInodes": [row[9] for row in matches if "socket:[" + row[9] + "]" in links]}
        self.save("listener-%03d.json" % self.listener_count, sample)
        need(self.show(self.units["server"]) == observed and self.process("server", observed) == process_identity,
             "Candidate process changed during listener observation.")
        if not matches and allow_absent:
            return None
        need(len(matches) == 1 and sample["ownedListenInodes"] == [matches[0][9]], "The HTTP listener is absent, foreign or ambiguous.")
        return {"pid": process_identity["pid"], "address": "127.0.0.1", "port": port, "socketInode": matches[0][9]}

    def psql(self, label, sql, database="postgres"):
        return self.command(label, ["/usr/sbin/runuser", "-u", "postgres", "--", str(PG / "psql"), "-X", "--no-password",
                                   "-h", str(self.pgroot / "socket"), "-p", str(self.value["ports"]["postgres"]),
                                   "-U", "postgres", "-d", database, "-v", "ON_ERROR_STOP=1", "-Atq"], sql.encode())

    def cluster(self, observed):
        need(self.show(self.units["postgres"]) == observed, "Owned PostgreSQL process changed.")
        current = self.process("postgres", observed)
        need(self.pg_identity is None or current == self.pg_identity, "Owned PostgreSQL file-object identity changed.")
        self.pg_identity = current
        lines = (self.pgroot / "data/postmaster.pid").read_text().splitlines()
        need(lines[0] == observed["MainPID"] and lines[1] == str(self.pgroot / "data") and lines[3] == str(self.value["ports"]["postgres"]), "Postmaster identity differs.")
        actual = self.psql("cluster-identity", "SELECT current_setting('data_directory'),current_setting('port'),(pg_control_system()).system_identifier,current_setting('server_version_num');")
        fields = actual.split("|")
        need(len(fields) == 4 and fields[:2] == [str(self.pgroot / "data"), str(self.value["ports"]["postgres"])] and
             fields[3].isdigit() and int(fields[3]) // 10000 == 17, "SQL connection reached another cluster or PostgreSQL version.")
        need(self.pg_version is None or self.pg_version == int(fields[3]), "Owned PostgreSQL version changed.")
        self.pg_version = int(fields[3])
        return fields[2]

    def database_facts(self, pg, name):
        self.cluster(pg)
        sql = ("SELECT json_build_object('database',d.datname,'databaseOid',d.oid::bigint,'roleOid',r.oid::bigint,"
               "'owner',pg_get_userbyid(d.datdba),'superuser',r.rolsuper,'createDb',r.rolcreatedb,'createRole',r.rolcreaterole,"
               "'replication',r.rolreplication,'bypassRls',r.rolbypassrls,'inherit',r.rolinherit,'login',r.rolcanlogin) "
               f"FROM pg_database d JOIN pg_roles r ON r.rolname='{name}' WHERE d.datname='{name}';")
        facts = parse(self.psql("database-identity-" + name, sql).encode())
        need(facts["database"] == facts["owner"] == name and facts["login"] is True and
             all(facts[key] is False for key in ("superuser", "createDb", "createRole", "replication", "bypassRls", "inherit")) and
             all(type(facts[key]) is int and facts[key] > 0 for key in ("databaseOid", "roleOid")), "Owned database identity or role privileges differ.")
        self.cluster(pg)
        objects = parse(self.psql("database-objects-" + name,
            "SELECT json_build_object('relations',(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace),"
            "'functions',(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace),"
            "'types',(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace),"
            "'schemas',(SELECT json_agg(nspname ORDER BY nspname) FROM pg_namespace WHERE nspname!~'^pg_' AND nspname<>'information_schema'));", name).encode())
        return {"identity": facts, "objects": objects}

    def run(self):
        report, binary, assets = verify_products(self.value)
        for unit in self.units.values():
            need(not os.path.lexists(Path("/run/systemd/system") / unit) and self.show(unit).get("LoadState") == "not-found", "New unit collision.")
        need(not os.path.lexists(self.root), "Fresh candidate scope already exists.")
        protected = {unit: self.show(unit) for unit in PROTECTED}
        need(all(protected[unit].get("ActiveState") == "inactive" and protected[unit].get("MainPID") == "0" for unit in PROTECTED[:2]),
             "The preserved source55 and primary services must retain the reviewed inactive baseline.")
        reservations = []
        try:
            for role in ("postgres", "http"):
                sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
                reservations.append(sock)
                sock.bind(("127.0.0.1", self.value["ports"][role]))
            owned(Path("/opt"), directory=True)
            self.mkdir(self.root, mode=0o755)
            self.mkdir(self.private)
            self.created = True
            self.save("provision-intent.json", {"input": self.input_pin, "source": self.source_pin, "scope": str(self.root), "units": self.units, "protected": protected})
            self.mkdir(self.install, mode=0o755)
            self.mkdir(self.install / "admin", mode=0o755)
            for name, raw in assets.items():
                target = self.install / "admin" / relative(name)
                for directory in reversed(target.parent.parents):
                    if directory.is_relative_to(self.install / "admin") and not directory.exists():
                        self.mkdir(directory, mode=0o755)
                if not target.parent.exists():
                    self.mkdir(target.parent, mode=0o755)
                self.write(target, raw, mode=0o644)
            installed = self.write(self.install / "goby", binary, mode=0o755)
            self.mkdir(self.data, "goby")
            for name in ("backups", "recovery", "operations", "diagnostics", "cache", "media"):
                self.mkdir(self.data / name, "goby")
            self.mkdir(self.pgroot, "postgres")
            self.mkdir(self.pgroot / "socket", "postgres")
            self.stage = "initialize_owned_cluster"
            self.command("initdb", ["/usr/sbin/runuser", "-u", "postgres", "--", str(PG / "initdb"), "-D", str(self.pgroot / "data"),
                                     "--username=postgres", "--encoding=UTF8", "--locale=C.UTF-8", "--auth-local=peer", "--auth-host=scram-sha-256", "--no-instructions"])
            db = "goby_candidate_" + self.value["runId"].split("-")[-1]
            target_db = "goby_recovery_" + self.value["runId"].split("-")[-1]
            port, webport = self.value["ports"]["postgres"], self.value["ports"]["http"]
            config = f"listen_addresses='127.0.0.1'\nport={port}\nunix_socket_directories='{self.pgroot / 'socket'}'\nhba_file='{self.pgroot / 'hba.conf'}'\nshared_buffers='32MB'\nwork_mem='1MB'\nmax_connections=40\nlog_min_error_statement=panic\nlog_statement=none\npassword_encryption='scram-sha-256'\n"
            self.write(self.pgroot / "server.conf", config.encode(), "postgres")
            self.write(self.pgroot / "hba.conf", f"local all postgres peer\nlocal all all reject\nhost {db} {db} 127.0.0.1/32 scram-sha-256\nhost {target_db} {target_db} 127.0.0.1/32 scram-sha-256\nhost all all 0.0.0.0/0 reject\nhost all all ::0/0 reject\n".encode(), "postgres")
            password, target_password, setup = secrets.token_hex(32), secrets.token_hex(32), secrets.token_hex(32)
            need(len({password, target_password, setup}) == 3, "Independent candidate credentials must be distinct.")
            runtime = {**SOFTWARE_ENV, "GOBY_DATABASE_URL": f"postgres://{db}:{password}@127.0.0.1:{port}/{db}?sslmode=disable", "GOBY_LISTEN": f"127.0.0.1:{webport}",
                       "GOBY_PUBLIC_URL": self.value["public_url"], "GOBY_SETUP_TOKEN": setup, "GOBY_WEB_DIR": str(self.install / "admin"),
                       "GOBY_COOKIE_SECURE": "false", "GOBY_STARTUP_TIMEOUT": "45s", "GOBY_MEDIA_ROOTS": str(self.data / "media"),
                       "GOBY_API_KEY_MASTER_KEY_FILE": str(self.data / "master.key"), "GOBY_BACKUP_DIR": str(self.data / "backups"),
                       "GOBY_RECOVERY_STATE_DIR": str(self.data / "recovery"), "GOBY_RECOVERY_OPERATIONS_DIR": str(self.data / "operations"),
                       "GOBY_RECOVERY_DATABASE_URL": f"postgres://{target_db}:{target_password}@127.0.0.1:{port}/{target_db}?sslmode=disable",
                       "GOBY_PG_DUMP": str(PG / "pg_dump"), "GOBY_PG_RESTORE": str(PG / "pg_restore"),
                       "GOBY_LOG_DIR": str(self.data / "diagnostics"), "GOBY_TRANSCODE_CACHE": str(self.data / "cache"), "GOBY_BACKUP_MIN_FREE_BYTES": "67108864",
                       "GOBY_LOG_MIN_FREE_BYTES": "16777216", "GOBY_FFMPEG": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg", "GOBY_FFPROBE": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe", "GOMAXPROCS": "2", "GOMEMLIMIT": "512MiB"}
            env_pin = self.write(self.private / "runtime.env", "".join(f"{key}={value}\n" for key, value in sorted(runtime.items())).encode())
            unit_pins = {}
            for role, user, command, writable in (("postgres", "postgres", f"{PG / 'postgres'} -D {self.pgroot / 'data'} -c config_file={self.pgroot / 'server.conf'}", self.pgroot),
                                                   ("server", "goby", str(self.install / "goby"), self.data)):
                self.argv[role] = shlex.split(command)
                unit_pins[role] = self.write(Path("/run/systemd/system") / self.units[role], unit_text(user, command, writable, writable,
                                                  self.private / (role + "-unit.log"), self.private / "runtime.env" if role == "server" else None))
            self.command("daemon-reload", ["/usr/bin/systemctl", "daemon-reload"])
            for role, user, write_path in (("postgres", "postgres", self.pgroot), ("server", "goby", self.data)):
                raw = subprocess.check_output(["/usr/bin/systemctl", "show", self.units[role], "--property=Type,User,Group,Restart,ProtectSystem,ProtectHome,PrivateTmp,NoNewPrivileges,ReadWritePaths,InaccessiblePaths,ExecStart,MemoryMax,MemorySwapMax,TasksMax"], env=ENV, timeout=10).decode()
                loaded = dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)
                blocked = loaded.pop("InaccessiblePaths").split()
                need(len(blocked) == len(INACCESSIBLE) and {path.removeprefix("-") for path in blocked} == set(INACCESSIBLE), "Old instance state is not hidden from the new unit.")
                command = loaded.pop("ExecStart")
                need(command.count("argv[]=") == 1 and shlex.split(command.split("argv[]=", 1)[1].split(";", 1)[0]) == self.argv[role], "Loaded unit command differs.")
                need(loaded == {"Type": "exec", "User": user, "Group": user, "Restart": "no", "ProtectSystem": "strict", "ProtectHome": "yes", "PrivateTmp": "yes",
                                "NoNewPrivileges": "yes", "ReadWritePaths": str(write_path), "MemoryMax": str((256 if role == "postgres" else 768) << 20),
                                "MemorySwapMax": "0", "TasksMax": "128"}, "Loaded privilege or resource boundaries differ.")
                environment = subprocess.check_output(["/usr/bin/systemctl", "show", self.units[role], "--property=EnvironmentFiles"], env=ENV, timeout=10).decode()
                files = [line.split("=", 1)[1] for line in environment.splitlines() if line.startswith("EnvironmentFiles=") and line.split("=", 1)[1]]
                need(files == ([str(self.private / "runtime.env") + " (ignore_errors=no)"] if role == "server" else []), "Unexpected additional unit environment file.")
            reservations[0].close()
            pg = self.start("postgres")
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                need(self.show(self.units["postgres"]) == pg, "PostgreSQL stopped before initialization completed.")
                pidfile = self.pgroot / "data/postmaster.pid"
                if pidfile.exists() and len(pidfile.read_text().splitlines()) >= 8 and pidfile.read_text().splitlines()[7].strip() == "ready":
                    break
                time.sleep(0.2)
            else:
                raise ValueError("Owned PostgreSQL readiness deadline expired.")
            cluster_id = self.cluster(pg)
            databases = {}
            empty = {"relations": 0, "functions": 0, "types": 0, "schemas": ["public"]}
            for slot, name, secret in (("source", db, password), ("recovery", target_db, target_password)):
                self.stage = slot + "_database_setup"
                for label, sql in (("create-role", f"CREATE ROLE {name} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD '{secret}';"),
                                   ("create-database", f"CREATE DATABASE {name} OWNER {name} TEMPLATE template0;"),
                                   ("restrict-database", f"REVOKE ALL ON DATABASE {name} FROM PUBLIC;")):
                    self.cluster(pg)
                    self.psql(slot + "-" + label, sql)
                before = self.database_facts(pg, name)
                need(before["objects"] == empty, "The new source and recovery databases must start empty.")
                databases[slot] = {"name": name, "role": name, "beforeStart": before}
            self.save("databases-before-start.json", databases)
            reservations[1].close()
            server = self.start("server")
            server_identity = self.process("server", server)
            self.stage = "readiness"
            deadline, checks, listen_anchor = time.monotonic() + 60, [], None
            while time.monotonic() < deadline:
                listener_before = self.listener(server, server_identity, allow_absent=True)
                if listener_before is None:
                    time.sleep(1)
                    continue
                need(listen_anchor is None or listener_before == listen_anchor, "The candidate listening socket changed.")
                listen_anchor = listener_before
                check = {"route": "/readyz", "listenerBefore": listener_before}
                ready = False
                try:
                    connection = http.client.HTTPConnection("127.0.0.1", webport, timeout=3)
                    connection.request("GET", "/readyz")
                    response = connection.getresponse()
                    raw = response.read(65537)
                    check.update(status=response.status, bodySha256=digest(raw))
                    ready = response.status == 200 and len(raw) <= 65536 and parse(raw) == {"Status": "ready"} and response.getheader("Set-Cookie") is None
                except (OSError, http.client.HTTPException):
                    check["transportFailure"] = True
                finally:
                    connection.close()
                check["listenerAfter"] = self.listener(server, server_identity)
                need(check["listenerAfter"] == listen_anchor, "The candidate listener changed during the readiness request.")
                checks.append(check)
                if ready:
                    break
                time.sleep(1)
            else:
                raise ValueError("Candidate readiness deadline expired.")
            listener_before = self.listener(server, server_identity)
            need(listener_before == listen_anchor, "Candidate listener changed before health check.")
            connection = http.client.HTTPConnection("127.0.0.1", webport, timeout=3)
            try:
                connection.request("GET", "/healthz")
                response = connection.getresponse()
                raw = response.read(65537)
                need(response.status == 200 and len(raw) <= 65536 and parse(raw) == {"Status": "ok"} and response.getheader("Set-Cookie") is None, "Health response differs.")
            finally:
                connection.close()
            listener_after = self.listener(server, server_identity)
            need(listener_after == listen_anchor, "Candidate listener changed during health check.")
            checks.append({"route": "/healthz", "status": 200, "bodySha256": digest(raw), "listenerBefore": listener_before, "listenerAfter": listener_after})
            self.save("readiness.json", checks)
            self.cluster(pg)
            facts = parse(self.psql("new-database-facts", "SELECT json_build_object('users',(SELECT count(*) FROM users),'schema',(SELECT max(version) FROM schema_migrations),'migrations',(SELECT count(*) FROM schema_migrations));", db).encode())
            need(facts == {"users": 0, "schema": 28, "migrations": 28}, "Bootstrap or schema state differs.")
            for slot, name in (("source", db), ("recovery", target_db)):
                after = self.database_facts(pg, name)
                need(after["identity"] == databases[slot]["beforeStart"]["identity"], "Owned database or role identity changed during startup.")
                need(slot != "recovery" or after["objects"] == empty, "The recovery target must remain uninitialized until its dedicated rehearsal.")
                databases[slot]["afterStart"] = after
            self.save("databases-after-start.json", databases)
            need(self.show(self.units["server"]) == server and self.process("server", server) == server_identity, "Candidate changed after readiness.")
            read(self.install / "goby", installed["sha256"], len(binary))
            need({unit: self.show(unit) for unit in PROTECTED} == protected, "An unrelated service identity changed.")
            final_listener = self.listener(server, server_identity)
            need(final_listener == listen_anchor, "Candidate listener changed before manifest publication.")
            manifest = {"kind": "audited-candidate-private-manifest", "status": "running_awaiting_live_acceptance", "runId": self.value["runId"],
                        "input": self.input_pin, "binary": installed, "frontendReport": self.value["frontendReport"], "backendReport": self.value["backendReport"],
                        "runtime": env_pin, "units": unit_pins, "processes": {"postgres": pg, "server": server}, "serverIdentity": server_identity,
                        "postgresIdentity": self.pg_identity, "postgresVersionNum": self.pg_version, "clusterSystemIdentifier": cluster_id,
                        "inaccessiblePaths": list(INACCESSIBLE), "transcodingProfile": self.value["transcodingProfile"], "transcodingEnvironment": SOFTWARE_ENV,
                        "database": db, "ports": self.value["ports"], "directUrl": f"http://127.0.0.1:{webport}", "publicUrl": self.value["public_url"],
                        "dataDirectory": str(self.data), "setupToken": setup, "recoveryDatabase": target_db, "databases": databases,
                        "sourceState": facts, "recoveryRestoreExecuted": False, "listener": final_listener,
                        "bootstrapExecuted": False, "clientAcceptance": False, "candidateAdmissionComplete": False,
                        "startCalls": self.starts, "commandResponsibilities": self.command_responsibilities}
            return self.save("manifest.json", manifest)
        finally:
            for reservation in reservations:
                reservation.close()


def failure_report(job, error):
    value = {"status": "provision_failed_resources_retained", "stage": job.stage, "errorType": type(error).__name__,
             "scope": str(job.root), "units": job.units, "startCalls": job.starts, "commandResponsibilities": job.command_responsibilities,
             "resumeAllowed": False, "automaticCleanup": False, "receipt": None}
    try:
        if job.created:
            value["receipt"] = job.save("failure.json", value)
    except Exception as receipt_error:
        value["receiptErrorType"] = type(receipt_error).__name__
    return value


def main():
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
         "Use authorized root SSH with /usr/bin/python3 -I -B.")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True)
    parser.add_argument("--input-sha256", required=True)
    parser.add_argument("--source-sha256", required=True)
    args = parser.parse_args()
    read(Path(__file__).absolute(), args.source_sha256)
    pin = {"path": args.input, "sha256": args.input_sha256}
    job = Provision(validate_input(descriptor(pin)), pin, {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256})
    try:
        result = job.run()
        print(json.dumps({"status": "running_awaiting_live_acceptance", "manifest": result, "candidateAdmissionComplete": False}))
    except Exception as error:
        print(json.dumps(failure_report(job, error)))
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
