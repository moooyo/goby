#!/usr/bin/env python3
"""Verify deployed HLS using owned immutable media only on Linux test-env.

The existing reference MP4 is never modified. A private record binds this
script's Goby library to the existing owned viewer. HTTP bodies, subprocess
output, and observation times are bounded. FFmpeg reads a private master file
whose media URI resolves to the real authenticated Goby HTTP playlist; no
credential URL is placed in argv or printed. All scratch files are removed.
"""

from __future__ import annotations

import importlib.util
import json
import math
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys
import tempfile
from urllib.parse import parse_qs, quote, urlencode, urljoin, urlsplit, urlunsplit


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

ROOT = Path("/opt/goby-fixtures/hls-reference")
MEDIA = ROOT / "Reference HLS Normal (2026).mp4"
MEDIA_SIZE = 967651
MEDIA_HASH = "332bce27f1e97d71d3ef7cffa59d8cf70cbcc51380a6b600e05048107432de4d"
SOURCE_MARKER = "goby-hls-normal-owned-v1"
RECORD = Path("/opt/goby-test/hls-goby-verification.json")
OWNER = "goby-hls-verification-v1"
LIBRARY_NAME = "Goby HLS verification"
FFMPEG = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
FFPROBE = FFMPEG.with_name("ffprobe")
MAX_BODY = 3 * 1024 * 1024
MAX_PROCESS_OUTPUT = 2 * 1024 * 1024
MAX_TOTAL_HTTP = 24 * 1024 * 1024


def check(condition: bool, label: str) -> None:
    smoke.check(condition, label)


def source_hashes() -> dict[Path, str]:
    info = ROOT.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and ROOT.resolve(strict=True) == ROOT and
          info.st_mode & 0o005 == 0o005 and info.st_mode & 0o022 == 0,
          "The marked HLS source directory is missing or has unsafe permissions")
    marker = ROOT / ".goby-managed"
    info = marker.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_size <= 256 and
          marker.read_text(encoding="utf-8").strip() == SOURCE_MARKER, "HLS reference ownership marker does not match")
    info = MEDIA.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and MEDIA.resolve(strict=True) == MEDIA and
          info.st_size == MEDIA_SIZE and info.st_mode & 0o004 != 0 and info.st_mode & 0o022 == 0,
          "The normal-frame-rate HLS source size, ownership, or permissions do not match")
    check(smoke.digest(MEDIA) == MEDIA_HASH, "The immutable HLS reference source hash does not match")
    return {MEDIA: MEDIA_HASH, marker: smoke.digest(marker)}


def save_record(value: dict, *, create=False) -> None:
    if create:
        descriptor = os.open(RECORD, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        return
    smoke.private_file(RECORD)
    descriptor, temporary = tempfile.mkstemp(prefix=".goby-hls-record-", dir=RECORD.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, sort_keys=True)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, RECORD)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def owned_library(api, owned) -> str:
    libraries = api.request("GET", "/admin/v1/libraries", admin=True, label="HLS library ownership lookup")["Items"]
    matching = [entry for entry in libraries if entry.get("Name") == LIBRARY_NAME or str(ROOT) in entry.get("Paths", [])]
    check(len(matching) <= 1, "HLS library ownership is ambiguous")
    if RECORD.exists() or RECORD.is_symlink():
        record = smoke.bounded_json(RECORD)
        check(record.get("owner") == OWNER and record.get("root") == str(ROOT) and
              record.get("name") == LIBRARY_NAME and record.get("source_sha256") == MEDIA_HASH and
              record.get("viewer_id") == api.user_id and record.get("fixture_nonce") == owned.state["nonce"],
              "Private HLS library record does not match its source and owner")
    else:
        check(not matching, "An unrecorded Goby library already uses the HLS source or verification name")
        record = {"owner": OWNER, "root": str(ROOT), "name": LIBRARY_NAME, "source_sha256": MEDIA_HASH,
                  "viewer_id": api.user_id, "fixture_nonce": owned.state["nonce"],
                  "library_id": "", "creation_pending": True}
        save_record(record, create=True)
    if matching:
        library = matching[0]
        check(record.get("library_id") == library.get("Id") or
              (not record.get("library_id") and record.get("creation_pending") is True),
              "Existing HLS library has no matching ownership record")
    else:
        check(not record.get("library_id") and record.get("creation_pending") is True,
              "Previously recorded HLS library is missing")
        library = api.request("POST", "/admin/v1/libraries", admin=True, expected=(201,),
                              label="Owned HLS movies library creation", body={"Name": LIBRARY_NAME,
                              "CollectionType": "movies", "Paths": [str(ROOT)], "Scan": False})["Library"]
    check(library.get("Name") == LIBRARY_NAME and library.get("Paths") == [str(ROOT)] and
          library.get("CollectionType") == "movies", "Owned HLS library configuration changed")
    record["library_id"], record["creation_pending"] = library["Id"], False
    save_record(record)
    return library["Id"]


