#!/usr/bin/env python3
"""Dispose only the independently reviewed, empty source24 Theme test pair.

This one-run operator reuses the pinned backup runner's removal guards. It
never changes the failed report, original receipt, logs, credentials, or HBA.
Any prior disposal artifact prevents a retry and requires a new review.
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
RUN = "20260911_101731_c0db70c07036"
WORK = Path("/opt/goby-test/exec-work-m3e")
SOURCE = WORK / "source-attempt-24"
OUTPUT = WORK / ("client-backup-run-" + RUN)
CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
RUNNER = SOURCE / "scripts/test-env/run-client-backup-tests.py"
RUNNER_SHA = "daea1d1685c073f37218b92c55ea7fa70bfe55853ee778650b4511b5b292b8b8"
MANIFEST_SHA = "3df8ab5eb5f9d641de302e457fa0fe5a61c6fbe681223929e087fc95c9bfeffc"
RECEIPT_SHA = "4b91d09d5a77df1a58fb8ae6da9263c5247162e61a8f543b7be3f30c6badb7a0"
REPORT_SHA = "7b2f2bbe44bc3bf4ac52cf4be58c7d1cea1b3135a1c207b42db5610df76bf474"
LOG_SHA = "bd9e75a467342b2e54b0304a9f6f81960ab71e61bb11f20d0489737185702590"
CATALOG_BEFORE_SHA = "ce62d3df80d4eb2b1595d712eeb30df38a9521c285722b2ca335daf90bb5d376"
HBA_SHA = "6571fe86239d7bec6f5e9de9ebefbeda56f0e7df43cdbc58a3dc064e41e958fd"
OWNER_SHA = "610b3dcdafa8b6bb960a26946c398b98c02357d61c9c7fdf9b5a8b8504b9d5af"
CLUSTER = {"system_identifier": "7684040109719526738", "process": {
    "pid": 327173, "start_ticks": 289359, "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"}}
PAIRS = [{"name": "goby_backup_m3e_source", "role_oid": 5166279, "database_oid": 5166280},
         {"name": "goby_backup_m3e_target", "role_oid": 5166281, "database_oid": 5166282}]
OUTPUT_IDENTITY = {"device": 2049, "inode": 3160515}


def load_runner():
    # Check the fixed executable source before importing any of its code.
    for path in reversed((RUNNER, *RUNNER.parents)):
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
            raise RuntimeError("The pinned runner path is not privately controlled.")
    info = RUNNER.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or hashlib.sha256(RUNNER.read_bytes()).hexdigest() != RUNNER_SHA:
        raise RuntimeError("The pinned runner differs from the reviewed source24 input.")
    spec = importlib.util.spec_from_file_location("source24_disposal_runner", RUNNER)
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
    m.require(m.WORK == WORK and m.CONTROL == CONTROL and tuple(p["name"] for p in PAIRS) == m.NAMES,
              "The runner does not address the reviewed workspace and pair.")
    identity, files = m.verify_source(SOURCE, MANIFEST_SHA, 26)
    workspace = m.load_workspace(SOURCE)
    lock = workspace.acquire_lock()
    started = False
    runner = None
    try:
        original = m.private_read(m.RECEIPT)
        m.require(m.sha(original) == RECEIPT_SHA, "The retained failure receipt changed.")
        receipt = m.decode(original)
        tag = m.MARKER + ":" + RUN
        unit = "goby-client-backup-" + RUN.replace("_", "-") + ".service"
        m.require(receipt["marker"] == m.MARKER and receipt["run_id"] == RUN and receipt["tag"] == tag and
                  receipt["unit"] == unit and receipt["source"] == str(SOURCE) and receipt["output"] == str(OUTPUT) and
                  receipt["source_manifest_sha256"] == MANIFEST_SHA and type(receipt["schema"]) is int and receipt["schema"] == 26 and
                  receipt["mode"] == "targeted" and receipt["phase"] == "retained" and
                  receipt["cleanup_complete"] is False and receipt["cluster"] == CLUSTER and
                  receipt["cluster_owner_sha256"] == OWNER_SHA and receipt["output_identity"] == OUTPUT_IDENTITY,
                  "The failure receipt is outside this exact reviewed run.")
        m.require([{key: p[key] for key in ("name", "role_oid", "database_oid")} for p in receipt["pairs"]] == PAIRS and
                  all(p["phase"] == "owned" for p in receipt["pairs"]), "The reviewed pair identities changed.")
        m.require(m.directory(OUTPUT) == OUTPUT_IDENTITY, "The failed output directory was replaced.")
        disposal_path = CONTROL / ("client-backup-disposal-" + RUN + ".json")
        m.require(not m.present(disposal_path) and not any(p.name.startswith("disposal-") for p in OUTPUT.iterdir()) and
                  not any(m.present(OUTPUT / (p["name"] + "-objects.json")) for p in PAIRS),
                  "Disposal was already attempted; no operation will be retried.")
        pinned = {"report.json": REPORT_SHA, "go.log": LOG_SHA, "catalog-before.json": CATALOG_BEFORE_SHA,
                  "hba-original": HBA_SHA}
        for name, digest in pinned.items():
            m.require(m.sha(m.private_read(OUTPUT / name)) == digest, "Original failure evidence changed.")
        failed = m.decode(m.private_read(OUTPUT / "report.json"))
        m.require(failed["status"] == "failed" and failed["run_id"] == RUN and failed["unit_exit"] == 1 and
                  failed["pair_evidence_retained"] is True and failed["hba_after_sha256"] == HBA_SHA and
                  failed["cleanup"] == {"hba_restored_exactly": True, "receipt_saved": True, "unit_terminal": True},
                  "The failure report no longer proves a terminal run with restored HBA.")
        before_catalog = m.decode(m.private_read(OUTPUT / "catalog-before.json"))
        args = SimpleNamespace(source=SOURCE, manifest_sha256=MANIFEST_SHA, schema=26, mode="targeted", run="", package=[])
        runner = m.Runner(args)
        runner.run, runner.tag, runner.unit, runner.output = RUN, tag, unit, OUTPUT
        runner.output_identity = OUTPUT_IDENTITY
        runner.module, runner.postgres = workspace, pwd.getpwnam("postgres")
        runner.binary_identity = workspace.binaries()
        runner.cluster_owner_sha, runner.cluster = OWNER_SHA, CLUSTER
        runner.source_identity, runner.source_files = identity, files
        runner.hba_before = m.private_read(OUTPUT / "hba-original")
        runner.pairs = copy.deepcopy(receipt["pairs"])
        m.require(runner.hba_before == workspace.HBA.encode(), "The original HBA is not the persistent baseline.")
        base_cluster_check = runner.check_cluster

        def check_cluster(hba=None):
            m.require(m.private_read(m.RECEIPT) == original and m.directory(OUTPUT) == OUTPUT_IDENTITY,
                      "The original receipt or output identity changed during disposal.")
            for name, digest in pinned.items():
                m.require(m.sha(m.private_read(OUTPUT / name)) == digest, "Original failure evidence changed during disposal.")
            m.require_unit_terminal(m.unit_state(unit), unit, tag)
            return base_cluster_check(runner.hba_before)

        runner.check_cluster = check_cluster
        original_inspect = runner.inspect_objects

        def inspect_empty(pair):
            objects = original_inspect(pair)
            m.require(objects == [], "This disposal is authorized only for the reviewed empty pair.")
            return objects

        runner.inspect_objects = inspect_empty

        def inspect_pair(pair):
            rows = m.pair_rows(pair["name"])
            m.validate_pair_rows(pair, rows, tag)
            m.require_role_isolated(pair)
            name, oid = pair["name"], pair["database_oid"]
            m.require(m.pg(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={oid})+"
                           f"(SELECT count(*) FROM pg_prepared_xacts WHERE database='{name}')+"
                           f"(SELECT count(*) FROM pg_replication_slots WHERE database='{name}');") == "0",
                      "The reviewed database has active or prepared work.")
            m.require(runner.public_identity(name) == pair["public"] and runner.cast_inventory(name) == pair["casts"],
                      "The reviewed empty public schema or cast inventory changed.")
            inspect_empty(pair)
            return {"identity": rows, "public": pair["public"], "catalog_objects": [], "table_row_counts": {},
                    "connections_prepared_slots": 0, "external_role_dependencies": 0, "casts_unchanged": True}

        def catalog_without_reviewed_pair():
            current = m.catalog_state()
            for field in ("roles", "databases"):
                current[field] = [row for row in current[field] if row["name"] not in m.NAMES]
            return current

        check_cluster()
        before_pairs = {pair["name"]: inspect_pair(pair) for pair in runner.pairs}
        m.require(catalog_without_reviewed_pair() == before_catalog, "Preexisting cluster metadata changed.")
        report = {"marker": "goby-client-backup-disposal-m3e-v1", "run_id": RUN, "tag": tag,
                  "status": "reviewed", "source_receipt_sha256": RECEIPT_SHA, "failed_report_sha256": REPORT_SHA,
                  "failed_log_sha256": LOG_SHA, "source_manifest_sha256": MANIFEST_SHA,
                  "system_identifier": CLUSTER["system_identifier"], "cluster": CLUSTER,
                  "hba_sha256": HBA_SHA, "pairs": PAIRS, "before": before_pairs,
                  "operator_sha256": m.sha(Path(__file__).read_bytes()), "removed": []}

        def publish(path, value):
            raw = json.dumps(value, sort_keys=True, indent=2).encode() + b"\n"
            m.create_private(path, raw)
            m.sync_directory(path.parent)
            return m.sha(raw)

        publish(OUTPUT / "disposal-intent.json", report)
        started = True
        sequence = 0

        def save_progress():
            # Retain the original failure receipt; only append disposal evidence.
            nonlocal sequence
            check_cluster()
            sequence += 1
            publish(OUTPUT / f"disposal-progress-{sequence:02d}.json",
                    {"run_id": RUN, "source_receipt_sha256": RECEIPT_SHA,
                     "pairs": [{k: p[k] for k in ("name", "role_oid", "database_oid", "phase")} for p in runner.pairs]})

        runner.save = save_progress
        for pair in reversed(runner.pairs):
            check_cluster()
            inspect_pair(pair)
            runner.remove_pair(pair)
            report["removed"].append({k: pair[k] for k in ("name", "role_oid", "database_oid", "phase")})
        check_cluster()
        m.require(all(m.pair_rows(p["name"]) == {"role": None, "database": None} for p in PAIRS),
                  "A reviewed pair identity survived disposal.")
        after = m.catalog_state()
        m.require(after == before_catalog, "Preexisting cluster metadata changed after disposal.")
        report.update(status="disposed", preexisting_catalog_unchanged=True, after=after,
                      original_failure_evidence_preserved=True, force_or_backend_termination_used=False)
        report_path = OUTPUT / "disposal-report.json"
        digest = publish(report_path, report)
        attestation = {"marker": report["marker"], "status": "disposed", "run_id": RUN, "tag": tag,
                       "source_receipt_sha256": RECEIPT_SHA, "pairs": PAIRS,
                       "system_identifier": CLUSTER["system_identifier"], "hba_sha256": HBA_SHA,
                       "report_path": str(report_path), "report_sha256": digest}
        m.validate_disposal(receipt, original, attestation, HBA_SHA)
        publish(disposal_path, attestation)
        print(json.dumps({"status": "disposed", "run_id": RUN, "report_sha256": digest, "removed": report["removed"]}))
    except Exception as error:
        if started:
            # A failure is evidence, never permission to retry, reopen, or clean up.
            failure = {"run_id": RUN, "status": "failed", "error_type": type(error).__name__,
                       "source_receipt_sha256": RECEIPT_SHA,
                       "pairs": [{k: p[k] for k in ("name", "role_oid", "database_oid", "phase")} for p in runner.pairs]}
            try:
                m.create_private(OUTPUT / "disposal-failed.json", json.dumps(failure, sort_keys=True).encode() + b"\n")
                m.sync_directory(OUTPUT)
            except Exception:
                pass
        raise
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN)
        os.close(lock)


if __name__ == "__main__":
    main()
