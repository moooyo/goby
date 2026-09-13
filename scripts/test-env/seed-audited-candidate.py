#!/usr/bin/env python3
"""Seed one already-running, pinned candidate; never admit or restart it."""
from __future__ import annotations
import argparse
import base64
import hashlib
import http.client
from http.cookies import SimpleCookie
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import stat
import sys
import time
import types
from urllib.parse import urlencode, urlsplit

C = Path("/opt/goby-audited-candidate-20260913T073217Z-ef77f9ffcf0b")
R = Path("/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14")
MEDIA = Path("/opt/goby-fixtures/client-m3e")
MANIFEST = {"path": str(C / "private/manifest.json"), "sha256": "022ca1e48aac6159750df72157dcddff3738e67962012f556825e26b0f0dfb09"}
INSPECTION = {"path": str(R / "candidate-runtime-inspection-01/report-02.json"), "sha256": "99ef8f05d9ad6f1b1534ea678fcdc36b3c920680d2de2ae4053968ab8197ae95"}
BUDGETS = {"maximumSeconds": 1200, "cleanupSeconds": 120, "maximumRequests": 240, "cleanupRequests": 8}
SCENARIOS = ("movie", "episode", "mp3", "flac", "subtitles", "tv-browse")
MOVIE_SHA = "7265bc56bd7f495bcbd5224adcf6df94478a99d1994ba713274a194f8f9db088"
FILES = {
    ".goby-managed": "82e6421f62a15c53c5756f4d08b10c89158d79daab7f04c397b784d465e6c585",
    "Movies/M3e Client Movie.en.srt": "aff96165b6479ebc0710e56faa52df525b13ea7dbc8f44c6fbb6d91e75f9838d",
    "Movies/M3e Client Movie.en.vtt": "efbce0e543ce0acef4a0ab9f979f2a4b5386658d4a96222ec95bdf917139dd6b",
    "Movies/M3e Client Movie.mp4": MOVIE_SHA,
    "Movies/M3e Client Movie.nfo": "af216bbe0582eef14ed88e1f41a927499413abc79917f088397f43edf09dd534",
    "Music/M3e Client Audio.flac": "b4c791e310105a785172d668d0be67d6834436cab2adb0a566d29df4d82daa9b",
    "Music/M3e Client Audio.mp3": "92cb857d16b92c1673ad6bfb299fa3986b3aa1771b5120d9e82490b61f0d89b0",
    "TV/M3e Client Series/Season 01/M3e Client Series S01E01.mp4": MOVIE_SHA,
    "TV/M3e Client Series/Season 01/M3e Client Series S01E01.nfo": "6cc72f6dd1fcb62b1b962aed50d2cdf9aa0da6b788f31aaf892148b17c319eeb",
    "TV/M3e Client Series/Season 01/M3e Client Series S01E02.mp4": MOVIE_SHA,
    "TV/M3e Client Series/Season 01/M3e Client Series S01E02.nfo": "500b190b44c8a8cf51b570a3c9c1273f579b574958853888f1059a78665f0db5",
    "TV/M3e Client Series/Season 02/M3e Client Series S02E01.mp4": MOVIE_SHA,
    "TV/M3e Client Series/Season 02/M3e Client Series S02E01.nfo": "386cabb6a34036f9346bf4b412fc17f9e3e7b53d940b9f735044abbce94749e7",
    "TV/M3e Client Series/tvshow.nfo": "60ddaac74f6de23ba93d59d3adb0d2cd966d26591bd5ca486b0ae4378e245afc",
}
LIBRARIES = (("Movies", "M3e Client Movies", "movies"), ("TV", "M3e Client Television", "tvshows"), ("Music", "M3e Client Music", "music"))
PLAYBACK = {"EnableMediaPlayback": True, "EnablePlaybackRemuxing": True, "EnableAudioPlaybackTranscoding": True, "EnableVideoPlaybackTranscoding": True}
FORBIDDEN = {"dispose-source41-resource-full-failed-pair.py", "test-dispose-source41-resource-full-failed-pair.py", "upgrade-main-schema25.py"}


def need(value, reason):
    if not value:
        raise ValueError(reason)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def parse(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            need(key not in result, "Duplicate JSON key.")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=lambda value: (_ for _ in ()).throw(ValueError("Nonfinite JSON.")))


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + "\n").encode()


