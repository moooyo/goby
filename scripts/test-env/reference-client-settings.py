#!/usr/bin/env python3
"""Research owned UserSettings write contracts with verified restoration.

Only fresh viewer2 keys prefixed m3e_ may change. The primary browser viewer
receives reads and, only after empirical proof, an empty Partial no-op. No
user configuration, policy, password, library, media, or display preferences
write is allowed. Results are protocol research, never client acceptance.
"""

from __future__ import annotations

import hashlib
import http.client
import importlib.util
import json
import os
import fcntl
from pathlib import Path
import secrets
import signal
import subprocess
import sys
import time
from urllib.parse import urlencode, urlsplit

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
ROOT = WORK / "reference-client-settings-v1"
PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
STATE = PRIVATE / "state.json"
LOCK = PRIVATE / "operator.lock"
MARKER = "goby-m3e-reference-client-settings-v1"
PREFIX = "m3e_"
MAX_REQUESTS, MAX_WRITE, MAX_READ = 100, 4096, 65536
PID, TICKS = 332054, "357218"
DEVICE_PREFIX = "goby-m3e-settings-research-"


class SettingsError(Exception):
    """A settings-research guard, restoration, or ownership check failed."""


def require(condition, message):
    if not condition:
        raise SettingsError(message)


def load_support():
    source = Path(__file__).absolute().with_name("reference-client-initialization.py")
    expected = json.loads((WORK / "reference-client-initialization-v1/export/safety-report.json").read_text())["sourceSha256"]
    require(hashlib.sha256(source.read_bytes()).hexdigest() == expected, "The accepted initialization recorder source changed.")
    spec = importlib.util.spec_from_file_location("reference_initialization_support", source)
    support = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(support)
    operator = support.load_operator()
    operator.canonical(source)
    return support, operator


def encoded(value):
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def validate_probe_body(raw, *, allow_invalid=False):
    require(isinstance(raw, str) and len(raw.encode()) <= MAX_WRITE, "A settings probe exceeds its 4 KiB write bound.")
    if allow_invalid:
        require(raw in ('["m3e_array_probe"]', '"m3e_scalar_probe"', '{"m3e_malformed":'),
                "An unreviewed invalid payload is forbidden.")
        return ["m3e_malformed"] if raw == '{"m3e_malformed":' else []
    pairs = json.loads(raw, object_pairs_hook=lambda rows: rows)
    require(raw.lstrip().startswith("{") and isinstance(pairs, list) and all(isinstance(row, tuple) and len(row) == 2 and
            isinstance(row[0], str) and row[0].startswith(PREFIX) for row in pairs),
            "Only explicitly prefixed top-level probe keys may be written.")
    return [row[0] for row in pairs]


def same_worker_lifetime(current, expected):
    return all(current.get(key) == expected.get(key) for key in ("pid", "bootId", "startTicks"))


