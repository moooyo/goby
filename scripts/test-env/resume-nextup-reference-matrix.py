#!/usr/bin/env python3
"""Prepare matrix07 from frozen matrix06 helpers without starting business work.

Each phase is one-shot. The root task supplies this file's SHA-256 and the
previous phase's exact receipt hash. Existing preparation, diagnostic, matrix,
source and unit scopes are read only. This helper has no launch operation.
"""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

WORK = Path("/opt/goby-test/exec-work-m3e")
EXECUTION = WORK / "reference-nextup-global-matrix-execution-07"
OPERATOR_PARENT = WORK / "nextup-global-reference-operator-runs-07"
ASSEMBLY_TOOL = WORK / "nextup-global-matrix-assembly-tool-03"
ASSEMBLY_SOURCE = ASSEMBLY_TOOL / "revision-01/assemble-matrix-attestation-07.py"
ASSEMBLY_INPUTS = WORK / "nextup-global-matrix-assembly-inputs-07"
ATTESTATION_ROOT = WORK / "reference-nextup-global-matrix-attestation-07"
UNIT = "goby-nextup-global-reference-matrix-07.service"
UNIT_FILE = Path("/run/systemd/system") / UNIT
OLD_OPERATOR_NAME = "nextup-global-reference-operator-tool-04/revision-01/run-nextup-global-reference.py"
OPERATOR_NAME = "nextup-global-reference-operator-tool-05/revision-01/run-nextup-global-reference.py"
OLD_OPERATOR_SHA = "e392591224e909d7f48b6af731d2808bde8641674a67f48dbd305464eca060e2"
OPERATOR_SHA = "ece90b7ff8a0d56a1b08825bd0dd7ec5b4603da432af6410a4af8ebe4dd968ce"
OLD_PREPARE = WORK / "reference-nextup-global-matrix-execution-06/prepare-matrix-unit-06.py"
OLD_PREPARE_SHA = "1e1ded4d06b935a591a985d53998903df5a6c566f9385c15afa1f8cf972975e1"
OLD_ASSEMBLER = WORK / "nextup-global-matrix-assembly-tool-02/revision-01/assemble-matrix-attestation-06.py"
OLD_ASSEMBLER_SHA = "85c27dc683c2ae9b56a0e2abebc053ac943d0d2e06766e47221f29494b686a51"
OLD_FINALIZE = WORK / "reference-nextup-global-matrix-execution-06/finalize-matrix-unit-06.py"
OLD_FINALIZE_SHA = "153dc24a351a8e802cbd2059f2b4dc8e3de2f0e2a15507a849e4b194a2361284"
OLD_CONFIG = WORK / "nextup-global-matrix-assembly-inputs-06/input.json"
OLD_CONFIG_SHA = "c4ba8af18fe09974db424e1fa165a37cf167e9efb2d26f1f5c2a18b3a2215224"
INPUT_MANIFEST = {"path": str(WORK / "nextup-global-reference-preparation-inputs-05/input.json"),
                  "sha256": "aea806c70aa7e0f84c55bdbac3eab4c436ccce41c4769532ee53c5751a5bdea5"}
PLAN_SHA = "2fff709c64b7e1747a120cf03f1e222302879c4c4f7ac020d07a221a3c53e6d5"
MAX_BYTES = 8 * 1024 * 1024


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def checksum(value):
    require(isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value), "An exact SHA-256 is required.")
    return value


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False) + "\n").encode()


def decode(raw):
    def pairs(rows):
        value = {}
        for key, item in rows:
            require(key not in value, "Duplicate JSON keys are forbidden.")
            value[key] = item
        return value

    def constant(value):
        raise ValueError("Nonfinite JSON is forbidden.")

    return json.loads(raw.decode("utf-8", errors="strict"), object_pairs_hook=pairs, parse_constant=constant)


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_nlink, info.st_uid,
            info.st_gid, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def owned(path, *, directory=False, private=False):
    path = Path(path)
    require(path.is_absolute() and ".." not in path.parts, "An exact absolute path is required.")
    for item in (path, *path.parents):
        info = item.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022,
                "An authority path has unsafe ownership, mode or symlink ancestry.")
        if item == path:
            require(stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode), "An authority has the wrong type.")
            require(directory or info.st_nlink == 1, "An authority file has unexpected hard links.")
            require(not private or not info.st_mode & 0o077, "Private authority is not owner-only.")
            result = info
        else:
            require(stat.S_ISDIR(info.st_mode), "An authority ancestor is not a directory.")
    return result


