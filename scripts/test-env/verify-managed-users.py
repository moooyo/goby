#!/usr/bin/env python3
"""Run the managed-user browser journey in an owned, disposable Linux database.

This runner never connects to port 5432, builds software, installs dependencies,
or changes a shared application service. The one temporary HBA entry names only
the newly created database and role. Its exact original bytes are restored.
"""

import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import select
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tarfile
import time
import urllib.error
import urllib.parse
import urllib.request


CONTROL = Path("/opt/goby-test")
EXEC_ROOT = CONTROL / "exec-scratch"
PG_BASE = Path("/dev/shm/goby-pg-m4c")
PG_DATA = PG_BASE / "data"
PG_SOCKET = PG_BASE / "socket"
PG_HBA = PG_DATA / "pg_hba.conf"
PG_BIN = Path("/usr/lib/postgresql/17/bin")
PG_PORT = 15432
NODE_MODULES = Path("/dev/shm/goby-admin-ui/node_modules")
BROWSER_CACHE = Path("/root/.cache/ms-playwright")
MARKER = "goby-managed-users-browser-v1"
RUN_ENV = "GOBY_MANAGED_USERS_RUN_ID"
BASE_ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
BASE_HBA = b"""# Only local peer administration and the dedicated TCP test role.
local all postgres peer
local all all reject
host goby_test goby_test 127.0.0.1/32 scram-sha-256
host all all 0.0.0.0/0 reject
host all all ::0/0 reject
"""


class VerificationError(Exception):
    pass


def require(condition, message):
    if not condition:
        raise VerificationError(message)


def digest(value):
    return hashlib.sha256(value).hexdigest()


def file_digest(path):
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def private_file(path, *, uid=0, maximum=65536):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == uid and
            stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1 and
            info.st_size <= maximum, "A private control file has unexpected ownership or type.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as source:
        return source.read(maximum + 1)


def private_write(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as output:
        output.write(value)
        output.flush()
        os.fsync(output.fileno())


def command(arguments, *, text=None, environment=None, timeout=30):
    process = None
    try:
        process = subprocess.Popen([str(arg) for arg in arguments], text=True,
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                   stderr=subprocess.PIPE, env=environment or BASE_ENV,
                                   start_new_session=True)
        stdout, _ = process.communicate(input=text, timeout=timeout)
    except (OSError, subprocess.TimeoutExpired):
        raise VerificationError("A verification command did not complete.") from None
    finally:
        if process is not None and process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGTERM)
                process.communicate(timeout=1)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.communicate(timeout=5)
            except ProcessLookupError:
                process.wait(timeout=5)
    require(process.returncode == 0, "A verification command failed; command output was not published.")
    return stdout.strip()


def pg(statement, *, cleanup=False):
    environment = dict(BASE_ENV)
    if cleanup:
        environment["PGAPPNAME"] += "_cleanup"
    return command(["runuser", "--user", "postgres", "--", PG_BIN / "psql",
                    "-X", "-v", "ON_ERROR_STOP=1", "-At", "-h", PG_SOCKET,
                    "-p", str(PG_PORT), "-d", "postgres"], text=statement + "\n", environment=environment)


def pg_json(expression):
    return json.loads(pg(expression))


def catalog_snapshot():
    return pg_json("""SELECT jsonb_agg(jsonb_build_object(
        'name', datname, 'owner', pg_get_userbyid(datdba), 'allows_connections', datallowconn)
        ORDER BY datname) FROM pg_database;""")


def service_state():
    output = command(["systemctl", "show", "goby-foundation-test.service",
                      "--property=MainPID", "--property=ActiveState"])
    return dict(line.split("=", 1) for line in output.splitlines() if "=" in line)


def free_bytes(path):
    fs = os.statvfs(path)
    return fs.f_bavail * fs.f_frsize


def wait_until(predicate, message, timeout=30):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(0.1)
    raise VerificationError(message)


def request_json(origin, route, body=None):
    data = None if body is None else json.dumps(body).encode("utf-8")
    headers = {"Accept": "application/json", "Origin": origin}
    if data is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(origin + route, data=data, headers=headers)
    try:
        with urllib.request.urlopen(request, timeout=3) as response:
            require(response.status in (200, 201), "A dedicated application endpoint returned an unexpected status.")
            return json.loads(response.read(1024 * 1024))
    except (OSError, ValueError, urllib.error.HTTPError):
        raise VerificationError("A dedicated application request failed.") from None