class Recorder:
    def __init__(self, support, operator, owner, browser, *, recover=False):
        self.support, self.op, self.owner = support, operator, owner
        self.accounts = {key: browser["accounts"][key] for key in ("viewer", "viewer2")}
        self.tokens, self.verified, self.revoked = {}, set(), set()
        self.authorized_keys = set()
        self.secrets = {row["password"] for row in browser["accounts"].values()}
        self.records, self.observations = {}, []
        self.count, self.write_count = 0, 0
        self.baseline, self.current = {}, {}
        self.cleanup_mode = None
        self.partial_empty_noop = False
        self.cleanup_passed = False
        self.configuration_preserved = False
        self.primary_preserved = False
        self.media_preserved = False
        self.finishing = False
        self.started = time.monotonic()
        self.research_deadline = self.started + 90
        self.cleanup_deadline = self.started + 225
        self.recovery_attempt = 0
        if recover:
            saved = operator.read_private(STATE)
            require(saved.get("marker") == MARKER and saved.get("serviceIdentity") == owner["serviceIdentity"],
                    "The retained restoration state belongs to another fixture.")
            self.count, self.write_count = saved["count"], saved["writeCount"]
            self.baseline, self.current = saved["baseline"], saved["current"]
            self.cleanup_mode, self.partial_empty_noop = saved["cleanupMode"], saved["partialEmptyNoop"]
            self.tokens = saved["tokens"]
            self.revoked = set(saved["revoked"])
            self.authorized_keys = set(saved.get("authorizedKeys", []))
            self.cleanup_passed = saved.get("cleanupPassed", False)
            self.primary_preserved = saved.get("primarySettingsPreserved", False)
            self.configuration_preserved = saved.get("bothUserConfigurationsAndPoliciesPreserved", False)
            self.media_preserved = saved.get("sourceMediaPreserved", False)
            self.recovery_attempt = saved.get("recoveryAttempt", 0) + 1
            require(self.recovery_attempt <= 3 and self.count < MAX_REQUESTS, "The retained recovery budget is exhausted.")
            require(set(self.tokens) <= set(self.accounts), "A retained token belongs to an unowned account.")
            for account in self.accounts:
                if not operator.present(PRIVATE / (account + "-login.json")):
                    require(account not in self.tokens, "A retained token has no durable login acknowledgement.")
                    continue
                login = operator.read_private(PRIVATE / (account + "-login.json"))
                token = login.get("AccessToken")
                require(isinstance(token, str) and token and self.tokens.get(account, token) == token and
                        login.get("ServerId") == owner["serverId"] and
                        login.get("User", {}).get("Id") == self.accounts[account]["userId"] and
                        login["User"].get("Name") == self.accounts[account]["username"] and
                        login["User"].get("Policy", {}).get("IsAdministrator") is False and
                        login.get("SessionInfo", {}).get("DeviceId") == DEVICE_PREFIX + account + "-v1",
                        "A retained recorder token lacks its exact private identity proof.")
                self.tokens[account] = token
                self.verified.add(account)
                self.secrets.add(token)
        self.worker = operator.process_identity(os.getpid())
        self.persist()

    def persist(self):
        value = {"marker": MARKER, "serviceIdentity": self.owner["serviceIdentity"], "worker": self.worker,
                 "count": self.count, "writeCount": self.write_count, "baseline": self.baseline, "current": self.current,
                 "cleanupMode": self.cleanup_mode, "partialEmptyNoop": self.partial_empty_noop, "tokens": self.tokens,
                 "verified": sorted(self.verified), "revoked": sorted(self.revoked), "recoveryAttempt": self.recovery_attempt,
                 "authorizedKeys": sorted(self.authorized_keys), "cleanupPassed": self.cleanup_passed,
                 "primarySettingsPreserved": self.primary_preserved,
                 "bothUserConfigurationsAndPoliciesPreserved": self.configuration_preserved,
                 "sourceMediaPreserved": self.media_preserved,
                 "phase": "restoring" if self.finishing else "research"}
        if self.op.present(STATE):
            require(self.op.read_private(STATE).get("marker") == MARKER, "An unrelated state file cannot be replaced.")
            temporary = PRIVATE / ("state.next-" + secrets.token_hex(10) + ".json")
            self.op.save(temporary, value)
            os.replace(temporary, STATE)
            self.op.sync_directory(PRIVATE)
        else:
            self.op.save(STATE, value)

    def identity(self):
        require(self.op.service_identity(self.owner) == self.owner["serviceIdentity"], "The fresh reference process changed.")
        identity = self.owner["serviceIdentity"]
        require(identity["pid"] == PID and identity["startTicks"] == TICKS and
                os.readlink("/proc/self/ns/net") == identity["networkNamespace"],
                "Settings research is outside the exact fresh reference namespace.")

    def route(self, account, partial=False):
        return "/emby/usersettings/" + self.accounts[account]["userId"] + ("/Partial" if partial else "")

    def written_keys(self, raw, invalid):
        return validate_probe_body(raw, allow_invalid=invalid)

    def approve(self, method, route, account, raw, content_type, *, invalid=False, cleanup=False):
        require(account in self.accounts and self.count < MAX_REQUESTS, "The account or request budget is outside this study.")
        parsed = urlsplit(route)
        require(not parsed.scheme and not parsed.netloc and not parsed.query and not parsed.fragment,
                "Only exact fresh-reference paths without query credentials are allowed.")
        if route == "/emby/Users/AuthenticateByName":
            require(method == "POST" and raw is None, "Only the separately encoded recorder login is allowed.")
            return
        if route == "/emby/Sessions/Logout":
            require(method == "POST" and raw is None and account in self.verified, "An unproven session cannot be logged out.")
            return
        if method == "GET":
            allowed = {"/emby/Sessions"} | {self.route(key) for key in self.accounts} | {
                "/emby/Users/" + row["userId"] for row in self.accounts.values()}
            require(route in allowed and raw is None, "The read is outside the two owned users and recorder revocation proof.")
            return
        require(method == "POST" and content_type in ("application/json", "text/plain", "application/octet-stream"),
                "The settings method or content type is outside the reviewed probes.")
        require(cleanup or self.count <= MAX_REQUESTS - 16, "The request budget must retain enough capacity for restoration.")
        if route == self.route("viewer", partial=True):
            require(account == "viewer2" and self.partial_empty_noop and raw == "{}" and not invalid,
                    "The primary browser user permits only a previously proven empty Partial no-op.")
            return
        require(route in (self.route("viewer2"), self.route("viewer2", partial=True)),
                "Settings writes are restricted to the fresh secondary viewer.")
        validate_probe_body(raw, allow_invalid=invalid)
        if account == "viewer":
            require(route == self.route("viewer2", partial=True) and self.partial_empty_noop and raw == "{}" and not invalid,
                    "Primary-viewer authentication permits only an empirically proven empty Partial no-op.")
        require("viewer2" in self.baseline and self.baseline["viewer2"]["settings"] == {},
                "A write requires the exact empty secondary-user baseline.")

    def request(self, label, method, route, *, account="viewer2", raw=None, content_type="application/json",
                invalid=False, cleanup=False, login=False):
        self.identity()
        require(time.monotonic() < (self.cleanup_deadline if self.finishing else self.research_deadline),
                "The research or reserved restoration deadline expired.")
        self.approve(method, route, account, raw, content_type, invalid=invalid, cleanup=cleanup)
        number = self.count + 1
        headers = {"Accept": "application/json", "Authorization":
                   'Emby Client="Goby UserSettings Contract Recorder", Device="Linux Contract Research", DeviceId="' +
                   DEVICE_PREFIX + account + '-v1", Version="1.0"'}
        payload = raw.encode() if raw is not None else None
        if login:
            require(route == "/emby/Users/AuthenticateByName" and method == "POST", "Unexpected credential-bearing request.")
            payload = urlencode({"Username": self.accounts[account]["username"], "Pw": self.accounts[account]["password"]}).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        else:
            require(account in self.tokens and account in self.verified, "The recorder account token is unproven.")
            headers["X-Emby-Token"] = self.tokens[account]
            if raw is not None:
                headers["Content-Type"] = content_type
        intent = {"marker": MARKER, "sequence": number, "case": label, "account": account, "method": method,
                  "path": route, "contentType": headers.get("Content-Type"), "body": raw,
                  "authenticationBodyRetained": False, "settingsBefore": self.current.get("viewer2"),
                  "primarySettingsBefore": self.current.get("viewer"), "cleanup": cleanup}
        self.count = number
        if method == "POST" and route.startswith("/emby/usersettings/"):
            self.write_count += 1
            self.authorized_keys.update(self.written_keys(raw, invalid))
        self.persist()
        self.op.save(PRIVATE / f"{number:03d}-{label}-intent.json", intent)
        connection = http.client.HTTPConnection("127.0.0.1", self.op.PORT, timeout=8)
        try:
            signal.setitimer(signal.ITIMER_REAL, 8)
            connection.request(method, route, body=payload, headers=headers)
            response = connection.getresponse()
            status = response.status
            response_headers = dict(response.getheaders())
            length = response.getheader("Content-Length")
            received = response.read(MAX_READ)
            require(len(received) < MAX_READ and (length is None or length.isdigit() and int(length) == len(received)),
                    "The bounded settings response is incomplete or oversized.")
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        try:
            result = json.loads(received) if received else None
            kind = "json" if received else "empty"
        except (ValueError, UnicodeDecodeError):
            result, kind = received.decode("utf-8", errors="replace"), "text"
        if login and isinstance(result, dict) and isinstance(result.get("AccessToken"), str) and result["AccessToken"]:
            self.tokens[account] = result["AccessToken"]
            self.secrets.add(self.tokens[account])
            self.op.save(PRIVATE / (account + "-login.json"), result)
            self.persist()
            self.identity()
            require(status == 200 and result.get("ServerId") == self.owner["serverId"] and
                    result.get("User", {}).get("Id") == self.accounts[account]["userId"] and
                    result["User"].get("Name") == self.accounts[account]["username"] and
                    result["User"].get("Policy", {}).get("IsAdministrator") is False and
                    result.get("SessionInfo", {}).get("DeviceId") == DEVICE_PREFIX + account + "-v1",
                    "The acknowledged token is not the exact new ordinary recorder session.")
            self.verified.add(account)
            self.persist()
        self.identity()
        record = {"classification": "protocol research; not client acceptance", "case": label,
                  "request": {"method": method, "path": route, "account": account, "contentType": headers.get("Content-Type"),
                              "body": raw, "authenticationBodyRetained": False, "cleanup": cleanup},
                  "response": {"status": status, "headers": response_headers, "contentType": response.getheader("Content-Type"),
                               "body": result, "bodyKind": kind, "bodyStructure": self.support.structure(result),
                               "bytesRead": len(received), "complete": True}}
        self.support.collect_secrets(record, self.secrets)
        record = self.support.sanitize(record, self.secrets)
        self.op.save(PRIVATE / f"{number:03d}-{label}-response.json", record)
        self.records[label] = record
        return status, result

    def login(self, account):
        status, _ = self.request(account + "-login", "POST", "/emby/Users/AuthenticateByName", account=account, login=True)
        require(status == 200 and account in self.verified, "The owned recorder login failed.")

    def read_settings(self, label, target="viewer2", account=None):
        status, result = self.request(label, "GET", self.route(target), account=account or target)
        if account is None or account == target:
            require(status == 200 and isinstance(result, dict), "The owned user settings state is unavailable.")
            if target == "viewer2":
                require(all(isinstance(key, str) and key in self.authorized_keys for key in result),
                        "The secondary user contains an unowned setting; refusing automatic replacement or deletion.")
            self.current[target] = result
            self.persist()
        return status, result

    def write(self, label, value=None, *, partial=False, content_type="application/json", raw=None, invalid=False,
              account="viewer2", target="viewer2", cleanup=False):
        if raw is None:
            raw = encoded(value)
        require(cleanup or self.count + 3 <= MAX_REQUESTS - 16,
                "A complete write-and-observe case must retain the independent restoration budget.")
        before = self.read_settings(label + "-before", target=target)[1]
        if target == "viewer":
            require(before == self.baseline["viewer"]["settings"], "The primary browser settings changed; its cross-user no-op is postponed.")
        status, body = self.request(label, "POST", self.route(target, partial), account=account, raw=raw,
                                    content_type=content_type, invalid=invalid, cleanup=cleanup)
        after = self.read_settings(label + "-after", target=target)[1]
        self.observations.append({"case": label, "method": "Partial" if partial else "Full", "contentType": content_type,
                                  "account": account, "target": target, "requestBody": raw, "status": status,
                                  "responseBody": body, "before": before, "after": after, "cleanup": cleanup})
        if target == "viewer":
            require(after == before, "The proven primary-user no-op changed its settings.")
        return status, after

    def baseline_users(self):
        for account in ("viewer2", "viewer"):
            self.login(account)
            status, dto = self.request(account + "-dto-before", "GET", "/emby/Users/" + self.accounts[account]["userId"], account=account)
            require(status == 200 and isinstance(dto, dict), "The owned account configuration baseline is unavailable.")
            settings = self.read_settings(account + "-settings-baseline", target=account)[1]
            if account == "viewer2":
                require(settings == {}, "The secondary user is not empty; no settings write is authorized.")
            self.baseline[account] = {"settings": settings, "Configuration": dto.get("Configuration"), "Policy": dto.get("Policy")}
            self.op.save(PRIVATE / (account + "-baseline.json"), self.baseline[account])
            self.persist()

    def establish_restoration(self):
        seed = {"m3e_full_a": "one", "m3e_full_b": "two"}
        _, observed = self.write("full-seed", seed)
        require(observed == seed, "A full JSON object did not persist the exact owned string map; no replacement semantics were inferred.")
        _, second = self.write("full-second", {"m3e_full_a": "replacement"})
        require(second.get("m3e_full_a") == "replacement" and set(second) in
                ({"m3e_full_a"}, {"m3e_full_a", "m3e_full_b"}) and second.get("m3e_full_b", "two") == "two",
                "The full-write replacement versus merge observation is not a valid persisted string-map control.")
        _, empty = self.write("full-empty-discovery", {})
        if empty == {}:
            self.cleanup_mode = "full-empty-replacement"
            self.persist()
            return
        require(empty and all(key.startswith(PREFIX) for key in empty), "The initial restoration population is not exclusively owned.")
        key = next(iter(empty))
        _, after = self.write("partial-null-discovery", {key: None}, partial=True)
        if key not in after:
            self.cleanup_mode = "partial-null-deletion"
        else:
            _, after = self.write("full-null-discovery", {key: None})
            if key not in after:
                self.cleanup_mode = "full-null-deletion"
        require(self.cleanup_mode is not None, "No safe restoration semantics were established; only owned probe keys remain for inspection.")
        self.persist()
        self.restore_settings("discovery")

    def cases(self):
        expected_seed = {"m3e_keep": "stay", "m3e_change": "before"}
        _, seed = self.write("partial-seed", expected_seed)
        require(seed == expected_seed, "The Partial controls lack their exact nonempty persisted sentinel map.")
        _, changed = self.write("partial-json-merge", {"m3e_change": "after", "m3e_new": "added"}, partial=True)
        require(changed.get("m3e_change") == "after" and changed.get("m3e_new") == "added",
                "The Partial control did not persist its owned update and addition.")
        before = dict(self.current["viewer2"])
        status, after = self.write("partial-empty-noop", {}, partial=True)
        self.partial_empty_noop = bool(before) and status in (200, 204) and after == before
        self.persist()
        for kind, content_type in (("text", "text/plain"), ("octet", "application/octet-stream")):
            self.write("partial-" + kind, {"m3e_partial_" + kind: kind + "-value"}, partial=True, content_type=content_type)
        self.write("full-text", {"m3e_full_text": "text-value"}, content_type="text/plain")
        self.write("full-octet", {"m3e_full_octet": "octet-value"}, content_type="application/octet-stream")
        self.write("string-values", {"m3e_null_target": "remove", "m3e_empty": "", "m3e_unicode": "caf\u00e9 \u96ea \U0001f3b5",
                                      "m3e_case": "lower", "m3e_Case": "upper"})
        self.write("partial-null", {"m3e_null_target": None}, partial=True)
        self.write("partial-case-update", {"m3e_case": "updated-lower"}, partial=True)
        self.write("full-duplicate-key", raw='{"m3e_duplicate":"first","m3e_duplicate":"second"}')
        self.write("partial-duplicate-key", partial=True, raw='{"m3e_duplicate":"third","m3e_duplicate":"fourth"}')
        self.write("full-array-type", raw='["m3e_array_probe"]', invalid=True)
        self.write("partial-object-type", {"m3e_type_object": {"nested": "value"}}, partial=True)
        self.write("partial-number-type", {"m3e_type_number": 123}, partial=True)
        self.write("partial-boolean-type", {"m3e_type_boolean": True}, partial=True)
        self.write("full-malformed-json", raw='{"m3e_malformed":', invalid=True)
        self.read_settings("viewer2-reads-primary", target="viewer", account="viewer2")
        self.read_settings("primary-reads-viewer2", target="viewer2", account="viewer")
        if self.partial_empty_noop:
            self.write("primary-viewer2-partial-noop", {}, partial=True, account="viewer")
            self.write("viewer2-primary-partial-noop", {}, partial=True, account="viewer2", target="viewer")
        else:
            self.observations.append({"case": "viewer2-primary-partial-noop", "attempted": False,
                                      "reason": "Empty Partial was not proven to preserve settings."})

    def restore_settings(self, label):
        state = self.read_settings(label + "-restore-inspect")[1]
        if state == self.baseline["viewer2"]["settings"]:
            return
        require(self.cleanup_mode is not None and all(key in self.authorized_keys for key in state),
                "Restoration refuses unowned keys or an unproven deletion strategy.")
        if self.cleanup_mode == "full-empty-replacement":
            self.write(label + "-restore-empty", {}, cleanup=True)
        else:
            self.write(label + "-restore-null", {key: None for key in state},
                       partial=self.cleanup_mode == "partial-null-deletion", cleanup=True)
        require(self.current["viewer2"] == self.baseline["viewer2"]["settings"], "The secondary settings baseline was not restored.")

    def finish(self):
        self.finishing = True
        self.persist()
        cleanup_errors = []
        if not self.cleanup_passed and "viewer2" in self.baseline and "viewer2" in self.verified:
            try:
                self.restore_settings("final")
                self.cleanup_passed = self.current["viewer2"] == self.baseline["viewer2"]["settings"]
            except Exception as error:
                cleanup_errors.append(type(error).__name__)
        if not self.configuration_preserved or not self.primary_preserved:
            preserved = {}
            for account in self.baseline:
                try:
                    state = self.read_settings(account + "-settings-final", target=account)[1]
                    status, dto = self.request(account + "-dto-final", "GET", "/emby/Users/" + self.accounts[account]["userId"], account=account)
                    preserved[account] = status == 200 and all(dto.get(key) == self.baseline[account][key] for key in ("Configuration", "Policy"))
                    if account == "viewer":
                        self.primary_preserved = state == self.baseline[account]["settings"]
                except Exception as error:
                    cleanup_errors.append(type(error).__name__)
            self.configuration_preserved = len(preserved) == 2 and all(preserved.values())
        # These protected reads are checkpointed before logout. Recovery can
        # finish revoking already invalid tokens without reusing them to read.
        self.persist()
        for account in sorted(self.verified - self.revoked):
            try:
                status, _ = self.request(account + "-logout", "POST", "/emby/Sessions/Logout", account=account)
                require(status in (204, 401), "An owned settings recorder logout was not acknowledged or already invalid.")
                status, _ = self.request(account + "-token-invalid", "GET", "/emby/Sessions", account=account)
                require(status == 401, "The settings recorder token invalidity is unproven.")
                self.revoked.add(account)
                self.persist()
            except Exception as error:
                cleanup_errors.append(type(error).__name__)
        self.media_preserved = self.op.verify_media() == self.owner["media"]
        self.identity()
        self.persist()
        return cleanup_errors

    def export(self, failure, cleanup_errors):
        report = {"schemaVersion": 1, "marker": MARKER, "classification": "protocol research; not client acceptance",
                  "serverVersion": "4.9.5.0", "serverId": self.owner["serverId"], "referencePID": PID, "referenceStartTicks": TICKS,
                  "requests": self.count, "settingsWriteAttempts": self.write_count, "maximumRequests": MAX_REQUESTS,
                  "maximumWriteBytes": MAX_WRITE, "maximumReadBytes": MAX_READ, "probeKeyPrefix": PREFIX,
                  "observations": self.observations, "cleanupMode": self.cleanup_mode, "cleanupPassed": self.cleanup_passed,
                  "secondarySettingsAfter": self.current.get("viewer2"), "primarySettingsPreserved": self.primary_preserved,
                  "bothUserConfigurationsAndPoliciesPreserved": self.configuration_preserved, "sourceMediaPreserved": self.media_preserved,
                  "newRecorderSessionsRevoked": sorted(self.revoked), "browserSessionLogoutAttempted": False,
                  "crossUserWriteAuthorizationProven": False, "crossUserPostScope": "Previously proven empty Partial no-ops only",
                  "recoveryAttempt": self.recovery_attempt,
                  "userConfigurationPolicyPasswordLibraryMediaWrites": 0, "displayPreferencesWrites": 0, "oldReferenceRequests": 0,
                  "failureType": failure, "cleanupErrors": cleanup_errors,
                  "caseStatuses": {key: value["response"]["status"] for key, value in self.records.items()}}
        self.support.collect_secrets(report, self.secrets)
        for name, value in (*self.records.items(), ("report", report)):
            safe = self.support.sanitize(value, self.secrets)
            text = encoded(safe)
            require(not any(secret and secret in text for secret in self.secrets) and
                    all(match.group(2) == "[redacted]" for match in self.support.URL_SECRET.finditer(text)),
                    "A known or URL credential survived the settings research export.")
            prefix = "recovery-" + str(self.recovery_attempt) + "-" if self.recovery_attempt else ""
            self.op.save(EXPORT / (prefix + name + ".json"), safe)
        return report


