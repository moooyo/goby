#!/usr/bin/env bash
# Prepare or stop only the explicitly owned PostgreSQL scratch cluster.
# Run through ssh test-env. This script never changes the host PostgreSQL service.
set -euo pipefail
set +x

python3 - "$@" <<'PY'
import fcntl
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shlex
import socket
import stat
import subprocess
import sys
import tempfile
import urllib.parse


class ScratchError(Exception):
    pass


BASE = Path("/dev/shm/goby-pg-m4c")
DATA = BASE / "data"
SOCKET = BASE / "socket"
LOG = BASE / "log"
CONTROL = Path("/opt/goby-test")
OWNER = CONTROL / "m4c-postgres.owner"
ENV_FILE = CONTROL / "m4c-test.env"
LOCK = CONTROL / "m4c-postgres.lock"
BIN = Path("/usr/lib/postgresql/17/bin")
PORT = 15432
MOUNT_SOURCE = "goby-pg-m4c"
MOUNT_BYTES = 1024 * 1024 * 1024
MARKER = "goby-postgres-scratch-m4c-v1"
PG_VERSION = "17.11"
BASE_ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}

CONF = f"""# Dedicated, disposable Goby PostgreSQL verification cluster.
listen_addresses = '127.0.0.1'
port = {PORT}
unix_socket_directories = '{SOCKET}'
unix_socket_permissions = 0700
cluster_name = 'goby-m4c-scratch'
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
HBA = """# Only local peer administration and the dedicated TCP test role.
local all postgres peer
local all all reject
host goby_test goby_test 127.0.0.1/32 scram-sha-256
host all all 0.0.0.0/0 reject
host all all ::0/0 reject
"""


def require(condition, message):
    if not condition:
        raise ScratchError(message)


def command(arguments, *, input_text=None, environment=None, timeout=40):
    try:
        result = subprocess.run(
            [str(value) for value in arguments], input=input_text, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
            env=environment or BASE_ENV, timeout=timeout,
        )
    except (OSError, subprocess.TimeoutExpired):
        raise ScratchError("A scratch-cluster command could not complete; no other service was changed.") from None
    require(result.returncode == 0, "A scratch-cluster command failed; inspect only its dedicated log directory.")
    return result.stdout.strip()


def private_regular(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and
            stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
            "A scratch control file is not a private, root-owned regular file.")
    return info


def read_private(path, limit=8192):
    info = private_regular(path)
    require(info.st_size <= limit, "A scratch control file exceeds its size limit.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "r", encoding="utf-8") as handle:
        return handle.read(limit + 1)


def create_private(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        handle.write(value)
        handle.flush()
        os.fsync(handle.fileno())


def save_owner(owner):
    private_regular(OWNER)
    temporary = CONTROL / ("m4c-postgres.owner.next-" + secrets.token_hex(8))
    try:
        create_private(temporary, json.dumps(owner, sort_keys=True) + "\n")
        os.replace(temporary, OWNER)
    finally:
        if temporary.exists():
            temporary.unlink()


def mount_info():
    result = subprocess.run(
        ["findmnt", "--json", "--mountpoint", str(BASE), "--output", "SOURCE,FSTYPE,TARGET,OPTIONS"],
        text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=BASE_ENV, check=False,
    )
    if result.returncode == 1:
        return None
    require(result.returncode == 0, "The scratch mount identity could not be read.")
    mounts = json.loads(result.stdout)["filesystems"]
    require(len(mounts) == 1, "The scratch path has an ambiguous mount identity.")
    return mounts[0]


def verify_mount(postgres):
    mounted = mount_info()
    require(mounted is not None and mounted["source"] == MOUNT_SOURCE and
            mounted["fstype"] == "tmpfs" and mounted["target"] == str(BASE),
            "The existing scratch mount does not match the owned tmpfs identity.")
    require({"rw", "nosuid", "nodev", "noexec"} <= set(mounted["options"].split(",")),
            "The scratch tmpfs is missing its required mount restrictions.")
    info = BASE.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == postgres.pw_uid and
            info.st_gid == postgres.pw_gid and stat.S_IMODE(info.st_mode) == 0o700,
            "The scratch mount must be a private directory owned by postgres.")
    filesystem = os.statvfs(BASE)
    require(filesystem.f_blocks * filesystem.f_frsize == MOUNT_BYTES,
            "The scratch tmpfs size does not match its owned configuration.")


def postgres_command(arguments, **kwargs):
    environment = dict(BASE_ENV, HOME="/var/lib/postgresql", TMPDIR=str(BASE))
    return command(["runuser", "--user", "postgres", "--", "env", "-i",
                    *[f"{key}={value}" for key, value in environment.items()], *arguments], **kwargs)


def cluster_identifier():
    output = postgres_command([BIN / "pg_controldata", DATA])
    match = re.search(r"^Database system identifier:\s+(\d+)$", output, re.MULTILINE)
    require(match is not None, "The scratch cluster system identifier is unavailable.")
    return match.group(1)


def verify_configuration():
    require((DATA / "PG_VERSION").read_text(encoding="ascii").strip() == "17",
            "The scratch data directory has a different PostgreSQL major version.")
    require((DATA / "postgresql.conf").read_text(encoding="utf-8") == CONF and
            (DATA / "pg_hba.conf").read_text(encoding="utf-8") == HBA,
            "The scratch PostgreSQL configuration was changed; refusing to overwrite it.")
    auto = (DATA / "postgresql.auto.conf").read_text(encoding="utf-8")
    require(all(not line.strip() or line.lstrip().startswith("#") for line in auto.splitlines()),
            "The scratch cluster has unexpected ALTER SYSTEM overrides.")


def verify_directories(postgres):
    for directory in (DATA, SOCKET, LOG):
        info = directory.lstat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == postgres.pw_uid and
                info.st_gid == postgres.pw_gid and stat.S_IMODE(info.st_mode) == 0o700,
                "A scratch cluster directory has unexpected ownership, mode, or type.")


def verify_process(postgres):
    pid_file = DATA / "postmaster.pid"
    if not pid_file.exists():
        return None
    lines = pid_file.read_text(encoding="utf-8").splitlines()
    require(len(lines) >= 6 and lines[0].isdigit(), "The scratch PID file is malformed.")
    pid = int(lines[0])
    require(pid > 1 and lines[1] == str(DATA) and lines[3] == str(PORT) and
            lines[4] == str(SOCKET) and lines[5] == "127.0.0.1",
            "The scratch PID file identifies a different cluster or port.")
    process = Path("/proc") / str(pid)
    if not process.exists():
        return None
    require(process.stat().st_uid == postgres.pw_uid and
            (process / "exe").resolve() == BIN / "postgres",
            "The recorded scratch PID belongs to another process; it will not be stopped.")
    arguments = (process / "cmdline").read_bytes().rstrip(b"\0").split(b"\0")
    require(arguments == [str(BIN / "postgres").encode(), b"-D", str(DATA).encode()],
            "The recorded process does not have the exact owned scratch command line.")
    listeners = command(["ss", "-H", "-ltnp", f"sport = :{PORT}"]).splitlines()
    require(len(listeners) == 1 and f"127.0.0.1:{PORT}" in listeners[0].split() and
            re.search(rf"\bpid={pid},", listeners[0]) is not None,
            "The scratch TCP listener does not belong to the verified cluster process.")
    return pid


def port_available():
    require(not command(["ss", "-H", "-ltnp", f"sport = :{PORT}"]),
            "TCP port 15432 already has a listener; no listener will be stopped or reused.")
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as probe:
        probe.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        try:
            probe.bind(("127.0.0.1", PORT))
        except OSError:
            raise ScratchError("TCP port 15432 is already in use; no listener will be stopped or reused.") from None


def require_stopped():
    port_available()
    expected = [str(BIN / "postgres").encode(), b"-D", str(DATA).encode()]
    for process in Path("/proc").iterdir():
        if not process.name.isdigit():
            continue
        try:
            if (process / "exe").resolve() != BIN / "postgres":
                continue
            arguments = (process / "cmdline").read_bytes().rstrip(b"\0").split(b"\0")
        except (FileNotFoundError, ProcessLookupError):
            continue
        require(arguments != expected,
                "An owned scratch PostgreSQL process is still live without a usable PID record; refusing to declare it stopped.")


def administrator(sql):
    return postgres_command([BIN / "psql", "-X", "--no-password", "-h", SOCKET,
                             "-p", PORT, "-U", "postgres", "-d", "postgres",
                             "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"], input_text=sql)


def credentials():
    if not ENV_FILE.exists():
        password = secrets.token_hex(32)
        url = f"postgresql://goby_test:{password}@127.0.0.1:{PORT}/goby_test?sslmode=disable"
        create_private(ENV_FILE, f"GOBY_DATABASE_URL='{url}'\nGOBY_TEST_DATABASE_URL='{url}'\n")
    values = {}
    for line in read_private(ENV_FILE).splitlines():
        name, separator, raw = line.partition("=")
        require(separator and name in ("GOBY_DATABASE_URL", "GOBY_TEST_DATABASE_URL") and name not in values,
                "The scratch environment has unexpected or duplicate settings.")
        parsed = shlex.split(raw)
        require(len(parsed) == 1, "A scratch connection setting is malformed.")
        values[name] = parsed[0]
    require(len(values) == 2 and values["GOBY_DATABASE_URL"] == values["GOBY_TEST_DATABASE_URL"],
            "The scratch connection settings do not match.")
    url = urllib.parse.urlsplit(values["GOBY_DATABASE_URL"])
    require(url.scheme == "postgresql" and url.username == "goby_test" and
            url.hostname == "127.0.0.1" and url.port == PORT and url.path == "/goby_test" and
            url.query == "sslmode=disable" and not url.fragment and
            re.fullmatch(r"[0-9a-f]{64}", url.password or "") is not None,
            "The scratch environment identifies an unexpected database or credential format.")
    return url.password


def check_tcp_login(password):
    descriptor, filename = tempfile.mkstemp(prefix="m4c-postgres-pass-", dir=CONTROL)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            handle.write(f"127.0.0.1:{PORT}:goby_test:goby_test:{password}\n")
        result = command([BIN / "psql", "-X", "--no-password", "-h", "127.0.0.1",
                          "-p", PORT, "-U", "goby_test", "-d", "goby_test",
                          "-v", "ON_ERROR_STOP=1", "-A", "-t", "-q"],
                         input_text="SELECT current_user, current_database(), inet_server_port();\n",
                         environment=dict(BASE_ENV, PGPASSFILE=filename, PGCONNECT_TIMEOUT="5"))
        require(result == f"goby_test|goby_test|{PORT}", "Scratch SCRAM login did not reach the expected database.")
    finally:
        Path(filename).unlink()


def prepare_role():
    role_exists = administrator("SELECT count(*) FROM pg_roles WHERE rolname = 'goby_test';\n") == "1"
    require(not role_exists or ENV_FILE.exists(),
            "The scratch role exists without its managed credential file; refusing password rotation.")
    password = credentials()
    if not role_exists:
        # The generated secret is sent through stdin, never argv or a log message.
        administrator("CREATE ROLE goby_test LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE "
                      "NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "';\n")
    db_owner = administrator("SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname = 'goby_test';\n")
    require(not db_owner or db_owner == "goby_test", "The scratch database has an unexpected owner.")
    if not db_owner:
        administrator("CREATE DATABASE goby_test OWNER goby_test;\n")
    flags = administrator("SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls "
                          "FROM pg_roles WHERE rolname = 'goby_test';\n")
    require(flags == "t|f|f|f|f|f", "The scratch test role has unexpected elevated privileges.")
    scram = administrator("SELECT rolpassword LIKE 'SCRAM-SHA-256$%' FROM pg_authid WHERE rolname = 'goby_test';\n")
    require(scram == "t", "The scratch test role does not have a SCRAM password verifier.")
    check_tcp_login(password)


def main():
    require(sys.argv[1:] in ([], ["--stop"]), "Usage: prepare-postgres-scratch.sh [--stop]")
    stop = sys.argv[1:] == ["--stop"]
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
            "Run through SSH as the authorized test-env root operator.")
    parent = CONTROL.lstat()
    require(stat.S_ISDIR(parent.st_mode) and parent.st_uid == 0 and stat.S_IMODE(parent.st_mode) == 0o700,
            "The existing Goby test control directory must be root-owned with mode 0700.")
    require((CONTROL / "test.env").is_file(), "The dedicated Goby test environment is missing.")
    if LOCK.exists() or LOCK.is_symlink():
        private_regular(LOCK)
    lock = os.open(LOCK, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        raise ScratchError("Another scratch-cluster preparation is running.") from None
    postgres = pwd.getpwnam("postgres")
    version = command([BIN / "postgres", "--version"])
    require(re.search(rf"\b{re.escape(PG_VERSION)}\b", version) is not None,
            "The installed PostgreSQL binary does not match the expected 17.11 baseline.")
    expected = {"marker": MARKER, "directory": str(BASE), "port": PORT,
                "binary": str(BIN / "postgres"), "version": PG_VERSION,
                "mount_source": MOUNT_SOURCE, "mount_bytes": MOUNT_BYTES,
                "uid": postgres.pw_uid, "gid": postgres.pw_gid}
    if not OWNER.exists() and not OWNER.is_symlink():
        require(not stop, "No owned scratch cluster is recorded; no process was stopped.")
        require(not BASE.exists() and not BASE.is_symlink() and not ENV_FILE.exists() and not ENV_FILE.is_symlink(),
                "Scratch paths already exist without an owner marker; refusing to claim them.")
        port_available()
        owner = dict(expected, system_identifier=None)
        create_private(OWNER, json.dumps(owner, sort_keys=True) + "\n")
    else:
        owner = json.loads(read_private(OWNER))
        require(all(owner.get(key) == value for key, value in expected.items()) and
                set(owner) == set(expected) | {"system_identifier"},
                "The scratch owner marker does not match this cluster configuration.")
    mounted = mount_info()
    if mounted is None:
        require(not stop and owner["system_identifier"] is None,
                "The recorded scratch cluster is not mounted; refusing automatic recreation or stop.")
        memory = dict(re.findall(r"^(\w+):\s+(\d+) kB$", Path("/proc/meminfo").read_text(), re.MULTILINE))
        require(int(memory["MemAvailable"]) * 1024 >= MOUNT_BYTES + 512 * 1024 * 1024,
                "Insufficient available memory for the dedicated 1 GiB scratch tmpfs and reserve.")
        if BASE.exists() or BASE.is_symlink():
            info = BASE.lstat()
            require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o700 and
                    not any(BASE.iterdir()), "The unmounted scratch directory has unexpected contents or ownership.")
        else:
            BASE.mkdir(mode=0o700)
        command(["mount", "-t", "tmpfs", "-o",
                 f"size={MOUNT_BYTES},nosuid,nodev,noexec,mode=0700,uid={postgres.pw_uid},gid={postgres.pw_gid}",
                 MOUNT_SOURCE, BASE])
    verify_mount(postgres)
    if owner["system_identifier"] is None:
        require(not stop and not any(BASE.iterdir()),
                "An unfinished scratch initialization has contents; inspect it without automatic deletion.")
        for directory in (DATA, SOCKET, LOG):
            directory.mkdir(mode=0o700)
            os.chown(directory, postgres.pw_uid, postgres.pw_gid)
        postgres_command([BIN / "initdb", "-D", DATA, "--username=postgres", "--locale=C.UTF-8",
                          "--encoding=UTF8", "--auth-local=peer", "--auth-host=scram-sha-256", "--data-checksums"], timeout=90)
        for filename, value in (("postgresql.conf", CONF), ("pg_hba.conf", HBA)):
            destination = DATA / filename
            destination.write_text(value, encoding="utf-8")
            os.chmod(destination, 0o600)
            os.chown(destination, postgres.pw_uid, postgres.pw_gid)
        owner["system_identifier"] = cluster_identifier()
        save_owner(owner)
    verify_directories(postgres)
    verify_configuration()
    require(cluster_identifier() == owner["system_identifier"],
            "The scratch data system identifier does not match its external owner record.")
    pid = verify_process(postgres)
    if stop:
        if pid is not None:
            postgres_command([BIN / "pg_ctl", "-D", DATA, "-m", "fast", "-w", "-t", "30", "stop"])
            require(not (DATA / "postmaster.pid").exists(), "The owned scratch cluster has not stopped.")
        require_stopped()
        print("The verified owned scratch cluster is stopped; its data and credentials remain available for reuse.")
        print("For intentional data disposal only: after checking the owner record and mount identity, unmount /dev/shm/goby-pg-m4c.")
        print("Preparation refuses to recreate recorded data after unmount/reboot. Review and remove only this cluster's control files before a fresh initialization.")
        return
    if pid is None:
        port_available()
        postgres_command([BIN / "pg_ctl", "-D", DATA, "-l", LOG / "postgres.log", "-w", "-t", "30", "start"])
        pid = verify_process(postgres)
        require(pid is not None, "The started scratch cluster has no verified process.")
    identity = administrator("SELECT current_setting('data_directory'), current_setting('port'), "
                             "current_setting('listen_addresses'), system_identifier FROM pg_control_system();\n")
    require(identity == f"{DATA}|{PORT}|127.0.0.1|{owner['system_identifier']}",
            "The connected PostgreSQL instance does not match the owned scratch cluster.")
    prepare_role()
    settings = administrator("SELECT current_setting('shared_buffers'), current_setting('max_connections'), "
                             "current_setting('min_wal_size'), current_setting('max_wal_size');\n")
    require(settings == "32MB|50|32MB|128MB", "The scratch server resource settings do not match.")
    print(f"PostgreSQL {PG_VERSION} scratch cluster ready on 127.0.0.1:{PORT}; verified PID {pid}.")
    print("Role goby_test: LOGIN, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, NOBYPASSRLS; SCRAM login verified.")
    print("Cluster/data/socket/log directories: postgres ownership, mode 0700. Connection override: /opt/goby-test/m4c-test.env, root mode 0600.")
    print("shared_buffers=32MB; max_connections=50; min_wal_size=32MB; max_wal_size=128MB.")
    print("max_wal_size is a checkpoint target, not a hard WAL limit; the dedicated 1 GiB tmpfs bounds aggregate files and can fill.")
    print(command(["du", "-sh", BASE]))
    print(command(["df", "-h", BASE, "/"]))
    print("Load the original test.env first, then m4c-test.env under set -a; PATH and FFmpeg settings are not overwritten.")
    print("Safe stop: rerun this script through ssh test-env with --stop; identity checks precede pg_ctl and no other service is stopped.")
    os.close(lock)


try:
    main()
except ScratchError as error:
    print(str(error), file=sys.stderr)
    sys.exit(1)
except Exception as error:
    # Exceptions can carry connection strings or SQL. Never print their values.
    print(f"Scratch preparation failed ({type(error).__name__}); inspect the owned paths without changing other services.", file=sys.stderr)
    sys.exit(1)
PY
