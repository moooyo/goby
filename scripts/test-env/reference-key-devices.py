#!/usr/bin/env python3
"""Capture application-key device membership in the isolated Emby reference.

Run only through authorized root SSH on test-env beside the two reviewed
reference recorder modules. The two application labels, two ordinary logins,
and three key client identities below are this capture's entire mutable scope.
Existing credentials are never used. Media, scans, playback, uploads, and user
policy changes are outside the request allowlist. An ambiguous server device
is recorded without deletion. Partial attempts and private evidence survive.
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
from urllib.parse import parse_qs, quote, urlencode, urlsplit

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("device_reference", Path(__file__).with_name("reference-devices.py"))
device = importlib.util.module_from_spec(spec)
spec.loader.exec_module(device)
base = device.base
base.__file__ = __file__

PREVIOUS = Path("/opt/goby-test/exec-scratch/devices-m5e-2")
PREVIOUS_RECORDS = 1476
APP_PREFIX = "Goby Key Devices M5e 20260910 03 "
device.DEVICE_PREFIX = "goby-key-devices-m5e-20260910-03-"
device.CLIENT_PREFIX = "Goby Key Devices M5e 20260910 03 "
device.CUSTOM_PREFIX = "Goby Key Devices M5e 03 Owned "
device.IDENTITIES = {
    "control": ("admin", "control", "Control", "Key Devices Control", "0.1.0"),
    "viewer": ("viewer", "viewer", "Viewer", "Key Devices Viewer", "0.1.0"),
}
device.REFERENCE_RUN = 2  # Reuse the reviewed 1.1-second ordinary-login spacing.
base.ROOT = Path("/opt/goby-test/exec-scratch/key-devices-m5e")
base.PRIVATE, base.RAW, base.EXPORT = base.ROOT / "private", base.ROOT / "private/raw", base.ROOT / "export"
base.PREFIX = "key-devices-m5e-"
base.MARKER = "goby-reference-key-devices-m5e-owned-v1"
base.MAX_BODY, base.MAX_TOTAL = 256 * 1024, 32 * 1024 * 1024
MISSING = device.MISSING
KEY_APPS = {"alpha": APP_PREFIX + "Alpha", "sibling": APP_PREFIX + "Sibling"}
KEY_CLIENTS = {
    "alpha": ("alpha", "key-alpha", "Key Alpha", "Key Alpha Device", "1.0.0"),
    "beta": ("alpha", "key-beta", "Key Beta", "Key Beta Device", "2.0.0"),
    "sibling": ("sibling", "key-sibling", "Key Sibling", "Key Sibling Device", "3.0.0"),
}
# This is the numeric DeviceId actually captured by the preceding key studies,
# not an assumed equivalence between a reported identifier and a numeric ID.
PREVIOUS_SERVER_NUMERIC_ID = "15"


class Recorder(device.Recorder):
    def __init__(self, pid: int) -> None:
        self.key_create_attempts: set[str] = set()
        self.key_credential_ids: dict[str, str] = {}
        self.key_creation_statuses: dict[str, int | None] = {}
        self.key_delete_acknowledgements: list[dict] = []
        self.key_probe_observations: list[dict] = []
        self.key_invalidity_proofs: dict[str, dict] = {}
        self.key_rows_observations: list[dict] = []
        self.key_device_observations: list[dict] = []
        self.server_public_id: str | None = None
        self.server_baseline_info: dict[str, dict] = {}
        self.server_baseline_list: list[dict] = []
        self.server_candidates: dict[str, dict] = {}
        self.protected_hidden_devices: dict[str, dict] = {}
        self.protected_hidden_options: dict[str, dict] = {}
        self.previous_device_ids: set[str] = set()
        self.device_baseline_complete = False
        self.old_key_baseline_complete = False
        self.server_creation_gate: dict = {"evaluated": False, "allowed": False, "blockedReason": "Not reached"}
        self.server_branch: dict = {"verified": False, "deleteAttempted": False, "reason": "Not reached"}
        self.header_delete_branch: dict = {"verified": False, "deleteAttempted": False, "reason": "Not reached"}
        self.virtual_write_id: str | None = None
        self.old_key_audit_complete = False
        super().__init__(pid)
        self.apps = set(KEY_APPS.values())
        self.read_ids = {PREVIOUS_SERVER_NUMERIC_ID}

    def snapshot(self) -> dict:
        prior_path = PREVIOUS / "private/baseline.json"
        audit_path = PREVIOUS / "private/raw/devices-m5e-2-audit.json"
        final_devices_path = PREVIOUS / "private/raw/devices-m5e-2-devices-final-before-control-logout.json"
        for path in (prior_path, audit_path, final_devices_path):
            base.private_file(path)
        prior, audit = json.loads(prior_path.read_text()), json.loads(audit_path.read_text())
        base.require(audit.get("cleanupPassed") is True and audit.get("captureFailureType") is None and
                     audit.get("httpAttempts") == 194 and audit.get("incompleteHTTP") == 0,
                     "The preceding approved device capture is not complete with proven cleanup")
        previous_devices = json.loads(final_devices_path.read_text())["response"]
        base.require(previous_devices.get("status") == 200 and isinstance(previous_devices.get("body"), dict) and
                     isinstance(previous_devices["body"].get("Items"), list), "Previous final device list is unavailable")
        previous_rows = previous_devices["body"]["Items"]
        base.require(len(previous_rows) <= device.MAX_OLD_DEVICES and all(isinstance(row, dict) and
                     isinstance(row.get("Id"), str) and row["Id"] for row in previous_rows) and
                     len({row["Id"] for row in previous_rows}) == len(previous_rows),
                     "Previous final device membership is unbounded or ambiguous")
        self.previous_device_ids = {row["Id"] for row in previous_rows}
        paths = [Path(path) for path in prior["records"]]
        for folder in (PREVIOUS / "private/raw", PREVIOUS / "export"):
            paths.extend(folder.glob("*.json"))
        base.require(len(paths) == PREVIOUS_RECORDS * 2 and len(set(paths)) == PREVIOUS_RECORDS * 2,
                     "Expected exactly 1476 preceding raw/export pairs")
        media = [Path(path) for path in prior["media"]]
        base.require(len(media) == 240 and len(set(media)) == 240, "Known source membership differs")
        private_roots = {path.parent.parent for path in paths if path.parent.name == "raw"}
        private_paths = sorted({path for folder in private_roots for path in folder.rglob("*") if path.is_file()})
        base.require(sum(path.stat().st_size for path in media) < 32 * 1024 * 1024 and
                     len(private_paths) < 4096 and sum(path.stat().st_size for path in private_paths) < 128 * 1024 * 1024,
                     "Preservation audit exceeds its finite bounds")
        for path in [*paths, *media, *private_paths]:
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode),
                         "A preserved path is not a canonical regular file")
        return {"records": {str(path): base.digest(path) for path in paths},
                "media": {str(path): base.digest(path) for path in media},
                "privateFiles": {str(path): base.digest(path) for path in private_paths}}

    @staticmethod
    def client_metadata(client: str) -> dict:
        _key, reported, app, name, version = KEY_CLIENTS[client]
        return {"Client": device.CLIENT_PREFIX + app, "DeviceId": device.DEVICE_PREFIX + reported,
                "Device": name, "Version": version}

    def owned_pair(self, item: dict) -> bool:
        if super().owned_pair(item):
            return True
        if any(item.get("ReportedDeviceId") == self.client_metadata(client)["DeviceId"] and
               item.get("AppName") == self.client_metadata(client)["Client"] for client in KEY_CLIENTS):
            return True
        return bool(self.virtual_write_id and item.get("Id") == self.virtual_write_id and
                    item.get("ReportedDeviceId") == self.server_public_id and
                    item.get("AppName") == self.owned_devices[self.virtual_write_id].get("AppName"))

    def token_for(self, name: str) -> str:
        matches = [token for token, row in self.owned.items() if row.get("AppName") == KEY_APPS[name]]
        base.require(len(matches) == 1, "Owned key application does not identify one acknowledged credential")
        return matches[0]

    def prove_write(self, label: str, device_id: str, *, allow_missing: bool = False) -> None:
        expected = self.owned_devices.get(device_id, {})
        base.require(all(str(expected[field]) not in self.protected_device_aliases for field in
                     ("Id", "ReportedDeviceId", "InternalId") if expected.get(field) is not None),
                     "Owned device identity collides with a protected canonical, reported, or internal alias")
        super().prove_write(label, device_id, allow_missing=allow_missing)

    def key_name(self, token: str) -> str:
        row = self.owned[token]
        return next(name for name, app in KEY_APPS.items() if app == row["AppName"])

    def server_creation_reason(self) -> str | None:
        if not self.old_key_baseline_complete or self.old_keys != []:
            return "The complete pre-creation key baseline is unavailable or contains existing keys"
        if not self.device_baseline_complete or self.old_devices is None or set(self.old_devices) != self.previous_device_ids:
            return "The complete previously audited device membership was not established"
        if not isinstance(self.server_public_id, str) or not self.server_public_id:
            return "Public server identity is unavailable"
        if self.server_baseline_list or any(row.get("ReportedDeviceId") == self.server_public_id for row in self.old_devices.values()):
            return "An existing listed server device must not receive implicit key metadata or activity writes"
        if self.server_baseline_info.get(self.server_public_id, {}).get("status") != 404:
            return "Reported server identity was not absent with HTTP 404 before key creation"
        numeric = self.server_baseline_info.get(PREVIOUS_SERVER_NUMERIC_ID)
        if not isinstance(numeric, dict) or numeric.get("status") not in {200, 204, 404}:
            return "The historical numeric alias could not be observed before key creation"
        if numeric["status"] == 204 and numeric.get("body") != "":
            return "Historical numeric HTTP 204 did not have the observed empty body"
        if numeric["status"] == 200:
            info = numeric.get("body")
            if not isinstance(info, dict) or not isinstance(info.get("Id"), str) or not info["Id"] or not isinstance(info.get("ReportedDeviceId"), str) or not info["ReportedDeviceId"]:
                return "Positive historical numeric identity is ambiguous"
        for lookup in self.server_baseline_info.values():
            if lookup.get("status") == 200 and isinstance(lookup.get("body"), dict) and lookup["body"].get("ReportedDeviceId") == self.server_public_id:
                return "An existing hidden server device must not receive implicit key metadata or activity writes"
        return None

    def establish_server_creation_gate(self) -> None:
        reason = self.server_creation_reason()
        self.server_creation_gate = {"evaluated": True, "allowed": reason is None, "blockedReason": reason,
            "reportedServerId": self.server_public_id, "completeDeviceBaseline": self.device_baseline_complete,
            "completeKeyBaseline": self.old_key_baseline_complete, "baselineListedServerDevices": self.server_baseline_list,
            "baselineServerInfo": self.server_baseline_info, "readOnlyServerBaselineBranch": reason is not None,
            "applicationKeyTrafficBeforeGate": len(self.key_create_attempts)}
        base.save(base.PRIVATE / "server-creation-gate.json", self.server_creation_gate)
        if reason is not None:
            self.server_branch = {"verified": False, "deleteAttempted": False,
                                  "reason": "Server creation gate blocked: " + reason}
        base.require(reason is None, "Server creation gate blocked; the preexisting shared server device must remain untouched")

    def authorize(self, method: str, path: str, token: str, body: object, identity: str,
                  client: str = "") -> None:
        parsed = urlsplit(path)
        base.require(not parsed.scheme and not parsed.netloc and not parsed.fragment and path.startswith("/emby/") and
                     "\\" not in path and len(path.encode()) <= 2048 and not any(ord(char) < 32 for char in path),
                     "Request is not a bounded relative reference path")
        query = parse_qs(parsed.query, keep_blank_values=True, strict_parsing=True)
        base.require(all(len(values) == 1 for values in query.values()), "Duplicate query fields are prohibited")
        issued = {login["AccessToken"] for login in self.logins.values()} | set(self.owned)
        base.require(not token or token in issued and token not in self.forbidden_tokens,
                     "HTTP credential is not acknowledged and owned")
        base.require(token not in self.owned or self.server_creation_gate.get("allowed") is True and
                     self.server_creation_reason() is None,
                     "Application-key HTTP traffic lacks the pre-creation server-device absence proof")
        base.require(not client or not identity and client in KEY_CLIENTS and token in self.owned and
                     self.owned[token].get("AppName") == KEY_APPS[KEY_CLIENTS[client][0]],
                     "Client metadata is not associated with the selected owned key")
        route, allowed = parsed.path, False
        if method == "GET" and not identity and body is MISSING:
            if route in {"/emby/System/Info/Public", "/emby/Users", "/emby/Devices", "/emby/Sessions", "/emby/Auth/Keys"}:
                allowed = not query and (not client or route in {"/emby/Devices", "/emby/Sessions", "/emby/Auth/Keys"})
                if route == "/emby/Auth/Keys" and not client and query == {"StartIndex": ["0"], "Limit": ["8"]}:
                    allowed = True
            elif route in {"/emby/Devices/Info", "/emby/Devices/Options"}:
                allowed = not client and set(query) == {"Id"} and query["Id"][0] in self.read_ids
        elif route == "/emby/Users/AuthenticateByName":
            if method == "POST" and not query and not token and not client and identity in device.IDENTITIES and identity not in self.login_attempts:
                account = self.account_credentials[device.IDENTITIES[identity][0]]
                allowed = body == {"Username": account["REFERENCE_USERNAME"], "Pw": account["REFERENCE_PASSWORD"]}
        elif route == "/emby/Sessions/Logout":
            allowed = method == "POST" and not query and not identity and not client and body is MISSING and bool(token)
        elif route == "/emby/Auth/Keys":
            allowed = (method == "POST" and set(query) == {"App"} and query["App"][0] in self.apps and
                       query["App"][0] not in self.key_create_attempts and self.old_keys == [] and
                       self.server_creation_gate.get("allowed") is True and self.server_creation_reason() is None and
                       token == self.admin() and not identity and not client and body is MISSING)
        elif route.startswith("/emby/Auth/Keys/"):
            allowed = (method == "DELETE" and not query and not identity and not client and body is MISSING and
                       token == self.admin() and any(route == "/emby/Auth/Keys/" + quote(key, safe="") for key in self.owned))
        elif set(query) == {"Id"} and not identity and not client:
            target, proof = query["Id"][0], self.pending_write_proof
            owned = (target in self.approved_write_ids and target in self.owned_devices and target not in self.ambiguous_ids and
                     target not in self.protected_device_aliases and target != self.control_id and
                     self.old_devices is not None and target not in self.old_devices)
            proved = owned and proof is not None and proof["targetId"] == target
            if route == "/emby/Devices/Options":
                allowed = (method == "POST" and proved and proof["matchedOwnedIdentity"] and isinstance(body, dict) and
                           set(body) == {"CustomName"} and isinstance(body["CustomName"], str) and
                           body["CustomName"].startswith(device.CUSTOM_PREFIX) and len(body["CustomName"].encode()) <= 128 and
                           target != self.virtual_write_id)
            elif route == "/emby/Devices":
                allowed = (method == "DELETE" and proved and body is MISSING and
                           (proof["matchedOwnedIdentity"] or proof["previouslyProvedTargetMissing"]))
        base.require(allowed, "Request is outside the explicit owned key-device allowlist")

    def acknowledge_keys(self, rows: object) -> None:
        if self.old_keys is None or not isinstance(rows, list):
            return
        old_tokens = {row["AccessToken"] for row in self.old_keys}
        for row in rows:
            if not isinstance(row, dict):
                continue
            token = row.get("AccessToken")
            if isinstance(token, str) and token:
                self.secrets.add(token)
                if row.get("AppName") in self.key_create_attempts and token not in old_tokens and token not in self.forbidden_tokens:
                    if token not in self.key_credential_ids:
                        self.key_credential_ids[token] = "owned-key-" + str(len(self.key_credential_ids) + 1)
                    self.owned[token] = row

    def request(self, label: str, method: str, path: str, *, token: str = "", body: object = MISSING,
                identity: str = "", client: str = "") -> tuple[int, object]:
        self.authorize(method, path, token, body, identity, client)
        base.require(self.record_count < (device.MAX_REQUESTS if self.finishing else device.MAIN_REQUESTS),
                     "HTTP request count exhausted its phase reserve")
        remaining = self.deadline - time.monotonic()
        base.require(remaining > 0, "Capture phase exceeded its wall-clock bound")
        byte_limit = base.MAX_TOTAL if self.finishing else base.MAX_TOTAL - 8 * 1024 * 1024
        limit = min(base.MAX_BODY, byte_limit - self.charged_bytes)
        base.require(limit > 0, "Capture exhausted its wire-byte reserve")
        headers = {"Accept": "application/json"}
        metadata = self.metadata(identity) if identity else self.client_metadata(client) if client else None
        if metadata:
            headers["Authorization"] = "Emby " + ", ".join(name + '=\"' + value + '\"' for name, value in metadata.items())
        if token:
            headers["X-Emby-Token"] = token
        wire = None
        if body is not MISSING:
            headers["Content-Type"] = "application/json"
            wire = json.dumps(body, separators=(",", ":")).encode()
            base.require(len(wire) <= 4096, "Request body exceeds its bound")
        request = {"method": method, "path": path, "headers": headers, "body": None if body is MISSING else body,
                   "bodyPresent": body is not MISSING, "clientMetadata": metadata, "ownedKeyClient": client or None}
        parsed = urlsplit(path)
        if method != "GET" and parsed.path.startswith("/emby/Devices"):
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
        if method == "POST" and parsed.path == "/emby/Auth/Keys":
            app = parse_qs(parsed.query)["App"][0]
            self.key_create_attempts.add(app)
            self.key_creation_statuses[app] = None
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
            self.charged_bytes += limit
            if mutation is not None:
                mutation["transportFailure"] = type(error).__name__
            self.write(label, {"request": request, "observation": {"completeHTTP": False, "captureIncomplete": True,
                "transportFailure": type(error).__name__, "unmeasuredWireBytesUpperBound": limit}})
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
        # Acknowledgements precede every DTO assertion and evidence write.
        if method == "DELETE" and parsed.path == "/emby/Devices" and status in {200, 204}:
            self.acknowledged_device_deletes[parse_qs(parsed.query)["Id"][0]] = {"label": label, "status": status}
        if method == "DELETE" and parsed.path.startswith("/emby/Auth/Keys/") and status in {200, 204}:
            names = [self.key_name(key) for key in self.owned if parsed.path == "/emby/Auth/Keys/" + quote(key, safe="")]
            self.key_delete_acknowledgements.append({"label": label, "keys": names, "status": status})
        if parsed.path == "/emby/Auth/Keys":
            if method == "POST":
                app = parse_qs(parsed.query)["App"][0]
                self.key_creation_statuses[app] = status
                if isinstance(result, dict) and isinstance(result.get("AccessToken"), str):
                    self.acknowledge_keys([{**result, "AppName": app}])
            elif method == "GET" and isinstance(result, dict):
                self.acknowledge_keys(result.get("Items"))
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
            base.save(base.PRIVATE / (identity + "-login-response.json"), result)
        self.write(label, {"request": request,
            "response": {"status": status, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": complete, "captureIncomplete": not complete, "wireBytes": len(content)}})
        self.incomplete_count += int(not complete)
        print(json.dumps({"capture": base.PREFIX + label, "status": status, "bodyType": kind,
                          "bytes": len(content), "completeHTTP": complete}), flush=True)
        base.require(complete, "Response exceeds the bounded JSON capture")
        return status, result

    def key_list(self, label: str) -> list[dict]:
        status, result = self.request(label, "GET", "/emby/Auth/Keys", token=self.admin())
        base.require(status == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list),
                     "Key list shape differs; acknowledged tokens remain tracked")
        rows = result["Items"]
        base.require(len(rows) <= 8 and all(isinstance(row, dict) and isinstance(row.get("AccessToken"), str) and
                     row["AccessToken"] for row in rows), "Key list is unbounded or lacks credential identity")
        # Unpaginated TotalRecordCount is an observed zero-count defect. The
        # already observed bounded paging contract supplies a real total and
        # independent complete membership proof for destructive ownership.
        page_status, page = self.request(label + "-bounded-page", "GET", "/emby/Auth/Keys?" +
                                          urlencode({"StartIndex": 0, "Limit": 8}), token=self.admin())
        base.require(page_status == 200 and isinstance(page, dict) and isinstance(page.get("Items"), list),
                     "Bounded key membership page is unavailable")
        page_rows, total = page["Items"], page.get("TotalRecordCount")
        base.require(isinstance(total, int) and not isinstance(total, bool) and 0 <= total <= 8 and
                     len(page_rows) == total and all(isinstance(row, dict) and isinstance(row.get("AccessToken"), str) and
                     row["AccessToken"] for row in page_rows), "Bounded key page does not prove complete membership")
        tokens = [row["AccessToken"] for row in rows]
        page_tokens = [row["AccessToken"] for row in page_rows]
        base.require(len(set(tokens)) == len(tokens) and len(set(page_tokens)) == len(page_tokens) and
                     set(tokens) == set(page_tokens), "Key identity membership is duplicated or differs across complete lists")
        self.key_rows_observations.append({"label": label, "totalRecordCount": result.get("TotalRecordCount"),
            "items": rows, "completeMembershipProof": {"pagedTotalRecordCount": total, "pageItemCount": len(page_rows),
                                                       "uniqueCredentialMembershipEqual": True}})
        return rows

    def probe_key(self, label: str, name: str, *, client: str = "", token_override: str = "") -> int:
        token = token_override or self.token_for(name)
        base.require(token in self.owned and self.key_name(token) == name, "Key probe credential does not match its owned label")
        status, body = self.request(label, "GET", "/emby/Sessions", token=token, client=client)
        credential_id = self.key_credential_ids[token]
        scope = credential_id + ":" + (client or "token-only")
        observation = {"label": label, "key": name, "credentialId": credential_id, "client": client or None, "status": status,
                       "sessions": body if isinstance(body, list) else None}
        self.key_probe_observations.append(observation)
        if status == 401:
            self.key_invalidity_proofs[scope] = {"label": label, "status": status}
        else:
            self.key_invalidity_proofs.pop(scope, None)
        return status

    def probe_keys(self, label: str) -> None:
        # Every acknowledged credential receives its own latest proof, even if
        # an unexpected duplicate application label caused capture to stop.
        for token in list(self.owned):
            name, credential_id = self.key_name(token), self.key_credential_ids[token]
            self.probe_key(label + "-" + credential_id, name, token_override=token)
            for client, values in KEY_CLIENTS.items():
                if values[0] == name:
                    self.probe_key(label + "-" + credential_id + "-" + client, name, client=client, token_override=token)

    def read_server_candidates(self, label: str, rows: list[dict]) -> None:
        key_rows = self.key_list(label + "-keys")
        self.server_candidates = {}
        mapped_ids = set()
        for row in key_rows:
            if row.get("AccessToken") in self.owned and row.get("ReportedDeviceId") == self.server_public_id:
                value = row.get("DeviceId")
                if isinstance(value, int) and not isinstance(value, bool) and value > 0:
                    mapped_ids.add(str(value))
        observation = {"label": label, "mappedNumericIds": sorted(mapped_ids), "lookups": [],
                       "listedServerDevices": [row for row in rows if row.get("ReportedDeviceId") == self.server_public_id]}
        for index, alias in enumerate(sorted(mapped_ids | {self.server_public_id})):
            self.read_ids.add(alias)
            status, info = self.request(label + "-info-" + str(index), "GET", self.route("/Info", alias), token=self.admin())
            observation["lookups"].append({"alias": alias, "status": status, "body": info})
            if (status == 200 and isinstance(info, dict) and isinstance(info.get("Id"), str) and
                    info["Id"] in mapped_ids and info.get("ReportedDeviceId") == self.server_public_id):
                self.server_candidates[info["Id"]] = info
        self.key_device_observations.append(observation)

    def server_key_mapping_reason(self, selected: dict, rows: list[dict]) -> str | None:
        if not isinstance(selected.get("Id"), str) or not selected["Id"].isdigit() or int(selected["Id"]) <= 0 or selected.get("ReportedDeviceId") != self.server_public_id:
            return "Selected DeviceInfo does not identify the positive numeric server device"
        if len(self.owned) != 2 or len(rows) != 2 or {row.get("AccessToken") for row in rows} != set(self.owned):
            return "Current key membership does not contain exactly both acknowledged owned credentials"
        if {row.get("AppName") for row in rows} != set(KEY_APPS.values()):
            return "Current key membership does not contain both reserved application labels"
        if any(not isinstance(row.get("DeviceId"), int) or isinstance(row["DeviceId"], bool) or
               str(row["DeviceId"]) != selected["Id"] or row.get("ReportedDeviceId") != self.server_public_id
               for row in rows):
            return "Both owned keys do not map to the selected numeric and reported server identity"
        return None

    def maybe_delete_server_device(self) -> None:
        gate_reason = self.server_creation_reason()
        if self.server_creation_gate.get("allowed") is True and gate_reason is None:
            self.read_server_candidates("server-delete-refresh", self.devices("server-delete-refresh-devices"))
        candidates = list(self.server_candidates.values())
        reason = None
        if self.server_creation_gate.get("allowed") is not True or gate_reason is not None:
            reason = gate_reason or "The server creation gate did not authorize application-key traffic"
        elif self.old_keys != []:
            reason = "Existing application keys prevent exclusive ownership"
        elif self.server_baseline_list:
            reason = "Server device existed in the baseline device list"
        elif self.server_baseline_info.get(self.server_public_id, {}).get("status") != 404:
            reason = "Reported server alias was not absent with HTTP 404 before key creation"
        elif len(candidates) != 1:
            reason = "Key numeric mapping did not identify one positive server DeviceInfo"
        else:
            selected = candidates[0]
            # Creation ownership differs from repeat-delete absence. A new
            # numeric ID may differ from the historical numeric alias. The
            # baseline reported-ID 404, absent list membership, empty old key
            # set, and positive post-creation key mapping establish ownership.
            # Any positive baseline alias remains protected independently.
            if any(str(selected.get(field)) in self.protected_device_aliases for field in
                   ("Id", "ReportedDeviceId", "InternalId") if selected.get(field) is not None):
                reason = "Mapped server device collides with a protected baseline alias"
            else:
                reason = self.server_key_mapping_reason(selected, self.key_list("server-delete-key-ownership"))
        self.server_branch = {"verified": False, "deleteAttempted": False, "reason": reason,
                              "baselineInfo": self.server_baseline_info, "baselineListed": self.server_baseline_list,
                              "candidateIds": sorted(self.server_candidates)}
        if reason is not None:
            return
        selected = candidates[0]
        self.virtual_write_id = selected["Id"]
        self.owned_devices[selected["Id"]] = selected
        self.read_ids.add(selected["Id"])
        self.server_branch["deleteAttempted"] = True
        status, _ = self.mutate_device("delete-owned-server-device", "DELETE", selected["Id"], token=self.admin())
        self.server_branch.update({"deleteStatus": status, "verified": status == 204,
                                   "reason": "Explicit old absence and current owned-key numeric mapping proved"})
        self.request("server-delete-sessions-before-key-probes", "GET", "/emby/Sessions", token=self.admin())
        self.request("server-delete-info-after", "GET", self.route("/Info", selected["Id"]), token=self.admin())
        self.devices("server-delete-devices-after")
        self.key_list("server-delete-keys-after")
        self.probe_keys("server-delete-key-protected")
        self.devices("server-delete-devices-after-key-probes")
        base.require(self.probe_login("server-delete-control-protected", "control") == 200,
                     "Server device deletion invalidated the independent control login")

    def capture(self) -> None:
        status, public = self.request("public-before", "GET", "/emby/System/Info/Public")
        base.require(status == 200 and isinstance(public, dict) and public.get("Version") == "4.9.5.0" and
                     isinstance(public.get("Id"), str) and public["Id"], "Reference version or public server ID differs")
        self.server_public_id = public["Id"]
        self.read_ids.add(self.server_public_id)
        self.login("control")
        rows = self.devices("devices-baseline")
        self.control_id = self.find_owned(rows, "control", "Control")
        base.require(all(not str(row.get("ReportedDeviceId", "")).startswith(device.DEVICE_PREFIX) or
                     row["Id"] == self.control_id for row in rows), "Reserved client metadata already exists")
        self.old_devices = {row["Id"]: row for row in rows if row["Id"] != self.control_id}
        base.require(len(self.old_devices) <= device.MAX_OLD_DEVICES, "Old device count exceeds the request reserve")
        base.require(set(self.old_devices) == self.previous_device_ids,
                     "Baseline list does not contain exactly the previously audited device membership and fresh control")
        self.device_baseline_complete = True
        for row in rows:
            for value in (row.get("Id"), row.get("ReportedDeviceId"), row.get("InternalId")):
                if isinstance(value, (str, int)) and not isinstance(value, bool) and str(value):
                    self.protected_device_aliases.add(str(value))
        self.read_ids.update(self.old_devices)
        base.save(base.PRIVATE / "old-devices.json", self.old_devices)
        for index, device_id in enumerate(sorted(self.old_devices)):
            status, options = self.request("old-options-before-" + str(index), "GET", self.route("/Options", device_id), token=self.admin())
            self.old_options[device_id] = {"status": status, "body": options}
        base.save(base.PRIVATE / "old-options.json", self.old_options)
        status, users = self.request("users-baseline", "GET", "/emby/Users", token=self.admin())
        base.require(status == 200 and isinstance(users, list) and all(isinstance(user, dict) and user.get("Id") for user in users),
                     "User baseline is unavailable")
        self.old_users = users
        base.save(base.PRIVATE / "old-users.json", users)
        self.old_keys = self.key_list("keys-baseline")
        self.old_key_baseline_complete = True
        base.save(base.PRIVATE / "old-keys.json", self.old_keys)
        self.server_baseline_list = [row for row in rows if row.get("ReportedDeviceId") == self.server_public_id]
        for index, alias in enumerate((self.server_public_id, PREVIOUS_SERVER_NUMERIC_ID)):
            status, info = self.request("server-info-before-" + str(index), "GET", self.route("/Info", alias), token=self.admin())
            self.server_baseline_info[alias] = {"status": status, "body": info}
            if status == 200 and isinstance(info, dict):
                for field in ("Id", "ReportedDeviceId", "InternalId"):
                    value = info.get(field)
                    if isinstance(value, (str, int)) and not isinstance(value, bool) and str(value):
                        self.protected_device_aliases.add(str(value))
                canonical = info.get("Id")
                if isinstance(canonical, str) and canonical and canonical not in self.old_devices and canonical != self.control_id:
                    self.protected_hidden_devices[canonical] = info
                    self.read_ids.add(canonical)
                    option_status, options = self.request("hidden-old-options-before-" + str(index), "GET",
                                                          self.route("/Options", canonical), token=self.admin())
                    self.protected_hidden_options[canonical] = {"status": option_status, "body": options}
        self.request("sessions-baseline", "GET", "/emby/Sessions", token=self.admin())
        self.establish_server_creation_gate()
        self.login("viewer")
        rows = self.devices("devices-after-viewer")
        viewer_id = self.find_owned(rows, "viewer", "Viewer")
        base.require(self.probe_login("viewer-protected-before", "viewer") == 200 and
                     self.probe_login("control-protected-before", "control") == 200, "Fresh ordinary credentials are unusable")
        for name, app in KEY_APPS.items():
            status, _ = self.request("create-key-" + name, "POST", "/emby/Auth/Keys?" + urlencode({"App": app}), token=self.admin())
            self.key_list("keys-created-" + name)
            base.require(status == 204 and self.token_for(name), "Key creation differs from the observed 204 contract")
        base.require(len(self.owned) == 2, "Expected exactly two independent owned keys")
        for name in KEY_APPS:
            key = self.token_for(name)
            self.request("key-" + name + "-devices-token-only", "GET", "/emby/Devices", token=key)
            base.require(self.probe_key("key-" + name + "-sessions-token-only", name) == 200, "Owned key is unusable")
        self.read_server_candidates("keys-token-only-server", self.devices("devices-after-key-token-only"))
        for client, values in KEY_CLIENTS.items():
            key = self.token_for(values[0])
            self.request("key-" + client + "-devices-metadata", "GET", "/emby/Devices", token=key, client=client)
            self.probe_key("key-" + client + "-sessions-metadata", values[0], client=client)
            self.devices("devices-after-key-" + client)
            self.key_list("keys-after-client-" + client)
        rows = self.devices("devices-after-key-clients")
        self.read_server_candidates("keys-client-server", rows)
        key = self.token_for("alpha")
        status, _ = self.mutate_device("key-rename-viewer", "POST", viewer_id, suffix="/Options", token=key,
                                       body={"CustomName": device.CUSTOM_PREFIX + "Viewer Renamed By Key"})
        base.require(status == 204, "Key rename of the positively owned viewer device was not acknowledged")
        self.info_options("viewer-after-key-rename", viewer_id)
        status, _ = self.mutate_device("key-delete-viewer", "DELETE", viewer_id, token=key)
        base.require(status == 204, "Key deletion of the positively owned viewer device was not acknowledged")
        self.request("viewer-after-key-delete-info", "GET", self.route("/Info", viewer_id), token=self.admin())
        self.devices("devices-after-key-delete-viewer")
        base.require(self.probe_login("viewer-after-key-delete-protected", "viewer") == 401 and
                     self.probe_login("control-after-key-delete-protected", "control") == 200,
                     "Viewer deletion did not preserve independent control authority and invalidate the viewer")
        for name in KEY_APPS:
            base.require(self.probe_key("viewer-delete-key-protected-" + name, name) == 200,
                         "Viewer device deletion invalidated an independent owned key")
        rows = self.devices("header-delete-candidates")
        metadata = self.client_metadata("alpha")
        matches = [row for row in rows if row.get("ReportedDeviceId") == metadata["DeviceId"] and row.get("AppName") == metadata["Client"]]
        if len(matches) == 1:
            target = matches[0]["Id"]
            self.header_delete_branch = {"verified": False, "deleteAttempted": True, "targetId": target,
                                         "reason": "One current owned Alpha header device matched"}
            status, _ = self.mutate_device("delete-owned-key-header-device", "DELETE", target, token=self.admin())
            self.header_delete_branch.update({"verified": status == 204, "deleteStatus": status})
            self.request("header-delete-sessions-before-key-probes", "GET", "/emby/Sessions", token=self.admin())
            self.request("header-delete-info-after", "GET", self.route("/Info", target), token=self.admin())
            self.devices("header-delete-devices-after")
            self.key_list("header-delete-keys-after")
            self.probe_keys("header-delete-key-protected")
            self.devices("header-delete-devices-after-key-probes")
            base.require(self.probe_login("header-delete-control-protected", "control") == 200,
                         "Header device deletion invalidated the independent control login")
        else:
            self.header_delete_branch = {"verified": False, "deleteAttempted": False, "matchedRows": len(matches),
                                         "reason": "Alpha header did not create one exclusively identified list device"}
        self.maybe_delete_server_device()

    def compare_devices(self) -> None:
        super().compare_devices()
        hidden_unchanged = True
        for index, (device_id, expected) in enumerate(self.protected_hidden_devices.items()):
            status, info = self.request("hidden-old-info-final-" + str(index), "GET", self.route("/Info", device_id), token=self.admin())
            option_status, options = self.request("hidden-old-options-final-" + str(index), "GET",
                                                  self.route("/Options", device_id), token=self.admin())
            hidden_unchanged = hidden_unchanged and status == 200 and info == expected and self.protected_hidden_options.get(device_id) == {"status": option_status, "body": options}
        self.checks["oldHiddenDeviceInfoAndOptionsUnchanged"] = hidden_unchanged
        rows = self.final_devices or []
        retained_candidates = [row for row in rows if row["Id"] not in self.old_devices and
                               not self.owned_pair(row) and row["Id"] in self.server_candidates and
                               all(row.get(field) == self.server_candidates[row["Id"]].get(field)
                                   for field in ("Id", "ReportedDeviceId", "AppName"))]
        self.checks.pop("noUnownedNewDevices", None)
        self.checks["noUnexplainedNewDevices"] = all(row["Id"] in self.old_devices or self.owned_pair(row) or
                                                    row in retained_candidates for row in rows)
        self.server_branch["retainedMappedServerDevices"] = retained_candidates
        self.server_branch["retentionDoesNotProveDeletionCompatibility"] = bool(retained_candidates)

    def finish(self) -> None:
        self.finishing = True
        self.deadline = time.monotonic() + device.PHASE_SECONDS
        if "control" in self.logins:
            if self.old_keys is not None:
                current = self.cleanup_step("discover-owned-keys", lambda: self.key_list("cleanup-keys-discover"))
                for index, token in enumerate(list(self.owned)):
                    self.cleanup_step("delete-key-" + str(index), lambda token=token, index=index:
                        self.request("cleanup-delete-key-" + str(index), "DELETE", "/emby/Auth/Keys/" + quote(token, safe=""), token=self.admin()))
                final = self.cleanup_step("compare-keys", lambda: self.key_list("cleanup-keys-final"))
                self.old_key_audit_complete = current is not None and final is not None and sorted(final, key=lambda row: row["AccessToken"]) == sorted(self.old_keys, key=lambda row: row["AccessToken"])
                self.cleanup_step("probe-retired-keys", lambda: self.probe_keys("cleanup-key-invalid"))
            self.cleanup_step("logout-viewer", lambda: self.logout("viewer"))
            rows = self.cleanup_step("discover-owned-devices", lambda: self.devices("cleanup-devices-discover"))
            if rows is not None and self.old_devices is not None:
                for index, row in enumerate(rows):
                    target = row["Id"]
                    if self.owned_pair(row) and target != self.control_id and target not in self.ambiguous_ids:
                        self.cleanup_step("delete-device-" + str(index), lambda target=target, index=index:
                            self.mutate_device("cleanup-delete-device-" + str(index), "DELETE", target, token=self.admin()))
            if self.old_devices is not None:
                self.cleanup_step("compare-devices", self.compare_devices)
            if self.old_users is not None:
                self.cleanup_step("compare-users", self.compare_users)
            self.cleanup_step("sessions-final", lambda: self.request("sessions-final-before-control-logout", "GET", "/emby/Sessions", token=self.admin()))
            self.cleanup_step("logout-control", lambda: self.logout("control"))
        expected_scopes = set()
        for token in self.owned:
            name, credential_id = self.key_name(token), self.key_credential_ids[token]
            expected_scopes.add(credential_id + ":token-only")
            expected_scopes.update(credential_id + ":" + client for client, values in KEY_CLIENTS.items() if values[0] == name)
        self.checks["allAcknowledgedKeyContextsInvalid"] = expected_scopes == set(self.key_invalidity_proofs)
        self.checks["oldKeysUnchangedAndOwnedKeysRemoved"] = self.old_key_audit_complete
        self.checks["allAcknowledgedLoginsInvalid"] = set(self.logins) == set(self.invalid_login_proofs)
        self.checks["allAttemptedLoginsAcknowledgedOrRejected"] = all(name in self.logins or self.login_statuses.get(name) in {400, 401, 403} for name in self.login_attempts)
        self.checks["referenceProcessUnchanged"] = self.cleanup_step("process-audit", device.preconditions) == self.pid
        self.checks["oldRecordsSourcesPrivateFilesUnchanged"] = self.cleanup_step("old-file-audit", self.snapshot) == self.baseline
        self.cleanup_step("private-mutation-journal", lambda: base.save(base.PRIVATE / "mutation-results.json", self.mutations))
        redaction_ok = True
        for path in base.RAW.glob(base.PREFIX + "*.json"):
            base.private_file(path)
            base.private_file(base.EXPORT / path.name)
            redaction_ok = redaction_ok and self.sanitize(json.loads(path.read_text())) == json.loads((base.EXPORT / path.name).read_text())
        self.checks["newRedactionAuditPassed"] = redaction_ok
        self.cleanup_ok = not self.cleanup_errors and all(self.checks.values())
        self.write("audit", {"kind": "capture-audit-observation", "referencePID": self.pid,
            "captureFailureType": self.capture_failure, "cleanupPassed": self.cleanup_ok, "checks": self.checks,
            "cleanupErrors": self.cleanup_errors, "preservedOldRecords": PREVIOUS_RECORDS,
            "preservedOldRecordFiles": len(self.baseline["records"]), "preservedOldMediaFiles": len(self.baseline["media"]),
            "preservedOldPrivateFiles": len(self.baseline["privateFiles"]), "oldDeviceCount": None if self.old_devices is None else len(self.old_devices),
            "oldDeviceOptionsAudited": len(self.old_options), "oldDeviceActivityDifferences": self.old_device_date_changes,
            "protectedHiddenDeviceIds": sorted(self.protected_hidden_devices),
            "previousAuditedDeviceIds": sorted(self.previous_device_ids),
            "attributedUserActivityDifferences": self.user_date_changes, "ownedLoginAttempts": sorted(self.login_attempts),
            "ownedLoginStatuses": self.login_statuses, "ownedLoginLogoutStatuses": self.logout_statuses,
            "ownedLoginInvalidityProofs": self.invalid_login_proofs, "ownedDeviceObservations": self.device_observations,
            "ownedDeviceWriteProofs": self.write_proofs, "ownedMutations": self.mutations,
            "keyCreationStatuses": self.key_creation_statuses, "ownedKeysAcknowledged": len(self.owned),
            "keyRowsObservations": self.key_rows_observations, "keyDeviceMappingObservations": self.key_device_observations,
            "keyProtectedRouteObservations": self.key_probe_observations, "keyInvalidityProofs": self.key_invalidity_proofs,
            "keyDeleteAcknowledgements": self.key_delete_acknowledgements, "headerDeviceDeletion": self.header_delete_branch,
            "serverCreationGate": self.server_creation_gate,
            "serverDeviceDeletion": self.server_branch, "retainedOwnedHistory": None if self.final_devices is None else
                [row for row in self.final_devices if self.owned_pair(row)],
            "retainedHistoryObservation": "Device list before final control logout; the owned credential is then independently probed",
            "existingCredentialHTTPRequests": 0, "sourceWrites": 0, "cameraUploadRequests": 0,
            "encoderRequests": 0, "userPolicyWrites": 0, "httpAttempts": self.record_count,
            "incompleteHTTP": self.incomplete_count, "wireBytes": self.total, "wireByteBudgetCharged": self.charged_bytes,
            "elapsedSeconds": round(time.monotonic() - self.started, 3),
            "limits": {"oldDevices": device.MAX_OLD_DEVICES, "httpAttempts": device.MAX_REQUESTS,
                       "mainHTTPAttempts": device.MAIN_REQUESTS, "responseBytes": base.MAX_BODY,
                       "totalResponseBytes": base.MAX_TOTAL, "phaseSeconds": device.PHASE_SECONDS,
                       "socketTimeoutSeconds": 5, "requestDeadlineSeconds": 15}})
        base.require(self.cleanup_ok, "Cleanup or preservation proof is incomplete; inspect private evidence")


def main() -> None:
    signal.signal(signal.SIGALRM, device.deadline_expired)
    recorder = Recorder(device.preconditions())
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
