#!/usr/bin/env python3
"""Verify deployed progressive video and a normal five-library ProbeVersion 5 upgrade.

Run only as root through SSH after the schema-13 service has been deployed.
Existing protected credentials, fixture ownership records, and the shared
fixture lock are required. The verifier never creates users, libraries, source
media, or ownership records. Its mutations are normal library scans and its
own login, playback, and conversion sessions. Complete semantic snapshots stay
in memory; the persistent report contains only fixed labels, counts, versions,
nonsecret media measurements, and aggregate hashes.

The baseline is captured after deployment. It proves preservation during the
probe upgrade and media requests, not preservation across the schema migration.
Other verifiers that change catalog or user playback data must remain idle.
"""

from __future__ import annotations

import copy
import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qs, quote, urlencode, urlsplit, urlunsplit


sys.dont_write_bytecode = True
try:
    specification = importlib.util.spec_from_file_location(
        "goby_progressive_video_support", Path(__file__).with_name("verify-audio-profiles.py"))
    support = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(support)
    audio, hls, smoke, prior = support.audio, support.hls, support.smoke, support.upgrade_module
except Exception:
    print(json.dumps({"status": "failed", "failed_stage": "helper import",
                      "error": "The sibling protected deployment helpers are unavailable", "cleanup_errors": []}))
    raise SystemExit(1) from None


RESULT = Path("/opt/goby-test/m4e-deployed-progressive-video.json")
REPORT_OWNER = "goby-progressive-video-m4e-verification-v1"
MAX_REPORT_BYTES = 128 * 1024
SEEK_TICKS = 63_700_000
PIXEL_WIDTH, PIXEL_HEIGHT = 32, 18
PIXEL_BYTES = PIXEL_WIDTH * PIXEL_HEIGHT
TICKS = smoke.TICKS


def check(condition: bool, label: str) -> None:
    smoke.check(condition, label)


def report_destination() -> tuple[int, int]:
    info = RESULT.parent.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and
          RESULT.parent.resolve(strict=True) == RESULT.parent,
          "The private progressive-video report directory has unsafe ownership")
    if RESULT.exists() or RESULT.is_symlink():
        smoke.private_file(RESULT)
        check(0 < RESULT.stat().st_size <= MAX_REPORT_BYTES, "The prior progressive-video report exceeds its size bound")
        previous = json.loads(RESULT.read_text(encoding="utf-8"))
        check(isinstance(previous, dict) and previous.get("owner") == REPORT_OWNER and
              previous.get("status") in {"passed", "failed"} and isinstance(previous.get("assertions"), list) and
              isinstance(previous.get("cleanup_errors"), list), "The prior progressive-video report is not owned by this verifier")
    return info.st_dev, info.st_ino


class Database(support.Database):
    def playbacks(self, api) -> list[str]:
        values = self.read("SELECT COALESCE(json_agg(id ORDER BY id),'[]'::json) FROM play_sessions WHERE user_id=" +
                           audio.sql(api.user_id) + " AND auth_session_id=" + audio.sql(api.session_id) +
                           " AND device_id=" + audio.sql(smoke.DEVICE_ID) + ";", "Owned playback cleanup identities")
        check(isinstance(values, list) and len(values) <= 64 and
              all(isinstance(value, str) and re.fullmatch(r"play_[0-9a-f]{32}", value) for value in values),
              "Owned playback cleanup identities exceed the acknowledged authentication scope")
        return values

    def completed(self, api, play_id: str, count: int) -> list[dict]:
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            plans = self.plans(api, play_id)
            check(isinstance(plans, list) and len(plans) <= count, "The canonical playback created unexpected conversion plans")
            if len(plans) == count and all(value.get("state") == "completed" and not value.get("error_code") for value in plans):
                return plans
            check(not any(value.get("state") in {"failed", "cancelled"} for value in plans),
                  "An owned progressive conversion failed before durable completion")
            time.sleep(0.1)
        raise smoke.VerificationFailure("The owned progressive conversion did not become durably complete")