def capture(support, operator, owner, *, recover=False):
    recorder = Recorder(support, operator, owner, operator.read_private(operator.BROWSER), recover=recover)
    failure = None
    try:
        if not recover:
            recorder.baseline_users()
            recorder.establish_restoration()
            recorder.cases()
    except Exception as error:
        failure = type(error).__name__
    cleanup_errors = recorder.finish()
    report = recorder.export(failure, cleanup_errors)
    require(failure is None and not cleanup_errors and recorder.cleanup_passed and recorder.primary_preserved and
            recorder.configuration_preserved and recorder.media_preserved and recorder.revoked == {"viewer", "viewer2"},
            "Settings research or restoration is incomplete; inspect retained evidence and do not claim cleanup.")
    print(json.dumps({"result": "complete", "classification": report["classification"], "requests": recorder.count,
                      "cleanupPassed": recorder.cleanup_passed, "report": str(EXPORT / "report.json")}), flush=True)


class KnownSettingRecorder(Recorder):
    """Attempt 02 uses the one key and value range observed in the real UI."""

    KEY = "genreLimitOnDetails"
    PROBES = {"m3e_keep", "m3e_edit", "m3e_new"}

    def __init__(self, *args):
        super().__init__(*args)
        first = self.op.read_private(WORK / "reference-client-settings-v1/export/report.json")
        require(first.get("requests") == 18 and first.get("cleanupPassed") is True and first.get("secondarySettingsAfter") == {},
                "The first attempt has no complete retained restoration proof.")
        self.count = first["requests"]
        self.authorized_keys.add(self.KEY)
        self.null_deletion_proven = False
        self.persist()

    def written_keys(self, raw, invalid):
        require(not invalid and isinstance(raw, str) and len(raw.encode()) <= MAX_WRITE, "The known-key probe exceeds its bound.")
        body = json.loads(raw)
        require(isinstance(body, dict) and set(body) <= self.PROBES | {self.KEY} and
                all(isinstance(value, str) or value is None for value in body.values()), "The known-key payload is outside its exact scope.")
        require(self.KEY not in body or body[self.KEY] in ("1", "2", None), "The real UI probe key has an unapproved value.")
        require(not (set(body) & self.PROBES) or self.null_deletion_proven,
                "New probe keys require an observed and restored deletion control first.")
        return list(body)

    def approve(self, method, route, account, raw, content_type, **kwargs):
        require(account == "viewer2" and self.count < MAX_REQUESTS, "Attempt 02 authenticates only the independent secondary recorder.")
        parsed = urlsplit(route)
        require(not parsed.scheme and not parsed.netloc and not parsed.fragment and parsed.query in ("", "reqformat=json"),
                "The route or response-format selector differs from the approved UI controls.")
        if parsed.path in ("/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"):
            require(method == "POST" and raw is None and not parsed.query, "Unexpected recorder session mutation.")
            require(parsed.path.endswith("AuthenticateByName") or account in self.verified, "An unproven token cannot be revoked.")
            return
        if method == "GET":
            require(raw is None and not parsed.query and parsed.path in
                    (self.route("viewer2"), self.route("viewer"), "/emby/Users/" + self.accounts["viewer2"]["userId"], "/emby/Sessions"),
                    "The known-key read is outside the two settings maps and secondary-user configuration proof.")
            return
        require(method == "POST" and parsed.path in (self.route("viewer2"), self.route("viewer2", True)) and
                content_type in ("text/plain", "application/json", "application/octet-stream") and
                self.baseline.get("viewer2", {}).get("settings") == {self.KEY: "1"},
                "A known-key update requires the exact newly acknowledged UI baseline.")
        self.written_keys(raw, False)

    def update_case(self, label, body, *, full=False, content_type="text/plain", reqformat=True):
        before = dict(self.current["viewer2"])
        route = self.route("viewer2", partial=not full) + ("?reqformat=json" if reqformat else "")
        status, response = self.request(label, "POST", route, raw=encoded(body), content_type=content_type)
        after = self.read_settings(label + "-after")[1]
        self.observations.append({"case": label, "method": "Full" if full else "Partial", "contentType": content_type,
                                  "reqformat": "json" if reqformat else None, "requestBody": body, "before": before,
                                  "status": status, "responseBody": response, "after": after})
        return status, after

    def restore_known(self, label):
        current = self.read_settings(label + "-inspect")[1]
        if current == {self.KEY: "1"}:
            return
        require(set(current) <= self.authorized_keys, "An unknown setting prevents automatic known-key restoration.")
        probes = set(current) & self.PROBES
        if probes:
            require(self.null_deletion_proven, "The new probe keys have no confirmed deletion control.")
            self.update_case(label + "-remove-probes", {key: None for key in probes})
        if self.current["viewer2"] != {self.KEY: "1"}:
            self.update_case(label + "-restore-ui-default", {self.KEY: "1"})
        require(self.current["viewer2"] == {self.KEY: "1"}, "The complete acknowledged UI baseline was not restored.")

    def run_known(self):
        self.login("viewer2")
        status, dto = self.request("viewer2-dto-baseline", "GET", "/emby/Users/" + self.accounts["viewer2"]["userId"])
        require(status == 200 and isinstance(dto, dict), "The secondary-user configuration baseline is unavailable.")
        settings = self.read_settings("acknowledged-ui-settings-baseline")[1]
        require(settings == {self.KEY: "1"}, "The actual UI-restored settings differ from the acknowledged baseline.")
        self.baseline["viewer2"] = {"settings": settings, "Configuration": dto.get("Configuration"), "Policy": dto.get("Policy")}
        self.op.save(PRIVATE / "acknowledged-baseline.json", self.baseline["viewer2"])
        self.persist()
        status, changed = self.update_case("partial-text-reqformat-set-2", {self.KEY: "2"})
        require(status in (200, 204) and changed == {self.KEY: "2"}, "The real-UI Partial shape did not persist its known value.")
        self.restore_known("first-toggle")
        status, removed = self.update_case("partial-text-null-known-key", {self.KEY: None})
        null_deleted = status in (200, 204) and self.KEY not in removed
        self.restore_known("null-control")
        self.null_deletion_proven = null_deleted
        self.op.save(PRIVATE / "null-control-proof.json", {"deletedKey": null_deleted, "observedAfterNull": removed,
                                                           "completeBaselineRestored": self.current["viewer2"] == {self.KEY: "1"}})
        self.update_case("partial-text-without-reqformat", {self.KEY: "2"}, reqformat=False)
        self.restore_known("no-selector-control")
        for label, full, content_type in (("full-text-reqformat", True, "text/plain"),
                                          ("full-json-reqformat", True, "application/json"),
                                          ("partial-json-reqformat", False, "application/json"),
                                          ("partial-octet-reqformat", False, "application/octet-stream")):
            self.update_case(label, {self.KEY: "2"}, full=full, content_type=content_type)
            self.restore_known(label)
        if self.null_deletion_proven:
            status, seeded = self.update_case("partial-two-key-seed", {"m3e_keep": "a", "m3e_edit": "b"})
            require(status in (200, 204) and seeded == {self.KEY: "1", "m3e_keep": "a", "m3e_edit": "b"},
                    "The safely removable two-key seed did not persist exactly.")
            self.update_case("partial-update-and-add", {"m3e_edit": "c", "m3e_new": "n"})
            self.restore_known("merge-control")
        self.read_settings("viewer2-read-primary-settings", target="viewer", account="viewer2")


