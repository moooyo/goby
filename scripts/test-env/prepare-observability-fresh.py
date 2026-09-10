#!/usr/bin/env python3
"""Prepare or remove one empty, owned Emby observability fixture.

Run only through authorized root SSH on test-env. Normal first-run setup
creates one administrator and one non-administrator viewer. Their two ordinary
logins remain live for the separate bounded observability recorder. No source,
media generation, library mutation, application key or hardware work is issued.
The official package and all earlier evidence remain shared read only. Failed
preparation invalidates every acknowledged login when possible, then stops only
the exact owned invocation. Explicit cleanup removes only the marked DATA root;
all private, sanitized and historical evidence remains retained.
"""

from __future__ import annotations

import argparse
import base64
from contextlib import contextmanager
import datetime as dt
import hashlib
import http.client
import importlib.util
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import stat
import subprocess
import sys
import time
from urllib.parse import urlencode

sys.dont_write_bytecode = True
WORK = Path("/opt/goby-test/exec-work-m5i")
ROOT = WORK / "emby-observability-fresh-m5i-20260910-01"
DATA = WORK / "emby-observability-fresh-data-01"
PRIVATE, RAW, EXPORT, RUNTIME = ROOT / "private", ROOT / "private/raw", ROOT / "export", ROOT / "runtime"
UNIT = "goby-emby-observability-fresh-m5i-20260910-01.service"
MARKER = "goby-emby-observability-fresh-m5i-20260910-01-owned-v1"
PREFIX = "observability-fresh-setup-m5i-"
DESCRIPTION = "Goby owned fresh Emby observability fixture M5i 20260910 01"
SERVER_NAME = "Goby Observability Fresh M5i 20260910 01"
PORT, HTTPS_PORT = 18101, 18501
PACKAGE_ROOT = Path("/dev/shm/goby-emby-reference")
APP = PACKAGE_ROOT / "package/opt/emby-server"
PACKAGE_SHA256 = "1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843"
OLD_DATA = Path("/opt/goby-test/emby-reference-data")
OLD_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
OLD_UNITS = {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3641418}
MAIN_START_TICKS = "26048863"
MAIN_BINARY_SHA256 = "62729fa1ba6b7d5f191d598c79a14606cb139bcdec6346ff5c3f1676551afd54"
PREVIOUS_ROOT = Path("/opt/goby-test/exec-work-m5h/emby-width-fresh-m5h-20260910-02")
PREVIOUS = PREVIOUS_ROOT / "runtime/width-capture"
PREVIOUS_DATA = PREVIOUS_ROOT.parent / "emby-width-fresh-data-02"
PREVIOUS_SOURCE = PREVIOUS_ROOT / "source"
PREVIOUS_UNIT = "goby-emby-width-fresh-m5h-20260910-02.service"
PREVIOUS_PID = 3640734
PREVIOUS_SERVER_ID = "61dea10d3f974dc784be57226a8daf62"
PREVIOUS_OPERATOR_SHA256 = "54ca55914049561752cb25e7825ae14a15aaa3d25b7275b9f9e8eba86c6e6c06"
PREVIOUS_CLEANUP_SHA256 = "e8c3b2672be065f5c97ded9b30378edabb6ea5db3b22580dd9d841baf9d35355"
PREVIOUS_SOURCE_SHA256 = "65cf7a11f33c785bcf885bec5d115d076e06967ab55c9aebea349d6a5f218953"
PREVIOUS_BUNDLE = PREVIOUS_ROOT.parent / "width-reference-attempt-2"
PREVIOUS_SUMMARY = PREVIOUS_ROOT.parent / "width-reference-summary.json"
PREVIOUS_BUNDLE_FILES = {
    "OWNER.txt", "source-inputs.json", "prepare-encoding-width-fresh.py", "reference-encoding-width-fresh.py",
    "test-encoding-width-reference.py", "reference-configuration.py", "reference-scheduled-tasks.py",
    "reference-devices.py", "reference-api-keys.py",
    *{"pure-tests-attempt-1." + extension for extension in ("json", "stderr", "exit")},
    *{prefix + "1." + extension for prefix in ("prepare-attempt-", "capture-attempt-", "cleanup-after-capture-attempt-")
      for extension in ("console.json", "stderr", "exit")},
}
HISTORICAL_REMOVED = [
    "/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-02/.goby-managed",
    "/dev/shm/goby-emby-scheduled-tasks-fresh-m5f-20260910-01",
    "/opt/goby-test/exec-scratch/emby-scheduled-tasks-fresh-m5f-20260910-01/source",
    "/opt/goby-test/exec-work-m5g/emby-configuration-fresh-data-01",
    "/opt/goby-test/exec-work-m5h/emby-width-fresh-data-01",
    "/opt/goby-test/exec-work-m5h/emby-width-fresh-m5h-20260910-01/source",
]
ALL_HISTORICAL_REMOVED = [*HISTORICAL_REMOVED, str(PREVIOUS_DATA), str(PREVIOUS_SOURCE)]
ACCOUNTS = {
    "admin": {"username": "reference-observability-fresh-admin-m5i-01",
              "client": "Goby Observability Fresh M5i 01 Admin",
              "deviceId": "goby-observability-fresh-m5i-20260910-01-admin", "administrator": True},
    "viewer": {"username": "reference-observability-fresh-viewer-m5i-01",
               "client": "Goby Observability Fresh M5i 01 Viewer",
               "deviceId": "goby-observability-fresh-m5i-20260910-01-viewer", "administrator": False},
}
SANITIZER_SOURCES = {
    "reference-configuration.py": "baa425eb9680d3e924c4d18d75ad99cc6d0a10ac438ba402501955bb96a359cd",
    "reference-scheduled-tasks.py": "b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1",
    "reference-devices.py": "96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d",
    "reference-api-keys.py": "ead25f9d47e8faf0b2b579ca8641b4ddcf4f7993c9e5a16710624c2927e749a8",
}
_CONFIGURATION_MODULE = None
INTENT, IDENTITY, DISPATCH = PRIVATE / "prepare-intent.json", PRIVATE / "service-identity.json", PRIVATE / "unit-dispatch.json"
DATA_IDENTITY = PRIVATE / "data-identity.json"
MANIFEST = PRIVATE / "manifest.json"
LOGIN_INTENTS = {account: PRIVATE / (account + "-login-intent.json") for account in ACCOUNTS}
LOGIN_RESPONSES = {account: PRIVATE / (account + "-login-response.json") for account in ACCOUNTS}
CREDENTIAL_FILES = {account: PRIVATE / (account + "-credentials.env") for account in ACCOUNTS}
MIB = 1024 * 1024
MAX_BODY, MAX_WIRE, MAX_REQUESTS = 256 * 1024, 8 * MIB, 24
MAX_CLEANUP_REQUESTS = 8
KNOWN_SECRETS: set[str] = set()
STATIC_PROPERTIES = ("Id", "Description", "PrivateNetwork", "PrivateTmp", "NoNewPrivileges", "ProtectSystem",
                     "ProtectHome", "WorkingDirectory", "ExecStart", "ControlGroup", "FragmentPath", "DropInPaths",
                     "ReadWritePaths", "ReadOnlyPaths", "MemoryMax", "CPUQuotaPerSecUSec", "TasksMax", "User",
                     "KillMode", "TimeoutStopUSec", "LimitNOFILE", "UMask", "PrivateDevices", "BindReadOnlyPaths")
READ_ONLY_PATHS = {str(WORK), str(PACKAGE_ROOT), str(OLD_DATA), "/opt/goby-fixtures", "/opt/goby-test/exec-scratch",
                   "/opt/goby-test/exec-work-m5g", str(PREVIOUS_ROOT.parent)}
