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
import urllib.parse
import zlib


DATA = Path("/opt/goby-test/emby-reference-data")
PRIVATE = DATA / "private"
EXPORT = DATA / "export"
ENV_FILE = PRIVATE / "credentials.env"
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


def read_credentials() -> dict[str, str]:
    if not ENV_FILE.exists():
        return {}
    if stat.S_IMODE(ENV_FILE.stat().st_mode) != 0o600:
        raise RuntimeError("Credential file must have mode 0600")
    return dict(line.split("=", 1) for line in ENV_FILE.read_text().splitlines() if line)


def save_credentials(values: dict[str, str]) -> None:
    private_write(ENV_FILE, "".join(f"{key}={value}\n" for key, value in values.items()))


class Recorder:
    def __init__(self) -> None:
        os.umask(0o077)
        if (DATA / ".goby-managed").read_text().strip() != "goby-emby-reference-owned-v1":
            raise RuntimeError("Reference ownership marker does not match")
        self.credentials = read_credentials()
        self.secret_values = {
            value for key, value in self.credentials.items()
            if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value
        }

    def clean_text(self, value: str) -> str:
        for secret in self.secret_values:
            value = value.replace(secret, "[REDACTED_SECRET]")
        value = re.sub(r"(?i)(api_key=)[^&\s\"]+", r"\1[REDACTED_TOKEN]", value)
        return value.replace(str(DATA), "/reference-data").replace(
            "/dev/shm/goby-emby-reference/package", "/reference-package"
        )

    def sanitize(self, value, key=""):
        if key.lower() in {"password", "pw", "accesstoken", "token", "x-emby-token", "x-mediabrowser-token"} and isinstance(value, str) and value:
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
            allowed_sensitive_field = key.lower() in {"password", "pw", "accesstoken", "token", "x-emby-token", "x-mediabrowser-token"}
            allowed_secret = any(secret in original for secret in self.secret_values)
            allowed_path = str(DATA) in original or "/dev/shm/goby-emby-reference/package" in original
            allowed_query = bool(re.search(r"(?i)api_key=", original))
            if original != exported and not any((allowed_sensitive_field, allowed_secret, allowed_path, allowed_query)):
                raise RuntimeError(f"Export changed a non-sensitive string field: {key}")
        elif type(original) is not type(exported) or original != exported:
            raise RuntimeError("Export changed a number, boolean, or null")

    def request(self, name, method, path, *, body=None, headers=None, authenticated=False, note=None, binary_image=False):
        if name.startswith("artwork-") and ((PRIVATE / "raw" / f"{name}.json").exists() or (EXPORT / f"{name}.json").exists()):
            raise RuntimeError(f"Refusing to overwrite artwork evidence: {name}")
        request_headers = dict(BASE_HEADERS if headers is None else headers)
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
            save_credentials(self.credentials)
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
    parser.add_argument("stage", choices=["public", "setup", "authentication", "auth_carriers", "final_two", "library", "queries", "sample_filter", "playback", "hls", "export", "artwork_prepare", "artwork_refresh", "artwork", "artwork_scalars", "export_artwork"])
    options = parser.parse_args()
    recorder = Recorder()
    getattr(recorder, options.stage)()


if __name__ == "__main__":
    main()
