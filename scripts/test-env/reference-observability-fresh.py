#!/usr/bin/env python3
"""Capture bounded log and activity contracts on one disposable Emby instance.

Run only through authorized root SSH beside the frozen preparation operator.
The two acknowledged ordinary credentials are handed off without another login.
Only one named, unlogged-in user and one uniquely identified application key
may be created. All log bytes remain private; sanitized exports are generated
after credential retirement and verified against the retained response bytes.
Explicit operator cleanup must always follow, including after a failed capture.
"""

from __future__ import annotations

import base64
import binascii
import copy
import datetime as dt
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import signal
import stat
import sys
import time
from urllib.parse import parse_qs, quote, unquote, urlencode, urlsplit

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m5i")
EVIDENCE = WORK / "emby-observability-fresh-m5i-20260910-01"
DATA = WORK / "emby-observability-fresh-data-01"
ROOT = EVIDENCE / "runtime/observability-capture"
PRIVATE, RAW, EXPORT = ROOT / "private", ROOT / "private/raw", ROOT / "export"
WIRE, MUTATIONS = PRIVATE / "wire", PRIVATE / "mutations"
UNIT = "goby-emby-observability-fresh-m5i-20260910-01.service"
MARKER = "goby-emby-observability-fresh-m5i-20260910-01-owned-v1"
CAPTURE_MARKER = "goby-reference-observability-fresh-m5i-owned-v1"
PREFIX = "observability-fresh-m5i-"
PORT, HTTPS_PORT, OLD_RECORDS = 18101, 18501, 2366
MANIFEST = EVIDENCE / "private/manifest.json"
OPERATOR_SOURCE = Path(__file__).with_name("prepare-observability-fresh.py")
OPERATOR_SOURCE_SHA256 = "a3f8824ce46ad99b8651e31dd088251f145e2cad6f32d4f861d70bc62b73d55c"
MIB = 1024 * 1024
MAX_BODY, MAX_LOG_BODY, MAX_REQUEST_BODY = MIB, 2 * MIB, 4096
MAX_JSON_TOTAL, MAIN_JSON_TOTAL, MAX_LOG_TOTAL = 16 * MIB, 8 * MIB, 16 * MIB
MAIN_REQUESTS, MAX_REQUESTS = 106, 118
MAIN_SECONDS, CLEANUP_SECONDS, HTTP_SECONDS = 240, 90, 8
CREDENTIAL_PHASE_SECONDS, INITIALIZATION_PHASE_SECONDS = 30, 40
OPERATOR_SETUP_REQUESTS, OPERATOR_CLEANUP_REQUESTS, ALL_HTTP_REQUESTS = 24, 8, 150
ACTIVITY_ROUTE = "/emby/System/ActivityLog/Entries"
LOGS_ROUTE = "/emby/System/Logs/Query"
LOG_PREFIX = "/emby/System/Logs/"
KEYS_ROUTE = "/emby/Auth/Keys"
KEYS_PAGE = KEYS_ROUTE + "?StartIndex=0&Limit=5"
SESSIONS_ROUTE, LOGOUT_ROUTE = "/emby/Sessions", "/emby/Sessions/Logout"
USERS_ROUTE, NEW_USER_ROUTE = "/emby/Users", "/emby/Users/New"
LIBRARIES_ROUTE = "/emby/Library/VirtualFolders/Query"
PUBLIC_ROUTE = "/emby/System/Info/Public"
APP_NAME = "Goby Observability Fresh M5i 20260910 01 Owned Read Credential"
CREATED_USER_NAME = "reference-observability-cause-no-login-m5i-01"
UNKNOWN_LOG_NAME = "goby-observability-m5i-01-not-present.log"
ACCOUNTS = {
    "admin": {"username": "reference-observability-fresh-admin-m5i-01",
              "client": "Goby Observability Fresh M5i 01 Admin",
              "deviceId": "goby-observability-fresh-m5i-20260910-01-admin"},
    "viewer": {"username": "reference-observability-fresh-viewer-m5i-01",
               "client": "Goby Observability Fresh M5i 01 Viewer",
               "deviceId": "goby-observability-fresh-m5i-20260910-01-viewer"},
}
SECRET_NAMES = {"accesstoken", "token", "apikey", "key", "password", "pw", "secret", "authorization",
                "proxyauthorization", "xembyauthorization", "xembytoken", "xmediabrowsertoken", "cookie", "setcookie"}
INTERNAL_ROOTS = (str(EVIDENCE), str(DATA), str(ROOT), str(WORK), "/opt/goby-test", "/opt/goby-fixtures",
                  "/opt/goby-toolchains", "/dev/shm/goby-emby-reference", "/root", "/home", "/tmp", "/var/tmp")
MISSING = object()
_operator = None


def require(condition: object, message: str) -> None:
    if not condition:
        raise RuntimeError(message)


def utc() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def digest_bytes(content: bytes) -> str:
    return hashlib.sha256(content).hexdigest()


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(65536), b""):
            result.update(block)
    return result.hexdigest()


def canonical(path: Path, *, directory: bool = False) -> None:
    require(path.is_absolute() and path.resolve(strict=True) == path, "An evidence path is not canonical")
    info = path.lstat()
    require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)) and info.st_uid == 0 and
            stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600), "Private ownership or mode differs")
    require(directory or info.st_nlink == 1, "A private evidence file has unexpected hard links")


def json_bytes(value: object) -> bytes:
    return (json.dumps(value, indent=2, ensure_ascii=False, allow_nan=False) + "\n").encode("utf-8")


def save_bytes(path: Path, content: bytes) -> None:
    require(path.is_absolute() and ROOT in path.parents and not path.is_symlink(), "Evidence output is outside the capture root")
    canonical(path.parent, directory=True)
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stream:
        stream.write(content)
        stream.flush()
        os.fsync(stream.fileno())


def save(path: Path, value: object) -> None:
    save_bytes(path, json_bytes(value))


def load(path: Path) -> object:
    canonical(path)
    require(path.stat().st_size <= 64 * MIB, "A private JSON input exceeds its bound")
    return json.loads(path.read_text(encoding="utf-8"))


def same_value(left: object, right: object) -> bool:
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(same_value(left[key], right[key]) for key in left)
    if isinstance(left, list):
        return len(left) == len(right) and all(same_value(a, b) for a, b in zip(left, right))
    return left == right


def operator_module():
    global _operator
    require(re.fullmatch(r"[0-9a-f]{64}", OPERATOR_SOURCE_SHA256) is not None and not OPERATOR_SOURCE.is_symlink() and
            digest(OPERATOR_SOURCE) == OPERATOR_SOURCE_SHA256, "The observability operator is not the frozen reviewed source")
    if _operator is None:
        specification = importlib.util.spec_from_file_location("observability_operator_authority", OPERATOR_SOURCE)
        _operator = importlib.util.module_from_spec(specification)
        specification.loader.exec_module(_operator)
    require(_operator.ROOT == EVIDENCE and _operator.DATA == DATA and _operator.UNIT == UNIT and
            _operator.PORT == PORT and _operator.HTTPS_PORT == HTTPS_PORT and _operator.MARKER == MARKER,
            "The observability operator constants differ")
    require(_operator.OLD_UNITS == {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3641418},
            "Protected service identities differ")
    return _operator


def load_manifest() -> dict:
    op = operator_module()
    manifest = op.load(MANIFEST)
    expected = {"state": "READY", "marker": MARKER, "unit": UNIT, "port": PORT, "httpsPort": HTTPS_PORT,
                "programData": str(DATA), "evidenceRoot": str(EVIDENCE), "operatorSha256": OPERATOR_SOURCE_SHA256,
                "credentialState": "LIVE_HANDOFF", "ordinaryLoginCount": 2}
    require(isinstance(manifest, dict) and all(manifest.get(key) == value for key, value in expected.items()),
            "The observability READY manifest differs")
    require(isinstance(manifest.get("serverId"), str) and manifest["serverId"] and manifest["serverId"] != op.OLD_SERVER_ID and
            set(manifest.get("credentials", {})) == set(ACCOUNTS), "The handoff is not the separate two-account fixture")
    require(len(manifest.get("oldBaseline", {}).get("records", {})) == OLD_RECORDS * 2 and
            len(manifest["oldBaseline"].get("media", {})) == 240, "The protected evidence corpus membership differs")
    require(type(manifest.get("setupRecordCount")) is int and 0 < manifest["setupRecordCount"] <= OPERATOR_SETUP_REQUESTS,
            "The setup HTTP allocation differs")
    require(manifest.get("bootstrapLibraries") == [], "The disposable reference has a library")
    require(manifest.get("oldPreservationVerified") is True and manifest.get("allFreshProgramDataOwned") is True and
            manifest.get("bootstrapConfigurationSafetyVerified") is True and manifest.get("viewerEmptyPasswordLoginVerified") is True and
            all(manifest.get(name) == 0 for name in ("bootstrapLibraryMutationRequests", "bootstrapConfigurationMutationRequests",
                "bootstrapNamedConfigurationRequests", "bootstrapTaskRequests", "bootstrapTaskMutationRequests",
                "bootstrapApplicationKeyRequests", "bootstrapMediaRequests")), "Fresh setup safety or ownership declarations differ")
    return manifest


