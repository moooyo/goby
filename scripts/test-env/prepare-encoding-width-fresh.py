#!/usr/bin/env python3
"""Prepare or remove one bounded, owned Emby encoding-width fixture.

Run only through authorized root SSH on test-env. One ordinary administrator
login is created and handed live to the separate recorder. Source generation,
local probing and strict full decoding use the installed FFmpeg 9.0.1 toolchain
inside the new network namespace with finite process, resource and byte bounds.
The shared official package, old evidence and service-visible source are read
only. Failure revokes the acknowledged credential when possible and stops only
the attested invocation. Explicit cleanup removes only marked DATA and SOURCE,
retaining all private, sanitized and downloaded-media evidence.
"""

from __future__ import annotations

import argparse
import base64
from contextlib import contextmanager
import datetime as dt
import fcntl
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import resource
import secrets
import selectors
import shutil
import signal
import stat
import subprocess
import sys
import time
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m5h")
ROOT = WORK / "emby-width-fresh-m5h-20260910-02"
DATA = WORK / "emby-width-fresh-data-02"
SOURCE = ROOT / "source"
SOURCE_FILE = SOURCE / "Width Source M5h (2026)" / "Width Source M5h (2026).mp4"
PRIVATE, RAW, EXPORT, RUNTIME = ROOT / "private", ROOT / "private/raw", ROOT / "export", ROOT / "runtime"
MEDIA_EXPORT = ROOT / "media-export"
UNIT = "goby-emby-width-fresh-m5h-20260910-02.service"
MARKER = "goby-emby-width-fresh-m5h-20260910-02-owned-v1"
PREFIX = "encoding-width-fresh-setup-m5h-2-"
DESCRIPTION = "Goby owned fresh Emby encoding width fixture M5h 20260910 02"
SERVER_NAME = "Goby Encoding Width Fresh M5h 20260910 02"
FIXTURE_ATTEMPT = 2
FAILED_ROOT = WORK / "emby-width-fresh-m5h-20260910-01"
FAILED_DATA = WORK / "emby-width-fresh-data-01"
FAILED_SOURCE = FAILED_ROOT / "source"
FAILED_UNIT = "goby-emby-width-fresh-m5h-20260910-01.service"
FAILED_MARKER = "goby-emby-width-fresh-m5h-20260910-01-owned-v1"
FAILED_OPERATOR_SHA256 = "f709780d76efb5adada682ec59b7a2a3695ad5c1d6da45137455ef71b1a907f0"
FAILED_BUNDLE_ROOT = WORK / "width-reference-attempt-1"
FAILED_FILES = {
    ".goby-managed", "private/cleanup-attempt-1.json", "private/cleanup-data-inventory-1.json",
    "private/cleanup-source-inventory-1.json", "private/data-identity.json", "private/data-removal-intent.json",
    "private/launcher-manifest.json", "private/prepare-failure.json", "private/prepare-intent.json",
    "private/source-identity.json", "private/source-removal-intent.json", "private/unit-dispatch.json",
    "runtime/launch.sh", "runtime/service.log",
}
FAILED_BUNDLE_FILES = {
    "OWNER.txt", "source-inputs.json", "preparation-absence-attempt-1.json",
    "prepare-encoding-width-fresh.py", "reference-encoding-width-fresh.py", "test-encoding-width-reference.py",
    "reference-configuration.py", "reference-scheduled-tasks.py", "reference-devices.py", "reference-api-keys.py",
    *{"pure-tests-attempt-1." + extension for extension in ("json", "stderr", "exit")},
    *{prefix + str(attempt) + "." + extension
      for prefix in ("prepare-attempt-", "cleanup-after-prepare-attempt-")
      for attempt in (1, 2) for extension in ("console.json", "stderr", "exit")},
}
PORT, HTTPS_PORT = 18100, 18500
PACKAGE_ROOT = Path("/dev/shm/goby-emby-reference")
APP = PACKAGE_ROOT / "package/opt/emby-server"
PACKAGE_SHA256 = "1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843"
OLD_DATA = Path("/opt/goby-test/emby-reference-data")
OLD_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
PREVIOUS_ROOT = Path("/opt/goby-test/exec-work-m5g/emby-configuration-fresh-m5g-20260910-01")
PREVIOUS = PREVIOUS_ROOT / "runtime/configuration-capture"
RETIRED_DATA = Path("/opt/goby-test/exec-work-m5g/emby-configuration-fresh-data-01")
PREVIOUS_UNIT = "goby-emby-configuration-fresh-m5g-20260910-01.service"
PREVIOUS_PID = 3613232
PREVIOUS_SERVER_ID = "f7fb7ac911e44a42a34c201d786376a5"
OLD_UNITS = {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3614026}
MAIN_START_TICKS = "25289276"
MAIN_BINARY_SHA256 = "e1f6b723eb963ad855f465d0798958480615b8b000455fcf26bc7b088b8d2f2f"
ADMIN_NAME = "reference-encoding-width-fresh-m5h-02"
CONTROL_CLIENT = "Goby Encoding Width Fresh M5h 02"
CONTROL_DEVICE = "goby-encoding-width-fresh-m5h-20260910-02-control"
FFMPEG = Path("/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg")
FFPROBE = FFMPEG.with_name("ffprobe")
RETIRED_SOURCE = Path("/opt/goby-test/exec-scratch/emby-scheduled-tasks-fresh-m5f-20260910-01/source")
RETIRED_CAPTURE = RETIRED_SOURCE.parent / "runtime/task-capture"
HISTORICAL_REMOVED = [
    "/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-02/.goby-managed",
    "/dev/shm/goby-emby-scheduled-tasks-fresh-m5f-20260910-01",
    str(RETIRED_SOURCE),
]
ALL_HISTORICAL_REMOVED = [*HISTORICAL_REMOVED, str(RETIRED_DATA), str(FAILED_DATA), str(FAILED_SOURCE)]
SANITIZER_SOURCES = {
    "reference-configuration.py": "baa425eb9680d3e924c4d18d75ad99cc6d0a10ac438ba402501955bb96a359cd",
    "reference-scheduled-tasks.py": "b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1",
    "reference-devices.py": "96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d",
    "reference-api-keys.py": "ead25f9d47e8faf0b2b579ca8641b4ddcf4f7993c9e5a16710624c2927e749a8",
}
FAILED_BUNDLE_SOURCE_SHA256 = {
    **SANITIZER_SOURCES,
    "prepare-encoding-width-fresh.py": FAILED_OPERATOR_SHA256,
    "reference-encoding-width-fresh.py": "da0778b5301bd8cc6cb36eb96eed21e14a0f895f90d7c071b8a801d90b51c93a",
    "test-encoding-width-reference.py": "99168ee43de6cf0ac1f16ba93947eb17403de725f16fa5931cf040a2ce731689",
}
_CONFIGURATION_MODULE = None
INTENT = PRIVATE / "prepare-intent.json"
IDENTITY = PRIVATE / "service-identity.json"
DISPATCH = PRIVATE / "unit-dispatch.json"
DATA_IDENTITY = PRIVATE / "data-identity.json"
SOURCE_IDENTITY = PRIVATE / "source-identity.json"
SOURCE_VERIFICATION = PRIVATE / "source-verification.json"
CONTROL_LOGIN = PRIVATE / "control-login-response.json"
CONTROL_LOGIN_INTENT = PRIVATE / "control-login-intent.json"
CONTROL_CREDENTIALS = PRIVATE / "control-credentials.env"
MANIFEST = PRIVATE / "manifest.json"
MIB = 1024 * 1024
MAX_BODY, MAX_WIRE, MAX_REQUESTS = 256 * 1024, 8 * MIB, 128
MEDIA_OUTPUT_LIMIT = 256 * 1024
MEDIA_LIMITS = {"addressSpaceBytes": 1024 * MIB, "cpuSeconds": 90, "fileSizeBytes": 32 * MIB,
                "stdoutBytes": MEDIA_OUTPUT_LIMIT, "stderrBytes": MEDIA_OUTPUT_LIMIT,
                "threads": 1, "sourceGenerationSeconds": 90, "probeSeconds": 60, "decodeSeconds": 90}
STATIC_PROPERTIES = ("Id", "Description", "PrivateNetwork", "PrivateTmp", "NoNewPrivileges", "ProtectSystem",
                     "ProtectHome", "WorkingDirectory", "ExecStart", "ControlGroup", "FragmentPath", "DropInPaths",
                     "ReadWritePaths", "ReadOnlyPaths", "MemoryMax", "CPUQuotaPerSecUSec", "TasksMax", "User",
                     "KillMode", "TimeoutStopUSec", "LimitNOFILE", "UMask", "PrivateDevices", "BindReadOnlyPaths")
READ_ONLY_PATHS = {str(WORK), str(PACKAGE_ROOT), str(OLD_DATA), "/opt/goby-fixtures",
                   "/opt/goby-test/exec-scratch", str(PREVIOUS_ROOT.parent), str(SOURCE), str(FAILED_ROOT), str(FAILED_BUNDLE_ROOT)}
ATOMIC_NAMES = {"prepare-intent.json", "service-identity.json", "unit-dispatch.json", "manifest.json",
                "launcher-manifest.json", "data-identity.json", "data-removal-intent.json",
                "source-identity.json", "source-removal-intent.json", "source-verification.json",
                "control-login-intent.json", "control-login-response.json", "control-credentials.env"}


def require(condition: object, message: str) -> None:
    if not condition:
        raise RuntimeError(message)


def utc() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(65536), b""):
            result.update(block)
    return result.hexdigest()


def canonical(path: Path, *, directory: bool = False, private: bool = False) -> os.stat_result:
    require(path.is_absolute() and path.resolve(strict=True) == path, "An expected path is not canonical")
    entry = path.lstat()
    require(stat.S_ISDIR(entry.st_mode) if directory else stat.S_ISREG(entry.st_mode), "An expected path has the wrong type")
    if private:
        require(entry.st_uid == 0 and stat.S_IMODE(entry.st_mode) == (0o700 if directory else 0o600),
                "Private ownership or mode differs")
    if not directory and private:
        require(entry.st_nlink == 1, "A private or preserved input has unexpected hard links")
    return entry


def save(path: Path, value: object, *, mode: int = 0o600) -> None:
    canonical(path.parent, directory=True, private=True)
    content = value if isinstance(value, str) else json.dumps(value, indent=2, ensure_ascii=False) + "\n"
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode),
                   "w", encoding="utf-8") as stream:
        stream.write(content)
        stream.flush()
        os.fsync(stream.fileno())


@contextmanager
def blocked_signals():
    """Defer handled termination only across short ownership publications."""
    previous = signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGINT, signal.SIGTERM, signal.SIGHUP})
    try:
        yield
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous)


def atomic_save(path: Path, value: object) -> None:
    require(path.parent == PRIVATE and path.name in ATOMIC_NAMES, "Unexpected atomic authority path")
    canonical(PRIVATE, directory=True, private=True)
    require(not path.exists() and not path.is_symlink(), "Refusing to replace an authority file")
    number = next((index for index in range(1, 100) if not (PRIVATE / ("." + path.name + ".pending-" + str(index))).exists()), None)
    require(number is not None, "Authority publication attempt budget exhausted")
    pending = PRIVATE / ("." + path.name + ".pending-" + str(number))
    # A failed full write leaves only a private pending file, never an apparent
    # final credential. Link creation is atomic and refuses an existing target.
    save(pending, value)
    with blocked_signals():
        try:
            os.link(pending, path, follow_symlinks=False)
        finally:
            if path.exists() and not path.is_symlink() and pending.exists() and not pending.is_symlink():
                current, temporary = path.lstat(), pending.lstat()
                if (current.st_dev, current.st_ino) == (temporary.st_dev, temporary.st_ino):
                    pending.unlink()
        directory_fd = os.open(PRIVATE, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)


def recover_atomic_publications() -> None:
    canonical(PRIVATE, directory=True, private=True)
    for name in sorted(ATOMIC_NAMES):
        final = PRIVATE / name
        if not final.exists() or final.is_symlink():
            continue
        info = final.lstat()
        if info.st_nlink == 1:
            continue
        require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 2,
                "An interrupted authority publication has an unexpected link or owner")
        matches = []
        for pending in PRIVATE.glob("." + name + ".pending-*"):
            require(re.fullmatch(re.escape("." + name + ".pending-") + r"[1-9][0-9]?", pending.name),
                    "Authority pending-file name differs")
            require(pending.resolve(strict=True) == pending and not pending.is_symlink(), "Authority pending file is not canonical")
            entry = pending.lstat()
            if (entry.st_dev, entry.st_ino) == (info.st_dev, info.st_ino):
                matches.append(pending)
        require(len(matches) == 1, "Interrupted authority publication lacks its exact temporary link")
        matches[0].unlink()


def load(path: Path) -> dict:
    canonical(path, private=True)
    require(path.stat().st_size < 8 * MIB, "An operator input exceeds its finite bound")
    result = json.loads(path.read_text())
    require(isinstance(result, dict), "An operator input is not an object")
    return result


