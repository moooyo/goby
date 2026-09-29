#!/usr/bin/env python3
"""Record actual FFmpeg processes and descriptors in one AMD OCI container.

Run as the container host's root, before starting the owned Goby installation.
The private JSON configuration requires container_id (the full Docker ID),
executable, executable_sha256, render_device, cache_path, and output. Optional
uid and poll_ms default to 10001 and 10. All paths are absolute; executable,
render_device, and cache_path are paths inside the selected container.

Each output JSONL record is an observation accepted by oci-amd-delivery.py.
Executable identity is proven by matching /proc/PID/exe to the configured file
through /proc/PID/root, then hashing the opened executable once per PID and
start time. Render descriptors must be character devices with the selected
device's actual major/minor numbers. Their raw targets remain in
render_fd_evidence; render_fds uses the proven container device path.

--snapshot prints collect_current(config): observations, live_job_pids and
open_output_pids (job ID to PID lists), cache_job_ids, and
container_process_count. Output descriptors are checked for every process in
the container, including Goby itself and descriptors for deleted files. The
controller must select job IDs from its owned database before returning lists
for an individual play. A snapshot contains current resources; JSONL retains
historical observations, including observations made before render FDs open.

This observer never sends signals to other processes or changes their files.
It exits on an incomplete inspection instead of reporting resource closure.
"""

import argparse
import errno
import fcntl
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import signal
import stat
import sys
import threading
import time


MAX_FILE_BYTES = 16 << 20
MAX_IDENTITIES = 4096
MAX_RECORDS = 8192
MAX_FDS = 4096
JOB_ID = re.compile(r"[0-9a-f]{32}")
GONE = {errno.ENOENT, errno.ESRCH}


class Failure(Exception):
    pass


def need(condition, reason):
    if not condition:
        raise Failure(reason)


def diagnostic_error_path(error):
    """Expose the failing proc resource without arbitrary host paths or text."""
    filename = getattr(error, "filename", None)
    if filename is None:
        return None
    candidate = os.fsdecode(filename)
    if len(candidate) <= 4096 and re.fullmatch(
            r"/proc/(?:self|[0-9]+)/(?:cgroup|stat|status|exe|cwd|fd(?:/[0-9]+)?|root(?:/[A-Za-z0-9_./-]+)?)",
            candidate) is not None:
        return candidate
    return "<non-proc-path>"


def read_bounded(path, maximum):
    with open(path, "rb") as stream:
        value = stream.read(maximum + 1)
    need(len(value) <= maximum, "proc_value_exceeds_bound")
    return value


def load_config(filename):
    path = Path(filename)
    need(path.is_absolute(), "config_must_be_absolute")
    metadata = path.lstat()
    need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
         and 0 < metadata.st_size <= 16384, "config_must_be_bounded_and_private")
    value = json.loads(read_bounded(path, 16384))
    required = {"container_id", "executable", "executable_sha256", "render_device", "cache_path", "output"}
    optional = {"uid", "poll_ms"}
    need(type(value) is dict and required <= value.keys() <= required | optional, "invalid_config_fields")
    need(all(type(value[key]) is str and value[key] for key in required), "invalid_config_strings")
    need(re.fullmatch(r"[0-9a-f]{64}", value["container_id"]) is not None, "full_container_id_required")
    need(re.fullmatch(r"[0-9a-f]{64}", value["executable_sha256"]) is not None, "invalid_executable_hash")
    for key in ("executable", "render_device", "cache_path", "output"):
        candidate = PurePosixPath(value[key])
        need(candidate.is_absolute() and str(candidate) == value[key] and ".." not in candidate.parts
             and value[key] != "/", key + "_must_be_a_normalized_absolute_path")
    need(re.fullmatch(r"/dev/dri/renderD[0-9]+", value["render_device"]) is not None,
         "invalid_render_device")
    value.setdefault("uid", 10001)
    value.setdefault("poll_ms", 10)
    need(type(value["uid"]) is int and 0 < value["uid"] < 1 << 32, "invalid_expected_uid")
    need(type(value["poll_ms"]) is int and 1 <= value["poll_ms"] <= 1000, "invalid_poll_interval")
    return value


def in_container(process, container_id):
    for line in read_bounded(process / "cgroup", 16384).decode("ascii").splitlines():
        fields = line.split(":", 2)
        if len(fields) == 3:
            parts = fields[2].split("/")
            if container_id in parts or "docker-" + container_id + ".scope" in parts:
                return True
    return False


def start_time(process):
    value = read_bounded(process / "stat", 16384)
    fields = value.rsplit(b") ", 1)[-1].split()
    need(len(fields) >= 20, "invalid_proc_stat")
    return int(fields[19])


def effective_uid(process):
    for line in read_bounded(process / "status", 65536).splitlines():
        if line.startswith(b"Uid:"):
            fields = line.split()
            need(len(fields) == 5, "invalid_proc_uid")
            return int(fields[2])
    raise Failure("proc_uid_missing")