def safe_remove(path, parent, marker_path):
    require(path.parent == parent and path.resolve() == path and
            marker_path.read_text(encoding="utf-8") == MARKER,
            "An owned cleanup path or marker changed; refusing recursive removal.")
    shutil.rmtree(path)


def sanitize_text(value, known_secrets):
    for secret in known_secrets:
        if secret:
            value = value.replace(secret, "[REDACTED]")
    value = re.sub(r"(?i)(postgres(?:ql)?://)[^\s\"']+", r"\1[REDACTED]", value)
    value = re.sub(r"(?i)((?:api_key|x-emby-token|x-mediabrowser-token|x-csrf-token|goby_session|access_token|accesstoken|password)\s*[:=]\s*)[^\s,;}\"']+", r"\1[REDACTED]", value)
    # Generated fixture tokens and passwords use base64url. User IDs are 32 hex
    # characters and need not be retained in diagnostic console output either.
    value = re.sub(r"(?<![A-Za-z0-9_-])[A-Za-z0-9_-]{32,}(?![A-Za-z0-9_-])", "[REDACTED]", value)
    return value


class Runner:
    browser_spec = "users-management.spec.ts"
    screenshot_names = ("users-management-desktop.png", "users-management-mobile.png")
    browser_timeout_seconds = 240

    def __init__(self, args):
        self.args = args
        self.run_id = time.strftime("%Y%m%d_%H%M%S", time.gmtime()) + "_" + secrets.token_hex(5)
        self.database = "goby_m5a_ui_" + self.run_id
        self.role = "goby_m5a_role_" + self.run_id
        self.tag = MARKER + ":" + self.run_id
        self.pg_app_name = "goby_m5a_control_" + self.run_id
        BASE_ENV["PGAPPNAME"] = self.pg_app_name
        self.start_ticks = int(time.clock_gettime(time.CLOCK_BOOTTIME) * os.sysconf("SC_CLK_TCK"))
        self.output = EXEC_ROOT / ("goby-managed-users-" + self.run_id)
        self.runtime = Path("/dev/shm") / ("goby-managed-users-" + self.run_id)
        self.role_password = secrets.token_urlsafe(32)
        self.admin_name = "m5a-admin-" + self.run_id
        self.admin_password = secrets.token_urlsafe(32)
        self.setup_token = secrets.token_urlsafe(32)
        self.secrets = [self.role_password, self.admin_password, self.setup_token]
        self.report = {"run_id": self.run_id, "status": "running", "database_port": PG_PORT,
                       "database": self.database, "role": self.role, "checks": {}, "cleanup": {}, "cleanup_errors": {}}
        self.lock = None
        self.binary_fd = None
        self.app = None
        self.browser = None
        self.app_log = None
        self.role_intent = False
        self.database_intent = False
        self.database_oid = None
        self.hba_restore_intent = False
        self.hba_original = None
        self.hba_temporary = None
        self.runtime_created = False
        self.output_created = False
        self.postgres = None
        self.goby = None

    def allocate(self):
        require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
                "Run this verifier through SSH as the authorized test-env operator.")
        require(CONTROL.resolve() == CONTROL and EXEC_ROOT.resolve() == EXEC_ROOT,
                "The verification control directory has an unexpected resolved location.")
        require(CONTROL.stat().st_uid == 0 and stat.S_IMODE(CONTROL.stat().st_mode) == 0o700,
                "The verification control directory must be private and root owned.")
        require(free_bytes(EXEC_ROOT) >= 64 * 1024 * 1024 and
                free_bytes(Path("/dev/shm")) >= 32 * 1024 * 1024 and
                free_bytes(PG_BASE) >= 64 * 1024 * 1024,
                "Insufficient scratch space for isolated browser verification.")
        self.postgres, self.goby = pwd.getpwnam("postgres"), pwd.getpwnam("goby")
        require(self.goby.pw_uid != 0, "The dedicated application account must be unprivileged.")
        require(self.args.snapshot.is_dir() and
                (self.args.snapshot / "web/admin/e2e" / self.browser_spec).is_file(),
                "The ready source snapshot is missing the selected browser spec.")
        require((NODE_MODULES / "@playwright/test/cli.js").is_file() and BROWSER_CACHE.is_dir(),
                "Reusable browser dependencies are unavailable; this verifier does not install software.")
        node_version = command(["/usr/bin/node", "--version"])
        match = re.fullmatch(r"v(\d+)\.(\d+)\.(\d+)", node_version)
        require(match is not None, "The reusable Node runtime did not report a valid version.")
        major, minor, _ = (int(value) for value in match.groups())
        require((major == 20 and minor >= 19) or (major == 22 and minor >= 12) or major >= 23,
                "The reusable Node runtime does not meet the administrator workspace engine requirement.")
        playwright = json.loads((NODE_MODULES / "@playwright/test/package.json").read_text())
        require(playwright.get("version") == "1.63.0", "The reusable Playwright package version differs from the locked dependency.")
        browser_manifests = json.loads((NODE_MODULES / "playwright-core/browsers.json").read_text())["browsers"]
        for browser_name, executable in (("chromium", "chrome"), ("chromium-headless-shell", "chrome-headless-shell")):
            entry = next((browser for browser in browser_manifests if browser["name"] == browser_name), None)
            require(entry is not None, "The locked browser manifest is incomplete.")
            directory = BROWSER_CACHE / (browser_name.replace("-", "_") + "-" + entry["revision"])
            matches = [path for path in directory.rglob(executable) if path.is_file() and os.access(path, os.X_OK)] if directory.is_dir() else []
            require(len(matches) == 1, "The exact reusable Chromium executable is missing or ambiguous.")
        binary = self.args.binary.resolve(strict=True)
        require(binary.is_relative_to(EXEC_ROOT), "Use the separately prepared executable in exec-scratch.")
        info = binary.stat()
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o111 and
                not info.st_mode & 0o022 and file_digest(binary) == self.args.binary_sha256,
                "The prepared application executable identity or permissions changed.")
        self.binary_fd = os.open(binary, os.O_RDONLY | os.O_NOFOLLOW)
        require(os.read(self.binary_fd, 4) == b"\x7fELF", "The application binary is not a Linux executable.")
        os.lseek(self.binary_fd, 0, os.SEEK_SET)
        self.output.mkdir(mode=0o700)
        self.output_created = True
        private_write(self.output / ".goby-managed", MARKER.encode())
        private_write(self.output / "owner.json", json.dumps({
            "marker": MARKER, "run_id": self.run_id, "database": self.database,
            "role": self.role, "runtime": str(self.runtime), "binary_sha256": self.args.binary_sha256,
        }, sort_keys=True).encode())
        self.runtime.mkdir(mode=0o750)
        self.runtime_created = True
        os.chown(self.runtime, self.goby.pw_uid, self.goby.pw_gid)
        private_write(self.runtime / ".goby-managed", MARKER.encode())
        self.report["binary_sha256"] = self.args.binary_sha256
        self.report["node_version"] = node_version
        self.report["playwright_version"] = playwright["version"]
        self.report["source_spec_sha256"] = file_digest(self.args.snapshot / "web/admin/e2e" / self.browser_spec)
        self.report["shared_service_before"] = service_state()
        self.report["space_before"] = {str(path): free_bytes(path) for path in (Path("/"), Path("/dev/shm"), EXEC_ROOT, PG_BASE)}

    def prepare_database(self):
        lock_path = CONTROL / "m4c-postgres.lock"
        if lock_path.exists() or lock_path.is_symlink():
            private_file(lock_path)
        self.lock = os.open(lock_path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
        try:
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise VerificationError("Another scratch PostgreSQL preparation holds its ownership lock.") from None
        owner = json.loads(private_file(CONTROL / "m4c-postgres.owner"))
        require(owner.get("marker") == "goby-postgres-scratch-m4c-v1" and
                owner.get("directory") == str(PG_BASE) and owner.get("port") == PG_PORT and
                owner.get("uid") == self.postgres.pw_uid,
                "The existing scratch cluster owner record does not match.")
        actual = pg_json("""SELECT jsonb_build_object('data', current_setting('data_directory'),
            'port', current_setting('port'), 'hba', current_setting('hba_file'),
            'system_identifier', system_identifier::text) FROM pg_control_system();""")
        require(actual == {"data": str(PG_DATA), "port": str(PG_PORT), "hba": str(PG_HBA),
                           "system_identifier": str(owner["system_identifier"])},
                "The connected database is not the expected port-15432 scratch cluster.")
        self.hba_original = private_file(PG_HBA, uid=self.postgres.pw_uid)
        require(self.hba_original == BASE_HBA, "The scratch HBA differs from its reviewed baseline; no rules were changed.")
        self.report["hba_original_sha256"] = digest(self.hba_original)
        self.report["databases_before"] = catalog_snapshot()
        require(pg(f"SELECT count(*) FROM pg_database WHERE datname = '{self.database}';") == "0" and
                pg(f"SELECT count(*) FROM pg_roles WHERE rolname = '{self.role}';") == "0",
                "The generated database or role already exists; refusing to claim it.")
        self.role_intent = True
        pg(f"BEGIN;\nSET password_encryption = 'scram-sha-256';\nCREATE ROLE {self.role} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT 20 PASSWORD '{self.role_password}';\nCOMMENT ON ROLE {self.role} IS '{self.tag}';\nCOMMIT;")
        self.database_intent = True
        pg(f"CREATE DATABASE {self.database} OWNER {self.role} TEMPLATE template0 ENCODING 'UTF8';")
        self.database_oid = int(pg(f"SELECT oid FROM pg_database WHERE datname = '{self.database}';"))
        pg(f"COMMENT ON DATABASE {self.database} IS '{self.tag}';\nREVOKE CONNECT, TEMPORARY ON DATABASE {self.database} FROM PUBLIC;")
        rule = f"# {self.tag}\nhost {self.database} {self.role} 127.0.0.1/32 scram-sha-256\n".encode()
        self.hba_temporary = rule + self.hba_original
        self.hba_restore_intent = True
        self.replace_hba(self.hba_original, self.hba_temporary)
        require(pg("SELECT pg_reload_conf();") == "t", "The temporary exact HBA entry could not be reloaded.")
        require(pg("SELECT count(*) FROM pg_hba_file_rules WHERE error IS NOT NULL;") == "0",
                "The temporary HBA rule did not parse.")
        flags = pg_json(f"""SELECT jsonb_build_object('login', rolcanlogin, 'superuser', rolsuper,
            'create_db', rolcreatedb, 'create_role', rolcreaterole, 'replication', rolreplication,
            'bypass_rls', rolbypassrls) FROM pg_roles WHERE rolname = '{self.role}';""")
        require(flags == {"login": True, "superuser": False, "create_db": False,
                          "create_role": False, "replication": False, "bypass_rls": False},
                "The new application database role has unexpected privileges.")
        self.report["checks"]["dedicated_low_privilege_role"] = flags
        self.report["checks"]["exact_temporary_hba_rule"] = True

    def replace_hba(self, expected, replacement):
        require(private_file(PG_HBA, uid=self.postgres.pw_uid) == expected,
                "The HBA changed outside this runner; refusing to overwrite it.")
        temporary = PG_DATA / (".managed-users-hba-" + self.run_id)
        private_write(temporary, replacement)
        os.chown(temporary, self.postgres.pw_uid, self.postgres.pw_gid)
        try:
            require(private_file(PG_HBA, uid=self.postgres.pw_uid) == expected,
                    "The HBA changed before replacement; refusing to overwrite it.")
            os.replace(temporary, PG_HBA)
        finally:
            if temporary.exists():
                temporary.unlink()

    def prepare_files(self):
        assets = self.runtime / "admin"
        assets.mkdir(mode=0o755)
        require(self.args.assets.is_file(), "The prepared administrator asset archive is missing.")
        self.report["assets_archive_sha256"] = file_digest(self.args.assets)
        total = 0
        with tarfile.open(self.args.assets, "r:*") as archive:
            for member in archive.getmembers():
                relative = Path(member.name)
                require(not relative.is_absolute() and ".." not in relative.parts and
                        (member.isfile() or member.isdir()), "The asset archive contains an unsafe entry.")
                total += member.size
                require(total <= 16 * 1024 * 1024, "The asset archive exceeds the isolated browser budget.")
                destination = assets / relative
                if member.isdir():
                    destination.mkdir(mode=0o755, parents=True, exist_ok=True)
                else:
                    destination.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
                    with archive.extractfile(member) as source, destination.open("xb") as target:
                        shutil.copyfileobj(source, target)
                    destination.chmod(0o644)
        require((assets / "index.html").is_file(), "The asset archive does not contain the dashboard root.")
        media = self.runtime / "media"
        media.mkdir(mode=0o750)
        os.chown(media, self.goby.pw_uid, self.goby.pw_gid)
        self.browser_work = self.output / "browser"
        self.browser_work.mkdir(mode=0o700)
        (self.browser_work / "e2e").mkdir(mode=0o700)
        for relative in ("package.json", "playwright.config.ts", "e2e/" + self.browser_spec):
            shutil.copyfile(self.args.snapshot / "web/admin" / relative, self.browser_work / relative)
        (self.browser_work / "node_modules").symlink_to(NODE_MODULES, target_is_directory=True)
        self.browser_temp = self.output / "browser-temp"
        self.browser_temp.mkdir(mode=0o700)
        self.results = self.output / "browser-results"
        self.results.mkdir(mode=0o700)
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            self.port = listener.getsockname()[1]
        require(self.port not in (5432, PG_PORT, 18096, 18097), "The temporary HTTP port is reserved.")
        self.origin = f"http://127.0.0.1:{self.port}"
        self.report["http_port"] = self.port

    def start_app(self):
        uri = f"postgresql://{self.role}:{self.role_password}@127.0.0.1:{PG_PORT}/{self.database}?sslmode=disable"
        environment = dict(BASE_ENV, **{
            "GOBY_LISTEN": f"127.0.0.1:{self.port}", "GOBY_PUBLIC_URL": self.origin,
            "GOBY_DATABASE_URL": uri, "GOBY_COOKIE_SECURE": "false",
            "GOBY_SETUP_TOKEN": self.setup_token, "GOBY_SERVER_NAME": "Goby managed-user verification",
            "GOBY_WEB_DIR": str(self.runtime / "admin"), "GOBY_MEDIA_ROOTS": str(self.runtime / "media"),
            "GOBY_TRANSCODING_ENABLED": "false", "GOBY_STARTUP_TIMEOUT": "30s",
            "GOBY_FFMPEG": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg",
            "GOBY_FFPROBE": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe", RUN_ENV: self.run_id,
            "TMPDIR": str(self.runtime),
        })
        self.app_log = (self.output / "app-private.log").open("ab")
        os.chmod(self.output / "app-private.log", 0o600)
        # The open executable descriptor avoids granting the goby UID access
        # to the shared root-private executable scratch directory.
        self.app = subprocess.Popen([str(self.args.binary)], executable=f"/proc/self/fd/{self.binary_fd}",
                                    pass_fds=(self.binary_fd,), env=environment, cwd=self.runtime,
                                    user=self.goby.pw_uid, group=self.goby.pw_gid, extra_groups=[],
                                    stdin=subprocess.DEVNULL, stdout=self.app_log, stderr=self.app_log,
                                    start_new_session=True)
        def ready():
            require(self.app.poll() is None, "The isolated application exited before readiness.")
            try:
                with urllib.request.urlopen(self.origin + "/readyz", timeout=1) as response:
                    return response.status == 200
            except OSError:
                return False
        wait_until(ready, "The isolated application did not become ready.")
        status = Path(f"/proc/{self.app.pid}/status").read_text()
        uids = [int(value) for value in re.search(r"^Uid:\s+(.+)$", status, re.MULTILINE).group(1).split()]
        require(uids == [self.goby.pw_uid] * 4, "The isolated application did not retain its unprivileged UID.")
        self.report["checks"]["application_uid"] = self.goby.pw_uid
        self.report["application_pids"] = self.report.get("application_pids", []) + [self.app.pid]

    def bootstrap(self):
        require(request_json(self.origin, "/admin/v1/bootstrap") == {"Initialized": False},
                "The dedicated database was not fresh before bootstrap.")
        created = request_json(self.origin, "/admin/v1/bootstrap", {
            "SetupToken": self.setup_token, "Name": self.admin_name, "Password": self.admin_password,
        })
        require(created.get("User", {}).get("Name") == self.admin_name and
                created["User"].get("IsAdministrator") is True,
                "The dedicated administrator was not created correctly.")
        self.report["checks"]["fresh_dedicated_administrator"] = True

    def browser_environment(self):
        return dict(BASE_ENV, **{
            "PLAYWRIGHT_BROWSERS_PATH": str(BROWSER_CACHE), "CI": "1", "FORCE_COLOR": "0",
            "GOBY_SMOKE_USERS_DISPOSABLE_DATABASE": "1", "GOBY_SMOKE_USERS_DEDICATED_ADMIN": "1",
            "GOBY_SMOKE_BASE_URL": self.origin, "GOBY_SMOKE_NAME": self.admin_name,
            "GOBY_SMOKE_PASSWORD": self.admin_password, "GOBY_SMOKE_MEDIA_PATH": str(self.runtime / "media"),
            "TMPDIR": str(self.browser_temp), RUN_ENV: self.run_id,
        })

    def run_browser(self):
        environment = self.browser_environment()
        stdout = self.output / "browser-private.json"
        stderr = self.output / "browser-private.stderr"
        with stdout.open("wb") as out, stderr.open("wb") as err:
            stdout.chmod(0o600)
            stderr.chmod(0o600)
            self.browser = subprocess.Popen(["/usr/bin/node", str(NODE_MODULES / "@playwright/test/cli.js"),
                                             "test", "e2e/" + self.browser_spec, "--config", "playwright.config.ts",
                                             "--reporter=json", "--output", str(self.results)],
                                            cwd=self.browser_work, env=environment, stdin=subprocess.DEVNULL,
                                            stdout=out, stderr=err, start_new_session=True)
            try:
                code = self.browser.wait(timeout=self.browser_timeout_seconds)
            except subprocess.TimeoutExpired:
                raise VerificationError("The isolated browser journey exceeded its time budget.") from None
        raw = stdout.read_text(encoding="utf-8")
        self.report["browser_driver_stderr"] = sanitize_text(stderr.read_text(encoding="utf-8"), self.secrets)[:8192]
        try:
            result = json.loads(raw)
        except ValueError:
            self.report["browser_driver_stdout"] = sanitize_text(raw, self.secrets)[:8192]
            raise VerificationError("The browser runner did not produce its structured report.") from None
        stats = result.get("stats", {})
        self.report["browser"] = {"exit_code": code, "expected": stats.get("expected"),
                                  "unexpected": stats.get("unexpected"), "skipped": stats.get("skipped"),
                                  "flaky": stats.get("flaky"), "duration_ms": stats.get("duration")}
        failures = []
        def collect(suite):
            for spec in suite.get("specs", []):
                for test in spec.get("tests", []):
                    for attempt in test.get("results", []):
                        for error in attempt.get("errors", []):
                            failures.append(sanitize_text(error.get("message", "Browser assertion failed."), self.secrets))
            for child in suite.get("suites", []):
                collect(child)
        for suite in result.get("suites", []):
            collect(suite)
        failures.extend(sanitize_text(error.get("message", "Browser runner failed."), self.secrets)
                        for error in result.get("errors", []))
        self.report["browser"]["failures"] = [failure[:8192] for failure in failures[:8]]
        for name in self.screenshot_names:
            matches = list(self.results.rglob(name))
            require(len(matches) <= 1, "The browser emitted duplicate explicit screenshot artifacts.")
            if matches:
                shutil.copyfile(matches[0], self.output / name)
                (self.output / name).chmod(0o600)
        require(code == 0 and stats.get("expected") == 1 and stats.get("unexpected") == 0 and
                stats.get("skipped") == 0 and stats.get("flaky") == 0 and not result.get("errors"),
                "The selected isolated browser journey did not pass without skips.")

    def account_snapshot(self):
        statement = """SELECT jsonb_build_object(
            'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
            'libraries', (SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
            'roots', (SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
            'settings', (SELECT jsonb_agg(to_jsonb(s) ORDER BY key) FROM server_settings s))::text;"""
        value = command(["runuser", "--user", "postgres", "--", PG_BIN / "psql", "-X", "-v",
                         "ON_ERROR_STOP=1", "-At", "-h", PG_SOCKET, "-p", str(PG_PORT), "-d", self.database],
                        text=statement)
        return digest(value.encode())

    def verify_restart(self):
        before = self.account_snapshot()
        self.stop_app()
        self.start_app()
        require(request_json(self.origin, "/admin/v1/bootstrap") == {"Initialized": True},
                "The application restart lost its initialized state.")
        require(self.account_snapshot() == before, "The application restart changed persisted account or library data.")
        self.report["checks"]["restart_preserved_account_and_library_snapshot"] = before

    def stop_app(self):
        if self.app is not None and self.app.poll() is None:
            self.app.terminate()
            try:
                self.app.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.app.kill()
                self.app.wait(timeout=5)
                raise VerificationError("The isolated application required forced shutdown.") from None
        if self.app_log is not None:
            self.app_log.close()
            self.app_log = None

    def stop_tagged_processes(self):
        marker = (RUN_ENV + "=" + self.run_id).encode()
        deadline = time.monotonic() + 15
        while True:
            handles = []
            for entry in Path("/proc").iterdir():
                if not entry.name.isdecimal():
                    continue
                descriptor = None
                try:
                    # Existing host processes cannot belong to this run. Check
                    # age and UID before inspecting a bounded environment.
                    if entry.stat().st_uid not in (0, self.goby.pw_uid if self.goby else -1):
                        continue
                    fields = (entry / "stat").read_text().rsplit(") ", 1)[1].split()
                    if int(fields[19]) < self.start_ticks:
                        continue
                    descriptor = os.pidfd_open(int(entry.name))
                    with (entry / "environ").open("rb") as source:
                        environment = source.read(1024 * 1024).split(b"\0")
                    if marker in environment:
                        signal.pidfd_send_signal(descriptor, signal.SIGTERM)
                        handles.append(descriptor)
                        descriptor = None
                except (OSError, ValueError, IndexError):
                    pass
                finally:
                    if descriptor is not None:
                        os.close(descriptor)
            if not handles:
                break
            poller = select.poll()
            pending = set(handles)
            for descriptor in handles:
                poller.register(descriptor, select.POLLIN)
            grace = min(deadline, time.monotonic() + 1)
            while pending and time.monotonic() < grace:
                pending.difference_update(descriptor for descriptor, _ in poller.poll(50))
            for descriptor in pending:
                try:
                    signal.pidfd_send_signal(descriptor, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            while pending and time.monotonic() < deadline:
                pending.difference_update(descriptor for descriptor, _ in poller.poll(50))
            for descriptor in handles:
                os.close(descriptor)
            require(not pending and time.monotonic() < deadline,
                    "A tagged descendant did not exit before the cleanup deadline.")
            # Repeat after all observed processes exit: descendants created
            # during the first scan must also be found and joined.
        if self.browser is not None:
            try:
                self.browser.wait(timeout=5)
            except subprocess.TimeoutExpired:
                raise VerificationError("A tagged browser process did not stop.") from None

    def cleanup(self):
        errors = []
        def attempt(name, function):
            try:
                function()
                self.report["cleanup"][name] = True
            except Exception as error:
                self.report["cleanup"][name] = False
                self.report.setdefault("cleanup_errors", {})[name] = str(error) if isinstance(error, VerificationError) else type(error).__name__
                errors.append(name)
        attempt("application_stopped", self.stop_app)
        attempt("tagged_processes_stopped", self.stop_tagged_processes)
        if self.role_intent or self.database_intent or self.hba_restore_intent:
            # An interrupted psql client can leave its server command finishing.
            # Fence only this run's uniquely named control connections first.
            def stop_control_connections():
                pg(f"SELECT pg_terminate_backend(pid, 5000) FROM pg_stat_activity WHERE application_name = '{self.pg_app_name}' AND pid <> pg_backend_pid();", cleanup=True)
                wait_until(lambda: pg(f"SELECT count(*) FROM pg_stat_activity WHERE application_name = '{self.pg_app_name}' AND pid <> pg_backend_pid();", cleanup=True) == "0",
                           "A private control connection remained active during cleanup.", timeout=10)
            attempt("inflight_control_connections_stopped", stop_control_connections)
        control_fenced = self.report["cleanup"].get("inflight_control_connections_stopped", True)
        if self.database_intent and control_fenced:
            def drop_database():
                owner = pg_json(f"SELECT COALESCE((SELECT jsonb_build_object('oid', oid, 'owner', pg_get_userbyid(datdba), 'tag', shobj_description(oid, 'pg_database')) FROM pg_database WHERE datname = '{self.database}'), 'null'::jsonb);")
                if owner is None:
                    return
                # CREATE DATABASE cannot include COMMENT atomically. The name
                # was proven absent before its intent, and its newly created
                # owner role is already transactionally tagged to this run.
                require(owner["owner"] == self.role and owner["tag"] in (None, self.tag) and
                        (self.database_oid is None or int(owner["oid"]) == self.database_oid) and
                        pg(f"SELECT shobj_description(oid, 'pg_authid') FROM pg_roles WHERE rolname = '{self.role}';") == self.tag,
                        "The disposable database ownership changed.")
                pg(f"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '{self.database}' AND pid <> pg_backend_pid();")
                pg(f"DROP DATABASE {self.database};")
                require(pg(f"SELECT count(*) FROM pg_database WHERE datname = '{self.database}';") == "0", "The disposable database remains present.")
            attempt("dedicated_database_removed", drop_database)
        if self.role_intent and control_fenced:
            def drop_role():
                if pg(f"SELECT count(*) FROM pg_roles WHERE rolname = '{self.role}';") == "0":
                    return
                require(pg(f"SELECT shobj_description(oid, 'pg_authid') FROM pg_roles WHERE rolname = '{self.role}';") == self.tag,
                        "The disposable role ownership tag changed.")
                pg(f"DROP ROLE {self.role};")
                require(pg(f"SELECT count(*) FROM pg_roles WHERE rolname = '{self.role}';") == "0", "The disposable role remains present.")
            attempt("dedicated_role_removed", drop_role)
        if self.hba_restore_intent:
            def restore_hba():
                current = private_file(PG_HBA, uid=self.postgres.pw_uid)
                require(current in (self.hba_original, self.hba_temporary),
                        "The HBA changed outside this runner; refusing restoration over unrelated edits.")
                if current == self.hba_temporary:
                    self.replace_hba(self.hba_temporary, self.hba_original)
                require(pg("SELECT pg_reload_conf();") == "t", "The original HBA reload failed.")
                require(private_file(PG_HBA, uid=self.postgres.pw_uid) == self.hba_original,
                        "The HBA was not restored exactly.")
                self.report["hba_restored_sha256"] = digest(self.hba_original)
            attempt("hba_restored_exactly", restore_hba)
        if self.report.get("databases_before") is not None:
            def verify_catalog():
                after = catalog_snapshot()
                self.report["databases_after"] = after
                require(after == self.report["databases_before"], "The pre-existing database catalog changed.")
            attempt("preexisting_database_catalog_unchanged", verify_catalog)
        if self.report.get("shared_service_before") is not None:
            def verify_service():
                after = service_state()
                self.report["shared_service_after"] = after
                require(after == self.report["shared_service_before"], "The shared application service changed during verification.")
            attempt("shared_application_service_unchanged", verify_service)
        processes_stopped = self.report["cleanup"].get("application_stopped") and self.report["cleanup"].get("tagged_processes_stopped")
        if self.runtime_created and processes_stopped:
            attempt("application_runtime_removed", lambda: safe_remove(self.runtime, Path("/dev/shm"), self.runtime / ".goby-managed"))
        if self.output_created and processes_stopped:
            def scrub_transient_files():
                for name in ("browser", "browser-temp", "browser-results"):
                    path = self.output / name
                    if path.exists():
                        safe_remove(path, self.output, self.output / ".goby-managed")
                private_logs = [self.output / name for name in ("app-private.log", "browser-private.json", "browser-private.stderr")]
                for path in private_logs:
                    if path.exists():
                        if self.report["status"] != "passed" and path.name == "app-private.log":
                            self.report["application_diagnostics"] = sanitize_text(path.read_text(encoding="utf-8", errors="replace"), self.secrets)[-8192:]
                        path.unlink()
            attempt("credential_bearing_temporary_files_removed", scrub_transient_files)
        if self.binary_fd is not None:
            os.close(self.binary_fd)
            self.binary_fd = None
        if self.lock is not None:
            os.close(self.lock)
            self.lock = None
        if errors:
            self.report["status"] = "failed"
            self.report["cleanup_failures"] = errors

    def execute(self):
        try:
            self.allocate()
            self.prepare_database()
            self.prepare_files()
            self.start_app()
            self.bootstrap()
            self.run_browser()
            self.verify_restart()
            self.report["status"] = "passed"
        except VerificationError as error:
            self.report["status"] = "failed"
            self.report["failure"] = str(error)
        except Exception as error:
            self.report["status"] = "failed"
            self.report["failure"] = "Unexpected verifier failure: " + type(error).__name__
        finally:
            # A second terminal signal must not interrupt the HBA/database
            # restoration already in progress. Individual commands are bounded.
            for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
                signal.signal(signum, signal.SIG_IGN)
            self.cleanup()
            if self.output_created:
                private_write(self.output / "report.json", (json.dumps(self.report, indent=2, sort_keys=True) + "\n").encode())
                print(json.dumps({"status": self.report["status"], "report": str(self.output / "report.json")}))
            else:
                print(json.dumps({"status": "failed", "failure": self.report.get("failure", "Preflight failed.")}))
        return 0 if self.report["status"] == "passed" else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", required=True)
    parser.add_argument("--assets", type=Path, required=True)
    args = parser.parse_args()
    require(re.fullmatch(r"[0-9a-f]{64}", args.binary_sha256) is not None, "Supply the prepared binary SHA-256.")
    def interrupted(signum, frame):
        raise VerificationError("The verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return Runner(args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except VerificationError as error:
        print(json.dumps({"status": "failed", "failure": str(error)}))
        sys.exit(1)
