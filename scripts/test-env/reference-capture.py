#!/usr/bin/env python3
"""Record sanitized HTTP contracts from the isolated official reference server.

Run only on test-env, inside the reference service's network namespace.
Private raw captures and credentials remain in a root-only directory.
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import stat
import struct
import subprocess
import time
import urllib.parse
import zlib


DATA = Path("/opt/goby-test/emby-reference-data")
PRIVATE = DATA / "private"
EXPORT = DATA / "export"
ENV_FILE = PRIVATE / "credentials.env"
PLAYBACK_ENV_FILE = PRIVATE / "playback-m3-credentials.env"
SESSION_ENV_FILE = PRIVATE / "session-m3b-credentials.env"
BASE_HEADERS = {
    "Accept": "application/json",
    "Authorization": (
        'Emby Client="Goby Reference Recorder", Device="Linux Test", '
        'DeviceId="goby-reference-recorder", Version="0.1.0"'
    ),
}


def private_write(path: Path, value: str) -> None:
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        stream.write(value)


def read_credentials(path: Path = ENV_FILE) -> dict[str, str]:
    if not path.exists():
        return {}
    if stat.S_IMODE(path.stat().st_mode) != 0o600:
        raise RuntimeError("Credential file must have mode 0600")
    return dict(line.split("=", 1) for line in path.read_text().splitlines() if line)


def save_credentials(values: dict[str, str], path: Path = ENV_FILE) -> None:
    private_write(path, "".join(f"{key}={value}\n" for key, value in values.items()))


class Recorder:
    def __init__(self) -> None:
        os.umask(0o077)
        if (DATA / ".goby-managed").read_text().strip() != "goby-emby-reference-owned-v1":
            raise RuntimeError("Reference ownership marker does not match")
        self.credentials = read_credentials()
        self.credential_path = ENV_FILE
        self.client_headers = dict(BASE_HEADERS)
        self.secret_values = {
            value for key, value in self.credentials.items()
            if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value
        }
        self.secret_values.update(value for key, value in read_credentials(PLAYBACK_ENV_FILE).items() if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)
        self.secret_values.update(value for key, value in read_credentials(SESSION_ENV_FILE).items() if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)

    def clean_text(self, value: str) -> str:
        for secret in self.secret_values:
            value = value.replace(secret, "[REDACTED_SECRET]")
        value = re.sub(r"(?i)(api_key=)[^&\s\"]+", r"\1[REDACTED_TOKEN]", value)
        return value.replace(str(DATA), "/reference-data").replace(
            "/dev/shm/goby-emby-reference/package", "/reference-package"
        )

    def sanitize(self, value, key=""):
        if key.lower() in {"password", "pw", "newpw", "accesstoken", "token", "x-emby-token", "x-mediabrowser-token"} and isinstance(value, str) and value:
            return "[REDACTED_SECRET]"
        if isinstance(value, dict):
            return {child_key: self.sanitize(child_value, child_key) for child_key, child_value in value.items()}
        if isinstance(value, (list, tuple)):
            return [self.sanitize(child_value, key.removesuffix("s")) for child_value in value]
        if isinstance(value, str):
            return self.clean_text(value)
        return value

    def audit_export(self, original, exported, key="") -> None:
        if isinstance(original, dict):
            if not isinstance(exported, dict) or original.keys() != exported.keys():
                raise RuntimeError("Export changed object properties")
            for child_key, child_value in original.items():
                self.audit_export(child_value, exported[child_key], child_key)
        elif isinstance(original, (list, tuple)):
            if not isinstance(exported, list) or len(original) != len(exported):
                raise RuntimeError("Export changed array structure")
            for before, after in zip(original, exported):
                self.audit_export(before, after, key)
        elif isinstance(original, str):
            if not isinstance(exported, str):
                raise RuntimeError("Export changed a string type")
            allowed_sensitive_field = key.lower() in {"password", "pw", "newpw", "accesstoken", "token", "x-emby-token", "x-mediabrowser-token"}
            allowed_secret = any(secret in original for secret in self.secret_values)
            allowed_path = str(DATA) in original or "/dev/shm/goby-emby-reference/package" in original
            allowed_query = bool(re.search(r"(?i)api_key=", original))
            if original != exported and not any((allowed_sensitive_field, allowed_secret, allowed_path, allowed_query)):
                raise RuntimeError(f"Export changed a non-sensitive string field: {key}")
        elif type(original) is not type(exported) or original != exported:
            raise RuntimeError("Export changed a number, boolean, or null")

    def request(self, name, method, path, *, body=None, headers=None, authenticated=False, note=None, binary_image=False, binary_media=False):
        if name.startswith(("artwork-", "entity-", "playback-m3-", "folder-state-", "session-m3b-", "nextup-m3b-", "nextup-long-", "nextup-capability-")) and ((PRIVATE / "raw" / f"{name}.json").exists() or (EXPORT / f"{name}.json").exists()):
            raise RuntimeError(f"Refusing to overwrite extension evidence: {name}")
        request_headers = dict(self.client_headers if headers is None else headers)
        if authenticated:
            request_headers["X-Emby-Token"] = self.credentials["REFERENCE_TOKEN"]
        if isinstance(body, (dict, list)):
            request_headers.setdefault("Content-Type", "application/json")
            wire_body = json.dumps(body, separators=(",", ":")).encode()
        elif isinstance(body, str):
            wire_body = body.encode()
        else:
            wire_body = body
        started = dt.datetime.now(dt.timezone.utc).isoformat()
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=45)
        connection.request(method, path, body=wire_body, headers=request_headers)
        response = connection.getresponse()
        content = response.read()
        status_code = response.status
        response_headers = response.getheaders()
        connection.close()
        content_type = dict((key.lower(), value) for key, value in response_headers).get("content-type", "")
        if binary_image and content and content_type.startswith("image/"):
            parsed = base64.b64encode(content).decode("ascii")
            representation = "binary-base64"
            image_format, width, height = self.image_dimensions(content)
            details = f"Wire image: {len(content)} bytes; SHA-256={hashlib.sha256(content).hexdigest()}; format={image_format}; dimensions={width}x{height}. Body is base64-encoded exact response bytes."
            note = f"{note} {details}" if note else details
        elif binary_media and content and (content_type.startswith(("video/", "audio/")) or content_type.startswith("application/octet-stream")):
            parsed = base64.b64encode(content).decode("ascii")
            representation = "binary-base64"
            details = f"Wire media body: {len(content)} bytes; SHA-256={hashlib.sha256(content).hexdigest()}. Body is base64-encoded exact response bytes."
            note = f"{note} {details}" if note else details
        else:
            text = content.decode("utf-8", errors="replace")
            try:
                parsed = json.loads(text)
                representation = "json"
            except json.JSONDecodeError:
                parsed = text
                representation = "text"
        if isinstance(parsed, dict) and parsed.get("AccessToken"):
            self.credentials["REFERENCE_TOKEN"] = parsed["AccessToken"]
            self.secret_values.add(parsed["AccessToken"])
            if parsed.get("User", {}).get("Id"):
                self.credentials["REFERENCE_USER_ID"] = parsed["User"]["Id"]
            save_credentials(self.credentials, self.credential_path)
        record = {
            "reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": started},
            "request": {"method": method, "path": path, "headers": request_headers, "body": body},
            "response": {"status": status_code, "headers": response_headers, "bodyType": representation, "body": parsed},
        }
        if note:
            record["observation"] = note
        private_write(PRIVATE / "raw" / f"{name}.json", json.dumps(record, indent=2) + "\n")
        private_write(EXPORT / f"{name}.json", json.dumps(self.sanitize(record), indent=2, ensure_ascii=False) + "\n")
        length = len(parsed) if isinstance(parsed, (list, dict, str)) else 0
        print(f"{name}: HTTP {status_code}, {representation}, top-level length={length}", flush=True)
        return status_code, parsed

    @staticmethod
    def image_dimensions(content: bytes) -> tuple[str, int, int]:
        if content.startswith(b"\x89PNG\r\n\x1a\n") and len(content) >= 24:
            width, height = struct.unpack(">II", content[16:24])
            return "png", width, height
        if content.startswith(b"\xff\xd8"):
            offset = 2
            while offset + 4 <= len(content):
                if content[offset] != 0xFF:
                    offset += 1
                    continue
                while offset < len(content) and content[offset] == 0xFF:
                    offset += 1
                marker = content[offset]
                offset += 1
                if marker in {0xD8, 0xD9} or 0xD0 <= marker <= 0xD7:
                    continue
                segment_size = struct.unpack(">H", content[offset:offset + 2])[0]
                if marker in {0xC0, 0xC1, 0xC2, 0xC3, 0xC5, 0xC6, 0xC7, 0xC9, 0xCA, 0xCB, 0xCD, 0xCE, 0xCF}:
                    height, width = struct.unpack(">HH", content[offset + 3:offset + 7])
                    return "jpeg", width, height
                offset += segment_size
        raise RuntimeError("Unexpected image format or missing dimensions")

    @staticmethod
    def synthetic_png(width: int, height: int) -> bytes:
        def chunk(kind: bytes, payload: bytes) -> bytes:
            checksum = zlib.crc32(kind + payload) & 0xFFFFFFFF
            return struct.pack(">I", len(payload)) + kind + payload + struct.pack(">I", checksum)
        pixels = bytearray()
        for y in range(height):
            pixels.append(0)
            for x in range(width):
                pixels.extend(((x // 11 * 29) % 256, (y // 13 * 41) % 256, ((x + y) // 17 * 53) % 256))
        return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(bytes(pixels), 9)) + chunk(b"IEND", b"")

    def artwork_prepare(self) -> None:
        baseline_file = PRIVATE / "artwork-baseline-hashes.json"
        if (PRIVATE / "artwork-source-provenance.json").exists():
            raise RuntimeError("Artwork source fixtures have already been prepared")
        if not baseline_file.exists():
            baseline = {}
            for directory in (PRIVATE / "raw", EXPORT):
                for path in sorted(directory.glob("*.json")):
                    baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
            private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        original = json.loads((PRIVATE / "raw" / "item-detail-default.json").read_text())
        movie = Path(original["response"]["body"]["Path"])
        if movie.parent != Path("/opt/goby-fixtures/movies") or not movie.is_file():
            raise RuntimeError("Unexpected reference media path")
        original_hash = hashlib.sha256(movie.read_bytes()).hexdigest()
        ffmpeg = shutil.which("ffmpeg") or "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg"
        if not Path(ffmpeg).is_file():
            raise RuntimeError("Existing test-env FFmpeg was not found")
        poster_png = self.synthetic_png(160, 240)
        poster = subprocess.run(
            [ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "image2pipe", "-i", "pipe:0", "-frames:v", "1", "-threads", "1", "-q:v", "2", "-f", "image2pipe", "-c:v", "mjpeg", "pipe:1"],
            input=poster_png, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True, timeout=20,
        ).stdout
        if self.image_dimensions(poster) != ("jpeg", 160, 240):
            raise RuntimeError("Generated JPEG dimensions do not match the requested fixture")
        nfo = """<?xml version="1.0" encoding="utf-8"?>
