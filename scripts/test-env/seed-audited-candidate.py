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
E3 = Path("/opt/goby-test/embedded-candidate-20260914")
INITIAL_INSPECTION = E3 / "initial-runtime-inspection-sql-corrected"
INSPECTION_HELPER = E3 / "private/operators-sql-corrected/inspect-audited-candidate.py"
INSPECTION_PRIORS = {
    "priorMetadataFailure": {"path": str(E3 / "initial-runtime-inspection-01/report.json"), "sha256": "7622127b9f288bc274566f1f05af80883120258dc55641d5d71618a40a26605d"},
    "priorSqlFailure": {"path": str(E3 / "initial-runtime-inspection-02/report.json"), "sha256": "1aaad33511366fd3b9eb54923460314f0bdd63c4160deb6fa3710ba4816f83db"},
    "sqlFailureDiagnostic": {"path": str(E3 / "private/first-sql-diagnostic/report.json"), "sha256": "65370c3558444a5e7503e8709ea30ceee4e1cf11c61a468699cc6d2ac882c47d"},
}
INSPECTION_METADATA_REVIEW = {"path": str(E3 / "empty-environment-files-review.json"), "sha256": "1b518abf9491027366936e7b88e4738d22c0a609d87c029e2f5d54b11c8c9ce8"}
INSPECTION_SQL_CORRECTION = {"priorFailurePreserved": True, "priorSqlResultRecovered": False, "diagnosticReadProcessesClosed": True,
                             "passfilePolicy": "owned_socket_path_required_absent", "stderrPolicy": "empty_required"}
INSPECTION_BUDGETS = {"maximumSeconds": 180, "maximumSqlSessions": 16, "maximumHttpRequests": 64, "maximumHttpBodyBytes": 4194304}
EMBEDDED_BINARY_SHA = "59096592c1f145004e4f664a833227bb7ce019acee746cf345379349b2784312"
PRODUCT_EVIDENCE = {
    "sourceArchive": {"path": "/opt/goby-test/full-regression-20260914/retained/source.tar.gz", "sha256": "418f9803237e02e0859f9bef8f6ed646730acb590a3482867a288cd64e8a0fad"},
    "sourceManifest": {"path": "/opt/goby-test/full-regression-continuation-20260914/retained/source-manifest.json", "sha256": "95fe6a40ecabdfe6b48260dcf200cf8a49d0cf9664c407ca91cd9eaba539d6f6"},
    "embeddedBuildManifest": {"path": "/opt/goby-test/m6-embedded-20260914/artifacts/linux-amd64/manifest.json", "sha256": "a732425002323e0e29a4ca7cf9e0fd517d773c1de027d810488173b75957e683"},
    "productionSourceBinding": {"path": "/opt/goby-test/full-regression-20260914/production-source-binding.json", "sha256": "823870bb9bcf9e2ca4b35e483d617de34e1c541b273cd6bf02e647a066e920dc"},
    "regressionReview": {"path": "/opt/goby-test/full-regression-continuation-20260914/independent-review.json", "sha256": "5b946d4b4bfee2b177b53861c89fad690c08c406b832a26f778d563ed4174b72"},
    "regressionClosure": {"path": "/opt/goby-test/full-regression-continuation-20260914/closure.json", "sha256": "6cd0b024834315db240d6268c94b578e4beb99977fd1740d51c5494cf4508b1c"},
}
FRESH_INACCESSIBLE = ("/opt/goby-test", "/opt/goby-dev", "/opt/goby-client-m3e", "/opt/goby-fixtures", "/var/lib/goby-test", "/var/lib/postgresql", str(C))
MEDIA_TOTAL_BYTES = 201156949
MEDIA_MIN_FREE_BYTES = 64 << 20
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


