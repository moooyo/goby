#!/usr/bin/env python3
"""Verify deployed original playback only through SSH on Linux test-env.

Run as root after deploying Goby. Credentials are read from browser.env and
never printed. The reusable account, library, and synthetic media are bound to
an ownership marker and a private state file. Only that account's fixture state
is reset. No existing library, reference media, database, or service is changed.
The final JSON is sanitized; exception bodies, tokens, and URLs are not emitted.
"""

from __future__ import annotations

from datetime import datetime
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import re
import secrets
import shlex
import stat
import subprocess
import sys
import tempfile
import time
from urllib.parse import parse_qs, quote, urlencode, urlsplit, urlunsplit


ENV_FILE = Path("/opt/goby-test/browser.env")
FIXTURE_ROOT = Path("/opt/goby-fixtures")
DIRECTORY = FIXTURE_ROOT / "direct-playback"
FFMPEG = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
ORIGIN = "http://127.0.0.1:18096"
OWNER = "goby-direct-playback-v1"
MARKER_NAME = ".goby-direct-playback-owned.json"
STATE_NAME = ".goby-direct-playback-state.json"
LOCK_NAME = ".goby-direct-playback.lock"
MEDIA_NAME = "Goby Direct Playback 600 Seconds.mp4"
PENDING_NAME = ".goby-direct-playback-pending.mp4"
DEVICE_ID = "goby-direct-playback-verifier"
TICKS = 10_000_000
POSITION = 120 * TICKS
MAX_MEDIA = 16 * 1024 * 1024


class VerificationFailure(Exception):
    """Contain only a fixed assertion label that is safe to print."""


def check(condition: bool, label: str) -> None:
    if not condition:
        raise VerificationFailure(label)


def private_file(path: Path) -> None:
    info = path.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and
          stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
          "A private state or credential file has unsafe ownership or permissions")


def bounded_json(path: Path) -> dict:
    private_file(path)
    check(path.stat().st_size <= 16_384, "A private state file exceeds its size limit")
    value = json.loads(path.read_text(encoding="utf-8"))
    check(isinstance(value, dict), "Private state must be a JSON object")
    return value


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(131_072), b""):
            result.update(chunk)
    return result.hexdigest()


def credentials() -> dict[str, str]:
    private_file(ENV_FILE)
    check(ENV_FILE.stat().st_size <= 65_536, "Credential file exceeds its size limit")
    wanted = {"GOBY_SMOKE_NAME", "GOBY_SMOKE_PASSWORD"}
    values = {}
    for line in ENV_FILE.read_text(encoding="utf-8").splitlines():
        key, separator, raw = line.strip().removeprefix("export ").partition("=")
        if separator and key in wanted:
            parts = shlex.split(raw, comments=False, posix=True)
            check(len(parts) == 1, "Credential file contains an unsupported assignment")
            values[key] = parts[0]
    check(all(values.get(key) for key in wanted), "Required synthetic credentials are missing")
    return values


