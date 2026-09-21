#!/usr/bin/env python3
"""Evaluate a frozen, independently labeled real-media corpus on Linux.

This file is a source-delivered verifier, not an execution record. It downloads
nothing and never installs tools, starts Goby, or modifies source media. See the
companion manifest document for the private input and public result contracts.
"""

from __future__ import annotations

import argparse
from bisect import bisect_right
from contextlib import ExitStack
from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import re
import selectors
import signal
import stat
import struct
import subprocess
import sys
import time


TICKS = 10_000_000
MAX_CASES = 256
MAX_JSON = 16 << 20
MAX_BIF = 128 << 20
MAX_JPEG = 2 << 20
MAX_FRAMES = 4096
MAX_PIXELS = 4 << 20
MAX_SOURCE_PIXELS = 16 << 20
MAX_SOURCE_FRAMES = 2_000_000
MAX_SOURCE_BYTES = 1 << 40
MAX_DURATION = 12 * 3600 * TICKS
MAX_PROBE = 192 << 20
MAX_DIAGNOSTICS = 8 << 20
PROCESS_SECONDS = 600
CASE_SECONDS = 1800
PREVIEW_PROFILE = "source-pts-display-preceding-hold-jpeg-v3;geometry=orthogonal-display-sar-v2"
GEOMETRY_PROFILE = "orthogonal-display-sar-v2"
UNREPORTED_SAR = object()
MAGIC = b"\x89BIF\r\n\x1a\n"
SHA = re.compile(r"[0-9a-f]{64}\Z")
IDENTIFIER = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}\Z")
CATEGORIES = frozenset({"normal_op", "cold_open", "recap", "changing_op",
                        "same_music_different_visuals", "dub", "insufficient_evidence",
                        "missing_audio", "vfr", "source_replacement", "no_intro", "logo", "silence"})
EVALUATION_ROLES = frozenset({"calibration", "regression", "fresh_holdout"})
COUNTS = ("false_positive", "miss", "abstention", "boundary", "narrative_safety", "pending", "failure")
FORMATS = "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,mpegts,avi"
CANCEL_REQUESTED = False


class Invalid(Exception):
    """A stable, non-sensitive failure code suitable for the public report."""


class Pending(Exception):
    """Required real evidence has not been captured."""


def request_cancel(_signal, _frame):
    # A handler must never interrupt ownership registration or process cleanup.
    global CANCEL_REQUESTED
    CANCEL_REQUESTED = True


def check_cancelled():
    require(not CANCEL_REQUESTED, "verification_cancelled")


def require(condition, code):
    if not condition:
        raise Invalid(code)


def object_fields(value, required, optional=()):
    require(isinstance(value, dict), "invalid_object")
    require(set(required) <= set(value) <= set(required) | set(optional), "invalid_object_fields")
    return value


def integer(value, minimum=0, maximum=(1 << 63) - 1):
    require(type(value) is int and minimum <= value <= maximum, "invalid_integer")
    return value


def number(value, minimum=0, maximum=255):
    require(type(value) in (int, float) and math.isfinite(value) and minimum <= value <= maximum,
            "invalid_number")
    return value


def text(value, maximum=4096):
    require(isinstance(value, str) and 0 < len(value) <= maximum and not any(ord(c) < 32 for c in value),
            "invalid_text")
    return value


def unique_strings(value, maximum, allowed=None):
    require(isinstance(value, list) and len(value) <= maximum and all(isinstance(item, str) for item in value),
            "invalid_string_array")
    require(len(set(value)) == len(value) and (allowed is None or set(value) <= allowed), "invalid_string_array")
    return value


def identifier(value):
    require(isinstance(value, str) and IDENTIFIER.fullmatch(value), "invalid_identifier")
    return value


def digest(value):
    require(isinstance(value, str) and SHA.fullmatch(value), "invalid_sha256")
    return value


def no_duplicates(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate_json_key")
        result[key] = value
    return result


def decode_json(data):
    try:
        return json.loads(data, object_pairs_hook=no_duplicates,
                          parse_constant=lambda _: (_ for _ in ()).throw(Invalid("nonfinite_json_number")))
    except (UnicodeError, ValueError, RecursionError) as error:
        raise Invalid("invalid_json") from error


def absolute_path(value):
    path = Path(text(value))
    require(path.is_absolute() and path == path.resolve(strict=True), "noncanonical_or_missing_path")
    return path


def identity(info):
    return {"device": info.st_dev, "inode": info.st_ino, "size_bytes": info.st_size,
            "mtime_ns": info.st_mtime_ns, "ctime_ns": info.st_ctime_ns}


class HeldFile:
    """Keep a regular file open and audit both its descriptor and its pathname."""

    def __init__(self, path, maximum, expected_sha=None, expected_size=None):
        self.path = absolute_path(str(path))
        self.fd = os.open(self.path, os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            info = os.fstat(self.fd)
            require(stat.S_ISREG(info.st_mode) and 0 < info.st_size <= maximum, "file_type_or_size")
            self.before = identity(info)
            require(identity(os.stat(self.path, follow_symlinks=False)) == self.before, "file_identity_changed")
            self.sha256 = self.hash()
            if expected_sha is not None:
                require(self.sha256 == digest(expected_sha), "file_sha256_mismatch")
            if expected_size is not None:
                require(info.st_size == expected_size, "file_size_mismatch")
            self.check(False)
        except BaseException:
            os.close(self.fd)
            raise

    def __enter__(self):
        return self

    def __exit__(self, *_):
        os.close(self.fd)

    def read(self, count, offset=0):
        require(0 <= count <= MAX_PROBE and 0 <= offset <= self.before["size_bytes"], "read_budget")
        chunks = bytearray()
        while len(chunks) < count:
            check_cancelled()
            block = os.pread(self.fd, min(1 << 20, count - len(chunks)), offset + len(chunks))
            require(bool(block), "truncated_file")
            chunks.extend(block)
        return bytes(chunks)

    def hash(self):
        result = hashlib.sha256()
        offset = 0
        while offset < self.before["size_bytes"]:
            check_cancelled()
            block = os.pread(self.fd, min(1 << 20, self.before["size_bytes"] - offset), offset)
            require(bool(block), "truncated_file")
            result.update(block)
            offset += len(block)
        require(not os.pread(self.fd, 1, offset), "file_size_changed")
        return result.hexdigest()

    def check(self, rehash=True):
        require(identity(os.fstat(self.fd)) == self.before and
                identity(os.stat(self.path, follow_symlinks=False)) == self.before, "file_identity_changed")
        if rehash:
            require(self.hash() == self.sha256, "file_content_changed")
            self.check(False)

    def snapshot(self):
        return dict(self.before, sha256=self.sha256)

    def proc_path(self):
        return "/proc/self/fd/" + str(self.fd)


def load_json(path, maximum=MAX_JSON):
    with HeldFile(path, maximum) as source:
        document = decode_json(source.read(source.before["size_bytes"]))
        source.check()
        return document, source.sha256


class PrivateArtifacts:
    def __init__(self, root):
        self.root = absolute_path(root)
        info = self.root.stat()
        require(stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid() and
                stat.S_IMODE(info.st_mode) & 0o077 == 0, "artifacts_root_not_private")

    def path(self, value):
        path = absolute_path(value)
        require(path != self.root and self.root in path.parents, "artifact_outside_private_root")
        for candidate in (path, *path.parents):
            info = candidate.stat()
            require(info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) & 0o077 == 0,
                    "artifact_not_private")
            if candidate == self.root:
                break
        return path

    def reference(self, value, pending=False):
        if value is None and pending:
            raise Pending("missing_evidence_artifact")
        object_fields(value, ("path", "sha256"))
        with HeldFile(self.path(value["path"]), MAX_JSON, digest(value["sha256"])) as artifact:
            artifact.check()
        return value["sha256"]

    def write_result(self, path, report):
        if report.get("admitted") is True or report.get("state") == "passed":
            check_cancelled()
        target = Path(path)
        require(target.is_absolute() and not target.exists(), "output_must_be_new_absolute_file")
        parent = absolute_path(str(target.parent))
        require(parent == self.root or self.root in parent.parents, "output_outside_private_root")
        if parent != self.root:
            self.path(str(parent))
        payload = (json.dumps(report, sort_keys=True, indent=2, allow_nan=False) + "\n").encode()
        require(len(payload) <= MAX_JSON, "report_budget")
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_CLOEXEC | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as output:
            output.write(payload)
            output.flush()
            os.fsync(output.fileno())


def stop_group(process):
    deadline = time.monotonic() + 2
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=0.25)
    except subprocess.TimeoutExpired:
        pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=max(0.001, deadline - time.monotonic()))
    except subprocess.TimeoutExpired as error:
        raise Invalid("process_reap_failed") from error


