#!/usr/bin/env python3
"""Run the fixed source54 storage-recovery helper in an owned mount namespace.

This one-shot operator extends only the frozen paired runner's generated worker
and result protocol. The original preparation, session ownership, pair/HBA
guards, bounded systemd execution, and cleanup methods remain unchanged. It
must follow the completed source54 full run and creates new database identities.
Importing this module does not access the environment, files, processes, or DB.
"""

from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shlex
import signal
import stat
import subprocess
import sys
import time


WORK = Path("/opt/goby-test/exec-work-m3e")
SOURCE = WORK / "source-attempt-54"
MANIFEST = "c5a5cf6bf0afa973fbd2c087d300dbe6bd3079f92c7237b5a108d76ef8ae6fc3"
CORE = SOURCE / "scripts/test-env/run-client-backup-tests.py"
CORE_SHA256 = "4c76cfa22fd1915a270756d82e313ce6e7bbf3a5bcc857339fd6d254f5c894a2"
HELPER_SHA256 = "f0fb0f55381282aac59da6a1df32475402227f390f949aa49b2253d5f6cac205"
PRIOR_MOUNT = WORK / "root-topology-mount-verification-01"
PRIOR_WORKER_SHA256 = "d71fa1157419bff0d7964ea7b442a7b2bdfe20475e429484b12b74ca49f598f4"
PRIOR_REPORT_SHA256 = "8e3588a232a9838523485c587769e8d8a57bef28cacd31f6cefc6f96a800eb40"
FULL_SCOPE = WORK / "scan-reconciliation-full-execution-01"
FULL_UNIT = "goby-scan-reconciliation-full-controller-v1.service"
FULL_INVOCATION = "c8641f6597f0411ca832d052b885499d"
FULL_RUN = "20260912_053517_9cb0074fc731"
FULL_INNER_UNIT = "goby-client-backup-20260912-053517-9cb0074fc731.service"
FULL_REPORT = WORK / ("client-backup-run-" + FULL_RUN) / "report.json"
FULL_RECEIPT = FULL_REPORT.parent / "receipt-3fcb8703a01a924c.json"
FULL_RECEIPT_SHA256 = "6accf5be1fa133d5d9f82a270ead3a0e2e82e1b5e0685f1cfee4d09843a48479"
FAILED_RUN = "20260912_061505_b2b453a716a9"
FAILED_OUTPUT = WORK / ("client-backup-run-" + FAILED_RUN)
FAILED_RECEIPT_SHA256 = "190c202ca956a52b1914fabe7363ea821bd691f0a6f47869edc8785bc3fe288f"
BLOCK_CLASS = Path("/sys/class/block")
MAX_BLOCK_DEVICES = 4096
MAX_BLOCK_ATTRIBUTE_BYTES = 4096
MAX_BLOCK_INVENTORY_BYTES = 4 << 20
TEST_NAME = "TestRootBindingScanMountNamespaceHelper"
GO = Path("/opt/goby-toolchains/go1.27.1/bin/go")
PAIR_NAMES = ("goby_backup_m3e_source", "goby_backup_m3e_target")
VERIFICATION = "source54-original-storage-scan-recovery-private-mount-v2"
RECOVERY_MARKER = ("original_remount_recovered=true replacement_approval_learned=false "
                   "approval_row_unchanged=true sibling_anchor_unchanged=true old_leases_retained=true "
                   "fresh_mount_witness=true system_reboot_tested=false")
CLEANUP_MARKER = ("mount_cleanup_completed=true held_descriptors_closed=true "
                  "owned_mounts_remaining=0 fixture_removed=true")
BASE_ENVIRONMENT = {"PATH": "/usr/bin:/bin", "LANG": "C.UTF-8", "LC_ALL": "C.UTF-8"}


class Failure(Exception):
    """A fixed verification boundary could not be established."""


def require(value, message):
    if not value:
        raise Failure(message)


def digest(value):
    return hashlib.sha256(value).hexdigest()