class ProbeFiveUpgrade(prior.UpgradeVerification):
    """Reuse ownership and scan cleanup without changing historical verifiers."""

    def __init__(self, api, db, smoke_module, audio_record: dict) -> None:
        super().__init__(api, db, smoke_module, audio_record)
        self.upgraded_clocks = None

    def _database_snapshot(self) -> dict:
        ids = [entry["id"] for entry in self.libraries.values()]
        check(len(ids) == 5 and len(set(ids)) == 5 and all(prior.ID_PATTERN.fullmatch(value) for value in ids),
              "The probe-five database scope is outside the five recorded libraries")
        selectors = ",".join(audio.sql(value) for value in ids)
        query = """
WITH selected AS MATERIALIZED (
    SELECT i.* FROM items i WHERE i.library_id IN (SELECTOR)
), selected_ids AS MATERIALIZED (SELECT id FROM selected)
SELECT jsonb_build_object(
    'item_count', (SELECT count(*) FROM selected),
    'all_item_count', (SELECT count(*) FROM items),
    'user_row_count', (SELECT count(*) FROM user_item_data),
    'selected_user_row_count', (SELECT count(*) FROM user_item_data WHERE item_id IN (SELECT id FROM selected_ids)),
    'items', COALESCE((SELECT jsonb_agg(to_jsonb(i) - 'media' - 'updated_at' - 'probed_at' ORDER BY i.id COLLATE "C")
        FROM (SELECT * FROM selected ORDER BY id COLLATE "C" LIMIT 257) i), '[]'::jsonb),
    'all_items', COALESCE((SELECT jsonb_agg(to_jsonb(i) - 'media' - 'updated_at' - 'probed_at' ORDER BY i.id COLLATE "C")
        FROM (SELECT * FROM items ORDER BY id COLLATE "C" LIMIT 257) i), '[]'::jsonb),
    'media', COALESCE((SELECT jsonb_agg(jsonb_build_object('id',i.id,'library_id',i.library_id,
        'path',i.path,'type',i.type,'probe_version',i.media->'ProbeVersion') ORDER BY i.id COLLATE "C")
        FROM (SELECT * FROM selected WHERE media IS NOT NULL ORDER BY id COLLATE "C" LIMIT 257) i), '[]'::jsonb),
    'media_semantics', COALESCE((SELECT jsonb_agg(jsonb_build_object('id',i.id,
        'facts',i.media - 'ProbeVersion' - 'FormatStartKnown' - 'FormatStartTicks') ORDER BY i.id COLLATE "C")
        FROM (SELECT * FROM items WHERE media IS NOT NULL ORDER BY id COLLATE "C" LIMIT 257) i), '[]'::jsonb),
    'libraries', COALESCE((SELECT jsonb_agg(to_jsonb(l) - 'last_scan_at' ORDER BY l.id COLLATE "C")
        FROM libraries l WHERE l.id IN (SELECTOR)), '[]'::jsonb),
    'roots', COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id COLLATE "C")
        FROM library_roots r WHERE r.library_id IN (SELECTOR)), '[]'::jsonb),
    'user_data', COALESCE((SELECT jsonb_agg(to_jsonb(d) ORDER BY d.user_id COLLATE "C",d.item_id COLLATE "C")
        FROM (SELECT * FROM user_item_data ORDER BY user_id COLLATE "C",item_id COLLATE "C" LIMIT 4097) d), '[]'::jsonb),
    'item_entities', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.item_id COLLATE "C",e.entity_id,e.position)
        FROM item_entities e WHERE e.item_id IN (SELECT id FROM selected_ids)), '[]'::jsonb),
    'entities', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.id) FROM catalog_entities e
        WHERE e.id IN (SELECT entity_id FROM item_entities WHERE item_id IN (SELECT id FROM selected_ids))), '[]'::jsonb),
    'images', COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.item_id COLLATE "C",i.image_type COLLATE "C",i.image_index)
        FROM item_images i WHERE i.item_id IN (SELECT id FROM selected_ids)), '[]'::jsonb),
    'subtitles', COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY s.item_id COLLATE "C",s.stream_index)
        FROM item_subtitles s WHERE s.item_id IN (SELECT id FROM selected_ids)), '[]'::jsonb)
);
""".replace("SELECTOR", selectors)
        snapshot = self.db.read(query, "Complete semantic catalog and all-user playback baseline")
        check(isinstance(snapshot, dict) and 0 < snapshot.get("item_count", 0) <= prior.MAX_ITEMS and
              snapshot["item_count"] == len(snapshot.get("items", [])) and
              snapshot["item_count"] <= snapshot.get("all_item_count", 0) <= prior.MAX_ITEMS and
              snapshot["all_item_count"] == len(snapshot.get("all_items", [])) and
              0 <= snapshot.get("user_row_count", -1) <= prior.MAX_USER_ROWS and
              snapshot["user_row_count"] == len(snapshot.get("user_data", [])),
              "The semantic snapshot exceeded its bounds or omitted catalog or user-state rows")
        check(len(snapshot["libraries"]) == len(snapshot["roots"]) == 5,
              "The five fixture libraries no longer have exactly one recorded root each")
        for key, value in self.libraries.items():
            root = prior.FIXTURE_ROOT / value["root"]
            roots = [entry for entry in snapshot["roots"] if entry["library_id"] == value["id"]]
            check(len(roots) == 1 and roots[0]["path"] == str(root) and roots[0]["allowed_path"] == str(prior.FIXTURE_ROOT) and
                  roots[0]["relative_path"] == value["root"], "A fixture database root differs from its exact ownership record")
            media = [entry for entry in snapshot["media"] if entry["library_id"] == value["id"]]
            check(len(media) == value["media_count"] and all(type(entry["probe_version"]) is int and
                  1 <= entry["probe_version"] <= 5 for entry in media), "A fixture has an unexpected media population or probe version")
            for entry in media:
                path = Path(entry["path"])
                check(path != root and path.is_relative_to(root) and path.resolve(strict=True) == path and path.is_file() and
                      entry["type"] == ("Audio" if key == "audio" else "Episode" if key == "nextup" else "Movie"),
                      "A fixture item does not identify its owned regular source and media type")
        return snapshot

    def _summary(self, snapshot: dict, files: dict) -> dict:
        return {**super()._summary(snapshot, files), "all_item_count": snapshot["all_item_count"],
                "complete_media_semantics_sha256": prior.aggregate(snapshot["media_semantics"])}

    def verify(self) -> dict:
        check(self.prepared and self.finished and not self.pending_jobs and not self.unconfirmed_scans,
              "Probe-five postconditions require all five owned scans to finish")
        after, files = self._preservation_observation()
        check(all(entry["probe_version"] == 5 for entry in after["media"]),
              "A normally rescanned fixture source did not reach ProbeVersion 5")
        selectors = ",".join(audio.sql(value["id"]) for value in self.libraries.values())
        clocks = self.db.read("SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'known',media->'FormatStartKnown',"
                              "'ticks',media->'FormatStartTicks') ORDER BY id COLLATE \"C\"),'[]'::jsonb) FROM items WHERE media IS NOT NULL "
                              "AND library_id IN (" + selectors + ");", "Probe-five source format-clock field shapes")
        check(isinstance(clocks, list) and len(clocks) == len(after["media"]) and all(
            type(value.get("known")) is bool and type(value.get("ticks")) is int and
            abs(value["ticks"]) <= 30 * 86400 * TICKS and (value["known"] or value["ticks"] == 0) for value in clocks),
            "A probe-five source has malformed or falsely populated format-clock facts")
        if self.upgraded_clocks is None:
            self.upgraded_clocks = copy.deepcopy(clocks)
        else:
            check(clocks == self.upgraded_clocks, "A source format-clock fact changed after the completed probe upgrade")
        self.verified = True
        return {"status": "passed", "before": self._summary(self.before, self.before_files),
                "after": self._summary(after, files), "scans": [dict(value) for value in self.scan_results],
                **self._preservation_summary(), "all_media_probe_version": 5,
                "source_clock_known_count": sum(value["known"] for value in clocks),
                "source_clock_unknown_count": sum(not value["known"] for value in clocks),
                "upgraded_source_clocks_sha256": prior.aggregate(clocks),
                "baseline_boundary": "Captured after schema-13 deployment and before normal library scans",
                "media_semantic_exceptions": ["ProbeVersion", "FormatStartKnown", "FormatStartTicks"]}