class Scratch:
    def __init__(self) -> None:
        self.path = None
        self.names = set()
        self.diagnostics = []
        self.marker = OWNER + ":" + secrets.token_hex(16)

    def create(self) -> None:
        self.path = Path(tempfile.mkdtemp(prefix="goby-hls-verification-", dir="/dev/shm"))
        self.path.chmod(0o700)
        self.write(".goby-managed", self.marker.encode("ascii"))

    def write(self, name: str, content: bytes) -> Path:
        check(self.path is not None and Path(name).name == name and len(content) <= MAX_BODY,
              "Scratch output must use a bounded owned filename")
        target = self.path / name
        descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        self.names.add(name)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(content)
        return target

    def run(self, arguments: list[str], label: str, *, timeout=30) -> bytes:
        import resource

        def limits() -> None:
            resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_PROCESS_OUTPUT, MAX_PROCESS_OUTPUT))

        # Unlinked private output files keep credential-bearing FFmpeg errors
        # off terminal output while enforcing an OS-level output size bound.
        with tempfile.TemporaryFile(dir=self.path) as output, tempfile.TemporaryFile(dir=self.path) as errors:
            result = subprocess.run(arguments, stdin=subprocess.DEVNULL, stdout=output, stderr=errors,
                                    timeout=timeout, preexec_fn=limits)
            output.seek(0)
            body = output.read(MAX_PROCESS_OUTPUT + 1)
            errors.seek(0)
            error_body = errors.read(MAX_PROCESS_OUTPUT + 1)
            check(len(body) <= MAX_PROCESS_OUTPUT and len(error_body) <= MAX_PROCESS_OUTPUT,
                  label + ": process output exceeded its limit")
            categories = []
            for needle, name in ((b"unrecognized option", "unsupported_option"),
                                 (b"option not found", "unsupported_option"),
                                 (b"not on whitelist", "protocol_whitelist"),
                                 (b"filename extension", "playlist_extension"),
                                 (b"allowed_segment_extensions", "allowed_segment_extensions"),
                                 (b"allowed_extensions", "allowed_extensions"),
                                 (b"extension mismatch", "extension_mismatch"),
                                 (b"detected format", "detected_format"),
                                 (b"error when loading first segment", "first_segment_load"),
                                 (b"failed to reload playlist", "playlist_reload"),
                                 (b"invalid data found", "invalid_media_data"),
                                 (b"error opening input", "input_open"),
                                 (b"error opening output", "output_open"),
                                 (b"non monotonically increasing dts", "non_monotonic_dts")):
                if needle in error_body.lower():
                    categories.append(name)
            http_error = re.search(rb"HTTP error ([1-5][0-9][0-9])", error_body)
            if http_error:
                categories.append("http_" + http_error.group(1).decode("ascii"))
            category = ",".join(categories) or "decode_or_transport_error"
            if result.returncode != 0 or error_body:
                descriptor, diagnostic = tempfile.mkstemp(prefix="m4b-hls-ffmpeg-", suffix=".stderr", dir="/opt/goby-test")
                with os.fdopen(descriptor, "wb") as stream:
                    stream.write(error_body)
                self.diagnostics.append(diagnostic)
            check(result.returncode == 0, label + f": media process failed (exit {result.returncode}, {category})")
            check(not error_body, label + ": media process reported " + category)
            return body

    def cleanup(self) -> None:
        if self.path is None:
            return
        check(not self.path.is_symlink() and self.path.resolve(strict=True).parent == Path("/dev/shm") and
              self.path.name.startswith("goby-hls-verification-"), "Scratch cleanup target ownership does not match")
        check((self.path / ".goby-managed").read_text(encoding="ascii") == self.marker,
              "Scratch cleanup ownership marker does not match")
        check({entry.name for entry in self.path.iterdir()} == self.names, "Unexpected files appeared in the owned HLS scratch directory")
        for name in self.names:
            smoke.private_file(self.path / name)
        for name in self.names - {".goby-managed"}:
            (self.path / name).unlink()
        (self.path / ".goby-managed").unlink()
        self.path.rmdir()


