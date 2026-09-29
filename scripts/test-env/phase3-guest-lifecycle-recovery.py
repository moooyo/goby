#!/usr/bin/env python3
"""Run three lifecycle cases against one explicitly owned, dedicated guest.

Use the adjacent CLI driver's private JSON contract. The host keeps an exact
media mirror at the same absolute paths used inside the guest. The fixed
external control executable owns only this VM and accepts start, stop, status,
reboot, reset, mount-media, arm-late-mount, and disarm-late-mount.
No SSH, QMP discovery, provisioning, or host power operation is implemented here.
"""

import argparse
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import time


spec = importlib.util.spec_from_file_location(
    "phase3_cli_recovery", Path(__file__).with_name("phase3-cli-supervisor-recovery.py"))
cli = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cli)
need, emit, Failure = cli.need, cli.emit, cli.Failure


class GuestDriver(cli.Driver):
    def __init__(self, config, checkpoint):
        super().__init__(config)
        self.checkpoint = Path(checkpoint)
        need(self.checkpoint.is_absolute(), "checkpoint_path_must_be_absolute")
        self.receipts = self.checkpoint.with_name(self.checkpoint.name + "." + self.run_id + ".jsonl")
        self.bindings = None
        self.machine_id = None
        self.media_uuid = None
        self.late_armed = False

    def raw_request(self, method, path, body=None, admin=False, public=False, timeout=10):
        # TCG guests keep the same functional checks with a wider HTTP hang
        # guard. This does not delay application work to manufacture overlap.
        return super().raw_request(method, path, body, admin, public, max(timeout, 30))

    def control(self, operation, *arguments):
        need(not arguments and operation in {"start", "stop", "status", "reboot", "reset",
                                            "mount-media", "arm-late-mount", "disarm-late-mount"},
             "unsupported_guest_control_operation")
        try:
            result = subprocess.run([self.config["control_script"], operation], stdin=subprocess.DEVNULL,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    timeout=self.timeout, check=False)
        except subprocess.TimeoutExpired:
            raise Failure("guest_control_timeout_" + operation) from None
        need(result.returncode == 0, "guest_control_failed_" + operation)
        if operation != "status":
            emit("guest_control", phase=self.phase, operation=operation)
            return None
        need(len(result.stdout) <= 65536, "guest_status_too_large")
        value = json.loads(result.stdout)
        need(type(value) is dict and type(value.get("vm_running")) is bool
             and type(value.get("guest_online")) is bool
             and type(value.get("qemu_pid")) is int and value["qemu_pid"] >= 0,
             "invalid_guest_status")
        if value["guest_online"]:
            required = {"active_state", "sub_state", "main_pid", "n_restarts", "exec_main_pid",
                        "exec_main_code", "exec_main_status", "invocation_id", "cgroup_pids", "pg_running",
                        "boot_id", "machine_id", "media_mounted", "media_filesystem_uuid"}
            need(value["vm_running"] and value["qemu_pid"] > 0 and required <= value.keys(),
                 "incomplete_online_guest_status")
            need(type(value["boot_id"]) is str and value["boot_id"]
                 and type(value["machine_id"]) is str and value["machine_id"]
                 and type(value["main_pid"]) is int and value["main_pid"] >= 0
                 and type(value["pg_running"]) is bool and type(value["media_mounted"]) is bool
                 and type(value["cgroup_pids"]) is list
                 and all(type(pid) is int and pid > 0 for pid in value["cgroup_pids"]),
                 "invalid_online_guest_identity")
        return value

    def ready(self, previous=None, mounted=True):
        def check():
            state = self.control("status")
            if not state["guest_online"]:
                return None
            if self.machine_id is not None:
                need(state["machine_id"] == self.machine_id, "guest_machine_identity_changed")
            if previous is not None and state["boot_id"] == previous["boot_id"]:
                return None
            if state["active_state"] != "active" or state["main_pid"] <= 0 or not state["pg_running"]:
                return None
            need(state["media_mounted"] is mounted, "guest_media_boot_policy_was_not_observed")
            code, _ = self.raw_request("GET", "/readyz", timeout=3)
            return state if code == 200 else None
        state = self.wait("guest_boot_and_application_ready", check)
        if self.machine_id is None:
            self.machine_id = state["machine_id"]
        if mounted:
            need(type(state["media_filesystem_uuid"]) is str and state["media_filesystem_uuid"],
                 "mounted_media_has_no_filesystem_identity")
            if self.media_uuid is None:
                self.media_uuid = state["media_filesystem_uuid"]
            need(state["media_filesystem_uuid"] == self.media_uuid, "guest_media_filesystem_was_replaced")
        emit("guest_ready", phase=self.phase, boot_id=state["boot_id"], app_pid=state["main_pid"],
             qemu_pid=state["qemu_pid"], pg_running=True, media_mounted=mounted)
        return state

    def binding_snapshot(self, verified):
        prefix = "/admin/v1/libraries/" + self.library_id + "/roots"
        roots = self.request("GET", prefix, admin=True)
        need(roots["TotalRecordCount"] == 1 and len(roots["Items"]) == 1,
             "guest_fixture_requires_one_registered_media_root")
        result = {}
        for root in roots["Items"]:
            binding = self.request("GET", prefix + "/" + root["Id"] + "/binding", admin=True)["Binding"]
            if verified:
                need(binding["Status"] == "verified"
                     and binding["ApprovedFingerprint"] == binding["ObservedFingerprint"],
                     "original_media_binding_is_not_verified")
            else:
                need(binding["Status"] in {"unavailable", "mismatch"},
                     "absent_media_was_treated_as_verified_storage")
            result[root["Id"]] = {"Revision": binding["Revision"],
                                  "ApprovedFingerprint": binding["ApprovedFingerprint"]}
        return result

    def checkpoint_before(self, state, scan_id=None, run_id=None):
        snapshot = {"catalog": self.baseline, "userdata": self.user_data, "settings": self.settings}
        encoded = json.dumps(snapshot, sort_keys=True, separators=(",", ":")).encode()
        if self.checkpoint.exists():
            metadata = self.checkpoint.lstat()
            need(stat.S_ISREG(metadata.st_mode) and stat.S_IMODE(metadata.st_mode) == 0o600
                 and metadata.st_size <= 1 << 20, "existing_checkpoint_is_not_private")
            need(json.loads(self.checkpoint.read_bytes()) == snapshot, "existing_checkpoint_differs")
        else:
            descriptor = os.open(self.checkpoint, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(descriptor, "wb") as output:
                output.write(encoded)
                output.flush()
                os.fsync(output.fileno())
        record = {"phase": self.phase, "boot_id": state["boot_id"], "machine_id": state["machine_id"],
                  "app_pid": state["main_pid"], "media_filesystem_uuid": self.media_uuid,
                  "bindings": self.bindings, "scan_id": scan_id, "task_run_id": run_id,
                  "recorded_unix_ns": time.time_ns()}
        descriptor = os.open(self.receipts, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        with os.fdopen(descriptor, "ab") as output:
            output.write(json.dumps(record, sort_keys=True).encode() + b"\n")
            output.flush()
            os.fsync(output.fileno())
        directory = os.open(self.checkpoint.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        emit("guest_checkpoint_persisted", phase=self.phase, boot_id=state["boot_id"],
             scan_id=scan_id, task_run_id=run_id)

    def assert_reboot_work_terminal(self, scan_id, run_id, abrupt):
        if abrupt:
            self.assert_interrupted(scan_id, run_id)
        else:
            scan, task = self.scan_job(scan_id), self.preview_run(run_id)
            terminal = {"completed", "cancelled", "interrupted"}
            need(scan["Status"] in terminal and task["Run"]["State"] in terminal
                 and all(child["State"] in terminal for child in task["Children"]["Items"]),
                 "normal_reboot_left_unfinished_or_failed_work")
            if scan["Status"] == "completed":
                need(not scan["Error"] and scan["Scanned"] == 20, "normal_reboot_scan_completed_incorrectly")
        scan, task = self.scan_job(scan_id), self.preview_run(run_id)
        emit("guest_old_work_terminal", phase=self.phase, scan_status=scan["Status"],
             task_status=task["Run"]["State"], abrupt_reset=abrupt)

    def execute(self):
        self.control("start")
        initial = self.ready()
        self.initialize()
        self.bindings = self.binding_snapshot(verified=True)
        self.checkpoint_before(initial)

        for operation, label in (("reboot", "normal_guest_reboot"), ("reset", "forced_guest_reset")):
            self.phase = label
            scan_id, run_id, before = self.active_work(operation)
            self.checkpoint_before(before, scan_id, run_id)
            self.control(operation)
            self.ready(previous=before)
            self.assert_durable()
            need(self.binding_snapshot(verified=True) == self.bindings, "guest_restart_changed_approved_binding")
            self.assert_reboot_work_terminal(scan_id, run_id, abrupt=operation == "reset")
            self.assert_working(operation)
            self.completed.append(label)

        self.phase = "late_media_mount"
        before = self.control("status")
        self.assert_durable()
        self.checkpoint_before(before)
        self.late_armed = True
        self.control("arm-late-mount")
        self.control("reboot")
        missing = self.ready(previous=before, mounted=False)
        self.assert_durable()
        need(self.binding_snapshot(verified=False) == self.bindings, "missing_mount_changed_persisted_approval")
        media_status, _ = self.raw_request("GET", "/emby/Videos/" + self.sample_id + "/stream.mp4?Static=true",
                                          public=True, timeout=10)
        need(media_status == 503, "absent_media_did_not_return_storage_unavailable")
        scan_id = self.start_scan()

        def failed_scan():
            job = self.scan_job(scan_id)
            if job["Status"] in {"pending", "running"}:
                return False
            need(job["Status"] == "failed" and bool(job["Error"]), "absent_media_scan_was_not_truthfully_failed")
            return True
        self.wait("late_mount_failed_scan", failed_scan)
        self.assert_durable()
        emit("late_mount_absence_verified", boot_id=missing["boot_id"], app_pid=missing["main_pid"],
             catalog_ids_retained=20, media_http_status=media_status, scan_failed=True, approved_binding_unchanged=True)
        self.control("mount-media")
        mounted = self.ready()
        need(mounted["boot_id"] == missing["boot_id"] and mounted["main_pid"] == missing["main_pid"]
             and mounted["invocation_id"] == missing["invocation_id"],
             "late_mount_recovery_restarted_the_application_or_guest")
        need(self.binding_snapshot(verified=True) == self.bindings, "late_mount_required_or_invented_a_rebind")
        self.assert_working("late-mount")
        self.control("disarm-late-mount")
        self.late_armed = False
        self.completed.append(self.phase)
        emit("late_mount_recovered", same_application=True, original_filesystem=True, rebind_required=False)

    def close(self):
        self.phase = "closure"
        restore_failed = False
        if self.late_armed:
            try:
                state = self.control("status")
                if state["guest_online"]:
                    self.control("disarm-late-mount")
                    self.late_armed = False
                else:
                    restore_failed = True
            except Exception:
                restore_failed = True
        self.control("stop")

        def stopped():
            state = self.control("status")
            return not state["vm_running"] and not state["guest_online"] and state["qemu_pid"] == 0
        self.wait("dedicated_guest_shutdown", stopped)
        code, _ = self.raw_request("GET", "/readyz", timeout=2)
        need(code == 0, "guest_http_forward_remained_live_after_vm_shutdown")
        emit("guest_closed", qemu_pid=0, vm_running=False, http_forward_closed=True,
             late_mount_policy_restored=not restore_failed)
        need(not restore_failed, "late_mount_policy_requires_restoration_on_next_guest_start")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="Absolute private JSON using the CLI driver contract")
    parser.add_argument("--checkpoint", required=True, help="Absolute host-side private baseline JSON")
    args = parser.parse_args()
    driver, failure, failed_phase = None, None, None

    def interrupted(_signum, _frame):
        raise Failure("guest_driver_interrupted")

    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    try:
        driver = GuestDriver(cli.load_config(args.config), args.checkpoint)
        driver.execute()
    except Failure as error:
        failure = str(error)
        failed_phase = None if driver is None else driver.phase
    except Exception as error:
        failure = "unexpected_" + type(error).__name__
        failed_phase = None if driver is None else driver.phase
    finally:
        if driver is not None:
            try:
                driver.close()
            except Failure as error:
                emit("guest_closure_failed", reason=str(error))
                failure = failure or "guest_closure_failed"
            except Exception as error:
                emit("guest_closure_failed", reason="unexpected_" + type(error).__name__)
                failure = failure or "guest_closure_failed"
    emit("guest_result", passed=failure is None, failure=failure, failed_phase=failed_phase,
         completed=[] if driver is None else driver.completed,
         scope="dedicated_guest_normal_reboot_QMP_reset_and_late_original_media_mount; no_shared_host_restart_or_physical_power_loss_claim")
    return 0 if failure is None else 1


if __name__ == "__main__":
    sys.exit(main())
