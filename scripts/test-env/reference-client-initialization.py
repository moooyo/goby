#!/usr/bin/env python3
"""Capture read-only initialization contracts from the owned fresh reference.

This is protocol research, not client acceptance. One uniquely named recorder
session is created and revoked; browser sessions and user preferences are not
modified. All HTTP bodies are bounded to 64 KiB. Authentication request bodies
are never retained, and all exported records are scrubbed of credentials.
"""

from __future__ import annotations

import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
from urllib.parse import urlencode, urlsplit, parse_qs, unquote

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
ROOT = WORK / "reference-client-initialization-v1"
PRIVATE = ROOT / "private"
EXPORT = ROOT / "export"
MARKER = "goby-m3e-reference-client-initialization-v1"
DEVICE = "goby-m3e-client-initialization-recorder-v1"
CLIENT = "Goby Initialization Contract Recorder"
LIMIT = 64 * 1024
EXPECTED_PID = 332054
EXPECTED_TICKS = "357218"
SECRET_KEYS = {"accesstoken", "access_token", "token", "authorization", "password", "pw", "api_key", "apikey",
               "secret", "clientsecret", "client_secret", "connectaccesstoken", "x-emby-token", "x-mediabrowser-token"}
URL_SECRET = re.compile(r"(?i)([?&](?:" + "|".join(re.escape(key) for key in sorted(SECRET_KEYS)) + r")=)([^&\s\"'<>]+)")


class CaptureError(Exception):
    """A contract-research guard or preservation check failed."""


def require(condition, message):
    if not condition:
        raise CaptureError(message)


def load_operator():
    source = Path(__file__).absolute().with_name("prepare-client-reference.py")
    for path in reversed((source, *source.parents)):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                "The fixed reference operator path is not protected and root-owned.")
    expected = json.loads((WORK / "reference-acceptance.json").read_text())["operatorSha256"]
    require(hashlib.sha256(source.read_bytes()).hexdigest() == expected, "The reference preparer differs from its accepted source.")
    spec = importlib.util.spec_from_file_location("owned_reference_preparer", source)
    operator = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(operator)
    return operator


def collect_secrets(value, secrets):
    if isinstance(value, dict):
        for key, item in value.items():
            collect_secrets(str(key), secrets)
            if str(key).lower() in SECRET_KEYS | {"set-cookie", "cookie"} and isinstance(item, str) and item and item != "[redacted]":
                secrets.add(item)
            if key != "bodyStructure":
                collect_secrets(item, secrets)
    elif isinstance(value, list):
        for item in value:
            collect_secrets(item, secrets)
    elif isinstance(value, str):
        for match in URL_SECRET.finditer(value):
            if match.group(2) != "[redacted]":
                secrets.update((match.group(2), unquote(match.group(2))))


def sanitize(value, secrets):
    if isinstance(value, dict):
        result = {}
        for key, item in value.items():
            safe_key = sanitize(str(key), secrets)
            require(safe_key not in result, "Sanitized response keys collide; refusing an ambiguous export.")
            result[safe_key] = ("[redacted]" if str(key).lower() in SECRET_KEYS | {"set-cookie", "cookie"} else sanitize(item, secrets))
        return result
    if isinstance(value, list):
        return [sanitize(item, secrets) for item in value]
    if isinstance(value, str):
        result = value
        for secret in sorted((item for item in secrets if item), key=len, reverse=True):
            result = result.replace(secret, "[redacted]")
        result = URL_SECRET.sub(lambda match: match.group(1) + "[redacted]", result)
        return result
    return value


def structure(value):
    if isinstance(value, dict):
        return {key: structure(item) for key, item in value.items()}
    if isinstance(value, list):
        unique = {}
        for item in value:
            shape = structure(item)
            unique[json.dumps(shape, sort_keys=True)] = shape
        return {"type": "array", "length": len(value), "itemShapes": list(unique.values())[:16]}
    if value is None:
        return "null"
    if type(value) is bool:
        return "boolean"
    if type(value) is int:
        return "integer"
    if type(value) is float:
        return "number"
    return "string"