def preconditions() -> dict:
    require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
            "Run only through authorized root SSH")
    op, manifest = operator_module(), load_manifest()
    op.common_preconditions(host=False)
    identity = op.load(op.IDENTITY)
    op.same_new_identity(identity)
    require(all(manifest.get(key) == value for key, value in identity.items()), "READY and the process attestation differ")
    op.check_old_services(manifest["oldServices"])
    op.verify_baseline(manifest["oldBaseline"])
    require(identity["networkNamespace"] not in {row["networkNamespace"] for row in manifest["oldServices"].values()} and
            identity["networkNamespace"] != os.readlink("/proc/1/ns/net"), "The fresh namespace overlaps a protected authority")
    if os.readlink("/proc/self/ns/net") != identity["networkNamespace"]:
        os.execvp("nsenter", ["nsenter", "-t", str(identity["pid"]), "-n", sys.executable, "-B", str(Path(__file__).resolve())])
    require(os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "The recorder is outside the fresh namespace")
    return manifest


def encoded_pattern(value: str) -> str:
    """Match literal and bounded mixed percent/JSON/hex byte spellings."""
    pieces = []
    for char in value:
        encoded = char.encode("utf-8")
        choices = [re.escape(char)]
        if len(encoded) == 1:
            number = encoded[0]
            choices.extend((f"%{number:02x}", f"%25{number:02x}", rf"\\u{number:04x}", rf"\\x{number:02x}"))
        if char == " ":
            choices.append(r"\+")
        if char == "/":
            choices.append(r"\\/")
        pieces.append("(?:" + "|".join(choices) + ")")
    return "".join(pieces)


def sensitive_field(name: str) -> bool:
    normalized = re.sub(r"[^a-z0-9]", "", name.lower())
    return normalized in SECRET_NAMES or "password" in normalized or normalized.endswith("token") or "apikey" in normalized


def discovered_secret_candidate(value: str) -> bool:
    """Do not turn unknown low-entropy metadata into global replacements."""
    return (len(value) >= 24 and len(set(value)) >= 8 and re.fullmatch(r"[A-Za-z0-9._~+/=-]+", value) is not None and
            any(char.isalpha() for char in value) and any(char.isdigit() for char in value))


def restore_alarm(previous: tuple[float, float], started: float) -> None:
    remaining = previous[0] - (time.monotonic() - started)
    # An expired outer alarm must remain pending while callers classify and
    # preserve a response. Clearing it would silently remove the phase bound.
    signal.setitimer(signal.ITIMER_REAL, max(0.001, remaining) if previous[0] > 0 else 0, previous[1])


def parse_body(content: bytes) -> tuple[object, str]:
    try:
        text = content.decode("utf-8", errors="strict")
    except UnicodeDecodeError:
        return None, "private-binary"
    try:
        return json.loads(text), "json"
    except (ValueError, RecursionError):
        return text, "text"


def date_tick_variants(value: str) -> dict[str, str]:
    """Retain a received timestamp and compare its exact 100 ns neighbors."""
    match = re.fullmatch(r"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,7}))?(Z|[+-]\d{2}:\d{2})", value)
    require(match is not None, "The received activity date is outside the bounded ISO timestamp grammar")
    fraction = int((match[2] or "").ljust(7, "0"))
    seconds = dt.datetime.fromisoformat(match[1] + ("+00:00" if match[3] == "Z" else match[3]))
    epoch = dt.datetime(1970, 1, 1, tzinfo=dt.timezone.utc)
    delta = seconds.astimezone(dt.timezone.utc) - epoch
    ticks = (delta.days * 86400 + delta.seconds) * 10000000 + fraction

    def render(amount: int, offset_hours: int = 0) -> str:
        whole, remainder = divmod(amount, 10000000)
        instant = epoch + dt.timedelta(seconds=whole, hours=offset_hours)
        suffix = "Z" if offset_hours == 0 else f"+{offset_hours:02d}:00"
        return instant.strftime("%Y-%m-%dT%H:%M:%S") + f".{remainder:07d}" + suffix

    return {"exact": value, "before-tick": render(ticks - 1), "after-tick": render(ticks + 1),
            "equivalent-offset": render(ticks, 8)}


