#!/usr/bin/env python3
"""Verify M5e ordinary devices once through authorized root SSH on Linux.

Required: GOBY_DEVICES_EXPECTED_BINARY_SHA256 identifies the accepted binary.
Optional: GOBY_DEVICES_VERIFICATION_RUN is canonical decimal 1..99, default 1.
Reports use /opt/goby-test/m5e-deployed-devices.json and its .attempt.json
marker; later manual runs append -attempt-N to their shared filename stem.
Existing selected files are never overwritten; requests are never retried.

Read existing administrator credentials from browser.env and the existing
direct-playback viewer's protected marker/state. Issue one native cookie and
four ordinary logins on two fresh reported IDs. Only acknowledged new ordinary
generations may be renamed or soft-deleted. Keep their real history and revoke
all new credentials. Never mutate users, old devices, options, media, or policy.
Do not create a key: doing so would update the preexisting shared server-device
row. That namespace and its credentials must remain byte-for-byte preserved.

The M5d verifier provides inert database, source, vault, preservation and time
utilities. No older verifier main, fixture initializer, or playback API runs.
All complete rows and secrets remain in memory; only sanitized aggregates are
written. The workflow uses at most 200 requests, 2 MiB HTTP bodies, and 180 s.
"""

from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timezone
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
from urllib.parse import urlencode, urlsplit


sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test")
VIEWER = Path("/opt/goby-fixtures/direct-playback")
OWNER = "goby-devices-deployed-m5e-v1"
ORIGIN = "http://127.0.0.1:18096"
FIELDS = set("Id Revision ReportedDeviceId Name ReportedName CustomName AppName AppVersion LastUserId LastUserName CreatedAt LastSeenAt IpAddress ActiveLoginCount".split())
ID, NUMBER, TOKEN, HASH = (re.compile(value) for value in
                         (r"[0-9a-f]{32}", r"[1-9][0-9]*", r"[A-Za-z0-9_-]{43}", r"[0-9a-f]{64}"))
STARTED = time.monotonic()


class Failure(Exception):
    """Contain only a fixed, non-secret assertion label."""


def check(condition, message):
    if not condition:
        raise Failure(message)


def deadline(_number, _frame):
    raise Failure("The bounded deployed workflow deadline was reached")


def helpers():
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        spec = importlib.util.spec_from_file_location("devices_deployed_core", Path(__file__).with_name("verify-application-keys-deployed.py"))
        check(spec is not None and spec.loader is not None, "The inert M5d utilities are unavailable")
        core = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(core)
        core.STARTED = STARTED
        core.TABLES = core.TABLES | {"devices", "application_key_devices"}
        core.MUTABLE = {"sessions", "devices"}
        utilities, sessions = core.helpers()
    return core, utilities, sessions


def output_files():
    value = os.environ.get("GOBY_DEVICES_VERIFICATION_RUN", "1")
    check(re.fullmatch(r"[1-9][0-9]?", value), "The verification run must be canonical decimal one through ninety-nine")
    stem = "m5e-deployed-devices" + ("" if value == "1" else "-attempt-" + value)
    return int(value), ROOT / (stem + ".json"), ROOT / (stem + ".attempt.json")


def private_json(path, core):
    check(path.parent == VIEWER and path.parent.resolve(strict=True) == VIEWER, "A viewer credential path escaped its fixed directory")
    for directory in (VIEWER.parent, VIEWER):
        info = directory.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and directory.resolve(strict=True) == directory,
              "An existing viewer credential directory has unsafe ownership")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME)
    with os.fdopen(descriptor, "rb") as source:
        info = os.fstat(source.fileno())
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
              info.st_nlink == 1 and info.st_size <= 16384, "An existing viewer credential file is not private and bounded")
        raw = source.read(16385)
        check(len(raw) <= 16384 and core.file_identity(info) == core.file_identity(os.fstat(source.fileno())) == core.file_identity(path.lstat()),
              "An existing viewer credential file changed while being observed")
    value = json.loads(raw)
    check(isinstance(value, dict), "An existing viewer credential document is not an object")
    return value, (core.file_identity(info), hashlib.sha256(raw).digest())


