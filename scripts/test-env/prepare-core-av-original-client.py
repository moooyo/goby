#!/usr/bin/env python3
"""Prepare one fresh, isolated original-client host from the retained official deb.

This operation extracts a verified package without installing it or running its
maintainer scripts. It starts exactly one new unit and only reads public server
metadata. It does not bootstrap users, libraries, or a reference comparison, and
does not read vendor web assets or reference databases. Failures retain evidence
and the current unit state; this tool never retries a start or stops a service.
"""

from __future__ import annotations

import argparse
import base64
from datetime import datetime, timezone
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import signal
import socket
import stat
import subprocess
import sys
import time

WORK = Path("/opt/goby-test/exec-work-m3e")
ROOT = WORK / "core-av-original-client-hosting-01"
PACKAGE = Path("/opt/goby-test/inactive-dependencies-m5h/emby-server-deb_4.9.5.0_amd64.deb")
PACKAGE_SHA256 = "1d718ffa0169c393de3eafda65b1b057a3db4ead93ffeb5883abd01735de9843"
PACKAGE_BYTES = 190375356
APP = ROOT / "package/opt/emby-server"
DATA = ROOT / "programdata"
BINARY = APP / "system/EmbyServer"
BINARY_SHA256 = "c109c9817dea25cc516b9969a87aa1ffa48e41adcb5e3dbc87d686c7bcb28ac2"
LAUNCHER = ROOT / "launch.sh"
UNIT = "goby-core-av-original-client-01.service"
UNIT_FILE = Path("/etc/systemd/system") / UNIT
PORT = 28497
SERVER_NAME = "Goby Core AV Original Client Host 01"
ENV = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}
PROPERTIES = ("Id", "LoadState", "ActiveState", "SubState", "MainPID", "InvocationID", "ControlGroup", "FragmentPath",
              "DropInPaths", "ExecStart", "Restart", "NRestarts", "ExecMainStatus", "Result", "PrivateNetwork",
              "PrivateTmp", "PrivateDevices", "NoNewPrivileges", "ProtectSystem", "ProtectHome", "ReadWritePaths",
              "ReadOnlyPaths", "WorkingDirectory", "User", "Group", "MemoryMax", "TasksMax", "CapabilityBoundingSet")


class PreparationError(Exception):
    """A fixed preparation failure without vendor or credential text."""


def require(condition, reason):
    if not condition:
        raise PreparationError(reason)


def now():
    return datetime.now(timezone.utc).isoformat()


def canonical(path, *, directory=False):
    require(path.is_absolute() and ".." not in path.parts, "path_not_absolute")
    for entry in (path, *path.parents):
        info = entry.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                not info.st_mode & 0o022, "path_not_protected")
        if entry != path or directory:
            require(stat.S_ISDIR(info.st_mode), "path_not_directory")
    info = path.stat()
    require(directory or stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "path_not_regular")
    return info


def digest(path):
    before = canonical(path)
    result = hashlib.sha256()
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        for chunk in iter(lambda: stream.read(1048576), b""):
            result.update(chunk)
        after = os.fstat(stream.fileno())
    fields = lambda value: (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns, value.st_mode)
    require(fields(before) == fields(after) == fields(path.stat()), "file_changed_during_hash")
    return result.hexdigest()


def save(path, value, *, mode=0o600):
    canonical(path.parent, directory=True)
    raw = value.encode() if isinstance(value, str) else (json.dumps(value, sort_keys=True, separators=(",", ":"), allow_nan=False) + "\n").encode()
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode), "wb") as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    return {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}


def group_alive(pid):
    try:
        os.killpg(pid, 0)
        return True
    except ProcessLookupError:
        return False


def stop_command_group(process):
    """Reap only the command's new session, never the systemd service it starts."""
    output = (b"", b"")
    for sig in (signal.SIGTERM, signal.SIGKILL):
        try:
            os.killpg(process.pid, sig)
        except ProcessLookupError:
            pass
        try:
            output = process.communicate(timeout=5)
        except subprocess.TimeoutExpired as error:
            output = (error.output or b"", error.stderr or b"")
        deadline = time.monotonic() + 1
        while group_alive(process.pid) and time.monotonic() < deadline:
            time.sleep(0.05)
        if not group_alive(process.pid):
            process.wait(timeout=1)
            return output, True
    return output, False


