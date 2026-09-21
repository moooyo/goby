#!/usr/bin/env python3
"""Real bounded HTTP/media observations for the isolated Phase 3 controller.

Importing performs no I/O. A successful RPC means observations were collected;
the external oracle, not this actor, decides whether a fault/recovery passed.
"""

import argparse
from decimal import Decimal, InvalidOperation
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import select
import signal
import socket
import stat
import subprocess
import sys
import time
from urllib.parse import parse_qs, urlencode, urlsplit


SAFE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}\Z")
SHA = re.compile(r"[a-f0-9]{64}\Z")
METHODS = {"healthy_probes", "affected_probe", "playback_resume", "runtime_resources", "readiness"}


class Failure(RuntimeError):
    pass


def need(value, code):
    if not value:
        raise Failure(code)


def encode(value):
    return (json.dumps(value, sort_keys=True, ensure_ascii=True, allow_nan=False, separators=(",", ":")) + "\n").encode()


def sha(value):
    return hashlib.sha256(value).hexdigest()


def decode(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            need(key not in result, "duplicate_json_key")
            result[key] = value
        return result
    return json.loads(raw.decode("utf-8"), object_pairs_hook=pairs,
                      parse_constant=lambda _: (_ for _ in ()).throw(Failure("nonfinite_json")))


def private(path, cap):
    path = Path(path)
    need(path.is_absolute() and path.resolve() == path, "private_path")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(fd)
        need(stat.S_ISREG(before.st_mode) and before.st_uid == os.getuid() and before.st_nlink == 1
             and stat.S_IMODE(before.st_mode) == 0o600 and 0 < before.st_size <= cap, "private_file")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            raw = stream.read(cap + 1)
        after = os.fstat(fd)
        need(len(raw) == before.st_size and (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns)
             == (after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns), "private_file_changed")
        return decode(raw)
    finally:
        os.close(fd)


def identity(pid):
    raw = Path("/proc/%d/stat" % pid).read_text().rsplit(")", 1)[1].split()
    return {"pid": pid, "start_ticks": raw[19], "process_group": int(raw[2])}


def group_members(group):
    found = []
    for path in Path("/proc").iterdir():
        if path.name.isdigit():
            try:
                row = identity(int(path.name))
                if row["process_group"] == group:
                    found.append(row)
            except (FileNotFoundError, ProcessLookupError):
                pass
    return found


def owned_command(tool, arguments, timeout, stdout_cap):
    path = Path(tool["path"])
    need(path.is_absolute() and path.resolve() == path and SHA.fullmatch(tool["sha256"]), "tool_binding")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    process = None
    try:
        fact = os.fstat(fd)
        need(stat.S_ISREG(fact.st_mode) and fact.st_uid == 0 and not stat.S_IMODE(fact.st_mode) & 0o022, "tool_identity")
        with os.fdopen(fd, "rb", closefd=False) as stream:
            need(hashlib.file_digest(stream, "sha256").hexdigest() == tool["sha256"], "tool_hash")
        started = time.time_ns()
        process = subprocess.Popen([str(path), *arguments], executable="/proc/self/fd/%d" % fd,
            pass_fds=(fd,), stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            start_new_session=True, env={"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C"})
        owner = identity(process.pid)
        buffers = {process.stdout.fileno(): bytearray(), process.stderr.fileno(): bytearray()}
        limits = {process.stdout.fileno(): stdout_cap, process.stderr.fileno(): 256 << 10}
        active = set(buffers)
        until = time.monotonic() + timeout
        while active:
            need(time.monotonic() < until, "media_child_timeout")
            ready, _, _ = select.select(list(active), [], [], min(0.2, max(0, until-time.monotonic())))
            for stream in ready:
                chunk = os.read(stream, 65536)
                if not chunk:
                    active.remove(stream)
                    continue
                need(len(chunk) <= limits[stream]-len(buffers[stream]), "media_child_output_budget")
                buffers[stream].extend(chunk)
        code = process.wait(timeout=max(0.01, until-time.monotonic()))
        need(code == 0 and not group_members(process.pid), "media_child_not_successfully_closed")
        raw = bytes(buffers[process.stdout.fileno()])
        return raw, {**owner, "started_unix_ns": started, "completed_unix_ns": time.time_ns(),
                     "exit_code": code, "process_group_closed": True, "stdout_sha256": sha(raw),
                     "stderr_sha256": sha(bytes(buffers[process.stderr.fileno()]))}
    finally:
        if process is not None:
            if process.poll() is None:
                # This group was created by this invocation and is never retried.
                try:
                    need(identity(process.pid) == owner, "media_child_identity_changed")
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait(timeout=5)
            need(not group_members(process.pid), "media_child_cleanup_incomplete")
            process.stdout.close()
            process.stderr.close()
        os.close(fd)


class Probe:
    def __init__(self, binding):
        self.c = binding
        need(os.geteuid() == 0 and binding["schema_version"] == 1 and binding["vmid"] == 106, "guest_binding")
        need(Path("/etc/machine-id").read_text().strip() == binding["machine_id"], "guest_machine_changed")
        need(Path("/sys/class/dmi/id/product_uuid").read_text().strip().lower() == binding["smbios_uuid"].lower(), "guest_uuid_changed")
        marker = private(binding["owner_file"], 65536)
        need(marker.get("owner_id") == binding["owner_id"], "owner_marker_changed")
        self.origin = urlsplit(binding["origin"])
        need(self.origin.scheme == "http" and self.origin.hostname in ("127.0.0.1", "::1")
             and self.origin.port and self.origin.path == "" and not self.origin.query and not self.origin.fragment
             and not self.origin.username, "probe_origin")
        self.artifacts = Path(binding["artifacts_directory"])
        info = self.artifacts.lstat()
        need(self.artifacts.is_absolute() and self.artifacts.resolve() == self.artifacts and stat.S_ISDIR(info.st_mode)
             and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o700, "probe_artifacts")
        self.roots = {row["root_id"]: row for row in binding["roots"]}
        need(1 <= len(self.roots) == len(binding["roots"]) <= 16, "probe_roots")
        for root in self.roots.values():
            for key in ("root_id", "library_id", "item_id", "media_source_id", "item_id_ref"):
                need(type(root[key]) is str and SAFE.fullmatch(root[key]), "probe_root_identifier")
            for key in ("lock_item_id", "preview_item_id"):
                if key in root:
                    need(type(root[key]) is str and SAFE.fullmatch(root[key]), "probe_fault_item_identifier")
            need(SHA.fullmatch(root["source_sha256"]) and 0 < root["source_bytes"] <= 128 << 20, "probe_media_bound")
            need(type(root["source_bytes"]) is int and type(root["source_clock_origin_ticks"]) is int
                 and type(root["tolerance_ticks"]) is int and 0 <= root["tolerance_ticks"] <= 30000000, "probe_clock_bound")
            need(root["container"] in {"mp4", "mkv", "webm", "mov"}, "probe_video_container")
            source = root["source_identity"]
            need(set(source) == {"path", "device", "inode", "mount_id"} and Path(source["path"]).is_absolute()
                 and all(type(source[key]) is int and source[key] >= 0 for key in ("device", "inode", "mount_id")), "expected_source_identity")
            need(type(root["search_params"]) is dict and len(root["search_params"]) <= 20, "probe_search")
            need(len(root["search_ids"]) <= 200, "probe_search_page")
        for value in binding["credentials"].values():
            need(type(value) is str and len(value) <= 8192 and not any(ch in value for ch in "\r\n\x00"), "probe_credential")
        self.receipts = []
        self.action = ""
        self.assert_application()

    def assert_application(self):
        unit = self.c["service"]["unit"]
        need(SAFE.fullmatch(unit), "application_unit")
        result = subprocess.run(["systemctl", "show", unit, "-p", "MainPID", "-p", "ControlGroup", "-p", "ActiveState"],
                                capture_output=True, timeout=10, check=True)
        values = dict(line.split("=", 1) for line in result.stdout.decode().splitlines() if "=" in line)
        need(values.get("ActiveState") == "active" and int(values.get("MainPID", "0")) > 1, "application_not_running")
        pid = int(values["MainPID"])
        before = identity(pid)
        with Path("/proc/%d/exe" % pid).open("rb") as stream:
            need(hashlib.file_digest(stream, "sha256").hexdigest() == self.c["service"]["executable_sha256"], "application_source_changed")
        need(identity(pid) == before and "0::" + values["ControlGroup"]
             in Path("/proc/%d/cgroup" % pid).read_text().splitlines(), "application_instance_changed")
        previous = getattr(self, "application", None)
        need(previous is None or previous == before, "application_restarted_during_probe")
        self.application = before

    def request(self, method, path, body=None, native=False, cap=4 << 20, timeout=10, sink=None, headers=None):
        need((path == "/readyz" or path.startswith(("/admin/v1/", "/emby/")))
             and "\r" not in path and "\n" not in path, "probe_route")
        credentials = self.c["credentials"]
        request_headers = {"Accept": "application/json", "Connection": "close"}
        if native:
            request_headers.update({"Cookie": credentials["admin_cookie"], "X-CSRF-Token": credentials["csrf_token"]})
        else:
            request_headers.update({"X-Emby-Token": credentials["emby_token"], "X-Emby-Authorization": credentials["emby_authorization"]})
        request_headers.update(headers or {})
        raw_body = encode(body) if body is not None else None
        if body is not None:
            request_headers["Content-Type"] = "application/json"
        connection = http.client.HTTPConnection(self.origin.hostname, self.origin.port, timeout=timeout)
        started, began = time.time_ns(), time.monotonic_ns()
        status, request_id, total, timed_out, disconnected = None, None, 0, False, False
        hasher, chunks = hashlib.sha256(), []
        try:
            connection.connect()
            transport_socket = connection.sock
            remaining = timeout - (time.monotonic_ns()-began) / 1e9
            if remaining <= 0:
                raise TimeoutError
            transport_socket.settimeout(remaining)
            connection.request(method, path, body=raw_body, headers=request_headers)
            remaining = timeout - (time.monotonic_ns()-began) / 1e9
            if remaining <= 0:
                raise TimeoutError
            transport_socket.settimeout(remaining)
            response = connection.getresponse()
            status, request_id = response.status, response.getheader("X-Request-Id")
            while True:
                if response.isclosed():
                    break
                remaining = timeout - (time.monotonic_ns()-began) / 1e9
                if remaining <= 0:
                    timed_out = True
                    break
                transport_socket.settimeout(remaining)
                chunk = response.read(min(65536, cap-total+1))
                if not chunk:
                    break
                need(len(chunk) <= cap-total, "http_body_budget")
                total += len(chunk)
                hasher.update(chunk)
                if sink is not None:
                    sink.write(chunk)
                else:
                    chunks.append(chunk)
        except (socket.timeout, TimeoutError):
            timed_out = True
        except (ConnectionError, http.client.RemoteDisconnected, http.client.IncompleteRead):
            disconnected = True
        finally:
            connection.close()
        record = {"http_status": status, "duration_ms": (time.monotonic_ns()-began)/1e6,
                  "started_unix_ns": started, "completed_unix_ns": time.time_ns(),
                  "response_sha256": hasher.hexdigest(), "bytes_received": total,
                  "client_timed_out": timed_out, "client_disconnected": disconnected, "request_id": request_id,
                  "method": method, "path_sha256": sha(path.split("?", 1)[0].encode())}
        self.receipts.append(record)
        return record, b"".join(chunks)

    def json(self, method, path, body=None, native=False, expected=(200,)):
        record, raw = self.request(method, path, body, native)
        need(record["http_status"] in expected and not record["client_timed_out"] and not record["client_disconnected"], "probe_http_contract")
        return record, decode(raw) if raw else None

    def resources(self):
        record, value = self.json("GET", "/admin/v1/runtime/resources", native=True)
        return {"http": record, "resources": value, "application": self.application}

    def readiness(self):
        record, _ = self.request("GET", "/readyz", cap=65536, timeout=5)
        need(record["http_status"] in (200, 503) and not record["client_timed_out"] and not record["client_disconnected"], "readiness_http_contract")
        return {"http": record, "ready_status": record["http_status"], "application": self.application}

    def search(self, root):
        _, value = self.json("GET", "/emby/Users/%s/Items?%s" % (self.c["credentials"]["user_id"], urlencode(root["search_params"])))
        need(value["TotalRecordCount"] == root["search_total"]
             and [row["Id"] for row in value["Items"]] == root["search_ids"], "healthy_search_exact_results")

    def scan(self, root):
        record, value = self.json("POST", "/admin/v1/libraries/%s/scan" % root["library_id"], {"ForceProbe": False}, True, (202,))
        need(type(value) is dict, "scan_admission")
        return record, value

    def media(self, root, ticks, playback_path=None):
        path = "/emby/Videos/%s/original.%s?%s" % (root["item_id"], root["container"], urlencode({"MediaSourceId": root["media_source_id"]}))
        if playback_path is not None:
            path = playback_path
        filename = self.artifacts / (self.action + "-" + root["root_id"] + ".media")
        output = os.open(filename, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        owned = os.fstat(output)
        try:
            with os.fdopen(output, "wb") as sink:
                record, _ = self.request("GET", path, cap=root["source_bytes"], timeout=60, sink=sink)
                sink.flush()
                os.fsync(sink.fileno())
            need(record["http_status"] == 200 and not record["client_timed_out"] and not record["client_disconnected"]
                 and record["bytes_received"] == root["source_bytes"] and record["response_sha256"] == root["source_sha256"], "source_bound_http_media")
            reference = Path(root["reference_file"])
            need(reference.resolve() == reference and reference.is_file(), "reference_file")
            with reference.open("rb") as source:
                need(hashlib.file_digest(source, "sha256").hexdigest() == root["source_sha256"], "reference_changed")
            observed, source_frame, source_children = self.frame(reference, ticks, root)
            response_ticks, response_frame, response_children = self.frame(filename, ticks, root)
            need(observed == response_ticks and source_frame == response_frame, "resume_frame_does_not_match_source")
            with filename.open("rb") as media:
                final = os.fstat(media.fileno())
                need((final.st_dev, final.st_ino) == (owned.st_dev, owned.st_ino)
                     and hashlib.file_digest(media, "sha256").hexdigest() == record["response_sha256"], "download_changed_during_decode")
            with reference.open("rb") as media:
                need(hashlib.file_digest(media, "sha256").hexdigest() == root["source_sha256"], "reference_changed_during_decode")
            evidence = {"source_sha256": root["source_sha256"], "http_media_sha256": record["response_sha256"],
                        "source_frame_sha256": sha(source_frame), "response_frame_sha256": sha(response_frame),
                        "requested_ticks": ticks, "observed_start_ticks": response_ticks, "decoded_frames": 1,
                        "frame_width": 64, "frame_height": 64, "children": source_children + response_children}
            return record, evidence
        finally:
            # The exact exclusive filename belongs only to this invocation.
            if filename.exists():
                final = filename.lstat()
                need((final.st_dev, final.st_ino) == (owned.st_dev, owned.st_ino) and stat.S_ISREG(final.st_mode), "download_cleanup_identity_changed")
                filename.unlink()

    def frame(self, path, ticks, root):
        origin = int(root["source_clock_origin_ticks"])
        target = Decimal(ticks + origin) / Decimal(10000000)
        interval = format(target, "f") + "%+#1024"
        raw, fact = owned_command(self.c["tools"]["ffprobe"], ["-v", "error", "-select_streams", "v:0",
            "-read_intervals", interval, "-show_frames", "-show_entries", "frame=best_effort_timestamp_time",
            "-of", "json", str(path)], 30, 2 << 20)
        value = decode(raw)
        frames = value.get("frames", [])
        need(0 < len(frames) <= 1024, "frame_timeline_budget")
        candidates = []
        for frame in frames:
            try:
                position = Decimal(frame["best_effort_timestamp_time"]) * Decimal(10000000) - origin
            except (KeyError, InvalidOperation):
                raise Failure("frame_timestamp") from None
            need(position.is_finite(), "frame_timestamp")
            if position >= ticks:
                candidates.append(position)
        need(candidates, "resume_frame_not_observed")
        observed = int(min(candidates))
        need(0 <= observed-ticks <= root["tolerance_ticks"] <= 30000000, "resume_frame_tolerance")
        # -copyts and select use the measured source timeline, not frame-number
        # arithmetic or the requested value substituted for an observation.
        selected = Decimal(observed + origin) / Decimal(10000000)
        pixels, decoded = owned_command(self.c["tools"]["ffmpeg"], ["-hide_banner", "-nostdin", "-v", "error",
            "-threads", "1", "-copyts", "-seek_timestamp", "1", "-ss", format(target, "f"), "-i", str(path), "-map", "0:v:0",
            "-vf", "select=gte(t\\,%s),scale=64:64" % format(selected, "f"), "-frames:v", "1", "-an", "-sn",
            "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1"], 30, 64*64*3)
        need(len(pixels) == 64*64*3, "resume_frame_decode")
        return observed, pixels, [fact, decoded]

    def healthy(self, payload):
        need(set(payload) == {"earliest_unix_ns", "roots"} and payload["roots"]
             and len(set(payload["roots"])) == len(payload["roots"]), "healthy_probe_payload")
        rows = []
        for root_id in payload["roots"]:
            root = self.roots[root_id]
            before = len(self.receipts)
            self.search(root)
            row = dict(self.receipts[before])
            rows.append({**row, "root_id": root_id, "operation": "search"})
            record, decoded = self.media(root, 0)
            rows.append({**record, "root_id": root_id, "operation": "playback", "media_decode": decoded})
            record, admitted = self.scan(root)
            rows.append({**record, "root_id": root_id, "operation": "scan", "scan_admission": admitted})
        need(all(row["started_unix_ns"] >= payload["earliest_unix_ns"] for row in rows), "stale_healthy_probe")
        return rows

    def affected(self, payload):
        need(set(payload) == {"root_id", "kind", "timeout_seconds"} and payload["kind"] in {"read", "metadata", "scan", "lock_metadata", "preview"}
             and type(payload["timeout_seconds"]) in (int, float) and 1 <= payload["timeout_seconds"] <= 30, "affected_probe_payload")
        root = self.roots[payload["root_id"]]
        if payload["kind"] == "read":
            path = "/emby/Videos/%s/original.%s?%s" % (root["item_id"], root["container"], urlencode({"MediaSourceId": root["media_source_id"]}))
            record, _ = self.request("GET", path, cap=root["source_bytes"], timeout=payload["timeout_seconds"])
            value = None
        elif payload["kind"] == "metadata":
            record, raw = self.request("GET", "/admin/v1/libraries/%s/roots/%s/binding" % (root["library_id"], root["root_id"]),
                                       native=True, timeout=payload["timeout_seconds"])
            value = decode(raw) if raw and record["http_status"] == 200 else None
        elif payload["kind"] == "scan":
            record, raw = self.request("POST", "/admin/v1/libraries/%s/scan" % root["library_id"],
                                       {"ForceProbe": True}, True, timeout=payload["timeout_seconds"])
            value = decode(raw) if raw and record["http_status"] == 202 else None
        elif payload["kind"] == "lock_metadata":
            route = "/admin/v1/items/%s/metadata" % root["lock_item_id"]
            _, current = self.json("GET", route, native=True)
            # Even an unchanged edit takes the real catalog-owner row lock.
            # It avoids an accidental durable mutation if injection was absent.
            body = {key: current[key] for key in ("Revision", "Overrides", "LockedFields")}
            record, raw = self.request("PUT", route, body, True, timeout=payload["timeout_seconds"])
            value = decode(raw) if raw and record["http_status"] == 200 else None
        else:
            body = {"Kind": "previews", "RequestId": "phase3-fault-" + sha(self.action.encode())[:32],
                    "LibraryIds": [], "ItemIds": [root["preview_item_id"]], "Force": True}
            record, raw = self.request("POST", "/admin/v1/media-analysis/runs", body, True, timeout=payload["timeout_seconds"])
            value = decode(raw) if raw and record["http_status"] == 202 else None
        return {"root_id": payload["root_id"], "kind": payload["kind"], "http": record, "admission_or_binding": value,
                "item_id": root["item_id"], "media_source_id": root["media_source_id"],
                "operation_item_id": root["lock_item_id"] if payload["kind"] == "lock_metadata" else root["preview_item_id"] if payload["kind"] == "preview" else root["item_id"],
                "expected_source_identity": root["source_identity"], "application": self.application, "http_evidence": list(self.receipts)}

    def resume(self, payload):
        need(set(payload) == {"persisted_ticks", "snapshot_sha256", "item_id_ref"}
             and type(payload["persisted_ticks"]) is int and payload["persisted_ticks"] > 0
             and SHA.fullmatch(payload["snapshot_sha256"]), "resume_payload")
        matches = [row for row in self.roots.values() if row["item_id_ref"] == payload["item_id_ref"]]
        need(len(matches) == 1, "resume_item_reference")
        root, ticks = matches[0], payload["persisted_ticks"]
        _, item = self.json("GET", "/emby/Users/%s/Items/%s" % (self.c["credentials"]["user_id"], root["item_id"]))
        need(item["UserData"]["PlaybackPositionTicks"] == ticks, "resume_http_durable_position")
        body = dict(root["playback_body"])
        body.pop("MediaSourceId", None)
        body.update(UserId=self.c["credentials"]["user_id"], StartTimeTicks=ticks, IsPlayback=True)
        _, info = self.json("POST", "/emby/Items/%s/PlaybackInfo" % root["item_id"], body)
        need(not info.get("ErrorCode") and info.get("PlaySessionId") and len(info["MediaSources"]) == 1, "resume_playback_admission")
        source = info["MediaSources"][0]
        need(source.get("SupportsDirectStream") is True and SAFE.fullmatch(source["Id"]), "resume_current_source")
        target = urlsplit(source["DirectStreamUrl"])
        need(not target.scheme and not target.netloc and not target.fragment and not target.username
             and target.path.lower() == ("/videos/%s/original.%s" % (root["item_id"], root["container"])).lower(), "resume_original_route")
        query = parse_qs(target.query, strict_parsing=True)
        need(query.get("MediaSourceId") == [source["Id"]] and query.get("PlaySessionId") == [info["PlaySessionId"]], "resume_url_scope")
        playback_path = "/emby" + target.path + "?" + target.query
        play = {"ItemId": root["item_id"], "MediaSourceId": source["Id"], "PlaySessionId": info["PlaySessionId"],
                "PositionTicks": ticks, "PlayMethod": "DirectStream", "IsPaused": False}
        self.json("POST", "/emby/Sessions/Playing", play, expected=(204,))
        try:
            record, decoded = self.media(root, ticks, playback_path)
        finally:
            stopped = {key: value for key, value in play.items() if key not in {"PlayMethod", "IsPaused"}}
            stopped["Failed"] = False
            self.json("POST", "/emby/Sessions/Playing/Stopped", stopped, expected=(200, 204))
        return {"reconnected": True, "persisted_ticks": ticks, "requested_ticks": ticks,
                "observed_start_ticks": decoded["observed_start_ticks"], "tolerance_ticks": root["tolerance_ticks"],
                "http_status": record["http_status"], "media_bytes": record["bytes_received"],
                "snapshot_sha256": payload["snapshot_sha256"], "item_id_ref": payload["item_id_ref"],
                "item_id": root["item_id"], "user_id": self.c["credentials"]["user_id"], "play_session_id": info["PlaySessionId"],
                "current_media_source_id": source["Id"], "media_decode": decoded, "http_evidence": list(self.receipts)}

    def retain_resume_receipt(self, result):
        raw = encode(result)
        need(len(raw) <= 4 << 20, "resume_receipt_budget")
        path = self.artifacts / ("resume-" + sha(self.action.encode())[:48] + ".probe.json")
        descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(raw)
            stream.flush()
            os.fsync(stream.fileno())
        directory = os.open(self.artifacts, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        return {"path": str(path), "bytes": len(raw), "sha256": sha(raw)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binding", "--context", dest="binding", required=True)
    arguments = parser.parse_args()
    result = {"schema_version": 1, "request_id": "invalid", "operation": "invalid",
              "owner_id": "invalid", "vmid": 106, "status": "failed", "data": {}}
    actor = None
    try:
        raw = sys.stdin.buffer.read((1 << 20)+1)
        need(len(raw) <= 1 << 20, "rpc_size")
        request = decode(raw)
        need(type(request) is dict and set(request) == {"schema_version", "request_id", "operation", "run_id", "owner_id", "profile_id", "source_revision", "tier", "scenario", "payload"}
             and request["schema_version"] == 1 and type(request["request_id"]) is str
             and SAFE.fullmatch(request["request_id"]) and type(request["operation"]) is str
             and request["operation"] in METHODS, "rpc_envelope")
        binding = private(arguments.binding, 2 << 20)
        for key in ("run_id", "owner_id", "profile_id", "source_revision", "tier"):
            need(request[key] == binding[key], "rpc_binding")
        result.update(request_id=request["request_id"], operation=request["operation"], owner_id=binding["owner_id"])
        actor = Probe(binding)
        actor.action = request["request_id"]
        if request["operation"] == "healthy_probes":
            data = actor.healthy(request["payload"])
        elif request["operation"] == "affected_probe":
            data = actor.affected(request["payload"])
        elif request["operation"] == "playback_resume":
            data = actor.resume(request["payload"])
        elif request["operation"] == "runtime_resources":
            need(request["payload"] == {}, "resources_payload")
            data = actor.resources()
        else:
            need(request["payload"] == {}, "readiness_payload")
            data = actor.readiness()
        actor.assert_application()
        result.update(status="ok", data=data)
        if request["operation"] == "playback_resume":
            data["private_artifact"] = actor.retain_resume_receipt(result)
    except (Failure, OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        result["status"] = "failed"
        result["data"] = {"error_code": str(error) if isinstance(error, Failure) else "probe_operation_failed"}
        if actor is not None:
            result["data"].update(http_evidence=list(actor.receipts), application=actor.application)
    output = encode(result)
    need(len(output) <= 4 << 20, "rpc_output_budget")
    sys.stdout.buffer.write(output)
    return 0 if result["status"] == "ok" else 1


if __name__ == "__main__":
    raise SystemExit(main())