def snapshot(core, utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == core.TABLES and len(value) == 21 and sum(map(len, value.values())) <= 5000,
          "The deployed schema escaped the complete twenty-one-table bound")
    migrations = core.rows(value, "schema_migrations")
    check(len(migrations) == 18 and {row["version"] for row in migrations} == set(range(1, 19)),
          "The deployed schema is not exactly version eighteen")
    media = [row["media"] for row in core.rows(value, "items") if row["media"] is not None]
    check(len(value["items"]) == 21 and len(media) == 11 and len(value["libraries"]) == 5 and
          all(item.get("ProbeVersion") == 6 for item in media), "The existing catalog differs from the accepted media population")
    return value


def source_hashes(core, value, configured):
    media = [row for row in core.rows(value, "items") if row["media"] is not None]
    check(sum(row["file_size"] for row in media) <= 256 * 1024 * 1024, "The existing source population exceeded the total hash bound")
    result = {}
    for item in media:
        identity, digest, _first = core.source_identity(item, value, configured)
        result[item["id"]] = {"identity": identity, "sha256": digest.hex()}
    return result


class Login:
    def __init__(self, kind, user_id, reported="", name="", app="", version="m5e-deployed"):
        self.kind, self.user_id, self.reported, self.name, self.app, self.version = kind, user_id, reported, name, app, version
        self.token = self.csrf = self.id = self.device = ""
        self.attempted = self.logout_attempted = False
        self.initial = None


class API:
    def __init__(self, core, logins):
        self.core, self.logins = core, logins
        self.devices, self.deleted = {}, {}
        self.counts = {"GET": 0, "POST": 0, "DELETE": 0}

    def request(self, method, path, login, *, body=None, expected=200, raw=False):
        route = urlsplit(path)
        check(path.startswith("/") and not path.startswith("//") and not route.scheme and not route.netloc and
              not route.fragment and "\r" not in path and "\n" not in path and login in self.logins,
              "A request escaped the acknowledged local authentication scope")
        native = login.kind == "admin"
        authenticating = method == "POST" and route.path in {"/admin/v1/session", "/emby/Users/AuthenticateByName"}
        if authenticating:
            check(not route.query and not login.attempted and not login.token and
                  native == (route.path == "/admin/v1/session"), "An additional or mismatched login was prevented")
            login.attempted = True
        elif method == "GET":
            check(route.path in {"/admin/v1/session", "/admin/v1/devices", "/admin/v1/capabilities"} or
                  route.path == "/emby/Users/" + login.user_id, "A read escaped the device acceptance scope")
        elif (method, path) in {("DELETE", "/admin/v1/session"), ("POST", "/emby/Sessions/Logout")}:
            check(login.token and not login.logout_attempted and native == (method == "DELETE"), "An unowned logout was prevented")
            login.logout_attempted = True
        else:
            match = re.fullmatch(r"/admin/v1/devices/([1-9][0-9]*)/(options|delete)", route.path)
            check(method == "POST" and native and not route.query and match and match[1] in self.devices and
                  int(match[1]) > 1 and isinstance(body, dict) and isinstance(body.get("Revision"), str) and NUMBER.fullmatch(body["Revision"]) and
                  set(body) == ({"Revision", "CustomName"} if match[2] == "options" else {"Revision"}),
                  "A device mutation escaped an acknowledged new ordinary generation")
        self.core.remaining()
        check(sum(self.counts.values()) < (200 if self.core.CLEANING else 165), "The HTTP request budget was exhausted")
        self.counts[method] += 1
        headers = {"Accept": "application/json", "Origin": ORIGIN}
        if native:
            if login.token:
                headers["Cookie"] = "goby_session=" + login.token
            if login.csrf:
                headers["X-CSRF-Token"] = login.csrf
        else:
            headers.update({"X-Emby-Client": login.app, "X-Emby-Device-Id": login.reported,
                            "X-Emby-Device-Name": login.name, "X-Emby-Client-Version": login.version})
            if login.token:
                headers["X-Emby-Token"] = login.token
        payload = None if body is None else json.dumps(body).encode("utf-8")
        check(payload is None or len(payload) <= self.core.MAX_BODY, "An HTTP request exceeded its byte bound")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=self.core.remaining())
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            cookies = SimpleCookie()
            cookies.load(response.getheader("Set-Cookie", ""))
            if authenticating and native and "goby_session" in cookies and TOKEN.fullmatch(cookies["goby_session"].value):
                login.token = cookies["goby_session"].value
            data = response.read(self.core.MAX_BODY + 1)
            check(len(data) <= self.core.MAX_BODY, "An HTTP response exceeded its byte bound")
            value = data if raw else json.loads(data) if data else None
            # Retain issued credentials before later status and DTO assertions;
            # unknown objects are never inferred from an interrupted response.
            if authenticating and isinstance(value, dict):
                token, csrf = value.get("AccessToken"), value.get("CSRFToken")
                if not native and isinstance(token, str) and TOKEN.fullmatch(token):
                    login.token = token
                if native and isinstance(csrf, str) and HASH.fullmatch(csrf):
                    login.csrf = csrf
            check(response.status in ((expected,) if isinstance(expected, int) else expected), "An HTTP request returned an unexpected status")
            check(response.status != 204 or not data, "An HTTP 204 response included content")
            return value, dict(response.getheaders())
        finally:
            connection.close()


