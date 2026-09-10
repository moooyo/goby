#!/usr/bin/env python3
"""Create and retain one backup on an explicitly accepted M5j deployment.

Root SSH only. Required environment pins are
GOBY_BACKUP_RECOVERY_EXPECTED_DEPLOYMENT_SHA256 and
GOBY_BACKUP_RECOVERY_EXPECTED_CANDIDATE_SHA256. Both refer to the fixed private
deployment evidence and candidate copy below, never an assumed prior build.
GOBY_BACKUP_RECOVERY_VERIFICATION_RUN selects an unused decimal 1..99 (default
1); there is no automatic retry or reuse. This script never restores, rolls
back, imports, cancels, deletes, changes settings, creates users, writes SQL,
restarts services, or changes the service's cgroup limits.

Read the existing administrator login from root-private browser.env. Runtime
and recovery environment files are only read/fingerprinted. Preserve all old
schema-23 rows. Expected new history is one revoked native session, its two
activity records, backup requested/finished and three download-grant records.
HEAD and Range each create a real download-grant audit; only complete bytes
and their digest prove a successful attachment transfer.

The strong random passphrase is fsynced before API admission into a separate
owned UID-995 directory outside all three strict recovery stores. Retain it
even on uncertainty/failure. Retain the encrypted downloaded attachment beside
it; a safe pairing document identifies both files without exposing a secret.
Root-only reports contain safe summaries/hashes, never raw SQL rows, response
bodies, passwords, passphrases, DSNs, cookies, CSRF values or master keys.

Serialize this run with other administrator writes and memory-heavy tests.
Recommended verifier envelope: MemoryMax=1536M, MemorySwapMax=0, CPUQuota=150%.
The asynchronous worker remains within the accepted main-service limits; this
script does not pretend a verifier cgroup also constrains that worker.
"""

from collections import Counter
from datetime import datetime, timedelta, timezone
from email.message import Message
import base64
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path, PurePosixPath
import re
import secrets
import shlex
import shutil
import signal
import stat
import subprocess
import sys
import time
from urllib.parse import quote, unquote, urlsplit

sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test")
DEPLOYED = ROOT / "backups/m5j-20260910/m5j-deployment-evidence.json"
CANDIDATE = DEPLOYED.parent / "candidate.json"
LIVE = Path("/opt/goby-dev/goby")
RUNTIME, BROWSER, RECOVERY_ENV = ROOT / "runtime.env", ROOT / "browser.env", ROOT / "recovery-m5j.env"
PRIVATE_PARENT = Path("/var/lib/goby-test")
SECRET_ROOT = PRIVATE_PARENT / "operator-secrets-m5j"
STRICT_DIRECTORIES = tuple(PRIVATE_PARENT / name for name in ("recovery-m5j", "backups-m5j", "recovery-operations-m5j"))
OWNER = "goby-backup-recovery-deployed-m5j-v1"
SECRET_OWNER = b"goby-backup-recovery-operator-secrets-m5j-v1\n"
SERVICE, REFERENCE_SERVICE = "goby-foundation-test.service", "goby-emby-reference.service"
REFERENCE_PID, REFERENCE_START = 3131777, "13964831"
ORIGIN = "http://127.0.0.1:18096"
SESSION, BACKUPS, OPERATIONS = "/admin/v1/session", "/admin/v1/backups", "/admin/v1/backup-operations"
STATUS = BACKUPS + "/status"
MIB = 1024 * 1024
MAX_BODY, MAX_DOWNLOAD, MAX_JSON_TOTAL = MIB, 64 * MIB, 16 * MIB
MAIN_SECONDS, CLEANUP_SECONDS, MAX_HTTP, MAX_POLLS = 300, 60, 120, 90
ID, HASH, TOKEN = re.compile(r"[0-9a-f]{32}"), re.compile(r"[0-9a-f]{64}"), re.compile(r"[A-Za-z0-9_-]{43}")
TABLES = set("schema_migrations server_settings users sessions libraries library_roots items scan_jobs catalog_entities item_entities "
    "item_images user_item_data play_sessions item_subtitles encoding_jobs client_playback_references item_metadata_state application_keys "
    "application_key_clients devices application_key_devices task_definitions task_triggers task_runs task_run_requests task_run_children "
    "task_occurrences managed_settings activity_entries".split())
ACTIVITY_COLUMNS = set("id created_at action severity source actor_kind actor_id actor_credential_id resource_kind resource_id request_id "
                       "revision affected_count state changed_fields".split())
BACKUP_FIELDS = set("Id Kind State CreatedAt UpdatedAt SizeBytes SHA256 Verified ErrorCode Source".split())
OPERATION_FIELDS = set("Id RequestId Revision Kind State Phase BackupId CreatedAt UpdatedAt ErrorCode Source RestoreDefaults ReplaceRollback "
                       "CanCancel CanApply GenerationRevision".split())
STATUS_FIELDS = set("Available UnavailableReason RestoreAvailable RestoreUnavailableReason Busy ActiveOperationId GenerationRevision Limits Storage Rollback".split())
STARTED, PHASE_END = time.monotonic(), 0.0