def scoped_descriptor(row, root, suffix=None):
    """Validate a metadata selector without opening it or changing its root."""
    need(isinstance(row, dict) and set(row) == {"path", "sha256"} and isinstance(row["path"], str) and
         isinstance(row["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", row["sha256"]), "Invalid scoped descriptor.")
    path = Path(row["path"])
    need(path.is_absolute() and str(path) == row["path"] and path.is_relative_to(root) and ".." not in path.parts and
         not any(c in row["path"] for c in "\\\r\n\x00") and path.name not in FORBIDDEN and
         (suffix is None or path.suffix == suffix), "Metadata escaped its declared authority scope.")
    return row


def fresh_candidate_root(pin):
    scoped_descriptor(pin, Path("/opt"), ".json")
    root = Path(pin["path"]).parent.parent
    need(root.parent == Path("/opt") and root != C and
         re.fullmatch(r"goby-audited-candidate-[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}", root.name) and
         pin["path"] == str(root / "private/manifest.json"), "A fresh embedded candidate cannot select a legacy or arbitrary root.")
    return root


def validate_seed_input(value, input_pin=None, source_pin=None):
    common = {"kind", "version", "runId", "output", "candidateManifest", "runtimeInspection", "runtimeHelper", "mediaManifest", "budgets"}
    need(isinstance(value, dict) and type(value.get("version")) is int and value["version"] in (1, 2) and
         set(value) == common | ({"provisionInput", "inspectionHelper"} if value["version"] == 2 else set()) and
         value["kind"] == "audited-candidate-seed-input" and value["budgets"] == BUDGETS and
         re.fullmatch(r"[a-z0-9][a-z0-9-]{1,79}", value["runId"]), "Unexpected one-shot seed contract.")
    if value["version"] == 2:
        fresh_candidate_root(value["candidateManifest"])
        for key, suffix in (("runtimeInspection", ".json"), ("provisionInput", ".json"), ("runtimeHelper", ".py"), ("inspectionHelper", ".py"), ("mediaManifest", ".json")):
            scoped_descriptor(value[key], E3, suffix)
        need(value["runtimeInspection"]["path"] == str(INITIAL_INSPECTION / "report.json") and value["inspectionHelper"]["path"] == str(INSPECTION_HELPER),
             "Only the reviewed SQL-corrected initial inspection is admitted.")
        output = Path(value["output"])
        need(output.parent == E3 and str(output) == value["output"] and re.fullmatch(r"[a-z0-9][a-z0-9-]{1,79}", output.name), "Fresh seed output escaped its scope.")
        if input_pin is not None:
            scoped_descriptor(input_pin, E3, ".json")
        if source_pin is not None:
            scoped_descriptor(source_pin, E3, ".py")
    return value


def validate_fresh_authority(value, candidate, attestation, inspection_input, provision_input):
    """Bind the fresh instance and its initial read-only evidence without IO."""
    root = fresh_candidate_root(value["candidateManifest"])
    for key, suffix in (("runtimeInspection", ".json"), ("runtimeHelper", ".py"), ("inspectionHelper", ".py")):
        scoped_descriptor(value[key], E3, suffix)
    scoped_descriptor(attestation["input"], E3, ".json")
    need(value["runtimeInspection"]["path"] == str(INITIAL_INSPECTION / "report.json") and value["inspectionHelper"]["path"] == str(INSPECTION_HELPER),
         "Only the reviewed SQL-corrected initial inspection is admitted.")
    need(re.fullmatch(r"[0-9]{8}T[0-9]{6}Z-[0-9a-f]{12}", candidate["runId"]) and
         root.name == "goby-audited-candidate-" + candidate["runId"] and candidate["dataDirectory"] == str(root / "data"), "Candidate run identity differs.")
    need(type(candidate.get("provisionVersion")) is int and candidate["provisionVersion"] == 2 and candidate["status"] == "running_awaiting_live_acceptance" and
         all(candidate.get(key) is False for key in ("bootstrapExecuted", "recoveryRestoreExecuted", "candidateAdmissionComplete", "clientAcceptance")) and
         candidate["binary"] == {"path": str(root / "install/goby"), "sha256": EMBEDDED_BINARY_SHA} and
         candidate["runtime"]["path"] == str(root / "private/runtime.env") and candidate["productEvidence"] == PRODUCT_EVIDENCE and
         candidate["ordinaryRegressionStatus"] == "passed_with_explicit_profile_gap" and candidate["ordinaryRegressionPhases"] == 2 and
         candidate["taggedFullRegressionClaimed"] is False and candidate["sourceState"] == {"users": 0, "schema": 28, "migrations": 28},
         "The candidate is not the selected fresh embedded preparation.")
    need(candidate["dashboard"] == {"mode": "embedded", "buildManifest": PRODUCT_EVIDENCE["embeddedBuildManifest"], "assetCount": 57,
         "externalDirectoryInstalled": False, "webDirectoryOverridePresent": False} and
         len(candidate["inaccessiblePaths"]) == len(FRESH_INACCESSIBLE) and set(candidate["inaccessiblePaths"]) == set(FRESH_INACCESSIBLE) and
         all(candidate["loadedInaccessiblePaths"][role] == sorted(FRESH_INACCESSIBLE) for role in ("server", "postgres")),
         "The embedded dashboard or protected-instance boundary differs.")
    suffix = candidate["runId"].split("-")[-1]
    need(candidate["database"] == "goby_candidate_" + suffix and candidate["recoveryDatabase"] == "goby_recovery_" + suffix and
         type(candidate["serverIdentity"]["uid"]) is int and candidate["serverIdentity"]["uid"] > 0 and
         type(candidate["postgresIdentity"]["uid"]) is int and candidate["postgresIdentity"]["uid"] > 0,
         "The new database slots or non-root identities differ.")
    scoped_descriptor(candidate["input"], E3, ".json")
    need(value.get("provisionInput", candidate["input"]) == candidate["input"] == attestation["provisionInput"] == inspection_input["provisionInput"] and
         provision_input.get("kind") == "audited-candidate-provision-input" and type(provision_input.get("version")) is int and provision_input["version"] == 2 and
         provision_input["runId"] == candidate["runId"] and provision_input["ports"] == candidate["ports"] and
         provision_input["public_url"] == candidate["publicUrl"] and provision_input["dashboardProfile"] == "embedded-administrator-v1" and
         all(provision_input[key] == pin for key, pin in PRODUCT_EVIDENCE.items()), "The fresh provision input or product evidence changed.")
    need(attestation.get("kind") == "audited-candidate-runtime-inspection" and type(attestation.get("version")) is int and attestation["version"] == 3 and
         attestation["status"] == "ready_pending_seed" and attestation["stage"] == "complete" and attestation["failure"] is None and
         attestation["provisionManifest"] == value["candidateManifest"] and
         attestation["provisionHelper"] == value["runtimeHelper"] == inspection_input["provisionHelper"] and
         attestation["helper"] == value["inspectionHelper"] and attestation["sourceEvidence"] == candidate["productEvidence"] and
         attestation["binary"] == candidate["binary"] and type(attestation.get("databaseWrites")) is int and attestation["databaseWrites"] == 0 and
         all(attestation.get(key) is False for key in ("bootstrapPerformed", "restorePerformed", "serviceChangesPerformed", "candidateAdmissionComplete", "clientAcceptance")),
         "Fresh runtime inspection has an invalid result or authority binding.")
    inspection_keys = {"kind", "version", "output", "provisionManifest", "provisionInput", "provisionHelper", "sourceArchive", "sourceManifest", "budgets"} | set(INSPECTION_PRIORS)
    need(set(inspection_input) == inspection_keys and inspection_input.get("kind") == "audited-candidate-inspection-input" and
         type(inspection_input.get("version")) is int and inspection_input["version"] == 3 and
         inspection_input["output"] == str(INITIAL_INSPECTION) and inspection_input["provisionManifest"] == value["candidateManifest"] and
         inspection_input["sourceArchive"] == PRODUCT_EVIDENCE["sourceArchive"] and inspection_input["sourceManifest"] == PRODUCT_EVIDENCE["sourceManifest"] and
         encoded(inspection_input["budgets"]) == encoded(INSPECTION_BUDGETS),
         "The initial inspection selected another candidate or source.")
    need(all(inspection_input[key] == pin and attestation.get(key) == pin for key, pin in INSPECTION_PRIORS.items()) and
         attestation.get("metadataCorrectionReview") == INSPECTION_METADATA_REVIEW and
         encoded(attestation.get("sqlCorrection")) == encoded(INSPECTION_SQL_CORRECTION) and attestation.get("readProcessesClosed") is True,
         "The reviewed inspection correction omitted or changed its preserved failure evidence.")
    database = attestation["database"]
    need(database["sourceSchemaVersion"] == 28 and database["sourceMigrationCount"] == 28 and type(database["sourceUsers"]) is int and database["sourceUsers"] == 0 and
         database["recoveryTargetEmpty"] is True and database["allSqlTransactionsReadOnly"] is True and
         all(all(attestation["processes"][role].get(key) == item for key, item in candidate[role + "Identity"].items()) for role in ("server", "postgres")) and
         attestation["httpListener"]["socketInode"] == candidate["listener"]["socketInode"], "The initial database/process snapshot differs.")
    need(attestation["dashboard"]["assetCount"] == 57 and attestation["dashboard"]["allAssetsMatched"] is True and
         attestation["dashboard"]["entryReferencesMatched"] == 5, "The fresh embedded dashboard inspection is incomplete.")
    return root


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


def fresh_sql_environment(environment, root):
    """Disable libpq's default credential-file lookup for this loaded instance."""
    socket = root / "postgres/socket"
    return {**environment, "PGPASSFILE": str(socket / ".goby-inspection-no-password"),
            "PGSERVICEFILE": str(socket / ".goby-inspection-no-service"), "PGCONNECT_TIMEOUT": "3"}


class CandidateIO:
    """Bound IO only. Construction never authenticates or changes the candidate."""
    def __init__(self, value, input_pin, source_pin, *, fresh=False):
        self.value, self.input_pin, self.source_pin = value, input_pin, source_pin
        self.fresh = fresh or (value.get("kind") == "audited-candidate-seed-input" and type(value.get("version")) is int and value["version"] == 2)
        self.root = fresh_candidate_root(value["candidateManifest"]) if self.fresh else C
        self.output, self.private = Path(value["output"]), Path(value["output"]) / "private"
        self.budgets, self.requests = value["budgets"], {"normal": 0, "cleanup": 0}
        self.started, self.byte_count, self.last_response = time.monotonic(), 0, None
        self.created, self.request_states = False, []
        need(self.output.parent == (E3 if self.fresh else R) and str(self.output) == value["output"] and
             re.fullmatch(r"[a-z0-9][a-z0-9-]{1,79}", self.output.name), "Fresh output must be directly within its declared evidence scope.")
        if self.fresh:
            for pin, suffix in ((input_pin, ".json"), (source_pin, ".py"), (value["runtimeInspection"], ".json"),
                                (value["runtimeHelper"], ".py"), (value["inspectionHelper"], ".py")):
                scoped_descriptor(pin, E3, suffix)
            need(value["runtimeInspection"]["path"] == str(INITIAL_INSPECTION / "report.json") and value["inspectionHelper"]["path"] == str(INSPECTION_HELPER),
                 "Fresh IO requires the reviewed SQL-corrected initial inspection.")
        else:
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
        self.attestation = attestation
        if self.fresh:
            scoped_descriptor(attestation["input"], E3, ".json")
            scoped_descriptor(self.candidate["input"], E3, ".json")
            self.inspection_input = descriptor(attestation["input"])
            self.provision_input = descriptor(self.candidate["input"])
            need(validate_fresh_authority(self.value, self.candidate, attestation, self.inspection_input, self.provision_input) == self.root,
                 "Fresh candidate authority changed.")
            read_checked(self.value["inspectionHelper"]["path"], self.value["inspectionHelper"]["sha256"])
        else:
            need(attestation["kind"] == "audited-candidate-runtime-inspection" and attestation["status"] == "ready_pending_live_acceptance" and
                 attestation["failure"] is None and attestation["manifest"] == MANIFEST and attestation["database"]["sourceSchemaVersion"] == 28 and
                 attestation["database"]["sourceUsers"] == 0 and attestation["database"]["recoveryTargetEmpty"] is True and
                 attestation["processes"]["server"] == self.candidate["serverIdentity"] and
                 attestation["processes"]["postgres"] == self.candidate["postgresIdentity"] and
                 attestation["httpListener"]["socketInode"] == self.candidate["listener"]["socketInode"], "Runtime inspection binding differs.")
        helper = self.value["runtimeHelper"]
        need(Path(helper["path"]).is_relative_to(E3 if self.fresh else R) and Path(helper["path"]).suffix == ".py", "Runtime helper escaped the approved scope.")
        module = types.ModuleType("frozen_candidate_provision")
        module.__file__ = helper["path"]
        exec(compile(read_checked(helper["path"], helper["sha256"]), helper["path"], "exec"), module.__dict__)
        self.runtime_module = module
        self.provision = module.Provision(self.provision_input if self.fresh else {"runId": self.candidate["runId"], "ports": self.candidate["ports"]}, self.input_pin, helper)
        self.provision.private = self.private
        self.provision.argv = {"server": [str(self.root / "install/goby")], "postgres": [str(module.PG / "postgres"), "-D", str(self.root / "postgres/data"), "-c", "config_file=" + str(self.root / "postgres/server.conf")]}
        self.provision.pg_identity, self.provision.pg_version = self.candidate["postgresIdentity"], self.candidate["postgresVersionNum"]
        need(self.provision.root == self.root and self.candidate["dataDirectory"] == str(self.root / "data") and
             self.candidate["status"] == "running_awaiting_live_acceptance" and self.candidate["bootstrapExecuted"] is False, "Unexpected candidate layout.")
        if self.fresh:
            self.configure_fresh_sql_environment()
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
        binding = {"candidate": self.value["candidateManifest"], "runtimeInspection": self.value["runtimeInspection"], "runtimeHelper": helper, "input": self.input_pin, "source": self.source_pin}
        if self.fresh:
            binding.update(version=2, provisionInput=self.candidate["input"], inspectionInput=attestation["input"],
                           inspectionHelper=self.value["inspectionHelper"], sourceEvidence=self.candidate["productEvidence"])
        write_json_once(self.private / "binding.json", binding)
        self.pin()
        return self

    def configure_fresh_sql_environment(self):
        """Wrap only this fresh instance's existing SQL command entry point."""
        need(self.fresh, "Legacy SQL environments must remain unchanged.")
        self.sql_environment = fresh_sql_environment(self.runtime_module.ENV, self.root)
        self.runtime_module.ENV = dict(self.sql_environment)
        self.sql_socket_identity = None
        self.check_fresh_sql_environment()
        original = self.provision.psql

        def checked_psql(label, sql, database="postgres"):
            self.check_fresh_sql_environment()
            try:
                return original(label, sql, database)
            finally:
                self.check_fresh_sql_environment()

        self.provision.psql = checked_psql

    def check_fresh_sql_environment(self):
        uid = self.candidate["postgresIdentity"]["uid"]
        socket = self.root / "postgres/socket"
        info = safe_path(socket, (0, uid), True)
        identity = (info.st_dev, info.st_ino, info.st_uid, info.st_gid, info.st_mode)
        need(info.st_uid == uid and stat.S_IMODE(info.st_mode) == 0o700 and
             (self.sql_socket_identity is None or identity == self.sql_socket_identity) and
             self.runtime_module.ENV == self.sql_environment and
             all(not os.path.lexists(self.sql_environment[key]) for key in ("PGPASSFILE", "PGSERVICEFILE")),
             "The fresh SQL socket authority or absent credential-file policy changed.")
        self.sql_socket_identity = identity

    def pin(self):
        if self.fresh:
            self.check_fresh_sql_environment()
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


def validate_media_manifest(manifest, fresh=False):
    """Bind reused fixture bytes to a new metadata receipt without reusing actors."""
    need(manifest["marker"] == "goby-client-media-m3e-v1" and manifest["files"] == FILES,
         "The approved fourteen-file media closure differs.")
    if fresh:
        need(set(manifest) == {"kind", "version", "sourceRoot", "marker", "files", "fileBytes", "totalBytes"} and
             manifest["kind"] == "audited-candidate-media-fixture-manifest" and type(manifest["version"]) is int and manifest["version"] == 2 and
             manifest["sourceRoot"] == str(MEDIA) and isinstance(manifest["fileBytes"], dict) and set(manifest["fileBytes"]) == set(FILES) and
             all(type(size) is int and size > 0 for size in manifest["fileBytes"].values()) and
             type(manifest["totalBytes"]) is int and sum(manifest["fileBytes"].values()) == manifest["totalBytes"] == MEDIA_TOTAL_BYTES,
             "The fresh media byte inventory or source scope differs.")
    return manifest


def validate_fixture_hardlinks(identities):
    """Allow only the complete declared four-path movie inode group."""
    need(set(identities) == set(FILES), "Fixture identity membership differs.")
    movies = [identities[name] for name, checksum in FILES.items() if checksum == MOVIE_SHA]
    need(len(movies) == 4 and len({tuple(row[:2]) for row in movies}) == 1 and
         all(row[5] == 4 and row[6] == 48786888 for row in movies) and
         all(identities[name][5] == 1 for name, checksum in FILES.items() if checksum != MOVIE_SHA),
         "The declared source hardlink group is incomplete or changed.")


STORED_Q_POLICY = {**PLAYBACK, "EnableAllFolders": False, "EnabledFolders": [], "IsAdministrator": False, "IsDisabled": False}
SOURCE_STATE_QUERY = """BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object(
 'schema',(SELECT max(version) FROM schema_migrations),
 'users',(SELECT json_agg(json_build_object('id',id,'name',name,'admin',is_administrator,'disabled',is_disabled,'policy',policy) ORDER BY id) FROM users),
 'libraries',(SELECT json_agg(json_build_object('id',id,'name',name,'collectionType',collection_type) ORDER BY id) FROM libraries),
 'roots',(SELECT json_agg(json_build_object('id',id,'libraryId',library_id,'path',path) ORDER BY id) FROM library_roots),
 'jobs',(SELECT json_agg(json_build_object('id',id,'libraryId',library_id,'status',status,'error',error,'forceProbe',force_probe) ORDER BY id) FROM scan_jobs),
 'items',(SELECT json_agg(json_build_object('id',id,'parentId',parent_id,'name',name,'type',type,'path',path) ORDER BY id) FROM items),
 'sessions',(SELECT json_agg(json_build_object('userId',user_id,'kind',kind,'tokenSha256',encode(token_hash,'hex'),'revoked',revoked_at IS NOT NULL) ORDER BY id) FROM sessions),
 'historyRows',(SELECT count(*) FROM user_item_data));
COMMIT;"""


class StateCheckError(ValueError):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


def validate_source_state(snapshot, details, libraries, actors, resources, cleanup, catalog):
    """Compare stored rows with captured projections and exact helper revocations."""
    def check(value, code):
        if not value:
            raise StateCheckError(code)
    try:
        check(snapshot["schema"] == 28 and snapshot["historyRows"] == 0 and len(snapshot["users"]) == 8 and
              len(resources["users"]) == 8 and {row["id"] for row in snapshot["users"]} == set(resources["users"]) == {row["id"] for row in actors.values()},
              "source_schema_or_user_membership_differs")
        by_id = {row["id"]: row for row in snapshot["users"]}
        for role, actor in actors.items():
            row = by_id[actor["id"]]
            check(row["name"] == actor["username"] and row["admin"] is (role == "admin") and row["disabled"] is False and isinstance(row["policy"], dict), "stored_account_identity_differs")
            if role == "control-q":
                check(set(row["policy"]) == set(STORED_Q_POLICY) and row["policy"]["EnabledFolders"] == [] and
                      all(row["policy"][key] is expected for key, expected in STORED_Q_POLICY.items() if key != "EnabledFolders"), "stored_q_policy_differs")
            else:
                check(row["policy"] == {}, "stored_default_policy_differs")
        library_ids = {row["Id"] for row in libraries.values()}
        check(len(libraries) == len(snapshot["libraries"]) == len(snapshot["roots"]) == len(library_ids) == 3 and
              len({row["id"] for row in snapshot["roots"]}) == 3, "library_root_count_differs")
        for library in libraries.values():
            check({"id": library["Id"], "name": library["Name"], "collectionType": library["CollectionType"]} in snapshot["libraries"] and
                  [row["path"] for row in snapshot["roots"] if row["libraryId"] == library["Id"]] == library["Paths"], "library_root_mapping_differs")
        check(len(snapshot["jobs"]) == len(resources["jobs"]) == 3 and
              {(row["id"], row["libraryId"]) for row in snapshot["jobs"]} == {(row["id"], row["libraryId"]) for row in resources["jobs"]} and
              all(row["status"] == "Completed" and row["error"] == "" and row["forceProbe"] is False for row in snapshot["jobs"]), "scan_completion_differs")
        detail_ids = {row["Id"] for row in details}
        check(len(details) == len(detail_ids) == 10 and not detail_ids & library_ids and len(snapshot["items"]) == 13 and
              {row["id"] for row in snapshot["items"]} == detail_ids | library_ids, "stored_item_membership_differs")
        stored = {row["id"]: row for row in snapshot["items"]}
        for library in libraries.values():
            check(stored[library["Id"]] == {"id": library["Id"], "name": library["Name"], "type": "CollectionFolder", "parentId": None, "path": ""}, "collection_folder_shape_differs")
        for dto in details:
            row = stored[dto["Id"]]
            if dto["Type"] == "MusicAlbum":
                check(dto["Id"] == catalog["album"]["id"] and "Path" not in dto and row["path"] == "" and
                      row["parentId"] == dto["ParentId"] == libraries["Music"]["Id"], "root_album_projection_differs")
                observed_path = ""
            else:
                observed_path = dto["Path"]
            check((row["name"], row["type"], row["path"], row["parentId"]) == (dto["Name"], dto["Type"], observed_path, dto["ParentId"]), "catalog_item_projection_differs")
        check(len(cleanup) == 2 and {row["kind"] for row in cleanup} == {"native", "emby"}, "helper_cleanup_kind_binding_differs")
        expected_sessions = {("admin" if row["kind"] == "native" else "emby", row["tokenSha256"]) for row in cleanup}
        check(len(snapshot["sessions"]) == 2 and {row["kind"] for row in snapshot["sessions"]} == {"admin", "emby"} and
              all(row["revoked"] is True and row["userId"] == actors["admin"]["id"] for row in snapshot["sessions"]) and
              {(row["kind"], row["tokenSha256"]) for row in snapshot["sessions"]} == expected_sessions, "helper_token_revocation_differs")
        return {"users": 8, "libraries": 3, "roots": 3, "completedScans": 3, "storedItems": 13, "publicItems": 10, "collectionRoots": 3, "revokedHelperSessions": 2, "historyRows": 0}
    except StateCheckError:
        raise
    except (KeyError, TypeError, ValueError, IndexError):
        raise StateCheckError("source_snapshot_shape_invalid") from None


class Seed:
    def __init__(self, io):
        self.io, self.stage, self.sessions = io, "preflight", {}
        self.resources = {"users": [], "libraries": [], "jobs": [], "copiedFiles": []}
        self.cleanup = []

    def api(self, label, method, route, body=None, auth=None, expected=(200,)):
        return self.io.request(label, method, route, body, auth, expected)["body"]

    def copy_media(self):
        if self.io.fresh:
            scoped_descriptor(self.io.value["mediaManifest"], E3, ".json")
        else:
            need(self.io.value["mediaManifest"]["path"] == str(MEDIA / "manifest.json"), "Original media manifest path differs.")
        manifest = validate_media_manifest(descriptor(self.io.value["mediaManifest"]), self.io.fresh)
        account = pwd.getpwnam("goby")
        destination = self.io.root / "data/media"
        safe_path(destination, (0, account.pw_uid), True)
        need(destination.stat().st_uid == account.pw_uid and not list(destination.iterdir()), "Candidate media root must be empty.")
        if self.io.fresh:
            disk = os.statvfs(destination)
            need(disk.f_bavail * disk.f_frsize >= MEDIA_TOTAL_BYTES + MEDIA_MIN_FREE_BYTES, "The media copy would consume the preserved free-space floor.")
        entries = list(MEDIA.rglob("*"))
        need(len(entries) <= 30 and all(not path.is_symlink() for path in entries) and
             {str(path.relative_to(MEDIA)) for path in entries if not path.is_dir()} == set(FILES) | {"manifest.json"}, "Original media membership differs.")
        source_identities = {}
        if self.io.fresh:
            source_identities = {name: file_identity(safe_path(MEDIA / name)) for name in FILES}
            validate_fixture_hardlinks(source_identities)
            need(all(source_identities[name][6] == size for name, size in manifest["fileBytes"].items()), "The fixture inventory changed before copying.")
            write_json_once(self.io.private / "source-fixture-identities.json", source_identities)
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
            if self.io.fresh:
                disk = os.statvfs(destination)
                need(file_identity(before) == source_identities[name] and before.st_size == manifest["fileBytes"][name] and
                     disk.f_bavail * disk.f_frsize >= MEDIA_TOTAL_BYTES - total + MEDIA_MIN_FREE_BYTES, "Fresh fixture size or remaining disk budget differs.")
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
        if self.io.fresh:
            need(total == MEDIA_TOTAL_BYTES and len(self.resources["copiedFiles"]) == len(FILES) and
                 all(file_identity(safe_path(MEDIA / name)) == identity for name, identity in source_identities.items()), "The fresh media copy is incomplete or its source changed.")

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
                    {"Name": name, "CollectionType": collection, "Paths": [str(self.io.root / "data/media" / directory)], "Scan": False}, auth, (201,))["Library"]
                library_id = item_id(library["Id"])
                self.resources["libraries"].append(library_id)
                need(library["Name"] == name and library["CollectionType"] == collection and library["Paths"] == [str(self.io.root / "data/media" / directory)], "Native library mapping differs.")
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
        effects = {}
        if self.io.fresh:
            self.stage = "stored_effects_and_helper_revocation"
            self.io.pin()
            need(provision_input == self.io.provision_input and self.io.provision.cluster(pg) == self.io.candidate["clusterSystemIdentifier"], "The provision or database authority changed.")
            snapshot = parse(self.io.provision.psql("seed-final-stored-state", SOURCE_STATE_QUERY, self.io.candidate["database"]))
            snapshot_pin = write_json_once(self.io.private / "source-state-after-cleanup.json", snapshot)
            counts = validate_source_state(snapshot, self.details, self.libraries,
                {"admin": self.admin, **self.actors, "control-q": self.control}, self.resources, self.cleanup, catalog)
            target = parse(self.io.provision.psql("seed-final-recovery-empty", "BEGIN READ ONLY; SELECT json_build_object('relations',(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace),'functions',(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace),'types',(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace),'schemas',(SELECT json_agg(nspname ORDER BY nspname) FROM pg_namespace WHERE nspname!~'^pg_' AND nspname<>'information_schema')); COMMIT;", self.io.candidate["recoveryDatabase"]))
            need(target == {"relations": 0, "functions": 0, "types": 0, "schemas": ["public"]}, "The isolated recovery target is no longer empty.")
            account = pwd.getpwnam("goby")
            for pin in self.resources["copiedFiles"]:
                copied = descriptor(pin)
                info = safe_path(Path(copied["path"]), (0, account.pw_uid))
                need(list(file_identity(info)) == copied["identity"], "A copied fixture changed after scanning.")
            need(self.io.provision.cluster(pg) == self.io.candidate["clusterSystemIdentifier"], "The final cluster identity changed.")
            effects = {"sourceState": snapshot_pin, "effectValidation": {**counts, "recoveryTargetEmpty": True, "copiedFilesUnchanged": True}}
        read_checked(self.io.candidate["binary"]["path"], self.io.candidate["binary"]["sha256"])
        candidate_process = self.io.pin()
        self.io.deadline(cleanup=True)
        self.stage = "complete"
        result = {"kind": "audited-candidate-seed-manifest", "version": 2 if self.io.fresh else 1, "status": "seeded_pending_live_acceptance", "input": self.io.input_pin,
            "helper": self.io.source_pin, "candidateManifest": self.io.value["candidateManifest"], "runtimeInspection": self.io.value["runtimeInspection"], "serverId": self.server_id, "admin": self.admin, "actors": self.actors, "controlQ": self.control,
            "catalog": catalog, "catalogFile": self.catalog_pin, "actualCatalogDtos": self.dto_pin, "libraries": self.libraries, "roots": {directory: library["Paths"][0] for directory, library in self.libraries.items()},
            "source": {"manifestSha256": provision_input["sourceManifest"]["sha256"], "binarySha256": self.io.candidate["binary"]["sha256"], "schema": 28},
            "processes": {"candidate": candidate_process}, "resources": self.resources, "cleanup": self.cleanup, "requests": self.io.requests, "budgets": BUDGETS,
            "elapsedMilliseconds": round((time.monotonic() - self.io.started) * 1000), "playbackRequests": 0, "clientAcceptance": False, "candidateAdmissionComplete": False}
        if self.io.fresh:
            result.update(sourceEvidence=self.io.candidate["productEvidence"], provisionInput=self.io.candidate["input"],
                          provisionHelper=self.io.value["runtimeHelper"], inspectionInput=self.io.attestation["input"],
                          inspectionHelper=self.io.value["inspectionHelper"], mediaManifest=self.io.value["mediaManifest"], **effects)
        return write_json_once(self.io.private / "manifest.json", result)

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
        self.details = details
        return map_catalog(details, self.libraries, root=self.io.root, observed_tv=self.io.fresh)


