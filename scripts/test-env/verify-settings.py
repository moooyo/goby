#!/usr/bin/env python3
"""Verify settings administration and startup defaults in an owned Linux database.

The frozen managed-user Runner owns the scratch cluster lock, exact temporary
HBA rule, low-privilege database and role, tagged processes, and final cleanup.
Only this scenario's application receives the two explicit startup configurations.
"""

import argparse
import http.client
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import urllib.error
import urllib.request


FIXTURE_MARKER = "goby-settings-browser-fixtures-v1"
RESULT_MARKER = "goby-settings-browser-result-v1"
SETTINGS_FIELDS = ("ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels")
PUBLIC_TABLES = tuple(sorted("""application_key_clients application_key_devices
application_keys catalog_entities client_playback_references devices encoding_jobs
item_entities item_images item_metadata_state item_subtitles items libraries
library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings
sessions user_item_data users task_definitions task_occurrences task_run_children
task_run_requests task_runs task_triggers""".split()))
FINAL_OVERRIDES = {"ServerName": None, "MaxBitrate": 1234567, "MaxWidth": None,
                   "MaxHeight": 720, "MaxAudioChannels": None}
DEPLOYMENT = {"TranscodingEnabled": False, "HardwareDecoder": "software", "HardwareEncoder": "software",
              "Threads": 2, "MaxJobs": 2, "MaxUserJobs": 1, "MaxSessionJobs": 1}
SAFE_ROUTES = (
    (r"/admin/v1/session", "/admin/v1/session"),
    (r"/admin/v1/sessions", "/admin/v1/sessions"),
    (r"/admin/v1/sessions/[0-9a-f]{32}/revoke", "/admin/v1/sessions/{id}/revoke"),
    (r"/admin/v1/settings", "/admin/v1/settings"),
    (r"/admin/v1/settings/reset", "/admin/v1/settings/reset"),
    (r"/admin/v1/overview", "/admin/v1/overview"),
    (r"/emby/System/Info/Public", "/emby/System/Info/Public"),
)
SAFE_ERROR_CODES = frozenset((
    "access_denied", "administrator_required", "authentication_required", "csrf_invalid",
    "internal_error", "invalid_input", "not_found", "not_ready", "origin_denied", "rate_limited",
    "revision_conflict", "settings_unavailable", "unsupported_media_type",
))