class HTTP:
    def __init__(self, api) -> None:
        self.api = api
        self.bytes = 0

    def request(self, method: str, target: str, label: str, *, expected=(200,), headers=None):
        metadata, body = self.api.request(method, target, label=label, expected=expected,
                                          headers=headers, parse=False, limit=MAX_BODY)
        self.bytes += len(body)
        check(self.bytes <= MAX_TOTAL_HTTP, "HLS HTTP bodies exceeded the total verification limit")
        return metadata, body


def resolve(parent: str, child: str, token: str) -> str:
    check(isinstance(child, str) and 0 < len(child) <= 8192,
          "HLS resource references must be nonempty bounded strings")
    source = urlsplit(child)
    check(not source.scheme and not source.netloc and not source.fragment and len(child) <= 8192,
          "HLS playlists must use bounded relative HTTP resource references")
    parsed = urlsplit(urljoin(smoke.ORIGIN + parent, child))
    check(parsed.scheme == "http" and parsed.hostname == "127.0.0.1" and parsed.port == 18096 and
          not parsed.username and not parsed.password and not parsed.fragment,
          "HLS resource resolved outside the authorized Goby loopback origin")
    check(parse_qs(parsed.query, keep_blank_values=True).get("api_key") == [token],
          "Every emitted HLS resource must carry its own authenticated query token")
    return urlunsplit(("", "", parsed.path, parsed.query, ""))


def playlist(body: bytes) -> tuple[list[str], list[str]]:
    content = body.decode("utf-8")
    check(content.startswith("#EXTM3U\n"), "HLS response is not an M3U8 playlist")
    lines = [line.strip() for line in content.splitlines() if line.strip()]
    check(not any("URI=" in line or line.startswith(("#EXT-X-KEY", "#EXT-X-MAP")) for line in lines),
          "The bounded TS verification does not accept additional playlist resource types")
    return lines, [line for line in lines if not line.startswith("#")]


