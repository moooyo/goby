#!/usr/bin/env python3
"""Verify deployed audio PlaybackInfo profiles and the owned ProbeVersion 4 upgrade.

Run only as root through SSH on the Linux test host. All five existing fixture
libraries are verified against ownership records before normal admin API scans.
No account, library, source media, or ownership record is created or rewritten.
Only this run's authentication sessions, conversion jobs, scan jobs, and private
temporary media are cleaned up. The persistent output is a sanitized JSON report.
"""

from __future__ import annotations

import copy
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
from urllib.parse import parse_qs, quote, urlencode, urlsplit, urlunsplit


sys.dont_write_bytecode = True
try:
    module_spec = importlib.util.spec_from_file_location(
        "goby_audio_profile_support", Path(__file__).with_name("verify-audio.py"))
    audio = importlib.util.module_from_spec(module_spec)
    module_spec.loader.exec_module(audio)
    upgrade_spec = importlib.util.spec_from_file_location(
        "goby_audio_profile_upgrade", Path(__file__).with_name("audio-profile-upgrade.py"))
    upgrade_module = importlib.util.module_from_spec(upgrade_spec)
    upgrade_spec.loader.exec_module(upgrade_module)
    hls, smoke = audio.hls, audio.smoke
except Exception:
    print(json.dumps({"status": "failed", "failed_stage": "helper import",
                      "error": "The sibling audio, HLS, direct-playback, or upgrade helper is unavailable",
                      "cleanup_errors": []}))
    raise SystemExit(1) from None


RESULT = Path("/opt/goby-test/m4d-deployed-audio-profiles.json")
REPORT_OWNER = "goby-audio-profiles-m4d-verification-v1"
MAX_REPORT_BYTES = 128 * 1024
SOURCE_FIELDS = ("Id", "ItemId", "Name", "Path", "Protocol", "Type", "Container", "Formats",
                 "RunTimeTicks", "Bitrate", "Size", "MediaStreams", "Chapters")


def check(condition: bool, label: str) -> None:
    smoke.check(condition, label)


def report_destination() -> tuple[int, int]:
    """Approve only the dedicated directory and this verifier's prior report."""
    parent_info = RESULT.parent.lstat()
    check(stat.S_ISDIR(parent_info.st_mode) and parent_info.st_uid == 0 and parent_info.st_mode & 0o022 == 0 and
          RESULT.parent.resolve(strict=True) == RESULT.parent, "The private test result directory has unsafe ownership")
    if RESULT.exists() or RESULT.is_symlink():
        smoke.private_file(RESULT)
        check(0 < RESULT.stat().st_size <= MAX_REPORT_BYTES, "The existing audio-profile report exceeds its ownership-check limit")
        previous = json.loads(RESULT.read_text(encoding="utf-8"))
        check(isinstance(previous, dict) and previous.get("owner") == REPORT_OWNER and
              previous.get("status") in {"passed", "failed"} and isinstance(previous.get("assertions"), list) and
              isinstance(previous.get("cleanup_errors"), list),
              "The existing audio-profile result is not owned by this verifier")
    return parent_info.st_dev, parent_info.st_ino


