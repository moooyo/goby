#!/usr/bin/env python3
"""Prepare, inspect, or stop one persistent, explicitly owned test cluster.

Run only as the authorized root operator through ssh test-env. No migration,
deletion, shared-cluster mutation, package installation, or service action is
performed. Interrupted initialization requires operator inspection; unknown
paths and processes are never adopted. Credentials are never printed.
"""

from __future__ import annotations

import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import select
import signal
import socket
import stat
import subprocess
import sys


class WorkspaceError(Exception):
    """A fail-closed operator guard rejected the requested action."""


PARENT = Path("/opt/goby-test")
CONTROL = PARENT / "postgres-workspace-v1"
BASE = Path("/var/lib/postgresql/goby-workspace-v1")
DATA = BASE / "data"
SOCKET = BASE / "socket"
LOG = BASE / "log"
OWNER = CONTROL / "owner.json"
ENV_FILE = CONTROL / "test.env"
LOCK = CONTROL / "operator.lock"
BIN = Path("/usr/lib/postgresql/17/bin")
PORT = 15432
PG_VERSION = "17.11"
MARKER = "goby-postgres-persistent-workspace-v1"
TOOLS = ("postgres", "pg_ctl", "pg_controldata", "initdb", "psql")
ENVIRONMENT = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
CONF = f"""# Dedicated persistent Goby verification cluster.
listen_addresses = '127.0.0.1'
port = {PORT}
unix_socket_directories = '{SOCKET}'
unix_socket_permissions = 0700
cluster_name = 'goby-workspace-v1'
ssl = off
shared_buffers = '32MB'
work_mem = '1MB'
maintenance_work_mem = '16MB'
max_connections = 50
temp_file_limit = '64MB'
dynamic_shared_memory_type = mmap
wal_level = minimal
max_wal_senders = 0
max_replication_slots = 0
archive_mode = off
wal_keep_size = 0
min_wal_size = '32MB'
max_wal_size = '128MB'
wal_buffers = '1MB'
wal_compression = on
checkpoint_timeout = '1min'
password_encryption = 'scram-sha-256'
log_destination = 'stderr'
logging_collector = off
log_min_messages = warning
log_min_error_statement = panic
log_statement = none
log_connections = off
log_disconnections = off
"""
HBA = """# Peer administration and explicitly named loopback SCRAM test logins only.
local all postgres peer
local all all reject
host goby_test goby_test 127.0.0.1/32 scram-sha-256
host goby_client_m3e goby_client_m3e 127.0.0.1/32 scram-sha-256
host all all 0.0.0.0/0 reject
host all all ::0/0 reject
"""


def require(condition, message):
    if not condition:
        raise WorkspaceError(message)


def present(path):
    """Include dangling symlinks when deciding whether a path is occupied."""
    try:
        path.lstat()
        return True
    except FileNotFoundError:
        return False


def canonical(path):
    """Reject every symlink component before reading a managed path."""
    require(path.is_absolute(), "An operator path is not absolute.")
    for part in reversed((path, *path.parents)):
        info = part.lstat()
        require(not stat.S_ISLNK(info.st_mode), "An operator path contains a symlink.")
    return path.lstat()


def directory(path, uid, gid, mode=0o700):
    info = canonical(path)
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == uid and info.st_gid == gid and
            stat.S_IMODE(info.st_mode) == mode,
            "A managed directory has unexpected ownership, permissions, or type.")
    return {"device": info.st_dev, "inode": info.st_ino}


def regular(path, uid=0, gid=0, mode=0o600, limit=None):
    info = canonical(path)
    require(stat.S_ISREG(info.st_mode) and info.st_uid == uid and info.st_gid == gid and
            stat.S_IMODE(info.st_mode) == mode and info.st_nlink == 1,
            "A managed file has unexpected ownership, permissions, type, or hard links.")
    require(limit is None or info.st_size <= limit, "A managed file exceeds its size limit.")
    return info


