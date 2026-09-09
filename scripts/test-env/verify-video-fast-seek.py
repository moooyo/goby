#!/usr/bin/env python3
"""Verify a deployed ProbeVersion 5-to-6 upgrade and private fast video seeking.

Run only through ssh test-env after the operator confirms the M4f deployment.
GOBY_FAST_SEEK_EXPECTED_BINARY_SHA256 must identify that accepted executable.
No user, library, source, configuration, or historical verifier is modified.
Normal scans and this run's authentication/playback/encoding calls are the only
application writes. Existing eligible playback expirations are narrowly audited;
old playback/reference deletion is forbidden. Complete SQL rows and private seek
evidence stay in memory. Only aggregates and non-secret measurements are saved.

The existing five-library fixture lock excludes cooperating media verifiers.
Other catalog/user-state writers must remain idle until cleanup completes.
This script has one exclusive attempt record and never retries an HTTP write or
overwrites a previous attempt/result. It does not run Go commands or deploy code.
"""

from __future__ import annotations

from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timedelta, timezone
from decimal import Decimal
from fractions import Fraction
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import threading
import time
from urllib.parse import parse_qs, quote, urlencode, urlsplit, urlunsplit


sys.dont_write_bytecode = True
RESULT = Path("/opt/goby-test/m4f-deployed-video-fast-seek.json")
ATTEMPT = Path("/opt/goby-test/m4f-video-fast-seek.attempt.json")
OWNER = "goby-video-fast-seek-m4f-verification-v1"
SERVICE = "goby-foundation-test.service"
DEVICE = "goby-video-fast-seek-m4f"
EXPECTED_UID = 995
MAX_REPORT = 128 * 1024
MAX_SNAPSHOT = 8 * 1024 * 1024
MAX_ROWS = 20000
MAX_ITEMS = 256
SEEK_TICKS = 63_700_000
PREFLIGHT_GUARD_SECONDS = 120
TICKS = 10_000_000
ID = re.compile(r"[0-9a-f]{32}\Z")
HASH = re.compile(r"[0-9a-f]{64}\Z")
TABLE = re.compile(r"[a-z_][a-z0-9_]*\Z")
PRIVATE_KEYS = {
    "videoseekindexes", "videoseekcandidate", "videoseekverification", "inputseekticks",
    "requestedstartticks", "sourceidentity", "toolidentity", "parametersetssha256",
    "codedsha256", "decodedsha256", "packetsidedatachecked", "nalscopechecked",
}