class Database(audio.Database):
    def playback(self, api, play_id: str):
        query = "SELECT COALESCE((SELECT row_to_json(v) FROM (SELECT p.id, p.state, p.item_id, p.media_source_id, " \
                "(SELECT count(*) FROM encoding_jobs e WHERE e.auth_session_id=p.auth_session_id AND e.play_session_id=p.id) AS jobs, " \
                "(SELECT COALESCE(json_agg(e.id ORDER BY e.id),'[]'::json) FROM encoding_jobs e " \
                "WHERE e.auth_session_id=p.auth_session_id AND e.play_session_id=p.id) AS job_ids, " \
                "(SELECT count(*) FROM encoding_jobs e WHERE e.auth_session_id=p.auth_session_id AND e.play_session_id=p.id " \
                "AND e.state IN ('queued','running')) AS active_jobs FROM play_sessions p WHERE p.id=" + audio.sql(play_id) + \
                " AND p.user_id=" + audio.sql(api.user_id) + " AND p.auth_session_id=" + audio.sql(api.session_id) + \
                " AND p.device_id=" + audio.sql(smoke.DEVICE_ID) + ") v),'null'::json);"
        return self.read(query, "Owned canonical audio playback and conversion state")

    def source_jobs(self, api, item_id: str, source_id: str) -> list[str]:
        query = "SELECT COALESCE(json_agg(id ORDER BY id),'[]'::json) FROM encoding_jobs WHERE user_id=" + audio.sql(api.user_id) + \
                " AND auth_session_id=" + audio.sql(api.session_id) + " AND device_id=" + audio.sql(smoke.DEVICE_ID) + \
                " AND item_id=" + audio.sql(item_id) + " AND media_source_id=" + audio.sql(source_id) + ";"
        result = self.read(query, "Owned source conversion identities before and after negotiation")
        check(isinstance(result, list) and len(result) <= 256 and
              all(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{32}", value) is not None for value in result),
              "Owned source conversion observation returned an unexpected job identity set")
        return result

    def revoked(self, api) -> bool:
        return self.read("SELECT COALESCE((SELECT to_json(revoked_at IS NOT NULL) FROM sessions WHERE id=" +
                         audio.sql(api.session_id) + " AND user_id=" + audio.sql(api.user_id) + "),'false'::json);",
                         "Owned authentication revocation") is True

    def plans(self, api, play_id: str) -> list[dict]:
        query = "SELECT COALESCE(jsonb_agg(jsonb_build_object('item_id',item_id,'media_source_id',media_source_id," \
                "'source_stamp',source_stamp,'plan',plan,'state',state,'error_code',error_code) ORDER BY id),'[]'::jsonb) " \
                "FROM encoding_jobs WHERE play_session_id=" + audio.sql(play_id) + " AND user_id=" + audio.sql(api.user_id) + \
                " AND auth_session_id=" + audio.sql(api.session_id) + " AND device_id=" + audio.sql(smoke.DEVICE_ID) + ";"
        return self.read(query, "Owned concrete audio conversion plans")


def existing_audio(owned) -> tuple[dict, dict[str, Path], dict[str, str]]:
    root = audio.ROOT
    info = root.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and
          root.resolve(strict=True) == root, "The existing audio fixture directory has unsafe ownership")
    marker = smoke.bounded_json(root / ".goby-managed")
    check(marker == {"owner": audio.OWNER, "fixture_nonce": owned.state["nonce"]},
          "The existing audio fixture marker does not match its owned viewer")
    record = smoke.bounded_json(audio.RECORD)
    check(record.get("owner") == audio.OWNER and record.get("fixture_nonce") == owned.state["nonce"] and
          record.get("root") == str(root) and record.get("viewer_id") == owned.state["user_id"] and
          re.fullmatch(r"[0-9a-f]{32}", str(record.get("library_id", ""))) is not None and
          record.get("creation_pending") is False,
          "The existing audio library has no matching completed ownership record")
    paths = {kind: root / ("Reference Audio M4c." + kind) for kind in audio.SOURCE_FACTS}
    paths["tail"] = root / audio.TAIL_NAME
    check({entry.name for entry in root.iterdir()} == {".goby-managed", *(path.name for path in paths.values())},
          "The existing audio fixture must contain exactly its five recorded sources")
    recorded_hashes = record.get("media_hashes", {})
    check(set(recorded_hashes) == {path.name for path in paths.values()}, "The audio source hash record is incomplete")
    hashes = {}
    for kind, path in paths.items():
        info = path.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and
              info.st_mode & 0o022 == 0 and info.st_mode & 0o004 != 0 and path.resolve(strict=True) == path and
              0 < info.st_size <= audio.MAX_BODY,
              "An existing audio source has unsafe ownership, permissions, or size")
        digest = audio.digest_checked(path)
        check(digest == recorded_hashes[path.name], "An existing audio source differs from its creation hash")
        if kind in audio.SOURCE_FACTS:
            expected_size, expected_hash, _ = audio.SOURCE_FACTS[kind]
            check(info.st_size == expected_size and digest == expected_hash,
                  "An original audio fixture differs from the immutable reference size or hash")
        hashes[kind] = digest
    return record, paths, hashes


def source_projection(source: dict) -> dict:
    check(isinstance(source, dict) and all(field in source for field in SOURCE_FIELDS),
          "The original media source DTO omitted required source facts")
    return copy.deepcopy({field: source[field] for field in SOURCE_FIELDS})


