#!/usr/bin/env python3
"""Verify one deployed M4g forced refresh through authorized root SSH on Linux.

Set GOBY_MEDIA_REFRESH_EXPECTED_BINARY_SHA256 to the accepted build digest.
The only writes are one native administrator login/logout, one forced scan of
each existing library, and cancellation of an acknowledged owned scan on failure.
Complete database rows, paths, credentials and index details stay in memory.
Only counts, timestamps and aggregate hashes enter the exclusive private report.
"""

from __future__ import annotations

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
import selectors
import stat
import subprocess
import sys
import time


sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test")
RESULT = ROOT / "m4g-deployed-media-refresh.json"
ATTEMPT = ROOT / "m4g-deployed-media-refresh.attempt.json"
OWNER = "goby-media-refresh-deployed-m4g-v1"
ORIGIN = "http://127.0.0.1:18096"
TABLES = set("schema_migrations server_settings users sessions libraries library_roots items scan_jobs "
             "catalog_entities item_entities item_images user_item_data play_sessions item_subtitles "
             "encoding_jobs client_playback_references item_metadata_state".split())
ID, HASH, TOKEN = re.compile(r"[0-9a-f]{32}"), re.compile(r"[0-9a-f]{64}"), re.compile(r"[A-Za-z0-9_-]{43}")
JOB_FIELDS = set("Id LibraryId ForceProbe Status Error Scanned Added Updated CreatedAt StartedAt FinishedAt".split())
MAX_BODY, MAX_DATABASE, MAX_ROWS = 2 * 1024 * 1024, 8 * 1024 * 1024, 5000


