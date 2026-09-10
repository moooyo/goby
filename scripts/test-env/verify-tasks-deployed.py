#!/usr/bin/env python3
"""Perform one bounded M5f acceptance after an explicitly identified deployment.

Run only through root SSH on the Linux main test instance. Require canonical
GOBY_TASKS_EXPECTED_PID and GOBY_TASKS_EXPECTED_BINARY_SHA256 values. Runtime,
database, media-root and vault paths come from existing root-private runtime.env;
the existing administrator's credentials come from root-private browser.env.
GOBY_TASKS_VERIFICATION_RUN optionally selects decimal 1..99, default 1. Files
stay under /opt/goby-test with stem m5f-deployed-tasks[-attempt-N]. Existing
report, marker, or private failure-log paths are never overwritten.

Issue one native login and one ordinary Emby administrator login on a fresh
reported device ID. Admit one normal library.scan run with one unique RequestId;
replay that receipt only after completion. Install two distant calendar rules,
check their revision conflict, then restore the original empty rules/timezone
only while the acknowledged revision, IDs, and complete stored rows still match.
Busy scans/tasks or any active preexisting rule cause refusal, never cancellation.
On failure only the proven owned run can be cancelled. Both issued credentials
are logged out and independently tested for 401. The one new ordinary device
and all task, scan, receipt and retired-trigger history remain stored.

Preserve all old rows exactly except completed owned scans' library watermarks,
the selected definition's acknowledged schedule revisions/timestamps, and the
single updated_at field of eligible old non-CollectionFolder directory items
within their own successful scan's time window. Every media-item field remains
exact. Hash all existing media and recorded NFO files with read-only no-follow
no-atime descriptors; preserve the vault and accepted process identity.
Before either run POST, recheck every exact scanner cache-hit predicate against
fresh no-follow descriptors and the accepted catalog, refusing any reprobe.

Only sanitized aggregates are exported. On failure, bounded task HTTP bodies
are retained unchanged in a root-0600 private log; login/session response bodies
and all authentication headers are excluded from this log. There are no SQL
writes, service changes, key/device management writes, or automatic retries.
"""

import base64
from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timedelta, timezone
from decimal import Decimal
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
OWNER = "goby-tasks-deployed-m5f-v1"
ORIGIN = "http://127.0.0.1:18096"
ID, HASH, NUMBER, TOKEN = (re.compile(pattern) for pattern in
                         (r"[0-9a-f]{32}", r"[0-9a-f]{64}", r"[1-9][0-9]*", r"[A-Za-z0-9_-]{43}"))
TASK_TABLES = {"task_definitions", "task_triggers", "task_runs", "task_run_requests", "task_run_children", "task_occurrences"}
APPEND_TABLES = {"sessions", "devices", "scan_jobs", "task_triggers", "task_runs", "task_run_requests", "task_run_children"}
ACTIVE_RUN = {"pending", "running", "stopping"}
ACTIVE_CHILD = {"waiting", "queued", "running"}
TERMINAL = {"completed", "failed", "cancelled", "interrupted"}
TRIGGER_FIELDS = {"Id", "Kind", "IntervalTicks", "TimeOfDayTicks", "MaxRuntimeTicks", "DayOfWeek", "NextFireAt", "CalculationError"}
TASK_FIELDS = {"Id", "Key", "Name", "Description", "Category", "IsHidden", "Enabled", "Revision", "ScheduleTimezone",
               "Triggers", "CurrentRun", "LastRun", "NextRunAt"}
RUN_FIELDS = set("Id TaskId TaskName State Source RequestId ScheduledFor MaxRuntimeTicks CreatedAt StartedAt DeadlineAt StopRequestedAt StopReason FinishedAt ErrorCode ErrorMessage TotalChildren TerminalChildren CompletedChildren FailedChildren CancelledChildren InterruptedChildren UnavailableChildren Scanned Added Updated".split())
CHILD_FIELDS = set("Id RunId LibraryId LibraryName Ordinal State ScanJobId Scanned Added Updated ErrorCode ErrorMessage CreatedAt StartedAt FinishedAt".split())
MAX_BODY, MAX_TRACE = 2 * 1024 * 1024, 8 * 1024 * 1024
STARTED = time.monotonic()


