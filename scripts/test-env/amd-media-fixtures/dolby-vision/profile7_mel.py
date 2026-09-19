#!/usr/bin/env python3
"""Build and inspect an original single-track, dual-layer MEL fixture on Linux."""

from __future__ import annotations

import argparse
import array
from fractions import Fraction
import hashlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import sys

from generate import (
    COLOR_OPTIONS, FPS, FRAME_SIGNAL, HEIGHT, SOURCE_REVISION, WIDTH,
    inject_rpus, split_annex_b, write_json,
)


FRAMES = 96
EL_WIDTH, EL_HEIGHT = WIDTH // 2, HEIGHT // 2
EL_PREFIX = b"\x7e\x01"
RPU_HEADER = b"\x7c\x01"
BASE_NAME = "base-pq-bt2020.hevc"
RPU_NAME = "profile7-mel-metadata-only-rpu.bin"


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def nal_type(unit: bytes) -> int:
    if len(unit) < 2 or unit[0] & 0x80 or not unit[1] & 7:
        raise ValueError("Invalid HEVC NAL header")
    return unit[0] >> 1 & 63


def access_units(path: Path) -> list[list[bytes]]:
    """Read this fixture's one-AUD, one-slice, no-B-frame access units only."""
    frames: list[list[bytes]] = []
    for unit in split_annex_b(path.read_bytes()):
        kind = nal_type(unit)
        if kind == 35:
            frames.append([])
        if not frames:
            raise ValueError("Expected an AUD before the first fixture NAL")
        frames[-1].append(unit)
    if len(frames) != FRAMES:
        raise ValueError(f"Expected {FRAMES} access units in {path.name}")
    for units in frames:
        slices = [unit for unit in units if nal_type(unit) < 32]
        if len(slices) != 1 or len(slices[0]) < 3 or not slices[0][2] & 0x80:
            raise ValueError("Expected one complete HEVC slice per fixture access unit")
        if any(nal_type(unit) in (36, 37) for unit in units):
            raise ValueError("EOS/EOB NALs are outside this fixture layout")
    return frames


def check_multiplexed(base: Path, enhancement: Path, rpu: Path, muxed: Path) -> dict:
    """Check the exact wrapping implemented by pinned dovi_tool's ElHandler."""
    base_frames = access_units(base)
    el_frames = access_units(enhancement)
    muxed_frames = access_units(muxed)
    rpus = split_annex_b(rpu.read_bytes())
    if len(rpus) != FRAMES or any(unit[0] != 0x19 for unit in rpus):
        raise ValueError("Expected one serialized RPU payload per frame")
    counts = []
    for index, (bl, el, combined, metadata) in enumerate(zip(
            base_frames, el_frames, muxed_frames, rpus, strict=True)):
        if any(nal_type(unit) in (62, 63) for unit in bl + el):
            raise ValueError("Unwrapped source layers must not contain DV NALs")
        # Official muxer.rs prefixes every EL NAL, including its AUD and parameter
        # sets, with 7e01. Its RPU NAL remains separate and is never wrapped.
        expected = bl + [EL_PREFIX + unit for unit in el] + [RPU_HEADER + metadata]
        if combined != expected:
            raise ValueError(f"Official BL/EL/RPU payload layout differs at frame {index}")
        bl_slice = next(nal_type(unit) for unit in bl if nal_type(unit) < 32)
        el_slice = next(nal_type(unit) for unit in el if nal_type(unit) < 32)
        if bl_slice != el_slice:
            raise ValueError(f"BL/EL random-access sequence differs at frame {index}")
        counts.append({"frame": index, "bl_vcl": 1, "el_vcl": 1,
                       "wrapped_el_nals": len(el), "rpu_nals": 1,
                       "vcl_nal_type": bl_slice})
    return {"frames": counts, "layout": "BL NALs, UNSPEC63(EL NALs), UNSPEC62(RPU)"}