def command_failure(args: list[str], *, timeout: float, failure_type: str, returncode: int | None = None,
                    stdout: bytes | str | None = None, stderr: bytes | str | None = None) -> None:
    """Retain bounded diagnostics only in the selected owned private root.

    These operator subprocesses carry service names, paths and flags, never
    HTTP credentials. HTTP requests use the separate namespace-bound recorder.
    """
    try:
        owned_root(ROOT)
        canonical(PRIVATE, directory=True, private=True)
        require(len(args) <= 128 and sum(len(value.encode()) for value in args) <= 65536,
                "Command arguments exceed the diagnostic bound")
        folder = PRIVATE / "command-failures"
        if not folder.exists():
            folder.mkdir(mode=0o700)
        canonical(folder, directory=True, private=True)
        number = next((index for index in range(1, 100) if not (folder / ("command-failure-" + str(index).zfill(3) + ".json")).exists()), None)
        require(number is not None, "Command diagnostic budget exhausted")
        def bounded(value: bytes | str | None) -> dict:
            content = value.encode() if isinstance(value, str) else value or b""
            return {"text": content[:65536].decode("utf-8", errors="replace"), "bytes": len(content), "truncated": len(content) > 65536}
        save(folder / ("command-failure-" + str(number).zfill(3) + ".json"),
             {"at": utc(), "arguments": args, "timeoutSeconds": timeout,
              "failureType": failure_type, "returnCode": returncode, "stdout": bounded(stdout), "stderr": bounded(stderr)})
    except Exception:
        # Diagnostics must not overwrite earlier evidence or replace the
        # original command failure when storage itself is unavailable.
        pass


def run(args: list[str], *, timeout: float = 15) -> str:
    try:
        result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout, check=False)
    except subprocess.TimeoutExpired as error:
        command_failure(args, timeout=timeout, failure_type="TimeoutExpired", stdout=error.stdout, stderr=error.stderr)
        raise RuntimeError("A bounded operator command timed out: " + Path(args[0]).name) from None
    except OSError as error:
        command_failure(args, timeout=timeout, failure_type=type(error).__name__)
        raise RuntimeError("A bounded operator command could not start: " + Path(args[0]).name) from None
    if result.returncode != 0 or len(result.stdout) >= 4 * MIB:
        command_failure(args, timeout=timeout, failure_type="NonzeroExit" if result.returncode else "OutputLimitExceeded",
                        returncode=result.returncode, stdout=result.stdout, stderr=result.stderr)
        raise RuntimeError("A bounded operator command failed: " + Path(args[0]).name)
    try:
        return result.stdout.decode("utf-8", errors="strict")
    except UnicodeDecodeError:
        command_failure(args, timeout=timeout, failure_type="InvalidUTF8", returncode=result.returncode,
                        stdout=result.stdout, stderr=result.stderr)
        raise RuntimeError("A bounded operator command returned invalid text: " + Path(args[0]).name) from None


def properties(unit: str) -> dict[str, str]:
    names = ("LoadState", "ActiveState", "SubState", "MainPID", "InvocationID", *STATIC_PROPERTIES)
    response = subprocess.run(["systemctl", "show", unit, *[arg for name in names for arg in ("-p", name)]],
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15, check=False)
    require(len(response.stdout) < MIB, "Service property output exceeded its bound")
    result = dict(line.split("=", 1) for line in response.stdout.decode().splitlines() if "=" in line)
    require(response.returncode == 0 or response.returncode == 1 and result.get("LoadState") == "not-found",
            "Service properties could not be obtained")
    return result


def process_identity(pid: int) -> dict:
    require(pid > 1, "Service has no process")
    proc = Path("/proc") / str(pid)
    raw_stat = (proc / "stat").read_text()
    fields = raw_stat[raw_stat.rfind(")") + 2:].split()
    args = [part.decode() for part in (proc / "cmdline").read_bytes().split(b"\0") if part]
    return {"pid": pid, "startTicks": fields[19], "uid": proc.stat().st_uid,
            "networkNamespace": os.readlink(proc / "ns/net"), "cmdline": args,
            "exe": os.readlink(proc / "exe"), "cwd": os.readlink(proc / "cwd"), "cgroup": (proc / "cgroup").read_text()}


def service_identity(unit: str) -> dict:
    state = properties(unit)
    require(state.get("ActiveState") == "active", "A required service is not active")
    identity = process_identity(int(state.get("MainPID", "0")))
    require(re.fullmatch(r"[0-9a-f]{32}", state.get("InvocationID", "")), "A required service has no stable invocation identity")
    identity.update({"unit": unit, "invocationId": state["InvocationID"],
                     "serviceProperties": {name: state.get(name, "") for name in STATIC_PROPERTIES}})
    return identity


def old_services() -> dict:
    result = {unit: service_identity(unit) for unit in OLD_UNITS}
    for unit, expected in OLD_UNITS.items():
        require(result[unit]["pid"] == expected, "A protected service process differs from the reviewed instance")
    reference = result["goby-emby-reference.service"]
    require(reference["uid"] == 0 and reference["serviceProperties"]["PrivateNetwork"] == "yes" and
            reference["networkNamespace"] != os.readlink("/proc/1/ns/net"), "The old reference isolation differs")
    main = result["goby-foundation-test.service"]
    require(main["uid"] == 995 and main["startTicks"] == MAIN_START_TICKS and main["exe"] == "/opt/goby-dev/goby" and
            digest(Path(main["exe"])) == MAIN_BINARY_SHA256, "The protected Goby process or binary differs")
    main["binarySha256"] = MAIN_BINARY_SHA256
    return result


def check_old_services(expected: dict) -> None:
    require(old_services() == expected, "A protected service identity or configuration changed")


def preserved_failed_tree(folder: Path, membership: set[str], *, private: bool) -> dict:
    """Read a fixed retired tree without repairing, deleting or adding evidence."""
    require(folder in {FAILED_ROOT, FAILED_BUNDLE_ROOT}, "Unexpected failed-attempt preservation root")
    root_entry = canonical(folder, directory=True, private=private)
    require(root_entry.st_uid == 0 and root_entry.st_mode & 0o022 == 0, "Failed-attempt root is not protected")
    files, entry_count, total = {}, 0, 0
    for current, directories, names in os.walk(folder, followlinks=False):
        for name in directories + names:
            path = Path(current) / name
            entry_count += 1
            require(entry_count <= 128 and path.resolve(strict=True) == path and not path.is_symlink() and not os.path.ismount(path),
                    "Failed-attempt membership crosses a path or mount boundary")
            entry = path.lstat()
            require(entry.st_uid == 0 and entry.st_mode & 0o022 == 0 and
                    (stat.S_ISDIR(entry.st_mode) or stat.S_ISREG(entry.st_mode)),
                    "Failed-attempt membership has an unsafe owner, mode or type")
            if stat.S_ISREG(entry.st_mode):
                require(entry.st_nlink == 1, "Failed-attempt evidence has an unexpected hard link")
                total += entry.st_size
                require(total <= 32 * MIB, "Failed-attempt preservation bytes exceed the finite bound")
                files[str(path)] = {"bytes": entry.st_size, "sha256": digest(path)}
    require({str(Path(name).relative_to(folder)) for name in files} == membership,
            "The exact failed-attempt file membership differs")
    if folder == FAILED_BUNDLE_ROOT:
        require(all(Path(name).parent == folder for name in files) and entry_count == 25,
                "The frozen source bundle must contain exactly 25 files and no subdirectories")
    return dict(sorted(files.items()))


def failed_fixture_provenance() -> dict:
    """Preserve the failed first attempt and require its complete prior teardown."""
    files = preserved_failed_tree(FAILED_ROOT, FAILED_FILES, private=True)
    require(len(files) == 14, "The failed first fixture must retain exactly 14 evidence files")
    marker = FAILED_ROOT / ".goby-managed"
    canonical(marker, private=True)
    require(marker.read_text().strip() == FAILED_MARKER, "Failed first fixture marker differs")
    intent_path = FAILED_ROOT / "private/prepare-intent.json"
    failure_path = FAILED_ROOT / "private/prepare-failure.json"
    cleanup_path = FAILED_ROOT / "private/cleanup-attempt-1.json"
    intent, failure, cleanup = load(intent_path), load(failure_path), load(cleanup_path)
    require(intent.get("marker") == FAILED_MARKER and intent.get("unit") == FAILED_UNIT and
            intent.get("evidenceRoot") == str(FAILED_ROOT) and intent.get("programData") == str(FAILED_DATA) and
            intent.get("sourceRoot") == str(FAILED_SOURCE) and intent.get("operatorSha256") == FAILED_OPERATOR_SHA256,
            "Failed first fixture intent does not identify the frozen first attempt")
    require(failure.get("message") == "Fresh service failed to establish its process identity",
            "The first fixture did not fail at the observed pre-bootstrap identity boundary")
    require(cleanup.get("attempt") == 1 and cleanup.get("unit") == FAILED_UNIT and
            cleanup.get("programData") == str(FAILED_DATA) and cleanup.get("sourceRoot") == str(FAILED_SOURCE) and
            all(cleanup.get(name) is True for name in ("dataRemoved", "sourceRemoved", "evidenceRetained",
                                                      "oldPreservationVerified", "controlInvalidityProvenOrNeverAcknowledged")) and
            cleanup.get("controlRevocation") == {"acknowledged": False, "notApplicable": True, "invalidityProven": False},
            "The first fixture lacks its confirmed complete cleanup without login")
    expected_stop = {"stopped": True, "ownedProcessGone": True, "ownedCgroupEmpty": True,
                     "unitMainPID": 0, "finalActiveState": "inactive"}
    require(cleanup.get("stop") == expected_stop, "The first fixture stop evidence differs")
    absent = [FAILED_DATA, FAILED_SOURCE, *[FAILED_ROOT / "private" / name for name in
              ("manifest.json", "control-login-intent.json", "control-login-response.json", "control-credentials.env",
               "control-account-secret.json", "source-verification.json", "source-generation.json", "bootstrap-result.json")]]
    require(all(not path.exists() and not path.is_symlink() for path in absent),
            "The first fixture contains a removed root, login or completed preparation artifact")
    for folder in (FAILED_ROOT / "private/raw", FAILED_ROOT / "private/wire", FAILED_ROOT / "export", FAILED_ROOT / "media-export"):
        canonical(folder, directory=True, private=True)
        require(not list(folder.iterdir()), "The failed first fixture unexpectedly contains HTTP or generated-media evidence")
    state = properties(FAILED_UNIT)
    require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
            "The failed first fixture has an active service or process")
    group = Path("/sys/fs/cgroup/system.slice") / FAILED_UNIT
    if group.exists():
        require((group / "cgroup.procs").read_text().strip() == "", "The failed first fixture cgroup still has a process")
        events = dict(line.split(maxsplit=1) for line in (group / "cgroup.events").read_text().splitlines())
        require(events.get("populated") == "0", "A descendant of the failed first fixture cgroup remains populated")
    bundle_files = preserved_failed_tree(FAILED_BUNDLE_ROOT, FAILED_BUNDLE_FILES, private=False)
    require(len(bundle_files) == 25 and all(bundle_files[str(FAILED_BUNDLE_ROOT / name)]["sha256"] == expected
                                          for name, expected in FAILED_BUNDLE_SOURCE_SHA256.items()),
            "The frozen first-attempt source bundle differs from its seven pinned source files")
    return {"evidenceRoot": str(FAILED_ROOT), "programData": str(FAILED_DATA), "sourceRoot": str(FAILED_SOURCE),
            "unit": FAILED_UNIT, "marker": FAILED_MARKER, "operatorSha256": FAILED_OPERATOR_SHA256,
            "prepareFailure": {"path": str(failure_path), "sha256": digest(failure_path)},
            "cleanupReport": {"path": str(cleanup_path), "sha256": digest(cleanup_path)}, "files": files,
            "sourceBundleRoot": str(FAILED_BUNDLE_ROOT), "sourceBundleFiles": bundle_files,
            "dataAbsent": True, "sourceAbsent": True, "unitInactive": True, "unitMainPID": 0,
            "cgroupEmpty": True, "noOrdinaryLogin": True, "noSourceGeneration": True, "httpRecordFiles": 0}


