#!/usr/bin/env python3
"""Prepare one owned Emby 4.9.5.0 client-acceptance reference fixture.

Only the fixed package executable and new fixture media are read. No server or
client implementation source, old reference data, or old account is accessed.
Failed attempts retain all data and credentials. Successful preparation can be
reused without restarting the service or logging out browser sessions.
"""

from __future__ import annotations

import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys
import time
from urllib.parse import urlencode, urlsplit

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
DATA = WORK / "reference-data"
OWNER = WORK / "reference-owner.json"
LOCK = WORK / "reference.lock"
CREDENTIALS = WORK / "reference-credentials.json"
BROWSER = WORK / "reference-browser.json"
BOOTSTRAP = WORK / "reference-bootstrap.json"
REPORT = WORK / "reference-report.json"
HTTP_LOG = WORK / "reference-http"
LAUNCHER = WORK / "reference-launch.sh"
APP = Path("/dev/shm/goby-emby-reference/package/opt/emby-server")
BINARY = APP / "system/EmbyServer"
BINARY_SHA256 = "c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2"
MEDIA = Path("/opt/goby-fixtures/client-m3e")
PARENT_MARKER = "goby-m3e-client-acceptance-v1"
MARKER = "goby-emby-client-reference-m3e-v1"
MEDIA_MARKER = "goby-client-media-m3e-v1"
UNIT = "goby-emby-client-m3e.service"
OLD_UNIT = "goby-emby-reference.service"
DESCRIPTION = "Goby owned Emby client acceptance reference M3e"
SERVER_NAME = "Goby Client Reference M3e"
PORT = 18097
NAMES = {"admin": "m3e-reference-admin", "viewer": "m3e-reference-viewer", "viewer2": "m3e-reference-viewer2"}
LIBRARIES = {"Movies": "movies", "TV": "tvshows", "Music": "music"}
BASE_ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
MAX_BODY = 2 * 1024 * 1024
STATIC_PROPERTIES = ("Id", "Description", "PrivateNetwork", "PrivateTmp", "NoNewPrivileges", "ProtectSystem", "ProtectHome",
                     "ReadWritePaths", "ReadOnlyPaths", "WorkingDirectory", "ExecStart", "ControlGroup", "DropInPaths",
                     "MemoryMax", "CPUQuotaPerSecUSec", "TasksMax", "LimitNOFILE", "UMask", "KillMode", "User",
                     "CapabilityBoundingSet", "FragmentPath", "TimeoutStopUSec")


class ReferenceError(Exception):
    """A bounded reference-fixture safety or acceptance check failed."""


def require(condition, message):
    if not condition:
        raise ReferenceError(message)


def present(path):
    try:
        path.lstat()
        return True
    except FileNotFoundError:
        return False


def canonical(path, *, directory=False, mode=None, allow_links=False):
    require(path.is_absolute(), "A fixture path is not absolute.")
    for entry in reversed((path, *path.parents)):
        info = entry.lstat()
        require(not stat.S_ISLNK(info.st_mode), "A fixture path contains a symlink.")
        if entry != path:
            sticky_shared_memory = entry == Path("/dev/shm") and stat.S_IMODE(info.st_mode) == 0o1777
            require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                    (not info.st_mode & 0o022 or sticky_shared_memory),
                    "A fixture administrative ancestor is not protected and root-owned.")
    info = path.lstat()
    require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "A fixture path has the wrong type.")
    require(info.st_uid == 0 and info.st_gid == 0 and not info.st_mode & 0o022, "A fixture path is not protected and root-owned.")
    require(mode is None or stat.S_IMODE(info.st_mode) == mode, "A fixture path has unexpected permissions.")
    require(directory or allow_links or info.st_nlink == 1, "A fixture file has unexpected hard links.")
    return info


def digest(path, *, allow_links=False):
    canonical(path, allow_links=allow_links)
    result = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(65536), b""):
            result.update(block)
    return result.hexdigest()


def read_private(path):
    info = canonical(path, mode=0o600)
    require(info.st_size <= 16 * MAX_BODY, "A private fixture input exceeds its bound.")
    return json.loads(path.read_text(encoding="utf-8"))


def save(path, value, *, mode=0o600):
    canonical(path.parent, directory=True, mode=0o700)
    value = value if isinstance(value, str) else json.dumps(value, sort_keys=True, indent=2) + "\n"
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        handle.write(value)
        handle.flush()
        os.fsync(handle.fileno())
    sync_directory(path.parent)


def sync_directory(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def update(path, value):
    require(path in (OWNER, BOOTSTRAP), "An unexpected authority file cannot be replaced.")
    canonical(path, mode=0o600)
    temporary = WORK / (path.name + ".next-" + secrets.token_hex(12))
    save(temporary, value)
    os.replace(temporary, path)
    sync_directory(WORK)


def run(arguments, *, timeout=20, pass_fds=()):
    try:
        result = subprocess.run([str(value) for value in arguments], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=timeout, check=False, env=BASE_ENV, pass_fds=pass_fds)
    except (OSError, subprocess.TimeoutExpired):
        raise ReferenceError("A bounded fixture command could not complete; retained evidence was preserved.") from None
    require(result.returncode == 0 and len(result.stdout) < 16 * MAX_BODY,
            "A fixture command failed; inspect only its private evidence and log.")
    return result.stdout.decode("utf-8").strip()


def properties(unit):
    result = subprocess.run(["/usr/bin/systemctl", "show", unit, "-p", "LoadState", "-p", "ActiveState",
                             "-p", "MainPID", "-p", "InvocationID", *[part for name in STATIC_PROPERTIES for part in ("-p", name)]],
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False, timeout=15, env=BASE_ENV)
    require(len(result.stdout) < MAX_BODY, "Service property output exceeded its bound.")
    values = dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)
    require(result.returncode == 0 or (result.returncode == 1 and values.get("LoadState") == "not-found"),
            "The service state could not be inspected.")
    return values