def require_mel_export(path: Path) -> None:
    values = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(values, list) or len(values) != FRAMES:
        raise ValueError("The official parser must export all 96 RPUs")
    expected_nlq = {
        "nlq_offset": [0, 0, 0], "vdr_in_max_int": [1, 1, 1],
        "vdr_in_max": [0, 0, 0], "linear_deadzone_slope_int": [0, 0, 0],
        "linear_deadzone_slope": [0, 0, 0],
        "linear_deadzone_threshold_int": [0, 0, 0],
        "linear_deadzone_threshold": [0, 0, 0],
    }
    for index, rpu in enumerate(values):
        header = rpu.get("header", {})
        mapping = rpu.get("rpu_data_mapping", {})
        expected_header = {"vdr_rpu_profile": 1, "el_spatial_resampling_filter_flag": True,
                           "disable_residual_flag": False, "vdr_bit_depth_minus8": 4,
                           "bl_bit_depth_minus8": 2, "el_bit_depth_minus8": 2,
                           "use_prev_vdr_rpu_flag": False}
        if rpu.get("dovi_profile") != 7 or rpu.get("el_type") != "MEL":
            raise ValueError(f"RPU {index} is not parsed as Profile 7 MEL")
        if any(header.get(key) != value for key, value in expected_header.items()):
            raise ValueError(f"RPU {index} has unexpected Profile 7 header values")
        if mapping.get("nlq") != expected_nlq:
            raise ValueError(f"RPU {index} does not have zero residual on all three components")


def add_profile7_config(source: Path, destination: Path) -> None:
    """Append truthful P7 configuration after dual-layer elementary validation."""
    data = source.read_bytes()
    count = 0
    # FFmpeg dovi_isom.c: version 1.0, profile 7, level 1, RPU/EL/BL present,
    # HDR10 compatibility ID 6, no metadata compression. Reserved bits are zero.
    payload = bytes((1, 0)) + struct.pack(">H", (7 << 9) | (1 << 3) | 7)
    config = struct.pack(">I4s", 32, b"dvcC") + payload + bytes((6 << 4,)) + bytes(19)

    def rewrite(content: bytes, prefix: int = 0) -> bytes:
        nonlocal count
        result = bytearray(content[:prefix])
        offset = prefix
        while offset < len(content):
            if len(content) - offset < 8:
                raise ValueError("Truncated MP4 box")
            size, kind = struct.unpack_from(">I4s", content, offset)
            if size < 8 or offset + size > len(content):
                raise ValueError("Expected explicit, small MP4 boxes")
            body = content[offset + 8:offset + size]
            if kind in (b"moov", b"trak", b"mdia", b"minf", b"stbl"):
                body = rewrite(body)
            elif kind == b"stsd":
                if len(body) < 8 or struct.unpack_from(">I", body, 4)[0] != 1:
                    raise ValueError("Expected one MP4 sample entry")
                body = rewrite(body, 8)
            elif kind == b"hev1":
                body = rewrite(body, 78) + config
                count += 1
            elif kind in (b"dvcC", b"dvvC", b"dvwC", b"hvc1"):
                raise ValueError("Unexpected preexisting DV configuration or sample entry")
            result.extend(struct.pack(">I4s", len(body) + 8, kind) + body)
            offset += size
        return bytes(result)

    offset, media_end, moov = 0, None, None
    while offset < len(data):
        if len(data) - offset < 8:
            raise ValueError("Truncated MP4 top-level box")
        size, kind = struct.unpack_from(">I4s", data, offset)
        if size < 8 or offset + size > len(data) or kind == b"moof":
            raise ValueError("Expected a small nonfragmented MP4")
        if kind == b"mdat":
            if media_end is not None:
                raise ValueError("Expected a single MP4 media data box")
            media_end = offset + size
        if kind == b"moov":
            if moov is not None:
                raise ValueError("Expected one MP4 movie box")
            moov = (offset, size)
        offset += size
    if moov is None or media_end is None or moov[0] < media_end or sum(moov) != len(data):
        raise ValueError("The final moov must follow mdat so sample offsets do not change")
    rewritten = rewrite(data[moov[0]:])
    if count != 1:
        raise ValueError("Expected exactly one HEVC video sample entry")
    destination.write_bytes(data[:moov[0]] + rewritten)


def frame_hash_timeline(path: Path, width: int, height: int) -> list[list[str]]:
    lines = path.read_text(encoding="utf-8").splitlines()
    if "#tb 0: 1/24" not in lines:
        raise ValueError(f"Unexpected decoded frame timebase in {path.name}")
    rows = [[field.strip() for field in line.split(",")]
            for line in lines if line and not line.startswith("#")]
    if len(rows) != FRAMES:
        raise ValueError(f"Expected 96 fully decoded frames in {path.name}")
    expected_size = width * height * 3
    for index, row in enumerate(rows):
        if len(row) != 6 or [int(value) for value in row[:5]] != [0, index, index, 1, expected_size]:
            raise ValueError(f"Unexpected decoded frame size/timing in {path.name}")
    return rows