def old_baseline() -> dict:
    previous = load(PREVIOUS / "private/baseline.json")
    audit = load(PREVIOUS / "private/raw/configuration-fresh-m5g-audit.json")
    require(audit.get("cleanupPassed") is True and audit.get("captureFailureType") is None and
            audit.get("httpAttempts") == 236 and audit.get("incompleteHTTP") == 0 and
            audit.get("cleanupErrors") == [] and audit.get("persistenceFailures") == [] and
            isinstance(audit.get("checks"), dict) and audit["checks"] and all(value is True for value in audit["checks"].values()) and
            audit.get("serverId") == PREVIOUS_SERVER_ID and audit.get("referencePID") == PREVIOUS_PID and
            audit.get("unit") == PREVIOUS_UNIT and audit.get("preservedRecordFilesIncludingSetup") == 4136,
            "The preceding configuration mutation study does not have proven bounded cleanup")
    for name in ("scheduledTaskMutationRequests", "libraryMutationRequests", "deviceMutationRequests", "mediaOrEncoderRequests", "sourceWrites"):
        require(audit.get(name) == 0, "The preceding configuration study escaped its recorded scope")
    require(len(previous.get("records", {})) == 4136 and len(previous.get("media", {})) == 240 and
            len(previous.get("privateFiles", {})) == 2854, "The inherited configuration preservation population differs")
    for group in ("records", "media", "privateFiles", "retainedHistoricalFiles"):
        require(isinstance(previous.get(group, {}), dict), "An inherited preservation group differs")
        for name, expected in previous.get(group, {}).items():
            canonical(Path(name))
            require(digest(Path(name)) == expected, "Previously recorded evidence or media changed")
    paths = [Path(name) for name in previous["records"]]
    raw, exported = [], []
    for folder, result in ((PREVIOUS / "private/raw", raw), (PREVIOUS / "export", exported)):
        canonical(folder, directory=True, private=True)
        result.extend(sorted(folder.glob("*.json")))
    require(len(raw) == len(exported) == 237 and {path.name for path in raw} == {path.name for path in exported} and
            any(path.name == "configuration-fresh-m5g-audit.json" for path in raw),
            "The preceding configuration raw/export membership is not 237 exact pairs")
    paths.extend(raw + exported)
    require(len(paths) == len(set(paths)) == 4610, "Expected exactly 2305 old raw/export record pairs")
    media = [Path(name) for name in previous["media"]]
    require(len(set(media)) == 240, "Expected exactly 240 surviving old media sources")
    require(previous.get("historicalRemovedPaths") == HISTORICAL_REMOVED and
            all(not Path(name).exists() and not Path(name).is_symlink() for name in ALL_HISTORICAL_REMOVED),
            "The exact historical removal exceptions differ")
    cleanup_path = PREVIOUS_ROOT / "private/cleanup-attempt-1.json"
    cleanup = load(cleanup_path)
    require(digest(cleanup_path) == "229113c24d22af55a80499f02cc7b1690a55cfde50cf7220060ea25b3eec375a" and
            cleanup.get("attempt") == 1 and cleanup.get("unit") == PREVIOUS_UNIT and
            cleanup.get("programData") == str(RETIRED_DATA) and cleanup.get("dataRemoved") is True and
            cleanup.get("evidenceRetained") is True and cleanup.get("oldPreservationVerified") is True and
            cleanup.get("stop", {}).get("stopped") is True and cleanup["stop"].get("ownedProcessGone") is True,
            "The retired M5g DATA lacks its pinned completed operator cleanup proof")
    previous_identity = load(PREVIOUS_ROOT / "private/service-identity.json")
    require(previous_identity.get("pid") == PREVIOUS_PID, "The retired M5g process proof differs")
    prior_state = properties(PREVIOUS_UNIT)
    require(int(prior_state.get("MainPID", "0")) == 0 and
            prior_state.get("ActiveState") not in {"active", "activating", "deactivating"}, "The retired M5g unit is active")
    if (Path("/proc") / str(PREVIOUS_PID)).exists():
        require(process_identity(PREVIOUS_PID)["startTicks"] != previous_identity["startTicks"], "The retired M5g process remains")
    retired = previous.get("retiredOwnedSources", {})
    retired_files, retired_snapshot = retired.get("files", {}), retired.get("snapshot", {})
    require(retired.get("removedAfterCompletedStudy") is True and len(retired_files) == 512 and
            retired_snapshot.get("root") == str(RETIRED_SOURCE) and retired_snapshot.get("complete") is True and
            retired_snapshot.get("fileCount") == 512 and set(retired_snapshot.get("files", {})) == set(retired_files) and
            all(Path(name).is_absolute() and ".." not in Path(name).parts and RETIRED_SOURCE in Path(name).parents and
                retired_snapshot["files"][name].get("sha256") == expected for name, expected in retired_files.items()),
            "The retired source provenance differs; retired paths must never be reopened")
    historical = load(RETIRED_CAPTURE / "private/baseline.json")
    require(historical.get("ownedSourceFiles") == retired_files and historical.get("ownedSourceSnapshot") == retired_snapshot,
            "The retired source proof no longer agrees with its preserved original capture")
    private_files: set[Path] = set()
    for folder in {path.parent.parent for path in paths if path.parent.name == "raw"} | {PREVIOUS_ROOT / "private"}:
        canonical(folder, directory=True, private=True)
        for path in folder.rglob("*"):
            require(not path.is_symlink(), "A protected private tree contains a symbolic link")
            if path.is_file():
                private_files.add(path)
            require(len(private_files) < 8192, "Protected private membership exceeds its bound")
    require(sum(path.stat().st_size for path in private_files) < 128 * MIB and
            sum(path.stat().st_size for path in media) < 32 * MIB, "The complete preservation bytes exceed their bounds")
    for path in [*paths, *media, *private_files]:
        canonical(path)
    failed = failed_fixture_provenance()
    failed_hashes = {name: entry["sha256"] for name, entry in {**failed["files"], **failed["sourceBundleFiles"]}.items()}
    return {"records": {str(path): digest(path) for path in sorted(paths)},
            "media": {str(path): digest(path) for path in sorted(media)},
            "privateFiles": {str(path): digest(path) for path in sorted(private_files)},
            "retainedHistoricalFiles": {**previous.get("retainedHistoricalFiles", {}), **failed_hashes},
            "historicalRemovedPaths": ALL_HISTORICAL_REMOVED, "retiredOwnedSources": retired,
            "failedFixtureProvenance": failed,
            "retiredProgramData": {"path": str(RETIRED_DATA), "removedAfterCompletedStudy": True,
                                   "cleanupReport": str(cleanup_path), "cleanupReportSha256": digest(cleanup_path),
                                   "serviceIdentity": previous_identity}}


def verify_baseline(expected: dict) -> None:
    require(old_baseline() == expected, "Old records, source files, or private evidence changed")


def verify_package(intent: dict) -> None:
    require((PACKAGE_ROOT / ".extracted").read_text().strip() == intent["packageSha256"] == PACKAGE_SHA256 and
            digest(APP / "system/EmbyServer") == intent["sharedServerBinarySha256"], "Shared official package provenance or binary changed")



def tree_size(root: Path) -> int:
    if not root.exists():
        return 0
    canonical(root, directory=True, private=True)
    total, count = 0, 0
    for folder, directories, files in os.walk(root, followlinks=False):
        for name in directories + files:
            path = Path(folder) / name
            require(not path.is_symlink(), "An owned tree contains a symbolic link")
            entry = path.lstat()
            require(stat.S_ISREG(entry.st_mode) or stat.S_ISDIR(entry.st_mode), "An owned tree contains a special file")
            if stat.S_ISREG(entry.st_mode):
                total += entry.st_size
            count += 1
            require(count <= 20000, "An owned tree has too many entries")
    return total


def resources(*, admission: bool = False) -> dict:
    memory = {line.split(":", 1)[0]: int(line.split()[1]) * 1024 for line in Path("/proc/meminfo").read_text().splitlines()
              if line.startswith(("MemAvailable:", "MemTotal:"))}
    report = {"at": utc(), **memory, "persistentFreeBytes": shutil.disk_usage(WORK).free,
              "dataBytes": tree_size(DATA), "evidenceBytes": tree_size(ROOT)}
    if admission:
        require(report["MemAvailable"] >= 1920 * MIB and report["persistentFreeBytes"] >= 2048 * MIB,
                "Fresh width fixture needs its bounded memory and persistent-disk reserve")
    else:
        require(report["MemAvailable"] >= 512 * MIB and report["persistentFreeBytes"] >= 512 * MIB,
                "Fresh width fixture exhausted its retained headroom")
        require(report["dataBytes"] <= 512 * MIB and report["evidenceBytes"] <= 256 * MIB,
                "Fresh width fixture exceeded its bounded data or evidence growth")
    state = properties(UNIT)
    cgroup = state.get("ControlGroup", "")
    if cgroup == "/system.slice/" + UNIT:
        current = Path("/sys/fs/cgroup") / cgroup.lstrip("/") / "memory.current"
        if current.exists():
            report["freshCgroupMemoryBytes"] = int(current.read_text())
    return report


def ensure_work_parent() -> None:
    require(WORK == Path("/opt/goby-test/exec-work-m5h"), "Unexpected configuration work parent")
    canonical(WORK.parent, directory=True)
    require(WORK.parent.stat().st_uid == 0 and WORK.parent.stat().st_mode & 0o022 == 0, "The fixed work parent is unsafe")
    if not WORK.exists() and not WORK.is_symlink():
        WORK.mkdir(mode=0o700)
    canonical(WORK, directory=True, private=True)


def common_preconditions(*, host: bool) -> None:
    require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
            "Run only through authorized root SSH on test-env")
    if host:
        require(os.readlink("/proc/self/ns/net") == os.readlink("/proc/1/ns/net"), "Operator must begin in the host network namespace")
    require(ROOT.parent == DATA.parent == WORK and SOURCE.parent == ROOT, "Fresh owned roots escaped their fixed parents")
    canonical(WORK, directory=True, private=True)
    for folder in (PACKAGE_ROOT, OLD_DATA):
        canonical(folder, directory=True, private=True)
        canonical(folder / ".goby-managed", private=True)
        require((folder / ".goby-managed").read_text().strip() == "goby-emby-reference-owned-v1", "Shared reference ownership differs")
    canonical(PACKAGE_ROOT / ".extracted")
    require((PACKAGE_ROOT / ".extracted").read_text().strip() == PACKAGE_SHA256, "Shared package extraction provenance differs")
    canonical(APP / "system/EmbyServer")
    operator_entry = canonical(Path(__file__).resolve())
    require(operator_entry.st_uid == 0 and operator_entry.st_mode & 0o022 == 0, "The width operator is not a protected root-owned file")


def verify_sanitizer_sources() -> None:
    for name, expected in SANITIZER_SOURCES.items():
        path = Path(__file__).resolve().with_name(name)
        entry = canonical(path)
        require(entry.st_uid == 0 and entry.st_mode & 0o022 == 0 and digest(path) == expected,
                "A pinned deep configuration sanitizer dependency changed")


def configuration_sanitizer():
    global _CONFIGURATION_MODULE
    verify_sanitizer_sources()
    if _CONFIGURATION_MODULE is None:
        path = Path(__file__).resolve().with_name("reference-configuration.py")
        specification = importlib.util.spec_from_file_location("fresh_configuration_deep_sanitizer", path)
        require(specification is not None and specification.loader is not None, "The pinned configuration sanitizer is unavailable")
        module = importlib.util.module_from_spec(specification)
        specification.loader.exec_module(module)
        # Only the pure redaction methods are used. Never call this recorder's
        # constructor, preconditions, snapshot, HTTP, writer or main entry point.
        module.base.DATA, module.base.ROOT, module.base.RUNTIME = DATA, ROOT, RUNTIME
        module.base.PRIVATE, module.base.RAW, module.base.EXPORT = PRIVATE, RAW, EXPORT
        module.base.PREFIX, module.base.MARKER, module.base.__file__ = PREFIX, MARKER, str(Path(__file__).resolve())
        _CONFIGURATION_MODULE = module
    redactor = _CONFIGURATION_MODULE.Recorder.__new__(_CONFIGURATION_MODULE.Recorder)
    redactor.secrets = set()
    redactor._secret_signature = redactor._secret_regex = None
    return redactor


def owned_root(folder: Path) -> None:
    require(folder in {ROOT, DATA, SOURCE}, "Unexpected owned root")
    canonical(folder, directory=True, private=True)
    canonical(folder / ".goby-managed", private=True)
    marker = MARKER
    require((folder / ".goby-managed").read_text().strip() == marker, "Fresh fixture ownership marker differs")


def create_owned_root(folder: Path) -> None:
    require(folder in {ROOT, DATA, SOURCE} and not folder.exists() and not folder.is_symlink(), "Owned root already exists")
    with blocked_signals():
        folder.mkdir(mode=0o700)
        created_inode = folder.stat().st_ino
        try:
            save(folder / ".goby-managed", MARKER + "\n")
            if folder == DATA:
                atomic_save(DATA_IDENTITY, data_identity())
            elif folder == SOURCE:
                atomic_save(SOURCE_IDENTITY, source_root_identity())
        except Exception:
            # No unit or application can use this root before its marker exists.
            # Remove only our just-created empty root or partial marker; never
            # remove other membership if an external actor changed the directory.
            entries = list(folder.iterdir())
            if folder.stat().st_ino == created_inode and set(entries) <= {folder / ".goby-managed"}:
                if entries:
                    canonical(entries[0], private=True)
                    entries[0].unlink()
                folder.rmdir()
            raise


def owned_roots() -> None:
    for folder in (ROOT, DATA, SOURCE):
        owned_root(folder)
    for folder in (PRIVATE, RAW, EXPORT, RUNTIME):
        canonical(folder, directory=True, private=True)
    require(load(DATA_IDENTITY) == data_identity(), "Fresh program-data creation identity differs")
    require(load(SOURCE_IDENTITY) == source_root_identity(), "Fresh source-root creation identity differs")


def owned_authority() -> None:
    """Retained stop authority does not depend on partially removed roots."""
    owned_root(ROOT)
    canonical(PRIVATE, directory=True, private=True)
    for folder in (RAW, EXPORT, MEDIA_EXPORT, RUNTIME):
        if folder.exists() or folder.is_symlink():
            canonical(folder, directory=True, private=True)
    if DATA.exists():
        require(load(DATA_IDENTITY) == data_identity(), "Fresh program-data creation identity differs")


def private_devices_proof(identity: dict) -> dict:
    """Observe the service's actual private device tree through its proc root."""
    proc = Path("/proc") / str(identity["pid"])
    device_root = proc / "root/dev"
    require(device_root.stat().st_dev != Path("/dev").stat().st_dev, "Fresh /dev still shares the host device mount")
    entries = list(device_root.iterdir())
    require(len(entries) <= 32, "Private device-tree membership exceeds its bound")
    safe_devices = {(1, 3), (1, 5), (1, 7), (1, 8), (1, 9), (5, 0), (5, 1), (5, 2)}
    nodes = []
    for entry in entries:
        require(not re.match(r"(?:dri|nvidia|kfd|video|dxg)", entry.name), "A GPU or video device is visible in the private tree")
        info = entry.lstat()
        if stat.S_ISCHR(info.st_mode) or stat.S_ISBLK(info.st_mode):
            pair = (os.major(info.st_rdev), os.minor(info.st_rdev))
            require(stat.S_ISCHR(info.st_mode) and pair in safe_devices, "An unexpected device node is visible")
            nodes.append({"name": entry.name, "major": pair[0], "minor": pair[1]})
    mount_lines = (proc / "mountinfo").read_text().splitlines()
    private_dev = [line for line in mount_lines if line.split()[4] == "/dev"]
    require(len(private_dev) == 1 and private_dev[0].split(" - ", 1)[1].split()[0] == "tmpfs",
            "The actual private /dev mount is not tmpfs")
    visible_binary = (proc / "root" / str(APP / "system/EmbyServer").lstrip("/")).stat()
    host_binary = (APP / "system/EmbyServer").stat()
    require((visible_binary.st_dev, visible_binary.st_ino) == (host_binary.st_dev, host_binary.st_ino),
            "The service does not see the same shared official package")
    return {"privateDevices": True, "mountNamespace": os.readlink(proc / "ns/mnt"),
            "deviceMount": private_dev[0], "deviceNodes": sorted(nodes, key=lambda row: row["name"]),
            "gpuDevicePathsAbsent": True, "sharedPackageVisible": True}


