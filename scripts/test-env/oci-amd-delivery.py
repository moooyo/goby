#!/usr/bin/env python3
"""Verify the real Goby process in one explicitly provisioned AMD OCI install.

The private JSON configuration requires base_url, control_script, movie_root,
sample_path, output_dir, ffmpeg_path, ffprobe_path, setup_token, admin_name,
admin_password, render_device, container_ffmpeg_path, and ffmpeg_sha256.
Optional fields are timeout_seconds (600), expected_movies (1),
container_cache_path (/var/cache/goby/transcodes), expect_av1_padding (false),
and fallback_sample_path. Select expect_av1_padding only after an independent
actual probe has reproduced that rejection on the selected image and device.
Media/tool/output paths are host paths; movie_root,
render_device, container_ffmpeg_path, and container_cache_path are container
paths. The primary fixture is H.264 8-bit 320x192 with 24-120 seconds of moving
SDR content. The optional padding fixture is H.264 8-bit 320x180, 2-10 seconds.

The external controller owns provisioning, the private database, and process
observation. It accepts start, stop, status (the oci-delivery.py contract), and
media-evidence PLAY_ID. The latter returns jobs (id, play_session_id, state,
error_code, output_bytes, plan), observations (job_id, pid, start_time_ticks,
uid, exe, exe_sha256, args, cwd, render_fds), live_job_pids, open_output_pids,
and cache_job_ids. All collections are scoped to that play ID. An observer must
start before the application and retain actual /proc observations of its
FFmpeg children, including exited children. It binds jobs by the real cache
working directory, not by a capability probe or an inferred command. args is
a JSON string array; render_fds is a list of actual descriptor target paths.
The three cleanup lists describe current resources, not recorded history.

This finite journey verifies six actual HTTP outputs, independent software
decoding, administrator selection of both hardware axes, immutable database
plans, observed executable/arguments/render descriptors, and explicit-stop
cleanup before container shutdown. Optional AV1 fallback retains its exact
requested output. Live-producer cancellation is a separate actual-GPU Go test;
completed HTTP producers are not relabeled as cancellation observations.
No credentials or returned media URLs enter this driver's output.
"""

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import subprocess
import sys
import urllib.parse


spec = importlib.util.spec_from_file_location(
    "oci_delivery", Path(__file__).with_name("oci-delivery.py"))
oci = importlib.util.module_from_spec(spec)
spec.loader.exec_module(oci)
need, emit, Failure = oci.need, oci.emit, oci.Failure


def load_config(filename):
    path = Path(filename)
    need(path.is_absolute(), "config_must_be_absolute")
    metadata = path.lstat()
    need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
         and 0 < metadata.st_size <= 16384, "config_must_be_a_bounded_private_regular_file")
    value = json.loads(path.read_bytes())
    required = {"base_url", "control_script", "movie_root", "sample_path", "output_dir",
                "ffmpeg_path", "ffprobe_path", "setup_token", "admin_name", "admin_password",
                "render_device", "container_ffmpeg_path", "ffmpeg_sha256"}
    optional = {"timeout_seconds", "expected_movies", "container_cache_path",
                "expect_av1_padding", "fallback_sample_path"}
    need(type(value) is dict and required <= value.keys() <= required | optional, "invalid_config_fields")
    need(all(type(value[key]) is str and value[key] for key in required), "invalid_config_strings")
    origin = urllib.parse.urlsplit(value["base_url"])
    need(origin.scheme == "http" and origin.hostname in {"127.0.0.1", "::1", "localhost"}
         and origin.port is not None and origin.username is None and origin.password is None
         and origin.path in {"", "/"} and not origin.query and not origin.fragment,
         "base_url_must_be_explicit_loopback_http")
    value["base_url"] = value["base_url"].rstrip("/")
    value.setdefault("timeout_seconds", 600)
    value.setdefault("expect_av1_padding", False)
    value.setdefault("expected_movies", 2 if value["expect_av1_padding"] else 1)
    value.setdefault("container_cache_path", "/var/cache/goby/transcodes")
    need(type(value["timeout_seconds"]) is int and 30 <= value["timeout_seconds"] <= 1800,
         "invalid_timeout")
    need(type(value["expected_movies"]) is int and 1 <= value["expected_movies"] <= 25,
         "invalid_fixture_population")
    need(type(value["expect_av1_padding"]) is bool, "invalid_padding_selection")
    need(re.fullmatch(r"/dev/dri/renderD(?:12[89]|1[3-9][0-9]|2[0-4][0-9]|25[0-5])",
                      value["render_device"]) is not None, "invalid_render_device")
    need(re.fullmatch(r"[0-9a-f]{64}", value["ffmpeg_sha256"]) is not None, "invalid_ffmpeg_sha256")
    for key in ("control_script", "movie_root", "sample_path", "output_dir", "ffmpeg_path",
                "ffprobe_path", "container_ffmpeg_path", "container_cache_path"):
        need(type(value[key]) is str and Path(value[key]).is_absolute(), key + "_must_be_absolute")
    for key in ("control_script", "ffmpeg_path", "ffprobe_path"):
        need(Path(value[key]).is_file() and os.access(value[key], os.X_OK), key + "_must_be_executable")
    need(Path(value["output_dir"]).is_dir(), "output_directory_must_exist")
    if value["expect_av1_padding"]:
        need(type(value.get("fallback_sample_path")) is str
             and Path(value["fallback_sample_path"]).is_absolute(), "padding_fixture_required")
    else:
        need("fallback_sample_path" not in value, "unused_padding_fixture")
    for key in ("sample_path", "fallback_sample_path"):
        if key in value:
            fixture = Path(value[key])
            need(fixture.is_file() and 1024 < fixture.stat().st_size < 32 << 20,
                 key + "_must_be_bounded_media")
    return value