class Failure(Exception):
    """Contain only a fixed assertion label, never response or secret text."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def deadline(_number, _frame):
    raise Failure("The bounded deployed task deadline was reached")


def helpers():
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        spec = importlib.util.spec_from_file_location("tasks_deployed_device_helpers", Path(__file__).with_name("verify-devices-deployed.py"))
        check(spec is not None and spec.loader is not None, "The inert deployed helpers are unavailable")
        devices = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(devices)
        core, utilities, sessions = devices.helpers()
    core.STARTED = STARTED
    core.TABLES |= TASK_TABLES
    # This narrow setting is used only by initial credential binding, before
    # this verifier admits its run or edits its schedule. Final auditing below
    # applies the separately proven task and completed-scan exceptions.
    core.MUTABLE = {"sessions", "devices"}
    return devices, core, utilities, sessions


def output_files():
    value = os.environ.get("GOBY_TASKS_VERIFICATION_RUN", "1")
    check(re.fullmatch(r"[1-9][0-9]?", value), "The verification run must be canonical decimal one through ninety-nine")
    stem = "m5f-deployed-tasks" + ("" if value == "1" else "-attempt-" + value)
    return int(value), ROOT / (stem + ".json"), ROOT / (stem + ".attempt.json"), ROOT / (stem + ".private.json")


def records(value, table):
    return [json.loads(raw, parse_float=Decimal) for raw in value[table]]


def indexed(value, table):
    entries = records(value, table)
    result = {str(row["id"]): row for row in entries}
    check(len(result) == len(entries), "A database row identity is duplicated")
    return result


def identical(left, right):
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(identical(value, right[key]) for key, value in left.items())
    if isinstance(left, list):
        return len(left) == len(right) and all(identical(a, b) for a, b in zip(left, right))
    if isinstance(left, Decimal):
        return left.as_tuple() == right.as_tuple()
    return left == right


def snapshot(core, utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == core.TABLES and len(value) == 27 and sum(map(len, value.values())) <= 5000,
          "The complete deployed snapshot escaped the twenty-seven-table bound")
    migrations = records(value, "schema_migrations")
    check(len(migrations) == 19 and {row["version"] for row in migrations} == set(range(1, 20)), "The main schema is not exactly version nineteen")
    media = [row for row in records(value, "items") if row["media"] is not None]
    check(len(value["libraries"]) == 5 and len(media) == 11 and all(row["media"].get("ProbeVersion") == 6 for row in media),
          "The accepted five-library and eleven-media catalog changed")
    return value


def database_type(core, sessions):
    class Database(core.database_type(sessions)):
        def __init__(self):
            super().__init__()
            self.queries = 0

        def read(self, query, label):
            check(self.queries < (160 if core.CLEANING else 130), "The read-only database query budget was exhausted")
            self.queries += 1
            return super().read(query, label)
    return Database


def idle(core, value):
    check(not any(row["status"] in {"Queued", "Running"} for row in records(value, "scan_jobs")), "An existing scan is busy; no task was admitted or cancelled")
    check(not any(row["state"] in ACTIVE_RUN for row in records(value, "task_runs")), "An existing task is busy; no task was admitted or cancelled")
    check(not any(row["state"] in ACTIVE_CHILD for row in records(value, "task_run_children")), "An existing task child is busy; no task was admitted or cancelled")
    check(not any(row["retired_at"] is None for row in records(value, "task_triggers")), "Existing active task rules must remain untouched; this environment was refused")
    check(not any(row["state"] in {"queued", "running"} for row in records(value, "encoding_jobs")), "An existing conversion is busy")


def nfo_hashes(core, value, configured):
    roots = indexed(value, "library_roots")
    allowed = {Path(path) for path in configured.split(os.pathsep)}
    result, total = {}, 0
    for item in records(value, "items"):
        relative = item.get("local_metadata_path")
        if not relative:
            continue
        root = roots.get(item["root_id"], {})
        relative = Path(relative)
        base = Path(root.get("path", ""))
        check(not relative.is_absolute() and ".." not in relative.parts and relative.suffix.lower() == ".nfo" and
              root.get("library_id") == item["library_id"] and Path(root.get("allowed_path", "")) in allowed and
              base.is_absolute() and base.is_relative_to(Path(root["allowed_path"])), "A recorded NFO escaped its existing approved root")
        path = base / relative
        check(path.resolve(strict=True) == path and path.is_relative_to(base), "A recorded NFO pathname is not canonical")
        if str(path) in result:
            check(result[str(path)]["sha256"] == item["local_metadata_hash"], "Recorded NFO aliases disagree on accepted bytes")
            continue
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
            check(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= MAX_BODY, "A recorded NFO is not a bounded regular file")
            total += info.st_size
            check(total <= 32 * 1024 * 1024 and len(result) < 128, "The recorded NFO hash population exceeded its bound")
            data = stream.read(MAX_BODY + 1)
            digest = hashlib.sha256(data).hexdigest()
            check(len(data) == info.st_size and digest == item["local_metadata_hash"] and
                  core.file_identity(info) == core.file_identity(os.fstat(stream.fileno())) == core.file_identity(path.lstat()),
                  "A recorded NFO no longer matches its accepted catalog hash or file identity")
        result[str(path)] = {"identity": core.file_identity(info), "sha256": digest}
    return result


def cache_preflight(core, baseline, current, configured, accepted_media):
    """Mirror scan.go's versioned normal-file cache predicate without probing."""
    roots, libraries = indexed(current, "library_roots"), indexed(current, "libraries")
    allowed = {Path(path) for path in configured.split(os.pathsep)}
    for root in roots.values():
        base, approved = Path(root["path"]), Path(root["allowed_path"])
        check(root["library_id"] in libraries and approved in allowed and base.is_absolute() and
              base == approved / root["relative_path"] and base.is_relative_to(approved) and base.resolve(strict=True) == base and base.is_dir(),
              "A registered scan root escaped its current private runtime approval")
    old_items = indexed(baseline, "items")
    media = [row for row in records(current, "items") if row["media"] is not None]
    check({row["id"] for row in media} == set(accepted_media), "The pre-run media cache population changed")
    empty_identity = 0
    for item in media:
        check(identical(item, old_items[item["id"]]), "A media catalog row changed before the normal cached request")
        root = roots.get(item["root_id"], {})
        base, relative = Path(root.get("path", "")), Path(item["relative_path"])
        path = Path(item["path"])
        check(root.get("library_id") == item["library_id"] and not relative.is_absolute() and ".." not in relative.parts and
              path == base / relative and path.resolve(strict=True) == path and path.is_relative_to(base),
              "The scanner cannot select this cached row by its unchanged root and relative pathname")
        descriptor = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
        try:
            for component in path.parts[1:-1]:
                child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
                os.close(descriptor)
                descriptor = child
            source = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME, dir_fd=descriptor)
        finally:
            os.close(descriptor)
        try:
            info = os.fstat(source)
            observed = core.file_identity(info)
            modified = datetime(1970, 1, 1, tzinfo=timezone.utc) + timedelta(microseconds=info.st_mtime_ns // 1000)
            probe = item["media"]
            # Empty historical file_identity is intentionally accepted by the
            # real cache predicate; ctime, mtime, size and version still match.
            check(stat.S_ISREG(info.st_mode) and item["is_folder"] is False and item["file_size"] == info.st_size and
                  item["modified_at"] is not None and core.timestamp(item["modified_at"]) == modified and
                  item["file_identity"] in {"", f"{info.st_dev}:{info.st_ino}"} and isinstance(probe, dict) and
                  type(probe.get("ProbeVersion")) is int and probe["ProbeVersion"] == 6 and
                  type(probe.get("FileChangeTimeNs")) is int and 0 < info.st_ctime_ns <= 9223372036854775807 and
                  probe["FileChangeTimeNs"] == info.st_ctime_ns and observed == accepted_media[item["id"]]["identity"] and
                  observed == core.file_identity(os.fstat(source)) == core.file_identity(path.lstat()),
                  "An existing media file would miss the real scanner cache; no task run was requested")
            empty_identity += item["file_identity"] == ""
        finally:
            os.close(source)
        core.remaining()
    return {"media_files": len(media), "empty_historical_identity_fields": empty_identity}


def directory_scope(item, items, roots):
    """Prove real paths or only the virtual directory forms emitted by scan.go."""
    root = roots.get(item["root_id"], {})
    if root.get("library_id") != item["library_id"] or not Path(root.get("path", "")).is_absolute():
        return False
    cursor, seen = item, set()
    while cursor["parent_id"] != item["library_id"]:
        if cursor["id"] in seen:
            return False
        seen.add(cursor["id"])
        cursor = items.get(cursor["parent_id"])
        if not cursor or cursor["library_id"] != item["library_id"] or cursor["root_id"] != item["root_id"] or cursor["is_folder"] is not True:
            return False
    collection = items.get(item["library_id"], {})
    if collection.get("type") != "CollectionFolder" or collection.get("is_folder") is not True or collection.get("library_id") != item["library_id"]:
        return False
    if item["path"]:
        path, base, relative = Path(item["path"]), Path(root["path"]), Path(item["relative_path"])
        return path.is_absolute() and not relative.is_absolute() and ".." not in relative.parts and path == base / relative and path.is_relative_to(base)
    relative = item["relative_path"]
    if item["type"] == "MusicAlbum":
        return relative == "//album/root" and item["parent_id"] == item["library_id"]
    if item["type"] == "Series":
        return item["parent_id"] == item["library_id"] and relative.startswith("//series/") and bool(relative[9:]) and "/" not in relative[9:]
    if item["type"] == "Season":
        parent = items.get(item["parent_id"], {})
        return parent.get("type") == "Series" and re.fullmatch(r"//season/" + re.escape(item["parent_id"]) + r"/(?:0|[1-9][0-9]*)", relative) is not None
    return False


def private_failure_log(path, value):
    payload = json.dumps(value, ensure_ascii=True, sort_keys=True).encode("utf-8") + b"\n"
    check(len(payload) <= 12 * 1024 * 1024, "The private failure trace exceeded its serialization bound")
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as stream:
        stream.write(payload)
        stream.flush()
        os.fsync(stream.fileno())
    info = path.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
          "The private failure trace has unsafe ownership or permissions")


