#!/usr/bin/env python3
"""Verify explicit media refresh in one disposable Linux application database.

The shared runner owns the port-15432 role, exact HBA restoration, UID-995
application, private browser artifacts, and cleanup. Only this run's synthetic
catalog is edited; no shared media, reference server, or database is touched.
"""

import argparse
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import sys
import urllib.error
import urllib.request


FIXTURE_MARKER = "goby-media-refresh-fixtures-v1"
FFMPEG = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
FFPROBE = FFMPEG.with_name("ffprobe")
MOVIE_NFO = """<?xml version="1.0" encoding="UTF-8"?>
<movie>
  <title>Automatic refresh movie</title>
  <plot>Automatic refresh overview</plot>
  <year>2024</year>
  <genre>Automatic Genre</genre>
  <tag>Automatic Tag</tag>
  <studio>Automatic Studio</studio>
  <actor><name>Automatic Actor</name><role>Guide</role><order>0</order></actor>
</movie>
"""


def load_core(path):
    sys.dont_write_bytecode = True
    specification = importlib.util.spec_from_file_location("goby_media_refresh_core", path)
    if specification is None or specification.loader is None:
        raise RuntimeError("The shared browser verifier could not be loaded.")
    module = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(module)
    return module


def create_runner(core, args):
    core.MARKER = "goby-media-refresh-browser-v1"
    core.RUN_ENV = "GOBY_MEDIA_REFRESH_RUN_ID"

    class MediaRefreshRunner(core.Runner):
        browser_spec = "media-refresh.spec.ts"
        browser_timeout_seconds = 240
        screenshot_names = (
            "media-refresh-libraries-desktop.png", "media-refresh-dialog-mobile.png",
            "media-refresh-tasks-desktop.png", "media-refresh-tasks-mobile.png",
        )

        def __init__(self, arguments):
            super().__init__(arguments)
            self.database = "goby_m4g_refresh_" + self.run_id
            self.role = "goby_m4g_role_" + self.run_id
            self.pg_app_name = "goby_m4g_control_" + self.run_id
            core.BASE_ENV["PGAPPNAME"] = self.pg_app_name
            self.output = core.EXEC_ROOT / ("goby-media-refresh-" + self.run_id)
            self.runtime = Path("/dev/shm") / ("goby-media-refresh-" + self.run_id)
            self.admin_name = "m4g-admin-" + self.run_id
            self.admin_opener = urllib.request.build_opener(
                urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            self.control_csrf = None
            self.scan_ids = []
            self.report.update({"scenario": "explicit_media_refresh", "database": self.database, "role": self.role})

        def allocate(self):
            super().allocate()
            core.require(self.goby.pw_uid == 995, "The isolated application must use the reviewed UID 995.")
            core.require(os.fstat(self.binary_fd).st_mode & 0o001,
                         "The prepared binary must be executable by the unprivileged application account.")
            for tool in (FFMPEG, FFPROBE):
                core.require(tool.is_file() and os.access(tool, os.X_OK), "A pinned media tool is unavailable.")
                version = core.command([tool, "-version"]).splitlines()[0]
                core.require(re.match(r"^ff(?:mpeg|probe) version 9\.0\.1(?:\s|[-+])", version),
                             "A pinned media tool did not report FFmpeg 9.0.1.")
                self.report[tool.name + "_sha256"] = core.file_digest(tool)
                self.report[tool.name + "_version"] = version
            self.report["verifier_sha256"] = core.file_digest(Path(__file__).resolve())
            self.report["shared_runner_sha256"] = core.file_digest(args.core_runner.resolve())

        def prepare_files(self):
            super().prepare_files()
            # Preserve readable public assets when the operator uses umask 077.
            assets = self.runtime / "admin"
            for directory in [assets, *(entry for entry in assets.rglob("*") if entry.is_dir())]:
                directory.chmod(0o755)
            media = self.runtime / "media"
            # Synthetic inputs are readable but cannot be replaced by the app.
            for directory in (self.runtime, media):
                os.chown(directory, 0, 0)
                directory.chmod(0o755)
            self.movie = media / "Refresh.Movie.2024.mp4"
            core.command([
                FFMPEG, "-hide_banner", "-loglevel", "error", "-nostdin",
                "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=3",
                "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=3",
                "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-preset", "ultrafast",
                "-crf", "28", "-pix_fmt", "yuv420p", "-g", "24", "-keyint_min", "24",
                "-sc_threshold", "0", "-bf", "0", "-c:a", "aac", "-b:a", "32k",
                "-threads", "1", "-filter_threads", "1", "-shortest", "-movflags", "+faststart", self.movie,
            ], timeout=30)
            core.require(0 < self.movie.stat().st_size <= 512 * 1024,
                         "The synthetic media fixture exceeded its byte budget.")
            nfo = self.movie.with_suffix(".nfo")
            subtitle = self.movie.with_name(self.movie.stem + ".en.srt")
            core.private_write(nfo, MOVIE_NFO.encode())
            core.private_write(subtitle, b"1\n00:00:00,000 --> 00:00:02,000\nRefresh preservation fixture\n")
            self.files = {}
            for filename in (self.movie, nfo, subtitle):
                filename.chmod(0o444)
                self.files[filename.name] = core.file_digest(filename)
            self.manifest = self.browser_work / "media-refresh-fixture.json"
            self.result_path = self.browser_work / "media-refresh-result.json"
            self.report["fixtures"] = {"media_count": 1, "nfo_count": 1, "subtitle_count": 1,
                                       "media_bytes": self.movie.stat().st_size, "source_hashes": self.files,
                                       "application_can_replace_sources": False}

        def http_json(self, route, *, body=None, headers=None, method=None, administrator=False, expected=(200, 201)):
            request_headers = {"Accept": "application/json", "Origin": self.origin}
            request_headers.update(headers or {})
            data = None if body is None else json.dumps(body).encode("utf-8")
            if data is not None:
                request_headers["Content-Type"] = "application/json"
            if administrator and (data is not None or method in ("POST", "PUT", "DELETE")) and self.control_csrf:
                request_headers["X-CSRF-Token"] = self.control_csrf
            request = urllib.request.Request(self.origin + route, data=data, headers=request_headers, method=method)
            try:
                opener = self.admin_opener.open if administrator else urllib.request.urlopen
                with opener(request, timeout=5) as response:
                    core.require(response.status in expected, "An isolated refresh request returned an unexpected status.")
                    raw = response.read(3 * 1024 * 1024 + 1)
                    core.require(len(raw) <= 3 * 1024 * 1024, "An isolated response exceeded its byte budget.")
                    return json.loads(raw)
            except (OSError, ValueError, urllib.error.HTTPError):
                raise core.VerificationError("An isolated refresh request failed; response details were not published.") from None

        def owned_database_sql(self, statement):
            core.require(self.database_oid is not None and self.lock is not None and
                         re.fullmatch(r"goby_m4g_refresh_[0-9]{8}_[0-9]{6}_[0-9a-f]{10}", self.database),
                         "Refresh fixture SQL requires this run's owned disposable database.")
            owner = core.pg_json(f"""SELECT jsonb_build_object('oid', oid::bigint,
                'owner', pg_get_userbyid(datdba), 'tag', shobj_description(oid, 'pg_database'))
                FROM pg_database WHERE datname = '{self.database}';""")
            core.require(owner == {"oid": self.database_oid, "owner": self.role, "tag": self.tag},
                         "The refresh fixture database ownership changed.")
            guarded = f"""BEGIN;
                DO $guard$ BEGIN
                    IF current_database() <> '{self.database}' OR current_user <> '{self.role}' THEN
                        RAISE EXCEPTION 'Unexpected refresh fixture database or role';
                    END IF;
                END $guard$;
                {statement}
                COMMIT;
            """
            return core.command([
                core.PG_BIN / "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-At",
                "-h", "127.0.0.1", "-p", str(core.PG_PORT), "-U", self.role, "-d", self.database,
            ], text=guarded, environment=dict(core.BASE_ENV, PGPASSWORD=self.role_password))

        def jobs(self):
            result = self.http_json("/admin/v1/jobs", administrator=True)
            core.require(result["TotalRecordCount"] == len(result["Items"]), "The owned scan history was truncated.")
            return result["Items"]

        def wait_scan(self, job):
            core.require(re.fullmatch(r"[0-9a-f]{32}", job.get("Id", "")) and
                         job.get("LibraryId") == self.library["Id"] and job.get("ForceProbe") is False,
                         "A normal fixture scan did not preserve ForceProbe false.")
            self.scan_ids.append(job["Id"])
            terminal = None
            def complete():
                nonlocal terminal
                current = next((row for row in self.jobs() if row["Id"] == job["Id"]), None)
                core.require(current is not None, "An acknowledged fixture scan disappeared.")
                if current["Status"] in ("pending", "running"):
                    return False
                core.require(current["Status"] == "completed" and current["Error"] == "" and
                             current["Scanned"] == 1 and current["ForceProbe"] is False and current["FinishedAt"],
                             "A normal fixture scan did not complete cleanly.")
                terminal = current
                return True
            core.wait_until(complete, "The isolated fixture scan did not finish.", timeout=60)
            return terminal

        def media(self):
            values = json.loads(self.owned_database_sql(f"""SELECT COALESCE(jsonb_agg(
                jsonb_build_object('Id', id, 'Path', path, 'Media', media) ORDER BY id), '[]'::jsonb)
                FROM items WHERE library_id = '{self.library['Id']}' AND media IS NOT NULL;"""))
            core.require(len(values) == 1 and values[0]["Path"] == str(self.movie),
                         "The isolated catalog does not contain its unique synthetic movie.")
            core.require(re.fullmatch(r"[0-9a-f]{32}", values[0]["Id"]), "The synthetic movie has an invalid identity.")
            if hasattr(self, "item_id"):
                core.require(values[0]["Id"] == self.item_id, "Refresh changed the movie identity.")
            self.item_id = values[0]["Id"]
            return values[0]["Media"]

        def check_index(self, media):
            core.require(media.get("ProbeVersion") == 6 and media.get("FormatStartKnown") is True and
                         type(media.get("DurationTicks")) is int and abs(media["DurationTicks"] - 30000000) <= 100000,
                         "The synthetic movie lacks current bounded probe-six source facts.")
            streams = media.get("Streams", [])
            videos = [stream for stream in streams if stream.get("CodecType") == "video"]
            audios = [stream for stream in streams if stream.get("CodecType") == "audio"]
            core.require(len(videos) == len(audios) == 1 and videos[0].get("Codec") == "h264" and
                         audios[0].get("Codec") == "aac" and
                         (videos[0].get("Width"), videos[0].get("Height")) == (160, 90),
                         "The real synthetic source did not retain its H.264/AAC media facts.")
            indexes = media.get("VideoSeekIndexes")
            core.require(isinstance(indexes, list) and len(indexes) == 1 and
                         len(json.dumps(indexes).encode()) <= 2 * 1024 * 1024,
                         "The synthetic movie lacks a bounded private video index.")
            index = indexes[0]
            core.require(index.get("version") == 1 and index.get("stream_index") == 0 and
                         (index.get("width"), index.get("height"), index.get("pixel_format")) == (160, 90, "yuv420p") and
                         index.get("duration_ticks") == media["DurationTicks"] and
                         index.get("format_start_ticks") == media.get("FormatStartTicks") and
                         index.get("packet_side_data_checked") is True and index.get("nal_scope_checked") is True and
                         index.get("decoded_frame_bytes") == 21600 and
                         all(re.fullmatch(r"[0-9a-f]{64}", index.get(key, "")) for key in
                             ("source_identity", "tool_identity", "parameter_sets_sha256")) and
                         all(type(index.get(key)) is int and index[key] > 0 for key in
                             ("time_base_numerator", "time_base_denominator")),
                         "The private video index is missing its source and packet-scope evidence.")
            entries = index.get("entries")
            core.require(isinstance(entries, list) and 1 <= len(entries) <= 8192 and
                         all(isinstance(entry, dict) and type(entry.get("pts")) is int and type(entry.get("dts")) is int and
                             all(re.fullmatch(r"[0-9a-f]{64}", entry.get(key, "")) for key in
                                 ("coded_sha256", "decoded_sha256")) for entry in entries) and
                         all(left["pts"] < right["pts"] and left["dts"] < right["dts"]
                             for left, right in zip(entries, entries[1:])),
                         "The private video restart entries are absent, malformed, or unbounded.")
            return {"probe_version": 6, "index_count": 1, "entry_count": len(entries),
                    "index_bytes": len(json.dumps(indexes).encode()), "packet_scope_checked": True,
                    "nal_scope_checked": True, "sha256": core.digest(json.dumps(indexes, sort_keys=True).encode())}

        def preservation_snapshot(self):
            # Exclude only the intentionally replaced technical index and scan
            # timestamps. Full manual state, revision, audit, hierarchy, user
            # data, associations, subtitles, and all other media facts remain.
            statement = """SELECT jsonb_build_object(
                'libraries', (SELECT jsonb_agg(to_jsonb(l) - 'last_scan_at' ORDER BY id) FROM libraries l),
                'roots', (SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
                'items', (SELECT jsonb_agg((to_jsonb(i) - 'updated_at' - 'probed_at' - 'media') ||
                    jsonb_build_object('media', CASE WHEN media IS NULL THEN 'null'::jsonb
                        ELSE media - 'VideoSeekIndexes' END) ORDER BY id) FROM items i),
                'metadata', (SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
                'entities', (SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM catalog_entities e),
                'associations', (SELECT jsonb_agg(to_jsonb(a) ORDER BY item_id, entity_id, position) FROM item_entities a),
                'subtitles', (SELECT jsonb_agg(to_jsonb(s) ORDER BY item_id, stream_index) FROM item_subtitles s),
                'images', (SELECT jsonb_agg(to_jsonb(i) ORDER BY item_id, image_type, image_index) FROM item_images i),
                'userdata', (SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id, item_id) FROM user_item_data u));"""
            return core.digest(self.owned_database_sql(statement).encode())

        def assert_preserved(self):
            core.require(self.preservation_snapshot() == self.preservation_before,
                         "A scan changed catalog identity, manual metadata, relationships, user state, or other media facts.")
            core.require(self.http_json(self.metadata_route, administrator=True) == self.metadata_before,
                         "A scan changed the administrator metadata projection.")
            core.require(self.http_json(self.item_route, headers=self.emby_headers) == self.public_before,
                         "A scan changed original client media, metadata, or user-data projections.")
            core.require({name: core.file_digest(self.movie.parent / name) for name in self.files} == self.files,
                         "A scan changed synthetic media, NFO, or subtitle bytes.")

        def bootstrap(self):
            super().bootstrap()
            control = self.http_json("/admin/v1/session", administrator=True, body={
                "Name": self.admin_name, "Password": self.admin_password,
            })
            self.control_csrf = control.get("CSRFToken")
            self.admin_id = control.get("User", {}).get("Id", "")
            core.require(self.control_csrf and re.fullmatch(r"[0-9a-f]{32}", self.admin_id),
                         "The fixture administrator did not receive a valid control session.")
            self.secrets.append(self.control_csrf)
            self.library = self.http_json("/admin/v1/libraries", administrator=True, body={
                "Name": "Media refresh " + self.run_id, "CollectionType": "movies",
                "Paths": [str(self.movie.parent)], "Scan": False,
            }, expected=(201,))["Library"]
            core.require(re.fullmatch(r"[0-9a-f]{32}", self.library.get("Id", "")), "The isolated library identity is invalid.")
            scan_route = "/admin/v1/libraries/" + self.library["Id"] + "/scan"
            self.wait_scan(self.http_json(scan_route, method="POST", administrator=True, expected=(202,))["Job"])
            self.report["initial_index"] = self.check_index(self.media())
            self.metadata_route = "/admin/v1/items/" + self.item_id + "/metadata"
            before = self.http_json(self.metadata_route, administrator=True)
            self.metadata_before = self.http_json(self.metadata_route, method="PUT", administrator=True, body={
                "Revision": before["Revision"], "Overrides": {
                    "Name": "Manual refresh title", "Overview": "Manual refresh overview",
                    "Genres": ["Manual Genre"], "Tags": ["Manual Tag"], "Studios": ["Manual Studio"],
                    "People": [{"Name": "Manual Actor", "Type": "Actor", "Role": "Guide", "SortOrder": 0}],
                }, "LockedFields": ["Overview", "Genres"],
            })
            core.require(self.metadata_before.get("Overrides") and self.metadata_before.get("LockedValues") and
                         self.metadata_before.get("LastEditedBy") == self.admin_id and self.metadata_before.get("LastEditedAt"),
                         "Nonempty manual override, lock, and audit fixtures were not created.")
            login = self.http_json("/emby/Users/AuthenticateByName", body={
                "Username": self.admin_name, "Pw": self.admin_password,
            }, headers={"Authorization": 'Emby Client="Media refresh fixture", DeviceId="refresh-' + self.run_id +
                        '", Device="Synthetic fixture", Version="m4g"'})
            token = login.get("AccessToken", "")
            core.require(isinstance(token, str) and len(token) >= 32 and login.get("User", {}).get("Id") == self.admin_id,
                         "The real fixture login failed to identify its user.")
            self.secrets.append(token)
            self.emby_headers = {"X-Emby-Token": token}
            for action in ("FavoriteItems", "PlayedItems"):
                self.http_json(f"/emby/Users/{self.admin_id}/{action}/{self.item_id}", method="POST", headers=self.emby_headers)
            self.item_route = f"/emby/Users/{self.admin_id}/Items/{self.item_id}"
            self.public_before = self.http_json(self.item_route, headers=self.emby_headers)
            state = self.public_before.get("UserData", {})
            core.require(state.get("IsFavorite") is True and state.get("Played") is True and
                         state.get("PlayCount", 0) > 0 and state.get("LastPlayedDate"),
                         "The real user-state fixture is empty.")
            core.require(len(self.public_before.get("MediaSources", [])) == 1 and
                         self.public_before["MediaSources"][0].get("Id") == "mediasource_" + self.item_id,
                         "The original client media source identity is missing.")
            public_fields = json.dumps(self.public_before)
            core.require(not any(field in public_fields for field in ("VideoSeekIndexes", "coded_sha256", "decoded_sha256")),
                         "Private refresh index evidence leaked into the client projection.")
            nonempty = json.loads(self.owned_database_sql("""SELECT jsonb_build_object(
                'manual_states', (SELECT count(*) FROM item_metadata_state WHERE overrides <> '{}'::jsonb
                    AND locked_values <> '{}'::jsonb AND last_edited_at IS NOT NULL),
                'associations', (SELECT count(*) FROM item_entities),
                'subtitles', (SELECT count(*) FROM item_subtitles WHERE active),
                'userdata', (SELECT count(*) FROM user_item_data WHERE is_favorite AND played
                    AND play_count > 0 AND last_played_at IS NOT NULL));"""))
            core.require(nonempty["manual_states"] == nonempty["subtitles"] == nonempty["userdata"] == 1 and
                         nonempty["associations"] >= 4, "The preservation comparison requires populated control rows.")
            self.report["fixtures"]["nonempty_control_rows"] = nonempty
            self.preservation_before = self.preservation_snapshot()
            # Deliberately remove only same-version optional index evidence in
            # the uniquely named disposable database. Never downgrade the probe
            # version: ordinary stale-version upgrade logic must not satisfy this.
            count = self.owned_database_sql(f"""WITH changed AS (
                UPDATE items SET media = media - 'VideoSeekIndexes'
                WHERE id = '{self.item_id}' AND library_id = '{self.library['Id']}'
                    AND media->>'ProbeVersion' = '6' AND media ? 'VideoSeekIndexes' RETURNING id)
                SELECT count(*) FROM changed;""")
            core.require(count == "1", "The owned same-version missing-index fixture was not prepared.")
            self.assert_preserved()
            for label, body in (("empty", None), ("empty_object", {})):
                self.wait_scan(self.http_json(scan_route, body=body, method="POST", administrator=True, expected=(202,))["Job"])
                media = self.media()
                core.require(media.get("ProbeVersion") == 6 and not media.get("VideoSeekIndexes"),
                             "An unchanged normal scan unexpectedly regenerated a same-version missing index.")
                self.assert_preserved()
                self.report["checks"][label + "_normal_scan_preserved_missing_index"] = True
            self.report["checks"]["same_version_missing_index_fixture"] = True
            core.private_write(self.manifest, (json.dumps({
                "Marker": FIXTURE_MARKER, "RunId": self.run_id, "Origin": self.origin,
                "Library": {"Id": self.library["Id"], "Name": self.library["Name"]},
                "ItemId": self.item_id, "InitialJobIds": self.scan_ids,
                "ResultPath": str(self.result_path),
            }, sort_keys=True) + "\n").encode())

        def browser_environment(self):
            environment = super().browser_environment()
            environment.pop("GOBY_SMOKE_USERS_DISPOSABLE_DATABASE", None)
            environment.pop("GOBY_SMOKE_USERS_DEDICATED_ADMIN", None)
            environment.update({"GOBY_SMOKE_MEDIA_REFRESH_DISPOSABLE_DATABASE": "1",
                                "GOBY_SMOKE_MEDIA_REFRESH_DEDICATED_ADMIN": "1",
                                "GOBY_SMOKE_MEDIA_REFRESH_FIXTURE_MANIFEST": str(self.manifest)})
            return environment

        def account_snapshot(self):
            # A full durable snapshot is compared immediately across restart;
            # scan jobs include every stored ForceProbe value and timestamp.
            statement = """SELECT jsonb_build_object(
                'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
                'sessions', (SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM sessions s),
                'libraries', (SELECT jsonb_agg(to_jsonb(l) ORDER BY id) FROM libraries l),
                'roots', (SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
                'items', (SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
                'metadata', (SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
                'userdata', (SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id, item_id) FROM user_item_data u),
                'jobs', (SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM scan_jobs j),
                'settings', (SELECT jsonb_agg(to_jsonb(s) ORDER BY key) FROM server_settings s));"""
            return core.digest(self.owned_database_sql(statement).encode())

        def verify_restart(self):
            result = json.loads(core.private_file(self.result_path))
            core.require(result.get("Marker") == FIXTURE_MARKER and result.get("RunId") == self.run_id and
                         result.get("CancelPosts") == 0 and result.get("ScanPostCount") == 2 and
                         result.get("NormalForceProbe") is False and result.get("RefreshForceProbe") is True and
                         result.get("MobileOverflow") is False,
                         "The browser did not publish the exact refresh acceptance contract.")
            jobs = self.jobs()
            expected = set(self.scan_ids + [result.get("NormalJobId"), result.get("RefreshJobId")])
            core.require(len(expected) == 5 and len(jobs) == 5 and {job["Id"] for job in jobs} == expected and
                         all(job["LibraryId"] == self.library["Id"] and job["Status"] == "completed" and
                             job["Error"] == "" and job["Scanned"] == 1 and job["FinishedAt"] and
                             job["ForceProbe"] is (job["Id"] == result["RefreshJobId"]) for job in jobs),
                         "The fixture jobs did not preserve their exact normal/forced modes and terminal states.")
            self.report["refreshed_index"] = self.check_index(self.media())
            self.assert_preserved()
            self.report["browser_contract"] = result
            before = self.account_snapshot()
            self.stop_app()
            self.start_app()
            core.require(core.request_json(self.origin, "/admin/v1/bootstrap") == {"Initialized": True},
                         "The refresh restart lost its initialized state.")
            core.require(self.account_snapshot() == before, "Restart changed durable refresh jobs, modes, catalog, or accounts.")
            core.require(self.jobs() == jobs and self.check_index(self.media()) == self.report["refreshed_index"],
                         "Restart changed the acknowledged job modes or regenerated video index.")
            self.assert_preserved()
            self.report["jobs"] = [{key: job[key] for key in ("Id", "ForceProbe", "Status", "Scanned", "Added", "Updated")}
                                   for job in jobs]
            self.report["checks"].update({
                "forced_refresh_restored_same_version_index": True, "nonempty_manual_overrides_and_locks_preserved": True,
                "metadata_revision_audit_entities_and_subtitles_preserved": True, "real_user_favorite_and_play_history_preserved": True,
                "item_hierarchy_and_original_media_source_preserved": True, "original_media_and_sidecar_bytes_unchanged": True,
                "all_five_jobs_and_modes_survived_restart": True, "restart_preserved_complete_durable_snapshot": True,
            })
            self.report["limitations"] = [
                "The refresh fixture covers one supported H.264/AAC file; unsupported formats may complete without indexes.",
                "Index existence and bounded persisted evidence are checked; this scenario does not assert runtime fast-seek speed.",
            ]

    return MediaRefreshRunner(args)


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
        raise core.VerificationError("The refresh verifier was interrupted; owned resources are being cleaned up.")
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, interrupted)
    return create_runner(core, args).execute()


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"status": "failed", "failure": "Refresh verifier setup failed: " + type(error).__name__}))
        sys.exit(1)