def process_identity(pid):
    require(type(pid) is int and pid > 1, "The service has no valid process.")
    process = Path("/proc") / str(pid)
    raw = (process / "stat").read_text()
    fields = raw[raw.rfind(")") + 2:].split()
    require(len(fields) > 19 and fields[19].isdigit(), "A process start identity is unavailable.")
    return {"pid": pid, "startTicks": fields[19], "bootId": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
            "uid": process.stat().st_uid, "exe": os.readlink(process / "exe"),
            "cmdline": [part.decode() for part in (process / "cmdline").read_bytes().split(b"\0") if part],
            "networkNamespace": os.readlink(process / "ns/net"), "cgroup": (process / "cgroup").read_text()}


def protected_reference():
    state = properties(OLD_UNIT)
    require(state.get("ActiveState") == "active" and state.get("PrivateNetwork") == "yes",
            "The protected reference service is not running in its existing private namespace.")
    identity = process_identity(int(state.get("MainPID", "0")))
    require(identity["exe"] == str(BINARY) and identity["uid"] == 0,
            "The protected reference process does not match the shared fixed executable.")
    return {"process": identity, "invocationId": state.get("InvocationID")}


def verify_media():
    canonical(MEDIA, directory=True)
    manifest_path = MEDIA / "manifest.json"
    canonical(manifest_path)
    manifest = json.loads(manifest_path.read_text())
    require(manifest.get("marker") == MEDIA_MARKER and isinstance(manifest.get("files"), dict) and
            len(manifest["files"]) == 14, "The new media manifest does not match the fourteen-file fixture.")
    actual, link_groups = {}, {}
    for path in MEDIA.rglob("*"):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode), "The new media fixture contains a symlink.")
        canonical(path, directory=stat.S_ISDIR(info.st_mode), allow_links=True)
        if path.is_file() and path != manifest_path:
            actual[str(path.relative_to(MEDIA))] = digest(path, allow_links=True)
            link_groups.setdefault((info.st_dev, info.st_ino), []).append(info.st_nlink)
    require(all(all(link_count == len(members) for link_count in members) for members in link_groups.values()),
            "A new media file has a hard link outside the complete owned fixture population.")
    require(actual == manifest["files"], "New media membership or content differs from its fixed manifest.")
    require((MEDIA / ".goby-managed").read_text().strip() == MEDIA_MARKER,
            "The new media ownership marker differs.")
    for name in LIBRARIES:
        canonical(MEDIA / name, directory=True)
    return {"path": str(MEDIA), "manifestSha256": digest(manifest_path), "files": actual}


def preconditions():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
            "Run the fixture operator only through authorized root SSH.")
    canonical(WORK, directory=True, mode=0o700)
    require(read_private(WORK / "OWNER.json") == {"marker": PARENT_MARKER, "path": str(WORK)},
            "The M3e parent ownership marker differs.")
    canonical(APP, directory=True)
    require(digest(BINARY) == BINARY_SHA256, "The reference executable differs from the pinned Emby package.")


def work_identity():
    info = canonical(WORK, directory=True, mode=0o700)
    return {"path": str(WORK), "device": info.st_dev, "inode": info.st_ino}


def data_identity():
    info = canonical(DATA, directory=True, mode=0o700)
    marker = read_private(DATA / "OWNER.json")
    require(marker == {"marker": MARKER, "path": str(DATA)}, "The reference data marker differs.")
    return {"device": info.st_dev, "inode": info.st_ino, "path": str(DATA)}


def service_arguments():
    return [str(BINARY), "-programdata", str(DATA), "-ffdetect", str(APP / "bin/ffdetect"),
            "-ffmpeg", str(APP / "bin/ffmpeg"), "-ffprobe", str(APP / "bin/ffprobe"),
            "-restartexitcode", "3", "-updatepackage", "emby-server-deb_{version}_amd64.deb"]


def service_identity(owner):
    require(work_identity() == owner["workIdentity"] and data_identity() == owner["dataIdentity"] and
            digest(LAUNCHER) == owner["launcherSha256"],
            "The owned data directory or launcher identity changed.")
    state = properties(UNIT)
    require(state.get("ActiveState") == "active", "The owned reference service is not active.")
    expected = {"Id": UNIT, "Description": DESCRIPTION, "PrivateNetwork": "yes", "PrivateTmp": "yes",
                "NoNewPrivileges": "yes", "ProtectSystem": "strict", "ProtectHome": "yes",
                "ReadWritePaths": str(DATA), "WorkingDirectory": str(DATA), "MemoryMax": str(1024 ** 3),
                "TasksMax": "256", "LimitNOFILE": "65536", "UMask": "0077", "KillMode": "control-group",
                "CapabilityBoundingSet": "", "ControlGroup": "/system.slice/" + UNIT, "DropInPaths": "",
                "TimeoutStopUSec": "25s"}
    require(all(state.get(key) == value for key, value in expected.items()) and state.get("User") in ("", "root") and
            state.get("CPUQuotaPerSecUSec") in ("1.5s", "1s 500ms", "1500ms", "1.500000s") and
            set(state.get("ReadOnlyPaths", "").split()) == {str(WORK), str(APP), str(MEDIA)} and
            re.findall(r"(?:^|[ {;])path=([^;]+?)\s*;", state.get("ExecStart", "")) == [str(LAUNCHER)] and
            state.get("FragmentPath") == "/run/systemd/transient/" + UNIT and
            re.fullmatch(r"[0-9a-f]{32}", state.get("InvocationID", "")),
            "The new service unit, resource limits, sandbox, or launch command differs.")
    identity = process_identity(int(state.get("MainPID", "0")))
    require(identity["uid"] == 0 and identity["exe"] == str(BINARY) and identity["cmdline"] == service_arguments() and
            identity["cgroup"].strip() == "0::/system.slice/" + UNIT and
            identity["networkNamespace"] not in (os.readlink("/proc/1/ns/net"), owner["protectedReference"]["process"]["networkNamespace"]),
            "The reference process executable, identity, or private namespace differs.")
    mounts = {}
    for line in (Path("/proc") / str(identity["pid"]) / "mountinfo").read_text().splitlines():
        fields = line.split()
        mounts[fields[4]] = set(fields[5].split(","))
    for path, required in ((WORK, "ro"), (APP, "ro"), (MEDIA, "ro"), (DATA, "rw")):
        candidates = [root for root in mounts if str(path) == root or str(path).startswith(root.rstrip("/") + "/")]
        require(candidates and required in mounts[max(candidates, key=len)], "Actual reference mount permissions differ.")
    require(process_identity(identity["pid"]) == identity, "The reference process changed during attestation.")
    return {**identity, "invocationId": state["InvocationID"]}


