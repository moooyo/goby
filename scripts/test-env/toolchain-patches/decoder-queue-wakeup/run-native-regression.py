#!/usr/bin/env python3
"""Build and enforce both decoder wakeup contracts before toolchain publication."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import time


CASES = [
    ("overflow_waiting", 124),
    ("notify_before_receive", 124),
    ("ordered_existing_input", 0),
    ("queued_while_choked", 124),
    ("normal_eof_waiter", 0),
    ("stop_queue_wait", 124),
    ("stop_waiter", 0),
]
SOURCES = (
    "fftools/thread_queue.h",
    "fftools/thread_queue.c",
    "fftools/ffmpeg_sched.c",
    "libavfilter/vf_libplacebo.c",
)


def digest(path: Path) -> str:
    result = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            result.update(block)
    return result.hexdigest()


def save(path: Path, value: dict) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def phase(evidence: Path, name: str, command: list[str], timeout: int) -> dict:
    start = time.monotonic()
    timed_out = False
    with (evidence / (name + ".stdout")).open("wb") as stdout, \
            (evidence / (name + ".stderr")).open("wb") as stderr:
        process = subprocess.Popen(command, stdout=stdout, stderr=stderr, start_new_session=True)
        try:
            code = process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=3)
            except subprocess.TimeoutExpired:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
            code = process.returncode
    result = {"argv": command, "exit_code": code, "timed_out": timed_out,
              "elapsed_seconds": round(time.monotonic() - start, 6)}
    save(evidence / (name + "-result.json"), result)
    if timed_out or code != 0:
        raise RuntimeError("native regression phase failed: " + name)
    return result


def check_contract(evidence: Path, mode: str) -> dict:
    name = "run-" + mode
    if (evidence / (name + ".stderr")).stat().st_size:
        raise RuntimeError("unexpected native regression diagnostics: " + mode)
    lines = (evidence / (name + ".stdout")).read_text().splitlines()
    expected = list(CASES)
    if mode == "candidate":
        expected = [(name, 0) for name, _ in expected] + [("interrupt_eof", 0)]
    if len(lines) != len(expected) + 1:
        raise RuntimeError("incomplete native regression records: " + mode)
    for line, (case, code) in zip(lines, expected):
        label = "DEFECT_REPRODUCED" if code == 124 else "PASS"
        wanted = f"mode={mode} case={case} exit={code} expected={code} result={label}"
        if line != wanted:
            raise RuntimeError("unexpected native regression outcome: " + mode + "/" + case)
    deadlocks = sum(code == 124 for _, code in expected)
    if lines[-1] != f"contract=PASS mode={mode} cases={len(expected)} reproduced_deadlocks={deadlocks}":
        raise RuntimeError("invalid native regression completion: " + mode)
    return {"mode": mode, "cases": len(expected), "reproduced_deadlocks": deadlocks}


def main() -> None:
    parser = argparse.ArgumentParser()
    for name in ("baseline", "candidate", "build", "binaries", "evidence"):
        parser.add_argument("--" + name, required=True, type=Path)
    options = parser.parse_args()
    harness = Path(__file__).resolve().parent
    for name in ("baseline", "candidate", "build"):
        path = getattr(options, name).resolve(strict=True)
        if not path.is_dir():
            raise ValueError("expected a source/build directory: " + name)
        setattr(options, name, path)
    if len({options.baseline, options.candidate, options.build}) != 3:
        raise ValueError("baseline, candidate, and out-of-tree build must be distinct")
    options.evidence.mkdir(parents=True, exist_ok=False)
    options.binaries.mkdir(parents=True, exist_ok=False)
    options.evidence = options.evidence.resolve()
    options.binaries = options.binaries.resolve()
    inputs = {"harness/" + name: harness / name for name in
              ("native-regression.c", "native-regression.mk", "run-native-regression.py")}
    for mode in ("baseline", "candidate"):
        for name in SOURCES:
            inputs[mode + "/" + name] = getattr(options, mode) / name
    for name in ("config.h", "config_components.h", "ffbuild/config.mak", "fftools/sync_queue.o",
                 "libavcodec/libavcodec.a", "libavutil/libavutil.a"):
        inputs["build/" + name] = options.build / name
    before = {name: digest(path) for name, path in inputs.items()}
    save(options.evidence / "inputs.json", before)
    if before["baseline/libavfilter/vf_libplacebo.c"] != before["candidate/libavfilter/vf_libplacebo.c"]:
        raise ValueError("the negative control must retain the identical strict Dolby Vision patch")
    for name in SOURCES[:3]:
        if before["baseline/" + name] == before["candidate/" + name]:
            raise ValueError("baseline and candidate must differ at wakeup source: " + name)
    contracts, binaries = [], {}
    for mode in ("baseline", "candidate"):
        output = options.binaries / (mode + "-regression")
        command = ["make", "-f", str(harness / "native-regression.mk"),
                   "FF_SOURCE=" + str(getattr(options, mode)), "FF_BUILD=" + str(options.build),
                   "WAKEUP_CANDIDATE=" + ("1" if mode == "candidate" else "0"),
                   "HARNESS_OUT=" + str(output)]
        phase(options.evidence, "build-" + mode, command, 120)
        binaries[mode] = digest(output)
        phase(options.evidence, "run-" + mode, [str(output)], 30)
        contracts.append(check_contract(options.evidence, mode))
    after = {name: digest(path) for name, path in inputs.items()}
    if after != before:
        raise RuntimeError("native regression inputs changed during execution")
    save(options.evidence / "receipt.json", {"accepted": True, "contracts": contracts,
         "binary_sha256": binaries, "inputs_sha256": before, "inputs_unchanged": True})


if __name__ == "__main__":
    main()
