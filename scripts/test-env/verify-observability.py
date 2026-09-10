#!/usr/bin/env python3
"""Verify activity and diagnostic administration in an owned Linux fixture.

The frozen managed-user runner supplies the scratch PostgreSQL ownership lock,
exact temporary HBA entry, pinned executable descriptor, browser process fencing,
and final disposal. This wrapper never builds software or changes either shared
service. Activity fixtures are committed through the native settings API only.
"""

import argparse
import base64
from collections import Counter
import http.client
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import stat
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request


FIXTURE_MARKER = "goby-observability-browser-fixtures-v1"
RESULT_MARKER = "goby-observability-result-v1"
SETTINGS_FIELDS = ("ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels")
PUBLIC_TABLES = tuple(sorted("""activity_entries application_key_clients application_key_devices
application_keys catalog_entities client_playback_references devices encoding_jobs
item_entities item_images item_metadata_state item_subtitles items libraries
library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings
sessions user_item_data users task_definitions task_occurrences task_run_children
task_run_requests task_runs task_triggers""".split()))
REQUIRED_BROWSER_CHECKS = frozenset((
    "RealActivityFiltersPagingRefreshAndDetails", "RealEmptyActivityAndInjectedErrorRecovery",
    "ObsoleteRealActivityResponseCancelled", "RealLogFilesPreviewPagingReloadAndCloseFocus",
    "RealLogPreviewReadMore", "ActualNativeCookieDownloadAndRedactedJSONL",
    "InjectedMissingFileAndUnavailableStorageRecovery", "MobileActivityAndLogsWithoutOverflow",
    "RealBrowserRevocationClearsActivityAndLogContent", "ReadOnlyBrowserRequests",
    "CredentialFreeURLsAndScreenshots",
))
SHARED_PID = 3641418
SHARED_START_TICKS = 26048863
SHARED_BINARY_SHA256 = "62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54"
REFERENCE_PID = 3131777
LOG_NAME = re.compile(r"goby-[0-9a-f]{32}-[0-9a-f]{32}\.jsonl")
SAFE_ROUTES = (
    (r"/admin/v1/session", "/admin/v1/session"),
    (r"/admin/v1/sessions", "/admin/v1/sessions"),
    (r"/admin/v1/sessions/[0-9a-f]{32}/revoke", "/admin/v1/sessions/{id}/revoke"),
    (r"/admin/v1/settings", "/admin/v1/settings"),
    (r"/admin/v1/activity", "/admin/v1/activity"),
    (r"/admin/v1/logs", "/admin/v1/logs"),
    (r"/admin/v1/logs/[^/]+/lines", "/admin/v1/logs/{name}/lines"),
    (r"/admin/v1/logs/[^/]+/download", "/admin/v1/logs/{name}/download"),
    (r"/healthz", "/healthz"),
)


def load_core(path):
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_observability_browser_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def valid_id(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{32}", value) is not None


def secret_variants(values):
    variants = set()
    for value in values:
        if isinstance(value, str) and value:
            raw = value.encode("utf-8")
            variants.update((value, json.dumps(value)[1:-1], urllib.parse.quote(value, safe=""),
                             urllib.parse.quote_plus(value), raw.hex(), base64.b64encode(raw).decode(),
                             base64.urlsafe_b64encode(raw).decode().rstrip("=")))
    return variants


def contains_secret(raw, values):
    return any(value.encode("utf-8") in raw for value in secret_variants(values))


def validate_browser_result(value, run_id, *, complete=False):
    if not isinstance(value, dict) or value.get("Marker") != RESULT_MARKER or value.get("RunId") != run_id:
        raise ValueError("The private browser result does not match this run.")
    if set(value) - {"Marker", "RunId", "Complete", "BrowserSessionId", "BrowserCookie", "BrowserCSRF", "BrowserSecrets", "Checks"}:
        raise ValueError("The private browser result contains unexpected fields.")
    secrets = value.get("BrowserSecrets")
    checks = value.get("Checks")
    if type(value.get("Complete")) is not bool or not isinstance(secrets, list) or len(secrets) > 16 or not all(
            isinstance(secret, str) and 0 < len(secret) <= 8192 for secret in secrets):
        raise ValueError("The private browser credential inventory is invalid.")
    if not isinstance(checks, dict) or not all(isinstance(key, str) and re.fullmatch(r"[A-Za-z][A-Za-z0-9_]{0,95}", key)
                                              and item is True for key, item in checks.items()):
        raise ValueError("The private browser checks are invalid.")
    if complete:
        cookie, csrf = value.get("BrowserCookie"), value.get("BrowserCSRF")
        if value["Complete"] is not True or not valid_id(value.get("BrowserSessionId")) or not REQUIRED_BROWSER_CHECKS <= checks.keys():
            raise ValueError("The browser did not complete the required acceptance checks.")
        if not isinstance(cookie, str) or not re.fullmatch(r"goby_session=[A-Za-z0-9_-]{32,4096}", cookie) or not isinstance(csrf, str) or not csrf:
            raise ValueError("The private result does not contain the original browser credentials.")
        if cookie not in secrets or cookie.split("=", 1)[1] not in secrets or csrf not in secrets:
            raise ValueError("The private browser credential inventory is incomplete.")
    return value


def diag_bytes_validate(raw, secrets):
    if len(raw) > 8192 or raw and not raw.endswith(b"\n") or contains_secret(raw, secrets):
        raise ValueError("A bounded diagnostic file is incomplete or contains a private value.")
    records = []
    for line in raw.splitlines():
        value = json.loads(line.decode("utf-8"))
        if not isinstance(value, dict) or not isinstance(value.get("event"), str):
            raise ValueError("A diagnostic line is not a structured safe event.")
        records.append(value)
    return records


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, file, code, message, headers, new_url):
        return None


