#!/usr/bin/env python3
"""Perform one bounded M5g acceptance on the explicitly accepted main service.

Run only through root SSH on Linux, without arguments. Require canonical
GOBY_SETTINGS_EXPECTED_PID and GOBY_SETTINGS_EXPECTED_BINARY_SHA256 inputs.
GOBY_SETTINGS_SOURCE_EVIDENCE_FILE must name the root-private canonical file
/opt/goby-test/exec-scratch/m5g-candidate.json; its actual bytes must match
GOBY_SETTINGS_EXPECTED_SOURCE_SHA256. No candidate digest or PID is guessed.
GOBY_SETTINGS_VERIFICATION_RUN selects decimal 1..99, default 1. Exclusive
reports use /opt/goby-test/m5g-deployed-settings[-attempt-N] with .json,
.attempt.json and, on failure, .private.json suffixes. Never overwrite them.

Use one newly issued native administrator cookie and its CSRF token, obtained
from existing root-private browser.env credentials. Never use stored tokens.
Observe the complete settings DTO, anonymous PublicInfo and native overview.
Temporarily replace all five overrides once: a unique name plus safe ceilings
no greater than the original effective limits. This publishes future planning
defaults; no playback or planning request is made. Restore the original five
overrides, including every null and explicit default, through native CAS.

Every restore requires a fresh matching published DTO and read-only database
row proving the exact owned temporary content at original revision plus one.
At most two restore requests use that same fixed revision and original body.
An uncertain response is followed by bounded reads, never a blind write retry
or adoption of another writer's revision. Observing old state while a request
may still commit does not prove restoration. Preserve uncertain evidence.

Preserve all old rows in the other twenty-seven tables exactly. Keep the one
new revoked native session; only its revoked_at changes after login. The
managed singleton may retain only the two revision/timestamp advances after
its exact overrides are restored. Preserve defaults, startup files, service
PID/executable, all media/NFO bytes and identities, and the private master.
No service restart, Emby/key login, scan/task/media/device management, SQL
write, history deletion or preexisting-credential operation is permitted.

Reuse only inert helpers from the accepted deployed verifiers. Export only
sanitized aggregate evidence. Bounded raw non-session HTTP bodies are saved
only in a root-0600 failure log; never log authentication headers or bodies.
"""

import base64
from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timedelta, timezone
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
import sys
import time


sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test")
EVIDENCE = ROOT / "exec-scratch/m5g-candidate.json"
ORIGIN = "http://127.0.0.1:18096"
OWNER = "goby-settings-deployed-m5g-v1"
SETTINGS = "/admin/v1/settings"
SESSION = "/admin/v1/session"
PUBLIC = "/emby/System/Info/Public"
OVERVIEW = "/admin/v1/overview"
FIELDS = {"ServerName": "server_name", "MaxBitrate": "max_bitrate", "MaxWidth": "max_width",
          "MaxHeight": "max_height", "MaxAudioChannels": "max_audio_channels"}
BOUNDS = {"MaxBitrate": 1_000_000_000, "MaxWidth": 8192, "MaxHeight": 8192, "MaxAudioChannels": 8}
DTO_FIELDS = {"Revision", "Defaults", "Overrides", "Effective", "Sources", "UpdatedAt", "Deployment"}
DEPLOYMENT_FIELDS = {"TranscodingEnabled", "HardwareDecoder", "HardwareEncoder", "Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs"}
ROW_FIELDS = set(FIELDS.values()) | {"id", "revision", "created_at", "updated_at"}
ID, HASH, NUMBER, TOKEN = (re.compile(pattern) for pattern in
                         (r"[0-9a-f]{32}", r"[0-9a-f]{64}", r"[1-9][0-9]*", r"[A-Za-z0-9_-]{43}"))
MAX_BODY, MAX_TRACE = 2 * 1024 * 1024, 4 * 1024 * 1024
STARTED = time.monotonic()