def assert_original_dtos(api, ids: dict, originals: dict) -> None:
    for kind, item_id in ids.items():
        item = api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items/{quote(item_id)}", emby=True,
                           label="Original audio DTO preservation after media requests")
        sources = item.get("MediaSources", [])
        check(len(sources) == 1 and source_projection(sources[0]) == originals[kind],
              "Fetching negotiated audio replaced the original catalog media DTO facts")


def indexed_audio(api, db, library_id: str, paths: dict[str, Path]) -> tuple[dict, dict, dict]:
    facts = db.read("SELECT COALESCE(json_agg(json_build_object('Id',id,'Path',path,'Media',media)),'[]'::json) "
                    "FROM items WHERE library_id=" + audio.sql(library_id) + " AND type='Audio';",
                    "Scoped version-four audio facts")
    check(isinstance(facts, list) and len(facts) == 5, "The owned audio library does not contain exactly five audio items")
    ids, streams, originals = {}, {}, {}
    for kind, path in paths.items():
        matches = [fact for fact in facts if fact.get("Path") == str(path)]
        check(len(matches) == 1, "An owned audio source has no unique catalog item")
        fact, media = matches[0], matches[0].get("Media") or {}
        candidates = [stream for stream in media.get("Streams", []) if stream.get("CodecType") == "audio"]
        check(media.get("ProbeVersion") == 4 and media.get("AudioDurationExact") is True and len(candidates) == 1 and
              (candidates[0].get("AudioTiming") or {}).get("Exact") is True,
              "All owned audio sources require ProbeVersion 4 and exact decoded timing after the upgrade")
        ids[kind], streams[kind] = fact["Id"], candidates[0]
        item = api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items/{quote(fact['Id'])}", emby=True,
                           label="Original audio DTO baseline")
        sources = item.get("MediaSources", [])
        check(len(sources) == 1 and sources[0].get("Id") == "mediasource_" + fact["Id"] and
              sources[0].get("Protocol") == "File" and sources[0].get("Path") == str(path),
              "The authorized original audio DTO does not match its indexed source")
        originals[kind] = source_projection(sources[0])
    check(streams["aac"].get("SampleRate") == 48000 and
          streams["aac"]["AudioTiming"].get("SampleCount") == audio.ADTS_SAMPLES,
          "The indexed ADTS presentation must contain exactly 289792 samples at 48000 Hz")
    return ids, streams, originals


def condition(property_name: str, value: str, comparison="Equals") -> dict:
    return {"Property": property_name, "Condition": comparison, "Value": value, "IsRequired": True}


def profile(protocol: str, container: str, codec: str) -> dict:
    result = {"Type": "Audio", "Protocol": protocol, "Context": "Streaming", "Container": container,
              "AudioCodec": codec, "MaxAudioChannels": "2"}
    if protocol == "hls":
        result.update({"SegmentLength": 3, "MinSegments": 1})
    return result


def request_body(api, original: dict, stream: dict, profiles: list[dict], *, codec_profiles=None,
                 transcoding=True, direct_stream=False, allow_copy=False, bitrate=128000) -> dict:
    return {"UserId": api.user_id, "MediaSourceId": original["Id"], "IsPlayback": True,
            "EnableDirectPlay": False, "EnableDirectStream": direct_stream, "EnableTranscoding": transcoding,
            "AllowAudioStreamCopy": allow_copy, "AudioStreamIndex": stream["Index"], "SubtitleStreamIndex": -1,
            "StartTimeTicks": 0, "MaxStreamingBitrate": bitrate, "MaxAudioChannels": 2,
            "DeviceProfile": {"Name": "Goby deployed M4d audio profile verification", "SupportedMediaTypes": "Audio",
                              "MaxStreamingBitrate": bitrate, "MusicStreamingTranscodingBitrate": bitrate,
                              "DirectPlayProfiles": [], "TranscodingProfiles": profiles,
                              "CodecProfiles": codec_profiles or []}}