def read(path, expected=None, *, private=False):
    path = Path(path)
    before = owned(path, private=private)
    require(before.st_size <= MAX_BYTES, "An authority file exceeds its byte bound.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        require(identity(os.fstat(stream.fileno())) == identity(before), "An authority changed during open.")
        raw = stream.read(MAX_BYTES + 1)
        require(len(raw) <= MAX_BYTES and identity(os.fstat(stream.fileno())) == identity(before),
                "An authority changed during its bounded read.")
    require(identity(owned(path, private=private)) == identity(before), "An authority path changed during reading.")
    if expected is not None:
        require(digest(raw) == checksum(expected), "A frozen authority digest differs.")
    return raw


def descriptor(path, expected=None, *, private=False):
    return {"path": str(path), "sha256": digest(read(path, expected, private=private))}


def read_descriptor(row, expected_path, *, private=True):
    require(isinstance(row, dict) and set(row) == {"path", "sha256"} and row["path"] == str(expected_path),
            "A receipt escaped its exact expected path.")
    return read(expected_path, row["sha256"], private=private)


def sync_directory(path):
    owned(path, directory=True)
    handle = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(handle)
    finally:
        os.close(handle)


def publish(path, raw):
    path = Path(path)
    owned(path.parent, directory=True)
    require(not os.path.lexists(path), "A phase output cannot be replaced or resumed.")
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), "wb") as stream:
        stream.write(raw)
        stream.flush()
        os.fsync(stream.fileno())
    sync_directory(path.parent)
    return descriptor(path, digest(raw), private=True)


def create_directory(path):
    owned(path.parent, directory=True)
    require(not os.path.lexists(path), "A new scope already exists and cannot be resumed.")
    os.mkdir(path, 0o700)
    sync_directory(path.parent)
    owned(path, directory=True, private=True)


def patch_exact(raw, replacements):
    for old, new in replacements:
        old_bytes, new_bytes = old.encode(), new.encode()
        require(raw.count(old_bytes) == 1, "A frozen helper patch does not identify exactly one reviewed literal.")
        raw = raw.replace(old_bytes, new_bytes)
    return raw


def phase_intent(phase, authority, inputs):
    return publish(EXECUTION / (phase + "-intent.json"), encoded({
        "schemaVersion": 1, "kind": "nextup-matrix07-prelaunch-phase-intent", "phase": phase,
        "createdAt": datetime.now(timezone.utc).isoformat(), "helper": authority, "inputs": inputs,
        "oneShot": True, "startCalls": 0, "businessHttpRequests": 0}))


def run_frozen(phase, source, arguments):
    read_descriptor(source, Path(source["path"]), private=True)
    argv = ["/usr/bin/python3", "-I", "-B", source["path"], *arguments]
    result = subprocess.run(argv, capture_output=True, timeout=180, check=False)
    require(len(result.stdout) <= MAX_BYTES and len(result.stderr) <= MAX_BYTES, "A helper output exceeds its bound.")
    stdout = publish(EXECUTION / (phase + "-stdout.log"), result.stdout)
    stderr = publish(EXECUTION / (phase + "-stderr.log"), result.stderr)
    invocation = publish(EXECUTION / (phase + "-invocation.json"), encoded({
        "schemaVersion": 1, "phase": phase, "argv": argv, "source": source,
        "returnCode": result.returncode, "stdout": stdout, "stderr": stderr,
        "startCalls": 0, "businessHttpRequests": 0}))
    require(result.returncode == 0, "A prelaunch helper failed; retain the consumed phase and inspect its private receipt.")
    return invocation