def read_file(path, *, uid=0, gid=0, mode=0o600, limit=16384):
    expected = regular(path, uid, gid, mode, limit)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "r", encoding="utf-8") as handle:
        opened = os.fstat(handle.fileno())
        require((opened.st_dev, opened.st_ino) == (expected.st_dev, expected.st_ino),
                "A managed file changed while it was opened.")
        value = handle.read(limit + 1)
    require(len(value.encode("utf-8")) <= limit, "A managed file exceeds its size limit.")
    return value


def create_private(path, value, uid=0, gid=0):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        os.fchown(handle.fileno(), uid, gid)
        handle.write(value)
        handle.flush()
        os.fsync(handle.fileno())


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def save_owner(owner):
    regular(OWNER)
    temporary = CONTROL / ("owner.next-" + secrets.token_hex(16))
    create_private(temporary, json.dumps(owner, sort_keys=True) + "\n")
    # The only replacement is our verified external owner record. A failed
    # write deliberately leaves the private temporary file for inspection.
    os.replace(temporary, OWNER)
    sync_directory(CONTROL)


def command(arguments, *, input_text=None, environment=None, timeout=45):
    try:
        result = subprocess.run([str(value) for value in arguments], input=input_text, text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
                                env=environment or ENVIRONMENT, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired):
        raise WorkspaceError("A dedicated-cluster command could not complete; inspect its private log.") from None
    require(result.returncode == 0, "A dedicated-cluster command failed; inspect its private log.")
    return result.stdout.strip()


def postgres_command(arguments, **kwargs):
    environment = dict(ENVIRONMENT, HOME="/var/lib/postgresql", TMPDIR=str(BASE))
    return command(["/usr/sbin/runuser", "--user", "postgres", "--", "/usr/bin/env", "-i",
                    *[f"{key}={value}" for key, value in environment.items()], *arguments], **kwargs)


def verify_parents(postgres):
    directory(PARENT, 0, 0)
    regular(PARENT / "test.env")
    for path in (Path("/"), Path("/opt"), Path("/var"), Path("/var/lib"), BASE.parent):
        info = canonical(path)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid in (0, postgres.pw_uid) and
                not stat.S_IMODE(info.st_mode) & 0o022,
                "A parent directory is not protected from untrusted writes.")
        require(info.st_uid != postgres.pw_uid or path == BASE.parent,
                "An administrative parent directory is not root-owned.")
        if path != Path("/opt"):
            access = 0o100 if info.st_uid == postgres.pw_uid else (0o010 if info.st_gid == postgres.pw_gid else 0o001)
            require(bool(info.st_mode & access), "The postgres account cannot traverse its data parent.")
    filesystem = json.loads(command(["/usr/bin/findmnt", "--json", "--target", BASE.parent,
                                     "--output", "SOURCE,FSTYPE,TARGET,OPTIONS"]))["filesystems"]
    require(len(filesystem) == 1 and filesystem[0]["fstype"] in ("ext4", "xfs", "btrfs") and
            filesystem[0]["source"].startswith("/dev/") and
            "rw" in filesystem[0]["options"].split(","),
            "The workspace data parent is not on an approved writable persistent disk filesystem.")