class Recorder:
    def __init__(self, manifest: dict) -> None:
        self.manifest, self.authority = manifest, operator_module().load(operator_module().IDENTITY)
        self.started, self.finishing, self.initialized = time.monotonic(), False, False
        self.deadline = self.started + MAIN_SECONDS
        self.cleanup_deadline, self.cleanup_phases = None, []
        self.secrets, self.credentials, self.logins = set(), {}, {}
        self.secret_regex, self.secret_signature, self.secret_byte_sequences = None, None, ()
        self.invalid_token = secrets.token_hex(32)
        self.secrets.add(self.invalid_token)
        self.labels, self.records, self.mutations = set(), {}, []
        self.record_count = self.json_bytes = self.log_bytes = self.incomplete_count = 0
        self.charged_json = self.charged_logs = 0
        self.persistence_failures, self.cleanup_errors, self.observations = [], [], []
        self.capture_failure, self.cleanup_ok, self.evidence_unavailable = None, False, False
        self.key_create_attempted, self.key_revoke_attempted, self.key_ownership_failed = False, False, False
        self.key_baseline_empty, self.owned_key, self.key_invalidity, self.key_revoke_status = False, None, None, None
        self.user_create_attempted, self.created_user, self.user_cause_proved = False, None, False
        self.ordinary_logout_attempted, self.logout_statuses, self.invalidity = set(), {}, {}
        self.allowed_log_names, self.selected_log_name, self.baselines = set(), None, {}
        self.allowed_dates, self.writes_blocked = set(), False
        self.load_handoff()
        require(not ROOT.exists() and not ROOT.is_symlink(), "Refusing to overwrite a previous observability capture")
        self.setup_baseline = self.snapshot_setup()
        os.umask(0o077)
        canonical(EVIDENCE / "runtime", directory=True)
        ROOT.mkdir(mode=0o700)
        for folder in (PRIVATE, RAW, EXPORT, WIRE, MUTATIONS):
            folder.mkdir(mode=0o700)
        save_bytes(ROOT / ".goby-managed", (CAPTURE_MARKER + "\n").encode())
        save(PRIVATE / "baseline.json", self.setup_baseline)
        self.initialized = True

    def load_handoff(self) -> None:
        for identity, fixed in ACCOUNTS.items():
            row = self.manifest["credentials"][identity]
            expected = {"username": fixed["username"], "deviceId": fixed["deviceId"], "client": fixed["client"],
                        "credentialsFile": str(EVIDENCE / "private" / (identity + "-credentials.env")),
                        "loginResponseFile": str(EVIDENCE / "private" / (identity + "-login-response.json"))}
            require(all(row.get(key) == value for key, value in expected.items()), "An ordinary handoff identity differs")
            credential_path, login_path = Path(row["credentialsFile"]), Path(row["loginResponseFile"])
            canonical(credential_path)
            canonical(login_path)
            require(digest(credential_path) == row["credentialsSha256"] and digest(login_path) == row["loginResponseSha256"],
                    "Handoff credential bytes changed after READY")
            lines = credential_path.read_text(encoding="utf-8").splitlines()
            require(all("=" in line for line in lines), "A credential handoff line is malformed")
            values = dict(line.split("=", 1) for line in lines)
            require(len(lines) == len(values) == 5 and set(values) == {"REFERENCE_USERNAME", "REFERENCE_PASSWORD", "REFERENCE_TOKEN",
                    "REFERENCE_USER_ID", "REFERENCE_DEVICE_ID"}, "Credential handoff fields differ")
            token = values["REFERENCE_TOKEN"]
            response = load(login_path)
            require(token and digest_bytes(token.encode()) == row["tokenSha256"] and values["REFERENCE_USERNAME"] == row["username"] and
                    values["REFERENCE_USER_ID"] == row["userId"] and values["REFERENCE_DEVICE_ID"] == row["deviceId"] and
                    isinstance(response, dict) and response.get("AccessToken") == token and response.get("User", {}).get("Id") == row["userId"] and
                    response.get("User", {}).get("Name") == row["username"] and
                    response.get("User", {}).get("Policy", {}).get("IsAdministrator") is (identity == "admin") and
                    response.get("ServerId") == self.manifest["serverId"] and
                    response.get("SessionInfo", {}).get("DeviceId") == row["deviceId"] and
                    response.get("SessionInfo", {}).get("UserId") == row["userId"] and
                    response.get("SessionInfo", {}).get("Client") == row["client"] and
                    response.get("SessionInfo", {}).get("Id") == row.get("sessionId") and
                    isinstance(row.get("internalDeviceId"), str) and bool(row["internalDeviceId"]) and
                    str(response.get("SessionInfo", {}).get("InternalDeviceId")) == row["internalDeviceId"] and
                    row.get("administrator") is (identity == "admin") and row.get("credentialState") == "LIVE_HANDOFF",
                    "An acknowledged ordinary login differs")
            require(token != self.invalid_token and token not in {value["REFERENCE_TOKEN"] for value in self.credentials.values()},
                    "Fresh ordinary credentials are not distinct")
            # Publish a validated credential in memory before any later input or
            # evidence operation can fail. Initialization cleanup can retire it.
            self.credentials[identity], self.logins[identity] = values, response
            self.secrets.update(values[key] for key in ("REFERENCE_TOKEN", "REFERENCE_PASSWORD") if values[key])
        require(len({value["REFERENCE_USER_ID"] for value in self.credentials.values()}) == 2,
                "The two ordinary handoffs do not belong to distinct fresh users")
        administrator = self.manifest["credentials"]["admin"]
        ownership = {"serverId": self.manifest["serverId"], "userId": administrator["userId"], "deviceId": administrator["deviceId"],
                     "internalDeviceId": administrator["internalDeviceId"], "sessionId": administrator["sessionId"],
                     "administrator": True, "tokenSha256": administrator["tokenSha256"]}
        require(self.manifest.get("freshAdministratorOwnership") == ownership,
                "The handoff administrator ownership attestation differs")
        proofs = self.manifest.get("ordinaryProtectedAccessProofs", {})
        require(set(proofs) == set(ACCOUNTS), "Ordinary protected-access proof membership differs")
        for identity in ACCOUNTS:
            proof, metadata = proofs[identity], self.manifest["credentials"][identity]
            capture = Path(proof.get("capture", ""))
            require(proof.get("status") == 200 and metadata.get("liveProbeStatus") == 200 and
                    capture.parent == EVIDENCE / "private/raw" and capture.name.startswith(operator_module().PREFIX) and capture.suffix == ".json",
                    "An ordinary protected-access proof is not a fresh operator capture")
            record = load(capture)
            request, response = record.get("request", {}), record.get("response", {})
            headers = request.get("headers", [])
            tokens = [value for name, value in headers if name.lower() == "x-emby-token"]
            require(request.get("method") == "GET" and request.get("path") == SESSIONS_ROUTE and
                    tokens == [self.token(identity)] and response.get("status") == 200 and record.get("observation", {}).get("completeHTTP") is True and
                    isinstance(response.get("body"), list) and any(isinstance(row, dict) and row.get("DeviceId") == metadata["deviceId"] and
                    row.get("UserId") == metadata["userId"] for row in response["body"]),
                    "An ordinary live-access proof does not match its acknowledged user credential")

    def snapshot_setup(self) -> dict:
        operator_module().verify_baseline(self.manifest["oldBaseline"])
        raw = sorted((EVIDENCE / "private/raw").glob("*.json"))
        exported = sorted((EVIDENCE / "export").glob("*.json"))
        require(len(raw) == len(exported) == self.manifest["setupRecordCount"] and
                {path.name for path in raw} == {path.name for path in exported}, "Fresh setup record membership differs")
        files = []
        for path in (EVIDENCE / "private").rglob("*"):
            require(not path.is_symlink(), "Setup evidence contains a symbolic link")
            if path.is_file():
                files.append(path)
        files.extend(exported)
        require(len(files) <= 1024 and sum(path.stat().st_size for path in files) <= 64 * MIB,
                "Fresh setup evidence exceeds its bound")
        for path in files:
            canonical(path)
        return {"files": {str(path): digest(path) for path in files}, "setupRecordCount": len(raw),
                "protectedRecords": OLD_RECORDS, "protectedMediaFiles": 240}

    def verify_preservation(self) -> None:
        op = operator_module()
        op.check_old_services(self.manifest["oldServices"])
        op.verify_baseline(self.manifest["oldBaseline"])
        require(self.snapshot_setup() == self.setup_baseline, "Fresh setup or protected evidence changed during capture")
        self.check_authority()

    def check_authority(self) -> None:
        operator_module().same_new_identity(self.authority)
        require(os.readlink("/proc/self/ns/net") == self.authority["networkNamespace"] and
                self.authority["networkNamespace"] not in {row["networkNamespace"] for row in self.manifest["oldServices"].values()},
                "A request attempted to leave the attested fresh namespace")

    def token(self, principal: str) -> str:
        if principal == "anonymous":
            return ""
        if principal == "invalid":
            return self.invalid_token
        if principal in self.credentials:
            return self.credentials[principal]["REFERENCE_TOKEN"]
        require(principal == "application" and self.owned_key is not None and not self.key_ownership_failed,
                "The application credential is not uniquely acknowledged")
        return self.owned_key["AccessToken"]

    def collect_secrets(self, value: object, name: str = "") -> None:
        if isinstance(value, dict):
            for key, child in value.items():
                require(isinstance(key, str), "A JSON object key is not a string")
                self.collect_secrets(child, key)
        elif isinstance(value, (list, tuple)):
            if name.lower() == "headers":
                for header, content in value:
                    self.collect_secrets(content, header)
            else:
                for child in value:
                    self.collect_secrets(child, name)
        elif isinstance(value, str) and value:
            if sensitive_field(name) and discovered_secret_candidate(value):
                self.secrets.add(value)
            for match in re.finditer(r"(?i)\b[a-z][a-z0-9+.-]{1,15}://([^/\s\"'<>]*)@", value):
                userinfo = match[1]
                for part in userinfo.split(":", 1):
                    decoded = unquote(part)
                    if discovered_secret_candidate(decoded):
                        self.secrets.add(decoded)

    def clean_text(self, value: str, depth: int = 0) -> str:
        require(len(value.encode("utf-8")) <= 8 * MIB, "A text field exceeds the sanitizer bound")
        signature = tuple(sorted(self.secrets))
        if signature != self.secret_signature:
            require(len(signature) <= 256 and sum(len(item) for item in signature) <= 65536,
                    "The known credential set exceeds its bound")
            choices = [pattern for item in sorted(signature, key=len, reverse=True) if item
                       for pattern in (encoded_pattern(item), re.escape(item.encode("utf-8").hex()))]
            self.secret_regex = re.compile("|".join(choices), re.IGNORECASE) if choices else None
            self.secret_byte_sequences = tuple({item.encode(encoding) for item in (*signature, *INTERNAL_ROOTS)
                                                for encoding in ("utf-8", "utf-16-le", "utf-16-be")})
            self.secret_signature = signature
        if self.secret_regex is not None:
            value = self.secret_regex.sub("[REDACTED_SECRET]", value)
        for root in INTERNAL_ROOTS:
            value = re.sub(encoded_pattern(root), "[REDACTED_PATH]", value, flags=re.IGNORECASE)
            value = re.sub(re.escape(root.encode("utf-8").hex()), "[REDACTED_PATH]", value, flags=re.IGNORECASE)
        value = re.sub(r"(?i)(?<![A-Za-z0-9])/(?:opt|dev|proc|sys|etc|usr|var|run|tmp|home|root|mnt|media|srv|data|config|cache)(?:/[^\s\"'<>]*)?",
                       "[REDACTED_PATH]", value)
        value = re.sub(r"(?i)\b[A-Z]:[\\/][^\s\"'<>]*", "[REDACTED_PATH]", value)
        value = re.sub(r"(?i)(\b[a-z][a-z0-9+.-]{1,15}://)[^/\s\"'<>]*@", r"\1[REDACTED_USERINFO]@", value)
        value = re.sub(r"(?i)(/Auth/Keys/)[^/?\s\"'<>]+", r"\1[REDACTED_KEY]", value)
        value = re.sub(r"(?i)((?:api[_-]?key|access[_-]?token|password|\bpw|\btoken|authorization|x-emby-token)[\"']?\s*[:=]\s*)"
                       r"(?:\"[^\"]*\"|'[^']*'|[^\s&,;<>]+)", r"\1[REDACTED_SECRET]", value)

        # Inspect whole encoded text as well as individually encoded secrets.
        # This covers encoded paths and credentials embedded in larger blobs.
        # Beyond the declared depth, hide the field rather than call it safe.
        probe = unquote(value)
        probe = re.sub(r"\\u([0-9a-fA-F]{4})", lambda match: chr(int(match[1], 16)), probe)
        probe = re.sub(r"\\x([0-9a-fA-F]{2})", lambda match: chr(int(match[1], 16)), probe)
        probe = probe.replace(r"\/", "/")
        if probe != value:
            if depth >= 3 or any(0xD800 <= ord(char) <= 0xDFFF for char in probe) or self.clean_text(probe, depth + 1) != probe:
                return "[REDACTED_ENCODED_TEXT]"

        def inspect_blob(match: re.Match) -> str:
            candidate = match[0]
            if len(candidate) > 4 * MIB:
                return "[REDACTED_ENCODED_TEXT]"
            try:
                content = base64.b64decode(candidate.replace("-", "+").replace("_", "/") + "=" * (-len(candidate) % 4), validate=True)
            except (ValueError, binascii.Error):
                return candidate
            if any(item in content for item in self.secret_byte_sequences):
                return "[REDACTED_ENCODED_TEXT]"
            decoded = content.decode("utf-8", errors="replace")
            if not decoded:
                return candidate
            if depth >= 3:
                return "[REDACTED_ENCODED_TEXT]"
            return "[REDACTED_ENCODED_TEXT]" if self.clean_text(decoded, depth + 1) != decoded else candidate

        return re.sub(r"(?<![A-Za-z0-9+/_-])[A-Za-z0-9+/_-]{4,}={0,2}(?![A-Za-z0-9+/_=-])", inspect_blob, value)

    def sanitize(self, value: object, name: str = "") -> object:
        if sensitive_field(name) and value is not None and value != "" and type(value) is not bool:
            return "[REDACTED_SECRET]"
        if isinstance(value, dict):
            result = {}
            for key, child in value.items():
                cleaned = self.clean_text(key)
                require(cleaned not in result, "Sanitized dictionary keys collide")
                result[cleaned] = self.sanitize(child, key)
            return result
        if isinstance(value, (list, tuple)):
            if name.lower() == "headers":
                return [[self.clean_text(header), "[REDACTED_SECRET]" if content and sensitive_field(header)
                         else self.sanitize(content, header)] for header, content in value]
            return [self.sanitize(child, name) for child in value]
        if isinstance(value, str):
            return "[REDACTED_SECRET]" if value and sensitive_field(name) else self.clean_text(value)
        return value

    def persist(self, label: str, operation, *, cleanup: bool = False) -> object:
        try:
            require(not self.evidence_unavailable, "Capture initialization did not complete")
            require(not cleanup or time.monotonic() < self.deadline, "The cleanup persistence phase deadline expired")
            return operation()
        except Exception as error:
            failure = {"stage": label, "error": type(error).__name__}
            self.persistence_failures.append(failure)
            if not cleanup:
                raise
            self.cleanup_errors.append(failure)
            self.cleanup_ok = False
            return None

    def cleanup_step(self, label: str, action) -> object:
        try:
            require(time.monotonic() < self.deadline, "The cleanup action phase deadline expired")
            return action()
        except Exception as error:
            self.cleanup_errors.append({"stage": label, "error": type(error).__name__})
            self.cleanup_ok = False
            return None

    def cleanup_phase(self, label: str, action, seconds: float) -> object:
        """Bound authority checks, HTTP, acknowledgement and persistence together."""
        started, before = time.monotonic(), len(self.cleanup_errors)
        require(self.finishing and self.cleanup_deadline is not None and 0 < seconds <= CLEANUP_SECONDS,
                "A cleanup phase has no finite owned allocation")
        self.deadline = min(self.cleanup_deadline, started + seconds)
        previous = signal.getitimer(signal.ITIMER_REAL)
        remaining = self.deadline - started
        if previous[0] > 0:
            remaining = min(remaining, previous[0])
            self.deadline = min(self.deadline, started + previous[0])
        result = None
        try:
            if remaining <= 0:
                self.cleanup_errors.append({"stage": label, "error": "CleanupPhaseBudgetExhausted"})
                self.cleanup_ok = False
            else:
                signal.setitimer(signal.ITIMER_REAL, remaining)
                def bounded_action():
                    value = action()
                    require(time.monotonic() <= self.deadline, "A cleanup action exceeded its allocated phase")
                    return value
                result = self.cleanup_step(label, bounded_action)
        finally:
            restore_alarm(previous, started)
            self.cleanup_phases.append({"stage": label, "allocationSeconds": seconds,
                "elapsedSeconds": round(time.monotonic() - started, 6), "completedWithoutErrors": len(self.cleanup_errors) == before})
        return result

    def authorize(self, method: str, path: str, principal: str, body: object) -> str:
        parsed = urlsplit(path)
        require(not parsed.scheme and not parsed.netloc and not parsed.fragment and path.startswith("/emby/") and
                "\\" not in path and len(path.encode()) <= 2048 and not any(ord(char) < 32 for char in path), "Unexpected fresh API path")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        require(all(len(values) == 1 for values in query.values()), "Duplicate query fields are prohibited")
        route, allowed, bucket = parsed.path, False, "json"
        require(principal in {"anonymous", "invalid", "admin", "viewer", "application"}, "Unknown principal")
        self.token(principal)
        if method == "GET" and body is MISSING:
            if route == ACTIVITY_ROUTE:
                allowed = set(query) <= {"StartIndex", "Limit", "MinDate"} and all(
                    (key == "MinDate" and values[0] in self.allowed_dates) or
                    (key != "MinDate" and values[0] in {"0", "1", "2", "5", "50", "-1", "2147483647", "invalid"})
                    for key, values in query.items())
            elif route == LOGS_ROUTE:
                allowed = set(query) <= {"StartIndex", "Limit"} and all(values[0] in {"0", "1", "2", "5", "-1", "2147483647", "invalid"}
                                                                               for values in query.values())
            elif route.startswith(LOG_PREFIX):
                is_lines = route.endswith("/Lines")
                encoded_name = route[len(LOG_PREFIX):-len("/Lines")] if is_lines else route[len(LOG_PREFIX):]
                names = self.allowed_log_names | {UNKNOWN_LOG_NAME}
                allowed = encoded_name in {quote(name, safe="") for name in names}
                if is_lines:
                    allowed = allowed and set(query) <= {"StartIndex", "Limit", "StartPosition", "SearchTerm"} and all(
                        values[0] in {"0", "1", "2147483647", "goby-observability-m5i-unmatched-text"} for values in query.values())
                else:
                    bucket = "log"
                    allowed = allowed and set(query) <= {"Sanitize"} and all(values[0] in {"false", "true", "invalid"} for values in query.values())
            elif path in {PUBLIC_ROUTE, USERS_ROUTE, LIBRARIES_ROUTE, KEYS_PAGE}:
                allowed = principal == "admin"
            elif path == SESSIONS_ROUTE:
                allowed = self.finishing and principal in {"admin", "viewer", "application"}
            if principal == "application":
                allowed = allowed and (route in {ACTIVITY_ROUTE, LOGS_ROUTE} or
                    route in {LOG_PREFIX + quote(self.selected_log_name or "", safe=""),
                              LOG_PREFIX + quote(self.selected_log_name or "", safe="") + "/Lines"} or
                    self.finishing and path == SESSIONS_ROUTE) and not query
        elif method == "HEAD" and body is MISSING:
            allowed = (principal in {"admin", "anonymous"} and not query and self.selected_log_name is not None and
                       route == LOG_PREFIX + quote(self.selected_log_name, safe=""))
            bucket = "log"
        elif method == "POST" and path == NEW_USER_ROUTE:
            allowed = (principal == "admin" and not self.finishing and not self.writes_blocked and not self.user_create_attempted and
                       "activity-before-user" in self.baselines and body == {"Name": CREATED_USER_NAME})
        elif method == "POST" and route == KEYS_ROUTE:
            allowed = (principal == "admin" and not self.finishing and not self.writes_blocked and self.key_baseline_empty and
                       not self.key_create_attempted and self.owned_key is None and query == {"App": [APP_NAME]} and body is MISSING)
        elif method == "DELETE" and route.startswith(KEYS_ROUTE + "/"):
            allowed = (principal == "admin" and self.finishing and not self.key_revoke_attempted and not query and body is MISSING and
                       self.owned_key is not None and not self.key_ownership_failed and
                       route == KEYS_ROUTE + "/" + quote(self.owned_key["AccessToken"], safe=""))
        elif method == "POST" and path == LOGOUT_ROUTE:
            allowed = self.finishing and principal in self.credentials and principal not in self.ordinary_logout_attempted and body is MISSING
        require(allowed, "Request is outside the bounded observability allowlist")
        return bucket

    def acknowledge(self, method: str, path: str, principal: str, status: int | None, result: object, complete: bool, label: str) -> None:
        if path == KEYS_PAGE and self.key_create_attempted and self.owned_key is None and not self.key_ownership_failed and status == 200 and complete:
            rows = result.get("Items") if isinstance(result, dict) else None
            if isinstance(rows, list) and len(rows) == 1 and type(result.get("TotalRecordCount")) is int and result["TotalRecordCount"] == 1:
                row = rows[0]
                token = row.get("AccessToken") if isinstance(row, dict) else None
                if isinstance(row, dict) and row.get("AppName") == APP_NAME and isinstance(token, str) and token and token not in {
                        self.invalid_token, *(values["REFERENCE_TOKEN"] for values in self.credentials.values())}:
                    self.owned_key = copy.deepcopy(row)
                    self.secrets.add(token)
                else:
                    self.key_ownership_failed = True
            else:
                self.key_ownership_failed = True
        if path == NEW_USER_ROUTE and method == "POST" and complete and status in {200, 201} and isinstance(result, dict):
            if (result.get("Name") == CREATED_USER_NAME and isinstance(result.get("Id"), str) and result["Id"] and
                    result["Id"] not in {value["REFERENCE_USER_ID"] for value in self.credentials.values()}):
                self.created_user = copy.deepcopy(result)
        if path == SESSIONS_ROUTE and self.finishing and complete:
            if principal == "application":
                self.key_invalidity = {"label": label, "status": status}
            else:
                self.invalidity[principal] = {"label": label, "status": status}
        if path == LOGOUT_ROUTE:
            self.logout_statuses[principal] = status
        if method == "DELETE" and path.startswith(KEYS_ROUTE + "/"):
            self.key_revoke_status = status

    def request(self, label: str, method: str, path: str, *, principal: str = "admin", body: object = MISSING) -> tuple[int | None, object]:
        require(time.monotonic() < self.deadline, "The HTTP phase deadline expired before authority verification")
        self.check_authority()
        bucket = self.authorize(method, path, principal, body)
        require(re.fullmatch(r"[a-z0-9-]+", label) and label not in self.labels, "HTTP label is unsafe or already consumed")
        retiring = self.finishing and path in {LOGOUT_ROUTE, SESSIONS_ROUTE} and principal in self.credentials
        request_limit = MAX_REQUESTS if retiring else MAX_REQUESTS - 4 if self.finishing else MAIN_REQUESTS
        byte_limit = MAX_JSON_TOTAL if retiring else MAX_JSON_TOTAL - 4 * MAX_BODY if self.finishing else MAIN_JSON_TOTAL
        remaining = self.deadline - time.monotonic()
        limit = min(MAX_LOG_BODY, MAX_LOG_TOTAL - self.charged_logs) if bucket == "log" else min(MAX_BODY, byte_limit - self.charged_json)
        require(self.record_count < request_limit and remaining > 0 and limit > 0, "HTTP time, count or byte budget exhausted")
        headers = {"Accept": "text/plain, application/octet-stream" if bucket == "log" else "application/json"}
        token = self.token(principal)
        if token:
            headers["X-Emby-Token"] = token
        if principal in ACCOUNTS:
            identity = ACCOUNTS[principal]
            headers["Authorization"] = ('Emby Client="' + identity["client"] + '", DeviceId="' + identity["deviceId"] +
                                        '", Device="Linux Fresh Test", Version="0.1.0"')
        request_body = b"" if body is MISSING else json.dumps(body, separators=(",", ":"), allow_nan=False).encode()
        require(len(request_body) <= MAX_REQUEST_BODY, "Request body exceeds its bound")
        if body is not MISSING:
            headers["Content-Type"] = "application/json"
        kind = {NEW_USER_ROUTE: "owned-user-create", LOGOUT_ROUTE: "ordinary-logout"}.get(path)
        if method == "POST" and urlsplit(path).path == KEYS_ROUTE:
            kind = "owned-application-key-create"
        if method == "DELETE":
            kind = "owned-application-key-revoke"
        request = {"method": method, "path": path, "headers": headers, "bodyPresent": body is not MISSING,
                   "body": None if body is MISSING else body, "principal": principal, "approvedMutationKind": kind}
        mutation = None
        if method not in {"GET", "HEAD"}:
            mutation = {"label": label, "request": copy.deepcopy(request), "status": None, "dispatched": False}
            self.mutations.append(mutation)
            self.persist("mutation-intent-" + label, lambda: save(MUTATIONS / (label + ".json"), mutation), cleanup=self.finishing)
        remaining = self.deadline - time.monotonic()
        require(remaining > 0, "The HTTP phase deadline expired during intent persistence")
        self.labels.add(label)
        self.record_count += 1
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=min(5, remaining))
        content, response_headers, status, reason, version, failure, complete = b"", [], None, None, None, None, False
        alarm_before, alarm_started = signal.getitimer(signal.ITIMER_REAL), time.monotonic()
        signal.setitimer(signal.ITIMER_REAL, min(HTTP_SECONDS, remaining, alarm_before[0] if alarm_before[0] > 0 else HTTP_SECONDS))
        try:
            if mutation is not None:
                mutation["dispatched"] = True
            if kind == "owned-user-create":
                self.user_create_attempted = True
            elif kind == "owned-application-key-create":
                self.key_create_attempted = True
            elif kind == "owned-application-key-revoke":
                self.key_revoke_attempted = True
            elif kind == "ordinary-logout":
                self.ordinary_logout_attempted.add(principal)
            connection.request(method, path, request_body if body is not MISSING else None, headers)
            response = connection.getresponse()
            status, reason, version = response.status, response.reason, response.version
            response_headers = [[name, value] for name, value in response.getheaders()]
            content_types = [value.lower() for name, value in response_headers if name.lower() == "content-type"]
            if bucket == "log" and method != "HEAD" and (status >= 400 or any("json" in value for value in content_types)):
                bucket = "json"
                limit = min(MAX_BODY, byte_limit - self.charged_json)
                require(limit > 0, "A JSON download response exhausted its separate JSON budget")
            content = response.read(limit)
            complete = response.isclosed() or response.length == 0
            lengths = [value for name, value in response_headers if name.lower() == "content-length"]
            if lengths and method != "HEAD":
                complete = complete and len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(content)
            if method == "HEAD":
                complete = complete and content == b""
        except Exception as error:
            failure = type(error).__name__
            if isinstance(error, http.client.IncompleteRead):
                content = error.partial[:limit]
        finally:
            restore_alarm(alarm_before, alarm_started)
            connection.close()
        if bucket == "log":
            self.log_bytes += len(content)
            self.charged_logs += len(content) if complete else limit
        else:
            self.json_bytes += len(content)
            self.charged_json += len(content) if complete else limit
        self.incomplete_count += int(not complete)
        result, body_type = parse_body(content)
        if mutation is not None:
            mutation.update({"status": status, "completeHTTP": complete, "transportFailure": failure})
        # Ownership and invalidity acknowledgements precede all fallible writes.
        acknowledgement_failure = None
        try:
            self.acknowledge(method, path, principal, status, result, complete, label)
        except Exception as error:
            acknowledgement_failure = type(error).__name__
            self.writes_blocked = True
        wire = {"label": label, "request": request, "requestBodyBase64": base64.b64encode(request_body).decode(),
                "responseStatus": status, "responseReason": reason, "responseHTTPVersion": version, "responseHeaders": response_headers,
                "responseBodyFile": label + ".body", "responseBodySha256": digest_bytes(content), "responseBodyBytes": len(content),
                "completeHTTP": complete, "transportFailure": failure, "bodyBudget": bucket,
                "acknowledgementFailure": acknowledgement_failure}
        record = {"request": request, "response": {"status": status, "reason": reason, "httpVersion": version,
                    "headers": response_headers, "bodyType": body_type, "body": result},
                  "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content),
                    "wireBodySha256": digest_bytes(content), "bodyExportable": body_type != "private-binary", "transportFailure": failure,
                    "bodyBudget": bucket, "privateWireFile": label + ".json", "acknowledgementFailure": acknowledgement_failure},
                  "reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": utc()}}
        self.records[label] = {"wire": wire, "record": record, "bytes": content, "persisted": False}
        def persist_http() -> None:
            save_bytes(WIRE / (label + ".body"), content)
            save(WIRE / (label + ".json"), wire)
            save(RAW / (PREFIX + label + ".json"), record)
            self.records[label]["persisted"] = True
        self.persist("http-evidence-" + label, persist_http, cleanup=self.finishing)
        self.persist("console-" + label, lambda: print(json.dumps({"capture": PREFIX + label, "status": status,
                    "completeHTTP": complete, "bytes": len(content), "bodyType": body_type}), flush=True), cleanup=self.finishing)
        if not complete or body_type == "private-binary":
            self.writes_blocked = True
        require(complete and body_type != "private-binary", "The response is not complete bounded UTF-8 evidence")
        require(acknowledgement_failure is None, "The received acknowledgement could not be classified; private bytes are retained")
        return status, result

    def query_result(self, label: str, route: str, *, parameters: dict | None = None, principal: str = "admin") -> dict:
        path = route + ("?" + urlencode(parameters) if parameters else "")
        status, result = self.request(label, "GET", path, principal=principal)
        require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list) and
                type(result.get("TotalRecordCount")) is int, "A required query response shape differs; inspect private evidence")
        return result

    def users(self, label: str, *, after: bool) -> list[dict]:
        status, rows = self.request(label, "GET", USERS_ROUTE)
        require(status == 200 and isinstance(rows, list) and all(isinstance(row, dict) for row in rows), "Fresh users response differs")
        expected = {values["REFERENCE_USER_ID"] for values in self.credentials.values()}
        if after:
            require(self.created_user is not None, "The owned new user was not acknowledged")
            expected.add(self.created_user["Id"])
        require(len(rows) == len(expected) and {row.get("Id") for row in rows} == expected,
                "Fresh account membership is outside the exact owned scope")
        if not after:
            require(not any(row.get("Name") == CREATED_USER_NAME for row in rows), "The one cause user already exists")
        for identity, values in self.credentials.items():
            row = next(row for row in rows if row["Id"] == values["REFERENCE_USER_ID"])
            require(row.get("Name") == values["REFERENCE_USERNAME"] and row.get("Policy", {}).get("IsAdministrator") is (identity == "admin"),
                    "An existing fresh account changed identity or role")
        if after:
            row = next(row for row in rows if row["Id"] == self.created_user["Id"])
            require(row.get("Name") == CREATED_USER_NAME and row.get("Policy", {}).get("IsAdministrator") is False,
                    "The unlogged-in cause account is not an ordinary owned user")
        return rows

    def prove_activity_cause(self) -> dict:
        before = self.query_result("activity-before-user", ACTIVITY_ROUTE)
        self.baselines["activity-before-user"] = copy.deepcopy(before)
        require(all(isinstance(row, dict) for row in before["Items"]), "Activity items are not objects")
        status, _ = self.request("create-owned-cause-user", "POST", NEW_USER_ROUTE, body={"Name": CREATED_USER_NAME})
        require(status in {200, 201} and self.created_user is not None, "The single owned cause user was not acknowledged")
        self.users("users-after-cause", after=True)
        previous = {json.dumps(row, sort_keys=True, ensure_ascii=False) for row in before["Items"]}
        after, matches = None, []
        for attempt in range(3):
            if attempt:
                time.sleep(0.5)
            after = self.query_result("activity-after-user-" + str(attempt), ACTIVITY_ROUTE)
            new = [row for row in after["Items"] if isinstance(row, dict) and json.dumps(row, sort_keys=True, ensure_ascii=False) not in previous]
            matches = [row for row in new if CREATED_USER_NAME in json.dumps(row, ensure_ascii=False) or
                       self.created_user["Id"] in (row.get("UserId"), row.get("ItemId"))]
            if matches:
                break
        self.user_cause_proved = bool(matches)
        self.observations.append({"case": "owned-user-creation-cause", "before": "activity-before-user",
            "after": "activity-after-user-" + str(attempt), "createdUserId": self.created_user["Id"], "newReferencingRows": matches,
            "proof": "New rows refer to the exact uniquely named user acknowledged by the immediately preceding owned creation.",
            "proved": self.user_cause_proved, "unrelatedRowsAreNotAttributed": True})
        require(self.user_cause_proved, "No new activity row refers to the owned cause user; the cause remains unproved")
        return after

    def activity_cases(self, received: dict) -> None:
        cases = [("limit-zero", {"Limit": "0"}), ("limit-one", {"Limit": "1"}),
                 ("offset-one", {"StartIndex": "1", "Limit": "1"}), ("offset-beyond", {"StartIndex": "2147483647", "Limit": "1"}),
                 ("negative-offset", {"StartIndex": "-1", "Limit": "1"}), ("negative-limit", {"Limit": "-1"}),
                 ("invalid-limit", {"Limit": "invalid"})]
        dates = [row.get("Date") for row in received["Items"] if isinstance(row, dict) and isinstance(row.get("Date"), str)]
        require(bool(dates), "The received activity rows have no usable timestamp")
        variants = date_tick_variants(dates[0])
        self.allowed_dates.update(variants.values())
        self.allowed_dates.add("goby-observability-invalid-date")
        self.observations.append({"case": "min-date-selected-from-response", "receivedDate": dates[0], "variants": variants,
                                  "boundarySemanticsNotAssumed": True})
        for label, parameters in cases:
            self.request("activity-" + label, "GET", ACTIVITY_ROUTE + "?" + urlencode(parameters))
        for label, value in variants.items():
            self.request("activity-min-date-" + label, "GET", ACTIVITY_ROUTE + "?" + urlencode({"MinDate": value}))
        self.request("activity-min-date-page", "GET", ACTIVITY_ROUTE + "?" + urlencode({"MinDate": dates[0], "StartIndex": "1", "Limit": "1"}))
        self.request("activity-min-date-invalid", "GET", ACTIVITY_ROUTE + "?" + urlencode({"MinDate": "goby-observability-invalid-date"}))

    def select_log(self, listed: dict) -> None:
        rows = listed["Items"]
        require(0 < len(rows) <= 64 and all(isinstance(row, dict) for row in rows), "The fresh log inventory is empty or unbounded")
        for row in rows:
            name = row.get("Name")
            require(isinstance(name, str) and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9 ._()-]{0,180}", name) is not None and
                    name not in {".", "..", UNKNOWN_LOG_NAME} and name == Path(name).name,
                    "A returned log name is outside the bounded filename grammar")
            self.allowed_log_names.add(name)
        eligible = [row for row in rows if type(row.get("Size")) is int and 0 <= row["Size"] < MAX_BODY // 4]
        require(bool(eligible), "No listed log fits the complete-download budget")
        chosen = next((row for row in eligible if row["Name"].lower() == "embyserver.txt"), eligible[0])
        self.selected_log_name = chosen["Name"]
        self.observations.append({"case": "selected-owned-fresh-log", "name": self.selected_log_name, "listedMetadata": chosen,
            "selection": "A basename returned by this attested empty reference, with a conservative listed-size ceiling of 256 KiB for repeated JSON line captures.",
            "listedSizeUnitNotAssumed": True, "listedSizeIsOnlyASelectionHint": True,
            "growthOrSerializationOverflowStopsCaptureAsIncomplete": True})

    def capture(self) -> None:
        status, public = self.request("public-identity", "GET", PUBLIC_ROUTE)
        require(status == 200 and isinstance(public, dict) and public.get("Id") == self.manifest["serverId"] and public.get("Version") == "4.9.5.0",
                "The HTTP server identity differs from the fresh handoff")
        self.baselines["users"] = self.users("users-before", after=False)
        libraries = self.query_result("libraries-before", LIBRARIES_ROUTE)
        require(libraries["Items"] == [] and libraries["TotalRecordCount"] == 0, "The fresh reference is not library-free")
        keys = self.query_result("keys-before", KEYS_PAGE)
        require(keys["Items"] == [] and keys["TotalRecordCount"] == 0, "Application credential baseline is not empty")
        self.key_baseline_empty = True
        activity = self.prove_activity_cause()
        listed = self.query_result("logs-before", LOGS_ROUTE)
        self.select_log(listed)
        status, _ = self.request("create-owned-read-key", "POST", KEYS_ROUTE + "?" + urlencode({"App": APP_NAME}))
        require(status in {200, 204}, "The single owned application creation was not acknowledged")
        self.query_result("keys-owned-proof", KEYS_PAGE)
        require(self.owned_key is not None and not self.key_ownership_failed, "Unique owned application credential membership is unproved")
        log_path = LOG_PREFIX + quote(self.selected_log_name, safe="")
        routes = (("activity", ACTIVITY_ROUTE), ("log-query", LOGS_ROUTE), ("log-download", log_path), ("log-lines", log_path + "/Lines"))
        for principal in ("anonymous", "invalid", "viewer", "admin", "application"):
            for name, path in routes:
                self.request("auth-" + principal + "-" + name, "GET", path, principal=principal)
        self.activity_cases(activity)
        for name, parameters in (("limit-zero", {"Limit": "0"}), ("limit-one", {"Limit": "1"}),
                ("offset-one", {"StartIndex": "1", "Limit": "1"}), ("offset-beyond", {"StartIndex": "2147483647", "Limit": "1"}),
                ("negative-offset", {"StartIndex": "-1"}), ("negative-limit", {"Limit": "-1"}), ("invalid-limit", {"Limit": "invalid"})):
            self.request("logs-query-" + name, "GET", LOGS_ROUTE + "?" + urlencode(parameters))
        for value in ("false", "true", "invalid"):
            self.request("log-download-sanitize-" + value, "GET", log_path + "?Sanitize=" + value)
        for name, parameters in (("limit-one", {"Limit": "1"}), ("limit-zero", {"Limit": "0"}),
                ("offset-one", {"StartIndex": "1", "Limit": "1"}), ("offset-beyond", {"StartIndex": "2147483647", "Limit": "1"}),
                ("start-position-one", {"StartPosition": "1"}), ("search-unmatched", {"SearchTerm": "goby-observability-m5i-unmatched-text"})):
            self.request("log-lines-" + name, "GET", log_path + "/Lines?" + urlencode(parameters))
        self.request("log-head-admin", "HEAD", log_path)
        self.request("log-head-anonymous", "HEAD", log_path, principal="anonymous")
        self.request("log-unknown-download", "GET", LOG_PREFIX + UNKNOWN_LOG_NAME)
        self.request("log-unknown-lines", "GET", LOG_PREFIX + UNKNOWN_LOG_NAME + "/Lines")
        self.users("users-final-before-cleanup", after=True)
        final_libraries = self.query_result("libraries-final-before-cleanup", LIBRARIES_ROUTE)
        require(final_libraries["Items"] == [] and final_libraries["TotalRecordCount"] == 0, "The fresh library inventory changed")
        self.query_result("activity-final-before-cleanup", ACTIVITY_ROUTE)
        self.query_result("logs-final-before-cleanup", LOGS_ROUTE)
        self.observations.append({"case": "wire-only-interpretation", "sortingCursorFilteringDefaultsNotAssumed": True,
            "logMayGrowBetweenRequests": True, "rawDownloadEqualityDoesNotAloneProveSanitizeSemantics": True,
            "configurationTaskLibraryMediaMutations": 0, "newOrdinaryLogins": 0, "createdUsers": 1,
            "createdUserWasNeverAuthenticated": True, "applicationReadRoutes": [path for _, path in routes]})

    def retire_key(self) -> None:
        if not self.key_create_attempted:
            return
        if self.owned_key is None and not self.key_ownership_failed:
            self.cleanup_step("key-discovery", lambda: self.request("cleanup-key-discover", "GET", KEYS_PAGE))
        require(self.owned_key is not None and not self.key_ownership_failed, "The attempted application credential has no unique ownership proof")
        if not self.key_revoke_attempted:
            self.cleanup_step("key-revocation", lambda: self.request("cleanup-key-revoke", "DELETE",
                              KEYS_ROUTE + "/" + quote(self.owned_key["AccessToken"], safe="")))
        self.cleanup_step("key-invalidity", lambda: self.request("cleanup-key-invalid", "GET", SESSIONS_ROUTE, principal="application"))
        result = self.cleanup_step("key-empty-list", lambda: self.query_result("cleanup-key-list-empty", KEYS_PAGE))
        require(self.key_revoke_status in {200, 204} and self.key_invalidity is not None and self.key_invalidity["status"] == 401 and
                isinstance(result, dict) and result["Items"] == [] and result["TotalRecordCount"] == 0,
                "Application credential revocation and independent invalidity are not fully proved")

    def logout(self, identity: str) -> None:
        if identity not in self.credentials:
            return
        self.cleanup_step("ordinary-logout-" + identity, lambda: self.request("cleanup-logout-" + identity, "POST", LOGOUT_ROUTE, principal=identity))
        self.cleanup_step("ordinary-invalidity-" + identity, lambda: self.request("cleanup-invalid-" + identity, "GET", SESSIONS_ROUTE, principal=identity))
        require(self.logout_statuses.get(identity) in {200, 204} and self.invalidity.get(identity, {}).get("status") == 401,
                "An ordinary credential lacks logout acknowledgement and an independent 401")

    def audit_http(self) -> dict:
        expected = set(self.records)
        require(expected == self.labels and len(expected) == self.record_count, "The HTTP attempt journal membership differs")
        require(load(PRIVATE / "baseline.json") == self.setup_baseline and
                (ROOT / ".goby-managed").read_bytes() == (CAPTURE_MARKER + "\n").encode() and
                same_value(load(PRIVATE / "mutation-results.json"), self.mutations), "Capture authority or mutation journal bytes differ")
        require({path.name for path in MUTATIONS.iterdir()} == {row["label"] + ".json" for row in self.mutations},
                "Mutation intent membership differs")
        for mutation in self.mutations:
            intent = {"label": mutation["label"], "request": mutation["request"], "status": None, "dispatched": False}
            require(same_value(load(MUTATIONS / (mutation["label"] + ".json")), intent), "A mutation intent differs from its retained dispatch")
            if mutation["dispatched"]:
                require(mutation["label"] in expected and same_value(mutation["request"], self.records[mutation["label"]]["record"]["request"]),
                        "A dispatched mutation lacks matching HTTP evidence")
        require({path.name for path in WIRE.iterdir()} == {label + extension for label in expected for extension in (".json", ".body")},
                "Private wire membership differs from all attempted HTTP requests")
        require({path.name for path in RAW.iterdir()} == {PREFIX + label + ".json" for label in expected},
                "Private record membership differs from all attempted HTTP requests")
        expected_private = {PRIVATE / "baseline.json", PRIVATE / "mutation-results.json",
                            *(MUTATIONS / (row["label"] + ".json") for row in self.mutations),
                            *(WIRE / (label + extension) for label in expected for extension in (".json", ".body")),
                            *(RAW / (PREFIX + label + ".json") for label in expected)}
        require({path for path in PRIVATE.rglob("*") if path.is_file()} == expected_private, "Unexpected private capture file membership")
        hashes = {}
        for label in sorted(expected):
            retained = self.records[label]
            require(retained["persisted"], "An attempted HTTP exchange has incomplete evidence persistence")
            wire_path, body_path, raw_path = WIRE / (label + ".json"), WIRE / (label + ".body"), RAW / (PREFIX + label + ".json")
            wire, raw = load(wire_path), load(raw_path)
            canonical(body_path)
            content = body_path.read_bytes()
            require(same_value(wire, retained["wire"]) and same_value(raw, retained["record"]) and content == retained["bytes"],
                    "Private evidence bytes changed after the HTTP exchange")
            require(wire["responseBodyFile"] == body_path.name and wire["responseBodyBytes"] == len(content) == raw["observation"]["wireBytes"] and
                    wire["responseBodySha256"] == digest_bytes(content) == raw["observation"]["wireBodySha256"], "Wire body digest or length differs")
            body, kind = parse_body(content)
            require(kind == raw["response"]["bodyType"] and same_value(body, raw["response"]["body"]),
                    "The raw response does not correspond to the retained HTTP body bytes")
            require(same_value(wire["request"], raw["request"]) and wire["responseStatus"] == raw["response"]["status"] and
                    same_value(wire["responseHeaders"], raw["response"]["headers"]) and wire["responseReason"] == raw["response"]["reason"] and
                    wire["responseHTTPVersion"] == raw["response"]["httpVersion"] and wire["completeHTTP"] == raw["observation"]["completeHTTP"],
                    "Response metadata does not correspond to private wire evidence")
            request_body = base64.b64decode(wire["requestBodyBase64"], validate=True)
            expected_body = json.dumps(raw["request"]["body"], separators=(",", ":"), allow_nan=False).encode() if raw["request"]["bodyPresent"] else b""
            require(request_body == expected_body, "The request body does not correspond to its dispatched bytes")
            self.collect_secrets(raw)
            hashes[label] = {"wireSha256": digest(wire_path), "wireBodySha256": digest(body_path), "rawSha256": digest(raw_path),
                             "bytes": len(content), "completeHTTP": wire["completeHTTP"]}
        return hashes

    def export_records(self, audited: dict) -> dict:
        require(not list(EXPORT.iterdir()), "Refusing to replace an earlier export")
        expected = {PREFIX + label + ".json" for label in self.records}
        for label in sorted(self.records):
            raw = load(RAW / (PREFIX + label + ".json"))
            exported = self.sanitize(raw)
            content = json_bytes(exported)
            require(not any(secret.encode() in content for secret in self.secrets), "A known credential survived export")
            save_bytes(EXPORT / (PREFIX + label + ".json"), content)
        require({path.name for path in EXPORT.iterdir()} == expected, "Safe export membership differs")
        for label in sorted(self.records):
            raw_path, export_path = RAW / (PREFIX + label + ".json"), EXPORT / (PREFIX + label + ".json")
            raw, exported = load(raw_path), load(export_path)
            require(same_value(self.sanitize(raw), exported) and export_path.read_bytes() == json_bytes(exported),
                    "The exported bytes do not match the deterministic sanitized private record")
            audited[label]["exportSha256"] = digest(export_path)
        return audited

    def finish(self) -> None:
        self.finishing, self.writes_blocked = True, True
        self.cleanup_deadline, self.cleanup_phases = time.monotonic() + CLEANUP_SECONDS, []
        # Every credential action remains independent of all persistence and
        # earlier cleanup failures. The ordinary administrator is always last.
        self.cleanup_phase("application-credential-retirement", self.retire_key, CREDENTIAL_PHASE_SECONDS)
        self.cleanup_phase("viewer-credential-retirement", lambda: self.logout("viewer"), CREDENTIAL_PHASE_SECONDS)
        self.cleanup_phase("administrator-credential-retirement", lambda: self.logout("admin"), CREDENTIAL_PHASE_SECONDS)
        audited = self.cleanup_phase("evidence-and-preservation", self.finish_evidence, CREDENTIAL_PHASE_SECONDS)
        require(audited is True, "The cleanup evidence phase did not complete")

    def finish_evidence(self) -> bool:
        preserved = self.cleanup_step("protected-and-setup-preservation", lambda: self.verify_preservation() or True) is True
        self.persist("mutation-results", lambda: save(PRIVATE / "mutation-results.json", self.mutations), cleanup=True)
        wire_audit = self.cleanup_step("complete-wire-correspondence", self.audit_http)
        exports = self.cleanup_step("safe-export-correspondence", lambda: self.export_records(wire_audit)) if wire_audit is not None else None
        credentials_retired = all(self.logout_statuses.get(identity) in {200, 204} and self.invalidity.get(identity, {}).get("status") == 401
                                  for identity in self.credentials)
        key_retired = (not self.key_create_attempted or self.owned_key is not None and self.key_revoke_status in {200, 204} and
                       self.key_invalidity is not None and self.key_invalidity.get("status") == 401)
        self.cleanup_ok = preserved and credentials_retired and key_retired and not self.cleanup_errors and not self.persistence_failures
        require(time.monotonic() < self.deadline, "The evidence audit deadline expired")
        files = {}
        for path in PRIVATE.rglob("*"):
            require(time.monotonic() < self.deadline, "The private evidence inventory deadline expired")
            require(not path.is_symlink(), "Capture evidence contains a symbolic link")
            if path.is_file():
                canonical(path)
                files[str(path.relative_to(ROOT))] = {"sha256": digest(path), "bytes": path.stat().st_size}
        audit = {"kind": "capture-audit-observation", "reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": utc()},
            "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok, "operatorCleanupRequired": True,
            "checks": {"protectedServicesAndAllEarlierEvidenceUnchanged": preserved, "ordinaryCredentialsInvalidated": credentials_retired,
                "applicationCredentialInvalidated": bool(key_retired), "allHTTPAttemptsHaveByteCorrespondence": wire_audit is not None,
                "allSafeExportsCorrespondToPrivateBytes": exports is not None, "allHTTPBodiesComplete": self.incomplete_count == 0,
                "allHTTPBodiesUTF8Exportable": all(row["record"]["observation"]["bodyExportable"] for row in self.records.values()),
                "ownedUserCreationActivityCauseProved": self.user_cause_proved},
            "httpAttempts": self.record_count, "jsonResponseBytes": self.json_bytes, "logResponseBytes": self.log_bytes,
            "chargedJsonBytes": self.charged_json, "chargedLogBytes": self.charged_logs,
            "httpRecords": exports, "privateFilesBeforeAudit": files, "observations": self.observations,
            "mutations": self.sanitize(self.mutations), "cleanupErrors": self.cleanup_errors, "persistenceFailures": self.persistence_failures,
            "ordinaryLogoutStatuses": self.logout_statuses, "ordinaryInvalidity": self.invalidity,
            "credentialCleanupPhases": copy.deepcopy(self.cleanup_phases),
            "applicationRevokeStatus": self.key_revoke_status, "applicationInvalidity": self.key_invalidity,
            "credentials": {"operatorOrdinaryLoginCount": 2, "recorderOrdinaryLoginCount": 0,
                "applicationKeyCreationAttempts": int(self.key_create_attempted), "userCreationAttempts": int(self.user_create_attempted),
                "createdUserAuthenticated": False, "retainedFreshAccountCountIfCaptureCompleted": 3},
            "limits": {"mainRequests": MAIN_REQUESTS, "captureRequestsIncludingCleanup": MAX_REQUESTS,
                "operatorSetupRequests": OPERATOR_SETUP_REQUESTS, "operatorCleanupRequests": OPERATOR_CLEANUP_REQUESTS,
                "allPhaseHTTPRequests": ALL_HTTP_REQUESTS, "jsonBodyBytes": MAX_BODY, "logBodyBytes": MAX_LOG_BODY,
                "totalJsonBytes": MAX_JSON_TOTAL, "mainJsonBytes": MAIN_JSON_TOTAL, "totalLogBytes": MAX_LOG_TOTAL,
                "mainSeconds": MAIN_SECONDS, "cleanupSeconds": CLEANUP_SECONDS, "singleHTTPSeconds": HTTP_SECONDS,
                "credentialPhaseSeconds": CREDENTIAL_PHASE_SECONDS, "initializationCredentialPhaseSeconds": INITIALIZATION_PHASE_SECONDS,
                "evidencePhaseSeconds": CREDENTIAL_PHASE_SECONDS, "cleanupAuthorityAndPersistenceIncludedInOuterAlarm": True},
            "interpretation": "Complete bounded response bytes are evidence; undeclared parameters, defaults, sorting, cursor and sanitize rules are not inferred.",
            "sanitization": {"exportsCreatedOnlyAfterAllCredentialCleanup": True, "rawLogsRemainPrivate": True,
                "knownCredentialsAndMixedPercentJSONHexSpellings": True, "wholeBase64AndURLUserinfoInspection": True,
                "unknownLowEntropySensitiveTextIsFieldMaskedOnly": True, "acknowledgedKnownCredentialsAlwaysGloballyRedacted": True,
                "encodedTextRecursionLimit": 3, "excessiveEncodedTextPolicy": "Hide the whole field", "internalPathsRedacted": True},
            "retainedFreshHistory": "Three exclusively owned accounts, two revoked ordinary logins and one revoked application credential remain until explicit operator DATA cleanup."}
        raw_path, export_path = RAW / (PREFIX + "audit.json"), EXPORT / (PREFIX + "audit.json")
        save(raw_path, audit)
        cleaned = self.sanitize(audit)
        save(export_path, cleaned)
        require(same_value(load(raw_path), audit) and same_value(load(export_path), cleaned) and export_path.read_bytes() == json_bytes(cleaned),
                "The final audit pair failed byte correspondence")
        require({path.name for path in EXPORT.iterdir()} == {PREFIX + label + ".json" for label in self.records} | {PREFIX + "audit.json"},
                "Final export membership differs from the audited HTTP records and audit pair")
        require(self.cleanup_ok and wire_audit is not None and exports is not None, "Capture cleanup, preservation or byte correspondence is incomplete")
        return True

    def initialization_cleanup(self) -> None:
        self.evidence_unavailable, self.finishing, self.writes_blocked = True, True, True
        self.cleanup_deadline, self.cleanup_phases = time.monotonic() + CLEANUP_SECONDS, []
        for identity in ("viewer", "admin"):
            self.cleanup_phase("initialization-retirement-" + identity, lambda identity=identity: self.logout(identity), INITIALIZATION_PHASE_SECONDS)
        print(json.dumps({"capture": PREFIX, "initializationComplete": False, "ordinaryInvalidity": self.invalidity,
                          "privateEvidencePersistenceComplete": False, "operatorCleanupRequired": True}), flush=True)