def same_service(owner):
    require(service_identity(owner) == owner["serviceIdentity"], "The reference process was replaced or restarted.")
    require(protected_reference() == owner["protectedReference"], "The protected reference process changed.")


def validate_owner(owner):
    require(isinstance(owner, dict) and owner.get("marker") == MARKER and owner.get("unit") == UNIT and
            owner.get("data") == str(DATA) and owner.get("binary") == str(BINARY) and
            owner.get("binarySha256") == BINARY_SHA256 and owner.get("port") == PORT,
            "The reference owner record does not identify this exact fixture.")


def library_body(name):
    require(name in LIBRARIES, "An unowned media library was requested.")
    types = ("Movie", "Series", "Season", "Episode", "MusicArtist", "MusicAlbum", "Audio")
    options = {"PathInfos": [{"Path": str(MEDIA / name)}], "SampleIgnoreSize": 0,
               "EnableRealtimeMonitor": False, "EnableChapterImageExtraction": False,
               "ExtractChapterImagesDuringLibraryScan": False, "EnableMarkerDetection": False,
               "EnableMarkerDetectionDuringLibraryScan": False, "DownloadImagesInAdvance": False,
               "SaveLocalMetadata": False, "SaveLocalThumbnailSets": False, "SaveSubtitlesWithMedia": False,
               "SaveLyricsWithMedia": False, "MetadataSavers": [], "SubtitleDownloadLanguages": [],
               "LyricsDownloadLanguages": [], "AutomaticRefreshIntervalDays": 0, "EnableEmbeddedTitles": True,
               "EnableAutomaticSeriesGrouping": False,
               "TypeOptions": [{"Type": media_type, "MetadataFetchers": [], "MetadataFetcherOrder": [],
                                "ImageFetchers": [], "ImageFetcherOrder": [], "ImageOptions": []} for media_type in types]}
    return {"Name": "M3e Reference " + name, "CollectionType": LIBRARIES[name], "RefreshLibrary": False,
            "Paths": [str(MEDIA / name)], "LibraryOptions": options}


def verify_library(row, name):
    expected = library_body(name)
    require(row.get("Name") == expected["Name"] and row.get("CollectionType") == expected["CollectionType"] and
            row.get("Locations") == expected["Paths"] and isinstance(row.get("ItemId"), str) and row["ItemId"],
            "A new reference library has an unexpected identity or source path.")
    options = row.get("LibraryOptions", {})
    for key, value in expected["LibraryOptions"].items():
        if key == "PathInfos":
            require(isinstance(options.get(key), list) and len(options[key]) == 1 and
                    options[key][0].get("Path") == str(MEDIA / name) and not options[key][0].get("NetworkPath"),
                    "A reference library path mapping differs.")
            continue
        if key == "TypeOptions":
            actual = options.get(key)
            require(isinstance(actual, list) and {item.get("Type") for item in actual} == {item["Type"] for item in value},
                    "The reference metadata type population differs.")
            require(all(item.get(field) == [] for item in actual for field in
                        ("MetadataFetchers", "MetadataFetcherOrder", "ImageFetchers", "ImageFetcherOrder", "ImageOptions")),
                    "A reference library has an enabled metadata or image provider.")
            continue
        require(options.get(key) == value, "A reference library option differs from its safe fixed template.")


