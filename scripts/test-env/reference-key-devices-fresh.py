#!/usr/bin/env python3
"""Capture key-device contracts in the attested disposable reference instance.

This recorder never reuses the original reference's authority or transport.
Only the wholly owned fresh program-data tree may contain an already present
server device that this study mutates. The original recorder remains pinned
and unchanged. Run through authorized root SSH after the preparation operator
has written its immutable READY manifest. Leave the fresh service alive for
the operator's separate final teardown. GOBY_FRESH_KEY_DEVICES_RUN accepts only
canonical 1 or 2, defaulting to 1. Run 2 uses separate fixed paths and retains
every file from the first attempt, which failed before process startup or HTTP.
GOBY_FRESH_KEY_DEVICES_CAPTURE_RUN independently accepts only 1 or 2. Capture
attempt 2 retains the first constructor failure and uses a new capture root.
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
from types import SimpleNamespace

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("fresh_key_device_reference", Path(__file__).with_name("reference-key-devices.py"))
key = importlib.util.module_from_spec(spec)
spec.loader.exec_module(key)
device, base = key.device, key.base


def selected_run(value: str) -> int:
    base.require(value in {"1", "2"}, "GOBY_FRESH_KEY_DEVICES_RUN must be canonical 1 or 2")
    return int(value)


def selected_capture_run(value: str) -> int:
    base.require(value in {"1", "2"}, "GOBY_FRESH_KEY_DEVICES_CAPTURE_RUN must be canonical 1 or 2")
    return int(value)


FRESH_RUN = selected_run(os.environ.get("GOBY_FRESH_KEY_DEVICES_RUN", "1"))
CAPTURE_RUN = selected_capture_run(os.environ.get("GOBY_FRESH_KEY_DEVICES_CAPTURE_RUN", "1"))
RUN_LABEL = str(FRESH_RUN).zfill(2)
CLIENT_LABEL = str(FRESH_RUN + 3).zfill(2)
FRESH_UNIT = "goby-emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL + ".service"
FRESH_DATA = Path("/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL)
EVIDENCE = Path("/opt/goby-test/exec-scratch/emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL)
FRESH_MARKER = "goby-emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL + "-owned-v1"
PREVIOUS_FAILED_EVIDENCE = Path("/opt/goby-test/exec-scratch/emby-key-devices-fresh-m5e-20260910-01")
PREVIOUS_FAILED_UNIT = "goby-emby-key-devices-fresh-m5e-20260910-01.service"
PREVIOUS_FAILED_DATA = Path("/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-01")
MANIFEST = EVIDENCE / "private/manifest.json"
FRESH_PORT = 18098
FRESH_SERVER_EXECUTABLE = "/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer"
FRESH_DESCRIPTION = "Goby owned fresh Emby key-device fixture M5e 20260910 " + RUN_LABEL
FRESH_STATIC_PROPERTIES = ("Id", "Description", "PrivateNetwork", "PrivateTmp", "NoNewPrivileges", "ProtectSystem",
                           "ProtectHome", "WorkingDirectory", "ExecStart", "ControlGroup", "FragmentPath", "DropInPaths",
                           "ReadWritePaths", "ReadOnlyPaths", "MemoryMax", "CPUQuotaPerSecUSec", "TasksMax", "User",
                           "KillMode", "TimeoutStopUSec", "LimitNOFILE", "UMask")
READ_ONLY_PATHS = {"/dev/shm", "/dev/shm/goby-emby-reference", "/opt/goby-test/emby-reference-data"}
ORIGINAL_CORPUS_RECORDS = 1538
PINNED_KEY_RECORDER_SHA256 = "678ae21ca471b3ea77c73484dfdf0d9058e437494954745cd0cf899894e196d9"
ORIGINAL_SERVICES = {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3494032}
ORIGINAL_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
SETUP_PREFIX = "key-devices-fresh-setup-m5e-" if FRESH_RUN == 1 else "key-devices-fresh-setup-m5e-2-"
ACTIVE_NETWORK_NAMESPACE: str | None = None
FIRST_CAPTURE_ROOT = EVIDENCE / "runtime/key-devices-capture"
FIRST_CAPTURE_PREFIX = "key-devices-fresh-m5e-" if FRESH_RUN == 1 else "key-devices-fresh-m5e-2-"
FIRST_CAPTURE_MARKER = "goby-reference-key-devices-fresh-m5e-" + ("" if FRESH_RUN == 1 else "2-") + "capture-owned-v1"
PREVIOUS_CAPTURE_FAILURE_ROOT = Path("/opt/goby-test/exec-scratch/devices-m5e-runner/fresh-capture-failure-1-private")
PREVIOUS_CAPTURE_SOURCE = PREVIOUS_CAPTURE_FAILURE_ROOT / "recorder-source.py"
PREVIOUS_CAPTURE_CONSOLE = PREVIOUS_CAPTURE_FAILURE_ROOT / "constructor-failure-console.log"
PREVIOUS_CAPTURE_SOURCE_SHA256 = "624fef764e8eb1eaffbd723db5f83e5c0f5ed4a323ac7ea1cf51e4ca48aa0031"
PREVIOUS_CAPTURE_CONSOLE_SHA256 = "ec1a432f16907b453ba98450c4ac664e60cedf0daa3fb9b6b26ec720ced9b055"

base.__file__ = __file__
base.ROOT = FIRST_CAPTURE_ROOT if CAPTURE_RUN == 1 else EVIDENCE / "runtime/key-devices-capture-2"
base.PRIVATE, base.RAW, base.EXPORT = base.ROOT / "private", base.ROOT / "private/raw", base.ROOT / "export"
base.PREFIX = FIRST_CAPTURE_PREFIX if CAPTURE_RUN == 1 else FIRST_CAPTURE_PREFIX + "attempt-2-"
base.MARKER = FIRST_CAPTURE_MARKER if CAPTURE_RUN == 1 else FIRST_CAPTURE_MARKER.replace("-owned-v1", "-attempt-2-owned-v1")
key.APP_PREFIX = "Goby Fresh Key Devices M5e 20260910 " + CLIENT_LABEL + " "
key.KEY_APPS = {"alpha": key.APP_PREFIX + "Alpha", "sibling": key.APP_PREFIX + "Sibling"}
device.DEVICE_PREFIX = "goby-key-devices-fresh-m5e-20260910-" + CLIENT_LABEL + "-"
device.CLIENT_PREFIX = "Goby Fresh Key Devices M5e 20260910 " + CLIENT_LABEL + " "
device.CUSTOM_PREFIX = "Goby Fresh Key Devices M5e " + CLIENT_LABEL + " Owned "


def process_identity(pid: int) -> dict:
    folder = Path("/proc") / str(pid)
    base.require(pid > 1 and folder.stat().st_uid == 0, "Reference process is not root-owned")
    fields = (folder / "stat").read_text().rsplit(")", 1)[1].split()
    return {"pid": pid, "uid": folder.stat().st_uid, "startTicks": fields[19], "networkNamespace": os.readlink(folder / "ns/net"),
            "cmdline": [value.decode("utf-8") for value in (folder / "cmdline").read_bytes().split(b"\0") if value],
            "exe": os.readlink(folder / "exe"), "cgroup": (folder / "cgroup").read_text()}


def service_properties(unit: str, fields: tuple[str, ...]) -> dict:
    arguments = ["systemctl", "show", unit]
    for field in fields:
        arguments.extend(("-p", field))
    return dict(line.split("=", 1) for line in base.subprocess.check_output(arguments, timeout=5, text=True).splitlines())


def verify_original_preserved_path(path: Path, group: str) -> None:
    base.require(group in {"records", "media", "privateFiles"}, "Unknown original preservation group")
    info = path.lstat()
    base.require(path.resolve(strict=True) == path and stat.S_ISREG(info.st_mode) and
                 (group == "media" or info.st_nlink == 1),
                 "An original preserved path is not canonical and regular or violates its evidence link boundary")
    # Known read-only media fixtures deliberately share inodes. Every path's
    # bytes are still compared with its attested hash in Recorder.snapshot.


def previous_empty_capture_snapshot() -> dict:
    if CAPTURE_RUN == 1:
        return {}
    base.require(FRESH_RUN == 2, "Only the proven constructor failure in fresh instance 2 permits capture attempt 2")
    marker = FIRST_CAPTURE_ROOT / ".goby-managed"
    expected_directories = {FIRST_CAPTURE_ROOT, FIRST_CAPTURE_ROOT / "private", FIRST_CAPTURE_ROOT / "private/raw",
                            FIRST_CAPTURE_ROOT / "export"}
    for folder in expected_directories | {PREVIOUS_CAPTURE_FAILURE_ROOT}:
        info = folder.lstat()
        base.require(folder.resolve(strict=True) == folder and stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                     stat.S_IMODE(info.st_mode) == 0o700, "Previous empty capture or private failure directory ownership differs")
    observed = set(FIRST_CAPTURE_ROOT.rglob("*"))
    base.require(observed == (expected_directories - {FIRST_CAPTURE_ROOT}) | {marker},
                 "Previous capture was not empty; refusing to adopt or overwrite a capture that reached HTTP")
    base.private_file(marker)
    base.require(marker.read_text().strip() == FIRST_CAPTURE_MARKER, "Previous empty capture marker differs")
    saved_files = {PREVIOUS_CAPTURE_SOURCE, PREVIOUS_CAPTURE_CONSOLE}
    base.require(set(PREVIOUS_CAPTURE_FAILURE_ROOT.rglob("*")) == saved_files,
                 "Previous constructor failure evidence membership changed")
    expected_hashes = {PREVIOUS_CAPTURE_SOURCE: PREVIOUS_CAPTURE_SOURCE_SHA256,
                       PREVIOUS_CAPTURE_CONSOLE: PREVIOUS_CAPTURE_CONSOLE_SHA256}
    result = {str(marker): base.digest(marker)}
    for path, expected_hash in expected_hashes.items():
        base.private_file(path)
        base.require(path.stat().st_nlink == 1 and path.stat().st_size <= 1024 * 1024,
                     "Previous constructor failure evidence exceeds its private file boundary")
        observed_hash = base.digest(path)
        base.require(observed_hash == expected_hash, "Previous constructor source or console bytes changed")
        result[str(path)] = observed_hash
    return result


def read_manifest() -> dict:
    base.private_file(MANIFEST)
    base.require(MANIFEST.stat().st_size <= 4 * 1024 * 1024, "Fresh authority manifest exceeds its bound")
    value = json.loads(MANIFEST.read_text())
    base.require(isinstance(value, dict) and value.get("schemaVersion") == 1 and value.get("state") == "READY" and
                 value.get("referenceRun") == FRESH_RUN and
                 value.get("unit") == FRESH_UNIT and value.get("port") == FRESH_PORT and
                 value.get("programData") == str(FRESH_DATA) and isinstance(value.get("serverId"), str) and
                 value["serverId"] and value["serverId"] != ORIGINAL_SERVER_ID and
                 value.get("bootstrapCredentialsRevoked") is True and value.get("viewerEmptyPasswordLoginVerified") is True and
                 value.get("bootstrapCredentialsInvalidity") == {"admin": 401, "viewer": 401} and
                 value.get("oldPreservationVerified") is True and value.get("allFreshProgramDataOwned") is True and
                 value.get("evidenceRoot") == str(EVIDENCE) and value.get("setupRawRoot") == str(EVIDENCE / "private/raw") and
                 value.get("setupExportRoot") == str(EVIDENCE / "export") and value.get("setupPrefix") == SETUP_PREFIX and
                 value.get("adminCredentialsFile") == str(EVIDENCE / "private/admin-credentials.env") and
                 value.get("viewerCredentialsFile") == str(EVIDENCE / "private/viewer-credentials.env"),
                 "Fresh authority manifest does not describe the reviewed isolated instance")
    base.require(isinstance(value.get("pid"), int) and not isinstance(value["pid"], bool) and value["pid"] > 1 and value.get("uid") == 0 and
                 isinstance(value.get("startTicks"), str) and value["startTicks"].isdigit() and
                 isinstance(value.get("networkNamespace"), str) and value["networkNamespace"].startswith("net:["),
                 "Fresh process attestation is incomplete")
    base.require(isinstance(value.get("oldServices"), dict) and set(value["oldServices"]) == set(ORIGINAL_SERVICES),
                 "Both original service identities must be attested")
    base.require(all(isinstance(value["oldServices"][unit], dict) and value["oldServices"][unit].get("unit") == unit and
                 value["oldServices"][unit].get("pid") == pid and isinstance(value["oldServices"][unit].get("networkNamespace"), str)
                 for unit, pid in ORIGINAL_SERVICES.items()) and value["pid"] not in ORIGINAL_SERVICES.values() and
                 value["networkNamespace"] not in {entry["networkNamespace"] for entry in value["oldServices"].values()},
                 "Fresh manifest reuses a protected process or network authority")
    base.require(isinstance(value.get("serviceProperties"), dict) and set(value["serviceProperties"]) == set(FRESH_STATIC_PROPERTIES) and
                 all(isinstance(item, str) for item in value["serviceProperties"].values()),
                 "Fresh static service-property attestation is incomplete")
    base.require(isinstance(value.get("setupRecordCount"), int) and not isinstance(value["setupRecordCount"], bool) and
                 0 < value["setupRecordCount"] <= 128, "Fresh setup record count exceeds its bound")
    for field, bound in (("bootstrapDevices", device.MAX_OLD_DEVICES), ("bootstrapUsers", 2)):
        rows = value.get(field)
        base.require(isinstance(rows, list) and len(rows) <= bound and all(isinstance(row, dict) and
                     isinstance(row.get("Id"), str) and row["Id"] for row in rows) and
                     len({row["Id"] for row in rows}) == len(rows), "Fresh bootstrap membership is unbounded or ambiguous")
    base.require(len(value["bootstrapUsers"]) == 2, "Fresh instance must contain exactly the two owned bootstrap users")
    lookup = value.get("bootstrapServerInfo")
    base.require(isinstance(lookup, dict) and lookup.get("alias") == value["serverId"],
                 "Fresh server lookup alias differs from the attested public server identity")
    validate_previous_failure_provenance(value)
    return value


def validate_previous_failure_provenance(manifest: dict) -> None:
    provenance = manifest.get("previousFailureProvenance")
    if FRESH_RUN == 1:
        base.require(provenance is None, "Fresh run 1 must not adopt another attempt's failure provenance")
        return
    base.require(isinstance(provenance, dict) and provenance.get("referenceRun") == 1 and
                 provenance.get("evidenceRoot") == str(PREVIOUS_FAILED_EVIDENCE) and
                 provenance.get("unit") == PREVIOUS_FAILED_UNIT and provenance.get("programData") == str(PREVIOUS_FAILED_DATA) and
                 provenance.get("outcome") == "initialization-failed-before-http" and provenance.get("setupRecordCount") == 0 and
                 provenance.get("failureRecord") == str(PREVIOUS_FAILED_EVIDENCE / "private/prepare-failure.json"),
                 "Fresh run 2 lacks the exact prior initialization-failure provenance")
    files = provenance.get("files")
    base.require(isinstance(files, dict) and 0 < len(files) <= 512 and provenance.get("fileCount") == len(files) and
                 isinstance(provenance.get("totalBytes"), int) and 0 <= provenance["totalBytes"] < 64 * 1024 * 1024 and
                 all(isinstance(name, str) and Path(name).is_absolute() and PREVIOUS_FAILED_EVIDENCE in Path(name).parents and
                     isinstance(digest, str) and base.re.fullmatch(r"[0-9a-f]{64}", digest) for name, digest in files.items()),
                 "Prior failed evidence membership or hash attestation is malformed")
    operator = provenance.get("operatorSource")
    cleanup = provenance.get("cleanupRecord")
    base.require(isinstance(operator, str) and operator in files and provenance.get("operatorSha256") == files[operator] and
                 isinstance(cleanup, str) and cleanup in files and Path(cleanup).parent == PREVIOUS_FAILED_EVIDENCE / "private" and
                 base.re.fullmatch(r"cleanup-attempt-[1-9][0-9]*\.json", Path(cleanup).name) and
                 provenance["failureRecord"] in files,
                 "Prior failure operator or successful cleanup record is not attested")


def previous_failure_snapshot(manifest: dict) -> dict:
    validate_previous_failure_provenance(manifest)
    if FRESH_RUN == 1:
        return {}
    provenance = manifest["previousFailureProvenance"]
    root_info = PREVIOUS_FAILED_EVIDENCE.lstat()
    base.require(PREVIOUS_FAILED_EVIDENCE.resolve(strict=True) == PREVIOUS_FAILED_EVIDENCE and
                 stat.S_ISDIR(root_info.st_mode) and root_info.st_uid == 0 and stat.S_IMODE(root_info.st_mode) == 0o700 and
                 (PREVIOUS_FAILED_EVIDENCE / ".goby-managed").read_text().strip() ==
                    "goby-emby-key-devices-fresh-m5e-20260910-01-owned-v1",
                 "Prior failed evidence ownership or marker changed")
    actual_paths = set()
    for path in PREVIOUS_FAILED_EVIDENCE.rglob("*"):
        base.require(not path.is_symlink(), "Prior failed evidence contains a symbolic link")
        if path.is_file():
            info = path.lstat()
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1,
                         "Prior failed evidence is not a canonical root-owned regular file")
            actual_paths.add(path)
        base.require(len(actual_paths) <= 512, "Prior failed evidence exceeds its finite membership bound")
    base.require({str(path) for path in actual_paths} == set(provenance["files"]) and
                 sum(path.stat().st_size for path in actual_paths) == provenance["totalBytes"],
                 "Prior failed evidence membership or size changed")
    result = {str(path): base.digest(path) for path in actual_paths}
    base.require(result == provenance["files"], "Prior failed evidence bytes changed")
    forbidden = {PREVIOUS_FAILED_EVIDENCE / "private/manifest.json", PREVIOUS_FAILED_EVIDENCE / "private/service-identity.json"}
    base.require(not (actual_paths & forbidden) and not any(path.parent in
                 {PREVIOUS_FAILED_EVIDENCE / "private/raw", PREVIOUS_FAILED_EVIDENCE / "export"} for path in actual_paths),
                 "Prior initialization failure unexpectedly contains a service identity or HTTP capture")
    failure = json.loads(Path(provenance["failureRecord"]).read_text())
    cleanup = json.loads(Path(provenance["cleanupRecord"]).read_text())
    base.require(isinstance(failure, dict) and isinstance(failure.get("cleanup"), dict) and
                 failure["cleanup"].get("stopped") is True and failure.get("preservationErrors") == [] and
                 isinstance(cleanup, dict) and cleanup.get("dataRemoved") is True and
                 cleanup.get("oldPreservationVerified") is True and cleanup.get("evidenceRetained") is True,
                 "Prior failed attempt lacks proven stop, data cleanup, or old-state preservation")
    base.require(not PREVIOUS_FAILED_DATA.exists() and not PREVIOUS_FAILED_DATA.is_symlink(),
                 "Prior failed program-data directory reappeared")
    prior_unit = service_properties(PREVIOUS_FAILED_UNIT, ("LoadState",))
    base.require(prior_unit.get("LoadState") == "not-found", "Prior failed unit unexpectedly exists")
    return result


def check_original_services(manifest: dict) -> None:
    for unit, pid in ORIGINAL_SERVICES.items():
        expected = manifest["oldServices"][unit]
        base.require(isinstance(expected, dict) and expected.get("unit") == unit and expected.get("pid") == pid,
                     "Original service attestation identifies another process")
        properties = service_properties(unit, ("MainPID", "ActiveState"))
        base.require(properties.get("ActiveState") == "active" and properties.get("MainPID") == str(pid),
                     "An original service stopped or changed its process")
        # Goby's non-root service is protected too; only fresh/reference Emby
        # processes must be root-owned. Its process identity remains exact.
        folder = Path("/proc") / str(pid)
        actual = {"pid": pid, "uid": folder.stat().st_uid, "startTicks": (folder / "stat").read_text().rsplit(")", 1)[1].split()[19],
                  "networkNamespace": os.readlink(folder / "ns/net"),
                  "cmdline": [value.decode("utf-8") for value in (folder / "cmdline").read_bytes().split(b"\0") if value],
                  "exe": os.readlink(folder / "exe"), "cgroup": (folder / "cgroup").read_text()}
        base.require(all(str(actual[name]) == str(expected.get(name)) for name in ("pid", "uid", "startTicks", "networkNamespace")),
                     "An original process identity changed")
        for name in ("cmdline", "exe", "cgroup"):
            if name in expected:
                base.require(actual[name] == expected[name], "An original process executable or process scope changed")
        static = expected.get("serviceProperties", {})
        base.require(isinstance(static, dict) and len(static) <= 32 and all(isinstance(name, str) and
                     name.replace("_", "").isalnum() for name in static), "Original service property attestation is malformed")
        if static:
            base.require(service_properties(unit, tuple(static)) == static, "An original static service property changed")


def check_fresh_service_authority(manifest: dict) -> dict:
    properties = service_properties(FRESH_UNIT, ("MainPID", "ActiveState", *FRESH_STATIC_PROPERTIES))
    base.require(properties.get("ActiveState") == "active" and properties.get("MainPID") == str(manifest["pid"]),
                 "Fresh service is inactive or changed its main process")
    settings = {name: properties.get(name) for name in FRESH_STATIC_PROPERTIES}
    base.require(settings == manifest["serviceProperties"], "Fresh static service properties changed after READY")
    base.require(settings["Id"] == FRESH_UNIT and settings["Description"] == FRESH_DESCRIPTION and
                 settings["WorkingDirectory"] == str(EVIDENCE / "runtime") and str(EVIDENCE / "runtime/launch.sh") in settings["ExecStart"] and
                 settings["MemoryMax"] == "536870912" and settings["PrivateNetwork"] == "yes" and
                 settings["PrivateTmp"] == "yes" and settings["NoNewPrivileges"] == "yes" and
                 settings["ProtectSystem"] == "strict" and settings["ProtectHome"] == "yes" and
                 settings["KillMode"] == "control-group" and settings["TasksMax"] == "256" and
                 settings["LimitNOFILE"] == "65536" and settings["UMask"] == "0077",
                 "Fresh service sandbox does not enforce the reviewed ownership boundary")
    writable = {str(FRESH_DATA), str(EVIDENCE / "runtime")}
    base.require(set(settings["ReadOnlyPaths"].split()) == READ_ONLY_PATHS and
                 set(settings["ReadWritePaths"].split()) == writable,
                 "Fresh service read-only or writable paths differ from the reviewed sandbox")
    actual = process_identity(manifest["pid"])
    base.require(all(actual[field] == manifest[field] for field in ("pid", "uid", "startTicks", "networkNamespace")) and
                 all(actual[field] == manifest.get(field) for field in ("cmdline", "exe", "cgroup")) and
                 actual["exe"] == FRESH_SERVER_EXECUTABLE, "Fresh executable or process identity changed")
    arguments = actual["cmdline"]
    base.require(arguments.count("-programdata") == 1 and arguments.index("-programdata") + 1 < len(arguments) and
                 arguments[arguments.index("-programdata") + 1] == str(FRESH_DATA),
                 "Fresh process program-data argument differs from its owned tree")
    mount_text = (Path("/proc") / str(actual["pid"]) / "mountinfo").read_text()
    base.require(len(mount_text.encode()) <= 4 * 1024 * 1024, "Fresh mount table exceeds its finite bound")
    mounts = {}
    for line in mount_text.splitlines():
        fields = line.split()
        base.require(len(fields) >= 10 and "-" in fields, "Fresh mount table contains an incomplete record")
        mounts[fields[4]] = set(fields[5].split(","))
    base.require(len(mounts) <= 8192, "Fresh mount table exceeds its finite record bound")
    def mount_options(path: str) -> set[str]:
        matches = [point for point in mounts if path == point or path.startswith(point.rstrip("/") + "/")]
        base.require(bool(matches), "Fresh mount table does not cover an expected protected path")
        return mounts[max(matches, key=len)]
    base.require(all("ro" in mount_options(path) and "rw" not in mount_options(path) for path in READ_ONLY_PATHS) and
                 all("rw" in mount_options(path) and "ro" not in mount_options(path) for path in writable),
                 "Actual fresh mount permissions differ from the declared read-only sandbox")
    return actual


def preconditions() -> int:
    global ACTIVE_NETWORK_NAMESPACE
    ACTIVE_NETWORK_NAMESPACE = None
    base.require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
                 "Run only through authorized root SSH")
    base.require(base.digest(Path(key.__file__)) == PINNED_KEY_RECORDER_SHA256,
                 "The already executed original key-device recorder changed")
    manifest = read_manifest()
    previous_failure_snapshot(manifest)
    previous_empty_capture_snapshot()
    for folder in (EVIDENCE, FRESH_DATA):
        info = folder.lstat()
        base.require(folder.resolve(strict=True) == folder and stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                     stat.S_IMODE(info.st_mode) == 0o700 and (folder / ".goby-managed").read_text().strip() == FRESH_MARKER,
                     "Fresh directory marker, ownership, mode, or canonical path differs")
    base.require(base.ROOT.parent.resolve(strict=True) == base.ROOT.parent and not base.ROOT.is_symlink(),
                 "Capture parent is not the canonical fresh evidence runtime")
    actual = check_fresh_service_authority(manifest)
    check_original_services(manifest)
    forbidden_namespaces = {os.readlink("/proc/1/ns/net"),
                            *(value["networkNamespace"] for value in manifest["oldServices"].values())}
    base.require(actual["networkNamespace"] not in forbidden_namespaces and actual["pid"] not in ORIGINAL_SERVICES.values(),
                 "Fresh reference shares an original or host authority")
    if os.readlink("/proc/self/ns/net") != actual["networkNamespace"]:
        os.execvp("nsenter", ["nsenter", "-t", str(actual["pid"]), "-n", sys.executable, "-B", str(Path(__file__).resolve())])
    base.require(os.readlink("/proc/self/ns/net") == actual["networkNamespace"],
                 "Namespace entry did not establish the attested fresh HTTP authority")
    base.require(base.shutil.disk_usage(base.ROOT.parent).free > 96 * 1024 * 1024,
                 "Fresh capture scratch cannot accommodate its bounded evidence")
    ACTIVE_NETWORK_NAMESPACE = actual["networkNamespace"]
    return actual["pid"]


class FreshHTTPConnection(http.client.HTTPConnection):
    """Adapt the reused recorder's constructor to one fixed fresh endpoint.

    The inherited recorder passes its historical constructor port. That value
    is never used as a network destination: only 18098 in the attested fresh
    namespace is connected. The standard http.client module is not modified.
    """

    def __init__(self, host: str, port: int, **options) -> None:
        base.require(host == "127.0.0.1" and port == 18097 and ACTIVE_NETWORK_NAMESPACE is not None and
                     os.readlink("/proc/self/ns/net") == ACTIVE_NETWORK_NAMESPACE,
                     "HTTP request is outside the attested fresh transport")
        super().__init__("127.0.0.1", FRESH_PORT, **options)


# Only this independently imported recorder module receives the adapter.
key.http = SimpleNamespace(client=SimpleNamespace(HTTPConnection=FreshHTTPConnection))
device.preconditions = preconditions


class Recorder(key.Recorder):
    def __init__(self, pid: int) -> None:
        self.manifest = read_manifest()
        base.require(pid == self.manifest["pid"] and ACTIVE_NETWORK_NAMESPACE == self.manifest["networkNamespace"],
                     "Recorder was not constructed under the attested fresh authority")
        self.bootstrap_tokens: set[str] = set()
        self.fresh_owned_server_baselines: dict[str, dict] = {}
        self.fresh_owned_servers: dict[str, dict] = {}
        self.server_cleanup_observations: list[dict] = []
        self.server_cleanup_done = False
        key.PREVIOUS_SERVER_NUMERIC_ID = self.manifest["bootstrapServerInfo"]["alias"]
        key.PREVIOUS_RECORDS = ORIGINAL_CORPUS_RECORDS + self.manifest["setupRecordCount"]
        super().__init__(pid)
        self.forbidden_tokens.update(self.bootstrap_tokens)
        self.secrets.update(self.bootstrap_tokens)

    def credentials(self, name: str) -> dict:
        base.require(name in {"admin", "viewer"}, "Fresh credential account is outside the owned bootstrap scope")
        path = EVIDENCE / "private" / (name + "-credentials.env")
        base.private_file(path)
        base.require(path.stat().st_size <= 8192, "Fresh credential input exceeds its bound")
        rows = path.read_text().splitlines()
        base.require(len(rows) == 2 and all("=" in row for row in rows), "Fresh credential input shape differs")
        values = dict(row.split("=", 1) for row in rows)
        base.require(set(values) == {"REFERENCE_USERNAME", "REFERENCE_PASSWORD"} and values["REFERENCE_USERNAME"],
                     "Fresh credential fields differ")
        self.secrets.update(value for name, value in values.items() if name == "REFERENCE_PASSWORD" and value)
        return values

    def snapshot(self) -> dict:
        prior_failure_files = previous_failure_snapshot(self.manifest)
        prior_capture_files = previous_empty_capture_snapshot()
        old = self.manifest.get("oldBaseline")
        base.require(isinstance(old, dict) and all(isinstance(old.get(name), dict) for name in
                     ("records", "media", "privateFiles")) and len(old["records"]) == ORIGINAL_CORPUS_RECORDS * 2 and
                     len(old["media"]) == 240, "Original evidence baseline membership differs")
        old_paths = {name: [Path(path) for path in old[name]] for name in ("records", "media", "privateFiles")}
        old_raw_roots = {path.parent for path in old_paths["records"] if path.parent.name == "raw"}
        actual_records = {path for folder in old_raw_roots for path in folder.glob("*.json")}
        actual_records.update(path for folder in old_raw_roots for path in (folder.parent.parent / "export").glob("*.json"))
        actual_private = set()
        for folder in {path.parent for path in old_raw_roots}:
            for path in folder.rglob("*"):
                base.require(not path.is_symlink(), "An original private tree acquired a symbolic link")
                if path.is_file():
                    actual_private.add(path)
                base.require(len(actual_private) < 8192, "Original private membership exceeds its finite bound")
        base.require(actual_records == set(old_paths["records"]) and actual_private == set(old_paths["privateFiles"]),
                     "Original record or private-file membership differs from the READY attestation")
        base.require(len(old_paths["privateFiles"]) < 8192 and
                     sum(path.stat().st_size for path in old_paths["media"]) < 32 * 1024 * 1024 and
                     sum(path.stat().st_size for path in old_paths["privateFiles"]) < 128 * 1024 * 1024,
                     "Original evidence audit exceeds its finite bounds")
        for group, paths in old_paths.items():
            for path in paths:
                verify_original_preserved_path(path, group)
        original = {name: {str(path): base.digest(path) for path in paths} for name, paths in old_paths.items()}
        base.require(original == {name: old[name] for name in old_paths}, "Original evidence differs from the READY attestation")
        setup_raw = sorted((EVIDENCE / "private/raw").glob("*.json"))
        setup_exports = sorted((EVIDENCE / "export").glob("*.json"))
        base.require(len(setup_raw) == len(setup_exports) == self.manifest["setupRecordCount"] and
                     {path.name for path in setup_raw} == {path.name for path in setup_exports} and
                     all(path.name.startswith(SETUP_PREFIX) for path in setup_raw), "Fresh setup record membership differs")
        for path in [*setup_raw, *setup_exports]:
            base.private_file(path)
        # Bootstrap credentials are revoked, but they remain forbidden HTTP
        # inputs even if a later login unexpectedly returns the same token.
        def collect_tokens(value: object) -> None:
            if isinstance(value, dict):
                for field, child in value.items():
                    if field.lower() == "accesstoken" and isinstance(child, str) and child:
                        self.bootstrap_tokens.add(child)
                    else:
                        collect_tokens(child)
            elif isinstance(value, list):
                for child in value:
                    collect_tokens(child)
        for path in setup_raw:
            collect_tokens(json.loads(path.read_text()))
        fresh_private = sorted(path for path in (EVIDENCE / "private").rglob("*") if path.is_file())
        base.require(len(fresh_private) < 1024 and sum(path.stat().st_size for path in fresh_private) < 32 * 1024 * 1024,
                     "Fresh setup private evidence exceeds its finite bounds")
        for path in fresh_private:
            base.private_file(path)
        self.previous_device_ids = {row["Id"] for row in self.manifest["bootstrapDevices"]}
        authority_paths = (EVIDENCE / ".goby-managed", FRESH_DATA / ".goby-managed", EVIDENCE / "runtime/launch.sh")
        for path in authority_paths:
            base.require(path.resolve(strict=True) == path and stat.S_ISREG(path.lstat().st_mode) and path.stat().st_uid == 0,
                         "Fresh authority file is not canonical and root-owned")
        return {"records": {**original["records"], **{str(path): base.digest(path) for path in [*setup_raw, *setup_exports]}},
                "media": original["media"],
                "privateFiles": {**original["privateFiles"], **{str(path): base.digest(path) for path in fresh_private}},
                "authorityFiles": {str(path): base.digest(path) for path in authority_paths},
                "previousFailureFiles": prior_failure_files, "previousCaptureFailureFiles": prior_capture_files}

    def server_creation_reason(self) -> str | None:
        if self.manifest.get("state") != "READY" or self.manifest.get("programData") != str(FRESH_DATA):
            return "The disposable fresh program-data authority is unavailable"
        if not self.old_key_baseline_complete or self.old_keys != []:
            return "The complete fresh pre-creation key baseline is unavailable or contains existing keys"
        expected = self.previous_device_ids - set(self.fresh_owned_server_baselines)
        if not self.device_baseline_complete or self.old_devices is None or set(self.old_devices) != expected:
            return "The complete fresh bootstrap device membership was not established"
        if self.server_public_id != self.manifest["serverId"]:
            return "Fresh public server identity differs from the attested instance"
        if self.old_users is None or {row["Id"] for row in self.old_users} != {row["Id"] for row in self.manifest["bootstrapUsers"]}:
            return "Fresh user membership differs from the two owned bootstrap accounts"
        return None

    @staticmethod
    def aliases(rows: list[dict]) -> set[str]:
        return {str(row[field]) for row in rows for field in ("Id", "ReportedDeviceId", "InternalId")
                if isinstance(row.get(field), (str, int)) and not isinstance(row[field], bool) and str(row[field])}

    def establish_server_creation_gate(self) -> None:
        base.require(read_manifest() == self.manifest and preconditions() == self.pid,
                     "Fresh authority changed before application-key creation")
        reason = self.server_creation_reason()
        base.require(reason is None, reason or "Fresh server creation authority is unavailable")
        candidates = {row["Id"]: row for row in [*self.old_devices.values(), *self.protected_hidden_devices.values()]
                      if row.get("ReportedDeviceId") == self.server_public_id}
        protected = [row for row in [*self.old_devices.values(), *self.protected_hidden_devices.values()]
                     if row.get("ReportedDeviceId") != self.server_public_id]
        protected.append(self.owned_devices[self.control_id])
        protected_aliases = self.aliases(protected)
        base.require(all(not (self.aliases([row]) & protected_aliases) for row in candidates.values()),
                     "Fresh shared server device collides with an ordinary bootstrap or control identity")
        for device_id, row in candidates.items():
            self.fresh_owned_server_baselines[device_id] = {"info": row,
                "options": self.old_options.get(device_id, self.protected_hidden_options.get(device_id)),
                "ownership": "Created inside the attested wholly owned fresh program-data tree"}
            self.old_devices.pop(device_id, None)
            self.old_options.pop(device_id, None)
            self.protected_hidden_devices.pop(device_id, None)
            self.protected_hidden_options.pop(device_id, None)
        self.protected_device_aliases = protected_aliases
        reason = self.server_creation_reason()
        self.server_creation_gate = {"evaluated": True, "allowed": reason is None, "blockedReason": reason,
            "authority": "Wholly owned disposable fresh instance", "programData": str(FRESH_DATA),
            "reportedServerId": self.server_public_id, "completeDeviceBaseline": self.device_baseline_complete,
            "completeKeyBaseline": self.old_key_baseline_complete, "baselineReportedAbsenceRequired": False,
            "freshOwnedBootstrapServerDevices": self.fresh_owned_server_baselines,
            "baselineServerInfo": self.server_baseline_info, "readOnlyServerBaselineBranch": reason is not None,
            "applicationKeyTrafficBeforeGate": len(self.key_create_attempts)}
        base.save(base.PRIVATE / "server-creation-gate.json", self.server_creation_gate)
        base.require(reason is None, "Fresh server creation gate rejected its owned bootstrap membership")

    def owned_pair(self, item: dict) -> bool:
        if super().owned_pair(item):
            return True
        expected = self.fresh_owned_servers.get(item.get("Id"))
        return bool(expected and all(item.get(field) == expected.get(field) for field in ("Id", "ReportedDeviceId", "AppName")))

    def register_server(self, selected: dict, rows: list[dict]) -> None:
        base.require(self.server_creation_gate.get("allowed") is True and self.server_creation_reason() is None and
                     self.server_key_mapping_reason(selected, rows) is None and isinstance(selected.get("AppName"), str) and
                     selected["AppName"] and not (self.aliases([selected]) & self.protected_device_aliases) and
                     selected["Id"] != self.control_id, "Fresh server device lacks positive exclusive numeric key ownership")
        self.fresh_owned_servers[selected["Id"]] = selected
        self.owned_devices[selected["Id"]] = selected
        self.read_ids.add(selected["Id"])

    def read_server_candidates(self, label: str, rows: list[dict]) -> None:
        super().read_server_candidates(label, rows)
        key_rows = self.key_rows_observations[-1]["items"]
        for selected in self.server_candidates.values():
            if self.server_key_mapping_reason(selected, key_rows) is None:
                self.register_server(selected, key_rows)

    def maybe_delete_server_device(self) -> None:
        reason = self.server_creation_reason()
        base.require(self.server_creation_gate.get("allowed") is True and reason is None,
                     reason or "Fresh server-device deletion lacks its creation authority")
        self.read_server_candidates("server-delete-refresh", self.devices("server-delete-refresh-devices"))
        candidates = list(self.server_candidates.values())
        if len(candidates) != 1:
            self.server_branch = {"verified": False, "deleteAttempted": False,
                                  "reason": "Fresh current key mapping did not identify one positive shared server device"}
            raise RuntimeError("Fresh shared server-device deletion remains unverified")
        selected = candidates[0]
        rows = self.key_list("server-delete-key-ownership")
        reason = self.server_key_mapping_reason(selected, rows)
        if reason is not None:
            self.server_branch = {"verified": False, "deleteAttempted": False, "reason": reason}
            raise RuntimeError("Both owned keys no longer prove the fresh shared server device")
        self.register_server(selected, rows)
        self.virtual_write_id = selected["Id"]
        self.server_branch = {"verified": False, "deleteAttempted": True, "targetId": selected["Id"],
            "reason": "Positive fresh DeviceInfo and both current owned keys prove the shared server device",
            "ownership": "Attested disposable program data", "baselineReportedAbsenceRequired": False}
        status, _ = self.mutate_device("delete-owned-server-device", "DELETE", selected["Id"], token=self.admin())
        self.server_branch["deleteStatus"] = status
        self.request("server-delete-sessions-before-key-probes", "GET", "/emby/Sessions", token=self.admin())
        self.request("server-delete-info-after", "GET", self.route("/Info", selected["Id"]), token=self.admin())
        self.devices("server-delete-devices-after")
        self.key_list("server-delete-keys-after")
        self.probe_keys("server-delete-key-protected")
        self.devices("server-delete-devices-after-key-probes")
        base.require(self.probe_login("server-delete-control-protected", "control") == 200,
                     "Shared fresh server deletion invalidated the independent control login")
        self.read_server_candidates("server-delete-registration-after-probes", self.devices("server-delete-registration-devices"))
        self.server_branch["verified"] = status == 204
        self.server_branch["postProbeMappedServerIds"] = sorted(self.server_candidates)

    def cleanup_hidden_servers(self) -> None:
        if self.server_cleanup_done:
            return
        self.server_cleanup_done = True
        for index, (device_id, expected) in enumerate(self.fresh_owned_servers.items()):
            status, info = self.request("cleanup-fresh-server-info-" + str(index), "GET", self.route("/Info", device_id), token=self.admin())
            observation = {"targetId": device_id, "status": status, "info": info, "deleteAttempted": False}
            self.server_cleanup_observations.append(observation)
            if status == 200 and isinstance(info, dict) and all(info.get(field) == expected.get(field) for field in ("Id", "ReportedDeviceId", "AppName")):
                observation["deleteAttempted"] = True
                delete_status, _ = self.mutate_device("cleanup-fresh-server-delete-" + str(index), "DELETE", device_id, token=self.admin())
                observation["deleteStatus"] = delete_status
                base.require(delete_status == 204, "Fresh positive server device cleanup deletion was not acknowledged")
            elif status == 404 or status == 204 and info == "":
                observation["retainedForOperatorTeardown"] = True
                observation["absenceNotInferredFromNumeric204"] = status == 204
                # Do not turn an empty numeric response into an unproved
                # repeat DELETE. Every key and login is independently retired;
                # the operator owns the remaining disposable registry state.
            else:
                raise RuntimeError("Fresh server cleanup cannot prove the previously owned positive identity")

    def compare_devices(self) -> None:
        self.cleanup_hidden_servers()
        super().compare_devices()
        rows = self.final_devices or []
        self.checks["onlyControlOwnedDeviceRetained"] = all(row["Id"] == self.control_id for row in rows if self.owned_pair(row))

    def write(self, label: str, value: dict) -> None:
        if "request" in value:
            value["transportAuthority"] = {"unit": FRESH_UNIT, "port": FRESH_PORT, "pid": self.manifest["pid"],
                "startTicks": self.manifest["startTicks"], "networkNamespace": self.manifest["networkNamespace"]}
        if label == "audit":
            value.update({"preservedOriginalCorpusRecords": ORIGINAL_CORPUS_RECORDS,
                "referenceRun": FRESH_RUN,
                "captureRun": CAPTURE_RUN,
                "previousCaptureAttempt": None if CAPTURE_RUN == 1 else {"captureRun": 1,
                    "captureRoot": str(FIRST_CAPTURE_ROOT), "outcome": "constructor-failed-before-http",
                    "httpAttempts": 0, "rawRecords": 0, "exportedRecords": 0,
                    "sourcePath": str(PREVIOUS_CAPTURE_SOURCE), "sourceSha256": PREVIOUS_CAPTURE_SOURCE_SHA256,
                    "consolePath": str(PREVIOUS_CAPTURE_CONSOLE), "consoleSha256": PREVIOUS_CAPTURE_CONSOLE_SHA256,
                    "preservedFileCount": len(self.baseline.get("previousCaptureFailureFiles", {}))},
                "preservedPreviousFailedEvidenceFiles": len(self.baseline.get("previousFailureFiles", {})),
                "previousFailureProvenance": None if FRESH_RUN == 1 else {name: self.manifest["previousFailureProvenance"][name]
                    for name in ("referenceRun", "evidenceRoot", "unit", "programData", "outcome", "operatorSource",
                                 "operatorSha256", "failureRecord", "cleanupRecord", "setupRecordCount", "fileCount", "totalBytes")},
                "preservedFreshSetupRecords": self.manifest["setupRecordCount"],
                "freshReferenceAuthority": {"unit": FRESH_UNIT, "port": FRESH_PORT, "pid": self.manifest["pid"],
                    "startTicks": self.manifest["startTicks"], "networkNamespace": self.manifest["networkNamespace"],
                    "serverId": self.manifest["serverId"], "programData": str(FRESH_DATA)},
                "freshOwnedBootstrapServerDevices": self.fresh_owned_server_baselines,
                "freshServerCleanupObservations": self.server_cleanup_observations,
                "originalServiceHTTPRequests": 0, "originalServiceMutations": 0,
                "originalServices": {unit: {name: self.manifest["oldServices"][unit][name] for name in
                    ("pid", "startTicks", "networkNamespace")} for unit in ORIGINAL_SERVICES},
                "freshServiceLeftAliveForOperatorTeardown": True,
                "bootstrapCredentialHTTPRequests": 0})
        super().write(label, value)


def main() -> None:
    signal.signal(signal.SIGALRM, device.deadline_expired)
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