class Failure(Exception):
    """Contain only a fixed, non-secret assertion label."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def helpers():
    modules = []
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        for name in ("metadata", "sessions"):
            specification = importlib.util.spec_from_file_location(
                "refresh_" + name, Path(__file__).with_name("verify-" + name + "-deployed.py"))
            check(specification is not None and specification.loader is not None, "Inert verification utilities are unavailable")
            module = importlib.util.module_from_spec(specification)
            specification.loader.exec_module(module)
            modules.append(module)
    return modules


def database_type(sessions):
    class Database(sessions.Database):
        def read(self, query, label):
            # Keep both child output streams bounded during capture, including
            # malformed data and failed queries. No raw output reaches disk.
            process = subprocess.Popen(["/usr/bin/psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"],
                                       stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       env=self.environment)
            output, errors = bytearray(), bytearray()
            try:
                process.stdin.write(query.encode("utf-8"))
                process.stdin.close()
                deadline = time.monotonic() + 15
                with selectors.DefaultSelector() as selector:
                    for stream, target, limit in ((process.stdout, output, MAX_DATABASE), (process.stderr, errors, 65536)):
                        os.set_blocking(stream.fileno(), False)
                        selector.register(stream, selectors.EVENT_READ, (target, limit))
                    while selector.get_map():
                        remaining = deadline - time.monotonic()
                        check(remaining > 0, "A read-only database observation timed out")
                        for key, _ in selector.select(min(remaining, 0.5)):
                            block = os.read(key.fd, 65536)
                            if not block:
                                selector.unregister(key.fileobj)
                                continue
                            target, limit = key.data
                            check(len(target) + len(block) <= limit, "A read-only database observation exceeded its byte bound")
                            target.extend(block)
                check(process.wait(timeout=max(0.1, deadline - time.monotonic())) == 0,
                      "A read-only database observation failed")
                return json.loads(output)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait(timeout=5)
                for stream in (process.stdin, process.stdout, process.stderr):
                    stream.close()
    return Database


def timestamp(value):
    check(isinstance(value, str), "A required timestamp is missing")
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    check(result.tzinfo is not None, "A required timestamp has no timezone")
    return result


def rows(snapshot, name):
    return [json.loads(value, parse_float=Decimal) for value in snapshot[name]]


def identical(left, right):
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(identical(value, right[key]) for key, value in left.items())
    if isinstance(left, list):
        return len(left) == len(right) and all(identical(a, b) for a, b in zip(left, right))
    return left == right


def indexed(snapshot, name):
    records = rows(snapshot, name)
    check(all(isinstance(row, dict) and isinstance(row.get("id"), str) and ID.fullmatch(row["id"]) for row in records),
          "A required row has an invalid identity")
    result = {row["id"]: row for row in records}
    check(len(result) == len(records), "A required row identity is duplicated")
    return result


def snapshot(utilities, database):
    value = utilities.table_snapshot(database)
    check(set(value) == TABLES and sum(map(len, value.values())) <= MAX_ROWS,
          "The complete snapshot exceeds the seventeen-table row scope")
    migrations = rows(value, "schema_migrations")
    check(len(migrations) == 15 and {row["version"] for row in migrations} == set(range(1, 16)),
          "The deployed schema is not exactly version fifteen")
    catalog = rows(value, "items")
    media = [row["media"] for row in catalog if row.get("media") is not None]
    check(len(catalog) == 21 and len(media) == 11 and all(isinstance(item, dict) and
          type(item.get("ProbeVersion")) is int and item["ProbeVersion"] == 6 for item in media),
          "The catalog is not the twenty-one existing items and eleven probe-six media items")
    return value


def quiescent(value, database):
    check(not any(row["status"] in {"Queued", "Running"} for row in rows(value, "scan_jobs")),
          "An existing scan is active; no additional scan was issued")
    check(not any(row["state"] in {"queued", "running"} for row in rows(value, "encoding_jobs")),
          "An existing conversion is active; no additional scan was issued")
    now = timestamp(database.read("SELECT to_json(clock_timestamp());", "Read-only playback expiry clock"))
    check(not any(row["state"] in {"Prepared", "Playing", "Paused"} and timestamp(row["expires_at"]) > now
                  for row in rows(value, "play_sessions")),
          "An unexpired old playback is active; no playback expiration or normalization is allowed")
    return sum(row["state"] in {"Prepared", "Playing", "Paused"} and timestamp(row["expires_at"]) <= now
               for row in rows(value, "play_sessions"))


def source_hashes(value, configured):
    roots, libraries = indexed(value, "library_roots"), indexed(value, "libraries")
    check(len(libraries) == len(roots) == 5 and len({row["library_id"] for row in roots.values()}) == 5,
          "The source scope is not five existing libraries with one root each")
    allowed = [Path(path) for path in configured.split(os.pathsep)]
    check(0 < len(allowed) <= 16 and all(path.is_absolute() and path.resolve(strict=True) == path and path.is_dir() for path in allowed),
          "The configured source roots are not canonical local directories")
    result, total = {}, 0
    for row in rows(value, "items"):
        if row["media"] is None:
            continue
        root = roots.get(row["root_id"], {})
        path, base = Path(row["path"]), Path(root.get("path", ""))
        check(root.get("library_id") == row["library_id"] and Path(root.get("allowed_path", "")) in allowed and
              base.is_absolute() and base.is_relative_to(Path(root["allowed_path"])) and
              base == Path(root["allowed_path"]) / root["relative_path"] and path.is_absolute() and
              path != base and path.is_relative_to(base) and path.resolve(strict=True) == path,
              "An existing source path escaped its configured library root")
        # Walk just this catalogued pathname using directory descriptors. Do not
        # enumerate configured external roots or follow any parent symlink.
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
            before = os.fstat(stream.fileno())
            check(stat.S_ISREG(before.st_mode) and 0 < before.st_size <= 64 * 1024 * 1024 and before.st_size == row["file_size"],
                  "An existing source is not a bounded regular media file")
            total += before.st_size
            check(total <= 256 * 1024 * 1024, "Existing source bytes exceed the hashing budget")
            digest = hashlib.sha256()
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(block)
            identity = lambda info: (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns, info.st_mode)
            check(identity(before) == identity(os.fstat(stream.fileno())) == identity(path.lstat()),
                  "An existing source changed while its hash was observed")
            result[row["id"]] = {"sha256": digest.hexdigest(), "identity": identity(before)}
    check(len(result) == 11, "The source hash population is not eleven existing media files")
    return result


def index_counts(value):
    result = {"media_count": 11, "indexed_media": 0, "index_count": 0, "entry_count": 0}
    for row in rows(value, "items"):
        if row["media"] is None:
            continue
        indexes = row["media"].get("VideoSeekIndexes")
        if indexes is None:
            indexes = []
        check(isinstance(indexes, list) and len(indexes) <= 64, "A technical index population exceeds its bound")
        result["indexed_media"] += bool(indexes)
        result["index_count"] += len(indexes)
        for index in indexes:
            check(isinstance(index, dict) and isinstance(index.get("entries"), list) and 0 < len(index["entries"]) <= 8192,
                  "A technical index entry population exceeds its bound")
            result["entry_count"] += len(index["entries"])
    return result


class API:
    def __init__(self, libraries):
        self.libraries, self.scans, self.cancelled, self.attempted_libraries = set(libraries), {}, set(), set()
        self.finished = {}
        self.token = self.csrf = ""
        self.login_attempted = self.logout_attempted = False
        self.counts = {"GET": 0, "POST": 0, "DELETE": 0}

    def request(self, method, path, *, body=None, expected=(200,), timeout=8):
        login = method == "POST" and path == "/admin/v1/session"
        if login:
            check(not self.login_attempted and not self.token, "An additional administrator login was prevented")
            self.login_attempted = True
        elif method == "GET":
            check(path in {"/admin/v1/session", "/admin/v1/libraries", "/admin/v1/jobs"}, "An HTTP read escaped the native scan scope")
        elif method == "DELETE":
            check(path == "/admin/v1/session" and self.token and self.csrf and not self.logout_attempted,
                  "An additional or unowned logout was prevented")
            self.logout_attempted = True
        else:
            scan = re.fullmatch(r"/admin/v1/libraries/([0-9a-f]{32})/scan", path)
            cancel = re.fullmatch(r"/admin/v1/jobs/([0-9a-f]{32})/cancel", path)
            check(method == "POST" and self.token and self.csrf, "An unauthenticated or unsupported mutation was prevented")
            if scan:
                check(scan[1] in self.libraries - self.attempted_libraries and body == {"ForceProbe": True},
                      "An additional scan or a scan outside the existing library scope was prevented")
                self.attempted_libraries.add(scan[1])
            else:
                check(cancel and cancel[1] in self.scans and cancel[1] not in self.cancelled and body == {},
                      "An unowned or repeated scan cancellation was prevented")
                self.cancelled.add(cancel[1])
        check(sum(self.counts.values()) < 650, "The native HTTP request budget was exceeded")
        self.counts[method] += 1
        headers = {"Accept": "application/json", "Origin": ORIGIN}
        if self.token:
            headers["Cookie"] = "goby_session=" + self.token
        if self.csrf and method != "GET":
            headers["X-CSRF-Token"] = self.csrf
        payload = None if body is None else json.dumps(body).encode("utf-8")
        if payload is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=timeout)
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            if login:
                cookie = SimpleCookie()
                cookie.load(response.getheader("Set-Cookie", ""))
                if "goby_session" in cookie and TOKEN.fullmatch(cookie["goby_session"].value):
                    self.token = cookie["goby_session"].value
            raw = response.read(MAX_BODY + 1)
            check(len(raw) <= MAX_BODY, "A native response exceeded its byte bound")
            value = json.loads(raw) if raw else None
            if isinstance(value, dict) and path == "/admin/v1/session" and method in {"GET", "POST"}:
                csrf = value.get("CSRFToken", "")
                if isinstance(csrf, str) and HASH.fullmatch(csrf):
                    self.csrf = csrf
            check(response.status in expected and (response.status != 204 or not raw), "A native response returned an unexpected status or body")
            return value
        finally:
            connection.close()


def owned_login(before, after, api, user_id):
    check(all(before[name] == after[name] for name in TABLES - {"sessions"}), "Administrator login changed a non-authentication table")
    old, new = indexed(before, "sessions"), indexed(after, "sessions")
    created = set(new) - set(old)
    check(len(created) == 1 and all(new.get(key) == row for key, row in old.items()), "Login changed old authentication or created extra sessions")
    row = new[next(iter(created))]
    check(api.token and row["token_hash"] == "\\x" + hashlib.sha256(api.token.encode("ascii")).hexdigest() and
          row["kind"] == "admin" and row["user_id"] == user_id and row["revoked_at"] is None and
          row["client_name"] == "Goby Dashboard" and row["device_id"] == "goby-dashboard",
          "The issued native credential does not identify exactly one new administrator session")
    return row


def preserve(before, after, api, login, earliest, *, revoked=False):
    for name in TABLES - {"sessions", "scan_jobs", "libraries", "items"}:
        check(before[name] == after[name], "A metadata, entity, subtitle, account, playback, reference, or user-data table changed")
    latest = datetime.now(timezone.utc) + timedelta(seconds=2)
    for name, admitted in (("libraries", {"last_scan_at"}), ("items", {"updated_at"})):
        old, new = indexed(before, name), indexed(after, name)
        check(set(old) == set(new), "An existing library or catalog item was added or removed")
        for key, original in old.items():
            current = new[key]
            left, right = ({field: value for field, value in row.items() if field not in admitted} for row in (original, current))
            if name == "items" and original["media"] is not None and original["library_id"] in api.scans.values():
                left["media"], right["media"] = ({field: value for field, value in row["media"].items() if field != "VideoSeekIndexes"}
                                                 for row in (original, current))
            check(identical(left, right), "A catalog or media fact changed outside its admitted refresh fields")
            for field in admitted:
                if current[field] != original[field]:
                    library_id = key if name == "libraries" else original["library_id"]
                    check(library_id in api.scans.values() and earliest <= timestamp(current[field]) <= latest,
                          "A catalog refresh timestamp escaped an acknowledged scan or its time window")
    for name in ("sessions", "scan_jobs"):
        old, new = indexed(before, name), indexed(after, name)
        old_raw, new_raw = ({json.loads(raw)["id"]: raw for raw in value[name]} for value in (before, after))
        check(all(new_raw.get(key) == raw for key, raw in old_raw.items()), "A preexisting authentication or scan-job row changed")
        created = set(new) - set(old)
        if name == "scan_jobs":
            check(created == set(api.scans), "New scan jobs escaped the acknowledged requests")
            check(all(new[key]["library_id"] == library and new[key]["force_probe"] is True for key, library in api.scans.items()),
                  "A stored new scan lost its library or forced-refresh flag")
            check(all(new_raw.get(key) == raw for key, raw in api.finished.items()), "An already completed owned scan row changed")
        else:
            check(created == ({login["id"]} if login else set()), "New authentication escaped the single attributable login")
            if login:
                row = new[login["id"]]
                check({key: value for key, value in row.items() if key not in {"last_seen_at", "revoked_at"}} ==
                      {key: value for key, value in login.items() if key not in {"last_seen_at", "revoked_at"}} and
                      timestamp(login["last_seen_at"]) <= timestamp(row["last_seen_at"]) <= latest,
                      "The owned login changed a fixed field or its activity time regressed")
                check(row["revoked_at"] is None if not revoked else earliest <= timestamp(row["revoked_at"]) <= latest,
                      "The owned login revocation does not match the expected cleanup state")


def wait_job(api, job_id, timeout):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = api.request("GET", "/admin/v1/jobs", timeout=min(8, max(0.1, deadline - time.monotonic())))
        check(isinstance(value, dict) and isinstance(value.get("Items"), list) and len(value["Items"]) <= 1000,
              "The native job list exceeded its scope")
        matches = [row for row in value["Items"] if row.get("Id") == job_id]
        check(len(matches) == 1 and set(matches[0]) == JOB_FIELDS and matches[0]["ForceProbe"] is True and
              matches[0]["LibraryId"] == api.scans[job_id], "The native job lost its identity, fields, or forced-refresh mode")
        if matches[0]["Status"] not in {"pending", "running"}:
            return matches[0]
        time.sleep(min(1, max(0, deadline - time.monotonic())))
    raise Failure("An acknowledged forced scan exceeded its terminal-state deadline")


def main():
    report = {"owner": OWNER, "status": "failed", "http_retries": 0, "snapshots": {}, "scans": [], "cleanup_errors": [],
              "source_hash_scope": "Only eleven catalogued media paths; read-only descriptors; no external-root enumeration",
              "index_scope": "Bounded count evidence; no claim that every media format produces a technical seek index",
              "admitted_existing_fields": ["Acknowledged libraries.last_scan_at", "Acknowledged items.updated_at",
                                            "Acknowledged media.VideoSeekIndexes"]}
    utilities = sessions = database = api = before = login = accepted_deployment = parent_identity = files = None
    earliest, stage = datetime.now(timezone.utc), "preflight"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and not sys.argv[1:],
              "Run only through authorized root SSH on Linux without arguments")
        accepted = os.environ.get("GOBY_MEDIA_REFRESH_EXPECTED_BINARY_SHA256", "")
        check(HASH.fullmatch(accepted), "Set the accepted deployed executable digest in GOBY_MEDIA_REFRESH_EXPECTED_BINARY_SHA256")
        utilities, sessions = helpers()
        os.umask(0o077)
        info = ROOT.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
              "The fixed report directory has unsafe ownership")
        check(not any(path.exists() or path.is_symlink() for path in (RESULT, ATTEMPT)), "A prior attempt exists; retry and overwrite are forbidden")
        utilities.private_write(ATTEMPT, {"owner": OWNER, "started_at": earliest.isoformat(),
                                         "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
        parent_identity = (info.st_dev, info.st_ino)
        stage = "accepted deployment and complete baseline"
        accepted_deployment = sessions.deployment(accepted)
        report["deployment"] = {**accepted_deployment, "schema_version": 15, "probe_version": 6}
        database = database_type(sessions)()
        before = snapshot(utilities, database)
        report["snapshots"]["before"] = utilities.snapshot_report(before)
        report["preexisting_expired_unfinalized_playbacks"] = quiescent(before, database)
        check(all(row.get("force_probe") is False for row in rows(before, "scan_jobs")),
              "The first deployed refresh requires every preexisting schema-fifteen job to retain its false migration default")
        check(0 < len(before["scan_jobs"]) <= 990, "The existing job inventory is outside complete native polling scope")
        report["preexisting_scan_job_count"] = len(before["scan_jobs"])
        report["preexisting_scan_modes_all_false"] = True
        libraries = indexed(before, "libraries")
        counts = {key: sum(row["library_id"] == key and row["media"] is not None for row in rows(before, "items")) for key in libraries}
        check(len(libraries) == 5 and all(count > 0 for count in counts.values()) and sum(counts.values()) == 11,
              "The existing media population does not span exactly five nonempty libraries")
        configured = sessions.private_values(ROOT / "runtime.env", {"GOBY_MEDIA_ROOTS"})["GOBY_MEDIA_ROOTS"]
        files = source_hashes(before, configured)
        report["source_before_sha256"], report["index_before"] = utilities.aggregate(files), index_counts(before)
        credentials = sessions.private_values(ROOT / "browser.env", {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        users = [row for row in rows(before, "users") if row["name"] == credentials["GOBY_SMOKE_NAME"]]
        check(len(users) == 1 and users[0]["is_administrator"] is True and users[0]["is_disabled"] is False,
              "Private credentials do not name one existing enabled administrator")
        stage = "single native administrator login"
        api = API(libraries)
        response = api.request("POST", "/admin/v1/session", body={"Name": credentials["GOBY_SMOKE_NAME"], "Password": credentials["GOBY_SMOKE_PASSWORD"]})
        credentials.clear()
        current = snapshot(utilities, database)
        login = owned_login(before, current, api, users[0]["id"])
        report["snapshots"]["after_login"] = utilities.snapshot_report(current)
        check(api.csrf and isinstance(response, dict) and response.get("User", {}).get("Id") == users[0]["id"],
              "Native login omitted CSRF protection or returned another administrator")
        inventory = api.request("GET", "/admin/v1/libraries")
        check(isinstance(inventory, dict) and inventory.get("TotalRecordCount") == 5 and len(inventory.get("Items", [])) == 5 and
              {row["Id"] for row in inventory["Items"]} == set(libraries), "Native libraries differ from the existing catalog")
        for ordinal, library_id in enumerate(sorted(libraries), 1):
            stage = "sequential forced refresh of existing library " + str(ordinal)
            current = snapshot(utilities, database)
            quiescent(current, database)
            preserve(before, current, api, login, earliest)
            queued_at = datetime.now(timezone.utc)
            response = api.request("POST", "/admin/v1/libraries/" + library_id + "/scan", body={"ForceProbe": True}, expected=(202,))
            job = response.get("Job") if isinstance(response, dict) else None
            check(isinstance(job, dict) and ID.fullmatch(job.get("Id", "")) and job["Id"] not in indexed(before, "scan_jobs") and
                  job["Id"] not in api.scans and job.get("LibraryId") == library_id and job.get("ForceProbe") is True,
                  "The queued scan did not acknowledge a new forced job for the existing library")
            api.scans[job["Id"]] = library_id
            terminal = wait_job(api, job["Id"], 120)
            check(terminal["Status"] == "completed" and terminal["Error"] == "" and
                  all(type(terminal[field]) is int for field in ("Scanned", "Updated", "Added")) and
                  terminal["Scanned"] == terminal["Updated"] == counts[library_id] and terminal["Added"] == 0,
                  "A forced scan did not complete with the expected existing-file and update counts")
            created, started, finished = (timestamp(terminal[field]) for field in ("CreatedAt", "StartedAt", "FinishedAt"))
            check(queued_at - timedelta(seconds=2) <= created <= started <= finished <= datetime.now(timezone.utc) + timedelta(seconds=2),
                  "A forced job has stale or unordered lifecycle timestamps")
            current = snapshot(utilities, database)
            stored = indexed(current, "scan_jobs")[job["Id"]]
            check(stored["status"] == "Completed" and stored["error"] == "" and stored["cancel_requested"] is False and
                  stored["scanned"] == stored["updated"] == counts[library_id] and stored["added"] == 0 and
                  all(timestamp(stored[column]) == timestamp(terminal[field]) for field, column in
                      (("CreatedAt", "created_at"), ("StartedAt", "started_at"), ("FinishedAt", "finished_at"))),
                  "The terminal HTTP job differs from the durable database result")
            old_items = indexed(before, "items")
            refreshed = [row for row in rows(current, "items") if row["library_id"] == library_id and row["media"] is not None]
            check(all(row["updated_at"] != old_items[row["id"]]["updated_at"] and
                      created <= timestamp(row["updated_at"]) <= finished for row in refreshed) and
                  started <= timestamp(indexed(current, "libraries")[library_id]["last_scan_at"]) <= finished,
                  "The forced refresh did not persist current item and library timestamps")
            api.finished[job["Id"]] = next(raw for raw in current["scan_jobs"] if json.loads(raw)["id"] == job["Id"])
            preserve(before, current, api, login, earliest)
            report["scans"].append({"library_ordinal": ordinal, "library_identity_sha256": utilities.aggregate(library_id),
                                    "job_identity_sha256": utilities.aggregate(job["Id"]), "force_probe": True,
                                    "scanned": terminal["Scanned"], "updated": terminal["Updated"], "added": 0,
                                    "created_at": terminal["CreatedAt"], "started_at": terminal["StartedAt"], "finished_at": terminal["FinishedAt"]})
        report["status"] = "passed"
    except BaseException as error:
        report["failed_stage"] = stage
        safe_types = tuple([Failure] + [module.Failure for module in (utilities, sessions) if module is not None])
        report["error"] = str(error) if isinstance(error, safe_types) else "A protected verification operation failed; raw exception details were withheld"
        report["error_type"] = type(error).__name__
    finally:
        if api is not None:
            for job_id in api.scans:
                try:
                    current = snapshot(utilities, database)
                    if indexed(current, "scan_jobs")[job_id]["status"] in {"Queued", "Running"}:
                        api.request("POST", "/admin/v1/jobs/" + job_id + "/cancel", body={}, expected=(202,))
                        wait_job(api, job_id, 15)
                except BaseException:
                    report["cleanup_errors"].append("An acknowledged owned scan could not be confirmed terminal")
            if api.login_attempted:
                try:
                    check(api.token, "An attempted login lost its credential and cannot be safely cleaned up")
                    if not api.csrf:
                        api.request("GET", "/admin/v1/session")
                    api.request("DELETE", "/admin/v1/session", expected=(204,))
                except BaseException:
                    report["cleanup_errors"].append("The single native administrator logout could not be confirmed")
            report["http_method_counts"] = dict(api.counts)
            report["acknowledged_scans"], report["attempted_scans"] = len(api.scans), len(api.attempted_libraries)
        if database is not None and before is not None:
            try:
                final = snapshot(utilities, database)
                report["snapshots"]["after_cleanup"] = utilities.snapshot_report(final)
                if api is not None:
                    preserve(before, final, api, login, earliest, revoked=True)
                else:
                    check(before == final, "A pre-login failure changed database rows")
                quiescent(final, database)
                report["preexisting_semantics_preserved"] = True
                report["preexisting_scan_jobs_and_authentication_exact"] = True
                report["metadata_entities_subtitles_user_data_exact"] = True
                report["own_administrator_revoked"] = bool(login)
                report["index_after"] = index_counts(final)
                if files is not None:
                    after_files = source_hashes(final, configured)
                    check(files == after_files, "An existing source file or its identity changed")
                    report["source_after_sha256"] = utilities.aggregate(after_files)
                    report["eleven_source_files_unchanged"] = True
                if report["status"] == "passed":
                    check(len(api.scans) == len(report["scans"]) == 5 and login is not None,
                          "The successful run did not complete exactly five scans and one revoked administrator login")
            except BaseException:
                report["cleanup_errors"].append("Complete final preservation, source hashes, or owned-login cleanup could not be proven")
        if accepted_deployment is not None:
            try:
                check(sessions.deployment(accepted) == accepted_deployment, "The accepted deployed process or executable changed")
                report["deployment_unchanged"] = True
            except BaseException:
                report["cleanup_errors"].append("The final PID, start ticks, UID, and executable digest could not be reconfirmed")
        if report["cleanup_errors"]:
            report["status"] = "failed"
        report["finished_at"] = datetime.now(timezone.utc).isoformat()
        if parent_identity is not None:
            try:
                info = ROOT.lstat()
                check((info.st_dev, info.st_ino) == parent_identity and stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                      info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT, "The fixed report directory changed")
                utilities.private_write(RESULT, report)
            except BaseException:
                report = {"owner": OWNER, "status": "failed", "failed_stage": "exclusive report persistence",
                          "error": "The private report could not be persisted; no retry or overwrite was attempted"}
        print(json.dumps(report, ensure_ascii=True, sort_keys=True))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