def create_runner(core, args):
    core.MARKER = "goby-observability-browser-v1"
    core.RUN_ENV = "GOBY_OBSERVABILITY_RUN_ID"

    class ObservabilityRunner(core.Runner):
        browser_spec = "observability.spec.ts"
        browser_timeout_seconds = 360
        screenshot_names = ("observability-activity-desktop.png", "observability-logs-desktop.png",
                            "observability-details-desktop.png", "observability-mobile.png")

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5i_observe_" + self.run_id
            self.role = "goby_m5i_role_" + self.run_id
            self.pg_app_name = "goby_m5i_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-observability-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-observability-" + self.run_id)
            self.admin_name = "m5i-admin-" + self.run_id
            self.cookies = http.cookiejar.CookieJar()
            self.admin_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPCookieProcessor(self.cookies))
            self.public_opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
            self.control_csrf = self.control_cookie = self.control_session_id = None
            self.administrator = self.browser_result = self.original_settings = None
            self.session_cleanup_required = False
            self.settings_restore_required = False
            self.settings_current_revision = None
            self.http_count = 0
            self.last_request_id = None
            self.sentinels = {name: "private-" + name + "-" + core.secrets.token_urlsafe(24)
                              for name in ("header", "query", "path", "body")}
            self.secrets.extend(self.sentinels.values())
            self.report.update({"scenario": "administrator_observability", "database": self.database, "role": self.role})

        def shared_identity(self):
            result = {}
            for name, expected in (("goby-foundation-test.service", SHARED_PID), ("goby-emby-reference.service", REFERENCE_PID)):
                state = dict(line.split("=", 1) for line in core.command([
                    "systemctl", "show", name, "--property=MainPID", "--property=ActiveState"]).splitlines() if "=" in line)
                core.require(state == {"MainPID": str(expected), "ActiveState": "active"},
                             "A reviewed shared service identity changed; no shared service was modified.")
                root = Path("/proc") / str(expected)
                ticks = int((root / "stat").read_text().rsplit(") ", 1)[1].split()[19])
                result[name] = {"pid": expected, "start_ticks": ticks, "binary_sha256": core.file_digest(root / "exe")}
            main = result["goby-foundation-test.service"]
            core.require(main["start_ticks"] == SHARED_START_TICKS and main["binary_sha256"] == SHARED_BINARY_SHA256,
                         "The reviewed shared application executable or process age changed.")
            return result

        def allocate(self):
            super().allocate()
            core.require(self.goby.pw_uid == 995 and os.fstat(self.binary_fd).st_mode & 0o001,
                         "The isolated executable must be runnable by the reviewed UID 995.")
            core.require(self.args.assets.is_file() and core.file_digest(self.args.assets) == self.args.assets_sha256,
                         "The prepared administrator assets do not match their supplied SHA-256.")
            core.require(core.free_bytes(core.EXEC_ROOT) >= 128 * 1024 * 1024 and core.free_bytes(Path("/dev/shm")) >= 96 * 1024 * 1024,
                         "Insufficient scratch headroom for the browser, database, and screenshots.")
            self.shared_before = self.shared_identity()
            self.report["shared_identities_before"] = self.shared_before
            source_paths = ("e2e/observability.spec.ts", "src/api.ts", "src/ObservabilityPage.tsx", "src/App.tsx",
                            "package.json", "package-lock.json", "playwright.config.ts")
            self.report["provenance"] = {
                "wrapper_sha256": core.file_digest(Path(__file__).resolve()),
                "shared_runner_sha256": core.file_digest(self.args.core_runner.resolve()),
                "binary_sha256": self.args.binary_sha256, "assets_archive_sha256": self.args.assets_sha256,
                "source_inputs_sha256": {relative: core.file_digest(self.args.snapshot / "web/admin" / relative) for relative in source_paths},
            }
            self.report["checks"]["provided_binary_and_assets_hashes_verified"] = True

        def prepare_files(self):
            super().prepare_files()
            core.require(self.report["assets_archive_sha256"] == self.args.assets_sha256,
                         "The asset archive changed before extraction.")
            self.runtime.chmod(0o750)
            (self.runtime / "media").chmod(0o750)
            self.log_directory = self.runtime / "diagnostics"
            self.log_directory.mkdir(mode=0o700)
            os.chown(self.log_directory, 995, self.goby.pw_gid)
            self.log_directory_identity = (self.log_directory.stat().st_dev, self.log_directory.stat().st_ino)
            assets = self.runtime / "admin"
            for directory in [assets, *(entry for entry in assets.rglob("*") if entry.is_dir())]:
                directory.chmod(0o755)
            (self.browser_work / "src").mkdir(mode=0o700)
            shutil.copyfile(self.args.snapshot / "web/admin/src/api.ts", self.browser_work / "src/api.ts")
            self.manifest = self.browser_work / "observability-fixture.json"
            self.result_path = self.browser_work / "observability-result.json"
            self.ui_files = {path.relative_to(assets).as_posix(): core.file_digest(path) for path in sorted(assets.rglob("*")) if path.is_file()}
            core.require("index.html" in self.ui_files, "The prepared assets do not contain the dashboard entry point.")
            self.report["provenance"].update({"loaded_ui_files_sha256": self.ui_files,
                "loaded_ui_manifest_sha256": core.digest(canonical(self.ui_files).encode()),
                "private_browser_spec_sha256": core.file_digest(self.browser_work / "e2e" / self.browser_spec),
                "private_browser_api_sha256": core.file_digest(self.browser_work / "src/api.ts")})

        def start_app(self):
            core.require(self.app is None or self.app.poll() is not None, "The isolated application is already running.")
            uri = f"postgresql://{self.role}:{self.role_password}@127.0.0.1:{core.PG_PORT}/{self.database}?sslmode=disable"
            environment = dict(core.BASE_ENV, **{
                "GOBY_LISTEN": f"127.0.0.1:{self.port}", "GOBY_PUBLIC_URL": self.origin, "GOBY_DATABASE_URL": uri,
                "GOBY_COOKIE_SECURE": "false", "GOBY_SETUP_TOKEN": self.setup_token,
                "GOBY_SERVER_NAME": "Observability deployment " + self.run_id,
                "GOBY_WEB_DIR": str(self.runtime / "admin"), "GOBY_MEDIA_ROOTS": str(self.runtime / "media"),
                "GOBY_TRANSCODING_ENABLED": "false", "GOBY_STARTUP_TIMEOUT": "30s",
                "GOBY_FFMPEG": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg",
                "GOBY_FFPROBE": "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe",
                "GOBY_LOG_DIR": str(self.log_directory), "GOBY_LOG_MAX_FILE_BYTES": "8192",
                "GOBY_LOG_MAX_FILES": "4", "GOBY_LOG_RETENTION_DAYS": "7", "GOBY_LOG_MIN_FREE_BYTES": "1048576",
                "GOBY_ACTIVITY_RETENTION_DAYS": "30", "GOMEMLIMIT": "256MiB", "GOMAXPROCS": "2",
                core.RUN_ENV: self.run_id, "TMPDIR": str(self.runtime),
            })
            self.app_log = (self.output / "app-private.log").open("ab")
            os.chmod(self.output / "app-private.log", 0o600)
            self.app = subprocess.Popen([str(self.args.binary)], executable=f"/proc/self/fd/{self.binary_fd}",
                pass_fds=(self.binary_fd,), env=environment, cwd=self.runtime, user=995, group=self.goby.pw_gid,
                extra_groups=[], stdin=subprocess.DEVNULL, stdout=self.app_log, stderr=self.app_log, start_new_session=True)
            def ready():
                core.require(self.app.poll() is None, "The isolated application exited before readiness.")
                try:
                    with self.public_opener.open(self.origin + "/readyz", timeout=1) as response:
                        self.last_request_id = response.headers.get("X-Request-Id")
                        return response.status == 200
                except OSError:
                    return False
            core.wait_until(ready, "The isolated application did not become ready.")
            match = re.search(r"^Uid:\s+(.+)$", Path(f"/proc/{self.app.pid}/status").read_text(), re.MULTILINE)
            core.require(match is not None and [int(value) for value in match.group(1).split()] == [995] * 4,
                         "The isolated application did not retain UID 995.")
            self.report["checks"]["application_uid"] = 995
            self.report.setdefault("application_pids", []).append(self.app.pid)

        def request(self, route, *, method="GET", body=None, headers=None, administrator=False, expected=(200,), limit=1024 * 1024):
            core.require(route.startswith("/") and not route.startswith("//") and "#" not in route and "\r" not in route and "\n" not in route,
                         "Fixture requests must remain on the isolated origin.")
            core.require(self.http_count < 600, "The isolated control HTTP request budget was exceeded.")
            self.http_count += 1
            request_headers = {"Accept": "application/json", "Origin": self.origin}
            request_headers.update(headers or {})
            data = None if body is None else canonical(body).encode()
            if data is not None:
                request_headers["Content-Type"] = "application/json"
            if administrator and method not in ("GET", "HEAD") and self.control_csrf:
                request_headers["X-CSRF-Token"] = self.control_csrf
            request = urllib.request.Request(self.origin + route, data=data, headers=request_headers, method=method)
            opener = self.admin_opener if administrator else self.public_opener
            response = None
            try:
                try:
                    response = opener.open(request, timeout=5)
                except urllib.error.HTTPError as error:
                    response = error
                raw = response.read(limit + 1)
                self.last_request_id = response.headers.get("X-Request-Id")
                if response.status not in expected or len(raw) > limit:
                    path = route.split("?", 1)[0]
                    template = next((label for pattern, label in SAFE_ROUTES if re.fullmatch(pattern, path)), "/{unmatched}")
                    diagnostic = {"Method": method, "RouteTemplate": template, "Status": response.status}
                    self.report.setdefault("http_failures", []).append(diagnostic)
                    raise core.VerificationError("An isolated request failed: " + canonical(diagnostic))
                return response.status, dict(response.headers.items()), raw
            except (OSError, http.client.HTTPException):
                raise core.VerificationError("An isolated request did not complete; private request details were omitted.") from None
            finally:
                if response is not None:
                    response.close()
                self.secrets.extend(cookie.value for cookie in self.cookies if cookie.value not in self.secrets)

        def http_json(self, route, **kwargs):
            _, _, raw = self.request(route, **kwargs)
            try:
                value = json.loads(raw)
            except ValueError:
                raise core.VerificationError("An isolated response was not valid JSON; private contents were omitted.") from None
            core.require(isinstance(value, dict), "An isolated JSON response had an invalid shape.")
            return value

        def list_sessions(self):
            value = self.http_json("/admin/v1/sessions?Status=all&StartIndex=0&Limit=200", administrator=True)
            items = value.get("Items")
            core.require(isinstance(items, list) and type(value.get("TotalRecordCount")) is int and value.get("TotalRecordCount") == len(items) <= 8 and
                         all(valid_id(item.get("Id")) for item in items) and len({item["Id"] for item in items}) == len(items),
                         "The dedicated native session inventory is incomplete or ambiguous.")
            return items

        def control_login(self):
            self.session_cleanup_required = True
            value = self.http_json("/admin/v1/session", method="POST", administrator=True,
                                   body={"Name": self.admin_name, "Password": self.admin_password})
            self.control_csrf = value.get("CSRFToken")
            user = value.get("User", {})
            self.secrets.extend([self.control_csrf] if isinstance(self.control_csrf, str) else [])
            cookies = [cookie for cookie in self.cookies if cookie.name == "goby_session"]
            core.require(isinstance(self.control_csrf, str) and self.control_csrf and len(cookies) == 1 and
                         valid_id(user.get("Id")) and user.get("Name") == self.admin_name and user.get("IsAdministrator") is True,
                         "The dedicated administrator did not receive one native control credential.")
            self.control_cookie = "goby_session=" + cookies[0].value
            self.secrets.append(self.control_cookie)
            self.administrator = {"Id": user["Id"], "Name": user["Name"]}
            current = [item for item in self.list_sessions() if item.get("IsCurrent") is True]
            core.require(len(current) == 1, "The native control session identity is ambiguous.")
            self.control_session_id = current[0]["Id"]

        def owned_database_read(self, statement):
            core.require(self.database_oid is not None and self.lock is not None and
                         re.fullmatch(r"goby_m5i_observe_[0-9]{8}_[0-9]{6}_[0-9a-f]{10}", self.database) and
                         self.database == "goby_m5i_observe_" + self.run_id and self.role == "goby_m5i_role_" + self.run_id,
                         "Persistence inspection requires this run's canonical owned database.")
            identity = core.pg_json(f"""SELECT jsonb_build_object('oid', d.oid::bigint,
                'owner', pg_get_userbyid(d.datdba), 'tag', shobj_description(d.oid, 'pg_database'),
                'role_tag', shobj_description(r.oid, 'pg_authid')) FROM pg_database d
                JOIN pg_roles r ON r.oid = d.datdba WHERE d.datname = '{self.database}';""")
            core.require(identity == {"oid": self.database_oid, "owner": self.role, "tag": self.tag, "role_tag": self.tag},
                         "The owned database or role identity changed.")
            guarded = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
                SET LOCAL statement_timeout = '10s';
                DO $guard$ BEGIN
                    IF current_database() <> '{self.database}' OR current_user <> '{self.role}' THEN
                        RAISE EXCEPTION 'Unexpected observability fixture database or role';
                    END IF;
                END $guard$;
                {statement}
                COMMIT;"""
            return core.command([core.PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At", "-h", "127.0.0.1",
                "-p", str(core.PG_PORT), "-U", self.role, "-d", self.database], text=guarded,
                environment=dict(core.BASE_ENV, PGPASSWORD=self.role_password))

        def full_private_snapshot(self):
            selections = [f"SELECT '{name}' AS name, COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text) "
                          f"FROM public.\"{name}\" r), '[]'::jsonb) AS rows" for name in PUBLIC_TABLES]
            raw = self.owned_database_read("SELECT jsonb_build_object('Tables', "
                "(SELECT jsonb_agg(tablename ORDER BY tablename) FROM pg_tables WHERE schemaname = 'public'), "
                "'Rows', (SELECT jsonb_object_agg(name, rows ORDER BY name) FROM (" + " UNION ALL ".join(selections) + ") contents));")
            core.require(len(raw.encode()) <= 8 * 1024 * 1024, "The private persistence snapshot exceeded its byte budget.")
            value = json.loads(raw)
            core.require(value.get("Tables") == list(PUBLIC_TABLES) and len(PUBLIC_TABLES) == 29 and set(value.get("Rows", {})) == set(PUBLIC_TABLES),
                         "The private persistence snapshot did not cover exactly all 29 public tables.")
            for rows in value["Rows"].values():
                for row in rows:
                    for key, secret in row.items():
                        if isinstance(secret, str) and secret and any(part in key for part in ("password", "token", "secret", "fingerprint")):
                            self.secrets.append(secret)
                            if secret.startswith("\\x"):
                                self.secrets.append(secret[2:])
            self.report["snapshot_tables"] = list(PUBLIC_TABLES)
            return value["Rows"]

        def diagnostic_files(self):
            info = self.log_directory.lstat()
            core.require(stat.S_ISDIR(info.st_mode) and info.st_uid == 995 and stat.S_IMODE(info.st_mode) == 0o700 and
                         (info.st_dev, info.st_ino) == self.log_directory_identity, "The owned diagnostic directory identity changed.")
            files = {}
            for path in sorted(self.log_directory.iterdir()):
                core.require(path.name in (".goby-diagnostics.json", ".goby-diagnostics.lock") or LOG_NAME.fullmatch(path.name),
                             "The dedicated diagnostic directory contains an unrecognized file.")
                if not LOG_NAME.fullmatch(path.name):
                    continue
                descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
                with os.fdopen(descriptor, "rb") as source:
                    entry = os.fstat(source.fileno())
                    core.require(stat.S_ISREG(entry.st_mode) and entry.st_uid == 995 and stat.S_IMODE(entry.st_mode) == 0o600
                                 and entry.st_nlink == 1 and entry.st_size <= 8192,
                                 "A retained diagnostic file has unexpected ownership, type, or size.")
                    raw = source.read(8193)
                    core.require(len(raw) == entry.st_size, "A diagnostic file changed while its process was paused.")
                    try:
                        diag_bytes_validate(raw, [*self.secrets, str(self.runtime), str(self.output)])
                    except (ValueError, UnicodeError):
                        raise core.VerificationError("A retained diagnostic file failed the immediate structured secrecy check.") from None
                    files[path.name] = raw
            core.require(1 <= len(files) <= 4, "The diagnostic file inventory exceeds its configured four-file policy.")
            return files

        def paused_diagnostics(self):
            core.require(self.app is not None and self.app.poll() is None, "A diagnostic snapshot requires this run's live child.")
            if valid_id(self.last_request_id):
                self.wait_for_request_record(self.last_request_id)
            descriptor = os.pidfd_open(self.app.pid)
            stopped = False
            try:
                signal.pidfd_send_signal(descriptor, signal.SIGSTOP)
                stopped = True
                def paused():
                    core.require(self.app.poll() is None, "The dedicated child exited during its diagnostic snapshot.")
                    return Path(f"/proc/{self.app.pid}/stat").read_text().rsplit(") ", 1)[1].split()[0] in ("T", "t")
                core.wait_until(paused, "The dedicated child could not be paused for its fixed log snapshot.", timeout=3)
                return self.diagnostic_files()
            finally:
                try:
                    if stopped:
                        signal.pidfd_send_signal(descriptor, signal.SIGCONT)
                finally:
                    os.close(descriptor)

        def wait_for_request_record(self, request_id):
            # The handler persists its record before writing the sanitized
            # fallback. Waiting for that complete line avoids pausing the app
            # in the middle of a rotation or a partial JSONL append. This is
            # only a synchronization signal; acceptance reads every retained
            # dedicated log byte afterward while the owned child is paused.
            path = self.output / "app-private.log"
            def complete():
                core.require(path.stat().st_size <= 8 * 1024 * 1024, "The private fallback log exceeded its byte budget.")
                raw = path.read_bytes()
                lines = raw.split(b"\n")[:-1]
                for line in reversed(lines):
                    try:
                        value = json.loads(line)
                    except ValueError:
                        continue
                    if isinstance(value, dict) and value.get("event") == "request.completed" and value.get("request_id") == request_id:
                        return True
                return False
            core.wait_until(complete, "The completed request did not reach the safe diagnostic fallback.", timeout=5)

        def immediate_injection_check(self, kind, request_id):
            core.require(kind in self.sentinels and valid_id(request_id), "The injected request did not expose its generated request identity.")
            evidence = None
            def complete():
                nonlocal evidence
                evidence = self.paused_diagnostics()
                return any(record.get("event") == "request.completed" and record.get("request_id") == request_id
                           for raw in evidence.values() for record in diag_bytes_validate(raw, self.secrets))
            # No later fixture request or rotation stimulus occurs until this
            # exact request is present and every currently retained byte passes.
            core.wait_until(complete, "The injected request did not produce a retained diagnostic completion record.", timeout=5)
            self.report.setdefault("immediate_injection_evidence", {})[kind] = {
                "request_completion_observed": True, "all_retained_files_checked": len(evidence),
                "all_retained_bytes_checked": sum(map(len, evidence.values())),
                "files_sha256": {name: core.digest(raw) for name, raw in evidence.items()},
            }

        def update_settings(self, overrides, mode, encoding):
            core.require(self.settings_current_revision is not None, "A settings write requires the last committed revision.")
            value = self.http_json("/admin/v1/settings", method="PUT", administrator=True, body={
                "Revision": self.settings_current_revision, "Overrides": overrides, "ServerNameMode": mode, "Encoding": encoding})
            revision = value.get("Revision")
            core.require(isinstance(revision, str) and revision.isdecimal() and int(revision) == int(self.settings_current_revision) + 1
                         and value.get("Overrides") == overrides and value.get("ServerNameMode") == mode and value.get("Encoding") == encoding,
                         "The native settings mutation did not commit exactly one intended revision.")
            self.settings_current_revision = revision
            return value

        def restore_settings(self):
            if not self.settings_restore_required:
                return
            current = self.http_json("/admin/v1/settings", administrator=True)
            # Never overwrite a revision not acknowledged by this runner.
            core.require(current.get("Revision") == self.settings_current_revision, "Settings changed outside the fixture's acknowledged revision chain.")
            original = self.original_settings
            value = self.update_settings(original["Overrides"], original["ServerNameMode"], original["Encoding"])
            core.require(all(value[field] == original[field] for field in ("Defaults", "Overrides", "Effective", "Sources", "ServerNameMode", "Encoding", "Deployment")),
                         "The native CAS restore did not recover the complete original settings state.")
            self.settings_restore_required = False
            self.restored_settings = value

        def bootstrap(self):
            before = self.full_private_snapshot()
            core.require(len(before["schema_migrations"]) == 22 and not before["activity_entries"] and not before["users"] and not before["sessions"],
                         "The candidate must start with schema 22 and no inferred activity or credentials.")
            super().bootstrap()
            self.control_login()
            core.require(len(self.list_sessions()) == 1, "The fixture must begin with exactly one control credential.")
            self.original_settings = self.http_json("/admin/v1/settings", administrator=True)
            original = self.original_settings
            core.require(set(original.get("Overrides", {})) == set(SETTINGS_FIELDS) and all(value is None for value in original["Overrides"].values())
                         and original.get("ServerNameMode") == "deployment" and original.get("Encoding") == {"TranscodingMaxWidth": 0}
                         and original.get("Revision") == "1", "The new database settings are not at their initial defaults.")
            self.settings_current_revision = original["Revision"]
            self.request("/healthz", headers={"X-Private-Observability": self.sentinels["header"], "Authorization": "Bearer " + self.sentinels["header"]})
            self.immediate_injection_check("header", self.last_request_id)
            self.request("/healthz?api_key=" + self.sentinels["query"])
            self.immediate_injection_check("query", self.last_request_id)
            self.request("/admin/v1/" + self.sentinels["path"], administrator=True, expected=(404,))
            self.immediate_injection_check("path", self.last_request_id)
            self.settings_restore_required = True
            for index in range(61):
                overrides = dict(original["Overrides"], ServerName=self.sentinels["body"] if index == 0 else "Observed change " + str(index))
                self.update_settings(overrides, "custom", original["Encoding"])
                if index == 0:
                    self.immediate_injection_check("body", self.last_request_id)
            self.restore_settings()
            activity = self.http_json("/admin/v1/activity?StartIndex=0&Limit=200&Action=settings.updated&ActorId=" + self.administrator["Id"], administrator=True)
            core.require(activity.get("TotalRecordCount") == 62 and len(activity.get("Items", [])) == 62 and activity.get("RetentionDays") == 30,
                         "The real settings writes and restore did not produce exactly 62 retained audit facts.")
            core.require(not contains_secret(canonical(activity).encode(), self.secrets), "Activity metadata exposed private request values.")
            # Generate only real low-cost requests until a retained file has a
            # complete preview page and the directory demonstrates rotation.
            for _ in range(120):
                files = self.paused_diagnostics()
                if len(files) >= 2 and max(len(raw.splitlines()) for raw in files.values()) >= 12:
                    break
                self.request("/healthz")
            else:
                raise core.VerificationError("Real diagnostic requests did not exercise file rotation and preview pagination.")
            self.seed_snapshot = self.full_private_snapshot()
            core.require(Counter(row["action"] for row in self.seed_snapshot["activity_entries"]) == {
                "user.created": 1, "session.login": 1, "settings.updated": 62}, "The seed activity includes unexpected committed mutations.")
            settings_events = sorted((row for row in self.seed_snapshot["activity_entries"] if row["action"] == "settings.updated"), key=lambda row: row["id"])
            core.require([row["revision"] for row in settings_events] == list(range(2, 64)) and all(
                row["source"] == "native" and row["actor_kind"] == "user" and row["actor_id"] == self.administrator["Id"]
                and row["actor_credential_id"] == self.control_session_id and row["resource_kind"] == "settings"
                and row["resource_id"] == "1" and row["affected_count"] == 1 and row["severity"] == "Info"
                and set(row["changed_fields"]) == ({"ServerName", "ServerNameMode"} if index in (0, 61) else {"ServerName"})
                for index, row in enumerate(settings_events)), "The real settings audit facts do not match their actor, revision, or changed field names.")
            self.fixture = {"Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                "Administrator": self.administrator, "Activity": {"Action": "settings.updated", "ActorId": self.administrator["Id"], "MinimumCount": 61},
                "Diagnostics": {"MinimumFiles": 2, "Sentinels": list(self.sentinels.values())}}
            core.private_write(self.manifest, (canonical(self.fixture) + "\n").encode())
            self.report["fixtures"] = {"dedicated_administrators": 1, "control_logins_issued": 1, "settings_mutations": 61,
                "settings_restore_mutations": 1, "settings_audit_entries": 62, "sentinel_channels_checked": sorted(self.sentinels),
                "media_files_created": 0, "playback_requests": 0, "private_manifest": True}
            self.report["checks"].update({"schema_22_fresh_activity_empty": True, "audit_fixtures_use_only_committed_native_mutations": True,
                "settings_restored_with_acknowledged_revision": True, "every_injection_checked_before_later_rotation": True,
                "real_diagnostic_file_rotation": True})

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_OBSERVABILITY_DISPOSABLE_DATABASE": "1", "GOBY_SMOKE_OBSERVABILITY_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_OBSERVABILITY_FIXTURE": str(self.manifest)})
            return environment

        def load_browser_result(self):
            value = json.loads(core.private_file(self.result_path))
            # A partially completed result must still supply cleanup secrets.
            if isinstance(value, dict):
                for field in ("BrowserCookie", "BrowserCSRF"):
                    secret = value.get(field)
                    if isinstance(secret, str) and 0 < len(secret) <= 8192:
                        self.secrets.append(secret)
                        if field == "BrowserCookie" and secret.startswith("goby_session="):
                            self.secrets.append(secret.split("=", 1)[1])
                values = value.get("BrowserSecrets")
                if isinstance(values, list) and len(values) <= 16:
                    self.secrets.extend(secret for secret in values if isinstance(secret, str) and 0 < len(secret) <= 8192)
            try:
                self.browser_result = validate_browser_result(value, self.run_id)
            except ValueError:
                raise core.VerificationError("The private browser result failed its run identity or schema check.") from None

        def verify_browser_revoked(self):
            result = self.browser_result
            self.request("/admin/v1/session", headers={"Cookie": result["BrowserCookie"]}, expected=(401,))
            control = self.http_json("/admin/v1/session", administrator=True)
            core.require(control.get("CSRFToken") == self.control_csrf and control.get("User", {}).get("Id") == self.administrator["Id"],
                         "Browser revocation also invalidated the independent control credential.")
            sessions = self.list_sessions()
            core.require(len(sessions) == 2 and {item["Id"] for item in sessions} == {self.control_session_id, result["BrowserSessionId"]}
                         and all(item["Kind"] == "admin" and item["UserId"] == self.administrator["Id"] for item in sessions)
                         and next(item for item in sessions if item["Id"] == self.control_session_id)["Status"] == "active"
                         and next(item for item in sessions if item["Id"] == result["BrowserSessionId"])["Status"] == "revoked",
                         "The browser journey did not retain exactly one active control and one revoked browser credential.")

        def run_browser(self):
            try:
                super().run_browser()
            finally:
                try:
                    if self.result_path.exists():
                        self.load_browser_result()
                finally:
                    self.sanitize_report()
            try:
                validate_browser_result(self.browser_result, self.run_id, complete=True)
            except ValueError:
                raise core.VerificationError("The browser did not complete every required observability acceptance check.") from None
            self.verify_browser_revoked()
            current = self.full_private_snapshot()
            for table in PUBLIC_TABLES:
                if table not in ("sessions", "activity_entries"):
                    core.require(current[table] == self.seed_snapshot[table], "The read-only browser journey changed unrelated persisted rows.")
            core.require(Counter(row["action"] for row in current["activity_entries"]) == {
                "user.created": 1, "session.login": 2, "settings.updated": 62, "session.revoked": 1},
                "The browser journey committed activity outside login and self-revocation.")
            previous = {row["id"]: row for row in self.seed_snapshot["activity_entries"]}
            core.require(previous.keys() <= {row["id"] for row in current["activity_entries"]} and
                         all(row == previous[row["id"]] for row in current["activity_entries"] if row["id"] in previous),
                         "The browser journey rewrote an existing audit fact.")
            appended = [row for row in current["activity_entries"] if row["id"] not in previous]
            core.require(len(appended) == 2 and {row["action"] for row in appended} == {"session.login", "session.revoked"} and all(
                row["source"] == "native" and row["actor_kind"] == "user" and row["actor_id"] == self.administrator["Id"]
                and row["actor_credential_id"] == self.browser_result["BrowserSessionId"] and row["resource_kind"] == "session"
                and row["resource_id"] == self.browser_result["BrowserSessionId"] and row["affected_count"] == 1 for row in appended),
                "The browser's two committed session events do not identify its own credential.")
            core.require(self.http_json("/admin/v1/settings", administrator=True) == self.restored_settings,
                         "The observability browser changed settings.")
            core.require(all((self.output / name).is_file() for name in self.screenshot_names), "The browser did not produce all four explicit screenshots.")
            self.assert_private_logs()
            safe_result = {"Marker": RESULT_MARKER, "RunId": self.run_id, "Complete": True,
                           "Checks": self.browser_result["Checks"], "NativeSessionCount": 2}
            self.report["browser_result"] = safe_result
            self.report["provenance"]["browser_result_projection_sha256"] = core.digest(canonical(safe_result).encode())
            self.report["provenance"]["screenshots_sha256"] = {name: core.file_digest(self.output / name) for name in self.screenshot_names}
            self.report["checks"].update({"browser_native_observability_journey": True, "browser_completed_without_skips_or_retries": True,
                "browser_self_revocation_does_not_revoke_control": True, "browser_business_effects_only_two_session_events": True,
                "secret_free_screenshots": 4})
            self.verify_download_readers()

        def verify_download_readers(self):
            for _ in range(16):
                inventory = self.http_json("/admin/v1/logs?StartIndex=0&Limit=200", administrator=True)
                files = inventory.get("Items", [])
                readable = [item for item in files if LOG_NAME.fullmatch(item.get("Name", "")) and
                            isinstance(item.get("Size"), str) and item["Size"].isdecimal() and int(item["Size"]) > 0]
                core.require(readable, "The diagnostic inventory did not contain a readable owned file.")
                selected = max(readable, key=lambda item: (item["DateCreated"], item["Name"]))
                _, _, raw = self.request("/admin/v1/logs/" + selected["Name"] + "/download", administrator=True, limit=8192)
                try:
                    core.require(bool(diag_bytes_validate(raw, self.secrets)), "A completed native diagnostic download was empty.")
                except (ValueError, UnicodeError):
                    raise core.VerificationError("A completed native diagnostic download failed its structured secrecy check.") from None
            self.report["checks"]["sixteen_completed_native_downloads_reuse_bounded_reader_slots"] = True

        def assert_private_logs(self):
            for name in ("app-private.log", "browser-private.json", "browser-private.stderr"):
                path = self.output / name
                if not path.exists():
                    continue
                core.require(path.stat().st_size <= 8 * 1024 * 1024, "A private verification log exceeded its byte budget.")
                core.require(not contains_secret(path.read_bytes(), self.secrets), "A private driver or fallback log contains a credential or request sentinel.")
            self.report["checks"]["private_driver_and_fallback_logs_exclude_secrets"] = True

        def verify_restart(self):
            for number in (1, 2):
                self.stop_app()
                before = self.full_private_snapshot()
                retained = self.diagnostic_files()
                core.require(not any(before[table] for table in ("libraries", "items", "scan_jobs", "play_sessions", "encoding_jobs")),
                             "Observability verification unexpectedly created media or background work.")
                self.start_app()
                # Readiness is unauthenticated. Compare all rows before a
                # legitimate session read can advance authentication metadata.
                core.require(self.full_private_snapshot() == before, "The isolated restart changed one or more of the 29 public tables.")
                recovered = self.paused_diagnostics()
                common = retained.keys() & recovered.keys()
                core.require(common and all(retained[name] == recovered[name] for name in common),
                             "The restarted store lost or rewrote every previously retained log file.")
                inventory = self.http_json("/admin/v1/logs?StartIndex=0&Limit=200", administrator=True)
                names = {item["Name"] for item in inventory.get("Items", [])}
                core.require(common <= names and inventory.get("Status", {}).get("Healthy") is True,
                             "Recovered diagnostic files are not readable through the healthy native inventory.")
                selected = max(common, key=lambda name: len(retained[name]))
                _, headers, raw = self.request("/admin/v1/logs/" + selected + "/download", administrator=True, limit=8192)
                core.require(raw == retained[selected] and {key.lower(): value for key, value in headers.items()}.get("cache-control") == "no-store",
                             "The native download did not return the exact retained bytes after restart.")
                self.verify_browser_revoked()
                activity = self.http_json("/admin/v1/activity?StartIndex=0&Limit=200&Action=settings.updated", administrator=True)
                core.require(activity.get("TotalRecordCount") == 62 and len(activity.get("Items", [])) == 62,
                             "A restart lost committed settings activity.")
                self.report.setdefault("restart_evidence", []).append({"restart": number, "all_public_tables_compared": 29,
                    "activity_entries_preserved": len(before["activity_entries"]), "retained_files_recovered": len(common),
                    "retained_download_bytes": len(raw), "retained_download_sha256": core.digest(raw)})
            assets = self.runtime / "admin"
            core.require({path.relative_to(assets).as_posix(): core.file_digest(path) for path in sorted(assets.rglob("*")) if path.is_file()} == self.ui_files,
                         "The loaded UI assets changed during verification.")
            self.assert_private_logs()
            self.report["checks"].update({"two_restarts_preserved_all_29_public_tables_exactly": True,
                "retained_safe_logs_recovered_and_downloaded_after_both_restarts": True, "revoked_browser_stays_revoked_after_both_restarts": True,
                "original_control_credential_survives_both_restarts": True, "loaded_ui_assets_unchanged": True})

        def sanitize_report(self):
            variants = sorted(secret_variants(self.secrets), key=len, reverse=True)
            def scrub(value):
                if isinstance(value, str):
                    for secret in variants:
                        value = value.replace(secret, "[REDACTED]")
                    return value
                if isinstance(value, list):
                    return [scrub(item) for item in value]
                if isinstance(value, dict):
                    return {scrub(key): scrub(item) for key, item in value.items()}
                return value
            self.report = scrub(self.report)

        def cleanup(self):
            failures = []
            def attempt(name, action):
                try:
                    action()
                    self.report["cleanup"][name] = True
                except Exception as error:
                    failures.append(name)
                    self.report["cleanup"][name] = False
                    self.report["cleanup_errors"][name] = str(error) if isinstance(error, core.VerificationError) else type(error).__name__
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
            attempt("browser_stopped_before_credential_cleanup", stop_browser)
            if hasattr(self, "result_path") and self.result_path.exists():
                attempt("private_browser_secrets_collected", self.load_browser_result)
            if self.session_cleanup_required:
                def available():
                    if self.app is None or self.app.poll() is not None:
                        self.stop_app()
                        self.start_app()
                    # Do not mint a replacement cleanup login. The dedicated
                    # database is always disposed even if an ACK was lost.
                    value = self.http_json("/admin/v1/session", administrator=True)
                    csrf = value.get("CSRFToken")
                    core.require(isinstance(csrf, str) and csrf, "The original cleanup control credential is unavailable.")
                    self.control_csrf = csrf
                    self.secrets.append(csrf)
                attempt("original_control_available_for_cleanup", available)
                attempt("acknowledged_settings_chain_restored", self.restore_settings)
                def revoke():
                    sessions = self.list_sessions()
                    current = [item for item in sessions if item.get("IsCurrent") is True]
                    core.require(len(current) == 1, "The cleanup control identity is ambiguous.")
                    current_id = current[0]["Id"]
                    errors = []
                    for item in sessions:
                        if item["Id"] != current_id and item["Status"] != "revoked":
                            try:
                                value = self.http_json("/admin/v1/sessions/" + item["Id"] + "/revoke", method="POST", administrator=True, body={})
                                core.require(value.get("SessionId") == item["Id"] and value.get("RevokedAt"), "A fixture credential revocation did not commit.")
                            except Exception:
                                errors.append("non_control_revocation")
                    if self.browser_result and isinstance(self.browser_result.get("BrowserCookie"), str):
                        try:
                            self.request("/admin/v1/session", headers={"Cookie": self.browser_result["BrowserCookie"]}, expected=(401,))
                        except Exception:
                            errors.append("browser_invalidity")
                    cookie = self.control_cookie or next(("goby_session=" + entry.value for entry in self.cookies if entry.name == "goby_session"), None)
                    core.require(isinstance(cookie, str) and cookie, "The original control cookie is unavailable for invalidity proof.")
                    try:
                        self.request("/admin/v1/session", method="DELETE", administrator=True, expected=(204,))
                    except Exception:
                        errors.append("control_logout")
                    try:
                        self.request("/admin/v1/session", headers={"Cookie": cookie}, expected=(401,))
                    except Exception:
                        errors.append("control_invalidity")
                    rows = self.full_private_snapshot()
                    core.require(len(rows["sessions"]) == len(sessions) and all(row["revoked_at"] is not None for row in rows["sessions"]),
                                 "A native fixture credential remains unrevoked in the owned database.")
                    core.require(not errors, "One or more independent credential cleanup acknowledgements or invalidity proofs failed.")
                    if self.report["status"] == "passed":
                        core.require(len(sessions) == 2 and Counter(row["action"] for row in rows["activity_entries"]) == {
                            "user.created": 1, "session.login": 2, "settings.updated": 62, "session.revoked": 2},
                            "The final committed audit facts do not match exactly two issued and revoked credentials.")
                    self.report["cleanup"]["native_sessions_revoked_count"] = len(sessions)
                    self.report["cleanup"]["credential_invalidation_http_401_and_sql_proven"] = True
                attempt("all_issued_credentials_revoked_control_logout_last", revoke)
                attempt("final_private_logs_exclude_secrets", self.assert_private_logs)
            if hasattr(self, "shared_before"):
                def preserved():
                    after = self.shared_identity()
                    self.report["shared_identities_after"] = after
                    core.require(after == self.shared_before, "A pre-existing application or reference process changed during verification.")
                attempt("shared_application_and_reference_identities_unchanged", preserved)
            if failures:
                self.report["status"] = "failed"
            try:
                super().cleanup()
            finally:
                if failures:
                    self.report["cleanup_failures"] = failures + self.report.get("cleanup_failures", [])
                    self.report["status"] = "failed"
                self.report["control_http_requests"] = self.http_count
                self.sanitize_report()

    return ObservabilityRunner(args)


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
        raise core.VerificationError("The observability verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Observability verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
