#!/usr/bin/env python3
"""Observe the already-started original client host without repeating preparation.

Only this new receipt directory is written. The old failed preparation, package,
configuration, and unit are retained. The only HTTP operation is at most one
public Info GET, after fixing the process and unique IPv4-eligible listener.
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import sys
import time

WORK = Path("/opt/goby-test/exec-work-m3e")
OLD = WORK / "core-av-original-client-hosting-01"
ROOT = WORK / "core-av-original-client-hosting-reconcile-01"
OLD_SOURCE = OLD / "prepare-core-av-original-client.py"
OLD_SOURCE_SHA256 = "9250d0bf71bbc224619677ebd2a8ecc53c4c1156381d7dba8f02459a1f585761"
OLD_TERMINAL_SHA256 = "8e6ea6a53bb23c8fffd14990b08a93bc097a026125635f7d07d843fdd41ab05d"
PID = 366598
INVOCATION = "c883bdf04e2f4d7ea6526fcd858fc342"
LISTENER = {"host": "127.0.0.1", "port": 28497, "socketInode": "330881"}


class ReconciliationError(Exception):
    """A fixed failed observation, never an instruction to restart the service."""


def require(condition, reason):
    if not condition:
        raise ReconciliationError(reason)


def read_pinned(path, checksum):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts and re.fullmatch(r"[0-9a-f]{64}", checksum), "descriptor_invalid")
    for entry in (path, *path.parents):
        info = entry.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and
                not info.st_mode & 0o022, "descriptor_path_not_protected")
    before = path.stat()
    require(stat.S_ISREG(before.st_mode) and before.st_nlink == 1 and before.st_size <= 2 * 1024 ** 2, "descriptor_file_invalid")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        raw = stream.read(2 * 1024 ** 2 + 1)
        after = os.fstat(stream.fileno())
    fields = lambda value: (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns)
    require(fields(before) == fields(after) == fields(path.stat()) and hashlib.sha256(raw).hexdigest() == checksum, "descriptor_changed")
    return raw


def load_source(path, checksum, name):
    raw = read_pinned(path, checksum)
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    exec(compile(raw, str(path), "exec"), module.__dict__)
    return module


def prior_proof(legacy):
    terminal = json.loads(read_pinned(OLD / "terminal.json", OLD_TERMINAL_SHA256))
    require(terminal.get("kind") == "core-av-original-client-hosting-terminal" and terminal.get("status") == "recovery_required" and
            terminal.get("startCalls") == 1 and terminal.get("publicHttpRequests") == 0 and
            terminal.get("failedCheck") == "public_readiness_not_observed", "old_failed_terminal_mismatch")
    observed = terminal["observedUnit"]
    require(observed.get("Id") == legacy.UNIT and observed.get("MainPID") == str(PID) and
            observed.get("InvocationID") == INVOCATION and observed.get("ActiveState") == "active", "old_invocation_mismatch")
    expected = {"intent": OLD / "prepare-intent.json", "startIntent": OLD / "start-intent.json", "startResult": OLD / "start-result.json",
                "launcher": OLD / "launch.sh", "unitSource": OLD / "unit.service", "installedUnit": legacy.UNIT_FILE,
                "extraction": OLD / "extraction-result.json", "packageMetadata": OLD / "package-metadata.json"}
    records, decoded = terminal["records"], {}
    for key, path in expected.items():
        descriptor = records[key]
        require(set(descriptor) == {"path", "sha256"} and descriptor["path"] == str(path), "old_record_path_mismatch")
        raw = read_pinned(path, descriptor["sha256"])
        if path.suffix == ".json":
            decoded[key] = json.loads(raw)
    intent, start = decoded["intent"], decoded["startIntent"]
    require(intent["source"] == {"path": str(OLD_SOURCE), "sha256": OLD_SOURCE_SHA256} and intent["unit"] == legacy.UNIT and
            intent["app"] == str(legacy.APP) and intent["programData"] == str(legacy.DATA) and intent["port"] == LISTENER["port"] and
            intent["package"] == {"path": str(legacy.PACKAGE), "sha256": legacy.PACKAGE_SHA256, "bytes": legacy.PACKAGE_BYTES}, "old_preparation_intent_mismatch")
    require(start["unit"] == legacy.UNIT and start["startCallsAuthorized"] == 1 and start["sourceSha256"] == OLD_SOURCE_SHA256 and
            start["launcher"] == records["launcher"] and start["installedUnit"] == records["installedUnit"] and
            decoded["startResult"]["argv"] == ["/usr/bin/systemctl", "start", legacy.UNIT], "old_start_intent_mismatch")
    for key in ("startResult", "extraction", "packageMetadata"):
        legacy.successful(decoded[key])
    require(records["installedUnit"]["sha256"] == records["unitSource"]["sha256"] and
            legacy.canonical(legacy.PACKAGE).st_size == legacy.PACKAGE_BYTES and legacy.digest(legacy.PACKAGE) == legacy.PACKAGE_SHA256 and
            legacy.digest(legacy.BINARY) == legacy.BINARY_SHA256, "old_package_or_executable_changed")
    legacy.canonical(legacy.DATA, directory=True)
    return {"terminal": {"path": str(OLD / "terminal.json"), "sha256": OLD_TERMINAL_SHA256},
            "preparationSource": intent["source"], "records": {key: records[key] for key in expected}}


def bound_runtime(legacy, gateway, pins, expected_process=None):
    state = legacy.properties()
    legacy.verify_unit(state)
    require(state.get("ActiveState") == "active" and state.get("SubState") == "running" and
            state.get("MainPID") == str(PID) and state.get("InvocationID") == INVOCATION, "current_invocation_changed")
    process = gateway.metadata(PID)
    executable = legacy.BINARY.stat()
    require(process["uid"] == 0 and process["exe"] == str(legacy.BINARY) and process["cmdline"] == legacy.service_arguments() and
            (process["exeDevice"], process["exeInode"]) == (executable.st_dev, executable.st_ino) and
            process["cgroup"].strip() == "0::/system.slice/" + legacy.UNIT and
            process["networkNamespace"] != os.readlink("/proc/1/ns/net") and
            (expected_process is None or process == expected_process), "current_process_changed")
    eligible = gateway.verify_listener(PID, LISTENER)
    require(eligible == [LISTENER["socketInode"]] and gateway.metadata(PID) == process and
            legacy.digest(legacy.UNIT_FILE) == pins["records"]["installedUnit"]["sha256"] and
            legacy.digest(legacy.LAUNCHER) == pins["records"]["launcher"]["sha256"], "current_listener_or_launch_changed")
    return {"process": process, "listener": dict(LISTENER), "executableSha256": legacy.BINARY_SHA256,
            "unit": legacy.UNIT, "invocationId": INVOCATION, "eligibleSocketInodes": eligible}, state


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode, "remote_isolated_reconciliation_required")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-sha256", required=True)
    parser.add_argument("--gateway", type=Path, required=True)
    parser.add_argument("--gateway-sha256", required=True)
    args = parser.parse_args()
    source = Path(__file__).absolute()
    require(source == ROOT / "reconcile-core-av-original-client.py" and args.gateway.name == "client-acceptance-gateway.py", "source_path_mismatch")
    read_pinned(source, args.source_sha256)
    legacy = load_source(OLD_SOURCE, OLD_SOURCE_SHA256, "frozen_original_client_preparation")
    info = legacy.canonical(ROOT, directory=True)
    require(stat.S_IMODE(info.st_mode) == 0o700 and set(ROOT.iterdir()) == {source}, "fresh_reconciliation_scope_required")
    gateway = load_source(args.gateway, args.gateway_sha256, "verified_original_client_listener")
    require(callable(gateway.verify_listener_inventory), "listener_revision_missing")
    os.umask(0o077)
    records, probes, attempted = {}, [], 0
    terminal = {"schemaVersion": 1, "kind": "core-av-original-client-hosting-reconciliation-terminal", "status": "failed_before_http",
                "startCalls": 0, "serviceMutationCalls": 0, "retryAllowed": False, "originalFailedTerminalPreserved": True,
                "vendorWebBodyRead": False, "referenceDatabaseRead": False, "clientAcceptanceClaim": False}
    try:
        pins = prior_proof(legacy)
        binding, state = bound_runtime(legacy, gateway, pins)
        records["intent"] = legacy.save(ROOT / "reconcile-intent.json", {"createdAt": legacy.now(), "priorProof": pins,
            "source": {"path": str(source), "sha256": args.source_sha256},
            "gatewaySource": {"path": str(args.gateway), "sha256": args.gateway_sha256}, "runtimeBefore": binding,
            "unitBefore": state, "publicGetLimit": 1})
        before = binding["process"]
        require(bound_runtime(legacy, gateway, pins, before)[0] == binding, "pre_http_binding_changed")
        terminal["status"] = "recovery_required"
        attempted = 1
        probe = {"method": "GET", "path": "/emby/System/Info/Public", "startedAt": legacy.now(), "completed": False}
        probes.append(probe)
        response, body = legacy.public_info(binding, time.monotonic() + 10)
        probe.update(response, completed=True, completedAt=legacy.now())
        require(response["complete"] and response["status"] == 200, "public_response_not_complete_200")
        value = json.loads(body)
        require(value.get("Version") == "4.9.5.0" and value.get("ServerName") == legacy.SERVER_NAME and
                isinstance(value.get("Id"), str) and value["Id"], "public_identity_mismatch")
        after, state = bound_runtime(legacy, gateway, pins, before)
        require(after == binding, "post_http_binding_changed")
        records["hosting"] = legacy.save(ROOT / "hosting.json", {"schemaVersion": 1, "kind": "core-av-original-client-hosting",
            "createdAt": legacy.now(), **binding, "version": value["Version"], "serverName": value["ServerName"], "serverId": value["Id"],
            "app": str(legacy.APP), "programData": str(legacy.DATA), "packageSha256": legacy.PACKAGE_SHA256,
            "sourceSha256": args.source_sha256, "preparationSourceSha256": OLD_SOURCE_SHA256, "priorProof": pins,
            "unitProperties": state, "ipv4PublicReachabilityObserved": True, "webServingValidation": "pending_gateway_client_observation"})
        terminal["status"] = "ready_for_gateway"
    except BaseException as error:
        terminal.update(failedCheck=str(error) if isinstance(error, (ReconciliationError, legacy.PreparationError, gateway.GatewayRejected))
                        else "reconciliation_observation_failed", errorType=type(error).__name__)
    finally:
        terminal.update(publicHttpRequestsAttempted=attempted, completedAt=legacy.now())
        receipt_errors, output = [], None
        try:
            records["publicInfo"] = legacy.save(ROOT / "public-info.json", probes)
        except BaseException as error:
            receipt_errors.append({"record": "publicInfo", "errorType": type(error).__name__})
            terminal["status"] = "recovery_required"
        terminal.update(records=records, receiptErrors=receipt_errors)
        try:
            output = legacy.save(ROOT / "terminal.json", terminal)
        except BaseException as error:
            receipt_errors.append({"record": "terminal", "errorType": type(error).__name__})
            terminal["status"] = "recovery_required"
        print(json.dumps({"scope": str(ROOT), "unit": legacy.UNIT, "status": terminal["status"], "terminal": output,
                          "startCalls": 0, "publicHttpRequestsAttempted": attempted, "receiptUnavailable": bool(receipt_errors),
                          "receiptErrors": receipt_errors}), flush=True)
    return 0 if terminal["status"] == "ready_for_gateway" else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ReconciliationError as error:
        print(json.dumps({"scope": str(ROOT), "status": "rejected_before_reconciliation", "startCalls": 0, "reason": str(error)}), flush=True)
        raise SystemExit(1) from None
