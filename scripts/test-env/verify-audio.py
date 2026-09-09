#!/usr/bin/env python3
"""Verify deployed Goby universal/progressive audio only through SSH.

Schema 12, enabled conversion, and scoped ProbeVersion 3 audio facts are required
before media verification. Existing reference sources are copied byte-for-byte
into one marked Goby fixture and never changed. HTTP and media-process output
are bounded; credentials stay in memory, private files, or child environment.
Only sanitized JSON is printed and saved. No credential URL appears in argv.
"""

from __future__ import annotations

import importlib.util
import json
import math
import os
from pathlib import Path
import re
import secrets
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from urllib.parse import parse_qs, quote, unquote, urlencode, urlsplit, urlunsplit


sys.dont_write_bytecode = True
try:
    spec = importlib.util.spec_from_file_location("goby_hls_support", Path(__file__).with_name("verify-hls.py"))
    hls = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(hls)
    smoke = hls.smoke
except Exception:
    print(json.dumps({"status": "failed", "failed_stage": "helper import",
                      "error": "The sibling HLS and direct-playback helpers are unavailable", "cleanup_errors": []}))
    raise SystemExit(1) from None

ROOT = Path("/opt/goby-fixtures/audio-m4c")
REFERENCE = Path("/dev/shm/goby-emby-reference/runtime/audio-m4c")
RECORD = Path("/opt/goby-test/audio-m4c-goby-verification.json")
RESULT = Path("/opt/goby-test/m4c-deployed-audio.json")
OWNER = "goby-audio-m4c-verification-v1"
NAME = "Goby Audio M4c verification"
RUNTIME_ENV = Path("/opt/goby-test/runtime.env")
FFMPEG = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg"
FFPROBE = "/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffprobe"
MAX_BODY = 3 * 1024 * 1024
MAX_PROCESS = 2 * 1024 * 1024
ADTS_SAMPLES = 289792
TAIL_NAME = "Goby Audio M4c Short Tail.flac"
SOURCE_FACTS = {
    "mp3": (97098, "75697f6abbda787e48de57462fef4c75c2decaa07576ef95d2105fd8151ed61b", "audio/mpeg"),
    "flac": (716925, "843ce1829f3ef1807233e4a0ed874d9132fa87f581b07298576ac18ea4216c81", "audio/flac"),
    "aac": (50440, "ae7f1c88684f05ec883b327dd2b1fae17445b3dfaba446c02613c2f70ac46160", "audio/aac"),
    "wav": (576160, "37fc3c2a29ff734981c1217003888b8aa7207fcf4c9f498c19fe9535ce36c8b5", "audio/wav"),
}


def check(condition: bool, label: str) -> None:
    smoke.check(condition, label)


def sql(value: str) -> str:
    check(isinstance(value, str) and len(value) <= 1024 and not any(ord(character) < 32 for character in value),
          "A scoped SQL selector is invalid")
    return "'" + value.replace("'", "''") + "'"


