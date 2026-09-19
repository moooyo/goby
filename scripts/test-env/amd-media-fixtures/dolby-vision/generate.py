#!/usr/bin/env python3
"""Generate original synthetic Dolby Vision media on an authorized Linux worker."""

from __future__ import annotations

import argparse
import array
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys


WIDTH, HEIGHT, FPS = 320, 180, 24
SOURCE_REVISION = "614c816b6446dcd1dbaf433403d499a6026fbb5a"
START_CODE = b"\x00\x00\x00\x01"
START_CODES = re.compile(b"\x00\x00\x00?\x01")
COLOR_OPTIONS = [
    "-color_range", "tv", "-colorspace", "bt2020nc",
    "-color_primaries", "bt2020", "-color_trc", "smpte2084",
]
FRAME_SIGNAL = "setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc"


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, indent=2) + "\n", encoding="utf-8")


def split_annex_b(data: bytes) -> list[bytes]:
    markers = list(START_CODES.finditer(data))
    if not markers or markers[0].start() != 0:
        raise ValueError("Expected Annex B start codes")
    units = [data[marker.end():markers[i + 1].start() if i + 1 < len(markers) else len(data)]
             for i, marker in enumerate(markers)]
    if any(not unit for unit in units):
        raise ValueError("Empty Annex B unit")
    return units