def without_deleted(target):
    return target[:-10] if target.endswith(" (deleted)") else target


def container_path(target, root_target):
    target, root_target = without_deleted(target), without_deleted(root_target).rstrip("/")
    if root_target and target.startswith(root_target + "/"):
        return target[len(root_target):]
    return target


def cache_mount_target(process, config):
    """Resolve the actual cache mount, including a bind mount outside overlayfs."""
    try:
        descriptor = os.open(process / "root" / config["cache_path"].lstrip("/"), os.O_PATH | os.O_DIRECTORY)
    except OSError as error:
        if error.errno in GONE:
            return None
        raise
    try:
        return without_deleted(os.readlink("/proc/self/fd/" + str(descriptor)))
    finally:
        os.close(descriptor)


def cache_container_path(target, root_target, cache_target, config):
    target = without_deleted(target)
    if cache_target is not None and (target == cache_target or target.startswith(cache_target + "/")):
        return config["cache_path"] + target[len(cache_target):]
    return container_path(target, root_target)


def job_for_path(path, cache_path):
    prefix = cache_path + "/"
    if not path.startswith(prefix):
        return None
    candidate = path[len(prefix):].split("/", 1)[0]
    return candidate if JOB_ID.fullmatch(candidate) is not None else None


def same_file(left, right):
    return (left.st_dev, left.st_ino) == (right.st_dev, right.st_ino)


def executable_digest(process, config, identity, hashes):
    """Return the digest only for the exact configured container executable."""
    executable = process / "exe"
    expected = process / "root" / config["executable"].lstrip("/")
    try:
        with open(executable, "rb") as stream:
            metadata = os.fstat(stream.fileno())
            if not stat.S_ISREG(metadata.st_mode) or not same_file(metadata, expected.stat()):
                return None
            inode = (metadata.st_dev, metadata.st_ino)
            cached = hashes.get(identity)
            if cached is not None:
                need(cached[:2] == inode, "executable_changed_within_process_identity")
                return cached[2]
            need(len(hashes) < MAX_IDENTITIES, "executable_identity_limit_reached")
            digest = hashlib.sha256()
            size = 0
            while chunk := stream.read(1 << 20):
                size += len(chunk)
                need(size <= 512 << 20, "executable_exceeds_hash_bound")
                digest.update(chunk)
            value = digest.hexdigest()
            need(value == config["executable_sha256"], "actual_executable_hash_mismatch")
            hashes[identity] = (*inode, value)
            return value
    except OSError as error:
        if error.errno in GONE:
            return None
        raise


def process_fds(process, root_target, cache_target, config, observe_render):
    output_jobs, render = set(), []
    selected = None
    if observe_render:
        selected = (process / "root" / config["render_device"].lstrip("/")).stat()
        need(stat.S_ISCHR(selected.st_mode), "selected_render_path_is_not_a_character_device")
    with os.scandir(process / "fd") as descriptors:
        for count, entry in enumerate(descriptors, 1):
            need(count <= MAX_FDS, "container_process_descriptor_limit_reached")
            if not entry.name.isdecimal():
                continue
            try:
                descriptor = Path(entry.path)
                target = os.readlink(descriptor)
                metadata = descriptor.stat()
            except OSError as error:
                if error.errno in GONE:
                    continue
                raise
            job_id = job_for_path(cache_container_path(target, root_target, cache_target, config), config["cache_path"])
            if job_id is not None:
                output_jobs.add(job_id)
            if selected is not None and stat.S_ISCHR(metadata.st_mode) and metadata.st_rdev == selected.st_rdev:
                render.append({"fd": int(entry.name), "target": target,
                               "major": os.major(metadata.st_rdev), "minor": os.minor(metadata.st_rdev)})
    return output_jobs, sorted(render, key=lambda item: item["fd"])


def collect_current(config, hashes=None):
    """Retry complete inspections briefly when a proc permission check races."""
    hashes = {} if hashes is None else hashes
    deadline, retries, last_path = None, 0, None
    while True:
        try:
            result = collect_current_once(config, hashes)
        except PermissionError as error:
            now = time.monotonic()
            if deadline is None:
                deadline = now + 0.5
            if now + 0.01 > deadline:
                raise
            retries += 1
            last_path = diagnostic_error_path(error)
            time.sleep(0.01)
            continue
        if retries:
            print(json.dumps({"event": "observer_permission_retry_recovered", "count": retries,
                              "last_path": last_path}), file=sys.stderr, flush=True)
        return result