def file_identity(info):
    return tuple(getattr(info, name) for name in ("st_dev", "st_ino", "st_mode", "st_uid", "st_gid", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns"))


def safe_path(path, owners=(0,), directory=False):
    path = Path(path)
    need(path.is_absolute() and ".." not in path.parts and not any(c in str(path) for c in "\r\n\x00"), "Unsafe path.")
    for node in (path, *path.parents):
        info = node.lstat()
        need(not stat.S_ISLNK(info.st_mode) and info.st_uid in owners and not info.st_mode & 0o022, "Unsafe path authority.")
        need(stat.S_ISDIR(info.st_mode) if directory or node != path else stat.S_ISREG(info.st_mode), "Unexpected path type.")
    return path.lstat()


def read_checked(path, sha256, limit=256 << 20):
    path = Path(path)
    need(path.name not in FORBIDDEN and isinstance(sha256, str) and re.fullmatch(r"[0-9a-f]{64}", sha256), "Invalid read authority.")
    before = safe_path(path)
    need(before.st_nlink == 1 and before.st_size <= limit, "Authority file bound differs.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        need(file_identity(os.fstat(stream.fileno())) == file_identity(before), "Authority changed during open.")
        raw = stream.read(limit + 1)
        need(file_identity(os.fstat(stream.fileno())) == file_identity(before), "Authority changed during read.")
    need(file_identity(path.lstat()) == file_identity(before) and len(raw) == before.st_size and sha(raw) == sha256, "Authority bytes changed.")
    return raw


def descriptor(row):
    need(isinstance(row, dict) and set(row) == {"path", "sha256"}, "Invalid descriptor.")
    return parse(read_checked(row["path"], row["sha256"]))


def sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def write_once(path, raw):
    path = Path(path)
    safe_path(path.parent, directory=True)
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    sync_dir(path.parent)
    return {"path": str(path), "sha256": sha(raw)}


def write_json_once(path, value):
    return write_once(path, encoded(value))


def response_complete(response, method, raw, maximum):
    if len(raw) > maximum or response.length not in (None, 0):
        return False
    if method == "HEAD" or response.status in (204, 304):
        return not raw
    return response.isclosed()


class CandidateIO:
    """Bound IO only. Construction never authenticates or changes the candidate."""
    def __init__(self, value, input_pin, source_pin):
        self.value, self.input_pin, self.source_pin = value, input_pin, source_pin
        self.output, self.private = Path(value["output"]), Path(value["output"]) / "private"
        self.budgets, self.requests = value["budgets"], {"normal": 0, "cleanup": 0}
        self.started, self.byte_count, self.last_response = time.monotonic(), 0, None
        self.created, self.request_states = False, []
        need(self.output.parent == R and re.fullmatch(r"[a-z0-9][a-z0-9-]{1,79}", self.output.name), "Fresh output must be directly within the resumed scope.")
        need(value["candidateManifest"] == MANIFEST and value["runtimeInspection"] == INSPECTION, "Candidate authority differs.")
        need(set(self.budgets) == set(BUDGETS) and all(type(n) is int and n > 0 for n in self.budgets.values()) and
             self.budgets["maximumSeconds"] <= 1200 and self.budgets["cleanupSeconds"] < self.budgets["maximumSeconds"] and
             self.budgets["maximumRequests"] <= 240 and self.budgets["cleanupRequests"] < self.budgets["maximumRequests"], "Invalid IO budget.")

    def deadline(self, cleanup=False):
        left = self.budgets["maximumSeconds"] - (0 if cleanup else self.budgets["cleanupSeconds"]) - (time.monotonic() - self.started)
        need(left > 0, "The frozen elapsed-time budget expired.")
        return left

    def open(self):
        need(not os.path.lexists(self.output), "Fresh output collision; resume is forbidden.")
        self.candidate = descriptor(self.value["candidateManifest"])
        attestation = descriptor(self.value["runtimeInspection"])
        need(attestation["kind"] == "audited-candidate-runtime-inspection" and attestation["status"] == "ready_pending_live_acceptance" and
             attestation["failure"] is None and attestation["manifest"] == MANIFEST and attestation["database"]["sourceSchemaVersion"] == 28 and
             attestation["database"]["sourceUsers"] == 0 and attestation["database"]["recoveryTargetEmpty"] is True and
             attestation["processes"]["server"] == self.candidate["serverIdentity"] and
             attestation["processes"]["postgres"] == self.candidate["postgresIdentity"] and
             attestation["httpListener"]["socketInode"] == self.candidate["listener"]["socketInode"], "Runtime inspection binding differs.")
        helper = self.value["runtimeHelper"]
        need(Path(helper["path"]).is_relative_to(R) and Path(helper["path"]).suffix == ".py", "Runtime helper escaped the approved scope.")
        module = types.ModuleType("frozen_candidate_provision")
        module.__file__ = helper["path"]
        exec(compile(read_checked(helper["path"], helper["sha256"]), helper["path"], "exec"), module.__dict__)
        self.provision = module.Provision({"runId": self.candidate["runId"], "ports": self.candidate["ports"]}, self.input_pin, helper)
        self.provision.private = self.private
        self.provision.argv = {"server": [str(C / "install/goby")], "postgres": [str(module.PG / "postgres"), "-D", str(C / "postgres/data"), "-c", "config_file=" + str(C / "postgres/server.conf")]}
        self.provision.pg_identity, self.provision.pg_version = self.candidate["postgresIdentity"], self.candidate["postgresVersionNum"]
        need(self.provision.root == C and self.candidate["dataDirectory"] == str(C / "data") and
             self.candidate["status"] == "running_awaiting_live_acceptance" and self.candidate["bootstrapExecuted"] is False, "Unexpected candidate layout.")
        for role in ("server", "postgres"):
            need(self.candidate["units"][role]["path"] == "/run/systemd/system/" + self.provision.units[role], "Unit binding differs.")
            read_checked(**dict(path=self.candidate["units"][role]["path"], sha256=self.candidate["units"][role]["sha256"]))
        read_checked(self.candidate["binary"]["path"], self.candidate["binary"]["sha256"])
        read_checked(self.candidate["runtime"]["path"], self.candidate["runtime"]["sha256"])
        endpoint = urlsplit(self.candidate["directUrl"])
        need(self.candidate["directUrl"] == "http://127.0.0.1:" + str(self.candidate["ports"]["http"]) and endpoint.port not in (18096, 18196, 18197, 18198), "Direct endpoint differs.")
        self.port = endpoint.port
        write_json_once(self.output.with_name(self.output.name + "-intent.json"), {"operation": "create-fresh-output", "output": str(self.output), "input": self.input_pin, "source": self.source_pin, "budgets": self.budgets})
        os.mkdir(self.output, 0o700)
        self.created = True
        sync_dir(self.output.parent)
        os.mkdir(self.private, 0o700)
        sync_dir(self.output)
        write_json_once(self.private / "binding.json", {"candidate": MANIFEST, "runtimeInspection": INSPECTION, "runtimeHelper": helper, "input": self.input_pin, "source": self.source_pin})
        self.pin()
        return self

    def pin(self):
        for role in ("server", "postgres"):
            need(self.provision.show(self.provision.units[role]) == self.candidate["processes"][role] and
                 self.provision.process(role, self.candidate["processes"][role]) == self.candidate[role + "Identity"], "Candidate unit or process drifted.")
        listener = self.provision.listener(self.candidate["processes"]["server"], self.candidate["serverIdentity"])
        need(listener == self.candidate["listener"], "Candidate listening socket changed.")
        identity = self.candidate["serverIdentity"]
        proc = Path("/proc") / str(identity["pid"])
        return {"bootId": identity["bootId"], "pid": identity["pid"], "startTicks": identity["startTicks"], "uid": identity["uid"], "exe": identity["exe"],
                "exeDevice": identity["executableDevice"], "exeInode": identity["executableInode"], "cmdline": self.provision.argv["server"],
                "cgroup": (proc / "cgroup").read_text(), "networkNamespace": os.readlink(proc / "ns/net"),
                "listener": {"host": "127.0.0.1", "port": self.port, "socketInode": listener["socketInode"]}}

    def request(self, label, method, route, body=None, auth=None, expected=(200,), cleanup=False, headers=None, maximum=2 << 20):
        need(re.fullmatch(r"[a-z0-9-]{1,70}", label) and method in ("GET", "HEAD", "POST", "PUT", "DELETE") and
             route.startswith("/") and not route.startswith("//") and not any(c in route for c in "\r\n\x00#"), "Invalid bounded HTTP request.")
        need(type(maximum) is int and 0 < maximum <= 64 << 20, "Response limit differs.")
        slot = "cleanup" if cleanup else "normal"
        cap = self.budgets["cleanupRequests"] if cleanup else self.budgets["maximumRequests"] - self.budgets["cleanupRequests"]
        need(self.requests[slot] < cap, "HTTP request reserve exhausted.")
        self.deadline(cleanup)
        self.pin()
        request_headers = {"Origin": self.candidate["publicUrl"], "Connection": "close", "Accept-Encoding": "identity", **(headers or {})}
        if auth:
            need(auth["kind"] in ("native", "emby"), "Unknown credential transport.")
            if auth["kind"] == "native":
                request_headers.update({"Cookie": "goby_session=" + auth["token"], "X-CSRF-Token": auth["csrf"]})
            else:
                request_headers["X-Emby-Token"] = auth["token"]
        if body is not None and not isinstance(body, bytes):
            body = encoded(body)
            request_headers["Content-Type"] = "application/json"
        need(body is None or len(body) <= 1 << 20, "Request body exceeded its bound.")
        number = sum(self.requests.values()) + 1
        base = "%03d-%s" % (number, label)
        write_json_once(self.private / (base + "-intent.json"), {"method": method, "route": route, "headers": request_headers, "bodyBase64": base64.b64encode(body or b"").decode(), "maximumResponseBytes": maximum, "cleanup": cleanup})
        state = {"number": number, "label": label, "method": method, "route": route, "outcome": "unknown", "cleanup": cleanup}
        self.request_states.append(state)
        self.requests[slot] += 1
        self.last_response = None
        chunks = []
        connection = http.client.HTTPConnection("127.0.0.1", self.port, timeout=min(15, self.deadline(cleanup)))
        try:
            connection.request(method, route, body=body, headers=request_headers)
            response = connection.getresponse()
            self.last_response = result = {"label": label, "status": response.status, "headers": response.getheaders(), "raw": b"", "body": None, "receipt": None, "complete": False}
            received = 0
            while received <= maximum:
                remaining = self.deadline(cleanup)
                if response.fp is not None:
                    response.fp.raw._sock.settimeout(min(15, remaining))
                chunk = response.read1(min(65536, maximum + 1 - received))
                if not chunk:
                    break
                chunks.append(chunk)
                received += len(chunk)
            raw = b"".join(chunks)
            result.update(raw=raw, complete=response_complete(response, method, raw, maximum))
            self.byte_count += len(raw)
            need(self.byte_count <= 128 << 20, "Cumulative response bytes exceeded their bound.")
            raw_pin = write_once(self.private / (base + "-body.bin"), raw)
            result["receipt"] = write_json_once(self.private / (base + "-response.json"), {"status": response.status, "headers": result["headers"], "body": raw_pin, "bytes": len(raw), "complete": result["complete"]})
            state.update(outcome="response_received" if result["complete"] else "incomplete_response", status=response.status, receipt=result["receipt"])
            need(result["complete"], "Response is oversized or transport completion is unknown.")
            if raw and response.getheader("Content-Type", "").split(";", 1)[0].strip().lower() == "application/json":
                result["body"] = parse(raw)
            self.pin()
            self.deadline(cleanup)
            need(response.status in expected, "HTTP status differs from the frozen action.")
            return result
        except Exception:
            if self.last_response and self.last_response["receipt"] is None:
                self.last_response["raw"] = b"".join(chunks)
                try:
                    partial = write_once(self.private / (base + "-partial-body.bin"), self.last_response["raw"])
                    self.last_response["receipt"] = write_json_once(self.private / (base + "-partial-response.json"), {"status": self.last_response["status"], "headers": self.last_response["headers"], "body": partial, "complete": False})
                    state["partialReceipt"] = self.last_response["receipt"]
                except Exception as receipt_error:
                    state["receiptErrorType"] = type(receipt_error).__name__
            raise
        finally:
            connection.close()


def response_auth(response, kind):
    need(response and response["status"] == 200, "A successful login response is required.")
    if kind == "native":
        cookie = SimpleCookie()
        for key, value in response["headers"]:
            if key.lower() == "set-cookie":
                cookie.load(value)
        token = cookie["goby_session"].value
        need(re.fullmatch(r"[A-Za-z0-9_-]{16,4096}", token), "Unexpected native token format.")
        return {"kind": kind, "token": token, "csrf": sha(("goby:admin:csrf:" + token).encode())}
    need(response["complete"], "A complete observer login body is required.")
    token = parse(response["raw"])["AccessToken"]
    need(isinstance(token, str) and re.fullmatch(r"[A-Za-z0-9_-]{16,4096}", token), "Unexpected observer token format.")
    return {"kind": kind, "token": token}


def items(value):
    need(isinstance(value, dict) and isinstance(value.get("Items"), list) and value.get("TotalRecordCount") == len(value["Items"]) and len(value["Items"]) <= 100, "Incomplete or oversized catalog listing.")
    return value["Items"]


def item_id(value):
    need(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{32}", value), "Invalid observed resource ID.")
    return value


def check_policy(user, all_folders):
    need(user["IsAdministrator"] is False and user["IsDisabled"] is False and user["Policy"] == {**PLAYBACK, "EnableAllFolders": all_folders, "EnabledFolders": []}, "Stored user policy differs.")


def completed_job(job, library_id, job_id):
    need(job["Id"] == job_id and job["LibraryId"] == library_id and job["ForceProbe"] is False and
         job["Status"] in ("pending", "running", "completed", "failed", "cancelled", "interrupted"), "Native scan identity or status differs.")
    need(job["Status"] not in ("failed", "cancelled", "interrupted"), "Owned scan did not complete.")
    if job["Status"] == "completed":
        need(not job["Error"], "Completed scan contains a warning; retain it for review.")
        return True
    return False


class Seed:
    def __init__(self, io):
        self.io, self.stage, self.sessions = io, "preflight", {}
        self.resources = {"users": [], "libraries": [], "jobs": [], "copiedFiles": []}
        self.cleanup = []

    def api(self, label, method, route, body=None, auth=None, expected=(200,)):
        return self.io.request(label, method, route, body, auth, expected)["body"]

    def copy_media(self):
        need(self.io.value["mediaManifest"]["path"] == str(MEDIA / "manifest.json"), "Original media manifest path differs.")
        manifest = descriptor(self.io.value["mediaManifest"])
        need(manifest["marker"] == "goby-client-media-m3e-v1" and manifest["files"] == FILES,
             "The approved fourteen-file media closure differs.")
        account = pwd.getpwnam("goby")
        destination = C / "data/media"
        safe_path(destination, (0, account.pw_uid), True)
        need(destination.stat().st_uid == account.pw_uid and not list(destination.iterdir()), "Candidate media root must be empty.")
        entries = list(MEDIA.rglob("*"))
        need(len(entries) <= 30 and all(not path.is_symlink() for path in entries) and
             {str(path.relative_to(MEDIA)) for path in entries if not path.is_dir()} == set(FILES) | {"manifest.json"}, "Original media membership differs.")
        write_json_once(self.io.private / "media-root-permissions-intent.json", {"operation": "protect-empty-media-root", "path": str(destination), "before": file_identity(destination.stat()), "owner": "root", "group": "goby", "mode": "0750"})
        os.chown(destination, 0, account.pw_gid)
        os.chmod(destination, 0o750)
        sync_dir(destination.parent)
        total = 0
        for number, (name, checksum) in enumerate(sorted(FILES.items()), 1):
            self.io.deadline()
            source, target = MEDIA / name, destination / name
            before = safe_path(source)
            need(before.st_nlink in ((1, 4) if checksum == MOVIE_SHA else (1,)) and before.st_size <= 256 << 20, "Original fixture file bound differs.")
            total += before.st_size
            need(total <= 512 << 20, "Media copy budget exceeded.")
            write_json_once(self.io.private / ("copy-%02d-intent.json" % number), {"source": str(source), "sourceIdentity": file_identity(before), "sha256": checksum, "destination": str(target), "bytes": before.st_size})
            for directory in reversed((target.parent, *target.parent.parents)):
                if directory.is_relative_to(destination) and not directory.exists():
                    os.mkdir(directory, 0o700)
                    os.chown(directory, 0, account.pw_gid)
                    os.chmod(directory, 0o550)
                    sync_dir(directory.parent)
            safe_path(target.parent, (0, account.pw_uid), True)
            digest = hashlib.sha256()
            with os.fdopen(os.open(source, os.O_RDONLY | os.O_NOFOLLOW), "rb") as src, os.fdopen(os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as dst:
                need(file_identity(os.fstat(src.fileno())) == file_identity(before), "Original fixture changed during open.")
                while True:
                    self.io.deadline()
                    chunk = src.read(1 << 20)
                    if not chunk:
                        break
                    digest.update(chunk)
                    dst.write(chunk)
                need(file_identity(os.fstat(src.fileno())) == file_identity(before) and digest.hexdigest() == checksum, "Original fixture changed during copy.")
                os.fchown(dst.fileno(), 0, account.pw_gid)
                os.fchmod(dst.fileno(), 0o440)
                dst.flush()
                os.fsync(dst.fileno())
            sync_dir(target.parent)
            need(file_identity(source.lstat()) == file_identity(before), "Original fixture metadata changed.")
            copied = safe_path(target, (0, account.pw_uid))
            need(copied.st_nlink == 1 and copied.st_uid == 0 and copied.st_gid == account.pw_gid and stat.S_IMODE(copied.st_mode) == 0o440 and copied.st_size == before.st_size and
                 (copied.st_dev, copied.st_ino) != (before.st_dev, before.st_ino), "Copied fixture authority differs.")
            with open(target, "rb") as stream:
                need(hashlib.file_digest(stream, "sha256").hexdigest() == checksum and file_identity(os.fstat(stream.fileno())) == file_identity(copied), "Copied bytes differ.")
            self.resources["copiedFiles"].append(write_json_once(self.io.private / ("copy-%02d-result.json" % number), {"path": str(target), "sha256": checksum, "bytes": copied.st_size, "identity": file_identity(copied)}))

    def credential(self, role, username, password, user_id):
        pin = write_json_once(self.io.private / (role + "-credentials.json"), {"actorId": item_id(user_id), "serverId": self.server_id, "username": username, "password": password})
        return {"id": user_id, "username": username, "credentials": pin}

    def login(self, kind, credential):
        label = "native-login" if kind == "native" else "observer-login"
        try:
            if kind == "native":
                response = self.io.request(label, "POST", "/admin/v1/session", {"Name": credential["username"], "Password": credential["password"]})
            else:
                response = self.io.request(label, "POST", "/emby/Users/AuthenticateByName", {"Username": credential["username"], "Pw": credential["password"]},
                    headers={"X-Emby-Authorization": 'Emby Client="Goby Candidate Seed", Device="Bounded Observer", DeviceId="' + self.io.value["runId"] + '-observer", Version="1"'})
        finally:
            if self.io.last_response and self.io.last_response.get("label") == label:
                try:
                    self.sessions[kind] = response_auth(self.io.last_response, kind)
                except (ValueError, KeyError, TypeError):
                    pass
        auth = self.sessions[kind]
        need(response["body"]["User"]["Id"] == credential["actorId"] and response["body"]["User"]["Name"] == credential["username"], "Login actor binding differs.")
        if kind == "native":
            need(response["body"]["CSRFToken"] == auth["csrf"], "Native CSRF response differs.")
        else:
            need(response["body"]["ServerId"] == self.server_id and response["body"]["SessionInfo"]["UserId"] == credential["actorId"], "Observer server/session binding differs.")
        return auth

    def logout(self):
        for kind, auth in reversed(list(self.sessions.items())):
            entry = {"kind": kind, "tokenSha256": sha(auth["token"].encode()), "logoutAcknowledged": False, "sameTokenRejected": False}
            self.cleanup.append(entry)
            try:
                native = kind == "native"
                result = self.io.request(kind + "-logout", "DELETE" if native else "POST", "/admin/v1/session" if native else "/emby/Sessions/Logout", auth=auth, expected=(204,), cleanup=True)
                entry.update(logoutAcknowledged=True, logout=result["receipt"])
                result = self.io.request(kind + "-same-token-rejected", "GET", "/admin/v1/session" if native else "/emby/Users/" + self.admin["id"], auth=auth, expected=(401,), cleanup=True)
                entry.update(sameTokenRejected=True, verification=result["receipt"])
            except Exception as error:
                entry["errorType"] = type(error).__name__

    def run(self):
        self.io.open()
        self.stage = "initial_empty_state"
        pg = self.io.candidate["processes"]["postgres"]
        need(self.io.provision.cluster(pg) == self.io.candidate["clusterSystemIdentifier"], "Owned cluster changed.")
        empty = parse(self.io.provision.psql("seed-initial-empty", "BEGIN READ ONLY; SELECT json_build_object('users',(SELECT count(*) FROM users),'libraries',(SELECT count(*) FROM libraries),'items',(SELECT count(*) FROM items),'schema',(SELECT max(version) FROM schema_migrations)); COMMIT;", self.io.candidate["database"]))
        need(empty == {"users": 0, "libraries": 0, "items": 0, "schema": 28}, "Candidate is no longer an empty source schema28.")
        self.io.provision.cluster(pg)
        public = self.api("public-system-info", "GET", "/emby/System/Info/Public")
        self.server_id = item_id(public["Id"])
        need(public["ProductName"] == "Goby" and public["StartupWizardCompleted"] is False and public["LocalAddress"] == self.io.candidate["publicUrl"], "Public candidate identity differs.")
        need(self.api("bootstrap-status", "GET", "/admin/v1/bootstrap") == {"Initialized": False}, "Bootstrap already executed.")
        self.stage = "copy_media"
        self.copy_media()
        suffix = self.io.value["runId"][-12:]
        name, password = "Audited admin " + suffix, secrets.token_hex(32)
        write_json_once(self.io.private / "admin-proposed-credential.json", {"username": name, "password": password, "serverId": self.server_id})
        self.stage = "bootstrap"
        user = self.api("bootstrap", "POST", "/admin/v1/bootstrap", {"SetupToken": self.io.candidate["setupToken"], "Name": name, "Password": password}, expected=(201,))["User"]
        need(user["Name"] == name and user["IsAdministrator"] is True and user["IsDisabled"] is False, "Bootstrap administrator differs.")
        self.resources["users"].append(item_id(user["Id"]))
        self.admin = self.credential("admin", name, password, user["Id"])
        try:
            auth = self.login("native", descriptor(self.admin["credentials"]))
            need([row["Id"] for row in items(self.api("initial-users", "GET", "/admin/v1/users", auth=auth))] == [self.admin["id"]] and
                 items(self.api("initial-libraries", "GET", "/admin/v1/libraries", auth=auth)) == [], "Unexpected native initial state.")
            self.stage = "scenario_users"
            self.actors = {}
            for role in (*SCENARIOS, "control-q"):
                username, secret = "Audited " + role + " " + suffix, secrets.token_hex(32)
                write_json_once(self.io.private / (role + "-proposed-credential.json"), {"username": username, "password": secret, "serverId": self.server_id})
                created = self.api("create-" + role, "POST", "/admin/v1/users", {"Name": username, "Password": secret, "IsAdministrator": False}, auth, (201,))["User"]
                user_id = item_id(created["Id"])
                self.resources["users"].append(user_id)
                mapped = self.credential(role, username, secret, user_id)
                managed = self.api("read-" + role, "GET", "/admin/v1/users/" + user_id, auth=auth)["User"]
                need(managed["Name"] == username and managed["Id"] == user_id, "Created user binding differs.")
                check_policy(managed, True)
                if role == "control-q":
                    body = {key: managed[key] for key in ("Revision", "Name", "IsAdministrator", "IsDisabled")}
                    body["Policy"] = {**PLAYBACK, "EnableAllFolders": False, "EnabledFolders": []}
                    self.api("restrict-control-q", "PUT", "/admin/v1/users/" + user_id, body, auth)
                    check_policy(self.api("verify-control-q", "GET", "/admin/v1/users/" + user_id, auth=auth)["User"], False)
                    self.control = mapped
                else:
                    self.actors[role] = mapped
            self.stage = "serial_libraries_and_scans"
            self.libraries = {}
            for directory, name, collection in LIBRARIES:
                route = "/admin/v1/libraries"
                library = self.api("create-library-" + directory.lower(), "POST", route,
                    {"Name": name, "CollectionType": collection, "Paths": [str(C / "data/media" / directory)], "Scan": False}, auth, (201,))["Library"]
                library_id = item_id(library["Id"])
                self.resources["libraries"].append(library_id)
                need(library["Name"] == name and library["CollectionType"] == collection and library["Paths"] == [str(C / "data/media" / directory)], "Native library mapping differs.")
                self.libraries[directory] = library
                job = self.api("start-scan-" + directory.lower(), "POST", route + "/" + library_id + "/scan", {}, auth, (202,))["Job"]
                job_id = item_id(job["Id"])
                self.resources["jobs"].append({"id": job_id, "libraryId": library_id, "status": job["Status"]})
                for attempt in range(60):
                    if completed_job(job, library_id, job_id):
                        self.resources["jobs"][-1]["status"] = "completed"
                        break
                    self.io.deadline()
                    time.sleep(min(5, self.io.deadline()))
                    rows = items(self.api("poll-" + directory.lower() + "-%02d" % attempt, "GET", "/admin/v1/jobs", auth=auth))
                    matching = [row for row in rows if row["Id"] == job_id]
                    need(len(matching) == 1, "Owned scan disappeared or became ambiguous.")
                    job = matching[0]
                    self.resources["jobs"][-1]["status"] = job["Status"]
                else:
                    need(completed_job(job, library_id, job_id), "Owned scan polling budget exhausted.")
                    self.resources["jobs"][-1]["status"] = "completed"
            self.stage = "map_actual_catalog"
            observer = self.login("emby", descriptor(self.admin["credentials"]))
            catalog = self.catalog(observer)
            need({row["Id"] for row in items(self.api("final-users", "GET", "/admin/v1/users", auth=auth))} == set(self.resources["users"]) and len(self.resources["users"]) == 8, "Final user membership differs.")
            need({row["Id"] for row in items(self.api("final-libraries", "GET", "/admin/v1/libraries", auth=auth))} == set(self.resources["libraries"]), "Final library membership differs.")
            final_jobs = items(self.api("final-jobs", "GET", "/admin/v1/jobs", auth=auth))
            need(len(final_jobs) == 3 and {row["Id"] for row in final_jobs} == {row["id"] for row in self.resources["jobs"]} and
                 all(completed_job(row, row["LibraryId"], row["Id"]) for row in final_jobs), "Unexpected final scan work.")
            self.catalog_pin = write_json_once(self.io.private / "catalog.json", catalog)
        finally:
            self.logout()
        need(len(self.cleanup) == 2 and all(row["logoutAcknowledged"] and row["sameTokenRejected"] for row in self.cleanup), "Helper credential cleanup remains incomplete.")
        self.io.deadline(cleanup=True)
        provision_input = descriptor(self.io.candidate["input"])
        read_checked(self.io.candidate["binary"]["path"], self.io.candidate["binary"]["sha256"])
        candidate_process = self.io.pin()
        self.io.deadline(cleanup=True)
        self.stage = "complete"
        return write_json_once(self.io.private / "manifest.json", {"kind": "audited-candidate-seed-manifest", "version": 1, "status": "seeded_pending_live_acceptance", "input": self.io.input_pin,
            "helper": self.io.source_pin, "candidateManifest": MANIFEST, "runtimeInspection": INSPECTION, "serverId": self.server_id, "admin": self.admin, "actors": self.actors, "controlQ": self.control,
            "catalog": catalog, "catalogFile": self.catalog_pin, "actualCatalogDtos": self.dto_pin, "libraries": self.libraries, "roots": {directory: library["Paths"][0] for directory, library in self.libraries.items()},
            "source": {"manifestSha256": provision_input["sourceManifest"]["sha256"], "binarySha256": self.io.candidate["binary"]["sha256"], "schema": 28},
            "processes": {"candidate": candidate_process}, "resources": self.resources, "cleanup": self.cleanup, "requests": self.io.requests, "budgets": BUDGETS,
            "elapsedMilliseconds": round((time.monotonic() - self.io.started) * 1000), "playbackRequests": 0, "clientAcceptance": False, "candidateAdmissionComplete": False})

    def catalog(self, auth):
        base = "/emby/Users/" + self.admin["id"]
        views = items(self.api("observer-views", "GET", base + "/Views", auth=auth))
        need({row["Id"] for row in views} == set(self.resources["libraries"]), "Public views differ from native libraries.")
        details = []
        for directory, library in self.libraries.items():
            query = urlencode({"ParentId": library["Id"], "Recursive": "true", "Limit": 100, "Fields": "Path,MediaStreams,MediaSources,UserData"})
            rows = items(self.api("catalog-" + directory.lower(), "GET", base + "/Items?" + query, auth=auth))
            for row in rows:
                item_id(row["Id"])
                detail = self.api("detail-" + row["Id"], "GET", base + "/Items/" + row["Id"], auth=auth)
                need(detail["Id"] == row["Id"] and detail["Type"] == row["Type"] and detail["ServerId"] == self.server_id, "Actual detail binding differs.")
                details.append(detail)
        need(len({row["Id"] for row in details}) == len(details) == 10, "The expected ten-item catalog closure differs.")
        self.dto_pin = write_json_once(self.io.private / "actual-catalog-dtos.json", details)
        return map_catalog(details, self.libraries)


def mapped_subtitle(row, codec):
    path = "Movies/M3e Client Movie.en." + codec
    need(codec in ("srt", "vtt") and row["Codec"] == codec and row["IsExternal"] is True and row["Language"] in ("en", "eng") and
         type(row["Index"]) is int and row["Index"] >= 0 and row["Path"] == str(C / "data/media" / path), "Actual external subtitle fields differ.")
    return {"index": row["Index"], "codec": codec, "language": row["Language"], "external": True, "sha256": FILES[path]}


def map_catalog(details, libraries):
    """Map observed native hierarchy; SeriesId is derived from actual parent edges."""
    def only(kind, path=None):
        rows = [row for row in details if row["Type"] == kind and (path is None or row.get("Path") == str(C / "data/media" / path))]
        need(len(rows) == 1, "Actual catalog mapping is absent or ambiguous.")
        return rows[0]

    def mapped(row, kind, name=None):
        need(row["Type"] == kind and (name is None or row["Name"] == name), "Actual item title/type differs.")
        result = {"id": item_id(row["Id"]), "type": kind, "name": row["Name"], "path": row["Path"]}
        if kind in ("Movie", "Episode", "Audio"):
            relative = str(Path(row["Path"]).relative_to(C / "data/media"))
            need(relative in FILES and type(row["RunTimeTicks"]) is int and row["RunTimeTicks"] > 0, "Unapproved playable item.")
            sources = row["MediaSources"]
            need(len(sources) == 1 and sources[0]["Path"] == row["Path"] and sources[0]["ItemId"] == row["Id"] and sources[0]["RunTimeTicks"] == row["RunTimeTicks"] and
                 sources[0]["SupportsDirectPlay"] is True and sources[0]["SupportsDirectStream"] is True, "Scanned source is not ready for its admitted software baseline.")
            need(row["UserData"]["Played"] is False and row["UserData"]["PlaybackPositionTicks"] == 0 and row["UserData"]["PlayCount"] == 0, "Seed observer unexpectedly has playback history.")
            result.update(runtimeTicks=row["RunTimeTicks"], mediaSha256=FILES[relative])
        return result

    movie = only("Movie", "Movies/M3e Client Movie.mp4")
    result = {"movie": mapped(movie, "Movie", "M3e Client Movie")}
    need(result["movie"]["runtimeTicks"] == 6000000000, "Movie duration differs.")
    streams = movie["MediaStreams"]
    need([row["Codec"] for row in streams if row["Type"] == "Video"] == ["h264"] and [row["Codec"] for row in streams if row["Type"] == "Audio"] == ["aac"], "Movie codec baseline differs.")
    subtitles = [row for row in streams if row["Type"] == "Subtitle"]
    need(len(subtitles) == 2, "Actual external subtitle inventory differs.")
    result["subtitles"] = []
    for codec in ("srt", "vtt"):
        matching = [row for row in subtitles if row["Codec"] == codec]
        need(len(matching) == 1, "Subtitle codec mapping differs.")
        result["subtitles"].append(mapped_subtitle(matching[0], codec))
    need(len({row["index"] for row in result["subtitles"]}) == 2, "Subtitle indexes overlap.")
    album = only("MusicAlbum")
    need(album["Name"] in ("Music", "M3e Synthetic Album"), "Album name differs.")
    result["album"] = {"id": item_id(album["Id"]), "name": album["Name"]}
    for codec in ("mp3", "flac"):
        row = only("Audio", "Music/M3e Client Audio." + codec)
        need(row["Name"] in ("M3e Client Audio", "M3e " + codec.upper()) and row["ParentId"] == album["Id"] and row["MediaSources"][0]["Container"] == codec and
             [stream["Codec"] for stream in row["MediaStreams"] if stream["Type"] == "Audio"] == [codec], "Actual audio mapping differs.")
        result[codec] = {**mapped(row, "Audio"), "container": codec}
        need(result[codec]["runtimeTicks"] == 1800000000, "Audio duration differs.")
    series = only("Series", "TV/M3e Client Series")
    result["series"] = mapped(series, "Series", "M3e Client Series")
    result["seasons"], result["episodes"] = [], []
    for number in (1, 2):
        rows = [row for row in details if row["Type"] == "Season" and row["ParentId"] == series["Id"] and row["IndexNumber"] == number]
        need(len(rows) == 1, "Actual season parent edge differs.")
        row = rows[0]
        result["seasons"].append({"id": item_id(row["Id"]), "type": "Season", "indexNumber": number, "seriesId": series["Id"], "parentId": row["ParentId"]})
    for season, episode in ((1, 1), (1, 2), (2, 1)):
        path = "TV/M3e Client Series/Season %02d/M3e Client Series S%02dE%02d.mp4" % (season, season, episode)
        row = only("Episode", path)
        need(row["ParentId"] == result["seasons"][season - 1]["id"] and row["ParentIndexNumber"] == season and row["IndexNumber"] == episode, "Actual episode parent edge differs.")
        projected = mapped(row, "Episode", "Episode %d-%d" % (season, episode))
        need(projected["runtimeTicks"] >= 1200000000, "Episode duration differs.")
        result["episodes"].append({**projected, "seriesId": series["Id"], "parentId": row["ParentId"], "parentIndexNumber": season, "indexNumber": episode})
    for field, directory in (("musicLibrary", "Music"), ("tvLibrary", "TV")):
        result[field] = {"id": item_id(libraries[directory]["Id"]), "name": libraries[directory]["Name"]}
    return result


def main():
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
         "Use authorized root SSH with /usr/bin/python3 -I -B.")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("input", "input-sha256", "source-sha256"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    read_checked(source_pin["path"], source_pin["sha256"])
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    value = descriptor(input_pin)
    need(set(value) == {"kind", "version", "runId", "output", "candidateManifest", "runtimeInspection", "runtimeHelper", "mediaManifest", "budgets"} and
         value["kind"] == "audited-candidate-seed-input" and value["version"] == 1 and value["budgets"] == BUDGETS and
         re.fullmatch(r"[a-z0-9][a-z0-9-]{1,79}", value["runId"]), "Unexpected one-shot seed contract.")
    job = Seed(CandidateIO(value, input_pin, source_pin))
    try:
        manifest = job.run()
        print(json.dumps({"status": "seeded_pending_live_acceptance", "manifest": manifest, "candidateAdmissionComplete": False}))
        return 0
    except Exception as error:
        failure = {"status": "seed_failed_resources_retained", "stage": job.stage, "errorType": type(error).__name__, "output": str(job.io.output),
                   "resources": job.resources, "requests": job.io.requests, "requestStates": job.io.request_states, "cleanup": job.cleanup, "automaticRetry": False,
                   "candidateAdmissionComplete": False, "receipt": None}
        try:
            if job.io.created:
                failure["receipt"] = write_json_once(job.io.private / "failure.json", failure)
        except Exception as receipt_error:
            failure["receiptErrorType"] = type(receipt_error).__name__
        print(json.dumps(failure))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
