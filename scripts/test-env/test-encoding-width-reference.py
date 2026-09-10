#!/usr/bin/env python3
"""Test owned encoding-width study boundaries with synthetic memory only.

This suite must run through authorized root SSH after both study scripts are
frozen. It never contacts an actual server, starts a media tool, modifies a
service, or reads fixture data. Passing synthetic guards is not live playback,
output-resolution, configuration-restoration, or teardown acceptance evidence.
"""

from __future__ import annotations

import _io
import argparse
import base64
import contextlib
import copy
import datetime
import hashlib
import http.client
import importlib.util
import io
import json
import linecache
import os
from pathlib import Path
import re
import secrets
import selectors
import shlex
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time
import types
import unittest
from unittest.mock import Mock, patch
from urllib.parse import parse_qs, quote, unquote, unquote_plus, urlencode, urlsplit, urlunsplit

if sys.platform == "linux":
    import fcntl
    import resource
else:
    fcntl = None
    resource = None

sys.dont_write_bytecode = True
RECORDER: types.ModuleType
OPERATOR: types.ModuleType
SOURCE_LINES: dict[str, list[str]] = {}
SOURCE_HASHES: dict[str, str] = {}


@contextlib.contextmanager
def memory_tracebacks():
    with patch.object(linecache, "checkcache", lambda filename=None: None), \
         patch.object(linecache, "lazycache", lambda filename, module_globals: False), \
         patch.object(linecache, "getlines", lambda filename, module_globals=None:
                      list(SOURCE_LINES.get(str(filename), ()))):
        yield


class EffectFence(contextlib.ExitStack):
    """Deny unfaked effects before imports, constructors, or cleanup hooks."""

    def __init__(self) -> None:
        super().__init__()
        self.violations: list[str] = []

    def __enter__(self) -> EffectFence:
        super().__enter__()
        import builtins

        self.enter_context(memory_tracebacks())
        fence = self

        class RejectUncachedImports:
            def find_spec(self, fullname: str, path: object = None, target: object = None) -> object:
                fence.violations.append("import:" + fullname)
                raise AssertionError("Uncached import is outside the memory fixture: " + fullname)

        self.enter_context(patch.object(sys, "meta_path", [RejectUncachedImports()]))
        targets = (
            (builtins, ("open",)),
            (io, ("open", "open_code", "FileIO")), (_io, ("open", "open_code", "FileIO")),
            (subprocess, ("run", "Popen", "call", "check_call", "check_output")),
            (selectors, ("DefaultSelector",)), (fcntl, ("flock", "lockf", "ioctl", "fcntl")),
            (socket, ("socket", "create_connection", "getaddrinfo")),
            (http.client, ("HTTPConnection", "HTTPSConnection")),
            (shutil, ("copy", "copy2", "copyfile", "copytree", "move", "rmtree", "disk_usage")),
            (tempfile, ("TemporaryDirectory", "NamedTemporaryFile", "TemporaryFile", "mkstemp", "mkdtemp")),
            (signal, ("signal", "getsignal", "setitimer", "pthread_sigmask")), (time, ("sleep",)),
            (resource, ("getrlimit", "setrlimit", "prlimit", "getrusage")),
            (os, ("open", "close", "fdopen", "stat", "lstat", "fstat", "readlink", "scandir", "listdir", "walk",
                  "read", "write", "fsync", "umask", "kill", "killpg", "getpgid", "setsid", "system", "popen", "fork",
                  "posix_spawn", "posix_spawnp", "execve", "execvp", "replace", "rename", "mkdir", "makedirs", "remove",
                  "unlink", "rmdir", "chmod", "chown", "link", "symlink", "truncate", "waitpid", "waitid", "set_blocking")),
            (os.path, ("ismount",)),
            (Path, ("open", "exists", "is_symlink", "is_dir", "is_file", "resolve", "stat", "lstat", "read_bytes",
                    "read_text", "write_bytes", "write_text", "mkdir", "rmdir", "unlink", "chmod", "touch",
                    "rename", "replace", "glob", "rglob", "iterdir")),
        )
        for owner, names in targets:
            for name in names:
                if not hasattr(owner, name):
                    continue
                label = owner.__name__ + "." + name

                def denied(*_args: object, _label: str = label, **_kwargs: object) -> object:
                    self.violations.append(_label)
                    raise AssertionError("Unfaked external effect: " + _label)

                self.enter_context(patch.object(owner, name, denied))
        return self

    def __exit__(self, *arguments: object) -> None:
        super().__exit__(*arguments)
        if self.violations:
            raise AssertionError("External effects were attempted: " + ", ".join(self.violations))