class API:
    def __init__(self, core, logins, task_id, request_id, old_run_ids):
        self.core, self.logins, self.task_id, self.request_id = core, logins, task_id, request_id
        self.devices, self.old_run_ids = {}, old_run_ids
        self.run_id = ""
        self.terminal = self.cancel_attempted = False
        self.start_calls = 0
        self.schedule_actions = set()
        self.schedule_body = self.schedule_receipt = self.restore_body = self.original_schedule = None
        self.counts = {"GET": 0, "POST": 0, "PUT": 0, "DELETE": 0}
        self.trace, self.trace_bytes = [], 0
        self.before_start = None
        self.cache_checks = []

    def request(self, method, path, login, *, body=None, expected=200, raw=False, schedule_action=None):
        route = urlsplit(path)
        check(login in self.logins and path.startswith("/") and not path.startswith("//") and not route.scheme and not route.netloc and
              not route.fragment and "\r" not in path and "\n" not in path, "A task HTTP request escaped its local credential scope")
        native = login.kind == "admin"
        task_path = "/admin/v1/tasks/" + self.task_id
        signing_in = method == "POST" and route.path in {"/admin/v1/session", "/emby/Users/AuthenticateByName"}
        if signing_in:
            check(not route.query and not login.attempted and not login.token and native == (route.path == "/admin/v1/session"), "An additional login was prevented")
            login.attempted = True
        elif method == "GET":
            check(route.path in {"/admin/v1/session", "/admin/v1/capabilities", "/admin/v1/tasks", task_path, task_path + "/runs",
                                 "/emby/Sessions", "/emby/ScheduledTasks", "/emby/ScheduledTasks/" + self.task_id} or
                  self.run_id and route.path == "/admin/v1/task-runs/" + self.run_id, "A read escaped the fixed task scope")
        elif (method, path) in {("DELETE", "/admin/v1/session"), ("POST", "/emby/Sessions/Logout")}:
            check(login.token and not login.logout_attempted and native == (method == "DELETE"), "An additional or unowned logout was prevented")
            login.logout_attempted = True
        elif method == "POST" and path == task_path + "/runs":
            check(native and body == {"RequestId": self.request_id} and (self.start_calls == 0 or self.start_calls == 1 and self.terminal),
                  "An additional task admission outside the single terminal receipt replay was prevented")
            check(callable(self.before_start), "The real scanner cache must be proven before every run request")
            self.cache_checks.append(self.before_start())
            self.start_calls += 1
        elif method == "POST" and self.run_id and path == "/admin/v1/task-runs/" + self.run_id + "/cancel":
            check(native and self.core.CLEANING and body == {} and not self.cancel_attempted, "An unowned or repeated cancellation was prevented")
            self.cancel_attempted = True
        elif method == "POST" and path == task_path + "/triggers/preview":
            check(native and self.schedule_body and body in (
                {key: value for key, value in self.schedule_body.items() if key != "Revision"}, self.original_schedule),
                  "A schedule preview escaped the owned future rules")
        else:
            check(method == "PUT" and path == task_path + "/triggers" and native and schedule_action in {"install", "stale", "restore"} and
                  schedule_action not in self.schedule_actions and body == (self.restore_body if schedule_action == "restore" else self.schedule_body),
                  "A schedule write escaped the acknowledged owned revision and rule content")
            self.schedule_actions.add(schedule_action)
        self.core.remaining()
        check(sum(self.counts.values()) < (200 if self.core.CLEANING else 150), "The bounded task HTTP budget was exhausted")
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
        payload = None if body is None else json.dumps(body, separators=(",", ":")).encode("utf-8")
        check(payload is None or len(payload) <= 32768, "A task request exceeded the native body bound")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=self.core.remaining())
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            cookies = SimpleCookie()
            cookies.load(response.getheader("Set-Cookie", ""))
            if signing_in and native and "goby_session" in cookies and TOKEN.fullmatch(cookies["goby_session"].value):
                login.token = cookies["goby_session"].value
            data = response.read(MAX_BODY + 1)
            check(len(data) <= MAX_BODY, "A task HTTP response exceeded its byte bound")
            if route.path not in {"/admin/v1/session", "/emby/Users/AuthenticateByName"}:
                self.trace_bytes += len(data) + len(path) + len(payload or b"")
                check(self.trace_bytes <= MAX_TRACE, "The private in-memory task trace exceeded its byte bound")
                self.trace.append({"method": method, "path": path, "request_body": body, "status": response.status,
                                   "response_body_base64": base64.b64encode(data).decode("ascii")})
            value = data if raw else json.loads(data) if data else None
            if signing_in and isinstance(value, dict):
                token, csrf = value.get("AccessToken"), value.get("CSRFToken")
                if not native and isinstance(token, str) and TOKEN.fullmatch(token):
                    login.token = token
                if native and isinstance(csrf, str) and HASH.fullmatch(csrf):
                    login.csrf = csrf
            # Retain acknowledged ownership before later status/DTO assertions.
            if method == "POST" and path == task_path + "/runs" and isinstance(value, dict):
                run = value.get("Run", {})
                if (isinstance(run, dict) and isinstance(run.get("Id"), str) and ID.fullmatch(run["Id"]) and
                        run["Id"] not in self.old_run_ids and run.get("TaskId") == self.task_id and
                        run.get("RequestId") == self.request_id and run.get("Source") == "manual"):
                    check(not self.run_id or self.run_id == run["Id"], "The receipt changed its owned task run")
                    self.run_id = run["Id"]
            if schedule_action == "install" and isinstance(value, dict):
                task = value.get("Task", {})
                if (isinstance(task, dict) and task.get("Id") == self.task_id and
                        task.get("Revision") == str(int(self.schedule_body["Revision"]) + 1) and
                        task.get("ScheduleTimezone") == self.schedule_body["ScheduleTimezone"] and
                        isinstance(task.get("Triggers"), list) and len(task["Triggers"]) == 2 and
                        all(isinstance(entry, dict) and isinstance(entry.get("Id"), str) and ID.fullmatch(entry["Id"]) for entry in task["Triggers"])):
                    self.schedule_receipt = task
            check(response.status in ((expected,) if isinstance(expected, int) else expected), "A task HTTP response returned an unexpected status")
            check(response.status != 204 or not data, "An HTTP 204 response included content")
            return value
        finally:
            connection.close()


def delta(before, after, table):
    previous, current = set(before[table]), set(after[table])
    check(len(previous) == len(before[table]) and len(current) == len(after[table]) and previous <= current,
          "A preexisting append-only row changed or disappeared")
    return [json.loads(raw, parse_float=Decimal) for raw in sorted(current - previous)]