def valid_digest(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def valid_namespace(value):
    return isinstance(value, str) and re.fullmatch(r"mnt:\[[1-9][0-9]*\]", value) is not None


def require_output(output):
    require(isinstance(output, Path) and output.parent == WORK and
            re.fullmatch(r"client-backup-run-[0-9]{8}_[0-9]{6}_[0-9a-f]{12}", output.name),
            "The output is outside a new paired-run directory.")


def compile_command(output):
    require_output(output)
    return [str(GO), "test", "-c", "-race", "-p=1", "-o", str(output / "tmp/library.test"), "./internal/library"]


def command_for_helper(binary):
    require(isinstance(binary, Path) and binary.name == "library.test" and binary.parent.name == "tmp",
            "The helper binary is not the fixed owned artifact.")
    require_output(binary.parent.parent)
    return ["/usr/bin/unshare", "--mount", "--fork", "--kill-child", "--propagation", "private", str(binary),
            "-test.run=^" + TEST_NAME + "$", "-test.v", "-test.timeout=120s"]


def helper_environment(output, host, source_url):
    require_output(output)
    require(valid_namespace(host), "An explicit host mount namespace witness is required.")
    require(isinstance(source_url, str) and re.fullmatch(
        r"postgresql://goby_backup_m3e_source:[0-9a-f]{64}@127\.0\.0\.1:15432/goby_backup_m3e_source\?sslmode=disable", source_url),
        "The helper URL is not this runner's dedicated source database identity.")
    fixtures = str(output / "tmp/mount-fixtures")
    return dict(BASE_ENVIRONMENT, GOTMPDIR=fixtures, TMPDIR=fixtures, GOMAXPROCS="2", GOMEMLIMIT="384MiB",
                GOBY_ROOT_BINDING_SCAN_MOUNT_HELPER="1", GOBY_ROOT_TOPOLOGY_HOST_MOUNT_NAMESPACE=host,
                GOBY_TEST_DATABASE_URL=source_url)


def require_exact_launch(command, environment, output, host, source_url):
    require(command == command_for_helper(output / "tmp/library.test"), "The private namespace helper argv changed.")
    require(environment == helper_environment(output, host, source_url), "The helper environment changed or gained another capability.")


def inspect_helper_output(stdout, stderr, exit_code):
    require(type(exit_code) is int and exit_code == 0 and stderr == b"", "The real mount helper failed or emitted stderr.")
    require(isinstance(stdout, bytes) and len(stdout) <= 8 << 20, "The helper output exceeds its bounded protocol.")
    try:
        text = stdout.decode("utf-8", "strict")
    except UnicodeError:
        raise Failure("The helper output is not UTF-8.") from None
    require("DATA RACE" not in text and "--- FAIL:" not in text and "--- SKIP:" not in text and
            not any(line.strip() in ("FAIL", "SKIP") for line in text.splitlines()),
            "The mount helper reported a failure, skip, or data race.")
    require(re.findall(r"(?m)^=== RUN   (\S+)$", text) == [TEST_NAME] and
            re.findall(r"(?m)^--- PASS: (\S+) \([^\n]*\)$", text) == [TEST_NAME] and
            text.splitlines().count("PASS") == 1,
            "The test stream did not execute exactly the fixed mount helper.")
    require(text.count(RECOVERY_MARKER) == 1 and text.count(CLEANUP_MARKER) == 1,
            "An inert PASS or incomplete helper cannot establish real mount recovery.")
    return {"test": TEST_NAME, "real_helper_completed": True, "recovery_marker": True, "fixture_cleanup_marker": True,
            "system_reboot_tested": False, "stdout_sha256": digest(stdout), "stderr_sha256": digest(stderr)}


def require_host_preservation(before, after):
    for value in (before, after):
        require(isinstance(value, dict) and set(value) == {"namespace", "mountinfo_sha256", "loops_sha256"} and
                valid_namespace(value["namespace"]) and valid_digest(value["mountinfo_sha256"]) and valid_digest(value["loops_sha256"]),
                "The host preservation witness is incomplete.")
    require(before == after, "The host mount namespace, mountinfo, or loop-device inventory changed.")


def require_full_prerequisite(report, receipt, terminal):
    require(isinstance(report, dict) and report.get("status") == "passed" and report.get("mode") == "full" and
            report.get("source") == str(SOURCE) and report.get("source_manifest_sha256") == MANIFEST and
            type(report.get("schema")) is int and report.get("schema") == 28,
            "The prerequisite is not the accepted source54 full regression.")
    require(report.get("run_id") == FULL_RUN and report.get("unit") == FULL_INNER_UNIT,
            "The full report is not the fixed paired run of the accepted controller invocation.")
    required_cleanup = {"unit_terminal", "hba_restored_exactly", "preexisting_catalog_unchanged", "receipt_saved",
                        PAIR_NAMES[0] + "_removed", PAIR_NAMES[1] + "_removed"}
    cleanup = report.get("cleanup", {})
    require(isinstance(cleanup, dict) and required_cleanup <= set(cleanup) and all(value is True for value in cleanup.values()),
            "The source54 full regression did not finish every cleanup boundary.")
    tests = report.get("tests", {})
    require(isinstance(tests, dict) and type(tests.get("top_level_passes")) is int and tests["top_level_passes"] > 0 and
            type(tests.get("failures")) is int and tests.get("failures") == 0 and
            type(tests.get("skips")) is int and tests.get("skips") == 0,
            "The source54 full report lacks complete passing test evidence.")
    require(isinstance(receipt, dict) and receipt.get("marker") == "goby-client-backup-pair-m3e-v1" and
            receipt.get("phase") == "finished" and receipt.get("cleanup_complete") is True and type(receipt.get("schema")) is int and
            receipt.get("output") == str(FULL_REPORT.parent) and
            all(receipt.get(key) == report.get(key) for key in ("run_id", "unit", "source", "source_manifest_sha256", "schema", "mode")),
            "The completed pair receipt does not belong to the exact source54 full report.")
    pairs = receipt.get("pairs", [])
    require(isinstance(pairs, list) and all(isinstance(pair, dict) for pair in pairs) and
            [pair.get("name") for pair in pairs] == list(PAIR_NAMES) and
            all(pair.get("phase") == "removed" for pair in pairs), "The source54 full pair has not been removed.")
    require(isinstance(terminal, dict) and isinstance(terminal.get("properties"), dict), "The full controller terminal evidence is malformed.")
    properties = terminal["properties"]
    require(terminal.get("unit") == FULL_UNIT and terminal.get("cgroup_empty") is True and
            properties.get("InvocationID") == FULL_INVOCATION and properties.get("MainPID") == "0" and
            properties.get("ExecMainStatus") == "0" and properties.get("Result") == "success" and
            properties.get("ControlGroup") in ("", "/system.slice/" + FULL_UNIT) and
            (properties.get("ActiveState"), properties.get("SubState")) in (("inactive", "dead"), ("active", "exited")),
            "The exact source54 full controller is not successfully terminal with an empty cgroup.")


def require_worker_admission(receipt, request, request_sha256, run_script_sha256):
    require(isinstance(receipt, dict) and isinstance(request, dict) and valid_digest(request_sha256) and valid_digest(run_script_sha256),
            "The private worker admission document is malformed.")
    require(request.get("verification") == VERIFICATION and request.get("source") == str(SOURCE) and request.get("manifest") == MANIFEST and
            valid_digest(request.get("worker_sha256")) and valid_namespace(request.get("host_namespace")),
            "The worker request does not identify the fixed source54 observation.")
    require(receipt.get("marker") == "goby-client-backup-pair-m3e-v1" and
            receipt.get("phase") in ("unit_pending", "running") and receipt.get("cleanup_complete") is False and
            receipt.get("mode") == "targeted" and type(receipt.get("schema")) is int and receipt.get("schema") == 28 and
            all(receipt.get(key) == request.get(key) for key in ("run_id", "unit", "tag", "output", "output_identity", "pairs")) and
            receipt.get("source") == str(SOURCE) and receipt.get("source_manifest_sha256") == MANIFEST,
            "The worker does not own the current new pair receipt.")
    run_id = request.get("run_id", "")
    require(isinstance(run_id, str) and re.fullmatch(r"[0-9]{8}_[0-9]{6}_[0-9a-f]{12}", run_id) and
            run_id not in (FULL_RUN, FAILED_RUN) and
            request.get("unit") == "goby-client-backup-" + run_id.replace("_", "-") + ".service" and
            request.get("tag") == "goby-client-backup-pair-m3e-v1:" + run_id and
            request.get("output") == str(WORK / ("client-backup-run-" + run_id)), "The worker run identity is incomplete.")
    pairs = request.get("pairs")
    require(isinstance(pairs, list) and all(isinstance(pair, dict) for pair in pairs) and
            [pair.get("name") for pair in pairs] == list(PAIR_NAMES) and all(pair.get("phase") == "owned" and
            type(pair.get("role_oid")) is int and pair["role_oid"] > 0 and type(pair.get("database_oid")) is int and pair["database_oid"] > 0 for pair in pairs),
            "The worker pair has not reached its exact newly owned phase.")
    require(receipt.get("bound_scan_mount") == {"verification": VERIFICATION, "request_sha256": request_sha256,
            "worker_sha256": request["worker_sha256"], "host_namespace": request["host_namespace"], "run_script_sha256": run_script_sha256},
            "The current receipt did not admit this exact private namespace worker.")


def load_core():
    info = CORE.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_gid == 0 and info.st_nlink == 1 and
            stat.S_IMODE(info.st_mode) in (0o600, 0o644) and digest(CORE.read_bytes()) == CORE_SHA256,
            "The frozen paired runner differs from the reviewed source54 input.")
    for path in (CORE, *CORE.parents):
        info = path.lstat()
        require(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022,
                "The frozen runner path is not root-controlled.")
    spec = importlib.util.spec_from_file_location("bound_scan_paired_runner", CORE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    require(module.WORK == WORK and module.GO == GO and tuple(module.NAMES) == PAIR_NAMES,
            "The frozen runner addresses a different execution or database scope.")
    return module


def controller_terminal(core):
    fields = "InvocationID,ActiveState,SubState,MainPID,ExecMainStatus,Result,ControlGroup"
    raw = core.command(["/usr/bin/systemctl", "show", FULL_UNIT, "--property=" + fields])
    properties = dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)
    cgroup = Path("/sys/fs/cgroup/system.slice") / FULL_UNIT / "cgroup.procs"
    empty = not core.present(cgroup) or not cgroup.read_text().strip()
    return {"unit": FULL_UNIT, "properties": properties, "cgroup_empty": empty}


