#!/usr/bin/env python3
"""Observe actual software output for two widths on one disposable Emby.

The single ordinary token is handed off by the separately attested operator.
There is no login, application key, external URL, original media, UI, alternate
protocol, media retry, or guessed session cleanup route in this recorder.
Run only through authorized root SSH after both new scripts are frozen.
"""

from __future__ import annotations

import base64
import copy
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import signal
import stat
import sys
import time
from urllib.parse import parse_qs, quote, urlencode, urlsplit

sys.dont_write_bytecode = True
SANITIZER_SOURCES = {
    "reference-configuration.py": "baa425eb9680d3e924c4d18d75ad99cc6d0a10ac438ba402501955bb96a359cd",
    "reference-scheduled-tasks.py": "b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1",
    "reference-devices.py": "96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d",
    "reference-api-keys.py": "ead25f9d47e8faf0b2b579ca8641b4ddcf4f7993c9e5a16710624c2927e749a8",
}
for dependency_name, expected_hash in SANITIZER_SOURCES.items():
    dependency = Path(__file__).resolve().with_name(dependency_name)
    if dependency.is_symlink() or hashlib.sha256(dependency.read_bytes()).hexdigest() != expected_hash:
        raise RuntimeError("A pinned width recorder dependency differs")
spec = importlib.util.spec_from_file_location("width_configuration_reference", Path(__file__).with_name("reference-configuration.py"))
configuration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(configuration)
device, base = configuration.device, configuration.base
MISSING = device.MISSING
WORK = Path("/opt/goby-test/exec-work-m5h")
EVIDENCE = WORK / "emby-width-fresh-m5h-20260910-02"
DATA = WORK / "emby-width-fresh-data-02"
SOURCE = EVIDENCE / "source"
SOURCE_PATH = SOURCE / "Width Source M5h (2026)/Width Source M5h (2026).mp4"
UNIT = "goby-emby-width-fresh-m5h-20260910-02.service"
MARKER = "goby-emby-width-fresh-m5h-20260910-02-owned-v1"
PORT, HTTPS_PORT, OLD_RECORDS = 18100, 18500, 2305
MANIFEST = EVIDENCE / "private/manifest.json"
OPERATOR_SOURCE = Path(__file__).with_name("prepare-encoding-width-fresh.py")
OPERATOR_SOURCE_SHA256 = "54ca55914049561752cb25e7825ae14a15aaa3d25b7275b9f9e8eba86c6e6c06"
CONTROL_CLIENT = "Goby Encoding Width Fresh M5h 02"
CONTROL_DEVICE = "goby-encoding-width-fresh-m5h-20260910-02-control"
LIBRARY_NAME = "Goby Encoding Width Fresh M5h 02 Owned Movie"
PUBLIC_ROUTE = "/emby/System/Info/Public"
TOTAL_ROUTE = "/emby/System/Configuration"
ENCODING_ROUTE = TOTAL_ROUTE + "/encoding"
LIBRARIES_ROUTE = "/emby/Library/VirtualFolders/Query?StartIndex=0&Limit=2"
STATIC_READS = {PUBLIC_ROUTE, TOTAL_ROUTE, ENCODING_ROUTE, LIBRARIES_ROUTE,
                "/emby/Users", "/emby/Devices", "/emby/Sessions", "/emby/ScheduledTasks"}
WIDTHS = (1280, 0)
MAX_BODY, MAX_REQUEST_BODY = 256 * 1024, 32 * 1024
MAX_JSON_TOTAL, MAIN_JSON_TOTAL = 8 * 1024 * 1024, 6 * 1024 * 1024
MAX_REQUESTS, MAIN_REQUESTS = 140, 104
CREDENTIAL_REQUEST_RESERVE, CREDENTIAL_BYTE_RESERVE = 2, 2 * MAX_BODY
MAX_MEDIA, MAX_MEDIA_TOTAL = 64 * 1024 * 1024, 128 * 1024 * 1024
MAIN_SECONDS, CLEANUP_SECONDS, MEDIA_SECONDS = 720, 180, 120
MAX_LOG_BYTES, MAX_LOG_FILES = 16 * 1024 * 1024, 64
ENCODING_FIELDS = {"EncodingThreadCount", "ExtractionThreadCount", "DownMixAudioBoost", "EnableThrottling", "ThrottleBufferSize",
    "ThrottleHysteresis", "ThrottlingMethod", "H264Crf", "EnableHardwareEncoding", "EnableSubtitleExtraction",
    "EnableOnTheFlyAttachmentExtraction", "CodecConfigurations", "HardwareAccelerationMode", "EnableHardwareToneMapping",
    "EnableSoftwareToneMapping", "TranscodingMaxWidth", "EnableHevcEncoding"}
APP = Path("/dev/shm/goby-emby-reference/package/opt/emby-server")
_operator = None
base.__file__ = __file__
base.DATA, base.RUNTIME = DATA, EVIDENCE / "runtime"
base.ROOT = EVIDENCE / "runtime/width-capture"
base.PRIVATE, base.RAW, base.EXPORT = base.ROOT / "private", base.ROOT / "private/raw", base.ROOT / "export"
base.PREFIX, base.MARKER = "encoding-width-fresh-m5h-2-", "goby-reference-encoding-width-fresh-m5h-02-owned-v1"


def operator_module():
    global _operator
    base.require(re.fullmatch(r"[0-9a-f]{64}", OPERATOR_SOURCE_SHA256) is not None and
                 base.digest(OPERATOR_SOURCE) == OPERATOR_SOURCE_SHA256, "Width operator is not the frozen reviewed source")
    if _operator is None:
        specification = importlib.util.spec_from_file_location("encoding_width_operator_authority", OPERATOR_SOURCE)
        _operator = importlib.util.module_from_spec(specification)
        specification.loader.exec_module(_operator)
    base.require(_operator.ROOT == EVIDENCE and _operator.DATA == DATA and _operator.UNIT == UNIT and _operator.PORT == PORT and
                 _operator.MARKER == MARKER and _operator.SANITIZER_SOURCES == SANITIZER_SOURCES, "Width operator constants differ")
    return _operator


def load_manifest() -> dict:
    op = operator_module()
    manifest = op.load(MANIFEST)
    required = {"state": "READY", "fixtureAttempt": 2, "marker": MARKER, "unit": UNIT, "port": PORT, "httpsPort": HTTPS_PORT,
        "programData": str(DATA), "evidenceRoot": str(EVIDENCE), "operatorSha256": OPERATOR_SOURCE_SHA256,
        "sanitizerSources": SANITIZER_SOURCES, "allFreshProgramDataOwned": True, "oldPreservationVerified": True,
        "controlCredentialState": "LIVE_HANDOFF", "ordinaryLoginCount": 1, "controlDeviceId": CONTROL_DEVICE,
        "controlClient": CONTROL_CLIENT, "sourceRoot": str(SOURCE), "sourcePath": str(SOURCE_PATH),
        "controlCredentialsFile": str(EVIDENCE / "private/control-credentials.env"),
        "controlLoginResponseFile": str(EVIDENCE / "private/control-login-response.json")}
    base.require(all(manifest.get(key) == value for key, value in required.items()), "Width READY manifest differs")
    base.require(isinstance(manifest.get("serverId"), str) and manifest["serverId"] and manifest["serverId"] != op.OLD_SERVER_ID and
                 manifest.get("bootstrapLibraries") == [] and manifest.get("bootstrapLibraryMutationRequests") == 0 and
                 manifest.get("bootstrapTaskMutationRequests") == 0 and manifest.get("bootstrapApplicationKeyRequests") == 0,
                 "The width fixture is not a separate initially empty instance")
    base.require(len(manifest.get("oldBaseline", {}).get("records", {})) == OLD_RECORDS * 2 and
                 len(manifest["oldBaseline"].get("media", {})) == 240, "Protected width corpus membership differs")
    failed = manifest.get("failedFixtureProvenance")
    base.require(isinstance(failed, dict) and failed == manifest["oldBaseline"].get("failedFixtureProvenance") and
                 failed.get("evidenceRoot") == str(WORK / "emby-width-fresh-m5h-20260910-01") and
                 failed.get("programData") == str(WORK / "emby-width-fresh-data-01") and
                 failed.get("dataAbsent") is True and failed.get("sourceAbsent") is True and failed.get("noOrdinaryLogin") is True,
                 "The failed first fixture is not preserved as a separate completed cleanup")
    base.require(manifest.get("serviceProperties", {}).get("PrivateDevices") == "yes" and
                 manifest.get("serviceProperties", {}).get("MemoryMax") == str(1152 * 1024 * 1024), "Fresh encoding limits differ")
    op.verify_source(manifest["sourceIdentity"])
    return manifest