def indexed_video(api, db, upgrade) -> tuple[str, dict, dict, dict]:
    library_id = upgrade.libraries["hls"]["id"]
    rows = db.read("SELECT COALESCE(json_agg(json_build_object('Id',id,'Path',path,'Media',media)),'[]'::json) "
                   "FROM items WHERE library_id=" + audio.sql(library_id) + " AND type='Movie';", "Owned probe-five video source")
    check(isinstance(rows, list) and len(rows) == 1 and rows[0].get("Path") == str(hls.MEDIA),
          "The owned HLS library does not identify its unique existing video source")
    item_id, media = rows[0]["Id"], rows[0].get("Media") or {}
    check(media.get("ProbeVersion") == 5 and media.get("FormatStartKnown") is True and
          type(media.get("FormatStartTicks")) is int and abs(media["FormatStartTicks"]) <= 30 * 86400 * TICKS and
          abs(media.get("DurationTicks", 0) / TICKS - 15) <= 0.01,
          "The source video requires a verified bounded format origin and a fifteen-second presentation")
    item = hls.detail(api, item_id)
    sources = item.get("MediaSources", [])
    check(len(sources) == 1 and sources[0].get("Id") == "mediasource_" + item_id and sources[0].get("Path") == str(hls.MEDIA) and
          sources[0].get("Protocol") == "File" and sources[0].get("Size") == hls.MEDIA_SIZE,
          "The original source DTO differs from its owned indexed source")
    original = support.source_projection(sources[0])
    videos = [stream for stream in original["MediaStreams"] if stream.get("Type") == "Video"]
    audios = [stream for stream in original["MediaStreams"] if stream.get("Type") == "Audio"]
    check(len(videos) == len(audios) == 1 and videos[0].get("Codec") == "h264" and audios[0].get("Codec") == "aac" and
          (videos[0].get("Width"), videos[0].get("Height")) == (320, 180),
          "The existing video fixture must retain its actual H.264/AAC tracks")
    return item_id, original, {"video": videos[0], "audio": audios[0], "media": media}, copy.deepcopy(item["UserData"])


def video_profile(protocol: str | None) -> dict:
    result = {"Type": "Video", "Container": "ts" if protocol == "hls" else "mp4",
              "VideoCodec": "h264", "AudioCodec": "aac", "Context": "Streaming"}
    if protocol is not None:
        result["Protocol"] = protocol
    return result


def request_body(api, original: dict, streams: dict, profiles: list[dict], *, encode=False) -> dict:
    conditions = []
    if encode:
        conditions = [
            {"Type": "Video", "Codec": "h264", "Container": "mp4", "Conditions": [
                support.condition("Width", "160"), support.condition("Height", "90"),
                support.condition("VideoBitrate", "300000")]},
            {"Type": "VideoAudio", "Codec": "aac", "Container": "mp4", "Conditions": [
                support.condition("AudioChannels", "2"), support.condition("AudioSampleRate", "48000"),
                support.condition("AudioBitrate", "96000")]},
        ]
    return {"UserId": api.user_id, "MediaSourceId": original["Id"], "IsPlayback": True,
            "EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": True,
            "AllowVideoStreamCopy": not encode, "AllowAudioStreamCopy": not encode,
            "AudioStreamIndex": streams["audio"]["Index"], "SubtitleStreamIndex": -1, "StartTimeTicks": 0,
            "MaxStreamingBitrate": 2_000_000, "MaxAudioChannels": 2,
            "DeviceProfile": {"Name": "Goby deployed progressive video verification", "SupportedMediaTypes": "Video",
                              "MaxStreamingBitrate": 2_000_000, "DirectPlayProfiles": [],
                              "TranscodingProfiles": profiles, "CodecProfiles": conditions}}


