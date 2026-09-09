#!/usr/bin/env python3
"""Capture bounded metadata contracts using only a newly owned reference item.

Run through SSH in the existing official reference network namespace. Existing
records, media, users, library options, and global configuration are immutable.
The single tiny source and all new evidence live in marked execution scratch.
"""

from __future__ import annotations

import argparse
import base64
import copy
import datetime as dt
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qs, urlencode, urlsplit

sys.dont_write_bytecode = True
BASE_PATH = Path("/opt/goby-test/repository/scripts/test-env/reference-capture.py")
SPEC = importlib.util.spec_from_file_location("metadata_reference_base", BASE_PATH)
BASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BASE)

ROOT = Path("/opt/goby-test/exec-scratch/metadata-m5b")
SOURCE = ROOT / "source"
MOVIE = SOURCE / "Metadata M5b Owned (2026).mp4"
NFO = MOVIE.with_suffix(".nfo")
PRIVATE, RAW, EXPORT = ROOT / "private", ROOT / "private/raw", ROOT / "export"
MARKER = "goby-metadata-m5b-owned-v1"
PREFIX = "metadata-m5b-"
LIBRARY_NAME = "Metadata M5b Owned Fixture"
DEVICE = "goby-metadata-m5b-recorder"
MAX_BODY, MAX_TOTAL = 256 * 1024, 2 * 1024 * 1024
FFMPEG = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
EDIT_FIELDS = (
    "Name", "SortName", "ForcedSortName", "OriginalTitle", "Overview", "ProductionYear", "PremiereDate",
    "EndDate", "CommunityRating", "CriticRating", "OfficialRating", "CustomRating", "ProviderIds",
    "Genres", "Tags", "TagItems", "Studios", "People", "LockedFields", "LockData", "Taglines", "ProductionLocations",
    "PreferredMetadataLanguage", "PreferredMetadataCountryCode", "IndexNumber", "ParentIndexNumber",
    "SortIndexNumber", "SortParentIndexNumber", "DisplayOrder", "Status", "DateCreated",
)


def require(condition: bool, message: str) -> None:
    if not condition:
        raise RuntimeError(message)


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(65536), b""):
            result.update(block)
    return result.hexdigest()


def write_private(path: Path, value: object, exclusive: bool = False) -> None:
    text = value if isinstance(value, str) else json.dumps(value, indent=2, ensure_ascii=False) + "\n"
    flags = os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | (os.O_EXCL if exclusive else os.O_TRUNC)
    with os.fdopen(os.open(path, flags, 0o600), "w", encoding="utf-8") as stream:
        stream.write(text)


def preconditions() -> int:
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run only through authorized root SSH")
    properties = dict(line.split("=", 1) for line in subprocess.check_output([
        "systemctl", "show", "goby-emby-reference.service", "-p", "MainPID", "-p", "PrivateNetwork", "-p", "ActiveState"
    ], timeout=5, text=True).splitlines())
    pid = int(properties["MainPID"])
    require(pid > 1 and properties["PrivateNetwork"] == "yes" and properties["ActiveState"] == "active", "Reference isolation is unavailable")
    require(os.readlink("/proc/self/ns/net") == os.readlink(f"/proc/{pid}/ns/net") and
            os.readlink("/proc/self/ns/net") != os.readlink("/proc/1/ns/net"), "Recorder is outside the reference network namespace")
    for directory in (BASE.DATA, Path("/dev/shm/goby-emby-reference")):
        require((directory / ".goby-managed").read_text().strip() == "goby-emby-reference-owned-v1", "Existing reference ownership differs")
    require(Path("/opt/goby-test/exec-scratch.owner").read_text().strip() == "goby-verification-scratch", "Execution scratch ownership differs")
    require(shutil.disk_usage(ROOT.parent).free > 8 * 1024 * 1024, "Execution scratch has insufficient bounded capture space")
    return pid


