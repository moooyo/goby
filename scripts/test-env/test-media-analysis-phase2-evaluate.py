#!/usr/bin/env python3
"""Parser contracts only; real media calibration is separate test-env evidence."""

import copy
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location(
    "media_analysis_evaluator", Path(__file__).with_name("media-analysis-phase2-evaluate.py"))
EVALUATOR = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(EVALUATOR)


class Source:
    def proc_path(self):
        return "/proc/self/fd/777"


def probe_document(stream_sar=EVALUATOR.UNREPORTED_SAR, frame_sar=EVALUATOR.UNREPORTED_SAR):
    stream = {"index": 0, "codec_type": "video", "width": 320, "height": 240,
              "time_base": "1/30", "disposition": {"attached_pic": 0}}
    if stream_sar is not EVALUATOR.UNREPORTED_SAR:
        stream["sample_aspect_ratio"] = stream_sar
    frames = [{"stream_index": 0, "pts": index, "width": 320, "height": 240}
              for index in range(3)]
    if frame_sar is not EVALUATOR.UNREPORTED_SAR:
        for frame in frames:
            frame["sample_aspect_ratio"] = frame_sar
    return {"streams": [stream], "format": {"start_time": "0.000000", "duration": "0.100000"},
            "packets": [{"stream_index": 0, "pts": index} for index in range(3)], "frames": frames}


CASE = {"source": {"video_stream_index": 0, "format_start_ticks": 0, "duration_ticks": 1_000_000},
        "preview": {"width": 240}}


class SourceSARContracts(unittest.TestCase):
    def read_probe(self, document):
        with patch.object(EVALUATOR, "run_tool", return_value=json.dumps(document).encode()) as run:
            result = EVALUATOR.probe_source(object(), Source(), CASE, None)
        entries = run.call_args.args[1]
        self.assertIn("frame=stream_index,pts,width,height,sample_aspect_ratio", entries[entries.index("-show_entries") + 1])
        return result

    def test_unknown_source_is_retained_and_display_fallback_is_explicit(self):
        for report in (EVALUATOR.UNREPORTED_SAR, "N/A", "0:1"):
            with self.subTest(report="absent" if report is EVALUATOR.UNREPORTED_SAR else report):
                document = probe_document(report, report)
                document["streams"][0]["display_aspect_ratio"] = "16:9"
                before = copy.deepcopy(document)
                result = self.read_probe(document)
                self.assertEqual(document, before)
                self.assertTrue(result["source_sar_unknown"])
                self.assertEqual(result["geometry_profile"], "orthogonal-display-sar-v2")
                self.assertEqual(result["display_sar_policy"], "unknown_source_square_pixel_display")
                self.assertEqual(result["display_sar"], "1/1")
                self.assertEqual(result["height"], 180)
                self.assertEqual(sum(result["source_frame_sar_reports"].values()), 3)
                self.assertEqual(result["source_sar_report"], "absent" if report is EVALUATOR.UNREPORTED_SAR else report)

    def test_known_ratios_remain_strict_and_rotate_exactly(self):
        result = self.read_probe(probe_document("2:1", "2:1"))
        self.assertFalse(result["source_sar_unknown"])
        self.assertEqual((result["display_sar"], result["height"]), ("2/1", 90))
        equivalent = self.read_probe(probe_document("4:3", "8:6"))
        self.assertEqual((equivalent["display_sar"], equivalent["height"]), ("4/3", 135))
        document = probe_document("2:1", "2:1")
        document["streams"][0]["side_data_list"] = [{"side_data_type": "Display Matrix", "rotation": -90,
            "displaymatrix": "00000000: 0 65536 0\n00000001: -65536 0 0\n00000002: 0 0 1073741824"}]
        result = self.read_probe(document)
        self.assertEqual((result["filters"], result["display_sar"], result["height"]), ("transpose=clock,", "1/2", 640))
        unknown = probe_document("0:1", "0:1")
        unknown["streams"][0]["side_data_list"] = copy.deepcopy(document["streams"][0]["side_data_list"])
        rotated = self.read_probe(unknown)
        self.assertTrue(rotated["source_sar_unknown"])
        self.assertEqual((rotated["display_sar"], rotated["height"]), ("1/1", 320))

    def test_malformed_probe_ratios_are_not_relabelled_unknown(self):
        for value in (None, "", "0/1", "1/1", "0:0", "1:0", "-1:1", "0:2", "unknown", 1, []):
            with self.subTest(value=value):
                with self.assertRaises(EVALUATOR.Invalid):
                    self.read_probe(probe_document(value, "0:1"))

    def test_frame_ratio_changes_fail_even_when_display_shapes_would_match(self):
        for stream, frame in (("0:1", "1:1"), ("1:1", "0:1"), ("2:1", "1:1"), ("1:1", None)):
            with self.subTest(stream=stream, frame=frame):
                with self.assertRaises(EVALUATOR.Invalid):
                    self.read_probe(probe_document(stream, frame))
        document = probe_document("0:1", "0:1")
        document["frames"][-1]["sample_aspect_ratio"] = "1:1"
        with self.assertRaises(EVALUATOR.Invalid):
            self.read_probe(document)

    def source_rgb(self, probe, sar):
        def run(_tool, args, _source=None, **kwargs):
            filters = args[args.index("-vf") + 1]
            self.assertLess(filters.index("showinfo@phase2_source"), filters.index("scale="))
            self.assertLess(filters.index("showinfo@phase2_source"), filters.index("setsar=1"))
            log = ("[showinfo@phase2_source @ 0x123] [info] config in time_base: 1/30, frame_rate: 30/1\n"
                   "[showinfo@phase2_source @ 0x123] [info] n: 0 pts: 1 pts_time:0.0333333 fmt:yuv420p " +
                   ("sar:" + sar + " " if sar is not None else "") + "s:320x240 i:P\n")
            kwargs["audit_stderr"](log.encode())
            return bytes(probe["width"] * probe["height"] * 3)
        with patch.object(EVALUATOR, "run_tool", side_effect=run):
            return EVALUATOR.source_rgb(object(), Source(), CASE, probe, {"pts": 1, "ordinal": 1}, None)

    def test_reference_decode_proves_original_unknown_sar_before_normalization(self):
        probe = self.read_probe(probe_document("0:1", "0:1"))
        self.assertEqual(len(self.source_rgb(probe, "0/1")), 240 * 180 * 3)
        for wrong in ("1/1", "0:1", "N/A", None):
            with self.subTest(sar=wrong):
                with self.assertRaises(EVALUATOR.Invalid):
                    self.source_rgb(probe, wrong)
        known = self.read_probe(probe_document("2:1", "2:1"))
        self.assertEqual(len(self.source_rgb(known, "2/1")), 240 * 90 * 3)
        with self.assertRaises(EVALUATOR.Invalid):
            self.source_rgb(known, "0/1")


if __name__ == "__main__":
    unittest.main()