def negotiated(api, db, item_id: str, original: dict, body: dict, play_ids: list[str], protocol: str) -> tuple[str, str, dict]:
    before = db.source_jobs(api, item_id, original["Id"])
    response = api.request("POST", "/emby/Items/" + quote(item_id) + "/PlaybackInfo", emby=True, body=body,
                           label="Ordered video PlaybackInfo negotiation")
    play_id = response.get("PlaySessionId", "")
    check(isinstance(play_id, str) and re.fullmatch(r"play_[0-9a-f]{32}", play_id),
          "Video PlaybackInfo omitted a canonical playback identity")
    if play_id not in play_ids:
        play_ids.append(play_id)
    check(not response.get("ErrorCode") and len(response.get("MediaSources", [])) == 1,
          "Video PlaybackInfo did not negotiate one compatible source")
    state = db.playback(api, play_id)
    check(state is not None and state.get("item_id") == item_id and state.get("media_source_id") == original["Id"] and
          state.get("state") == "Prepared" and state.get("active_jobs") == 0 and
          db.source_jobs(api, item_id, original["Id"]) == before, "Video negotiation created a job or lost canonical scope")
    source = response["MediaSources"][0]
    check(support.source_projection(source) == original and source.get("SupportsTranscoding") is True and
          source.get("SupportsDirectPlay") is False and source.get("SupportsDirectStream") is False and
          source.get("TranscodingSubProtocol") == protocol and source.get("TranscodingContainer") == ("ts" if protocol == "hls" else "mp4"),
          "Video negotiation replaced original facts or selected the wrong protocol")
    target = source.get("TranscodingUrl")
    check(isinstance(target, str) and 0 < len(target) <= 8192, "Video negotiation omitted its bounded media URL")
    parsed = urlsplit(target)
    expected_path = "/emby/Videos/" + quote(item_id) + ("/master.m3u8" if protocol == "hls" else "/stream.mp4")
    check(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path == expected_path,
          "Video PlaybackInfo did not emit the standard relative media route")
    query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
    check(len(query) <= 64 and all(len(value) == 1 for value in query.values()) and
          query.get("DeviceId") == [smoke.DEVICE_ID] and query.get("MediaSourceId") == [original["Id"]] and
          query.get("PlaySessionId") == [play_id] and query.get("api_key") == [api.token],
          "The negotiated video URL lost authentication, device, source, or playback scope")
    private_keys = {"sourceformatstartknown", "sourceformatstartticks", "formatstartticks", "formatstartknown",
                    "presentationoriginticks", "sourcevideostartticks", "sourceaudiostartticks",
                    "audiosourcesamplerate", "audiosourcesamplecount", "durationticks", "path", "jobid", "hardware"}
    check(not any(key.lower() in private_keys for key in query), "The negotiated video URL exposed private plan or source-clock fields")
    if protocol == "http":
        check(query.get("Static") == ["false"] and query.get("StartTimeTicks") == ["0"] and
              not any(key.lower() == "gobyhlsid" for key in query),
              "The progressive MP4 URL omitted concrete dynamic output settings")
    return target, play_id, query


def video_headers(headers: dict) -> None:
    check(headers.get("content-type") == "video/mp4" and headers.get("accept-ranges") == "none" and
          all(key not in headers for key in ("content-length", "content-range", "etag")) and
          "no-store" in headers.get("cache-control", ""),
          "Progressive MP4 advertised a byte range, fixed size, validator, or incorrect MIME type")


def head_without_job(api, db, http, target: str, play_id: str) -> None:
    before = db.playback(api, play_id)
    headers, body = http.request("HEAD", target, "Negotiated progressive MP4 HEAD")
    video_headers(headers)
    after = db.playback(api, play_id)
    check(not body and before is not None and after is not None and before == after and
          after["state"] == "Prepared" and after["active_jobs"] == 0,
          "Progressive MP4 HEAD returned a body, created a job, or changed canonical playback state")


def finite(value, label: str) -> float:
    number = float(value)
    check(math.isfinite(number), label)
    return number


def pixels(scratch, path: Path) -> bytes:
    return scratch.run([audio.FFMPEG, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1",
                        "-filter_threads", "1", "-i", str(path), "-map", "0:v:0", "-an", "-sn", "-dn",
                        "-vf", f"scale={PIXEL_WIDTH}:{PIXEL_HEIGHT},format=gray", "-fps_mode", "passthrough",
                        "-threads", "1", "-f", "rawvideo", "pipe:1"], "Decoded video scene sequence", timeout=30)