def negotiated(api, db, item_id: str, original: dict, body: dict, play_ids: list[str], *, protocol: str,
               container: str, delivery="TranscodingUrl") -> tuple[str, str, dict]:
    before_jobs = db.source_jobs(api, item_id, original["Id"])
    response = api.request("POST", f"/emby/Items/{quote(item_id)}/PlaybackInfo", emby=True, body=body,
                           label="Audio profile PlaybackInfo negotiation")
    check(not response.get("ErrorCode") and len(response.get("MediaSources", [])) == 1,
          "The audio profile did not negotiate one compatible source")
    play_id = response.get("PlaySessionId", "")
    check(isinstance(play_id, str) and re.fullmatch(r"play_[0-9a-f]{32}", play_id) is not None,
          "PlaybackInfo did not return a canonical playback identity")
    state = db.playback(api, play_id)
    check(state is not None and state.get("item_id") == item_id and state.get("media_source_id") == original["Id"] and
          state.get("state") == "Prepared",
          "PlaybackInfo failed to preserve its authenticated canonical scope")
    if play_id not in play_ids:
        play_ids.append(play_id)
    check(db.source_jobs(api, item_id, original["Id"]) == before_jobs,
          "PlaybackInfo created or removed a conversion while negotiating an existing source")
    source = response["MediaSources"][0]
    check(source_projection(source) == original, "Audio negotiation replaced original media DTO facts with output facts")
    if delivery == "TranscodingUrl":
        check(source.get("SupportsTranscoding") is True and source.get("TranscodingSubProtocol") == protocol and
              source.get("TranscodingContainer") == container,
              "PlaybackInfo delivery metadata does not match the selected ordered profile")
    else:
        check(delivery == "DirectStreamUrl" and source.get("SupportsDirectStream") is True and
              source.get("SupportsTranscoding") is False and not source.get("TranscodingUrl"),
              "A DirectStream fallback was incorrectly advertised as transcoding")
    target = source.get(delivery)
    check(isinstance(target, str) and 0 < len(target) <= 8192, "PlaybackInfo omitted its bounded media URL")
    parsed = urlsplit(target)
    if protocol == "original":
        expected_path = f"/audio/{quote(item_id)}/original." + container
    else:
        expected_path = f"/emby/Audio/{quote(item_id)}/" + ("master.m3u8" if protocol == "hls" else "stream." + container)
    check(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path == expected_path,
          "PlaybackInfo did not emit the standard relative audio route")
    query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
    check(len(query) <= 64 and all(len(values) == 1 for values in query.values()) and
          query.get("DeviceId") == [smoke.DEVICE_ID] and query.get("MediaSourceId") == [original["Id"]] and
          query.get("PlaySessionId") == [play_id] and query.get("api_key") == [api.token],
          "The negotiated media URL lost its device, source, canonical playback, or authentication scope")
    check(not any(key in query for key in ("AudioSourceSampleCount", "AudioSourceSampleRate", "DurationTicks", "Path", "JobId")),
          "The negotiated URL exposed internal measurements, paths, or a private conversion identity")
    if protocol == "http":
        check(query.get("Static") == ["false"] and query.get("StartTimeTicks") == ["0"],
              "The progressive media URL omitted its concrete dynamic output settings")
    return target, play_id, query


def head_without_job(api, db, http, target: str, play_id: str, mime: str) -> None:
    before = db.playback(api, play_id)
    headers, content = http.request("HEAD", target, "Negotiated progressive audio HEAD")
    audio.progressive_headers(headers, mime)
    after = db.playback(api, play_id)
    check(not content and before is not None and after is not None and before["jobs"] == after["jobs"] and
          before["job_ids"] == after["job_ids"] and before["state"] == after["state"] and
          after["active_jobs"] == 0 and after["state"] == "Prepared",
          "Negotiated progressive HEAD started a job, changed playback state, or returned a body")


def seek_url(target: str, ticks: int) -> str:
    parsed = urlsplit(target)
    query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
    query["StartTimeTicks"] = [str(ticks)]
    return urlunsplit(("", "", parsed.path, urlencode(query, doseq=True), ""))


