#!/usr/bin/env python3
"""Run the isolated NFO smoke check only on the authorized Linux test host.

Uses the existing root-only synthetic credentials and the real Goby APIs. It
creates one marked directory below /opt/goby-fixtures, never touches reference
sidecars, and removes only its own catalog entry and explicitly owned files.
Stable API probe fields do not prove that ffprobe was not invoked again; the
deterministic Go tests cover invocation counts.
"""

from __future__ import annotations

import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from urllib.parse import quote, urlencode
import xml.etree.ElementTree as ET


ENV_FILE = Path("/opt/goby-test/browser.env")
FIXTURE_ROOT = Path("/opt/goby-fixtures")
ORIGIN = "http://127.0.0.1:18096"
SERVICE = "goby-foundation-test.service"
MEDIA_NAME = "NFO Smoke Media.mp4"
NFO_NAME = "NFO Smoke Media.nfo"
MARKER_NAME = ".goby-nfo-smoke-owned.json"
INITIAL = {
    "Name": "NFO smoke initial title", "ProductionYear": 1998,
    "Overview": "Initial synthetic local metadata.",
    "Genres": ["Adventure", "Drama"],
    "ProviderIds": {"Imdb": "tt9000001", "Tmdb": "9000001"},
}
UPDATED = {
    "Name": "NFO smoke revised title", "ProductionYear": 2003,
    "Overview": "Revised synthetic metadata with a different description.",
    "Genres": ["Adventure", "Documentary"],
    "ProviderIds": {"Imdb": "tt9000002", "Tmdb": "9000002"},
}


class SmokeFailure(Exception):
    """A safe assertion label that never contains credentials or API bodies."""


def check(condition: bool, label: str) -> None:
    if not condition:
        raise SmokeFailure(label)


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(131072), b""):
            result.update(chunk)
    return result.hexdigest()


def credentials() -> dict[str, str]:
    info = ENV_FILE.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o077 == 0,
          "Synthetic credential file must be a root-owned, private regular file")
    wanted = {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD", "GOBY_SMOKE_MEDIA_FILE"}
    values = {}
    for line in ENV_FILE.read_text(encoding="utf-8").splitlines():
        line = line.strip().removeprefix("export ")
        key, separator, raw = line.partition("=")
        if not separator or key not in wanted:
            continue
        parts = shlex.split(raw, comments=False, posix=True)
        check(len(parts) == 1, "Synthetic credential file has an unsupported assignment")
        values[key] = parts[0]
    check(all(values.get(key) for key in wanted), "Required synthetic fixture settings are missing")
    return values


class API:
    def __init__(self) -> None:
        self.cookie = ""
        self.csrf = ""
        self.token = ""
        self.user_id = ""

    def request(self, method: str, path: str, *, body=None, admin=False,
                emby=False, expected=(200,), parse=True):
        headers = {"Accept": "application/json", "Origin": ORIGIN}
        if admin:
            if self.cookie:
                headers["Cookie"] = self.cookie
            if method not in {"GET", "HEAD"} and self.csrf:
                headers["X-CSRF-Token"] = self.csrf
        if emby:
            headers["Authorization"] = (
                'Emby Client="Goby NFO Smoke", DeviceId="goby-nfo-smoke", '
                'Device="Linux Test", Version="0.1.0"'
            )
            if self.token:
                headers["X-Emby-Token"] = self.token
        wire = None if body is None else json.dumps(body).encode("utf-8")
        if wire is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=15)
        try:
            connection.request(method, path, wire, headers)
            response = connection.getresponse()
            status_code = response.status
            content = response.read(2 * 1024 * 1024 + 1)
            check(len(content) <= 2 * 1024 * 1024, "API response exceeded the smoke-check limit")
            check(status_code in expected, f"{method} {path.split('?')[0]} returned HTTP {status_code}")
            if admin and method == "POST" and path == "/admin/v1/session":
                cookies = SimpleCookie()
                cookies.load(response.getheader("Set-Cookie", ""))
                check("goby_session" in cookies, "Administrator login did not return a session cookie")
                self.cookie = "goby_session=" + cookies["goby_session"].value
            return json.loads(content) if parse and content else None
        except (OSError, http.client.HTTPException, ValueError) as error:
            raise SmokeFailure(f"{method} {path.split('?')[0]} failed ({type(error).__name__})") from None
        finally:
            connection.close()

    def login(self, values: dict[str, str]) -> None:
        session = self.request("POST", "/admin/v1/session", admin=True, body={
            "Name": values["GOBY_SMOKE_NAME"], "Password": values["GOBY_SMOKE_PASSWORD"],
        })
        self.csrf = session.get("CSRFToken", "")
        check(bool(self.csrf), "Administrator session did not contain a CSRF token")
        session = self.request("POST", "/emby/Users/AuthenticateByName", emby=True, body={
            "Username": values["GOBY_SMOKE_NAME"], "Pw": values["GOBY_SMOKE_PASSWORD"],
        })
        self.token = session.get("AccessToken", "")
        self.user_id = session.get("User", {}).get("Id", "")
        check(bool(self.token and self.user_id), "Emby authentication did not return a complete session")

    def wait_job(self, job_id: str, timeout=30):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            jobs = self.request("GET", "/admin/v1/jobs", admin=True)["Items"]
            found = next((job for job in jobs if job["Id"] == job_id), None)
            check(found is not None, "Created scan job was not returned by the server")
            if found["Status"] not in {"pending", "running"}:
                return found
            time.sleep(0.25)
        raise SmokeFailure("Scan did not finish within the bounded smoke-check deadline")