class API:
    def __init__(self) -> None:
        self.cookie = ""
        self.csrf = ""
        self.token = ""
        self.user_id = ""
        self.session_id = ""

    def request(self, method: str, path: str, *, label: str, body=None,
                admin=False, emby=False, headers=None, expected=(200,),
                parse=True, limit=2 * 1024 * 1024):
        check(path.startswith("/") and not path.startswith("//") and
              "\r" not in path and "\n" not in path, "HTTP target must be a local relative route")
        fields = {"Accept": "application/json", "Origin": ORIGIN}
        if admin:
            if self.cookie:
                fields["Cookie"] = self.cookie
            if method not in {"GET", "HEAD"} and self.csrf:
                fields["X-CSRF-Token"] = self.csrf
        if emby:
            fields["Authorization"] = (
                'Emby Client="Goby Direct Playback Verification", '
                f'DeviceId="{DEVICE_ID}", Device="Linux Test", Version="0.1.0"'
            )
            if self.token:
                fields["X-Emby-Token"] = self.token
        fields.update(headers or {})
        payload = None if body is None else json.dumps(body).encode("utf-8")
        if payload is not None:
            fields["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection("127.0.0.1", 18096, timeout=15)
        try:
            connection.request(method, path, payload, fields)
            response = connection.getresponse()
            content = response.read(limit + 1)
            check(len(content) <= limit, label + ": response exceeded its size limit")
            check(response.status in expected, label + f": unexpected HTTP {response.status}")
            metadata = {key.lower(): value for key, value in response.getheaders()}
            if admin and method == "POST" and path == "/admin/v1/session":
                cookies = SimpleCookie()
                cookies.load(response.getheader("Set-Cookie", ""))
                check("goby_session" in cookies, "Administrator login did not return a session cookie")
                self.cookie = "goby_session=" + cookies["goby_session"].value
            if response.status == 204:
                check(not content, label + ": HTTP 204 included a response body")
            if parse:
                return json.loads(content) if content else None
            return metadata, content
        except (OSError, http.client.HTTPException, ValueError):
            raise VerificationFailure(label + ": request or response decoding failed") from None
        finally:
            connection.close()

    def admin_login(self, values: dict[str, str]) -> None:
        result = self.request("POST", "/admin/v1/session", label="Administrator login", admin=True,
                              body={"Name": values["GOBY_SMOKE_NAME"],
                                    "Password": values["GOBY_SMOKE_PASSWORD"]})
        self.csrf = result.get("CSRFToken", "")
        check(bool(self.csrf), "Administrator login omitted CSRF protection")

    def viewer_login(self, state: dict) -> None:
        result = self.request("POST", "/emby/Users/AuthenticateByName", emby=True,
                              label="Owned viewer login", body={"Username": state["user_name"],
                                                                "Pw": state["user_password"]})
        self.token = result.get("AccessToken", "")
        self.user_id = result.get("User", {}).get("Id", "")
        self.session_id = result.get("SessionInfo", {}).get("Id", "")
        check(bool(self.token and self.user_id and self.session_id), "Owned viewer login omitted session fields")
        check(self.user_id == state["user_id"], "Owned viewer login returned a different account")
        policy = result.get("User", {}).get("Policy", {})
        check(policy.get("IsAdministrator") is False and policy.get("IsDisabled") is False,
              "Owned viewer must be an enabled non-administrator")

    def wait_job(self, job_id: str, timeout=45) -> dict:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            jobs = self.request("GET", "/admin/v1/jobs", admin=True, label="Owned scan polling")["Items"]
            job = next((entry for entry in jobs if entry.get("Id") == job_id), None)
            check(job is not None, "Owned scan job was not returned")
            if job.get("Status") not in {"pending", "running"}:
                return job
            time.sleep(0.25)
        raise VerificationFailure("Owned scan exceeded its time limit")


class OwnedFixture:
    def __init__(self) -> None:
        self.state: dict = {}
        self.lock = None

    def open(self) -> None:
        import fcntl

        check(not FIXTURE_ROOT.is_symlink() and FIXTURE_ROOT.resolve(strict=True) == FIXTURE_ROOT,
              "Synthetic fixture root must be the expected real directory")
        root_info = FIXTURE_ROOT.lstat()
        check(stat.S_ISDIR(root_info.st_mode) and root_info.st_uid == 0 and root_info.st_mode & 0o022 == 0,
              "Synthetic fixture root has unsafe ownership or permissions")
        root_marker = FIXTURE_ROOT / ".goby-managed"
        info = root_marker.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and
              root_marker.read_text(encoding="utf-8").strip() == "goby-generated-media-fixtures",
              "Shared synthetic fixture ownership marker does not match")
        if not DIRECTORY.exists() and not DIRECTORY.is_symlink():
            DIRECTORY.mkdir(mode=0o755)
            DIRECTORY.chmod(0o755)
            marker = {"owner": OWNER, "nonce": secrets.token_hex(16)}
            self.write_private(MARKER_NAME, marker, create=True)
        info = DIRECTORY.lstat()
        check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and
              DIRECTORY.resolve(strict=True) == DIRECTORY,
              "Direct-playback fixture directory ownership does not match")
        marker = bounded_json(DIRECTORY / MARKER_NAME)
        check(marker.get("owner") == OWNER and
              re.fullmatch(r"[0-9a-f]{32}", str(marker.get("nonce", ""))) is not None,
              "Direct-playback fixture marker does not match")
        allowed = {MARKER_NAME, STATE_NAME, LOCK_NAME, MEDIA_NAME, PENDING_NAME}
        check(all(entry.name in allowed for entry in DIRECTORY.iterdir()),
              "Unexpected entries exist in the owned fixture directory")
        lock_path = DIRECTORY / LOCK_NAME
        if lock_path.exists() or lock_path.is_symlink():
            private_file(lock_path)
        descriptor = os.open(lock_path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
        self.lock = os.fdopen(descriptor, "r+")
        try:
            fcntl.flock(self.lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise VerificationFailure("Another direct-playback verification is already active") from None
        if (DIRECTORY / STATE_NAME).exists() or (DIRECTORY / STATE_NAME).is_symlink():
            self.state = bounded_json(DIRECTORY / STATE_NAME)
            check(self.state.get("owner") == OWNER and self.state.get("nonce") == marker["nonce"],
                  "Private state is not bound to the owned fixture marker")
        else:
            suffix = marker["nonce"][:16]
            self.state = {**marker, "user_name": "Goby Direct Playback " + suffix,
                          "user_password": secrets.token_urlsafe(32),
                          "library_name": "Goby Direct Playback " + suffix,
                          "user_id": "", "library_id": "", "media_sha256": ""}
            self.write_private(STATE_NAME, self.state, create=True)
        expected_name = "Goby Direct Playback " + marker["nonce"][:16]
        check(self.state.get("user_name") == self.state.get("library_name") == expected_name and
              isinstance(self.state.get("user_password"), str) and len(self.state["user_password"]) >= 32,
              "Owned account state is invalid")

    def write_private(self, name: str, value: dict, *, create=False) -> None:
        check(name in {MARKER_NAME, STATE_NAME}, "Private output must use an owned filename")
        target = DIRECTORY / name
        if create:
            descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
                json.dump(value, stream, sort_keys=True)
                stream.flush()
                os.fsync(stream.fileno())
            return
        private_file(target)
        descriptor, temporary = tempfile.mkstemp(prefix=".goby-state-", dir=DIRECTORY)
        try:
            with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
                json.dump(value, stream, sort_keys=True)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, target)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)

    def save(self) -> None:
        self.write_private(STATE_NAME, self.state)

    def media(self) -> Path:
        path = DIRECTORY / MEDIA_NAME
        pending = DIRECTORY / PENDING_NAME
        if not path.exists() and not path.is_symlink():
            check(not self.state.get("media_sha256"), "Previously recorded media is missing")
            if pending.exists() or pending.is_symlink():
                info = pending.lstat()
                check(self.state.get("media_pending") is True and stat.S_ISREG(info.st_mode) and
                      info.st_uid == 0 and info.st_nlink == 1,
                      "Pending media ownership does not match")
                pending.unlink()
            self.state["media_pending"] = True
            self.save()
            result = subprocess.run([
                str(FFMPEG), "-nostdin", "-hide_banner", "-loglevel", "error", "-n",
                "-f", "lavfi", "-i", "color=c=0x28586b:size=160x90:rate=1",
                "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000",
                "-t", "600", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "35",
                "-pix_fmt", "yuv420p", "-g", "30", "-threads", "1",
                "-c:a", "aac", "-b:a", "16k", "-movflags", "+faststart", "-f", "mp4", str(pending),
            ], stdin=subprocess.DEVNULL, capture_output=True, timeout=90)
            check(result.returncode == 0, "Synthetic H.264/AAC fixture generation failed")
            pending.chmod(0o644)
            os.replace(pending, path)
        info = path.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and
              info.st_mode & 0o022 == 0 and 1024 < info.st_size <= MAX_MEDIA,
              "Owned media has invalid ownership, permissions, or size")
        current_hash = digest(path)
        if not self.state.get("media_sha256"):
            check(self.state.get("media_pending") is True, "Existing media has no recorded creation intent")
            self.state["media_sha256"] = current_hash
            self.state["media_pending"] = False
            self.save()
        check(current_hash == self.state["media_sha256"], "Owned media changed since its recorded creation")
        return path

    def catalog(self, api: API) -> str:
        state = self.state
        users = api.request("GET", "/admin/v1/users", admin=True, label="Owned account lookup")["Items"]
        matching = [user for user in users if user.get("Name") == state["user_name"]]
        check(len(matching) <= 1, "Owned account lookup is ambiguous")
        if state.get("user_id"):
            check(len(matching) == 1 and matching[0].get("Id") == state["user_id"],
                  "Recorded owned account no longer matches")
        elif matching:
            check(state.get("user_creation_pending") is True, "Account exists without recorded creation intent")
            state["user_id"] = matching[0]["Id"]
        else:
            state["user_creation_pending"] = True
            self.save()
            result = api.request("POST", "/admin/v1/users", admin=True, expected=(201,),
                                 label="Owned non-administrator creation", body={"Name": state["user_name"],
                                 "Password": state["user_password"], "IsAdministrator": False})
            state["user_id"] = result["User"]["Id"]
        api.viewer_login(state)
        state["user_creation_pending"] = False
        self.save()
        libraries = api.request("GET", "/admin/v1/libraries", admin=True, label="Owned library lookup")["Items"]
        matching = [entry for entry in libraries if entry.get("Name") == state["library_name"] or
                    str(DIRECTORY) in entry.get("Paths", [])]
        check(len(matching) <= 1, "Owned library lookup is ambiguous")
        if matching:
            entry = matching[0]
            check(entry.get("Name") == state["library_name"] and entry.get("Paths") == [str(DIRECTORY)] and
                  entry.get("CollectionType") == "movies", "Existing library does not match the owned fixture")
            check((state.get("library_id") == entry.get("Id")) or
                  (not state.get("library_id") and state.get("library_creation_pending") is True),
                  "Existing library has no matching ownership record")
            state["library_id"] = entry["Id"]
        else:
            check(not state.get("library_id"), "Previously recorded owned library is missing")
            state["library_creation_pending"] = True
            self.save()
            result = api.request("POST", "/admin/v1/libraries", admin=True, expected=(201,),
                                 label="Owned library creation", body={"Name": state["library_name"],
                                 "CollectionType": "movies", "Paths": [str(DIRECTORY)], "Scan": False})
            state["library_id"] = result["Library"]["Id"]
        state["library_creation_pending"] = False
        self.save()
        return state["library_id"]


