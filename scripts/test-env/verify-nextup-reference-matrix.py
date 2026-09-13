#!/usr/bin/env python3
"""Independently reconstruct a fresh matrix from retained protocol wire receipts.

This remote-only, single-use verifier makes no HTTP request, invokes no process,
and never constructs Admission or TransportRunner. It replays the frozen pure
Matrix with a logical clock, verifies each durable request/state/export byte,
and writes one exclusive safe summary. The original monotonic clock, actual
unit closure, live identity chain, preparation admission, and preservation
remain separate independent checks; they are never inferred from this replay.
"""

from __future__ import annotations

import argparse
import base64
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import stat
import sys
from urllib.parse import unquote, urlencode


W = Path("/opt/goby-test/exec-work-m3e")
ATTESTATION = W / "reference-nextup-global-matrix-attestation-07/attestation.json"
EXECUTION = ATTESTATION.parent / "published-execution.json"
OPERATOR_ROOT = W / "nextup-global-reference-operator-runs-07/operator-07"
MATRIX_ROOT = W / "nextup-global-reference-runs-05/matrix-05"
OUTPUT = W / "reference-nextup-global-matrix-execution-07/independent-terminal.json"
TOOL_ROOT = W / "nextup-global-preparation-tool-09/revision-02"
OPERATOR_SOURCE = W / "nextup-global-reference-operator-tool-05/revision-01/run-nextup-global-reference.py"
SOURCE_PINS = {
    "operator": (OPERATOR_SOURCE, "ece90b7ff8a0d56a1b08825bd0dd7ec5b4603da432af6410a4af8ebe4dd968ce"),
    "matrix": (TOOL_ROOT / "nextup-global-matrix.py", "a69ff17c26934abbf09375a7832d7e11cd898d5e696c4ba5ce8a718efd19e65d"),
    "transport": (TOOL_ROOT / "nextup-global-transport.py", "4134c66a58a1542fc3c7dc9007bcd9ae289d094bb7db28a95d59a3557be4ceb8"),
}
MAX_FILE_BYTES = 64 * 1024 * 1024
MAX_REQUESTS = 300
WIRE_KEYS = {"ordinal", "intentReceiptSha256", "request", "requestHeaders", "requestBodyBase64",
             "responseStatus", "responseHeaders", "responseBodyBase64", "completeHTTP",
             "adapterTruncatedCapture", "reportedResponseBytes", "headerCaptureTruncated",
             "completedAt", "transportFailure"}


class ReconstructionError(ValueError):
    """A retained record did not meet a named independent reconstruction check."""


def require(condition, check):
    if not condition:
        raise ReconstructionError(check)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False)


def encoded(value):
    return (canonical(value) + "\n").encode("utf-8")


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def same(left, right):
    return canonical(left) == canonical(right)


def strict_json(raw):
    def pairs(rows):
        value = {}
        for key, item in rows:
            require(key not in value, "json-duplicate-key")
            value[key] = item
        return value

    def constant(unused):
        raise ReconstructionError("json-nonfinite-number")

    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_constant=constant)


def instant(value):
    require(isinstance(value, str), "response-time-type")
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(parsed.utcoffset() is not None, "response-time-zone")
    return parsed


def protected(path, *, directory=False):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts and W in path.parents, "input-path-scope")
    for entry in (path, *path.parents):
        info = entry.lstat()
        require(not stat.S_ISLNK(info.st_mode), "input-symlink")
        if entry == path:
            require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "input-file-type")
            require(info.st_uid == 0 and not info.st_mode & 0o022, "input-owner-mode")
            require(directory or info.st_nlink == 1, "input-hardlink")
        else:
            require(stat.S_ISDIR(info.st_mode) and not info.st_mode & 0o022, "input-parent-mode")
    return path.lstat()


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink, info.st_uid, info.st_gid,
            info.st_size, info.st_mtime_ns, info.st_ctime_ns)


