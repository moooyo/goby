#!/usr/bin/env python3
"""Contract tests for the separately built, pinned PCM helper. No media tools."""

import argparse
import array
import json
import math
import subprocess
import sys
import unittest


RATE = 11025
STEP = 1365
DELAY = 28666
FIRST_END = STEP + DELAY
MAX_SAMPLES = RATE * 600
MAX_BYTES = MAX_SAMPLES * 2
MAX_OUTPUT = 65536
PCM_ARGS = ["--sample-rate", str(RATE), "--channels", "1"]
COMMON = {
    "protocol_version": 1,
    "chromaprint_version": "1.6.1",
    "chromaprint_revision": "aed8eba2202dd9d7b3b0a56c77904cc805490d72",
    "algorithm": 1,
    "sample_rate": RATE,
    "channels": 1,
    "item_duration_samples": STEP,
    "delay_samples": DELAY,
    "first_item_end_sample": FIRST_END,
    "max_input_samples": MAX_SAMPLES,
    "max_input_bytes": MAX_BYTES,
    "max_output_bytes": MAX_OUTPUT,
}


def no_duplicate_keys(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise AssertionError("duplicate JSON key")
        result[key] = value
    return result


def signal_pcm(seconds=12):
    samples = array.array("h")
    state = 1729
    for index in range(seconds * RATE):
        state = (state * 1664525 + 1013904223) & 0xFFFFFFFF
        noise = ((state >> 16) - 32768) / 32768
        time = index / RATE
        tone = math.sin(2 * math.pi * (130 * time + 35 * time * time))
        overtone = math.sin(2 * math.pi * (430 * time + 3 * time * time))
        samples.append(round(11000 * tone + 7000 * overtone + 2500 * noise))
    if sys.byteorder != "little":
        samples.byteswap()
    return samples.tobytes()


class ProtocolTests(unittest.TestCase):
    helper = None

    def invoke(self, data=b"", args=None):
        return subprocess.run(
            [self.helper] + (PCM_ARGS if args is None else args), input=data,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, check=False)

    def document(self, result, mode, samples=None):
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, b"")
        self.assertLessEqual(len(result.stdout), MAX_OUTPUT)
        self.assertTrue(result.stdout.endswith(b"\n"))
        self.assertEqual(result.stdout.count(b"\n"), 1)
        value = json.loads(result.stdout, object_pairs_hook=no_duplicate_keys)
        keys = set(COMMON) | {"mode"}
        if mode == "fingerprint":
            keys |= {"input_samples", "input_bytes", "raw_count", "raw"}
        self.assertEqual(set(value), keys)
        self.assertEqual(value["mode"], mode)
        for key, expected in COMMON.items():
            self.assertEqual(value[key], expected, key)
            if isinstance(expected, int):
                self.assertIs(type(value[key]), int)
        if mode == "fingerprint":
            count = max(0, (samples - DELAY) // STEP)
            self.assertEqual(value["input_samples"], samples)
            self.assertEqual(value["input_bytes"], samples * 2)
            self.assertEqual(value["raw_count"], count)
            self.assertEqual(len(value["raw"]), count)
            for item in value["raw"]:
                self.assertIs(type(item), int)
                self.assertGreaterEqual(item, 0)
                self.assertLessEqual(item, 0xFFFFFFFF)
        return value

    def test_describe_does_not_require_stdin_eof(self):
        process = subprocess.Popen([self.helper, "--describe"], stdin=subprocess.PIPE,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            process.wait(timeout=10)  # Stdin remains open for this complete wait.
            stdout, stderr = process.communicate(timeout=10)
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate()
        self.document(subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr), "describe")

    def test_rejects_arguments_without_echoing_paths(self):
        for args in [[], ["/private/media/input.mkv"], ["--describe", "extra"],
                     ["--sample-rate", "48000", "--channels", "1"],
                     ["--sample-rate", "11025", "--channels", "2"],
                     ["--sample-rate", "11025", "--sample-rate", "11025"],
                     PCM_ARGS + ["/private/media/input.mkv"]]:
            with self.subTest(args=args):
                result = self.invoke(args=args)
                self.assertEqual(result.returncode, 2)
                self.assertEqual(result.stdout, b"")
                self.assertEqual(result.stderr, b"goby-intro-fingerprint: invalid_arguments\n")

    def test_empty_and_partial_sample_rejected(self):
        for payload, message in [(b"", b"empty_input"), (b"\x00", b"unaligned_pcm_eof"),
                                 (b"\x00" * (FIRST_END * 2 + 1), b"unaligned_pcm_eof")]:
            with self.subTest(length=len(payload)):
                result = self.invoke(payload)
                self.assertEqual(result.returncode, 3)
                self.assertEqual(result.stdout, b"")
                self.assertEqual(result.stderr, b"goby-intro-fingerprint: " + message + b"\n")

    def test_exact_first_window_and_stride_without_silence_trim(self):
        for samples in [1, FIRST_END - 1, FIRST_END, FIRST_END + STEP - 1, FIRST_END + STEP]:
            with self.subTest(samples=samples):
                self.document(self.invoke(b"\x00\x00" * samples), "fingerprint", samples)

    def test_little_endian_pcm_and_odd_write_chunking_are_stable(self):
        payload = signal_pcm()
        expected = self.document(self.invoke(payload), "fingerprint", len(payload) // 2)
        self.assertGreater(len(set(expected["raw"])), 10)
        process = subprocess.Popen([self.helper] + PCM_ARGS[2:] + PCM_ARGS[:2],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        try:
            cursor = 0
            sizes = [1, 3, 4097, 8191, 257]
            while cursor < len(payload):
                size = sizes[(cursor // 3) % len(sizes)]
                process.stdin.write(payload[cursor:cursor + size])
                process.stdin.flush()
                cursor += size
            process.stdin.close()
            process.stdin = None
            stdout, stderr = process.communicate(timeout=60)
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate()
        actual = self.document(subprocess.CompletedProcess(process.args, process.returncode, stdout, stderr),
                               "fingerprint", len(payload) // 2)
        self.assertEqual(actual, expected)

    def test_whole_stride_input_offset_maps_to_raw_index(self):
        payload = signal_pcm(16)
        original = self.document(self.invoke(payload), "fingerprint", len(payload) // 2)
        offset = 17
        cropped = payload[offset * STEP * 2:]
        shifted = self.document(self.invoke(cropped), "fingerprint", len(cropped) // 2)
        self.assertEqual(shifted["raw"], original["raw"][offset:])

    def test_exact_maximum_and_single_byte_sentinel(self):
        payload = b"\x00\x00" * MAX_SAMPLES
        document = self.document(self.invoke(payload), "fingerprint", MAX_SAMPLES)
        self.assertEqual(document["raw_count"], 4825)
        result = self.invoke(payload + b"\x01")
        self.assertEqual(result.returncode, 3)
        self.assertEqual(result.stdout, b"")
        self.assertEqual(result.stderr, b"goby-intro-fingerprint: input_budget_exceeded\n")


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--helper", required=True, help="Absolute path to the separately built helper")
    options, remaining = parser.parse_known_args()
    ProtocolTests.helper = options.helper
    unittest.main(argv=[sys.argv[0]] + remaining)