def prove_run(core, api, before, value):
    runs = [row for row in records(value, "task_runs") if row.get("task_id") == api.task_id and row.get("request_id") == api.request_id]
    receipts = [row for row in records(value, "task_run_requests") if row["task_id"] == api.task_id and row["request_id"] == api.request_id]
    check(len(runs) == len(receipts) == 1 and runs[0]["id"] not in indexed(before, "task_runs"), "The unique request does not prove one newly owned run and receipt")
    run, receipt, native = runs[0], receipts[0], api.logins[0]
    fingerprint = "\\x" + hashlib.sha256(json.dumps({"TaskID": api.task_id, "Executor": "library.scan", "Source": "manual"}, separators=(",", ":")).encode()).hexdigest()
    check(run["actor_user_id"] == native.user_id and run["actor_session_id"] == native.id and run["actor_kind"] == "admin" and
          run["source"] == "manual" and run["task_key"] == "library.scan" and run["task_emby_key"] == "RefreshLibrary" and
          run["task_name"] == indexed(before, "task_definitions")[api.task_id]["name"] and
          receipt["run_id"] == run["id"] and receipt["fingerprint"] == run["request_fingerprint"] == fingerprint and
          all(run[field] is None for field in ("trigger_id", "trigger_revision", "scheduled_for", "max_runtime_ticks")),
          "The request receipt does not belong to the exact owned native actor and task")
    check(not api.run_id or api.run_id == run["id"], "The acknowledged run differs from its durable owned receipt")
    api.run_id = run["id"]
    return run


def poll_run(core, api, maximum=80):
    for _ in range(maximum):
        value = api.request("GET", "/admin/v1/task-runs/" + api.run_id + "?StartIndex=0&Limit=200", api.logins[0])
        check(isinstance(value, dict) and set(value) == {"Run", "Children"} and value.get("Run", {}).get("Id") == api.run_id and
              value["Run"].get("TaskId") == api.task_id, "Run polling returned another execution")
        if value["Run"].get("State") in TERMINAL:
            return value
        check(value["Run"].get("State") in ACTIVE_RUN, "Run polling returned an invalid state")
        time.sleep(min(0.5, core.remaining()))
    raise Failure("The owned run did not reach its bounded terminal deadline")


def assert_completed(core, api, baseline, current, wire):
    run = prove_run(core, api, baseline, current)
    children = [row for row in delta(baseline, current, "task_run_children") if row["run_id"] == api.run_id]
    jobs = {row["id"]: row for row in delta(baseline, current, "scan_jobs")}
    libraries = indexed(baseline, "libraries")
    media_counts = {identifier: sum(row["library_id"] == identifier and row["media"] is not None for row in records(baseline, "items")) for identifier in libraries}
    check(run["state"] == "completed" and run["total_children"] == run["terminal_children"] == run["completed_children"] == len(libraries) and
          all(run[field] == 0 for field in ("failed_children", "cancelled_children", "interrupted_children", "unavailable_children", "added", "updated")) and
          run["scanned"] == sum(media_counts.values()) and run["error_code"] == run["error_message"] == "",
          "The normal cached task did not complete the exact whole-library media population")
    check(len(children) == len(jobs) == len(libraries) and {row["library_id"] for row in children} == set(libraries) and
          {row["scan_job_id"] for row in children} == set(jobs), "The task did not retain exactly one owned child and scan per baseline library")
    ordered = sorted(libraries)
    for child in children:
        job = jobs[child["scan_job_id"]]
        check(child["id"] == hashlib.md5((api.run_id + ":" + child["library_id"]).encode()).hexdigest() and
              child["library_name"] == libraries[child["library_id"]]["name"] and child["ordinal"] == ordered.index(child["library_id"]) and
              child["state"] == "completed" and child["error_code"] == child["error_message"] == "" and
              job["task_child_id"] == child["id"] and job["library_id"] == child["library_id"] and job["status"] == "Completed" and
              job["force_probe"] is False and job["cancel_requested"] is False and job["error"] == "" and
              child["scanned"] == job["scanned"] == media_counts[child["library_id"]] and child["added"] == job["added"] == child["updated"] == job["updated"] == 0 and
              child["started_at"] == job["started_at"] and child["finished_at"] == job["finished_at"],
              "A terminal child and its reciprocal normal cached scan disagree")
    page, public_run = wire["Children"], wire["Run"]
    check(set(public_run) == RUN_FIELDS and public_run["Id"] == api.run_id and public_run["TaskId"] == api.task_id and
          public_run["TaskName"] == run["task_name"] and public_run["State"] == run["state"] and public_run["Source"] == "manual" and
          public_run["RequestId"] == api.request_id and public_run["ErrorCode"] == public_run["ErrorMessage"] == "",
          "The public terminal run changed its exact safe identity and state contract")
    check(set(page) == {"Items", "TotalRecordCount", "StartIndex", "Limit"} and page["StartIndex"] == 0 and page["Limit"] == 200 and
          page["TotalRecordCount"] == len(page["Items"]) == len(children) and [row["Id"] for row in page["Items"]] ==
          [row["id"] for row in sorted(children, key=lambda row: (row["ordinal"], row["id"]))], "The public child page lost its complete deterministic library snapshot")
    for field, column in (("TotalChildren", "total_children"), ("TerminalChildren", "terminal_children"), ("CompletedChildren", "completed_children"),
                          ("FailedChildren", "failed_children"), ("CancelledChildren", "cancelled_children"), ("InterruptedChildren", "interrupted_children"),
                          ("UnavailableChildren", "unavailable_children"), ("Scanned", "scanned"), ("Added", "added"), ("Updated", "updated")):
        check(type(public_run[field]) is int and public_run[field] == run[column], "A public run aggregate differs from stored child totals")
    by_id = {row["id"]: row for row in children}
    for child in page["Items"]:
        check(isinstance(child, dict) and set(child) == CHILD_FIELDS, "A public task child changed its exact safe field contract")
        stored = by_id[child["Id"]]
        for field, column in (("RunId", "run_id"), ("LibraryId", "library_id"), ("LibraryName", "library_name"), ("Ordinal", "ordinal"),
                              ("State", "state"), ("ScanJobId", "scan_job_id"), ("Scanned", "scanned"), ("Added", "added"), ("Updated", "updated")):
            check(child[field] == stored[column], "A public child field differs from its stored snapshot")
    api.terminal = True
    return run


