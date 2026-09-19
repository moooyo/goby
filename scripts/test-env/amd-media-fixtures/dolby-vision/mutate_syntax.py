#!/usr/bin/env python3
"""Create a same-length MP4 RPU with valid CRC and invalid mapping syntax."""

import argparse
import importlib.util
import json
from pathlib import Path


def unescape(data: bytes) -> bytes:
    result = bytearray()
    zeroes = 0
    for index, value in enumerate(data):
        if zeroes == 2 and value == 3:
            if index + 1 == len(data) or data[index + 1] > 3:
                raise ValueError("Invalid emulation-prevention sequence")
            zeroes = 0
            continue
        result.append(value)
        zeroes = zeroes + 1 if value == 0 else 0
    return bytes(result)


def escape(data: bytes) -> bytes:
    result = bytearray()
    zeroes = 0
    for value in data:
        if zeroes == 2 and value <= 3:
            result.append(3)
            zeroes = 0
        result.append(value)
        zeroes = zeroes + 1 if value == 0 else 0
    return bytes(result)


def crc_mpeg2(data: bytes) -> int:
    value = 0xFFFFFFFF
    for octet in data:
        value ^= octet << 24
        for _ in range(8):
            value = ((value << 1) ^ (0x04C11DB7 if value & 0x80000000 else 0)) & 0xFFFFFFFF
    return value


class Bits:
    def __init__(self, data: bytes):
        self.data, self.position = data, 0

    def read(self, count: int) -> int:
        if self.position + count > len(self.data) * 8:
            raise ValueError("Truncated RPU header")
        value = 0
        for _ in range(count):
            value = (value << 1) | ((self.data[self.position // 8] >> (7 - self.position % 8)) & 1)
            self.position += 1
        return value

    def ue(self) -> int:
        zeroes = 0
        while not self.read(1):
            zeroes += 1
            if zeroes > 31:
                raise ValueError("Oversized Exp-Golomb field")
        return (1 << zeroes) - 1 + self.read(zeroes)


def first_pivot_count(body: bytes) -> int:
    bits = Bits(body)
    if bits.read(6) != 2 or bits.read(11) & 0x700:
        raise ValueError("Expected an ordinary type-2 RPU")
    bits.read(8)
    if bits.read(1) != 1:
        raise ValueError("Sequence information is required")
    bits.read(1)
    coefficient = bits.read(2)
    if coefficient == 0:
        bits.ue()
    elif coefficient != 1:
        raise ValueError("Unknown coefficient representation")
    bits.read(3)
    bits.ue()
    bits.ue()
    bits.ue()
    bits.read(1)
    if bits.read(3) != 0:
        raise ValueError("Use an uncompressed fixture")
    bits.read(3)
    if bits.read(1) != 0:
        raise ValueError("Use a fixture without previous-mapping reuse")
    bits.ue()
    bits.ue()
    bits.ue()
    return bits.position


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--helpers", type=Path, default=Path(__file__).with_name("generate.py"))
    args = parser.parse_args()
    if args.source.resolve() == args.destination.resolve() or args.source.stat().st_size > 32 * 1024 * 1024:
        parser.error("Use a separate destination and a bounded fixture")
    spec = importlib.util.spec_from_file_location("goby_fixture_helpers", args.helpers)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    source = bytearray(args.source.read_bytes())
    offsets = module.mp4_rpu_offsets(source)
    if len(offsets) < 3:
        raise ValueError("At least three RPUs are required")
    index = len(offsets) // 2
    start, end = offsets[index]
    original = bytes(source[start:end])
    rbsp = unescape(original[2:])
    if rbsp[0] != 25 or rbsp[-1] != 0x80 or crc_mpeg2(rbsp[1:-5]) != int.from_bytes(rbsp[-5:-1], "big"):
        raise ValueError("The source must have a valid RPU CRC envelope")
    body = bytearray(rbsp[1:-5])
    pivot_bit = first_pivot_count(body)
    # ue(31) exceeds FFmpeg's maximum pivot-count-minus-two. Altering a field
    # after the complete header leaves the production header gate satisfied.
    for offset, bit in enumerate("00000100000"):
        position = pivot_bit + offset
        mask = 1 << (7 - position % 8)
        body[position // 8] = (body[position // 8] & ~mask) | (int(bit) * mask)
    salt_position = len(body) - 10
    if salt_position * 8 <= pivot_bit + 11:
        raise ValueError("Fixture has no independent trailing salt byte")
    original_salt = body[salt_position]
    for salt in range(256):
        body[salt_position] = original_salt ^ salt
        crc = crc_mpeg2(body)
        replacement = original[:2] + escape(bytes((25,)) + body + crc.to_bytes(4, "big") + bytes((0x80,)))
        if len(replacement) == len(original):
            source[start:end] = replacement
            with args.destination.open("xb") as output:
                output.write(source)
            print(json.dumps({"rpu_index": index, "rpu_count": len(offsets), "nal_length": len(original),
                              "pivot_bit": pivot_bit, "invalid_pivot_count_minus_two": 31, "crc": f"{crc:08x}"}))
            return
    raise ValueError("No same-length escaped mutation was found")


if __name__ == "__main__":
    main()