def mapped_subtitle(row, codec, root=C):
    path = "Movies/M3e Client Movie.en." + codec
    need(codec in ("srt", "vtt") and row["Codec"] == codec and row["IsExternal"] is True and row["Language"] in ("en", "eng") and
         type(row["Index"]) is int and row["Index"] >= 0 and row["Path"] == str(root / "data/media" / path), "Actual external subtitle fields differ.")
    return {"index": row["Index"], "codec": codec, "language": row["Language"], "external": True, "sha256": FILES[path]}


def map_catalog(details, libraries, root=C, *, observed_tv=False):
    """Map parent edges; fresh embedded catalogs must also expose the TV metadata."""
    def only(kind, path=None):
        rows = [row for row in details if row["Type"] == kind and (path is None or row.get("Path") == str(root / "data/media" / path))]
        need(len(rows) == 1, "Actual catalog mapping is absent or ambiguous.")
        return rows[0]

    def mapped(row, kind, name=None):
        need(row["Type"] == kind and (name is None or row["Name"] == name), "Actual item title/type differs.")
        result = {"id": item_id(row["Id"]), "type": kind, "name": row["Name"], "path": row["Path"]}
        if kind in ("Movie", "Episode", "Audio"):
            relative = str(Path(row["Path"]).relative_to(root / "data/media"))
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
        result["subtitles"].append(mapped_subtitle(matching[0], codec, root=root))
    need(len({row["index"] for row in result["subtitles"]}) == 2, "Subtitle indexes overlap.")
    album = only("MusicAlbum")
    need(album["Name"] in ("Music", "M3e Synthetic Album"), "Album name differs.")
    if observed_tv:
        need("Path" not in album and album["ParentId"] == libraries["Music"]["Id"], "The root album projection differs.")
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
    seasons = {}
    for number in (1, 2):
        rows = [row for row in details if row["Type"] == "Season" and row["ParentId"] == series["Id"] and row["IndexNumber"] == number]
        need(len(rows) == 1, "Actual season parent edge differs.")
        row = rows[0]
        seasons[number] = row
        if observed_tv:
            need(row["SeriesId"] == series["Id"] and row["SeriesName"] == series["Name"], "The observed season relationship metadata differs.")
        result["seasons"].append({"id": item_id(row["Id"]), "type": "Season", "indexNumber": number, "seriesId": series["Id"], "parentId": row["ParentId"]})
    for season, episode in ((1, 1), (1, 2), (2, 1)):
        path = "TV/M3e Client Series/Season %02d/M3e Client Series S%02dE%02d.mp4" % (season, season, episode)
        row = only("Episode", path)
        need(row["ParentId"] == result["seasons"][season - 1]["id"] and row["ParentIndexNumber"] == season and row["IndexNumber"] == episode, "Actual episode parent edge differs.")
        if observed_tv:
            need(row["SeriesId"] == series["Id"] and row["SeriesName"] == series["Name"] and
                 row["SeasonId"] == seasons[season]["Id"] and row["SeasonName"] == seasons[season]["Name"], "The observed episode relationship metadata differs.")
        projected = mapped(row, "Episode", "Episode %d-%d" % (season, episode))
        need(projected["runtimeTicks"] >= 1200000000, "Episode duration differs.")
        result["episodes"].append({**projected, "seriesId": series["Id"], "parentId": row["ParentId"], "parentIndexNumber": season, "indexNumber": episode})
    for field, directory in (("musicLibrary", "Music"), ("tvLibrary", "TV")):
        result[field] = {"id": item_id(libraries[directory]["Id"]), "name": libraries[directory]["Name"]}
    return result


def private_failure_receipt(failure, error):
    """Keep a bounded diagnostic only in the private receipt copy."""
    return {**failure, "errorMessage": str(error)[:4096]}


def public_failure_summary(failure):
    return {key: failure[key] for key in ("status", "stage", "errorType", "receipt", "candidateAdmissionComplete")}


def main():
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode,
         "Use authorized root SSH with /usr/bin/python3 -I -B.")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("input", "input-sha256", "source-sha256"):
        parser.add_argument("--" + name, required=True)
    args = parser.parse_args()
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    value = descriptor(input_pin)
    validate_seed_input(value, input_pin, source_pin)
    read_checked(source_pin["path"], source_pin["sha256"])
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
                failure["receipt"] = write_json_once(job.io.private / "failure.json", private_failure_receipt(failure, error))
        except Exception as receipt_error:
            failure["receiptErrorType"] = type(receipt_error).__name__
        print(json.dumps(public_failure_summary(failure) if job.io.fresh else failure))
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