class EncodingWidthGuards(unittest.TestCase):
    def setUp(self) -> None:
        fence = EffectFence()
        fence.__enter__()
        self.addCleanup(fence.__exit__, None, None, None)
        output = contextlib.redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)

    def replace(self, owner: object, name: str, value: object) -> object:
        replacement = patch.object(owner, name, value)
        replacement.start()
        self.addCleanup(replacement.stop)
        return value

    @staticmethod
    def baseline() -> dict:
        return {"EncodingThreadCount": 0, "ExtractionThreadCount": 2, "DownMixAudioBoost": 2.0,
                "EnableThrottling": True, "ThrottleBufferSize": 120, "ThrottleHysteresis": 30, "ThrottlingMethod": 0,
                "H264Crf": 23, "EnableHardwareEncoding": True, "EnableSubtitleExtraction": True,
                "EnableOnTheFlyAttachmentExtraction": False, "CodecConfigurations": [{"Name": "synthetic", "Options": [1, 2]}],
                "HardwareAccelerationMode": 7, "EnableHardwareToneMapping": False, "EnableSoftwareToneMapping": True,
                "TranscodingMaxWidth": 0, "EnableHevcEncoding": False}

    def recorder(self) -> object:
        value = RECORDER.Recorder.__new__(RECORDER.Recorder)
        value.authority = {"pid": 12345, "networkNamespace": "net:[fresh]",
                           "serviceProperties": {"ControlGroup": "/system.slice/" + RECORDER.UNIT}}
        value.manifest = {"serverId": "synthetic-server", "oldServices": {"old": {"networkNamespace": "net:[old]"}},
                          "oldBaseline": {}, "sourceIdentity": {"path": str(RECORDER.SOURCE_PATH), "bytes": 1234,
                          "sha256": "a" * 64, "device": 41, "inode": 9}}
        value.pid, value.started, value.deadline, value.finishing = 12345, 100.0, 1000.0, False
        value.control_token, value.control_user_id = "synthetic-control-token", "synthetic-user"
        value.logins = {"control": {"AccessToken": value.control_token}}
        value.secrets, value.forbidden_tokens = {value.control_token}, set()
        value._secret_signature, value._secret_regex = None, None
        value.invalid_login_proofs, value.logout_statuses = {}, {}
        value.invalid_tokens, value.logged_out = set(), set()
        value.record_count = value.total = value.charged_bytes = value.incomplete_count = value.media_bytes = 0
        value.labels, value.mutations, value.wire_records = set(), [], {}
        value.persistence_failures, value.cleanup_errors, value.checks = [], [], {}
        value.pending, value.current_width = None, None
        value.library_attempted, value.refresh_attempted, value.library_id = False, False, None
        value.item, value.source_id, value.video_index, value.audio_index = {"Id": "synthetic-item"}, "synthetic-source", 0, 1
        value.encoding_baseline, value.total_baseline = self.baseline(), {}
        value.encoding_dirty, value.restored, value.writes_blocked = False, True, False
        value.width_attempts, value.playback_attempts, value.restore_attempts = set(), set(), set()
        value.sessions, value.media_attempts, value.stop_attempts, value.stopped_sessions = {}, set(), set(), set()
        value.cleanup_stop_retries, value.cleanup_restore_retries, value.stop_acks = set(), set(), set()
        value.evidence_unavailable, value.capture_failure, value.cleanup_ok = False, None, False
        value.case_results, value.restorations = [], []
        value.fresh_users, value.fresh_devices, value.fresh_tasks = None, None, None
        self.replace(RECORDER, "operator_module", lambda: OPERATOR)
        return value

    @staticmethod
    def producer_arguments() -> list[str]:
        return [str(RECORDER.APP / "bin/ffmpeg"), "-hwaccel", "none", "-i", str(RECORDER.SOURCE_PATH),
                "-c:v", "libx264", "-c:a", "aac", "-f", "mp4", str(RECORDER.DATA / "transcoding-temp/owned.mp4")]

    def playback_result(self, recorder: object, session: str = "owned-session") -> dict:
        query = {"DeviceId": RECORDER.CONTROL_DEVICE, "MediaSourceId": recorder.source_id, "PlaySessionId": session,
                 "api_key": recorder.admin(), "VideoCodec": "h264", "AudioCodec": "aac",
                 "AllowVideoStreamCopy": "false", "AllowAudioStreamCopy": "false", "VideoBitrate": "100000000"}
        path = "/emby/videos/" + recorder.item["Id"] + "/stream.mp4?" + urlencode(query)
        return {"PlaySessionId": session, "MediaSources": [{"Id": recorder.source_id, "TranscodingUrl": path}]}

    def test_fixed_linux_resources_and_toolchain_preserve_the_accepted_main(self) -> None:
        self.assertEqual((OPERATOR.PORT, OPERATOR.HTTPS_PORT), (18100, 18500))
        self.assertEqual((OPERATOR.ROOT, OPERATOR.DATA, OPERATOR.SOURCE_FILE), (RECORDER.EVIDENCE, RECORDER.DATA, RECORDER.SOURCE_PATH))
        self.assertEqual(OPERATOR.ROOT, OPERATOR.WORK / "emby-width-fresh-m5h-20260910-02")
        self.assertEqual(OPERATOR.DATA, OPERATOR.WORK / "emby-width-fresh-data-02")
        self.assertEqual(OPERATOR.FIXTURE_ATTEMPT, 2)
        self.assertEqual(OPERATOR.ADMIN_NAME, "reference-encoding-width-fresh-m5h-02")
        self.assertEqual(OPERATOR.CONTROL_CLIENT, "Goby Encoding Width Fresh M5h 02")
        self.assertEqual(OPERATOR.CONTROL_DEVICE, "goby-encoding-width-fresh-m5h-20260910-02-control")
        self.assertEqual((OPERATOR.UNIT, OPERATOR.MARKER, OPERATOR.CONTROL_CLIENT, OPERATOR.CONTROL_DEVICE),
                         (RECORDER.UNIT, RECORDER.MARKER, RECORDER.CONTROL_CLIENT, RECORDER.CONTROL_DEVICE))
        self.assertEqual(OPERATOR.PREFIX, "encoding-width-fresh-setup-m5h-2-")
        self.assertEqual(RECORDER.base.PREFIX, "encoding-width-fresh-m5h-2-")
        self.assertNotEqual(OPERATOR.ROOT, OPERATOR.FAILED_ROOT)
        self.assertNotEqual(OPERATOR.DATA, OPERATOR.FAILED_DATA)
        self.assertEqual(OPERATOR.OLD_UNITS["goby-foundation-test.service"], 3614026)
        self.assertEqual(OPERATOR.MAIN_START_TICKS, "25289276")
        self.assertEqual(OPERATOR.MAIN_BINARY_SHA256, "e1f6b723eb963ad855f465d0798958480615b8b000455fcf26bc7b088b8d2f2f")
        self.assertEqual(RECORDER.OLD_RECORDS, 2305)
        self.assertIn(str(OPERATOR.RETIRED_DATA), OPERATOR.ALL_HISTORICAL_REMOVED)
        self.assertEqual(str(OPERATOR.FFMPEG), "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
        self.assertEqual(RECORDER.OPERATOR_SOURCE_SHA256, SOURCE_HASHES["prepare-encoding-width-fresh.py"])
        for name, digest in OPERATOR.SANITIZER_SOURCES.items():
            self.assertEqual(digest, SOURCE_HASHES[name])

    def test_encoding_cases_are_complete_independent_clones_with_only_three_controls(self) -> None:
        baseline = self.baseline()
        original = copy.deepcopy(baseline)
        first, second = RECORDER.encoding_body(baseline, 1280), RECORDER.encoding_body(baseline, 0)
        self.assertEqual(set(first), set(baseline))
        self.assertEqual({key for key in first if first[key] != second[key]}, {"TranscodingMaxWidth"})
        for value in (first, second):
            self.assertIs(value["EnableHardwareEncoding"], False)
            self.assertEqual(value["EncodingThreadCount"], 1)
            self.assertEqual(value["HardwareAccelerationMode"], 7)
            self.assertEqual({key: item for key, item in value.items() if key not in {"TranscodingMaxWidth", "EnableHardwareEncoding", "EncodingThreadCount"}},
                             {key: item for key, item in baseline.items() if key not in {"TranscodingMaxWidth", "EnableHardwareEncoding", "EncodingThreadCount"}})
        first["CodecConfigurations"][0]["Options"].append(3)
        self.assertEqual(baseline, original)
        self.assertEqual(second["CodecConfigurations"], original["CodecConfigurations"])
        for width in (False, True, "0", 0.0, -1, 1920):
            with self.subTest(width=width), self.assertRaises(RuntimeError):
                RECORDER.encoding_body(baseline, width)
        with self.assertRaises(RuntimeError):
            RECORDER.encoding_body({**baseline, "UnexpectedField": 1}, 0)

    def test_profile_has_no_client_dimension_limit_or_direct_or_copy_fallback(self) -> None:
        body = RECORDER.playback_body("user", "source", 0, 1)
        for flag in ("AllowVideoStreamCopy", "AllowAudioStreamCopy", "EnableDirectPlay", "EnableDirectStream"):
            self.assertIs(body[flag], False)
        self.assertEqual(body["StartTimeTicks"], 0)
        self.assertEqual(body["MaxStreamingBitrate"], 100000000)
        self.assertEqual(body["DeviceProfile"]["DirectPlayProfiles"], [])
        profile = body["DeviceProfile"]["TranscodingProfiles"][0]
        self.assertEqual((profile["Type"], profile["Context"], profile["Protocol"], profile["Container"], profile["VideoCodec"], profile["AudioCodec"]),
                         ("Video", "Streaming", "http", "mp4", "h264", "aac"))

        def names(value: object) -> list[str]:
            if isinstance(value, dict):
                return [*value, *[name for item in value.values() for name in names(item)]]
            if isinstance(value, list):
                return [name for item in value for name in names(item)]
            return []

        self.assertFalse({"width", "height", "maxwidth", "maxheight"} & {name.lower() for name in names(body)})
        for indexes in ((0, 0), (-1, 1), (True, 1)):
            with self.subTest(indexes=indexes), self.assertRaises(RuntimeError):
                RECORDER.playback_body("user", "source", *indexes)

    def test_library_is_single_owned_read_only_source_and_mutation_is_single_use(self) -> None:
        body = RECORDER.library_body()
        self.assertEqual(body["Paths"], [str(RECORDER.SOURCE)])
        options = body["LibraryOptions"]
        self.assertEqual(options["PathInfos"], [{"Path": str(RECORDER.SOURCE)}])
        self.assertIs(options["SaveLocalMetadata"], False)
        self.assertIs(options["EnableRealtimeMonitor"], False)
        self.assertEqual(options["MetadataSavers"], [])
        recorder = self.recorder()
        recorder.approve("library-create", "POST", "/emby/Library/VirtualFolders", body)
        recorder.consume(recorder.pending)
        with self.assertRaises(RuntimeError):
            recorder.approve("library-create", "POST", "/emby/Library/VirtualFolders", body)
        for route in ("/emby/Users/AuthenticateByName", "/emby/Auth/Keys", "/emby/Sessions/Playing", "/emby/web/index.html"):
            with self.subTest(route=route), self.assertRaises(RuntimeError):
                recorder.authorize("POST", route, recorder.admin(), {})

    def test_only_complete_dto_acknowledges_a_unique_playback_session(self) -> None:
        recorder = self.recorder()
        recorder.current_width = 1280
        result = self.playback_result(recorder)
        recorder.ack_playback(200, result, False, "partial")
        recorder.ack_playback(302, result, True, "redirect")
        self.assertEqual(recorder.sessions, {})
        with self.assertRaises(RuntimeError):
            recorder.media_path(result, "owned-session")
        recorder.ack_playback(200, result, True, "complete")
        self.assertEqual(set(recorder.sessions), {"owned-session"})
        with self.assertRaises(RuntimeError):
            recorder.ack_playback(200, result, True, "duplicate")
        with self.assertRaises(RuntimeError):
            recorder.stop_path("foreign-session")

    def test_media_route_preserves_returned_parameters_and_refuses_foreign_or_direct_urls(self) -> None:
        recorder = self.recorder()
        result = self.playback_result(recorder)
        recorder.ack_playback(200, result, True, "complete")
        original = result["MediaSources"][0]["TranscodingUrl"]
        self.assertEqual(recorder.media_path(result, "owned-session"), original)
        variants = ("https://evil.invalid" + original, "//evil.invalid" + original,
                    original.replace("stream.mp4", "original.mp4"), original + "&PlaySessionId=foreign",
                    original + "&playsessionid=foreign", original.replace("AllowAudioStreamCopy=false", "AllowAudioStreamCopy=true"),
                    original.replace("synthetic-source", "foreign-source"), original.replace("owned-session", "foreign-session"))
        for url in variants:
            with self.subTest(url=url):
                bad = copy.deepcopy(result)
                bad["MediaSources"][0]["TranscodingUrl"] = url
                with self.assertRaises(RuntimeError):
                    recorder.media_path(bad, "owned-session")

    def test_software_proof_rejects_hardware_decoding_copy_or_foreign_input(self) -> None:
        arguments = self.producer_arguments()
        self.assertTrue(RECORDER.software_command(arguments)["softwareVideoCommandProved"])
        variants = [arguments[:1] + ["-hwaccel", "auto"] + arguments[3:],
                    arguments[:1] + ["-hwaccel_device", "/dev/dri/renderD128"] + arguments[3:],
                    [value.replace("libx264", "h264_nvenc") for value in arguments],
                    [value.replace("aac", "copy") for value in arguments],
                    ["/usr/bin/ffmpeg", *arguments[1:]],
                    [str(RECORDER.APP / "../outside/ffmpeg"), *arguments[1:]],
                    [*arguments[:-1], str(RECORDER.DATA / "../outside.mp4")],
                    ["/unrelated/source.mp4" if value == str(RECORDER.SOURCE_PATH) else value for value in arguments]]
        for value in variants:
            with self.subTest(arguments=value), self.assertRaises(RuntimeError):
                RECORDER.software_command(value)

    def test_media_process_rlimits_are_finite_and_single_tool_inputs_cannot_be_urls(self) -> None:
        applied = {}
        self.replace(resource, "setrlimit", lambda kind, bounds: applied.update({kind: bounds}))
        OPERATOR.media_process_limits()
        self.assertEqual(applied[resource.RLIMIT_AS], (1024 * 1024 * 1024,) * 2)
        self.assertEqual(applied[resource.RLIMIT_CPU], (90, 90))
        self.assertEqual(applied[resource.RLIMIT_FSIZE], (32 * 1024 * 1024,) * 2)
        self.assertEqual(applied[resource.RLIMIT_CORE], (0, 0))
        self.assertEqual(applied[resource.RLIMIT_NOFILE], (64, 64))
        self.replace(OPERATOR, "same", lambda identity: None)
        for arguments in ([str(OPERATOR.FFPROBE), "http://127.0.0.1:18100/stream.mp4"],
                          [str(OPERATOR.FFMPEG), "-i", "https://example.invalid/media.mp4"],
                          [str(OPERATOR.FFPROBE), "file:/etc/passwd"], ["/usr/bin/ffprobe", "-version"]):
            with self.subTest(arguments=arguments), self.assertRaises(RuntimeError):
                OPERATOR.bounded_media_command(arguments, {"pid": 12345}, seconds=10)

    def test_local_media_path_rejects_foreign_url_empty_and_oversized_inputs(self) -> None:
        size = [100]
        self.replace(OPERATOR, "owned_root", lambda path: None)

        def canonical(path: Path, **kwargs: object) -> object:
            if ".." in path.parts:
                raise RuntimeError("Synthetic noncanonical media path")
            return types.SimpleNamespace(st_size=size[0], st_dev=41, st_ino=7)

        self.replace(OPERATOR, "canonical", canonical)
        self.replace(OPERATOR, "digest", lambda path: "a" * 64)
        owned = OPERATOR.ROOT / "private/output.mp4"
        self.assertEqual(OPERATOR.local_media_path(owned)["bytes"], 100)
        for path in ("https://example.invalid/output.mp4", Path("https://example.invalid/output.mp4"),
                     Path("/opt/goby-fixtures/old.mp4"), OPERATOR.ROOT / "../foreign.mp4", owned.with_suffix(".m3u8")):
            with self.subTest(path=str(path)), self.assertRaises(RuntimeError):
                OPERATOR.local_media_path(path)
        for amount in (0, 64 * 1024 * 1024 + 1):
            size[0] = amount
            with self.subTest(size=amount), self.assertRaises(RuntimeError):
                OPERATOR.local_media_path(owned)

    def test_media_lease_prevents_overlapping_generation_and_probe_work(self) -> None:
        self.replace(OPERATOR, "canonical", lambda *args, **kwargs: None)
        state = {"next": 100, "holder": None, "closed": []}

        def open_lock(path: Path, flags: int, mode: int) -> int:
            self.assertEqual(path, OPERATOR.PRIVATE / "media-operation.lock")
            self.assertEqual(flags, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW)
            self.assertEqual(mode, 0o600)
            state["next"] += 1
            return state["next"]

        def lock(descriptor: int, operation: int) -> None:
            self.assertEqual(operation, fcntl.LOCK_EX | fcntl.LOCK_NB)
            if state["holder"] is not None:
                raise BlockingIOError("Synthetic media operation already active")
            state["holder"] = descriptor

        def close(descriptor: int) -> None:
            state["closed"].append(descriptor)
            if state["holder"] == descriptor:
                state["holder"] = None

        self.replace(os, "open", open_lock)
        self.replace(os, "close", close)
        self.replace(fcntl, "flock", lock)
        with OPERATOR.media_lease():
            with self.assertRaises(BlockingIOError):
                with OPERATOR.media_lease():
                    self.fail("Concurrent media lease unexpectedly succeeded")
            self.assertEqual(state["holder"], 101)
        self.assertIsNone(state["holder"])
        self.assertEqual(state["closed"], [102, 101])

    def install_probe(self) -> types.SimpleNamespace:
        target = OPERATOR.ROOT / "private/output.mp4"
        identity = {"path": str(target), "bytes": 100, "sha256": "a" * 64, "device": 41, "inode": 7}
        fixture = types.SimpleNamespace(path=target, identity=identity, changed=False, reads=0, calls=[], saved={},
            parsed={"streams": [{"codec_type": "video", "codec_name": "h264", "width": 640, "height": 360, "nb_read_frames": "8"},
                                 {"codec_type": "audio", "codec_name": "aac"}]}, decode_complete=True, decode_frames=8)

        def local(path: Path) -> dict:
            self.assertEqual(path, target)
            fixture.reads += 1
            return {**identity, "sha256": "b" * 64} if fixture.changed and fixture.reads > 1 else copy.deepcopy(identity)

        def command(arguments: list[str], actual_identity: dict, *, seconds: int) -> dict:
            fixture.calls.append(arguments)
            self.assertIn("-protocol_whitelist", arguments)
            self.assertEqual(arguments[arguments.index("-protocol_whitelist") + 1], "file,pipe")
            self.assertNotIn("-read_intervals", arguments)
            if arguments[0] == str(OPERATOR.FFPROBE):
                self.assertEqual(arguments[-1], str(target))
                self.assertIn("-count_frames", arguments)
                self.assertEqual(seconds, 60)
                return {"complete": True, "returnCode": 0, "stdout": json.dumps(fixture.parsed)}
            self.assertEqual(arguments[0], str(OPERATOR.FFMPEG))
            self.assertEqual(arguments[arguments.index("-i") + 1], str(target))
            self.assertIn("-xerror", arguments)
            self.assertEqual(seconds, 90)
            return {"complete": fixture.decode_complete, "returnCode": 0 if fixture.decode_complete else 1,
                    "stdout": "frame=" + str(fixture.decode_frames) + "\nprogress=end\n"}

        self.replace(OPERATOR, "local_media_path", local)
        self.replace(OPERATOR, "bounded_media_command", command)
        self.replace(OPERATOR, "save", lambda path, value: fixture.saved.update({path: copy.deepcopy(value)}))
        self.replace(OPERATOR, "configuration_sanitizer", lambda: types.SimpleNamespace(sanitize=lambda value: copy.deepcopy(value)))
        return fixture

    def test_complete_probe_accepts_observed_positive_zero_case_dimensions_without_presets(self) -> None:
        fixture = self.install_probe()
        for width, height in ((640, 360), (1920, 1080), (3840, 2160)):
            fixture.parsed["streams"][0].update({"width": width, "height": height})
            result = OPERATOR._probe_local_media(fixture.path, "zero-" + str(width), {}, {}, expected_video_codec="h264", expected_frames=8)
            self.assertEqual((result["width"], result["height"]), (width, height))
            self.assertTrue(result["complete"] and result["strictDecodePassed"])
            self.assertEqual(result["decodedFrames"], 8)
        self.assertEqual(len(fixture.calls), 6)

    def test_probe_rejects_invalid_dimensions_partial_frames_decode_failure_and_file_changes(self) -> None:
        fixture = self.install_probe()
        video = fixture.parsed["streams"][0]
        cases = (("width", 0), ("height", True), ("nb_read_frames", "7"), ("codec_name", "mpeg4"))
        for index, (field, invalid) in enumerate(cases):
            original = video[field]
            video[field] = invalid
            with self.subTest(field=field), self.assertRaisesRegex(RuntimeError, "Local media verification failed"):
                OPERATOR._probe_local_media(fixture.path, "bad-" + str(index), {}, {}, expected_video_codec="h264", expected_frames=8)
            video[field] = original
        fixture.decode_complete = False
        with self.assertRaisesRegex(RuntimeError, "Local media verification failed"):
            OPERATOR._probe_local_media(fixture.path, "decode-failed", {}, {}, expected_video_codec="h264", expected_frames=8)
        fixture.decode_complete, fixture.decode_frames = True, 7
        with self.assertRaisesRegex(RuntimeError, "Local media verification failed"):
            OPERATOR._probe_local_media(fixture.path, "decode-short", {}, {}, expected_video_codec="h264", expected_frames=8)
        fixture.decode_frames, fixture.changed, fixture.reads = 8, True, 0
        with self.assertRaisesRegex(RuntimeError, "Local media verification failed"):
            OPERATOR._probe_local_media(fixture.path, "file-changed", {}, {}, expected_video_codec="h264", expected_frames=8)

    def test_ready_requires_one_live_login_empty_library_and_the_full_preserved_corpus(self) -> None:
        manifest = {"state": "READY", "fixtureAttempt": 2, "marker": RECORDER.MARKER, "unit": RECORDER.UNIT, "port": 18100, "httpsPort": 18500,
            "programData": str(RECORDER.DATA), "evidenceRoot": str(RECORDER.EVIDENCE), "operatorSha256": RECORDER.OPERATOR_SOURCE_SHA256,
            "sanitizerSources": RECORDER.SANITIZER_SOURCES, "allFreshProgramDataOwned": True, "oldPreservationVerified": True,
            "controlCredentialState": "LIVE_HANDOFF", "ordinaryLoginCount": 1, "controlDeviceId": RECORDER.CONTROL_DEVICE,
            "controlClient": RECORDER.CONTROL_CLIENT, "sourceRoot": str(RECORDER.SOURCE), "sourcePath": str(RECORDER.SOURCE_PATH),
            "controlCredentialsFile": str(RECORDER.EVIDENCE / "private/control-credentials.env"),
            "controlLoginResponseFile": str(RECORDER.EVIDENCE / "private/control-login-response.json"),
            "serverId": "synthetic-fresh-server", "bootstrapLibraries": [], "bootstrapLibraryMutationRequests": 0,
            "bootstrapTaskMutationRequests": 0, "bootstrapApplicationKeyRequests": 0,
            "oldBaseline": {"records": {str(index): "a" * 64 for index in range(4610)},
                            "media": {str(index): "b" * 64 for index in range(240)}},
            "serviceProperties": {"PrivateDevices": "yes", "MemoryMax": str(1152 * 1024 * 1024)}, "sourceIdentity": {"synthetic": "source"}}
        failed = {"evidenceRoot": str(OPERATOR.FAILED_ROOT), "programData": str(OPERATOR.FAILED_DATA),
                  "dataAbsent": True, "sourceAbsent": True, "noOrdinaryLogin": True}
        manifest["failedFixtureProvenance"] = failed
        manifest["oldBaseline"]["failedFixtureProvenance"] = copy.deepcopy(failed)
        self.replace(RECORDER, "operator_module", lambda: OPERATOR)
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(manifest))
        self.replace(OPERATOR, "verify_source", lambda expected: expected)
        self.assertEqual(RECORDER.load_manifest()["ordinaryLoginCount"], 1)
        for field, bad in (("fixtureAttempt", 1), ("ordinaryLoginCount", 2), ("controlCredentialState", "REVOKED"),
                           ("bootstrapLibraries", [{"Id": "foreign"}]), ("bootstrapApplicationKeyRequests", 1),
                           ("serverId", OPERATOR.OLD_SERVER_ID), ("failedFixtureProvenance", {**failed, "dataAbsent": False})):
            with self.subTest(field=field):
                previous = manifest[field]
                manifest[field] = bad
                with self.assertRaises(RuntimeError):
                    RECORDER.load_manifest()
                manifest[field] = previous
        manifest["oldBaseline"]["records"].pop("0")
        with self.assertRaisesRegex(RuntimeError, "corpus membership differs"):
            RECORDER.load_manifest()
        manifest["oldBaseline"]["records"]["0"] = "a" * 64
        manifest["oldBaseline"]["media"].pop("0")
        with self.assertRaisesRegex(RuntimeError, "corpus membership differs"):
            RECORDER.load_manifest()

    def test_launcher_changes_to_vendor_app_while_unit_starts_in_owned_runtime(self) -> None:
        saved, commands = {}, []
        self.replace(OPERATOR, "save", lambda path, value, **kwargs: saved.update({path: value}))
        self.replace(OPERATOR, "run", lambda arguments, **kwargs: commands.append(arguments))
        OPERATOR.write_configuration()
        OPERATOR.start_service()
        launcher = saved[OPERATOR.RUNTIME / "launch.sh"]
        self.assertIn("APP_DIR=" + str(OPERATOR.APP), launcher)
        self.assertIn("EMBY_DATA=" + str(OPERATOR.DATA), launcher)
        self.assertLess(launcher.index('cd "$APP_DIR"'), launcher.index('exec "$APP_DIR/system/EmbyServer"'))
        self.assertIn("--property=WorkingDirectory=" + str(OPERATOR.RUNTIME), commands[0])
        self.assertNotIn("--property=WorkingDirectory=" + str(OPERATOR.APP), commands[0])

        settings = {"Description": OPERATOR.DESCRIPTION, "WorkingDirectory": str(OPERATOR.RUNTIME),
                    "ExecStart": str(OPERATOR.RUNTIME / "launch.sh"), "MemoryMax": str(1152 * OPERATOR.MIB),
                    "PrivateNetwork": "yes", "PrivateTmp": "yes", "PrivateDevices": "yes", "NoNewPrivileges": "yes",
                    "ProtectSystem": "strict", "ProtectHome": "yes", "KillMode": "control-group", "TasksMax": "256",
                    "LimitNOFILE": "65536", "UMask": "0077", "TimeoutStopUSec": "25s", "CPUQuotaPerSecUSec": "1.5s", "User": "root",
                    "ReadWritePaths": str(OPERATOR.DATA) + " " + str(OPERATOR.RUNTIME),
                    "ReadOnlyPaths": " ".join(sorted(OPERATOR.READ_ONLY_PATHS)), "BindReadOnlyPaths": str(OPERATOR.PACKAGE_ROOT)}
        identity = {"pid": 12345, "uid": 0, "exe": str(OPERATOR.APP / "system/EmbyServer"), "cwd": str(OPERATOR.APP),
                    "cmdline": [str(OPERATOR.APP / "system/EmbyServer"), "-programdata", str(OPERATOR.DATA)],
                    "networkNamespace": "net:[fresh]", "serviceProperties": settings}
        self.replace(OPERATOR, "owned_roots", lambda: None)
        self.replace(OPERATOR, "service_identity", lambda unit: copy.deepcopy(identity))
        self.replace(OPERATOR, "process_identity", lambda pid: {"networkNamespace": "net:[old]"})
        self.replace(OPERATOR, "private_devices_proof", lambda actual: {"synthetic": "no GPU devices"})
        self.replace(os, "readlink", lambda path: "net:[host]")
        mounts = {"/": "rw", **{path: "ro" for path in OPERATOR.READ_ONLY_PATHS}, str(OPERATOR.DATA): "rw", str(OPERATOR.RUNTIME): "rw"}
        self.replace(Path, "read_text", lambda path: "\n".join("1 0 0:1 / " + point + " " + options + " - tmpfs tmpfs rw"
                     for point, options in mounts.items()))
        self.assertEqual(OPERATOR.new_identity()["cwd"], str(OPERATOR.APP))
        identity["cwd"] = str(OPERATOR.RUNTIME)
        with self.assertRaisesRegex(RuntimeError, "relative ELF interpreter"):
            OPERATOR.new_identity()
        identity["cwd"] = str(OPERATOR.APP)
        settings["WorkingDirectory"] = str(OPERATOR.APP)
        with self.assertRaisesRegex(RuntimeError, "sandbox or launcher differs"):
            OPERATOR.new_identity()

    def install_failed_first_fixture(self) -> types.SimpleNamespace:
        files = {OPERATOR.FAILED_ROOT / name for name in OPERATOR.FAILED_FILES}
        files.update(OPERATOR.FAILED_BUNDLE_ROOT / name for name in OPERATOR.FAILED_BUNDLE_FILES)
        directories = {OPERATOR.FAILED_ROOT, OPERATOR.FAILED_BUNDLE_ROOT}
        for path in files:
            parent = path.parent
            while parent not in {OPERATOR.FAILED_ROOT.parent, OPERATOR.FAILED_BUNDLE_ROOT.parent}:
                directories.add(parent)
                parent = parent.parent
        directories.update(OPERATOR.FAILED_ROOT / name for name in ("private/raw", "private/wire", "export", "media-export"))
        intent_path = OPERATOR.FAILED_ROOT / "private/prepare-intent.json"
        cleanup_path = OPERATOR.FAILED_ROOT / "private/cleanup-attempt-1.json"
        records = {
            intent_path: {"marker": OPERATOR.FAILED_MARKER, "unit": OPERATOR.FAILED_UNIT,
                "evidenceRoot": str(OPERATOR.FAILED_ROOT), "programData": str(OPERATOR.FAILED_DATA),
                "sourceRoot": str(OPERATOR.FAILED_SOURCE), "operatorSha256": OPERATOR.FAILED_OPERATOR_SHA256},
            OPERATOR.FAILED_ROOT / "private/prepare-failure.json": {"message": "Fresh service failed to establish its process identity"},
            cleanup_path: {"attempt": 1, "unit": OPERATOR.FAILED_UNIT, "programData": str(OPERATOR.FAILED_DATA),
                "sourceRoot": str(OPERATOR.FAILED_SOURCE), "dataRemoved": True, "sourceRemoved": True, "evidenceRetained": True,
                "oldPreservationVerified": True, "controlInvalidityProvenOrNeverAcknowledged": True,
                "controlRevocation": {"acknowledged": False, "notApplicable": True, "invalidityProven": False},
                "stop": {"stopped": True, "ownedProcessGone": True, "ownedCgroupEmpty": True,
                         "unitMainPID": 0, "finalActiveState": "inactive"}},
        }
        fixture = types.SimpleNamespace(files=files, directories=directories, records=records, present=set(),
                                        hashes={path: "a" * 64 for path in files}, http=[],
                                        state={"MainPID": "0", "ActiveState": "inactive"}, cleanup_path=cleanup_path)
        for name, digest in OPERATOR.FAILED_BUNDLE_SOURCE_SHA256.items():
            fixture.hashes[OPERATOR.FAILED_BUNDLE_ROOT / name] = digest

        def walk(folder: Path, *, followlinks: bool):
            self.assertIn(folder, {OPERATOR.FAILED_ROOT, OPERATOR.FAILED_BUNDLE_ROOT})
            self.assertFalse(followlinks)
            for current in sorted(path for path in fixture.directories if path == folder or folder in path.parents):
                yield str(current), sorted(path.name for path in fixture.directories if path.parent == current), \
                    sorted(path.name for path in fixture.files if path.parent == current)

        def metadata(path: Path) -> object:
            self.assertIn(path, fixture.files | fixture.directories)
            return types.SimpleNamespace(st_uid=0, st_mode=(stat.S_IFDIR | 0o700) if path in fixture.directories else stat.S_IFREG | 0o600,
                                         st_nlink=1, st_size=1)

        self.replace(OPERATOR, "canonical", lambda path, **kwargs: metadata(path))
        self.replace(OPERATOR, "digest", lambda path: fixture.hashes[path])
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(fixture.records[path]))
        self.replace(OPERATOR, "properties", lambda unit: fixture.state if unit == OPERATOR.FAILED_UNIT else self.fail("Unexpected old unit lookup"))
        self.replace(os, "walk", walk)
        self.replace(os.path, "ismount", lambda path: False)
        self.replace(Path, "resolve", lambda path, **kwargs: path)
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "lstat", lambda path: metadata(path))
        self.replace(Path, "exists", lambda path: path in fixture.present)
        self.replace(Path, "iterdir", lambda path: iter(fixture.http))
        self.replace(Path, "read_text", lambda path: OPERATOR.FAILED_MARKER + "\n"
                     if path == OPERATOR.FAILED_ROOT / ".goby-managed" else self.fail("Unexpected old content read"))
        return fixture

    def test_failed_first_fixture_retains_all_14_root_and_25_bundle_files_with_hashes(self) -> None:
        fixture = self.install_failed_first_fixture()
        self.assertEqual(OPERATOR.FAILED_OPERATOR_SHA256, "f709780d76efb5adada682ec59b7a2a3695ad5c1d6da45137455ef71b1a907f0")
        proof = OPERATOR.failed_fixture_provenance()
        self.assertEqual((len(proof["files"]), len(proof["sourceBundleFiles"])), (14, 25))
        self.assertTrue(proof["dataAbsent"] and proof["sourceAbsent"] and proof["noOrdinaryLogin"] and proof["noSourceGeneration"])
        self.assertEqual(proof["httpRecordFiles"], 0)
        self.replace(OPERATOR, "old_baseline", lambda: {"failedFixtureProvenance": OPERATOR.failed_fixture_provenance()})
        expected = {"failedFixtureProvenance": proof}
        OPERATOR.verify_baseline(expected)
        fixture.hashes[OPERATOR.FAILED_ROOT / "runtime/service.log"] = "b" * 64
        with self.assertRaisesRegex(RuntimeError, "changed"):
            OPERATOR.verify_baseline(expected)
        fixture.hashes[OPERATOR.FAILED_BUNDLE_ROOT / "prepare-encoding-width-fresh.py"] = "c" * 64
        with self.assertRaisesRegex(RuntimeError, "source bundle differs"):
            OPERATOR.failed_fixture_provenance()

    def test_failed_first_fixture_refuses_missing_files_unproven_cleanup_or_live_resources(self) -> None:
        fixture = self.install_failed_first_fixture()
        missing = OPERATOR.FAILED_ROOT / "runtime/service.log"
        fixture.files.remove(missing)
        with self.assertRaisesRegex(RuntimeError, "file membership differs"):
            OPERATOR.failed_fixture_provenance()
        fixture.files.add(missing)
        fixture.records[fixture.cleanup_path]["dataRemoved"] = False
        with self.assertRaisesRegex(RuntimeError, "confirmed complete cleanup"):
            OPERATOR.failed_fixture_provenance()
        fixture.records[fixture.cleanup_path]["dataRemoved"] = True
        for path in (OPERATOR.FAILED_DATA, OPERATOR.FAILED_SOURCE, OPERATOR.FAILED_ROOT / "private/control-login-response.json"):
            with self.subTest(path=str(path)):
                fixture.present.add(path)
                with self.assertRaisesRegex(RuntimeError, "removed root, login or completed preparation artifact"):
                    OPERATOR.failed_fixture_provenance()
                fixture.present.remove(path)
        fixture.http.append(OPERATOR.FAILED_ROOT / "private/raw/unexpected.json")
        with self.assertRaisesRegex(RuntimeError, "HTTP or generated-media evidence"):
            OPERATOR.failed_fixture_provenance()
        fixture.http.clear()
        fixture.state["MainPID"] = "12345"
        with self.assertRaisesRegex(RuntimeError, "active service or process"):
            OPERATOR.failed_fixture_provenance()

    def test_handoff_uses_only_exact_token_hash_admin_user_and_device_without_login(self) -> None:
        recorder = self.recorder()
        credentials = {"REFERENCE_USERNAME": "synthetic-admin", "REFERENCE_PASSWORD": "synthetic-password",
                       "REFERENCE_TOKEN": recorder.control_token, "REFERENCE_USER_ID": recorder.control_user_id,
                       "REFERENCE_DEVICE_ID": RECORDER.CONTROL_DEVICE}
        login = {"AccessToken": recorder.control_token, "User": {"Id": recorder.control_user_id, "Policy": {"IsAdministrator": True}},
                 "SessionInfo": {"DeviceId": RECORDER.CONTROL_DEVICE}}
        recorder.manifest.update({"controlCredentialsFile": str(OPERATOR.CONTROL_CREDENTIALS), "controlLoginResponseFile": str(OPERATOR.CONTROL_LOGIN),
            "controlTokenSha256": hashlib.sha256(recorder.control_token.encode()).hexdigest(), "controlUserId": recorder.control_user_id})
        self.replace(RECORDER.base, "private_file", lambda path: self.assertEqual(path, OPERATOR.CONTROL_CREDENTIALS))
        self.replace(Path, "read_text", lambda path: "\n".join(key + "=" + value for key, value in credentials.items()))
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(login))
        recorder.load_handoff()
        self.assertEqual(set(recorder.logins), {"control"})
        self.assertEqual(recorder.logins["control"], login)
        for field, bad in (("REFERENCE_TOKEN", "foreign-token"), ("REFERENCE_USER_ID", "foreign-user"), ("REFERENCE_DEVICE_ID", "foreign-device")):
            with self.subTest(field=field):
                previous = credentials[field]
                credentials[field] = bad
                with self.assertRaises(RuntimeError):
                    recorder.load_handoff()
                credentials[field] = previous
        credentials["API_KEY"] = "not-an-ordinary-token"
        with self.assertRaisesRegex(RuntimeError, "credential fields differ"):
            recorder.load_handoff()

    def test_owned_source_verification_requires_exact_identity_codec_geometry_and_full_decode(self) -> None:
        identity = {"path": str(OPERATOR.SOURCE_FILE), "bytes": 1234, "sha256": "a" * 64, "device": 41, "dev": 41, "inode": 7}
        proof = {"sourceIdentity": identity, "complete": True, "videoCodec": "mpeg4", "audioCodec": "aac", "width": 3840,
                 "height": 2160, "decodedFrames": 8, "strictDecodePassed": True}
        self.replace(OPERATOR, "source_file_identity", lambda: copy.deepcopy(identity))
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(proof))
        self.assertEqual(OPERATOR.verify_source(), identity)
        for field, bad in (("videoCodec", "h264"), ("width", 1920), ("decodedFrames", 7), ("strictDecodePassed", False)):
            with self.subTest(field=field):
                previous = proof[field]
                proof[field] = bad
                with self.assertRaises(RuntimeError):
                    OPERATOR.verify_source()
                proof[field] = previous
        with self.assertRaises(RuntimeError):
            OPERATOR.verify_source({**identity, "inode": 8})

    def test_source_and_data_teardown_never_adopts_an_old_or_replaced_root(self) -> None:
        roots = {OPERATOR.DATA, OPERATOR.SOURCE}
        root_entries = {path: types.SimpleNamespace(st_dev=41, st_ino=index) for index, path in enumerate(sorted(roots), 1)}
        self.replace(OPERATOR, "owned_root", lambda path: None)
        self.replace(OPERATOR, "canonical", lambda path, **kwargs: root_entries[path])
        self.replace(OPERATOR, "properties", lambda unit: {"MainPID": "0", "ActiveState": "inactive"})
        saved = {OPERATOR.DATA_IDENTITY: {"path": str(OPERATOR.DATA), "device": 41, "inode": root_entries[OPERATOR.DATA].st_ino, "marker": OPERATOR.MARKER},
                 OPERATOR.SOURCE_IDENTITY: {"path": str(OPERATOR.SOURCE), "device": 41, "inode": root_entries[OPERATOR.SOURCE].st_ino, "marker": OPERATOR.MARKER}}
        deleted = []
        self.replace(OPERATOR, "atomic_save", lambda path, value: saved.update({path: copy.deepcopy(value)}))
        self.replace(OPERATOR, "load", lambda path: copy.deepcopy(saved[path]))
        self.replace(Path, "exists", lambda path: path in saved)
        self.replace(Path, "rglob", lambda path, pattern: iter([path / ".goby-managed"]))
        self.replace(Path, "resolve", lambda path, **kwargs: path)
        self.replace(Path, "is_symlink", lambda path: False)
        self.replace(Path, "lstat", lambda path: types.SimpleNamespace(st_uid=0, st_mode=stat.S_IFREG | 0o600, st_nlink=1))
        self.replace(os.path, "ismount", lambda path: False)
        self.replace(Path, "unlink", lambda path: deleted.append(path))
        self.replace(Path, "rmdir", lambda path: deleted.append(path))
        for foreign in (OPERATOR.ROOT, OPERATOR.OLD_DATA, OPERATOR.RETIRED_DATA, OPERATOR.RETIRED_SOURCE):
            with self.subTest(foreign=str(foreign)), self.assertRaises(RuntimeError):
                OPERATOR.remove_owned_tree(foreign)
        self.assertEqual(deleted, [])
        for root in (OPERATOR.SOURCE, OPERATOR.DATA):
            OPERATOR.remove_owned_tree(root)
        self.assertEqual(set(deleted), {OPERATOR.SOURCE, OPERATOR.DATA, OPERATOR.SOURCE / ".goby-managed", OPERATOR.DATA / ".goby-managed"})
        root_entries[OPERATOR.SOURCE].st_ino += 1
        with self.assertRaises(RuntimeError):
            OPERATOR.remove_owned_tree(OPERATOR.SOURCE)

    def install_download(self, *, eof: bool = True, declared_delta: int = 0, file_delta: int = 0) -> types.SimpleNamespace:
        recorder = self.recorder()
        recorder.current_width = 0
        result = self.playback_result(recorder)
        recorder.ack_playback(200, result, True, "owned-info")
        path = recorder.media_path(result, "owned-session")
        fixture = types.SimpleNamespace(recorder=recorder, path=path, payload=b"synthetic-complete-mp4", data=b"", records={},
                                        offset=0, eof=eof, closed=False, producing=False, handlers={}, events=[], width=640, height=360)
        self.replace(recorder, "check_authority", lambda: None)
        self.replace(recorder, "log_snapshot", lambda: {})
        row = {"pid": 12346, "startTicks": "99", "cmdline": self.producer_arguments()}
        self.replace(recorder, "producer_snapshot", lambda: [row] if fixture.producing else [])

        class Response:
            status, reason, version = 200, "Synthetic media", 11
            length = None

            def read1(self, limit: int) -> bytes:
                content = fixture.payload[fixture.offset:fixture.offset + limit]
                fixture.offset += len(content)
                return content

            def isclosed(self) -> bool:
                return fixture.eof

            def getheaders(self) -> list:
                return [("Content-Type", "video/mp4"), ("Content-Length", str(len(fixture.payload) + declared_delta))]

        class Connection:
            def request(inner, method: str, actual_path: str, *, headers: dict) -> None:
                self.assertEqual((method, actual_path), ("GET", path))
                self.assertEqual(headers["X-Emby-Token"], recorder.admin())
                fixture.producing = True
                fixture.handlers[signal.SIGALRM]()

            def getresponse(inner) -> Response:
                return Response()

            def close(inner) -> None:
                fixture.closed = True

        class Writer(io.BytesIO):
            def fileno(inner) -> int:
                return 101

            def close(inner) -> None:
                if not inner.closed:
                    fixture.data = inner.getvalue()
                super().close()

        def opened(target: Path, flags: int, mode: int) -> int:
            self.assertEqual(target, RECORDER.base.PRIVATE / "media/complete.mp4")
            self.assertEqual(flags, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW)
            self.assertEqual(mode, 0o600)
            return 101

        def digest(target: Path) -> str:
            if target.suffix == ".mp4":
                return hashlib.sha256(fixture.data).hexdigest()
            return hashlib.sha256(json.dumps(fixture.records[target], sort_keys=True).encode()).hexdigest()

        def stop(session: str, label: str) -> None:
            self.assertEqual(session, "owned-session")
            fixture.events.append("stop")
            fixture.producing = False
            recorder.stopped_sessions.add(session)

        def probe(target: Path, label: str, **kwargs: object) -> dict:
            self.assertIn("owned-session", recorder.stopped_sessions)
            self.assertEqual(kwargs, {"expected_video_codec": "h264", "expected_frames": 8})
            fixture.events.append("probe")
            return {"complete": True, "strictDecodePassed": True, "decodedFrames": 8, "videoCodec": "h264", "audioCodec": "aac",
                    "width": fixture.width, "height": fixture.height, "inputIdentity": {"sha256": digest(target), "bytes": len(fixture.data)}}

        self.replace(os, "open", opened)
        self.replace(os, "fdopen", lambda descriptor, mode: Writer())
        self.replace(os, "fsync", lambda descriptor: self.assertEqual(descriptor, 101))
        self.replace(Path, "stat", lambda target: types.SimpleNamespace(st_size=len(fixture.data) + file_delta))
        self.replace(RECORDER.base, "digest", digest)
        self.replace(RECORDER.base, "save", lambda target, value: fixture.records.update({target: copy.deepcopy(value)}))
        self.replace(recorder, "write", lambda label, value: fixture.records.update({label: copy.deepcopy(value)}))
        self.replace(recorder, "stop_session", stop)
        self.replace(OPERATOR, "probe_local_media", probe)
        self.replace(OPERATOR, "resources", lambda: {"MemAvailable": 1024 * 1024 * 1024})
        self.replace(http.client, "HTTPConnection", lambda host, port, **kwargs: Connection() if (host, port) == ("127.0.0.1", 18100)
                     else self.fail("Unexpected media origin"))
        self.replace(signal, "getsignal", lambda signum: signal.SIG_DFL)
        self.replace(signal, "signal", lambda signum, handler: fixture.handlers.update({signum: handler}))
        self.replace(signal, "setitimer", lambda *args: None)
        self.replace(time, "monotonic", lambda: 100.0)
        return fixture

    def test_media_success_requires_full_file_and_stops_producer_before_independent_probe(self) -> None:
        fixture = self.install_download()
        result = fixture.recorder.download("complete", fixture.path, "owned-session")
        self.assertEqual(fixture.data, fixture.payload)
        self.assertEqual(fixture.events, ["stop", "probe"])
        self.assertTrue(fixture.closed)
        self.assertEqual(result["configuredWidth"], 0)
        self.assertEqual((result["probe"]["width"], result["probe"]["height"]), (640, 360))
        self.assertEqual(result["outputBytes"], len(fixture.payload))
        with self.assertRaises(RuntimeError):
            fixture.recorder.download("again", fixture.path, "owned-session")

    def test_media_rejects_missing_eof_short_content_length_and_file_size_mismatch(self) -> None:
        for options in ({"eof": False}, {"declared_delta": 1}, {"file_delta": 1}):
            with self.subTest(options=options):
                fixture = self.install_download(**options)
                with self.assertRaisesRegex(RuntimeError, "not complete successful MP4 evidence"):
                    fixture.recorder.download("complete", fixture.path, "owned-session")
                self.assertEqual(fixture.events, ["stop"])
                self.assertTrue(fixture.closed)

    def test_changed_old_log_prefix_cannot_prove_a_new_case_encoder(self) -> None:
        recorder = self.recorder()
        path = RECORDER.DATA / "logs/ffmpeg.log"
        old = (shlex.join(self.producer_arguments()) + "\n").encode()
        content = old + b"Unrelated later log activity\n"
        before = {str(path): {"size": len(old), "sha256": hashlib.sha256(old).hexdigest(), "device": 41, "inode": 7}}
        after = {str(path): {"size": len(content), "sha256": hashlib.sha256(content).hexdigest(), "device": 41, "inode": 7}}
        self.replace(recorder, "log_snapshot", lambda: after)
        self.replace(Path, "read_bytes", lambda actual: content)
        stored = {}
        self.replace(RECORDER, "save_binary", lambda actual, value: stored.update({actual: value}))
        self.replace(RECORDER.base, "digest", lambda actual: hashlib.sha256(stored[actual]).hexdigest())
        self.replace(RECORDER.base, "save", lambda *args: None)
        with self.assertRaisesRegex(RuntimeError, "No actual software encoder command"):
            recorder.producer_proof("second", [], before)
        content = old + old
        after[str(path)].update({"size": len(content), "sha256": hashlib.sha256(content).hexdigest()})
        self.assertTrue(recorder.producer_proof("second-appended", [], before)["softwareCommandProved"])

    def install_json(self, recorder: object, responses: list[tuple[int, object]]) -> types.SimpleNamespace:
        fixture = types.SimpleNamespace(opened=0, closed=0, saved={})

        class Connection:
            def __init__(inner, status: int, body: object) -> None:
                inner.status, inner.reason, inner.version = status, "Synthetic response", 11
                inner.content = b"" if body is None else json.dumps(body).encode()
                inner.length = len(inner.content)

            def request(inner, *args: object, **kwargs: object) -> None:
                pass

            def getresponse(inner) -> Connection:
                return inner

            def getheaders(inner) -> list:
                return [("Content-Length", str(len(inner.content)))]

            def read(inner, limit: int) -> bytes:
                content = inner.content[:limit]
                inner.length -= len(content)
                return content

            def isclosed(inner) -> bool:
                return inner.length == 0

            def close(inner) -> None:
                fixture.closed += 1

        def connect(host: str, port: int, **kwargs: object) -> Connection:
            self.assertEqual((host, port), ("127.0.0.1", 18100))
            self.assertLess(fixture.opened, len(responses))
            status, body = responses[fixture.opened]
            fixture.opened += 1
            return Connection(status, body)

        self.replace(recorder, "check_authority", lambda: None)
        self.replace(http.client, "HTTPConnection", connect)
        self.replace(signal, "setitimer", lambda *args: None)
        self.replace(time, "monotonic", lambda: 100.0)
        self.replace(RECORDER.base, "save", lambda path, value: fixture.saved.update({path: copy.deepcopy(value)}))
        self.replace(RECORDER.base, "digest", lambda path: "a" * 64)
        self.replace(recorder, "write", lambda label, value: RECORDER.base.save(RECORDER.base.RAW / (label + ".json"), value))
        return fixture

    def test_playback_session_acknowledgement_precedes_fallible_response_persistence(self) -> None:
        recorder = self.recorder()
        recorder.current_width, recorder.encoding_dirty = 0, True
        recorder.width_attempts.add(0)
        fixture = self.install_json(recorder, [(200, self.playback_result(recorder))])
        body = RECORDER.playback_body(recorder.control_user_id, recorder.source_id, 0, 1)
        recorder.approve("playback-info", "POST", recorder.playback_path(), body)

        def save(path: Path, value: object) -> None:
            if path.parent == RECORDER.base.PRIVATE / "mutations":
                fixture.saved[path] = value
                return
            self.assertIn("owned-session", recorder.sessions)
            raise OSError("Synthetic response storage failure")

        self.replace(RECORDER.base, "save", save)
        with self.assertRaisesRegex(OSError, "response storage failure"):
            recorder.request("playback", "POST", recorder.playback_path(), token=recorder.admin(), body=body)
        self.assertIn("owned-session", recorder.sessions)
        self.assertEqual((fixture.opened, fixture.closed), (1, 1))

    def test_cleanup_retains_two_requests_and_bytes_for_logout_even_without_evidence_storage(self) -> None:
        self.assertEqual((RECORDER.CREDENTIAL_REQUEST_RESERVE, RECORDER.CREDENTIAL_BYTE_RESERVE), (2, 512 * 1024))
        for unavailable in (False, True):
            with self.subTest(initialization_evidence_unavailable=unavailable):
                recorder = self.recorder()
                fixture = self.install_json(recorder, [(204, None), (401, None)])
                recorder.finishing, recorder.evidence_unavailable = True, unavailable
                recorder.record_count = RECORDER.MAX_REQUESTS - 2
                recorder.charged_bytes = RECORDER.MAX_JSON_TOTAL - RECORDER.CREDENTIAL_BYTE_RESERVE
                with self.assertRaisesRegex(RuntimeError, "request budget exhausted"):
                    recorder.request("other-cleanup", "GET", RECORDER.ENCODING_ROUTE, token=recorder.admin())
                writes = []

                def fail_write(path: Path, value: object) -> None:
                    writes.append(path)
                    raise OSError("Synthetic storage exhausted")

                self.replace(RECORDER.base, "save", fail_write)
                recorder.logout("control")
                self.assertEqual(recorder.invalid_login_proofs["control"]["status"], 401)
                self.assertIn("control", recorder.logged_out)
                self.assertEqual((fixture.opened, fixture.closed), (2, 2))
                self.assertTrue(recorder.persistence_failures or recorder.cleanup_errors)
                if unavailable:
                    self.assertEqual(writes, [])

    def test_failed_stop_and_restore_have_only_one_owned_cleanup_compensation(self) -> None:
        recorder = self.recorder()
        recorder.finishing, recorder.current_width, recorder.encoding_dirty = True, 0, True
        recorder.sessions["owned-session"] = {"width": 0, "mediaPath": None}
        recorder.stop_attempts.add("owned-session")
        recorder.restore_attempts.add(0)
        producing, calls = [True], []
        self.replace(recorder, "check_authority", lambda: None)
        self.replace(recorder, "producer_snapshot", lambda: ["owned-producer"] if producing[0] else [])
        self.replace(time, "monotonic", lambda: 100.0)

        def request(label: str, method: str, path: str, **kwargs: object) -> tuple:
            calls.append((method, path))
            if method == "GET":
                return 200, RECORDER.encoding_body(recorder.encoding_baseline, 0)
            recorder.consume(recorder.pending)
            if method == "DELETE":
                producing[0] = False
                recorder.stop_acks.add("owned-session")
            return 204, None

        self.replace(recorder, "request", request)
        self.replace(recorder, "read_configuration", lambda label, expected: self.assertEqual(expected, recorder.encoding_baseline))
        recorder.stop_session("owned-session", "cleanup-stop")
        self.assertEqual(recorder.cleanup_stop_retries, {"owned-session"})
        with self.assertRaises(RuntimeError):
            recorder.approve("encoder-stop", "DELETE", recorder.stop_path("owned-session"))
        recorder.restore("cleanup-restore")
        self.assertEqual(recorder.cleanup_restore_retries, {0})
        self.assertTrue(recorder.restored)
        recorder.encoding_dirty = True
        with self.assertRaises(RuntimeError):
            recorder.restore("forbidden-third-restore")
        self.assertEqual(sum(method == "DELETE" for method, path in calls), 1)
        self.assertEqual(sum(method == "POST" for method, path in calls), 1)