def preconditions() -> dict:
    base.require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")), "Run only through authorized root SSH")
    op, manifest = operator_module(), load_manifest()
    op.common_preconditions(host=False)
    expected = op.load(op.IDENTITY)
    op.same_new_identity(expected)
    base.require(all(manifest.get(name) == value for name, value in expected.items()), "READY and fresh process attestation differ")
    op.check_old_services(manifest["oldServices"])
    op.verify_baseline(manifest["oldBaseline"])
    namespaces = {row["networkNamespace"] for row in manifest["oldServices"].values()}
    base.require(expected["networkNamespace"] not in namespaces and expected["networkNamespace"] != os.readlink("/proc/1/ns/net"),
                 "The disposable namespace overlaps a protected authority")
    if os.readlink("/proc/self/ns/net") != expected["networkNamespace"]:
        os.execvp("nsenter", ["nsenter", "-t", str(expected["pid"]), "-n", sys.executable, "-B", str(Path(__file__).resolve())])
    base.require(os.readlink("/proc/self/ns/net") == expected["networkNamespace"], "Recorder is outside the disposable network namespace")
    return manifest


def encoding_body(baseline: dict, width: int) -> dict:
    base.require(isinstance(baseline, dict) and set(baseline) == ENCODING_FIELDS and type(width) is int and width in WIDTHS,
                 "Encoding experiment lacks the observed complete field set or fixed width")
    base.require(type(baseline["TranscodingMaxWidth"]) is int and baseline["TranscodingMaxWidth"] == 0 and
                 type(baseline["EncodingThreadCount"]) is int and type(baseline["EnableHardwareEncoding"]) is bool,
                 "Encoding baseline types or initial width differ")
    body = copy.deepcopy(baseline)
    body.update({"TranscodingMaxWidth": width, "EnableHardwareEncoding": False, "EncodingThreadCount": 1})
    return body


def playback_body(user_id: str, source_id: str, video_index: int, audio_index: int) -> dict:
    base.require(bool(user_id) and bool(source_id) and type(video_index) is int and type(audio_index) is int and
                 min(video_index, audio_index) >= 0 and video_index != audio_index, "Playback source stream identity differs")
    profile = {"Type": "Video", "Container": "mp4", "VideoCodec": "h264", "AudioCodec": "aac", "MaxAudioChannels": "2",
               "Protocol": "http", "Context": "Streaming"}
    return {"UserId": user_id, "MediaSourceId": source_id, "IsPlayback": True, "EnableDirectPlay": False, "EnableDirectStream": False,
        "EnableTranscoding": True, "AllowVideoStreamCopy": False, "AllowAudioStreamCopy": False, "StartTimeTicks": 0,
        "VideoStreamIndex": video_index, "AudioStreamIndex": audio_index, "SubtitleStreamIndex": -1, "MaxStreamingBitrate": 100000000,
        "DeviceProfile": {"Name": "Goby Width M5h 02 Fixed Software MP4", "MaxStreamingBitrate": 100000000,
                          "DirectPlayProfiles": [], "TranscodingProfiles": [profile]}}


def library_body() -> dict:
    return {"Name": LIBRARY_NAME, "CollectionType": "movies", "Paths": [str(SOURCE)], "RefreshLibrary": False,
        "LibraryOptions": {"PathInfos": [{"Path": str(SOURCE)}], "SaveLocalMetadata": False, "MetadataSavers": [],
            "EnableRealtimeMonitor": False, "SampleIgnoreSize": 0, "EnableAutomaticSeriesGrouping": False,
            "EnableChapterImageExtraction": False, "EnableMarkerDetection": False, "AutomaticRefreshIntervalDays": 0,
            "TypeOptions": [{"Type": "Movie", "MetadataFetchers": [], "MetadataFetcherOrder": [], "ImageFetchers": [], "ImageFetcherOrder": []}]}}


def software_command(arguments: list[str]) -> dict:
    """Require an actual owned input and libx264 command, not a profile label."""
    base.require(isinstance(arguments, list) and 1 < len(arguments) <= 512 and all(isinstance(item, str) for item in arguments),
                 "Producer command is not a bounded argument list")
    executable = Path(arguments[0])
    base.require(executable.is_absolute() and ".." not in executable.parts and executable.name == "ffmpeg" and APP in executable.parents,
                 "Producer executable is outside the shared official package")
    input_positions = [index for index, item in enumerate(arguments[:-1]) if item == "-i"]
    inputs = [arguments[index + 1] for index in input_positions]
    base.require(len(inputs) == 1 and inputs[0] in {str(SOURCE_PATH), "file:" + str(SOURCE_PATH)}, "Producer input is not the sole owned source")
    output_arguments = arguments[input_positions[0] + 2:]
    encoders = [output_arguments[index + 1] for index, item in enumerate(output_arguments[:-1])
                if re.fullmatch(r"-(?:(?:c|codec):v(?::[0-9]+)?|vcodec)", item)]
    base.require(encoders == ["libx264"], "Actual video command is not exactly software libx264")
    base.require(not any(re.search(r"(?i)(?:nvenc|cuvid|cuda|vaapi|qsv|videotoolbox|v4l2m2m|amf|vulkan|opencl)", item)
                         for item in arguments), "Actual producer requests hardware acceleration")
    base.require(not any(item.startswith(("-init_hw_device", "-hwaccel_device", "-hwaccel_output_format")) for item in arguments) and
                 all(arguments[index + 1] == "none" for index, item in enumerate(arguments[:-1]) if item == "-hwaccel"),
                 "An automatic or explicit hardware decoder request is prohibited")
    for index, argument in enumerate(arguments[:-1]):
        if re.fullmatch(r"-(?:(?:c|codec)(?::[av](?::[0-9]+)?)?|[av]codec)", argument):
            base.require(arguments[index + 1] != "copy", "Actual command uses a prohibited stream copy")
    output = output_arguments[-1] if output_arguments else ""
    destination = Path(output)
    formats = [output_arguments[index + 1] for index, item in enumerate(output_arguments[:-1]) if item == "-f"]
    base.require(formats == ["mp4"] and ((destination.is_absolute() and ".." not in destination.parts and
                 destination.suffix.lower() == ".mp4" and DATA in destination.parents) or output in {"pipe:1", "-"}),
                 "Actual software producer does not emit owned progressive MP4")
    return {"videoEncoder": "libx264", "softwareVideoCommandProved": True, "inputMatchesOwnedSource": True,
            "outputDestination": output, "outputFormat": "mp4",
            "commandSha256": hashlib.sha256(json.dumps(arguments, separators=(",", ":")).encode()).hexdigest()}


def save_binary(path: Path, content: bytes) -> None:
    base.require(base.PRIVATE in path.parents and isinstance(content, bytes), "Binary evidence must remain in the owned private root")
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as output:
        output.write(content)
        output.flush()
        os.fsync(output.fileno())