def detail(api: API, item_id: str, *, alias=False) -> dict:
    prefix = "/uSeRs/" if alias else "/emby/Users/"
    path = prefix + quote(api.user_id) + "/Items/" + quote(item_id)
    return api.request("GET", path, emby=True, label="Owned item detail")


def resume(api: API, library_id: str, *, alias=False) -> dict:
    prefix = "/uSeRs/" if alias else "/emby/Users/"
    path = prefix + quote(api.user_id) + "/iTeMs/rEsUmE?" + urlencode({
        "ParentId": library_id, "Recursive": "true", "IncludeItemTypes": "Movie", "Limit": 10})
    return api.request("GET", path, emby=True, label="Owned resume list")


def state_is(data: dict, position: int, count: int, played: bool, favorite: bool) -> None:
    check(data.get("PlaybackPositionTicks") == position and data.get("PlayCount") == count and
          data.get("Played") is played and data.get("IsFavorite") is favorite,
          "Owned playback position, count, watched, or favorite state did not match")


def reset_state(api: API, item_id: str, library_id: str) -> None:
    for flag in ("PlayedItems", "FavoriteItems"):
        api.request("DELETE", f"/emby/Users/{quote(api.user_id)}/{flag}/{quote(item_id)}",
                    emby=True, label="Owned user state reset")
    data = detail(api, item_id)["UserData"]
    state_is(data, 0, 0, False, False)
    check(not data.get("LastPlayedDate"), "Owned user state reset retained a last-played date")
    result = resume(api, library_id)
    check(result.get("TotalRecordCount") == 0 and result.get("Items") == [],
          "Owned user state reset retained a resume entry")