def binaries():
    identity = {}
    for name in TOOLS:
        path = BIN / name
        info = canonical(path)
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                info.st_nlink == 1 and stat.S_IMODE(info.st_mode) == 0o755,
                "A PostgreSQL binary has unexpected ownership, permissions, or type.")
        for parent in path.parents:
            metadata = canonical(parent)
            require(stat.S_ISDIR(metadata.st_mode) and metadata.st_uid == 0 and not metadata.st_mode & 0o022,
                    "A PostgreSQL binary parent is not protected from writes.")
        version = command([path, "--version"])
        require(re.fullmatch(rf"{re.escape(name)} \(PostgreSQL\) {re.escape(PG_VERSION)}(?: \([^\n]+\))?", version),
                "A PostgreSQL binary does not match the pinned 17.11 baseline.")
        identity[name] = {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
    return identity


def expected_owner(postgres, binary_identity):
    return {"marker": MARKER, "control": str(CONTROL), "cluster": str(BASE), "data": str(DATA),
            "socket": str(SOCKET), "log": str(LOG), "port": PORT, "version": PG_VERSION,
            "role": "goby_test", "database": "goby_test", "uid": postgres.pw_uid,
            "gid": postgres.pw_gid, "binaries": binary_identity,
            "configuration_sha256": hashlib.sha256(CONF.encode()).hexdigest(),
            "hba_sha256": hashlib.sha256(HBA.encode()).hexdigest()}


def strict_object(pairs):
    value = {}
    for key, item in pairs:
        require(key not in value, "An owner record contains duplicate fields.")
        value[key] = item
    return value


def validate_owner(owner, expected, control_identity):
    dynamic = {"control_identity", "directories", "system_identifier", "process", "phase"}
    require(isinstance(owner, dict) and set(owner) == set(expected) | dynamic and
            all(owner.get(key) == value for key, value in expected.items()),
            "The external owner record does not match this exact workspace configuration.")
    require(owner["control_identity"] == control_identity,
            "The control directory identity differs from its owner record.")
    require(owner["phase"] == "initialized" and
            isinstance(owner["system_identifier"], str) and
            re.fullmatch(r"[0-9]{10,20}", owner["system_identifier"]),
            "Initialization is incomplete; inspect owned paths without automatic recreation.")
    require(isinstance(owner["directories"], dict) and
            set(owner["directories"]) == {str(path) for path in (BASE, DATA, SOCKET, LOG)},
            "The owner record lacks the exact persistent directory identities.")
    process = owner["process"]
    require(process is None or (isinstance(process, dict) and set(process) == {"pid", "start_ticks", "boot_id"} and
            type(process["pid"]) is int and process["pid"] > 1 and
            type(process["start_ticks"]) is int and process["start_ticks"] > 0 and
            re.fullmatch(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", process["boot_id"])),
            "The owner record has an invalid process identity.")


def acquire_lock(create=False):
    if not create:
        regular(LOCK)
    flags = os.O_RDWR | os.O_NOFOLLOW | (os.O_CREAT | os.O_EXCL if create else 0)
    descriptor = os.open(LOCK, flags, 0o600)
    try:
        expected = regular(LOCK)
        opened = os.fstat(descriptor)
        require((opened.st_dev, opened.st_ino) == (expected.st_dev, expected.st_ino),
                "The operator lock changed while it was opened.")
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(descriptor)
        raise WorkspaceError("Another workspace operator is running.") from None
    except BaseException:
        os.close(descriptor)
        raise
    return descriptor


def port_available():
    require(not command(["/usr/bin/ss", "-H", "-ltnp", f"sport = :{PORT}"]),
            "TCP port 15432 is occupied; its listener will not be stopped or adopted.")
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        try:
            probe.bind(("127.0.0.1", PORT))
        except OSError:
            raise WorkspaceError("TCP port 15432 cannot be exclusively bound; no listener will be changed.") from None


def cluster_identifier():
    output = postgres_command([BIN / "pg_controldata", DATA])
    match = re.search(r"^Database system identifier:\s+([0-9]+)$", output, re.MULTILINE)
    require(match is not None, "The persistent cluster system identifier is unavailable.")
    return match.group(1)


def verify_directories(postgres, owner):
    device = canonical(BASE.parent).st_dev
    for path in (BASE, DATA, SOCKET, LOG):
        identity = directory(path, postgres.pw_uid, postgres.pw_gid)
        require(identity == owner["directories"][str(path)] and identity["device"] == device,
                "A persistent directory was replaced or moved to another filesystem.")
    require(set(BASE.iterdir()) == {DATA, SOCKET, LOG}, "The workspace root contains unexpected entries.")
    # No tablespace, WAL, configuration, or log symlink can redirect a command
    # outside the recorded cluster. No recursive deletion is ever performed.
    for path in BASE.rglob("*"):
        info = path.lstat()
        require(info.st_uid == postgres.pw_uid and info.st_gid == postgres.pw_gid and info.st_dev == device,
                "A cluster entry has unexpected ownership or filesystem identity.")
        if stat.S_ISDIR(info.st_mode):
            require(stat.S_IMODE(info.st_mode) == 0o700, "A cluster directory is not private.")
        elif stat.S_ISREG(info.st_mode):
            require(stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
                    "A cluster file is not private or has a hard link.")
        else:
            require(path == SOCKET / f".s.PGSQL.{PORT}" and stat.S_ISSOCK(info.st_mode) and
                    stat.S_IMODE(info.st_mode) == 0o700,
                    "A cluster entry is a symlink or an unexpected special file.")


def verify_configuration(postgres):
    options = {"uid": postgres.pw_uid, "gid": postgres.pw_gid}
    require(read_file(DATA / "PG_VERSION", **options).strip() == "17", "The data major version differs.")
    require(read_file(DATA / "postgresql.conf", **options) == CONF and
            read_file(DATA / "pg_hba.conf", **options) == HBA,
            "The cluster configuration differs; it will not be overwritten.")
    for name in ("postgresql.auto.conf", "pg_ident.conf"):
        value = read_file(DATA / name, **options)
        require(all(not line.strip() or line.lstrip().startswith("#") for line in value.splitlines()),
                "The cluster has unexpected configuration overrides or identity mappings.")


def process_identity(pid):
    value = (Path("/proc") / str(pid) / "stat").read_text(encoding="ascii")
    close = value.rfind(")")
    require(close >= 0 and value[:value.find(" ")] == str(pid), "A process stat record is malformed.")
    fields = value[close + 2:].split()
    require(len(fields) >= 20 and fields[19].isdigit(), "A process start identity is unavailable.")
    boot_id = Path("/proc/sys/kernel/random/boot_id").read_text(encoding="ascii").strip()
    require(re.fullmatch(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", boot_id),
            "The host boot identity is malformed.")
    return {"pid": pid, "start_ticks": int(fields[19]), "boot_id": boot_id}


def verify_process(postgres, owner, *, newly_started=False):
    pid_file = DATA / "postmaster.pid"
    if not present(pid_file):
        return None
    lines = read_file(pid_file, uid=postgres.pw_uid, gid=postgres.pw_gid).splitlines()
    require(len(lines) >= 8 and lines[0].isdigit() and int(lines[0]) > 1 and lines[1] == str(DATA) and
            lines[2].isdigit() and lines[3] == str(PORT) and lines[4] == str(SOCKET) and
            lines[5] == "127.0.0.1" and lines[7].strip() == "ready",
            "The PostgreSQL PID file does not identify the expected ready cluster.")
    pid = int(lines[0])
    process = Path("/proc") / str(pid)
    if not present(process):
        return None
    identity = process_identity(pid)
    require(newly_started or owner["process"] == identity,
            "The PID, boot identifier, or process start ticks differ from the external owner record.")
    require(process.stat().st_uid == postgres.pw_uid and
            (process / "exe").readlink() == BIN / "postgres" and
            (process / "cmdline").read_bytes().rstrip(b"\0").split(b"\0") ==
            [str(BIN / "postgres").encode(), b"-D", str(DATA).encode()],
            "The recorded PID belongs to an unexpected executable, account, or command line.")
    listeners = command(["/usr/bin/ss", "-H", "-ltnp", f"sport = :{PORT}"]).splitlines()
    require(len(listeners) == 1 and f"127.0.0.1:{PORT}" in listeners[0].split() and
            re.search(rf"\bpid={pid},", listeners[0]) is not None,
            "The TCP listener does not belong exclusively to the verified loopback process.")
    socket_file = SOCKET / f".s.PGSQL.{PORT}"
    socket_info = canonical(socket_file)
    require(stat.S_ISSOCK(socket_info.st_mode) and socket_info.st_uid == postgres.pw_uid and
            socket_info.st_gid == postgres.pw_gid and stat.S_IMODE(socket_info.st_mode) == 0o700,
            "The cluster administration socket is not private and owned.")
    require(process_identity(pid) == identity, "The cluster process changed during identity verification.")
    return identity


def require_stopped(postgres):
    port_available()
    expected = [str(BIN / "postgres").encode(), b"-D", str(DATA).encode()]
    for path in Path("/proc").iterdir():
        if not path.name.isdigit():
            continue
        try:
            arguments = (path / "cmdline").read_bytes().rstrip(b"\0").split(b"\0")
        except (FileNotFoundError, ProcessLookupError):
            continue
        require(arguments != expected and str(DATA).encode() not in arguments,
                "A process references the workspace without a usable owner PID; refusing to start or stop it.")
    for path in SOCKET.iterdir():
        require(path.name in (f".s.PGSQL.{PORT}", f".s.PGSQL.{PORT}.lock"),
                "The socket directory contains an unknown entry.")
    # Stale sockets are left for PostgreSQL to diagnose. No file is unlinked.


def stop_process(postgres, owner, process):
    """Bind the stop signal to the checked process, never a re-read PID file."""
    descriptor = os.pidfd_open(process["pid"], 0)
    try:
        require(verify_process(postgres, owner) == process,
                "The process identity changed immediately before the requested stop.")
        waiter = select.poll()
        waiter.register(descriptor, select.POLLIN)
        require(not waiter.poll(0), "The verified process exited before the requested stop.")
        signal.pidfd_send_signal(descriptor, signal.SIGINT)
        require(bool(waiter.poll(30000)), "The owned process has not stopped within 30 seconds.")
        require(not present(DATA / "postmaster.pid"), "The owned cluster PID file remains after process exit.")
    finally:
        os.close(descriptor)


def administrator(sql):
    return postgres_command([BIN / "psql", "-X", "--no-password", "-h", SOCKET, "-p", PORT,
                             "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"],
                            input_text=sql, timeout=15)


def credentials(*, create=False):
    if not present(ENV_FILE):
        require(create, "The managed credential file is missing; no password was rotated.")
        password = secrets.token_hex(32)
        url = f"postgresql://goby_test:{password}@127.0.0.1:{PORT}/goby_test?sslmode=disable"
        create_private(ENV_FILE, f"GOBY_DATABASE_URL='{url}'\nGOBY_TEST_DATABASE_URL='{url}'\n")
        sync_directory(CONTROL)
    value = read_file(ENV_FILE, limit=1024)
    pattern = (rf"GOBY_DATABASE_URL='postgresql://goby_test:([0-9a-f]{{64}})@127\.0\.0\.1:{PORT}/goby_test\?sslmode=disable'\n"
               rf"GOBY_TEST_DATABASE_URL='postgresql://goby_test:\1@127\.0\.0\.1:{PORT}/goby_test\?sslmode=disable'\n")
    match = re.fullmatch(pattern, value)
    require(match is not None, "The credential file does not contain the exact dedicated connection settings.")
    return match.group(1)


def check_tcp_login(password):
    password_file = CONTROL / ("login.pass-" + secrets.token_hex(16))
    create_private(password_file, f"127.0.0.1:{PORT}:goby_test:goby_test:{password}\n")
    try:
        result = command([BIN / "psql", "-X", "--no-password", "-h", "127.0.0.1", "-p", PORT,
                          "-U", "goby_test", "-d", "goby_test", "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"],
                         input_text="SELECT current_user, current_database(), inet_server_port();\n",
                         environment=dict(ENVIRONMENT, PGPASSFILE=str(password_file), PGCONNECT_TIMEOUT="5"), timeout=15)
        require(result == f"goby_test|goby_test|{PORT}", "SCRAM login did not reach the expected test database.")
    finally:
        regular(password_file)
        password_file.unlink()


def verify_server(owner):
    identity = administrator("SELECT current_setting('data_directory'), current_setting('port'), "
                             "current_setting('listen_addresses'), system_identifier FROM pg_control_system();\n")
    require(identity == f"{DATA}|{PORT}|127.0.0.1|{owner['system_identifier']}",
            "The administration connection reached an unexpected server identity.")
    settings = administrator("SELECT current_setting('config_file'), current_setting('hba_file'), "
                             "current_setting('ident_file'), current_setting('unix_socket_directories'), "
                             "current_setting('shared_buffers'), current_setting('max_connections'), "
                             "current_setting('min_wal_size'), current_setting('max_wal_size'), "
                             "current_setting('password_encryption'), current_setting('ssl');\n")
    require(settings == f"{DATA}/postgresql.conf|{DATA}/pg_hba.conf|{DATA}/pg_ident.conf|{SOCKET}|"
                        "32MB|50|32MB|128MB|scram-sha-256|off",
            "The running server configuration or resource limits differ from the owned configuration.")


def prepare_role(*, check_only=False):
    exists = administrator("SELECT count(*) FROM pg_roles WHERE rolname = 'goby_test';\n") == "1"
    require(not exists or present(ENV_FILE), "The role exists without managed credentials; refusing password rotation.")
    require(exists or not check_only, "The dedicated login role is missing.")
    password = credentials(create=not exists and not check_only)
    if not exists:
        administrator("CREATE ROLE goby_test LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE "
                      "NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "';\n")
    flags = administrator("SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls "
                          "FROM pg_roles WHERE rolname = 'goby_test';\n")
    require(flags == "t|f|f|f|f|f", "The dedicated role has unexpected privileges; they will not be changed.")
    memberships = administrator("SELECT count(*) FROM pg_auth_members WHERE member = "
                                "(SELECT oid FROM pg_roles WHERE rolname = 'goby_test');\n")
    require(memberships == "0", "The dedicated role has unexpected inherited role memberships.")
    scram = administrator("SELECT rolpassword LIKE 'SCRAM-SHA-256$%' FROM pg_authid WHERE rolname = 'goby_test';\n")
    require(scram == "t", "The dedicated role lacks a SCRAM verifier.")
    owner = administrator("SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'goby_test';\n")
    require(owner in ("", "goby_test"), "The dedicated database has an unexpected owner.")
    require(owner or not check_only, "The dedicated database is missing.")
    if not owner:
        administrator("CREATE DATABASE goby_test OWNER goby_test;\n")
    check_tcp_login(password)


def initialize(postgres, owner):
    require(not present(BASE), "The data workspace exists without a completed owner record; refusing adoption.")
    filesystem = os.statvfs(BASE.parent)
    require(filesystem.f_bavail * filesystem.f_frsize >= 2 * 1024 ** 3,
            "The persistent data filesystem has less than 2 GiB available.")
    BASE.mkdir(mode=0o700)
    base_descriptor = os.open(BASE, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    data_descriptor = None
    initial = {}
    try:
        info = os.fstat(base_descriptor)
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                stat.S_IMODE(info.st_mode) == 0o700,
                "The new workspace directory changed before it was fixed by a directory handle.")
        initial[str(BASE)] = {"device": info.st_dev, "inode": info.st_ino}
        # Keep the root directory inaccessible to postgres until every child
        # has been created through the fixed directory handle.
        for path in (DATA, SOCKET, LOG):
            os.mkdir(path.name, mode=0o700, dir_fd=base_descriptor)
            child = os.open(path.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=base_descriptor)
            try:
                info = os.fstat(child)
                require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                        stat.S_IMODE(info.st_mode) == 0o700,
                        "A newly created workspace child has an unexpected identity.")
                initial[str(path)] = {"device": info.st_dev, "inode": info.st_ino}
                os.fchown(child, postgres.pw_uid, postgres.pw_gid)
                if path == DATA:
                    data_descriptor = os.dup(child)
            finally:
                os.close(child)
        os.fchown(base_descriptor, postgres.pw_uid, postgres.pw_gid)
        for path in (BASE, DATA, SOCKET, LOG):
            require(directory(path, postgres.pw_uid, postgres.pw_gid) == initial[str(path)],
                    "A newly created workspace directory was replaced before initialization.")
        postgres_command([BIN / "initdb", "-D", DATA, "--username=postgres", "--locale=C.UTF-8",
                          "--encoding=UTF8", "--auth-local=peer", "--auth-host=scram-sha-256", "--data-checksums"], timeout=90)
        for name, value in (("postgresql.conf", CONF), ("pg_hba.conf", HBA)):
            descriptor = os.open(name, os.O_WRONLY | os.O_NOFOLLOW, dir_fd=data_descriptor)
            with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
                info = os.fstat(handle.fileno())
                require(stat.S_ISREG(info.st_mode) and info.st_uid == postgres.pw_uid and
                        info.st_gid == postgres.pw_gid and stat.S_IMODE(info.st_mode) == 0o600 and
                        info.st_nlink == 1 and info.st_dev == initial[str(DATA)]["device"],
                        "A new configuration file is not a private file in the fixed data directory.")
                handle.write(value)
                handle.truncate()
                handle.flush()
                os.fsync(handle.fileno())
        for path in (BASE, DATA, SOCKET, LOG):
            require(directory(path, postgres.pw_uid, postgres.pw_gid) == initial[str(path)],
                    "A newly initialized workspace directory was replaced.")
        owner.update(phase="initialized", system_identifier=cluster_identifier(), directories=initial)
        os.fsync(data_descriptor)
        os.fsync(base_descriptor)
        save_owner(owner)
    finally:
        if data_descriptor is not None:
            os.close(data_descriptor)
        os.close(base_descriptor)


def main(arguments=None):
    arguments = sys.argv[1:] if arguments is None else arguments
    require(arguments in ([], ["--check"], ["--stop"]),
            "Usage: prepare-postgres-workspace.py [--check | --stop]")
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
            "Run through SSH as the authorized test-env root operator.")
    os.umask(0o077)
    postgres = pwd.getpwnam("postgres")
    verify_parents(postgres)
    expected = expected_owner(postgres, binaries())
    fresh = not present(CONTROL)
    if fresh:
        require(not arguments, "No owned workspace exists; no process or directory was changed.")
        require(not present(BASE), "The new data path already exists without ownership; refusing adoption.")
        port_available()
        CONTROL.mkdir(mode=0o700)
    control_identity = directory(CONTROL, 0, 0)
    if not fresh:
        regular(OWNER)
    lock = acquire_lock(create=fresh)
    try:
        if fresh:
            owner = dict(expected, control_identity=control_identity, directories={},
                         system_identifier=None, process=None, phase="initializing")
            create_private(OWNER, json.dumps(owner, sort_keys=True) + "\n")
            sync_directory(CONTROL)
            initialize(postgres, owner)
        else:
            owner = json.loads(read_file(OWNER), object_pairs_hook=strict_object)
        validate_owner(owner, expected, control_identity)
        verify_directories(postgres, owner)
        verify_configuration(postgres)
        require(cluster_identifier() == owner["system_identifier"],
                "The on-disk cluster system identifier differs from the external owner record.")
        process = verify_process(postgres, owner)
        if arguments == ["--stop"]:
            if process is not None:
                verify_server(owner)
                stop_process(postgres, owner, process)
            require_stopped(postgres)
            owner["process"] = None
            save_owner(owner)
            print("The verified persistent workspace is stopped. All data, ownership records, and credentials remain.")
            return
        if process is None:
            require_stopped(postgres)
            if arguments == ["--check"]:
                print("Persistent cluster ownership, system identifier, configuration, and stopped state verified.")
                return
            postgres_command([BIN / "pg_ctl", "-D", DATA, "-l", LOG / "postgres.log", "-w", "-t", "30", "start"])
            process = verify_process(postgres, owner, newly_started=True)
            require(process is not None, "The started cluster has no verified process identity.")
            owner["process"] = process
            save_owner(owner)
        verify_server(owner)
        prepare_role(check_only=arguments == ["--check"])
        require(verify_process(postgres, owner) == process, "The cluster process changed during preparation.")
        print(f"PostgreSQL {PG_VERSION} persistent workspace ready on 127.0.0.1:{PORT}; PID {process['pid']} identity verified.")
        print("Role goby_test: LOGIN, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, NOBYPASSRLS; no memberships; SCRAM login verified.")
        print(f"External control: {CONTROL} (root:root 0700). Credentials: {ENV_FILE} (root:root 0600).")
        print(f"Persistent data, socket, and log: {BASE} (postgres:postgres 0700).")
        print("Load /opt/goby-test/test.env first, then the dedicated credential file under set -a. Credential values are never printed.")
        print("Reuse after reboot: run this operator again. Safe inspection: --check. Safe stop preserving data: --stop.")
        print("Only the explicitly recorded cluster is managed. No shared service, old scratch record, or parent permission was changed.")
    finally:
        os.close(lock)


if __name__ == "__main__":
    try:
        main()
    except WorkspaceError as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
    except Exception as error:
        # Exception values can contain credentials, SQL, or captured stderr.
        print(f"Workspace preparation failed ({type(error).__name__}); inspect only its explicitly owned paths.", file=sys.stderr)
        sys.exit(1)