def read_prerequisites(core, args):
    require(args.full_report == FULL_REPORT and valid_digest(args.full_report_sha256),
            "The full report is outside the fixed source54 controller run.")
    report_raw = core.private_read(args.full_report)
    require(digest(report_raw) == args.full_report_sha256, "The accepted full report bytes changed.")
    receipt_raw = core.private_read(FULL_RECEIPT)
    require(digest(receipt_raw) == FULL_RECEIPT_SHA256, "The exact historical full-run receipt changed.")
    terminal = controller_terminal(core)
    require_full_prerequisite(core.decode(report_raw), core.decode(receipt_raw), terminal)
    receipt = core.decode(receipt_raw)
    require(receipt.get("output") == str(args.full_report.parent) and
            core.directory(args.full_report.parent) == receipt.get("output_identity"),
            "The full report is outside its exact owned receipt directory.")
    prior = core.private_read(PRIOR_MOUNT / "report.json")
    require(digest(prior) == PRIOR_REPORT_SHA256 and digest(core.private_read(PRIOR_MOUNT / "worker.py")) == PRIOR_WORKER_SHA256,
            "The previously reviewed source42 namespace launcher evidence changed.")
    accepted = core.decode(prior)
    require(accepted.get("status") == "passed" and accepted.get("host_mountinfo_unchanged") is True and
            accepted.get("host_namespace_unchanged") is True and accepted.get("fixture_directory_empty") is True,
            "The source42 isolation launcher did not retain its accepted boundaries.")
    require(valid_namespace(args.host_namespace) and os.readlink("/proc/self/ns/mnt") == args.host_namespace == os.readlink("/proc/1/ns/mnt"),
            "The new controller does not match the explicit host namespace witness.")
    disposal = read_failed_disposal(core, core.decode(report_raw))
    return {"full_report": str(args.full_report), "full_report_sha256": digest(report_raw),
            "full_receipt": str(FULL_RECEIPT), "full_receipt_sha256": digest(receipt_raw), "full_controller_terminal": terminal,
            "full_run": FULL_RUN, "full_inner_unit": FULL_INNER_UNIT, "full_controller_scope": str(FULL_SCOPE),
            "failed_run_disposal": disposal,
            "prior_namespace_report_sha256": PRIOR_REPORT_SHA256, "prior_namespace_worker_sha256": PRIOR_WORKER_SHA256}


