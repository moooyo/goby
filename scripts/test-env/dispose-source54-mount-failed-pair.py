#!/usr/bin/env python3
"""Dispose only the reviewed, empty source54 failed actual-mount pair.

The failed run, its source, and its live retained receipt remain immutable.
The pinned runner removes only the two exact owned identities, without FORCE,
backend termination, schema normalization, or configuration changes. A separate
attestation lets a fresh runner validate the disposal. Any attempt is one-shot.
"""

from __future__ import annotations

import argparse
import copy
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pwd
import stat
import sys
from types import SimpleNamespace

sys.dont_write_bytecode = True
RUN = "20260912_061505_b2b453a716a9"
WORK = Path("/opt/goby-test/exec-work-m3e")
SOURCE = WORK / "source-attempt-54"
OUTPUT = WORK / ("client-backup-run-" + RUN)
EXECUTION = WORK / "bound-scan-root-mount-execution-01"
CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
RUNNER = SOURCE / "scripts/test-env/run-client-backup-tests.py"
GUARDS = WORK / "schema28-runner-guard-01"
RUNNER_SHA = "4c76cfa22fd1915a270756d82e313ce6e7bbf3a5bcc857339fd6d254f5c894a2"
MANIFEST_SHA = "c5a5cf6bf0afa973fbd2c087d300dbe6bd3079f92c7237b5a108d76ef8ae6fc3"
RECEIPT_SHA = "190c202ca956a52b1914fabe7363ea821bd691f0a6f47869edc8785bc3fe288f"
REPORT_SHA = "248fc45c97250da02994d3947b401f43bb74ee568e3ebadb31dec46a291487f6"
LOG_SHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
LOG_BYTES = 0
TERMINAL_SHA = "737c70c0919491a559f0b79457849e34d6b0a19e394b955bf0ff5310ae88de60"
MOUNT_REPORT_SHA = "0111e24b9a21e8ba4cc0fdebf02707611c29915adb57610f7852d987463190a3"
MOUNT_WORKER_SHA = "a605cc2f3bd64c5258b261d4ead54e6d67ee691d84c12f36c6cb62028782f11c"
MOUNT_REQUEST_SHA = "41ad7c2cc37dfc2dedadf4cec9e2da81b0505ebe825e098b9a2bbbf86236a762"
RUN_SCRIPT_SHA = "fe008e8c899514c0221e1ff736dc175768dde728b88d0e2ca55cb722f030bc82"
VERIFICATION = "source54-original-storage-scan-recovery-private-mount"
HOST_NAMESPACE = "mnt:[4026531841]"
CATALOG_BEFORE_SHA = "ce62d3df80d4eb2b1595d712eeb30df38a9521c285722b2ca335daf90bb5d376"
CATALOG_SHA = "8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b"
HBA_SHA = "6571fe86239d7bec6f5e9de9ebefbeda56f0e7df43cdbc58a3dc064e41e958fd"
OWNER_SHA = "610b3dcdafa8b6bb960a26946c398b98c02357d61c9c7fdf9b5a8b8504b9d5af"
CLUSTER = {"system_identifier": "7684040109719526738", "process": {
    "pid": 327173, "start_ticks": 289359, "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"}}
PAIRS = [{"name": "goby_backup_m3e_source", "role_oid": 18925101, "database_oid": 18925102},
         {"name": "goby_backup_m3e_target", "role_oid": 18925103, "database_oid": 18925104}]
OUTPUT_IDENTITY = {"device": 2049, "inode": 2526409}
TAG = "goby-client-backup-pair-m3e-v1:" + RUN
UNIT = "goby-client-backup-20260912-061505-b2b453a716a9.service"
OUTER_UNIT = "goby-bound-scan-root-mount-controller-v1.service"
INNER_PROCESS = {"pid": 1329950, "start_ticks": 11657250,
                 "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"}