def command(arguments, *, timeout=20, pass_fds=()):
    record = {"argv": [str(value) for value in arguments], "startedAt": now(), "returncode": None,
              "timedOut": False, "descendantsRemained": False, "groupClosed": False}
    stdout, stderr = b"", b""
    try:
        process = subprocess.Popen(record["argv"], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   env=ENV, pass_fds=pass_fds, start_new_session=True)
    except OSError:
        record.update(launchFailed=True, groupClosed=True)
    else:
        record.update(pid=process.pid, processGroup=process.pid)
        try:
            stdout, stderr = process.communicate(timeout=timeout)
            record["groupClosed"] = not group_alive(process.pid)
            if not record["groupClosed"]:
                record["descendantsRemained"] = True
                (stdout, stderr), record["groupClosed"] = stop_command_group(process)
        except subprocess.TimeoutExpired:
            record["timedOut"] = True
            (stdout, stderr), record["groupClosed"] = stop_command_group(process)
        except BaseException:
            record["interrupted"] = True
            (stdout, stderr), record["groupClosed"] = stop_command_group(process)
        record["returncode"] = process.returncode
    record.update(stdout=stdout[:65536].decode(errors="replace"), stderr=stderr[:65536].decode(errors="replace"),
                  outputTruncated=len(stdout) > 65536 or len(stderr) > 65536)
    record["completedAt"] = now()
    return record


def successful(record):
    require(record["returncode"] == 0 and record["groupClosed"] and
            not any(record.get(key) for key in ("outputTruncated", "timedOut", "descendantsRemained", "interrupted")), "bounded_command_failed")


def properties(*, timeout=15):
    record = command(["/usr/bin/systemctl", "show", UNIT, *[part for key in PROPERTIES for part in ("-p", key)]], timeout=timeout)
    values = dict(line.split("=", 1) for line in record.get("stdout", "").splitlines() if "=" in line)
    require(record["groupClosed"] and not any(record.get(key) for key in ("outputTruncated", "timedOut", "descendantsRemained", "interrupted")) and
            (record["returncode"] == 0 or record["returncode"] == 1 and values.get("LoadState") == "not-found"), "unit_state_unavailable")
    return values


def metadata(pid):
    process = Path("/proc") / str(pid)
    raw = (process / "stat").read_text()
    fields = raw[raw.rfind(")") + 2:].split()
    require(len(fields) > 19 and fields[0] != "Z", "process_unavailable")
    executable = (process / "exe").stat()
    return {"pid": pid, "startTicks": fields[19], "bootId": Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
            "uid": process.stat().st_uid, "exe": os.readlink(process / "exe"), "exeDevice": executable.st_dev,
            "exeInode": executable.st_ino, "cmdline": [value.decode() for value in (process / "cmdline").read_bytes().split(b"\0") if value],
            "networkNamespace": os.readlink(process / "ns/net"), "cgroup": (process / "cgroup").read_text()}


def service_arguments():
    return [str(BINARY), "-programdata", str(DATA), "-ffdetect", str(APP / "bin/ffdetect"), "-ffmpeg", str(APP / "bin/ffmpeg"),
            "-ffprobe", str(APP / "bin/ffprobe"), "-restartexitcode", "3", "-updatepackage", "emby-server-deb_{version}_amd64.deb"]


