#!/usr/bin/env python3
"""Verify application-key administration in one owned disposable Linux database.

The shared runner owns the port-15432 role, exact HBA restoration, unprivileged
application, private browser artifacts, and cleanup. Every fixture credential is
issued by the application. The master key and issued secrets remain private and
are removed with this run's database and runtime; no shared service is changed.
"""

import argparse
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import stat
import sys
import urllib.error
import urllib.request


FIXTURE_MARKER = "goby-application-key-browser-fixtures-v1"
RESULT_MARKER = "goby-application-key-browser-result-v1"
SAFE_FIELDS = {"Id", "AppName", "CreatedAt", "LastUsedAt", "RevokedAt", "CreatedBy", "IPAddress", "Status"}


def load_core(path):
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_application_key_browser_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def create_runner(core, args):
    core.MARKER = "goby-application-keys-browser-v1"
    core.RUN_ENV = "GOBY_APPLICATION_KEYS_RUN_ID"

    class ApplicationKeysRunner(core.Runner):
        browser_spec = "application-keys.spec.ts"
        browser_timeout_seconds = 240
        screenshot_names = (
            "application-keys-desktop.png", "application-keys-mobile.png",
            "application-keys-created-masked.png", "application-keys-revoke-mobile.png",
        )

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5d_keys_" + self.run_id
            self.role = "goby_m5d_role_" + self.run_id
            self.pg_app_name = "goby_m5d_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-application-keys-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-application-keys-" + self.run_id)
            self.vault = self.runtime / "vault"
            self.master_file = self.vault / "master.key"
            core.BASE_ENV["GOBY_API_KEY_MASTER_KEY_FILE"] = str(self.master_file)
            self.admin_name = "m5d-admin-" + self.run_id
            self.cookies = http.cookiejar.CookieJar()
            self.admin_opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))
            self.control_csrf = None
            self.fixture = None
            self.browser_result = None
            self.report.update({"scenario": "administrator_application_keys", "database": self.database, "role": self.role})

        def allocate(self):
            super().allocate()
            core.require(self.goby.pw_uid == 995, "The isolated application account must have the reviewed UID 995.")
            core.require(os.fstat(self.binary_fd).st_mode & 0o001,
                         "The prepared binary must be executable by the unprivileged application account.")
            self.report["verifier_sha256"] = core.file_digest(Path(__file__).resolve())
            self.report["shared_runner_sha256"] = core.file_digest(args.core_runner.resolve())

        def prepare_files(self):
            super().prepare_files()
            # Public assets must remain readable even under the operator's
            # umask 077. The runtime stays owned by the application account.
            assets = self.runtime / "admin"
            for directory in [assets, *(entry for entry in assets.rglob("*") if entry.is_dir())]:
                directory.chmod(0o755)
            self.vault.mkdir(mode=0o700)
            os.chown(self.vault, self.goby.pw_uid, self.goby.pw_gid)
            self.vault.chmod(0o700)
            self.manifest = self.browser_work / "application-key-fixture.json"
            self.result_path = self.browser_work / "application-key-result.json"

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
                    core.require(response.status in expected, "An isolated application-key request returned an unexpected status.")
                    raw = response.read(1024 * 1024 + 1)
                    core.require(len(raw) <= 1024 * 1024, "An isolated application-key response exceeded its byte budget.")
                    return json.loads(raw)
            except (OSError, ValueError, urllib.error.HTTPError):
                raise core.VerificationError("An isolated application-key request failed; response details were not published.") from None

        def create_key(self, name):
            created = self.http_json("/admin/v1/api-keys", administrator=True, body={"AppName": name}, expected=(201,))
            key, token = created.get("Key", {}), created.get("AccessToken", "")
            core.require(set(created) == {"Key", "AccessToken"} and set(key) == SAFE_FIELDS and
                         re.fullmatch(r"[1-9][0-9]*", key.get("Id", "")) and key.get("AppName") == name and
                         key.get("Status") == "active" and key.get("RevokedAt") is None and
                         isinstance(token, str) and len(token) >= 32,
                         "A real fixture application key did not match the native creation contract.")
            self.secrets.append(token)
            return {"Id": key["Id"], "AppName": name, "Token": token}

        def master_identity(self):
            info = self.vault.lstat()
            core.require(stat.S_ISDIR(info.st_mode) and info.st_uid == 995 and
                         stat.S_IMODE(info.st_mode) == 0o700 and self.vault.resolve() == self.vault,
                         "The owned application-key vault directory is no longer private.")
            value = core.private_file(self.master_file, uid=995, maximum=32)
            core.require(len(value) == 32, "The master key must contain exactly 32 private bytes.")
            return core.digest(value)

        def bootstrap(self):
            super().bootstrap()
            control = self.http_json("/admin/v1/session", administrator=True, body={
                "Name": self.admin_name, "Password": self.admin_password,
            })
            self.control_csrf = control.get("CSRFToken")
            administrator = control.get("User", {})
            core.require(isinstance(self.control_csrf, str) and self.control_csrf and
                         re.fullmatch(r"[0-9a-f]{32}", administrator.get("Id", "")),
                         "The fixture administrator did not receive a valid control session.")
            self.secrets.append(self.control_csrf)
            self.secrets.extend(cookie.value for cookie in self.cookies)
            sibling = self.create_key("Browser sibling application")
            literal_name = "Archive 100%_\\ application"
            historical = []
            for number in range(31):
                key = self.create_key(literal_name if number == 0 else f"Archive application {number:02d}")
                if number >= 28:
                    revoked = self.http_json("/admin/v1/api-keys/" + key["Id"] + "/revoke",
                                             administrator=True, body={}, expected=(200,))
                    core.require(revoked.get("Id") == key["Id"] and revoked.get("RevokedAt"),
                                 "A fixture application key did not retain its real revocation.")
                    historical.append(key["Id"])
            self.master_before = self.master_identity()
            self.fixture = {"Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                            "Administrator": {"Id": administrator["Id"], "Name": self.admin_name},
                            "Sibling": sibling, "SeedCount": 32, "RevokedCount": 3,
                            "HistoricalRevokedIds": historical, "LiteralName": literal_name,
                            "LiteralSearch": "100%_\\", "TargetName": "Browser \u754c application"}
            core.private_write(self.manifest, (json.dumps(self.fixture, sort_keys=True) + "\n").encode())
            self.report["fixtures"] = {"users": 1, "real_keys_issued": 32, "historical_revoked_keys": 3,
                                       "active_keys": 29, "private_manifest": True, "synthetic_credentials": False}
            self.report["checks"].update({"master_key_bytes": 32, "master_key_mode": "0600",
                                          "master_key_uid": 995, "vault_directory_mode": "0700"})

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_APPLICATION_KEYS_DISPOSABLE_DATABASE": "1",
                                "GOBY_SMOKE_APPLICATION_KEYS_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_APPLICATION_KEYS_FIXTURE_MANIFEST": str(self.manifest)})
            return environment

        def load_browser_result(self):
            value = json.loads(core.private_file(self.result_path))
            core.require(value.get("Marker") == RESULT_MARKER and value.get("RunId") == self.run_id and
                         re.fullmatch(r"[1-9][0-9]*", value.get("TargetId", "")) and
                         isinstance(value.get("TargetToken"), str) and len(value["TargetToken"]) >= 32,
                         "The private application-key browser result does not match this run.")
            self.secrets.append(value["TargetToken"])
            self.browser_result = value
            return value

        def run_browser(self):
            try:
                super().run_browser()
            finally:
                # Learn a created token even when a later browser assertion
                # fails, before the shared cleanup publishes sanitized errors.
                if self.result_path.exists():
                    self.load_browser_result()
                for cookie in self.cookies:
                    if cookie.value not in self.secrets:
                        self.secrets.append(cookie.value)
                for key in ("browser_driver_stderr", "browser_driver_stdout"):
                    if key in self.report:
                        self.report[key] = core.sanitize_text(self.report[key], self.secrets)
                browser = self.report.get("browser", {})
                browser["failures"] = [core.sanitize_text(failure, self.secrets) for failure in browser.get("failures", [])]
            value = self.browser_result
            core.require(value is not None and value.get("Complete") is True and value.get("RevokedAt") and
                         re.fullmatch(r"[0-9a-f]{32}", value.get("AlphaSessionId", "")) and
                         re.fullmatch(r"[0-9a-f]{32}", value.get("BetaSessionId", "")) and
                         value["AlphaSessionId"] != value["BetaSessionId"],
                         "The browser did not complete distinct client contexts and key revocation.")
            self.assert_private_logs()
            self.report["checks"].update({"browser_native_management_journey": True,
                                          "distinct_userless_client_contexts": 2,
                                          "browser_secret_lifetime_and_storage": True,
                                          "masked_or_secret_free_screenshots": 4})

        def assert_private_logs(self):
            for name in ("app-private.log", "browser-private.json", "browser-private.stderr"):
                path = self.output / name
                core.require(path.stat().st_size <= 8 * 1024 * 1024, "A private verification log exceeded its byte budget.")
                value = path.read_text(encoding="utf-8", errors="replace")
                core.require(not any(secret and secret in value for secret in self.secrets),
                             "A verification log contained a raw authentication secret; it will not be exported.")
            self.report["checks"]["private_logs_exclude_raw_secrets"] = True

        def token_status(self, token, client):
            request = urllib.request.Request(self.origin + "/emby/Users", headers={
                "X-Emby-Token": token, "X-Emby-Client": client,
                "X-Emby-Device-Id": "browser-key-shared-device", "X-Emby-Device-Name": "Browser fixture",
            })
            try:
                with urllib.request.urlopen(request, timeout=5) as response:
                    return response.status
            except urllib.error.HTTPError as error:
                return error.code
            except OSError:
                raise core.VerificationError("A persisted application-key check could not reach the isolated server.") from None

        def persisted_state(self):
            result = self.http_json("/admin/v1/api-keys?IncludeRevoked=true&Limit=200", administrator=True)
            core.require(result.get("TotalRecordCount") == 33 and len(result.get("Items", [])) == 33 and
                         all(set(item) == SAFE_FIELDS for item in result["Items"]),
                         "The native list lost safe application-key metadata or its full fixture count.")
            serialized = json.dumps(result)
            core.require(not any(secret in serialized for secret in self.secrets),
                         "The native key list exposed an authentication secret.")
            value = self.browser_result
            target = next((item for item in result["Items"] if item["Id"] == value["TargetId"]), None)
            core.require(target is not None and target["Status"] == "revoked" and target["RevokedAt"] == value["RevokedAt"],
                         "The UI-selected key did not preserve its exact durable revocation.")
            historical = [item for item in result["Items"] if item["Id"] in self.fixture["HistoricalRevokedIds"]]
            core.require(len(historical) == 3 and all(item["Status"] == "revoked" and item["RevokedAt"] for item in historical),
                         "The isolated key history did not preserve every seeded revocation.")
            for client in ("Browser key alpha", "Browser key beta"):
                core.require(self.token_status(value["TargetToken"], client) == 401,
                             "A revoked parent key still authorized one of its client contexts.")
            sibling = self.fixture["Sibling"]
            core.require(self.token_status(sibling["Token"], "Browser key sibling") == 200,
                         "The independent sibling key lost authorization.")
            revealed = self.http_json("/admin/v1/api-keys/" + sibling["Id"] + "/reveal",
                                      administrator=True, body={}, expected=(200,))
            core.require(set(revealed) == {"Id", "AccessToken"} and revealed["Id"] == sibling["Id"] and
                         revealed["AccessToken"] == sibling["Token"],
                         "The encrypted sibling secret did not decrypt to the original issued key.")
            return [(item["Id"], item["AppName"], item["CreatedAt"], item["RevokedAt"], item["Status"]) for item in result["Items"]]

        def verify_restart(self):
            before = self.persisted_state()
            core.require(self.master_identity() == self.master_before, "The application changed its private master key.")
            self.stop_app()
            self.start_app()
            core.require(core.request_json(self.origin, "/admin/v1/bootstrap") == {"Initialized": True},
                         "The application-key restart lost its initialized state.")
            core.require(self.master_identity() == self.master_before and self.persisted_state() == before,
                         "Restart changed key metadata, encrypted-secret recovery, or durable revocation.")
            self.assert_private_logs()
            self.report["checks"].update({"restart_preserved_key_metadata": True,
                                          "restart_decrypted_original_sibling_key": True,
                                          "revoked_parent_denied_for_both_clients_after_restart": True,
                                          "original_sibling_token_authorized_after_restart": True,
                                          "restart_preserved_private_master_key": True,
                                          "native_list_excludes_secrets": True})

    return ApplicationKeysRunner(args)


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
        raise core.VerificationError("The application-key verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Application-key verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