class OwnedFixture:
    def __init__(self, source: Path, source_hash: str, *, prefix="nfo-smoke-",
                 marker_name=MARKER_NAME, extra_files=()) -> None:
        self.source = source
        self.directory: Path | None = None
        check(prefix in {"nfo-smoke-", "artwork-entities-smoke-"}, "Unsupported fixture ownership prefix")
        check(all(Path(name).name == name and name not in {"", ".", ".."} for name in (marker_name, *extra_files)),
              "Fixture file names must be simple basenames")
        self.prefix, self.marker_name, self.extra_files = prefix, marker_name, tuple(extra_files)
        self.marker = json.dumps({"owner": "goby-" + prefix.rstrip("-") + "-v1", "nonce": secrets.token_hex(16),
                                  "source_sha256": source_hash}, sort_keys=True)

    def prepare(self) -> None:
        self.directory = Path(tempfile.mkdtemp(prefix=self.prefix, dir=FIXTURE_ROOT))
        self.directory.chmod(0o755)
        (self.directory / self.marker_name).write_text(self.marker, encoding="utf-8")
        shutil.copyfile(self.source, self.directory / MEDIA_NAME)
        (self.directory / MEDIA_NAME).chmod(0o644)

    def owned(self) -> Path:
        check(self.directory is not None, "Owned fixture was not created")
        directory = self.directory
        check(not directory.is_symlink() and directory.resolve(strict=True).parent == FIXTURE_ROOT,
              "Owned fixture resolved outside its allowed parent")
        check(directory.name.startswith(self.prefix), "Owned fixture directory name did not match")
        marker = directory / self.marker_name
        check(stat.S_ISREG(marker.lstat().st_mode) and marker.read_text(encoding="utf-8") == self.marker,
              "Owned fixture marker did not match; cleanup refused")
        return directory

    def nfo(self, values=None, malformed=False) -> None:
        path = self.owned() / NFO_NAME
        if malformed:
            content = "<movie><title>Invalid replacement that must not be applied"
        else:
            root = ET.Element("movie")
            for tag, field in (("title", "Name"), ("year", "ProductionYear"), ("plot", "Overview")):
                ET.SubElement(root, tag).text = str(values[field])
            for genre in values["Genres"]:
                ET.SubElement(root, "genre").text = genre
            for provider, value in values["ProviderIds"].items():
                ET.SubElement(root, "uniqueid", {"type": provider.lower()}).text = value
            content = ET.tostring(root, encoding="unicode")
        path.write_text(content, encoding="utf-8")
        path.chmod(0o644)

    def cleanup(self) -> None:
        directory = self.owned()
        allowed = {MEDIA_NAME, NFO_NAME, self.marker_name, *self.extra_files}
        entries = list(directory.iterdir())
        check(all(path.name in allowed and stat.S_ISREG(path.lstat().st_mode) for path in entries),
              "Unexpected files appeared in the owned fixture; cleanup refused")
        for name in (NFO_NAME, MEDIA_NAME, *self.extra_files, self.marker_name):
            path = directory / name
            if path.exists():
                path.unlink()
        directory.rmdir()