def read_failed_disposal(core, full_report):
    raw = core.private_read(core.RECEIPT)
    require(digest(raw) == FAILED_RECEIPT_SHA256, "The current receipt is not the exact retained mount failure.")
    previous = core.decode(raw)
    require(isinstance(previous, dict) and previous.get("run_id") == FAILED_RUN and
            previous.get("output") == str(FAILED_OUTPUT) and previous.get("phase") == "retained" and
            previous.get("cleanup_complete") is False and previous.get("mode") == "targeted" and
            previous.get("source") == str(SOURCE) and previous.get("source_manifest_sha256") == MANIFEST and
            type(previous.get("schema")) is int and previous.get("schema") == 28,
            "The predecessor is not the preserved source54 mount failure.")
    hba = full_report.get("hba_before_sha256")
    require(valid_digest(hba) and full_report.get("hba_after_sha256") == hba and
            isinstance(full_report.get("cluster"), dict) and isinstance(previous.get("cluster"), dict) and
            full_report["cluster"].get("system_identifier") is not None and
            full_report["cluster"].get("system_identifier") == previous["cluster"].get("system_identifier"),
            "The completed full report does not establish the predecessor's cluster and restored HBA.")
    path = core.CONTROL / ("client-backup-disposal-" + FAILED_RUN + ".json")
    disposal_raw = core.private_read(path)
    disposal = core.decode(disposal_raw)
    # Reuse the frozen runner's exact OID, receipt, cluster and HBA binding.
    # Its ordinary prepare() will independently repeat this gate under its lock.
    core.validate_disposal(previous, raw, disposal, hba)
    require(core.directory(FAILED_OUTPUT) == previous.get("output_identity"),
            "The retained failure output directory changed before disposal admission.")
    report_path = FAILED_OUTPUT / "disposal-report.json"
    require(disposal.get("report_path") == str(report_path), "The disposal report is outside the retained failure directory.")
    report_raw = core.private_read(report_path)
    require(digest(report_raw) == disposal.get("report_sha256"), "The independently bound disposal report changed.")
    return {"run_id": FAILED_RUN, "source_receipt_sha256": digest(raw), "receipt": str(path),
            "receipt_sha256": digest(disposal_raw), "report": str(report_path), "report_sha256": digest(report_raw)}


def block_class_names():
    names, used = set(), 0
    with os.scandir(BLOCK_CLASS) as entries:
        for entry in entries:
            name = entry.name
            require(isinstance(name, str) and name not in ("", ".", "..") and "/" not in name and "\0" not in name and
                    len(name.encode("utf-8", "strict")) <= 255 and name not in names,
                    "The complete block class has an invalid or repeated member.")
            used += 128 + len(name.encode("utf-8"))
            require(len(names) < MAX_BLOCK_DEVICES and used <= MAX_BLOCK_INVENTORY_BYTES,
                    "The complete block class exceeds its finite inventory budget.")
            names.add(name)
    return sorted(names)


def block_attribute(path):
    with path.open("rb") as handle:
        raw = handle.read(MAX_BLOCK_ATTRIBUTE_BYTES + 1)
    require(0 < len(raw) <= MAX_BLOCK_ATTRIBUTE_BYTES, "A required block-device attribute is empty or oversized.")
    return raw.decode("utf-8", "strict")


