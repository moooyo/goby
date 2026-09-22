#!/usr/bin/env python3
"""Prepare a fresh, owned Phase 3 native fixture. Source-only until admission.

The external controller starts and owns the guest, PostgreSQL, Goby and its
deployment configuration. This actor creates accounts/libraries through real
HTTP, writes only beneath its declared fresh workspace, and reads PostgreSQL to
bind actual identities. It never inserts synthetic catalog rows or fabricates
an acceptance result. Failed preparation requires external owned-resource
closure; it must not be replayed over its partially populated fixture.
"""

import argparse
import copy
import hashlib
from http.cookies import SimpleCookie
import importlib.util
import json
import os
from pathlib import Path
import secrets
import stat
import sys
import time
from urllib.parse import urlsplit
from xml.sax.saxutils import escape


HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("phase3_workload", HERE / "media-analysis-phase3-workload.py")
WORK = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(WORK)
need, exact, digest, json_bytes = WORK.need, WORK.exact, WORK.digest, WORK.json_bytes
strict_json, read_private, sql_string = WORK.strict_json, WORK.read_private, WORK.sql_string


def hash_file(path, deadline=None, maximum=24 << 30):
    hasher = hashlib.sha256()
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_size <= maximum, "hash_file_bound")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            remaining = before.st_size
            while remaining:
                need(deadline is None or time.monotonic() < deadline, "hash_deadline")
                chunk = stream.read(min(1 << 20, remaining))
                need(chunk, "hash_source_shortened")
                hasher.update(chunk)
                remaining -= len(chunk)
            need(not stream.read(1), "hash_source_grew")
        after = os.fstat(fd)
        stamp = lambda value: (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns)
        need(stamp(before) == stamp(after) == stamp(os.stat(path, follow_symlinks=False)), "hash_source_changed")
    finally:
        os.close(fd)
    return hasher.hexdigest()


def private_write(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())


def safe_source(path, expected_bytes, expected_hash, deadline):
    path = Path(path)
    need(path.is_absolute() and path.resolve() == path, "licensed_source_path")
    info = path.stat()
    need(stat.S_ISREG(info.st_mode) and info.st_size == expected_bytes and 0 < info.st_size <= 16 << 30, "licensed_source_size")
    need(hash_file(path, deadline, expected_bytes) == expected_hash, "licensed_source_hash")
    return path


def source_components(cases):
    """Keep episode, variant and content aliases in one independent group."""
    parents = list(range(len(cases)))
    def find(index):
        while parents[index] != index:
            parents[index] = parents[parents[index]]
            index = parents[index]
        return index
    seen = {}
    for index, case in enumerate(cases):
        aliases = [("episode", case["series_id"], case["season_id"], case["episode_id"]),
                   ("content", case["source"]["sha256"]), ("variant", case["variant_group"])]
        for alias in aliases:
            if alias in seen:
                parents[find(index)] = find(seen[alias])
            else:
                seen[alias] = index
    groups = {}
    for index, case in enumerate(cases):
        groups.setdefault(find(index), []).append(case["case_id"])
    return {identity: "licensed-source-" + digest(json_bytes(sorted(identities)))[:24]
            for identities in groups.values() for identity in identities}


def accepted_compound(result, events, manifest, context):
    """Admit only the complete, original three-phase capacity conclusion."""
    need(type(result) is dict and type(result.get("schema_version")) is int and result.get("schema_version") == 1 and result.get("accepted") is True
         and result.get("execution_complete") is True and result.get("failure_codes") == []
         and result.get("cleanup_errors") == [], "after_compound_result_not_accepted")
    checks = result.get("checks")
    need(type(checks) is dict and checks and all(value is True for value in checks.values())
         and checks.get("cleanup") is True and checks.get("no_observer_failures") is True, "after_compound_checks_incomplete")
    for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier", "guest", "thresholds", "concurrency"):
        need(result.get(key) == manifest[key], "after_compound_result_scope")
    need(result.get("manifest_sha256") == context["manifest_sha256"] and result.get("driver_sha256") == context["driver_sha256"], "after_compound_result_source_binding")
    need(type(events) is list and len(events) <= manifest["budgets"]["max_events"], "after_compound_event_budget")
    need(len(context["scan_libraries"]) == 1, "after_compound_single_capacity_library_required")
    need(all(context["catalog_expected"][phase] == manifest["tier"] for phase in WORK.PHASES), "after_compound_original_exact_tier_required")
    library = context["scan_libraries"][0]
    for phase in WORK.PHASES:
        observed = [row for row in events if row.get("kind") == "scan_completed" and row.get("phase") == phase]
        need(len(observed) == 1 and observed[0].get("counters") == library["expected"][phase]
             and observed[0].get("force_probe") is (phase == "cold"), "after_compound_phase_scan_missing")
        need(result.get("scan_work", {}).get(phase, {}).get("completed_scans") == observed, "after_compound_phase_result_mismatch")
        counted = [row for row in events if row.get("kind") == "catalog_exact_count" and row.get("phase") == phase]
        overlap = [row for row in events if row.get("kind") == "compound_overlap" and row.get("phase") == phase]
        need(len(counted) == 1 and counted[0].get("count") == context["catalog_expected"][phase]
             and len(overlap) == 1 and overlap[0].get("analysis_admission_overlapped") is True
             and set(overlap[0].get("productive_overlap_ms", {})) == {"intro", "previews"}
             and all(value >= manifest["thresholds"]["min_all_lane_overlap_ms"] for value in overlap[0]["productive_overlap_ms"].values()), "after_compound_phase_acceptance_missing")
    increments = [row for row in events if row.get("kind") == "increment_verified"]
    need(len(increments) == 1 and increments[0].get("phase") == "incremental" and increments[0].get("deleted") == 1
         and increments[0].get("added") == 1 and increments[0].get("stable_cross_root_identity") is True
         and increments[0].get("user_state_preserved") is True, "after_compound_increment_proof_missing")
    return {"Scanned": library["expected"]["incremental"]["Scanned"], "Added": 1, "Updated": 1}


def reconciled_transition(before, after, old, context):
    """One restored pathname is new; an intentional deletion is never revived."""
    mutation = context["mutation"]
    moved_id, removed_id, added_id = old["move"]["id"], old["delete_id"], before["addition"]["id"]
    need(before["catalog_count"] == after["catalog_count"] == context["catalog_expected"]["incremental"], "after_compound_exact_catalog_count")
    need(before["move"]["id"] == after["move"]["id"] == moved_id
         and before["move"]["path"] == mutation["move_to"] and after["move"]["path"] == mutation["move_from"]
         and after["move"]["root_id"] == old["move"]["root_id"]
         and before["move"]["file_identity"] == after["move"]["file_identity"] == old["move"]["file_identity"], "after_compound_move_identity")
    restored = after["restored"]
    need(before["restored"] is None and type(restored) is dict and restored["path"] == mutation["delete_path"]
         and restored["id"] not in {removed_id, added_id, moved_id} and before["old_deleted_count"] == after["old_deleted_count"] == 0
         and after["addition"] is None and after["restored_userdata_count"] == 0, "after_compound_deleted_identity_not_new")
    need(before["unaffected_sha256"] == after["unaffected_sha256"] and before["unaffected_count"] == after["unaffected_count"], "after_compound_unrelated_catalog_changed")
    need(before["move_userdata"] == after["move_userdata"] and before["userdata_sha256"] == after["userdata_sha256"], "after_compound_user_state_changed")
    need(before["settings_sha256"] == after["settings_sha256"] and before["metadata_sha256"] == after["metadata_sha256"], "after_compound_controls_changed")
    return {"removed_incremental_item_id": added_id, "permanently_deleted_original_item_id": removed_id,
            "recreated_item_id": restored["id"], "stable_moved_item_id": moved_id,
            "removed_item_count": 1, "created_item_count": 1, "old_deleted_identity_revived": False,
            "old_deleted_user_state_revived": False, "moved_user_state_preserved": True}


def reconciliation_population(tier, catalog_count, userdata_count, move_userdata_count, expected_move_rows):
    """Population checks accompany, and never truncate, ordered row hashes."""
    integer = WORK.integer
    integer(tier, 1, 200000, "reconciliation_tier_bound")
    integer(catalog_count, 0, tier, "reconciliation_catalog_population_bound")
    integer(userdata_count, 0, 200000, "reconciliation_userdata_population_bound")
    integer(move_userdata_count, 0, 200000, "reconciliation_witness_observed_bound")
    integer(expected_move_rows, 1, 4096, "reconciliation_witness_population_bound")
    need(move_userdata_count == expected_move_rows, "reconciliation_witness_population_changed")