ATOMIC_NAMES = {"prepare-intent.json", "service-identity.json", "unit-dispatch.json", "manifest.json",
                "launcher-manifest.json", "data-identity.json", "data-removal-intent.json",
                *{path.name for group in (LOGIN_INTENTS, LOGIN_RESPONSES, CREDENTIAL_FILES) for path in group.values()}}

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


def recover_atomic_publications(names: set[str] | None = None) -> None:
    canonical(PRIVATE, directory=True, private=True)
    selected = ATOMIC_NAMES if names is None else names
    require(selected <= ATOMIC_NAMES, "Unexpected authority recovery names")
    for name in sorted(selected):
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


def preserved_tree(folder: Path, *, maximum_files: int = 8192, maximum_bytes: int = 256 * MIB) -> dict:
    canonical(folder, directory=True)
    require(folder.stat().st_uid == 0 and folder.stat().st_mode & 0o022 == 0, "A preserved root is not protected")
    files, total, count = {}, 0, 0
    for current, directories, names in os.walk(folder, followlinks=False):
        for name in directories + names:
            path = Path(current) / name
            count += 1
            require(count <= 20000 and path.resolve(strict=True) == path and not path.is_symlink() and not os.path.ismount(path),
                    "A preserved tree crosses a path or mount boundary")
            entry = path.lstat()
            require(entry.st_uid == 0 and entry.st_mode & 0o022 == 0 and
                    (stat.S_ISREG(entry.st_mode) or stat.S_ISDIR(entry.st_mode)), "A preserved tree has an unsafe owner, mode or type")
            if stat.S_ISREG(entry.st_mode):
                total += entry.st_size
                require(len(files) < maximum_files and total <= maximum_bytes, "A preserved tree exceeds its finite bound")
                files[str(path)] = {"bytes": entry.st_size, "sha256": digest(path)}
    return dict(sorted(files.items()))


def old_baseline() -> dict:
    previous = load(PREVIOUS / "private/baseline.json")
    ready = load(PREVIOUS_ROOT / "private/manifest.json")
    identity = load(PREVIOUS_ROOT / "private/service-identity.json")
    cleanup_path = PREVIOUS_ROOT / "private/cleanup-attempt-1.json"
    cleanup = load(cleanup_path)
    audit_path = PREVIOUS / "private/raw/encoding-width-fresh-m5h-2-audit.json"
    audit = load(audit_path)
    require(ready.get("state") == "READY" and ready.get("fixtureAttempt") == 2 and ready.get("pid") == PREVIOUS_PID and
            ready.get("serverId") == PREVIOUS_SERVER_ID and ready.get("setupRecordCount") == 12 and
            ready.get("operatorSha256") == PREVIOUS_OPERATOR_SHA256 and identity.get("pid") == PREVIOUS_PID and
            ready.get("startTicks") == identity.get("startTicks"), "The latest width fixture authority differs")
    require(digest(cleanup_path) == PREVIOUS_CLEANUP_SHA256 and cleanup.get("unit") == PREVIOUS_UNIT and cleanup.get("attempt") == 1 and
            cleanup.get("programData") == str(PREVIOUS_DATA) and cleanup.get("sourceRoot") == str(PREVIOUS_SOURCE) and
            all(cleanup.get(name) is True for name in ("dataRemoved", "sourceRemoved", "oldPreservationVerified", "evidenceRetained",
                                                      "controlInvalidityProvenOrNeverAcknowledged")), "The latest width cleanup proof differs")
    stop, revoked = cleanup.get("stop", {}), cleanup.get("controlRevocation", {})
    require(all(stop.get(name) is True for name in ("stopped", "ownedProcessGone", "ownedCgroupEmpty")) and
            stop.get("unitMainPID") == 0 and stop.get("finalActiveState") == "inactive" and revoked.get("acknowledged") is True and
            revoked.get("invalidityProven") is True and revoked.get("invalidityStatus") == 401 and
            revoked.get("persistenceComplete") is True and revoked.get("ordinaryLoginsIssued") == 0 and revoked.get("persistenceFailures") == [],
            "The latest width process or ordinary credential teardown is incomplete")
    require(audit.get("cleanupPassed") is True and audit.get("captureFailureType") is None and audit.get("cleanupErrors") == [] and
            audit.get("persistenceFailures") == [] and audit.get("httpAttempts") == 44 and audit.get("incompleteHTTP") == 0 and
            audit.get("serverId") == PREVIOUS_SERVER_ID and audit.get("referencePID") == PREVIOUS_PID and
            isinstance(audit.get("checks"), dict) and len(audit["checks"]) == 8 and all(value is True for value in audit["checks"].values()),
            "The latest width capture lacks its complete bounded audit")
    exported_audit = PREVIOUS / "export/encoding-width-fresh-m5h-2-audit.json"
    canonical(exported_audit)
    require(digest(exported_audit) == "6501efe05b55cef45982f4d1f19cfbc16e82538dc3c3c206a27179880ff76be2",
            "The latest width audit export differs")
    require(len(previous.get("records", {})) == 4634 and len(previous.get("media", {})) == 240 and
            len(previous.get("privateFiles", {})) == 3425 and len(previous.get("retainedHistoricalFiles", {})) == 55 and
            previous.get("historicalRemovedPaths") == HISTORICAL_REMOVED, "The inherited width preservation population differs")
    for group in ("records", "media", "privateFiles", "retainedHistoricalFiles"):
        require(isinstance(previous.get(group), dict), "An inherited preservation group is not an object")
        for name, expected in previous[group].items():
            canonical(Path(name))
            require(digest(Path(name)) == expected, "An inherited record, media file or historical proof changed")
    require(all(not Path(name).exists() and not Path(name).is_symlink() for name in ALL_HISTORICAL_REMOVED),
            "A historically removed fixture root reappeared")
    retired_source = previous.get("ownedWidthSource", {})
    require(retired_source == ready.get("sourceIdentity") and retired_source.get("path") ==
            str(PREVIOUS_SOURCE / "Width Source M5h (2026)" / "Width Source M5h (2026).mp4") and
            retired_source.get("sha256") == PREVIOUS_SOURCE_SHA256,
            "The retired width source metadata differs; its removed path must never be reopened")
    state = properties(PREVIOUS_UNIT)
    require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
            "The retired width unit is active")
    if (Path("/proc") / str(PREVIOUS_PID)).exists():
        require(process_identity(PREVIOUS_PID)["startTicks"] != identity["startTicks"], "The retired width process remains")
    group_path = Path("/sys/fs/cgroup/system.slice") / PREVIOUS_UNIT
    if group_path.exists():
        require((group_path / "cgroup.procs").read_text().strip() == "", "The retired width cgroup contains a process")
        events = dict(line.split(maxsplit=1) for line in (group_path / "cgroup.events").read_text().splitlines())
        require(events.get("populated") == "0", "A retired width descendant cgroup remains populated")
    paths = {Path(name) for name in previous["records"]}
    for study, count in ((PREVIOUS_ROOT, 14), (PREVIOUS, 47)):
        raw, exported = sorted((study / "private/raw").glob("*.json")), sorted((study / "export").glob("*.json"))
        require(len(raw) == len(exported) == count and {path.name for path in raw} == {path.name for path in exported},
                "The latest width raw/export pair membership differs")
        paths.update(raw + exported)
    require(len(paths) == 4732, "Expected exactly 2366 preceding raw/export pairs after set union")
    private_files = dict(previous["privateFiles"])
    for folder in {path.parent.parent for path in paths if path.parent.name == "raw"}:
        private_files.update({name: entry["sha256"] for name, entry in preserved_tree(folder).items()})
    width_files, bundle_files = preserved_tree(PREVIOUS_ROOT), preserved_tree(PREVIOUS_BUNDLE, maximum_files=21, maximum_bytes=32 * MIB)
    require({str(Path(name).relative_to(PREVIOUS_BUNDLE)) for name in bundle_files} == PREVIOUS_BUNDLE_FILES and
            all(Path(name).parent == PREVIOUS_BUNDLE for name in bundle_files) and len(list(PREVIOUS_BUNDLE.iterdir())) == 21,
            "The latest width source bundle is not its exact 21-file membership")
    expected_sources = {**SANITIZER_SOURCES, "prepare-encoding-width-fresh.py": PREVIOUS_OPERATOR_SHA256,
                        "reference-encoding-width-fresh.py": "39e88efeab63ea3ce14aee8460e59c0dfffd878b5da702820bd64ac258b1804e",
                        "test-encoding-width-reference.py": "0f963b3e634370f7af51dbd436a1c3ca68ff10fec69f0e0730aa978cc45663c9"}
    require(all(bundle_files[str(PREVIOUS_BUNDLE / name)]["sha256"] == expected for name, expected in expected_sources.items()),
            "A pinned latest-width operator or sanitizer source changed")
    canonical(PREVIOUS_SUMMARY)
    require(PREVIOUS_SUMMARY.stat().st_size < MIB and json.loads(PREVIOUS_SUMMARY.read_text()).get("status") == "passed",
            "The completed width summary is unavailable")
    retained = {**previous["retainedHistoricalFiles"], **{name: entry["sha256"] for name, entry in {**width_files, **bundle_files}.items()},
                str(PREVIOUS_SUMMARY): digest(PREVIOUS_SUMMARY)}
    require(len(private_files) < 8192 and sum(Path(name).stat().st_size for name in private_files) <= 256 * MIB,
            "The complete preserved private corpus exceeds its bound")
    return {"records": {str(path): digest(path) for path in sorted(paths)}, "media": previous["media"],
            "privateFiles": dict(sorted(private_files.items())), "retainedHistoricalFiles": dict(sorted(retained.items())),
            "historicalRemovedPaths": ALL_HISTORICAL_REMOVED, "retiredOwnedSources": previous.get("retiredOwnedSources", {}),
            "failedFixtureProvenance": previous.get("failedFixtureProvenance", {}),
            "retiredProgramData": ready.get("oldBaseline", {}).get("retiredProgramData", {}),
            "retiredWidthSource": {"identity": retired_source, "removedAfterCompletedStudy": True,
                                    "cleanupReport": str(cleanup_path), "cleanupReportSha256": PREVIOUS_CLEANUP_SHA256},
            "retainedWidthFixture": {"root": str(PREVIOUS_ROOT), "unit": PREVIOUS_UNIT, "identity": identity,
                                      "files": width_files, "sourceBundleRoot": str(PREVIOUS_BUNDLE), "sourceBundleFiles": bundle_files}}


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
        require(report["MemAvailable"] >= 1280 * MIB and report["persistentFreeBytes"] >= 2048 * MIB,
                "Fresh observability fixture needs its bounded memory and persistent-disk reserve")
    else:
        require(report["MemAvailable"] >= 384 * MIB and report["persistentFreeBytes"] >= 512 * MIB,
                "Fresh observability fixture exhausted its retained headroom")
        require(report["dataBytes"] <= 256 * MIB and report["evidenceBytes"] <= 256 * MIB,
                "Fresh observability fixture exceeded its bounded data or evidence growth")
    state = properties(UNIT)
    cgroup = state.get("ControlGroup", "")
    if cgroup == "/system.slice/" + UNIT:
        current = Path("/sys/fs/cgroup") / cgroup.lstrip("/") / "memory.current"
        if current.exists():
            report["freshCgroupMemoryBytes"] = int(current.read_text())
    return report