def deadline_expired(_signum: int, _frame: object) -> None:
    raise TimeoutError("The bounded HTTP deadline expired")


def main() -> None:
    signal.signal(signal.SIGALRM, deadline_expired)
    def terminate(_signum: int, _frame: object) -> None:
        raise InterruptedError("The observability recorder received a termination signal")
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
        if recorder is not None:
            recorder.capture_failure = failure
            recorder.writes_blocked = True
    finally:
        if not initialized:
            if recorder is not None and getattr(recorder, "credentials", None):
                recorder.initialization_cleanup()
            print(json.dumps({"capture": PREFIX, "result": "partial", "failureType": failure,
                              "initializationComplete": False, "operatorCleanupRequired": True}), flush=True)
            raise SystemExit(1)
        try:
            recorder.finish()
        except Exception as error:
            print(json.dumps({"capture": PREFIX, "result": "partial", "cleanup": "failed", "failureType": type(error).__name__,
                              "operatorCleanupRequired": True}), flush=True)
            raise SystemExit(1)
    print(json.dumps({"capture": PREFIX, "result": "complete" if failure is None else "partial", "failureType": failure,
                      "httpAttempts": recorder.record_count, "captureRecords": recorder.record_count + 1,
                      "cleanup": recorder.cleanup_ok, "operatorCleanupRequired": True}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
