#!/usr/bin/env python3
"""Capture owned API-key contracts from the isolated reference through SSH.

Only a fresh dedicated login and API keys bearing this run's application prefix
may be mutated. Existing evidence and known source files are hashed before and
after. Raw data remains root-private; only sanitized exports may leave test-env.
"""

from __future__ import annotations

import datetime as dt
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import sys
from urllib.parse import parse_qs, quote, urlencode, urlsplit

sys.dont_write_bytecode = True
ROOT = Path("/opt/goby-test/exec-scratch/keys-m5d")
DATA = Path("/opt/goby-test/emby-reference-data")
RUNTIME = Path("/dev/shm/goby-emby-reference/runtime")
PRIVATE, RAW, EXPORT = ROOT / "private", ROOT / "private/raw", ROOT / "export"
PREFIX = "keys-m5d-"
MARKER = "goby-reference-keys-m5d-owned-v1"
APP_PREFIX = "Goby Keys M5d 20260910 01 "
DEVICE = "goby-keys-m5d-20260910-01"
MAX_BODY, MAX_TOTAL = 256 * 1024, 2 * 1024 * 1024
SECRET_FIELDS = frozenset({"accesstoken", "token", "key", "pw", "password"})


def require(value: bool, label: str) -> None:
    if not value:
        raise RuntimeError(label)


def sensitive_header(name: str) -> bool:
    normalized = re.sub(r"[^a-z0-9]", "", name.lower())
    return (normalized in {"authorization", "proxyauthorization", "xembyauthorization", "cookie", "setcookie"} or
            "token" in normalized or "apikey" in normalized)


def header_pairs(value: object) -> list[tuple[str, str]]:
    pairs = list(value.items()) if isinstance(value, dict) else value
    require(isinstance(pairs, (list, tuple)), "HTTP headers have an unsupported representation")
    require(all(isinstance(pair, (list, tuple)) and len(pair) == 2 and
                all(isinstance(part, str) for part in pair) for pair in pairs), "HTTP headers contain a malformed pair")
    return [(name, content) for name, content in pairs]


def private_file(path: Path) -> None:
    info = path.lstat()
    require(path.resolve(strict=True) == path and stat.S_ISREG(info.st_mode) and
            info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600,
            "Private input ownership or permissions differ")


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(65536), b""):
            result.update(block)
    return result.hexdigest()


def save(path: Path, value: object) -> None:
    text = value if isinstance(value, str) else json.dumps(value, indent=2, ensure_ascii=False) + "\n"
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600),
                   "w", encoding="utf-8") as stream:
        stream.write(text)


def preconditions() -> int:
    require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
            "Run only through authorized root SSH")
    properties = dict(line.split("=", 1) for line in subprocess.check_output([
        "systemctl", "show", "goby-emby-reference.service", "-p", "MainPID", "-p", "PrivateNetwork", "-p", "ActiveState"
    ], timeout=5, text=True).splitlines())
    pid = int(properties["MainPID"])
    require(pid > 1 and properties["PrivateNetwork"] == "yes" and properties["ActiveState"] == "active" and
            Path(f"/proc/{pid}").stat().st_uid == 0, "Reference service ownership or isolation differs")
    require(os.readlink(f"/proc/{pid}/ns/net") != os.readlink("/proc/1/ns/net"), "Reference uses the host namespace")
    for folder in (DATA, RUNTIME.parent):
        require(folder.resolve(strict=True) == folder and folder.stat().st_uid == 0 and
                (folder / ".goby-managed").read_text().strip() == "goby-emby-reference-owned-v1", "Reference marker differs")
    require(Path("/opt/goby-test/exec-scratch.owner").read_text().strip() == "goby-verification-scratch",
            "Execution scratch marker differs")
    if os.readlink("/proc/self/ns/net") != os.readlink(f"/proc/{pid}/ns/net"):
        os.execvp("nsenter", ["nsenter", "-t", str(pid), "-n", "python3", "-B", str(Path(__file__).resolve())])
    require(shutil.disk_usage(ROOT.parent).free > 8 * 1024 * 1024, "Capture space is insufficient")
    return pid


