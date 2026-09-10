#!/usr/bin/env python3
"""Test fresh-reference authority using only in-memory manifests and system data.

Run through authorized root SSH. No recorder is initialized, and no namespace,
service, transport, timer, capture directory, or credential file is touched.
The standard HTTP module remains unchanged by the imported fresh adapter.
"""

from __future__ import annotations

import contextlib
import copy
import hashlib
import http.client
import importlib.util
import io
import json
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

sys.dont_write_bytecode = True
if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
    print(json.dumps({"suite": "fresh-reference-key-device-hardening", "result": "blocked",
                      "reason": "Authorized root SSH and one fresh recorder source path are required"}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SOURCE_HASH = hashlib.sha256(SOURCE.read_bytes()).hexdigest()
TEST_HASH = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
STANDARD_HTTP_CONNECTION = http.client.HTTPConnection
SPEC = importlib.util.spec_from_file_location("fresh_reference_key_devices_under_test", SOURCE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
STANDARD_TRANSPORT_UNCHANGED = http.client.HTTPConnection is STANDARD_HTTP_CONNECTION
PINNED_SOURCE_HASH = hashlib.sha256(Path(MODULE.key.__file__).read_bytes()).hexdigest()
FRESH_PID = 4000001
FRESH_NAMESPACE = "net:[410001]"
HOST_NAMESPACE = "net:[410000]"


def fresh_properties() -> dict:
    return {"Id": MODULE.FRESH_UNIT, "Description": MODULE.FRESH_DESCRIPTION,
            "PrivateNetwork": "yes", "PrivateTmp": "yes", "NoNewPrivileges": "yes", "ProtectSystem": "strict",
            "ProtectHome": "yes", "WorkingDirectory": str(MODULE.EVIDENCE / "runtime"),
            "ExecStart": str(MODULE.EVIDENCE / "runtime/launch.sh"), "ControlGroup": "/system.slice/" + MODULE.FRESH_UNIT,
            "FragmentPath": "/run/systemd/transient/" + MODULE.FRESH_UNIT, "DropInPaths": "",
            "ReadWritePaths": str(MODULE.FRESH_DATA) + " " + str(MODULE.EVIDENCE / "runtime"),
            "ReadOnlyPaths": "/dev/shm /dev/shm/goby-emby-reference /opt/goby-test/emby-reference-data",
            "MemoryMax": "536870912", "CPUQuotaPerSecUSec": "1.500000s", "TasksMax": "256", "User": "",
            "KillMode": "control-group", "TimeoutStopUSec": "25s", "LimitNOFILE": "65536", "UMask": "0077"}


def previous_failure_fixture() -> tuple[dict, dict[str, bytes]]:
    root = MODULE.PREVIOUS_FAILED_EVIDENCE
    operator = str(root / "runtime/prepare-original.py")
    failure = str(root / "private/prepare-failure.json")
    cleanup = str(root / "private/cleanup-attempt-1.json")
    files = {str(root / ".goby-managed"): b"goby-emby-key-devices-fresh-m5e-20260910-01-owned-v1\n",
             operator: b"# Synthetic retained first-attempt operator.\n",
             failure: json.dumps({"cleanup": {"stopped": True}, "preservationErrors": []}).encode(),
             cleanup: json.dumps({"dataRemoved": True, "oldPreservationVerified": True, "evidenceRetained": True}).encode()}
    hashes = {name: hashlib.sha256(content).hexdigest() for name, content in files.items()}
    return {"referenceRun": 1, "evidenceRoot": str(root), "unit": MODULE.PREVIOUS_FAILED_UNIT,
            "programData": str(MODULE.PREVIOUS_FAILED_DATA), "outcome": "initialization-failed-before-http",
            "setupRecordCount": 0, "failureRecord": failure, "cleanupRecord": cleanup,
            "operatorSource": operator, "operatorSha256": hashes[operator], "files": hashes,
            "fileCount": len(files), "totalBytes": sum(len(content) for content in files.values())}, files


def manifest_fixture() -> dict:
    old = {}
    for index, (unit, pid) in enumerate(MODULE.ORIGINAL_SERVICES.items()):
        old[unit] = {"unit": unit, "pid": pid, "uid": 0 if index == 0 else 995, "startTicks": str(1000 + index),
                     "networkNamespace": "net:[410002]" if index == 0 else HOST_NAMESPACE,
                     "cmdline": ["/synthetic/old-program-" + str(index)], "exe": "/synthetic/old-program-" + str(index),
                     "cgroup": "0::/system.slice/" + unit + "\n",
                     "serviceProperties": {**fresh_properties(), "Id": unit, "Description": "Synthetic Original Service",
                                           "PrivateNetwork": "yes" if index == 0 else "no", "User": "" if index == 0 else "995",
                                           "WorkingDirectory": "/synthetic/old-data-" + str(index),
                                           "ExecStart": "/synthetic/old-program-" + str(index),
                                           "ControlGroup": "/system.slice/" + unit, "FragmentPath": "/synthetic/" + unit}}
    server_id = "synthetic-owned-fresh-server-id"
    server = {"Id": "7", "ReportedDeviceId": server_id, "AppName": "Synthetic Fresh Server", "Name": "Fresh Server"}
    properties = fresh_properties()
    executable = "/dev/shm/goby-emby-reference/package/opt/emby-server/system/EmbyServer"
    return {"schemaVersion": 1, "referenceRun": MODULE.FRESH_RUN, "state": "READY", "unit": MODULE.FRESH_UNIT, "port": MODULE.FRESH_PORT,
            "previousFailureProvenance": None if MODULE.FRESH_RUN == 1 else previous_failure_fixture()[0],
            "programData": str(MODULE.FRESH_DATA), "serverId": server_id, "evidenceRoot": str(MODULE.EVIDENCE),
            "pid": FRESH_PID, "uid": 0, "startTicks": "9001", "networkNamespace": FRESH_NAMESPACE,
            "cmdline": [executable, "-programdata", str(MODULE.FRESH_DATA)], "exe": executable,
            "cgroup": "0::/system.slice/" + MODULE.FRESH_UNIT + "\n", "serviceProperties": properties,
            "bootstrapCredentialsRevoked": True, "bootstrapCredentialsInvalidity": {"admin": 401, "viewer": 401},
            "viewerEmptyPasswordLoginVerified": True, "oldPreservationVerified": True, "allFreshProgramDataOwned": True,
            "setupRawRoot": str(MODULE.EVIDENCE / "private/raw"), "setupExportRoot": str(MODULE.EVIDENCE / "export"),
            "setupPrefix": MODULE.SETUP_PREFIX, "setupRecordCount": 20,
            "adminCredentialsFile": str(MODULE.EVIDENCE / "private/admin-credentials.env"),
            "viewerCredentialsFile": str(MODULE.EVIDENCE / "private/viewer-credentials.env"),
            "bootstrapUsers": [{"Id": "synthetic-admin", "Name": "Synthetic Admin"},
                               {"Id": "synthetic-viewer", "Name": "Synthetic Viewer"}],
            "bootstrapDevices": [{"Id": "5", "ReportedDeviceId": "synthetic-bootstrap-admin-device", "AppName": "Bootstrap Admin"},
                                 {"Id": "6", "ReportedDeviceId": "synthetic-bootstrap-viewer-device", "AppName": "Bootstrap Viewer"}, server],
            "bootstrapServerInfo": {"alias": server_id, "status": 200, "body": server}, "oldServices": old}


@contextlib.contextmanager
def virtual_system(manifest: dict, *, actual_identities: dict | None = None, property_overrides: dict | None = None,
                   mount_overrides: dict | None = None, current_namespace: str = FRESH_NAMESPACE, free_bytes: int = 200 * 1024 * 1024,
                   prior_files: dict[str, bytes] | None = None, prior_data_exists: bool = False,
                   prior_unit_state: str = "not-found", prior_metadata: dict | None = None,
                   capture_entries: set[Path] | None = None, capture_evidence_entries: set[Path] | None = None,
                   capture_hashes: dict[Path, str] | None = None, capture_marker: str | None = None,
                   capture_metadata: dict | None = None):
    prior_tree = previous_failure_fixture()[1] if prior_files is None else prior_files
    prior_directories = {MODULE.PREVIOUS_FAILED_EVIDENCE}
    for name in prior_tree:
        prior_directories.update(parent for parent in Path(name).parents if
                                 parent == MODULE.PREVIOUS_FAILED_EVIDENCE or MODULE.PREVIOUS_FAILED_EVIDENCE in parent.parents)
    capture_directories = {MODULE.FIRST_CAPTURE_ROOT, MODULE.FIRST_CAPTURE_ROOT / "private",
                           MODULE.FIRST_CAPTURE_ROOT / "private/raw", MODULE.FIRST_CAPTURE_ROOT / "export",
                           MODULE.PREVIOUS_CAPTURE_FAILURE_ROOT}
    first_marker = MODULE.FIRST_CAPTURE_ROOT / ".goby-managed"
    captured_paths = (capture_directories - {MODULE.FIRST_CAPTURE_ROOT, MODULE.PREVIOUS_CAPTURE_FAILURE_ROOT}) | {first_marker}
    if capture_entries is not None:
        captured_paths = capture_entries
    captured_evidence = {MODULE.PREVIOUS_CAPTURE_SOURCE, MODULE.PREVIOUS_CAPTURE_CONSOLE} if capture_evidence_entries is None else capture_evidence_entries
    captured_hashes = {MODULE.PREVIOUS_CAPTURE_SOURCE: MODULE.PREVIOUS_CAPTURE_SOURCE_SHA256,
                       MODULE.PREVIOUS_CAPTURE_CONSOLE: MODULE.PREVIOUS_CAPTURE_CONSOLE_SHA256,
                       first_marker: hashlib.sha256((MODULE.FIRST_CAPTURE_MARKER + "\n").encode()).hexdigest()}
    captured_hashes.update(capture_hashes or {})
    identities = {manifest["pid"]: copy.deepcopy(manifest),
                  **{value["pid"]: copy.deepcopy(value) for value in manifest["oldServices"].values()}}
    if actual_identities:
        identities.update(copy.deepcopy(actual_identities))
    properties = {MODULE.FRESH_UNIT: {**manifest["serviceProperties"], "MainPID": str(manifest["pid"]), "ActiveState": "active"}}
    properties.update({unit: {**value["serviceProperties"], "MainPID": str(value["pid"]), "ActiveState": "active"}
                       for unit, value in manifest["oldServices"].items()})
    if MODULE.FRESH_RUN == 2:
        properties[MODULE.PREVIOUS_FAILED_UNIT] = {"LoadState": prior_unit_state}
    for unit, changes in (property_overrides or {}).items():
        properties[unit].update(changes)
    mounts = {"/": "ro", "/dev/shm": "ro", "/dev/shm/goby-emby-reference": "ro",
              "/opt/goby-test/emby-reference-data": "ro", str(MODULE.FRESH_DATA): "rw", str(MODULE.EVIDENCE / "runtime"): "rw"}
    mounts.update(mount_overrides or {})

    def proc(path: Path) -> tuple[dict, str]:
        parts = path.parts
        if len(parts) >= 3 and parts[1] == "proc" and parts[2].isdigit() and int(parts[2]) in identities:
            return identities[int(parts[2])], "/".join(parts[3:])
        raise AssertionError("Unexpected path outside the synthetic process table: " + str(path))

    def file_stat(path: Path, *_args: object, **_kwargs: object) -> SimpleNamespace:
        if str(path).startswith("/proc/"):
            row, _suffix = proc(path)
            return SimpleNamespace(st_uid=row["uid"], st_mode=stat.S_IFDIR | 0o700, st_size=1024, st_nlink=1)
        directory = path in {MODULE.EVIDENCE, MODULE.FRESH_DATA, MODULE.base.ROOT.parent} or path in prior_directories or path in capture_directories
        values = {"st_uid": 0, "st_mode": (stat.S_IFDIR | 0o700) if directory else (stat.S_IFREG | 0o600),
                  "st_size": len(prior_tree[str(path)]) if str(path) in prior_tree else 1024, "st_nlink": 1}
        values.update((prior_metadata or {}).get(str(path), {}))
        values.update((capture_metadata or {}).get(str(path), {}))
        return SimpleNamespace(**values)

    def read_text(path: Path, *_args: object, **_kwargs: object) -> str:
        if path == MODULE.MANIFEST:
            return json.dumps(manifest)
        if str(path) in prior_tree:
            return prior_tree[str(path)].decode()
        if path == first_marker:
            return (MODULE.FIRST_CAPTURE_MARKER if capture_marker is None else capture_marker) + "\n"
        if path.name == ".goby-managed":
            return MODULE.FRESH_MARKER
        row, suffix = proc(path)
        if suffix == "stat":
            return str(row["pid"]) + " (synthetic worker) " + " ".join(["S", *(["0"] * 18), row["startTicks"], "0"])
        if suffix == "cgroup":
            return row["cgroup"]
        if suffix == "mountinfo":
            return "\n".join(str(index + 1) + " 0 0:1 / " + point + " " + mode + " - tmpfs tmpfs " + mode
                              for index, (point, mode) in enumerate(mounts.items()))
        raise AssertionError("Unexpected synthetic text read: " + str(path))

    def read_bytes(path: Path, *_args: object, **_kwargs: object) -> bytes:
        if str(path) in prior_tree:
            return prior_tree[str(path)]
        row, suffix = proc(path)
        if suffix == "cmdline":
            return b"\0".join(value.encode() for value in row["cmdline"]) + b"\0"
        raise AssertionError("Unexpected synthetic binary read: " + str(path))

    def readlink(path: object) -> str:
        if str(path) == "/proc/1/ns/net":
            return HOST_NAMESPACE
        if str(path) == "/proc/self/ns/net":
            return current_namespace
        row, suffix = proc(Path(path))
        if suffix == "ns/net":
            return row["networkNamespace"]
        if suffix == "exe":
            return row["exe"]
        raise AssertionError("Unexpected synthetic symbolic-link read: " + str(path))

    def service(unit: str, fields: tuple[str, ...]) -> dict:
        return {field: properties[unit][field] for field in fields}

    def retained_paths(path: Path, pattern: str) -> list[Path]:
        if path == MODULE.PREVIOUS_FAILED_EVIDENCE and pattern == "*":
            return sorted({Path(name) for name in prior_tree} | (prior_directories - {path}))
        if path == MODULE.FIRST_CAPTURE_ROOT and pattern == "*":
            return sorted(captured_paths)
        if path == MODULE.PREVIOUS_CAPTURE_FAILURE_ROOT and pattern == "*":
            return sorted(captured_evidence)
        raise AssertionError("Unexpected traversal outside the synthetic previous evidence tree")

    def exists(path: Path) -> bool:
        if path == MODULE.PREVIOUS_FAILED_DATA:
            return prior_data_exists
        raise AssertionError("Unexpected synthetic existence check: " + str(path))

    def digest(path: Path) -> str:
        if str(path) in prior_tree:
            return hashlib.sha256(prior_tree[str(path)]).hexdigest()
        if path == Path(MODULE.key.__file__):
            return MODULE.PINNED_KEY_RECORDER_SHA256
        if path in captured_hashes:
            return captured_hashes[path]
        raise AssertionError("Unexpected digest outside the synthetic source and retained evidence")

    with contextlib.ExitStack() as mocks:
        mocks.enter_context(patch.object(Path, "stat", new=file_stat))
        mocks.enter_context(patch.object(Path, "lstat", new=file_stat))
        mocks.enter_context(patch.object(Path, "resolve", new=lambda path, *_args, **_kwargs: path))
        mocks.enter_context(patch.object(Path, "is_symlink", return_value=False))
        mocks.enter_context(patch.object(Path, "is_file", new=lambda path: str(path) in prior_tree))
        mocks.enter_context(patch.object(Path, "exists", new=exists))
        mocks.enter_context(patch.object(Path, "rglob", new=retained_paths))
        mocks.enter_context(patch.object(Path, "read_text", new=read_text))
        mocks.enter_context(patch.object(Path, "read_bytes", new=read_bytes))
        mocks.enter_context(patch.object(os, "readlink", side_effect=readlink))
        mocks.enter_context(patch.object(MODULE.base, "private_file"))
        mocks.enter_context(patch.object(MODULE.base, "digest", side_effect=digest))
        mocks.enter_context(patch.object(MODULE.base.shutil, "disk_usage", return_value=SimpleNamespace(free=free_bytes)))
        service_mock = mocks.enter_context(patch.object(MODULE, "service_properties", side_effect=service))
        yield service_mock


class FreshSafetyTests(unittest.TestCase):
    def setUp(self) -> None:
        self.namespace_patch = patch.object(MODULE, "ACTIVE_NETWORK_NAMESPACE", None)
        self.namespace_patch.start()
        self.addCleanup(self.namespace_patch.stop)

    def recorder(self) -> object:
        recorder = MODULE.Recorder.__new__(MODULE.Recorder)
        recorder.manifest, recorder.pid = manifest_fixture(), FRESH_PID
        recorder.server_public_id = recorder.manifest["serverId"]
        recorder.previous_device_ids = {row["Id"] for row in recorder.manifest["bootstrapDevices"]}
        recorder.old_devices = {row["Id"]: copy.deepcopy(row) for row in recorder.manifest["bootstrapDevices"]}
        recorder.old_options = {device_id: {"status": 200, "body": {}} for device_id in recorder.old_devices}
        recorder.old_users = copy.deepcopy(recorder.manifest["bootstrapUsers"])
        recorder.fresh_owned_server_baselines, recorder.fresh_owned_servers = {}, {}
        recorder.protected_hidden_devices, recorder.protected_hidden_options = {}, {}
        recorder.control_id = "30"
        metadata = MODULE.Recorder.metadata("control")
        recorder.owned_devices = {"30": {"Id": "30", "ReportedDeviceId": metadata["DeviceId"], "AppName": metadata["Client"]}}
        recorder.protected_device_aliases = recorder.aliases([*recorder.old_devices.values(), recorder.owned_devices["30"]])
        recorder.old_keys, recorder.old_key_baseline_complete, recorder.device_baseline_complete = [], True, True
        recorder.key_create_attempts, recorder.owned, recorder.key_credential_ids, recorder.secrets = set(), {}, {}, set()
        recorder.forbidden_tokens = set()
        recorder.server_creation_gate = {"evaluated": False, "allowed": False}
        recorder.server_baseline_info = {recorder.server_public_id: {"status": 200, "body": copy.deepcopy(recorder.old_devices["7"])}}
        recorder.read_ids, recorder.virtual_write_id = set(recorder.old_devices) | {"30"}, None
        recorder.server_branch = {"verified": False, "deleteAttempted": False}
        return recorder

    def establish_gate(self, recorder: object) -> None:
        with patch.object(MODULE, "read_manifest", return_value=copy.deepcopy(recorder.manifest)), \
                patch.object(MODULE, "preconditions", return_value=recorder.pid), patch.object(MODULE.base, "save"):
            recorder.establish_server_creation_gate()

    @staticmethod
    def owned_key_rows(recorder: object, device_id: int = 7) -> list[dict]:
        rows = [{"AccessToken": "synthetic-fresh-alpha-key", "AppName": MODULE.key.KEY_APPS["alpha"],
                 "DeviceId": device_id, "ReportedDeviceId": recorder.server_public_id},
                {"AccessToken": "synthetic-fresh-sibling-key", "AppName": MODULE.key.KEY_APPS["sibling"],
                 "DeviceId": device_id, "ReportedDeviceId": recorder.server_public_id}]
        recorder.key_create_attempts = set(MODULE.key.KEY_APPS.values())
        recorder.acknowledge_keys(rows)
        return rows

    def test_pinned_recorder_source_and_standard_http_module_are_unchanged(self) -> None:
        self.assertEqual(PINNED_SOURCE_HASH, MODULE.PINNED_KEY_RECORDER_SHA256)
        self.assertTrue(STANDARD_TRANSPORT_UNCHANGED)
        self.assertIs(http.client.HTTPConnection, STANDARD_HTTP_CONNECTION)
        self.assertIsNot(MODULE.key.http, http.client)
        self.assertIs(MODULE.key.http.client.HTTPConnection, MODULE.FreshHTTPConnection)

    def test_explicit_run_selection_accepts_only_reviewed_canonical_values(self) -> None:
        with patch.object(Path, "exists", return_value=True) as exists:
            self.assertEqual(MODULE.selected_run("1"), 1)
            self.assertEqual(MODULE.selected_run("2"), 2)
            self.assertEqual(MODULE.selected_run("1"), 1)
            for value in ("", "0", "3", "01", "02", "+2", "-1", " 1", "2 ", "2\n", "2.0", "\uff12"):
                with self.subTest(value=value), self.assertRaises(RuntimeError):
                    MODULE.selected_run(value)
        exists.assert_not_called()

    def test_capture_attempt_selection_is_explicit_and_does_not_probe_for_a_free_directory(self) -> None:
        with patch.object(Path, "exists", return_value=True) as exists:
            self.assertEqual(MODULE.selected_capture_run("1"), 1)
            self.assertEqual(MODULE.selected_capture_run("2"), 2)
            for value in ("", "0", "3", "01", "02", "+2", " 2", "2\n", "2.0", "\uff12"):
                with self.subTest(value=value), self.assertRaises(RuntimeError):
                    MODULE.selected_capture_run(value)
        exists.assert_not_called()

    def test_known_read_only_hardlinked_media_does_not_relax_record_or_private_evidence_links(self) -> None:
        source = Path("/synthetic/readonly/cancel-0000.mp4")
        regular = SimpleNamespace(st_mode=stat.S_IFREG | 0o644, st_nlink=201)
        with patch.object(Path, "resolve", new=lambda path, *_args, **_kwargs: path), patch.object(Path, "lstat", return_value=regular):
            MODULE.verify_original_preserved_path(source, "media")
            for group in ("records", "privateFiles", "other"):
                with self.subTest(group=group), self.assertRaises(RuntimeError):
                    MODULE.verify_original_preserved_path(source, group)
        with patch.object(Path, "resolve", new=lambda path, *_args, **_kwargs: path), \
                patch.object(Path, "lstat", return_value=SimpleNamespace(st_mode=stat.S_IFLNK | 0o777, st_nlink=1)), self.assertRaises(RuntimeError):
            MODULE.verify_original_preserved_path(source, "media")
        with patch.object(Path, "resolve", return_value=Path("/synthetic/other-source")), \
                patch.object(Path, "lstat", return_value=regular), self.assertRaises(RuntimeError):
            MODULE.verify_original_preserved_path(source, "media")

    def test_second_capture_preserves_the_empty_first_root_and_pinned_constructor_failure_evidence(self) -> None:
        with patch.object(MODULE, "CAPTURE_RUN", 2), patch.object(MODULE, "FRESH_RUN", 2):
            with virtual_system(manifest_fixture()):
                observed = MODULE.previous_empty_capture_snapshot()
            self.assertEqual(set(observed), {str(MODULE.FIRST_CAPTURE_ROOT / ".goby-managed"),
                                            str(MODULE.PREVIOUS_CAPTURE_SOURCE), str(MODULE.PREVIOUS_CAPTURE_CONSOLE)})
            self.assertEqual(observed[str(MODULE.PREVIOUS_CAPTURE_SOURCE)], MODULE.PREVIOUS_CAPTURE_SOURCE_SHA256)
            self.assertEqual(observed[str(MODULE.PREVIOUS_CAPTURE_CONSOLE)], MODULE.PREVIOUS_CAPTURE_CONSOLE_SHA256)
        with patch.object(MODULE, "CAPTURE_RUN", 1):
            self.assertEqual(MODULE.previous_empty_capture_snapshot(), {})

    def test_second_capture_rejects_prior_http_or_changed_failure_source_console_and_root_ownership(self) -> None:
        expected = {MODULE.FIRST_CAPTURE_ROOT / "private", MODULE.FIRST_CAPTURE_ROOT / "private/raw",
                    MODULE.FIRST_CAPTURE_ROOT / "export", MODULE.FIRST_CAPTURE_ROOT / ".goby-managed"}
        cases = [dict(capture_entries=expected | {MODULE.FIRST_CAPTURE_ROOT / "private/baseline.json"}),
                 dict(capture_entries=expected | {MODULE.FIRST_CAPTURE_ROOT / "private/raw/unexpected-http.json"}),
                 dict(capture_evidence_entries={MODULE.PREVIOUS_CAPTURE_SOURCE, MODULE.PREVIOUS_CAPTURE_CONSOLE,
                                                MODULE.PREVIOUS_CAPTURE_FAILURE_ROOT / "unexpected.json"}),
                 dict(capture_marker="Unrelated capture marker"),
                 dict(capture_hashes={MODULE.PREVIOUS_CAPTURE_SOURCE: "0" * 64}),
                 dict(capture_hashes={MODULE.PREVIOUS_CAPTURE_CONSOLE: "0" * 64}),
                 dict(capture_metadata={str(MODULE.PREVIOUS_CAPTURE_SOURCE): {"st_nlink": 2}}),
                 dict(capture_metadata={str(MODULE.FIRST_CAPTURE_ROOT): {"st_uid": 995}})]
        with patch.object(MODULE, "CAPTURE_RUN", 2), patch.object(MODULE, "FRESH_RUN", 2):
            for index, options in enumerate(cases):
                with self.subTest(case=index), virtual_system(manifest_fixture(), **options), self.assertRaises(RuntimeError):
                    MODULE.previous_empty_capture_snapshot()
        with patch.object(MODULE, "CAPTURE_RUN", 2), patch.object(MODULE, "FRESH_RUN", 1), self.assertRaises(RuntimeError):
            MODULE.previous_empty_capture_snapshot()

    def test_run_one_cannot_adopt_previous_failure_authority(self) -> None:
        with patch.object(MODULE, "FRESH_RUN", 1):
            self.assertEqual(MODULE.previous_failure_snapshot({"previousFailureProvenance": None}), {})
            with self.assertRaises(RuntimeError):
                MODULE.validate_previous_failure_provenance({"previousFailureProvenance": previous_failure_fixture()[0]})

    def test_run_two_requires_the_exact_bounded_first_failure_provenance(self) -> None:
        with patch.object(MODULE, "FRESH_RUN", 2):
            valid = manifest_fixture()
            MODULE.validate_previous_failure_provenance(valid)
            provenance = valid["previousFailureProvenance"]
            changes = [("referenceRun", 2), ("evidenceRoot", "/synthetic/other-evidence"), ("unit", "other.service"),
                       ("programData", "/synthetic/other-program-data"), ("outcome", "complete"), ("setupRecordCount", 1),
                       ("fileCount", provenance["fileCount"] + 1), ("totalBytes", 64 * 1024 * 1024),
                       ("operatorSource", "/synthetic/unattested-operator.py"), ("operatorSha256", "0" * 64),
                       ("cleanupRecord", str(MODULE.PREVIOUS_FAILED_EVIDENCE / "private/unattested-cleanup.json")),
                       ("files", {"/synthetic/foreign.py": "1" * 64})]
            with self.assertRaises(RuntimeError):
                MODULE.validate_previous_failure_provenance({"previousFailureProvenance": None})
            for field, value in changes:
                with self.subTest(field=field):
                    invalid = copy.deepcopy(valid)
                    invalid["previousFailureProvenance"][field] = value
                    with self.assertRaises(RuntimeError):
                        MODULE.validate_previous_failure_provenance(invalid)

    def test_previous_failure_snapshot_preserves_the_complete_attested_file_tree(self) -> None:
        with patch.object(MODULE, "FRESH_RUN", 2):
            manifest = manifest_fixture()
            with virtual_system(manifest):
                observed = MODULE.previous_failure_snapshot(manifest)
            self.assertEqual(observed, manifest["previousFailureProvenance"]["files"])
            self.assertEqual(len(observed), 4)
            self.assertIsNone(MODULE.ACTIVE_NETWORK_NAMESPACE)

    def test_previous_failure_snapshot_refuses_changed_bytes_or_unproven_cleanup(self) -> None:
        conditions = ("missing-file", "extra-file", "changed-bytes", "hard-link", "data-returned", "unit-returned",
                      "cleanup-not-proven", "stop-not-proven", "old-preservation-failed", "unexpected-http", "unexpected-identity")
        with patch.object(MODULE, "FRESH_RUN", 2):
            for condition in conditions:
                with self.subTest(condition=condition):
                    manifest = manifest_fixture()
                    provenance = manifest["previousFailureProvenance"]
                    files = previous_failure_fixture()[1]
                    options = {}
                    if condition == "missing-file":
                        files.pop(provenance["operatorSource"])
                    elif condition == "extra-file":
                        files[str(MODULE.PREVIOUS_FAILED_EVIDENCE / "unexpected.txt")] = b"Synthetic unexpected evidence"
                    elif condition == "changed-bytes":
                        name = provenance["operatorSource"]
                        files[name] = files[name].replace(b"Synthetic", b"SynthetiX", 1)
                    elif condition == "hard-link":
                        options["prior_metadata"] = {provenance["operatorSource"]: {"st_nlink": 2}}
                    elif condition == "data-returned":
                        options["prior_data_exists"] = True
                    elif condition == "unit-returned":
                        options["prior_unit_state"] = "loaded"
                    elif condition in {"cleanup-not-proven", "stop-not-proven", "old-preservation-failed"}:
                        name = provenance["cleanupRecord"] if condition == "cleanup-not-proven" else provenance["failureRecord"]
                        content = json.loads(files[name])
                        if condition == "cleanup-not-proven":
                            content["dataRemoved"] = False
                        elif condition == "stop-not-proven":
                            content["cleanup"]["stopped"] = False
                        else:
                            content["preservationErrors"] = ["Synthetic preservation failure"]
                        files[name] = json.dumps(content).encode()
                    elif condition == "unexpected-http":
                        files[str(MODULE.PREVIOUS_FAILED_EVIDENCE / "private/raw/unexpected-http.json")] = b"{}"
                    else:
                        files[str(MODULE.PREVIOUS_FAILED_EVIDENCE / "private/service-identity.json")] = b"{}"
                    if condition in {"cleanup-not-proven", "stop-not-proven", "old-preservation-failed", "unexpected-http", "unexpected-identity"}:
                        # A self-consistent hash list cannot turn HTTP, an
                        # acknowledged process, or failed cleanup into a safe
                        # initialization failure before HTTP.
                        provenance["files"] = {name: hashlib.sha256(content).hexdigest() for name, content in files.items()}
                        provenance["fileCount"] = len(files)
                        provenance["totalBytes"] = sum(len(content) for content in files.values())
                    with virtual_system(manifest, prior_files=files, **options), self.assertRaises(RuntimeError):
                        MODULE.previous_failure_snapshot(manifest)

    def test_transport_maps_only_the_historical_constructor_to_the_fresh_endpoint(self) -> None:
        MODULE.ACTIVE_NETWORK_NAMESPACE = FRESH_NAMESPACE
        with patch.object(os, "readlink", return_value=FRESH_NAMESPACE), \
                patch.object(STANDARD_HTTP_CONNECTION, "__init__", return_value=None) as initialize:
            MODULE.FreshHTTPConnection("127.0.0.1", 18097, timeout=3)
        initialize.assert_called_once_with("127.0.0.1", 18098, timeout=3)
        self.assertIs(http.client.HTTPConnection, STANDARD_HTTP_CONNECTION)

    def test_transport_rejects_unattested_namespaces_and_arbitrary_constructor_destinations(self) -> None:
        cases = [(None, FRESH_NAMESPACE, "127.0.0.1", 18097), (FRESH_NAMESPACE, HOST_NAMESPACE, "127.0.0.1", 18097),
                 (FRESH_NAMESPACE, "net:[410002]", "127.0.0.1", 18097), (FRESH_NAMESPACE, FRESH_NAMESPACE, "localhost", 18097),
                 (FRESH_NAMESPACE, FRESH_NAMESPACE, "127.0.0.2", 18097), (FRESH_NAMESPACE, FRESH_NAMESPACE, "127.0.0.1", 18098),
                 (FRESH_NAMESPACE, FRESH_NAMESPACE, "127.0.0.1", 80)]
        for active, current, host, port in cases:
            with self.subTest(active=active, current=current, host=host, port=port):
                MODULE.ACTIVE_NETWORK_NAMESPACE = active
                with patch.object(os, "readlink", return_value=current), \
                        patch.object(STANDARD_HTTP_CONNECTION, "__init__", return_value=None) as initialize:
                    with self.assertRaises(RuntimeError):
                        MODULE.FreshHTTPConnection(host, port)
                initialize.assert_not_called()

    def test_ready_manifest_requires_complete_owned_authority_and_exact_credential_paths(self) -> None:
        valid = manifest_fixture()
        with virtual_system(valid):
            self.assertEqual(MODULE.read_manifest(), valid)
        changes = [("state", "PREPARING"), ("referenceRun", 1 if MODULE.FRESH_RUN == 2 else 2),
                   ("programData", "/opt/goby-test/emby-reference-data"),
                   ("serverId", MODULE.ORIGINAL_SERVER_ID), ("bootstrapCredentialsRevoked", False),
                   ("bootstrapCredentialsInvalidity", {"admin": 401, "viewer": 200}),
                   ("bootstrapCredentialsInvalidity", {"admin": 401}),
                   ("allFreshProgramDataOwned", False), ("oldPreservationVerified", False),
                   ("viewerEmptyPasswordLoginVerified", False), ("port", 18097),
                   ("adminCredentialsFile", "/opt/goby-test/old-admin-credentials.env"),
                   ("viewerCredentialsFile", str(MODULE.EVIDENCE / "private/admin-credentials.env")),
                   ("setupRawRoot", "/synthetic/old-capture/private/raw"), ("setupRecordCount", True),
                   ("serviceProperties", {}),
                   ("pid", next(iter(MODULE.ORIGINAL_SERVICES.values()))),
                   ("networkNamespace", valid["oldServices"][next(iter(MODULE.ORIGINAL_SERVICES))]["networkNamespace"]),
                   ("networkNamespace", HOST_NAMESPACE)]
        for field, value in changes:
            with self.subTest(field=field, value=value):
                invalid = copy.deepcopy(valid)
                invalid[field] = value
                with virtual_system(invalid), self.assertRaises(RuntimeError):
                    MODULE.read_manifest()

    def test_ready_manifest_rejects_ambiguous_bootstrap_membership(self) -> None:
        for field, value in (("bootstrapUsers", [{"Id": "same"}, {"Id": "same"}]),
                             ("bootstrapUsers", [{"Id": "only-user"}]),
                             ("bootstrapDevices", [{"Id": "same"}, {"Id": "same"}]),
                             ("bootstrapServerInfo", {"alias": MODULE.ORIGINAL_SERVER_ID})):
            with self.subTest(field=field):
                invalid = manifest_fixture()
                invalid[field] = value
                with virtual_system(invalid), self.assertRaises(RuntimeError):
                    MODULE.read_manifest()

    def test_original_service_process_and_static_properties_must_match_attestation(self) -> None:
        valid = manifest_fixture()
        with virtual_system(valid):
            MODULE.check_original_services(valid)
        unit = next(iter(MODULE.ORIGINAL_SERVICES))
        expected = valid["oldServices"][unit]
        changes = [("uid", 995), ("startTicks", "changed"), ("networkNamespace", FRESH_NAMESPACE),
                   ("cmdline", ["/synthetic/replaced"]), ("exe", "/synthetic/replaced"), ("cgroup", "0::/wrong-scope\n")]
        for field, value in changes:
            with self.subTest(field=field):
                actual = {**expected, field: value}
                with virtual_system(valid, actual_identities={expected["pid"]: actual}), self.assertRaises(RuntimeError):
                    MODULE.check_original_services(valid)
        for changes in ({"MainPID": "999"}, {"ActiveState": "inactive"}, {"ProtectSystem": "no"}):
            with self.subTest(properties=changes):
                with virtual_system(valid, property_overrides={unit: changes}), self.assertRaises(RuntimeError):
                    MODULE.check_original_services(valid)

    def test_preconditions_publish_transport_authority_only_after_every_check_passes(self) -> None:
        valid = manifest_fixture()
        with virtual_system(valid):
            self.assertEqual(MODULE.preconditions(), FRESH_PID)
        self.assertEqual(MODULE.ACTIVE_NETWORK_NAMESPACE, FRESH_NAMESPACE)
        with virtual_system(valid), patch.object(MODULE.base, "digest", return_value="0" * 64), \
                patch.object(MODULE, "read_manifest") as read_manifest:
            with self.assertRaises(RuntimeError):
                MODULE.preconditions()
        read_manifest.assert_not_called()
        self.assertIsNone(MODULE.ACTIVE_NETWORK_NAMESPACE)
        with patch.object(os, "readlink", return_value=FRESH_NAMESPACE), \
                patch.object(STANDARD_HTTP_CONNECTION, "__init__", return_value=None) as initialize:
            with self.assertRaises(RuntimeError):
                MODULE.FreshHTTPConnection("127.0.0.1", 18097)
        initialize.assert_not_called()

    def test_failed_preconditions_never_leave_a_previous_namespace_authorized(self) -> None:
        valid = manifest_fixture()
        failures = [("process-start", {"actual_identities": {FRESH_PID: {**valid, "startTicks": "9002"}}}),
                    ("fresh-static", {"property_overrides": {MODULE.FRESH_UNIT: {"ProtectSystem": "no"}}}),
                    ("readonly-mount", {"mount_overrides": {"/dev/shm/goby-emby-reference": "rw"}}),
                    ("writable-mount", {"mount_overrides": {str(MODULE.FRESH_DATA): "ro"}}),
                    ("scratch-budget", {"free_bytes": 1})]
        for label, options in failures:
            with self.subTest(label=label):
                MODULE.ACTIVE_NETWORK_NAMESPACE = FRESH_NAMESPACE
                with virtual_system(valid, **options), self.assertRaises(RuntimeError):
                    MODULE.preconditions()
                self.assertIsNone(MODULE.ACTIVE_NETWORK_NAMESPACE)
                with patch.object(os, "readlink", return_value=FRESH_NAMESPACE), \
                        patch.object(STANDARD_HTTP_CONNECTION, "__init__", return_value=None) as initialize:
                    with self.assertRaises(RuntimeError):
                        MODULE.FreshHTTPConnection("127.0.0.1", 18097)
                initialize.assert_not_called()

    def test_namespace_handoff_cannot_start_http_before_verified_reentry(self) -> None:
        valid = manifest_fixture()
        with virtual_system(valid, current_namespace=HOST_NAMESPACE), \
                patch.object(os, "execvp", side_effect=RuntimeError("Synthetic namespace handoff")) as handoff:
            with self.assertRaisesRegex(RuntimeError, "Synthetic namespace handoff"):
                MODULE.preconditions()
        handoff.assert_called_once()
        self.assertEqual(handoff.call_args.args[0], "nsenter")
        self.assertEqual(handoff.call_args.args[1][1:4], ["-t", str(FRESH_PID), "-n"])
        self.assertIsNone(MODULE.ACTIVE_NETWORK_NAMESPACE)

    def test_matching_manifest_and_runtime_cannot_authorize_an_unsafe_sandbox(self) -> None:
        for condition in ("permissive-root", "extra-writable-path", "missing-readonly-path", "duplicate-programdata", "under-dev-working-directory"):
            with self.subTest(condition=condition):
                manifest = manifest_fixture()
                if condition == "permissive-root":
                    manifest["serviceProperties"]["ProtectSystem"] = "no"
                elif condition == "extra-writable-path":
                    manifest["serviceProperties"]["ReadWritePaths"] += " /opt/goby-test/emby-reference-data"
                elif condition == "missing-readonly-path":
                    manifest["serviceProperties"]["ReadOnlyPaths"] = "/dev/shm /dev/shm/goby-emby-reference"
                elif condition == "duplicate-programdata":
                    manifest["cmdline"].extend(["-programdata", "/opt/goby-test/emby-reference-data"])
                else:
                    manifest["serviceProperties"]["WorkingDirectory"] = str(MODULE.FRESH_DATA)
                MODULE.ACTIVE_NETWORK_NAMESPACE = FRESH_NAMESPACE
                with virtual_system(manifest), self.assertRaises(RuntimeError):
                    MODULE.preconditions()
                self.assertIsNone(MODULE.ACTIVE_NETWORK_NAMESPACE)

    def test_owned_positive_server_baseline_lifts_only_the_server_aliases(self) -> None:
        recorder = self.recorder()
        ordinary = {name: copy.deepcopy(recorder.old_devices[name]) for name in ("5", "6")}
        self.establish_gate(recorder)
        self.assertTrue(recorder.server_creation_gate["allowed"])
        self.assertFalse(recorder.server_creation_gate["baselineReportedAbsenceRequired"])
        self.assertEqual(set(recorder.fresh_owned_server_baselines), {"7"})
        self.assertEqual(recorder.old_devices, ordinary)
        self.assertEqual(set(recorder.old_options), {"5", "6"})
        self.assertTrue(recorder.aliases([*ordinary.values(), recorder.owned_devices["30"]]) <= recorder.protected_device_aliases)
        self.assertNotIn("7", recorder.protected_device_aliases)
        self.assertNotIn(recorder.server_public_id, recorder.protected_device_aliases)
        self.assertIsNone(recorder.server_creation_reason())

    def test_owned_server_baseline_cannot_collide_with_bootstrap_or_control_aliases(self) -> None:
        for alias in ("5", "30", "synthetic-bootstrap-viewer-device"):
            with self.subTest(alias=alias):
                recorder = self.recorder()
                recorder.old_devices["7"]["InternalId"] = int(alias) if alias.isdigit() else alias
                before = copy.deepcopy(recorder.old_devices)
                with self.assertRaises(RuntimeError):
                    self.establish_gate(recorder)
                self.assertFalse(recorder.server_creation_gate["allowed"])
                self.assertEqual(recorder.fresh_owned_server_baselines, {})
                self.assertEqual(recorder.old_devices, before)

    def test_hidden_fresh_server_lifts_only_its_own_aliases_and_preserves_hidden_ordinary_devices(self) -> None:
        for collision in (False, True):
            with self.subTest(collision=collision):
                recorder = self.recorder()
                server = recorder.old_devices.pop("7")
                recorder.old_options.pop("7")
                recorder.manifest["bootstrapDevices"] = [row for row in recorder.manifest["bootstrapDevices"] if row["Id"] != "7"]
                recorder.previous_device_ids.remove("7")
                ordinary = {"Id": "8", "ReportedDeviceId": "synthetic-hidden-ordinary-device", "AppName": "Hidden Ordinary"}
                recorder.protected_hidden_devices = {"7": server, "8": ordinary}
                recorder.protected_hidden_options = {"7": {"status": 200, "body": {}},
                                                     "8": {"status": 200, "body": {"CustomName": "Preserved Hidden Name"}}}
                if collision:
                    server["InternalId"] = 8
                    with self.assertRaises(RuntimeError):
                        self.establish_gate(recorder)
                    self.assertFalse(recorder.server_creation_gate["allowed"])
                    self.assertEqual(recorder.fresh_owned_server_baselines, {})
                    self.assertEqual(set(recorder.protected_hidden_devices), {"7", "8"})
                else:
                    self.establish_gate(recorder)
                    self.assertTrue(recorder.server_creation_gate["allowed"])
                    self.assertEqual(set(recorder.fresh_owned_server_baselines), {"7"})
                    self.assertEqual(recorder.protected_hidden_devices, {"8": ordinary})
                    self.assertEqual(set(recorder.protected_hidden_options), {"8"})
                    self.assertTrue(recorder.aliases([ordinary, recorder.owned_devices["30"]]) <= recorder.protected_device_aliases)
                    self.assertNotIn(recorder.server_public_id, recorder.protected_device_aliases)

    def test_owned_server_gate_still_requires_exact_fresh_device_and_user_membership(self) -> None:
        for field in ("old-key", "incomplete-key-baseline", "incomplete-device-baseline", "device-membership", "user-membership", "server-identity"):
            with self.subTest(field=field):
                recorder = self.recorder()
                if field == "old-key":
                    recorder.old_keys = [{"AccessToken": "synthetic-preexisting-key"}]
                elif field == "incomplete-key-baseline":
                    recorder.old_key_baseline_complete = False
                elif field == "incomplete-device-baseline":
                    recorder.device_baseline_complete = False
                elif field == "device-membership":
                    recorder.old_devices.pop("6")
                elif field == "user-membership":
                    recorder.old_users = recorder.old_users[:1]
                elif field == "server-identity":
                    recorder.server_public_id = MODULE.ORIGINAL_SERVER_ID
                with self.assertRaises(RuntimeError):
                    self.establish_gate(recorder)
                self.assertFalse(recorder.server_creation_gate["allowed"])

    def test_register_server_requires_both_current_keys_and_positive_protected_alias_checks(self) -> None:
        recorder = self.recorder()
        self.establish_gate(recorder)
        rows = self.owned_key_rows(recorder)
        selected = copy.deepcopy(recorder.manifest["bootstrapServerInfo"]["body"])
        recorder.register_server(selected, rows)
        self.assertTrue(recorder.owned_pair(selected))
        self.assertEqual(recorder.fresh_owned_servers, {"7": selected})
        self.assertFalse(recorder.owned_pair({**selected, "AppName": "Unrelated App"}))
        invalid = [(selected, rows[:1]), (selected, [rows[0], {**rows[1], "DeviceId": 8}]),
                   (selected, [rows[0], {**rows[1], "AccessToken": "synthetic-unowned-key"}]),
                   ({**selected, "AppName": ""}, rows), ({**selected, "InternalId": 5}, rows),
                   ({**selected, "InternalId": 30}, rows)]
        for index, (candidate, current_rows) in enumerate(invalid):
            with self.subTest(case=index):
                recorder.fresh_owned_servers = {}
                recorder.owned_devices.pop("7", None)
                with self.assertRaises(RuntimeError):
                    recorder.register_server(candidate, current_rows)
                self.assertEqual(recorder.fresh_owned_servers, {})
                self.assertNotIn("7", recorder.owned_devices)


def main() -> None:
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(FreshSafetyTests)
    stream = io.StringIO()
    forbidden = RuntimeError("Fresh synthetic tests forbid external side effects")
    with contextlib.ExitStack() as guards:
        targets = [(socket, "socket"), (socket, "create_connection"), (STANDARD_HTTP_CONNECTION, "__init__"),
                   (subprocess, "Popen"), (subprocess, "run"), (subprocess, "check_output"), (os, "execvp"),
                   (os, "system"), (os, "open"), (os, "unlink"), (os, "remove"), (os, "rename"), (os, "replace"),
                   (os, "readlink"), (Path, "mkdir"), (Path, "write_text"), (Path, "write_bytes"),
                   (Path, "read_text"), (Path, "read_bytes"), (Path, "stat"), (Path, "lstat"), (Path, "glob"),
                   (Path, "rglob"), (Path, "exists"), (Path, "is_file"),
                   (MODULE.signal, "setitimer"), (MODULE.time, "sleep"), (MODULE.base, "save"), (MODULE.base, "private_file"),
                   (MODULE.base, "digest"), (MODULE.Recorder, "__init__"), (MODULE.key.Recorder, "__init__"),
                   (MODULE.device.Recorder, "__init__"), (MODULE.base.Recorder, "__init__")]
        for owner, name in targets:
            guards.enter_context(patch.object(owner, name, side_effect=forbidden))
        result = unittest.TextTestRunner(stream=stream).run(suite)
    print(json.dumps({"suite": "fresh-reference-key-device-hardening", "result": "passed" if result.wasSuccessful() else "failed",
                      "tests": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
                      "failedTests": [test.id() for test, _ in result.failures + result.errors],
                      "referenceRun": MODULE.FRESH_RUN, "recorderSha256": SOURCE_HASH, "testSha256": TEST_HASH,
                      "captureRun": MODULE.CAPTURE_RUN,
                      "pinnedKeyRecorderSha256": PINNED_SOURCE_HASH,
                      "liveHTTPRequests": 0, "captureWrites": 0, "processCalls": 0}, sort_keys=True))
    if not result.wasSuccessful():
        print(stream.getvalue(), file=sys.stderr)
    raise SystemExit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