class Recorder(configuration.Recorder):
    def __init__(self, manifest: dict) -> None:
        self.manifest = manifest
        self.authority = operator_module().load(operator_module().IDENTITY)
        self.pid = self.authority["pid"]
        self.started, self.finishing = time.monotonic(), False
        self.deadline = self.started + MAIN_SECONDS
        self.secrets, self.forbidden_tokens = set(), set()
        self._secret_signature, self._secret_regex = None, None
        self.logins, self.invalid_login_proofs, self.logout_statuses = {}, {}, {}
        self.invalid_tokens, self.logged_out = set(), set()
        self.record_count = self.total = self.charged_bytes = self.incomplete_count = self.media_bytes = 0
        self.labels, self.mutations, self.wire_records = set(), [], {}
        self.persistence_failures, self.cleanup_errors, self.checks = [], [], {}
        self.pending, self.current_width = None, None
        self.library_attempted, self.refresh_attempted, self.library_id = False, False, None
        self.item, self.source_id, self.video_index, self.audio_index = None, None, None, None
        self.encoding_baseline, self.total_baseline = None, None
        self.encoding_dirty, self.restored, self.writes_blocked = False, False, False
        self.width_attempts, self.playback_attempts, self.restore_attempts = set(), set(), set()
        self.sessions, self.media_attempts, self.stop_attempts, self.stopped_sessions = {}, set(), set(), set()
        self.cleanup_stop_retries, self.cleanup_restore_retries, self.stop_acks = set(), set(), set()
        self.capture_failure, self.cleanup_ok = None, False
        self.case_results, self.restorations = [], []
        self.fresh_users, self.fresh_devices, self.fresh_tasks = None, None, None
        self.control_user_id, self.control_token = None, None
        self.evidence_unavailable = False
        base.require(not base.ROOT.exists() and not base.ROOT.is_symlink(), "Refusing to overwrite a prior width capture")
        self.load_handoff()
        self.baseline = self.snapshot()
        os.umask(0o077)
        base.ROOT.mkdir(mode=0o700)
        for folder in (base.PRIVATE, base.RAW, base.EXPORT, base.PRIVATE / "wire", base.PRIVATE / "mutations",
                       base.PRIVATE / "media", base.PRIVATE / "producer"):
            folder.mkdir(mode=0o700)
        base.save(base.ROOT / ".goby-managed", base.MARKER + "\n")
        base.save(base.PRIVATE / "baseline.json", self.baseline)

    def load_handoff(self) -> None:
        path = Path(self.manifest["controlCredentialsFile"])
        base.private_file(path)
        values = dict(line.split("=", 1) for line in path.read_text().splitlines())
        base.require(set(values) == {"REFERENCE_USERNAME", "REFERENCE_PASSWORD", "REFERENCE_TOKEN", "REFERENCE_USER_ID", "REFERENCE_DEVICE_ID"},
                     "The handoff credential fields differ")
        token = values["REFERENCE_TOKEN"]
        base.require(token and hashlib.sha256(token.encode()).hexdigest() == self.manifest["controlTokenSha256"] and
                     values["REFERENCE_USER_ID"] == self.manifest["controlUserId"] and values["REFERENCE_DEVICE_ID"] == CONTROL_DEVICE,
                     "The live ordinary credential differs from READY")
        response = operator_module().load(Path(self.manifest["controlLoginResponseFile"]))
        base.require(response.get("AccessToken") == token and response.get("User", {}).get("Id") == values["REFERENCE_USER_ID"] and
                     response.get("User", {}).get("Policy", {}).get("IsAdministrator") is True and
                     response.get("SessionInfo", {}).get("DeviceId") == CONTROL_DEVICE,
                     "The live handoff is not the exact acknowledged ordinary administrator login")
        self.control_token, self.control_user_id = token, values["REFERENCE_USER_ID"]
        self.logins["control"] = response
        self.secrets.update(value for key, value in values.items() if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)

    def snapshot(self) -> dict:
        op, old = operator_module(), self.manifest["oldBaseline"]
        op.verify_baseline(old)
        op.verify_source(self.manifest["sourceIdentity"])
        raw, exported = sorted((EVIDENCE / "private/raw").glob("*.json")), sorted((EVIDENCE / "export").glob("*.json"))
        count = self.manifest.get("setupRecordCount")
        base.require(type(count) is int and 0 < count <= 64 and len(raw) == len(exported) == count and
                     {path.name for path in raw} == {path.name for path in exported}, "Fresh setup record membership differs")
        private = []
        for path in (EVIDENCE / "private").rglob("*"):
            base.require(not path.is_symlink(), "Setup evidence contains a symbolic link")
            if path.is_file():
                private.append(path)
        base.require(len(private) <= 1024 and sum(path.stat().st_size for path in private) <= 64 * 1024 * 1024,
                     "Setup private evidence exceeds its bounded membership")
        for path in [*private, *exported]:
            base.private_file(path)
        return {"records": {**old["records"], **{str(path): base.digest(path) for path in [*raw, *exported]}},
            "media": old["media"], "privateFiles": {**old["privateFiles"], **{str(path): base.digest(path) for path in private}},
            "retainedHistoricalFiles": old.get("retainedHistoricalFiles", {}), "historicalRemovedPaths": old.get("historicalRemovedPaths", []),
            "retiredOwnedSources": old.get("retiredOwnedSources", {}), "failedFixtureProvenance": old["failedFixtureProvenance"],
            "ownedWidthSource": copy.deepcopy(self.manifest["sourceIdentity"])}

    def check_authority(self) -> None:
        operator_module().same_new_identity(self.authority)
        base.require(os.readlink("/proc/self/ns/net") == self.authority["networkNamespace"] and
                     self.authority["networkNamespace"] not in {row["networkNamespace"] for row in self.manifest["oldServices"].values()},
                     "A request attempted to leave the attested fresh namespace")

    def verify_saved_baseline(self) -> bool:
        op = operator_module()
        op.verify_baseline(self.manifest["oldBaseline"])
        op.verify_source(self.manifest["sourceIdentity"])
        for group in ("records", "media", "privateFiles", "retainedHistoricalFiles"):
            for name, expected in self.baseline.get(group, {}).items():
                path = Path(name)
                base.require(path.resolve(strict=True) == path and not path.is_symlink() and stat.S_ISREG(path.lstat().st_mode) and
                             base.digest(path) == expected, "A retained baseline source or evidence file changed")
        raw, exported = sorted((EVIDENCE / "private/raw").glob("*.json")), sorted((EVIDENCE / "export").glob("*.json"))
        base.require(len(raw) == len(exported) == self.manifest["setupRecordCount"] and
                     {path.name for path in raw} == {path.name for path in exported}, "Bootstrap HTTP membership changed")
        # Operator local probes add new PRIVATE/media-*.json evidence. Such
        # additions cannot modify any of the immutable setup paths above.
        return True

    def admin(self) -> str:
        return self.control_token

    def persist(self, label: str, operation, *, cleanup: bool = False) -> object:
        if self.evidence_unavailable:
            failure = {"stage": label, "error": "CaptureInitializationIncomplete"}
            self.persistence_failures.append(failure)
            self.cleanup_errors.append(failure)
            return None
        return super().persist(label, operation, cleanup=cleanup)

    def item_query(self) -> str:
        return "/emby/Items?" + urlencode({"UserId": self.control_user_id, "Recursive": "true", "Path": str(SOURCE_PATH),
                                         "Fields": "Path,MediaSources,MediaStreams", "StartIndex": "0", "Limit": "2"})

    def refresh_path(self) -> str:
        base.require(isinstance(self.library_id, str) and re.fullmatch(r"[A-Za-z0-9_-]+", self.library_id), "Owned library identity is missing")
        return "/emby/Items/" + self.library_id + "/Refresh?Recursive=true&MetadataRefreshMode=FullRefresh&ImageRefreshMode=FullRefresh"

    def playback_path(self) -> str:
        base.require(self.item is not None, "No uniquely indexed owned source")
        return "/emby/Items/" + self.item["Id"] + "/PlaybackInfo"

    def stop_path(self, session_id: str) -> str:
        base.require(session_id in self.sessions, "Encoder cleanup requires a DTO-acknowledged owned session")
        return "/emby/Videos/ActiveEncodings?" + urlencode({"DeviceId": CONTROL_DEVICE, "PlaySessionId": session_id})

    def approve(self, kind: str, method: str, path: str, body: object = MISSING) -> None:
        base.require(self.pending is None, "An owned mutation approval is already pending")
        self.pending = {"kind": kind, "method": method, "path": path, "body": body if body is MISSING else copy.deepcopy(body),
                        "width": self.current_width}
        try:
            self.authorize(method, path, self.admin(), body)
        except Exception:
            self.pending = None
            raise

    def authorize(self, method: str, path: str, token: str, body: object, identity: str = "") -> None:
        parsed = urlsplit(path)
        base.require(not identity and not parsed.scheme and not parsed.netloc and not parsed.fragment and path.startswith("/emby/") and
                     "\\" not in path and len(path.encode()) <= 4096 and not any(ord(char) < 32 for char in path), "Unexpected width API path")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(values) == 1 for values in query.values()), "Duplicate query fields are prohibited")
        base.require(token == self.admin() or not token and path == PUBLIC_ROUTE, "Request token is outside the single live handoff")
        if method == "GET" and body is MISSING:
            base.require(path in STATIC_READS or path == self.item_query(), "GET is outside the fixed width read allowlist")
            return
        if method == "POST" and path == "/emby/Sessions/Logout" and body is MISSING:
            base.require(self.finishing and token == self.admin(), "Logout is outside owned cleanup")
            return
        plan = self.pending
        base.require(plan is not None and method == plan["method"] and path == plan["path"] and token == self.admin() and
                     (body is MISSING and plan["body"] is MISSING or configuration.same_value(body, plan["body"])), "Mutation lacks its exact pending approval")
        kind = plan["kind"]
        base.require(not self.writes_blocked or kind in {"encoding-restore", "encoder-stop"}, "Ordinary writes are blocked after a failure")
        if kind == "library-create":
            valid = (not self.finishing and not self.library_attempted and self.library_id is None and method == "POST" and
                path == "/emby/Library/VirtualFolders" and configuration.same_value(body, library_body()))
        elif kind == "library-refresh":
            valid = (not self.finishing and self.library_attempted and not self.refresh_attempted and method == "POST" and
                path == self.refresh_path() and body == {})
        elif kind == "encoding-width":
            valid = (not self.finishing and self.restored and not self.encoding_dirty and self.current_width in WIDTHS and
                self.current_width not in self.width_attempts and not self.producer_snapshot() and method == "POST" and path == ENCODING_ROUTE and
                configuration.same_value(body, encoding_body(self.encoding_baseline, self.current_width)))
        elif kind == "encoding-restore":
            can_attempt = self.current_width not in self.restore_attempts or self.finishing and self.current_width not in self.cleanup_restore_retries
            valid = (self.encoding_baseline is not None and self.current_width in WIDTHS and can_attempt and
                self.media_attempts <= self.stopped_sessions and not self.producer_snapshot() and method == "POST" and path == ENCODING_ROUTE and
                configuration.same_value(body, self.encoding_baseline))
        elif kind == "playback-info":
            valid = (not self.finishing and self.current_width in self.width_attempts and self.current_width not in self.playback_attempts and
                self.encoding_dirty and method == "POST" and path == self.playback_path() and
                configuration.same_value(body, playback_body(self.control_user_id, self.source_id, self.video_index, self.audio_index)))
        elif kind == "encoder-stop":
            matches = [session for session in self.sessions if path == self.stop_path(session)]
            valid = (len(matches) == 1 and (matches[0] not in self.stop_attempts or self.finishing and matches[0] not in self.cleanup_stop_retries) and
                     method == "DELETE" and body is MISSING)
        else:
            valid = False
        base.require(valid, "Mutation is outside the bounded owned width experiment")

    def consume(self, plan: dict | None) -> None:
        if plan is None:
            return
        self.pending = None
        kind = plan["kind"]
        if kind == "library-create":
            self.library_attempted = True
        elif kind == "library-refresh":
            self.refresh_attempted = True
        elif kind == "encoding-width":
            self.width_attempts.add(self.current_width)
            self.encoding_dirty, self.restored = True, False
        elif kind == "encoding-restore":
            if self.current_width in self.restore_attempts:
                self.cleanup_restore_retries.add(self.current_width)
            self.restore_attempts.add(self.current_width)
        elif kind == "playback-info":
            self.playback_attempts.add(self.current_width)
        elif kind == "encoder-stop":
            session = next(session for session in self.sessions if plan["path"] == self.stop_path(session))
            if session in self.stop_attempts:
                self.cleanup_stop_retries.add(session)
            self.stop_attempts.add(session)

    def ack_playback(self, status: int | None, result: object, complete: bool, label: str) -> None:
        if not complete or status != 200 or not isinstance(result, dict):
            return
        session = result.get("PlaySessionId")
        base.require(isinstance(session, str) and re.fullmatch(r"[A-Za-z0-9_-]{1,128}", session) and session not in self.sessions,
                     "PlaybackInfo did not acknowledge a unique bounded session")
        # This complete response to our owned PlaybackInfo request establishes
        # cleanup ownership before response persistence or media validation.
        self.sessions[session] = {"width": self.current_width, "capture": label, "mediaPath": None}

    def request(self, label: str, method: str, path: str, *, token: str = "", body: object = MISSING,
                identity: str = "") -> tuple[int, object]:
        self.check_authority()
        self.authorize(method, path, token, body, identity)
        base.require(re.fullmatch(r"[a-z0-9-]+", label) and label not in self.labels, "HTTP label is unsafe or already consumed")
        retiring = self.finishing and token == self.admin() and path in {"/emby/Sessions/Logout", "/emby/Sessions"}
        request_limit = MAX_REQUESTS if retiring else MAX_REQUESTS - CREDENTIAL_REQUEST_RESERVE if self.finishing else MAIN_REQUESTS
        base.require(self.record_count < request_limit, "Width request budget exhausted")
        remaining = self.deadline - time.monotonic()
        phase_bytes = MAX_JSON_TOTAL if retiring else MAX_JSON_TOTAL - CREDENTIAL_BYTE_RESERVE if self.finishing else MAIN_JSON_TOTAL
        limit = min(MAX_BODY, phase_bytes - self.charged_bytes)
        base.require(remaining > 0 and limit > 0, "Width phase time or JSON byte budget exhausted")
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="' + CONTROL_CLIENT + '", DeviceId="' + CONTROL_DEVICE + '", Device="Linux Fresh Test", Version="0.1.0"'}
        if token:
            headers["X-Emby-Token"] = token
        wire = None
        if body is not MISSING:
            wire = json.dumps(body, separators=(",", ":")).encode()
            headers["Content-Type"] = "application/json"
            base.require(len(wire) <= MAX_REQUEST_BODY, "Width request body exceeds its bound")
        plan = self.pending if method != "GET" and path != "/emby/Sessions/Logout" else None
        request = {"method": method, "path": path, "headers": headers, "bodyPresent": body is not MISSING,
                   "body": None if body is MISSING else body, "widthExperiment": self.current_width,
                   "approvedMutationKind": plan["kind"] if plan else None}
        mutation = None
        if method != "GET":
            mutation = {"label": label, "request": request, "status": None, "transportFailure": None}
            self.mutations.append(mutation)
            self.persist("intent-" + label, lambda: base.save(base.PRIVATE / "mutations" / (str(len(self.mutations)).zfill(3) + "-intent.json"), request), cleanup=self.finishing)
        self.labels.add(label)
        self.record_count += 1
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=min(5, remaining))
        status, reason, version, response_headers, content, failure, complete = None, None, None, [], b"", None, False
        signal.setitimer(signal.ITIMER_REAL, min(15, remaining))
        try:
            self.consume(plan)
            connection.request(method, path, wire, headers)
            response = connection.getresponse()
            status, reason, version, response_headers = response.status, response.reason, response.version, response.getheaders()
            content = response.read(limit)
            complete = response.isclosed() or response.length == 0
            declared = [value for name, value in response_headers if name.lower() == "content-length"]
            if declared:
                complete = complete and len(set(declared)) == 1 and declared[0].isdigit() and int(declared[0]) == len(content)
        except Exception as error:
            failure = type(error).__name__
            if isinstance(error, http.client.IncompleteRead):
                content = error.partial[:limit]
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        self.total += len(content)
        self.charged_bytes += len(content) if complete else limit
        self.incomplete_count += int(not complete)
        exportable = True
        try:
            text = content.decode("utf-8", errors="strict")
            try:
                result, kind = json.loads(text), "json"
            except json.JSONDecodeError:
                result, kind = text, "text"
        except UnicodeDecodeError:
            result, kind, exportable = None, "private-binary", False
        if mutation is not None:
            mutation.update({"status": status, "transportFailure": failure})
        if plan and plan["kind"] == "encoder-stop" and complete and status in {200, 204}:
            self.stop_acks.add(next(session for session in self.sessions if path == self.stop_path(session)))
        acknowledgement_error = None
        if plan and plan["kind"] == "playback-info":
            try:
                self.ack_playback(status, result, complete, label)
            except Exception as error:
                acknowledgement_error = type(error).__name__
                self.writes_blocked = True
        if complete and path == "/emby/Sessions" and token == self.admin() and status == 401:
            self.invalid_tokens.add(token)
            self.invalid_login_proofs["control"] = {"label": label, "status": 401}
        elif complete and path == "/emby/Sessions" and token == self.admin():
            self.invalid_tokens.discard(token)
            self.invalid_login_proofs.pop("control", None)
        if path == "/emby/Sessions/Logout":
            self.logout_statuses["control"] = status
        if not complete or not exportable:
            self.writes_blocked = True
        wire_path = base.PRIVATE / "wire" / (label + ".json")
        self.wire_records[label] = {"path": str(wire_path), "sha256": None, "completeHTTP": complete, "persisted": False}
        def save_wire():
            base.save(wire_path, {"request": request, "requestBodyBase64": base64.b64encode(wire or b"").decode(),
                "responseStatus": status, "responseReason": reason, "responseVersion": version, "responseHeaders": response_headers,
                "responseBodyBase64": base64.b64encode(content).decode(), "completeHTTP": complete, "transportFailure": failure})
            return base.digest(wire_path)
        digest = self.persist("wire-" + label, save_wire, cleanup=self.finishing)
        self.wire_records[label].update({"sha256": digest, "persisted": digest is not None})
        self.persist("record-" + label, lambda: self.write(label, {"request": request,
            "response": {"status": status, "reason": reason, "httpVersion": version, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": complete, "bodyExportable": exportable, "wireBytes": len(content), "transportFailure": failure,
                            "acknowledgementFailure": acknowledgement_error, "privateWireSha256": digest}}), cleanup=self.finishing)
        self.persist("progress-" + label, lambda: print(json.dumps({"capture": base.PREFIX + label, "status": status,
                     "completeHTTP": complete, "bytes": len(content)}), flush=True), cleanup=self.finishing)
        base.require(complete and exportable, "Response was not complete bounded UTF-8 evidence")
        base.require(acknowledgement_error is None, "PlaybackInfo did not provide a usable owned session acknowledgment")
        return status, result

    def producer_snapshot(self) -> list[dict]:
        group = self.authority["serviceProperties"]["ControlGroup"]
        base.require(group == "/system.slice/" + UNIT, "Fresh cgroup identity differs")
        folder = Path("/sys/fs/cgroup") / group.lstrip("/")
        members = (folder / "cgroup.procs").read_text().splitlines()
        base.require(len(members) <= 256 and all(value.isdigit() for value in members), "Fresh cgroup membership exceeds its bound")
        producers = []
        for value in members:
            process = Path("/proc") / value
            try:
                executable = os.readlink(process / "exe")
                if Path(executable).name != "ffmpeg":
                    continue
                base.require(APP in Path(executable).parents and os.readlink(process / "ns/net") == self.authority["networkNamespace"],
                             "An encoder escaped the shared package or owned namespace")
                content = (process / "cmdline").read_bytes()
                base.require(len(content) <= 65536 and process.stat().st_uid == 0, "Producer command or owner differs")
                arguments = [value.decode("utf-8", errors="strict") for value in content.split(b"\0") if value]
                producers.append({"pid": int(value), "startTicks": (process / "stat").read_text().rsplit(")", 1)[1].split()[19],
                    "networkNamespace": os.readlink(process / "ns/net"), "executable": executable, "cmdline": arguments})
            except (FileNotFoundError, ProcessLookupError):
                continue
        base.require(len(producers) <= 1, "Concurrent encoders exceed the single-producer budget")
        return producers

    def media_path(self, result: dict, session_id: str) -> str:
        base.require(session_id in self.sessions and result.get("PlaySessionId") == session_id, "Media route lacks a DTO session acknowledgment")
        sources = result.get("MediaSources")
        base.require(isinstance(sources, list) and len(sources) == 1 and sources[0].get("Id") == self.source_id,
                     "PlaybackInfo did not return the sole owned media source")
        value = sources[0].get("TranscodingUrl")
        base.require(isinstance(value, str) and 0 < len(value.encode()) <= 4096, "A bounded progressive TranscodingUrl is missing")
        parsed = urlsplit(value)
        base.require(not parsed.scheme and not parsed.netloc and not parsed.fragment and "\\" not in value and
                     not any(ord(character) < 32 for character in value), "External or malformed media URLs are prohibited")
        expected = "/videos/" + self.item["Id"] + "/stream.mp4"
        base.require(parsed.path in {expected, "/emby" + expected}, "The returned route is not the owned progressive MP4")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(values) == 1 for values in query.values()), "Duplicate media parameters are prohibited")
        lowered = {name.lower(): values[0] for name, values in query.items()}
        base.require(len(lowered) == len(query), "Case-ambiguous media parameters are prohibited")
        needed = {"deviceid": CONTROL_DEVICE, "mediasourceid": self.source_id, "playsessionid": session_id,
                  "api_key": self.admin(), "videocodec": "h264", "audiocodec": "aac", "allowvideostreamcopy": "false", "allowaudiostreamcopy": "false"}
        base.require(all(lowered.get(key) == value for key, value in needed.items()), "Returned media identity, codec, or copy policy differs")
        base.require(lowered.get("static", "false").lower() != "true", "A direct static stream is prohibited")
        path = value if parsed.path.startswith("/emby/") else "/emby" + value
        self.sessions[session_id]["mediaPath"] = path
        return path

    def log_snapshot(self) -> dict:
        folder = DATA / "logs"
        base.require(folder.resolve(strict=True) == folder and not folder.is_symlink(), "Fresh log root is not canonical")
        paths = sorted(path for path in folder.rglob("*") if path.is_file())
        base.require(len(paths) <= MAX_LOG_FILES and sum(path.stat().st_size for path in paths) <= MAX_LOG_BYTES, "Fresh logs exceed the evidence bound")
        result = {}
        for path in paths:
            info = path.lstat()
            base.require(not path.is_symlink() and path.resolve(strict=True) == path and stat.S_ISREG(info.st_mode) and
                         info.st_uid == 0 and info.st_nlink == 1, "A fresh log is not an owned regular file")
            result[str(path)] = {"size": info.st_size, "sha256": base.digest(path), "device": info.st_dev, "inode": info.st_ino}
        return result

    def producer_proof(self, label: str, observed: list[dict], before_logs: dict) -> dict:
        after = self.log_snapshot()
        commands, retained_logs = [], []
        for row in observed:
            proof = software_command(row["cmdline"])
            commands.append({"origin": "live-owned-cgroup", "pid": row["pid"], "startTicks": row["startTicks"], **proof})
        for name, facts in after.items():
            if before_logs.get(name) == facts:
                continue
            content = Path(name).read_bytes()
            base.require(len(content) <= MAX_LOG_BYTES, "A producer log grew beyond its bound")
            target = base.PRIVATE / "producer" / (label + "-log-" + str(len(retained_logs) + 1) + ".bin")
            save_binary(target, content)
            retained_logs.append({"originalPath": name, "privatePath": str(target), "bytes": len(content), "sha256": base.digest(target)})
            previous = before_logs.get(name)
            if previous is not None:
                if (facts["device"], facts["inode"]) != (previous["device"], previous["inode"]) or len(content) < previous["size"] or \
                        hashlib.sha256(content[:previous["size"]]).hexdigest() != previous["sha256"]:
                    retained_logs[-1]["commandProofExcluded"] = "Existing log was replaced or rewritten"
                    continue
                # The prior case's command can never be inherited from an
                # older prefix merely because a general server log grew.
                appended = content[previous["size"]:]
                if previous["size"] and content[previous["size"] - 1:previous["size"]] != b"\n":
                    appended = appended.partition(b"\n")[2]
                retained_logs[-1]["commandObservationOffset"] = previous["size"]
            else:
                appended = content
                retained_logs[-1]["commandObservationOffset"] = 0
            for line in appended.decode("utf-8", errors="replace").splitlines():
                if str(SOURCE_PATH) not in line or "libx264" not in line or len(line.encode()) > 65536:
                    continue
                try:
                    arguments = shlex.split(line)
                except ValueError:
                    continue
                start = next((index for index, value in enumerate(arguments) if Path(value).name == "ffmpeg" and APP in Path(value).parents), None)
                if start is None:
                    continue
                proof = software_command(arguments[start:])
                commands.append({"origin": "retained-owned-transcode-log", "logSha256": retained_logs[-1]["sha256"], **proof})
        base.save(base.PRIVATE / "producer" / (label + "-commands.json"), {"liveProcesses": observed, "retainedLogs": retained_logs, "proofs": commands})
        base.require(commands, "No actual software encoder command was captured; output alone does not prove software execution")
        return {"softwareCommandProved": True, "commands": commands, "retainedLogCount": len(retained_logs)}

    def download(self, label: str, path: str, session_id: str) -> dict:
        self.check_authority()
        base.require(not self.finishing and not self.writes_blocked and session_id in self.sessions and
                     self.sessions[session_id]["mediaPath"] == path and session_id not in self.media_attempts and
                     self.media_attempts <= self.stopped_sessions and not self.producer_snapshot(), "Media execution lacks its single-use owned route or previous producer exit")
        base.require(self.record_count < MAIN_REQUESTS and self.media_bytes + MAX_MEDIA <= MAX_MEDIA_TOTAL and
                     label not in self.labels and re.fullmatch(r"[a-z0-9-]+", label), "Media request/output budget exhausted")
        before_logs = self.log_snapshot()
        target = base.PRIVATE / "media" / (label + ".mp4")
        descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        headers = {"Accept": "video/mp4", "X-Emby-Token": self.admin()}
        request = {"method": "GET", "path": path, "headers": headers, "bodyPresent": False, "body": None,
                   "widthExperiment": self.current_width, "approvedMutationKind": "owned-progressive-producer"}
        try:
            base.save(base.PRIVATE / "mutations" / ("media-" + label + "-intent.json"), request)
        except Exception:
            os.close(descriptor)
            raise
        self.labels.add(label)
        self.record_count += 1
        self.media_attempts.add(session_id)
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=10)
        status, reason, version, response_headers, complete, failure, count = None, None, None, [], False, None, 0
        observed, observations, last_sample, observing = {}, [], [0.0], [False]
        until = min(self.deadline, time.monotonic() + MEDIA_SECONDS)
        old_handler = signal.getsignal(signal.SIGALRM)
        def observe(_signum=None, _frame=None):
            if observing[0]:
                return
            observing[0] = True
            try:
                now = time.monotonic()
                base.require(now < until, "Progressive producer exceeded its wall-clock budget")
                for row in self.producer_snapshot():
                    software_command(row["cmdline"])
                    observed[(row["pid"], row["startTicks"])] = row
                if now - last_sample[0] >= 1:
                    sample = operator_module().resources()
                    observations.append(sample)
                    base.require(len(observations) <= MEDIA_SECONDS + 5, "Producer observation budget exhausted")
                    last_sample[0] = now
            finally:
                observing[0] = False
        try:
            signal.signal(signal.SIGALRM, observe)
            signal.setitimer(signal.ITIMER_REAL, 0.05, 0.05)
            with os.fdopen(descriptor, "wb") as output:
                connection.request("GET", path, headers=headers)
                response = connection.getresponse()
                status, reason, version, response_headers = response.status, response.reason, response.version, response.getheaders()
                while True:
                    content = response.read1(min(65536, MAX_MEDIA - count + 1))
                    if not content:
                        complete = response.isclosed() or response.length == 0
                        break
                    count += len(content)
                    # At most one excess byte may be consumed to prove overflow;
                    # retain it separately without enlarging the media artifact.
                    if count > MAX_MEDIA:
                        retained = len(content) - (count - MAX_MEDIA)
                        output.write(content[:retained])
                        save_binary(base.PRIVATE / "media" / (label + "-overflow.bin"), content[retained:])
                        raise RuntimeError("Progressive output exceeds its byte bound")
                    output.write(content)
                output.flush()
                os.fsync(output.fileno())
                declared = [value for name, value in response_headers if name.lower() == "content-length"]
                if declared:
                    complete = complete and len(set(declared)) == 1 and declared[0].isdigit() and int(declared[0]) == count
        except Exception as error:
            failure = type(error).__name__
            self.writes_blocked = True
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            signal.signal(signal.SIGALRM, old_handler)
            connection.close()
        self.media_bytes += min(count, MAX_MEDIA)
        self.incomplete_count += int(not complete)
        wire_path = base.PRIVATE / "wire" / (label + ".json")
        size, digest = target.stat().st_size, base.digest(target)
        self.wire_records[label] = {"path": str(wire_path), "sha256": None, "completeHTTP": complete, "persisted": False}
        base.save(wire_path, {"request": request, "responseStatus": status, "responseReason": reason, "responseVersion": version,
            "responseHeaders": response_headers, "responseBodyFile": str(target), "responseBodySha256": digest, "responseBodyBytes": size,
            "completeHTTP": complete, "transportFailure": failure, "observedProcesses": list(observed.values()), "resourceObservations": observations})
        self.wire_records[label].update({"sha256": base.digest(wire_path), "persisted": True})
        self.write(label, {"request": request, "response": {"status": status, "reason": reason, "headers": response_headers,
            "bodyType": "private-mp4", "body": {"sha256": digest, "bytes": size}}, "observation": {"completeHTTP": complete,
            "transportFailure": failure, "actualBodyFileRetained": True, "resourceSampleCount": len(observations)}})
        # Prove producer exit before probing or restoring any settings, including
        # failed/partial downloads. The only stop authority is the DTO session.
        self.stop_session(session_id, "case-" + str(self.current_width) + "-producer-stop")
        base.require(complete and failure is None and status == 200 and 0 < size == count <= MAX_MEDIA and
                     any(name.lower() == "content-type" and value.lower().split(";", 1)[0].strip() == "video/mp4" for name, value in response_headers),
                     "Progressive output is not complete successful MP4 evidence")
        producer = self.producer_proof(label, list(observed.values()), before_logs)
        probe = operator_module().probe_local_media(target, label, expected_video_codec="h264", expected_frames=8)
        base.require(probe.get("complete") is True and probe.get("strictDecodePassed") is True and probe.get("decodedFrames") == 8 and
                     probe.get("videoCodec") == "h264" and probe.get("audioCodec") == "aac" and
                     type(probe.get("width")) is int and type(probe.get("height")) is int and min(probe["width"], probe["height"]) > 0 and
                     probe.get("inputIdentity", {}).get("sha256") == digest and probe["inputIdentity"].get("bytes") == size,
                     "The retained actual output lacks a complete independent media verification")
        result = {"configuredWidth": self.current_width, "sourceSha256": self.manifest["sourceIdentity"]["sha256"],
                  "outputSha256": digest, "outputBytes": size, "producer": producer, "probe": probe}
        self.write(label + "-probe", {"kind": "actual-complete-progressive-width-observation", **result})
        return result

    def stop_session(self, session_id: str, label: str) -> None:
        base.require(session_id in self.sessions, "Only a DTO-acknowledged owned playback session can be stopped")
        self.pending = None
        retry_needed = self.finishing and session_id not in self.stopped_sessions and bool(self.producer_snapshot())
        if session_id not in self.stop_attempts or retry_needed and session_id not in self.cleanup_stop_retries:
            self.approve("encoder-stop", "DELETE", self.stop_path(session_id))
            status, _body = self.request(label, "DELETE", self.stop_path(session_id), token=self.admin())
            base.require(status in {200, 204}, "Owned encoder stop was not acknowledged")
        until = min(self.deadline, time.monotonic() + 20)
        while True:
            self.check_authority()
            if not self.producer_snapshot():
                self.stopped_sessions.add(session_id)
                return
            base.require(time.monotonic() < until, "Owned encoder did not exit within its stop budget")
            time.sleep(0.2)

    def read_configuration(self, label: str, *, expected: dict | None = None) -> dict:
        status, public = self.request(label + "-public", "GET", PUBLIC_ROUTE)
        base.require(status == 200 and isinstance(public, dict) and public.get("Id") == self.manifest["serverId"] and public.get("Version") == "4.9.5.0",
                     "The fresh public server identity differs")
        status, total = self.request(label + "-total", "GET", TOTAL_ROUTE, token=self.admin())
        base.require(status == 200 and isinstance(total, dict), "Complete total configuration is unavailable")
        status, encoding = self.request(label + "-encoding", "GET", ENCODING_ROUTE, token=self.admin())
        base.require(status == 200 and isinstance(encoding, dict) and set(encoding) == ENCODING_FIELDS, "Complete encoding configuration is unavailable")
        if self.total_baseline is None:
            self.total_baseline, self.encoding_baseline = copy.deepcopy(total), copy.deepcopy(encoding)
            encoding_body(encoding, WIDTHS[0])
            self.restored = True
        else:
            base.require(configuration.same_value(total, self.total_baseline), "The study changed total configuration")
        if expected is not None:
            base.require(configuration.same_value(encoding, expected), "Encoding read-back differs from the complete expected object")
        return encoding

    def restore(self, label: str) -> None:
        self.pending = None
        base.require(self.encoding_baseline is not None and self.media_attempts <= self.stopped_sessions and not self.producer_snapshot(),
                     "Exact encoding restoration requires a baseline and all producers stopped")
        needs_post = self.encoding_dirty and self.current_width not in self.restore_attempts
        if self.encoding_dirty and self.current_width in self.restore_attempts:
            status, observed = self.request(label + "-prior-attempt-read", "GET", ENCODING_ROUTE, token=self.admin())
            base.require(status == 200 and isinstance(observed, dict), "The earlier restore outcome is unavailable")
            needs_post = not configuration.same_value(observed, self.encoding_baseline)
            base.require(not needs_post or self.finishing and self.current_width not in self.cleanup_restore_retries,
                         "The bounded restoration retry is unavailable")
        if needs_post:
            self.approve("encoding-restore", "POST", ENCODING_ROUTE, self.encoding_baseline)
            status, _ = self.request(label + "-post", "POST", ENCODING_ROUTE, token=self.admin(), body=self.encoding_baseline)
            base.require(status in {200, 204}, "Exact encoding restoration was not acknowledged")
        self.read_configuration(label, expected=self.encoding_baseline)
        self.restorations.append({"width": self.current_width, "capture": label, "allOriginalFieldsRestored": True})
        self.encoding_dirty, self.restored = False, True

    def libraries(self, label: str) -> list[dict]:
        status, result = self.request(label, "GET", LIBRARIES_ROUTE, token=self.admin())
        base.require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list) and
                     type(result.get("TotalRecordCount")) is int and result["TotalRecordCount"] == len(result["Items"]) <= 1,
                     "Fresh library membership is not a complete singleton or empty set")
        rows = result["Items"]
        for row in rows:
            base.require(self.library_attempted and row.get("Name") == LIBRARY_NAME and row.get("Locations") == [str(SOURCE)] and
                         isinstance(row.get("ItemId"), str) and re.fullmatch(r"[A-Za-z0-9_-]+", row["ItemId"]), "A library is outside the sole owned source")
            base.require(self.library_id in {None, row["ItemId"]}, "Owned library identity changed")
            self.library_id = row["ItemId"]
        return rows

    def prepare_library(self) -> None:
        base.require(self.libraries("libraries-before") == [], "Fresh libraries are not initially empty")
        operator_module().verify_source(self.manifest["sourceIdentity"])
        self.approve("library-create", "POST", "/emby/Library/VirtualFolders", library_body())
        status, _ = self.request("library-create", "POST", "/emby/Library/VirtualFolders", token=self.admin(), body=library_body())
        base.require(status in {200, 204} and len(self.libraries("library-owned-proof")) == 1, "Sole owned library creation is unproved")
        self.approve("library-refresh", "POST", self.refresh_path(), {})
        status, _ = self.request("library-refresh", "POST", self.refresh_path(), token=self.admin(), body={})
        base.require(status in {200, 204}, "Owned library refresh was not acknowledged")
        until = min(self.deadline, time.monotonic() + 90)
        for attempt in range(30):
            status, result = self.request("owned-item-" + str(attempt + 1), "GET", self.item_query(), token=self.admin())
            base.require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list) and
                         type(result.get("TotalRecordCount")) is int and result["TotalRecordCount"] == len(result["Items"]) <= 1,
                         "The owned source query is not a complete bounded singleton")
            if result["Items"]:
                item = result["Items"][0]
                sources = item.get("MediaSources", [])
                streams = item.get("MediaStreams", [])
                if len(sources) == 1 and streams:
                    base.require(item.get("Path") == str(SOURCE_PATH) and item.get("Type") == "Movie" and
                                 isinstance(item.get("Id"), str) and re.fullmatch(r"[A-Za-z0-9_-]+", item["Id"]), "Indexed item identity differs from the owned video")
                    source = sources[0]
                    base.require(source.get("Path") == str(SOURCE_PATH) and isinstance(source.get("Id"), str) and source["Id"], "Indexed media source differs")
                    videos, audios = [row for row in streams if row.get("Type") == "Video"], [row for row in streams if row.get("Type") == "Audio"]
                    base.require(len(videos) == len(audios) == 1 and videos[0].get("Codec") == "mpeg4" and
                                 videos[0].get("Width") == 3840 and videos[0].get("Height") == 2160 and audios[0].get("Codec") == "aac",
                                 "Indexed source does not match the independently probed MPEG-4 4K source")
                    self.item, self.source_id = item, source["Id"]
                    self.video_index, self.audio_index = videos[0]["Index"], audios[0]["Index"]
                    playback_body(self.control_user_id, self.source_id, self.video_index, self.audio_index)
                    return
            base.require(time.monotonic() < until, "Owned source indexing exceeded its bounded deadline")
            time.sleep(2)
        raise RuntimeError("Owned source did not become uniquely indexed within the request budget")

    def fresh_state(self, label: str, *, final: bool = False) -> None:
        values = {}
        for kind in ("Users", "Devices", "ScheduledTasks"):
            status, result = self.request(label + "-" + kind.lower(), "GET", "/emby/" + kind, token=self.admin())
            base.require(status == 200, "Fresh non-configuration state is unavailable")
            values[kind] = result
        users, devices, tasks = values["Users"], values["Devices"], values["ScheduledTasks"]
        base.require(isinstance(users, list) and len(users) == 1 and users[0].get("Id") == self.control_user_id and
                     isinstance(devices, dict) and isinstance(devices.get("Items"), list) and
                     len(devices["Items"]) == 1 and devices["Items"][0].get("ReportedDeviceId") == CONTROL_DEVICE and
                     devices["Items"][0].get("Id") == self.manifest["bootstrapDevices"][0].get("Id") and
                     isinstance(tasks, list) and 0 < len(tasks) <= 64, "Fresh account, ordinary device, or task membership differs")
        trim = lambda rows, fields: {row["Id"]: {key: value for key, value in row.items() if key not in fields} for row in rows}
        normalized = (trim(users, {"LastLoginDate", "LastActivityDate"}), trim(devices["Items"], {"DateLastActivity"}),
                      trim(tasks, configuration.read.RUNTIME_TASK_FIELDS))
        if final:
            base.require(configuration.same_value(normalized, (self.fresh_users, self.fresh_devices, self.fresh_tasks)),
                         "Fresh user policies, ordinary device metadata, or task definitions changed")
        else:
            self.fresh_users, self.fresh_devices, self.fresh_tasks = normalized

    def capture(self) -> None:
        status, _ = self.request("handoff-protected", "GET", "/emby/Sessions", token=self.admin())
        base.require(status == 200, "The single live handoff cannot establish protected access")
        self.fresh_state("before")
        self.read_configuration("baseline")
        self.prepare_library()
        for width in WIDTHS:
            self.current_width = width
            base.require(self.restored and self.media_attempts <= self.stopped_sessions, "Previous case has not stopped and restored")
            operator_module().verify_source(self.manifest["sourceIdentity"])
            body = encoding_body(self.encoding_baseline, width)
            self.approve("encoding-width", "POST", ENCODING_ROUTE, body)
            status, _ = self.request("width-" + str(width) + "-settings", "POST", ENCODING_ROUTE, token=self.admin(), body=body)
            base.require(status in {200, 204}, "Width settings were not acknowledged")
            self.read_configuration("width-" + str(width) + "-readback", expected=body)
            body = playback_body(self.control_user_id, self.source_id, self.video_index, self.audio_index)
            self.approve("playback-info", "POST", self.playback_path(), body)
            status, result = self.request("width-" + str(width) + "-playback", "POST", self.playback_path(), token=self.admin(), body=body)
            base.require(status == 200 and isinstance(result, dict), "Owned progressive negotiation failed")
            session = result["PlaySessionId"]
            path = self.media_path(result, session)
            self.case_results.append(self.download("width-" + str(width) + "-media", path, session))
            self.restore("width-" + str(width) + "-restore")
        base.require(len(self.case_results) == 2, "The bounded comparison is incomplete")

    def finish(self) -> None:
        self.finishing, self.writes_blocked, self.pending = True, True, None
        self.deadline = time.monotonic() + CLEANUP_SECONDS
        for index, session in enumerate(self.sessions):
            self.cleanup_step("encoder-stop-" + str(index), lambda session=session, index=index: self.stop_session(session, "cleanup-producer-stop-" + str(index)))
        if self.encoding_baseline is not None:
            self.cleanup_step("encoding-restoration", lambda: self.restore("cleanup-encoding"))
        if self.fresh_users is not None:
            self.cleanup_step("fresh-state-preservation", lambda: self.fresh_state("final", final=True))
        self.cleanup_step("owned-library-preservation", lambda: self.libraries("libraries-final"))
        # Storage errors are recorded by the inherited cleanup path and cannot
        # prevent either the owned logout or its independent Sessions 401 probe.
        self.deadline = max(self.deadline, time.monotonic() + 45)
        self.cleanup_step("ordinary-credential-retirement", lambda: self.logout("control"))
        self.checks["singleOrdinaryCredentialInvalid"] = self.invalid_login_proofs.get("control", {}).get("status") == 401
        self.checks["allAcknowledgedProducersStopped"] = set(self.sessions) == self.stopped_sessions and self.cleanup_step("final-encoders", self.producer_snapshot) == []
        self.checks["completeEncodingBaselineRestored"] = self.encoding_baseline is not None and self.restored and not self.encoding_dirty
        self.checks["freshProcessUnchanged"] = self.cleanup_step("fresh-identity", lambda: self.check_authority() or True) is True
        self.checks["protectedServicesUnchanged"] = self.cleanup_step("old-services", lambda: operator_module().check_old_services(self.manifest["oldServices"]) or True) is True
        self.checks["priorEvidenceAndSourcesUnchanged"] = self.cleanup_step("old-evidence", self.verify_saved_baseline) is True
        self.cleanup_step("mutations", lambda: base.save(base.PRIVATE / "mutation-results.json", self.mutations))
        self.checks["wireEvidenceUnchanged"] = len(self.wire_records) == self.record_count and all(row["persisted"] and
            base.digest(Path(row["path"])) == row["sha256"] for row in self.wire_records.values())
        redaction = True
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            try:
                redaction = redaction and configuration.same_value(self.sanitize(json.loads(path.read_text())),
                    json.loads((base.EXPORT / path.name).read_text()))
            except Exception:
                redaction = False
        self.checks["redactionAuditPassed"] = redaction
        self.cleanup_ok = not self.cleanup_errors and all(self.checks.values())
        self.write("audit", {"kind": "capture-audit-observation", "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok,
            "checks": self.checks, "cleanupErrors": self.cleanup_errors, "persistenceFailures": self.persistence_failures,
            "serverId": self.manifest["serverId"], "unit": UNIT, "referencePID": self.pid, "preservedOldRecords": OLD_RECORDS,
            "preservedRecordFilesIncludingSetup": len(self.baseline["records"]), "preservedSourceFiles": len(self.baseline["media"]),
            "ordinaryLoginsIncludingPreparation": 1, "captureLoginRequests": 0, "applicationKeyRequests": 0,
            "configurationCases": self.case_results, "restorationProofs": self.restorations,
            "ownedLoginInvalidityProofs": self.invalid_login_proofs, "ownedLoginLogoutStatuses": self.logout_statuses,
            "libraryCreationAttempted": self.library_attempted, "ownedLibraryRefreshAttempted": self.refresh_attempted,
            "widthsAttempted": sorted(self.width_attempts), "playbackInfoAttempts": len(self.playback_attempts),
            "acknowledgedPlaySessions": len(self.sessions), "mediaGetAttempts": len(self.media_attempts), "mediaRetries": 0,
            "cleanupStopRetries": len(self.cleanup_stop_retries), "cleanupRestoreRetries": len(self.cleanup_restore_retries),
            "httpAttempts": self.record_count, "incompleteHTTP": self.incomplete_count, "jsonWireBytes": self.total,
            "mediaWireBytes": self.media_bytes, "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "limits": {"mainRequests": MAIN_REQUESTS, "totalRequests": MAX_REQUESTS, "jsonBodyBytes": MAX_BODY,
                "totalJsonBytes": MAX_JSON_TOTAL, "mediaBodyBytes": MAX_MEDIA, "totalMediaBytes": MAX_MEDIA_TOTAL,
                "mainSeconds": MAIN_SECONDS, "cleanupSeconds": CLEANUP_SECONDS, "mediaSeconds": MEDIA_SECONDS,
                "sourceFrames": 8, "concurrentProducers": 1, "ordinaryCredentials": 1,
                "credentialRequestReserve": CREDENTIAL_REQUEST_RESERVE, "credentialByteReserve": CREDENTIAL_BYTE_RESERVE},
            "operatorSha256": OPERATOR_SOURCE_SHA256, "sanitizerSources": SANITIZER_SOURCES,
            "interpretationScope": "Observed complete outputs for this source, profile, software encoder and server version; zero is not preclassified as unlimited.",
            "operatorTeardownRequired": True, "retainedFreshHistory": "The single library, source, program data and revoked ordinary device are wholly owned; operator cleanup retains evidence."})
        base.require(self.cleanup_ok, "Width capture cleanup or preservation proof is incomplete")

    def initialization_cleanup(self) -> None:
        # No capture mutation has been dispatched yet. Do not create or write
        # evidence beneath a root whose initialization did not complete.
        # The root runner must retain this stdout and invoke operator cleanup.
        self.evidence_unavailable = True
        self.finishing, self.writes_blocked, self.pending = True, True, None
        self.deadline = time.monotonic() + 45
        self.cleanup_step("initialization-credential-retirement", lambda: self.logout("control"))
        print(json.dumps({"capture": base.PREFIX, "result": "partial", "initializationComplete": False,
            "ordinaryCredentialInvalidity": self.invalid_login_proofs.get("control", {}).get("status"),
            "privateEvidencePersistenceComplete": False, "operatorCleanupRequired": True}), flush=True)