def ensure_work_parent() -> None:
    require(WORK == Path("/opt/goby-test/exec-work-m5i"), "Unexpected observability work parent")
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
    require(ROOT.parent == DATA.parent == WORK, "Fresh owned roots escaped their fixed parents")
    canonical(WORK, directory=True, private=True)
    for folder in (PACKAGE_ROOT, OLD_DATA):
        canonical(folder, directory=True, private=True)
        canonical(folder / ".goby-managed", private=True)
        require((folder / ".goby-managed").read_text().strip() == "goby-emby-reference-owned-v1", "Shared reference ownership differs")
    canonical(PACKAGE_ROOT / ".extracted")
    require((PACKAGE_ROOT / ".extracted").read_text().strip() == PACKAGE_SHA256, "Shared package extraction provenance differs")
    canonical(APP / "system/EmbyServer")
    operator_entry = canonical(Path(__file__).resolve())
    require(operator_entry.st_uid == 0 and operator_entry.st_mode & 0o022 == 0, "The observability operator is not a protected root-owned file")


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


def emit(value: object) -> None:
    redactor = configuration_sanitizer()
    redactor.secrets = KNOWN_SECRETS
    redactor.collect_secrets(value)
    content = json.dumps(redactor.sanitize(value), ensure_ascii=False)
    require(not any(secret and secret in content for secret in KNOWN_SECRETS), "A known secret survived console redaction")
    print(content, flush=True)


def owned_root(folder: Path) -> None:
    require(folder in {ROOT, DATA}, "Unexpected owned root")
    canonical(folder, directory=True, private=True)
    canonical(folder / ".goby-managed", private=True)
    marker = MARKER
    require((folder / ".goby-managed").read_text().strip() == marker, "Fresh fixture ownership marker differs")


def create_owned_root(folder: Path) -> None:
    require(folder in {ROOT, DATA} and not folder.exists() and not folder.is_symlink(), "Owned root already exists")
    with blocked_signals():
        folder.mkdir(mode=0o700)
        created_inode = folder.stat().st_ino
        try:
            save(folder / ".goby-managed", MARKER + "\n")
            if folder == DATA:
                atomic_save(DATA_IDENTITY, data_identity())
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
    for folder in (ROOT, DATA):
        owned_root(folder)
    for folder in (PRIVATE, RAW, EXPORT, RUNTIME):
        canonical(folder, directory=True, private=True)
    require(load(DATA_IDENTITY) == data_identity(), "Fresh program-data creation identity differs")


def owned_authority() -> None:
    """Retained stop authority does not depend on partially removed roots."""
    owned_root(ROOT)
    canonical(PRIVATE, directory=True, private=True)


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
            str(RUNTIME / "launch.sh") in settings["ExecStart"] and settings["MemoryMax"] == str(768 * MIB) and
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


def deadline_expired(_signum: int, _frame: object) -> None:
    raise TimeoutError("The bounded fresh bootstrap request expired")


def interrupted(_signum: int, _frame: object) -> None:
    raise InterruptedError("The fresh reference operator received a termination signal")


