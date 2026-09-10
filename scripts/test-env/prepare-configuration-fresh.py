#!/usr/bin/env python3
"""Prepare or remove one owned empty Emby ConfigurationService fixture.

Run only through authorized root SSH on test-env. Preparation creates exactly
one fresh private namespace, safe initial configuration and two fresh accounts.
Only the two bootstrap logins and their verified logout are issued. No library,
media copy, task, key, full configuration write or named configuration write is
performed. Later configuration experiments require a separate fresh recorder.

The official extracted package stays shared read-only. Failed preparation stops
only its attested new service and retains data/evidence. Explicit cleanup stops
only that exact invocation/process and deletes only the fixed, marked DATA root
after preservation proofs. Evidence, all older studies and all old media remain.
The internal bootstrap mode runs only inside the attested new network namespace.
Exports reuse the pinned deep ConfigurationService URL/header-secret sanitizer.
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
WORK = Path("/opt/goby-test/exec-work-m5g")
ROOT = WORK / "emby-configuration-fresh-m5g-20260910-01"
DATA = WORK / "emby-configuration-fresh-data-01"
PRIVATE, RAW, EXPORT, RUNTIME = ROOT / "private", ROOT / "private/raw", ROOT / "export", ROOT / "runtime"
UNIT = "goby-emby-configuration-fresh-m5g-20260910-01.service"
MARKER = "goby-emby-configuration-fresh-m5g-20260910-01-owned-v1"
PREFIX = "configuration-fresh-setup-m5g-"
DESCRIPTION = "Goby owned fresh Emby configuration fixture M5g 20260910 01"
SERVER_NAME = "Goby Configuration Fresh M5g 20260910 01"
PORT, HTTPS_PORT = 18099, 18499
PACKAGE_ROOT = Path("/dev/shm/goby-emby-reference")
APP = PACKAGE_ROOT / "package/opt/emby-server"
PACKAGE_SHA256 = "1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843"
OLD_DATA = Path("/opt/goby-test/emby-reference-data")
OLD_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
PREVIOUS = Path("/opt/goby-test/exec-scratch/configuration-m5g")
OLD_UNITS = {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3570491}
MAIN_START_TICKS = "24600634"
MAIN_BINARY_SHA256 = "2993870cce6f4e0ae2c3630b645985cab664ce5e5e18123fdc920fb241bb2abf"
ADMIN_NAME = "reference-configuration-fresh-m5g-01"
VIEWER_NAME = "reference-configuration-fresh-viewer-m5g-01"
ADMIN_DEVICE = "goby-configuration-fresh-m5g-20260910-01-bootstrap-admin"
VIEWER_DEVICE = "goby-configuration-fresh-m5g-20260910-01-bootstrap-viewer"
RETIRED_SOURCE = Path("/opt/goby-test/exec-scratch/emby-scheduled-tasks-fresh-m5f-20260910-01/source")
RETIRED_CAPTURE = RETIRED_SOURCE.parent / "runtime/task-capture"
HISTORICAL_REMOVED = [
    "/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-02/.goby-managed",
    "/dev/shm/goby-emby-scheduled-tasks-fresh-m5f-20260910-01",
    str(RETIRED_SOURCE),
]
SANITIZER_SOURCES = {
    "reference-configuration.py": "baa425eb9680d3e924c4d18d75ad99cc6d0a10ac438ba402501955bb96a359cd",
    "reference-scheduled-tasks.py": "b037ab51f5d0fb9e84d4121466a34263ebbf166c99eff2e56cc2dc5b68ada4f1",
    "reference-devices.py": "96d5378d82ce3fcd5dd9a325b02ea3b49bb9c045bcd6b320952222cc888b556d",
    "reference-api-keys.py": "ead25f9d47e8faf0b2b579ca8641b4ddcf4f7993c9e5a16710624c2927e749a8",
}
_CONFIGURATION_MODULE = None
INTENT = PRIVATE / "prepare-intent.json"
IDENTITY = PRIVATE / "service-identity.json"
DISPATCH = PRIVATE / "unit-dispatch.json"
DATA_IDENTITY = PRIVATE / "data-identity.json"
MANIFEST = PRIVATE / "manifest.json"
MIB = 1024 * 1024
MAX_BODY, MAX_WIRE, MAX_REQUESTS = 256 * 1024, 8 * MIB, 128
STATIC_PROPERTIES = ("Id", "Description", "PrivateNetwork", "PrivateTmp", "NoNewPrivileges", "ProtectSystem",
                     "ProtectHome", "WorkingDirectory", "ExecStart", "ControlGroup", "FragmentPath", "DropInPaths",
                     "ReadWritePaths", "ReadOnlyPaths", "MemoryMax", "CPUQuotaPerSecUSec", "TasksMax", "User",
                     "KillMode", "TimeoutStopUSec", "LimitNOFILE", "UMask")
READ_ONLY_PATHS = {str(WORK), str(PACKAGE_ROOT), str(OLD_DATA), "/opt/goby-fixtures", str(PREVIOUS)}
ATOMIC_NAMES = {"prepare-intent.json", "service-identity.json", "unit-dispatch.json", "manifest.json",
                "launcher-manifest.json", "data-identity.json", "data-removal-intent.json"}


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
            "exe": os.readlink(proc / "exe"), "cgroup": (proc / "cgroup").read_text()}


def service_identity(unit: str) -> dict:
    state = properties(unit)
    require(state.get("ActiveState") == "active", "A required service is not active")
    identity = process_identity(int(state.get("MainPID", "0")))
    identity.update({"unit": unit, "serviceProperties": {name: state.get(name, "") for name in STATIC_PROPERTIES}})
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
            digest(Path(main["exe"])) == MAIN_BINARY_SHA256, "The protected M5f Goby process or binary differs")
    main["binarySha256"] = MAIN_BINARY_SHA256
    return result


def check_old_services(expected: dict) -> None:
    require(old_services() == expected, "A protected service identity or configuration changed")


def old_baseline() -> dict:
    previous = load(PREVIOUS / "private/baseline.json")
    audit = load(PREVIOUS / "private/raw/configuration-m5g-audit.json")
    require(audit.get("cleanupPassed") is True and audit.get("captureFailureType") is None and
            audit.get("httpAttempts") == 85 and audit.get("incompleteHTTP") == 0 and
            audit.get("cleanupErrors") == [] and audit.get("persistenceFailures") == [] and
            isinstance(audit.get("checks"), dict) and audit["checks"] and all(value is True for value in audit["checks"].values()) and
            audit.get("serverId") == OLD_SERVER_ID and audit.get("referencePID") == OLD_UNITS["goby-emby-reference.service"],
            "The preceding read-only configuration study does not have proven bounded cleanup")
    for name in ("configurationMutationRequests", "scheduledTaskMutationRequests", "deviceMutationRequests", "applicationKeyRequests",
                 "existingCredentialHTTPRequests", "mediaOrEncoderRequests", "sourceWrites"):
        require(audit.get(name) == 0, "The preceding configuration study escaped its read-only scope")
    require(len(previous.get("records", {})) == 3930 and len(previous.get("media", {})) == 240 and
            len(previous.get("privateFiles", {})) == 2621, "The inherited configuration preservation population differs")
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
    require(len(raw) == len(exported) == 86 and {path.name for path in raw} == {path.name for path in exported} and
            any(path.name == "configuration-m5g-audit.json" for path in raw),
            "The preceding configuration raw/export membership is not eighty-six exact pairs")
    paths.extend(raw + exported)
    require(len(paths) == len(set(paths)) == 4102, "Expected exactly 2051 old raw/export record pairs")
    media = [Path(name) for name in previous["media"]]
    require(len(set(media)) == 240, "Expected exactly 240 surviving old media sources")
    require(previous.get("historicalRemovedPaths") == HISTORICAL_REMOVED and
            all(not Path(name).exists() and not Path(name).is_symlink() for name in HISTORICAL_REMOVED),
            "The exact historical removal exceptions differ")
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
    for folder in {path.parent.parent for path in paths if path.parent.name == "raw"}:
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
    return {"records": {str(path): digest(path) for path in sorted(paths)},
            "media": {str(path): digest(path) for path in sorted(media)},
            "privateFiles": {str(path): digest(path) for path in sorted(private_files)},
            "retainedHistoricalFiles": previous.get("retainedHistoricalFiles", {}),
            "historicalRemovedPaths": HISTORICAL_REMOVED, "retiredOwnedSources": retired}


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
        require(report["MemAvailable"] >= 768 * MIB and report["persistentFreeBytes"] >= 512 * MIB,
                "Fresh configuration fixture needs its bounded memory and persistent-disk reserve")
    else:
        require(report["MemAvailable"] >= 192 * MIB and report["persistentFreeBytes"] >= 128 * MIB,
                "Fresh configuration fixture exhausted its retained headroom")
        require(report["dataBytes"] <= 192 * MIB and report["evidenceBytes"] <= 64 * MIB,
                "Fresh configuration fixture exceeded its bounded data or evidence growth")
    state = properties(UNIT)
    cgroup = state.get("ControlGroup", "")
    if cgroup == "/system.slice/" + UNIT:
        current = Path("/sys/fs/cgroup") / cgroup.lstrip("/") / "memory.current"
        if current.exists():
            report["freshCgroupMemoryBytes"] = int(current.read_text())
    return report


def ensure_work_parent() -> None:
    require(WORK == Path("/opt/goby-test/exec-work-m5g"), "Unexpected configuration work parent")
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
    require(ROOT.parent == DATA.parent == WORK, "Fresh data and evidence escaped their fixed persistent parent")
    canonical(WORK, directory=True, private=True)
    for folder in (PACKAGE_ROOT, OLD_DATA):
        canonical(folder, directory=True, private=True)
        canonical(folder / ".goby-managed", private=True)
        require((folder / ".goby-managed").read_text().strip() == "goby-emby-reference-owned-v1", "Shared reference ownership differs")
    canonical(PACKAGE_ROOT / ".extracted")
    require((PACKAGE_ROOT / ".extracted").read_text().strip() == PACKAGE_SHA256, "Shared package extraction provenance differs")
    canonical(APP / "system/EmbyServer")
    canonical(Path(__file__).resolve())


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


def new_identity() -> dict:
    owned_roots()
    identity = service_identity(UNIT)
    settings, args = identity["serviceProperties"], identity["cmdline"]
    require(identity["uid"] == 0 and identity["exe"] == str(APP / "system/EmbyServer"), "Fresh service executable or UID differs")
    require(args.count("-programdata") == 1 and args[args.index("-programdata") + 1] == str(DATA),
            "Fresh service program-data argument differs")
    require(settings["Description"] == DESCRIPTION and settings["WorkingDirectory"] == str(RUNTIME) and
            str(RUNTIME / "launch.sh") in settings["ExecStart"] and settings["MemoryMax"] == str(512 * MIB) and
            settings["PrivateNetwork"] == "yes" and settings["PrivateTmp"] == "yes" and
            settings["NoNewPrivileges"] == "yes" and settings["ProtectSystem"] == "strict" and settings["ProtectHome"] == "yes" and
            settings["KillMode"] == "control-group" and settings["TasksMax"] == "256" and settings["LimitNOFILE"] == "65536" and
            settings["UMask"] == "0077" and settings["TimeoutStopUSec"] == "25s" and
            settings["CPUQuotaPerSecUSec"] in {"1.5s", "1.500000s", "1500ms", "1s 500ms"} and settings["User"] in {"", "root"},
            "Fresh service sandbox or launcher differs")
    require(set(settings["ReadWritePaths"].split()) == {str(DATA), str(RUNTIME)}, "Fresh service writable paths differ")
    require(set(settings["ReadOnlyPaths"].split()) == READ_ONLY_PATHS, "Shared tmpfs and reference paths are not explicitly read-only")
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
    return identity


def same_new_identity(expected: dict) -> None:
    require(new_identity() == expected, "Fresh service identity changed")


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
        device = ADMIN_DEVICE if account == "admin" else VIEWER_DEVICE
        return ('Emby Client="Goby Fresh Reference Bootstrap", Device="Linux Fresh Test", '
                'DeviceId="' + device + '", Version="0.1.0"')

    def request(self, label: str, method: str, route: str, *, body: object = None, token: str = "",
                account: str = "", form: bool = False, retry_readiness: bool = False) -> tuple[int | None, object]:
        require(time.monotonic() < self.deadline and self.count < MAX_REQUESTS, "Fresh bootstrap request budget exhausted")
        same_new_identity(self.identity)
        require(os.readlink("/proc/self/ns/net") == self.identity["networkNamespace"], "HTTP is outside the fresh namespace")
        routes = {"GET": {"/emby/System/Info/Public", "/emby/Startup/User", "/emby/Users", "/emby/Devices", "/emby/System/Configuration",
                           "/emby/Library/VirtualFolders/Query", "/emby/Sessions"},
                  "POST": {"/emby/Startup/User", "/emby/Startup/RemoteAccess", "/emby/Startup/Complete",
                            "/emby/Users/AuthenticateByName", "/emby/Users/New", "/emby/Sessions/Logout"}}
        require(route in routes.get(method, set()), "Bootstrap route is outside the exact setup allowlist")
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
        name = PREFIX + str(self.count).zfill(3) + "-" + label
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
        self.complete_count += int(complete)
        self.incomplete_count += int(not complete)
        # Acknowledgement precedes all DTO checks so failed assertions retain
        # the exact newly issued credential and its cleanup responsibility.
        if isinstance(parsed, dict) and isinstance(parsed.get("AccessToken"), str) and parsed["AccessToken"]:
            require(account in {"admin", "viewer"}, "Unexpected bootstrap credential response")
            self.tokens[account] = parsed["AccessToken"]
            self.secrets.add(parsed["AccessToken"])
            save(PRIVATE / (account + "-bootstrap-login.json"), parsed)
        record = {"reference": {"product": "Emby Server", "version": "4.9.5.0", "capturedAt": started},
                  "request": {"method": method, "path": route, "headers": list(headers.items()), "body": recorded_body},
                  "response": {"status": status_code, "headers": response_headers, "bodyType": kind, "body": parsed},
                  "observation": {"completeHTTP": complete, "captureIncomplete": not complete,
                                  "failureType": failure, "wireBytes": len(content), "freshFixtureOnly": True}}
        save(RAW / (name + ".json"), record)
        save(EXPORT / (name + ".json"), self.sanitize(record))
        save(PRIVATE / "wire" / (name + ".b64"), base64.b64encode(content).decode() + "\n")
        self.last_record = name
        print(json.dumps({"stage": label, "status": status_code, "completeHTTP": complete, "record": self.count}), flush=True)
        require(self.wire <= MAX_WIRE, "Bootstrap wire-byte budget exhausted")
        if not retry_readiness:
            require(complete, "Fresh bootstrap HTTP response was incomplete")
        return status_code, parsed

    def logout(self, account: str) -> None:
        token = self.tokens[account]
        status_code, _ = self.request(account + "-logout", "POST", "/emby/Sessions/Logout", token=token)
        require(status_code == 204, "Fresh bootstrap logout status differs from the recorded contract")
        status_code, _ = self.request(account + "-logout-invalid", "GET", "/emby/Sessions", token=token)
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
                isinstance(public.get("Id"), str) and public["Id"] and public["Id"] != OLD_SERVER_ID,
                "Fresh public identity is wrong or collides with the old reference")
        self.server_id = public["Id"]
        save(PRIVATE / "fresh-public-identity.json", public)
        password = secrets.token_hex(32)
        self.secrets.add(password)
        save(PRIVATE / "admin-credentials.env", "REFERENCE_USERNAME=" + ADMIN_NAME + "\nREFERENCE_PASSWORD=" + password + "\n")
        save(PRIVATE / "viewer-credentials.env", "REFERENCE_USERNAME=" + VIEWER_NAME + "\nREFERENCE_PASSWORD=\n")
        status_code, startup = self.request("startup-user-get", "GET", "/emby/Startup/User", account="admin")
        require(status_code == 200 and isinstance(startup, dict) and isinstance(startup.get("Name"), str), "Fresh startup user is unavailable")
        status_code, result = self.request("startup-user-post", "POST", "/emby/Startup/User", account="admin", form=True,
                                           body={"Name": ADMIN_NAME, "Password": password})
        require(status_code == 200 and result == {}, "Fresh startup account creation differs")
        status_code, _ = self.request("startup-remote-access", "POST", "/emby/Startup/RemoteAccess", account="admin", form=True,
                                      body={"EnableAutomaticPortMapping": "false"})
        require(status_code == 204, "Fresh startup remote-access configuration differs")
        status_code, _ = self.request("startup-complete", "POST", "/emby/Startup/Complete", account="admin")
        require(status_code == 204, "Fresh startup completion differs")
        status_code, admin = self.request("admin-login", "POST", "/emby/Users/AuthenticateByName", account="admin",
                                          body={"Username": ADMIN_NAME, "Pw": password})
        require(status_code == 200 and "admin" in self.tokens and isinstance(admin, dict) and admin.get("ServerId") == self.server_id and
                admin.get("User", {}).get("Name") == ADMIN_NAME and admin["User"].get("Policy", {}).get("IsAdministrator") is True,
                "Fresh administrator login or role differs")
        admin_token = self.tokens["admin"]
        status_code, viewer = self.request("viewer-create", "POST", "/emby/Users/New", token=admin_token, body={"Name": VIEWER_NAME})
        require(status_code == 200 and isinstance(viewer, dict) and isinstance(viewer.get("Id"), str) and viewer["Id"] and
                viewer.get("Name") == VIEWER_NAME and viewer.get("HasPassword") is False and viewer.get("HasConfiguredPassword") is False and
                viewer.get("Policy", {}).get("IsAdministrator") is False, "Fresh viewer creation or password flags differ")
        save(PRIVATE / "viewer-created.json", viewer)
        time.sleep(1.1)
        status_code, viewer_login = self.request("viewer-empty-password-login", "POST", "/emby/Users/AuthenticateByName", account="viewer",
                                                 body={"Username": VIEWER_NAME, "Pw": ""})
        # This is a new bounded observation. Historical fixtures establish
        # password flags after Users/New, but do not establish empty-Pw login.
        require(status_code == 200 and "viewer" in self.tokens and isinstance(viewer_login, dict) and
                viewer_login.get("ServerId") == self.server_id and viewer_login.get("User", {}).get("Id") == viewer["Id"] and
                viewer_login["User"].get("Policy", {}).get("IsAdministrator") is False,
                "Fresh empty-password viewer login was not established; no password fallback is permitted")
        self.logout("viewer")
        status_code, users = self.request("bootstrap-final-users", "GET", "/emby/Users", token=admin_token)
        require(status_code == 200 and isinstance(users, list) and len(users) == 2 and
                {row.get("Name") for row in users if isinstance(row, dict)} == {ADMIN_NAME, VIEWER_NAME}, "Fresh final user membership differs")
        status_code, devices = self.request("bootstrap-final-devices", "GET", "/emby/Devices", token=admin_token)
        require(status_code == 200 and isinstance(devices, dict) and isinstance(devices.get("Items"), list) and
                len(devices["Items"]) <= 16 and all(isinstance(row, dict) and isinstance(row.get("Id"), str) and row["Id"] for row in devices["Items"]),
                "Fresh final device membership is unavailable or unbounded")
        status_code, configuration = self.request("bootstrap-safe-configuration", "GET", "/emby/System/Configuration", token=admin_token)
        require(status_code == 200 and isinstance(configuration, dict), "Fresh safe configuration is unavailable")
        for name in ("EnableHttps", "EnableUPnP", "EnableRemoteAccess", "EnableAutoUpdate", "EnableAutomaticRestart", "AutoRunWebApp"):
            require(configuration.get(name) is False, "Fresh configuration enabled an unsafe startup capability")
        require(configuration.get("HttpServerPortNumber") == PORT and configuration.get("PublicPort") == PORT and
                configuration.get("HttpsPortNumber") == HTTPS_PORT and configuration.get("PublicHttpsPort") == HTTPS_PORT and
                configuration.get("ServerName") == SERVER_NAME and configuration.get("LocalNetworkAddresses") == ["127.0.0.1"] and
                configuration.get("IsStartupWizardCompleted") is True, "Fresh loopback ports, name or completed setup differs")
        configuration_record = str(RAW / (self.last_record + ".json"))
        status_code, libraries = self.request("bootstrap-final-libraries", "GET", "/emby/Library/VirtualFolders/Query", token=admin_token)
        require(status_code == 200 and isinstance(libraries, dict) and libraries.get("Items") == [],
                "Fresh bootstrap unexpectedly contains a media library")
        self.logout("admin")
        require(self.revoked == {"admin", "viewer"}, "Bootstrap credentials were not both invalidated")
        for raw in sorted(RAW.glob(PREFIX + "*.json")):
            require(self.sanitize(load(raw)) == load(EXPORT / raw.name), "Fresh setup export redaction audit failed")
        return {"serverId": self.server_id, "bootstrapDevices": devices["Items"], "bootstrapUsers": users,
                "bootstrapLibraries": libraries["Items"], "bootstrapLibraryMutationRequests": 0,
                "bootstrapConfigurationRecord": configuration_record, "bootstrapConfigurationSafetyVerified": True,
                "bootstrapConfigurationMutationRequests": 0, "bootstrapNamedConfigurationRequests": 0,
                "bootstrapTaskRequests": 0, "bootstrapTaskMutationRequests": 0, "bootstrapApplicationKeyRequests": 0,
                "viewerEmptyPasswordLoginVerified": True, "bootstrapCredentialsRevoked": True,
                "bootstrapCredentialsInvalidity": {"admin": 401, "viewer": 401}, "setupRecordCount": self.count,
                "setupCompleteHTTPResponses": self.complete_count, "setupIncompleteAttempts": self.incomplete_count,
                "wireBytes": self.wire, "bootstrapUserIds": {"admin": admin["User"]["Id"], "viewer": viewer["Id"]}}


def bootstrap() -> None:
    common_preconditions(host=False)
    owned_roots()
    intent, identity = load(INTENT), load(IDENTITY)
    require(intent.get("marker") == MARKER and intent.get("operatorSha256") == digest(Path(__file__).resolve()) and
            intent.get("sanitizerSources") == SANITIZER_SOURCES, "Fresh bootstrap intent or sanitizer provenance differs")
    same_new_identity(identity)
    require(os.readlink("/proc/self/ns/net") == identity["networkNamespace"], "Bootstrap must enter the attested fresh namespace")
    signal.signal(signal.SIGALRM, deadline_expired)
    recorder = Bootstrap(identity)
    try:
        result = recorder.capture()
        save(PRIVATE / "bootstrap-result.json", result)
    except Exception as error:
        # The host supervisor stops this wholly owned instance on failure.
        # Raw responses and any acknowledged credentials remain private.
        save(PRIVATE / "bootstrap-failure.json", {"failureType": type(error).__name__, "message": str(error),
             "records": recorder.count, "acknowledgedAccounts": sorted(recorder.tokens), "revokedAccounts": sorted(recorder.revoked)})
        raise


def write_configuration() -> None:
    save(DATA / "config/system.xml", f'''<?xml version="1.0" encoding="utf-8"?>
<ServerConfiguration xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <HttpServerPortNumber>18099</HttpServerPortNumber><PublicPort>18099</PublicPort>
  <HttpsPortNumber>18499</HttpsPortNumber><PublicHttpsPort>18499</PublicHttpsPort>
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
cd "$APP_DIR"
exec "$APP_DIR/system/EmbyServer" -programdata "$EMBY_DATA" \\
  -ffdetect "$APP_DIR/bin/ffdetect" -ffmpeg "$APP_DIR/bin/ffmpeg" -ffprobe "$APP_DIR/bin/ffprobe" \\
  -restartexitcode 3 -updatepackage 'emby-server-deb_{version}_amd64.deb'
'''.replace("<OWNED_PROGRAM_DATA>", str(DATA)), mode=0o700)
    save(RUNTIME / "service.log", "")


def start_service() -> None:
    settings = {"Description": DESCRIPTION, "PrivateNetwork": "yes", "PrivateTmp": "yes", "NoNewPrivileges": "yes",
                "ProtectSystem": "strict", "ProtectHome": "yes", "ReadWritePaths": str(DATA) + " " + str(RUNTIME),
                "ReadOnlyPaths": " ".join(sorted(READ_ONLY_PATHS)),
                "CPUQuota": "150%", "MemoryMax": "512M", "TasksMax": "256", "LimitNOFILE": "65536",
                "TimeoutStopSec": "25", "KillMode": "control-group", "WorkingDirectory": str(RUNTIME), "UMask": "0077",
                "StandardOutput": "append:" + str(RUNTIME / "service.log"), "StandardError": "append:" + str(RUNTIME / "service.log")}
    run(["systemd-run", "--unit=" + UNIT, "--collect", *["--property=" + key + "=" + value for key, value in settings.items()],
         str(RUNTIME / "launch.sh")])


def dispatch_identity(state: dict | None = None) -> dict:
    """Attest unit ownership before the final Emby process becomes ready."""
    owned_roots()
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
    owned_roots()
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
    return {"stopped": True, "ownedProcessGone": True, "unitMainPID": 0, "finalActiveState": final.get("ActiveState")}


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
            "Fresh configuration fixture paths already exist")
    require(properties(UNIT).get("LoadState") == "not-found", "Fresh configuration fixture unit already exists")
    before_services, before_files = old_services(), old_baseline()
    initial_resources = resources(admission=True)
    os.umask(0o077)
    intent = {"schemaVersion": 1, "state": "PREPARING", "marker": MARKER, "unit": UNIT, "programData": str(DATA),
              "evidenceRoot": str(ROOT), "port": PORT, "httpsPort": HTTPS_PORT, "startedAt": utc(),
              "operatorSha256": digest(Path(__file__).resolve()), "sanitizerSources": SANITIZER_SOURCES,
              "oldServices": before_services, "oldBaseline": before_files, "initialResources": initial_resources,
              "packageSha256": PACKAGE_SHA256, "sharedServerBinarySha256": digest(APP / "system/EmbyServer"),
              "mediaCopiesCreated": 0, "configurationMutationPhase": "Separate future recorder only",
              "limits": {"serviceMemoryBytes": 512 * MIB, "admissionAvailableMemoryBytes": 768 * MIB,
                         "minimumAvailableMemoryBytes": 192 * MIB, "dataGrowthBytes": 192 * MIB,
                         "evidenceGrowthBytes": 64 * MIB, "minimumPersistentFreeBytes": 128 * MIB,
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
        listeners = listener_observation(identity)
        check_old_services(before_services)
        verify_baseline(before_files)
        verify_package(intent)
        samples.append(resources())
        save(PRIVATE / "resource-observations.json", {"samples": samples})
        require(len(list(RAW.glob(PREFIX + "*.json"))) == bootstrap_result["setupRecordCount"] and
                len(list(EXPORT.glob(PREFIX + "*.json"))) == bootstrap_result["setupRecordCount"], "Bootstrap record membership differs")
        manifest = {**intent, **identity, **bootstrap_result, "state": "READY", "readyAt": utc(),
                    "programDataIdentity": load(DATA_IDENTITY), "tcpListeners": listeners,
                    "setupRawRoot": str(RAW), "setupExportRoot": str(EXPORT), "setupPrefix": PREFIX,
                    "adminCredentialsFile": str(PRIVATE / "admin-credentials.env"),
                    "viewerCredentialsFile": str(PRIVATE / "viewer-credentials.env"),
                    "oldPreservationVerified": True, "allFreshProgramDataOwned": True,
                    "bootstrapDeviceSnapshotTiming": "After viewer logout, before final administrator logout"}
        atomic_save(MANIFEST, manifest)
        status = {"state": "READY", "unit": UNIT, "pid": identity["pid"], "serverId": bootstrap_result["serverId"],
                  "port": PORT, "setupRecordCount": bootstrap_result["setupRecordCount"], "oldPreservationVerified": True,
                  "bootstrapCredentialsRevoked": True, "mediaCopiesCreated": 0, "configurationMutationRequests": 0}
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
        if PRIVATE.exists():
            try:
                owned_root(ROOT)
                recover_atomic_publications()
            except Exception as publication_error:
                preservation_errors.append({"check": "authorityPublicationRecovery", "failureType": type(publication_error).__name__})
        if started_service:
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
                     "cleanup": cleanup_result, "preservationErrors": preservation_errors, "dataRetained": DATA.exists(), "resourceSamples": samples})
                saved = True
            except Exception:
                pass
        print(json.dumps({"state": "FAILED", "failureType": type(error).__name__, "ownedServiceStopped": cleanup_result.get("stopped"),
                          "oldPreservationVerified": not preservation_errors, "privateEvidenceRetained": PRIVATE.exists(),
                          "failureReportSaved": saved}), flush=True)
        raise SystemExit(1)


def removal_proof(folder: Path) -> tuple[Path, dict]:
    require(folder == DATA and folder.parent == WORK, "Unexpected configuration fixture removal root")
    current = data_identity()
    require(load(DATA_IDENTITY) == current, "Program-data root no longer matches its immutable creation identity")
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
    if not DATA.exists() or (DATA / ".goby-managed").exists():
        return
    info = canonical(DATA, directory=True, private=True)
    proof = {"path": str(DATA), "device": info.st_dev, "inode": info.st_ino, "marker": MARKER}
    require(load(DATA_IDENTITY) == proof and load(PRIVATE / "data-removal-intent.json") == proof and
            not os.path.ismount(DATA) and not list(DATA.iterdir()), "Unmarked data root is not the attested empty removal directory")
    state = properties(UNIT)
    require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
            "An empty removal directory still has a service process")
    save(DATA / ".goby-managed", MARKER + "\n")


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
    report = {"attempt": attempt, "at": utc(), "unit": UNIT, "programData": str(DATA),
              "dataRemoved": False, "evidenceRetained": True, "oldPreservationVerified": False}
    try:
        if not INTENT.exists():
            require(not DATA.exists() and not DATA.is_symlink() and properties(UNIT).get("LoadState") == "not-found",
                    "Incomplete initialization has unexpected fixture resources")
            report.update({"dataAbsent": True, "serviceAbsent": True, "initializationIncomplete": True})
            save(report_path, report)
            print(json.dumps({"dataAbsent": True, "serviceAbsent": True, "evidenceRetained": True}), flush=True)
            return
        intent = load(INTENT)
        require(intent.get("marker") == MARKER and intent.get("unit") == UNIT and intent.get("programData") == str(DATA) and
                intent.get("evidenceRoot") == str(ROOT) and intent.get("mediaCopiesCreated") == 0,
                "Cleanup intent does not identify this exact empty configuration fixture")
        expected = load(IDENTITY) if IDENTITY.exists() else None
        ready = load(MANIFEST) if MANIFEST.exists() else None
        if ready is not None:
            require(ready.get("state") == "READY" and expected is not None and ready.get("pid") == expected["pid"] and
                    ready.get("startTicks") == expected["startTicks"] and ready.get("programDataIdentity") == load(DATA_IDENTITY),
                    "Ready manifest and process/data attestation disagree")
        if DATA.exists():
            report["stop"] = stop_owned(expected)
        else:
            state = properties(UNIT)
            require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
                    "Absent program data still has an active service")
            if expected is not None and (Path("/proc") / str(expected["pid"])).exists():
                require(process_identity(expected["pid"])["startTicks"] != expected["startTicks"], "Owned process is still present")
            report["stop"] = {"stopped": True, "ownedProcessGone": True, "unitMainPID": 0, "finalActiveState": state.get("ActiveState")}
        # Stop the independently owned invocation before a historical comparison
        # can veto deletion. No old file or evidence root is a removal candidate.
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
    print(json.dumps({key: report[key] for key in ("attempt", "dataRemoved", "evidenceRetained", "oldPreservationVerified")}), flush=True)
    require(report["dataRemoved"] and report["oldPreservationVerified"], "Cleanup is incomplete; inspect retained private evidence")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("prepare", "cleanup", "_bootstrap"))
    options = parser.parse_args()
    for signum in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(signum, interrupted)
    try:
        {"prepare": prepare, "cleanup": cleanup, "_bootstrap": bootstrap}[options.mode]()
    except Exception as error:
        # Never expose exception details or the child's private console log.
        print(json.dumps({"mode": options.mode, "result": "failed", "failureType": type(error).__name__}), flush=True)
        raise SystemExit(1)


if __name__ == "__main__":
    main()