def collect_current_once(config, hashes):
    """Return only a complete inspection; partial resource lists never escape."""
    observations, live, output, cache_jobs = [], {}, {}, set()
    process_count, cache_checked = 0, False
    with os.scandir("/proc") as processes:
        for entry in processes:
            if not entry.name.isdecimal():
                continue
            process, pid = Path(entry.path), int(entry.name)
            try:
                if not in_container(process, config["container_id"]):
                    continue
                ticks = start_time(process)
                identity = (pid, ticks)
                root_target = os.readlink(process / "root")
                cache_target = cache_mount_target(process, config)
                cwd = cache_container_path(os.readlink(process / "cwd"), root_target, cache_target, config)
                job_id = job_for_path(cwd, config["cache_path"])
                digest = None
                if job_id is not None and cwd == config["cache_path"] + "/" + job_id:
                    digest = executable_digest(process, config, identity, hashes)
                output_jobs, render = process_fds(process, root_target, cache_target, config, digest is not None)
                observation = None
                if digest is not None:
                    uid = effective_uid(process)
                    need(uid == config["uid"], "actual_ffmpeg_uid_mismatch")
                    arguments = read_bounded(process / "cmdline", 128 << 10).split(b"\0")
                    if arguments and arguments[-1] == b"":
                        arguments.pop()
                    need(0 < len(arguments) <= 512 and all(len(value) <= 16384 for value in arguments),
                         "ffmpeg_arguments_exceed_bound")
                    observation = {"job_id": job_id, "pid": pid, "start_time_ticks": ticks,
                        "uid": uid, "exe": config["executable"], "exe_sha256": digest,
                        "args": [value.decode("utf-8", errors="surrogateescape") for value in arguments],
                        "cwd": cwd, "render_fds": [config["render_device"]] if render else [],
                        "render_fd_evidence": render}
                current_cache = set()
                if not cache_checked:
                    try:
                        with os.scandir(process / "root" / config["cache_path"].lstrip("/")) as children:
                            for child in children:
                                if JOB_ID.fullmatch(child.name) is not None:
                                    current_cache.add(child.name)
                                    need(len(current_cache) <= MAX_IDENTITIES, "cache_job_limit_reached")
                    except OSError as error:
                        if error.errno not in GONE:
                            raise
                if start_time(process) != ticks or not in_container(process, config["container_id"]):
                    continue
                process_count += 1
                need(process_count <= MAX_IDENTITIES, "container_process_limit_reached")
                cache_jobs.update(current_cache)
                cache_checked = True
                if job_id is not None:
                    live.setdefault(job_id, set()).add(pid)
                for output_job in output_jobs:
                    output.setdefault(output_job, set()).add(pid)
                if observation is not None:
                    observations.append(observation)
            except OSError as error:
                if error.errno in GONE:
                    continue
                raise
    return {"observations": sorted(observations, key=lambda item: (item["pid"], item["start_time_ticks"])),
            "live_job_pids": {key: sorted(value) for key, value in sorted(live.items())},
            "open_output_pids": {key: sorted(value) for key, value in sorted(output.items())},
            "cache_job_ids": sorted(cache_jobs), "container_process_count": process_count}


def observe(config):
    stopped = threading.Event()
    signal.signal(signal.SIGTERM, lambda _number, _frame: stopped.set())
    signal.signal(signal.SIGINT, lambda _number, _frame: stopped.set())
    descriptor = os.open(config["output"], os.O_WRONLY | os.O_CREAT | os.O_APPEND | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "a", encoding="utf-8", buffering=1) as stream:
        metadata = os.fstat(stream.fileno())
        need(stat.S_ISREG(metadata.st_mode) and metadata.st_mode & 0o077 == 0
             and metadata.st_size <= MAX_FILE_BYTES, "output_must_be_bounded_and_private")
        fcntl.flock(stream.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        hashes, previous = {}, {}
        size, records = metadata.st_size, 0
        try:
            while not stopped.is_set():
                for observation in collect_current(config, hashes)["observations"]:
                    identity = (observation["pid"], observation["start_time_ticks"])
                    signature = json.dumps(observation, sort_keys=True, separators=(",", ":"))
                    if previous.get(identity) == signature:
                        continue
                    need(identity in previous or len(previous) < MAX_IDENTITIES, "observation_identity_limit_reached")
                    record = dict(observation, observed_at_ns=time.time_ns())
                    line = json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n"
                    size += len(line.encode("utf-8"))
                    records += 1
                    need(size <= MAX_FILE_BYTES and records <= MAX_RECORDS, "observation_output_limit_reached")
                    stream.write(line)
                    previous[identity] = signature
                stopped.wait(config["poll_ms"] / 1000)
        finally:
            stream.flush()
            os.fsync(stream.fileno())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--snapshot", action="store_true")
    arguments = parser.parse_args()
    config = load_config(arguments.config)
    need(os.geteuid() == 0, "container_host_root_required")
    if arguments.snapshot:
        print(json.dumps(collect_current(config), sort_keys=True))
    else:
        observe(config)


if __name__ == "__main__":
    try:
        main()
    except Failure as error:
        print(json.dumps({"error": str(error)}), file=sys.stderr)
        sys.exit(1)
    except (OSError, ValueError, UnicodeError) as error:
        print(json.dumps({"error": "observer_inspection_failed", "type": type(error).__name__,
                          "errno": getattr(error, "errno", None),
                          "path": diagnostic_error_path(error)}), file=sys.stderr)
        sys.exit(1)
