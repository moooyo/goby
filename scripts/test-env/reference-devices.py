#!/usr/bin/env python3
"""Capture owned user-device contracts from the isolated Emby reference.

Run only through authorized root SSH on test-env beside reference-api-keys.py.
No existing credential is used for HTTP. Only the explicitly listed fresh logins and devices
uniquely identified by their reserved reported IDs and client names may be
mutated. Camera uploads, key changes, playback, scans, and source writes are
outside the request allowlist. Failed or partial captures are never overwritten.
GOBY_DEVICE_REFERENCE_RUN accepts only 1 or 2. Run 2 preserves every record
from the partial first capture and adds spaced logins, an owned relogin, and numeric missing-ID
observations. The first run's original namespace remains the default.
"""

from __future__ import annotations

import http.client
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import sys
import time
from urllib.parse import parse_qs, urlencode, urlsplit

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("key_reference", Path(__file__).with_name("reference-api-keys.py"))
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
base.__file__ = __file__


def selected_run(value: str) -> int:
    base.require(value in {"1", "2"}, "GOBY_DEVICE_REFERENCE_RUN must be canonical 1 or 2")
    return int(value)


REFERENCE_RUN = selected_run(os.environ.get("GOBY_DEVICE_REFERENCE_RUN", "1"))
PREVIOUS_RECORDS = 1128 if REFERENCE_RUN == 1 else 1281
PREVIOUS = Path("/opt/goby-test/exec-scratch/keys-scope-m5d" if REFERENCE_RUN == 1 else
                "/opt/goby-test/exec-scratch/devices-m5e")
CAPTURE_NAME = "devices-m5e" if REFERENCE_RUN == 1 else "devices-m5e-2"
base.ROOT = Path("/opt/goby-test/exec-scratch") / CAPTURE_NAME
base.PRIVATE = base.ROOT / "private"
base.RAW = base.PRIVATE / "raw"
base.EXPORT = base.ROOT / "export"
base.PREFIX = CAPTURE_NAME + "-"
base.MARKER = "goby-reference-" + CAPTURE_NAME + "-owned-v1"
base.MAX_BODY = 256 * 1024
base.MAX_TOTAL = 32 * 1024 * 1024
EXPECTED_PID = 3131777
MAX_OLD_DEVICES = 128
MAX_REQUESTS = 480
MAIN_REQUESTS = 300
PHASE_SECONDS = 360
RUN_LABEL = str(REFERENCE_RUN).zfill(2)
DEVICE_PREFIX = "goby-devices-m5e-20260910-" + RUN_LABEL + "-"
CLIENT_PREFIX = "Goby Devices M5e 20260910 " + RUN_LABEL + " "
CUSTOM_PREFIX = "Goby Devices M5e Owned " if REFERENCE_RUN == 1 else "Goby Devices M5e 02 Owned "
UNKNOWN_ID = DEVICE_PREFIX + "unknown-read-only"
UNKNOWN_NUMERIC_ID = "2147483647"
MISSING = object()
IDENTITIES = {
    "control": ("admin", "control", "Control", "Control Device", "0.1.0"),
    "alpha-admin": ("admin", "alpha", "Alpha", "Alpha Admin Device", "1.0.0"),
    "alpha-viewer": ("viewer", "alpha", "Alpha", "Alpha Viewer Device", "1.0.1"),
    "alpha-other-client": ("viewer", "alpha", "Other", "Alpha Other Device", "2.0.0"),
    "beta-viewer": ("viewer", "beta", "Alpha", "Beta Viewer Device", "3.0.0"),
    "permission-viewer": ("viewer", "permission", "Permission", "Permission Device", "4.0.0"),
}
if REFERENCE_RUN == 2:
    IDENTITIES["alpha-relogin"] = ("viewer", "alpha", "Alpha", "Alpha Relogin Device", "5.0.0")


def preconditions() -> int:
    pid = base.preconditions()
    base.require(pid == EXPECTED_PID, "The explicitly reviewed reference process changed")
    base.require(not base.ROOT.is_symlink(), "Capture root must not be a symlink")
    base.require(base.ROOT.parent.resolve(strict=True) == base.ROOT.parent,
                 "Execution scratch path is not canonical")
    base.require(base.shutil.disk_usage(base.ROOT.parent).free > 96 * 1024 * 1024,
                 "Capture scratch cannot accommodate the bounded private and exported records")
    return pid


def deadline_expired(_signum: int, _frame: object) -> None:
    raise TimeoutError("The bounded HTTP operation expired")


