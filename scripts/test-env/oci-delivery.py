#!/usr/bin/env python3
"""Verify one explicitly provisioned Linux amd64 OCI installation on test-env.

Private JSON requires base_url, control_script, movie_root, tv_root, sample_path,
output_dir, ffmpeg_path, ffprobe_path, setup_token, admin_name, admin_password,
and backup_passphrase. Optional: timeout_seconds (600), expected_movies (2),
expected_episodes (3), and image_upgrade (false). Paths to tools, the sample and
output directory are host paths; library roots are container paths.

The external controller owns provisioning and accepts start, stop, recreate,
status, analysis-evidence, and optionally upgrade and rollback-image. Status
returns running, pid, image_id, exit_code, process_pids, master_key_sha256,
uid, read_only_root, media_read_only and root_write_rejected. Analysis evidence
returns items with item_id, audio_samples and visual_samples, read from the
owned database's GAFB feature headers. No credentials enter this driver's output.
The driver stops its installation on success or failure; databases and evidence
remain available for the provisioning owner to close and retain.
"""

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import struct
import subprocess
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid


spec = importlib.util.spec_from_file_location(
    "phase3_cli_recovery", Path(__file__).with_name("phase3-cli-supervisor-recovery.py"))
cli = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cli)
need, emit, Failure = cli.need, cli.emit, cli.Failure


def load_config(filename):
    path = Path(filename)
    need(path.is_absolute(), "config_must_be_absolute")
    metadata = path.lstat()
    need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
         and 0 < metadata.st_size <= 16384, "config_must_be_a_bounded_private_regular_file")
    value = json.loads(path.read_bytes())
    required = {"base_url", "control_script", "movie_root", "tv_root", "sample_path", "output_dir",
                "ffmpeg_path", "ffprobe_path", "setup_token", "admin_name", "admin_password", "backup_passphrase"}
    optional = {"timeout_seconds", "expected_movies", "expected_episodes", "image_upgrade"}
    need(type(value) is dict and required <= value.keys() <= required | optional, "invalid_config_fields")
    need(all(type(value[key]) is str and value[key] for key in required), "invalid_config_strings")
    origin = urllib.parse.urlsplit(value["base_url"])
    need(origin.scheme == "http" and origin.hostname in {"127.0.0.1", "::1", "localhost"}
         and origin.port is not None and origin.username is None and origin.password is None
         and origin.path in {"", "/"} and not origin.query and not origin.fragment,
         "base_url_must_be_explicit_loopback_http")
    value["base_url"] = value["base_url"].rstrip("/")
    for key in ("control_script", "movie_root", "tv_root", "sample_path", "output_dir", "ffmpeg_path", "ffprobe_path"):
        need(Path(value[key]).is_absolute(), key + "_must_be_absolute")
    for key in ("control_script", "ffmpeg_path", "ffprobe_path"):
        need(Path(value[key]).is_file() and os.access(value[key], os.X_OK), key + "_must_be_executable")
    need(Path(value["output_dir"]).is_dir(), "output_directory_must_exist")
    need(Path(value["sample_path"]).is_file() and 1024 < Path(value["sample_path"]).stat().st_size < 32 << 20,
         "sample_must_be_a_bounded_existing_media_file")
    value.setdefault("timeout_seconds", 600)
    value.setdefault("expected_movies", 2)
    value.setdefault("expected_episodes", 3)
    value.setdefault("image_upgrade", False)
    need(type(value["timeout_seconds"]) is int and 30 <= value["timeout_seconds"] <= 1800, "invalid_timeout")
    need(all(type(value[key]) is int and 1 <= value[key] <= 25
             for key in ("expected_movies", "expected_episodes")), "invalid_fixture_population")
    need(type(value["image_upgrade"]) is bool, "invalid_image_upgrade_selection")
    need(len(value["backup_passphrase"].encode()) >= 24, "backup_passphrase_too_short")
    return value