class Database:
    """Use only the configured local test database with read-only transactions."""

    def __init__(self) -> None:
        smoke.private_file(RUNTIME_ENV)
        check(RUNTIME_ENV.stat().st_size <= 65536, "Runtime configuration exceeds its private size limit")
        address = ""
        for line in RUNTIME_ENV.read_text(encoding="utf-8").splitlines():
            key, separator, raw = line.strip().removeprefix("export ").partition("=")
            if separator and key == "GOBY_DATABASE_URL":
                fields = shlex.split(raw, comments=False, posix=True)
                check(len(fields) == 1, "Runtime database configuration has an unsupported assignment")
                address = fields[0]
        parsed = urlsplit(address)
        check(parsed.scheme in {"postgres", "postgresql"} and parsed.hostname == "127.0.0.1" and
              unquote(parsed.path.lstrip("/")) == "goby_test" and parsed.port in {None, 5432} and
              bool(parsed.username and parsed.password), "The runtime database is not the authorized local test database")
        self.environment = dict(os.environ)
        self.environment.update({"PGHOST": parsed.hostname, "PGPORT": str(parsed.port or 5432),
                                 "PGDATABASE": "goby_test", "PGUSER": unquote(parsed.username),
                                 "PGPASSWORD": unquote(parsed.password), "PGCONNECT_TIMEOUT": "5",
                                 "PGSSLMODE": "disable", "PGCLIENTENCODING": "UTF8",
                                 "PGOPTIONS": "-c default_transaction_read_only=on -c statement_timeout=5000 -c lock_timeout=2000"})

    def read(self, query: str, label: str):
        # SQL and opaque selectors use stdin; database credentials use only the
        # child environment. Neither application usernames nor URLs enter argv.
        result = subprocess.run(["psql", "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"],
                                input=query.encode("utf-8"), env=self.environment,
                                capture_output=True, timeout=10)
        check(result.returncode == 0 and len(result.stdout) <= MAX_PROCESS and len(result.stderr) <= MAX_PROCESS,
              label + ": read-only database observation failed")
        return json.loads(result.stdout)

    def reference(self, api, nonce: str):
        query = "SELECT COALESCE((SELECT row_to_json(v) FROM (SELECT r.play_session_id, p.state, p.item_id, " \
                "(SELECT count(*) FROM encoding_jobs e WHERE e.auth_session_id=r.auth_session_id AND e.play_session_id=r.play_session_id) AS jobs, " \
                "(SELECT count(*) FROM encoding_jobs e WHERE e.auth_session_id=r.auth_session_id AND e.play_session_id=r.play_session_id AND e.state IN ('queued','running')) AS active_jobs, " \
                "(SELECT COALESCE(json_agg(e.id ORDER BY e.id),'[]'::json) FROM encoding_jobs e WHERE e.auth_session_id=r.auth_session_id AND e.play_session_id=r.play_session_id) AS job_ids " \
                "FROM client_playback_references r JOIN play_sessions p ON p.id=r.play_session_id WHERE r.user_id=" + sql(api.user_id) + \
                " AND r.auth_session_id=" + sql(api.session_id) + " AND r.device_id=" + sql(smoke.DEVICE_ID) + \
                " AND r.client_nonce=" + sql(nonce) + ") v),'null'::json);"
        return self.read(query, "Owned client nonce and encoding state")

    def active(self, api) -> int:
        return self.read("SELECT to_json(count(*)) FROM encoding_jobs WHERE auth_session_id=" + sql(api.session_id) +
                         " AND state IN ('queued','running');", "Owned active encoder count")


class Scratch:
    def __init__(self) -> None:
        self.path = None
        self.names = set()
        self.marker = OWNER + ":" + secrets.token_hex(16)

    def create(self) -> None:
        self.path = Path(tempfile.mkdtemp(prefix="goby-audio-m4c-verify-", dir="/dev/shm"))
        self.path.chmod(0o700)
        self.write(".goby-managed", self.marker.encode())

    def write(self, name: str, content: bytes) -> Path:
        check(self.path is not None and Path(name).name == name and len(content) <= MAX_BODY,
              "Audio scratch output must use a bounded owned filename")
        target = self.path / name
        descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        self.names.add(name)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(content)
        return target

    def run(self, arguments: list[str], label: str, *, timeout=30) -> bytes:
        import resource

        def limits() -> None:
            resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_PROCESS, MAX_PROCESS))

        with tempfile.TemporaryFile(dir=self.path) as output, tempfile.TemporaryFile(dir=self.path) as errors:
            result = subprocess.run(arguments, stdin=subprocess.DEVNULL, stdout=output, stderr=errors,
                                    timeout=timeout, preexec_fn=limits)
            output.seek(0)
            body = output.read(MAX_PROCESS + 1)
            errors.seek(0)
            error_body = errors.read(MAX_PROCESS + 1)
            check(len(body) <= MAX_PROCESS and len(error_body) <= MAX_PROCESS, label + ": media output limit exceeded")
            categories = [name for needle, name in ((b"invalid data", "invalid_media"), (b"http error", "http_error"),
                          (b"corrupt", "corrupt_packet"), (b"unrecognized option", "unsupported_option")) if needle in error_body.lower()]
            category = ",".join(categories) or "media_process_error"
            check(result.returncode == 0, label + f": media process failed (exit {result.returncode}, {category})")
            check(not error_body, label + ": media process reported " + category)
            return body

    def cleanup(self) -> None:
        if self.path is None:
            return
        check(not self.path.is_symlink() and self.path.resolve(strict=True).parent == Path("/dev/shm") and
              self.path.name.startswith("goby-audio-m4c-verify-") and
              (self.path / ".goby-managed").read_text() == self.marker,
              "Audio scratch cleanup ownership does not match")
        check({entry.name for entry in self.path.iterdir()} == self.names, "Unexpected files appeared in audio scratch")
        for name in self.names:
            smoke.private_file(self.path / name)
        for name in self.names - {".goby-managed"}:
            (self.path / name).unlink()
        (self.path / ".goby-managed").unlink()
        self.path.rmdir()


