#!/usr/bin/env python3
"""Verify deployed M5c sessions once through authorized root SSH on Linux.

Set GOBY_SESSIONS_EXPECTED_BINARY_SHA256 to the accepted deployed build digest.
Only three normal logins, revocation of their own sessions, and their normal
logouts may write application state. No source media is opened. Complete public
row texts and credentials remain in memory; the exclusive report is aggregate.
The metadata verifier supplies only its inert snapshot/report utility functions.
"""

from __future__ import annotations

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
import shlex
import stat
import subprocess
import sys
from urllib.parse import unquote, urlencode, urlsplit


sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test")
RESULT = ROOT / "m5c-deployed-sessions.json"
ATTEMPT = ROOT / "m5c-deployed-sessions.attempt.json"
OWNER = "goby-sessions-deployed-m5c-v1"
ORIGIN = "http://127.0.0.1:18096"
SERVICE = "goby-foundation-test.service"
TABLES = set("schema_migrations server_settings users sessions libraries library_roots items scan_jobs "
             "catalog_entities item_entities item_images user_item_data play_sessions item_subtitles "
             "encoding_jobs client_playback_references item_metadata_state".split())
FIELDS = set("Id UserId UserName UserIsAdministrator UserIsDisabled Kind Client DeviceId DeviceName "
             "ApplicationVersion CreatedAt LastSeenAt ExpiresAt RevokedAt Status IsCurrent".split())
ID = re.compile(r"[0-9a-f]{32}")
TOKEN = re.compile(r"[A-Za-z0-9_-]{43}")
HASH = re.compile(r"[0-9a-f]{64}")
UTC = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z")
MAX_BODY = 2 * 1024 * 1024