class Bootstrap:
    def __init__(self, identity: dict) -> None:
        self.identity = identity
        self.tokens: dict[str, str] = {}
        self.secrets = KNOWN_SECRETS
        self.redactor = configuration_sanitizer()
        self.redactor.secrets = self.secrets
        self.count, self.wire = 0, 0
        self.complete_count, self.incomplete_count = 0, 0
        self.server_id: str | None = None
        self.deadline = time.monotonic() + 180
        self.revoked: set[str] = set()
        self.login_attempted: set[str] = set()
        self.passwords: dict[str, str] = {}
        self.viewer_creation_attempted = False
        self.prefix = PREFIX
        self.persistence_failures: list[dict] = []
        self.cleanup_count, self.cleanup_wire = 0, 0
        self.cleanup_by_account = {account: 0 for account in ACCOUNTS}

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
        require(account in ACCOUNTS, "Only the two reserved ordinary identities are allowed")
        return ('Emby Client="' + ACCOUNTS[account]["client"] + '", Device="Linux Fresh Test", '
                'DeviceId="' + ACCOUNTS[account]["deviceId"] + '", Version="0.1.0"')

    def request(self, label: str, method: str, route: str, *, body: object = None, token: str = "",
                account: str = "", form: bool = False, retry_readiness: bool = False, cleanup: bool = False) -> tuple[int | None, object]:
        require(time.monotonic() < self.deadline and (self.cleanup_count < MAX_CLEANUP_REQUESTS if cleanup else self.count < MAX_REQUESTS),
                "Fresh bootstrap request budget exhausted")
        if cleanup:
            require((method, route) in {("GET", "/emby/Sessions"), ("POST", "/emby/Sessions/Logout")},
                    "The independent cleanup reserve only permits ordinary credential invalidation")
            require(account in ACCOUNTS and self.cleanup_by_account[account] < 4, "This account exhausted its independent cleanup reserve")
            self.cleanup_count += 1
            self.cleanup_by_account[account] += 1
        same_new_identity(self.identity)
        require(os.readlink("/proc/self/ns/net") == self.identity["networkNamespace"], "HTTP is outside the fresh namespace")
        routes = {"GET": {"/emby/System/Info/Public", "/emby/Startup/User", "/emby/Users", "/emby/Devices", "/emby/System/Configuration",
                           "/emby/Library/VirtualFolders/Query", "/emby/Sessions"},
                  "POST": {"/emby/Startup/User", "/emby/Startup/RemoteAccess", "/emby/Startup/Complete",
                            "/emby/Users/AuthenticateByName", "/emby/Users/New", "/emby/Sessions/Logout"}}
        require(route in routes.get(method, set()), "Bootstrap route is outside the exact setup allowlist")
        if route == "/emby/Users/AuthenticateByName":
            require(account in ACCOUNTS and method == "POST" and account not in self.login_attempted and
                    not LOGIN_RESPONSES[account].exists() and not LOGIN_INTENTS[account].exists() and
                    account in self.passwords and body == {"Username": ACCOUNTS[account]["username"], "Pw": self.passwords[account]},
                    "An ordinary login was already attempted or its exact credential differs")
            self.login_attempted.add(account)
            atomic_save(LOGIN_INTENTS[account], {"at": utc(), "account": account, "serverId": self.server_id,
                        "client": ACCOUNTS[account]["client"], "deviceId": ACCOUNTS[account]["deviceId"],
                        "serviceIdentity": self.identity, "ordinaryLoginAttempt": 1})
        if route == "/emby/Users/New":
            require(account == "admin" and token and token == self.tokens.get("admin") and not self.viewer_creation_attempted and
                    body == {"Name": ACCOUNTS["viewer"]["username"]}, "Only the single new viewer may be created")
            self.viewer_creation_attempted = True
        if method != "GET":
            require(self.server_id and self.server_id != OLD_SERVER_ID, "Mutation requires a distinct proven fresh server identity")
        headers = {"Accept": "application/json"}
        if account:
            headers["Authorization"] = self.metadata(account)
        if token:
            require(account in ACCOUNTS and token == self.tokens.get(account), "Bootstrap HTTP token does not belong to this ordinary identity")
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
        alarm_before, alarm_started = signal.getitimer(signal.ITIMER_REAL), time.monotonic()
        try:
            remaining = self.deadline - time.monotonic()
            require(remaining > 0, "HTTP deadline expired during authority checks")
            signal.setitimer(signal.ITIMER_REAL, min(15, remaining, alarm_before[0] if alarm_before[0] > 0 else 15))
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
            restored = alarm_before[0] - (time.monotonic() - alarm_started)
            signal.setitimer(signal.ITIMER_REAL, max(0.001, restored) if cleanup and alarm_before[0] > 0 else max(0, restored), alarm_before[1])
            connection.close()
        self.wire += len(content)
        if cleanup:
            self.cleanup_wire += len(content)
        self.complete_count += int(complete)
        self.incomplete_count += int(not complete)
        # Acknowledgement precedes all DTO checks so failed assertions retain
        # the exact newly issued credential and its cleanup responsibility.
        if isinstance(parsed, dict) and isinstance(parsed.get("AccessToken"), str) and parsed["AccessToken"]:
            require(account in ACCOUNTS and route == "/emby/Users/AuthenticateByName", "Unexpected bootstrap credential response")
            self.tokens[account] = parsed["AccessToken"]
            self.secrets.add(parsed["AccessToken"])
            atomic_save(LOGIN_RESPONSES[account], parsed)
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
                if not cleanup or time.monotonic() >= self.deadline:
                    raise
        self.last_record = name
        if not cleanup:
            emit({"stage": label, "status": status_code, "completeHTTP": complete, "record": self.count})
        require(self.cleanup_wire <= MAX_CLEANUP_REQUESTS * MAX_BODY if cleanup else self.wire <= MAX_WIRE, "Bootstrap wire-byte budget exhausted")
        if not retry_readiness:
            require(complete, "Fresh bootstrap HTTP response was incomplete")
        return status_code, parsed

    def logout(self, account: str) -> dict:
        token = self.tokens[account]
        report = {"account": account, "acknowledged": True, "tokenSha256": hashlib.sha256(token.encode()).hexdigest(),
                  "invalidityProven": False, "errors": []}
        before = len(self.persistence_failures)
        try:
            status, _ = self.request(account + "-logout", "POST", "/emby/Sessions/Logout", token=token, account=account, cleanup=True)
            report["logoutStatus"] = status
            require(status in {204, 401}, "Owned ordinary logout was neither acknowledged nor already invalid")
        except Exception as error:
            report["errors"].append({"stage": "logout", "failureType": type(error).__name__})
        try:
            status, _ = self.request(account + "-logout-invalid", "GET", "/emby/Sessions", token=token, account=account, cleanup=True)
            report["invalidityStatus"] = status
            require(status == 401, "Owned ordinary credential invalidity is unproven")
            self.revoked.add(account)
            report["invalidityProven"] = True
        except Exception as error:
            report["errors"].append({"stage": "invalidity", "failureType": type(error).__name__})
        report["persistenceFailures"] = self.persistence_failures[before:]
        report["persistenceComplete"] = not report["persistenceFailures"]
        return report

    def login(self, account: str) -> dict:
        expected = ACCOUNTS[account]
        status, body = self.request(account + "-login", "POST", "/emby/Users/AuthenticateByName", account=account,
                                    body={"Username": expected["username"], "Pw": self.passwords[account]})
        require(status == 200 and account in self.tokens and isinstance(body, dict) and body.get("ServerId") == self.server_id,
                "The one ordinary login failed; no password or authentication fallback is permitted")
        user, session = body.get("User", {}), body.get("SessionInfo", {})
        require(isinstance(user, dict) and user.get("Name") == expected["username"] and isinstance(user.get("Id"), str) and user["Id"] and
                user.get("Policy", {}).get("IsAdministrator") is expected["administrator"] and isinstance(session, dict) and
                session.get("DeviceId") == expected["deviceId"] and session.get("Client") == expected["client"] and
                session.get("UserId") == user["Id"] and isinstance(session.get("Id"), str) and session["Id"],
                "An acknowledged ordinary login has an unexpected user, role or device session")
        values = {"REFERENCE_USERNAME": expected["username"], "REFERENCE_PASSWORD": self.passwords[account],
                  "REFERENCE_TOKEN": self.tokens[account], "REFERENCE_USER_ID": user["Id"], "REFERENCE_DEVICE_ID": expected["deviceId"]}
        require(all(isinstance(value, str) and "\n" not in value and "\r" not in value and "\0" not in value and
                    (bool(value) or account == "viewer" and name == "REFERENCE_PASSWORD") for name, value in values.items()),
                "Ordinary handoff credentials have invalid private fields")
        atomic_save(CREDENTIAL_FILES[account], "".join(name + "=" + value + "\n" for name, value in values.items()))
        return {"userId": user["Id"], "username": expected["username"], "deviceId": expected["deviceId"],
                "client": expected["client"], "administrator": expected["administrator"], "sessionId": session["Id"],
                "credentialState": "LIVE_HANDOFF", "tokenSha256": hashlib.sha256(self.tokens[account].encode()).hexdigest(),
                "credentialsFile": str(CREDENTIAL_FILES[account]), "credentialsSha256": digest(CREDENTIAL_FILES[account]),
                "loginResponseFile": str(LOGIN_RESPONSES[account]), "loginResponseSha256": digest(LOGIN_RESPONSES[account]),
                "loginIntentFile": str(LOGIN_INTENTS[account]), "loginIntentSha256": digest(LOGIN_INTENTS[account])}

    def capture(self) -> dict:
        readiness_deadline = time.monotonic() + 90
        public, status = None, None
        for attempt in range(10):
            status, public = self.request("readiness-public", "GET", "/emby/System/Info/Public", retry_readiness=True)
            if status == 200:
                break
            require(attempt < 9 and time.monotonic() + 7 < readiness_deadline, "Fresh reference exhausted bounded readiness")
            time.sleep(7)
        require(status == 200 and isinstance(public, dict) and public.get("Version") == "4.9.5.0" and
                public.get("ServerName") == SERVER_NAME and isinstance(public.get("Id"), str) and public["Id"] and
                public["Id"] not in {OLD_SERVER_ID, PREVIOUS_SERVER_ID}, "Fresh public server identity differs")
        self.server_id = public["Id"]
        save(PRIVATE / "fresh-public-identity.json", public)
        self.passwords = {"admin": secrets.token_hex(32), "viewer": ""}
        self.secrets.add(self.passwords["admin"])
        save(PRIVATE / "account-secrets.json", self.passwords)
        status, startup = self.request("startup-user-get", "GET", "/emby/Startup/User", account="admin")
        require(status == 200 and isinstance(startup, dict) and isinstance(startup.get("Name"), str), "Fresh startup user is unavailable")
        status, result = self.request("startup-user-post", "POST", "/emby/Startup/User", account="admin", form=True,
                                      body={"Name": ACCOUNTS["admin"]["username"], "Password": self.passwords["admin"]})
        require(status == 200 and result == {}, "Fresh administrator setup differs")
        status, _ = self.request("startup-remote-access", "POST", "/emby/Startup/RemoteAccess", account="admin", form=True,
                                 body={"EnableAutomaticPortMapping": "false"})
        require(status == 204, "Fresh remote-access setup differs")
        status, _ = self.request("startup-complete", "POST", "/emby/Startup/Complete", account="admin")
        require(status == 204, "Fresh startup completion differs")
        credentials = {"admin": self.login("admin")}
        admin_token = self.tokens["admin"]
        status, viewer = self.request("viewer-create", "POST", "/emby/Users/New", account="admin", token=admin_token,
                                      body={"Name": ACCOUNTS["viewer"]["username"]})
        require(status == 200 and isinstance(viewer, dict) and isinstance(viewer.get("Id"), str) and viewer["Id"] and
                viewer.get("Name") == ACCOUNTS["viewer"]["username"] and viewer.get("HasPassword") is False and
                viewer.get("HasConfiguredPassword") is False and viewer.get("Policy", {}).get("IsAdministrator") is False,
                "Fresh viewer creation or its proven empty-password flags differ")
        save(PRIVATE / "viewer-created.json", viewer)
        time.sleep(1.1)
        credentials["viewer"] = self.login("viewer")
        require(credentials["viewer"]["userId"] == viewer["Id"] and len(set(self.tokens.values())) == 2 and
                credentials["admin"]["userId"] != credentials["viewer"]["userId"], "Fresh ordinary identities are not distinct")
        status, users = self.request("bootstrap-final-users", "GET", "/emby/Users", token=admin_token, account="admin")
        require(status == 200 and isinstance(users, list) and len(users) == 2 and
                {row.get("Id") for row in users if isinstance(row, dict)} == {row["userId"] for row in credentials.values()},
                "Fresh user membership differs from the two newly created accounts")
        status, devices = self.request("bootstrap-final-devices", "GET", "/emby/Devices", token=admin_token, account="admin")
        require(status == 200 and isinstance(devices, dict) and isinstance(devices.get("Items"), list) and len(devices["Items"]) == 2,
                "Fresh device membership differs from the two newly logged-in devices")
        for account, metadata in credentials.items():
            matches = [row for row in devices["Items"] if isinstance(row, dict) and row.get("ReportedDeviceId") == metadata["deviceId"]]
            login_session = load(LOGIN_RESPONSES[account])["SessionInfo"]
            require(len(matches) == 1 and matches[0].get("AppName") == metadata["client"] and
                    matches[0].get("LastUserId") == metadata["userId"] and isinstance(matches[0].get("Id"), str) and matches[0]["Id"] and
                    matches[0]["Id"] == str(login_session.get("InternalDeviceId")), "A fresh device is not owned by its ordinary credential")
            metadata["internalDeviceId"] = matches[0]["Id"]
        administrator = credentials["admin"]
        administrator_ownership = {"serverId": self.server_id, "userId": administrator["userId"], "deviceId": administrator["deviceId"],
                                   "internalDeviceId": administrator["internalDeviceId"], "sessionId": administrator["sessionId"],
                                   "administrator": True, "tokenSha256": administrator["tokenSha256"]}
        status, configuration = self.request("bootstrap-safe-configuration", "GET", "/emby/System/Configuration", token=admin_token, account="admin")
        require(status == 200 and isinstance(configuration, dict), "Fresh safe configuration is unavailable")
        for name in ("EnableHttps", "EnableUPnP", "EnableRemoteAccess", "EnableAutoUpdate", "EnableAutomaticRestart", "AutoRunWebApp"):
            require(configuration.get(name) is False, "Fresh configuration enabled an unsafe startup capability")
        require(configuration.get("HttpServerPortNumber") == PORT and configuration.get("PublicPort") == PORT and
                configuration.get("HttpsPortNumber") == HTTPS_PORT and configuration.get("PublicHttpsPort") == HTTPS_PORT and
                configuration.get("ServerName") == SERVER_NAME and configuration.get("LocalNetworkAddresses") == ["127.0.0.1"] and
                configuration.get("IsStartupWizardCompleted") is True, "Fresh loopback ports, server name or setup state differs")
        configuration_record = str(RAW / (self.last_record + ".json"))
        status, libraries = self.request("bootstrap-final-libraries", "GET", "/emby/Library/VirtualFolders/Query", token=admin_token, account="admin")
        require(status == 200 and isinstance(libraries, dict) and libraries.get("Items") == [], "Fresh bootstrap contains an unexpected library")
        status, sessions = self.request("ordinary-live-handoff", "GET", "/emby/Sessions", token=admin_token, account="admin")
        require(status == 200 and isinstance(sessions, list) and all(any(isinstance(row, dict) and
                row.get("DeviceId") == entry["deviceId"] and row.get("UserId") == entry["userId"] for row in sessions)
                for entry in credentials.values()), "The two acknowledged ordinary sessions are not live at handoff")
        credentials["admin"]["liveProbeStatus"] = 200
        protected_access = {"admin": {"status": 200, "capture": str(RAW / (self.last_record + ".json"))}}
        viewer_metadata = credentials["viewer"]
        status, viewer_sessions = self.request("viewer-live-handoff", "GET", "/emby/Sessions", token=self.tokens["viewer"], account="viewer")
        require(status == 200 and isinstance(viewer_sessions, list) and any(isinstance(row, dict) and
                row.get("DeviceId") == viewer_metadata["deviceId"] and row.get("UserId") == viewer_metadata["userId"] for row in viewer_sessions),
                "The viewer credential failed its previously observed protected-session contract")
        credentials["viewer"]["liveProbeStatus"] = status
        protected_access["viewer"] = {"status": status, "capture": str(RAW / (self.last_record + ".json"))}
        require(self.login_attempted == set(ACCOUNTS) and not self.revoked and not self.persistence_failures,
                "The two ordinary logins are not a complete live handoff")
        for raw in sorted(RAW.glob(PREFIX + "*.json")):
            require(self.sanitize(load(raw)) == load(EXPORT / raw.name), "Fresh setup redaction audit failed")
        return {"serverId": self.server_id, "credentialState": "LIVE_HANDOFF", "ordinaryLoginCount": 2, "credentials": credentials,
                "freshAdministratorOwnership": administrator_ownership, "ordinaryProtectedAccessProofs": protected_access,
                "bootstrapUsers": users, "bootstrapDevices": devices["Items"], "bootstrapLibraries": libraries["Items"],
                "bootstrapConfigurationRecord": configuration_record, "bootstrapConfigurationSafetyVerified": True,
                "bootstrapLibraryMutationRequests": 0, "bootstrapConfigurationMutationRequests": 0, "bootstrapNamedConfigurationRequests": 0,
                "bootstrapTaskRequests": 0, "bootstrapTaskMutationRequests": 0, "bootstrapApplicationKeyRequests": 0,
                "bootstrapMediaRequests": 0, "viewerEmptyPasswordLoginVerified": True, "setupRecordCount": self.count,
                "setupCompleteHTTPResponses": self.complete_count, "setupIncompleteAttempts": self.incomplete_count, "wireBytes": self.wire}

    def revoke_all(self) -> dict:
        cleanup_deadline = time.monotonic() + 90
        results = {}
        for account in ("viewer", "admin"):
            if account not in self.tokens:
                results[account] = absent_login_state(account)
                continue
            try:
                self.deadline = min(cleanup_deadline, time.monotonic() + 40)
                signal.setitimer(signal.ITIMER_REAL, max(0.001, self.deadline - time.monotonic()))
                results[account] = self.logout(account)
            except Exception as error:
                results[account] = {"account": account, "acknowledged": True, "invalidityProven": False,
                                    "failureType": type(error).__name__}
            finally:
                signal.setitimer(signal.ITIMER_REAL, 0)
        return {"accounts": results, "ordinaryLoginsIssued": 0,
                "allInvalidOrNeverAttempted": all(revocation_complete(row) for row in results.values())}