def protocols(api, baseline, expected_run=None):
    native, emby = api.logins
    listing = api.request("GET", "/admin/v1/tasks", native)
    check(isinstance(listing, dict) and set(listing) == {"Items", "TotalRecordCount"} and
          listing["TotalRecordCount"] == len(listing["Items"]) == len(baseline["task_definitions"]) and
          {row["Id"] for row in listing["Items"]} == set(indexed(baseline, "task_definitions")), "The native task list lost stable definition identities")
    task = api.request("GET", "/admin/v1/tasks/" + api.task_id, native).get("Task", {})
    check(set(task) == TASK_FIELDS and task.get("Key") == "library.scan" and task.get("Id") == api.task_id and
          next((row for row in listing["Items"] if row["Id"] == api.task_id), None) == task, "Native task list and detail disagree")
    stored = indexed(baseline, "task_definitions")[api.task_id]
    for field, column in (("Name", "name"), ("Description", "description"), ("Category", "category"), ("IsHidden", "is_hidden"), ("Enabled", "enabled")):
        check(identical(task[field], stored[column]), "A native task definition differs from its stable stored metadata")
    compatibility = api.request("GET", "/emby/ScheduledTasks", emby)
    detail = api.request("GET", "/emby/ScheduledTasks/" + api.task_id, emby)
    check(isinstance(compatibility, list) and len(compatibility) == 1 and compatibility[0] == detail and
          detail.get("Id") == task["Id"] and detail.get("Key") == "RefreshLibrary" and detail.get("Name") == task["Name"] and
          detail.get("State") == "Idle", "Native and Emby task protocols lost their common definition identity")
    if expected_run:
        last = detail.get("LastExecutionResult", {})
        check(task.get("CurrentRun") is None and task.get("LastRun", {}).get("Id") == expected_run and task["LastRun"].get("TaskId") == api.task_id and
              task["LastRun"].get("State") == "completed" and last.get("Id") == api.task_id and last.get("Key") == "RefreshLibrary" and
              last.get("Name") == task["Name"] and last.get("Status") == "Completed" and "RunId" not in last,
              "The compatibility last result confused a definition with its execution")
        check(api.core.timestamp(last["StartTimeUtc"]) == api.core.timestamp(task["LastRun"]["StartedAt"] or task["LastRun"]["CreatedAt"]) and
              api.core.timestamp(last["EndTimeUtc"]) == api.core.timestamp(task["LastRun"]["FinishedAt"]),
              "Native and compatibility terminal result timestamps disagree")
        history = api.request("GET", "/admin/v1/tasks/" + api.task_id + "/runs?StartIndex=0&Limit=200", native)
        check(history.get("Items") and history["Items"][0]["Id"] == expected_run and history["Items"][0] == task["LastRun"],
              "Native history did not retain its acknowledged terminal run")
    return task


class Schedule:
    def __init__(self, core, utilities, database, api, baseline):
        self.core, self.utilities, self.database, self.api, self.baseline = core, utilities, database, api, baseline
        self.original = indexed(baseline, "task_definitions")[api.task_id]
        api.original_schedule = {"ScheduleTimezone": self.original["schedule_timezone"], "Triggers": []}
        self.saved_definition = self.saved_triggers = self.restored_definition = None

    def current(self):
        return snapshot(self.core, self.utilities, self.database)

    def bind_receipt(self, current):
        check(self.api.schedule_receipt is not None, "The future schedule write lacks an acknowledged revision and trigger identities")
        receipt = self.api.schedule_receipt
        definition = indexed(current, "task_definitions")[self.api.task_id]
        new = delta(self.baseline, current, "task_triggers")
        expected = dict(self.original)
        expected.update({"revision": self.original["revision"] + 1, "schedule_timezone": self.api.schedule_body["ScheduleTimezone"], "updated_at": definition["updated_at"]})
        check(identical(definition, expected) and len(new) == 2 and {row["position"] for row in new} == {0, 1} and
              {row["id"] for row in new} == {row["Id"] for row in receipt["Triggers"]},
              "The acknowledged future schedule differs from its exact stored definition and trigger identities")
        public = {row["Id"]: row for row in receipt["Triggers"]}
        for row, rule in zip(sorted(new, key=lambda entry: entry["position"]), self.api.schedule_body["Triggers"]):
            check(row["task_id"] == self.api.task_id and row["schedule_revision"] == expected["revision"] and row["kind"] == rule["Kind"] and
                  row["time_of_day_ticks"] == int(rule["TimeOfDayTicks"]) and row["day_of_week"] == rule.get("DayOfWeek") and
                  row["timezone"] == expected["schedule_timezone"] and row["retired_at"] is None and row["last_due_at"] is None and
                  row["interval_ticks"] is None and row["anchor_at"] is None and row["max_runtime_ticks"] is None and row["calculation_error"] == "" and
                  row["created_at"] == row["updated_at"] == definition["updated_at"] and
                  self.core.timestamp(row["next_fire_at"]) > datetime.now(timezone.utc) + timedelta(hours=2),
                  "The future schedule contains unowned content, activity, or a near-term due time")
            shown = public[row["id"]]
            check(set(shown) == TRIGGER_FIELDS and shown["Kind"] == row["kind"] and shown["TimeOfDayTicks"] == str(row["time_of_day_ticks"]) and
                  identical(shown["DayOfWeek"], row["day_of_week"]) and shown["IntervalTicks"] is None and shown["MaxRuntimeTicks"] is None and
                  shown["CalculationError"] == "" and self.core.timestamp(shown["NextFireAt"]) == self.core.timestamp(row["next_fire_at"]),
                  "A public future trigger differs from its acknowledged persisted rule")
        check(self.core.timestamp(receipt["NextRunAt"]) == min(self.core.timestamp(row["next_fire_at"]) for row in new),
              "The public next-run time differs from the earliest stored calendar rule")
        self.saved_definition, self.saved_triggers = definition, {row["id"]: row for row in new}

    def restore(self):
        if "install" not in self.api.schedule_actions:
            return
        current = self.current()
        if self.saved_definition is None:
            self.bind_receipt(current)
        check(identical(indexed(current, "task_definitions")[self.api.task_id], self.saved_definition) and
              identical({row["id"]: row for row in delta(self.baseline, current, "task_triggers")}, self.saved_triggers),
              "Concurrent schedule changes prevent safe restoration; no replacement was issued")
        self.api.restore_body = {"Revision": str(self.saved_definition["revision"]), "ScheduleTimezone": self.original["schedule_timezone"], "Triggers": []}
        result = self.api.request("PUT", "/admin/v1/tasks/" + self.api.task_id + "/triggers", self.api.logins[0],
                                  body=self.api.restore_body, schedule_action="restore")
        task = result.get("Task", {})
        check(task.get("Id") == self.api.task_id and task.get("Revision") == str(self.original["revision"] + 2) and
              task.get("ScheduleTimezone") == self.original["schedule_timezone"] and task.get("Triggers") == [] and task.get("NextRunAt") is None,
              "The schedule restoration did not return the exact original rules and timezone")
        final = self.current()
        definition = indexed(final, "task_definitions")[self.api.task_id]
        expected = dict(self.original)
        expected.update({"revision": self.original["revision"] + 2, "updated_at": definition["updated_at"]})
        check(identical(definition, expected), "Restoration changed another definition field")
        retired = {row["id"]: row for row in delta(self.baseline, final, "task_triggers")}
        check(retired.keys() == self.saved_triggers.keys(), "Restoration added or removed unrelated trigger history")
        for identifier, row in retired.items():
            wanted = dict(self.saved_triggers[identifier])
            wanted.update({"retired_at": definition["updated_at"], "updated_at": definition["updated_at"]})
            check(identical(row, wanted), "Restoration changed more than the owned triggers' retirement timestamps")
        self.restored_definition = definition