def main() -> None:
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
        print(json.dumps({"suite": "encoding-width-reference-guards", "result": "blocked",
                          "reason": "Authorized root SSH and one width recorder source path are required"}))
        raise SystemExit(2)
    source = Path(sys.argv[1]).resolve(strict=True)
    operator = source.with_name("prepare-encoding-width-fresh.py")
    paths = [source, operator, *[source.with_name(name) for name in
             ("reference-configuration.py", "reference-scheduled-tasks.py", "reference-devices.py", "reference-api-keys.py")]]
    contents = {str(path): path.read_bytes() for path in paths}
    suite_bytes = Path(__file__).read_bytes()
    for filename, content in contents.items():
        SOURCE_LINES[filename] = content.decode().splitlines(keepends=True)
        SOURCE_HASHES[Path(filename).name] = hashlib.sha256(content).hexdigest()
    SOURCE_LINES[__file__] = suite_bytes.decode().splitlines(keepends=True)
    code = {filename: compile(content, filename, "exec") for filename, content in contents.items()}

    class MemoryLoader:
        def __init__(self, filename: str) -> None:
            if filename not in code:
                raise AssertionError("Unexpected width-study source import: " + filename)
            self.filename = filename

        def create_module(self, specification: object) -> None:
            return None

        def exec_module(self, module: types.ModuleType) -> None:
            module.__file__ = self.filename
            exec(code[self.filename], module.__dict__)

    def specification(name: str, filename: object, **kwargs: object) -> object:
        if kwargs:
            raise AssertionError("Unexpected source import options")
        return importlib.util.spec_from_loader(name, MemoryLoader(str(filename)), origin=str(filename))

    def cached_path(path: Path, **kwargs: object) -> Path:
        if str(path) not in contents:
            raise AssertionError("Import attempted non-source path inspection: " + str(path))
        return path

    global RECORDER, OPERATOR
    RECORDER, OPERATOR = types.ModuleType("synthetic_width_recorder"), types.ModuleType("synthetic_width_operator")
    RECORDER.__file__, OPERATOR.__file__ = str(source), str(operator)
    result, import_failure = unittest.TestResult(), None
    with memory_tracebacks():
        try:
            with EffectFence(), patch.object(importlib.util, "spec_from_file_location", specification), \
                    patch.object(os, "environ", {"GOBY_DEVICE_REFERENCE_RUN": "2"}), \
                    patch.object(Path, "resolve", cached_path), \
                    patch.object(Path, "is_symlink", lambda path: False if str(cached_path(path)) in contents else True), \
                    patch.object(Path, "read_bytes", lambda path: contents[str(cached_path(path))]):
                exec(code[str(operator)], OPERATOR.__dict__)
                exec(code[str(source)], RECORDER.__dict__)
        except BaseException as error:
            import_failure = {"type": type(error).__name__, "message": str(error)[:1000]}
        else:
            unittest.defaultTestLoader.loadTestsFromTestCase(EncodingWidthGuards).run(result)
    failures = [*result.failures, *result.errors]
    summaries = [{"test": test.id(), "message": detail.rstrip().splitlines()[-1]} for test, detail in failures[:8]]
    passed = import_failure is None and result.wasSuccessful() and result.testsRun > 0
    print(json.dumps({"suite": "encoding-width-reference-guards", "result": "passed" if passed else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors), "skipped": len(result.skipped),
                      "sourceSha256": SOURCE_HASHES, "testSha256": hashlib.sha256(suite_bytes).hexdigest(),
                      "httpRequests": 0, "mediaProcesses": 0, "captureWrites": 0, "fixtures": "synthetic-memory-only",
                      "importFailure": import_failure, "failureSummaries": summaries,
                      "failureSummariesOmitted": max(0, len(failures) - 8)}))
    raise SystemExit(0 if passed else 1)


if __name__ == "__main__":
    main()