RUN_EXPRESSION = "^TestRootBindingScanMountNamespaceHelper$"
PACKAGES = ("./internal/library",)
FULL_RUN = "20260912_053517_9cb0074fc731"
FULL_REPORT = WORK / ("client-backup-run-" + FULL_RUN) / "report.json"
FULL_REPORT_SHA = "50f8543b1b11cf7a3e00d3a0a83684080835fdad0f54ca4c983650f0f1e8892e"
UNIT_PINS = {
    UNIT: {"InvocationID": "bf8018cffd8548e99d177f54628752cc", "ExecMainPID": "1329950", "Description": TAG},
    OUTER_UNIT: {"InvocationID": "6c741a464ae74b31839355d3ed2e9f06", "ExecMainPID": "1329773",
        "Description": "[systemd-run] /usr/bin/python3 -I -B " + str(WORK) +
        "/bound-scan-root-mount-tool-01/verify-bound-scan-root-mount.py --full-report " + str(FULL_REPORT) +
        " --full-report-sha256 " + FULL_REPORT_SHA + " --host-namespace \"" + HOST_NAMESPACE + "\""},
}
GUARD_SHA = {
    "stdout": "9ecb9d07c61c61b7510c8ed16e895ad2c146d0244920914906663d1d40703ea9",
    "stderr": "adcceb7f042242fdd8375aae0fbd1334ce7179489e307babca2d44babde86aac",
    "report.json": "ba27c2c539e5f18020db811c31d82e56b1e528e456d0a66e59baf0f1f9c8f6d3",
}
TREE_SHA = {
    str(SOURCE): "1d94233a92f26b4a9f66d6210de1c744d79729b9a6a4d63df9aca94fd866f355",
    str(OUTPUT): "a55839d5dda0ea825f77c0364e7a14b6e1148bc9969e743a76d04e1462035e3f",
    str(EXECUTION): "d2daa0c92c12a0d9029929cf3eb8feebf79db73f8220ca6a7077bda14309032a",
}
PUBLIC_OIDS = {"goby_backup_m3e_source": 2200, "goby_backup_m3e_target": 2200}
IDENTITY_SHA = {
    "goby_backup_m3e_source": "27ececc8bfdbffe1f3bbdecf0711fb1c8740fd3932592439734a3d5ce333ad7a",
    "goby_backup_m3e_target": "73a6af41578c6e663a0f310f6f8bd3a81fe427f8ea4e12cc918e97202882f1b3",
}
CASTS_SHA = "571c35d40ff27b9972b3275e878fac303ec17df99663c72a04801f9c973926ef"
OBJECT_CATALOGS = ("pg_class", "pg_proc", "pg_type", "pg_constraint", "pg_trigger", "pg_attrdef",
    "pg_collation", "pg_conversion", "pg_operator", "pg_opclass", "pg_opfamily", "pg_ts_config",
    "pg_ts_dict", "pg_ts_parser", "pg_ts_template", "pg_rewrite", "pg_extension", "pg_language",
    "pg_am", "pg_amop", "pg_amproc")
BINARY = {"path": str(OUTPUT / "tmp/library.test"), "bytes": 34877621, "device": 2049,
          "inode": 2526708, "sha256": "c9bf6cfd000afe3b0da512d98d7ffcf811885f31fb2cc3e7e8d82195d4fc03a0"}
ABSENT_MOUNT_ARTIFACTS = ("helper.stdout", "helper.stderr", "host-mountinfo-before", "host-loops-before.json",
                        "host-mountinfo-after", "host-loops-after.json", "host-mountinfo-terminal",
                        "host-loops-terminal.json", "tmp/goby-linux-amd64")
MOUNT_ADMISSION = {"host_namespace": HOST_NAMESPACE, "request_sha256": MOUNT_REQUEST_SHA,
    "run_script_sha256": RUN_SCRIPT_SHA, "verification": VERIFICATION, "worker_sha256": MOUNT_WORKER_SHA}
PREREQUISITES = {"full_controller_scope": str(WORK / "scan-reconciliation-full-execution-01"),
    "full_controller_terminal": {"cgroup_empty": True, "properties": {"ActiveState": "active", "ControlGroup": "",
        "ExecMainStatus": "0", "InvocationID": "c8641f6597f0411ca832d052b885499d", "MainPID": "0", "Result": "success",
        "SubState": "exited"}, "unit": "goby-scan-reconciliation-full-controller-v1.service"},
    "full_inner_unit": "goby-client-backup-20260912-053517-9cb0074fc731.service",
    "full_receipt_sha256": "6accf5be1fa133d5d9f82a270ead3a0e2e82e1b5e0685f1cfee4d09843a48479",
    "full_report": str(FULL_REPORT), "full_report_sha256": FULL_REPORT_SHA, "full_run": FULL_RUN,
    "prior_namespace_report_sha256": "8e3588a232a9838523485c587769e8d8a57bef28cacd31f6cefc6f96a800eb40",
    "prior_namespace_worker_sha256": "d71fa1157419bff0d7964ea7b442a7b2bdfe20475e429484b12b74ca49f598f4"}


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def exact_pair(m, pair, phase="owned"):
    identity = {key: pair.get(key) for key in ("name", "role_oid", "database_oid")}
    m.require(identity in PAIRS and type(identity["role_oid"]) is int and
              type(identity["database_oid"]) is int and pair.get("phase") == phase,
              "Only the exact reviewed pair and phase are authorized.")
    return identity


