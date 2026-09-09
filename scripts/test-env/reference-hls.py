#!/usr/bin/env python3
"""Capture bounded HLS contracts from the isolated official Emby reference.

Run only on test-env in the reference service network namespace. Credentials,
wire originals, and probe inputs stay private. Existing fixtures are immutable.
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import subprocess
import time
import urllib.parse


SOURCE = Path(__file__).with_name("reference-capture.py")
if not SOURCE.exists():
    SOURCE = Path("/dev/shm/goby-emby-reference/reference-capture.py")
SPEC = importlib.util.spec_from_file_location("reference_capture", SOURCE)
BASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BASE)
PRIVATE = BASE.PRIVATE
EXPORT = BASE.EXPORT
PREFIX = "hls-m4a-"
DEVICE = "goby-hls-m4a-recorder"
CREDS = PRIVATE / "hls-m4a-credentials.env"
NORMAL_ADMIN_CREDS = PRIVATE / "hls-m4a-normal-admin-credentials.env"
NORMAL_BASELINE = PRIVATE / "hls-m4a-normal-baseline-hashes.json"
NORMAL_MEDIA = Path("/opt/goby-fixtures/hls-reference/Reference HLS Normal (2026).mp4")
BASELINE = PRIVATE / "hls-m4a-baseline-hashes.json"
SCRATCH = Path("/dev/shm/goby-emby-reference/runtime/hls-m4a")
MARKER = "goby-hls-m4a-owned-v1"
MAX_BODY = 1024 * 1024
MAX_JOB = 8 * 1024 * 1024
MAX_TOTAL = 30 * 1024 * 1024
MEDIA = Path("/opt/goby-fixtures/playback-reference/Reference Playback M3.mp4")
MEDIA_HASH = "997af268405a91e01685afa52d70a892c767e0e7135d25f6a33087cfb72de1c3"


def timestamp():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def update_query(path, **values):
    parts = urllib.parse.urlsplit(path)
    query = dict(urllib.parse.parse_qsl(parts.query, keep_blank_values=True))
    query.update({key: str(value) for key, value in values.items()})
    return parts.path + "?" + urllib.parse.urlencode(query)


def resolve(parent, child):
    target = urllib.parse.urlsplit(urllib.parse.urljoin("http://127.0.0.1:18097" + parent, child))
    if target.scheme != "http" or target.hostname != "127.0.0.1" or target.port != 18097:
        raise RuntimeError("Refusing a playlist URL outside the isolated reference")
    return target.path + ("?" + target.query if target.query else "")


def playlist_children(body):
    if not isinstance(body, str) or not body.startswith("#EXTM3U"):
        return []
    return [line.strip() for line in body.splitlines() if line.strip() and not line.startswith("#")]


class Recorder(BASE.Recorder):
    def __init__(self):
        super().__init__()
        for path in PRIVATE.glob("*credentials.env"):
            self.secret_values.update(value for key, value in BASE.read_credentials(path).items()
                                      if key in {"REFERENCE_TOKEN", "REFERENCE_PASSWORD"} and value)
        self.job_bytes = 0
        self.total_bytes = 0
        self.context = json.loads((PRIVATE / "playback-m3-context.json").read_text())

    def clean_text(self, value):
        return super().clean_text(value).replace(str(SCRATCH), "/reference-hls-private")

    def audit_export(self, original, exported, key=""):
        if isinstance(original, str) and str(SCRATCH) in original:
            if exported != self.clean_text(original):
                raise RuntimeError("Private HLS path sanitization changed other text")
            return
        super().audit_export(original, exported, key)

    def unused(self, name):
        if not name.startswith(PREFIX):
            raise RuntimeError("Only owned HLS fixture names are permitted")
        if any((directory / (name + ".json")).exists() for directory in (PRIVATE / "raw", EXPORT)):
            raise RuntimeError("Refusing to overwrite existing HLS evidence")

    def own(self):
        self.credentials = BASE.read_credentials(CREDS)
        self.credential_path = CREDS
        self.secret_values.update(value for key, value in self.credentials.items()
                                  if key in {"REFERENCE_TOKEN", "REFERENCE_PASSWORD"} and value)
        self.client_headers = {
            "Accept": "application/json",
            "Authorization": ('Emby Client="Goby HLS Recorder", Device="Linux HLS Test", '
                              f'DeviceId="{DEVICE}", Version="0.1.0"'),
        }

    def write_record(self, name, record):
        self.unused(name)
        cleaned = self.sanitize(record)
        self.audit_export(record, cleaned)
        serialized = json.dumps(cleaned, indent=2, ensure_ascii=False) + "\n"
        if any(secret in serialized for secret in self.secret_values):
            raise RuntimeError("A credential survived export")
        BASE.private_write(PRIVATE / "raw" / (name + ".json"), json.dumps(record, indent=2) + "\n")
        BASE.private_write(EXPORT / (name + ".json"), serialized)

    def request(self, name, method, path, *, body=None, headers=None, authenticated=False, note=None, binary=False):
        self.unused(name)
        request_headers = dict(self.client_headers if headers is None else headers)
        if authenticated:
            request_headers["X-Emby-Token"] = self.credentials["REFERENCE_TOKEN"]
        wire_body = None
        if body is not None:
            request_headers["Content-Type"] = "application/json"
            wire_body = json.dumps(body, separators=(",", ":")).encode()
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=12)
        started = timestamp()
        try:
            connection.request(method, path, body=wire_body, headers=request_headers)
            response = connection.getresponse()
            response_headers = response.getheaders()
            content = response.read(MAX_BODY + 1)
            if len(content) > MAX_BODY:
                raise RuntimeError("HLS response exceeded the one-MiB per-request limit")
            status = response.status
        finally:
            connection.close()
        self.job_bytes += len(content)
        self.total_bytes += len(content)
        if self.job_bytes > MAX_JOB or self.total_bytes > MAX_TOTAL:
            raise RuntimeError("HLS wire output exceeded the configured budget")
        BASE.private_write(SCRATCH / "wire" / (name + ".b64"), base64.b64encode(content).decode())
        content_type = dict((key.lower(), value) for key, value in response_headers).get("content-type", "")
        if binary and content and not content_type.startswith(("text/", "application/json")):
            parsed, body_type = base64.b64encode(content).decode(), "binary-base64"
        else:
            text = content.decode("utf-8", errors="strict")
            try:
                parsed, body_type = json.loads(text), "json"
            except json.JSONDecodeError:
                parsed, body_type = text, "text"
        if isinstance(parsed, dict) and parsed.get("AccessToken"):
            self.credentials["REFERENCE_TOKEN"] = parsed["AccessToken"]
            self.secret_values.add(parsed["AccessToken"])
            self.credentials["REFERENCE_USER_ID"] = parsed["User"]["Id"]
            BASE.save_credentials(self.credentials, self.credential_path)
        record = {"reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": started},
                  "request": {"method": method, "path": path, "headers": request_headers, "body": body},
                  "response": {"status": status, "headers": response_headers, "bodyType": body_type, "body": parsed},
                  "observation": {"wireBytes": len(content), "wireSha256": hashlib.sha256(content).hexdigest()}}
        if note:
            record["observation"]["note"] = note
        self.write_record(name, record)
        print(f"{name}: HTTP {status}, {body_type}, {len(content)} bytes", flush=True)
        return status, parsed, content

    def setup(self):
        if CREDS.exists() or BASELINE.exists() or SCRATCH.exists():
            raise RuntimeError("HLS setup already exists")
        baseline = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                    for directory in (PRIVATE / "raw", EXPORT)
                    for path in sorted(directory.glob("*.json"))}
        if len(baseline) != 684:
            raise RuntimeError("Expected exactly 342 preceding raw/export pairs")
        if hashlib.sha256(MEDIA.read_bytes()).hexdigest() != MEDIA_HASH:
            raise RuntimeError("Owned media bytes differ from the known synthetic source")
        BASE.private_write(BASELINE, json.dumps(baseline, indent=2) + "\n")
        SCRATCH.mkdir(mode=0o700)
        BASE.private_write(SCRATCH / ".goby-managed", MARKER + "\n")
        credentials = {"REFERENCE_USERNAME": "reference-hls-m4a", "REFERENCE_PASSWORD": secrets.token_hex(32)}
        BASE.save_credentials(credentials, CREDS)
        self.secret_values.add(credentials["REFERENCE_PASSWORD"])
        status, result, _ = self.request(PREFIX + "user-create", "POST", "/emby/Users/New",
                                         body={"Name": credentials["REFERENCE_USERNAME"]}, authenticated=True)
        if status != 200 or not result.get("Id") or result.get("Policy", {}).get("IsAdministrator"):
            raise RuntimeError("Dedicated ordinary HLS user creation failed")
        credentials["REFERENCE_USER_ID"] = result["Id"]
        BASE.save_credentials(credentials, CREDS)
        self.request(PREFIX + "user-password", "POST", f"/emby/Users/{result['Id']}/Password",
                     body={"Id": result["Id"], "NewPw": credentials["REFERENCE_PASSWORD"], "ResetPassword": False}, authenticated=True)
        self.own()
        status, result, _ = self.request(PREFIX + "user-login", "POST", "/emby/Users/AuthenticateByName",
                                         body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        if status != 200:
            raise RuntimeError("Dedicated HLS login failed")
        self.credentials["REFERENCE_SESSION_ID"] = result["SessionInfo"]["Id"]
        BASE.save_credentials(self.credentials, CREDS)

    def negotiate(self):
        self.own()
        for name in ("remux", "audio", "video"):
            transcode = {"Container": "ts", "Type": "Video", "VideoCodec": "h264",
                         "AudioCodec": "mp3" if name == "audio" else "aac", "Protocol": "hls", "Context": "Streaming",
                         "MaxAudioChannels": "2", "MinSegments": 1, "SegmentLength": 3}
            if name == "video":
                transcode["MaxWidth"] = 80
            profile = {"Name": "HLS M4a " + name, "MaxStreamingBitrate": 200000,
                       "DirectPlayProfiles": [], "TranscodingProfiles": [transcode],
                       "SubtitleProfiles": [{"Format": "srt", "Method": "External"}, {"Format": "vtt", "Method": "External"}]}
            body = {"UserId": self.credentials["REFERENCE_USER_ID"], "MediaSourceId": self.context["MediaSourceId"],
                    "DeviceProfile": profile, "IsPlayback": True, "EnableDirectPlay": False,
                    "EnableDirectStream": False, "EnableTranscoding": True, "AllowVideoStreamCopy": name != "video",
                    "AllowAudioStreamCopy": name != "audio", "StartTimeTicks": 5700000000,
                    "AudioStreamIndex": 1, "SubtitleStreamIndex": -1}
            status, result, _ = self.request(PREFIX + name + "-playbackinfo", "POST", f"/emby/Items/{self.context['ItemId']}/PlaybackInfo",
                                             body=body, authenticated=True, note="Requests initial playback position 570 seconds in the owned 600-second source. Actual playlist and segment seek semantics are observed separately; no playback reports or watch-state changes are sent.")
            if status != 200 or not result.get("MediaSources"):
                continue
            source = result["MediaSources"][0]
            url = source.get("TranscodingUrl")
            if not url or not url.startswith("/videos/") or "/master.m3u8?" not in url:
                print(f"{name}: no supported local HLS URL was offered", flush=True)
                continue
            url = "/emby" + url
            url = update_query(url, StartTimeTicks=5700000000)
            BASE.private_write(SCRATCH / (name + ".json"), json.dumps({"url": url, "playSessionId": result["PlaySessionId"]}, indent=2) + "\n")

    def cleanup(self, name, session, method="DELETE"):
        path = "/emby/Videos/ActiveEncodings" + ("/Delete" if method == "POST" else "")
        query = urllib.parse.urlencode({"DeviceId": DEVICE, "PlaySessionId": session})
        return self.request(PREFIX + name, method, path + "?" + query, authenticated=True)

    def probes(self, name, wire):
        directory = SCRATCH / "probe"
        directory.mkdir(mode=0o700, exist_ok=True)
        target = directory / (name + ".ts")
        target.write_bytes(wire)
        target.chmod(0o600)
        ffprobe = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe"
        ffmpeg = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg"
        if not Path(ffprobe).is_file() or not Path(ffmpeg).is_file():
            raise RuntimeError("Existing remote FFmpeg toolchain is unavailable")
        command = [ffprobe, "-v", "error", "-show_streams", "-show_format", "-show_packets", "-read_intervals", "%+#8", "-of", "json", str(target)]
        probe = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
        decode = subprocess.run([ffmpeg, "-hide_banner", "-loglevel", "error", "-threads", "1", "-i", str(target), "-map", "0:v?", "-map", "0:a?", "-threads", "1", "-f", "null", "-"],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
        result = {"reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": timestamp()},
                  "kind": "remote-media-probe", "sourceFixture": name,
                  "input": {"bytes": len(wire), "sha256": hashlib.sha256(wire).hexdigest()},
                  "ffprobe": {"exitCode": probe.returncode, "body": json.loads(probe.stdout) if probe.stdout else None, "stderr": probe.stderr.decode()},
                  "ffmpegDecode": {"exitCode": decode.returncode, "stderr": decode.stderr.decode(), "scope": "The complete downloaded segment only; no server URL or full source is decoded."}}
        self.write_record(name + "-probe", result)
        print(f"{name}-probe: ffprobe={probe.returncode}, decode={decode.returncode}", flush=True)

    def playlists(self, name, url, segments=True):
        status, master, _ = self.request(PREFIX + name + "-master", "GET", url, headers={}, note="Follows the local negotiated master URL with the recorded StartTimeTicks control; the dimension-control case also changes the recorded size parameters. Query serialization may normalize escaping. The query token is the only credential.")
        children = playlist_children(master)
        if status != 200 or not children:
            return None
        media_path = resolve(url, children[0])
        status, main, _ = self.request(PREFIX + name + "-main", "GET", media_path, headers={}, note="Relative child resolved exactly; no query token is added if absent from the emitted URI.")
        children = playlist_children(main)
        if status != 200 or not children:
            return None
        first_path = None
        offset = int(dict(urllib.parse.parse_qsl(urllib.parse.urlsplit(url).query)).get("StartTimeTicks", "0")) / 10000000
        durations = [float(line.split(":", 1)[1].split(",", 1)[0]) for line in main.splitlines() if line.startswith("#EXTINF:")]
        position = 0.0
        start_index = 0
        for index, duration in enumerate(durations):
            if position + duration > offset:
                start_index = index
                break
            position += duration
        for index, child in enumerate(children[start_index:start_index + 2] if segments else []):
            segment_path = resolve(media_path, child)
            if first_path is None:
                first_path = segment_path
            status, _, content = self.request(PREFIX + name + f"-segment-{index}", "GET", segment_path, headers={}, binary=True,
                                               note=f"Selects manifest entry {start_index + index} at the requested playback offset. Uses only credentials emitted in the relative URI; no token header, cookie, or inherited parent query is added.")
            if status == 200 and content:
                self.probes(PREFIX + name + f"-segment-{index}", content)
        return first_path

    def run_job(self, name, context_name=None):
        self.own()
        self.job_bytes = 0
        context = json.loads((SCRATCH / ((context_name or name) + ".json")).read_text())
        first_path = None
        try:
            first_path = self.playlists(name, context["url"])
        finally:
            self.cleanup(name + "-cleanup", context["playSessionId"], "POST" if name == "audio" else "DELETE")
        if first_path:
            self.request(PREFIX + name + "-segment-after-cleanup", "GET", first_path, headers={}, binary=True,
                         note="Bounded read of the same emitted segment URL after ActiveEncodings cleanup; no new master or media playlist request is sent.")
            self.cleanup(name + "-cleanup-final", context["playSessionId"])

    def remux(self):
        self.run_job("remux")

    def remux_tail(self):
        self.run_job("remux-tail", "remux")

    def audio(self):
        self.run_job("audio")

    def video(self):
        self.run_job("video")

    def video_upscale(self):
        self.own()
        self.job_bytes = 0
        context = json.loads((SCRATCH / "video.json").read_text())
        url = update_query(context["url"], Width=1920, Height=1080, MaxWidth=1920, MaxHeight=1080)
        try:
            self.playlists("video-upscale", url)
        finally:
            self.cleanup("video-upscale-cleanup", context["playSessionId"])

    def normal_prepare(self):
        target = NORMAL_MEDIA.parent
        if NORMAL_BASELINE.exists() or NORMAL_ADMIN_CREDS.exists() or target.exists():
            raise RuntimeError("Normal-frame-rate HLS control already exists")
        baseline = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                    for directory in (PRIVATE / "raw", EXPORT)
                    for path in sorted(directory.glob("*.json"))}
        BASE.private_write(NORMAL_BASELINE, json.dumps(baseline, indent=2) + "\n")
        target.mkdir(mode=0o755)
        target.chmod(0o755)
        marker = target / ".goby-managed"
        marker.write_text("goby-hls-normal-owned-v1\n")
        marker.chmod(0o644)
        ffmpeg = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg"
        subprocess.run([ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24",
                        "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "15",
                        "-c:v", "libx264", "-preset", "ultrafast", "-crf", "28", "-g", "72", "-keyint_min", "72",
                        "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p", "-threads", "1",
                        "-c:a", "aac", "-b:a", "64k", "-ar", "48000", "-ac", "1", "-movflags", "+faststart", str(NORMAL_MEDIA)],
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=True)
        NORMAL_MEDIA.chmod(0o644)
        if NORMAL_MEDIA.stat().st_size >= 3 * 1024 * 1024:
            raise RuntimeError("Normal HLS source exceeds three MiB")
        provenance = {"path": str(NORMAL_MEDIA), "bytes": NORMAL_MEDIA.stat().st_size,
                      "sha256": hashlib.sha256(NORMAL_MEDIA.read_bytes()).hexdigest(),
                      "durationSeconds": 15, "frameRate": 24, "width": 320, "height": 180,
                      "keyframeIntervalSeconds": 3, "audioSampleRate": 48000}
        BASE.private_write(SCRATCH / "normal-provenance.json", json.dumps(provenance, indent=2) + "\n")
        BASE.save_credentials({"REFERENCE_USERNAME": "reference-hls-m4a-admin", "REFERENCE_PASSWORD": secrets.token_hex(32)}, NORMAL_ADMIN_CREDS)
        print(f"Prepared {provenance['bytes']} bytes of owned normal-frame-rate media; protected {len(baseline)} previous raw/export files.", flush=True)

    def normal_admin(self):
        self.credentials = BASE.read_credentials(NORMAL_ADMIN_CREDS)
        self.credential_path = NORMAL_ADMIN_CREDS
        self.secret_values.update(value for key, value in self.credentials.items()
                                  if key in {"REFERENCE_TOKEN", "REFERENCE_PASSWORD"} and value)
        self.client_headers = {"Accept": "application/json", "Authorization": ('Emby Client="Goby HLS Recorder", Device="Linux HLS Admin", '
                              'DeviceId="goby-hls-m4a-admin-recorder", Version="0.1.0"')}

    def normal_setup(self):
        credentials = BASE.read_credentials(NORMAL_ADMIN_CREDS)
        if credentials.get("REFERENCE_TOKEN"):
            raise RuntimeError("Dedicated HLS administrator is already initialized")
        status, created, _ = self.request(PREFIX + "normal-admin-create", "POST", "/emby/Users/New",
                                          body={"Name": credentials["REFERENCE_USERNAME"]}, authenticated=True)
        if status != 200 or not created.get("Id"):
            raise RuntimeError("Dedicated HLS administrator creation failed")
        credentials["REFERENCE_USER_ID"] = created["Id"]
        BASE.save_credentials(credentials, NORMAL_ADMIN_CREDS)
        self.request(PREFIX + "normal-admin-password", "POST", f"/emby/Users/{created['Id']}/Password",
                     body={"Id": created["Id"], "NewPw": credentials["REFERENCE_PASSWORD"], "ResetPassword": False}, authenticated=True)
        policy = dict(created["Policy"])
        policy["IsAdministrator"] = True
        self.request(PREFIX + "normal-admin-policy", "POST", f"/emby/Users/{created['Id']}/Policy", body=policy, authenticated=True)
        self.normal_admin()
        status, result, _ = self.request(PREFIX + "normal-admin-login", "POST", "/emby/Users/AuthenticateByName",
                                         body={"Username": credentials["REFERENCE_USERNAME"], "Pw": credentials["REFERENCE_PASSWORD"]})
        if status != 200 or not result["User"]["Policy"]["IsAdministrator"]:
            raise RuntimeError("Dedicated administrator login did not establish its policy")
        old = json.loads((PRIVATE / "raw" / "playback-m3-library-query.json").read_text())["response"]["body"]["Items"]
        library = next(item for item in old if item["Name"] == "Reference Playback M3")
        options = json.loads(json.dumps(library["LibraryOptions"]))
        options["PathInfos"] = [{"Path": str(NORMAL_MEDIA.parent)}]
        options["SampleIgnoreSize"] = 0
        self.request(PREFIX + "normal-library-create", "POST", "/emby/Library/VirtualFolders", authenticated=True,
                     body={"Name": "Reference HLS Normal M4a", "CollectionType": "movies", "RefreshLibrary": False,
                           "Paths": [str(NORMAL_MEDIA.parent)], "LibraryOptions": options})
        _, result, _ = self.request(PREFIX + "normal-library-query", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        library = next(item for item in result["Items"] if item["Name"] == "Reference HLS Normal M4a")
        BASE.private_write(SCRATCH / "normal-library.json", json.dumps(library, indent=2) + "\n")
        self.request(PREFIX + "normal-library-refresh", "POST", f"/emby/Items/{library['ItemId']}/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=FullRefresh",
                     body={}, authenticated=True, note="Only the new owned normal-frame-rate library is refreshed by its dedicated administrator.")

    def normal_negotiate(self):
        self.own()
        time.sleep(2)
        query = urllib.parse.urlencode({"UserId": self.credentials["REFERENCE_USER_ID"], "Recursive": "true", "Path": str(NORMAL_MEDIA), "Fields": "Path,MediaSources,MediaStreams"})
        status, result, _ = self.request(PREFIX + "normal-item", "GET", "/emby/Items?" + query, authenticated=True)
        if status != 200 or len(result.get("Items", [])) != 1:
            raise RuntimeError("The owned normal HLS source is not indexed")
        item = result["Items"][0]
        for name in ("remux", "video"):
            transcode = {"Container": "ts", "Type": "Video", "VideoCodec": "h264", "AudioCodec": "aac", "Protocol": "hls", "Context": "Streaming",
                         "MaxAudioChannels": "2", "MinSegments": 1, "SegmentLength": 3}
            if name == "video":
                transcode["MaxWidth"] = 160
            profile = {"Name": "HLS normal M4a " + name, "MaxStreamingBitrate": 2000000, "DirectPlayProfiles": [], "TranscodingProfiles": [transcode]}
            body = {"UserId": self.credentials["REFERENCE_USER_ID"], "DeviceProfile": profile, "IsPlayback": True,
                    "EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": True,
                    "AllowVideoStreamCopy": name != "video", "AllowAudioStreamCopy": True, "StartTimeTicks": 0,
                    "AudioStreamIndex": 1, "SubtitleStreamIndex": -1}
            status, result, _ = self.request(PREFIX + "normal-" + name + "-playbackinfo", "POST", f"/emby/Items/{item['Id']}/PlaybackInfo", body=body, authenticated=True)
            if status != 200 or not result.get("MediaSources") or not result["MediaSources"][0].get("TranscodingUrl"):
                raise RuntimeError("No HLS URL was offered for the normal source")
            BASE.private_write(SCRATCH / ("normal-" + name + ".json"), json.dumps({"url": "/emby" + result["MediaSources"][0]["TranscodingUrl"], "playSessionId": result["PlaySessionId"]}, indent=2) + "\n")

    def normal(self):
        self.own()
        self.job_bytes = 0
        for mode in ("remux", "video"):
            context = json.loads((SCRATCH / ("normal-" + mode + ".json")).read_text())
            for label, ticks in (("zero", 0), ("six", 60000000)):
                name = "normal-" + mode + "-" + label
                try:
                    self.playlists(name, update_query(context["url"], StartTimeTicks=ticks))
                    self.normal_cache_observation(name)
                finally:
                    self.cleanup(name + "-cleanup", context["playSessionId"], "POST" if label == "six" else "DELETE")

    def normal_cache_observation(self, name):
        candidates = set()
        for index in range(2):
            record_file = PRIVATE / "raw" / (PREFIX + name + f"-segment-{index}.json")
            if record_file.exists():
                response = json.loads(record_file.read_text())["response"]
                if response["bodyType"] == "text":
                    candidates.update(re.findall(r"/transcoding-temp/([A-Za-z0-9_-]+)/", response["body"]))
        observations = []
        for folder in sorted(candidates):
            target = BASE.DATA / "transcoding-temp" / folder
            files = [{"name": path.name, "bytes": path.stat().st_size} for path in target.iterdir() if path.is_file()] if target.is_dir() else []
            observations.append({"publicLogDirectory": "/transcoding-temp/" + folder, "directoryExists": target.is_dir(), "files": files})
        self.write_record(PREFIX + name + "-cache-before-cleanup", {
            "reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": timestamp()},
            "kind": "remote-runtime-observation", "scope": "Only output directories named in this job's public HTTP error bodies, observed before its explicit cleanup API request.",
            "outputDirectories": observations,
        })

    def normal_audit(self):
        baseline = json.loads(NORMAL_BASELINE.read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("Pre-existing evidence changed during the normal source control")
        provenance = json.loads((SCRATCH / "normal-provenance.json").read_text())
        if hashlib.sha256(NORMAL_MEDIA.read_bytes()).hexdigest() != provenance["sha256"]:
            raise RuntimeError("Normal reference source changed after generation")
        self.audit()
        total = sum(json.loads(path.read_text()).get("observation", {}).get("wireBytes", 0) for path in (PRIVATE / "raw").glob(PREFIX + "normal-*.json"))
        if total > MAX_JOB:
            raise RuntimeError("Normal source control exceeded eight MiB of wire output")
        print(f"Normal source control preserved {len(baseline)} prior raw/export files and downloaded {total} HTTP bytes.", flush=True)

    def normal_finish(self):
        self.own()
        for mode in ("remux", "video"):
            context = json.loads((SCRATCH / ("normal-" + mode + ".json")).read_text())
            self.cleanup("normal-final-" + mode + "-cleanup", context["playSessionId"])
        pid = int(subprocess.check_output(["systemctl", "show", "goby-emby-reference", "-p", "MainPID", "--value"]).strip())
        cache = BASE.DATA / "transcoding-temp"
        files = [path for path in cache.rglob("*") if path.is_file()] if cache.exists() else []
        self.write_record(PREFIX + "normal-runtime-after-cleanup", {
            "reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": timestamp()},
            "kind": "remote-runtime-observation", "referencePID": pid,
            "directChildPIDs": [int(child) for child in Path(f"/proc/{pid}/task/{pid}/children").read_text().split()],
            "namespaceMatchesReference": os.readlink("/proc/self/ns/net") == os.readlink(f"/proc/{pid}/ns/net"),
            "transcodingFileCount": len(files), "transcodingBytes": sum(path.stat().st_size for path in files),
            "rootFreeBytes": shutil.disk_usage("/").free, "tmpfsFreeBytes": shutil.disk_usage("/dev/shm").free,
        })
        self.normal_audit()
        if SCRATCH.resolve() != Path("/dev/shm/goby-emby-reference/runtime/hls-m4a") or (SCRATCH / ".goby-managed").read_text().strip() != MARKER:
            raise RuntimeError("Refusing to clean an unowned scratch directory")
        probe = SCRATCH / "probe"
        if probe.exists():
            shutil.rmtree(probe)
        print("Removed owned normal-source probe inputs; exact private wire captures remain.", flush=True)

    def seek(self):
        self.own()
        self.job_bytes = 0
        context = json.loads((SCRATCH / "video.json").read_text())
        for name, ticks in (("seek-forward", 5800000000), ("seek-backward", 5600000000)):
            try:
                self.playlists(name, update_query(context["url"], StartTimeTicks=ticks))
            finally:
                self.cleanup(name + "-cleanup", context["playSessionId"])

    def audit(self):
        baseline = json.loads(BASELINE.read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("A pre-existing fixture changed")
        if hashlib.sha256(MEDIA.read_bytes()).hexdigest() != MEDIA_HASH:
            raise RuntimeError("The owned source media changed")
        total = 0
        records = sorted((PRIVATE / "raw").glob(PREFIX + "*.json"))
        for path in records:
            raw = json.loads(path.read_text())
            exported = json.loads((EXPORT / path.name).read_text())
            self.audit_export(raw, exported)
            if exported != self.sanitize(raw):
                raise RuntimeError("Export differs from the deterministic sensitive-field sanitizer")
            if any(secret in (EXPORT / path.name).read_text() for secret in self.secret_values):
                raise RuntimeError("A credential remains in exported evidence")
            if raw.get("kind") in {"remote-media-probe", "remote-runtime-observation"}:
                continue
            wire = base64.b64decode((SCRATCH / "wire" / (path.stem + ".b64")).read_text(), validate=True)
            response = raw["response"]
            headers = {key.lower(): value for key, value in response["headers"]}
            if response["headers"] != exported["response"]["headers"]:
                raise RuntimeError("A response header changed")
            if raw["request"]["method"] != "HEAD" and "content-length" in headers and int(headers["content-length"]) != len(wire):
                raise RuntimeError("Content-Length differs from wire bytes")
            if response["bodyType"] == "text" and response["body"].encode() != wire:
                raise RuntimeError("Text wire bytes changed")
            if response["bodyType"] == "binary-base64":
                if base64.b64decode(response["body"], validate=True) != wire:
                    raise RuntimeError("Binary wire bytes changed")
                if any(secret.encode() in wire for secret in self.secret_values):
                    raise RuntimeError("A binary body contains a credential")
            if response["bodyType"] == "json" and json.loads(wire) != response["body"]:
                raise RuntimeError("JSON structure changed")
            total += len(wire)
        if total > MAX_TOTAL:
            raise RuntimeError("Total wire evidence exceeds its budget")
        print(f"Audited {len(records)} HLS records and {total} wire bytes; preserved all {len(baseline)} pre-existing raw/export files and the source media hash.", flush=True)

    def finish(self):
        self.own()
        for name in ("remux", "audio", "video"):
            path = SCRATCH / (name + ".json")
            if path.exists():
                context = json.loads(path.read_text())
                self.cleanup("final-" + name + "-cleanup", context["playSessionId"])
        pid = int(subprocess.check_output(["systemctl", "show", "goby-emby-reference", "-p", "MainPID", "--value"]).strip())
        children = []
        for child in Path(f"/proc/{pid}/task/{pid}/children").read_text().split():
            try:
                children.append({"pid": int(child), "name": Path(f"/proc/{child}/comm").read_text().strip()})
            except FileNotFoundError:
                pass
        cache = BASE.DATA / "transcoding-temp"
        files = list(cache.rglob("*")) if cache.exists() else []
        files = [path for path in files if path.is_file()]
        self.write_record(PREFIX + "runtime-after-cleanup", {
            "reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": timestamp()},
            "kind": "remote-runtime-observation", "referencePID": pid, "directChildProcesses": children,
            "namespaceMatchesReference": os.readlink("/proc/self/ns/net") == os.readlink(f"/proc/{pid}/ns/net"),
            "transcodingFileCount": len(files), "transcodingBytes": sum(path.stat().st_size for path in files),
            "scratchBytes": sum(path.stat().st_size for path in SCRATCH.rglob("*") if path.is_file()),
            "rootFreeBytes": shutil.disk_usage("/").free, "tmpfsFreeBytes": shutil.disk_usage("/dev/shm").free,
        })
        self.audit()
        if SCRATCH.resolve() != Path("/dev/shm/goby-emby-reference/runtime/hls-m4a") or (SCRATCH / ".goby-managed").read_text().strip() != MARKER:
            raise RuntimeError("Refusing to clean an unowned scratch directory")
        probe = SCRATCH / "probe"
        if probe.exists():
            shutil.rmtree(probe)
        print("Removed only owned temporary probe inputs; private wire evidence remains for audit.", flush=True)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("stage", choices=["setup", "negotiate", "remux", "remux_tail", "audio", "video", "video_upscale", "seek", "audit", "finish",
                                         "normal_prepare", "normal_setup", "normal_negotiate", "normal", "normal_audit", "normal_finish"])
    args = parser.parse_args()
    recorder = Recorder()
    getattr(recorder, args.stage)()


if __name__ == "__main__":
    main()