def write_raw_chart(path: Path, frames: int) -> None:
    # These original code values represent a PQ grayscale chart, not sRGB pixels
    # relabeled as HDR. Cb/Cr remain neutral; no external media or fonts are used.
    levels = (64, 128, 256, 384, 448, 512, 640, 720)
    chroma = [512] * (WIDTH * HEIGHT // 2)
    with path.open("wb") as stream:
        for frame in range(frames):
            values = []
            for y in range(HEIGHT):
                for x in range(WIDTH):
                    value = levels[min(x * len(levels) // WIDTH, len(levels) - 1)]
                    if HEIGHT * 3 // 4 <= y < HEIGHT * 7 // 8:
                        value = 64 + (x * 656 // (WIDTH - 1))
                    if HEIGHT * 7 // 8 <= y and frame * 3 % WIDTH <= x < min(WIDTH, frame * 3 % WIDTH + 12):
                        value = 720
                    values.append(value)
            samples = array.array("H", values + chroma)
            if sys.byteorder != "little":
                samples.byteswap()
            stream.write(samples.tobytes())


def inject_rpus(base: Path, rpu_file: Path, destination: Path, frames: int) -> None:
    rpus = split_annex_b(rpu_file.read_bytes())
    if len(rpus) != frames or any(rpu[0] != 0x19 for rpu in rpus):
        raise ValueError("RPU.bin must have one payload per source frame")
    groups: list[list[bytes]] = []
    prefix: list[bytes] = []
    current: list[bytes] | None = None
    for unit in split_annex_b(base.read_bytes()):
        nal_type = unit[0] >> 1 & 0x3F
        if nal_type == 35:
            current = []
            groups.append(current)
        if current is None:
            prefix.append(unit)
        else:
            current.append(unit)
    if len(groups) != frames:
        raise ValueError(f"Expected {frames} access units, found {len(groups)}")
    with destination.open("wb") as stream:
        for unit in prefix:
            stream.write(START_CODE + unit)
        for units, rpu in zip(groups, rpus, strict=True):
            # The encoder uses one slice, no B frames, and one AUD per frame.
            if sum((unit[0] >> 1 & 0x3F) < 32 for unit in units) != 1:
                raise ValueError("Unexpected multi-slice or empty access unit")
            if any((unit[0] >> 1 & 0x3F) in (62, 63) for unit in units):
                raise ValueError("The original base layer must not contain Dolby Vision NALs")
            endings = [unit for unit in units if (unit[0] >> 1 & 0x3F) in (36, 37)]
            for unit in units:
                if (unit[0] >> 1 & 0x3F) in (36, 37):
                    continue
                stream.write(START_CODE + unit)
            stream.write(START_CODE + b"\x7c\x01" + rpu)
            for unit in endings:
                stream.write(START_CODE + unit)


def mp4_rpu_offsets(data: bytes) -> list[tuple[int, int]]:
    # FFmpeg's single-video, unfragmented MP4 stores the four-byte length-prefixed
    # NALs contiguously in mdat. Reject other layouts instead of guessing offsets.
    offset = 0
    result = []
    media_boxes = 0
    while offset < len(data):
        size, kind = struct.unpack_from(">I4s", data, offset)
        header = 8
        if size == 1:
            size = struct.unpack_from(">Q", data, offset + 8)[0]
            header = 16
        elif size == 0:
            size = len(data) - offset
        if size < header or offset + size > len(data):
            raise ValueError("Invalid MP4 box length")
        if kind == b"moof":
            raise ValueError("Fragmented MP4 is not supported by this fixture mutator")
        if kind == b"mdat":
            media_boxes += 1
            cursor = offset + header
            while cursor < offset + size:
                length = struct.unpack_from(">I", data, cursor)[0]
                start, end = cursor + 4, cursor + 4 + length
                if length < 2 or end > offset + size:
                    raise ValueError("Expected a single-video MP4 with four-byte NAL lengths")
                if data[start] >> 1 & 0x3F == 62:
                    result.append((start, end))
                cursor = end
        offset += size
    if media_boxes != 1:
        raise ValueError("Expected exactly one media data box")
    return result


def corrupt_crc(data: bytearray, start: int, end: int) -> None:
    # Locate the CRC in the unescaped RPU while preserving sample and box sizes.
    positions = []
    cursor = start + 2
    zeroes = 0
    while cursor < end:
        value = data[cursor]
        if zeroes == 2 and value == 3:
            zeroes = 0
            cursor += 1
            continue
        positions.append(cursor)
        zeroes = zeroes + 1 if value == 0 else 0
        cursor += 1
    if len(positions) < 8 or data[positions[-1]] != 0x80:
        raise ValueError("RPU does not end with a CRC and alignment byte")
    for position in positions[-5:-1]:
        original = data[position]
        for mask in (0x10, 0x20, 0x40, 0x80):
            replacement = original ^ mask
            if original > 3 and replacement > 3:
                data[position] = replacement
                return
    raise ValueError("No same-length CRC corruption is available")


def negative_controls(positive: Path, output: Path, frames: int) -> None:
    source = positive.read_bytes()
    offsets = mp4_rpu_offsets(source)
    if len(offsets) != frames:
        raise ValueError(f"Expected {frames} muxed RPUs, found {len(offsets)}")
    # Reserved UNSPEC61 is ignored by HEVC decoders. Reclassifying a NAL preserves
    # all sample offsets while removing its role as RPU. Do not use UNSPEC63: it
    # has a separate Dolby Vision enhancement-layer meaning.
    missing = bytearray(source)
    missing[offsets[frames // 2][0]] = 61 << 1
    (output / "profile81-missing-middle-rpu.mp4").write_bytes(missing)
    tags_only = bytearray(source)
    for start, _ in offsets:
        tags_only[start] = 61 << 1
    (output / "profile81-config-without-rpu.mp4").write_bytes(tags_only)
    broken = bytearray(source)
    corrupt_crc(broken, *offsets[frames // 2])
    (output / "profile81-corrupt-middle-crc.mp4").write_bytes(broken)


def add_incomplete_profile7_config(source: Path, destination: Path) -> None:
    """Declare intentionally incomplete P7 BL+RPU media for narrow filter checks."""
    data = source.read_bytes()
    count = 0
    # Version 1.0, profile 7, level 1, RPU/EL/BL present, compatibility 6.
    # EL is deliberately absent in the file. This is a negative/incomplete source,
    # never a conformant profile 7 positive asset or a FEL reconstruction test.
    payload = bytes((1, 0)) + struct.pack(">H", (7 << 9) | (1 << 3) | 7) + bytes((6 << 4,)) + bytes(19)
    config = struct.pack(">I4s", 32, b"dvcC") + payload

    def rewrite_boxes(content: bytes, prefix: int = 0) -> bytes:
        nonlocal count
        result = bytearray(content[:prefix])
        offset = prefix
        while offset < len(content):
            size, kind = struct.unpack_from(">I4s", content, offset)
            if size < 8 or offset + size > len(content):
                raise ValueError("Expected small, unfragmented MP4 boxes")
            body = content[offset + 8:offset + size]
            if kind in (b"moov", b"trak", b"mdia", b"minf", b"stbl"):
                body = rewrite_boxes(body)
            elif kind == b"stsd":
                body = rewrite_boxes(body, 8)
            elif kind in (b"hvc1", b"hev1"):
                body = rewrite_boxes(body, 78) + config
                count += 1
            elif kind in (b"dvcC", b"dvvC", b"dvwC"):
                raise ValueError("Unexpected existing Dolby Vision configuration")
            result.extend(struct.pack(">I4s", len(body) + 8, kind) + body)
            offset += size
        return bytes(result)

    # Only a final moov can grow without adjusting media chunk offsets.
    offset = 0
    moov = None
    media_end = None
    while offset < len(data):
        size, kind = struct.unpack_from(">I4s", data, offset)
        if size < 8 or offset + size > len(data):
            raise ValueError("Expected a small MP4 file with explicit box sizes")
        if kind == b"mdat":
            media_end = offset + size
        if kind == b"moov":
            moov = (offset, size)
        offset += size
    if moov is None or media_end is None or moov[0] < media_end or sum(moov) != len(data):
        raise ValueError("Profile 7 fixture mux must have a final moov after mdat")
    rewritten = rewrite_boxes(data[moov[0]:])
    if count != 1:
        raise ValueError("Expected exactly one HEVC sample entry")
    destination.write_bytes(data[:moov[0]] + rewritten)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="New artifact directory")
    parser.add_argument("--ffmpeg", default=os.environ.get("GOBY_FFMPEG", "ffmpeg"))
    parser.add_argument("--ffprobe", default=os.environ.get("GOBY_FFPROBE", "ffprobe"))
    parser.add_argument("--cargo", default="cargo")
    args = parser.parse_args()
    if sys.platform != "linux":
        parser.error("Run fixture generation on an authorized Linux worker")
    source = Path(__file__).resolve().parent
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    config = json.loads((source / "profile81.json").read_text(encoding="utf-8"))
    frames = config["length"]
    commands: list[list[str]] = []

    def run(command: list[str], log_name: str) -> str:
        commands.append(command)
        write_json(output / "commands.json", commands)
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        (output / log_name).write_text(result.stdout, encoding="utf-8")
        if result.returncode:
            raise RuntimeError(f"Command failed with status {result.returncode}; see {output / log_name}")
        return result.stdout

    def require_chart_signal(path: Path, log_name: str) -> None:
        # Inspect the encoded signal, not command-line intent. FFmpeg 9 copies
        # color properties from the first frame into the encoder context, so
        # output-only primary/transfer options can be overwritten by rawvideo's
        # unspecified frame properties. Both base and injected VUI must agree.
        actual = json.loads(run([
            args.ffprobe, "-v", "error", "-select_streams", "v:0",
            "-show_entries", "stream=codec_name,profile,pix_fmt,width,height,color_range,color_space,color_transfer,color_primaries",
            "-of", "json", str(path),
        ], log_name))
        streams = actual.get("streams", [])
        expected = {
            "codec_name": "hevc", "profile": "Main 10", "pix_fmt": "yuv420p10le",
            "width": WIDTH, "height": HEIGHT, "color_range": "tv",
            "color_space": "bt2020nc", "color_transfer": "smpte2084",
            "color_primaries": "bt2020",
        }
        if len(streams) != 1 or any(streams[0].get(key) != value for key, value in expected.items()):
            raise ValueError(f"Encoded chart signal does not match its authored PQ values; see {output / log_name}")

    for tool, filename in ((args.ffmpeg, "ffmpeg-version.txt"), (args.ffprobe, "ffprobe-version.txt")):
        run([tool, "-version"], filename)
    run([args.cargo, "--version"], "cargo-version.txt")
    build = output / "rpu-generator"
    build.mkdir()
    shutil.copy2(source / "Cargo.toml", build / "Cargo.toml")
    shutil.copytree(source / "src", build / "src")
    shutil.copy2(source / "profile81.json", output / "profile81.json")
    # The first generation resolves a Cargo.lock on the worker. Preserve it with
    # the artifacts; reuse it with --locked for exact subsequent dependency reuse.
    run([args.cargo, "build", "--release", "--manifest-path", str(build / "Cargo.toml"),
         "--target-dir", str(build / "target")], "cargo-build.txt")
    run([str(build / "target/release/goby-synthetic-dovi-rpu"), str(output / "profile81.json"), str(output)], "rpu-generation.txt")
    raw = output / "chart-pq-bt2020.yuv"
    write_raw_chart(raw, frames)
    base = output / "base-pq-bt2020.hevc"
    run([
        args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info", "-f", "rawvideo",
        "-pixel_format", "yuv420p10le", "-video_size", f"{WIDTH}x{HEIGHT}",
        "-framerate", str(FPS), *COLOR_OPTIONS, "-i", str(raw), "-frames:v", str(frames),
        "-vf", FRAME_SIGNAL,
        "-an", "-c:v", "libx265", "-preset", "ultrafast", "-crf", "10",
        "-profile:v", "main10", "-pix_fmt", "yuv420p10le", *COLOR_OPTIONS,
        "-x265-params", "repeat-headers=1:aud=1:bframes=0:keyint=24:min-keyint=24:scenecut=0:pools=1:frame-threads=1",
        "-f", "hevc", str(base),
    ], "base-encode.txt")
    require_chart_signal(base, "base-signal.json")
    for variant in ("profile81-identity", "profile81-nonidentity"):
        elementary = output / f"{variant}.hevc"
        inject_rpus(base, output / f"{variant}-rpu.bin", elementary, frames)
        require_chart_signal(elementary, f"{variant}-signal.json")
        run([
            args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info",
            "-fflags", "+genpts", "-r", str(FPS), "-f", "hevc", "-i", str(elementary),
            "-map", "0:v:0", "-c:v", "copy", "-bsf:v", "dovi_rpu=compression=none",
            "-tag:v", "hvc1", "-strict", "unofficial", "-movflags", "+write_colr",
            str(output / f"{variant}.mp4"),
        ], f"{variant}-mux.txt")
    positive = output / "profile81-nonidentity.mp4"
    negative_controls(positive, output, frames)
    for kind in ("mel", "residual"):
        metadata = f"profile7-{kind}-metadata-only"
        variant = f"profile7-{kind}-bl-rpu-only"
        elementary = output / f"{variant}.hevc"
        unconfigured = output / f"{variant}-unconfigured.mp4"
        inject_rpus(base, output / f"{metadata}-rpu.bin", elementary, frames)
        require_chart_signal(elementary, f"{variant}-signal.json")
        # Stream-copying preserves the generated P7 NLQ rather than rewriting it
        # as P8 through the dovi_rpu encoder. Add the deliberately incomplete
        # container declaration afterward without touching media sample bytes.
        run([
            args.ffmpeg, "-hide_banner", "-nostdin", "-v", "info",
            "-fflags", "+genpts", "-r", str(FPS), "-f", "hevc", "-i", str(elementary),
            "-map", "0:v:0", "-c:v", "copy", "-tag:v", "hvc1",
            "-movflags", "+write_colr", str(unconfigured),
        ], f"{variant}-mux.txt")
        add_incomplete_profile7_config(unconfigured, output / f"{variant}.mp4")
    hashes = {}
    for path in sorted(output.iterdir()):
        if path.is_file():
            hashes[path.name] = hashlib.sha256(path.read_bytes()).hexdigest()
    lock = build / "Cargo.lock"
    hashes["rpu-generator/Cargo.lock"] = hashlib.sha256(lock.read_bytes()).hexdigest()
    write_json(output / "manifest.json", {
        "source_revision": SOURCE_REVISION,
        "source_release": "quietvoid/dovi_tool 2.3.4; dolby_vision 3.4.0",
        "origin": "Original procedural PQ grayscale media; no downloaded audiovisual assets",
        "width": WIDTH, "height": HEIGHT, "fps": FPS, "frames": frames,
        "seconds": frames / FPS,
        "positive_fixture": positive.name,
        "rpu_luma_mapping": "f(x) = 1/16 + 3*x/4; chroma identity",
        "profile7_artifacts": "RPU metadata and deliberately incomplete BL+RPU-only MP4; no EL, no complete MEL/FEL video or reconstruction claim",
        "verification_status": "Generated only; product playback and color acceptance are separate gates",
        "sha256": hashes,
    })
    print(positive)


if __name__ == "__main__":
    main()