def graph(api, http: HTTP, item_id: str, source_id: str, audio_index: int, start_seconds: int, jobs: list) -> dict:
    request = {"UserId": api.user_id, "MediaSourceId": source_id, "IsPlayback": True,
               "EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": True,
               "AllowVideoStreamCopy": False, "AllowAudioStreamCopy": True,
               "AudioStreamIndex": audio_index, "SubtitleStreamIndex": -1,
               "StartTimeTicks": start_seconds * smoke.TICKS,
               "DeviceProfile": {"Name": "Goby HLS video verification", "MaxStreamingBitrate": 2_000_000,
                   "DirectPlayProfiles": [], "TranscodingProfiles": [{"Type": "Video", "Container": "ts",
                       "Protocol": "hls", "Context": "Streaming", "VideoCodec": "h264", "AudioCodec": "aac",
                       "MaxWidth": 160, "MaxAudioChannels": "2", "SegmentLength": 3, "MinSegments": 1}]}}
    result = api.request("POST", f"/emby/Items/{quote(item_id)}/PlaybackInfo", emby=True,
                         body=request, label="Forced H.264 TS HLS negotiation")
    job = {"api": api, "play_id": result.get("PlaySessionId", ""), "retired": False}
    if job["play_id"]:
        jobs.append(job)
    sources = result.get("MediaSources", [])
    check(bool(job["play_id"]) and len(sources) == 1 and isinstance(sources[0], dict) and "ErrorCode" not in result,
          "HLS PlaybackInfo omitted its playable source or session")
    source = sources[0]
    check(source.get("SupportsDirectPlay") is False and source.get("SupportsDirectStream") is False and
          source.get("SupportsTranscoding") is True and source.get("TranscodingContainer") == "ts" and
          source.get("TranscodingSubProtocol") == "hls", "PlaybackInfo did not advertise the forced HLS conversion")
    master = resolve("/", source.get("TranscodingUrl", ""), api.token)
    selectors = parse_qs(urlsplit(master).query)
    hls_id = selectors.get("GobyHlsId", [""])[0]
    check(bool(hls_id) and selectors.get("PlaySessionId") == [job["play_id"]] and
          selectors.get("MediaSourceId") == [source_id] and selectors.get("StartTimeTicks") == [str(start_seconds * smoke.TICKS)],
          "HLS master URL omitted its immutable owner, source, or start hint")
    metadata, master_body = http.request("GET", master, "Authenticated HLS master")
    check(metadata.get("content-type") == "application/vnd.apple.mpegurl" and
          metadata.get("content-length") == str(len(master_body)), "HLS master MIME or length does not match")
    master_lines, main_references = playlist(master_body)
    check(len(main_references) == 1 and any("RESOLUTION=160x90" in line for line in master_lines),
          "HLS master must describe one 160x90 variant")
    main = resolve(master, main_references[0], api.token)
    metadata, main_body = http.request("GET", main, "Authenticated HLS media playlist")
    check(metadata.get("content-type") == "application/vnd.apple.mpegurl" and
          metadata.get("content-length") == str(len(main_body)), "HLS media playlist MIME or length does not match")
    lines, child_references = playlist(main_body)
    durations = [float(line.split(":", 1)[1].split(",", 1)[0]) for line in lines if line.startswith("#EXTINF:")]
    targets = [int(line.split(":", 1)[1]) for line in lines if line.startswith("#EXT-X-TARGETDURATION:")]
    check(len(child_references) == len(durations) == 5 and all(math.isfinite(value) and abs(value - 3) <= 0.01 for value in durations) and
          abs(sum(durations) - 15) <= 0.01 and "#EXT-X-MEDIA-SEQUENCE:0" in lines and
          "#EXT-X-PLAYLIST-TYPE:VOD" in lines and lines[-1] == "#EXT-X-ENDLIST" and
          len(targets) == 1 and targets[0] >= max(round(value) for value in durations),
          "HLS media playlist did not retain the full fifteen-second VOD segment table")
    # Independently muxed TS segments reset continuity counters. The public
    # playlist must declare every noninitial boundary for HLS clients as well.
    discontinuities, segment_number = [], 0
    for line in lines:
        if line == "#EXT-X-DISCONTINUITY":
            discontinuities.append(segment_number)
        elif not line.startswith("#"):
            segment_number += 1
    check(discontinuities == [1, 2, 3, 4],
          "HLS media playlist must declare exactly one discontinuity before each noninitial segment")
    hints = [line for line in lines if line.startswith("#EXT-X-START:")]
    expected_hint = f"#EXT-X-START:TIME-OFFSET={start_seconds}.0000000,PRECISE=YES"
    check(hints == ([expected_hint] if start_seconds else []), "Initial HLS seek position is not a full-VOD start hint")
    children = [resolve(main, child, api.token) for child in child_references]
    for number, child in enumerate(children):
        parsed = urlsplit(child)
        check(parsed.path.endswith(f"/hls1/{hls_id}/{number}.ts") and
              parse_qs(parsed.query).get("PlaySessionId") == [job["play_id"]],
              "HLS children do not retain source-global numbering and playback ownership")
    job.update({"hls_id": hls_id, "master": master, "main": main, "children": children,
                "durations": durations, "master_lines": master_lines, "discontinuities": discontinuities})
    return job


def progress(body: bytes, expected_seconds: float, expected_frames: int, label: str) -> dict:
    values = {}
    for line in body.decode("utf-8").splitlines():
        key, separator, value = line.partition("=")
        if separator:
            values[key] = value.strip()
    frames = int(values.get("frame", "0"))
    seconds = int(values.get("out_time_us", "0")) / 1_000_000
    check(values.get("progress") == "end" and abs(frames - expected_frames) <= 1 and
          math.isfinite(seconds) and abs(seconds - expected_seconds) <= 0.15,
          label + f": complete decode mismatch (frames {frames}, seconds {seconds:.6f})")
    return {"video_frames": frames, "decoded_seconds": round(seconds, 6)}


