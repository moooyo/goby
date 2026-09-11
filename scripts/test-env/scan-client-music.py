#!/usr/bin/env python3
"""Run one receipted normal scan of the existing owned M3e Music library.

Run only through root SSH after the explicitly pinned schema25 candidate has
completed its upgrade. The pinned fixture operator supplies read-only owner,
process, database, media, credential, and complete-snapshot checks. This script
never starts services, creates libraries, edits media, or restores user data.
An existing operation directory is never resumed, adopted, or retried.
"""

from __future__ import annotations

import argparse
import copy
import datetime as dt
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import stat
import sys
import time
import types

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
OPERATOR = WORK / "prepare-client-fixture.py"
OUTPUT = WORK / "client-music-scan-v1"
MARKER = "goby-client-music-scan-m3e-v1"
ARTIST = "M3e Synthetic Artist"
ALBUM = "M3e Synthetic Album"
TRACKS = {"M3e Client Audio.mp3": "M3e MP3", "M3e Client Audio.flac": "M3e FLAC"}
CONTROLS = {"Name", "SortName", "Overview"}
JOB_FIELDS = {"Id", "LibraryId", "ForceProbe", "Status", "Error", "Scanned", "Added", "Updated", "CreatedAt", "StartedAt", "FinishedAt"}


class ScanError(Exception):
    """The one-shot scan's owner, intent, or preservation proof is incomplete."""


def require(condition, message):
    if not condition:
        raise ScanError(message)


def validate_arguments(args):
    require(type(args.schema) is int and args.schema == 25, "Only the explicit schema25 scan is supported.")
    for value in (args.candidate_sha256, args.operator_sha256):
        require(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value), "An explicit expected input SHA-256 is required.")


def stamp(value):
    require(isinstance(value, str), "A database timestamp is missing.")
    try:
        result = dt.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        raise ScanError("A database timestamp is malformed.") from None
    require(result.tzinfo is not None, "A database timestamp has no timezone.")
    return result


def in_window(value, window):
    return stamp(window["before"]) <= stamp(value) <= stamp(window["after"])


def indexed(rows, key):
    require(isinstance(rows, list) and all(isinstance(row, dict) and key in row for row in rows), "A complete row inventory is malformed.")
    result = {row[key]: row for row in rows}
    require(len(result) == len(rows), "A complete row inventory contains duplicate identities.")
    return result


def unchanged(op, before, after, omitted=()):
    return op.equal_json({key: value for key, value in before.items() if key not in omitted},
                         {key: value for key, value in after.items() if key not in omitted})


def controlled_values(metadata):
    result = {}
    for key in ("locked_values", "overrides"):
        controls = metadata.get(key)
        require(isinstance(controls, dict) and set(controls) <= CONTROLS and
                all(isinstance(value, str) for value in controls.values()),
                "This first scan supports only existing Name, SortName, or Overview controls; artist/all-field locks require separate review.")
        result.update(controls)
    return result


def accepted_source(name, album_artist=False):
    # Struct order and empty arrays match musicMetadataSource's JSON encoding.
    return {"Version": 1, "Name": name, "Album": ALBUM, "Artists": [ARTIST],
            "AlbumArtists": [ARTIST] if album_artist else []}