def verify_unit(state):
    expected = {"Id": UNIT, "FragmentPath": str(UNIT_FILE), "DropInPaths": "", "Restart": "no", "NRestarts": "0",
                "PrivateNetwork": "yes", "PrivateTmp": "yes", "PrivateDevices": "yes", "NoNewPrivileges": "yes",
                "ProtectSystem": "strict", "ProtectHome": "yes", "ReadWritePaths": str(DATA), "ReadOnlyPaths": str(ROOT),
                "WorkingDirectory": str(DATA), "User": "root", "Group": "root", "MemoryMax": str(1073741824),
                "TasksMax": "256", "CapabilityBoundingSet": ""}
    require(all(state.get(key) == value for key, value in expected.items()) and
            re.findall(r"(?:^|[ {;])path=([^;]+?)\s*;", state.get("ExecStart", "")) == [str(LAUNCHER)], "unit_contract_changed")


def runtime(*, timeout=15):
    state = properties(timeout=timeout)
    verify_unit(state)
    pid = int(state.get("MainPID", "0"))
    if state.get("ActiveState") != "active" or pid <= 1:
        return None, state
    process = metadata(pid)
    # The launcher can briefly own MainPID before exec; wait for the pinned ELF.
    if process["exe"] != str(BINARY):
        require(process["exe"] in ("/usr/bin/bash", "/bin/bash", "/usr/bin/env") and str(LAUNCHER) in process["cmdline"], "unexpected_launch_process")
        return None, state
    binary = BINARY.stat()
    require(process["uid"] == 0 and process["cmdline"] == service_arguments() and
            (process["exeDevice"], process["exeInode"]) == (binary.st_dev, binary.st_ino) and
            process["cgroup"].strip() == "0::/system.slice/" + UNIT and
            process["networkNamespace"] != os.readlink("/proc/1/ns/net") and re.fullmatch(r"[0-9a-f]{32}", state.get("InvocationID", "")), "runtime_binding_changed")
    inodes = {line.split()[9] for line in (Path("/proc") / str(pid) / "net/tcp").read_text().splitlines()[1:]
              if line.split()[3] == "0A" and line.split()[1] in ("0100007F:" + format(PORT, "04X"), "00000000:" + format(PORT, "04X"))}
    owned = set()
    for path in (Path("/proc") / str(pid) / "fd").iterdir():
        try:
            match = re.fullmatch(r"socket:\[([0-9]+)\]", os.readlink(path))
            if match and match[1] in inodes:
                owned.add(match[1])
        except FileNotFoundError:
            continue
    if not owned:
        return None, state
    require(len(owned) == 1 and metadata(pid) == process, "listener_binding_changed")
    return {"process": process, "listener": {"host": "127.0.0.1", "port": PORT, "socketInode": owned.pop()},
            "executableSha256": BINARY_SHA256, "unit": UNIT, "invocationId": state["InvocationID"]}, state


def public_info(binding, deadline):
    def remaining():
        value = deadline - time.monotonic()
        require(value > 0, "public_readiness_deadline_exhausted")
        return min(2, value)

    process = binding["process"]
    require(metadata(process["pid"]) == process, "public_request_process_changed")
    current = os.open("/proc/self/ns/net", os.O_RDONLY | os.O_CLOEXEC)
    target = os.open("/proc/%d/ns/net" % process["pid"], os.O_RDONLY | os.O_CLOEXEC)
    connection = http.client.HTTPConnection("127.0.0.1", PORT, timeout=remaining())
    try:
        require("net:[%d]" % os.fstat(target).st_ino == process["networkNamespace"], "public_request_namespace_changed")
        try:
            os.setns(target, 0)
            connection.sock = socket.create_connection(("127.0.0.1", PORT), timeout=remaining())
            transport = connection.sock
        finally:
            os.setns(current, 0)
        require(metadata(process["pid"]) == process, "public_request_process_changed")
        connection.sock.settimeout(remaining())
        connection.request("GET", "/emby/System/Info/Public", headers={"Accept": "application/json", "Connection": "close"})
        connection.sock.settimeout(remaining())
        response = connection.getresponse()
        transport.settimeout(remaining())
        body = response.read(65537)
        result = {"status": response.status, "headers": response.getheaders(), "bodyBase64": base64.b64encode(body).decode(),
                  "bodySha256": hashlib.sha256(body).hexdigest(), "complete": len(body) <= 65536 and response.length in (None, 0)}
        return result, body
    finally:
        connection.close()
        os.close(target)
        os.close(current)