def run_tool(tool, args, source=None, input_bytes=None, stdout_limit=MAX_PROBE, deadline=None, audit_stderr=None):
    """Bound both pipes and wall time, then reap the owned process group."""
    check_cancelled()
    tool.check(False)
    if source is not None:
        source.check(False)
    require(input_bytes is None or len(input_bytes) <= MAX_JPEG, "process_input_budget")
    end = min(time.monotonic() + PROCESS_SECONDS, deadline or float("inf"))
    require(end > time.monotonic(), "case_deadline")
    descriptors = tuple({tool.fd} | ({source.fd} if source is not None else set()))
    process = subprocess.Popen([str(tool.path), *args], executable=tool.proc_path(),
                               stdin=subprocess.PIPE if input_bytes is not None else subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, pass_fds=descriptors,
                               start_new_session=True, close_fds=True,
                               env={"PATH": "/usr/bin:/bin", "LC_ALL": "C", "AV_LOG_FORCE_NOCOLOR": "1"})
    selector = None
    stdout, stderr = bytearray(), bytearray()
    input_offset = 0
    try:
        selector = selectors.DefaultSelector()
        for pipe, label in ((process.stdout, "stdout"), (process.stderr, "stderr")):
            os.set_blocking(pipe.fileno(), False)
            selector.register(pipe, selectors.EVENT_READ, label)
        if process.stdin is not None:
            os.set_blocking(process.stdin.fileno(), False)
            selector.register(process.stdin, selectors.EVENT_WRITE, "stdin")
        while selector.get_map():
            check_cancelled()
            remaining = end - time.monotonic()
            require(remaining > 0, "process_timeout")
            for key, _ in selector.select(min(remaining, 0.25)):
                if key.data == "stdin":
                    try:
                        count = os.write(key.fd, input_bytes[input_offset:input_offset + 65536])
                    except BrokenPipeError:
                        count = 0
                    input_offset += count
                    if count == 0 or input_offset == len(input_bytes):
                        selector.unregister(key.fileobj)
                        key.fileobj.close()
                    continue
                block = os.read(key.fd, 65536)
                if not block:
                    selector.unregister(key.fileobj)
                    key.fileobj.close()
                    continue
                destination = stdout if key.data == "stdout" else stderr
                limit = stdout_limit if key.data == "stdout" else MAX_DIAGNOSTICS
                require(len(destination) + len(block) <= limit, "process_output_budget")
                destination.extend(block)
        try:
            status = process.wait(timeout=max(0.001, end - time.monotonic()))
        except subprocess.TimeoutExpired as error:
            raise Invalid("process_timeout") from error
        require(status == 0, "decoder_process_failed")
        check_cancelled()
        require(input_bytes is None or input_offset == len(input_bytes), "incomplete_decoder_input")
        if audit_stderr is None:
            require(not stderr, "decoder_reported_diagnostics")
        else:
            audit_stderr(bytes(stderr))
        return bytes(stdout)
    finally:
        if selector is not None:
            selector.close()
        try:
            stop_group(process)
        finally:
            for pipe in (process.stdin, process.stdout, process.stderr):
                if pipe is not None and not pipe.closed:
                    pipe.close()
            tool.check(False)
            if source is not None:
                source.check(False)


def interval(value, duration):
    object_fields(value, ("start_ticks", "end_ticks"))
    start = integer(value["start_ticks"], 0, duration - 1)
    end = integer(value["end_ticks"], start + 1, duration)
    return start, end


def validate_manifest(document, artifacts):
    require(isinstance(document, dict) and type(document.get("schema_version")) is int and
            document["schema_version"] in (1, 2), "manifest_version")
    version = document["schema_version"]
    manifest_fields = ("schema_version", "corpus_id", "label_revision", "split_unit", "artifacts_root",
                       "tools", "thresholds", "cases")
    if version == 2:
        manifest_fields += ("consumer_case_id", "original_attempt_refs")
    object_fields(document, manifest_fields)
    identifier(document["corpus_id"])
    text(document["label_revision"], 256)
    require(document["split_unit"] in ("series", "season", "episode"), "invalid_split_unit")
    object_fields(document["tools"], ("ffmpeg", "ffprobe"))
    for tool in document["tools"].values():
        object_fields(tool, ("path", "sha256"))
        absolute_path(tool["path"])
        digest(tool["sha256"])
    thresholds = document["thresholds"]
    population_thresholds = ("min_holdout_positive_cases", "min_holdout_negative_cases") if version == 1 else \
                            ("min_fresh_holdout_positive_cases", "min_regression_negative_cases")
    object_fields(thresholds, ("min_independent_positive_episodes", *population_thresholds,
                              "max_boundary_error_ticks", "max_rgb_mae",
                              "max_rgb_p95_error"), ("required_categories",))
    integer(thresholds["min_independent_positive_episodes"], 3, MAX_CASES)
    for name in population_thresholds:
        minimum = 3 if name == "min_fresh_holdout_positive_cases" else \
                  2 if name == "min_regression_negative_cases" else 1
        integer(thresholds[name], minimum, MAX_CASES)
    integer(thresholds["max_boundary_error_ticks"], 0, 30 * TICKS)
    number(thresholds["max_rgb_mae"])
    number(thresholds["max_rgb_p95_error"])
    required_categories = unique_strings(thresholds.get("required_categories", sorted(CATEGORIES)), len(CATEGORIES), CATEGORIES)
    cases = document["cases"]
    require(isinstance(cases, list) and 1 <= len(cases) <= MAX_CASES, "manifest_case_budget")
    case_ids, split_groups, variant_groups, hashes = set(), {}, {}, {}
    by_id = {}
    for case in cases:
        case_fields = ("case_id", "split", "categories", "series_id", "season_id", "episode_id",
                       "variant_group", "source", "provenance", "expected", "preview")
        if version == 2:
            case_fields += ("evaluation_role",)
        object_fields(case, case_fields)
        case_id = identifier(case["case_id"])
        require(case_id not in case_ids, "duplicate_case_id")
        case_ids.add(case_id)
        by_id[case_id] = case
        for name in ("series_id", "season_id", "episode_id", "variant_group"):
            identifier(case[name])
        require(case["split"] in ("calibration", "holdout"), "invalid_split")
        if version == 2:
            role = case["evaluation_role"]
            require(isinstance(role, str) and role in EVALUATION_ROLES, "invalid_evaluation_role")
            require(role == "regression" or case["split"] ==
                    ("calibration" if role == "calibration" else "holdout"), "role_split_mismatch")
        categories = unique_strings(case["categories"], len(CATEGORIES), CATEGORIES)
        require(bool(categories), "invalid_categories")
        require(case["season_id"] != "unknown" or document["split_unit"] == "episode",
                "unknown_season_requires_episode_split")
        group = (case["series_id"],)
        if document["split_unit"] != "series":
            group += (case["season_id"],)
        if document["split_unit"] == "episode":
            group += (case["episode_id"],)
        require(split_groups.setdefault(group, case["split"]) == case["split"], "calibration_holdout_leakage")
        source = case["source"]
        object_fields(source, ("path", "sha256", "size_bytes", "video_stream_index", "format_start_ticks", "duration_ticks"))
        absolute_path(source["path"])
        digest(source["sha256"])
        integer(source["size_bytes"], 1, MAX_SOURCE_BYTES)
        integer(source["video_stream_index"], 0, 4095)
        integer(source["format_start_ticks"], -MAX_DURATION, MAX_DURATION)
        duration = integer(source["duration_ticks"], 1, MAX_DURATION)
        require(hashes.setdefault(source["sha256"], case["split"]) == case["split"], "duplicate_content_across_split")
        require(variant_groups.setdefault(case["variant_group"], case["split"]) == case["split"],
                "variant_across_split")
        provenance = case["provenance"]
        object_fields(provenance, ("kind", "license_ref", "author", "authorship_evidence", "labeler", "label_evidence",
                                   "label_method"))
        require(provenance["kind"] == "real", "synthetic_corpus_not_admitted")
        require(provenance["label_method"] in ("human_review", "assistant_source_review"), "invalid_label_method")
        for name in ("license_ref", "author", "labeler"):
            text(provenance[name])
        for name in ("authorship_evidence", "label_evidence"):
            artifacts.reference(provenance[name])
        expected = case["expected"]
        object_fields(expected, ("kind", "intro", "safe", "start_tolerance_ticks", "end_tolerance_ticks", "narrative_intervals"))
        require(expected["kind"] in ("positive", "negative"), "invalid_expected_kind")
        for name in ("start_tolerance_ticks", "end_tolerance_ticks"):
            integer(expected[name], 0, thresholds["max_boundary_error_ticks"])
        require(isinstance(expected["narrative_intervals"], list) and len(expected["narrative_intervals"]) <= 256,
                "narrative_interval_budget")
        narratives = [interval(value, duration) for value in expected["narrative_intervals"]]
        if expected["kind"] == "positive":
            start, end = interval(expected["intro"], duration)
            safe_start, safe_end = interval(expected["safe"], duration)
            require(safe_start <= start < end <= safe_end, "intro_outside_safe_interval")
            require(not any(max(safe_start, a) < min(safe_end, b) for a, b in narratives),
                    "safe_interval_overlaps_narrative")
        else:
            require(expected["intro"] is None and expected["safe"] is None, "negative_has_intro")
        preview = case["preview"]
        object_fields(preview, ("required", "width", "interval_ticks", "pts_tolerance_ticks"))
        require(type(preview["required"]) is bool and preview["width"] in (240, 320, 400), "invalid_preview_profile")
        integer(preview["interval_ticks"], 2 * TICKS, 120 * TICKS)
        require(preview["interval_ticks"] % TICKS == 0, "preview_interval_not_whole_seconds")
        integer(preview["pts_tolerance_ticks"], 0, TICKS)
    if version == 2:
        validate_original_attempts(document, by_id, artifacts)
        consumer = by_id.get(identifier(document["consumer_case_id"]))
        require(consumer is not None and consumer["evaluation_role"] == "fresh_holdout" and
                consumer["expected"]["kind"] == "positive" and consumer["preview"]["required"] and
                Path(consumer["source"]["path"]).suffix.lower() == ".mp4", "invalid_fresh_consumer")
        check_group_splits(by_id, independent_groups(cases, {}), document["consumer_case_id"])
    return by_id, required_categories