def bind_login(core, api, baseline, current, login):
    created = core.preserve(baseline, current)
    digest = "\\x" + hashlib.sha256(login.token.encode("ascii")).hexdigest()
    matching = [row for row in created["sessions"].values() if row["token_hash"] == digest]
    check(len(matching) == 1, "A newly issued credential could not be bound to one database row")
    row = matching[0]
    check(ID.fullmatch(row["id"]) and row["kind"] == login.kind and row["user_id"] == login.user_id and row["revoked_at"] is None,
          "The issued credential has an unexpected identity or state")
    login.id, login.initial = row["id"], dict(row)
    check(set(created["sessions"]) == {entry.id for entry in api.logins if entry.id}, "A login created an unacknowledged extra credential")
    if login.kind == "admin":
        check(row["device_registry_id"] is None, "The native cookie registered an ordinary device")
        return
    check(row["device_id"] == login.reported and row["device_name"] == login.name and row["client_name"] == login.app and
          row["client_version"] == login.version and type(row["device_registry_id"]) is int and row["device_registry_id"] > 1,
          "The ordinary login lost its reported metadata or registry generation")
    login.device = str(row["device_registry_id"])
    device = created["devices"].get(login.device, {})
    check(device.get("reported_device_id") == login.reported and device.get("last_user_id") == login.user_id and
          device.get("reported_name") == login.name and device.get("app_name") == login.app and device.get("app_version") == login.version and
          device.get("deleted_at") is None, "The issued ordinary credential did not register its own new device")
    check(login.device not in api.devices or api.devices[login.device] == login.reported, "A device identity changed its reported owner")
    api.devices[login.device] = login.reported
    check(set(created["devices"]) == set(api.devices), "A login created an unacknowledged extra device")


def native_dto(core, value, current, now):
    check(isinstance(value, dict) and set(value) == FIELDS and isinstance(value["Id"], str) and NUMBER.fullmatch(value["Id"]) and
          int(value["Id"]) > 1 and isinstance(value["Revision"], str) and NUMBER.fullmatch(value["Revision"]),
          "A native device omitted its exact safe fields or canonical decimal identities")
    stored = core.index(current, "devices").get(value["Id"], {})
    check(stored and stored["deleted_at"] is None, "A native device did not match an active ordinary row")
    users = core.index(current, "users")
    user = users.get(stored["last_user_id"], {})
    expected_name = stored["custom_name"] if stored["custom_name"] is not None else next(
        (text for text in (stored["reported_name"], stored["app_name"], stored["reported_device_id"]) if text.strip()), "")
    expected = {"Id": str(stored["id"]), "Revision": str(stored["revision"]), "ReportedDeviceId": stored["reported_device_id"],
                "Name": expected_name, "ReportedName": stored["reported_name"], "CustomName": stored["custom_name"],
                "AppName": stored["app_name"], "AppVersion": stored["app_version"], "LastUserId": stored["last_user_id"],
                "LastUserName": user.get("name"), "IpAddress": stored["ip_address"]}
    count = sum(row["kind"] == "emby" and row["device_registry_id"] == stored["id"] and row["revoked_at"] is None and
                core.timestamp(row["expires_at"]) > now and not users[row["user_id"]]["is_disabled"]
                for row in core.rows(current, "sessions"))
    check(all(value[field] == wanted for field, wanted in expected.items()) and type(value["ActiveLoginCount"]) is int and
          value["ActiveLoginCount"] == count, "A native device field or active-login count differs from its real database projection")
    for field, column in (("CreatedAt", "created_at"), ("LastSeenAt", "last_seen_at")):
        check(core.timestamp(value[field]) == core.timestamp(stored[column]), "A native device timestamp differs from its stored value")


