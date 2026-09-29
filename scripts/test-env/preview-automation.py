#!/usr/bin/env python3
"""Accept library-driven BIF generation on one explicitly owned test deployment.

Required private JSON: base_url, control_script, setup_token, admin_name,
admin_password, target_movie_root, disabled_movie_root, output_dir, ffmpeg_path,
ffprobe_path. The two disjoint container roots each contain exactly one movie.
Tool and output paths are host paths. Optional: allow_existing_admin (false),
timeout_seconds (600), quiet_seconds (3), target_interval_seconds (20).

The root-owned executable controller accepts start, recreate, stop, status.
Status returns running (bool), pid (int), optionally process_pids (int[]).
Only this exact controller owns the deployment and database. No service is
inferred, and every invocation stops the application on success or failure.

--phase all runs enable, rescan, configuration, disable in order. Individual
phases reuse preview-automation-state.json in output_dir. Completed phases are
not repeated. A retained pending phase resumes its observed job/run; a missing
scan receipt is reported for investigation instead of blindly scanning again.

Only library options, ordinary scans, and a real interval configuration change
trigger analysis. Manual analysis/task starts and Force/ForceProbe are forbidden.
The driver verifies all BIF frames structurally, decodes first/middle/last JPEGs
with the explicitly selected FFmpeg tools, follows ThumbnailSet image tags,
checks HTTP ranges, and compares complete retained bytes across reuse/disable.
Failed work is reported with safe task errors, never as a successful empty set.
No GPU or broad performance claim is made.
"""

import argparse
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import struct
import sys
import time
import urllib.parse


spec = importlib.util.spec_from_file_location("intro_automation", Path(__file__).with_name("intro-automation.py"))
intro = importlib.util.module_from_spec(spec)
spec.loader.exec_module(intro)
need, emit, Failure = intro.need, intro.emit, intro.Failure
PHASES = ("enable", "rescan", "configuration", "disable")
WIDTHS = (240, 320, 400)
TICKS_PER_SECOND = 10_000_000


def load_config(filename):
    path = Path(filename)
    need(path.is_absolute(), "config_path_must_be_absolute")
    metadata = path.lstat()
    need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
         and 0 < metadata.st_size <= 16384, "config_must_be_a_bounded_private_regular_file")
    value = json.loads(path.read_bytes())
    required = {"base_url", "control_script", "setup_token", "admin_name", "admin_password",
                "target_movie_root", "disabled_movie_root", "output_dir", "ffmpeg_path", "ffprobe_path"}
    optional = {"allow_existing_admin", "timeout_seconds", "quiet_seconds", "target_interval_seconds"}
    need(type(value) is dict and required <= value.keys() <= required | optional, "invalid_config_fields")
    need(all(type(value[key]) is str and value[key] for key in required), "invalid_config_strings")
    origin = urllib.parse.urlsplit(value["base_url"])
    need(origin.scheme == "http" and origin.hostname in {"127.0.0.1", "::1", "localhost"}
         and origin.port is not None and origin.username is None and origin.password is None
         and origin.path in {"", "/"} and not origin.query and not origin.fragment,
         "base_url_must_be_explicit_loopback_http")
    value["base_url"] = value["base_url"].rstrip("/")
    for key in ("control_script", "ffmpeg_path", "ffprobe_path"):
        executable = Path(value[key])
        need(executable.is_absolute() and executable.is_file() and os.access(executable, os.X_OK),
             key + "_must_be_an_absolute_executable")
    controller = Path(value["control_script"]).stat()
    need(controller.st_uid == 0 and controller.st_mode & 0o022 == 0,
         "controller_must_be_root_owned_without_group_or_other_write")
    output = Path(value["output_dir"])
    need(output.is_absolute() and output.is_dir(), "output_directory_must_exist_and_be_absolute")
    roots = [PurePosixPath(value[key]) for key in ("target_movie_root", "disabled_movie_root")]
    need(all(root.is_absolute() and ".." not in root.parts for root in roots)
         and not roots[0].is_relative_to(roots[1]) and not roots[1].is_relative_to(roots[0]),
         "fixture_roots_must_be_absolute_and_disjoint")
    value.setdefault("allow_existing_admin", False)
    value.setdefault("timeout_seconds", 600)
    value.setdefault("quiet_seconds", 3)
    value.setdefault("target_interval_seconds", 20)
    need(type(value["allow_existing_admin"]) is bool, "invalid_existing_admin_flag")
    need(type(value["timeout_seconds"]) is int and 30 <= value["timeout_seconds"] <= 3600,
         "invalid_timeout_seconds")
    need(type(value["quiet_seconds"]) is int and 1 <= value["quiet_seconds"] <= 15,
         "invalid_quiet_seconds")
    need(type(value["target_interval_seconds"]) is int and 2 <= value["target_interval_seconds"] <= 120,
         "invalid_target_interval")
    return value