def private_json(path: Path, value: dict, *, create=False) -> None:
    if create:
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, indent=2, sort_keys=True)
        return
    smoke.private_file(path)
    descriptor, temporary = tempfile.mkstemp(prefix=".goby-audio-json-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, indent=2, sort_keys=True)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def reference_hashes() -> dict[Path, str]:
    check(REFERENCE.resolve(strict=True) == REFERENCE and
          (REFERENCE / ".goby-managed").read_text().strip() == "goby-audio-m4c-owned-v1",
          "The immutable reference audio source ownership marker does not match")
    hashes = {}
    for kind, (size, expected, _) in SOURCE_FACTS.items():
        path = REFERENCE / "source" / ("Reference Audio M4c." + kind)
        info = path.lstat()
        check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_size == size and digest_checked(path) == expected,
              "An immutable reference audio source does not match its recorded hash and size")
        hashes[path] = expected
    return hashes


def digest_checked(path: Path) -> str:
    check(not path.is_symlink() and path.is_file() and 0 < path.stat().st_size <= MAX_BODY,
          "A source or copied fixture is not a bounded regular file")
    return smoke.digest(path)


def copy_fixture(owned, hashes: dict[Path, str], scratch: Scratch) -> tuple[dict, dict[str, Path]]:
    marker = ROOT / ".goby-managed"
    if not ROOT.exists() and not ROOT.is_symlink():
        check(shutil.disk_usage(ROOT.parent).free >= 12 * 1024 * 1024,
              "Insufficient root filesystem space for the bounded owned audio copies")
        ROOT.mkdir(mode=0o755)
        ROOT.chmod(0o755)
        private_json(marker, {"owner": OWNER, "fixture_nonce": owned.state["nonce"]}, create=True)
    info = ROOT.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and ROOT.resolve(strict=True) == ROOT,
          "Goby audio fixture directory ownership does not match")
    check(smoke.bounded_json(marker) == {"owner": OWNER, "fixture_nonce": owned.state["nonce"]},
          "Goby audio fixture marker does not match the owned account")
    if RECORD.exists() or RECORD.is_symlink():
        record = smoke.bounded_json(RECORD)
        check(record.get("owner") == OWNER and record.get("fixture_nonce") == owned.state["nonce"] and
              record.get("root") == str(ROOT) and record.get("viewer_id") == owned.state["user_id"],
              "Private audio library ownership record does not match")
    else:
        record = {"owner": OWNER, "fixture_nonce": owned.state["nonce"], "root": str(ROOT),
                  "viewer_id": owned.state["user_id"], "library_id": "", "media_hashes": {}}
        private_json(RECORD, record, create=True)
    paths = {kind: ROOT / ("Reference Audio M4c." + kind) for kind in SOURCE_FACTS}
    paths["tail"] = ROOT / TAIL_NAME
    check({entry.name for entry in ROOT.iterdir()} <= {".goby-managed", *(path.name for path in paths.values())},
          "Unexpected files exist in the owned Goby audio directory")
    for source, expected in hashes.items():
        target = ROOT / source.name
        if not target.exists() and not target.is_symlink():
            descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(descriptor, "wb") as stream:
                stream.write(source.read_bytes())
            target.chmod(0o644)
        check(digest_checked(target) == expected, "An owned audio copy differs from its immutable reference")
        record["media_hashes"][target.name] = expected
    tail = paths["tail"]
    if not tail.exists() and not tail.is_symlink():
        check(TAIL_NAME not in record["media_hashes"], "Previously recorded short-tail audio source is missing")
        generated = scratch.path / "generated-tail.flac"
        try:
            scratch.run([FFMPEG, "-hide_banner", "-nostdin", "-v", "error", "-n", "-filter_threads", "1", "-f", "lavfi", "-i",
                         "sine=frequency=733:sample_rate=48000:duration=6.001", "-t", "6.001", "-ac", "2", "-ar", "48000",
                         "-c:a", "flac", "-sample_fmt", "s16", "-threads:a", "1", str(generated)], "Owned exact short-tail audio generation")
        finally:
            if generated.exists():
                scratch.names.add(generated.name)
        content = generated.read_bytes()
        check(0 < len(content) <= MAX_BODY, "Generated short-tail source exceeds its size limit")
        descriptor = os.open(tail, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(content)
        tail.chmod(0o644)
        record["media_hashes"][TAIL_NAME] = digest_checked(tail)
    check(TAIL_NAME in record["media_hashes"] and digest_checked(tail) == record["media_hashes"][TAIL_NAME],
          "Owned exact short-tail source has no matching creation hash")
    private_json(RECORD, record)
    return record, paths


def library(api, record: dict) -> str:
    listed = api.request("GET", "/admin/v1/libraries", admin=True, label="Owned audio library lookup")["Items"]
    matches = [entry for entry in listed if entry.get("Name") == NAME or str(ROOT) in entry.get("Paths", [])]
    check(len(matches) <= 1, "Owned audio library lookup is ambiguous")
    if matches:
        entry = matches[0]
        check(entry.get("Name") == NAME and entry.get("Paths") == [str(ROOT)] and entry.get("CollectionType") == "music" and
              (record.get("library_id") == entry.get("Id") or not record.get("library_id") and record.get("creation_pending") is True),
              "Existing audio library has no matching ownership record")
    else:
        check(not record.get("library_id"), "Previously recorded audio library is missing")
        record["creation_pending"] = True
        private_json(RECORD, record)
        entry = api.request("POST", "/admin/v1/libraries", admin=True, expected=(201,), label="Owned audio library creation",
                            body={"Name": NAME, "CollectionType": "music", "Paths": [str(ROOT)], "Scan": False})["Library"]
    record["library_id"], record["creation_pending"] = entry["Id"], False
    private_json(RECORD, record)
    return entry["Id"]


def nonce() -> str:
    return "audio-m4c-" + secrets.token_hex(16)


def universal(api, item_id: str, parameters=None) -> str:
    query = {"DeviceId": smoke.DEVICE_ID, "api_key": api.token}
    query.update(parameters or {})
    return f"/emby/Audio/{quote(item_id)}/universal?" + urlencode(query)


def mp3_parameters(reference: str, start=0) -> dict:
    return {"PlaySessionId": reference, "Container": "mp3", "TranscodingContainer": "mp3", "TranscodingProtocol": "",
            "AudioCodec": "mp3", "MaxStreamingBitrate": 128000, "AudioSampleRate": 48000, "AudioChannels": 2,
            "StartTimeTicks": start * smoke.TICKS}


def progressive_headers(headers: dict, mime: str) -> None:
    check(headers.get("content-type") == mime and headers.get("accept-ranges") == "none" and
          all(key not in headers for key in ("content-length", "content-range", "etag")) and
          "no-store" in headers.get("cache-control", ""),
          "Progressive audio advertised a fixed length, byte range, validator, or incorrect MIME type")


def audio_probe(scratch: Scratch, content: bytes, name: str, *, expected_codec: str, seconds=None,
                channels=2, sample_rate=48000, exact_samples=None, max_bitrate=None, hls_input=False) -> dict:
    path = scratch.write(name, content)
    network = ["-protocol_whitelist", "file,http,tcp,pipe", "-rw_timeout", "15000000", "-allowed_extensions", "m3u8,ts", "-prefer_x_start", "0"] if hls_input else []
    facts = json.loads(scratch.run([FFPROBE, "-v", "error", *network, "-show_streams", "-show_format", "-of", "json", str(path)],
                                  "Received audio ffprobe", timeout=30))
    streams = facts.get("streams", [])
    check(len(streams) == 1 and streams[0].get("codec_type") == "audio" and streams[0].get("codec_name") == expected_codec and
          streams[0].get("channels") == channels and int(streams[0].get("sample_rate", "0")) == sample_rate,
          "Received audio codec, channel count, or sample rate does not match")
    if max_bitrate is not None:
        check(0 < int(streams[0].get("bit_rate", "0")) <= max_bitrate, "Progressive audio exceeded its declared bitrate ceiling")
    pcm = scratch.run([FFMPEG, "-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-threads", "1", *network,
                       "-i", str(path), "-map", "0:a:0", "-vn", "-sn", "-dn", "-ac", str(channels), "-ar", str(sample_rate),
                       "-c:a", "pcm_s16le", "-threads:a", "1", "-f", "s16le", "-"], "Complete audio sample decode", timeout=45)
    check(len(pcm) % (2 * channels) == 0, "Decoded PCM ended inside an audio sample frame")
    samples = len(pcm) // (2 * channels)
    duration = samples / sample_rate
    if exact_samples is not None:
        check(samples == exact_samples, f"Exact decoded sample count differs (received {samples}, expected {exact_samples})")
    if seconds is not None:
        check(abs(duration - seconds) <= 0.08, f"Decoded audio duration differs (received {duration:.6f}, expected {seconds:.6f})")
    return {"codec": expected_codec, "channels": channels, "sample_rate": sample_rate,
            "decoded_samples": samples, "decoded_seconds": round(duration, 6), "probe_exit": 0, "decode_exit": 0}


def user_data(api, ids: dict[str, str]) -> dict:
    return {key: api.request("GET", f"/emby/Users/{quote(api.user_id)}/Items/{quote(item_id)}", emby=True,
                             label="Owned audio user-data snapshot")["UserData"] for key, item_id in ids.items()}


def cleanup_encoding(api, reference: str, method="DELETE") -> None:
    path = "/emby/Videos/ActiveEncodings" + ("/Delete" if method == "POST" else "")
    query = urlencode({"DeviceId": smoke.DEVICE_ID, "PlaySessionId": reference})
    api.request(method, path + "?" + query, emby=True, expected=(204,), parse=False, label="Owned audio nonce encoding cleanup")


def wait_inactive(db: Database, api) -> None:
    deadline = time.monotonic() + 5
    while db.active(api):
        check(time.monotonic() < deadline, "Owned encoding jobs did not become inactive after cleanup")
        time.sleep(0.1)


def main() -> int:
    os.umask(0o077)
    api, second, owned, scratch = smoke.API(), smoke.API(), smoke.OwnedFixture(), Scratch()
    http = hls.HTTP(api)
    summary = {"status": "failed", "assertions": [], "cleanup_errors": []}
    db = None
    record = None
    hashes = {}
    ids = {}
    baseline = None
    active_scan = ""
    references = []
    stage = "deployment schema and capabilities"
    try:
        check(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
              "Run only through SSH on the authorized Linux test host")
        db = Database()
        schema = db.read("SELECT json_build_object('version',max(version),'count',count(*)) FROM schema_migrations;", "Deployment schema")
        check(schema.get("version") == 12 and schema.get("count") == 12,
              "Deployment schema must be exactly 12 before audio verification")
        api.request("GET", "/readyz", label="Goby audio deployment readiness")
        api.admin_login(smoke.credentials())
        capabilities = api.request("GET", "/admin/v1/capabilities", admin=True, label="Deployed audio capabilities")
        check(capabilities.get("Features", {}).get("Playback") is True and capabilities.get("Features", {}).get("Transcoding") is True and
              capabilities.get("Toolchain", {}).get("FFmpeg") == "9.0.1", "Deployed playback/transcoding capabilities or pinned FFmpeg do not match")
        pid = int(subprocess.check_output(["systemctl", "show", "goby-foundation-test.service", "-p", "MainPID", "--value"], timeout=5))
        uid_line = next(line for line in Path(f"/proc/{pid}/status").read_text().splitlines() if line.startswith("Uid:"))
        uids = [int(value) for value in uid_line.split()[1:]]
        check(pid > 1 and len(uids) == 4 and all(value > 0 for value in uids), "Goby must run with non-root process credentials")
        summary["deployment"] = {"schema_version": 12, "main_pid": pid, "effective_uid": uids[1], "non_root": True,
                                 "transcoding_enabled": True, "ffmpeg": "9.0.1"}
        check((smoke.DIRECTORY / smoke.STATE_NAME).is_file(), "The reusable owned viewer has not been prepared")
        owned.open()
        scratch.create()
        hashes = reference_hashes()
        api.viewer_login(owned.state)
        second.viewer_login(owned.state)
        check(api.session_id != second.session_id and api.token != second.token, "Audio verification requires two independent login sessions")
        record, paths = copy_fixture(owned, hashes, scratch)
        library_id = library(api, record)
        stage = "owned audio scan and ProbeVersion 3 gate"
        started = api.request("POST", f"/admin/v1/libraries/{quote(library_id)}/scan", admin=True, expected=(202,), label="Owned audio scan")["Job"]
        active_scan = started["Id"]
        scanned = api.wait_job(started["Id"])
        active_scan = ""
        check(scanned.get("Status") == "completed" and scanned.get("Scanned") == 5 and not scanned.get("Error"),
              "Owned audio scan did not inspect exactly five clean files")
        facts = db.read("SELECT COALESCE(json_agg(json_build_object('Id',id,'Path',path,'Media',media)),'[]'::json) "
                        "FROM items WHERE library_id=" + sql(library_id) + " AND type='Audio';", "Scoped audio source-version facts")
        check(len(facts) == 5, "The owned audio catalog contains an unexpected item count")
        source_facts = {}
        for key, path in paths.items():
            matches = [entry for entry in facts if entry["Path"] == str(path)]
            check(len(matches) == 1, "An owned audio source is absent from the scoped catalog")
            fact = matches[0]
            media = fact.get("Media") or {}
            check(media.get("ProbeVersion") == 3 and media.get("AudioDurationExact") is True,
                  "Scoped scan must produce ProbeVersion 3 and exact audio duration before media requests")
            audio = [stream for stream in media.get("Streams", []) if stream.get("CodecType") == "audio"]
            check(len(audio) == 1 and (audio[0].get("AudioTiming") or {}).get("Exact") is True,
                  "Scoped ProbeVersion 3 audio lacks exact decoded timing")
            ids[key], source_facts[key] = fact["Id"], audio[0]
        timing = source_facts["aac"]["AudioTiming"]
        check(timing.get("SampleCount") == ADTS_SAMPLES and timing.get("EndTicks") == (ADTS_SAMPLES * smoke.TICKS + 47999) // 48000,
              "ADTS indexed duration is not backed by all 289792 decoded samples")
        check(source_facts["tail"]["AudioTiming"].get("SampleCount") == 288048 and
              source_facts["tail"]["AudioTiming"].get("EndTicks") == 60010000, "The new short-tail source is not exactly 6.001 seconds")
        baseline = user_data(api, ids)
        check(all(data.get("PlaybackPositionTicks") == 0 for data in baseline.values()),
              "An owned test item has existing resume progress; verification will not overwrite it")
        summary["assertions"].append({"stage": stage, "audio_items": 5, "probe_version": 3,
                                      "exact_adts_samples": ADTS_SAMPLES, "tail_samples": 288048})

        stage = "bare universal original bytes HEAD Range and 304"
        for kind, (size, expected_hash, mime) in SOURCE_FACTS.items():
            target = universal(api, ids[kind])
            headers, content = http.request("GET", target, "Bare original audio GET")
            check(content == paths[kind].read_bytes() and headers.get("content-type") == mime and
                  headers.get("content-length") == str(size) and headers.get("accept-ranges") == "bytes" and bool(headers.get("etag")),
                  "Bare universal did not serve the truthful complete original representation")
            head, body = http.request("HEAD", target, "Bare original audio HEAD")
            check(not body and all(head.get(key) == headers.get(key) for key in ("content-type", "content-length", "etag")),
                  "Original audio HEAD differs from GET metadata")
            ranged, body = http.request("GET", target, "Original audio byte range", expected=(206,), headers={"Range": "bytes=7-38"})
            check(body == content[7:39] and ranged.get("content-range") == f"bytes 7-38/{size}", "Original audio range differs from source bytes")
            conditional = {"If-None-Match": headers["etag"]}
            _, body = http.request("GET", target, "Conditional original audio GET", expected=(304,), headers=conditional)
            check(not body, "Conditional original audio returned a body")
            for token in (None, "invalid-goby-audio-token"):
                http.request("GET", hls.change_token(target, token), "Missing or invalid original audio token", expected=(401,), headers=conditional)
        summary["assertions"].append({"stage": stage, "formats": list(SOURCE_FACTS), "original": 200, "head": 200,
                                      "range": 206, "conditional": 304, "missing_and_invalid_token": 401, "adts_mime": "audio/aac"})

        stage = "progressive HEAD without an encoder"
        head_nonce = nonce()
        references.append((api, head_nonce))
        check(db.reference(api, head_nonce) is None, "Fresh header-only nonce already exists")
        head, body = http.request("HEAD", universal(api, ids["flac"], mp3_parameters(head_nonce)), "Progressive audio HEAD")
        progressive_headers(head, "audio/mpeg")
        head_state = db.reference(api, head_nonce)
        check(not body and head_state is not None and head_state["jobs"] == 0 and head_state["state"] == "Prepared",
              "Progressive HEAD created an encoding job or returned a body")
        summary["assertions"].append({"stage": stage, "head": 200, "content_length_omitted": True, "encoding_jobs": 0})

        stage = "progressive MP3 full seek retry and nonce isolation"
        full_nonce, seek_nonce = nonce(), nonce()
        references.extend(((api, full_nonce), (api, seek_nonce), (second, full_nonce)))
        full_target = universal(api, ids["flac"], mp3_parameters(full_nonce))
        headers, full = http.request("GET", full_target, "Fresh progressive MP3 with byte range", headers={"Range": "bytes=0-31"})
        progressive_headers(headers, "audio/mpeg")
        full_decode = audio_probe(scratch, full, "whole.mp3", expected_codec="mp3", seconds=6, max_bitrate=128000)
        first_state = db.reference(api, full_nonce)
        check(first_state is not None and first_state["jobs"] == 1 and first_state["item_id"] == ids["flac"],
              "Fresh progressive GET did not create exactly one scoped encoding job")
        _, retried = http.request("GET", full_target, "Identical progressive MP3 retry")
        retry_state = db.reference(api, full_nonce)
        check(retried == full and retry_state["job_ids"] == first_state["job_ids"] and retry_state["play_session_id"] == first_state["play_session_id"],
              "Identical source and quality retry did not reuse its owned completed output")
        check(db.reference(second, full_nonce) is None, "A client nonce crossed authentication scope before use")
        second_target = universal(second, ids["flac"], mp3_parameters(full_nonce))
        _, second_body = http.request("GET", second_target, "Same nonce from another authentication session")
        second_state = db.reference(second, full_nonce)
        check(second_state is not None and second_state["jobs"] == 1 and second_state["play_session_id"] != first_state["play_session_id"] and
              second_state["job_ids"] != first_state["job_ids"] and bool(second_body), "Another authentication session reused the first cache identity")
        http.request("GET", universal(api, ids["tail"], mp3_parameters(full_nonce)), "Same nonce with a different source", expected=(404,))
        check(db.reference(api, full_nonce)["play_session_id"] == first_state["play_session_id"], "Cross-source rejection rebound an existing nonce")
        seek_target = universal(api, ids["flac"], mp3_parameters(seek_nonce, 2))
        headers, sought = http.request("GET", seek_target, "Fresh progressive MP3 seek")
        progressive_headers(headers, "audio/mpeg")
        seek_decode = audio_probe(scratch, sought, "seek.mp3", expected_codec="mp3", seconds=4, max_bitrate=128000)
        check(user_data(api, ids) == baseline, "Progressive audio fetching fabricated user playback history")
        summary["assertions"].append({"stage": stage, "whole": full_decode, "seek_two_seconds": seek_decode,
                                      "range_ignored_status": 200, "retry_reuses_job": True, "cross_auth_nonce_isolated": True,
                                      "cross_source_nonce": 404, "user_data_unchanged": True})

        stage = "ADTS to WAV exact integer samples"
        wav_nonce = nonce()
        references.append((api, wav_nonce))
        target = universal(api, ids["aac"], {"PlaySessionId": wav_nonce, "Container": "wav", "TranscodingContainer": "wav",
                           "AudioCodec": "pcm_s16le", "AudioSampleRate": 48000, "AudioChannels": 1})
        headers, wav = http.request("GET", target, "Exact ADTS to WAV conversion")
        progressive_headers(headers, "audio/wav")
        decoded = audio_probe(scratch, wav, "exact-adts.wav", expected_codec="pcm_s16le", channels=1, exact_samples=ADTS_SAMPLES)
        summary["assertions"].append({"stage": stage, **decoded, "all_presentation_samples_preserved": True})

        stage = "6.001-second HLS full timeline hint and every segment"
        hls_nonce = nonce()
        references.append((api, hls_nonce))
        master_target = universal(api, ids["tail"], {"PlaySessionId": hls_nonce, "Container": "mp3", "TranscodingProtocol": "hls",
                                  "TranscodingContainer": "ts", "AudioCodec": "aac", "AllowAudioStreamCopy": "false",
                                  "SegmentLength": 3, "MinSegments": 1, "MaxStreamingBitrate": 128000, "AudioSampleRate": 48000,
                                  "AudioChannels": 2, "StartTimeTicks": 2 * smoke.TICKS})
        headers, master = http.request("GET", master_target, "Short-tail audio HLS master")
        master_lines, main_children = hls.playlist(master)
        check(headers.get("content-type") == "application/vnd.apple.mpegurl" and len(main_children) == 1 and
              not any("RESOLUTION=" in line for line in master_lines), "Audio HLS master did not describe one audio-only variant")
        main_target = hls.resolve(master_target, main_children[0], api.token)
        canonical = db.reference(api, hls_nonce)["play_session_id"]
        check(parse_qs(urlsplit(main_target).query).get("PlaySessionId") == [canonical], "Audio HLS child did not use the owned canonical playback identity")
        _, main = http.request("GET", main_target, "Short-tail audio HLS main")
        lines, children = hls.playlist(main)
        durations = [float(line.split(":", 1)[1].split(",", 1)[0]) for line in lines if line.startswith("#EXTINF:")]
        check(len(children) == 2 and len(durations) == 2 and abs(durations[0] - 3) < 0.000001 and abs(durations[1] - 3.001) < 0.000001 and
              "#EXT-X-MEDIA-SEQUENCE:0" in lines and "#EXT-X-PLAYLIST-TYPE:VOD" in lines and lines[-1] == "#EXT-X-ENDLIST" and
              "#EXT-X-START:TIME-OFFSET=2.0000000,PRECISE=YES" in lines,
              "Audio HLS did not retain the full 6.001-second timeline and merge its short tail")
        targets = []
        for number, child in enumerate(children):
            target = hls.resolve(main_target, child, api.token)
            check(urlsplit(target).path.endswith(f"/{number}.ts"), "Audio HLS segment numbers are not source-global")
            targets.append(target)
            segment_headers, segment = http.request("GET", target, "Advertised audio HLS segment")
            check(segment_headers.get("content-type") == "video/mp2t" and len(segment) >= 188, "Advertised audio HLS segment has no MPEG-TS body")
            audio_probe(scratch, segment, f"audio-segment-{number}.ts", expected_codec="aac")
        phantom = urlsplit(targets[-1])
        http.request("GET", urlunsplit(("", "", phantom.path.rsplit("/", 1)[0] + "/2.ts", phantom.query, "")),
                     "Unadvertised phantom audio HLS segment", expected=(404,))
        local_master = "\n".join(line if line.startswith("#") else smoke.ORIGIN + hls.resolve(master_target, line, api.token)
                                  for line in master_lines) + "\n"
        hls_decode = audio_probe(scratch, local_master.encode(), "audio-master.m3u8", expected_codec="aac", seconds=6.001, hls_input=True)
        summary["assertions"].append({"stage": stage, "segment_durations": durations, "hint_seconds": 2,
                                      "every_advertised_segment": 200, "phantom_segment": 404, "full_graph": hls_decode})

        stage = "nonce stop active encoding deletion and logout"
        api.request("POST", "/emby/Sessions/Playing/Stopped", emby=True, expected=(204,), parse=False,
                    label="Owned client nonce stop", body={"PlaySessionId": full_nonce, "ItemId": ids["flac"], "PositionTicks": 0})
        http.request("GET", full_target, "Stopped client nonce URL", expected=(404,))
        _, surviving = http.request("GET", second_target, "Independent nonce owner after another owner stops")
        check(surviving == second_body, "Stopping one nonce owner changed another owner's completed output")
        cleanup_encoding(api, hls_nonce)
        http.request("GET", targets[0], "Audio HLS child after nonce deletion", expected=(404,))
        cleanup_encoding(api, seek_nonce, "POST")
        second.request("POST", "/emby/Sessions/Logout", emby=True, parse=False, label="Owned second audio session logout")
        second.token = ""
        http.request("GET", second_target, "Audio URL after owner logout", expected=(401,))
        wait_inactive(db, second)
        check(user_data(api, ids) == baseline, "Stop, encoding cleanup, or logout fabricated user playback data")
        summary["assertions"].append({"stage": stage, "stopped_nonce": 404, "independent_owner_preserved": True,
                                      "delete_and_post_cleanup": 204, "deleted_hls_child": 404, "logged_out_url": 401,
                                      "user_data_unchanged": True})
        summary["http_response_bytes"] = http.bytes
        summary["status"] = "passed"
    except Exception as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, smoke.VerificationFailure) else type(error).__name__
    finally:
        if active_scan and api.cookie:
            try:
                api.request("POST", f"/admin/v1/jobs/{quote(active_scan)}/cancel", admin=True, expected=(202,),
                            label="Owned audio scan cancellation")
                api.wait_job(active_scan, timeout=10)
            except Exception:
                summary["cleanup_errors"].append("Owned audio scan cancellation failed")
        for owner, reference in references:
            if owner.token:
                try:
                    cleanup_encoding(owner, reference)
                except Exception:
                    summary["cleanup_errors"].append("Owned audio encoding cleanup failed")
        if api.token and baseline is not None:
            try:
                check(user_data(api, ids) == baseline, "Owned audio user data changed")
                summary["owned_user_data_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("Owned audio user-data preservation check failed")
        for viewer in (second, api):
            if viewer.token:
                try:
                    viewer.request("POST", "/emby/Sessions/Logout", emby=True, parse=False, label="Audio verification viewer logout")
                    viewer.token = ""
                except Exception:
                    summary["cleanup_errors"].append("Owned audio login revocation failed")
            if db is not None and viewer.session_id:
                try:
                    wait_inactive(db, viewer)
                except Exception:
                    summary["cleanup_errors"].append("Owned audio encoding jobs remained active")
        if api.cookie:
            try:
                api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False, label="Audio administrator logout")
            except Exception:
                summary["cleanup_errors"].append("Audio administrator session revocation failed")
        if hashes:
            try:
                check(all(digest_checked(path) == expected for path, expected in hashes.items()), "Reference source bytes changed")
                summary["reference_sources_unchanged"] = True
                if record is not None:
                    check(all(digest_checked(ROOT / name) == expected for name, expected in record["media_hashes"].items()),
                          "Owned audio fixture bytes changed")
                    smoke.private_file(RECORD)
                    summary["owned_fixture_hashes"] = {kind: record["media_hashes"]["Reference Audio M4c." + kind] for kind in SOURCE_FACTS}
                    summary["owned_tail_sha256"] = record["media_hashes"][TAIL_NAME]
                    summary["reference_and_owned_sources_unchanged"] = True
            except Exception:
                summary["cleanup_errors"].append("Audio source preservation check failed")
        try:
            scratch.cleanup()
            summary["private_tmpfs_scratch_removed"] = scratch.path is not None
        except Exception:
            summary["cleanup_errors"].append("Owned audio tmpfs scratch cleanup failed")
        if owned.lock is not None:
            owned.lock.close()
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        try:
            private_json(RESULT, summary, create=not RESULT.exists())
        except Exception:
            summary["status"] = "failed"
            summary["cleanup_errors"].append("Sanitized audio result file could not be saved privately")
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
