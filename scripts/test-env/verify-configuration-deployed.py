#!/usr/bin/env python3
"""Verify one explicitly accepted M5h main deployment through root SSH only.

Require GOBY_CONFIGURATION_EXPECTED_PID and
GOBY_CONFIGURATION_EXPECTED_BINARY_SHA256. The required
GOBY_CONFIGURATION_SOURCE_EVIDENCE_FILE is exactly the canonical root-0600
/opt/goby-test/exec-scratch/m5h-candidate.json; its bytes must match
GOBY_CONFIGURATION_EXPECTED_SOURCE_SHA256. No old candidate or PID is assumed.
GOBY_CONFIGURATION_VERIFICATION_RUN selects decimal 1..99, default 1. Claim
exclusive m5h-deployed-configuration[-attempt-N].attempt.json, .json and
failure-only .private.json files under /opt/goby-test; never overwrite history.

Issue one fresh native administrator cookie and one fresh ordinary Emby login
on a unique reported device ID, using existing private browser.env credentials.
Read both configuration projections, then POST one Partial name and one named
encoding width. Preserve all native numeric overrides. Select a width no larger
than the existing combined ceiling; at its minimum, verify a documented no-op
instead of relaxing that ceiling. A unique name always proves a real revision
advance. Observe every committed change through native GET and PublicInfo.

Before any native restoration, revoke the Emby credential, independently prove
401 and read its exact committed revoked row. Its transaction-held session lock
then excludes late non-CAS compatibility writes. Classify only a proven owned
prefix, select its native restore revision once, and allow at most two CAS PUTs
with that fixed revision and all original Overrides, ServerNameMode and Encoding.
Never infer restoration merely because a timed-out write still shows old state.
After native logout and 401, confirm its committed revocation, then read the
singleton again and audit every old row in all twenty-eight tables.

Keep two revoked sessions and one ordinary device as history. Only the newly
owned Emby session/device last_seen_at fields may advance through real 15-second
Touch behavior; old rows remain exact. Never create keys, reuse credentials,
manage devices, play media, plan conversion, scan, run tasks, restart services,
write SQL or delete history. Preserve PID/executable, startup/credential/source
inputs, the master file and all recorded media/NFO bytes and file identities.

Only inert helpers from accepted deployed verifiers are imported. Reports are
sanitized aggregates. Bounded raw configuration HTTP evidence is retained only
in an exclusive root-0600 failure log; authentication headers and all login or
session response bodies are excluded. Python standard library only.
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
EVIDENCE = ROOT / "exec-scratch/m5h-candidate.json"
ORIGIN = "http://127.0.0.1:18096"
OWNER = "goby-configuration-deployed-m5h-v1"
SETTINGS, NATIVE_SESSION = "/admin/v1/settings", "/admin/v1/session"
EMBY_LOGIN, EMBY_LOGOUT = "/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"
CONFIGURATION, PUBLIC = "/emby/System/Configuration", "/emby/System/Info/Public"
PARTIAL, ENCODING = CONFIGURATION + "/Partial", CONFIGURATION + "/encoding"
FIELDS = {"ServerName": "server_name", "MaxBitrate": "max_bitrate", "MaxWidth": "max_width",
          "MaxHeight": "max_height", "MaxAudioChannels": "max_audio_channels"}
BOUNDS = {"MaxBitrate": 1_000_000_000, "MaxWidth": 8192, "MaxHeight": 8192, "MaxAudioChannels": 8}
MODES = {"deployment", "custom", "empty", "unset"}
DTO_FIELDS = {"Revision", "Defaults", "Overrides", "Effective", "Sources", "UpdatedAt", "Deployment", "ServerNameMode", "Encoding"}
DEPLOYMENT_FIELDS = {"HostName", "TranscodingEnabled", "HardwareDecoder", "HardwareEncoder", "Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs"}
ROW_FIELDS = set(FIELDS.values()) | {"id", "revision", "created_at", "updated_at", "server_name_mode", "compatibility_max_width"}
ID, HASH, NUMBER, TOKEN = (re.compile(pattern) for pattern in
                         (r"[0-9a-f]{32}", r"[0-9a-f]{64}", r"[1-9][0-9]*", r"[A-Za-z0-9_-]{43}"))
MAX_BODY, MAX_TRACE = 2 * 1024 * 1024, 4 * 1024 * 1024
STARTED = time.monotonic()


class Failure(Exception):
    """Contain only a fixed label, never a secret, response or SQL diagnostic."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def deadline(_number, _frame):
    raise Failure("The bounded deployed configuration deadline was reached")


def helpers():
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        spec = importlib.util.spec_from_file_location("configuration_deployed_inert_helpers", Path(__file__).with_name("verify-settings-deployed.py"))
        check(spec is not None and spec.loader is not None, "The inert deployed helpers are unavailable")
        previous = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(previous)
        tasks, devices, core, utilities, sessions = previous.helpers()
    core.STARTED = STARTED
    core.MUTABLE = {"sessions", "devices"}
    return previous, tasks, devices, core, utilities, sessions


def output_files():
    value = os.environ.get("GOBY_CONFIGURATION_VERIFICATION_RUN", "1")
    check(re.fullmatch(r"[1-9][0-9]?", value), "The verification run must be canonical decimal one through ninety-nine")
    stem = "m5h-deployed-configuration" + ("" if value == "1" else "-attempt-" + value)
    return int(value), ROOT / (stem + ".json"), ROOT / (stem + ".attempt.json"), ROOT / (stem + ".private.json")


