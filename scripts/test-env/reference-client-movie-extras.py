#!/usr/bin/env python3
"""Capture bounded read-only extra-media contracts from the owned reference.

Only one new ordinary-viewer recorder credential is created and revoked. This
research never plays media or mutates preferences, metadata, libraries or plugins.
"""

from __future__ import annotations

import hashlib
import fcntl
import importlib.util
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
from urllib.parse import urlencode, urlsplit

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m3e")
ROOT = WORK / "reference-movie-extras-v1"
PRIVATE, EXPORT = ROOT / "private", ROOT / "export"
MARKER = "goby-m3e-reference-movie-extras-v1"
DEVICE = "goby-m3e-movie-extras-recorder-v1"
CLIENT = "Goby Movie Extras Contract Recorder"
PID, TICKS = 332054, "357218"
BINARY_SHA256 = "c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2"
MOVIE_NAME = "M3e Client Movie"
MOVIE_PATH = "/opt/goby-fixtures/client-m3e/Movies/M3e Client Movie.mp4"
MAX_BUSINESS = 12
PRIVATE_URL = re.compile(r"https?://[^\s\"'<>]+", re.IGNORECASE)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def load_support():
    source = Path("/opt/goby-test/workspace-operator-review.WCj6Fjt4/reference-client-initialization.py")
    expected = json.loads((WORK / "reference-client-initialization-v1/export/safety-report.json").read_text())["sourceSha256"]
    require(hashlib.sha256(source.read_bytes()).hexdigest() == expected, "The accepted recorder source changed.")
    spec = importlib.util.spec_from_file_location("movie_extra_capture_support", source)
    support = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(support)
    operator = support.load_operator()
    operator.canonical(source)
    require(operator.BINARY_SHA256 == BINARY_SHA256 and operator.digest(operator.BINARY, allow_links=True) == BINARY_SHA256,
            "The pinned reference executable changed.")
    support.ROOT, support.PRIVATE, support.EXPORT = ROOT, PRIVATE, EXPORT
    support.MARKER, support.DEVICE, support.CLIENT = MARKER, DEVICE, CLIENT
    return support, operator


def safe_urls(value):
    if isinstance(value, dict):
        return {key: safe_urls(item) for key, item in value.items()}
    if isinstance(value, list):
        return [safe_urls(item) for item in value]
    if isinstance(value, str):
        return PRIVATE_URL.sub("[redacted URL]", value).replace("/opt/goby-fixtures/client-m3e/", "[fixture media]/")
    return value