def runtime_uid() -> int:
    process = subprocess.run(["systemctl", "show", SERVICE, "--property=MainPID", "--value"],
                             check=True, text=True, capture_output=True, timeout=5)
    pid = int(process.stdout.strip())
    check(pid > 1, "The Goby service does not have an active process")
    status = Path(f"/proc/{pid}/status").read_text(encoding="utf-8")
    uid_line = next(line for line in status.splitlines() if line.startswith("Uid:"))
    uids = [int(value) for value in uid_line.split()[1:]]
    check(len(uids) == 4 and all(uid != 0 for uid in uids), "The running Goby process must not use root credentials")
    return uids[1]


def probe_view(item: dict) -> dict:
    sources = item.get("MediaSources") or []
    check(len(sources) == 1 and isinstance(sources[0], dict), "Movie detail did not return one probed media source")
    source = sources[0]
    return {
        "RunTimeTicks": item.get("RunTimeTicks"), "MediaStreams": item.get("MediaStreams"),
        "Chapters": item.get("Chapters"),
        "Source": {key: source.get(key) for key in (
            "Container", "Formats", "RunTimeTicks", "Bitrate", "Size", "Path", "MediaStreams",
        )},
    }


def main() -> int:
    os.umask(0o077)
    summary = {"status": "failed", "assertions": [], "cleanup_errors": []}
    api = API()
    fixture = None
    library_id = ""
    creation_attempted = False
    active_job = ""
    source = None
    source_hash = ""
    stage = "preconditions"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0, "Run this script as root only on Linux test-env")
        check(not FIXTURE_ROOT.is_symlink() and FIXTURE_ROOT.resolve(strict=True) == FIXTURE_ROOT,
              "The fixture root must be the expected real directory")
        check((FIXTURE_ROOT / ".goby-managed").read_text().strip() == "goby-generated-media-fixtures",
              "The shared synthetic fixture ownership marker did not match")
        values = credentials()
        source = Path(values["GOBY_SMOKE_MEDIA_FILE"]).resolve(strict=True)
        check(source.is_relative_to(FIXTURE_ROOT) and source.is_file() and source.suffix.lower() == ".mp4",
              "The configured source must be an MP4 inside the authorized fixture root")
        check(0 < source.stat().st_size <= 5 * 1024 * 1024, "The source must be a small media fixture")
        source_hash = digest(source)
        summary["runtime_uid"] = runtime_uid()
        api.request("GET", "/readyz", parse=False)
        api.login(values)
        fixture = OwnedFixture(source, source_hash)
        fixture.prepare()
        copied_media = fixture.owned() / MEDIA_NAME
        copied_stat = copied_media.stat()
        check(digest(copied_media) == source_hash, "The isolated media copy differs from its source")
        fixture.nfo(INITIAL)
        stage = "create isolated library"
        creation_attempted = True
        created = api.request("POST", "/admin/v1/libraries", admin=True, expected=(201,), body={
            "Name": fixture.directory.name, "CollectionType": "movies", "Paths": [str(fixture.directory)], "Scan": False,
        })
        library_id = created["Library"]["Id"]
        original_id = ""
        original_probe = None
        genre_ids = {}
        for stage, expected in (("valid NFO", INITIAL), ("NFO-only update", UPDATED),
                                ("malformed NFO retention", UPDATED), ("sidecar removal", None)):
            if stage == "NFO-only update":
                fixture.nfo(UPDATED)
            elif stage == "malformed NFO retention":
                fixture.nfo(malformed=True)
            elif stage == "sidecar removal":
                (fixture.owned() / NFO_NAME).unlink()
            job = api.request("POST", f"/admin/v1/libraries/{quote(library_id)}/scan", admin=True, expected=(202,))["Job"]
            active_job = job["Id"]
            job = api.wait_job(active_job)
            active_job = ""
            check(job["Status"] == "completed", stage + ": scan did not complete")
            check(job["Scanned"] == 1, stage + ": scan did not inspect exactly the isolated movie")
            expected_counts = {"valid NFO": (1, 0), "NFO-only update": (0, 1),
                               "malformed NFO retention": (0, 0), "sidecar removal": (0, 1)}[stage]
            check((job["Added"], job["Updated"]) == expected_counts, stage + ": catalog change counts did not match")
            check(bool(job.get("Error")) == (stage == "malformed NFO retention"), stage + ": unexpected job warning state")
            query = urlencode({"ParentId": library_id, "Recursive": "true", "IncludeItemTypes": "Movie",
                               "Fields": "Path,Overview,ProductionYear,Genres,ProviderIds", "Limit": 10})
            endpoint = f"/emby/Users/{quote(api.user_id)}/Items"
            listed = api.request("GET", endpoint + "?" + query, emby=True)["Items"]
            check(len(listed) == 1 and listed[0].get("Path") == str(copied_media), stage + ": isolated movie lookup failed")
            item = api.request("GET", endpoint + "/" + quote(listed[0]["Id"]), emby=True)
            if expected is not None:
                for key, value in expected.items():
                    check(item.get(key) == value and listed[0].get(key) == value, stage + ": " + key + " DTO mismatch")
                for projected in (item, listed[0]):
                    genre_items = projected.get("GenreItems", [])
                    check([genre.get("Name") for genre in genre_items] == expected["Genres"], stage + ": GenreItems names mismatch")
                    for genre in genre_items:
                        genre_id = genre.get("Id")
                        check(type(genre_id) is int and genre_id > 0, stage + ": embedded genre ID is not a positive number")
                        check(genre_ids.setdefault(genre["Name"], genre_id) == genre_id, stage + ": persistent genre ID changed")
                entity_query = urlencode({"UserId": api.user_id, "ParentId": library_id, "IncludeItemTypes": "Movie", "Limit": 100})
                entities = api.request("GET", "/emby/Genres?" + entity_query, emby=True)
                check(entities.get("TotalRecordCount") == len(expected["Genres"]) and
                      {entry.get("Name"): entry.get("Id") for entry in entities.get("Items", [])} ==
                      {name: str(genre_ids[name]) for name in expected["Genres"]}, stage + ": genre list IDs do not match embedded references")
            else:
                check(item.get("Name") == "NFO Smoke Media" and item.get("Overview") == "" and
                      item.get("ProductionYear") is None and item.get("Genres") == [] and item.get("ProviderIds") == {},
                      "Removing the sidecar did not restore filename-only metadata")
            probe = probe_view(item)
            if original_probe is None:
                original_id, original_probe = item["Id"], probe
                streams = probe["MediaStreams"] or []
                check(any(stream.get("Type") == "Video" and stream.get("Codec") == "h264" and
                          stream.get("Width") == 160 and stream.get("Height") == 90 for stream in streams),
                      "The real fixture video probe does not match H.264 at 160x90")
                check(any(stream.get("Type") == "Audio" and stream.get("Codec") == "aac" for stream in streams),
                      "The real fixture audio probe does not contain AAC")
                check(19_000_000 <= probe["RunTimeTicks"] <= 22_000_000 and probe["Source"]["Size"] == copied_stat.st_size,
                      "The real fixture duration or media size is invalid")
            check(item["Id"] == original_id and probe == original_probe, stage + ": item ID or probe fields changed")
            check(digest(copied_media) == source_hash and copied_media.stat().st_mtime_ns == copied_stat.st_mtime_ns,
                  stage + ": media bytes or mtime changed")
            summary["assertions"].append({"stage": stage, "scanned": job["Scanned"], "added": job["Added"],
                                          "updated": job["Updated"], "warning": bool(job.get("Error")),
                                          "item_id_stable": True, "probe_fields_stable": True})
        stage = "native catalog deletion"
        api.request("DELETE", f"/admin/v1/libraries/{quote(library_id)}", admin=True, expected=(204,), parse=False)
        library_id = ""
        creation_attempted = False
        check(copied_media.is_file() and digest(copied_media) == source_hash, "Native deletion changed the media file")
        api.request("GET", "/readyz", parse=False)
        summary["assertions"].append({"stage": stage, "media_preserved": True, "ready_status": 200})
        summary["probe_scope"] = "Stable HTTP technical fields; ffprobe invocation counts remain covered by Go tests."
        summary["facet_scope"] = "Positive embedded numeric genre IDs match queryable string entity IDs and remain stable across NFO updates."
        summary["genre_ids"] = genre_ids
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, SmokeFailure) else type(error).__name__
    finally:
        catalog_unknown = False
        if creation_attempted and not library_id and fixture and fixture.directory:
            try:
                libraries = api.request("GET", "/admin/v1/libraries", admin=True)["Items"]
                matches = [entry for entry in libraries if entry.get("Name") == fixture.directory.name and
                           entry.get("Paths") == [str(fixture.directory)]]
                check(len(matches) <= 1, "Multiple catalog entries matched the unique owned fixture")
                library_id = matches[0]["Id"] if matches else ""
            except Exception:
                catalog_unknown = True
                summary["cleanup_errors"].append("Library creation result is unknown; fixture directory retained")
        if library_id:
            try:
                jobs = api.request("GET", "/admin/v1/jobs", admin=True)["Items"]
                for owned_job in jobs:
                    if owned_job.get("LibraryId") == library_id and owned_job.get("Status") in {"pending", "running"}:
                        api.request("POST", f"/admin/v1/jobs/{quote(owned_job['Id'])}/cancel", admin=True, expected=(202,))
                        api.wait_job(owned_job["Id"], timeout=10)
                api.request("DELETE", f"/admin/v1/libraries/{quote(library_id)}", admin=True, expected=(204, 404), parse=False)
                library_id = ""
            except Exception:
                summary["cleanup_errors"].append("Owned catalog entry could not be removed; fixture directory retained")
        if fixture and fixture.directory and not library_id and not catalog_unknown:
            try:
                fixture.cleanup()
                summary["owned_directory_removed"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned directory cleanup refused or failed")
        for mode, method, path, expected in (("emby", "POST", "/emby/Sessions/Logout", (204,)),
                                              ("admin", "DELETE", "/admin/v1/session", (204,))):
            if (mode == "emby" and api.token) or (mode == "admin" and api.cookie):
                try:
                    api.request(method, path, **{mode: True}, expected=expected, parse=False)
                except Exception:
                    summary["cleanup_errors"].append("Smoke-check " + mode + " session could not be revoked")
        if source and source_hash:
            try:
                summary["source_sha256_before"] = source_hash
                summary["source_sha256_after"] = digest(source)
                check(summary["source_sha256_after"] == source_hash, "Source media hash changed")
                summary["source_media_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("Source media hash was not preserved or could not be verified")
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