def setup(authority):
    for path in (OPERATOR_PARENT, ASSEMBLY_TOOL, ASSEMBLY_INPUTS, ATTESTATION_ROOT, UNIT_FILE,
                 EXECUTION / "setup.json", EXECUTION / "unit-preparation.json"):
        require(not os.path.lexists(path), "A matrix07 setup scope has already been consumed.")
    matrix_parent = WORK / "nextup-global-reference-runs-05"
    owned(matrix_parent, directory=True, private=True)
    require(not list(matrix_parent.iterdir()), "The immutable matrix output parent is no longer empty.")
    read(WORK / OPERATOR_NAME, OPERATOR_SHA)
    read(Path(INPUT_MANIFEST["path"]), INPUT_MANIFEST["sha256"], private=True)
    old_config = decode(read(OLD_CONFIG, OLD_CONFIG_SHA, private=True))
    require(old_config["inputManifest"] == INPUT_MANIFEST and old_config["planSha256"] == PLAN_SHA,
            "The retained configuration does not bind the accepted preparation05 input and plan.")
    prepare_raw = patch_exact(read(OLD_PREPARE, OLD_PREPARE_SHA), [
        ("reference-nextup-global-matrix-execution-06", EXECUTION.name),
        ("nextup-global-reference-operator-runs-06", OPERATOR_PARENT.name),
        ("goby-nextup-global-reference-matrix-06.service", UNIT),
        ("reference-nextup-global-matrix-attestation-06", ATTESTATION_ROOT.name),
        (OLD_OPERATOR_NAME, OPERATOR_NAME), (OLD_OPERATOR_SHA, OPERATOR_SHA),
        ("Description=Goby NextUp reference matrix 06", "Description=Goby NextUp reference matrix 07")])
    assembler_raw = patch_exact(read(OLD_ASSEMBLER, OLD_ASSEMBLER_SHA), [
        (OLD_OPERATOR_NAME, OPERATOR_NAME), (OLD_OPERATOR_SHA, OPERATOR_SHA)])
    intent = phase_intent("setup", authority, {
        "previousPrepare": {"path": str(OLD_PREPARE), "sha256": OLD_PREPARE_SHA},
        "previousAssembler": {"path": str(OLD_ASSEMBLER), "sha256": OLD_ASSEMBLER_SHA},
        "previousConfig": {"path": str(OLD_CONFIG), "sha256": OLD_CONFIG_SHA},
        "operator": {"path": str(WORK / OPERATOR_NAME), "sha256": OPERATOR_SHA}})
    for path in (OPERATOR_PARENT, ASSEMBLY_TOOL, ASSEMBLY_TOOL / "revision-01", ASSEMBLY_INPUTS):
        create_directory(path)
    assembler = publish(ASSEMBLY_SOURCE, assembler_raw)
    prepare = publish(EXECUTION / "prepare-matrix-unit-07.py", prepare_raw)
    invocation = run_frozen("setup", prepare, [])
    prep_descriptor = descriptor(EXECUTION / "unit-preparation.json", private=True)
    prep = decode(read_descriptor(prep_descriptor, EXECUTION / "unit-preparation.json"))
    require(prep["unitName"] == UNIT and prep["notStarted"] is True and prep["placeholderHash"] is True and
            prep["operatorSource"] == {"path": str(WORK / OPERATOR_NAME), "sha256": OPERATOR_SHA} and prep["startCalls"] == 0,
            "The new unit preparation does not bind the reviewed operator or inactive state.")
    read_descriptor(prep["unitFile"], UNIT_FILE)
    old_config["scope"] = {"attestationRoot": str(ATTESTATION_ROOT), "operatorEvidenceRoot": str(OPERATOR_PARENT / "operator-07")}
    old_config["runtime"] = {"unitName": UNIT, "properties": prep["runtimeProperties"]}
    config = publish(ASSEMBLY_INPUTS / "input.json", encoded(old_config))
    record = publish(EXECUTION / "setup.json", encoded({
        "schemaVersion": 1, "kind": "nextup-matrix07-prelaunch-setup", "intent": intent, "helper": authority,
        "assembler": assembler, "prepareSource": prepare, "config": config, "unitPreparation": prep_descriptor,
        "unitFile": prep["unitFile"], "invocation": invocation, "notStarted": True, "startCalls": 0,
        "businessHttpRequests": 0, "consumedScopesReplayed": False}))
    return {"record": record, "unitPreparation": prep_descriptor, "config": config, "assembler": assembler, "notStarted": True}


