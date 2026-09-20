#!/usr/bin/env python3
"""Author real text PGS and DVD subtitle fixtures without sampling video frames.

Run only in the authorized verification environment. This script requires Pillow
and a separately provisioned, SHA-256-pinned Noto Sans CJK SC Regular font. It
does not download fonts, invoke media executables, or modify existing files.

Primary font source, pinned to the Sans2.004 release commit:
https://github.com/notofonts/noto-cjk/tree/523d033d6cb47f4a80c58a35753646f5c3608a78
Font: Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Regular.otf
Git blob: dc15562470b4f842321894787a0d066879ccff8b; size: 16437364 bytes.
License: SIL Open Font License 1.1, at that commit's root LICENSE file.
https://github.com/notofonts/noto-cjk/blob/523d033d6cb47f4a80c58a35753646f5c3608a78/LICENSE
The script distributes rendered text, not font software. Preserve the license
with the separately downloaded font if redistributing that font.

Format references: FFmpeg n9.0.1 libavcodec/pgssubdec.c,
libavcodec/dvdsubdec.c, libavcodec/dvdsubenc.c; Matroska's S_VOBSUB codec mapping.
The independent encoders below emit display events and control commands. The
existing Goby rectangle PGS fixture informed the SUP segment envelope only.

Example:
  python3 bitmap-subtitle-fixtures.py --font-file /owned/NotoSansCJKsc-Regular.otf \
    --font-sha256 <verified-sha256> --output-dir /owned/new-bitmap-fixtures

The output manifest records exact packet/display timing, expected original
pixels, forced flags, and expected text. Generation is not decoder/OCR proof;
the caller must decode and recognize these artifacts through the real service.
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat
import struct
from dataclasses import dataclass

from PIL import Image, ImageDraw, ImageFont, __version__ as pillow_version
from PIL import features


FONT_COMMIT = "523d033d6cb47f4a80c58a35753646f5c3608a78"
FONT_PATH = "Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Regular.otf"
FONT_GIT_BLOB = "dc15562470b4f842321894787a0d066879ccff8b"
FONT_BYTES = 16437364
FONT_BASE = f"https://raw.githubusercontent.com/notofonts/noto-cjk/{FONT_COMMIT}"
CANVAS_WIDTH, CANVAS_HEIGHT = 720, 576
DURATION_MS = 9728
TICKS_PER_MS = 10000
FONT_SIZE = 48
PADDING = 12


@dataclass(frozen=True)
class Glyph:
    name: str
    text: str
    width: int
    height: int
    pixels: bytes
    x: int
    y: int


@dataclass(frozen=True)
class Cue:
    glyph: Glyph
    start_ms: int
    end_ms: int
    forced: bool = False


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def u16(value: int) -> bytes:
    return struct.pack(">H", value)


def pinned_font(path: Path, expected: str) -> tuple[ImageFont.FreeTypeFont, str]:
    if not path.is_absolute() or not re.fullmatch(r"[0-9a-f]{64}", expected):
        raise ValueError("font path must be absolute and its SHA-256 lowercase hex")
    with path.open("rb") as source:
        before = os.fstat(source.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size != FONT_BYTES:
            raise ValueError("font is not the pinned release's regular file")
        font_bytes = source.read(FONT_BYTES + 1)
        after = os.fstat(source.fileno())
    identity = lambda value: (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns)
    digest = sha256(font_bytes)
    git_blob = hashlib.sha1(f"blob {len(font_bytes)}\0".encode() + font_bytes).hexdigest()
    if identity(before) != identity(after) or digest != expected or git_blob != FONT_GIT_BLOB:
        raise ValueError("font identity differs from the admitted release and digest")
    return ImageFont.truetype(io.BytesIO(font_bytes), FONT_SIZE), digest


def render_glyph(font: ImageFont.FreeTypeFont, name: str, text: str, y: int) -> Glyph:
    left, top, right, bottom = font.getbbox(text)
    width, height = right - left + 2 * PADDING, bottom - top + 2 * PADDING
    if width <= 2 * PADDING or width > CANVAS_WIDTH or y + height > CANVAS_HEIGHT:
        raise ValueError("glyph dimensions exceed the fixed subtitle canvas")
    mask = Image.new("L", (width, height), 0)
    ImageDraw.Draw(mask).text((PADDING - left, PADDING - top), text, font=font, fill=255)
    # Keep the exact same binary source pixels for PGS and DVD, with no scaling
    # during either encoder. The font rasterizer version is recorded below.
    pixels = bytes(1 if value >= 128 else 0 for value in mask.tobytes())
    if not any(pixels) or all(pixels):
        raise ValueError("font did not render a visible text-shaped bitmap")
    return Glyph(name, text, width, height, pixels, (CANVAS_WIDTH - width) // 2, y)


def rgba_pixels(glyph: Glyph) -> bytes:
    return b"".join(b"\xff\xff\xff\xff" if value else b"\x00\x00\x00\x00" for value in glyph.pixels)


def png_bytes(glyph: Glyph) -> bytes:
    output = io.BytesIO()
    Image.frombytes("RGBA", (glyph.width, glyph.height), rgba_pixels(glyph)).save(output, format="PNG")
    return output.getvalue()


def pgs_rle(glyph: Glyph) -> bytes:
    output = bytearray()
    for row in range(glyph.height):
        pixels = glyph.pixels[row * glyph.width:(row + 1) * glyph.width]
        x = 0
        while x < len(pixels):
            value, end = pixels[x], x + 1
            while end < len(pixels) and pixels[end] == value and end - x < 0x3FFF:
                end += 1
            length = end - x
            if value and length == 1:
                output.append(value)
            else:
                output.append(0)
                flags = (0x80 if value else 0) | (0x40 if length >= 64 else 0)
                if length >= 64:
                    output.extend((flags | (length >> 8), length & 0xFF))
                else:
                    output.append(flags | length)
                if value:
                    output.append(value)
            x = end
        output.extend((0, 0))
    return bytes(output)


def sup_segment(time_ms: int, kind: int, payload: bytes) -> bytes:
    if len(payload) > 65535:
        raise ValueError("fixture segment requires unsupported object fragmentation")
    pts = time_ms * 90
    return b"PG" + struct.pack(">IIBH", pts, pts, kind, len(payload)) + payload


def display_events(cues: list[Cue]) -> list[tuple[int, list[Cue]]]:
    times = sorted({0, *(cue.start_ms for cue in cues), *(cue.end_ms for cue in cues)})
    return [(time, [cue for cue in cues if cue.start_ms <= time < cue.end_ms]) for time in times]


def encode_sup(cues: list[Cue]) -> bytes:
    output = bytearray()
    for number, (time, active) in enumerate(display_events(cues)):
        if len(active) > 2:
            raise ValueError("the PGS fixture permits at most two simultaneous objects")
        # Every display set starts an epoch with complete dependencies, so no
        # decoder may rely on stale palette/object state from an earlier set.
        presentation = bytearray(struct.pack(">HHBHBBBB", CANVAS_WIDTH, CANVAS_HEIGHT,
                                             0x20, number, 0x80, 0, 0, len(active)))
        for object_id, cue in enumerate(active, 1):
            presentation.extend(struct.pack(">HBBHH", object_id, 0, 0x40 if cue.forced else 0,
                                            cue.glyph.x, cue.glyph.y))
        output.extend(sup_segment(time, 0x16, bytes(presentation)))
        if active:
            window = b"\x01\x00" + struct.pack(">HHHH", 0, 0, CANVAS_WIDTH, CANVAS_HEIGHT)
            output.extend(sup_segment(time, 0x17, window))
            palette = bytes((0, 0, 0, 16, 128, 128, 0, 1, 235, 128, 128, 255))
            output.extend(sup_segment(time, 0x14, palette))
            for object_id, cue in enumerate(active, 1):
                encoded = pgs_rle(cue.glyph)
                header = (u16(object_id) + b"\x00\xc0" + (len(encoded) + 4).to_bytes(3, "big")
                          + u16(cue.glyph.width) + u16(cue.glyph.height))
                output.extend(sup_segment(time, 0x15, header + encoded))
        output.extend(sup_segment(time, 0x80, b""))
    return bytes(output)


def compose(cues: list[Cue], name: str) -> Glyph:
    if not cues:
        raise ValueError("cannot compose an empty display interval")
    x, y = min(c.glyph.x for c in cues), min(c.glyph.y for c in cues)
    right = max(c.glyph.x + c.glyph.width for c in cues)
    bottom = max(c.glyph.y + c.glyph.height for c in cues)
    width, height = right - x, bottom - y
    pixels = bytearray(width * height)
    for cue in cues:
        glyph = cue.glyph
        for row in range(glyph.height):
            destination = (glyph.y - y + row) * width + glyph.x - x
            source = row * glyph.width
            for column in range(glyph.width):
                pixels[destination + column] |= glyph.pixels[source + column]
    ordered = sorted(cues, key=lambda cue: (cue.glyph.y, cue.glyph.x))
    return Glyph(name, "\n".join(cue.glyph.text for cue in ordered), width, height, bytes(pixels), x, y)


def tight_crop(glyph: Glyph) -> Glyph:
    # DVD decoding trims a fully transparent outer border. Record that exact
    # visible-pixel oracle separately from the rectangle written into the SPU.
    visible = [index for index, value in enumerate(glyph.pixels) if value]
    if not visible:
        raise ValueError("cannot crop an empty subtitle glyph")
    left = min(index % glyph.width for index in visible)
    right = max(index % glyph.width for index in visible) + 1
    top = min(index // glyph.width for index in visible)
    bottom = max(index // glyph.width for index in visible) + 1
    pixels = b"".join(glyph.pixels[row * glyph.width + left:row * glyph.width + right]
                      for row in range(top, bottom))
    return Glyph(glyph.name, glyph.text, right - left, bottom - top, pixels, glyph.x + left, glyph.y + top)


def dvd_field_rle(glyph: Glyph, first_row: int) -> bytes:
    output = bytearray()
    for row in range(first_row, glyph.height, 2):
        values = glyph.pixels[row * glyph.width:(row + 1) * glyph.width]
        nibbles: list[int] = []
        x = 0
        while x < glyph.width:
            value, end = values[x], x + 1
            while end < glyph.width and values[end] == value:
                end += 1
            length = end - x
            if length >= 64 and end == glyph.width:
                # Four-nibble zero run means fill the rest of this scan line.
                nibbles.extend((0, 0, 0, value))
            else:
                length = min(length, 255)
                code = length * 4 + value
                digits = 1 if length < 4 else 2 if length < 16 else 3 if length < 64 else 4
                nibbles.extend((code >> shift) & 15 for shift in range(4 * (digits - 1), -1, -4))
            x += length
        if len(nibbles) % 2:
            nibbles.append(0)
        output.extend(nibbles[i] * 16 + nibbles[i + 1] for i in range(0, len(nibbles), 2))
    return bytes(output)


def dvd_coordinates(first: int, last: int) -> bytes:
    if first < 0 or last >= 4096 or first > last:
        raise ValueError("DVD rectangle exceeds the 12-bit coordinate space")
    return bytes((first >> 4, ((first & 15) << 4) | (last >> 8), last & 255))


def encode_spu(glyph: Glyph, duration_ms: int, forced: bool) -> bytes:
    if duration_ms <= 0 or duration_ms * 90 % 1024:
        raise ValueError("DVD fixture duration must be exact in its 90 kHz/1024 control clock")
    top, bottom = dvd_field_rle(glyph, 0), dvd_field_rle(glyph, 1)
    control = 4 + len(top) + len(bottom)
    commands = (b"\x03\x32\x10\x04\x00\xf0\x05"
                + dvd_coordinates(glyph.x, glyph.x + glyph.width - 1)
                + dvd_coordinates(glyph.y, glyph.y + glyph.height - 1)
                + b"\x06" + u16(4) + u16(4 + len(top))
                + bytes((0 if forced else 1, 255)))
    stop = control + 4 + len(commands)
    packet = (b"\x00\x00" + u16(control) + top + bottom
              + u16(0) + u16(stop) + commands
              + u16(duration_ms * 90 // 1024) + u16(stop) + b"\x02\xff")
    if len(packet) > 65535:
        raise ValueError("DVD fixture exceeds the SPU packet length field")
    return u16(len(packet)) + packet[2:]


def ebml_size(value: int) -> bytes:
    for width in range(1, 9):
        if value < (1 << (7 * width)) - 1:
            return ((1 << (7 * width)) | value).to_bytes(width, "big")
    raise ValueError("EBML size exceeds the fixture writer's bound")


def element(identifier: int, payload: bytes) -> bytes:
    return identifier.to_bytes((identifier.bit_length() + 7) // 8, "big") + ebml_size(len(payload)) + payload


def uint_element(identifier: int, value: int) -> bytes:
    return element(identifier, value.to_bytes(max(1, (value.bit_length() + 7) // 8), "big"))


DVD_PRIVATE = (f"size: {CANVAS_WIDTH}x{CANVAS_HEIGHT}\n"
               "palette: 000000, ffffff, 000000, 808080, 000000, 000000, 000000, 000000, "
               "000000, 000000, 000000, 000000, 000000, 000000, 000000, 000000\n").encode("ascii")


def encode_matroska(packets: list[tuple[int, int, bytes]], name: str,
                    codec: bytes = b"S_VOBSUB", private: bytes = DVD_PRIVATE) -> bytes:
    header = (uint_element(0x4286, 1) + uint_element(0x42F7, 1) + uint_element(0x42F2, 4)
              + uint_element(0x42F3, 8) + element(0x4282, b"matroska")
              + uint_element(0x4287, 4) + uint_element(0x4285, 2))
    info = (uint_element(0x2AD7B1, 1_000_000) + element(0x4489, struct.pack(">d", DURATION_MS))
            + element(0x4D80, b"Goby fixture author") + element(0x5741, b"Goby fixture author"))
    track = (uint_element(0xD7, 1) + uint_element(0x73C5, 1) + uint_element(0x83, 17)
             + uint_element(0x9C, 0) + element(0x22B59C, b"und")
             + element(0x536E, name.encode("ascii")) + element(0x86, codec))
    if private:
        track += element(0x63A2, private)
    cluster = bytearray(uint_element(0xE7, 0))
    for start_ms, duration_ms, packet in packets:
        block = b"\x81" + struct.pack(">hB", start_ms, 0) + packet
        cluster.extend(element(0xA0, element(0xA1, block) + uint_element(0x9B, duration_ms)))
    segment = (element(0x1549A966, info) + element(0x1654AE6B, element(0xAE, track))
               + element(0x1F43B675, bytes(cluster)))
    return element(0x1A45DFA3, header) + element(0x18538067, segment)


def pgs_matroska_packets(sup: bytes) -> list[tuple[int, int, bytes]]:
    # The Matroska PGS block contains segment type/length/payload, while the SUP
    # envelope carries PTS/DTS separately. Preserve each entire display set.
    displays: list[tuple[int, bytearray]] = []
    offset = 0
    while offset < len(sup):
        if sup[offset:offset + 2] != b"PG" or offset + 13 > len(sup):
            raise ValueError("invalid authored SUP envelope")
        pts, dts, _, size = struct.unpack(">IIBH", sup[offset + 2:offset + 13])
        if pts != dts or pts % 90 or offset + 13 + size > len(sup):
            raise ValueError("invalid authored SUP timing or length")
        if not displays or displays[-1][0] != pts // 90:
            displays.append((pts // 90, bytearray()))
        displays[-1][1].extend(sup[offset + 10:offset + 13 + size])
        offset += 13 + size
    return [(start, (displays[index + 1][0] if index + 1 < len(displays) else DURATION_MS) - start,
             bytes(payload)) for index, (start, payload) in enumerate(displays)]


def glyph_record(glyph: Glyph) -> dict:
    return {"text": glyph.text, "x": glyph.x, "y": glyph.y,
            "width": glyph.width, "height": glyph.height,
            "pixel_format": "RGBA8-straight-alpha", "rgba_sha256": sha256(rgba_pixels(glyph)),
            "binary_pixels_sha256": sha256(glyph.pixels)}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--font-file", type=Path, required=True)
    parser.add_argument("--font-sha256", required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    font, font_digest = pinned_font(args.font_file, args.font_sha256)
    if not args.output_dir.is_absolute():
        parser.error("--output-dir must be absolute and must not already exist")
    # An exclusive new directory keeps failed generations and successful prior
    # attempts intact. The manifest is written last as the completeness marker.
    args.output_dir.mkdir(mode=0o700, parents=False, exist_ok=False)
    files: dict[str, dict] = {}

    def write(name: str, data: bytes) -> None:
        with (args.output_dir / name).open("xb") as target:
            target.write(data)
        files[name] = {"bytes": len(data), "sha256": sha256(data)}

    english = render_glyph(font, "english", "Hello world", 392)
    chinese = render_glyph(font, "chinese", "中文测试", 480)
    traditional = render_glyph(font, "chinese-traditional", "中文測試", 480)
    for glyph in (english, chinese, traditional):
        write(f"{glyph.name}-source.png", png_bytes(glyph))
    write("dvd-codec-private.txt", DVD_PRIVATE)
    cases = {
        "english": [Cue(english, 1024, 4096)],
        "chinese-forced": [Cue(chinese, 2560, 5632, True)],
        "chinese-traditional": [Cue(traditional, 2560, 5632)],
        "overlap": [Cue(english, 1024, 4096), Cue(chinese, 2560, 5632), Cue(english, 6656, 8704)],
        "mixed-forced": [Cue(english, 1024, 4096), Cue(chinese, 2560, 5632, True)],
    }
    records = []
    for name, cues in cases.items():
        sup = encode_sup(cues)
        write(f"{name}.sup", sup)
        write(f"{name}-pgs.mks", encode_matroska(pgs_matroska_packets(sup), name, b"S_HDMV/PGS", b""))
        models = (["eng"] if name == "english" else ["chi_sim"] if name == "chinese-forced"
                  else ["chi_tra"] if name == "chinese-traditional" else ["eng", "chi_sim"])
        record = {"name": name, "sup_file": f"{name}.sup", "pgs_matroska_file": f"{name}-pgs.mks",
                  "pgs_container_origin_ticks": 0, "ocr_models": models, "authored_cues": [],
                  "pgs_intervals": [], "dvd_intervals": [], "clear_events_ms": []}
        for cue in cues:
            record["authored_cues"].append({"start_ticks": cue.start_ms * TICKS_PER_MS,
                                            "end_ticks": cue.end_ms * TICKS_PER_MS,
                                            "forced": cue.forced, **glyph_record(cue.glyph)})
        events = display_events(cues)
        dvd_packets = []
        for index, (start, active) in enumerate(events):
            if not active:
                record["clear_events_ms"].append(start)
                continue
            end = events[index + 1][0]
            for forced in (False, True):
                group = [cue for cue in active if cue.forced == forced]
                if group:
                    rendered = compose(group, f"{name}-{index}-{int(forced)}")
                    record["pgs_intervals"].append({"start_ticks": start * TICKS_PER_MS,
                                                   "end_ticks": end * TICKS_PER_MS,
                                                   "forced": forced, **glyph_record(rendered)})
            if name == "mixed-forced":
                # DVD has one SPU forced bit, not a flag per rectangle. Do not
                # label a fabricated mixed-forced DVD equivalent as valid.
                continue
            rendered = compose(active, f"{name}-{index}")
            forced = active[0].forced
            packet = encode_spu(rendered, end - start, forced)
            packet_name = f"{name}-dvd-{len(dvd_packets):03d}.spu"
            write(packet_name, packet)
            write(f"{name}-dvd-{len(dvd_packets):03d}-source.png", png_bytes(rendered))
            dvd_packets.append((start, end - start, packet))
            record["dvd_intervals"].append({"packet_file": packet_name,
                                           "pts_ticks": start * TICKS_PER_MS,
                                           "duration_ticks": (end - start) * TICKS_PER_MS,
                                           "start_ticks": start * TICKS_PER_MS,
                                           "end_ticks": end * TICKS_PER_MS,
                                           "forced": forced, "encoded_rectangle": glyph_record(rendered),
                                           **glyph_record(tight_crop(rendered))})
        if dvd_packets:
            write(f"{name}-dvd.mks", encode_matroska(dvd_packets, name))
            record["dvd_matroska_file"] = f"{name}-dvd.mks"
            # A subtitle-only Matroska file begins at its first SPU packet;
            # the production reader subtracts that explicit demuxer origin.
            record["dvd_container_origin_ticks"] = dvd_packets[0][0] * TICKS_PER_MS
        else:
            record["dvd_omission_reason"] = "DVD SPU cannot represent forced and optional objects in the same display interval."
        records.append(record)
    manifest = {"format": "goby-bitmap-subtitle-fixtures-v1",
                "canvas_width": CANVAS_WIDTH, "canvas_height": CANVAS_HEIGHT,
                "duration_ticks": DURATION_MS * TICKS_PER_MS,
                "font": {"source": f"{FONT_BASE}/{FONT_PATH}", "sha256": font_digest,
                         "git_blob_sha1": FONT_GIT_BLOB, "license": "SIL-OFL-1.1",
                         "license_source": f"{FONT_BASE}/LICENSE", "pixel_size": FONT_SIZE},
                "renderer": {"pillow": pillow_version, "freetype": features.version("freetype2"),
                             "threshold": 128, "resampling": "none"},
                "cases": records, "files": files,
                "verification": "Generated expected inputs only; real decoder and OCR acceptance remains required."}
    write("manifest.json", (json.dumps(manifest, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8"))
    print(json.dumps({"format": manifest["format"], "output_dir": str(args.output_dir),
                      "case_count": len(records), "manifest_sha256": files["manifest.json"]["sha256"]}))


if __name__ == "__main__":
    main()