def validate_receipt(m, receipt):
    expected = {"marker": "goby-client-backup-pair-m3e-v1", "run_id": RUN, "tag": TAG,
        "unit": UNIT, "source": str(SOURCE), "output": str(OUTPUT), "source_manifest_sha256": MANIFEST_SHA,
        "schema": 28, "catalog_sha256": CATALOG_SHA, "mode": "targeted", "phase": "retained",
        "cleanup_complete": False, "cluster": CLUSTER, "cluster_owner_sha256": OWNER_SHA,
        "output_identity": OUTPUT_IDENTITY, "bound_scan_mount": MOUNT_ADMISSION}
    m.require(set(receipt) == set(expected) | {"pairs"} and
              all(receipt.get(key) == value for key, value in expected.items()) and
              type(receipt.get("schema")) is int and receipt.get("cleanup_complete") is False,
              "The failure receipt is outside this exact reviewed run.")
    m.require([exact_pair(m, pair) for pair in receipt["pairs"]] == PAIRS,
              "The original pair order or identity changed.")


def validate_failed_report(m, failed):
    expected = {"marker": "goby-client-backup-pair-m3e-v1", "status": "failed", "run_id": RUN,
        "source": str(SOURCE), "source_manifest_sha256": MANIFEST_SHA, "schema": 28, "mode": "targeted",
        "catalog_sha256": CATALOG_SHA, "unit": UNIT, "unit_exit": 1, "unit_process": INNER_PROCESS,
        "cluster": CLUSTER, "pair_evidence_retained": True, "hba_before_sha256": HBA_SHA,
        "hba_after_sha256": HBA_SHA,
        "error": "The verified unit failed or was never observed running.", "verification": VERIFICATION,
        "operator_sha256": MOUNT_WORKER_SHA, "prerequisites": PREREQUISITES,
        "cleanup": {"hba_restored_exactly": True, "receipt_saved": True, "unit_terminal": True}}
    m.require(set(failed) == set(expected) | {"tools"} and
              all(failed.get(key) == value for key, value in expected.items()) and
              failed.get("pair_evidence_retained") is True and type(failed.get("unit_exit")) is int,
              "The report does not prove this terminal failed run with restored HBA.")


def validate_mount_launch(m, args):
    m.require(args.source == SOURCE and args.manifest_sha256 == MANIFEST_SHA and
              type(args.schema) is int and args.schema == 28 and args.mode == "targeted" and
              args.run == RUN_EXPRESSION and args.package == list(PACKAGES),
              "This disposal requires the exact failed mount helper selector and source.")


def validate_mount_evidence(m, terminal, mount_report, artifacts):
    expected_report = {"binary": BINARY, "child_cleanup": {"killed": False, "terminal": True},
        "failure": "FileNotFoundError", "invocation_id": UNIT_PINS[UNIT]["InvocationID"],
        "loop_scope": "observed-only-no-loop-allocation", "manifest": MANIFEST_SHA, "run_id": RUN,
        "schema": 28, "source": str(SOURCE), "status": "failed", "system_reboot_tested": False,
        "verification": VERIFICATION}
    m.require(mount_report == expected_report and type(mount_report.get("schema")) is int and
              mount_report["child_cleanup"]["killed"] is False and
              mount_report["child_cleanup"]["terminal"] is True and mount_report["system_reboot_tested"] is False,
              "The mount failure no longer proves the reviewed pre-dispatch boundary.")
    state = {"ActiveState": "failed", "MainPID": "0", "Result": "exit-code", "ExecMainStatus": "1",
             "ControlGroup": "", "SubState": "failed"}
    expected_terminal = {"actual_mount_recovery_accepted": False, "binary": BINARY,
        "cleanup": {"hba_restored_exactly": True, "receipt_saved": True, "unit_terminal": True},
        "failure_boundary": "The sysfs block directory required by host_witness does not exist; the namespace helper was not dispatched.",
        "fixture_directory_empty": True, "helper_stdout_exists": False,
        "marker": "goby-bound-scan-root-mount-failed-terminal-v1", "mount_report_sha256": MOUNT_REPORT_SHA,
        "observed_at": "2026-09-12T06:16:36.133200+00:00", "pair_evidence_retained": True,
        "receipt_sha256": RECEIPT_SHA, "report_sha256": REPORT_SHA, "run_id": RUN,
        "states": [{"properties": dict(state, InvocationID=UNIT_PINS[unit]["InvocationID"]),
                    "recursive_cgroup_empty": True, "unit": unit} for unit in (OUTER_UNIT, UNIT)], "status": "failed"}
    m.require(terminal == expected_terminal and terminal["actual_mount_recovery_accepted"] is False and
              terminal["helper_stdout_exists"] is False and terminal["fixture_directory_empty"] is True and
              terminal["pair_evidence_retained"] is True and
              all(value is True for value in terminal["cleanup"].values()) and
              all(row["recursive_cgroup_empty"] is True for row in terminal["states"]),
              "The sealed failed mount terminal or its retained-pair boundary changed.")
    expected_artifacts = {"binary": BINARY, "fixture_directory_empty": True,
        "absent_paths": list(ABSENT_MOUNT_ARTIFACTS), "go_log_bytes": 0,
        "compile_stdout_bytes": 0, "compile_stderr_bytes": 0}
    m.require(artifacts == expected_artifacts and artifacts["fixture_directory_empty"] is True and
              all(type(artifacts[key]) is int for key in ("go_log_bytes", "compile_stdout_bytes", "compile_stderr_bytes")) and
              all(type(artifacts["binary"][key]) is int for key in ("bytes", "device", "inode")) and
              all(type(mount_report["binary"][key]) is int and type(terminal["binary"][key]) is int
                  for key in ("bytes", "device", "inode")),
              "The compiled artifact, empty fixture, or absent dispatch evidence changed.")


