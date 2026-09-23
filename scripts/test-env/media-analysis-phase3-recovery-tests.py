#!/usr/bin/env python3
"""Source-delivered pure contract tests; run only on the authorized test host.

These tests use immutable fixtures and mocks. They do not inject faults, run
SSH, start services, mount devices, or establish real recovery acceptance.
"""

from __future__ import annotations

import copy
import importlib.util
from pathlib import Path
import unittest
from unittest.mock import patch


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


controller = load("phase3_recovery", "media-analysis-phase3-recovery.py")
guest = load("phase3_recovery_guest", "media-analysis-phase3-recovery-guest.py")
SHA = "a" * 64
OTHER_SHA = "b" * 64


def scenario(fault="blocked_read"):
    return {"scenario_id": "blocked-read-01", "fault": fault, "volume_id": "media", "replacement_volume_id": None,
            "relative_path": "nested", "late_mount": False, "restart_goby_after": False, "require_interrupted_jobs": False}


def stable_state():
    return {"catalog_count": 10000, **{name: SHA for name in controller.STATE_HASHES}}


def inventory_manifest(disk_bytes=73 << 30):
    root = "/var/lib/goby-phase3/inventory-case"
    def reference(name):
        return {"path": "/external/" + name, "sha256": SHA}
    volumes = {}
    for index, (name, purpose, writable) in enumerate((("media", "media", False),
                                                       ("derivatives", "derivatives", True))):
        volumes[name] = {"mountpoint": root + "/mounts/" + name,
                         "backing_file": root + "/backing/" + name,
                         "loop_device": "/dev/loop" + str(index), "mapper_name": "goby-phase3-" + name,
                         "dm_uuid": "GOBY-PHASE3-owner1-" + name,
                         "filesystem_uuid": "11111111-2222-3333-4444-555555555555",
                         "major_minor": "253:" + str(index), "size_bytes": 32 << 20,
                         "writable": writable, "purpose": purpose}
    return {"schema_version": 1, "run_id": "run1", "source_revision": "c" * 40,
            "profile_id": "profile1", "tier": 10000, "owner_id": "owner1",
            "controller": {"machine_id": "1" * 32, "artifacts_root": "/external", "marker": reference("owner.json")},
            "guest": {"vmid": 106, "name": "goby-phase3-inventory", "machine_id": "2" * 32,
                      "smbios_uuid": "11111111-2222-3333-4444-555555555555", "owner_marker_sha256": SHA,
                      "owned_root": root, "disks": {"scsi0": {"volume": "local-lvm:vm-106-disk-0",
                          "uuid": "actual-owned-disk-uuid", "size_bytes": disk_bytes}}, "goby_uid": 1001,
                      "goby_cgroup": "/system.slice/goby-phase3-app.service",
                      "postgres_cgroup": "/system.slice/goby-phase3-postgres.service"},
            "adapters": {role: {**reference(role + ".py"), "context": reference(role + ".json")}
                         for role in ("executor", "observer", "workload", "hypervisor")},
            "workload_manifest": reference("workload.json"), "volumes": volumes, "healthy_roots": ["healthy-root"],
            "budgets": {"rpc_seconds": 120, "recovery_seconds": 900, "fault_seconds": 120,
                        "poll_seconds": 0.25, "max_output_bytes": 8 << 20,
                        "healthy_latency_ms": 1500, "cleanup_seconds": 120},
            "scenarios": [scenario()],
            "state_validator": {"source": reference("state.py"), "binding_sha256": SHA,
                "scope": {"user_ids": ["viewer"], "item_ids": ["item"], "library_ids": ["library"],
                          "root_ids": ["healthy-root", "fault-root"], "fault_root_ids": ["fault-root"]}}}