def check_mp4_samples(path: Path, probe: dict, rpu: Path) -> None:
    streams, packets = probe.get("streams", []), probe.get("packets", [])
    if len(streams) != 1 or len(packets) != FRAMES:
        raise ValueError("The fixture must contain exactly one video track and 96 samples")
    stream = streams[0]
    if stream.get("codec_type") != "video" or stream.get("has_b_frames") != 0:
        raise ValueError("Unexpected track or frame reordering")
    config = [item for item in stream.get("side_data_list", [])
              if item.get("side_data_type") == "DOVI configuration record"]
    expected = {"dv_profile": 7, "dv_level": 1, "rpu_present_flag": 1,
                "el_present_flag": 1, "bl_present_flag": 1,
                "dv_bl_signal_compatibility_id": 6}
    if len(config) != 1 or any(config[0].get(key) != value for key, value in expected.items()):
        raise ValueError("FFprobe did not recover the expected dual-layer P7 configuration")
    timebase = Fraction(stream["time_base"])
    data, rpus = path.read_bytes(), split_annex_b(rpu.read_bytes())
    for index, packet in enumerate(packets):
        if (Fraction(int(packet["pts"])) * timebase != Fraction(index, FPS)
                or packet["dts"] != packet["pts"]
                or Fraction(int(packet["duration"])) * timebase != Fraction(1, FPS)):
            raise ValueError(f"MP4 sample {index} is not aligned to the source frame clock")
        start, length = int(packet["pos"]), int(packet["size"])
        sample = data[start:start + length]
        units, cursor = [], 0
        while cursor < len(sample):
            if len(sample) - cursor < 4:
                raise ValueError("Truncated MP4 NAL length")
            size = struct.unpack_from(">I", sample, cursor)[0]
            if size < 2 or cursor + 4 + size > len(sample):
                raise ValueError("Invalid MP4 NAL length")
            units.append(sample[cursor + 4:cursor + 4 + size])
            cursor += 4 + size
        el = [unit[2:] for unit in units if nal_type(unit) == 63 and unit[:2] == EL_PREFIX]
        actual_rpu = [unit for unit in units if nal_type(unit) == 62]
        if (sum(nal_type(unit) < 32 for unit in units) != 1
                or sum(nal_type(unit) < 32 for unit in el) != 1
                or actual_rpu != [RPU_HEADER + rpus[index]]):
            raise ValueError(f"MP4 sample {index} lacks its complete BL/EL/RPU triple")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures", type=Path, required=True, help="Existing generate.py output")
    parser.add_argument("--dovi-source", type=Path, required=True, help="Pristine pinned dovi_tool checkout")
    parser.add_argument("--output", type=Path, required=True, help="New artifact directory")
    parser.add_argument("--ffmpeg", default=os.environ.get("GOBY_FFMPEG", "ffmpeg"))
    parser.add_argument("--ffprobe", default=os.environ.get("GOBY_FFPROBE", "ffprobe"))
    parser.add_argument("--cargo", default="cargo")
    parser.add_argument("--git", default="git")
    args = parser.parse_args()
    if sys.platform != "linux":
        parser.error("Run generation and verification only on an authorized Linux worker")
    fixtures, checkout, output = args.fixtures.resolve(), args.dovi_source.resolve(), args.output.resolve()
    if output.is_relative_to(fixtures) or output.is_relative_to(checkout):
        parser.error("Keep new artifacts outside both input directories")
    output.mkdir(parents=True, exist_ok=False)
    commands = []

    def run(command: list[str], log: str, timeout: int = 180) -> str:
        record = {"argv": command, "log": log, "returncode": None}
        commands.append(record)
        write_json(output / "commands.json", commands)
        result = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
        record["returncode"] = result.returncode
        write_json(output / "commands.json", commands)
        (output / log).write_text(result.stdout + result.stderr, encoding="utf-8")
        if result.returncode:
            raise RuntimeError(f"Command failed with status {result.returncode}; see {output / log}")
        return result.stdout

    manifest = json.loads((fixtures / "manifest.json").read_text(encoding="utf-8"))
    expected_source = {"source_revision": SOURCE_REVISION, "width": WIDTH,
                       "height": HEIGHT, "fps": FPS, "frames": FRAMES}
    if any(manifest.get(key) != value for key, value in expected_source.items()):
        raise ValueError("Input fixtures must match the pinned four-second procedural source")
    for name in (BASE_NAME, RPU_NAME):
        if sha256(fixtures / name) != manifest.get("sha256", {}).get(name):
            raise ValueError(f"Input manifest does not bind {name}")
        shutil.copy2(fixtures / name, output / name)
    shutil.copy2(fixtures / "manifest.json", output / "source-manifest.json")
    revision = run([args.git, "-C", str(checkout), "rev-parse", "HEAD"], "dovi-source-revision.txt").strip()
    if revision != SOURCE_REVISION:
        raise ValueError("The dovi_tool checkout is not at the pinned source revision")
    dirty = run([args.git, "-C", str(checkout), "status", "--porcelain", "--untracked-files=all"],
                "dovi-source-status.txt")
    if dirty.strip():
        raise ValueError("The pinned dovi_tool checkout must be pristine")
    shutil.copy2(checkout / "Cargo.lock", output / "dovi-tool-Cargo.lock")
    for tool, label in ((args.ffmpeg, "ffmpeg"), (args.ffprobe, "ffprobe")):
        run([tool, "-version"], f"{label}-version.txt")
    run([args.cargo, "--version"], "cargo-version.txt")
    run([args.cargo, "build", "--locked", "--release", "--no-default-features", "--features", "internal-font", "--jobs", "1",
         "--manifest-path", str(checkout / "Cargo.toml"), "--target-dir", str(output / "dovi-tool-target")],
        "dovi-tool-build.txt", timeout=1200)
    dovi = output / "dovi-tool-target/release/dovi_tool"
    run([str(dovi), "--version"], "dovi-tool-version.txt")
    base, rpu = output / BASE_NAME, output / RPU_NAME
    source_export = output / "source-mel-rpus.json"
    run([str(dovi), "export", str(rpu), "--data", f"all={source_export}"], "source-rpu-export.txt")
    require_mel_export(source_export)

    raw = output / "el-neutral-160x90.yuv"
    samples = array.array("H", [512] * (EL_WIDTH * EL_HEIGHT * 3 // 2))
    if sys.byteorder != "little":
        samples.byteswap()
    with raw.open("wb") as stream:
        for _ in range(FRAMES):
            stream.write(samples.tobytes())
    el = output / "el-neutral-160x90.hevc"
    run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info", "-f", "rawvideo",
         "-pixel_format", "yuv420p10le", "-video_size", f"{EL_WIDTH}x{EL_HEIGHT}",
         "-framerate", str(FPS), *COLOR_OPTIONS, "-i", str(raw), "-frames:v", str(FRAMES),
         "-vf", FRAME_SIGNAL, "-an", "-c:v", "libx265", "-preset", "ultrafast", "-crf", "10",
         "-profile:v", "main10", "-pix_fmt", "yuv420p10le", *COLOR_OPTIONS,
         "-x265-params", "repeat-headers=1:aud=1:bframes=0:keyint=24:min-keyint=24:scenecut=0:pools=1:frame-threads=1",
         "-f", "hevc", str(el)], "el-encode.txt")
    el_rpu, combined = output / "el-with-mel-rpu.hevc", output / "profile7-mel-single-track.hevc"
    inject_rpus(el, rpu, el_rpu, FRAMES)
    run([str(dovi), "--start-code", "four", "mux", "--bl", str(base), "--el", str(el_rpu),
         "--no-add-aud", "--output", str(combined)], "official-mux.txt")
    write_json(output / "nal-layout.json", check_multiplexed(base, el, rpu, combined))

    unconfigured, positive = output / "profile7-mel-unconfigured.mp4", output / "profile7-mel-single-track.mp4"
    run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info", "-fflags", "+genpts",
         "-r", str(FPS), "-f", "hevc", "-i", str(combined), "-map", "0:v:0", "-c:v", "copy",
         "-tag:v", "hev1", "-movflags", "+write_colr", str(unconfigured)], "mp4-mux.txt")
    add_profile7_config(unconfigured, positive)
    probe = json.loads(run([args.ffprobe, "-v", "error", "-show_streams", "-show_packets", "-of", "json",
                            str(positive)], "profile7-mel-probe.json"))
    check_mp4_samples(positive, probe, rpu)

    # Extract from the final container, not only from the pre-container source.
    recovered = output / "mp4-recovered.hevc"
    recovered_bl, recovered_el = output / "recovered-bl.hevc", output / "recovered-el-with-rpu.hevc"
    run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", str(positive),
         "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "hevc_mp4toannexb", "-f", "hevc", str(recovered)],
        "mp4-extract.txt")
    run([str(dovi), "--start-code", "four", "demux", str(recovered),
         "--bl-out", str(recovered_bl), "--el-out", str(recovered_el)], "official-demux.txt")
    # MP4 may repeat BL parameter sets from hvcC. Every original EL NAL and RPU
    # must nevertheless survive byte-for-byte after official unwrapping.
    if split_annex_b(recovered_el.read_bytes()) != split_annex_b(el_rpu.read_bytes()):
        raise ValueError("Container round trip changed the EL or its frame-associated RPUs")
    for original, restored in zip(access_units(base), access_units(recovered_bl), strict=True):
        if ([unit for unit in original if nal_type(unit) < 32]
                != [unit for unit in restored if nal_type(unit) < 32]):
            raise ValueError("Container round trip changed a base-layer slice")
    recovered_rpu, recovered_export = output / "recovered-rpu.bin", output / "recovered-mel-rpus.json"
    run([str(dovi), "extract-rpu", str(recovered), "--rpu-out", str(recovered_rpu)], "rpu-extract.txt")
    if recovered_rpu.read_bytes() != rpu.read_bytes():
        raise ValueError("All 96 RPUs must survive the container round trip unchanged")
    run([str(dovi), "export", str(recovered_rpu), "--data", f"all={recovered_export}"], "recovered-rpu-export.txt")
    require_mel_export(recovered_export)

    decoded = {}
    for name, path, width, height in (
            ("source-bl", base, WIDTH, HEIGHT), ("source-el", el, EL_WIDTH, EL_HEIGHT),
            ("recovered-bl", recovered_bl, WIDTH, HEIGHT),
            ("recovered-el", recovered_el, EL_WIDTH, EL_HEIGHT)):
        signal = json.loads(run([args.ffprobe, "-v", "error", "-show_streams", "-of", "json", str(path)],
                                f"{name}-signal.json"))
        streams = signal.get("streams", [])
        expected = {"codec_name": "hevc", "profile": "Main 10", "pix_fmt": "yuv420p10le",
                    "width": width, "height": height, "r_frame_rate": "24/1", "has_b_frames": 0,
                    "color_range": "tv", "color_space": "bt2020nc",
                    "color_transfer": "smpte2084", "color_primaries": "bt2020"}
        if len(streams) != 1 or any(streams[0].get(key) != value for key, value in expected.items()):
            raise ValueError(f"Unexpected encoded layer properties in {name}")
        hashes = output / f"{name}-decoded.framemd5"
        run([args.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-err_detect", "explode",
             "-f", "hevc", "-i", str(path), "-map", "0:v:0", "-an", "-c:v", "rawvideo",
             "-pix_fmt", "yuv420p10le", "-fps_mode", "passthrough", "-f", "framemd5", str(hashes)],
            f"{name}-decode.txt")
        decoded[name] = frame_hash_timeline(hashes, width, height)
    if decoded["source-bl"] != decoded["recovered-bl"] or decoded["source-el"] != decoded["recovered-el"]:
        raise ValueError("Decoded layer pixels or clocks changed during mux/demux")
    if [row[1:4] for row in decoded["recovered-bl"]] != [row[1:4] for row in decoded["recovered-el"]]:
        raise ValueError("The two independently decoded layers do not share the same frame clock")
    hashes = {path.name: sha256(path) for path in sorted(output.iterdir()) if path.is_file()}
    hashes["dovi-tool-target/release/dovi_tool"] = sha256(dovi)
    write_json(output / "manifest.json", {
        "source_revision": SOURCE_REVISION, "source_release": "dovi_tool 2.3.4; dolby_vision 3.4.0",
        "source_fixture_directory": str(fixtures), "positive_fixture": positive.name,
        "frames": FRAMES, "fps": FPS, "seconds": 4,
        "base_dimensions": [WIDTH, HEIGHT], "enhancement_dimensions": [EL_WIDTH, EL_HEIGHT],
        "layout": "Single HEVC video track; real half-size Main 10 EL wrapped in UNSPEC63; one P7 MEL RPU per AU",
        "enhancement_signal": "Original constant code value 512 in Y/Cb/Cr; three-component zero NLQ suppresses residual contribution",
        "fixture_checks": {"official_mux_demux": True, "complete_bl_and_el_decode": True,
                           "synchronized_96_frames": True, "all_rpus_parsed_with_zero_nlq": True,
                           "per_mp4_sample_bl_el_rpu": True},
        "product_acceptance": False,
        "acceptance_boundary": "Synthetic transport/parser fixture; GPU execution, pixels and product Probe require separate remote acceptance; no licensed encoder certification or FEL reconstruction claim",
        "sha256": hashes,
    })
    print(positive)


if __name__ == "__main__":
    main()
