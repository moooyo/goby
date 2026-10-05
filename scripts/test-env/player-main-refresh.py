#!/usr/bin/env python3
"""Verify the schema-61 image refresh in the retained software release scope.

The coordinator owns container replacement, PostgreSQL startup and shutdown.
This helper preserves the original state mount, creates one new bounded media
fixture, and exercises only the scan and persistent-media paths changed at the
main integration. Run it on test-env; never on a developer workstation.
"""

import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


OWNER = "goby-player-release-165c-20261005"
BASE = Path("/opt/goby-test/player-release-20261005-165c")
ACTIVE = {"pending", "running", "stopping"}
TASKS = {
    "background": ("background-preview", "BackgroundPreview", "goby", "mp4"),
    "waveform": ("audio-waveforms", "AudioWaveforms", "goby-waveforms", "gawf"),
    "subtitle-timeline": ("subtitle-timelines", "SubtitleTimelines", "goby-subtitle-timelines", "gstl"),
}


def require(value, message):
    if not value:
        raise RuntimeError(message)


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def sha(data):
    return hashlib.sha256(data).hexdigest()


class Check:
    def __init__(self, root):
        require(os.name == "posix", "Run only on the designated Linux verification host")
        require(root == BASE and root.resolve(strict=True) == root, "Use the original retained release root")
        require((root / ".owner").read_text().strip() == OWNER, "Unexpected release owner")
        self.root = root
        self.old = root / "software-upgrade"
        self.refresh = root / "refresh-20261006"
        require(self.refresh.resolve(strict=True) == self.refresh, "The refresh scope must exist without symlinks")
        self.config = json.loads((self.refresh / "software.private.json").read_text())
        require(self.config["owner"] == OWNER, "Unexpected refresh configuration owner")
        self.private = json.loads((root / "release.private.json").read_text())
        require(self.private["owner"] == OWNER, "Unexpected retained credentials owner")
        self.evidence = self.refresh / "software/evidence"
        self.evidence.mkdir(parents=True, exist_ok=True, mode=0o700)
        require(self.evidence.resolve(strict=True) == self.evidence, "Invalid evidence path")
        self.base = self.origin(self.config["baseURL"])
        self.player = self.origin(self.config["playerURL"])
        self.media = self.old / "media"
        self.state = self.old / "old/state"
        require(self.media.resolve(strict=True) == self.media and self.state.resolve(strict=True) == self.state, "Retain original media and state paths")
        self.name = self.config.get("fixtureName", "Main Refresh 20261006")
        require(re.fullmatch(r"[A-Za-z0-9 -]{1,64}", self.name), "Invalid fixture name")
        self.fixture = self.media / (self.name + " (2026)")
        self.source = self.fixture / (self.name + ".mp4")
        self.inside = "/media/" + str(self.source.relative_to(self.media))
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
        self.csrf = ""
        self.token = ""
        self.user = ""
        self.runs = set()
        self.items = {}
        self.report = {"format": "goby-player-main-refresh-v1", "owner": OWNER, "phases": [], "runs": [], "failures": []}

    @staticmethod
    def origin(value):
        parsed = urllib.parse.urlparse(value)
        require(parsed.scheme == "http" and parsed.hostname in {"127.0.0.1", "localhost", "::1"}
                and not parsed.username and not parsed.password and parsed.path in {"", "/"}, "Use a loopback HTTP origin")
        return value.rstrip("/")

    def write(self, name, value, exclusive=True):
        path = self.evidence / name
        require(path.parent == self.evidence, "Invalid evidence name")
        with path.open("x" if exclusive else "w") as handle:
            json.dump(value, handle, indent=2)
            handle.write("\n")
        path.chmod(0o600)

    def command(self, args, timeout=90):
        value = subprocess.run(args, capture_output=True, timeout=timeout)
        if value.returncode:
            path = self.evidence / ("private-command-" + uuid.uuid4().hex + ".log")
            path.write_bytes(value.stdout + value.stderr)
            path.chmod(0o600)
            raise RuntimeError("Owned command failed; private diagnostics retained: " + args[0])
        return value.stdout

    def sql(self, query):
        return self.command(["docker", "exec", "goby-player-release-165c-pg", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-At",
                             "-U", self.private["databaseRole"], "-d", self.private["softwareDatabase"], "-c", query]).decode().strip()

    def call(self, method, path, body=None, status=200, proxy=False, headers=None):
        base = self.player if proxy else self.base
        url = urllib.parse.urljoin(base + "/", path)
        require(urllib.parse.urlparse(url).netloc == urllib.parse.urlparse(base).netloc, "An API URL escaped the owned origin")
        selected = {"Accept": "application/json", "Origin": base}
        if body is not None:
            selected["Content-Type"] = "application/json"
        if self.csrf and path.startswith("/admin/"):
            selected["X-CSRF-Token"] = self.csrf
        if self.token and path.startswith("/emby/"):
            selected["X-Emby-Token"] = self.token
        if headers:
            selected.update(headers)
        request = urllib.request.Request(url, method=method, headers=selected,
                                         data=json.dumps(body).encode() if body is not None else None)
        try:
            response = self.opener.open(request, timeout=45)
        except urllib.error.HTTPError as error:
            self.write("private-http-" + uuid.uuid4().hex + ".json",
                       {"method": method, "path": path.split("?")[0], "status": error.code, "body": error.read().decode(errors="replace")})
            raise RuntimeError("HTTP failed: " + method + " " + path.split("?")[0] + " " + str(error.code)) from None
        data = response.read()
        require(response.status == status, "Unexpected status: " + method + " " + path.split("?")[0])
        return json.loads(data) if data and "json" in response.headers.get("Content-Type", "") else data

    def login(self):
        value = self.call("POST", "/admin/v1/session", {"Name": self.private["username"], "Password": self.private["password"]})
        self.csrf = value["CSRFToken"]
        authenticated = self.call("POST", "/emby/Users/AuthenticateByName", {"Username": "Upgrade Viewer", "Pw": self.private["password"]},
                                  headers={"X-Emby-Authorization": 'Emby Client="Main refresh acceptance", Device="Remote test", DeviceId="main-refresh-165c", Version="1"'})
        self.token = authenticated["AccessToken"]
        self.user = authenticated["User"]["Id"]
        return value["User"]["Id"], self.user

    def file_identity(self, path, content=False):
        require(path.resolve(strict=True) == path and not path.is_symlink(), "A retained path changed identity")
        info = path.stat()
        value = {"device": info.st_dev, "inode": info.st_ino, "mode": info.st_mode, "uid": info.st_uid, "gid": info.st_gid}
        if content:
            value.update({"sha256": digest(path), "bytes": info.st_size, "mtimeNs": str(info.st_mtime_ns)})
        return value

    def old_publication(self):
        accepted = json.loads((self.old / "evidence/generated-sidecar.json").read_text())
        directory = self.media / accepted["relativeDirectory"]
        require(directory.is_relative_to(self.media), "Retained sidecar path escaped media")
        manifest = directory / "manifest.json"
        video = directory / accepted["manifest"]["Generation"]
        require(digest(manifest) == accepted["manifestSHA256"] and digest(video) == accepted["streamSHA256"]
                and video.stat().st_mtime_ns == accepted["fileMtimeNs"], "Previously accepted sidecar changed")
        return {"manifest": self.file_identity(manifest, True), "video": self.file_identity(video, True),
                "relativeDirectory": accepted["relativeDirectory"]}

    def snapshot(self):
        require(not (self.evidence / "baseline.json").exists(), "The baseline already exists")
        value = {"schema": 61, "state": self.file_identity(self.state),
                 "controls": self.controls(), "sidecar": self.old_publication(),
                 "seed": json.loads((self.old / "evidence/seed-old.json").read_text())}
        self.write("baseline.json", value)
        return {"baselineCaptured": True, "originalStatePath": str(self.state), "oldSidecarUnchanged": True}

    def controls(self):
        values = {}
        for name in ["backups/.goby-backup-store.json", "recovery-operations/.goby-recovery-control.json", "application-key-master.key"]:
            path = self.state / name
            values[name] = self.file_identity(path, True) if path.exists() else None
        require(values["backups/.goby-backup-store.json"] and values["recovery-operations/.goby-recovery-control.json"], "Original fenced controls are missing")
        return values

    def runtime(self):
        values = []
        for key, image, user in [("container", "imageId", "10001:10001"), ("playerContainer", "playerImageId", "101:101")]:
            container = self.config[key]
            require(re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,100}", container), "Invalid container name")
            value = json.loads(self.command(["docker", "inspect", container]))[0]
            require(value["Image"] == self.config[image] and value["State"]["Running"] is True, "Unexpected image or stopped container")
            require(value["Config"]["User"] == user and value["HostConfig"]["ReadonlyRootfs"] is True, "Container isolation changed")
            if key == "container":
                mounts = {entry["Destination"]: entry for entry in value["Mounts"]}
                require(mounts["/var/lib/goby"]["Source"] == str(self.state), "The original fenced state mount was replaced")
                require(mounts["/media"]["Source"] == str(self.media) and mounts["/media"]["RW"], "Use the retained writable media mount")
                actual = self.command(["docker", "exec", "--user=10001:10001", container, "sha256sum", "/usr/local/bin/goby"]).decode().split()[0]
                require(actual == self.config["applicationSHA256"], "Running application digest differs")
            values.append({"container": container, "imageId": value["Image"], "user": user, "readOnlyRoot": True})
        require(int(self.sql("SELECT max(version) FROM schema_migrations")) == 61, "Schema changed from 61")
        return values

    def fixture_prepare(self):
        self.runtime()
        require(not self.fixture.exists(), "Use a fresh fixture directory; retain earlier attempts")
        reference = Path(self.config["externalFixtureRoot"])
        require(reference.is_absolute() and reference.resolve(strict=True) == reference, "Invalid external fixture source")
        oracle = json.loads((reference / "expected.json").read_text())
        self.fixture.mkdir(mode=0o755)
        os.chown(self.fixture, 10001, 10001)
        copied = []
        for record in oracle["Files"]:
            source = reference / record["Name"]
            require(source.parent == reference and source.resolve(strict=True) == source and source.stat().st_size == record["Bytes"]
                    and digest(source) == record["SHA256"], "External fixture digest mismatch")
            target = self.fixture / (self.name + source.suffix)
            with target.open("xb") as handle:
                handle.write(source.read_bytes())
            target.chmod(0o644)
            os.chown(target, 10001, 10001)
            copied.append({"name": target.name, "sha256": digest(target), "bytes": target.stat().st_size})
        self.command(["docker", "exec", "--user=10001:10001", self.config["container"], "/opt/ffmpeg/9.0.1/bin/ffmpeg",
                      "-hide_banner", "-nostdin", "-loglevel", "error", "-n", "-filter_threads", "1",
                      "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=35", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=35",
                      "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads", "1", "-preset", "veryfast", "-crf", "25", "-pix_fmt", "yuv420p",
                      "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", "-color_range", "tv",
                      "-c:a", "aac", "-b:a", "96k", "-movflags", "+faststart", self.inside], timeout=180)
        require(self.source.is_file(), "No authored video was produced")
        value = {"sourcePath": self.inside, "sourceSHA256": digest(self.source), "sourceBytes": self.source.stat().st_size,
                 "durationTicks": 350000000, "externalFiles": copied, "oracle": oracle}
        self.write("fixture.json", value)
        return {key: val for key, val in value.items() if key != "oracle"}

    def phase(self, name, value):
        self.report["phases"].append({"name": name, "passed": True, **value})
        print(json.dumps({"phase": name, "passed": True}), flush=True)

    def preserved(self):
        baseline = json.loads((self.evidence / "baseline.json").read_text())
        require(self.file_identity(self.state) == baseline["state"], "State directory identity changed")
        require(self.controls() == baseline["controls"], "Persistent control or application key identity changed")
        require(self.old_publication() == baseline["sidecar"], "Existing sidecar identity changed")
        admin, viewer = self.login()
        seed = baseline["seed"]
        require(admin == seed["adminId"] and viewer == seed["viewerId"], "Existing account identity changed")
        item = self.call("GET", "/emby/Users/" + viewer + "/Items/" + seed["itemId"])
        require(item["Id"] == seed["itemId"] and item["Name"] == seed["itemName"] and item["RunTimeTicks"] == seed["runtimeTicks"]
                and item["UserData"] == seed["userData"], "Existing item, favorite or progress changed")
        library = next((entry for entry in self.call("GET", "/admin/v1/libraries")["Items"] if entry["Id"] == seed["libraryId"]), None)
        require(library and library["Paths"] == ["/media"], "Existing library identity or root changed")
        for field in ["EnableBackgroundPreviewGeneration", "EnableAudioWaveformGeneration", "EnableSubtitleTimelineGeneration"]:
            require(library["LibraryOptions"].get(field, False) is False, "Automatic generation changed")
        descriptor = self.call("GET", "/emby/Items/" + item["Id"] + "/BackgroundPreview", proxy=True)
        require(descriptor["Available"], "Existing publication unavailable through player proxy")
        payload = self.call("GET", descriptor["StreamUrl"], proxy=True)
        require(sha(payload) == baseline["sidecar"]["video"]["sha256"], "Player proxy changed existing publication")
        require(self.call("GET", "/emby/System/Info/Public", proxy=True)["Id"] == self.call("GET", "/emby/System/Info/Public")["Id"], "Player proxy targets a different server")
        return {"schema": 61, "adminId": admin, "viewerId": viewer, "libraryId": library["Id"], "itemId": item["Id"],
                "userData": item["UserData"], "stateAndControlIdentityPreserved": True, "oldSidecarBytesAndTimestampsPreserved": True,
                "playerImageReused": self.config["playerImageId"], "automaticGenerationEnabled": False}

    def wait(self, check, message, timeout=240):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            value = check()
            if value:
                return value
            time.sleep(0.3)
        raise RuntimeError(message)

    def scan(self):
        seed = json.loads((self.evidence / "baseline.json").read_text())["seed"]
        receipt = self.call("POST", "/admin/v1/libraries/" + seed["libraryId"] + "/scan", {"ForceProbe": False}, 202)
        job_id = receipt["Job"]["Id"]
        def complete():
            job = next((value for value in self.call("GET", "/admin/v1/jobs")["Items"] if value["Id"] == job_id), None)
            require(not job or job["Status"] not in {"failed", "canceled", "cancelled"}, "The real library scan failed")
            return job if job and job["Status"] in {"completed", "succeeded"} else None
        job = self.wait(complete, "The real library scan did not finish")
        matches = [value for value in self.call("GET", "/emby/Items?ParentId=" + seed["libraryId"] + "&Recursive=true&IncludeItemTypes=Movie&Fields=MediaSources,MediaStreams,Path")["Items"]
                   if value.get("Path") == self.inside or any(source.get("Path") == self.inside for source in value.get("MediaSources", []))]
        require(len(matches) == 1, "Expected exactly one new source item")
        item = matches[0]
        require(item["RunTimeTicks"] == 350000000, "Authored runtime changed")
        streams = item["MediaSources"][0]["MediaStreams"]
        subtitle = [value for value in streams if value["Type"] == "Subtitle"]
        require(len(subtitle) == 3 and all(value["IsExternal"] and value["GobySubtitleTimelineOnly"] and not value["SupportsExternalStream"] for value in subtitle), "SUP and both IDX tracks were not discovered")
        require(len([value for value in streams if value["Type"] == "Audio"]) == 1, "Expected one authored tone track")
        return item, {"jobId": job_id, "scanStatus": job["Status"], "itemId": item["Id"], "sourcePath": self.inside, "subtitles": subtitle}

    def generate(self, item, kind):
        suffix = TASKS[kind][0]
        path = "/admin/v1/items/" + item["Id"] + "/" + suffix
        before = self.call("GET", path)
        require(before["State"] == "missing" and not before["Artifact"]["Available"], "Use a fresh source for first-generation acceptance: " + kind)
        if kind == "background":
            profile = self.call("GET", "/admin/v1/background-previews/configuration")["Profile"]
            require(profile["DurationSeconds"] == 25 and profile["MaxWidth"] == 1280, "Expected accepted 25-second profile")
            self.call("PUT", path, {"Revision": before["Revision"], "SourceRevision": before["SourceRevision"], "StartTicks": 10000000})
        body = {"Kind": kind, "RequestId": str(uuid.uuid4()), "LibraryIds": [], "ItemIds": [item["Id"]], "Force": False}
        receipt = self.call("POST", "/admin/v1/media-analysis/runs", body, 202)
        self.runs.add(receipt["RunId"])
        expected = str(int(before["RequestedRevision"]) + 1)
        self.items[kind] = (item["Id"], expected)
        def complete():
            value = self.call("GET", path)
            if value["RequestedRevision"] == expected and value.get("RunId"):
                self.runs.add(value["RunId"])
            return value if value["RequestedRevision"] == expected and value["CompletedRevision"] == expected and value["State"] not in ACTIVE else None
        result = self.wait(complete, "Media generation timed out: " + kind, 600)
        require(result["State"] == "ready" and not result["Reused"] and result["Artifact"]["Available"], "Media generation failed: " + kind + " " + result["State"] + " " + result.get("ErrorCode", ""))
        self.report["runs"].append({"kind": kind, "input": body, "receipt": receipt, "detail": result})
        return result

    def publication(self, kind):
        directory = self.fixture / "backdrops" / TASKS[kind][2] / hashlib.sha256(self.source.name.encode()).hexdigest()
        require(directory.resolve(strict=True) == directory, "Invalid publication directory")
        manifest_path = directory / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        require(manifest["SourceName"] == self.source.name and re.fullmatch("gen-[a-f0-9]{32}\\." + TASKS[kind][3], manifest["Generation"]), "Invalid publication manifest")
        path = directory / manifest["Generation"]
        require(digest(path) == manifest["SHA256"] and not any(entry.name.endswith(".part") for entry in directory.iterdir()), "Invalid or partial publication")
        return {"relativeDirectory": str(directory.relative_to(self.media)), "manifest": manifest,
                "manifestIdentity": self.file_identity(manifest_path, True), "artifactIdentity": self.file_identity(path, True)}

    def payload(self, item, kind):
        descriptor = self.call("GET", "/emby/Items/" + item["Id"] + "/" + TASKS[kind][1], proxy=True)
        require(descriptor["Available"], "Published descriptor unavailable through player proxy")
        if kind == "background":
            data = self.call("GET", descriptor["StreamUrl"], proxy=True)
            publication = self.publication(kind)
            require(sha(data) == publication["manifest"]["SHA256"], "Background stream digest differs")
            path = "/media/" + publication["relativeDirectory"] + "/" + publication["manifest"]["Generation"]
            probe = json.loads(self.command(["docker", "exec", "--user=10001:10001", self.config["container"], "/opt/ffmpeg/9.0.1/bin/ffprobe",
                                             "-v", "error", "-count_frames", "-show_streams", "-show_format", "-of", "json", path]))
            streams = probe["streams"]
            require(len(streams) == 1 and streams[0]["codec_type"] == "video" and streams[0]["codec_name"] == "h264"
                    and streams[0]["pix_fmt"] == "yuv420p" and streams[0]["width"] == 1280 and streams[0]["height"] == 720
                    and int(streams[0]["nb_read_frames"]) == 600 and abs(float(probe["format"]["duration"]) - 25) < 0.1, "Background output contract differs")
            return {"bytes": len(data), "sha256": sha(data), "duration": 25, "frames": 600, "silent": True}
        require(descriptor["DurationTicks"] == item["RunTimeTicks"], "Media descriptor runtime differs")
        if kind == "subtitle-timeline":
            oracle = json.loads((self.evidence / "fixture.json").read_text())["oracle"]
            expected = [track for source in oracle["Sources"] for track in source["Tracks"]]
            subtitles = [value for value in item["MediaSources"][0]["MediaStreams"] if value["Type"] == "Subtitle"]
            require(len(descriptor["Streams"]) == 3, "Expected three timeline descriptors")
            results = []
            for stream in descriptor["Streams"]:
                original = next(value for value in subtitles if value["Index"] == stream["StreamIndex"])
                authored = [value for value in expected if value["Codec"] == original["Codec"] and (value["Language"] or "") == (original.get("Language") or "")]
                require(len(authored) == 1, "Timeline oracle mapping is ambiguous")
                payload = self.call("GET", stream["Url"], proxy=True)
                require(payload["StreamIndex"] == stream["StreamIndex"] and payload["DurationTicks"] == item["RunTimeTicks"]
                        and payload["Intervals"] == authored[0]["Intervals"], "Native bitmap intervals differ from authored control clocks")
                results.append(payload)
            return {"tracks": results, "authoredIntervalsMatch": True}
        audio = [value for value in item["MediaSources"][0]["MediaStreams"] if value["Type"] == "Audio"]
        require([value["StreamIndex"] for value in descriptor["Streams"]] == [value["Index"] for value in audio], "Waveform indexes differ")
        results = []
        for stream in descriptor["Streams"]:
            require(len(stream["Levels"]) == 4, "Expected four waveform levels")
            for level in stream["Levels"]:
                data = self.call("GET", level["Url"], proxy=True)
                require(data[:4] == b"GAWL", "Invalid waveform payload")
                version, header, index, buckets, ticks, mask_bytes, reserved = struct.unpack_from("<HHIIQII", data, 4)
                require((version, header, index, ticks, reserved) == (1, 32, stream["StreamIndex"], item["RunTimeTicks"], 0)
                        and buckets == level["BucketCount"] and mask_bytes == (buckets + 7) // 8 and len(data) == 32 + buckets * 4 + mask_bytes, "Invalid waveform header")
                valid = nonzero = 0
                for offset in range(buckets):
                    peak, rms = struct.unpack_from("<HH", data, 32 + offset * 4)
                    present = bool(data[32 + buckets * 4 + offset // 8] & (1 << (offset % 8)))
                    require(rms <= peak and (present or peak + rms == 0), "Invalid waveform energy")
                    valid += int(present)
                    nonzero += int(present and peak > 0 and rms > 0)
                require(valid > buckets * 0.95 and nonzero > buckets * 0.95, "The authored tone has no waveform energy")
                results.append({"streamIndex": index, "buckets": buckets, "valid": valid, "nonzero": nonzero, "bytes": len(data), "sha256": sha(data)})
        return {"levels": results}

    def verify(self):
        self.phase("schema-61 replacement preserves original identity and publication", {"runtime": self.runtime(), **self.preserved()})
        fixture = json.loads((self.evidence / "fixture.json").read_text())
        require(digest(self.source) == fixture["sourceSHA256"], "Authored source changed")
        item, scanned = self.scan()
        self.phase("real native scan discovers SUP and both IDX+SUB tracks", scanned)
        publications = {}
        for kind in TASKS:
            self.generate(item, kind)
            publications[kind] = self.publication(kind)
            self.phase("new administrator request completes " + kind, {"publication": publications[kind], "payload": self.payload(item, kind)})
        self.phase("old identity and publication remain unchanged after new work", self.preserved())
        self.write("new-publications.json", {"item": item, "publications": publications})
        return {"phases": len(self.report["phases"]), "newItemId": item["Id"]}

    def baseline_live(self):
        value = {"runtime": self.runtime(), **self.preserved()}
        self.phase("original schema-61 installation remains readable before replacement", value)
        return value

    def post_restart(self):
        self.phase("container restart retains original identity and publication", {"runtime": self.runtime(), **self.preserved()})
        accepted = json.loads((self.evidence / "new-publications.json").read_text())
        for kind in TASKS:
            require(self.publication(kind) == accepted["publications"][kind], "Restart changed a new persistent publication")
            self.phase("restart retains " + kind, {"payload": self.payload(accepted["item"], kind)})
        return {"phases": len(self.report["phases"]), "newPublicationsPreserved": True}

    def retire(self):
        for kind, (item, revision) in self.items.items():
            value = self.call("GET", "/admin/v1/items/" + item + "/" + TASKS[kind][0])
            if value["RequestedRevision"] == revision and value.get("RunId"):
                self.runs.add(value["RunId"])
        for run_id in self.runs:
            path = "/admin/v1/task-runs/" + run_id
            if self.call("GET", path)["Run"]["State"] in ACTIVE:
                self.call("POST", path + "/cancel", {})
            self.wait(lambda: self.call("GET", path)["Run"]["State"] not in ACTIVE, "Owned task did not retire", 60)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=BASE)
    parser.add_argument("action", choices=["snapshot", "baseline-live", "fixture", "verify", "post-restart"])
    args = parser.parse_args()
    check = Check(args.root)
    try:
        result = getattr(check, args.action.replace("-", "_") if args.action != "fixture" else "fixture_prepare")()
        check.report.update({"complete": True, "action": args.action, "result": result})
    except Exception as error:
        message = str(error)
        for secret in [check.private.get("password"), check.private.get("databasePassword"), check.private.get("setupToken"), check.token, check.csrf]:
            if secret:
                message = message.replace(secret, "[redacted]")
        check.report.update({"complete": False, "action": args.action})
        check.report["failures"].append(message)
        print(json.dumps({"failed": True, "message": message}), file=sys.stderr)
    finally:
        if check.runs:
            try:
                check.retire()
            except Exception:
                check.report["complete"] = False
                check.report["failures"].append("Owned task retirement failed; coordinator must inspect before shutdown")
        name = args.action + "-" + str(time.time_ns()) + ".json"
        check.write(name, check.report)
    print(json.dumps({"complete": check.report["complete"], "action": args.action, "evidence": str(check.evidence / name)}))
    return 0 if check.report["complete"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