class API:
    def __init__(self, owner):
        self.owner = owner
        self.state = read_private(BOOTSTRAP)
        require(self.state.get("marker") == MARKER, "The bootstrap state marker differs.")
        self.credentials = read_private(CREDENTIALS)
        require(self.credentials.get("marker") == MARKER and set(self.credentials.get("accounts", {})) == set(NAMES),
                "The bootstrap credentials do not belong to the exact three accounts.")
        for key, name in NAMES.items():
            account = self.credentials["accounts"][key]
            require(account.get("username") == name and re.fullmatch(r"[0-9a-f]{64}", account.get("password", "")),
                    "A bootstrap account credential differs from its owned format.")
        self.tokens = {}
        require(isinstance(self.state.get("tokens", []), list) and all(isinstance(entry, dict) and
                entry.get("account") in NAMES and isinstance(entry.get("token"), str) and entry["token"] and
                type(entry.get("revoked")) is bool for entry in self.state.get("tokens", [])),
                "The private bootstrap token ledger is malformed.")
        self.deadline = time.monotonic() + 240
        self.count = 0

    def persist(self):
        update(BOOTSTRAP, self.state)

    def request(self, label, method, route, *, account="admin", token="", body=None, form=False, readiness=False):
        require(self.count < 200 and time.monotonic() < self.deadline, "The bootstrap request budget expired.")
        same_service(self.owner)
        require(os.readlink("/proc/self/ns/net") == self.owner["serviceIdentity"]["networkNamespace"],
                "HTTP cannot run outside the exact new service namespace.")
        self.approve(method, route, body)
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="Goby Client Fixture Bootstrap", '
                   'Device="Linux", DeviceId="goby-m3e-reference-bootstrap-' + account + '", Version="1.0"'}
        if token:
            require(token in self.tokens.values() or any(entry["token"] == token for entry in self.state.get("tokens", [])),
                    "An unowned bootstrap token was supplied.")
            headers["X-Emby-Token"] = token
        payload = None
        if body is not None:
            payload = (urlencode(body) if form else json.dumps(body, separators=(",", ":"))).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded" if form else "application/json"
        # Reserve a durable sequence before dispatch. Interrupted requests may
        # leave gaps, but never overwrite prior response evidence on retry.
        number = self.state.get("requestCount", 0) + 1
        self.state["requestCount"] = number
        self.persist()
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=8)
        status, value, failure = None, None, None
        try:
            connection.request(method, route, body=payload, headers=headers)
            response = connection.getresponse()
            status = response.status
            raw = response.read(MAX_BODY + 1)
            require(len(raw) <= MAX_BODY and response.read(1) == b"", "The reference response exceeds its size bound.")
            try:
                value = json.loads(raw) if raw else None
            except (ValueError, UnicodeDecodeError):
                value = raw.decode("utf-8", errors="replace")
        except (OSError, http.client.HTTPException) as error:
            failure = type(error).__name__
        finally:
            connection.close()
        self.count += 1
        if isinstance(value, dict) and value.get("AccessToken"):
            self.tokens[account] = value["AccessToken"]
            ledger = self.state.setdefault("tokens", [])
            previous = [entry for entry in ledger if entry["token"] == value["AccessToken"]]
            require(not previous or all(entry["account"] == account for entry in previous),
                    "A bootstrap token unexpectedly identifies more than one account.")
            if previous:
                previous[0]["revoked"] = False
            else:
                ledger.append({"account": account, "token": value["AccessToken"], "revoked": False})
            self.state.setdefault("logoutVerified", {})[account] = False
            self.persist()
        save(HTTP_LOG / f"{number:04d}-{label}.json", {"method": method, "route": route, "headers": headers,
             "body": body, "status": status, "response": value, "failureType": failure})
        require(readiness or failure is None, "Reference HTTP did not complete; inspect the private request record.")
        return status, value

    def approve(self, method, route, body):
        parsed = urlsplit(route)
        require(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.path.startswith("/emby/"),
                "The request does not target the owned reference API.")
        static = {"GET": {"/emby/System/Info/Public", "/emby/Startup/User", "/emby/Users", "/emby/Sessions",
                           "/emby/System/Configuration", "/emby/Library/VirtualFolders/Query"},
                  "POST": {"/emby/Startup/User", "/emby/Startup/RemoteAccess", "/emby/Startup/Complete",
                            "/emby/Users/AuthenticateByName", "/emby/Users/New", "/emby/Sessions/Logout",
                            "/emby/Library/VirtualFolders", "/emby/Library/Refresh"}}
        allowed = parsed.path in static.get(method, set())
        owned_ids = {value for value in self.state.get("userIds", {}).values()
                     if isinstance(value, str) and re.fullmatch(r"[0-9a-f]{32}", value)}
        for user_id in owned_ids:
            allowed = allowed or (method == "GET" and parsed.path in
                                   {f"/emby/Users/{user_id}", f"/emby/Users/{user_id}/Views", f"/emby/Users/{user_id}/Items"})
            allowed = allowed or (method == "POST" and parsed.path in
                                   {f"/emby/Users/{user_id}/Password", f"/emby/Users/{user_id}/Policy"})
        require(allowed, "The request is outside the exact bootstrap route allowlist.")
        if method != "GET":
            require(self.state.get("serverId"), "Mutation requires an attested fresh public server identity.")
        if parsed.path == "/emby/Users/New":
            require(body in ({"Name": NAMES["viewer"]}, {"Name": NAMES["viewer2"]}), "An unowned account cannot be created.")
        if method == "POST" and parsed.path == "/emby/Startup/User":
            admin = self.credentials["accounts"]["admin"]
            require(body == {"Name": admin["username"], "Password": admin["password"]}, "The startup account differs from persisted credentials.")
        if method == "POST" and parsed.path == "/emby/Startup/RemoteAccess":
            require(body == {"EnableAutomaticPortMapping": "false"}, "Automatic remote port mapping cannot be enabled.")
        if method == "POST" and parsed.path == "/emby/Users/AuthenticateByName":
            require(body in [{"Username": value["username"], "Pw": value["password"]}
                             for value in self.credentials["accounts"].values()], "Only persisted fresh account logins are allowed.")
        if method == "POST" and parsed.path.endswith("/Password"):
            key = next((key for key, user_id in self.state.get("userIds", {}).items()
                        if parsed.path == f"/emby/Users/{user_id}/Password" and key in ("viewer", "viewer2")), None)
            require(key is not None and body == {"Id": self.state["userIds"][key],
                    "NewPw": self.credentials["accounts"][key]["password"], "ResetPassword": False},
                    "Only the persisted fresh viewer password can be configured.")
        if parsed.path == "/emby/Library/VirtualFolders" and method == "POST":
            require(body in [library_body(name) for name in LIBRARIES], "A library mutation differs from its fixed safe template.")
        if method == "POST" and parsed.path.endswith("/Policy"):
            require(isinstance(body, dict) and body.get("IsAdministrator") is False and body.get("IsDisabled") is False and
                    body.get("EnableAllFolders") is True and body.get("EnableMediaPlayback") is True,
                    "A viewer policy cannot elevate privileges or remove intended library access.")

    def login(self, key):
        credential = self.credentials["accounts"][key]
        status, result = self.request(key + "-login", "POST", "/emby/Users/AuthenticateByName", account=key,
                                      body={"Username": credential["username"], "Pw": credential["password"]})
        require(status == 200 and isinstance(result, dict) and key in self.tokens and
                result.get("ServerId") == self.state["serverId"] and result.get("User", {}).get("Name") == NAMES[key] and
                result["User"].get("Policy", {}).get("IsAdministrator") is (key == "admin"),
                "A fresh account login or privilege check failed.")
        user_id = result["User"].get("Id")
        require(isinstance(user_id, str) and re.fullmatch(r"[0-9a-f]{32}", user_id) and
                self.state.get("userIds", {}).get(key, user_id) == user_id, "An account ID changed.")
        self.state.setdefault("userIds", {})[key] = user_id
        self.persist()
        return result["User"]

    def logout(self, key):
        token = self.tokens[key]
        self.revoke(key, token)

    def revoke(self, key, token):
        status, _ = self.request(key + "-logout", "POST", "/emby/Sessions/Logout", token=token, account=key)
        require(status in (204, 401), "Bootstrap logout did not acknowledge revocation or an already invalid token.")
        status, _ = self.request(key + "-logout-check", "GET", "/emby/Sessions", token=token, account=key)
        require(status == 401, "The logged-out bootstrap token remains usable.")
        for entry in self.state.get("tokens", []):
            if entry["token"] == token:
                entry["revoked"] = True
        self.state.setdefault("logoutVerified", {})[key] = all(entry["revoked"] for entry in self.state.get("tokens", []) if entry["account"] == key)
        self.persist()

    def capture(self):
        deadline = time.monotonic() + 90
        while True:
            status, public = self.request("public", "GET", "/emby/System/Info/Public", readiness=True)
            if status == 200:
                break
            require(time.monotonic() < deadline, "The fresh reference readiness deadline expired.")
            time.sleep(1)
        require(isinstance(public, dict) and public.get("Version") == "4.9.5.0" and public.get("ServerName") == SERVER_NAME and
                isinstance(public.get("Id"), str) and public["Id"] and self.state.get("serverId", public["Id"]) == public["Id"],
                "The new reference public identity differs.")
        self.state["serverId"] = public["Id"]
        self.persist()
        if not self.state.get("startupCompleted") and self.state.get("startupCompletionDispatched"):
            self.login("admin")
            status, recovered = self.request("startup-recovery-check", "GET", "/emby/System/Configuration", token=self.tokens["admin"])
            require(status == 200 and isinstance(recovered, dict) and recovered.get("IsStartupWizardCompleted") is True,
                    "An interrupted startup completion cannot be proven; the wizard will not be replayed.")
            self.state["startupCompleted"] = True
            self.persist()
        if not self.state.get("startupCompleted"):
            status, startup = self.request("startup-get", "GET", "/emby/Startup/User")
            require(status == 200 and isinstance(startup, dict) and isinstance(startup.get("Name"), str),
                    "The fresh startup account is unavailable.")
            admin = self.credentials["accounts"]["admin"]
            status, result = self.request("startup-user", "POST", "/emby/Startup/User", form=True,
                                           body={"Name": admin["username"], "Password": admin["password"]})
            require(status == 200 and result == {}, "Fresh startup user creation failed.")
            status, _ = self.request("startup-remote", "POST", "/emby/Startup/RemoteAccess", form=True,
                                      body={"EnableAutomaticPortMapping": "false"})
            require(status == 204, "Fresh remote access setup failed.")
            self.state["startupCompletionDispatched"] = True
            self.persist()
            status, _ = self.request("startup-complete", "POST", "/emby/Startup/Complete")
            require(status == 204, "Fresh startup completion failed.")
            self.state["startupCompleted"] = True
            self.persist()
        if "admin" not in self.tokens:
            self.login("admin")
        admin_token = self.tokens["admin"]
        status, users = self.request("users-before", "GET", "/emby/Users", token=admin_token)
        require(status == 200 and isinstance(users, list) and len(users) <= 3 and
                all(row.get("Name") in NAMES.values() for row in users), "The fresh fixture contains an unknown account.")
        for key in ("viewer", "viewer2"):
            user = next((row for row in users if row.get("Name") == NAMES[key]), None)
            if user is None:
                status, user = self.request(key + "-create", "POST", "/emby/Users/New", token=admin_token, body={"Name": NAMES[key]})
                require(status == 200 and isinstance(user, dict) and user.get("Name") == NAMES[key], "Fresh viewer creation failed.")
            require(isinstance(user.get("Id"), str) and re.fullmatch(r"[0-9a-f]{32}", user["Id"]) and user.get("Policy", {}).get("IsAdministrator") is False,
                    "The new viewer is not an owned ordinary account.")
            user_id = user["Id"]
            require(self.state.get("userIds", {}).get(key, user_id) == user_id, "A recorded viewer ID changed.")
            self.state.setdefault("userIds", {})[key] = user_id
            self.persist()
            status, _ = self.request(key + "-password", "POST", f"/emby/Users/{user_id}/Password", token=admin_token,
                                      body={"Id": user_id, "NewPw": self.credentials["accounts"][key]["password"], "ResetPassword": False})
            require(status in (200, 204), "The persisted fresh viewer password could not be configured.")
            policy = dict(user["Policy"], IsAdministrator=False, IsDisabled=False, EnableAllFolders=True, EnabledFolders=[],
                          EnableMediaPlayback=True, EnableAudioPlaybackTranscoding=True, EnableVideoPlaybackTranscoding=True,
                          EnablePlaybackRemuxing=True, EnableContentDeletion=False, EnableContentDownloading=False)
            status, _ = self.request(key + "-policy", "POST", f"/emby/Users/{user_id}/Policy", token=admin_token, body=policy)
            require(status in (200, 204), "The ordinary viewer playback policy was not accepted.")
        status, libraries = self.request("libraries-before", "GET", "/emby/Library/VirtualFolders/Query", token=admin_token)
        require(status == 200 and isinstance(libraries, dict) and isinstance(libraries.get("Items"), list) and
                len(libraries["Items"]) <= 3 and all(row.get("Name") in {"M3e Reference " + name for name in LIBRARIES}
                                                   for row in libraries["Items"]), "The fixture contains an unknown library.")
        for name in LIBRARIES:
            existing = [row for row in libraries["Items"] if row.get("Name") == "M3e Reference " + name]
            require(len(existing) <= 1, "The fixture contains duplicate libraries.")
            if existing:
                verify_library(existing[0], name)
            else:
                status, _ = self.request("library-" + name.lower(), "POST", "/emby/Library/VirtualFolders", token=admin_token,
                                          body=library_body(name))
                require(status in (200, 204), "A fresh reference library could not be added.")
        status, _ = self.request("library-refresh", "POST", "/emby/Library/Refresh", token=admin_token)
        require(status in (200, 204), "The owned fixture library refresh was not accepted.")
        user_id = self.state["userIds"]["admin"]
        query = "?Recursive=true&Fields=Path,MediaSources,MediaStreams,Overview,ProviderIds&Limit=100"
        scan_deadline = time.monotonic() + 90
        while True:
            status, result = self.request("scan-items", "GET", f"/emby/Users/{user_id}/Items" + query, token=admin_token)
            require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list), "The owned scan result is invalid.")
            items = result["Items"]
            leaves = [row for row in items if row.get("Type") in ("Movie", "Episode", "Audio")]
            counts = {kind: sum(row.get("Type") == kind for row in leaves) for kind in ("Movie", "Episode", "Audio")}
            if counts == {"Movie": 1, "Episode": 3, "Audio": 2} and all(row.get("MediaSources") for row in leaves):
                break
            require(time.monotonic() < scan_deadline, "The fresh media scan did not discover all six playable fixtures.")
            time.sleep(1)
        expected_paths = {str(MEDIA / name) for name in self.owner["media"]["files"] if Path(name).suffix.lower() in (".mp4", ".mp3", ".flac")}
        require({row.get("Path") for row in leaves} == expected_paths and len({row.get("Id") for row in items}) == len(items),
                "The scanned reference media IDs or source paths differ from the owned fixture.")
        for row in leaves:
            streams = [stream for source in row["MediaSources"] for stream in source.get("MediaStreams", [])]
            stream_types = {stream.get("Type") for stream in streams}
            require("Audio" in stream_types and (row["Type"] == "Audio" or "Video" in stream_types),
                    "A reference playable item lacks its expected probed audio or video streams.")
        status, libraries = self.request("libraries-final", "GET", "/emby/Library/VirtualFolders/Query", token=admin_token)
        require(status == 200 and len(libraries.get("Items", [])) == 3, "The reference does not contain exactly three libraries.")
        for name in LIBRARIES:
            rows = [row for row in libraries["Items"] if row.get("Name") == "M3e Reference " + name]
            require(len(rows) == 1, "The final library names differ.")
            verify_library(rows[0], name)
        viewers = {}
        for key in ("viewer", "viewer2"):
            user = self.login(key)
            token = self.tokens[key]
            status, views = self.request(key + "-views", "GET", f"/emby/Users/{user['Id']}/Views", token=token, account=key)
            require(status == 200 and isinstance(views, dict) and len(views.get("Items", [])) == 3 and
                    {row.get("CollectionType") for row in views["Items"]} == set(LIBRARIES.values()),
                    "An ordinary viewer cannot read all three reference libraries.")
            status, visible = self.request(key + "-items", "GET", f"/emby/Users/{user['Id']}/Items" + query, token=token, account=key)
            require(status == 200 and {row.get("Id") for row in visible.get("Items", []) if row.get("Type") in ("Movie", "Episode", "Audio")} ==
                    {row["Id"] for row in leaves}, "An ordinary viewer cannot read every owned playable item.")
            viewers[key] = {"userId": user["Id"], "views": views["Items"], "playableCount": len(leaves)}
            self.logout(key)
        status, config = self.request("configuration-final", "GET", "/emby/System/Configuration", token=admin_token)
        require(status == 200 and isinstance(config, dict) and config.get("ServerName") == SERVER_NAME and
                config.get("HttpServerPortNumber") == PORT and config.get("PublicPort") == PORT and
                config.get("LocalNetworkAddresses") == ["127.0.0.1"] and config.get("IsStartupWizardCompleted") is True and
                all(config.get(name) is False for name in ("EnableHttps", "EnableUPnP", "EnableRemoteAccess", "EnableAutoUpdate",
                                                           "EnableAutomaticRestart", "AutoRunWebApp")),
                "The final reference startup or network configuration differs.")
        self.logout("admin")
        for entry in self.state.get("tokens", []):
            if not entry["revoked"]:
                self.revoke(entry["account"], entry["token"])
        require(self.state.get("logoutVerified") == {key: True for key in NAMES} and
                all(entry["revoked"] for entry in self.state.get("tokens", [])),
                "A previously confirmed bootstrap token has not been revoked.")
        accounts = {key: dict(value, userId=self.state["userIds"][key]) for key, value in self.credentials["accounts"].items()}
        browser = {"schemaVersion": 1, "marker": MARKER, "unit": UNIT, "port": PORT, "serverId": self.state["serverId"],
                   **accounts["viewer"], "accounts": accounts}
        report = {"schemaVersion": 1, "marker": MARKER, "serverId": self.state["serverId"], "version": "4.9.5.0",
                  "serviceIdentity": self.owner["serviceIdentity"], "libraries": libraries["Items"], "items": items,
                  "viewers": viewers, "bootstrapLogoutVerified": self.state["logoutVerified"], "requestCount": self.state["requestCount"],
                  "confirmedBootstrapTokenCount": len(self.state.get("tokens", [])),
                  "media": self.owner["media"], "oldReferenceProcessUnchanged": True, "oldReferenceDataAccessed": False,
                  "internetProvidersDisabled": True, "mediaReadOnly": True}
        for path, value in ((BROWSER, browser), (REPORT, report)):
            if present(path):
                require(read_private(path) == value, "A completed reference output differs; refusing replacement.")
            else:
                save(path, value)


