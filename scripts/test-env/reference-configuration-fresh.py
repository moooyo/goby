#!/usr/bin/env python3
"""Capture bounded configuration writes on one attested disposable instance.

Only two fresh logins, one uniquely proved application credential, and the
fixed configuration experiments below may mutate state. Every ordinary case
starts from a proved complete baseline and ends with proved restoration.
Full empty/minimal configuration objects, network/path/hardware changes, and
library/media/task/device/user mutations are outside the request allowlist.
Run only through authorized root SSH beside the pinned recorder dependencies.
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
        raise RuntimeError("A pinned configuration recorder dependency differs")
spec = importlib.util.spec_from_file_location("fresh_configuration_reference", Path(__file__).with_name("reference-configuration.py"))
configuration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(configuration)
device, base = configuration.device, configuration.base
MISSING = device.MISSING
WORK = Path("/opt/goby-test/exec-work-m5g")
EVIDENCE = WORK / "emby-configuration-fresh-m5g-20260910-01"
DATA = WORK / "emby-configuration-fresh-data-01"
UNIT = "goby-emby-configuration-fresh-m5g-20260910-01.service"
MARKER = "goby-emby-configuration-fresh-m5g-20260910-01-owned-v1"
PORT, HTTPS_PORT, OLD_RECORDS = 18099, 18499, 2051
MANIFEST = EVIDENCE / "private/manifest.json"
OPERATOR_SOURCE = Path(__file__).with_name("prepare-configuration-fresh.py")
OPERATOR_SOURCE_SHA256 = "6aa2f03e2db5fd0028159c55bf6fc2cd22bb33597a9c4cb7166e40b2b14329d8"
MAX_BODY, MAX_REQUEST_BODY = 256 * 1024, 32 * 1024
MAX_TOTAL, MAIN_TOTAL = 8 * 1024 * 1024, 6 * 1024 * 1024
MAX_REQUESTS, MAIN_REQUESTS = 280, 240
MAIN_SECONDS, CLEANUP_SECONDS = 360, 180
OLD_UNITS = {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3570491}
OLD_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
CLIENT_PREFIX = "Goby Configuration Fresh M5g 20260910 01 Capture "
DEVICE_PREFIX = "goby-configuration-fresh-m5g-20260910-01-capture-"
APP_NAME = "Goby Configuration Fresh M5g 20260910 01 Owned Credential"
NAMES = {"first": "Goby Configuration M5g Owned First", "second": "Goby Configuration M5g Owned Second"}
MESSAGE = "Goby Configuration M5g Owned Inactive Maintenance Message"
WORDS = ["goby-m5g-owned-first", "goby-m5g-owned-second"]
INVALID_INTEGER = "goby-m5g-invalid-integer"
UNKNOWN = "goby-configuration-fresh-m5g-20260910-01-unknown"
TOTAL_ROUTE = "/emby/System/Configuration"
PARTIAL_ROUTE = TOTAL_ROUTE + "/Partial"
ENCODING_ROUTE = TOTAL_ROUTE + "/encoding"
UNKNOWN_ROUTE = TOTAL_ROUTE + "/" + UNKNOWN
KEYS_ROUTE = "/emby/Auth/Keys"
KEYS_PAGE = KEYS_ROUTE + "?StartIndex=0&Limit=200"
PUBLIC_ROUTE = "/emby/System/Info/Public"
LIBRARIES_ROUTE = "/emby/Library/VirtualFolders/Query"
CONFIG_ROUTES = {"total": TOTAL_ROUTE, "encoding": ENCODING_ROUTE, "unknown": UNKNOWN_ROUTE}
IDENTITIES = {"control": "admin", "viewer": "viewer"}
TOTAL_FIELDS = {"ServerName", "MaintenanceModeMessage", "SortRemoveWords", "ImageExtractionTimeoutMs"}
RUNTIME_TASK_FIELDS = configuration.read.RUNTIME_TASK_FIELDS
_operator = None
base.__file__ = __file__
base.DATA, base.RUNTIME = DATA, EVIDENCE / "runtime"
base.ROOT = EVIDENCE / "runtime/configuration-capture"
base.PRIVATE, base.RAW, base.EXPORT = base.ROOT / "private", base.ROOT / "private/raw", base.ROOT / "export"
base.PREFIX = "configuration-fresh-m5g-"
base.MARKER = "goby-reference-configuration-fresh-m5g-owned-v1"


def operator_module():
    global _operator
    base.require(re.fullmatch(r"[0-9a-f]{64}", OPERATOR_SOURCE_SHA256) is not None and
                 base.digest(OPERATOR_SOURCE) == OPERATOR_SOURCE_SHA256,
                 "Fresh configuration operator is not the frozen reviewed source")
    if _operator is None:
        specification = importlib.util.spec_from_file_location("fresh_configuration_operator_authority", OPERATOR_SOURCE)
        _operator = importlib.util.module_from_spec(specification)
        specification.loader.exec_module(_operator)
    base.require(_operator.ROOT == EVIDENCE and _operator.DATA == DATA and _operator.UNIT == UNIT and
                 _operator.PORT == PORT and _operator.MARKER == MARKER and _operator.SANITIZER_SOURCES == SANITIZER_SOURCES,
                 "Fresh configuration operator constants differ")
    return _operator


def load_manifest() -> dict:
    op = operator_module()
    manifest = op.load(MANIFEST)
    base.require(manifest.get("state") == "READY" and manifest.get("marker") == MARKER and
                 manifest.get("unit") == UNIT and manifest.get("port") == PORT and manifest.get("httpsPort") == HTTPS_PORT and
                 manifest.get("programData") == str(DATA) and manifest.get("evidenceRoot") == str(EVIDENCE) and
                 manifest.get("operatorSha256") == OPERATOR_SOURCE_SHA256 and manifest.get("sanitizerSources") == SANITIZER_SOURCES and
                 manifest.get("allFreshProgramDataOwned") is True and manifest.get("oldPreservationVerified") is True and
                 manifest.get("bootstrapCredentialsRevoked") is True and
                 manifest.get("bootstrapCredentialsInvalidity") == {"admin": 401, "viewer": 401} and
                 manifest.get("adminCredentialsFile") == str(EVIDENCE / "private/admin-credentials.env") and
                 manifest.get("viewerCredentialsFile") == str(EVIDENCE / "private/viewer-credentials.env"),
                 "The fresh configuration READY manifest is incomplete or differs")
    base.require(isinstance(manifest.get("serverId"), str) and manifest["serverId"] and manifest["serverId"] != op.OLD_SERVER_ID and
                 manifest.get("bootstrapLibraries") == [] and manifest.get("bootstrapLibraryMutationRequests") == 0 and
                 manifest.get("bootstrapTaskMutationRequests") == 0 and manifest.get("bootstrapApplicationKeyRequests") == 0,
                 "The fresh fixture is not an independently initialized empty instance")
    base.require(len(manifest.get("oldBaseline", {}).get("records", {})) == OLD_RECORDS * 2 and
                 len(manifest["oldBaseline"].get("media", {})) == 240,
                 "The protected configuration corpus has unexpected membership")
    return manifest


def preconditions() -> dict:
    base.require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
                 "Run only through authorized root SSH")
    op, manifest = operator_module(), load_manifest()
    op.common_preconditions(host=False)
    expected = op.load(op.IDENTITY)
    op.same_new_identity(expected)
    base.require(all(manifest.get(name) == value for name, value in expected.items()), "READY does not match the attested fresh process")
    op.check_old_services(manifest["oldServices"])
    op.verify_baseline(manifest["oldBaseline"])
    namespaces = {row["networkNamespace"] for row in manifest["oldServices"].values()}
    base.require(expected["networkNamespace"] not in namespaces and expected["networkNamespace"] != os.readlink("/proc/1/ns/net"),
                 "The fresh namespace overlaps a protected authority")
    if os.readlink("/proc/self/ns/net") != expected["networkNamespace"]:
        os.execvp("nsenter", ["nsenter", "-t", str(expected["pid"]), "-n", sys.executable, "-B", str(Path(__file__).resolve())])
    base.require(os.readlink("/proc/self/ns/net") == expected["networkNamespace"], "The recorder is outside its fresh namespace")
    return manifest


def changed_fields(before: dict, after: dict) -> set[str]:
    return {name for name in set(before) | set(after) if (name in before) != (name in after) or
            not configuration.same_value(before.get(name), after.get(name))}


class Recorder(configuration.Recorder):
    def __init__(self, manifest: dict) -> None:
        self.manifest = manifest
        self.authority = operator_module().load(operator_module().IDENTITY)
        self.pid = self.authority["pid"]
        self.started = time.monotonic()
        self.deadline = self.started + MAIN_SECONDS
        self.finishing = False
        self.secrets, self.forbidden_tokens = set(), set()
        self._secret_signature, self._secret_regex = None, None
        self.logins, self.account_credentials = {}, {}
        self.login_attempts, self.invalid_tokens, self.logged_out = set(), set(), set()
        self.login_statuses, self.invalid_login_proofs, self.logout_statuses = {}, {}, {}
        self.record_count = self.total = self.charged_bytes = self.incomplete_count = 0
        self.mutations, self.persistence_failures, self.cleanup_errors, self.observations = [], [], [], []
        self.wire_records, self.checks = {}, {}
        self.capture_failure = None
        self.cleanup_ok = False
        self.pending = None
        self.writes_blocked = False
        self.block_reasons = []
        self.baselines, self.latest, self.config_dirty = {}, {}, set()
        self.public_baseline = None
        self.restored = False
        self.current_case = None
        self.case_results, self.restoration_results = [], []
        self.restore_attempts = {}
        self.key_baseline_empty = False
        self.key_create_attempted = False
        self.owned_key = None
        self.key_ownership_failed = False
        self.key_invalidity = None
        self.key_revoke_attempted = False
        self.control_user_id = None
        self.old_users, self.task_baseline, self.device_baseline = None, {}, []
        self.baseline = self.snapshot()
        base.require(not base.ROOT.exists() and not base.ROOT.is_symlink(), "Refusing to overwrite a prior fresh capture")
        os.umask(0o077)
        base.ROOT.mkdir(mode=0o700)
        for folder in (base.PRIVATE, base.RAW, base.EXPORT, base.PRIVATE / "wire", base.PRIVATE / "mutations"):
            folder.mkdir(mode=0o700)
        base.save(base.ROOT / ".goby-managed", base.MARKER + "\n")
        base.save(base.PRIVATE / "baseline.json", self.baseline)
        for account in ("admin", "viewer"):
            path = Path(manifest[account + "CredentialsFile"])
            base.private_file(path)
            values = dict(line.split("=", 1) for line in path.read_text().splitlines())
            base.require(set(values) == {"REFERENCE_USERNAME", "REFERENCE_PASSWORD"} and values["REFERENCE_USERNAME"],
                         "Fresh account credential fields differ")
            self.account_credentials[account] = values
            if values["REFERENCE_PASSWORD"]:
                self.secrets.add(values["REFERENCE_PASSWORD"])

    def snapshot(self) -> dict:
        op = operator_module()
        op.verify_baseline(self.manifest["oldBaseline"])
        old = self.manifest["oldBaseline"]
        raw = sorted((EVIDENCE / "private/raw").glob("*.json"))
        exported = sorted((EVIDENCE / "export").glob("*.json"))
        count = self.manifest.get("setupRecordCount")
        base.require(type(count) is int and 0 < count <= 128 and len(raw) == len(exported) == count and
                     {path.name for path in raw} == {path.name for path in exported}, "Fresh setup capture membership differs")
        private = []
        for path in (EVIDENCE / "private").rglob("*"):
            base.require(not path.is_symlink(), "Fresh setup private evidence contains a symlink")
            if path.is_file():
                private.append(path)
        base.require(len(private) < 1024 and sum(path.stat().st_size for path in private) < 32 * 1024 * 1024,
                     "Fresh setup private baseline exceeds its bound")
        def credentials(value: object) -> None:
            if isinstance(value, dict):
                for name, child in value.items():
                    if name.lower() == "accesstoken" and isinstance(child, str) and child:
                        self.forbidden_tokens.add(child)
                        self.secrets.add(child)
                    else:
                        credentials(child)
            elif isinstance(value, list):
                for child in value:
                    credentials(child)
        for path in [*private, *exported]:
            base.private_file(path)
        for path in raw:
            credentials(json.loads(path.read_text()))
        return {"records": {**old["records"], **{str(path): base.digest(path) for path in [*raw, *exported]}},
                "media": old["media"], "privateFiles": {**old["privateFiles"], **{str(path): base.digest(path) for path in private}},
                "retainedHistoricalFiles": old.get("retainedHistoricalFiles", {}),
                "historicalRemovedPaths": old.get("historicalRemovedPaths", []), "retiredOwnedSources": old.get("retiredOwnedSources", {})}

    def check_authority(self) -> None:
        op = operator_module()
        op.same_new_identity(self.authority)
        base.require(os.readlink("/proc/self/ns/net") == self.authority["networkNamespace"] and
                     self.authority["networkNamespace"] not in {row["networkNamespace"] for row in self.manifest["oldServices"].values()},
                     "A request attempted to leave the attested fresh namespace")

    @staticmethod
    def metadata(identity: str) -> dict:
        base.require(identity in IDENTITIES, "Unknown fresh login identity")
        return {"Client": CLIENT_PREFIX + identity.title(), "DeviceId": DEVICE_PREFIX + identity,
                "Device": "Fresh Configuration " + identity.title(), "Version": "0.1.0"}

    def admin(self) -> str:
        return self.logins["control"]["AccessToken"]

    def block_writes(self, reason: str) -> None:
        self.writes_blocked = True
        self.pending = None
        self.block_reasons.append(reason)

    def actor_token(self, actor: str) -> str:
        if actor == "anonymous":
            return ""
        if actor == "application":
            base.require(self.owned_key is not None and not self.key_ownership_failed, "Application credential is not uniquely owned")
            return self.owned_key["AccessToken"]
        base.require(actor in self.logins, "Fresh ordinary credential is unavailable")
        return self.logins[actor]["AccessToken"]

    def validate_payload(self, area: str, body: object, actor: str, *, restore: bool = False) -> None:
        base.require(set(self.baselines) == {"total", "encoding", "unknown"}, "Complete configuration baseline is unavailable")
        baseline = self.baselines["encoding" if area in {"encoding", "unknown"} else "total"]["body"]
        base.require(isinstance(body, dict) and isinstance(baseline, dict), "Configuration writes require an object baseline")
        if restore or actor != "control" or area == "unknown":
            base.require(configuration.same_value(body, baseline), "Permission/restore input must equal the complete baseline")
            if area == "unknown":
                unknown = self.baselines["unknown"]
                base.require(self.current_case == "unknown-named-post" and unknown.get("status") in {404, 500} and
                             type(unknown.get("body")) is str, "Unknown named configuration lacks its explicit absence proof")
            return
        if area in {"total", "encoding"}:
            allowed = {"ServerName"} if area == "total" else {"TranscodingMaxWidth"}
            base.require(body.keys() == baseline.keys() and changed_fields(baseline, body) <= allowed,
                         "Full configuration input changes unsupported fields or omits original fields")
            if area == "total":
                base.require(type(body["ServerName"]) is str and body["ServerName"] in {*NAMES.values(), baseline["ServerName"]},
                             "Full name is outside the fixed cases")
            else:
                width = body["TranscodingMaxWidth"]
                base.require(type(width) is int and width in {640, 1280, baseline["TranscodingMaxWidth"]}, "Encoding width is outside the fixed cases")
            return
        base.require(area == "partial", "Unknown configuration write area")
        if configuration.same_value(body, baseline):
            return
        base.require(body and set(body) <= TOTAL_FIELDS, "Partial input changes an unsupported field")
        for field, value in body.items():
            if field == "ServerName":
                base.require(value is None or type(value) is str and value in {"", *NAMES.values()}, "Partial name is outside the fixed cases")
            elif field == "MaintenanceModeMessage":
                base.require(value == MESSAGE and self.baselines["total"]["body"].get("IsInMaintenanceMode") is False,
                             "Maintenance text requires the inactive baseline")
            elif field == "SortRemoveWords":
                base.require(any(configuration.same_value(value, candidate) for candidate in (WORDS, WORDS[:1], [])),
                             "Sort words are outside the fixed cases")
            else:
                base.require(value == INVALID_INTEGER and set(body) == {"ServerName", "ImageExtractionTimeoutMs"},
                             "The malformed integer is outside its atomicity case")

    def approve_configuration(self, area: str, actor: str, body: dict, *, mime: str = "application/json", restore: bool = False) -> None:
        base.require(self.pending is None and self.current_case is not None and mime in {"application/json", "application/octet-stream"},
                     "A configuration approval is pending or has no bounded case")
        base.require(not self.writes_blocked or restore and self.finishing, "Ordinary configuration writes are blocked")
        base.require(area in {"total", "partial", "encoding", "unknown"}, "Unknown configuration operation")
        base.require(not restore or area in {"total", "encoding"} and actor == "control", "Only control can restore baseline configuration")
        base.require(mime == "application/json" or area == "encoding", "Octet-stream is restricted to the named no-op case")
        self.validate_payload(area, body, actor, restore=restore)
        if mime == "application/octet-stream":
            base.require(configuration.same_value(body, self.baselines["encoding"]["body"]), "MIME experiment must be a no-op")
        path = {"total": TOTAL_ROUTE, "partial": PARTIAL_ROUTE, "encoding": ENCODING_ROUTE, "unknown": UNKNOWN_ROUTE}[area]
        self.pending = {"kind": "configuration-restore" if restore else "configuration-case", "area": area,
                        "case": self.current_case, "method": "POST", "path": path,
                        "actor": actor, "actorToken": self.actor_token(actor), "body": copy.deepcopy(body), "mime": mime}

    def authorize(self, method: str, path: str, token: str, body: object, identity: str, mime: str = "application/json") -> None:
        parsed = urlsplit(path)
        base.require(not parsed.scheme and not parsed.netloc and not parsed.fragment and path.startswith("/emby/") and
                     "\\" not in path and len(path.encode()) <= 2048 and not any(ord(char) < 32 for char in path), "Unexpected fresh API path")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(values) == 1 for values in query.values()), "Duplicate query fields are prohibited")
        owned = {row["AccessToken"] for row in self.logins.values()}
        if self.owned_key is not None and not self.key_ownership_failed:
            owned.add(self.owned_key["AccessToken"])
        base.require(not token or token in owned and token not in self.forbidden_tokens, "HTTP token is not acknowledged and owned")
        route, allowed = parsed.path, False
        if method == "GET" and body is MISSING and not identity:
            allowed = path in {*CONFIG_ROUTES.values(), PUBLIC_ROUTE, "/emby/Users", "/emby/Devices", "/emby/Sessions",
                               "/emby/ScheduledTasks", LIBRARIES_ROUTE, KEYS_ROUTE, KEYS_PAGE}
            if route == KEYS_ROUTE:
                allowed = allowed and token == self.admin()
            if self.owned_key is not None and token == self.owned_key["AccessToken"]:
                allowed = allowed and (path in {TOTAL_ROUTE, ENCODING_ROUTE} or self.finishing and path == "/emby/Sessions")
        elif route == "/emby/Users/AuthenticateByName" and method == "POST" and not query and not token:
            if identity in IDENTITIES and identity not in self.login_attempts:
                account = self.account_credentials[IDENTITIES[identity]]
                allowed = body == {"Username": account["REFERENCE_USERNAME"], "Pw": account["REFERENCE_PASSWORD"]}
        elif route == "/emby/Sessions/Logout":
            allowed = method == "POST" and not query and not identity and body is MISSING and token in {
                row["AccessToken"] for row in self.logins.values()}
        elif self.pending is not None and not identity:
            plan = self.pending
            allowed = method == plan["method"] and path == plan["path"] and token == plan["actorToken"] and mime == plan["mime"] and (
                body is MISSING and plan["body"] is MISSING or configuration.same_value(body, plan["body"]))
            if plan["kind"].startswith("configuration"):
                allowed = allowed and (not self.writes_blocked or self.finishing and plan["kind"] == "configuration-restore")
                if allowed:
                    expected_paths = {"total": TOTAL_ROUTE, "partial": PARTIAL_ROUTE, "encoding": ENCODING_ROUTE, "unknown": UNKNOWN_ROUTE}
                    restore = plan["kind"] == "configuration-restore"
                    allowed = (plan["kind"] in {"configuration-case", "configuration-restore"} and method == "POST" and
                        plan.get("case") == self.current_case and self.current_case is not None and
                        path == expected_paths.get(plan["area"]) and token == self.actor_token(plan["actor"]) and
                        (not restore or plan["actor"] == "control" and plan["area"] in {"total", "encoding"}) and
                        (mime == "application/json" or mime == "application/octet-stream" and plan["area"] == "encoding" and
                         configuration.same_value(body, self.baselines["encoding"]["body"])))
                    if allowed:
                        self.validate_payload(plan["area"], body, plan["actor"], restore=restore)
            elif plan["kind"] == "application-create":
                allowed = (allowed and self.key_baseline_empty and self.restored and not self.writes_blocked and
                    not self.key_create_attempted and self.owned_key is None and not self.key_ownership_failed and
                    method == "POST" and path == KEYS_ROUTE + "?" + urlencode({"App": APP_NAME}) and
                    token == self.admin() and body is MISSING and mime == "application/json")
            elif plan["kind"] == "application-revoke":
                allowed = (allowed and self.finishing and self.owned_key is not None and not self.key_ownership_failed and
                    not self.key_revoke_attempted and method == "DELETE" and
                    path == KEYS_ROUTE + "/" + quote(self.owned_key["AccessToken"], safe="") and
                    token == self.admin() and body is MISSING and mime == "application/json")
            else:
                allowed = False
        base.require(allowed, "Request is outside the bounded fresh configuration allowlist")

    def acknowledge_key_listing(self, status: int | None, result: object, complete: bool, path: str) -> None:
        if not self.key_create_attempted or self.owned_key is not None or self.key_ownership_failed or path != KEYS_PAGE or status != 200 or not complete:
            return
        if not isinstance(result, dict) or not isinstance(result.get("Items"), list):
            self.key_ownership_failed = True
            return
        rows = result["Items"]
        if len(rows) != 1 or type(result.get("TotalRecordCount")) is not int or result["TotalRecordCount"] != 1:
            self.key_ownership_failed = True
            return
        row = rows[0]
        token = row.get("AccessToken") if isinstance(row, dict) else None
        if not isinstance(row, dict) or row.get("AppName") != APP_NAME or not isinstance(token, str) or not token or token in self.forbidden_tokens or token in {
                login["AccessToken"] for login in self.logins.values()}:
            self.key_ownership_failed = True
            return
        # Unique membership and the reserved application label prove ownership.
        # Retain the token before any further DTO checks or persistence can fail.
        self.owned_key = copy.deepcopy(row)
        self.secrets.add(token)

    def request(self, label: str, method: str, path: str, *, token: str = "", body: object = MISSING,
                identity: str = "", mime: str = "application/json") -> tuple[int, object]:
        self.check_authority()
        self.authorize(method, path, token, body, identity, mime)
        base.require(self.record_count < (MAX_REQUESTS if self.finishing else MAIN_REQUESTS), "Fresh HTTP request reserve exhausted")
        remaining = self.deadline - time.monotonic()
        base.require(remaining > 0, "Fresh capture phase deadline expired")
        limit = min(MAX_BODY, (MAX_TOTAL if self.finishing else MAIN_TOTAL) - self.charged_bytes)
        base.require(limit > 0, "Fresh wire-byte reserve exhausted")
        cleanup_io = self.finishing
        metadata = self.metadata(identity) if identity else None
        headers = {"Accept": "application/json"}
        if metadata:
            headers["Authorization"] = "Emby " + ", ".join(name + '=\"' + value + '\"' for name, value in metadata.items())
        if token:
            headers["X-Emby-Token"] = token
        wire = None
        if body is not MISSING:
            headers["Content-Type"] = mime
            wire = json.dumps(body, separators=(",", ":")).encode()
            base.require(len(wire) <= (4096 if identity else MAX_REQUEST_BODY), "Fresh request body exceeds its bound")
        plan = self.pending if method != "GET" and not identity and path != "/emby/Sessions/Logout" else None
        request = {"method": method, "path": path, "headers": headers, "body": None if body is MISSING else body,
                   "bodyPresent": body is not MISSING, "clientMetadata": metadata,
                   "experiment": self.current_case, "approvedMutationKind": plan["kind"] if plan else None}
        mutation = None
        if method != "GET":
            mutation = {"label": label, "request": request, "responseStatus": None, "acknowledgedLogin": False, "transportFailure": None}
            self.mutations.append(mutation)
            self.persist("intent-" + label, lambda: base.save(base.PRIVATE / "mutations" / (str(len(self.mutations)).zfill(3) + "-intent.json"), request), cleanup=cleanup_io)
        if identity:
            self.login_attempts.add(identity)
            self.login_statuses[identity] = None
        self.record_count += 1
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=min(5, remaining))
        content, response_headers, status, reason, version, failure = b"", [], None, None, None, None
        complete = False
        signal.setitimer(signal.ITIMER_REAL, min(15, remaining))
        try:
            if plan:
                self.pending = None
                if plan["kind"].startswith("configuration"):
                    if plan["kind"] == "configuration-restore":
                        marker = plan["case"] + ":" + plan["area"]
                        base.require(self.restore_attempts.get(marker, 0) == 0, "Refusing an automatic restoration retry")
                        # Consume an actual attempt only after intent storage and
                        # transport construction, immediately before dispatch.
                        self.restore_attempts[marker] = 1
                    self.config_dirty.add("encoding" if plan["area"] == "encoding" else "total")
                    self.restored = False
                elif plan["kind"] == "application-create":
                    self.key_create_attempted = True
                elif plan["kind"] == "application-revoke":
                    self.key_revoke_attempted = True
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
        exportable = True
        try:
            text = content.decode("utf-8", errors="strict")
            try:
                result, kind = json.loads(text), "json"
            except json.JSONDecodeError:
                result, kind = text, "text"
        except UnicodeDecodeError:
            result, kind, exportable = None, "private-binary", False
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
                self.cleanup_errors.append({"stage": "login", "error": "ExistingCredentialReturned"})
        self.acknowledge_key_listing(status, result, complete, path)
        if complete and path == "/emby/Sessions" and token:
            for name, login in self.logins.items():
                if login["AccessToken"] == token:
                    if status == 401:
                        self.invalid_tokens.add(token)
                        self.invalid_login_proofs[name] = {"label": label, "status": 401}
                    else:
                        self.invalid_tokens.discard(token)
                        self.invalid_login_proofs.pop(name, None)
            if self.owned_key is not None and self.owned_key["AccessToken"] == token:
                self.key_invalidity = {"label": label, "status": status} if status == 401 else None
        if path == "/emby/Sessions/Logout":
            for name, login in self.logins.items():
                if login["AccessToken"] == token:
                    self.logout_statuses[name] = status
        if not complete or not exportable:
            self.block_writes("incomplete-or-nontext-HTTP-" + label)
        if identity and isinstance(result, dict) and result.get("AccessToken"):
            self.persist("login-ack-" + label, lambda: base.save(base.PRIVATE / (identity + "-login-response.json"), result), cleanup=cleanup_io)
        wire_path = base.PRIVATE / "wire" / (base.PREFIX + label + ".json")
        self.wire_records[label] = {"path": str(wire_path), "sha256": None, "completeHTTP": complete, "persisted": False}
        def persist_wire() -> str:
            base.save(wire_path, {"request": request, "requestBodyBase64": base64.b64encode(wire or b"").decode(),
                "responseStatus": status, "responseHeaders": response_headers, "responseReason": reason, "responseHTTPVersion": version,
                "responseBodyBase64": base64.b64encode(content).decode(), "completeHTTP": complete, "transportFailure": failure,
                "unmeasuredWireBytesUpperBound": 0 if complete else limit})
            return base.digest(wire_path)
        wire_hash = self.persist("wire-" + label, persist_wire, cleanup=cleanup_io)
        self.wire_records[label].update({"sha256": wire_hash, "persisted": wire_hash is not None})
        self.persist("record-" + label, lambda: self.write(label, {"request": request,
            "response": {"status": status, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content),
                            "failureType": failure, "bodyExportable": exportable, "privateWireSha256": wire_hash},
            "transportAuthority": {"unit": UNIT, "pid": self.pid, "startTicks": self.authority["startTicks"],
                                   "networkNamespace": self.authority["networkNamespace"], "port": PORT}}), cleanup=cleanup_io)
        self.persist("console-" + label, lambda: print(json.dumps({"capture": base.PREFIX + label, "status": status,
                     "bodyType": kind, "bytes": len(content), "completeHTTP": complete}), flush=True), cleanup=cleanup_io)
        base.require(complete and exportable, "Response was not a complete bounded UTF-8 capture")
        return status, result

    def login(self, identity: str) -> None:
        account = self.account_credentials[IDENTITIES[identity]]
        status, result = self.request(identity + "-login", "POST", "/emby/Users/AuthenticateByName", identity=identity,
                                     body={"Username": account["REFERENCE_USERNAME"], "Pw": account["REFERENCE_PASSWORD"]})
        base.require(status == 200 and identity in self.logins and isinstance(result.get("User"), dict) and
                     result["User"].get("Name") == account["REFERENCE_USERNAME"] and
                     result["User"].get("Policy", {}).get("IsAdministrator") is (identity == "control"),
                     "Acknowledged fresh login does not match its expected account")

    def read_state(self, label: str, *, baseline: bool = False, allowed_total: set[str] = frozenset(),
                   allowed_encoding: set[str] = frozenset(), exact: bool = False) -> dict:
        state = {}
        try:
            for area in ("total", "encoding"):
                status, body = self.request(label + "-" + area, "GET", CONFIG_ROUTES[area], token=self.admin())
                base.require(status == 200 and isinstance(body, dict) and body, "Full administrator configuration read is unavailable")
                state[area] = {"status": status, "body": copy.deepcopy(body)}
            status, public = self.request(label + "-public", "GET", PUBLIC_ROUTE)
            base.require(status == 200 and isinstance(public, dict) and public.get("Version") == "4.9.5.0" and
                         public.get("Id") == self.manifest["serverId"], "Fresh public instance identity changed")
            state["public"] = public
            if baseline:
                total, encoding = state["total"]["body"], state["encoding"]["body"]
                base.require(type(total.get("ServerName")) is str and total["ServerName"] and
                             total.get("HttpServerPortNumber") == PORT and total.get("HttpsPortNumber") == HTTPS_PORT and
                             total.get("EnableHttps") is False and total.get("EnableRemoteAccess") is False and
                             total.get("EnableUPnP") is False and total.get("EnableAutoUpdate") is False and
                             total.get("EnableAutomaticRestart") is False and total.get("IsInMaintenanceMode") is False and
                             isinstance(total.get("SortRemoveWords"), list) and
                             "ImageExtractionTimeoutMs" in total and type(encoding.get("TranscodingMaxWidth")) is int,
                             "Fresh baseline lacks the required safe transport and experiment fields")
                self.baselines.update({area: state[area] for area in ("total", "encoding")})
                self.public_baseline = copy.deepcopy(public)
            else:
                changes = {area: changed_fields(self.baselines[area]["body"], state[area]["body"]) for area in ("total", "encoding")}
                base.require(changes["total"] <= allowed_total and changes["encoding"] <= allowed_encoding,
                             "A configuration experiment changed an unapproved property")
                base.require(state["total"]["body"].get("IsInMaintenanceMode") is False, "Maintenance mode became active")
                if exact:
                    base.require(not changes["total"] and not changes["encoding"] and
                                 public.get("ServerName") == self.public_baseline.get("ServerName"), "Configuration restoration is not exact")
            self.latest = copy.deepcopy(state)
            self.observations.append({"label": label, "totalChanged": sorted(changed_fields(self.baselines["total"]["body"], state["total"]["body"])),
                                      "encodingChanged": sorted(changed_fields(self.baselines["encoding"]["body"], state["encoding"]["body"])),
                                      "publicServerName": public.get("ServerName"), "stableServerId": True, "exactRequired": exact})
            return state
        except Exception:
            self.block_writes("configuration-read-or-preservation-" + label)
            raise

    def begin_case(self, name: str) -> None:
        base.require(self.current_case is None and self.restored and not self.writes_blocked and self.pending is None,
                     "A new experiment requires the previous exact restoration")
        self.current_case = name

    def post_configuration(self, label: str, area: str, actor: str, body: dict, *, mime: str = "application/json",
                           total_fields: set[str] = frozenset(), encoding_fields: set[str] = frozenset()) -> tuple[int, object]:
        self.approve_configuration(area, actor, body, mime=mime)
        path, token = self.pending["path"], self.pending["actorToken"]
        status, response = self.request(label, "POST", path, token=token, body=body, mime=mime)
        self.read_state(label + "-after", allowed_total=total_fields, allowed_encoding=encoding_fields)
        self.case_results.append({"case": self.current_case, "operation": area, "actor": actor, "capture": label, "status": status})
        return status, response

    def restore_configuration(self, label: str) -> None:
        base.require(self.current_case is not None and set(self.baselines) == {"total", "encoding", "unknown"}, "No complete restoration baseline")
        self.pending = None
        # Read and verify the exact process at the fixed port before considering
        # a restoration write. Never follow a redirected or changed listener.
        status, public = self.request(label + "-authority", "GET", PUBLIC_ROUTE)
        base.require(status == 200 and isinstance(public, dict) and public.get("Id") == self.manifest["serverId"], "Restoration authority is unavailable")
        for area in ("total", "encoding"):
            if area not in self.config_dirty:
                continue
            marker = self.current_case + ":" + area
            if self.restore_attempts.get(marker, 0) != 0:
                # A dispatched restoration is never resent after an uncertain
                # result. Cleanup may only prove that it already took effect.
                base.require(self.finishing, "Refusing an automatic restoration retry")
                observed_status, observed = self.request(label + "-prior-restore-" + area, "GET", CONFIG_ROUTES[area], token=self.admin())
                base.require(observed_status == 200 and configuration.same_value(observed, self.baselines[area]["body"]),
                             "A dispatched restoration is not independently proved complete")
                continue
            body = copy.deepcopy(self.baselines[area]["body"])
            self.approve_configuration(area, "control", body, restore=True)
            status, _ = self.request(label + "-restore-" + area, "POST", CONFIG_ROUTES[area], token=self.admin(), body=body)
            base.require(status in {200, 204}, "Exact baseline restoration was not acknowledged")
        self.read_state(label + "-restored", exact=True)
        status, unknown = self.request(label + "-unknown", "GET", UNKNOWN_ROUTE, token=self.admin())
        base.require(configuration.same_value({"status": status, "body": unknown}, self.baselines["unknown"]), "Unknown configuration membership changed")
        self.config_dirty.clear()
        self.restored = True
        self.restoration_results.append({"case": self.current_case, "capture": label, "exact": True})
        self.current_case = None

    def approve_key_create(self) -> None:
        base.require(self.pending is None and self.restored and not self.writes_blocked and self.key_baseline_empty and
                     not self.key_create_attempted and self.owned_key is None and not self.key_ownership_failed,
                     "Application credential creation lacks the empty owned baseline")
        self.pending = {"kind": "application-create", "method": "POST", "path": KEYS_ROUTE + "?" + urlencode({"App": APP_NAME}),
                        "actorToken": self.admin(), "body": MISSING, "mime": "application/json"}

    def revoke_key(self) -> None:
        if not self.key_create_attempted:
            return
        if self.owned_key is None and not self.key_ownership_failed:
            self.request("cleanup-application-discover", "GET", KEYS_PAGE, token=self.admin())
        base.require(self.owned_key is not None and not self.key_ownership_failed, "The new application credential is not uniquely proved")
        token = self.owned_key["AccessToken"]
        if not self.key_revoke_attempted:
            self.pending = {"kind": "application-revoke", "method": "DELETE", "path": KEYS_ROUTE + "/" + quote(token, safe=""),
                            "actorToken": self.admin(), "body": MISSING, "mime": "application/json"}
            self.cleanup_step("application-revoke-request", lambda: self.request("cleanup-application-revoke", "DELETE", self.pending["path"], token=self.admin()))
        self.cleanup_step("application-invalidity-request", lambda: self.request("cleanup-application-invalid", "GET", "/emby/Sessions", token=token))
        base.require(self.key_invalidity is not None and self.key_invalidity["status"] == 401, "Application credential invalidity is unproven")
        status, listed = self.request("cleanup-application-list", "GET", KEYS_PAGE, token=self.admin())
        base.require(status == 200 and isinstance(listed, dict) and listed.get("Items") == [] and type(listed.get("TotalRecordCount")) is int and listed["TotalRecordCount"] == 0,
                     "The fresh application credential list is not empty after revocation")

    def capture(self) -> None:
        self.login("control")
        self.login("viewer")
        for identity in IDENTITIES:
            base.require(self.probe_login(identity + "-protected-before", identity) == 200, "Fresh login cannot establish protected access")
        status, users = self.request("users-before", "GET", "/emby/Users", token=self.admin())
        base.require(status == 200 and isinstance(users, list) and len(users) == 2, "Fresh account membership differs")
        self.old_users = copy.deepcopy(users)
        status, devices = self.request("devices-before", "GET", "/emby/Devices", token=self.admin())
        base.require(status == 200 and isinstance(devices, dict) and isinstance(devices.get("Items"), list), "Fresh device baseline unavailable")
        self.device_baseline = copy.deepcopy(devices["Items"])
        status, tasks = self.request("task-definitions-before", "GET", "/emby/ScheduledTasks", token=self.admin())
        base.require(status == 200 and isinstance(tasks, list) and 0 < len(tasks) <= 64, "Fresh task baseline unavailable")
        self.task_baseline = {row["Id"]: {name: value for name, value in row.items() if name not in RUNTIME_TASK_FIELDS} for row in tasks}
        status, libraries = self.request("libraries-before", "GET", LIBRARIES_ROUTE, token=self.admin())
        base.require(status == 200 and isinstance(libraries, dict) and libraries.get("Items") == [], "Configuration experiments require an empty library instance")
        self.read_state("baseline", baseline=True)
        status, unknown = self.request("unknown-before", "GET", UNKNOWN_ROUTE, token=self.admin())
        base.require(status in {404, 500} and isinstance(unknown, str), "Reserved unknown configuration is not proved absent")
        self.baselines["unknown"] = {"status": status, "body": unknown}
        base.save(base.PRIVATE / "configuration-baselines.json", self.baselines)
        self.restored = True
        for path, label in ((KEYS_ROUTE, "applications-before"), (KEYS_PAGE, "applications-before-paged")):
            status, listed = self.request(label, "GET", path, token=self.admin())
            base.require(status == 200 and isinstance(listed, dict) and listed.get("Items") == [] and type(listed.get("TotalRecordCount")) is int and listed["TotalRecordCount"] == 0,
                         "Fresh application credential baseline is not completely empty")
        self.key_baseline_empty = True

        self.begin_case("full-server-name")
        body = {**copy.deepcopy(self.baselines["total"]["body"]), "ServerName": NAMES["first"]}
        self.post_configuration("full-name", "total", "control", body, total_fields={"ServerName"})
        self.restore_configuration("full-name")

        self.begin_case("partial-flat-merge")
        self.post_configuration("partial-flat-both", "partial", "control", {"ServerName": NAMES["first"], "MaintenanceModeMessage": MESSAGE},
                                total_fields={"ServerName", "MaintenanceModeMessage"})
        self.post_configuration("partial-flat-name-only", "partial", "control", {"ServerName": NAMES["second"]},
                                total_fields={"ServerName", "MaintenanceModeMessage"})
        self.restore_configuration("partial-flat")

        self.begin_case("partial-array-replacement")
        for label, words in (("two", WORDS), ("one", WORDS[:1]), ("empty", [])):
            self.post_configuration("partial-words-" + label, "partial", "control", {"SortRemoveWords": list(words)}, total_fields={"SortRemoveWords"})
        self.restore_configuration("partial-words")

        for label, value in (("empty", ""), ("null", None)):
            self.begin_case("partial-name-" + label)
            self.post_configuration("partial-name-" + label, "partial", "control", {"ServerName": value}, total_fields={"ServerName"})
            self.restore_configuration("partial-name-" + label)

        self.begin_case("partial-invalid-atomicity")
        self.post_configuration("partial-invalid-atomicity", "partial", "control",
                                {"ServerName": NAMES["first"], "ImageExtractionTimeoutMs": INVALID_INTEGER},
                                total_fields={"ServerName", "ImageExtractionTimeoutMs"})
        self.restore_configuration("partial-invalid")

        self.begin_case("named-encoding-width")
        before = self.baselines["encoding"]["body"]["TranscodingMaxWidth"]
        body = {**copy.deepcopy(self.baselines["encoding"]["body"]), "TranscodingMaxWidth": 640 if before == 1280 else 1280}
        self.post_configuration("encoding-width", "encoding", "control", body, encoding_fields={"TranscodingMaxWidth"})
        self.restore_configuration("encoding-width")

        for label, mime in (("json", "application/json"), ("octet-stream", "application/octet-stream")):
            self.begin_case("named-mime-" + label)
            self.post_configuration("encoding-mime-" + label, "encoding", "control", copy.deepcopy(self.baselines["encoding"]["body"]), mime=mime)
            self.restore_configuration("encoding-mime-" + label)

        for actor in ("anonymous", "viewer"):
            for area in ("total", "partial", "encoding"):
                self.begin_case(actor + "-permission-" + area)
                body = copy.deepcopy(self.baselines["encoding" if area == "encoding" else "total"]["body"])
                self.post_configuration(actor + "-permission-" + area, area, actor, body)
                self.restore_configuration(actor + "-permission-" + area)

        self.begin_case("unknown-named-post")
        self.post_configuration("unknown-named-post", "unknown", "control", copy.deepcopy(self.baselines["encoding"]["body"]))
        status, unknown = self.request("unknown-named-after", "GET", UNKNOWN_ROUTE, token=self.admin())
        base.require(configuration.same_value({"status": status, "body": unknown}, self.baselines["unknown"]), "Unknown named configuration changed")
        self.restore_configuration("unknown-named")

        self.approve_key_create()
        self.request("application-create", "POST", self.pending["path"], token=self.admin())
        self.request("application-created-list", "GET", KEYS_PAGE, token=self.admin())
        base.require(self.owned_key is not None and not self.key_ownership_failed, "New application credential ownership was not established")
        for area in ("total", "encoding"):
            self.request("application-read-" + area, "GET", CONFIG_ROUTES[area], token=self.actor_token("application"))
        for area in ("total", "partial", "encoding"):
            self.begin_case("application-permission-" + area)
            body = copy.deepcopy(self.baselines["encoding" if area == "encoding" else "total"]["body"])
            self.post_configuration("application-permission-" + area, area, "application", body)
            self.restore_configuration("application-permission-" + area)

    def finish(self) -> None:
        self.finishing = True
        self.deadline = time.monotonic() + CLEANUP_SECONDS
        self.pending = None
        if "control" in self.logins:
            if set(self.baselines) == {"total", "encoding", "unknown"}:
                if self.current_case is None:
                    self.current_case = "final-cleanup"
                self.cleanup_step("configuration-restoration", lambda: self.restore_configuration("cleanup-configuration"))
            self.cleanup_step("application-retirement", self.revoke_key)
            self.cleanup_step("empty-libraries", self.compare_libraries)
            self.cleanup_step("task-preservation", self.compare_task_definitions)
            self.cleanup_step("user-preservation", self.compare_fresh_users)
        self.deadline = max(self.deadline, time.monotonic() + 45)
        for identity in ("viewer", "control"):
            self.cleanup_step("logout-" + identity, lambda identity=identity: self.logout(identity))
        self.checks["allAcknowledgedLoginsInvalid"] = set(self.logins) == set(self.invalid_login_proofs)
        self.checks["applicationCredentialInvalidOrNeverAttempted"] = not self.key_create_attempted or self.key_invalidity is not None
        self.checks["configurationRestoredExactly"] = self.restored and not self.config_dirty
        self.checks["allAttemptedLoginsAccountedFor"] = all(name in self.logins or self.login_statuses.get(name) in {400, 401, 403} for name in self.login_attempts)
        self.checks["freshAuthorityUnchanged"] = self.cleanup_step("fresh-process", lambda: self.check_authority() or True) is True
        self.checks["protectedServicesUnchanged"] = self.cleanup_step("protected-services", lambda: operator_module().check_old_services(self.manifest["oldServices"]) or True) is True
        self.checks["allPriorEvidenceAndSourcesUnchanged"] = self.cleanup_step("preserved-evidence", self.snapshot) == self.baseline
        self.cleanup_step("mutation-journal", lambda: base.save(base.PRIVATE / "mutation-results.json", self.mutations))
        redaction = True
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            try:
                base.private_file(path)
                base.private_file(base.EXPORT / path.name)
                redaction = redaction and configuration.same_value(self.sanitize(json.loads(path.read_text())), json.loads((base.EXPORT / path.name).read_text()))
            except Exception:
                redaction = False
        self.checks["newRedactionAuditPassed"] = redaction
        self.checks["privateWireEvidenceUnchanged"] = len(self.wire_records) == self.record_count and all(
            row["persisted"] and base.digest(Path(row["path"])) == row["sha256"] for row in self.wire_records.values())
        self.cleanup_ok = not self.cleanup_errors and all(self.checks.values())
        self.write("audit", {"kind": "capture-audit-observation", "unit": UNIT, "referencePID": self.pid,
            "serverId": self.manifest["serverId"], "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok,
            "checks": self.checks, "cleanupErrors": self.cleanup_errors, "persistenceFailures": self.persistence_failures,
            "preservedOldRecords": OLD_RECORDS, "preservedRecordFilesIncludingSetup": len(self.baseline["records"]),
            "preservedSourceFiles": len(self.baseline["media"]), "preservedPrivateFiles": len(self.baseline["privateFiles"]),
            "configurationObservations": self.observations, "configurationCases": self.case_results,
            "restorationProofs": self.restoration_results, "ordinaryWritesBlocked": self.writes_blocked, "writeBlockReasons": self.block_reasons,
            "ownedLoginAttempts": sorted(self.login_attempts), "ownedLoginStatuses": self.login_statuses,
            "ownedLoginInvalidityProofs": self.invalid_login_proofs, "ownedLoginLogoutStatuses": self.logout_statuses,
            "applicationCreationAttempted": self.key_create_attempted, "applicationUniquelyOwned": self.owned_key is not None and not self.key_ownership_failed,
            "applicationInvalidityProof": self.key_invalidity, "applicationRevocationAttempted": self.key_revoke_attempted,
            "scheduledTaskMutationRequests": 0, "libraryMutationRequests": 0, "deviceMutationRequests": 0,
            "mediaOrEncoderRequests": 0, "sourceWrites": 0, "httpAttempts": self.record_count, "incompleteHTTP": self.incomplete_count,
            "wireBytes": self.total, "wireByteBudgetCharged": self.charged_bytes, "privateWireRecords": len(self.wire_records),
            "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "limits": {"mainRequests": MAIN_REQUESTS, "allRequests": MAX_REQUESTS, "requestBodyBytes": MAX_REQUEST_BODY,
                       "responseBodyBytes": MAX_BODY, "totalResponseBytes": MAX_TOTAL, "mainSeconds": MAIN_SECONDS, "cleanupSeconds": CLEANUP_SECONDS},
            "sanitizerSources": SANITIZER_SOURCES, "operatorSha256": OPERATOR_SOURCE_SHA256,
            "retainedFreshHistory": "Revoked login/device/application history belongs to the separately attested disposable instance; its operator owns teardown"})
        base.require(self.cleanup_ok, "Fresh configuration cleanup or preservation proof is incomplete")

    def compare_libraries(self) -> None:
        status, libraries = self.request("libraries-final", "GET", LIBRARIES_ROUTE, token=self.admin())
        self.checks["freshLibrariesRemainEmpty"] = status == 200 and isinstance(libraries, dict) and libraries.get("Items") == []

    def compare_task_definitions(self) -> None:
        status, rows = self.request("task-definitions-final", "GET", "/emby/ScheduledTasks", token=self.admin())
        base.require(status == 200 and isinstance(rows, list), "Final task definitions are unavailable")
        current = {row["Id"]: {name: value for name, value in row.items() if name not in RUNTIME_TASK_FIELDS} for row in rows}
        self.checks["freshTaskDefinitionsUnchanged"] = configuration.same_value(current, self.task_baseline)

    def compare_fresh_users(self) -> None:
        status, users = self.request("users-final", "GET", "/emby/Users", token=self.admin())
        base.require(status == 200 and isinstance(users, list) and self.old_users is not None, "Final fresh users are unavailable")
        fields = {"LastLoginDate", "LastActivityDate"}
        before = {row["Id"]: {name: value for name, value in row.items() if name not in fields} for row in self.old_users}
        after = {row["Id"]: {name: value for name, value in row.items() if name not in fields} for row in users}
        self.checks["freshUsersAndPoliciesUnchanged"] = configuration.same_value(before, after)


def main() -> None:
    signal.signal(signal.SIGALRM, device.deadline_expired)
    def terminate(_signum: int, _frame: object) -> None:
        raise InterruptedError("Fresh configuration capture received a termination signal")
    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, terminate)
    recorder = Recorder(preconditions())
    failure = None
    try:
        recorder.capture()
    except Exception as error:
        failure = recorder.capture_failure = type(error).__name__
        recorder.block_writes("capture-failed")
        recorder.persist("capture-failure", lambda: base.save(base.PRIVATE / "failure.txt", failure + ": " + str(error) + "\n"), cleanup=True)
    finally:
        try:
            recorder.finish()
        except Exception as error:
            recorder.persist("cleanup-failure", lambda: base.save(base.PRIVATE / "cleanup-failure.txt", type(error).__name__ + ": " + str(error) + "\n"), cleanup=True)
            print(json.dumps({"capture": base.PREFIX, "result": "partial", "cleanup": "failed", "failureType": type(error).__name__}), flush=True)
            raise SystemExit(1)
    print(json.dumps({"capture": base.PREFIX, "result": "complete" if failure is None else "partial", "failureType": failure, "cleanup": recorder.cleanup_ok}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
