#!/usr/bin/env python3
"""Publish one private discovery input and load its inactive, restricted unit.

This controller reads frozen matrix07 evidence and metadata only. It performs
no HTTP, launches no browser, and has no service-start or resume operation.
"""

from __future__ import annotations

import argparse
import base64
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys

W = Path("/opt/goby-test/exec-work-m3e")
TOOL = W / "nextup-client-discovery-tool-01/revision-01"
INPUTS = W / "nextup-client-discovery-inputs-01"
ROOT = W / "reference-nextup-client-discovery-01"
E = W / "reference-nextup-client-discovery-execution-01"
E07 = W / "reference-nextup-global-matrix-execution-07"
A07 = W / "reference-nextup-global-matrix-attestation-07"
PREP = W / "reference-nextup-global-preparation-05/private"
MATRIX = W / "nextup-global-reference-runs-05/matrix-05/private"
OPERATOR = W / "nextup-global-reference-operator-runs-07/operator-07/private"
UNIT = "goby-nextup-client-discovery-01.service"
UNIT_FILE = Path("/run/systemd/system") / UNIT
MATRIX_UNIT = "goby-nextup-global-reference-matrix-07.service"
FILES = ("client-browser-nextup-discovery.mjs", "client-browser-nextup-discovery-runtime.mjs",
         "client-browser-session-proof.mjs")
NODE = Path("/usr/bin/node")
NODE_SHA = "ca0728526aa1cc4e3056decec848ecc6d2c5391cecdd4e21a0ebd221d665c84e"
PINS = {
    "execution": (A07 / "published-execution.json", "586860e8109ee3b9d21f13126a9d5b715f02c2c95d7a6d115c060eab7b234766"),
    "attestation": (A07 / "attestation.json", "c2dfa248e6823b0e9bc4aa04f9034d9ca0c872de80895f4ce5a50db32c66021a"),
    "matrixTerminal": (OPERATOR / "terminal.json", "b08b1a2ea8ce659ac1868df44b557455efccf69ae2dfd5caf33bb0c1b8b627da"),
    "operatorCommit": (OPERATOR / "commit.json", "46e5811f1b2099ad49a20a4915fe0d83bd6adeb3ea0cdd5a51d455b714ba2b18"),
    "independentTerminal": (E07 / "independent-terminal.json", "4689a670ad80efcd0334c3f1956e540a986cfbc735da8f4e6b93acd68d804d8c"),
    "independentRuntimeTerminal": (E07 / "independent-runtime-terminal.json", "782ab2ddb4e1d979f9f93a42b3c1d9cbb9b1078413d5e8a354242ff715eeae93"),
}
BUDGETS = {"maximumSeconds": 1200, "normalSeconds": 840, "maximumPlaybackAttempts": 2,
           "permittedPlaybackAttempts": 0, "recorderRequests": 34, "recorderBodyBytes": 2097152,
           "browserNavigationActions": 3}
MAX_JSON = 32 * 1024 * 1024
READS = {}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def checksum(value):
    require(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value), "An exact SHA-256 is required.")
    return value


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + "\n").encode()


def decode(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            require(key not in result, "Duplicate JSON keys are forbidden.")
            result[key] = value
        return result

    def constant(value):
        raise ValueError("Nonfinite JSON is forbidden.")

    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_constant=constant)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def owned(path, *, directory=False, private=False):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts, "An exact absolute path is required.")
    for candidate in (path, *path.parents):
        info = candidate.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                "An authority path has unsafe ownership, permissions or symlink ancestry.")
        if candidate == path:
            require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "An authority has the wrong type.")
            require(directory or info.st_nlink == 1, "An authority file has unexpected hard links.")
            require(not private or not info.st_mode & 0o077, "A private authority is not owner-only.")
            result = info
        else:
            require(stat.S_ISDIR(info.st_mode), "An authority ancestor is not a directory.")
    return result