class Evidence:
    """Read bounded immutable files and retain their exact digests for the report."""

    def __init__(self):
        self.files = {}
        self.forbidden = []

    def read(self, path, *, checksum=None, private=True):
        path = Path(path)
        require(all(path != root and root not in path.parents for root in self.forbidden), "original-path-excluded")
        before = protected(path)
        require(before.st_size <= MAX_FILE_BYTES and (not private or not before.st_mode & 0o077), "input-size-private-mode")
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(descriptor, "rb") as handle:
            require(identity(os.fstat(handle.fileno())) == identity(before), "input-open-race")
            raw = handle.read(MAX_FILE_BYTES + 1)
            require(identity(os.fstat(handle.fileno())) == identity(before), "input-read-race")
        require(identity(path.lstat()) == identity(before) and len(raw) <= MAX_FILE_BYTES, "input-after-read-race")
        checksum_actual = sha(raw)
        require(checksum is None or checksum_actual == checksum, "input-sha256")
        previous = self.files.get(str(path))
        require(previous is None or previous == {"sha256": checksum_actual, "sizeBytes": len(raw)}, "input-changed-between-reads")
        self.files[str(path)] = {"sha256": checksum_actual, "sizeBytes": len(raw)}
        return raw

    def document(self, path, *, checksum=None, private=True, expected=None):
        raw = self.read(path, checksum=checksum, private=private)
        value = strict_json(raw)
        require(raw == encoded(value), "receipt-canonical-bytes")
        if expected is not None:
            require(raw == encoded(expected), "reconstructed-receipt-bytes")
        return value

    def descriptor(self, value):
        require(isinstance(value, dict) and set(value) == {"path", "sha256"} and
                isinstance(value["sha256"], str) and re.fullmatch(r"[0-9a-f]{64}", value["sha256"]), "descriptor-shape")
        return self.document(value["path"], checksum=value["sha256"])

    def recheck(self):
        for path, fact in list(self.files.items()):
            self.read(path, checksum=fact["sha256"], private=False)


def names(path):
    protected(path, directory=True)
    values = set()
    for child in Path(path).iterdir():
        protected(child)
        values.add(child.name)
    return values


def load_source(evidence, role):
    path, checksum = SOURCE_PINS[role]
    raw = evidence.read(path, checksum=checksum, private=False)
    name = "independent_matrix07_" + role
    module = importlib.util.module_from_spec(importlib.util.spec_from_file_location(name, path))
    sys.modules[name] = module
    exec(compile(raw, str(path), "exec"), module.__dict__)
    return module


def collect_secrets(value, secrets, transport):
    if isinstance(value, dict):
        session = value.get("SessionInfo")
        if isinstance(session, dict) and isinstance(session.get("Id"), str):
            secrets.add(session["Id"])
        for source in value.get("MediaSources", []) if isinstance(value.get("MediaSources"), list) else []:
            if isinstance(source, dict) and isinstance(source.get("Id"), str):
                secrets.add(source["Id"])
        for key, item in value.items():
            if key.lower() in transport.SECRET_KEYS and isinstance(item, str):
                secrets.add(item)
            collect_secrets(item, secrets, transport)
    elif isinstance(value, list):
        for item in value:
            collect_secrets(item, secrets, transport)
    elif isinstance(value, str):
        for match in transport.URL_SECRET.finditer(value):
            secrets.update((match.group(2), unquote(match.group(2))))


