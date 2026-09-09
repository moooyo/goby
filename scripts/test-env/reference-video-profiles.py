#!/usr/bin/env python3
"""Capture bounded progressive Video PlaybackInfo contracts from Emby 4.9.5.

Run only through SSH in the existing official reference network namespace.
The existing normal-frame-rate source and all preceding evidence are immutable.
Private raw records, exact wire bodies, credentials, and probe inputs use tmpfs.
Only sanitized video-profile-m4e exports are suitable for the repository.
"""

from __future__ import annotations

import argparse
import copy
import importlib.util
import json
import os
from pathlib import Path
import resource
import stat
import sys
import tempfile
import time
import traceback
from urllib.parse import parse_qs, urlencode, urlsplit, urlunsplit


sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("reference_audio", Path(__file__).with_name("reference-audio.py"))
AUDIO = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(AUDIO)
BASE = AUDIO.BASE
RUNTIME = AUDIO.ROOT.parent
ROOT = RUNTIME / "video-profile-m4e"
PRIVATE = ROOT / "private"
RAW, WIRE, EXPORT, PROBES = PRIVATE / "raw", PRIVATE / "wire", ROOT / "export", PRIVATE / "probe"
BASELINE, CONTEXT = PRIVATE / "baseline.json", PRIVATE / "context.json"
MARKER, PREFIX = "goby-video-profile-m4e-owned-v1", "video-profile-m4e-"
DEVICE = "goby-video-profile-m4e-recorder"
SOURCE = Path("/opt/goby-fixtures/hls-reference/Reference HLS Normal (2026).mp4")
SOURCE_HASH = "332bce27f1e97d71d3ef7cffa59d8cf70cbcc51380a6b600e05048107432de4d"
MAX_BODY, MAX_TOTAL, MAX_PROCESS = 3 * 1024 * 1024, 12 * 1024 * 1024, 3 * 1024 * 1024
URL_FIELDS = {"videocodec", "audiocodec", "videobitrate", "audiobitrate", "videostreamindex", "audiostreamindex",
              "subtitlestreamindex", "width", "height", "maxwidth", "maxheight", "maxframerate", "framerate",
              "audiochannels", "audiosamplerate", "transcodingmaxaudiochannels", "starttimeticks", "static",
              "container", "segmentcontainer", "segmentlength", "minsegments", "transcodereasons",
              "allowvideostreamcopy", "allowaudiostreamcopy", "copytimestamps", "breakonnonkeyframes"}

for key, value in {"ROOT": ROOT, "PRIVATE": PRIVATE, "RAW": RAW, "WIRE": WIRE, "EXPORT": EXPORT,
                   "PROBES": PROBES, "BASELINE": BASELINE, "CONTEXT": CONTEXT, "PREFIX": PREFIX,
                   "MARKER": MARKER, "DEVICE": DEVICE, "SOURCES": SOURCE.parent,
                   "MAX_BODY": MAX_BODY, "MAX_TOTAL": MAX_TOTAL}.items():
    setattr(AUDIO, key, value)


def check(condition: bool, label: str) -> None:
    AUDIO.check(condition, label)


def output_profile(protocol="http", *, context="Streaming", scaled=True) -> dict:
    result = {"Type": "Video", "Container": "mp4" if protocol != "hls" else "ts",
              "VideoCodec": "h264", "AudioCodec": "aac", "MaxAudioChannels": "2"}
    if protocol is not None:
        result["Protocol"] = protocol
    if context is not None:
        result["Context"] = context
    if scaled:
        result.update({"MaxWidth": 160, "MaxHeight": 90})
    if protocol == "hls":
        result.update({"SegmentLength": 3, "MinSegments": 1})
    return result


def technical_url(value: str) -> dict:
    parsed = urlsplit(value)
    query = parse_qs(parsed.query, keep_blank_values=True)
    return {"endpointLeaf": parsed.path.rsplit("/", 1)[-1], "parameterNames": sorted(query),
            "hasApiKey": bool(query.get("api_key")),
            "mediaParameters": {key: value for key, value in query.items() if key.lower() in URL_FIELDS}}