class EdgeSettingRecorder(KnownSettingRecorder):
    """Attempt 03 covers value edges only after the proven null-delete study."""

    PROBES = {"m3e_empty", "m3e_unicode", "m3e_case", "m3e_Case", "m3e_duplicate", "m3e_type", "m3e_absent"}

    def __init__(self, *args):
        super().__init__(*args)
        prior = self.op.read_private(WORK / "reference-client-settings-v2/export/report.json")
        require(prior.get("totalStudyRequests") == 64 and prior.get("cleanupPassed") is True and
                prior.get("nullDeletionProven") is True and prior.get("newRecorderRevoked") is True and
                prior.get("settingsAfter") == {self.KEY: "1"}, "The second attempt lacks the exact deletion and restoration proof.")
        self.count, self.null_deletion_proven = 64, True
        self.persist()

    def written_keys(self, raw, invalid):
        require(not invalid and isinstance(raw, str) and len(raw.encode()) <= MAX_WRITE, "An edge payload exceeds its bound.")
        body = json.loads(raw)
        require(isinstance(body, dict) and set(body) <= self.PROBES | {self.KEY}, "An edge payload contains an unowned top-level key.")
        require(self.KEY not in body or body[self.KEY] in ("1", "2", None), "The protected UI default has an unapproved value.")
        return list(body)

    def raw_edge(self, label, raw):
        before = dict(self.current["viewer2"])
        status, response = self.request(label, "POST", self.route("viewer2", True) + "?reqformat=json",
                                        raw=raw, content_type="text/plain")
        after = self.read_settings(label + "-after")[1]
        self.observations.append({"case": label, "method": "Partial", "contentType": "text/plain", "reqformat": "json",
                                  "rawJSON": raw, "before": before, "status": status, "responseBody": response, "after": after})

    def run_known(self):
        self.login("viewer2")
        status, dto = self.request("viewer2-dto-baseline", "GET", "/emby/Users/" + self.accounts["viewer2"]["userId"])
        require(status == 200 and isinstance(dto, dict), "The value-edge account baseline is unavailable.")
        settings = self.read_settings("acknowledged-ui-settings-baseline")[1]
        require(settings == {self.KEY: "1"}, "The acknowledged default changed before value-edge research.")
        self.baseline["viewer2"] = {"settings": settings, "Configuration": dto.get("Configuration"), "Policy": dto.get("Policy")}
        self.op.save(PRIVATE / "acknowledged-baseline.json", self.baseline["viewer2"])
        self.persist()
        self.update_case("empty-unicode-case-keys", {"m3e_empty": "", "m3e_unicode": "caf\u00e9 \u96ea \U0001f3b5",
                                                     "m3e_case": "lower", "m3e_Case": "upper"})
        self.raw_edge("duplicate-json-key", '{"m3e_duplicate":"first","m3e_duplicate":"second"}')
        self.update_case("number-value", {"m3e_type": 123})
        self.update_case("boolean-value", {"m3e_type": True})
        self.update_case("object-value", {"m3e_type": {"nested": "value"}})
        self.update_case("array-value", {"m3e_type": ["value"]})
        self.update_case("empty-partial-object", {})
        self.update_case("absent-key-null", {"m3e_absent": None})
        self.restore_known("edge-cleanup")