def jpeg_dimensions(content):
    need(content.startswith(b"\xff\xd8") and content.endswith(b"\xff\xd9"), "invalid_jpeg_envelope")
    position = 2
    while position < len(content) - 2:
        need(content[position] == 0xff, "invalid_jpeg_marker")
        while position < len(content) and content[position] == 0xff:
            position += 1
        need(position < len(content), "truncated_jpeg_marker")
        marker = content[position]
        position += 1
        need(marker not in {0x00, 0xd8, 0xd9, 0xda}, "jpeg_dimensions_missing_before_scan")
        if marker == 0x01 or 0xd0 <= marker <= 0xd7:
            continue
        need(position + 2 <= len(content), "truncated_jpeg_segment")
        size = struct.unpack_from(">H", content, position)[0]
        need(size >= 2 and position + size <= len(content), "invalid_jpeg_segment_length")
        if marker in {0xc0, 0xc1, 0xc2}:
            need(size >= 8 and content[position + 2] == 8, "unsupported_jpeg_dimensions")
            height, width = struct.unpack_from(">HH", content, position + 3)
            need(width > 0 and height > 0, "empty_jpeg_dimensions")
            return width, height
        position += size
    raise Failure("jpeg_dimensions_missing")


def parse_bif(content):
    need(len(content) >= 72 and content[:8] == b"\x89BIF\r\n\x1a\n", "invalid_bif_magic")
    version, count, multiplier = struct.unpack_from("<III", content, 8)
    need(version == 0 and count <= 4096 and not any(content[20:64]), "invalid_bif_header")
    end = 64 + 8 * (count + 1)
    need(end <= len(content), "bif_index_is_truncated")
    entries = [struct.unpack_from("<II", content, 64 + 8 * index) for index in range(count + 1)]
    need(entries[-1] == (0xffffffff, len(content)) and entries[0][1] == end,
         "invalid_bif_offsets_or_sentinel")
    need(all(entries[index][0] < entries[index + 1][0] and entries[index][1] < entries[index + 1][1]
             for index in range(count)), "bif_index_is_not_monotonic")
    effective_multiplier = multiplier or 1000
    ticks = [entry[0] * effective_multiplier * 10000 for entry in entries[:-1]]
    frames = [content[entries[index][1]:entries[index + 1][1]] for index in range(count)]
    return {"frames": frames, "ticks": ticks, "multiplier": effective_multiplier,
            "dimensions": [jpeg_dimensions(frame) for frame in frames]}