def absent_login_state(account: str) -> dict:
    require(account in ACCOUNTS, "Unknown ordinary account")
    if LOGIN_INTENTS[account].exists():
        return {"account": account, "acknowledged": False, "invalidityProven": False, "unresolvedLoginAttempt": True,
                "failureType": "LoginIntentWithoutDurableAcknowledgement"}
    return {"account": account, "acknowledged": False, "notApplicable": True, "invalidityProven": False}


def revocation_complete(value: dict) -> bool:
    return value.get("notApplicable") is True or (value.get("invalidityProven") is True and value.get("persistenceComplete") is True)


def bootstrap() -> None:
    common_preconditions(host=False)
    owned_roots()
    intent, identity = load(INTENT), load(IDENTITY)
    require(intent.get("marker") == MARKER and intent.get("operatorSha256") == digest(Path(__file__).resolve()) and
            intent.get("sanitizerSources") == SANITIZER_SOURCES and intent.get("ordinaryLoginBudget") == 2,
            "Fresh bootstrap intent or provenance differs")
    same(identity)
    require(os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "Bootstrap must enter the attested fresh namespace")
    signal.signal(signal.SIGALRM, deadline_expired)
    recorder = Bootstrap(identity)
    try:
        save(PRIVATE / "bootstrap-result.json", recorder.capture())
    except Exception as error:
        revocation = recorder.revoke_all()
        save(PRIVATE / "bootstrap-failure.json", {"failureType": type(error).__name__, "message": str(error),
             "records": recorder.count, "ordinaryLoginAttempts": sorted(recorder.login_attempted),
             "credentialRevocation": revocation, "persistenceFailures": recorder.persistence_failures})
        raise


