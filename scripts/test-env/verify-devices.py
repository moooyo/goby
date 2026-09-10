#!/usr/bin/env python3
"""Verify device administration in one owned disposable Linux database.

Every device and credential is issued through the application's real APIs. The
shared runner owns the temporary PostgreSQL role, database, exact HBA entry,
unprivileged process, private artifacts, and cleanup. No shared service changes.
"""

import argparse
import http.client
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import sys
import urllib.error
import urllib.request


FIXTURE_MARKER = "goby-device-browser-fixtures-v1"
RESULT_MARKER = "goby-device-browser-result-v1"
SAFE_FIELDS = {"Id", "Revision", "ReportedDeviceId", "Name", "ReportedName", "CustomName", "AppName",
               "AppVersion", "LastUserId", "LastUserName", "CreatedAt", "LastSeenAt", "IpAddress", "ActiveLoginCount"}


def load_core(path):
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_device_browser_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


class LoopbackSourceHandler(urllib.request.HTTPHandler):
    def __init__(self, address):
        super().__init__()
        self.address = address

    def http_open(self, request):
        def connection(host, **options):
            return http.client.HTTPConnection(host, source_address=(self.address, 0), **options)
        return self.do_open(connection, request)


def create_runner(core, args):
    core.MARKER = "goby-devices-browser-v1"
    core.RUN_ENV = "GOBY_DEVICES_RUN_ID"

    class DevicesRunner(core.Runner):
        browser_spec = "device-management.spec.ts"
        browser_timeout_seconds = 300
        screenshot_names = ("devices-desktop.png", "devices-rename-desktop.png",
                            "devices-mobile.png", "devices-remove-mobile.png")

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5e_devices_" + self.run_id
            self.role = "goby_m5e_role_" + self.run_id
            self.pg_app_name = "goby_m5e_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-devices-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-devices-" + self.run_id)
            self.admin_name = "m5e-admin-" + self.run_id
            self.member_name = "m5e-member-" + self.run_id
            self.member_password = secrets.token_urlsafe(32)
            self.secrets.append(self.member_password)
            self.cookies = http.cookiejar.CookieJar()
            self.admin_opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
            self.control_csrf = None
            self.fixture = None
            self.browser_result = None
            self.report.update({"scenario": "administrator_devices", "database": self.database, "role": self.role})

        def allocate(self):
            super().allocate()
            core.require(self.goby.pw_uid == 995 and os.fstat(self.binary_fd).st_mode & 0o001,
                         "The isolated executable must be runnable by the reviewed UID 995.")
            core.require(core.free_bytes(core.EXEC_ROOT) >= 128 * 1024 * 1024 and
                         core.free_bytes(Path("/dev/shm")) >= 96 * 1024 * 1024,
                         "Insufficient headroom for the device browser, database, and screenshots.")
            self.report["verifier_sha256"] = core.file_digest(Path(__file__).resolve())
            self.report["shared_runner_sha256"] = core.file_digest(args.core_runner.resolve())

        def prepare_files(self):
            super().prepare_files()
            assets = self.runtime / "admin"
            for directory in [assets, *(entry for entry in assets.rglob("*") if entry.is_dir())]:
                directory.chmod(0o755)
            self.manifest = self.browser_work / "device-fixture.json"
            self.result_path = self.browser_work / "device-result.json"

        def http_json(self, route, *, body=None, headers=None, administrator=False, expected=(200, 201), source_address=None):
            request_headers = {"Accept": "application/json", "Origin": self.origin}
            request_headers.update(headers or {})
            data = None if body is None else json.dumps(body).encode("utf-8")
            if data is not None:
                request_headers["Content-Type"] = "application/json"
            if administrator and data is not None and self.control_csrf:
                request_headers["X-CSRF-Token"] = self.control_csrf
            request = urllib.request.Request(self.origin + route, data=data, headers=request_headers)
            try:
                if source_address is not None:
                    core.require(not administrator and re.fullmatch(r"127\.0\.0\.(?:[2-9]|[12][0-9]|3[0-3])", source_address),
                                 "Fixture source addresses must remain on the owned loopback range.")
                    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), LoopbackSourceHandler(source_address)).open
                else:
                    opener = self.admin_opener.open if administrator else urllib.request.urlopen
                with opener(request, timeout=5) as response:
                    core.require(response.status in expected, "An isolated device request returned an unexpected status.")
                    raw = response.read(1024 * 1024 + 1)
                    core.require(len(raw) <= 1024 * 1024, "An isolated device response exceeded its byte budget.")
                    return json.loads(raw)
            except (OSError, ValueError, urllib.error.HTTPError):
                raise core.VerificationError("An isolated device request failed; response details were not published.") from None

        def real_login(self, member, number):
            reported_id = "goby-dashboard" if number == 29 else "device-browser-" + self.run_id + f"-{number:02d}"
            reported_name = "Archive 100%_\\ display" if number == 28 else f"Browser display {number:02d}"
            client = "Device browser client"
            result = self.http_json("/emby/Users/AuthenticateByName", body={
                "Username": member["Name"], "Pw": self.member_password,
            }, headers={"X-Emby-Client": client, "X-Emby-Device-Id": reported_id,
                        "X-Emby-Device-Name": reported_name, "X-Emby-Client-Version": "device-browser-1"},
                                     source_address=f"127.0.0.{number + 2}")
            token = result.get("AccessToken", "")
            if isinstance(token, str) and token:
                self.secrets.append(token)
            login_id = result.get("SessionInfo", {}).get("Id", "")
            core.require(isinstance(token, str) and len(token) >= 32 and re.fullmatch(r"[0-9a-f]{32}", login_id)
                         and result.get("User", {}).get("Id") == member["Id"],
                         "A real fixture login did not return the expected user and independent credential.")
            return {"SessionId": login_id, "Token": token, "ReportedDeviceId": reported_id,
                    "ReportedName": reported_name, "AppName": client, "AppVersion": "device-browser-1"}

        def list_devices(self):
            result = self.http_json("/admin/v1/devices?StartIndex=0&Limit=200", administrator=True)
            core.require(set(result) == {"Items", "TotalRecordCount", "StartIndex", "Limit"}
                         and result["StartIndex"] == 0 and result["Limit"] == 200
                         and isinstance(result["Items"], list) and result["TotalRecordCount"] == len(result["Items"])
                         and all(set(item) == SAFE_FIELDS for item in result["Items"]),
                         "The device list lost its exact safe metadata or coherent count.")
            core.require(not any(secret in json.dumps(result) for secret in self.secrets),
                         "A device metadata response exposed an authentication secret.")
            return result["Items"]

        def bootstrap(self):
            super().bootstrap()
            control = self.http_json("/admin/v1/session", administrator=True, body={
                "Name": self.admin_name, "Password": self.admin_password,
            })
            self.control_csrf = control.get("CSRFToken")
            administrator = control.get("User", {})
            core.require(isinstance(self.control_csrf, str) and self.control_csrf and
                         re.fullmatch(r"[0-9a-f]{32}", administrator.get("Id", "")),
                         "The dedicated administrator did not receive a control session.")
            self.secrets.append(self.control_csrf)
            self.secrets.extend(cookie.value for cookie in self.cookies)
            core.require(self.list_devices() == [], "Native dashboard login incorrectly registered a physical device.")
            member = self.http_json("/admin/v1/users", administrator=True, body={
                "Name": self.member_name, "Password": self.member_password, "IsAdministrator": False,
            }, expected=(201,))["User"]
            core.require(member.get("Name") == self.member_name and re.fullmatch(r"[0-9a-f]{32}", member.get("Id", "")),
                         "The dedicated device fixture member was not created correctly.")
            logins = [self.real_login(member, number) for number in range(32)]
            second_target = self.real_login(member, 31)
            devices = self.list_devices()
            core.require(len(devices) == 32 and len({item["Id"] for item in devices}) == 32,
                         "Real Emby logins did not create exactly 32 distinct ordinary devices.")
            for number, login in enumerate(logins):
                matching = [item for item in devices if item["ReportedDeviceId"] == login["ReportedDeviceId"]]
                core.require(len(matching) == 1 and matching[0]["ReportedName"] == login["ReportedName"]
                             and matching[0]["AppName"] == login["AppName"] and matching[0]["LastUserId"] == member["Id"]
                             and matching[0]["IpAddress"] == f"127.0.0.{number + 2}"
                             and matching[0]["ActiveLoginCount"] == (2 if number == 31 else 1),
                             "The real device registry lost its client metadata or authorized-login count.")
                login["Id"] = matching[0]["Id"]
            core.require(second_target["SessionId"] != logins[31]["SessionId"] and
                         second_target["Token"] != logins[31]["Token"],
                         "The target device must have two independently issued ordinary logins.")
            second_target["Id"] = logins[31]["Id"]
            self.fixture = {"Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                            "Administrator": {"Id": administrator["Id"], "Name": self.admin_name},
                            "Member": {"Id": member["Id"], "Name": self.member_name, "Password": self.member_password},
                            "SeedCount": 32, "Devices": logins, "Target": logins[31], "SecondTarget": second_target,
                            "Sibling": logins[30], "DashboardCollision": logins[29], "Missing": logins[27],
                            "LiteralName": logins[28]["ReportedName"], "LiteralSearch": "100%_\\",
                            "PersistedName": "Study \u754c player"}
            core.private_write(self.manifest, (json.dumps(self.fixture, sort_keys=True) + "\n").encode())
            self.report["fixtures"] = {"users": 2, "real_devices": 32, "real_emby_logins": 33,
                                       "target_logins": 2, "reported_dashboard_id_collision": True,
                                       "real_loopback_source_addresses": 32, "login_limit_configuration_unchanged": True,
                                       "private_manifest": True, "synthetic_credentials": False}

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_DEVICES_DISPOSABLE_DATABASE": "1",
                                "GOBY_SMOKE_DEVICES_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_DEVICES_FIXTURE_MANIFEST": str(self.manifest)})
            return environment

        def load_browser_result(self):
            value = json.loads(core.private_file(self.result_path))
            core.require(value.get("Marker") == RESULT_MARKER and value.get("RunId") == self.run_id,
                         "The private device browser result does not match this run.")
            # Record newly issued secrets before checking any later result
            # assertion, including a partially completed browser journey.
            replacement = value.get("ReplacementLogin")
            if isinstance(replacement, dict) and isinstance(replacement.get("Token"), str) and replacement["Token"]:
                self.secrets.append(replacement["Token"])
            browser_secrets = value.get("BrowserSecrets", [])
            core.require(isinstance(browser_secrets, list) and len(browser_secrets) <= 8
                         and all(isinstance(secret, str) and 0 < len(secret) <= 4096 for secret in browser_secrets),
                         "The private browser session secret inventory is invalid.")
            self.secrets.extend(browser_secrets)
            self.browser_result = value

        def run_browser(self):
            try:
                super().run_browser()
            finally:
                try:
                    if self.result_path.exists():
                        self.load_browser_result()
                finally:
                    self.secrets.extend(cookie.value for cookie in self.cookies if cookie.value not in self.secrets)
                    for name in ("browser_driver_stdout", "browser_driver_stderr"):
                        if name in self.report:
                            self.report[name] = core.sanitize_text(self.report[name], self.secrets)
                    browser = self.report.get("browser", {})
                    browser["failures"] = [core.sanitize_text(value, self.secrets) for value in browser.get("failures", [])]
            value = self.browser_result
            core.require(value is not None and value.get("Complete") is True and value.get("ExpectedCount") == 25
                         and isinstance(value.get("Removed"), list) and len(value["Removed"]) == 8
                         and len({item["Id"] for item in value["Removed"]}) == 8
                         and re.fullmatch(r"[0-9a-f]{32}", value.get("BrowserSessionId", "")),
                         "The browser did not complete real device administration and bounded page cleanup.")
            replacement = value.get("ReplacementLogin", {})
            core.require(re.fullmatch(r"[1-9][0-9]*", replacement.get("Id", ""))
                         and isinstance(replacement.get("Token"), str) and len(replacement["Token"]) >= 32
                         and replacement["Id"] != self.fixture["Target"]["Id"],
                         "A later real login did not register a new device generation.")
            core.require(all((self.output / name).is_file() for name in self.screenshot_names),
                         "The browser did not produce all four explicit secret-free screenshots.")
            self.assert_private_logs()
            self.report["checks"].update({"browser_native_device_journey": True, "initial_page_size": 25,
                                          "literal_search_and_utf8_bounds": True,
                                          "real_revision_conflict_and_missing_device_recovery": True,
                                          "committed_response_loss_requires_explicit_refresh": True,
                                          "removal_clamps_empty_final_page": True,
                                          "target_removal_revokes_two_independent_logins": True,
                                          "later_login_registers_new_device_generation": True,
                                          "native_cookie_survives_reported_id_collision_removal": True,
                                          "secret_free_screenshots": 4})

        def assert_private_logs(self):
            for name in ("app-private.log", "browser-private.json", "browser-private.stderr"):
                path = self.output / name
                core.require(path.stat().st_size <= 8 * 1024 * 1024, "A private verification log exceeded its byte budget.")
                value = path.read_text(encoding="utf-8", errors="replace")
                core.require(not any(secret and secret in value for secret in self.secrets),
                             "A verification log contained a raw authentication secret; it will not be exported.")
            self.report["checks"]["private_logs_exclude_raw_secrets"] = True

        def token_status(self, token):
            request = urllib.request.Request(self.origin + "/emby/Users/" + self.fixture["Member"]["Id"] + "/Views",
                                             headers={"X-Emby-Token": token})
            try:
                with urllib.request.urlopen(request, timeout=5) as response:
                    return response.status
            except urllib.error.HTTPError as error:
                return error.code
            except OSError:
                raise core.VerificationError("A persisted device login check could not reach the isolated server.") from None

        def persisted_state(self):
            devices = self.list_devices()
            result = self.browser_result
            expected_ids = result["ExpectedDeviceIds"]
            core.require(len(devices) == result["ExpectedCount"] and
                         sorted(item["Id"] for item in devices) == sorted(expected_ids),
                         "The device registry changed its exact surviving fixture generations.")
            sibling = next((item for item in devices if item["Id"] == self.fixture["Sibling"]["Id"]), None)
            core.require(sibling is not None and sibling["CustomName"] == self.fixture["PersistedName"]
                         and sibling["Name"] == self.fixture["PersistedName"]
                         and sibling["Revision"] == result["PersistedRevision"],
                         "The administrator's saved custom name or revision was not preserved.")
            removed_ids = {item["Id"] for item in result["Removed"]}
            for login in self.fixture["Devices"] + [self.fixture["SecondTarget"]]:
                expected = 401 if login["Id"] in removed_ids else 200
                core.require(self.token_status(login["Token"]) == expected,
                             "An ordinary device login did not retain its expected authorization state.")
            core.require(self.token_status(result["ReplacementLogin"]["Token"]) == 200,
                         "The replacement device login lost authorization.")
            sessions = self.http_json("/admin/v1/sessions?Kind=admin&Status=active&Limit=200", administrator=True)
            core.require(sessions.get("TotalRecordCount") == 2 and
                         any(item["Id"] == result["BrowserSessionId"] for item in sessions["Items"]),
                         "Removing an ordinary device invalidated an independent native administrator login.")
            browser_cookie = result.get("BrowserCookie", "")
            core.require(isinstance(browser_cookie, str) and
                         re.fullmatch(r"goby_session=[A-Za-z0-9_-]{32,4096}", browser_cookie),
                         "The persisted browser administrator cookie is invalid.")
            browser_session = self.http_json("/admin/v1/session", headers={"Cookie": browser_cookie})
            core.require(browser_session.get("User", {}).get("Id") == self.fixture["Administrator"]["Id"]
                         and browser_session.get("CSRFToken") == result["BrowserSecrets"][0],
                         "The exact browser cookie no longer authorizes its original administrator session.")
            for removed in result["Removed"]:
                repeated = self.http_json("/admin/v1/devices/" + removed["Id"] + "/delete", administrator=True,
                                          body={"Revision": removed["Revision"]}, expected=(200,))
                core.require(repeated == {"Id": removed["Id"], "DeletedAt": removed["DeletedAt"], "RevokedLoginCount": 0},
                             "A removed device did not retain its exact idempotent deletion receipt.")
            fields = ("Id", "Revision", "Name", "ReportedName", "CustomName", "ReportedDeviceId", "AppName", "AppVersion", "CreatedAt")
            return sorted(tuple(item[field] for field in fields) for item in devices)

        def owned_database_read(self, statement):
            core.require(self.database_oid is not None and self.lock is not None and
                         re.fullmatch(r"goby_m5e_devices_[0-9]{8}_[0-9]{6}_[0-9a-f]{10}", self.database),
                         "Device persistence inspection requires this run's owned database.")
            owner = core.pg_json(f"""SELECT jsonb_build_object('oid', oid::bigint, 'owner', pg_get_userbyid(datdba),
                'tag', shobj_description(oid, 'pg_database')) FROM pg_database WHERE datname = '{self.database}';""")
            core.require(owner == {"oid": self.database_oid, "owner": self.role, "tag": self.tag},
                         "The owned device database identity changed.")
            guarded = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
                DO $guard$ BEGIN
                    IF current_database() <> '{self.database}' OR current_user <> '{self.role}' THEN
                        RAISE EXCEPTION 'Unexpected device fixture database or role';
                    END IF;
                END $guard$;
                {statement}
                COMMIT;
            """
            return core.command([core.PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At",
                                 "-h", "127.0.0.1", "-p", str(core.PG_PORT), "-U", self.role, "-d", self.database],
                                text=guarded, environment=dict(core.BASE_ENV, PGPASSWORD=self.role_password))

        def full_private_snapshot(self):
            tables = json.loads(self.owned_database_read(
                "SELECT jsonb_agg(tablename ORDER BY tablename) FROM pg_tables WHERE schemaname = 'public';"))
            core.require(isinstance(tables, list) and tables and
                         all(re.fullmatch(r"[a-z_][a-z0-9_]*", name) for name in tables),
                         "The disposable database table inventory is invalid.")
            selections = [f"SELECT '{name}' AS name, COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) "
                          f"FROM public.\"{name}\" r), '[]'::jsonb) AS rows" for name in tables]
            value = self.owned_database_read("SELECT jsonb_object_agg(name, rows ORDER BY name)::text FROM (" +
                                             " UNION ALL ".join(selections) + ") AS contents;")
            self.report["checks"]["restart_snapshot_public_table_count"] = len(tables)
            return core.digest(value.encode())

        def verify_restart(self):
            before = self.persisted_state()
            self.stop_app()
            snapshot = self.full_private_snapshot()
            self.start_app()
            core.require(self.full_private_snapshot() == snapshot,
                         "Restart changed persisted device, credential, account, or catalog rows.")
            core.require(core.request_json(self.origin, "/admin/v1/bootstrap") == {"Initialized": True}
                         and self.persisted_state() == before,
                         "Restart changed native device names, identities, revocations, or administrator authorization.")
            self.assert_private_logs()
            self.report["checks"].update({"restart_preserved_all_public_table_rows": True,
                                          "restart_preserved_device_names_and_revisions": True,
                                          "removed_device_logins_denied_after_restart": True,
                                          "retained_and_replacement_logins_authorized_after_restart": True,
                                          "native_administrator_logins_preserved_after_restart": True,
                                          "deletion_receipts_remain_idempotent_after_restart": True,
                                          "remaining_devices": self.browser_result["ExpectedCount"]})

    return DevicesRunner(args)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--core-runner", type=Path, default=Path(__file__).with_name("verify-managed-users.py"))
    args = parser.parse_args()
    core = load_core(args.core_runner)
    core.require(re.fullmatch(r"[0-9a-f]{64}", args.binary_sha256) is not None, "Supply the prepared binary SHA-256.")
    def interrupted(signum, frame):
        raise core.VerificationError("The device verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Device verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
