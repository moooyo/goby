#!/usr/bin/env python3
"""Finite fault primitives for the exclusively owned Phase 3 Linux guest.

The infrastructure owner provisions this source and freezes a private binding.
No fault targets are discovered by name alone. Every operation rechecks the
guest, service, loop backing file and device-mapper identity. This program does
not install packages, accept arbitrary commands/SQL, or perform hypervisor work.
"""

from __future__ import annotations

import argparse
import base64
import errno
import hashlib
import json
import os
import platform
from pathlib import Path
import re
import selectors
import signal
import stat
import subprocess
import sys
import time


MAX_JSON = 8 << 20
MAX_OUTPUT = 2 << 20
MAX_VOLUME = 2 << 30
MAX_COMMAND_SECONDS = 30
SHA = re.compile(r"[0-9a-f]{64}\Z")
IDENTIFIER = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}\Z")
UNIT = re.compile(r"goby-phase3-[a-z0-9-]{1,80}\.service\Z")
MUTATIONS = {"prepare_volumes", "arm_late_mount", "inject", "recover", "close", "observe_failure_errno"}


class FaultError(RuntimeError):
    """A non-sensitive, stable failure code."""


def need(value, code):
    if not value:
        raise FaultError(code)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True, allow_nan=False).encode()


def pairs(items):
    result = {}
    for key, value in items:
        need(key not in result, "duplicate_json_key")
        result[key] = value
    return result


def decode(raw):
    need(len(raw) <= MAX_JSON, "json_budget")
    try:
        return json.loads(raw, object_pairs_hook=pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(FaultError("nonfinite_json")))
    except (ValueError, UnicodeError, RecursionError) as error:
        raise FaultError("invalid_json") from error


def fields(value, required, optional=()):
    need(isinstance(value, dict) and set(required) <= value.keys() <= set(required) | set(optional), "invalid_fields")
    return value


def safe_id(value):
    need(isinstance(value, str) and IDENTIFIER.fullmatch(value), "invalid_id")
    return value


def integer(value, minimum=0, maximum=(1 << 63) - 1):
    need(type(value) is int and minimum <= value <= maximum, "invalid_integer")
    return value


def absolute(value):
    need(isinstance(value, str) and len(value) <= 4096 and "\x00" not in value, "invalid_path")
    path = Path(value)
    need(path.is_absolute() and str(path) == value and ".." not in path.parts and path != Path("/"), "unsafe_path")
    return path


def regular(path, private=False, maximum=MAX_JSON):
    path = absolute(str(path))
    need(path.resolve(strict=True) == path, "symlinked_file")
    info = path.lstat()
    need(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1 and info.st_size <= maximum,
         "unsafe_regular_file")
    if private:
        need(stat.S_IMODE(info.st_mode) & 0o077 == 0, "private_permissions")
    return info


def read_pinned(reference, private=False):
    fields(reference, ("path", "sha256"))
    need(SHA.fullmatch(reference["sha256"]), "invalid_sha256")
    path = absolute(reference["path"])
    before = regular(path, private)
    raw = path.read_bytes()
    after = regular(path, private)
    identity = lambda value: (value.st_dev, value.st_ino, value.st_size, value.st_mtime_ns, value.st_ctime_ns)
    need(identity(before) == identity(after) and hashlib.sha256(raw).hexdigest() == reference["sha256"], "pinned_file_changed")
    return raw