def decode_arguments(path: Path, *, hls=False, seek=0) -> list[str]:
    args = [str(FFMPEG), "-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-threads", "1"]
    if hls:
        args += ["-protocol_whitelist", "file,http,tcp,pipe", "-rw_timeout", "15000000",
                 "-allowed_extensions", "m3u8,ts", "-prefer_x_start", "0", "-f", "hls"]
    if seek:
        args += ["-ss", str(seek)]
    args += ["-i", str(path), "-map", "0:v:0", "-map", "0:a:0", "-threads", "1",
             "-fps_mode", "passthrough", "-progress", "pipe:1", "-nostats", "-f", "null", "-"]
    return args


def probe_segment(scratch: Scratch, wire: bytes, number: int, duration: float) -> dict:
    path = scratch.write(f"received-segment-{number}.ts", wire)
    encoded = scratch.run([str(FFPROBE), "-v", "error", "-show_streams", "-show_packets",
                           "-show_entries", "stream=index,codec_name,codec_type,width,height,start_time,duration:packet=stream_index,pts_time,dts_time,flags",
                           "-of", "json", str(path)], "Downloaded segment ffprobe", timeout=20)
    result = json.loads(encoded)
    streams = result.get("streams", [])
    check(len(streams) == 2 and {entry.get("codec_type") for entry in streams} == {"video", "audio"},
          "Downloaded TS segment must contain both audio and video")
    facts = {entry["codec_type"]: entry for entry in streams}
    check(facts["video"].get("codec_name") == "h264" and facts["audio"].get("codec_name") == "aac" and
          (facts["video"].get("width"), facts["video"].get("height")) == (160, 90),
          "Downloaded TS codecs or resized dimensions differ from negotiation")
    starts = {}
    for kind, stream in facts.items():
        start, span = float(stream["start_time"]), float(stream["duration"])
        check(math.isfinite(start) and math.isfinite(span) and abs(start - (1 + number * 3)) <= 0.15 and
              abs(span - duration) <= 0.15,
              f"Downloaded segment {number} {kind} global timeline mismatch (start {start:.6f}, duration {span:.6f})")
        packets = [packet for packet in result.get("packets", []) if packet.get("stream_index") == stream["index"]]
        check(packets and abs(float(packets[0]["pts_time"]) - start) <= 0.05,
              "Downloaded TS packet timestamps do not match stream start metadata")
        if kind == "video":
            check("K" in packets[0].get("flags", "") and abs(start - (1 + number * 3)) <= 1 / 24 + 0.005,
                  "Source-global video start or independently decodable keyframe did not match")
        starts[kind] = round(start, 6)
    decoded = progress(scratch.run(decode_arguments(path), "Downloaded complete A/V segment decode", timeout=20),
                       duration, round(duration * 24), "Downloaded segment")
    return {"segment": number, "http_status": 200, "video_codec": "h264", "audio_codec": "aac",
            "width": 160, "height": 90, "video_start_pts": starts["video"], "audio_start_pts": starts["audio"], **decoded}


def decode_graph(scratch: Scratch, job: dict) -> list[dict]:
    api = job["api"]
    lines = [line if line.startswith("#") else smoke.ORIGIN + resolve(job["master"], line, api.token)
             for line in job["master_lines"]]
    master = scratch.write("authenticated-master.m3u8", ("\n".join(lines) + "\n").encode("utf-8"))
    results = []
    for seek, seconds, frames in ((0, 15, 360), (6, 9, 216)):
        encoded = scratch.run(decode_arguments(master, hls=True, seek=seek),
                              "Real HTTP HLS master graph decode", timeout=60)
        results.append({"seek_seconds": seek, "input": "private_master_to_real_http_main_and_segments",
                        **progress(encoded, seconds, frames, "Real HLS demuxer")})
    return results


def change_token(target: str, token: str | None) -> str:
    parsed = urlsplit(target)
    query = parse_qs(parsed.query, keep_blank_values=True)
    query.pop("api_key", None)
    if token is not None:
        query["api_key"] = [token]
    return urlunsplit(("", "", parsed.path, urlencode(query, doseq=True), ""))