def create_configuration():
    save(DATA / "config/system.xml", f'''<?xml version="1.0" encoding="utf-8"?>
<ServerConfiguration xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <HttpServerPortNumber>{PORT}</HttpServerPortNumber><PublicPort>{PORT}</PublicPort>
  <HttpsPortNumber>18497</HttpsPortNumber><PublicHttpsPort>18497</PublicHttpsPort>
  <EnableHttps>false</EnableHttps><EnableUPnP>false</EnableUPnP><EnableRemoteAccess>false</EnableRemoteAccess>
  <EnableAutoUpdate>false</EnableAutoUpdate><EnableAutomaticRestart>false</EnableAutomaticRestart>
  <AutoRunWebApp>false</AutoRunWebApp><IsStartupWizardCompleted>false</IsStartupWizardCompleted>
  <EnableExternalContentInSuggestions>false</EnableExternalContentInSuggestions>
  <ServerName>{SERVER_NAME}</ServerName><LocalNetworkAddresses><string>127.0.0.1</string></LocalNetworkAddresses>
  <PreferredMetadataLanguage>en</PreferredMetadataLanguage><MetadataCountryCode>US</MetadataCountryCode><UICulture>en-US</UICulture>
  <DatabaseCacheSizeMB>64</DatabaseCacheSizeMB><LogFileRetentionDays>2</LogFileRetentionDays>
</ServerConfiguration>
''')
    launch = f'''#!/usr/bin/env bash
set -euo pipefail
APP_DIR={APP}
EMBY_DATA={DATA}
export EMBY_DATA
export AMDGPU_IDS="$APP_DIR/extra/share/libdrm/amdgpu.ids"
export FONTCONFIG_PATH="$APP_DIR/etc/fonts"
export LD_LIBRARY_PATH="$APP_DIR/lib:$APP_DIR/extra/lib"
export LIBVA_DRIVERS_PATH="$APP_DIR/extra/lib/dri"
export OCL_ICD_VENDORS="$APP_DIR/extra/etc/OpenCL/vendors"
export PATH="$APP_DIR/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PCI_IDS_PATH="$APP_DIR/share/hwdata/pci.ids"
export SSL_CERT_FILE="$APP_DIR/etc/ssl/certs/ca-certificates.crt"
export XDG_CACHE_HOME="$EMBY_DATA/cache"
export NEOReadDebugKeys=1
export OverrideGpuAddressSpace=48
cd "$APP_DIR"
exec "$APP_DIR/system/EmbyServer" -programdata "$EMBY_DATA" -ffdetect "$APP_DIR/bin/ffdetect" -ffmpeg "$APP_DIR/bin/ffmpeg" -ffprobe "$APP_DIR/bin/ffprobe" -restartexitcode 3 -updatepackage 'emby-server-deb_{{version}}_amd64.deb'
'''
    save(LAUNCHER, launch, mode=0o700)
    save(DATA / "service.log", "")