def report(api: API, event: str, play_id: str, item_id: str, source_id: str, position: int) -> None:
    paths = {"Started": "/sEsSiOnS/pLaYiNg", "Progress": "/emby/Sessions/Playing/Progress",
             "Stopped": "/eMbY/sEsSiOnS/pLaYiNg/sToPpEd"}
    body = {"PlaySessionId": play_id, "ItemId": item_id, "MediaSourceId": source_id,
            "SessionId": api.session_id, "PositionTicks": position, "RunTimeTicks": 600 * TICKS,
            "PlayMethod": "DirectStream", "IsPaused": False}
    if event == "Started":
        body.update({"CanSeek": True, "IsMuted": True, "VolumeLevel": 0, "PlaybackRate": 1.25})
    api.request("POST", paths[event], emby=True, expected=(204,), parse=False,
                label="Playback " + event, body=body)


def current_session(api: API) -> dict:
    sessions = api.request("GET", "/sEsSiOnS?" + urlencode({"Id": api.session_id}),
                           emby=True, label="Current client session")
    check(isinstance(sessions, list) and len(sessions) == 1 and
          sessions[0].get("Id") == api.session_id and sessions[0].get("UserId") == api.user_id,
          "Client session query did not return only the owned authentication session")
    session = sessions[0]
    check(not any(field in session for field in ("AccessToken", "Token", "PushToken", "Capabilities", "DeviceProfile")),
          "Client session exposed a private declaration or credential")
    return session