def referenced_json(artifacts, reference):
    artifacts.reference(reference)
    value, actual = load_json(artifacts.path(reference["path"]))
    require(actual == reference["sha256"], "original_reference_changed")
    return value


def validate_original_attempts(document, by_id, artifacts):
    """Bind observed cases to immutable history; a new name cannot make them fresh."""
    attempts = document["original_attempt_refs"]
    require(isinstance(attempts, list) and 1 <= len(attempts) <= 32, "original_attempt_budget")
    runs, historical = set(), set()
    for attempt in attempts:
        object_fields(attempt, ("run_id", "case_ids", "manifest", "labels", "receipt"))
        run = identifier(attempt["run_id"])
        require(run not in runs, "duplicate_original_attempt")
        runs.add(run)
        selected = unique_strings(attempt["case_ids"], MAX_CASES, set(by_id))
        original = referenced_json(artifacts, attempt["manifest"])
        artifacts.reference(attempt["labels"])
        receipt = referenced_json(artifacts, attempt["receipt"])
        require(isinstance(receipt, dict) and receipt.get("run") == run, "original_receipt_run_mismatch")
        require(isinstance(original, dict) and type(original.get("schema_version")) is int and
                original["schema_version"] in (1, 2) and isinstance(original.get("cases"), list) and
                1 <= len(original["cases"]) <= MAX_CASES, "invalid_original_manifest")
        old_thresholds = original.get("thresholds")
        require(isinstance(old_thresholds, dict), "invalid_original_thresholds")
        for name in ("min_independent_positive_episodes", "max_boundary_error_ticks", "max_rgb_mae",
                     "max_rgb_p95_error", "required_categories"):
            require(document["thresholds"].get(name, sorted(CATEGORIES)) ==
                    old_thresholds.get(name, sorted(CATEGORIES)), "original_threshold_changed")
        for current_name, old_name in (("min_fresh_holdout_positive_cases", "min_holdout_positive_cases"),
                                       ("min_regression_negative_cases", "min_holdout_negative_cases")):
            previous = old_thresholds.get(old_name, old_thresholds.get(current_name))
            require(type(previous) is int and document["thresholds"][current_name] >= previous,
                    "original_population_threshold_lowered")
        original_cases = {}
        for old in original["cases"]:
            require(isinstance(old, dict) and isinstance(old.get("case_id"), str) and
                    old["case_id"] not in original_cases, "invalid_original_case_identity")
            original_cases[old["case_id"]] = old
        require(set(selected) == set(original_cases), "original_case_omitted")
        for case_id, old in original_cases.items():
            current = by_id[case_id]
            # Do not reinterpret old labels, source identities or preview obligations.
            for name in ("split", "categories", "series_id", "season_id", "episode_id", "variant_group",
                         "source", "provenance", "expected", "preview"):
                require(name in old and current[name] == old[name], "original_case_changed")
            require(old["provenance"]["label_evidence"]["sha256"] == attempt["labels"]["sha256"],
                    "original_labels_reference_mismatch")
            original_role = old.get("evaluation_role", old["split"])
            required_role = "calibration" if original_role == "calibration" else "regression"
            require(current["evaluation_role"] == required_role, "observed_case_relabelled_fresh")
            historical.add(case_id)
    require(all(case_id in historical for case_id, case in by_id.items()
                if case["evaluation_role"] == "regression"), "regression_missing_original_attempt")


def observation_snapshot(value, source, require_ctime=False):
    if value is None:
        raise Pending("missing_source_run_snapshot")
    required = ("sha256", "size_bytes", "device", "inode", "mtime_ns")
    object_fields(value, (*required, "ctime_ns") if require_ctime else required, () if require_ctime else ("ctime_ns",))
    digest(value["sha256"])
    for name in set(value) - {"sha256"}:
        integer(value[name])
    require(all(source.snapshot()[name] == expected for name, expected in value.items()),
            "source_run_snapshot_mismatch")


def processed_source(value, case, observation, artifacts, stack):
    """Bind the actual application input, not only the immutable corpus master."""
    if value is None:
        raise Pending("missing_processed_source_evidence")
    object_fields(value, ("path", "item_id", "source_revision"), ("source_before", "source_after"))
    identifier(value["item_id"])
    text(value["source_revision"], 256)
    source = stack.enter_context(HeldFile(value["path"], MAX_SOURCE_BYTES,
                                         case["source"]["sha256"], case["source"]["size_bytes"]))
    info = os.fstat(source.fd)
    require(info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) & 0o077 == 0,
            "processed_source_not_private")
    observation_snapshot(value.get("source_before"), source, require_ctime=True)
    observation_snapshot(value.get("source_after"), source, require_ctime=True)
    reference = observation["detection"]["evidence"]
    response, response_sha = load_json(artifacts.path(reference["path"]))
    require(response_sha == reference["sha256"] and isinstance(response, dict) and
            type(response.get("Status")) is int and response["Status"] == 200,
            "processed_source_http_evidence_mismatch")
    body = response.get("Body")
    require(isinstance(body, dict) and body.get("Id") == value["item_id"] and
            body.get("SourceRevision") == value["source_revision"] and
            isinstance(body.get("Detection"), dict) and
            body["Detection"].get("SourceRevision") == value["source_revision"],
            "processed_source_http_identity_mismatch")
    return source


def independent_groups(cases, snapshots):
    """Union aliases transitively; alternate dubs never create extra support."""
    parents = {case["case_id"]: case["case_id"] for case in cases}

    def find(value):
        while parents[value] != value:
            parents[value] = parents[parents[value]]
            value = parents[value]
        return value

    seen = {}
    for case in cases:
        case_id = case["case_id"]
        keys = [("episode", case["series_id"], case["season_id"], case["episode_id"]),
                ("variant", case["variant_group"]), ("sha256", case["source"]["sha256"])]
        snapshot = snapshots.get(case_id)
        if snapshot:
            keys.append(("file", snapshot["device"], snapshot["inode"]))
        for key in keys:
            if key in seen:
                parents[find(case_id)] = find(seen[key])
            else:
                seen[key] = case_id
    return {case_id: find(case_id) for case_id in parents}


def check_group_splits(by_id, groups, consumer_case_id=None):
    grouped_splits, grouped_roles = {}, {}
    for case_id, group in groups.items():
        split = by_id[case_id]["split"]
        require(grouped_splits.setdefault(group, split) == split, "physical_identity_across_split")
        if "evaluation_role" in by_id[case_id]:
            role = by_id[case_id]["evaluation_role"]
            require(grouped_roles.setdefault(group, role) == role, "physical_identity_across_role")
    if consumer_case_id is not None:
        require(sum(group == groups[consumer_case_id] for group in groups.values()) == 1,
                "consumer_identity_not_independent")


def admit(manifest, manifest_sha, artifacts):
    """Validate identity only; never execute FFmpeg or ffprobe in this mode."""
    by_id, _ = validate_manifest(manifest, artifacts)
    snapshots = {}
    with ExitStack() as stack:
        sources = []
        for case_id, case in by_id.items():
            expected = case["source"]
            source = stack.enter_context(HeldFile(expected["path"], MAX_SOURCE_BYTES,
                                                 expected["sha256"], expected["size_bytes"]))
            snapshots[case_id] = source.snapshot()
            sources.append(source)
        check_group_splits(by_id, independent_groups(list(by_id.values()), snapshots), manifest.get("consumer_case_id"))
        for expected in manifest["tools"].values():
            source = stack.enter_context(HeldFile(expected["path"], 1 << 30, expected["sha256"]))
            require(os.fstat(source.fd).st_mode & 0o111, "pinned_tool_not_executable")
            sources.append(source)
        for source in sources:
            source.check()
    return {"schema_version": manifest["schema_version"], "manifest_sha256": manifest_sha, "admitted": True,
            "cases": [{"case_id": case_id, "source": snapshots[case_id]} for case_id in by_id]}