def mp4_edits(content: bytes) -> dict:
    """Observe bounded ISO BMFF timing boxes without changing the media."""
    def children(start, end):
        offset = start
        while offset + 8 <= end:
            size = int.from_bytes(content[offset:offset + 4], "big")
            kind = content[offset + 4:offset + 8].decode("ascii", errors="replace")
            header = 8
            if size == 1:
                check(offset + 16 <= end, "A video box has an incomplete extended size")
                size = int.from_bytes(content[offset + 8:offset + 16], "big")
                header = 16
            elif size == 0:
                size = end - offset
            check(size >= header and offset + size <= end, "A video box exceeds its captured response")
            yield kind, offset + header, offset + size
            offset += size

    result = {"isISOBaseMedia": False, "tracks": []}
    if len(content) < 12 or content[4:8] != b"ftyp":
        return result
    top = []
    try:
        for box in children(0, len(content)):
            top.append(box)
    except RuntimeError as error:
        # Retain complete initialization boxes preceding a truncated mdat, and
        # explicitly preserve that the whole response is structurally invalid.
        result["parseError"] = str(error)
        result["completeTopLevelBoxesBeforeError"] = len(top)
    result.update({"isISOBaseMedia": True, "fragmentCount": sum(kind == "moof" for kind, _, _ in top)})
    for kind, start, end in top:
        if kind != "moov":
            continue
        for kind, start, end in children(start, end):
            if kind == "mvhd":
                offset = start + (20 if content[start] == 1 else 12)
                result["movieTimescale"] = int.from_bytes(content[offset:offset + 4], "big")
            if kind != "trak":
                continue
            track = {"editListPresent": False}
            for kind, start, end in children(start, end):
                if kind == "tkhd":
                    offset = start + (20 if content[start] == 1 else 12)
                    track["trackId"] = int.from_bytes(content[offset:offset + 4], "big")
                if kind == "mdia":
                    for kind, start, end in children(start, end):
                        if kind == "mdhd":
                            offset = start + (20 if content[start] == 1 else 12)
                            track["mediaTimescale"] = int.from_bytes(content[offset:offset + 4], "big")
                if kind == "edts":
                    for kind, start, end in children(start, end):
                        if kind != "elst":
                            continue
                        version = content[start]
                        count = int.from_bytes(content[start + 4:start + 8], "big")
                        check(version in (0, 1) and count <= 16, "The captured edit list exceeds its bound")
                        width = 8 if version == 1 else 4
                        offset = start + 8
                        entries = []
                        for _ in range(count):
                            check(offset + width * 2 + 4 <= end, "The captured edit-list entry is incomplete")
                            entries.append({"segmentDuration": int.from_bytes(content[offset:offset + width], "big"),
                                            "mediaTime": int.from_bytes(content[offset + width:offset + width * 2], "big", signed=True),
                                            "mediaRateInteger": int.from_bytes(content[offset + width * 2:offset + width * 2 + 2], "big", signed=True),
                                            "mediaRateFraction": int.from_bytes(content[offset + width * 2 + 2:offset + width * 2 + 4], "big", signed=True)})
                            offset += width * 2 + 4
                        track.update({"editListPresent": True, "editListVersion": version, "editList": entries})
            result["tracks"].append(track)
    return result