def make_recorder(support, operator, owner):
    class Recorder(support.Recorder):
        def __init__(self):
            super().__init__(operator, owner, operator.read_private(operator.BROWSER))
            self.business_count, self.sequence = 0, 0
            self.movie_id = None
            self.routes = {}
            self.configuration_preserved = False
            self.movie_metadata_preserved = False
            self.catalog = []
            self.media_unchanged = False
            self.recovering = False

        def approve(self, method, route, user_id, *, authenticated):
            require(user_id == self.user_id, "The request attempted a different research user.")
            parsed = urlsplit(route)
            require(not parsed.scheme and not parsed.netloc and not parsed.fragment, "External request routing is forbidden.")
            if method == "POST":
                require(route in ("/emby/Users/AuthenticateByName", "/emby/Sessions/Logout") and
                        authenticated is (route == "/emby/Sessions/Logout"), "Only the owned login and logout may use POST.")
                return
            require(method == "GET" and authenticated, "Only authenticated GET business requests are allowed.")
            if route == "/emby/Sessions":
                return
            require(route in self.routes.values(), "The request is outside the fixed extra-media capture list.")

        def request(self, label, method, route, **kwargs):
            self.approve(method, route, self.user_id, authenticated=kwargs.get("authenticated", True))
            if method == "GET" and route != "/emby/Sessions":
                require(self.business_count < MAX_BUSINESS, "The read-only business request budget is exhausted.")
                self.business_count += 1
            self.sequence += 1
            if self.recovering:
                label = "recovery-" + label
            # Reserve each attempt before HTTP dispatch, including a failed read.
            operator.save(PRIVATE / ("request-intent-%02d.json" % self.sequence),
                          {"marker": MARKER, "sequence": self.sequence, "label": label, "method": method,
                           "path": route, "businessRequestsReserved": self.business_count})
            return super().request(label, method, route, **kwargs)

        def export(self, path, value):
            return super().export(path, safe_urls(value))

        def get(self, label):
            status, body = self.request(label, "GET", self.routes[label])
            require(self.records[label]["response"]["complete"], "An extra-media response exceeded its read bound.")
            return status, body

        def logout(self):
            proof = PRIVATE / "recorder-revocation.json"
            if operator.present(proof):
                require(operator.read_private(proof) == {"marker": MARKER, "deviceId": DEVICE,
                        "logoutStatus": 204, "invalidTokenStatus": 401}, "The retained recorder revocation proof differs.")
                status, _ = self.request("recorder-token-invalid-rechecked", "GET", "/emby/Sessions")
                require(status == 401, "The recorded revoked token became valid.")
                self.revoked = True
                return
            acknowledgement = PRIVATE / "recorder-logout-record.json"
            if operator.present(acknowledgement):
                saved = operator.read_private(acknowledgement)
                if saved.get("response", {}).get("status") == 204 and saved["response"].get("complete"):
                    status, _ = self.request("recorder-token-invalid-after-acknowledged-logout", "GET", "/emby/Sessions")
                    require(status == 401, "The acknowledged logout did not invalidate the owned token.")
                    operator.save(proof, {"marker": MARKER, "deviceId": DEVICE, "logoutStatus": 204, "invalidTokenStatus": 401})
                    self.revoked = True
                    return
            return super().logout()

        def capture(self):
            self.request("recorder-login", "POST", "/emby/Users/AuthenticateByName", authenticated=False, login=True)
            require(self.token_identity_proven, "The new recorder credential was not proven.")
            user = "/emby/Users/" + self.user_id
            self.routes = {
                "viewer-before": user,
                "catalog": user + "/Items?" + urlencode({"Recursive": "true", "IncludeItemTypes": "Movie,Episode,Audio",
                    "Fields": "Path,MediaSources,MediaStreams", "Limit": "100"}),
            }
            status, before = self.get("viewer-before")
            require(status == 200 and isinstance(before, dict), "The viewer configuration baseline is unavailable.")
            status, catalog = self.get("catalog")
            require(status == 200 and isinstance(catalog, dict) and isinstance(catalog.get("Items"), list),
                    "The fixture catalog is unavailable.")
            rows = catalog["Items"]
            require(len(rows) == 6 and catalog.get("TotalRecordCount") == 6 and
                    all(isinstance(item, dict) and isinstance(item.get("Path"), str) and
                        item["Path"].startswith("/opt/goby-fixtures/client-m3e/") for item in rows),
                    "The catalog differs from the six approved synthetic media items.")
            matches = [item for item in rows if item.get("Name") == MOVIE_NAME and item.get("Path") == MOVIE_PATH and item.get("Type") == "Movie"]
            require(len(matches) == 1 and re.fullmatch(r"[A-Za-z0-9_-]{1,128}", str(matches[0].get("Id", ""))),
                    "The synthetic movie identity is ambiguous or unexpected.")
            self.movie_id = str(matches[0]["Id"])
            self.catalog = [{key: item.get(key) for key in ("Id", "Name", "Type", "Path", "ParentId", "MediaType")} for item in rows]
            movie = user + "/Items/" + self.movie_id
            theme = "/emby/Items/" + self.movie_id + "/ThemeMedia?"
            self.routes.update({
                "movie-before": movie,
                "intros": movie + "/Intros",
                "special-features": movie + "/SpecialFeatures",
                "special-features-fields": movie + "/SpecialFeatures?" + urlencode({"Fields": "Path,MediaSources,MediaStreams,Overview"}),
                "theme-inherit": theme + urlencode({"UserId": self.user_id, "InheritFromParent": "true", "EnableThemeSongs": "true", "EnableThemeVideos": "true"}),
                "theme-direct": theme + urlencode({"UserId": self.user_id, "InheritFromParent": "false", "EnableThemeSongs": "true", "EnableThemeVideos": "true"}),
                "theme-disabled": theme + urlencode({"UserId": self.user_id, "InheritFromParent": "true", "EnableThemeSongs": "false", "EnableThemeVideos": "false"}),
                "movie-after": movie,
                "viewer-after": user,
            })
            status, movie_before = self.get("movie-before")
            require(status == 200 and isinstance(movie_before, dict) and movie_before.get("Id") == self.movie_id,
                    "The synthetic movie detail is unavailable.")
            for label in ("intros", "special-features", "special-features-fields", "theme-inherit", "theme-direct", "theme-disabled"):
                self.get(label)
            status, movie_after = self.get("movie-after")
            self.movie_metadata_preserved = status == 200 and isinstance(movie_after, dict) and all(
                movie_before.get(key) == movie_after.get(key) for key in ("Id", "Name", "Path", "Type", "MediaSources", "MediaStreams", "RunTimeTicks"))
            status, after = self.get("viewer-after")
            self.configuration_preserved = status == 200 and isinstance(after, dict) and all(
                before.get(key) == after.get(key) for key in ("Configuration", "Policy"))
            require(self.movie_metadata_preserved and self.configuration_preserved, "Independent fixture metadata changed during read-only research.")

    recorder = Recorder()
    support.approve = recorder.approve
    return recorder