def parse_bif(source):
    """Parse only the published Roku v0 layout, independently of Goby's codec."""
    size = source.before["size_bytes"]
    require(72 <= size <= MAX_BIF, "bif_size")
    header = source.read(64)
    require(header[:8] == MAGIC, "bif_magic")
    version, count, multiplier = struct.unpack_from("<III", header, 8)
    require(version == 0 and count <= MAX_FRAMES and header[20:] == bytes(44), "bif_header")
    require(64 + 8 * (count + 1) <= size, "bif_index_budget")
    entries = list(struct.iter_unpack("<II", source.read(8 * (count + 1), 64)))
    require(entries[-1] == (0xFFFFFFFF, size), "bif_sentinel")
    multiplier = multiplier or 1000
    if count == 0:
        require(entries[0][1] == 72, "bif_empty_layout")
        return [], multiplier
    require(entries[0][1] >= 64 + 8 * (count + 1), "bif_first_offset")
    frames = []
    for index, (timestamp, offset) in enumerate(entries[:-1]):
        end = entries[index + 1][1]
        require(timestamp != 0xFFFFFFFF and offset < end <= size and end - offset <= MAX_JPEG,
                "bif_frame_bounds")
        require(index == 0 or timestamp > entries[index - 1][0], "bif_timestamp_order")
        require(timestamp == index, "bif_consumer_requires_ordinal_timestamps")
        frames.append({"ticks": timestamp * multiplier * 10_000, "offset": offset, "size": end - offset})
    require(frames[0]["ticks"] == 0, "bif_uniform_origin")
    if len(frames) > 1:
        step = frames[1]["ticks"]
        require(step > 0 and all(frame["ticks"] == index * step for index, frame in enumerate(frames)),
                "bif_nonuniform_consumer_timeline")
    return frames, multiplier


def rational(value, separator="/"):
    require(isinstance(value, str) and len(value) <= 64 and re.fullmatch(r"[0-9]+" + re.escape(separator) + r"[0-9]+", value),
            "invalid_time_base")
    numerator, denominator = map(int, value.split(separator))
    require(0 < numerator <= (1 << 31) - 1 and 0 < denominator <= (1 << 31) - 1, "invalid_time_base")
    return Fraction(numerator, denominator)


def source_sample_aspect_ratio(value=UNREPORTED_SAR):
    """Keep unknown source facts separate from a square-pixel display policy."""
    if value is UNREPORTED_SAR:
        return Fraction(1), True, "absent"
    if value in ("N/A", "0:1"):
        return Fraction(1), True, value
    try:
        return rational(value, ":"), False, value
    except Invalid as error:
        raise Invalid("invalid_source_sample_aspect_ratio") from error


def decimal_fraction(value):
    require(isinstance(value, str) and len(value) <= 64 and re.fullmatch(r"-?[0-9]+(?:\.[0-9]+)?", value),
            "unknown_source_time")
    return Fraction(value)


def rotation(stream):
    transforms = {
        (65536, 0, 0, 0, 65536, 0, 0, 0, 1073741824): (0, ""),
        (0, 65536, 0, -65536, 0, 0, 0, 0, 1073741824): (90, "transpose=clock,"),
        (-65536, 0, 0, 0, -65536, 0, 0, 0, 1073741824): (180, "hflip,vflip,"),
        (0, -65536, 0, 65536, 0, 0, 0, 0, 1073741824): (270, "transpose=cclock,"),
    }
    sides = stream.get("side_data_list", [])
    require(isinstance(sides, list) and len(sides) <= 16, "display_side_data_budget")
    matrices = [side for side in sides if side.get("side_data_type") == "Display Matrix"]
    require(len(matrices) <= 1, "ambiguous_display_matrix")
    clockwise, filters = 0, ""
    if matrices:
        side = matrices[0]
        value = side.get("displaymatrix")
        require(isinstance(value, str) and len(value) <= 512, "invalid_display_matrix")
        lines = value.strip().splitlines()
        require(len(lines) == 3, "invalid_display_matrix")
        matrix = []
        for row, line in enumerate(lines):
            parts = line.split()
            require(len(parts) == 4 and parts[0] == f"{row:08d}:", "invalid_display_matrix")
            try:
                matrix.extend(int(part) for part in parts[1:])
            except ValueError as error:
                raise Invalid("invalid_display_matrix") from error
        require(tuple(matrix) in transforms, "unsupported_display_matrix")
        clockwise, filters = transforms[tuple(matrix)]
        require(type(side.get("rotation")) is int and side["rotation"] % 360 == (-clockwise) % 360,
                "inconsistent_display_rotation")
    tag = stream.get("tags", {}).get("rotate")
    if tag is not None:
        angle = decimal_fraction(tag)
        require(angle.denominator == 1 and angle % 360 == (-clockwise) % 360, "inconsistent_rotation_tag")
    return clockwise, filters


def probe_source(tool, source, case, deadline):
    index = case["source"]["video_stream_index"]
    args = ["-v", "error", "-max_alloc", "268435456", "-threads", "1", "-max_pixels", str(MAX_SOURCE_PIXELS),
            "-probesize", "8388608", "-analyzeduration", "10000000", "-max_probe_packets", "256",
            "-protocol_whitelist", "file,pipe", "-format_whitelist", FORMATS, "-fflags", "+nofillin-genpts",
            "-select_streams", str(index), "-show_frames", "-show_packets", "-show_entries",
            "format=start_time,duration:stream=index,codec_type,width,height,time_base,sample_aspect_ratio:"
            "stream_disposition=attached_pic:stream_tags=rotate:stream_side_data=side_data_type,displaymatrix,rotation:"
            "frame=stream_index,pts,width,height,sample_aspect_ratio:packet=stream_index,pts", "-of", "json", "-i", source.proc_path()]
    document = decode_json(run_tool(tool, args, source, deadline=deadline))
    streams = document.get("streams")
    require(isinstance(streams, list) and len(streams) == 1, "ambiguous_video_stream")
    stream = streams[0]
    require(stream.get("index") == index and stream.get("codec_type") == "video" and
            stream.get("disposition", {}).get("attached_pic", 0) == 0, "source_stream_mismatch")
    width = integer(stream.get("width"), 1, MAX_SOURCE_PIXELS)
    height = integer(stream.get("height"), 1, MAX_SOURCE_PIXELS)
    require(width * height <= MAX_SOURCE_PIXELS, "source_pixel_budget")
    base = rational(stream.get("time_base"))
    source_sar, source_sar_unknown, source_sar_report = source_sample_aspect_ratio(stream.get("sample_aspect_ratio", UNREPORTED_SAR))
    sar = source_sar
    clockwise, filters = rotation(stream)
    if clockwise in (90, 270):
        width, height, sar = height, width, 1 / sar
    target_width = case["preview"]["width"]
    exact_height = Fraction(target_width * height, width) / sar
    target_height = (2 * exact_height.numerator + exact_height.denominator) // (2 * exact_height.denominator)
    require(0 < target_height <= 4096 and target_width * target_height <= MAX_PIXELS, "preview_geometry_budget")
    origin = case["source"]["format_start_ticks"]
    observed_origin = decimal_fraction(document.get("format", {}).get("start_time")) * TICKS
    require(abs(observed_origin - origin) <= 10, "format_origin_mismatch")
    duration = case["source"]["duration_ticks"]
    observed_duration = decimal_fraction(document.get("format", {}).get("duration")) * TICKS
    require(abs(observed_duration - duration) <= 10, "source_duration_mismatch")
    combined = document.get("packets_and_frames")
    if combined is not None:
        require(isinstance(combined, list) and len(combined) <= MAX_SOURCE_FRAMES * 5, "source_frame_budget")
        packets = [item for item in combined if item.get("type") == "packet"]
        frames = [item for item in combined if item.get("type") == "frame"]
    else:
        packets, frames = document.get("packets"), document.get("frames")
    require(isinstance(packets, list) and 0 < len(packets) <= MAX_SOURCE_FRAMES * 4 and
            isinstance(frames, list) and 0 < len(frames) <= MAX_SOURCE_FRAMES, "missing_original_pts")
    packet_pts = set()
    for number, packet in enumerate(packets):
        if number % 1024 == 0:
            check_cancelled()
        require(packet.get("stream_index") == index, "unexpected_packet_stream")
        pts = integer(packet.get("pts"), -(1 << 63) + 1)
        require(pts not in packet_pts, "duplicate_original_pts")
        packet_pts.add(pts)
    timeline, last_pts, frame_sar_reports = [], None, {}
    for number, frame in enumerate(frames):
        if number % 1024 == 0:
            check_cancelled()
        require(frame.get("stream_index") == index and frame.get("width") == stream["width"] and
                frame.get("height") == stream["height"], "source_frame_geometry_changed")
        frame_sar, frame_sar_unknown, frame_sar_report = source_sample_aspect_ratio(frame.get("sample_aspect_ratio", UNREPORTED_SAR))
        require(frame_sar_unknown == source_sar_unknown and
                (source_sar_unknown or frame_sar == source_sar), "source_frame_sar_changed")
        frame_sar_reports[frame_sar_report] = frame_sar_reports.get(frame_sar_report, 0) + 1
        require(len(frame_sar_reports) <= 16, "source_frame_sar_report_budget")
        pts = integer(frame.get("pts"), -(1 << 63) + 1)
        require(pts in packet_pts and (last_pts is None or pts > last_pts), "unproven_or_unordered_frame_pts")
        relative = pts * base * TICKS - origin
        require(0 <= relative < duration, "frame_outside_admitted_source_timeline")
        ticks = relative.numerator // relative.denominator
        require(not timeline or ticks > timeline[-1][1], "frame_pts_tick_collision")
        timeline.append((pts, ticks))
        last_pts = pts
    return {"width": target_width, "height": target_height, "filters": filters, "base": base,
            "timeline": timeline, "origin_ticks": origin, "complete_decoded_eof": True,
            "source_pts": [pts for pts, _ in timeline], "geometry_profile": GEOMETRY_PROFILE,
            "source_sar_unknown": source_sar_unknown, "source_sar_report": source_sar_report,
            "source_frame_sar_reports": frame_sar_reports,
            "display_sar": f"{sar.numerator}/{sar.denominator}",
            "display_sar_policy": "unknown_source_square_pixel_display" if source_sar_unknown else "known_source_ratio"}