def new_identity() -> dict:
    owned_roots()
    identity = service_identity(UNIT)
    settings, args = identity["serviceProperties"], identity["cmdline"]
    require(identity["uid"] == 0 and identity["exe"] == str(APP / "system/EmbyServer"), "Fresh service executable or UID differs")
    require(identity["cwd"] == str(APP), "Fresh Emby working directory does not resolve its relative ELF interpreter")
    require(args.count("-programdata") == 1 and args[args.index("-programdata") + 1] == str(DATA),
            "Fresh service program-data argument differs")
    require(settings["Description"] == DESCRIPTION and settings["WorkingDirectory"] == str(RUNTIME) and
            str(RUNTIME / "launch.sh") in settings["ExecStart"] and settings["MemoryMax"] == str(1152 * MIB) and
            settings["PrivateNetwork"] == "yes" and settings["PrivateTmp"] == "yes" and settings["PrivateDevices"] == "yes" and
            settings["NoNewPrivileges"] == "yes" and settings["ProtectSystem"] == "strict" and settings["ProtectHome"] == "yes" and
            settings["KillMode"] == "control-group" and settings["TasksMax"] == "256" and settings["LimitNOFILE"] == "65536" and
            settings["UMask"] == "0077" and settings["TimeoutStopUSec"] == "25s" and
            settings["CPUQuotaPerSecUSec"] in {"1.5s", "1.500000s", "1500ms", "1s 500ms"} and settings["User"] in {"", "root"},
            "Fresh service sandbox or launcher differs")
    require(set(settings["ReadWritePaths"].split()) == {str(DATA), str(RUNTIME)}, "Fresh service writable paths differ")
    require(set(settings["ReadOnlyPaths"].split()) == READ_ONLY_PATHS, "Shared tmpfs and reference paths are not explicitly read-only")
    shared_path = str(PACKAGE_ROOT)
    require(settings["BindReadOnlyPaths"] in {shared_path, shared_path + ":" + shared_path,
                                            shared_path + ":" + shared_path + ":rbind"},
            "The shared package bind mount differs")
    mounts = {}
    for line in (Path("/proc") / str(identity["pid"]) / "mountinfo").read_text().splitlines():
        fields = line.split()
        mounts[fields[4]] = set(fields[5].split(","))
    def mount_options(path: str) -> set[str]:
        matches = [point for point in mounts if path == point or path.startswith(point.rstrip("/") + "/")]
        require(matches, "Fresh mount table does not cover an expected path")
        return mounts[max(matches, key=len)]
    require(all("ro" in mount_options(path) for path in READ_ONLY_PATHS) and
            all("rw" in mount_options(path) for path in (str(DATA), str(RUNTIME))),
            "Actual fresh mount permissions differ from the declared sandbox")
    require(identity["networkNamespace"] != os.readlink("/proc/1/ns/net") and
            identity["networkNamespace"] != process_identity(OLD_UNITS["goby-emby-reference.service"])["networkNamespace"],
            "Fresh service shares a protected network namespace")
    identity["privateDevicesProof"] = private_devices_proof(identity)
    identity["mountPermissions"] = {path: sorted(mount_options(path)) for path in sorted(READ_ONLY_PATHS | {str(DATA), str(RUNTIME)})}
    return identity


def same_new_identity(expected: dict) -> None:
    require(new_identity() == expected, "Fresh service identity changed")


def same(expected: dict) -> None:
    same_new_identity(expected)


def source_root_identity() -> dict:
    owned_root(SOURCE)
    entry = canonical(SOURCE, directory=True, private=True)
    return {"path": str(SOURCE), "device": entry.st_dev, "inode": entry.st_ino, "marker": MARKER}


def source_file_identity() -> dict:
    require(load(SOURCE_IDENTITY) == source_root_identity(), "Owned source root changed")
    entries = sorted(SOURCE.rglob("*"))
    require(set(entries) == {SOURCE / ".goby-managed", SOURCE_FILE.parent, SOURCE_FILE}, "Owned source membership differs")
    canonical(SOURCE_FILE.parent, directory=True, private=True)
    entry = canonical(SOURCE_FILE, private=True)
    require(0 < entry.st_size <= 32 * MIB, "Owned source exceeds its finite byte bound")
    return {"path": str(SOURCE_FILE), "bytes": entry.st_size, "sha256": digest(SOURCE_FILE),
            "device": entry.st_dev, "dev": entry.st_dev, "inode": entry.st_ino}


def verify_source(expected: dict | None = None) -> dict:
    result = source_file_identity()
    proof = load(SOURCE_VERIFICATION)
    require(proof.get("sourceIdentity") == result and proof.get("complete") is True and
            proof.get("videoCodec") == "mpeg4" and proof.get("audioCodec") == "aac" and
            proof.get("width") == 3840 and proof.get("height") == 2160 and proof.get("decodedFrames") == 8 and
            proof.get("strictDecodePassed") is True, "Owned source verification is incomplete or no longer matches")
    if expected is not None:
        require(expected == result or expected == proof, "Owned source differs from its supplied attestation")
    return result


def media_process_limits() -> None:
    resource.setrlimit(resource.RLIMIT_AS, (MEDIA_LIMITS["addressSpaceBytes"],) * 2)
    resource.setrlimit(resource.RLIMIT_CPU, (MEDIA_LIMITS["cpuSeconds"],) * 2)
    resource.setrlimit(resource.RLIMIT_FSIZE, (MEDIA_LIMITS["fileSizeBytes"],) * 2)
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    resource.setrlimit(resource.RLIMIT_NOFILE, (64, 64))