def fingerprint(core, path):
    check(path in {ROOT / "runtime.env", ROOT / "browser.env", EVIDENCE} and path.resolve(strict=True) == path,
          "A private input escaped its fixed canonical pathname")
    parent = path.parent.lstat()
    check(stat.S_ISDIR(parent.st_mode) and parent.st_uid == 0 and parent.st_mode & 0o022 == 0, "A private input directory has unsafe ownership")
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
              "A private input changed while being observed")
    return core.file_identity(info), hashlib.sha256(data).hexdigest()


def snapshot(tasks, core, utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == core.TABLES and len(value) == 28 and sum(map(len, value.values())) <= 5000,
          "The complete main snapshot escaped the twenty-eight-table bound")
    migrations = tasks.records(value, "schema_migrations")
    check(len(migrations) == 21 and {row["version"] for row in migrations} == set(range(1, 22)), "The main schema is not exactly version twenty-one")
    media = [row for row in tasks.records(value, "items") if row["media"] is not None]
    check(len(value["libraries"]) == 5 and len(media) == 11 and all(row["media"].get("ProbeVersion") == 6 for row in media),
          "The accepted five-library and eleven-media catalog changed")
    check(len(value["managed_settings"]) == 1 and set(tasks.indexed(value, "managed_settings")) == {"1"}, "The managed singleton inventory changed")
    return value


def validate_dto(core, value):
    check(isinstance(value, dict) and set(value) == DTO_FIELDS and isinstance(value["Revision"], str) and
          NUMBER.fullmatch(value["Revision"]) and int(value["Revision"]) <= 9223372036854775807 and value["ServerNameMode"] in MODES,
          "The native configuration DTO has an invalid shape, revision or name mode")
    for section in ("Defaults", "Overrides", "Effective", "Sources"):
        check(isinstance(value[section], dict) and set(value[section]) == set(FIELDS), "A native settings section omitted its exact five fields")
    deployment, encoding, mode = value["Deployment"], value["Encoding"], value["ServerNameMode"]
    check(isinstance(deployment, dict) and set(deployment) == DEPLOYMENT_FIELDS and
          isinstance(deployment["HostName"], str) and 1 <= len(deployment["HostName"].encode("utf-8")) <= 128 and "\x00" not in deployment["HostName"] and
          type(deployment["TranscodingEnabled"]) is bool and
          all(isinstance(deployment[field], str) and len(deployment[field]) <= 128 for field in ("HardwareDecoder", "HardwareEncoder")) and
          all(type(deployment[field]) is int and 0 <= deployment[field] <= 1_000_000_000 for field in ("Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs")),
          "The deployment DTO escaped its exact safe hostname and startup whitelist")
    check(isinstance(encoding, dict) and set(encoding) == {"TranscodingMaxWidth"} and type(encoding["TranscodingMaxWidth"]) is int and
          0 <= encoding["TranscodingMaxWidth"] <= 8192, "The independent encoding DTO is not one bounded integer")
    for section in ("Defaults", "Overrides", "Effective"):
        for field, item in value[section].items():
            if section == "Overrides" and item is None:
                continue
            if field == "ServerName":
                minimum = 0 if section == "Overrides" and mode == "empty" else 1
                check(isinstance(item, str) and minimum <= len(item.encode("utf-8")) <= 128 and "\x00" not in item,
                      "A native name is not a bounded lossless string")
            else:
                check(type(item) is int and 1 <= item <= BOUNDS[field], "A native numeric setting is not a bounded JSON integer")
    name = value["Overrides"]["ServerName"]
    check((mode in {"deployment", "unset"} and name is None) or (mode == "empty" and name == "") or
          (mode == "custom" and isinstance(name, str) and len(name) > 0), "The raw name does not match its declared mode")
    expected_name = value["Defaults"]["ServerName"] if mode == "deployment" else name if mode == "custom" else deployment["HostName"]
    check(value["Effective"]["ServerName"] == expected_name and value["Sources"]["ServerName"] == ("deployment" if mode == "deployment" else "database"),
          "The name mode lost its effective hostname or deployment provenance")
    for field in BOUNDS:
        override = value["Overrides"][field]
        check(value["Effective"][field] == (value["Defaults"][field] if override is None else override) and
              value["Sources"][field] == ("deployment" if override is None else "database"), "A native output ceiling or source changed its nullable semantics")
    check(isinstance(value["UpdatedAt"], str) and value["UpdatedAt"].endswith("Z") and core.timestamp(value["UpdatedAt"]).utcoffset() == timedelta(0),
          "The native settings timestamp is not explicit UTC")