class FinalSettingRecorder(EdgeSettingRecorder):
    """Attempt 04 resolves existing-empty and authenticated-user selection."""

    PROBES = {"m3e_complex"}
    HISTORY = "?Recursive=true&IncludeItemTypes=Movie,Episode,Audio&Fields=UserData&Limit=100"

    def __init__(self, *args):
        super().__init__(*args)
        prior = self.op.read_private(WORK / "reference-client-settings-v3/export/report.json")
        require(prior.get("totalStudyRequests") == 90 and prior.get("cleanupPassed") is True and
                prior.get("newRecorderRevoked") is True and prior.get("settingsAfter") == {self.KEY: "1"},
                "Attempt 03 has no exact restored baseline and revoked-token proof.")
        self.count = 90
        self.primary_history_preserved = False
        self.primary_configuration_preserved = False
        self.selection = {}
        self.persist()

    def written_keys(self, raw, invalid):
        require(not invalid and isinstance(raw, str) and len(raw.encode()) <= MAX_WRITE, "The final probe exceeds its bound.")
        body = json.loads(raw)
        require(isinstance(body, dict) and set(body) <= self.PROBES | {self.KEY}, "The final probe contains an unowned key.")
        require(self.KEY not in body or body[self.KEY] in ("", "1", None), "The protected UI key has an unapproved final-probe value.")
        return list(body)

    def approve(self, method, route, account, raw, content_type, **kwargs):
        if account != "viewer":
            return super().approve(method, route, account, raw, content_type, **kwargs)
        require(self.count < MAX_REQUESTS and raw is None, "The primary recorder permits bounded reads and session cleanup only.")
        if route in ("/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"):
            require(method == "POST" and (route.endswith("AuthenticateByName") or account in self.verified),
                    "The primary recorder session operation is unproven.")
            return
        require(method == "GET" and route in (self.route("viewer"), self.route("viewer2"), "/emby/Sessions",
                "/emby/Users/" + self.accounts["viewer"]["userId"],
                "/emby/Users/" + self.accounts["viewer"]["userId"] + "/Items" + self.HISTORY),
                "The primary user permits only exact read-isolation and history preservation observations.")

    def history(self, label):
        status, value = self.request(label, "GET", "/emby/Users/" + self.accounts["viewer"]["userId"] + "/Items" + self.HISTORY,
                                      account="viewer")
        require(status == 200 and isinstance(value, dict) and isinstance(value.get("Items"), list) and
                len(value["Items"]) == 6, "The complete six-item primary playback-history baseline is unavailable.")
        return {row["Id"]: row.get("UserData") for row in value["Items"]}

    def run_known(self):
        self.login("viewer2")
        user = self.op.read_private(PRIVATE / "viewer2-login.json")["User"]
        settings = self.read_settings("acknowledged-ui-settings-baseline")[1]
        require(settings == {self.KEY: "1"}, "The acknowledged UI baseline changed before the final probes.")
        self.baseline["viewer2"] = {"settings": settings, "Configuration": user.get("Configuration"), "Policy": user.get("Policy")}
        self.op.save(PRIVATE / "acknowledged-baseline.json", self.baseline["viewer2"])
        self.persist()
        self.update_case("existing-key-empty-string", {self.KEY: ""})
        self.update_case("existing-key-empty-restore-one", {self.KEY: "1"})
        require(self.current["viewer2"] == {self.KEY: "1"}, "The empty-string probe did not restore the complete UI baseline.")
        complex_value = {"object": {"text": 'space value, "quoted" \\ backslash'},
                         "array": ["two words", "with,comma", 'with"quote', "with\\backslash"]}
        self.update_case("complex-object-array-stringification", {"m3e_complex": complex_value})
        self.update_case("complex-key-delete", {"m3e_complex": None})
        require(self.current["viewer2"] == {self.KEY: "1"}, "The complex-value key was not removed from the complete baseline.")
        self.cleanup_passed = True
        self.persist()
        self.login("viewer")
        primary = self.op.read_private(PRIVATE / "viewer-login.json")["User"]
        self.op.save(PRIVATE / "primary-configuration-baseline.json", {key: primary.get(key) for key in ("Configuration", "Policy")})
        history_before = self.history("primary-history-before")
        self.op.save(PRIVATE / "primary-history-baseline.json", history_before)
        own_status, own = self.request("primary-own-settings", "GET", self.route("viewer"), account="viewer")
        cross_status, cross = self.request("primary-viewer2-path-settings", "GET", self.route("viewer2"), account="viewer")
        self.selection = {"primaryOwnStatus": own_status, "primaryOwnSettings": own, "primaryViewer2PathStatus": cross_status,
                          "primaryViewer2PathSettings": cross, "knownViewer2OwnSettings": {self.KEY: "1"},
                          "authenticatedUserSelectionObserved": own_status == cross_status == 200 and own == cross and own != {self.KEY: "1"}}
        self.primary_history_preserved = self.history("primary-history-after") == history_before
        status, after = self.request("primary-dto-final", "GET", "/emby/Users/" + self.accounts["viewer"]["userId"], account="viewer")
        self.primary_configuration_preserved = status == 200 and all(after.get(key) == primary.get(key) for key in ("Configuration", "Policy"))
        require(self.primary_history_preserved and self.primary_configuration_preserved,
                "The primary user's playback history, configuration, or policy changed during read-isolation observation.")