def decode_wire(wire, execution, transport):
    require(isinstance(wire, dict) and set(wire) == WIRE_KEYS, "wire-exact-fields")
    require(wire["completeHTTP"] is True and wire["adapterTruncatedCapture"] is False and
            wire["headerCaptureTruncated"] is False and wire["transportFailure"] is None, "complete-http-required")
    raw = base64.b64decode(wire["responseBodyBase64"], validate=True)
    payload = base64.b64decode(wire["requestBodyBase64"], validate=True)
    require(base64.b64encode(raw).decode() == wire["responseBodyBase64"] and
            base64.b64encode(payload).decode() == wire["requestBodyBase64"], "wire-canonical-base64")
    require(type(wire["reportedResponseBytes"]) is int and wire["reportedResponseBytes"] == len(raw) and
            len(raw) <= execution["budgets"]["responseBytes"] and len(payload) <= execution["budgets"]["requestBytes"], "wire-body-bounds")
    status = wire["responseStatus"]
    require(type(status) is int and 100 <= status <= 599, "wire-status")
    headers = wire["responseHeaders"]
    require(isinstance(headers, list) and all(isinstance(pair, list) and len(pair) == 2 and
            all(isinstance(part, str) for part in pair) for pair in headers), "wire-ordered-headers")
    require(same(transport.bounded_header_prefix(headers), headers), "wire-header-bounds")
    lengths = [value for key, value in headers if key.lower() == "content-length"]
    if lengths:
        require(len(set(lengths)) == 1 and lengths[0].isdigit() and int(lengths[0]) == len(raw) and
                not any(key.lower() == "transfer-encoding" for key, value in headers), "wire-content-length-framing")
    media_types = [value.split(";", 1)[0].strip().lower() for key, value in headers if key.lower() == "content-type"]
    require(len(set(media_types)) <= 1, "wire-media-type")
    status_only = wire["request"]["route"] in ("/emby/Sessions", "/emby/Sessions/Logout") and status == 401
    declared_json = any(value == "application/json" or value.endswith("+json") for value in media_types)
    try:
        body = strict_json(raw) if raw else None
    except json.JSONDecodeError:
        require(status_only and not declared_json, "wire-json-required")
        body = raw.decode("utf-8", errors="strict")
    return raw, payload, body