def old_files() -> tuple[list[Path], list[Path]]:
    runtime = Path("/dev/shm/goby-emby-reference/runtime")
    directories = [BASE.PRIVATE / "raw", BASE.EXPORT]
    for name in ("audio-m4c", "audio-m4c-defaults", "audio-profile-m4d", "video-profile-m4e"):
        directory = runtime / name
        require(directory.is_dir() and (directory / ".goby-managed").is_file(), "A preceding reference directory is missing")
        directories.extend((directory / "private/raw", directory / "export"))
    records = sorted(path for directory in directories for path in directory.glob("*.json"))
    require(len(records) == 1656, "Expected exactly 828 preserved raw/export record pairs")
    media = []
    for directory in (Path("/opt/goby-fixtures"), runtime / "audio-m4c/source"):
        for path in directory.rglob("*"):
            if path.is_file():
                info = path.lstat()
                require(stat.S_ISREG(info.st_mode) and path.resolve() == path, "A preserved media path is not an ordinary file")
                media.append(path)
    # Existing synthetic episodes include hard links: about 23 MiB of paths
    # refer to about 5 MiB of unique media. Hashing remains stream-buffered.
    require(sum(path.stat().st_size for path in media) < 32 * 1024 * 1024, "Preserved media hashing exceeds its bound")
    return records, sorted(media)


