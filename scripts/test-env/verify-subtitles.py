#!/usr/bin/env python3
"""Verify deployed external subtitles only through SSH on Linux test-env.

The marked reference MP4, SRT, and native VTT are read-only inputs. This script
reuses the private direct-playback viewer and records its own Goby movies library
in a root-only file. Reference response bodies are read from the staged
repository. Only sanitized English JSON is printed; credential URLs, response
objects, and exception bodies are never printed.
"""

from __future__ import annotations

import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
from urllib.parse import parse_qs, quote, urlencode, urlsplit, urlunsplit


sys.dont_write_bytecode = True
try:
    spec = importlib.util.spec_from_file_location(
        "goby_direct_smoke", Path(__file__).with_name("verify-direct-playback.py"))
    smoke = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(smoke)
except Exception:
    print(json.dumps({"status": "failed", "failed_stage": "helper import",
                      "error": "The sibling direct-playback helper is unavailable", "cleanup_errors": []}))
    raise SystemExit(1) from None

ROOT = Path("/opt/goby-fixtures/subtitle-reference")
TITLE = "Reference Subtitle M3c (2026)"
FOLDER = ROOT / TITLE
MEDIA = FOLDER / (TITLE + ".mp4")
SRT = FOLDER / (TITLE + ".en.srt")
VTT = FOLDER / (TITLE + ".en.forced.vtt")
RECORD = Path("/opt/goby-test/subtitle-goby-verification.json")
OWNER = "goby-subtitle-verification-v1"
NAME = "Goby Subtitle verification"
REFERENCE = Path("/opt/goby-test/repository/tests/compatibility/fixtures/reference/emby-4.9.5.0")
CAPTURES = {
    "full": "subtitle-m3c-vtt-get.json",
    "shifted": "subtitle-m3c-vtt-query-copy-false.json",
    "copied": "subtitle-m3c-vtt-query-copy-true.json",
}
SUBTITLE_LIMIT = 1024 * 1024


def check(condition: bool, label: str) -> None:
    smoke.check(condition, label)


def source_files() -> dict[Path, str]:
    for directory in (ROOT, FOLDER):
        info = directory.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and directory.resolve(strict=True) == directory,
              "Reference subtitle directory ownership or resolved path does not match")
        check(info.st_mode & 0o005 == 0o005 and info.st_mode & 0o022 == 0,
              "Reference subtitle directories need readable and traversable permissions; no repair was attempted")
    marker = ROOT / ".goby-managed"
    info = marker.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_size <= 256 and
          marker.read_text(encoding="utf-8").strip() == "goby-subtitle-reference-owned-v1",
          "Reference subtitle ownership marker does not match")
    for path, limit in ((MEDIA, smoke.MAX_MEDIA), (SRT, SUBTITLE_LIMIT), (VTT, SUBTITLE_LIMIT)):
        info = path.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and path.resolve(strict=True) == path and
              0 < info.st_size <= limit, "A reference media or subtitle input is missing or unsafe")
        check(info.st_mode & 0o004 != 0 and info.st_mode & 0o022 == 0,
              "Reference media and subtitles need readable permissions; no repair was attempted")
    return {path: smoke.digest(path) for path in (MEDIA, SRT, VTT, marker)}


def reference_bodies() -> dict[str, bytes]:
    results = {}
    for key, name in CAPTURES.items():
        path = REFERENCE / name
        info = path.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_size <= SUBTITLE_LIMIT,
              "A required committed subtitle response fixture is unavailable")
        fixture = json.loads(path.read_text(encoding="utf-8"))
        response = fixture.get("response", {})
        check(fixture.get("reference", {}).get("product") == "Emby Server" and
              fixture.get("reference", {}).get("version") == "4.9.5.0" and
              response.get("status") == 200 and response.get("bodyType") == "text" and
              isinstance(response.get("body"), str), "A committed subtitle response fixture has an unexpected structure")
        content = response["body"].encode("utf-8")
        headers = {key.lower(): value for key, value in response.get("headers", [])}
        check(content.startswith(b"WEBVTT\n\n") and headers.get("content-type") == "text/vtt" and
              headers.get("content-length") == str(len(content)),
              "Committed subtitle response bytes do not match their recorded headers")
        results[key] = content
    check(results["full"] != results["shifted"] != results["copied"],
          "Committed subtitle window controls must contain distinct representations")
    return results