class Failure(Exception):
    """Contain only a fixed assertion label, never response or secret text."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def deadline(_number, _frame):
    raise Failure("The bounded deployed settings deadline was reached")


def helpers():
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        spec = importlib.util.spec_from_file_location("settings_deployed_task_helpers", Path(__file__).with_name("verify-tasks-deployed.py"))
        check(spec is not None and spec.loader is not None, "The inert deployed helpers are unavailable")
        tasks = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(tasks)
        devices, core, utilities, sessions = tasks.helpers()
    core.STARTED = STARTED
    core.TABLES |= {"managed_settings"}
    core.MUTABLE = {"sessions"}
    return tasks, devices, core, utilities, sessions


def output_files():
    value = os.environ.get("GOBY_SETTINGS_VERIFICATION_RUN", "1")
    check(re.fullmatch(r"[1-9][0-9]?", value), "The verification run must be canonical decimal one through ninety-nine")
    stem = "m5g-deployed-settings" + ("" if value == "1" else "-attempt-" + value)
    return int(value), ROOT / (stem + ".json"), ROOT / (stem + ".attempt.json"), ROOT / (stem + ".private.json")


def fingerprint(core, path):
    check(path in {ROOT / "runtime.env", ROOT / "browser.env", EVIDENCE} and path.resolve(strict=True) == path,
          "A private input escaped its fixed canonical pathname")
    parent = path.parent.lstat()
    check(stat.S_ISDIR(parent.st_mode) and parent.st_uid == 0 and parent.st_mode & 0o022 == 0,
          "A private input directory has unsafe ownership")
    descriptor = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        for component in path.parts[1:-1]:
            child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
        source = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME, dir_fd=descriptor)
    finally:
        os.close(descriptor)
    with os.fdopen(source, "rb") as stream:
        info = os.fstat(stream.fileno())
        bound = MAX_BODY if path == EVIDENCE else 65536
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
              info.st_nlink == 1 and 0 < info.st_size <= bound, "A private input is not root-private and bounded")
        data = stream.read(bound + 1)
        check(len(data) == info.st_size and core.file_identity(info) == core.file_identity(os.fstat(stream.fileno())) == core.file_identity(path.lstat()),
              "A private input changed while its identity was observed")
    return core.file_identity(info), hashlib.sha256(data).hexdigest()


def snapshot(tasks, core, utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == core.TABLES and len(value) == 28 and sum(map(len, value.values())) <= 5000,
          "The complete main snapshot escaped the twenty-eight-table bound")
    migrations = tasks.records(value, "schema_migrations")
    check(len(migrations) == 20 and {row["version"] for row in migrations} == set(range(1, 21)), "The main schema is not exactly version twenty")
    media = [row for row in tasks.records(value, "items") if row["media"] is not None]
    check(len(value["libraries"]) == 5 and len(media) == 11 and all(row["media"].get("ProbeVersion") == 6 for row in media),
          "The accepted five-library and eleven-media catalog changed")
    check(len(value["managed_settings"]) == 1 and set(tasks.indexed(value, "managed_settings")) == {"1"},
          "The managed settings singleton inventory changed")
    return value


def database_type(core, sessions):
    class Database(core.database_type(sessions)):
        def __init__(self):
            super().__init__()
            self.queries = 0

        def read(self, query, label):
            check(self.queries < (80 if core.CLEANING else 50), "The read-only database query budget was exhausted")
            self.queries += 1
            return super().read(query, label)
    return Database


def idle(tasks, core, database, value):
    tasks.idle(core, value)
    now = core.timestamp(database.read("SELECT to_json(clock_timestamp());", "Observe the protected playback clock"))
    check(not any(row["state"] in {"Prepared", "Playing", "Paused"} and core.timestamp(row["expires_at"]) > now
                  for row in tasks.records(value, "play_sessions")), "An existing playback is active; no settings were changed")


def validate_dto(core, value):
    check(isinstance(value, dict) and set(value) == DTO_FIELDS and isinstance(value["Revision"], str) and
          NUMBER.fullmatch(value["Revision"]) and int(value["Revision"]) <= 9223372036854775807,
          "The settings DTO omitted its exact fields or canonical safe revision")
    for section in ("Defaults", "Overrides", "Effective", "Sources"):
        check(isinstance(value[section], dict) and set(value[section]) == set(FIELDS), "A settings section omitted its exact five fields")
    for section in ("Defaults", "Overrides", "Effective"):
        for field, item in value[section].items():
            if section == "Overrides" and item is None:
                continue
            if field == "ServerName":
                check(isinstance(item, str) and 1 <= len(item.encode("utf-8")) <= 128 and "\x00" not in item,
                      "The settings name is not a bounded valid string")
            else:
                check(type(item) is int and 1 <= item <= BOUNDS[field], "A settings numeric value is not a bounded JSON integer")
    for field in FIELDS:
        override = value["Overrides"][field]
        check(value["Effective"][field] == (value["Defaults"][field] if override is None else override) and
              value["Sources"][field] == ("deployment" if override is None else "database"),
              "A settings effective value or source differs from its nullable override")
    deployment = value["Deployment"]
    check(isinstance(deployment, dict) and set(deployment) == DEPLOYMENT_FIELDS and type(deployment["TranscodingEnabled"]) is bool and
          all(isinstance(deployment[field], str) and len(deployment[field]) <= 128 for field in ("HardwareDecoder", "HardwareEncoder")) and
          all(type(deployment[field]) is int and 0 <= deployment[field] <= 1_000_000_000 for field in ("Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs")),
          "The startup deployment DTO escaped its safe whitelist")
    check(isinstance(value["UpdatedAt"], str) and value["UpdatedAt"].endswith("Z") and
          core.timestamp(value["UpdatedAt"]).utcoffset() == timedelta(0), "The settings timestamp is not native UTC")


class API:
    def __init__(self, core, login):
        self.core, self.login = core, login
        self.counts = {"GET": 0, "POST": 0, "PUT": 0, "DELETE": 0}
        self.trace, self.trace_bytes = [], 0
        self.scope = None
        self.logout_denied = False
        self.last_status = None
        self.write_response_failures = 0

    def request(self, method, path, *, body=None, expected=200, raw=False, anonymous=False, action=None):
        signing_in = (method, path) == ("POST", SESSION)
        check((path == PUBLIC) == anonymous and method in self.counts, "An HTTP request escaped its fixed native or anonymous scope")
        if signing_in:
            check(not self.login.attempted and not self.login.token, "An additional login or existing token reuse was prevented")
        elif method == "GET":
            check(path in {SESSION, SETTINGS, OVERVIEW, PUBLIC}, "A read escaped the exact settings endpoint whitelist")
        elif (method, path) == ("DELETE", SESSION):
            check(self.login.token and self.login.csrf and self.login.initial is not None and ID.fullmatch(self.login.id) and
                  not self.login.logout_attempted, "An additional or unowned logout was prevented")
        else:
            check(method == "PUT" and path == SETTINGS and self.scope is not None and self.login.initial is not None and
                  self.login.token and self.login.csrf, "A mutation escaped the fixed managed settings scope")
            self.scope.authorize(action, body)
        check(body is None or signing_in or method == "PUT", "A read or logout included an unapproved request body")
        self.core.remaining()
        check(sum(self.counts.values()) < (60 if self.core.CLEANING else 40), "The bounded settings HTTP budget was exhausted")
        self.counts[method] += 1
        headers = {"Accept": "application/json"}
        if not anonymous:
            headers["Origin"] = ORIGIN
            if self.login.token:
                headers["Cookie"] = "goby_session=" + self.login.token
            if self.login.csrf:
                headers["X-CSRF-Token"] = self.login.csrf
        payload = None if body is None else json.dumps(body, ensure_ascii=True, separators=(",", ":")).encode("utf-8")
        check(payload is None or len(payload) <= 16384, "A settings request exceeded the native body bound")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if signing_in:
            self.login.attempted = True
        if (method, path) == ("DELETE", SESSION):
            self.login.logout_attempted = True
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=self.core.remaining())
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            self.last_status = response.status
            cookies = SimpleCookie()
            cookies.load(response.getheader("Set-Cookie", ""))
            if signing_in and "goby_session" in cookies and TOKEN.fullmatch(cookies["goby_session"].value):
                self.login.token = cookies["goby_session"].value
            data = response.read(MAX_BODY + 1)
            check(len(data) <= MAX_BODY, "A settings HTTP response exceeded its byte bound")
            if path != SESSION:
                self.trace_bytes += len(data) + len(payload or b"") + len(path)
                check(self.trace_bytes <= MAX_TRACE, "The private settings trace exceeded its byte bound")
                self.trace.append({"method": method, "path": path, "request_body": body, "status": response.status,
                                   "response_body_base64": base64.b64encode(data).decode("ascii")})
            value = data if raw else json.loads(data) if data else None
            if path == SESSION and isinstance(value, dict):
                csrf = value.get("CSRFToken")
                if isinstance(csrf, str) and HASH.fullmatch(csrf) and self.login.token:
                    check(csrf == hashlib.sha256(("goby:admin:csrf:" + self.login.token).encode()).hexdigest(),
                          "The CSRF token does not belong to the newly issued native cookie")
                    self.login.csrf = csrf
            check(response.status in ((expected,) if isinstance(expected, int) else expected), "A settings HTTP response returned an unexpected status")
            check(response.status != 204 or not data, "An HTTP 204 response included content")
            return value
        except BaseException:
            if method == "PUT":
                self.write_response_failures += 1
            raise
        finally:
            connection.close()


class SettingsScope:
    def __init__(self, tasks, core, database, api, original, initial):
        self.tasks, self.core, self.database, self.api = tasks, core, database, api
        self.original, self.initial = original, initial
        self.revision = original["revision"]
        check(type(self.revision) is int and 1 <= self.revision <= 9223372036854775805, "The baseline revision has insufficient safe CAS headroom")
        self.temporary = {"ServerName": "M5g temporary settings " + secrets.token_hex(16)}
        for field, ceiling in (("MaxBitrate", 1_000_000), ("MaxWidth", 640), ("MaxHeight", 360), ("MaxAudioChannels", 2)):
            self.temporary[field] = min(ceiling, max(1, initial["Effective"][field] // 2))
        check(self.temporary["ServerName"] != initial["Effective"]["ServerName"], "The unique temporary settings marker collided")
        self.temp_attempts = self.restore_attempts = 0
        self.temp_row = self.restored_row = self.grant = None
        self.lower = None
        self.restored = False
        self.no_commit = False
        self.revocation_barrier = False
        self.unresolved = False

    def observe(self):
        # GET publication and the SQL observation are separate reads. A race
        # grants no write; repeat only these bounded read operations.
        for _ in range(3):
            shown = self.api.request("GET", SETTINGS)
            validate_dto(self.core, shown)
            check(shown["Defaults"] == self.initial["Defaults"] and shown["Deployment"] == self.initial["Deployment"],
                  "The published deployment defaults changed during the owned scope")
            observation = self.database.read("SELECT json_build_object('row',to_jsonb(s),'observed_at',clock_timestamp()) FROM managed_settings s WHERE id=1;",
                                             "Observe published settings against the stored singleton")
            check(isinstance(observation, dict) and isinstance(observation.get("row"), dict), "The managed singleton observation is absent")
            row = observation["row"]
            check(set(row) == ROW_FIELDS and row["id"] == 1 and row["created_at"] == self.original["created_at"],
                  "The managed singleton identity or immutable columns changed")
            if (shown["Revision"] == str(row["revision"]) and self.core.timestamp(shown["UpdatedAt"]) == self.core.timestamp(row["updated_at"]) and
                    all(self.tasks.identical(shown["Overrides"][field], row[column]) for field, column in FIELDS.items())):
                return shown, row, self.core.timestamp(observation["observed_at"])
            time.sleep(0.05)
        raise Failure("Published and stored settings could not be observed consistently; no write was authorized")

    def expected_row(self, overrides, revision, updated_at):
        wanted = dict(self.original)
        wanted.update({column: overrides[field] for field, column in FIELDS.items()})
        wanted.update({"revision": revision, "updated_at": updated_at})
        return wanted

    def classify(self, row, now):
        if self.tasks.identical(row, self.original) and self.temp_row is None and self.restore_attempts == 0:
            return "original"
        check(self.temp_attempts == 1 and self.lower is not None,
              "A settings change has no owned temporary request")
        # clock_timestamp does not promise monotonic wall time. The fixed CAS
        # revision, unique full replacement and exact captured row establish
        # ownership; timestamp ordering across separate commits does not.
        self.core.timestamp(row["updated_at"])
        if self.tasks.identical(row, self.expected_row(self.temporary, self.revision + 1, row["updated_at"])):
            check(self.temp_row is None or self.tasks.identical(row, self.temp_row), "The previously proven temporary settings row changed")
            self.temp_row = row
            return "temporary"
        if self.restore_attempts and self.tasks.identical(row, self.expected_row(self.initial["Overrides"], self.revision + 2, row["updated_at"])):
            check(self.temp_row is not None and
                  (self.restored_row is None or self.tasks.identical(row, self.restored_row)), "The restored settings row has an unowned timestamp or change")
            self.restored, self.restored_row = True, row
            return "restored"
        raise Failure("Concurrent or unproven settings state prevents restoration; no replacement was issued")

    def authorize(self, action, body):
        check(action in {"temporary", "restore"} and self.grant == action, "A settings write lacks a fresh single-use ownership proof")
        self.grant = None
        if action == "temporary":
            check(self.temp_attempts == 0 and body == {"Revision": str(self.revision), "Overrides": self.temporary}, "An additional temporary settings write was prevented")
            self.temp_attempts += 1
        else:
            check(self.temp_attempts == 1 and self.temp_row is not None and not self.restored and self.restore_attempts < 2 and
                  body == {"Revision": str(self.revision + 1), "Overrides": self.initial["Overrides"]}, "An unowned or additional settings restoration was prevented")
            self.restore_attempts += 1

    def install(self):
        _shown, row, now = self.observe()
        check(self.tasks.identical(row, self.original), "The original settings changed before the single temporary write")
        self.lower, self.grant = now, "temporary"
        receipt = self.api.request("PUT", SETTINGS, body={"Revision": str(self.revision), "Overrides": self.temporary}, action="temporary")
        shown, row, now = self.observe()
        check(self.classify(row, now) == "temporary" and shown == receipt and shown["Effective"] == self.temporary and
              set(shown["Sources"].values()) == {"database"}, "The temporary commit was not published as the exact acknowledged override snapshot")
        return shown

    def restore(self):
        if not self.temp_attempts:
            return
        # The retry cap is global across normal and finally paths. Both writes
        # keep the same CAS revision, so a late response cannot advance twice.
        for _ in range(6):
            try:
                shown, row, now = self.observe()
                state = self.classify(row, now)
            except (OSError, http.client.HTTPException):
                continue
            if state == "restored":
                check(all(shown[field] == self.initial[field] for field in ("Defaults", "Overrides", "Effective", "Sources", "Deployment")),
                      "Settings restoration did not preserve every original nullable override and effective source")
                self.unresolved = False
                return
            if state == "temporary" and self.restore_attempts < 2:
                self.grant = "restore"
                try:
                    receipt = self.api.request("PUT", SETTINGS, body={"Revision": str(self.revision + 1), "Overrides": self.initial["Overrides"]},
                                               action="restore", expected=(200, 409))
                except Exception:
                    # The response may be lost, malformed, or an error after
                    # commit. The next iteration observes state before it can
                    # authorize any further fixed-revision request.
                    continue
                if self.api.last_status == 200:
                    validate_dto(self.core, receipt)
                    check(receipt["Revision"] == str(self.revision + 2) and receipt["Overrides"] == self.initial["Overrides"],
                          "The restoration receipt has another revision or override set")
                else:
                    # A conflict may be our earlier delayed restore or another
                    # writer. It can prove a clean final value after revocation,
                    # but cannot make this uninterrupted acceptance pass.
                    self.api.write_response_failures += 1
                    check(isinstance(receipt, dict) and receipt.get("Error", {}).get("Code") == "revision_conflict",
                          "The fixed restoration CAS conflict returned another error")
            # Old state after an unacknowledged PUT is not proof that a late
            # request cannot still commit. Observe only; never resubmit it.
            time.sleep(0.1)
        self.unresolved = True
        raise Failure("The bounded observations did not prove exact settings restoration")

    def after_revocation(self):
        login = self.api.login
        check(self.api.logout_denied and login.initial is not None and ID.fullmatch(login.id),
              "The final settings observation lacks an acknowledged revoked credential")
        row = self.database.read("SELECT to_jsonb(s) FROM sessions s WHERE id='" + login.id + "';", "Confirm the owned native revocation barrier")
        check(isinstance(row, dict) and row.get("revoked_at") is not None, "The owned session revocation is not committed")
        wanted = dict(login.initial)
        wanted["revoked_at"] = row["revoked_at"]
        check(self.tasks.identical(row, wanted), "The committed revocation row differs from the exact newly issued credential")
        # Settings writers hold this session FOR SHARE until COMMIT/ROLLBACK.
        # A committed logout therefore drains an earlier writer or prevents a
        # late writer's transactional revalidation. Only now can an unchanged
        # singleton prove that a lost temporary request never committed.
        observed = self.database.read("SELECT json_build_object('row',to_jsonb(s),'observed_at',clock_timestamp()) FROM managed_settings s WHERE id=1;",
                                      "Observe final settings after the committed native revocation")
        final = observed["row"]
        if self.tasks.identical(final, self.original) and self.temp_row is None and self.restore_attempts == 0:
            self.no_commit, self.unresolved = True, False
            self.revocation_barrier = True
            return
        check(self.classify(final, self.core.timestamp(observed["observed_at"])) == "restored",
              "Settings remain dirty or unproven after logout; no replacement credential or write was issued")
        self.unresolved = False
        self.revocation_barrier = True


def projections(api, expected_name, namespace, version=None):
    public = api.request("GET", PUBLIC, anonymous=True)
    overview = api.request("GET", OVERVIEW)
    check(isinstance(public, dict) and public.get("Id") == namespace and public.get("ServerName") == expected_name and
          public.get("ProductName") == "Goby" and public.get("LocalAddress") == ORIGIN and public.get("StartupWizardCompleted") is True,
          "Anonymous PublicInfo did not project the committed effective server name and namespace")
    server = overview.get("Server", {}) if isinstance(overview, dict) else {}
    check(server.get("Id") == namespace and server.get("Name") == expected_name and isinstance(server.get("Version"), str) and
          server["Version"] == public.get("Version") and (version is None or server["Version"] == version),
          "The native overview did not project the same committed server name and service version")
    return server["Version"]


def bind_login(tasks, core, api, baseline, current):
    added = tasks.delta(baseline, current, "sessions")
    login = api.login
    digest = "\\x" + hashlib.sha256(login.token.encode("ascii")).hexdigest()
    matches = [row for row in added if row["token_hash"] == digest]
    check(len(matches) == 1, "The issued native cookie cannot be bound exclusively to one new row")
    row = matches[0]
    check(ID.fullmatch(row["id"]) and row["kind"] == "admin" and row["user_id"] == login.user_id and row["revoked_at"] is None,
          "The newly issued native credential has an unexpected identity or state")
    # Retain the exact owned identity before secondary projection assertions;
    # unrelated new history must fail the audit without stranding this token.
    login.id, login.initial = row["id"], row
    check(len(added) == 1 and row["device_registry_id"] is None and row["client_name"] == "Goby Dashboard" and row["device_id"] == "goby-dashboard" and
          row["device_name"] == "Web browser" and row["created_at"] == row["last_seen_at"] and
          core.timestamp(row["expires_at"]) - core.timestamp(row["created_at"]) == timedelta(hours=24),
          "The fresh native login has unexpected identity, registration, or activity fields")


def audit(tasks, core, baseline, final, api, scope):
    for table in core.TABLES - {"sessions", "managed_settings"}:
        check(baseline[table] == final[table], "A protected old table changed during the settings-only workflow")
    added = tasks.delta(baseline, final, "sessions")
    if api is None or not api.login.attempted:
        check(not added, "A session appeared without the authorized fresh login")
    else:
        login = api.login
        check(login.initial is not None and len(added) == 1 and added[0]["id"] == login.id and api.logout_denied,
              "The owned login lacks exclusive retained history or independent logout denial")
        wanted = dict(login.initial)
        wanted["revoked_at"] = added[0]["revoked_at"]
        check(wanted["revoked_at"] is not None and tasks.identical(added[0], wanted), "The owned session changed beyond its exact logout timestamp")
        core.timestamp(wanted["revoked_at"])
    row = tasks.indexed(final, "managed_settings")["1"]
    if scope is None or not scope.temp_attempts:
        check(baseline["managed_settings"] == final["managed_settings"], "Settings changed without the owned temporary request")
    elif scope.no_commit:
        check(scope.revocation_barrier and baseline["managed_settings"] == final["managed_settings"], "Settings changed after the committed native revocation barrier")
    else:
        check(scope.revocation_barrier and scope.restored and scope.restored_row is not None and tasks.identical(row, scope.restored_row),
              "The final singleton lacks an exact proven restoration; unknown or concurrent state was not overwritten")
    return len(added)


def main():
    report = {"owner": OWNER, "status": "failed", "cleanup": {"proven": False, "errors": []}, "snapshots": {},
              "limits": {"http_requests": 60, "response_bytes": MAX_BODY, "database_queries": 80, "workflow_seconds": 180,
                         "temporary_puts": 1, "restore_puts": 2}, "http_transport_retries": 0,
              "allowed_old_row_changes": {"managed_settings": "Only revision and updated_at remain advanced after exact original overrides are restored"},
              "limitations": ["Published effective ceilings affect future planning snapshots; no planning, playback, or media request is executed",
                              "No main restart; isolated acceptance covers persisted settings across restarts",
                              "A lost write response permits only bounded observation and a proven fixed-revision restoration; unknown state is never overwritten"]}
    tasks = devices = core = utilities = sessions = database = baseline = api = scope = deployment = vault = media = nfos = configured = parent_identity = None
    inputs, credentials = {}, {}
    stage = "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without arguments")
        signal.signal(signal.SIGALRM, deadline)
        signal.setitimer(signal.ITIMER_REAL, max(0.1, 120 - (time.monotonic() - STARTED)))
        accepted, expected_pid = os.environ.get("GOBY_SETTINGS_EXPECTED_BINARY_SHA256", ""), os.environ.get("GOBY_SETTINGS_EXPECTED_PID", "")
        evidence_hash = os.environ.get("GOBY_SETTINGS_EXPECTED_SOURCE_SHA256", "")
        check(HASH.fullmatch(accepted) and NUMBER.fullmatch(expected_pid) and 1 < int(expected_pid) <= 2147483647 and
              HASH.fullmatch(evidence_hash) and os.environ.get("GOBY_SETTINGS_SOURCE_EVIDENCE_FILE") == str(EVIDENCE),
              "The accepted binary, positive PID and fixed private source evidence constraints are required")
        number, result_path, attempt_path, private_path = output_files()
        report["verification_run"] = number
        report["output_scope"] = {"directory": str(ROOT), "report": result_path.name, "attempt_marker": attempt_path.name, "private_failure_log": private_path.name}
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed output directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (result_path, attempt_path, private_path)), "The selected attempt already exists; no overwrite is allowed")
        tasks, devices, core, utilities, sessions = helpers()
        utilities.private_write(attempt_path, {"owner": OWNER, "verification_run": number, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                                               "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
        parent_identity = info.st_dev, info.st_ino
        for path in (EVIDENCE, ROOT / "runtime.env", ROOT / "browser.env"):
            inputs[path] = fingerprint(core, path)
        check(inputs[EVIDENCE][1] == evidence_hash, "The private source evidence differs from the explicitly accepted manifest")
        deployment = sessions.deployment(accepted)
        check(deployment["main_pid"] == int(expected_pid), "The live main PID differs from the explicitly accepted deployment")
        runtime = sessions.private_values(ROOT / "runtime.env", {"GOBY_LISTEN", "GOBY_PUBLIC_URL", "GOBY_MEDIA_ROOTS"})
        check(runtime["GOBY_LISTEN"] == "127.0.0.1:18096" and runtime["GOBY_PUBLIC_URL"] == ORIGIN, "The private main runtime escaped its fixed loopback endpoint")
        configured = runtime["GOBY_MEDIA_ROOTS"]
        database = database_type(core, sessions)()
        baseline = snapshot(tasks, core, utilities, database)
        report["snapshots"]["before"] = utilities.snapshot_report(baseline)
        idle(tasks, core, database, baseline)
        namespace = [row["value"] for row in tasks.records(baseline, "server_settings") if row["key"] == "server_id"]
        check(len(namespace) == 1 and ID.fullmatch(namespace[0]), "The persistent main server namespace is invalid")
        report["deployment"] = {**deployment, "schema_version": 20, "database_role": "goby_test", "database_port": 5432,
                                "postgresql_forced_read_only": True, "source_evidence_sha256": evidence_hash,
                                "server_namespace_sha256": hashlib.sha256(namespace[0].encode()).hexdigest()}
        vault = core.vault_identity(sessions)
        media, nfos = devices.source_hashes(core, baseline, configured), tasks.nfo_hashes(core, baseline, configured)
        report["source_hashes_before"] = {"media_count": len(media), "media_sha256": utilities.aggregate(media), "nfo_count": len(nfos), "nfo_sha256": utilities.aggregate(nfos)}
        credentials = sessions.private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        users = [row for row in tasks.records(baseline, "users") if row["name"] == credentials["GOBY_SMOKE_NAME"] and row["is_administrator"] and not row["is_disabled"]]
        check(len(users) == 1, "The private administrator credentials do not identify exactly one enabled existing administrator")
        api = API(core, devices.Login("admin", users[0]["id"]))
        stage = "one fresh native administrator credential"
        signed = api.request("POST", SESSION, body={"Name": credentials["GOBY_SMOKE_NAME"], "Password": credentials["GOBY_SMOKE_PASSWORD"]})
        credentials.clear()
        check(api.login.token and api.login.csrf and signed.get("User", {}).get("Id") == api.login.user_id, "The native login omitted its new cookie or selected another account")
        current = snapshot(tasks, core, utilities, database)
        core.preserve(baseline, current)
        bind_login(tasks, core, api, baseline, current)
        initial = api.request("GET", SETTINGS)
        validate_dto(core, initial)
        scope = SettingsScope(tasks, core, database, api, tasks.indexed(baseline, "managed_settings")["1"], initial)
        api.scope = scope
        shown, row, _now = scope.observe()
        check(shown == initial and tasks.identical(row, scope.original), "The initial published settings differ from the complete baseline singleton")
        version = projections(api, initial["Effective"]["ServerName"], namespace[0])
        check(api.login.initial["client_version"] == version, "The native login client version differs from the accepted service")
        stage = "single temporary settings publication"
        current = snapshot(tasks, core, utilities, database)
        core.preserve(baseline, current)
        idle(tasks, core, database, current)
        temporary = scope.install()
        projections(api, temporary["Effective"]["ServerName"], namespace[0], version)
        stage = "exact full nullable override restoration"
        scope.restore()
        check(api.write_response_failures == 0, "A settings write response failed even though bounded restoration was attempted")
        restored, _row, _now = scope.observe()
        projections(api, restored["Effective"]["ServerName"], namespace[0], version)
        report["checks"] = {"issued_credentials": 1, "native_cookie_only": True, "temporary_override_fields": 5,
                            "temporary_sources_database": True, "published_limits_never_increased": True,
                            "anonymous_and_overview_names_follow_commits": True, "original_nullable_overrides_restored": True,
                            "defaults_and_startup_deployment_unchanged": True, "media_or_planning_requests": 0}
        report["status"] = "passed"
    except BaseException as error:
        report["failed_stage"] = stage
        report["error"] = str(error) if isinstance(error, Failure) else "A protected operation failed; exception details were withheld"
    finally:
        credentials.clear()
        if core is not None:
            core.CLEANING = True
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, max(0.1, 178 - (time.monotonic() - STARTED)))
        if api is not None and api.login.attempted:
            login = api.login
            if login.token:
                if not login.csrf:
                    try:
                        api.request("GET", SESSION)
                        check(login.csrf, "The owned native cleanup CSRF token is absent")
                    except BaseException:
                        report["cleanup"]["errors"].append("The owned native CSRF token could not be recovered")
                if login.initial is None:
                    try:
                        bind_login(tasks, core, api, baseline, snapshot(tasks, core, utilities, database))
                    except BaseException:
                        report["cleanup"]["errors"].append("The issued cookie could not be bound to its exclusive new native session")
                if scope is not None:
                    try:
                        scope.restore()
                    except BaseException:
                        scope.unresolved = True
                try:
                    if not login.logout_attempted:
                        try:
                            api.request("DELETE", SESSION, expected=204, raw=True)
                        except (OSError, http.client.HTTPException):
                            pass
                    # A fresh connection carries only this newly issued cookie;
                    # 401 and the retained revoked row prove its invalidation.
                    api.request("GET", SETTINGS, expected=401, raw=True)
                    api.logout_denied = True
                    report["native_logout_protected_status"] = 401
                except BaseException:
                    report["cleanup"]["errors"].append("The owned native logout and independent protected denial could not be confirmed")
                if scope is not None:
                    try:
                        scope.after_revocation()
                    except BaseException:
                        report["cleanup"]["errors"].append("Exact settings restoration after the committed logout barrier remains unproven; unknown state was not overwritten")
            else:
                report["cleanup"]["errors"].append("The fresh credential response was lost; no existing token or unknown session was used")
            report["http_method_counts"] = api.counts
        if scope is not None:
            report["settings_restoration"] = {"temporary_put_attempts": scope.temp_attempts, "restore_put_attempts": scope.restore_attempts,
                                              "exact_original_overrides_proven": False, "not_applied_proven_after_revocation": False,
                                              "fixed_restore_revision": True, "write_response_failures": api.write_response_failures}
        if baseline is not None:
            try:
                final = snapshot(tasks, core, utilities, database)
                report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                count = audit(tasks, core, baseline, final, api, scope)
                if report["status"] == "passed":
                    check(count == 1 and scope is not None and scope.restored, "The successful workflow lacks its exact restored settings and one revoked credential")
                report["cleanup"].update({"proven": True, "new_credentials_revoked": count, "retained_native_sessions": count, "physical_history_deletions": 0})
                report["old_twenty_seven_table_rows_exact"] = True
                report["old_user_login_activity_unchanged"] = True
                report["managed_settings_only_revision_and_timestamp_advanced"] = scope is not None and scope.restored
                if scope is not None:
                    report["settings_restoration"].update({"exact_original_overrides_proven": scope.revocation_barrier,
                                                           "not_applied_proven_after_revocation": scope.revocation_barrier and scope.no_commit and scope.temp_attempts == 1})
            except BaseException:
                report["cleanup"]["errors"].append("Complete protected-row preservation or exact owned settings restoration could not be proven")
        for label, initial_value, observe in (("deployment", deployment, lambda: sessions.deployment(accepted)),
                                               ("vault", vault, lambda: core.vault_identity(sessions)),
                                               ("media", media, lambda: devices.source_hashes(core, baseline, configured)),
                                               ("nfo", nfos, lambda: tasks.nfo_hashes(core, baseline, configured))):
            if initial_value is not None:
                try:
                    core.remaining()
                    check(observe() == initial_value, "A protected process, master, media, or NFO identity changed")
                    report[label + "_unchanged"] = True
                except BaseException:
                    report["cleanup"]["errors"].append("A protected process, master, media, or NFO identity could not be proven unchanged")
        if inputs:
            try:
                check(all(fingerprint(core, path) == original for path, original in inputs.items()), "A private startup, credential, or candidate input changed")
                report["private_runtime_credentials_and_source_evidence_unchanged"] = True
            except BaseException:
                report["cleanup"]["errors"].append("Private startup, credential, and source evidence inputs could not be proven unchanged")
        if database is not None:
            report["database_queries"] = database.queries
        if vault is not None:
            report["vault"] = {"uid": 995, "mode": "0600", "bytes": 32, "directory_mode": "0700"}
        if not report["cleanup"]["proven"] or report["cleanup"]["errors"]:
            report["status"] = "failed"
        report["elapsed_seconds"] = round(time.monotonic() - STARTED, 3)
        if parent_identity is not None:
            try:
                info = ROOT.lstat()
                check((info.st_dev, info.st_ino) == parent_identity and info.st_uid == 0 and info.st_mode & 0o022 == 0, "The fixed output directory changed")
                if report["status"] != "passed":
                    tasks.private_failure_log(private_path, {"owner": OWNER, "failed_stage": report.get("failed_stage", "cleanup"), "settings_http": api.trace if api else []})
                    report["private_failure_log_retained"] = True
                utilities.private_write(result_path, report)
            except BaseException:
                report = {"owner": OWNER, "status": "failed", "error": "Exclusive private evidence could not be saved; no overwrite was attempted"}
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, 0)
        print(json.dumps(report, ensure_ascii=True, sort_keys=True))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