class Delivery(cli.Driver):
    def __init__(self, config):
        super().__init__(config)
        self.libraries = {}
        self.episode_ids = []
        self.initial_identity = None
        self.original_application_key_id = None
        self.application_key_id = None
        self.application_key_sha256 = None
        self.output = Path(config["output_dir"])

    def raw_request(self, method, path, body=None, admin=False, public=False, timeout=10):
        status, content = super().raw_request(method, path, body, admin, public, timeout)
        if status >= 400:
            code = "unavailable"
            try:
                candidate = json.loads(content).get("Error", {}).get("Code")
                if (type(candidate) is str and 0 < len(candidate) <= 64
                        and all(character in "abcdefghijklmnopqrstuvwxyz0123456789_" for character in candidate)):
                    code = candidate
            except (AttributeError, ValueError, UnicodeError):
                pass
            emit("http_error", phase=self.phase, method=method, route=path.split("?", 1)[0],
                 status=status, code=code)
        return status, content

    def control(self, operation):
        need(operation in {"start", "stop", "recreate", "status", "analysis-evidence", "upgrade", "rollback-image"},
             "unsupported_oci_control_operation")
        result = subprocess.run([self.config["control_script"], operation], stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=self.timeout, check=False)
        need(result.returncode == 0, "oci_control_failed_" + operation)
        if operation not in {"status", "analysis-evidence"}:
            emit("container_control", phase=self.phase, operation=operation)
            return None
        need(len(result.stdout) <= 65536, "control_result_too_large")
        value = json.loads(result.stdout)
        need(type(value) is dict, "invalid_control_result")
        if operation == "status":
            fields = {"running", "pid", "image_id", "exit_code", "process_pids", "master_key_sha256",
                      "uid", "read_only_root", "media_read_only", "root_write_rejected"}
            need(fields <= value.keys() and type(value["running"]) is bool
                 and type(value["pid"]) is int and type(value["process_pids"]) is list,
                 "incomplete_container_status")
        return value

    def ready(self):
        def check():
            state = self.control("status")
            code, _ = self.raw_request("GET", "/readyz", timeout=3)
            return state if state["running"] and state["pid"] > 0 and code == 200 else None
        state = self.wait("container_ready", check)
        need(state["uid"] == 10001 and state["read_only_root"] is True
             and state["media_read_only"] is True and state["root_write_rejected"] is True,
             "selected_container_isolation_not_effective")
        if self.initial_identity is not None:
            need(type(state["master_key_sha256"]) is str and len(state["master_key_sha256"]) == 64,
                 "persistent_master_key_not_observed")
            need(state["master_key_sha256"] == self.initial_identity["master_key_sha256"],
                 "persistent_master_key_changed")
        emit("container_ready", phase=self.phase, pid=state["pid"], image_id=state["image_id"],
             uid=state["uid"], read_only_root=True, media_read_only=True)
        return state

    def authenticate(self):
        self.cookies.clear()
        login = self.request("POST", "/admin/v1/session", {
            "Name": self.config["admin_name"], "Password": self.config["admin_password"]})
        self.csrf = login["CSRFToken"]
        login = self.request("POST", "/emby/Users/AuthenticateByName", {
            "Username": self.config["admin_name"], "Pw": self.config["admin_password"]})
        self.token = login["AccessToken"]
        need(self.user_id is None or self.user_id == login["User"]["Id"], "administrator_identity_changed")
        self.user_id = login["User"]["Id"]

    def issue_application_key(self):
        issued = self.request("POST", "/admin/v1/api-keys", {"AppName": "OCI delivery persistence"},
                              expected=201, admin=True)
        need(issued["Key"]["Status"] == "active" and type(issued["AccessToken"]) is str
             and issued["AccessToken"], "application_key_was_not_issued")
        self.application_key_id = issued["Key"]["Id"]
        if self.original_application_key_id is None:
            self.original_application_key_id = self.application_key_id
        self.application_key_sha256 = hashlib.sha256(issued["AccessToken"].encode()).hexdigest()
        # The vault creates its master key lazily when the first credential is
        # sealed. Readiness before bootstrap does not imply a key file exists.
        state = self.control("status")
        need(type(state["master_key_sha256"]) is str and len(state["master_key_sha256"]) == 64,
             "issued_application_key_has_no_persistent_master_key")
        if self.initial_identity is None:
            self.initial_identity = state
        else:
            need(state["master_key_sha256"] == self.initial_identity["master_key_sha256"],
                 "persistent_master_key_changed")
        emit("application_key_issued", phase=self.phase, recoverable=True, persistent_master_key=True)

    def assert_original_key_revoked(self):
        # Full restore and rollback retain encrypted credential history while
        # revoking imported sessions. Ordinary container recreation does not.
        page = self.request("GET", "/admin/v1/api-keys?IncludeRevoked=true", admin=True)
        matches = [key for key in page["Items"] if key["Id"] == self.original_application_key_id]
        need(len(matches) == 1 and matches[0]["Status"] == "revoked" and matches[0]["RevokedAt"] is not None,
             "recovered_original_application_key_was_not_revoked")
        rejected = self.request("POST", "/admin/v1/api-keys/" + self.original_application_key_id + "/reveal",
                                {}, expected=409, admin=True)
        need(rejected["Error"]["Code"] == "key_revoked", "recovered_key_reveal_failed_for_the_wrong_reason")
        emit("recovered_application_key_revoked", phase=self.phase, metadata_retained=True, reveal_status=409)

    def save(self, name, content):
        path = self.output / name
        with path.open("xb") as output:
            os.chmod(path, 0o600)
            output.write(content)
        return path

    def binary_request(self, path, expected=200, byte_range=None):
        parsed = urllib.parse.urlsplit(path)
        need(not parsed.scheme and not parsed.netloc and path.startswith("/") and not path.startswith("//"),
             "media_url_must_stay_on_the_owned_origin")
        headers = {"X-Emby-Token": self.token, "Origin": self.config["base_url"]}
        if byte_range:
            headers["Range"] = byte_range
        request = urllib.request.Request(self.config["base_url"] + path, headers=headers)
        with self.http.open(request, timeout=self.timeout) as response:
            content = response.read((32 << 20) + 1)
            need(response.status == expected and len(content) <= 32 << 20, "unexpected_media_response")
            return content, response.headers

    def scan_complete(self, job_id):
        def check():
            job = self.scan_job(job_id)
            if job["Status"] in {"pending", "running"}:
                return None
            need(job["Status"] == "completed" and not job["Error"], "fixture_scan_failed")
            return job
        return self.wait("fixture_scan_complete", check)

    def catalog_for(self, kind):
        query = urllib.parse.urlencode({"ParentId": self.libraries[kind], "Recursive": "true",
                                        "IncludeItemTypes": kind, "Limit": 100})
        page = self.request("GET", "/emby/Items?" + query, public=True)
        expected = self.config["expected_movies" if kind == "Movie" else "expected_episodes"]
        need(page["TotalRecordCount"] == expected and len(page["Items"]) == expected,
             "catalog_population_changed_" + kind)
        result = {item["Name"]: item["Id"] for item in page["Items"]}
        need(len(result) == expected and len(set(result.values())) == expected, "catalog_identity_not_unique")
        return result

    def set_settings(self, name):
        current = self.request("GET", "/admin/v1/settings", admin=True)
        overrides = dict(current["Overrides"])
        overrides["ServerName"] = name
        return self.request("PUT", "/admin/v1/settings", {
            "Revision": current["Revision"], "Overrides": overrides}, admin=True)

    def assert_persistent(self, userdata=None, settings=None):
        userdata = self.user_data if userdata is None else userdata
        settings = self.settings if settings is None else settings
        need({kind: self.catalog_for(kind) for kind in self.libraries} == self.baseline, "catalog_ids_changed")
        need(self.request("GET", self.user_data_path(), public=True) == userdata, "userdata_changed")
        current = self.request("GET", "/admin/v1/settings", admin=True)
        need(current["Overrides"] == settings["Overrides"]
             and current["Effective"]["ServerName"] == settings["Effective"]["ServerName"], "settings_changed")
        revealed = self.request("POST", "/admin/v1/api-keys/" + self.application_key_id + "/reveal", {}, admin=True)
        need(revealed["Id"] == self.application_key_id
             and hashlib.sha256(revealed["AccessToken"].encode()).hexdigest() == self.application_key_sha256,
             "recoverable_application_key_changed")

    def initialize(self):
        need(self.request("GET", "/admin/v1/bootstrap")["Initialized"] is False, "fresh_database_required")
        self.request("POST", "/admin/v1/bootstrap", {"SetupToken": self.config["setup_token"],
            "Name": self.config["admin_name"], "Password": self.config["admin_password"]}, expected=201)
        self.authenticate()
        self.issue_application_key()
        dashboard = self.request("GET", "/admin/", binary=True)
        need(b"<html" in dashboard.lower() and b"/assets/" in dashboard, "embedded_dashboard_missing")
        for kind, collection, root in (("Movie", "movies", "movie_root"), ("Episode", "tvshows", "tv_root")):
            value = self.request("POST", "/admin/v1/libraries", {"Name": "OCI " + kind,
                "CollectionType": collection, "Paths": [self.config[root]], "Scan": True}, expected=201, admin=True)
            self.libraries[kind] = value["Library"]["Id"]
            self.scan_complete(value["Job"]["Id"])
        self.baseline = {kind: self.catalog_for(kind) for kind in self.libraries}
        self.sample_id = self.baseline["Movie"].get(Path(self.config["sample_path"]).stem)
        need(self.sample_id is not None, "sample_filename_does_not_match_catalog_movie")
        self.episode_ids = list(self.baseline["Episode"].values())
        self.request("POST", self.user_data_path(), {"PlaybackPositionTicks": 5_000_000,
            "PlayCount": 7, "Played": False, "IsFavorite": True, "LastPlayedDate": "2026-09-01T03:04:05Z"}, public=True)
        self.user_data = self.request("GET", self.user_data_path(), public=True)
        self.settings = self.set_settings("OCI delivery baseline")
        self.assert_persistent()
        emit("baseline_verified", movies=len(self.baseline["Movie"]), episodes=len(self.episode_ids),
             embedded_dashboard=True, catalog_ids=True, userdata=True, settings=True)

    def inspect_media(self, path, width=None, audio=True):
        probe = subprocess.run([self.config["ffprobe_path"], "-v", "error", "-show_streams", "-of", "json", str(path)],
                               stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               timeout=self.timeout, check=False)
        need(probe.returncode == 0, "downloaded_media_probe_failed")
        streams = json.loads(probe.stdout)["streams"]
        video = [stream for stream in streams if stream["codec_type"] == "video"]
        need(len(video) == 1 and (width is None or video[0]["width"] == width), "output_video_dimensions_changed")
        if audio:
            need(video[0]["codec_name"] == "h264"
                 and any(stream["codec_type"] == "audio" and stream["codec_name"] == "aac" for stream in streams),
                 "output_is_not_h264_aac")
        decoded = subprocess.run([self.config["ffmpeg_path"], "-hide_banner", "-nostdin", "-v", "error", "-xerror",
                                  "-threads", "1", "-i", str(path), "-f", "null", "-"], stdin=subprocess.DEVNULL,
                                 stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=self.timeout, check=False)
        need(decoded.returncode == 0, "downloaded_media_decode_failed")

    def playback(self):
        original = Path(self.config["sample_path"]).read_bytes()
        route = "/emby/Videos/" + self.sample_id + "/stream.mp4?Static=true"
        actual, _ = self.binary_request(route)
        need(actual == original, "direct_media_bytes_changed")
        partial, headers = self.binary_request(route, expected=206, byte_range="bytes=0-1023")
        need(partial == original[:1024] and headers.get("Content-Range") == "bytes 0-1023/" + str(len(original)),
             "direct_range_is_incorrect")
        for mode in ("remux", "transcode"):
            profile = {"Type": "Video", "Container": "mp4", "Protocol": "http", "Context": "Streaming",
                       "VideoCodec": "h264", "AudioCodec": "aac"}
            if mode == "transcode":
                profile.update(MaxWidth=96, MaxHeight=54)
            body = {"EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": True,
                    "AllowVideoStreamCopy": mode == "remux", "AllowAudioStreamCopy": mode == "remux",
                    "DeviceProfile": {"TranscodingProfiles": [profile]}}
            playback = self.request("POST", "/emby/Items/" + self.sample_id + "/PlaybackInfo", body, public=True)
            need(not playback.get("ErrorCode") and len(playback["MediaSources"]) == 1, "playback_negotiation_failed")
            uri = playback["MediaSources"][0]["TranscodingUrl"]
            query = urllib.parse.parse_qs(urllib.parse.urlsplit(uri).query)
            expected_video, expected_audio = ("copy", "copy") if mode == "remux" else ("h264", "aac")
            need(query.get("VideoCodec") == [expected_video] and query.get("AudioCodec") == [expected_audio],
                 "playback_did_not_select_" + mode)
            content, _ = self.binary_request(uri)
            path = self.save(mode + ".mp4", content)
            self.inspect_media(path, width=96 if mode == "transcode" else None)
            stop = "/emby/Videos/ActiveEncodings?" + urllib.parse.urlencode({
                "DeviceId": "phase3-cli-recovery", "PlaySessionId": playback["PlaySessionId"]})
            code, _ = self.raw_request("DELETE", stop, public=True)
            need(code == 204, "playback_stop_failed")
            emit("playback_verified", mode=mode, bytes=len(content), sha256=hashlib.sha256(content).hexdigest(), decoded=True)
        emit("direct_media_verified", bytes=len(original), range_bytes=len(partial), sha256=hashlib.sha256(original).hexdigest())

    def analysis_run(self, kind, item_ids):
        admitted = self.request("POST", "/admin/v1/media-analysis/runs", {"Kind": kind,
            "RequestId": "oci-" + uuid.uuid4().hex, "LibraryIds": [], "ItemIds": item_ids, "Force": True}, expected=202, admin=True)
        def check():
            task = self.preview_run(admitted["RunId"])
            if task["Run"]["State"] in {"pending", "running"}:
                return None
            need(task["Run"]["State"] == "completed" and all(child["State"] == "completed" and not child["ErrorCode"]
                 for child in task["Children"]["Items"]), "analysis_run_failed_" + kind)
            return task
        self.wait("analysis_complete_" + kind, check)

    def preview(self, label):
        self.analysis_run("previews", [self.sample_id])
        content, _ = self.binary_request("/emby/Videos/" + self.sample_id + "/index.bif?Width=240")
        need(len(content) >= 88 and content[:8] == b"\x89BIF\r\n\x1a\n", "bif_header_invalid")
        version, count, multiplier = struct.unpack_from("<III", content, 8)
        need(version == 0 and 2 <= count <= 4096 and multiplier > 0 and 64 + 8 * (count + 1) < len(content),
             "bif_index_invalid")
        entries = [struct.unpack_from("<II", content, 64 + 8 * index) for index in range(count + 1)]
        need(entries[-1] == (0xffffffff, len(content))
             and all(entries[index][0] < entries[index + 1][0] and entries[index][1] < entries[index + 1][1]
                     for index in range(count)), "bif_index_not_monotonic")
        for index in sorted({0, count // 2, count - 1}):
            jpeg = content[entries[index][1]:entries[index + 1][1]]
            need(jpeg.startswith(b"\xff\xd8") and jpeg.endswith(b"\xff\xd9"), "bif_frame_not_jpeg")
            self.inspect_media(self.save(label + "-frame-" + str(index) + ".jpg", jpeg), width=240, audio=False)
        self.save(label + ".bif", content)
        emit("bif_verified", phase=self.phase, frames=count, sampled_frames=3, bytes=len(content), decoded=True)

    def analysis(self):
        overview = self.request("GET", "/admin/v1/media-analysis", admin=True)
        need(overview["Runtime"]["IntroAvailable"] is True and overview["Runtime"]["PreviewAvailable"] is True,
             "container_analysis_inventory_unavailable")
        profile = dict(overview["Configuration"]["Profile"])
        profile["PreviewIntervalSeconds"] = 2
        self.request("PUT", "/admin/v1/media-analysis/configuration", {
            "Revision": overview["Configuration"]["Revision"], "Profile": profile}, admin=True)
        self.preview("initial")
        self.analysis_run("intro", self.episode_ids)
        evidence = self.control("analysis-evidence")["items"]
        need(len(evidence) == len(self.episode_ids) and {row["item_id"] for row in evidence} == set(self.episode_ids)
             and all(row["audio_samples"] > 0 and row["visual_samples"] > 0 for row in evidence),
             "native_intro_features_not_persisted")
        for item_id in self.episode_ids:
            detection = self.request("GET", "/admin/v1/media-analysis/items/" + item_id, admin=True)["Detection"]
            need(detection["Status"] in {"qualified", "review", "no_result"}, "intro_lacks_completed_result")
            emit("intro_verified", item_id=item_id, status=detection["Status"], reasons=detection["Reasons"])
        emit("intro_features_verified", items=evidence, accuracy_claim=False)

    def operation(self, operation_id, expected="completed"):
        def check():
            value = self.request("GET", "/admin/v1/backup-operations/" + operation_id, admin=True)["Operation"]
            need(value["State"] not in {"failed", "cancelled", "interrupted"}, "backup_operation_failed_" + value["ErrorCode"])
            return value if value["State"] == expected else None
        return self.wait("backup_operation_" + expected, check)

    def await_generation(self, previous):
        def check():
            code, content = self.raw_request("GET", "/admin/v1/backups/status", admin=True)
            if code == 401:
                self.authenticate()
                return None
            if code in {0, 503}:
                return None
            need(code == 200, "recovery_status_unexpected")
            value = json.loads(content)
            return value if int(value["GenerationRevision"]) > int(previous) and not value["Busy"] else None
        value = self.wait("activated_database_generation", check)
        self.authenticate()
        return value

    def backup_restore(self):
        before = self.request("GET", "/admin/v1/backups/status", admin=True)
        need(before["Available"] and before["RestoreAvailable"] and not before["Busy"], "isolated_recovery_unavailable")
        op = self.request("POST", "/admin/v1/backups", {"RequestId": uuid.uuid4().hex,
            "Passphrase": self.config["backup_passphrase"]}, expected=202, admin=True)["Operation"]
        op = self.operation(op["Id"])
        backup = self.request("GET", "/admin/v1/backups/" + op["BackupId"], admin=True)["Backup"]
        archive = self.request("GET", "/admin/v1/backups/" + backup["Id"] + "/file", admin=True, binary=True)
        need(archive.startswith(b"age-encryption.org/v1\n") and hashlib.sha256(archive).hexdigest() == backup["SHA256"]
             and len(archive) == int(backup["SizeBytes"]), "encrypted_backup_download_invalid")
        self.save("backup.age", archive)
        self.request("POST", self.user_data_path(), {"PlaybackPositionTicks": 8_000_000, "PlayCount": 9,
            "Played": False, "IsFavorite": False, "LastPlayedDate": "2026-09-02T03:04:05Z"}, public=True)
        rollback_userdata = self.request("GET", self.user_data_path(), public=True)
        rollback_settings = self.set_settings("OCI rollback marker")
        plan = self.request("POST", "/admin/v1/restores/plans", {"RequestId": uuid.uuid4().hex,
            "BackupId": backup["Id"], "SHA256": backup["SHA256"], "Passphrase": self.config["backup_passphrase"],
            "RestoreDefaults": True, "ReplaceRollback": False, "GenerationRevision": before["GenerationRevision"]},
            expected=202, admin=True)["Operation"]
        plan = self.operation(plan["Id"], "ready")
        self.request("POST", "/admin/v1/restores/" + plan["Id"] + "/apply", {
            "Revision": plan["Revision"], "GenerationRevision": before["GenerationRevision"]}, expected=202, admin=True)
        restored = self.await_generation(before["GenerationRevision"])
        self.operation(plan["Id"])
        self.assert_original_key_revoked()
        self.issue_application_key()
        self.assert_persistent()
        self.recreate()
        self.preview("restored")
        need(restored["Rollback"]["Available"], "rollback_copy_not_retained")
        rollback = self.request("POST", "/admin/v1/restores/rollback", {"RequestId": uuid.uuid4().hex,
            "GenerationRevision": restored["GenerationRevision"]}, expected=202, admin=True)["Operation"]
        rolled_back = self.await_generation(restored["GenerationRevision"])
        self.operation(rollback["Id"])
        self.assert_original_key_revoked()
        self.issue_application_key()
        self.assert_persistent(rollback_userdata, rollback_settings)
        self.user_data, self.settings = rollback_userdata, rollback_settings
        emit("encrypted_recovery_verified", backup_bytes=len(archive), backup_sha256=backup["SHA256"],
             restore_generation=restored["GenerationRevision"], rollback_generation=rolled_back["GenerationRevision"],
             catalog_ids=True, restored_userdata=True, rollback_userdata=True, container_recreated=True)

    def stopped(self):
        state = self.control("status")
        return state if not state["running"] and state["pid"] == 0 and not state["process_pids"] else None

    def recreate(self, operation="recreate", changed_image=False):
        previous = self.control("status")
        self.control("stop")
        closed = self.wait("container_process_closure", self.stopped)
        need(closed["exit_code"] == 0, "container_sigterm_was_not_clean")
        self.control(operation)
        current = self.ready()
        need(current["pid"] != previous["pid"], "container_process_was_not_replaced")
        need((current["image_id"] != previous["image_id"]) == changed_image, "container_image_transition_mismatch")
        self.authenticate()
        self.assert_persistent()
        emit("container_replacement_verified", operation=operation, changed_image=changed_image,
             previous_image=previous["image_id"], current_image=current["image_id"], clean_exit=True, durable_state=True)

    def execute(self):
        self.phase = "bootstrap_and_media"
        self.control("start")
        self.ready()
        self.initialize()
        self.playback()
        self.analysis()
        self.completed.append(self.phase)
        self.phase = "graceful_recreate"
        self.recreate()
        self.completed.append(self.phase)
        self.phase = "encrypted_backup_restore_restart_rollback"
        self.backup_restore()
        self.completed.append(self.phase)
        if self.config["image_upgrade"]:
            for phase, operation in (("image_upgrade", "upgrade"), ("image_rollback", "rollback-image")):
                self.phase = phase
                self.recreate(operation, changed_image=True)
                self.completed.append(self.phase)
        self.phase = "final_persistence"
        self.recreate()
        self.completed.append(self.phase)

    def close(self):
        self.phase = "closure"
        self.control("stop")
        state = self.wait("container_final_closure", self.stopped)
        need(state["exit_code"] == 0, "final_container_sigterm_was_not_clean")
        code, _ = self.raw_request("GET", "/readyz", timeout=2)
        need(code == 0, "owned_http_listener_remains_open")
        emit("closed", running=False, pid=0, child_processes=0, exit_code=state["exit_code"], http_listener_closed=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    args = parser.parse_args()
    driver, failure, failed_phase = None, None, None

    def interrupted(_signum, _frame):
        raise Failure("driver_interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        driver = Delivery(load_config(args.config))
        driver.execute()
    except Exception as error:
        failure = str(error) if isinstance(error, Failure) else "unexpected_" + type(error).__name__
        failed_phase = None if driver is None else driver.phase
    finally:
        if driver is not None:
            try:
                driver.close()
            except Exception as error:
                emit("closure_failed", reason=str(error) if isinstance(error, Failure) else type(error).__name__)
                failure = failure or "closure_failed"
    emit("result", passed=failure is None, failure=failure, failed_phase=failed_phase,
         completed=[] if driver is None else driver.completed,
         image_upgrade_selected=False if driver is None else driver.config["image_upgrade"],
         scope="linux_amd64_software_oci_functional_delivery; no_hardware_capacity_or_registry_publication_claim")
    return 0 if failure is None else 1


if __name__ == "__main__":
    sys.exit(main())