class Failure(Exception):
    """Contain a fixed assertion label only."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def helpers():
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        specification = importlib.util.spec_from_file_location(
            "sessions_snapshot_helpers", Path(__file__).with_name("verify-metadata-deployed.py"))
        check(specification is not None and specification.loader is not None, "Snapshot utilities are unavailable")
        module = importlib.util.module_from_spec(specification)
        specification.loader.exec_module(module)
    return module


def private_values(path, wanted):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(descriptor, "rb") as source:
        info = os.fstat(source.fileno())
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and
              info.st_nlink == 1 and info.st_size <= 65536, "A configuration file is not private and bounded")
        raw = source.read(65537)
    check(len(raw) <= 65536, "A configuration file exceeded its byte limit")
    values = {}
    for line in raw.decode("utf-8").splitlines():
        name, separator, value = line.strip().removeprefix("export ").partition("=")
        if separator and name in wanted:
            parts = shlex.split(value, comments=False, posix=True)
            check(name not in values and len(parts) == 1 and parts[0], "A required configuration assignment is invalid")
            values[name] = parts[0]
    check(set(values) == wanted, "Required private configuration is missing")
    return values


class Database:
    """Observe only the deployed database through forced read-only psql."""

    def __init__(self):
        values = private_values(ROOT / "runtime.env", {"GOBY_DATABASE_URL"})
        parsed = urlsplit(values["GOBY_DATABASE_URL"])
        check(parsed.scheme in {"postgres", "postgresql"} and parsed.hostname == "127.0.0.1" and
              parsed.port in {None, 5432} and unquote(parsed.path) == "/goby_test" and
              parsed.username and parsed.password and parsed.query in {"", "sslmode=disable"} and not parsed.fragment,
              "The runtime database is outside the fixed deployed scope")
        self.environment = {"PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", "PGHOST": "127.0.0.1", "PGPORT": "5432",
                            "PGDATABASE": "goby_test", "PGUSER": unquote(parsed.username),
                            "PGPASSWORD": unquote(parsed.password), "PGPASSFILE": "/dev/null", "PGCONNECT_TIMEOUT": "5",
                            "PGSSLMODE": "disable", "PGCLIENTENCODING": "UTF8",
                            "PGOPTIONS": "-c default_transaction_read_only=on -c statement_timeout=5000 "
                                         "-c lock_timeout=2000 -c timezone=UTC -c bytea_output=hex -c DateStyle=ISO,YMD"}

    def read(self, query, label):
        result = subprocess.run(["/usr/bin/psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"],
                                input=query.encode("utf-8"), env=self.environment, capture_output=True, timeout=15)
        check(result.returncode == 0 and len(result.stdout) <= 8 * 1024 * 1024 and len(result.stderr) <= 65536,
              "A bounded read-only database observation failed")
        return json.loads(result.stdout)


def deployment(accepted):
    result = subprocess.run(["/usr/bin/systemctl", "show", SERVICE, "-p", "MainPID", "-p", "User", "-p", "ActiveState"],
                            capture_output=True, text=True, timeout=5)
    check(result.returncode == 0 and len(result.stdout) < 1024, "The deployment observation failed")
    values = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
    check(values.get("User") == "goby" and values.get("ActiveState") == "active", "The accepted service is not active")
    pid = int(values.get("MainPID", "0"))
    check(pid > 1, "The accepted service has no live process")
    status = Path(f"/proc/{pid}/status").read_text(encoding="utf-8")
    uids = next(line for line in status.splitlines() if line.startswith("Uid:"))
    check([int(value) for value in uids.split()[1:]] == [995] * 4, "The service UID differs from 995")
    started = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8").rsplit(") ", 1)[1].split()[19]
    digest = hashlib.sha256()
    with Path(f"/proc/{pid}/exe").open("rb") as source:
        check(os.fstat(source.fileno()).st_size <= 512 * 1024 * 1024, "The executable exceeded its byte limit")
        for block in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(block)
    check(digest.hexdigest() == accepted, "The service executable differs from the accepted M5c build")
    return {"main_pid": pid, "uid": 995, "start_ticks": started, "binary_sha256": accepted}


def timestamp(value, native=False):
    check(isinstance(value, str) and (not native or UTC.fullmatch(value)), "A session timestamp is not UTC")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    check(parsed.tzinfo is not None, "A session timestamp has no timezone")
    return parsed


def snapshot(utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == TABLES, "The public table inventory is not exactly the seventeen accepted tables")
    migrations = utilities.parsed_rows(value, "schema_migrations")
    check(len(migrations) == 14 and {row["version"] for row in migrations} == set(range(1, 15)),
          "The deployed schema differs from version fourteen")
    media = [row["media"] for row in utilities.parsed_rows(value, "items") if row.get("media") is not None]
    check(media and all(isinstance(row, dict) and type(row.get("ProbeVersion")) is int and row["ProbeVersion"] == 6 for row in media),
          "The deployed media catalog differs from probe version six")
    return value


def preserve(utilities, before, after):
    check(set(before) == set(after) == TABLES, "The public table inventory changed")
    check(all(before[name] == after[name] for name in TABLES - {"sessions"}), "A non-session table changed")
    old, current = utilities.rows_by_id(before, "sessions"), utilities.rows_by_id(after, "sessions")
    check(all(current.get(key) == value for key, value in old.items()), "A preexisting session row changed")
    return {key: json.loads(current[key]) for key in current.keys() - old.keys()}


class Login:
    def __init__(self, kind, client=""):
        self.kind, self.client = kind, client
        self.token = self.csrf = self.identifier = ""
        self.attempted = self.logout_attempted = False


class API:
    def __init__(self, user_id, device_id):
        self.user_id, self.device_id = user_id, device_id
        self.owned = set()
        self.counts = {"GET": 0, "POST": 0, "DELETE": 0}
        self.revoke_count = 0

    def request(self, method, path, login, *, body=None, expected=(200,), token_as_cookie=False, parse=True):
        route = urlsplit(path)
        check(path.startswith("/") and not path.startswith("//") and not route.scheme and not route.netloc and
              not route.fragment and "\r" not in path and "\n" not in path, "The HTTP route escaped its local scope")
        is_login = method == "POST" and route.path in {"/admin/v1/session", "/emby/Users/AuthenticateByName"}
        if is_login:
            check(not route.query and not login.attempted and not login.token and
                  (route.path == "/admin/v1/session") == (login.kind == "admin"), "An additional login was prevented")
            login.attempted = True
        elif method == "GET":
            check(route.path in {"/admin/v1/sessions", "/admin/v1/session", "/admin/v1/capabilities",
                                 "/emby/Users/" + self.user_id}, "An HTTP read escaped the session scope")
        elif (method, route.path) in {("DELETE", "/admin/v1/session"), ("POST", "/emby/Sessions/Logout")}:
            check(not route.query and not login.logout_attempted and login.token and
                  (method == "DELETE") == (login.kind == "admin"), "An additional or unowned logout was prevented")
            login.logout_attempted = True
        else:
            match = re.fullmatch(r"/admin/v1/sessions/([0-9a-f]{32})/revoke", route.path)
            check(method == "POST" and not route.query and match and match[1] in self.owned and body == {} and
                  self.revoke_count < 5, "An unowned session mutation was prevented")
            self.revoke_count += 1
        check(sum(self.counts.values()) < 60, "The HTTP request budget was exceeded")
        self.counts[method] += 1
        headers = {"Accept": "application/json", "Origin": ORIGIN}
        if login.kind == "admin" or token_as_cookie:
            if login.token:
                headers["Cookie"] = "goby_session=" + login.token
            if login.csrf:
                headers["X-CSRF-Token"] = login.csrf
        if login.kind == "emby":
            headers["Authorization"] = (f'Emby Client="{login.client}", DeviceId="{self.device_id}", '
                                        'Device="Linux Test", Version="m5c-deployed"')
            if login.token:
                headers["X-Emby-Token"] = login.token
        payload = None if body is None else json.dumps(body).encode("utf-8")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=8)
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            cookies = SimpleCookie()
            cookies.load(response.getheader("Set-Cookie", ""))
            if is_login and login.kind == "admin" and "goby_session" in cookies:
                value = cookies["goby_session"].value
                if TOKEN.fullmatch(value):
                    login.token = value
            raw = response.read(MAX_BODY + 1)
            check(len(raw) <= MAX_BODY, "An HTTP response exceeded its byte limit")
            value = json.loads(raw) if parse and raw else None
            # Retain issued credentials before further assertions so malformed
            # envelopes or partial administrator responses can still be cleaned.
            if is_login and isinstance(value, dict):
                token, csrf = value.get("AccessToken"), value.get("CSRFToken")
                if login.kind == "emby" and isinstance(token, str) and TOKEN.fullmatch(token):
                    login.token = token
                if login.kind == "admin" and isinstance(csrf, str) and HASH.fullmatch(csrf):
                    login.csrf = csrf
            check(response.status in expected, "An HTTP response returned an unexpected status")
            check(response.status != 204 or not raw, "An HTTP 204 response included content")
            return response.status, value, cookies
        finally:
            connection.close()


def bind_login(utilities, before, after, login, user_id, known, device_id):
    created = preserve(utilities, before, after)
    digest = "\\x" + hashlib.sha256(login.token.encode("ascii")).hexdigest()
    matching = [row for row in created.values() if row.get("token_hash") == digest]
    check(login.token and len(matching) == 1, "The issued login could not be bound to one new database row")
    row = matching[0]
    check(ID.fullmatch(row.get("id", "")) and row["id"] not in known and row["user_id"] == user_id and
          row["kind"] == login.kind and row["revoked_at"] is None, "The created login has an unexpected identity or state")
    check(row["client_name"] == ("Goby Dashboard" if login.kind == "admin" else login.client) and
          row["device_id"] == ("goby-dashboard" if login.kind == "admin" else device_id),
          "The created login has unexpected client metadata")
    login.identifier = row["id"]
    known[row["id"]] = row
    check(set(created) == set(known), "Login created an unexpected additional session")


def list_sessions(utilities, api, admin, current, parameters):
    _, page, _ = api.request("GET", "/admin/v1/sessions" + ("?" + urlencode(parameters) if parameters else ""), admin)
    check(isinstance(page, dict) and set(page) == {"Items", "TotalRecordCount", "StartIndex", "Limit"},
          "The native session page envelope changed")
    users = {row["id"]: row for row in utilities.parsed_rows(current, "users")}
    expected = []
    now = datetime.now(timezone.utc)
    for row in utilities.parsed_rows(current, "sessions"):
        user = users[row["user_id"]]
        status = ("revoked" if row["revoked_at"] is not None else "disabled" if user["is_disabled"] or
                  row["kind"] == "admin" and not user["is_administrator"] else
                  "expired" if timestamp(row["expires_at"]) <= now else "active")
        if any(parameters.get(key) and parameters[key] != row[column]
               for key, column in (("UserId", "user_id"), ("Kind", "kind"), ("DeviceId", "device_id"))):
            continue
        if parameters.get("Status", "active") not in {"all", status}:
            continue
        expected.append((row, user, status))
    expected.sort(key=lambda entry: (timestamp(entry[0]["created_at"]), entry[0]["id"]), reverse=True)
    start, limit = int(parameters.get("StartIndex", 0)), int(parameters.get("Limit", 50))
    check(type(page["TotalRecordCount"]) is int and page["TotalRecordCount"] == len(expected) and
          type(page["StartIndex"]) is int and page["StartIndex"] == start and
          type(page["Limit"]) is int and page["Limit"] == limit and isinstance(page["Items"], list),
          "The native session count or pagination changed")
    selected = expected[start:start + limit]
    check(len(page["Items"]) == len(selected), "The native session filter returned an unexpected row count")
    for item, (row, user, status) in zip(page["Items"], selected):
        wanted = {"Id": row["id"], "UserId": row["user_id"], "UserName": user["name"],
                  "UserIsAdministrator": user["is_administrator"], "UserIsDisabled": user["is_disabled"],
                  "Kind": row["kind"], "Client": row["client_name"], "DeviceId": row["device_id"],
                  "DeviceName": row["device_name"], "ApplicationVersion": row["client_version"],
                  "Status": status, "IsCurrent": row["id"] == admin.identifier}
        check(isinstance(item, dict) and set(item) == FIELDS and all(item[key] == value for key, value in wanted.items()) and
              all(type(item[key]) is bool for key in ("UserIsAdministrator", "UserIsDisabled", "IsCurrent")),
              "A native session did not match its sixteen public fields")
        for key, column in (("CreatedAt", "created_at"), ("LastSeenAt", "last_seen_at"),
                            ("ExpiresAt", "expires_at"), ("RevokedAt", "revoked_at")):
            check(item[key] is None if row[column] is None else timestamp(item[key], True) == timestamp(row[column]),
                  "A native session timestamp differs from its database value")
    return page["Items"]


def revocation(value, login, user_id, current):
    check(isinstance(value, dict) and set(value) == {"SessionId", "UserId", "Kind", "RevokedAt", "CurrentSessionRevoked"} and
          value["SessionId"] == login.identifier and value["UserId"] == user_id and value["Kind"] == login.kind and
          value["CurrentSessionRevoked"] is current, "A revocation response has unexpected scope")
    timestamp(value["RevokedAt"], True)
    return value["RevokedAt"]


def main():
    report = {"owner": OWNER, "status": "failed", "http_retries": 0, "source_media_opened": False,
              "cleanup": {"proven": False, "errors": []}, "snapshots": {}}
    utilities = database = api = before = accepted_deployment = parent_identity = None
    admin, target, sibling = Login("admin"), Login("emby", "M5c target client"), Login("emby", "M5c sibling client")
    logins, known = [admin, target, sibling], {}
    stage = "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without command-line arguments")
        accepted = os.environ.get("GOBY_SESSIONS_EXPECTED_BINARY_SHA256", "")
        check(HASH.fullmatch(accepted), "Set the accepted deployed M5c executable digest in the required environment variable")
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed report directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (RESULT, ATTEMPT)), "A prior attempt exists; no retry or overwrite is allowed")
        utilities = helpers()
        utilities.private_write(ATTEMPT, {"owner": OWNER, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                                         "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
        parent_identity = (info.st_dev, info.st_ino)
        stage = "deployment and complete baseline"
        accepted_deployment = deployment(accepted)
        report["deployment"] = dict(accepted_deployment)
        database = Database()
        before = snapshot(utilities, database)
        report["deployment"].update({"schema_version": 14, "probe_version": 6})
        report["snapshots"]["before"] = utilities.snapshot_report(before)
        credentials = private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        users = [row for row in utilities.parsed_rows(before, "users") if row["name"] == credentials["GOBY_SMOKE_NAME"]]
        check(len(users) == 1 and users[0]["is_administrator"] is True and users[0]["is_disabled"] is False,
              "The credential file does not identify one existing enabled administrator")
        user_id = users[0]["id"]
        check(ID.fullmatch(user_id), "The existing administrator identifier is invalid")
        api = API(user_id, "goby-m5c-deployed-" + secrets.token_hex(16))
        check(all(row["device_id"] != api.device_id for row in utilities.parsed_rows(before, "sessions")), "The owned device identifier already exists")
        for login in logins:
            stage = "three independent normal logins"
            native = login.kind == "admin"
            body = {"Name" if native else "Username": credentials["GOBY_SMOKE_NAME"],
                    "Password" if native else "Pw": credentials["GOBY_SMOKE_PASSWORD"]}
            _, value, _ = api.request("POST", "/admin/v1/session" if native else "/emby/Users/AuthenticateByName", login, body=body)
            check(login.token and (not native or login.csrf) and isinstance(value, dict) and value.get("User", {}).get("Id") == user_id,
                  "A normal login omitted its credential or returned a different account")
            current = snapshot(utilities, database)
            bind_login(utilities, before, current, login, user_id, known, api.device_id)
            api.owned.add(login.identifier)
            if not native:
                check(value.get("SessionInfo", {}).get("Id") == login.identifier, "An Emby login returned a different session identity")
        credentials.clear()
        check(len({login.token for login in logins}) == 3, "The logins did not issue independent credentials")
        report["snapshots"]["after_logins"] = utilities.snapshot_report(current)
        stage = "native list contract and session filters"
        _, capability, _ = api.request("GET", "/admin/v1/capabilities", admin)
        check(capability.get("Features", {}).get("SessionManagement") is True, "The deployed session-management capability is disabled")
        variants = [{}, {"Status": "all", "Limit": "200"}, {"Kind": "admin"}, {"Kind": "emby"},
                    {"UserId": user_id}, {"DeviceId": api.device_id}, {"Status": "revoked"},
                    {"UserId": user_id, "Kind": "emby", "Status": "active", "DeviceId": api.device_id},
                    {"DeviceId": api.device_id, "StartIndex": "1", "Limit": "1"}]
        for parameters in variants:
            items = list_sessions(utilities, api, admin, current, parameters)
            if parameters == {"Kind": "admin"}:
                check(sum(item["IsCurrent"] for item in items) == 1, "The current administrator was not identified exactly once")
        stage = "Emby credentials denied by native session API"
        revoke_route = "/admin/v1/sessions/" + target.identifier + "/revoke"
        for login in (target, sibling):
            for as_cookie in (False, True):
                api.request("GET", "/admin/v1/sessions", login, token_as_cookie=as_cookie, expected=(401,))
            api.request("POST", revoke_route, login, body={}, token_as_cookie=True, expected=(401,))
            api.request("GET", "/emby/Users/" + user_id, login)
        check(snapshot(utilities, database) == current, "Read-only session requests or denied writes changed complete database rows")
        stage = "target-only durable and idempotent revocation"
        _, result, _ = api.request("POST", revoke_route, admin, body={})
        revoked_at = revocation(result, target, user_id, False)
        after_revoke = snapshot(utilities, database)
        created = preserve(utilities, before, after_revoke)
        expected_rows = {key: dict(row) for key, row in known.items()}
        expected_rows[target.identifier]["revoked_at"] = created[target.identifier]["revoked_at"]
        check(created == expected_rows and timestamp(created[target.identifier]["revoked_at"]) == timestamp(revoked_at, True),
              "Target revocation changed another session or another target field")
        api.request("GET", "/emby/Users/" + user_id, target, expected=(401,), parse=False)
        api.request("GET", "/emby/Users/" + user_id, sibling)
        _, repeated, _ = api.request("POST", revoke_route, admin, body={})
        check(revocation(repeated, target, user_id, False) == revoked_at and snapshot(utilities, database) == after_revoke,
              "Repeated revocation changed its timestamp or another database row")
        for parameters in ({"DeviceId": api.device_id}, {"DeviceId": api.device_id, "Status": "revoked"},
                           {"DeviceId": api.device_id, "Status": "all"}):
            list_sessions(utilities, api, admin, after_revoke, parameters)
        stage = "current administrator self-revocation"
        _, result, cookies = api.request("POST", "/admin/v1/sessions/" + admin.identifier + "/revoke", admin, body={})
        revocation(result, admin, user_id, True)
        cookie = cookies.get("goby_session")
        check(cookie is not None and cookie.value == "" and cookie["path"] == "/admin" and cookie["max-age"] == "0" and
              cookie["httponly"] and cookie["samesite"].lower() == "strict", "Self-revocation did not clear the administrator cookie")
        api.request("GET", "/admin/v1/sessions", admin, expected=(401,))
        api.request("GET", "/emby/Users/" + user_id, sibling)
        report["checks"] = {"native_fields": 16, "utc_timestamps": True, "no_token_or_hash_fields": True,
                            "kind_status_device_user_filters": True, "current_session_identity": True,
                            "emby_native_denial": True, "target_only_revoke": True, "repeat_timestamp_unchanged": True,
                            "target_status": 401, "sibling_status": 200, "self_clear_cookie_and_denial": True}
        report["status"] = "passed"
    except BaseException as error:
        report["failed_stage"] = stage
        report["error"] = str(error) if isinstance(error, Failure) else "A protected operation failed; exception details were withheld"
    finally:
        if api is not None:
            for login in logins:
                if not login.attempted:
                    continue
                if not login.token:
                    report["cleanup"]["errors"].append("An attempted login lost its credential; its outcome and cleanup cannot be proven")
                    continue
                try:
                    if login.kind == "admin" and not login.csrf:
                        status, value, _ = api.request("GET", "/admin/v1/session", login, expected=(200, 401))
                        if status == 200:
                            login.csrf = value.get("CSRFToken", "")
                            check(isinstance(login.csrf, str) and HASH.fullmatch(login.csrf), "Administrator cleanup could not recover CSRF")
                    api.request("DELETE" if login.kind == "admin" else "POST",
                                "/admin/v1/session" if login.kind == "admin" else "/emby/Sessions/Logout", login,
                                expected=(204, 401) if login.kind == "admin" else (200, 401), parse=False)
                except BaseException:
                    report["cleanup"]["errors"].append("A normal logout could not be confirmed; final database evidence is required")
            report["http_method_counts"] = api.counts
        if database is not None and before is not None:
            try:
                final = snapshot(utilities, database)
                report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                created = preserve(utilities, before, final)
                report["all_preexisting_rows_unchanged"] = True
                digests = {"\\x" + hashlib.sha256(login.token.encode("ascii")).hexdigest()
                           for login in logins if login.token}
                check(len(created) == len(digests) and all(row["token_hash"] in digests and row["revoked_at"] is not None
                                                           for row in created.values()), "Not every new session is attributable and revoked")
                check(all(not login.attempted or login.token for login in logins), "A login response was lost without recoverable credentials")
                for key, original in known.items():
                    final_row = dict(created[key])
                    timestamp(final_row["revoked_at"])
                    final_row["revoked_at"] = None
                    check(final_row == original, "Cleanup changed a new session field other than revocation")
                report["cleanup"].update({"proven": True, "created_sessions": len(created), "all_new_sessions_revoked": True})
                if report["status"] == "passed":
                    check(len(created) == len(known) == 3, "The successful run did not retain exactly its three revoked logins")
            except BaseException:
                report["cleanup"]["errors"].append("Complete final preservation or session cleanup could not be proven")
        if accepted_deployment is not None:
            try:
                check(deployment(accepted) == accepted_deployment, "The deployed process or executable changed")
                report["deployment_unchanged"] = True
            except BaseException:
                report["cleanup"]["errors"].append("The final deployment identity could not be proven unchanged")
        if not report["cleanup"]["proven"] or report["cleanup"]["errors"]:
            report["status"] = "failed"
        if parent_identity is not None:
            try:
                info = ROOT.lstat()
                check((info.st_dev, info.st_ino) == parent_identity and info.st_uid == 0 and info.st_mode & 0o022 == 0,
                      "The fixed report directory changed")
                utilities.private_write(RESULT, report)
            except BaseException:
                report = {"owner": OWNER, "status": "failed", "failed_stage": "private report persistence",
                          "error": "The exclusive result could not be saved; no retry or overwrite was attempted"}
        print(json.dumps(report, ensure_ascii=True, sort_keys=True))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