class Recorder:
    def __init__(self, pid: int) -> None:
        require(not ROOT.exists(), "Refusing to overwrite an existing capture")
        os.umask(0o077)
        ROOT.mkdir(mode=0o700)
        for folder in (PRIVATE, RAW, EXPORT):
            folder.mkdir(mode=0o700)
        save(ROOT / ".goby-managed", MARKER + "\n")
        self.pid, self.total = pid, 0
        self.secrets, self.owned, self.logins = set(), {}, {}
        self.logged_out, self.logout_statuses = set(), {}
        self.apps, self.old_keys = set(), None
        self.cleanup_count = 0
        self.baseline = self.snapshot()
        save(PRIVATE / "baseline.json", self.baseline)

    def snapshot(self) -> dict:
        bases = [DATA, *(RUNTIME / name for name in (
            "audio-m4c", "audio-m4c-defaults", "audio-profile-m4d", "video-profile-m4e")),
            Path("/opt/goby-test/exec-scratch/metadata-m5b")]
        paths = [path for base in bases for folder in (base / "private/raw", base / "export") for path in folder.glob("*.json")]
        require(len(paths) == 1916, "Expected exactly 958 preceding raw/export pairs")
        previous = json.loads(Path("/opt/goby-test/exec-scratch/metadata-m5b/private/baseline.json").read_text())
        media = [Path(path) for path in previous["media"]]
        media.extend(Path("/opt/goby-test/exec-scratch/metadata-m5b/source").iterdir())
        require(sum(path.stat().st_size for path in media) < 32 * 1024 * 1024, "Known source audit exceeds its bound")
        for path in [*paths, *media]:
            require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode), "Preserved path is not a regular file")
        return {"records": {str(path): digest(path) for path in paths},
                "media": {str(path): digest(path) for path in media}}

    def credentials(self, name: str) -> dict:
        path = DATA / "private" / ("credentials.env" if name == "admin" else "session-m3b-credentials.env")
        private_file(path)
        result = dict(line.split("=", 1) for line in path.read_text().splitlines() if line)
        self.secrets.update(value for key, value in result.items() if key in {"REFERENCE_PASSWORD", "REFERENCE_TOKEN"} and value)
        return result

    def collect_secrets(self, value: object, key: str = "") -> None:
        if key.lower() == "headers":
            for name, content in header_pairs(value):
                if sensitive_header(name) and content:
                    self.secrets.add(content)
                else:
                    self.collect_secrets(content, name)
        elif isinstance(value, dict):
            for child, item in value.items():
                self.collect_secrets(item, child)
        elif isinstance(value, (list, tuple)):
            for item in value:
                self.collect_secrets(item, key)
        elif isinstance(value, str) and key.lower() in SECRET_FIELDS and value:
            self.secrets.add(value)

    def sanitize(self, value: object, key: str = "") -> object:
        if key.lower() == "headers":
            pairs = [(name, "[REDACTED_SECRET]" if sensitive_header(name) and content else self.sanitize(content, name))
                     for name, content in header_pairs(value)]
            return dict(pairs) if isinstance(value, dict) else [list(pair) for pair in pairs]
        if isinstance(value, dict):
            return {child: self.sanitize(item, child) for child, item in value.items()}
        if isinstance(value, (list, tuple)):
            return [self.sanitize(item, key) for item in value]
        if not isinstance(value, str):
            return value
        if (key.lower() in SECRET_FIELDS or sensitive_header(key)) and value:
            return "[REDACTED_SECRET]"
        for secret in sorted(self.secrets, key=len, reverse=True):
            value = value.replace(secret, "[REDACTED_SECRET]")
        value = re.sub(r"(?i)(api_key=)[^&\s\"]+", r"\1[REDACTED_TOKEN]", value)
        value = re.sub(r"(?i)(/Auth/Keys/)[^/?\s]+", r"\1[REDACTED_KEY]", value)
        for before, after in ((str(DATA), "/reference-data"), (str(ROOT), "/reference-keys-m5d-private"),
                              ("/opt/goby-fixtures", "/reference-fixtures"), (str(RUNTIME), "/reference-runtime")):
            value = value.replace(before, after)
        return value

    def write(self, label: str, value: dict) -> None:
        require(re.fullmatch(r"[a-z0-9-]+", label) is not None, "Unsafe capture label")
        value.setdefault("reference", {"product": "Emby Server", "version": "4.9.5.0",
                                       "capturedAt": dt.datetime.now(dt.timezone.utc).isoformat()})
        self.collect_secrets(value)
        cleaned = self.sanitize(value)
        text = json.dumps(cleaned, indent=2, ensure_ascii=False) + "\n"
        require(not any(secret and secret in text for secret in self.secrets), "Secret survived export redaction")
        save(RAW / (PREFIX + label + ".json"), value)
        save(EXPORT / (PREFIX + label + ".json"), text)

    def request(self, label: str, method: str, path: str, *, token: str = "", query_token: bool = False,
                body: dict | None = None, identity: str = "") -> tuple[int, object]:
        parsed = urlsplit(path)
        if method != "GET":
            if parsed.path == "/emby/Users/AuthenticateByName":
                require(method == "POST" and identity in {"admin", "viewer"} and identity not in self.logins,
                        "Login is outside the owned scope")
            elif parsed.path == "/emby/Auth/Keys":
                values = parse_qs(parsed.query, keep_blank_values=True)
                require(method == "POST" and len(values.get("App", [])) == 1 and
                        values["App"][0] in self.apps, "Key creation is outside the owned application scope")
            elif parsed.path == "/emby/Sessions/Logout":
                require(method == "POST" and token and (token in self.owned or
                        any(login["AccessToken"] == token for login in self.logins.values())), "Logout is not owned")
            else:
                matches = [key for key in self.owned if parsed.path in {
                    "/emby/Auth/Keys/" + quote(key, safe=""), "/emby/Auth/Keys/" + quote(key, safe="") + "/Delete"}]
                require(len(matches) == 1 and ((method == "DELETE" and not parsed.path.endswith("/Delete")) or
                        (method == "POST" and parsed.path.endswith("/Delete"))), "Deletion is outside the owned keys")
        headers = {"Accept": "application/json"}
        if identity:
            headers["Authorization"] = ('Emby Client="Goby Keys M5d Recorder", Device="Keys M5d Owned", '
                f'DeviceId="{DEVICE}-{identity}", Version="0.1.0"')
        if token:
            self.secrets.add(token)
            if query_token:
                path += ("&" if parsed.query else "?") + urlencode({"api_key": token})
            else:
                headers["X-Emby-Token"] = token
        wire = None
        if body is not None:
            headers["Content-Type"] = "application/json"
            wire = json.dumps(body, separators=(",", ":")).encode()
        connection = http.client.HTTPConnection("127.0.0.1", 18097, timeout=10)
        try:
            connection.request(method, path, wire, headers)
            response = connection.getresponse()
            content = response.read(MAX_BODY + 1)
            require(len(content) <= MAX_BODY, "Response exceeded its capture bound")
            self.total += len(content)
            require(self.total <= MAX_TOTAL, "Capture exceeded its total wire bound")
            text = content.decode("utf-8")
            try:
                result, kind = json.loads(text), "json"
            except json.JSONDecodeError:
                result, kind = text, "text"
            status_code, response_headers = response.status, response.getheaders()
        finally:
            connection.close()
        if identity and isinstance(result, dict) and result.get("AccessToken"):
            self.logins[identity] = result
            self.secrets.add(result["AccessToken"])
            save(PRIVATE / (identity + "-login.json"), result)
        self.write(label, {"request": {"method": method, "path": path, "headers": headers, "body": body},
            "response": {"status": status_code, "headers": response_headers, "bodyType": kind, "body": result},
            "observation": {"completeHTTP": True, "wireBytes": len(content)}})
        print(json.dumps({"capture": PREFIX + label, "status": status_code, "bodyType": kind, "bytes": len(content)}), flush=True)
        return status_code, result

    def key_list(self, label: str) -> list[dict]:
        status_code, result = self.request(label, "GET", "/emby/Auth/Keys", token=self.logins["admin"]["AccessToken"])
        require(status_code == 200 and isinstance(result, dict) and isinstance(result.get("Items"), list),
                "Key list response shape differs; inspect private evidence")
        keys = result["Items"]
        require(all(isinstance(item, dict) and isinstance(item.get("AccessToken"), str) for item in keys),
                "Key records omit their credential field")
        if self.old_keys is not None:
            old_tokens = {item["AccessToken"] for item in self.old_keys}
            for item in keys:
                if item["AccessToken"] not in old_tokens and item.get("AppName") in self.apps:
                    self.owned[item["AccessToken"]] = item
        return keys

    def login(self, identity: str) -> None:
        values = self.credentials(identity)
        status_code, result = self.request(identity + "-login", "POST", "/emby/Users/AuthenticateByName", identity=identity,
            body={"Username": values["REFERENCE_USERNAME"], "Pw": values["REFERENCE_PASSWORD"]})
        require(status_code == 200 and identity in self.logins, "Owned ordinary login failed")

    def capture(self) -> None:
        self.login("admin")
        self.old_keys = self.key_list("list-before")
        save(PRIVATE / "old-keys.json", self.old_keys)
        require(not any(item.get("AppName", "").startswith(APP_PREFIX) for item in self.old_keys), "Application prefix already exists")
        self.request("list-anonymous", "GET", "/emby/Auth/Keys")
        self.login("viewer")
        admin = self.logins["admin"]["AccessToken"]
        viewer = self.logins["viewer"]["AccessToken"]
        self.request("list-viewer", "GET", "/emby/Auth/Keys", token=viewer)
        for label, suffix, credential in (("create-viewer", "Viewer Denial", viewer),
                                           ("create-alpha", "Alpha", admin), ("create-beta", "Beta", admin),
                                           ("create-alpha-duplicate", "Alpha", admin)):
            app = APP_PREFIX + suffix
            self.apps.add(app)
            self.request(label, "POST", "/emby/Auth/Keys?" + urlencode({"App": app}), token=credential)
        keys = self.key_list("list-created")
        require(len(self.owned) >= 3, "Expected at least three owned application keys")
        for label, query in (("page-zero", {"StartIndex": 0, "Limit": 1}), ("page-one", {"StartIndex": 1, "Limit": 1}),
                             ("page-empty", {"StartIndex": len(keys) + 10, "Limit": 2}), ("limit-zero", {"Limit": 0}),
                             ("start-negative", {"StartIndex": -1, "Limit": 1}), ("limit-negative", {"Limit": -1}),
                             ("limit-malformed", {"Limit": "nope"})):
            self.request(label, "GET", "/emby/Auth/Keys?" + urlencode(query), token=admin)
        own = list(self.owned)
        key = own[0]
        user_id = self.logins["admin"]["User"]["Id"]
        viewer_id = self.logins["viewer"]["User"]["Id"]
        for carrier in ("header", "query"):
            for label, route in (("keys", "/emby/Auth/Keys"), ("users", "/emby/Users"),
                                 ("admin-user", "/emby/Users/" + user_id), ("viewer-user", "/emby/Users/" + viewer_id),
                                 ("items", "/emby/Items?Limit=1"), ("viewer-items", "/emby/Users/" + viewer_id + "/Items?Limit=1"),
                                 ("sessions", "/emby/Sessions")):
                self.request("key-" + carrier + "-" + label, "GET", route, token=key, query_token=carrier == "query")
        app = APP_PREFIX + "Issued By Key"
        self.apps.add(app)
        self.request("create-by-key", "POST", "/emby/Auth/Keys?" + urlencode({"App": app}), token=key)
        self.key_list("list-after-key-use")
        self.request("logout-key", "POST", "/emby/Sessions/Logout", token=key)
        self.request("key-after-logout", "GET", "/emby/Auth/Keys", token=key)
        self.key_list("list-after-key-logout")
        other = own[1]
        self.request("delete-viewer-denied", "DELETE", "/emby/Auth/Keys/" + quote(other, safe=""), token=viewer)
        self.request("delete-owned", "DELETE", "/emby/Auth/Keys/" + quote(other, safe=""), token=admin)
        self.request("deleted-key-protected", "GET", "/emby/Users", token=other)
        self.request("delete-owned-repeat", "DELETE", "/emby/Auth/Keys/" + quote(other, safe=""), token=admin)
        alias = own[2]
        self.request("delete-alias-owned", "POST", "/emby/Auth/Keys/" + quote(alias, safe="") + "/Delete", token=admin)
        self.request("alias-deleted-key-protected", "GET", "/emby/Users", token=alias, query_token=True)

    def logout_owned_logins(self) -> None:
        for identity in ("viewer", "admin"):
            if identity not in self.logins or identity in self.logged_out:
                continue
            try:
                status_code, _ = self.request(identity + "-logout", "POST", "/emby/Sessions/Logout",
                                              token=self.logins[identity]["AccessToken"])
                self.logout_statuses[identity] = status_code
                if status_code in {200, 204}:
                    self.logged_out.add(identity)
            except Exception:
                self.logout_statuses[identity] = None
        require(self.logged_out == set(self.logins), "One or more owned login logouts did not succeed")

    def finish(self) -> None:
        if "admin" not in self.logins:
            return
        if self.old_keys is None:
            self.logout_owned_logins()
            return
        admin = self.logins["admin"]["AccessToken"]
        current = self.key_list("cleanup-list")
        for item in current:
            token = item["AccessToken"]
            if token in self.owned:
                self.cleanup_count += 1
                self.request(f"cleanup-delete-{self.cleanup_count}", "DELETE", "/emby/Auth/Keys/" + quote(token, safe=""), token=admin)
        final = self.key_list("list-final")
        require(sorted(final, key=lambda item: item["AccessToken"]) == sorted(self.old_keys, key=lambda item: item["AccessToken"]),
                "Existing API-key records changed or owned keys remain")
        self.logout_owned_logins()
        require(preconditions() == self.pid, "Reference process changed during capture")
        require(self.snapshot() == self.baseline, "An existing record or source file changed")
        for path in RAW.glob(PREFIX + "*.json"):
            original = json.loads(path.read_text())
            exported = json.loads((EXPORT / path.name).read_text())
            require(self.sanitize(original) == exported, "Capture redaction audit differs")
            private_file(path)
            private_file(EXPORT / path.name)
        self.write("audit", {"kind": "capture-audit-observation", "referencePID": self.pid,
            "preservedOldRecords": 958, "preservedOldRecordFiles": len(self.baseline["records"]),
            "preservedOldMediaFiles": len(self.baseline["media"]), "oldHashesUnchanged": True,
            "oldKeyRecordsUnchanged": True, "oldKeyCount": len(self.old_keys),
            "ownedKeysObserved": len(self.owned), "ownedKeysRemaining": 0,
            "ownedLoginsLoggedOut": len(self.logged_out), "ownedLoginLogoutStatuses": self.logout_statuses,
            "wireBytes": self.total,
            "completeHTTP": len(list(RAW.glob(PREFIX + "*.json")))})


def main() -> None:
    pid = preconditions()
    recorder = Recorder(pid)
    error = None
    try:
        recorder.capture()
    except Exception as caught:
        error = type(caught).__name__
        save(PRIVATE / "failure.txt", error + ": " + str(caught))
    finally:
        try:
            recorder.finish()
        except Exception as caught:
            save(PRIVATE / "cleanup-failure.txt", type(caught).__name__ + ": " + str(caught))
            print(json.dumps({"capture": "keys-m5d", "cleanup": "failed", "failureType": type(caught).__name__}), flush=True)
            raise SystemExit(1)
    print(json.dumps({"capture": "keys-m5d", "result": "complete" if error is None else "partial", "failureType": error}), flush=True)
    if error:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
