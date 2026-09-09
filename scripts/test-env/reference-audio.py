#!/usr/bin/env python3
"""Capture bounded universal/progressive audio contracts from official Emby.

Run only through SSH inside the existing reference service network namespace.
New sources, raw records, wire bodies, credentials, and exports stay in a
separately marked tmpfs directory. Older reference records and media are hashed
before work and audited afterward. Only sanitized exports belong in the repo.
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
import secrets
import shutil
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qs, urlencode, urljoin, urlsplit, urlunsplit


sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("reference_capture", Path(__file__).with_name("reference-capture.py"))
BASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BASE)
ROOT = Path("/dev/shm/goby-emby-reference/runtime/audio-m4c")
PRIVATE = ROOT / "private"
RAW = PRIVATE / "raw"
WIRE = PRIVATE / "wire"
EXPORT = ROOT / "export"
SOURCES = ROOT / "source"
PROBES = PRIVATE / "probe"
MARKER = "goby-audio-m4c-owned-v1"
PREFIX = "audio-m4c-"
DEVICE = "goby-audio-m4c-recorder"
ORIGIN = "http://127.0.0.1:18097"
CONTEXT = PRIVATE / "context.json"
BASELINE = PRIVATE / "baseline.json"
FFMPEG = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg"
FFPROBE = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe"
MAX_BODY = 2 * 1024 * 1024
MAX_TOTAL = 12 * 1024 * 1024
SOURCE_NAMES = {kind: "Reference Audio M4c." + kind for kind in ("mp3", "flac", "aac", "wav")}


def check(condition: bool, label: str) -> None:
    if not condition:
        raise RuntimeError(label)


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(131072), b""):
            result.update(chunk)
    return result.hexdigest()


def private_write(path: Path, value: str, *, exclusive=False) -> None:
    flags = os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | (os.O_EXCL if exclusive else os.O_TRUNC)
    if path.exists():
        info = path.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and
              stat.S_IMODE(info.st_mode) == 0o600, "Private capture file ownership does not match")
    descriptor = os.open(path, flags, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
        stream.write(value)


def credentials(mode: str) -> Path:
    check(mode in {"admin", "user"}, "Unsupported private credential role")
    return PRIVATE / (mode + "-credentials.env")


def preconditions(preparing=False) -> int:
    check(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
          "Run only as root through SSH on the authorized Linux test host")
    pid = int(subprocess.check_output(["systemctl", "show", "goby-emby-reference.service", "-p", "MainPID", "--value"], timeout=5))
    check(pid > 1 and os.readlink("/proc/self/ns/net") == os.readlink(f"/proc/{pid}/ns/net") and
          os.readlink("/proc/self/ns/net") != os.readlink("/proc/1/ns/net"), "Recorder is outside the isolated reference network namespace")
    for path in (BASE.DATA, Path("/dev/shm/goby-emby-reference")):
        check((path / ".goby-managed").read_text().strip() == "goby-emby-reference-owned-v1",
              "Existing reference ownership marker does not match")
    if preparing:
        check(not ROOT.exists(), "Audio reference preparation already exists")
        check(shutil.disk_usage("/dev/shm").free > 128 * 1024 * 1024, "Insufficient tmpfs space for the bounded audio capture")
    else:
        check(ROOT.resolve(strict=True) == ROOT and (ROOT / ".goby-managed").read_text().strip() == MARKER,
              "Audio capture ownership marker does not match")
    return pid


class Recorder(BASE.Recorder):
    def __init__(self) -> None:
        super().__init__()
        for directory in (BASE.PRIVATE, PRIVATE):
            if directory.exists():
                for path in directory.glob("*credentials.env"):
                    self.secret_values.update(value for key, value in BASE.read_credentials(path).items()
                                              if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)
        self.total = sum(json.loads(path.read_text()).get("observation", {}).get("wireBytes", 0)
                         for path in RAW.glob(PREFIX + "*.json")) if RAW.exists() else 0

    def clean_text(self, value: str) -> str:
        return super().clean_text(value).replace(str(SOURCES), "/reference-audio-source").replace(str(ROOT), "/reference-audio-private")

    def audit_export(self, original, exported, key="") -> None:
        if isinstance(original, str) and str(ROOT) in original:
            check(exported == self.clean_text(original), "Private audio path sanitization changed unrelated text")
            return
        super().audit_export(original, exported, key)

    def use(self, mode: str) -> None:
        self.credential_path = credentials(mode)
        self.credentials = BASE.read_credentials(self.credential_path)
        self.secret_values.update(value for key, value in self.credentials.items()
                                  if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)
        device = DEVICE if mode == "user" else DEVICE + "-admin"
        self.client_headers = {"Accept": "application/json", "Authorization":
            'Emby Client="Goby Audio Reference Recorder", Device="Linux Audio Test", '
            f'DeviceId="{device}", Version="0.1.0"'}

    def write(self, suffix: str, record: dict) -> None:
        name = PREFIX + suffix
        check(all(not (folder / (name + ".json")).exists() for folder in (RAW, EXPORT)),
              "Refusing to overwrite an existing audio evidence record")
        record.setdefault("reference", {"product": "Emby Server", "version": "4.9.5.0",
                                       "capturedAt": dt.datetime.now(dt.timezone.utc).isoformat()})
        cleaned = self.sanitize(record)
        self.audit_export(record, cleaned)
        text = json.dumps(cleaned, indent=2, ensure_ascii=False) + "\n"
        check(not any(secret in text for secret in self.secret_values), "A credential survived export sanitization")
        private_write(RAW / (name + ".json"), json.dumps(record, indent=2) + "\n", exclusive=True)
        private_write(EXPORT / (name + ".json"), text, exclusive=True)

    def request(self, suffix: str, method: str, target: str, *, body=None, headers=None, authenticated=False, note=""):
        check(all(not (folder / (PREFIX + suffix + extension)).exists()
                  for folder, extension in ((RAW, ".json"), (EXPORT, ".json"), (WIRE, ".b64"))),
              "Refusing to repeat a request whose audio evidence already exists")
        fields = dict(self.client_headers if headers is None else headers)
        if authenticated:
            fields["X-Emby-Token"] = self.credentials["REFERENCE_TOKEN"]
        payload = None if body is None else json.dumps(body, separators=(",", ":")).encode()
        if payload is not None:
            fields["Content-Type"] = "application/json"
        check(target.startswith("/") and not target.startswith("//") and len(target) <= 16384,
              "Reference request target must be a bounded local path")
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=15)
        try:
            connection.request(method, target, payload, fields)
            response = connection.getresponse()
            status_code, response_headers = response.status, response.getheaders()
            wire = response.read(MAX_BODY + 1)
        finally:
            connection.close()
        check(len(wire) <= MAX_BODY, "Audio response exceeded the per-request size limit")
        self.total += len(wire)
        check(self.total <= MAX_TOTAL, "Audio captures exceeded the total wire budget")
        metadata = {key.lower(): value for key, value in response_headers}
        content_type = metadata.get("content-type", "")
        if wire and content_type.startswith(("audio/", "video/", "application/octet-stream")):
            parsed, representation = base64.b64encode(wire).decode("ascii"), "binary-base64"
        else:
            try:
                text = wire.decode("utf-8")
                try:
                    parsed, representation = json.loads(text), "json"
                except json.JSONDecodeError:
                    parsed, representation = text, "text"
            except UnicodeDecodeError:
                parsed, representation = base64.b64encode(wire).decode("ascii"), "binary-base64"
        if isinstance(parsed, dict) and parsed.get("AccessToken"):
            self.credentials["REFERENCE_TOKEN"] = parsed["AccessToken"]
            self.credentials["REFERENCE_USER_ID"] = parsed["User"]["Id"]
            self.secret_values.add(parsed["AccessToken"])
            BASE.save_credentials(self.credentials, self.credential_path)
        private_write(WIRE / (PREFIX + suffix + ".b64"), base64.b64encode(wire).decode("ascii"), exclusive=True)
        record = {"request": {"method": method, "path": target, "headers": fields, "body": body},
                  "response": {"status": status_code, "headers": response_headers, "bodyType": representation, "body": parsed},
                  "observation": {"wireBytes": len(wire), "wireSha256": hashlib.sha256(wire).hexdigest(), "note": note}}
        self.write(suffix, record)
        print(f"{PREFIX}{suffix}: HTTP {status_code}, {representation}, {len(wire)} bytes", flush=True)
        return {"status": status_code, "headers": metadata, "parsed": parsed, "wire": wire, "path": target}

    def runtime(self) -> dict:
        pid = preconditions()
        children = []
        for child in Path(f"/proc/{pid}/task/{pid}/children").read_text().split():
            try:
                children.append({"pid": int(child), "name": Path(f"/proc/{child}/comm").read_text().strip()})
            except FileNotFoundError:
                pass
        cache = BASE.DATA / "transcoding-temp"
        files = [path for path in cache.rglob("*") if path.is_file()] if cache.exists() else []
        return {"referencePID": pid, "directChildProcesses": children, "transcodingFileCount": len(files),
                "transcodingBytes": sum(path.stat().st_size for path in files),
                "rootFreeBytes": shutil.disk_usage("/").free, "tmpfsFreeBytes": shutil.disk_usage("/dev/shm").free}

    def prepare(self) -> None:
        preconditions(preparing=True)
        records = {str(path): digest(path) for folder in (BASE.PRIVATE / "raw", BASE.EXPORT) for path in folder.glob("*.json")}
        check(len(records) == 872, "Expected the preserved 436 reference raw/export pairs")
        old_media = {}
        media_bytes = 0
        for path in Path("/opt/goby-fixtures").rglob("*"):
            if path.suffix.lower() not in {".mp4", ".mkv", ".flac", ".mp3", ".aac", ".wav", ".srt", ".vtt"}:
                continue
            info = path.lstat()
            check(stat.S_ISREG(info.st_mode) and path.resolve(strict=True).is_relative_to("/opt/goby-fixtures"),
                  "An existing synthetic input is not a regular owned-tree file")
            media_bytes += info.st_size
            check(media_bytes <= 64 * 1024 * 1024, "Existing synthetic input hashing exceeded its bound")
            old_media[str(path)] = digest(path)
        ROOT.mkdir(mode=0o700)
        for path in (PRIVATE, RAW, WIRE, EXPORT, PROBES, SOURCES):
            path.mkdir(mode=0o700)
        private_write(ROOT / ".goby-managed", MARKER + "\n", exclusive=True)
        private_write(BASELINE, json.dumps({"records": records, "media": old_media,
                                          "gobyPID": int(subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5)),
                                          "oldRawExportPairs": 436}, indent=2), exclusive=True)
        source_facts = []
        options = {
            "mp3": ["-c:a", "libmp3lame", "-b:a", "128k", "-ar", "44100", "-ac", "2"],
            "flac": ["-c:a", "flac", "-ar", "96000", "-ac", "2", "-sample_fmt", "s32", "-bits_per_raw_sample", "24"],
            "aac": ["-c:a", "aac", "-b:a", "64k", "-ar", "48000", "-ac", "1", "-f", "adts"],
            "wav": ["-c:a", "pcm_s16le", "-ar", "48000", "-ac", "1"],
        }
        for kind, extra in options.items():
            path = SOURCES / SOURCE_NAMES[kind]
            command = [FFMPEG, "-hide_banner", "-nostdin", "-loglevel", "error", "-n", "-f", "lavfi", "-i",
                       "sine=frequency=440:sample_rate=96000:duration=6", "-t", "6", "-threads", "1", *extra,
                       "-metadata", "title=Reference Audio " + kind.upper(), "-metadata", "artist=Goby Audio Reference",
                       "-metadata", "album=Goby M4c Audio", str(path)]
            result = subprocess.run(command, capture_output=True, timeout=30)
            check(result.returncode == 0 and not result.stderr, "Owned synthetic audio generation failed")
            path.chmod(0o644)
            check(path.stat().st_size <= MAX_BODY, "An owned synthetic audio source exceeds two MiB")
            probe = subprocess.run([FFPROBE, "-v", "error", "-show_streams", "-show_format", "-of", "json", str(path)],
                                   capture_output=True, timeout=15)
            check(probe.returncode == 0 and not probe.stderr, "Owned source probe failed")
            source_facts.append({"kind": kind, "path": str(path), "bytes": path.stat().st_size,
                                 "sha256": digest(path), "probe": json.loads(probe.stdout)})
        SOURCES.chmod(0o755)
        private_write(PRIVATE / "sources.json", json.dumps(source_facts, indent=2), exclusive=True)
        self.write("source-provenance", {"kind": "synthetic-source-provenance", "sources": source_facts,
                                        "note": "New six-second synthetic tones generated only in the marked tmpfs directory."})
        public = self.request("system-info", "GET", "/emby/System/Info/Public", headers={})
        check(public["status"] == 200 and public["parsed"].get("Version") == "4.9.5.0", "Unexpected official reference version")
        self.write("runtime-before", {"kind": "remote-runtime-observation", **self.runtime()})

    def setup(self) -> None:
        preconditions()
        check(not CONTEXT.exists() and not credentials("admin").exists() and not credentials("user").exists(),
              "Audio reference account setup already exists")
        original = BASE.read_credentials()
        BASE.save_credentials({key: original[key] for key in ("REFERENCE_USERNAME", "REFERENCE_PASSWORD")}, credentials("admin"))
        self.use("admin")
        login = self.request("admin-login", "POST", "/emby/Users/AuthenticateByName",
                             body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        check(login["status"] == 200, "Dedicated administrator-device login failed")
        user = {"REFERENCE_USERNAME": "reference-audio-m4c", "REFERENCE_PASSWORD": secrets.token_hex(32)}
        BASE.save_credentials(user, credentials("user"))
        self.secret_values.add(user["REFERENCE_PASSWORD"])
        created = self.request("user-create", "POST", "/emby/Users/New", authenticated=True, body={"Name": user["REFERENCE_USERNAME"]})
        check(created["status"] == 200 and not created["parsed"].get("Policy", {}).get("IsAdministrator"), "Ordinary audio user creation failed")
        user["REFERENCE_USER_ID"] = created["parsed"]["Id"]
        BASE.save_credentials(user, credentials("user"))
        password = self.request("user-password", "POST", f"/emby/Users/{user['REFERENCE_USER_ID']}/Password", authenticated=True,
                                body={"Id": user["REFERENCE_USER_ID"], "NewPw": user["REFERENCE_PASSWORD"], "ResetPassword": False})
        check(password["status"] in {200, 204}, "Owned ordinary audio password setup failed")
        libraries = self.request("libraries-before", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        check(libraries["status"] == 200, "Existing reference library query failed")
        items = libraries["parsed"]["Items"]
        check(not any(item.get("Name") == "Reference Audio M4c" for item in items), "Owned audio library already exists")
        prior = next(item for item in items if item.get("Name") == "Reference Music")
        options = json.loads(json.dumps(prior["LibraryOptions"]))
        options.update({"PathInfos": [{"Path": str(SOURCES)}], "EnableRealtimeMonitor": False, "SampleIgnoreSize": 0,
                        "SaveLocalMetadata": False, "EnableAutomaticSeriesGrouping": False})
        created = self.request("library-create", "POST", "/emby/Library/VirtualFolders", authenticated=True,
                               body={"Name": "Reference Audio M4c", "CollectionType": "music", "Paths": [str(SOURCES)],
                                     "RefreshLibrary": False, "LibraryOptions": options})
        check(created["status"] in {200, 204}, "Owned audio library creation failed")
        libraries = self.request("libraries-after", "GET", "/emby/Library/VirtualFolders/Query", authenticated=True)
        library = next(item for item in libraries["parsed"]["Items"] if item.get("Name") == "Reference Audio M4c")
        self.request("library-refresh", "POST", f"/emby/Items/{library['ItemId']}/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=FullRefresh", authenticated=True, body={})
        self.use("user")
        login = self.request("user-login", "POST", "/emby/Users/AuthenticateByName",
                             body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        check(login["status"] == 200, "Owned ordinary audio login failed")
        found = {}
        query = urlencode({"UserId": self.credentials["REFERENCE_USER_ID"], "ParentId": library["ItemId"], "Recursive": "true",
                           "IncludeItemTypes": "Audio", "Fields": "Path,MediaSources,MediaStreams", "Limit": 20})
        for attempt in range(20):
            listed = self.request(f"source-items-{attempt:02}", "GET", "/emby/Items?" + query, authenticated=True)
            for item in listed["parsed"].get("Items", []):
                for kind, name in SOURCE_NAMES.items():
                    if item.get("Path") == str(SOURCES / name):
                        found[kind] = {"Id": item["Id"], "MediaSourceId": item["MediaSources"][0]["Id"], "Path": item["Path"]}
            if len(found) == 4:
                break
            time.sleep(0.5)
        check(len(found) == 4, "The bounded scan did not index all four owned audio formats")
        private_write(CONTEXT, json.dumps({"items": found, "library": library, "nonce": secrets.token_hex(16)}, indent=2), exclusive=True)

    def local(self, parent: str, child: str) -> str:
        target = urlsplit(urljoin(ORIGIN + parent, child))
        check(target.scheme == "http" and target.hostname == "127.0.0.1" and target.port == 18097 and
              not target.username and not target.password and not target.fragment, "Refusing to follow a reference URL outside the isolated loopback origin")
        return urlunsplit(("", "", target.path, target.query, ""))

    def probe(self, suffix: str, wire: bytes, *, manifest=None, source_fixture=None) -> dict:
        path = PROBES / (PREFIX + suffix + (".m3u8" if manifest is not None else ".media"))
        content = manifest.encode("utf-8") if manifest is not None else wire
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(content)
        inputs = ["-protocol_whitelist", "file,http,tcp,pipe", "-rw_timeout", "10000000", "-allowed_extensions", "ALL"] if manifest is not None else []
        probed = subprocess.run([FFPROBE, "-v", "error", *inputs, "-show_streams", "-show_format", "-of", "json", str(path)],
                                capture_output=True, timeout=20)
        decoded = subprocess.run([FFMPEG, "-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-threads", "1", *inputs,
                                  "-i", str(path), "-map", "0:a:0", "-vn", "-threads", "1", "-progress", "pipe:1", "-nostats", "-f", "null", "-"],
                                 capture_output=True, timeout=25)
        check(max(len(probed.stdout), len(probed.stderr), len(decoded.stdout), len(decoded.stderr)) <= MAX_BODY,
              "Audio media-process output exceeded its limit")
        facts = json.loads(probed.stdout) if probed.stdout else None
        progress = dict(line.partition("=")[::2] for line in decoded.stdout.decode(errors="replace").splitlines() if "=" in line)
        result = {"kind": "remote-media-probe", "sourceFixture": source_fixture or PREFIX + suffix,
                  "input": {"bytes": len(content), "sha256": hashlib.sha256(content).hexdigest()},
                  "ffprobe": {"exitCode": probed.returncode, "body": facts, "stderr": probed.stderr.decode(errors="replace")},
                  "ffmpegDecode": {"exitCode": decoded.returncode, "stderr": decoded.stderr.decode(errors="replace"),
                                   "progress": progress, "scope": "Complete real audio response or emitted HLS graph; software decoding."}}
        self.write(suffix + "-probe", result)
        audio = next((stream for stream in (facts or {}).get("streams", []) if stream.get("codec_type") == "audio"), {})
        return {"probeExit": probed.returncode, "decodeExit": decoded.returncode, "codec": audio.get("codec_name"),
                "sampleRate": audio.get("sample_rate"), "bitsPerRawSample": audio.get("bits_per_raw_sample"),
                "channels": audio.get("channels"), "duration": (facts or {}).get("format", {}).get("duration"),
                "decodedSeconds": int(progress.get("out_time_us", "0")) / 1_000_000}

    def follow(self, suffix: str, response: dict, play_ids: set[str]) -> dict:
        redirects = []
        for hop in range(3):
            play_ids.update(parse_qs(urlsplit(response["path"]).query).get("PlaySessionId", []))
            if response["status"] not in {301, 302, 303, 307, 308}:
                break
            location = response["headers"].get("location", "")
            check(bool(location), "Audio redirect omitted its Location header")
            redirects.append(response["status"])
            target = self.local(response["path"], location)
            response = self.request(f"{suffix}-redirect-{hop + 1}", "GET", target, headers={},
                                    note="Follows the emitted same-origin Location without adding an inherited query token or authentication header.")
        result = {"redirectStatuses": redirects, "finalStatus": response["status"],
                  "finalContentType": response["headers"].get("content-type"), "finalBytes": len(response["wire"])}
        if response["status"] != 200:
            return result
        if response["wire"].startswith(b"#EXTM3U"):
            result["delivery"] = "hls"
            root_response = response
            children = [line.strip() for line in response["wire"].decode().splitlines() if line.strip() and not line.startswith("#")]
            if "#EXT-X-STREAM-INF" in response["wire"].decode():
                check(len(children) == 1, "The bounded audio HLS control expected one variant")
                response = self.request(suffix + "-hls-main", "GET", self.local(response["path"], children[0]), headers={})
                if response["status"] != 200:
                    result["mediaPlaylistStatus"] = response["status"]
                    return result
                children = [line.strip() for line in response["wire"].decode().splitlines() if line.strip() and not line.startswith("#")]
            check(0 < len(children) <= 6, "Audio HLS segment count exceeded its source bound")
            statuses = []
            for index, child in enumerate(children):
                target = self.local(response["path"], child)
                play_ids.update(parse_qs(urlsplit(target).query).get("PlaySessionId", []))
                segment = self.request(f"{suffix}-hls-segment-{index}", "GET", target, headers={})
                statuses.append(segment["status"])
            result["segmentStatuses"] = statuses
            if all(status == 200 for status in statuses):
                lines = [line if line.startswith("#") or not line.strip() else ORIGIN + self.local(root_response["path"], line.strip())
                         for line in root_response["wire"].decode().splitlines()]
                result["media"] = self.probe(suffix + "-hls", b"", manifest="\n".join(lines) + "\n")
        elif response["headers"].get("content-type", "").startswith(("audio/", "video/", "application/octet-stream")):
            result["delivery"] = "progressive_or_original"
            result["media"] = self.probe(suffix + "-media", response["wire"])
            result["matchesSourceKinds"] = [kind for kind, name in SOURCE_NAMES.items() if response["wire"] == (SOURCES / name).read_bytes()]
        else:
            result["delivery"] = "non_media_response"
        return result

    def cleanup(self, suffix: str, play_ids: set[str]) -> None:
        for index, play_id in enumerate(sorted(play_ids)):
            query = urlencode({"DeviceId": DEVICE, "PlaySessionId": play_id})
            self.request(f"{suffix}-cleanup-{index}", "DELETE", "/emby/Videos/ActiveEncodings?" + query, authenticated=True)

    def case(self, name: str, kind: str, parameters: dict, *, suffix="", minimal=False, token="own", group_id=None,
             request_headers=None, static=False, perform_cleanup=True) -> dict:
        context = json.loads(CONTEXT.read_text())
        item = context["items"][kind]
        play_id = group_id or hashlib.sha256((context["nonce"] + name).encode()).hexdigest()[:32]
        query = {"UserId": self.credentials["REFERENCE_USER_ID"], "DeviceId": DEVICE,
                 "PlaySessionId": play_id, "MaxStreamingBitrate": "2000000"}
        if minimal:
            query = {"DeviceId": DEVICE}
        query.update(parameters)
        if token is not None:
            query["api_key"] = self.credentials["REFERENCE_TOKEN"] if token == "own" else token
        base = f"/emby/Audio/{item['Id']}/" + ("stream.mp3" if static else "universal" + suffix)
        target = base + "?" + urlencode(query)
        before = self.runtime()
        head = self.request(name + "-head", "HEAD", target, headers=request_headers or {})
        after_head = self.runtime()
        time.sleep(0.15)
        later_head = self.runtime()
        ids = {play_id}
        try:
            response = self.request(name + "-get", "GET", target, headers=request_headers or {})
            result = {"kind": "audio-case-observation", "case": name, "sourceKind": kind, "parameters": parameters,
                      "headStatus": head["status"], "initialGetStatus": response["status"],
                      "headLocationPresent": "location" in head["headers"], "getLocationPresent": "location" in response["headers"],
                      "headProcessObservations": {"before": before, "immediateAfter": after_head, "after150ms": later_head}}
            if static:
                result["originalPrefixMatches"] = response["wire"] == (SOURCES / SOURCE_NAMES[kind]).read_bytes()[:1024]
                result["contentRange"] = response["headers"].get("content-range")
            else:
                result.update(self.follow(name, response, ids))
            self.write(name + "-observation", result)
            return result
        finally:
            if perform_cleanup:
                self.cleanup(name, ids)

    def capture(self) -> None:
        preconditions()
        self.use("user")
        cases = [
            ("mp3-minimal", "mp3", {}, {"minimal": True}),
            ("mp3-match", "mp3", {"Container": "mp3"}, {}),
            ("mp3-match-other-codec", "mp3", {"Container": "mp3", "AudioCodec": "aac"}, {}),
            ("flac-match", "flac", {"Container": "flac"}, {}),
            ("flac-list-mp3-first", "flac", {"Container": "mp3,flac"}, {}),
            ("flac-list-flac-first", "flac", {"Container": "flac,mp3"}, {}),
            ("flac-list-uppercase", "flac", {"Container": "FLAC,MP3"}, {}),
            ("flac-max-sample", "flac", {"Container": "flac", "MaxSampleRate": 48000}, {}),
            ("flac-sample-precedence", "flac", {"Container": "flac", "MaxSampleRate": 48000, "AudioSampleRate": 22050}, {}),
            ("flac-bit-depth-exploratory", "flac", {"Container": "flac", "MaxBitDepth": 16}, {}),
            ("mp3-low-bitrate", "mp3", {"Container": "mp3", "MaxStreamingBitrate": 64000, "TranscodingContainer": "mp3", "AudioCodec": "mp3"}, {}),
            ("mp3-low-hls", "mp3", {"Container": "mp3", "MaxStreamingBitrate": 64000, "TranscodingContainer": "ts", "TranscodingProtocol": "hls", "AudioCodec": "aac"}, {}),
            ("flac-progressive-defaults", "flac", {"Container": "mp3"}, {}),
            ("flac-progressive-empty", "flac", {"Container": "mp3", "TranscodingProtocol": "", "TranscodingContainer": "mp3", "AudioCodec": "mp3"}, {}),
            ("flac-http-codec-conflict", "flac", {"Container": "mp3", "TranscodingProtocol": "http", "TranscodingContainer": "mp3", "AudioCodec": "aac"}, {}),
            ("mp3-suffix-query-conflict", "mp3", {"Container": "flac"}, {"suffix": ".mp3"}),
            ("mp3-original-start", "mp3", {"Container": "mp3", "StartTimeTicks": 20000000}, {}),
            ("flac-progressive-start", "flac", {"Container": "mp3", "TranscodingContainer": "mp3", "AudioCodec": "mp3", "StartTimeTicks": 20000000}, {}),
            ("aac-match", "aac", {"Container": "aac"}, {}),
            ("wav-match", "wav", {"Container": "wav"}, {}),
            ("no-token", "mp3", {"Container": "mp3"}, {"token": None}),
            ("bad-token", "mp3", {"Container": "mp3"}, {"token": "invalid-audio-reference-token"}),
            ("mp3-static-range-start", "mp3", {"Static": "true", "AudioCodec": "aac", "AudioSampleRate": 22050, "StartTimeTicks": 20000000},
             {"static": True, "request_headers": {"Range": "bytes=0-1023"}}),
        ]
        summaries = []
        for name, kind, parameters, options in cases:
            summaries.append(self.case(name, kind, parameters, **options))
        group_id = hashlib.sha256((json.loads(CONTEXT.read_text())["nonce"] + "shared-session").encode()).hexdigest()[:32]
        for name, kind, bitrate, start in (("reuse-first", "mp3", 64000, 0), ("reuse-quality", "mp3", 96000, 0),
                                           ("reuse-seek", "mp3", 96000, 20000000), ("reuse-source", "flac", 96000, 0)):
            summaries.append(self.case(name, kind, {"Container": "mp3", "MaxStreamingBitrate": bitrate,
                             "TranscodingContainer": "mp3", "AudioCodec": "mp3", "StartTimeTicks": start},
                             group_id=group_id, perform_cleanup=False))
        self.cleanup("reuse-final", {group_id})
        self.request("device-only-cleanup", "DELETE", "/emby/Videos/ActiveEncodings?" + urlencode({"DeviceId": DEVICE}), authenticated=True,
                     note="Targets only this newly created recorder device, including a minimal URL that omitted PlaySessionId.")
        private_write(PRIVATE / "case-summaries.json", json.dumps(summaries, indent=2), exclusive=True)
        self.write("runtime-after-cleanup", {"kind": "remote-runtime-observation", **self.runtime()})

    def audit(self) -> None:
        preconditions()
        baseline = json.loads(BASELINE.read_text())
        check(all(digest(Path(path)) == value for path, value in baseline["records"].items()), "A preserved pre-existing reference record changed")
        check(all(digest(Path(path)) == value for path, value in baseline["media"].items()), "An existing synthetic media input changed")
        facts = json.loads((PRIVATE / "sources.json").read_text())
        check(all(digest(Path(source["path"])) == source["sha256"] for source in facts), "A new owned source changed after generation")
        total, http_records = 0, 0
        paths = sorted(RAW.glob(PREFIX + "*.json"))
        for path in paths:
            original = json.loads(path.read_text())
            exported = json.loads((EXPORT / path.name).read_text())
            self.audit_export(original, exported)
            check(exported == self.sanitize(original), "Audio export differs from deterministic sanitization")
            check(not any(secret in (EXPORT / path.name).read_text() for secret in self.secret_values), "A credential remains in an audio export")
            if "request" not in original:
                continue
            wire = base64.b64decode((WIRE / (path.stem + ".b64")).read_text(), validate=True)
            response = original["response"]
            headers = {key.lower(): value for key, value in response["headers"]}
            check(exported["response"]["headers"] == self.sanitize(response["headers"]), "Response header sanitization changed its structure or non-secret values")
            if original["request"]["method"] != "HEAD" and "content-length" in headers:
                check(int(headers["content-length"]) == len(wire), "Recorded Content-Length differs from exact response bytes")
            if response["bodyType"] == "text":
                check(response["body"].encode("utf-8") == wire, "Recorded audio text wire bytes changed")
            elif response["bodyType"] == "binary-base64":
                check(base64.b64decode(response["body"], validate=True) == wire, "Recorded audio binary wire bytes changed")
                check(not any(secret.encode() in wire for secret in self.secret_values), "An audio binary body contains a credential")
            elif response["bodyType"] == "json":
                check(json.loads(wire) == response["body"], "Recorded audio JSON response changed")
            check(hashlib.sha256(wire).hexdigest() == original["observation"]["wireSha256"], "Audio wire digest changed")
            total += len(wire)
            http_records += 1
        check(total <= MAX_TOTAL, "Audio evidence exceeded its total wire budget")
        goby_pid = int(subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        summary = {"status": "passed", "audioRecords": len(paths), "httpRecords": http_records, "wireBytes": total,
                   "preservedOldRecords": len(baseline["records"]), "preservedOldMedia": len(baseline["media"]),
                   "newSourceFilesUnchanged": len(facts), "gobyPIDBefore": baseline["gobyPID"], "gobyPIDAfter": goby_pid,
                   "gobyPIDUnchanged": goby_pid == baseline["gobyPID"]}
        private_write(PRIVATE / "audit-summary.json", json.dumps(summary, indent=2))
        print(json.dumps(summary, indent=2, sort_keys=True))

    def controls(self) -> None:
        preconditions()
        self.use("user")
        name = "control-progressive-start-fresh"
        parameters = {"Container": "mp3", "TranscodingProtocol": "", "TranscodingContainer": "mp3",
                      "AudioCodec": "mp3", "MaxStreamingBitrate": 128000, "StartTimeTicks": 20000000}
        first = self.case(name, "flac", parameters, perform_cleanup=False)
        original = json.loads((RAW / (PREFIX + name + "-get.json")).read_text())
        target = original["request"]["path"]
        ids = set(parse_qs(urlsplit(target).query).get("PlaySessionId", []))
        try:
            time.sleep(0.2)
            retried = self.request(name + "-retry", "GET", target, headers={},
                                   note="One bounded retry of the identical fresh stream URL before cleanup; the initial result remains immutable.")
            result = {"kind": "audio-retry-observation", "firstStatus": first["initialGetStatus"],
                      "retryStatus": retried["status"], "parameters": parameters, "identicalURL": True}
            result.update(self.follow(name + "-retry", retried, ids))
            ranged = self.request(name + "-range", "GET", target, headers={"Range": "bytes=0-1023"})
            cached_head = self.request(name + "-cached-head", "HEAD", target, headers={})
            result.update({"rangeStatus": ranged["status"], "rangeBytes": len(ranged["wire"]),
                           "rangeContentRange": ranged["headers"].get("content-range"),
                           "rangePrefixMatches": ranged["wire"] == retried["wire"][:1024],
                           "cachedHeadStatus": cached_head["status"]})
            self.write(name + "-retry-observation", result)
        finally:
            self.cleanup(name + "-final", ids)
        for number in (0, 1):
            source = PREFIX + f"mp3-low-hls-hls-segment-{number}"
            wire = base64.b64decode((WIRE / (source + ".b64")).read_text(), validate=True)
            self.probe(f"control-existing-hls-segment-{number}", wire, source_fixture=source)
        self.case("control-flac-hls-exact-duration", "flac", {"Container": "mp3", "TranscodingProtocol": "hls",
                  "TranscodingContainer": "ts", "AudioCodec": "aac", "MaxStreamingBitrate": 128000,
                  "MaxSampleRate": 48000, "AudioSampleRate": 48000})
        self.write("runtime-after-controls", {"kind": "remote-runtime-observation", **self.runtime()})

    def finish(self) -> None:
        self.use("user")
        self.request("final-device-cleanup", "DELETE", "/emby/Videos/ActiveEncodings?" + urlencode({"DeviceId": DEVICE}), authenticated=True)
        time.sleep(0.5)
        self.write("runtime-final", {"kind": "remote-runtime-observation", **self.runtime()})
        self.request("user-logout", "POST", "/emby/Sessions/Logout", authenticated=True)
        self.use("admin")
        self.request("admin-logout", "POST", "/emby/Sessions/Logout", authenticated=True)
        self.audit()
        check(PROBES.resolve(strict=True).parent == PRIVATE and (ROOT / ".goby-managed").read_text().strip() == MARKER,
              "Probe cleanup ownership does not match")
        for path in PROBES.iterdir():
            info = path.lstat()
            check(path.name.startswith(PREFIX) and stat.S_ISREG(info.st_mode) and info.st_uid == 0,
                  "Unexpected file appeared in the owned audio probe directory")
        for path in PROBES.iterdir():
            path.unlink()
        PROBES.rmdir()
        print("Removed only owned temporary probe inputs; private originals, wire evidence, and source media remain.")

    def defaults(self) -> None:
        """Append only the three missing bare-universal source controls."""
        global ROOT, PRIVATE, RAW, WIRE, EXPORT, PROBES, MARKER, PREFIX, BASELINE

        preconditions()
        original_root = ROOT
        original_private = PRIVATE
        context = json.loads((original_private / "context.json").read_text())
        sources = json.loads((original_private / "sources.json").read_text())
        old_folders = (BASE.PRIVATE / "raw", BASE.EXPORT, RAW, EXPORT)
        baseline = {str(path): digest(path) for folder in old_folders for path in folder.glob("*.json")}
        check(len(list(RAW.glob("*.json"))) == len(list(EXPORT.glob("*.json"))) == 165 and len(baseline) == 1202,
              "Expected the immutable initial 165 audio records and 436 preceding reference pairs")
        check(all(digest(Path(source["path"])) == source["sha256"] for source in sources),
              "An original audio source changed before the default-format control")
        user = BASE.read_credentials(original_private / "user-credentials.env")
        ROOT = original_root.with_name("audio-m4c-defaults")
        PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
        RAW, WIRE, PROBES = PRIVATE / "raw", PRIVATE / "wire", PRIVATE / "probe"
        BASELINE = PRIVATE / "baseline.json"
        MARKER, PREFIX = "goby-audio-m4c-defaults-owned-v1", "audio-m4c-defaults-"
        preconditions(preparing=True)
        ROOT.mkdir(mode=0o700)
        for directory in (PRIVATE, RAW, WIRE, EXPORT):
            directory.mkdir(mode=0o700)
        private_write(ROOT / ".goby-managed", MARKER + "\n", exclusive=True)
        private_write(BASELINE, json.dumps(baseline, indent=2), exclusive=True)
        BASE.save_credentials({key: user[key] for key in ("REFERENCE_USERNAME", "REFERENCE_PASSWORD")}, credentials("user"))
        self.total = 0
        self.use("user")
        login = self.request("user-login", "POST", "/emby/Users/AuthenticateByName",
                             body={"Username": self.credentials["REFERENCE_USERNAME"], "Pw": self.credentials["REFERENCE_PASSWORD"]})
        check(login["status"] == 200 and self.credentials["REFERENCE_USER_ID"] == user["REFERENCE_USER_ID"],
              "Default-format control login did not resolve the existing owned user")
        before = self.runtime()
        results = []
        try:
            for kind in ("flac", "aac", "wav"):
                source = next(item for item in sources if item["kind"] == kind)
                item_id = context["items"][kind]["Id"]
                target = f"/emby/Audio/{item_id}/universal?" + urlencode({"DeviceId": DEVICE, "api_key": self.credentials["REFERENCE_TOKEN"]})
                head = self.request(kind + "-minimal-head", "HEAD", target, headers={},
                                    note="Only DeviceId and api_key are supplied, matching the initial MP3 minimal control.")
                fetched = self.request(kind + "-minimal-get", "GET", target, headers={})
                results.append({"sourceKind": kind, "headStatus": head["status"], "getStatus": fetched["status"],
                                "sourceBytes": source["bytes"], "receivedBytes": len(fetched["wire"]),
                                "sourceBytesMatch": fetched["wire"] == Path(source["path"]).read_bytes(),
                                "sourceSha256": source["sha256"], "receivedSha256": hashlib.sha256(fetched["wire"]).hexdigest(),
                                "headContentType": head["headers"].get("content-type"), "getContentType": fetched["headers"].get("content-type"),
                                "headContentLength": head["headers"].get("content-length"), "getContentLength": fetched["headers"].get("content-length"),
                                "headAcceptRanges": head["headers"].get("accept-ranges"), "getAcceptRanges": fetched["headers"].get("accept-ranges"),
                                "headLocationPresent": "location" in head["headers"], "getLocationPresent": "location" in fetched["headers"]})
            if not all(row["headStatus"] == row["getStatus"] == 200 and row["sourceBytesMatch"] for row in results):
                self.request("device-cleanup", "DELETE", "/emby/Videos/ActiveEncodings?" + urlencode({"DeviceId": DEVICE}), authenticated=True)
        finally:
            self.request("user-logout", "POST", "/emby/Sessions/Logout", authenticated=True)
        after = self.runtime()
        self.write("observation", {"kind": "bare-universal-default-observation", "queryNames": ["DeviceId", "api_key"],
                                   "omittedParameters": ["UserId", "PlaySessionId", "MaxStreamingBitrate", "Container",
                                                         "TranscodingProtocol", "TranscodingContainer", "AudioCodec"],
                                   "results": results, "runtimeBefore": before, "runtimeAfter": after})
        check(all(digest(Path(path)) == value for path, value in baseline.items()), "A preceding audio or reference record changed")
        check(all(digest(Path(source["path"])) == source["sha256"] for source in sources), "An existing audio source changed")
        total, http_records = 0, 0
        paths = sorted(RAW.glob(PREFIX + "*.json"))
        for path in paths:
            raw = json.loads(path.read_text())
            exported = json.loads((EXPORT / path.name).read_text())
            self.audit_export(raw, exported)
            check(exported == self.sanitize(raw) and not any(secret in (EXPORT / path.name).read_text() for secret in self.secret_values),
                  "Default-format export sanitization did not preserve the original record safely")
            if "request" not in raw:
                continue
            wire = base64.b64decode((WIRE / (path.stem + ".b64")).read_text(), validate=True)
            response = raw["response"]
            headers = {key.lower(): value for key, value in response["headers"]}
            check(exported["response"]["headers"] == self.sanitize(response["headers"]), "Default-format response headers changed")
            if raw["request"]["method"] != "HEAD" and "content-length" in headers:
                check(int(headers["content-length"]) == len(wire), "Default-format response length differs from wire bytes")
            if response["bodyType"] == "text":
                check(response["body"].encode() == wire, "Default-format text response changed")
            elif response["bodyType"] == "json":
                check(json.loads(wire) == response["body"], "Default-format JSON response changed")
            else:
                check(base64.b64decode(response["body"], validate=True) == wire and
                      not any(secret.encode() in wire for secret in self.secret_values), "Default-format binary response changed or contains credentials")
            check(hashlib.sha256(wire).hexdigest() == raw["observation"]["wireSha256"], "Default-format wire digest changed")
            http_records += 1
            total += len(wire)
        summary = {"status": "passed", "newRecords": len(paths), "newHTTPRecords": http_records, "wireBytes": total,
                   "preservedPrecedingRecordFiles": len(baseline), "preservedInitialAudioRecordFiles": 330,
                   "sourceFilesUnchanged": len(sources), "allThreeSourcesDeliveredOriginal": all(
                       row["headStatus"] == row["getStatus"] == 200 and row["sourceBytesMatch"] for row in results), "results": results}
        private_write(PRIVATE / "audit-summary.json", json.dumps(summary, indent=2), exclusive=True)
        print(json.dumps(summary, indent=2, sort_keys=True))


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("stage", choices=("prepare", "setup", "capture", "controls", "audit", "finish", "defaults"))
    stage = parser.parse_args().stage
    try:
        getattr(Recorder(), stage)()
    except Exception as error:
        print(json.dumps({"status": "failed", "stage": stage, "errorType": type(error).__name__}))
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