@contextmanager
def media_lease():
    canonical(PRIVATE, directory=True, private=True)
    path = PRIVATE / "media-operation.lock"
    descriptor = os.open(path, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    try:
        canonical(path, private=True)
        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield
    finally:
        os.close(descriptor)


def live_media_group_members(group_id: int) -> list[int]:
    entries = list(Path("/proc").iterdir())
    require(len(entries) <= 20000, "Process membership observation exceeds its bound")
    members = []
    for entry in entries:
        if not entry.name.isdigit():
            continue
        try:
            content = (entry / "stat").read_text()
        except FileNotFoundError:
            continue
        fields = content[content.rfind(")") + 2:].split()
        if len(fields) >= 3 and int(fields[2]) == group_id and fields[0] != "Z":
            members.append(int(entry.name))
    return sorted(members)


def terminate_media_group(child: subprocess.Popen) -> None:
    """Keep the leader unreaped until the owned process group has no live member."""
    try:
        os.killpg(child.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    deadline = time.monotonic() + 3
    while live_media_group_members(child.pid) and time.monotonic() < deadline:
        time.sleep(0.05)
    if live_media_group_members(child.pid):
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        deadline = time.monotonic() + 5
        while live_media_group_members(child.pid) and time.monotonic() < deadline:
            time.sleep(0.05)
    require(not live_media_group_members(child.pid), "The bounded owned media process group did not exit")
    child.wait(timeout=5)


def bounded_media_command(arguments: list[str], identity: dict, *, seconds: int) -> dict:
    """Run one fixed tool in the attested namespace with bounded in-memory output."""
    same(identity)
    require(arguments and arguments[0] in {str(FFMPEG), str(FFPROBE)} and
            0 < seconds <= 90 and len(arguments) < 96 and sum(len(value) for value in arguments) < 16384,
            "Media command is outside the bounded fixed toolchain")
    require(all("://" not in value and not value.startswith("//") and
                (not re.match(r"^[A-Za-z][A-Za-z0-9+.-]*:", value) or value == "pipe:1") for value in arguments[1:]),
            "Media subprocess arguments may not contain network or file URIs")
    command = ["nsenter", "-t", str(identity["pid"]), "-n", *arguments]
    started, deadline = time.monotonic(), time.monotonic() + seconds
    output = {"stdout": bytearray(), "stderr": bytearray()}
    evidence = {"arguments": arguments, "namespaceArguments": command, "timeoutSeconds": seconds,
                "limits": MEDIA_LIMITS, "networkNamespace": identity["networkNamespace"],
                "privateDevicesProof": identity["privateDevicesProof"], "startedAt": utc(),
                "complete": False, "returnCode": None, "failureType": None, "resourceSamples": []}
    child, selector = None, selectors.DefaultSelector()
    try:
        child = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                 start_new_session=True, preexec_fn=media_process_limits,
                                 env={**os.environ, "OMP_NUM_THREADS": "1", "OPENBLAS_NUM_THREADS": "1"})
        for label, stream in (("stdout", child.stdout), ("stderr", child.stderr)):
            os.set_blocking(stream.fileno(), False)
            selector.register(stream, selectors.EVENT_READ, label)
        next_check = time.monotonic()
        while selector.get_map():
            now = time.monotonic()
            require(now < deadline, "Media subprocess deadline expired")
            if now >= next_check:
                same(identity)
                evidence["resourceSamples"].append(resources())
                next_check = now + 2
            for key, _ in selector.select(min(0.25, max(0, deadline - now))):
                block = os.read(key.fileobj.fileno(), 65536)
                if not block:
                    selector.unregister(key.fileobj)
                    continue
                remaining = MEDIA_OUTPUT_LIMIT - len(output[key.data])
                output[key.data].extend(block[:remaining])
                require(len(block) <= remaining, "Media subprocess output limit exceeded")
        observed = None
        while observed is None:
            require(time.monotonic() < deadline, "Media subprocess exit deadline expired")
            observed = os.waitid(os.P_PID, child.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            if observed is None:
                time.sleep(0.05)
        evidence["returnCode"] = observed.si_status if observed.si_code == os.CLD_EXITED else -observed.si_status
        require(evidence["returnCode"] == 0, "Media subprocess failed")
        same(identity)
        evidence["complete"] = True
    except Exception as error:
        evidence["failureType"] = type(error).__name__
        evidence["failureMessage"] = str(error)
    finally:
        selector.close()
        if child is not None:
            remaining = live_media_group_members(child.pid)
            if remaining and evidence["complete"]:
                evidence.update({"complete": False, "failureType": "LingeringMediaProcessGroup", "remainingProcessIds": remaining})
            if not evidence["complete"]:
                terminate_media_group(child)
            else:
                child.wait(timeout=5)
            evidence["returnCode"] = child.returncode
            evidence["processGroupExited"] = True
            for stream in (child.stdout, child.stderr):
                if stream is not None:
                    stream.close()
        evidence.update({label: bytes(value).decode("utf-8", errors="replace") for label, value in output.items()})
        evidence["elapsedSeconds"] = round(time.monotonic() - started, 3)
    return evidence


def media_binary_evidence(identity: dict) -> dict:
    result = {}
    for tool in (FFMPEG, FFPROBE):
        entry = canonical(tool)
        require(entry.st_uid == 0 and entry.st_mode & 0o022 == 0, "Installed media binary ownership differs")
        observation = bounded_media_command([str(tool), "-version"], identity, seconds=10)
        require(observation["complete"] and observation["stdout"].splitlines() and
                observation["stdout"].splitlines()[0].startswith(tool.name + " version 9.0.1 "),
                "Installed media tool does not report version 9.0.1")
        result[tool.name] = {"path": str(tool), "sha256": digest(tool), "version": observation["stdout"].splitlines()[0],
                             "versionCommand": observation}
    if SOURCE_VERIFICATION.exists():
        prior = load(SOURCE_VERIFICATION)["binaries"]
        require(all({key: result[name][key] for key in ("path", "sha256", "version")} ==
                    {key: prior[name][key] for key in ("path", "sha256", "version")} for name in ("ffmpeg", "ffprobe")),
                "The media toolchain differs from the completely verified source toolchain")
    return result


def local_media_path(path: Path) -> dict:
    require(isinstance(path, Path) and path.is_absolute() and ROOT in path.parents,
            "Media probing accepts only files inside the fixed owned evidence root")
    owned_root(ROOT)
    for parent in path.parents:
        if parent == ROOT:
            break
        canonical(parent, directory=True, private=True)
    entry = canonical(path, private=True)
    require(0 < entry.st_size <= 64 * MIB and path.suffix.lower() == ".mp4", "Local media input exceeds its bound or format")
    return {"path": str(path), "bytes": entry.st_size, "sha256": digest(path), "device": entry.st_dev, "inode": entry.st_ino}


def _probe_local_media(path: Path, label: str, identity: dict, binaries: dict, *,
                       expected_video_codec: str | None, expected_frames: int) -> dict:
    require(re.fullmatch(r"[a-z0-9][a-z0-9-]{0,63}", label) and expected_frames == 8 and
            expected_video_codec in {None, "mpeg4", "h264"}, "Media probe expectation is outside the fixed study")
    before = local_media_path(path)
    report = {"kind": "local-media-verification", "label": label, "inputIdentity": before, "binaries": binaries,
              "limits": MEDIA_LIMITS, "complete": False, "strictDecodePassed": False}
    try:
        probe = bounded_media_command([str(FFPROBE), "-hide_banner", "-v", "error", "-threads", "1",
                "-protocol_whitelist", "file,pipe", "-count_frames", "-show_streams", "-show_format", "-of", "json", str(path)],
                identity, seconds=60)
        report["ffprobeCommand"] = probe
        require(probe["complete"], "Bounded local ffprobe failed")
        parsed = json.loads(probe["stdout"])
        report["ffprobe"] = parsed
        require(isinstance(parsed, dict) and isinstance(parsed.get("streams"), list), "Local ffprobe stream shape differs")
        videos = [row for row in parsed["streams"] if row.get("codec_type") == "video"]
        audios = [row for row in parsed["streams"] if row.get("codec_type") == "audio"]
        require(len(videos) == len(audios) == 1 and len(parsed["streams"]) == 2,
                "The bounded fixture must have exactly one video and one audio stream")
        video, audio = videos[0], audios[0]
        require(type(video.get("width")) is int and type(video.get("height")) is int and
                video["width"] > 0 and video["height"] > 0, "Local media dimensions are invalid")
        require(int(video.get("nb_read_frames", "0")) == expected_frames and audio.get("codec_name") == "aac" and
                (expected_video_codec is None or video.get("codec_name") == expected_video_codec),
                "The local media codec or completely counted frame result differs")
        decode = bounded_media_command([str(FFMPEG), "-hide_banner", "-loglevel", "error", "-nostdin", "-xerror",
                "-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-protocol_whitelist", "file,pipe",
                "-i", str(path), "-map", "0:v:0", "-map", "0:a:0", "-threads", "1", "-progress", "pipe:1", "-nostats", "-f", "null", "-"],
                identity, seconds=90)
        report["ffmpegDecodeCommand"] = decode
        frames = re.findall(r"(?m)^frame=\s*(\d+)\s*$", decode["stdout"])
        require(decode["complete"] and decode["returnCode"] == 0 and "progress=end" in decode["stdout"] and
                frames and int(frames[-1]) == expected_frames, "Strict full decode did not finish all expected video frames")
        require(local_media_path(path) == before, "Local media changed during complete probe and decode")
        report.update({"complete": True, "videoCodec": video["codec_name"], "audioCodec": audio["codec_name"],
                       "width": video["width"], "height": video["height"], "decodedFrames": int(frames[-1]),
                       "strictDecodePassed": True})
    except Exception as error:
        report.update({"failureType": type(error).__name__, "failureMessage": str(error)})
    save(PRIVATE / ("media-" + label + ".json"), report)
    save(MEDIA_EXPORT / ("media-" + label + ".json"), configuration_sanitizer().sanitize(report))
    require(report["complete"], "Local media verification failed; inspect retained bounded evidence")
    return report


def probe_local_media(path: Path, label: str, expected_video_codec: str | None = None, expected_frames: int = 8) -> dict:
    identity = load(IDENTITY)
    with media_lease():
        binaries = media_binary_evidence(identity)
        return _probe_local_media(path, label, identity, binaries,
                                  expected_video_codec=expected_video_codec, expected_frames=expected_frames)


def generate_source(identity: dict) -> dict:
    owned_roots()
    require(set(SOURCE.iterdir()) == {SOURCE / ".goby-managed"}, "Source generation root is not newly empty")
    SOURCE_FILE.parent.mkdir(mode=0o700)
    arguments = [str(FFMPEG), "-hide_banner", "-loglevel", "error", "-nostdin", "-filter_threads", "1",
                 "-filter_complex_threads", "1", "-f", "lavfi", "-i", "testsrc2=size=3840x2160:rate=2",
                 "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "4", "-c:v", "mpeg4",
                 "-q:v", "2", "-bf", "0", "-g", "2", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "64k",
                 "-ac", "1", "-ar", "48000", "-threads", "1", "-movflags", "+faststart", "-n", str(SOURCE_FILE)]
    with media_lease():
        binaries = media_binary_evidence(identity)
        generation = bounded_media_command(arguments, identity, seconds=90)
        generation_record = {"kind": "owned-synthetic-source-generation", "binaries": binaries, "generation": generation,
                             "sourceRootIdentity": load(SOURCE_IDENTITY), "oldInputsRead": False, "mediaCopiesCreated": 0}
        save(PRIVATE / "source-generation.json", generation_record)
        save(MEDIA_EXPORT / "source-generation.json", configuration_sanitizer().sanitize(generation_record))
        require(generation["complete"], "Owned source generation failed; no alternate input is permitted")
        result = _probe_local_media(SOURCE_FILE, "source", identity, binaries, expected_video_codec="mpeg4", expected_frames=8)
    require(result["width"] == 3840 and result["height"] == 2160 and
            result["ffprobe"]["streams"][0].get("r_frame_rate") == "2/1",
            "The generated source dimensions or frame rate differ")
    source_audio = next(row for row in result["ffprobe"]["streams"] if row["codec_type"] == "audio")
    require(source_audio.get("channels") == 1 and source_audio.get("sample_rate") == "48000" and
            abs(float(result["ffprobe"]["format"].get("duration", "0")) - 4.0) <= 0.05,
            "The generated source mono audio rate or four-second duration differs")
    proof = {**result, "sourceIdentity": source_file_identity(), "sourceRootIdentity": load(SOURCE_IDENTITY)}
    atomic_save(SOURCE_VERIFICATION, proof)
    verify_source(proof)
    return proof


def deadline_expired(_signum: int, _frame: object) -> None:
    raise TimeoutError("The bounded fresh bootstrap request expired")


def interrupted(_signum: int, _frame: object) -> None:
    raise InterruptedError("The fresh reference operator received a termination signal")


class Bootstrap:
    def __init__(self, identity: dict) -> None:
        self.identity = identity
        self.tokens: dict[str, str] = {}
        self.secrets: set[str] = set()
        self.redactor = configuration_sanitizer()
        self.redactor.secrets = self.secrets
        self.count, self.wire = 0, 0
        self.complete_count, self.incomplete_count = 0, 0
        self.server_id: str | None = None
        self.deadline = time.monotonic() + 180
        self.revoked: set[str] = set()
        self.login_attempted = False
        self.prefix = PREFIX
        self.persistence_failures: list[dict] = []
        self.cleanup_count, self.cleanup_wire = 0, 0

    def sanitize(self, value: object, field: str = "") -> object:
        # Collect first so a diagnostic echo that precedes its secret-bearing
        # header/field in the response is still redacted before export.
        self.redactor.collect_secrets(value, field)
        result = self.redactor.sanitize(value, field)
        text = json.dumps(result, ensure_ascii=False)
        require(not any(secret and secret in text for secret in self.secrets), "A known bootstrap secret survived deep export")
        return result

    @staticmethod
    def metadata(account: str) -> str:
        require(account == "control", "Only the reserved ordinary control identity is allowed")
        return ('Emby Client="' + CONTROL_CLIENT + '", Device="Linux Fresh Test", '
                'DeviceId="' + CONTROL_DEVICE + '", Version="0.1.0"')

    def request(self, label: str, method: str, route: str, *, body: object = None, token: str = "",
                account: str = "", form: bool = False, retry_readiness: bool = False, cleanup: bool = False) -> tuple[int | None, object]:
        require(time.monotonic() < self.deadline and (self.cleanup_count < 4 if cleanup else self.count < MAX_REQUESTS),
                "Fresh bootstrap request budget exhausted")
        if cleanup:
            require((method, route) in {("GET", "/emby/Sessions"), ("POST", "/emby/Sessions/Logout")},
                    "The independent cleanup reserve only permits control invalidation")
            self.cleanup_count += 1
        same_new_identity(self.identity)
        require(os.readlink("/proc/self/ns/net") == self.identity["networkNamespace"], "HTTP is outside the fresh namespace")
        routes = {"GET": {"/emby/System/Info/Public", "/emby/Startup/User", "/emby/Users", "/emby/Devices", "/emby/System/Configuration",
                           "/emby/Library/VirtualFolders/Query", "/emby/Sessions"},
                  "POST": {"/emby/Startup/User", "/emby/Startup/RemoteAccess", "/emby/Startup/Complete",
                            "/emby/Users/AuthenticateByName", "/emby/Sessions/Logout"}}
        require(route in routes.get(method, set()), "Bootstrap route is outside the exact setup allowlist")
        if route == "/emby/Users/AuthenticateByName":
            require(method == "POST" and not self.login_attempted and not CONTROL_LOGIN.exists() and not CONTROL_LOGIN_INTENT.exists(),
                    "The one ordinary login has already been attempted")
            self.login_attempted = True
            atomic_save(CONTROL_LOGIN_INTENT, {"at": utc(), "serverId": self.server_id,
                        "controlClient": CONTROL_CLIENT, "controlDeviceId": CONTROL_DEVICE,
                        "serviceIdentity": self.identity, "ordinaryLoginAttempt": 1})
        if method != "GET":
            require(self.server_id and self.server_id != OLD_SERVER_ID, "Mutation requires a distinct proven fresh server identity")
        headers = {"Accept": "application/json"}
        if account:
            headers["Authorization"] = self.metadata(account)
        if token:
            require(token in self.tokens.values(), "Bootstrap HTTP token is not owned")
            headers["X-Emby-Token"] = token
        if form:
            require(isinstance(body, dict), "Bootstrap form body differs")
            wire_body = urlencode(body).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
            recorded_body = wire_body.decode()
        elif body is not None:
            wire_body = json.dumps(body, separators=(",", ":")).encode()
            headers["Content-Type"] = "application/json"
            recorded_body = body
        else:
            wire_body, recorded_body = None, None
        self.count += 1
        name = self.prefix + str(self.count).zfill(3) + "-" + label
        connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=5)
        status_code, response_headers, content, parsed, kind, failure = None, [], b"", None, "incomplete", None
        complete = False
        started = utc()
        try:
            signal.setitimer(signal.ITIMER_REAL, min(15, self.deadline - time.monotonic()))
            connection.request(method, route, body=wire_body, headers=headers)
            response = connection.getresponse()
            status_code, response_headers = response.status, response.getheaders()
            content = response.read(MAX_BODY + 1)
            require(len(content) <= MAX_BODY, "Bootstrap response exceeds its bounded capture")
            declared_lengths = [value for key, value in response_headers if key.lower() == "content-length"]
            if declared_lengths:
                require(len(set(declared_lengths)) == 1 and declared_lengths[0].isdigit() and
                        int(declared_lengths[0]) == len(content), "Bootstrap response Content-Length was not satisfied")
            require(response.read(1) == b"", "Bootstrap response was not completely consumed")
            text = content.decode("utf-8", errors="strict")
            try:
                parsed, kind = json.loads(text), "json"
            except json.JSONDecodeError:
                parsed, kind = text, "text"
            complete = True
        except Exception as error:
            failure = type(error).__name__
        finally:
            signal.setitimer(signal.ITIMER_REAL, 0)
            connection.close()
        self.wire += len(content)
        if cleanup:
            self.cleanup_wire += len(content)
        self.complete_count += int(complete)
        self.incomplete_count += int(not complete)
        # Acknowledgement precedes all DTO checks so failed assertions retain
        # the exact newly issued credential and its cleanup responsibility.
        if isinstance(parsed, dict) and isinstance(parsed.get("AccessToken"), str) and parsed["AccessToken"]:
            require(account == "control" and route == "/emby/Users/AuthenticateByName", "Unexpected bootstrap credential response")
            self.tokens[account] = parsed["AccessToken"]
            self.secrets.add(parsed["AccessToken"])
            atomic_save(CONTROL_LOGIN, parsed)
        record = {"reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": started},
                  "request": {"method": method, "path": route, "headers": list(headers.items()), "body": recorded_body},
                  "response": {"status": status_code, "headers": response_headers, "bodyType": kind, "body": parsed},
                  "observation": {"completeHTTP": complete, "captureIncomplete": not complete,
                                  "failureType": failure, "wireBytes": len(content), "freshFixtureOnly": True}}
        for path, factory in ((RAW / (name + ".json"), lambda: record),
                              (EXPORT / (name + ".json"), lambda: self.sanitize(record)),
                              (PRIVATE / "wire" / (name + ".b64"), lambda: base64.b64encode(content).decode() + "\n")):
            try:
                save(path, factory())
            except Exception as error:
                self.persistence_failures.append({"path": str(path), "failureType": type(error).__name__})
                if not cleanup:
                    raise
        self.last_record = name
        if not cleanup:
            print(json.dumps({"stage": label, "status": status_code, "completeHTTP": complete, "record": self.count}), flush=True)
        require(self.cleanup_wire <= MIB if cleanup else self.wire <= MAX_WIRE, "Bootstrap wire-byte budget exhausted")
        if not retry_readiness:
            require(complete, "Fresh bootstrap HTTP response was incomplete")
        return status_code, parsed

    def logout(self, account: str) -> None:
        token = self.tokens[account]
        status_code, _ = self.request(account + "-live-state", "GET", "/emby/Sessions", token=token, account="control", cleanup=True)
        require(status_code in {200, 401}, "Fresh control credential state is unavailable")
        if status_code == 200:
            status_code, _ = self.request(account + "-logout", "POST", "/emby/Sessions/Logout", token=token, account="control", cleanup=True)
            require(status_code == 204, "Fresh control logout status differs from the recorded contract")
        status_code, _ = self.request(account + "-logout-invalid", "GET", "/emby/Sessions", token=token, account="control", cleanup=True)
        require(status_code == 401, "Fresh bootstrap credential invalidity is unproven")
        self.revoked.add(account)

    def capture(self) -> dict:
        readiness_deadline = time.monotonic() + 90
        while True:
            status_code, public = self.request("readiness-public", "GET", "/emby/System/Info/Public", retry_readiness=True)
            if status_code == 200:
                break
            require(time.monotonic() < readiness_deadline, "Fresh reference readiness deadline expired")
            time.sleep(1)
        require(isinstance(public, dict) and public.get("Version") == "4.9.5.0" and public.get("ServerName") == SERVER_NAME and
                isinstance(public.get("Id"), str) and public["Id"] and public["Id"] not in {OLD_SERVER_ID, PREVIOUS_SERVER_ID},
                "Fresh public identity is wrong or collides with a protected reference")
        self.server_id = public["Id"]
        save(PRIVATE / "fresh-public-identity.json", public)
        password = secrets.token_hex(32)
        self.secrets.add(password)
        save(PRIVATE / "control-account-secret.json", {"Username": ADMIN_NAME, "Password": password})
        status_code, startup = self.request("startup-user-get", "GET", "/emby/Startup/User", account="control")
        require(status_code == 200 and isinstance(startup, dict) and isinstance(startup.get("Name"), str), "Fresh startup user is unavailable")
        status_code, result = self.request("startup-user-post", "POST", "/emby/Startup/User", account="control", form=True,
                                           body={"Name": ADMIN_NAME, "Password": password})
        require(status_code == 200 and result == {}, "Fresh startup account creation differs")
        status_code, _ = self.request("startup-remote-access", "POST", "/emby/Startup/RemoteAccess", account="control", form=True,
                                      body={"EnableAutomaticPortMapping": "false"})
        require(status_code == 204, "Fresh startup remote-access configuration differs")
        status_code, _ = self.request("startup-complete", "POST", "/emby/Startup/Complete", account="control")
        require(status_code == 204, "Fresh startup completion differs")
        status_code, control = self.request("control-login", "POST", "/emby/Users/AuthenticateByName", account="control",
                                            body={"Username": ADMIN_NAME, "Pw": password})
        require(status_code == 200 and "control" in self.tokens and isinstance(control, dict) and
                control.get("ServerId") == self.server_id and control.get("User", {}).get("Name") == ADMIN_NAME and
                isinstance(control["User"].get("Id"), str) and control["User"]["Id"] and
                control["User"].get("Policy", {}).get("IsAdministrator") is True,
                "Fresh control login or administrator role differs")
        token, user_id = self.tokens["control"], control["User"]["Id"]
        session = control.get("SessionInfo", {})
        require(isinstance(session, dict) and session.get("DeviceId") == CONTROL_DEVICE and
                session.get("Client") == CONTROL_CLIENT and session.get("UserId") == user_id,
                "The acknowledged ordinary login does not own the reserved control session")
        credentials = {"REFERENCE_USERNAME": ADMIN_NAME, "REFERENCE_PASSWORD": password, "REFERENCE_TOKEN": token,
                       "REFERENCE_USER_ID": user_id, "REFERENCE_DEVICE_ID": CONTROL_DEVICE}
        require(all(isinstance(value, str) and value and "\n" not in value and "\r" not in value for value in credentials.values()),
                "Control credential fields are not single-line private values")
        atomic_save(CONTROL_CREDENTIALS, "".join(name + "=" + value + "\n" for name, value in credentials.items()))
        status_code, users = self.request("bootstrap-final-users", "GET", "/emby/Users", token=token, account="control")
        require(status_code == 200 and isinstance(users, list) and len(users) == 1 and users[0].get("Id") == user_id and
                users[0].get("Name") == ADMIN_NAME, "Fresh final user membership is not exactly the ordinary control administrator")
        status_code, devices = self.request("bootstrap-final-devices", "GET", "/emby/Devices", token=token, account="control")
        require(status_code == 200 and isinstance(devices, dict) and isinstance(devices.get("Items"), list) and
                len(devices["Items"]) == 1 and devices["Items"][0].get("ReportedDeviceId") == CONTROL_DEVICE and
                devices["Items"][0].get("AppName") == CONTROL_CLIENT and devices["Items"][0].get("LastUserId") == user_id and
                str(session.get("InternalDeviceId")) == devices["Items"][0].get("Id"),
                "Fresh final device membership differs from the one control device")
        status_code, configuration = self.request("bootstrap-safe-configuration", "GET", "/emby/System/Configuration", token=token, account="control")
        require(status_code == 200 and isinstance(configuration, dict), "Fresh safe configuration is unavailable")
        for name in ("EnableHttps", "EnableUPnP", "EnableRemoteAccess", "EnableAutoUpdate", "EnableAutomaticRestart", "AutoRunWebApp"):
            require(configuration.get(name) is False, "Fresh configuration enabled an unsafe startup capability")
        require(configuration.get("HttpServerPortNumber") == PORT and configuration.get("PublicPort") == PORT and
                configuration.get("HttpsPortNumber") == HTTPS_PORT and configuration.get("PublicHttpsPort") == HTTPS_PORT and
                configuration.get("ServerName") == SERVER_NAME and configuration.get("LocalNetworkAddresses") == ["127.0.0.1"] and
                configuration.get("IsStartupWizardCompleted") is True, "Fresh loopback ports, name or completed setup differs")
        configuration_record = str(RAW / (self.last_record + ".json"))
        status_code, libraries = self.request("bootstrap-final-libraries", "GET", "/emby/Library/VirtualFolders/Query", token=token, account="control")
        require(status_code == 200 and isinstance(libraries, dict) and libraries.get("Items") == [],
                "Fresh bootstrap unexpectedly contains a media library")
        status_code, sessions = self.request("control-live-handoff", "GET", "/emby/Sessions", token=token, account="control")
        require(status_code == 200 and isinstance(sessions, list) and
                any(row.get("DeviceId") == CONTROL_DEVICE and row.get("UserId") == user_id for row in sessions),
                "The exact ordinary control login is not live at handoff")
        for raw in sorted(RAW.glob(PREFIX + "*.json")):
            require(self.sanitize(load(raw)) == load(EXPORT / raw.name), "Fresh setup export redaction audit failed")
        return {"serverId": self.server_id, "bootstrapDevices": devices["Items"], "bootstrapUsers": users,
                "bootstrapLibraries": libraries["Items"], "bootstrapLibraryMutationRequests": 0,
                "bootstrapConfigurationRecord": configuration_record, "bootstrapConfigurationSafetyVerified": True,
                "bootstrapConfigurationMutationRequests": 0, "bootstrapNamedConfigurationRequests": 0,
                "bootstrapTaskRequests": 0, "bootstrapTaskMutationRequests": 0, "bootstrapApplicationKeyRequests": 0,
                "ordinaryLoginCount": 1, "controlCredentialState": "LIVE_HANDOFF", "controlUserId": user_id,
                "controlDeviceId": CONTROL_DEVICE, "controlClient": CONTROL_CLIENT,
                "controlTokenSha256": hashlib.sha256(token.encode()).hexdigest(),
                "controlCredentialsFile": str(CONTROL_CREDENTIALS), "controlLoginResponseFile": str(CONTROL_LOGIN),
                "controlCredentialsSha256": digest(CONTROL_CREDENTIALS), "controlLoginResponseSha256": digest(CONTROL_LOGIN),
                "setupRecordCount": self.count, "setupCompleteHTTPResponses": self.complete_count,
                "setupIncompleteAttempts": self.incomplete_count, "wireBytes": self.wire}