class Preparer(WORK.Actor):
    def __init__(self, manifest, operator, evidence):
        self.operator = operator
        self.workspace = Path(operator["workspace_root"])
        self.original_owner = Path(operator["owner_file"])
        self.generated_bytes = 0
        self.inventory_rows, self.originals = [], {}
        self.hardlink_batches = {}
        self.item_paths, self.library_ids, self.root_bindings = {}, {}, []
        context = {"run_id": manifest["run_id"], "owner_id": manifest["owner_id"], "origin": operator["origin"],
            "admin_cookie": "", "csrf_token": "", "emby_token": "", "user_id": "uninitialized",
            "device_id": "phase3-workload", "owner_file": operator["owner_file"], "app_pid": operator["app_pid"],
            "app_start_ticks": operator["app_start_ticks"], "cgroup_path": operator["cgroup_path"],
            "postgres": operator["postgres"], "process_observer": operator["process_observer"],
            "scan_evidence_path": operator["scan_evidence_path"], "scan_evidence_sha256": manifest["scan_evidence"]["configuration_sha256"],
            "pg_env": operator["pg_env"], "tools": operator["tools"]}
        super().__init__(manifest, context, evidence)
        self.deadline = time.monotonic() + operator["preparation_seconds"]

    def validate_context(self):
        origin = urlsplit(self.c["origin"])
        need(origin.scheme == "http" and origin.hostname in {"127.0.0.1", "::1"} and origin.port and not origin.path
             and not origin.query and not origin.fragment and not origin.username, "preparation_loopback_origin")
        need(set(self.c["pg_env"]) <= {"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE"}
             and {"PGHOST", "PGDATABASE", "PGUSER"} <= set(self.c["pg_env"]), "preparation_pg_environment")
        WORK.integer(self.operator["media_read_gid"], 1, 1 << 31, "media_read_group")
        need(self.operator["media_read_gid"] in {os.getgid(), *os.getgroups()}, "actor_cannot_publish_media_group")
        need(os.stat("/proc/%d" % self.c["app_pid"]).st_uid != os.getuid(), "goby_and_actor_must_have_distinct_uids")
        self.validate_process_observer()
        self.assert_owned()

    def assert_owned(self):
        marker = strict_json(read_private(self.original_owner, 16384))
        need(all(marker.get(key) == self.m[key] for key in ("run_id", "owner_id", "source_revision", "guest")), "preparation_owner_marker")
        need(marker.get("workspace_root") == str(self.workspace) and marker.get("app_pid") == self.c["app_pid"]
             and marker.get("app_start_ticks") == self.c["app_start_ticks"] and marker.get("cgroup_path") == self.c["cgroup_path"], "preparation_owned_scope")
        need(marker.get("postgres") == self.c["postgres"], "preparation_postgres_owned_scope")
        fields = Path("/proc/%d/stat" % self.c["app_pid"]).read_text().rsplit(")", 1)[1].split()
        need(int(fields[19]) == self.c["app_start_ticks"], "preparation_app_replaced")
        group = Path(self.c["cgroup_path"])
        need(group.is_absolute() and group.resolve() == group and Path("/sys/fs/cgroup") in group.parents, "preparation_cgroup")
        members = (group / "cgroup.procs").read_text().split()
        need(str(self.c["app_pid"]) in members and str(os.getpid()) not in members, "preparation_cgroup_membership")
        measured = "/" + group.relative_to("/sys/fs/cgroup").as_posix()
        memberships = [line.split(":", 2)[2] for line in Path("/proc/self/cgroup").read_text().splitlines() if line.startswith("0::")]
        need(len(memberships) == 1 and memberships[0] != measured and not memberships[0].startswith(measured + "/"), "preparation_inside_measured_cgroup")
        need(Path("/etc/machine-id").read_text().strip() == self.m["guest"]["machine_id"], "preparation_guest_identity")
        self.validate_postgres_binding()

    def owned(self, path):
        path = Path(path)
        need(path.is_absolute() and path.resolve() == path and self.workspace in path.parents, "preparation_path_scope")
        return path

    def reserve(self, size):
        need(type(size) is int and size >= 0 and self.generated_bytes + size <= self.operator["max_fixture_allocated_bytes"], "preparation_media_byte_budget")
        self.generated_bytes += size

    def media_permissions(self, path, directory=False):
        """Publish only approved media to Goby's read-only supplementary group."""
        path = self.owned(path)
        info = path.lstat()
        need(info.st_uid == os.getuid() and not stat.S_ISLNK(info.st_mode), "media_permission_ownership")
        mode = 0o750 if directory else 0o640
        if info.st_gid != self.operator["media_read_gid"]:
            os.chown(path, -1, self.operator["media_read_gid"], follow_symlinks=False)
        if stat.S_IMODE(info.st_mode) != mode:
            os.chmod(path, mode, follow_symlinks=False)

    def publish_media_tree(self, root):
        root = self.owned(root)
        parent = root
        while parent != self.workspace:
            self.media_permissions(parent, directory=True)
            parent = parent.parent
        count = 0
        for directory, children, filenames in os.walk(root, followlinks=False):
            self.remaining()
            self.media_permissions(Path(directory), directory=True)
            for name in children:
                self.media_permissions(Path(directory) / name, directory=True)
            for name in filenames:
                self.media_permissions(Path(directory) / name)
                count += 1
                need(count <= 600000, "media_permission_entry_budget")

    def copy_pinned(self, source, target):
        source = Path(source)
        before = source.stat()
        self.reserve(((before.st_size + 4095) // 4096 + 1) * 4096)
        incoming = os.open(source, os.O_RDONLY | os.O_NOFOLLOW)
        outgoing = None
        try:
            outgoing = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            need((os.fstat(incoming).st_dev, os.fstat(incoming).st_ino) == (before.st_dev, before.st_ino), "copy_source_replaced")
            remaining = before.st_size
            while remaining:
                self.remaining()
                chunk = os.read(incoming, min(1 << 20, remaining))
                need(chunk, "copy_source_shortened")
                written = 0
                while written < len(chunk):
                    progress = os.write(outgoing, chunk[written:])
                    need(progress > 0, "copy_write_stalled")
                    written += progress
                remaining -= len(chunk)
            need(not os.read(incoming, 1), "copy_source_grew")
            after = os.fstat(incoming)
            need((after.st_size, after.st_mtime_ns, after.st_ctime_ns) == (before.st_size, before.st_mtime_ns, before.st_ctime_ns), "copy_source_changed")
            os.fsync(outgoing)
        finally:
            os.close(incoming)
            if outgoing is not None:
                os.close(outgoing)

    def media_fact(self, path, origin, original_id):
        path = self.owned(path)
        info = path.stat()
        value = {"path": str(path), "sha256": hash_file(path, self.deadline), "bytes": info.st_size, "origin": origin, "original_id": original_id}
        self.inventory_rows.append(value)
        return value

    def copy_media(self, source, target, origin, original_id, hardlink=False):
        self.assert_owned()
        self.remaining()
        target = self.owned(target)
        need(not target.exists() and target.parent.is_dir(), "preparation_target_exists")
        if hardlink:
            source = Path(source)
            batches = self.hardlink_batches.setdefault(str(source), [source])
            if batches[-1].stat().st_nlink >= 10001:
                batch = source.parent / (source.stem + "-link-batch-%04d" % len(batches) + source.suffix)
                need(not batch.exists(), "hardlink_batch_exists")
                self.copy_pinned(source, batch)
                batches.append(batch)
            self.reserve(4096)
            os.link(batches[-1], target, follow_symlinks=False)
        else:
            self.copy_pinned(source, target)
        self.media_permissions(target)
        return self.media_fact(target, origin, original_id)

    def generate(self):
        fixtures = self.workspace / "media-fixtures"
        fixtures.mkdir(mode=0o700)
        templates = fixtures / "templates"
        templates.mkdir(mode=0o700)
        short = templates / "short.mp4"
        av = templates / "playback.mp4"
        audio = templates / "audio.flac"
        self.reserve(3 * (16 << 20))
        base = ["-v", "error", "-nostdin", "-n", "-threads", "1"]
        self.command("ffmpeg", base + ["-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-an",
            "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-threads:v", "1", "-movflags", "+faststart", "-fs", str(16 << 20), str(short)])
        self.command("ffmpeg", base + ["-f", "lavfi", "-i", "testsrc2=s=128x72:r=12", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
            "-t", "120", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "28", "-g", "12", "-pix_fmt", "yuv420p", "-threads:v", "1",
            "-c:a", "aac", "-b:a", "64000", "-ac", "1", "-movflags", "+faststart", "-fs", str(16 << 20), str(av)])
        self.command("ffmpeg", base + ["-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-t", "0.4", "-c:a", "flac", "-threads:a", "1", "-fs", str(16 << 20), str(audio)])
        for path in (short, av, audio):
            os.chmod(path, 0o600)
            need(path.stat().st_size <= 16 << 20, "generated_template_budget")
            raw, _ = self.command("ffprobe", ["-v", "error", "-show_format", "-show_streams", "-of", "json", str(path)])
            probe = strict_json(raw)
            duration = float(probe["format"]["duration"])
            target_duration = 120 if path == av else .4
            need(abs(duration - target_duration) <= .1 and probe.get("streams"), "generated_template_truncated")
            self.e.artifact("generated-template.json", json_bytes({"path": str(path), "sha256": hash_file(path, self.deadline), "probe": probe}))
        self.publish_media_tree(fixtures)
        return short, av, audio

    def authenticate(self):
        initial = self.http("preparation-bootstrap-state", "GET", "/admin/v1/bootstrap", admin=True)[0]
        need(initial == {"Initialized": False}, "preparation_service_not_fresh")
        body = {"SetupToken": self.operator["setup_token"], **self.operator["admin"]}
        self.http("preparation-bootstrap", "POST", "/admin/v1/bootstrap", body, admin=True, statuses=(201,))
        session, headers, _, _ = self.http("preparation-admin-login", "POST", "/admin/v1/session", self.operator["admin"], admin=True)
        cookies = SimpleCookie()
        cookies.load(headers.get("Set-Cookie", ""))
        need("goby_session" in cookies and session.get("CSRFToken"), "preparation_admin_session")
        self.c["admin_cookie"] = "goby_session=" + cookies["goby_session"].value
        self.c["csrf_token"] = session["CSRFToken"]
        self.admin_id = session["User"]["Id"]
        need(self.native("/admin/v1/libraries")["TotalRecordCount"] == 0, "preparation_existing_library")
        viewer = self.native("/admin/v1/users", "POST", {**self.operator["viewer"], "IsAdministrator": False}, (201,))["User"]
        current = self.native("/admin/v1/users/" + viewer["Id"])["User"]
        policy = {**current["Policy"], "EnableAllFolders": True, "EnableMediaPlayback": True, "EnablePlaybackRemuxing": True,
                  "EnableAudioPlaybackTranscoding": True, "EnableVideoPlaybackTranscoding": True}
        self.native("/admin/v1/users/" + viewer["Id"], "PUT", {"Revision": current["Revision"], "Name": current["Name"],
            "IsAdministrator": False, "IsDisabled": False, "Policy": policy})
        login = self.http("preparation-viewer-login", "POST", "/emby/Users/AuthenticateByName",
            {"Username": self.operator["viewer"]["Name"], "Pw": self.operator["viewer"]["Password"]})[0]
        self.c["emby_token"], self.c["user_id"] = login["AccessToken"], login["User"]["Id"]
        self.e.artifact("credentials.json", json_bytes({"admin_id": self.admin_id, "viewer_id": self.c["user_id"],
            "admin_cookie": self.c["admin_cookie"], "csrf_token": self.c["csrf_token"], "emby_token": self.c["emby_token"], "viewer_session_id": login["SessionInfo"]["Id"]}))

    def create_library(self, label, kind, paths):
        for path in paths:
            self.publish_media_tree(path)
        intent = self.admission_intent("library_create", {"label": label, "paths": [str(path) for path in paths]})
        result = self.native("/admin/v1/libraries", "POST", {"Name": "Phase 3 " + label, "CollectionType": kind,
            "Paths": [str(path) for path in paths], "Scan": False}, (201,))
        identity = result["Library"]["Id"]
        self.library_ids[label] = identity
        del self.intents[intent]
        roots = self.sql("SELECT json_agg(json_build_object('id',id,'library_id',library_id,'path',path) ORDER BY path) FROM library_roots WHERE library_id=" + sql_string(identity))
        need(len(roots) == len(paths) and {row["path"] for row in roots} == {str(path) for path in paths}, "preparation_root_binding")
        self.root_bindings.extend(roots)
        return identity

    def scan_seed(self, identity, expected_media):
        job = self.native("/admin/v1/libraries/" + identity + "/scan", "POST", {"ForceProbe": False}, (202,))["Job"]
        self.jobs[job["Id"]] = {"kind": "scan", "active": True}
        while True:
            jobs = {row["Id"]: row for row in self.native("/admin/v1/jobs")["Items"]}
            current = jobs[job["Id"]]
            if current["Status"] in WORK.TERMINAL:
                self.jobs[job["Id"]]["active"] = False
                need(current["Status"] == "completed" and not current["Error"] and current["Scanned"] == expected_media
                     and current["Added"] == expected_media and current["Updated"] == 0, "preparation_seed_scan")
                self.e.artifact("seed-scan.json", json_bytes(current))
                return
            self.remaining()
            time.sleep(.5)

    def item(self, path):
        result = self.sql("SELECT json_build_object('id',id,'name',name,'type',type,'sort_name',sort_name,'is_folder',is_folder) FROM items WHERE path=" + sql_string(str(path)))
        need(type(result) is dict and result.get("id"), "preparation_item_binding")
        return result["id"]

    def seed_userdata(self, resume, move):
        for item in (resume, move):
            self.http("preparation-favorite", "POST", "/emby/Users/" + self.c["user_id"] + "/FavoriteItems/" + item)
        prepared = self.http("preparation-progress-prepare", "POST", "/emby/Items/" + resume + "/PlaybackInfo", {"IsPlayback": True})[0]
        report = {"PlaySessionId": prepared["PlaySessionId"], "ItemId": resume, "MediaSourceId": prepared["MediaSources"][0]["Id"], "PositionTicks": 0, "IsPaused": False}
        self.http("preparation-progress-start", "POST", "/emby/Sessions/Playing", report, statuses=(204,))
        report["PositionTicks"] = 300000000
        self.http("preparation-progress", "POST", "/emby/Sessions/Playing/Progress", report, statuses=(204,))
        self.http("preparation-progress-stop", "POST", "/emby/Sessions/Playing/Stopped", report, statuses=(204,))
        observed = self.sql("SELECT json_build_object('resume',(SELECT playback_position_ticks FROM user_item_data WHERE user_id=" + sql_string(self.c["user_id"]) + " AND item_id=" + sql_string(resume) + "),'move',(SELECT is_favorite FROM user_item_data WHERE user_id=" + sql_string(self.c["user_id"]) + " AND item_id=" + sql_string(move) + "))")
        need(observed == {"resume": 300000000, "move": True}, "preparation_userdata_readback")

    def query_truth(self, where, offset=0, limit=20, future_added=0):
        result = self.sql("SELECT json_build_object('total',(SELECT count(*) FROM items WHERE " + where + "),'ids',(SELECT COALESCE(json_agg(id ORDER BY lower(sort_name),id),'[]'::json) FROM (SELECT id,sort_name FROM items WHERE " + where + " ORDER BY lower(sort_name),id OFFSET " + str(offset) + " LIMIT " + str(limit) + ")p))")
        result["total"] += future_added
        return result

    def frozen_queries(self, library, seed_parent, state_parent, resume, expansion):
        result = {}
        seed_where = "type='Movie' AND parent_id=" + sql_string(seed_parent)
        full_where = "type='Movie' AND library_id=" + sql_string(library)
        for phase in WORK.PHASES:
            cold = phase == "cold"
            base = {"ParentId": seed_parent if cold else library, "Recursive": "true", "IncludeItemTypes": "Movie", "SortBy": "SortName", "SortOrder": "Ascending"}
            where = seed_where if cold else full_where
            extra = 0 if cold else expansion
            queries = []
            for kind, offset, limit in (("shallow", 0, 20), ("deep", self.m["tier"] // 2, 20), ("exact_total", 0, 0)):
                truth = self.query_truth(where, offset, limit, extra)
                need(kind == "exact_total" or len(truth["ids"]) == 20, "preparation_known_page")
                queries.append({"id": kind, "kind": kind, "params": {**base, "StartIndex": str(offset), "Limit": str(limit)}, **truth})
            unicode_truth = self.query_truth(where + " AND name LIKE '%\u7535\u5f71%'", 0, 20)
            queries.append({"id": "unicode", "kind": "unicode", "params": {**base, "SearchTerm": "\u7535\u5f71", "Limit": "20"}, **unicode_truth})
            state_params = {"ParentId": state_parent, "Recursive": "true", "IncludeItemTypes": "Movie", "Limit": "20"}
            queries.append({"id": "filter", "kind": "filter", "params": {**state_params, "IsFavorite": "true", "SortBy": "SortName", "SortOrder": "Ascending"}, "total": 1, "ids": [resume]})
            queries.append({"id": "resume", "kind": "resume", "params": state_params, "total": 1, "ids": [resume]})
            queries.append({"id": "latest", "kind": "latest", "params": {**state_params, "GroupItems": "false"}, "total": 1, "ids": [resume]})
            result[phase] = queries
        return result

    def licensed(self):
        reference = self.operator["licensed_manifest"]
        exact(reference, "path sha256", "licensed_manifest_reference")
        raw = read_private(reference["path"], 2 << 20)
        need(digest(raw) == reference["sha256"], "licensed_manifest_identity")
        manifest = strict_json(raw)
        cases = manifest.get("cases")
        need(type(cases) is list and len(cases) == 14 and len({case["case_id"] for case in cases}) == 14, "fourteen_licensed_cases_required")
        need(set(self.operator["licensed_paths"]) == {case["case_id"] for case in cases}, "licensed_path_mapping")
        selected = self.operator["analysis_case_ids"]
        need(type(selected) is list and 2 <= len(selected) <= 14 and len(set(selected)) == len(selected)
             and set(selected) <= set(self.operator["licensed_paths"]), "analysis_case_selection")
        components = source_components(cases)
        self.licensed_groups = len(set(components.values()))
        roots, counts, episodes, selected_paths, mappings = {}, {}, {}, {}, []
        media = self.workspace / "media" / "licensed"
        media.mkdir(mode=0o700)
        for index, case in enumerate(cases):
            source = case["source"]
            actual = safe_source(self.operator["licensed_paths"][case["case_id"]], source["size_bytes"], source["sha256"], self.deadline)
            cohort = (case.get("evaluation_role", ""), case.get("split", ""), case["series_id"], str(case["season_id"]))
            key = digest(json_bytes(cohort))[:16]
            if key not in roots:
                root = media / ("cohort-" + key)
                season = root / "Corpus Series" / "Season 01"
                season.mkdir(mode=0o700, parents=True)
                roots[key] = root
                counts[key] = 0
                episodes[key] = {}
            season = roots[key] / "Corpus Series" / "Season 01"
            counts[key] += 1
            number = episodes[key].setdefault(case["episode_id"], len(episodes[key]) + 1)
            target = season / ("Corpus.S01E%02d.%s%s" % (number, case["case_id"], actual.suffix.lower()))
            copied = self.copy_media(actual, target, "licensed", components[case["case_id"]])
            need(copied["sha256"] == source["sha256"] and hash_file(actual, self.deadline, source["size_bytes"]) == source["sha256"], "licensed_copy_or_original_changed")
            private_write(target.with_suffix(".nfo"), ("<episodedetails><title>Licensed Case %s</title><season>1</season><episode>%d</episode></episodedetails>" % (escape(case["case_id"]), number)).encode())
            mappings.append({"case_id": case["case_id"], "original_series_id": case["series_id"], "original_season_id": case["season_id"],
                "original_episode_id": case["episode_id"], "independent_source_group": components[case["case_id"]], "actual_catalog_episode_number": number,
                "actual_catalog_season_number": 1, "grouping_basis": "test_indexing_container", "cohort": key})
            if case["case_id"] in selected:
                selected_paths[case["case_id"]] = target
        selected_cohorts = {}
        for case in cases:
            if case["case_id"] in selected:
                group = (case.get("evaluation_role", ""), case.get("split", ""), case["series_id"], str(case["season_id"]))
                selected_cohorts.setdefault(group, set()).add(components[case["case_id"]])
        need(any(len(group) >= 2 for group in selected_cohorts.values()), "analysis_repeated_cohort_required")
        self.e.artifact("licensed-catalog-mapping.json", json_bytes(mappings))
        for key, root in roots.items():
            library = self.create_library("licensed-" + key, "tvshows", [root])
            self.scan_seed(library, counts[key])
        return [self.item(selected_paths[identity]) for identity in selected]

    def populate(self):
        need(not self.workspace.exists() and self.workspace.is_absolute() and self.workspace.resolve() == self.workspace, "preparation_workspace_not_fresh")
        self.workspace.mkdir(mode=0o700)
        os.chown(self.workspace, -1, self.operator["media_read_gid"])
        os.chmod(self.workspace, 0o710)
        for name in ("private", "media"):
            (self.workspace / name).mkdir(mode=0o700)
        quarantine = self.workspace / "private" / "quarantine"
        quarantine.mkdir(mode=0o700)
        short, av, audio = self.generate()
        stage = self.workspace / "media-fixtures" / "staging"
        stage.mkdir(mode=0o700)
        self.media_permissions(stage, directory=True)
        self.authenticate()
        analysis_items = self.licensed()
        left, right = self.workspace / "media" / "capacity-a", self.workspace / "media" / "capacity-b"
        left.mkdir(mode=0o700)
        right.mkdir(mode=0o700)
        seed, expansion_root, state_root, work = (left / name for name in ("Seed", "Expansion", "State", "Workload"))
        for path in (seed, expansion_root, state_root, work):
            path.mkdir(mode=0o700)
        # 4,200 traversed directories and >64 MiB of raw directory-entry names
        # independently exercise the old descriptor and evidence-size limits.
        directory_count, entries_per_directory, entry_name_bytes = 4200, 80, 0
        self.reserve((directory_count * (entries_per_directory + 1) + 512) * 4096)
        for directory in range(directory_count):
            self.remaining()
            path = expansion_root / ("directory-%04d" % directory)
            path.mkdir(mode=0o700)
            entry_name_bytes += len(path.name.encode()) + 1
            for entry in range(entries_per_directory):
                filename = "%03d-%s.fixture" % (entry, "e" * 224)
                fd = os.open(path / filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                os.close(fd)
                entry_name_bytes += len(filename.encode()) + 1
            fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        need(entry_name_bytes > 64 << 20, "raw_directory_evidence_target")
        count = self.m["tier"] // 2 + 100
        for index in range(count):
            target = seed / ("Seed%06d%s.mp4" % (index, "\u7535\u5f71" if index < 10 else ""))
            self.copy_media(short, target, "generated", "short-h264-template", hardlink=True)
        for mode in ("direct", "remux", "transcode"):
            self.copy_media(av, work / ("Play " + mode + ".mp4"), "generated", "playback-h264-aac-template")
        self.copy_media(av, state_root / "State Resume.mp4", "generated", "playback-h264-aac-template")
        for name in ("The Witness.mp4", "Zzz Delete.mp4", "Zzz Move.mp4"):
            self.copy_media(short, work / name, "generated", "short-h264-template")
        self.copy_media(audio, work / "Audio.flac", "generated", "short-flac-template")
        # Staging is not in the media inventory or a scanned root until the
        # incremental phase. Its bytes retain their generated-source identity.
        self.copy_pinned(short, stage / "addition.mp4")
        self.media_permissions(stage / "addition.mp4")
        library = self.create_library("capacity", "mixed", [left, right])
        self.scan_seed(library, count + 8)
        initial = self.sql("SELECT count(*) FROM items")
        pending = self.m["tier"] - initial
        need(pending > 0, "catalog_support_exceeds_tier")
        # Future names sort after every frozen page. Only IDs on returned pages
        # need preindexing; exact full-tier totals use the fixed addition count.
        for index in range(pending):
            target = expansion_root / ("directory-%04d" % (index % directory_count)) / ("Zzz Expansion%06d.mp4" % index)
            self.copy_media(short, target, "generated", "short-h264-template", hardlink=True)
        resume, move = self.item(state_root / "State Resume.mp4"), self.item(work / "Zzz Move.mp4")
        self.seed_userdata(resume, move)
        seed_parent, state_parent = self.item(seed), self.item(state_root)
        queries = self.frozen_queries(library, seed_parent, state_parent, resume, pending)
        raw_rows = []
        for row in self.inventory_rows:
            path = Path(row["path"])
            root = next(root for root in self.root_bindings if Path(root["path"]) in path.parents)
            raw_rows.append(json_bytes({"root_id": root["id"], "relative_path": path.relative_to(root["path"]).as_posix(),
                "sha256": row["sha256"], "bytes": row["bytes"], "origin": row["origin"], "original_id": row["original_id"]}))
        inventory = self.workspace / "private" / "inventory.jsonl"
        inventory_bytes = b"".join(raw_rows)
        self.reserve(((len(inventory_bytes) + 4095) // 4096 + 1) * 4096)
        private_write(inventory, inventory_bytes)
        roots = self.root_bindings
        owner_file = self.workspace / "workload-owner.json"
        owner = {key: self.m[key] for key in ("owner_id", "run_id", "source_revision", "guest")}
        owner.update({"app_pid": self.c["app_pid"], "app_start_ticks": self.c["app_start_ticks"], "cgroup_path": self.c["cgroup_path"], "postgres": self.c["postgres"], "roots": roots})
        private_write(owner_file, json_bytes(owner))
        playback = []
        profile = {"Type": "Video", "Container": "mp4", "Protocol": "http", "Context": "Streaming", "VideoCodec": "h264", "AudioCodec": "aac"}
        for mode in ("direct", "remux", "transcode"):
            path = work / ("Play " + mode + ".mp4")
            if mode == "direct":
                body = {"IsPlayback": True, "DeviceProfile": {"DirectPlayProfiles": [{"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"}]}}
            else:
                body = {"IsPlayback": True, "EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": True,
                    "AllowVideoStreamCopy": mode == "remux", "AllowAudioStreamCopy": mode == "remux", "DeviceProfile": {"TranscodingProfiles": [profile]}}
            playback.append({"mode": mode, "item_id": self.item(path), "path": str(path), "body": body, "seek_ticks": 300000000, "expected_codecs": ["h264", "aac"]})
        observation = self.process_sample()
        configuration_sha256 = observation["scan_evidence"]["configuration_sha256"]
        need(configuration_sha256 == self.m["scan_evidence"]["configuration_sha256"], "preparation_scan_configuration")
        context = {**self.c, "schema_version": 1, "manifest_sha256": self.operator["manifest_sha256"],
            "driver_sha256": hash_file(HERE / "media-analysis-phase3-workload.py"), "owner_file": str(owner_file), "roots": roots,
            "inventory_path": str(inventory), "inventory_sha256": hash_file(inventory),
            "catalog_expected": {"initial": initial, "cold": self.m["tier"], "cached": self.m["tier"], "incremental": self.m["tier"]},
            "scan_libraries": [{"id": library, "expected": {"cold": {"Scanned": count + 8 + pending, "Added": pending, "Updated": count + 8},
                "cached": {"Scanned": count + 8 + pending, "Added": 0, "Updated": 0},
                "incremental": {"Scanned": count + 8 + pending, "Added": 1, "Updated": 1}}}],
            "queries": queries, "playback": playback, "analysis_item_ids": analysis_items,
            "mutation": {"delete_path": str(work / "Zzz Delete.mp4"), "quarantine_path": str(quarantine / "deleted.mp4"),
                "move_from": str(work / "Zzz Move.mp4"), "move_to": str(right / "Zzz Move.mp4"),
                "add_from": str(stage / "addition.mp4"), "add_to": str(work / "Zzz Addition.mp4"),
                "metadata_item_id": self.item(work / "The Witness.mp4"), "sort_words": ["The"]},
            "scan_evidence_path": self.operator["scan_evidence_path"], "scan_evidence_sha256": configuration_sha256}
        # Freeze one coherent handoff; this invokes only admission logic and
        # cannot start a workload, scan, command, or background observer.
        WORK.Actor(self.m, context, self.e)
        private_write(self.workspace / "private" / "workload-context.json", json_bytes(context))
        manifest_bytes = read_private(self.operator["manifest_path"], 1 << 20)
        need(digest(manifest_bytes) == self.operator["manifest_sha256"], "preparation_manifest_changed")
        private_write(self.workspace / "private" / "workload-manifest.json", manifest_bytes)
        allocated, identities, entries = 0, set(), 0
        for directory, children, filenames in os.walk(self.workspace, followlinks=False):
            self.remaining()
            need(all(not (Path(directory) / child).is_symlink() for child in children), "preparation_unexpected_symlink")
            for path in [Path(directory), *(Path(directory) / filename for filename in filenames)]:
                info = path.lstat()
                need(not stat.S_ISLNK(info.st_mode), "preparation_unexpected_symlink")
                identity = (info.st_dev, info.st_ino)
                if identity not in identities:
                    allocated += info.st_blocks * 512
                    identities.add(identity)
                entries += 1
                need(entries <= 600000 and allocated <= self.operator["max_fixture_allocated_bytes"], "preparation_actual_allocation_budget")
        self.e.atomic("prepared.json", {"schema_version": 1, "prepared": True, "accepted_capacity": False,
            "run_id": self.m["run_id"], "tier": self.m["tier"], "manifest_sha256": self.operator["manifest_sha256"],
            "seed_catalog_count": initial, "settled_catalog_count": self.m["tier"], "pending_media_files": pending,
            "licensed_source_cases": 14, "independent_licensed_source_groups": self.licensed_groups,
            "generated_distinct_templates": 3, "media_paths": len(self.inventory_rows), "conservative_fixture_reserved_bytes": self.generated_bytes,
            "actual_fixture_allocated_bytes": allocated, "actual_fixture_unique_inodes": len(identities), "tool_sha256": self.operator["tool_sha256"],
            "hardlink_batches": sum(len(batches) for batches in self.hardlink_batches.values()),
            "licensed_manifest_sha256": self.operator["licensed_manifest"]["sha256"],
            "selected_analysis_cases": self.operator["analysis_case_ids"],
            "directory_stress": {"directories": directory_count, "raw_entry_name_bytes": entry_name_bytes, "zero_byte_nonmedia_entries": directory_count * entries_per_directory},
            "cold_definition": "ForceProbe=true across the complete real tree after a declared indexed seed; OS cache is not claimed cold.",
            "closure": "The external controller owns all issued credentials, native services, PostgreSQL, guest and this prepared workspace."})
        self.e.artifact("prepared-private.json", json_bytes({"context_path": str(self.workspace / "private" / "workload-context.json"),
            "manifest_path": str(self.workspace / "private" / "workload-manifest.json"), "libraries": self.library_ids, "roots": roots,
            "generated_templates": {"short_video": str(short), "playback_video": str(av), "audio": str(audio)}}))


def pinned_private(reference, maximum=16 << 20):
    exact(reference, "path sha256", "pinned_private_fields")
    raw = read_private(reference["path"], maximum)
    need(digest(raw) == reference["sha256"], "pinned_private_changed")
    return raw


def current_mounts():
    def unescape(value):
        return value.replace("\\040", " ").replace("\\011", "\t").replace("\\012", "\n").replace("\\134", "\\")
    rows = []
    for line in Path("/proc/self/mountinfo").read_text().splitlines():
        before, after = line.split(" - ", 1)
        fields, filesystem = before.split(), after.split()
        rows.append({"major_minor": fields[2], "mountpoint": unescape(fields[4]), "options": fields[5].split(","),
                     "filesystem": filesystem[0], "source": unescape(filesystem[1])})
    return rows


class FaultFixture(Preparer):
    """Small media-only extension over already prepared, owned fault mounts."""

    def __init__(self, manifest, context, operator, evidence, binding, receipt):
        self.operator = {**operator, "max_fixture_allocated_bytes": 64 << 20}
        self.workspace = Path(context["owner_file"]).parent
        self.guest_binding, self.prepare_receipt = binding, receipt
        self.generated_bytes, self.inventory_rows, self.hardlink_batches = 0, [], {}
        self.library_ids, self.root_bindings = {}, []
        self.sentinel_record = None
        self.compound_handoff = None
        self.sentinel_root = self.workspace / "media" / "fault-sentinel"
        self.fixture_roots = {Path(fact["fixture_path"]) for fact in receipt["data"]["fixture_directories"].values()
                              if fact["purpose"] in {"media", "replacement"}}
        self.fixture_roots.add(self.sentinel_root)
        self.base_context = copy.deepcopy(context)
        WORK.Actor.__init__(self, manifest, context, evidence)
        self.deadline = time.monotonic() + 600

    def validate_context(self):
        WORK.Actor.validate_context(self)
        need(type(self.operator["media_read_gid"]) is int and self.operator["media_read_gid"] in {os.getgid(), *os.getgroups()}, "fault_media_group")
        need(os.stat("/proc/%d" % self.c["app_pid"]).st_uid != os.getuid(), "fault_goby_and_actor_must_differ")
        need(self.guest_binding["run_id"] == self.m["run_id"] and self.guest_binding["owner_id"] == self.m["owner_id"], "fault_binding_scope")
        need(self.guest_binding["guest"]["vmid"] == 106 and self.guest_binding["guest"]["machine_id"] == self.m["guest"]["machine_id"], "fault_binding_guest")
        need(self.guest_binding["fixture_access"] == {"actor_uid": os.getuid(), "media_read_gid": self.operator["media_read_gid"]}, "fault_fixture_access_binding")
        need(self.prepare_receipt["schema_version"] == 1 and self.prepare_receipt["status"] == "ok"
             and self.prepare_receipt["op"] == "prepare_volumes" and self.prepare_receipt["owner_id"] == self.m["owner_id"]
             and self.prepare_receipt["vmid"] == 106 and self.prepare_receipt["data"]["corpus_populated"] is False, "fault_prepare_receipt")
        need(set(self.prepare_receipt["data"]["prepared_volumes"]) == set(self.guest_binding["volumes"])
             == set(self.prepare_receipt["data"]["fixture_directories"]), "fault_volume_receipt_inventory")
        need(2 <= len(self.guest_binding["volumes"]) <= 8, "fault_volume_limit")
        purposes = [volume["purpose"] for volume in self.guest_binding["volumes"].values()]
        need(purposes.count("media") >= 2 and "replacement" in purposes and "derivatives" in purposes, "fault_volume_roles")

    def assert_owned(self):
        WORK.Actor.assert_owned(self)
        self.assert_volumes()

    def assert_volumes(self):
        mounts = current_mounts()
        owned_root = Path(self.guest_binding["owned_root"])
        for identity, volume in self.guest_binding["volumes"].items():
            fact = self.prepare_receipt["data"]["fixture_directories"][identity]
            exact(fact, "fixture_path purpose device major_minor filesystem_uuid dm_uuid writable_observed actor_uid owner_uid media_read_gid mode inode", "fault_fixture_directory_receipt")
            mount, fixture = Path(volume["mountpoint"]), Path(fact["fixture_path"])
            expected_name = "analysis-cache" if volume["purpose"] == "derivatives" else "fixture-media"
            need(mount.is_absolute() and mount.resolve() == mount and fixture == mount / expected_name
                 and fixture.resolve() == fixture, "fault_mount_path")
            need(owned_root in mount.parents and owned_root in Path(volume["backing_file"]).parents
                 and volume["mapper_name"].startswith("goby-phase3-") and volume["dm_uuid"].startswith("GOBY-PHASE3-" + self.m["owner_id"] + "-"), "fault_mount_owned_namespace")
            matching = [row for row in mounts if row["mountpoint"] == str(mount)]
            need(len(matching) == 1 and matching[0]["major_minor"] == volume["major_minor"] and "rw" in matching[0]["options"]
                 and matching[0]["filesystem"] == "ext4", "fault_prepare_actual_mount")
            info = fixture.lstat()
            need(stat.S_ISDIR(info.st_mode) and info.st_dev == fact["device"] and info.st_ino == fact["inode"]
                 and "%d:%d" % (os.major(info.st_dev), os.minor(info.st_dev)) == volume["major_minor"], "fault_fixture_device_identity")
            need(fact["major_minor"] == volume["major_minor"] and fact["filesystem_uuid"] == volume["filesystem_uuid"]
                 and fact["dm_uuid"] == volume["dm_uuid"] and fact["purpose"] == volume["purpose"]
                 and fact["writable_observed"] == "rw" and fact["actor_uid"] == os.getuid()
                 and fact["media_read_gid"] == self.operator["media_read_gid"], "fault_fixture_receipt_binding")
            expected_owner = self.guest_binding["services"]["goby"]["uid"] if volume["purpose"] == "derivatives" else os.getuid()
            expected_mode = 0o700 if volume["purpose"] == "derivatives" else 0o750
            need(info.st_uid == expected_owner == fact["owner_uid"] and info.st_gid == self.operator["media_read_gid"]
                 and stat.S_IMODE(info.st_mode) == expected_mode == fact["mode"], "fault_fixture_permissions")
            mapper = Path("/dev/mapper") / volume["mapper_name"]
            need((Path("/sys/class/block") / mapper.resolve().name / "dm/uuid").read_text().strip() == volume["dm_uuid"], "fault_dm_identity")
            backing = (Path("/sys/class/block") / Path(volume["loop_device"]).name / "loop/backing_file").read_text().strip()
            need(Path("/" + backing.lstrip("/")).resolve() == Path(volume["backing_file"]), "fault_loop_backing_identity")

    def owned(self, path):
        path = Path(path)
        need(path.is_absolute() and path.resolve() == path and any(path == root or root in path.parents for root in self.fixture_roots), "fault_write_outside_fixture")
        return path

    def publish_media_tree(self, root):
        root = self.owned(root)
        for directory, children, filenames in os.walk(root, followlinks=False):
            self.media_permissions(Path(directory), directory=True)
            for name in children:
                self.media_permissions(Path(directory) / name, directory=True)
            for name in filenames:
                self.media_permissions(Path(directory) / name)

    def item_fact(self, path, library_id, root_id):
        row = self.sql("SELECT json_build_object('id',id,'file_identity',file_identity,'media',media) FROM items WHERE path=" + sql_string(str(path)))
        need(type(row) is dict and row["media"]["FormatStartKnown"] is True, "fault_item_clock")
        detail = self.native("/admin/v1/media-analysis/items/" + row["id"])
        need(detail["Id"] == row["id"] and detail["LibraryId"] == library_id and detail["MediaSourceId"] and detail["SourceRevision"], "fault_item_api_binding")
        info = path.stat()
        videos = [stream for stream in row["media"]["Streams"] if stream["CodecType"] == "video"]
        need(len(videos) == 1, "fault_video_stream")
        root = next(Path(value["path"]) for value in self.root_bindings if value["id"] == root_id)
        return {"item_id": row["id"], "media_source_id": detail["MediaSourceId"], "source_revision": detail["SourceRevision"],
            "path": str(path), "relative_path": path.relative_to(root).as_posix(),
            "source": {"sha256": hash_file(path, self.deadline), "bytes": info.st_size, "device": info.st_dev, "inode": info.st_ino,
                       "modified_ns": info.st_mtime_ns, "changed_ns": info.st_ctime_ns, "file_identity": row["file_identity"]},
            "clock": {"duration_ticks": row["media"]["DurationTicks"], "format_start_ticks": row["media"]["FormatStartTicks"], "format_start_known": True},
            "video_stream_index": videos[0]["Index"]}

    def root_fact(self, library, path, volume_id=None):
        root = next(root for root in self.root_bindings if root["library_id"] == library and root["path"] == str(path))
        binding = self.native("/admin/v1/libraries/" + library + "/roots/" + root["id"] + "/binding")["Binding"]
        need(binding["Status"] == "verified" and binding["ApprovedFingerprint"] == binding["ObservedFingerprint"], "fault_native_root_not_verified")
        if volume_id is not None:
            expected = self.guest_binding["volumes"][volume_id]["filesystem_uuid"]
            need(binding["Approved"]["RegisteredRoot"]["FilesystemUUID"].lower().replace("-", "") == expected.lower().replace("-", ""), "fault_native_filesystem_uuid")
        return {"volume_id": volume_id, "library_id": library, "root_id": root["id"], "path": str(path), "binding": binding, "items": []}

    def sentinel_account(self):
        account = {"Name": "Phase3 sentinel " + self.m["run_id"][:64], "Password": secrets.token_urlsafe(32), "IsAdministrator": False}
        intent = self.admission_intent("sentinel_user", {"name": account["Name"]})
        user = self.native("/admin/v1/users", "POST", account, (201,))["User"]
        del self.intents[intent]
        current = self.native("/admin/v1/users/" + user["Id"])["User"]
        policy = {**current["Policy"], "EnableAllFolders": True, "EnableMediaPlayback": True,
            "EnablePlaybackRemuxing": True, "EnableAudioPlaybackTranscoding": True, "EnableVideoPlaybackTranscoding": True}
        self.native("/admin/v1/users/" + user["Id"], "PUT", {"Revision": current["Revision"], "Name": current["Name"],
            "IsAdministrator": False, "IsDisabled": False, "Policy": policy})
        device = "phase3-fault-sentinel"
        login = self.http("sentinel-login", "POST", "/emby/Users/AuthenticateByName", {"Username": account["Name"], "Pw": account["Password"]},
            headers={"X-Emby-Authorization": 'MediaBrowser Client="Phase3", Device="Linux", DeviceId="%s", Version="1"' % device})[0]
        need(login["User"]["Id"] == user["Id"], "sentinel_login_identity")
        self.sentinel_record = {"user_id": user["Id"], "name": account["Name"], "password": account["Password"], "token": login["AccessToken"],
                                "device_id": device, "auth_session_id": login["SessionInfo"]["Id"]}
        self.e.artifact("sentinel-credential.json", json_bytes(self.sentinel_record))
        return self.sentinel_record

    def compound_artifact(self, reference, maximum=2 << 20):
        exact(reference, "name sha256 bytes", "compound_artifact_fields")
        need(type(reference["name"]) is str and WORK.SAFE.fullmatch(reference["name"]), "compound_artifact_name")
        raw = read_private(self.compound_directory / "private" / reference["name"], maximum)
        need(len(raw) == reference["bytes"] and digest(raw) == reference["sha256"], "compound_artifact_changed")
        return raw

    def compound_http_before(self, events, path):
        candidates = []
        for event in events:
            self.remaining()
            if event.get("kind") != "http" or event.get("phase") != "cold" or event.get("label") != "control" or event.get("status") != 200 or event.get("error"):
                continue
            receipt = strict_json(self.compound_artifact(event["receipt"]))
            if receipt.get("method") == "GET" and receipt.get("path") == path:
                body = self.compound_artifact(event["body"])
                need(receipt.get("body_sha256") == digest(body) and receipt.get("status") == 200, "compound_http_body_binding")
                candidates.append((event["start_ns"], strict_json(body)))
        need(candidates, "compound_original_control_snapshot_missing")
        return min(candidates, key=lambda candidate: candidate[0])[1]

    def compound_sql(self, reference, required_suffix):
        path = Path(reference["path"])
        need(path.parent == self.compound_directory / "private", "compound_sql_outside_result_scope")
        value = strict_json(pinned_private(reference, 2 << 20))
        exact(value, "statement raw command", "compound_sql_receipt_fields")
        need(type(value["statement"]) is str and value["statement"].endswith(required_suffix)
             and type(value["raw"]) is str, "compound_sql_statement_binding")
        command = strict_json(self.compound_artifact(value["command"]))
        need(command.get("tool") == "psql" and command.get("exit_code") == 0 and command.get("stdout_sha256") == digest(value["raw"].encode()), "compound_sql_process_binding")
        return strict_json(value["raw"].encode())

    def no_active_work(self):
        end = min(self.deadline, time.monotonic() + 30)
        while True:
            value = self.sql("SELECT json_build_object('scan_jobs',(SELECT count(*) FROM scan_jobs WHERE status IN ('Queued','Running')),'task_runs',(SELECT count(*) FROM task_runs WHERE state IN ('pending','queued','running','stopping')),'encoding_jobs',(SELECT count(*) FROM encoding_jobs WHERE state IN ('queued','running')),'play_sessions',(SELECT count(*) FROM play_sessions WHERE state IN ('Prepared','Playing','Paused') AND expires_at>clock_timestamp()))")
            resources = self.native("/admin/v1/runtime/resources")
            observation = self.process_sample()
            closed = all(count == 0 for count in value.values()) and resources["OriginalStreams"]["ActiveCount"] == 0 and not observation["processes"]
            evidence = resources["ScanEvidence"]
            closed = closed and evidence["ActivePasses"] == 0 and evidence["RetiringPasses"] == 0 and evidence["CleanupFailures"] == 0
            if closed:
                return {"database": value, "scan_evidence": evidence, "original_active": 0, "media_processes": [],
                        "observed_monotonic_ns": observation["observed_monotonic_ns"]}
            need(time.monotonic() < end, "after_compound_work_not_closed")
            time.sleep(.25)

    def restored_file_facts(self, inventory):
        mutation = self.c["mutation"]
        rows = [strict_json(row) for row in inventory.splitlines()]
        facts = {}
        for key in ("delete_path", "move_from", "add_from"):
            path = Path(mutation[key])
            if key == "add_from":
                expected = self.operator["templates"]["short_video"]
                expected_size, expected_hash = expected["bytes"], expected["sha256"]
            else:
                root_id = self.root_for(str(path))
                relative = path.relative_to(self.roots[root_id]).as_posix()
                candidates = [row for row in rows if row["root_id"] == root_id and row["relative_path"] == relative]
                need(len(candidates) == 1, "after_compound_mutation_inventory")
                expected_size, expected_hash = candidates[0]["bytes"], candidates[0]["sha256"]
            info = path.lstat()
            need(path.resolve() == path and stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size == expected_size
                 and hash_file(path, self.deadline, expected_size) == expected_hash, "after_compound_restored_media_changed")
            facts[key] = {"path": str(path), "device": info.st_dev, "inode": info.st_ino, "bytes": info.st_size,
                          "modified_ns": info.st_mtime_ns, "changed_ns": info.st_ctime_ns, "sha256": expected_hash,
                          "file_identity": "%d:%d" % (info.st_dev, info.st_ino)}
        for key in ("quarantine_path", "move_to", "add_to"):
            need(not os.path.lexists(mutation[key]), "after_compound_restoration_target_retained")
            facts[key] = {"path": mutation[key], "absent": True}
        return facts

    def reconciliation_snapshot(self, old, added_id, expected_move_rows):
        mutation = self.c["mutation"]
        moved, removed, added = map(sql_string, (old["move"]["id"], old["delete_id"], added_id))
        catalog_limit = WORK.integer(self.m["tier"], 1, 200000, "reconciliation_tier_bound")
        WORK.integer(expected_move_rows, 1, 4096, "reconciliation_witness_population_bound")
        row = "jsonb_build_array(id,library_id,root_id,parent_id,type,path,relative_path,name,sort_name,file_identity,file_size)"
        # Aggregate only fixed 64-byte lowercase row hashes, not complete row
        # JSON. IDs stay inside each row hash as well as defining stable order.
        row_hash = "encode(sha256(convert_to(" + row + "::text,'UTF8')),'hex')"
        digest_sql = "encode(sha256(convert_to(COALESCE(string_agg(" + row_hash + ",E'\\n' ORDER BY id),''),'UTF8')),'hex')"
        user_row_hash = "encode(sha256(convert_to(to_jsonb(u)::text,'UTF8')),'hex')"
        user_digest = "encode(sha256(convert_to(COALESCE(string_agg(" + user_row_hash + ",E'\\n' ORDER BY user_id,item_id),''),'UTF8')),'hex')"
        selection = "id NOT IN (" + moved + "," + added + ") AND path<>" + sql_string(mutation["delete_path"])
        item = "SELECT id,library_id,root_id,parent_id,type,path,file_identity FROM items WHERE "
        catalog_guard = "(SELECT catalog_count FROM population)<=" + str(catalog_limit)
        user_guard = "(SELECT userdata_count FROM population)<=200000"
        witness_guard = "(SELECT move_userdata_count FROM population)=" + str(expected_move_rows)
        result = self.sql("WITH population AS MATERIALIZED (SELECT (SELECT count(*) FROM items) AS catalog_count,"
            "(SELECT count(*) FROM user_item_data) AS userdata_count,(SELECT count(*) FROM user_item_data WHERE item_id=" + moved + ") AS move_userdata_count) "
            "SELECT json_build_object('digest_format','sha256-ordered-row-hashes-v1','catalog_count',(SELECT catalog_count FROM population),"
            "'userdata_count',(SELECT userdata_count FROM population),'move_userdata_count',(SELECT move_userdata_count FROM population),"
            "'catalog_sha256',CASE WHEN " + catalog_guard + " THEN (SELECT " + digest_sql + " FROM items) ELSE NULL END,"
            "'unaffected_count',(SELECT count(*) FROM items WHERE " + selection + "),'unaffected_sha256',CASE WHEN " + catalog_guard + " THEN (SELECT " + digest_sql + " FROM items WHERE " + selection + ") ELSE NULL END,"
            "'move',(SELECT row_to_json(x) FROM (" + item + "id=" + moved + ")x),'addition',(SELECT row_to_json(x) FROM (" + item + "id=" + added + ")x),"
            "'restored',(SELECT row_to_json(x) FROM (" + item + "path=" + sql_string(mutation["delete_path"]) + ")x),'old_deleted_count',(SELECT count(*) FROM items WHERE id=" + removed + "),"
            "'move_userdata',CASE WHEN " + witness_guard + " THEN (SELECT COALESCE(json_agg(row_to_json(u) ORDER BY user_id),'[]'::json) FROM user_item_data u WHERE item_id=" + moved + ") ELSE NULL END,"
            "'restored_userdata_count',(SELECT count(*) FROM user_item_data WHERE item_id IN (SELECT id FROM items WHERE path=" + sql_string(mutation["delete_path"]) + ")),"
            "'addition_userdata_count',(SELECT count(*) FROM user_item_data WHERE item_id=" + added + "),"
            "'userdata_sha256',CASE WHEN " + user_guard + " THEN (SELECT " + user_digest + " FROM user_item_data u) ELSE NULL END,"
            "'settings_sha256',(SELECT encode(sha256(convert_to(to_jsonb(s)::text,'UTF8')),'hex') FROM managed_settings s WHERE id=1),"
            "'metadata_sha256',(SELECT encode(sha256(convert_to(to_jsonb(m)::text,'UTF8')),'hex') FROM item_metadata_state m WHERE item_id=" + sql_string(mutation["metadata_item_id"]) + "))")
        reconciliation_population(catalog_limit, result["catalog_count"], result["userdata_count"], result["move_userdata_count"], expected_move_rows)
        need(result["digest_format"] == "sha256-ordered-row-hashes-v1" and all(type(result[key]) is str and len(result[key]) == 64
             for key in ("catalog_sha256", "unaffected_sha256", "userdata_sha256")), "reconciliation_digest_unavailable")
        return result

    def reconcile_after_compound(self):
        need(self.operator["after_compound"] is True, "after_compound_mode_required")
        refs = self.operator["compound_receipts"]
        exact(refs, "result events mutation_before move_userdata_before", "compound_receipt_references")
        self.compound_directory = Path(refs["result"]["path"]).parent
        need(Path(refs["result"]["path"]).name == "result.json" and Path(refs["events"]["path"]) == self.compound_directory / "events.jsonl", "compound_result_directory")
        result = strict_json(pinned_private(refs["result"], 16 << 20))
        raw_events = pinned_private(refs["events"], 128 << 20)
        events = [strict_json(row) for row in raw_events.splitlines()]
        expected = accepted_compound(result, events, self.m, self.base_context)
        need(self.c["driver_sha256"] == hash_file(HERE / "media-analysis-phase3-workload.py"), "after_compound_driver_changed")
        mutation = self.c["mutation"]
        old = self.compound_sql(refs["mutation_before"], "'postgres_version',current_setting('server_version'));\n")
        need(old.get("catalog_count") == self.c["catalog_expected"]["incremental"] and type(old.get("move")) is dict
             and old["move"]["path"] == mutation["move_from"] and old.get("delete_id"), "compound_mutation_before_binding")
        # The captured statement must be the actual Actor snapshot of these
        # two paths, never a selected row from an unrelated fixture.
        old_receipt = strict_json(pinned_private(refs["mutation_before"], 2 << 20))
        need("path=" + sql_string(mutation["move_from"]) in old_receipt["statement"]
             and "path=" + sql_string(mutation["delete_path"]) in old_receipt["statement"], "compound_mutation_path_binding")
        userdata = self.compound_sql(refs["move_userdata_before"], "SELECT COALESCE(json_agg(row_to_json(u) ORDER BY user_id),'[]'::json) FROM user_item_data u WHERE item_id=" + sql_string(old["move"]["id"]) + ";\n")
        need(type(userdata) is list and userdata, "compound_original_move_userdata_missing")
        library = self.c["scan_libraries"][0]
        need(old["move"]["library_id"] == library["id"] and old["move"]["root_id"] == self.root_for(mutation["move_from"]), "compound_move_library_binding")
        selected = {self.c["mutation"]["metadata_item_id"], *self.c["analysis_item_ids"], *(play["item_id"] for play in self.c["playback"])}
        selected.update(identity for queries in self.c["queries"].values() for query in queries for identity in query["ids"])
        need(old["delete_id"] not in selected, "compound_deleted_id_still_selected")
        closed_before = self.no_active_work()
        inventory = pinned_private({"path": self.c["inventory_path"], "sha256": self.c["inventory_sha256"]}, 128 << 20)
        scan_roots = {root["id"] for root in self.c["roots"] if root["library_id"] == library["id"]}
        need(sum(strict_json(row)["root_id"] in scan_roots for row in inventory.splitlines()) == expected["Scanned"], "after_compound_scan_population_changed")
        files_before = self.restored_file_facts(inventory)
        need(files_before["move_from"]["file_identity"] == old["move"]["file_identity"], "after_compound_moved_inode_changed")
        inventory_summary = WORK.Actor.inventory(self)
        addition = self.sql("SELECT json_build_object('id',id,'path',path,'file_identity',file_identity,'type',type,'library_id',library_id) FROM items WHERE path=" + sql_string(mutation["add_to"]))
        need(type(addition) is dict and addition["id"] not in selected and addition["type"] == "Movie"
             and addition["library_id"] == library["id"] and addition["file_identity"] == files_before["add_from"]["file_identity"], "after_compound_incremental_addition_missing")
        before = self.reconciliation_snapshot(old, addition["id"], len(userdata))
        need(before["catalog_count"] == self.c["catalog_expected"]["incremental"] and before["old_deleted_count"] == 0
             and before["restored"] is None and before["move"]["path"] == mutation["move_to"]
             and before["move"]["file_identity"] == old["move"]["file_identity"] and before["move_userdata"] == userdata
             and before["addition_userdata_count"] == 0,
             "after_compound_database_not_incremental_state")
        original_settings = self.compound_http_before(events, "/admin/v1/settings")
        metadata_path = "/admin/v1/items/" + mutation["metadata_item_id"] + "/metadata"
        original_metadata = self.compound_http_before(events, metadata_path)
        settings, metadata = self.native("/admin/v1/settings"), self.native(metadata_path)
        need(all(settings[key] == original_settings[key] for key in ("Overrides", "Sorting"))
             and all(metadata[key] == original_metadata[key] for key in ("Overrides", "LockedFields")), "after_compound_controls_not_restored")
        expected_removed = {"item_id": addition["id"], "count": 1}
        plan = {"schema_version": 1, "operation": "after_compound_reconciliation", "run_id": self.m["run_id"],
            "compound_receipts": refs, "original_manifest": self.operator["manifest"], "original_context": self.operator["base_context"],
            "library_id": library["id"], "force_probe": False, "expected": expected, "expected_removed": expected_removed,
            "catalog_count": before["catalog_count"], "database_before": before, "files_restored_before": files_before,
            "closed_before": closed_before, "inventory_summary": inventory_summary,
            "old_delete_inode_not_independently_recorded_by_compound": True,
            "restored_delete_content_bound_by_original_inventory": True}
        self.e.atomic("after-compound-plan-private.json", plan)
        jobs_before = self.sql("SELECT COALESCE(json_agg(id),'[]'::json) FROM scan_jobs WHERE library_id=" + sql_string(library["id"]))
        need(type(jobs_before) is list and len(jobs_before) <= 256, "after_compound_scan_history_bound")
        intent = self.admission_intent("after_compound_reconciliation", {"library_id": library["id"], "expected": expected, "known_job_ids": jobs_before})
        admission_error, admitted = None, None
        try:
            admitted = self.native("/admin/v1/libraries/" + library["id"] + "/scan", "POST", {"ForceProbe": False}, (202,))["Job"]["Id"]
        except Exception as error:
            admission_error = str(error) if isinstance(error, WORK.Failure) else "reconciliation_admission_response_lost"
        # Never replay POST after an uncertain response. Discover the sole new
        # scan under this frozen library while the database is otherwise idle.
        discovery_deadline = min(self.deadline, time.monotonic() + 30)
        while True:
            jobs = self.native("/admin/v1/jobs")["Items"]
            candidates = [job for job in jobs if job["LibraryId"] == library["id"] and job["Id"] not in jobs_before]
            need(len(candidates) <= 1, "after_compound_ambiguous_scan_admission")
            if candidates:
                job = candidates[0]
                need(admitted is None or admitted == job["Id"], "after_compound_scan_id_mismatch")
                break
            need(time.monotonic() < discovery_deadline, "after_compound_scan_admission_unresolved")
            time.sleep(.25)
        self.jobs[job["Id"]] = {"kind": "scan", "active": True}
        self.intents.pop(intent)
        while job["Status"] not in WORK.TERMINAL:
            self.remaining()
            time.sleep(.25)
            candidates = [row for row in self.native("/admin/v1/jobs")["Items"] if row["Id"] == job["Id"]]
            need(len(candidates) == 1, "after_compound_scan_disappeared")
            job = candidates[0]
        self.jobs[job["Id"]]["active"] = False
        need(job["Status"] == "completed" and job["ForceProbe"] is False and not job["Error"]
             and all(job[key] == value for key, value in expected.items()), "after_compound_reconciliation_counters")
        after = self.reconciliation_snapshot(old, addition["id"], len(userdata))
        transition = reconciled_transition(before, after, old, self.c)
        need(after["restored"]["file_identity"] == files_before["delete_path"]["file_identity"], "after_compound_recreated_file_binding")
        recreated = self.native("/admin/v1/media-analysis/items/" + after["restored"]["id"])
        moved = self.native("/admin/v1/media-analysis/items/" + old["move"]["id"])
        need(recreated["Id"] == after["restored"]["id"] and moved["Id"] == old["move"]["id"]
             and recreated["LibraryId"] == moved["LibraryId"] == library["id"], "after_compound_business_reference_binding")
        transition["recreated_media_source_id"] = recreated["MediaSourceId"]
        transition["recreated_source_revision"] = recreated["SourceRevision"]
        transition["moved_media_source_id"] = moved["MediaSourceId"]
        transition["moved_source_revision"] = moved["SourceRevision"]
        files_after = self.restored_file_facts(inventory)
        need(files_after == files_before, "after_compound_scan_modified_media")
        closed_after = self.no_active_work()
        handoff = {"schema_version": 1, "complete": True, "run_id": self.m["run_id"], "owner_id": self.m["owner_id"],
            "source_revision": self.m["source_revision"], "tier": self.m["tier"], "compound_receipts": refs,
            "original_capacity_result_modified": False, "reconciliation_is_outside_capacity_measurements": True,
            "scan_job": job, "admission_response_error": admission_error, "database_before": before, "database_after": after,
            "files_before": files_before, "files_after": files_after, "closed_after": closed_after, "identity_transition": transition,
            "allowed_workload_phases": ["cached"]}
        self.e.atomic("after-compound-handoff-private.json", handoff)
        path = self.e.directory / "after-compound-handoff-private.json"
        self.compound_handoff = {"path": str(path), "sha256": hash_file(path)}
        return after["catalog_count"]

    def populate_faults(self):
        mutation = self.base_context["mutation"]
        need(not Path(mutation["quarantine_path"]).exists() and Path(mutation["add_from"]).is_file()
             and Path(mutation["add_from"]).stat().st_nlink == 1, "fault_base_staging_changed")
        templates = {}
        for name, reference in self.operator["templates"].items():
            exact(reference, "path sha256 bytes", "fault_template_fields")
            path = Path(reference["path"])
            need(path.parent == self.workspace / "media-fixtures" / "templates" and path.name == ("short.mp4" if name == "short_video" else "playback.mp4"), "fault_approved_template_path")
            WORK.integer(reference["bytes"], 1024, 16 << 20, "fault_template_budget")
            templates[name] = safe_source(path, reference["bytes"], reference["sha256"], self.deadline)
        need(set(templates) == {"short_video", "playback_video"}, "fault_template_inventory")
        need(hash_file(mutation["add_from"], self.deadline, 16 << 20) == self.operator["templates"]["short_video"]["sha256"], "fault_base_addition_changed")
        initial = self.reconcile_after_compound()
        fault_roots, derivatives = [], []
        for volume_id, volume in self.guest_binding["volumes"].items():
            self.assert_volumes()
            fact = self.prepare_receipt["data"]["fixture_directories"][volume_id]
            path = Path(fact["fixture_path"])
            if volume["purpose"] == "derivatives":
                derivatives.append({"volume_id": volume_id, "cache_directory": str(path), "device": fact["device"],
                                    "filesystem_uuid": fact["filesystem_uuid"], "dm_uuid": fact["dm_uuid"]})
                continue
            need(not list(path.iterdir()), "fault_fixture_not_empty")
            nested = path / "nested"
            nested.mkdir(mode=0o700)
            self.media_permissions(nested, directory=True)
            template_kind = "short_video" if volume["purpose"] == "replacement" else "playback_video"
            source = templates[template_kind]
            free = os.statvfs(path)
            need(free.f_bavail * free.f_frsize >= 2 * source.stat().st_size + (1 << 20), "fault_volume_copy_headroom")
            paths = [path / "Fault Video.mp4", nested / "Nested Video.mp4"]
            for target in paths:
                self.copy_media(source, target, "generated", "short-h264-template" if template_kind == "short_video" else "playback-h264-aac-template")
            library = self.create_library("fault-" + volume_id, "movies", [path])
            self.scan_seed(library, 2)
            result = self.root_fact(library, path, volume_id)
            result.update({"purpose": volume["purpose"], "nested_target_relative_path": "fixture-media/nested", "permission_target_relative_path": "fixture-media"})
            result["items"] = [self.item_fact(target, library, result["root_id"]) for target in paths]
            fault_roots.append(result)
        need(not self.sentinel_root.exists(), "sentinel_root_exists")
        self.sentinel_root.mkdir(mode=0o700)
        sentinel_paths = {"progress": self.sentinel_root / "Sentinel Movie.mp4", "metadata": self.sentinel_root / "Metadata Movie.mp4", "lock": self.sentinel_root / "Lock Movie.mp4"}
        for role, path in sentinel_paths.items():
            self.copy_media(templates["playback_video" if role == "progress" else "short_video"], path,
                "generated", "playback-h264-aac-template" if role == "progress" else "short-h264-template")
        library = self.create_library("fault-sentinel", "movies", [self.sentinel_root])
        self.scan_seed(library, 3)
        healthy = self.root_fact(library, self.sentinel_root)
        facts = {role: self.item_fact(path, library, healthy["root_id"]) for role, path in sentinel_paths.items()}
        healthy["items"] = list(facts.values())
        sentinel = self.sentinel_account()
        saved_credentials = {key: self.c[key] for key in ("emby_token", "user_id", "device_id")}
        try:
            self.c.update(emby_token=sentinel["token"], user_id=sentinel["user_id"], device_id=sentinel["device_id"])
            self.seed_userdata(facts["progress"]["item_id"], facts["metadata"]["item_id"])
        finally:
            self.c.update(saved_credentials)
        final_count = self.sql("SELECT count(*) FROM items")
        delta = final_count - initial
        need(0 < delta <= 64, "fault_catalog_delta")
        derived = copy.deepcopy(self.base_context)
        derived["roots"] += self.root_bindings
        # This is a cached-only fault context, not another capacity profile.
        # Historical cold/incremental selectors are retained as source facts;
        # the handoff and wrapper must prohibit executing those phases.
        derived["catalog_expected"] = {phase: final_count for phase in derived["catalog_expected"]}
        raw_inventory = pinned_private({"path": derived["inventory_path"], "sha256": derived["inventory_sha256"]}, 128 << 20)
        for row in self.inventory_rows:
            path = Path(row["path"])
            root = next(root for root in self.root_bindings if Path(root["path"]) in path.parents)
            raw_inventory += json_bytes({"root_id": root["id"], "relative_path": path.relative_to(root["path"]).as_posix(),
                "sha256": row["sha256"], "bytes": row["bytes"], "origin": row["origin"], "original_id": row["original_id"]})
        inventory_path = self.workspace / "private" / "fault-inventory.jsonl"
        private_write(inventory_path, raw_inventory)
        derived["inventory_path"], derived["inventory_sha256"] = str(inventory_path), digest(raw_inventory)
        marker = strict_json(read_private(derived["owner_file"], 16384))
        marker["roots"] = derived["roots"]
        owner_path = self.workspace / "fault-workload-owner.json"
        private_write(owner_path, json_bytes(marker))
        derived["owner_file"] = str(owner_path)
        derived["driver_sha256"] = hash_file(HERE / "media-analysis-phase3-workload.py")
        context_path = self.workspace / "private" / "fault-workload-context.json"
        context_bytes = json_bytes(derived)
        private_write(context_path, context_bytes)
        WORK.Actor(self.m, derived, self.e)
        state_refs = {"user_id": sentinel["user_id"], "metadata_item_id": facts["metadata"]["item_id"],
            "playback_item_id": facts["progress"]["item_id"], "playback_media_source_id": facts["progress"]["media_source_id"],
            "resume_item_id": facts["progress"]["item_id"], "lock_item_id": facts["lock"]["item_id"], "settings_scope": "server"}
        result = {key: self.m[key] for key in ("run_id", "owner_id", "source_revision", "tier")}
        result.update({"schema_version": 1, "prepared": True, "manifest_sha256": self.operator["manifest"]["sha256"],
            "after_compound": True, "compound_result": self.operator["compound_receipts"]["result"],
            "after_compound_handoff": self.compound_handoff, "allowed_workload_phases": ["cached"],
            "guest_binding_sha256": self.operator["guest_binding"]["sha256"], "prepare_receipt_sha256": self.operator["prepare_receipt"]["sha256"],
            "workload_context": {"path": str(context_path), "sha256": digest(context_bytes)}, "workload_manifest": self.operator["manifest"],
            "healthy_roots": [healthy], "fault_roots": fault_roots, "derivatives": derivatives, "sentinel": sentinel, "state_refs": state_refs,
            "mutation_ids": {key: "sentinel-" + key for key in ("metadata", "settings", "progress", "user", "preferences")},
            "catalog_count_before": initial, "catalog_count_after": final_count})
        self.e.atomic("fault-fixture-private.json", result)
        self.e.atomic("fault-prepared.json", {"schema_version": 1, "prepared": True, "accepted_capacity": False, "run_id": self.m["run_id"],
            "after_compound": True, "allowed_workload_phases": ["cached"],
            "fault_media_roots": len(fault_roots), "healthy_roots": 1, "new_catalog_items": delta, "fixture_media_paths": len(self.inventory_rows),
            "conservative_reserved_bytes": self.generated_bytes, "requires_external_readonly_remount": True,
            "derivative_cache_deployment_must_be_bound_by_runtime": True})


def prepare_fault_fixture(operator_path, output):
    operator = strict_json(read_private(operator_path, 2 << 20))
    exact(operator, "schema_version manifest base_context guest_binding prepare_receipt templates media_read_gid after_compound compound_receipts", "fault_operator_fields")
    need(operator["schema_version"] == 1 and operator["after_compound"] is True, "fault_operator_after_compound_mode")
    manifest = WORK.validate_manifest(strict_json(pinned_private(operator["manifest"], 1 << 20)))
    context = strict_json(pinned_private(operator["base_context"]))
    need(context["manifest_sha256"] == operator["manifest"]["sha256"], "fault_manifest_binding")
    binding = strict_json(pinned_private(operator["guest_binding"]))
    receipt = strict_json(pinned_private(operator["prepare_receipt"]))
    evidence = WORK.Evidence(output, manifest)
    actor = FaultFixture(manifest, context, operator, evidence, binding, receipt)
    try:
        actor.populate_faults()
        return 0
    except Exception as error:
        evidence.closing = True
        evidence.artifact("partial-fault-state.json", json_bytes({"libraries": actor.library_ids, "roots": actor.root_bindings,
            "jobs": actor.jobs, "admission_intents": actor.intents, "sentinel": actor.sentinel_record,
            "after_compound_handoff": actor.compound_handoff}))
        evidence.atomic("fault-prepared.json", {"schema_version": 1, "prepared": False, "accepted_capacity": False,
            "run_id": manifest["run_id"], "error_code": str(error) if isinstance(error, WORK.Failure) else "fault_preparation_failure",
            "closure": "External controller must reconcile partial native admissions and close every owned fault resource."})
        return 1


def main(argv=None):
    argv = list(sys.argv[1:] if argv is None else argv)
    operation = argv.pop(0) if argv and argv[0] in {"prepare", "fault-fixture"} else "prepare"
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--operator", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args(argv)
    need(sys.platform == "linux" and os.getuid() != 0, "remote_linux_unprivileged_preparer_required")
    os.umask(0o077)
    if operation == "fault-fixture":
        return prepare_fault_fixture(args.operator, args.output)
    operator = strict_json(read_private(args.operator, 2 << 20))
    exact(operator, "schema_version manifest_path manifest_sha256 workspace_root owner_file origin app_pid app_start_ticks cgroup_path postgres process_observer media_read_gid pg_env tools tool_sha256 setup_token admin viewer licensed_manifest licensed_paths analysis_case_ids preparation_seconds max_fixture_allocated_bytes scan_evidence_path", "operator_fields")
    need(operator["schema_version"] == 1, "operator_version")
    WORK.integer(operator["preparation_seconds"], 60, 14400, "preparation_deadline")
    WORK.integer(operator["max_fixture_allocated_bytes"], 1 << 20, 24 << 30, "preparation_byte_budget")
    for key in ("admin", "viewer"):
        exact(operator[key], "Name Password", "preparation_credentials")
        need(all(type(value) is str and 1 <= len(value) <= 256 for value in operator[key].values()), "preparation_credential_bounds")
    exact(operator["tools"], "psql ffmpeg ffprobe", "preparation_tools")
    need(set(operator["tool_sha256"]) == set(operator["tools"]), "preparation_tool_hash_fields")
    for name, path in operator["tools"].items():
        need(Path(path).is_absolute() and Path(path).resolve() == Path(path) and hash_file(path) == operator["tool_sha256"][name], "preparation_tool_identity")
    raw = read_private(operator["manifest_path"], 1 << 20)
    need(digest(raw) == operator["manifest_sha256"], "preparation_manifest_binding")
    manifest = WORK.validate_manifest(strict_json(raw))
    evidence = WORK.Evidence(args.output, manifest)
    evidence.atomic("preparation-manifest.json", manifest)
    actor = Preparer(manifest, operator, evidence)
    try:
        actor.populate()
        return 0
    except Exception as error:
        evidence.closing = True
        evidence.artifact("partial-owned-state.json", json_bytes({"libraries": actor.library_ids, "roots": actor.root_bindings, "jobs": actor.jobs,
            "credentials": {key: actor.c[key] for key in ("admin_cookie", "csrf_token", "emby_token", "user_id")}}))
        evidence.atomic("prepared.json", {"schema_version": 1, "prepared": False, "accepted_capacity": False,
            "run_id": manifest["run_id"], "error_code": str(error) if isinstance(error, WORK.Failure) else "preparation_failure",
            "closure": "Partial fixture retained; external controller must reconcile late admissions and close every owned resource."})
        return 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"prepared": False, "error_code": str(error) if isinstance(error, WORK.Failure) else "preparation_admission_failure"}))
        sys.exit(1)