def approve(method, route, user_id, *, authenticated):
    parsed = urlsplit(route)
    require(not parsed.scheme and not parsed.netloc and not parsed.fragment, "An external or fragmented route is forbidden.")
    query = parse_qs(parsed.query, keep_blank_values=True)
    if method == "POST":
        require(parsed.path in ("/emby/Users/AuthenticateByName", "/emby/Sessions/Logout") and not query,
                "Only the new recorder login and logout may mutate session state.")
        require(authenticated is (parsed.path == "/emby/Sessions/Logout"), "Recorder authentication mode differs.")
        return
    require(method == "GET", "Preference, policy, library, and capability writes are outside this read-only study.")
    public = {"/emby/Branding/Configuration", "/emby/Branding/Css"}
    if parsed.path in public:
        require(not authenticated and not query, "Branding controls must be public exact GETs.")
        return
    require(authenticated and re.fullmatch(r"[0-9a-f]{32}", user_id), "A private research GET requires the owned viewer identity.")
    if parsed.path == f"/emby/usersettings/{user_id}":
        require(query in ({}, {"Client": ["emby"]}, {"Client": ["embyweb"]}), "An unexpected UserSettings variant was requested.")
    elif parsed.path in (f"/emby/Users/{user_id}", "/emby/System/Endpoint", "/emby/Sessions"):
        require(not query, "The private control route has unexpected parameters.")
    elif parsed.path == "/emby/DisplayPreferences/usersettings":
        require(query in ({"UserId": [user_id], "client": ["emby"]}, {"UserId": [user_id], "client": ["embyweb"]}),
                "An unexpected DisplayPreferences identity or client was requested.")
    elif parsed.path == "/emby/Playback/BitrateTest":
        require(query == {"Size": ["1024"]}, "The bitrate probe must request exactly 1024 bytes.")
    else:
        raise CaptureError("The GET is outside the exact initialization-research allowlist.")