def inventory_observations(manifest):
    configured = manifest["guest"]
    observed = {key: copy.deepcopy(configured[key]) for key in
                ("vmid", "name", "machine_id", "smbios_uuid", "owner_marker_sha256", "disks")}
    observed.update(owner_id=manifest["owner_id"], boot_id="aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", btime=1,
                    volumes=copy.deepcopy(manifest["volumes"]), observed_unix_ns=10)
    for index, role in enumerate(("goby", "postgres")):
        observed[role] = {"pid": 100 + index, "start_ticks": 200 + index,
                          "cgroup": configured[role + "_cgroup"], "executable_sha256": SHA}
    observed["volume_observations"] = {name: {
        "present": True, "mounted": True, "dm_uuid": volume["dm_uuid"], "major_minor": volume["major_minor"],
        "loop_backing_file": volume["backing_file"], "size_bytes": volume["size_bytes"]}
        for name, volume in manifest["volumes"].items()}
    hypervisor = {"owner_id": manifest["owner_id"], "vmid": configured["vmid"],
                  "smbios_uuid": configured["smbios_uuid"], "status": "running", "qemu_pid": 500,
                  "qemu_start_ticks": 600, "config_sha256": SHA, "disks": copy.deepcopy(configured["disks"]),
                  "observed_unix_ns": 10, "observer_machine_id": manifest["controller"]["machine_id"]}
    return observed, hypervisor


class MemoryJournal:
    def __init__(self):
        self.events = []

    def write(self, kind, data):
        self.events.append((kind, data))
        return {"path": "/external/" + str(len(self.events)) + ".json", "sha256": SHA}