def decode_media(content: bytes) -> dict:
    offset, moov, mdat = 0, None, None
    while offset + 8 <= len(content):
        size = int.from_bytes(content[offset:offset + 4], "big")
        kind = content[offset + 4:offset + 8]
        if size == 1:
            check(offset + 16 <= len(content), "MP4 extended atom is truncated")
            size = int.from_bytes(content[offset + 8:offset + 16], "big")
        if size == 0:
            size = len(content) - offset
        check(size >= 8 and offset + size <= len(content), "MP4 atom size is invalid")
        if kind == b"moov":
            moov = offset
        if kind == b"mdat":
            mdat = offset
        offset += size
    check(moov is not None and mdat is not None and moov < mdat, "Synthetic MP4 is missing faststart layout")
    result = subprocess.run([
        str(FFMPEG), "-nostdin", "-hide_banner", "-loglevel", "error", "-xerror",
        "-threads", "1", "-i", "pipe:0", "-map", "0:v:0", "-map", "0:a:0",
        "-progress", "pipe:1", "-nostats", "-f", "null", "-",
    ], input=content, capture_output=True, timeout=60)
    check(result.returncode == 0, "FFmpeg could not decode fetched H.264 and AAC streams")
    progress = {}
    for line in result.stdout.decode("utf-8", errors="replace").splitlines():
        key, separator, value = line.partition("=")
        if separator:
            progress[key] = value
    frames = int(progress.get("frame", "0"))
    microseconds = int(progress.get("out_time_us", "0"))
    check(progress.get("progress") == "end" and frames >= 599 and microseconds >= 599_000_000,
          "FFmpeg did not finish decoding the complete ten-minute fixture")
    return {"video_frames": frames, "decoded_seconds": microseconds / 1_000_000,
            "video_codec": "h264", "audio_codec": "aac", "input": "authenticated_http_bytes_via_pipe"}


