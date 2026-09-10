#!/usr/bin/env python3
"""Verify one explicitly accepted M5i main deployment through root SSH only.

Require GOBY_OBSERVABILITY_EXPECTED_PID, GOBY_OBSERVABILITY_EXPECTED_BINARY_SHA256,
GOBY_OBSERVABILITY_EXPECTED_SOURCE_SHA256, and
GOBY_OBSERVABILITY_SOURCE_EVIDENCE_FILE. The evidence path must be exactly the
canonical root-0600 /opt/goby-test/exec-scratch/m5i-candidate.json. No candidate
hash, process identity, or older deployment is assumed. Select a fresh decimal
GOBY_OBSERVABILITY_VERIFICATION_RUN in 1..99, default 1. Exclusive attempt and
aggregate report files are retained under /opt/goby-test; never overwrite them.
Before the sole mutation, retain an exclusive root-0600 settings recovery intent
containing the original complete settings and fixed target revisions, but no
password, token, CSRF value or diagnostic body. Public reports name its hash.

Use one fresh native administrator cookie and one fresh ordinary Emby
administrator credential from the existing private browser.env login. Exercise
the four native and four Emby observation routes, native download HEAD/Range,
and the explicit Emby download HEAD-404 contract. Download only a basename
actually listed by both APIs, with a one-MiB response limit. Inspect JSONL in
memory, report only byte counts and hashes, and reject this workflow's secret
values in activity or diagnostic output. Never place credentials in a URL.

Perform one native CAS name replacement, preserving all numeric overrides and
the independent Encoding value, then restore the complete original Overrides,
ServerNameMode and Encoding at the fixed original-plus-one revision. Restoration
is authorized only by a fresh matching full SQL row and published native DTO.
Never adopt another writer's revision or retry the initial mutation. At most two
restore attempts use the same original-plus-one CAS revision. A timed-out write
followed by old state is not proof of no commit: final proof is read only after
the native credential's committed revocation barrier. No Emby settings write,
key, task, scan, media request, service restart, SQL write or deletion is issued.

Successful new history is exactly two revoked sessions, one ordinary device,
two settings.updated activities and four session activities. Preserve every old
activity row and every old row in the other twenty-eight tables, except the
singleton's two expected revision/time advances after complete restoration.
Only the owned ordinary session/device may receive real last_seen_at Touches.
Preserve the process, startup inputs, vault, and all media/NFO identities/bytes.

The main phase has sixty seconds. Cleanup has an independent forty-five-second
budget, with separate CSRF/binding, restore, ordinary logout, native logout and
final-proof slices. Every acknowledged credential is attempted independently on
success and failure. An unknown credential is never guessed or reused. All
database helpers force read-only PostgreSQL. Imported historical helpers are
inert; this file never calls a historical verifier's main function.
"""

from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timedelta, timezone
import base64
import hashlib
import http.client
from http.cookies import SimpleCookie
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qsl, quote, urlencode, urlsplit


sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test")
EVIDENCE = ROOT / "exec-scratch/m5i-candidate.json"
LOG_DIRECTORY = Path("/var/log/goby-test")
OBSERVABILITY_DROPIN = Path("/etc/systemd/system/goby-foundation-test.service.d/30-observability.conf")
OBSERVABILITY_BYTES = (b"[Service]\nEnvironment=GOBY_LOG_DIR=/var/log/goby-test\n"
                       b"LogsDirectory=goby-test\nLogsDirectoryMode=0700\n")
ORIGIN = "http://127.0.0.1:18096"
OWNER = "goby-observability-deployed-m5i-v1"
SESSION, SETTINGS = "/admin/v1/session", "/admin/v1/settings"
LOGIN, LOGOUT = "/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"
ACTIVITY, LOGS = "/admin/v1/activity", "/admin/v1/logs"
EMBY_ACTIVITY, EMBY_LOGS = "/emby/System/ActivityLog/Entries", "/emby/System/Logs/Query"
ID, HASH, NUMBER, TOKEN = (re.compile(pattern) for pattern in
                         (r"[0-9a-f]{32}", r"[0-9a-f]{64}", r"[1-9][0-9]*", r"[A-Za-z0-9_-]{43}"))
LOG_NAME = re.compile(r"goby-[0-9a-f]{32}-[0-9a-f]{32}\.jsonl")
MAX_BODY = 1 << 20
MAX_HTTP = 70
STARTED = time.monotonic()
PHASE_END = STARTED + 60
FINAL_END = None
ACTIVITY_COLUMNS = set("id created_at action severity source actor_kind actor_id actor_credential_id resource_kind "
                       "resource_id request_id revision affected_count state changed_fields".split())
ACTIVITY_FIELDS = set("Id Date Action Severity Source Actor Resource Revision Count State ChangedFields Name Overview".split())
NATIVE_LOG_FIELDS = {"Name", "DateCreated", "DateModified", "Size"}
LOG_STATUS_FIELDS = set("Healthy Degraded Closed MaxFileBytes MaxFiles RetentionDays MinFreeBytes Format".split())
SETTINGS_SECTIONS = ("Defaults", "Overrides", "Effective", "Sources", "Deployment", "ServerNameMode", "Encoding")
BASE_LOG_FIELDS = {"time", "level", "msg", "event"}
LOG_GROUPS = set("request http server transcode task library identity diagnostics context operation".split())
LOG_EVENTS = {
    "server.starting": ("server starting", "version database"),
    "server.listening": ("server listening", "version database"),
    "server.stopped": ("server stopped", "error_class"),
    "server.shutdown.completed": ("server shutdown completed", "duration_ms"),
    "server.shutdown.failed": ("background work did not close cleanly", "error_class"),
    "request.completed": ("request completed", "request_id method route status duration_ms bytes outcome"),
    "request.panic": ("request panic", "request_id"),
    "activity.retention.retry": ("activity retention will retry", "error_class"),
    "administrator.setup.completed": ("administrator setup completed", "user_id"),
    "user.created": ("user created", "actor_id user_id administrator"),
    "administrator.device.updated": ("administrator device options updated", "actor_id device_id"),
    "administrator.device.removed": ("administrator device removed", "actor_id device_id revoked_login_count"),
    "administrator.metadata.updated": ("administrator metadata mutation", "actor_id item_id revision"),
    "administrator.session.revoked": ("administrator session revocation", "actor_id user_id session_id kind"),
    "administrator.user.updated": ("administrator user mutation", "actor_id user_id action"),
    "application_key.created": ("application key created", "actor_credential_id application_key_id"),
    "application_key.revealed": ("application key revealed", "actor_credential_id application_key_id"),
    "application_key.revoked": ("application key revoked", "application_key_id"),
    "compatibility.device.updated": ("compatibility device options updated", "actor_credential_id device_id"),
    "compatibility.device.removed": ("compatibility device removed", "actor_credential_id device_id revoked_login_count"),
    "library.created": ("library created", "actor_id library_id"),
    "library.removed": ("library removed", "actor_id library_id"),
    "library.scan.requested": ("library scan requested", "actor_id library_id job_id force_probe"),
    "library.scan.finalization.retry": ("Retrying scan job finalization", "job_id error_class"),
    "transcode.completed": ("transcode completed", "job_id request_id item_id duration_ms bytes mode exit_code cancelled"),
    "transcode.failed": ("transcode failed", "job_id request_id item_id duration_ms mode exit_code cancelled error_class"),
    "unclassified": ("unclassified event", ""),
}
for _operation in ("task", "settings", "identity", "library", "compatibility configuration", "compatibility task"):
    LOG_EVENTS[_operation.replace(" ", ".") + ".operation.failed"] = (_operation + " operation failed", "request_id error_class")