def observe_mount_artifacts(m):
    binary = OUTPUT / "tmp/library.test"
    info = m.canonical(binary)
    m.require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1, "The compiled helper is not the original regular file.")
    raw = m.private_read(binary, modes=(0o700, 0o755), limit=64 << 20)
    fixture = OUTPUT / "tmp/mount-fixtures"
    m.directory(fixture)
    return {"binary": {"path": str(binary), "bytes": len(raw), "device": info.st_dev, "inode": info.st_ino,
                       "sha256": m.sha(raw)}, "fixture_directory_empty": not list(fixture.iterdir()),
        "absent_paths": [name for name in ABSENT_MOUNT_ARTIFACTS if not m.present(OUTPUT / name)],
        "go_log_bytes": (OUTPUT / "go.log").stat().st_size,
        "compile_stdout_bytes": (OUTPUT / "compile.stdout").stat().st_size,
        "compile_stderr_bytes": (OUTPUT / "compile.stderr").stat().st_size}


def validate_terminal(m, states, cgroups, inner_process):
    m.require(set(states) == set(cgroups) == set(UNIT_PINS), "Both exact controller lifetimes must be observed.")
    terminal = {"LoadState": "loaded", "ActiveState": "failed", "SubState": "failed", "MainPID": "0",
                "ExecMainCode": "1", "ExecMainStatus": "1", "Result": "exit-code", "ControlGroup": ""}
    for unit, pins in UNIT_PINS.items():
        expected = dict(terminal, Id=unit, **pins)
        m.require(states[unit] == expected and cgroups[unit] == [],
                  "A reviewed unit invocation changed or still owns processes.")
    m.require(inner_process is None or
              (isinstance(inner_process, dict) and set(inner_process) == set(INNER_PROCESS) and
               inner_process["pid"] == INNER_PROCESS["pid"] and type(inner_process["start_ticks"]) is int and
               inner_process["start_ticks"] > 0 and inner_process["boot_id"] == INNER_PROCESS["boot_id"] and
               inner_process != INNER_PROCESS), "The failed inner process is live or its replacement identity is unproven.")


def public_expected(name):
    return {"oid": PUBLIC_OIDS[name], "owner": "pg_database_owner",
        "acl": ["pg_database_owner=UC/pg_database_owner", "=U/pg_database_owner"],
        "comment": "standard public schema"}


def validate_empty_snapshot(m, pair, snapshot):
    exact_pair(m, pair)
    name = pair["name"]
    m.require(set(snapshot) == {"identity", "public", "catalog_objects", "public_identities", "post_init_objects",
        "local_role_dependencies", "sessions", "casts_sha256", "table_row_counts", "sequence_states"},
        "The empty database observation contains missing or unreviewed fields.")
    m.require(fingerprint(snapshot["identity"]) == IDENTITY_SHA[name] and
        snapshot["public"] == public_expected(name) and snapshot["catalog_objects"] == [] and
        snapshot["public_identities"] == [] and snapshot["post_init_objects"] == [] and
        snapshot["local_role_dependencies"] == [] and
        snapshot["sessions"] == {"activity": [], "prepared": 0, "slots": 0} and
        snapshot["casts_sha256"] == CASTS_SHA and snapshot["table_row_counts"] == {} and
        snapshot["sequence_states"] == {}, "The pair is not the exact reviewed empty database.")


def validate_preservation(m, before, after):
    m.require(before == after and set(before) == set(TREE_SHA),
              "A historical source, failed output, or controller evidence tree changed.")
    for root, entries in before.items():
        m.require(fingerprint(entries) == TREE_SHA[root], "The original root-owned tree is outside the reviewed evidence.")
        m.require(all(row["uid"] == row["gid"] == 0 and not row["mode"] & 0o022
                      for row in entries.values()), "An original evidence entry lost private root ownership.")


def post_init_objects_sql():
    # FirstNormalObjectId is 16384 in the fixed PostgreSQL 17 cluster. These
    # catalogs have no post-init objects in either reviewed empty database.
    return "SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY catalog,oid),'[]'::jsonb) FROM (" + \
        " UNION ALL ".join("SELECT '" + name + "'::text catalog,oid::bigint FROM " + name +
                           " WHERE oid>=16384" for name in OBJECT_CATALOGS) + ") q;"