def start_service():
    require(properties(UNIT).get("LoadState") == "not-found", "The new reference unit is occupied; refusing reuse.")
    settings = {"Description": DESCRIPTION, "PrivateNetwork": "yes", "PrivateTmp": "yes", "NoNewPrivileges": "yes",
                "ProtectSystem": "strict", "ProtectHome": "yes", "ReadWritePaths": str(DATA),
                "ReadOnlyPaths": " ".join((str(WORK), str(APP), str(MEDIA))), "CapabilityBoundingSet": "",
                "CPUQuota": "150%", "MemoryMax": "1G", "TasksMax": "256", "LimitNOFILE": "65536",
                "TimeoutStopSec": "25", "KillMode": "control-group", "WorkingDirectory": str(DATA), "UMask": "0077",
                "StandardOutput": "append:" + str(DATA / "service.log"), "StandardError": "append:" + str(DATA / "service.log")}
    run(["/usr/bin/systemd-run", "--unit=" + UNIT, "--collect",
         *["--property=" + key + "=" + value for key, value in settings.items()], LAUNCHER])


def bootstrap_child(owner):
    same_service(owner)
    descriptor = os.open(f"/proc/{owner['serviceIdentity']['pid']}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(descriptor).st_ino == int(owner["serviceIdentity"]["networkNamespace"].removeprefix("net:[").removesuffix("]")),
                "The fixed namespace handle differs from the service identity.")
        same_service(owner)
        # Only the network namespace is entered; fixed host-owned evidence and
        # source paths remain visible. No credential is present in argv.
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(descriptor), "/usr/bin/python3", "-B",
                                 str(Path(__file__).absolute()), "_bootstrap"], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=270, check=False, pass_fds=(descriptor,), env=dict(BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "The private reference bootstrap failed; inspect its retained HTTP records.")
    finally:
        os.close(descriptor)