def setup_record(value, authority):
    row = {"path": str(EXECUTION / "setup.json"), "sha256": checksum(value)}
    record = decode(read_descriptor(row, EXECUTION / "setup.json"))
    require(record["kind"] == "nextup-matrix07-prelaunch-setup" and record["helper"] == authority and
            record["notStarted"] is True and record["startCalls"] == 0, "The setup authority differs.")
    read_descriptor(record["assembler"], ASSEMBLY_SOURCE)
    read_descriptor(record["config"], ASSEMBLY_INPUTS / "input.json")
    read_descriptor(record["unitPreparation"], EXECUTION / "unit-preparation.json")
    return row, record


def assemble(authority, setup_sha):
    setup_descriptor, prepared = setup_record(setup_sha, authority)
    require(not os.path.lexists(ATTESTATION_ROOT) and not os.path.lexists(EXECUTION / "assemble.json"),
            "The matrix07 assembly scope has already been consumed.")
    read_descriptor(prepared["unitFile"], UNIT_FILE)
    intent = phase_intent("assemble", authority, {"setup": setup_descriptor})
    invocation = run_frozen("assemble", prepared["assembler"], [
        "--config", prepared["config"]["path"], "--config-sha256", prepared["config"]["sha256"],
        "--source-sha256", prepared["assembler"]["sha256"]])
    assembly = descriptor(ATTESTATION_ROOT / "assembly.json", private=True)
    assembled = decode(read_descriptor(assembly, ATTESTATION_ROOT / "assembly.json"))
    require(assembled["status"] == "assembled_pending_operator_admission" and assembled["assembler"] == prepared["assembler"] and
            assembled["config"] == prepared["config"] and assembled["requestCount"] == 269 and
            assembled["producerReplayPerformed"] is False and assembled["matrixHttpPerformed"] is False,
            "The new assembly receipt differs from the frozen prelaunch contract.")
    attestation = assembled["attestation"]
    current = decode(read_descriptor(attestation, ATTESTATION_ROOT / "attestation.json"))
    prep = decode(read_descriptor(prepared["unitPreparation"], EXECUTION / "unit-preparation.json"))
    require(current["sources"]["operator"] == {"path": str(WORK / OPERATOR_NAME), "sha256": OPERATOR_SHA} and
            current["runtime"] == {"unitName": UNIT, "properties": prep["runtimeProperties"]},
            "The actual assembly has another operator or runtime authority.")
    record = publish(EXECUTION / "assemble.json", encoded({
        "schemaVersion": 1, "kind": "nextup-matrix07-prelaunch-assembly", "intent": intent,
        "helper": authority, "setup": setup_descriptor, "assembly": assembly, "attestation": attestation,
        "invocation": invocation, "notStarted": True, "startCalls": 0, "businessHttpRequests": 0}))
    return {"record": record, "assembly": assembly, "attestation": attestation, "notStarted": True}