class API:
    def __init__(self, core, logins):
        self.core, self.logins = core, logins
        self.native, self.emby = logins
        self.scope = None
        self.counts = {"GET": 0, "POST": 0, "PUT": 0, "DELETE": 0}
        self.trace, self.trace_bytes, self.write_response_failures, self.logout_response_failures = [], 0, 0, 0
        self.last_status = None
        for login in logins:
            login.denied = login.barrier = False
            login.initial_device = None

    def request(self, method, path, login=None, *, body=None, expected=200, raw=False, action=None):
        anonymous = login is None
        check(method in self.counts and (anonymous and method == "GET" and path == PUBLIC or login in self.logins),
              "A configuration HTTP request escaped its fixed authentication scope")
        native = login is self.native
        signing_in = (method, path) in {("POST", NATIVE_SESSION), ("POST", EMBY_LOGIN)}
        logging_out = (method, path) in {("DELETE", NATIVE_SESSION), ("POST", EMBY_LOGOUT)}
        if signing_in:
            check(not anonymous and native == (path == NATIVE_SESSION) and not login.attempted and not login.token,
                  "An additional login or existing credential reuse was prevented")
        elif method == "GET":
            check(anonymous or native and path in {NATIVE_SESSION, SETTINGS} or not native and path in {CONFIGURATION, ENCODING},
                  "A read escaped the closed configuration endpoint list")
        elif logging_out:
            check(not anonymous and native == (path == NATIVE_SESSION) and login.initial is not None and login.token and
                  ID.fullmatch(login.id) and not login.logout_attempted and (not native or login.csrf), "An additional or unowned logout was prevented")
        else:
            check(not anonymous and login.initial is not None and login.token and self.scope is not None and
                  (native and method == "PUT" and path == SETTINGS and login.csrf or
                   not native and method == "POST" and path in {PARTIAL, ENCODING}), "A mutation escaped the exact owned configuration scope")
            check((action, method, path) in {("name", "POST", PARTIAL), ("encoding", "POST", ENCODING), ("restore", "PUT", SETTINGS)},
                  "A configuration action used another mutation endpoint")
            self.scope.authorize(action, body)
        check(body is None or signing_in or action is not None, "A read or logout included an unapproved body")
        self.core.remaining()
        check(sum(self.counts.values()) < (100 if self.core.CLEANING else 70), "The configuration HTTP request budget was exhausted")
        self.counts[method] += 1
        headers = {"Accept": "application/json"}
        if not anonymous:
            if native:
                headers["Origin"] = ORIGIN
                if login.token:
                    headers["Cookie"] = "goby_session=" + login.token
                if login.csrf:
                    headers["X-CSRF-Token"] = login.csrf
            else:
                headers.update({"X-Emby-Client": login.app, "X-Emby-Device-Id": login.reported,
                                "X-Emby-Device-Name": login.name, "X-Emby-Client-Version": login.version})
                if login.token:
                    headers["X-Emby-Token"] = login.token
        payload = None if body is None else json.dumps(body, ensure_ascii=True, separators=(",", ":")).encode("utf-8")
        check(payload is None or len(payload) <= 16384, "A configuration request exceeded its restricted body bound")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if signing_in:
            login.attempted = True
        if logging_out:
            login.logout_attempted = True
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=self.core.remaining())
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            self.last_status = response.status
            cookies = SimpleCookie()
            cookies.load(response.getheader("Set-Cookie", ""))
            if signing_in and native and "goby_session" in cookies and TOKEN.fullmatch(cookies["goby_session"].value):
                login.token = cookies["goby_session"].value
            data = response.read(MAX_BODY + 1)
            check(len(data) <= MAX_BODY, "A configuration response exceeded its byte bound")
            if path not in {NATIVE_SESSION, EMBY_LOGIN, EMBY_LOGOUT}:
                self.trace_bytes += len(data) + len(payload or b"") + len(path)
                check(self.trace_bytes <= MAX_TRACE, "The bounded private configuration trace was exhausted")
                self.trace.append({"method": method, "path": path, "request_body": body, "status": response.status,
                                   "response_body_base64": base64.b64encode(data).decode("ascii")})
            value = data if raw else json.loads(data) if data else None
            if signing_in and not native and isinstance(value, dict) and isinstance(value.get("AccessToken"), str) and TOKEN.fullmatch(value["AccessToken"]):
                login.token = value["AccessToken"]
            if path == NATIVE_SESSION and isinstance(value, dict) and isinstance(value.get("CSRFToken"), str) and HASH.fullmatch(value["CSRFToken"]) and login.token:
                check(value["CSRFToken"] == hashlib.sha256(("goby:admin:csrf:" + login.token).encode()).hexdigest(), "The CSRF token does not belong to the new native cookie")
                login.csrf = value["CSRFToken"]
            check(response.status in ((expected,) if isinstance(expected, int) else expected), "A configuration response returned an unexpected status")
            check(not (response.status == 204 or logging_out) or not data, "A successful write or logout returned an unexpected body")
            return value
        except BaseException:
            if action is not None:
                self.write_response_failures += 1
            raise
        finally:
            connection.close()


def bind_login(tasks, core, api, before, current, login):
    added = tasks.delta(before, current, "sessions")
    digest = "\\x" + hashlib.sha256(login.token.encode("ascii")).hexdigest()
    matches = [row for row in added if row["token_hash"] == digest]
    check(len(matches) == 1, "A new credential cannot be bound to one exclusive added session")
    row = matches[0]
    check(ID.fullmatch(row["id"]) and row["kind"] == login.kind and row["user_id"] == login.user_id and row["revoked_at"] is None,
          "The acknowledged new credential has an unexpected identity or state")
    login.id, login.initial = row["id"], row
    check({entry["id"] for entry in added} == {known.id for known in api.logins if known.id} and row["created_at"] == row["last_seen_at"] and
          core.timestamp(row["expires_at"]) - core.timestamp(row["created_at"]) == timedelta(days=1 if login.kind == "admin" else 30),
          "New authentication history escaped its two owned logins or fixed lifetime")
    if login.kind == "admin":
        check(row["device_registry_id"] is None and row["client_name"] == "Goby Dashboard" and row["device_id"] == "goby-dashboard" and
              row["device_name"] == "Web browser", "The native login registered a device or changed its expected client identity")
        return
    check(row["device_id"] == login.reported and row["device_name"] == login.name and row["client_name"] == login.app and
          row["client_version"] == login.version and type(row["device_registry_id"]) is int and row["device_registry_id"] > 1,
          "The ordinary credential lost its unique reported client identity")
    created = tasks.delta(before, current, "devices")
    matches = [entry for entry in created if entry["id"] == row["device_registry_id"] and entry["reported_device_id"] == login.reported]
    check(len(matches) == 1, "The ordinary login did not create its own unique device generation")
    login.device, login.initial_device = str(matches[0]["id"]), matches[0]
    device = login.initial_device
    check(len(created) == 1 and device["reported_name"] == login.name and device["app_name"] == login.app and device["app_version"] == login.version and
          device["last_user_id"] == login.user_id and device["ip_address"] == "127.0.0.1" and device["revision"] == 1 and
          device["custom_name"] is None and device["deleted_at"] is None, "The new device has unowned options, metadata, or transport identity")