def source_hash(source):
    # All accepted source strings are the exact ASCII tags in the fixed fixture.
    return hashlib.sha256(json.dumps(source, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


def sequence_next(sequence):
    require(isinstance(sequence, dict) and set(sequence) == {"last_value", "is_called"} and
            type(sequence["last_value"]) is int and type(sequence["is_called"]) is bool,
            "A sequence counter is not an exact recorded integer and boolean.")
    return sequence["last_value"] + int(sequence["is_called"])


def prepare_music_plan(op, before, state):
    """Reject unsupported initial state before creating a login or scan intent."""
    require(before.get("schema") == 25 and state.get("schema") == 25, "The candidate has not completed schema25.")
    database = before["database"]
    tables = database["tables"]
    music_id = state["libraries"]["music"]["id"]
    require(isinstance(music_id, str) and re.fullmatch(r"[0-9a-f]{32}", music_id), "The Music library ID is not canonical.")
    libraries = indexed(tables["libraries"], "id")
    require(music_id in libraries and libraries[music_id]["name"] == "M3e Client Music" and
            libraries[music_id]["collection_type"] == "music", "The selected library is not the existing owned Music library.")
    roots = [row for row in tables["library_roots"] if row["library_id"] == music_id]
    root_path = str(op.MEDIA_ROOT / "Music")
    require(len(roots) == 1 and roots[0]["path"] == root_path, "Music no longer has its single owned fixture root.")
    items = indexed(tables["items"], "id")
    music = [row for row in items.values() if row["library_id"] == music_id]
    albums = [row for row in music if row["type"] == "MusicAlbum" and row["is_folder"] is True]
    audio = [row for row in music if row["type"] == "Audio" and row["is_folder"] is False]
    containers = [row for row in music if row["id"] == music_id and row["type"] == "CollectionFolder" and row["is_folder"] is True]
    require(len(music) == 4 and len(albums) == 1 and len(audio) == 2 and len(containers) == 1,
            "The first-scan catalog must contain exactly its old root, album, and two audio items.")
    album = albums[0]
    require(album["root_id"] == roots[0]["id"] and album["parent_id"] == music_id and
            album["path"] == "" and album["relative_path"] == "//album/root",
            "The existing root album hierarchy differs from its scanner identity.")
    by_file = {Path(row["path"]).name: row for row in audio}
    require(set(by_file) == set(TRACKS) and all(row["parent_id"] == album["id"] and row["root_id"] == roots[0]["id"] and
            row["path"] == root_path + "/" + name and row["relative_path"] == name for name, row in by_file.items()),
            "The music files or their old parent identities differ from the fixed fixture.")
    metadata = indexed(tables["item_metadata_state"], "item_id")
    targets = {album["id"], *(row["id"] for row in audio)}
    require(not any(row["kind"] == "MusicArtist" for row in tables["catalog_entities"]) and
            not any(row["item_id"] in targets for row in tables["item_entities"]),
            "Existing music artist facts require a separately reviewed rescan plan.")
    for row in (album, *audio):
        record = metadata[row["id"]]
        controls = controlled_values(record)
        fallback = "Music" if row["type"] == "MusicAlbum" else "M3e Client Audio"
        automatic = record.get("automatic")
        require(row["local_metadata"] is None and op.equal_json(record.get("music_source"), {}) and
                isinstance(automatic, dict) and automatic.get("Name") == fallback and automatic.get("SortName") == fallback.lower() and
                automatic.get("Overview") == "" and isinstance(record.get("source_key"), dict) and
                "MusicSourceHash" not in record["source_key"] and record["source_key"].get("HasNFO") is False,
                "The first music scan has stale, edited, or previously accepted source facts.")
        require(op.equal_json(record.get("effective"), controls or None),
                "The initial sparse projection differs from its controls and would add an unaccounted entity synchronization.")
        require(row["name"] == controls.get("Name", fallback) and row["sort_name"] == controls.get("SortName", fallback.lower()) and
                row["overview"] == controls.get("Overview", ""), "The initial effective item does not match its preserved controls.")
        if row["type"] == "Audio":
            require(isinstance(row["media"], dict) and "EmbeddedMusic" not in row["media"], "Audio embedded music was already accepted.")
        else:
            require(row["media"] is None, "The root album unexpectedly contains technical media facts.")
    for table, field in (("scan_jobs", "status"), ("task_runs", "state"), ("task_run_children", "state")):
        require(not any(str(row.get(field, "")).lower() in {"waiting", "pending", "queued", "running", "stopping"} for row in tables[table]),
                "An existing scan or scheduled operation is still active.")
    sessions = indexed(tables["sessions"], "id")
    for row in tables["play_sessions"]:
        require(row.get("state") not in {"Playing", "Paused"},
                "Playback must be quiescent before the controlled scan.")
        if row.get("state") != "Prepared":
            continue
        # Logout may leave an unstarted Prepared row as immutable history.
        credential_id, user_id = row.get("auth_session_id"), row.get("user_id")
        require(isinstance(credential_id, str) and credential_id != "" and credential_id in sessions and
                isinstance(user_id, str) and user_id != "" and
                "started_at" in row and row["started_at"] is None and
                "counted" in row and row["counted"] is False and
                "application_client_id" in row and row["application_client_id"] is None,
                "Prepared playback lacks an unstarted ordinary-session history proof.")
        credential = sessions[credential_id]
        require(credential.get("kind") == "emby" and credential.get("user_id") == user_id and
                credential.get("revoked_at") is not None and
                stamp(credential["revoked_at"]) <= stamp(database["metadata"]["captured_at"]),
                "Prepared playback is not owned by the same user's already revoked ordinary session.")
    baseline = op.trusted_schema_baseline(25, op.schema25_binding(state))
    for name in ("catalog_entities_id_seq", "activity_entries_id_seq"):
        facts = [row["value"] for row in baseline["objects"] if row["kind"] == "sequence" and row["name"] == name]
        require(len(facts) == 1 and facts[0]["increment"] == 1 and facts[0]["cache"] == 1 and facts[0]["cycle"] is False,
                "A sequence no longer has the reviewed exact consumption model.")
        require(sequence_next(database["sequences"][name]) + 4 <= facts[0]["max"], "A controlled sequence would overflow.")
    return {"music_id": music_id, "album_id": album["id"], "audio": {name: row["id"] for name, row in by_file.items()},
            "music_ids": sorted(targets), "artist_id": sequence_next(database["sequences"]["catalog_entities_id_seq"])}


class ScopedAPI:
    """Use the frozen transport only for this operation's exact native calls."""

    def __init__(self, op, state, receipt, persist, check):
        self.op, self.state, self.receipt, self.persist, self.check = op, state, receipt, persist, check
        self.transport = op.NativeAPI(state)
        self.transport.received = self.received
        self.original_cookie = None
        self.csrf = None
        self.verified_admin = False
        library_id = state["libraries"]["music"]["id"]
        require(isinstance(library_id, str) and re.fullmatch(r"[0-9a-f]{32}", library_id), "The scan route has no canonical owned library ID.")
        self.scan_route = "/admin/v1/libraries/" + library_id + "/scan"

    def received(self, route, method, code, raw, cookie):
        if route == "/admin/v1/session" and method == "POST" and code == 200:
            data = self.op.precise_json(raw)
            token = re.fullmatch(r"goby_session=([A-Za-z0-9_-]{43})", (cookie or "").split(";", 1)[0])
            user = data.get("User", {})
            require(token and user.get("Id") == self.state["admin_id"] and user.get("Name") == self.op.ACCOUNTS["admin"] and
                    user.get("IsAdministrator") is True and user.get("IsDisabled") is False and
                    isinstance(data.get("CSRFToken"), str) and 1 <= len(data["CSRFToken"]) <= 256 and
                    all(32 <= ord(character) < 127 for character in data["CSRFToken"]), "The login response is not the owned native administrator.")
            self.original_cookie, self.csrf = token.group(0), data["CSRFToken"]
            self.receipt["token_sha256"] = hashlib.sha256(token.group(1).encode()).hexdigest()
            self.receipt["login_status"] = 200
            self.verified_admin = True
            self.persist("login_acknowledged")
        elif route == self.scan_route and method == "POST" and code == 202:
            data = self.op.precise_json(raw)
            job = data.get("Job", {})
            require(isinstance(job.get("Id"), str) and re.fullmatch(r"[0-9a-f]{32}", job["Id"]) and
                    job.get("LibraryId") == self.state["libraries"]["music"]["id"] and job.get("ForceProbe") is False and
                    job["Id"] not in self.receipt["previous_job_ids"] and "job_id" not in self.receipt,
                    "The scan response did not identify exactly one new owned normal job.")
            self.receipt["job_id"] = job["Id"]
            self.receipt["scan_status"] = 202
            self.persist("scan_acknowledged")

    def request(self, route, method="GET", body=None, expected=(200,), admin=False):
        login = route == "/admin/v1/session" and method == "POST"
        scan = route == self.scan_route and method == "POST"
        permitted = login or scan or (method == "GET" and route in ("/admin/v1/libraries", "/admin/v1/jobs", "/admin/v1/session")) or \
            (method == "DELETE" and route == "/admin/v1/session")
        require(permitted and "?" not in route and "#" not in route, "A request escaped the one-shot music scan API scope.")
        self.check()
        if login:
            require(not self.receipt.get("login_requested") and not admin and expected == (200,) and
                    isinstance(body, dict) and set(body) == {"Name", "Password"} and body["Name"] == self.op.ACCOUNTS["admin"],
                    "An administrator login cannot be repeated or redirected.")
            self.receipt["login_requested"] = True
            self.persist("login_requested")
        else:
            require(self.verified_admin and admin and self.original_cookie and self.csrf,
                    "A protected operation has no exact owned administrator credential.")
            self.transport.cookie, self.transport.csrf = self.original_cookie, self.csrf
            if scan:
                require(not self.receipt.get("scan_requested") and body == {} and expected == (202,),
                        "A normal scan cannot be changed, retried, or forced.")
                self.receipt["scan_requested"] = True
                self.persist("scan_requested")
            else:
                require(body is None, "A read or logout cannot carry a mutation body.")
                if method == "DELETE":
                    require(not self.receipt.get("logout_requested") and expected == (204,), "Logout cannot be repeated.")
                    self.receipt["logout_requested"] = True
                    self.persist("logout_requested")
        return self.transport.request(route, method, body, expected=expected, admin=admin)

    def poll_job(self, deadline):
        require(self.receipt.get("job_id"), "A scan with unknown outcome cannot be adopted from a job list.")
        while True:
            result = self.request("/admin/v1/jobs", admin=True)
            require(isinstance(result.get("Items"), list) and len(result["Items"]) <= 1000, "The job list is not bounded.")
            matches = [job for job in result["Items"] if job.get("Id") == self.receipt["job_id"]]
            require(len(matches) == 1 and matches[0].get("LibraryId") == self.state["libraries"]["music"]["id"] and
                    matches[0].get("ForceProbe") is False and set(matches[0]) == JOB_FIELDS,
                    "The exact returned scan job is missing, has unowned fields, or changed ownership.")
            job = matches[0]
            if job.get("Status") not in ("pending", "queued", "running"):
                require(job.get("Status") == "completed" and job.get("Error") == "" and
                        all(type(job.get(key)) is int for key in ("Scanned", "Added", "Updated")) and
                        job.get("Scanned") == 2 and job.get("Added") == 0 and job.get("Updated") == 2,
                        "The exact normal scan did not complete the two existing audio files without warnings.")
                self.receipt["job_result"] = job
                self.persist("scan_completed")
                return job
            require(time.monotonic() < deadline, "The exact job is still live beyond this observation window; it will not be restarted.")
            time.sleep(2)

    def logout(self):
        require(self.verified_admin, "There is no owned administrator token to retire.")
        self.request("/admin/v1/session", "DELETE", expected=(204,), admin=True)
        denied = self.request("/admin/v1/session", expected=(401,), admin=True)
        require(isinstance(denied, dict) and denied.get("Error", {}).get("Code") == "invalid_credentials",
                "The original native token was not proven invalid after logout.")
        self.receipt["logout_status"], self.receipt["token_readback_status"] = 204, 401
        self.persist("token_revocation_proven")


def compare_scan_snapshots(op, before, after, state, receipt, plan):
    """Prove every owned difference, then reuse the full preservation comparer."""
    op.validate_preservation_snapshot(before, 25, state)
    op.validate_preservation_snapshot(after, 25, state)
    require(receipt.get("login_status") == 200 and receipt.get("scan_status") == 202 and
            receipt.get("logout_status") == 204 and receipt.get("token_readback_status") == 401,
            "The operation lacks its exact login, scan acknowledgement, or token rejection proof.")
    windows = receipt["windows"]
    for key in ("login", "scan", "logout"):
        require(stamp(windows[key]["before"]) <= stamp(windows[key]["after"]), "An operation window moved backwards.")
    require(stamp(before["database"]["metadata"]["captured_at"]) <= stamp(windows["login"]["before"]) <=
            stamp(windows["login"]["after"]) <= stamp(windows["scan"]["before"]) <= stamp(windows["scan"]["after"]) <=
            stamp(windows["logout"]["before"]) <= stamp(windows["logout"]["after"]) <=
            stamp(after["database"]["metadata"]["captured_at"]), "Snapshot and action windows do not form one ordered operation.")
    left, right = before["database"]["tables"], after["database"]["tables"]
    normalized = copy.deepcopy(after)
    output = normalized["database"]["tables"]

    old_sessions, sessions = indexed(left["sessions"], "id"), indexed(right["sessions"], "id")
    new_sessions = set(sessions) - set(old_sessions)
    require(len(new_sessions) == 1 and set(old_sessions) <= set(sessions) and
            all(op.equal_json(row, sessions[key]) for key, row in old_sessions.items()), "An old session changed or more than one native login was created.")
    session_id = next(iter(new_sessions))
    session = sessions[session_id]
    expected_session = {"id": session_id, "user_id": state["admin_id"], "token_hash": "\\x" + receipt["token_sha256"],
        "kind": "admin", "client_name": "Goby Dashboard", "device_id": "goby-dashboard", "device_name": "Web browser",
        "client_version": receipt["product_version"], "created_at": session["created_at"], "expires_at": session["expires_at"],
        "last_seen_at": session["created_at"], "revoked_at": session["revoked_at"], "client_capabilities": {}, "device_registry_id": None}
    require(op.equal_json(session, expected_session) and in_window(session["created_at"], windows["login"]) and
            in_window(session["revoked_at"], windows["logout"]) and
            stamp(session["expires_at"]) - stamp(session["created_at"]) == dt.timedelta(hours=24),
            "The new session does not bind the actual token, native administrator, or action windows.")
    output["sessions"] = copy.deepcopy(left["sessions"])

    old_jobs, jobs = indexed(left["scan_jobs"], "id"), indexed(right["scan_jobs"], "id")
    job_id = receipt["job_id"]
    require(set(jobs) - set(old_jobs) == {job_id} and set(old_jobs) <= set(jobs) and
            all(op.equal_json(row, jobs[key]) for key, row in old_jobs.items()), "An unowned scan job was changed or added.")
    job = jobs[job_id]
    expected_job = {"id": job_id, "library_id": plan["music_id"], "status": "Completed", "error": "", "scanned": 2, "added": 0, "updated": 2,
        "created_at": job["created_at"], "started_at": job["started_at"], "finished_at": job["finished_at"],
        "cancel_requested": False, "force_probe": False, "task_child_id": None}
    require(op.equal_json(job, expected_job) and all(in_window(job[key], windows["scan"]) for key in ("created_at", "started_at", "finished_at")) and
            stamp(job["created_at"]) <= stamp(job["started_at"]) <= stamp(job["finished_at"]), "The completed job is not the returned normal two-file scan.")
    result = receipt["job_result"]
    expected_result = {"Id": job_id, "LibraryId": plan["music_id"], "Status": "completed", "Error": "", "ForceProbe": False,
        "Scanned": 2, "Added": 0, "Updated": 2, "CreatedAt": result["CreatedAt"], "StartedAt": result["StartedAt"], "FinishedAt": result["FinishedAt"]}
    require(op.equal_json(result, expected_result) and all(stamp(result[key]) == stamp(job[column]) for key, column in
            (("CreatedAt", "created_at"), ("StartedAt", "started_at"), ("FinishedAt", "finished_at"))), "The terminal HTTP job and stored job differ.")
    output["scan_jobs"] = copy.deepcopy(left["scan_jobs"])

    old_items, items = indexed(left["items"], "id"), indexed(right["items"], "id")
    old_metadata, metadata = indexed(left["item_metadata_state"], "item_id"), indexed(right["item_metadata_state"], "item_id")
    require(set(items) == set(old_items) and set(metadata) == set(old_metadata), "A scan changed item or metadata identities.")
    sources = {plan["album_id"]: accepted_source(ALBUM, True)}
    sources.update({item_id: accepted_source(TRACKS[name]) for name, item_id in plan["audio"].items()})
    for item_id, original in old_items.items():
        current = items[item_id]
        if item_id not in sources:
            require(op.equal_json(original, current), "A non-music item changed, including its technical or source facts.")
            continue
        old_record, record = old_metadata[item_id], metadata[item_id]
        controls = controlled_values(old_record)
        source = sources[item_id]
        require(unchanged(op, original, current, {"name", "sort_name", "media", "updated_at"}) and
                current["name"] == controls.get("Name", source["Name"]) and
                current["sort_name"] == controls.get("SortName", source["Name"].lower()) and
                in_window(current["updated_at"], windows["scan"]), "Music changed an unowned item field or suppressed preserved name controls.")
        if original["type"] == "Audio":
            expected_media = dict(original["media"], EmbeddedMusic={"Version": 1, "Title": source["Name"], "Album": ALBUM, "Artist": ARTIST})
            require(op.equal_json(current["media"], expected_media), "Music probing changed technical media facts or invented embedded tags.")
        else:
            require(current["media"] is None, "The album acquired invented technical media.")
        automatic = dict(old_record["automatic"], Name=source["Name"], SortName=source["Name"].lower(),
                         Album=ALBUM, Artists=[ARTIST], AlbumArtists=source["AlbumArtists"])
        key = dict(old_record["source_key"], MusicSourceHash=source_hash(source))
        projection = {name: value for name, value in source.items() if name != "Version"}
        projection.update(controls)
        require(unchanged(op, old_record, record, {"automatic", "source_key", "effective", "revision", "updated_at", "music_source"}) and
                op.equal_json(record["music_source"], source) and op.equal_json(record["automatic"], automatic) and
                op.equal_json(record["source_key"], key) and op.equal_json(record["effective"], projection) and
                type(old_record["revision"]) is int and type(record["revision"]) is int and record["revision"] == old_record["revision"] + 1 and
                in_window(record["updated_at"], windows["scan"]), "Music source, projection, revision, or preserved controls differ from accepted tags.")
    for item_id in set(old_metadata) - set(sources):
        require(op.equal_json(old_metadata[item_id], metadata[item_id]), "A non-music metadata row changed.")
    output["items"], output["item_metadata_state"] = copy.deepcopy(left["items"]), copy.deepcopy(left["item_metadata_state"])

    old_entities, entities = indexed(left["catalog_entities"], "id"), indexed(right["catalog_entities"], "id")
    artist_id = plan["artist_id"]
    require(set(entities) - set(old_entities) == {artist_id} and set(old_entities) <= set(entities) and
            all(op.equal_json(row, entities[key]) for key, row in old_entities.items()), "A preexisting entity changed or an unowned entity was created.")
    artist = entities[artist_id]
    expected_artist = {"id": artist_id, "kind": "MusicArtist", "name": ARTIST, "normalized_name": ARTIST.lower(),
                       "normalized_hash": "\\x" + hashlib.sha256(ARTIST.lower().encode()).hexdigest(), "created_at": artist["created_at"]}
    require(op.equal_json(artist, expected_artist) and in_window(artist["created_at"], windows["scan"]),
            "The new music artist does not come from the exact accepted Artist tag.")
    expected_links = copy.deepcopy(left["item_entities"])
    for item_id in sources:
        expected_links.append({"item_id": item_id, "entity_id": artist_id, "position": 1, "display_name": ARTIST,
                               "role": "", "credit_type": "Artist", "sort_order": None, "credit_group": 1})
    expected_links.append({"item_id": plan["album_id"], "entity_id": artist_id, "position": 1, "display_name": ARTIST,
                           "role": "", "credit_type": "AlbumArtist", "sort_order": None, "credit_group": 2})
    require(sorted(op.canonical_json(row) for row in right["item_entities"]) == sorted(op.canonical_json(row) for row in expected_links),
            "Music role indexes changed an old association or invented a credit.")
    output["catalog_entities"], output["item_entities"] = copy.deepcopy(left["catalog_entities"]), copy.deepcopy(left["item_entities"])

    old_libraries, libraries = indexed(left["libraries"], "id"), indexed(right["libraries"], "id")
    require(set(libraries) == set(old_libraries), "A scan added or removed a library.")
    for library_id, original in old_libraries.items():
        if library_id == plan["music_id"]:
            require(unchanged(op, original, libraries[library_id], {"last_scan_at"}) and
                    in_window(libraries[library_id]["last_scan_at"], windows["scan"]), "The Music definition changed outside its scan timestamp.")
        else:
            require(op.equal_json(original, libraries[library_id]), "A non-music library changed.")
    output["libraries"] = copy.deepcopy(left["libraries"])

    old_audit, audit = indexed(left["activity_entries"], "id"), indexed(right["activity_entries"], "id")
    first_audit = sequence_next(before["database"]["sequences"]["activity_entries_id_seq"])
    require(set(audit) - set(old_audit) == set(range(first_audit, first_audit + 4)) and set(old_audit) <= set(audit) and
            all(op.equal_json(row, audit[key]) for key, row in old_audit.items()), "An old activity changed or sequence consumption lacks exact operation events.")
    actions = ("session.login", "scan.requested", "scan.finished", "session.revoked")
    for index, action in enumerate(actions):
        event = audit[first_audit + index]
        is_session, system = action.startswith("session."), action == "scan.finished"
        expected_event = {"id": first_audit + index, "created_at": event["created_at"], "action": action, "severity": "Info",
            "source": "system" if system else "native", "actor_kind": "system" if system else "user", "actor_id": "" if system else state["admin_id"],
            "actor_credential_id": "" if system else session_id, "resource_kind": "session" if is_session else "scan",
            "resource_id": session_id if is_session else job_id, "request_id": "", "revision": 0, "affected_count": 1 if is_session else 0,
            "state": "completed" if system else "", "changed_fields": []}
        window = windows["login"] if action == "session.login" else windows["logout"] if action == "session.revoked" else windows["scan"]
        require(op.equal_json(event, expected_event) and in_window(event["created_at"], window), "An appended activity is not the exact owned native session or scan event.")
    output["activity_entries"] = copy.deepcopy(left["activity_entries"])
    for name, count in (("catalog_entities_id_seq", 3), ("activity_entries_id_seq", 4)):
        first = sequence_next(before["database"]["sequences"][name])
        require(op.equal_json(after["database"]["sequences"][name], {"last_value": first + count - 1, "is_called": True}),
                "A sequence advanced beyond the exact accepted insert and conflict-insert count.")
        normalized["database"]["sequences"][name] = copy.deepcopy(before["database"]["sequences"][name])
    # Every approved difference above has been independently proved. Restoring
    # only those exact rows/counters in a comparison copy delegates all other
    # tables, credentials, recovery files, OIDs, ACLs, and catalog facts to the
    # already verified strict preservation reader. No stored data is rewritten.
    op.compare_preservation_snapshots(before, normalized, 25, 25, state)
    return {"session_id": session_id, "job_id": job_id, "new_music_artist_id": artist_id,
            "music_items_updated": 3, "music_role_rows_added": 4, "audit_rows_added": 4}


def protected_operator_bytes():
    for path in (OPERATOR, *OPERATOR.parents):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022,
                "The active fixture operator or a parent permits untrusted replacement.")
        if path != OPERATOR:
            require(stat.S_ISDIR(info.st_mode), "An operator parent is not a directory.")
    before = OPERATOR.lstat()
    require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and stat.S_IMODE(before.st_mode) in (0o600, 0o644) and
            before.st_size <= 2 << 20, "The active operator has an unexpected file identity.")
    with os.fdopen(os.open(OPERATOR, os.O_RDONLY | os.O_NOFOLLOW), "rb") as handle:
        opened = os.fstat(handle.fileno())
        require((opened.st_dev, opened.st_ino) == (before.st_dev, before.st_ino), "The operator changed while opening it.")
        raw = handle.read((2 << 20) + 1)
    require(len(raw) <= 2 << 20, "The active operator exceeds its read limit.")
    return raw