def finalize(authority, setup_sha, assembly_sha):
    setup_descriptor, prepared = setup_record(setup_sha, authority)
    assembled_descriptor = {"path": str(EXECUTION / "assemble.json"), "sha256": checksum(assembly_sha)}
    assembled = decode(read_descriptor(assembled_descriptor, EXECUTION / "assemble.json"))
    require(assembled["kind"] == "nextup-matrix07-prelaunch-assembly" and assembled["helper"] == authority and
            assembled["setup"] == setup_descriptor and assembled["notStarted"] is True and assembled["startCalls"] == 0,
            "The assembly authority differs from this one-shot setup.")
    read_descriptor(assembled["assembly"], ATTESTATION_ROOT / "assembly.json")
    read_descriptor(assembled["attestation"], ATTESTATION_ROOT / "attestation.json")
    read_descriptor(prepared["unitFile"], UNIT_FILE)
    for path in (EXECUTION / "unit-finalization-intent.json", EXECUTION / "unit-finalized.json", EXECUTION / "finalize.json"):
        require(not os.path.lexists(path), "The matrix07 finalization has already been consumed.")
    finalizer_raw = patch_exact(read(OLD_FINALIZE, OLD_FINALIZE_SHA), [
        ("reference-nextup-global-matrix-execution-06", EXECUTION.name),
        ("reference-nextup-global-matrix-attestation-06", ATTESTATION_ROOT.name),
        ("goby-nextup-global-reference-matrix-06.service", UNIT),
        ("c99e8fadd3f1cb46a552b210e6f68ae0d4b1e1aaa8590732bd0a99ad2bcc9446", prepared["unitPreparation"]["sha256"]),
        ("49cec4512dee90658dc8de07c9e1abb04b1701c06a28d38f4fb6268a6b3828d9", prepared["unitFile"]["sha256"]),
        ("a16f833b942cbd0f7020a9eb2350ec86b7b7ffa3dac57831f0763799c6317de7", assembled["attestation"]["sha256"]),
        ("'.next-06'", "'.next-07'")])
    intent = phase_intent("finalize", authority, {"setup": setup_descriptor, "assembled": assembled_descriptor,
        "previousFinalizer": {"path": str(OLD_FINALIZE), "sha256": OLD_FINALIZE_SHA}})
    finalizer = publish(EXECUTION / "finalize-matrix-unit-07.py", finalizer_raw)
    invocation = run_frozen("finalize", finalizer, [])
    finalized = descriptor(EXECUTION / "unit-finalized.json", private=True)
    final = decode(read_descriptor(finalized, EXECUTION / "unit-finalized.json"))
    require(final["unitName"] == UNIT and final["attestation"] == assembled["attestation"] and
            final["notStarted"] is True and final["placeholderHash"] is False and final["startCalls"] == 0,
            "The final unit is not bound to this inactive attested execution.")
    read_descriptor(final["unitFile"], UNIT_FILE)
    record = publish(EXECUTION / "finalize.json", encoded({
        "schemaVersion": 1, "kind": "nextup-matrix07-prelaunch-finalization", "intent": intent, "helper": authority,
        "setup": setup_descriptor, "assembled": assembled_descriptor, "finalizer": finalizer,
        "unitFinalized": finalized, "unitFile": final["unitFile"], "attestation": assembled["attestation"],
        "invocation": invocation, "notStarted": True, "startCalls": 0, "businessHttpRequests": 0,
        "freshPreservationAndIndependentLaunchRequired": True}))
    return {"record": record, "unitFinalized": finalized, "unitFile": final["unitFile"],
            "attestation": assembled["attestation"], "notStarted": True, "businessHttpRequests": 0}


def main():
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and
            sys.flags.isolated and sys.flags.dont_write_bytecode and not sys.flags.optimize,
            "Use authorized root SSH with /usr/bin/python3 -I -B and assertions enabled.")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=("setup", "assemble", "finalize"))
    parser.add_argument("--source-sha256", required=True)
    parser.add_argument("--setup-sha256")
    parser.add_argument("--assembly-sha256")
    args = parser.parse_args()
    owned(EXECUTION, directory=True, private=True)
    source_path = Path(__file__).absolute()
    require(source_path == EXECUTION / "resume-nextup-reference-matrix.py", "Use the exact new execution07 helper path.")
    authority = descriptor(source_path, checksum(args.source_sha256), private=True)
    require((args.setup_sha256 is not None) is (args.phase in ("assemble", "finalize")) and
            (args.assembly_sha256 is not None) is (args.phase == "finalize"), "Use only the phase's exact preceding receipt hashes.")
    if args.phase == "setup":
        result = setup(authority)
    elif args.phase == "assemble":
        result = assemble(authority, args.setup_sha256)
    else:
        result = finalize(authority, args.setup_sha256, args.assembly_sha256)
    print(json.dumps(result, sort_keys=True, allow_nan=False))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"status": "matrix07_prelaunch_phase_failed", "errorType": type(error).__name__,
                          "resumeAllowed": False, "startCalls": 0}), file=sys.stderr)
        raise SystemExit(2)