def completed_outputs(owner):
    """Validate complete private outputs before committing or recovering READY."""
    report = read_private(REPORT)
    browser = read_private(BROWSER)
    state = read_private(BOOTSTRAP)
    credentials = read_private(CREDENTIALS)
    require(report.get("marker") == browser.get("marker") == state.get("marker") == credentials.get("marker") == MARKER and
            report.get("serverId") == browser.get("serverId") == state.get("serverId") and state.get("serverId") and
            report.get("serviceIdentity") == owner["serviceIdentity"] and report.get("media") == owner["media"] and
            report.get("bootstrapLogoutVerified") == state.get("logoutVerified") == {key: True for key in NAMES} and
            len(state.get("tokens", [])) >= 3 and all(entry.get("revoked") is True for entry in state["tokens"]) and
            report.get("confirmedBootstrapTokenCount") == len(state["tokens"]),
            "The completed reference outputs do not match the owned service, media, or revoked bootstrap credentials.")
    accounts = {key: dict(value, userId=state["userIds"][key]) for key, value in credentials["accounts"].items()}
    require(browser == {"schemaVersion": 1, "marker": MARKER, "unit": UNIT, "port": PORT, "serverId": state["serverId"],
                        **accounts["viewer"], "accounts": accounts}, "The private browser accounts differ from their persisted intent.")
    return report