def save_new(path, value):
    raw = canonical(value) + b"\n"
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        view = memoryview(raw)
        while view:
            size = os.write(fd, view)
            need(size > 0, "short_write")
            view = view[size:]
        os.fsync(fd)
    finally:
        os.close(fd)
    fd = os.open(path.parent, os.O_DIRECTORY | os.O_RDONLY | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    return {"path": str(path), "sha256": hashlib.sha256(raw).hexdigest()}


def command(argv, timeout=MAX_COMMAND_SECONDS, env=None, input_bytes=None, user=None, group=None,
            extra_groups=None, process_receipt=False):
    """Use only fixed argument arrays; a timeout leaves disposition unknown."""
    child = subprocess.Popen(argv, stdin=subprocess.PIPE if input_bytes is not None else subprocess.DEVNULL,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True,
                             env=env or {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C"},
                             user=user, group=group, extra_groups=extra_groups)
    output, errors = bytearray(), bytearray()
    deadline = time.monotonic() + timeout
    failure = None
    owner = None
    try:
        if process_receipt:
            raw = (Path("/proc") / str(child.pid) / "stat").read_text()
            owner = {"pid": child.pid, "start_ticks": int(raw[raw.rfind(")") + 2:].split()[19])}
        pending = memoryview(input_bytes or b"")
        with selectors.DefaultSelector() as selector:
            for pipe, event, role in ((child.stdout, selectors.EVENT_READ, "stdout"),
                                      (child.stderr, selectors.EVENT_READ, "stderr")):
                os.set_blocking(pipe.fileno(), False)
                selector.register(pipe, event, role)
            if child.stdin:
                if pending:
                    os.set_blocking(child.stdin.fileno(), False)
                    selector.register(child.stdin, selectors.EVENT_WRITE, "stdin")
                else:
                    child.stdin.close()
            while selector.get_map():
                remaining = deadline - time.monotonic()
                need(remaining > 0, "command_disposition_unknown_no_retry")
                for key, _event in selector.select(min(remaining, 0.25)):
                    if key.data == "stdin":
                        count = os.write(key.fileobj.fileno(), pending)
                        pending = pending[count:]
                        if not pending:
                            selector.unregister(key.fileobj)
                            key.fileobj.close()
                        continue
                    block = os.read(key.fileobj.fileno(), 65536)
                    if not block:
                        selector.unregister(key.fileobj)
                        key.fileobj.close()
                    else:
                        (output if key.data == "stdout" else errors).extend(block)
                        need(len(output) + len(errors) <= MAX_OUTPUT, "command_output_budget")
        remaining = deadline - time.monotonic()
        need(remaining > 0, "command_disposition_unknown_no_retry")
        child.wait(timeout=remaining)
        need(child.returncode == 0, "owned_command_failed")
    except BaseException as error:
        failure = error
    finally:
        if failure or child.poll() is None:
            # The process-group ID remains valid after its leader exits.
            # Descendants holding a pipe must not survive a timed-out caller.
            try:
                os.killpg(child.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                failure = FaultError("command_process_closure_unknown")
        for pipe in (child.stdin, child.stdout, child.stderr):
            if pipe and not pipe.closed:
                pipe.close()
    if failure:
        raise failure
    value = bytes(output).decode("utf-8", "strict").strip()
    if process_receipt:
        return {"stdout": value, "owned_child": {**owner, "exit_code": child.returncode, "reaped": True}}
    return value


def proc_identity(pid):
    integer(pid, 2, 1 << 30)
    root = Path("/proc") / str(pid)
    raw = (root / "stat").read_text()
    tail = raw[raw.rfind(")") + 2:].split()
    need(len(tail) >= 20, "proc_stat_short")
    cgroups = (root / "cgroup").read_text().splitlines()
    cgroup = next((line[3:] for line in cgroups if line.startswith("0::")), None)
    need(cgroup is not None, "cgroup_v2_required")
    executable = root / "exe"
    fd = os.open(executable, os.O_RDONLY | os.O_CLOEXEC)
    try:
        sha = hashlib.sha256()
        while True:
            part = os.read(fd, 1 << 20)
            if not part:
                break
            sha.update(part)
    finally:
        os.close(fd)
    return {"pid": pid, "start_ticks": int(tail[19]), "cgroup": cgroup, "executable_sha256": sha.hexdigest()}


def mountinfo():
    result = []
    for line in Path("/proc/self/mountinfo").read_text().splitlines():
        left, right = line.split(" - ", 1)
        before, after = left.split(), right.split()
        unescape = lambda value: re.sub(r"\\([0-7]{3})", lambda match: chr(int(match[1], 8)), value)
        result.append({"mount_id": int(before[0]), "parent_id": int(before[1]), "major_minor": before[2],
                       "root": unescape(before[3]), "mountpoint": unescape(before[4]), "options": before[5],
                       "filesystem": after[0], "source": unescape(after[1]), "super_options": after[2]})
    return result


class Guest:
    def __init__(self, binding):
        self.binding = binding
        fields(binding, ("schema_version", "run_id", "owner_id", "guest", "owner_marker", "owned_root",
                          "services", "volumes", "tools", "postgres", "allow_guest_cache_drop", "fixture_access"),
               ("spool_directory",))
        need(binding["schema_version"] == 1, "unsupported_schema")
        safe_id(binding["run_id"])
        safe_id(binding["owner_id"])
        self.root = absolute(binding["owned_root"])
        need(str(self.root).startswith("/var/lib/goby-phase3/") or str(self.root).startswith("/opt/goby-phase3/"), "unsafe_owned_root")
        self.operations = self.root / "recovery-operations"
        self.guard()

    def guard(self):
        need(sys.platform == "linux" and os.geteuid() == 0, "owned_linux_guest_root_required")
        expected = fields(self.binding["guest"], ("vmid", "name", "machine_id", "smbios_uuid", "disks"))
        need(expected["vmid"] == 106 and expected["name"].startswith("goby-phase3-"), "guest_scope_mismatch")
        need(Path("/etc/machine-id").read_text().strip() == expected["machine_id"], "machine_identity_mismatch")
        need(Path("/sys/class/dmi/id/product_uuid").read_text().strip().lower() == expected["smbios_uuid"], "smbios_identity_mismatch")
        marker = decode(read_pinned(self.binding["owner_marker"], private=True))
        need(marker.get("owner_id") == self.binding["owner_id"] and marker.get("vmid") == 106 and
             marker.get("smbios_uuid") == expected["smbios_uuid"] and marker.get("machine_id") == expected["machine_id"],
             "owner_marker_mismatch")
        need(self.root.resolve(strict=True) == self.root and self.root.lstat().st_uid == 0 and
             stat.S_IMODE(self.root.lstat().st_mode) & 0o022 == 0, "owned_root_guard")
        need(self.operations.resolve(strict=True) == self.operations and self.operations.lstat().st_uid == 0 and
             stat.S_IMODE(self.operations.lstat().st_mode) & 0o077 == 0, "operation_directory_guard")
        for name, service in self.binding["services"].items():
            need(name in {"goby", "postgres"}, "unknown_service")
            fields(service, ("unit", "unit_file", "uid", "executable_sha256"))
            need(UNIT.fullmatch(service["unit"]), "unowned_unit")
            need(Path(service["unit_file"]["path"]).name == service["unit"], "unit_file_name_mismatch")
            read_pinned(service["unit_file"])
            integer(service["uid"], 1, 65534)
            need(SHA.fullmatch(service["executable_sha256"]), "invalid_service_digest")
        need(set(self.binding["services"]) == {"goby", "postgres"}, "service_inventory_missing")
        need(isinstance(self.binding["allow_guest_cache_drop"], bool), "invalid_cache_drop_policy")
        access = fields(self.binding["fixture_access"], ("actor_uid", "media_read_gid"))
        integer(access["actor_uid"], 1, 65534)
        integer(access["media_read_gid"], 1, 65534)
        need(access["actor_uid"] != self.binding["services"]["goby"]["uid"], "fixture_actor_is_application")
        self.validate_volumes()

    def validate_volumes(self):
        volumes = self.binding["volumes"]
        need(isinstance(volumes, dict) and 2 <= len(volumes) <= 8, "volume_count")
        mounts, backings, loops, names, numbers = [], [], set(), set(), set()
        for volume_id, volume in volumes.items():
            safe_id(volume_id)
            fields(volume, ("mountpoint", "backing_file", "loop_device", "mapper_name", "dm_uuid", "filesystem_uuid",
                            "major_minor", "size_bytes", "writable", "purpose"))
            mount, backing = absolute(volume["mountpoint"]), absolute(volume["backing_file"])
            need(self.root in mount.parents and self.root in backing.parents and
                 self.operations not in (mount, backing) and self.operations not in mount.parents and
                 self.operations not in backing.parents, "volume_path_scope")
            need(re.fullmatch(r"/dev/loop[0-9]{1,3}", volume["loop_device"]) and
                 re.fullmatch(r"goby-phase3-[a-z0-9-]{1,64}", volume["mapper_name"]) and
                 re.fullmatch(r"[0-9]{1,5}:[0-9]{1,7}", volume["major_minor"]), "invalid_volume_device")
            need(re.fullmatch(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", volume["filesystem_uuid"]) and
                 re.fullmatch(r"[A-Za-z0-9_.:-]{1,240}", volume["dm_uuid"]) and
                 volume["dm_uuid"].startswith("GOBY-PHASE3-" + self.binding["owner_id"] + "-"), "volume_uuid_scope")
            integer(volume["size_bytes"], 16 << 20, MAX_VOLUME)
            need(volume["size_bytes"] % 512 == 0 and type(volume["writable"]) is bool and
                 volume["purpose"] in {"media", "derivatives", "replacement"}, "volume_policy")
            need(volume["loop_device"] not in loops and volume["mapper_name"] not in names and volume["major_minor"] not in numbers,
                 "aliased_volume_devices")
            loops.add(volume["loop_device"])
            names.add(volume["mapper_name"])
            numbers.add(volume["major_minor"])
            mounts.append(mount)
            backings.append(backing)
        need(len(set(mounts)) == len(mounts) and len(set(backings)) == len(backings), "aliased_volume_paths")
        for mount in mounts:
            need(not any(mount != other and (mount in other.parents or other in mount.parents) for other in mounts),
                 "nested_declared_fault_volumes")
            need(not any(backing == mount or mount in backing.parents for backing in backings), "backing_on_fault_volume")
            datadir = absolute(self.binding["postgres"]["data_directory"])
            need(datadir != mount and mount not in datadir.parents and datadir not in mount.parents, "postgres_on_fault_volume")

    def tool(self, name):
        reference = self.binding["tools"][name]
        read_pinned(reference)
        return reference["path"]

    def owned_path(self, value, exists=True):
        path = absolute(value)
        need(self.root in path.parents and path != self.operations and self.operations not in path.parents,
             "path_outside_fault_scope")
        resolved = path.resolve(strict=exists)
        need(resolved == path, "symlinked_owned_path")
        return path

    def service(self, name, running=True):
        service = self.binding["services"][name]
        properties = command([self.tool("systemctl"), "show", service["unit"],
                              "--property=MainPID,ControlGroup,User,ActiveState,FragmentPath,DropInPaths,LoadState"])
        parsed = dict(line.split("=", 1) for line in properties.splitlines())
        pid = int(parsed["MainPID"])
        need(parsed["LoadState"] == "loaded" and parsed["FragmentPath"] == service["unit_file"]["path"] and
             parsed["DropInPaths"] == "", "unit_configuration_changed")
        read_pinned(service["unit_file"])
        if not running and pid == 0:
            return {"pid": 0, "active_state": parsed["ActiveState"], "cgroup": parsed["ControlGroup"]}
        need(parsed["ControlGroup"] == "/system.slice/" + service["unit"], "unit_cgroup_changed")
        need(pid > 1 and parsed["ActiveState"] == "active", "owned_service_not_running")
        identity = proc_identity(pid)
        need(identity["cgroup"] == parsed["ControlGroup"] and identity["executable_sha256"] == service["executable_sha256"],
             "service_process_identity_changed")
        status_text = (Path("/proc") / str(pid) / "status").read_text()
        uid = int(next(line for line in status_text.splitlines() if line.startswith("Uid:")).split()[1])
        need(uid == service["uid"], "service_uid_changed")
        if name == "goby":
            values = dict(line.split(":", 1) for line in status_text.splitlines() if ":" in line)
            need(int(values["CapEff"].strip(), 16) == 0 and values["NoNewPrivs"].strip() == "1", "goby_privilege_contract")
        return identity

    def volume(self, volume_id, mounted=True):
        safe_id(volume_id)
        volume = self.binding["volumes"][volume_id]
        fields(volume, ("mountpoint", "backing_file", "loop_device", "mapper_name", "dm_uuid", "filesystem_uuid",
                        "major_minor", "size_bytes", "writable", "purpose"))
        integer(volume["size_bytes"], 16 << 20, MAX_VOLUME)
        need(re.fullmatch(r"/dev/loop[0-9]{1,3}", volume["loop_device"]), "invalid_loop")
        need(re.fullmatch(r"goby-phase3-[a-z0-9-]{1,64}", volume["mapper_name"]), "unowned_dm_name")
        need(volume["dm_uuid"].startswith("GOBY-PHASE3-" + self.binding["owner_id"] + "-"), "unowned_dm_uuid")
        backing = self.owned_path(volume["backing_file"])
        info = regular(backing, private=True, maximum=MAX_VOLUME)
        need(info.st_size == volume["size_bytes"], "backing_size_changed")
        loop_name = Path(volume["loop_device"]).name
        loop_backing = Path("/sys/class/block") / loop_name / "loop/backing_file"
        observed_backing = loop_backing.read_text().strip()
        need(observed_backing in (str(backing), str(backing).lstrip("/")), "loop_backing_changed")
        mapper = Path("/dev/mapper") / volume["mapper_name"]
        device = mapper.resolve(strict=True)
        need(re.fullmatch(r"/dev/dm-[0-9]+", str(device)), "mapper_not_dm")
        sysdir = Path("/sys/class/block") / device.name
        need((sysdir / "dm/uuid").read_text().strip() == volume["dm_uuid"] and
             (sysdir / "dm/name").read_text().strip() == volume["mapper_name"], "dm_identity_changed")
        need([part.name for part in (sysdir / "slaves").iterdir()] == [loop_name], "dm_slaves_changed")
        need((sysdir / "dev").read_text().strip() == volume["major_minor"], "dm_device_changed")
        need(int((sysdir / "size").read_text().strip()) * 512 == volume["size_bytes"], "dm_size_changed")
        mounts = [item for item in mountinfo() if item["mountpoint"] == volume["mountpoint"]]
        if mounted:
            need(len(mounts) == 1 and mounts[0]["major_minor"] == volume["major_minor"], "owned_mount_changed")
        return volume

    def release(self, request):
        need(self.release_ref is not None, "explicit_run_release_required")
        release = decode(read_pinned(self.release_ref, private=True))
        fields(release, ("schema_version", "run_id", "owner_id", "vmid", "smbios_uuid", "source_revision",
                         "binding_sha256", "operations", "scenarios", "expires_unix_ns"))
        need(release["schema_version"] == 1 and release["run_id"] == self.binding["run_id"] and
             release["owner_id"] == self.binding["owner_id"] and release["vmid"] == 106 and
             release["smbios_uuid"] == self.binding["guest"]["smbios_uuid"] and
             release["source_revision"] == request["source_revision"] and
             release["binding_sha256"] == request["binding_sha256"] == self.binding_sha256, "release_scope_mismatch")
        need(isinstance(release["scenarios"], dict) and request["op"] in release["operations"] and
             release["scenarios"].get(request["scenario"]["scenario_id"]) == hashlib.sha256(canonical(request["scenario"])).hexdigest() and
             time.time_ns() < integer(release["expires_unix_ns"], 1), "operation_not_released")

    def scenario(self, value):
        fields(value, ("scenario_id", "fault", "volume_id", "replacement_volume_id", "relative_path", "late_mount",
                       "restart_goby_after", "require_interrupted_jobs"))
        safe_id(value["scenario_id"])
        need(value["fault"] in {"blocked_read", "blocked_metadata", "mount_loss", "changed_root_mount", "changed_nested_mount",
                                "permission_failure", "enospc", "postgres_disconnect", "postgres_lock_wait", "process_crash",
                                "postgres_restart", "guest_reboot", "guest_reset"}, "unsupported_fault")
        for name in ("late_mount", "restart_goby_after", "require_interrupted_jobs"):
            need(type(value[name]) is bool, "invalid_scenario_flag")
        relative = value["relative_path"]
        need(isinstance(relative, str) and len(relative) <= 512 and "\x00" not in relative and
             not relative.startswith("/") and ".." not in Path(relative).parts and
             (relative in ("", ".") or str(Path(relative)) == relative), "unsafe_relative_path")
        storage = value["fault"] in {"blocked_read", "blocked_metadata", "mount_loss", "changed_root_mount", "changed_nested_mount",
                                    "permission_failure", "enospc"} or value["late_mount"]
        need((value["volume_id"] in self.binding["volumes"]) if storage else value["volume_id"] is None,
             "scenario_volume_scope")
        if value["fault"] in {"changed_root_mount", "changed_nested_mount"}:
            need(value["replacement_volume_id"] in self.binding["volumes"] and
                 value["replacement_volume_id"] != value["volume_id"], "replacement_volume_scope")
        else:
            need(value["replacement_volume_id"] is None, "unexpected_replacement_volume")
        need(not value["late_mount"] or value["fault"] in {"guest_reboot", "guest_reset"}, "invalid_late_mount_fault")
        if value["fault"] == "changed_nested_mount":
            need(relative not in ("", "."), "nested_mount_requires_descendant")
        return value

    def volume_target(self, volume, relative):
        mount = self.owned_path(volume["mountpoint"])
        target = self.owned_path(str(mount / relative))
        need(target == mount or mount in target.parents, "target_outside_selected_volume")
        need(target.stat().st_dev == mount.stat().st_dev, "target_crosses_nested_mount")
        return target

    def fixture_parent_chain(self, mountpoint):
        parent = absolute(mountpoint).parent
        need(parent == self.root or self.root in parent.parents, "fixture_parent_scope")
        relative = parent.relative_to(self.root)
        need(len(relative.parts) <= 16, "fixture_parent_depth")
        current, result = self.root, []
        for part in (None, *relative.parts):
            if part is not None:
                current = current / part
            need(current.resolve(strict=True) == current, "fixture_parent_symlink")
            info = current.lstat()
            need(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
                 info.st_gid == self.binding["fixture_access"]["media_read_gid"] and
                 stat.S_IMODE(info.st_mode) & 0o010 and not stat.S_IMODE(info.st_mode) & 0o022,
                 "fixture_parent_not_group_traversable")
            result.append({"path": str(current), "device": info.st_dev, "inode": info.st_ino,
                           "uid": info.st_uid, "gid": info.st_gid, "mode": stat.S_IMODE(info.st_mode)})
        return result

    def postgres(self, query, timeout=MAX_COMMAND_SECONDS):
        context = fields(self.binding["postgres"], ("service_file", "service_name", "database", "schema", "data_directory", "lock_item_id", "json_log"))
        need(re.fullmatch(r"[A-Za-z0-9_]{1,63}", context["service_name"]) and
             re.fullmatch(r"goby_phase3_[a-z0-9_]{1,63}", context["database"]) and context["schema"] == "public",
             "unowned_postgres_scope")
        datadir = self.owned_path(context["data_directory"])
        read_pinned(context["service_file"], private=True)
        env = {"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LANG": "C", "PGSERVICEFILE": context["service_file"]["path"],
               "PGSERVICE": context["service_name"], "PGCONNECT_TIMEOUT": "5", "PGAPPNAME": "goby-phase3-fault-controller"}
        argv = [self.tool("psql"), "-X", "--no-password", "-A", "-t", "-v", "ON_ERROR_STOP=1"]
        guard = "SELECT current_database() || '|' || current_setting('data_directory');"
        answer = command(argv, env=env, input_bytes=guard.encode())
        need(answer == context["database"] + "|" + str(datadir), "postgres_cluster_identity_changed")
        self.service("postgres")
        return command(argv, timeout=timeout, env=env, input_bytes=query.encode())

    def owner_backend(self):
        context = self.binding["postgres"]
        key = int.from_bytes(hashlib.sha256(("goby.library.owner\x00" + context["database"] + "\x00" + context["schema"]).encode()).digest()[:8], "big")
        query = ("SELECT json_build_object('pid',a.pid,'backend_start',a.backend_start::text)::text "
                 "FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE l.locktype='advisory' "
                 "AND l.granted AND l.objsubid=1 AND l.classid=" + str(key >> 32) + "::oid AND l.objid=" + str(key & 0xffffffff) +
                 "::oid AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database()) "
                 "AND a.datname=current_database() AND a.application_name='goby' AND a.pid<>pg_backend_pid();")
        answer = self.postgres(query)
        need(answer and len(answer.splitlines()) == 1, "owner_backend_not_unique")
        return decode(answer.encode())

    def postgres_observation(self):
        # These are server-observed waits, not a client request duration. The
        # statement outcome and rollback still require product/API evidence.
        query = ("SELECT coalesce(json_agg(json_build_object('pid',pid,'backend_start',backend_start::text,'application_name',application_name,"
                 "'xact_start',xact_start::text,'state',state,'wait_event_type',wait_event_type,'wait_event',wait_event,"
                 "'blocking_pids',pg_blocking_pids(pid),'query_start',query_start::text,'query_sha256',"
                 "encode(sha256(convert_to(query,'UTF8')),'hex')) ORDER BY pid),'[]'::json)::text "
                 "FROM pg_stat_activity WHERE datname=current_database() AND application_name IN "
                 "('goby','goby-phase3-fault-controller') AND pid<>pg_backend_pid();")
        rows = decode(self.postgres(query).encode())
        need(isinstance(rows, list) and len(rows) <= 128, "postgres_observation_budget")
        try:
            owner = self.owner_backend()
        except FaultError as error:
            if str(error) != "owner_backend_not_unique":
                raise
            owner = None
        settings = decode(self.postgres("SELECT json_build_object('server_version_num',current_setting('server_version_num'),"
            "'logging_collector',current_setting('logging_collector'),'log_destination',current_setting('log_destination'),"
            "'log_error_verbosity',current_setting('log_error_verbosity'),'log_directory',current_setting('log_directory'),"
            "'log_filename',current_setting('log_filename'),'current_json_log',pg_current_logfile('jsonlog'))::text;").encode())
        return {"owner_backend": owner, "backends": rows, "postgres": self.service("postgres"),
                "log_settings": settings, "observed_unix_ns": time.time_ns()}

    def postgres_log(self, payload):
        fields(payload, ("cursor",))
        configuration = self.binding["postgres"]["json_log"]
        need(configuration is not None, "postgres_json_log_not_configured")
        fields(configuration, ("path", "format"))
        need(configuration["format"] == "postgresql-jsonlog-v17", "postgres_log_format")
        path = self.owned_path(configuration["path"])
        need(not any(Path(volume["mountpoint"]) in path.parents for volume in self.binding["volumes"].values()), "postgres_log_on_fault_volume")
        observed = self.postgres_observation()["log_settings"]
        need(170000 <= int(observed["server_version_num"]) < 180000 and observed["logging_collector"] == "on" and
             "jsonlog" in {value.strip() for value in observed["log_destination"].split(",")} and observed["log_error_verbosity"] == "verbose" and
             isinstance(observed["current_json_log"], str), "postgres_json_log_settings")
        active = Path(observed["current_json_log"])
        if not active.is_absolute():
            active = Path(self.binding["postgres"]["data_directory"]) / active
        need(active.resolve(strict=True) == path, "postgres_active_log_changed")
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            before = os.fstat(fd)
            need(stat.S_ISREG(before.st_mode) and before.st_uid in {0, self.binding["services"]["postgres"]["uid"]} and
                 before.st_nlink == 1 and stat.S_IMODE(before.st_mode) & 0o077 == 0 and before.st_size <= 512 << 20,
                 "postgres_log_identity")
            cursor = payload["cursor"]
            if cursor is None:
                # Establish an exact pre-fault tail, without reusing old errors.
                tail_start = max(0, before.st_size - (1 << 20))
                tail = os.pread(fd, before.st_size - tail_start, tail_start)
                last = tail.rfind(b"\n")
                need(last >= 0 or tail_start == 0, "postgres_log_row_budget")
                offset = tail_start + last + 1 if last >= 0 else 0
                raw = b""
            else:
                fields(cursor, ("device", "inode", "offset"))
                need(cursor["device"] == before.st_dev and cursor["inode"] == before.st_ino, "postgres_log_rotated")
                offset = integer(cursor["offset"], 0, before.st_size)
                raw = os.pread(fd, min(1 << 20, before.st_size - offset), offset)
                # Keep the cursor at a complete JSONL boundary. A partial
                # record is observed again; an oversized single row fails.
                if raw and not raw.endswith(b"\n"):
                    last = raw.rfind(b"\n")
                    need(last >= 0 or len(raw) < 1 << 20, "postgres_log_row_budget")
                    raw = raw[:last + 1] if last >= 0 else b""
            after = os.fstat(fd)
            current = path.lstat()
            need((before.st_dev, before.st_ino) == (after.st_dev, after.st_ino) == (current.st_dev, current.st_ino) and
                 after.st_size >= before.st_size, "postgres_log_rotated_or_truncated")
            summary = lambda info: {"device": info.st_dev, "inode": info.st_ino, "size_bytes": info.st_size, "mtime_ns": info.st_mtime_ns}
            return {"format": configuration["format"], "before": summary(before), "after": summary(after),
                    "start_offset": offset, "end_offset": offset + len(raw), "raw_base64": base64.b64encode(raw).decode("ascii"),
                    "raw_sha256": hashlib.sha256(raw).hexdigest(), "raw_bytes": len(raw),
                    "next_cursor": {"device": before.st_dev, "inode": before.st_ino, "offset": offset + len(raw)},
                    "log_settings": observed, "observed_unix_ns": time.time_ns()}
        finally:
            os.close(fd)

    def fd_inventory(self, identity):
        root = Path("/proc") / str(identity["pid"])
        need(proc_identity(identity["pid"]) == identity, "fd_process_identity_changed")
        records = []
        for descriptor in (root / "fd").iterdir():
            need(len(records) < 8192, "fd_observation_budget")
            try:
                link = os.readlink(descriptor)
                info = (root / "fdinfo" / descriptor.name).read_text()
                values = dict(line.split(":", 1) for line in info.splitlines() if ":" in line)
                records.append({"fd": int(descriptor.name), "target": link, "mount_id": int(values.get("mnt_id", "0").strip()),
                                "inode": int(values.get("ino", "0").strip()), "flags": values.get("flags", "").strip()})
            except FileNotFoundError:
                continue
        need(proc_identity(identity["pid"]) == identity, "fd_process_changed_during_observation")
        return records

    def spool_inventory(self):
        configured = self.binding.get("spool_directory")
        if configured is None:
            return None
        root = self.owned_path(configured)
        need(not any(Path(volume["mountpoint"]) == root or Path(volume["mountpoint"]) in root.parents
                     for volume in self.binding["volumes"].values()), "spool_on_fault_volume")
        uid = self.binding["services"]["goby"]["uid"]
        root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        generations, controls, total_bytes, allocated, inodes, entries = [], [], 0, 0, 1, 0
        deadline = time.monotonic() + 5
        def safe_info(info, directory):
            need(info.st_uid == uid and (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)) and
                 stat.S_IMODE(info.st_mode) == (0o700 if directory else 0o600) and (directory or info.st_nlink == 1),
                 "spool_entry_ownership")
        def record(name, info):
            return {"name": name, "device": info.st_dev, "inode": info.st_ino, "mtime_ns": info.st_mtime_ns,
                    "bytes": info.st_size, "allocated_bytes": info.st_blocks * 512}
        try:
            before = os.fstat(root_fd)
            safe_info(before, True)
            with os.scandir(root_fd) as children:
                for child in children:
                    need(time.monotonic() < deadline and entries < 4096, "spool_inventory_budget")
                    info = os.stat(child.name, dir_fd=root_fd, follow_symlinks=False)
                    entries += 1
                    inodes += 1
                    if child.name in {".goby-scan-evidence.json", ".goby-scan-evidence.next", ".goby-scan-evidence.lock"}:
                        safe_info(info, False)
                        controls.append(record(child.name, info))
                        total_bytes += info.st_size
                        allocated += info.st_blocks * 512
                        continue
                    need(re.fullmatch(r"scan-evidence-[0-9a-f]{32}", child.name) and len(generations) < 64,
                         "unexpected_spool_generation")
                    safe_info(info, True)
                    child_fd = os.open(child.name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root_fd)
                    generation = record(child.name, info)
                    generation["entries"] = []
                    generation["bytes"], generation["allocated_bytes"] = 0, info.st_blocks * 512
                    try:
                        opened = os.fstat(child_fd)
                        need((info.st_dev, info.st_ino) == (opened.st_dev, opened.st_ino), "spool_generation_replaced")
                        with os.scandir(child_fd) as members:
                            for member in members:
                                need(time.monotonic() < deadline and entries < 4096, "spool_inventory_budget")
                                item = os.stat(member.name, dir_fd=child_fd, follow_symlinks=False)
                                safe_info(item, False)
                                need(item.st_dev == before.st_dev and item.st_size <= 1 << 30, "spool_member_scope")
                                generation["entries"].append(record(member.name, item))
                                generation["bytes"] += item.st_size
                                generation["allocated_bytes"] += item.st_blocks * 512
                                entries += 1
                                inodes += 1
                        after = os.fstat(child_fd)
                        named = os.stat(child.name, dir_fd=root_fd, follow_symlinks=False)
                        need((info.st_dev, info.st_ino, info.st_mtime_ns, info.st_ctime_ns) ==
                             (after.st_dev, after.st_ino, after.st_mtime_ns, after.st_ctime_ns) ==
                             (named.st_dev, named.st_ino, named.st_mtime_ns, named.st_ctime_ns), "spool_generation_changed")
                    finally:
                        os.close(child_fd)
                    generation["entries"].sort(key=lambda row: row["name"])
                    generations.append(generation)
                    total_bytes += generation["bytes"]
                    allocated += generation["allocated_bytes"]
                    need(total_bytes <= 4 << 30, "spool_byte_budget")
            after = os.fstat(root_fd)
            named = root.lstat()
            need((before.st_dev, before.st_ino, before.st_mtime_ns, before.st_ctime_ns) ==
                 (after.st_dev, after.st_ino, after.st_mtime_ns, after.st_ctime_ns) ==
                 (named.st_dev, named.st_ino, named.st_mtime_ns, named.st_ctime_ns), "spool_inventory_changed")
            return {"generations": sorted(generations, key=lambda row: row["name"]),
                    "control_files": sorted(controls, key=lambda row: row["name"]), "total_bytes": total_bytes,
                    "allocated_bytes": allocated, "total_inodes": inodes, "observed_unix_ns": time.time_ns()}
        finally:
            os.close(root_fd)

    def failure_errno(self, scenario, payload):
        fields(payload, ())
        fault = scenario["fault"]
        need(fault in {"permission_failure", "enospc"}, "errno_probe_fault_scope")
        volume = self.volume(scenario["volume_id"])
        target = self.volume_target(volume, scenario["relative_path"] if fault == "permission_failure" else "analysis-cache")
        identity = self.service("goby")
        status_text = (Path("/proc") / str(identity["pid"]) / "status").read_text()
        values = dict(line.split(":", 1) for line in status_text.splitlines() if ":" in line)
        uid, gid = int(values["Uid"].split()[0]), int(values["Gid"].split()[0])
        groups = [int(value) for value in values["Groups"].split()]
        need(uid == self.binding["services"]["goby"]["uid"] and proc_identity(identity["pid"]) == identity,
             "errno_probe_application_identity_changed")
        info = target.lstat()
        need(stat.S_ISDIR(info.st_mode), "errno_probe_target_not_directory")
        filename = ".goby-phase3-enospc-probe-" + hashlib.sha256(self.request_id.encode()).hexdigest()[:24]
        parameters = {"fault": fault, "target": str(target), "uid": uid, "filename": filename}
        child_source = r'''
import ctypes,errno,json,os,stat,sys
p=json.loads(sys.stdin.buffer.read(8193));assert os.getuid()==p['uid'] and os.getuid()!=0
libc=ctypes.CDLL(None,use_errno=True);assert libc.prctl(38,1,0,0,0)==0
s=dict(line.split(':',1) for line in open('/proc/self/status').read().splitlines() if ':' in line)
assert int(s['CapEff'].strip(),16)==0 and s['NoNewPrivs'].strip()=='1'
fd=None;created=False;removed=True;observed=None;number=None;path=p['target']
if p['fault']=='enospc': path=os.path.join(path,p['filename'])
try:
 if p['fault']=='permission_failure': fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW|os.O_CLOEXEC);os.listdir(fd)
 else:
  fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW|os.O_CLOEXEC,384);created=True
  data=b'x'*32768;view=memoryview(data)
  while view:
   n=os.write(fd,view);assert n>0;view=view[n:]
  os.fsync(fd)
 info=os.fstat(fd);observed={'device':info.st_dev,'inode':info.st_ino}
except OSError as error: number=error.errno
finally:
 if fd is not None: os.close(fd)
 if created:
  try: os.unlink(path)
  except OSError: removed=False
print(json.dumps({'errno':errno.errorcode.get(number) if number is not None else None,'errno_number':number,
 'probe_uid':os.getuid(),'probe_gid':os.getgid(),'target':path,'opened_identity':observed,'probe_file_created':created,
 'probe_file_removed':removed,'effective_capabilities':int(s['CapEff'].strip(),16),'no_new_privileges':True},separators=(',',':')))
'''
        result = command([self.tool("python"), "-I", "-B", "-c", child_source], timeout=10,
                         input_bytes=canonical(parameters), user=uid, group=gid, extra_groups=groups, process_receipt=True)
        facts = decode(result["stdout"].encode())
        need(facts["probe_uid"] == uid and facts["effective_capabilities"] == 0 and facts["probe_file_removed"] is True,
             "errno_probe_resource_closure")
        return {**facts, "owned_child": result["owned_child"], "volume_id": scenario["volume_id"],
                "target_directory_identity": {"device": info.st_dev, "inode": info.st_ino}, "observed_unix_ns": time.time_ns()}

    def scenario_closure(self, scenario, volumes):
        fault, volume_id = scenario["fault"], scenario["volume_id"]
        result = {"target": None, "filler": None, "lock_unit": None, "mounts_at_target": [],
                  "suspended": volumes.get(volume_id, {}).get("suspended")}
        if volume_id is not None:
            volume = self.binding["volumes"][volume_id]
            target = Path(volume["mountpoint"])
            if fault in {"changed_nested_mount", "permission_failure"}:
                target = target / scenario["relative_path"]
            result["mounts_at_target"] = [row for row in mountinfo() if row["mountpoint"] == str(target)]
            if fault in {"permission_failure", "changed_root_mount", "changed_nested_mount"}:
                target = self.owned_path(str(target))
                info = target.lstat()
                need(not stat.S_ISLNK(info.st_mode), "closure_target_symlink")
                result["target"] = {"device": info.st_dev, "inode": info.st_ino, "mode": stat.S_IMODE(info.st_mode),
                                    "uid": info.st_uid, "gid": info.st_gid, "exists": True}
            if fault == "enospc":
                filler = Path(volume["mountpoint"]) / (".goby-phase3-fill-" + self.binding["run_id"])
                try:
                    info = filler.lstat()
                except FileNotFoundError:
                    result["filler"] = {"exists": False}
                else:
                    need(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_nlink == 1, "closure_filler_identity")
                    result["filler"] = {"exists": True, "device": info.st_dev, "inode": info.st_ino, "bytes": info.st_size}
        if fault == "postgres_lock_wait":
            unit = "goby-phase3-lock-" + hashlib.sha256((self.binding["run_id"] + scenario["scenario_id"]).encode()).hexdigest()[:20] + ".service"
            raw = command([self.tool("systemctl"), "show", unit, "--property=MainPID,ActiveState,SubState,ControlGroup,LoadState"])
            values = dict(row.split("=", 1) for row in raw.splitlines())
            cgroup = values["ControlGroup"]
            need(cgroup in ("", "/system.slice/" + unit), "closure_lock_cgroup_changed")
            members = []
            if cgroup:
                path = Path("/sys/fs/cgroup") / cgroup.lstrip("/") / "cgroup.procs"
                try:
                    raw = path.read_bytes()
                except FileNotFoundError:
                    raw = b""
                need(len(raw) <= 65536, "closure_lock_pid_budget")
                members = [integer(int(value), 2, 1 << 30) for value in raw.splitlines()]
                need(len(members) <= 64, "closure_lock_pid_budget")
            result["lock_unit"] = {"name": unit, "main_pid": int(values["MainPID"]), "active_state": values["ActiveState"],
                                   "sub_state": values["SubState"], "load_state": values["LoadState"],
                                   "cgroup": cgroup, "cgroup_pids": members}
        return result

    def observe(self, scenario):
        guest = self.binding["guest"]
        btime = next(int(line.split()[1]) for line in Path("/proc/stat").read_text().splitlines() if line.startswith("btime "))
        services = {name: self.service(name, running=False) for name in ("goby", "postgres")}
        tasks, all_tasks, descriptors = [], [], []
        if services["goby"]["pid"]:
            pid = services["goby"]["pid"]
            descriptors = self.fd_inventory(services["goby"])
            fd_targets = {row["fd"]: row["target"] for row in descriptors}
            for task in (Path("/proc") / str(pid) / "task").iterdir():
                need(len(all_tasks) < 1024, "task_observation_budget")
                try:
                    raw = (task / "stat").read_text()
                    tail = raw[raw.rfind(")") + 2:].split()
                    syscall = (task / "syscall").read_text().strip()
                    tokens = syscall.split()
                    first_argument = int(tokens[1], 0) if len(tokens) >= 2 and tokens[0] != "running" else -1
                    observed = {"pid": pid, "start_ticks": services["goby"]["start_ticks"], "tid": int(task.name),
                                "task_start_ticks": int(tail[19]), "state": tail[0], "syscall": syscall,
                                "syscall_first_fd_target": fd_targets.get(first_argument), "architecture": platform.machine(),
                                "wchan": (task / "wchan").read_text().strip(), "cgroup": services["goby"]["cgroup"]}
                    all_tasks.append(observed)
                    if tail[0] == "D":
                        tasks.append(observed)
                except FileNotFoundError:
                    continue
        volumes = {}
        for volume_id, volume in self.binding["volumes"].items():
            mapper = Path("/dev/mapper") / volume["mapper_name"]
            if not mapper.exists():
                volumes[volume_id] = {"present": False, "mounted": False}
                continue
            self.volume(volume_id, mounted=False)
            device = mapper.resolve(strict=True)
            sysdir = Path("/sys/class/block") / device.name
            backing = Path("/sys/class/block") / Path(volume["loop_device"]).name / "loop/backing_file"
            mounts = [row for row in mountinfo() if row["mountpoint"] == volume["mountpoint"]]
            volumes[volume_id] = {"present": True, "mounted": len(mounts) == 1 and mounts[0]["major_minor"] == volume["major_minor"],
                                  "dm_uuid": (sysdir / "dm/uuid").read_text().strip(),
                                  "major_minor": (sysdir / "dev").read_text().strip(),
                                  "loop_backing_file": "/" + backing.read_text().strip().lstrip("/"),
                                  "size_bytes": int((sysdir / "size").read_text().strip()) * 512,
                                  "suspended": (sysdir / "dm/suspended").read_text().strip() == "1"}
        return {"owner_id": self.binding["owner_id"], **guest, "owner_marker_sha256": self.binding["owner_marker"]["sha256"],
                "boot_id": Path("/proc/sys/kernel/random/boot_id").read_text().strip(), "btime": btime,
                **services, "volumes": self.binding["volumes"], "mounts": mountinfo(), "blocked_tasks": tasks,
                "volume_observations": volumes, "fd_inventory": descriptors, "task_inventory": all_tasks,
                "spool_inventory": self.spool_inventory(),
                "scenario_closure": self.scenario_closure(scenario, volumes),
                "observed_unix_ns": time.time_ns()}

    def prepare(self):
        prepared, directories, parent_chains = [], {}, {}
        for volume_id, volume in self.binding["volumes"].items():
            parent_chains[volume_id] = self.fixture_parent_chain(volume["mountpoint"])
        for volume_id, volume in self.binding["volumes"].items():
            safe_id(volume_id)
            integer(volume["size_bytes"], 16 << 20, MAX_VOLUME)
            need(volume["size_bytes"] % 512 == 0, "unaligned_volume_size")
            need(re.fullmatch(r"/dev/loop[0-9]{1,3}", volume["loop_device"]) and
                 re.fullmatch(r"goby-phase3-[a-z0-9-]{1,64}", volume["mapper_name"]), "unsafe_volume_names")
            need(volume["dm_uuid"].startswith("GOBY-PHASE3-" + self.binding["owner_id"] + "-"), "unowned_dm_uuid")
            backing = self.owned_path(volume["backing_file"], exists=False)
            mount = self.owned_path(volume["mountpoint"], exists=False)
            need(not backing.exists() and not mount.exists(), "prepare_target_exists")
            need(not (Path("/sys/class/block") / Path(volume["loop_device"]).name / "loop/backing_file").exists(), "loop_already_used")
            need(not (Path("/dev/mapper") / volume["mapper_name"]).exists(), "mapper_already_used")
            need(backing.parent.resolve(strict=True) == backing.parent and mount.parent.resolve(strict=True) == mount.parent,
                 "prepare_parent_missing")
            free = os.statvfs(backing.parent)
            need(free.f_bavail * free.f_frsize >= volume["size_bytes"] + (2 << 30), "guest_disk_headroom")
            fd = os.open(backing, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            try:
                os.posix_fallocate(fd, 0, volume["size_bytes"])
                os.fsync(fd)
            finally:
                os.close(fd)
            mount.mkdir(mode=0o700)
            command([self.tool("losetup"), volume["loop_device"], str(backing)])
            command([self.tool("mkfs_ext4"), "-t", "ext4", "-F", "-U", volume["filesystem_uuid"], "-m", "0", volume["loop_device"]])
            table = "0 " + str(volume["size_bytes"] // 512) + " linear " + volume["loop_device"] + " 0"
            major, minor = volume["major_minor"].split(":")
            integer(int(major), 1, 4095)
            integer(int(minor), 0, 1 << 20)
            command([self.tool("dmsetup"), "create", volume["mapper_name"], "--major", major, "--minor", minor,
                     "--uuid", volume["dm_uuid"], "--table", table])
            mapper = Path("/dev/mapper") / volume["mapper_name"]
            real = mapper.resolve(strict=True)
            actual = (Path("/sys/class/block") / real.name / "dev").read_text().strip()
            need(actual == volume["major_minor"], "prepared_device_number_requires_refreeze")
            command([self.tool("mount"), "--no-canonicalize", "-t", "ext4", "-o", "rw,nosuid,nodev,noexec", str(mapper), str(mount)])
            self.volume(volume_id)
            gid = self.binding["fixture_access"]["media_read_gid"]
            actor = self.binding["fixture_access"]["actor_uid"]
            os.chown(mount, 0, gid, follow_symlinks=False)
            os.chmod(mount, 0o710, follow_symlinks=False)
            derivative = volume["purpose"] == "derivatives"
            target = mount / ("analysis-cache" if derivative else "fixture-media")
            target.mkdir(mode=0o700 if derivative else 0o750)
            owner = self.binding["services"]["goby"]["uid"] if derivative else actor
            os.chown(target, owner, gid, follow_symlinks=False)
            os.chmod(target, 0o700 if derivative else 0o750, follow_symlinks=False)
            for directory in (target, mount):
                fd = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
                try:
                    os.fsync(fd)
                finally:
                    os.close(fd)
            observed_uuid = command([self.tool("blkid"), "-p", "-s", "UUID", "-o", "value", str(mapper)])
            need(observed_uuid == volume["filesystem_uuid"], "prepared_filesystem_identity_changed")
            info = target.lstat()
            directories[volume_id] = {"fixture_path": str(target), "purpose": volume["purpose"], "device": info.st_dev,
                                      "major_minor": str(os.major(info.st_dev)) + ":" + str(os.minor(info.st_dev)),
                                      "filesystem_uuid": observed_uuid, "dm_uuid": volume["dm_uuid"], "writable_observed": "rw",
                                      "actor_uid": actor, "owner_uid": info.st_uid, "media_read_gid": info.st_gid,
                                      "mode": stat.S_IMODE(info.st_mode), "inode": info.st_ino}
            prepared.append(volume_id)
        return {"prepared_volumes": prepared, "fixture_directories": directories, "fixture_parent_chains": parent_chains,
                "corpus_populated": False, "runtime_admitted": False}

    def inject(self, scenario):
        fault = scenario["fault"]
        facts = {"fault": fault, "injected_unix_ns": time.time_ns()}
        if fault in {"blocked_read", "blocked_metadata", "mount_loss", "changed_root_mount", "changed_nested_mount", "permission_failure", "enospc"}:
            volume = self.volume(scenario["volume_id"])
            mount = self.owned_path(volume["mountpoint"])
        if fault in {"blocked_read", "blocked_metadata"}:
            need(volume["purpose"] == "media" and not volume["writable"], "blocked_io_requires_readonly_media")
            mounted = next(item for item in mountinfo() if item["mountpoint"] == str(mount))
            need("ro" in mounted["options"].split(","), "fault_volume_not_readonly")
            if fault in {"blocked_read", "blocked_metadata"}:
                # Per-guest cache invalidation is explicit in the release. It
                # does not itself prove a blocked metadata operation: the
                # independent observer must capture the actual Goby task.
                need(self.binding["allow_guest_cache_drop"] is True, "guest_cache_drop_not_released")
                fd = os.open(mount, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
                try:
                    os.syncfs(fd) if hasattr(os, "syncfs") else command([self.tool("sync"), "-f", str(mount)])
                finally:
                    os.close(fd)
                Path("/proc/sys/vm/drop_caches").write_text("3\n")
            command([self.tool("dmsetup"), "suspend", "--noflush", "--nolockfs", volume["mapper_name"]])
            facts.update(mechanism="dm_suspend", dm_uuid=volume["dm_uuid"], suspended=True)
        elif fault == "mount_loss":
            command([self.tool("umount"), "--lazy", "--", str(mount)])
            facts.update(mechanism="owned_unmount", mountpoint=str(mount))
        elif fault in {"changed_root_mount", "changed_nested_mount"}:
            replacement = self.volume(scenario["replacement_volume_id"])
            target = self.volume_target(volume, "." if fault == "changed_root_mount" else scenario["relative_path"])
            need(target.is_dir(), "replacement_target_not_directory")
            before = [row for row in mountinfo() if row["mountpoint"] == str(target)]
            command([self.tool("mount"), "--bind", "--", replacement["mountpoint"], str(target)])
            after = [row for row in mountinfo() if row["mountpoint"] == str(target)]
            facts.update(mechanism="owned_bind_mount", target=str(target), before_mounts=before, after_mounts=after)
        elif fault == "permission_failure":
            need(volume["writable"], "permission_fault_requires_writable_volume")
            target = self.volume_target(volume, scenario["relative_path"])
            info = target.lstat()
            need(stat.S_ISDIR(info.st_mode), "permission_target_not_directory")
            facts.update(mechanism="owned_chmod", target=str(target), before_mode=stat.S_IMODE(info.st_mode),
                         before_device=info.st_dev, before_inode=info.st_ino)
            # Persist restoration facts before the only chmod mutation.
            save_new(self.operations / (scenario["scenario_id"] + "-permission-before.json"), facts)
            os.chmod(target, 0, follow_symlinks=False)
        elif fault == "enospc":
            need(volume["purpose"] == "derivatives" and volume["writable"], "enospc_requires_derivative_volume")
            filler = mount / (".goby-phase3-fill-" + self.binding["run_id"])
            fd = os.open(filler, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            written, no_space = 0, False
            deadline = time.monotonic() + 60
            try:
                block = b"\x00" * (1 << 20)
                while written < volume["size_bytes"] and time.monotonic() < deadline:
                    try:
                        written += os.write(fd, block[:min(len(block), volume["size_bytes"] - written)])
                    except OSError as error:
                        if error.errno != errno.ENOSPC:
                            raise
                        no_space = True
                        break
                try:
                    os.fsync(fd)
                except OSError as error:
                    if error.errno != errno.ENOSPC:
                        raise
                    no_space = True
            finally:
                os.close(fd)
            need(no_space, "volume_did_not_reach_enospc")
            facts.update(mechanism="owned_volume_fill", filler=str(filler), filler_bytes=written, errno="ENOSPC")
        elif fault == "postgres_disconnect":
            owner = self.owner_backend()
            backend_start = owner["backend_start"].replace("'", "''")
            query = ("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid=" + str(integer(owner["pid"], 2)) +
                     " AND backend_start='" + backend_start + "'::timestamptz AND datname=current_database() "
                     "AND application_name='goby' AND pid<>pg_backend_pid();")
            answer = self.postgres(query)
            need(answer == "t", "owned_backend_not_terminated")
            facts.update(mechanism="terminate_owned_backend", **owner, terminated=True)
        elif fault == "postgres_lock_wait":
            # A separate finite helper process holds one declared item row.
            # systemd owns its lifetime even when the RPC client disconnects.
            item = self.binding["postgres"]["lock_item_id"]
            safe_id(item)
            owner = self.owner_backend()
            lock_unit = "goby-phase3-lock-" + hashlib.sha256((self.binding["run_id"] + scenario["scenario_id"]).encode()).hexdigest()[:20] + ".service"
            command([self.tool("systemd_run"), "--quiet", "--unit", lock_unit, "--property=RuntimeMaxSec=120",
                     "--property=TimeoutStopSec=5", "--property=KillMode=control-group", "--",
                     self.tool("python"), "-I", "-B", str(Path(__file__).resolve()), "--binding", self.binding_path,
                     "--binding-sha256", self.binding_sha256, "--release", self.release_ref["path"],
                     "--release-sha256", self.release_ref["sha256"], "--lock-holder", "--parent-request-id", self.request_id])
            facts.update(mechanism="owned_row_lock", lock_unit=lock_unit, item_id=item, owner_backend=owner)
        elif fault == "process_crash":
            target = self.service("goby")
            need(hasattr(os, "pidfd_open") and hasattr(signal, "pidfd_send_signal"), "pidfd_required")
            fd = os.pidfd_open(target["pid"], 0)
            try:
                need(proc_identity(target["pid"]) == target, "process_changed_before_signal")
                signal.pidfd_send_signal(fd, signal.SIGKILL, None, 0)
            finally:
                os.close(fd)
            facts.update(mechanism="sigkill_owned_process", target=target)
        elif fault == "postgres_restart":
            self.postgres("SELECT 1;")
            target = self.service("postgres")
            command([self.tool("systemctl"), "restart", self.binding["services"]["postgres"]["unit"]])
            facts.update(mechanism="restart_owned_postgres", target=target)
        elif fault == "guest_reboot":
            unit = "goby-phase3-reboot-" + hashlib.sha256(self.binding["run_id"].encode()).hexdigest()[:16]
            command([self.tool("systemd_run"), "--quiet", "--unit", unit, "--on-active=5s", "--",
                     self.tool("systemctl"), "reboot", "--no-block"])
            facts.update(mechanism="guest_systemctl_reboot", unit=unit, delay_seconds=5)
        else:
            raise FaultError("unsupported_guest_fault")
        return facts

    def recover(self, scenario, injection):
        fault = scenario["fault"]
        if fault in {"guest_reboot", "guest_reset"}:
            self.reestablish_volumes()
        if fault in {"blocked_read", "blocked_metadata", "mount_loss", "changed_root_mount", "changed_nested_mount", "permission_failure", "enospc"} or scenario["late_mount"]:
            volume = self.volume(scenario["volume_id"], mounted=fault not in {"mount_loss", "changed_root_mount"} and not scenario["late_mount"])
        if fault in {"blocked_read", "blocked_metadata"}:
            command([self.tool("dmsetup"), "resume", volume["mapper_name"]])
        elif fault in {"changed_root_mount", "changed_nested_mount"}:
            target = volume["mountpoint"] if fault == "changed_root_mount" else str(Path(volume["mountpoint"]) / scenario["relative_path"])
            command([self.tool("umount"), "--", target])
        elif fault == "permission_failure":
            before = decode((self.operations / (scenario["scenario_id"] + "-permission-before.json")).read_bytes())
            target = self.owned_path(before["target"])
            info = target.lstat()
            need(info.st_dev == before["before_device"] and info.st_ino == before["before_inode"] and
                 stat.S_IMODE(info.st_mode) == 0, "permission_target_identity_changed")
            os.chmod(target, before["before_mode"], follow_symlinks=False)
        elif fault == "enospc":
            filler = self.owned_path(str(Path(volume["mountpoint"]) / (".goby-phase3-fill-" + self.binding["run_id"])))
            regular(filler, private=True, maximum=MAX_VOLUME)
            need(filler.stat().st_dev == Path(volume["mountpoint"]).stat().st_dev, "filler_device_changed")
            filler.unlink()
        elif fault == "postgres_lock_wait":
            unit = "goby-phase3-lock-" + hashlib.sha256((self.binding["run_id"] + scenario["scenario_id"]).encode()).hexdigest()[:20] + ".service"
            need(injection["lock_unit"] == unit, "unowned_lock_unit")
            command([self.tool("systemctl"), "stop", unit])
        if fault == "mount_loss":
            need(not any(row["mountpoint"] == volume["mountpoint"] for row in mountinfo()), "restore_mountpoint_busy")
            options = "rw" if volume["writable"] else "ro"
            command([self.tool("mount"), "--no-canonicalize", "-t", "ext4", "-o", options + ",nosuid,nodev,noexec",
                     "/dev/mapper/" + volume["mapper_name"], volume["mountpoint"]])
        if scenario["restart_goby_after"]:
            command([self.tool("systemctl"), "restart", self.binding["services"]["goby"]["unit"]])
        elif fault == "process_crash":
            command([self.tool("systemctl"), "start", self.binding["services"]["goby"]["unit"]])
        return {"recovery_requested": True, "resource_closure_established": False}

    def reestablish_volumes(self):
        """Reattach original bytes after reboot; never format or replace them."""
        self.validate_volumes()
        # Admit all paths and absent device slots before the first attachment;
        # a bad later volume must not leave a silently partial preparation.
        for volume in self.binding["volumes"].values():
            self.fixture_parent_chain(volume["mountpoint"])
            self.owned_path(volume["mountpoint"])
            backing = self.owned_path(volume["backing_file"])
            info = regular(backing, private=True, maximum=MAX_VOLUME)
            need(info.st_size == volume["size_bytes"] and Path(volume["mountpoint"]).is_dir(), "reboot_volume_path_changed")
            need(not (Path("/sys/class/block") / Path(volume["loop_device"]).name / "loop/backing_file").exists() and
                 not (Path("/dev/mapper") / volume["mapper_name"]).exists() and
                 not (Path("/sys/dev/block") / volume["major_minor"]).exists() and
                 not any(row["mountpoint"] == volume["mountpoint"] for row in mountinfo()), "reboot_volume_slot_occupied")
        for volume_id, volume in self.binding["volumes"].items():
            backing = self.owned_path(volume["backing_file"])
            info = regular(backing, private=True, maximum=MAX_VOLUME)
            need(info.st_size == volume["size_bytes"], "reboot_backing_size_changed")
            loop = Path(volume["loop_device"])
            sysloop = Path("/sys/class/block") / loop.name / "loop/backing_file"
            mapper = Path("/dev/mapper") / volume["mapper_name"]
            need(not sysloop.exists() and not mapper.exists() and
                 not any(row["mountpoint"] == volume["mountpoint"] for row in mountinfo()),
                 "reboot_volume_already_attached_requires_observation")
            major, minor = volume["major_minor"].split(":")
            need(not (Path("/sys/dev/block") / volume["major_minor"]).exists(), "reboot_device_number_occupied")
            command([self.tool("losetup"), str(loop), str(backing)])
            table = "0 " + str(volume["size_bytes"] // 512) + " linear " + str(loop) + " 0"
            command([self.tool("dmsetup"), "create", volume["mapper_name"], "--major", major, "--minor", minor,
                     "--uuid", volume["dm_uuid"], "--table", table])
            actual_uuid = command([self.tool("blkid"), "-p", "-s", "UUID", "-o", "value", str(mapper)])
            need(actual_uuid == volume["filesystem_uuid"], "original_filesystem_uuid_changed")
            options = "rw" if volume["writable"] else "ro"
            command([self.tool("mount"), "--no-canonicalize", "-t", "ext4", "-o", options + ",nosuid,nodev,noexec",
                     str(mapper), volume["mountpoint"]])
            self.volume(volume_id)

    def lock_holder(self, parent_request_id):
        safe_id(parent_request_id)
        operation_id = hashlib.sha256(parent_request_id.encode()).hexdigest()
        parent = decode((self.operations / (operation_id + "-intent.json")).read_bytes())
        need(parent["op"] == "inject" and parent["scenario"]["fault"] == "postgres_lock_wait", "lock_holder_parent_mismatch")
        self.release(parent)
        unit = "goby-phase3-lock-" + hashlib.sha256((self.binding["run_id"] + parent["scenario"]["scenario_id"]).encode()).hexdigest()[:20] + ".service"
        need("0::/system.slice/" + unit in Path("/proc/self/cgroup").read_text().splitlines(), "lock_holder_cgroup_mismatch")
        item = safe_id(self.binding["postgres"]["lock_item_id"])
        # statement_timeout and RuntimeMaxSec independently bound the blocker;
        # the product's shorter statement timeout must be observed separately.
        sql = ("BEGIN; SET LOCAL statement_timeout='110s'; SET LOCAL lock_timeout='5s'; "
               "SELECT id FROM public.items WHERE id='" + item + "' FOR UPDATE; "
               "SELECT pg_sleep(100); ROLLBACK;")
        self.postgres(sql, timeout=115)

    def run(self, request):
        fields(request, ("schema_version", "request_id", "op", "run_id", "owner_id", "source_revision",
                         "binding_sha256", "scenario", "payload"))
        need(request["schema_version"] == 1 and request["run_id"] == self.binding["run_id"] and
             request["owner_id"] == self.binding["owner_id"], "request_scope_mismatch")
        safe_id(request["request_id"])
        need(request["binding_sha256"] == self.binding_sha256, "binding_request_digest_mismatch")
        self.request_id = request["request_id"]
        safe_id(request["scenario"]["scenario_id"])
        self.scenario(request["scenario"])
        op = request["op"]
        need(op in MUTATIONS | {"observe", "observe_postgres", "observe_postgres_log"}, "unsupported_operation")
        self.guard()
        if op in MUTATIONS:
            self.release(request)
            operation_id = hashlib.sha256(request["request_id"].encode()).hexdigest()
            save_new(self.operations / (operation_id + "-intent.json"), request)
        if op == "observe":
            data = self.observe(request["scenario"])
        elif op == "observe_postgres":
            data = self.postgres_observation()
        elif op == "observe_postgres_log":
            data = self.postgres_log(request["payload"])
        elif op == "observe_failure_errno":
            data = self.failure_errno(request["scenario"], request["payload"])
        elif op == "prepare_volumes":
            data = self.prepare()
        elif op == "arm_late_mount":
            need(request["scenario"]["late_mount"] and request["scenario"]["fault"] in {"guest_reboot", "guest_reset"},
                 "late_mount_scope_mismatch")
            volume = self.volume(request["scenario"]["volume_id"])
            # Fault mounts are deliberately not installed in fstab or durable
            # mount units. They therefore remain absent until explicit restore.
            fstab = Path("/etc/fstab").read_text()
            need(volume["mountpoint"] not in fstab and volume["filesystem_uuid"] not in fstab and
                 volume["mapper_name"] not in fstab, "fault_volume_automount_configured")
            data = {"late_mount_armed": True, "requires_loop_dm_reestablishment_after_boot": True}
        elif op == "inject":
            data = self.inject(request["scenario"])
        elif op == "recover":
            data = self.recover(request["scenario"], request["payload"]["injection"])
        else:
            # No broad deletion, unmount-all, loop detach-all or process kill.
            # The external observer proves closure after the declared recovery.
            data = {"close_requested": True, "automatic_cleanup": False}
        if op in MUTATIONS:
            data["dispatches"] = 1
            save_new(self.operations / (operation_id + "-result.json"), data)
        return {"schema_version": 1, "request_id": request["request_id"], "op": op, "owner_id": self.binding["owner_id"],
                "vmid": 106, "status": "ok", "data": data}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binding", required=True)
    parser.add_argument("--binding-sha256", required=True)
    parser.add_argument("--release")
    parser.add_argument("--release-sha256")
    parser.add_argument("--lock-holder", action="store_true")
    parser.add_argument("--parent-request-id")
    args = parser.parse_args()
    try:
        need(sys.platform == "linux", "linux_guest_required")
        binding = decode(read_pinned({"path": args.binding, "sha256": args.binding_sha256}, private=True))
        guest = Guest(binding)
        guest.binding_path, guest.binding_sha256 = args.binding, args.binding_sha256
        need(bool(args.release) == bool(args.release_sha256), "incomplete_release_reference")
        guest.release_ref = {"path": args.release, "sha256": args.release_sha256} if args.release else None
        if args.lock_holder:
            guest.lock_holder(args.parent_request_id)
            return 0
        request = decode(sys.stdin.buffer.read(MAX_JSON + 1))
        result = guest.run(request)
        print(canonical(result).decode(), flush=True)
        return 0
    except BaseException as error:
        reason = str(error) if isinstance(error, FaultError) else type(error).__name__
        print(canonical({"status": "failed", "reason": reason, "automatic_retry": False,
                         "remote_resource_closure": "not_established"}).decode(), flush=True)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