class ContractTests(unittest.TestCase):
    def test_guest_disk_inventory_describes_authorized_73_gib_and_finite_128_gib_ceiling(self):
        for size in (73 << 30, 128 << 30):
            with self.subTest(size=size):
                manifest = inventory_manifest(size)
                self.assertIs(controller.load_manifest(manifest), manifest)
        with self.assertRaisesRegex(controller.Invalid, "invalid_integer"):
            controller.load_manifest(inventory_manifest((128 << 30) + 1))

    def test_inventory_ceiling_does_not_allow_changed_owner_volume_uuid_or_actual_size(self):
        manifest = controller.load_manifest(inventory_manifest())
        observed, hypervisor = inventory_observations(manifest)
        controller.validate_identity(observed, manifest)
        controller.validate_hypervisor(hypervisor, manifest)
        changes = (("owner_id", "another-owner"), ("volume", "local-lvm:vm-106-disk-9"),
                   ("uuid", "another-disk-uuid"), ("size_bytes", (73 << 30) + 512))
        for name, value in changes:
            for original, validate in ((observed, controller.validate_identity),
                                       (hypervisor, controller.validate_hypervisor)):
                with self.subTest(field=name, validator=validate.__name__):
                    changed = copy.deepcopy(original)
                    if name == "owner_id":
                        changed[name] = value
                    else:
                        changed["disks"]["scsi0"][name] = value
                    with self.assertRaises(controller.Invalid):
                        validate(changed, manifest)

    def test_larger_guest_inventory_keeps_original_two_gib_fault_volume_limit(self):
        manifest = inventory_manifest()
        manifest["volumes"]["media"]["size_bytes"] = (2 << 30) + 512
        with self.assertRaisesRegex(controller.Invalid, "invalid_integer"):
            controller.load_manifest(manifest)

    def test_duplicate_json_keys_and_nonfinite_values_are_rejected(self):
        for raw in (b'{"vmid":106,"vmid":101}', b'{"value":NaN}', b'{"value":Infinity}'):
            with self.subTest(raw=raw), self.assertRaises(controller.Invalid):
                controller.decode(raw)

    def test_missing_or_changed_catalog_members_fail_even_when_count_is_unchanged(self):
        before, after = stable_state(), stable_state()
        after["catalog_identity_sha256"] = OTHER_SHA
        with self.assertRaisesRegex(controller.Invalid, "acknowledged_state_changed"):
            controller.assert_preserved(before, after)

    def test_lost_progress_is_not_hidden_by_a_healthy_catalog_digest(self):
        before, after = stable_state(), stable_state()
        after["playback_progress_sha256"] = OTHER_SHA
        with self.assertRaises(controller.Invalid):
            controller.assert_preserved(before, after)

    def test_guest_rejects_path_escape_inside_an_authorized_owned_root(self):
        instance = guest.Guest.__new__(guest.Guest)
        instance.binding = {"volumes": {"media": {}}}
        for relative in ("/var/lib/goby-phase3/run/postgres", "../postgres", "nested/../../postgres"):
            with self.subTest(relative=relative), self.assertRaises(guest.FaultError):
                instance.scenario({**scenario("permission_failure"), "relative_path": relative})

    def test_nested_mount_requires_a_real_descendant_and_distinct_replacement(self):
        instance = guest.Guest.__new__(guest.Guest)
        instance.binding = {"volumes": {"media": {}, "replacement": {}}}
        value = {**scenario("changed_nested_mount"), "relative_path": ".", "replacement_volume_id": "replacement"}
        with self.assertRaises(guest.FaultError):
            instance.scenario(value)
        with self.assertRaises(guest.FaultError):
            instance.scenario({**value, "relative_path": "nested", "replacement_volume_id": "media"})

    def test_release_cannot_be_replayed_with_a_different_binding_or_fault(self):
        instance = guest.Guest.__new__(guest.Guest)
        instance.binding = {"run_id": "run1", "owner_id": "owner1", "guest": {"smbios_uuid": "uuid"}}
        instance.binding_sha256 = SHA
        instance.release_ref = {"path": "/private/release.json", "sha256": SHA}
        selected = scenario()
        release = {"schema_version": 1, "run_id": "run1", "owner_id": "owner1", "vmid": 106,
                   "smbios_uuid": "uuid", "source_revision": "c" * 40, "binding_sha256": SHA,
                   "operations": ["inject"], "scenarios": {selected["scenario_id"]: guest.hashlib.sha256(guest.canonical(selected)).hexdigest()},
                   "expires_unix_ns": (1 << 63) - 1}
        request = {"source_revision": "c" * 40, "binding_sha256": SHA, "op": "inject", "scenario": selected}
        with patch.object(guest, "read_pinned", return_value=guest.canonical(release)):
            instance.release(request)
            with self.assertRaises(guest.FaultError):
                instance.release({**request, "binding_sha256": OTHER_SHA})
            with self.assertRaises(guest.FaultError):
                instance.release({**request, "scenario": {**selected, "fault": "permission_failure"}})

    def test_http_timeout_without_a_real_blocked_task_is_not_accepted(self):
        value = {"fault": "blocked_read", "mechanism": "dm_suspend", "observed_unix_ns": 9,
                 "facts": {"injected_unix_ns": 1, "suspended": True, "dm_uuid": "owned",
                           "caller_timed_out": True, "task_alive_after_timeout": True},
                 "state": stable_state(), "healthy_probes": [], "dispatches": 1}
        manifest = {"volumes": {"media": {"dm_uuid": "owned"}}, "guest": {"goby_cgroup": "/system.slice/goby-phase3-app.service"}}
        with patch.object(controller, "healthy_probes"), self.assertRaises(controller.Invalid):
            controller.validate_fault_observation(value, scenario(), manifest, {"acknowledged_state": stable_state()})

    def test_bounded_metadata_503_requires_a_real_live_blocked_get_and_not_post_admission(self):
        group = "/system.slice/goby-phase3-app.service"
        witness = {"pid": 200, "start_ticks": 100, "tid": 201, "task_start_ticks": 101, "state": "D",
                   "syscall": "262 0x4", "wchan": "io_schedule", "operation": "metadata", "cgroup": group,
                   "request_sha256": SHA, "first_seen_unix_ns": 1, "last_seen_unix_ns": 2_000_000_000}
        facts = {"injected_unix_ns": 1, "suspended": True, "dm_uuid": "owned", "blocked_task": witness,
                 "caller_timed_out": False, "bounded_http_return": True, "http_method": "GET", "http_status": 503,
                 "task_alive_after_caller_return": True}
        value = {"fault": "blocked_metadata", "mechanism": "dm_suspend", "observed_unix_ns": 3_000_000_000,
                 "facts": facts, "state": stable_state(), "healthy_probes": [], "dispatches": 1}
        manifest = {"volumes": {"media": {"dm_uuid": "owned"}}, "guest": {"goby_cgroup": group}}
        with patch.object(controller, "healthy_probes"):
            controller.validate_fault_observation(value, scenario("blocked_metadata"), manifest, {"acknowledged_state": stable_state()})
            for changed in ({"http_method": "POST", "http_status": 202}, {"task_alive_after_caller_return": False},
                            {"caller_timed_out": True}):
                with self.subTest(changed=changed), self.assertRaises(controller.Invalid):
                    controller.validate_fault_observation({**value, "facts": {**facts, **changed}}, scenario("blocked_metadata"),
                                                          manifest, {"acknowledged_state": stable_state()})

    def test_rebind_chain_cannot_change_root_or_restore_a_different_storage_document(self):
        original = {"storage_binding": {"identity": "original"}, "binding_revision": 3}
        replacement = {"storage_binding": {"identity": "replacement"}, "binding_revision": 4}
        restored = {"storage_binding": {"identity": "original"}, "binding_revision": 5}
        def proof(old, new, key="root1"):
            return {"native_ack": {"postimages": [{"table": "library_roots", "key": [key], "before_row": old, "row": new}]}}
        first, second = proof(original, replacement), proof(replacement, restored)
        controller.validate_rebind_chain(first, second)
        with self.assertRaises(controller.Invalid):
            controller.validate_rebind_chain(first, proof(replacement, restored, "root2"))
        with self.assertRaises(controller.Invalid):
            controller.validate_rebind_chain(first, proof(replacement, {**restored, "storage_binding": {"identity": "third"}}))

    def test_resume_cannot_bless_an_internally_consistent_but_wrong_position(self):
        controller.validate_resume_position(900, 900, 900, 900)
        with self.assertRaisesRegex(controller.Invalid, "resume_did_not_use_acknowledged_position"):
            controller.validate_resume_position(900, 123, 123, 900)
        with self.assertRaises(controller.Invalid):
            controller.validate_resume_position(123, 123, 123, 900)

    def test_repeated_native_settings_ack_cannot_replace_missing_durable_writes(self):
        names = ["metadata", "favorite", "progress", "settings"]
        high = [{"request_id": name} for name in names]
        controller.link_ack_references(high, [{"mutation_id": name} for name in names])
        repeated = [{"mutation_id": name} for name in ("progress", "settings", "settings", "settings")]
        with self.assertRaisesRegex(controller.Invalid, "acknowledgement_reference_bijection"):
            controller.link_ack_references(high, repeated)

    def test_replacement_cannot_be_accepted_from_a_changed_mount_id_alone(self):
        value = {"fault": "changed_root_mount", "mechanism": "owned_bind_mount", "observed_unix_ns": 9,
                 "facts": {"injected_unix_ns": 1, "before_mount_id": 20, "after_mount_id": 21, "replacement_visible": True,
                           "binding_status": "verified", "explicit_rebind_required": False, "automatic_rebind": True},
                 "state": stable_state(), "healthy_probes": [], "dispatches": 1}
        with patch.object(controller, "healthy_probes"), self.assertRaisesRegex(controller.Invalid, "replacement_not_fenced"):
            controller.validate_fault_observation(value, scenario("changed_root_mount"), {}, {"acknowledged_state": stable_state()})

    def test_analysis_overlap_accepts_serial_worker_slots_but_requires_both_features(self):
        manifest = {"run_id": "run1", "owner_id": "owner1", "profile_id": "profile1", "source_revision": "c" * 40, "tier": 10000}
        windows = [{"analysis_lane": lane, "lanes": ["scan", "search", "playback", lane], "start_unix_ns": index * 10 + 1,
                    "end_unix_ns": index * 10 + 9, "process_source_fd_sha256": SHA}
                   for index, lane in enumerate(("intro_analysis", "preview"))]
        value = {**manifest, "sha256": SHA, "active_lanes": sorted(controller.LANES), "observed_unix_ns": 30,
                 "overlap_windows": windows}
        controller.validate_checkpoint(value, manifest)
        with self.assertRaises(controller.Invalid):
            controller.validate_checkpoint({**value, "overlap_windows": [windows[0], windows[0]]}, manifest)

    def test_unknown_injection_disposition_never_retries_or_enters_recovery(self):
        journal = MemoryJournal()
        manifest = {"controller": {"artifacts_root": "/external"}, "workload_manifest": {"path": "/external/workload", "sha256": SHA}}
        runner = controller.Controller(manifest, SHA, journal)
        calls = []
        ack = {"snapshot": {"path": "/external/snapshot", "sha256": SHA, "bytes": 10, "fsynced": True}}
        def invoke(role, operation, selected, payload=None, mutation=False, timeout=None):
            calls.append(operation)
            if operation == "acknowledge":
                return {"status": "ok", "data": ack}
            if operation == "inject":
                self.assertTrue(any(kind == "durable-acknowledged-state" for kind, _data in journal.events))
                raise controller.ObservationTimeout("unknown")
            self.fail("Unexpected operation after ambiguous mutation: " + operation)
        runner.invoke = invoke
        runner.observe = lambda *_args, **_kwargs: {}
        with patch.object(controller, "validate_identity", side_effect=lambda value, _manifest: value), \
                patch.object(controller, "validate_hypervisor", side_effect=lambda value, _manifest: value), \
                patch.object(controller, "validate_ack"), \
                patch.object(runner, "native_baseline"), \
                patch.object(controller, "durable_artifact", return_value=ack["snapshot"]), \
                self.assertRaises(controller.ObservationTimeout):
            runner.scenario(scenario())
        self.assertEqual(calls, ["acknowledge", "inject"])

    def test_storage_and_lock_observation_mutations_are_not_polled_or_retried(self):
        for fault in ("blocked_metadata", "enospc", "postgres_lock_wait"):
            runner = controller.Controller({"budgets": {"fault_seconds": 17}}, SHA, MemoryJournal())
            calls = []
            def invoke(role, operation, selected, payload, mutation=False, timeout=None):
                calls.append((role, operation, selected["fault"], mutation, timeout))
                raise controller.ObservationTimeout("unknown")
            runner.invoke = invoke
            runner.observe = lambda *_args, **_kwargs: self.fail("A mutating observation was polled")
            with self.subTest(fault=fault), self.assertRaises(controller.ObservationTimeout):
                runner.fault_observation(scenario(fault), {})
            self.assertEqual(calls, [("observer", "fault_observation", fault, True, 17)])

    def test_restart_fault_observation_keeps_read_only_polling(self):
        runner = controller.Controller({"budgets": {"fault_seconds": 17}}, SHA, MemoryJournal())
        runner.invoke = lambda *_args, **_kwargs: self.fail("A read-only restart observation was dispatched as mutation")
        with patch.object(runner, "observe", return_value={"observed": True}) as observe:
            self.assertEqual(runner.fault_observation(scenario("guest_reboot"), {}), {"observed": True})
        observe.assert_called_once_with("fault_observation", scenario("guest_reboot"), {}, timeout=17)

    def test_failed_external_durability_prevents_any_fault_dispatch(self):
        journal = MemoryJournal()
        runner = controller.Controller({"controller": {"artifacts_root": "/external"}, "workload_manifest": {}}, SHA, journal)
        calls = []
        def invoke(_role, operation, *_args, **_kwargs):
            calls.append(operation)
            return {"status": "ok", "data": {"snapshot": {}}}
        runner.invoke = invoke
        runner.observe = lambda *_args, **_kwargs: {}
        with patch.object(controller, "validate_identity", side_effect=lambda value, _manifest: value), \
                patch.object(controller, "validate_hypervisor", side_effect=lambda value, _manifest: value), \
                patch.object(controller, "validate_ack"), \
                patch.object(runner, "native_baseline"), \
                patch.object(controller, "durable_artifact", side_effect=OSError("fsync")), \
                self.assertRaises(OSError):
            runner.scenario(scenario())
        self.assertEqual(calls, ["acknowledge"])
        self.assertFalse(any(kind == "durable-acknowledged-state" for kind, _data in journal.events))


if __name__ == "__main__":
    unittest.main()
