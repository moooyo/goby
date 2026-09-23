#!/usr/bin/env python3
"""Bounded, native Phase 3 compound workload. Importing performs no I/O.

This is a guest-local workload actor, not a VM or fault controller. A fresh,
reviewed manifest and private controller context are mandatory. Every acceptance
observation comes from production HTTP, PostgreSQL, procfs, or actual media
decoding. Missing observations and missing overlap fail closed.
"""

import argparse
import concurrent.futures
from datetime import datetime
from fractions import Fraction
import hashlib
import http.client
import json
import math
import os
from pathlib import Path
import re
import select
import selectors
import signal
import socket
import stat
import subprocess
import sys
import threading
import time
from urllib.parse import urlencode, urlsplit, urlunsplit, parse_qsl


VERSION = 1
CONTEXT_VERSION = 2
PHASES = ("cold", "cached", "incremental")
QUERY_KINDS = {"unicode", "filter", "exact_total", "shallow", "deep", "resume", "latest"}
PLAY_MODES = {"direct", "remux", "transcode"}
POOL_GAUGES = ("MaxConns", "TotalConns", "IdleConns", "AcquiredConns", "ConstructingConns")
POOL_COUNTERS = ("AcquireCount", "AcquireDurationNanoseconds", "EmptyAcquireCount", "EmptyAcquireWaitNanoseconds", "CanceledAcquireCount")
MEDIA_EXTENSIONS = {".mp4", ".mkv", ".mov", ".webm", ".avi", ".ts", ".m4v", ".mp3", ".flac", ".m4a", ".aac", ".ogg", ".wav"}
TERMINAL = {"completed", "failed", "cancelled", "interrupted"}
SAFE = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,95}\Z")


class Failure(RuntimeError):
    """Only bounded machine codes enter public evidence."""


def need(condition, code):
    if not condition:
        raise Failure(code)


def exact(value, keys, code):
    need(type(value) is dict and set(value) == set(keys.split()), code)


def playback_client_headers(client):
    """Use the immutable native credential belonging to one playback lane."""
    exact(client, "emby_token auth_session_id device_id", "playback_client_fields")
    token = client["emby_token"]
    need(type(token) is str and 0 < len(token) <= 4096 and
         not any(character in token for character in "\r\n\x00"), "playback_client_token")
    need(all(type(client[key]) is str and SAFE.fullmatch(client[key])
             for key in ("auth_session_id", "device_id")), "playback_client_identity")
    return {"X-Emby-Token": token, "X-Emby-Authorization":
            'MediaBrowser Client="Phase3", Device="Linux", DeviceId="%s", Version="1"' % client["device_id"]}


def validate_playback_clients(clients, primary_token, primary_device):
    exact(clients, "direct remux transcode", "playback_client_modes")
    for client in clients.values():
        playback_client_headers(client)
    need(all(len({client[key] for client in clients.values()}) == 3
             for key in ("emby_token", "auth_session_id", "device_id")), "distinct_playback_clients_required")
    need(clients["direct"]["emby_token"] == primary_token and clients["direct"]["device_id"] == primary_device,
         "primary_direct_client_binding")
    return clients


def integer(value, lower, upper, code):
    need(type(value) is int and lower <= value <= upper, code)
    return value


def number(value, lower, upper, code):
    need(type(value) in (int, float) and math.isfinite(value) and lower <= value <= upper, code)
    return value


def digest(data):
    return hashlib.sha256(data).hexdigest()


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=True, allow_nan=False, sort_keys=True, separators=(",", ":")) + "\n").encode()


def strict_json(data):
    def pairs(entries):
        result = {}
        for key, value in entries:
            need(key not in result, "json_duplicate_key")
            result[key] = value
        return result
    try:
        value = json.loads(data.decode("utf-8"), object_pairs_hook=pairs,
                           parse_constant=lambda _: (_ for _ in ()).throw(Failure("json_nonfinite")))
        pending, visited = [(value, 0)], 0
        while pending:
            node, depth = pending.pop()
            visited += 1
            need(depth <= 64 and visited <= 1000000, "json_structure_budget")
            if type(node) is str:
                need(not any(0xd800 <= ord(ch) <= 0xdfff for ch in node), "json_unpaired_surrogate")
            elif type(node) is dict:
                pending.extend((child, depth + 1) for pair in node.items() for child in pair)
            elif type(node) is list:
                pending.extend((child, depth + 1) for child in node)
        return value
    except (ValueError, UnicodeError, RecursionError):
        raise Failure("json_invalid") from None


def read_private(path, maximum=16 << 20):
    path = Path(path)
    need(path.is_absolute() and path.resolve() == path, "private_path")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        need(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid() and info.st_nlink == 1
             and stat.S_IMODE(info.st_mode) == 0o600 and 0 < info.st_size <= maximum, "private_identity")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            data = stream.read(maximum + 1)
        need(len(data) == info.st_size, "private_size_changed")
        return data
    finally:
        os.close(fd)


def percentile(values, fraction):
    """Nearest rank; empty distributions remain absent, never zero latency."""
    if not values:
        return None
    ordered = sorted(values)
    return ordered[max(0, math.ceil(fraction * len(ordered)) - 1)]


def distribution(values):
    return {"count": len(values), "min": min(values) if values else None,
            "p50": percentile(values, .50), "p95": percentile(values, .95),
            "p99": percentile(values, .99), "max": max(values) if values else None}