def activity(core, original, current):
    first, last = core.timestamp(original), core.timestamp(current)
    # GREATEST guarantees monotonic stored activity, but clock_timestamp can
    # move backwards between predicate evaluation, assignment and observation.
    # Neither a minimum elapsed wall time nor a later clock is an auth barrier.
    check(last >= first, "Owned activity moved backwards instead of retaining the real Touch monotonicity")
    return int(last != first)


def session_row(tasks, core, login, row, revoked):
    check(login.initial is not None and isinstance(row, dict), "An owned credential has no complete bound row")
    wanted = dict(login.initial)
    touches = 0
    if login.kind == "emby":
        touches = activity(core, login.initial["last_seen_at"], row["last_seen_at"])
        wanted["last_seen_at"] = row["last_seen_at"]
    if revoked:
        check(row.get("revoked_at") is not None, "The owned credential revocation is not committed")
        core.timestamp(row["revoked_at"])
        wanted["revoked_at"] = row["revoked_at"]
    check(tasks.identical(row, wanted), "An owned session changed beyond its exact logout and applicable Touch fields")
    return touches


def logout(tasks, core, database, api, login):
    check(login.token and login.initial is not None, "The owned logout lacks an acknowledged new credential")
    if not login.logout_attempted:
        try:
            api.request("DELETE" if login.kind == "admin" else "POST", NATIVE_SESSION if login.kind == "admin" else EMBY_LOGOUT,
                        login, expected=204 if login.kind == "admin" else 200, raw=True)
        except Exception:
            api.logout_response_failures += 1
    api.request("GET", SETTINGS if login.kind == "admin" else CONFIGURATION, login, expected=401, raw=True)
    login.denied = True
    check(ID.fullmatch(login.id), "The revocation observation lacks a canonical owned identity")
    row = database.read("SELECT to_jsonb(s) FROM sessions s WHERE id='" + login.id + "';", "Confirm the exact owned credential revocation barrier")
    session_row(tasks, core, login, row, True)
    login.barrier = True