def capture(support, operator, owner, recover=False):
    recorder = make_recorder(support, operator, owner)
    failure = None
    operator.save(PRIVATE / ("recovery-worker.json" if recover else "worker.json"), operator.process_identity(os.getpid()))
    try:
        signal.alarm(85)
        if recover:
            recorder.recovering = True
            login = operator.read_private(PRIVATE / "recorder-login.json")
            recorder.token = login.get("AccessToken")
            require(isinstance(recorder.token, str) and recorder.token, "No owned recorder acknowledgement is available.")
            recorder.secrets.add(recorder.token)
            recorder.prove_login(200, login)
            recorder.sequence = max(int(path.stem.rsplit("-", 1)[1]) for path in PRIVATE.glob("request-intent-*.json"))
        else:
            recorder.capture()
    except Exception as error:
        failure = type(error).__name__
    finally:
        signal.alarm(50)
        try:
            recorder.logout()
        except Exception as error:
            failure = failure or type(error).__name__
        recorder.media_unchanged = operator.verify_media() == owner["media"]
        recorder.identity()
        signal.alarm(0)
    summary = {
        "marker": MARKER, "classification": "protocol research; not client acceptance", "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "referencePID": PID, "referenceStartTicks": TICKS, "referenceExecutableSha256": BINARY_SHA256,
        "businessRequests": recorder.business_count, "maximumBusinessRequests": MAX_BUSINESS, "requests": recorder.count,
        "recorderDeviceId": DEVICE, "newRecorderTokenRevoked": recorder.revoked, "movieId": recorder.movie_id,
        "approvedFixtureCatalog": recorder.catalog, "configurationAndPolicyPreserved": recorder.configuration_preserved,
        "movieMetadataPreserved": recorder.movie_metadata_preserved, "mediaManifestPreserved": recorder.media_unchanged,
        "playbackPreferenceMetadataOrLibraryWrites": 0, "browserSessionUsedOrRevoked": False,
        "oldReferenceRequests": 0, "failureType": failure, "recoveryOnly": recover,
        "caseStatuses": {key: record["response"]["status"] for key, record in recorder.records.items()},
        "scope": "Only the existing synthetic movie and six approved catalog media items were read. Empty results do not establish behavior for configured intros, special features, or theme media.",
    }
    export_root = EXPORT
    if recover:
        export_root = ROOT / "recovery-export"
        require(not operator.present(export_root), "Recovery evidence already exists.")
        export_root.mkdir(mode=0o700)
    for label, record in recorder.records.items():
        recorder.export(export_root / (label + ".json"), record)
    recorder.export(export_root / "report.json", summary)
    require(failure is None and recorder.revoked and recorder.media_unchanged, "Research or exact recorder cleanup is incomplete; evidence is retained.")
    print(json.dumps({"result": "complete", "businessRequests": recorder.business_count,
                      "newRecorderTokenRevoked": recorder.revoked, "report": str(export_root / "report.json")}), flush=True)


