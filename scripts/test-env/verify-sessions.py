#!/usr/bin/env python3
"""Verify login-session administration against one disposable Linux database.

The shared browser runner owns the role, database, exact temporary HBA entry,
UID-995 application, browser, and cleanup. This scenario adds bounded historical
fixtures and real independent logins; it never writes a shared media catalog.
"""

import argparse
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import sys
import urllib.error
import urllib.parse
import urllib.request


FIXTURE_MARKER = "goby-session-browser-fixtures-v1"


def load_core(path):
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_session_browser_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def create_runner(core, args):
    core.MARKER = "goby-sessions-browser-v1"
    core.RUN_ENV = "GOBY_SESSIONS_RUN_ID"

    class SessionsRunner(core.Runner):
        browser_spec = "session-management.spec.ts"
        browser_timeout_seconds = 300
        screenshot_names = (
            "sessions-desktop.png", "sessions-mobile.png",
            "sessions-revoke-mobile.png", "sessions-signed-out.png",
        )

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5c_sessions_" + self.run_id
            self.role = "goby_m5c_role_" + self.run_id
            self.pg_app_name = "goby_m5c_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-sessions-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-sessions-" + self.run_id)
            self.admin_name = "m5c-admin-" + self.run_id
            self.member_name = "m5c-member-" + self.run_id
            self.member_password = secrets.token_urlsafe(32)
            self.disabled_password = secrets.token_urlsafe(32)
            self.secrets.extend((self.member_password, self.disabled_password))
            self.admin_opener = urllib.request.build_opener(
                urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            self.control_csrf = None
            self.fixture = None
            self.report.update({"scenario": "administrator_login_sessions", "database": self.database, "role": self.role})

        def allocate(self):
            super().allocate()
            core.require(self.goby.pw_uid == 995, "The isolated application account must have the reviewed UID 995.")
            core.require(os.fstat(self.binary_fd).st_mode & 0o001,
                         "The prepared binary must be executable by the unprivileged application account.")
            self.report["verifier_sha256"] = core.file_digest(Path(__file__).resolve())
            self.report["shared_runner_sha256"] = core.file_digest(args.core_runner.resolve())

        def prepare_files(self):
            super().prepare_files()
            # Asset directories are public application inputs. Keep them
            # readable under a restrictive caller umask without broadening
            # browser manifests, credentials, logs, or the private run output.
            assets = self.runtime / "admin"
            for directory in [assets, *(entry for entry in assets.rglob("*") if entry.is_dir())]:
                directory.chmod(0o755)
            (self.browser_work / "src").mkdir(mode=0o700)
            shutil.copyfile(self.args.snapshot / "web/admin/src/api.ts", self.browser_work / "src/api.ts")
            self.manifest = self.browser_work / "session-fixture.json"

        def http_json(self, route, *, body=None, headers=None, method=None, administrator=False, expected=(200, 201)):
            request_headers = {"Accept": "application/json", "Origin": self.origin}
            request_headers.update(headers or {})
            data = None if body is None else json.dumps(body).encode("utf-8")
            if data is not None:
                request_headers["Content-Type"] = "application/json"
            if administrator and data is not None and self.control_csrf:
                request_headers["X-CSRF-Token"] = self.control_csrf
            request = urllib.request.Request(self.origin + route, data=data, headers=request_headers, method=method)
            try:
                opener = self.admin_opener.open if administrator else urllib.request.urlopen
                with opener(request, timeout=5) as response:
                    core.require(response.status in expected, "An isolated session fixture request returned an unexpected status.")
                    raw = response.read(1024 * 1024 + 1)
                    core.require(len(raw) <= 1024 * 1024, "An isolated session response exceeded its byte budget.")
                    return json.loads(raw)
            except (OSError, ValueError, urllib.error.HTTPError):
                raise core.VerificationError("An isolated session fixture request failed; response details were not published.") from None

        def owned_database_sql(self, statement):
            core.require(self.database_oid is not None and self.lock is not None and
                         re.fullmatch(r"goby_m5c_sessions_[0-9]{8}_[0-9]{6}_[0-9a-f]{10}", self.database),
                         "Session fixture SQL requires this run's owned disposable database.")
            owner = core.pg_json(f"""SELECT jsonb_build_object('oid', oid::bigint,
                'owner', pg_get_userbyid(datdba), 'tag', shobj_description(oid, 'pg_database'))
                FROM pg_database WHERE datname = '{self.database}';""")
            core.require(owner == {"oid": self.database_oid, "owner": self.role, "tag": self.tag},
                         "The session fixture database ownership changed.")
            environment = dict(core.BASE_ENV, PGPASSWORD=self.role_password)
            guarded = f"""BEGIN;
                DO $guard$ BEGIN
                    IF current_database() <> '{self.database}' OR current_user <> '{self.role}' THEN
                        RAISE EXCEPTION 'Unexpected session fixture database or role';
                    END IF;
                END $guard$;
                {statement}
                COMMIT;
            """
            return core.command([
                core.PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At",
                "-h", "127.0.0.1", "-p", str(core.PG_PORT), "-U", self.role, "-d", self.database,
            ], text=guarded, environment=environment)

        def create_user(self, name, password):
            user = self.http_json("/admin/v1/users", administrator=True, body={
                "Name": name, "Password": password, "IsAdministrator": False,
            }, expected=(201,))["User"]
            core.require(user.get("Name") == name and re.fullmatch(r"[0-9a-f]{32}", user.get("Id", "")),
                         "The isolated member identity was not created correctly.")
            return {"Id": user["Id"], "Name": user["Name"]}

        def real_login(self, member, client, device_id):
            result = self.http_json("/emby/Users/AuthenticateByName", body={
                "Username": member["Name"], "Pw": self.member_password,
            }, headers={"Authorization": f'Emby Client="{client}", DeviceId="{device_id}", Device="Shared test device", Version="5.3-session"'})
            token = result.get("AccessToken", "")
            identifier = result.get("SessionInfo", {}).get("Id", "")
            core.require(isinstance(token, str) and len(token) >= 32 and
                         re.fullmatch(r"[0-9a-f]{32}", identifier) and result.get("User", {}).get("Id") == member["Id"],
                         "A normal Emby login did not return its independent session identity.")
            self.secrets.append(token)
            return {"Id": identifier, "Token": token, "Client": client, "DeviceId": device_id,
                    "DeviceName": "Shared test device"}

        def bootstrap(self):
            super().bootstrap()
            control = self.http_json("/admin/v1/session", administrator=True, body={
                "Name": self.admin_name, "Password": self.admin_password,
            })
            self.control_csrf = control.get("CSRFToken")
            administrator = control.get("User", {})
            core.require(isinstance(self.control_csrf, str) and self.control_csrf and
                         re.fullmatch(r"[0-9a-f]{32}", administrator.get("Id", "")),
                         "The fixture control administrator did not receive a valid session.")
            self.secrets.append(self.control_csrf)
            member = self.create_user(self.member_name, self.member_password)
            disabled = self.create_user("m5c-disabled-" + self.run_id, self.disabled_password)
            user_route = "/admin/v1/users/" + disabled["Id"]
            managed = self.http_json(user_route, administrator=True)["User"]
            updated = self.http_json(user_route, administrator=True, method="PUT", body={
                "Revision": managed["Revision"], "Name": managed["Name"], "IsAdministrator": False,
                "IsDisabled": True, "Policy": managed["Policy"],
            })
            core.require(updated["User"]["IsDisabled"] is True and updated["CurrentSessionRevoked"] is False,
                         "The dedicated disabled member fixture could not be saved.")
            device_id = "session-shared-" + self.run_id
            target = self.real_login(member, "Session target client", device_id)
            sibling = self.real_login(member, "Session sibling client", device_id)
            core.require(target["Id"] != sibling["Id"] and target["Token"] != sibling["Token"],
                         "The fixture requires independent real logins on the same device.")
            control_rows = self.http_json("/admin/v1/sessions?Kind=admin&Status=active", administrator=True)["Items"]
            core.require(len(control_rows) == 1 and control_rows[0]["IsCurrent"] is True,
                         "The fresh fixture must have exactly one control administrator login.")
            self.control_session_id = control_rows[0]["Id"]
            # These records exercise history and paging without repeated bcrypt
            # calls. Their random stored digests have no corresponding issued
            # token, and every synthetic record is unauthorized from creation.
            rows = []
            for number in range(1, 61):
                identifier, stored_digest = secrets.token_hex(16), secrets.token_hex(32)
                client = "Archive 100%_\\ session" if number == 1 else "Archive client"
                revoked = "NULL" if number <= 52 else "now() - interval '20 days'"
                rows.append(f"('{identifier}', '{member['Id']}', decode('{stored_digest}', 'hex'), 'emby', "
                            f"'{client}', 'history-{number:03d}', 'Historical fixture', 'archive-5.3', "
                            f"now() - interval '40 days' - interval '{number} minutes', "
                            f"now() - interval '10 days', now() - interval '30 days', {revoked})")
            for user, kind, expires in ((disabled, "emby", "now() + interval '10 days'"),
                                        (member, "admin", "now() - interval '1 day'")):
                rows.append(f"('{secrets.token_hex(16)}', '{user['Id']}', decode('{secrets.token_hex(32)}', 'hex'), "
                            f"'{kind}', 'Disabled access fixture', 'disabled-{kind}', 'Historical fixture', 'archive-5.3', "
                            f"now() - interval '40 days', {expires}, now() - interval '30 days', NULL)")
            count = self.owned_database_sql("SET LOCAL standard_conforming_strings = on;\n"
                "INSERT INTO sessions (id, user_id, token_hash, kind, client_name, device_id, device_name, client_version, "
                "created_at, expires_at, last_seen_at, revoked_at) VALUES\n" + ",\n".join(rows) + ";\n"
                "SELECT count(*) FROM sessions WHERE client_version = 'archive-5.3';")
            core.require(count == "62", "The bounded historical session fixtures were not seeded completely.")
            self.fixture = {"Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                            "Administrator": {"Id": administrator["Id"], "Name": self.admin_name},
                            "Member": dict(member, Password=self.member_password), "DisabledUser": disabled,
                            "Target": target, "Sibling": sibling, "HistoricalCount": 62,
                            "ExpiredCount": 52, "RevokedCount": 8, "DisabledCount": 2,
                            "LiteralSearch": "100%_\\ ", "ReplacementClient": "Session replacement client"}
            core.private_write(self.manifest, (json.dumps(self.fixture, sort_keys=True) + "\n").encode())
            self.report["fixtures"] = {"users": 3, "real_independent_emby_logins": 2, "same_device_id": True,
                                       "historical_rows": 62, "expired": 52, "revoked": 8, "disabled": 2,
                                       "historical_tokens_issued": False, "private_manifest": True}

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_SESSIONS_DISPOSABLE_DATABASE": "1",
                                "GOBY_SMOKE_SESSIONS_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_SESSIONS_FIXTURE_MANIFEST": str(self.manifest)})
            return environment

        def token_status(self, token):
            request = urllib.request.Request(self.origin + "/emby/Users/" + self.fixture["Member"]["Id"] + "/Views",
                                             headers={"X-Emby-Token": token})
            try:
                with urllib.request.urlopen(request, timeout=5) as response:
                    return response.status
            except urllib.error.HTTPError as error:
                return error.code
            except OSError:
                raise core.VerificationError("A persisted login authorization check could not reach the isolated server.") from None

        def persisted_revocations(self):
            core.require(self.token_status(self.fixture["Target"]["Token"]) == 401 and
                         self.token_status(self.fixture["Sibling"]["Token"]) == 200,
                         "Revocation did not preserve the independent login on the same device.")
            result = self.http_json("/admin/v1/sessions?" + urllib.parse.urlencode({
                "Status": "all", "UserId": self.fixture["Member"]["Id"], "DeviceId": self.fixture["Target"]["DeviceId"],
            }), administrator=True)
            target = next((item for item in result["Items"] if item["Id"] == self.fixture["Target"]["Id"]), None)
            core.require(target is not None and target["Status"] == "revoked" and target["RevokedAt"],
                         "The selected login did not retain its revoked record.")
            core.require(len(result["Items"]) == 3 and any(item["Client"] == self.fixture["ReplacementClient"]
                         and item["Status"] == "active" for item in result["Items"]),
                         "The later normal login on the same device was not preserved.")
            admin_rows = self.http_json("/admin/v1/sessions?" + urllib.parse.urlencode({
                "Kind": "admin", "Status": "all", "UserId": self.fixture["Administrator"]["Id"],
            }), administrator=True)["Items"]
            browser_rows = [item for item in admin_rows if item["Id"] != self.control_session_id]
            core.require(len(browser_rows) == 1 and browser_rows[0]["Status"] == "revoked" and browser_rows[0]["RevokedAt"],
                         "The browser administrator did not revoke only its own login.")
            return (target["RevokedAt"], browser_rows[0]["Id"], browser_rows[0]["RevokedAt"])

        def account_snapshot(self):
            # The full rows remain private in memory. Only equality is reported;
            # neither token/password digests nor the database dump are emitted.
            statement = """SELECT jsonb_build_object(
                'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
                'sessions', (SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s),
                'libraries', (SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
                'items', (SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
                'settings', (SELECT jsonb_agg(to_jsonb(c) ORDER BY key) FROM server_settings c))::text;"""
            return core.digest(self.owned_database_sql(statement).encode())

        def verify_restart(self):
            revocations = self.persisted_revocations()
            before = self.account_snapshot()
            self.stop_app()
            self.start_app()
            core.require(core.request_json(self.origin, "/admin/v1/bootstrap") == {"Initialized": True},
                         "The session verification restart lost its initialized state.")
            core.require(self.account_snapshot() == before, "Restart changed persisted session, account, or catalog state.")
            core.require(self.persisted_revocations() == revocations, "Restart changed a durable login revocation.")
            self.report["checks"].update({"restart_preserved_sessions_accounts_and_catalog": True,
                                          "revoked_token_denied_after_restart": True,
                                          "same_device_sibling_authorized_after_restart": True,
                                          "browser_self_revocation_persisted": True})

    return SessionsRunner(args)


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
        raise core.VerificationError("The session verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Session verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