<movie>
  <title>Reference Artwork Film</title>
  <originaltitle>Reference Original Title</originaltitle>
  <year>2024</year>
  <premiered>2024-01-02</premiered>
  <rating>7.5</rating>
  <mpaa>PG-13</mpaa>
  <genre>Drama</genre>
  <genre>Science Fiction</genre>
  <tag>reference</tag>
  <tag>local-artwork</tag>
  <studio>Reference Studio</studio>
  <uniqueid type="imdb" default="true">tt999999999</uniqueid>
  <uniqueid type="tmdb">999999999</uniqueid>
  <actor><name>Reference Actor</name><role>Lead</role><order>0</order></actor>
  <director>Reference Director</director>
</movie>
"""
        sources = {
            movie.with_name(movie.stem + "-poster.jpg"): poster,
            movie.with_name(movie.stem + "-fanart.png"): self.synthetic_png(320, 180),
            movie.with_suffix(".nfo"): nfo.encode(),
        }
        for path, content in sources.items():
            if path.exists():
                raise RuntimeError(f"Refusing to replace existing source fixture: {path.name}")
            with path.open("xb") as stream:
                stream.write(content)
            path.chmod(0o644)
        provenance = {
            "generator": "Python standard-library synthetic color pattern; existing test-env FFmpeg JPEG encoder",
            "mediaPath": str(movie), "mediaSha256Before": original_hash,
            "mediaSha256After": hashlib.sha256(movie.read_bytes()).hexdigest(),
            "sources": [{"path": str(path), "bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()} for path, content in sources.items()],
        }
        if provenance["mediaSha256After"] != original_hash:
            raise RuntimeError("Original movie bytes changed")
        private_write(PRIVATE / "artwork-source-provenance.json", json.dumps(provenance, indent=2) + "\n")
        print("Created one 160x240 JPEG, one 320x180 PNG, and one synthetic NFO; original movie SHA-256 is unchanged.", flush=True)

    def artwork_refresh(self) -> None:
        if not (PRIVATE / "artwork-source-provenance.json").exists():
            raise RuntimeError("Artwork preparation must complete before refreshing the item")
        original = json.loads((PRIVATE / "raw" / "item-detail-default.json").read_text())
        item_id = original["response"]["body"]["Id"]
        self.request(
            "artwork-refresh", "POST", f"/emby/Items/{item_id}/Refresh?Recursive=false&MetadataRefreshMode=FullRefresh&ImageRefreshMode=FullRefresh&ReplaceAllMetadata=true&ReplaceAllImages=true",
            body={}, authenticated=True,
            note="Only the existing synthetic reference movie is refreshed after adding local artwork and a same-basename NFO. No full-library scan is requested.",
        )

    def artwork(self) -> None:
        original = json.loads((PRIVATE / "raw" / "item-detail-default.json").read_text())
        item_id = original["response"]["body"]["Id"]
        user_id = self.credentials["REFERENCE_USER_ID"]
        self.request("artwork-nfo-default-items", "GET", f"/emby/Users/{user_id}/Items?Ids={item_id}", authenticated=True)
        _, detail = self.request("artwork-nfo-detail", "GET", f"/emby/Users/{user_id}/Items/{item_id}", authenticated=True)
        self.request("artwork-nfo-projected-items", "GET", f"/emby/Users/{user_id}/Items?Ids={item_id}&Fields=ProviderIds,Genres,Tags,Studios,People", authenticated=True)
        if not isinstance(detail, dict) or not detail.get("ImageTags", {}).get("Primary"):
            raise RuntimeError("Local primary artwork was not identified; refusing to capture misleading image results")
        image_path = f"/emby/Items/{item_id}/Images/Primary"
        self.request("artwork-images-list", "GET", f"/emby/Items/{item_id}/Images", authenticated=True)
        self.request("artwork-primary-get", "GET", image_path, authenticated=True, binary_image=True)
        self.request("artwork-primary-head", "HEAD", image_path, authenticated=True, binary_image=True)
        self.request("artwork-primary-index-zero", "GET", image_path + "/0", authenticated=True, binary_image=True)
        self.request("artwork-primary-index-one", "GET", image_path + "/1", authenticated=True, binary_image=True)
        for name, query in (
            ("width-64", "Width=64"), ("maxwidth-64", "MaxWidth=64"), ("maxwidth-320", "MaxWidth=320"),
            ("format-png", "Format=png"), ("quality-30", "Format=jpg&Quality=30"), ("quality-90", "Format=jpg&Quality=90"),
        ):
            self.request("artwork-primary-" + name, "GET", image_path + "?" + query, authenticated=True, binary_image=True)
        primary = json.loads((PRIVATE / "raw" / "artwork-primary-get.json").read_text())
        etag = next((value for key, value in primary["response"]["headers"] if key.lower() == "etag"), None)
        if etag:
            self.request("artwork-primary-if-none-match-get", "GET", image_path, headers={**BASE_HEADERS, "If-None-Match": etag}, authenticated=True, binary_image=True)
            self.request("artwork-primary-if-none-match-head", "HEAD", image_path, headers={**BASE_HEADERS, "If-None-Match": etag}, authenticated=True, binary_image=True)
        self.request("artwork-primary-no-token", "GET", image_path, headers={"Accept": "image/*"}, binary_image=True)
        self.request("artwork-primary-invalid-token", "GET", image_path, headers={"Accept": "image/*", "X-Emby-Token": "invalid-reference-token"}, binary_image=True)
        self.request("artwork-images-list-no-token", "GET", f"/emby/Items/{item_id}/Images", headers={"Accept": "application/json"})
        self.request("artwork-images-list-invalid-token", "GET", f"/emby/Items/{item_id}/Images", headers={"Accept": "application/json", "X-Emby-Token": "invalid-reference-token"})
        self.request("artwork-primary-tagged", "GET", image_path + "?" + urllib.parse.urlencode({"Tag": detail["ImageTags"]["Primary"]}), authenticated=True, binary_image=True)
        self.request("artwork-backdrop-original", "GET", f"/emby/Items/{item_id}/Images/Backdrop/0?Format=original", authenticated=True, binary_image=True)

    def export_artwork(self) -> None:
        self.export(prefix="artwork-")
        baseline = json.loads((PRIVATE / "artwork-baseline-hashes.json").read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("A pre-existing baseline capture was changed")
        print(f"Preserved all {len(baseline)} pre-existing raw/export fixture files byte-for-byte.", flush=True)

    def artwork_scalars(self) -> None:
        original = json.loads((PRIVATE / "raw" / "item-detail-default.json").read_text())
        item_id = original["response"]["body"]["Id"]
        user_id = self.credentials["REFERENCE_USER_ID"]
        fields = "ProductionYear,PremiereDate,OriginalTitle,CommunityRating,OfficialRating,Overview,SortName,DateCreated"
        self.request(
            "artwork-nfo-scalar-fields", "GET", f"/emby/Users/{user_id}/Items?Ids={item_id}&Fields={fields}",
            authenticated=True,
            note="Scalar field names are requested explicitly against the same NFO-backed movie. The NFO fixture does not contain an overview.",
        )

    def entities_prepare(self) -> None:
        baseline_file = PRIVATE / "entities-baseline-hashes.json"
        if baseline_file.exists():
            raise RuntimeError("Entity capture baseline is already recorded")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        print(f"Recorded hashes of {len(baseline)} pre-existing raw/export fixtures; no server state changed.", flush=True)

    def entity_lists(self) -> None:
        if not (PRIVATE / "entities-baseline-hashes.json").exists():
            raise RuntimeError("Entity baseline must be recorded before capture")
        user_id = self.credentials["REFERENCE_USER_ID"]
        for route in ("Genres", "Tags", "Studios", "Persons"):
            self.request(f"entity-list-{route.lower()}", "GET", f"/emby/{route}?UserId={user_id}", authenticated=True)

    def entity_navigation(self) -> None:
        user_id = self.credentials["REFERENCE_USER_ID"]
        original = json.loads((PRIVATE / "raw" / "artwork-nfo-detail.json").read_text())
        movie = original["response"]["body"]
        genre = next(item for item in movie["GenreItems"] if item["Name"] == "Drama")
        tag = next(item for item in movie["TagItems"] if item["Name"] == "reference")
        studio = movie["Studios"][0]
        person = next(item for item in movie["People"] if item["Name"] == "Reference Actor")
        for route, entity in (("Genres", genre), ("Studios", studio), ("Persons", person)):
            encoded_name = urllib.parse.quote(entity["Name"], safe="")
            self.request(f"entity-name-{route.lower()}", "GET", f"/emby/{route}/{encoded_name}?UserId={user_id}", authenticated=True)
        for label, entity in (("genre", genre), ("person", person)):
            self.request(f"entity-item-detail-{label}", "GET", f"/emby/Users/{user_id}/Items/{entity['Id']}", authenticated=True)
        for parameter, entity in (("GenreIds", genre), ("TagIds", tag), ("StudioIds", studio), ("PersonIds", person)):
            query = urllib.parse.urlencode({"UserId": user_id, "Recursive": "true", parameter: str(entity["Id"])})
            self.request(f"entity-filter-{parameter.lower()}", "GET", "/emby/Items?" + query, authenticated=True)
        for parameter, value in (("Genres", "Drama|Science Fiction"), ("Tags", "reference|local-artwork")):
            query = urllib.parse.urlencode({"UserId": user_id, "Recursive": "true", parameter: value})
            self.request(f"entity-filter-{parameter.lower()}-pipe", "GET", "/emby/Items?" + query, authenticated=True)
        self.request(
            "entity-genres-movie-page", "GET", f"/emby/Genres?UserId={user_id}&IncludeItemTypes=Movie&StartIndex=1&Limit=1",
            authenticated=True,
        )
        query = urllib.parse.urlencode({
            "UserId": user_id, "Recursive": "true", "Genres": "Drama|Missing Reference Genre",
            "Tags": "reference|Missing Reference Tag",
        })
        self.request(
            "entity-filter-mixed-positive-missing", "GET", "/emby/Items?" + query, authenticated=True,
            note="Each name filter contains one existing matching value and one nonexistent value; no data was created to satisfy the missing values.",
        )
        query = urllib.parse.urlencode({
            "UserId": user_id, "Recursive": "true", "Genres": "Drama", "Tags": "Missing Reference Tag",
        })
        self.request(
            "entity-filter-negative-tag", "GET", "/emby/Items?" + query, authenticated=True,
            note="The genre matches the synthetic movie but the tag does not exist; this distinguishes a restrictive negative tag from an ignored filter.",
        )

    def export_entities(self) -> None:
        self.export(prefix="entity-")
        baseline = json.loads((PRIVATE / "entities-baseline-hashes.json").read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("A pre-existing baseline capture was changed")
        print(f"Preserved all {len(baseline)} pre-existing raw/export fixture files byte-for-byte.", flush=True)

    @staticmethod
    def m3_profile() -> dict:
        return {
            "Name": "M3 MP4 H264 AAC client", "MaxStreamingBitrate": 200000000,
            "DirectPlayProfiles": [{"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"}],
            "TranscodingProfiles": [{"Container": "ts", "Type": "Video", "VideoCodec": "h264", "AudioCodec": "aac", "Protocol": "hls", "Context": "Streaming", "MaxAudioChannels": "2", "MinSegments": 1, "SegmentLength": 3}],
            "SubtitleProfiles": [{"Format": "srt", "Method": "External"}, {"Format": "vtt", "Method": "External"}],
        }

    def playback_m3_begin(self) -> None:
        baseline_file = PRIVATE / "playback-m3-baseline-hashes.json"
        if baseline_file.exists():
            raise RuntimeError("M3 baseline already exists")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        print(f"Recorded {len(baseline)} pre-existing raw/export hashes.", flush=True)

    def playback_m3_flags(self) -> None:
        original = json.loads((PRIVATE / "raw" / "item-detail-default.json").read_text())
        item_id = original["response"]["body"]["Id"]
        user_id = self.credentials["REFERENCE_USER_ID"]
        path = f"/emby/Items/{item_id}/PlaybackInfo"
        self.request("playback-m3-short-get", "GET", path + "?UserId=" + user_id, authenticated=True)
        self.request("playback-m3-short-post-minimal", "POST", path, body={"UserId": user_id}, authenticated=True)
        profile = self.m3_profile()
        self.request("playback-m3-short-profile-match", "POST", path, body={"UserId": user_id, "DeviceProfile": profile}, authenticated=True)
        mismatch = self.m3_profile()
        mismatch["DirectPlayProfiles"][0]["VideoCodec"] = "hevc"
        self.request("playback-m3-short-profile-mismatch", "POST", path, body={"UserId": user_id, "DeviceProfile": mismatch}, authenticated=True)
        self.request("playback-m3-short-directstream-only", "POST", path, body={"UserId": user_id, "DeviceProfile": profile, "EnableDirectPlay": False, "EnableDirectStream": True, "EnableTranscoding": False}, authenticated=True)
        self.request("playback-m3-short-all-disabled", "POST", path, body={"UserId": user_id, "DeviceProfile": profile, "EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": False}, authenticated=True)

    def playback_m3_prepare(self) -> None:
        target = Path("/opt/goby-fixtures/playback-reference")
        if target.exists() or PLAYBACK_ENV_FILE.exists():
            raise RuntimeError("Dedicated playback source or credentials already exist")
        if not (PRIVATE / "playback-m3-baseline-hashes.json").exists():
            raise RuntimeError("M3 baseline must be recorded first")
        target.mkdir(mode=0o755)
        (target / ".goby-managed").write_text("goby-playback-reference-owned-v1\n")
        movie = target / "Reference Playback M3.mp4"
        ffmpeg = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg"
        subprocess.run([
            ffmpeg, "-nostdin", "-hide_banner", "-loglevel", "error",
            "-f", "lavfi", "-i", "color=c=black:s=160x90:r=1",
            "-f", "lavfi", "-i", "anullsrc=r=8000:cl=mono", "-t", "600",
            "-c:v", "libx264", "-preset", "ultrafast", "-crf", "40", "-pix_fmt", "yuv420p", "-g", "60", "-threads", "1",
            "-c:a", "aac", "-b:a", "8k", "-ar", "8000", "-ac", "1",
            "-metadata", "title=Reference Playback M3", "-movflags", "+faststart", str(movie),
        ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True, timeout=90)
        if movie.stat().st_size >= 2 * 1024 * 1024:
            raise RuntimeError("Generated playback fixture exceeds the two-MiB limit")
        subtitle = target / "Reference Playback M3.srt"
        subtitle.write_text("1\n00:00:00,000 --> 00:00:02,000\nReference playback begins.\n\n2\n00:02:00,000 --> 00:02:02,000\nReference resume checkpoint.\n", encoding="utf-8")
        for path in (movie, subtitle, target / ".goby-managed"):
            path.chmod(0o644)
        provenance = [{"path": str(path), "bytes": path.stat().st_size, "sha256": hashlib.sha256(path.read_bytes()).hexdigest()} for path in (movie, subtitle)]
        private_write(PRIVATE / "playback-m3-source-provenance.json", json.dumps(provenance, indent=2) + "\n")
        credentials = {"REFERENCE_USERNAME": "reference-playback-m3", "REFERENCE_PASSWORD": secrets.token_hex(32)}
        save_credentials(credentials, PLAYBACK_ENV_FILE)
        self.secret_values.add(credentials["REFERENCE_PASSWORD"])
        print(f"Generated a 600-second H264/AAC MP4 ({movie.stat().st_size} bytes) and external SRT; dedicated credentials are private.", flush=True)

    def playback_m3_rejection(self) -> None:
        original = json.loads((PRIVATE / "raw" / "item-detail-default.json").read_text())
        profile = self.m3_profile()
        profile["DirectPlayProfiles"][0]["VideoCodec"] = "hevc"
        self.request(
            "playback-m3-short-mismatch-no-transcoding", "POST", f"/emby/Items/{original['response']['body']['Id']}/PlaybackInfo",
            body={"UserId": self.credentials["REFERENCE_USER_ID"], "DeviceProfile": profile, "EnableTranscoding": False, "IsPlayback": True},
            authenticated=True,
        )

    def playback_m3_factorial(self) -> None:
        original = json.loads((PRIVATE / "raw" / "item-detail-default.json").read_text())
        profile = self.m3_profile()
        profile["DirectPlayProfiles"][0]["VideoCodec"] = "hevc"
        for name, transcoding, is_playback in (("mismatch-transcoding-false-playback-false", False, False), ("mismatch-transcoding-true-playback-true", True, True)):
            self.request(
                "playback-m3-short-" + name, "POST", f"/emby/Items/{original['response']['body']['Id']}/PlaybackInfo",
                body={"UserId": self.credentials["REFERENCE_USER_ID"], "DeviceProfile": profile, "EnableTranscoding": transcoding, "IsPlayback": is_playback},
                authenticated=True,
                note="Controlled follow-up: the mismatch profile and all other fields are unchanged; only EnableTranscoding/IsPlayback vary.",
            )

    def playback_m3_setup(self) -> None:
        target = "/opt/goby-fixtures/playback-reference"
        credentials = read_credentials(PLAYBACK_ENV_FILE)
        if not credentials or credentials.get("REFERENCE_TOKEN"):
            raise RuntimeError("Dedicated credential preparation is missing or setup is complete")
        self.secret_values.add(credentials["REFERENCE_PASSWORD"])
        options = {
            "EnableRealtimeMonitor": False, "EnableChapterImageExtraction": False, "EnableMarkerDetection": False,
            "ExtractChapterImagesDuringLibraryScan": False, "EnableMarkerDetectionDuringLibraryScan": False,
            "DownloadImagesInAdvance": False, "SaveLocalMetadata": False, "SaveLocalThumbnailSets": False,
            "SaveSubtitlesWithMedia": False, "SubtitleDownloadLanguages": [], "AutomaticRefreshIntervalDays": 0,
            "EnableEmbeddedTitles": True, "TypeOptions": [{"Type": "Movie", "MetadataFetchers": [], "ImageFetchers": []}],
        }
        self.request("playback-m3-library-create", "POST", "/emby/Library/VirtualFolders", authenticated=True,
                     body={"Name": "Reference Playback M3", "CollectionType": "movies", "RefreshLibrary": False, "Paths": [target], "LibraryOptions": options})
        status_code, created = self.request("playback-m3-user-create", "POST", "/emby/Users/New", body={"Name": credentials["REFERENCE_USERNAME"]}, authenticated=True)
        if status_code != 200 or not created.get("Id"):
            raise RuntimeError("Dedicated playback user creation failed")
        credentials["REFERENCE_USER_ID"] = created["Id"]
        save_credentials(credentials, PLAYBACK_ENV_FILE)
        status_code, _ = self.request("playback-m3-user-password", "POST", f"/emby/Users/{created['Id']}/Password",
                                     body={"Id": created["Id"], "NewPw": credentials["REFERENCE_PASSWORD"], "ResetPassword": False}, authenticated=True)
        if status_code not in {200, 204}:
            raise RuntimeError("Dedicated playback password update failed")
        _, libraries = self.request("playback-m3-library-query", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        library = next(item for item in libraries["Items"] if item["Name"] == "Reference Playback M3")
        private_write(PRIVATE / "playback-m3-library.json", json.dumps(library, indent=2) + "\n")
        self.request("playback-m3-library-refresh", "POST", f"/emby/Items/{library['ItemId']}/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=FullRefresh",
                     body={}, authenticated=True, note="Targeted refresh of the new dedicated library only; no full-server or Goby scan is requested.")
        self.use_playback_m3_credentials()
        status_code, result = self.request("playback-m3-user-login", "POST", "/emby/Users/AuthenticateByName",
                                          body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        if status_code != 200:
            raise RuntimeError("Dedicated playback login failed")
        self.credentials["REFERENCE_SESSION_ID"] = result["SessionInfo"]["Id"]
        save_credentials(self.credentials, PLAYBACK_ENV_FILE)

    def use_playback_m3_credentials(self) -> None:
        self.credentials = read_credentials(PLAYBACK_ENV_FILE)
        self.credential_path = PLAYBACK_ENV_FILE
        self.client_headers = {"Accept": "application/json", "Authorization": BASE_HEADERS["Authorization"].replace("goby-reference-recorder", "goby-playback-m3-recorder")}
        self.secret_values.update(value for key, value in self.credentials.items() if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)

    def playback_m3_long_info(self) -> None:
        self.use_playback_m3_credentials()
        user_id = self.credentials["REFERENCE_USER_ID"]
        query = urllib.parse.urlencode({"UserId": user_id, "Recursive": "true", "Path": "/opt/goby-fixtures/playback-reference/Reference Playback M3.mp4", "Fields": "Path,MediaSources,MediaStreams"})
        _, result = self.request("playback-m3-long-item", "GET", "/emby/Items?" + query, authenticated=True)
        if len(result.get("Items", [])) != 1:
            raise RuntimeError("The new playback fixture is not ready as a single library item")
        item = result["Items"][0]
        _, info = self.request("playback-m3-long-info", "POST", f"/emby/Items/{item['Id']}/PlaybackInfo",
                               body={"UserId": user_id, "DeviceProfile": self.m3_profile(), "IsPlayback": True}, authenticated=True)
        context = {"ItemId": item["Id"], "MediaSourceId": info["MediaSources"][0]["Id"], "PlaySessionId": info["PlaySessionId"],
                   "RunTimeTicks": info["MediaSources"][0]["RunTimeTicks"], "AudioStreamIndex": info["MediaSources"][0].get("DefaultAudioStreamIndex", 1)}
        private_write(PRIVATE / "playback-m3-context.json", json.dumps(context, indent=2) + "\n")

    def playback_m3_transport(self) -> None:
        self.use_playback_m3_credentials()
        context = json.loads((PRIVATE / "playback-m3-context.json").read_text())
        query = urllib.parse.urlencode({"Static": "true", "MediaSourceId": context["MediaSourceId"], "PlaySessionId": context["PlaySessionId"]})
        path = f"/emby/Videos/{context['ItemId']}/stream?" + query
        self.request("playback-m3-media-full", "GET", path, authenticated=True, binary_media=True)
        self.request("playback-m3-media-head", "HEAD", path, authenticated=True, binary_media=True)
        for name, byte_range in (("range-first", "bytes=0-31"), ("range-suffix", "bytes=-32"), ("range-invalid", "bytes=9999999-")):
            self.request("playback-m3-media-" + name, "GET", path, headers={**self.client_headers, "Range": byte_range}, authenticated=True, binary_media=True)
        self.request("playback-m3-media-no-token", "GET", path, headers={"Range": "bytes=0-31"}, binary_media=True,
                     note="No token is supplied, but the URL still contains a PlaySessionId created by authenticated negotiation.")
        self.request("playback-m3-media-invalid-token", "GET", path, headers={"Range": "bytes=0-31", "X-Emby-Token": "invalid-reference-token"}, binary_media=True,
                     note="The invalid token case retains the authenticated negotiation's MediaSourceId and PlaySessionId.")
        self.request("playback-m3-media-root-alias", "GET", path.removeprefix("/emby"), headers={**self.client_headers, "Range": "bytes=0-31"}, authenticated=True, binary_media=True)
        self.request("playback-m3-media-lowercase-alias", "GET", path.replace("/Videos/", "/videos/"), headers={**self.client_headers, "Range": "bytes=0-31"}, authenticated=True, binary_media=True)
        info = json.loads((PRIVATE / "raw" / "playback-m3-long-info.json").read_text())
        direct_url = info["response"]["body"]["MediaSources"][0]["DirectStreamUrl"]
        if not direct_url.startswith("/videos/"):
            raise RuntimeError("Unexpected server-generated direct URL")
        self.request("playback-m3-media-original-alias", "GET", "/emby" + direct_url, headers={"Range": "bytes=0-31"}, binary_media=True,
                     note="Exact server-generated original.mp4 URL, including its query token; no Static parameter or authorization header is added.")
        head = json.loads((PRIVATE / "raw" / "playback-m3-media-head.json").read_text())
        response_headers = {key.lower(): value for key, value in head["response"]["headers"]}
        validator = response_headers.get("etag") or response_headers.get("last-modified")
        if validator:
            self.request("playback-m3-media-if-range", "GET", path, headers={**self.client_headers, "Range": "bytes=0-31", "If-Range": validator}, authenticated=True, binary_media=True)

    def playback_m3_reports(self) -> None:
        self.use_playback_m3_credentials()
        context = json.loads((PRIVATE / "playback-m3-context.json").read_text())
        user_id = self.credentials["REFERENCE_USER_ID"]
        item_id = context["ItemId"]
        detail_path = f"/emby/Users/{user_id}/Items/{item_id}"
        started = {
            **context, "SessionId": self.credentials["REFERENCE_SESSION_ID"], "PositionTicks": 0,
            "CanSeek": True, "IsPaused": False, "IsMuted": False, "VolumeLevel": 100,
            "PlayMethod": "DirectStream", "SubtitleStreamIndex": -1, "PlaybackRate": 1,
        }
        self.request("playback-m3-started", "POST", "/emby/Sessions/Playing", body=started, authenticated=True)
        self.request("playback-m3-detail-after-start", "GET", detail_path, authenticated=True)
        progress = {**started, "PositionTicks": 1200000000, "EventName": "TimeUpdate"}
        self.request("playback-m3-progress-120", "POST", "/emby/Sessions/Playing/Progress", body=progress, authenticated=True,
                     note="Client-reported 120-second position; no real-time playback wait or decoding claim is made.")
        self.request("playback-m3-detail-after-progress", "GET", detail_path, authenticated=True)
        self.request("playback-m3-resume-after-progress", "GET", f"/emby/Users/{user_id}/Items/Resume?MediaTypes=Video&Limit=10", authenticated=True)
        stopped = {key: value for key, value in context.items() if key in {"ItemId", "MediaSourceId", "PlaySessionId"}}
        stopped.update({"SessionId": self.credentials["REFERENCE_SESSION_ID"], "PositionTicks": 1200000000, "Failed": False, "IsAutomated": False})
        self.request("playback-m3-stopped-120", "POST", "/emby/Sessions/Playing/Stopped", body=stopped, authenticated=True)
        self.request("playback-m3-detail-after-stop", "GET", detail_path, authenticated=True)
        self.request("playback-m3-stopped-duplicate", "POST", "/emby/Sessions/Playing/Stopped", body=stopped, authenticated=True,
                     note="Exact duplicate stop report for the same dedicated user and play session.")
        self.request("playback-m3-detail-after-duplicate-stop", "GET", detail_path, authenticated=True)
        for name, method, route in (
            ("mark-played", "POST", "PlayedItems"), ("mark-unplayed", "DELETE", "PlayedItems"),
            ("favorite-add", "POST", "FavoriteItems"), ("favorite-remove", "DELETE", "FavoriteItems"),
        ):
            self.request("playback-m3-" + name, method, f"/emby/Users/{user_id}/{route}/{item_id}", authenticated=True)
        self.request("playback-m3-detail-final", "GET", detail_path, authenticated=True)

    def export_playback_m3(self) -> None:
        self.export(prefix="playback-m3-")
        records = [json.loads(path.read_text()) for path in (PRIVATE / "raw").glob("playback-m3-*.json")]
        if len(records) > 42:
            raise RuntimeError("M3 capture exceeds the authorized fixture limit")
        source_path = Path("/opt/goby-fixtures/playback-reference/Reference Playback M3.mp4")
        if source_path.exists():
            source = source_path.read_bytes()
            checked = 0
            for record in records:
                response = record["response"]
                if response["bodyType"] != "binary-base64":
                    continue
                content = base64.b64decode(response["body"], validate=True)
                headers = {key.lower(): value for key, value in response["headers"]}
                content_range = headers.get("content-range")
                if content_range:
                    match = re.fullmatch(r"bytes (\d+)-(\d+)/(\d+)", content_range)
                    if not match:
                        raise RuntimeError("Unexpected recorded Content-Range")
                    start, end, total = map(int, match.groups())
                    if total != len(source) or content != source[start:end + 1]:
                        raise RuntimeError("Recorded media bytes disagree with Content-Range")
                elif content != source:
                    raise RuntimeError("Recorded full media response differs from the source")
                if int(headers["content-length"]) != len(content):
                    raise RuntimeError("Recorded media Content-Length does not match the bytes")
                checked += 1
            print(f"Checked {checked} media bodies against the source and original length/range headers.", flush=True)
        baseline = json.loads((PRIVATE / "playback-m3-baseline-hashes.json").read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("A pre-existing M3 baseline file changed")
        print(f"Preserved all {len(baseline)} pre-existing raw/export files byte-for-byte.", flush=True)

    def folder_state_begin(self) -> None:
        baseline_file = PRIVATE / "folder-state-baseline-hashes.json"
        if baseline_file.exists():
            raise RuntimeError("Folder-state baseline already exists")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        print(f"Recorded {len(baseline)} pre-existing raw/export hashes.", flush=True)

    def folder_state(self) -> None:
        if not (PRIVATE / "folder-state-baseline-hashes.json").exists():
            raise RuntimeError("Folder-state baseline must be recorded first")
        self.use_playback_m3_credentials()
        original = json.loads((PRIVATE / "raw" / "items-type-series.json").read_text())
        series = next(item for item in original["response"]["body"]["Items"] if item["Name"] == "Example Series")
        user_id = self.credentials["REFERENCE_USER_ID"]
        state_path = f"/emby/Users/{user_id}/PlayedItems/{series['Id']}"
        episodes_path = f"/emby/Shows/{series['Id']}/Episodes?UserId={user_id}"
        try:
            self.request("folder-state-series-played", "POST", state_path, authenticated=True,
                         note="Marks only the dedicated M3 user's existing synthetic series as played; no other user's state is targeted.")
            self.request("folder-state-episodes-after-played", "GET", episodes_path, authenticated=True)
        finally:
            self.request("folder-state-series-unplayed", "DELETE", state_path, authenticated=True,
                         note="Restores the dedicated M3 user's synthetic series to unplayed through the ordinary API.")
        self.request("folder-state-episodes-after-unplayed", "GET", episodes_path, authenticated=True)

    def export_folder_state(self) -> None:
        self.export(prefix="folder-state-")
        if len(list((PRIVATE / "raw").glob("folder-state-*.json"))) > 4:
            raise RuntimeError("Folder-state capture exceeds the authorized fixture bound")
        baseline = json.loads((PRIVATE / "folder-state-baseline-hashes.json").read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("A pre-existing folder-state baseline file changed")
        print(f"Preserved all {len(baseline)} pre-existing raw/export files byte-for-byte.", flush=True)

    def session_m3b_begin(self) -> None:
        baseline_file = PRIVATE / "session-m3b-baseline-hashes.json"
        if baseline_file.exists() or SESSION_ENV_FILE.exists():
            raise RuntimeError("Session M3b preparation already exists")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        credentials = {"REFERENCE_USERNAME": "reference-session-m3b", "REFERENCE_PASSWORD": secrets.token_hex(32)}
        save_credentials(credentials, SESSION_ENV_FILE)
        self.secret_values.add(credentials["REFERENCE_PASSWORD"])
        print(f"Recorded {len(baseline)} original raw/export hashes; dedicated credentials are private.", flush=True)

    def use_session_m3b_credentials(self) -> None:
        self.credentials = read_credentials(SESSION_ENV_FILE)
        self.credential_path = SESSION_ENV_FILE
        self.client_headers = {"Accept": "application/json", "Authorization": 'Emby Client="Goby Reference Recorder", Device="Linux Session Test", DeviceId="goby-session-m3b-recorder", Version="0.1.0"'}
        self.secret_values.update(value for key, value in self.credentials.items() if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)

    def session_m3b_setup(self) -> None:
        credentials = read_credentials(SESSION_ENV_FILE)
        if not credentials or credentials.get("REFERENCE_TOKEN"):
            raise RuntimeError("Dedicated session preparation is missing or already completed")
        status_code, created = self.request("session-m3b-user-create", "POST", "/emby/Users/New", body={"Name": credentials["REFERENCE_USERNAME"]}, authenticated=True)
        if status_code != 200 or not created.get("Id"):
            raise RuntimeError("Dedicated session user creation failed")
        credentials["REFERENCE_USER_ID"] = created["Id"]
        save_credentials(credentials, SESSION_ENV_FILE)
        status_code, _ = self.request("session-m3b-user-password", "POST", f"/emby/Users/{created['Id']}/Password",
                                     body={"Id": created["Id"], "NewPw": credentials["REFERENCE_PASSWORD"], "ResetPassword": False}, authenticated=True)
        if status_code not in {200, 204}:
            raise RuntimeError("Dedicated session password update failed")
        self.use_session_m3b_credentials()
        status_code, result = self.request("session-m3b-user-login", "POST", "/emby/Users/AuthenticateByName",
                                          body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        if status_code != 200:
            raise RuntimeError("Dedicated session login failed")
        self.credentials["REFERENCE_SESSION_ID"] = result["SessionInfo"]["Id"]
        save_credentials(self.credentials, SESSION_ENV_FILE)

    def session_m3b_lists(self) -> None:
        self.request("session-m3b-list-admin", "GET", "/emby/Sessions", authenticated=True,
                     note="Read-only administrator request; no capability or playback mutation targets this administrator session.")
        self.use_session_m3b_credentials()
        session_id = self.credentials["REFERENCE_SESSION_ID"]
        for name, query in (
            ("ordinary", ""), ("device", "?DeviceId=goby-session-m3b-recorder"),
            ("id", "?Id=" + session_id), ("missing-id", "?Id=ffffffffffffffffffffffffffffffff"),
            ("active-zero", "?ActiveWithinSeconds=0"), ("active-hour", "?ActiveWithinSeconds=3600"),
        ):
            self.request("session-m3b-list-" + name, "GET", "/emby/Sessions" + query, authenticated=True)

    def session_m3b_caps(self) -> None:
        self.use_session_m3b_credentials()
        session_id = self.credentials["REFERENCE_SESSION_ID"]
        own_query = "?Id=" + session_id
        own_path = "/emby/Sessions" + own_query
        self.request("session-m3b-caps-simple-empty", "POST", "/emby/Sessions/Capabilities" + own_query, authenticated=True)
        self.request("session-m3b-after-simple-empty", "GET", own_path, authenticated=True)
        populated = "&PlayableMediaTypes=Audio,Video&SupportedCommands=PlayMediaSource,SetVolume&SupportsMediaControl=true&SupportsSync=false"
        self.request("session-m3b-caps-simple-populated", "POST", "/emby/Sessions/Capabilities" + own_query + populated, authenticated=True)
        self.request("session-m3b-after-simple-populated", "GET", own_path, authenticated=True)
        self.request("session-m3b-caps-full-empty", "POST", "/emby/Sessions/Capabilities/Full" + own_query, body={}, authenticated=True)
        self.request("session-m3b-after-full-empty", "GET", own_path, authenticated=True)
        profile = self.m3_profile()
        profile["Name"] = "Stored HEVC-only direct profile"
        profile["DirectPlayProfiles"][0]["VideoCodec"] = "hevc"
        full = {"PlayableMediaTypes": ["Video", "Audio"], "SupportedCommands": ["PlayMediaSource", "SetAudioStreamIndex", "SetSubtitleStreamIndex", "SetVolume"],
                "SupportsMediaControl": True, "SupportsSync": False, "DeviceProfile": profile}
        self.request("session-m3b-caps-full-populated", "POST", "/emby/Sessions/Capabilities/Full" + own_query, body=full, authenticated=True)
        self.request("session-m3b-after-full-populated", "GET", own_path, authenticated=True)
        context = json.loads((PRIVATE / "playback-m3-context.json").read_text())
        _, info = self.request("session-m3b-playbackinfo-after-profile", "POST", f"/emby/Items/{context['ItemId']}/PlaybackInfo",
                               body={"UserId": self.credentials["REFERENCE_USER_ID"]}, authenticated=True,
                               note="No request DeviceProfile is supplied after uploading a mismatching HEVC-only direct profile through Full capabilities.")
        private_write(PRIVATE / "session-m3b-play-session.json", json.dumps({"PlaySessionId": info["PlaySessionId"]}) + "\n")
        self.request("session-m3b-caps-simple-missing-id", "POST", "/emby/Sessions/Capabilities?PlayableMediaTypes=Audio&SupportedCommands=VolumeUp&SupportsMediaControl=true", authenticated=True)
        self.request("session-m3b-after-missing-id", "GET", own_path, authenticated=True)
        self.request("session-m3b-caps-full-unknown-id", "POST", "/emby/Sessions/Capabilities/Full?Id=ffffffffffffffffffffffffffffffff",
                     body={"PlayableMediaTypes": ["Audio"], "SupportedCommands": ["VolumeDown"], "SupportsMediaControl": True}, authenticated=True,
                     note="The supplied session ID is nonexistent; no other user's existing session is targeted.")
        self.request("session-m3b-after-unknown-id", "GET", own_path, authenticated=True)

    def session_m3b_ping(self) -> None:
        self.use_session_m3b_credentials()
        play_session = json.loads((PRIVATE / "session-m3b-play-session.json").read_text())["PlaySessionId"]
        for name, query in (("omitted", ""), ("unknown", "?PlaySessionId=ffffffffffffffffffffffffffffffff"), ("negotiated", "?PlaySessionId=" + play_session)):
            self.request("session-m3b-ping-" + name, "POST", "/emby/Sessions/Playing/Ping" + query, authenticated=True)

    def nextup_m3b(self) -> None:
        self.use_session_m3b_credentials()
        user_id = self.credentials["REFERENCE_USER_ID"]
        original = json.loads((PRIVATE / "raw" / "items-type-series.json").read_text())
        series_id = next(item["Id"] for item in original["response"]["body"]["Items"] if item["Name"] == "Example Series")
        original_episodes = json.loads((PRIVATE / "raw" / "shows-episodes.json").read_text())["response"]["body"]["Items"]
        ordered = sorted(original_episodes, key=lambda item: (item["ParentIndexNumber"], item["IndexNumber"]))
        first_id, middle_id = ordered[0]["Id"], ordered[1]["Id"]
        libraries = json.loads((PRIVATE / "raw" / "library-query-after-scan.json").read_text())["response"]["body"]["Items"]
        parent_id = next(item["ItemId"] for item in libraries if item["CollectionType"] == "tvshows")
        base = "/emby/Shows/NextUp?UserId=" + user_id
        state_prefix = f"/emby/Users/{user_id}/PlayedItems/"
        try:
            self.request("nextup-m3b-unstarted-default", "GET", base, authenticated=True)
            self.request("nextup-m3b-unstarted-series", "GET", base + "&SeriesId=" + series_id, authenticated=True)
            self.request("nextup-m3b-middle-played", "POST", state_prefix + middle_id, authenticated=True)
            self.request("nextup-m3b-after-middle", "GET", base, authenticated=True,
                         note="Only S01E02 is marked played; S01E01 remains an earlier gap and S02E01 is a later episode.")
            self.request("nextup-m3b-middle-unplayed", "DELETE", state_prefix + middle_id, authenticated=True)
            self.request("nextup-m3b-first-played", "POST", state_prefix + first_id, authenticated=True)
            self.request("nextup-m3b-after-first", "GET", base, authenticated=True)
            self.request("nextup-m3b-after-first-filtered", "GET", base + f"&SeriesId={series_id}&ParentId={parent_id}&EnableResumable=true&EnableRewatching=false", authenticated=True)
            self.request("nextup-m3b-series-played", "POST", state_prefix + series_id, authenticated=True)
            self.request("nextup-m3b-all-played-default", "GET", base, authenticated=True)
            self.request("nextup-m3b-all-played-rewatch", "GET", base + f"&SeriesId={series_id}&EnableRewatching=true", authenticated=True)
        finally:
            self.request("nextup-m3b-series-restored", "DELETE", state_prefix + series_id, authenticated=True,
                         note="Restores the new dedicated user's series and descendants to unplayed after the bounded NextUp sequence.")

    def nextup_m3b_split_begin(self) -> None:
        baseline_file = PRIVATE / "nextup-m3b-split-baseline-hashes.json"
        if baseline_file.exists():
            raise RuntimeError("NextUp split baseline already exists")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        print(f"Recorded {len(baseline)} existing raw/export hashes before additive NextUp controls.", flush=True)

    def nextup_m3b_split(self) -> None:
        self.use_session_m3b_credentials()
        user_id = self.credentials["REFERENCE_USER_ID"]
        original = json.loads((PRIVATE / "raw" / "items-type-series.json").read_text())
        series_id = next(item["Id"] for item in original["response"]["body"]["Items"] if item["Name"] == "Example Series")
        episodes = json.loads((PRIVATE / "raw" / "shows-episodes.json").read_text())["response"]["body"]["Items"]
        ordered = sorted(episodes, key=lambda item: (item["ParentIndexNumber"], item["IndexNumber"]))
        first_id, middle_id = ordered[0]["Id"], ordered[1]["Id"]
        libraries = json.loads((PRIVATE / "raw" / "library-query-after-scan.json").read_text())["response"]["body"]["Items"]
        parent_id = next(item["ItemId"] for item in libraries if item["CollectionType"] == "tvshows")
        base = "/emby/Shows/NextUp?UserId=" + user_id
        state_prefix = f"/emby/Users/{user_id}/PlayedItems/"
        try:
            self.request("nextup-m3b-split-unplayed-parent", "GET", base + "&ParentId=" + parent_id, authenticated=True)
            self.request("nextup-m3b-split-unplayed-series-parent", "GET", base + f"&SeriesId={series_id}&ParentId={parent_id}", authenticated=True)
            self.request("nextup-m3b-split-middle-played", "POST", state_prefix + middle_id, authenticated=True)
            time.sleep(2)
            for label, suffix in (("default", ""), ("series", "&SeriesId=" + series_id), ("parent", "&ParentId=" + parent_id), ("series-parent", f"&SeriesId={series_id}&ParentId={parent_id}")):
                self.request("nextup-m3b-split-middle-" + label, "GET", base + suffix, authenticated=True,
                             note="Read after a two-second wait following the middle-episode played mutation; the first episode remains an earlier gap.")
            self.request("nextup-m3b-split-middle-unplayed", "DELETE", state_prefix + middle_id, authenticated=True)
            self.request("nextup-m3b-split-first-played", "POST", state_prefix + first_id, authenticated=True)
            self.request("nextup-m3b-split-first-series-parent", "GET", base + f"&SeriesId={series_id}&ParentId={parent_id}", authenticated=True)
            time.sleep(30)
            self.request("nextup-m3b-split-first-default-delayed", "GET", base, authenticated=True,
                         note="Default NextUp read after a 30-second wait; no resume or rewatch flags are supplied.")
        finally:
            self.request("nextup-m3b-split-series-restored", "DELETE", state_prefix + series_id, authenticated=True,
                         note="Restores the dedicated new user's entire synthetic series to unplayed after the additional controls.")

    def nextup_m3b_playback_begin(self) -> None:
        baseline_file = PRIVATE / "nextup-m3b-playback-baseline-hashes.json"
        if baseline_file.exists():
            raise RuntimeError("NextUp playback-control baseline already exists")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        print(f"Recorded {len(baseline)} raw/export hashes before playback-event controls.", flush=True)

    def nextup_m3b_playback(self) -> None:
        self.use_session_m3b_credentials()
        user_id = self.credentials["REFERENCE_USER_ID"]
        original = json.loads((PRIVATE / "raw" / "items-type-series.json").read_text())
        series_id = next(item["Id"] for item in original["response"]["body"]["Items"] if item["Name"] == "Example Series")
        episodes = json.loads((PRIVATE / "raw" / "shows-episodes.json").read_text())["response"]["body"]["Items"]
        first_id = min(episodes, key=lambda item: (item["ParentIndexNumber"], item["IndexNumber"]))["Id"]
        libraries = json.loads((PRIVATE / "raw" / "library-query-after-scan.json").read_text())["response"]["body"]["Items"]
        parent_id = next(item["ItemId"] for item in libraries if item["CollectionType"] == "tvshows")
        base = "/emby/Shows/NextUp?UserId=" + user_id
        try:
            _, info = self.request("nextup-m3b-playback-info", "POST", f"/emby/Items/{first_id}/PlaybackInfo",
                                   body={"UserId": user_id, "IsPlayback": True}, authenticated=True)
            source = info["MediaSources"][0]
            started = {"ItemId": first_id, "MediaSourceId": source["Id"], "PlaySessionId": info["PlaySessionId"],
                       "SessionId": self.credentials["REFERENCE_SESSION_ID"], "RunTimeTicks": source["RunTimeTicks"],
                       "PositionTicks": 0, "CanSeek": True, "IsPaused": False, "IsMuted": False, "PlayMethod": "DirectStream", "PlaybackRate": 1}
            self.request("nextup-m3b-playback-started", "POST", "/emby/Sessions/Playing", body=started, authenticated=True)
            stopped = {key: value for key, value in started.items() if key in {"ItemId", "MediaSourceId", "PlaySessionId", "SessionId"}}
            stopped.update({"PositionTicks": source["RunTimeTicks"], "Failed": False, "IsAutomated": False})
            self.request("nextup-m3b-playback-stopped", "POST", "/emby/Sessions/Playing/Stopped", body=stopped, authenticated=True,
                         note="Completion is reported at the source's actual two-second duration; this is a protocol event test, not a real-time decode claim.")
            self.request("nextup-m3b-playback-episode-detail", "GET", f"/emby/Users/{user_id}/Items/{first_id}", authenticated=True)
            time.sleep(2)
            for label, suffix in (("default", ""), ("parent", "&ParentId=" + parent_id), ("series", "&SeriesId=" + series_id)):
                self.request("nextup-m3b-playback-" + label, "GET", base + suffix, authenticated=True,
                             note="Read after a two-second wait following Started/Stopped completion reports.")
            self.request("nextup-m3b-playback-series-detail", "GET", f"/emby/Users/{user_id}/Items/{series_id}", authenticated=True)
        finally:
            self.request("nextup-m3b-playback-series-restored", "DELETE", f"/emby/Users/{user_id}/PlayedItems/{series_id}", authenticated=True,
                         note="Restores the dedicated user's series and descendants to unplayed after the playback-event controls.")

    def export_session_m3b(self) -> None:
        self.export(prefix="session-m3b-")
        self.export(prefix="nextup-m3b-")
        if len(list((PRIVATE / "raw").glob("session-m3b-*.json"))) > 26 or len(list((PRIVATE / "raw").glob("nextup-m3b-*.json"))) > 36:
            raise RuntimeError("Session/NextUp capture exceeds its bounded plan")
        baseline = json.loads((PRIVATE / "session-m3b-baseline-hashes.json").read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("A previous session/NextUp baseline capture changed")
        print(f"Preserved all {len(baseline)} pre-existing raw/export files byte-for-byte.", flush=True)
        split_file = PRIVATE / "nextup-m3b-split-baseline-hashes.json"
        if split_file.exists():
            split_baseline = json.loads(split_file.read_text())
            for name, expected in split_baseline.items():
                if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                    raise RuntimeError("Existing evidence changed during additive NextUp controls")
            print(f"Preserved all {len(split_baseline)} raw/export files from before the split controls.", flush=True)
        playback_file = PRIVATE / "nextup-m3b-playback-baseline-hashes.json"
        if playback_file.exists():
            playback_baseline = json.loads(playback_file.read_text())
            for name, expected in playback_baseline.items():
                if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                    raise RuntimeError("Existing evidence changed during playback-event controls")
            print(f"Preserved all {len(playback_baseline)} raw/export files from before playback-event controls.", flush=True)

    def nextup_long_prepare(self) -> None:
        baseline_file = PRIVATE / "nextup-long-baseline-hashes.json"
        target = Path("/opt/goby-fixtures/nextup-long-reference")
        if baseline_file.exists() or target.exists():
            raise RuntimeError("Long NextUp preparation already exists")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        source = Path("/opt/goby-fixtures/playback-reference/Reference Playback M3.mp4")
        source_hash = hashlib.sha256(source.read_bytes()).hexdigest()
        if source_hash != "997af268405a91e01685afa52d70a892c767e0e7135d25f6a33087cfb72de1c3":
            raise RuntimeError("The existing synthetic playback source changed")
        target.mkdir(mode=0o755)
        target.chmod(0o755)
        marker = target / ".goby-managed"
        marker.write_text("goby-nextup-long-reference-owned-v1\n")
        marker.chmod(0o644)
        files = []
        for season, episode in ((1, 1), (1, 2), (2, 1)):
            folder = target / "NextUp Long Reference" / f"Season {season:02d}"
            folder.mkdir(mode=0o755, parents=True, exist_ok=True)
            folder.chmod(0o755)
            folder.parent.chmod(0o755)
            destination = folder / f"NextUp Long Reference S{season:02d}E{episode:02d}.mp4"
            shutil.copyfile(source, destination)
            destination.chmod(0o644)
            digest = hashlib.sha256(destination.read_bytes()).hexdigest()
            if digest != source_hash:
                raise RuntimeError("Copied NextUp source differs from its original")
            files.append({"path": str(destination), "bytes": destination.stat().st_size, "sha256": digest})
        private_write(PRIVATE / "nextup-long-source-provenance.json", json.dumps(files, indent=2) + "\n")
        print(f"Created three owned 600-second episode copies totaling {sum(item['bytes'] for item in files)} bytes; original source unchanged.", flush=True)

    def nextup_long_setup(self) -> None:
        old = json.loads((PRIVATE / "raw" / "library-query-after-scan.json").read_text())["response"]["body"]["Items"]
        old_tv = next(item for item in old if item["CollectionType"] == "tvshows")
        options = json.loads(json.dumps(old_tv["LibraryOptions"]))
        target = "/opt/goby-fixtures/nextup-long-reference"
        options["PathInfos"] = [{"Path": target}]
        self.request("nextup-long-library-create", "POST", "/emby/Library/VirtualFolders", authenticated=True,
                     body={"Name": "Reference NextUp Long", "CollectionType": "tvshows", "RefreshLibrary": False, "Paths": [target], "LibraryOptions": options},
                     note="New dedicated TV library using the old test TV library's options, with only paths changed; providers remain disabled.")
        _, result = self.request("nextup-long-library-query", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        library = next(item for item in result["Items"] if item["Name"] == "Reference NextUp Long")
        private_write(PRIVATE / "nextup-long-library.json", json.dumps(library, indent=2) + "\n")
        self.request("nextup-long-library-refresh", "POST", f"/emby/Items/{library['ItemId']}/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=FullRefresh",
                     body={}, authenticated=True, note="Targeted refresh of the new TV library only.")

    def nextup_long_capture(self) -> None:
        self.use_session_m3b_credentials()
        time.sleep(2)
        user_id = self.credentials["REFERENCE_USER_ID"]
        library = json.loads((PRIVATE / "nextup-long-library.json").read_text())
        parent_id = library["ItemId"]
        _, result = self.request("nextup-long-items", "GET", f"/emby/Users/{user_id}/Items?ParentId={parent_id}&Recursive=true&IncludeItemTypes=Series,Season,Episode&Fields=Path,MediaSources,MediaStreams", authenticated=True)
        series_id = next(item["Id"] for item in result["Items"] if item["Type"] == "Series")
        episodes = sorted((item for item in result["Items"] if item["Type"] == "Episode"), key=lambda item: (item["ParentIndexNumber"], item["IndexNumber"]))
        if len(episodes) != 3 or any(item.get("RunTimeTicks") != 6000000000 for item in episodes):
            raise RuntimeError("The dedicated long-series episode set is not ready")
        first_id, middle_id = episodes[0]["Id"], episodes[1]["Id"]
        base = "/emby/Shows/NextUp?UserId=" + user_id
        state_prefix = f"/emby/Users/{user_id}/PlayedItems/"
        try:
            self.request("nextup-long-unstarted-default", "GET", base, authenticated=True)
            self.request("nextup-long-unstarted-series", "GET", base + "&SeriesId=" + series_id, authenticated=True)
            self.request("nextup-long-first-played", "POST", state_prefix + first_id, authenticated=True)
            time.sleep(2)
            _, global_result = self.request("nextup-long-after-first-default", "GET", base, authenticated=True)
            self.request("nextup-long-after-first-parent", "GET", base + "&ParentId=" + parent_id, authenticated=True)
            self.request("nextup-long-after-first-series", "GET", base + "&SeriesId=" + series_id, authenticated=True)
            self.request("nextup-long-series-detail", "GET", f"/emby/Users/{user_id}/Items/{series_id}", authenticated=True)
            if any(item.get("SeriesId") == series_id for item in global_result.get("Items", [])):
                self.request("nextup-long-first-unplayed", "DELETE", state_prefix + first_id, authenticated=True)
                self.request("nextup-long-middle-played", "POST", state_prefix + middle_id, authenticated=True)
                time.sleep(2)
                self.request("nextup-long-after-middle-default", "GET", base, authenticated=True)
                self.request("nextup-long-after-middle-series", "GET", base + "&SeriesId=" + series_id, authenticated=True)
        finally:
            self.request("nextup-long-series-restored", "DELETE", state_prefix + series_id, authenticated=True,
                         note="Restores only the new long-series watch state for the dedicated account; the older series and users are not changed.")

    def export_nextup_long(self) -> None:
        self.export(prefix="nextup-long-")
        if len(list((PRIVATE / "raw").glob("nextup-long-*.json"))) > 16:
            raise RuntimeError("Long NextUp capture exceeds the authorized bound")
        baseline = json.loads((PRIVATE / "nextup-long-baseline-hashes.json").read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("Existing evidence changed during the long-duration control")
        print(f"Preserved all {len(baseline)} pre-existing raw/export files byte-for-byte.", flush=True)

    def nextup_capability_begin(self) -> None:
        baseline_file = PRIVATE / "nextup-capability-baseline-hashes.json"
        if baseline_file.exists():
            raise RuntimeError("NextUp capability baseline already exists")
        baseline = {}
        for directory in (PRIVATE / "raw", EXPORT):
            for path in sorted(directory.glob("*.json")):
                baseline[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()
        private_write(baseline_file, json.dumps(baseline, indent=2) + "\n")
        print(f"Recorded {len(baseline)} original raw/export hashes before the capability control.", flush=True)

    def nextup_capability(self) -> None:
        self.use_session_m3b_credentials()
        user_id = self.credentials["REFERENCE_USER_ID"]
        session_id = self.credentials["REFERENCE_SESSION_ID"]
        items = json.loads((PRIVATE / "raw" / "nextup-long-items.json").read_text())["response"]["body"]["Items"]
        series_id = next(item["Id"] for item in items if item["Type"] == "Series")
        first_id = min((item for item in items if item["Type"] == "Episode"), key=lambda item: (item["ParentIndexNumber"], item["IndexNumber"]))["Id"]
        parent_id = json.loads((PRIVATE / "nextup-long-library.json").read_text())["ItemId"]
        caps = "/emby/Sessions/Capabilities?Id=" + session_id
        state_prefix = f"/emby/Users/{user_id}/PlayedItems/"
        base = "/emby/Shows/NextUp?UserId=" + user_id
        try:
            self.request("nextup-capability-video-declaration", "POST", caps + "&PlayableMediaTypes=Video,Audio&SupportedCommands=&SupportsMediaControl=true&SupportsSync=false",
                         authenticated=True, note="Simple capability update only; no DeviceProfile property is supplied.")
            self.request("nextup-capability-session-confirmed", "GET", "/emby/Sessions?Id=" + session_id, authenticated=True)
            self.request("nextup-capability-first-played", "POST", state_prefix + first_id, authenticated=True)
            time.sleep(2)
            self.request("nextup-capability-global", "GET", base, authenticated=True)
            self.request("nextup-capability-parent", "GET", base + "&ParentId=" + parent_id, authenticated=True)
            self.request("nextup-capability-series", "GET", base + "&SeriesId=" + series_id, authenticated=True)
        finally:
            self.request("nextup-capability-series-restored", "DELETE", state_prefix + series_id, authenticated=True,
                         note="Restores the dedicated user's long series to unplayed.")
            self.request("nextup-capability-audio-restored", "POST", caps + "&PlayableMediaTypes=Audio&SupportedCommands=VolumeDown&SupportsMediaControl=true&SupportsSync=false",
                         authenticated=True, note="Restores the prior Audio/VolumeDown capability declaration without supplying a DeviceProfile.")

    def export_nextup_capability(self) -> None:
        self.export(prefix="nextup-capability-")
        if len(list((PRIVATE / "raw").glob("nextup-capability-*.json"))) > 8:
            raise RuntimeError("NextUp capability capture exceeds the authorized bound")
        baseline = json.loads((PRIVATE / "nextup-capability-baseline-hashes.json").read_text())
        for name, expected in baseline.items():
            if hashlib.sha256(Path(name).read_bytes()).hexdigest() != expected:
                raise RuntimeError("Existing evidence changed during the capability control")
        print(f"Preserved all {len(baseline)} pre-existing raw/export files byte-for-byte.", flush=True)

    def public(self, prefix="public") -> None:
        self.request(f"{prefix}-system-info", "GET", "/emby/System/Info/Public", headers={"Accept": "application/json"})
        self.request(f"{prefix}-users", "GET", "/emby/Users/Public", headers={"Accept": "application/json"})
        for method in ("HEAD", "GET", "POST"):
            self.request(f"{prefix}-ping-{method.lower()}", method, "/emby/System/Ping", headers={})

    def setup(self) -> None:
        if self.credentials.get("REFERENCE_TOKEN"):
            raise RuntimeError("Reference setup has already produced credentials")
        self.public("before-setup")
        self.request("setup-user-get", "GET", "/emby/Startup/User")
        self.credentials = {"REFERENCE_USERNAME": "reference-admin", "REFERENCE_PASSWORD": secrets.token_hex(32)}
        save_credentials(self.credentials)
        self.secret_values.add(self.credentials["REFERENCE_PASSWORD"])
        status_code, _ = self.request(
            "setup-user-post", "POST", "/emby/Startup/User",
            body=urllib.parse.urlencode({"Name": self.credentials["REFERENCE_USERNAME"], "Password": self.credentials["REFERENCE_PASSWORD"]}),
            headers={**BASE_HEADERS, "Content-Type": "application/x-www-form-urlencoded"},
            note="Normal setup wizard request; password is redacted from the exported fixture.",
        )
        if status_code != 200:
            raise RuntimeError("Setup user request failed")
        self.request(
            "setup-remote-access", "POST", "/emby/Startup/RemoteAccess", body="EnableAutomaticPortMapping=false",
            headers={**BASE_HEADERS, "Content-Type": "application/x-www-form-urlencoded"},
        )
        self.request("setup-complete", "POST", "/emby/Startup/Complete")
        self.login()
        self.public("after-setup")

    def login(self) -> None:
        status_code, _ = self.request(
            "auth-success-authorization", "POST", "/emby/Users/AuthenticateByName",
            body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]},
            note="Correct password with the Authorization header; secrets are redacted.",
        )
        if status_code != 200:
            raise RuntimeError("Reference administrator authentication failed")

    def authentication(self) -> None:
        self.request("auth-unknown-user", "POST", "/emby/Users/AuthenticateByName", body={"Username": "reference-user-does-not-exist", "Pw": "invalid-reference-password"})
        self.request("auth-wrong-password", "POST", "/emby/Users/AuthenticateByName", body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": "invalid-reference-password"})
        self.request("auth-missing-authorization", "POST", "/emby/Users/AuthenticateByName", body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]}, headers={"Accept": "application/json"})
        self.request("auth-success-x-emby-authorization", "POST", "/emby/Users/AuthenticateByName", body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]}, headers={"Accept": "application/json", "X-Emby-Authorization": BASE_HEADERS["Authorization"]})
        self.request("auth-malformed-json", "POST", "/emby/Users/AuthenticateByName", body="{", headers={**BASE_HEADERS, "Content-Type": "application/json"})
        self.request("auth-protected-without-token", "GET", "/emby/Users", headers={"Accept": "application/json"})

    def auth_carriers(self) -> None:
        body = {"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]}
        self.request("auth-legacy-mediabrowser-scheme", "POST", "/emby/Users/AuthenticateByName", body=body, headers={
            "Accept": "application/json", "Authorization": BASE_HEADERS["Authorization"].replace("Emby ", "MediaBrowser ", 1),
        })
        self.request("auth-four-separate-headers", "POST", "/emby/Users/AuthenticateByName", body=body, headers={
            "Accept": "application/json", "X-Emby-Client": "Goby Reference Recorder",
            "X-Emby-Client-Version": "0.1.0", "X-Emby-Device-Id": "goby-reference-recorder",
            "X-Emby-Device-Name": "Linux Test",
        })
        self.request("auth-client-without-device-id", "POST", "/emby/Users/AuthenticateByName", body=body, headers={
            "Accept": "application/json", "Authorization": 'Emby Client="Goby Reference Recorder", Device="Linux Test", Version="0.1.0"',
        })

    def final_two(self) -> None:
        user_id = self.credentials["REFERENCE_USER_ID"]
        self.request("auth-x-mediabrowser-token", "GET", f"/emby/Users/{user_id}", headers={
            "Accept": "application/json", "X-MediaBrowser-Token": self.credentials["REFERENCE_TOKEN"],
        })
        self.request("cors-options-items", "OPTIONS", "/emby/Items", headers={
            "Origin": "https://example.invalid", "Access-Control-Request-Method": "GET",
            "Access-Control-Request-Headers": "X-Emby-Token,Content-Type",
        })

    def library(self) -> None:
        _, existing = self.request("library-query-before", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        if isinstance(existing, dict) and existing.get("Items"):
            raise RuntimeError("Libraries already exist; refusing to duplicate them")
        for name, content_type, source in (
            ("Reference Movies", "movies", "/opt/goby-fixtures/movies"),
            ("Reference Shows", "tvshows", "/opt/goby-fixtures/tv"),
            ("Reference Music", "music", "/opt/goby-fixtures/music"),
        ):
            types = ("Movie", "Series", "Season", "Episode", "MusicArtist", "MusicAlbum", "Audio")
            options = {
                "EnableRealtimeMonitor": False,
                "EnableChapterImageExtraction": False,
                "ExtractChapterImagesDuringLibraryScan": False,
                "EnableMarkerDetection": False,
                "EnableMarkerDetectionDuringLibraryScan": False,
                "DownloadImagesInAdvance": False,
                "SaveLocalMetadata": False,
                "SaveLocalThumbnailSets": False,
                "SaveSubtitlesWithMedia": False,
                "SaveLyricsWithMedia": False,
                "SubtitleDownloadLanguages": [],
                "LyricsDownloadLanguages": [],
                "AutomaticRefreshIntervalDays": 0,
                "EnableEmbeddedTitles": True,
                "TypeOptions": [{"Type": media_type, "MetadataFetchers": [], "ImageFetchers": []} for media_type in types],
            }
            status_code, _ = self.request(
                f"library-add-{content_type}", "POST", "/emby/Library/VirtualFolders", authenticated=True,
                body={"Name": name, "CollectionType": content_type, "RefreshLibrary": False, "Paths": [source], "LibraryOptions": options},
                note="Provider arrays are explicitly empty; PrivateNetwork independently prevents all external traffic. Media paths are read-only in the service mount namespace.",
            )
            if status_code not in {200, 204}:
                raise RuntimeError(f"Failed to add reference library: {content_type}")
        self.request("library-query-after-add", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        self.request("library-refresh", "POST", "/emby/Library/Refresh", authenticated=True)

    def queries(self) -> None:
        user_id = self.credentials["REFERENCE_USER_ID"]
        self.request("library-query-after-scan", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        self.request("users-views", "GET", f"/emby/Users/{user_id}/Views", authenticated=True)
        _, all_items = self.request("items-recursive-full-request", "GET", f"/emby/Users/{user_id}/Items?Recursive=true&Fields=Path,MediaSources,MediaStreams,Overview,ProviderIds,Genres,People,DateCreated&EnableImages=true", authenticated=True)
        items = all_items.get("Items", []) if isinstance(all_items, dict) else []
        private_write(PRIVATE / "items.json", json.dumps(items, indent=2) + "\n")
        self.request("items-recursive-default-fields", "GET", f"/emby/Users/{user_id}/Items?Recursive=true", authenticated=True)
        self.request("items-recursive-path-only", "GET", f"/emby/Users/{user_id}/Items?Recursive=true&Fields=Path", authenticated=True)
        self.request("items-recursive-projection-off", "GET", f"/emby/Users/{user_id}/Items?Recursive=true&Fields=Path&EnableImages=false&EnableUserData=false", authenticated=True)
        self.request("items-query-global", "GET", f"/emby/Items?UserId={user_id}&Recursive=true&Fields=Path", authenticated=True)
        for media_type in ("Movie", "Episode", "Series", "Season", "Audio", "MusicAlbum", "MusicArtist"):
            self.request(f"items-type-{media_type.lower()}", "GET", f"/emby/Users/{user_id}/Items?Recursive=true&IncludeItemTypes={media_type}", authenticated=True)
        self.request("latest-default-group", "GET", f"/emby/Users/{user_id}/Items/Latest?Limit=20", authenticated=True)
        self.request("latest-group-false", "GET", f"/emby/Users/{user_id}/Items/Latest?Limit=20&GroupItems=false", authenticated=True)
        self.request("latest-episodes-default-group", "GET", f"/emby/Users/{user_id}/Items/Latest?Limit=20&IncludeItemTypes=Episode", authenticated=True)
        self.request("latest-episodes-group-false", "GET", f"/emby/Users/{user_id}/Items/Latest?Limit=20&IncludeItemTypes=Episode&GroupItems=false", authenticated=True)
        series = next((item for item in items if item.get("Type") == "Series"), None)
        if series:
            series_id = series["Id"]
            self.request("shows-seasons", "GET", f"/emby/Shows/{series_id}/Seasons?UserId={user_id}", authenticated=True)
            self.request("shows-episodes", "GET", f"/emby/Shows/{series_id}/Episodes?UserId={user_id}", authenticated=True)
            self.request("shows-episodes-season-one", "GET", f"/emby/Shows/{series_id}/Episodes?UserId={user_id}&Season=1", authenticated=True)
        self.request("shows-nextup", "GET", f"/emby/Shows/NextUp?UserId={user_id}&Limit=10", authenticated=True)

    def sample_filter(self) -> None:
        original = json.loads((PRIVATE / "raw" / "library-query-after-scan.json").read_text())
        library = next(item for item in original["response"]["body"]["Items"] if item["CollectionType"] == "movies")
        options = library["LibraryOptions"]
        options["SampleIgnoreSize"] = 0
        self.request(
            "library-options-disable-sample-size-filter", "POST", "/emby/Library/VirtualFolders/LibraryOptions",
            body={"Id": library["ItemId"], "LibraryOptions": options}, authenticated=True,
            note="Only the dedicated reference movies library changes. The source media files remain read-only and unchanged.",
        )
        self.request("library-refresh-after-sample-filter", "POST", "/emby/Library/Refresh", authenticated=True)

    def playback(self) -> None:
        user_id = self.credentials["REFERENCE_USER_ID"]
        items = json.loads((PRIVATE / "items.json").read_text())
        _, current_movies = self.request("items-movies-after-sample-filter", "GET", f"/emby/Users/{user_id}/Items?Recursive=true&IncludeItemTypes=Movie", authenticated=True)
        candidates = current_movies.get("Items", []) if isinstance(current_movies, dict) else []
        selected = candidates[0] if candidates else next(item for item in items if item.get("Type") == "Episode")
        item_id = selected["Id"]
        self.request("item-detail-default", "GET", f"/emby/Users/{user_id}/Items/{item_id}", authenticated=True)
        self.request("item-detail-path-field", "GET", f"/emby/Users/{user_id}/Items/{item_id}?Fields=Path", authenticated=True)
        self.request("playback-info-get", "GET", f"/emby/Items/{item_id}/PlaybackInfo?UserId={user_id}", authenticated=True)
        self.request("playback-info-post-minimal", "POST", f"/emby/Items/{item_id}/PlaybackInfo", body={"UserId": user_id}, authenticated=True)
        profile = {
            "Name": "Reference HLS profile",
            "MaxStreamingBitrate": 2000000,
            "DirectPlayProfiles": [],
            "TranscodingProfiles": [{"Container": "ts", "Type": "Video", "VideoCodec": "h264", "AudioCodec": "aac", "Protocol": "hls", "Context": "Streaming", "MaxAudioChannels": "2", "MinSegments": 1, "SegmentLength": 3}],
            "SubtitleProfiles": [{"Format": "vtt", "Method": "External"}],
        }
        self.request(
            "playback-info-post-hls-profile", "POST", f"/emby/Items/{item_id}/PlaybackInfo", authenticated=True,
            body={"UserId": user_id, "IsPlayback": True, "EnableDirectPlay": False, "EnableDirectStream": False, "EnableTranscoding": True, "MaxStreamingBitrate": 2000000, "DeviceProfile": profile},
            note="Negotiation only. No Premiere entitlement or paid feature was bypassed, and no returned media URL was automatically played.",
        )

    def export(self, prefix=None) -> None:
        records = [(path.stem, json.loads(path.read_text())) for path in sorted((PRIVATE / "raw").glob("*.json"))]
        for _, record in records:
            for header_name in ("X-Emby-Token", "X-MediaBrowser-Token"):
                token = record["request"]["headers"].get(header_name)
                if token:
                    self.secret_values.add(token)
            result = record["response"]["body"]
            if isinstance(result, dict) and result.get("AccessToken"):
                self.secret_values.add(result["AccessToken"])
        selected = [(name, record) for name, record in records if prefix is None or name.startswith(prefix)]
        for name, record in selected:
            sanitized = self.sanitize(record)
            self.audit_export(record, sanitized)
            for original_header, exported_header in zip(record["response"]["headers"], sanitized["response"]["headers"]):
                if original_header != exported_header:
                    raise RuntimeError(f"Export changed a response header in {name}")
            serialized = json.dumps(sanitized, indent=2, ensure_ascii=False) + "\n"
            if any(secret in serialized for secret in self.secret_values):
                raise RuntimeError(f"Secret redaction failed for {name}")
            if sanitized["response"]["bodyType"] == "binary-base64":
                binary = base64.b64decode(sanitized["response"]["body"], validate=True)
                if any(secret.encode() in binary for secret in self.secret_values):
                    raise RuntimeError(f"Binary body contains a credential in {name}")
            private_write(EXPORT / f"{name}.json", serialized)
        print(f"Exported {len(selected)} fixtures; all response headers are exact, JSON structure/types/numbers are preserved, and all recorded credentials are absent. Synthetic reference IDs remain unchanged.", flush=True)

    def hls(self) -> None:
        original = json.loads((PRIVATE / "raw" / "playback-info-post-hls-profile.json").read_text())
        negotiation = original["response"]["body"]
        url = negotiation["MediaSources"][0].get("TranscodingUrl")
        if not url:
            raise RuntimeError("Reference did not offer a transcoding URL")
        if not url.startswith("/") or url.startswith("//"):
            raise RuntimeError("Refusing a non-local reference URL")
        path = "/emby" + url
        try:
            status_code, manifest = self.request(
                "hls-master", "GET", path, headers={},
                note="Requested the exact server-generated URL using its query token. This is a bounded ordinary playback request; an entitlement denial is not bypassed.",
            )
            if status_code == 200 and isinstance(manifest, str) and manifest.startswith("#EXTM3U"):
                child = next((line.strip() for line in manifest.splitlines() if line.strip() and not line.startswith("#")), None)
                if child:
                    target = urllib.parse.urlsplit(urllib.parse.urljoin("http://127.0.0.1:18097" + path, child))
                    if target.hostname != "127.0.0.1" or target.port != 18097:
                        raise RuntimeError("Refusing a manifest URL outside the isolated reference origin")
                    child_path = target.path + ("?" + target.query if target.query else "")
                    self.request("hls-media", "GET", child_path, headers={})
        finally:
            query = urllib.parse.urlencode({"DeviceId": "goby-reference-recorder", "PlaySessionId": negotiation["PlaySessionId"]})
            self.request("hls-cleanup", "DELETE", "/emby/Videos/ActiveEncodings?" + query, authenticated=True)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("stage", choices=["public", "setup", "authentication", "auth_carriers", "final_two", "library", "queries", "sample_filter", "playback", "hls", "export", "artwork_prepare", "artwork_refresh", "artwork", "artwork_scalars", "export_artwork", "entities_prepare", "entity_lists", "entity_navigation", "export_entities", "playback_m3_begin", "playback_m3_flags", "playback_m3_rejection", "playback_m3_factorial", "playback_m3_prepare", "playback_m3_setup", "playback_m3_long_info", "playback_m3_transport", "playback_m3_reports", "export_playback_m3", "folder_state_begin", "folder_state", "export_folder_state", "session_m3b_begin", "session_m3b_setup", "session_m3b_lists", "session_m3b_caps", "session_m3b_ping", "nextup_m3b", "nextup_m3b_split_begin", "nextup_m3b_split", "nextup_m3b_playback_begin", "nextup_m3b_playback", "export_session_m3b", "nextup_long_prepare", "nextup_long_setup", "nextup_long_capture", "export_nextup_long", "nextup_capability_begin", "nextup_capability", "export_nextup_capability"])
    options = parser.parse_args()
    recorder = Recorder()
    getattr(recorder, options.stage)()


if __name__ == "__main__":
    main()