def list_page(core, api, admin, current, parameters):
    path = "/admin/v1/devices" + ("?" + urlencode(parameters) if parameters else "")
    page, headers = api.request("GET", path, admin)
    check(isinstance(page, dict) and set(page) == {"Items", "TotalRecordCount", "StartIndex", "Limit"} and
          isinstance(page["Items"], list) and headers.get("Cache-Control") == "no-store", "The native device page envelope or cache contract changed")
    users, term = core.index(current, "users"), parameters.get("SearchTerm", "").lower()
    expected = []
    for row in core.rows(current, "devices"):
        search = [row["custom_name"] if row["custom_name"] is not None else row["reported_name"], row["reported_name"],
                  row["reported_device_id"], row["app_name"], users.get(row["last_user_id"], {}).get("name", "")]
        if row["id"] > 1 and row["deleted_at"] is None and (not term or any(term in field.lower() for field in search)):
            expected.append(row)
    expected.sort(key=lambda row: (core.timestamp(row["last_seen_at"]), row["id"]), reverse=True)
    start, limit = int(parameters.get("StartIndex", 0)), int(parameters.get("Limit", 50))
    check(type(page["TotalRecordCount"]) is int and page["TotalRecordCount"] == len(expected) and page["StartIndex"] == start and page["Limit"] == limit and
          [entry["Id"] for entry in page["Items"]] == [str(row["id"]) for row in expected[start:start + limit]],
          "Native device search, pagination, count, or numeric activity ordering changed")
    for value in page["Items"]:
        native_dto(core, value, current, datetime.now(timezone.utc))
    encoded = json.dumps(page)
    check(not any(secret and secret in encoded for login in api.logins for secret in (login.token, login.csrf)),
          "The native device page exposed a raw authentication secret")
    return page["Items"]


def mutate(core, utilities, database, api, admin, baseline, identifier, action, revision, name=None, expected=200):
    prior = snapshot(core, utilities, database)
    core.preserve(baseline, prior)
    stored = core.index(prior, "devices").get(identifier, {})
    check(identifier in api.devices and int(identifier) > 1 and stored.get("reported_device_id") == api.devices[identifier] and
          identifier not in core.index(baseline, "devices"), "The mutation target is not the exact owned ordinary identity and reported ID")
    body = {"Revision": str(revision)}
    if action == "options":
        body["CustomName"] = name
    value, _ = api.request("POST", "/admin/v1/devices/" + identifier + "/" + action, admin, body=body, expected=expected)
    after = snapshot(core, utilities, database)
    core.preserve(baseline, after)
    if expected == 409:
        check(value.get("Error", {}).get("Code") == "revision_conflict" and prior == after, "A stale device revision changed state or returned another error")
        return value, after
    current = core.index(after, "devices")[identifier]
    wanted = dict(stored)
    expected_sessions = core.index(prior, "sessions")
    if action == "options":
        wanted["custom_name"] = name.strip() or None
        wanted["revision"] += wanted["custom_name"] != stored["custom_name"]
        native_dto(core, value, after, datetime.now(timezone.utc))
        check(value["Id"] == identifier and value["ReportedDeviceId"] == api.devices[identifier], "A rename response selected another device")
    else:
        check(isinstance(value, dict) and set(value) == {"Id", "DeletedAt", "RevokedLoginCount"} and value["Id"] == identifier,
              "A delete receipt selected another device or exposed extra fields")
        revoked_count = 0
        if stored["deleted_at"] is None:
            wanted["deleted_at"], wanted["revision"] = current["deleted_at"], stored["revision"] + 1
            for row in expected_sessions.values():
                if row["kind"] == "emby" and row["device_registry_id"] == int(identifier) and row["revoked_at"] is None:
                    check(row["id"] in {login.id for login in api.logins if login.id}, "Device deletion would revoke a foreign credential")
                    row["revoked_at"] = current["deleted_at"]
                    revoked_count += 1
        check(current["deleted_at"] is not None and core.timestamp(value["DeletedAt"]) == core.timestamp(current["deleted_at"]) and
              type(value["RevokedLoginCount"]) is int and value["RevokedLoginCount"] == revoked_count,
              "The delete receipt differs from the exact committed credential revocations")
        api.deleted[identifier] = dict(current)
    check(current == wanted and core.index(after, "sessions") == expected_sessions, "A device mutation changed an unexpected field or credential")
    for table in prior:
        if table not in {"devices", "sessions"}:
            check(prior[table] == after[table], "A device mutation escaped its two owned tables")
    check({key: row for key, row in core.index(prior, "devices").items() if key != identifier} ==
          {key: row for key, row in core.index(after, "devices").items() if key != identifier}, "A device mutation changed another generation")
    return value, after