def load_core(path):
    # Import read-only without invoking the frozen runner's CLI or emitting
    # bytecode into the prepared source snapshot.
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_settings_browser_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def valid_id(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{32}", value) is not None


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def safe_error_code(raw):
    if len(raw) > 4096:
        return None
    try:
        value = json.loads(raw)
    except (ValueError, TypeError):
        return None
    error = value.get("Error") if isinstance(value, dict) else None
    code = error.get("Code") if isinstance(error, dict) else None
    return code if isinstance(code, str) and code in SAFE_ERROR_CODES else None


def create_runner(core, args):
    core.MARKER = "goby-settings-browser-v1"
    core.RUN_ENV = "GOBY_SETTINGS_RUN_ID"

    class SettingsRunner(core.Runner):
        browser_spec = "settings.spec.ts"
        browser_timeout_seconds = 600
        screenshot_names = ("settings-defaults-desktop.png", "settings-overrides-desktop.png",
                            "settings-reset-desktop.png", "settings-mobile.png")

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5g_settings_" + self.run_id
            self.role = "goby_m5g_role_" + self.run_id
            self.pg_app_name = "goby_m5g_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-settings-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-settings-" + self.run_id)
            self.admin_name = "m5g-admin-" + self.run_id
            self.initial_defaults = {"ServerName": "Settings deployment " + self.run_id,
                                     "MaxBitrate": 20000000, "MaxWidth": 1920,
                                     "MaxHeight": 1080, "MaxAudioChannels": 8}
            self.replacement_defaults = dict(self.initial_defaults, **{
                "ServerName": "Settings replacement " + self.run_id, "MaxWidth": 2560, "MaxAudioChannels": 6,
            })
            self.startup_defaults = dict(self.initial_defaults)
            self.cookies = http.cookiejar.CookieJar()
            self.admin_opener = urllib.request.build_opener(
                urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.cookies))
            self.public_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
            self.control_csrf = None
            self.control_session_id = None
            self.administrator = None
            self.session_cleanup_required = False
            self.browser_result = None
            self.ui_files = None
            self.report.update({"scenario": "administrator_settings", "database": self.database, "role": self.role})

        def allocate(self):
            super().allocate()
            core.require(self.goby.pw_uid == 995 and os.fstat(self.binary_fd).st_mode & 0o001,
                         "The isolated executable must be runnable by the reviewed UID 995.")
            core.require(self.report["shared_service_before"] == {"MainPID": "3570491", "ActiveState": "active"},
                         "The reviewed shared application service identity changed; no shared service was modified.")
            core.require(self.args.assets.is_file() and core.file_digest(self.args.assets) == self.args.assets_sha256,
                         "The prepared administrator asset archive does not match its supplied SHA-256.")
            core.require(core.free_bytes(core.EXEC_ROOT) >= 128 * 1024 * 1024 and
                         core.free_bytes(Path("/dev/shm")) >= 96 * 1024 * 1024,
                         "Insufficient headroom for the settings browser, database, and screenshots.")
            source_paths = ("e2e/settings.spec.ts", "src/api.ts", "src/settingsDraft.ts", "src/SettingsPage.tsx",
                            "src/App.tsx", "package.json", "package-lock.json", "playwright.config.ts")
            self.report["provenance"] = {
                "wrapper_sha256": core.file_digest(Path(__file__).resolve()),
                "shared_runner_sha256": core.file_digest(args.core_runner.resolve()),
                "binary_sha256": self.args.binary_sha256,
                "assets_archive_sha256": self.args.assets_sha256,
                "source_inputs_sha256": {relative: core.file_digest(self.args.snapshot / "web/admin" / relative)
                                         for relative in source_paths},
            }
            self.report["checks"]["provided_binary_and_assets_hashes_verified"] = True

        def prepare_files(self):
            super().prepare_files()
            core.require(self.report["assets_archive_sha256"] == self.args.assets_sha256,
                         "The asset archive changed between allocation and extraction.")
            # Explicit chmod survives a root operator's restrictive umask. The
            # application owns only its runtime and empty media directory.
            self.runtime.chmod(0o750)
            (self.runtime / "media").chmod(0o750)
            assets = self.runtime / "admin"
            for directory in [assets, *(entry for entry in assets.rglob("*") if entry.is_dir())]:
                directory.chmod(0o755)
            (self.browser_work / "src").mkdir(mode=0o700)
            shutil.copyfile(self.args.snapshot / "web/admin/src/api.ts", self.browser_work / "src/api.ts")
            self.manifest = self.browser_work / "settings-fixture.json"
            self.result_path = self.browser_work / "settings-result.json"
            self.ui_files = {path.relative_to(assets).as_posix(): core.file_digest(path)
                             for path in sorted(assets.rglob("*")) if path.is_file()}
            core.require("index.html" in self.ui_files and self.ui_files,
                         "The prepared UI assets do not contain an application entry point.")
            self.report["provenance"]["loaded_ui_files_sha256"] = self.ui_files
            self.report["provenance"]["loaded_ui_manifest_sha256"] = core.digest(canonical(self.ui_files).encode())
            self.report["provenance"]["private_browser_spec_sha256"] = core.file_digest(
                self.browser_work / "e2e" / self.browser_spec)
            self.report["provenance"]["private_browser_api_sha256"] = core.file_digest(self.browser_work / "src/api.ts")

        def start_app(self):
            core.require(self.app is None or self.app.poll() is not None,
                         "Refusing to start a second isolated settings application concurrently.")
            core.require(self.startup_defaults in (self.initial_defaults, self.replacement_defaults),
                         "The isolated application received an unreviewed startup configuration.")
            defaults = self.startup_defaults
            uri = f"postgresql://{self.role}:{self.role_password}@127.0.0.1:{core.PG_PORT}/{self.database}?sslmode=disable"
            environment = dict(core.BASE_ENV, **{
                "GOBY_LISTEN": f"127.0.0.1:{self.port}", "GOBY_PUBLIC_URL": self.origin,
                "GOBY_DATABASE_URL": uri, "GOBY_COOKIE_SECURE": "false", "GOBY_SETUP_TOKEN": self.setup_token,
                "GOBY_SERVER_NAME": defaults["ServerName"], "GOBY_TRANSCODE_MAX_BITRATE": str(defaults["MaxBitrate"]),
                "GOBY_TRANSCODE_MAX_WIDTH": str(defaults["MaxWidth"]),
                "GOBY_TRANSCODE_MAX_HEIGHT": str(defaults["MaxHeight"]),
                "GOBY_TRANSCODE_MAX_AUDIO_CHANNELS": str(defaults["MaxAudioChannels"]),
                "GOBY_WEB_DIR": str(self.runtime / "admin"), "GOBY_MEDIA_ROOTS": str(self.runtime / "media"),
                "GOBY_TRANSCODING_ENABLED": "false", "GOBY_STARTUP_TIMEOUT": "30s",
                "GOBY_FFMPEG": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg",
                "GOBY_FFPROBE": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe",
                core.RUN_ENV: self.run_id, "TMPDIR": str(self.runtime),
            })
            self.app_log = (self.output / "app-private.log").open("ab")
            os.chmod(self.output / "app-private.log", 0o600)
            # The inherited pinned descriptor grants no access to the shared
            # root-private binary directory. Only this child receives its env.
            self.app = subprocess.Popen([str(self.args.binary)], executable=f"/proc/self/fd/{self.binary_fd}",
                                        pass_fds=(self.binary_fd,), env=environment, cwd=self.runtime,
                                        user=self.goby.pw_uid, group=self.goby.pw_gid, extra_groups=[],
                                        stdin=subprocess.DEVNULL, stdout=self.app_log, stderr=self.app_log,
                                        start_new_session=True)
            def ready():
                core.require(self.app.poll() is None, "The isolated settings application exited before readiness.")
                try:
                    with self.public_opener.open(self.origin + "/readyz", timeout=1) as response:
                        return response.status == 200
                except OSError:
                    return False
            core.wait_until(ready, "The isolated settings application did not become ready.")
            status = Path(f"/proc/{self.app.pid}/status").read_text()
            match = re.search(r"^Uid:\s+(.+)$", status, re.MULTILINE)
            core.require(match is not None and [int(value) for value in match.group(1).split()] == [995] * 4,
                         "The isolated settings application did not retain UID 995.")
            self.report["checks"]["application_uid"] = 995
            self.report.setdefault("application_pids", []).append(self.app.pid)
            self.report.setdefault("application_startup_defaults", []).append(dict(defaults))

        def http_failure(self, request, route, status, code=None):
            path = route.split("?", 1)[0].split("#", 1)[0]
            template = next((label for pattern, label in SAFE_ROUTES if re.fullmatch(pattern, path)), "/{unrecognized-route}")
            method = request.get_method()
            diagnostic = {"Method": method if method in ("GET", "POST", "PUT", "DELETE") else "OTHER",
                          "RouteTemplate": template,
                          "Status": status if type(status) is int and 100 <= status <= 599 else None}
            if isinstance(code, str) and code in SAFE_ERROR_CODES:
                diagnostic["Code"] = code
            failures = self.report.setdefault("http_failures", [])
            if len(failures) < 32:
                failures.append(diagnostic)
            return core.VerificationError("An isolated settings request failed: " + canonical(diagnostic))

        def http_json(self, route, *, body=None, method=None, headers=None, administrator=False, expected=(200, 201)):
            core.require(route.startswith("/admin/v1/") or route == "/emby/System/Info/Public",
                         "Settings fixture requests must remain on the isolated reviewed API routes.")
            request_headers = {"Accept": "application/json", "Origin": self.origin}
            request_headers.update(headers or {})
            data = None if body is None else json.dumps(body).encode("utf-8")
            if data is not None:
                request_headers["Content-Type"] = "application/json"
                if administrator and self.control_csrf:
                    request_headers["X-CSRF-Token"] = self.control_csrf
            request = urllib.request.Request(self.origin + route, data=data, headers=request_headers, method=method)
            opener = self.admin_opener if administrator else self.public_opener
            status = None
            try:
                with opener.open(request, timeout=5) as response:
                    status = response.status
                    raw = response.read(1024 * 1024 + 1)
                    if status not in expected or len(raw) > 1024 * 1024:
                        raise self.http_failure(request, route, status)
                    try:
                        value = json.loads(raw)
                    except ValueError:
                        raise self.http_failure(request, route, status) from None
                    core.require(isinstance(value, dict), "An isolated settings response had an invalid JSON shape.")
                    return value
            except urllib.error.HTTPError as error:
                code = None
                try:
                    code = safe_error_code(error.read(4097))
                except (OSError, ValueError, http.client.HTTPException):
                    pass
                finally:
                    error.close()
                raise self.http_failure(request, route, error.code, code) from None
            except (OSError, http.client.HTTPException):
                raise self.http_failure(request, route, status) from None
            finally:
                self.secrets.extend(cookie.value for cookie in self.cookies if cookie.value not in self.secrets)

        def list_sessions(self):
            result, total = [], None
            for start in range(0, 2000, 200):
                page = self.http_json(f"/admin/v1/sessions?Status=all&StartIndex={start}&Limit=200", administrator=True)
                items, count = page.get("Items"), page.get("TotalRecordCount")
                core.require(isinstance(items, list) and type(count) is int and 0 <= count <= 2000
                             and (total is None or total == count) and page.get("StartIndex") == start
                             and page.get("Limit") == 200 and len(items) == min(200, max(0, count - start))
                             and all(valid_id(item.get("Id")) for item in items),
                             "The isolated session inventory did not form complete bounded pages.")
                total = count
                result.extend(items)
                if len(result) == count:
                    core.require(len({item["Id"] for item in result}) == count,
                                 "The isolated session inventory contained duplicate identities.")
                    return result
            raise core.VerificationError("The isolated session inventory exceeded its record budget.")

        def control_login(self):
            value = self.http_json("/admin/v1/session", administrator=True,
                                   body={"Name": self.admin_name, "Password": self.admin_password})
            csrf = value.get("CSRFToken")
            if isinstance(csrf, str) and csrf:
                self.secrets.append(csrf)
            administrator = value.get("User", {})
            core.require(isinstance(csrf, str) and csrf and valid_id(administrator.get("Id"))
                         and administrator.get("Name") == self.admin_name and administrator.get("IsAdministrator") is True,
                         "The dedicated administrator did not receive a native control session.")
            self.control_csrf = csrf
            self.administrator = {"Id": administrator["Id"], "Name": administrator["Name"]}
            current = [item for item in self.list_sessions() if item.get("IsCurrent") is True]
            core.require(len(current) == 1, "The native control session identity is ambiguous.")
            self.control_session_id = current[0]["Id"]

        def assert_settings(self, value, defaults, overrides, *, revision=None, updated_at=None):
            core.require(isinstance(value, dict) and set(value) == {
                "Revision", "Defaults", "Overrides", "Effective", "Sources", "UpdatedAt", "Deployment"},
                "The native settings response lost its exact safe projection.")
            current_revision = value["Revision"]
            core.require(isinstance(current_revision, str) and re.fullmatch(r"[1-9][0-9]{0,18}", current_revision)
                         and int(current_revision) <= 9223372036854775807
                         and isinstance(value["UpdatedAt"], str) and value["UpdatedAt"],
                         "The native settings revision or update timestamp is invalid.")
            for key in ("Defaults", "Overrides", "Effective", "Sources"):
                core.require(isinstance(value[key], dict) and set(value[key]) == set(SETTINGS_FIELDS),
                             "The native settings field inventory is incomplete.")
            for field in SETTINGS_FIELDS:
                for group in ("Defaults", "Overrides", "Effective"):
                    entry = value[group][field]
                    if group == "Overrides" and entry is None:
                        continue
                    if field == "ServerName":
                        core.require(isinstance(entry, str) and entry.strip() and len(entry.encode("utf-8")) <= 128,
                                     "A native server name is invalid.")
                    else:
                        maximum = 1000000000 if field == "MaxBitrate" else 8 if field == "MaxAudioChannels" else 8192
                        core.require(type(entry) is int and 1 <= entry <= maximum,
                                     "A native output planning limit is invalid.")
            effective = {field: defaults[field] if overrides[field] is None else overrides[field] for field in SETTINGS_FIELDS}
            sources = {field: "deployment" if overrides[field] is None else "database" for field in SETTINGS_FIELDS}
            core.require(value["Defaults"] == defaults and value["Overrides"] == overrides
                         and value["Effective"] == effective and value["Sources"] == sources
                         and isinstance(value["Deployment"], dict) and value["Deployment"] == DEPLOYMENT
                         and type(value["Deployment"].get("TranscodingEnabled")) is bool
                         and all(type(value["Deployment"].get(field)) is int
                                 for field in ("Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs")),
                         "Native settings did not retain the expected startup defaults, explicit overrides, or sources.")
            core.require((revision is None or current_revision == revision)
                         and (updated_at is None or value["UpdatedAt"] == updated_at),
                         "A restart changed the stored settings revision or update timestamp.")
            return value

        def bootstrap(self):
            super().bootstrap()
            # Login may commit even if its response is lost; cleanup discovers
            # every issued session whenever authentication was attempted.
            self.session_cleanup_required = True
            self.control_login()
            core.require(len(self.list_sessions()) == 1, "The fixture must start with exactly one native control login.")
            initial = self.http_json("/admin/v1/settings", administrator=True)
            self.assert_settings(initial, self.initial_defaults, dict.fromkeys(SETTINGS_FIELDS))
            self.fixture = {"Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                            "Administrator": self.administrator, "InitialDefaults": self.initial_defaults,
                            "ReplacementDefaults": self.replacement_defaults, "FinalOverrides": FINAL_OVERRIDES,
                            "ResultPath": str(self.result_path)}
            core.private_write(self.manifest, (canonical(self.fixture) + "\n").encode())
            self.report["fixtures"] = {"dedicated_administrators": 1, "initial_native_sessions": 1,
                                       "initial_defaults": self.initial_defaults,
                                       "replacement_defaults": self.replacement_defaults,
                                       "final_overrides": FINAL_OVERRIDES, "private_manifest": True,
                                       "media_files_created": 0, "playback_requests": 0}

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_SETTINGS_DISPOSABLE_DATABASE": "1",
                                "GOBY_SMOKE_SETTINGS_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_SETTINGS_FIXTURE_MANIFEST": str(self.manifest)})
            return environment

        def load_browser_result(self):
            value = json.loads(core.private_file(self.result_path))
            core.require(isinstance(value, dict), "The private settings browser result is invalid.")
            # Collect browser credentials before checking later assertions,
            # including partially completed journeys that need failure cleanup.
            for field in ("BrowserCookie", "BrowserCSRF"):
                secret = value.get(field)
                if isinstance(secret, str) and 0 < len(secret) <= 8192:
                    self.secrets.append(secret)
                    if field == "BrowserCookie" and secret.startswith("goby_session="):
                        self.secrets.append(secret.split("=", 1)[1])
            inventory = value.get("BrowserSecrets", [])
            core.require(isinstance(inventory, list) and len(inventory) <= 16
                         and all(isinstance(secret, str) and 0 < len(secret) <= 8192 for secret in inventory),
                         "The private browser secret inventory is invalid.")
            self.secrets.extend(inventory)
            core.require(value.get("Marker") == RESULT_MARKER and value.get("RunId") == self.run_id,
                         "The private settings browser result does not match this run.")
            self.browser_result = value
            self.report["browser_result_status"] = {"marker": RESULT_MARKER, "run_id": self.run_id,
                                                     "complete": value.get("Complete") is True}

        def sanitize_report(self):
            def scrub(value):
                if isinstance(value, str):
                    return core.sanitize_text(value, self.secrets)
                if isinstance(value, list):
                    return [scrub(item) for item in value]
                if isinstance(value, dict):
                    return {key: scrub(item) for key, item in value.items()}
                return value
            for key in ("failure", "cleanup_errors", "application_diagnostics", "browser_driver_stdout",
                        "browser_driver_stderr", "browser"):
                if key in self.report:
                    self.report[key] = scrub(self.report[key])
            def known_only(value):
                if isinstance(value, str):
                    for secret in self.secrets:
                        if secret:
                            value = value.replace(secret, "[REDACTED]")
                    return value
                if isinstance(value, list):
                    return [known_only(item) for item in value]
                if isinstance(value, dict):
                    return {known_only(key): known_only(item) for key, item in value.items()}
                return value
            # Scrub known credentials from every exported field while retaining
            # unrelated artifact digests required for reproducible provenance.
            self.report = known_only(self.report)

        def assert_private_logs(self):
            for name in ("app-private.log", "browser-private.json", "browser-private.stderr"):
                path = self.output / name
                core.require(path.stat().st_size <= 8 * 1024 * 1024,
                             "A private settings verification log exceeded its byte budget.")
                value = path.read_text(encoding="utf-8", errors="replace")
                core.require(not any(secret and secret in value for secret in self.secrets),
                             "A private verification log contained an authentication secret; it will not be exported.")
            self.report["checks"]["private_logs_exclude_raw_secrets"] = True

        def verify_browser_session(self):
            result = self.browser_result
            value = self.http_json("/admin/v1/session", headers={"Cookie": result["BrowserCookie"]})
            core.require(value.get("User", {}).get("Id") == self.administrator["Id"]
                         and value.get("CSRFToken") == result["BrowserCSRF"],
                         "The original browser cookie or CSRF identity did not survive the isolated restart.")
            sessions = self.list_sessions()
            core.require(len(sessions) == 2 and {item["Id"] for item in sessions} == {
                self.control_session_id, result["BrowserSessionId"]}
                and all(item.get("Kind") == "admin" and item.get("Status") == "active"
                        and item.get("UserId") == self.administrator["Id"] for item in sessions),
                "The fixture did not retain exactly its two independent native administrator logins.")
            self.report["checks"]["native_credentials_before_cleanup"] = 2

        def run_browser(self):
            try:
                super().run_browser()
            finally:
                try:
                    if self.result_path.exists():
                        self.load_browser_result()
                finally:
                    self.sanitize_report()
            value = self.browser_result
            core.require(value is not None and value.get("Complete") is True and valid_id(value.get("BrowserSessionId")),
                         "The native settings browser journey did not complete.")
            checks = value.get("Checks")
            core.require(isinstance(checks, dict) and checks and all(
                isinstance(key, str) and re.fullmatch(r"[A-Za-z][A-Za-z0-9_]{0,95}", key) and item is True
                for key, item in checks.items()), "The browser did not report its required settings acceptance checks.")
            cookie, csrf = value.get("BrowserCookie"), value.get("BrowserCSRF")
            core.require(isinstance(cookie, str) and re.fullmatch(r"goby_session=[A-Za-z0-9_-]{32,4096}", cookie)
                         and isinstance(csrf, str) and csrf and cookie in value["BrowserSecrets"]
                         and cookie.split("=", 1)[1] in value["BrowserSecrets"] and csrf in value["BrowserSecrets"],
                         "The private result lacks its exact original browser credentials.")
            final = self.assert_settings(value.get("FinalSettings"), self.initial_defaults, FINAL_OVERRIDES)
            core.require(self.http_json("/admin/v1/settings", administrator=True) == final,
                         "The browser result differs from the committed native settings projection.")
            self.verify_browser_session()
            core.require(all((self.output / name).is_file() for name in self.screenshot_names),
                         "The settings browser did not produce all four explicit screenshots.")
            self.assert_private_logs()
            self.report["browser_checks"] = checks
            safe_result = {"Marker": RESULT_MARKER, "RunId": self.run_id, "Complete": True,
                           "FinalSettings": final, "Checks": checks, "NativeSessionCount": 2}
            # Hash only the credential-free projection, never the private result
            # file containing cookies or any database snapshot of stored hashes.
            self.report["provenance"]["browser_result_projection_sha256"] = core.digest(canonical(safe_result).encode())
            self.report["browser_result"] = safe_result
            self.report["provenance"]["screenshots_sha256"] = {
                name: core.file_digest(self.output / name) for name in self.screenshot_names}
            self.report["checks"].update({"browser_native_settings_journey": True,
                                          "browser_completed_without_skips_or_retries": True,
                                          "secret_free_screenshots": 4})

        def owned_database_read(self, statement):
            core.require(self.database_oid is not None and self.lock is not None
                         and re.fullmatch(r"goby_m5g_settings_[0-9]{8}_[0-9]{6}_[0-9a-f]{10}", self.database)
                         and self.database == "goby_m5g_settings_" + self.run_id
                         and self.role == "goby_m5g_role_" + self.run_id,
                         "Settings persistence inspection requires this run's canonical owned database.")
            identity = core.pg_json(f"""SELECT jsonb_build_object('oid', d.oid::bigint,
                'owner', pg_get_userbyid(d.datdba), 'tag', shobj_description(d.oid, 'pg_database'),
                'role_tag', shobj_description(r.oid, 'pg_authid'))
                FROM pg_database d JOIN pg_roles r ON r.oid = d.datdba WHERE d.datname = '{self.database}';""")
            core.require(identity == {"oid": self.database_oid, "owner": self.role,
                                      "tag": self.tag, "role_tag": self.tag},
                         "The owned settings database or role identity changed.")
            guarded = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
                SET LOCAL statement_timeout = '10s';
                DO $guard$ BEGIN
                    IF current_database() <> '{self.database}' OR current_user <> '{self.role}' THEN
                        RAISE EXCEPTION 'Unexpected settings fixture database or role';
                    END IF;
                END $guard$;
                {statement}
                COMMIT;
            """
            return core.command([core.PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At",
                                 "-h", "127.0.0.1", "-p", str(core.PG_PORT), "-U", self.role, "-d", self.database],
                                text=guarded, environment=dict(core.BASE_ENV, PGPASSWORD=self.role_password))

        def full_private_snapshot(self):
            selections = [f"SELECT '{name}' AS name, COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) "
                          f"FROM public.\"{name}\" r), '[]'::jsonb) AS rows" for name in PUBLIC_TABLES]
            raw = self.owned_database_read("SELECT jsonb_build_object('Tables', "
                "(SELECT jsonb_agg(tablename ORDER BY tablename) FROM pg_tables WHERE schemaname = 'public'), "
                "'Rows', (SELECT jsonb_object_agg(name, rows ORDER BY name) FROM (" +
                " UNION ALL ".join(selections) + ") contents));")
            core.require(len(raw.encode("utf-8")) <= 8 * 1024 * 1024,
                         "The owned settings snapshot exceeded its private byte budget.")
            value = json.loads(raw)
            core.require(value.get("Tables") == list(PUBLIC_TABLES) and len(PUBLIC_TABLES) == 28
                         and set(value.get("Rows", {})) == set(PUBLIC_TABLES),
                         "The restart snapshot did not cover exactly all 28 public tables.")
            for rows in value["Rows"].values():
                for row in rows:
                    for key, secret in row.items():
                        if isinstance(secret, str) and secret and any(part in key for part in ("password", "token", "secret", "fingerprint")):
                            self.secrets.append(secret)
                            if secret.startswith("\\x"):
                                self.secrets.append(secret[2:])
            self.report["snapshot_tables"] = list(PUBLIC_TABLES)
            self.report["checks"]["restart_snapshot_public_table_count"] = 28
            return value["Rows"]

        def verify_public_name(self, expected):
            public = self.http_json("/emby/System/Info/Public")
            overview = self.http_json("/admin/v1/overview", administrator=True)
            core.require(public.get("ServerName") == expected and overview.get("Server", {}).get("Name") == expected,
                         "Public server information or the native overview did not use the effective server name.")

        def verify_restart(self):
            final = self.browser_result["FinalSettings"]
            self.stop_app()
            before = self.full_private_snapshot()
            core.require(len(before["managed_settings"]) == 1 and not before["libraries"] and not before["items"]
                         and not before["scan_jobs"] and not before["play_sessions"] and not before["encoding_jobs"],
                         "Settings verification unexpectedly created library, media, playback, or conversion work.")
            self.start_app()
            # Readiness is unauthenticated. Compare before any session/overview/
            # settings request can legitimately advance authentication metadata.
            core.require(self.full_private_snapshot() == before,
                         "The first isolated restart changed rows in the complete 28-table snapshot.")
            self.verify_browser_session()
            unchanged = self.http_json("/admin/v1/settings", administrator=True)
            self.assert_settings(unchanged, self.initial_defaults, FINAL_OVERRIDES,
                                 revision=final["Revision"], updated_at=final["UpdatedAt"])
            core.require(unchanged == final, "The same-default restart changed the confirmed native settings projection.")
            self.verify_public_name(self.initial_defaults["ServerName"])
            self.report["checks"].update({"first_restart_preserved_all_28_public_tables_exactly": True,
                                          "first_restart_preserved_revision_overrides_defaults_and_sources": True,
                                          "original_browser_login_survives_first_restart": True})

            self.stop_app()
            replacement_before = self.full_private_snapshot()
            core.require(replacement_before["managed_settings"] == before["managed_settings"],
                         "Read-only settings checks changed the stored managed settings row.")
            changed = {field for field in SETTINGS_FIELDS if self.initial_defaults[field] != self.replacement_defaults[field]}
            core.require(changed == {"ServerName", "MaxWidth", "MaxAudioChannels"}
                         and all(FINAL_OVERRIDES[field] is None for field in changed),
                         "The replacement startup values changed an unapproved field or an active database override.")
            self.startup_defaults = dict(self.replacement_defaults)
            self.start_app()
            core.require(self.full_private_snapshot() == replacement_before,
                         "The replacement-default restart wrote defaults or changed existing rows in the complete 28-table snapshot.")
            self.verify_browser_session()
            replaced = self.http_json("/admin/v1/settings", administrator=True)
            self.assert_settings(replaced, self.replacement_defaults, FINAL_OVERRIDES,
                                 revision=final["Revision"], updated_at=final["UpdatedAt"])
            self.verify_public_name(self.replacement_defaults["ServerName"])
            reset = self.http_json("/admin/v1/settings/reset", administrator=True,
                                   body={"Revision": replaced["Revision"], "Fields": list(SETTINGS_FIELDS)})
            self.assert_settings(reset, self.replacement_defaults, dict.fromkeys(SETTINGS_FIELDS))
            core.require(int(reset["Revision"]) == int(replaced["Revision"]) + 1,
                         "Resetting all explicit settings did not advance the native revision exactly once.")
            core.require(self.http_json("/admin/v1/settings", administrator=True) == reset,
                         "The all-default settings reset did not remain committed.")
            self.verify_browser_session()
            self.verify_public_name(self.replacement_defaults["ServerName"])
            assets = self.runtime / "admin"
            core.require({path.relative_to(assets).as_posix(): core.file_digest(path)
                          for path in sorted(assets.rglob("*")) if path.is_file()} == self.ui_files,
                         "The loaded administrator UI files changed during settings verification.")
            self.assert_private_logs()
            self.report["replacement_settings"] = replaced
            self.report["reset_settings"] = reset
            self.report["checks"].update({"second_restart_preserved_all_28_public_tables_exactly": True,
                                          "second_restart_changed_only_null_override_default_fields": True,
                                          "database_bitrate_and_height_overrides_survive_new_startup_defaults": True,
                                          "deployment_defaults_never_written_to_managed_settings": True,
                                          "original_browser_login_survives_second_restart": True,
                                          "all_settings_reset_to_replacement_deployment_defaults": True,
                                          "public_info_and_overview_use_effective_server_name": True,
                                          "loaded_ui_assets_unchanged": True})

        def cleanup(self):
            failures = []
            def attempt(name, action):
                try:
                    action()
                    self.report["cleanup"][name] = True
                except Exception as error:
                    failures.append(name)
                    self.report["cleanup"][name] = False
                    self.report["cleanup_errors"][name] = (str(error) if isinstance(error, core.VerificationError)
                                                           else type(error).__name__)
            def stop_browser():
                if self.browser is not None and self.browser.poll() is None:
                    try:
                        os.killpg(self.browser.pid, signal.SIGTERM)
                        self.browser.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(self.browser.pid, signal.SIGKILL)
                        self.browser.wait(timeout=5)
                    except ProcessLookupError:
                        self.browser.wait(timeout=5)
            attempt("browser_stopped_before_session_cleanup", stop_browser)
            if hasattr(self, "result_path") and self.result_path.exists():
                attempt("private_browser_secrets_collected", self.load_browser_result)
            if self.session_cleanup_required:
                def ensure_control():
                    if self.app is None or self.app.poll() is not None:
                        self.stop_app()
                        self.start_app()
                    try:
                        value = self.http_json("/admin/v1/session", administrator=True)
                        csrf = value.get("CSRFToken")
                        core.require(isinstance(csrf, str) and csrf, "The cleanup control session has no CSRF credential.")
                        self.control_csrf = csrf
                        self.secrets.append(csrf)
                    except core.VerificationError:
                        self.control_login()
                attempt("native_cleanup_control_available", ensure_control)
                def revoke_sessions():
                    sessions = self.list_sessions()
                    current = [item for item in sessions if item.get("IsCurrent") is True]
                    core.require(len(current) == 1, "Cleanup could not identify its final native control session.")
                    current_id = current[0]["Id"]
                    errors = []
                    for item in sorted(sessions, key=lambda entry: entry["Id"] == current_id):
                        try:
                            result = self.http_json("/admin/v1/sessions/" + item["Id"] + "/revoke",
                                                    administrator=True, body={})
                            core.require(result.get("SessionId") == item["Id"] and result.get("RevokedAt")
                                         and result.get("CurrentSessionRevoked") == (item["Id"] == current_id),
                                         "A native fixture session revocation did not commit.")
                        except Exception:
                            errors.append(item["Id"])
                    core.require(not errors, "One or more issued native fixture sessions could not be revoked.")
                    self.report["cleanup"]["native_sessions_revoked_count"] = len(sessions)
                attempt("every_issued_session_revoked_control_last", revoke_sessions)
            if failures:
                self.report["status"] = "failed"
            try:
                # Always retain inherited signal-safe process fencing, exact
                # HBA restoration, owned database/role drop, and private scrub.
                super().cleanup()
            finally:
                if failures:
                    self.report["cleanup_failures"] = failures + self.report.get("cleanup_failures", [])
                    self.report["status"] = "failed"
                self.sanitize_report()

    return SettingsRunner(args)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", type=Path, required=True)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--binary-sha256", required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--assets-sha256", required=True)
    parser.add_argument("--core-runner", type=Path, default=Path(__file__).with_name("verify-managed-users.py"))
    args = parser.parse_args()
    core = load_core(args.core_runner)
    core.require(all(re.fullmatch(r"[0-9a-f]{64}", value) for value in (args.binary_sha256, args.assets_sha256)),
                 "Supply the prepared binary and administrator asset SHA-256 values.")
    def interrupted(signum, frame):
        raise core.VerificationError("The settings verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Settings verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