def bootstrap() -> None:
    common_preconditions(host=False)
    owned_roots()
    intent, identity = load(INTENT), load(IDENTITY)
    require(intent.get("marker") == MARKER and intent.get("operatorSha256") == digest(Path(__file__).resolve()) and
            intent.get("sanitizerSources") == SANITIZER_SOURCES, "Fresh bootstrap intent or sanitizer provenance differs")
    same(identity)
    verify_source()
    require(os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "Bootstrap must enter the attested fresh namespace")
    signal.signal(signal.SIGALRM, deadline_expired)
    recorder = Bootstrap(identity)
    try:
        result = recorder.capture()
        save(PRIVATE / "bootstrap-result.json", result)
    except Exception as error:
        revocation = {"acknowledged": "control" in recorder.tokens, "invalidityProven": False}
        if "control" in recorder.tokens:
            try:
                recorder.deadline = time.monotonic() + 60
                recorder.logout("control")
                revocation["invalidityProven"] = True
            except Exception as revoke_error:
                revocation["failureType"] = type(revoke_error).__name__
        save(PRIVATE / "bootstrap-failure.json", {"failureType": type(error).__name__, "message": str(error),
             "records": recorder.count, "ordinaryLoginAttempted": recorder.login_attempted, "controlRevocation": revocation,
             "persistenceFailures": recorder.persistence_failures})
        raise


def revoke_control() -> None:
    common_preconditions(host=False)
    owned_roots()
    identity = load(IDENTITY)
    same(identity)
    require(os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "Control cleanup escaped the fresh namespace")
    login = load(CONTROL_LOGIN)
    login_intent = load(CONTROL_LOGIN_INTENT)
    token = login.get("AccessToken")
    require(isinstance(token, str) and token and login_intent.get("serviceIdentity") == identity and
            login_intent.get("serverId") == load(PRIVATE / "fresh-public-identity.json")["Id"] and
            login_intent.get("controlClient") == CONTROL_CLIENT and login_intent.get("controlDeviceId") == CONTROL_DEVICE,
            "Control cleanup has no acknowledged fresh token")
    if MANIFEST.exists():
        manifest = load(MANIFEST)
        require(manifest.get("controlTokenSha256") == hashlib.sha256(token.encode()).hexdigest() and
                manifest.get("controlCredentialState") == "LIVE_HANDOFF" and manifest.get("ordinaryLoginCount") == 1,
                "Control cleanup credential differs from the exact handoff")
    attempt = next((index for index in range(1, 100) if not (PRIVATE / ("control-revocation-" + str(index) + ".json")).exists()), None)
    require(attempt is not None, "Control revocation attempt budget exhausted")
    recorder = Bootstrap(identity)
    recorder.prefix = "encoding-width-fresh-operator-cleanup-m5h-2-" + str(attempt) + "-"
    recorder.deadline = time.monotonic() + 60
    # Revocation responsibility survives later authentication DTO failures;
    # the durable request intent identifies the server that issued the token.
    recorder.server_id, recorder.tokens = login_intent["serverId"], {"control": token}
    recorder.secrets.add(token)
    if (PRIVATE / "control-account-secret.json").exists():
        recorder.secrets.add(load(PRIVATE / "control-account-secret.json")["Password"])
    signal.signal(signal.SIGALRM, deadline_expired)
    report = {"at": utc(), "attempt": attempt, "controlTokenSha256": hashlib.sha256(token.encode()).hexdigest(),
              "invalidityProven": False, "ordinaryLoginsIssued": 0}
    try:
        recorder.logout("control")
        report.update({"invalidityProven": True, "invalidityStatus": 401})
    except Exception as error:
        report.update({"failureType": type(error).__name__, "failureMessage": str(error)})
    report["persistenceFailures"] = recorder.persistence_failures
    report["persistenceComplete"] = not recorder.persistence_failures
    save(PRIVATE / ("control-revocation-" + str(attempt) + ".json"), report)
    require(report["invalidityProven"] and report["persistenceComplete"], "The one control credential invalidity or evidence is incomplete")


def revoke_via_namespace(identity: dict | None) -> dict:
    if not CONTROL_LOGIN.exists():
        if CONTROL_LOGIN_INTENT.exists():
            return {"acknowledged": False, "invalidityProven": False, "unresolvedLoginAttempt": True,
                    "failureType": "LoginIntentWithoutDurableAcknowledgement"}
        return {"acknowledged": False, "invalidityProven": False, "notApplicable": True}
    if identity is None:
        return {"acknowledged": True, "invalidityProven": False, "failureType": "MissingServiceIdentity"}
    same(identity)
    before = set(PRIVATE.glob("control-revocation-*.json"))
    try:
        run(["nsenter", "-t", str(identity["pid"]), "-n", "python3", "-B", str(Path(__file__).resolve()), "_revoke"], timeout=75)
    except Exception as error:
        failure = type(error).__name__
    else:
        failure = None
    created = set(PRIVATE.glob("control-revocation-*.json")) - before
    if len(created) == 1:
        return {"acknowledged": True, **load(created.pop())}
    return {"acknowledged": True, "invalidityProven": False, "failureType": failure or "MissingRevocationEvidence"}

def write_configuration() -> None:
    save(DATA / "config/system.xml", f'''<?xml version="1.0" encoding="utf-8"?>
<ServerConfiguration xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <HttpServerPortNumber>18100</HttpServerPortNumber><PublicPort>18100</PublicPort>
  <HttpsPortNumber>18500</HttpsPortNumber><PublicHttpsPort>18500</PublicHttpsPort>
  <EnableHttps>false</EnableHttps><EnableUPnP>false</EnableUPnP><EnableRemoteAccess>false</EnableRemoteAccess>
  <EnableAutoUpdate>false</EnableAutoUpdate><EnableAutomaticRestart>false</EnableAutomaticRestart>
  <AutoRunWebApp>false</AutoRunWebApp><IsStartupWizardCompleted>false</IsStartupWizardCompleted>
  <EnableExternalContentInSuggestions>false</EnableExternalContentInSuggestions>
  <ServerName>{SERVER_NAME}</ServerName>
  <LocalNetworkAddresses><string>127.0.0.1</string></LocalNetworkAddresses>
  <PreferredMetadataLanguage>en</PreferredMetadataLanguage><MetadataCountryCode>US</MetadataCountryCode><UICulture>en-US</UICulture>
  <DatabaseCacheSizeMB>64</DatabaseCacheSizeMB><LogFileRetentionDays>2</LogFileRetentionDays>
</ServerConfiguration>
''')
    save(RUNTIME / "launch.sh", '''#!/usr/bin/env bash
set -euo pipefail
APP_DIR=/dev/shm/goby-emby-reference/package/opt/emby-server
EMBY_DATA=<OWNED_PROGRAM_DATA>
export EMBY_DATA
export AMDGPU_IDS="$APP_DIR/extra/share/libdrm/amdgpu.ids"
export FONTCONFIG_PATH="$APP_DIR/etc/fonts"
export LD_LIBRARY_PATH="$APP_DIR/lib:$APP_DIR/extra/lib"
export LIBVA_DRIVERS_PATH="$APP_DIR/extra/lib/dri"
export OCL_ICD_VENDORS="$APP_DIR/extra/etc/OpenCL/vendors"
export PATH="$APP_DIR/bin:$PATH"
export PCI_IDS_PATH="$APP_DIR/share/hwdata/pci.ids"
export SSL_CERT_FILE="$APP_DIR/etc/ssl/certs/ca-certificates.crt"
export XDG_CACHE_HOME="$EMBY_DATA/cache"
export NEOReadDebugKeys=1
export OverrideGpuAddressSpace=48
# The vendor ELF interpreter is relative to the shared application directory.
cd "$APP_DIR"
exec "$APP_DIR/system/EmbyServer" -programdata "$EMBY_DATA" \\
  -ffdetect "$APP_DIR/bin/ffdetect" -ffmpeg "$APP_DIR/bin/ffmpeg" -ffprobe "$APP_DIR/bin/ffprobe" \\
  -restartexitcode 3 -updatepackage 'emby-server-deb_{version}_amd64.deb'
'''.replace("<OWNED_PROGRAM_DATA>", str(DATA)), mode=0o700)
    save(RUNTIME / "service.log", "")


