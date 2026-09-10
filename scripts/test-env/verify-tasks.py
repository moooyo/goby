#!/usr/bin/env python3
"""Verify scheduled-task administration in one owned disposable Linux database.

The shared browser runner retains ownership of PostgreSQL, its exact temporary
HBA rule, the unprivileged application, private artifacts, and final cleanup.
This scenario creates two tiny real media files and uses only native task APIs
for mutations. Read-only snapshots cover every public table across two restarts.
"""

import argparse
from datetime import datetime, timedelta, timezone
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
import time
import urllib.error
import urllib.request


FIXTURE_MARKER = "goby-tasks-browser-fixtures-v1"
RESULT_MARKER = "goby-tasks-browser-result-v1"
TASK_KEY = "library.scan"
PUBLIC_TABLES = tuple(sorted("""application_key_clients application_key_devices
application_keys catalog_entities client_playback_references devices encoding_jobs
item_entities item_images item_metadata_state item_subtitles items libraries
library_roots play_sessions scan_jobs schema_migrations server_settings sessions
user_item_data users task_definitions task_occurrences task_run_children
task_run_requests task_runs task_triggers""".split()))
TRIGGER_INPUT_FIELDS = ("Kind", "IntervalTicks", "TimeOfDayTicks", "DayOfWeek", "MaxRuntimeTicks")
TERMINAL_STATES = {"completed", "failed", "cancelled", "interrupted"}
SAFE_HTTP_METHODS = frozenset(("GET", "POST", "PUT", "DELETE"))
SAFE_HTTP_ROUTES = (
    (r"/admin/v1/bootstrap", "/admin/v1/bootstrap"),
    (r"/admin/v1/session", "/admin/v1/session"),
    (r"/admin/v1/sessions", "/admin/v1/sessions"),
    (r"/admin/v1/sessions/[0-9a-f]{32}/revoke", "/admin/v1/sessions/{id}/revoke"),
    (r"/admin/v1/libraries", "/admin/v1/libraries"),
    (r"/admin/v1/libraries/[0-9a-f]{32}", "/admin/v1/libraries/{id}"),
    (r"/admin/v1/libraries/[0-9a-f]{32}/scan", "/admin/v1/libraries/{id}/scan"),
    (r"/admin/v1/libraries/[0-9a-f]{32}/items", "/admin/v1/libraries/{id}/items"),
    (r"/admin/v1/tasks", "/admin/v1/tasks"),
    (r"/admin/v1/tasks/[0-9a-f]{32}", "/admin/v1/tasks/{id}"),
    (r"/admin/v1/tasks/[0-9a-f]{32}/runs", "/admin/v1/tasks/{id}/runs"),
    (r"/admin/v1/tasks/[0-9a-f]{32}/triggers", "/admin/v1/tasks/{id}/triggers"),
    (r"/admin/v1/tasks/[0-9a-f]{32}/triggers/preview", "/admin/v1/tasks/{id}/triggers/preview"),
    (r"/admin/v1/task-runs/[0-9a-f]{32}", "/admin/v1/task-runs/{id}"),
    (r"/admin/v1/task-runs/[0-9a-f]{32}/cancel", "/admin/v1/task-runs/{id}/cancel"),
    (r"/admin/v1/jobs", "/admin/v1/jobs"),
    (r"/admin/v1/jobs/[0-9a-f]{32}/cancel", "/admin/v1/jobs/{id}/cancel"),
)
SAFE_API_ERROR_CODES = frozenset((
    "access_denied", "administrator_required", "authentication_required", "catalog_not_ready",
    "csrf_invalid", "internal_error", "invalid_input", "library_unavailable", "not_found",
    "not_implemented", "not_ready", "origin_denied", "rate_limited", "request_conflict",
    "revision_conflict", "scan_busy", "setup_token_invalid", "task_disabled", "task_unavailable",
    "tasks_not_ready", "unsupported_media_type",
))


def safe_route_template(route):
    # Return only literal templates from this allowlist, never any URL segment,
    # identifier, query value, or fragment supplied by a request.
    pathname = route.split("?", 1)[0].split("#", 1)[0]
    for pattern, template in SAFE_HTTP_ROUTES:
        if re.fullmatch(pattern, pathname):
            return template
    return "/admin/v1/{unrecognized-route}"


def safe_api_error_code(raw):
    # The bounded response stays private; only a known constant may be exported.
    if len(raw) > 4096:
        return None
    try:
        value = json.loads(raw)
    except (ValueError, TypeError):
        return None
    error = value.get("Error") if isinstance(value, dict) else None
    code = error.get("Code") if isinstance(error, dict) else None
    return code if isinstance(code, str) and code in SAFE_API_ERROR_CODES else None