def revoke_credentials() -> None:
    common_preconditions(host=False)
    owned_roots()
    identity = load(IDENTITY)
    same(identity)
    require(os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "Credential cleanup escaped the fresh namespace")
    recorder = Bootstrap(identity)
    recorder.server_id = load(PRIVATE / "fresh-public-identity.json")["Id"]
    if (PRIVATE / "account-secrets.json").exists():
        for value in load(PRIVATE / "account-secrets.json").values():
            if isinstance(value, str) and value:
                recorder.secrets.add(value)
    ready, ready_error = None, None
    if MANIFEST.exists():
        try:
            ready = load(MANIFEST)
        except Exception as error:
            ready_error = {"stage": "readyManifest", "failureType": type(error).__name__}
    account_errors = {}
    for account in ACCOUNTS:
        if not LOGIN_RESPONSES[account].exists():
            continue
        try:
            login, login_intent = load(LOGIN_RESPONSES[account]), load(LOGIN_INTENTS[account])
            token = login.get("AccessToken")
            require(isinstance(token, str) and token, "A retained login acknowledgement has no token")
            recorder.secrets.add(token)
            require(login_intent.get("account") == account and login_intent.get("serviceIdentity") == identity and
                    login_intent.get("serverId") == recorder.server_id and login_intent.get("client") == ACCOUNTS[account]["client"] and
                    login_intent.get("deviceId") == ACCOUNTS[account]["deviceId"], "A retained login intent does not own this credential")
            if ready is not None:
                require(ready.get("credentialState") == "LIVE_HANDOFF" and ready.get("ordinaryLoginCount") == 2 and
                        ready.get("credentials", {}).get(account, {}).get("tokenSha256") == hashlib.sha256(token.encode()).hexdigest(),
                        "An ordinary credential differs from its exact READY handoff")
            recorder.tokens[account] = token
        except Exception as error:
            account_errors[account] = {"account": account, "invalidityProven": False, "failureType": type(error).__name__}
    attempt = next((index for index in range(1, 100) if not (PRIVATE / ("credential-revocation-" + str(index) + ".json")).exists()), None)
    require(attempt is not None, "Credential revocation attempt budget exhausted")
    recorder.prefix = "observability-fresh-operator-cleanup-m5i-" + str(attempt) + "-"
    signal.signal(signal.SIGALRM, deadline_expired)
    report = {"at": utc(), "attempt": attempt, **recorder.revoke_all()}
    report["accounts"].update(account_errors)
    report["authorityErrors"] = [ready_error] if ready_error is not None else []
    report["allInvalidOrNeverAttempted"] = not report["authorityErrors"] and all(revocation_complete(row) for row in report["accounts"].values())
    report["persistenceFailures"] = recorder.persistence_failures
    save(PRIVATE / ("credential-revocation-" + str(attempt) + ".json"), report)
    require(report["allInvalidOrNeverAttempted"], "Ordinary credential cleanup is incomplete")


def revoke_via_namespace(identity: dict | None) -> dict:
    if not any(path.exists() for path in LOGIN_RESPONSES.values()):
        results = {account: absent_login_state(account) for account in ACCOUNTS}
        return {"accounts": results, "allInvalidOrNeverAttempted": all(revocation_complete(row) for row in results.values())}
    require(identity is not None, "Acknowledged credentials have no service identity for cleanup")
    same(identity)
    before = set(PRIVATE.glob("credential-revocation-*.json"))
    try:
        run(["nsenter", "-t", str(identity["pid"]), "-n", "python3", "-B", str(Path(__file__).resolve()), "_revoke"], timeout=105)
    except Exception as error:
        failure = type(error).__name__
    else:
        failure = None
    created = set(PRIVATE.glob("credential-revocation-*.json")) - before
    if len(created) == 1:
        return load(created.pop())
    return {"allInvalidOrNeverAttempted": False, "failureType": failure or "MissingRevocationEvidence"}