def sessions_sql(m, pair):
    exact_pair(m, pair)
    name, database = pair["name"], pair["database_oid"]
    return "SELECT jsonb_build_object('activity',(SELECT coalesce(jsonb_agg(jsonb_build_object(" \
        "'pid',pid,'datid',datid,'usesysid',usesysid,'backend_type',backend_type,'state',state," \
        "'backend_start',backend_start) ORDER BY pid),'[]'::jsonb) FROM pg_stat_activity WHERE datid=" + \
        str(database) + "),'prepared',(SELECT count(*) FROM pg_prepared_xacts WHERE database='" + name + \
        "'),'slots',(SELECT count(*) FROM pg_replication_slots WHERE database='" + name + "'));"


def public_identities_sql(m, pair):
    exact_pair(m, pair)
    oid = PUBLIC_OIDS[pair["name"]]
    return "SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY catalog,oid),'[]'::jsonb) FROM (" + \
        " UNION ALL ".join("SELECT '" + catalog + "'::text catalog,oid::bigint FROM " + catalog +
                           " WHERE " + column + "=" + str(oid) for catalog, column in
                           (("pg_class", "relnamespace"), ("pg_proc", "pronamespace"),
                            ("pg_type", "typnamespace"), ("pg_constraint", "connamespace"))) + ") q;"


def observe_empty_pair(m, runner, pair):
    exact_pair(m, pair)
    name, database, role = pair["name"], pair["database_oid"], pair["role_oid"]
    rows = m.pair_rows(name)
    m.validate_pair_rows(pair, rows, TAG)
    m.require_role_isolated(pair)
    sessions = m.decode(m.pg(sessions_sql(m, pair)))
    m.require(sessions == {"activity": [], "prepared": 0, "slots": 0}, "The pair has live or prepared work.")
    casts = runner.cast_inventory(name)
    m.require(casts == pair["casts"], "The reviewed cast inventory changed.")
    snapshot = {"identity": rows, "public": runner.public_identity(name),
        "catalog_objects": runner.original_inspect(pair),
        "public_identities": m.decode(m.pg(public_identities_sql(m, pair), name)),
        "post_init_objects": m.decode(m.pg(post_init_objects_sql(), name)),
        "local_role_dependencies": m.decode(m.pg("SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY classid,objid,objsubid,deptype),'[]'::jsonb) "
            f"FROM pg_shdepend q WHERE dbid={database} AND refclassid='pg_authid'::regclass AND refobjid={role};")),
        "sessions": sessions, "casts_sha256": fingerprint(casts), "table_row_counts": {}, "sequence_states": {}}
    validate_empty_snapshot(m, pair, snapshot)
    m.require(m.pair_rows(name) == rows and m.decode(m.pg(sessions_sql(m, pair))) == sessions,
              "The pair changed while observing its exact empty contents.")
    return snapshot


def root_tree(m, root, excluded=()):
    m.require(root in (SOURCE, OUTPUT, EXECUTION), "Only the three fixed historical trees can be inventoried.")
    result = {}
    for path in [root, *sorted(root.rglob("*"))]:
        if path in excluded:
            continue
        info = m.canonical(path)
        m.require(info.st_uid == info.st_gid == 0 and not info.st_mode & 0o022 and
                  (stat.S_ISDIR(info.st_mode) or stat.S_ISREG(info.st_mode)),
                  "A historical evidence path has unexpected ownership, mode, or type.")
        row = {"device": info.st_dev, "inode": info.st_ino, "uid": info.st_uid, "gid": info.st_gid,
               "mode": stat.S_IMODE(info.st_mode), "kind": "directory" if stat.S_ISDIR(info.st_mode) else "file"}
        if stat.S_ISREG(info.st_mode):
            raw = m.private_read(path, modes=(0o600, 0o644, 0o755, 0o700), limit=64 << 20)
            m.require(len(raw) == info.st_size, "A historical file changed during its snapshot.")
            row.update(bytes=info.st_size, links=info.st_nlink, sha256=m.sha(raw))
        result[str(path.relative_to(root))] = row
    return result