def save_record(value: dict, *, create=False) -> None:
    if create:
        descriptor = os.open(RECORD, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        return
    smoke.private_file(RECORD)
    descriptor, temporary = tempfile.mkstemp(prefix=".goby-subtitle-verification-", dir=RECORD.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, RECORD)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def owned_library(api) -> dict:
    listed = api.request("GET", "/admin/v1/libraries", admin=True,
                         label="Subtitle library ownership lookup")["Items"]
    matching = [entry for entry in listed if entry.get("Name") == NAME or str(ROOT) in entry.get("Paths", [])]
    check(len(matching) <= 1, "Subtitle verification library ownership is ambiguous")
    if RECORD.exists() or RECORD.is_symlink():
        record = smoke.bounded_json(RECORD)
        check(record.get("owner") == OWNER and record.get("name") == NAME and
              record.get("root") == str(ROOT) and record.get("viewer_id") == api.user_id,
              "Private subtitle library record does not match this verification")
    else:
        check(not matching, "An unrecorded Goby library already uses the reference subtitle fixture or name")
        record = {"owner": OWNER, "name": NAME, "root": str(ROOT), "viewer_id": api.user_id,
                  "library_id": "", "creation_pending": True}
        save_record(record, create=True)
    if matching:
        library = matching[0]
        check(record.get("library_id") == library.get("Id") or
              (not record.get("library_id") and record.get("creation_pending") is True),
              "Existing subtitle library has no matching creation record")
    else:
        check(not record.get("library_id") and record.get("creation_pending") is True,
              "Previously recorded subtitle library is missing")
        library = api.request("POST", "/admin/v1/libraries", admin=True, expected=(201,),
                              label="Owned subtitle movies library creation", body={
                                  "Name": NAME, "CollectionType": "movies", "Paths": [str(ROOT)], "Scan": False,
                              })["Library"]
    check(library.get("Name") == NAME and library.get("Paths") == [str(ROOT)] and
          library.get("CollectionType") == "movies", "Owned subtitle library configuration does not match")
    record["library_id"] = library["Id"]
    record["creation_pending"] = False
    save_record(record)
    return library


def media_source(item: dict) -> dict:
    sources = item.get("MediaSources", [])
    check(len(sources) == 1 and isinstance(sources[0], dict),
          "Subtitle item must contain exactly one original media source")
    return sources[0]


def delivery_url(api, item_id: str, source_id: str, track: dict, output_format: str) -> str:
    value = track.get("DeliveryUrl", "")
    check(isinstance(value, str) and bool(value), "An external subtitle is missing its delivery URL")
    parsed = urlsplit(value)
    expected_path = (f"/Videos/{quote(item_id)}/{quote(source_id)}/Subtitles/"
                     f"{track['Index']}/0/Stream.{output_format}")
    query = parse_qs(parsed.query, keep_blank_values=True)
    check(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path == expected_path and
          query == {"api_key": [api.token]},
          "Subtitle URL must use the indexed selectors and the current viewer token")
    return value


def subtitle_tracks(api, item_id: str, source_id: str, descriptor: dict, *, converted=False) -> dict:
    streams = descriptor.get("MediaStreams", [])
    tracks = [stream for stream in streams if stream.get("Type") == "Subtitle" and stream.get("IsExternal") is True]
    check(len(tracks) == 2 and {track.get("Codec") for track in tracks} == {"srt", "vtt"},
          "Indexed media must expose exactly two external subtitles with native SRT and VTT codecs")
    result = {track["Codec"]: track for track in tracks}
    indexes = [track.get("Index") for track in tracks]
    check(all(type(index) is int and index >= 0 for index in indexes) and len(set(indexes)) == 2,
          "External subtitle indexes must be distinct non-negative integers")
    embedded_indexes = [stream.get("Index") for stream in streams if stream.get("Type") in {"Video", "Audio"}]
    check(embedded_indexes and all(type(index) is int for index in embedded_indexes) and
          min(indexes) > max(embedded_indexes), "External subtitle indexes overlap the primary media streams")
    for codec, path, forced in (("srt", SRT, False), ("vtt", VTT, True)):
        track = result[codec]
        check(track.get("Language") == "en" and track.get("IsForced") is forced and
              track.get("IsDefault") is False and track.get("IsTextSubtitleStream") is True and
              track.get("SupportsExternalStream") is True and track.get("DeliveryMethod") == "External" and
              track.get("Protocol") == "File" and track.get("Path") == str(path),
              "External subtitle language, forced flag, source path, or delivery metadata does not match")
        delivery_url(api, item_id, source_id, track, "vtt" if converted and codec == "srt" else codec)
    return result


def representation(api, target: str, expected: bytes, mime: str, label: str, *, head=True) -> dict:
    headers, content = api.request("GET", target, label=label + " GET", parse=False, limit=SUBTITLE_LIMIT)
    check(content == expected and headers.get("content-type") == mime and
          headers.get("content-length") == str(len(expected)) and bool(headers.get("etag")),
          label + ": bytes, MIME, length, or ETag did not match")
    if head:
        head_headers, body = api.request("HEAD", target, label=label + " HEAD", parse=False)
        check(not body and all(head_headers.get(key) == headers.get(key) for key in
                               ("content-type", "content-length", "etag")),
              label + ": HEAD did not preserve GET metadata with an empty body")
    return headers


def main() -> int:
    os.umask(0o077)
    api, owned = smoke.API(), smoke.OwnedFixture()
    summary = {"status": "failed", "assertions": [], "cleanup_errors": []}
    hashes = {}
    active_job = ""
    library_id = ""
    stage = "preconditions"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
              "Run as root through SSH only on the authorized Linux test-env host")
        hashes = source_files()
        references = reference_bodies()
        srt_bytes, vtt_bytes = SRT.read_bytes(), VTT.read_bytes()
        check(vtt_bytes == references["full"], "The native VTT source differs from the committed full VTT control")
        check((smoke.DIRECTORY / smoke.STATE_NAME).is_file(), "The reusable direct-playback viewer has not been prepared")
        owned.open()
        check(bool(owned.state.get("user_id")), "The private reusable viewer record has no owned account")
        api.request("GET", "/readyz", label="Deployed Goby readiness")
        api.admin_login(smoke.credentials())
        api.viewer_login(owned.state)
        library = owned_library(api)
        library_id = library["Id"]
        stage = "native movie and external subtitle scan"
        job = api.request("POST", f"/admin/v1/libraries/{quote(library_id)}/scan", admin=True,
                          expected=(202,), label="Owned subtitle library scan")["Job"]
        active_job = job["Id"]
        job = api.wait_job(active_job)
        active_job = ""
        check(job.get("Status") == "completed" and job.get("Scanned") == 1 and not job.get("Error"),
              "The owned subtitle library scan did not inspect exactly one clean movie")
        query = urlencode({"ParentId": library_id, "Recursive": "true", "IncludeItemTypes": "Movie",
                           "Fields": "Path,MediaSources,MediaStreams", "Limit": 10})
        listed = api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items?" + query,
                             emby=True, label="Owned subtitle movie lookup")
        check(listed.get("TotalRecordCount") == 1 and len(listed.get("Items", [])) == 1 and
              listed["Items"][0].get("Path") == str(MEDIA), "Owned subtitle library did not return the expected movie")
        item_id = listed["Items"][0]["Id"]
        item = api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items/{quote(item_id)}",
                           emby=True, label="Owned subtitle movie detail")
        source = media_source(item)
        source_id = source.get("Id", "")
        check(source_id == "mediasource_" + item_id and source.get("Size") == MEDIA.stat().st_size and
              source.get("RunTimeTicks") == 600 * smoke.TICKS and source.get("Container") == "mp4",
              "Subtitle movie detail does not contain the actual ten-minute MP4 probe facts")
        streams = source.get("MediaStreams", [])
        check(any(stream.get("Type") == "Video" and stream.get("Codec") == "h264" for stream in streams) and
              any(stream.get("Type") == "Audio" and stream.get("Codec") == "aac" for stream in streams),
              "Subtitle movie detail is missing the actual H.264/AAC media streams")
        top_tracks = subtitle_tracks(api, item_id, source_id, item)
        source_tracks = subtitle_tracks(api, item_id, source_id, source)
        check(top_tracks == source_tracks, "Top-level and media-source external subtitle descriptors differ")
        summary["assertions"].append({"stage": stage, "movies": 1, "external_tracks": 2,
                                      "native_codecs": ["srt", "vtt"], "language": "en", "native_vtt_forced": True,
                                      "credentialed_descriptor_levels": 2})

        stage = "original SRT and native VTT GET HEAD"
        srt_url = delivery_url(api, item_id, source_id, source_tracks["srt"], "srt")
        representation(api, srt_url, srt_bytes, "text/plain", "Original SRT")
        vtt_url = delivery_url(api, item_id, source_id, source_tracks["vtt"], "vtt")
        representation(api, vtt_url, vtt_bytes, "text/vtt", "Native VTT")
        summary["assertions"].append({"stage": stage, "get": 200, "head": 200,
                                      "original_srt_bytes_preserved": True, "native_vtt_bytes_preserved": True,
                                      "srt_mime": "text/plain", "vtt_mime": "text/vtt"})

        stage = "selected SRT with a VTT-only external profile"
        negotiated = api.request("POST", f"/emby/Items/{quote(item_id)}/PlaybackInfo", emby=True,
                                 label="VTT-only external subtitle negotiation", body={
                                     "UserId": api.user_id, "MediaSourceId": source_id,
                                     "SubtitleStreamIndex": source_tracks["srt"]["Index"], "IsPlayback": True,
                                     "DeviceProfile": {"Name": "Goby VTT external subtitle verification",
                                         "DirectPlayProfiles": [{"Type": "Video", "Container": "mp4",
                                                                  "VideoCodec": "h264", "AudioCodec": "aac"}],
                                         "SubtitleProfiles": [{"Format": "vtt", "Method": "External"}]},
                                 })
        planned = media_source(negotiated)
        check(bool(negotiated.get("PlaySessionId")) and "ErrorCode" not in negotiated and
              planned.get("SupportsDirectPlay") is True and planned.get("SupportsDirectStream") is True and
              planned.get("SupportsTranscoding") is False and "TranscodingUrl" not in planned and
              "DefaultSubtitleStreamIndex" not in planned,
              "External subtitle conversion did not retain supported original playback without an embedded default")
        planned_tracks = subtitle_tracks(api, item_id, source_id, planned, converted=True)
        selected = planned_tracks["srt"]
        check(selected["Index"] == source_tracks["srt"]["Index"] and selected.get("Codec") == "srt",
              "VTT negotiation changed the selected subtitle index or native SRT codec")
        converted_url = delivery_url(api, item_id, source_id, selected, "vtt")
        converted_headers = representation(api, converted_url, references["full"], "text/vtt", "Reference-matched SRT to VTT")
        summary["assertions"].append({"stage": stage, "native_codec": "srt", "delivery_format": "vtt",
                                      "direct_play": True, "direct_stream": True, "transcoding": False,
                                      "embedded_default_omitted": True, "full_reference_body_matches": True})

        stage = "reference VTT windows and timestamp copying"
        parsed = urlsplit(converted_url)
        for copy, capture in ((False, "shifted"), (True, "copied")):
            parameters = parse_qs(parsed.query, keep_blank_values=True)
            parameters.update({"StartPositionTicks": [str(10 * smoke.TICKS)],
                               "EndPositionTicks": [str(20 * smoke.TICKS)],
                               "CopyTimestamps": [str(copy).lower()]})
            target = urlunsplit(("", "", parsed.path, urlencode(parameters, doseq=True), ""))
            representation(api, target, references[capture], "text/vtt", "Reference-matched VTT window", head=False)
        summary["assertions"].append({"stage": stage, "start_seconds": 10, "end_seconds": 20,
                                      "copy_timestamps_false_matches": True, "copy_timestamps_true_matches": True})

        stage = "authorization before conditional subtitle responses"
        conditional = {"If-None-Match": converted_headers["etag"]}
        headers, body = api.request("GET", converted_url, headers=conditional, expected=(304,),
                                    parse=False, label="Authenticated conditional subtitle GET")
        check(not body and headers.get("etag") == converted_headers["etag"],
              "Authenticated conditional subtitle GET did not retain its ETag and empty body")
        # The generated URL already carries api_key. Removing every query
        # parameter is necessary to exercise an unauthenticated route.
        no_token_url = urlunsplit(("", "", parsed.path, "", ""))
        api.request("GET", no_token_url, headers=conditional, expected=(401,), parse=False,
                    label="No-token conditional subtitle GET")
        invalid_url = urlunsplit(("", "", parsed.path, urlencode({"api_key": "invalid-goby-subtitle-token"}), ""))
        api.request("GET", invalid_url, headers=conditional, expected=(401,), parse=False,
                    label="Invalid-token conditional subtitle GET")
        summary["assertions"].append({"stage": stage, "authenticated_not_modified": 304,
                                      "all_query_parameters_removed_no_token": 401, "invalid_token": 401,
                                      "intentional_reference_difference": "Goby requires authentication for subtitle delivery"})
        api.request("GET", "/readyz", label="Goby readiness after subtitle verification")
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
    finally:
        if active_job and api.cookie:
            try:
                api.request("POST", f"/admin/v1/jobs/{quote(active_job)}/cancel", admin=True,
                            expected=(202,), label="Owned subtitle scan cancellation")
                api.wait_job(active_job, timeout=10)
            except Exception:
                summary["cleanup_errors"].append("Owned subtitle scan cancellation failed")
        if api.token:
            try:
                api.request("POST", "/emby/Sessions/Logout", emby=True, parse=False,
                            label="Subtitle viewer logout")
                summary["owned_viewer_session_revoked"] = True
            except Exception:
                summary["cleanup_errors"].append("Subtitle viewer session revocation failed")
        if api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False,
                            label="Subtitle administrator logout")
                summary["administrator_session_revoked"] = True
            except Exception:
                summary["cleanup_errors"].append("Subtitle administrator session revocation failed")
        if hashes:
            try:
                check(all(smoke.digest(path) == original for path, original in hashes.items()),
                      "Reference subtitle input bytes changed")
                summary["source_bytes_unchanged"] = True
                summary["preserved_media_and_subtitle_files"] = 3
                summary["ownership_marker_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("Reference source preservation check failed")
        if library_id:
            try:
                smoke.private_file(RECORD)
                summary["owned_movies_library_retained"] = True
                summary["private_record_mode"] = "0600"
            except Exception:
                summary["cleanup_errors"].append("Private subtitle ownership record check failed")
        if owned.lock is not None:
            owned.lock.close()
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