def cpu_usage_sample(group):
    """Bind one cumulative counter to the clock interval of its own read."""
    path = Path(group) / "cpu.stat"
    started = time.monotonic_ns()
    raw = path.read_text()
    ended = time.monotonic_ns()
    integer(started, 0, 1 << 63, "cpu_sample_clock")
    integer(ended, started, 1 << 63, "cpu_sample_clock")
    rows = [line.split() for line in raw.splitlines()]
    need(all(len(row) == 2 for row in rows) and len({row[0] for row in rows}) == len(rows), "cpu_stat_shape")
    usage = dict(rows).get("usage_usec")
    need(type(usage) is str and re.fullmatch(r"[0-9]+", usage), "cpu_usage_counter")
    return {"cpu_usage_usec": integer(int(usage), 0, 1 << 63, "cpu_usage_counter"),
            "cpu_sample_start_ns": started, "cpu_sample_end_ns": ended,
            "cpu_sample_at_ns": (started + ended) // 2}


def service_cpu_percent(previous, current):
    """Sum each service's rate over its own consecutive counter-read intervals.

    The service windows are slightly staggered, not an atomic VM-wide sample.
    Keep their raw read boundaries in evidence; never use an earlier broker
    timestamp, clamp a high rate, or turn a reset into zero utilization.
    """
    need(type(current) is dict and set(current) == {"goby", "postgres"}, "cpu_service_scope")
    need(previous is None or type(previous) is dict and set(previous) == set(current), "cpu_service_scope")
    for samples in ((current,) if previous is None else (previous, current)):
        for sample in samples.values():
            integer(sample["cpu_usage_usec"], 0, 1 << 63, "cpu_usage_counter")
            started = integer(sample["cpu_sample_start_ns"], 0, 1 << 63, "cpu_sample_clock")
            ended = integer(sample["cpu_sample_end_ns"], started, 1 << 63, "cpu_sample_clock")
            need(type(sample["cpu_sample_at_ns"]) is int and
                 sample["cpu_sample_at_ns"] == (started + ended) // 2, "cpu_sample_clock")
    if previous is None:
        return None
    total = Fraction(0)
    for name, sample in current.items():
        before = previous[name]
        elapsed = sample["cpu_sample_at_ns"] - before["cpu_sample_at_ns"]
        need(elapsed > 0 and sample["cpu_sample_start_ns"] >= before["cpu_sample_end_ns"], "cpu_sample_interval")
        delta = sample["cpu_usage_usec"] - before["cpu_usage_usec"]
        need(delta >= 0, "cpu_counter_regressed")
        total += Fraction(100000 * delta, elapsed)
    return float(total)


def http_exception_details(error):
    """Retain a bounded exception type/errno, never exception text or arguments."""
    kind = type(error)
    name = kind.__name__
    known = kind is Failure or kind.__module__ in ("builtins", "http.client", "socket")
    if not known or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]{0,63}", name):
        name = "OtherException"
    try:
        error_number = getattr(error, "errno", None)
    except Exception:
        error_number = None
    if type(error_number) is not int or not -(1 << 31) <= error_number < 1 << 31:
        error_number = None
    return {"exception_type": name, "errno": error_number}


def progressive_request(path, mode, requested_ticks):
    parts = urlsplit(path)
    query = dict(parse_qsl(parts.query, keep_blank_values=True))
    query["StartTimeTicks"] = str(requested_ticks)
    if mode == "remux":
        need(query.get("VideoCodec") == query.get("AudioCodec") == "copy", "remux_copy_request_required")
        if requested_ticks:
            query["AllowVideoSeekAlignment"] = "true"
    return urlunsplit(("", "", parts.path, urlencode(query), ""))


def progressive_start(headers, requested_ticks, allow_alignment):
    values = {key.lower(): value for key, value in headers.items()}
    raw = values.get("x-goby-start-time-ticks", "")
    need(type(raw) is str and re.fullmatch(r"0|[1-9][0-9]*", raw), "progressive_actual_start_missing")
    actual = integer(int(raw), 0, requested_ticks, "progressive_actual_start_bounds")
    aligned = actual != requested_ticks
    need(values.get("x-goby-seek-aligned", "") == ("true" if aligned else "") and
         (not aligned or allow_alignment and requested_ticks - actual <= 100000000), "progressive_alignment_contract")
    return actual


def remux_seek_contract(plan, requested_ticks, actual_ticks):
    """Read completed-job preparation data; it never replaces runtime copy proof."""
    need(type(plan.get("StartTicks")) is int and plan["StartTicks"] == actual_ticks and
         plan.get("VideoCodec") == plan.get("AudioCodec") == "copy" and
         type(plan.get("VideoStreamIndex")) is int and plan["VideoStreamIndex"] >= 0 and
         type(plan.get("AudioStreamIndex")) is int and plan["AudioStreamIndex"] >= 0, "remux_seek_plan")
    copy_timestamps = plan.get("CopyTimestamps", False)
    need(type(copy_timestamps) is bool and (actual_ticks == requested_ticks or copy_timestamps), "remux_seek_clock_contract")
    raw = plan.get("VideoCopySeekCandidate", "")
    need(type(raw) is str and 0 < len(raw.encode("utf-8")) <= 8192, "remux_seek_candidate_missing")
    candidate = strict_json(raw.encode("utf-8"))
    need(type(candidate) is dict, "remux_seek_joint_candidate")
    original = candidate.get("original_requested_start_ticks", 0)
    need(type(candidate.get("version")) is int and candidate["version"] == 1 and type(candidate.get("requested_start_ticks")) is int and
         candidate["requested_start_ticks"] == actual_ticks and type(original) is int and
         original == (requested_ticks if actual_ticks != requested_ticks else 0) and
         candidate.get("copy_timestamps", False) is copy_timestamps, "remux_seek_request_binding")
    index, audio = candidate.get("index"), candidate.get("audio")
    need(type(index) is dict and index.get("stream_index") == plan["VideoStreamIndex"] and
         type(index.get("entries")) is list and len(index["entries"]) == 1 and
         type(audio) is dict and audio.get("stream_index") == plan["AudioStreamIndex"] and
         audio.get("codec") == "aac", "remux_seek_joint_candidate")
    exact(audio, "stream_index codec time_base_numerator time_base_denominator sample_rate channels pts duration packet_sha256",
          "remux_seek_audio_proof")
    for key, lower, upper in (("stream_index", 0, (1 << 63) - 1), ("time_base_numerator", 1, (1 << 63) - 1),
        ("time_base_denominator", 1, (1 << 63) - 1), ("sample_rate", 8000, 192000), ("channels", 1, 8),
        ("pts", -(1 << 63) + 1, (1 << 63) - 1), ("duration", 1, (1 << 63) - 1)):
        integer(audio[key], lower, upper, "remux_seek_audio_proof")
    need(type(audio["packet_sha256"]) is str and re.fullmatch(r"[0-9a-f]{64}", audio["packet_sha256"]),
         "remux_seek_audio_proof")
    audio_clock = Fraction(audio["time_base_numerator"], audio["time_base_denominator"])
    need(audio["duration"] * audio_clock <= Fraction(1, 2), "remux_seek_audio_proof")
    point = index["entries"][0]
    need(type(point) is dict and type(point.get("audio")) is list and 0 < len(point["audio"]) <= 32,
         "remux_seek_audio_binding")
    matches = [proof for proof in point["audio"] if type(proof) is dict and proof.get("stream_index") == audio["stream_index"]]
    # Canonical JSON keeps bools distinct from integers when binding the full proof.
    need(len(matches) == 1 and json_bytes(matches[0]) == json_bytes(audio), "remux_seek_audio_binding")
    for key in ("time_base_numerator", "time_base_denominator"):
        integer(index.get(key), 1, (1 << 63) - 1, "remux_seek_joint_clock")
    integer(point.get("pts"), -(1 << 63) + 1, (1 << 63) - 1, "remux_seek_joint_clock")
    need(type(point.get("dts")) is int and point["pts"] == point["dts"] and
         audio["pts"] * audio_clock == point["pts"] * Fraction(index["time_base_numerator"], index["time_base_denominator"]),
         "remux_seek_joint_clock")
    return copy_timestamps, audio["packet_sha256"]


def copied_packet_start(probe, expected_ticks, kind, expected_audio_sha256=None):
    streams, packets = probe.get("streams"), probe.get("packets")
    need(type(streams) is list and len(streams) == 1 and streams[0].get("codec_type") == kind and
         type(packets) is list and 0 < len(packets) <= 64, "copied_packet_observation_missing")
    stream, packet = streams[0], packets[0]
    clock = stream.get("time_base", "")
    need(type(clock) is str and re.fullmatch(r"[1-9][0-9]*/[1-9][0-9]*", clock), "copied_packet_clock_missing")
    need(type(packet.get("pts")) is int and type(packet.get("dts")) is int and packet["pts"] == packet["dts"] and
         packet.get("stream_index") == stream.get("index"), "copied_packet_restart_timestamp")
    position = packet["pts"] * Fraction(clock) * 10000000
    # Aligned native timestamps may round down by less than one public tick.
    need(Fraction(expected_ticks) <= position < expected_ticks + 1, "copied_packet_source_clock_mismatch")
    result = {"stream_index": stream["index"], "pts": packet["pts"], "dts": packet["dts"], "time_base": clock,
              "position_ticks_numerator": position.numerator, "position_ticks_denominator": position.denominator}
    if kind == "audio":
        need(type(expected_audio_sha256) is str and re.fullmatch(r"[0-9a-f]{64}", expected_audio_sha256),
             "copied_audio_expected_hash")
        payload_hash = packet.get("data_hash")
        need(type(payload_hash) is str and re.fullmatch(r"SHA256:[0-9a-fA-F]{64}", payload_hash), "copied_audio_hash_missing")
        actual_hash = payload_hash[7:].lower()
        need(actual_hash == expected_audio_sha256, "copied_audio_hash_mismatch")
        result.update(packet_sha256=actual_hash, expected_packet_sha256=expected_audio_sha256)
    return result


def progressive_frame_arguments(output, requested_ticks, copy_timestamps):
    inputs, filters = ["-i", output], "scale=64:36"
    if copy_timestamps:
        inputs = ["-copyts", *inputs]
        seconds, ticks = divmod(requested_ticks, 10000000)
        filters = "select=gte(t\\,%d.%07d)," % (seconds, ticks) + filters
    return ["-v", "error", "-nostdin", "-threads", "1", *inputs, "-map", "0:v:0", "-an", "-sn",
            "-frames:v", "1", "-vf", filters, "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"]


def interval_overlap(left, right):
    return max(0, min(left[1], right[1]) - max(left[0], right[0]))


def confirmed_intervals(samples, maximum_gap_ns):
    """Only adjacent running observations of the SAME identity bound work.

    A queued task, a terminal timestamp, one running observation, or separated
    observations with a collection gap cannot manufacture an active interval.
    """
    result = []
    previous = None
    for sample in sorted(samples, key=lambda value: value[0]):
        at, identity, active = sample
        if previous and active and previous[2] and identity == previous[1] and 0 < at - previous[0] <= maximum_gap_ns:
            result.append((previous[0], at))
        previous = sample
    return result


def union_length(intervals):
    total, start, end = 0, None, None
    for lower, upper in sorted(intervals):
        if upper <= lower:
            continue
        if start is None:
            start, end = lower, upper
        elif lower <= end:
            end = max(end, upper)
        else:
            total += end - start
            start, end = lower, upper
    return total + (0 if start is None else end - start)


def all_overlap(groups):
    """Exact intersection of interval unions; pairwise overlap is insufficient."""
    need(bool(groups), "overlap_empty_groups")
    def merged(intervals):
        result = []
        for start, end in sorted(intervals):
            if end <= start:
                continue
            if result and start <= result[-1][1]:
                result[-1] = (result[-1][0], max(result[-1][1], end))
            else:
                result.append((start, end))
        return result
    result = merged(groups[0])
    for group in groups[1:]:
        other, overlap, left, right = merged(group), [], 0, 0
        while left < len(result) and right < len(other):
            a, b = result[left]
            c, d = other[right]
            if max(a, c) < min(b, d):
                overlap.append((max(a, c), min(b, d)))
            if b <= d:
                left += 1
            else:
                right += 1
        result = overlap
    return union_length(result)


def media_process_kind(arguments):
    if "s16le" in arguments:
        return "intro"
    filters = arguments[arguments.index("-vf") + 1] if "-vf" in arguments and arguments.index("-vf") + 1 < len(arguments) else ""
    if "showinfo@analysis_source" in filters:
        return "intro" if "trim=end=" in filters else "previews"
    return "playback"


def process_observer_arguments(observer):
    """Validate the only privileged argv; never accept a command template."""
    exact(observer, "argv binding_sha256 source_sha256", "process_observer_fields")
    arguments = observer["argv"]
    need(type(arguments) is list and len(arguments) == 10 and all(type(value) is str and len(value) <= 2048 for value in arguments), "process_observer_argv")
    need(arguments[:6] == ["/usr/bin/sudo", "-n", "/usr/bin/python3.13", "-I", "-B", "/opt/goby-phase3-runtime-setup-20260922-01/process-observer.py"]
         and arguments[6] == "--binding" and arguments[8] == "--binding-sha256"
         and re.fullmatch(r"/var/lib/goby-phase3/[a-z0-9][a-z0-9-]{0,31}/control/(?:cases/[A-Za-z0-9][A-Za-z0-9_.-]{0,95}/)?process-observer.json", arguments[7])
         and arguments[9] == observer["binding_sha256"] and re.fullmatch(r"[0-9a-f]{64}", observer["binding_sha256"])
         and re.fullmatch(r"[0-9a-f]{64}", observer["source_sha256"]), "process_observer_fixed_scope")
    return arguments


def database_pool_values(value):
    """Decode exact wire integers without losing cumulative nanoseconds."""
    exact(value, " ".join(POOL_GAUGES + POOL_COUNTERS), "database_pool_fields")
    result = {}
    for name in POOL_GAUGES:
        result[name] = integer(value[name], 0 if name != "MaxConns" else 1, (1 << 31) - 1, "database_pool_gauge")
    for name in POOL_COUNTERS:
        need(type(value[name]) is str and re.fullmatch(r"0|[1-9][0-9]{0,18}", value[name]), "database_pool_counter_wire")
        result[name] = integer(int(value[name]), 0, (1 << 63) - 1, "database_pool_counter_range")
    need(result["TotalConns"] <= result["MaxConns"]
         and all(result[name] <= result["TotalConns"] for name in ("IdleConns", "AcquiredConns", "ConstructingConns")), "database_pool_snapshot_bounds")
    return result


def database_pool_summary(samples):
    """No samples is unavailable, never a zero-wait result."""
    if not samples:
        return {"available": False, "samples": 0, "delta": None, "peaks": None}
    ordered = sorted(samples, key=lambda sample: sample["at_ns"])
    for previous, current in zip(ordered, ordered[1:]):
        need(current["values"]["MaxConns"] == previous["values"]["MaxConns"], "database_pool_capacity_changed")
        need(all(current["values"][name] >= previous["values"][name] for name in POOL_COUNTERS), "database_pool_counter_reset")
    first, last = ordered[0], ordered[-1]
    return {"available": True, "samples": len(ordered), "interval_ns": str(last["at_ns"] - first["at_ns"]),
        "before": {name: str(first["values"][name]) for name in POOL_COUNTERS},
        "after": {name: str(last["values"][name]) for name in POOL_COUNTERS},
        "delta": {name: str(last["values"][name] - first["values"][name]) for name in POOL_COUNTERS},
        "peaks": {name: max(sample["values"][name] for sample in ordered) for name in POOL_GAUGES}}


def validate_manifest(value):
    exact(value, "schema_version admission run_id owner_id source_revision profile_id tier guest resource_envelope budgets concurrency thresholds scan_evidence", "manifest_fields")
    need(type(value["schema_version"]) is int and value["schema_version"] == VERSION and value["admission"] == "approved", "profile_not_admitted")
    for key in ("run_id", "owner_id", "profile_id"):
        need(type(value[key]) is str and SAFE.fullmatch(value[key]), "manifest_identifier")
    need(re.fullmatch(r"[0-9a-f]{40}", value["source_revision"]) is not None, "source_revision")
    need(value["tier"] in (10000, 100000), "catalog_tier")
    exact(value["guest"], "vmid machine_id", "guest_fields")
    need(value["guest"]["vmid"] == 106 and re.fullmatch(r"[0-9a-f]{32}", value["guest"]["machine_id"]), "isolated_guest")
    exact(value["resource_envelope"], "cpu_count memory_bytes disk_bytes postgres_version storage_description", "envelope_fields")
    for key in ("cpu_count", "memory_bytes", "disk_bytes"):
        integer(value["resource_envelope"][key], 1, 1 << 50, "envelope_number")
    exact(value["budgets"], "total_seconds phase_seconds idle_seconds cleanup_seconds request_seconds poll_seconds max_requests max_events max_artifact_bytes max_response_bytes max_stream_bytes max_process_seconds", "budget_fields")
    limits = {"total_seconds": (60, 14400), "phase_seconds": (10, 3600), "idle_seconds": (3, 120),
              "cleanup_seconds": (10, 300), "request_seconds": (1, 180), "poll_seconds": (.05, 5),
              "max_requests": (512, 100000), "max_events": (1024, 500000),
              "max_artifact_bytes": (1 << 20, 4 << 30), "max_response_bytes": (1024, 16 << 20),
              "max_stream_bytes": (4096, 256 << 20), "max_process_seconds": (1, 180)}
    for key, bounds in limits.items():
        number(value["budgets"][key], *bounds, "budget_number")
        if key.startswith("max_") and key != "max_process_seconds":
            integer(value["budgets"][key], *bounds, "budget_integer")
    exact(value["concurrency"], "query_workers playback_workers", "concurrency_fields")
    integer(value["concurrency"]["query_workers"], 1, 16, "query_workers")
    # One independent binding per mode. Additional viewers require a new schema
    # with distinct auth/device scopes rather than counting deduplicated work.
    integer(value["concurrency"]["playback_workers"], 3, 3, "playback_workers")
    exact(value["thresholds"], "query_p95_ms query_p99_ms playback_start_p95_ms seek_p95_ms scan_seconds max_failures max_memory_bytes max_cpu_percent max_io_bytes max_children min_all_lane_overlap_ms min_requests_per_query min_media_bytes frame_mae", "threshold_fields")
    for key, threshold in value["thresholds"].items():
        number(threshold, 0, 1 << 50, "threshold_number")
    need(value["thresholds"]["min_all_lane_overlap_ms"] > 0 and value["thresholds"]["min_requests_per_query"] >= 1
         and value["thresholds"]["min_media_bytes"] >= 1024, "threshold_vacuous")
    exact(value["scan_evidence"], "enabled spool_required configuration_sha256", "scan_evidence_fields")
    need(value["scan_evidence"]["enabled"] is True and value["scan_evidence"]["spool_required"] is True
         and re.fullmatch(r"[0-9a-f]{64}", value["scan_evidence"]["configuration_sha256"]), "scan_evidence_required")
    return value


def sql_string(value):
    need(type(value) is str and "\x00" not in value and len(value) <= 4096, "sql_literal")
    return "'" + value.replace("'", "''") + "'"


class Evidence:
    def __init__(self, directory, manifest):
        self.directory = Path(directory)
        need(self.directory.is_absolute() and not self.directory.exists(), "output_not_fresh")
        self.directory.mkdir(mode=0o700)
        self.private = self.directory / "private"
        self.private.mkdir(mode=0o700)
        self.manifest = manifest
        self.lock = threading.RLock()
        self.events, self.bytes, self.serial = [], 0, 0
        self.closing = False

    def charge(self, size):
        maximum = self.manifest["budgets"]["max_artifact_bytes"]
        reserve = min(64 << 20, maximum // 5)
        need(self.bytes + size <= maximum - (0 if self.closing else reserve), "artifact_budget")
        self.bytes += size

    def artifact(self, label, data):
        with self.lock:
            need(SAFE.fullmatch(label), "artifact_label")
            self.charge(len(data))
            self.serial += 1
            name = "%07d-%s" % (self.serial, label)
            fd = os.open(self.private / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, "wb") as stream:
                stream.write(data)
            return {"name": name, "sha256": digest(data), "bytes": len(data)}

    def event(self, kind, **values):
        with self.lock:
            need(len(self.events) < self.manifest["budgets"]["max_events"] - (0 if self.closing else 512), "event_budget")
            event = {"kind": kind, "at_ns": time.monotonic_ns(), **values}
            encoded = json_bytes(event)
            self.charge(len(encoded))
            self.events.append(event)
            # Append synchronously so a forced reset cannot erase all observations.
            with open(self.directory / "events.jsonl", "ab", buffering=0) as stream:
                os.chmod(stream.name, 0o600)
                stream.write(encoded)
            return event

    def checkpoint(self, value):
        self.atomic("checkpoint.json", value)

    def atomic(self, name, value):
        data = json_bytes(value)
        with self.lock:
            self.charge(len(data))
            temporary = self.directory / (name + ".pending")
            fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, "wb") as stream:
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, self.directory / name)
            fd = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)