def main():
    arguments = sys.argv[1:]
    require(arguments in ([], ["_capture"], ["--recover"], ["_recover"]), "Unsupported capture invocation.")
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"), "Run through root SSH on test-env only.")
    os.umask(0o077)
    signal.signal(signal.SIGALRM, lambda *_: (_ for _ in ()).throw(TimeoutError("The bounded capture deadline expired.")))
    support, operator = load_support()
    operator.preconditions()
    owner = operator.read_private(operator.OWNER)
    operator.validate_owner(owner)
    require(owner.get("phase") == "ready" and owner["serviceIdentity"]["pid"] == PID and
            owner["serviceIdentity"]["startTicks"] == TICKS and operator.service_identity(owner) == owner["serviceIdentity"],
            "The exact approved fresh reference instance changed.")
    internal, recovering = arguments in (["_capture"], ["_recover"]), arguments in (["--recover"], ["_recover"])
    if internal:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT), "deviceId": DEVICE}, "The capture ownership differs.")
        signal.alarm(130)
        capture(support, operator, owner, recover=recovering)
        return
    if recovering:
        require(operator.read_private(ROOT / "OWNER.json") == {"marker": MARKER, "path": str(ROOT), "deviceId": DEVICE}, "The retained capture ownership differs.")
        if operator.present(PRIVATE / "worker.json"):
            worker = operator.read_private(PRIVATE / "worker.json")
            if operator.present(Path("/proc") / str(worker["pid"])):
                current = operator.process_identity(worker["pid"])
                require(any(current.get(key) != worker.get(key) for key in ("pid", "bootId", "startTicks")),
                        "The original capture worker is still live; recovery cannot race it.")
    else:
        require(not operator.present(ROOT), "The capture evidence already exists and will not be replaced.")
        require(operator.verify_media() == owner["media"], "The approved media manifest changed.")
        ROOT.mkdir(mode=0o700)
        operator.save(ROOT / "OWNER.json", {"marker": MARKER, "path": str(ROOT), "deviceId": DEVICE})
        PRIVATE.mkdir(mode=0o700)
        EXPORT.mkdir(mode=0o700)
        operator.save(PRIVATE / "intent.json", {"marker": MARKER, "sourceSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "serviceIdentity": owner["serviceIdentity"], "maximumBusinessRequests": MAX_BUSINESS, "newRecorderDeviceId": DEVICE,
            "businessWriteRequests": 0})
        operator.save(PRIVATE / "operator.lock", "")
    operator.canonical(PRIVATE / "operator.lock", mode=0o600)
    lock = os.open(PRIVATE / "operator.lock", os.O_RDWR | os.O_NOFOLLOW)
    try:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        os.close(lock)
        raise RuntimeError("Another capture or recovery supervisor is active.") from None
    namespace = os.open(f"/proc/{PID}/ns/net", os.O_RDONLY)
    try:
        require(os.fstat(namespace).st_ino == int(owner["serviceIdentity"]["networkNamespace"][5:-1]) and
                operator.service_identity(owner) == owner["serviceIdentity"], "The namespace handle changed before capture.")
        result = subprocess.run(["/usr/bin/nsenter", "--net=/proc/self/fd/" + str(namespace), "/usr/bin/python3", "-I", "-B",
                                 str(Path(__file__).absolute()), "_recover" if recovering else "_capture"],
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=150, check=False, pass_fds=(namespace,),
                                env=dict(operator.BASE_ENV, SSH_CONNECTION=os.environ["SSH_CONNECTION"]))
        require(result.returncode == 0, "The capture failed; retained private acknowledgement supports exact cleanup.")
        print(result.stdout.decode().strip())
    finally:
        os.close(namespace)
        os.close(lock)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"result": "failed", "failureType": type(error).__name__, "evidenceRetained": True}), file=sys.stderr)
        sys.exit(1)