def configuration():
    for path in (DATA, DATA / "config", DATA / "cache", DATA / "tmp"):
        path.mkdir(mode=0o700)
    xml = f'''<?xml version="1.0" encoding="utf-8"?>
<ServerConfiguration xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
  <HttpServerPortNumber>{PORT}</HttpServerPortNumber><PublicPort>{PORT}</PublicPort>
  <HttpsPortNumber>28897</HttpsPortNumber><PublicHttpsPort>28897</PublicHttpsPort>
  <EnableHttps>false</EnableHttps><EnableUPnP>false</EnableUPnP><EnableRemoteAccess>false</EnableRemoteAccess>
  <EnableAutoUpdate>false</EnableAutoUpdate><EnableAutomaticRestart>false</EnableAutomaticRestart>
  <AutoRunWebApp>false</AutoRunWebApp><IsStartupWizardCompleted>false</IsStartupWizardCompleted>
  <EnableExternalContentInSuggestions>false</EnableExternalContentInSuggestions>
  <ServerName>{SERVER_NAME}</ServerName><LocalNetworkAddresses><string>127.0.0.1</string></LocalNetworkAddresses>
  <PreferredMetadataLanguage>en</PreferredMetadataLanguage><MetadataCountryCode>US</MetadataCountryCode><UICulture>en-US</UICulture>
  <DatabaseCacheSizeMB>64</DatabaseCacheSizeMB><LogFileRetentionDays>2</LogFileRetentionDays>
</ServerConfiguration>
'''
    launcher = f'''#!/usr/bin/env bash
set -euo pipefail
APP_DIR={APP}
EMBY_DATA={DATA}
export EMBY_DATA
export AMDGPU_IDS="$APP_DIR/extra/share/libdrm/amdgpu.ids"
export FONTCONFIG_PATH="$APP_DIR/etc/fonts"
export LD_LIBRARY_PATH="$APP_DIR/lib:$APP_DIR/extra/lib"
export LIBVA_DRIVERS_PATH="$APP_DIR/extra/lib/dri"
export OCL_ICD_VENDORS="$APP_DIR/extra/etc/OpenCL/vendors"
export PATH="$APP_DIR/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export PCI_IDS_PATH="$APP_DIR/share/hwdata/pci.ids"
export SSL_CERT_FILE="$APP_DIR/etc/ssl/certs/ca-certificates.crt"
export XDG_CACHE_HOME="$EMBY_DATA/cache"
export TMPDIR="$EMBY_DATA/tmp"
export NEOReadDebugKeys=1
export OverrideGpuAddressSpace=48
cd "$APP_DIR"
exec "$APP_DIR/system/EmbyServer" -programdata "$EMBY_DATA" -ffdetect "$APP_DIR/bin/ffdetect" -ffmpeg "$APP_DIR/bin/ffmpeg" -ffprobe "$APP_DIR/bin/ffprobe" -restartexitcode 3 -updatepackage 'emby-server-deb_{{version}}_amd64.deb'
'''
    unit = f'''[Unit]
Description=Goby fresh original client hosting 01
[Service]
Type=exec
User=root
Group=root
ExecStart={LAUNCHER}
WorkingDirectory={DATA}
Restart=no
PrivateNetwork=yes
PrivateTmp=yes
PrivateDevices=yes
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths={DATA}
ReadOnlyPaths={ROOT}
InaccessiblePaths=-{WORK / "reference-data"} -/opt/goby-test/emby-reference-data -/dev/shm/goby-emby-reference
CapabilityBoundingSet=
CPUQuota=150%
MemoryMax=1G
TasksMax=256
LimitNOFILE=65536
TimeoutStartSec=25
TimeoutStopSec=25
KillMode=control-group
UMask=0077
StandardOutput=append:{DATA / "service.log"}
StandardError=append:{DATA / "service.log"}
'''
    return {"initialConfiguration": save(DATA / "config/system.xml", xml), "launcher": save(LAUNCHER, launcher, mode=0o700),
            "unitSource": save(ROOT / "unit.service", unit), "installedUnit": save(UNIT_FILE, unit, mode=0o644)}


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode, "remote_isolated_operator_required")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-sha256", required=True)
    args = parser.parse_args()
    source = Path(__file__).absolute()
    root_info = canonical(ROOT, directory=True)
    require(stat.S_IMODE(root_info.st_mode) == 0o700 and source == ROOT / "prepare-core-av-original-client.py" and
            re.fullmatch(r"[0-9a-f]{64}", args.source_sha256) and digest(source) == args.source_sha256 and
            set(ROOT.iterdir()) == {source}, "fresh_source_scope_required")
    require(not os.path.lexists(UNIT_FILE), "new_unit_file_occupied")
    before = properties()
    require(before.get("LoadState") == "not-found" and before.get("MainPID", "0") == "0", "new_unit_occupied")
    package_info = canonical(PACKAGE)
    require(package_info.st_size == PACKAGE_BYTES and digest(PACKAGE) == PACKAGE_SHA256, "cached_package_changed")
    capacity = os.statvfs(ROOT)
    require(capacity.f_bavail * capacity.f_frsize >= 1536 * 1024 ** 2, "insufficient_extract_space")
    os.umask(0o077)
    records, probes, start_calls, phase = {}, [], 0, "preparation"
    records["intent"] = save(ROOT / "prepare-intent.json", {"schemaVersion": 1, "kind": "core-av-original-client-preparation",
        "createdAt": now(), "source": {"path": str(source), "sha256": args.source_sha256}, "unit": UNIT, "unitBefore": before,
        "package": {"path": str(PACKAGE), "sha256": PACKAGE_SHA256, "bytes": PACKAGE_BYTES}, "app": str(APP), "programData": str(DATA), "port": PORT})
    terminal = {"schemaVersion": 1, "kind": "core-av-original-client-hosting-terminal", "status": "failed_before_start",
                "clientAcceptanceClaim": False, "vendorWebBodyRead": False, "referenceDatabaseRead": False,
                "usersOrLibrariesBootstrapped": False, "retryAllowed": False, "writablePaths": [str(DATA)],
                "privateTemporaryDirectories": True}
    try:
        package_fd = os.open(PACKAGE, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            require(os.fstat(package_fd).st_ino == package_info.st_ino, "cached_package_replaced")
            archive = "/proc/self/fd/%d" % package_fd
            package_metadata = command(["/usr/bin/dpkg-deb", "--field", archive, "Package", "Version", "Architecture"], pass_fds=(package_fd,))
            records["packageMetadata"] = save(ROOT / "package-metadata.json", package_metadata)
            successful(package_metadata)
            require(dict(line.split(": ", 1) for line in package_metadata["stdout"].splitlines()) ==
                    {"Package": "emby-server", "Version": "4.9.5.0", "Architecture": "amd64"}, "package_metadata_mismatch")
            (ROOT / "package").mkdir(mode=0o700)
            extraction = command(["/usr/bin/dpkg-deb", "--extract", archive, str(ROOT / "package")], timeout=180, pass_fds=(package_fd,))
            records["extraction"] = save(ROOT / "extraction-result.json", extraction)
            successful(extraction)
        finally:
            os.close(package_fd)
        require(digest(PACKAGE) == PACKAGE_SHA256 and digest(BINARY) == BINARY_SHA256, "extracted_executable_mismatch")
        records.update(configuration())
        reload_result = command(["/usr/bin/systemctl", "daemon-reload"])
        records["daemonReload"] = save(ROOT / "daemon-reload-result.json", reload_result)
        successful(reload_result)
        state = properties()
        verify_unit(state)
        require(state.get("MainPID") == "0" and state.get("ActiveState") == "inactive", "new_unit_started_before_intent")
        phase = "start"
        records["startIntent"] = save(ROOT / "start-intent.json", {"unit": UNIT, "createdAt": now(), "startCallsAuthorized": 1,
            "sourceSha256": args.source_sha256, "launcher": records["launcher"], "installedUnit": records["installedUnit"], "unitBefore": state})
        start_calls = 1
        terminal["status"] = "recovery_required"
        started = command(["/usr/bin/systemctl", "start", UNIT], timeout=30)
        records["startResult"] = save(ROOT / "start-result.json", started)
        successful(started)
        phase = "public-readiness"
        deadline = time.monotonic() + 60
        previous = binding = public = None
        while time.monotonic() < deadline and len(probes) < 30:
            binding, state = runtime(timeout=min(5, max(0.05, deadline - time.monotonic())))
            if time.monotonic() >= deadline:
                break
            if binding is not None and binding == previous:
                probe = {"ordinal": len(probes) + 1, "method": "GET", "path": "/emby/System/Info/Public", "startedAt": now()}
                probes.append(probe)
                try:
                    response, body = public_info(binding, deadline)
                    probe.update(response)
                    require(response["complete"], "public_response_incomplete")
                    if response["status"] == 200:
                        value = json.loads(body)
                        require(value.get("Version") == "4.9.5.0" and value.get("ServerName") == SERVER_NAME and
                                isinstance(value.get("Id"), str) and value["Id"], "public_identity_mismatch")
                        public = {"version": value["Version"], "serverName": value["ServerName"], "serverId": value["Id"]}
                        break
                except (OSError, http.client.HTTPException):
                    probe.update(complete=False, failure="public_transport_unavailable")
                finally:
                    probe["completedAt"] = now()
            previous = binding
            time.sleep(min(0.5, max(0, deadline - time.monotonic())))
        require(public is not None and binding is not None, "public_readiness_not_observed")
        current, state = runtime()
        require(current == binding and digest(BINARY) == BINARY_SHA256 and digest(UNIT_FILE) == records["installedUnit"]["sha256"] and
                digest(LAUNCHER) == records["launcher"]["sha256"], "ready_binding_changed")
        records["hosting"] = save(ROOT / "hosting.json", {"schemaVersion": 1, "kind": "core-av-original-client-hosting", "createdAt": now(),
            **binding, **public, "app": str(APP), "programData": str(DATA), "packageSha256": PACKAGE_SHA256,
            "sourceSha256": args.source_sha256, "unitProperties": state, "webServingValidation": "pending_gateway_client_observation"})
        terminal["status"] = "ready_for_gateway"
    except BaseException as error:
        terminal.update(failedPhase=phase, failedCheck=str(error) if isinstance(error, PreparationError) else "preparation_runtime_failed",
                        errorType=type(error).__name__)
    finally:
        terminal.update(startCalls=start_calls, publicHttpRequests=len(probes), completedAt=now())
        try:
            terminal["observedUnit"] = properties()
        except BaseException:
            terminal["observedUnitUnavailable"] = True
        receipt_errors = []
        output = None
        try:
            records["publicProbes"] = save(ROOT / "public-probes.json", probes)
        except BaseException as error:
            receipt_errors.append({"record": "publicProbes", "errorType": type(error).__name__})
            terminal["status"] = "recovery_required" if start_calls else "failed_before_start"
        terminal["records"] = records
        terminal["receiptErrors"] = receipt_errors
        try:
            output = save(ROOT / "terminal.json", terminal)
        except BaseException as error:
            receipt_errors.append({"record": "terminal", "errorType": type(error).__name__})
            terminal["status"] = "recovery_required" if start_calls else "failed_before_start"
        print(json.dumps({"scope": str(ROOT), "unit": UNIT, "status": terminal["status"], "terminal": output,
                          "startCalls": start_calls, "receiptUnavailable": bool(receipt_errors), "receiptErrors": receipt_errors}), flush=True)
    return 0 if terminal["status"] == "ready_for_gateway" else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except PreparationError as error:
        print(json.dumps({"status": "rejected_before_preparation", "reason": str(error)}), flush=True)
        raise SystemExit(1) from None
