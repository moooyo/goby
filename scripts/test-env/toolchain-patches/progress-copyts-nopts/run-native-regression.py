#!/usr/bin/env python3
"""Bind both real FFmpeg progress reporters to deterministic scheduler states."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import time


# This literal is the observed FFmpeg 9.0.1 failure, not a second implementation
# of its timestamp arithmetic. None denotes the existing N/A wire representation.
OVERFLOW = "9223372036844692475"
CASES = {
    "unknown_before_origin": [None, "0", "102876667"],
    "unknown_after_origin": ["0", "102876667", None, "109916667"],
    "final_unknown": ["0", None],
    "copyts_disabled": ["10083333", None, "120000000"],
    "zero_negative_and_recovery": [None, "-21000", "0", "1", "0", None, "500000"],
}
DEFECT_POSITIONS = {
    "unknown_after_origin": 2,
    "final_unknown": 1,
    "zero_negative_and_recovery": 5,
}


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def save(path: Path, value: dict) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def phase(evidence: Path, name: str, command: list[str], timeout: int) -> None:
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
    save(evidence / (name + "-result.json"), {
        "argv": command, "exit_code": code, "timed_out": timed_out,
        "elapsed_seconds": round(time.monotonic() - start, 6),
    })
    if timed_out or code != 0:
        raise RuntimeError("progress regression phase failed: " + name)


def check_progress(path: Path, mode: str, case: str) -> dict:
    if not 0 < path.stat().st_size <= 64 * 1024:
        raise RuntimeError("missing or excessive progress output: " + case)
    blocks, block = [], {}
    for line in path.read_text(encoding="ascii").splitlines():
        key, separator, value = line.partition("=")
        if not separator or not key or key in block:
            raise RuntimeError("invalid progress record: " + case)
        block[key] = value
        if key == "progress":
            blocks.append(block)
            block = {}
    expected = list(CASES[case])
    if mode == "baseline" and case in DEFECT_POSITIONS:
        expected[DEFECT_POSITIONS[case]] = OVERFLOW
    if block or len(blocks) != len(expected):
        raise RuntimeError("incomplete progress sequence: " + mode + "/" + case)
    for index, (block, value) in enumerate(zip(blocks, expected)):
        wanted = "N/A" if value is None else value
        if block.get("out_time_us") != wanted or block.get("out_time_ms") != wanted:
            raise RuntimeError("incorrect progress timestamp: " + mode + "/" + case)
        if value is None and any(block.get(key) != "N/A" for key in ("out_time", "bitrate", "speed")):
            raise RuntimeError("unknown clock lost its N/A representation: " + case)
        if block.get("total_size") != "2880192":
            raise RuntimeError("unknown clock changed output accounting: " + case)
        if block.get("progress") != ("end" if index + 1 == len(expected) else "continue"):
            raise RuntimeError("incorrect progress lifecycle: " + case)
    reproduced = mode == "baseline" and case in DEFECT_POSITIONS
    return {"case": case, "reports": len(blocks), "reproduced_overflow": reproduced,
            "progress_sha256": digest(path), "result": "DEFECT_REPRODUCED" if reproduced else "PASS"}


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
    if options.baseline == options.candidate:
        raise ValueError("the baseline and candidate source trees must differ")
    options.evidence.mkdir(parents=True, exist_ok=False)
    options.binaries.mkdir(parents=True, exist_ok=False)
    options.evidence = options.evidence.resolve()
    options.binaries = options.binaries.resolve()
    inputs = {"harness/" + name: harness / name for name in
              ("native-regression.c", "native-regression.mk", "run-native-regression.py")}
    for mode in ("baseline", "candidate"):
        for name in ("fftools/ffmpeg.c", "fftools/ffmpeg.h"):
            inputs[mode + "/" + name] = getattr(options, mode) / name
    for name in ("config.h", "config_components.h", "ffbuild/config.mak",
                 "libavformat/libavformat.a", "libavcodec/libavcodec.a", "libavutil/libavutil.a"):
        inputs["build/" + name] = options.build / name
    before = {name: digest(path) for name, path in inputs.items()}
    save(options.evidence / "inputs.json", before)
    if before["baseline/fftools/ffmpeg.c"] == before["candidate/fftools/ffmpeg.c"]:
        raise ValueError("the negative control must contain the unpatched reporter")
    if before["baseline/fftools/ffmpeg.h"] != before["candidate/fftools/ffmpeg.h"]:
        raise ValueError("baseline and candidate must use identical FFmpeg CLI declarations")
    contracts, binaries = [], {}
    for mode in ("baseline", "candidate"):
        output = options.binaries / (mode + "-regression")
        command = ["make", "-f", str(harness / "native-regression.mk"),
                   "FF_SOURCE=" + str(getattr(options, mode)), "FF_BUILD=" + str(options.build),
                   "HARNESS_OUT=" + str(output)]
        phase(options.evidence, "build-" + mode, command, 120)
        binaries[mode] = digest(output)
        outcomes = []
        for case in CASES:
            name = "run-" + mode + "-" + case
            progress = options.evidence / (name + ".progress")
            phase(options.evidence, name, [str(output), case, str(progress)], 10)
            if (options.evidence / (name + ".stdout")).stat().st_size or \
                    (options.evidence / (name + ".stderr")).stat().st_size:
                raise RuntimeError("unexpected harness diagnostics: " + name)
            outcomes.append(check_progress(progress, mode, case))
        contracts.append({"mode": mode, "cases": outcomes,
                          "reproduced_overflows": sum(case["reproduced_overflow"] for case in outcomes)})
    after = {name: digest(path) for name, path in inputs.items()}
    if after != before:
        raise RuntimeError("progress regression inputs changed during execution")
    save(options.evidence / "receipt.json", {
        "accepted": True, "contracts": contracts, "binary_sha256": binaries,
        "inputs_sha256": before, "inputs_unchanged": True,
        "negative_control_arithmetic": "explicit -fwrapv; production reporter included unchanged",
    })


if __name__ == "__main__":
    main()