def start_service() -> None:
    settings = {"Description": DESCRIPTION, "PrivateNetwork": "yes", "PrivateTmp": "yes", "PrivateDevices": "yes", "NoNewPrivileges": "yes",
                "ProtectSystem": "strict", "ProtectHome": "yes", "ReadWritePaths": str(DATA) + " " + str(RUNTIME),
                "ReadOnlyPaths": " ".join(sorted(READ_ONLY_PATHS)),
                "BindReadOnlyPaths": str(PACKAGE_ROOT),
                "CPUQuota": "150%", "MemoryMax": "1152M", "TasksMax": "256", "LimitNOFILE": "65536",
                "TimeoutStopSec": "25", "KillMode": "control-group", "WorkingDirectory": str(RUNTIME), "UMask": "0077",
                "StandardOutput": "append:" + str(RUNTIME / "service.log"), "StandardError": "append:" + str(RUNTIME / "service.log")}
    run(["systemd-run", "--unit=" + UNIT, "--collect", *["--property=" + key + "=" + value for key, value in settings.items()],
         str(RUNTIME / "launch.sh")])


def dispatch_identity(state: dict | None = None) -> dict:
    """Attest unit ownership before the final Emby process becomes ready."""
    owned_authority()
    intent = load(INTENT)
    launcher = load(PRIVATE / "launcher-manifest.json")
    require(intent.get("marker") == MARKER and launcher.get("sha256") == digest(RUNTIME / "launch.sh"),
            "Owned launcher provenance differs")
    current = properties(UNIT) if state is None else state
    launch_matches = re.findall(r"(?:^|[ {;])path=([^;]+?)\s*;", current.get("ExecStart", ""))
    require(current.get("Id") == UNIT and current.get("Description") == DESCRIPTION and
            current.get("WorkingDirectory") == str(RUNTIME) and
            launch_matches == [str(RUNTIME / "launch.sh")] and
            current.get("ControlGroup") == "/system.slice/" + UNIT and
            re.fullmatch(r"[0-9a-f]{32}", current.get("InvocationID", "")),
            "The dispatched unit does not identify this operation's launcher and cgroup")
    return {"unit": UNIT, "invocationId": current["InvocationID"], "controlGroup": current["ControlGroup"],
            "launcherSha256": launcher["sha256"], "programData": str(DATA),
            "serviceProperties": {name: current.get(name, "") for name in STATIC_PROPERTIES if name != "ExecStart"}}


def dispatched_process(state: dict, expected: dict | None) -> dict | None:
    pid = int(state.get("MainPID", "0"))
    if pid <= 1:
        return None
    current = process_identity(pid)
    require(current["uid"] == 0 and any(line.endswith(":/system.slice/" + UNIT) for line in current["cgroup"].splitlines()),
            "Dispatched process is outside the owned unit cgroup")
    if expected is not None:
        require(current["pid"] == expected["pid"] and current["startTicks"] == expected["startTicks"],
                "Refusing to stop a replacement process")
    args = current["cmdline"]
    final_process = current["exe"] == str(APP / "system/EmbyServer") and args.count("-programdata") == 1 and \
        args.index("-programdata") + 1 < len(args) and args[args.index("-programdata") + 1] == str(DATA)
    launcher_process = current["exe"] in {"/usr/bin/bash", "/bin/bash", "/usr/bin/env"} and \
        str(RUNTIME / "launch.sh") in args and len(args) <= 4
    require(final_process or launcher_process, "Dispatched process is neither the owned launcher nor its Emby child")
    return current


def stop_owned(expected: dict | None) -> dict:
    owned_authority()
    state = properties(UNIT)
    pid = int(state.get("MainPID", "0"))
    if state.get("LoadState") != "not-found" and (pid > 1 or state.get("ActiveState") in {"active", "activating", "deactivating"}):
        current_dispatch = dispatch_identity(state)
        if DISPATCH.exists():
            require(current_dispatch == load(DISPATCH), "Refusing to stop a replacement unit invocation")
        else:
            # A successful dispatch may precede a timeout/transport failure in
            # systemd-run. Exact owned launcher/cgroup attribution permits
            # recovery without requiring final application readiness.
            try:
                atomic_save(DISPATCH, current_dispatch)
            except Exception:
                # Inability to persist a recovery observation must not keep
                # the exactly attributed launcher/cgroup running. Its durable
                # setup intent and launcher hash already prove ownership.
                pass
        current = dispatched_process(state, expected)
        expected = current if current is not None else expected
        run(["systemctl", "stop", UNIT], timeout=35)
    else:
        require(state.get("LoadState") == "not-found" or state.get("Description") == DESCRIPTION,
                "Refusing to stop an unrelated inactive unit")
    if expected is not None:
        try:
            remaining = process_identity(expected["pid"])
        except FileNotFoundError:
            remaining = None
        require(remaining is None or remaining["startTicks"] != expected["startTicks"], "Owned fresh process did not exit")
    final = properties(UNIT)
    require(int(final.get("MainPID", "0")) == 0 and final.get("ActiveState") not in {"active", "activating", "deactivating"},
            "Fresh unit remains active or is still transitioning")
    cgroup_processes = Path("/sys/fs/cgroup/system.slice") / UNIT / "cgroup.procs"
    require(not cgroup_processes.exists() or cgroup_processes.read_text().strip() == "", "An owned producer remains in the service cgroup")
    return {"stopped": True, "ownedProcessGone": True, "ownedCgroupEmpty": True,
            "unitMainPID": 0, "finalActiveState": final.get("ActiveState")}


def listener_observation(identity: dict) -> list[str]:
    same_new_identity(identity)
    interfaces = json.loads(run(["nsenter", "-t", str(identity["pid"]), "-n", "ip", "-j", "address", "show"], timeout=10))
    require(isinstance(interfaces, list) and len(interfaces) == 1 and interfaces[0].get("ifname") == "lo" and
            all(entry.get("local") in {"127.0.0.1", "::1"} for entry in interfaces[0].get("addr_info", [])),
            "Fresh service network namespace is not loopback-only")
    response = run(["nsenter", "-t", str(identity["pid"]), "-n", "ss", "-H", "-ltn"], timeout=10)
    listeners = []
    for line in response.splitlines():
        columns = line.split()
        require(len(columns) >= 5, "Fresh listener observation has an unexpected shape")
        address = columns[3]
        host, separator, port = address.rpartition(":")
        # A wildcard bind remains loopback-only in the already attested
        # namespace, which has no other interfaces or host-network membership.
        require(separator and host.strip("[]") in {"127.0.0.1", "::1", "0.0.0.0", "::", "*"} and port in {str(PORT), str(HTTPS_PORT)},
                "Fresh service exposed a TCP listener outside its isolated allowed ports")
        listeners.append(address)
    require(any(value.rpartition(":")[2] == str(PORT) for value in listeners), "Fresh loopback HTTP listener is missing")
    same_new_identity(identity)
    return sorted(listeners)


def data_identity() -> dict:
    owned_root(DATA)
    info = canonical(DATA, directory=True, private=True)
    return {"path": str(DATA), "device": info.st_dev, "inode": info.st_ino, "marker": MARKER}


def prepare() -> None:
    ensure_work_parent()
    common_preconditions(host=True)
    verify_sanitizer_sources()
    require(not ROOT.exists() and not ROOT.is_symlink() and not DATA.exists() and not DATA.is_symlink(),
            "Fresh width fixture paths already exist")
    require(properties(UNIT).get("LoadState") == "not-found", "Fresh width fixture unit already exists")
    before_services, before_files = old_services(), old_baseline()
    initial_resources = resources(admission=True)
    os.umask(0o077)
    intent = {"schemaVersion": 1, "state": "PREPARING", "fixtureAttempt": FIXTURE_ATTEMPT,
              "marker": MARKER, "unit": UNIT, "programData": str(DATA),
              "evidenceRoot": str(ROOT), "sourceRoot": str(SOURCE), "sourcePath": str(SOURCE_FILE),
              "port": PORT, "httpsPort": HTTPS_PORT, "startedAt": utc(),
              "operatorSha256": digest(Path(__file__).resolve()), "sanitizerSources": SANITIZER_SOURCES,
              "oldServices": before_services, "oldBaseline": before_files,
              "failedFixtureProvenance": before_files["failedFixtureProvenance"], "initialResources": initial_resources,
              "packageSha256": PACKAGE_SHA256, "sharedServerBinarySha256": digest(APP / "system/EmbyServer"),
              "mediaCopiesCreated": 0, "configurationMutationPhase": "Separate width recorder only",
              "ordinaryLoginBudget": 1, "sourceGenerationCount": 1, "mediaLimits": MEDIA_LIMITS,
              "limits": {"serviceMemoryBytes": 1152 * MIB, "admissionAvailableMemoryBytes": 1920 * MIB,
                         "admissionPersistentFreeBytes": 2048 * MIB, "minimumAvailableMemoryBytes": 512 * MIB,
                         "dataGrowthBytes": 512 * MIB, "evidenceGrowthBytes": 256 * MIB, "minimumPersistentFreeBytes": 512 * MIB,
                         "httpAttempts": MAX_REQUESTS, "singleResponseBytes": MAX_BODY, "totalResponseBytes": MAX_WIRE,
                         "readinessSeconds": 90, "bootstrapHTTPSeconds": 180, "supervisorSeconds": 210}}
    identity, child, started_service = None, None, False
    samples: list[dict] = []
    try:
        create_owned_root(ROOT)
        PRIVATE.mkdir(mode=0o700)
        # Full old-state and fixture authority are durable before DATA or a
        # service can exist. DATA gets a separate immutable inode attestation.
        atomic_save(INTENT, intent)
        for folder in (RAW, PRIVATE / "wire", EXPORT, MEDIA_EXPORT, RUNTIME):
            folder.mkdir(mode=0o700)
        create_owned_root(DATA)
        create_owned_root(SOURCE)
        (DATA / "config").mkdir(mode=0o700)
        write_configuration()
        atomic_save(PRIVATE / "launcher-manifest.json", {"path": str(RUNTIME / "launch.sh"), "sha256": digest(RUNTIME / "launch.sh")})
        require(properties(UNIT).get("LoadState") == "not-found", "Fresh unit appeared before dispatch")
        started_service = True
        start_service()
        atomic_save(DISPATCH, dispatch_identity())
        process_deadline = time.monotonic() + 15
        while True:
            try:
                identity = new_identity()
                break
            except (RuntimeError, FileNotFoundError, IndexError):
                require(time.monotonic() < process_deadline, "Fresh service failed to establish its process identity")
                time.sleep(0.25)
        atomic_save(IDENTITY, identity)
        source_verification = generate_source(identity)
        samples.append(resources())
        with (PRIVATE / "bootstrap-console.log").open("x", encoding="utf-8") as output:
            child = subprocess.Popen(["nsenter", "-t", str(identity["pid"]), "-n", "python3", "-B", str(Path(__file__).resolve()), "_bootstrap"],
                                     stdout=output, stderr=output)
            deadline = time.monotonic() + 210
            while child.poll() is None:
                require(time.monotonic() < deadline, "Bootstrap supervisor deadline expired")
                same_new_identity(identity)
                sample = resources()
                samples.append(sample)
                print(json.dumps({"stage": "bootstrap-running", "dataBytes": sample["dataBytes"],
                                  "freshCgroupMemoryBytes": sample.get("freshCgroupMemoryBytes"), "at": sample["at"]}), flush=True)
                time.sleep(3)
            require(child.returncode == 0, "Fresh bootstrap failed; inspect retained private evidence")
        bootstrap_result = load(PRIVATE / "bootstrap-result.json")
        require(bootstrap_result.get("ordinaryLoginCount") == 1 and bootstrap_result.get("controlCredentialState") == "LIVE_HANDOFF",
                "Preparation did not hand off exactly one live ordinary login")
        verify_source(source_verification)
        listeners = listener_observation(identity)
        check_old_services(before_services)
        verify_baseline(before_files)
        verify_package(intent)
        samples.append(resources())
        save(PRIVATE / "resource-observations.json", {"samples": samples})
        require(len(list(RAW.glob(PREFIX + "*.json"))) == bootstrap_result["setupRecordCount"] and
                len(list(EXPORT.glob(PREFIX + "*.json"))) == bootstrap_result["setupRecordCount"], "Bootstrap record membership differs")
        manifest = {**intent, **identity, **bootstrap_result, "state": "READY", "readyAt": utc(),
                    "programDataIdentity": load(DATA_IDENTITY), "sourceRootIdentity": load(SOURCE_IDENTITY),
                    "sourceIdentity": source_verification["sourceIdentity"], "sourceVerification": source_verification,
                    "sourceVerificationFile": str(SOURCE_VERIFICATION), "mediaExportRoot": str(MEDIA_EXPORT), "tcpListeners": listeners,
                    "setupRawRoot": str(RAW), "setupExportRoot": str(EXPORT), "setupPrefix": PREFIX,
                    "oldPreservationVerified": True, "allFreshProgramDataOwned": True,
                    "bootstrapDeviceSnapshotTiming": "The single ordinary control credential remains live for the recorder"}
        atomic_save(MANIFEST, manifest)
        status = {"state": "READY", "fixtureAttempt": FIXTURE_ATTEMPT,
                  "unit": UNIT, "pid": identity["pid"], "serverId": bootstrap_result["serverId"],
                  "port": PORT, "setupRecordCount": bootstrap_result["setupRecordCount"], "oldPreservationVerified": True,
                  "ordinaryLoginCount": 1, "controlCredentialState": "LIVE_HANDOFF", "mediaCopiesCreated": 0,
                  "sourceGenerated": True, "sourceCompletelyVerified": True, "configurationMutationRequests": 0}
        save(ROOT / "ready-status.json", status)
        print(json.dumps(status), flush=True)
    except Exception as error:
        if child is not None and child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)
        cleanup_result, preservation_errors = {"stopped": False, "attempted": started_service}, []
        revocation_result = {"acknowledged": False, "invalidityProven": False, "notApplicable": True}
        if PRIVATE.exists():
            try:
                owned_root(ROOT)
                recover_atomic_publications()
            except Exception as publication_error:
                preservation_errors.append({"check": "authorityPublicationRecovery", "failureType": type(publication_error).__name__})
        if started_service:
            try:
                revocation_result = revoke_via_namespace(identity)
            except Exception as revoke_error:
                revocation_result = {"acknowledged": CONTROL_LOGIN.exists(), "invalidityProven": False,
                                     "failureType": type(revoke_error).__name__}
            try:
                cleanup_result = stop_owned(identity)
            except Exception as cleanup_error:
                cleanup_result = {"stopped": False, "failureType": type(cleanup_error).__name__, "message": str(cleanup_error)}
        for label, action in (("oldServices", lambda: check_old_services(before_services)), ("oldFiles", lambda: verify_baseline(before_files)),
                              ("sharedPackage", lambda: verify_package(intent))):
            try:
                action()
            except Exception as preservation_error:
                preservation_errors.append({"check": label, "failureType": type(preservation_error).__name__})
        saved = False
        if PRIVATE.exists() and (ROOT / ".goby-managed").exists():
            try:
                owned_root(ROOT)
                save(PRIVATE / "prepare-failure.json", {"failureType": type(error).__name__, "message": str(error),
                     "cleanup": cleanup_result, "controlRevocation": revocation_result, "preservationErrors": preservation_errors,
                     "dataRetained": DATA.exists(), "sourceRetained": SOURCE.exists(), "resourceSamples": samples})
                saved = True
            except Exception:
                pass
        print(json.dumps({"state": "FAILED", "failureType": type(error).__name__, "ownedServiceStopped": cleanup_result.get("stopped"),
                          "controlInvalidityProven": revocation_result.get("invalidityProven"),
                          "oldPreservationVerified": not preservation_errors, "privateEvidenceRetained": PRIVATE.exists(),
                          "failureReportSaved": saved}), flush=True)
        raise SystemExit(1)