def main(arguments=None):
    arguments = sys.argv[1:] if arguments is None else arguments
    require(arguments in ([], ["--check"], ["_bootstrap"]), "Usage: prepare-client-reference.py [--check]")
    preconditions()
    os.umask(0o077)
    if arguments == ["_bootstrap"]:
        owner = read_private(OWNER)
        validate_owner(owner)
        require(owner.get("phase") == "service-started", "Bootstrap is allowed only for the owned unfinished fixture.")
        same_service(owner)
        API(owner).capture()
        return
    fresh = not present(OWNER)
    if fresh:
        require(not arguments, "No owned reference fixture has been prepared.")
        require(all(not present(path) for path in (DATA, LOCK, CREDENTIALS, BROWSER, BOOTSTRAP, REPORT, HTTP_LOG, LAUNCHER)),
                "A reference fixture path exists without ownership; refusing adoption.")
        require(properties(UNIT).get("LoadState") == "not-found", "The reference unit is already occupied.")
        media = verify_media()
        old = protected_reference()
    else:
        canonical(LOCK, mode=0o600)
    descriptor = os.open(LOCK, os.O_RDWR | os.O_NOFOLLOW | (os.O_CREAT | os.O_EXCL if fresh else 0), 0o600)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ReferenceError("Another reference fixture operator is running.") from None
        if fresh:
            owner = {"marker": MARKER, "unit": UNIT, "data": str(DATA), "binary": str(BINARY), "binarySha256": BINARY_SHA256,
                     "port": PORT, "phase": "preparing", "media": media, "protectedReference": old, "workIdentity": work_identity()}
            save(OWNER, owner)
            save(CREDENTIALS, {"schemaVersion": 1, "marker": MARKER,
                               "accounts": {key: {"username": name, "password": secrets.token_hex(32)} for key, name in NAMES.items()}})
            save(BOOTSTRAP, {"marker": MARKER, "requestCount": 0, "userIds": {}})
            HTTP_LOG.mkdir(mode=0o700)
            DATA.mkdir(mode=0o700)
            save(DATA / "OWNER.json", {"marker": MARKER, "path": str(DATA)})
            (DATA / "config").mkdir(mode=0o700)
            create_configuration()
            owner.update(dataIdentity=data_identity(), launcherSha256=digest(LAUNCHER))
            update(OWNER, owner)
            require(protected_reference() == old and verify_media() == media, "Inputs changed before reference service dispatch.")
            start_service()
            deadline = time.monotonic() + 20
            while True:
                try:
                    identity = service_identity(owner)
                    break
                except (ReferenceError, FileNotFoundError):
                    require(time.monotonic() < deadline, "The new service did not establish its exact process identity.")
                    time.sleep(0.25)
            owner.update(serviceIdentity=identity, phase="service-started")
            update(OWNER, owner)
        else:
            owner = read_private(OWNER)
            validate_owner(owner)
            require(owner.get("phase") in ("service-started", "ready"),
                    "Reference initialization is incomplete; retain data and inspect before continuing.")
            same_service(owner)
            require(verify_media() == owner["media"], "The original fixture media changed.")
        if owner["phase"] != "ready":
            require(not arguments, "The reference bootstrap is incomplete.")
            if not (present(REPORT) and present(BROWSER)):
                bootstrap_child(owner)
            same_service(owner)
            require(verify_media() == owner["media"], "The reference scan changed its read-only media.")
            report = completed_outputs(owner)
            owner.update(phase="ready", serverId=report["serverId"], reportSha256=digest(REPORT), browserSha256=digest(BROWSER))
            update(OWNER, owner)
        require(digest(REPORT) == owner["reportSha256"] and digest(BROWSER) == owner["browserSha256"],
                "Completed reference evidence or private browser credentials changed.")
        print(json.dumps({"state": "ready", "unit": UNIT, "pid": owner["serviceIdentity"]["pid"],
                          "startTicks": owner["serviceIdentity"]["startTicks"], "networkNamespace": owner["serviceIdentity"]["networkNamespace"],
                          "port": PORT, "serverId": owner["serverId"], "browserFile": str(BROWSER), "reportFile": str(REPORT),
                          "oldReferenceUnchanged": True, "mediaReadOnlyVerified": True, "bootstrapTokensRevoked": True}), flush=True)
    finally:
        os.close(descriptor)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Neither exception values nor captured HTTP responses are printed.
        print(json.dumps({"result": "failed", "failureType": type(error).__name__, "evidenceRetained": True}), file=sys.stderr)
        sys.exit(1)
