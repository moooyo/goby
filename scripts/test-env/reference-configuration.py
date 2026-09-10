#!/usr/bin/env python3
"""Capture bounded, read-only ConfigurationService contracts on test-env.

Only two reserved ordinary logins and their logout requests may write state.
Configuration, task, device, user-policy, library, media and application-key
mutations are denied. Named configuration routes have explicit official client
provenance; historical callers do not imply support in the sampled version.
Raw wire bodies, configuration values and credentials remain root-private.
Run only through authorized root SSH beside the three pinned recorder modules.
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
import signal
import stat
import sys
import time
from urllib.parse import parse_qs, quote, unquote, unquote_plus, urlsplit, urlunsplit

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("configuration_scheduled_reference", Path(__file__).with_name("reference-scheduled-tasks.py"))
read = importlib.util.module_from_spec(spec)
spec.loader.exec_module(read)
device, base = read.device, read.base
READ_SOURCE_SHA256 = "b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1"
DEVICE_SOURCE_SHA256 = "96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d"
BASE_SOURCE_SHA256 = "ead25f9d47e8faf0b2b579ca8641b4ddcf4f7993c9e5a16710624c2927e749a8"
EXPECTED_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
REFERENCE_PID, MAIN_PID, MAIN_START_TICKS = 3131777, 3570491, "24600634"
MAIN_UNIT = "goby-foundation-test.service"
MAIN_BINARY_SHA256 = "2993870cce6f4e0ae2c3630b645985cab664ce5e5e18123fdc920fb241bb2abf"
OLD_RECORDS, MAX_TASKS = 1965, 64
PRIOR_EVIDENCE = Path("/opt/goby-test/exec-scratch/emby-scheduled-tasks-fresh-m5f-20260910-01")
PRIOR_CAPTURE = PRIOR_EVIDENCE / "runtime/task-capture"
REMOVED_DATA = Path("/dev/shm/goby-emby-scheduled-tasks-fresh-m5f-20260910-01")
REMOVED_SOURCE = PRIOR_EVIDENCE / "source"
HISTORICAL_REMOVED_MARKER = "/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-02/.goby-managed"
ORIGINAL_DEVICE_FINAL = Path("/opt/goby-test/exec-scratch/scheduled-tasks-m5f/private/raw/scheduled-tasks-m5f-devices-final-before-control-logout.json")
HIDDEN_DEVICE_ID = "15"
MISSING = device.MISSING
base.__file__ = __file__
base.ROOT = Path("/opt/goby-test/exec-scratch/configuration-m5g")
base.PRIVATE, base.RAW, base.EXPORT = base.ROOT / "private", base.ROOT / "private/raw", base.ROOT / "export"
base.PREFIX = "configuration-m5g-"
base.MARKER = "goby-reference-configuration-m5g-read-only-v1"
device.DEVICE_PREFIX = "goby-configuration-m5g-20260910-01-"
device.CLIENT_PREFIX = "Goby Configuration M5g 20260910 01 "
device.IDENTITIES = {"control": ("admin", "control", "Control", "Configuration Control", "0.1.0"),
                     "viewer": ("viewer", "viewer", "Viewer", "Configuration Viewer", "0.1.0")}
device.REFERENCE_RUN = 2
UNKNOWN_CONFIGURATION = device.DEVICE_PREFIX + "unknown-configuration"
NAMED_CONFIGURATION_EVIDENCE = {
    "encoding": {"url": "https://github.com/MediaBrowser/Emby/blob/66e4d9ca63dcf46c4e57159252d34f9479515a42/MediaBrowser.WebDashboard/dashboard-ui/scripts/encodingsettings.js#L1",
                 "call": 'ApiClient.getNamedConfiguration("encoding")', "scope": "Official historical WebDashboard client, 2018; current support is unverified"},
    "devices": {"url": "https://github.com/MediaBrowser/Emby.ApiClient.Javascript/blob/fdd0939ac596406ab92274f30fd83d226a677424/apiclient.js#L2198",
                "call": 'this.getUrl("System/Configuration/devices")', "scope": "Official API client getDevicesOptions, followed by getJSON; target support is unverified"},
    "dlna": {"url": "https://github.com/MediaBrowser/Emby/blob/2334e1ecd577e3275602f0155793cb9024adc39f/MediaBrowser.WebDashboard/dashboard-ui/scripts/dlnasettings.js#L1",
             "call": 'ApiClient.getNamedConfiguration("dlna")', "scope": "Official historical WebDashboard client, 2017; current support is unverified"},
}
NAMED_KEYS = tuple(NAMED_CONFIGURATION_EVIDENCE)
CONFIGURATION_ROUTES = {"total": "/emby/System/Configuration", **{key: "/emby/System/Configuration/" + key for key in NAMED_KEYS},
                        "unknown": "/emby/System/Configuration/" + UNKNOWN_CONFIGURATION}
URL_PATTERN = re.compile(r"(?i)(?:\b[a-z][a-z0-9+.-]{0,31}:)?//[^\s\"'<>]+|(?<![\w:/])/(?!/)[^\s\"'<>]*[?#][^\s\"'<>]*")
PUBLIC_BOOLEAN_FLAGS = {"haspassword", "hasconfiguredpassword", "enablelocalpassword"}
SECRET_EXACT = {"key", "secret", "password", "pw", "passwd", "newpw", "token", "authorization", "cookie", "setcookie",
                "pin", "pincode", "salt", "privatekey", "certificatepassword", "pfxpassword", "connectionstring", "dsn", "credentials"}
SECRET_SUFFIXES = ("password", "passwd", "passwordhash", "secret", "token", "apikey", "accesskey", "privatekey", "signingkey",
                   "encryptionkey", "licensekey", "subscriptionkey", "secretkey", "masterkey", "credential", "credentials", "connectionstring", "signature")


def normalized(value: str) -> str:
    return re.sub(r"[^a-z0-9]", "", value.lower())


def sensitive_field(value: str) -> bool:
    name = normalized(value)
    return name in SECRET_EXACT or name.endswith(SECRET_SUFFIXES)


def header_field(value: str) -> bool:
    return normalized(value).endswith(("headers", "header"))


def sensitive_header(value: str) -> bool:
    return base.sensitive_header(value) or sensitive_field(value)


def header_descriptor(value: dict, field: str) -> tuple[str, str] | None:
    if not header_field(field):
        return None
    name = next((key for key in ("Name", "name", "Key", "key") if isinstance(value.get(key), str)), None)
    content = next((key for key in ("Value", "value") if key in value), None)
    return (name, content) if name and content else None


def decoded_url(value: str) -> str | None:
    candidate = value
    for depth in range(3):
        if re.match(r"(?i)^(?:[a-z][a-z0-9+.-]{0,31}:)?//", candidate) or (
                not any(character.isspace() for character in candidate) and any(marker in candidate for marker in ("?", "#")) and
                (candidate.startswith(("/", "./", "../", "?", "#")) or re.match(r"^[A-Za-z0-9._~-]+(?:/|[?#])", candidate))):
            return candidate
        if depth == 2:
            # Do not perform a third decode or return a still-encoded URL to
            # the exporter. Delimiter escapes remain recognizable even when
            # their percent sign is wrapped in additional encoding layers.
            return "[REDACTED_URL]" if re.search(r"(?i)%(?:25)*(?:3a|2f|3f|23|40)", candidate) else None
        decoded = unquote(candidate)
        if decoded == candidate:
            break
        candidate = decoded
    return None


def urls_in_text(value: str) -> list[str]:
    whole = decoded_url(value)
    return [whole] if whole is not None else [matched.group() for matched in URL_PATTERN.finditer(value)]


def same_value(before: object, after: object) -> bool:
    if type(before) is not type(after):
        return False
    if isinstance(before, dict):
        return before.keys() == after.keys() and all(same_value(value, after[key]) for key, value in before.items())
    if isinstance(before, (list, tuple)):
        return len(before) == len(after) and all(same_value(left, right) for left, right in zip(before, after))
    return before == after


def main_identity() -> dict:
    output = base.subprocess.check_output(["systemctl", "show", MAIN_UNIT, "-p", "MainPID", "-p", "ActiveState", "-p", "User",
                                          "-p", "WorkingDirectory", "-p", "ExecStart"], text=True, timeout=5)
    properties = dict(line.split("=", 1) for line in output.splitlines())
    folder = Path("/proc") / str(MAIN_PID)
    identity = {"pid": MAIN_PID, "uid": folder.stat().st_uid, "startTicks": (folder / "stat").read_text().rsplit(")", 1)[1].split()[19],
                "networkNamespace": os.readlink(folder / "ns/net"), "executable": os.readlink(folder / "exe")}
    base.require(properties.get("MainPID") == str(MAIN_PID) and properties.get("ActiveState") == "active" and identity["uid"] == 995 and
                 identity["startTicks"] == MAIN_START_TICKS and identity["executable"] == "/opt/goby-dev/goby", "The protected M5f Goby process differs")
    identity["binarySha256"] = base.digest(Path(identity["executable"]))
    base.require(identity["binarySha256"] == MAIN_BINARY_SHA256, "The protected M5f binary differs")
    identity["serviceProperties"] = properties
    return identity


def preconditions() -> int:
    base.require(base.digest(Path(read.__file__)) == READ_SOURCE_SHA256 and base.digest(Path(device.__file__)) == DEVICE_SOURCE_SHA256 and
                 base.digest(Path(__file__).with_name("reference-api-keys.py")) == BASE_SOURCE_SHA256, "A pinned recorder dependency changed")
    pid = base.preconditions()
    base.require(pid == REFERENCE_PID and base.ROOT.parent.resolve(strict=True) == base.ROOT.parent and not base.ROOT.is_symlink(),
                 "Original reference process or capture root differs")
    base.require(base.shutil.disk_usage(base.ROOT.parent).free > 96 * 1024 * 1024, "Capture scratch lacks its bounded evidence reserve")
    main_identity()
    return pid


class Recorder(read.Recorder):
    def __init__(self, pid: int) -> None:
        self.task_ids, self.task_baseline, self.baseline_filters = set(), {}, {}
        self.task_observations, self.task_configuration_changes, self.task_runtime_changes = [], [], []
        self.task_bound_exceeded = False
        self.hidden_device_before, self.hidden_options_before = None, None
        self.previous_device_ids, self.historical_missing_paths = set(), []
        self.configuration_baseline, self.configuration_observations, self.configuration_changes = {}, [], []
        self.configuration_complete = False
        self.wire_records, self.redaction_audits = {}, []
        self.persistence_failures = []
        self._secret_signature, self._secret_regex = None, None
        self.main_before = main_identity()
        # Bypass the old ScheduledTasks constructor's obsolete main-service pin.
        # The unchanged device/base constructors retain owned-login bookkeeping.
        device.Recorder.__init__(self, pid)
        self.read_ids = {HIDDEN_DEVICE_ID}
        (base.PRIVATE / "wire").mkdir(mode=0o700)

    def snapshot(self) -> dict:
        baseline_path = PRIOR_CAPTURE / "private/baseline.json"
        audit_path = PRIOR_CAPTURE / "private/raw/scheduled-tasks-fresh-m5f-audit.json"
        for path in (baseline_path, audit_path, ORIGINAL_DEVICE_FINAL):
            base.private_file(path)
        prior, audit = json.loads(baseline_path.read_text()), json.loads(audit_path.read_text())
        base.require(audit.get("cleanupPassed") is True and audit.get("captureFailureType") is None and
                     audit.get("httpAttempts") == 153 and audit.get("incompleteHTTP") == 0,
                     "The preceding fresh task capture lacks its successful bounded cleanup")
        paths = [Path(name) for name in prior["records"]]
        for folder in (PRIOR_CAPTURE / "private/raw", PRIOR_CAPTURE / "export"):
            paths.extend(folder.glob("*.json"))
        base.require(len(paths) == 3930 and len(set(paths)) == 3930, "Expected exactly 1965 preceding raw/export pairs")
        media = [Path(name) for name in prior["media"]]
        base.require(len(media) == 240 and len(set(media)) == 240, "Expected exactly 240 original source files")
        for group in ("records", "media", "privateFiles", "retainedHistoricalFiles"):
            for name, expected in prior.get(group, {}).items():
                path = Path(name)
                base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode) and base.digest(path) == expected,
                             "A retained prior evidence or source file changed")
        base.require(prior.get("historicalRemovedPaths") == [HISTORICAL_REMOVED_MARKER] and not Path(HISTORICAL_REMOVED_MARKER).exists() and
                     not Path(HISTORICAL_REMOVED_MARKER).is_symlink(), "The earlier explicit marker deletion differs")
        owned = prior.get("ownedSourceFiles", {})
        source_snapshot = prior.get("ownedSourceSnapshot", {})
        base.require(isinstance(owned, dict) and len(owned) == 512 and source_snapshot.get("root") == str(REMOVED_SOURCE) and
                     source_snapshot.get("fileCount") == 512 and set(source_snapshot.get("files", {})) == set(owned) and
                     all(REMOVED_SOURCE in Path(name).parents and source_snapshot["files"][name].get("sha256") == digest for name, digest in owned.items()),
                     "The retired owned source provenance differs")
        base.require(not REMOVED_SOURCE.exists() and not REMOVED_SOURCE.is_symlink() and not REMOVED_DATA.exists() and not REMOVED_DATA.is_symlink(),
                     "The previous disposable task fixture has not been removed")
        self.historical_missing_paths = [HISTORICAL_REMOVED_MARKER, str(REMOVED_DATA), str(REMOVED_SOURCE)]
        roots = {path.parent.parent for path in paths if path.parent.name == "raw"}
        private_files = set()
        for root in roots:
            for path in root.rglob("*"):
                base.require(not path.is_symlink(), "A preserved private tree contains a symbolic link")
                if path.is_file():
                    private_files.add(path)
                base.require(len(private_files) < 8192, "Prior private membership exceeds its bound")
        base.require(sum(path.stat().st_size for path in private_files) < 128 * 1024 * 1024 and
                     sum(path.stat().st_size for path in media) < 32 * 1024 * 1024, "Preserved private/source bytes exceed their bounds")
        for path in [*paths, *media, *private_files]:
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode), "A preserved path is not canonical and regular")
        devices = json.loads(ORIGINAL_DEVICE_FINAL.read_text())["response"]
        base.require(devices.get("status") == 200 and isinstance(devices.get("body"), dict) and isinstance(devices["body"].get("Items"), list),
                     "Original reference device membership is unavailable")
        self.previous_device_ids = {row["Id"] for row in devices["body"]["Items"]}
        base.require(len(devices["body"]["Items"]) == len(self.previous_device_ids) == 22, "Expected 22 protected original devices")
        return {"records": {str(path): base.digest(path) for path in paths}, "media": {str(path): base.digest(path) for path in media},
                "privateFiles": {str(path): base.digest(path) for path in sorted(private_files)},
                "retainedHistoricalFiles": prior.get("retainedHistoricalFiles", {}), "historicalRemovedPaths": self.historical_missing_paths,
                "retiredOwnedSources": {"files": owned, "snapshot": source_snapshot, "removedAfterCompletedStudy": True}}

    @staticmethod
    def public_paths(record: object) -> set[tuple]:
        # Configuration JSON has no generic public Key exemption. Only the
        # already observed, separately read task DTO locations are public.
        return set(read.task_identifier_paths(record))

    def collect_secrets(self, value: object, key: str = "") -> None:
        public = self.public_paths(value) if not key else set()
        def collect(node: object, field: str, path: tuple, inherited: bool = False) -> None:
            sensitive = inherited or sensitive_field(field) and path not in public
            if isinstance(node, dict):
                descriptor = header_descriptor(node, field)
                for name, item in node.items():
                    if descriptor and name == descriptor[0]:
                        continue
                    force = sensitive or header_field(field) and sensitive_header(name) or (
                        descriptor is not None and name == descriptor[1] and sensitive_header(node[descriptor[0]]))
                    collect(item, name, path + (name,), force)
            elif isinstance(node, (list, tuple)):
                if header_field(field) and all(isinstance(pair, (list, tuple)) and len(pair) == 2 for pair in node):
                    for index, (name, item) in enumerate(node):
                        collect(item, str(name), path + (index, 1), sensitive or sensitive_header(str(name)))
                else:
                    for index, item in enumerate(node):
                        collect(item, field, path + (index,), sensitive)
            elif isinstance(node, str):
                if sensitive and node:
                    self.secrets.add(node)
                    authorization = re.match(r"(?i)^(?:Bearer|Basic|Token)\s+(.+)$", node)
                    if authorization:
                        self.secrets.add(authorization[1])
                for url in urls_in_text(node):
                    try:
                        parsed = urlsplit(url)
                        for credential in (parsed.username, parsed.password):
                            if credential:
                                self.secrets.add(unquote(credential))
                        for part in re.split(r"[&;]", parsed.query):
                            name, separator, item = part.partition("=")
                            if separator and item and sensitive_field(unquote_plus(name)):
                                self.secrets.add(unquote_plus(item))
                    except ValueError:
                        pass
        collect(value, key, ())

    @staticmethod
    def secret_pattern(secret: str) -> str:
        parts = []
        for character in secret:
            encoded = "".join("%" + "".join("[" + digit + digit.upper() + "]" if digit.isalpha() else digit for digit in format(byte, "02x"))
                              for byte in character.encode("utf-8"))
            alternatives = [re.escape(character), encoded, encoded.replace("%", "%25")]
            if character == " ":
                alternatives.append(r"\+")
            parts.append("(?:" + "|".join(alternatives) + ")")
        return "".join(parts)

    def clean_known(self, value: str) -> str:
        signature = tuple(sorted((secret for secret in self.secrets if secret), key=lambda item: (-len(item), item)))
        base.require(len(signature) <= 512 and sum(len(secret.encode()) for secret in signature) <= 256 * 1024,
                     "Known configuration secrets exceed the bounded redaction set")
        if signature != self._secret_signature:
            self._secret_signature = signature
            self._secret_regex = re.compile("(?:" + "|".join(self.secret_pattern(secret) for secret in signature) + ")") if signature else None
        return self._secret_regex.sub("[REDACTED_SECRET]", value) if self._secret_regex is not None else value

    def clean_text(self, value: str) -> str:
        def clean_url(url: str) -> str:
            try:
                parsed = urlsplit(url)
                authority = "[REDACTED_USERINFO]@" + parsed.netloc.rsplit("@", 1)[1] if "@" in parsed.netloc else parsed.netloc
                query = []
                for part in re.split(r"[&;]", parsed.query):
                    if not part:
                        continue
                    name, separator, _item = part.partition("=")
                    query.append(self.clean_known(name) + "=[REDACTED_QUERY]" if separator else "[REDACTED_QUERY]")
                return urlunsplit((parsed.scheme, self.clean_known(authority), self.clean_known(parsed.path), "&".join(query),
                                   "[REDACTED_FRAGMENT]" if parsed.fragment else ""))
            except ValueError:
                return "[REDACTED_URL]"
        whole = decoded_url(value)
        value = clean_url(whole) if whole is not None else URL_PATTERN.sub(lambda match: clean_url(match.group()), value)
        value = self.clean_known(value)
        value = re.sub(r"(?i)((?:api_key|access_token|token|password)=)[^&\s\"]+", r"\1[REDACTED_SECRET]", value)
        for before, after in ((str(base.DATA), "/reference-data"), (str(base.ROOT), "/reference-configuration-private"),
                              ("/opt/goby-fixtures", "/reference-fixtures"), (str(base.RUNTIME), "/reference-runtime")):
            value = value.replace(before, after)
        return value

    def sanitize(self, value: object, key: str = "") -> object:
        public = self.public_paths(value) if not key else set()
        def clean(node: object, field: str, path: tuple, inherited: bool = False) -> object:
            sensitive = inherited or sensitive_field(field) and path not in public
            if type(node) is bool and normalized(field) in PUBLIC_BOOLEAN_FLAGS and not inherited:
                sensitive = False
            if isinstance(node, dict):
                result = {}
                descriptor = header_descriptor(node, field)
                for name, item in node.items():
                    base.require(isinstance(name, str), "Configuration dictionary key is not a string")
                    cleaned_name = self.clean_text(name)
                    base.require(cleaned_name not in result, "Sanitized dictionary key collision")
                    force = sensitive or header_field(field) and sensitive_header(name) or (
                        descriptor is not None and name == descriptor[1] and sensitive_header(node[descriptor[0]]))
                    result[cleaned_name] = clean(item, name, path + (name,), force)
                return result
            if isinstance(node, (list, tuple)):
                if header_field(field) and all(isinstance(pair, (list, tuple)) and len(pair) == 2 for pair in node):
                    return [[self.clean_text(str(name)), "[REDACTED_SECRET]" if item and (sensitive or sensitive_header(str(name)))
                             else clean(item, str(name), path + (index, 1), sensitive)] for index, (name, item) in enumerate(node)]
                return [clean(item, field, path + (index,), sensitive) for index, item in enumerate(node)]
            if sensitive and node is not None and node != "":
                return "[REDACTED_SECRET]"
            return self.clean_text(node) if isinstance(node, str) else node
        return clean(value, key, ())

    def write(self, label: str, value: dict) -> None:
        base.require(re.fullmatch(r"[a-z0-9-]+", label), "Unsafe capture label")
        value.setdefault("reference", {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": base.dt.datetime.now(base.dt.timezone.utc).isoformat()})
        # Preserve the original response even if privacy classification fails.
        base.save(base.RAW / (base.PREFIX + label + ".json"), value)
        self.collect_secrets(value)
        exported = self.sanitize(value)
        text = json.dumps(exported, indent=2, ensure_ascii=False) + "\n"
        base.require(not any(secret and secret in text for secret in self.secrets), "A known secret survived configuration export")
        base.save(base.EXPORT / (base.PREFIX + label + ".json"), text)

    def authorize(self, method: str, path: str, token: str, body: object, identity: str) -> None:
        parsed = urlsplit(path)
        base.require(not parsed.scheme and not parsed.netloc and not parsed.fragment and path.startswith("/emby/") and
                     "\\" not in path and len(path.encode()) <= 2048 and not any(ord(char) < 32 for char in path), "Unexpected reference API path")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(values) == 1 for values in query.values()), "Duplicate query fields are prohibited")
        issued = {row["AccessToken"] for row in self.logins.values()}
        base.require(not token or token in issued and token not in self.forbidden_tokens, "HTTP credential is not newly acknowledged and owned")
        route, allowed = parsed.path, False
        if method == "GET" and not identity and body is MISSING:
            if route in {*CONFIGURATION_ROUTES.values(), "/emby/System/Info/Public", "/emby/Users", "/emby/Devices", "/emby/Sessions", "/emby/ScheduledTasks"}:
                allowed = not query
            elif route in {"/emby/Devices/Info", "/emby/Devices/Options"}:
                allowed = set(query) == {"Id"} and query["Id"][0] in self.read_ids
        elif route == "/emby/Users/AuthenticateByName":
            if method == "POST" and not query and not token and identity in device.IDENTITIES and identity not in self.login_attempts:
                account = self.account_credentials[device.IDENTITIES[identity][0]]
                allowed = body == {"Username": account["REFERENCE_USERNAME"], "Pw": account["REFERENCE_PASSWORD"]}
        elif route == "/emby/Sessions/Logout":
            allowed = method == "POST" and not query and not identity and body is MISSING and bool(token)
        base.require(allowed, "Request is outside the read-only configuration and owned-login allowlist")

    def persist(self, label: str, operation, *, cleanup: bool = False) -> object:
        try:
            return operation()
        except Exception as error:
            if not cleanup:
                raise
            failure = {"stage": label, "error": type(error).__name__}
            self.persistence_failures.append(failure)
            self.cleanup_errors.append(failure)
            return None

    def cleanup_step(self, label: str, action) -> object:
        try:
            return action()
        except Exception as error:
            self.cleanup_errors.append({"stage": label, "error": type(error).__name__})
            # Diagnostic storage failure must never skip remaining logouts.
            self.persist("cleanup-diagnostic-" + label, lambda: base.save(base.PRIVATE / ("cleanup-error-" + str(len(self.cleanup_errors)) + ".txt"),
                         label + ": " + type(error).__name__ + "\n"), cleanup=True)
            return None

    def logout(self, identity: str) -> None:
        if identity not in self.logins:
            return
        token = self.logins[identity]["AccessToken"]
        if token not in self.invalid_tokens:
            self.cleanup_step("logout-request-" + identity, lambda: self.request("cleanup-logout-" + identity, "POST", "/emby/Sessions/Logout", token=token))
        # Probe even if the preceding request or its evidence persistence failed.
        status = self.probe_login("cleanup-invalid-" + identity, identity)
        base.require(status == 401 and token in self.invalid_tokens, "Owned credential invalidity is unproven")
        self.logged_out.add(identity)

    def request(self, label: str, method: str, path: str, *, token: str = "", body: object = MISSING,
                identity: str = "") -> tuple[int, object]:
        self.authorize(method, path, token, body, identity)
        cleanup_io = self.finishing and bool(token) and path in {"/emby/Sessions", "/emby/Sessions/Logout"}
        base.require(self.record_count < (device.MAX_REQUESTS if self.finishing else device.MAIN_REQUESTS), "HTTP request reserve exhausted")
        remaining = self.deadline - time.monotonic()
        base.require(remaining > 0, "Capture phase deadline expired")
        byte_limit = base.MAX_TOTAL if self.finishing else base.MAX_TOTAL - 8 * 1024 * 1024
        limit = min(base.MAX_BODY, byte_limit - self.charged_bytes)
        base.require(limit > 0, "Capture wire-byte reserve exhausted")
        headers, metadata = {"Accept": "application/json"}, self.metadata(identity) if identity else None
        if metadata:
            headers["Authorization"] = "Emby " + ", ".join(name + '=\"' + value + '\"' for name, value in metadata.items())
        if token:
            headers["X-Emby-Token"] = token
        wire = None
        if body is not MISSING:
            headers["Content-Type"] = "application/json"
            wire = json.dumps(body, separators=(",", ":")).encode()
            base.require(len(wire) <= 4096, "Owned login request body exceeds its bound")
        request = {"method": method, "path": path, "headers": headers, "body": None if body is MISSING else body,
                   "bodyPresent": body is not MISSING, "clientMetadata": metadata}
        mutation = None
        if method != "GET":
            mutation = {"label": label, "request": request, "responseStatus": None, "acknowledgedLogin": False, "transportFailure": None}
            self.mutations.append(mutation)
            self.persist("mutation-intent-" + label, lambda: base.save(base.PRIVATE / "mutations" / (str(len(self.mutations)).zfill(3) + "-intent.json"), request),
                         cleanup=cleanup_io)
        if identity:
            self.login_attempts.add(identity)
            self.login_statuses[identity] = None
        self.record_count += 1
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=min(5, remaining))
        content, response_headers, status, reason, version, failure = b"", [], None, None, None, None
        complete = False
        signal.setitimer(signal.ITIMER_REAL, min(15, remaining))
        try:
            connection.request(method, path, wire, headers)
            response = connection.getresponse()
            status, response_headers, reason, version = response.status, response.getheaders(), response.reason, response.version
            content = response.read(limit)
            complete = response.isclosed() or response.length == 0
            declared = [value for name, value in response_headers if name.lower() == "content-length"]
            if declared:
                complete = complete and len(set(declared)) == 1 and declared[0].isdigit() and int(declared[0]) == len(content)
        except Exception as error:
            failure = type(error).__name__
            if isinstance(error, http.client.IncompleteRead):
                content = error.partial[:limit]
            if mutation is not None:
                mutation["transportFailure"] = failure
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        self.total += len(content)
        self.charged_bytes += len(content) if complete else limit
        self.incomplete_count += int(not complete)
        wire_path = base.PRIVATE / "wire" / (base.PREFIX + label + ".json")
        self.wire_records[label] = {"path": str(wire_path), "sha256": None, "completeHTTP": complete,
                                   "responseBytes": len(content), "persisted": False}
        exportable = True
        try:
            text = content.decode("utf-8", errors="strict")
            try:
                result, kind = json.loads(text), "json"
            except json.JSONDecodeError:
                result, kind = text, "text"
        except UnicodeDecodeError:
            # Arbitrary non-text payloads are retained privately. They cannot
            # be exported as unchecked base64 that could conceal credentials.
            result, kind = None, "private-binary"
            exportable = False
            failure = failure or "NonUTF8ConfigurationPayload"
        if mutation is not None:
            mutation["responseStatus"] = status
        if identity:
            self.login_statuses[identity] = status
        if identity and isinstance(result, dict) and isinstance(result.get("AccessToken"), str) and result["AccessToken"]:
            acknowledged = result["AccessToken"]
            self.secrets.add(acknowledged)
            if acknowledged not in self.forbidden_tokens:
                self.logins[identity] = result
                mutation["acknowledgedLogin"] = True
            else:
                self.cleanup_errors.append({"stage": "login", "identity": identity, "error": "ExistingCredentialReturned"})
        # Track acknowledged credentials and protected-route results before any
        # fallible wire, acknowledgement, export, hash or console persistence.
        if complete and path == "/emby/Sessions" and token:
            for name, login in self.logins.items():
                if login["AccessToken"] == token:
                    if status == 401:
                        self.invalid_tokens.add(token)
                        self.invalid_login_proofs[name] = {"label": label, "status": 401}
                    else:
                        self.invalid_tokens.discard(token)
                        self.invalid_login_proofs.pop(name, None)
        if path == "/emby/Sessions/Logout" and token:
            for name, login in self.logins.items():
                if login["AccessToken"] == token:
                    self.logout_statuses[name] = status
        if identity and isinstance(result, dict) and result.get("AccessToken"):
            self.persist("login-ack-" + label, lambda: base.save(base.PRIVATE / (identity + "-login-response.json"), result), cleanup=cleanup_io)
        def persist_wire() -> str:
            base.save(wire_path, {"request": request, "requestBodyBase64": base64.b64encode(wire or b"").decode(),
                "responseStatus": status, "responseHeaders": response_headers, "responseReason": reason, "responseHTTPVersion": version,
                "responseBodyBase64": base64.b64encode(content).decode(), "completeHTTP": complete,
                "transportFailure": failure, "unmeasuredWireBytesUpperBound": 0 if complete else limit})
            return base.digest(wire_path)
        wire_hash = self.persist("wire-" + label, persist_wire, cleanup=cleanup_io)
        self.wire_records[label] = {"path": str(wire_path), "sha256": wire_hash, "completeHTTP": complete,
                                   "responseBytes": len(content), "persisted": wire_hash is not None}
        self.persist("record-" + label, lambda: self.write(label, {"request": request, "response": {"status": status, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content),
                            "failureType": failure, "bodyExportable": exportable, "privateWireSha256": wire_hash}}), cleanup=cleanup_io)
        self.persist("console-" + label, lambda: print(json.dumps({"capture": base.PREFIX + label, "status": status, "bodyType": kind,
                     "bytes": len(content), "completeHTTP": complete}), flush=True), cleanup=cleanup_io)
        base.require(complete and exportable, "Response was not a complete bounded UTF-8 capture; private wire evidence is retained")
        return status, result

    def capture_configuration(self, label: str, name: str, token: str, *, baseline: bool = False, final: bool = False) -> None:
        route = CONFIGURATION_ROUTES[name]
        status, body = self.request(label, "GET", route, token=token)
        response = {"status": status, "body": copy.deepcopy(body)}
        self.configuration_observations.append({"capture": label, "configurationName": name, "path": route, "status": status,
            "responseShape": type(body).__name__, "properties": list(body) if isinstance(body, dict) else None})
        if baseline:
            self.configuration_baseline[name] = response
        if final:
            before = self.configuration_baseline[name]
            if not same_value(before, response):
                changed = []
                if isinstance(before.get("body"), dict) and isinstance(body, dict):
                    changed = sorted(key for key in set(before["body"]) | set(body) if (key in before["body"]) != (key in body) or
                                     not same_value(before["body"].get(key), body.get(key)))
                self.configuration_changes.append({"configurationName": name, "beforeStatus": before["status"], "afterStatus": status,
                    "changedTopLevelProperties": changed, "attribution": "Observed during GET-only configuration traffic"})

    def capture(self) -> None:
        status, public = self.request("public-before", "GET", "/emby/System/Info/Public")
        base.require(status == 200 and isinstance(public, dict) and public.get("Version") == "4.9.5.0" and public.get("Id") == EXPECTED_SERVER_ID,
                     "Original reference identity differs")
        self.login("control")
        rows = self.devices("devices-baseline")
        self.control_id = self.find_owned(rows, "control", "Control")
        self.old_devices = {row["Id"]: row for row in rows if row["Id"] != self.control_id}
        base.require(set(self.old_devices) == self.previous_device_ids and all(not str(row.get("ReportedDeviceId", "")).startswith(device.DEVICE_PREFIX)
                     for row in self.old_devices.values()), "Old device membership changed or reserved metadata collides")
        self.read_ids.update(self.old_devices)
        base.save(base.PRIVATE / "old-devices.json", self.old_devices)
        for index, device_id in enumerate(sorted(self.old_devices)):
            status, body = self.request("old-options-before-" + str(index), "GET", self.route("/Options", device_id), token=self.admin())
            self.old_options[device_id] = {"status": status, "body": body}
        base.save(base.PRIVATE / "old-options.json", self.old_options)
        status, hidden = self.request("hidden-server-info-before", "GET", self.route("/Info", HIDDEN_DEVICE_ID), token=self.admin())
        base.require(status == 200 and isinstance(hidden, dict) and hidden.get("Id") == HIDDEN_DEVICE_ID and
                     hidden.get("ReportedDeviceId") == EXPECTED_SERVER_ID, "Protected hidden server device differs")
        self.hidden_device_before = hidden
        status, options = self.request("hidden-server-options-before", "GET", self.route("/Options", HIDDEN_DEVICE_ID), token=self.admin())
        self.hidden_options_before = {"status": status, "body": options}
        status, users = self.request("users-baseline", "GET", "/emby/Users", token=self.admin())
        base.require(status == 200 and isinstance(users, list), "User baseline is unavailable")
        self.old_users = users
        base.save(base.PRIVATE / "old-users.json", users)
        self.login("viewer")
        self.devices("devices-after-viewer")
        for identity in device.IDENTITIES:
            base.require(self.probe_login(identity + "-protected-before", identity) == 200, "Owned login cannot establish ordinary authenticated access")
        self.task_list("task-definitions-before", {}, baseline=True)
        base.save(base.PRIVATE / "task-baseline.json", self.task_baseline)
        for name in CONFIGURATION_ROUTES:
            self.capture_configuration("admin-configuration-before-" + name, name, self.admin(), baseline=True)
        self.configuration_complete = len(self.configuration_baseline) == len(CONFIGURATION_ROUTES)
        base.save(base.PRIVATE / "configuration-baseline.json", self.configuration_baseline)
        for actor, token in (("anonymous", ""), ("viewer", self.logins["viewer"]["AccessToken"])):
            for name in CONFIGURATION_ROUTES:
                self.capture_configuration(actor + "-configuration-" + name, name, token)

    def compare_tasks(self) -> None:
        rows = self.task_list("task-definitions-final", {}, baseline=False)
        current = {row["Id"]: row for row in rows}
        self.checks["taskDefinitionMembershipUnchanged"] = set(current) == set(self.task_baseline)
        for task_id, before in self.task_baseline.items():
            after = current.get(task_id, {})
            config_before = {key: value for key, value in before.items() if key not in read.RUNTIME_TASK_FIELDS}
            config_after = {key: value for key, value in after.items() if key not in read.RUNTIME_TASK_FIELDS}
            if not same_value(config_before, config_after):
                self.task_configuration_changes.append({"taskId": task_id})
            if any((key in before) != (key in after) or not same_value(before.get(key), after.get(key)) for key in read.RUNTIME_TASK_FIELDS):
                self.task_runtime_changes.append({"taskId": task_id, "attribution": "Observed without task mutation requests"})
        self.checks["taskConfigurationUnchanged"] = not self.task_configuration_changes

    def compare_configurations(self) -> None:
        for name in self.configuration_baseline:
            self.capture_configuration("admin-configuration-final-" + name, name, self.admin(), final=True)
        self.checks["configurationReadsUnchanged"] = not self.configuration_changes

    def finish(self) -> None:
        self.finishing = True
        self.deadline = time.monotonic() + device.PHASE_SECONDS
        if "control" in self.logins:
            if self.configuration_baseline:
                self.cleanup_step("compare-configuration", self.compare_configurations)
            if self.task_ids and not self.task_bound_exceeded:
                self.cleanup_step("compare-task-definitions", self.compare_tasks)
            if self.old_devices is not None:
                self.cleanup_step("compare-devices", self.compare_devices)
            if self.old_users is not None:
                self.cleanup_step("compare-users", self.compare_users)
            self.cleanup_step("sessions-before-logout", lambda: self.request("sessions-before-owned-logouts", "GET", "/emby/Sessions", token=self.admin()))
        self.deadline = max(self.deadline, time.monotonic() + 60)
        for identity in ("viewer", "control"):
            self.cleanup_step("logout-" + identity, lambda identity=identity: self.logout(identity))
        self.checks["allAcknowledgedLoginsInvalid"] = set(self.logins) == set(self.invalid_login_proofs)
        self.checks["allAttemptedLoginsAcknowledgedOrRejected"] = all(name in self.logins or self.login_statuses.get(name) in {400, 401, 403}
                                                                    for name in self.login_attempts)
        self.checks["originalReferenceProcessUnchanged"] = self.cleanup_step("reference-process-audit", preconditions) == self.pid
        self.checks["mainGobyProcessAndBinaryUnchanged"] = self.cleanup_step("main-process-audit", main_identity) == self.main_before
        self.checks["allPriorEvidenceSourcesAndPrivateFilesUnchanged"] = self.cleanup_step("prior-file-audit", self.snapshot) == self.baseline
        self.checks["onlyOwnedLoginLogoutMutations"] = all(row["request"]["method"] == "POST" and row["request"]["path"] in
            {"/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"} for row in self.mutations)
        self.cleanup_step("mutation-journal", lambda: base.save(base.PRIVATE / "mutation-results.json", self.mutations))
        redaction_ok = True
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            exported = base.EXPORT / path.name
            try:
                base.private_file(path)
                base.private_file(exported)
                redaction_ok = redaction_ok and same_value(self.sanitize(json.loads(path.read_text())), json.loads(exported.read_text()))
            except Exception:
                redaction_ok = False
        self.checks["newRedactionAuditPassed"] = redaction_ok
        self.checks["privateWireEvidenceUnchanged"] = len(self.wire_records) == self.record_count and all(
            row["persisted"] and base.digest(Path(row["path"])) == row["sha256"] for row in self.wire_records.values())
        self.cleanup_ok = not self.cleanup_errors and all(self.checks.values())
        self.write("audit", {"kind": "capture-audit-observation", "referencePID": self.pid, "serverId": EXPECTED_SERVER_ID,
            "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok, "checks": self.checks, "cleanupErrors": self.cleanup_errors,
            "preservedOldRecords": OLD_RECORDS, "preservedOldRecordFiles": len(self.baseline["records"]),
            "preservedOldMediaFiles": len(self.baseline["media"]), "preservedOldPrivateFiles": len(self.baseline["privateFiles"]),
            "historicallyRemovedPathsNotRequiredToExist": self.historical_missing_paths, "retiredOwnedMediaFiles": 512,
            "mainGobyBefore": self.main_before, "oldListedDeviceCount": None if self.old_devices is None else len(self.old_devices),
            "oldDeviceActivityDifferences": self.old_device_date_changes, "attributedUserActivityDifferences": self.user_date_changes,
            "ownedLoginAttempts": sorted(self.login_attempts), "ownedLoginStatuses": self.login_statuses,
            "ownedLoginInvalidityProofs": self.invalid_login_proofs, "ownedLoginLogoutStatuses": self.logout_statuses,
            "configurationObservations": self.configuration_observations, "configurationChanges": self.configuration_changes,
            "completeConfigurationBaseline": self.configuration_complete, "namedConfigurationEvidence": NAMED_CONFIGURATION_EVIDENCE,
            "taskConfigurationChanges": self.task_configuration_changes, "taskRuntimeChanges": self.task_runtime_changes,
            "configurationMutationRequests": 0, "scheduledTaskMutationRequests": 0, "deviceMutationRequests": 0,
            "applicationKeyRequests": 0, "existingCredentialHTTPRequests": 0, "mediaOrEncoderRequests": 0, "sourceWrites": 0,
            "httpAttempts": self.record_count, "incompleteHTTP": self.incomplete_count, "wireBytes": self.total,
            "wireByteBudgetCharged": self.charged_bytes, "privateWireRecords": len(self.wire_records),
            "persistenceFailures": self.persistence_failures,
            "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "redactionPolicy": "Sensitive fields and known credentials are redacted deeply, including dictionary keys; URL userinfo/query values/fragments are private; configuration Key fields have no public exception",
            "urlPercentDecodeLayers": 2, "urlDecodeBudgetExhaustion": "Entire values with residual URL delimiter encodings are redacted",
            "limits": {"taskDefinitions": MAX_TASKS, "httpAttempts": device.MAX_REQUESTS, "mainHTTPAttempts": device.MAIN_REQUESTS,
                       "responseBytes": base.MAX_BODY, "totalResponseBytes": base.MAX_TOTAL, "phaseSeconds": device.PHASE_SECONDS},
            "dependencies": [{"sourceFile": "reference-scheduled-tasks.py", "sha256": READ_SOURCE_SHA256},
                             {"sourceFile": "reference-devices.py", "sha256": DEVICE_SOURCE_SHA256},
                             {"sourceFile": "reference-api-keys.py", "sha256": BASE_SOURCE_SHA256}]})
        base.require(self.cleanup_ok, "Cleanup or preservation proof is incomplete; inspect private evidence")


def main() -> None:
    signal.signal(signal.SIGALRM, device.deadline_expired)
    def terminate(_signum: int, _frame: object) -> None:
        raise InterruptedError("Configuration capture received a termination signal")
    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, terminate)
    recorder = Recorder(preconditions())
    failure = None
    try:
        recorder.capture()
    except Exception as error:
        failure = type(error).__name__
        recorder.capture_failure = failure
        base.save(base.PRIVATE / "failure.txt", failure + ": " + str(error) + "\n")
    finally:
        try:
            recorder.finish()
        except Exception as error:
            base.save(base.PRIVATE / "cleanup-failure.txt", type(error).__name__ + ": " + str(error) + "\n")
            print(json.dumps({"capture": base.PREFIX, "cleanup": "failed", "failureType": type(error).__name__}), flush=True)
            raise SystemExit(1)
    print(json.dumps({"capture": base.PREFIX, "result": "complete" if failure is None else "partial", "failureType": failure,
                      "cleanup": recorder.cleanup_ok}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