class NestedSettingRecorder(EdgeSettingRecorder):
    """Attempt 05 is one ordered nested-value matrix and exact restoration."""

    PROBES = {"m3e_nested"}
    RAW = ('{"m3e_nested":{"nullValue":null,"emptyString":"","emptyObject":{},"emptyArray":[],'
           '"object":{"dup":"first","dup":"second","Case":"upper","case":"lower","":"empty-key",'
           '"key with space":"space","key,comma":"comma","key:colon":"colon",'
           '"key\\\"quote":"quote","key\\\\slash":"slash","\\u96ea":"unicode-key"},'
           '"array":[null,"",{},[],{"innerNull":null,"innerEmpty":""},["",null,{},[]]],"tail":"end"}}')

    def __init__(self, *args):
        super().__init__(*args)
        prior = self.op.read_private(WORK / "reference-client-settings-v4/export/report.json")
        require(prior.get("totalStudyRequests") == 111 and prior.get("cleanupPassed") is True and
                prior.get("newRecorderRevoked") is True and prior.get("settingsAfter") == {self.KEY: "1"},
                "The final-boundary attempt lacks its complete restoration and token-revocation proof.")
        self.count = 111
        self.persist()

    def run_known(self):
        self.login("viewer2")
        user = self.op.read_private(PRIVATE / "viewer2-login.json")["User"]
        settings = self.read_settings("acknowledged-ui-settings-baseline")[1]
        require(settings == {self.KEY: "1"}, "The acknowledged baseline changed before the single nested matrix.")
        self.baseline["viewer2"] = {"settings": settings, "Configuration": user.get("Configuration"), "Policy": user.get("Policy")}
        self.op.save(PRIVATE / "acknowledged-baseline.json", self.baseline["viewer2"])
        self.persist()
        self.raw_edge("nested-visitor-matrix", self.RAW)
        self.update_case("nested-key-delete", {"m3e_nested": None})
        require(self.current["viewer2"] == {self.KEY: "1"}, "The single nested probe was not removed from the complete baseline.")
        self.cleanup_passed = True
        self.persist()