class Failure(Exception):
    """Carry a fixed label without raw SQL, HTTP, paths, or credentials."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(",", ":"), allow_nan=False).encode("ascii")


def aggregate(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def timestamp(value):
    check(isinstance(value, str) and len(value) <= 64, "A database clock has an unexpected shape")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    check(parsed.tzinfo is not None, "A database clock omitted its time zone")
    return parsed.astimezone(timezone.utc)


def load_video_helper():
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        specification = importlib.util.spec_from_file_location(
            "goby_fast_seek_video_support", Path(__file__).with_name("verify-progressive-video.py"))
        check(specification is not None and specification.loader is not None, "The progressive-video helper is unavailable")
        module = importlib.util.module_from_spec(specification)
        specification.loader.exec_module(module)
    return module


def private_write(path, value):
    encoded = json.dumps(value, sort_keys=True, ensure_ascii=True, indent=2, allow_nan=False).encode("ascii") + b"\n"
    check(len(encoded) <= MAX_REPORT, "The sanitized report exceeds its byte bound")
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as output:
        output.write(encoded)
        output.flush()
        os.fsync(output.fileno())
    info = path.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
          "The report ownership or permissions changed")


def deployment():
    accepted = os.environ.get("GOBY_FAST_SEEK_EXPECTED_BINARY_SHA256", "")
    check(HASH.fullmatch(accepted) is not None, "Set the accepted M4f binary digest before running this verifier")
    result = subprocess.run(["systemctl", "show", SERVICE, "-p", "MainPID", "-p", "User", "-p", "ActiveState"],
                            capture_output=True, text=True, timeout=5)
    check(result.returncode == 0 and len(result.stdout) < 1024, "The deployed service identity is unavailable")
    values = dict(line.split("=", 1) for line in result.stdout.splitlines() if "=" in line)
    check(values.get("User") == "goby" and values.get("ActiveState") == "active", "The accepted non-root service is not active")
    pid = int(values.get("MainPID", "0"))
    check(pid > 1, "The accepted service has no live main process")
    status = Path(f"/proc/{pid}/status").read_text(encoding="utf-8")
    credentials = next(line for line in status.splitlines() if line.startswith("Uid:"))
    check([int(value) for value in credentials.split()[1:]] == [EXPECTED_UID] * 4,
          "The deployed process has unexpected user credentials")
    digest = hashlib.sha256()
    with Path(f"/proc/{pid}/exe").open("rb") as executable:
        for block in iter(lambda: executable.read(1024 * 1024), b""):
            digest.update(block)
    check(digest.hexdigest() == accepted, "The deployed executable differs from the accepted M4f binary")
    return {"main_pid": pid, "uid": EXPECTED_UID, "binary_sha256": accepted, "schema_version": 14}


def reserve_attempt():
    check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
          "Run only as root through SSH after the accepted M4f deployment")
    check(not sys.argv[1:], "This verifier accepts no alternate target or command-line credential")
    os.umask(0o077)
    info = RESULT.parent.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and
          RESULT.parent.resolve(strict=True) == RESULT.parent, "The fixed report directory is not privately owned")
    check(not RESULT.exists() and not RESULT.is_symlink() and not ATTEMPT.exists() and not ATTEMPT.is_symlink(),
          "A prior fast-seek verification attempt exists; no retry or overwrite was performed")
    private_write(ATTEMPT, {"owner": OWNER, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                            "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
    return info.st_dev, info.st_ino


def database_type(video):
    class Database(video.Database):
        def read(self, query, label):
            # Credentials remain in the protected helper's child environment;
            # SQL and selectors use stdin, never argv or a persistent SQL file.
            result = subprocess.run(["psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"],
                                    input=query.encode("utf-8"), env=self.environment, capture_output=True, timeout=15)
            check(result.returncode == 0 and len(result.stdout) <= MAX_SNAPSHOT and len(result.stderr) <= 65536,
                  label + ": bounded read-only database observation failed")
            return json.loads(result.stdout)
    return Database


def full_snapshot(db):
    names = db.read("SELECT COALESCE(json_agg(tablename ORDER BY tablename),'[]'::json) FROM pg_tables WHERE schemaname='public';",
                    "Complete public table inventory")
    check(isinstance(names, list) and 0 < len(names) <= 48 and all(isinstance(name, str) and TABLE.fullmatch(name) for name in names),
          "The public table inventory is outside the fixed bounded schema")
    counts = db.read("SELECT json_build_object(" + ",".join(
        "'" + name + "',(SELECT count(*) FROM public.\"" + name + "\")" for name in names) + ");", "Complete table row bounds")
    check(set(counts) == set(names) and all(type(count) is int and 0 <= count <= MAX_ROWS for count in counts.values()) and
          sum(counts.values()) <= MAX_ROWS, "The complete table snapshot exceeds its row budget")

    def encoded_rows(expression, table):
        return "COALESCE((SELECT json_agg(row_text ORDER BY row_text) FROM (SELECT (" + expression + \
               ')::text AS row_text FROM public."' + table + '" t) snapshot_rows),\'[]\'::json)'

    entries = ["'" + name + "'," + encoded_rows("to_jsonb(t)", name) for name in names]
    item_expression = "(to_jsonb(t) - 'updated_at' - 'probed_at') || jsonb_build_object('media', " \
                      "CASE WHEN t.media IS NULL THEN 'null'::jsonb ELSE t.media - 'ProbeVersion' - 'VideoSeekIndexes' END)"
    query = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT json_build_object('tables',json_build_object(" + ",".join(entries) + \
            "),'item_semantics'," + encoded_rows(item_expression, "items") + \
            ",'metadata_semantics'," + encoded_rows("to_jsonb(t)", "item_metadata_state") + \
            ",'library_semantics'," + encoded_rows("to_jsonb(t) - 'last_scan_at'", "libraries") + "); COMMIT;"
    snapshot = db.read(query, "Complete in-memory business snapshot")
    check(isinstance(snapshot, dict) and set(snapshot) == {"tables", "item_semantics", "metadata_semantics", "library_semantics"} and
          set(snapshot["tables"]) == set(names), "A complete business snapshot omitted a table or semantic projection")
    for name, rows in snapshot["tables"].items():
        check(isinstance(rows, list) and len(rows) == counts[name] and all(isinstance(row, str) for row in rows),
              "A complete table changed population during the bounded snapshot")
        rows.sort()
    for key in ("item_semantics", "metadata_semantics", "library_semantics"):
        check(isinstance(snapshot[key], list) and all(isinstance(row, str) for row in snapshot[key]),
              "A semantic projection has an unexpected shape")
        snapshot[key].sort()
    return snapshot


def rows(snapshot, name):
    check(name in snapshot["tables"], "A required deployed table is absent")
    return [json.loads(value, parse_float=Decimal) for value in snapshot["tables"][name]]


def id_rows(snapshot, name):
    values = rows(snapshot, name)
    check(all(isinstance(value, dict) and isinstance(value.get("id"), str) for value in values), "A business row has no stable identity")
    result = {value["id"]: (value, raw) for value, raw in zip(values, snapshot["tables"][name])}
    check(len(result) == len(values), "A business table contains duplicate identities")
    return result


def snapshot_summary(snapshot):
    return {"tables": {name: {"count": len(values), "sha256": aggregate(values)}
                       for name, values in sorted(snapshot["tables"].items())},
            "item_semantics_sha256": aggregate(snapshot["item_semantics"]),
            "metadata_semantics_sha256": aggregate(snapshot["metadata_semantics"]),
            "library_semantics_sha256": aggregate(snapshot["library_semantics"])}


def catalog_stable(snapshot):
    ignored = {"items", "item_metadata_state", "libraries", "sessions", "play_sessions", "encoding_jobs", "scan_jobs"}
    return {"tables": {key: value for key, value in snapshot["tables"].items() if key not in ignored},
            "items": snapshot["item_semantics"], "metadata": snapshot["metadata_semantics"], "libraries": snapshot["library_semantics"]}


def upgrade_type(prior):
    class ProbeSixUpgrade(prior.UpgradeVerification):
        def __init__(self, *arguments):
            super().__init__(*arguments)
            self.index_baseline = None

        def _database_snapshot(self):
            complete = full_snapshot(self.db)
            catalog, states = rows(complete, "items"), rows(complete, "item_metadata_state")
            identities = {value["id"] for value in self.libraries.values()}
            self.check(len(identities) == 5 and len(catalog) == 21 and len(states) == 21 and
                       {value["library_id"] for value in catalog} == identities and len(rows(complete, "libraries")) == 5,
                       "The deployed catalog is not the existing five libraries and twenty-one metadata states")
            self.check({value["item_id"] for value in states} == {value["id"] for value in catalog} and
                       all(type(value["revision"]) is int and value["revision"] > 0 and
                           isinstance(value["overrides"], dict) and isinstance(value["locked_values"], dict) for value in states),
                       "The metadata state identity, revision, or saved controls are invalid")
            media = [{"id": value["id"], "library_id": value["library_id"], "path": value["path"],
                      "type": value["type"], "probe_version": value["media"].get("ProbeVersion")}
                     for value in catalog if value["media"] is not None]
            self.check(len(media) == 11 and all(type(value["probe_version"]) is int and value["probe_version"] in {5, 6} for value in media),
                       "The existing eleven media items have an unexpected pre-upgrade probe version")
            roots = rows(complete, "library_roots")
            self.check(len(roots) == 5, "The fixture catalog no longer has exactly one root per library")
            for key, value in self.libraries.items():
                expected = prior.FIXTURE_ROOT / value["root"]
                matches = [root for root in roots if root["library_id"] == value["id"]]
                self.check(len(matches) == 1 and matches[0]["path"] == str(expected) and
                           matches[0]["allowed_path"] == str(prior.FIXTURE_ROOT) and matches[0]["relative_path"] == value["root"],
                           "A fixture root changed from its exact ownership record")
                population = [item for item in media if item["library_id"] == value["id"]]
                self.check(len(population) == value["media_count"], "A fixture's existing media population changed")
                for item in population:
                    path = Path(item["path"])
                    self.check(path != expected and path.is_relative_to(expected) and path.resolve(strict=True) == path and path.is_file() and
                               item["type"] == ("Audio" if key == "audio" else "Episode" if key == "nextup" else "Movie"),
                               "A fixture media path or type escaped the owned scope")
            data = rows(complete, "user_item_data")
            return {"item_count": len(catalog), "items": complete["item_semantics"], "media": sorted(media, key=lambda value: value["id"]),
                    "user_row_count": len(data), "selected_user_row_count": len(data),
                    "stable": catalog_stable(complete), "user_data": complete["tables"]["user_item_data"]}

        def verify(self):
            self.check(self.prepared and self.finished and not self.pending_jobs and not self.unconfirmed_scans,
                       "Probe-six verification requires all five acknowledged scans to finish")
            after, files = self._preservation_observation()
            self.check(all(value["probe_version"] == 6 for value in after["media"]), "A normal scan did not reach ProbeVersion 6")
            self.verified = True
            return {"status": "passed", "before": self._summary(self.before, self.before_files),
                    "after": self._summary(after, files), "scans": [dict(value) for value in self.scan_results],
                    **self._preservation_summary(), "all_media_probe_version": 6,
                    "baseline_boundary": "After schema-14 deployment, before normal probe-five-to-six scans",
                    "technical_item_exceptions": ["updated_at", "probed_at", "media.ProbeVersion", "media.VideoSeekIndexes"],
                    "technical_metadata_exceptions": [], "metadata_revisions_controls_and_effective_preserved": True}
    return ProbeSixUpgrade


class Preservation:
    def __init__(self, db, baseline, audio, api):
        self.db, self.before, self.audio, self.api = db, baseline, audio, api
        self.auth = {}
        self.expirations = set()
        self.expiration_windows = {}
        self.item_id = self.source_id = ""
        self.prune_counts = {"old_playback_rows": 0, "old_reference_rows": 0}

    def record_login(self, kind):
        current = full_snapshot(self.db)
        old, new = id_rows(self.before, "sessions"), id_rows(current, "sessions")
        created = set(new) - set(old) - set(self.auth)
        check(len(created) == 1 and all(new.get(key, (None, None))[1] == value[1] for key, value in old.items()),
              "Login changed preexisting authentication or created unexpected session identities")
        identifier = next(iter(created))
        check(new[identifier][0]["kind"] == kind and new[identifier][0]["revoked_at"] is None,
              "The new authentication has the wrong kind or revocation state")
        self.auth[identifier] = copy.deepcopy(new[identifier][0])
        check(catalog_stable(current) == catalog_stable(self.before), "Login changed existing catalog, metadata, accounts, or user state")
        return identifier

    def preflight(self):
        # Simulate cleanupPlayback's two ordered 256-row batches and the
        # separate nonce batch. Do not treat every eligible active row as if
        # the first UPDATE had already expired it. Locks are not taken here;
        # rejecting every possible deletion is conservative under SKIP LOCKED.
        current = full_snapshot(self.db)
        now = timestamp(self.db.read("SELECT to_json(clock_timestamp());", "Playback-maintenance clock"))
        horizon = now + timedelta(seconds=PREFLIGHT_GUARD_SECONDS)
        accounts = {row["id"]: row for row in rows(current, "users")}
        owner = accounts.get(self.api.user_id)
        check(owner is not None and owner["is_disabled"] is False and isinstance(owner["policy"], dict),
              "The owned viewer no longer has a valid enabled account")
        policy = owner["policy"]
        check(policy.get("EnableMediaPlayback", True) is True, "The owned viewer lacks current playback permission")
        all_folders = owner["is_administrator"] is True or policy.get("EnableAllFolders", True)
        check(type(all_folders) is bool, "The current library policy cannot be conservatively interpreted")
        folders = policy.get("EnabledFolders", [])
        if folders is None:
            folders = []
        if not all_folders:
            check(isinstance(folders, list) and all(isinstance(value, str) for value in folders),
                  "The current folder allowlist cannot be conservatively interpreted")
        authentication = {row["id"]: row for row in rows(current, "sessions")}
        catalog = {row["id"]: row for row in rows(current, "items")}
        plays = [row for row in rows(current, "play_sessions") if row["user_id"] == self.api.user_id]
        check(self.item_id in catalog and (all_folders or catalog[self.item_id]["library_id"] in folders),
              "The owned viewer lacks access to the selected source")
        own_auth = authentication.get(self.api.session_id)
        check(own_auth is not None and own_auth["user_id"] == self.api.user_id and own_auth["device_id"] == DEVICE and
              own_auth["kind"] == "emby" and own_auth["revoked_at"] is None and timestamp(own_auth["expires_at"]) > horizon,
              "The current owned authentication cannot safely span the next playback request")
        eligible = []
        for play in plays:
            auth = authentication.get(play["auth_session_id"])
            # A clock boundary close to the request could change the ordered
            # batches between observation and commit. Refuse instead of
            # broadening the set of historical mutations we later accept.
            for instant in (timestamp(play["expires_at"]),
                            timestamp(play["expires_at"]) + timedelta(days=7)):
                check(not now <= instant <= horizon, "An old playback expiry or retention boundary is too close to this request")
            if auth is not None:
                check(not now < timestamp(auth["expires_at"]) <= horizon,
                      "An old authentication expiry boundary is too close to this request")
            active_auth = auth is not None and auth["user_id"] == play["user_id"] and auth["device_id"] == play["device_id"] and \
                auth["revoked_at"] is None and timestamp(auth["expires_at"]) > now and \
                (auth["kind"] != "admin" or accounts.get(auth["user_id"], {}).get("is_administrator") is True)
            item = catalog.get(play["item_id"])
            visible = item is not None and (all_folders or item["library_id"] in folders)
            if play["state"] in {"Prepared", "Playing", "Paused"} and (timestamp(play["expires_at"]) <= now or not active_auth or not visible):
                eligible.append(play)
        check(len(eligible) <= 256, "Playback cleanup exceeds one unambiguous expiration batch; no playback call was issued")
        selected = sorted(eligible, key=lambda row: (timestamp(row["expires_at"]), row["id"]))[:256]
        selected_ids = {row["id"] for row in selected}
        terminal = [row for row in plays if row["state"] in {"Stopped", "Expired"} or row["id"] in selected_ids]
        newest = sorted(terminal, key=lambda row: (timestamp(row["created_at"]), row["id"]), reverse=True)
        excess = {row["id"] for row in newest[256:]}
        removable = sorted((row for row in terminal if timestamp(row["expires_at"]) < now - timedelta(days=7) or row["id"] in excess),
                           key=lambda row: (timestamp(row["created_at"]), row["id"]))
        references = []
        for reference in rows(current, "client_playback_references"):
            auth = authentication.get(reference["auth_session_id"])
            if reference["user_id"] != self.api.user_id or auth is None:
                continue
            check(not now < timestamp(auth["expires_at"]) <= horizon,
                  "A nonce authentication expiry boundary is too close to this request")
            if auth["revoked_at"] is not None or timestamp(auth["expires_at"]) <= now:
                references.append(reference)
        references.sort(key=lambda row: (row["auth_session_id"], row["device_id"], row["client_nonce"]))
        old = id_rows(self.before, "play_sessions")
        # Check the complete removable pools, not only their first 256 rows:
        # SKIP LOCKED may reach a later row in either deletion batch.
        self.prune_counts = {"old_playback_rows": sum(row["id"] in old for row in removable),
                             "old_reference_rows": len(references)}
        check(not removable and not references, "Normal playback could delete playback or nonce history; no such call was issued")
        for identifier in selected_ids & set(old):
            self.expirations.add(identifier)
            self.expiration_windows.setdefault(identifier, (now, horizon))

    def audit(self, *, revoked=False):
        current = full_snapshot(self.db)
        check(set(current["tables"]) == set(self.before["tables"]), "The deployed business table inventory changed")
        check(catalog_stable(current) == catalog_stable(self.before), "Catalog metadata, revisions, saved controls, accounts, or user data changed")
        changed = 0
        for name in ("sessions", "play_sessions", "encoding_jobs", "scan_jobs"):
            old, new = id_rows(self.before, name), id_rows(current, name)
            check(set(old) <= set(new), "A preexisting business row was deleted during verification")
            for identifier, (original, raw) in old.items():
                now, current_raw = new[identifier]
                if current_raw == raw:
                    continue
                check(name == "play_sessions" and identifier in self.expirations and original["user_id"] == self.api.user_id and
                      original["state"] in {"Prepared", "Playing", "Paused"} and now["state"] == "Expired",
                      "A preexisting row changed outside its acknowledged playback expiration")
                stable = {key: value for key, value in original.items() if key not in {"state", "stopped_at", "updated_at"}}
                check(stable == {key: value for key, value in now.items() if key not in {"state", "stopped_at", "updated_at"}} and
                      isinstance(now["updated_at"], str) and isinstance(now["stopped_at"], str) and
                      (original["stopped_at"] is None or now["stopped_at"] == original["stopped_at"]),
                      "A permitted old playback expiration changed other semantics")
                earliest, latest = self.expiration_windows[identifier]
                check(earliest <= timestamp(now["updated_at"]) <= latest and
                      (original["stopped_at"] is not None or earliest <= timestamp(now["stopped_at"]) <= latest),
                      "An old playback changed outside its preflighted expiration window")
                changed += 1
            added = set(new) - set(old)
            if name == "sessions":
                check(added == set(self.auth), "An authentication row escaped this run's two known logins")
                for identifier in added:
                    actual, expected = new[identifier][0], self.auth[identifier]
                    check({key: value for key, value in actual.items() if key not in {"last_seen_at", "revoked_at"}} ==
                          {key: value for key, value in expected.items() if key not in {"last_seen_at", "revoked_at"}},
                          "An owned authentication changed its fixed identity")
                    if revoked:
                        check(isinstance(actual.get("revoked_at"), str), "An owned authentication remains unrevoked")
            elif name == "scan_jobs":
                check(added == set(self.api.scan_ids) and all(new[key][0]["library_id"] == self.api.scan_ids[key] for key in added),
                      "Scan-job changes escaped the acknowledged five-library requests")
                if revoked:
                    check(all(new[key][0]["status"] not in {"Queued", "Running"} for key in added),
                          "An acknowledged scan remains active after cleanup")
            else:
                for identifier in added:
                    row = new[identifier][0]
                    check(row["user_id"] == self.api.user_id and row["auth_session_id"] == self.api.session_id and
                          row["device_id"] == DEVICE and row["item_id"] == self.item_id and row["media_source_id"] == self.source_id,
                          "A playback or encoding row escaped this run's canonical source and authentication")
                    if name == "encoding_jobs" and revoked:
                        check(row["state"] not in {"queued", "running"}, "An owned encoder remains active after cleanup")
                    if name == "play_sessions":
                        check(row["state"] == "Prepared" and row["started_at"] is None and row["stopped_at"] is None,
                              "A transport-only request fabricated player activity")
        metadata = rows(current, "item_metadata_state")
        return {"complete_snapshot": snapshot_summary(current), "allowed_old_playback_expirations": len(self.expirations),
                "observed_old_playback_expirations": changed, "forbidden_old_prune_candidates": dict(self.prune_counts),
                "all_other_old_business_rows_preserved": True, "metadata_state_count": len(metadata),
                "metadata_revisions_controls_effective_preserved": True,
                "metadata_revision_one_count": sum(value["revision"] == 1 for value in metadata),
                "metadata_empty_controls_count": sum(value["overrides"] == {} and value["locked_values"] == {} for value in metadata),
                "all_user_data_preserved": True}


def api_type(smoke):
    class API(smoke.API):
        def __init__(self):
            super().__init__()
            self.scan_ids = {}
            self.library_ids = set()
            self.item_id = self.source_id = ""
            self.play_ids = set()
            self.attempted = set()
            self.preflight = None
            self.calls = {"GET": 0, "HEAD": 0, "POST": 0, "DELETE": 0}

        def request(self, method, path, **options):
            parsed = urlsplit(path)
            route = parsed.path
            check(not parsed.scheme and not parsed.netloc and not parsed.fragment, "HTTP requests must remain on the fixed relative Goby routes")
            allowed = False
            scan = re.fullmatch(r"/admin/v1/libraries/([0-9a-f]{32})/scan", route)
            cancel = re.fullmatch(r"/admin/v1/jobs/([0-9a-f]{32})/cancel", route)
            info = re.fullmatch(r"/emby/Items/([0-9a-f]{32})/PlaybackInfo", route)
            stream = re.fullmatch(r"/emby/Videos/([0-9a-f]{32})/stream\.mp4", route)
            detail = re.fullmatch(r"/emby/Users/([0-9a-f]{32})/Items/([0-9a-f]{32})", route)
            query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
            check(all(len(value) == 1 for value in query.values()), "An owned HTTP query contains duplicate selectors")
            if info or stream:
                check(self.item_id and (info or stream)[1] == self.item_id, "A media request escaped its selected owned item")
            if stream:
                check(query.get("api_key") == [self.token] and query.get("DeviceId") == [DEVICE] and
                      query.get("MediaSourceId") == [self.source_id] and
                      (query.get("Static") == ["true"] or query.get("PlaySessionId", [""])[0] in self.play_ids),
                      "A media request escaped its current authentication and canonical playback scope")
            if info:
                body = options.get("body")
                check(isinstance(body, dict) and body.get("UserId") == self.user_id and body.get("MediaSourceId") == self.source_id,
                      "PlaybackInfo escaped its owned user or source")
            if method == "POST":
                allowed = route in {"/admin/v1/session", "/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"} or bool(info) or \
                          bool(scan and scan[1] in self.library_ids) or bool(cancel and cancel[1] in self.scan_ids)
            elif method == "DELETE":
                allowed = route in {"/admin/v1/session", "/emby/Videos/ActiveEncodings"}
            elif method == "GET":
                allowed = route in {"/readyz", "/admin/v1/libraries", "/admin/v1/jobs", "/admin/v1/capabilities"} or bool(stream) or \
                          bool(detail and detail[1] == self.user_id and detail[2] == self.item_id)
            elif method == "HEAD":
                allowed = bool(stream)
            check(allowed and sum(self.calls.values()) < 4000, "The verifier attempted a route or request count outside its bounded task")
            if route == "/emby/Videos/ActiveEncodings":
                check(set(query) == {"DeviceId", "PlaySessionId"} and query["DeviceId"] == [DEVICE] and
                      query["PlaySessionId"][0] in self.play_ids, "Encoding cleanup escaped its owned playback scope")
            if method in {"POST", "DELETE"}:
                key = (method, route, query.get("PlaySessionId", [""])[0])
                check(key not in self.attempted, "An HTTP mutation was already attempted; no write retry was issued")
                self.attempted.add(key)
            if self.preflight is not None and (info or stream and parse_qs(parsed.query).get("Static") != ["true"]):
                self.preflight()
            self.calls[method] += 1
            result = super().request(method, path, **options)
            if info and isinstance(result, dict):
                play = result.get("PlaySessionId", "")
                if isinstance(play, str) and re.fullmatch(r"play_[0-9a-f]{32}", play):
                    self.play_ids.add(play)
            if scan and isinstance(result, dict) and isinstance(result.get("Job"), dict):
                job = result["Job"]
                if isinstance(job.get("Id"), str) and ID.fullmatch(job["Id"]) and job.get("LibraryId") == scan[1]:
                    self.scan_ids[job["Id"]] = scan[1]
            return result
    return API


def no_private_fields(value):
    if isinstance(value, dict):
        check(not any(re.sub(r"[^a-z0-9]", "", key.lower()) in PRIVATE_KEYS for key in value),
              "A public response exposed private video seek evidence")
        for nested in value.values():
            no_private_fields(nested)
    elif isinstance(value, list):
        for nested in value:
            no_private_fields(nested)


def indexed_video(video, api, db, upgrade):
    values = db.read("SELECT COALESCE(json_agg(json_build_object('Id',id,'Path',path,'Media',media)),'[]'::json) "
                     "FROM items WHERE library_id=" + video.audio.sql(upgrade.libraries["hls"]["id"]) + " AND type='Movie';",
                     "Owned probe-six video source")
    check(isinstance(values, list) and len(values) == 1 and values[0].get("Path") == str(video.hls.MEDIA),
          "The HLS fixture no longer identifies its unique owned video source")
    item_id, media = values[0]["Id"], values[0].get("Media") or {}
    check(isinstance(item_id, str) and ID.fullmatch(item_id) and media.get("ProbeVersion") == 6 and
          media.get("FormatStartKnown") is True and type(media.get("FormatStartTicks")) is int and
          abs(media["FormatStartTicks"]) <= 30 * 86400 * TICKS and
          type(media.get("DurationTicks")) is int and abs(media["DurationTicks"] - 15 * TICKS) <= TICKS // 100,
          "The indexed video lacks its bounded probe-six source clock and duration")
    api.item_id, api.source_id = item_id, "mediasource_" + item_id
    item = video.hls.detail(api, item_id)
    no_private_fields(item)
    sources = item.get("MediaSources", [])
    check(len(sources) == 1 and sources[0].get("Id") == api.source_id and sources[0].get("Path") == str(video.hls.MEDIA) and
          sources[0].get("Protocol") == "File" and sources[0].get("Size") == video.hls.MEDIA_SIZE,
          "The original public source differs from its owned indexed file")
    original = video.support.source_projection(sources[0])
    videos = [stream for stream in original["MediaStreams"] if stream.get("Type") == "Video"]
    audios = [stream for stream in original["MediaStreams"] if stream.get("Type") == "Audio"]
    check(len(videos) == len(audios) == 1 and videos[0].get("Codec") == "h264" and audios[0].get("Codec") == "aac" and
          (videos[0].get("Width"), videos[0].get("Height")) == (320, 180),
          "The owned fixture lost its actual H.264 and AAC streams")
    indexes = media.get("VideoSeekIndexes")
    check(isinstance(indexes, list) and len(indexes) == 1 and isinstance(indexes[0], dict),
          "A normal scan did not persist the unique private video restart index")
    index = indexes[0]
    check(index.get("version") == 1 and index.get("stream_index") == videos[0]["Index"] and
          index.get("duration_ticks") == media["DurationTicks"] and index.get("format_start_ticks") == media["FormatStartTicks"] and
          (index.get("width"), index.get("height"), index.get("pixel_format")) == (320, 180, "yuv420p") and
          index.get("packet_side_data_checked") is True and index.get("nal_scope_checked") is True and
          all(isinstance(index.get(key), str) and HASH.fullmatch(index[key]) for key in
              ("source_identity", "tool_identity", "parameter_sets_sha256")),
          "The private scan evidence does not match its source clock, stream, or complete packet scope")
    numerator, denominator = index.get("time_base_numerator"), index.get("time_base_denominator")
    check(type(numerator) is int and type(denominator) is int and numerator > 0 and denominator > 0,
          "The private native time base is invalid")
    points = index.get("entries")
    check(isinstance(points, list) and 1 <= len(points) <= 8192 and all(isinstance(point, dict) and
          type(point.get("pts")) is int and type(point.get("dts")) is int and
          all(isinstance(point.get(key), str) and HASH.fullmatch(point[key]) for key in ("coded_sha256", "decoded_sha256"))
          for point in points), "The private native restart evidence is missing or malformed")
    check(all(left["pts"] < right["pts"] and left["dts"] < right["dts"] for left, right in zip(points, points[1:])),
          "The private restart evidence is not strictly ordered")
    time_base = Fraction(numerator, denominator)
    requested = Fraction(media["FormatStartTicks"] + SEEK_TICKS, TICKS)
    preceding = [position for position, point in enumerate(points) if point["pts"] * time_base <= requested]
    check(preceding, "The requested fast seek has no preceding native restart evidence")
    last = preceding[-1]
    relative = (points[last]["pts"] * time_base * TICKS).__floor__() - media["FormatStartTicks"]
    check(0 < relative <= SEEK_TICKS, "The chosen source cannot demonstrate a positive indexed input seek")
    candidate_index = copy.deepcopy(index)
    candidate_index["entries"] = candidate_index["entries"][max(0, last - 63):last + 1]
    candidate = {"version": 1, "requested_start_ticks": SEEK_TICKS, "input_seek_ticks": relative, "index": candidate_index}
    return original, {"video": videos[0], "audio": audios[0], "media": media}, copy.deepcopy(item["UserData"]), candidate


class ProcessObservation:
    """Sample only accepted-service descendants; retain no raw command lines."""

    def __init__(self, pid):
        self.pid = pid
        self.stop = threading.Event()
        self.ready = threading.Event()
        self.thread = None
        self.seen = set()
        self.records = []
        self.errors = 0
        self.samples = 0

    def __enter__(self):
        self.thread = threading.Thread(target=self._run, name="goby-fast-seek-process-observer", daemon=True)
        self.thread.start()
        check(self.ready.wait(2), "The bounded process observer did not start")
        return self

    def __exit__(self, *_):
        self.stop.set()
        self.thread.join(timeout=2)
        check(not self.thread.is_alive(), "The bounded process observer did not stop")

    def _run(self):
        while not self.stop.is_set():
            try:
                self._sample()
            except (OSError, ValueError, Failure):
                self.errors += 1
            finally:
                self.ready.set()
            self.stop.wait(0.005)

    def _sample(self):
        self.samples += 1
        parents, visited = [self.pid], set()
        while parents:
            parent = parents.pop()
            if parent in visited:
                continue
            visited.add(parent)
            check(len(visited) <= 128, "Service descendant observation exceeded its process bound")
            task = Path(f"/proc/{parent}/task")
            try:
                threads = list(task.iterdir())
            except FileNotFoundError:
                continue
            check(len(threads) <= 512, "Service descendant observation exceeded its thread bound")
            for thread in threads:
                try:
                    child_text = (thread / "children").read_text(encoding="ascii")
                    check(len(child_text) <= 8192, "A service descendant list exceeded its byte bound")
                    children = [int(value) for value in child_text.split()]
                except FileNotFoundError:
                    continue
                for child in children:
                    parents.append(child)
                    self._process(child)

    def _process(self, pid):
        try:
            root = Path(f"/proc/{pid}")
            process_stat = (root / "stat").read_text(encoding="ascii")
            start = process_stat.rsplit(")", 1)[1].split()[19]
            key = (pid, start)
            if key in self.seen:
                return
            check(root.stat().st_uid == EXPECTED_UID, "An observed service child has unexpected credentials")
            with (root / "cmdline").open("rb") as source:
                raw = source.read(65537)
            check(len(raw) <= 65536, "An observed service command exceeded its byte bound")
            arguments = [value.decode("utf-8", "strict") for value in raw.rstrip(b"\0").split(b"\0")]
            if not arguments or not arguments[0]:
                return
            tool = Path(arguments[0]).name
            if tool not in {"ffmpeg", "ffprobe"}:
                return
            self.seen.add(key)
            check(len(self.records) < 128, "Process observations exceeded their bounded population")
            category = "probe" if tool == "ffprobe" else "identity" if "-version" in arguments else \
                "producer" if arguments[-1] == "pipe:4" else \
                "index" if "-skip_frame" in arguments else "proof" if "framehash" in arguments else "other"
            record = {"category": category}
            if category == "producer":
                inputs = [position for position, value in enumerate(arguments) if value == "-i"]
                seeks = [position for position, value in enumerate(arguments) if value == "-ss"]
                pairs = list(zip(arguments, arguments[1:]))
                record.update({"input_count": len(inputs), "seek_count": len(seeks),
                    "inherited_source_inputs": bool(inputs) and all(arguments[position + 1] == "/proc/self/fd/3" for position in inputs),
                    "absolute_input_seek": ("-seek_timestamp", "1") in pairs and "-noaccurate_seek" in arguments,
                    "linear_audio_input": len(inputs) == 2 and ("-discard:v", "all") in pairs and
                        inputs[0] < arguments.index("-discard:v") < inputs[1],
                    "maps": [arguments[position + 1] for position, value in enumerate(arguments[:-1]) if value == "-map"],
                    "video_encoder": next((value for option, value in pairs if option == "-c:v"), ""),
                    "input_seek_seconds": [arguments[position + 1] for position in seeks if inputs and position < inputs[0]],
                    "output_seek_seconds": [arguments[position + 1] for position in seeks if inputs and position > inputs[-1]]})
            self.records.append(record)
        except (FileNotFoundError, ProcessLookupError):
            # Short-lived children can disappear between proc reads. This is
            # a sampling limitation, never evidence that no process ran.
            return

    def summary(self):
        return {"samples": self.samples, "observation_errors": self.errors,
                "observed_process_counts": {kind: sum(record["category"] == kind for record in self.records)
                    for kind in ("probe", "identity", "index", "proof", "producer", "other")},
                "method": "Read-only sampling of accepted service descendants through proc; short-lived children may be missed"}

    def fast_result(self, candidate, streams):
        producers = [record for record in self.records if record["category"] == "producer"]
        result = self.summary()
        result["actual_fast_execution_proven"] = False
        if not producers:
            return {**result, "status": "not_observed", "limitation": "The producer command was not captured; media success and a stored candidate do not prove fast execution"}
        check(len(producers) == 1, "The single GET unexpectedly started multiple observed producers")
        record = producers[0]
        if record["input_count"] == 1 and not record["input_seek_seconds"]:
            return {**result, "status": "linear_fallback_observed", "limitation": "The actual producer retained linear video decoding; this run does not prove fast execution"}
        check(record["input_count"] == 2 and record["seek_count"] == 2 and record["inherited_source_inputs"] and
              record["absolute_input_seek"] and record["linear_audio_input"] and record["video_encoder"] == "libx264" and
              record["maps"] == ["0:" + str(streams["video"]["Index"]), "1:" + str(streams["audio"]["Index"])] and
              len(record["input_seek_seconds"]) == len(record["output_seek_seconds"]) == 1,
              "The observed producer did not use the private fast-video and independent linear-audio contract")
        index = candidate["index"]
        time_base = Fraction(index["time_base_numerator"], index["time_base_denominator"])
        allowed = set()
        for point in reversed(index["entries"][-4:]):
            for key in ("pts", "dts"):
                absolute_ticks = (point[key] * time_base * TICKS).__floor__()
                relative = absolute_ticks - index["format_start_ticks"]
                if 0 < relative <= SEEK_TICKS:
                    allowed.add(Fraction(absolute_ticks, TICKS))
        actual = Fraction(record["input_seek_seconds"][0])
        check(actual in allowed and Fraction(record["output_seek_seconds"][0]) == Fraction(SEEK_TICKS, TICKS),
              "The observed fast input argument is not an exact native indexed proposal or changed the requested output seek")
        return {**result, "status": "observed", "actual_fast_execution_proven": True,
                "relative_input_seek_ticks": int(actual * TICKS) - index["format_start_ticks"],
                "output_seek_ticks": SEEK_TICKS, "native_indexed_argument": True,
                "same_inherited_source_twice": True, "independent_linear_audio": True}


def negotiate(video, api, db, original, streams):
    check(db.source_jobs(api, api.item_id, api.source_id) == [], "The fresh owned source already has an encoding job")
    body = video.request_body(api, original, streams, [video.video_profile("http")], encode=True)
    body["StartTimeTicks"] = SEEK_TICKS
    response = api.request("POST", "/emby/Items/" + quote(api.item_id) + "/PlaybackInfo", emby=True, body=body,
                           label="Private indexed video PlaybackInfo negotiation")
    check(isinstance(response, dict) and not response.get("ErrorCode") and len(response.get("MediaSources", [])) == 1,
          "PlaybackInfo did not negotiate one compatible video source")
    no_private_fields(response)
    play = response.get("PlaySessionId", "")
    check(play in api.play_ids, "PlaybackInfo omitted its canonical playback identity")
    state = db.playback(api, play)
    check(state is not None and state.get("item_id") == api.item_id and state.get("media_source_id") == api.source_id and
          state.get("state") == "Prepared" and state.get("active_jobs") == 0 and
          db.source_jobs(api, api.item_id, api.source_id) == [], "PlaybackInfo started a conversion or lost its canonical scope")
    source = response["MediaSources"][0]
    check(video.support.source_projection(source) == original and source.get("SupportsTranscoding") is True and
          source.get("SupportsDirectPlay") is False and source.get("SupportsDirectStream") is False and
          source.get("TranscodingSubProtocol") == "http" and source.get("TranscodingContainer") == "mp4",
          "PlaybackInfo changed original media facts or selected the wrong output transport")
    target = source.get("TranscodingUrl")
    check(isinstance(target, str) and 0 < len(target) <= 8192, "PlaybackInfo omitted its bounded progressive URL")
    parsed = urlsplit(target)
    check(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path == "/emby/Videos/" + api.item_id + "/stream.mp4",
          "PlaybackInfo returned an unexpected media origin or route")
    query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
    no_private_fields(query)
    check(len(query) <= 64 and all(len(value) == 1 for value in query.values()) and
          query.get("DeviceId") == [DEVICE] and query.get("MediaSourceId") == [api.source_id] and
          query.get("PlaySessionId") == [play] and query.get("api_key") == [api.token] and
          query.get("Static") == ["false"] and query.get("StartTimeTicks") == [str(SEEK_TICKS)] and
          query.get("VideoCodec") == ["h264"] and query.get("AudioCodec") == ["aac"] and
          query.get("AllowVideoStreamCopy") == query.get("AllowAudioStreamCopy") == ["false"],
          "PlaybackInfo did not preserve the requested encoding, seek, and authorization scope")
    hidden = {"sourceformatstartknown", "sourceformatstartticks", "formatstartticks", "formatstartknown",
              "durationticks", "path", "jobid", "hardware", "gobyhlsid"}
    check(not any(key.lower() in hidden for key in query), "The URL exposed a private source clock or producer setting")
    # Public unknown query names are harmless inputs, never authority for the
    # private candidate or its execution proof. Both HEAD and GET use these.
    query.update({"VideoSeekCandidate": ["untrusted-query-candidate"], "VideoSeekVerification": ["untrusted-query-proof"],
                  "InputSeekTicks": ["1"], "SourceFormatStartTicks": ["1"]})
    return urlunsplit(("", "", parsed.path, urlencode(query, doseq=True), "")), play


def main():
    report = {"owner": OWNER, "status": "failed", "cleanup_errors": [], "assertions": [],
              "scope": "One normal scan of each existing owned library and one nonzero progressive-video GET; no new fixture, user, library, player report, or historical row deletion",
              "baseline_boundary": "After accepted schema-14 deployment, before probe-five-to-six scans; schema migration preservation is independently audited"}
    parent_identity = None
    video = api = db = owned = scratch = upgrade = preservation = http = None
    original = user_data = accepted = None
    secrets = []
    cleanup_attempts = set()
    stage = "exclusive first-attempt reservation"
    try:
        parent_identity = reserve_attempt()
        stage = "accepted deployment and protected helper import"
        accepted = deployment()
        video = load_video_helper()
        video.smoke.DEVICE_ID = DEVICE
        api = api_type(video.smoke)()
        db = database_type(video)()
        check(db.environment.get("PGHOST") == "127.0.0.1" and db.environment.get("PGPORT") == "5432" and
              db.environment.get("PGDATABASE") == "goby_test", "The database observation escaped the fixed main test instance")
        schema = db.read("SELECT json_build_object('version',max(version),'count',count(*),"
                         "'readonly',current_setting('transaction_read_only'),'server_version_num',current_setting('server_version_num')) "
                         "FROM schema_migrations;", "Accepted schema and read-only observation mode")
        check(schema.get("version") == schema.get("count") == 14 and schema.get("readonly") == "on",
              "The accepted deployment does not have exactly schema fourteen with read-only SQL observation")
        report["deployment"] = {**accepted, "probe_version": 6, "postgresql_version_num": int(schema["server_version_num"]),
                                "database_port": 5432, "database_read_only": True}
        stage = "existing fixture ownership and complete pre-login snapshot"
        for name in (video.smoke.MARKER_NAME, video.smoke.STATE_NAME, video.smoke.LOCK_NAME):
            video.smoke.private_file(video.smoke.DIRECTORY / name)
        owned = video.smoke.OwnedFixture()
        owned.open()
        record, _, _ = video.support.existing_audio(owned)
        credentials = video.smoke.credentials()
        secrets.extend([credentials["GOBY_SMOKE_PASSWORD"], owned.state["user_password"], db.environment["PGPASSWORD"]])
        baseline = full_snapshot(db)
        check(not any(row["status"] in {"Queued", "Running"} for row in rows(baseline, "scan_jobs")) and
              not any(row["state"] in {"queued", "running"} for row in rows(baseline, "encoding_jobs")),
              "Another scan or conversion is active; no application write was issued")
        media = [row["media"] for row in rows(baseline, "items") if row["media"] is not None]
        check(len(media) == 11 and all(value.get("ProbeVersion") == 5 for value in media),
              "The first-attempt baseline must contain the eleven existing probe-five media items")
        preservation = Preservation(db, baseline, video.audio, api)
        report["complete_snapshot_before"] = snapshot_summary(baseline)
        http = video.hls.HTTP(api)
        api.request("GET", "/readyz", label="Accepted fast-seek deployment readiness")
        stage = "owned administrator and existing viewer authentication"
        api.admin_login(credentials)
        secrets.extend([api.cookie, api.csrf])
        preservation.record_login("admin")
        capabilities = api.request("GET", "/admin/v1/capabilities", admin=True, label="Accepted fast-seek capabilities")
        check(capabilities.get("Features", {}).get("Playback") is True and
              capabilities.get("Features", {}).get("Transcoding") is True and capabilities.get("Toolchain", {}).get("FFmpeg") == "9.0.1",
              "The accepted service lacks playback, transcoding, or the pinned FFmpeg toolchain")
        api.viewer_login(owned.state)
        secrets.append(api.token)
        check(preservation.record_login("emby") == api.session_id, "The viewer response differs from its newly stored authentication")
        scratch = video.audio.Scratch()
        scratch.create()
        stage = "five normal library scans and complete semantic preservation"
        upgrade = upgrade_type(video.prior)(api, db, video.smoke, record)
        report["upgrade_before"] = upgrade.prepare()
        api.library_ids = {value["id"] for value in upgrade.libraries.values()}
        report["upgrade_scans"] = upgrade.run()
        report["upgrade_after"] = upgrade.verify()
        preservation.audit()
        stage = "trusted private index and immutable source facts"
        original, streams, user_data, candidate = indexed_video(video, api, db, upgrade)
        preservation.item_id, preservation.source_id = api.item_id, api.source_id
        check(sum(row["user_id"] == api.user_id and row["item_id"] == api.item_id for row in rows(baseline, "user_item_data")) == 1,
              "The existing viewer lacks source user data; negotiation must not create a row during this preservation run")
        api.preflight = preservation.preflight
        reference = video.pixels(scratch, video.hls.MEDIA)
        check(len(reference) == 360 * video.PIXEL_BYTES, "The immutable source no longer contains its expected decoded frame population")
        report["source"] = {"seconds": 15, "bytes": video.hls.MEDIA_SIZE, "sha256": video.hls.MEDIA_HASH,
                            "video_frames": 360, "private_index_count": 1, "native_restart_entries": len(streams["media"]["VideoSeekIndexes"][0]["entries"]),
                            "candidate_window_entries": len(candidate["index"]["entries"]), "source_format_clock_known": True}
        stage = "PlaybackInfo and HEAD without producer or proof work"
        with ProcessObservation(accepted["main_pid"]) as planning_observation:
            target, play = negotiate(video, api, db, original, streams)
            video.head_without_job(api, db, http, target, play)
        check(db.source_jobs(api, api.item_id, api.source_id) == [] and not planning_observation.records,
              "PlaybackInfo or HEAD created an encoding row or an observed media-tool process")
        report["assertions"].append({"stage": stage, "encoding_jobs_created": 0, "head_body_bytes": 0,
            "public_private_fields_absent": True, **planning_observation.summary(),
            "proof_absence_boundary": "No tool was observed during planning; deterministic no-tool invocation is covered by the separate instrumented server integration test"})
        stage = "one actual GET with private-query injection ignored"
        with ProcessObservation(accepted["main_pid"]) as playback_observation:
            headers, content = http.request("GET", target, "Complete indexed progressive-video media response", headers={"Range": "bytes=0-127"})
        video.video_headers(headers)
        plans = db.completed(api, play, 1)
        plan = plans[0]["plan"]
        encoded = plan.get("VideoSeekCandidate")
        check(isinstance(encoded, str) and 0 < len(encoded) <= 64 * 1024 and json.loads(encoded) == candidate and
              plan.get("StartTicks") == SEEK_TICKS and plan.get("SourceFormatStartKnown") is True and
              plan.get("SourceFormatStartTicks", 0) == streams["media"]["FormatStartTicks"] and
              plan.get("OutputMode") == "progressive" and plan.get("Container") == "mp4" and
              plan.get("VideoCodec") == "h264" and plan.get("AudioCodec") == "aac" and
              plan.get("VideoStreamIndex") == streams["video"]["Index"] and plan.get("AudioStreamIndex") == streams["audio"]["Index"],
              "The persisted plan lost trusted scan evidence or accepted a private query injection")
        playback = video.video_probe(scratch, content, "m4f-fast-seek.mp4", reference,
                                     start_ticks=SEEK_TICKS, width=160, height=90)
        report["media"] = {**playback, "private_query_injection_ignored": True,
                           "stored_candidate_equals_current_scan_selection": True, "range_ignored_status": 200}
        report["fast_execution"] = playback_observation.fast_result(candidate, streams)
        check(not any(record["category"] in {"probe", "index"} for record in playback_observation.records),
              "The media GET repeated a technical source probe or whole-file seek index analysis")
        item = video.hls.detail(api, api.item_id)
        no_private_fields(item)
        check(item.get("UserData") == user_data and video.support.source_projection(item["MediaSources"][0]) == original,
              "Media delivery changed original DTO facts or user playback history")
        report["status"] = "passed" if report["fast_execution"]["actual_fast_execution_proven"] and not planning_observation.errors else "incomplete"
        if report["status"] == "incomplete":
            report["limitation"] = "Functional seek and preservation completed, but this first attempt cannot fully prove the process-observation contract"
    except BaseException as error:
        report["failed_stage"] = stage
        report["error"] = str(error) if isinstance(error, Failure) else "A protected operation failed; raw exception details were suppressed"
    finally:
        if upgrade is not None:
            try:
                report["cleanup_errors"].extend(upgrade.cancel_pending())
            except BaseException:
                report["cleanup_errors"].append("Owned scan cleanup could not be confirmed")
        if api is not None and api.token and db is not None:
            try:
                # Recover a committed canonical scope even when its original
                # HTTP response was lost. Identity comes from the fresh auth,
                # device, item and source, never from an unrelated old row.
                discovered = db.playbacks(api)
                for play in discovered:
                    state = db.playback(api, play)
                    check(state is not None and state.get("item_id") == api.item_id and state.get("media_source_id") == api.source_id,
                          "Owned playback recovery found an unexpected source")
                    api.play_ids.add(play)
            except BaseException:
                report["cleanup_errors"].append("Owned playback cleanup inventory could not be confirmed")
            for play in sorted(api.play_ids):
                if play in cleanup_attempts:
                    continue
                cleanup_attempts.add(play)
                try:
                    video.cleanup_encoding(api, db, play)
                except BaseException:
                    report["cleanup_errors"].append("Owned encoding cleanup failed; its mutation was not retried")
            if original is not None:
                try:
                    item = video.hls.detail(api, api.item_id)
                    no_private_fields(item)
                    check(item.get("UserData") == user_data and video.support.source_projection(item["MediaSources"][0]) == original,
                          "Original public facts or user history changed before logout")
                    report["original_dto_and_user_data_preserved"] = True
                except BaseException:
                    report["cleanup_errors"].append("Original DTO or user-data preservation could not be confirmed")
        if api is not None and api.token:
            try:
                api.request("POST", "/emby/Sessions/Logout", emby=True, expected=(200,), parse=False,
                            label="Owned fast-seek viewer logout")
                api.token = ""
            except BaseException:
                report["cleanup_errors"].append("Owned viewer authentication revocation failed; logout was not retried")
        if api is not None and api.session_id and db is not None:
            try:
                video.audio.wait_inactive(db, api)
                check(db.revoked(api), "The owned viewer authentication remains active")
                report["owned_playback_scopes_with_revoked_authentication"] = len(api.play_ids)
                report["owned_session_revoked_and_encoders_inactive"] = True
            except BaseException:
                report["cleanup_errors"].append("Owned authentication and encoding shutdown could not be confirmed")
        if upgrade is not None and upgrade.prepared:
            try:
                report["preservation_after_cleanup"] = upgrade.audit_preservation()
                if upgrade.finished:
                    report["probe_six_after_cleanup"] = upgrade.verify()
            except BaseException:
                report["cleanup_errors"].append("Final fixture and complete semantic preservation could not be confirmed")
        if api is not None and api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False,
                            label="Owned fast-seek administrator logout")
                api.cookie = ""
            except BaseException:
                report["cleanup_errors"].append("Owned administrator authentication revocation failed; logout was not retried")
        if preservation is not None:
            try:
                report["business_preservation"] = preservation.audit(revoked=True)
            except BaseException:
                report["cleanup_errors"].append("Complete old-row preservation and owned-row cleanup audit failed")
        if accepted is not None:
            try:
                check(deployment() == accepted, "The accepted service process or executable changed during verification")
                report["accepted_deployment_unchanged"] = True
            except BaseException:
                report["cleanup_errors"].append("The accepted service identity could not be reconfirmed")
        if scratch is not None:
            try:
                scratch.cleanup()
                report["owned_tmpfs_output_removed"] = True
            except BaseException:
                report["cleanup_errors"].append("Owned temporary media output cleanup failed")
        if owned is not None and owned.lock is not None:
            try:
                owned.lock.close()
            except BaseException:
                report["cleanup_errors"].append("The existing fixture ownership lock could not be released")
        if api is not None:
            secrets.extend([api.cookie, api.csrf, api.token])
            report["http_requests"] = dict(api.calls)
        report["http_response_bytes"] = http.bytes if http is not None else 0
        report["player_reports_sent"] = 0
        report["finished_at"] = datetime.now(timezone.utc).isoformat()
        if report["cleanup_errors"]:
            report["status"] = "failed"
        if parent_identity is not None:
            try:
                info = RESULT.parent.lstat()
                check((info.st_dev, info.st_ino) == parent_identity and stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                      info.st_mode & 0o022 == 0 and RESULT.parent.resolve(strict=True) == RESULT.parent,
                      "The fixed report directory changed during verification")
                encoded = canonical(report).decode("ascii")
                check(not any(value and value in encoded for value in secrets), "The sanitized report contains a credential")
                private_write(RESULT, report)
            except BaseException:
                report = {"owner": OWNER, "status": "failed", "failed_stage": "sanitized first-result persistence",
                          "error": "The protected report could not be safely persisted; no overwrite or retry was issued",
                          "cleanup_errors": ["Inspect protected remote state; raw details were not emitted"]}
        print(json.dumps(report, sort_keys=True, ensure_ascii=True, indent=2))
    return 0 if report["status"] == "passed" else 2 if report["status"] == "incomplete" else 1


if __name__ == "__main__":
    raise SystemExit(main())