def main() -> int:
    if not (sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION")):
        print(json.dumps({"status": "failed", "failed_stage": "execution boundary",
                          "error": "Run only as root through SSH on the authorized Linux test host", "cleanup_errors": []}))
        return 1
    os.umask(0o077)
    api, owned, scratch = smoke.API(), smoke.OwnedFixture(), audio.Scratch()
    http = hls.HTTP(api)
    db = upgrade = None
    record = None
    paths, hashes, ids, originals = {}, {}, {}, {}
    baseline = None
    play_ids = []
    report_writable = False
    report_parent = None
    summary = {"owner": REPORT_OWNER, "status": "failed", "assertions": [], "cleanup_errors": []}
    stage = "report destination ownership"
    try:
        report_parent = report_destination()
        report_writable = True
        stage = "existing fixture and deployment ownership"
        for name in (smoke.MARKER_NAME, smoke.STATE_NAME, smoke.LOCK_NAME):
            smoke.private_file(smoke.DIRECTORY / name)
        owned.open()
        record, paths, hashes = existing_audio(owned)
        db = Database()
        schema = db.read("SELECT json_build_object('version',max(version),'count',count(*)) FROM schema_migrations;",
                         "Deployment schema")
        check(schema.get("version") == 12 and schema.get("count") == 12, "The deployed schema must be exactly 12")
        api.request("GET", "/readyz", label="Deployed audio-profile readiness")
        api.admin_login(smoke.credentials())
        capabilities = api.request("GET", "/admin/v1/capabilities", admin=True, label="Deployed audio-profile capabilities")
        check(capabilities.get("Features", {}).get("Playback") is True and capabilities.get("Features", {}).get("Transcoding") is True and
              capabilities.get("Toolchain", {}).get("FFmpeg") == "9.0.1",
              "The deployed service lacks enabled playback, transcoding, or the pinned FFmpeg toolchain")
        pid = int(subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        uid_line = next(line for line in Path(f"/proc/{pid}/status").read_text().splitlines() if line.startswith("Uid:"))
        uids = [int(value) for value in uid_line.split()[1:]]
        check(pid > 1 and len(uids) == 4 and all(value > 0 for value in uids), "The deployed Goby process must run without root credentials")
        api.viewer_login(owned.state)
        scratch.create()
        summary["deployment"] = {"schema_version": 12, "probe_version": 4, "non_root": True,
                                 "transcoding_enabled": True, "ffmpeg": "9.0.1"}

        stage = "five owned libraries normal ProbeVersion 4 upgrade"
        upgrade = upgrade_module.UpgradeVerification(api, db, smoke, record)
        summary["upgrade_before"] = upgrade.prepare()
        summary["upgrade_scans"] = upgrade.run()
        summary["upgrade_after"] = upgrade.verify()
        ids, streams, originals = indexed_audio(api, db, record["library_id"], paths)
        baseline = audio.user_data(api, ids)

        stage = "FLAC to mono 24000 Hz MP3 under codec-profile bitrate ceiling"
        mp3_constraints = [{"Type": "Audio", "Codec": "mp3", "Conditions": [
            condition("AudioChannels", "1"), condition("AudioSampleRate", "24000"),
            condition("AudioBitrate", "96000", "LessThanEqual")]}]
        body = request_body(api, originals["flac"], streams["flac"], [profile("http", "mp3", "mp3")],
                            codec_profiles=mp3_constraints)
        target, play_id, query = negotiated(api, db, ids["flac"], originals["flac"], body, play_ids, protocol="http", container="mp3")
        check(query.get("AudioCodec") == ["mp3"] and query.get("AllowAudioStreamCopy") == ["false"] and
              query.get("AudioChannels") == ["1"] and query.get("AudioSampleRate") == ["24000"] and
              0 < int(query.get("AudioBitrate", ["0"])[0]) <= 96000,
              "The concrete MP3 URL does not satisfy required channel, sample-rate, and bitrate conditions")
        head_without_job(api, db, http, target, play_id, "audio/mpeg")
        headers, content = http.request("GET", target, "Negotiated full MP3 audio")
        audio.progressive_headers(headers, "audio/mpeg")
        duration = streams["flac"]["AudioTiming"]["SampleCount"] / streams["flac"]["SampleRate"]
        check(duration > 2, "The existing FLAC fixture is too short for the fixed two-second seek")
        full = audio.audio_probe(scratch, content, "m4d-full.mp3", expected_codec="mp3", seconds=duration,
                                 channels=1, sample_rate=24000, max_bitrate=96000)
        check(db.playback(api, play_id)["jobs"] == 1, "The full MP3 request did not create exactly one owned conversion")
        sought_target = seek_url(target, 2 * smoke.TICKS)
        sought_headers, sought_content = http.request("GET", sought_target, "Same negotiated MP3 URL with a two-second seek")
        audio.progressive_headers(sought_headers, "audio/mpeg")
        sought = audio.audio_probe(scratch, sought_content, "m4d-seek.mp3", expected_codec="mp3", seconds=duration-2,
                                   channels=1, sample_rate=24000, max_bitrate=96000)
        check(db.playback(api, play_id)["jobs"] == 2, "Changing only StartTimeTicks did not produce the two owned conversion plans")
        plans = sorted(db.plans(api, play_id), key=lambda entry: entry.get("plan", {}).get("StartTicks", -1))
        check(len(plans) == 2 and [entry["plan"].get("StartTicks") for entry in plans] == [0, 2 * smoke.TICKS] and
              all(entry.get("state") == "completed" and not entry.get("error_code") and
                  entry.get("item_id") == ids["flac"] and entry.get("media_source_id") == originals["flac"]["Id"]
                  for entry in plans),
              "The two completed owned conversion plans do not record the requested source-time origins")
        stable_plans = copy.deepcopy(plans)
        for entry in stable_plans:
            del entry["plan"]["StartTicks"]
        check(stable_plans[0] == stable_plans[1], "The URL seek changed output settings, source identity, or scope beyond its start time")
        summary["assertions"].append({"stage": stage, "relative_standard_audio_url": True, "canonical_scope_preserved": True,
                                      "head_encoding_jobs_created": 0, "head_content_length_omitted": True, "codec_bitrate_ceiling": 96000,
                                      "declared_audio_bitrate": int(query["AudioBitrate"][0]), "full": full,
                                      "same_url_seek_two_seconds": sought, "persisted_plan_start_ticks": [0, 2 * smoke.TICKS],
                                      "other_plan_and_source_fields_unchanged": True,
                                      "seek_evidence_limit": "The periodic fixture does not independently identify the audible source position",
                                      "source_dto_unchanged": True})

        stage = "PlaybackInfo ADTS to WAV preserves every presentation sample"
        wav_constraints = [{"Type": "Audio", "Codec": "pcm_s16le", "Conditions": [
            condition("AudioChannels", "1"), condition("AudioSampleRate", "48000")]}]
        body = request_body(api, originals["aac"], streams["aac"], [profile("http", "wav", "pcm_s16le")],
                            codec_profiles=wav_constraints, bitrate=2_000_000)
        target, play_id, query = negotiated(api, db, ids["aac"], originals["aac"], body, play_ids, protocol="http", container="wav")
        check(query.get("AudioCodec") == ["pcm_s16le"] and query.get("AudioChannels") == ["1"] and
              query.get("AudioSampleRate") == ["48000"], "The negotiated WAV URL changed the required decoded presentation")
        head_without_job(api, db, http, target, play_id, "audio/wav")
        headers, content = http.request("GET", target, "Negotiated complete ADTS to WAV audio")
        audio.progressive_headers(headers, "audio/wav")
        wav = audio.audio_probe(scratch, content, "m4d-exact-adts.wav", expected_codec="pcm_s16le", channels=1,
                                sample_rate=48000, exact_samples=audio.ADTS_SAMPLES)
        summary["assertions"].append({"stage": stage, "head_encoding_jobs_created": 0, "source_dto_unchanged": True, **wav})

        stage = "mixed audio profile ordering"
        ordered = []
        for protocol in ("http", "hls"):
            choices = [profile("http", "mp3", "mp3"), profile("hls", "ts", "aac")]
            if protocol == "hls":
                choices.reverse()
            body = request_body(api, originals["flac"], streams["flac"], choices)
            negotiated(api, db, ids["flac"], originals["flac"], body, play_ids, protocol=protocol,
                       container="mp3" if protocol == "http" else "ts")
            ordered.append({"requested_order": [choice["Protocol"] for choice in choices], "selected_protocol": protocol})
        summary["assertions"].append({"stage": stage, "orders": ordered, "negotiation_encoding_jobs_created": 0,
                                      "source_dto_unchanged": True})

        stage = "copy delivery and explicit original fallback"
        body = request_body(api, originals["aac"], streams["aac"], [profile("http", "m4a", "aac")],
                            transcoding=True, direct_stream=False, allow_copy=True, bitrate=500000)
        target, play_id, query = negotiated(api, db, ids["aac"], originals["aac"], body, play_ids,
                                            protocol="http", container="m4a")
        check(query.get("AudioCodec") == ["copy"] and query.get("AllowAudioStreamCopy") == ["true"],
              "An authorized remux profile was not preserved as a physical stream copy")
        head_without_job(api, db, http, target, play_id, "audio/mp4")

        # Disabling transcoding explicitly permits the existing original-file
        # fallback. This single-track ADTS fixture therefore does not exercise
        # the narrower case where only a converted DirectStream is compatible.
        body = request_body(api, originals["aac"], streams["aac"], [profile("http", "m4a", "aac")],
                            transcoding=False, direct_stream=True, allow_copy=True, bitrate=500000)
        target, play_id, query = negotiated(api, db, ids["aac"], originals["aac"], body, play_ids,
                                            protocol="original", container=originals["aac"]["Container"], delivery="DirectStreamUrl")
        before = db.playback(api, play_id)
        headers, content = http.request("HEAD", target, "Explicit original fallback HEAD")
        after = db.playback(api, play_id)
        check(before is not None and after is not None and not content and headers.get("content-type") == "audio/aac" and
              headers.get("content-length") == str(originals["aac"]["Size"]) and headers.get("accept-ranges") == "bytes" and
              before["job_ids"] == after["job_ids"] and not query.get("AudioCodec"),
              "The original fallback changed its complete-file representation or created an encoding job")
        summary["assertions"].append({"stage": stage, "transcoding_enabled_copy_url": "TranscodingUrl",
                                      "transcoding_disabled_original_url": "DirectStreamUrl", "head_encoding_jobs_created": 0})
        check(audio.user_data(api, ids) == baseline, "Audio profile requests changed user playback history")
        assert_original_dtos(api, ids, originals)
        summary["original_source_dtos_unchanged"] = True
        summary["upgrade_preserved_after_media_requests"] = upgrade.verify()
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
                summary["cleanup_errors"].append("Owned upgrade scan cancellation failed")
            if upgrade.prepared:
                try:
                    summary["upgrade_preservation_on_exit"] = upgrade.audit_preservation()
                except Exception as error:
                    detail = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
                    summary["cleanup_errors"].append("Upgrade preservation audit failed: " + detail)
        for play_id in play_ids:
            if api.token:
                try:
                    audio.cleanup_encoding(api, play_id)
                except Exception:
                    summary["cleanup_errors"].append("Owned profile encoding cleanup failed")
        if api.token and baseline is not None:
            try:
                check(audio.user_data(api, ids) == baseline, "Audio user playback data changed")
                assert_original_dtos(api, ids, originals)
                summary["owned_user_data_unchanged"] = True
                summary["original_source_dtos_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("Audio user-data or original DTO preservation check failed")
        if api.token:
            try:
                api.request("POST", "/emby/Sessions/Logout", emby=True, expected=(200,), parse=False,
                            label="Audio-profile verification viewer logout")
                api.token = ""
            except Exception:
                summary["cleanup_errors"].append("Owned audio-profile login revocation failed")
        if db is not None and api.session_id:
            try:
                audio.wait_inactive(db, api)
                check(db.revoked(api), "The verification authentication session remains valid")
                summary["owned_session_revoked_and_encoders_inactive"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned authentication or conversion jobs remained active")
        if api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False,
                            label="Audio-profile verification administrator logout")
                api.cookie = ""
            except Exception:
                summary["cleanup_errors"].append("Audio-profile administrator session revocation failed")
        if hashes:
            try:
                check(all(audio.digest_checked(paths[kind]) == expected for kind, expected in hashes.items()),
                      "The immutable existing audio fixture bytes changed")
                check(smoke.bounded_json(audio.RECORD) == record, "The audio ownership record changed")
                summary["owned_audio_source_hashes_unchanged"] = hashes
            except Exception:
                summary["cleanup_errors"].append("Existing audio source or ownership-record preservation check failed")
        try:
            scratch.cleanup()
            summary["private_tmpfs_scratch_removed"] = scratch.path is not None
        except Exception:
            summary["cleanup_errors"].append("Owned audio-profile temporary file cleanup failed")
        if owned.lock is not None:
            owned.lock.close()
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        if report_writable:
            try:
                check(report_destination() == report_parent, "The approved report directory changed during verification")
                check(len(json.dumps(summary, indent=2, sort_keys=True).encode("utf-8")) <= MAX_REPORT_BYTES,
                      "The sanitized audio-profile report exceeds its size limit")
                audio.private_json(RESULT, summary, create=not RESULT.exists())
            except Exception:
                summary["status"] = "failed"
                summary["cleanup_errors"].append("Sanitized audio-profile report could not be saved privately")
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