class ConfigurationScope:
    def __init__(self, tasks, core, database, api, original, initial):
        self.tasks, self.core, self.database, self.api = tasks, core, database, api
        self.original, self.initial = original, initial
        self.revision = original["revision"]
        check(type(self.revision) is int and 1 <= self.revision <= 9223372036854775804, "The settings revision has insufficient safe mutation headroom")
        self.name = "M5h configuration " + secrets.token_hex(16)
        check(self.name not in {initial["Effective"]["ServerName"], initial["Overrides"]["ServerName"]}, "The unique temporary name collided with existing settings")
        old_width = initial["Encoding"]["TranscodingMaxWidth"]
        ceiling = min(initial["Effective"]["MaxWidth"], old_width) if old_width > 0 else initial["Effective"]["MaxWidth"]
        self.width = min(640, ceiling)
        if self.width == old_width and self.width > 1:
            self.width = max(1, self.width // 2)
        # At a one-pixel existing extra ceiling, a different positive value
        # would relax that limit. Preserve it and verify the real no-op instead.
        self.width_increment = int(self.width != old_width)
        self.name_attempts = self.encoding_attempts = self.restore_attempts = 0
        self.grant = self.restore_revision = self.restore_target = None
        self.saved = {}
        self.restored_row = self.final_row = None
        self.no_commit = self.restored = self.final_proven = False

    def observe(self):
        for _ in range(3):
            shown = self.api.request("GET", SETTINGS, self.api.native)
            validate_dto(self.core, shown)
            check(shown["Defaults"] == self.initial["Defaults"] and shown["Deployment"] == self.initial["Deployment"],
                  "Frozen native defaults or deployment values changed")
            observed = self.database.read("SELECT json_build_object('row',to_jsonb(s),'observed_at',clock_timestamp()) FROM managed_settings s WHERE id=1;",
                                          "Observe native publication against its complete stored configuration")
            check(isinstance(observed, dict) and isinstance(observed.get("row"), dict), "The configuration singleton observation is absent")
            row = observed["row"]
            check(set(row) == ROW_FIELDS and row["id"] == 1 and row["created_at"] == self.original["created_at"],
                  "The configuration singleton identity or immutable columns changed")
            if (shown["Revision"] == str(row["revision"]) and shown["ServerNameMode"] == row["server_name_mode"] and
                    shown["Encoding"]["TranscodingMaxWidth"] == row["compatibility_max_width"] and
                    self.core.timestamp(shown["UpdatedAt"]) == self.core.timestamp(row["updated_at"]) and
                    all(self.tasks.identical(shown["Overrides"][field], row[column]) for field, column in FIELDS.items())):
                return shown, row
            # A GET/SQL race grants no write. Repeat only the bounded reads.
            time.sleep(0.05)
        raise Failure("Published and stored configuration could not be observed consistently")

    def expected(self, phase, updated_at):
        result = dict(self.original)
        result["updated_at"] = updated_at
        if phase in {"name", "encoding"}:
            result.update({"revision": self.revision + 1, "server_name": self.name, "server_name_mode": "custom"})
        if phase == "encoding":
            result.update({"revision": self.revision + 1 + self.width_increment, "compatibility_max_width": self.width})
        if phase == "restored":
            check(self.restore_revision is not None, "A restored configuration lacks its fixed owned CAS revision")
            result["revision"] = self.restore_revision + 1
        return result

    def classify(self, row):
        check(set(row) == ROW_FIELDS and row["id"] == 1 and row["created_at"] == self.original["created_at"], "The configuration row escaped its original identity")
        self.core.timestamp(row["updated_at"])
        if self.tasks.identical(row, self.original) and not self.saved and self.restore_attempts == 0:
            return "original"
        if self.restore_attempts and self.tasks.identical(row, self.expected("restored", row["updated_at"])):
            check(self.restored_row is None or self.tasks.identical(row, self.restored_row), "The acknowledged restored configuration changed")
            self.restored, self.restored_row = True, row
            return "restored"
        for phase, attempted in (("encoding", self.encoding_attempts), ("name", self.name_attempts)):
            if attempted and self.tasks.identical(row, self.expected(phase, row["updated_at"])):
                check(not (phase == "name" and self.width_increment and "encoding" in self.saved), "A confirmed encoding revision reverted to an older prefix")
                check(self.restore_revision is None or self.tasks.identical(row, self.restore_target), "A different temporary prefix appeared after the restore revision was fixed")
                check(phase not in self.saved or self.tasks.identical(row, self.saved[phase]), "A previously acknowledged owned configuration row changed")
                self.saved[phase] = row
                return phase
        raise Failure("An unowned configuration revision or complete value set prevents restoration")

    def authorize(self, action, body):
        check(action in {"name", "encoding", "restore"} and self.grant == action, "A configuration write lacks a fresh single-use ownership proof")
        self.grant = None
        if action == "name":
            check(self.name_attempts == 0 and not self.api.emby.logout_attempted and body == {"ServerName": self.name}, "An additional or unowned Partial write was prevented")
            self.name_attempts += 1
        elif action == "encoding":
            check(self.name_attempts == 1 and self.encoding_attempts == 0 and "name" in self.saved and not self.api.emby.logout_attempted and
                  body == {"TranscodingMaxWidth": self.width}, "An additional or unowned encoding write was prevented")
            self.encoding_attempts += 1
        else:
            check(self.api.emby.barrier and self.restore_revision is not None and self.restore_target is not None and not self.restored and
                  self.restore_attempts < 2 and body == self.restore_body(), "A restoration escaped its drained issuer, fixed revision, or original complete state")
            self.restore_attempts += 1

    def restore_body(self):
        return {"Revision": str(self.restore_revision), "Overrides": self.initial["Overrides"], "ServerNameMode": self.initial["ServerNameMode"],
                "Encoding": self.initial["Encoding"]}

    def change_name(self):
        _shown, row = self.observe()
        check(self.tasks.identical(row, self.original), "The original configuration changed before the owned Partial write")
        self.grant = "name"
        self.api.request("POST", PARTIAL, self.api.emby, body={"ServerName": self.name}, expected=204, action="name")
        shown, row = self.observe()
        check(self.classify(row) == "name" and shown["ServerNameMode"] == "custom" and shown["Effective"]["ServerName"] == self.name,
              "The Partial name was not committed as the expected native revision")
        return shown

    def change_encoding(self):
        _shown, row = self.observe()
        check(self.tasks.identical(row, self.saved["name"]), "Configuration changed before the sole named encoding write")
        self.grant = "encoding"
        self.api.request("POST", ENCODING, self.api.emby, body={"TranscodingMaxWidth": self.width}, expected=204, action="encoding")
        shown, row = self.observe()
        check(self.classify(row) == "encoding" and (self.width_increment or self.tasks.identical(row, self.saved["name"])),
              "Named encoding did not preserve native overrides or its correct revision/no-op behavior")
        return shown

    def restore(self):
        if not self.name_attempts:
            return
        check(self.api.emby.barrier, "The non-CAS Emby issuer must be proven revoked before any native restoration")
        for _ in range(5):
            shown, row = self.observe()
            phase = self.classify(row)
            if phase == "original":
                # This conclusion is safe only after the other issuer's
                # committed revocation has excluded every late non-CAS write.
                self.no_commit = True
                return
            if phase == "restored":
                check(all(shown[field] == self.initial[field] for field in ("Defaults", "Overrides", "Effective", "Sources", "Deployment", "ServerNameMode", "Encoding")),
                      "Native restoration lost an original name mode, nullable override, or independent encoding value")
                return
            if self.restore_revision is None:
                self.restore_revision, self.restore_target = row["revision"], row
            check(self.tasks.identical(row, self.restore_target), "The fixed owned restore target changed; no revision was adopted")
            if self.restore_attempts < 2:
                self.grant = "restore"
                try:
                    receipt = self.api.request("PUT", SETTINGS, self.api.native, body=self.restore_body(), expected=(200, 409), action="restore")
                except Exception:
                    continue
                if self.api.last_status == 200:
                    validate_dto(self.core, receipt)
                    check(receipt["Revision"] == str(self.restore_revision + 1) and
                          all(receipt[field] == self.initial[field] for field in ("Defaults", "Overrides", "Effective", "Sources", "Deployment", "ServerNameMode", "Encoding")),
                          "The native restoration receipt does not describe the exact original complete settings")
                else:
                    self.api.write_response_failures += 1
                    check(isinstance(receipt, dict) and receipt.get("Error", {}).get("Code") == "revision_conflict", "The native restore conflict returned another error")
            time.sleep(0.05)
        raise Failure("The bounded fixed-revision restoration could not be proven")

    def after_native_revocation(self):
        check(self.api.native.barrier and (not self.name_attempts or self.api.emby.barrier), "Both writing credentials lack committed revocation barriers")
        row = self.database.read("SELECT to_jsonb(s) FROM managed_settings s WHERE id=1;", "Observe final configuration after both revoked issuer barriers")
        phase = self.classify(row)
        check(phase == "restored" or phase == "original" and self.restore_attempts == 0,
              "Configuration remains dirty or unproven after logout; no new credential or unknown write was used")
        self.no_commit = phase == "original"
        self.final_proven, self.final_row = True, row


def projections(api, shown, namespace, version=None, compatibility=True):
    if compatibility:
        full = api.request("GET", CONFIGURATION, api.emby)
        expected = {"IsStartupWizardCompleted": True}
        if shown["ServerNameMode"] != "unset":
            expected["ServerName"] = shown["Defaults"]["ServerName"] if shown["ServerNameMode"] == "deployment" else shown["Overrides"]["ServerName"]
        encoding = api.request("GET", ENCODING, api.emby)
        check(api.scope.tasks.identical(full, expected) and api.scope.tasks.identical(encoding, shown["Encoding"]),
              "The Emby projections lost exact value types, configured-name omission, or independent encoding semantics")
    public = api.request("GET", PUBLIC)
    check(isinstance(public, dict) and public.get("Id") == namespace and public.get("ServerName") == shown["Effective"]["ServerName"] and
          public.get("ProductName") == "Goby" and public.get("LocalAddress") == ORIGIN and public.get("StartupWizardCompleted") is True and
          isinstance(public.get("Version"), str) and (version is None or public["Version"] == version),
          "Anonymous PublicInfo did not reflect the native effective name and accepted service namespace")
    return public["Version"]


def audit(tasks, core, baseline, final, api, scope):
    for table in core.TABLES - {"sessions", "devices", "managed_settings"}:
        check(baseline[table] == final[table], "A protected old business table changed during configuration acceptance")
    sessions_added, devices_added = tasks.delta(baseline, final, "sessions"), tasks.delta(baseline, final, "devices")
    logins = api.logins if api else []
    check({row["id"] for row in sessions_added} == {login.id for login in logins if login.id} and
          len(sessions_added) == sum(login.attempted for login in logins), "New session history escaped the acknowledged fresh credentials")
    session_touches = device_touches = 0
    current_sessions = {row["id"]: row for row in sessions_added}
    for login in logins:
        if login.attempted:
            check(login.denied and login.barrier, "An issued credential lacks independent denial and committed revocation proof")
            session_touches += session_row(tasks, core, login, current_sessions[login.id], True)
    ordinary = api.emby if api and api.emby.initial_device is not None else None
    check({str(row["id"]) for row in devices_added} == ({ordinary.device} if ordinary else set()), "Device history escaped the one new ordinary login")
    if ordinary:
        row, wanted = devices_added[0], dict(ordinary.initial_device)
        device_touches = activity(core, wanted["last_seen_at"], row["last_seen_at"])
        wanted["last_seen_at"] = row["last_seen_at"]
        check(tasks.identical(row, wanted), "The owned device changed beyond its real activity timestamp")
    if scope is None:
        check(baseline["managed_settings"] == final["managed_settings"], "Configuration changed without an authorized mutation scope")
    else:
        check(scope.final_proven and tasks.identical(tasks.indexed(final, "managed_settings")["1"], scope.final_row),
              "The final complete configuration lacks both issuer barriers and exact original-state restoration")
    return len(sessions_added), len(devices_added), session_touches, device_touches


def main():
    report = {"owner": OWNER, "status": "failed", "cleanup": {"proven": False, "errors": []}, "snapshots": {},
              "limits": {"http_requests": 100, "response_bytes": MAX_BODY, "database_queries": 80, "workflow_seconds": 180,
                         "partial_name_posts": 1, "encoding_posts": 1, "native_restore_puts": 2}, "http_transport_retries": 0,
              "allowed_old_row_changes": {"managed_settings": "Only revision and updated_at remain advanced after complete original state is restored"},
              "owned_activity_scope": "Only new ordinary session/device last_seen_at through real Touch; native session changes only revoked_at",
              "limitations": ["No external configuration writer may run during this acceptance; the Emby interface has no CAS and read checks cannot eliminate that race",
                              "No planning, playback, conversion, scan, task execution, device management, key creation, or service restart",
                              "An encoding value already at the minimum safe ceiling may be verified as a no-op instead of relaxing that ceiling"]}
    previous = tasks = devices = core = utilities = sessions = database = baseline = api = scope = deployment = vault = media = nfos = configured = parent_identity = None
    credentials, inputs = {}, {}
    stage = "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without arguments")
        signal.signal(signal.SIGALRM, deadline)
        signal.setitimer(signal.ITIMER_REAL, max(0.1, 120 - (time.monotonic() - STARTED)))
        accepted, expected_pid = os.environ.get("GOBY_CONFIGURATION_EXPECTED_BINARY_SHA256", ""), os.environ.get("GOBY_CONFIGURATION_EXPECTED_PID", "")
        evidence_hash = os.environ.get("GOBY_CONFIGURATION_EXPECTED_SOURCE_SHA256", "")
        check(HASH.fullmatch(accepted) and NUMBER.fullmatch(expected_pid) and 1 < int(expected_pid) <= 2147483647 and HASH.fullmatch(evidence_hash) and
              os.environ.get("GOBY_CONFIGURATION_SOURCE_EVIDENCE_FILE") == str(EVIDENCE), "Accepted main PID, binary and fixed M5h source evidence constraints are required")
        number, result_path, attempt_path, private_path = output_files()
        report["verification_run"] = number
        report["output_scope"] = {"directory": str(ROOT), "report": result_path.name, "attempt_marker": attempt_path.name, "private_failure_log": private_path.name}
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed output directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (result_path, attempt_path, private_path)), "The selected attempt already exists; no overwrite is allowed")
        previous, tasks, devices, core, utilities, sessions = helpers()
        utilities.private_write(attempt_path, {"owner": OWNER, "verification_run": number, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                                               "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
        parent_identity = info.st_dev, info.st_ino
        for path in (EVIDENCE, ROOT / "runtime.env", ROOT / "browser.env"):
            inputs[path] = fingerprint(core, path)
        check(inputs[EVIDENCE][1] == evidence_hash, "The M5h source evidence differs from its explicitly accepted digest")
        deployment = sessions.deployment(accepted)
        check(deployment["main_pid"] == int(expected_pid), "The main service PID differs from the accepted deployment")
        runtime = sessions.private_values(ROOT / "runtime.env", {"GOBY_LISTEN", "GOBY_PUBLIC_URL", "GOBY_MEDIA_ROOTS"})
        check(runtime["GOBY_LISTEN"] == "127.0.0.1:18096" and runtime["GOBY_PUBLIC_URL"] == ORIGIN, "The main runtime escaped the fixed loopback endpoint")
        configured = runtime["GOBY_MEDIA_ROOTS"]
        database = previous.database_type(core, sessions)()
        baseline = snapshot(tasks, core, utilities, database)
        report["snapshots"]["before"] = utilities.snapshot_report(baseline)
        previous.idle(tasks, core, database, baseline)
        namespace = [row["value"] for row in tasks.records(baseline, "server_settings") if row["key"] == "server_id"]
        check(len(namespace) == 1 and ID.fullmatch(namespace[0]), "The persistent main server namespace is invalid")
        report["deployment"] = {**deployment, "schema_version": 21, "database_role": "goby_test", "database_port": 5432,
                                "postgresql_forced_read_only": True, "source_evidence_sha256": evidence_hash,
                                "server_namespace_sha256": hashlib.sha256(namespace[0].encode()).hexdigest()}
        vault = core.vault_identity(sessions)
        media, nfos = devices.source_hashes(core, baseline, configured), tasks.nfo_hashes(core, baseline, configured)
        report["source_hashes_before"] = {"media_count": len(media), "media_sha256": utilities.aggregate(media), "nfo_count": len(nfos), "nfo_sha256": utilities.aggregate(nfos)}
        credentials = sessions.private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        users = [row for row in tasks.records(baseline, "users") if row["name"] == credentials["GOBY_SMOKE_NAME"] and row["is_administrator"] and not row["is_disabled"]]
        check(len(users) == 1, "Private credentials do not identify exactly one enabled existing administrator")
        nonce = secrets.token_hex(16)
        reported = "m5h-configuration-" + nonce
        check(not any(row["reported_device_id"] == reported for row in tasks.records(baseline, "devices")) and
              not any(row["device_id"] == reported for row in tasks.records(baseline, "sessions")), "The unique reported client identity already has history")
        native = devices.Login("admin", users[0]["id"])
        emby = devices.Login("emby", users[0]["id"], reported=reported, name="M5h configuration " + nonce,
                             app="Goby M5h configuration " + nonce, version="m5h-deployed")
        api = API(core, [native, emby])
        stage = "two fresh independent administrator credentials"
        for login in api.logins:
            is_native = login.kind == "admin"
            signed = api.request("POST", NATIVE_SESSION if is_native else EMBY_LOGIN, login,
                                 body={"Name" if is_native else "Username": credentials["GOBY_SMOKE_NAME"],
                                       "Password" if is_native else "Pw": credentials["GOBY_SMOKE_PASSWORD"]})
            check(login.token and signed.get("User", {}).get("Id") == login.user_id and (not is_native or login.csrf), "A fresh login omitted its credential or selected another account")
            current = snapshot(tasks, core, utilities, database)
            bind_login(tasks, core, api, baseline, current, login)
            core.preserve(baseline, current)
            if not is_native:
                check(signed.get("ServerId") == namespace[0] and signed.get("SessionInfo", {}).get("Id") == login.id,
                      "The ordinary wire login differs from its real session or persistent server identity")
        credentials.clear()
        check(native.token != emby.token and native.id != emby.id, "The two credential kinds lost their independent identity")
        initial = api.request("GET", SETTINGS, native)
        validate_dto(core, initial)
        scope = ConfigurationScope(tasks, core, database, api, tasks.indexed(baseline, "managed_settings")["1"], initial)
        api.scope = scope
        shown, row = scope.observe()
        check(shown == initial and tasks.identical(row, scope.original), "The initial native publication differs from the complete stored baseline")
        version = projections(api, initial, namespace[0])
        check(native.initial["client_version"] == version, "The native credential belongs to another service version")
        stage = "one Partial name and one bounded independent encoding write"
        current = snapshot(tasks, core, utilities, database)
        core.preserve(baseline, current)
        previous.idle(tasks, core, database, current)
        named = scope.change_name()
        projections(api, named, namespace[0], version)
        encoded = scope.change_encoding()
        projections(api, encoded, namespace[0], version)
        check(encoded["Overrides"] == named["Overrides"] and encoded["Effective"] == named["Effective"] and encoded["Sources"] == named["Sources"],
              "Independent encoding changed native name or output-ceiling settings")
        stage = "drain the non-CAS issuer before native restoration"
        logout(tasks, core, database, api, emby)
        stage = "exact native CAS restoration of all original settings"
        scope.restore()
        check(scope.restored and api.write_response_failures == 0 and api.logout_response_failures == 0,
              "A compatibility write, issuer logout, or native restore did not complete its uninterrupted contract")
        restored, row = scope.observe()
        check(tasks.identical(row, scope.restored_row), "The acknowledged restored singleton changed")
        projections(api, restored, namespace[0], version, compatibility=False)
        report["checks"] = {"issued_credentials": 2, "new_ordinary_devices": 1, "partial_name_posts": 1, "encoding_posts": 1,
                            "compatibility_revision_increments": 1 + scope.width_increment, "encoding_noop_at_minimum": not bool(scope.width_increment),
                            "temporary_encoding_does_not_relax_original_combined_ceiling": True, "native_numeric_overrides_unchanged": True,
                            "native_and_emby_configured_name_semantics_match": True, "anonymous_public_name_follows_commits": True,
                            "native_restore_includes_original_name_mode_and_encoding": True, "media_or_planning_requests": 0}
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
        if api is not None:
            for login in api.logins:
                if not login.attempted:
                    continue
                if not login.token:
                    report["cleanup"]["errors"].append("A fresh login response was lost; no existing token or unknown session was used")
                    continue
                if login is api.native and not login.csrf:
                    try:
                        api.request("GET", NATIVE_SESSION, login)
                        check(login.csrf, "The owned native cleanup CSRF token is absent")
                    except BaseException:
                        report["cleanup"]["errors"].append("The newly owned native CSRF token could not be recovered")
                if login.initial is None:
                    try:
                        bind_login(tasks, core, api, baseline, snapshot(tasks, core, utilities, database), login)
                    except BaseException:
                        report["cleanup"]["errors"].append("A fresh credential could not be bound to its exact exclusive new history")
            if api.emby.attempted:
                try:
                    logout(tasks, core, database, api, api.emby)
                except BaseException:
                    report["cleanup"]["errors"].append("The ordinary issuer could not be proven drained; no unsafe native restore was authorized")
            if scope is not None:
                try:
                    scope.restore()
                except BaseException:
                    # A later native revocation and fresh database observation
                    # may still prove a delayed CAS commit. Never add authority.
                    pass
            if api.native.attempted:
                try:
                    logout(tasks, core, database, api, api.native)
                except BaseException:
                    report["cleanup"]["errors"].append("The native logout and exact committed revocation barrier could not be confirmed")
            if scope is not None:
                try:
                    scope.after_native_revocation()
                except BaseException:
                    report["cleanup"]["errors"].append("Final original configuration remains dirty or unproven after logout; unknown state was not overwritten")
            report["http_method_counts"] = api.counts
            report["write_response_failures"] = api.write_response_failures
            report["logout_response_failures"] = api.logout_response_failures
            report["logout_protected_statuses"] = {login.kind: 401 for login in api.logins if login.denied}
            if api.write_response_failures or api.logout_response_failures:
                report["status"] = "failed"
        if scope is not None:
            report["settings_restoration"] = {"partial_attempts": scope.name_attempts, "encoding_attempts": scope.encoding_attempts,
                                              "native_restore_attempts": scope.restore_attempts, "fixed_restore_revision": True,
                                              "exact_original_state_proven": False, "original_name_mode": scope.initial["ServerNameMode"]}
        if baseline is not None:
            try:
                final = snapshot(tasks, core, utilities, database)
                report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                session_count, device_count, session_touches, device_touches = audit(tasks, core, baseline, final, api, scope)
                if report["status"] == "passed":
                    check(session_count == 2 and device_count == 1 and scope is not None and scope.restored, "The successful run lacks its exact two revoked credentials, retained device, or restored settings")
                report["cleanup"].update({"proven": True, "new_credentials_revoked": session_count, "retained_ordinary_devices": device_count, "physical_history_deletions": 0})
                report["owned_history"] = {"new_sessions": session_count, "new_devices": device_count,
                                           "ordinary_session_rows_touched": session_touches, "ordinary_device_rows_touched": device_touches}
                report["old_other_twenty_seven_table_rows_exact"] = True
                report["old_users_credentials_keys_devices_metadata_jobs_and_task_history_exact"] = True
                if scope is not None:
                    report["settings_restoration"].update({"exact_original_state_proven": scope.final_proven,
                                                           "not_applied_proven_after_issuer_revocation": scope.no_commit,
                                                           "only_revision_and_updated_at_remain_advanced": scope.restored})
            except BaseException:
                report["cleanup"]["errors"].append("Complete old-row preservation, exclusive new history, or exact original settings could not be proven")
        for label, initial_value, observe in (("deployment", deployment, lambda: sessions.deployment(accepted)),
                                               ("vault", vault, lambda: core.vault_identity(sessions)),
                                               ("media", media, lambda: devices.source_hashes(core, baseline, configured)),
                                               ("nfo", nfos, lambda: tasks.nfo_hashes(core, baseline, configured))):
            if initial_value is not None:
                try:
                    core.remaining()
                    check(observe() == initial_value, "A protected main process, vault, media, or NFO identity changed")
                    report[label + "_unchanged"] = True
                except BaseException:
                    report["cleanup"]["errors"].append("A protected process, master, media, or NFO identity could not be proven unchanged")
        if inputs:
            try:
                check(all(fingerprint(core, path) == original for path, original in inputs.items()), "A private runtime, credential, or source evidence input changed")
                report["private_runtime_credentials_and_source_evidence_unchanged"] = True
            except BaseException:
                report["cleanup"]["errors"].append("The private startup, credential, and accepted source inputs could not be proven unchanged")
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
                    tasks.private_failure_log(private_path, {"owner": OWNER, "failed_stage": report.get("failed_stage", "cleanup"), "configuration_http": api.trace if api else []})
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