def argument_pair(arguments, option, value):
    return any(arguments[index:index + 2] == [option, value] for index in range(len(arguments) - 1))


class AMDDelivery(oci.Delivery):
    def __init__(self, config):
        super().__init__(config)
        self.device_id = None
        self.fixture_facts = {}
        self.active_plays = set()
        self.accepted_jobs = []

    def media_evidence(self, play_id):
        need(type(play_id) is str and re.fullmatch(r"play_[0-9a-f]{32}", play_id) is not None,
             "invalid_owned_play_identity")
        result = subprocess.run([self.config["control_script"], "media-evidence", play_id],
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=min(self.timeout, 30), check=False)
        need(result.returncode == 0 and len(result.stdout) <= 4 << 20, "media_evidence_control_failed")
        value = json.loads(result.stdout)
        fields = {"jobs", "observations", "live_job_pids", "open_output_pids", "cache_job_ids"}
        need(type(value) is dict and fields <= value.keys()
             and all(type(value[field]) is list for field in fields), "invalid_media_evidence")
        need(len(value["jobs"]) <= 32 and len(value["observations"]) <= 1024,
             "media_evidence_exceeds_selected_scope")
        for job in value["jobs"]:
            need(type(job) is dict and {"id", "play_session_id", "state", "error_code", "output_bytes", "plan"}
                 <= job.keys() and type(job["id"]) is str and re.fullmatch(r"[0-9a-f]{32}", job["id"])
                 and job["play_session_id"] == play_id and type(job["plan"]) is dict,
                 "invalid_scoped_encoding_job")
        need(len({job["id"] for job in value["jobs"]}) == len(value["jobs"]), "duplicate_encoding_identity")
        return value

    def media_command(self, executable, *arguments):
        result = subprocess.run([executable, *arguments], stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=self.timeout, check=False)
        need(result.returncode == 0, "independent_media_tool_failed")
        need(len(result.stdout) <= 8 << 20 and len(result.stderr) <= 1 << 20,
             "independent_media_tool_output_exceeds_bound")
        return result.stdout

    def probe(self, path):
        value = json.loads(self.media_command(self.config["ffprobe_path"], "-v", "error", "-count_frames",
            "-show_entries", "stream=codec_type,codec_name,codec_tag_string,profile,pix_fmt,width,height,nb_read_frames:format=duration",
            "-of", "json", str(path)))
        videos = [stream for stream in value["streams"] if stream["codec_type"] == "video"]
        need(len(videos) == 1, "media_must_have_exactly_one_video_stream")
        video = videos[0]
        video["frames"] = int(video["nb_read_frames"])
        video["seconds"] = float(value["format"]["duration"])
        video["audio"] = [stream["codec_name"] for stream in value["streams"] if stream["codec_type"] == "audio"]
        return video

    def pixels(self, path):
        return self.media_command(self.config["ffmpeg_path"], "-hide_banner", "-nostdin", "-v", "error",
            "-xerror", "-hwaccel", "none", "-threads", "1", "-i", str(path), "-map", "0:v:0",
            "-an", "-filter_threads", "1", "-vf", "scale=32:18,format=gray", "-f", "rawvideo", "pipe:1")

    def admit_fixture(self, key, width, height, minimum_seconds, maximum_seconds):
        path = Path(self.config[key])
        facts = self.probe(path)
        need(facts["codec_name"] == "h264" and facts["pix_fmt"] == "yuv420p"
             and (facts["width"], facts["height"]) == (width, height)
             and minimum_seconds <= facts["seconds"] <= maximum_seconds
             and 24 <= facts["frames"] <= 4000 and len(facts["audio"]) <= 1,
             "fixture_does_not_match_selected_sdr_scope")
        facts["pixels"] = self.pixels(path)
        need(len(facts["pixels"]) == facts["frames"] * 32 * 18, "fixture_pixels_are_incomplete")
        self.fixture_facts[key] = facts
        emit("fixture_admitted", fixture=key, width=width, height=height,
             frames=facts["frames"], seconds=facts["seconds"], sha256=hashlib.sha256(path.read_bytes()).hexdigest())

    def initialize(self):
        need(self.request("GET", "/admin/v1/bootstrap")["Initialized"] is False, "fresh_database_required")
        self.request("POST", "/admin/v1/bootstrap", {"SetupToken": self.config["setup_token"],
            "Name": self.config["admin_name"], "Password": self.config["admin_password"]}, expected=201)
        self.authenticate()
        dashboard = self.request("GET", "/admin/", binary=True)
        need(b"<html" in dashboard.lower() and b"/assets/" in dashboard, "embedded_dashboard_missing")
        library = self.request("POST", "/admin/v1/libraries", {"Name": "OCI AMD movies",
            "CollectionType": "movies", "Paths": [self.config["movie_root"]], "Scan": True}, expected=201, admin=True)
        self.libraries["Movie"] = library["Library"]["Id"]
        self.scan_complete(library["Job"]["Id"])
        self.baseline = {"Movie": self.catalog_for("Movie")}
        for key in self.fixture_facts:
            item_id = self.baseline["Movie"].get(Path(self.config[key]).stem)
            need(item_id is not None, "fixture_filename_does_not_match_catalog_movie")
            self.fixture_facts[key]["item_id"] = item_id
        current = self.request("GET", "/admin/v1/settings", admin=True)
        devices = current["Runtime"]["Hardware"]["Devices"]
        need(type(devices) is list and len(devices) == 1 and devices[0]["Available"] is True,
             "exactly_one_available_authorized_amd_device_required")
        self.device_id = devices[0]["DeviceId"]
        need(type(self.device_id) is str and self.device_id.startswith("amd-"), "invalid_opaque_amd_identity")
        emit("amd_inventory_admitted", available=True, opaque_device_id=self.device_id,
             catalog_items=len(self.baseline["Movie"]), embedded_dashboard=True)

    def select_axes(self, decode, encode):
        current = self.request("GET", "/admin/v1/settings", admin=True)
        selected = {"Decode": decode, "Encode": encode, "DeviceId": self.device_id}
        self.request("PUT", "/admin/v1/settings", {"Revision": current["Revision"], "Overrides": current["Overrides"],
            "Runtime": {"Hardware": selected, "Threads": 2}}, admin=True)
        after = self.request("GET", "/admin/v1/settings", admin=True)
        need(after["Revision"] != current["Revision"] or current["Runtime"]["Effective"]["Hardware"] == selected,
             "hardware_selection_revision_did_not_advance")
        need(after["Runtime"]["Effective"]["Hardware"] == selected
             and after["Runtime"]["Effective"]["Threads"] == 2, "hardware_selection_was_not_effective")
        emit("managed_hardware_selected", decode=decode, encode=encode, opaque_device_id=self.device_id)

    def prepare(self, fixture, codec):
        width, height = fixture["width"], fixture["height"]
        profile_name = "high" if codec == "h264" else "main"
        target = {"VideoCodec": codec, "VideoProfile": profile_name, "VideoBitDepth": "8",
                  "Width": str(width), "Height": str(height), "VideoBitrate": "300000"}
        profile = {"Type": "Video", "Container": "mp4", "Protocol": "http", "Context": "Streaming",
                   "VideoCodec": codec, "MaxWidth": width, "MaxHeight": height}
        if fixture["audio"]:
            profile["AudioCodec"] = "aac"
        conditions = [{"Property": name, "Condition": "Equals", "Value": value, "IsRequired": True}
                      for name, value in target.items() if name != "VideoCodec"]
        body = {"EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": True,
                "AllowVideoStreamCopy": False, "AllowAudioStreamCopy": False, "MaxStreamingBitrate": 600000,
                "DeviceProfile": {"TranscodingProfiles": [profile], "CodecProfiles": [
                    {"Type": "Video", "Container": "mp4", "Codec": codec, "Conditions": conditions}]}}
        playback = self.request("POST", "/emby/Items/" + fixture["item_id"] + "/PlaybackInfo", body, public=True)
        need(not playback.get("ErrorCode") and len(playback["MediaSources"]) == 1, "playback_negotiation_failed")
        uri, play_id = playback["MediaSources"][0]["TranscodingUrl"], playback["PlaySessionId"]
        parsed = urllib.parse.urlsplit(uri)
        need(not parsed.scheme and not parsed.netloc and parsed.path == "/emby/Videos/" + fixture["item_id"] + "/stream.mp4",
             "transcode_url_must_stay_on_owned_catalog_route")
        query = urllib.parse.parse_qs(parsed.query)
        need(all(query.get(name) == [value] for name, value in target.items())
             and query.get("PlaySessionId") == [play_id], "negotiated_output_tuple_changed")
        self.active_plays.add(play_id)
        before = self.media_evidence(play_id)
        need(not any(job["state"] in {"queued", "running"} for job in before["jobs"])
             and not before["live_job_pids"], "negotiation_started_a_media_producer")
        return uri, play_id, {job["id"] for job in before["jobs"]}, target

    def completed_job(self, play_id, previous):
        def check():
            evidence = self.media_evidence(play_id)
            jobs = [job for job in evidence["jobs"] if job["id"] not in previous]
            need(len(jobs) <= 1, "one_http_request_created_multiple_jobs")
            if not jobs or jobs[0]["state"] in {"queued", "running"}:
                return None
            need(jobs[0]["state"] == "completed" and not jobs[0]["error_code"]
                 and jobs[0]["output_bytes"] > 0, "http_media_job_failed")
            return jobs[0], evidence
        return self.wait("http_media_job_completed", check)

    def verify_process(self, job, evidence, decode, encode, codec):
        software = {"h264": "libx264", "hevc": "libx265", "av1": "libaom-av1"}
        encoder = codec + "_vaapi" if encode == "vaapi" else software[codec]
        expected_cwd = str(PurePosixPath(self.config["container_cache_path"]) / job["id"])
        for observed in evidence["observations"]:
            if observed.get("job_id") != job["id"]:
                continue
            arguments = observed.get("args")
            need(type(arguments) is list and len(arguments) <= 512
                 and all(type(value) is str and len(value) <= 16384 for value in arguments), "invalid_observed_arguments")
            if (observed.get("uid") != 10001 or observed.get("exe") != self.config["container_ffmpeg_path"]
                    or observed.get("exe_sha256") != self.config["ffmpeg_sha256"]
                    or observed.get("cwd") != expected_cwd or type(observed.get("pid")) is not int
                    or observed["pid"] <= 0 or type(observed.get("start_time_ticks")) is not int
                    or observed["start_time_ticks"] <= 0 or not argument_pair(arguments, "-c:v", encoder)):
                continue
            filters = ",".join(arguments[index + 1] for index, value in enumerate(arguments[:-1])
                               if value in {"-vf", "-filter_complex"})
            if decode == "vaapi":
                if (not argument_pair(arguments, "-hwaccel", "vaapi")
                        or not argument_pair(arguments, "-hwaccel_output_format", "vaapi")
                        or "hwdownload" not in filters
                        or "hwupload" in filters and filters.index("hwupload") < filters.index("hwdownload")):
                    continue
            elif "-hwaccel" in arguments:
                continue
            if encode == "vaapi" and "hwupload" not in filters:
                continue
            descriptors = observed.get("render_fds")
            need(type(descriptors) is list and all(type(path) is str for path in descriptors), "invalid_render_observation")
            if decode == "vaapi" or encode == "vaapi":
                if self.config["render_device"] not in descriptors:
                    continue
            elif descriptors:
                continue
            return observed
        raise Failure("actual_job_ffmpeg_axes_and_render_descriptors_not_observed")

    def verify_output(self, path, fixture, codec, encode):
        facts = self.probe(path)
        tag = "hev1" if codec == "hevc" and encode == "vaapi" else {"h264": "avc1", "hevc": "hvc1", "av1": "av01"}[codec]
        profile = "high" if codec == "h264" else "main"
        need(facts["codec_name"] == codec and facts["codec_tag_string"] == tag
             and facts["pix_fmt"] == "yuv420p" and facts["profile"].lower().replace(" ", "") == profile
             and (facts["width"], facts["height"]) == (fixture["width"], fixture["height"])
             and facts["frames"] == fixture["frames"] and abs(facts["seconds"] - fixture["seconds"]) <= 0.1
             and facts["audio"] == (["aac"] if fixture["audio"] else []), "coded_output_contract_changed")
        self.media_command(self.config["ffmpeg_path"], "-hide_banner", "-nostdin", "-v", "error", "-xerror",
            "-hwaccel", "none", "-threads", "1", "-i", str(path), "-f", "null", "-")
        actual, reference = self.pixels(path), fixture["pixels"]
        need(len(actual) == len(reference), "software_decoded_frame_count_changed")
        maximum_error = 0
        for offset in range(0, len(actual), 32 * 18):
            error = sum(abs(a - b) for a, b in zip(actual[offset:offset + 576], reference[offset:offset + 576])) / 576
            maximum_error = max(maximum_error, error)
        need(maximum_error <= 15, "software_decoded_picture_does_not_match_source")
        return {"frames": facts["frames"], "seconds": facts["seconds"], "codec": codec,
                "sample_entry": tag, "maximum_frame_mean_luma_error": round(maximum_error, 4)}

    def stop_play(self, play_id, job):
        route = "/emby/Videos/ActiveEncodings?" + urllib.parse.urlencode({
            "DeviceId": "phase3-cli-recovery", "PlaySessionId": play_id})
        status, _ = self.raw_request("DELETE", route, public=True)
        need(status == 204, "explicit_media_stop_failed")

        def closed():
            evidence = self.media_evidence(play_id)
            retained = [record for record in evidence["jobs"] if record["id"] == job["id"]]
            need(len(retained) == 1 and retained[0]["plan"] == job["plan"]
                 and retained[0]["state"] == "completed", "terminal_job_history_changed_after_stop")
            return evidence if (not any(record["state"] in {"queued", "running"} for record in evidence["jobs"])
                and not evidence["live_job_pids"] and not evidence["open_output_pids"]
                and not evidence["cache_job_ids"]) else None
        evidence = self.wait("explicit_stop_resource_closure", closed, timeout=30)
        self.active_plays.discard(play_id)
        return evidence

    def run_output(self, name, decode, encode, codec, fixture_key="sample_path", fallback=False):
        self.phase = name
        self.select_axes(decode, encode)
        fixture = self.fixture_facts[fixture_key]
        uri, play_id, previous, target = self.prepare(fixture, codec)
        payload, _ = self.binary_request(uri)
        path = self.save(name + ".mp4", payload)
        job, evidence = self.completed_job(play_id, previous)
        self.save(name + "-producer-evidence.json", json.dumps(evidence, indent=2, sort_keys=True).encode() + b"\n")
        actual_decode, actual_encode = ("software", "software") if fallback else (decode, encode)
        plan = job["plan"]
        hardware = plan.get("Hardware", {})
        need(hardware.get("Decode", "software") == actual_decode
             and hardware.get("Encode", "software") == actual_encode
             and hardware.get("Device", "") == ("" if fallback else self.config["render_device"])
             and plan.get("VideoCodec") == codec and plan.get("VideoProfile", "high" if codec == "h264" else "main") == target["VideoProfile"]
             and plan.get("VideoBitDepth", 8) == 8 and plan.get("Width") == fixture["width"]
             and plan.get("Height") == fixture["height"] and plan.get("VideoBitrate") == 300000
             and plan.get("OutputMode") == "progressive" and plan.get("Container") == "mp4"
             and plan.get("ExecutionVersion", 0) > 0 and plan.get("Execution", {}).get("Threads") == 2,
             "persisted_job_does_not_match_admitted_axes_and_output")
        observed = self.verify_process(job, evidence, actual_decode, actual_encode, codec)
        output = self.verify_output(path, fixture, codec, actual_encode)
        closed = self.stop_play(play_id, job)
        self.save(name + "-evidence.json", json.dumps({"case": name, "target": target, "job": job,
            "producer": observed, "output": output, "closure": closed}, indent=2, sort_keys=True).encode() + b"\n")
        emit("http_amd_output_verified", case=name, decode=actual_decode, encode=actual_encode,
             codec=codec, fallback=fallback, frames=output["frames"], sample_entry=output["sample_entry"],
             bytes=len(payload), sha256=hashlib.sha256(payload).hexdigest(), observed_uid=observed["uid"],
             actual_executable_and_arguments=True, render_descriptor=not fallback,
             software_decode=True, source_picture_comparison=True, explicit_stop_cleanup=True)
        self.accepted_jobs.append((play_id, job))
        self.completed.append(name)

    def verify_retained_history(self):
        evidence_by_play = {play_id: self.media_evidence(play_id)
                            for play_id in sorted({play_id for play_id, _ in self.accepted_jobs})}
        for play_id, job in self.accepted_jobs:
            evidence = evidence_by_play[play_id]
            retained = [record for record in evidence["jobs"] if record["id"] == job["id"]]
            need(len(retained) == 1 and retained[0]["plan"] == job["plan"]
                 and retained[0]["state"] == "completed" and not retained[0]["error_code"],
                 "settings_change_rewrote_historical_encoding_plan")
            need(not any(record["state"] in {"queued", "running"} for record in evidence["jobs"])
                 and not evidence["live_job_pids"] and not evidence["open_output_pids"]
                 and not evidence["cache_job_ids"], "closed_play_regained_media_resources")
        self.save("final-history-evidence.json", json.dumps(evidence_by_play, indent=2, sort_keys=True).encode() + b"\n")
        emit("immutable_history_verified", completed_jobs=len(self.accepted_jobs),
             hardware_settings_changed=True, history_unchanged=True, resources_closed=True)

    def execute(self):
        self.phase = "fixture_admission"
        self.admit_fixture("sample_path", 320, 192, 24, 120)
        if self.config["expect_av1_padding"]:
            self.admit_fixture("fallback_sample_path", 320, 180, 2, 10)
        self.phase = "bootstrap_and_inventory"
        self.control("start")
        self.ready()
        self.initialize()
        self.completed.append(self.phase)
        for label, decode, encode in (("decode_only", "vaapi", "software"),
                                      ("encode_only", "software", "vaapi"),
                                      ("combined", "vaapi", "vaapi")):
            for codec in ("h264", "hevc"):
                self.run_output(label + "_" + codec, decode, encode, codec)
        if self.config["expect_av1_padding"]:
            self.run_output("av1_exact_software_fallback", "software", "vaapi", "av1",
                            "fallback_sample_path", fallback=True)
        self.phase = "final_http_state"
        need(self.catalog_for("Movie") == self.baseline["Movie"], "fixture_catalog_changed")
        need(not self.active_plays, "driver_retained_an_unclosed_play")
        self.select_axes("software", "software")
        self.verify_retained_history()
        self.completed.append(self.phase)


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
        driver = AMDDelivery(load_config(args.config))
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
         av1_padding_selected=False if driver is None else driver.config["expect_av1_padding"],
         scope="linux_amd64_selected_amd_oci_http_axes_and_output; live_cancellation_has_separate_gpu_test_evidence")
    return 0 if failure is None else 1


if __name__ == "__main__":
    sys.exit(main())