def main():
    report = {"owner": OWNER, "status": "failed", "http_retries": 0, "cleanup": {"proven": False, "errors": []}, "snapshots": {},
              "scope": "Four new ordinary logins, one new native login, and mutations of three owned ordinary generations only",
              "limitations": ["Shared-key mutation and same-reported-ID key isolation belong to disposable acceptance; historical shared rows are preserved"],
              "request_limit": 200, "body_limit_bytes": 2 * 1024 * 1024, "workflow_limit_seconds": 180, "raw_response_files_written": False}
    core = utilities = sessions = database = baseline = api = deployment = vault = sources = configured = parent_identity = None
    viewer_files, credentials, logins = {}, {}, []
    stage = "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without arguments")
        signal.signal(signal.SIGALRM, deadline)
        signal.setitimer(signal.ITIMER_REAL, max(0.1, 135 - (time.monotonic() - STARTED)))
        accepted = os.environ.get("GOBY_DEVICES_EXPECTED_BINARY_SHA256", "")
        check(HASH.fullmatch(accepted), "The accepted M5e binary digest environment variable is required")
        number, result_path, attempt_path = output_files()
        report["verification_run"] = number
        report["output_scope"] = {"directory": str(ROOT), "report": result_path.name, "attempt_marker": attempt_path.name}
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed report directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (result_path, attempt_path)), "The selected attempt already exists; no overwrite is allowed")
        core, utilities, sessions = helpers()
        script_hash = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        utilities.private_write(attempt_path, {"owner": OWNER, "verification_run": number, "status": "claimed",
                                               "started_at": datetime.now(timezone.utc).isoformat(), "script_sha256": script_hash})
        parent_identity = info.st_dev, info.st_ino
        deployment = sessions.deployment(accepted)
        runtime = sessions.private_values(ROOT / "runtime.env", {"GOBY_LISTEN", "GOBY_PUBLIC_URL", "GOBY_MEDIA_ROOTS"})
        check(runtime["GOBY_LISTEN"] == "127.0.0.1:18096" and runtime["GOBY_PUBLIC_URL"] == ORIGIN, "The main HTTP namespace is outside its loopback scope")
        configured = runtime["GOBY_MEDIA_ROOTS"]
        database = core.database_type(sessions)()
        baseline = snapshot(core, utilities, database)
        core.quiescent(baseline, database)
        report["deployment"] = {**deployment, "schema_version": 18, "database_role": "goby_test", "database_port": 5432,
                                "postgresql_forced_read_only": True, "verifier_sha256": script_hash}
        report["helper_sha256"] = {name: hashlib.sha256(Path(__file__).with_name(name).read_bytes()).hexdigest() for name in
                                    ("verify-application-keys-deployed.py", "verify-metadata-deployed.py", "verify-sessions-deployed.py")}
        report["snapshots"]["before"] = utilities.snapshot_report(baseline)
        namespaces = [row["value"] for row in core.rows(baseline, "server_settings") if row["key"] == "server_id"]
        check(len(namespaces) == 1 and ID.fullmatch(namespaces[0]), "The persistent server namespace is invalid")
        namespace = namespaces[0]
        shared = core.rows(baseline, "application_key_devices")
        check(shared and all(row["reported_device_id"] == namespace for row in shared), "The historical shared-key namespace differs from the persistent server")
        report["server_namespace_sha256"] = hashlib.sha256(namespace.encode()).hexdigest()
        vault = core.vault_identity(sessions)
        sources = source_hashes(core, baseline, configured)
        report["source_before_sha256"] = utilities.aggregate(sources)
        report["source_scope"] = {"files_hashed": len(sources), "per_file_limit_bytes": 64 * 1024 * 1024,
                                  "total_limit_bytes": 256 * 1024 * 1024, "read_only_no_follow_no_atime": True}
        admin_values = sessions.private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        marker, viewer_files[".goby-direct-playback-owned.json"] = private_json(VIEWER / ".goby-direct-playback-owned.json", core)
        viewer, viewer_files[".goby-direct-playback-state.json"] = private_json(VIEWER / ".goby-direct-playback-state.json", core)
        check(marker.get("owner") == viewer.get("owner") == "goby-direct-playback-v1" and
              isinstance(marker.get("nonce"), str) and ID.fullmatch(marker["nonce"]) and viewer.get("nonce") == marker["nonce"] and
              viewer.get("user_name") == "Goby Direct Playback " + marker["nonce"][:16] and isinstance(viewer.get("user_password"), str) and
              32 <= len(viewer["user_password"]) <= 72, "The existing viewer credentials do not match their protected ownership marker")
        users = core.index(baseline, "users")
        admins = [row for row in users.values() if row["name"] == admin_values["GOBY_SMOKE_NAME"] and row["is_administrator"] and not row["is_disabled"]]
        check(len(admins) == 1 and viewer.get("user_id") in users, "The private credentials do not identify existing accounts")
        administrator, member = admins[0], users[viewer["user_id"]]
        check(member["name"] == viewer["user_name"] and not member["is_administrator"] and not member["is_disabled"] and
              member["id"] != administrator["id"], "The existing viewer is not an independent enabled non-administrator")
        credentials = {administrator["id"]: (administrator["name"], admin_values["GOBY_SMOKE_PASSWORD"]),
                       member["id"]: (member["name"], viewer["user_password"])}
        run_id = secrets.token_hex(16)
        prefix = "m5e-" + run_id
        reported, unrelated = prefix + "-shared%_\\", prefix + "-sibling"
        check(all(row["reported_device_id"] not in {reported, unrelated} for table in ("devices", "application_key_devices") for row in core.rows(baseline, table)) and
              all(row["device_id"] not in {reported, unrelated} for table in ("sessions", "application_key_clients") for row in core.rows(baseline, table)),
              "A proposed reported identifier already belongs to existing history")
        admin = Login("admin", administrator["id"])
        first = Login("emby", administrator["id"], reported, "M5e first display " + run_id, "M5e first app " + run_id)
        second = Login("emby", member["id"], reported, "M5e latest display " + run_id, "M5e latest app " + run_id, "m5e-second")
        sibling = Login("emby", member["id"], unrelated, "M5e sibling display " + run_id, "M5e sibling app " + run_id)
        replacement = Login("emby", member["id"], reported, "M5e replacement display " + run_id, "M5e replacement app " + run_id)
        logins = [admin, first, second, sibling, replacement]
        api = API(core, logins)

        def login(entry):
            name, password = credentials[entry.user_id]
            native = entry.kind == "admin"
            value, _ = api.request("POST", "/admin/v1/session" if native else "/emby/Users/AuthenticateByName", entry,
                                   body={"Name" if native else "Username": name, "Password" if native else "Pw": password})
            check(entry.token and value.get("User", {}).get("Id") == entry.user_id, "A real login returned another account or omitted its token")
            if native:
                check(entry.csrf == hashlib.sha256(("goby:admin:csrf:" + entry.token).encode()).hexdigest(), "The native CSRF token is not bound to its cookie")
            else:
                check(value.get("ServerId") == namespace, "The login belongs to another server namespace")
            current = snapshot(core, utilities, database)
            bind_login(core, api, baseline, current, entry)
            if not native:
                check(value.get("SessionInfo", {}).get("Id") == entry.id and value["SessionInfo"].get("DeviceId") == entry.reported,
                      "The real login wire identity differs from its stored credential")
            return current

        stage = "new native cookie and cross-user reported-device grouping"
        for entry in (admin, first, second, sibling):
            current = login(entry)
        check(first.id != second.id and first.device == second.device != sibling.device and len(api.devices) == 2,
              "Two users did not share one ordinary generation independently of the unrelated device")
        devices = list_page(core, api, admin, current, {"SearchTerm": prefix, "Limit": "200"})
        target = next(entry for entry in devices if entry["Id"] == first.device)
        check(target["ActiveLoginCount"] == 2 and target["LastUserId"] == member["id"] and target["ReportedName"] == second.name and
              target["AppName"] == second.app and target["CustomName"] is None, "Cross-user grouping lost the last login's actual metadata")
        for parameters in ({}, {"SearchTerm": prefix, "StartIndex": "0", "Limit": "1"}, {"SearchTerm": prefix, "StartIndex": "1", "Limit": "1"},
                           {"SearchTerm": prefix, "StartIndex": "2", "Limit": "1"}, {"SearchTerm": reported},
                           {"SearchTerm": second.app.upper()}, {"SearchTerm": member["name"]}, {"SearchTerm": prefix + "-absent"}):
            list_page(core, api, admin, current, parameters)
        check(snapshot(core, utilities, database) == current, "Native device reads changed database rows")
        stage = "owned custom name, literal search, stale revision, and clear"
        original_revision = target["Revision"]
        renamed = "M5e custom 100%_\\ " + run_id
        changed, current = mutate(core, utilities, database, api, admin, baseline, first.device, "options", original_revision, renamed)
        check(changed["Name"] == renamed and changed["CustomName"] == renamed and int(changed["Revision"]) == int(original_revision) + 1,
              "Native rename did not persist its exact custom name and revision")
        list_page(core, api, admin, current, {"SearchTerm": "100%_\\ " + run_id})
        for action in ("options", "delete"):
            _, current = mutate(core, utilities, database, api, admin, baseline, first.device, action, original_revision, "Rejected stale name", expected=409)
        cleared, current = mutate(core, utilities, database, api, admin, baseline, first.device, "options", changed["Revision"], "")
        check(cleared["CustomName"] is None and cleared["Name"] == second.name and int(cleared["Revision"]) == int(changed["Revision"]) + 1,
              "Clearing a custom name did not restore the reported-name projection")
        stage = "exact generation deletion and fresh relogin"
        receipt, current = mutate(core, utilities, database, api, admin, baseline, first.device, "delete", cleared["Revision"])
        check(receipt["RevokedLoginCount"] == 2, "The owned target deletion did not revoke exactly its two ordinary logins")
        for entry in (first, second):
            api.request("GET", "/emby/Users/" + entry.user_id, entry, expected=401, raw=True)
        api.request("GET", "/emby/Users/" + sibling.user_id, sibling)
        value, _ = api.request("GET", "/admin/v1/session", admin)
        check(value.get("User", {}).get("Id") == admin.user_id and value.get("CSRFToken") == admin.csrf,
              "Ordinary device deletion invalidated the independent native cookie")
        current = login(replacement)
        check(replacement.device != first.device and int(replacement.device) > int(first.device) and len(api.devices) == 3 and
              core.index(current, "devices")[first.device] == api.deleted[first.device], "Relogin reused or rewrote the deleted device generation")
        repeated, current = mutate(core, utilities, database, api, admin, baseline, first.device, "delete", cleared["Revision"])
        check(repeated == {"Id": first.device, "DeletedAt": receipt["DeletedAt"], "RevokedLoginCount": 0}, "Historical delete lost its idempotent receipt")
        api.request("GET", "/emby/Users/" + replacement.user_id, replacement)
        current = snapshot(core, utilities, database)
        visible = list_page(core, api, admin, current, {"SearchTerm": prefix, "Limit": "200"})
        check({entry["Id"] for entry in visible} == {sibling.device, replacement.device} and
              all(entry["CustomName"] is None and entry["ActiveLoginCount"] == 1 for entry in visible), "The exact two surviving ordinary generations are incorrect")
        report["checks"] = {"native_fields": 14, "cross_user_grouped_logins": 2, "independent_ordinary_device": True,
                            "exact_search_pagination_counts": True, "literal_custom_name_and_clear": True, "stale_options_and_delete_status": 409,
                            "target_tokens_denied": 2, "native_cookie_survives": True, "unrelated_token_status": 200,
                            "relogin_new_generation": True, "historical_delete_idempotent": True, "replacement_token_status": 200}
        report["status"] = "passed"
    except BaseException as error:
        report["failed_stage"] = stage
        report["error"] = str(error) if isinstance(error, Failure) else "A protected operation failed; exception details were withheld"
    finally:
        if core is not None:
            core.CLEANING = True
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, max(0.1, 178 - (time.monotonic() - STARTED)))
        if api is not None:
            admin = logins[0]
            if admin.token and not admin.csrf:
                try:
                    value, _ = api.request("GET", "/admin/v1/session", admin)
                    admin.csrf = value.get("CSRFToken", "")
                    check(HASH.fullmatch(admin.csrf), "The native cleanup CSRF token could not be recovered")
                except BaseException:
                    report["cleanup"]["errors"].append("The owned native cleanup credential could not be recovered")
            for identifier in api.devices:
                try:
                    current = snapshot(core, utilities, database)
                    device = core.index(current, "devices")[identifier]
                    if device["deleted_at"] is None:
                        mutate(core, utilities, database, api, admin, baseline, identifier, "delete", device["revision"])
                except BaseException:
                    report["cleanup"]["errors"].append("An acknowledged ordinary generation could not be retired")
            for entry in reversed(logins):
                if not entry.attempted:
                    continue
                if not entry.token:
                    report["cleanup"]["errors"].append("A login response lost its token; no unknown credential or device was mutated")
                    continue
                try:
                    native = entry.kind == "admin"
                    api.request("DELETE" if native else "POST", "/admin/v1/session" if native else "/emby/Sessions/Logout", entry,
                                expected=(204, 401) if native else (200, 401), raw=True)
                except BaseException:
                    report["cleanup"]["errors"].append("An owned credential logout could not be confirmed")
            report["http_method_counts"] = api.counts
        credentials.clear()
        if baseline is not None:
            try:
                final = snapshot(core, utilities, database)
                created = core.preserve(baseline, final)
                report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                check(api is not None and set(created["sessions"]) == {entry.id for entry in logins if entry.id} and
                      set(created["devices"]) == set(api.devices), "Final new rows escaped acknowledged credentials and ordinary generations")
                for entry in logins:
                    if not entry.attempted:
                        continue
                    check(entry.initial is not None and entry.id in created["sessions"], "A login's final database identity is unproven")
                    stored = created["sessions"][entry.id]
                    check(stored["revoked_at"] is not None and core.timestamp(stored["last_seen_at"]) >= core.timestamp(entry.initial["last_seen_at"]) and
                          all(stored[field] == value for field, value in entry.initial.items() if field not in {"revoked_at", "last_seen_at"}),
                          "A new login changed more than its attributable activity and revocation timestamps")
                for identifier, device in created["devices"].items():
                    check(device["reported_device_id"] == api.devices[identifier] and device["deleted_at"] is not None and
                          (identifier not in api.deleted or device == api.deleted[identifier]), "An owned generation was not retained as exact deleted history")
                if report["status"] == "passed":
                    check(len(created["sessions"]) == 5 and len(created["devices"]) == 3, "The successful run did not retain exactly five logins and three device generations")
                report["cleanup"].update({"proven": True, "new_credentials_revoked": len(created["sessions"]),
                                           "new_device_generations_soft_deleted": len(created["devices"]), "physical_deletions": 0})
                report["all_preexisting_rows_raw_exact"] = report["historical_key_namespace_raw_exact"] = report["existing_users_and_policy_raw_exact"] = True
                report["expired_prepared_row_raw_exact"] = True
            except BaseException:
                report["cleanup"]["errors"].append("Complete old-row preservation or owned credential and device retirement could not be proven")
        for label, initial, observe in (("deployment", deployment, lambda: sessions.deployment(accepted)),
                                        ("vault", vault, lambda: core.vault_identity(sessions)),
                                        ("sources", sources, lambda: source_hashes(core, baseline, configured))):
            if initial is not None:
                try:
                    core.remaining()
                    check(observe() == initial, "A protected deployment, vault, or source identity changed")
                    report[label + "_unchanged"] = True
                except BaseException:
                    report["cleanup"]["errors"].append("A protected deployment, vault, or media source identity could not be proven unchanged")
        for name, initial in viewer_files.items():
            try:
                check(private_json(VIEWER / name, core)[1] == initial, "An existing viewer credential file changed")
            except BaseException:
                report["cleanup"]["errors"].append("An existing private viewer credential document could not be proven unchanged")
        if vault is not None:
            report["vault"] = {"uid": 995, "mode": "0600", "bytes": 32, "directory_mode": "0700"}
        if not report["cleanup"]["proven"] or report["cleanup"]["errors"]:
            report["status"] = "failed"
        report["elapsed_seconds"] = round(time.monotonic() - STARTED, 3)
        if parent_identity is not None:
            try:
                info = ROOT.lstat()
                check((info.st_dev, info.st_ino) == parent_identity and info.st_uid == 0 and info.st_mode & 0o022 == 0, "The fixed report directory changed")
                utilities.private_write(result_path, report)
            except BaseException:
                report = {"owner": OWNER, "status": "failed", "error": "The exclusive sanitized report could not be saved; no overwrite was attempted"}
        if sys.platform == "linux":
            signal.setitimer(signal.ITIMER_REAL, 0)
        print(json.dumps(report, ensure_ascii=True, sort_keys=True))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