def active_cleanup(job: dict, method: str) -> None:
    path = "/emby/Videos/ActiveEncodings" + ("/Delete" if method == "POST" else "")
    query = urlencode({"DeviceId": smoke.DEVICE_ID, "PlaySessionId": job["play_id"]})
    job["api"].request(method, path + "?" + query, emby=True, expected=(204,), parse=False,
                       label="Owned active HLS encoding cleanup")
    job["retired"] = True


def detail(api, item_id: str) -> dict:
    return api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items/{quote(item_id)}",
                       emby=True, label="Owned HLS item detail")


def main() -> int:
    os.umask(0o077)
    api, observer, owned, scratch = smoke.API(), smoke.API(), smoke.OwnedFixture(), Scratch()
    http = HTTP(api)
    jobs = []
    hashes = {}
    baseline = None
    item_id = library_id = active_job = ""
    summary = {"status": "failed", "assertions": [], "cleanup_errors": []}
    stage = "preconditions"
    try:
        check(sys.argv[1:] in ([], ["--whole-first"]), "Only the bounded whole-first diagnostic option is supported")
        whole_first = sys.argv[1:] == ["--whole-first"]
        if whole_first:
            summary["diagnostic_order"] = "whole_graph_before_random_access"
        check(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
              "Run as root through SSH only on the authorized Linux test-env host")
        hashes = source_hashes()
        check(FFMPEG.is_file() and FFPROBE.is_file() and os.access(FFMPEG, os.X_OK) and os.access(FFPROBE, os.X_OK),
              "Pinned FFmpeg and ffprobe executables are unavailable")
        check((smoke.DIRECTORY / smoke.STATE_NAME).is_file(), "The reusable direct-playback viewer has not been prepared")
        owned.open()
        check(bool(owned.state.get("user_id")), "The reusable owned viewer record has no account")
        scratch.create()
        api.request("GET", "/readyz", label="Deployed Goby readiness")
        api.admin_login(smoke.credentials())
        api.viewer_login(owned.state)
        observer.viewer_login(owned.state)
        check(api.session_id != observer.session_id and api.token != observer.token and api.user_id == observer.user_id,
              "Cross-owner verification requires separate authenticated sessions for the same authorized viewer")
        library_id = owned_library(api, owned)
        stage = "owned normal-frame-rate source scan"
        job = api.request("POST", f"/admin/v1/libraries/{quote(library_id)}/scan", admin=True,
                          expected=(202,), label="Owned HLS library scan")["Job"]
        active_job = job["Id"]
        job = api.wait_job(active_job)
        active_job = ""
        check(job.get("Status") == "completed" and job.get("Scanned") == 1 and not job.get("Error"),
              "HLS source scan did not inspect exactly one clean movie")
        query = urlencode({"ParentId": library_id, "Recursive": "true", "IncludeItemTypes": "Movie",
                           "Fields": "Path,MediaSources,MediaStreams", "Limit": 10})
        listed = api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items?" + query,
                             emby=True, label="Owned HLS source lookup")
        check(listed.get("TotalRecordCount") == 1 and len(listed.get("Items", [])) == 1 and
              listed["Items"][0].get("Path") == str(MEDIA), "HLS library does not contain the expected immutable source")
        item_id = listed["Items"][0]["Id"]
        item = detail(api, item_id)
        sources = item.get("MediaSources", [])
        check(len(sources) == 1 and sources[0].get("Size") == MEDIA_SIZE and
              abs(sources[0].get("RunTimeTicks", 0) / smoke.TICKS - 15) <= 0.01,
              "Indexed HLS source has an unexpected size or duration")
        source_id = sources[0]["Id"]
        streams = sources[0].get("MediaStreams", [])
        audio = [stream for stream in streams if stream.get("Type") == "Audio" and stream.get("Codec") == "aac"]
        check(len(audio) == 1 and any(stream.get("Type") == "Video" and stream.get("Codec") == "h264" and
              (stream.get("Width"), stream.get("Height")) == (320, 180) for stream in streams),
              "Indexed HLS source is missing the actual 320x180 H.264/AAC streams")
        audio_index = audio[0]["Index"]
        baseline = item["UserData"]
        check(baseline.get("PlaybackPositionTicks") == 0,
              "Owned fixture has an existing resume position; verification will not overwrite it")
        summary["assertions"].append({"stage": stage, "source_seconds": 15, "source_bytes": MEDIA_SIZE,
                                      "source_width": 320, "source_height": 180, "source_hash_matches": True})

        stage = "six-second hint and complete authenticated VOD graph"
        first = graph(api, http, item_id, source_id, audio_index, 6, jobs)
        summary["assertions"].append({"stage": stage, "vod_seconds": 15, "start_hint_seconds": 6,
                                      "source_global_segments": [0, 1, 2, 3, 4], "segment_seconds": 3,
                                      "discontinuity_before_segments": first["discontinuities"],
                                      "token_bearing_children": True, "output_width": 160, "output_height": 90})
        if whole_first:
            stage = "fresh graph FFmpeg HLS demuxer full graph and seek"
            summary["assertions"].append({"stage": stage, "software_av_decode": True,
                                          "decodes": decode_graph(scratch, first)})
        stage = "middle neighbor backward and tail segment decoding"
        facts = []
        for number in (2, 3, 0, 4):
            metadata, content = http.request("GET", first["children"][number], "Source-global HLS segment")
            check(metadata.get("content-type") == "video/mp2t" and metadata.get("content-length") == str(len(content)) and
                  bool(content), "HLS segment bytes, MIME, or declared length do not match")
            facts.append(probe_segment(scratch, content, number, first["durations"][number]))
        by_number = {fact["segment"]: fact for fact in facts}
        for kind in ("video_start_pts", "audio_start_pts"):
            check(abs(by_number[3][kind] - by_number[2][kind] - 3) <= 0.1 and
                  abs(by_number[2][kind] - by_number[0][kind] - 6) <= 0.1 and
                  abs(by_number[4][kind] - by_number[0][kind] - 12) <= 0.1,
                  "Independent random-access segment runs do not share a global A/V timeline")
        summary["assertions"].append({"stage": stage, "output_pts_base_seconds": 1,
                                      "request_order": [2, 3, 0, 4], "segments": facts})
        if not whole_first:
            stage = "real FFmpeg HLS demuxer full graph and seek"
            summary["assertions"].append({"stage": stage, "software_av_decode": True,
                                          "decodes": decode_graph(scratch, first)})

        stage = "cached segment HEAD conditional authentication and ownership"
        target = first["children"][2]
        cached, body = http.request("GET", target, "Cached HLS segment")
        head, head_body = http.request("HEAD", target, "Cached HLS segment HEAD")
        check(not head_body and head.get("content-type") == "video/mp2t" and head.get("content-length") == str(len(body)) and
              bool(head.get("etag")) and head.get("etag") == cached.get("etag"),
              "Cached segment HEAD did not preserve current GET metadata")
        conditional = {"If-None-Match": head["etag"]}
        metadata, content = http.request("GET", target, "Authenticated HLS conditional GET", expected=(304,), headers=conditional)
        check(not content and metadata.get("etag") == head["etag"], "HLS conditional GET did not retain an empty 304 and matching ETag")
        for token in (None, "invalid-goby-hls-token"):
            http.request("GET", change_token(target, token), "Cached HLS authentication rejection", expected=(401,), headers=conditional)
        http.request("GET", change_token(target, observer.token), "Foreign HLS authentication-session owner",
                     expected=(404,), headers=conditional)
        check(detail(api, item_id)["UserData"] == baseline, "HLS negotiation or fetching changed user playback data")
        summary["assertions"].append({"stage": stage, "head": 200, "conditional_get": 304,
                                      "missing_and_invalid_token": 401, "different_session_same_user": 404,
                                      "user_data_dto_unchanged": True})

        stage = "DELETE and POST active encoding cleanup"
        active_cleanup(first, "DELETE")
        http.request("GET", first["children"][2], "HLS child after DELETE cleanup", expected=(404,))
        check(detail(api, item_id)["UserData"] == baseline, "DELETE encoding cleanup changed user data")
        second = graph(api, http, item_id, source_id, audio_index, 0, jobs)
        http.request("GET", second["children"][0], "Second HLS job initial segment")
        active_cleanup(second, "POST")
        http.request("GET", second["children"][0], "HLS child after POST cleanup", expected=(404,))
        check(detail(api, item_id)["UserData"] == baseline, "POST encoding cleanup changed user data")
        summary["assertions"].append({"stage": stage, "delete": 204, "post_delete": 204,
                                      "retired_children": 404, "zero_hint_preserves_full_vod": True,
                                      "user_data_dto_unchanged": True})

        stage = "Stopped and Logout HLS cleanup"
        stopped = graph(observer, http, item_id, source_id, audio_index, 0, jobs)
        http.request("GET", stopped["children"][0], "Prepared HLS stop-control segment")
        observer.request("POST", "/emby/Sessions/Playing/Stopped", emby=True, expected=(204,), parse=False,
                         label="Owned prepared playback stop", body={"PlaySessionId": stopped["play_id"],
                         "ItemId": item_id, "MediaSourceId": source_id, "SessionId": observer.session_id, "PositionTicks": 0})
        stopped["retired"] = True
        http.request("GET", stopped["children"][0], "HLS child after playback stop", expected=(404,))
        check(detail(api, item_id)["UserData"] == baseline, "Prepared zero-position stop changed visible user data")
        logged_out = graph(observer, http, item_id, source_id, audio_index, 0, jobs)
        http.request("GET", logged_out["children"][0], "Prepared HLS logout-control segment")
        observer.request("POST", "/emby/Sessions/Logout", emby=True, parse=False, label="Owned HLS viewer logout")
        observer.token = ""
        logged_out["retired"] = True
        http.request("GET", logged_out["children"][0], "HLS child after owner logout", expected=(401,))
        check(detail(api, item_id)["UserData"] == baseline, "HLS owner logout changed user data")
        summary["assertions"].append({"stage": stage, "stopped": 204, "stopped_child": 404,
                                      "logout": 200, "logged_out_child": 401, "user_data_dto_unchanged": True})
        api.request("GET", "/readyz", label="Goby readiness after HLS verification")
        summary["http_response_bytes"] = http.bytes
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
    finally:
        if active_job and api.cookie:
            try:
                api.request("POST", f"/admin/v1/jobs/{quote(active_job)}/cancel", admin=True, expected=(202,),
                            label="Owned HLS scan cancellation")
                api.wait_job(active_job, timeout=10)
            except Exception:
                summary["cleanup_errors"].append("Owned HLS scan cancellation failed")
        for job in jobs:
            if job["api"].token and not job["retired"]:
                try:
                    active_cleanup(job, "DELETE")
                except Exception:
                    summary["cleanup_errors"].append("Owned HLS encoding cancellation failed")
        if api.token and item_id and baseline is not None:
            try:
                check(detail(api, item_id)["UserData"] == baseline, "Visible owned user data changed")
                summary["owned_user_data_dto_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned user-data preservation check failed")
        for viewer in (observer, api):
            if viewer.token:
                try:
                    viewer.request("POST", "/emby/Sessions/Logout", emby=True, parse=False, label="HLS verification viewer logout")
                    viewer.token = ""
                except Exception:
                    summary["cleanup_errors"].append("Owned HLS viewer session revocation failed")
        if api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False,
                            label="HLS verification administrator logout")
            except Exception:
                summary["cleanup_errors"].append("HLS administrator session revocation failed")
        if hashes:
            try:
                check(all(smoke.digest(path) == value for path, value in hashes.items()), "HLS reference input bytes changed")
                summary["source_media_and_marker_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("HLS source preservation check failed")
        if library_id:
            try:
                smoke.private_file(RECORD)
                summary["private_library_record_mode"] = "0600"
                summary["owned_library_retained"] = True
            except Exception:
                summary["cleanup_errors"].append("Private HLS ownership record check failed")
        try:
            scratch.cleanup()
            summary["owned_private_scratch_removed"] = True
        except Exception:
            summary["cleanup_errors"].append("Owned private HLS scratch cleanup failed")
        if owned.lock is not None:
            owned.lock.close()
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        if scratch.diagnostics:
            summary["private_process_diagnostics"] = scratch.diagnostics
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