class Recorder:
    def __init__(self, operator, owner, browser):
        self.operator, self.owner = operator, owner
        self.credential = {key: browser[key] for key in ("username", "password", "userId")}
        require(self.credential == browser["accounts"]["viewer"], "The research identity is not the ordinary fresh viewer.")
        self.secrets = {entry["password"] for entry in browser["accounts"].values()}
        self.user_id, self.token, self.revoked = self.credential["userId"], None, False
        self.token_identity_proven = False
        self.records = {}
        self.count = 0
        self.bytes_read = 0

    def identity(self):
        require(self.operator.service_identity(self.owner) == self.owner["serviceIdentity"], "The owned reference process changed.")
        identity = self.owner["serviceIdentity"]
        require(identity["pid"] == EXPECTED_PID and identity["startTicks"] == EXPECTED_TICKS and
                os.readlink("/proc/self/ns/net") == identity["networkNamespace"],
                "The recorder is outside the explicitly attested fresh reference process namespace.")

    def export(self, path, value):
        collect_secrets(value, self.secrets)
        safe = sanitize(value, self.secrets)
        text = json.dumps(safe, sort_keys=True, ensure_ascii=False)
        require(not any(secret and secret in text for secret in self.secrets), "A known credential survived export sanitization.")
        require(all(match.group(2) == "[redacted]" for match in URL_SECRET.finditer(text)),
                "An unredacted URL credential survived export sanitization.")
        self.operator.save(path, safe, mode=0o644)
        return safe

    def request(self, label, method, route, *, authenticated=True, login=False):
        self.identity()
        require(self.count < 20, "The bounded initialization request budget is exhausted.")
        approve(method, route, self.user_id, authenticated=authenticated)
        headers = {"Accept": "application/json", "Authorization":
                   f'Emby Client="{CLIENT}", Device="Linux Contract Research", DeviceId="{DEVICE}", Version="1.0"'}
        body = None
        if authenticated:
            require(self.token, "The private request has no confirmed new recorder token.")
            headers["X-Emby-Token"] = self.token
        if login:
            require(method == "POST" and route == "/emby/Users/AuthenticateByName" and not authenticated,
                    "Only the new recorder authentication request may contain credentials.")
            headers["Content-Type"] = "application/x-www-form-urlencoded"
            body = urlencode({"Username": self.credential["username"], "Pw": self.credential["password"]}).encode()
        elif not authenticated:
            # Public branding controls must not carry client metadata or a token.
            headers = {"Accept": "application/json"}
        connection = http.client.HTTPConnection("127.0.0.1", self.operator.PORT, timeout=8)
        response_headers, status, payload = {}, None, b""
        complete, truncated = False, False
        try:
            connection.request(method, route, body=body, headers=headers)
            response = connection.getresponse()
            status = response.status
            response_headers = dict(response.getheaders())
            length = response.getheader("Content-Length")
            payload = response.read(LIMIT)
            truncated = len(payload) == LIMIT and (length is None or not length.isdigit() or int(length) >= LIMIT)
            complete = not truncated and (length is None or length.isdigit() and int(length) == len(payload))
        finally:
            # A server ignoring Size might stream a large default response.
            # Closing here bounds both memory and bytes received by this script.
            connection.close()
        self.count += 1
        self.bytes_read += len(payload)
        content_type = next((value for key, value in response_headers.items() if key.lower() == "content-type"), "")
        try:
            parsed = json.loads(payload) if payload else None
            kind = "json" if payload else "empty"
        except (ValueError, UnicodeDecodeError):
            parsed = payload.decode("utf-8", errors="replace") if content_type.startswith(("text/", "application/xml")) else None
            kind = "text" if parsed is not None else "binary"
        if login and isinstance(parsed, dict) and isinstance(parsed.get("AccessToken"), str) and parsed["AccessToken"]:
            # The complete credential acknowledgement is private and durable
            # before any server, user, device, or role assertion can fail.
            self.token = parsed["AccessToken"]
            self.secrets.add(self.token)
            self.operator.save(PRIVATE / "recorder-login.json", parsed)
        self.identity()
        if login:
            self.prove_login(status, parsed)
        record = {"classification": "protocol research; not client acceptance", "case": label,
                  "request": {"method": method, "path": route, "authenticated": authenticated,
                              "contentType": headers.get("Content-Type"), "authenticationBodyRetained": False},
                  "response": {"status": status, "contentType": content_type, "headers": response_headers,
                               "bodyKind": kind, "body": parsed if kind != "binary" else None,
                               "bodyStructure": structure(parsed) if kind in ("json", "text") else "binary",
                               "bytesRead": len(payload), "bodySha256": hashlib.sha256(payload).hexdigest(),
                               "complete": complete, "truncatedAtReadLimit": truncated, "readLimitBytes": LIMIT}}
        collect_secrets(record, self.secrets)
        safe = sanitize(record, self.secrets)
        self.operator.save(PRIVATE / (label + "-record.json"), safe)
        # Public records are emitted together only after all responses have
        # contributed their secret values, including later mirrored values.
        self.records[label] = safe
        return status, parsed

    def prove_login(self, status, login):
        require(status == 200 and self.token and isinstance(login, dict) and login.get("ServerId") == self.owner["serverId"] and
                login.get("User", {}).get("Id") == self.user_id and login["User"].get("Name") == self.credential["username"] and
                login["User"].get("Policy", {}).get("IsAdministrator") is False and
                login.get("SessionInfo", {}).get("DeviceId") == DEVICE,
                "The confirmed recorder token did not identify the exact ordinary viewer and new device.")
        self.token_identity_proven = True

    def logout(self):
        if not self.token:
            return
        require(self.token_identity_proven, "Recorder token identity is unproven; no browser or unknown session will be logged out.")
        status, _ = self.request("recorder-logout", "POST", "/emby/Sessions/Logout")
        require(status == 204, "The new recorder logout was not acknowledged.")
        status, _ = self.request("recorder-token-invalid", "GET", "/emby/Sessions")
        require(status == 401, "The new recorder token invalidity is not proven.")
        self.revoked = True
        self.operator.save(PRIVATE / "recorder-revocation.json", {"marker": MARKER, "deviceId": DEVICE,
                                                                 "logoutStatus": 204, "invalidTokenStatus": 401})

    def capture(self):
        status, login = self.request("recorder-login", "POST", "/emby/Users/AuthenticateByName", authenticated=False, login=True)
        require(status == 200 and self.token_identity_proven, "The new recorder login was not proven.")
        status, before = self.request("viewer-dto-before", "GET", f"/emby/Users/{self.user_id}")
        require(status == 200 and isinstance(before, dict), "The viewer preservation baseline is unavailable.")
        requests = (("usersettings-default", f"/emby/usersettings/{self.user_id}"),
                    ("usersettings-emby", f"/emby/usersettings/{self.user_id}?Client=emby"),
                    ("usersettings-embyweb", f"/emby/usersettings/{self.user_id}?Client=embyweb"),
                    ("system-endpoint", "/emby/System/Endpoint"),
                    ("displaypreferences-emby", "/emby/DisplayPreferences/usersettings?" + urlencode({"UserId": self.user_id, "client": "emby"})),
                    ("displaypreferences-embyweb", "/emby/DisplayPreferences/usersettings?" + urlencode({"UserId": self.user_id, "client": "embyweb"})),
                    ("bitrate-size-1024", "/emby/Playback/BitrateTest?Size=1024"))
        for label, route in requests:
            self.request(label, "GET", route)
        for label, route in (("branding-configuration-public", "/emby/Branding/Configuration"), ("branding-css-public", "/emby/Branding/Css")):
            self.request(label, "GET", route, authenticated=False)
        status, after = self.request("viewer-dto-after", "GET", f"/emby/Users/{self.user_id}")
        require(status == 200 and isinstance(after, dict) and all(before.get(key) == after.get(key) for key in ("Configuration", "Policy")),
                "The viewer configuration or policy changed during this read-only study.")
        return before