class Actor:
    def __init__(self, manifest, context, evidence, request_scope="profile"):
        self.m, self.c, self.e = manifest, context, evidence
        need(type(request_scope) is str and SAFE.fullmatch(request_scope), "request_scope")
        self.request_scope = request_scope
        self.started = time.monotonic()
        self.deadline = self.started + manifest["budgets"]["total_seconds"]
        self.lock, self.stop = threading.RLock(), threading.Event()
        self.requests, self.phase = 0, "admission"
        self.jobs, self.plays, self.lanes, self.failures = {}, {}, {}, []
        self.closed_plays, self.intents = {}, {}
        self.unresolved_brokers = {}
        self.samples, self.resources, self.procs = {}, [], {}
        self.pool_samples, self.pool_lock = [], threading.Lock()
        self.changed, self.settings_before, self.metadata_before = [], None, None
        self.cleanup_mode = False
        self.phase_deadline = None
        self.owner_filter = None
        self.spool_path = None
        self.analysis_paths = set()
        self.validate_context()

    def validate_context(self):
        required = "schema_version manifest_sha256 driver_sha256 run_id owner_id origin admin_cookie csrf_token emby_token user_id device_id owner_file app_pid app_start_ticks cgroup_path postgres process_observer pg_env tools roots inventory_path inventory_sha256 scan_libraries catalog_expected queries playback analysis_item_ids mutation scan_evidence_path scan_evidence_sha256"
        exact(self.c, required, "context_fields")
        need(type(self.c["schema_version"]) is int and self.c["schema_version"] == CONTEXT_VERSION and self.c["run_id"] == self.m["run_id"]
             and self.c["owner_id"] == self.m["owner_id"], "context_binding")
        origin = urlsplit(self.c["origin"])
        need(origin.scheme == "http" and origin.hostname in ("127.0.0.1", "::1") and origin.port
             and origin.path == "" and not origin.query and not origin.fragment and not origin.username, "loopback_origin")
        for key in ("admin_cookie", "csrf_token", "emby_token", "user_id", "device_id"):
            need(type(self.c[key]) is str and 0 < len(self.c[key]) <= 4096 and not any(ch in self.c[key] for ch in "\r\n\x00"), "credential_shape")
        exact(self.c["tools"], "psql ffmpeg ffprobe", "tools_fields")
        for path in self.c["tools"].values():
            need(Path(path).is_absolute() and Path(path).resolve() == Path(path) and os.access(path, os.X_OK), "tool_identity")
        need(set(self.c["pg_env"]) <= {"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD", "PGSSLMODE"}
             and {"PGHOST", "PGDATABASE", "PGUSER"} <= set(self.c["pg_env"]), "pg_environment")
        self.validate_postgres_binding()
        self.validate_process_observer()
        need(type(self.c["roots"]) is list and 2 <= len(self.c["roots"]) <= 32, "root_count")
        self.roots = {}
        for root in self.c["roots"]:
            exact(root, "id library_id path", "root_fields")
            path = Path(root["path"])
            need(SAFE.fullmatch(root["id"]) and root["id"] not in self.roots and path.is_absolute()
                 and path.resolve() == path and path.is_dir(), "root_identity")
            need(all(path != other and path not in other.parents and other not in path.parents for other in self.roots.values()), "nested_root")
            self.roots[root["id"]] = path
        exact(self.c["catalog_expected"], "initial cold cached incremental", "catalog_expected_fields")
        integer(self.c["catalog_expected"]["initial"], self.m["tier"] // 2 + 1, self.m["tier"] + 999, "initial_catalog_tier")
        for phase in PHASES:
            integer(self.c["catalog_expected"][phase], self.m["tier"], self.m["tier"] + 999, "settled_catalog_tier")
        exact(self.c["queries"], "cold cached incremental", "query_phase_fields")
        for phase in PHASES:
            queries = self.c["queries"][phase]
            need(type(queries) is list and 7 <= len(queries) <= 32 and {q.get("kind") for q in queries} == QUERY_KINDS, "query_coverage")
            need(len({q["id"] for q in queries}) == len(queries), "query_duplicate")
        for query in [query for phase in PHASES for query in self.c["queries"][phase]]:
            exact(query, "id kind params total ids", "query_fields")
            need(SAFE.fullmatch(query["id"]) and type(query["params"]) is dict and type(query["ids"]) is list
                 and len(query["ids"]) <= 200 and len(set(query["ids"])) == len(query["ids"]), "query_shape")
            integer(query["total"], 0, 1000000, "query_total")
            need(len(query["params"]) <= 24 and all(type(key) is str and len(key) <= 64 and type(value) is str and len(value) <= 2048
                 for key, value in query["params"].items()) and all(type(value) is str and SAFE.fullmatch(value) for value in query["ids"]), "query_value_bounds")
            if query["kind"] == "unicode":
                need(any(ord(ch) > 127 for ch in query["params"].get("SearchTerm", "")), "unicode_query_required")
            if query["kind"] == "filter":
                need(bool({"IncludeItemTypes", "IsFavorite", "IsPlayed", "Genres", "Years"} & set(query["params"])), "filter_query_required")
            if query["kind"] == "deep":
                need(int(query["params"].get("StartIndex", 0)) >= self.m["tier"] // 2 and query["ids"], "deep_page_required")
            if query["kind"] in {"resume", "latest", "shallow"}:
                need(query["ids"], "nonempty_query_required")
        need(type(self.c["playback"]) is list and {p.get("mode") for p in self.c["playback"]} == PLAY_MODES
             and len(self.c["playback"]) == 3, "playback_modes")
        need(len({p.get("item_id") for p in self.c["playback"]}) == 3, "distinct_playback_items_required")
        for play in self.c["playback"]:
            exact(play, "mode item_id path body seek_ticks expected_codecs client", "playback_fields")
            need(type(play["body"]) is dict and type(play["expected_codecs"]) is list and play["expected_codecs"], "playback_profile")
            need(len(json_bytes(play["body"])) <= 65536 and len(play["expected_codecs"]) <= 8 and all(type(codec) is str and SAFE.fullmatch(codec) for codec in play["expected_codecs"]), "playback_profile_bounds")
            integer(play["seek_ticks"], 10000000, 1 << 53, "seek_required")
            self.owned_media(play["path"])
            if play["mode"] != "direct":
                need(play["body"].get("EnableDirectPlay") is False and play["body"].get("EnableTranscoding") is True, "conversion_profile")
        validate_playback_clients({play["mode"]: play["client"] for play in self.c["playback"]},
                                  self.c["emby_token"], self.c["device_id"])
        need(type(self.c["analysis_item_ids"]) is list and 2 <= len(self.c["analysis_item_ids"]) <= 200, "analysis_cohort")
        need(type(self.c["scan_libraries"]) is list and 1 <= len(self.c["scan_libraries"]) <= 8, "scan_scope")
        for library in self.c["scan_libraries"]:
            exact(library, "id expected", "scan_library_fields")
            need(set(library["expected"]) == set(PHASES), "scan_phases")
            for phase in PHASES:
                exact(library["expected"][phase], "Scanned Added Updated", "scan_counters")
                for value in library["expected"][phase].values():
                    integer(value, 0, 200000, "scan_counter")
            need(library["expected"]["cold"]["Added"] > 0 and library["expected"]["cached"]["Added"] == 0
                 and library["expected"]["cached"]["Updated"] == 0, "cold_cached_contract")
        mutation = self.c["mutation"]
        exact(mutation, "delete_path quarantine_path move_from move_to add_from add_to metadata_item_id sort_words", "mutation_fields")
        for key in ("delete_path", "move_from"):
            self.owned_media(mutation[key])
        for key in ("move_to", "add_to"):
            self.owned_media(mutation[key], exists=False)
        need(self.root_for(mutation["move_from"]) != self.root_for(mutation["move_to"]), "cross_root_move_required")
        need(Path(mutation["move_from"]).name == Path(mutation["move_to"]).name, "move_name_must_remain_stable")
        root_libraries = {r["id"]: r["library_id"] for r in self.c["roots"]}
        need(root_libraries[self.root_for(mutation["move_from"])] == root_libraries[self.root_for(mutation["move_to"])], "move_same_library_required")
        need(type(mutation["sort_words"]) is list and mutation["sort_words"], "sorting_change_required")
        self.assert_owned()

    def remaining(self):
        remaining = min(self.deadline, self.phase_deadline or self.deadline) - time.monotonic()
        need(remaining > 0, "workload_deadline")
        return remaining

    def root_for(self, path):
        path = Path(path)
        matches = [key for key, root in self.roots.items() if root in path.parents]
        need(len(matches) == 1, "media_outside_roots")
        return matches[0]

    def owned_media(self, path, exists=True):
        path = Path(path)
        self.root_for(path)
        need(path.is_absolute() and path.resolve() == path and path.suffix.lower() in MEDIA_EXTENSIONS, "owned_media_path")
        need(path.is_file() if exists else not path.exists(), "owned_media_exists")
        return path

    def assert_owned(self):
        marker = strict_json(read_private(self.c["owner_file"], 16384))
        need(all(marker.get(key) == self.m[key] for key in ("owner_id", "run_id", "source_revision", "guest")), "owner_marker")
        need(marker.get("app_pid") == self.c["app_pid"] and marker.get("app_start_ticks") == self.c["app_start_ticks"]
             and marker.get("roots") == self.c["roots"] and marker.get("cgroup_path") == self.c["cgroup_path"], "owner_scope")
        need(marker.get("postgres") == self.c["postgres"], "postgres_owner_scope")
        need(Path("/etc/machine-id").read_text().strip() == self.m["guest"]["machine_id"], "guest_machine_identity")
        fields = Path("/proc/%d/stat" % self.c["app_pid"]).read_text().rsplit(")", 1)[1].split()
        need(int(fields[19]) == self.c["app_start_ticks"], "app_pid_reused")
        group = Path(self.c["cgroup_path"])
        need(group.is_absolute() and group.resolve() == group and Path("/sys/fs/cgroup") in group.parents, "cgroup_scope")
        need(str(self.c["app_pid"]) in (group / "cgroup.procs").read_text().split(), "app_not_in_owned_cgroup")
        # CPU and memory accounting include descendant cgroups. Checking only
        # this directory's cgroup.procs would miss an actor in a nested scope.
        measured = "/" + group.relative_to("/sys/fs/cgroup").as_posix()
        memberships = [line.split(":", 2)[2] for line in Path("/proc/self/cgroup").read_text().splitlines()
                       if line.startswith("0::")]
        need(len(memberships) == 1 and memberships[0].startswith("/"), "actor_cgroup_identity")
        need(memberships[0] != measured and not memberships[0].startswith(measured + "/"), "actor_in_measured_cgroup")
        self.validate_postgres_binding()

    def validate_postgres_binding(self):
        postgres = self.c["postgres"]
        exact(postgres, "cgroup_path postmaster_pid postmaster_start_ticks postmaster_started_at", "postgres_binding_fields")
        integer(postgres["postmaster_pid"], 2, 1 << 31, "postmaster_pid")
        integer(postgres["postmaster_start_ticks"], 1, 1 << 63, "postmaster_start_ticks")
        need(type(postgres["postmaster_started_at"]) is str and 20 <= len(postgres["postmaster_started_at"]) <= 40, "postmaster_start_time")
        instant = datetime.fromisoformat(postgres["postmaster_started_at"].replace("Z", "+00:00"))
        need(instant.tzinfo is not None, "postmaster_start_timezone")
        group, app_group = Path(postgres["cgroup_path"]), Path(self.c["cgroup_path"])
        need(group.is_absolute() and group.resolve() == group and Path("/sys/fs/cgroup") in group.parents, "postgres_cgroup_scope")
        need(group != app_group and group not in app_group.parents and app_group not in group.parents, "service_cgroups_overlap")
        fields = Path("/proc/%d/stat" % postgres["postmaster_pid"]).read_text().rsplit(")", 1)[1].split()
        need(int(fields[19]) == postgres["postmaster_start_ticks"], "postmaster_pid_reused")
        need(str(postgres["postmaster_pid"]) in (group / "cgroup.procs").read_text().split(), "postmaster_cgroup_mismatch")
        measured = "/" + group.relative_to("/sys/fs/cgroup").as_posix()
        memberships = [line.split(":", 2)[2] for line in Path("/proc/self/cgroup").read_text().splitlines() if line.startswith("0::")]
        need(len(memberships) == 1 and memberships[0] != measured and not memberships[0].startswith(measured + "/"), "actor_in_postgres_cgroup")

    def validate_process_observer(self):
        observer = self.c["process_observer"]
        arguments = process_observer_arguments(observer)
        script = Path(arguments[5])
        info = script.lstat()
        need(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and info.st_mode & 0o022 == 0
             and 0 < info.st_size <= 1 << 20 and digest(script.read_bytes()) == observer["source_sha256"], "process_observer_source_identity")

    def process_sample(self):
        self.validate_process_observer()
        started = time.monotonic_ns()
        raw, command = self.command("process_observer", [], json_bytes({"schema_version": 1, "operation": "sample"}), maximum=8 << 20)
        ended = time.monotonic_ns()
        value = strict_json(raw)
        exact(value, "schema_version complete run_id owner_id binding_sha256 app observed_monotonic_ns resource processes scan_evidence race_count", "process_observation_fields")
        need(value["schema_version"] == 1 and value["complete"] is True and value["run_id"] == self.m["run_id"]
             and value["owner_id"] == self.m["owner_id"] and value["binding_sha256"] == self.c["process_observer"]["binding_sha256"], "process_observation_scope")
        need(value["app"] == {"pid": self.c["app_pid"], "start_ticks": self.c["app_start_ticks"], "cgroup_path": self.c["cgroup_path"]}, "process_observation_app")
        integer(value["observed_monotonic_ns"], started, ended, "process_observation_clock")
        exact(value["resource"], "cpu_usage_usec io_bytes memory_bytes scan_spool_generations", "process_resource_fields")
        for observation in value["resource"].values():
            integer(observation, 0, 1 << 63, "process_resource_value")
        need(value["scan_evidence"] == {"path": self.c["scan_evidence_path"], "configuration_sha256": self.c["scan_evidence_sha256"], "environment_matches": True}, "scan_evidence_deployment_mismatch")
        need(type(value["processes"]) is list and len(value["processes"]) <= 4096, "process_observation_count")
        seen = set()
        for process in value["processes"]:
            exact(process, "pid start_ticks executable arguments source_paths lineage cpu_ticks", "process_observation_process")
            integer(process["pid"], 2, 1 << 31, "media_process_pid")
            integer(process["start_ticks"], 1, 1 << 63, "media_process_start")
            integer(process["cpu_ticks"], 0, 1 << 63, "media_process_cpu")
            identity = (process["pid"], process["start_ticks"])
            need(identity not in seen and process["executable"] in {self.c["tools"]["ffmpeg"], self.c["tools"]["ffprobe"]}, "media_process_identity")
            seen.add(identity)
            need(type(process["arguments"]) is list and 1 <= len(process["arguments"]) <= 512
                 and all(type(argument) is str and len(argument) <= 65536 for argument in process["arguments"]), "media_process_arguments")
            need(type(process["source_paths"]) is list and len(process["source_paths"]) <= 4096
                 and all(type(path) is str and len(path) <= 4096 for path in process["source_paths"]), "media_process_paths")
            lineage = process["lineage"]
            need(type(lineage) is list and 1 <= len(lineage) <= 64 and lineage[0]["pid"] == process["pid"]
                 and lineage[0]["start_ticks"] == process["start_ticks"] and lineage[-1]["pid"] == self.c["app_pid"]
                 and lineage[-1]["start_ticks"] == self.c["app_start_ticks"], "media_process_lineage_binding")
            for index, link in enumerate(lineage):
                exact(link, "pid start_ticks parent_pid", "media_process_lineage_fields")
                integer(link["pid"], 2, 1 << 31, "media_lineage_pid")
                integer(link["parent_pid"], 0, 1 << 31, "media_lineage_parent")
                integer(link["start_ticks"], 1, 1 << 63, "media_lineage_start")
                if index + 1 < len(lineage):
                    need(link["parent_pid"] == lineage[index + 1]["pid"], "media_process_lineage_chain")
        integer(value["race_count"], 0, 1 << 31, "process_observation_races")
        self.e.artifact("root-process-observation.json", raw)
        self.e.event("process_observer", phase=self.phase, start_ns=started, end_ns=ended, receipt=command, races=value["race_count"])
        return value

    def app_process_lineage(self, pid, start_ticks, anchor_pid=None, anchor_ticks=None):
        """Return a revalidated service ancestry, None for a race, or [] if foreign."""
        anchor_pid = self.c["app_pid"] if anchor_pid is None else anchor_pid
        anchor_ticks = self.c["app_start_ticks"] if anchor_ticks is None else anchor_ticks
        lineage, seen = [], set()
        current, child_start = pid, None
        try:
            for _ in range(64):
                if current in seen or current == os.getpid():
                    return []
                seen.add(current)
                fields = Path("/proc/%d/stat" % current).read_text().rsplit(")", 1)[1].split()
                parent, started = int(fields[1]), int(fields[19])
                if (current == pid and started != start_ticks) or (child_start is not None and started > child_start):
                    return None
                lineage.append((current, started, parent))
                if current == anchor_pid:
                    if started != anchor_ticks:
                        return None
                    # A PID or parent change while traversing cannot establish
                    # ancestry. Recheck every link before admitting evidence.
                    for identity, stamp, parent_id in lineage:
                        check = Path("/proc/%d/stat" % identity).read_text().rsplit(")", 1)[1].split()
                        if int(check[19]) != stamp or int(check[1]) != parent_id:
                            return None
                    return [{"pid": identity, "start_ticks": stamp, "parent_pid": parent_id}
                            for identity, stamp, parent_id in lineage]
                if current <= 1 or parent <= 0:
                    return []
                current, child_start = parent, started
        except (FileNotFoundError, ProcessLookupError):
            return None
        return []

    def command(self, tool, arguments, input_data=b"", maximum=1 << 20, timeout=None, environment=None):
        self.assert_owned()
        limit = min(timeout or self.m["budgets"]["max_process_seconds"], self.remaining())
        env = {"PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8", "TZ": "UTC"}
        if environment:
            env.update(environment)
        need(len(input_data) <= 65536 and len(arguments) <= 128 and sum(len(str(arg)) for arg in arguments) <= 65536, "child_input_bound")
        # Nonblocking pipes prevent a descendant retaining an inherited pipe
        # from defeating a leader-only wait or a buffered-reader thread join.
        output, errors = bytearray(), bytearray()
        selector = None
        argv = self.c["process_observer"]["argv"] if tool == "process_observer" else [self.c["tools"][tool], *arguments]
        need(tool != "process_observer" or not arguments and environment is None, "process_observer_no_extra_arguments")
        process = subprocess.Popen(argv, stdin=subprocess.PIPE,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, start_new_session=True, bufsize=0)
        try:
            # Initialization can exhaust descriptors too. Every operation after
            # a successful spawn belongs inside the same child cleanup boundary.
            selector = selectors.DefaultSelector()
            for stream, target in ((process.stdout, output), (process.stderr, errors)):
                os.set_blocking(stream.fileno(), False)
                selector.register(stream, selectors.EVENT_READ, target)
            os.set_blocking(process.stdin.fileno(), False)
            if input_data:
                selector.register(process.stdin, selectors.EVENT_WRITE, None)
            else:
                process.stdin.close()
            written = 0
            deadline = time.monotonic() + limit
            while selector.get_map() or process.poll() is None:
                need(time.monotonic() < deadline, "child_deadline")
                for key, _ in selector.select(.05):
                    if key.fileobj is process.stdin:
                        written += os.write(process.stdin.fileno(), input_data[written:])
                        if written == len(input_data):
                            selector.unregister(process.stdin)
                            process.stdin.close()
                    else:
                        chunk = os.read(key.fileobj.fileno(), 65536)
                        if not chunk:
                            selector.unregister(key.fileobj)
                        else:
                            need(len(key.data) + len(chunk) <= maximum, "child_output_budget")
                            key.data.extend(chunk)
        finally:
            try:
                if tool == "process_observer":
                    # The root supervisor owns a separate worker process group
                    # and enforces 12s wall time plus 3s kill/reap grace. An
                    # unprivileged actor cannot claim that killpg(root) worked.
                    try:
                        process.stdin.close()
                        pipes = {process.stdout.fileno(): output, process.stderr.fileno(): errors}
                        for descriptor in pipes:
                            os.set_blocking(descriptor, False)
                        close_deadline = time.monotonic() + 16
                        while process.poll() is None and time.monotonic() < close_deadline:
                            readable, _, _ = select.select(list(pipes), [], [], .05)
                            for descriptor in readable:
                                chunk = os.read(descriptor, 65536)
                                if not chunk:
                                    pipes.pop(descriptor, None)
                                else:
                                    # Keep draining even after an earlier output
                                    # or phase budget failure so root cannot be
                                    # stranded while writing its final receipt.
                                    room = max(0, maximum - len(pipes[descriptor]))
                                    pipes[descriptor].extend(chunk[:room])
                        process.wait(timeout=max(.001, close_deadline - time.monotonic()))
                    except subprocess.TimeoutExpired:
                        self.unresolved_brokers[process.pid] = {"pid": process.pid, "reason": "root_supervisor_not_reaped"}
                        raise Failure("root_process_observer_not_reaped") from None
                    finally:
                        self.e.artifact("root-observer-stdout.json", bytes(output))
                        self.e.artifact("root-observer-stderr.txt", bytes(errors))
                        self.e.artifact("root-observer-exit.json", json_bytes({"pid": process.pid, "returncode": process.poll(),
                            "root_worker_lifecycle_owned_by_supervisor": True, "actor_kill_not_attempted": True}))
                    if process.returncode != 0:
                        self.unresolved_brokers[process.pid] = {"pid": process.pid, "reason": "root_observer_failure_requires_external_receipt_review"}
                else:
                    try:
                        os.killpg(process.pid, signal.SIGKILL)
                    except ProcessLookupError:
                        pass
                    finally:
                        process.wait(timeout=5)
            finally:
                # A failed kill or reap must not retain the actor's pipe and
                # selector descriptors. It still fails the workload closure.
                try:
                    if selector is not None:
                        selector.close()
                finally:
                    try:
                        process.stdin.close()
                    finally:
                        try:
                            process.stdout.close()
                        finally:
                            process.stderr.close()
        receipt = self.e.artifact("child.json", json_bytes({"tool": tool, "arguments": arguments, "exit_code": process.returncode,
                                                           "stdout_sha256": digest(output), "stderr": errors.decode("utf-8", "replace")}))
        need(process.returncode == 0, "child_exit")
        return bytes(output), receipt

    def sql(self, expression):
        statement = "SET statement_timeout='10s'; SET lock_timeout='5s'; SET timezone='UTC'; " + expression + ";\n"
        raw, ref = self.command("psql", ["-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"], statement.encode(), environment=self.c["pg_env"])
        self.e.artifact("sql.json", json_bytes({"statement": statement, "raw": raw.decode("utf-8"), "command": ref}))
        return strict_json(raw.strip())

    def http(self, label, method, path, body=None, admin=False, statuses=(200,), binary=False, headers=None, include_status=False):
        self.assert_owned()
        parsed = urlsplit(path)
        need(not parsed.scheme and not parsed.netloc and parsed.path.startswith("/") and not parsed.fragment, "http_same_origin")
        origin = urlsplit(self.c["origin"])
        with self.lock:
            self.requests += 1
            need(self.requests <= self.m["budgets"]["max_requests"] - (0 if self.cleanup_mode else 256), "request_budget")
        request_headers = {"Connection": "close", "Accept-Encoding": "identity", "Origin": self.c["origin"]}
        if admin:
            request_headers.update({"Cookie": self.c["admin_cookie"], "X-CSRF-Token": self.c["csrf_token"]})
        else:
            request_headers.update({"X-Emby-Token": self.c["emby_token"], "X-Emby-Authorization":
                'MediaBrowser Client="Phase3", Device="Linux", DeviceId="%s", Version="1"' % self.c["device_id"]})
        if headers:
            request_headers.update(headers)
        payload = None if body is None else json_bytes(body)
        if payload is not None:
            request_headers["Content-Type"] = "application/json"
        started = time.monotonic_ns()
        status, first, data, error, response_headers = None, None, bytearray(), "", {}
        exception_details = None
        connection = http.client.HTTPConnection(origin.hostname, origin.port, timeout=min(self.m["budgets"]["request_seconds"], self.remaining()))
        request_deadline = time.monotonic() + min(self.m["budgets"]["request_seconds"], self.remaining())
        watchdog = None
        try:
            connection.connect()
            transport = connection.sock
            def expire():
                try:
                    transport.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass
            watchdog = threading.Timer(max(.001, request_deadline - time.monotonic()), expire)
            watchdog.start()
            connection.request(method, path, payload, request_headers)
            response = connection.getresponse()
            status, response_headers = response.status, dict(response.getheaders())
            maximum = self.m["budgets"]["max_stream_bytes" if binary else "max_response_bytes"]
            while True:
                if response.length == 0 or response.fp is None:
                    break
                remaining = request_deadline - time.monotonic()
                need(remaining > 0, "http_total_deadline")
                transport.settimeout(remaining)
                chunk = response.read1(65536)
                if not chunk:
                    break
                if first is None:
                    first = time.monotonic_ns()
                need(len(data) + len(chunk) <= maximum, "http_body_budget")
                data.extend(chunk)
            need(status in statuses, "http_status")
        except Exception as caught:
            error = str(caught) if isinstance(caught, Failure) else "http_transport"
            exception_details = http_exception_details(caught)
            raise Failure(error) from None
        finally:
            if watchdog:
                watchdog.cancel()
                watchdog.join(timeout=2)
                need(not watchdog.is_alive(), "http_watchdog_not_joined")
            connection.close()
            ended = time.monotonic_ns()
            ref = self.e.artifact("http.json", json_bytes({"label": label, "method": method, "path": path, "request_body": body,
                "status": status, "headers": response_headers, "body_sha256": digest(data), "error": error,
                "exception_details": exception_details}))
            body_ref = self.e.artifact("body.bin", bytes(data))
            self.e.event("http", label=label, phase=self.phase, start_ns=started, end_ns=ended,
                         first_byte_ns=first, status=status, bytes=len(data), error=error, receipt=ref, body=body_ref,
                         exception_details=exception_details)
        result = (bytes(data) if binary or not data else strict_json(data)), response_headers, (started, ended), body_ref
        return (*result, status) if include_status else result

    def native(self, path, method="GET", body=None, statuses=(200,)):
        return self.http("control", method, path, body, admin=True, statuses=statuses)[0]

    def sample_database_pool(self, boundary="sample"):
        with self.pool_lock:
            phase = self.phase
            result, _, span, receipt = self.http("database-pool", "GET", "/admin/v1/runtime/resources", admin=True)
            need(type(result) is dict and "DatabasePool" in result, "database_pool_endpoint_missing")
            values = database_pool_values(result["DatabasePool"])
            sample = {"phase": phase, "at_ns": span[1], "request_start_ns": span[0], "values": values, "boundary": boundary}
            if self.pool_samples:
                database_pool_summary([self.pool_samples[-1], sample])
            self.pool_samples.append(sample)
            self.e.event("database_pool", phase=phase, start_ns=span[0], end_ns=span[1], boundary=boundary,
                values={name: values[name] if name in POOL_GAUGES else str(values[name]) for name in POOL_GAUGES + POOL_COUNTERS}, receipt=receipt)
            return sample

    def admission_intent(self, kind, scope):
        with self.lock:
            identity = "%s-%d-%d" % (kind, time.monotonic_ns(), len(self.intents))
            self.intents[identity] = {"kind": kind, "scope": scope}
            self.e.artifact("admission-intent.json", json_bytes({"id": identity, "phase": self.phase, **self.intents[identity]}))
            return identity

    def sample_job(self, kind, identity, active, track=True):
        with self.lock:
            self.samples.setdefault((self.phase, kind, identity), []).append((time.monotonic_ns(), identity, active))
            if track:
                self.jobs[identity] = {"kind": kind, "active": active}

    def owner(self):
        if self.owner_filter is None:
            scope = self.sql("SELECT json_build_object('database',current_database(),'schema',current_schema())")
            key = int.from_bytes(hashlib.sha256(("goby.library.owner\x00" + scope["database"] + "\x00" + scope["schema"]).encode()).digest()[:8], "big", signed=True)
            self.owner_filter = ("l.locktype='advisory' AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND l.classid=((%d::bigint >> 32)&4294967295)::oid AND l.objid=(%d::bigint&4294967295)::oid AND l.objsubid=1 AND l.mode='ExclusiveLock' AND l.granted" % (key, key))
        result = self.sql("SELECT COALESCE(json_agg(json_build_object('pid',a.pid,'backend_start',a.backend_start,'postmaster_started_at',pg_postmaster_start_time(),'waiting',a.wait_event_type='Lock')),'[]'::json) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE " + self.owner_filter)
        need(len(result) == 1, "catalog_owner_missing_or_ambiguous")
        postgres = self.c["postgres"]
        need(str(result[0]["pid"]) in (Path(postgres["cgroup_path"]) / "cgroup.procs").read_text().split(), "postgres_backend_outside_owned_cgroup")
        need(datetime.fromisoformat(result[0]["postmaster_started_at"].replace("Z", "+00:00")) ==
             datetime.fromisoformat(postgres["postmaster_started_at"].replace("Z", "+00:00")), "postgres_server_identity_changed")
        fields = Path("/proc/%d/stat" % result[0]["pid"]).read_text().rsplit(")", 1)[1].split()
        lineage = self.app_process_lineage(result[0]["pid"], int(fields[19]), postgres["postmaster_pid"], postgres["postmaster_start_ticks"])
        need(lineage, "postgres_backend_not_owned_postmaster_child")
        return result[0]

    def snapshot(self, cutoff=None):
        move = sql_string(self.c["mutation"]["move_from"])
        deletion = sql_string(self.c["mutation"]["delete_path"])
        scope = "created_at <= " + (sql_string(cutoff) + "::timestamptz" if cutoff else "transaction_timestamp()")
        if self.phase == "incremental" and hasattr(self, "mutation_before"):
            scope += " AND id<>" + sql_string(self.mutation_before["delete_id"])
        result = self.sql("SELECT json_build_object('captured_at',transaction_timestamp(),'catalog_count',(SELECT count(*) FROM items),'file_items',(SELECT count(*) FROM items WHERE NOT is_folder AND path<>''),'nonfile_items',(SELECT count(*) FROM items WHERE path=''),'move',(SELECT row_to_json(x) FROM (SELECT id,library_id,root_id,path,file_identity FROM items WHERE path=" + move + ")x),'delete_id',(SELECT id FROM items WHERE path=" + deletion + "),'settings',(SELECT row_to_json(m) FROM managed_settings m WHERE id=1),'sort_digest',(SELECT md5(string_agg(id||':'||sort_name,E'\\n' ORDER BY id)) FROM items WHERE " + scope + "),'sort_scope_count',(SELECT count(*) FROM items WHERE " + scope + "),'sort_witness',(SELECT sort_name FROM items WHERE id=" + sql_string(self.c["mutation"]["metadata_item_id"]) + "),'postgres_version',current_setting('server_version'))")
        owner = self.owner()
        result["owner"] = {key: owner[key] for key in ("pid", "backend_start")}
        return result

    def inventory(self):
        raw = read_private(self.c["inventory_path"], 128 << 20)
        need(digest(raw) == self.c["inventory_sha256"], "inventory_digest")
        rows, paths, inodes, hashes, originals, formats = 0, set(), {}, set(), set(), {}
        content_formats, format_paths, format_bytes, content_bytes = {}, {}, {}, 0
        apparent, allocated, generated, licensed = 0, 0, 0, 0
        for line in raw.splitlines():
            row = strict_json(line)
            exact(row, "root_id relative_path sha256 bytes origin original_id", "inventory_fields")
            need(row["root_id"] in self.roots and row["origin"] in {"generated", "licensed"}, "inventory_origin")
            path = self.owned_media(str(self.roots[row["root_id"]] / row["relative_path"]))
            need(path not in paths, "inventory_duplicate")
            paths.add(path)
            info = path.stat()
            identity = (info.st_dev, info.st_ino)
            need(info.st_size == row["bytes"] and info.st_size > 0, "inventory_size")
            if identity not in inodes:
                hasher = hashlib.sha256()
                with path.open("rb") as stream:
                    while chunk := stream.read(1 << 20):
                        self.remaining()
                        hasher.update(chunk)
                inodes[identity] = hasher.hexdigest()
                allocated += info.st_blocks * 512
            need(inodes[identity] == row["sha256"], "inventory_source_changed")
            if row["sha256"] not in hashes:
                probe, _ = self.command("ffprobe", ["-v", "error", "-show_format", "-show_streams", "-of", "json", str(path)])
                fact = strict_json(probe)
                need(fact.get("streams"), "inventory_not_media")
                format_name = fact.get("format", {}).get("format_name", "unknown")
                formats[format_name] = formats.get(format_name, 0) + 1
                content_formats[row["sha256"]] = format_name
                content_bytes += info.st_size
            format_name = content_formats[row["sha256"]]
            format_paths[format_name] = format_paths.get(format_name, 0) + 1
            format_bytes[format_name] = format_bytes.get(format_name, 0) + info.st_size
            hashes.add(row["sha256"])
            originals.add((row["origin"], row["original_id"]))
            apparent += info.st_size
            generated += row["origin"] == "generated"
            licensed += row["origin"] == "licensed"
            rows += 1
            need(rows <= 200000, "inventory_count_budget")
        observed = set()
        entries = 0
        for root in self.roots.values():
            for directory, directories, filenames in os.walk(root, followlinks=False):
                self.remaining()
                entries += 1 + len(directories) + len(filenames)
                need(entries <= 1000000, "inventory_entry_budget")
                need(all(not (Path(directory) / child).is_symlink() for child in directories), "inventory_symlink_directory")
                for filename in filenames:
                    path = Path(directory) / filename
                    if path.suffix.lower() in MEDIA_EXTENSIONS:
                        need(not path.is_symlink(), "inventory_symlink_file")
                        observed.add(path)
        need(paths == observed and rows > 0, "inventory_incomplete")
        return {"media_paths": rows, "distinct_file_identities": len(inodes), "distinct_content_hashes": len(hashes),
                "declared_original_identities": len(originals), "hardlink_aliases": rows - len(inodes),
                "generated_paths": generated, "licensed_paths": licensed, "apparent_bytes": apparent,
                "allocated_bytes": allocated, "distinct_content_formats": formats, "media_paths_by_format": format_paths,
                "apparent_bytes_by_format": format_bytes, "unique_content_bytes": content_bytes}

    def observer(self):
        previous_cpu, previous_io = None, None
        last_pool_sample = 0
        group = Path(self.c["cgroup_path"])
        while not self.stop.is_set():
            try:
                self.assert_owned()
                observed = self.process_sample()
                now = observed["observed_monotonic_ns"]
                services = {}
                for label, service_group in (("goby", group), ("postgres", Path(self.c["postgres"]["cgroup_path"]))):
                    services[label] = {**cpu_usage_sample(service_group),
                        "io_bytes": sum(int(field.split("=", 1)[1]) for line in (service_group / "io.stat").read_text().splitlines()
                            for field in line.split()[1:] if field.startswith(("rbytes=", "wbytes="))),
                        "memory_bytes": int((service_group / "memory.current").read_text())}
                cpu_percent = service_cpu_percent(previous_cpu, services)
                io_bytes = sum(service["io_bytes"] for service in services.values())
                memory = sum(service["memory_bytes"] for service in services.values())
                current = []
                for observation in observed["processes"]:
                    key = "%d:%s" % (observation["pid"], observation["start_ticks"])
                    arguments, paths = observation["arguments"], observation["source_paths"]
                    current.append(key)
                    process = self.procs.setdefault(key, {"first_ns": now, "last_ns": now, "phase": self.phase,
                        "kind": media_process_kind(arguments), "executable": observation["executable"],
                        "source_paths": paths, "command": arguments, "lineage": observation["lineage"],
                        "cpu_ticks": observation["cpu_ticks"], "work_intervals": []})
                    ticks = observation["cpu_ticks"]
                    if ticks > process["cpu_ticks"] and 0 < now - process["last_ns"] <= self.m["budgets"]["poll_seconds"] * 4e9:
                        process["work_intervals"].append((process["last_ns"], now))
                    process["last_ns"], process["cpu_ticks"] = now, ticks
                    process["source_paths"] = sorted(set(process["source_paths"]) | set(paths))
                row = {"phase": self.phase, "at_ns": now, "memory_bytes": memory, "children": len(current),
                       "services": services,
                       "cpu_percent": cpu_percent, "cpu_sampling": "per_service_read_midpoint",
                       "io_bytes": 0 if previous_io is None else max(0, io_bytes - previous_io)}
                row["scan_spool_generations"] = observed["resource"]["scan_spool_generations"]
                self.resources.append(row)
                self.e.event("resource", **row)
                if now - last_pool_sample >= 1000000000:
                    self.sample_database_pool()
                    last_pool_sample = now
                previous_cpu, previous_io = services, io_bytes
                with self.lock:
                    checkpoint = {key: self.m[key] for key in ("owner_id", "run_id", "profile_id", "source_revision", "tier")}
                    checkpoint.update({"schema_version": VERSION, "phase": self.phase, "at_monotonic_ns": now,
                        "active_lanes": dict(self.lanes), "active_jobs": [{"reference": digest(identity.encode()), **job} for identity, job in self.jobs.items()]})
                self.e.checkpoint(checkpoint)
            except Exception as error:
                self.failures.append(str(error) if isinstance(error, Failure) else "observer_failure")
                self.stop.set()
                return
            self.stop.wait(self.m["budgets"]["poll_seconds"])

    def lane(self, name, action):
        with self.lock:
            self.lanes[name] = {"begun": True, "completed": False, "last_progress_monotonic_ns": time.monotonic_ns()}
        try:
            return action()
        finally:
            with self.lock:
                self.lanes[name]["completed"] = True
                self.lanes[name]["last_progress_monotonic_ns"] = time.monotonic_ns()

    def query_loop(self, worker, done):
        while not done.is_set() and not self.stop.is_set():
            for query in self.c["queries"][self.phase]:
                if done.is_set() or self.stop.is_set():
                    break
                suffix = "/" + query["kind"].capitalize() if query["kind"] in {"resume", "latest"} else ""
                path = "/emby/Users/" + self.c["user_id"] + "/Items" + suffix + "?" + urlencode(query["params"])
                result, _, _, _ = self.http("query-" + query["id"], "GET", path)
                items = result if query["kind"] == "latest" else result.get("Items")
                total = len(items) if query["kind"] == "latest" else result.get("TotalRecordCount")
                need(type(items) is list and total == query["total"] and [item.get("Id") for item in items] == query["ids"], "query_exact_result")
                with self.lock:
                    self.lanes["search-%d" % worker]["last_progress_monotonic_ns"] = time.monotonic_ns()

    def scan(self):
        active = {}
        for library in self.c["scan_libraries"]:
            force = self.phase == "cold"
            intent = self.admission_intent("scan", {"library_id": library["id"], "force_probe": force})
            admitted = self.native("/admin/v1/libraries/" + library["id"] + "/scan", "POST", {"ForceProbe": force}, (202,))
            job = admitted["Job"]
            self.jobs[job["Id"]] = {"kind": "scan", "active": True}
            del self.intents[intent]
            need(job["LibraryId"] == library["id"] and job["ForceProbe"] is force, "scan_admission")
            active[job["Id"]] = library
        deadline = min(self.deadline, time.monotonic() + self.m["budgets"]["phase_seconds"])
        while active:
            need(time.monotonic() < deadline and not self.stop.is_set(), "scan_deadline")
            result = self.native("/admin/v1/jobs")
            jobs = {job["Id"]: job for job in result["Items"]}
            for identity, library in list(active.items()):
                need(identity in jobs, "scan_observation_missing")
                job = jobs[identity]
                self.sample_job("scan", identity, job["Status"] == "running", track=False)
                self.jobs[identity]["active"] = job["Status"] not in TERMINAL
                if job["Status"] in TERMINAL:
                    need(job["Status"] == "completed" and not job["Error"] and all(job[key] == value for key, value in library["expected"][self.phase].items()), "scan_exact_completion")
                    self.e.event("scan_completed", phase=self.phase, reference=digest(identity.encode()), force_probe=force, counters=library["expected"][self.phase])
                    del active[identity]
            time.sleep(self.m["budgets"]["poll_seconds"])

    def analysis(self, kind):
        request_id = "phase3-" + digest(json_bytes({"run": self.m["run_id"], "scope": self.request_scope, "phase": self.phase, "kind": kind}))
        intent = self.admission_intent(kind, {"request_id": request_id})
        result = self.native("/admin/v1/media-analysis/runs", "POST", {"Kind": kind,
            "RequestId": request_id, "LibraryIds": [],
            "ItemIds": self.c["analysis_item_ids"], "Force": True}, (202,))
        identity = result["RunId"]
        self.jobs[identity] = {"kind": kind, "active": True}
        del self.intents[intent]
        while not self.stop.is_set():
            value = self.native("/admin/v1/task-runs/" + identity + "?StartIndex=0&Limit=200")
            state = value["Run"]["State"]
            self.jobs[identity]["active"] = state not in TERMINAL
            self.sample_job(kind + "-admitted", identity, state not in TERMINAL, track=False)
            children = value["Children"]
            need(children["TotalRecordCount"] == len(children["Items"]), "analysis_child_page_incomplete")
            for child in children["Items"]:
                self.sample_job(kind, child["Id"], child["State"] == "running", track=False)
            if state in TERMINAL:
                need(state == "completed" and children["Items"] and all(child["State"] == "completed" and not child["ErrorCode"] for child in children["Items"]), "analysis_completion")
                self.e.artifact("analysis-final.json", json_bytes(value))
                for item in self.c["analysis_item_ids"]:
                    detail = self.native("/admin/v1/media-analysis/items/" + item)
                    if kind == "intro":
                        need(detail["Detection"]["Status"] in {"qualified", "review", "no_result"}, "intro_result_missing")
                    else:
                        need(any(preview["Width"] == 240 and preview["Status"] == "ready" and preview["FrameCount"] > 0 for preview in detail["Previews"]), "preview_result_missing")
                if kind == "previews":
                    self.read_bif(self.c["analysis_item_ids"][0])
                return
            self.remaining()
            time.sleep(self.m["budgets"]["poll_seconds"])
        raise Failure("analysis_interrupted")

    def read_bif(self, item):
        raw, _, _, _ = self.http("bif", "GET", "/emby/Videos/" + item + "/index.bif?Width=240", binary=True)
        import struct
        need(len(raw) >= 80 and raw[:8] == b"\x89BIF\r\n\x1a\n", "bif_header")
        version, count, interval = struct.unpack_from("<III", raw, 8)
        need(version == 0 and 0 < count <= 4096 and 64 + (count + 1) * 8 < len(raw), "bif_count")
        previous = 64 + (count + 1) * 8
        last_timestamp = -1
        for index in range(count):
            timestamp, offset = struct.unpack_from("<II", raw, 64 + index * 8)
            _, end = struct.unpack_from("<II", raw, 72 + index * 8)
            need(timestamp > last_timestamp and offset == previous and offset < end <= len(raw)
                 and raw[offset:offset + 2] == b"\xff\xd8" and raw[end - 2:end] == b"\xff\xd9", "bif_index")
            last_timestamp = timestamp
            if index in {0, count // 2, count - 1}:
                frame = self.e.artifact("bif-frame.jpg", raw[offset:end])
                decoded, _ = self.command("ffmpeg", ["-v", "error", "-nostdin", "-threads", "1", "-i", str(self.e.private / frame["name"]),
                    "-frames:v", "1", "-vf", "scale=64:36", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"])
                need(len(decoded) == 64 * 36 * 3, "bif_frame_decode")
            previous = end
        need(struct.unpack_from("<II", raw, 64 + count * 8) == (0xffffffff, len(raw)), "bif_sentinel")
        self.e.event("bif_decoded", phase=self.phase, count=count, interval_ms=interval or 1000)

    def playback(self, mode):
        play = next(p for p in self.c["playback"] if p["mode"] == mode)
        client = play["client"]
        client_headers = playback_client_headers(client)
        owner = {"auth_session_id": client["auth_session_id"], "device_id": client["device_id"]}
        intent = self.admission_intent("playback", {"item_id": play["item_id"], "user_id": self.c["user_id"], **owner})
        prepared, _, _, _ = self.http(mode + "-prepare", "POST", "/emby/Items/" + play["item_id"] + "/PlaybackInfo",
                                      play["body"], headers=client_headers)
        identity = prepared["PlaySessionId"]
        with self.lock:
            need(identity not in self.plays, "playback_scope_reused")
            self.plays[identity] = {"item_id": play["item_id"], "source_id": "", "mode": mode, **owner}
        del self.intents[intent]
        need(len(prepared["MediaSources"]) == 1 and identity.startswith("play_"), "playback_prepare")
        source = prepared["MediaSources"][0]
        path = source.get("DirectStreamUrl" if mode == "direct" else "TranscodingUrl", "")
        need(path and ".m3u8" not in urlsplit(path).path, "progressive_profile_required")
        with self.lock:
            self.plays[identity] = {"item_id": play["item_id"], "source_id": source["Id"], "mode": mode, **owner}
        self.verify_playback_identity(identity, self.plays[identity])
        report = {"PlaySessionId": identity, "ItemId": play["item_id"], "MediaSourceId": source["Id"], "PositionTicks": 0, "IsPaused": False}
        self.http(mode + "-started", "POST", "/emby/Sessions/Playing", report, statuses=(204,), headers=client_headers)
        for seeking in (False, True):
            current = path
            if mode == "direct":
                size = Path(play["path"]).stat().st_size
                offset = min(size // 2, size - 4096) if seeking else 0
                length = min(size - offset, self.m["budgets"]["max_stream_bytes"], 1 << 20)
                need(offset >= 0 and length >= self.m["thresholds"]["min_media_bytes"], "direct_source_size")
                raw, headers, span, _ = self.http(mode + ("-seek" if seeking else "-start"), "GET", current, binary=True,
                    headers={**client_headers, "Range": "bytes=%d-%d" % (offset, offset + length - 1)}, statuses=(206,))
                with open(play["path"], "rb") as stream:
                    stream.seek(offset)
                    need(raw == stream.read(length), "direct_range_bytes")
                need(headers.get("Content-Range", "").startswith("bytes %d-%d/" % (offset, offset + length - 1)), "direct_range_header")
            else:
                ticks = play["seek_ticks"] if seeking else 0
                current = progressive_request(current, mode, ticks)
                # Alignment negotiation occurs inside this same measured GET;
                # no separate request may hide its cost from seek first-byte time.
                raw, headers, span, ref = self.http(mode + ("-seek" if seeking else "-start"), "GET", current,
                                                 binary=True, headers=client_headers)
                actual_ticks = progressive_start(headers, ticks, mode == "remux" and seeking)
                need(len(raw) >= self.m["thresholds"]["min_media_bytes"], "conversion_empty")
                output = str(self.e.private / ref["name"])
                probe, _ = self.command("ffprobe", ["-v", "error", "-show_streams", "-of", "json", output])
                streams = strict_json(probe)["streams"]
                need(any(stream["codec_type"] == "video" for stream in streams), "video_seek_consumer_required")
                need(sorted(s["codec_name"] for s in streams if s["codec_type"] in {"audio", "video"}) == sorted(play["expected_codecs"]), "actual_output_codecs")
                jobs = self.sql("SELECT COALESCE(json_agg(json_build_object('id',id,'state',state,'plan',plan,'source_stamp',source_stamp,'bytes',output_bytes)),'[]'::json) FROM encoding_jobs WHERE play_session_id=" + sql_string(identity) + " AND item_id=" + sql_string(play["item_id"]) + " AND media_source_id=" + sql_string(source["Id"]) + " AND auth_session_id=" + sql_string(client["auth_session_id"]) + " AND device_id=" + sql_string(client["device_id"]) + " AND user_id=" + sql_string(self.c["user_id"]))
                matching = [job for job in jobs if job["plan"].get("StartTicks") == actual_ticks]
                need(matching, "encoding_database_binding")
                clocks, audio_hashes = [], []
                for job in matching:
                    plan = job["plan"]
                    need(type(plan.get("StartTicks")) is int and type(plan.get("CopyTimestamps", False)) is bool, "encoding_start_clock")
                    codecs = [plan.get(key) for key, stream in (("VideoCodec", "VideoStreamIndex"), ("AudioCodec", "AudioStreamIndex")) if plan.get(stream, -1) >= 0]
                    need(plan.get("OutputMode") == "progressive" and plan.get("Container") == "mp4" and job["source_stamp"]
                         and job["state"] == "completed" and job["bytes"] >= len(raw), "encoding_actual_completion")
                    need(all(codec == "copy" for codec in codecs) if mode == "remux" else any(codec and codec != "copy" for codec in codecs), "encoding_mode_not_exercised")
                    if mode == "remux" and seeking:
                        clock, audio_hash = remux_seek_contract(plan, ticks, actual_ticks)
                        clocks.append(clock)
                        audio_hashes.append(audio_hash)
                    else:
                        clocks.append(plan.get("CopyTimestamps", False))
                need(all(clock is clocks[0] for clock in clocks), "encoding_clock_ambiguous")
                copy_timestamps = clocks[0]
                if mode == "remux" and seeking:
                    need(len(set(audio_hashes)) == 1, "encoding_audio_proof_ambiguous")
                    packet_starts = {}
                    for kind, selector in (("video", "v:0"), ("audio", "a:0")):
                        packet_fields, hash_args = "stream_index,pts,dts", []
                        if kind == "audio":
                            # The MP4 workload's AAC payload is the same AVPacket data
                            # hashed by production's copy/framehash proof. Neither an
                            # ADTS file checksum nor stream extradata is this evidence.
                            packet_fields += ",data_hash"
                            hash_args = ["-show_data_hash", "sha256"]
                        packet_raw, packet_ref = self.command("ffprobe", ["-v", "error", "-select_streams", selector,
                            "-read_intervals", "%+#64", "-show_streams", "-show_packets", *hash_args, "-show_entries",
                            "stream=index,codec_type,time_base:packet=" + packet_fields, "-of", "json", output])
                        packet_starts[kind] = {**copied_packet_start(strict_json(packet_raw), actual_ticks if copy_timestamps else 0,
                            kind, audio_hashes[0] if kind == "audio" else None), "receipt": packet_ref}
                    self.e.event("copied_seek_clock", phase=self.phase, mode=mode, requested_start_ticks=ticks,
                        actual_start_ticks=actual_ticks, copy_timestamps=copy_timestamps, packet_starts=packet_starts)
                args = ["-v", "error", "-nostdin", "-threads", "1"]
                tail = ["-an", "-sn", "-frames:v", "1", "-vf", "scale=64:36", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"]
                original, _ = self.command("ffmpeg", args + ["-ss", str(ticks / 10000000), "-i", play["path"]] + tail, maximum=1 << 20)
                decoded, _ = self.command("ffmpeg", progressive_frame_arguments(output, ticks, copy_timestamps), maximum=1 << 20)
                need(len(original) == len(decoded) == 64 * 36 * 3, "decoded_frame_size")
                mae = sum(abs(a - b) for a, b in zip(original, decoded)) / len(original)
                need(mae <= self.m["thresholds"]["frame_mae"], "seek_frame_mismatch")
                self.e.event("decoded_media", phase=self.phase, mode=mode, seeking=seeking, frame_mae=mae,
                    requested_start_ticks=ticks, actual_start_ticks=actual_ticks, copy_timestamps=copy_timestamps,
                    consumer_target_ticks=ticks)
            self.e.event("playback_bytes", phase=self.phase, mode=mode, seeking=seeking, start_ns=span[0], end_ns=span[1], bytes=len(raw))
        report["PositionTicks"] = play["seek_ticks"]
        self.http(mode + "-progress", "POST", "/emby/Sessions/Playing/Progress", report, statuses=(204,), headers=client_headers)
        # Persisted progress is checked through the real item/user projection.
        detail, _, _, _ = self.http(mode + "-progress-read", "GET", "/emby/Users/" + self.c["user_id"] + "/Items/" + play["item_id"],
                                  headers=client_headers)
        need(detail["UserData"]["PlaybackPositionTicks"] == play["seek_ticks"], "playback_progress_persistence")
        self.close_one_playback(identity)
        self.drain_playback()

    def playback_loop(self, index, mode, done):
        while not done.is_set() and not self.stop.is_set():
            self.playback(mode)
            with self.lock:
                self.lanes["playback-%d-%s" % (index, mode)]["last_progress_monotonic_ns"] = time.monotonic_ns()

    def mutate_files(self):
        mutation = self.c["mutation"]
        old = self.snapshot()
        need(old["move"] and old["delete_id"], "mutation_witnesses")
        self.mutation_before = old
        self.move_user_data = self.sql("SELECT COALESCE(json_agg(row_to_json(u) ORDER BY user_id),'[]'::json) FROM user_item_data u WHERE item_id=" + sql_string(old["move"]["id"]))
        need(self.move_user_data, "move_user_state_witness_required")
        for source_key, target_key in (("delete_path", "quarantine_path"), ("move_from", "move_to"), ("add_from", "add_to")):
            source, target = Path(mutation[source_key]), Path(mutation[target_key])
            marker = Path(self.c["owner_file"]).parent
            if source_key == "add_from":
                need(marker in source.parents and source.resolve() == source and source.is_file(), "addition_owned_staging")
            if target_key == "quarantine_path":
                need(marker in target.parents and target.resolve() == target and not target.exists()
                     and all(root not in target.parents for root in self.roots.values()), "deletion_owned_quarantine")
            need(source.stat().st_nlink == 1 and target.parent.is_dir() and not target.exists(), "mutation_identity")
            stamp = (source.stat().st_dev, source.stat().st_ino)
            source.rename(target)
            self.changed.append((str(source), str(target), stamp))
            need((target.stat().st_dev, target.stat().st_ino) == stamp, "move_identity_changed")
        self.e.event("filesystem_increment", phase=self.phase, deleted=1, added=1, cross_root_moves=1)

    def verify_increment(self):
        mutation, old = self.c["mutation"], self.mutation_before
        value = self.sql("SELECT json_build_object('moved',(SELECT row_to_json(x) FROM (SELECT id,library_id,root_id,path,file_identity FROM items WHERE path=" + sql_string(mutation["move_to"]) + ")x),'deleted',(SELECT count(*) FROM items WHERE id=" + sql_string(old["delete_id"]) + "),'added',(SELECT count(*) FROM items WHERE path=" + sql_string(mutation["add_to"]) + "),'user_data',(SELECT COALESCE(json_agg(row_to_json(u) ORDER BY user_id),'[]'::json) FROM user_item_data u WHERE item_id=" + sql_string(old["move"]["id"]) + "))")
        need(value["deleted"] == 0 and value["added"] == 1 and value["moved"]["id"] == old["move"]["id"]
             and value["moved"]["root_id"] != old["move"]["root_id"] and value["moved"]["file_identity"] == old["move"]["file_identity"]
             and value["user_data"] == self.move_user_data, "increment_identity_or_userdata")
        self.e.event("increment_verified", phase=self.phase, deleted=1, added=1, stable_cross_root_identity=True, user_state_preserved=True)

    def concurrent_metadata_edit(self, metadata_path, metadata, settings, update):
        overview = "Phase 3 concurrent metadata " + self.phase

        def revision(detail):
            raw = detail.get("Revision")
            need(type(raw) is str and re.fullmatch(r"[1-9][0-9]*", raw), "metadata_revision_shape")
            return int(raw)

        def controls_unchanged(before, after, overrides):
            need(after["Item"] == before["Item"], "metadata_edit_identity_changed")
            need(after["Overrides"] == overrides and all(after[key] == before[key] for key in
                 ("LockedFields", "LockedValues", "EditableFields", "InactiveFields")), "metadata_edit_controls_changed")

        def sorting_changed(before, after):
            old, current = before["Automatic"], after["Automatic"]
            need(type(current.get("SortName")) is str and current["SortName"] != old["SortName"]
                 and {key: value for key, value in current.items() if key != "SortName"}
                 == {key: value for key, value in old.items() if key != "SortName"}
                 and after["Effective"]["SortName"] == current["SortName"], "metadata_edit_automatic_sort_changed")

        def edit_body(detail):
            return {"Revision": detail["Revision"], "Overrides": {**detail["Overrides"], "Overview": overview},
                    "LockedFields": list(detail["LockedFields"])}

        def saved_edit(before, after):
            controls_unchanged(before, after, edit_body(before)["Overrides"])
            need(revision(after) == revision(before) + 1 and after["Automatic"] == before["Automatic"]
                 and after["Effective"] == {**before["Effective"], "Overview": overview}, "metadata_edit_save_mismatch")

        need(metadata["Overrides"].get("Overview") != overview and "Overview" not in metadata["LockedFields"]
             and "SortName" not in metadata["Overrides"] and "SortName" not in metadata["LockedFields"], "metadata_edit_witness_controls")
        # The prepared witness has unchanged source bytes and metadata. Only
        # one sorting change and this one manual edit may advance its revision.
        initial_revision = revision(metadata)
        # Keep the original request as the sole metadata overlap witness. A
        # sorting commit may legitimately invalidate its whole-item revision.
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            rebuild = executor.submit(self.http, "sorting-rebuild", "PUT", "/admin/v1/settings", update, True)
            editing = executor.submit(self.http, "metadata-edit", "PUT", metadata_path, edit_body(metadata), True,
                                      statuses=(200, 409), include_status=True)
            saved, first = rebuild.result()[0], editing.result()
        need(saved["Sorting"]["SortRemoveWords"] == update["Sorting"]["SortRemoveWords"]
             and int(saved["Revision"]) == int(settings["Revision"]) + 1, "concurrent_edit_readback")
        edited, _, first_span, first_receipt, status = first
        need(status == 200 or status == 409 and type(edited) is dict and type(edited.get("Error")) is dict
             and edited["Error"].get("Code") == "revision_conflict", "metadata_edit_unexpected_response")
        if status == 409:
            fresh, _, _, fresh_receipt = self.http("metadata-edit-refresh", "GET", metadata_path, admin=True)
            controls_unchanged(metadata, fresh, metadata["Overrides"])
            need(revision(fresh) == initial_revision + 1 and all(fresh[key] == metadata[key] for key in
                 ("LastEditedBy", "LastEditedAt")), "metadata_edit_conflict_changed_manual_state")
            sorting_changed(metadata, fresh)
            need(fresh["Effective"] == {**metadata["Effective"], "SortName": fresh["Automatic"]["SortName"]},
                 "metadata_edit_conflict_changed_effective_state")
            self.e.event("metadata_edit_conflict", phase=self.phase, start_ns=first_span[0], end_ns=first_span[1],
                         code="revision_conflict", rejected_revision=metadata["Revision"], current_revision=fresh["Revision"],
                         rejected_body=first_receipt, current_body=fresh_receipt, manual_state_unchanged=True)
            # One fresh CAS is allowed after proving the rejected request left
            # manual state intact. Any further conflict fails the workload.
            edited, _, _, _, rebased_status = self.http("metadata-edit-rebased", "PUT", metadata_path,
                                                       edit_body(fresh), admin=True, include_status=True)
            need(rebased_status == 200, "metadata_edit_rebase_failed")
            saved_edit(fresh, edited)
        else:
            saved_edit(metadata, edited)
        final, _, _, final_receipt = self.http("metadata-edit-readback", "GET", metadata_path, admin=True)
        controls_unchanged(metadata, final, edit_body(metadata)["Overrides"])
        sorting_changed(metadata, final)
        need(revision(final) == initial_revision + 2 and final["Effective"] ==
             {**metadata["Effective"], "Overview": overview, "SortName": final["Automatic"]["SortName"]}
             and all(final[key] == edited[key] for key in ("LastEditedBy", "LastEditedAt")), "metadata_edit_final_readback")
        if status == 409:
            need(final["Automatic"] == fresh["Automatic"], "metadata_edit_rebase_overwrote_automatic")
        self.e.event("metadata_edit_verified", phase=self.phase, first_status=status, rebased=status == 409,
                     initial_revision=metadata["Revision"], final_revision=final["Revision"], final_body=final_receipt)
        return final

    def edits(self):
        settings = self.native("/admin/v1/settings")
        metadata_path = "/admin/v1/items/" + self.c["mutation"]["metadata_item_id"] + "/metadata"
        metadata = self.native(metadata_path)
        if self.settings_before is None:
            self.settings_before, self.metadata_before = settings, metadata
        words = self.c["mutation"]["sort_words"]
        if words == settings["Sorting"]["SortRemoveWords"]:
            words = []
        update = {"Revision": settings["Revision"], "Overrides": settings["Overrides"], "Sorting": {"SortRemoveWords": words}}
        before = self.snapshot()
        # Lock only the declared sorting witness. A global journal lock would
        # also make unrelated scan publications fail, invalidating this load
        # experiment. The sorting transaction has already updated its settings
        # when its real rebuild reaches this item, so rollback is meaningful.
        environment = {"PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", **self.c["pg_env"]}
        holder = subprocess.Popen([self.c["tools"]["psql"], "-X", "-q", "-A", "-t", "-v", "ON_ERROR_STOP=1"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=environment, start_new_session=True, bufsize=0)
        try:
            lock_statement = ("BEGIN; SET idle_in_transaction_session_timeout='120s'; SELECT id FROM items WHERE id="
                + sql_string(self.c["mutation"]["metadata_item_id"]) + " FOR UPDATE; SELECT 'PHASE3_READY';\n")
            holder.stdin.write(lock_statement.encode())
            holder.stdin.flush()
            ready, deadline = False, min(self.deadline, time.monotonic() + 10)
            while time.monotonic() < deadline:
                readable, _, _ = select.select([holder.stdout], [], [], .1)
                if readable:
                    line = holder.stdout.readline(1024)
                    if line.strip() == b"PHASE3_READY":
                        ready = True
                        break
                    need(line, "lock_holder_exit")
            need(ready, "lock_holder_not_ready")
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
                pending = executor.submit(self.http, "sorting-timeout", "PUT", "/admin/v1/settings", update, True, (503,))
                waited = False
                while not pending.done():
                    observation = self.owner()
                    waited |= observation["waiting"] is True
                    self.remaining()
                    time.sleep(self.m["budgets"]["poll_seconds"])
                pending.result()
            need(waited, "actual_owner_lock_wait_missing")
            after = self.snapshot(before["captured_at"])
            need(before["sort_scope_count"] >= self.m["tier"] // 2 and before["sort_scope_count"] == after["sort_scope_count"]
                 and before["owner"] == after["owner"] and before["settings"] == after["settings"] and before["sort_digest"] == after["sort_digest"], "sorting_timeout_partial_commit_or_owner_loss")
            self.e.event("sorting_rollback", phase=self.phase, actual_lock_wait=True, settings_unchanged=True, keys_unchanged=True, owner_survived=True)
        finally:
            if holder.poll() is None:
                try:
                    holder.stdin.write(b"ROLLBACK;\n\\q\n")
                    holder.stdin.flush()
                    holder.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    os.killpg(holder.pid, signal.SIGKILL)
                    holder.wait(timeout=5)
            holder.stdin.close()
            holder.stdout.close()
            holder.stderr.close()
        # The settings CAS survives rollback; metadata keeps its separate
        # source-sensitive revision while the actual rebuild runs concurrently.
        edited = self.concurrent_metadata_edit(metadata_path, metadata, settings, update)
        after = self.snapshot()
        need(after["owner"] == before["owner"], "catalog_owner_changed")
        need(before["sort_witness"] is not None and after["sort_witness"] != before["sort_witness"]
             and after["sort_witness"] == edited["Effective"]["SortName"], "sorting_rebuild_no_effect")

    def run_phase(self, phase):
        with self.pool_lock:
            self.phase, self.lanes = phase, {}
        self.phase_deadline = min(self.deadline, time.monotonic() + self.m["budgets"]["phase_seconds"])
        self.sample_database_pool("phase-start")
        # The fault wrapper calls a single real phase without execute(). It
        # must still bind analysis worker descriptors to these selected items.
        if not self.analysis_paths:
            paths = self.sql("SELECT COALESCE(json_agg(path),'[]'::json) FROM items WHERE id IN (" + ",".join(sql_string(identity) for identity in self.c["analysis_item_ids"]) + ")")
            need(len(paths) == len(self.c["analysis_item_ids"]), "analysis_source_binding")
            self.analysis_paths = {str(self.owned_media(path)) for path in paths}
        if phase == "incremental":
            self.mutate_files()
        done = threading.Event()
        workers = self.m["concurrency"]["query_workers"]
        playback_workers = self.m["concurrency"]["playback_workers"]
        started = time.monotonic_ns()
        with concurrent.futures.ThreadPoolExecutor(max_workers=workers + playback_workers + 4) as executor:
            readers = [executor.submit(self.lane, "search-%d" % index, lambda index=index: self.query_loop(index, done)) for index in range(workers)]
            tasks = [executor.submit(self.lane, "intro_analysis", lambda: self.analysis("intro")),
                     executor.submit(self.lane, "preview", lambda: self.analysis("previews")),
                     executor.submit(self.lane, "editing", self.edits)]
            players = []
            for index in range(playback_workers):
                mode = ("direct", "remux", "transcode")[index % 3]
                players.append(executor.submit(self.lane, "playback-%d-%s" % (index, mode), lambda index=index, mode=mode: self.playback_loop(index, mode, done)))
            scanning = executor.submit(self.lane, "scan", self.scan)
            try:
                pending = [scanning, *tasks]
                phase_end = min(self.deadline, time.monotonic() + self.m["budgets"]["phase_seconds"])
                while pending:
                    need(time.monotonic() < phase_end and not self.stop.is_set(), "phase_deadline")
                    for future in readers + players + pending:
                        if future.done():
                            future.result()
                    pending = [future for future in pending if not future.done()]
                    time.sleep(.05)
            except Exception:
                self.stop.set()
                raise
            finally:
                done.set()
                for future in readers + players:
                    future.result()
        elapsed = (time.monotonic_ns() - started) / 1e9
        need(elapsed <= self.m["thresholds"]["scan_seconds"], "scan_latency_threshold")
        if phase == "incremental":
            self.verify_increment()
        catalog = self.snapshot()
        need(catalog["catalog_count"] == self.c["catalog_expected"][phase], "settled_catalog_exact_count")
        self.e.event("catalog_exact_count", phase=phase, count=catalog["catalog_count"], file_items=catalog["file_items"], nonfile_items=catalog["nonfile_items"])
        self.check_phase(phase)
        self.close_playback()
        self.sample_database_pool("phase-end")
        need({sample["boundary"] for sample in self.pool_samples if sample["phase"] == phase} >= {"phase-start", "phase-end"}, "database_pool_phase_boundaries")
        self.phase_deadline = None

    def check_phase(self, phase):
        events = [event for event in self.e.events if event.get("phase") == phase]
        http = [event for event in events if event["kind"] == "http"]
        gaps = int(self.m["budgets"]["poll_seconds"] * 4e9)
        intervals = {}
        for kind in ("scan", "intro", "previews", "intro-admitted", "previews-admitted"):
            intervals[kind] = [span for (p, k, _), observations in self.samples.items() if p == phase and k == kind
                               for span in confirmed_intervals(observations, gaps)]
            need(intervals[kind], "running_observation_missing_" + kind)
        query = [(event["start_ns"], event["end_ns"]) for event in http if event["label"].startswith("query-") and not event["error"]]
        play = [(event["start_ns"], event["end_ns"]) for event in events if event["kind"] == "playback_bytes"]
        # Shared analysis admission may intentionally serialize intro and BIF.
        # Prove simultaneous admitted runs AND actual productive overlap for
        # each feature separately; never call two waiting parent runs workers.
        need(all_overlap([intervals["intro-admitted"], intervals["previews-admitted"]]) > 0, "analysis_concurrent_admission_missing")
        overlap_ms = {}
        for kind in ("intro", "previews"):
            process_intervals = [span for proc in list(self.procs.values()) if proc["phase"] == phase and proc["kind"] == kind
                                 and self.analysis_paths.intersection(proc["source_paths"]) for span in proc["work_intervals"]]
            overlap_ms[kind] = all_overlap([intervals["scan"], intervals[kind], process_intervals, query, play]) / 1e6
            need(overlap_ms[kind] >= self.m["thresholds"]["min_all_lane_overlap_ms"], "actual_compound_overlap_missing_" + kind)
        need(any(row["phase"] == phase and row["scan_spool_generations"] > 0 for row in self.resources), "actual_scan_spool_not_observed")
        for kind in QUERY_KINDS:
            ids = {q["id"] for q in self.c["queries"][phase] if q["kind"] == kind}
            selected = [event for event in http if event["label"] in {"query-" + identity for identity in ids}]
            need(len(selected) >= self.m["thresholds"]["min_requests_per_query"], "query_sample_count")
            need(any(interval_overlap((event["start_ns"], event["end_ns"]), span) for event in selected for span in intervals["scan"]), "query_scan_overlap_missing")
        for mode in PLAY_MODES:
            for seeking in (False, True):
                selected = [event for event in events if event["kind"] == "playback_bytes" and event["mode"] == mode and event["seeking"] == seeking]
                need(selected and any(interval_overlap((event["start_ns"], event["end_ns"]), span) for event in selected for span in intervals["scan"]), "playback_scan_overlap_missing")
        for mode in ("remux", "transcode"):
            source = next(play["path"] for play in self.c["playback"] if play["mode"] == mode)
            processes = [proc for proc in list(self.procs.values()) if proc["phase"] == phase and source in proc["source_paths"] and proc["executable"] == self.c["tools"]["ffmpeg"]]
            need(any(proc["work_intervals"] for proc in processes), "actual_media_child_missing")
        sorting = [(event["start_ns"], event["end_ns"]) for event in http if event["label"] == "sorting-rebuild"]
        editing = [(event["start_ns"], event["end_ns"]) for event in http if event["label"] == "metadata-edit"]
        need(all_overlap([sorting, editing, intervals["scan"]]) > 0, "sorting_metadata_scan_overlap_missing")
        self.e.event("compound_overlap", phase=phase, productive_overlap_ms=overlap_ms, analysis_admission_overlapped=True)

    def close_playback(self):
        for identity in list(self.plays):
            self.close_one_playback(identity)

    def verify_playback_identity(self, identity, play):
        observed = self.sql("SELECT row_to_json(p) FROM (SELECT id,user_id,auth_session_id,device_id,item_id,media_source_id "
            "FROM play_sessions WHERE id=" + sql_string(identity) + ") p")
        need(observed == {"id": identity, "user_id": self.c["user_id"], "auth_session_id": play["auth_session_id"],
             "device_id": play["device_id"], "item_id": play["item_id"], "media_source_id": play["source_id"]},
             "playback_authentication_binding")

    def close_one_playback(self, identity):
        play = self.plays[identity]
        binding = next(p for p in self.c["playback"] if p["mode"] == play["mode"])
        client = binding["client"]
        need(all(play[key] == client[key] for key in ("auth_session_id", "device_id")), "playback_client_changed")
        headers = playback_client_headers(client)
        self.http("playback-stop", "POST", "/emby/Sessions/Playing/Stopped", {"PlaySessionId": identity,
            "ItemId": play["item_id"], "MediaSourceId": play["source_id"], "PositionTicks": binding["seek_ticks"]},
            statuses=(204,), headers=headers)
        self.http("encoding-stop", "DELETE", "/emby/Videos/ActiveEncodings?" + urlencode({"PlaySessionId": identity,
            "DeviceId": client["device_id"]}), statuses=(204,), headers=headers)
        del self.plays[identity]
        self.closed_plays[identity] = play

    def drain_playback(self):
        deadline = min(self.deadline, time.monotonic() + 20)
        while self.closed_plays:
            need(time.monotonic() < deadline, "owned_encoders_not_drained")
            for identity, play in list(self.closed_plays.items()):
                active = self.sql("SELECT count(*) FROM encoding_jobs WHERE play_session_id=" + sql_string(identity) + " AND state IN ('queued','running')")
                source = next(p["path"] for p in self.c["playback"] if p["mode"] == play["mode"])
                live = False
                for key, process in list(self.procs.items()):
                    if process["kind"] != "playback" or source not in process["source_paths"]:
                        continue
                    pid, birth = key.split(":")
                    try:
                        fields = Path("/proc/" + pid + "/stat").read_text().rsplit(")", 1)[1].split()
                        live |= fields[19] == birth and fields[0] != "Z"
                    except FileNotFoundError:
                        pass
                if active == 0 and not live:
                    self.closed_plays.pop(identity, None)
            if self.closed_plays:
                time.sleep(.1)

    def drain_jobs(self):
        deadline = min(self.deadline, time.monotonic() + 60)
        while any(job["active"] for job in self.jobs.values()):
            need(time.monotonic() < deadline, "owned_jobs_not_drained")
            scans = None
            for identity, job in list(self.jobs.items()):
                if not job["active"]:
                    continue
                if job["kind"] == "scan":
                    if scans is None:
                        scans = {row["Id"]: row for row in self.native("/admin/v1/jobs")["Items"]}
                    need(identity in scans, "cleanup_scan_missing")
                    state = scans[identity]["Status"]
                else:
                    state = self.native("/admin/v1/task-runs/" + identity + "?StartIndex=0&Limit=200")["Run"]["State"]
                job["active"] = state not in TERMINAL
            time.sleep(1)

    def cleanup(self):
        self.cleanup_mode = True
        self.e.closing = True
        self.phase_deadline = None
        self.deadline = time.monotonic() + self.m["budgets"]["cleanup_seconds"]
        errors = []
        def attempt(action):
            try:
                action()
            except Exception as error:
                errors.append(str(error) if isinstance(error, Failure) else "cleanup_operation")
        for identity, job in list(self.jobs.items()):
            if job["active"]:
                path = "/admin/v1/jobs/" + identity + "/cancel" if job["kind"] == "scan" else "/admin/v1/task-runs/" + identity + "/cancel"
                attempt(lambda path=path: self.native(path, "POST", {}, (200, 202, 204, 409)))
        attempt(self.drain_jobs)
        attempt(self.close_playback)
        attempt(self.drain_playback)
        if self.metadata_before:
            def restore_metadata():
                path = "/admin/v1/items/" + self.c["mutation"]["metadata_item_id"] + "/metadata"
                current = self.native(path)
                self.native(path, "PUT", {"Revision": current["Revision"], "Overrides": self.metadata_before["Overrides"], "LockedFields": self.metadata_before["LockedFields"]})
            attempt(restore_metadata)
        if self.settings_before:
            def restore_settings():
                current = self.native("/admin/v1/settings")
                self.native("/admin/v1/settings", "PUT", {"Revision": current["Revision"], "Overrides": self.settings_before["Overrides"], "Sorting": self.settings_before["Sorting"]})
            attempt(restore_settings)
        unresolved = bool(self.intents or self.plays or self.closed_plays or self.unresolved_brokers or any(job["active"] for job in self.jobs.values()))
        for source, target, identity in reversed(self.changed if not unresolved else []):
            def restore_file(source=Path(source), target=Path(target), identity=identity):
                self.assert_owned()
                need(not source.exists() and (target.stat().st_dev, target.stat().st_ino) == identity, "cleanup_file_identity")
                target.rename(source)
            attempt(restore_file)
        self.stop.set()
        if unresolved:
            errors.append("filesystem_restore_held_for_active_worker")
        if self.intents:
            errors.append("unresolved_admission_requires_controller_reconciliation")
            self.e.artifact("unresolved-admissions.json", json_bytes(self.intents))
        if self.unresolved_brokers:
            errors.append("root_observer_failure_requires_external_closure_review")
            self.e.artifact("unresolved-root-observers.json", json_bytes(self.unresolved_brokers))
        return errors

    def summarize(self, complete, corpus, baseline, cleanup_errors):
        events = [event for event in self.e.events if event["kind"] == "http"]
        def measurements(selected_events):
            result = {}
            for label in sorted({event["label"] for event in selected_events}):
                selected = [event for event in selected_events if event["label"] == label]
                result[label] = {"latency_ms": distribution([(event["end_ns"] - event["start_ns"]) / 1e6 for event in selected]),
                    "first_byte_ms": distribution([(event["first_byte_ns"] - event["start_ns"]) / 1e6 for event in selected if event["first_byte_ns"] is not None]),
                    "failures": sum(bool(event["error"]) for event in selected)}
            return result
        metrics = measurements(events)
        phase_metrics = {phase: measurements([event for event in events if event["phase"] == phase]) for phase in PHASES}
        peak = {"memory_bytes": max((row["memory_bytes"] for row in self.resources), default=None),
                "cpu_percent": max((row["cpu_percent"] for row in self.resources if row["cpu_percent"] is not None), default=None),
                "children": max((row["children"] for row in self.resources), default=None),
                "io_bytes": sum(row["io_bytes"] for row in self.resources)}
        checks = {"cleanup": not cleanup_errors, "no_observer_failures": not self.failures}
        thresholds = self.m["thresholds"]
        for key, threshold in (("memory_bytes", "max_memory_bytes"), ("cpu_percent", "max_cpu_percent"), ("children", "max_children"), ("io_bytes", "max_io_bytes")):
            checks[key] = peak[key] is not None and peak[key] <= thresholds[threshold]
        for phase, values in phase_metrics.items():
            for label, metric in values.items():
                if label.startswith("query-"):
                    checks[phase + ":" + label] = metric["latency_ms"]["p95"] <= thresholds["query_p95_ms"] and metric["latency_ms"]["p99"] <= thresholds["query_p99_ms"]
                if label in {mode + suffix for mode in PLAY_MODES for suffix in ("-start", "-seek")}:
                    checks[phase + ":" + label] = metric["first_byte_ms"]["p95"] is not None and metric["first_byte_ms"]["p95"] <= thresholds["seek_p95_ms" if label.endswith("-seek") else "playback_start_p95_ms"]
        checks["http_failures"] = sum(metric["failures"] for metric in metrics.values()) <= thresholds["max_failures"]
        accepted = complete and all(checks.values())
        result = {key: self.m[key] for key in ("run_id", "owner_id", "source_revision", "profile_id", "tier", "guest", "resource_envelope", "concurrency", "thresholds")}
        try:
            pool_report = {"observed": database_pool_summary(self.pool_samples),
                           "phases": {phase: database_pool_summary([sample for sample in self.pool_samples if sample["phase"] == phase]) for phase in ("idle", *PHASES)}}
        except Failure as error:
            accepted = False
            checks["database_pool_monotonic"] = False
            self.failures.append(str(error))
            pool_report = {"available": False, "error": str(error), "samples": len(self.pool_samples)}
        result.update({"schema_version": VERSION, "accepted": accepted, "execution_complete": complete,
            "manifest_sha256": self.c["manifest_sha256"], "driver_sha256": self.c["driver_sha256"],
            "actual_corpus": corpus, "idle_baseline": baseline, "metrics": metrics, "phase_metrics": phase_metrics, "resource_peaks": peak,
            "checks": checks, "failure_codes": self.failures, "cleanup_errors": cleanup_errors,
            "supported_concurrency": self.m["concurrency"] if accepted else None,
            "scan_work": {phase: {"completed_scans": [event for event in self.e.events if event["kind"] == "scan_completed" and event["phase"] == phase],
                "observed_ffprobe_processes_lower_bound": sum(process["phase"] == phase and process["executable"] == self.c["tools"]["ffprobe"] for process in self.procs.values()),
                "process_sampling_is_not_total_probe_count": True} for phase in PHASES},
            "database_pool": {"source": "/admin/v1/runtime/resources.DatabasePool", "counters_include_observer_authentication": True,
                "empty_waits_include_connection_construction": True, **pool_report},
            "consumer_scope": "Production HTTP and real media decoders; browser UX and power-loss durability are separate acceptance scopes.",
            "resource_closure": "Workload jobs and file mutations only; external controller must independently close credentials, services, PostgreSQL and VM resources."})
        self.e.artifact("process-observations.json", json_bytes(self.procs))
        self.e.artifact("job-observations.json", json_bytes({"jobs": self.jobs, "samples": [{"phase": p, "kind": k, "id": identity, "samples": rows} for (p, k, identity), rows in self.samples.items()]}))
        self.e.atomic("result.json", result)
        return accepted

    def execute(self):
        corpus, baseline, complete = None, None, False
        observer = None
        try:
            need(self.c["scan_evidence_sha256"] == self.m["scan_evidence"]["configuration_sha256"], "scan_evidence_configuration_identity")
            self.process_sample()
            envelope = self.m["resource_envelope"]
            need(os.cpu_count() == envelope["cpu_count"], "cpu_envelope_changed")
            memory = os.sysconf("SC_PHYS_PAGES") * os.sysconf("SC_PAGE_SIZE")
            need(.9 * envelope["memory_bytes"] <= memory <= envelope["memory_bytes"], "memory_envelope_changed")
            volume = os.statvfs(next(iter(self.roots.values())))
            need(.8 * envelope["disk_bytes"] <= volume.f_blocks * volume.f_frsize <= envelope["disk_bytes"], "disk_envelope_changed")
            initial = self.snapshot()
            need(initial["catalog_count"] == self.c["catalog_expected"]["initial"], "initial_catalog_exact_count")
            need(initial["postgres_version"] == self.m["resource_envelope"]["postgres_version"], "postgres_version_changed")
            paths = self.sql("SELECT COALESCE(json_agg(path),'[]'::json) FROM items WHERE id IN (" + ",".join(sql_string(identity) for identity in self.c["analysis_item_ids"]) + ")")
            need(len(paths) == len(self.c["analysis_item_ids"]), "analysis_source_binding")
            self.analysis_paths = {str(self.owned_media(path)) for path in paths}
            corpus = {**self.inventory(), "initial_catalog_items": initial["catalog_count"], "initial_file_items": initial["file_items"], "initial_nonfile_items": initial["nonfile_items"]}
            observer = threading.Thread(target=self.observer)
            observer.start()
            self.phase = "idle"
            self.sample_database_pool("idle-start")
            time.sleep(self.m["budgets"]["idle_seconds"])
            self.sample_database_pool("idle-end")
            baseline = {"samples": len(self.resources), "memory_bytes": distribution([row["memory_bytes"] for row in self.resources]),
                        "cpu_percent": distribution([row["cpu_percent"] for row in self.resources if row["cpu_percent"] is not None])}
            for phase in PHASES:
                self.run_phase(phase)
            complete = True
        except Exception as error:
            self.failures.append(str(error) if isinstance(error, Failure) else "unexpected_workload_failure")
        finally:
            cleanup_errors = self.cleanup()
            if observer:
                observer.join(timeout=10)
                if observer.is_alive():
                    cleanup_errors.append("observer_not_joined")
        return self.summarize(complete, corpus, baseline, cleanup_errors)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--context", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args(argv)
    need(sys.platform == "linux" and os.getuid() != 0, "isolated_linux_unprivileged_actor_required")
    manifest_raw = read_private(args.manifest, 1 << 20)
    manifest = validate_manifest(strict_json(manifest_raw))
    context = strict_json(read_private(args.context))
    need(context.get("manifest_sha256") == digest(manifest_raw)
         and context.get("driver_sha256") == digest(Path(__file__).read_bytes()), "frozen_input_identity")
    evidence = Evidence(args.output, manifest)
    evidence.atomic("manifest.json", manifest)
    return 0 if Actor(manifest, context, evidence).execute() else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(json.dumps({"accepted": False, "error_code": str(error) if isinstance(error, Failure) else "admission_failure"}))
        sys.exit(1)