def load_operator(expected):
    raw = protected_operator_bytes()
    require(hashlib.sha256(raw).hexdigest() == expected, "The active operator differs from its required SHA-256.")
    # Execute the exact already-verified bytes, not a second path-based read.
    module = types.ModuleType("goby_owned_music_fixture_operator")
    module.__file__ = str(OPERATOR)
    exec(compile(raw, str(OPERATOR), "exec"), module.__dict__)
    require(module.WORK == WORK and module.PG_PORT == 15432 and module.PORT == 18198 and
            module.MARKER == "goby-m3e-client-acceptance-v1", "The pinned operator addresses a different fixture.")
    return module


class MusicScan:
    def __init__(self, args):
        validate_arguments(args)
        self.args, self.op, self.state, self.state_bytes = args, None, None, None
        self.lock, self.output_identity = None, None
        self.receipt, self.receipt_bytes, self.events = None, None, 0
        self.before, self.after, self.plan, self.api = None, None, None, None
        self.stage = "initialization"

    def check(self):
        op, state = self.op, self.state
        require(hashlib.sha256(protected_operator_bytes()).hexdigest() == self.args.operator_sha256 and
                op.read(op.STATE_FILE) == self.state_bytes, "The active operator or fixture receipt changed during this operation.")
        require(state.get("marker") == op.MARKER and state.get("phase") == "ready" and state.get("schema") == self.args.schema and
                state.get("binary_sha256") == self.args.candidate_sha256 and state.get("work") == str(WORK) and
                state.get("work_identity") == op.directory(WORK, 0, 0o700, 0) and
                state.get("upgrade", {}).get("phase") == "complete" and
                not any(state.get(key) for key in ("start_pending", "credentials_pending", "directory_pending", "binary_pending", "unit_pending")),
                "The candidate is not the exact completed schema25 upgrade.")
        op.verify_fixture_directories(state)
        op.verify_database(state)
        require(op.verify_service(state) == state["process"], "The owned candidate process changed.")
        require(op.added_viewer_receipt(state) is not None, "The schema25 candidate lacks the receipted third AV account.")
        if self.output_identity is not None:
            require(op.directory(OUTPUT, 0, 0o700, 0) == self.output_identity, "The operation evidence directory changed identity.")
            if self.receipt_bytes is not None:
                require(op.read(OUTPUT / "receipt.json") == self.receipt_bytes, "The one-shot receipt changed outside this operation.")

    def clock(self):
        return self.op.postgres("SELECT clock_timestamp()::text;", self.op.ROLE)

    def persist(self, phase):
        destination = OUTPUT / "receipt.json"
        if self.receipt_bytes is None:
            require(not self.op.exists(destination), "An unrecorded operation receipt cannot be adopted.")
        else:
            require(self.op.read(destination) == self.receipt_bytes, "The operation receipt changed before publication.")
        self.receipt["phase"] = phase
        self.events += 1
        require(self.events <= 64 and self.op.directory(OUTPUT, 0, 0o700, 0) == self.output_identity,
                "The bounded operation journal changed identity or exceeded its phase limit.")
        payload = self.op.canonical_json(self.receipt) + b"\n"
        self.op.create(OUTPUT / f"event-{self.events:04d}.json", payload)
        next_file = OUTPUT / f"receipt-{self.events:04d}.next"
        self.op.create(next_file, payload)
        require((not self.op.exists(destination)) if self.receipt_bytes is None else self.op.read(destination) == self.receipt_bytes,
                "The operation receipt changed immediately before replacement.")
        os.replace(next_file, destination)
        self.receipt_bytes = payload
        self.op.sync(OUTPUT)

    def prepare(self):
        require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
                "Run the controlled music scan only through authorized root SSH.")
        os.umask(0o077)
        self.op = load_operator(self.args.operator_sha256)
        op = self.op
        op.host_inputs(initial=False)
        lock_info = op.regular(op.LOCK)
        self.lock = os.open(op.LOCK, os.O_RDWR | os.O_NOFOLLOW)
        require(op.identity(os.fstat(self.lock)) == op.identity(lock_info), "The existing fixture lock changed while opening it.")
        fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        require(not op.exists(OUTPUT), "A music-scan attempt already has evidence; it cannot be repeated or adopted.")
        self.state_bytes = op.read(op.STATE_FILE)
        self.state = op.precise_json(self.state_bytes)
        self.check()
        require(op.media_snapshot() == self.state["media"], "The full immutable media manifest or file identities changed.")
        manifest = op.precise_json(op.read(op.MEDIA_ROOT / "manifest.json", mode=0o644))
        require(len(manifest.get("files", {})) == 14 and {name for name in manifest["files"] if name.startswith("Music/")} ==
                {"Music/" + name for name in TRACKS}, "The scan requires the exact fourteen-file media fixture with two unmodified music files.")
        self.before = op.preservation_snapshot(self.state, 25)
        op.validate_preservation_snapshot(self.before, 25, self.state)
        self.plan = prepare_music_plan(op, self.before, self.state)
        version = self.state["upgrade"].get("product_version")
        require(isinstance(version, str) and version, "The completed upgrade did not record its native product version.")
        OUTPUT.mkdir(mode=0o700)
        self.output_identity = op.directory(OUTPUT, 0, 0o700, 0)
        self.receipt = {"marker": MARKER, "fixture_tag": self.state["tag"], "server_id": self.state["server_id"],
            "candidate_sha256": self.args.candidate_sha256, "operator_sha256": self.args.operator_sha256, "schema": 25,
            "fixture_state_sha256": hashlib.sha256(self.state_bytes).hexdigest(), "process": self.state["process"],
            "cluster": self.state["cluster"], "music_id": self.plan["music_id"], "product_version": version,
            "media_manifest_sha256": self.state["media"]["manifest_sha256"],
            "previous_job_ids": sorted(row["id"] for row in self.before["database"]["tables"]["scan_jobs"]), "windows": {}}
        op.create(OUTPUT / "OWNER.json", {"marker": MARKER, "fixture_tag": self.state["tag"],
                  "process": self.state["process"], "candidate_sha256": self.args.candidate_sha256, "identity": self.output_identity})
        op.create(OUTPUT / "before-full.json", self.before)
        self.persist("prepared")
        self.api = ScopedAPI(op, self.state, self.receipt, self.persist, self.check)

    def execute(self):
        op = self.op
        browser = op.load(op.BROWSER)
        require(browser.get("marker") == op.MARKER and browser.get("admin", {}).get("username") == op.ACCOUNTS["admin"] and
                isinstance(browser["admin"].get("password"), str) and re.fullmatch(r"[0-9a-f]{48}", browser["admin"]["password"]),
                "The existing private administrator credential does not match the fixture.")
        self.stage = "login"
        self.receipt["windows"]["login"] = {"before": self.clock()}
        self.api.request("/admin/v1/session", "POST", {"Name": browser["admin"]["username"], "Password": browser["admin"]["password"]})
        self.receipt["windows"]["login"]["after"] = self.clock()
        del browser
        libraries = self.api.request("/admin/v1/libraries", admin=True)
        selected = [row for row in libraries.get("Items", []) if row.get("Id") == self.plan["music_id"]]
        require(len(selected) == 1 and selected[0].get("Name") == "M3e Client Music" and selected[0].get("CollectionType") == "music" and
                selected[0].get("Paths") == [str(op.MEDIA_ROOT / "Music")], "The API Music library differs from the exact owned database root.")
        self.stage = "scan"
        self.receipt["windows"]["scan"] = {"before": self.clock()}
        self.api.request(self.api.scan_route, "POST", {}, expected=(202,), admin=True)
        self.api.poll_job(time.monotonic() + 300)
        self.receipt["windows"]["scan"]["after"] = self.clock()

    def run(self):
        error = None
        try:
            self.prepare()
            self.execute()
        except Exception as caught:
            error = str(caught) if isinstance(caught, ScanError) else type(caught).__name__
        finally:
            if self.api is not None and self.api.verified_admin:
                try:
                    self.receipt["windows"]["logout"] = {"before": self.clock()}
                    self.api.logout()
                    self.receipt["windows"]["logout"]["after"] = self.clock()
                except Exception as caught:
                    error = error or (str(caught) if isinstance(caught, ScanError) else type(caught).__name__)
            if self.output_identity is not None:
                try:
                    self.check()
                    self.after = self.op.preservation_snapshot(self.state, 25)
                    self.op.create(OUTPUT / "after-full.json", self.after)
                    require(self.op.media_snapshot() == self.state["media"], "A shared media file or its manifest changed during scanning.")
                    if error is None:
                        self.receipt["proof"] = compare_scan_snapshots(self.op, self.before, self.after, self.state, self.receipt, self.plan)
                except Exception as caught:
                    error = error or (str(caught) if isinstance(caught, ScanError) else type(caught).__name__)
                try:
                    self.receipt["result"] = "passed" if error is None else "retained_for_review"
                    self.receipt["error"] = error
                    self.persist("complete" if error is None else "incomplete")
                    self.op.create(OUTPUT / "report.json", {"marker": MARKER, "result": self.receipt["result"], "error": error,
                        "job_id": self.receipt.get("job_id"), "proof": self.receipt.get("proof"),
                        "logout_status": self.receipt.get("logout_status"), "token_readback_status": self.receipt.get("token_readback_status"),
                        "before": self.op.preservation_summary(self.before),
                        "after": self.op.preservation_summary(self.after) if self.after else None,
                        "rollback_performed": False, "user_data_rewrites": 0})
                except Exception:
                    error = error or "The operation result could not be durably recorded."
            if self.api is not None:
                self.api.original_cookie = self.api.csrf = None
                self.api.transport.cookie = self.api.transport.csrf = None
            if self.lock is not None:
                os.close(self.lock)
                self.lock = None
        print(json.dumps({"result": "passed" if error is None else "retained_for_review", "error": error,
                          "output": str(OUTPUT) if self.output_identity else None, "job_id": self.receipt.get("job_id") if self.receipt else None}))
        return 0 if error is None else 1


def main(arguments=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--candidate-sha256", required=True)
    parser.add_argument("--operator-sha256", required=True)
    parser.add_argument("--schema", type=int, required=True, choices=(25,))
    args = parser.parse_args(arguments)
    validate_arguments(args)
    def interrupted(_number, _frame):
        raise ScanError("The one-shot music scan was interrupted; no scan will be resent.")
    for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(number, interrupted)
    return MusicScan(args).run()


if __name__ == "__main__":
    raise SystemExit(main())