def reconstruct(evidence, attestation_sha):
    attestation = evidence.document(ATTESTATION, checksum=attestation_sha)
    require(attestation.get("kind") == "nextup-global-reference-matrix-attestation" and
            Path(attestation["execution"]["path"]) == EXECUTION and
            Path(attestation["scope"]["operatorEvidenceRoot"]) == OPERATOR_ROOT, "matrix07-attestation-scope")
    evidence.forbidden = [Path(value) for value in attestation["forbiddenOriginalRoots"]]
    for role, (path, checksum) in SOURCE_PINS.items():
        require(attestation["sources"][role] == {"path": str(path), "sha256": checksum}, "fixed-source-descriptor")
        evidence.read(path, checksum=checksum, private=False)
    require(Path(attestation["sources"]["preparation"]["path"]) == TOOL_ROOT / "prepare-nextup-global-reference.py", "preparation-source-scope")
    evidence.read(attestation["sources"]["preparation"]["path"], checksum=attestation["sources"]["preparation"]["sha256"], private=False)
    execution = evidence.descriptor(attestation["execution"])
    require(Path(execution["matrix"]["binding"]["evidenceRoot"]) == MATRIX_ROOT and
            execution["matrix"]["target"] == "reference", "matrix-output-binding")
    for role in ("matrix", "transport"):
        require(execution["sources"][role] == attestation["sources"][role], "execution-source-binding")
    credential_path = Path(execution["credentials"]["path"])
    require(Path(execution["scope"]["credentialRoot"]) in credential_path.parents, "credential-scope")
    credentials = evidence.descriptor(execution["credentials"])
    require(credentials["runId"] == execution["matrix"]["runId"] and set(credentials["actors"]) == {"P", "Q"}, "credential-run-actors")
    for actor in ("P", "Q"):
        require(all(credentials["actors"][actor][key] == execution["matrix"]["actors"][actor][key]
                    for key in ("credentialRef", "userId", "username")), "credential-actor-binding")
    matrix_module = load_source(evidence, "matrix")
    transport = load_source(evidence, "transport")
    matrix = matrix_module.Matrix(execution["matrix"])
    private, export = MATRIX_ROOT / "private", MATRIX_ROOT / "export"
    require({child.name for child in MATRIX_ROOT.iterdir()} == {"private", "export"}, "matrix-root-membership")
    evidence.document(private / "execution.json", expected=execution)
    evidence.document(private / "plan.json", expected=matrix.frozen_plan)
    private_names, export_names = names(private), names(export)
    wire_names = sorted(name for name in private_names if name.endswith("-wire.json"))
    require(0 < len(wire_names) <= MAX_REQUESTS, "bounded-wire-ledger")
    expected_private = {"execution.json", "plan.json", "state.json"}
    expected_export = {"result.json"}
    tokens, attempted, failure_events = {}, set(), []
    failure = None
    charged = 0
    elapsed = 0.0
    previous_time = None
    response_clock_regressions = []
    last_stop_time = None
    interval_checks = []
    secrets = {row["password"] for row in credentials["actors"].values()}
    secrets.update(row["deviceId"] for row in execution["matrix"]["actors"].values())
    secrets.update(str(value) for value in execution["scope"].values())
    secrets.add(str(MATRIX_ROOT))
    ledger_index = []
    for ordinal, wire_name in enumerate(wire_names, 1):
        if matrix.mode == "recovery-required" and matrix.pending is None:
            matrix.begin_cleanup()
        request = matrix.prepare_next(elapsed)
        if request is None and matrix.mode in ("api-observed", "client-discovery-required"):
            matrix.begin_cleanup()
            request = matrix.prepare_next(elapsed)
        if isinstance(request, matrix_module.WaitRequired):
            require(0 < request.seconds <= 5, "logical-lifecycle-wait")
            elapsed += request.seconds
            request = matrix.prepare_next(elapsed)
        require(isinstance(request, matrix_module.Request), "wire-after-planner-closure")
        prefix = str(ordinal).zfill(4) + "-" + request.label
        require(wire_name == prefix + "-wire.json" and request.label not in attempted, "ledger-ordinal-label")
        expected_private.update(prefix + ending for ending in ("-intent.json", "-reserved.json", "-wire.json"))
        expected_export.add(prefix + "-response.json")
        wire_path = private / wire_name
        wire = evidence.document(wire_path)
        wire_sha = evidence.files[str(wire_path)]["sha256"]
        require(wire["ordinal"] == ordinal and type(wire["ordinal"]) is int and same(wire["request"], request.fact()), "wire-request-facts")
        raw, actual_payload, body = decode_wire(wire, execution, transport)
        completed = instant(wire["completedAt"])
        if previous_time is not None and completed < previous_time:
            response_clock_regressions.append(request.label)
        previous_time = completed
        actor = execution["matrix"]["actors"][request.actor]
        headers = {"Accept": "application/json", "Authorization": 'Emby Client="Goby Global NextUp Recorder", Device="Linux Protocol Research", DeviceId="' + actor["deviceId"] + '", Version="1.0"'}
        token_sha = None
        if request.login:
            credential = credentials["actors"][request.actor]
            payload = urlencode({"Username": credential["username"], "Pw": credential["password"]}).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded"
        else:
            require(request.actor in tokens, "request-owned-login")
            token_sha = sha(tokens[request.actor].encode())
            headers["X-Emby-Token"] = tokens[request.actor]
            payload = request.body_json.encode() if request.body_json is not None else None
            if payload is not None:
                headers["Content-Type"] = "application/json"
        transport.request_metadata_size(request, headers)
        require(same(headers, wire["requestHeaders"]) and actual_payload == (payload or b""), "actual-request-headers-payload")
        intent = {"ordinal": ordinal, "request": request.fact(), "planSha256": matrix.plan_sha256,
                  "executionSha256": sha(canonical(execution).encode()), "actorTokenSha256": token_sha,
                  "matrixState": matrix.private_state(), "sourceSha256": execution["sources"]["transport"]["sha256"]}
        intent_sha = sha(encoded(intent))
        evidence.document(private / (prefix + "-intent.json"), expected=intent)
        require(wire["intentReceiptSha256"] == intent_sha, "wire-intent-hash")
        matrix.authorize(request, intent_sha, elapsed, actor_token_sha256=token_sha)
        evidence.document(private / (prefix + "-reserved.json"), expected={"ordinal": ordinal,
            "intentReceiptSha256": intent_sha, "matrixState": matrix.private_state()})
        attempted.add(request.label)
        byte_limit = execution["budgets"]["totalResponseBytes"] - (
            0 if request.cleanup else execution["budgets"]["cleanupResponseBytes"]) - charged
        require(byte_limit > execution["budgets"]["responseBytes"], "response-byte-phase-reserve")
        charged += len(raw)
        collect_secrets(body, secrets, transport)
        for key, value in wire["responseHeaders"]:
            if key.lower() in transport.SECRET_KEYS:
                secrets.add(value)
        exported = transport.sanitized({"ordinal": ordinal, "actor": request.actor, "request": request.fact(),
            "response": {"status": wire["responseStatus"], "headers": [[key, "[redacted]" if key.lower() in transport.SECRET_KEYS
                else transport.sanitized(value, secrets)] for key, value in wire["responseHeaders"]], "body": body},
            "completedAt": wire["completedAt"], "completeHTTP": True, "privateWireSha256": wire_sha}, secrets)
        evidence.document(export / (prefix + "-response.json"), expected=exported)
        if request.route.endswith("/PlaybackInfo") and last_stop_time is not None:
            difference = (completed - last_stop_time).total_seconds()
            interval_checks.append({"label": request.label, "responseSeparationSeconds": difference,
                "meetsFrozenSeparation": difference >= execution["matrix"]["lifecycleSeparationSeconds"]})
        try:
            matrix.accept(wire["responseStatus"], body, wire["completedAt"], wire_sha, elapsed)
        except BaseException as error:
            require(matrix.pending is None and not request.login and not request.route.endswith("/PlaybackInfo") and
                    matrix.failure is not None and not request.cleanup, "unsupported-blocking-response-path")
            failure = {"kind": "observation-failure", "errorType": type(error).__name__, "message": str(error)}
            failure_events.append(failure)
        else:
            if request.login:
                tokens[request.actor] = body["AccessToken"]
            if request.route == "/emby/Sessions/Playing/Stopped":
                last_stop_time = completed
        ledger_index.append({"ordinal": ordinal, "label": request.label, "intentSha256": intent_sha, "wireSha256": wire_sha})
        elapsed += 0.001
    require(private_names == expected_private and export_names == expected_export, "complete-matrix-file-membership")
    require(matrix.mode in ("closed", "closed-with-observation-failure") and matrix.revoked == {"P", "Q"} and
            matrix.pending is None and matrix.current == matrix.baseline, "reconstructed-matrix-complete-cleanup")
    state = {"schemaVersion": 1, "matrix": matrix.private_state(), "tokens": tokens, "unverifiedLogins": {},
             "attemptedLabels": sorted(attempted), "chargedResponseBytes": charged, "failure": failure,
             "blocked": False, "finished": True, "persistenceFailure": None, "unresolvedResponses": [], "failureEvents": failure_events}
    evidence.document(private / "state.json", expected=state)
    result = {"schemaVersion": 1, "classification": "protocol transport; not client acceptance", "mode": matrix.mode,
              "outcome": matrix.outcome, "httpAttempts": len(attempted), "reservedRequests": matrix.count,
              "chargedResponseBytes": charged, "cleanupComplete": True, "failure": failure, "planSha256": matrix.plan_sha256,
              "sourceSha256": execution["sources"]["transport"]["sha256"], "unverifiedLoginActors": [], "unresolvedResponses": [],
              "evidenceComplete": True, "persistenceFailure": None, "liveAcceptanceClaim": False, "failureEvents": failure_events}
    evidence.document(export / "result.json", expected=transport.sanitized(result, secrets))
    terminal_path = OPERATOR_ROOT / "private/terminal.json"
    terminal = evidence.document(terminal_path)
    commit = evidence.document(OPERATOR_ROOT / "private/commit.json")
    terminal_export = evidence.document(OPERATOR_ROOT / "export/terminal.json")
    expected_status = "matrix_protocol_complete" if failure is None else "matrix_observation_failed_cleanup_complete"
    require(terminal["status"] == "awaiting_operator_commit" and terminal["candidateStatus"] == expected_status and
            terminal["completionCommitted"] is False and terminal["cleanupComplete"] is True and terminal["failure"] is None and
            same(terminal["transportResult"], result), "operator-terminal-transport-binding")
    require(commit["kind"] == "nextup-global-reference-matrix-operator-commit" and commit["status"] == expected_status and
            commit["runId"] == terminal["runId"] == attestation["runId"] and commit["attestationSha256"] == attestation_sha and
            commit["terminalPrivateSha256"] == sha(encoded(terminal)) and commit["terminalExportSha256"] == sha(encoded(terminal_export)) and
            same(commit["runtime"], terminal["pins"]["runtime"]) and same(commit["liveIdentity"], terminal["liveIdentity"]), "operator-commit-hash-binding")
    for record in (terminal, commit):
        require(record["independentRuntimeClosureRequired"] is True and record["clientAcceptanceClaim"] is False and
                record["resumeOrRetryAllowed"] is False, "operator-acceptance-boundary")
    require(terminal["pins"]["attestationSha256"] == attestation_sha and
            terminal["pins"]["executionSha256"] == attestation["execution"]["sha256"] and
            same(terminal["pins"]["sources"], attestation["sources"]), "operator-source-pins")
    identity = terminal["liveIdentity"]
    require(identity["indexComplete"] is True and identity["blocked"] is False and identity["pending"] is None and
            identity["persistenceFailure"] is None and Path(identity["index"]["path"]) == OPERATOR_ROOT / "private/identity-index.json", "identity-index-binding")
    evidence.descriptor(identity["index"])
    evidence.recheck()
    require(names(private) == expected_private and names(export) == expected_export and
            {child.name for child in MATRIX_ROOT.iterdir()} == {"private", "export"}, "final-matrix-file-membership")
    return {"status": "wire_reconstructed_cleanup_complete" if failure is None else "observation_failed_wire_reconstructed_cleanup_complete",
            "protocolMatrixCompleted": failure is None, "matrixOutcome": matrix.outcome,
            "requestCount": matrix.count, "normalRequestCount": matrix.normal_count,
            "cleanupRequestCount": matrix.count - matrix.normal_count, "cleanupReconstructed": True,
            "firstPositiveObserved": matrix.positive is not None, "extensionComplete": matrix.extension_complete,
            "r5R6Started": matrix.r5_r6_started, "ranking": matrix.ranking,
            "observationFailureLabel": None if matrix.failure is None else matrix.failure["label"],
            "ledgerSha256": sha(encoded(ledger_index)), "ledger": ledger_index,
            "responseTimestampsMonotonic": not response_clock_regressions,
            "responseClockRegressionLabels": response_clock_regressions,
            "recordedLifecycleIntervals": interval_checks, "privateFileCount": len(private_names), "exportFileCount": len(export_names),
            "operatorCommitSha256": evidence.files[str(OPERATOR_ROOT / "private/commit.json")]["sha256"],
            "matrixArtifactSha256": {"execution": evidence.files[str(private / "execution.json")]["sha256"],
                "plan": evidence.files[str(private / "plan.json")]["sha256"],
                "state": evidence.files[str(private / "state.json")]["sha256"],
                "exportResult": evidence.files[str(export / "result.json")]["sha256"]},
            "exactMatrixReceiptBytesReconstructed": True, "exactTransportStateAndResultBytesReconstructed": True,
            "exactResponseExportBytesReconstructed": True, "operatorExportHashVerified": True,
            "operatorExportRedactionReconstructed": False, "monotonicTimelineReconstructed": False,
            "liveIdentityChainIndependentlyVerified": False, "runtimeClosureIndependentlyVerified": False,
            "preparationAdmissionIndependentlyReplayed": False, "preservationIndependentlyVerified": False,
            "independentRuntimeClosureRequired": True, "clientAcceptanceClaim": False, "resumeOrRetryAllowed": False}