def known_capture(support, operator, owner, *, attempt=2):
    recorder_type = {2: KnownSettingRecorder, 3: EdgeSettingRecorder, 4: FinalSettingRecorder, 5: NestedSettingRecorder}[attempt]
    prior_count = {2: 18, 3: 64, 4: 90, 5: 111}[attempt]
    recorder = recorder_type(support, operator, owner, operator.read_private(operator.BROWSER))
    failure, cleanup_errors = None, []
    try:
        recorder.run_known()
    except Exception as error:
        failure = type(error).__name__
    recorder.finishing = True
    try:
        if "viewer2" in recorder.baseline:
            if not recorder.cleanup_passed:
                recorder.restore_known("final")
            recorder.cleanup_passed = recorder.current["viewer2"] == recorder.baseline["viewer2"]["settings"]
            status, dto = recorder.request("viewer2-dto-final", "GET", "/emby/Users/" + recorder.accounts["viewer2"]["userId"])
            recorder.configuration_preserved = status == 200 and all(dto.get(key) == recorder.baseline["viewer2"][key] for key in ("Configuration", "Policy"))
    except Exception as error:
        cleanup_errors.append(type(error).__name__)
    try:
        for account in sorted(recorder.verified):
            status, _ = recorder.request(account + "-logout", "POST", "/emby/Sessions/Logout", account=account)
            require(status in (204, 401), "The known-key recorder logout failed.")
            status, _ = recorder.request(account + "-token-invalid", "GET", "/emby/Sessions", account=account)
            require(status == 401, "The known-key recorder token invalidity is unproven.")
            recorder.revoked.add(account)
    except Exception as error:
        cleanup_errors.append(type(error).__name__)
    recorder.media_preserved = operator.verify_media() == owner["media"]
    recorder.identity()
    recorder.persist()
    report = {"schemaVersion": 1, "marker": MARKER, "classification": "protocol research; not client acceptance",
              "attempt": attempt, "priorRequests": prior_count, "requestsThisAttempt": recorder.count - prior_count, "totalStudyRequests": recorder.count,
              "acknowledgedBaseline": recorder.baseline.get("viewer2", {}).get("settings"), "observations": recorder.observations,
              "nullDeletionProven": recorder.null_deletion_proven, "cleanupPassed": recorder.cleanup_passed,
              "settingsAfter": recorder.current.get("viewer2"), "viewer2ConfigurationAndPolicyPreserved": recorder.configuration_preserved,
              "primaryViewerAuthenticatedOrWritten": attempt == 4, "primaryViewerReadIsolationOnly": True,
              "primaryViewerSettingsWriteAttempts": 0, "primaryViewerAuthenticatedForReadIsolation": attempt == 4,
              "primaryPlaybackHistoryPreserved": getattr(recorder, "primary_history_preserved", None),
              "primaryConfigurationAndPolicyPreserved": getattr(recorder, "primary_configuration_preserved", None),
              "userSelection": getattr(recorder, "selection", None),
              "sourceMediaPreserved": recorder.media_preserved, "newRecorderRevoked": recorder.revoked == recorder.verified,
              "newRecorderAccountsRevoked": sorted(recorder.revoked),
              "failureType": failure, "cleanupErrors": cleanup_errors,
              "caseStatuses": {key: value["response"]["status"] for key, value in recorder.records.items()}}
    for name, value in (*recorder.records.items(), ("report", report)):
        support.collect_secrets(value, recorder.secrets)
        safe = support.sanitize(value, recorder.secrets)
        require(not any(secret and secret in encoded(safe) for secret in recorder.secrets), "A known credential survived export.")
        operator.save(EXPORT / (name + ".json"), safe)
    require(failure is None and not cleanup_errors and recorder.cleanup_passed and recorder.configuration_preserved and
            recorder.media_preserved and recorder.revoked == recorder.verified, "The follow-up attempt is incomplete; inspect retained restoration evidence.")
    print(json.dumps({"result": "complete", "attempt": attempt, "totalStudyRequests": recorder.count,
                      "cleanupPassed": True, "report": str(EXPORT / "report.json")}), flush=True)


