#!/usr/bin/env python3
"""Record sanitized HTTP contracts from the isolated official reference server.

Run only on test-env, inside the reference service's network namespace.
Private raw captures and credentials remain in a root-only directory.
"""

from __future__ import annotations

import argparse
import datetime as dt
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import stat
import urllib.parse


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

    def request(self, name, method, path, *, body=None, headers=None, authenticated=False, note=None):
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

    def export(self) -> None:
        records = [(path.stem, json.loads(path.read_text())) for path in sorted((PRIVATE / "raw").glob("*.json"))]
        for _, record in records:
            for header_name in ("X-Emby-Token", "X-MediaBrowser-Token"):
                token = record["request"]["headers"].get(header_name)
                if token:
                    self.secret_values.add(token)
            result = record["response"]["body"]
            if isinstance(result, dict) and result.get("AccessToken"):
                self.secret_values.add(result["AccessToken"])
        for name, record in records:
            sanitized = self.sanitize(record)
            self.audit_export(record, sanitized)
            for original_header, exported_header in zip(record["response"]["headers"], sanitized["response"]["headers"]):
                if original_header != exported_header:
                    raise RuntimeError(f"Export changed a response header in {name}")
            serialized = json.dumps(sanitized, indent=2, ensure_ascii=False) + "\n"
            if any(secret in serialized for secret in self.secret_values):
                raise RuntimeError(f"Secret redaction failed for {name}")
            private_write(EXPORT / f"{name}.json", serialized)
        print(f"Exported {len(records)} fixtures; all response headers are exact, JSON structure/types/numbers are preserved, and all recorded credentials are absent. Synthetic reference IDs remain unchanged.", flush=True)

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
    parser.add_argument("stage", choices=["public", "setup", "authentication", "auth_carriers", "final_two", "library", "queries", "sample_filter", "playback", "hls", "export"])
    options = parser.parse_args()
    recorder = Recorder()
    getattr(recorder, options.stage)()


if __name__ == "__main__":
    main()