def block_class_inventory_once():
    before = BLOCK_CLASS.lstat()
    require(stat.S_ISDIR(before.st_mode) and before.st_uid == 0 and before.st_gid == 0 and not before.st_mode & 0o022,
            "The required block class is not its root-controlled directory.")
    names = block_class_names()
    devices, used = {}, 0
    for name in names:
        device = BLOCK_CLASS / name
        link = device.lstat()
        require(stat.S_ISLNK(link.st_mode), "A block class member is not its sysfs device link.")
        target = device.resolve(strict=True)
        require(target.is_relative_to(Path("/sys/devices")) and target.name == name and len(str(target).encode()) <= 4096,
                "A block class member resolves outside the bounded sysfs device tree.")
        number = block_attribute(device / "dev")
        require(re.fullmatch(r"[0-9]+:[0-9]+\n?", number), "A block class member lacks an exact device number.")
        values = None
        if re.fullmatch(r"loop[0-9]+", name):
            require(number.split(":", 1)[0] == "7", "A loop device has a different major number.")
            values = {field: block_attribute(device / "loop" / field)
                      for field in ("backing_file", "offset", "sizelimit", "autoclear")}
            require(all(re.fullmatch(r"[0-9]+\n?", values[field]) for field in ("offset", "sizelimit", "autoclear")),
                    "A loop device has incomplete numeric configuration attributes.")
        after = device.lstat()
        require((link.st_dev, link.st_ino, link.st_mode) == (after.st_dev, after.st_ino, after.st_mode) and
                device.resolve(strict=True) == target and block_attribute(device / "dev") == number,
                "A block class member changed during inventory.")
        used += 512 + len(name.encode()) + len(str(target).encode()) + len(number.encode())
        if values is not None:
            used += sum(128 + len(value.encode()) for value in values.values())
        require(used <= MAX_BLOCK_INVENTORY_BYTES, "The complete block inventory exceeds its byte budget.")
        devices[name] = {"target": str(target), "dev": number, "loop": values}
    after = BLOCK_CLASS.lstat()
    require((before.st_dev, before.st_ino, before.st_mode) == (after.st_dev, after.st_ino, after.st_mode) and
            block_class_names() == names, "The complete block class changed during inventory.")
    return devices


def block_class_inventory():
    # Repeated complete observations detect visible changes without claiming
    # that sysfs and the external test are protected by a shared atomic lock.
    try:
        first, second = block_class_inventory_once(), block_class_inventory_once()
    except (OSError, UnicodeError) as error:
        raise Failure("The required complete block class or one of its attributes is unavailable.") from error
    require(first == second, "The repeated complete block inventory changed.")
    return {"path": str(BLOCK_CLASS), "complete": True, "observations": 2, "device_count": len(first),
            "loop_count": sum(device["loop"] is not None for device in first.values()), "devices": first}


def host_witness(core, output, suffix):
    host = os.readlink("/proc/1/ns/mnt")
    require(os.readlink("/proc/self/ns/mnt") == host, "The controller left the original host mount namespace.")
    mountinfo = Path("/proc/1/mountinfo").read_bytes()
    loops = block_class_inventory()
    loop_raw = json.dumps(loops, sort_keys=True).encode()
    require(len(loop_raw) <= MAX_BLOCK_INVENTORY_BYTES, "The serialized complete block inventory exceeds its byte budget.")
    core.create_private(output / ("host-mountinfo-" + suffix), mountinfo)
    core.create_private(output / ("host-loops-" + suffix + ".json"), loop_raw)
    return {"namespace": host, "mountinfo_sha256": digest(mountinfo), "loops_sha256": digest(loop_raw)}


def artifact(core, path, modes=(0o700, 0o755)):
    raw = core.private_read(path, limit=128 << 20, modes=modes)
    require(len(raw) > 1 << 20, "The compiled helper artifact is unexpectedly small.")
    info = path.lstat()
    return {"path": str(path), "sha256": digest(raw), "bytes": len(raw), "device": info.st_dev, "inode": info.st_ino}


def observed_namespace(process, command, host, unit):
    directory = Path("/proc") / str(process.pid)
    try:
        namespace = os.readlink(directory / "ns/mnt")
        arguments = (directory / "cmdline").read_bytes().split(b"\0")[:-1]
        cgroup = (directory / "cgroup").read_text()
        status = (directory / "stat").read_text()
    except FileNotFoundError:
        return None
    if namespace == host:
        return None
    require(valid_namespace(namespace) and arguments == [argument.encode() for argument in command] and
            cgroup.splitlines() == ["0::/system.slice/" + unit],
            "The namespace observer does not identify the exact owned unshare process.")
    fields = status[status.rfind(")") + 2:].split()
    require(len(fields) > 19 and fields[19].isdigit(), "The unshare process start witness is incomplete.")
    return {"namespace": namespace, "pid": process.pid, "start_ticks": fields[19], "cgroup": "/system.slice/" + unit}