def main() -> None:
    signal.signal(signal.SIGALRM, device.deadline_expired)
    def terminate(_signum: int, _frame: object) -> None:
        raise InterruptedError("Width capture received a termination signal")
    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, terminate)
    recorder, initialized, failure = None, False, None
    try:
        manifest = preconditions()
        recorder = Recorder.__new__(Recorder)
        recorder.__init__(manifest)
        initialized = True
        recorder.capture()
    except Exception as error:
        failure = type(error).__name__
        if recorder is not None and initialized:
            recorder.capture_failure = failure
            recorder.writes_blocked = True
            recorder.persist("capture-failure", lambda: base.save(base.PRIVATE / "failure.txt", failure + ": " + str(error) + "\n"), cleanup=True)
    finally:
        if not initialized:
            if recorder is not None and getattr(recorder, "control_token", None):
                recorder.initialization_cleanup()
            print(json.dumps({"capture": base.PREFIX, "result": "partial", "failureType": failure,
                              "initializationComplete": False, "operatorCleanupRequired": True}), flush=True)
            raise SystemExit(1)
        try:
            recorder.finish()
        except Exception as error:
            recorder.persist("cleanup-failure", lambda: base.save(base.PRIVATE / "cleanup-failure.txt", type(error).__name__ + ": " + str(error) + "\n"), cleanup=True)
            print(json.dumps({"capture": base.PREFIX, "result": "partial", "cleanup": "failed", "failureType": type(error).__name__}), flush=True)
            raise SystemExit(1)
    print(json.dumps({"capture": base.PREFIX, "result": "complete" if failure is None else "partial", "failureType": failure,
                      "cleanup": recorder.cleanup_ok}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
