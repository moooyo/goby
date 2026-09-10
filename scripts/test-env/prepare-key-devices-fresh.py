#!/usr/bin/env python3
"""Prepare or remove one entirely owned, isolated official Emby fixture.

Run only through authorized root SSH on test-env. The official extracted
package is shared read-only; no existing account, database, credential, service,
or package is changed. Failed attempts retain their data and private evidence.
Preparation stops its own service on failure. Explicit cleanup removes only
the owned program-data directory, after process identity and preservation checks.
The internal bootstrap mode is invoked only in the new service's namespace.
GOBY_FRESH_KEY_DEVICES_RUN accepts only canonical 1 or 2, defaulting to 1.
Run 2 retains and audits the failed first attempt; it never reuses its paths.
"""

from __future__ import annotations

import argparse
import base64
import datetime as dt
import hashlib
import http.client
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
from urllib.parse import quote, urlencode

sys.dont_write_bytecode = True


def selected_run(value: str) -> int:
    if not isinstance(value, str) or value not in {"1", "2"}:
        raise ValueError("GOBY_FRESH_KEY_DEVICES_RUN must be canonical 1 or 2")
    return int(value)


REFERENCE_RUN = selected_run(os.environ.get("GOBY_FRESH_KEY_DEVICES_RUN", "1"))
RUN_LABEL = str(REFERENCE_RUN).zfill(2)
ROOT = Path("/opt/goby-test/exec-scratch/emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL)
DATA = Path("/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL)
PRIVATE, RAW, EXPORT, RUNTIME = ROOT / "private", ROOT / "private/raw", ROOT / "export", ROOT / "runtime"
UNIT = "goby-emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL + ".service"
MARKER = "goby-emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL + "-owned-v1"
PREFIX = "key-devices-fresh-setup-m5e-" if REFERENCE_RUN == 1 else "key-devices-fresh-setup-m5e-2-"
DESCRIPTION = "Goby owned fresh Emby key-device fixture M5e 20260910 " + RUN_LABEL
SERVER_NAME = "Goby Key Devices Fresh M5e 20260910 " + RUN_LABEL
PORT = 18098
PACKAGE_ROOT = Path("/dev/shm/goby-emby-reference")
APP = PACKAGE_ROOT / "package/opt/emby-server"
PACKAGE_SHA256 = "1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843"
OLD_DATA = Path("/opt/goby-test/emby-reference-data")
OLD_SERVER_ID = "ec69ef1cf84140e88489c30326529308"
PREVIOUS = Path("/opt/goby-test/exec-scratch/key-devices-m5e")
OLD_UNITS = {"goby-emby-reference.service": 3131777, "goby-foundation-test.service": 3494032}
ADMIN_NAME = "reference-key-devices-fresh-m5e-" + RUN_LABEL
VIEWER_NAME = "reference-key-devices-fresh-viewer-m5e-" + RUN_LABEL
ADMIN_DEVICE = "goby-key-devices-fresh-m5e-20260910-" + RUN_LABEL + "-bootstrap-admin"
VIEWER_DEVICE = "goby-key-devices-fresh-m5e-20260910-" + RUN_LABEL + "-bootstrap-viewer"
FIRST_ROOT = Path("/opt/goby-test/exec-scratch/emby-key-devices-fresh-m5e-20260910-01")
FIRST_DATA = Path("/dev/shm/goby-emby-key-devices-fresh-m5e-20260910-01")
FIRST_UNIT = "goby-emby-key-devices-fresh-m5e-20260910-01.service"
FIRST_MARKER = "goby-emby-key-devices-fresh-m5e-20260910-01-owned-v1"
FIRST_OPERATOR_SHA256 = "8f18f8f7603c935db626df2b62a8d5a4ea0ce9b9a3646346f9f954775703dd35"
INTENT = PRIVATE / "prepare-intent.json"
IDENTITY = PRIVATE / "service-identity.json"
DISPATCH = PRIVATE / "unit-dispatch.json"
MANIFEST = PRIVATE / "manifest.json"
MIB = 1024 * 1024
MAX_BODY, MAX_WIRE, MAX_REQUESTS = 256 * 1024, 8 * MIB, 128
STATIC_PROPERTIES = ("Id", "Description", "PrivateNetwork", "PrivateTmp", "NoNewPrivileges", "ProtectSystem",
                     "ProtectHome", "WorkingDirectory", "ExecStart", "ControlGroup", "FragmentPath", "DropInPaths",
                     "ReadWritePaths", "ReadOnlyPaths", "MemoryMax", "CPUQuotaPerSecUSec", "TasksMax", "User",
                     "KillMode", "TimeoutStopUSec", "LimitNOFILE", "UMask")
READ_ONLY_PATHS = {str(DATA.parent), str(PACKAGE_ROOT), str(OLD_DATA)}


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
             {"at": utc(), "referenceRun": REFERENCE_RUN, "arguments": args, "timeoutSeconds": timeout,
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
    return result


def check_old_services(expected: dict) -> None:
    require(old_services() == expected, "A protected service identity or configuration changed")


def old_baseline() -> dict:
    previous = load(PREVIOUS / "private/baseline.json")
    audit = load(PREVIOUS / "private/raw/key-devices-m5e-audit.json")
    require(audit.get("cleanupPassed") is True and audit.get("httpAttempts") == 61 and
            audit.get("incompleteHTTP") == 0 and audit.get("ownedKeysAcknowledged") == 0 and
            audit.get("serverCreationGate", {}).get("allowed") is False and
            audit.get("serverCreationGate", {}).get("reportedServerId") == OLD_SERVER_ID,
            "The preceding read-only key-device gate capture does not have proven cleanup")
    paths = [Path(name) for name in previous["records"]]
    for folder in (PREVIOUS / "private/raw", PREVIOUS / "export"):
        canonical(folder, directory=True, private=True)
        paths.extend(folder.glob("*.json"))
    require(len(paths) == 3076 and len(set(paths)) == 3076, "Expected exactly 1538 old raw/export record pairs")
    media = [Path(name) for name in previous["media"]]
    require(len(media) == 240 and len(set(media)) == 240, "Expected exactly 240 old source files")
    private_roots = {path.parent.parent for path in paths if path.parent.name == "raw"}
    private_files: set[Path] = set()
    for folder in private_roots:
        canonical(folder, directory=True, private=True)
        for path in folder.rglob("*"):
            require(not path.is_symlink(), "A protected private tree contains a symbolic link")
            if path.is_file():
                private_files.add(path)
    require(len(private_files) < 4096 and sum(path.stat().st_size for path in private_files) < 128 * MIB and
            sum(path.stat().st_size for path in media) < 32 * MIB, "The preservation baseline exceeds its bounds")
    for path in [*paths, *media, *private_files]:
        canonical(path)
    return {"records": {str(path): digest(path) for path in sorted(paths)},
            "media": {str(path): digest(path) for path in sorted(media)},
            "privateFiles": {str(path): digest(path) for path in sorted(private_files)}}


def verify_baseline(expected: dict) -> None:
    require(old_baseline() == expected, "Old records, source files, or private evidence changed")


def previous_failure_provenance() -> dict | None:
    if REFERENCE_RUN == 1:
        return None
    canonical(FIRST_ROOT, directory=True, private=True)
    canonical(FIRST_ROOT / ".goby-managed", private=True)
    require((FIRST_ROOT / ".goby-managed").read_text().strip() == FIRST_MARKER,
            "The first fresh attempt ownership marker differs")
    require(not FIRST_DATA.exists() and not FIRST_DATA.is_symlink() and properties(FIRST_UNIT).get("LoadState") == "not-found",
            "Run 2 requires the first fresh data and unit to be absent")
    prior_private = FIRST_ROOT / "private"
    prior_intent = load(prior_private / "prepare-intent.json")
    failure_path = prior_private / "prepare-failure.json"
    failure = load(failure_path)
    require(prior_intent.get("operatorSha256") == FIRST_OPERATOR_SHA256 and prior_intent.get("unit") == FIRST_UNIT and
            prior_intent.get("programData") == str(FIRST_DATA) and prior_intent.get("marker") == FIRST_MARKER,
            "The first attempt does not match the executed operator and fixed resources")
    require(failure.get("cleanup", {}).get("stopped") is True and failure.get("preservationErrors") == [],
            "The first preparation failure lacks proven service cleanup or preservation")
    for folder in (prior_private / "raw", FIRST_ROOT / "export"):
        canonical(folder, directory=True, private=True)
        require(not list(folder.iterdir()), "The first preparation unexpectedly contains HTTP capture evidence")
    require(not any((prior_private / name).exists() for name in
                    ("manifest.json", "service-identity.json", "bootstrap-result.json", "fresh-public-identity.json")),
            "The first attempt reached process identity or bootstrap unexpectedly")
    cleanup_paths = sorted(prior_private.glob("cleanup-attempt-*.json"))
    require(1 <= len(cleanup_paths) <= 10, "The first attempt cleanup evidence is absent or unbounded")
    successful = []
    for path in cleanup_paths:
        require(re.fullmatch(r"cleanup-attempt-[1-9][0-9]?\.json", path.name), "The first cleanup attempt name is invalid")
        record = load(path)
        if record.get("dataRemoved") is True and record.get("oldPreservationVerified") is True and record.get("evidenceRetained") is True:
            require(record.get("unit") == FIRST_UNIT and record.get("programData") == str(FIRST_DATA) and
                    record.get("stop", {}).get("stopped") is True, "The successful first cleanup has ambiguous ownership")
            successful.append(path)
    require(len(successful) == 1, "The first owned data removal has no unique successful cleanup report")
    entries = list(FIRST_ROOT.rglob("*"))
    require(len(entries) <= 256, "The first attempt evidence membership exceeds its finite bound")
    files = []
    for path in entries:
        require(path.resolve(strict=True) == path and FIRST_ROOT in path.parents and not path.is_symlink(),
                "The first attempt contains a noncanonical evidence path")
        entry = path.lstat()
        require(entry.st_uid == 0 and stat.S_IMODE(entry.st_mode) & 0o077 == 0 and
                (stat.S_ISREG(entry.st_mode) or stat.S_ISDIR(entry.st_mode)), "The first attempt evidence is not root-private")
        if stat.S_ISREG(entry.st_mode):
            require(entry.st_nlink == 1, "The first attempt evidence contains a hard link")
            files.append(path)
    total = sum(path.stat().st_size for path in files)
    require(total <= 16 * MIB, "The first attempt evidence exceeds its byte bound")
    source_paths = [path for path in files if path == FIRST_ROOT / "private/operator-source-attempt-1.py"]
    require(len(source_paths) == 1 and digest(source_paths[0]) == FIRST_OPERATOR_SHA256,
            "The immutable executed first-attempt operator source is missing or differs")
    return {"referenceRun": 1, "evidenceRoot": str(FIRST_ROOT), "unit": FIRST_UNIT, "programData": str(FIRST_DATA),
            "outcome": "initialization-failed-before-http", "operatorSource": str(source_paths[0]),
            "operatorSha256": FIRST_OPERATOR_SHA256, "failureRecord": str(failure_path), "cleanupRecord": str(successful[0]),
            "setupRecordCount": 0, "files": {str(path): digest(path) for path in sorted(files)},
            "fileCount": len(files), "totalBytes": total}


def verify_previous_failure(expected: dict | None) -> None:
    require(previous_failure_provenance() == expected, "The first fresh attempt evidence changed")


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
    report = {"at": utc(), **memory, "scratchFreeBytes": shutil.disk_usage(ROOT.parent).free,
              "shmFreeBytes": shutil.disk_usage(DATA.parent).free,
              "dataBytes": tree_size(DATA), "evidenceBytes": tree_size(ROOT)}
    if admission:
        require(report["MemAvailable"] >= 768 * MIB, "Fresh reference needs at least 768 MiB available memory")
        require(report["scratchFreeBytes"] >= 96 * MIB and report["shmFreeBytes"] >= 192 * MIB,
                "Fresh reference scratch or tmpfs headroom is insufficient")
    else:
        require(report["MemAvailable"] >= 192 * MIB and report["scratchFreeBytes"] >= 32 * MIB and
                report["shmFreeBytes"] >= 64 * MIB, "Fresh reference exhausted its retained headroom")
        require(report["dataBytes"] <= 192 * MIB and report["evidenceBytes"] <= 64 * MIB,
                "Fresh reference exceeded its bounded data or evidence growth")
    state = properties(UNIT)
    cgroup = state.get("ControlGroup", "")
    if cgroup.startswith("/system.slice/") and cgroup.endswith(UNIT):
        current = Path("/sys/fs/cgroup") / cgroup.lstrip("/") / "memory.current"
        if current.exists():
            report["freshCgroupMemoryBytes"] = int(current.read_text())
    return report


def common_preconditions(*, host: bool) -> None:
    require(sys.platform == "linux" and os.geteuid() == 0 and bool(os.environ.get("SSH_CONNECTION")),
            "Run only through authorized root SSH on test-env")
    if host:
        require(os.readlink("/proc/self/ns/net") == os.readlink("/proc/1/ns/net"), "Operator must begin in the host network namespace")
    canonical(ROOT.parent, directory=True)
    canonical(DATA.parent, directory=True)
    scratch_marker = ROOT.parent.parent / "exec-scratch.owner"
    canonical(scratch_marker)
    require(scratch_marker.read_text().strip() == "goby-verification-scratch", "Execution scratch marker differs")
    for folder in (PACKAGE_ROOT, OLD_DATA):
        canonical(folder, directory=True, private=True)
        canonical(folder / ".goby-managed", private=True)
        require((folder / ".goby-managed").read_text().strip() == "goby-emby-reference-owned-v1", "Shared reference ownership differs")
    canonical(PACKAGE_ROOT / ".extracted")
    require((PACKAGE_ROOT / ".extracted").read_text().strip() == PACKAGE_SHA256, "Shared package extraction provenance differs")
    canonical(APP / "system/EmbyServer")
    canonical(Path(__file__).resolve())


def owned_root(folder: Path) -> None:
    require(folder in {ROOT, DATA}, "Unexpected owned root")
    canonical(folder, directory=True, private=True)
    canonical(folder / ".goby-managed", private=True)
    require((folder / ".goby-managed").read_text().strip() == MARKER, "Fresh fixture ownership marker differs")


def create_owned_root(folder: Path) -> None:
    require(folder in {ROOT, DATA} and not folder.exists() and not folder.is_symlink(), "Owned root already exists")
    folder.mkdir(mode=0o700)
    created_inode = folder.stat().st_ino
    try:
        save(folder / ".goby-managed", MARKER + "\n")
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
            settings["UMask"] == "0077",
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
        self.count, self.wire = 0, 0
        self.complete_count, self.incomplete_count = 0, 0
        self.server_id: str | None = None
        self.deadline = time.monotonic() + 180
        self.revoked: set[str] = set()

    def sanitize(self, value: object, field: str = "") -> object:
        if isinstance(value, dict):
            return {key: self.sanitize(item, key) for key, item in value.items()}
        if isinstance(value, list):
            if field == "headers":
                return [[key, "[REDACTED_HEADER]" if any(term in re.sub(r"[^a-z]", "", key.lower())
                        for term in ("authorization", "token", "apikey", "cookie")) else self.sanitize(item)]
                        for key, item in value]
            return [self.sanitize(item, field) for item in value]
        if isinstance(value, str):
            if field.lower() in {"pw", "password", "accesstoken", "token", "key"} and value:
                return "[REDACTED_SECRET]"
            for secret in sorted(self.secrets, key=len, reverse=True):
                value = value.replace(secret, "[REDACTED_SECRET]")
            return re.sub(r"(?i)(api_key=)[^&\s\"]+", r"\1[REDACTED_SECRET]", value)
        return value

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
        require(route.startswith("/emby/") and not any(char in route for char in "\\\r\n#"), "Unexpected bootstrap route")
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
        status_code, server_info = self.request("bootstrap-final-server-info", "GET", "/emby/Devices/Info?Id=" + quote(self.server_id, safe=""), token=admin_token)
        require(status_code in {200, 204, 404}, "Fresh server-device observation has an unexpected status")
        server_observation = {"alias": self.server_id, "status": status_code, "body": server_info}
        status_code, keys = self.request("bootstrap-final-keys", "GET", "/emby/Auth/Keys", token=admin_token)
        require(status_code == 200 and isinstance(keys, dict) and keys.get("Items") == [], "Fresh bootstrap unexpectedly contains application keys")
        self.logout("admin")
        require(self.revoked == {"admin", "viewer"}, "Bootstrap credentials were not both invalidated")
        for raw in sorted(RAW.glob(PREFIX + "*.json")):
            require(self.sanitize(load(raw)) == load(EXPORT / raw.name), "Fresh setup export redaction audit failed")
        return {"serverId": self.server_id, "bootstrapDevices": devices["Items"], "bootstrapUsers": users,
                "bootstrapServerInfo": server_observation, "bootstrapServerInfoObservations": [server_observation],
                "viewerEmptyPasswordLoginVerified": True, "bootstrapCredentialsRevoked": True,
                "bootstrapCredentialsInvalidity": {"admin": 401, "viewer": 401}, "setupRecordCount": self.count,
                "setupCompleteHTTPResponses": self.complete_count, "setupIncompleteAttempts": self.incomplete_count,
                "wireBytes": self.wire, "bootstrapUserIds": {"admin": admin["User"]["Id"], "viewer": viewer["Id"]}}


def bootstrap() -> None:
    common_preconditions(host=False)
    owned_roots()
    intent, identity = load(INTENT), load(IDENTITY)
    require(intent.get("marker") == MARKER and intent.get("operatorSha256") == digest(Path(__file__).resolve()), "Fresh bootstrap intent differs")
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
  <HttpServerPortNumber>18098</HttpServerPortNumber><PublicPort>18098</PublicPort>
  <HttpsPortNumber>18498</HttpsPortNumber><PublicHttpsPort>18498</PublicHttpsPort>
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
            save(DISPATCH, current_dispatch)
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


def prepare() -> None:
    common_preconditions(host=True)
    require(not ROOT.exists() and not ROOT.is_symlink() and not DATA.exists() and not DATA.is_symlink(), "Fresh fixture paths already exist")
    require(properties(UNIT).get("LoadState") == "not-found", "Fresh fixture unit already exists")
    before_services, before_files = old_services(), old_baseline()
    previous_failure = previous_failure_provenance()
    initial_resources = resources(admission=True)
    os.umask(0o077)
    intent = {"schemaVersion": 1, "referenceRun": REFERENCE_RUN, "state": "PREPARING", "marker": MARKER, "unit": UNIT, "programData": str(DATA),
              "evidenceRoot": str(ROOT), "port": PORT, "startedAt": utc(), "operatorSha256": digest(Path(__file__).resolve()),
              "oldServices": before_services, "oldBaseline": before_files, "initialResources": initial_resources,
              "previousFailureProvenance": previous_failure,
              "packageSha256": PACKAGE_SHA256, "sharedServerBinarySha256": digest(APP / "system/EmbyServer"),
              "limits": {"serviceMemoryBytes": 512 * MIB, "admissionAvailableMemoryBytes": 768 * MIB,
                         "minimumAvailableMemoryBytes": 192 * MIB, "dataGrowthBytes": 192 * MIB, "evidenceGrowthBytes": 64 * MIB,
                         "minimumScratchBytes": 32 * MIB, "minimumShmBytes": 64 * MIB, "httpAttempts": MAX_REQUESTS,
                         "singleResponseBytes": MAX_BODY, "totalResponseBytes": MAX_WIRE,
                         "readinessSeconds": 90, "bootstrapHTTPSeconds": 180, "supervisorSeconds": 210}}
    identity, child, started_service = None, None, False
    samples: list[dict] = []
    try:
        create_owned_root(ROOT)
        PRIVATE.mkdir(mode=0o700)
        # The full preservation/ownership intent is durable before any data
        # root exists or service can be dispatched. Partial evidence-tree
        # creation therefore cannot orphan a running instance.
        save(INTENT, intent)
        for folder in (RAW, PRIVATE / "wire", EXPORT, RUNTIME):
            folder.mkdir(mode=0o700)
        create_owned_root(DATA)
        (DATA / "config").mkdir(mode=0o700)
        write_configuration()
        save(PRIVATE / "launcher-manifest.json", {"path": str(RUNTIME / "launch.sh"), "sha256": digest(RUNTIME / "launch.sh")})
        # Recheck immediately before dispatch; a pre-existing service may not
        # be adopted, restarted, or stopped under this operation's identity.
        require(properties(UNIT).get("LoadState") == "not-found", "Fresh unit appeared before dispatch")
        started_service = True
        start_service()
        save(DISPATCH, dispatch_identity())
        process_deadline = time.monotonic() + 15
        while True:
            try:
                identity = new_identity()
                break
            except (RuntimeError, FileNotFoundError, IndexError):
                require(time.monotonic() < process_deadline, "Fresh service failed to establish its process identity")
                time.sleep(0.25)
        save(IDENTITY, identity)
        canonical(Path(__file__).resolve())
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
        same_new_identity(identity)
        check_old_services(before_services)
        verify_baseline(before_files)
        verify_package(intent)
        verify_previous_failure(previous_failure)
        samples.append(resources())
        save(PRIVATE / "resource-observations.json", {"samples": samples})
        require(len(list(RAW.glob(PREFIX + "*.json"))) == bootstrap_result["setupRecordCount"] and
                len(list(EXPORT.glob(PREFIX + "*.json"))) == bootstrap_result["setupRecordCount"], "Bootstrap record membership differs")
        manifest = {**intent, **identity, **bootstrap_result, "state": "READY", "readyAt": utc(),
                    "setupRawRoot": str(RAW), "setupExportRoot": str(EXPORT), "setupPrefix": PREFIX,
                    "adminCredentialsFile": str(PRIVATE / "admin-credentials.env"),
                    "viewerCredentialsFile": str(PRIVATE / "viewer-credentials.env"),
                    "oldPreservationVerified": True, "allFreshProgramDataOwned": True,
                    "bootstrapDeviceSnapshotTiming": "After viewer logout, before final administrator logout"}
        save(MANIFEST, manifest)
        save(ROOT / "ready-status.json", {"state": "READY", "referenceRun": REFERENCE_RUN, "unit": UNIT, "pid": identity["pid"],
             "serverId": bootstrap_result["serverId"], "port": PORT, "setupRecordCount": bootstrap_result["setupRecordCount"],
             "oldPreservationVerified": True, "bootstrapCredentialsRevoked": True})
        print((ROOT / "ready-status.json").read_text(), flush=True)
    except Exception as error:
        if child is not None and child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait(timeout=5)
        cleanup, preservation_errors = {"stopped": False, "attempted": started_service}, []
        if started_service:
            try:
                cleanup = stop_owned(identity)
            except Exception as cleanup_error:
                cleanup = {"stopped": False, "failureType": type(cleanup_error).__name__, "message": str(cleanup_error)}
        for label, check in (("oldServices", lambda: check_old_services(before_services)), ("oldFiles", lambda: verify_baseline(before_files)),
                             ("sharedPackage", lambda: verify_package(intent)),
                             ("previousFreshAttempt", lambda: verify_previous_failure(previous_failure))):
            try:
                check()
            except Exception as preservation_error:
                preservation_errors.append({"check": label, "failureType": type(preservation_error).__name__})
        report_saved = False
        if PRIVATE.exists() and (ROOT / ".goby-managed").exists():
            try:
                owned_root(ROOT)
                save(PRIVATE / "prepare-failure.json", {"failureType": type(error).__name__, "message": str(error), "cleanup": cleanup,
                     "preservationErrors": preservation_errors, "dataRetained": DATA.exists(), "resourceSamples": samples})
                report_saved = True
            except Exception:
                # Exhausted storage may prevent a new failure report; retain
                # every existing file and report that limitation explicitly.
                pass
        print(json.dumps({"state": "FAILED", "failureType": type(error).__name__, "ownedServiceStopped": cleanup.get("stopped"),
                          "oldPreservationVerified": not preservation_errors, "privateEvidenceRetained": PRIVATE.exists(),
                          "failureReportSaved": report_saved}), flush=True)
        raise SystemExit(1)


def remove_owned_data() -> None:
    owned_roots()
    require(DATA.parent == Path("/dev/shm") and DATA.name == "goby-emby-key-devices-fresh-m5e-20260910-" + RUN_LABEL,
            "Data removal target differs")
    require(not os.path.ismount(DATA), "Owned data is unexpectedly a mount point")
    root_entry = DATA.stat()
    removal_identity = {"programData": str(DATA), "device": root_entry.st_dev, "inode": root_entry.st_ino, "marker": MARKER}
    removal_intent = PRIVATE / "data-removal-intent.json"
    if removal_intent.exists():
        require(load(removal_intent) == removal_identity, "Data removal identity changed across attempts")
    else:
        save(removal_intent, removal_identity)
    entries = list(DATA.rglob("*"))
    require(len(entries) <= 20000, "Owned data removal exceeds its finite membership bound")
    for path in entries:
        require(path.resolve(strict=True) == path and DATA in path.parents and not path.is_symlink() and not os.path.ismount(path),
                "Owned data contains an unsafe removal target")
        entry = path.lstat()
        require(entry.st_uid == 0 and (stat.S_ISDIR(entry.st_mode) or stat.S_ISREG(entry.st_mode)), "Owned data contains an unexpected owner or type")
        if stat.S_ISREG(entry.st_mode):
            require(entry.st_nlink == 1, "Owned data contains an unexpected hard link")
    for path in sorted(entries, key=lambda item: len(item.parts), reverse=True):
        if path == DATA / ".goby-managed":
            continue
        path.rmdir() if path.is_dir() else path.unlink()
    try:
        (DATA / ".goby-managed").unlink()
        DATA.rmdir()
    except Exception:
        # The durable inode attestation also covers a second interruption
        # during marker restoration; cleanup can finish that exact empty root.
        if DATA.exists() and not DATA.is_symlink() and DATA.stat().st_dev == root_entry.st_dev and \
                DATA.stat().st_ino == root_entry.st_ino and not list(DATA.iterdir()):
            save(DATA / ".goby-managed", MARKER + "\n")
        raise


def recover_empty_removal_root() -> None:
    if not DATA.exists() or (DATA / ".goby-managed").exists():
        return
    canonical(DATA, directory=True, private=True)
    entry = DATA.stat()
    proof = load(PRIVATE / "data-removal-intent.json")
    require(proof == {"programData": str(DATA), "device": entry.st_dev, "inode": entry.st_ino, "marker": MARKER} and
            not os.path.ismount(DATA) and not list(DATA.iterdir()), "Unmarked data is not the previously attested empty removal root")
    state = properties(UNIT)
    require(int(state.get("MainPID", "0")) == 0 and state.get("ActiveState") not in {"active", "activating", "deactivating"},
            "An empty removal root still has a service process")
    save(DATA / ".goby-managed", MARKER + "\n")


def cleanup() -> None:
    common_preconditions(host=True)
    owned_root(ROOT)
    if not PRIVATE.exists():
        PRIVATE.mkdir(mode=0o700)
    canonical(PRIVATE, directory=True, private=True)
    recover_empty_removal_root()
    if not INTENT.exists() or not DATA.exists():
        require(not DATA.exists() and not DATA.is_symlink() and properties(UNIT).get("LoadState") == "not-found",
                "Incomplete initialization has unexpected data or a service")
        preserved, failure_type = False, None
        if INTENT.exists():
            try:
                partial_intent = load(INTENT)
                check_old_services(partial_intent["oldServices"])
                verify_baseline(partial_intent["oldBaseline"])
                verify_package(partial_intent)
                verify_previous_failure(partial_intent.get("previousFailureProvenance"))
                preserved = True
            except Exception as error:
                # No data or unit exists to remove. A truncated intent or a
                # failed historical comparison remains explicitly unproven.
                failure_type = type(error).__name__
        attempt = next((index for index in range(1, 100) if not (PRIVATE / ("cleanup-absent-data-" + str(index) + ".json")).exists()), None)
        require(attempt is not None, "Absent-data cleanup attempt budget exhausted")
        save(PRIVATE / ("cleanup-absent-data-" + str(attempt) + ".json"), {"at": utc(), "dataRemoved": False, "dataAbsent": True,
             "serviceAbsent": True, "evidenceRetained": True, "oldPreservationVerified": preserved,
             "preservationFailureType": failure_type,
             "note": "The data root and unit are absent; no new deletion was attempted, and all evidence is retained."})
        print(json.dumps({"dataAbsent": True, "serviceAbsent": True, "evidenceRetained": True, "oldPreservationVerified": preserved}), flush=True)
        require(not INTENT.exists() or preserved, "Absent-data cleanup cannot prove the recorded preservation baseline")
        return
    owned_roots()
    intent = load(INTENT)
    require(intent.get("marker") == MARKER and intent.get("unit") == UNIT and intent.get("programData") == str(DATA) and
            intent.get("evidenceRoot") == str(ROOT) and intent.get("referenceRun", 1) == REFERENCE_RUN,
            "Cleanup intent does not identify this exact fixture")
    manifest = load(MANIFEST) if MANIFEST.exists() else intent
    expected = load(IDENTITY) if IDENTITY.exists() else None
    if MANIFEST.exists():
        require(manifest.get("state") == "READY" and expected is not None and manifest.get("pid") == expected["pid"] and
                manifest.get("startTicks") == expected["startTicks"], "Ready manifest and process attestation disagree")
    attempt = next((index for index in range(1, 100) if not (PRIVATE / ("cleanup-attempt-" + str(index) + ".json")).exists()), None)
    require(attempt is not None, "Cleanup attempt budget exhausted")
    report_path = PRIVATE / ("cleanup-attempt-" + str(attempt) + ".json")
    report: dict = {"attempt": attempt, "at": utc(), "unit": UNIT, "programData": str(DATA), "dataRemoved": False,
                    "evidenceRetained": True, "oldPreservationVerified": False}
    try:
        report["stop"] = stop_owned(expected)
        # Stop the independently proven owned service even if a protected
        # baseline has changed. Such a change still forbids data deletion.
        check_old_services(intent["oldServices"])
        verify_baseline(intent["oldBaseline"])
        verify_package(intent)
        verify_previous_failure(intent.get("previousFailureProvenance"))
        report["dataBytesBeforeRemoval"] = tree_size(DATA)
        # Snapshot the stopped data membership and bytes before removal. Raw
        # HTTP and credentials already live outside this disposable directory.
        inventory = {str(path.relative_to(DATA)): {"size": path.stat().st_size, "sha256": digest(path)}
                     for path in sorted(DATA.rglob("*")) if path.is_file() and not path.is_symlink()}
        save(PRIVATE / ("cleanup-data-inventory-" + str(attempt) + ".json"), inventory)
        remove_owned_data()
        report["dataRemoved"] = not DATA.exists()
        check_old_services(intent["oldServices"])
        verify_baseline(intent["oldBaseline"])
        verify_package(intent)
        verify_previous_failure(intent.get("previousFailureProvenance"))
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
