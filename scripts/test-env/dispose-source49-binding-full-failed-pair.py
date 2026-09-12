#!/usr/bin/env python3
"""Dispose only the reviewed, empty source49 binding full-regression pair.

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
RUN = "20260912_042841_89ef5954034a"
WORK = Path("/opt/goby-test/exec-work-m3e")
SOURCE = WORK / "source-attempt-49"
OUTPUT = WORK / ("client-backup-run-" + RUN)
EXECUTION = WORK / "storage-binding-schema28-full-execution-01"
CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
RUNNER = SOURCE / "scripts/test-env/run-client-backup-tests.py"
GUARDS = WORK / "schema28-runner-guard-01"
RUNNER_SHA = "4c76cfa22fd1915a270756d82e313ce6e7bbf3a5bcc857339fd6d254f5c894a2"
MANIFEST_SHA = "a22a89b460a1bf5d4033336ea57fd7a8e69975c01bf883ad54fccf1cd5c6ee3a"
RECEIPT_SHA = "b5a891eb63d1d7adae6e841a4162b408bf9e1e183b8ddc0773404c38e9dfc684"
REPORT_SHA = "4d61ae8737438ccf03a11b43983898cb23a60a0f3ec2b64781b7ea1fc49f03be"
LOG_SHA = "138e48ed7d890e3bd26203a31fdec8220c8a49d7d559d4846f96536d5a5d0f5c"
LOG_BYTES = 9406631
TERMINAL_SHA = "a6dde10cb27f1408a28faaaf12c7d0904f6fa72758bbaf68fe8777a808db92c5"
CATALOG_BEFORE_SHA = "ce62d3df80d4eb2b1595d712eeb30df38a9521c285722b2ca335daf90bb5d376"
CATALOG_SHA = "8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b"
HBA_SHA = "6571fe86239d7bec6f5e9de9ebefbeda56f0e7df43cdbc58a3dc064e41e958fd"
OWNER_SHA = "610b3dcdafa8b6bb960a26946c398b98c02357d61c9c7fdf9b5a8b8504b9d5af"
CLUSTER = {"system_identifier": "7684040109719526738", "process": {
    "pid": 327173, "start_ticks": 289359, "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"}}
PAIRS = [{"name": "goby_backup_m3e_source", "role_oid": 16410704, "database_oid": 16410705},
         {"name": "goby_backup_m3e_target", "role_oid": 16410706, "database_oid": 16410707}]
OUTPUT_IDENTITY = {"device": 2049, "inode": 2504671}
TAG = "goby-client-backup-pair-m3e-v1:" + RUN
UNIT = "goby-client-backup-20260912-042841-89ef5954034a.service"
OUTER_UNIT = "goby-storage-binding-schema28-full-controller-v1.service"
INNER_PROCESS = {"pid": 1252659, "start_ticks": 11018776,
                 "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"}
RUN_EXPRESSION = ""
PACKAGES = ()
UNIT_PINS = {
    UNIT: {"InvocationID": "f9cf1ffaf283452ebf58d23e2b1a6b8c", "ExecMainPID": "1252659", "Description": TAG},
    OUTER_UNIT: {"InvocationID": "769e7b698560419d906a7df1838f8781", "ExecMainPID": "1252471",
        "Description": "[systemd-run] /usr/bin/python3 -I -B " + str(SOURCE) +
        "/scripts/test-env/run-client-backup-tests.py --source " + str(SOURCE) +
        " --manifest-sha256 " + MANIFEST_SHA + " --schema 28 --mode full"},
}
GUARD_SHA = {
    "stdout": "9ecb9d07c61c61b7510c8ed16e895ad2c146d0244920914906663d1d40703ea9",
    "stderr": "adcceb7f042242fdd8375aae0fbd1334ce7179489e307babca2d44babde86aac",
    "report.json": "ba27c2c539e5f18020db811c31d82e56b1e528e456d0a66e59baf0f1f9c8f6d3",
}
TREE_SHA = {
    str(SOURCE): "a18e3df758f17774cdebb1fe07ba2bbe1693180358cb833fac01b29fc9478c89",
    str(OUTPUT): "4eb53dc7ba188b3342a898a30a140c17cb903658394f2c68f8ea2b8f1ee08ea8",
    str(EXECUTION): "c8431a96d3291935732c7f96d8ad16786a0ae1245963e292fd97723ffc7d7f0e",
}
PUBLIC_OIDS = {"goby_backup_m3e_source": 2200, "goby_backup_m3e_target": 17186575}
IDENTITY_SHA = {
    "goby_backup_m3e_source": "348c359b8daa7f6d7172db09995612174920bf2f518aa33341d5c99468eea946",
    "goby_backup_m3e_target": "79246172f5c5944dd06a2a06d851861f8d253e173d3412886de4e34809903e99",
}
CASTS_SHA = "571c35d40ff27b9972b3275e878fac303ec17df99663c72a04801f9c973926ef"
OBJECT_CATALOGS = ("pg_class", "pg_proc", "pg_type", "pg_constraint", "pg_trigger", "pg_attrdef",
    "pg_collation", "pg_conversion", "pg_operator", "pg_opclass", "pg_opfamily", "pg_ts_config",
    "pg_ts_dict", "pg_ts_parser", "pg_ts_template", "pg_rewrite", "pg_extension", "pg_language",
    "pg_am", "pg_amop", "pg_amproc")
FAILED_TESTS = ("TestRecoveryManagerSchema23EncryptedArchiveApplyRestartAndRollback",
               "TestRecoveryManagerSchema24EncryptedArchiveApplyRestartAndRollback")
PACKAGE_RESULTS = (
    ("github.com/moooyo/goby/cmd/goby", "pass"),
    ("github.com/moooyo/goby/internal/activity", "pass"),
    ("github.com/moooyo/goby/internal/artwork", "pass"),
    ("github.com/moooyo/goby/internal/backupformat", "pass"),
    ("github.com/moooyo/goby/internal/backuppg", "pass"),
    ("github.com/moooyo/goby/internal/backupstore", "pass"),
    ("github.com/moooyo/goby/internal/config", "pass"),
    ("github.com/moooyo/goby/internal/database", "pass"),
    ("github.com/moooyo/goby/internal/diagnostics", "pass"),
    ("github.com/moooyo/goby/internal/events", "pass"),
    ("github.com/moooyo/goby/internal/identity", "pass"),
    ("github.com/moooyo/goby/internal/library", "pass"),
    ("github.com/moooyo/goby/internal/lifecycle", "pass"),
    ("github.com/moooyo/goby/internal/media", "pass"),
    ("github.com/moooyo/goby/internal/metadata", "pass"),
    ("github.com/moooyo/goby/internal/playback", "pass"),
    ("github.com/moooyo/goby/internal/recovery", "fail"),
    ("github.com/moooyo/goby/internal/recoverycontrol", "pass"),
    ("github.com/moooyo/goby/internal/server", "pass"),
    ("github.com/moooyo/goby/internal/settings", "pass"),
    ("github.com/moooyo/goby/internal/storagebinding", "pass"),
    ("github.com/moooyo/goby/internal/subtitle", "pass"),
    ("github.com/moooyo/goby/internal/tasks", "pass"),
    ("github.com/moooyo/goby/internal/transcode", "pass"),
)


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
        "schema": 28, "catalog_sha256": CATALOG_SHA, "mode": "full", "phase": "retained",
        "cleanup_complete": False, "cluster": CLUSTER, "cluster_owner_sha256": OWNER_SHA,
        "output_identity": OUTPUT_IDENTITY}
    m.require(set(receipt) == set(expected) | {"pairs"} and
              all(receipt.get(key) == value for key, value in expected.items()) and
              type(receipt.get("schema")) is int and receipt.get("cleanup_complete") is False,
              "The failure receipt is outside this exact reviewed run.")
    m.require([exact_pair(m, pair) for pair in receipt["pairs"]] == PAIRS,
              "The original pair order or identity changed.")


def validate_failed_report(m, failed):
    expected = {"marker": "goby-client-backup-pair-m3e-v1", "status": "failed", "run_id": RUN,
        "source": str(SOURCE), "source_manifest_sha256": MANIFEST_SHA, "schema": 28, "mode": "full",
        "catalog_sha256": CATALOG_SHA, "unit": UNIT, "unit_exit": 1, "unit_process": INNER_PROCESS,
        "cluster": CLUSTER, "pair_evidence_retained": True, "hba_before_sha256": HBA_SHA,
        "hba_after_sha256": HBA_SHA,
        "error": "The verified unit failed or was never observed running.",
        "cleanup": {"hba_restored_exactly": True, "receipt_saved": True, "unit_terminal": True}}
    m.require(set(failed) == set(expected) | {"tools"} and
              all(failed.get(key) == value for key, value in expected.items()) and
              failed.get("pair_evidence_retained") is True and type(failed.get("unit_exit")) is int,
              "The report does not prove this terminal failed run with restored HBA.")


def validate_full_launch(m, args):
    m.require(args.source == SOURCE and args.manifest_sha256 == MANIFEST_SHA and
              type(args.schema) is int and args.schema == 28 and args.mode == "full" and
              args.run == "" and args.package == [],
              "This full-run disposal cannot inherit targeted selectors or another source.")


def full_log_summary(m, log):
    m.require(isinstance(log, bytes) and log, "The complete full-run log is not a byte record.")
    events = [m.decode(line) for line in log.splitlines() if line.startswith(b"{")]
    m.require(events and all(isinstance(event, dict) for event in events), "The final Go stream is malformed or empty.")
    top = [event for event in events if isinstance(event.get("Test"), str) and event["Test"] and
           "/" not in event["Test"] and event.get("Action") in ("pass", "fail", "skip")]
    packages = [{"package": event.get("Package"), "status": event["Action"]} for event in events
                if not event.get("Test") and event.get("Action") in ("pass", "fail", "skip")]
    m.require(all(isinstance(row["package"], str) and row["package"] for row in packages),
              "A completed package has no exact source identity.")
    return {"counts": {action: len([event for event in top if event["Action"] == action])
                       for action in ("pass", "fail", "skip")},
            "failed_tests": sorted(event["Test"] for event in top if event["Action"] == "fail"),
            "packages": sorted(packages, key=lambda row: row["package"]),
            "recoverydb_executed": any(event.get("Package") == "github.com/moooyo/goby/internal/recoverydb" for event in events),
            "race_warning": b"WARNING: DATA RACE" in log,
            "unique_top_level": len(top) == len({(event.get("Package"), event["Test"]) for event in top})}


def validate_full_evidence(m, terminal, summary, binary_exists):
    packages = [{"package": name, "status": status} for name, status in PACKAGE_RESULTS]
    expected = {"counts": {"pass": 2105, "fail": 2, "skip": 0}, "failed_tests": list(FAILED_TESTS),
                "packages": packages, "recoverydb_executed": False, "race_warning": False, "unique_top_level": True}
    m.require(summary == expected and all(type(value) is int for value in summary["counts"].values()) and
              summary["recoverydb_executed"] is False and summary["race_warning"] is False and
              summary["unique_top_level"] is True,
              "The final full log differs from 2105 passes, two exact recovery failures and the unexecuted recoverydb step.")
    state = {"MainPID": "0", "Result": "exit-code", "ExecMainStatus": "1", "ControlGroup": "", "SubState": "failed"}
    m.require(terminal.get("marker") == "goby-source49-schema28-full-failed-terminal-v1" and
              terminal.get("unit") == OUTER_UNIT and terminal.get("inner_unit") == UNIT and
              terminal.get("state") == dict(state, InvocationID=UNIT_PINS[OUTER_UNIT]["InvocationID"],
                                           Description=UNIT_PINS[OUTER_UNIT]["Description"]) and
              terminal.get("inner_state") == dict(state, InvocationID=UNIT_PINS[UNIT]["InvocationID"], Description=TAG) and
              terminal.get("report_sha256") == REPORT_SHA and terminal.get("go_log_sha256") == LOG_SHA and
              type(terminal.get("go_log_bytes")) is int and terminal["go_log_bytes"] == LOG_BYTES and
              terminal.get("counts") == {"pass": 2105, "fail": 2} and
              all(type(value) is int for value in terminal["counts"].values()) and
              terminal.get("failed_tests") == list(FAILED_TESTS) and terminal.get("packages") == packages and
              terminal.get("recursive_cgroup_empty") is True and terminal.get("pair_retained") is True and
              terminal.get("recoverydb_executed") is False and terminal.get("linux_build_artifact_exists") is False and
              binary_exists is False,
              "The sealed full terminal, retained-pair claim or actual absent build artifact changed.")


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
        raise RuntimeError("The pinned runner differs from the reviewed source49 executable.")
    spec = importlib.util.spec_from_file_location("source49_disposal_runner", RUNNER)
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
              "The source49 executable or exact schema28 catalog changed.")
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
                  "hba-original": HBA_SHA}
        for name, digest in pinned.items():
            m.require(m.sha(m.private_read(OUTPUT / name, limit=LOG_BYTES if name == "go.log" else 8 << 20)) == digest,
                      "Original failure evidence changed.")
        m.require(m.sha(m.private_read(EXECUTION / "terminal.json")) == TERMINAL_SHA,
                  "The failed controller's terminal evidence changed.")
        m.require((OUTPUT / "go.log").stat().st_size == LOG_BYTES, "The complete final Go log changed.")
        validate_failed_report(m, m.decode(m.private_read(OUTPUT / "report.json")))
        failure_summary = full_log_summary(m, m.private_read(OUTPUT / "go.log", limit=LOG_BYTES))
        validate_full_evidence(m, m.decode(m.private_read(EXECUTION / "terminal.json")), failure_summary,
                               m.present(OUTPUT / "tmp/goby-linux-amd64"))
        before_catalog = m.decode(m.private_read(OUTPUT / "catalog-before.json"))
        before_trees = {str(root): root_tree(m, root) for root in (SOURCE, OUTPUT, EXECUTION)}
        validate_preservation(m, before_trees, before_trees)
        args = SimpleNamespace(source=SOURCE, manifest_sha256=MANIFEST_SHA, schema=28,
                               mode="full", run=RUN_EXPRESSION, package=list(PACKAGES))
        validate_full_launch(m, args)
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
            m.require(not m.present(OUTPUT / "tmp/goby-linux-amd64"), "An unexpected build artifact appeared after the full failure.")
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
            "full_failure_summary": failure_summary, "recoverydb_executed": False, "linux_build_artifact_exists": False,
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
        raise RuntimeError("Source49 disposal stopped; private evidence is retained and retry is forbidden.") from None
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN)
        os.close(lock)


if __name__ == "__main__":
    # Output failure after a successful commit cannot create a failure artifact.
    print(json.dumps(main()))