def scene_positions(reference: bytes, observed: bytes, first_index: int) -> list[dict]:
    check(len(reference) == 360 * PIXEL_BYTES and observed and len(observed) % PIXEL_BYTES == 0,
          "Decoded scene sequences have an unexpected bounded frame shape")
    count = len(observed) // PIXEL_BYTES
    matches = []
    for output_index in sorted({0, count // 2, count - 1}):
        frame = observed[output_index * PIXEL_BYTES:(output_index + 1) * PIXEL_BYTES]
        scores = []
        for source_index in range(360):
            expected = reference[source_index * PIXEL_BYTES:(source_index + 1) * PIXEL_BYTES]
            scores.append(sum(abs(left - right) for left, right in zip(frame, expected)) / PIXEL_BYTES)
        best = min(range(len(scores)), key=scores.__getitem__)
        wanted = first_index + output_index
        distant = min(score for index, score in enumerate(scores) if abs(index - wanted) > 1)
        check(abs(best - wanted) <= 1 and scores[best] <= 20 and scores[best] < distant,
              "Decoded output scenes do not identify the requested source presentation window")
        matches.append({"output_frame": output_index, "expected_source_frame": wanted, "nearest_source_frame": best,
                        "mean_absolute_luma_error": round(scores[best], 4), "position_tolerance_frames": 1})
    return matches


def video_probe(scratch, content: bytes, name: str, reference: bytes, *, start_ticks=0, width=320, height=180, copied=False) -> dict:
    path = scratch.write(name, content)
    raw = scratch.run([audio.FFPROBE, "-v", "error", "-show_streams", "-show_frames", "-show_format", "-show_entries",
                       "stream=index,codec_name,codec_type,codec_tag_string,width,height,channels,sample_rate:"
                       "frame=media_type,best_effort_timestamp_time,duration_time,nb_samples:format=duration", "-of", "json", str(path)],
                      "Downloaded fragmented MP4 probe", timeout=30)
    facts = json.loads(raw)
    streams = facts.get("streams", [])
    check(len(streams) == 2 and {stream.get("codec_type") for stream in streams} == {"video", "audio"},
          "Progressive MP4 must contain exactly the selected video and audio tracks")
    by_type = {stream["codec_type"]: stream for stream in streams}
    check(by_type["video"].get("codec_name") == "h264" and by_type["video"].get("codec_tag_string") == "avc1" and
          (by_type["video"].get("width"), by_type["video"].get("height")) == (width, height) and
          by_type["audio"].get("codec_name") == "aac" and by_type["audio"].get("codec_tag_string") == "mp4a",
          "Actual fragmented MP4 streams do not match the declared codecs or dimensions")
    sample_rate = int(by_type["audio"].get("sample_rate", "0"))
    channels = by_type["audio"].get("channels")
    check(sample_rate > 0 and type(channels) is int and channels > 0 and
          (copied or sample_rate == 48000 and channels == 2),
          "Actual progressive AAC output does not satisfy its channel or sample-rate targets")
    first_index = (start_ticks * 24 + TICKS - 1) // TICKS
    frame_count = 360 - first_index
    frames = facts.get("frames", [])
    video = [frame for frame in frames if frame.get("media_type") == "video"]
    audio_frames = [frame for frame in frames if frame.get("media_type") == "audio"]
    check(len(video) == frame_count and audio_frames, "Progressive MP4 lost video frames or its decoded audio track")
    audio_start = finite(audio_frames[0].get("best_effort_timestamp_time"), "Missing decoded audio presentation origin")
    audio_end = audio_start
    for frame in audio_frames:
        pts = finite(frame.get("best_effort_timestamp_time"), "Invalid decoded audio presentation timestamp")
        samples = frame.get("nb_samples")
        check(type(samples) is int and samples > 0 and abs(pts - audio_end) <= 2 / sample_rate,
              "Progressive MP4 audio contains a decoded presentation gap or invalid sample count")
        audio_end = pts + samples / sample_rate
    check(abs(audio_start) <= 0.05 and abs(audio_end - (15 - start_ticks / TICKS)) <= 0.05,
          "Progressive MP4 audio does not cover the requested source presentation window")
    first_pts = finite(video[0].get("best_effort_timestamp_time"), "Missing progressive video presentation time")
    expected_first = first_index / 24 - start_ticks / TICKS
    check(abs(first_pts - expected_first) <= 0.003,
          "Progressive video timestamps do not begin at the requested decoded frame boundary")
    for index, frame in enumerate(video):
        pts = finite(frame.get("best_effort_timestamp_time"), "Invalid progressive video presentation time")
        check(abs(pts - expected_first - index / 24) <= 0.003,
              "Progressive video presentation timestamps contain an unexpected gap or origin shift")
    progress = scratch.run([audio.FFMPEG, "-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-err_detect", "explode",
                            "-threads", "1", "-i", str(path), "-map", "0:v:0", "-map", "0:a:0", "-threads", "1",
                            "-fps_mode", "passthrough", "-progress", "pipe:1", "-nostats", "-f", "null", "-"],
                           "Strict complete progressive MP4 audio/video decode", timeout=30)
    decoded = hls.progress(progress, 15 - start_ticks / TICKS, frame_count, "Complete progressive MP4")
    image_sequence = pixels(scratch, path)
    check(len(image_sequence) == frame_count * PIXEL_BYTES, "The decoded output scene count differs from its presentation frames")
    if copied:
        check(start_ticks == 0 and image_sequence == reference, "Zero-start video copy changed the decoded original frame sequence")
        positions = []
    else:
        positions = scene_positions(reference, image_sequence, first_index)
    return {"http_status": 200, "bytes": len(content), "sha256": hashlib.sha256(content).hexdigest(),
            "video_codec": "h264", "audio_codec": "aac", "width": width, "height": height,
            "start_ticks": start_ticks, "first_video_pts_seconds": round(first_pts, 7),
            "audio_channels": channels, "audio_sample_rate": sample_rate,
            "audio_start_seconds": round(audio_start, 7), "audio_end_seconds": round(audio_end, 7),
            "decoded_audio_frames": len(audio_frames), "complete_strict_decode": True,
            "copied_frame_sequence_identical": copied, "scene_positions": positions, **decoded}


def cleanup_encoding(api, db, play_id: str) -> None:
    # A transport check must not invent a player event. Playing/Stopped is a
    # user-data write even when the verifier has only prepared a zero-position
    # session. Cancel owned encoders and revoke authentication instead.
    before = db.playback(api, play_id)
    audio.cleanup_encoding(api, play_id)
    after = db.playback(api, play_id)
    check(before is not None and after is not None and before["state"] == after["state"] == "Prepared" and
          before["item_id"] == after["item_id"] and before["media_source_id"] == after["media_source_id"],
          "Deleting owned encoders changed canonical playback state or source identity")


def main() -> int:
    if not (sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION")):
        print(json.dumps({"status": "failed", "failed_stage": "execution boundary",
                          "error": "Run only as root through SSH after the schema-13 deployment", "cleanup_errors": []}))
        return 1
    os.umask(0o077)
    api, owned, scratch = smoke.API(), smoke.OwnedFixture(), audio.Scratch()
    http = hls.HTTP(api)
    db = upgrade = None
    item_id = source_id = ""
    original = baseline = None
    plays, cleaned, secrets, retired_targets = [], set(), [], []
    report_parent = None
    summary = {"owner": REPORT_OWNER, "status": "failed", "assertions": [], "cleanup_errors": []}
    stage = "protected report destination"
    try:
        check(not sys.argv[1:], "This verifier accepts no alternate targets or command-line credentials")
        report_parent = report_destination()
        stage = "schema-13 deployment and existing fixture ownership"
        db = Database()
        schema = db.read("SELECT json_build_object('version',max(version),'count',count(*),"
                         "'server_version_num',current_setting('server_version_num')) FROM schema_migrations;", "Deployed database version")
        check(schema.get("version") == 13 and schema.get("count") == 13,
              "Deploy the schema-13 service before running progressive video verification")
        for name in (smoke.MARKER_NAME, smoke.STATE_NAME, smoke.LOCK_NAME):
            smoke.private_file(smoke.DIRECTORY / name)
        owned.open()
        record, _, _ = support.existing_audio(owned)
        credentials = smoke.credentials()
        secrets.extend([credentials["GOBY_SMOKE_PASSWORD"], owned.state["user_password"], db.environment["PGPASSWORD"]])
        api.request("GET", "/readyz", label="Deployed progressive-video readiness")
        pid = int(subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        check(pid > 1, "The deployed service has no active process")
        uid_line = next(line for line in Path(f"/proc/{pid}/status").read_text().splitlines() if line.startswith("Uid:"))
        uids = [int(value) for value in uid_line.split()[1:]]
        check(len(uids) == 4 and all(value > 0 for value in uids), "The deployed service must run without root process credentials")
        api.admin_login(credentials)
        secrets.extend([api.cookie, api.csrf])
        capabilities = api.request("GET", "/admin/v1/capabilities", admin=True, label="Deployed progressive-video capabilities")
        check(capabilities.get("Features", {}).get("Playback") is True and capabilities.get("Features", {}).get("Transcoding") is True and
              capabilities.get("Toolchain", {}).get("FFmpeg") == "9.0.1",
              "The deployed service lacks playback, transcoding, or the pinned FFmpeg toolchain")
        for tool in (audio.FFMPEG, audio.FFPROBE):
            check(Path(tool).is_file() and os.access(tool, os.X_OK), "The pinned remote media tool is unavailable")
        api.viewer_login(owned.state)
        secrets.append(api.token)
        scratch.create()
        summary["deployment"] = {"schema_version": 13, "probe_version": 5, "main_pid": pid, "effective_uid": uids[1],
                                 "non_root": True, "transcoding_enabled": True, "ffmpeg": "9.0.1",
                                 "postgresql_version_num": int(schema["server_version_num"]), "database": "goby_test", "database_read_only": True}

        stage = "five existing libraries normal ProbeVersion 5 upgrade"
        upgrade = ProbeFiveUpgrade(api, db, smoke, record)
        summary["upgrade_before"] = upgrade.prepare()
        summary["upgrade_scans"] = upgrade.run()
        summary["upgrade_after"] = upgrade.verify()
        item_id, original, streams, baseline = indexed_video(api, db, upgrade)
        source_id = original["Id"]
        check(sum(row.get("user_id") == api.user_id and row.get("item_id") == item_id
                  for row in upgrade.before["user_data"]) == 1,
              "The fixture viewer has no existing source user-data row; negotiation must not create one during preservation verification")
        reference_pixels = pixels(scratch, hls.MEDIA)
        check(len(reference_pixels) == 360 * PIXEL_BYTES, "The immutable source no longer contains 360 video frames at 24 FPS")
        summary["source"] = {"seconds": 15, "bytes": hls.MEDIA_SIZE, "sha256": hls.MEDIA_HASH,
                             "video_frames": 360, "format_start_known": True, "format_start_ticks": streams["media"]["FormatStartTicks"]}

        stage = "HTTP and omitted protocol defaults preserve mixed profile order"
        orders = []
        for labels, choices, expected in [
            (["http", "hls"], [video_profile("http"), video_profile("hls")], "http"),
            (["hls", "http"], [video_profile("hls"), video_profile("http")], "hls"),
            (["omitted", "hls"], [video_profile(None), video_profile("hls")], "http"),
            (["empty", "hls"], [video_profile(""), video_profile("hls")], "http"),
        ]:
            if labels[0] == "omitted":
                choices[0].pop("Context")
            elif labels[0] == "empty":
                choices[0]["Context"] = ""
            target, play_id, _ = negotiated(api, db, item_id, original, request_body(api, original, streams, choices), plays, expected)
            if expected == "http":
                head_without_job(api, db, http, target, play_id)
            orders.append({"requested_order": labels, "selected_protocol": expected})
        summary["assertions"].append({"stage": stage, "orders": orders, "negotiation_encoding_jobs_created": 0,
                                      "progressive_head_jobs_created": 0, "original_source_dto_preserved": True})

        stage = "authorized zero-start copy and explicit copy-seek rejection"
        copy_target, copy_play, query = negotiated(api, db, item_id, original,
            request_body(api, original, streams, [video_profile("http")]), plays, "http")
        retired_targets.append(copy_target)
        check(query.get("VideoCodec") == ["copy"] and query.get("AudioCodec") == ["copy"] and
              query.get("AllowVideoStreamCopy") == ["true"] and query.get("AllowAudioStreamCopy") == ["true"],
              "Authorized progressive remux was replaced by unnecessary encoding")
        head_without_job(api, db, http, copy_target, copy_play)
        headers, content = http.request("GET", copy_target, "Complete progressive H.264/AAC remux")
        video_headers(headers)
        copied = video_probe(scratch, content, "m4e-copy.mp4", reference_pixels, copied=True)
        copy_plans = db.completed(api, copy_play, 1)
        check(copy_plans[0]["plan"].get("VideoCodec") == copy_plans[0]["plan"].get("AudioCodec") == "copy",
              "The persisted remux plan differs from the negotiated physical copy")
        before_jobs = db.source_jobs(api, item_id, source_id)
        forbidden_seek = support.seek_url(copy_target, SEEK_TICKS)
        http.request("HEAD", forbidden_seek, "Explicit progressive video-copy seek HEAD", expected=(415,))
        http.request("GET", forbidden_seek, "Explicit progressive video-copy seek GET", expected=(415,))
        check(db.source_jobs(api, item_id, source_id) == before_jobs, "Rejected copied-video seek created an encoding job")
        summary["assertions"].append({"stage": stage, "full": copied, "nonzero_video_copy_seek_status": 415,
                                      "rejected_seek_jobs_created": 0})

        stage = "encoded standard stream URL full GET Range and 6.37-second seek"
        encoded_target, encoded_play, query = negotiated(api, db, item_id, original,
            request_body(api, original, streams, [video_profile("http")], encode=True), plays, "http")
        retired_targets.append(encoded_target)
        check(encoded_play == copy_play, "Same authenticated source negotiation did not reuse its canonical prepared playback")
        for key, value in {"VideoCodec": "h264", "AudioCodec": "aac", "Width": "160", "Height": "90",
                           "VideoBitrate": "300000", "AudioBitrate": "96000", "AudioChannels": "2", "AudioSampleRate": "48000",
                           "AllowVideoStreamCopy": "false", "AllowAudioStreamCopy": "false"}.items():
            check(query.get(key) == [value], "The negotiated MP4 URL omitted or weakened a concrete encoder target")
        check(not any(key.lower() == "framerate" for key in query),
              "An unrequested frame-rate conversion replaced the source timestamp-preserving plan")
        head_without_job(api, db, http, encoded_target, encoded_play)
        headers, content = http.request("GET", encoded_target, "Progressive MP4 GET with an ignored byte Range", headers={"Range": "bytes=17-31"})
        video_headers(headers)
        encoded = video_probe(scratch, content, "m4e-encoded.mp4", reference_pixels, width=160, height=90)
        db.completed(api, encoded_play, 2)
        before_jobs = db.source_jobs(api, item_id, source_id)
        retry_headers, retry = http.request("GET", encoded_target, "Identical complete progressive MP4 retry")
        video_headers(retry_headers)
        check(retry == content and db.source_jobs(api, item_id, source_id) == before_jobs,
              "Ignoring Range or retrying an identical output changed the representation or created a duplicate job")
        seek_target = support.seek_url(encoded_target, SEEK_TICKS)
        headers, content = http.request("GET", seek_target, "Same encoded MP4 URL with a 6.37-second source seek")
        video_headers(headers)
        sought = video_probe(scratch, content, "m4e-seek.mp4", reference_pixels, start_ticks=SEEK_TICKS, width=160, height=90)
        all_plans = db.completed(api, encoded_play, 3)
        plans = sorted((value for value in all_plans if value["plan"].get("VideoCodec") == "h264"),
                       key=lambda value: value["plan"].get("StartTicks", -1))
        check(len(plans) == 2 and [value["plan"].get("StartTicks") for value in plans] == [0, SEEK_TICKS] and
              all(value["plan"].get("SourceFormatStartKnown") is True and
                  value["plan"].get("SourceFormatStartTicks", 0) == streams["media"]["FormatStartTicks"] and
                  value["plan"].get("OutputMode") == "progressive" and value["plan"].get("Container") == "mp4" for value in plans),
              "The persisted MP4 plans lost the requested seek or verified private format clock")
        stable_plans = copy.deepcopy(plans)
        for entry in stable_plans:
            del entry["plan"]["StartTicks"]
        check(stable_plans[0] == stable_plans[1], "Changing only StartTimeTicks changed other output settings or source scope")
        summary["assertions"].append({"stage": stage, "full": encoded, "seek": sought, "range_ignored_status": 200,
                                      "identical_retry_reuses_job": True, "only_start_ticks_changed": True,
                                      "persisted_plan_start_ticks": [0, SEEK_TICKS], "head_jobs_created": 0})

        stage = "original byte representation and catalog facts remain unchanged"
        original_target = "/emby/Videos/" + quote(item_id) + "/stream.mp4?" + urlencode({"Static": "true",
            "MediaSourceId": source_id, "DeviceId": smoke.DEVICE_ID, "api_key": api.token})
        before_jobs = db.source_jobs(api, item_id, source_id)
        headers, original_bytes = http.request("GET", original_target, "Explicit original video byte stream")
        check(headers.get("content-type") == "video/mp4" and headers.get("content-length") == str(hls.MEDIA_SIZE) and
              headers.get("accept-ranges") == "bytes" and headers.get("etag") and
              len(original_bytes) == hls.MEDIA_SIZE and hashlib.sha256(original_bytes).hexdigest() == hls.MEDIA_HASH and
              db.source_jobs(api, item_id, source_id) == before_jobs, "Original video delivery changed source bytes or created a conversion")
        range_headers, ranged = http.request("GET", original_target, "Original video retains HTTP byte ranges",
                                             headers={"Range": "bytes=0-127"}, expected=(206,))
        check(ranged == original_bytes[:128] and range_headers.get("content-range") == f"bytes 0-127/{hls.MEDIA_SIZE}",
              "Original byte-range delivery no longer identifies the original source representation")
        item = hls.detail(api, item_id)
        check(item.get("UserData") == baseline and support.source_projection(item["MediaSources"][0]) == original,
              "Video media requests changed user playback history or original catalog media facts")
        summary["assertions"].append({"stage": stage, "original_sha256_matches": True, "original_range_status": 206,
                                      "original_get_jobs_created": 0, "original_dto_and_user_data_preserved": True})

        stage = "owned conversion cleanup without fabricated player reports"
        for play_id in plays:
            cleanup_encoding(api, db, play_id)
            cleaned.add(play_id)
        audio.wait_inactive(db, api)
        check(hls.detail(api, item_id)["UserData"] == baseline, "Deleting video encoders changed user history")
        summary["assertions"].append({"stage": stage, "owned_playback_scopes_cleaned": len(cleaned), "active_encodings_delete_status": 204,
                                      "active_owned_encoders": 0, "player_reports_sent": 0, "user_data_preserved": True,
                                      "coverage_boundary": "Live-response interruption is covered by the separate remote HTTP integration suite"})
        summary["http_response_bytes"] = http.bytes
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
    finally:
        if upgrade is not None:
            try:
                summary["cleanup_errors"].extend(upgrade.cancel_pending())
            except Exception:
                summary["cleanup_errors"].append("Owned upgrade scan cleanup could not be confirmed")
        if api.token and db is not None:
            try:
                plays = sorted(set(plays) | set(db.playbacks(api)))
            except Exception:
                summary["cleanup_errors"].append("Owned authentication playback cleanup inventory failed")
            for play_id in plays:
                if play_id in cleaned:
                    continue
                try:
                    state = db.playback(api, play_id)
                    check(state is not None, "Owned playback cleanup scope disappeared")
                    cleanup_encoding(api, db, play_id)
                    cleaned.add(play_id)
                except Exception:
                    summary["cleanup_errors"].append("Owned progressive-video playback or encoding cleanup failed")
        if api.token and original is not None:
            try:
                item = hls.detail(api, item_id)
                check(item.get("UserData") == baseline and support.source_projection(item["MediaSources"][0]) == original,
                      "Original video facts or user data changed before verification logout")
                summary["original_video_dto_and_user_data_preserved"] = True
            except Exception:
                summary["cleanup_errors"].append("Original video DTO or user-data preservation failed")
        if api.token:
            try:
                api.request("POST", "/emby/Sessions/Logout", emby=True, expected=(200,), parse=False, label="Progressive-video verifier logout")
                api.token = ""
            except Exception:
                summary["cleanup_errors"].append("Owned progressive-video authentication revocation failed")
        if db is not None and api.session_id:
            try:
                audio.wait_inactive(db, api)
                check(db.revoked(api), "The owned progressive-video authentication remains active")
                check(all(db.playback(api, play_id).get("state") == "Prepared" for play_id in plays),
                      "A transport-only verification changed persisted player activity")
                for target in retired_targets:
                    http.request("GET", target, "Progressive MP4 URL after owner authentication revocation", expected=(401,))
                summary["owned_playback_scopes_with_revoked_authentication"] = len(plays)
                summary["player_reports_sent"] = 0
                if retired_targets:
                    summary["logged_out_media_url_status"] = 401
                summary["owned_session_revoked_and_encoders_inactive"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned authentication, playback, or encoding state remained active")
        # The final semantic observation follows encoder shutdown and viewer
        # logout, closing the tail of the media-verification mutation window.
        if upgrade is not None and upgrade.prepared:
            try:
                summary["preservation_after_cleanup"] = upgrade.audit_preservation()
                if upgrade.finished:
                    summary["probe_five_after_cleanup"] = upgrade.verify()
            except Exception as error:
                detail = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
                summary["cleanup_errors"].append("Final complete semantic preservation audit failed: " + detail)
        if api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False, label="Progressive-video administrator logout")
                api.cookie = ""
            except Exception:
                summary["cleanup_errors"].append("Owned progressive-video administrator logout failed")
        try:
            scratch.cleanup()
            summary["private_tmpfs_scratch_removed"] = scratch.path is not None
        except Exception:
            summary["cleanup_errors"].append("Owned progressive-video temporary output cleanup failed")
        if owned.lock is not None:
            owned.lock.close()
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        summary["http_response_bytes"] = http.bytes
        if report_parent is not None:
            try:
                check(report_destination() == report_parent, "The approved report directory changed during verification")
                encoded = json.dumps(summary, indent=2, sort_keys=True)
                check(len(encoded.encode("utf-8")) <= MAX_REPORT_BYTES and
                      not any(value and value in encoded for value in secrets), "The sanitized report exceeds its bounds or contains a credential")
                audio.private_json(RESULT, summary, create=not RESULT.exists())
            except Exception:
                summary = {"owner": REPORT_OWNER, "status": "failed", "failed_stage": "sanitized report persistence",
                           "error": "The report could not be safely persisted", "assertions": [],
                           "cleanup_errors": ["See protected remote state; unsanitized details were not emitted"]}
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