def selected_frames(probe, nominal, duration):
    """Select presentation intervals from exact source PTS, including holds."""
    require(probe["complete_decoded_eof"] and probe["timeline"], "missing_complete_source_decode")
    result = []
    for tick in nominal:
        check_cancelled()
        require(0 <= tick < duration, "nominal_slot_outside_source_timeline")
        # Compare in the original stream's exact PTS units. Flooring to the
        # public tick grid before selection could choose a later source frame.
        target_pts = Fraction(tick + probe["origin_ticks"], TICKS) / probe["base"]
        ordinal = bisect_right(probe["source_pts"], target_pts) - 1
        if ordinal < 0:
            ordinal = 0
            policy = "first_frame_hold"
            require(target_pts < probe["source_pts"][0], "unproven_first_frame_hold")
        elif ordinal == len(probe["timeline"]) - 1:
            policy = "last_frame_hold_to_container_end"
        else:
            policy = "preceding_until_next_source_pts"
            require(probe["source_pts"][ordinal] <= target_pts < probe["source_pts"][ordinal + 1],
                    "unproven_source_presentation_interval")
        pts, actual = probe["timeline"][ordinal]
        require(not result or ordinal >= result[-1]["ordinal"] and actual >= result[-1]["actual_ticks"],
                "unordered_selected_source_pts")
        result.append({"ordinal": ordinal, "pts": pts, "actual_ticks": actual, "selection_policy": policy,
                       "next_source_pts": probe["source_pts"][ordinal + 1] if ordinal + 1 < len(probe["timeline"]) else None})
    return result


def jpeg_rgb(ffmpeg, jpeg, width, height, deadline):
    require(jpeg.startswith(b"\xff\xd8") and jpeg.endswith(b"\xff\xd9"), "incomplete_jpeg_markers")
    jpeg_dimensions(jpeg, width, height)
    args = ["-hide_banner", "-nostdin", "-nostats", "-v", "error", "-xerror", "-max_alloc", "268435456",
            "-threads", "1", "-max_pixels", str(MAX_PIXELS), "-filter_threads", "1", "-filter_complex_threads", "1",
            "-protocol_whitelist", "pipe", "-f", "image2pipe", "-c:v", "mjpeg", "-i", "pipe:0",
            "-map", "0:v:0", "-an", "-sn", "-dn", "-frames:v", "2", "-fps_mode:v", "passthrough",
            "-c:v", "rawvideo", "-threads:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"]
    expected = width * height * 3
    pixels = run_tool(ffmpeg, args, input_bytes=jpeg, stdout_limit=expected + 1, deadline=deadline)
    require(len(pixels) == expected, "jpeg_decoded_dimensions_or_count")
    return pixels


def jpeg_dimensions(data, width, height):
    """Check the declared raster bound; actual decoding still follows."""
    offset, found = 2, False
    while offset < len(data):
        require(data[offset] == 0xFF, "invalid_jpeg_marker")
        while offset < len(data) and data[offset] == 0xFF:
            offset += 1
        require(offset < len(data), "truncated_jpeg_marker")
        marker = data[offset]
        offset += 1
        if marker == 0xDA:
            require(found, "jpeg_missing_frame_header")
            return
        require(marker not in (0, 0xD8, 0xD9) and not 0xD0 <= marker <= 0xD7, "invalid_jpeg_header_marker")
        if marker == 1:
            continue
        require(offset + 2 <= len(data), "truncated_jpeg_segment")
        length = struct.unpack_from(">H", data, offset)[0]
        require(length >= 2 and offset + length <= len(data), "jpeg_segment_bounds")
        if marker in (0xC0, 0xC1, 0xC2):
            require(not found and length >= 8, "ambiguous_jpeg_frame_header")
            precision, decoded_height, decoded_width, components = struct.unpack_from(">BHHB", data, offset + 2)
            require(precision == 8 and decoded_width == width and decoded_height == height and
                    components in (1, 3) and length == 8 + 3 * components, "jpeg_declared_dimensions")
            found = True
        elif 0xC0 <= marker <= 0xCF and marker not in (0xC4, 0xC8, 0xCC):
            raise Invalid("unsupported_jpeg_frame_header")
        offset += length
    raise Invalid("jpeg_missing_scan")


def source_rgb(ffmpeg, source, case, probe, selected, deadline):
    pts = selected["pts"]
    filters = probe["filters"] + "sidedata=mode=delete,select='eq(n," + str(selected["ordinal"]) + ")',showinfo@phase2_source=checksum=0,scale=" + \
        str(probe["width"]) + ":" + str(probe["height"]) + ":flags=area,setsar=1,format=rgb24"
    args = ["-hide_banner", "-nostdin", "-nostats", "-loglevel", "repeat+level+info", "-xerror", "-max_alloc", "268435456",
            "-copyts", "-fflags", "+nofillin-genpts", "-threads", "1", "-max_pixels", str(MAX_SOURCE_PIXELS),
            "-filter_threads", "1", "-filter_complex_threads", "1", "-noautorotate", "-reinit_filter", "0",
            "-protocol_whitelist", "file,pipe", "-format_whitelist", FORMATS, "-i", source.proc_path(),
            "-map", "0:" + str(case["source"]["video_stream_index"]), "-an", "-sn", "-dn",
            "-map_metadata", "-1", "-map_chapters", "-1", "-vf", filters, "-frames:v", "1",
            "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter", "-c:v", "rawvideo", "-threads:v", "1",
            "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"]
    expected = probe["width"] * probe["height"] * 3
    def audit(data):
        log = data.decode("utf-8", errors="replace")
        require(not any(level in log for level in ("[error]", "[fatal]", "[panic]")), "source_decoder_reported_error")
        prefix = r"\[showinfo@phase2_source @ [0-9a-fA-Fx]+\]\s+(?:\[info\]\s+)?"
        bases = re.findall(prefix + r"config in time_base: ([0-9]+/[0-9]+),", log)
        frames = re.findall(prefix + r"n:\s*([0-9]+)\s+pts:\s*(-?[0-9]+)\s+pts_time:", log)
        require(len(bases) == 1 and rational(bases[0]) == probe["base"] and frames == [("0", str(pts))],
                "source_rgb_pts_not_proven")
        # showinfo observes the selected frame after the declared orthogonal
        # transform and before scale/setsar. An unknown source remains 0/1;
        # square pixels are a display choice, not invented source metadata.
        frame_lines = [line for line in log.splitlines() if re.search(prefix + r"n:\s*[0-9]+\s+pts:", line)]
        require(len(frame_lines) == 1, "source_rgb_sar_not_proven")
        reported = re.findall(r"(?:^|\s)sar:([^\s]+)", frame_lines[0])
        require(len(reported) == 1, "source_rgb_sar_not_proven")
        if probe["source_sar_unknown"]:
            require(reported[0] == "0/1", "source_rgb_sar_not_proven")
        else:
            require(rational(reported[0]) == rational(probe["display_sar"]), "source_rgb_sar_not_proven")
    pixels = run_tool(ffmpeg, args, source, stdout_limit=expected + 1, deadline=deadline, audit_stderr=audit)
    require(len(pixels) == expected, "incomplete_source_rgb_frame")
    return pixels


def pixel_error(left, right):
    require(len(left) == len(right) and len(left) > 0, "rgb_shape_mismatch")
    histogram = [0] * 256
    total = 0
    for index, (a, b) in enumerate(zip(left, right)):
        if index % 65536 == 0:
            check_cancelled()
        difference = abs(a - b)
        histogram[difference] += 1
        total += difference
    threshold, cumulative, p95 = (len(left) * 95 + 99) // 100, 0, 0
    for p95, count in enumerate(histogram):
        cumulative += count
        if cumulative >= threshold:
            break
    return total / len(left), p95