def run_capture(operator, owner):
    browser = operator.read_private(operator.BROWSER)
    recorder = Recorder(operator, owner, browser)
    before, failure = None, None
    try:
        before = recorder.capture()
    except Exception as error:
        failure = type(error).__name__
    finally:
        try:
            recorder.logout()
        except Exception as error:
            failure = failure or type(error).__name__
        media_unchanged = operator.verify_media() == owner["media"]
        recorder.identity()
    settings = {name: recorder.records.get("usersettings-" + name, {}).get("response", {})
                for name in ("default", "emby", "embyweb")}
    display = {name: recorder.records.get("displaypreferences-" + name, {}).get("response", {}) for name in ("emby", "embyweb")}
    comparisons = {"defaultEqualsEmby": settings["default"].get("body") == settings["emby"].get("body"),
                   "defaultEqualsEmbyweb": settings["default"].get("body") == settings["embyweb"].get("body"),
                   "embyEqualsEmbyweb": settings["emby"].get("body") == settings["embyweb"].get("body"),
                   "displayPreferencesEmbyEqualsEmbyweb": display["emby"].get("body") == display["embyweb"].get("body")}
    summary = {"schemaVersion": 1, "marker": MARKER, "classification": "protocol research; not client acceptance",
               "serverVersion": "4.9.5.0", "serverId": owner["serverId"], "referencePID": EXPECTED_PID,
               "referenceStartTicks": EXPECTED_TICKS, "recorderDeviceId": DEVICE, "requests": recorder.count,
               "totalBytesRead": recorder.bytes_read, "maxSingleReadBytes": LIMIT,
               "userSettings": settings, "userSettingsComparisons": comparisons, "displayPreferences": display,
               "viewerConfiguration": before.get("Configuration") if isinstance(before, dict) else None,
               "viewerConfigurationAndPolicyPreserved": before is not None and failure is None,
               "mediaManifestPreserved": media_unchanged, "newRecorderTokenRevoked": recorder.revoked,
               "browserSessionLogoutAttempted": False, "preferencePolicyOrCapabilityWrites": 0,
               "oldReferenceRequests": 0, "failureType": failure,
               "caseStatuses": {name: record["response"]["status"] for name, record in recorder.records.items()}}
    for label, record in recorder.records.items():
        recorder.export(EXPORT / (label + ".json"), record)
    recorder.export(EXPORT / "report.json", summary)
    require(failure is None and media_unchanged and recorder.revoked, "The bounded research capture is incomplete; inspect its sanitized report.")
    print(json.dumps({"result": "complete", "classification": summary["classification"], "requests": recorder.count,
                      "newRecorderTokenRevoked": recorder.revoked, "report": str(EXPORT / "report.json")}), flush=True)


def main(arguments=None):
    arguments = sys.argv[1:] if arguments is None else arguments
    require(arguments in ([], ["_capture"]), "Usage: reference-client-initialization.py")
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
            "Run only through authorized root SSH on test-env.")
    os.umask(0o077)
    operator = load_operator()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    operator.validate_owner(owner)
    require(owner.get("phase") == "ready" and owner["serviceIdentity"]["pid"] == EXPECTED_PID and
            owner["serviceIdentity"]["startTicks"] == EXPECTED_TICKS and
            operator.service_identity(owner) == owner["serviceIdentity"], "The explicitly approved fresh reference identity changed.")
    if arguments == ["_capture"]:
        operator.canonical(ROOT, directory=True, mode=0o700)
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT), "deviceId": DEVICE},
                "The independent research evidence marker differs.")
        run_capture(operator, owner)
        return
    require(not operator.present(ROOT), "Research evidence already exists; no recorder session or evidence was replaced.")
    require(operator.verify_media() == owner["media"], "The owned media baseline changed before capture.")
    ROOT.mkdir(mode=0o700)
    operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT), "deviceId": DEVICE})
    PRIVATE.mkdir(mode=0o700)
    EXPORT.mkdir(mode=0o700)
    operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                 "serviceIdentity": owner["serviceIdentity"], "sessionDeviceId": DEVICE, "userId": operator.read_private(operator.BROWSER)["userId"],
                 "preferencePolicyOrCapabilityWrites": 0, "maximumBodyReadBytes": LIMIT})
    descriptor = os.open(f"/proc/{EXPECTED_PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(descriptor).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]), "The fixed namespace handle differs.")
        require(operator.service_identity(owner) == owner["serviceIdentity"], "The reference changed before namespace entry.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(descriptor), "/usr/bin/python3", "-B",
                                 str(Path(__file__).absolute()), "_capture"], stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=180, check=False, pass_fds=(descriptor,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "Research capture failed; private credentials and sanitized evidence remain for exact cleanup.")
        print(result.stdout.decode().strip())
    finally:
        os.close(descriptor)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"result": "failed", "failureType": type(error).__name__, "evidenceRetained": True}), file=sys.stderr)
        sys.exit(1)