def read(path, expected, *, private=True, maximum=MAX_JSON):
    path = Path(path)
    before = owned(path, private=private)
    require(before.st_size <= maximum, "An authority exceeds its finite byte limit.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), "An authority changed during open.")
        raw = stream.read(maximum + 1)
        require(len(raw) <= maximum and identity(os.fstat(stream.fileno())) == identity(before), "An authority changed during read.")
    require(identity(owned(path, private=private)) == identity(before) and sha(raw) == checksum(expected),
            "An authority identity or SHA-256 differs.")
    row = {"path": str(path), "sha256": expected, "identity": list(identity(before)), "private": private}
    require(str(path) not in READS or READS[str(path)] == row, "A previously read authority changed.")
    READS[str(path)] = row
    return raw


def desc(path, value):
    return {"path": str(path), "sha256": checksum(value)}


def document(row, expected_path):
    require(isinstance(row, dict) and set(row) == {"path", "sha256"} and row["path"] == str(expected_path),
            "An evidence descriptor escaped its exact allowed file.")
    return decode(read(expected_path, row["sha256"]))


def recheck_reads():
    for row in list(READS.values()):
        require(list(identity(owned(row["path"], private=row["private"]))) == row["identity"],
                "A pinned source or evidence file changed before publication.")


def synchronize(path):
    owned(path, directory=True)
    handle = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(handle)
    finally:
        os.close(handle)


def publish(path, raw):
    owned(path.parent, directory=True)
    require(not os.path.lexists(path), "An output cannot be replaced or resumed.")
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    synchronize(path.parent)
    require(path.read_bytes() == raw, "A newly published output differs.")
    return desc(path, sha(raw))


def create_directory(path):
    owned(path.parent, directory=True)
    require(not os.path.lexists(path), "A new scope is already occupied.")
    os.mkdir(path, 0o700)
    synchronize(path.parent)
    owned(path, directory=True, private=True)


def show(name, keys, *, missing=False):
    require(re.fullmatch(r"[A-Za-z0-9_.-]+\.service", name), "An exact service name is required.")
    result = subprocess.run(["/usr/bin/systemctl", "show", name, "--property=" + ",".join(sorted(keys))],
                            capture_output=True, timeout=15, check=False)
    require(len(result.stdout) <= 65536 and len(result.stderr) <= 65536 and (missing or result.returncode == 0),
            "A bounded systemd observation failed.")
    values = dict(line.split("=", 1) for line in result.stdout.decode("utf-8", errors="strict").splitlines() if "=" in line)
    require(set(values) == set(keys), "A required systemd property is missing.")
    return values


def verify_shutdown(runtime):
    unit = runtime["unit"]
    require(unit["name"] == MATRIX_UNIT and unit["argvExact"] is True and unit["formerPidAbsent"] is True and
            unit["cgroupAbsent"] is True, "The matrix has no complete independent shutdown proof.")
    expected = unit["properties"]
    require(expected["MainPID"] == "0" and expected["Result"] == "success" and expected["ExecMainStatus"] == "0" and
            (expected["ActiveState"], expected["SubState"]) in (("active", "exited"), ("inactive", "dead")),
            "The matrix worker did not complete successfully.")
    require(show(MATRIX_UNIT, set(expected)) == expected, "The completed matrix unit changed after independent closure.")
    matrix_unit_file = Path("/run/systemd/system") / MATRIX_UNIT
    require(expected["FragmentPath"] == str(matrix_unit_file), "The completed matrix unit names another unit file.")
    read(matrix_unit_file, "1d39043e34da230dff75d2902944fbcc981a5985f53485797d3f6305cf258242")
    command = show(MATRIX_UNIT, {"ExecStart"})["ExecStart"]
    argv = ["/usr/bin/python3", "-I", "-B", str(W / "nextup-global-reference-operator-tool-05/revision-01/run-nextup-global-reference.py"),
            "--attestation", str(PINS["attestation"][0]), "--attestation-sha256", PINS["attestation"][1]]
    require(command.count("argv[]=") == 1 and shlex.split(command.split("argv[]=", 1)[1].split(";", 1)[0].strip()) == argv,
            "The completed matrix unit command differs from its attested operation.")
    require(not os.path.lexists(Path("/proc") / str(unit["runtime"]["pid"])) and
            not os.path.lexists(Path("/sys/fs/cgroup/system.slice") / MATRIX_UNIT),
            "A former matrix worker or its cgroup is present.")


def proc_bytes(path, maximum=65536):
    with open(path, "rb") as stream:
        raw = stream.read(maximum + 1)
    require(len(raw) <= maximum, "A process metadata observation exceeded its bound.")
    return raw


def process_snapshot(execution):
    result = {}
    expected_process = execution["process"]
    boot = proc_bytes("/proc/sys/kernel/random/boot_id").decode().strip()
    for role in ("application", "endpoint"):
        expected = expected_process[role]
        pid = expected["pid"]
        require(type(pid) is int and pid > 1 and expected["bootId"] == boot, "A target process identity is invalid.")
        root = Path("/proc") / str(pid)
        first = proc_bytes(root / "stat").decode()
        fields = first[first.rfind(")") + 2:].split()
        status = proc_bytes(root / "status").decode()
        uid = int(re.search(r"^Uid:\s+(\d+)", status, re.MULTILINE).group(1))
        raw = {"pid": pid, "bootId": boot, "startTicks": fields[19], "uid": uid,
               "exe": os.readlink(root / "exe"), "cmdline": proc_bytes(root / "cmdline").decode().rstrip("\0").split("\0"),
               "cgroup": proc_bytes(root / "cgroup").decode(), "networkNamespace": os.readlink(root / "ns/net")}
        handle = os.open(root / "exe", os.O_PATH | os.O_CLOEXEC)
        try:
            opened = os.fstat(handle)
            named = (root / "exe").stat()
            require(identity(opened) == identity(named) and stat.S_ISREG(opened.st_mode), "The executable metadata is unstable.")
            require((opened.st_dev, opened.st_ino) == (expected["exeDevice"], expected["exeInode"]),
                    "The bound executable file object changed.")
            require(raw["exe"] == expected["exe"] or role == "endpoint" and raw["exe"] == expected["exe"] + " (deleted)" and opened.st_nlink == 0,
                    "The executable display changed outside the explicit proxy rule.")
            raw["executableStat"] = list(identity(opened))
            require(identity(os.fstat(handle)) == identity(opened) and identity((root / "exe").stat()) == identity(opened),
                    "The executable changed during its metadata-only observation.")
        finally:
            os.close(handle)
        last = proc_bytes(root / "stat").decode()
        last_fields = last[last.rfind(")") + 2:].split()
        require(fields[0] != "Z" and last_fields[0] != "Z" and last_fields[19] == fields[19],
                "The target process changed during observation.")
        require(all(raw[key] == expected[key] for key in ("pid", "bootId", "startTicks", "uid", "cmdline", "cgroup", "networkNamespace")),
                "A bound target process or namespace changed.")
        if role == "endpoint":
            expected_socket = "socket:[" + expected["listener"]["socketInode"] + "]"
            links = []
            entries = list((root / "fd").iterdir())
            require(len(entries) <= 4096, "The owned proxy descriptor count exceeds its bound.")
            for entry in entries:
                try:
                    links.append(os.readlink(entry))
                except FileNotFoundError:
                    pass
            require(expected_socket in links and expected["listener"]["port"] == 18197, "The proxy no longer owns its bound listener.")
            listeners = [line.split() for line in proc_bytes(root / "net/tcp", 1024 * 1024).decode().splitlines()[1:]]
            require(any(len(row) > 9 and row[1] == "0100007F:4715" and row[3] == "0A" and
                        row[9] == expected["listener"]["socketInode"] for row in listeners), "The bound proxy listener is not listening.")
        result[role] = raw
    require(os.readlink("/proc/self/ns/net") == expected_process["workerNetworkNamespace"], "The controller network namespace differs.")
    return result


def release_evidence():
    documents = {key: decode(read(path, checksum_value)) for key, (path, checksum_value) in PINS.items()}
    execution, attestation = documents["execution"], documents["attestation"]
    terminal, commit = documents["matrixTerminal"], documents["operatorCommit"]
    wire, runtime = documents["independentTerminal"], documents["independentRuntimeTerminal"]
    require(execution["matrix"]["target"] == "reference" and execution["matrix"]["binding"]["fixtureReleased"] is True and
            execution["endpoint"] == {"scheme": "http", "host": "127.0.0.1", "port": 18197}, "The execution is not the released reference fixture.")
    require(attestation["execution"] == desc(*PINS["execution"]), "The matrix attestation names another execution.")
    require(commit["status"] == "matrix_protocol_complete" and commit["terminalPrivateSha256"] == PINS["matrixTerminal"][1] and
            commit["attestationSha256"] == PINS["attestation"][1] and commit["resumeOrRetryAllowed"] is False,
            "The original operator commit does not close this matrix.")
    require(terminal["status"] == "awaiting_operator_commit" and terminal["candidateStatus"] == commit["status"] and
            terminal["completionCommitted"] is False and terminal["cleanupComplete"] is True and terminal["failure"] is None,
            "The retained worker terminal differs from its committed successful result.")
    transport = terminal["transportResult"]
    require(transport["mode"] == "closed" and transport["outcome"] == "reference_global_positive_unresolved" and
            transport["cleanupComplete"] is True and transport["evidenceComplete"] is True and transport["failure"] is None and
            transport["httpAttempts"] == 158 and transport["unresolvedResponses"] == [] and transport["unverifiedLoginActors"] == [],
            "The matrix transport retains incomplete responsibility.")
    require(wire["kind"] == "nextup-reference-matrix-independent-wire-reconstruction" and
            wire["status"] == "wire_reconstructed_cleanup_complete" and wire["protocolMatrixCompleted"] is True and
            wire["cleanupReconstructed"] is True and wire["exactMatrixReceiptBytesReconstructed"] is True and
            wire["exactResponseExportBytesReconstructed"] is True and wire["exactTransportStateAndResultBytesReconstructed"] is True and
            wire["requestCount"] == 158 and wire["operatorCommitSha256"] == PINS["operatorCommit"][1] and
            wire["matrixArtifactSha256"]["execution"] == PINS["execution"][1] and wire["r5R6Started"] is False,
            "The independent wire reconstruction is incomplete or bound elsewhere.")
    require(runtime["kind"] == "nextup-global-matrix-independent-runtime-terminal" and
            runtime["status"] == "runtime_identity_preservation_independently_verified" and runtime["failure"] is None and
            runtime["runtimeIdentityPreservationVerified"] is True and runtime["commit"] == desc(*PINS["operatorCommit"]) and
            runtime["terminalPrivateSha256"] == PINS["matrixTerminal"][1] and runtime["rawReceiptCount"] == 475 and
            runtime["canonicalReturnCount"] == runtime["aliasReturnCount"] == 475 and runtime["rawAndKernelAnchorStable"] is True and
            runtime["postPersistenceChecksBoundByIndex"] is True and runtime["actualBusinessHttpRequests"] == 158,
            "The independent runtime or 475-call identity closure is incomplete.")
    require(commit["liveIdentity"] == terminal["liveIdentity"] and commit["liveIdentity"]["indexComplete"] is True and
            commit["liveIdentity"]["blocked"] is False and commit["liveIdentity"]["pending"] is None and
            commit["liveIdentity"]["persistenceFailure"] is None and commit["liveIdentity"]["probeCount"] == 475,
            "The committed live identity evidence is incomplete.")
    state = decode(read(MATRIX / "state.json", wire["matrixArtifactSha256"]["state"]))
    matrix = state["matrix"]
    require(state["finished"] is True and state["blocked"] is False and state["failure"] is None and state["persistenceFailure"] is None and
            state["unverifiedLogins"] == {} and state["unresolvedResponses"] == [] and matrix["pending"] is None and
            matrix["mode"] == "closed" and matrix["revokedActors"] == ["P", "Q"] and matrix["restorationFailed"] is False and
            matrix["remainingSteps"] == [] and matrix["current"] == matrix["baseline"], "The matrix did not restore its complete zero baseline.")
    require(set(matrix["baseline"]) == {"P", "Q"}, "The baseline actors differ.")
    for actor in ("P", "Q"):
        require(set(matrix["baseline"][actor]) == {"A1", "A2", "A3", "B1", "B2", "B3"}, "The baseline episode set differs.")
        for episode in matrix["baseline"][actor].values():
            value = episode["value"]
            require(value["Played"] is False and type(value["PlayCount"]) is int and value["PlayCount"] == 0 and
                    type(value["PlaybackPositionTicks"]) is int and value["PlaybackPositionTicks"] == 0 and value.get("LastPlayedDate") is None,
                    "A retained episode baseline contains playback history.")
    require(all(play["stopped"] is True for play in matrix["plays"].values()), "A matrix playback remains active.")
    verify_shutdown(runtime)
    return documents


def device_inputs(documents):
    execution, attestation, wire = documents["execution"], documents["attestation"], documents["independentTerminal"]
    manifest = document(attestation["preparation"]["manifest"], PREP / "manifest.json")
    baseline = document(manifest["inputs"]["publicBaseline"], W / "reference-nextup-global-baseline-observation-03/private/public-baseline.json")
    require(isinstance(baseline["devices"], dict) and len(baseline["devices"]) == 98, "The accepted baseline device population differs.")
    ids = [row["ReportedDeviceId"] for row in baseline["devices"].values()]
    require(len(set(ids)) == 98, "The accepted baseline contains duplicate reported device identities.")
    prep_index = document(attestation["preparation"]["wireIndex"], A07 / "wire-index.json")
    require(prep_index["producerRoot"] == str(PREP.parent) and len(prep_index["requests"]) == 269, "The preparation wire index differs.")
    sources = {"publicBaseline": manifest["inputs"]["publicBaseline"], "preparationLogins": [], "matrixLogins": []}
    for actor, ordinal in (("admin", 1), ("P", 89), ("Q", 107)):
        label = "login-" + actor
        row = prep_index["requests"][ordinal - 1]
        require(row["ordinal"] == ordinal and row["label"] == label, "A preparation login ordinal changed.")
        path = PREP / ("%04d-%s-response.json" % (ordinal, label))
        response = document(row["response"], path)
        require(response["actor"] == actor and response["label"] == label and response["ordinal"] == ordinal and
                response["status"] == 200 and response["completeHttp"] is True and response["failure"] is None and
                response["retainedRawTruncated"] is False, "A preparation login is incomplete.")
        raw = base64.b64decode(response["rawBase64"], validate=True)
        require(len(raw) == response["observedRawBytes"] <= MAX_JSON, "A preparation login body is truncated.")
        body = decode(raw)
        require(body["ServerId"] == execution["matrix"]["serverId"] and
                body["SessionInfo"]["DeviceId"] == manifest["actors"][actor]["deviceId"] and
                body["User"]["Id"] == body["SessionInfo"]["UserId"], "A preparation login names another actor or device.")
        ids.append(body["SessionInfo"]["DeviceId"])
        sources["preparationLogins"].append(row["response"])
    require(len(wire["ledger"]) == 158, "The matrix wire ledger is incomplete.")
    for actor, ordinal in (("P", 1), ("Q", 10)):
        label = "PRE-" + actor + "-login"
        row = wire["ledger"][ordinal - 1]
        require(row["ordinal"] == ordinal and row["label"] == label, "A matrix login ordinal changed.")
        path = MATRIX / ("%04d-%s-wire.json" % (ordinal, label))
        response_descriptor = desc(path, row["wireSha256"])
        response = document(response_descriptor, path)
        require(response["ordinal"] == ordinal and response["request"]["actor"] == actor and response["request"]["label"] == label and
                response["responseStatus"] == 200 and response["completeHTTP"] is True and response["transportFailure"] is None and
                response["adapterTruncatedCapture"] is False and response["headerCaptureTruncated"] is False and
                response["intentReceiptSha256"] == row["intentSha256"], "A matrix login wire is incomplete or has another intent.")
        raw = base64.b64decode(response["responseBodyBase64"], validate=True)
        require(len(raw) == response["reportedResponseBytes"] <= MAX_JSON, "A matrix login body is truncated.")
        body = decode(raw)
        require(body["ServerId"] == execution["matrix"]["serverId"] and body["User"]["Id"] == execution["matrix"]["actors"][actor]["userId"] and
                body["SessionInfo"]["UserId"] == body["User"]["Id"], "A matrix login names another actor.")
        ids.append(body["SessionInfo"]["DeviceId"])
        sources["matrixLogins"].append(response_descriptor)
    require(all(isinstance(value, str) and 0 < len(value) <= 256 for value in ids), "A reported device identity is invalid.")
    ids = sorted(set(ids))
    require(all("goby-nextup-client-discovery-01-" + suffix not in ids for suffix in ("before", "after")),
            "A future state-observer device already exists.")
    libraries = {key: {"viewId": execution["matrix"]["libraries"][key]["viewId"], "name": manifest["libraries"][key]["name"]}
                 for key in ("LA", "LB")}
    require(libraries["LA"]["viewId"] == "133" and libraries["LB"]["viewId"] == "135" and
            all(isinstance(row["name"], str) and 0 < len(row["name"]) <= 256 for row in libraries.values()), "The owned library identity differs.")
    return ids, libraries, sources


def unit_bytes(input_descriptor):
    argv = ["/usr/bin/flock", "--exclusive", "--nonblock", "--no-fork", str(W / "reference.lock"),
            str(NODE), str(TOOL / FILES[0]), "--input", input_descriptor["path"], "--input-sha256", input_descriptor["sha256"]]
    lines = ["[Unit]", "Description=Goby bounded NextUp original-client discovery 01", "", "[Service]",
             "Type=oneshot", "RemainAfterExit=yes", "Restart=no", "User=root", "UMask=0077", "NoNewPrivileges=yes",
             "ProtectSystem=strict", "ProtectHome=read-only", "PrivateTmp=yes", "PrivateNetwork=no", "MemoryMax=2G",
             "TasksMax=192", "CPUQuota=100%", "TimeoutStartSec=1320", "TimeoutStopSec=20", "KillMode=control-group",
             "WorkingDirectory=/", "Environment=PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright",
             "ReadWritePaths=" + str(ROOT) + " " + str(W / "reference.lock"),
             "StandardOutput=append:" + str(E / "unit-stdout.log"), "StandardError=append:" + str(E / "unit-stderr.log"),
             "ExecStart=" + " ".join(argv), ""]
    return "\n".join(lines).encode(), argv


def verify_new_unit(argv):
    strings = {"Id": UNIT, "LoadState": "loaded", "ActiveState": "inactive", "SubState": "dead", "MainPID": "0",
               "ExecMainPID": "0", "InvocationID": "", "Type": "oneshot", "RemainAfterExit": "yes", "Restart": "no",
               "User": "root", "UMask": "0077", "NoNewPrivileges": "yes", "ProtectSystem": "strict", "ProtectHome": "read-only",
               "PrivateTmp": "yes", "PrivateNetwork": "no", "MemoryMax": str(2 * 1024 ** 3), "TasksMax": "192",
               "KillMode": "control-group", "WorkingDirectory": "/", "FragmentPath": str(UNIT_FILE),
               "StandardOutput": "append", "StandardError": "append", "Environment": "PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright"}
    extra = {"ExecStart", "ReadWritePaths", "TimeoutStartUSec", "TimeoutStopUSec", "CPUQuotaPerSecUSec"}
    value = show(UNIT, set(strings) | extra)
    require(all(value[key] == expected for key, expected in strings.items()), "The loaded unit differs from its inactive restricted contract.")
    def seconds(text):
        parts = re.findall(r"([0-9]+)(us|ms|s|min|h)", text.replace(" ", ""))
        require(parts and "".join(number + unit for number, unit in parts) == text.replace(" ", ""), "A systemd duration is unsupported.")
        return sum(int(number) * {"us": 0.000001, "ms": 0.001, "s": 1, "min": 60, "h": 3600}[unit] for number, unit in parts)
    require(seconds(value["TimeoutStartUSec"]) == 1320 and seconds(value["TimeoutStopUSec"]) == 20 and
            seconds(value["CPUQuotaPerSecUSec"]) == 1 and len(shlex.split(value["ReadWritePaths"])) == 2 and
            set(shlex.split(value["ReadWritePaths"])) == {str(ROOT), str(W / "reference.lock")},
            "The loaded unit has another timeout, CPU allowance or write scope.")
    require(value["ExecStart"].count("argv[]=") == 1 and
            shlex.split(value["ExecStart"].split("argv[]=", 1)[1].split(";", 1)[0].strip()) == argv,
            "The loaded unit command differs from its exact input and non-forking lock wrapper.")
    return value


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode, "Use root SSH with /usr/bin/python3 -I -B.")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source-manifest", required=True, type=Path)
    parser.add_argument("--source-manifest-sha256", required=True)
    parser.add_argument("--source-sha256", required=True)
    args = parser.parse_args()
    owned(E, directory=True, private=True)
    self_path = E / "prepare-nextup-client-discovery.py"
    require(Path(__file__).absolute() == self_path and args.source_manifest == E / "source-approval.json", "The controller or approval path differs.")
    read(self_path, args.source_sha256)
    approval_descriptor = desc(args.source_manifest, args.source_manifest_sha256)
    approval = document(approval_descriptor, E / "source-approval.json")
    require(set(approval) == {"kind", "version", "files"} and approval["kind"] == "nextup-client-discovery-source-approval" and
            type(approval["version"]) is int and approval["version"] == 1 and set(approval["files"]) == set(FILES),
            "The root's exact three-file source approval is required.")
    for name in FILES:
        row = approval["files"][name]
        require(set(row) == {"path", "sha256"} and row["path"] == str(TOOL / name), "A source escaped the approved tool directory.")
        read(TOOL / name, row["sha256"], private=False)
    read(NODE, NODE_SHA, private=False, maximum=256 * 1024 * 1024)
    owned(Path("/usr/bin/flock"))
    for path in (INPUTS, ROOT, UNIT_FILE, E / "preparation-intent.json", E / "preparation.json", E / "unit-stdout.log", E / "unit-stderr.log"):
        require(not os.path.lexists(path), "The discovery preparation scope is occupied; no resume is permitted.")
    missing = show(UNIT, {"LoadState", "ActiveState", "SubState", "MainPID"}, missing=True)
    require(missing == {"LoadState": "not-found", "ActiveState": "inactive", "SubState": "dead", "MainPID": "0"},
            "The exact discovery unit already exists or is active.")
    documents = release_evidence()
    existing_ids, libraries, device_sources = device_inputs(documents)
    execution = documents["execution"]
    lock = execution["lock"]
    require(lock["path"] == str(W / "reference.lock"), "The fixture lock path differs.")
    lock_before = owned(Path(lock["path"]), private=True)
    require((lock_before.st_dev, lock_before.st_ino) == (lock["device"], lock["inode"]), "The fixture lock identity changed.")
    lock_fd = os.open(lock["path"], os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        require(identity(os.fstat(lock_fd)) == identity(lock_before), "The fixture lock changed while opening.")
        fcntl.flock(lock_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        before = process_snapshot(execution)
        verify_shutdown(documents["independentRuntimeTerminal"])
        recheck_reads()
        intent = publish(E / "preparation-intent.json", encoded({
            "kind": "nextup-client-discovery-preparation-intent", "version": 1, "createdAt": datetime.now(timezone.utc).isoformat(),
            "controller": desc(self_path, args.source_sha256), "sourceApproval": approval_descriptor, "sourceClosure": approval["files"],
            "matrixEvidence": {key: desc(*value) for key, value in PINS.items()}, "deviceSources": device_sources,
            "node": desc(NODE, NODE_SHA), "lockIdentity": list(identity(lock_before)), "metadataBefore": before,
            "readFiles": sorted(READS.values(), key=lambda row: row["path"]), "oneShot": True, "startCalls": 0, "businessHttpRequests": 0}))
        create_directory(INPUTS)
        create_directory(ROOT)
        release = {"kind": "nextup-reference-client-discovery-release", "version": 1,
                   **{key: desc(*PINS[key]) for key in ("execution", "matrixTerminal", "independentTerminal", "independentRuntimeTerminal", "operatorCommit")},
                   "apiClosed": True, "revokedActors": ["P", "Q"], "zeroBaselineVerified": True, "unitInactive": True, "originalClientAllowed": True}
        release_descriptor = publish(INPUTS / "release.json", encoded(release))
        input_value = {"kind": "nextup-reference-client-discovery-input", "version": 1,
                       "runId": "nextup-reference-client-discovery-01", "root": str(ROOT), "execution": desc(*PINS["execution"]),
                       "release": release_descriptor, "sourceClosure": approval["files"], "existingDeviceIds": existing_ids,
                       "libraries": libraries, "budgets": BUDGETS}
        input_descriptor = publish(INPUTS / "input.json", encoded(input_value))
        unit_raw, argv = unit_bytes(input_descriptor)
        unit_descriptor = publish(UNIT_FILE, unit_raw)
        reload = subprocess.run(["/usr/bin/systemctl", "daemon-reload"], capture_output=True, timeout=30, check=False)
        require(reload.returncode == 0, "The one permitted daemon reload failed.")
        properties = verify_new_unit(argv)
        require(not list(ROOT.iterdir()) and not os.path.lexists(E / "unit-stdout.log") and not os.path.lexists(E / "unit-stderr.log"),
                "Loading the unit unexpectedly populated a fresh output or log.")
        require(UNIT_FILE.read_bytes() == unit_raw, "The newly loaded unit bytes changed.")
        after = process_snapshot(execution)
        require(after == before and identity(os.fstat(lock_fd)) == identity(lock_before) and identity(owned(Path(lock["path"]), private=True)) == identity(lock_before),
                "A bound process or the existing fixture lock changed during preparation.")
        verify_shutdown(documents["independentRuntimeTerminal"])
        recheck_reads()
        record = publish(E / "preparation.json", encoded({
            "kind": "nextup-client-discovery-preparation", "version": 1, "capturedAt": datetime.now(timezone.utc).isoformat(),
            "intent": intent, "input": input_descriptor, "release": release_descriptor, "unitFile": unit_descriptor,
            "unitName": UNIT, "properties": properties, "sourceApproval": approval_descriptor, "sourceClosure": approval["files"],
            "node": desc(NODE, NODE_SHA), "existingDeviceCount": len(existing_ids), "deviceSources": device_sources,
            "metadataBefore": before, "metadataAfter": after, "lockIdentity": list(identity(lock_before)),
            "outputRootEmpty": True, "notStarted": True, "startCalls": 0, "businessHttpRequests": 0,
            "daemonReloadCalls": 1, "oldScopesWritten": False, "originalImplementationBytesRead": False,
            "referenceDatabaseRead": False, "clientAcceptanceClaim": False}))
        print(json.dumps({"record": record, "input": input_descriptor, "release": release_descriptor, "unitFile": unit_descriptor,
                          "unitName": UNIT, "existingDeviceCount": len(existing_ids), "notStarted": True, "businessHttpRequests": 0}, sort_keys=True))
    finally:
        os.close(lock_fd)


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"status": "nextup_discovery_preparation_failed", "errorType": type(error).__name__,
                          "resumeAllowed": False, "startCalls": 0, "businessHttpRequests": 0}), file=sys.stderr)
        raise SystemExit(2)