class Recorder(base.Recorder):
    def __init__(self, pid: int) -> None:
        self.started = time.monotonic()
        self.deadline = self.started + PHASE_SECONDS
        self.finishing = False
        self.record_count = 0
        self.charged_bytes = 0
        self.incomplete_count = 0
        self.cleanup_ok = False
        self.login_attempts: set[str] = set()
        self.forbidden_tokens: set[str] = set()
        self.account_credentials: dict[str, dict] = {}
        self.login_statuses: dict[str, int | None] = {}
        self.invalid_tokens: set[str] = set()
        self.invalid_login_proofs: dict[str, dict] = {}
        self.owned_devices: dict[str, dict] = {}
        self.ambiguous_ids: set[str] = set()
        self.approved_write_ids: set[str] = set()
        self.write_proofs: list[dict] = []
        self.pending_write_proof: dict | None = None
        self.acknowledged_device_deletes: dict[str, dict] = {}
        self.device_observations: list[dict] = []
        self.old_devices: dict[str, dict] | None = None
        self.protected_device_aliases: set[str] = set()
        self.old_options: dict[str, dict] = {}
        self.old_users: list[dict] | None = None
        self.control_id: str | None = None
        self.final_devices: list[dict] | None = None
        self.read_ids: set[str] = {UNKNOWN_ID}
        self.mutations: list[dict] = []
        self.cleanup_errors: list[dict] = []
        self.capture_failure: str | None = None
        self.user_date_changes: list[dict] = []
        self.old_device_date_changes: list[dict] = []
        self.alpha_registry_shape: dict | None = None
        self.alpha_relogin_observation: dict | None = None
        self.login_delays: list[dict] = []
        self.prior_capture_observation: dict | None = None
        self.checks: dict[str, bool] = {}
        super().__init__(pid)
        (base.PRIVATE / "mutations").mkdir(mode=0o700)
        # Load both fixed credential inputs before any login. Their existing
        # tokens are forbidden HTTP credentials, even if a response reuses one.
        for account in ("admin", "viewer"):
            values = self.credentials(account)
            self.account_credentials[account] = values
            if values.get("REFERENCE_TOKEN"):
                self.forbidden_tokens.add(values["REFERENCE_TOKEN"])

    def snapshot(self) -> dict:
        prior_path = PREVIOUS / "private/baseline.json"
        base.private_file(prior_path)
        prior = json.loads(prior_path.read_text())
        if REFERENCE_RUN == 2:
            audit_path = PREVIOUS / "private/raw/devices-m5e-audit.json"
            base.private_file(audit_path)
            audit = json.loads(audit_path.read_text())
            base.require(audit.get("cleanupPassed") is True and audit.get("captureFailureType") is not None,
                         "Run 2 requires the preserved partial first run with proven cleanup")
            self.prior_capture_observation = {"recordPrefix": "devices-m5e-", "records": 153,
                                              "outcome": "partial", "cleanupPassed": True,
                                              "failureType": audit["captureFailureType"]}
        paths = [Path(path) for path in prior["records"]]
        for folder in (PREVIOUS / "private/raw", PREVIOUS / "export"):
            paths.extend(folder.glob("*.json"))
        base.require(len(paths) == PREVIOUS_RECORDS * 2 and len(set(paths)) == PREVIOUS_RECORDS * 2,
                     "Expected exactly all " + str(PREVIOUS_RECORDS) + " preceding raw/export pairs")
        media = [Path(path) for path in prior["media"]]
        base.require(len(media) == 240 and len(set(media)) == 240,
                     "Expected exactly 240 known source paths")
        private_roots = {path.parent.parent for path in paths if path.parent.name == "raw"}
        private_paths = sorted({path for folder in private_roots for path in folder.rglob("*") if path.is_file()})
        base.require(sum(path.stat().st_size for path in media) < 32 * 1024 * 1024,
                     "Known source audit exceeds its bound")
        base.require(len(private_paths) < 4096 and
                     sum(path.stat().st_size for path in private_paths) < 128 * 1024 * 1024,
                     "Prior private-file audit exceeds its bound")
        for path in [*paths, *media, *private_paths]:
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode),
                         "Preserved path is not a regular canonical file")
        return {"records": {str(path): base.digest(path) for path in paths},
                "media": {str(path): base.digest(path) for path in media},
                "privateFiles": {str(path): base.digest(path) for path in private_paths}}

    @staticmethod
    def metadata(identity: str) -> dict:
        _account, reported, client, name, version = IDENTITIES[identity]
        return {"Client": CLIENT_PREFIX + client, "DeviceId": DEVICE_PREFIX + reported,
                "Device": name, "Version": version}

    def owned_pair(self, item: dict) -> bool:
        return any(item.get("ReportedDeviceId") == self.metadata(identity)["DeviceId"] and
                   item.get("AppName") == self.metadata(identity)["Client"] for identity in IDENTITIES)

    def authorize(self, method: str, path: str, token: str, body: object, identity: str) -> None:
        parsed = urlsplit(path)
        base.require(not parsed.scheme and not parsed.netloc and not parsed.fragment and
                     path.startswith("/emby/") and "\\" not in path and len(path.encode()) <= 2048 and
                     not any(ord(character) < 32 for character in path),
                     "Request is not a relative reference API path")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(values) == 1 for values in query.values()), "Duplicate query fields are outside the study")
        route = parsed.path
        issued = {login["AccessToken"] for login in self.logins.values()}
        base.require(not token or token in issued and token not in self.forbidden_tokens,
                     "HTTP credential is not acknowledged and owned")
        allowed = False
        if method == "GET" and not identity and body is MISSING:
            if route in {"/emby/System/Info/Public", "/emby/Users", "/emby/Sessions"}:
                allowed = not query
            elif route == "/emby/Devices":
                allowed = not query or set(query) == {"SortOrder"} and query["SortOrder"][0] in {
                    "Ascending", "Descending", "ascending", "invalid"}
            elif route in {"/emby/Devices/Info", "/emby/Devices/Options"}:
                allowed = not query or set(query) == {"Id"} and query["Id"][0] in self.read_ids
        elif route == "/emby/Users/AuthenticateByName":
            if method == "POST" and not query and not token and identity in IDENTITIES and identity not in self.login_attempts:
                account = self.account_credentials[IDENTITIES[identity][0]]
                allowed = body == {"Username": account["REFERENCE_USERNAME"], "Pw": account["REFERENCE_PASSWORD"]}
        elif route == "/emby/Sessions/Logout":
            allowed = method == "POST" and not query and not identity and body is MISSING and bool(token)
        elif set(query) == {"Id"} and not identity:
            target = query["Id"][0]
            owned = target in self.approved_write_ids and target in self.owned_devices and target not in self.ambiguous_ids and target not in self.protected_device_aliases and target != self.control_id and (
                self.old_devices is not None and target not in self.old_devices)
            proof = self.pending_write_proof
            proved = owned and proof is not None and proof["targetId"] == target
            if route == "/emby/Devices/Options":
                allowed = method == "POST" and proved and proof["matchedOwnedIdentity"] and isinstance(body, dict) and (
                    body == {} or set(body) == {"CustomName"} and
                    (body["CustomName"] is None or body["CustomName"] == "" or
                     isinstance(body["CustomName"], str) and body["CustomName"].startswith(CUSTOM_PREFIX) and
                     len(body["CustomName"].encode()) <= 128))
            elif route in {"/emby/Devices", "/emby/Devices/Delete"}:
                allowed = proved and (proof["matchedOwnedIdentity"] or proof["previouslyProvedTargetMissing"]) and body is MISSING and (
                    method == "DELETE" and route == "/emby/Devices" or
                    method == "POST" and route == "/emby/Devices/Delete")
        base.require(allowed, "Request is outside the explicit owned device allowlist")

    def request(self, label: str, method: str, path: str, *, token: str = "", body: object = MISSING,
                identity: str = "") -> tuple[int, object]:
        self.authorize(method, path, token, body, identity)
        base.require(self.record_count < (MAX_REQUESTS if self.finishing else MAIN_REQUESTS),
                     "HTTP request count exhausted its phase reserve")
        remaining = self.deadline - time.monotonic()
        base.require(remaining > 0, "Capture phase exceeded its wall-clock bound")
        byte_limit = base.MAX_TOTAL if self.finishing else base.MAX_TOTAL - 8 * 1024 * 1024
        limit = min(base.MAX_BODY, byte_limit - self.charged_bytes)
        base.require(limit > 0, "Capture exhausted its wire-byte reserve")
        headers = {"Accept": "application/json"}
        metadata = self.metadata(identity) if identity else None
        if metadata:
            headers["Authorization"] = "Emby " + ", ".join(name + '=\"' + value + '\"' for name, value in metadata.items())
        if token:
            headers["X-Emby-Token"] = token
        wire = None
        if body is not MISSING:
            headers["Content-Type"] = "application/json"
            wire = json.dumps(body, separators=(",", ":")).encode()
            base.require(len(wire) <= 4096, "Request body exceeds its bound")
        request = {"method": method, "path": path, "headers": headers,
                   "body": None if body is MISSING else body, "bodyPresent": body is not MISSING,
                   "clientMetadata": metadata}
        if method != "GET" and urlsplit(path).path.startswith("/emby/Devices"):
            request["ownedDeviceWriteProof"] = self.pending_write_proof
            self.pending_write_proof = None
        mutation = None
        if method != "GET":
            mutation = {"label": label, "request": request, "responseStatus": None,
                        "acknowledgedLogin": False, "transportFailure": None}
            self.mutations.append(mutation)
            base.save(base.PRIVATE / "mutations" / (str(len(self.mutations)).zfill(3) + "-intent.json"), request)
        if identity:
            self.login_attempts.add(identity)
            self.login_statuses[identity] = None
        self.record_count += 1
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=min(5, remaining))
        signal.setitimer(signal.ITIMER_REAL, min(15, remaining))
        try:
            connection.request(method, path, wire, headers)
            response = connection.getresponse()
            content = response.read(limit)
            complete = response.isclosed() or response.length == 0
            status, response_headers = response.status, response.getheaders()
        except Exception as error:
            # A timed-out read may have consumed bytes that http.client does not
            # return. Charge its full allowance rather than undercount the cap.
            self.charged_bytes += limit
            if mutation is not None:
                mutation["transportFailure"] = type(error).__name__
            self.write(label, {"request": request, "observation": {
                "completeHTTP": False, "captureIncomplete": True, "transportFailure": type(error).__name__,
                "unmeasuredWireBytesUpperBound": limit}})
            self.incomplete_count += 1
            raise
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        self.total += len(content)
        self.charged_bytes += len(content)
        text = content.decode("utf-8", errors="replace")
        try:
            result, kind = json.loads(text), "json"
        except json.JSONDecodeError:
            result, kind = text, "text"
        if mutation is not None:
            mutation["responseStatus"] = status
        parsed = urlsplit(path)
        if status in {200, 204} and (method == "DELETE" and parsed.path == "/emby/Devices" or
                                    method == "POST" and parsed.path == "/emby/Devices/Delete"):
            device_id = parse_qs(parsed.query)["Id"][0]
            self.acknowledged_device_deletes[device_id] = {"label": label, "status": status}
        if identity:
            self.login_statuses[identity] = status
        # Persist any parseable acknowledged credential before status, user,
        # session, device, or other DTO assertions. Never revoke an old token.
        if identity and isinstance(result, dict) and isinstance(result.get("AccessToken"), str) and result["AccessToken"]:
            acknowledged = result["AccessToken"]
            self.secrets.add(acknowledged)
            if acknowledged not in self.forbidden_tokens:
                self.logins[identity] = result
                mutation["acknowledgedLogin"] = True
            else:
                self.cleanup_errors.append({"stage": "login", "identity": identity, "error": "ExistingCredentialReturned"})
            base.save(base.PRIVATE / (identity + "-login-response.json"), result)
        self.write(label, {"request": request,
            "response": {"status": status, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content)}})
        self.incomplete_count += int(not complete)
        print(json.dumps({"capture": base.PREFIX + label, "status": status, "bodyType": kind,
                          "bytes": len(content), "completeHTTP": complete}), flush=True)
        base.require(complete, "Response exceeds the bounded JSON capture")
        return status, result

    def login(self, identity: str) -> None:
        if REFERENCE_RUN == 2 and identity != "control":
            base.require(self.deadline - time.monotonic() > 1.1,
                         "Login spacing would exceed the capture phase deadline")
            started = time.monotonic()
            time.sleep(1.1)
            self.login_delays.append({"identity": identity, "requestedSeconds": 1.1,
                                      "elapsedSeconds": round(time.monotonic() - started, 6),
                                      "reason": "Separate observed reference activity timestamps"})
        account = self.account_credentials[IDENTITIES[identity][0]]
        status, result = self.request(identity + "-login", "POST", "/emby/Users/AuthenticateByName", identity=identity,
                                     body={"Username": account["REFERENCE_USERNAME"], "Pw": account["REFERENCE_PASSWORD"]})
        base.require(status == 200 and identity in self.logins and isinstance(result.get("User"), dict),
                     "Owned login response differs; acknowledged credentials remain tracked")
        base.require(result["User"].get("Name") == account["REFERENCE_USERNAME"] and
                     isinstance(result["User"].get("Id"), str), "Login user identity differs")
        base.require(result["User"].get("Policy", {}).get("IsAdministrator") is (IDENTITIES[identity][0] == "admin"),
                     "Login administrator/viewer role differs")

    def admin(self) -> str:
        return self.logins["control"]["AccessToken"]

    def devices(self, label: str, *, require_complete: bool = False) -> list[dict]:
        status, result = self.request(label, "GET", "/emby/Devices", token=self.admin())
        base.require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list),
                     "Device list shape differs; no device mutation is inferred")
        rows = result["Items"]
        base.require(len(rows) <= MAX_OLD_DEVICES + len(IDENTITIES) * 3 and
                     all(isinstance(item, dict) and isinstance(item.get("Id"), str) and item["Id"] for item in rows),
                     "Device list contains an unbounded or unusable identity")
        base.require(len({item["Id"] for item in rows}) == len(rows), "Device list canonical IDs are not unique")
        membership = None
        if require_complete:
            # The first live study returned TotalRecordCount=0 with nonempty
            # Items. Establish completeness for every known device identity
            # from membership, never by interpreting that defective counter.
            base.require(self.old_devices is not None and self.control_id is not None,
                         "Device absence requires an established membership baseline")
            observed_ids = {item["Id"] for item in rows}
            protected_ids = set(self.old_devices) | {self.control_id}
            known_owned = set(self.owned_devices)
            acknowledged = set(self.acknowledged_device_deletes)
            expected_owned = known_owned - acknowledged
            base.require(protected_ids <= observed_ids and expected_owned <= observed_ids and
                         observed_ids <= protected_ids | known_owned,
                         "Device absence list omitted known membership or contains an unexplained identity")
            membership = {"protectedIds": sorted(protected_ids), "knownOwnedIds": sorted(known_owned),
                          "acknowledgedDeletedIds": sorted(acknowledged), "expectedOwnedPresentIds": sorted(expected_owned),
                          "observedIds": sorted(observed_ids)}
        for item in rows:
            if self.owned_pair(item):
                device_id = item["Id"]
                if self.old_devices is not None:
                    base.require(device_id not in self.old_devices, "Reserved client metadata collided with an old device")
                previous = self.owned_devices.get(device_id)
                if previous and previous["ReportedDeviceId"] != item["ReportedDeviceId"]:
                    self.ambiguous_ids.add(device_id)
                    raise RuntimeError("Canonical device ID changed its owned reported identity")
                self.owned_devices[device_id] = item
                self.acknowledged_device_deletes.pop(device_id, None)
                self.read_ids.add(device_id)
                self.read_ids.add(item["ReportedDeviceId"])
                internal = item.get("InternalId")
                if isinstance(internal, int) and not isinstance(internal, bool):
                    self.read_ids.add(str(internal))
        self.device_observations.append({"label": label, "totalRecordCount": result.get("TotalRecordCount"),
                                        "itemsCount": len(rows), "completeKnownMembershipRequired": require_complete,
                                        "reportedTotalMatchesItems": result.get("TotalRecordCount") == len(rows),
                                        "membershipProof": membership,
                                        "owned": [item for item in rows if self.owned_pair(item)]})
        return rows

    @staticmethod
    def route(suffix: str, device_id: str) -> str:
        return "/emby/Devices" + suffix + "?" + urlencode({"Id": device_id})

    def find_owned(self, rows: list[dict], reported: str, client: str | None = None) -> str:
        matches = [item["Id"] for item in rows if item.get("ReportedDeviceId") == DEVICE_PREFIX + reported and
                   self.owned_pair(item) and (client is None or item.get("AppName") == CLIENT_PREFIX + client)]
        base.require(len(matches) == 1, "Owned device match is missing or ambiguous; no mutation is inferred")
        return matches[0]

    def info_options(self, label: str, device_id: str) -> None:
        for suffix, name in (("/Info", "info"), ("/Options", "options")):
            self.request(label + "-" + name, "GET", self.route(suffix, device_id), token=self.admin())

    def prove_write(self, label: str, device_id: str, *, allow_missing: bool = False) -> None:
        self.pending_write_proof = None
        base.require(self.old_devices is not None and device_id not in self.old_devices and
                     device_id in self.owned_devices and device_id != self.control_id and
                     device_id not in self.ambiguous_ids and device_id not in self.protected_device_aliases,
                     "Write proof target is not exclusively owned or collides with a protected device alias")
        status, info = self.request(label, "GET", self.route("/Info", device_id), token=self.admin())
        expected = self.owned_devices[device_id]
        fields = ("Id", "ReportedDeviceId", "AppName")
        matched = status == 200 and isinstance(info, dict) and all(info.get(field) == expected.get(field) for field in fields)
        missing = allow_missing and status == 404 and device_id in self.approved_write_ids
        missing_kind = "HTTP404" if missing else None
        absence_list = None
        if allow_missing and status == 204 and info == "" and device_id in self.approved_write_ids:
            absence_label = label + "-absence-list"
            try:
                rows = self.devices(absence_label, require_complete=True)
                absent = all(item["Id"] != device_id for item in rows)
                observation = self.device_observations[-1]
                absence_list = {"label": absence_label, "completeKnownMembership": True, "itemCount": len(rows),
                                "reportedTotalRecordCount": observation["totalRecordCount"],
                                "reportedTotalMatchesItems": observation["reportedTotalMatchesItems"],
                                "membershipProof": observation["membershipProof"], "targetAbsent": absent}
                if absent:
                    missing = True
                    missing_kind = "HTTP204EmptyAndAbsentFromCompleteList"
            except Exception as error:
                absence_list = {"label": absence_label, "completeKnownMembership": False, "error": type(error).__name__}
        proof = {"label": label, "targetId": device_id, "status": status,
                 "matchedOwnedIdentity": matched, "previouslyProvedTargetMissing": missing,
                 "missingResponseKind": missing_kind, "absenceList": absence_list,
                 "expected": {field: expected.get(field) for field in fields},
                 "observed": {field: info.get(field) for field in fields} if isinstance(info, dict) else None}
        self.write_proofs.append(proof)
        if not matched and not missing:
            self.approved_write_ids.discard(device_id)
            self.ambiguous_ids.add(device_id)
        base.require(matched or missing, "Device route does not prove the selected owned identity; all writes to it are prohibited")
        if matched:
            self.approved_write_ids.add(device_id)
            self.acknowledged_device_deletes.pop(device_id, None)
        self.pending_write_proof = proof

    def mutate_device(self, label: str, method: str, device_id: str, *, suffix: str = "",
                      token: str = "", body: object = MISSING) -> tuple[int, object]:
        self.prove_write(label + "-owned-proof", device_id,
                         allow_missing=suffix != "/Options")
        return self.request(label, method, self.route(suffix, device_id), token=token, body=body)

    def probe_login(self, label: str, identity: str) -> int:
        token = self.logins[identity]["AccessToken"]
        status, _ = self.request(label, "GET", "/emby/Sessions", token=token)
        if status == 401:
            self.invalid_tokens.add(token)
            for name, login in self.logins.items():
                if login["AccessToken"] == token:
                    self.invalid_login_proofs[name] = {"label": label, "status": status}
        else:
            # Relogin can reveal credential reuse or unexpected resurrection.
            # Earlier invalidity must never stand in for the latest probe.
            self.invalid_tokens.discard(token)
            for name, login in self.logins.items():
                if login["AccessToken"] == token:
                    self.invalid_login_proofs.pop(name, None)
        return status

    def capture(self) -> None:
        status, public = self.request("public-before", "GET", "/emby/System/Info/Public")
        base.require(status == 200 and isinstance(public, dict) and public.get("Version") == "4.9.5.0",
                     "Reference product version differs")
        self.login("control")
        rows = self.devices("devices-baseline")
        self.control_id = self.find_owned(rows, "control", "Control")
        # The baseline necessarily follows the fresh control login. Its one
        # owned registry row is excluded explicitly, never treated as preexisting.
        base.require(all(not str(item.get("ReportedDeviceId", "")).startswith(DEVICE_PREFIX) or
                         item["Id"] == self.control_id for item in rows), "Reserved reported device IDs already exist")
        self.old_devices = {item["Id"]: item for item in rows if item["Id"] != self.control_id}
        base.require(len(self.old_devices) <= MAX_OLD_DEVICES, "Old device count exceeds the audited request reserve")
        for item in rows:
            for value in (item.get("Id"), item.get("ReportedDeviceId"), item.get("InternalId")):
                if isinstance(value, (str, int)) and not isinstance(value, bool) and str(value):
                    self.protected_device_aliases.add(str(value))
        self.read_ids.update(self.old_devices)
        base.save(base.PRIVATE / "old-devices.json", self.old_devices)
        for index, device_id in enumerate(sorted(self.old_devices)):
            status, options = self.request("old-options-before-" + str(index), "GET",
                                           self.route("/Options", device_id), token=self.admin())
            self.old_options[device_id] = {"status": status, "body": options}
        base.save(base.PRIVATE / "old-options.json", self.old_options)
        status, users = self.request("users-baseline", "GET", "/emby/Users", token=self.admin())
        base.require(status == 200 and isinstance(users, list) and all(isinstance(user, dict) and user.get("Id") for user in users),
                     "User baseline is unavailable")
        self.old_users = users
        base.save(base.PRIVATE / "old-users.json", users)
        self.request("sessions-baseline", "GET", "/emby/Sessions", token=self.admin())
        base.require(self.probe_login("control-protected-before", "control") == 200,
                     "Owned control token cannot establish the protected probe route")
        for identity in ("alpha-admin", "alpha-viewer", "alpha-other-client", "beta-viewer", "permission-viewer"):
            self.login(identity)
            self.devices("devices-after-" + identity)
            base.require(self.probe_login(identity + "-protected-before", identity) == 200,
                         "Owned token cannot establish the protected probe route")
        rows = self.devices("devices-created")
        alpha_rows = [item for item in rows if item.get("ReportedDeviceId") == DEVICE_PREFIX + "alpha" and self.owned_pair(item)]
        base.require(bool(alpha_rows), "No uniquely owned alpha device is available")
        preferred = [item for item in alpha_rows if item.get("AppName") == CLIENT_PREFIX + "Alpha"]
        merged_other = not preferred and len(alpha_rows) == 1 and alpha_rows[0].get("AppName") == CLIENT_PREFIX + "Other"
        base.require(len(preferred) == 1 or merged_other, "Alpha canonical selection is ambiguous")
        selected = preferred[0] if preferred else alpha_rows[0]
        alpha = selected["Id"]
        self.alpha_registry_shape = {"selectedId": alpha, "selectedClient": selected.get("AppName"),
                                     "selection": "Preferred Alpha client" if preferred else "Only merged Other client row observed",
                                     "observedRows": alpha_rows}
        beta = self.find_owned(rows, "beta")
        permission = self.find_owned(rows, "permission")
        base.require(len({self.control_id, alpha, beta, permission}) == 4, "Independent owned device IDs collide")
        self.request("sessions-created", "GET", "/emby/Sessions", token=self.admin())
        for name, order in (("ascending", "Ascending"), ("descending", "Descending"),
                            ("lowercase", "ascending"), ("invalid", "invalid")):
            self.request("devices-sort-" + name, "GET", "/emby/Devices?" + urlencode({"SortOrder": order}), token=self.admin())
        target = self.owned_devices[alpha]
        aliases = [("canonical", alpha), ("reported", target["ReportedDeviceId"])]
        if isinstance(target.get("InternalId"), int) and not isinstance(target["InternalId"], bool):
            aliases.append(("internal", str(target["InternalId"])))
        aliases.append(("unknown", UNKNOWN_ID))
        if REFERENCE_RUN == 2:
            current_aliases = {str(value) for item in rows for value in
                               (item.get("Id"), item.get("ReportedDeviceId"), item.get("InternalId"))
                               if isinstance(value, (str, int)) and not isinstance(value, bool)}
            base.require(UNKNOWN_NUMERIC_ID not in self.protected_device_aliases and
                         UNKNOWN_NUMERIC_ID not in current_aliases, "Numeric missing-ID probe collides with an existing alias")
            self.read_ids.add(UNKNOWN_NUMERIC_ID)
            aliases.append(("unknown-numeric", UNKNOWN_NUMERIC_ID))
        for name, alias in aliases:
            self.info_options("lookup-" + name, alias)
        for suffix, name in (("/Info", "info"), ("/Options", "options")):
            self.request("lookup-missing-" + name, "GET", "/emby/Devices" + suffix, token=self.admin())
        viewer = self.logins["beta-viewer"]["AccessToken"]
        for name, token in (("anonymous", ""), ("viewer", viewer),
                            ("viewer-self", self.logins["permission-viewer"]["AccessToken"])):
            self.request(name + "-devices", "GET", "/emby/Devices", token=token)
            self.request(name + "-info", "GET", self.route("/Info", permission), token=token)
            self.request(name + "-options", "GET", self.route("/Options", permission), token=token)
            self.mutate_device(name + "-rename", "POST", permission, suffix="/Options", token=token,
                               body={"CustomName": CUSTOM_PREFIX + name})
            self.info_options(name + "-rename-observed", permission)
        for name, token in (("anonymous", ""), ("viewer", viewer)):
            self.mutate_device(name + "-delete", "DELETE", permission, token=token)
            self.mutate_device(name + "-delete-alias", "POST", permission, suffix="/Delete", token=token)
            self.devices("devices-after-" + name + "-permissions")
        for name, options in (("rename", {"CustomName": CUSTOM_PREFIX + "Alpha Renamed"}),
                              ("clear-empty", {"CustomName": ""}),
                              ("rename-before-missing", {"CustomName": CUSTOM_PREFIX + "Before Missing"}),
                              ("clear-missing", {}),
                              ("rename-before-null", {"CustomName": CUSTOM_PREFIX + "Before Null"}),
                              ("clear-null", {"CustomName": None})):
            self.mutate_device("options-" + name, "POST", alpha, suffix="/Options", token=self.admin(), body=options)
            self.info_options("options-" + name + "-observed", alpha)
            self.request("options-" + name + "-sessions", "GET", "/emby/Sessions", token=self.admin())
        if REFERENCE_RUN == 2:
            self.mutate_device("options-before-delete-relogin", "POST", alpha, suffix="/Options", token=self.admin(),
                               body={"CustomName": CUSTOM_PREFIX + "Before Delete Relogin"})
            self.request("options-before-delete-relogin-info", "GET", self.route("/Info", alpha), token=self.admin())
            status, options = self.request("options-before-delete-relogin-options", "GET",
                                           self.route("/Options", alpha), token=self.admin())
            self.alpha_relogin_observation = {"previousCanonicalIds": [item["Id"] for item in alpha_rows],
                                             "beforeDeleteOptions": {"status": status, "body": options}}
        deletion_targets = [("canonical", alpha, "DELETE", ""), ("alias", beta, "POST", "/Delete")]
        for index, item in enumerate(sorted(alpha_rows, key=lambda row: (row.get("AppName", ""), row["Id"]))):
            if item["Id"] != alpha:
                self.prove_write("remaining-alpha-" + str(index) + "-initial-proof", item["Id"])
                deletion_targets.append(("remaining-alpha-" + str(index), item["Id"], "DELETE", ""))
        for name, device_id, method, suffix in deletion_targets:
            self.mutate_device("delete-" + name, method, device_id, suffix=suffix, token=self.admin())
            self.mutate_device("delete-" + name + "-repeat", method, device_id, suffix=suffix, token=self.admin())
            self.info_options("delete-" + name + "-observed", device_id)
            self.devices("devices-after-delete-" + name)
            for identity in ("alpha-admin", "alpha-viewer", "alpha-other-client", "beta-viewer", "permission-viewer"):
                self.probe_login("delete-" + name + "-protected-" + identity, identity)
            self.request("delete-" + name + "-sessions", "GET", "/emby/Sessions", token=self.admin())
        if REFERENCE_RUN == 2:
            self.login("alpha-relogin")
            rows = self.devices("devices-after-alpha-relogin")
            current_alpha = self.find_owned(rows, "alpha", "Alpha")
            self.request("alpha-relogin-info", "GET", self.route("/Info", current_alpha), token=self.admin())
            status, options = self.request("alpha-relogin-options", "GET", self.route("/Options", current_alpha), token=self.admin())
            self.alpha_relogin_observation.update({"newCanonicalId": current_alpha,
                "differentCanonicalIdObserved": current_alpha not in self.alpha_relogin_observation["previousCanonicalIds"],
                "afterReloginOptions": {"status": status, "body": options}})
            base.require(self.probe_login("alpha-relogin-protected", "alpha-relogin") == 200,
                         "Owned relogin cannot establish the protected probe route")
            for identity in ("alpha-admin", "alpha-viewer", "alpha-other-client", "beta-viewer", "permission-viewer"):
                self.probe_login("alpha-relogin-old-protected-" + identity, identity)

    def cleanup_step(self, label: str, action) -> object:
        try:
            return action()
        except Exception as error:
            self.cleanup_errors.append({"stage": label, "error": type(error).__name__})
            base.save(base.PRIVATE / ("cleanup-error-" + str(len(self.cleanup_errors)) + ".txt"),
                      label + ": " + type(error).__name__ + ": " + str(error) + "\n")
            return None

    def logout(self, identity: str) -> None:
        if identity not in self.logins:
            return
        token = self.logins[identity]["AccessToken"]
        if token not in self.invalid_tokens:
            status, _ = self.request("cleanup-logout-" + identity, "POST", "/emby/Sessions/Logout", token=token)
            self.logout_statuses[identity] = status
            # A logout 401 is not cleanup proof by itself. The dedicated owned
            # protected-route request below must independently return 401.
        status = self.probe_login("cleanup-invalid-" + identity, identity)
        base.require(status == 401 and token in self.invalid_tokens, "Owned login remains usable or invalidity is unproven")
        self.logged_out.add(identity)

    def compare_devices(self) -> None:
        rows = self.devices("devices-final-before-control-logout")
        self.final_devices = rows
        base.require(self.old_devices is not None, "Old device baseline was not established")
        current = {item["Id"]: item for item in rows if item["Id"] in self.old_devices}
        base.require(set(current) == set(self.old_devices), "An old device disappeared")
        self.old_device_date_changes = [{"Id": key, "before": old.get("DateLastActivity"),
                                        "after": current[key].get("DateLastActivity")}
                                       for key, old in self.old_devices.items()
                                       if old.get("DateLastActivity") != current[key].get("DateLastActivity")]
        self.checks["oldDeviceStructuralFieldsUnchanged"] = all(
            {key: value for key, value in old.items() if key != "DateLastActivity"} ==
            {key: value for key, value in current[device_id].items() if key != "DateLastActivity"}
            for device_id, old in self.old_devices.items())
        self.checks["oldDeviceActivityUnchanged"] = not self.old_device_date_changes
        options_equal = len(self.old_options) == len(self.old_devices)
        for index, device_id in enumerate(sorted(self.old_options)):
            status, body = self.request("old-options-final-" + str(index), "GET",
                                        self.route("/Options", device_id), token=self.admin())
            options_equal = options_equal and self.old_options[device_id] == {"status": status, "body": body}
        self.checks["oldDeviceOptionsUnchanged"] = options_equal
        self.checks["noUnownedNewDevices"] = all(item["Id"] in self.old_devices or self.owned_pair(item) for item in rows)
        self.checks["onlyControlOwnedDeviceRetained"] = all(
            item["Id"] == self.control_id for item in rows if self.owned_pair(item))

    def compare_users(self) -> None:
        status, users = self.request("users-final", "GET", "/emby/Users", token=self.admin())
        base.require(self.old_users is not None and status == 200 and isinstance(users, list), "Final users cannot be audited")
        old = {user["Id"]: user for user in self.old_users}
        current = {user["Id"]: user for user in users}
        self.checks["oldUserMembershipUnchanged"] = set(current) == set(old)
        participants = {login.get("User", {}).get("Id") for login in self.logins.values()}
        date_fields = {"LastLoginDate", "LastActivityDate"}
        unchanged = set(current) == set(old)
        for user_id, before in old.items():
            after = current.get(user_id, {})
            ignored = date_fields if user_id in participants else set()
            unchanged = unchanged and {key: value for key, value in before.items() if key not in ignored} == {
                key: value for key, value in after.items() if key not in ignored}
            for field in ignored:
                if before.get(field) != after.get(field):
                    self.user_date_changes.append({"userId": user_id, "field": field, "before": before.get(field),
                                                   "after": after.get(field), "attribution": "Owned fresh login and requests"})
        self.checks["oldUsersUnchangedExceptAttributedParticipantActivity"] = unchanged

    def finish(self) -> None:
        self.finishing = True
        self.deadline = time.monotonic() + PHASE_SECONDS
        for identity in IDENTITIES:
            if identity != "control":
                self.cleanup_step("logout-" + identity, lambda identity=identity: self.logout(identity))
        if "control" in self.logins:
            rows = self.cleanup_step("discover-owned-devices", lambda: self.devices("cleanup-devices-discover"))
            if rows is not None and self.old_devices is not None:
                for index, item in enumerate(rows):
                    device_id = item["Id"]
                    if self.owned_pair(item) and device_id != self.control_id and device_id not in self.ambiguous_ids:
                        self.cleanup_step("delete-device-" + str(index), lambda device_id=device_id, index=index:
                            self.mutate_device("cleanup-delete-device-" + str(index), "DELETE", device_id, token=self.admin()))
            if self.old_devices is not None:
                self.cleanup_step("compare-devices", self.compare_devices)
            if self.old_users is not None:
                self.cleanup_step("compare-users", self.compare_users)
            self.cleanup_step("sessions-final", lambda: self.request("sessions-final-before-control-logout", "GET",
                                                                      "/emby/Sessions", token=self.admin()))
            self.cleanup_step("logout-control", lambda: self.logout("control"))
        self.checks["allAcknowledgedLoginsInvalid"] = set(self.logins) == set(self.invalid_login_proofs)
        self.checks["allAttemptedLoginsAcknowledgedOrRejected"] = all(
            name in self.logins or isinstance(self.login_statuses.get(name), int) and self.login_statuses[name] in {400, 401, 403}
            for name in self.login_attempts)
        self.checks["referenceProcessUnchanged"] = self.cleanup_step("process-audit", preconditions) == self.pid
        snapshot = self.cleanup_step("old-file-audit", self.snapshot)
        self.checks["oldRecordsSourcesPrivateFilesUnchanged"] = snapshot == self.baseline
        self.cleanup_step("private-mutation-journal", lambda: base.save(base.PRIVATE / "mutation-results.json", self.mutations))
        redaction_ok = True
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            base.private_file(path)
            base.private_file(base.EXPORT / path.name)
            redaction_ok = redaction_ok and self.sanitize(json.loads(path.read_text())) == json.loads((base.EXPORT / path.name).read_text())
        self.checks["newRedactionAuditPassed"] = redaction_ok
        self.cleanup_ok = not self.cleanup_errors and all(self.checks.values())
        self.write("audit", {"kind": "capture-audit-observation", "referencePID": self.pid,
            "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok,
            "checks": self.checks, "cleanupErrors": self.cleanup_errors,
            "referenceRun": REFERENCE_RUN, "preservedOldRecords": PREVIOUS_RECORDS,
            "preservedPriorDeviceCapture": self.prior_capture_observation,
            "preservedOldRecordFiles": len(self.baseline["records"]),
            "preservedOldMediaFiles": len(self.baseline["media"]),
            "preservedOldPrivateFiles": len(self.baseline["privateFiles"]),
            "oldDeviceCount": None if self.old_devices is None else len(self.old_devices),
            "oldDeviceOptionsAudited": len(self.old_options), "oldDeviceActivityDifferences": self.old_device_date_changes,
            "attributedUserActivityDifferences": self.user_date_changes,
            "ownedLoginAttempts": sorted(self.login_attempts), "ownedLoginStatuses": self.login_statuses,
            "ownedLoginsAcknowledged": sorted(self.logins), "ownedLoginLogoutStatuses": self.logout_statuses,
            "ownedLoginInvalidityProofs": self.invalid_login_proofs, "ownedDeviceObservations": self.device_observations,
            "alphaRegistryIdentityObservation": self.alpha_registry_shape,
            "alphaReloginObservation": self.alpha_relogin_observation, "loginSpacingObservations": self.login_delays,
            "ownedDeviceMutations": self.mutations, "ownedDeviceWriteProofs": self.write_proofs,
            "retainedOwnedHistory": None if self.final_devices is None else [item for item in self.final_devices if self.owned_pair(item)],
            "retainedHistoryObservation": "Device list immediately before final control logout; that owned token is then probed independently",
            "existingCredentialHTTPRequests": 0, "applicationKeyMutations": 0, "sourceWrites": 0,
            "cameraUploadRequests": 0, "encoderRequests": 0, "httpAttempts": self.record_count,
            "incompleteHTTP": self.incomplete_count, "wireBytes": self.total,
            "wireByteBudgetCharged": self.charged_bytes,
            "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "limits": {"oldDevices": MAX_OLD_DEVICES, "httpAttempts": MAX_REQUESTS,
                       "mainHTTPAttempts": MAIN_REQUESTS, "responseBytes": base.MAX_BODY,
                       "totalResponseBytes": base.MAX_TOTAL, "phaseSeconds": PHASE_SECONDS,
                       "socketTimeoutSeconds": 5, "requestDeadlineSeconds": 15}})
        base.require(self.cleanup_ok, "Cleanup or preservation proof is incomplete; inspect private evidence")


def main() -> None:
    signal.signal(signal.SIGALRM, deadline_expired)
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
    print(json.dumps({"capture": base.PREFIX, "result": "complete" if failure is None else "partial",
                      "failureType": failure, "cleanup": recorder.cleanup_ok}), flush=True)
    if failure:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