class Recorder(BASE.Recorder):
    def __init__(self) -> None:
        super().__init__()
        require(ROOT.resolve(strict=True) == ROOT and (ROOT / ".goby-managed").read_text().strip() == MARKER, "New capture ownership differs")
        own_credentials = PRIVATE / "credentials.env"
        if own_credentials.exists():
            self.credentials = BASE.read_credentials(own_credentials)
        else:
            self.credentials = {key: self.credentials[key] for key in ("REFERENCE_USERNAME", "REFERENCE_PASSWORD")}
        self.credential_path = own_credentials
        self.secret_values.update(value for key, value in self.credentials.items() if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"})
        self.client_headers = {"Accept": "application/json", "Authorization":
            'Emby Client="Goby Metadata Reference", Device="Metadata M5b Owned Fixture", '
            f'DeviceId="{DEVICE}", Version="0.1.0"'}
        self.context = json.loads((PRIVATE / "context.json").read_text()) if (PRIVATE / "context.json").exists() else {}
        self.total = sum(json.loads(path.read_text()).get("observation", {}).get("wireBytes", 0) for path in RAW.glob(PREFIX + "*.json"))
        self.checks = len(list(RAW.glob(PREFIX + "owned-check-*.json")))

    def clean_text(self, value: str) -> str:
        return super().clean_text(value).replace(str(SOURCE), "/reference-metadata-m5b-source").replace(
            str(ROOT), "/reference-metadata-m5b-private").replace("/opt/goby-fixtures", "/reference-fixtures").replace(
            "/dev/shm/goby-emby-reference/runtime", "/reference-runtime")

    def audit_export(self, original, exported, key="") -> None:
        if isinstance(original, str) and any(path in original for path in (str(ROOT), "/opt/goby-fixtures", "/dev/shm/goby-emby-reference/runtime")):
            require(exported == self.clean_text(original), "Path redaction changed unrelated metadata text")
            return
        super().audit_export(original, exported, key)

    def write(self, name: str, record: dict) -> None:
        require(all(not (folder / (PREFIX + name + ".json")).exists() for folder in (RAW, EXPORT)), "Refusing to overwrite capture evidence")
        record.setdefault("reference", {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": dt.datetime.now(dt.timezone.utc).isoformat()})
        cleaned = self.sanitize(record)
        self.audit_export(record, cleaned)
        text = json.dumps(cleaned, indent=2, ensure_ascii=False) + "\n"
        require(not any(secret and secret in text for secret in self.secret_values), "A secret survived capture redaction")
        write_private(RAW / (PREFIX + name + ".json"), record, True)
        write_private(EXPORT / (PREFIX + name + ".json"), text, True)

    def save_context(self) -> None:
        write_private(PRIVATE / "context.json", self.context)

    def request(self, name: str, method: str, path: str, body=None, authenticated=True) -> tuple[int, object]:
        require(path.startswith("/emby/") and not path.startswith("//") and len(path) < 8192, "Request is not a bounded reference route")
        if method != "GET":
            self.guard_write(path, body)
        require(all(not (folder / (PREFIX + name + ".json")).exists() for folder in (RAW, EXPORT)), "Capture label already exists")
        headers = dict(self.client_headers)
        if authenticated:
            headers["X-Emby-Token"] = self.credentials["REFERENCE_TOKEN"]
        payload = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        if payload is not None:
            headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=10)
        status_code, response_headers, content = None, [], b""
        complete, failure = False, None
        try:
            connection.request(method, path, payload, headers)
            response = connection.getresponse()
            status_code, response_headers = response.status, response.getheaders()
            content = response.read(MAX_BODY + 1)
            require(len(content) <= MAX_BODY, "Metadata HTTP body exceeds its bound")
            complete = True
        except http.client.IncompleteRead as error:
            content, failure = error.partial[:MAX_BODY], "IncompleteRead"
        except Exception as error:
            failure = type(error).__name__
        finally:
            connection.close()
        self.total += len(content)
        require(self.total <= MAX_TOTAL, "Metadata capture exceeds its total wire budget")
        try:
            text = content.decode("utf-8")
            try:
                parsed, kind = json.loads(text), "json"
            except json.JSONDecodeError:
                parsed, kind = text, "text"
        except UnicodeDecodeError:
            parsed, kind = base64.b64encode(content).decode("ascii"), "binary-base64"
        if isinstance(parsed, dict) and parsed.get("AccessToken"):
            self.credentials["REFERENCE_TOKEN"] = parsed["AccessToken"]
            self.credentials["REFERENCE_USER_ID"] = parsed["User"]["Id"]
            self.secret_values.add(parsed["AccessToken"])
            BASE.save_credentials(self.credentials, self.credential_path)
        self.write(name, {"request": {"method": method, "path": path, "headers": headers, "body": body},
            "response": {"status": status_code, "headers": response_headers, "bodyType": kind, "body": parsed},
            "observation": {"completeHTTP": complete, "wireBytes": len(content), "wireSha256": hashlib.sha256(content).hexdigest(), "failureType": failure}})
        print(f"{PREFIX}{name}: HTTP {status_code}, complete={complete}, bytes={len(content)}", flush=True)
        require(complete, "Reference response was incomplete; evidence was retained")
        return status_code, parsed

    def guard_write(self, target: str, body: object) -> None:
        path, query = urlsplit(target).path, parse_qs(urlsplit(target).query)
        if path == "/emby/Users/AuthenticateByName":
            require(not self.context.get("loggedOut"), "The owned capture session was already closed")
            return
        if path == "/emby/Sessions/Logout":
            require(self.credentials.get("REFERENCE_TOKEN") and DEVICE in self.client_headers["Authorization"], "Logout lacks an owned device session")
            return
        if path == "/emby/Library/VirtualFolders":
            require(not self.context.get("libraryID") and isinstance(body, dict) and body.get("Name") == LIBRARY_NAME and
                    body.get("Paths") == [str(SOURCE)] and body.get("RefreshLibrary") is False, "Library mutation is outside the new fixture")
            require((SOURCE / ".goby-managed").read_text().strip() == MARKER, "New source ownership differs")
            return
        library_id, item_id = self.context.get("libraryID"), self.context.get("itemID")
        if library_id and path == f"/emby/Items/{library_id}/Refresh":
            require(self.context.get("libraryPath") == str(SOURCE) and query.get("Recursive") == ["true"], "Library refresh is outside the owned tree")
            return
        require(item_id and path in {f"/emby/Items/{item_id}", f"/emby/Items/{item_id}/Refresh", "/emby/Items/Metadata/Reset"}, "Mutation route is outside the owned item")
        if path == "/emby/Items/Metadata/Reset":
            require(query.get("ItemIds") == [item_id], "Metadata reset is not restricted to the owned item")
        if path == f"/emby/Items/{item_id}":
            require(isinstance(body, dict) and str(body.get("Id")) == item_id, "Update body does not match the owned item")
        if path.endswith("/Refresh"):
            require(query.get("Recursive") == ["false"], "Item refresh may not traverse other items")
        self.checks += 1
        current = self.owned_item(f"owned-check-{self.checks:02}")
        require(current.get("Path") == str(MOVIE), "Mutation target no longer belongs to the owned source")

    def owned_item(self, label: str) -> dict:
        query = urlencode({"UserId": self.credentials["REFERENCE_USER_ID"], "ParentId": self.context["libraryID"], "Recursive": "true",
                           "Ids": self.context["itemID"], "Fields": "Path,Overview,ProviderIds,Genres,Tags", "Limit": 2})
        status_code, value = self.request(label, "GET", "/emby/Items?" + query)
        require(status_code == 200 and len(value.get("Items", [])) == 1 and str(value["Items"][0].get("Id")) == self.context["itemID"] and
                value["Items"][0].get("Path") == str(MOVIE), "Owned item is absent from its isolated library")
        return value["Items"][0]

    def detail(self, label: str) -> dict:
        status_code, item = self.request(label, "GET", f"/emby/Users/{self.credentials['REFERENCE_USER_ID']}/Items/{self.context['itemID']}")
        require(status_code == 200 and str(item.get("Id")) == self.context["itemID"] and item.get("Path") == str(MOVIE), "Item detail is outside the owned fixture")
        return item

    def update(self, label: str, changes: dict, missing=()) -> dict:
        current = self.detail(label + "-before")
        body = {key: copy.deepcopy(current[key]) for key in EDIT_FIELDS if key in current}
        body["Id"] = self.context["itemID"]
        body.update(changes)
        for key in missing:
            body.pop(key, None)
        status_code, _ = self.request(label + "-post", "POST", f"/emby/Items/{self.context['itemID']}", body)
        require(status_code in {200, 204}, "Owned item update was not accepted")
        result = self.detail(label + "-after")
        self.write(label + "-observation", {"kind": "metadata-state-observation", "source": PREFIX + label + "-after.json",
            "fields": {key: result.get(key) for key in EDIT_FIELDS}})
        return result

    def refresh(self, label: str, replace: bool) -> dict:
        query = urlencode({"Recursive": "false", "MetadataRefreshMode": "FullRefresh", "ImageRefreshMode": "ValidationOnly",
                           "ReplaceAllMetadata": str(replace).lower(), "ReplaceAllImages": "false"})
        status_code, _ = self.request(label + "-post", "POST", f"/emby/Items/{self.context['itemID']}/Refresh?" + query,
                                     {"ReplaceThumbnailImages": False})
        require(status_code in {200, 204}, "Owned item refresh was not accepted")
        for index, delay in enumerate((.3, .7, 1.5)):
            time.sleep(delay)
            result = self.detail(f"{label}-after-{index}")
        self.write(label + "-observation", {"kind": "metadata-state-observation", "source": PREFIX + label + "-after-2.json",
            "fields": {key: result.get(key) for key in EDIT_FIELDS}, "waitedSeconds": 2.5})
        return result

    def setup(self) -> None:
        require(not self.context, "Metadata setup already has context")
        status_code, public = self.request("system-info", "GET", "/emby/System/Info/Public", authenticated=False)
        require(status_code == 200 and public.get("Version") == "4.9.5.0", "Unexpected official reference version")
        status_code, _ = self.request("admin-login", "POST", "/emby/Users/AuthenticateByName",
            {"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]}, authenticated=False)
        require(status_code == 200, "Owned administrator-device login failed")
        status_code, libraries = self.request("libraries-before", "GET", "/emby/Library/VirtualFolders/Query")
        require(status_code == 200 and not any(item.get("Name") == LIBRARY_NAME for item in libraries["Items"]), "New metadata library already exists")
        self.context["oldLibraries"] = libraries["Items"]
        self.save_context()
        original = next(item for item in libraries["Items"] if item.get("Name") == "Reference Movies")
        options = copy.deepcopy(original["LibraryOptions"])
        options.update({"PathInfos": [{"Path": str(SOURCE)}], "SaveLocalMetadata": False, "MetadataSavers": [],
            "EnableRealtimeMonitor": False, "SampleIgnoreSize": 0, "EnableAutomaticSeriesGrouping": False,
            "EnableChapterImageExtraction": False, "EnableMarkerDetection": False, "AutomaticRefreshIntervalDays": 0})
        options["TypeOptions"] = [{"Type": "Movie", "MetadataFetchers": [], "MetadataFetcherOrder": [], "ImageFetchers": [], "ImageFetcherOrder": []}]
        status_code, _ = self.request("library-create", "POST", "/emby/Library/VirtualFolders",
            {"Name": LIBRARY_NAME, "CollectionType": "movies", "Paths": [str(SOURCE)], "RefreshLibrary": False, "LibraryOptions": options})
        require(status_code in {200, 204}, "Owned metadata library creation failed")
        status_code, libraries = self.request("libraries-after-create", "GET", "/emby/Library/VirtualFolders/Query")
        created = [item for item in libraries["Items"] if item.get("Name") == LIBRARY_NAME]
        require(status_code == 200 and len(created) == 1 and created[0].get("Locations") == [str(SOURCE)], "New metadata library ownership is ambiguous")
        require(str(created[0]["ItemId"]) not in {str(item["ItemId"]) for item in self.context["oldLibraries"]}, "New library reused an old library identifier")
        self.context.update({"libraryID": str(created[0]["ItemId"]), "libraryPath": str(SOURCE)})
        self.save_context()
        status_code, _ = self.request("library-initial-refresh", "POST", f"/emby/Items/{self.context['libraryID']}/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=ValidationOnly", {})
        require(status_code in {200, 204}, "Owned library refresh failed")
        query = urlencode({"UserId": self.credentials["REFERENCE_USER_ID"], "ParentId": self.context["libraryID"], "Recursive": "true",
                           "IncludeItemTypes": "Movie", "Fields": "Path,Overview,ProviderIds,Genres,Tags", "Limit": 10})
        for attempt in range(12):
            status_code, listed = self.request(f"owned-discovery-{attempt:02}", "GET", "/emby/Items?" + query)
            require(status_code == 200, "Owned library discovery failed")
            items = [item for item in listed.get("Items", []) if item.get("Path") == str(MOVIE)]
            if len(items) == 1:
                self.context["itemID"] = str(items[0]["Id"])
                self.save_context()
                break
            time.sleep(.3)
        require(self.context.get("itemID"), "Owned movie did not appear in its isolated library")
        self.detail("initial-detail")
        self.request("metadata-editor", "GET", f"/emby/Items/{self.context['itemID']}/MetadataEditor")

    def sort(self) -> None:
        self.update("full-fields-and-forced-sort", {
            "Name": "M5b Manual Name One", "SortName": "M5b Sort Input One", "ForcedSortName": "M5b Forced Sort One",
            "OriginalTitle": "M5b Original Title", "Overview": "M5b manual overview.", "ProductionYear": 2012,
            "PremiereDate": "2012-03-04T00:00:00.0000000Z", "CommunityRating": 7.25, "CriticRating": 83,
            "OfficialRating": "PG-13", "CustomRating": "M5b Custom Rating", "ProviderIds": {"MetadataM5b": "owned-provider-one"},
            "Genres": ["M5b Owned Genre"], "Tags": ["M5b Owned Tag"], "Studios": [{"Name": "M5b Owned Studio", "Id": 0}],
            "People": [{"Name": "M5b Owned Actor", "Type": "Actor", "Role": "M5b Role"}, {"Name": "M5b Owned Director", "Type": "Director"}],
            "LockedFields": [], "LockData": False,
        })
        self.update("name-with-sort-fields-missing", {"Name": "M5b Manual Name Two"}, ("SortName", "ForcedSortName"))
        self.update("sortname-with-forced-missing", {"Name": "M5b Manual Name Three", "SortName": "M5b Sort Input Three"}, ("ForcedSortName",))
        self.update("forced-sort-restored", {"Name": "M5b Manual Name Four", "ForcedSortName": "M5b Forced Sort Four"}, ("SortName",))
        self.update("name-with-forced-empty", {"Name": "M5b Manual Name Five", "ForcedSortName": ""}, ("SortName",))

    def nfo_control(self) -> None:
        require((SOURCE / ".goby-managed").read_text().strip() == MARKER, "NFO control ownership differs")
        previous = SOURCE / "movie.nfo"
        if previous.exists():
            require(previous.resolve(strict=True) == previous and not NFO.exists(), "Named NFO control would overwrite another file")
            previous.rename(NFO)
        self.write("named-nfo-control-source", {"kind": "owned-source-observation", "nfoPath": str(NFO),
            "nfoSha256": digest(NFO), "note": "The flat library initially ignored movie.nfo. This owned-only control uses the matching media basename."})
        self.refresh("named-nfo-control", True)

    def sort_locked(self) -> None:
        self.update("locked-sort-and-tagitems", {"Name": "M5b Locked Sort Name", "SortName": "M5b Locked Sort Input",
            "ForcedSortName": "M5b Locked Forced Input", "LockedFields": ["SortName"], "Tags": ["M5b Legacy Tag Control"],
            "TagItems": [{"Name": "M5b Tag Item Control", "Id": 0}]})
        self.update("locked-sort-fields-missing", {"Name": "M5b Locked Sort Renamed"}, ("SortName", "ForcedSortName"))
        self.update("locked-sort-forced-empty", {"Name": "M5b Locked Sort Empty", "ForcedSortName": ""}, ("SortName",))

    def locks(self) -> None:
        self.update("name-lock", {"Name": "M5b Locked Manual Name", "Overview": "M5b unlocked manual overview.", "LockedFields": ["Name"], "LockData": False}, ("SortName", "ForcedSortName"))
        write_nfo("M5b NFO Name Two", "M5b NFO overview two.", 2015)
        self.refresh("name-lock-full-refresh", True)
        self.update("manual-edit-while-name-locked", {"Name": "M5b Manual Edit While Locked"}, ("SortName", "ForcedSortName"))
        self.update("lockdata-enabled", {"Name": "M5b LockData Manual Name", "Overview": "M5b LockData manual overview.", "LockedFields": [], "LockData": True}, ("SortName", "ForcedSortName"))
        write_nfo("M5b NFO Name Three", "M5b NFO overview three.", 2016)
        self.refresh("lockdata-refresh", False)
        self.refresh("lockdata-replace-all-refresh", True)
        status_code, _ = self.request("reset-post", "POST", "/emby/Items/Metadata/Reset?" + urlencode({"ItemIds": self.context["itemID"]}))
        require(status_code in {200, 204}, "Owned metadata reset was not accepted")
        for index, delay in enumerate((.3, .7, 1.5)):
            time.sleep(delay)
            result = self.detail(f"reset-after-{index}")
        self.write("reset-observation", {"kind": "metadata-state-observation", "source": PREFIX + "reset-after-2.json",
            "fields": {key: result.get(key) for key in EDIT_FIELDS}, "waitedSeconds": 2.5})

    def confirm(self) -> None:
        self.update("sortname-only-under-lock", {"Name": "M5b SortName Only Locked", "SortName": "M5b Plain Sort Under Lock",
            "LockedFields": ["SortName"], "LockData": False}, ("ForcedSortName",))
        self.update("manual-name-lock-established", {"Name": "M5b Confirmed Name Lock", "LockedFields": ["Name"], "LockData": False},
                    ("SortName", "ForcedSortName"))
        self.update("manual-edit-confirmed-name-lock", {"Name": "M5b Direct Edit Under Name Lock"}, ("SortName", "ForcedSortName"))
        self.update("lockdata-reestablished-for-replace", {"Name": "M5b Confirmed LockData Name", "Overview": "M5b confirmed LockData overview.",
            "LockedFields": [], "LockData": True}, ("SortName", "ForcedSortName"))
        self.refresh("confirmed-lockdata-replace-all", True)
        status_code, _ = self.request("final-reset-post", "POST", "/emby/Items/Metadata/Reset?" + urlencode({"ItemIds": self.context["itemID"]}))
        require(status_code in {200, 204}, "Final owned metadata reset was not accepted")
        time.sleep(.5)
        self.detail("final-reset-after")

    def finish(self) -> None:
        status_code, libraries = self.request("libraries-final", "GET", "/emby/Library/VirtualFolders/Query")
        require(status_code == 200, "Final library audit failed")
        current = {str(item["ItemId"]): item for item in libraries["Items"]}
        for prior in self.context["oldLibraries"]:
            value = current[str(prior["ItemId"])]
            require(value.get("Name") == prior.get("Name") and value.get("Locations") == prior.get("Locations") and
                    value.get("LibraryOptions") == prior.get("LibraryOptions"), "An old library name, path, or options changed")
        self.detail("final-owned-detail")
        self.request("admin-logout", "POST", "/emby/Sessions/Logout")
        self.context["loggedOut"] = True
        self.save_context()
        baseline = json.loads((PRIVATE / "baseline.json").read_text())
        require(all(digest(Path(path)) == expected for path, expected in baseline["records"].items()), "A preceding reference record changed")
        require(all(digest(Path(path)) == expected for path, expected in baseline["media"].items()), "A preceding reference source changed")
        require(digest(MOVIE) == baseline["movieSha256"], "The new owned movie bytes changed")
        for path in RAW.glob(PREFIX + "*.json"):
            original = json.loads(path.read_text())
            exported = json.loads((EXPORT / path.name).read_text())
            self.audit_export(original, exported)
            require(not any(secret and secret in json.dumps(exported) for secret in self.secret_values), "Final export contains a secret")
        records = [json.loads(path.read_text()) for path in RAW.glob(PREFIX + "*.json")]
        http = [record for record in records if "request" in record]
        observed = {"kind": "capture-audit-observation", "newRecordsBeforeAudit": len(records),
            "completeHTTP": sum(record["observation"]["completeHTTP"] for record in http),
            "incompleteHTTP": sum(not record["observation"]["completeHTTP"] for record in http),
            "supportingObservationsBeforeAudit": len(records) - len(http), "wireBytes": self.total,
            "preservedOldRecordFiles": len(baseline["records"]), "preservedOldRecords": 828,
            "preservedOldMediaFiles": len(baseline["media"]), "sourceBytes": sum(path.stat().st_size for path in SOURCE.iterdir()),
            "referencePID": preconditions(), "oldHashesUnchanged": True, "oldLibraryOptionsUnchanged": True,
            "ownedLibraryRetained": True, "note": "Only the new marked micro-library is retained. The pinned delete route omits targeting parameters, so no undocumented delete was guessed."}
        self.write("audit", observed)
        print(json.dumps({key: value for key, value in observed.items() if key not in {"note", "kind"}}, sort_keys=True), flush=True)


def write_nfo(name: str, overview: str, year: int, generic: bool = False) -> None:
    require(SOURCE.resolve(strict=True) == SOURCE and (SOURCE / ".goby-managed").read_text().strip() == MARKER, "NFO write is outside the owned source")
    require(all("<" not in value and "&" not in value for value in (name, overview)), "Fixture XML text must be literal")
    path = SOURCE / "movie.nfo" if generic else NFO
    write_private(path, f"<?xml version=\"1.0\" encoding=\"utf-8\"?>\n<movie><title>{name}</title><plot>{overview}</plot><year>{year}</year></movie>\n")
    require(sum(path.stat().st_size for path in SOURCE.iterdir()) < 1024 * 1024, "Owned metadata source exceeds one MiB")


def prepare() -> None:
    pid = preconditions()
    require(not ROOT.exists(), "Metadata capture directory already exists")
    records, media = old_files()
    ROOT.mkdir(mode=0o700)
    for directory in (PRIVATE, RAW, EXPORT, SOURCE):
        directory.mkdir(mode=0o700)
    write_private(ROOT / ".goby-managed", MARKER + "\n", True)
    write_private(SOURCE / ".goby-managed", MARKER + "\n", True)
    baseline = {"records": {str(path): digest(path) for path in records}, "media": {str(path): digest(path) for path in media}, "referencePID": pid}
    subprocess.run([str(FFMPEG), "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=blue:size=64x36:rate=10:duration=0.6",
                    "-an", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-pix_fmt", "yuv420p", str(MOVIE)],
                   timeout=15, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    write_nfo("M5b NFO Source Alpha", "M5b NFO source overview alpha.", 2001, generic=True)
    baseline["movieSha256"] = digest(MOVIE)
    write_private(PRIVATE / "baseline.json", baseline, True)
    recorder = Recorder()
    recorder.write("source-provenance", {"kind": "owned-source-provenance", "mediaBytes": MOVIE.stat().st_size, "mediaSha256": digest(MOVIE),
        "format": "MP4/H.264", "durationSeconds": .6, "dimensions": [64, 36], "sourcePath": str(MOVIE),
        "sourceOwnershipMarker": MARKER, "preservedRecordPairs": 828, "preservedMediaFiles": len(media), "referencePID": pid})


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("prepare", "setup", "nfo_control", "sort", "sort_locked", "locks", "confirm", "finish"))
    action = parser.parse_args().action
    if action == "prepare":
        prepare()
    else:
        preconditions()
        getattr(Recorder(), action)()