def finish_owned_child(process):
    """Terminate only the exact child retained by this worker's Popen call."""
    if process is None:
        return {"terminal": True, "killed": False}
    killed = process.poll() is None
    if killed:
        process.kill()
        process.wait(timeout=15)
    require(process.poll() is not None, "The exact owned namespace child did not become terminal.")
    return {"terminal": True, "killed": killed}


def worker(request_path):
    core = load_core()
    require(request_path.name == "mount-request.json", "The worker request name is invalid.")
    output = request_path.parent
    require_output(output)
    request_bytes = core.private_read(request_path)
    request = core.decode(request_bytes)
    require(request.get("verification") == VERIFICATION and request.get("source") == str(SOURCE) and request.get("manifest") == MANIFEST and
            core.directory(output) == request.get("output_identity") and request.get("output") == str(output),
            "The worker request does not match its fixed source and owned output.")
    report = {"verification": VERIFICATION, "source": str(SOURCE), "manifest": MANIFEST, "schema": 28,
              "run_id": request["run_id"], "status": "failed", "system_reboot_tested": False,
              "loop_scope": "observed-only-no-loop-allocation"}
    process = None
    before = None
    try:
        require(digest(core.private_read(output / "mount-worker.py")) == request["worker_sha256"] and
                Path(__file__) == output / "mount-worker.py", "The fixed worker entrypoint changed.")
        require(digest(core.private_read(output / "run.env")) == request["environment_sha256"], "The new run environment changed.")
        receipt = core.decode(core.private_read(core.RECEIPT))
        require_worker_admission(receipt, request, digest(request_bytes), digest(core.private_read(output / "run.sh")))
        require(os.geteuid() == 0 and os.getuid() == 0 and
                Path("/proc/self/cgroup").read_text().splitlines() == ["0::/system.slice/" + request["unit"]],
                "The worker is outside its exact bounded root unit.")
        invocation = os.environ.get("INVOCATION_ID", "")
        require(re.fullmatch(r"[0-9a-f]{32}", invocation), "The worker has no systemd invocation witness.")
        report["invocation_id"] = invocation
        core.verify_source(SOURCE, MANIFEST, 28)
        require(digest(core.private_read(SOURCE / "internal/library/root_binding_scan_mount_namespace_test.go", modes=(0o600, 0o644))) == HELPER_SHA256,
                "The actual mount helper differs from the reviewed source54 test.")
        raw_environment = core.private_read(output / "run.env").decode()
        environment = dict(line.split("=", 1) for line in raw_environment.splitlines())
        require(os.environ.get("GOBY_TEST_DATABASE_URL") == environment.get("GOBY_TEST_DATABASE_URL") ==
                environment.get("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL") and environment.get("GOBY_BACKUP_PG_RUN_ID") == request["run_id"],
                "The worker did not receive this new run's dedicated source identity.")
        source_url = environment["GOBY_TEST_DATABASE_URL"]
        runtime_environment = helper_environment(output, request["host_namespace"], source_url)
        temporary = output / "tmp"
        core.directory(temporary)
        binary = temporary / "library.test"
        require(not core.present(binary), "An earlier compiled helper artifact must not be adopted.")
        compile_keys = ("PATH", "LANG", "LC_ALL", "HOME", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN", "GOWORK", "GOFLAGS", "GOMEMLIMIT", "GOMAXPROCS", "GOTMPDIR", "TMPDIR")
        compile_environment = {key: environment[key] for key in compile_keys}
        compile_environment.update(GOPROXY="off", GOSUMDB="off", CGO_ENABLED="1")
        with (output / "compile.stdout").open("xb") as out, (output / "compile.stderr").open("xb") as err:
            compiled = subprocess.run(compile_command(output), cwd=SOURCE, env=compile_environment, stdin=subprocess.DEVNULL,
                                      stdout=out, stderr=err, timeout=600)
        require(compiled.returncode == 0, "The fixed source54 race-enabled test binary did not compile.")
        report["binary"] = artifact(core, binary)
        core.verify_source(SOURCE, MANIFEST, 28)
        fixtures = temporary / "mount-fixtures"
        fixtures.mkdir(mode=0o700)
        core.directory(fixtures)
        # The Go helper checks the owned canonical GOTMPDIR is actually ext4.
        before = host_witness(core, output, "before")
        require(before["namespace"] == request["host_namespace"], "The explicit host witness changed before unshare.")
        command = command_for_helper(binary)
        require_exact_launch(command, runtime_environment, output, request["host_namespace"], source_url)
        unshare_bytes = core.private_read(Path(command[0]), modes=(0o755,))
        report["unshare_sha256"] = digest(unshare_bytes)
        observed = None
        with (output / "helper.stdout").open("xb") as out, (output / "helper.stderr").open("xb") as err:
            process = subprocess.Popen(command, cwd=SOURCE, env=runtime_environment, stdin=subprocess.DEVNULL, stdout=out, stderr=err)
            deadline = time.monotonic() + 150
            while process.poll() is None:
                current = observed_namespace(process, command, request["host_namespace"], request["unit"])
                if current is not None:
                    require(observed is None or current == observed, "The owned private namespace process changed identity.")
                    observed = current
                require(time.monotonic() < deadline, "The bounded namespace helper exceeded its deadline.")
                time.sleep(0.01)
        report["helper"] = inspect_helper_output(core.private_read(output / "helper.stdout"), core.private_read(output / "helper.stderr"), process.returncode)
        require(observed is not None and not Path("/proc", str(process.pid)).exists(),
                "The exact private namespace was not observed running and then terminal.")
        report["private_namespace"] = dict(observed, subprocess_terminal=True)
        require(not list(fixtures.iterdir()), "The real helper retained an owned fixture after cleanup.")
        report["fixture_directory_empty"] = True
        require(artifact(core, binary) == report["binary"], "The executed helper artifact changed.")
        require(digest(core.private_read(Path(command[0]), modes=(0o755,))) == report["unshare_sha256"],
                "The private namespace executable changed during verification.")
        core.verify_source(SOURCE, MANIFEST, 28)
        report.update(source_unchanged=True, binary_unchanged=True, status="passed")
    except Exception as error:
        report["failure"] = str(error) if isinstance(error, (Failure, core.Failure)) else type(error).__name__
    finally:
        try:
            # unshare --kill-child terminates its exact child. The inherited
            # runner independently verifies the whole owned cgroup afterward.
            report["child_cleanup"] = finish_owned_child(process)
        except Exception as error:
            report.update(status="failed", child_cleanup={"terminal": False}, child_cleanup_failure=type(error).__name__)
        if before is not None:
            try:
                after = host_witness(core, output, "after")
                report["host_before"], report["host_after"] = before, after
                require_host_preservation(before, after)
                report["host_preserved"] = True
            except Exception as error:
                report.update(status="failed", host_preserved=False, preservation_failure=type(error).__name__)
        if report.get("host_preserved") is not True:
            report["status"] = "failed"
        core.create_private(output / "mount-report.json", json.dumps(report, sort_keys=True, indent=2).encode() + b"\n")
    return 0 if report["status"] == "passed" else 1


def create_runner(core, args, prerequisites):
    operator_path = Path(__file__)
    operator_bytes = core.private_read(operator_path, modes=(0o600, 0o644, 0o755))

    class BoundScanMountRunner(core.Runner):
        def write_environment(self, passwords):
            super().write_environment(passwords)
            self.report.update(verification=VERIFICATION, prerequisites=prerequisites, operator_sha256=digest(operator_bytes))
            worker_path, request_path = self.output / "mount-worker.py", self.output / "mount-request.json"
            core.create_private(worker_path, operator_bytes)
            request = {"verification": VERIFICATION, "source": str(SOURCE), "manifest": MANIFEST,
                       "output": str(self.output), "output_identity": self.output_identity, "run_id": self.run,
                       "unit": self.unit, "tag": self.tag, "pairs": self.pairs,
                       "host_namespace": args.host_namespace, "worker_sha256": digest(operator_bytes),
                       "environment_sha256": digest(core.private_read(self.output / "run.env"))}
            encoded = json.dumps(request, sort_keys=True, indent=2).encode() + b"\n"
            core.create_private(request_path, encoded)
            body = "#!/bin/bash\nset -euo pipefail\numask 077\nexec " + shlex.join([
                "/usr/bin/python3", "-I", "-B", str(worker_path), "--worker", str(request_path)]) + "\n"
            run_script = self.output / "run.sh"
            core.replace_private(run_script, core.private_read(run_script), body.encode())
            self.mount_request_sha256 = digest(encoded)
            self.mount_script_sha256 = digest(body.encode())
            # The inherited prepare() saves this new receipt after this method
            # returns. A full or unrelated targeted run never admits a worker.
            self.receipt["bound_scan_mount"] = {"verification": VERIFICATION, "request_sha256": self.mount_request_sha256,
                                               "worker_sha256": digest(operator_bytes), "host_namespace": args.host_namespace,
                                               "run_script_sha256": self.mount_script_sha256}

        def inspect_results(self):
            require(core.directory(self.output) == self.output_identity and core.private_read(core.RECEIPT) == self.receipt_bytes,
                    "The owned output or receipt changed before mount result inspection.")
            require(digest(core.private_read(self.output / "mount-request.json")) == self.mount_request_sha256 and
                    digest(core.private_read(self.output / "run.sh")) == self.mount_script_sha256 and
                    digest(core.private_read(self.output / "mount-worker.py")) == digest(operator_bytes),
                    "The fixed worker command or request changed during execution.")
            request = core.decode(core.private_read(self.output / "mount-request.json"))
            require(digest(core.private_read(self.output / "run.env")) == request.get("environment_sha256"),
                    "The exact newly owned worker environment changed during execution.")
            raw = core.private_read(self.output / "mount-report.json")
            observed = core.decode(raw)
            require(isinstance(observed, dict) and observed.get("status") == "passed" and observed.get("verification") == VERIFICATION and
                    observed.get("source") == str(SOURCE) and observed.get("manifest") == MANIFEST and observed.get("run_id") == self.run and
                    type(observed.get("schema")) is int and observed.get("schema") == 28 and observed.get("system_reboot_tested") is False and
                    observed.get("loop_scope") == "observed-only-no-loop-allocation" and observed.get("host_preserved") is True and
                    isinstance(observed.get("child_cleanup"), dict) and set(observed["child_cleanup"]) == {"terminal", "killed"} and
                    observed["child_cleanup"].get("terminal") is True and observed["child_cleanup"].get("killed") is False and
                    observed.get("source_unchanged") is True and observed.get("binary_unchanged") is True and observed.get("fixture_directory_empty") is True,
                    "The actual mount verification did not complete its exact owned scope.")
            checked = inspect_helper_output(core.private_read(self.output / "helper.stdout"), core.private_read(self.output / "helper.stderr"), 0)
            require(observed.get("helper") == checked, "The helper report no longer matches its real output.")
            require_host_preservation(observed.get("host_before"), observed.get("host_after"))
            for suffix in ("before", "after"):
                require(digest(core.private_read(self.output / ("host-mountinfo-" + suffix))) == observed["host_" + suffix]["mountinfo_sha256"] and
                        digest(core.private_read(self.output / ("host-loops-" + suffix + ".json"))) == observed["host_" + suffix]["loops_sha256"],
                        "A saved host preservation witness changed.")
            terminal_host = host_witness(core, self.output, "terminal")
            require_host_preservation(observed["host_before"], terminal_host)
            private = observed.get("private_namespace", {})
            require(isinstance(private, dict) and set(private) == {"namespace", "pid", "start_ticks", "cgroup", "subprocess_terminal"} and
                    type(private.get("pid")) is int and private["pid"] > 0 and isinstance(private.get("start_ticks"), str) and
                    re.fullmatch(r"[0-9]+", private["start_ticks"]) is not None and
                    private.get("subprocess_terminal") is True and valid_namespace(private.get("namespace")) and
                    private["namespace"] != args.host_namespace and private.get("cgroup") == "/system.slice/" + self.unit,
                    "The result lacks a distinct terminal private namespace witness.")
            require(artifact(core, self.output / "tmp/library.test") == observed.get("binary") and
                    not list((self.output / "tmp/mount-fixtures").iterdir()), "The retained binary or cleaned fixture changed.")
            state = core.unit_state(self.unit)
            core.require_unit_terminal(state, self.unit, self.tag)
            invocation = observed.get("invocation_id", "")
            require(isinstance(invocation, str) and re.fullmatch(r"[0-9a-f]{32}", invocation),
                    "The worker lacks an exact systemd invocation witness.")
            if state.get("LoadState") != "not-found":
                current_invocation = core.command(["/usr/bin/systemctl", "show", self.unit, "--property=InvocationID", "--value"]).strip()
                require(current_invocation == invocation, "The completed unit is not the worker's exact systemd invocation.")
            self.report.update(mount_report_sha256=digest(raw), mount=observed,
                               mount_unit_terminal={"properties": state, "invocation_id": invocation, "cgroup_empty": True},
                               terminal_host=terminal_host,
                               tests={"top_level_passes": 1, "passed": [TEST_NAME], "failures": 0, "skips": 0},
                               binary=observed["binary"], log_sha256=digest(core.private_read(self.output / "go.log")))

    # The inherited lifecycle owns every cleanup decision and database identity.
    for method in ("prepare", "execute", "cleanup", "stop_unit", "remove_pair", "create_pair", "check_cluster"):
        require(getattr(BoundScanMountRunner, method) is getattr(core.Runner, method), "A protected paired-runner method was overridden.")
    return BoundScanMountRunner(args)


def main(arguments=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--full-report", type=Path)
    parser.add_argument("--full-report-sha256")
    parser.add_argument("--host-namespace")
    parser.add_argument("--worker", type=Path, help=argparse.SUPPRESS)
    args = parser.parse_args(arguments)
    sys.dont_write_bytecode = True
    os.umask(0o077)
    if args.worker is not None:
        require(args.full_report is None and args.full_report_sha256 is None and args.host_namespace is None,
                "A worker cannot accept controller arguments.")
        return worker(args.worker)
    require(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION"),
            "Run this one-shot controller only through authorized root SSH on test-env.")
    require(args.full_report is not None and args.full_report_sha256 is not None and args.host_namespace is not None,
            "The completed full report, its digest, and explicit host witness are required.")
    core = load_core()
    prerequisites = read_prerequisites(core, args)
    args.source, args.manifest_sha256, args.schema = SOURCE, MANIFEST, 28
    args.mode, args.package, args.run = "targeted", ["./internal/library"], "^" + TEST_NAME + "$"
    def interrupted(_number, _frame):
        raise core.Failure("The mount verification controller was interrupted; owned evidence will be retained.")
    for number in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
        signal.signal(number, interrupted)
    return create_runner(core, args, prerequisites).run_all()


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Failure as error:
        raise SystemExit(str(error)) from None