def known_main(internal=False, attempt=2):
    global ROOT, PRIVATE, EXPORT, STATE, LOCK, MARKER, DEVICE_PREFIX, MAX_REQUESTS
    require(attempt in (2, 3, 4, 5), "Only the reviewed follow-up attempts are supported.")
    if attempt == 4:
        MAX_REQUESTS = 112
    if attempt == 5:
        MAX_REQUESTS = 128
    ROOT = WORK / ("reference-client-settings-v" + str(attempt))
    PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
    STATE, LOCK = PRIVATE / "state.json", PRIVATE / "operator.lock"
    MARKER = "goby-m3e-reference-client-settings-v" + str(attempt)
    DEVICE_PREFIX = "goby-m3e-settings-research-attempt0" + str(attempt) + "-"
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run only through authorized root SSH.")
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_args: (_ for _ in ()).throw(TimeoutError("A settings HTTP deadline expired.")))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    require(owner["serviceIdentity"]["pid"] == PID and owner["serviceIdentity"]["startTicks"] == TICKS and
            operator.service_identity(owner) == owner["serviceIdentity"], "The explicitly approved fresh reference changed.")
    if internal:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT)}, "Attempt 02 ownership differs.")
        known_capture(support, operator, owner, attempt=attempt)
        return
    require(not operator.present(ROOT), "Attempt 02 evidence exists and will not be overwritten.")
    ROOT.mkdir(mode=0o700)
    operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT)})
    PRIVATE.mkdir(mode=0o700)
    EXPORT.mkdir(mode=0o700)
    operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  "serviceIdentity": owner["serviceIdentity"], "priorRequests": {2: 18, 3: 64, 4: 90, 5: 111}[attempt],
                  "acknowledgedBaseline": {"genreLimitOnDetails": "1"}, "primaryViewerMutation": False})
    namespace = os.open(f"/proc/{PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(namespace).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]), "The namespace handle differs.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(namespace), "/usr/bin/python3", "-I", "-B",
                                 str(Path(__file__).absolute()), "_attempt-0" + str(attempt)], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=240, check=False, pass_fds=(namespace,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "Attempt 02 failed; its private and sanitized restoration evidence is retained.")
        print(result.stdout.decode().strip())
    finally:
        os.close(namespace)


def main(arguments=None):
    arguments = sys.argv[1:] if arguments is None else arguments
    if arguments in (["--attempt-02"], ["_attempt-02"]):
        known_main(internal=arguments == ["_attempt-02"])
        return
    if arguments in (["--attempt-03"], ["_attempt-03"]):
        known_main(internal=arguments == ["_attempt-03"], attempt=3)
        return
    if arguments in (["--attempt-04"], ["_attempt-04"]):
        known_main(internal=arguments == ["_attempt-04"], attempt=4)
        return
    if arguments in (["--attempt-05"], ["_attempt-05"]):
        known_main(internal=arguments == ["_attempt-05"], attempt=5)
        return
    require(arguments in ([], ["--recover"], ["_capture"], ["_recover"]), "Usage: reference-client-settings.py [--recover]")
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run only through authorized root SSH.")
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_args: (_ for _ in ()).throw(TimeoutError("A settings HTTP deadline expired.")))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    operator.validate_owner(owner)
    require(owner.get("phase") == "ready" and owner["serviceIdentity"]["pid"] == PID and owner["serviceIdentity"]["startTicks"] == TICKS and
            operator.service_identity(owner) == owner["serviceIdentity"], "The exact approved fresh reference instance changed.")
    if arguments in (["_capture"], ["_recover"]):
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT)}, "The settings evidence marker differs.")
        capture(support, operator, owner, recover=arguments == ["_recover"])
        return
    recovering = arguments == ["--recover"]
    if recovering:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT)}, "The recovery root marker differs.")
        retained = operator.read_private(STATE)
        require(retained.get("marker") == MARKER and retained.get("serviceIdentity") == owner["serviceIdentity"], "Recovery state ownership differs.")
        worker = retained["worker"]
        if operator.present(Path("/proc") / str(worker["pid"])):
            require(not same_worker_lifetime(operator.process_identity(worker["pid"]), worker),
                    "The original settings worker is still live; recovery will not race it.")
    else:
        require(not operator.present(ROOT), "Settings evidence already exists; no account state or evidence was replaced.")
    require(operator.verify_media() == owner["media"], "The new media baseline changed before settings research.")
    if not recovering:
        ROOT.mkdir(mode=0o700)
        operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT)})
        PRIVATE.mkdir(mode=0o700)
        EXPORT.mkdir(mode=0o700)
        operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                      "serviceIdentity": owner["serviceIdentity"], "probePrefix": PREFIX, "maximumRequests": MAX_REQUESTS,
                      "maximumWriteBytes": MAX_WRITE, "primaryUserMutation": "Only a proven empty Partial no-op"})
        operator.save(LOCK, "")
    operator.canonical(LOCK, mode=0o600)
    lock = os.open(LOCK, os.O_RDWR | os.O_NOFOLLOW)
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(lock)
        raise SettingsError("A settings research or recovery supervisor is already active.") from None
    descriptor = os.open(f"/proc/{PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(descriptor).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]) and
                operator.service_identity(owner) == owner["serviceIdentity"], "The pinned reference namespace handle differs.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(descriptor), "/usr/bin/python3", "-I", "-B",
                                 str(Path(__file__).absolute()), "_recover" if recovering else "_capture"], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=240, check=False, pass_fds=(descriptor,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "Settings research failed; retained evidence records restoration and owned recorder cleanup state.")
        print(result.stdout.decode().strip())
    finally:
        os.close(descriptor)
        os.close(lock)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"result": "failed", "failureType": type(error).__name__, "evidenceRetained": True}), file=sys.stderr)
        sys.exit(1)