def main():
    global ATTESTATION, EXECUTION, OPERATOR_ROOT, OUTPUT
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode, "remote-root-isolated-no-bytecode-required")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--attestation-sha256", required=True)
    parser.add_argument("--run-number", type=int, choices=range(7, 100), default=7,
                        help="Select the matching fresh attestation, operator, and execution scope.")
    arguments = parser.parse_args()
    require(re.fullmatch(r"[0-9a-f]{64}", arguments.attestation_sha256) is not None, "attestation-sha256-format")
    scope_number = str(arguments.run_number).zfill(2)
    ATTESTATION = W / ("reference-nextup-global-matrix-attestation-" + scope_number) / "attestation.json"
    EXECUTION = ATTESTATION.parent / "published-execution.json"
    OPERATOR_ROOT = W / ("nextup-global-reference-operator-runs-" + scope_number) / ("operator-" + scope_number)
    OUTPUT = W / ("reference-nextup-global-matrix-execution-" + scope_number) / "independent-terminal.json"
    protected(OUTPUT.parent, directory=True)
    require(not os.path.lexists(OUTPUT), "fresh-independent-output-required")
    attempts = {"network": 0, "subprocess": 0, "unexpectedWrite": 0}

    def audit(event, args):
        if event.startswith("socket."):
            attempts["network"] += 1
            raise ReconstructionError("network-forbidden")
        if event.startswith("subprocess.") or event in ("os.system", "os.fork", "os.forkpty", "os.posix_spawn"):
            attempts["subprocess"] += 1
            raise ReconstructionError("subprocess-forbidden")
        if event in ("os.remove", "os.rename", "os.mkdir", "os.rmdir", "os.chmod", "os.chown",
                     "os.link", "os.symlink", "os.truncate"):
            attempts["unexpectedWrite"] += 1
            raise ReconstructionError("filesystem-mutation-forbidden")
        if event == "open":
            path, mode, flags = args
            writing = (isinstance(mode, str) and any(value in mode for value in "wax+")) or (
                isinstance(flags, int) and flags & (os.O_WRONLY | os.O_RDWR | os.O_CREAT | os.O_TRUNC | os.O_APPEND))
            if writing and (not isinstance(path, (str, bytes, os.PathLike)) or Path(os.fsdecode(path)) != OUTPUT):
                attempts["unexpectedWrite"] += 1
                raise ReconstructionError("write-outside-summary-forbidden")

    sys.addaudithook(audit)
    evidence = Evidence()
    summary = {"schemaVersion": 1, "kind": "nextup-reference-matrix-independent-wire-reconstruction",
               "runNumber": arguments.run_number,
               "attestationSha256": arguments.attestation_sha256, "status": "evidence_not_reconstructed",
               "clientAcceptanceClaim": False, "resumeOrRetryAllowed": False}
    try:
        summary.update(reconstruct(evidence, arguments.attestation_sha256))
    except BaseException as error:
        summary.update(status="evidence_not_reconstructed", errorType=type(error).__name__,
                       failedCheck=str(error) if isinstance(error, ReconstructionError) else "frozen-replay-or-input-failed",
                       exactMatrixReceiptBytesReconstructed=False, exactTransportStateAndResultBytesReconstructed=False,
                       cleanupReconstructed=False, independentRuntimeClosureRequired=True)
    summary.update(completedAt=datetime.now(timezone.utc).isoformat(), attemptCounters=attempts,
                   readFileCount=len(evidence.files), evidenceSetSha256=sha(encoded(evidence.files)))
    descriptor = os.open(OUTPUT, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        raw = encoded(summary)
        offset = 0
        while offset < len(raw):
            offset += os.write(descriptor, raw[offset:])
        os.fsync(descriptor)
    finally:
        os.close(descriptor)
    parent = os.open(OUTPUT.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)
    print(canonical({"status": summary["status"], "requestCount": summary.get("requestCount"),
                     "summarySha256": sha(encoded(summary)), "clientAcceptanceClaim": False}))
    return 0 if summary["status"] == "wire_reconstructed_cleanup_complete" else 2


if __name__ == "__main__":
    raise SystemExit(main())