class Failure(Exception):
    """Only fixed, non-secret labels may escape to a safe result."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def canonical(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":"), allow_nan=False).encode()


def sha(value):
    return hashlib.sha256(value).hexdigest()


def identity(info):
    return [info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns, info.st_mode, info.st_uid, info.st_gid, info.st_nlink]


def timeout(maximum=10):
    remaining = PHASE_END - time.monotonic()
    check(remaining > 0.05, "The bounded workflow phase expired")
    return min(maximum, remaining)


def expired(_number, _frame):
    raise Failure("The bounded workflow phase expired")


def phase(seconds):
    global PHASE_END
    PHASE_END = time.monotonic() + seconds
    signal.setitimer(signal.ITIMER_REAL, seconds)


def safe_step(seconds, action, errors, label):
    try:
        phase(seconds)
        action()
        return True
    except BaseException:
        errors.append(label)
        return False
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


def directory(path, owner=0, mode=None):
    check(path.is_absolute() and ".." not in path.parts and path.resolve(strict=True) == path, "A directory is not canonical")
    value = path.lstat()
    check(stat.S_ISDIR(value.st_mode) and value.st_uid == owner and value.st_mode & 0o022 == 0 and
          (mode is None or stat.S_IMODE(value.st_mode) == mode), "A directory has unsafe ownership or mode")
    return value


def read_file(path, maximum=MIB, owner=0, mode=None):
    check(path.is_absolute() and ".." not in path.parts and path.resolve(strict=True) == path, "A file pathname is not canonical")
    descriptor = os.open("/", os.O_RDONLY | os.O_DIRECTORY)
    try:
        for part in path.parts[1:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=descriptor)
            os.close(descriptor)
            descriptor = child
            check(os.fstat(child).st_mode & 0o022 == 0, "A file ancestor is writable by unrelated users")
        source = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME, dir_fd=descriptor)
    finally:
        os.close(descriptor)
    with os.fdopen(source, "rb") as stream:
        before = os.fstat(stream.fileno())
        check(stat.S_ISREG(before.st_mode) and before.st_uid == owner and before.st_nlink == 1 and before.st_size <= maximum and
              (mode is None or stat.S_IMODE(before.st_mode) == mode), "An input file is not owned, regular or bounded")
        body = stream.read(maximum + 1)
        check(len(body) == before.st_size and identity(before) == identity(os.fstat(stream.fileno())) == identity(path.lstat()),
              "An input changed during observation")
    return body, {"identity": identity(before), "sha256": sha(body)}


def file_digest(path, maximum=256 * MIB, owner=0):
    check(path.is_absolute() and ".." not in path.parts and path.resolve(strict=True) == path, "A preserved file escaped its canonical path")
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NOATIME)
    with os.fdopen(descriptor, "rb") as stream:
        before = os.fstat(stream.fileno())
        check(stat.S_ISREG(before.st_mode) and before.st_uid == owner and before.st_nlink == 1 and before.st_size <= maximum,
              "A preserved file has an unexpected owner, type, links or size")
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
        check(identity(before) == identity(os.fstat(stream.fileno())) == identity(path.lstat()), "A preserved file changed during hashing")
    return {"identity": identity(before), "sha256": digest}


def write_exclusive(path, payload, *, owner=0, group=0):
    check(isinstance(payload, bytes) and len(payload) <= 8 * MIB, "A private evidence write exceeded its bound")
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as output:
        os.fchown(output.fileno(), owner, group)
        output.write(payload)
        output.flush()
        os.fsync(output.fileno())
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def write_json(path, value, *, owner=0, group=0):
    write_exclusive(path, canonical(value) + b"\n", owner=owner, group=group)


def private_values(path, names):
    raw, proof = read_file(path, 65536, mode=0o600)
    values = {}
    for line in raw.decode("utf-8").splitlines():
        name, separator, value = line.strip().removeprefix("export ").partition("=")
        if separator and name in names:
            parts = shlex.split(value, comments=False, posix=True)
            check(name not in values and len(parts) == 1 and parts[0], "A required private assignment is invalid")
            values[name] = parts[0]
    check(set(values) == set(names), "Required private configuration is missing")
    return values, proof


def command(arguments, *, environment=None, data=None, maximum=16 * MIB):
    result = subprocess.run(arguments, env=environment, input=data, capture_output=True, timeout=timeout(15))
    check(result.returncode == 0 and len(result.stdout) <= maximum and len(result.stderr) <= 65536,
          "A bounded read-only command failed")
    return result.stdout


def service_identity(name, expected_pid=None, expected_uid=None, binary=None):
    raw = command(["/usr/bin/systemctl", "show", name, "-p", "MainPID", "-p", "ActiveState", "-p", "User", "-p", "InvocationID"], maximum=4096)
    values = dict(line.split("=", 1) for line in raw.decode().splitlines() if "=" in line)
    check(values.get("ActiveState") == "active" and values.get("MainPID", "").isdigit(), "A protected service is not active")
    pid = int(values["MainPID"])
    folder = Path("/proc") / str(pid)
    check(pid > 1 and (expected_pid is None or pid == expected_pid), "A service PID differs from its accepted evidence")
    status = (folder / "status").read_text()
    uids = [int(value) for value in next(line for line in status.splitlines() if line.startswith("Uid:")).split()[1:]]
    check(expected_uid is None or uids == [expected_uid] * 4, "A service UID differs from its accepted owner")
    ticks = (folder / "stat").read_text().rsplit(") ", 1)[1].split()[19]
    executable = os.readlink(folder / "exe")
    result = {"pid": pid, "uid": uids[0], "start_ticks": ticks, "executable": executable,
              "network_namespace": os.readlink(folder / "ns/net"), "invocation_id": values.get("InvocationID")}
    if binary:
        check(executable == str(LIVE), "The main executable path differs")
        with (folder / "exe").open("rb") as source:
            check(os.fstat(source.fileno()).st_size <= 256 * MIB, "The main executable exceeds its bound")
            digest = hashlib.file_digest(source, "sha256").hexdigest()
        check(digest == binary, "The running executable differs from the accepted candidate")
        result["binary_sha256"] = digest
    return result


def accepted_deployment():
    deployment_raw, deployment_proof = read_file(DEPLOYED, 4 * MIB, mode=0o600)
    candidate_raw, candidate_proof = read_file(CANDIDATE, 4 * MIB, mode=0o600)
    expected_deployment = os.environ.get("GOBY_BACKUP_RECOVERY_EXPECTED_DEPLOYMENT_SHA256", "")
    expected_candidate = os.environ.get("GOBY_BACKUP_RECOVERY_EXPECTED_CANDIDATE_SHA256", "")
    check(HASH.fullmatch(expected_deployment) and HASH.fullmatch(expected_candidate) and deployment_proof["sha256"] == expected_deployment and
          candidate_proof["sha256"] == expected_candidate, "Explicit accepted deployment and candidate hashes are required")
    deployed, candidate = json.loads(deployment_raw), json.loads(candidate_raw)
    check(deployed.get("owner") == "goby-m5j-deployment-backup-v1" and deployed.get("status") == "passed" and
          deployed.get("schema_version") == 23 and deployed.get("probe_version") == 6 and
          deployed.get("candidate_manifest_sha256") == expected_candidate and candidate.get("owner") == "goby-m5j-candidate-v1",
          "A completed accepted M5j deployment is unavailable")
    binary = candidate.get("binary", {})
    check(binary.get("path") == str(ROOT / "exec-scratch/goby-m5j-linux-amd64") and HASH.fullmatch(binary.get("sha256", "")) and
          deployed.get("binary_sha256") == binary["sha256"], "Candidate and deployment binaries disagree")
    check(candidate.get("gates") == deployed.get("accepted_gates") and set(candidate["gates"]) == {"final_go", "full", "browser", "recovery"},
          "The accepted final candidate gates differ")
    inputs = {str(DEPLOYED): deployment_proof, str(CANDIDATE): candidate_proof}
    for gate in candidate["gates"].values():
        path = Path(gate.get("evidence_path", ""))
        check(gate.get("status") == "passed" and HASH.fullmatch(gate.get("sha256", "")) and path.is_absolute() and ".." not in path.parts and
              (ROOT in path.parents or Path("/dev/shm") in path.parents), "A candidate gate escaped its accepted evidence scope")
        proof = file_digest(path, 128 * MIB)
        check(proof["sha256"] == gate["sha256"], "An accepted candidate gate changed")
        inputs[str(path)] = proof
    for name in ("isolated_restore_verified", "isolated_restore_database_removed", "backup_complete_before_service_stop",
                 "all_old_public_business_fields_preserved", "master_runtime_unit_media_preserved", "original_reference_process_preserved",
                 "new_owned_recovery_dropin_verified", "recovery_database_url_configured"):
        check(deployed.get(name) is True, "The protected M5j deployment lacks a required acceptance proof")
    check(deployed.get("isolated_restore_tables_raw_exact") == 29 and deployed.get("live_database_restore_executed") is False,
          "The deployment restore rehearsal is not the accepted isolated scope")
    main = service_identity(SERVICE, deployed["main_pid"], 995, binary["sha256"])
    check(main["start_ticks"] == deployed.get("start_ticks"), "The main process start identity differs")
    reference = service_identity(REFERENCE_SERVICE, REFERENCE_PID, 0)
    check(reference["start_ticks"] == REFERENCE_START, "The original Emby process start identity differs")
    return deployed, candidate, inputs, main, reference


class Database:
    """No SQL write is possible through this fixed read-only observer."""

    def __init__(self, uri):
        parsed = urlsplit(uri)
        check(parsed.scheme in {"postgres", "postgresql"} and parsed.hostname == "127.0.0.1" and parsed.port in {None, 5432} and
              unquote(parsed.path) == "/goby_test" and unquote(parsed.username or "") == "goby_test" and parsed.password and
              parsed.query in {"", "sslmode=disable"} and not parsed.fragment, "The observer database escaped the protected primary scope")
        self.environment = {"PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", "PGHOST": "127.0.0.1", "PGPORT": "5432", "PGDATABASE": "goby_test",
            "PGUSER": "goby_test", "PGPASSWORD": unquote(parsed.password), "PGPASSFILE": "/dev/null", "PGCONNECT_TIMEOUT": "5",
            "PGSSLMODE": "disable", "PGCLIENTENCODING": "UTF8", "PGOPTIONS": "-c default_transaction_read_only=on -c statement_timeout=5000 "
            "-c lock_timeout=2000 -c timezone=UTC -c bytea_output=hex -c DateStyle=ISO,YMD"}
        self.queries = 0

    def read(self, query):
        check(self.queries < 30 and query.startswith(("SELECT ", "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;")),
              "The read-only database request budget or query kind differs")
        self.queries += 1
        raw = command(["/usr/lib/postgresql/17/bin/psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"],
                      environment=self.environment, data=query.encode())
        return json.loads(raw)

    def snapshot(self):
        names = self.read("SELECT COALESCE(json_agg(tablename ORDER BY tablename),'[]'::json) FROM pg_tables WHERE schemaname='public';")
        check(isinstance(names, list) and len(names) == 29 and set(names) == TABLES, "The primary public schema is not the exact 29-table inventory")
        fields = ["'" + name + "',COALESCE((SELECT json_agg(row_text ORDER BY row_text) FROM "
                  '(SELECT to_jsonb(t)::text AS row_text FROM public."' + name + '" t) rows),\'[]\'::json)' for name in sorted(TABLES)]
        result = self.read("BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT json_build_object(" + ",".join(fields) + "); COMMIT;")
        check(isinstance(result, dict) and set(result) == TABLES and all(isinstance(rows, list) and all(isinstance(row, str) for row in rows)
              for rows in result.values()) and sum(map(len, result.values())) <= 10000, "The complete old-row snapshot exceeds its bound")
        migrations = records(result, "schema_migrations")
        check(len(migrations) == 23 and {row["version"] for row in migrations} == set(range(1, 24)), "The deployed schema is not exactly 23")
        return {name: sorted(rows) for name, rows in result.items()}


def records(snapshot, table):
    return [json.loads(row) for row in snapshot[table]]


def snapshot_report(snapshot):
    return {name: {"count": len(rows), "sha256": sha(canonical(rows)), "row_sha256": sorted(sha(row.encode()) for row in rows)}
            for name, rows in sorted(snapshot.items())}


def added_rows(before, after, table):
    old, new = Counter(before[table]), Counter(after[table])
    check(not old - new, "A preexisting database row changed or disappeared")
    return [json.loads(raw) for raw in (new - old).elements()]


def old_rows_exact(before, after):
    check(set(before) == set(after) == TABLES, "The public table inventory changed")
    for table in TABLES:
        if table in {"sessions", "activity_entries"}:
            added_rows(before, after, table)
        else:
            check(before[table] == after[table], "An old business table changed during backup acceptance")


def idle(snapshot):
    for table, field, states in (("scan_jobs", "status", {"Queued", "Running"}), ("encoding_jobs", "state", {"queued", "running"}),
                                 ("task_runs", "state", {"queued", "running", "cancelling"}),
                                 ("task_run_children", "state", {"queued", "running", "cancelling"})):
        check(not any(row.get(field) in states for row in records(snapshot, table)), "An existing background operation is active")
    check(not any(row.get("retired_at") is None for row in records(snapshot, "task_triggers")), "An old task schedule can race preservation")
    now = datetime.now(timezone.utc)
    check(not any(row.get("state") in {"Prepared", "Playing", "Paused"} and datetime.fromisoformat(row["expires_at"]) > now
                  for row in records(snapshot, "play_sessions")), "An existing playback is active")
    check(all(datetime.fromisoformat(row["created_at"]) > now - timedelta(days=1) + timedelta(minutes=10)
              for row in records(snapshot, "activity_entries")), "An old activity could cross the minimum retention boundary")


def physical_sources(snapshot, configured):
    roots = {row["id"]: row for row in records(snapshot, "library_roots")}
    allowed, result, total = {Path(value) for value in configured.split(os.pathsep)}, {}, 0
    for item in records(snapshot, "items"):
        for value, expected_hash in ((item.get("path") if item.get("media") is not None else None, None),
                                     (item.get("local_metadata_path"), item.get("local_metadata_hash"))):
            if not value:
                continue
            root = roots.get(item["root_id"], {})
            base, approved = Path(root.get("path", "")), Path(root.get("allowed_path", ""))
            check(approved in allowed and base.is_absolute() and base.is_relative_to(approved) and
                  root.get("library_id") == item["library_id"], "A recorded media root is not approved")
            path = Path(value)
            if expected_hash:
                check(not path.is_absolute(), "A sidecar escaped its relative path")
                path = base / path
            check(path != base and ".." not in path.parts and path.is_relative_to(base), "A media or sidecar path escaped its owned root")
            if str(path) in result:
                continue
            owner = path.lstat().st_uid
            check(owner in {0, 995}, "A preserved media or sidecar owner is outside the test fixture")
            proof = file_digest(path, 128 * MIB, owner)
            total += proof["identity"][2]
            check(total <= 512 * MIB and (not expected_hash or proof["sha256"] == expected_hash), "Media bytes exceed the bound or sidecar hash differs")
            result[str(path)] = proof
    return result


def secret_absence(raw, values):
    for value in values:
        if value:
            encoded = value.encode()
            candidates = {encoded, quote(value, safe="").encode(), json.dumps(value)[1:-1].encode(), base64.b64encode(encoded),
                          base64.urlsafe_b64encode(encoded).rstrip(b"=")}
            check(all(candidate not in raw for candidate in candidates), "A safe observation exposed a workflow secret")


class API:
    def __init__(self, before, administrator, password, request_id, passphrase):
        self.before, self.administrator, self.password = before, administrator, password
        self.request_id, self.passphrase = request_id, passphrase
        self.cookie = self.csrf = self.cleanup_csrf = self.session_id = ""
        self.login_attempted = self.logout_attempted = self.create_attempted = False
        self.denied = self.barrier = False
        self.operation_id = self.backup_id = ""
        self.calls, self.json_bytes, self.download_grants = [], 0, 0
        self.worker_observation = {"State": "not_admitted", "Phase": "", "ErrorCode": ""}
        self.service_check = None
        self.finished = False

    def secrets(self):
        return [self.password, self.passphrase, self.cookie, self.csrf, self.cleanup_csrf]

    def authorize(self, method, path, body, byte_range):
        check(not urlsplit(path).query and "?" not in path and not urlsplit(path).scheme and not urlsplit(path).netloc and
              not urlsplit(path).fragment and path.startswith("/admin/v1/"), "A request escaped the fixed native route scope")
        signing_in = (method, path) == ("POST", SESSION)
        retiring = (method, path) == ("DELETE", SESSION)
        if signing_in:
            check(not self.login_attempted and not self.cookie and body == {"Name": self.administrator["name"], "Password": self.password},
                  "An extra login or different existing administrator was prevented")
        elif retiring:
            check(self.finished and self.cookie and not self.logout_attempted and body is None and (self.csrf or self.cleanup_csrf),
                  "Logout lacks its exact acknowledged native credential")
        elif (method, path) == ("POST", BACKUPS):
            check(not self.finished and self.cookie and self.csrf and not self.create_attempted and
                  body == {"RequestId": self.request_id, "Passphrase": self.passphrase}, "Creation escaped its one exact new backup request")
        else:
            permitted = {SESSION, STATUS, BACKUPS, OPERATIONS}
            if self.operation_id:
                permitted.add(OPERATIONS + "/" + self.operation_id)
            if self.backup_id:
                permitted |= {BACKUPS + "/" + self.backup_id, BACKUPS + "/" + self.backup_id + "/file"}
            check(method in {"GET", "HEAD"} and self.cookie and body is None and path in permitted,
                  "A read escaped the acknowledged operation and backup IDs")
            file_route = self.backup_id and path == BACKUPS + "/" + self.backup_id + "/file"
            check(method != "HEAD" or file_route and not self.finished, "HEAD escaped the sole new attachment")
            check(not self.finished or path in {SESSION, STATUS}, "Cleanup attempted an unrelated business request")
            if byte_range:
                check(file_route and method == "GET" and byte_range == "bytes=0-63" and not self.finished, "An unapproved byte range was prevented")
        check(not byte_range or not signing_in and not retiring, "A mutation supplied a Range header")
        limit = MAX_HTTP if self.finished else MAX_HTTP - 4
        check(len(self.calls) < limit, "The HTTP attempt budget is exhausted")
        return signing_in, retiring

    def request(self, method, path, *, body=None, expected=200, byte_range=None, output=None, limit=MAX_BODY):
        check(callable(self.service_check), "An HTTP request has no accepted live service authority")
        self.service_check()
        signing_in, retiring = self.authorize(method, path, body, byte_range)
        headers = {"Accept": "application/json", "Origin": ORIGIN, "Connection": "close"}
        if self.cookie:
            headers["Cookie"] = "goby_session=" + self.cookie
        if self.csrf or retiring and self.cleanup_csrf:
            headers["X-CSRF-Token"] = self.csrf or self.cleanup_csrf
        if byte_range:
            headers["Range"] = byte_range
        payload = None if body is None else canonical(body)
        if payload is not None:
            check(len(payload) <= 16384, "A request body exceeds its strict native bound")
            headers["Content-Type"] = "application/json"
        if signing_in:
            self.login_attempted = True
        if retiring:
            self.logout_attempted = True
        if method == "POST" and path == BACKUPS:
            self.create_attempted = True
        receipt = {"method": method, "route": "attachment" if path.endswith("/file") else "session" if path == SESSION else
                   "status" if path == STATUS else "operation" if path.startswith(OPERATIONS) else "backup",
                   "status": None, "bytes": 0, "complete": False, "range": bool(byte_range)}
        self.calls.append(receipt)
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=timeout(15))
        sink = None
        content, response_headers = b"", {}
        digest, count = hashlib.sha256(), 0
        try:
            if output is not None:
                check(output.parent == self.secret_directory and output.name == self.backup_id + ".age", "Attachment output escaped its dedicated secret directory")
                sink = os.fdopen(os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb")
                os.fchown(sink.fileno(), 995, self.secret_group)
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            receipt["status"] = response.status
            pairs = response.getheaders()
            # A parseable new cookie must become cleanup responsibility before
            # unrelated representation-header or response-body checks can fail.
            if signing_in:
                cookies = SimpleCookie()
                cookies.load(response.getheader("Set-Cookie", ""))
                if "goby_session" in cookies and TOKEN.fullmatch(cookies["goby_session"].value):
                    token = cookies["goby_session"].value
                    check(not any(row["token_hash"] == "\\x" + sha(token.encode()) for row in records(self.before, "sessions")),
                          "Login returned a preexisting credential")
                    self.cookie = token
                    self.cleanup_csrf = sha(("goby:admin:csrf:" + token).encode())
            for name in ("content-type", "content-length", "content-disposition", "content-range", "cache-control", "etag", "accept-ranges"):
                values = [value for key, value in pairs if key.lower() == name]
                check(len(values) <= 1, "A representation header was repeated")
                if values:
                    response_headers[name] = values[0]
            json_budget = MAX_JSON_TOTAL if self.finished else MAX_JSON_TOTAL - 2 * MAX_BODY
            maximum = min(limit, json_budget - self.json_bytes) if sink is None else limit
            check(maximum > 0, "The response byte budget is exhausted")
            while True:
                block = response.read1(min(65536, maximum - count + 1))
                if not block:
                    break
                count += len(block)
                check(count <= maximum, "A response exceeds its bounded complete capture")
                digest.update(block)
                if sink is not None:
                    sink.write(block)
                else:
                    content += block
            complete = response.isclosed() or response.length == 0
            if method != "HEAD" and response.status not in {204, 304} and "content-length" in response_headers:
                length = response_headers["content-length"]
                complete = complete and length.isdigit() and int(length) == count
            receipt.update({"bytes": count, "sha256": digest.hexdigest(), "complete": complete})
            if sink is not None:
                sink.flush()
                os.fsync(sink.fileno())
            else:
                self.json_bytes += count
            check(complete, "A response did not reach complete HTTP EOF")
            if path == STATUS and self.finished and response.status == 401:
                self.denied = True
            if method != "HEAD" and sink is None and content and not byte_range:
                value = json.loads(content)
            else:
                value = content if sink is None else None
            if path == SESSION and isinstance(value, dict) and self.cookie and HASH.fullmatch(value.get("CSRFToken", "")):
                check(value["CSRFToken"] == self.cleanup_csrf, "Native CSRF is not bound to the acknowledged cookie")
                self.csrf = value["CSRFToken"]
            if method == "POST" and path == BACKUPS and isinstance(value, dict) and isinstance(value.get("Operation"), dict):
                self.acknowledge_operation(value["Operation"])
            check(response.status == expected, "A bounded workflow request returned an unexpected status")
            check(method != "HEAD" and response.status != 204 or count == 0, "A bodyless response contained bytes")
            check("no-store" in response_headers.get("cache-control", ""), "A native response lacks its no-store contract")
            if path != SESSION and sink is None:
                secret_absence(content, self.secrets())
            if path.endswith("/file"):
                self.download_grants += 1
            return value, response_headers, receipt
        finally:
            if sink is not None:
                sink.close()
            connection.close()

    def acknowledge_operation(self, value):
        check(value.get("RequestId") == self.request_id and value.get("Kind") == "create" and ID.fullmatch(value.get("Id", "")) and
              ID.fullmatch(value.get("BackupId", "")), "The admitted operation does not identify this exact request")
        check(not self.operation_id or self.operation_id == value["Id"], "The acknowledged operation identity changed")
        self.operation_id, self.backup_id = value["Id"], value["BackupId"]


def validate_status(value):
    check(isinstance(value, dict) and set(value) == STATUS_FIELDS and value["Available"] is True and value["RestoreAvailable"] is True and
          value["UnavailableReason"] == value["RestoreUnavailableReason"] == "" and value["Busy"] is False and value["ActiveOperationId"] == "",
          "The accepted deployment is not both backup-available and restore-available while idle")
    check(re.fullmatch(r"0|[1-9][0-9]*", value["GenerationRevision"]), "Generation revision is not a canonical decimal string")


def inventory(api, route):
    value, _, _ = api.request("GET", route)
    check(isinstance(value, dict) and set(value) == {"Items", "TotalRecordCount", "StartIndex", "Limit"} and
          isinstance(value["Items"], list) and type(value["TotalRecordCount"]) is int and
          value["TotalRecordCount"] == len(value["Items"]) <= 25 and value["StartIndex"] == 0 and value["Limit"] == 25,
          "The default object or operation page is not a complete bounded inventory")
    result = {row["Id"]: row for row in value["Items"] if isinstance(row, dict) and ID.fullmatch(row.get("Id", ""))}
    check(len(result) == len(value["Items"]), "An object or operation inventory has invalid or duplicate identities")
    return result


def bind_session(api, current):
    old_rows_exact(api.before, current)
    added = added_rows(api.before, current, "sessions")
    check(len(added) == 1 and ID.fullmatch(added[0].get("id", "")) and added[0].get("kind") == "admin" and
          added[0].get("user_id") == api.administrator["id"] and added[0].get("token_hash") == "\\x" + sha(api.cookie.encode()),
          "The native cookie does not bind exactly one newly created administrator session")
    api.session_id = added[0]["id"]
    return added[0]


def validate_attachment(api, headers, receipt, backup, *, byte_range=False, head=False):
    size = int(backup["SizeBytes"])
    disposition = Message()
    disposition["Content-Disposition"] = headers.get("content-disposition", "")
    check(headers.get("content-type") == "application/octet-stream" and disposition.get_content_disposition() == "attachment" and
          disposition.get_filename() == api.backup_id + ".age" and headers.get("etag") == '"' + backup["SHA256"] + '"' and
          headers.get("accept-ranges") == "bytes", "The attachment headers differ from the acknowledged immutable backup")
    expected_bytes = 64 if byte_range else size
    check(headers.get("content-length") == str(expected_bytes) and receipt["bytes"] == (0 if head else expected_bytes),
          "Attachment length differs from the complete expected representation")
    if byte_range:
        check(headers.get("content-range") == "bytes 0-63/" + str(size), "The bounded range response selected different bytes")
    elif not head:
        check(receipt["sha256"] == backup["SHA256"], "The complete attachment digest differs from its object metadata")


def verify_activity(api, before, after, *, successful):
    added = added_rows(before, after, "activity_entries")
    expected = []
    def fact(action, resource, resource_id, *, system=False, state="", count=0):
        return {"action": action, "severity": "Info", "source": "system" if system else "native", "actor_kind": "system" if system else "user",
            "actor_id": "" if system else api.administrator["id"], "actor_credential_id": "" if system else api.session_id,
            "resource_kind": resource, "resource_id": resource_id, "request_id": "", "revision": 0, "affected_count": count,
            "state": state, "changed_fields": []}
    expected.extend([fact("session.login", "session", api.session_id, count=1), fact("session.revoked", "session", api.session_id, count=1)])
    if successful:
        expected.extend([fact("backup.requested", "backup", api.operation_id),
                         fact("backup.finished", "backup", api.operation_id, system=True, state="completed")])
        expected.extend(fact("backup.downloaded", "backup", api.backup_id) for _ in range(3))
        check(len(added) == 7 and api.download_grants == 3, "New activity exceeds the exact successful workflow facts")
    for row in added:
        check(set(row) == ACTIVITY_COLUMNS and type(row["id"]) is int and row["id"] > 0, "An activity row has unexpected fields")
        fields = {key: value for key, value in row.items() if key not in {"id", "created_at"}}
        if successful:
            check(fields in expected, "A new activity has an unrelated actor, resource or state")
            expected.remove(fields)
        else:
            check(row["action"] in {"session.login", "session.revoked", "backup.requested", "backup.finished", "backup.downloaded"} and
                  row["resource_id"] in {api.session_id, api.operation_id, api.backup_id}, "Failure-side history escaped its acknowledged workflow")
    secret_absence("\n".join(after["activity_entries"]).encode(), api.secrets())
    return {"new_records": len(added), "actions": dict(Counter(row["action"] for row in added)), "old_records_exact": True,
            "successful_workflow_facts_exact": successful}


def create_secret_directory(run, request_id, passphrase, group):
    parent = PRIVATE_PARENT.lstat()
    check(PRIVATE_PARENT.resolve(strict=True) == PRIVATE_PARENT and stat.S_ISDIR(parent.st_mode) and parent.st_uid in {0, 995} and
          parent.st_mode & 0o022 == 0, "The independent operator-secret parent is unsafe")
    if not SECRET_ROOT.exists():
        SECRET_ROOT.mkdir(mode=0o700)
        os.chown(SECRET_ROOT, 995, group)
        write_exclusive(SECRET_ROOT / ".goby-managed", SECRET_OWNER, owner=995, group=group)
    directory(SECRET_ROOT, 995, 0o700)
    check(read_file(SECRET_ROOT / ".goby-managed", 128, 995, 0o600)[0] == SECRET_OWNER, "The independent operator-secret root is not owned")
    target = SECRET_ROOT / ("attempt-" + str(run).zfill(2))
    check(not target.exists() and not target.is_symlink() and not any(root == target or root in target.parents for root in STRICT_DIRECTORIES),
          "The operator-secret attempt already exists or overlaps a strict recovery store")
    target.mkdir(mode=0o700)
    os.chown(target, 995, group)
    write_exclusive(target / ".goby-managed", (OWNER + "\n").encode(), owner=995, group=group)
    path = target / (request_id + ".passphrase")
    write_exclusive(path, passphrase.encode(), owner=995, group=group)
    check(read_file(path, 1024, 995, 0o600)[0] == passphrase.encode(), "The independently retained passphrase failed its durable readback")
    write_json(target / "request.json", {"owner": OWNER, "request_id": request_id, "passphrase_file": path.name,
               "admission_outcome": "Not yet known; retain this file even if the initiating request fails"}, owner=995, group=group)
    return target, path


def workflow(api, output, passphrase_path, source_before):
    signed, _, _ = api.request("POST", SESSION, body={"Name": api.administrator["name"], "Password": api.password})
    check(api.cookie and api.csrf and isinstance(signed, dict) and signed.get("User", {}).get("Id") == api.administrator["id"],
          "The native login did not establish the existing administrator")
    bind_session(api, api.database.snapshot())
    status, _, _ = api.request("GET", STATUS)
    validate_status(status)
    api.initial_status = status
    old_backups, old_operations = inventory(api, BACKUPS), inventory(api, OPERATIONS)
    check(len(old_backups) < 25 and len(old_operations) < 25, "The complete final inventory has no reserved slot for one new backup/job")
    check(not any(row.get("RequestId") == api.request_id for row in old_operations.values()), "The new request ID already has operation history")
    created, _, _ = api.request("POST", BACKUPS, body={"RequestId": api.request_id, "Passphrase": api.passphrase}, expected=202)
    operation = created["Operation"]
    for poll in range(MAX_POLLS + 1):
        check(set(operation) == OPERATION_FIELDS and operation["RequestId"] == api.request_id and operation["Id"] == api.operation_id and
              operation["BackupId"] == api.backup_id and operation["Kind"] == "create", "The owned operation DTO or identity changed")
        allowed_states = {"pending", "running", "ready", "applying", "completed", "failed", "cancelled", "interrupted"}
        allowed_phases = {"admission", "upload", "snapshot", "encryption", "publication", "validation", "staging", "ready", "activation", "rollback", "cleanup", "finished"}
        allowed_errors = {"", "invalid_archive", "capacity_exceeded", "target_not_ready", "source_changed", "authority_changed", "audit_unavailable",
                          "operation_cancelled", "operation_interrupted", "activation_failed", "storage_unavailable", "database_unavailable", "tools_unavailable"}
        api.worker_observation = {"State": operation["State"] if operation["State"] in allowed_states else "unrecognized",
            "Phase": operation["Phase"] if operation["Phase"] in allowed_phases else "unrecognized",
            "ErrorCode": operation["ErrorCode"] if operation["ErrorCode"] in allowed_errors else "unrecognized"}
        if operation["State"] == "completed":
            check(operation["Phase"] == "finished" and operation["ErrorCode"] == "", "The owned operation did not finish successfully")
            break
        check(operation["State"] in {"pending", "running"} and operation["ErrorCode"] == "" and poll < MAX_POLLS,
              "The backup worker failed, was interrupted or exceeded its poll budget")
        time.sleep(min(2, timeout(2)))
        value, _, _ = api.request("GET", OPERATIONS + "/" + api.operation_id)
        operation = value["Operation"]
    result, _, _ = api.request("GET", BACKUPS + "/" + api.backup_id)
    backup = result.get("Backup")
    check(isinstance(backup, dict) and set(backup) == BACKUP_FIELDS and backup["Id"] == api.backup_id and backup["Kind"] == "generated" and
          backup["State"] == "ready" and backup["Verified"] is True and backup["ErrorCode"] == "" and
          re.fullmatch(r"[1-9][0-9]*", backup["SizeBytes"]) and 64 <= int(backup["SizeBytes"]) <= MAX_DOWNLOAD and HASH.fullmatch(backup["SHA256"]),
          "The generated backup is not a bounded, ready, verified object")
    source = backup["Source"]
    check(isinstance(source, dict) and source.get("SchemaVersion") == "23" and isinstance(source.get("Tables"), list) and
          {row.get("Name") for row in source["Tables"]} == TABLES and len(source["Tables"]) == 29,
          "The generated archive metadata does not describe the complete current schema")
    route = BACKUPS + "/" + api.backup_id + "/file"
    _, headers, receipt = api.request("HEAD", route)
    validate_attachment(api, headers, receipt, backup, head=True)
    prefix, headers, receipt = api.request("GET", route, expected=206, byte_range="bytes=0-63", limit=64)
    validate_attachment(api, headers, receipt, backup, byte_range=True)
    stanza = prefix.splitlines()[1].split() if len(prefix.splitlines()) >= 2 else []
    check(prefix.startswith(b"age-encryption.org/v1\n-> scrypt ") and len(stanza) == 4 and stanza[3] == b"18",
          "The generated attachment does not have the expected production age/scrypt header")
    archive = api.secret_directory / (api.backup_id + ".age")
    _, headers, receipt = api.request("GET", route, output=archive, limit=int(backup["SizeBytes"]))
    validate_attachment(api, headers, receipt, backup)
    proof = file_digest(archive, MAX_DOWNLOAD, 995)
    check(proof["sha256"] == backup["SHA256"] and proof["identity"][2] == int(backup["SizeBytes"]), "The retained encrypted file differs after fsync")
    with archive.open("rb") as stream:
        check(stream.read(64) == prefix, "The separately downloaded byte range differs from the complete archive")
    pairing = {"owner": OWNER, "request_id": api.request_id, "operation_id": api.operation_id, "backup_id": api.backup_id,
        "encrypted_file": archive.name, "passphrase_file": passphrase_path.name, "encrypted_bytes": int(backup["SizeBytes"]),
        "encrypted_sha256": backup["SHA256"], "schema_version": 23, "verified_generated_backup": True}
    write_json(api.secret_directory / "backup.json", pairing, owner=995, group=api.secret_group)
    write_json(output / "backup.json", {**pairing, "operator_directory": str(api.secret_directory)})
    final_status, _, _ = api.request("GET", STATUS)
    validate_status(final_status)
    check(final_status["GenerationRevision"] == status["GenerationRevision"] and final_status["Rollback"] == status["Rollback"] and
          final_status["Storage"]["Objects"] == status["Storage"]["Objects"] + 1 and
          int(final_status["Storage"]["Bytes"]) == int(status["Storage"]["Bytes"]) + int(backup["SizeBytes"]),
          "Creation changed generation/rollback state or unexpected stored objects")
    new_backups, new_operations = inventory(api, BACKUPS), inventory(api, OPERATIONS)
    check(set(new_backups) == set(old_backups) | {api.backup_id} and set(new_operations) == set(old_operations) | {api.operation_id} and
          api.backup_id not in old_backups and api.operation_id not in old_operations and
          all(canonical(new_backups[name]) == canonical(value) for name, value in old_backups.items()) and
          all(canonical(new_operations[name]) == canonical(value) for name, value in old_operations.items()),
          "Creation changed an older backup or operation instead of adding only the new owned pair")
    return {"request_id": api.request_id, "operation_id": api.operation_id, "backup_id": api.backup_id,
            "archive_sha256": proof["sha256"], "archive_bytes": int(backup["SizeBytes"]), "schema_version": 23,
            "head_range_complete_download_verified": True, "retained_operator_directory": str(api.secret_directory),
            "passphrase_retained_separately": True, "generation_and_rollback_unchanged": True,
            "old_backup_and_operation_projections_preserved": True, "age_scrypt_header_verified": True,
            "exact_attachment_decryption_or_restore_executed": False}


def logout(api):
    if not api.cookie:
        return
    api.finished = True
    if not api.logout_attempted:
        try:
            api.request("DELETE", SESSION, expected=204)
        except BaseException:
            pass
    api.request("GET", STATUS, expected=401)
    check(api.denied, "The native cookie has no independent unauthorized barrier")


def main():
    report = {"owner": OWNER, "status": "failed", "cleanup": {"proven": False, "errors": []}, "raw_response_exports": 0,
              "restores": 0, "rollbacks": 0, "deletions": 0, "settings_writes": 0, "media_requests": 0,
              "limits": {"main_seconds": MAIN_SECONDS, "cleanup_seconds": CLEANUP_SECONDS, "json_body_bytes": MAX_BODY,
                         "attachment_bytes": MAX_DOWNLOAD, "http_requests": MAX_HTTP, "polls": MAX_POLLS, "poll_interval_seconds": 2}}
    output = api = database = before = inputs = physical = main_before = reference_before = deployment = None
    successful, stage, credentials = False, "preflight", {}
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and len(sys.argv) == 1,
              "Run only through authorized root SSH on Linux")
        signal.signal(signal.SIGALRM, expired)
        for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(number, expired)
        phase(MAIN_SECONDS)
        run = os.environ.get("GOBY_BACKUP_RECOVERY_VERIFICATION_RUN", "1")
        check(re.fullmatch(r"[1-9][0-9]?", run), "The verification run must be canonical decimal one through ninety-nine")
        os.umask(0o077)
        directory(ROOT)
        work = ROOT / "exec-work-m5j"
        directory(work, 0, 0o700)
        output = work / ("deployed-backup-workflow-" + run.zfill(2))
        check(not output.exists() and not output.is_symlink(), "This deployed workflow attempt already exists")
        output.mkdir(mode=0o700)
        write_json(output / "attempt.json", {"owner": OWNER, "run": int(run), "script_sha256": file_digest(Path(__file__).resolve())["sha256"],
                                             "started_at": datetime.now(timezone.utc).isoformat()})
        deployment, candidate, inputs, main_before, reference_before = accepted_deployment()
        report["deployment"] = {"schema_version": 23, "probe_version": 6, "main_pid": main_before["pid"],
            "uid": main_before["uid"], "start_ticks": main_before["start_ticks"], "binary_sha256": main_before["binary_sha256"],
            "deployment_evidence_sha256": inputs[str(DEPLOYED)]["sha256"], "candidate_manifest_sha256": inputs[str(CANDIDATE)]["sha256"],
            "original_reference_pid": reference_before["pid"], "original_reference_start_ticks": reference_before["start_ticks"]}
        runtime, runtime_proof = private_values(RUNTIME, {"GOBY_LISTEN", "GOBY_PUBLIC_URL", "GOBY_MEDIA_ROOTS", "GOBY_DATABASE_URL"})
        check(runtime["GOBY_LISTEN"] == "127.0.0.1:18096" and runtime["GOBY_PUBLIC_URL"] == ORIGIN, "Runtime listener escaped loopback")
        credentials, browser_proof = private_values(BROWSER, {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"})
        inputs.update({str(RUNTIME): runtime_proof, str(BROWSER): browser_proof, str(RECOVERY_ENV): read_file(RECOVERY_ENV, 65536, mode=0o600)[1]})
        for path in (Path("/etc/systemd/system/goby-foundation-test.service"),
                     *[Path("/etc/systemd/system/goby-foundation-test.service.d") / name for name in
                       ("20-application-keys.conf", "30-observability.conf", "40-backup-recovery.conf")]):
            inputs[str(path)] = file_digest(path, MIB)
        master = PRIVATE_PARENT / "application-key-vault/master.key"
        inputs[str(master)] = read_file(master, 32, 995, 0o600)[1]
        database = Database(runtime["GOBY_DATABASE_URL"])
        before = database.snapshot()
        # Durable safe hashes exist before either authentication or creation.
        write_json(output / "source-before.json", {"owner": OWNER, "schema_version": 23, "tables": snapshot_report(before)})
        report["source_before"] = {"path": str(output / "source-before.json"), "sha256": file_digest(output / "source-before.json")["sha256"],
                                   "tables": 29, "rows": sum(map(len, before.values()))}
        idle(before)
        physical = physical_sources(before, runtime["GOBY_MEDIA_ROOTS"])
        admins = [row for row in records(before, "users") if row.get("name") == credentials["GOBY_SMOKE_NAME"] and
                  row.get("is_administrator") is True and row.get("is_disabled") is False]
        check(len(admins) == 1, "Existing private credentials do not identify one enabled administrator")
        for path in STRICT_DIRECTORIES:
            directory(path, 995, 0o700)
        check(shutil.disk_usage(PRIVATE_PARENT).free >= 512 * MIB, "The private operator file reserve is unavailable")
        request_id, passphrase = secrets.token_hex(16), secrets.token_urlsafe(48)
        api = API(before, admins[0], credentials["GOBY_SMOKE_PASSWORD"], request_id, passphrase)
        api.database, api.secret_group = database, PRIVATE_PARENT.stat().st_gid
        def same_main():
            current = service_identity(SERVICE, main_before["pid"], 995)
            check(current == {name: value for name, value in main_before.items() if name != "binary_sha256"},
                  "The accepted service authority changed before an HTTP request")
        api.service_check = same_main
        api.secret_directory, passphrase_path = create_secret_directory(int(run), request_id, passphrase, api.secret_group)
        report["request_id"] = request_id
        report["retained_operator_directory"] = str(api.secret_directory)
        report["passphrase_retained_before_admission"] = True
        write_json(output / "creation-intent.json", {"owner": OWNER, "request_id": request_id, "passphrase_file": str(passphrase_path),
            "native_logins": 1, "create_requests": 1, "restore_requests": 0, "delete_requests": 0})
        stage = "one native login and backup creation/download"
        report["backup"] = workflow(api, output, passphrase_path, before)
        successful = True
    except BaseException:
        report["failure_stage"] = stage
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        credentials.clear()
        if api is not None and api.cookie:
            safe_step(25, lambda: logout(api), report["cleanup"]["errors"], "Native logout or independent HTTP 401 failed")
        def final_proof():
            check(database is not None and before is not None and inputs is not None, "The source-before proof is unavailable")
            after = database.snapshot()
            if output is not None:
                write_json(output / "source-after.json", {"owner": OWNER, "schema_version": 23, "tables": snapshot_report(after)})
                report["source_after"] = {"path": str(output / "source-after.json"), "sha256": file_digest(output / "source-after.json")["sha256"],
                                          "tables": 29, "rows": sum(map(len, after.values()))}
            old_rows_exact(before, after)
            report["old_29_table_rows_preserved"] = True
            if api is not None and api.cookie:
                row = bind_session(api, after)
                check(row.get("revoked_at") is not None and api.denied, "The sole native session lacks committed SQL revocation and HTTP 401")
                api.barrier = True
                report["activity"] = verify_activity(api, before, after, successful=successful)
                report["new_native_sessions"] = 1
            else:
                check(before == after, "An unacknowledged attempt changed database state")
            for name, proof in inputs.items():
                expected_owner = proof["identity"][6]
                check(file_digest(Path(name), 256 * MIB, expected_owner) == proof, "An accepted input or master changed")
            if physical is not None:
                check(all(file_digest(Path(name), 128 * MIB, proof["identity"][6]) == proof for name, proof in physical.items()),
                      "An original media or sidecar changed")
            check(service_identity(SERVICE, main_before["pid"], 995, main_before["binary_sha256"]) == main_before and
                  service_identity(REFERENCE_SERVICE, REFERENCE_PID, 0) == reference_before, "A protected main or reference process changed")
            report["protected_inputs_media_and_services_preserved"] = True
            report["cleanup"]["proven"] = api is None or not api.cookie or api.barrier
        if database is not None and before is not None:
            safe_step(35, final_proof, report["cleanup"]["errors"], "Final read-only preservation or credential proof failed")
        report["elapsed_seconds"] = round(time.monotonic() - STARTED, 3)
        report["database_queries"] = database.queries if database is not None else 0
        if api is not None:
            report["http_evidence"] = api.calls
            report["login_acknowledged"] = bool(api.cookie)
            report["create_request_attempted"] = api.create_attempted
            report["operation_id"] = api.operation_id
            report["backup_id"] = api.backup_id
            report["last_observed_worker"] = api.worker_observation
            report["http_401_and_sql_revocation_proven"] = api.barrier
            report["admission_uncertainty_requires_operator_review"] = api.create_attempted and not successful
        report["status"] = "passed" if successful and report["cleanup"]["proven"] and not report["cleanup"]["errors"] else "failed"
        encoded = canonical(report) + b"\n"
        if api is not None:
            secret_absence(encoded, api.secrets())
        if output is not None:
            try:
                write_exclusive(output / "report.json", encoded)
            except BaseException:
                print(json.dumps({"status": "failed", "report_persisted": False, "owned_files_retained": True}))
                raise SystemExit(1)
        print(json.dumps({"status": report["status"], "report": str(output / "report.json") if output else None,
                          "owned_files_retained": True, "backup_retained": successful, "cleanup": report["cleanup"]["proven"]}))
    if report["status"] != "passed":
        raise SystemExit(1)


if __name__ == "__main__":
    main()