class Failure(Exception):
    """Carry only a fixed label, never a response, SQL diagnostic or secret."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def remaining(maximum=3):
    available = PHASE_END - time.monotonic()
    check(available > 0.05, "The current bounded verification phase expired")
    return min(maximum, available)


def deadline(_number, _frame):
    raise Failure("The current bounded verification phase expired")


def phase(seconds):
    global PHASE_END
    PHASE_END = time.monotonic() + seconds
    if FINAL_END is not None:
        PHASE_END = min(PHASE_END, FINAL_END)
    signal.setitimer(signal.ITIMER_REAL, max(0.001, PHASE_END - time.monotonic()))


def cleanup_step(seconds, operation, errors, label=None):
    # Timers are armed inside this exception boundary and always disarmed
    # before returning. An expired earlier phase cannot interrupt another
    # credential's independent cleanup or report construction between steps.
    try:
        phase(seconds)
        operation()
        return True
    except BaseException:
        if label is not None:
            errors.append(label)
        return False
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


def helpers():
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        spec = importlib.util.spec_from_file_location("observability_deployed_inert", Path(__file__).with_name("verify-configuration-deployed.py"))
        check(spec is not None and spec.loader is not None, "The inert deployed helper is unavailable")
        configuration = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(configuration)
        settings, tasks, devices, core, utilities, sessions = configuration.helpers()
    core.STARTED = STARTED
    core.remaining = remaining
    core.TABLES |= {"activity_entries"}
    core.MUTABLE = {"sessions", "devices", "activity_entries"}
    return configuration, settings, tasks, devices, core, utilities, sessions


def output_files():
    value = os.environ.get("GOBY_OBSERVABILITY_VERIFICATION_RUN", "1")
    check(re.fullmatch(r"[1-9][0-9]?", value), "The verification run must be canonical decimal one through ninety-nine")
    stem = "m5i-deployed-observability" + ("" if value == "1" else "-attempt-" + value)
    return int(value), ROOT / (stem + ".json"), ROOT / (stem + ".attempt.json"), ROOT / (stem + ".settings.private.json")


def fingerprint(core, path):
    check(path in {ROOT / "runtime.env", ROOT / "browser.env", EVIDENCE} and path.resolve(strict=True) == path,
          "A private input escaped its fixed canonical pathname")
    descriptor = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        for component in path.parts[1:-1]:
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
            info = os.fstat(descriptor)
            check(info.st_uid == 0 and info.st_mode & 0o022 == 0, "A private input ancestor has unsafe ownership")
        source = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME, dir_fd=descriptor)
    finally:
        os.close(descriptor)
    with os.fdopen(source, "rb") as stream:
        info = os.fstat(stream.fileno())
        bound = MAX_BODY if path == EVIDENCE else 65536
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
              info.st_nlink == 1 and 0 < info.st_size <= bound, "A private input is not root-private and bounded")
        raw = stream.read(bound + 1)
        check(len(raw) == info.st_size and core.file_identity(info) == core.file_identity(os.fstat(stream.fileno())) == core.file_identity(path.lstat()),
              "A private input changed during observation")
        if path == ROOT / "runtime.env":
            names = [line.strip().removeprefix("export ").partition("=")[0].strip() for line in raw.decode("utf-8").splitlines()]
            check(not any(name.startswith("GOBY_LOG_") or name == "GOBY_ACTIVITY_RETENTION_DAYS" for name in names),
                  "A diagnostic override was inserted into the protected original runtime file")
    return core.file_identity(info), hashlib.sha256(raw).hexdigest()


def log_directory_identity():
    check(LOG_DIRECTORY.resolve(strict=True) == LOG_DIRECTORY, "The diagnostic directory is not canonical")
    info = LOG_DIRECTORY.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == 995 and stat.S_IMODE(info.st_mode) == 0o700,
          "The diagnostic directory is not owned privately by the accepted service")
    return info.st_dev, info.st_ino, info.st_uid, stat.S_IMODE(info.st_mode)


def diagnostic_deployment(core, deployment):
    check(OBSERVABILITY_DROPIN.resolve(strict=True) == OBSERVABILITY_DROPIN, "The diagnostic drop-in path is not canonical")
    parent = OBSERVABILITY_DROPIN.parent.lstat()
    check(stat.S_ISDIR(parent.st_mode) and parent.st_uid == 0 and parent.st_mode & 0o022 == 0,
          "The diagnostic drop-in directory has unsafe ownership")
    descriptor = os.open(OBSERVABILITY_DROPIN, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME)
    with os.fdopen(descriptor, "rb") as source:
        info = os.fstat(source.fileno())
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o644 and
              info.st_nlink in {1, 2} and info.st_size == len(OBSERVABILITY_BYTES) and source.read(len(OBSERVABILITY_BYTES) + 1) == OBSERVABILITY_BYTES and
              core.file_identity(info) == core.file_identity(os.fstat(source.fileno())) == core.file_identity(OBSERVABILITY_DROPIN.lstat()),
              "The diagnostic drop-in differs from its exact owned bytes, identity or mode")
    result = subprocess.run(["/usr/bin/systemctl", "show", "goby-foundation-test.service", "-p", "User", "-p", "Group", "-p", "LogsDirectory",
                             "-p", "LogsDirectoryMode", "-p", "DropInPaths"], capture_output=True, timeout=remaining())
    check(result.returncode == 0 and len(result.stdout) <= 4096 and len(result.stderr) <= 4096, "The effective diagnostic unit observation failed")
    unit = dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)
    check(set(unit) == {"User", "Group", "LogsDirectory", "LogsDirectoryMode", "DropInPaths"} and unit["User"] == unit["Group"] == "goby" and
          unit["LogsDirectory"] == "goby-test" and unit["LogsDirectoryMode"] == "0700" and
          set(unit["DropInPaths"].split()) == {str(OBSERVABILITY_DROPIN), str(OBSERVABILITY_DROPIN.with_name("20-application-keys.conf"))},
          "The running unit differs from its fixed diagnostic directory or service identity")
    process = Path("/proc") / str(deployment["main_pid"])
    before = (process / "stat").read_text().rsplit(") ", 1)[1].split()[19]
    descriptor = os.open(process / "environ", os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as source:
        check(os.fstat(source.fileno()).st_uid == 995, "The accepted process environment has another owner")
        environment = source.read(MAX_BODY + 1)
    check(len(environment) <= MAX_BODY, "The accepted process environment exceeds its bounded observation scope")
    selected = [value for value in environment.split(b"\x00") if value.partition(b"=")[0].startswith(b"GOBY_LOG_") or
                value.partition(b"=")[0] == b"GOBY_ACTIVITY_RETENTION_DAYS"]
    del environment
    after = (process / "stat").read_text().rsplit(") ", 1)[1].split()[19]
    check(before == after == deployment["start_ticks"] and selected == [b"GOBY_LOG_DIR=/var/log/goby-test"],
          "The accepted process has another lifetime or diagnostic environment policy")
    return core.file_identity(info), hashlib.sha256(OBSERVABILITY_BYTES).hexdigest(), unit


def snapshot(tasks, core, utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == core.TABLES and len(value) == 29 and sum(map(len, value.values())) <= 5000,
          "The complete main snapshot escaped the twenty-nine-table bound")
    migrations = tasks.records(value, "schema_migrations")
    check(len(migrations) == 22 and {row["version"] for row in migrations} == set(range(1, 23)), "The main schema is not exactly version twenty-two")
    media = [row for row in tasks.records(value, "items") if row["media"] is not None]
    check(len(value["libraries"]) == 5 and len(media) == 11 and all(row["media"].get("ProbeVersion") == 6 for row in media),
          "The accepted five-library and eleven-media catalog changed")
    check(len(value["managed_settings"]) == 1 and set(tasks.indexed(value, "managed_settings")) == {"1"}, "The managed singleton inventory changed")
    return value


def secrets_absent(raw, private_values):
    for value in private_values:
        if not value:
            continue
        encoded = value.encode("utf-8")
        variants = {encoded, json.dumps(value, ensure_ascii=True)[1:-1].encode(), quote(value, safe="").encode(),
                    base64.b64encode(encoded), base64.urlsafe_b64encode(encoded).rstrip(b"=")}
        check(all(variant not in raw for variant in variants), "An observation disclosed a workflow credential or private setting value")


class API:
    def __init__(self, core, logins, password, old_sessions):
        self.core, self.logins = core, logins
        self.native, self.emby = logins
        self.private_values = [password]
        self.old_token_hashes = {row["token_hash"] for row in old_sessions}
        self.old_session_ids = {row["id"] for row in old_sessions}
        self.scope = None
        self.names = set()
        self.counts = {"GET": 0, "HEAD": 0, "POST": 0, "PUT": 0, "DELETE": 0}
        self.evidence = []
        self.last_status, self.last_headers = None, {}
        self.write_response_failures = self.logout_response_failures = 0
        for login in logins:
            login.denied = login.barrier = False
            login.initial_device = None
            login.cleanup_csrf = ""

    def credential_values(self):
        return self.private_values[:1] + [value for login in self.logins for value in (login.token, login.csrf, login.cleanup_csrf)]

    def observation_values(self):
        return self.private_values[1:] + self.credential_values()

    def acknowledge(self, login, token):
        check(login.attempted and not login.token and TOKEN.fullmatch(token) and
              "\\x" + hashlib.sha256(token.encode()).hexdigest() not in self.old_token_hashes and
              not any(other.token == token for other in self.logins if other is not login),
              "A login response attempted to reuse an old or previously acknowledged credential")
        # A response-confirmed token whose hash was absent from every old row
        # can be retired independently of later database availability. SQL
        # binding remains mandatory for success and exact history assertions.
        login.token = token
        if login is self.native:
            # Keep this separate from the server-returned CSRF receipt. It can
            # only retire an ACKed cookie after a truncated login/session body;
            # it never authorizes a settings write or satisfies login checks.
            login.cleanup_csrf = hashlib.sha256(("goby:admin:csrf:" + token).encode()).hexdigest()

    def request(self, method, path, login, *, body=None, expected=200, raw=False, action=None, byte_range=None):
        parsed = urlsplit(path)
        check(method in self.counts and login in self.logins and path.startswith("/") and not path.startswith("//") and
              not parsed.scheme and not parsed.netloc and not parsed.fragment and len(path) <= 4096 and "\r" not in path and "\n" not in path,
              "An HTTP request escaped its fixed authentication scope")
        route, native = parsed.path, login is self.native
        signing_in = (method, route) in {("POST", SESSION), ("POST", LOGIN)}
        logging_out = (method, route) in {("DELETE", SESSION), ("POST", LOGOUT)}
        diagnostic = False
        if signing_in:
            check(not parsed.query and native == (route == SESSION) and not login.attempted and not login.token,
                  "An additional or mismatched login was prevented")
        elif logging_out:
            check(not parsed.query and native == (route == SESSION) and login.attempted and login.token and
                  not login.logout_attempted and (not native or login.csrf or login.cleanup_csrf), "An unowned or repeated logout was prevented")
        elif method == "PUT":
            check(native and route == SETTINGS and not parsed.query and login.csrf and self.scope is not None,
                  "A mutation escaped the sole native settings scope")
            self.scope.authorize(action, body)
        else:
            check(method in {"GET", "HEAD"} and body is None, "A read used an unapproved method or body")
            permitted = {SESSION, SETTINGS, ACTIVITY, LOGS} if native else {EMBY_ACTIVITY, EMBY_LOGS}
            for name in self.names:
                permitted |= ({LOGS + "/" + name + "/lines", LOGS + "/" + name + "/download"} if native else
                              {"/emby/System/Logs/" + name, "/emby/System/Logs/" + name + "/Lines"})
            check(route in permitted, "A read escaped the closed observation route list")
            diagnostic = route not in {SESSION, SETTINGS, ACTIVITY, EMBY_ACTIVITY}
            download = route in {LOGS + "/" + name + "/download" for name in self.names} if native else route in {"/emby/System/Logs/" + name for name in self.names}
            check(method != "HEAD" or download, "HEAD was attempted outside the owned download routes")
            query = parse_qsl(parsed.query, strict_parsing=True, keep_blank_values=True)
            fields = {"StartIndex", "Limit", "Action", "ActorId"} if route == ACTIVITY else {"StartIndex", "Limit"}
            check(not parsed.query or not download and route != SESSION and route != SETTINGS and
                  len(query) == len({name for name, _value in query}) and all(name in fields and value for name, value in query),
                  "An observation query escaped its closed non-secret field list")
            if byte_range is not None:
                check(native and method == "GET" and download and byte_range == "bytes=0-63", "An unbounded or non-native range was prevented")
        check(body is None or signing_in or method == "PUT", "A request body escaped the login or sole CAS write")
        check(sum(self.counts.values()) < MAX_HTTP, "The complete HTTP request budget was exhausted")
        self.counts[method] += 1
        headers = {"Accept": "application/json", "Connection": "close"}
        if native:
            headers["Origin"] = ORIGIN
            if login.token:
                headers["Cookie"] = "goby_session=" + login.token
            if login.csrf:
                headers["X-CSRF-Token"] = login.csrf
            elif logging_out and login.cleanup_csrf:
                headers["X-CSRF-Token"] = login.cleanup_csrf
        else:
            headers.update({"X-Emby-Client": login.app, "X-Emby-Device-Id": login.reported,
                            "X-Emby-Device-Name": login.name, "X-Emby-Client-Version": login.version})
            if login.token:
                headers["X-Emby-Token"] = login.token
        if byte_range:
            headers["Range"] = byte_range
        payload = None if body is None else json.dumps(body, ensure_ascii=True, separators=(",", ":")).encode()
        check(payload is None or len(payload) <= 16384, "A request body exceeded its exact scope bound")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if signing_in:
            login.attempted = True
        if logging_out:
            login.logout_attempted = True
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=remaining())
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            self.last_status = response.status
            self.last_headers = {name.lower(): value for name, value in response.getheaders()
                                 if name.lower() in {"content-type", "content-length", "content-disposition", "content-range", "cache-control", "pragma"}}
            cookies = SimpleCookie()
            cookies.load(response.getheader("Set-Cookie", ""))
            if signing_in and native and "goby_session" in cookies and TOKEN.fullmatch(cookies["goby_session"].value):
                self.acknowledge(login, cookies["goby_session"].value)
            content = response.read(MAX_BODY + 1)
            check(len(content) <= MAX_BODY, "An observation response exceeded the one-MiB byte bound")
            value = content if raw else json.loads(content) if content else None
            if signing_in and not native and isinstance(value, dict) and isinstance(value.get("AccessToken"), str) and TOKEN.fullmatch(value["AccessToken"]):
                self.acknowledge(login, value["AccessToken"])
            if route == SESSION and isinstance(value, dict) and isinstance(value.get("CSRFToken"), str) and HASH.fullmatch(value["CSRFToken"]) and login.token:
                check(value["CSRFToken"] == hashlib.sha256(("goby:admin:csrf:" + login.token).encode()).hexdigest(), "The CSRF token does not belong to the owned cookie")
                login.csrf = value["CSRFToken"]
            check(response.status in ((expected,) if isinstance(expected, int) else expected), "An observation request returned an unexpected status")
            check(not (method == "HEAD" or response.status == 204 or logging_out) or not content, "A bodyless response unexpectedly contained bytes")
            if route not in {SESSION, LOGIN, LOGOUT, SETTINGS}:
                secrets_absent(content, self.observation_values())
                check("no-store" in response.getheader("Cache-Control", "") and response.getheader("Pragma") == "no-cache",
                      "An observation response omitted its no-cache contract")
                self.evidence.append({"method": method, "route_kind": "diagnostics" if diagnostic else "activity",
                                      "audience": "native" if native else "emby", "status": response.status,
                                      "bytes": len(content), "sha256": hashlib.sha256(content).hexdigest(), "range": bool(byte_range)})
            return value
        except BaseException:
            if action is not None:
                self.write_response_failures += 1
            raise
        finally:
            connection.close()


class SettingsScope:
    def __init__(self, configuration, tasks, core, database, api, original, initial):
        self.configuration, self.tasks, self.core, self.database, self.api = configuration, tasks, core, database, api
        self.original, self.initial = original, initial
        self.revision = original["revision"]
        check(type(self.revision) is int and 1 <= self.revision <= 9223372036854775805, "The original settings revision lacks safe headroom")
        self.name = "M5i activity proof " + secrets.token_hex(16)
        check(self.name not in {initial["Overrides"]["ServerName"], initial["Effective"]["ServerName"]}, "The temporary name unexpectedly collided")
        api.private_values.append(self.name)
        self.name_attempts = self.restore_attempts = 0
        self.grant = None
        self.temporary_row = self.restored_row = self.final_row = None
        self.restored = self.final_proven = self.no_commit = False

    def observe(self):
        for _attempt in range(3):
            shown = self.api.request("GET", SETTINGS, self.api.native)
            self.configuration.validate_dto(self.core, shown)
            check(all(shown[field] == self.initial[field] for field in ("Defaults", "Deployment")), "Frozen settings defaults or deployment values changed")
            row = self.database.read("SELECT to_jsonb(s) FROM managed_settings s WHERE id=1;", "Observe the exact settings publication and stored singleton")
            check(set(row) == self.configuration.ROW_FIELDS and row["id"] == 1 and row["created_at"] == self.original["created_at"],
                  "The settings singleton escaped its original identity")
            if (shown["Revision"] == str(row["revision"]) and shown["ServerNameMode"] == row["server_name_mode"] and
                    shown["Encoding"]["TranscodingMaxWidth"] == row["compatibility_max_width"] and
                    self.core.timestamp(shown["UpdatedAt"]) == self.core.timestamp(row["updated_at"]) and
                    all(self.tasks.identical(shown["Overrides"][field], row[column]) for field, column in self.configuration.FIELDS.items())):
                return shown, row
        raise Failure("Native publication and the stored settings row did not match within the bounded reads")

    def expected(self, restored, updated_at):
        wanted = dict(self.original)
        wanted.update({"revision": self.revision + (2 if restored else 1), "updated_at": updated_at})
        if not restored:
            wanted.update({"server_name": self.name, "server_name_mode": "custom"})
        return wanted

    def classify(self, row):
        self.core.timestamp(row["updated_at"])
        if self.tasks.identical(row, self.original) and self.temporary_row is None and not self.restore_attempts:
            return "original"
        if self.name_attempts and self.tasks.identical(row, self.expected(False, row["updated_at"])):
            check(self.restored_row is None and (self.temporary_row is None or self.tasks.identical(row, self.temporary_row)),
                  "The previously confirmed owned temporary settings changed")
            self.temporary_row = row
            return "temporary"
        if self.restore_attempts and self.tasks.identical(row, self.expected(True, row["updated_at"])):
            check(self.restored_row is None or self.tasks.identical(row, self.restored_row), "The acknowledged restored settings changed")
            self.restored, self.restored_row = True, row
            return "restored"
        raise Failure("An unowned settings revision or complete value set prevents restoration")

    def body(self, restore):
        overrides = dict(self.initial["Overrides"])
        if not restore:
            overrides["ServerName"] = self.name
        return {"Revision": str(self.revision + int(restore)), "Overrides": overrides,
                "ServerNameMode": self.initial["ServerNameMode"] if restore else "custom", "Encoding": self.initial["Encoding"]}

    def authorize(self, action, body):
        check(action in {"name", "restore"} and self.grant == action and not self.api.native.logout_attempted,
              "A CAS mutation lacks a fresh single-use ownership grant")
        self.grant = None
        if action == "name":
            check(self.name_attempts == 0 and body == self.body(False), "An additional or altered initial CAS write was prevented")
            self.name_attempts += 1
        else:
            check(self.name_attempts == 1 and self.temporary_row is not None and not self.restored and self.restore_attempts < 2 and body == self.body(True),
                  "A restore escaped its original complete state or fixed original-plus-one revision")
            self.restore_attempts += 1

    def install(self):
        shown, row = self.observe()
        check(self.tasks.identical(row, self.original) and self.tasks.identical(shown, self.initial),
              "The initial complete settings DTO or baseline row changed before the sole CAS replacement")
        self.grant = "name"
        receipt = self.api.request("PUT", SETTINGS, self.api.native, body=self.body(False), action="name")
        self.configuration.validate_dto(self.core, receipt)
        shown, row = self.observe()
        check(self.classify(row) == "temporary" and shown["Revision"] == receipt["Revision"] == str(self.revision + 1) and
              shown["Effective"]["ServerName"] == self.name, "The actual name replacement did not produce the exact owned next revision")
        check(all(shown["Overrides"][field] == self.initial["Overrides"][field] for field in self.configuration.BOUNDS) and
              shown["Encoding"] == self.initial["Encoding"], "The name replacement changed a numeric override or independent encoding value")

    def restore(self):
        if not self.name_attempts or self.api.native.logout_attempted:
            return
        for _attempt in range(4):
            shown, row = self.observe()
            state = self.classify(row)
            if state == "restored":
                check(all(shown[field] == self.initial[field] for field in SETTINGS_SECTIONS), "The restored DTO lost an original value, omission, override or name mode")
                return
            if state == "original":
                # A late original CAS may still commit. Grant no write and do
                # not call this restored until the native revocation barrier.
                continue
            check(self.tasks.identical(row, self.temporary_row), "A fresh ownership check did not match the exact temporary row")
            if self.restore_attempts >= 2:
                continue
            self.grant = "restore"
            try:
                receipt = self.api.request("PUT", SETTINGS, self.api.native, body=self.body(True), expected=(200, 409), action="restore")
                if self.api.last_status == 200:
                    self.configuration.validate_dto(self.core, receipt)
                    check(receipt["Revision"] == str(self.revision + 2) and all(receipt[field] == self.initial[field] for field in SETTINGS_SECTIONS),
                          "The restoration receipt did not preserve the complete original configuration")
                else:
                    self.api.write_response_failures += 1
            except Exception:
                # Any additional attempt still requires a new full ownership
                # observation and the same fixed CAS revision; never rebase.
                continue
        raise Failure("The bounded original-revision CAS restoration remains unproven")

    def after_revocation(self):
        check(self.api.native.barrier, "The writing credential has no committed revocation barrier")
        row = self.database.read("SELECT to_jsonb(s) FROM managed_settings s WHERE id=1;", "Observe original configuration after the owned writer revocation barrier")
        state = self.classify(row)
        check(state == "restored" or state == "original" and self.restore_attempts == 0, "The original settings remain dirty or unproven after writer revocation")
        self.no_commit, self.final_proven, self.final_row = state == "original", True, row


def logout(configuration, tasks, core, database, api, login):
    check(login.attempted and login.token, "The owned logout has no response-confirmed fresh credential")
    if not login.logout_attempted:
        try:
            api.request("DELETE" if login.kind == "admin" else "POST", SESSION if login.kind == "admin" else LOGOUT,
                        login, expected=204 if login.kind == "admin" else 200, raw=True)
        except Exception:
            api.logout_response_failures += 1
    api.request("GET", ACTIVITY if login.kind == "admin" else EMBY_ACTIVITY, login, expected=401, raw=True)
    login.denied = True
    if login.initial is not None:
        check(ID.fullmatch(login.id), "The owned revocation row lacks a canonical identity")
        row = database.read("SELECT to_jsonb(s) FROM sessions s WHERE id='" + login.id + "';", "Confirm exact committed credential revocation")
        configuration.session_row(tasks, core, login, row, True)
    else:
        digest = hashlib.sha256(login.token.encode()).hexdigest()
        row = database.read("SELECT to_jsonb(s) FROM sessions s WHERE token_hash=decode('" + digest + "','hex');",
                            "Confirm revocation of the response-confirmed new credential after unavailable binding")
        check(isinstance(row, dict) and ID.fullmatch(row.get("id", "")) and row["id"] not in api.old_session_ids and
              row["kind"] == login.kind and row["user_id"] == login.user_id and row["token_hash"] == "\\x" + digest and row["revoked_at"] is not None,
              "The fresh unbound credential has no exact committed revocation row")
        core.timestamp(row["revoked_at"])
        login.id = row["id"]
    login.barrier = True


def expected_activity(api, scope, revoked):
    result = []
    for login in api.logins:
        if login.initial is None:
            continue
        for action in (["session.login", "session.revoked"] if revoked else ["session.login"]):
            result.append({"action": action, "source": "native" if login.kind == "admin" else "emby", "actor_kind": "user",
                           "actor_id": login.user_id, "actor_credential_id": login.id, "resource_kind": "session", "resource_id": login.id,
                           "revision": 0, "affected_count": 1, "changed_fields": []})
    if scope is not None and scope.temporary_row is not None:
        fields = ["ServerName"] + (["ServerNameMode"] if scope.initial["ServerNameMode"] != "custom" else [])
        for revision in ([scope.revision + 1, scope.revision + 2] if scope.restored else [scope.revision + 1]):
            result.append({"action": "settings.updated", "source": "native", "actor_kind": "user", "actor_id": api.native.user_id,
                           "actor_credential_id": api.native.id, "resource_kind": "settings", "resource_id": "1", "revision": revision,
                           "affected_count": 1, "changed_fields": fields})
    return [dict(item, severity="Info", state="", request_id="") for item in result]


def audit_activity(tasks, core, baseline, current, api, scope, revoked):
    added = tasks.delta(baseline, current, "activity_entries")
    wanted = expected_activity(api, scope, revoked)
    check(len(added) == len(wanted), "Activity history escaped the exact committed session and settings facts")
    unmatched = list(wanted)
    for row in added:
        check(set(row) == ACTIVITY_COLUMNS and type(row["id"]) is int and row["id"] > 0, "An activity row lost its closed schema or numeric identity")
        core.timestamp(row["created_at"])
        fields = {name: value for name, value in row.items() if name not in {"id", "created_at"}}
        matches = [entry for entry in unmatched if tasks.identical(fields, entry)]
        check(len(matches) == 1, "An activity fact has an unexpected actor, source, resource, revision or changed field")
        unmatched.remove(matches[0])
    secrets_absent("\n".join(current["activity_entries"]).encode(), api.observation_values())
    return added


def native_activity(configuration, tasks, core, api, rows):
    settings_rows = sorted([row for row in rows if row["action"] == "settings.updated"], key=lambda row: (row["created_at"], row["id"]), reverse=True)
    path = ACTIVITY + "?" + urlencode({"Action": "settings.updated", "ActorId": api.native.user_id, "StartIndex": 0, "Limit": 50})
    page = api.request("GET", path, api.native)
    check(isinstance(page, dict) and set(page) == {"Items", "TotalRecordCount", "StartIndex", "Limit", "RetentionDays"} and
          isinstance(page["Items"], list) and type(page["TotalRecordCount"]) is int and page["TotalRecordCount"] >= len(settings_rows) and
          page["StartIndex"] == 0 and page["Limit"] == 50 and type(page["RetentionDays"]) is int and 1 <= page["RetentionDays"] <= 365,
          "The native activity page lost its exact bounded contract")
    indexed = {entry["Id"]: entry for entry in page["Items"] if isinstance(entry, dict) and isinstance(entry.get("Id"), str)}
    check(len(indexed) == len(page["Items"]), "The native activity page has duplicate or non-string identifiers")
    for row in settings_rows:
        item = indexed.get(str(row["id"]))
        check(isinstance(item, dict) and set(item) == ACTIVITY_FIELDS and item["Action"] == row["action"] and item["Severity"] == "Info" and
              item["Source"] == "native" and item["Actor"] == {"Kind": "user", "Id": api.native.user_id, "Name": api.admin_name} and
              item["Resource"] == {"Kind": "settings", "Id": "1"} and item["Revision"] == str(row["revision"]) and item["Count"] == "1" and
              item["State"] is None and item["ChangedFields"] == row["changed_fields"] and
              item["Name"] == "Server settings updated" and item["Overview"] == "Supported server settings were updated." and
              core.timestamp(item["Date"]) == core.timestamp(row["created_at"]), "Native activity does not describe the exact committed settings fact")
    emby = api.request("GET", EMBY_ACTIVITY + "?StartIndex=0&Limit=50", api.emby)
    check(isinstance(emby, dict) and set(emby) == {"Items", "TotalRecordCount"} and isinstance(emby.get("Items"), list) and
          type(emby.get("TotalRecordCount")) is int and emby["TotalRecordCount"] >= len(rows),
          "The Emby activity response omitted its query-result collection")
    for row in settings_rows:
        matches = [item for item in emby["Items"] if isinstance(item, dict) and type(item.get("Id")) is int and item["Id"] == row["id"]]
        check(len(matches) == 1 and set(matches[0]) == {"Id", "Name", "Overview", "Type", "Date", "UserId", "Severity"} and
              matches[0].get("Name") == "Server settings updated" and matches[0].get("Overview") == "Supported server settings were updated." and
              matches[0].get("Severity") == "Info" and matches[0].get("Type") == "settings.updated" and
              matches[0].get("UserId") == api.native.user_id and
              re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{7}Z", matches[0].get("Date", "")) and
              core.timestamp(matches[0].get("Date")) == core.timestamp(row["created_at"]),
              "The Emby activity projection lost the actual committed fact or numeric identifier")
    return {"native_settings_facts": len(settings_rows), "emby_settings_facts": len(settings_rows), "retention_days": page["RetentionDays"]}


def jsonl(core, raw, private_values):
    check(raw and raw.endswith(b"\n") and len(raw) <= MAX_BODY, "The diagnostic snapshot is not bounded complete JSONL")
    secrets_absent(raw, private_values)
    lines = raw.decode("utf-8").splitlines()
    for line in lines:
        check(0 < len(line.encode()) <= 8192, "A diagnostic line escaped its fixed record bound")
        record = json.loads(line)
        check(isinstance(record, dict) and BASE_LOG_FIELDS <= set(record) and record["event"] in LOG_EVENTS,
              "A diagnostic line omitted its fixed envelope or known event")
        message, fields = LOG_EVENTS[record["event"]]
        check(record["msg"] == message and record["level"] in {"INFO", "WARN", "ERROR", "DEBUG"}, "A diagnostic line contains an unclassified message or severity")
        core.timestamp(record["time"])
        allowed = set(fields.split())

        def attributes(values, depth=0):
            check(depth <= 6 and isinstance(values, dict), "A diagnostic attribute group escaped its bound")
            for key, value in values.items():
                if key in LOG_GROUPS and isinstance(value, dict):
                    attributes(value, depth + 1)
                else:
                    check(key in allowed and (type(value) in {int, bool} or isinstance(value, str) and len(value.encode()) <= 256),
                          "A diagnostic attribute escaped its fixed event allowlist")
        attributes({name: value for name, value in record.items() if name not in BASE_LOG_FIELDS})
    return lines


def diagnostics(core, api):
    native = api.request("GET", LOGS + "?StartIndex=0&Limit=200", api.native)
    check(isinstance(native, dict) and set(native) == {"Items", "TotalRecordCount", "StartIndex", "Limit", "Status"} and
          isinstance(native["Items"], list) and type(native["TotalRecordCount"]) is int and native["TotalRecordCount"] == len(native["Items"]) and
          native["StartIndex"] == 0 and native["Limit"] == 200, "The native diagnostic index lost its complete bounded page")
    status = native["Status"]
    check(isinstance(status, dict) and set(status) == LOG_STATUS_FIELDS and status["Healthy"] is True and status["Degraded"] is False and
          status["Closed"] is False and status["Format"] == "jsonl" and isinstance(status["MaxFileBytes"], str) and NUMBER.fullmatch(status["MaxFileBytes"]) and
          isinstance(status["MinFreeBytes"], str) and NUMBER.fullmatch(status["MinFreeBytes"]) and type(status["MaxFiles"]) is int and
          1 <= status["MaxFiles"] <= 200 and type(status["RetentionDays"]) is int and 1 <= status["RetentionDays"] <= 365,
          "The diagnostic status is not healthy or its policy lost safe integer types")
    check(status == {"Healthy": True, "Degraded": False, "Closed": False, "MaxFileBytes": "4194304", "MaxFiles": 16,
                     "RetentionDays": 7, "MinFreeBytes": "33554432", "Format": "jsonl"},
          "The running diagnostic defaults differ from the accepted directory-only configuration")
    for item in native["Items"]:
        check(isinstance(item, dict) and set(item) == NATIVE_LOG_FIELDS and isinstance(item["Name"], str) and LOG_NAME.fullmatch(item["Name"]) and
              isinstance(item["Size"], str) and re.fullmatch(r"0|[1-9][0-9]*", item["Size"]), "The diagnostic index contains an unowned name or non-string size")
        core.timestamp(item["DateCreated"])
        core.timestamp(item["DateModified"])
    emby = api.request("GET", EMBY_LOGS + "?StartIndex=0&Limit=200", api.emby)
    check(isinstance(emby, dict) and set(emby) == {"Items", "TotalRecordCount"} and isinstance(emby.get("Items"), list) and type(emby.get("TotalRecordCount")) is int,
          "The Emby diagnostic index omitted its query-result collection")
    emby_files = {item["Name"]: item for item in emby["Items"] if isinstance(item, dict) and isinstance(item.get("Name"), str)}
    candidates = [item for item in native["Items"] if 64 <= int(item["Size"]) <= MAX_BODY // 2 and item["Name"] in emby_files]
    check(candidates, "No shared listed diagnostic snapshot fits the bounded download scope")
    selected = sorted(candidates, key=lambda item: (item["DateCreated"], item["Name"]), reverse=True)[0]
    name, equivalent = selected["Name"], emby_files[selected["Name"]]
    check(set(equivalent) == NATIVE_LOG_FIELDS and type(equivalent["Size"]) is int and equivalent["Size"] >= int(selected["Size"]),
          "The Emby diagnostic file lost its numeric size or fixed file fields")
    api.names.add(name)
    native_download, emby_download = LOGS + "/" + name + "/download", "/emby/System/Logs/" + name
    full = api.request("GET", native_download, api.native, raw=True)
    lines = jsonl(core, full, api.observation_values())
    check(api.last_headers.get("content-type", "").split(";", 1)[0] == "application/x-ndjson" and
          api.last_headers.get("content-disposition", "").startswith("attachment;") and
          int(api.last_headers.get("content-length", "-1")) == len(full), "Native download omitted its attachment, JSONL type or fixed length")
    api.request("HEAD", native_download, api.native, raw=True)
    check(len(full) <= int(api.last_headers.get("content-length", "-1")) <= MAX_BODY and
          api.last_headers.get("content-type", "").split(";", 1)[0] == "application/x-ndjson", "Native HEAD lost its current fixed snapshot length or JSONL type")
    prefix = api.request("GET", native_download, api.native, expected=206, raw=True, byte_range="bytes=0-63")
    check(prefix == full[:64] and re.fullmatch(r"bytes 0-63/[1-9][0-9]*", api.last_headers.get("content-range", "")),
          "Native Range did not return the exact pinned diagnostic prefix")
    preview_count = min(2, len(lines))
    preview = api.request("GET", LOGS + "/" + name + "/lines?StartIndex=0&Limit=" + str(preview_count), api.native)
    check(isinstance(preview, dict) and set(preview) == {"Items", "StartIndex", "NextIndex", "TotalRecordCount", "SnapshotSize"} and
          preview["Items"] == lines[:preview_count] and preview["StartIndex"] == 0 and preview["NextIndex"] == preview_count and
          type(preview["TotalRecordCount"]) is int and preview["TotalRecordCount"] >= len(lines) and
          isinstance(preview["SnapshotSize"], str) and int(preview["SnapshotSize"]) >= len(full),
          "Native line pagination did not preserve the same listed file prefix and decimal snapshot size")
    compatible = api.request("GET", emby_download, api.emby, raw=True)
    compatible_lines = jsonl(core, compatible, api.observation_values())
    check(compatible.startswith(full) and api.last_headers.get("content-type", "").lower() == "text/plain; charset=utf-8" and
          api.last_headers.get("content-disposition", "").startswith("attachment;") and
          int(api.last_headers.get("content-length", "-1")) == len(compatible),
          "The Emby download lost its safe prefix, attachment, fixed byte count or observed plain-text contract")
    check(any(json.loads(line).get("event") == "request.completed" for line in compatible_lines),
          "The accepted diagnostic snapshot contains no actual HTTP completion record")
    wire_lines = api.request("GET", emby_download + "/Lines?StartIndex=0&Limit=500", api.emby)
    check(isinstance(wire_lines, dict) and set(wire_lines) == {"Items", "TotalRecordCount"} and isinstance(wire_lines.get("Items"), list) and
          type(wire_lines.get("TotalRecordCount")) is int and len(wire_lines["Items"]) == min(500, wire_lines["TotalRecordCount"]) and
          wire_lines["TotalRecordCount"] >= len(compatible_lines) and
          wire_lines["Items"][:min(500, len(compatible_lines))] == compatible_lines[:500],
          "The Emby line projection did not preserve the listed JSONL file prefix")
    api.request("HEAD", emby_download, api.emby, expected=404, raw=True)
    return {"shared_file_name_sha256": hashlib.sha256(name.encode()).hexdigest(), "native_download_bytes": len(full),
            "native_download_sha256": hashlib.sha256(full).hexdigest(), "native_complete_lines": len(lines),
            "emby_download_bytes": len(compatible), "emby_download_sha256": hashlib.sha256(compatible).hexdigest(),
            "native_head_and_single_range": True, "emby_head_status": 404, "fixed_jsonl_keys_and_secret_absence": True}


def audit(configuration, tasks, core, baseline, final, api, scope):
    for table in core.TABLES - {"sessions", "devices", "managed_settings", "activity_entries"}:
        check(baseline[table] == final[table], "A protected old business table changed")
    added_sessions, added_devices = tasks.delta(baseline, final, "sessions"), tasks.delta(baseline, final, "devices")
    logins = api.logins if api else []
    check({row["id"] for row in added_sessions} == {login.id for login in logins if login.id} and
          len(added_sessions) == sum(login.attempted for login in logins), "New session history escaped the two acknowledged logins")
    current, touches = {row["id"]: row for row in added_sessions}, 0
    for login in logins:
        if login.attempted:
            check(login.denied and login.barrier, "An issued credential lacks independent denial and committed revocation proof")
            touches += configuration.session_row(tasks, core, login, current[login.id], True)
    ordinary = api.emby if api and api.emby.initial_device else None
    check({str(row["id"]) for row in added_devices} == ({ordinary.device} if ordinary else set()), "New device history escaped the sole ordinary login")
    device_touches = 0
    if ordinary:
        wanted = dict(ordinary.initial_device)
        device_touches = configuration.activity(core, wanted["last_seen_at"], added_devices[0]["last_seen_at"])
        wanted["last_seen_at"] = added_devices[0]["last_seen_at"]
        check(tasks.identical(wanted, added_devices[0]), "The owned ordinary device changed beyond its real Touch timestamp")
    if scope is not None:
        check(scope.final_proven and tasks.identical(tasks.indexed(final, "managed_settings")["1"], scope.final_row), "Complete original settings restoration is not proven")
    else:
        check(baseline["managed_settings"] == final["managed_settings"], "Settings changed without a mutation scope")
    activities = audit_activity(tasks, core, baseline, final, api, scope, True) if api else tasks.delta(baseline, final, "activity_entries")
    check(api is not None or not activities, "Activity appeared without any acknowledged workflow")
    return {"new_sessions": len(added_sessions), "new_devices": len(added_devices), "new_activity_entries": len(activities),
            "ordinary_session_rows_touched": touches, "ordinary_device_rows_touched": device_touches}


def main():
    global FINAL_END
    report = {"owner": OWNER, "status": "failed", "cleanup": {"proven": False, "errors": []}, "snapshots": {},
              "limits": {"main_seconds": 60, "independent_cleanup_seconds": 45, "response_bytes": MAX_BODY, "http_requests": MAX_HTTP,
                         "database_queries": 80, "initial_native_cas_puts": 1, "fixed_revision_restore_puts": 2},
              "limitations": ["No concurrent external administrator writes may run during this preservation acceptance",
                              "No rotation, retention deletion, revocation during an in-flight download or client compatibility matrix is claimed here"],
              "http_transport_retries": 0, "raw_response_or_credential_exports": 0}
    configuration = settings = tasks = devices = core = utilities = sessions = database = baseline = api = scope = None
    deployment = vault = media = nfos = configured = parent_identity = log_identity = log_configuration = None
    credentials, inputs = {}, {}
    stage = "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without arguments")
        signal.signal(signal.SIGALRM, deadline)
        signal.setitimer(signal.ITIMER_REAL, max(0.001, PHASE_END - time.monotonic()))
        accepted = os.environ.get("GOBY_OBSERVABILITY_EXPECTED_BINARY_SHA256", "")
        expected_pid = os.environ.get("GOBY_OBSERVABILITY_EXPECTED_PID", "")
        evidence_hash = os.environ.get("GOBY_OBSERVABILITY_EXPECTED_SOURCE_SHA256", "")
        check(HASH.fullmatch(accepted) and NUMBER.fullmatch(expected_pid) and 1 < int(expected_pid) <= 2147483647 and HASH.fullmatch(evidence_hash) and
              os.environ.get("GOBY_OBSERVABILITY_SOURCE_EVIDENCE_FILE") == str(EVIDENCE), "Explicit accepted PID, binary and fixed M5i source evidence constraints are required")
        number, result_path, attempt_path, intent_path = output_files()
        report.update({"verification_run": number, "output_scope": {"directory": str(ROOT), "report": result_path.name,
                                                                    "attempt_marker": attempt_path.name, "settings_recovery_intent": intent_path.name}})
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed report directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (result_path, attempt_path, intent_path)), "The selected evidence attempt already exists")
        configuration, settings, tasks, devices, core, utilities, sessions = helpers()
        utilities.private_write(attempt_path, {"owner": OWNER, "verification_run": number, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                                               "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
        parent_identity = info.st_dev, info.st_ino
        for path in (EVIDENCE, ROOT / "runtime.env", ROOT / "browser.env"):
            inputs[path] = fingerprint(core, path)
        check(inputs[EVIDENCE][1] == evidence_hash, "The source evidence differs from the explicitly accepted digest")
        deployment = sessions.deployment(accepted)
        check(deployment["main_pid"] == int(expected_pid), "The service PID differs from the explicitly accepted deployment")
        runtime = sessions.private_values(ROOT / "runtime.env", {"GOBY_LISTEN", "GOBY_PUBLIC_URL", "GOBY_MEDIA_ROOTS"})
        check(runtime["GOBY_LISTEN"] == "127.0.0.1:18096" and runtime["GOBY_PUBLIC_URL"] == ORIGIN,
              "The runtime escaped the fixed loopback endpoint")
        log_configuration = diagnostic_deployment(core, deployment)
        configured, log_identity = runtime["GOBY_MEDIA_ROOTS"], log_directory_identity()
        database = settings.database_type(core, sessions)()
        baseline = snapshot(tasks, core, utilities, database)
        report["snapshots"]["before"] = utilities.snapshot_report(baseline)
        settings.idle(tasks, core, database, baseline)
        now = core.timestamp(database.read("SELECT to_json(clock_timestamp());", "Observe the diagnostic acceptance and retention clock"))
        check(all(core.timestamp(row["created_at"]) > now - timedelta(days=1) + timedelta(minutes=2) for row in tasks.records(baseline, "activity_entries")),
              "An old activity may reach the minimum retention boundary during exact-row preservation")
        namespace = [row["value"] for row in tasks.records(baseline, "server_settings") if row["key"] == "server_id"]
        check(len(namespace) == 1 and ID.fullmatch(namespace[0]), "The persistent server namespace is invalid")
        report["deployment"] = {**deployment, "schema_version": 22, "probe_version": 6, "database_role": "goby_test", "database_port": 5432,
                                "postgresql_forced_read_only": True, "source_evidence_sha256": evidence_hash}
        vault = core.vault_identity(sessions)
        media, nfos = devices.source_hashes(core, baseline, configured), tasks.nfo_hashes(core, baseline, configured)
        report["source_inventory"] = {"media_count": len(media), "media_sha256": utilities.aggregate(media), "nfo_count": len(nfos), "nfo_sha256": utilities.aggregate(nfos)}
        credentials = sessions.private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        users = [row for row in tasks.records(baseline, "users") if row["name"] == credentials["GOBY_SMOKE_NAME"] and row["is_administrator"] and not row["is_disabled"]]
        check(len(users) == 1, "Private login data does not identify exactly one enabled existing administrator")
        nonce = secrets.token_hex(16)
        reported = "m5i-observability-" + nonce
        check(not any(row["reported_device_id"] == reported for row in tasks.records(baseline, "devices")) and
              not any(row["device_id"] == reported for row in tasks.records(baseline, "sessions")), "The unique client identity already has history")
        native = devices.Login("admin", users[0]["id"])
        emby = devices.Login("emby", users[0]["id"], reported=reported, name="M5i observability " + nonce, app="Goby M5i observability", version="m5i-deployed")
        api = API(core, [native, emby], credentials["GOBY_SMOKE_PASSWORD"], tasks.records(baseline, "sessions"))
        api.admin_name = users[0]["name"]
        stage = "two fresh independent administrator credentials"
        for login in api.logins:
            is_native = login.kind == "admin"
            signed = api.request("POST", SESSION if is_native else LOGIN, login,
                                 body={"Name" if is_native else "Username": credentials["GOBY_SMOKE_NAME"], "Password" if is_native else "Pw": credentials["GOBY_SMOKE_PASSWORD"]})
            check(login.token and isinstance(signed, dict) and signed.get("User", {}).get("Id") == login.user_id and (not is_native or login.csrf),
                  "A fresh login omitted its credential or selected another account")
            current = snapshot(tasks, core, utilities, database)
            configuration.bind_login(tasks, core, api, baseline, current, login)
            core.preserve(baseline, current)
            audit_activity(tasks, core, baseline, current, api, None, False)
            if not is_native:
                check(signed.get("ServerId") == namespace[0] and signed.get("SessionInfo", {}).get("Id") == login.id,
                      "The ordinary login differs from its stored session or server namespace")
        credentials.clear()
        check(native.token != emby.token and native.id != emby.id, "The two credential kinds lost their independent identities")
        initial = api.request("GET", SETTINGS, native)
        configuration.validate_dto(core, initial)
        scope = SettingsScope(configuration, tasks, core, database, api, tasks.indexed(baseline, "managed_settings")["1"], initial)
        api.scope = scope
        recovery_intent = {"owner": OWNER, "verification_run": number, "stage": "prepared-before-initial-cas", "deployment": deployment,
                           "source_evidence_sha256": evidence_hash, "original_row": scope.original, "initial_dto": initial,
                           "temporary_name": scope.name, "original_revision": scope.revision, "fixed_restore_revision": scope.revision + 1,
                           "writer_user_id": native.user_id, "writer_session_id": native.id}
        secrets_absent(json.dumps(recovery_intent, ensure_ascii=True, separators=(",", ":")).encode(), api.credential_values())
        utilities.private_write(intent_path, recovery_intent)
        report["settings_recovery_intent"] = {"file": intent_path.name, "canonical_content_sha256": utilities.aggregate(recovery_intent),
                                               "mode": "0600", "contains_authentication_secrets": False, "contains_diagnostic_bodies": False}
        stage = "one actual native CAS change and full original restoration"
        scope.install()
        scope.restore()
        check(scope.restored and api.write_response_failures == 0, "The sole replacement and original-state restoration did not complete cleanly")
        current = snapshot(tasks, core, utilities, database)
        added = audit_activity(tasks, core, baseline, current, api, scope, False)
        stage = "real committed activity through native and Emby projections"
        report["activity"] = native_activity(configuration, tasks, core, api, added)
        stage = "bounded native and Emby diagnostic observations"
        report["diagnostics"] = diagnostics(core, api)
        report["status"] = "passed"
    except BaseException as error:
        report["failed_stage"] = stage
        report["error"] = str(error) if isinstance(error, Failure) else "A protected operation failed; exception details were withheld"
    finally:
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, 0)
        credentials.clear()
        FINAL_END = time.monotonic() + 45
        if core is not None:
            core.CLEANING = True
        if sys.platform == "linux":
            signal.signal(signal.SIGALRM, deadline)
        if api is not None:
            for login in api.logins:
                if not login.attempted:
                    continue
                if not login.token:
                    report["cleanup"]["errors"].append("A login response was lost; no unknown credential was guessed or reused")
                    continue
                def bind_owned(login=login):
                    if login is api.native and not login.csrf:
                        api.request("GET", SESSION, login)
                        check(login.csrf, "The owned native CSRF token could not be recovered")
                    if login.initial is None:
                        configuration.bind_login(tasks, core, api, baseline, snapshot(tasks, core, utilities, database), login)
                cleanup_step(3, bind_owned, report["cleanup"]["errors"],
                             "A fresh credential could not be bound or its native cleanup CSRF token recovered")
            if scope is not None:
                # Final proof after native revocation can still observe a
                # delayed CAS. A restore timeout introduces no new authority.
                cleanup_step(8, scope.restore, report["cleanup"]["errors"])
            for login in (api.emby, api.native):
                if not login.attempted:
                    continue
                cleanup_step(8, lambda login=login: logout(configuration, tasks, core, database, api, login), report["cleanup"]["errors"],
                             "An acknowledged credential lacks both protected denial and committed revocation proof")
            report.update({"http_method_counts": api.counts, "http_evidence": api.evidence, "write_response_failures": api.write_response_failures,
                           "logout_response_failures": api.logout_response_failures, "logout_protected_statuses": {login.kind: 401 for login in api.logins if login.denied}})
            if api.write_response_failures or api.logout_response_failures:
                report["status"] = "failed"
        def final_proof():
            if scope is not None:
                try:
                    scope.after_revocation()
                except BaseException:
                    report["cleanup"]["errors"].append("Complete original settings remain dirty or unproven after the writer revocation barrier")
                report["settings_restoration"] = {"initial_cas_attempts": scope.name_attempts, "restore_attempts": scope.restore_attempts,
                                                  "fixed_original_plus_one_restore_revision": True, "exact_original_state_proven": scope.final_proven,
                                                  "not_applied_proven_after_writer_revocation": scope.no_commit,
                                                  "original_name_mode": scope.initial["ServerNameMode"],
                                                  "original_encoding_and_all_nullable_overrides_preserved": scope.final_proven}
            if baseline is not None:
                try:
                    final = snapshot(tasks, core, utilities, database)
                    report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                    history = audit(configuration, tasks, core, baseline, final, api, scope)
                    if report["status"] == "passed":
                        check(history["new_sessions"] == 2 and history["new_devices"] == 1 and history["new_activity_entries"] == 6 and scope.restored,
                              "A successful run lacks its exact bounded session, device, activity and restored-settings history")
                    report["owned_history"] = history
                    report["cleanup"].update({"proven": True, "physical_history_deletions": 0})
                    report["all_old_rows_preserved_except_two_settings_revision_and_timestamp_advances"] = True
                    report["all_old_activity_rows_exact"] = True
                except BaseException:
                    report["cleanup"]["errors"].append("Complete old-row preservation, exact owned history or original settings could not be proven")
            for label, original, observe in (("deployment", deployment, lambda: sessions.deployment(accepted)),
                                              ("vault", vault, lambda: core.vault_identity(sessions)),
                                              ("media", media, lambda: devices.source_hashes(core, baseline, configured)),
                                              ("nfo", nfos, lambda: tasks.nfo_hashes(core, baseline, configured)),
                                              ("diagnostic_directory", log_identity, log_directory_identity),
                                              ("diagnostic_configuration", log_configuration, lambda: diagnostic_deployment(core, deployment))):
                if original is not None:
                    try:
                        remaining()
                        check(observe() == original, "A protected service, vault, source or diagnostic directory identity changed")
                        report[label + "_unchanged"] = True
                    except BaseException:
                        report["cleanup"]["errors"].append("A protected process, vault, source or diagnostic directory could not be proven unchanged")
            if inputs:
                try:
                    remaining()
                    check(all(fingerprint(core, path) == original for path, original in inputs.items()), "A private runtime, login or accepted source input changed")
                    report["private_runtime_credentials_and_source_evidence_unchanged"] = True
                except BaseException:
                    report["cleanup"]["errors"].append("Private runtime, login and accepted source inputs could not be proven unchanged")

        if core is not None:
            cleanup_step(15, final_proof, report["cleanup"]["errors"], "The bounded final preservation proof did not complete")
        if database is not None:
            report["database_queries"] = database.queries
        if not report["cleanup"]["proven"] or report["cleanup"]["errors"]:
            report["status"] = "failed"
        report["elapsed_seconds"] = round(time.monotonic() - STARTED, 3)
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, 0)
        if parent_identity is not None:
            try:
                info = ROOT.lstat()
                check((info.st_dev, info.st_ino) == parent_identity and info.st_uid == 0 and info.st_mode & 0o022 == 0, "The fixed evidence directory changed")
                utilities.private_write(result_path, report)
            except BaseException:
                report = {"owner": OWNER, "status": "failed", "error": "Exclusive aggregate evidence could not be saved; no overwrite was attempted"}
        print(json.dumps(report, ensure_ascii=True, sort_keys=True))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