def load_runner():
    for path in reversed((RUNNER, *RUNNER.parents)):
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
            raise RuntimeError("The pinned runner path is not privately controlled.")
    info = RUNNER.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or hashlib.sha256(RUNNER.read_bytes()).hexdigest() != RUNNER_SHA:
        raise RuntimeError("The pinned runner differs from the reviewed source54 executable.")
    spec = importlib.util.spec_from_file_location("source54_disposal_runner", RUNNER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply-reviewed-run", required=True, choices=[RUN])
    parser.parse_args()
    if sys.platform != "linux" or os.geteuid() != 0 or not os.environ.get("SSH_CONNECTION"):
        raise RuntimeError("Run this reviewed operator only through root SSH on test-env.")
    os.umask(0o077)
    m = load_runner()
    m.require(m.WORK == WORK and m.CONTROL == CONTROL and tuple(p["name"] for p in PAIRS) == m.NAMES and
              str(m.SOCKET) == "/var/lib/postgresql/goby-workspace-v1/socket" and
              str(m.PG) == "/usr/lib/postgresql/17/bin", "The runner addresses an unreviewed endpoint.")
    identity, files = m.verify_source(SOURCE, MANIFEST_SHA, 28)
    m.require(files[m.catalog_name(28)] == CATALOG_SHA and
              files["scripts/test-env/run-client-backup-tests.py"] == RUNNER_SHA and
              len(m.read_source_catalog(SOURCE, files, 28)["catalog"]["Tables"]) == 35,
              "The source54 executable or exact schema28 catalog changed.")
    for name, digest in GUARD_SHA.items():
        m.require(m.sha(m.private_read(GUARDS / name)) == digest, "The frozen runner guard evidence changed.")
    workspace = m.load_workspace(SOURCE)
    m.require(workspace.PORT == 15432 and workspace.SOCKET == m.SOCKET, "The workspace endpoint changed.")
    lock = workspace.acquire_lock()
    started = False
    runner = None
    try:
        original = m.private_read(m.RECEIPT)
        m.require(m.sha(original) == RECEIPT_SHA, "The retained failure receipt changed.")
        receipt = m.decode(original)
        validate_receipt(m, receipt)
        m.require(m.directory(OUTPUT) == OUTPUT_IDENTITY, "The failed output directory was replaced.")
        disposal_path = CONTROL / ("client-backup-disposal-" + RUN + ".json")
        m.require(not m.present(disposal_path) and not any(p.name.startswith("disposal-") for p in OUTPUT.iterdir()) and
                  not any(m.present(OUTPUT / (p["name"] + "-objects.json")) for p in PAIRS),
                  "Disposal was already attempted; no operation will be retried.")
        pinned = {"report.json": REPORT_SHA, "go.log": LOG_SHA, "catalog-before.json": CATALOG_BEFORE_SHA,
                  "hba-original": HBA_SHA, "mount-report.json": MOUNT_REPORT_SHA, "mount-request.json": MOUNT_REQUEST_SHA,
                  "mount-worker.py": MOUNT_WORKER_SHA, "run.sh": RUN_SCRIPT_SHA,
                  "compile.stdout": LOG_SHA, "compile.stderr": LOG_SHA}
        for name, digest in pinned.items():
            m.require(m.sha(m.private_read(OUTPUT / name, limit=LOG_BYTES if name == "go.log" else 8 << 20)) == digest,
                      "Original failure evidence changed.")
        m.require(m.sha(m.private_read(EXECUTION / "terminal.json")) == TERMINAL_SHA,
                  "The failed controller's terminal evidence changed.")
        m.require((OUTPUT / "go.log").stat().st_size == LOG_BYTES, "The empty pre-dispatch Go log changed.")
        validate_failed_report(m, m.decode(m.private_read(OUTPUT / "report.json")))
        failure_artifacts = observe_mount_artifacts(m)
        validate_mount_evidence(m, m.decode(m.private_read(EXECUTION / "terminal.json")),
                               m.decode(m.private_read(OUTPUT / "mount-report.json")), failure_artifacts)
        before_catalog = m.decode(m.private_read(OUTPUT / "catalog-before.json"))
        before_trees = {str(root): root_tree(m, root) for root in (SOURCE, OUTPUT, EXECUTION)}
        validate_preservation(m, before_trees, before_trees)
        args = SimpleNamespace(source=SOURCE, manifest_sha256=MANIFEST_SHA, schema=28,
                               mode="targeted", run=RUN_EXPRESSION, package=list(PACKAGES))
        validate_mount_launch(m, args)
        m.validate_arguments(args)
        runner = m.Runner(args)
        runner.run, runner.tag, runner.unit, runner.output = RUN, TAG, UNIT, OUTPUT
        runner.output_identity = OUTPUT_IDENTITY
        runner.module, runner.postgres = workspace, pwd.getpwnam("postgres")
        runner.binary_identity = workspace.binaries()
        runner.cluster_owner_sha, runner.cluster = OWNER_SHA, CLUSTER
        runner.source_identity, runner.source_files = identity, files
        runner.hba_before = m.private_read(OUTPUT / "hba-original")
        runner.pairs = copy.deepcopy(receipt["pairs"])
        m.require(runner.hba_before == workspace.HBA.encode(), "The original HBA is not the persistent baseline.")
        base_cluster_check = runner.check_cluster
        created = {}

        def terminal_evidence():
            states, cgroups = {}, {}
            properties = "Id,Description,LoadState,ActiveState,SubState,MainPID,ExecMainPID,ExecMainCode,ExecMainStatus,InvocationID,Result,ControlGroup"
            for unit in UNIT_PINS:
                raw = m.command(["/usr/bin/systemctl", "show", unit, "--property=" + properties])
                states[unit] = dict(line.split("=", 1) for line in raw.splitlines() if "=" in line)
                root = Path("/sys/fs/cgroup/system.slice") / unit
                cgroups[unit] = []
                if m.present(root):
                    m.require(stat.S_ISDIR(m.canonical(root).st_mode), "The owned cgroup path is not a real directory.")
                    # A terminal top-level group can still hide live descendants.
                    # Inspect every real descendant directory, not only the root.
                    for group in [root, *sorted(path for path in root.rglob("*") if path.is_dir())]:
                        m.require(stat.S_ISDIR(m.canonical(group).st_mode), "A descendant cgroup path changed type.")
                        path = group / "cgroup.procs"
                        m.require(stat.S_ISREG(m.canonical(path).st_mode), "A descendant cgroup lacks a real process file.")
                        cgroups[unit].extend(path.read_text().split())
            process = workspace.process_identity(INNER_PROCESS["pid"]) if m.present(Path("/proc") / str(INNER_PROCESS["pid"])) else None
            validate_terminal(m, states, cgroups, process)
            return {"units": states, "cgroups": cgroups, "original_inner_process_absent": process != INNER_PROCESS}

        def check_cluster(hba=None):
            m.require(m.private_read(m.RECEIPT) == original and m.directory(OUTPUT) == OUTPUT_IDENTITY,
                      "The original receipt or output identity changed during disposal.")
            for name, digest in pinned.items():
                m.require(m.sha(m.private_read(OUTPUT / name, limit=LOG_BYTES if name == "go.log" else 8 << 20)) == digest,
                          "Original failure evidence changed during disposal.")
            m.require(m.sha(m.private_read(EXECUTION / "terminal.json")) == TERMINAL_SHA,
                      "The failed controller's terminal evidence changed during disposal.")
            m.require(observe_mount_artifacts(m) == failure_artifacts,
                      "The compiled helper or pre-dispatch mount artifacts changed during disposal.")
            for path, digest in created.items():
                m.require(m.sha(m.private_read(path)) == digest, "An exclusive disposal evidence file changed.")
            terminal_evidence()
            return base_cluster_check(runner.hba_before)

        runner.check_cluster = check_cluster
        runner.original_inspect = runner.inspect_objects

        def inspect_empty(pair):
            return observe_empty_pair(m, runner, pair)["catalog_objects"]

        runner.inspect_objects = inspect_empty

        def catalog_without_pair():
            current = m.catalog_state()
            for field in ("roles", "databases"):
                current[field] = [row for row in current[field] if row["name"] not in m.NAMES]
            return current

        def check_preservation():
            after = {str(root): root_tree(m, root, created) for root in (SOURCE, OUTPUT, EXECUTION)}
            validate_preservation(m, before_trees, after)
            return after

        def publish(path, value, raw=False):
            allowed_output = path.parent == OUTPUT and (path.name.startswith("disposal-") or
                path.name in [p["name"] + "-objects.json" for p in PAIRS])
            m.require(allowed_output or path == disposal_path, "An evidence destination escaped the fixed disposal scope.")
            encoded = value if raw else json.dumps(value, sort_keys=True, indent=2).encode() + b"\n"
            m.create_private(path, encoded)
            m.sync_directory(path.parent)
            created[path] = m.sha(encoded)
            return created[path]

        check_cluster()
        before_pairs = {pair["name"]: observe_empty_pair(m, runner, pair) for pair in runner.pairs}
        m.require(catalog_without_pair() == before_catalog, "Preexisting cluster metadata changed before disposal.")
        check_preservation()
        report = {"marker": "goby-client-backup-disposal-m3e-v1", "run_id": RUN, "tag": TAG, "status": "reviewed",
            "source_receipt_sha256": RECEIPT_SHA, "failed_report_sha256": REPORT_SHA, "failed_log_sha256": LOG_SHA,
            "failed_controller_terminal_sha256": TERMINAL_SHA,
            "failed_mount_report_sha256": MOUNT_REPORT_SHA, "mount_failure_artifacts": failure_artifacts,
            "namespace_helper_dispatched": False, "actual_mount_recovery_accepted": False,
            "failed_log_bytes": LOG_BYTES, "source_manifest_sha256": MANIFEST_SHA, "schema": 28,
            "catalog_sha256": CATALOG_SHA, "runner_sha256": RUNNER_SHA, "runner_guard_evidence": GUARD_SHA,
            "system_identifier": CLUSTER["system_identifier"], "cluster": CLUSTER, "hba_sha256": HBA_SHA,
            "cluster_owner_sha256": OWNER_SHA, "pairs": PAIRS, "before": before_pairs,
            "terminal_before": terminal_evidence(), "root_tree_sha256": TREE_SHA,
            "operator_sha256": m.sha(Path(__file__).read_bytes()), "removed": []}
        # Even the first partial artifact permanently closes the retry gate.
        started = True
        publish(OUTPUT / "disposal-original-receipt.json", original, raw=True)
        publish(OUTPUT / "disposal-root-ownership-before.json", before_trees)
        publish(OUTPUT / "disposal-catalog-before.json", before_catalog)
        publish(OUTPUT / "disposal-before.json", before_pairs)
        publish(OUTPUT / "disposal-intent.json", report)
        sequence = 0

        def save_progress():
            nonlocal sequence
            check_cluster()
            sequence += 1
            publish(OUTPUT / f"disposal-progress-{sequence:02d}.json", {"run_id": RUN,
                "source_receipt_sha256": RECEIPT_SHA,
                "pairs": [{k: p[k] for k in ("name", "role_oid", "database_oid", "phase")} for p in runner.pairs]})

        # The frozen runner's object evidence creation is also append-only and
        # registered, so preservation never excludes a filename by prefix.
        original_create = m.create_private

        def create_owned_objects(path, value, uid=0, gid=0):
            objects_path = path in [OUTPUT / (p["name"] + "-objects.json") for p in PAIRS]
            if objects_path:
                m.require(value == b"[]" and uid == gid == 0, "A pair's removal evidence is not empty and privately owned.")
            original_create(path, value, uid, gid)
            if objects_path:
                m.sync_directory(OUTPUT)
                created[path] = m.sha(value)

        m.create_private = create_owned_objects
        runner.save = save_progress
        for pair in reversed(runner.pairs):
            check_cluster()
            check_preservation()
            m.require(observe_empty_pair(m, runner, pair) == before_pairs[pair["name"]], "The empty pair changed before removal.")
            runner.remove_pair(pair)
            exact_pair(m, pair, "removed")
            report["removed"].append({k: pair[k] for k in ("name", "role_oid", "database_oid", "phase")})
        check_cluster()
        m.require(all(m.pair_rows(p["name"]) == {"role": None, "database": None} for p in PAIRS),
                  "A reviewed pair identity survived disposal.")
        after_catalog = m.catalog_state()
        m.require(after_catalog == before_catalog, "Preexisting catalog metadata did not return to the matching snapshot.")
        after_trees = check_preservation()
        publish(OUTPUT / "disposal-catalog-after.json", after_catalog)
        publish(OUTPUT / "disposal-root-ownership-after.json", after_trees)
        report.update(status="disposed", preexisting_catalog_unchanged=True, after=after_catalog,
            root_ownership_and_historical_trees_unchanged=True, original_failure_evidence_preserved=True,
            live_failure_receipt_preserved=True, force_or_backend_termination_used=False, normalization_used=False,
            terminal_after=terminal_evidence(), pair_identities_absent=True,
            receipt_transition="Retained bytes preserved; the fresh runner must validate the separate disposal attestation.")
        report_path = OUTPUT / "disposal-report.json"
        digest = publish(report_path, report)
        attestation = {"marker": report["marker"], "status": "disposed", "run_id": RUN, "tag": TAG,
            "source_receipt_sha256": RECEIPT_SHA, "pairs": PAIRS, "system_identifier": CLUSTER["system_identifier"],
            "hba_sha256": HBA_SHA, "report_path": str(report_path), "report_sha256": digest}
        m.validate_disposal(receipt, original, attestation, HBA_SHA)
        check_cluster()
        check_preservation()
        m.require(m.catalog_state() == before_catalog and
                  all(m.pair_rows(p["name"]) == {"role": None, "database": None} for p in PAIRS),
                  "The final catalog or absent pair identities changed before attestation.")
        # The control attestation is the final commit point. No fallible guard
        # follows it: a failed post-commit check must never leave usable success.
        publish(disposal_path, attestation)
        return {"status": "disposed", "run_id": RUN, "report_sha256": digest, "removed": report["removed"],
                "live_failure_receipt_sha256": RECEIPT_SHA}
    except Exception as error:
        if started:
            failure = {"run_id": RUN, "status": "failed", "error_type": type(error).__name__,
                "source_receipt_sha256": RECEIPT_SHA,
                "pairs": [{k: p[k] for k in ("name", "role_oid", "database_oid", "phase")} for p in runner.pairs]}
            try:
                m.create_private(OUTPUT / "disposal-failed.json", json.dumps(failure, sort_keys=True).encode() + b"\n")
                m.sync_directory(OUTPUT)
            except Exception:
                pass
        # Controlled failures disclose a guard message, never SQL output or credentials.
        raise RuntimeError("Source54 disposal stopped; private evidence is retained and retry is forbidden.") from None
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN)
        os.close(lock)


if __name__ == "__main__":
    # Output failure after a successful commit cannot create a failure artifact.
    print(json.dumps(main()))