def removal_proof(folder: Path) -> tuple[Path, dict]:
    require(folder == DATA and folder.parent == WORK or folder == SOURCE and folder.parent == ROOT,
            "Unexpected width fixture removal root")
    current = data_identity() if folder == DATA else source_root_identity()
    authority = DATA_IDENTITY if folder == DATA else SOURCE_IDENTITY
    require(load(authority) == current, "Removal root no longer matches its immutable creation identity")
    return PRIVATE / ("data-removal-intent.json" if folder == DATA else "source-removal-intent.json"), current


def remove_owned_tree(folder: Path) -> None:
    require(folder == DATA and folder.parent == WORK or folder == SOURCE and folder.parent == ROOT,
            "Only this fixture's fixed DATA or SOURCE may be removed")
    owned_root(ROOT)
    owned_root(folder)
    require(not os.path.ismount(folder), "Owned removal root is a mount point")
    state = properties(UNIT)
    require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
            "Owned service must be stopped before removing fixture files")
    proof_path, proof = removal_proof(folder)
    if proof_path.exists():
        require(load(proof_path) == proof, "Removal root identity changed between attempts")
    else:
        atomic_save(proof_path, proof)
    entries = list(folder.rglob("*"))
    require(len(entries) <= 20000, "Owned removal membership exceeds its bound")
    for path in entries:
        require(path.resolve(strict=True) == path and folder in path.parents and not path.is_symlink() and not os.path.ismount(path),
                "An owned removal target crosses a path or mount boundary")
        info = path.lstat()
        require(info.st_uid == 0 and (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)),
                "An owned removal target has an unexpected owner or type")
        require(stat.S_ISDIR(info.st_mode) or info.st_nlink == 1, "An owned removal target is hard-linked")
    for path in sorted(entries, key=lambda item: len(item.parts), reverse=True):
        if path == folder / ".goby-managed":
            continue
        path.rmdir() if path.is_dir() else path.unlink()
    try:
        (folder / ".goby-managed").unlink()
        folder.rmdir()
    except Exception:
        if folder.exists() and not folder.is_symlink() and folder.stat().st_dev == proof["device"] and \
                folder.stat().st_ino == proof["inode"] and not list(folder.iterdir()):
            save(folder / ".goby-managed", MARKER + "\n")
        raise


def remove_owned_data() -> None:
    remove_owned_tree(DATA)


def recover_empty_removal_root() -> None:
    for folder, authority, removal in ((DATA, DATA_IDENTITY, PRIVATE / "data-removal-intent.json"),
                                       (SOURCE, SOURCE_IDENTITY, PRIVATE / "source-removal-intent.json")):
        if not folder.exists() or (folder / ".goby-managed").exists():
            continue
        info = canonical(folder, directory=True, private=True)
        proof = {"path": str(folder), "device": info.st_dev, "inode": info.st_ino, "marker": MARKER}
        require(load(authority) == proof and load(removal) == proof and not os.path.ismount(folder) and not list(folder.iterdir()),
                "Unmarked root is not the attested empty removal directory")
        state = properties(UNIT)
        require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
                "An empty removal directory still has a service process")
        save(folder / ".goby-managed", MARKER + "\n")


def retained_revocation_proof() -> dict:
    if not CONTROL_LOGIN.exists():
        if CONTROL_LOGIN_INTENT.exists():
            return {"acknowledged": False, "invalidityProven": False, "unresolvedLoginAttempt": True,
                    "failureType": "LoginIntentWithoutDurableAcknowledgement"}
        return {"acknowledged": False, "notApplicable": True, "invalidityProven": False}
    token = load(CONTROL_LOGIN).get("AccessToken")
    require(isinstance(token, str) and token, "Retained control acknowledgement has no token")
    token_hash = hashlib.sha256(token.encode()).hexdigest()
    for path in sorted(PRIVATE.glob("control-revocation-*.json")):
        proof = load(path)
        if proof.get("controlTokenSha256") == token_hash and proof.get("invalidityProven") is True and \
                proof.get("invalidityStatus") == 401 and proof.get("persistenceComplete") is True:
            return {"acknowledged": True, "retainedProof": str(path), **proof}
    return {"acknowledged": True, "invalidityProven": False, "failureType": "StoppedBeforeInvalidityProof"}


def cleanup() -> None:
    common_preconditions(host=True)
    owned_root(ROOT)
    if not PRIVATE.exists():
        PRIVATE.mkdir(mode=0o700)
    canonical(PRIVATE, directory=True, private=True)
    recover_atomic_publications()
    recover_empty_removal_root()
    attempt = next((index for index in range(1, 100) if not (PRIVATE / ("cleanup-attempt-" + str(index) + ".json")).exists()), None)
    require(attempt is not None, "Cleanup attempt budget exhausted")
    report_path = PRIVATE / ("cleanup-attempt-" + str(attempt) + ".json")
    report = {"attempt": attempt, "fixtureAttempt": FIXTURE_ATTEMPT, "at": utc(),
              "unit": UNIT, "programData": str(DATA), "sourceRoot": str(SOURCE),
              "dataRemoved": False, "sourceRemoved": False, "evidenceRetained": True, "oldPreservationVerified": False,
              "controlInvalidityProvenOrNeverAcknowledged": False}
    try:
        if not INTENT.exists():
            require(not DATA.exists() and not DATA.is_symlink() and not SOURCE.exists() and not SOURCE.is_symlink() and
                    properties(UNIT).get("LoadState") == "not-found",
                    "Incomplete initialization has unexpected fixture resources")
            report.update({"dataAbsent": True, "sourceAbsent": True, "serviceAbsent": True, "initializationIncomplete": True})
            save(report_path, report)
            print(json.dumps({"dataAbsent": True, "serviceAbsent": True, "evidenceRetained": True}), flush=True)
            return
        intent = load(INTENT)
        require(intent.get("marker") == MARKER and intent.get("unit") == UNIT and intent.get("programData") == str(DATA) and
                intent.get("evidenceRoot") == str(ROOT) and intent.get("sourceRoot") == str(SOURCE) and
                intent.get("mediaCopiesCreated") == 0 and intent.get("ordinaryLoginBudget") == 1 and
                intent.get("fixtureAttempt") == FIXTURE_ATTEMPT,
                "Cleanup intent does not identify this exact width fixture")
        expected = load(IDENTITY) if IDENTITY.exists() else None
        ready = load(MANIFEST) if MANIFEST.exists() else None
        try:
            state = properties(UNIT)
            if int(state.get("MainPID", "0")) > 1:
                report["controlRevocation"] = revoke_via_namespace(expected)
            else:
                report["controlRevocation"] = retained_revocation_proof()
        except Exception as error:
            report["controlRevocation"] = {"invalidityProven": False, "failureType": type(error).__name__}
        finally:
            # Token or preservation failures cannot keep an exactly owned
            # invocation and any of its producers running.
            report["stop"] = stop_owned(expected)
        proof = report["controlRevocation"]
        report["controlInvalidityProvenOrNeverAcknowledged"] = (proof.get("invalidityProven") is True and
                                                               proof.get("persistenceComplete") is True) or proof.get("notApplicable") is True
        if ready is not None:
            require(ready.get("state") == "READY" and expected is not None and ready.get("pid") == expected["pid"] and
                    ready.get("startTicks") == expected["startTicks"] and ready.get("programDataIdentity") == load(DATA_IDENTITY) and
                    ready.get("sourceRootIdentity") == load(SOURCE_IDENTITY),
                    "Ready manifest and process/data attestation disagree")
        # Stop the independently owned invocation before a historical comparison
        # can veto deletion. No old file or evidence root is a removal candidate.
        check_old_services(intent["oldServices"])
        verify_baseline(intent["oldBaseline"])
        verify_package(intent)
        if SOURCE.exists():
            report["sourceBytesBeforeRemoval"] = tree_size(SOURCE)
            if SOURCE_VERIFICATION.exists() and not (PRIVATE / "source-removal-intent.json").exists():
                verify_source(ready["sourceIdentity"] if ready is not None else None)
            source_inventory = {str(path.relative_to(SOURCE)): {"size": path.stat().st_size, "sha256": digest(path)}
                                for path in sorted(SOURCE.rglob("*")) if path.is_file() and not path.is_symlink()}
            save(PRIVATE / ("cleanup-source-inventory-" + str(attempt) + ".json"), source_inventory)
            remove_owned_tree(SOURCE)
        report["sourceRemoved"] = not SOURCE.exists() and not SOURCE.is_symlink()
        if DATA.exists():
            report["dataBytesBeforeRemoval"] = tree_size(DATA)
            inventory = {str(path.relative_to(DATA)): {"size": path.stat().st_size, "sha256": digest(path)}
                         for path in sorted(DATA.rglob("*")) if path.is_file() and not path.is_symlink()}
            save(PRIVATE / ("cleanup-data-inventory-" + str(attempt) + ".json"), inventory)
            remove_owned_data()
        report["dataRemoved"] = not DATA.exists() and not DATA.is_symlink()
        check_old_services(intent["oldServices"])
        verify_baseline(intent["oldBaseline"])
        verify_package(intent)
        report["oldPreservationVerified"] = True
    except Exception as error:
        report.update({"failureType": type(error).__name__, "message": str(error)})
    save(report_path, report)
    print(json.dumps({key: report[key] for key in ("attempt", "dataRemoved", "sourceRemoved", "evidenceRetained", "oldPreservationVerified",
                                                  "controlInvalidityProvenOrNeverAcknowledged")}), flush=True)
    require(report["dataRemoved"] and report["sourceRemoved"] and report["oldPreservationVerified"] and
            report["controlInvalidityProvenOrNeverAcknowledged"], "Cleanup is incomplete; inspect retained private evidence")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("prepare", "cleanup", "_bootstrap", "_revoke"))
    options = parser.parse_args()
    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, interrupted)
    try:
        {"prepare": prepare, "cleanup": cleanup, "_bootstrap": bootstrap, "_revoke": revoke_control}[options.mode]()
    except Exception as error:
        # Never expose exception details or the child's private console log.
        print(json.dumps({"mode": options.mode, "result": "failed", "failureType": type(error).__name__}), flush=True)
        raise SystemExit(1)


if __name__ == "__main__":
    main()
