#!/usr/bin/env python3
"""Parser contracts only; real media calibration is separate test-env evidence."""

import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
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


class ManifestRoleContracts(unittest.TestCase):
    """Metadata/parser adversaries only; these files are never decoded as media."""

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="phase2-manifest-contract-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        os.chmod(self.root, 0o700)
        self.artifacts = EVALUATOR.PrivateArtifacts(str(self.root))
        self.labels = self.write_json("labels.json", {"purpose": "Parser unit fixture, not source review."})
        tool = self.root / "tool"
        tool.write_bytes(b"not an executable media tool")
        os.chmod(tool, 0o600)
        self.original = {
            "schema_version": 1, "corpus_id": "unit-corpus", "label_revision": "unit-labels",
            "split_unit": "episode", "artifacts_root": str(self.root),
            "tools": {name: {"path": str(tool), "sha256": hashlib.sha256(tool.read_bytes()).hexdigest()}
                      for name in ("ffmpeg", "ffprobe")},
            "thresholds": {"min_independent_positive_episodes": 3, "min_holdout_positive_cases": 3,
                "min_holdout_negative_cases": 2, "max_boundary_error_ticks": 50_000_000,
                "max_rgb_mae": 5, "max_rgb_p95_error": 16,
                "required_categories": ["normal_op", "insufficient_evidence"]},
            "cases": [self.case(name) for name in ("C1", "C2", "C3", "H1", "H2", "H3", "N1", "N2")]}
        original_ref = self.write_json("original-manifest.json", self.original)
        receipt = self.write_json("original-receipt.json", {"run": "observed-run", "complete": False})
        self.manifest = copy.deepcopy(self.original)
        self.manifest.update(schema_version=2, corpus_id="unit-expanded", consumer_case_id="FH1",
            original_attempt_refs=[{"run_id": "observed-run", "case_ids": [case["case_id"] for case in self.original["cases"]],
                "manifest": original_ref, "labels": self.labels, "receipt": receipt}])
        self.manifest["thresholds"].pop("min_holdout_positive_cases")
        self.manifest["thresholds"].pop("min_holdout_negative_cases")
        self.manifest["thresholds"].update(min_fresh_holdout_positive_cases=3, min_regression_negative_cases=2)
        for case in self.manifest["cases"]:
            case["evaluation_role"] = "calibration" if case["split"] == "calibration" else "regression"
        for name in ("C4", "C5", "C6", "FH1", "FH2", "FH3"):
            case = self.case(name)
            case["evaluation_role"] = "calibration" if name.startswith("C") else "fresh_holdout"
            self.manifest["cases"].append(case)

    def write_json(self, name, value):
        path = self.root / name
        raw = json.dumps(value).encode()
        path.write_bytes(raw)
        os.chmod(path, 0o600)
        return {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}

    def case(self, name):
        source = self.root / (name + ".mp4")
        source.write_bytes(("Parser identity fixture " + name).encode())
        os.chmod(source, 0o600)
        negative = name.startswith("N")
        return {"case_id": name, "split": "calibration" if name.startswith("C") else "holdout",
            "series_id": "unit:" + name if negative else "unit:series", "season_id": "unknown",
            "episode_id": name, "variant_group": "unit:" + name,
            "categories": ["insufficient_evidence" if negative else "normal_op"],
            "source": {"path": str(source), "sha256": hashlib.sha256(source.read_bytes()).hexdigest(),
                "size_bytes": source.stat().st_size, "video_stream_index": 0,
                "format_start_ticks": 0, "duration_ticks": 600_000_000},
            "provenance": {"kind": "real", "license_ref": "Unit parser metadata only", "author": "Unit fixture",
                "authorship_evidence": self.labels, "labeler": "Unit fixture", "label_evidence": self.labels,
                "label_method": "assistant_source_review"},
            "expected": {"kind": "negative" if negative else "positive",
                "intro": None if negative else {"start_ticks": 50_000_000, "end_ticks": 150_000_000},
                "safe": None if negative else {"start_ticks": 40_000_000, "end_ticks": 160_000_000},
                "start_tolerance_ticks": 10_000_000, "end_tolerance_ticks": 10_000_000,
                "narrative_intervals": [{"start_ticks": 200_000_000, "end_ticks": 500_000_000}]},
            "preview": {"required": True, "width": 320, "interval_ticks": 100_000_000, "pts_tolerance_ticks": 0}}

    def by_id(self, document=None):
        return {case["case_id"]: case for case in (document or self.manifest)["cases"]}

    def validate(self, document=None):
        return EVALUATOR.validate_manifest(document or self.manifest, self.artifacts)[0]

    def test_v1_replay_and_v2_calibration_population_preserve_originals(self):
        before = copy.deepcopy(self.original)
        self.assertEqual(len(self.validate(self.original)), 8)
        cases = self.validate()
        self.assertEqual(len(cases), 14)
        self.assertEqual(self.original, before)
        cohorts = {}
        for name, case in cases.items():
            key = (case["evaluation_role"], case["split"], case["series_id"], case["season_id"])
            cohorts.setdefault(key, set()).add(name)
        self.assertIn(set(("C1", "C2", "C3", "C4", "C5", "C6")), cohorts.values())
        self.assertIn(set(("H1", "H2", "H3")), cohorts.values())
        self.assertIn(set(("FH1", "FH2", "FH3")), cohorts.values())
        self.assertEqual(len(cohorts), 5)

    def test_observed_cases_cannot_be_relabelled_omitted_or_changed(self):
        for attack in ("fresh", "omitted", "renamed", "label", "preview", "no_history"):
            with self.subTest(attack=attack):
                value = copy.deepcopy(self.manifest)
                cases = self.by_id(value)
                if attack == "fresh":
                    cases["H1"]["evaluation_role"] = "fresh_holdout"
                elif attack == "omitted":
                    value["cases"].remove(cases["H1"])
                    value["original_attempt_refs"][0]["case_ids"].remove("H1")
                elif attack == "renamed":
                    cases["H1"]["case_id"] = "FH-renamed"
                    value["original_attempt_refs"][0]["case_ids"].remove("H1")
                elif attack == "label":
                    cases["C1"]["expected"]["end_tolerance_ticks"] += 1
                elif attack == "preview":
                    cases["H1"]["preview"]["required"] = False
                else:
                    value["original_attempt_refs"] = []
                with self.assertRaises(EVALUATOR.Invalid):
                    self.validate(value)

    def test_original_thresholds_and_reference_bindings_cannot_be_rewritten(self):
        for attack in ("rgb", "boundary", "support", "fresh_count", "negative_count", "categories", "labels", "receipt", "duplicate_run"):
            with self.subTest(attack=attack):
                value = copy.deepcopy(self.manifest)
                if attack == "rgb":
                    value["thresholds"]["max_rgb_mae"] = 6
                elif attack == "boundary":
                    value["thresholds"]["max_boundary_error_ticks"] += 1
                elif attack == "support":
                    value["thresholds"]["min_independent_positive_episodes"] = 4
                elif attack == "fresh_count":
                    value["thresholds"]["min_fresh_holdout_positive_cases"] = 2
                elif attack == "negative_count":
                    value["thresholds"]["min_regression_negative_cases"] = 1
                elif attack == "categories":
                    value["thresholds"]["required_categories"] = ["normal_op"]
                elif attack == "labels":
                    value["original_attempt_refs"][0]["labels"] = self.write_json("wrong-labels.json", {"other": True})
                elif attack == "receipt":
                    value["original_attempt_refs"][0]["receipt"] = self.write_json("wrong-receipt.json", {"run": "another-run"})
                else:
                    value["original_attempt_refs"].append(copy.deepcopy(value["original_attempt_refs"][0]))
                with self.assertRaises(EVALUATOR.Invalid):
                    self.validate(value)

    def test_consumer_requires_fresh_positive_preview_mp4_and_independent_identity(self):
        for attack in ("calibration", "regression", "missing", "negative", "no_preview", "mkv", "variant", "bytes", "invalid_role", "role_split"):
            with self.subTest(attack=attack):
                value = copy.deepcopy(self.manifest)
                cases = self.by_id(value)
                if attack in ("calibration", "regression", "missing"):
                    value["consumer_case_id"] = {"calibration": "C1", "regression": "H1", "missing": "absent"}[attack]
                elif attack == "negative":
                    cases["FH1"]["expected"].update(kind="negative", intro=None, safe=None)
                elif attack == "no_preview":
                    cases["FH1"]["preview"]["required"] = False
                elif attack == "mkv":
                    source = self.root / "consumer.mkv"
                    source.write_bytes(Path(cases["FH1"]["source"]["path"]).read_bytes())
                    os.chmod(source, 0o600)
                    cases["FH1"]["source"]["path"] = str(source)
                elif attack == "variant":
                    cases["FH2"]["variant_group"] = cases["FH1"]["variant_group"]
                elif attack == "bytes":
                    cases["FH2"]["source"] = copy.deepcopy(cases["FH1"]["source"])
                elif attack == "invalid_role":
                    cases["FH1"]["evaluation_role"] = ["fresh_holdout"]
                else:
                    cases["FH1"]["split"] = "calibration"
                with self.assertRaises(EVALUATOR.Invalid):
                    self.validate(value)

    def test_cross_role_aliases_include_real_file_identity(self):
        value = copy.deepcopy(self.manifest)
        cases = self.by_id(value)
        cases["FH2"]["variant_group"] = cases["H1"]["variant_group"]
        with self.assertRaisesRegex(EVALUATOR.Invalid, "physical_identity_across_role"):
            self.validate(value)
        cases = self.validate()
        snapshots = {name: {"device": 1, "inode": index} for index, name in enumerate(cases, 1)}
        snapshots["FH2"] = snapshots["H1"]
        groups = EVALUATOR.independent_groups(list(cases.values()), snapshots)
        with self.assertRaisesRegex(EVALUATOR.Invalid, "physical_identity_across_role"):
            EVALUATOR.check_group_splits(cases, groups, "FH1")

    def test_support_uses_real_independence_and_never_crosses_roles(self):
        cases = self.validate()
        groups = EVALUATOR.independent_groups(list(cases.values()), {})
        def observe(target, supports):
            observation = {"status": "published", "support_case_ids": supports, "reason": "unit-contract",
                "evidence": self.labels, **cases[target]["expected"]["intro"]}
            result = {"counts": dict.fromkeys(EVALUATOR.COUNTS, 0)}
            return EVALUATOR.evaluate_detection(cases[target], observation, self.artifacts, cases, groups,
                                                self.manifest["thresholds"], result)
        self.assertEqual(observe("C1", ["C1", "C4", "C5"])["independent_support_count"], 3)
        self.assertEqual(observe("FH1", ["FH1", "FH2", "FH3"])["independent_support_count"], 3)
        for target, support in (("FH1", ["FH1", "FH2", "H1"]), ("H1", ["H1", "FH2", "FH3"]),
                                ("FH1", ["FH1", "FH2"]), ("FH1", ["FH1", "FH2", "FH2"])):
            with self.subTest(target=target, support=support), self.assertRaises(EVALUATOR.Invalid):
                observe(target, support)

    def test_old_holdout_success_never_satisfies_fresh_population_gate(self):
        cases = self.validate()
        groups = EVALUATOR.independent_groups(list(cases.values()), {})
        rows = [{"case_id": name, "evaluation_role": case["evaluation_role"], "split": case["split"],
                 "expected": case["expected"]["kind"], "state": "pending" if name.startswith("FH") else "passed"}
                for name, case in cases.items()]
        gates = {gate["name"]: gate for gate in EVALUATOR.population_gates(self.manifest, rows, groups)}
        self.assertEqual((gates["fresh_holdout_positive"]["count"], gates["fresh_holdout_positive"]["state"]), (0, "pending"))
        self.assertEqual((gates["regression_negative"]["count"], gates["regression_negative"]["state"]), (2, "passed"))
        self.assertNotIn("fresh_holdout_negative", gates)
        for row in rows:
            row["state"] = "passed"
        gates = {gate["name"]: gate for gate in EVALUATOR.population_gates(self.manifest, rows, groups)}
        self.assertEqual(gates["fresh_holdout_positive"]["count"], 3)
        aliased = dict(groups, FH2=groups["FH1"])
        gates = {gate["name"]: gate for gate in EVALUATOR.population_gates(self.manifest, rows, aliased)}
        self.assertEqual((gates["fresh_holdout_positive"]["count"], gates["fresh_holdout_positive"]["state"]), (2, "pending"))
        old_rows = [row for row in rows if row["case_id"] in self.by_id(self.original)]
        legacy = {gate["name"]: gate for gate in EVALUATOR.population_gates(self.original, old_rows, groups)}
        self.assertEqual((legacy["holdout_positive"]["count"], legacy["holdout_negative"]["count"]), (3, 2))


if __name__ == "__main__":
    unittest.main()