def load_core(path):
    # Import the shared Runner read-only without executing its CLI or writing
    # bytecode into the prepared source snapshot.
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_tasks_browser_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def valid_id(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{32}", value) is not None


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"))


def timestamp(value):
    if not isinstance(value, str):
        raise ValueError("A timestamp must be a string.")
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if result.tzinfo is None:
        raise ValueError("A timestamp must include its timezone.")
    return result.astimezone(timezone.utc)


def create_runner(core, args):
    core.MARKER = "goby-tasks-browser-v1"
    core.RUN_ENV = "GOBY_TASKS_RUN_ID"

    class TasksRunner(core.Runner):
        browser_spec = "scheduled-tasks.spec.ts"
        browser_timeout_seconds = 600
        screenshot_names = ("tasks-desktop.png", "tasks-schedule-desktop.png",
                            "tasks-mobile.png", "tasks-scan-history.png")

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m5f_tasks_" + self.run_id
            self.role = "goby_m5f_role_" + self.run_id
            self.pg_app_name = "goby_m5f_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-tasks-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-tasks-" + self.run_id)
            self.admin_name = "m5f-admin-" + self.run_id
            self.cookies = http.cookiejar.CookieJar()
            self.admin_opener = urllib.request.build_opener(
                urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(self.cookies))
            self.control_csrf = None
            self.control_session_id = None
            self.session_cleanup_required = False
            self.fixture = None
            self.browser_result = None
            self.task_id = None
            self.media_files = {}
            self.report.update({"scenario": "administrator_scheduled_tasks", "database": self.database,
                                "role": self.role})

        def allocate(self):
            super().allocate()
            core.require(self.goby.pw_uid == 995 and os.fstat(self.binary_fd).st_mode & 0o001,
                         "The isolated executable must be runnable by the reviewed UID 995.")
            core.require(self.report["shared_service_before"] == {"MainPID": "3535438", "ActiveState": "active"},
                         "The reviewed shared application service identity changed; no shared service was modified.")
            core.require(core.free_bytes(core.EXEC_ROOT) >= 128 * 1024 * 1024 and
                         core.free_bytes(Path("/dev/shm")) >= 96 * 1024 * 1024,
                         "Insufficient headroom for the task browser, database, and screenshots.")
            self.report["verifier_sha256"] = core.file_digest(Path(__file__).resolve())
            self.report["shared_runner_sha256"] = core.file_digest(args.core_runner.resolve())

        def prepare_files(self):
            super().prepare_files()
            (self.browser_work / "src").mkdir(mode=0o700)
            shutil.copyfile(self.args.snapshot / "web/admin/src/api.ts", self.browser_work / "src/api.ts")
            assets = self.runtime / "admin"
            for directory in [assets, *(entry for entry in assets.rglob("*") if entry.is_dir())]:
                directory.chmod(0o755)
            media = self.runtime / "media"
            # The scanner can read the fixtures but cannot replace their files
            # or parent directories. Application scratch remains isolated.
            for directory in (self.runtime, media):
                os.chown(directory, 0, 0)
                directory.chmod(0o755)
            for number, name in enumerate(("first", "second")):
                directory = media / name
                directory.mkdir(mode=0o755)
                # mkdir's mode is filtered by the operator's umask. Explicitly
                # restore read/traverse access for the UID-995 application.
                os.chown(directory, 0, 0)
                directory.chmod(0o755)
                status = directory.stat()
                core.require(status.st_uid == 0 and status.st_gid == 0 and status.st_mode & 0o777 == 0o755,
                             "An owned library fixture directory lacks its required application access mode.")
                path = directory / ("Task." + name.title() + ".2026.mp4")
                core.command([
                    "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
                    "-f", "lavfi", "-i", f"color=c={'blue' if number == 0 else 'green'}:s=160x90:r=10:d=1",
                    "-f", "lavfi", "-i", f"sine=frequency={440 + number * 110}:sample_rate=48000:duration=1",
                    "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-preset", "ultrafast",
                    "-crf", "28", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "32k",
                    "-threads", "1", "-filter_threads", "1", "-shortest", "-movflags", "+faststart", path,
                ], timeout=30)
                core.require(0 < path.stat().st_size <= 128 * 1024,
                             "A real media fixture exceeded its byte budget.")
                os.chown(path, 0, 0)
                path.chmod(0o444)
                self.media_files[str(path)] = core.file_digest(path)
            self.report["checks"]["owned_library_directories_explicit_mode_0755"] = True
            self.manifest = self.browser_work / "tasks-fixture.json"
            self.result_path = self.browser_work / "tasks-result.json"

        def http_failure(self, request, route, status, failure, code=None):
            method = request.get_method()
            diagnostic = {
                "Method": method if method in SAFE_HTTP_METHODS else "OTHER",
                "RouteTemplate": safe_route_template(route),
                "Status": status if isinstance(status, int) and 100 <= status <= 599 else None,
                "Failure": failure if failure in ("http_error", "unexpected_status", "transport_error",
                                                    "invalid_json", "response_too_large") else "request_error",
            }
            if isinstance(code, str) and code in SAFE_API_ERROR_CODES:
                diagnostic["Code"] = code
            failures = self.report.setdefault("http_failures", [])
            if len(failures) < 32:
                failures.append(diagnostic)
            label = f"{diagnostic['Method']} {diagnostic['RouteTemplate']}"
            status_label = str(diagnostic["Status"]) if diagnostic["Status"] is not None else "unavailable"
            code_label = "; code=" + diagnostic["Code"] if "Code" in diagnostic else ""
            return core.VerificationError(
                f"Isolated request failed: {label}; status={status_label}; failure={diagnostic['Failure']}{code_label}.")

        def http_json(self, route, *, body=None, method=None, headers=None, administrator=False, expected=(200, 201), timeout=5):
            core.require(route.startswith("/admin/v1/") and not route.startswith("//"),
                         "Task fixture requests must use the isolated native administrator API.")
            request_headers = {"Accept": "application/json", "Origin": self.origin}
            request_headers.update(headers or {})
            data = None if body is None else json.dumps(body).encode("utf-8")
            if data is not None:
                request_headers["Content-Type"] = "application/json"
                if administrator and self.control_csrf:
                    request_headers["X-CSRF-Token"] = self.control_csrf
            request = urllib.request.Request(self.origin + route, data=data, headers=request_headers, method=method)
            opener = self.admin_opener if administrator else urllib.request.build_opener(urllib.request.ProxyHandler({}))
            status = None
            try:
                with opener.open(request, timeout=timeout) as response:
                    status = response.status
                    raw = response.read(1024 * 1024 + 1)
                    if status not in expected:
                        raise self.http_failure(request, route, status, "unexpected_status", safe_api_error_code(raw))
                    if len(raw) > 1024 * 1024:
                        raise self.http_failure(request, route, status, "response_too_large")
                    try:
                        return json.loads(raw)
                    except ValueError:
                        raise self.http_failure(request, route, status, "invalid_json") from None
            except urllib.error.HTTPError as error:
                # Never retain the error object, headers, body, or exception text.
                code = None
                try:
                    code = safe_api_error_code(error.read(4097))
                except (OSError, ValueError, http.client.HTTPException):
                    pass
                finally:
                    error.close()
                raise self.http_failure(request, route, error.code, "http_error", code) from None
            except (OSError, http.client.HTTPException):
                raise self.http_failure(request, route, status, "transport_error") from None
            finally:
                self.secrets.extend(cookie.value for cookie in self.cookies if cookie.value not in self.secrets)

        def control_login(self):
            result = self.http_json("/admin/v1/session", administrator=True, body={
                "Name": self.admin_name, "Password": self.admin_password,
            })
            csrf = result.get("CSRFToken")
            if isinstance(csrf, str) and csrf:
                self.secrets.append(csrf)
            core.require(isinstance(csrf, str) and csrf and valid_id(result.get("User", {}).get("Id")),
                         "The dedicated administrator did not receive a control session.")
            self.control_csrf = csrf
            current = [item for item in self.list_sessions() if item.get("IsCurrent") is True]
            core.require(len(current) == 1 and valid_id(current[0].get("Id")),
                         "The native control session identity is ambiguous.")
            self.control_session_id = current[0]["Id"]
            return result["User"]

        def list_sessions(self):
            result = []
            total = None
            for start in range(0, 2000, 200):
                value = self.http_json(f"/admin/v1/sessions?Status=all&StartIndex={start}&Limit=200", administrator=True)
                items = value.get("Items")
                count = value.get("TotalRecordCount")
                core.require(isinstance(items, list) and isinstance(count, int) and 0 <= count <= 2000
                             and (total is None or total == count) and value.get("StartIndex") == start
                             and value.get("Limit") == 200 and len(items) == min(200, max(0, count - start))
                             and all(valid_id(item.get("Id")) for item in items),
                             "The complete isolated login inventory exceeded its bounded coherent pages.")
                total = count
                result.extend(items)
                if len(result) == count:
                    core.require(len({item["Id"] for item in result}) == count,
                                 "The isolated login inventory contained duplicate records.")
                    return result
            raise core.VerificationError("The complete isolated login inventory exceeded its record budget.")

        def bootstrap(self):
            super().bootstrap()
            # Set intent before login: a lost response can still leave a real
            # issued session that final cleanup must discover and revoke.
            self.session_cleanup_required = True
            administrator = self.control_login()
            libraries = []
            self.report["checks"]["native_fixture_libraries_created"] = 0
            for name in ("first", "second"):
                path = str(self.runtime / "media" / name)
                library_name = "Task " + name.title() + " " + self.run_id
                response = self.http_json("/admin/v1/libraries", administrator=True, body={
                    "Name": library_name, "CollectionType": "movies", "Paths": [path], "Scan": False,
                }, expected=(201,))
                item = response.get("Library", {})
                core.require(set(response) == {"Library"} and valid_id(item.get("Id"))
                             and item.get("Name") == library_name and item.get("Paths") == [path]
                             and item.get("LastScanAt") is None,
                             "A native task fixture library was not created without scanning.")
                libraries.append({"Id": item["Id"], "Name": library_name, "Path": path, "FileCount": 1})
                self.report["checks"]["native_fixture_libraries_created"] = len(libraries)
            definitions = self.http_json("/admin/v1/tasks", administrator=True)
            items = definitions.get("Items")
            core.require(isinstance(items, list) and definitions.get("TotalRecordCount") == len(items),
                         "The native task definition inventory is invalid.")
            matching = [item for item in items if item.get("Key") == TASK_KEY]
            core.require(len(matching) == 1 and valid_id(matching[0].get("Id"))
                         and matching[0].get("Enabled") is True and matching[0].get("Triggers") == []
                         and matching[0].get("CurrentRun") is None and matching[0].get("LastRun") is None,
                         "The fresh library task must have a stable identity and no fabricated schedule or history.")
            self.task_id = matching[0]["Id"]
            self.fixture = {"Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                            "Administrator": {"Id": administrator["Id"], "Name": self.admin_name},
                            "TaskId": self.task_id, "TaskKey": TASK_KEY, "Libraries": libraries,
                            "MediaSHA256": self.media_files, "ResultPath": str(self.result_path)}
            core.private_write(self.manifest, (canonical(self.fixture) + "\n").encode())
            initial = self.full_private_snapshot()
            core.require(not initial["task_runs"] and not initial["scan_jobs"]
                         and len(initial["libraries"]) == 2,
                         "Fixture preparation fabricated scan history or escaped the two owned libraries.")
            self.report["fixtures"] = {"libraries": 2, "real_mp4_files": 2, "files_per_library": 1,
                                       "media_bytes": sum(Path(path).stat().st_size for path in self.media_files),
                                       "root_owned_read_only_to_application": True, "native_api_creation": True,
                                       "initial_scan_disabled": True, "private_manifest": True}

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_TASKS_DISPOSABLE_DATABASE": "1",
                                "GOBY_SMOKE_TASKS_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_TASKS_FIXTURE_MANIFEST": str(self.manifest)})
            return environment

        def load_browser_result(self):
            value = json.loads(core.private_file(self.result_path))
            core.require(isinstance(value, dict), "The private task browser result is invalid.")
            # Record newly issued secrets even when the journey is incomplete.
            # Raw result contents never enter the exported report.
            secrets = value.get("BrowserSecrets", [])
            for field in ("BrowserCookie", "BrowserCSRF"):
                secret = value.get(field)
                if isinstance(secret, str) and 0 < len(secret) <= 8192:
                    self.secrets.append(secret)
                    if field == "BrowserCookie" and secret.startswith("goby_session="):
                        self.secrets.append(secret.split("=", 1)[1])
            core.require(isinstance(secrets, list) and len(secrets) <= 16
                         and all(isinstance(secret, str) and 0 < len(secret) <= 8192 for secret in secrets),
                         "The private browser session secret inventory is invalid.")
            self.secrets.extend(secrets)
            core.require(value.get("Marker") == RESULT_MARKER and value.get("RunId") == self.run_id,
                         "The private task browser result does not match this run.")
            self.browser_result = value

        def sanitize_report(self):
            def scrub(value):
                if isinstance(value, str):
                    return core.sanitize_text(value, self.secrets)
                if isinstance(value, list):
                    return [scrub(item) for item in value]
                if isinstance(value, dict):
                    return {key: scrub(item) for key, item in value.items()}
                return value
            # Preserve the independently prepared artifact digests used to
            # identify this runner, source spec, binary, assets, and exact HBA.
            diagnostics = ("failure", "cleanup_errors", "application_diagnostics", "browser_driver_stdout",
                           "browser_driver_stderr", "browser")
            for key in diagnostics:
                if key in self.report:
                    self.report[key] = scrub(self.report[key])

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
            core.require(value is not None and value.get("Complete") is True and value.get("TaskId") == self.task_id
                         and value.get("Libraries") == self.fixture["Libraries"]
                         and valid_id(value.get("BrowserSessionId")) and valid_id(value.get("ManualRunId"))
                         and valid_id(value.get("ReceiptRunId")),
                         "The browser did not complete the owned native task journey.")
            request_id = value.get("ReceiptRequestId")
            intervals = value.get("IntervalRunIds")
            checks = value.get("Checks")
            observations = value.get("Observations")
            core.require(isinstance(request_id, str) and 0 < len(request_id.encode("utf-8")) <= 128
                         and request_id.strip() == request_id and not any(ord(char) < 32 for char in request_id)
                         and isinstance(intervals, list) and intervals and all(valid_id(item) for item in intervals)
                         and len(set(intervals)) == len(intervals) and isinstance(checks, dict) and checks
                         and all(re.fullmatch(r"[A-Za-z][A-Za-z0-9_]{0,95}", key) and item is True
                                 for key, item in checks.items()),
                         "The browser result is missing its real execution, request receipt, or acceptance checks.")
            core.require(isinstance(observations, dict)
                         and isinstance(observations.get("ActiveStopDialogObserved"), bool),
                         "The browser result is missing its explicit active-stop observation boundary.")
            core.require(isinstance(value.get("BrowserCookie"), str)
                         and re.fullmatch(r"goby_session=[A-Za-z0-9_-]{32,4096}", value["BrowserCookie"])
                         and isinstance(value.get("BrowserCSRF"), str) and value["BrowserCSRF"]
                         and value["BrowserCSRF"] in value["BrowserSecrets"],
                         "The private result lacks the original browser administrator credentials.")
            task = self.task()
            core.require(task.get("Revision") == value.get("FinalRevision")
                         and task.get("Triggers") == value.get("FinalTriggers")
                         and task.get("ScheduleTimezone") == value.get("FinalTimezone") == "UTC"
                         and task.get("CurrentRun") is None,
                         "The browser left an active execution or uncommitted schedule state.")
            self.require_future_calendar(task)
            core.require(all((self.output / name).is_file() for name in self.screenshot_names),
                         "The browser did not produce all four explicit secret-free screenshots.")
            self.assert_private_logs()
            self.report["browser_checks"] = checks
            self.report["browser_observations"] = {
                "active_stop_dialog_observed": observations["ActiveStopDialogObserved"],
                "active_cancel_executed": False,
            }
            self.report["checks"].update({"browser_native_task_journey": True, "secret_free_screenshots": 4,
                                          "real_manual_and_interval_runs": True})

        def task(self):
            value = self.http_json("/admin/v1/tasks/" + self.task_id, administrator=True).get("Task", {})
            core.require(value.get("Id") == self.task_id and value.get("Key") == TASK_KEY,
                         "The stable native library task identity changed.")
            return value

        def require_future_calendar(self, task):
            triggers = task.get("Triggers", [])
            core.require(len(triggers) == 2 and sorted(trigger.get("Kind") for trigger in triggers) == ["daily", "weekly"]
                         and task.get("ScheduleTimezone") == "UTC"
                         and all(trigger.get("CalculationError") == "" and
                                 timestamp(trigger.get("NextFireAt")) > datetime.now(timezone.utc) + timedelta(minutes=10)
                                 for trigger in triggers),
                         "Restart verification requires two distant calendar rules and no startup or interval rule.")

        def owned_database_read(self, statement):
            core.require(self.database_oid is not None and self.lock is not None
                         and re.fullmatch(r"goby_m5f_tasks_[0-9]{8}_[0-9]{6}_[0-9a-f]{10}", self.database)
                         and self.database == "goby_m5f_tasks_" + self.run_id
                         and self.role == "goby_m5f_role_" + self.run_id,
                         "Task persistence inspection requires this run's canonical owned database.")
            identity = core.pg_json(f"""SELECT jsonb_build_object('oid', d.oid::bigint,
                'owner', pg_get_userbyid(d.datdba), 'tag', shobj_description(d.oid, 'pg_database'),
                'role_tag', shobj_description(r.oid, 'pg_authid'))
                FROM pg_database d JOIN pg_roles r ON r.oid = d.datdba WHERE d.datname = '{self.database}';""")
            core.require(identity == {"oid": self.database_oid, "owner": self.role,
                                      "tag": self.tag, "role_tag": self.tag},
                         "The owned task database or role identity changed.")
            guarded = f"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
                SET LOCAL statement_timeout = '10s';
                DO $guard$ BEGIN
                    IF current_database() <> '{self.database}' OR current_user <> '{self.role}' THEN
                        RAISE EXCEPTION 'Unexpected task fixture database or role';
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
                         "The owned task database snapshot exceeded its byte budget.")
            value = json.loads(raw)
            core.require(value.get("Tables") == list(PUBLIC_TABLES) and len(PUBLIC_TABLES) == 27
                         and set(value.get("Rows", {})) == set(PUBLIC_TABLES),
                         "The persistence snapshot did not cover exactly all 27 public tables.")
            # Credential-bearing snapshots remain in memory. Collect persisted
            # hashes as redaction inputs; publish neither rows nor row digests.
            for rows in value["Rows"].values():
                for row in rows:
                    for key, secret in row.items():
                        if isinstance(secret, str) and secret and any(part in key for part in ("password", "token", "secret", "fingerprint")):
                            self.secrets.append(secret)
                            if secret.startswith("\\x"):
                                self.secrets.append(secret[2:])
            self.report["checks"]["restart_snapshot_public_table_count"] = len(PUBLIC_TABLES)
            self.report["snapshot_tables"] = list(PUBLIC_TABLES)
            return value["Rows"]

        def assert_private_logs(self):
            for name in ("app-private.log", "browser-private.json", "browser-private.stderr"):
                path = self.output / name
                core.require(path.stat().st_size <= 8 * 1024 * 1024,
                             "A private verification log exceeded its byte budget.")
                text = path.read_text(encoding="utf-8", errors="replace")
                core.require(not any(secret and secret in text for secret in self.secrets),
                             "A verification log contained a raw authentication secret; it will not be exported.")
            self.report["checks"]["private_logs_exclude_raw_secrets"] = True

        def assert_media_unchanged(self):
            core.require({path: core.file_digest(Path(path)) for path in self.media_files} == self.media_files,
                         "Task execution changed the original real media bytes.")
            for name in ("first", "second"):
                directory = self.runtime / "media" / name
                files = list(directory.iterdir())
                core.require(len(files) == 1 and str(files[0]) in self.media_files
                             and directory.stat().st_uid == 0 and not directory.stat().st_mode & 0o022
                             and files[0].stat().st_uid == 0 and files[0].stat().st_mode & 0o777 == 0o444,
                             "The root-owned media fixture inventory or read-only permissions changed.")

        def completed_run(self, run_id, *, source=None, cached=False):
            value = self.http_json("/admin/v1/task-runs/" + run_id + "?StartIndex=0&Limit=200", administrator=True)
            run, children = value.get("Run", {}), value.get("Children", {})
            items = children.get("Items", [])
            core.require(run.get("Id") == run_id and run.get("TaskId") == self.task_id
                         and run.get("State") == "completed" and (source is None or run.get("Source") == source)
                         and run.get("TotalChildren") == run.get("TerminalChildren") == run.get("CompletedChildren") == 2
                         and all(run.get(field) == 0 for field in ("FailedChildren", "CancelledChildren", "InterruptedChildren", "UnavailableChildren"))
                         and run.get("Scanned") == 2 and run.get("ErrorCode") == "" and run.get("ErrorMessage") == ""
                         and children.get("StartIndex") == 0 and children.get("Limit") == 200
                         and children.get("TotalRecordCount") == len(items) == 2,
                         "A real task execution did not complete both owned library children.")
            expected = {item["Id"]: item for item in self.fixture["Libraries"]}
            core.require({item.get("LibraryId") for item in items} == set(expected)
                         and len({item.get("Id") for item in items}) == 2
                         and all(valid_id(item.get("Id")) and item.get("RunId") == run_id
                                 and item.get("LibraryName") == expected[item["LibraryId"]]["Name"]
                                 and item.get("State") == "completed" and valid_id(item.get("ScanJobId"))
                                 and item.get("Scanned") == expected[item["LibraryId"]]["FileCount"]
                                 and item.get("ErrorCode") == "" and item.get("ErrorMessage") == ""
                                 for item in items),
                         "A completed task lost its exact library snapshots or linked scan jobs.")
            if cached:
                core.require(run.get("Added") == run.get("Updated") == 0
                             and all(item.get("Added") == item.get("Updated") == 0 for item in items),
                             "The startup cached scan unexpectedly changed media catalog content.")
            return value

        def replay_receipt(self):
            value = self.http_json("/admin/v1/tasks/" + self.task_id + "/runs", administrator=True,
                                   body={"RequestId": self.browser_result["ReceiptRequestId"]}, expected=(202,))
            core.require(value.get("Admitted") is False
                         and value.get("Run", {}).get("Id") == self.browser_result["ReceiptRunId"],
                         "The original request receipt admitted a duplicate execution after restart.")

        def verify_browser_session(self):
            result = self.browser_result
            original = self.http_json("/admin/v1/session", headers={"Cookie": result["BrowserCookie"]})
            core.require(original.get("User", {}).get("Id") == self.fixture["Administrator"]["Id"]
                         and original.get("CSRFToken") == result["BrowserCSRF"],
                         "The original browser cookie or CSRF identity was not preserved across restart.")
            sessions = self.list_sessions()
            browser = next((item for item in sessions if item["Id"] == result["BrowserSessionId"]), None)
            core.require(browser is not None and browser.get("Status") == "active"
                         and browser.get("UserId") == self.fixture["Administrator"]["Id"],
                         "The original browser administrator login is no longer active.")

        def replace_triggers(self, triggers):
            current = self.task()
            result = self.http_json("/admin/v1/tasks/" + self.task_id + "/triggers", method="PUT",
                                    administrator=True, body={"Revision": current["Revision"],
                                    "ScheduleTimezone": "UTC", "Triggers": triggers}).get("Task", {})
            core.require(result.get("Id") == self.task_id and result.get("ScheduleTimezone") == "UTC"
                         and len(result.get("Triggers", [])) == len(triggers),
                         "The real native trigger replacement did not commit.")
            return result

        def wait_no_work(self, *, cancel=False, timeout=30):
            core.require(valid_id(self.task_id) and self.fixture is not None,
                         "Work draining requires the owned task and library fixture identities.")
            libraries = {item["Id"] for item in self.fixture["Libraries"]}
            deadline = time.monotonic() + timeout
            observed_runs, requested_runs, requested_jobs = set(), set(), set()
            cancellation_errors = []
            timeout_message = "Owned task or scan work did not reach a terminal state before the cleanup deadline."
            def request(route, **options):
                remaining = deadline - time.monotonic()
                core.require(remaining > 0, timeout_message)
                return self.http_json(route, administrator=True, timeout=min(5, remaining), **options)
            while True:
                definition = request("/admin/v1/tasks/" + self.task_id).get("Task", {})
                core.require(definition.get("Id") == self.task_id and definition.get("Key") == TASK_KEY,
                             "The owned task identity changed while draining work.")
                current = definition.get("CurrentRun")
                if current is not None:
                    core.require(valid_id(current.get("Id")) and current.get("TaskId") == self.task_id
                                 and current.get("State") in ("pending", "running", "stopping"),
                                 "The owned current task run has an invalid active state.")
                    observed_runs.add(current["Id"])
                    if cancel and current["Id"] not in requested_runs:
                        requested_runs.add(current["Id"])
                        try:
                            result = request("/admin/v1/task-runs/" + current["Id"] + "/cancel",
                                             body={}, expected=(202,)).get("Run", {})
                            core.require(result.get("Id") == current["Id"] and result.get("TaskId") == self.task_id
                                         and result.get("State") in TERMINAL_STATES | {"stopping"},
                                         "A native task cancellation did not acknowledge the owned run.")
                        except core.VerificationError:
                            cancellation_errors.append("task")
                page = request("/admin/v1/jobs")
                jobs = page.get("Items")
                core.require(isinstance(jobs, list) and page.get("TotalRecordCount") == len(jobs)
                             and len(jobs) <= 1000 and len({job.get("Id") for job in jobs}) == len(jobs)
                             and all(valid_id(job.get("Id")) and valid_id(job.get("LibraryId"))
                                     and job.get("Status") in TERMINAL_STATES | {"pending", "running"} for job in jobs),
                             "The native scan inventory is invalid or exceeds its bounded result window.")
                owned_jobs = [job for job in jobs if job["LibraryId"] in libraries]
                active_jobs = [job for job in owned_jobs if job["Status"] in ("pending", "running")]
                if cancel:
                    for job in active_jobs:
                        if job["Id"] in requested_jobs:
                            continue
                        requested_jobs.add(job["Id"])
                        try:
                            result = request("/admin/v1/jobs/" + job["Id"] + "/cancel",
                                             body={}, expected=(202,)).get("Job", {})
                            core.require(result.get("Id") == job["Id"] and result.get("LibraryId") == job["LibraryId"]
                                         and result.get("Status") in TERMINAL_STATES | {"pending", "running"},
                                         "A native scan cancellation did not acknowledge the owned scan job.")
                        except core.VerificationError:
                            cancellation_errors.append("scan")
                if current is None and not active_jobs:
                    # The native jobs API has a fixed 1000-row window. Refuse
                    # to claim a complete drain when that window is saturated.
                    core.require(len(jobs) < 1000 and len(owned_jobs) == len(jobs),
                                 "The scan inventory is incomplete or contains a non-fixture library.")
                    for run_id in observed_runs:
                        result = request("/admin/v1/task-runs/" + run_id + "?StartIndex=0&Limit=200").get("Run", {})
                        core.require(result.get("Id") == run_id and result.get("TaskId") == self.task_id
                                     and result.get("State") in TERMINAL_STATES,
                                     "An observed task run did not retain a terminal state after draining.")
                    core.require(definition.get("Triggers") == [], "Task rules remained installed after work draining.")
                    core.require(not cancellation_errors,
                                 "One or more native task or scan cancellation requests failed during work draining.")
                    return
                remaining = deadline - time.monotonic()
                core.require(remaining > 0, timeout_message)
                time.sleep(min(0.2, remaining))

        def assert_preserved_rows(self, before, after, table, *, additions=0, ignored=()):
            old = {row["id"]: row for row in before[table]}
            new = {row["id"]: row for row in after[table]}
            core.require(len(old) == len(before[table]) and len(new) == len(after[table])
                         and set(old).issubset(new) and len(new) == len(old) + additions,
                         "A restart changed the expected persistent row identities.")
            for identity, row in old.items():
                comparable = lambda value: {key: item for key, item in value.items() if key not in ignored}
                core.require(comparable(row) == comparable(new[identity]),
                             "A restart changed existing task or scan history content.")
            return [new[identity] for identity in sorted(set(new) - set(old))]

        def compare_startup_snapshot(self, before, after, startup_id):
            changed = {"libraries", "task_triggers", "task_runs", "task_run_children", "task_occurrences", "scan_jobs"}
            core.require(all(before[name] == after[name] for name in PUBLIC_TABLES if name not in changed),
                         "Startup changed account, session, catalog, metadata, user data, receipt, or definition rows.")
            runs = self.assert_preserved_rows(before, after, "task_runs", additions=1)
            children = self.assert_preserved_rows(before, after, "task_run_children", additions=2)
            occurrences = self.assert_preserved_rows(before, after, "task_occurrences", additions=1)
            jobs = self.assert_preserved_rows(before, after, "scan_jobs", additions=2)
            run = runs[0]
            core.require(run["id"] == startup_id and run["task_id"] == self.task_id
                         and run["source"] == "startup" and run["state"] == "completed"
                         and run["total_children"] == run["terminal_children"] == run["completed_children"] == 2
                         and run["scanned"] == 2 and run["added"] == run["updated"] == 0
                         and not run["actor_user_id"] and not run["actor_session_id"] and not run["actor_kind"]
                         and run["request_id"] is None and run["request_fingerprint"] is None,
                         "The new startup row is not a genuine successful scheduler admission.")
            libraries = {item["Id"]: item for item in self.fixture["Libraries"]}
            core.require({child["library_id"] for child in children} == set(libraries)
                         and {child["ordinal"] for child in children} == {0, 1}
                         and all(child["run_id"] == startup_id and child["state"] == "completed"
                                 and child["library_name"] == libraries[child["library_id"]]["Name"]
                                 and child["scanned"] == 1 and child["added"] == child["updated"] == 0
                                 and not child["error_code"] and not child["error_message"] for child in children),
                         "The startup children do not match the exact two-library snapshot.")
            jobs_by_id = {job["id"]: job for job in jobs}
            core.require({child["scan_job_id"] for child in children} == set(jobs_by_id),
                         "The startup scan jobs do not belong exclusively to the new children.")
            for child in children:
                job = jobs_by_id[child["scan_job_id"]]
                core.require(job["task_child_id"] == child["id"] and job["library_id"] == child["library_id"]
                             and job["status"] == "Completed" and job["scanned"] == 1
                             and job["added"] == job["updated"] == 0 and not job["error"]
                             and job["force_probe"] is False and job["cancel_requested"] is False,
                             "A startup child did not complete its own normal cached scan.")
            occurrence = occurrences[0]
            core.require(occurrence["task_id"] == self.task_id and occurrence["run_id"] == startup_id
                         and occurrence["trigger_id"] == run["trigger_id"]
                         and occurrence["schedule_revision"] == run["trigger_revision"]
                         and occurrence["due_at"] == run["scheduled_for"]
                         and occurrence["disposition"] == "admitted" and occurrence["occurrence_count"] == 1,
                         "The startup occurrence receipt does not identify its admitted run and trigger revision.")
            self.assert_preserved_rows(before, after, "libraries", ignored=("last_scan_at",))
            old_libraries = {row["id"]: row for row in before["libraries"]}
            for row in after["libraries"]:
                core.require(row["id"] in libraries and
                             timestamp(row["last_scan_at"]) > timestamp(old_libraries[row["id"]]["last_scan_at"]),
                             "Only the two scanned fixture libraries may advance their scan watermark.")
            old_triggers = {row["id"]: row for row in before["task_triggers"]}
            new_triggers = {row["id"]: row for row in after["task_triggers"]}
            core.require(set(old_triggers) == set(new_triggers), "Restart replaced persistent trigger identities.")
            for identity, prior in old_triggers.items():
                current = new_triggers[identity]
                if identity != run["trigger_id"]:
                    core.require(prior == current, "Restart changed a calendar or retired trigger.")
                    continue
                core.require(prior["kind"] == "startup" and prior["retired_at"] is None
                             and prior["last_due_at"] is None and current["last_due_at"] == run["scheduled_for"]
                             and timestamp(current["updated_at"]) >= timestamp(prior["updated_at"])
                             and {key: value for key, value in prior.items() if key not in ("last_due_at", "updated_at")} ==
                                 {key: value for key, value in current.items() if key not in ("last_due_at", "updated_at")},
                             "The startup trigger changed more than its explicit dispatch watermarks.")

        def verify_restart(self):
            initial_task = self.task()
            self.require_future_calendar(initial_task)
            core.require(initial_task.get("CurrentRun") is None, "A task was still active before the exact restart.")
            self.stop_app()
            before = self.full_private_snapshot()
            self.start_app()
            # No authenticated HTTP request may precede this exact comparison:
            # even legitimate session-presence updates are not excluded here.
            after = self.full_private_snapshot()
            core.require(before == after, "The first restart changed persisted rows in the complete 27-table snapshot.")
            self.assert_media_unchanged()
            core.require(core.request_json(self.origin, "/admin/v1/bootstrap") == {"Initialized": True},
                         "The isolated application lost bootstrap state after restart.")
            self.verify_browser_session()
            self.replay_receipt()
            self.completed_run(self.browser_result["ManualRunId"], source="manual")
            self.completed_run(self.browser_result["ReceiptRunId"], source="manual")
            for run_id in self.browser_result["IntervalRunIds"]:
                self.completed_run(run_id, source="schedule")
            current = self.task()
            core.require(current == initial_task, "The first restart or receipt replay changed the exact task projection.")
            self.report["checks"].update({"first_restart_preserved_all_27_public_tables_exactly": True,
                                          "original_browser_session_survives_restart": True,
                                          "original_manual_receipt_replays_without_admission": True})

            calendar = [{field: trigger[field] for field in TRIGGER_INPUT_FIELDS} for trigger in current["Triggers"]]
            scheduled = self.replace_triggers(calendar + [{"Kind": "startup"}])
            core.require(scheduled.get("CurrentRun") is None and
                         sorted(trigger["Kind"] for trigger in scheduled["Triggers"]) == ["daily", "startup", "weekly"],
                         "The startup rule was not installed through the native API without starting an immediate run.")
            self.stop_app()
            baseline = self.full_private_snapshot()
            known_runs = {row["id"] for row in baseline["task_runs"]}
            startup_trigger = next(trigger for trigger in scheduled["Triggers"] if trigger["Kind"] == "startup")
            core.require(valid_id(startup_trigger.get("Id")), "The native startup trigger identity is invalid.")
            self.start_app()
            startup_id = None
            def startup_finished():
                nonlocal startup_id
                values = json.loads(self.owned_database_read(
                    "SELECT COALESCE(jsonb_agg(jsonb_build_object('id', id, 'state', state)), '[]'::jsonb) "
                    f"FROM task_runs WHERE task_id = '{self.task_id}' AND source = 'startup' "
                    f"AND trigger_id = '{startup_trigger['Id']}';"))
                fresh = [value for value in values if value["id"] not in known_runs]
                core.require(len(fresh) <= 1, "One startup event admitted duplicate task runs.")
                if not fresh or fresh[0]["state"] not in TERMINAL_STATES:
                    return False
                core.require(fresh[0]["state"] == "completed", "The real startup execution did not complete successfully.")
                startup_id = fresh[0]["id"]
                return True
            core.wait_until(startup_finished, "The new startup execution did not finish within its bounded time.", timeout=60)
            startup_state = self.full_private_snapshot()
            self.compare_startup_snapshot(baseline, startup_state, startup_id)
            confirmed = self.task()
            core.require(confirmed.get("Id") == scheduled["Id"]
                         and confirmed.get("Revision") == scheduled.get("Revision")
                         and confirmed.get("Triggers") == scheduled.get("Triggers")
                         and confirmed.get("ScheduleTimezone") == "UTC" and confirmed.get("CurrentRun") is None,
                         "The second restart changed task identity, committed rules, revision, or timezone.")
            self.completed_run(startup_id, source="startup", cached=True)
            self.replay_receipt()
            cleared = self.replace_triggers([])
            core.require(cleared.get("Triggers") == [],
                         "The isolated task rules were not cleared after startup verification.")
            self.wait_no_work()
            self.verify_browser_session()
            self.assert_media_unchanged()
            self.assert_private_logs()
            self.report["checks"].update({"second_restart_admitted_one_real_startup_run": True,
                                          "startup_completed_two_owned_library_children": True,
                                          "startup_preserved_existing_history_and_receipts": True,
                                          "startup_preserved_all_other_public_rows": True,
                                          "startup_allowed_only_explicit_scan_and_dispatch_watermarks": True,
                                          "startup_normal_cached_scan_added_or_updated_no_items": True,
                                          "original_media_bytes_unchanged": True,
                                          "all_task_triggers_cleared": True,
                                          "all_task_and_scan_work_settled_after_rule_removal": True})

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
            attempt("browser_stopped_before_api_cleanup", stop_browser)
            if hasattr(self, "result_path") and self.result_path.exists():
                attempt("private_browser_secrets_collected", self.load_browser_result)
            if self.session_cleanup_required:
                def ensure_control():
                    if self.app is None or self.app.poll() is not None:
                        self.stop_app()
                        self.start_app()
                    try:
                        session = self.http_json("/admin/v1/session", administrator=True)
                        csrf = session.get("CSRFToken")
                        core.require(isinstance(csrf, str) and csrf,
                                     "The cleanup control session did not return its CSRF credential.")
                        self.control_csrf = csrf
                        self.secrets.append(csrf)
                    except core.VerificationError:
                        self.control_login()
                attempt("native_cleanup_control_available", ensure_control)
                if self.task_id is not None:
                    attempt("native_task_triggers_cleared", lambda: self.replace_triggers([]))
                    attempt("native_task_and_scan_work_drained", lambda: self.wait_no_work(cancel=True))
                def revoke_sessions():
                    sessions = self.list_sessions()
                    current = [item for item in sessions if item.get("IsCurrent") is True]
                    core.require(len(current) == 1, "Cleanup could not identify its last control session.")
                    control_id = current[0]["Id"]
                    errors = []
                    # All statuses are included, including browser logout and
                    # expired records. Revoke the original control session last.
                    for item in sorted(sessions, key=lambda value: value["Id"] == control_id):
                        try:
                            response = self.http_json("/admin/v1/sessions/" + item["Id"] + "/revoke",
                                                      administrator=True, body={})
                            core.require(response.get("SessionId") == item["Id"] and response.get("RevokedAt")
                                         and response.get("CurrentSessionRevoked") == (item["Id"] == control_id),
                                         "A native fixture session revocation did not commit.")
                        except Exception:
                            errors.append(item["Id"])
                    core.require(not errors, "One or more issued fixture sessions could not be revoked through the native API.")
                    self.report["cleanup"]["native_sessions_revoked_count"] = len(sessions)
                attempt("every_issued_session_revoked_control_last", revoke_sessions)
            if failures:
                self.report["status"] = "failed"
            try:
                # Preserve the shared runner's exact HBA restoration, process
                # fencing, database/role drop, service checks, and secret scrub.
                super().cleanup()
            finally:
                if failures:
                    self.report["cleanup_failures"] = failures + self.report.get("cleanup_failures", [])
                    self.report["status"] = "failed"
                self.sanitize_report()

    return TasksRunner(args)


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
        raise core.VerificationError("The task verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Task verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