class PreviewAutomation(intro.IntroAutomation):
    """Share authenticated HTTP and exact controller lifecycle with intro acceptance."""

    def __init__(self, config):
        super().__init__(config)
        self.checkpoint_path = self.output / "preview-automation-state.json"
        self.task_id = None

    def raw_request(self, method, path, body=None, admin=False, public=False, timeout=10):
        route = path.split("?", 1)[0]
        if method != "GET":
            profile_update = method == "PUT" and route == "/admin/v1/media-analysis/configuration"
            need(profile_update or not route.startswith(("/admin/v1/media-analysis", "/admin/v1/tasks/",
                 "/admin/v1/task-runs/")), "manual_analysis_or_task_start_is_prohibited")
            need(not route.endswith(("/intro", "/decision")), "manual_intro_write_is_prohibited")

        def no_force(value):
            if type(value) is dict:
                return all(key.lower() not in {"force", "forceprobe"} and no_force(child)
                           for key, child in value.items())
            return type(value) is not list or all(no_force(child) for child in value)
        need(no_force(body), "force_option_is_prohibited")
        # Intro acceptance forbids global configuration writes; this acceptance
        # intentionally permits only the native profile update above.
        return intro.oci.Delivery.raw_request(self, method, path, body, admin, public, timeout)

    def fixture_roots(self):
        return {kind: self.config[kind + "_movie_root"] for kind in ("target", "disabled")}

    def load_state(self):
        metadata = self.checkpoint_path.lstat()
        need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
             and 0 < metadata.st_size <= 1 << 20, "checkpoint_must_be_a_bounded_private_regular_file")
        self.state = json.loads(self.checkpoint_path.read_bytes())
        need(self.state["version"] == 1 and self.state["base_url"] == self.config["base_url"]
             and self.state["roots"] == self.fixture_roots(), "checkpoint_does_not_match_owned_fixture")
        self.user_id, self.task_id = self.state["user_id"], self.state["task_id"]
        self.completed = list(self.state["completed"])

    def task_definition(self):
        matches = [item for item in self.request("GET", "/admin/v1/tasks", admin=True)["Items"]
                   if item["Key"] == "media.preview_generation"]
        need(len(matches) == 1 and matches[0]["Enabled"] is True, "automatic_preview_task_missing_or_disabled")
        need(self.task_id is None or self.task_id == matches[0]["Id"], "checkpoint_task_identity_changed")
        self.task_id = matches[0]["Id"]

    def runs(self):
        page = self.request("GET", "/admin/v1/tasks/" + self.task_id + "/runs?Limit=200", admin=True)
        need(page["TotalRecordCount"] == len(page["Items"]), "owned_task_history_exceeds_acceptance_bound")
        return page["Items"]

    def wait_automatic_run(self, before):
        last_progress = None

        def check():
            nonlocal last_progress
            fresh = [run for run in self.runs() if run["Id"] not in before]
            for summary in sorted(fresh, key=lambda run: (run["CreatedAt"], run["Id"])):
                detail = self.run_detail(summary["Id"])
                run, children = detail["Run"], detail["Children"]["Items"]
                need(run["Source"] == "system_event", "preview_run_was_not_triggered_by_library_or_profile")
                need(all(child["LibraryId"] == self.state["libraries"]["target"] for child in children),
                     "automatic_preview_run_touched_disabled_library")
                progress = (run["Id"], run["State"], run["TerminalChildren"], run["TotalChildren"])
                if progress != last_progress:
                    emit("automatic_preview_progress", phase=self.phase, run_id=run["Id"], state=run["State"],
                         terminal_children=run["TerminalChildren"], total_children=run["TotalChildren"])
                    last_progress = progress
                failures = [entry for entry in [run, *children]
                            if entry["State"] in {"failed", "unavailable", "cancelled", "interrupted"}]
                if failures:
                    readable = all(entry.get("ErrorCode") and entry.get("ErrorMessage") for entry in failures)
                    emit("automatic_preview_failure", phase=self.phase, run_id=run["Id"],
                         task_error_readable=readable, treated_as_empty_success=False,
                         outcomes=[{"id": entry["Id"], "state": entry["State"],
                                    "code": self.safe_text(entry.get("ErrorCode")),
                                    "message": self.safe_text(entry.get("ErrorMessage"))} for entry in failures])
                    need(readable, "failed_preview_work_has_no_readable_task_error")
                    raise Failure("automatic_preview_work_failed")
                if run["State"] in {"pending", "running", "stopping"}:
                    continue
                need(run["State"] == "completed" and len(children) == 1 and not run["ErrorCode"]
                     and not run["ErrorMessage"] and all(child["State"] == "completed"
                     and not child["ErrorCode"] and not child["ErrorMessage"] for child in children),
                     "automatic_preview_run_did_not_complete_exact_target")
                emit("automatic_preview_completed", phase=self.phase, run_id=run["Id"],
                     source=run["Source"], enabled_library_only=True)
                return run["Id"]
            return None
        return self.wait("new_successful_automatic_preview_run", check)

    def assert_no_new_runs(self, before):
        deadline = time.monotonic() + self.config["quiet_seconds"]
        while True:
            need({run["Id"] for run in self.runs()} == set(before), "disabled_library_triggered_preview_work")
            if time.monotonic() >= deadline:
                return
            time.sleep(0.25)

    def movie(self, kind):
        query = urllib.parse.urlencode({"ParentId": self.state["libraries"][kind], "Recursive": "true",
                                        "IncludeItemTypes": "Movie", "Fields": "Path", "Limit": 10})
        page = self.request("GET", "/emby/Items?" + query, public=True)
        need(page["TotalRecordCount"] == 1 and len(page["Items"]) == 1, "fixture_requires_one_movie_" + kind)
        item = page["Items"][0]
        need(PurePosixPath(item["Path"]).is_relative_to(PurePosixPath(self.config[kind + "_movie_root"])),
             "fixture_movie_is_outside_configured_root")
        return {"id": item["Id"], "path": item["Path"]}

    def assert_catalog(self):
        for kind, expected in self.state["movies"].items():
            need(self.movie(kind) == expected, "fixture_movie_identity_changed_" + kind)

    def set_enabled(self, enabled):
        current = self.library("target")
        if current["LibraryOptions"]["EnablePreviewGeneration"] is enabled:
            return
        saved = self.request("PATCH", "/admin/v1/libraries/" + current["Id"], {
            "Revision": current["Revision"], "LibraryOptions": {"EnablePreviewGeneration": enabled}}, admin=True)["Library"]
        need(saved["LibraryOptions"]["EnablePreviewGeneration"] is enabled, "preview_library_option_not_saved")
        emit("preview_library_option", phase=self.phase, enabled=enabled, library_id=current["Id"])

    def analysis_item(self, kind):
        return self.request("GET", "/admin/v1/media-analysis/items/" + self.state["movies"][kind]["id"], admin=True)

    def assert_missing(self, kind):
        item = self.state["movies"][kind]["id"]
        need(not self.analysis_item(kind)["Previews"], "disabled_fixture_has_preview_references")
        for width in WIDTHS:
            data, _ = self.binary_request("/emby/Videos/" + item + "/index.bif?Width=" + str(width))
            need(not parse_bif(data)["frames"] and len(data) == 72, "missing_preview_is_not_an_empty_bif")
            self.request("GET", "/emby/Items/" + item + "/ThumbnailSet?Width=" + str(width),
                         expected=404, public=True)

    def retain(self, label, content):
        digest = hashlib.sha256(content).hexdigest()
        suffix = ".bif" if label.endswith("bif") else ".jpg"
        path = self.output / (label + "-" + digest[:16] + suffix)
        if path.exists():
            need(path.read_bytes() == content, "retained_artifact_bytes_changed")
        else:
            with path.open("xb") as output:
                os.chmod(path, 0o600)
                output.write(content)
        return path

    def image_route(self, tag, ticks):
        query = urllib.parse.urlencode({"PositionTicks": ticks, "tag": tag})
        return "/emby/Items/" + self.state["movies"]["target"]["id"] + "/Images/Thumbnail?" + query

    def verify_previews(self, interval_seconds, decode=True):
        self.assert_catalog()
        item_id = self.state["movies"]["target"]["id"]
        playback = self.request("POST", "/emby/Items/" + item_id + "/PlaybackInfo", {
            "EnableDirectPlay": True, "EnableDirectStream": False, "EnableTranscoding": False,
            "IsPlayback": False}, public=True)
        need(not playback.get("ErrorCode") and len(playback["MediaSources"]) == 1,
             "fixture_playback_info_unavailable")
        source = playback["MediaSources"][0]
        duration = source["RunTimeTicks"]
        need(source["SupportsDirectPlay"] is True and type(duration) is int and duration > TICKS_PER_SECOND,
             "fixture_direct_play_or_duration_unavailable")
        partial, headers = self.binary_request("/emby/Videos/" + item_id + "/stream.mp4?Static=true",
                                              expected=206, byte_range="bytes=0-1023")
        need(len(partial) == 1024 and headers.get("Content-Range") == "bytes 0-1023/" + str(source["Size"]),
             "source_playback_range_failed")
        effective_seconds = max(interval_seconds, (duration - 1) // (4096 * TICKS_PER_SECOND) + 1)
        count = (duration - 1) // (effective_seconds * TICKS_PER_SECOND) + 1
        need(count >= 2, "fixture_is_too_short_to_verify_interval_changes")
        expected_ticks = [index * effective_seconds * TICKS_PER_SECOND for index in range(count)]
        inventory = self.analysis_item("target")
        references = {str(value["Width"]): value for value in inventory["Previews"]}
        need(set(references) == {str(width) for width in WIDTHS}, "complete_preview_width_set_missing")
        result = {"source_revision": inventory["SourceRevision"], "interval_seconds": interval_seconds,
                  "duration_ticks": duration, "variants": {}}
        for width in WIDTHS:
            query = urllib.parse.urlencode({"Width": width, "MediaSourceId": source["Id"]})
            route = "/emby/Videos/" + item_id + "/index.bif?" + query
            content, bif_headers = self.binary_request(route)
            bif = parse_bif(content)
            reference = references[str(width)]
            need(bif["ticks"] == expected_ticks and bif["multiplier"] == effective_seconds * 1000,
                 "bif_does_not_cover_expected_complete_timeline")
            need(reference["Status"] == "ready" and not reference["FailureCode"]
                 and reference["FrameCount"] == count and reference["Size"] == len(content),
                 "published_preview_metadata_differs_from_bytes")
            dimensions = (width, reference["Height"])
            need(all(size == dimensions for size in bif["dimensions"]), "bif_jpeg_dimensions_differ_from_variant")
            part, range_headers = self.binary_request(route, expected=206, byte_range="bytes=0-63")
            need(part == content[:64] and range_headers.get("Content-Range") == "bytes 0-63/" + str(len(content))
                 and bif_headers.get("ETag") and range_headers.get("ETag") == bif_headers.get("ETag"),
                 "bif_range_does_not_match_full_archive")
            thumbnail_set = self.request("GET", "/emby/Items/" + item_id + "/ThumbnailSet?" + query, public=True)
            thumbnails = thumbnail_set["Thumbnails"]
            need(set(thumbnail_set) == {"AspectRatio", "Thumbnails"} and len(thumbnails) == count
                 and math.isclose(thumbnail_set["AspectRatio"], width / dimensions[1], rel_tol=1e-9)
                 and all(set(value) == {"PositionTicks", "ImageTag"} for value in thumbnails)
                 and [value["PositionTicks"] for value in thumbnails] == bif["ticks"],
                 "thumbnail_set_does_not_match_bif_timeline")
            tags = [value["ImageTag"] for value in thumbnails]
            need(len(set(tags)) == count and all(type(tag) is str and re.fullmatch(
                 "goby-preview-" + str(width) + "-[0-9a-f]{64}", tag) for tag in tags), "invalid_thumbnail_tags")
            for index in sorted({0, count // 2, count - 1}):
                image_bytes, image_headers = self.binary_request(self.image_route(tags[index], bif["ticks"][index]))
                need(image_headers.get_content_type() == "image/jpeg" and jpeg_dimensions(image_bytes) == dimensions,
                     "thumbnail_tag_did_not_deliver_matching_jpeg")
                if decode:
                    raw_path = self.retain("frame-" + str(width) + "-" + str(index), bif["frames"][index])
                    self.inspect_media(raw_path, width=width, audio=False)
                    image_path = self.retain("http-frame-" + str(width) + "-" + str(index), image_bytes)
                    self.inspect_media(image_path, width=width, audio=False)
            self.retain("preview-" + str(width) + "-bif", content)
            variant = {"sha256": hashlib.sha256(content).hexdigest(), "bytes": len(content),
                       "ticks": bif["ticks"], "tags": tags, "width": width, "height": dimensions[1],
                       "updated_at": reference["UpdatedAt"]}
            result["variants"][str(width)] = variant
            emit("bif_variant_verified", phase=self.phase, width=width, frames=count, bytes=len(content),
                 sha256=variant["sha256"], interval_seconds=effective_seconds, all_frames_jpeg=True,
                 sampled_frames_decoded=3 if decode and count >= 3 else min(count, 3) if decode else 0,
                 thumbnail_set=True, image_tags_followed=True, range=True)
        self.assert_missing("disabled")
        return result

    def pending(self):
        if self.state.get("pending") is None:
            self.state["pending"] = {"phase": self.phase, "before_runs": [run["Id"] for run in self.runs()]}
            self.save_state()
        need(self.state["pending"]["phase"] == self.phase, "another_phase_has_unfinished_evidence")
        return self.state["pending"]

    def finish_pending_run(self):
        pending = self.pending()
        run_id = self.wait_automatic_run(set(pending["before_runs"]))
        if run_id not in self.state["run_ids"]:
            self.state["run_ids"].append(run_id)
        self.save_state()

    def ordinary_scan(self):
        pending = self.pending()
        if "scan_job_id" not in pending:
            need(not pending.get("scan_request_sent"), "scan_request_receipt_missing_requires_inspection")
            pending["scan_request_sent"] = True
            self.save_state()
            result = self.request("POST", "/admin/v1/libraries/" + self.state["libraries"]["target"] + "/scan",
                                  {}, expected=202, admin=True)
            pending["scan_job_id"] = result["Job"]["Id"]
            self.save_state()
        job = self.scan_complete(pending["scan_job_id"])
        need(job["Scanned"] == 1 and job["ForceProbe"] is False, "scan_did_not_use_ordinary_one_movie_contract")

    def initialize(self):
        bootstrap = self.request("GET", "/admin/v1/bootstrap")
        if bootstrap["Initialized"]:
            need(self.config["allow_existing_admin"], "fresh_admin_required_unless_explicitly_selected")
        else:
            self.request("POST", "/admin/v1/bootstrap", {"SetupToken": self.config["setup_token"],
                "Name": self.config["admin_name"], "Password": self.config["admin_password"]}, expected=201)
        self.authenticate()
        need(self.request("GET", "/admin/v1/libraries", admin=True)["TotalRecordCount"] == 0,
             "acceptance_requires_no_existing_libraries")
        self.task_definition()
        overview = self.request("GET", "/admin/v1/media-analysis", admin=True)
        need(overview["Runtime"]["PreviewAvailable"] is True, "real_preview_runtime_unavailable")
        interval = overview["Configuration"]["Profile"]["PreviewIntervalSeconds"]
        need(interval != self.config["target_interval_seconds"], "target_interval_must_change_current_profile")
        self.state = {"version": 1, "base_url": self.config["base_url"], "roots": self.fixture_roots(),
                      "user_id": self.user_id, "task_id": self.task_id, "libraries": {}, "movies": {},
                      "run_ids": [], "completed": [], "pending": None, "interval_seconds": interval}
        before = {run["Id"] for run in self.runs()}
        for kind in ("target", "disabled"):
            body = {"Name": "Preview automation " + kind, "CollectionType": "movies",
                    "Paths": [self.config[kind + "_movie_root"]], "Scan": True}
            if kind == "target":
                body["LibraryOptions"] = {"EnablePreviewGeneration": False}
            # The control library omits the option to verify its actual default.
            result = self.request("POST", "/admin/v1/libraries", body, expected=201, admin=True)
            self.state["libraries"][kind] = result["Library"]["Id"]
            need(result["Library"]["LibraryOptions"]["EnablePreviewGeneration"] is False,
                 "created_library_preview_option_is_not_disabled")
            need(self.scan_complete(result["Job"]["Id"])["Scanned"] == 1, "initial_scan_population_is_wrong")
        self.state["movies"] = {kind: self.movie(kind) for kind in ("target", "disabled")}
        self.assert_missing("target")
        self.assert_missing("disabled")
        self.assert_no_new_runs(before)
        self.save_state()

    def enable_phase(self):
        if self.state is None:
            self.initialize()
        self.pending()
        self.set_enabled(True)
        self.finish_pending_run()
        self.state["previews"] = self.verify_previews(self.state["interval_seconds"])

    def rescan_phase(self):
        need("enable" in self.completed, "rescan_requires_enabled_checkpoint")
        need(self.library("target")["LibraryOptions"]["EnablePreviewGeneration"] is True,
             "enabled_preview_option_was_not_persisted")
        self.ordinary_scan()
        self.finish_pending_run()
        observed = self.verify_previews(self.state["interval_seconds"], decode=False)
        need(observed == self.state["previews"], "ordinary_scan_replaced_valid_preview_artifacts_or_references")
        emit("preview_cache_reuse_verified", phase=self.phase, bif_bytes_and_timeline_unchanged=True,
             image_tags_unchanged=True, reference_update_times_unchanged=True, ordinary_scan=True)

    def configuration_phase(self):
        need("rescan" in self.completed, "configuration_requires_rescan_checkpoint")
        pending = self.pending()
        target = self.config["target_interval_seconds"]
        current = self.request("GET", "/admin/v1/media-analysis", admin=True)["Configuration"]
        if current["Profile"]["PreviewIntervalSeconds"] != target:
            need(current["Profile"]["PreviewIntervalSeconds"] == self.state["interval_seconds"],
                 "configuration_changed_outside_owned_acceptance")
            profile = dict(current["Profile"])
            profile["PreviewIntervalSeconds"] = target
            result = self.request("PUT", "/admin/v1/media-analysis/configuration", {
                "Revision": current["Revision"], "Profile": profile}, admin=True)
            need(result["Profile"]["PreviewIntervalSeconds"] == target
                 and result["Revision"] != current["Revision"], "real_interval_update_was_not_committed")
            pending["configuration_revision"] = result["Revision"]
            self.save_state()
        for old in self.state["previews"]["variants"].values():
            self.request("GET", self.image_route(old["tags"][0], old["ticks"][0]), expected=404, public=True)
        self.finish_pending_run()
        rebuilt = self.verify_previews(target)
        for width, old in self.state["previews"]["variants"].items():
            new = rebuilt["variants"][width]
            need(new["sha256"] != old["sha256"] and new["ticks"] != old["ticks"]
                 and new["updated_at"] != old["updated_at"] and set(new["tags"]).isdisjoint(old["tags"]),
                 "profile_change_reused_obsolete_preview_references")
            self.request("GET", self.image_route(old["tags"][0], old["ticks"][0]), expected=404, public=True)
        self.state["interval_seconds"], self.state["previews"] = target, rebuilt
        emit("preview_profile_rebuild_verified", phase=self.phase, interval_seconds=target,
             old_image_tags_rejected=True, complete_new_timeline=True, automatic=True)

    def disable_phase(self):
        need("configuration" in self.completed, "disable_requires_configuration_checkpoint")
        self.pending()
        self.set_enabled(False)
        need(self.verify_previews(self.state["interval_seconds"], decode=False) == self.state["previews"],
             "disable_withdrew_or_changed_existing_previews")
        self.ordinary_scan()
        self.assert_no_new_runs(self.pending()["before_runs"])
        previous = self.control("status")
        self.control("recreate")
        current = self.ready()
        need(current["pid"] != previous["pid"], "application_was_not_recreated")
        self.authenticate()
        for kind in ("target", "disabled"):
            need(self.library(kind)["LibraryOptions"]["EnablePreviewGeneration"] is False,
                 "disabled_preview_option_was_not_persisted")
        need(self.verify_previews(self.state["interval_seconds"], decode=False) == self.state["previews"],
             "recreation_changed_retained_preview_bytes_or_timeline")
        self.assert_no_new_runs(self.pending()["before_runs"])
        emit("preview_disable_persistence_verified", phase=self.phase, new_automatic_runs=0,
             ordinary_scan=True, recreated=True, retained_bif_bytes=True, retained_thumbnail_timeline=True)

    def execute(self, selected):
        if self.checkpoint_path.exists():
            self.load_state()
        else:
            need(selected in {"all", "enable"}, "selected_phase_requires_retained_checkpoint")
        self.control("start")
        self.ready()
        if self.state is not None:
            self.authenticate()
            self.task_definition()
        selected_phases = [phase for phase in PHASES if phase not in self.completed] if selected == "all" else [selected]
        need(selected_phases and all(phase not in self.completed for phase in selected_phases), "phase_already_completed")
        for phase in selected_phases:
            self.phase = phase
            getattr(self, phase + "_phase")()
            self.completed.append(phase)
            self.state["completed"] = list(self.completed)
            self.state["pending"] = None
            self.save_state()
            emit("phase_completed", phase=phase)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="Absolute path to private acceptance JSON")
    parser.add_argument("--phase", choices=("all", *PHASES), default="all")
    arguments = parser.parse_args()
    driver, failure, failed_phase = None, None, None

    def interrupted(_signum, _frame):
        raise Failure("driver_interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        driver = PreviewAutomation(load_config(arguments.config))
        driver.execute(arguments.phase)
    except Exception as error:
        failure = str(error) if isinstance(error, Failure) else "unexpected_" + type(error).__name__
        failed_phase = None if driver is None else driver.phase
    finally:
        if driver is not None:
            try:
                driver.close()
            except Exception as error:
                emit("closure_failed", reason=str(error) if isinstance(error, Failure)
                     else "unexpected_" + type(error).__name__)
                failure, failed_phase = failure or "closure_failed", failed_phase or "closure"
    emit("result", passed=failure is None, failure=failure, failed_phase=failed_phase,
         selected_phase=arguments.phase, completed=[] if driver is None else driver.completed,
         scope="real_library_preview_automation_three_width_bif_thumbnailset_reuse_rebuild_and_disable",
         manual_analysis_starts=0, forced_work=0, numeric_gpu_claim=False)
    return 0 if failure is None else 1


if __name__ == "__main__":
    sys.exit(main())