def audit(core, baseline, current, api, schedule):
    additions = {table: delta(baseline, current, table) for table in APPEND_TABLES}
    for table in core.TABLES - APPEND_TABLES - {"libraries", "items", "task_definitions"}:
        check(baseline[table] == current[table], "A protected metadata, user-state, key, configuration, or occurrence table changed")
    check(api is not None and {row["id"] for row in additions["sessions"]} == {login.id for login in api.logins if login.id} and
          {str(row["id"]) for row in additions["devices"]} == set(api.devices), "New authentication rows escaped the two acknowledged credentials and one ordinary device")
    for login in api.logins:
        if not login.attempted:
            continue
        stored = indexed(current, "sessions").get(login.id)
        check(stored and login.initial and stored["revoked_at"] is not None and
              core.timestamp(stored["last_seen_at"]) >= core.timestamp(login.initial["last_seen_at"]) and
              all(identical(stored[field], value) for field, value in login.initial.items() if field not in {"revoked_at", "last_seen_at"}),
              "An owned login was not revoked or changed more than attributable activity timestamps")
    for device in additions["devices"]:
        login = api.logins[1]
        check(str(device["id"]) == login.device and device["reported_device_id"] == login.reported and device["app_name"] == login.app and
              device["reported_name"] == login.name and device["app_version"] == login.version and device["last_user_id"] == login.user_id and
              device["revision"] == 1 and device["custom_name"] is None and device["deleted_at"] is None,
              "The new ordinary login device lost its original attributable history")
    run = prove_run(core, api, baseline, current) if api.start_calls else None
    check({row["id"] for row in additions["task_runs"]} == ({api.run_id} if run else set()) and
          len(additions["task_run_requests"]) == (1 if run else 0), "Additional task admissions or receipts escaped the single manual request")
    children = additions["task_run_children"]
    scans = {row["id"]: row for row in additions["scan_jobs"]}
    libraries, roots = indexed(baseline, "libraries"), indexed(baseline, "library_roots")
    if run:
        check(run["state"] in TERMINAL and len(children) == len(libraries) and {row["library_id"] for row in children} == set(libraries) and
              all(row["run_id"] == api.run_id and row["state"] not in ACTIVE_CHILD and row["library_name"] == libraries[row["library_id"]]["name"] for row in children),
              "The owned run or its complete library snapshot is not terminal")
    else:
        check(not children and not scans, "Task or scan children appeared without the acknowledged run")
    check({row["scan_job_id"] for row in children if row["scan_job_id"] is not None} == set(scans), "New scan rows are not exclusively linked to owned children")
    completed = {}
    for child in children:
        if child["scan_job_id"] is None:
            continue
        scan = scans[child["scan_job_id"]]
        check(scan["task_child_id"] == child["id"] and scan["library_id"] == child["library_id"] and scan["status"] not in {"Queued", "Running"} and
              scan["force_probe"] is False and all(child[field] == scan[field] for field in ("scanned", "added", "updated", "started_at", "finished_at")),
              "A terminal owned child and its reciprocal scan disagree")
        if child["state"] == "completed":
            check(scan["status"] == "Completed", "A completed child does not have a successful scan")
            completed[child["library_id"]] = scan
    final_libraries = indexed(current, "libraries")
    check(final_libraries.keys() == libraries.keys(), "The library inventory changed")
    for identifier, old in libraries.items():
        new, wanted = final_libraries[identifier], dict(old)
        if identifier in completed:
            scan = completed[identifier]
            check(new["last_scan_at"] is not None and (old["last_scan_at"] is None or core.timestamp(new["last_scan_at"]) > core.timestamp(old["last_scan_at"])) and
                  core.timestamp(scan["finished_at"]) <= core.timestamp(new["last_scan_at"]) <= core.timestamp(run["finished_at"]),
                  "A library watermark is outside its own successful task scan")
            wanted["last_scan_at"] = new["last_scan_at"]
        check(identical(new, wanted), "A library field changed outside the owned completion watermark")
    old_items, new_items = indexed(baseline, "items"), indexed(current, "items")
    check(old_items.keys() == new_items.keys(), "The cached task changed item identities")
    directory_counts = {}
    for identifier, old in old_items.items():
        new = new_items[identifier]
        if identical(old, new):
            continue
        root, scan = roots.get(old["root_id"], {}), completed.get(old["library_id"])
        check(old["is_folder"] is True and old["type"] != "CollectionFolder" and old["media"] is None and scan and
              root.get("library_id") == old["library_id"] and directory_scope(old, old_items, roots),
              "A changed item is not an eligible directory in its own completed scan root")
        wanted = dict(old)
        wanted["updated_at"] = new["updated_at"]
        check(identical(new, wanted) and core.timestamp(new["updated_at"]) >= core.timestamp(old["updated_at"]) and
              core.timestamp(scan["started_at"]) <= core.timestamp(new["updated_at"]) <= core.timestamp(scan["finished_at"]),
              "A directory changed more than its bounded successful-scan timestamp")
        directory_counts[old["library_id"]] = directory_counts.get(old["library_id"], 0) + 1
    old_definitions, definitions = indexed(baseline, "task_definitions"), indexed(current, "task_definitions")
    check(definitions.keys() == old_definitions.keys(), "The task definition inventory changed")
    for identifier, old in old_definitions.items():
        wanted = schedule.restored_definition if schedule and identifier == api.task_id and "install" in api.schedule_actions else old
        check(wanted is not None and identical(definitions[identifier], wanted), "A task definition lacks a proven exact schedule restoration")
    if "install" in api.schedule_actions:
        check(schedule.restored_definition and len(additions["task_triggers"]) == 2 and
              all(row["retired_at"] is not None for row in additions["task_triggers"]), "The temporary future rules remain active or lost their history")
    else:
        check(not additions["task_triggers"], "Triggers appeared without the owned schedule edit")
    return additions, directory_counts