class Recorder(AUDIO.Recorder):
    def clean_text(self, value: str) -> str:
        return super().clean_text(value).replace("/reference-audio-private", "/reference-video-profile-private").replace(
            "/reference-audio-source", "/reference-video-source")

    def audit_export(self, original, exported, key="") -> None:
        if isinstance(original, str) and str(SOURCE.parent) in original:
            check(exported == self.clean_text(original), "Source path sanitization changed unrelated text")
            return
        super().audit_export(original, exported, key)

    def setup(self) -> None:
        AUDIO.preconditions(preparing=True)
        info = SOURCE.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and
              SOURCE.resolve(strict=True) == SOURCE and info.st_size == 967651 and AUDIO.digest(SOURCE) == SOURCE_HASH and
              (SOURCE.parent / ".goby-managed").read_text().strip() == "goby-hls-normal-owned-v1",
              "The immutable normal-frame-rate video does not match its ownership record")
        folders = [BASE.PRIVATE / "raw", BASE.EXPORT]
        for name, marker in (("audio-m4c", "goby-audio-m4c-owned-v1"),
                             ("audio-m4c-defaults", "goby-audio-m4c-defaults-owned-v1"),
                             ("audio-profile-m4d", "goby-audio-profile-m4d-owned-v1")):
            directory = RUNTIME / name
            check(directory.resolve(strict=True) == directory and (directory / ".goby-managed").read_text().strip() == marker,
                  "A preceding reference capture ownership marker does not match")
            folders.extend((directory / "private/raw", directory / "export"))
        baseline = {str(path): AUDIO.digest(path) for folder in folders for path in folder.glob("*.json")}
        check(len(baseline) == 1472, "Expected the preserved 736 preceding raw/export reference pairs")
        original = json.loads((BASE.PRIVATE / "raw/hls-m4a-normal-item.json").read_text())["response"]["body"]["Items"]
        check(len(original) == 1 and original[0].get("Path") == str(SOURCE), "The recorded reference item does not name the owned video")
        item = original[0]
        check(len(item.get("MediaSources", [])) == 1, "The normal video requires one recorded media source")
        context = {"item_id": item["Id"], "source_id": item["MediaSources"][0]["Id"]}
        ROOT.mkdir(mode=0o700)
        for directory in (PRIVATE, RAW, WIRE, EXPORT, PROBES):
            directory.mkdir(mode=0o700)
        AUDIO.private_write(ROOT / ".goby-managed", MARKER + "\n", exclusive=True)
        goby_pid = int(AUDIO.subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        AUDIO.private_write(BASELINE, json.dumps({"records": baseline, "sourceSha256": SOURCE_HASH, "gobyPID": goby_pid}, indent=2), exclusive=True)
        AUDIO.private_write(CONTEXT, json.dumps(context, indent=2), exclusive=True)
        prior = BASE.read_credentials(BASE.PRIVATE / "hls-m4a-credentials.env")
        BASE.save_credentials({key: prior[key] for key in ("REFERENCE_USERNAME", "REFERENCE_PASSWORD")}, AUDIO.credentials("user"))
        self.use("user")
        login = self.request("user-login", "POST", "/emby/Users/AuthenticateByName",
                             body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        check(login["status"] == 200 and self.credentials["REFERENCE_USER_ID"] == prior["REFERENCE_USER_ID"],
              "Reference video login did not resolve the existing owned ordinary user")
        public = self.request("system-info", "GET", "/emby/System/Info/Public", headers={})
        check(public["status"] == 200 and public["parsed"].get("Version") == "4.9.5.0", "Unexpected official reference version")
        self.write("runtime-before", {"kind": "remote-runtime-observation", "sourceSha256": SOURCE_HASH, **self.runtime()})
        source = self.video_probe("source", path=SOURCE)
        check(source.get("videoFrames") == 360 and source.get("videoCodec") == "h264" and source.get("audioCodec") == "aac",
              "The immutable video source probe differs from the known 15-second reference")
        AUDIO.private_write(PRIVATE / "source-observation.json", json.dumps(source, indent=2), exclusive=True)

    def process(self, arguments: list[str], timeout=40) -> dict:
        def limits() -> None:
            resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_PROCESS, MAX_PROCESS))

        with tempfile.TemporaryFile(dir=PROBES) as out, tempfile.TemporaryFile(dir=PROBES) as err:
            result = AUDIO.subprocess.run(arguments, stdin=AUDIO.subprocess.DEVNULL, stdout=out, stderr=err,
                                          timeout=timeout, preexec_fn=limits)
            out.seek(0)
            err.seek(0)
            stdout, stderr = out.read(MAX_PROCESS + 1), err.read(MAX_PROCESS + 1)
        check(max(len(stdout), len(stderr)) <= MAX_PROCESS, "Video media-process output exceeded its private bound")
        return {"exitCode": result.returncode, "stdout": stdout.decode(errors="replace"), "stderr": stderr.decode(errors="replace")}

    def video_probe(self, name: str, *, path=None, wire=b"", manifest=None) -> dict:
        if path is None:
            path = PROBES / (PREFIX + name + (".m3u8" if manifest is not None else ".media"))
            payload = manifest.encode() if manifest is not None else wire
            check(0 < len(payload) <= MAX_BODY, "Video probe input exceeds its body limit")
            descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(descriptor, "wb") as handle:
                handle.write(payload)
        network = ["-protocol_whitelist", "file,http,tcp,pipe", "-rw_timeout", "10000000",
                   "-allowed_extensions", "ALL", "-allowed_segment_extensions", "ALL", "-extension_picky", "0"] if manifest is not None else []
        probe = self.process([AUDIO.FFPROBE, "-v", "error", *network, "-show_streams", "-show_format", "-show_packets",
                              "-show_entries", "packet=stream_index,pts_time,dts_time,duration_time,flags,data_hash",
                              "-show_data_hash", "sha256", "-of", "json", str(path)])
        frames = self.process([AUDIO.FFPROBE, "-v", "error", *network, "-show_frames", "-show_entries",
                               "frame=media_type,stream_index,best_effort_timestamp_time,pts_time,duration_time,nb_samples",
                               "-of", "json", str(path)])
        decoded = self.process([AUDIO.FFMPEG, "-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-threads", "1", *network,
                                "-i", str(path), "-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-progress", "pipe:1",
                                "-nostats", "-f", "null", "-"], timeout=45)
        frame_hashes = self.process([AUDIO.FFMPEG, "-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-threads", "1", *network,
                                     "-i", str(path), "-map", "0:v:0", "-an", "-threads", "1", "-fps_mode", "passthrough",
                                     "-f", "framemd5", "-"], timeout=45)
        facts = json.loads(probe["stdout"]) if probe["stdout"] else {}
        frame_data = json.loads(frames["stdout"]) if frames["stdout"] else {}
        probe["body"], frames["body"] = facts, frame_data
        del probe["stdout"], frames["stdout"]
        progress = dict(line.partition("=")[::2] for line in decoded["stdout"].splitlines() if "=" in line)
        decoded["progress"] = progress
        del decoded["stdout"]
        try:
            edits = mp4_edits(path.read_bytes()) if manifest is None else {"isISOBaseMedia": False, "tracks": []}
        except RuntimeError as error:
            # A malformed vendor body is evidence, not permission to silently
            # truncate its boxes or stop the remaining independent controls.
            edits = {"isISOBaseMedia": path.read_bytes()[4:8] == b"ftyp", "parseError": str(error), "tracks": []}
        self.write(name + "-probe", {"kind": "remote-video-probe", "ffprobe": probe, "decodedFrames": frames,
                                    "ffmpegDecode": decoded, "ffmpegVideoFrameHashes": frame_hashes, "mp4TimingBoxes": edits,
                                    "scope": "Complete returned bytes or real emitted HLS graph, with strict xerror."})
        video = next((value for value in facts.get("streams", []) if value.get("codec_type") == "video"), {})
        audio = next((value for value in facts.get("streams", []) if value.get("codec_type") == "audio"), {})
        spans, hashes, counts = {}, {}, {}
        for label, stream in (("video", video), ("audio", audio)):
            packets = [entry for entry in facts.get("packets", []) if entry.get("stream_index") == stream.get("index")]
            hashes[label] = AUDIO.hashlib.sha256(json.dumps([entry.get("data_hash") for entry in packets], separators=(",", ":")).encode()).hexdigest()
            entries = [entry for entry in frame_data.get("frames", []) if entry.get("media_type") == label]
            counts[label] = len(entries)
            intervals = []
            for entry in entries:
                raw = entry.get("best_effort_timestamp_time", entry.get("pts_time"))
                if raw is None:
                    continue
                start = float(raw)
                duration = float(entry.get("duration_time", "0"))
                if label == "audio" and stream.get("sample_rate"):
                    duration = int(entry.get("nb_samples", 0)) / int(stream["sample_rate"])
                intervals.append((start, start + duration))
            if intervals:
                spans[label] = {"firstPTS": min(start for start, _ in intervals), "endPTS": max(end for _, end in intervals)}
        observation = {"probeExit": probe["exitCode"], "frameProbeExit": frames["exitCode"], "decodeExit": decoded["exitCode"],
                       "decodeStderrEmpty": not decoded["stderr"], "videoCodec": video.get("codec_name"), "audioCodec": audio.get("codec_name"),
                       "width": video.get("width"), "height": video.get("height"), "frameRate": video.get("avg_frame_rate"),
                       "audioSampleRate": audio.get("sample_rate"), "audioChannels": audio.get("channels"),
                       "formatName": facts.get("format", {}).get("format_name"), "formatDuration": facts.get("format", {}).get("duration"),
                       "videoFrames": counts.get("video", 0), "audioFrames": counts.get("audio", 0),
                       "audioSamples": sum(int(entry.get("nb_samples", 0)) for entry in frame_data.get("frames", []) if entry.get("media_type") == "audio"),
                       "decodeProgressFrames": int(progress.get("frame", "0")), "decodeProgressSeconds": int(progress.get("out_time_us", "0")) / 1000000,
                       "decodedPresentation": spans, "packetPayloadDigests": hashes}
        observation["frameHashExit"] = frame_hashes["exitCode"]
        observation["mp4TimingBoxes"] = edits
        source_record = RAW / (PREFIX + "source-probe.json")
        if name != "source" and source_record.exists():
            original = json.loads(source_record.read_text())
            original_hashes = [line.rsplit(",", 1)[-1].strip() for line in original["ffmpegVideoFrameHashes"]["stdout"].splitlines()
                               if line.strip() and not line.startswith("#")]
            observed_hashes = [line.rsplit(",", 1)[-1].strip() for line in frame_hashes["stdout"].splitlines()
                               if line.strip() and not line.startswith("#")]
            matches = [index for index, digest in enumerate(original_hashes) if observed_hashes and digest == observed_hashes[0]]
            observation["firstDecodedVideoSourceFrameMatches"] = matches
            observation["firstDecodedVideoSourceSeconds"] = matches[0] / 24 if len(matches) == 1 else None
            locations = {}
            for label, stream in (("video", video), ("audio", audio)):
                source_stream = next((value for value in original["ffprobe"]["body"].get("streams", []) if value.get("codec_type") == label), {})
                packets = [entry for entry in facts.get("packets", []) if entry.get("stream_index") == stream.get("index")]
                source_packets = [entry for entry in original["ffprobe"]["body"].get("packets", []) if entry.get("stream_index") == source_stream.get("index")]
                first = packets[0].get("data_hash") if packets else None
                locations[label] = [entry.get("pts_time") for entry in source_packets if first is not None and entry.get("data_hash") == first]
            observation["firstPacketSourcePTSMatches"] = locations
        if set(spans) == {"video", "audio"}:
            observation["audioMinusVideoStartSeconds"] = round(spans["audio"]["firstPTS"] - spans["video"]["firstPTS"], 6)
            observation["audioMinusVideoEndSeconds"] = round(spans["audio"]["endPTS"] - spans["video"]["endPTS"], 6)
        return observation

    def follow_video(self, name: str, response: dict, ids: set[str]) -> dict:
        redirects = []
        for hop in range(3):
            ids.update(parse_qs(urlsplit(response["path"]).query).get("PlaySessionId", []))
            if response["status"] not in {301, 302, 303, 307, 308}:
                break
            redirects.append(response["status"])
            response = self.request(name + "-redirect-" + str(hop + 1), "GET",
                                    self.local(response["path"], response["headers"].get("location", "")), headers={})
        observation = {"finalStatus": response["status"], "redirectStatuses": redirects, "wireBytes": len(response["wire"]),
                       "contentType": response["headers"].get("content-type"), "sourceBytesMatch": response["wire"] == SOURCE.read_bytes()}
        if response["status"] != 200:
            return observation
        if response["wire"].startswith(b"#EXTM3U"):
            master = response
            children = [line.strip() for line in response["wire"].decode().splitlines() if line.strip() and not line.startswith("#")]
            if b"#EXT-X-STREAM-INF" in response["wire"]:
                check(len(children) == 1, "The bounded video profile control requires one HLS variant")
                response = self.request(name + "-main", "GET", self.local(response["path"], children[0]), headers={})
                observation["mainStatus"] = response["status"]
                if response["status"] != 200:
                    return observation
                children = [line.strip() for line in response["wire"].decode().splitlines() if line.strip() and not line.startswith("#")]
            check(0 < len(children) <= 6, "The video HLS graph exceeded the six-segment bound")
            statuses = []
            for index, child in enumerate(children):
                target = self.local(response["path"], child)
                ids.update(parse_qs(urlsplit(target).query).get("PlaySessionId", []))
                segment = self.request(name + "-segment-" + str(index), "GET", target, headers={})
                statuses.append(segment["status"])
            observation["segmentStatuses"] = statuses
            if all(status == 200 for status in statuses):
                manifest = "\n".join(line if line.startswith("#") or not line.strip() else AUDIO.ORIGIN + self.local(master["path"], line.strip())
                                     for line in master["wire"].decode().splitlines()) + "\n"
                observation["media"] = self.video_probe(name + "-hls", manifest=manifest)
        elif response["headers"].get("content-type", "").startswith(("video/", "audio/", "application/octet-stream")):
            observation["media"] = self.video_probe(name + "-media", wire=response["wire"])
            source = json.loads((PRIVATE / "source-observation.json").read_text())
            observation["packetPayloadMatchesSource"] = {label: observation["media"]["packetPayloadDigests"][label] == source["packetPayloadDigests"][label]
                                                         for label in ("video", "audio")}
        return observation

    def case(self, name: str, *, protocol="http", context="Streaming", outputs=None, direct=False,
             copy_video=False, copy_audio=False, scaled=True, start=None, range_request=False) -> None:
        if (RAW / (PREFIX + name + "-observation.json")).exists():
            # Completed controls are immutable. Resume stages may skip them,
            # but incomplete HTTP records still refuse automatic repetition.
            return
        self.use("user")
        identifiers = json.loads(CONTEXT.read_text())
        output = output_profile(protocol, context=context, scaled=scaled)
        body = {"UserId": self.credentials["REFERENCE_USER_ID"], "MediaSourceId": identifiers["source_id"],
                "IsPlayback": True, "EnableDirectPlay": direct, "EnableDirectStream": direct, "EnableTranscoding": True,
                "AllowVideoStreamCopy": copy_video, "AllowAudioStreamCopy": copy_audio, "StartTimeTicks": 0,
                "VideoStreamIndex": 0, "AudioStreamIndex": 1, "SubtitleStreamIndex": -1,
                "MaxStreamingBitrate": 2000000 if direct or copy_video else 500000,
                "DeviceProfile": {"Name": "Goby video progressive reference", "MaxStreamingBitrate": 2000000 if direct or copy_video else 500000,
                                  "DirectPlayProfiles": [{"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"}] if direct else [],
                                  "TranscodingProfiles": outputs if outputs is not None else [output]}}
        response = self.request(name + "-info", "POST", f"/emby/Items/{identifiers['item_id']}/PlaybackInfo", body=body, authenticated=True)
        result = response["parsed"] if isinstance(response["parsed"], dict) else {}
        source = result.get("MediaSources", [{}])[0] if result.get("MediaSources") else {}
        keys = ("Container", "SupportsDirectPlay", "SupportsDirectStream", "SupportsTranscoding", "TranscodingContainer", "TranscodingSubProtocol",
                "DefaultAudioStreamIndex", "DefaultVideoStreamIndex", "DefaultSubtitleStreamIndex", "RunTimeTicks", "AddApiKeyToDirectStreamUrl")
        observation = {"kind": "video-profile-observation", "case": name, "playbackInfoStatus": response["status"],
                       "sourceFields": {key: source[key] for key in keys if key in source}, "omittedSourceFields": [key for key in keys if key not in source],
                       "hasTranscodingUrl": bool(source.get("TranscodingUrl")), "hasDirectStreamUrl": bool(source.get("DirectStreamUrl")),
                       "hasPlaySessionId": bool(result.get("PlaySessionId")), "errorCode": result.get("ErrorCode")}
        ids = {result["PlaySessionId"]} if result.get("PlaySessionId") else set()
        try:
            preferred = ("DirectStreamUrl", "TranscodingUrl") if direct else ("TranscodingUrl", "DirectStreamUrl")
            field = next((key for key in preferred if source.get(key)), None)
            if response["status"] == 200 and field:
                target = self.local("/", source[field])
                observation["returnedUrl"] = technical_url(target)
                if start is not None:
                    parsed = urlsplit(target)
                    query = parse_qs(parsed.query, keep_blank_values=True)
                    query["StartTimeTicks"] = [str(start)]
                    target = urlunsplit(("", "", parsed.path, urlencode(query, doseq=True), ""))
                    observation["soleModifiedParameter"] = {"StartTimeTicks": start}
                observation["selectedUrlField"] = field
                ids.update(parse_qs(urlsplit(target).query).get("PlaySessionId", []))
                AUDIO.private_write(PRIVATE / (name + "-context.json"), json.dumps({"target": target, "playSessionIds": sorted(ids)}, indent=2), exclusive=True)
                head = self.request(name + "-head", "HEAD", target, headers={}, note="Follows the emitted same-origin URL; no extra authentication header.")
                fetched = self.request(name + "-get", "GET", target, headers={})
                observation.update({"headStatus": head["status"], "initialGetStatus": fetched["status"]})
                if fetched["status"] == 500:
                    time.sleep(0.2)
                    fetched = self.request(name + "-retry", "GET", target, headers={}, note="One identical-URL retry after 200 ms; the initial failure is retained.")
                    observation["retryStatus"] = fetched["status"]
                observation["delivery"] = self.follow_video(name, fetched, ids)
                if range_request:
                    ranged = self.request(name + "-range", "GET", target, headers={"Range": "bytes=0-1023"})
                    observation["range"] = {"status": ranged["status"], "wireBytes": len(ranged["wire"]),
                                             "matchesFullResponse": ranged["wire"] == fetched["wire"],
                                             "matchesResponsePrefix": ranged["wire"] == fetched["wire"][:1024]}
                    if ranged["status"] == 200 and ranged["wire"] != fetched["wire"]:
                        observation["range"]["delivery"] = self.follow_video(name + "-range", ranged, ids)
            self.write(name + "-observation", observation)
            print(json.dumps({"case": name, "sourceFields": observation["sourceFields"],
                              "returnedUrlFacts": observation.get("returnedUrl"),
                              "delivery": observation.get("delivery"), "range": observation.get("range")}, sort_keys=True), flush=True)
        finally:
            self.cleanup(name, ids)

    def core(self) -> None:
        AUDIO.preconditions()
        self.case("http-mp4", range_request=True)
        self.case("protocol-omitted", protocol=None)
        self.case("protocol-empty", protocol="")
        self.case("context-empty", context="")

    def remaining(self) -> None:
        AUDIO.preconditions()
        self.case("original-compatible", direct=True, copy_video=True, copy_audio=True, scaled=False)
        self.case("copy-copy", copy_video=True, copy_audio=True, scaled=False)
        self.case("copy-start-nonkeyframe", copy_video=True, copy_audio=True, scaled=False, start=63700000)
        self.case("http-start-nonkeyframe", start=63700000)
        self.case("video-copy-audio-encode", copy_video=True, scaled=False)
        self.case("http-first", outputs=[output_profile("http"), output_profile("hls")])
        self.case("hls-first", outputs=[output_profile("hls"), output_profile("http")])
        self.write("runtime-after-capture", {"kind": "remote-runtime-observation", **self.runtime()})

    def recover_copy(self) -> None:
        """Finish postprocessing already captured bytes; no network requests."""
        AUDIO.preconditions()
        self.use("user")
        name = "copy-copy"
        info = json.loads((RAW / (PREFIX + name + "-info.json")).read_text())
        response = json.loads((RAW / (PREFIX + name + "-retry.json")).read_text())
        initial = json.loads((RAW / (PREFIX + name + "-get.json")).read_text())
        head = json.loads((RAW / (PREFIX + name + "-head.json")).read_text())
        path = PROBES / (PREFIX + name + "-media.media")
        wire = AUDIO.base64.b64decode(response["response"]["body"], validate=True)
        check(path.read_bytes() == wire, "The partial copy probe input differs from retained wire evidence")
        source = info["response"]["body"]["MediaSources"][0]
        fields = ("Container", "SupportsDirectPlay", "SupportsDirectStream", "SupportsTranscoding", "TranscodingContainer",
                  "TranscodingSubProtocol", "DefaultAudioStreamIndex", "DefaultVideoStreamIndex", "DefaultSubtitleStreamIndex", "RunTimeTicks", "AddApiKeyToDirectStreamUrl")
        observation = {"kind": "video-profile-observation", "case": name, "playbackInfoStatus": info["response"]["status"],
                       "sourceFields": {key: source[key] for key in fields if key in source}, "omittedSourceFields": [key for key in fields if key not in source],
                       "hasTranscodingUrl": bool(source.get("TranscodingUrl")), "hasDirectStreamUrl": bool(source.get("DirectStreamUrl")),
                       "hasPlaySessionId": bool(info["response"]["body"].get("PlaySessionId")), "errorCode": info["response"]["body"].get("ErrorCode"),
                       "returnedUrl": technical_url(source["TranscodingUrl"]), "selectedUrlField": "TranscodingUrl",
                       "headStatus": head["response"]["status"], "initialGetStatus": initial["response"]["status"], "retryStatus": response["response"]["status"],
                       "captureRecovery": "The first strict MP4 box observation rejected truncated bytes after HTTP capture and scoped cleanup. Only retained bytes were reprocessed; no HTTP request was repeated.",
                       "delivery": {"finalStatus": response["response"]["status"], "wireBytes": len(wire),
                                    "contentType": next(value for key, value in response["response"]["headers"] if key.lower() == "content-type"),
                                    "redirectStatuses": [], "sourceBytesMatch": wire == SOURCE.read_bytes(),
                                    "media": self.video_probe(name + "-media", path=path)}}
        self.write(name + "-observation", observation)
        print(json.dumps({"case": name, "delivery": observation["delivery"]}, sort_keys=True), flush=True)

    def seek_mixed(self) -> None:
        AUDIO.preconditions()
        self.case("video-copy-audio-encode-start", copy_video=True, scaled=False, start=63700000)
        self.write("runtime-after-nonkeyframe-control", {"kind": "remote-runtime-observation", **self.runtime()})

    def initialization(self) -> None:
        """Inspect existing media bytes and response identity without requests."""
        AUDIO.preconditions()
        self.use("user")
        observations = {"source": mp4_edits(SOURCE.read_bytes())}
        play_ids = []
        for path in sorted(RAW.glob(PREFIX + "*.json")):
            raw = json.loads(path.read_text())
            if "response" not in raw:
                continue
            response = raw["response"]
            if urlsplit(raw["request"]["path"]).path.endswith("/PlaybackInfo") and isinstance(response.get("body"), dict):
                play_ids.append(response["body"].get("PlaySessionId"))
            if response.get("bodyType") != "binary-base64":
                continue
            wire = AUDIO.base64.b64decode(response["body"], validate=True)
            if wire[4:8] == b"ftyp":
                observations[path.stem.removeprefix(PREFIX)] = mp4_edits(wire)
        self.write("mp4-initialization-summary-final", {"kind": "derived-mp4-initialization-observation", "cases": observations,
                   "playbackInfoResponses": len(play_ids), "allPlaySessionIdsPresent": all(play_ids),
                   "allPlaySessionIdsUnique": len(play_ids) == len(set(play_ids)),
                   "note": "Complete boxes preceding an invalid/truncated media-data box remain observable. parseError is retained; incomplete outputs are not classified as valid MP4. This final summary selects PlaybackInfo by request route; the earlier derived summary's filename selector also counted System/Info/Public and is superseded for identity counts."})
        print(json.dumps({"playbackInfoResponses": len(play_ids), "allPlaySessionIdsUnique": len(play_ids) == len(set(play_ids)),
                          "mp4TimingObservations": len(observations)}, sort_keys=True), flush=True)

    def audit(self) -> None:
        AUDIO.preconditions()
        baseline = json.loads(BASELINE.read_text())
        check(all(AUDIO.digest(Path(path)) == expected for path, expected in baseline["records"].items()), "A preceding reference record changed")
        check(AUDIO.digest(SOURCE) == baseline["sourceSha256"] == SOURCE_HASH, "The immutable normal video source changed")
        total, count = 0, 0
        records = sorted(RAW.glob(PREFIX + "*.json"))
        for path in records:
            raw = json.loads(path.read_text())
            exported = json.loads((EXPORT / path.name).read_text())
            self.audit_export(raw, exported)
            check(exported == self.sanitize(raw) and not any(secret in (EXPORT / path.name).read_text() for secret in self.secret_values),
                  "Video export differs from deterministic sanitization or contains a credential")
            if "request" not in raw:
                continue
            wire = AUDIO.base64.b64decode((WIRE / (path.stem + ".b64")).read_text(), validate=True)
            response = raw["response"]
            headers = {key.lower(): value for key, value in response["headers"]}
            if raw["request"]["path"].startswith("/emby/Videos/ActiveEncodings?") or raw["request"]["path"] == "/emby/Sessions/Logout":
                check(response["status"] == 204 and not wire, "An owned cleanup or logout did not complete with an empty 204")
            check(exported["response"]["headers"] == self.sanitize(response["headers"]), "Video response headers changed")
            if raw["request"]["method"] != "HEAD" and "content-length" in headers:
                check(int(headers["content-length"]) == len(wire), "Video Content-Length differs from exact wire bytes")
            if response["bodyType"] == "json":
                check(json.loads(wire) == response["body"], "Video JSON wire reconstruction differs")
            elif response["bodyType"] == "text":
                check(response["body"].encode() == wire, "Video text wire reconstruction differs")
            else:
                check(AUDIO.base64.b64decode(response["body"], validate=True) == wire and
                      not any(secret.encode() in wire for secret in self.secret_values), "Video binary wire differs or contains credentials")
            check(AUDIO.hashlib.sha256(wire).hexdigest() == raw["observation"]["wireSha256"], "Video wire hash differs")
            total += len(wire)
            count += 1
        check(total <= MAX_TOTAL, "The video reference wire budget was exceeded")
        goby_pid = int(AUDIO.subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        summary = {"status": "passed", "records": len(records), "httpRecords": count, "wireBytes": total,
                   "preservedOldRecordFiles": len(baseline["records"]), "sourceSha256Unchanged": SOURCE_HASH,
                   "gobyPIDBefore": baseline["gobyPID"], "gobyPIDAfter": goby_pid, "gobyPIDUnchanged": goby_pid == baseline["gobyPID"]}
        AUDIO.private_write(PRIVATE / "audit-summary.json", json.dumps(summary, indent=2))
        print(json.dumps(summary, sort_keys=True), flush=True)

    def finish(self) -> None:
        AUDIO.preconditions()
        self.use("user")
        self.request("final-device-cleanup", "DELETE", "/emby/Videos/ActiveEncodings?" + urlencode({"DeviceId": DEVICE}), authenticated=True,
                     note="Targets only the independent video-profile recorder device.")
        time.sleep(0.5)
        runtime = self.runtime()
        self.write("runtime-final", {"kind": "remote-runtime-observation", **runtime})
        check(not runtime["directChildProcesses"] and runtime["transcodingFileCount"] == 0 and runtime["transcodingBytes"] == 0,
              "Reference child processes or transcoding cache entries remain after owned cleanup")
        self.request("user-logout", "POST", "/emby/Sessions/Logout", authenticated=True)
        self.audit()
        check(PROBES.resolve(strict=True).parent == PRIVATE and (ROOT / ".goby-managed").read_text().strip() == MARKER,
              "Video probe cleanup ownership does not match")
        for path in PROBES.iterdir():
            info = path.lstat()
            check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and
                  stat.S_IMODE(info.st_mode) == 0o600 and path.name.startswith(PREFIX), "Unexpected video probe input")
        for path in PROBES.iterdir():
            path.unlink()
        PROBES.rmdir()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("stage", choices=("setup", "core", "remaining", "recover_copy", "seek_mixed", "initialization", "audit", "finish"))
    stage = parser.parse_args().stage
    try:
        getattr(Recorder(), stage)()
    except Exception as error:
        if PRIVATE.is_dir() and (ROOT / ".goby-managed").read_text().strip() == MARKER:
            error_path = PRIVATE / ("failure-" + stage + ".txt")
            if not error_path.exists():
                AUDIO.private_write(error_path, traceback.format_exc(), exclusive=True)
        print(json.dumps({"status": "failed", "stage": stage, "errorType": type(error).__name__}), flush=True)
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