def browser_frame(value, index, frame, jpeg, pixels, width, height, nominal, duration, thresholds, equivalents):
    object_fields(value, ("index", "jpeg_sha256", "rgba_sha256", "width", "height", "pixels", "visible",
                          "hover_seconds", "observed_index", "observed_jpeg_sha256", "observed_equivalent_indexes",
                          "tooltip_seconds"))
    require(value["index"] == index and value["visible"] is True, "hover_did_not_display_expected_frame")
    require(integer(value["width"], 1, 4096) == width and integer(value["height"], 1, 4096) == height,
            "browser_image_dimensions")
    observed_sha = digest(value["observed_jpeg_sha256"])
    require(observed_sha == digest(value["jpeg_sha256"]) == hashlib.sha256(jpeg).hexdigest(), "browser_jpeg_not_bif_slice")
    observed_equivalents = value["observed_equivalent_indexes"]
    require(isinstance(observed_equivalents, list) and len(observed_equivalents) <= MAX_FRAMES,
            "invalid_observed_equivalent_indexes")
    for candidate in observed_equivalents:
        integer(candidate, 0, len(nominal) - 1)
    require(observed_equivalents == equivalents and index in equivalents, "browser_jpeg_equivalence_mismatch")
    if len(equivalents) == 1:
        require(integer(value["observed_index"], 0, len(nominal) - 1) == equivalents[0], "browser_unique_index_mismatch")
    else:
        require(value["observed_index"] is None, "browser_ambiguous_index_must_be_null")
    digest(value["rgba_sha256"])
    hover_seconds = number(value["hover_seconds"], 0, duration / TICKS)
    hover = hover_seconds * TICKS
    tooltip = integer(value["tooltip_seconds"], 0, MAX_DURATION // TICKS)
    require(tooltip == math.floor(hover_seconds), "browser_tooltip_hover_mismatch")
    end = nominal[index + 1] if index + 1 < len(nominal) else duration
    require(frame["ticks"] <= hover < end and frame["ticks"] <= tooltip * TICKS < end, "hover_not_in_observed_slot")
    expected_points = {(0, 0), (width - 1, 0), (0, height - 1), (width - 1, height - 1), (width // 2, height // 2)}
    samples = value["pixels"]
    require(isinstance(samples, list) and len(samples) == len(expected_points), "browser_pixel_samples")
    seen = set()
    maximum = 0
    for sample in samples:
        object_fields(sample, ("x", "y", "rgba"))
        x, y = integer(sample["x"], 0, width - 1), integer(sample["y"], 0, height - 1)
        require((x, y) in expected_points and (x, y) not in seen, "browser_pixel_coordinates")
        seen.add((x, y))
        rgba = sample["rgba"]
        require(isinstance(rgba, list) and len(rgba) == 4, "browser_rgba_shape")
        for channel in rgba:
            integer(channel, 0, 255)
        require(rgba[3] == 255, "browser_jpeg_alpha")
        offset = (y * width + x) * 3
        maximum = max(maximum, *(abs(rgba[c] - pixels[offset + c]) for c in range(3)))
    require(maximum <= thresholds["max_rgb_p95_error"], "browser_decode_pixel_mismatch")
    return {"index": index, "jpeg_sha256": value["jpeg_sha256"], "rgba_sha256": value["rgba_sha256"],
            "observed_jpeg_sha256": observed_sha, "observed_equivalent_indexes": equivalents,
            "observed_index": value["observed_index"], "hover_seconds": hover_seconds, "tooltip_seconds": tooltip,
            "index_evidence": "unique_jpeg_match" if len(equivalents) == 1 else "byte_equivalence",
            "plugin_internal_index_observed": False, "sample_max_error": maximum, "hover_visible": True}


def evaluate_preview(case, observation, source, artifacts, ffmpeg, ffprobe, thresholds):
    if not case["preview"]["required"]:
        return {"state": "not_required"}
    if observation is None:
        raise Pending("preview_not_ready")
    require(isinstance(observation, dict), "invalid_preview_observation")
    if observation.get("status") in ("pending", "missing"):
        object_fields(observation, ("status",), ("bif_path", "bif_sha256", "width", "height", "nominal_ticks",
                                                "actual_ticks", "browser_frames", "http_evidence"))
        raise Pending("preview_not_ready")
    if observation.get("status") == "failed":
        raise Invalid("preview_failed")
    object_fields(observation, ("status", "bif_path", "bif_sha256", "width", "height", "nominal_ticks",
                                "browser_frames", "http_evidence"), ("actual_ticks",))
    require(observation["status"] == "ready", "preview_failed")
    artifacts.reference(observation["http_evidence"], pending=True)
    deadline = time.monotonic() + CASE_SECONDS
    with HeldFile(artifacts.path(observation["bif_path"]), MAX_BIF, digest(observation["bif_sha256"])) as bif:
        frames, multiplier = parse_bif(bif)
        require(bool(frames), "required_preview_is_empty")
        nominal = [frame["ticks"] for frame in frames]
        configured = case["preview"]["interval_ticks"]
        duration = case["source"]["duration_ticks"]
        bounded_interval = max(configured, ((duration + MAX_FRAMES * TICKS - 1) // (MAX_FRAMES * TICKS)) * TICKS)
        actual_interval = nominal[1] if len(nominal) > 1 else bounded_interval
        require(actual_interval == bounded_interval and multiplier * 10_000 == actual_interval and
                len(frames) == (duration - 1) // actual_interval + 1,
                "preview_sampling_plan_mismatch")
        require(isinstance(observation["nominal_ticks"], list) and
                all(type(value) is int for value in observation["nominal_ticks"]) and
                observation["nominal_ticks"] == nominal, "http_nominal_timeline_mismatch")
        probe = probe_source(ffprobe, source, case, deadline)
        width, height = probe["width"], probe["height"]
        require(integer(observation["width"], 1, 4096) == width and integer(observation["height"], 1, 4096) == height,
                "preview_display_geometry_mismatch")
        selected = selected_frames(probe, nominal, duration)
        if "actual_ticks" in observation:
            ticks = observation["actual_ticks"]
            require(isinstance(ticks, list) and len(ticks) == len(selected), "actual_tick_count")
            for index, (actual, expected) in enumerate(zip(ticks, selected)):
                actual = integer(actual, 0, duration - 1)
                require(index == 0 or actual >= ticks[index - 1], "unordered_actual_source_pts")
                require(actual == expected["actual_ticks"] and
                        abs(actual - expected["actual_ticks"]) <= case["preview"]["pts_tolerance_ticks"],
                        "actual_source_pts_mismatch")
        browser = observation["browser_frames"]
        require(isinstance(browser, list) and len(browser) <= MAX_FRAMES, "browser_frame_budget")
        browser_by_index = {}
        for value in browser:
            require(isinstance(value, dict), "invalid_browser_frame")
            index = integer(value.get("index"), 0, len(frames) - 1)
            require(index not in browser_by_index, "duplicate_browser_frame")
            browser_by_index[index] = value
        required = {0, len(frames) // 2, len(frames) - 1}
        if not required <= set(browser_by_index):
            raise Pending("missing_first_middle_last_hover")
        equivalent_indexes = {}
        for index, frame in enumerate(frames):
            frame["sha256"] = hashlib.sha256(bif.read(frame["size"], frame["offset"])).hexdigest()
            equivalent_indexes.setdefault(frame["sha256"], []).append(index)
        result = {"state": "passed", "bif_sha256": bif.sha256, "frame_count": len(frames),
                  "verification_profile": PREVIEW_PROFILE, "complete_source_decode_observed": probe["complete_decoded_eof"],
                  "geometry_profile": probe["geometry_profile"], "source_sar_unknown": probe["source_sar_unknown"],
                  "source_sar_report": probe["source_sar_report"], "source_frame_sar_reports": probe["source_frame_sar_reports"],
                  "display_sar": probe["display_sar"], "display_sar_policy": probe["display_sar_policy"],
                  "multiplier_millis": multiplier, "interval_ticks": actual_interval,
                  "interval_observed": len(frames) > 1,
                  "selection_policy_counts": {policy: sum(point["selection_policy"] == policy for point in selected)
                                               for policy in ("first_frame_hold", "preceding_until_next_source_pts",
                                                              "last_frame_hold_to_container_end")},
                  "decoded_jpeg_count": 0, "source_compared_count": 0, "source_unique_decode_count": 0, "frames": []}
        previous_ordinal, source_pixels = None, None
        # Every JPEG is really decoded. Source comparisons additionally cover
        # every browser-observed frame, including first, middle, and last.
        for index, frame in enumerate(frames):
            require(time.monotonic() < deadline, "case_deadline")
            jpeg = bif.read(frame["size"], frame["offset"])
            pixels = jpeg_rgb(ffmpeg, jpeg, width, height, deadline)
            result["decoded_jpeg_count"] += 1
            if index not in browser_by_index:
                continue
            point = selected[index]
            if point["ordinal"] != previous_ordinal:
                source_pixels = source_rgb(ffmpeg, source, case, probe, point, deadline)
                previous_ordinal = point["ordinal"]
                result["source_unique_decode_count"] += 1
            mae, p95 = pixel_error(pixels, source_pixels)
            require(mae <= thresholds["max_rgb_mae"] and p95 <= thresholds["max_rgb_p95_error"],
                    "jpeg_source_pixel_mismatch")
            captured = browser_frame(browser_by_index[index], index, frame, jpeg, pixels, width, height,
                                     nominal, duration, thresholds, equivalent_indexes[frame["sha256"]])
            captured.update({"nominal_ticks": frame["ticks"], "actual_ticks": point["actual_ticks"], "source_pts": point["pts"],
                             "source_ordinal": point["ordinal"], "selection_policy": point["selection_policy"],
                             "next_source_pts": point["next_source_pts"],
                             "source_time_base": str(probe["base"]), "rgb_mae": mae, "rgb_p95_error": p95,
                             "decoded_rgb_sha256": hashlib.sha256(pixels).hexdigest(),
                             "source_rgb_sha256": hashlib.sha256(source_pixels).hexdigest()})
            result["source_compared_count"] += 1
            result["frames"].append(captured)
        bif.check()
        return result


def evaluate_detection(case, observation, artifacts, by_id, groups, thresholds, result):
    if observation is None:
        raise Pending("detection_pending")
    require(isinstance(observation, dict), "invalid_detection_observation")
    if observation.get("status") == "pending":
        object_fields(observation, ("status",), ("support_case_ids", "reason", "evidence", "start_ticks", "end_ticks"))
        raise Pending("detection_pending")
    object_fields(observation, ("status", "support_case_ids", "reason", "evidence"), ("start_ticks", "end_ticks"))
    status = observation["status"]
    require(status in ("published", "abstained", "miss", "failed"), "invalid_detection_status")
    text(observation["reason"])
    supports = unique_strings(observation["support_case_ids"], MAX_CASES, set(by_id))
    if status == "failed":
        raise Invalid("detection_failed")
    expected = case["expected"]
    positive = expected["kind"] == "positive"
    if status != "published":
        require("start_ticks" not in observation and "end_ticks" not in observation, "unpublished_detection_has_interval")
        if status == "abstained":
            result["counts"]["abstention"] = 1
        if positive:
            result["counts"]["miss"] = 1
        result["classification"] = "positive_abstention" if positive and status == "abstained" else \
            "miss" if positive else "negative_no_publication"
        artifacts.reference(observation["evidence"], pending=True)
        return result
    start, end = interval({"start_ticks": observation.get("start_ticks"), "end_ticks": observation.get("end_ticks")},
                          case["source"]["duration_ticks"])
    result["observed_interval"] = {"start_ticks": start, "end_ticks": end}
    if not positive:
        result["counts"]["false_positive"] = 1
        result["classification"] = "false_positive"
    else:
        expected_start, expected_end = interval(expected["intro"], case["source"]["duration_ticks"])
        start_error, end_error = abs(start - expected_start), abs(end - expected_end)
        result["boundary_error_ticks"] = {"start": start_error, "end": end_error}
        if start_error > expected["start_tolerance_ticks"] or end_error > expected["end_tolerance_ticks"]:
            result["counts"]["boundary"] = 1
        safe_start, safe_end = interval(expected["safe"], case["source"]["duration_ticks"])
        if start < safe_start or end > safe_end:
            result["counts"]["narrative_safety"] = 1
        result["classification"] = "positive_published"
    for narrative in expected["narrative_intervals"]:
        a, b = interval(narrative, case["source"]["duration_ticks"])
        if max(start, a) < min(end, b):
            result["counts"]["narrative_safety"] = 1
    if positive:
        for support in supports:
            peer = by_id[support]
            require(peer.get("evaluation_role") == case.get("evaluation_role"), "support_outside_evaluation_role")
            require((peer["series_id"], peer["season_id"], peer["split"]) ==
                    (case["series_id"], case["season_id"], case["split"]), "support_outside_frozen_cohort")
            require(peer["expected"]["kind"] == "positive", "negative_case_used_as_positive_support")
        independent = {groups[value] for value in supports} | {groups[case["case_id"]]}
        result["independent_support_count"] = len(independent)
        require(len(independent) >= thresholds["min_independent_positive_episodes"], "insufficient_independent_support")
    artifacts.reference(observation["evidence"], pending=True)
    return result


def population_gates(manifest, cases, groups):
    accepted = [row for row in cases if row["state"] == "passed"]
    required = manifest["thresholds"]
    positives = {groups[row["case_id"]] for row in accepted if row["expected"] == "positive"}
    specs = [("independent_positive_episodes", len(positives), required["min_independent_positive_episodes"])]
    if manifest["schema_version"] == 1:
        populations = [("holdout_" + kind, "split", "holdout", kind, "min_holdout_" + kind + "_cases")
                       for kind in ("positive", "negative")]
    else:
        populations = [("fresh_holdout_positive", "evaluation_role", "fresh_holdout", "positive",
                        "min_fresh_holdout_positive_cases"),
                       ("regression_negative", "evaluation_role", "regression", "negative",
                        "min_regression_negative_cases")]
    for name, field, population, kind, threshold in populations:
        count = len({groups[row["case_id"]] for row in accepted
                     if row[field] == population and row["expected"] == kind})
        specs.append((name, count, required[threshold]))
    return [{"name": name, "count": count, "minimum": minimum,
             "state": "passed" if count >= minimum else "pending"} for name, count, minimum in specs]


def evaluate(manifest, manifest_sha, observations, artifacts):
    by_id, required_categories = validate_manifest(manifest, artifacts)
    object_fields(observations, ("schema_version", "manifest_sha256", "run_id", "captured_at", "origin", "cases"))
    require(type(observations["schema_version"]) is int and observations["schema_version"] == 1 and
            digest(observations["manifest_sha256"]) == manifest_sha,
            "observation_manifest_mismatch")
    identifier(observations["run_id"])
    text(observations["captured_at"], 64)
    require(observations["origin"] == "real_http", "observation_not_real_http")
    require(isinstance(observations["cases"], list) and len(observations["cases"]) <= MAX_CASES, "observation_case_budget")
    observed = {}
    for row in observations["cases"]:
        object_fields(row, ("case_id",), ("task", "source_before", "source_after", "processed_source", "detection", "preview"))
        case_id = row["case_id"]
        require(case_id in by_id and case_id not in observed, "unknown_or_duplicate_observation_case")
        observed[case_id] = row
    label_methods = sorted({case["provenance"]["label_method"] for case in by_id.values()})
    scopes = {"series": "independent_new_series", "season": "independent_new_seasons",
              "episode": "independent_new_episodes"}
    report = {"schema_version": manifest["schema_version"], "manifest_sha256": manifest_sha, "corpus_id": manifest["corpus_id"],
              "label_revision_sha256": hashlib.sha256(manifest["label_revision"].encode()).hexdigest(),
              "label_method": label_methods[0] if len(label_methods) == 1 else "mixed",
              "label_methods": label_methods, "human_reviewed": label_methods == ["human_review"],
              "split_unit": manifest["split_unit"], "scope": scopes[manifest["split_unit"]],
              "runtime_hierarchy_role": "test_indexing_container",
              "generalization_exclusions": ["new_series_generalization", "new_season_generalization"]
                                             if manifest["split_unit"] == "episode" else
                                             ["new_series_generalization"] if manifest["split_unit"] == "season" else [],
              "run_id": observations["run_id"], "state": "pending",
              "mechanical_coverage_exclusions": ["source_replacement_fault_injection", "cancellation_fault_injection",
                                                   "authorization_revocation", "http_range_and_cache_contracts"],
              "cases": [], "counts": {key: 0 for key in COUNTS}, "category_counts": {}, "gates": [], "global_failures": []}
    if manifest["schema_version"] == 2:
        report.update(scope="mixed_calibration_regression_fresh_holdout",
                      fresh_holdout_scope=scopes[manifest["split_unit"]],
                      consumer_case_id=manifest["consumer_case_id"],
                      original_attempt_refs=[{"run_id": entry["run_id"], "case_ids": entry["case_ids"],
                          **{name + "_sha256": entry[name]["sha256"] for name in ("manifest", "labels", "receipt")}}
                          for entry in manifest["original_attempt_refs"]], evaluation_role_counts={})
        report["generalization_exclusions"].append("fresh_holdout_negative_specificity")
        report["fresh_holdout_negative_coverage"] = {"state": "not_covered", "count": 0,
            "reason": "No fresh-negative acceptance gate is declared; regression negatives do not establish fresh specificity."}
    snapshots, source_errors, verified_run_sources = {}, {}, set()
    # Hash admission before determining independence, so hard links and inode
    # aliases cannot count as independent episodes under different labels.
    for case_id, case in by_id.items():
        try:
            expected = case["source"]
            with HeldFile(expected["path"], MAX_SOURCE_BYTES, expected["sha256"], expected["size_bytes"]) as source:
                snapshots[case_id] = source.snapshot()
                source.check()
        except (Invalid, OSError) as error:
            if CANCEL_REQUESTED:
                raise Invalid("verification_cancelled") from error
            source_errors[case_id] = str(error) if isinstance(error, Invalid) else "source_unavailable"
    groups = independent_groups(list(by_id.values()), snapshots)
    check_group_splits(by_id, groups, manifest.get("consumer_case_id"))
    with ExitStack() as stack:
        toolset, held_sources, held_processed = {}, {}, {}
        for name, expected in manifest["tools"].items():
            toolset[name] = stack.enter_context(HeldFile(expected["path"], 1 << 30, expected["sha256"]))
            require(os.fstat(toolset[name].fd).st_mode & 0o111, "pinned_tool_not_executable")
        for case_id, case in by_id.items():
            if CANCEL_REQUESTED:
                report["global_failures"].append("verification_cancelled")
                break
            row = observed.get(case_id)
            result = {"case_id": case_id, "source_sha256": case["source"]["sha256"], "split": case["split"],
                      "label_method": case["provenance"]["label_method"],
                      "human_reviewed": case["provenance"]["label_method"] == "human_review",
                      "original_season_unknown": case["season_id"] == "unknown",
                      "categories": case["categories"], "expected": case["expected"]["kind"], "state": "pending",
                      "classification": "pending", "counts": {key: 0 for key in COUNTS}}
            if manifest["schema_version"] == 2:
                result["evaluation_role"] = case["evaluation_role"]
            source, processed = None, None
            try:
                if case_id in source_errors:
                    raise Invalid(source_errors[case_id])
                source = stack.enter_context(HeldFile(case["source"]["path"], MAX_SOURCE_BYTES,
                                                      case["source"]["sha256"], case["source"]["size_bytes"]))
                held_sources[case_id] = source
                require(source.snapshot() == snapshots[case_id], "source_changed_since_admission")
                if row is None:
                    raise Pending("missing_case_observation")
                evaluate_detection(case, row.get("detection"), artifacts, by_id, groups, manifest["thresholds"], result)
                observation_snapshot(row.get("source_before"), source)
                observation_snapshot(row.get("source_after"), source)
                processed = processed_source(row.get("processed_source"), case, row, artifacts, stack)
                held_processed[case_id] = processed
                verified_run_sources.add(case_id)
                task = row.get("task")
                if task is None:
                    raise Pending("task_not_terminal")
                require(isinstance(task, dict), "invalid_task_observation")
                if task.get("status") == "pending":
                    object_fields(task, ("status",), ("run_id", "http_evidence"))
                    raise Pending("task_not_terminal")
                object_fields(task, ("run_id", "status", "http_evidence"))
                identifier(task["run_id"])
                require(task["status"] in ("succeeded", "failed", "cancelled"), "invalid_task_status")
                artifacts.reference(task["http_evidence"], pending=True)
                require(task["status"] == "succeeded", "task_did_not_succeed")
                result["preview"] = evaluate_preview(case, row.get("preview"), processed, artifacts,
                                                     toolset["ffmpeg"], toolset["ffprobe"], manifest["thresholds"])
                result["state"] = "failed" if any(result["counts"][key] for key in
                                                    ("false_positive", "miss", "boundary", "narrative_safety")) else "passed"
            except Pending as error:
                result["state"] = "pending"
                result["reason_code"] = str(error)
                result["counts"]["pending"] = 1
            except (Invalid, OSError, ValueError, TypeError, KeyError, AttributeError, OverflowError) as error:
                result["state"] = "failed"
                result["reason_code"] = str(error) if isinstance(error, Invalid) else "unavailable_or_malformed_evidence"
                result["counts"]["failure"] = 1
            finally:
                for held in (source, processed):
                    if held is None:
                        continue
                    try:
                        held.check()
                    except (Invalid, OSError):
                        result["state"] = "failed"
                        result["reason_code"] = "verification_cancelled" if CANCEL_REQUESTED else "source_changed_during_verification"
                        result["counts"]["failure"] = 1
                        verified_run_sources.discard(case_id)
            if any(result["counts"][key] for key in ("false_positive", "miss", "boundary", "narrative_safety", "failure")):
                result["state"] = "failed"
            report["cases"].append(result)
        # Keep both the original and the actual application input descriptors
        # alive until every case has finished, then rehash each before closing.
        for result in report["cases"]:
            case_id = result["case_id"]
            for held in (held_sources.get(case_id), held_processed.get(case_id)):
                if held is None:
                    continue
                try:
                    held.check()
                except (Invalid, OSError):
                    verified_run_sources.discard(case_id)
                    result["state"] = "failed"
                    result["reason_code"] = "verification_cancelled" if CANCEL_REQUESTED else "source_changed_during_verification"
                    result["counts"]["failure"] = 1
        for tool in toolset.values():
            try:
                tool.check()
            except (Invalid, OSError):
                report["global_failures"].append("verification_cancelled" if CANCEL_REQUESTED else "pinned_tool_changed_during_verification")
    for result in report["cases"]:
        if result["state"] != "passed" or result["classification"] != "positive_published":
            continue
        supports = observed[result["case_id"]]["detection"]["support_case_ids"]
        if not set(supports) <= verified_run_sources:
            result["state"] = "pending"
            result["reason_code"] = "support_source_evidence_incomplete"
            result["counts"]["pending"] = 1
    for result in report["cases"]:
        for key in COUNTS:
            report["counts"][key] += result["counts"][key]
        for category in result["categories"]:
            counters = report["category_counts"].setdefault(category, dict.fromkeys(("cases", "passed", "failed", *COUNTS), 0))
            counters["cases"] += 1
            if result["state"] in ("passed", "failed"):
                counters[result["state"]] += 1
            for key in COUNTS:
                counters[key] += result["counts"][key]
        if manifest["schema_version"] == 2:
            counters = report["evaluation_role_counts"].setdefault(result["evaluation_role"],
                dict.fromkeys(("cases", "passed", "failed", *COUNTS), 0))
            counters["cases"] += 1
            if result["state"] in ("passed", "failed"):
                counters[result["state"]] += 1
            for key in COUNTS:
                counters[key] += result["counts"][key]
    accepted = [result for result in report["cases"] if result["state"] == "passed"]
    report["gates"].extend(population_gates(manifest, report["cases"], groups))
    if manifest["schema_version"] == 2:
        report["fresh_holdout_negative_coverage"]["count"] = len({groups[row["case_id"]] for row in accepted
            if row["evaluation_role"] == "fresh_holdout" and row["expected"] == "negative"})
    for category in required_categories:
        count = 0 if category == "source_replacement" else sum(category in row["categories"] for row in accepted)
        gate = {"name": "category_" + category, "count": count, "minimum": 1,
                "state": "passed" if count else "pending"}
        if category == "source_replacement":
            gate["reason_code"] = "mechanical_coverage_out_of_scope"
        report["gates"].append(gate)
    failures = bool(report["global_failures"]) or any(row["state"] == "failed" for row in report["cases"])
    incomplete = any(row["state"] == "pending" for row in report["cases"] + report["gates"])
    report["state"] = "failed" if failures else "pending" if incomplete else "passed"
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--observations")
    parser.add_argument("--output", required=True)
    parser.add_argument("--admit-only", action="store_true")
    options = parser.parse_args()
    signal.signal(signal.SIGTERM, request_cancel)
    signal.signal(signal.SIGINT, request_cancel)
    try:
        require(sys.platform == "linux" and Path("/proc/self/fd").is_dir(), "linux_descriptor_runtime_required")
        require((options.observations is None) == options.admit_only, "invalid_cli_mode")
        manifest, manifest_sha = load_json(options.manifest)
        require(isinstance(manifest, dict), "invalid_manifest")
        artifacts = PrivateArtifacts(manifest.get("artifacts_root"))
        artifacts.path(options.manifest)
        if options.admit_only:
            report = admit(manifest, manifest_sha, artifacts)
            artifacts.write_result(options.output, report)
            print(json.dumps({"admitted": True, "manifest_sha256": manifest_sha}, sort_keys=True))
            return 0
        observations, observation_sha = load_json(artifacts.path(options.observations))
        report = evaluate(manifest, manifest_sha, observations, artifacts)
        report["observations_sha256"] = observation_sha
        artifacts.write_result(options.output, report)
        print(json.dumps({"state": report["state"], "manifest_sha256": manifest_sha,
                          "counts": report["counts"]}, sort_keys=True))
        return 0 if report["state"] == "passed" else 2 if report["state"] == "pending" else 1
    except (Invalid, OSError, ValueError, TypeError, KeyError, AttributeError, OverflowError) as error:
        code = str(error) if isinstance(error, Invalid) else "input_or_runtime_unavailable"
        print(json.dumps({"state": "failed", "reason_code": code}, sort_keys=True))
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