def main():
    report = {"owner": OWNER, "status": "failed", "http_transport_retries": 0, "cleanup": {"proven": False, "errors": []}, "snapshots": {},
              "limits": {"http_requests": 200, "response_bytes": MAX_BODY, "database_queries": 160, "workflow_seconds": 180},
              "allowed_old_row_changes": {"libraries": "Only last_scan_at for a completed owned child scan",
                                           "directory_items": "Only updated_at for baseline non-CollectionFolder directories in their completed scan root and time window",
                                           "selected_task_definition": "Only acknowledged schedule revision and updated_at; original timezone and empty rules restored"},
              "limitations": ["No automatic timer firing or service restart; isolated browser acceptance covers two restarts"]}
    devices = core = utilities = sessions = database = baseline = api = schedule = deployment = vault = media = nfos = configured = parent_identity = None
    stage = "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without arguments")
        signal.signal(signal.SIGALRM, deadline)
        signal.setitimer(signal.ITIMER_REAL, max(0.1, 135 - (time.monotonic() - STARTED)))
        accepted, expected_pid = os.environ.get("GOBY_TASKS_EXPECTED_BINARY_SHA256", ""), os.environ.get("GOBY_TASKS_EXPECTED_PID", "")
        check(HASH.fullmatch(accepted) and NUMBER.fullmatch(expected_pid) and 1 < int(expected_pid) <= 2147483647,
              "The accepted binary digest and positive main PID environment constraints are required")
        number, result_path, attempt_path, private_path = output_files()
        report["verification_run"] = number
        report["output_scope"] = {"directory": str(ROOT), "report": result_path.name, "attempt_marker": attempt_path.name, "private_failure_log": private_path.name}
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed output directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (result_path, attempt_path, private_path)), "The selected attempt already exists; no overwrite is allowed")
        devices, core, utilities, sessions = helpers()
        utilities.private_write(attempt_path, {"owner": OWNER, "verification_run": number, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                                               "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
        parent_identity = info.st_dev, info.st_ino
        deployment = sessions.deployment(accepted)
        check(deployment["main_pid"] == int(expected_pid), "The live service PID differs from the explicitly accepted deployment")
        runtime = sessions.private_values(ROOT / "runtime.env", {"GOBY_LISTEN", "GOBY_PUBLIC_URL", "GOBY_MEDIA_ROOTS"})
        check(runtime["GOBY_LISTEN"] == "127.0.0.1:18096" and runtime["GOBY_PUBLIC_URL"] == ORIGIN, "The private main runtime is outside the fixed loopback endpoint")
        configured = runtime["GOBY_MEDIA_ROOTS"]
        database = database_type(core, sessions)()
        baseline = snapshot(core, utilities, database)
        report["snapshots"]["before"] = utilities.snapshot_report(baseline)
        idle(core, baseline)
        core.quiescent(baseline, database)
        definitions = [row for row in records(baseline, "task_definitions") if row["key"] == "library.scan"]
        check(len(definitions) == 1 and ID.fullmatch(definitions[0]["id"]) and definitions[0]["enabled"] is True and
              definitions[0]["emby_key"] == "RefreshLibrary", "The executable library task definition is not reconciled")
        task_id = definitions[0]["id"]
        namespace = [row["value"] for row in records(baseline, "server_settings") if row["key"] == "server_id"]
        check(len(namespace) == 1 and ID.fullmatch(namespace[0]), "The persistent server namespace is invalid")
        report["deployment"] = {**deployment, "schema_version": 19, "database_role": "goby_test", "database_port": 5432,
                                "postgresql_forced_read_only": True, "server_namespace_sha256": hashlib.sha256(namespace[0].encode()).hexdigest()}
        vault = core.vault_identity(sessions)
        media, nfos = devices.source_hashes(core, baseline, configured), nfo_hashes(core, baseline, configured)
        report["initial_scanner_cache_preflight"] = cache_preflight(core, baseline, baseline, configured, media)
        report["source_hashes_before"] = {"media_count": len(media), "media_sha256": utilities.aggregate(media), "nfo_count": len(nfos), "nfo_sha256": utilities.aggregate(nfos)}
        credentials = sessions.private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        users = [row for row in records(baseline, "users") if row["name"] == credentials["GOBY_SMOKE_NAME"] and row["is_administrator"] and not row["is_disabled"]]
        check(len(users) == 1, "The private credentials do not identify one existing enabled administrator")
        nonce = secrets.token_hex(16)
        reported = "goby-m5f-task-client-" + nonce
        check(all(row["device_id"] != reported for table in ("sessions", "application_key_clients") for row in records(baseline, table)) and
              all(row["reported_device_id"] != reported for table in ("devices", "application_key_devices") for row in records(baseline, table)),
              "The reserved ordinary reported ID already belongs to existing history")
        native = devices.Login("admin", users[0]["id"])
        emby = devices.Login("emby", users[0]["id"], reported, "M5f task device " + nonce, "M5f task verifier " + nonce, "m5f-deployed")
        request_id = "goby-m5f-main-" + nonce
        check(not any(row["request_id"] == request_id for row in records(baseline, "task_run_requests")), "The unique manual request already has a receipt")
        api = API(core, [native, emby], task_id, request_id, set(indexed(baseline, "task_runs")))

        def before_start():
            current = snapshot(core, utilities, database)
            if api.start_calls == 0:
                idle(core, current)
                core.preserve(baseline, current)
            defaults = database.read("SELECT json_build_object('expression',pg_get_expr(d.adbin,d.adrelid),'required',a.attnotnull,"
                                     "'boolean',a.atttypid='boolean'::regtype) FROM pg_attribute a LEFT JOIN pg_attrdef d "
                                     "ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid='public.scan_jobs'::regclass "
                                     "AND a.attname='force_probe' AND NOT a.attisdropped;", "Observe normal-scan default")
            check(defaults == {"expression": "false", "required": True, "boolean": True},
                  "Task scan admission would not inherit the required false ForceProbe default")
            return cache_preflight(core, baseline, current, configured, media)

        api.before_start = before_start
        stage = "two fresh independent administrator credentials"
        for login in api.logins:
            is_native = login.kind == "admin"
            value = api.request("POST", "/admin/v1/session" if is_native else "/emby/Users/AuthenticateByName", login,
                                body={"Name" if is_native else "Username": credentials["GOBY_SMOKE_NAME"], "Password" if is_native else "Pw": credentials["GOBY_SMOKE_PASSWORD"]})
            check(login.token and value.get("User", {}).get("Id") == users[0]["id"], "A login omitted its credential or selected another account")
            if is_native:
                check(login.csrf == hashlib.sha256(("goby:admin:csrf:" + login.token).encode()).hexdigest(), "The native CSRF token does not belong to its issued cookie")
            else:
                check(value.get("ServerId") == namespace[0], "The ordinary login belongs to another server namespace")
            current = snapshot(core, utilities, database)
            devices.bind_login(core, api, baseline, current, login)
            if not is_native:
                check(value.get("SessionInfo", {}).get("Id") == login.id, "The ordinary wire session differs from its stored credential")
        credentials.clear()
        check(native.token != emby.token and native.id != emby.id and len(api.devices) == 1, "The two login kinds did not retain independent credentials")
        initial_task = protocols(api, baseline)
        check(initial_task["Revision"] == str(definitions[0]["revision"]) and initial_task["ScheduleTimezone"] == definitions[0]["schedule_timezone"] and
              initial_task["Triggers"] == [] and initial_task["CurrentRun"] is None, "The initial definition is not idle with its exact original empty schedule")
        current = snapshot(core, utilities, database)
        idle(core, current)
        core.preserve(baseline, current)
        stage = "one normal cached full-library run"
        admitted = api.request("POST", "/admin/v1/tasks/" + task_id + "/runs", native, body={"RequestId": request_id}, expected=202)
        check(api.run_id and admitted.get("Admitted") is True, "The unique manual request did not admit its own new run")
        wire = poll_run(core, api)
        current = snapshot(core, utilities, database)
        assert_completed(core, api, baseline, current, wire)
        repeated = api.request("POST", "/admin/v1/tasks/" + task_id + "/runs", native, body={"RequestId": request_id}, expected=202)
        check(repeated.get("Admitted") is False and repeated.get("Run") == wire["Run"] and snapshot(core, utilities, database) == current,
              "Replaying the terminal request receipt admitted or changed task history")
        protocols(api, baseline, api.run_id)
        stage = "distant calendar schedule and stale revision"
        schedule = Schedule(core, utilities, database, api, baseline)
        now = core.timestamp(database.read("SELECT to_json(clock_timestamp());", "Observe the calendar preview clock"))
        daily, weekly = now + timedelta(hours=6), now + timedelta(days=2, hours=6)
        clock_ticks = str((daily.hour * 3600 + daily.minute * 60 + daily.second) * 10_000_000)
        api.schedule_body = {"Revision": str(schedule.original["revision"]), "ScheduleTimezone": "UTC",
                             "Triggers": [{"Kind": "daily", "TimeOfDayTicks": clock_ticks},
                                          {"Kind": "weekly", "TimeOfDayTicks": clock_ticks, "DayOfWeek": (weekly.weekday() + 1) % 7}]}
        original_preview = api.request("POST", "/admin/v1/tasks/" + task_id + "/triggers/preview", native, body=api.original_schedule)
        check(original_preview.get("Items") == [], "The original empty schedule and timezone cannot be restored through the native API")
        preview = api.request("POST", "/admin/v1/tasks/" + task_id + "/triggers/preview", native,
                              body={key: value for key, value in api.schedule_body.items() if key != "Revision"})
        check(isinstance(preview.get("Items"), list) and len(preview["Items"]) == 2 and
              all(row.get("Event") is None and len(row.get("Occurrences", [])) == 3 and
                  all(core.timestamp(due) > core.timestamp(preview["ServerTime"]) + timedelta(hours=2) for due in row["Occurrences"]) for row in preview["Items"]),
              "Calendar preview did not prove two distant bounded future rules")
        before_schedule = snapshot(core, utilities, database)
        idle(core, before_schedule)
        check(indexed(before_schedule, "task_definitions")[task_id] == schedule.original, "The definition changed before the owned schedule edit")
        saved = api.request("PUT", "/admin/v1/tasks/" + task_id + "/triggers", native, body=api.schedule_body, schedule_action="install")
        current = snapshot(core, utilities, database)
        schedule.bind_receipt(current)
        readback = api.request("GET", "/admin/v1/tasks/" + task_id, native)
        check(readback == saved and all(set(row) == TRIGGER_FIELDS for row in saved["Task"]["Triggers"]), "Future schedule readback changed its exact safe trigger contract")
        conflict = api.request("PUT", "/admin/v1/tasks/" + task_id + "/triggers", native,
                               body=api.schedule_body, expected=409, schedule_action="stale")
        check(conflict.get("Error", {}).get("Code") == "revision_conflict" and snapshot(core, utilities, database) == current,
              "A stale schedule revision changed the acknowledged rule set")
        report["checks"] = {"issued_credentials": 2, "new_ordinary_devices": 1, "manual_runs": 1, "receipt_replay_no_new_run": True,
                            "baseline_libraries_snapshotted": len(baseline["libraries"]), "normal_cached_media_scanned": 11,
                            "reciprocal_child_scan_ownership": True, "run_child_aggregates_match": True,
                            "native_and_emby_definition_identity": True, "emby_key": "RefreshLibrary", "compatibility_last_result_uses_definition_id": True,
                            "distant_daily_weekly_rules": 2, "stale_schedule_status": 409}
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
            native, emby = api.logins
            if native.token and not native.csrf:
                try:
                    native.csrf = api.request("GET", "/admin/v1/session", native).get("CSRFToken", "")
                    check(HASH.fullmatch(native.csrf), "The owned cleanup CSRF token could not be recovered")
                except BaseException:
                    report["cleanup"]["errors"].append("The owned native cleanup credential could not be recovered")
            if api.start_calls:
                try:
                    current = snapshot(core, utilities, database)
                    run = prove_run(core, api, baseline, current)
                    if run["state"] in ACTIVE_RUN:
                        api.request("POST", "/admin/v1/task-runs/" + api.run_id + "/cancel", native, body={}, expected=202)
                        poll_run(core, api, maximum=35)
                except BaseException:
                    report["cleanup"]["errors"].append("The unique owned run could not be proven terminal; no unrelated run was cancelled")
            if schedule is not None:
                try:
                    schedule.restore()
                    report["schedule_restored_exact_rules_and_timezone"] = schedule.restored_definition is not None
                except BaseException:
                    report["cleanup"]["errors"].append("The acknowledged schedule could not be safely restored; concurrent or unproven state was not overwritten")
            for login in (emby, native):
                if not login.attempted:
                    continue
                if not login.token:
                    report["cleanup"]["errors"].append("An issued credential response was lost; no existing credential was reused")
                    continue
                try:
                    is_native = login.kind == "admin"
                    api.request("DELETE" if is_native else "POST", "/admin/v1/session" if is_native else "/emby/Sessions/Logout", login,
                                expected=204 if is_native else 200, raw=True)
                    api.request("GET", "/admin/v1/capabilities" if is_native else "/emby/Sessions", login, expected=401, raw=True)
                    report[login.kind + "_logout_protected_status"] = 401
                except BaseException:
                    report["cleanup"]["errors"].append("An owned logout and independent protected-endpoint denial could not be confirmed")
            report["http_method_counts"] = api.counts
            report["run_post_scanner_cache_preflights"] = api.cache_checks
        if baseline is not None:
            try:
                final = snapshot(core, utilities, database)
                report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                additions, counts = audit(core, baseline, final, api, schedule)
                if report["status"] == "passed":
                    check(len(additions["sessions"]) == 2 and len(additions["devices"]) == 1 and len(additions["task_runs"]) == 1 and
                          len(additions["task_run_requests"]) == 1 and len(additions["task_run_children"]) == len(additions["scan_jobs"]) == 5 and
                          len(additions["task_triggers"]) == 2 and report.get("schedule_restored_exact_rules_and_timezone") is True,
                          "The successful workflow did not retain its exact acknowledged additions")
                report["cleanup"].update({"proven": True, "new_credentials_revoked": len(additions["sessions"]),
                                           "retained_ordinary_devices": len(additions["devices"]), "physical_history_deletions": 0})
                report["owned_history_additions"] = {table: len(rows) for table, rows in sorted(additions.items())}
                report["eligible_directory_timestamp_changes"] = {"count": sum(counts.values()), "per_library_counts": counts}
                report["old_media_items_and_all_other_item_fields_exact"] = True
                report["old_keys_devices_users_policy_metadata_and_task_history_exact"] = True
                report["old_scan_jobs_and_expired_prepared_row_exact"] = True
            except BaseException:
                report["cleanup"]["errors"].append("Complete protected-row preservation or exclusive owned task history could not be proven")
        for label, initial, observe in (("deployment", deployment, lambda: sessions.deployment(accepted)),
                                        ("vault", vault, lambda: core.vault_identity(sessions)),
                                        ("media", media, lambda: devices.source_hashes(core, baseline, configured)),
                                        ("nfo", nfos, lambda: nfo_hashes(core, baseline, configured))):
            if initial is not None:
                try:
                    core.remaining()
                    check(observe() == initial, "A protected deployment, vault, media, or NFO identity changed")
                    report[label + "_unchanged"] = True
                except BaseException:
                    report["cleanup"]["errors"].append("A protected process, master, media, or NFO identity could not be proven unchanged")
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
                    private_failure_log(private_path, {"owner": OWNER, "failed_stage": report.get("failed_stage", "cleanup"), "task_http": api.trace if api else []})
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