def main() -> int:
    os.umask(0o077)
    summary = {"status": "failed", "assertions": [], "cleanup_errors": []}
    api, fixture = API(), OwnedFixture()
    item_id = library_id = play_id = source_id = ""
    stopped = False
    state_reset = False
    media_path = None
    stage = "preconditions"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
              "Run as root through SSH only on the authorized Linux test-env host")
        check(FFMPEG.is_file() and os.access(FFMPEG, os.X_OK), "The pinned FFmpeg executable is unavailable")
        fixture.open()
        values = credentials()
        stage = "owned synthetic fixture"
        media_path = fixture.media()
        media_size = media_path.stat().st_size
        api.request("GET", "/readyz", label="Deployed Goby readiness")
        api.admin_login(values)
        library_id = fixture.catalog(api)
        stage = "native library scan"
        job = api.request("POST", f"/admin/v1/libraries/{quote(library_id)}/scan", admin=True,
                          expected=(202,), label="Owned library scan")["Job"]
        job = api.wait_job(job["Id"])
        check(job.get("Status") == "completed" and not job.get("Error") and job.get("Scanned") == 1,
              "Owned library scan did not inspect exactly one clean movie")
        query = urlencode({"ParentId": library_id, "Recursive": "true", "IncludeItemTypes": "Movie",
                           "Fields": "Path,MediaSources,MediaStreams", "Limit": 10})
        listing = api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items?" + query,
                              emby=True, label="Owned movie lookup")
        items = listing.get("Items", [])
        check(listing.get("TotalRecordCount") == 1 and len(items) == 1 and
              items[0].get("Path") == str(media_path), "Owned movie lookup did not match the synthetic file")
        item_id = items[0]["Id"]
        reset_state(api, item_id, library_id)
        summary["assertions"].append({"stage": stage, "scanned_movies": 1})

        stage = "client capability report and session query"
        api.request("POST", "/emby/Sessions/Capabilities/Full?Id=stale-session-hint", emby=True,
                    expected=(204,), parse=False, label="Owned client capability report",
                    body={"PlayableMediaTypes": ["Video"], "SupportedCommands": ["Pause"],
                          "SupportsMediaControl": True, "DeviceProfile": {"Name": "Recorded Profile",
                          "DirectPlayProfiles": [{"Type": "Video", "Container": "mp4", "VideoCodec": "hevc"}]}})
        session = current_session(api)
        check(session.get("PlayableMediaTypes") == ["Video"] and session.get("SupportedCommands") == ["Pause"] and
              session.get("SupportsRemoteControl") is False and "NowPlayingItem" not in session,
              "Client capabilities or idle session projection did not match the report")
        summary["assertions"].append({"stage": stage, "capabilities": 204, "owned_session": True,
                                      "remote_control": False})

        stage = "matching PlaybackInfo and route aliases"
        profile = {"UserId": api.user_id, "IsPlayback": True, "DeviceProfile": {
            "Name": "Goby Direct Playback Verification", "DirectPlayProfiles": [{
                "Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac"}]}}
        negotiated = api.request("POST", f"/emby/Items/{quote(item_id)}/PlaybackInfo",
                                 body=profile, emby=True, label="Matching PlaybackInfo")
        play_id = negotiated.get("PlaySessionId", "")
        sources = negotiated.get("MediaSources", [])
        check(len(sources) == 1 and isinstance(sources[0], dict), "PlaybackInfo omitted its original media source")
        source = sources[0]
        source_id = source.get("Id", "")
        check(bool(play_id) and play_id != api.session_id and source_id == "mediasource_" + item_id,
              "PlaybackInfo returned invalid session or source identifiers")
        check(source.get("SupportsDirectPlay") is True and source.get("SupportsDirectStream") is True and
              source.get("SupportsTranscoding") is False and "TranscodingUrl" not in source and
              "ErrorCode" not in negotiated, "Matching profile did not advertise only supported original delivery")
        check(599 * TICKS <= source.get("RunTimeTicks", 0) <= 601 * TICKS and
              source.get("Size") == media_size and source.get("Container") == "mp4",
              "PlaybackInfo source duration, size, or container did not match real media")
        streams = source.get("MediaStreams", [])
        check(any(stream.get("Type") == "Video" and stream.get("Codec") == "h264" and
                  stream.get("Width") == 160 and stream.get("Height") == 90 for stream in streams) and
              any(stream.get("Type") == "Audio" and stream.get("Codec") == "aac" for stream in streams),
              "PlaybackInfo did not contain real H.264/AAC probe facts")
        target = source.get("DirectStreamUrl", "")
        parsed = urlsplit(target)
        check(not parsed.scheme and not parsed.netloc and not parsed.fragment and
              parsed.path == f"/videos/{quote(item_id)}/original.mp4",
              "Generated direct URL is not the expected relative original-file route")
        query = parse_qs(parsed.query, keep_blank_values=True)
        check(query.get("api_key") == [api.token] and query.get("DeviceId") == [DEVICE_ID] and
              query.get("MediaSourceId") == [source_id] and query.get("PlaySessionId") == [play_id] and
              set(query) == {"api_key", "DeviceId", "MediaSourceId", "PlaySessionId"},
              "Generated direct URL does not contain the expected authentication binding")
        reused = api.request("POST", f"/iTeMs/{quote(item_id)}/pLaYbAcKiNfO", emby=True,
                             label="Root mixed-case PlaybackInfo alias",
                             body={**profile, "CurrentPlaySessionId": play_id})
        check(reused.get("PlaySessionId") == play_id and reused.get("MediaSources") == sources,
              "Root mixed-case PlaybackInfo alias changed its source or current session")
        summary["assertions"].append({"stage": stage, "direct_play": True, "direct_stream": True,
                                      "transcoding": False, "authenticated_relative_url": True})

        stage = "authenticated original HEAD Range and decode"
        metadata, content = api.request("HEAD", target, label="Generated URL HEAD", parse=False)
        check(not content and metadata.get("content-type", "").split(";")[0] == "video/mp4" and
              metadata.get("content-length") == str(media_size) and metadata.get("accept-ranges") == "bytes" and
              bool(metadata.get("etag")), "Original HEAD metadata did not match the complete media")
        ranged_headers, ranged = api.request("GET", target, label="Generated URL byte range", parse=False,
                                            headers={"Range": "bytes=0-1023"}, expected=(206,))
        with media_path.open("rb") as media_file:
            first_bytes = media_file.read(1024)
        check(ranged == first_bytes and ranged_headers.get("content-range") == f"bytes 0-1023/{media_size}" and
              ranged_headers.get("content-length") == "1024", "Original byte range differs from the indexed file")
        alias = urlunsplit(("", "", f"/eMbY/vIdEoS/{quote(item_id)}/OrIgInAl.Mp4", parsed.query, ""))
        alias_headers, alias_body = api.request("HEAD", alias, label="Mixed-case original route alias", parse=False)
        check(not alias_body and alias_headers.get("content-length") == str(media_size) and
              alias_headers.get("etag") == metadata.get("etag"), "Mixed-case original route alias changed delivery")
        for invalid in (True, False):
            bad_query = dict(query)
            if invalid:
                bad_query["api_key"] = ["invalid-goby-direct-playback-token"]
            else:
                bad_query.pop("api_key")
            rejected = urlunsplit(("", "", parsed.path, urlencode(bad_query, doseq=True), ""))
            api.request("GET", rejected, expected=(401,), parse=False, label="Missing or invalid stream token")
        _, content = api.request("GET", target, label="Generated URL complete original fetch", parse=False, limit=MAX_MEDIA)
        check(len(content) == media_size and hashlib.sha256(content).hexdigest() == fixture.state["media_sha256"],
              "Authenticated HTTP media bytes differ from the owned original file")
        decoded = decode_media(content)
        summary["assertions"].append({"stage": stage, "head": 200, "range": 206,
                                      "missing_and_invalid_token": 401, "original_bytes_match": True, **decoded})

        stage = "Started Progress detail and Resume"
        report(api, "Started", play_id, item_id, source_id, 0)
        state_is(detail(api, item_id)["UserData"], 0, 1, False, False)
        report(api, "Progress", play_id, item_id, source_id, POSITION)
        progress_data = detail(api, item_id, alias=True)["UserData"]
        state_is(progress_data, POSITION, 1, False, False)
        played_date = datetime.fromisoformat(progress_data.get("LastPlayedDate", "").replace("Z", "+00:00"))
        check(played_date.tzinfo is not None, "Progress did not persist a timezone-aware last-played date")
        result = resume(api, library_id, alias=True)
        check(result.get("TotalRecordCount") == 1 and len(result.get("Items", [])) == 1 and
              result["Items"][0].get("Id") == item_id, "Progress at 120 of 600 seconds is absent from Resume")
        state_is(result["Items"][0]["UserData"], POSITION, 1, False, False)
        session = current_session(api)
        player = session.get("PlayState", {})
        item = session.get("NowPlayingItem", {})
        check(item.get("Id") == item_id and "Path" not in item and "UserData" not in item and
              player.get("PositionTicks") == POSITION and player.get("CanSeek") is True and
              player.get("IsMuted") is True and player.get("VolumeLevel") == 0 and player.get("PlaybackRate") == 1.25,
              "Current session lost the authorized item or persistent player hints")
        api.request("POST", "/emby/Sessions/Playing/Ping?" + urlencode({"PlaySessionId": play_id}),
                    emby=True, expected=(204,), parse=False, label="Owned playback ping")
        summary["assertions"].append({"stage": stage, "started": 204, "progress": 204,
                                      "position_seconds": 120, "duration_seconds": 600, "play_count": 1,
                                      "current_session_player_hints": True, "ping": 204})

        stage = "Stopped and duplicate Stopped"
        report(api, "Stopped", play_id, item_id, source_id, POSITION)
        stopped = True
        stopped_data = detail(api, item_id)["UserData"]
        state_is(stopped_data, POSITION, 1, False, False)
        report(api, "Stopped", play_id, item_id, source_id, 590 * TICKS)
        check(detail(api, item_id)["UserData"] == stopped_data, "Duplicate Stopped changed persisted user state")
        check("NowPlayingItem" not in current_session(api), "Stopped playback remained in the current session")
        result = resume(api, library_id)
        check(result.get("TotalRecordCount") == 1 and len(result.get("Items", [])) == 1 and
              result["Items"][0].get("Id") == item_id,
              "Duplicate Stopped changed Resume membership")
        summary["assertions"].append({"stage": stage, "stopped": 204, "duplicate_stopped": 204,
                                      "state_stable": True})

        stage = "Played Favorite and owned state reset"
        played = api.request("POST", f"/uSeRs/{quote(api.user_id)}/pLaYeDiTeMs/{quote(item_id)}",
                             emby=True, label="Owned watched flag")
        state_is(played, 0, 1, True, False)
        favorite = api.request("POST", f"/emby/Users/{quote(api.user_id)}/FavoriteItems/{quote(item_id)}",
                               emby=True, label="Owned favorite flag")
        state_is(favorite, 0, 1, True, True)
        state_is(detail(api, item_id)["UserData"], 0, 1, True, True)
        check(resume(api, library_id).get("TotalRecordCount") == 0, "Watched fixture remained in Resume")
        reset_state(api, item_id, library_id)
        state_reset = True
        summary["owned_user_state_reset"] = True
        summary["assertions"].append({"stage": stage, "played": True, "favorite": True,
                                      "position_count_flags_and_resume_reset": True})
        api.request("GET", "/readyz", label="Goby readiness after verification")
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, VerificationFailure) else type(error).__name__
    finally:
        if api.token and item_id and library_id and not state_reset:
            try:
                if play_id and source_id and not stopped:
                    report(api, "Stopped", play_id, item_id, source_id, 0)
                reset_state(api, item_id, library_id)
                summary["owned_user_state_reset"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned playback state reset failed")
        if api.token:
            try:
                api.request("POST", "/emby/Sessions/Logout", emby=True, parse=False, label="Owned viewer logout")
                summary["owned_viewer_session_revoked"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned viewer session revocation failed")
        if api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,),
                            parse=False, label="Administrator logout")
            except Exception:
                summary["cleanup_errors"].append("Administrator session revocation failed")
        if media_path is not None:
            try:
                check(digest(media_path) == fixture.state["media_sha256"], "Owned synthetic media changed")
                private_file(DIRECTORY / STATE_NAME)
                summary["owned_media_unchanged"] = True
                summary["private_state_mode"] = "0600"
                summary["retained_scope"] = "One marked synthetic fixture, one owned library, and one reusable non-administrator"
            except Exception:
                summary["cleanup_errors"].append("Owned media or private state preservation check failed")
        if fixture.lock is not None:
            fixture.lock.close()
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