def write_configuration() -> None:
    save(DATA / "config/system.xml", f'''<?xml version="1.0" encoding="utf-8"?>
<ServerConfiguration xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <HttpServerPortNumber>18101</HttpServerPortNumber><PublicPort>18101</PublicPort>
  <HttpsPortNumber>18501</HttpsPortNumber><PublicHttpsPort>18501</PublicHttpsPort>
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
                "CPUQuota": "150%", "MemoryMax": "768M", "TasksMax": "256", "LimitNOFILE": "65536",
                "TimeoutStopSec": "25", "KillMode": "control-group", "WorkingDirectory": str(RUNTIME), "UMask": "0077",
                "StandardOutput": "append:" + str(RUNTIME / "service.log"), "StandardError": "append:" + str(RUNTIME / "service.log")}
    run(["systemd-run", "--unit=" + UNIT, "--collect", *["--property=" + key + "=" + value for key, value in settings.items()],
         str(RUNTIME / "launch.sh")])


def dispatch_identity(state: dict | None = None, *, stopping: bool = False) -> dict:
    """Attest unit ownership before the final Emby process becomes ready."""
    owned_authority()
    if stopping and DISPATCH.exists():
        launcher = {"sha256": load(DISPATCH)["launcherSha256"]}
    else:
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
        current_dispatch = dispatch_identity(state, stopping=True)
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
            "Fresh observability fixture paths already exist")
    require(properties(UNIT).get("LoadState") == "not-found", "Fresh observability unit already exists")
    before_services, before_files = old_services(), old_baseline()
    initial_resources = resources(admission=True)
    os.umask(0o077)
    intent = {"schemaVersion": 1, "state": "PREPARING", "marker": MARKER, "unit": UNIT, "programData": str(DATA),
              "evidenceRoot": str(ROOT), "port": PORT, "httpsPort": HTTPS_PORT, "startedAt": utc(),
              "operatorSha256": digest(Path(__file__).resolve()), "sanitizerSources": SANITIZER_SOURCES,
              "oldServices": before_services, "oldBaseline": before_files, "initialResources": initial_resources,
              "packageSha256": PACKAGE_SHA256, "sharedServerBinarySha256": digest(APP / "system/EmbyServer"),
              "ordinaryLoginBudget": 2, "mediaCopiesCreated": 0, "sourceDirectoriesCreated": 0,
              "libraryMutationRequests": 0, "applicationKeyRequests": 0, "accounts": ACCOUNTS,
              "limits": {"serviceMemoryBytes": 768 * MIB, "admissionAvailableMemoryBytes": 1280 * MIB,
                         "admissionPersistentFreeBytes": 2048 * MIB, "minimumAvailableMemoryBytes": 384 * MIB,
                         "dataGrowthBytes": 256 * MIB, "evidenceGrowthBytes": 256 * MIB, "minimumPersistentFreeBytes": 512 * MIB,
                         "setupHTTPAttempts": MAX_REQUESTS, "cleanupHTTPAttempts": MAX_CLEANUP_REQUESTS,
                         "singleResponseBytes": MAX_BODY, "totalResponseBytes": MAX_WIRE,
                         "readinessAttempts": 10, "readinessSeconds": 90, "bootstrapMainSeconds": 180,
                         "bootstrapCleanupSeconds": 90, "bootstrapSupervisorSeconds": 300}}
    identity, child, started_service = None, None, False
    samples: list[dict] = []
    try:
        create_owned_root(ROOT)
        PRIVATE.mkdir(mode=0o700)
        atomic_save(INTENT, intent)
        for folder in (RAW, PRIVATE / "wire", EXPORT, RUNTIME):
            folder.mkdir(mode=0o700)
        create_owned_root(DATA)
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
        with (PRIVATE / "bootstrap-console.log").open("x", encoding="utf-8") as output:
            child = subprocess.Popen(["nsenter", "-t", str(identity["pid"]), "-n", "python3", "-B", str(Path(__file__).resolve()), "_bootstrap"],
                                     stdout=output, stderr=output)
            deadline = time.monotonic() + 300
            while child.poll() is None:
                require(time.monotonic() < deadline, "Bootstrap supervisor deadline expired")
                same(identity)
                sample = resources()
                samples.append(sample)
                emit({"stage": "bootstrap-running", "dataBytes": sample["dataBytes"],
                      "freshCgroupMemoryBytes": sample.get("freshCgroupMemoryBytes"), "at": sample["at"]})
                time.sleep(3)
            require(child.returncode == 0, "Fresh bootstrap failed; inspect retained private evidence")
        result = load(PRIVATE / "bootstrap-result.json")
        require(result.get("ordinaryLoginCount") == 2 and result.get("credentialState") == "LIVE_HANDOFF" and
                set(result.get("credentials", {})) == set(ACCOUNTS), "Preparation did not hand off exactly two ordinary credentials")
        listeners = listener_observation(identity)
        check_old_services(before_services)
        verify_baseline(before_files)
        verify_package(intent)
        samples.append(resources())
        save(PRIVATE / "resource-observations.json", {"samples": samples})
        require(len(list(RAW.glob(PREFIX + "*.json"))) == result["setupRecordCount"] and
                len(list(EXPORT.glob(PREFIX + "*.json"))) == result["setupRecordCount"], "Bootstrap record membership differs")
        manifest = {**intent, **identity, **result, "state": "READY", "readyAt": utc(),
                    "programDataIdentity": load(DATA_IDENTITY), "tcpListeners": listeners,
                    "setupRawRoot": str(RAW), "setupExportRoot": str(EXPORT), "setupPrefix": PREFIX,
                    "oldPreservationVerified": True, "allFreshProgramDataOwned": True}
        atomic_save(MANIFEST, manifest)
        status = {"state": "READY", "unit": UNIT, "pid": identity["pid"], "serverId": result["serverId"],
                  "port": PORT, "setupRecordCount": result["setupRecordCount"], "oldPreservationVerified": True,
                  "ordinaryLoginCount": 2, "credentialState": "LIVE_HANDOFF", "mediaCopiesCreated": 0,
                  "sourceDirectoriesCreated": 0, "libraryMutationRequests": 0, "applicationKeyRequests": 0}
        save(ROOT / "ready-status.json", status)
        emit(status)
    except Exception as error:
        if child is not None and child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)
        stop_result, preservation_errors = {"stopped": False, "attempted": started_service}, []
        revocation = {"allInvalidOrNeverAttempted": False}
        if PRIVATE.exists():
            try:
                owned_root(ROOT)
                recover_atomic_publications()
            except Exception as recovery_error:
                preservation_errors.append({"check": "authorityRecovery", "failureType": type(recovery_error).__name__})
        if started_service:
            try:
                revocation = revoke_via_namespace(identity)
            except Exception as revoke_error:
                revocation = {"allInvalidOrNeverAttempted": False, "failureType": type(revoke_error).__name__}
            finally:
                try:
                    stop_result = stop_owned(identity)
                except Exception as stop_error:
                    stop_result = {"stopped": False, "failureType": type(stop_error).__name__, "message": str(stop_error)}
        for label, action in (("oldServices", lambda: check_old_services(before_services)),
                              ("oldFiles", lambda: verify_baseline(before_files)), ("sharedPackage", lambda: verify_package(intent))):
            try:
                action()
            except Exception as preservation_error:
                preservation_errors.append({"check": label, "failureType": type(preservation_error).__name__})
        saved = False
        if PRIVATE.exists() and (ROOT / ".goby-managed").exists():
            try:
                owned_root(ROOT)
                save(PRIVATE / "prepare-failure.json", {"failureType": type(error).__name__, "message": str(error),
                     "cleanup": stop_result, "credentialRevocation": revocation, "preservationErrors": preservation_errors,
                     "dataRetained": DATA.exists(), "resourceSamples": samples})
                saved = True
            except Exception:
                pass
        emit({"state": "FAILED", "failureType": type(error).__name__, "ownedServiceStopped": stop_result.get("stopped"),
              "ordinaryCredentialsInvalidOrNeverAttempted": revocation.get("allInvalidOrNeverAttempted"),
              "oldPreservationVerified": not preservation_errors, "privateEvidenceRetained": PRIVATE.exists(), "failureReportSaved": saved})
        raise SystemExit(1)


def removal_proof(folder: Path) -> tuple[Path, dict]:
    require(folder == DATA and folder.parent == WORK, "Only this fixture's fixed DATA may be removed")
    current = data_identity()
    require(load(DATA_IDENTITY) == current, "Data root no longer matches its immutable creation identity")
    return PRIVATE / "data-removal-intent.json", current


def remove_owned_tree(folder: Path) -> None:
    require(folder == DATA and folder.parent == WORK, "Only this fixture's fixed DATA may be removed")
    owned_root(ROOT)
    owned_root(DATA)
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
        entry = path.lstat()
        require(entry.st_uid == 0 and (stat.S_ISDIR(entry.st_mode) or stat.S_ISREG(entry.st_mode)),
                "An owned removal target has an unexpected owner or type")
        require(stat.S_ISDIR(entry.st_mode) or entry.st_nlink == 1, "An owned removal target is hard-linked")
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
    if not DATA.exists() or (DATA / ".goby-managed").exists():
        return
    entry = canonical(DATA, directory=True, private=True)
    proof = {"path": str(DATA), "device": entry.st_dev, "inode": entry.st_ino, "marker": MARKER}
    require(load(DATA_IDENTITY) == proof and load(PRIVATE / "data-removal-intent.json") == proof and
            not os.path.ismount(DATA) and not list(DATA.iterdir()), "Unmarked DATA is not the attested empty removal root")
    state = properties(UNIT)
    require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
            "An empty removal root still has a service process")
    save(DATA / ".goby-managed", MARKER + "\n")


def retained_revocation_proof() -> dict:
    results, read_errors = {}, []
    for account in ACCOUNTS:
        if not LOGIN_RESPONSES[account].exists():
            results[account] = absent_login_state(account)
            continue
        results[account] = {"account": account, "acknowledged": True, "invalidityProven": False,
                            "failureType": "StoppedBeforeInvalidityProof"}
        try:
            token = load(LOGIN_RESPONSES[account]).get("AccessToken")
            require(isinstance(token, str) and token, "Retained ordinary acknowledgement has no token")
            expected_hash = hashlib.sha256(token.encode()).hexdigest()
        except Exception as error:
            read_errors.append({"account": account, "path": str(LOGIN_RESPONSES[account]), "failureType": type(error).__name__})
            continue
        for path in sorted(PRIVATE.glob("credential-revocation-*.json")):
            try:
                proof = load(path).get("accounts", {}).get(account, {})
                if proof.get("tokenSha256") == expected_hash and proof.get("invalidityStatus") == 401 and revocation_complete(proof):
                    results[account] = {**proof, "retainedProof": str(path)}
                    break
            except Exception as error:
                read_errors.append({"account": account, "path": str(path), "failureType": type(error).__name__})
    return {"accounts": results, "readErrors": read_errors,
            "allInvalidOrNeverAttempted": all(revocation_complete(row) for row in results.values())}


def cleanup() -> None:
    require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")) and
            os.readlink("/proc/self/ns/net") == os.readlink("/proc/1/ns/net"), "Cleanup requires authorized root SSH in the host namespace")
    canonical(WORK, directory=True, private=True)
    owned_root(ROOT)
    if not PRIVATE.exists():
        PRIVATE.mkdir(mode=0o700)
    canonical(PRIVATE, directory=True, private=True)
    attempt = next((index for index in range(1, 100) if not (PRIVATE / ("cleanup-attempt-" + str(index) + ".json")).exists()), None)
    require(attempt is not None, "Cleanup attempt budget exhausted")
    report_path = PRIVATE / ("cleanup-attempt-" + str(attempt) + ".json")
    report = {"attempt": attempt, "at": utc(), "unit": UNIT, "programData": str(DATA), "dataRemoved": False,
              "evidenceRetained": True, "oldPreservationVerified": False, "ordinaryCredentialsInvalidOrNeverAttempted": False,
              "preStopErrors": []}
    expected = None
    try:
        # Recover only the files needed to attribute a stop before attempting
        # optional credential/READY recovery or historical preservation checks.
        try:
            recover_atomic_publications({"prepare-intent.json", "service-identity.json", "unit-dispatch.json", "launcher-manifest.json"})
        except Exception as error:
            report["preStopErrors"].append({"stage": "stopAuthorityRecovery", "failureType": type(error).__name__})
        if IDENTITY.exists():
            try:
                expected = load(IDENTITY)
            except Exception as error:
                report["preStopErrors"].append({"stage": "processIdentity", "failureType": type(error).__name__})
        try:
            try:
                recover_atomic_publications()
            except Exception as error:
                report["preStopErrors"].append({"stage": "otherAuthorityRecovery", "failureType": type(error).__name__})
            state = properties(UNIT)
            report["credentialRevocation"] = revoke_via_namespace(expected) if int(state.get("MainPID", "0")) > 1 else retained_revocation_proof()
        except Exception as error:
            report["credentialRevocation"] = {"allInvalidOrNeverAttempted": False, "failureType": type(error).__name__}
        finally:
            report["stop"] = stop_owned(expected)
        report["ordinaryCredentialsInvalidOrNeverAttempted"] = report["credentialRevocation"].get("allInvalidOrNeverAttempted") is True
        if not INTENT.exists():
            require(not DATA.exists() and not DATA.is_symlink() and properties(UNIT).get("LoadState") == "not-found",
                    "Incomplete initialization has unexpected fixture resources")
            report.update({"dataAbsent": True, "serviceAbsent": True, "initializationIncomplete": True})
            save(report_path, report)
            emit({"dataAbsent": True, "serviceAbsent": True, "evidenceRetained": True})
            return
        # The application is already stopped before old package, READY or data
        # validation can veto deletion. None of the old paths is a removal target.
        common_preconditions(host=True)
        recover_empty_removal_root()
        intent = load(INTENT)
        require(intent.get("marker") == MARKER and intent.get("unit") == UNIT and intent.get("programData") == str(DATA) and
                intent.get("evidenceRoot") == str(ROOT) and intent.get("mediaCopiesCreated") == 0 and
                intent.get("sourceDirectoriesCreated") == 0 and intent.get("ordinaryLoginBudget") == 2,
                "Cleanup intent does not identify this exact empty observability fixture")
        ready = load(MANIFEST) if MANIFEST.exists() else None
        if ready is not None:
            require(ready.get("state") == "READY" and expected is not None and ready.get("pid") == expected["pid"] and
                    ready.get("startTicks") == expected["startTicks"] and ready.get("programDataIdentity") == load(DATA_IDENTITY),
                    "READY and process/data attestation disagree")
        require(not report["preStopErrors"], "Authority recovery errors require retained evidence review before deletion")
        check_old_services(intent["oldServices"])
        verify_baseline(intent["oldBaseline"])
        verify_package(intent)
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
    emit({key: report[key] for key in ("attempt", "dataRemoved", "evidenceRetained", "oldPreservationVerified", "ordinaryCredentialsInvalidOrNeverAttempted")})
    require(report["dataRemoved"] and report["oldPreservationVerified"] and report["ordinaryCredentialsInvalidOrNeverAttempted"],
            "Cleanup is incomplete; inspect retained private evidence")

def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("prepare", "cleanup", "_bootstrap", "_revoke"))
    options = parser.parse_args()
    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, interrupted)
    try:
        {"prepare": prepare, "cleanup": cleanup, "_bootstrap": bootstrap, "_revoke": revoke_credentials}[options.mode]()
    except Exception as error:
        emit({"mode": options.mode, "result": "failed", "failureType": type(error).__name__})
        raise SystemExit(1)


if __name__ == "__main__":
    main()
