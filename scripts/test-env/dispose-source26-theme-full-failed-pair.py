#!/usr/bin/env python3
"""Dispose only the independently reviewed, retained source26 Theme test pair.

This one-run operator reuses the pinned backup runner's removal guards. It
never changes the failed report, receipt, logs, credentials, HBA, or retained dumps.
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
import re
import stat
import sys
from types import SimpleNamespace

sys.dont_write_bytecode = True
RUN = "20260911_105904_88799d500ae6"
WORK = Path("/opt/goby-test/exec-work-m3e")
SOURCE = WORK / "source-attempt-26"
OUTPUT = WORK / ("client-backup-run-" + RUN)
CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
RUNNER = SOURCE / "scripts/test-env/run-client-backup-tests.py"
RUNNER_SHA = "daea1d1685c073f37218b92c55ea7fa70bfe55853ee778650b4511b5b292b8b8"
MANIFEST_SHA = "df27faddb9864a9fa7fce00112c373261c8c4f08f32cfacaaf0e7bcbb72783a1"
RECEIPT_SHA = "51eb6ea2d8de96e6b92f4ed08a807740002cea42aa65731c75d83535d56e5f58"
REPORT_SHA = "5222548cda4709640e3ca8cfa6c50310a370ebf9f80ca5499a7fadabd48aef9a"
LOG_SHA = "5680fba9b090577d8990eda0ec746c300c60b85f75137eafaa833c098e53c6cb"
CATALOG_BEFORE_SHA = "ce62d3df80d4eb2b1595d712eeb30df38a9521c285722b2ca335daf90bb5d376"
HBA_SHA = "6571fe86239d7bec6f5e9de9ebefbeda56f0e7df43cdbc58a3dc064e41e958fd"
OWNER_SHA = "610b3dcdafa8b6bb960a26946c398b98c02357d61c9c7fdf9b5a8b8504b9d5af"
CLUSTER = {"system_identifier": "7684040109719526738", "process": {
    "pid": 327173, "start_ticks": 289359, "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"}}
PAIRS = [{"name": "goby_backup_m3e_source", "role_oid": 6150502, "database_oid": 6150503},
         {"name": "goby_backup_m3e_target", "role_oid": 6150504, "database_oid": 6150505}]
OUTPUT_IDENTITY = {"device": 2049, "inode": 3160563}
OBSERVED_PUBLIC = {
    "goby_backup_m3e_source": {"oid": 2200, "owner": "pg_database_owner",
        "acl": ["pg_database_owner=UC/pg_database_owner", "=U/pg_database_owner"],
        "comment": "standard public schema"},
    "goby_backup_m3e_target": {"oid": 7066578, "owner": "pg_database_owner",
        "acl": ["pg_database_owner=UC/pg_database_owner", "=U/pg_database_owner"],
        "comment": "standard public schema"},
}
EVIDENCE = WORK / "source26-retained-database-evidence-01"
OBSERVATIONS = EVIDENCE / "observations.json"
OBSERVATIONS_SHA = "b145cd7ab9f609bb9f8bf7448e2303bdd1384e65657f216496360d1eb85f2291"
OBSERVATIONS_BYTES = 536956
DUMPS = {
    "goby_backup_m3e_source": {"path": str(EVIDENCE / "goby_backup_m3e_source.dump"), "bytes": 281822,
        "sha256": "c6706cc9860126edfce2433991f785b3ca715683ab5a56d8b8d058c12d3b2661"},
    "goby_backup_m3e_target": {"path": str(EVIDENCE / "goby_backup_m3e_target.dump"), "bytes": 2812,
        "sha256": "270e8c8c4d2e6d0d45a7c792eeee0e1a6270077327417055add48d427b866b43"},
}
USERS_ACL = ["goby_backup_m3e_source=arwdDxtm/goby_backup_m3e_source"]
RELATIONS_SQL = "SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY oid),'[]'::jsonb) FROM " \
    "(SELECT c.oid::bigint,c.relname,c.relkind,c.relowner::bigint,c.relacl FROM pg_class c " \
    "JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public') x;"


def load_runner():
    # Check the fixed executable source before importing any of its code.
    for path in reversed((RUNNER, *RUNNER.parents)):
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
            raise RuntimeError("The pinned runner path is not privately controlled.")
    info = RUNNER.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or hashlib.sha256(RUNNER.read_bytes()).hexdigest() != RUNNER_SHA:
        raise RuntimeError("The pinned runner differs from the reviewed source26 input.")
    spec = importlib.util.spec_from_file_location("source26_full_disposal_runner", RUNNER)
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
                  receipt["mode"] == "full" and receipt["phase"] == "retained" and
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
        evidence_identity = m.directory(EVIDENCE)

        def check_retained_evidence():
            m.require(m.directory(EVIDENCE) == evidence_identity, "The retained database evidence directory changed.")
            raw = m.private_read(OBSERVATIONS, limit=1 << 20)
            m.require(len(raw) == OBSERVATIONS_BYTES and m.sha(raw) == OBSERVATIONS_SHA,
                      "The retained database observation changed.")
            for dump in DUMPS.values():
                content = m.private_read(Path(dump["path"]), limit=dump["bytes"])
                m.require(len(content) == dump["bytes"] and m.sha(content) == dump["sha256"],
                          "A retained custom database dump changed.")
            return m.decode(raw)

        observations = check_retained_evidence()
        m.require(set(observations) == {"marker", "run_id", "receipt_sha256", "source_manifest_sha256", "before",
                                       "dumps", "database_mutations", "after_equal"} and
                  observations["marker"] == "goby-source26-retained-evidence-v1" and observations["run_id"] == RUN and
                  observations["receipt_sha256"] == RECEIPT_SHA and observations["source_manifest_sha256"] == MANIFEST_SHA and
                  observations["dumps"] == DUMPS and observations["database_mutations"] is False and
                  observations["after_equal"] is True and set(observations["before"]) == set(m.NAMES),
                  "The database observation does not describe this exact reviewed run.")
        baseline = m.read_source_catalog(SOURCE, files, 26)
        source_observed = observations["before"][PAIRS[0]["name"]]
        target_observed = observations["before"][PAIRS[1]["name"]]
        tables = {table["Name"] for table in baseline["catalog"]["Tables"]}
        sequences = {sequence["Name"] for sequence in baseline["catalog"]["Sequences"]}
        m.require(len(tables) == 33 and source_observed["catalog_objects"] == baseline["objects"] and
                  set(source_observed["table_data"]) == tables and set(source_observed["sequences"]) == sequences and
                  len(source_observed["relations"]) == 138 and
                  {row["relname"] for row in source_observed["relations"] if row["relkind"] == "r"} == tables and
                  {row["relname"] for row in source_observed["relations"] if row["relkind"] == "S"} == sequences and
                  [row for row in source_observed["relations"] if row["relacl"] is not None] ==
                  [{"oid": 7063563, "relname": "users", "relkind": "r", "relowner": 6150502, "relacl": USERS_ACL}] and
                  target_observed["catalog_objects"] == [] and target_observed["relations"] == [] and
                  target_observed["table_data"] == {} and target_observed["sequences"] == {},
                  "The retained source is not the exact schema26 catalog or the target is not empty.")
        for name, snapshot in observations["before"].items():
            m.require(set(snapshot) == {"identity", "public", "relations", "catalog_objects", "table_data", "sequences", "casts_sha256"} and
                      snapshot["public"] == OBSERVED_PUBLIC[name], "A retained database observation has unexpected fields or public identity.")
        args = SimpleNamespace(source=SOURCE, manifest_sha256=MANIFEST_SHA, schema=26, mode="full", run="", package=[])
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
            check_retained_evidence()
            m.require_unit_terminal(m.unit_state(unit), unit, tag)
            return base_cluster_check(runner.hba_before)

        runner.check_cluster = check_cluster
        original_inspect = runner.inspect_objects
        original_unsupported_sql = m.unsupported_objects_sql
        population_checks = {pair["name"]: 0 for pair in PAIRS}

        def reviewed_unsupported_sql(role):
            statement = original_unsupported_sql(role)
            if role != 6150502:
                return statement
            needle = "c.relacl IS NOT NULL"
            m.require(statement.count(needle) == 1, "The pinned runner's relation ACL predicate changed.")
            exception = "(current_database()='goby_backup_m3e_source' AND c.relnamespace=2200 AND c.oid=7063563" \
                " AND c.relname='users' AND c.relkind='r' AND c.relowner=6150502" \
                " AND c.relacl::text[]=ARRAY['goby_backup_m3e_source=arwdDxtm/goby_backup_m3e_source']::text[]" \
                " AND c.relacl=pg_catalog.acldefault('r'::\"char\",c.relowner))"
            return statement.replace(needle, "(" + needle + " AND NOT " + exception + ")", 1)

        def inspect_retained(pair):
            name = pair["name"]
            m.require(pair["phase"] == "owned" and name in observations["before"],
                      "Database content may be inspected only before its removal fence.")
            check_retained_evidence()
            expected = observations["before"][name]
            # The single ACL exception is scoped to this call in the privately
            # imported runner. All its other SQL branches remain byte-for-byte.
            m.require(m.unsupported_objects_sql is original_unsupported_sql, "The runner's inspection function changed.")
            m.unsupported_objects_sql = reviewed_unsupported_sql
            try:
                objects = original_inspect(pair)
            finally:
                m.unsupported_objects_sql = original_unsupported_sql
            m.require(objects == expected["catalog_objects"] and
                      objects == (baseline["objects"] if name == PAIRS[0]["name"] else []),
                      "The retained schema object inventory changed.")
            relations = m.decode(m.pg(RELATIONS_SQL, name))
            m.require(relations == expected["relations"], "A retained relation OID, owner, kind, name, or ACL changed.")
            actual_tables, actual_sequences = {}, {}
            for row in relations:
                relation = row["relname"]
                m.require(re.fullmatch("[a-z0-9_]+", relation) is not None, "A retained relation name is outside its fixed SQL grammar.")
                if row["relkind"] == "r":
                    data = m.pg(f'SELECT coalesce(jsonb_agg(to_jsonb(v) ORDER BY to_jsonb(v)::text),\'[]\'::jsonb) FROM public."{relation}" v;', name)
                    encoded = data.encode()
                    m.require(len(encoded) <= 8 << 20 and m.sha(encoded) == expected["table_data"][relation]["sha256"],
                              "A retained table's exact JSON data fingerprint changed.")
                    values = m.decode(data)
                    m.require(isinstance(values, list), "A retained table observation is not a row array.")
                    actual_tables[relation] = {"rows": len(values), "sha256": m.sha(encoded)}
                    del values, data, encoded
                elif row["relkind"] == "S":
                    actual_sequences[relation] = m.decode(m.pg(f'SELECT jsonb_build_object(\'last_value\',last_value,\'log_cnt\',log_cnt,\'is_called\',is_called) FROM public."{relation}";', name))
            m.require(actual_tables == expected["table_data"] and actual_sequences == expected["sequences"],
                      "Retained table counts or sequence state changed.")
            m.require(m.decode(m.pg(RELATIONS_SQL, name)) == relations,
                      "A retained relation changed during the data observation.")
            population_checks[name] += 1
            return objects

        runner.inspect_objects = inspect_retained
        original_public_identity = runner.public_identity

        def public_identity(name):
            observed = original_public_identity(name)
            m.require(name in OBSERVED_PUBLIC and observed == OBSERVED_PUBLIC[name],
                      "The public schema differs from this run's exact reviewed observation.")
            return observed

        # Keep the original receipt's public identity for the runner's own
        # metadata comparison; only the observed namespace OIDs are authorized.
        runner.public_identity = public_identity

        def inspect_pair(pair):
            rows = m.pair_rows(pair["name"])
            m.validate_pair_rows(pair, rows, tag)
            m.require(rows == observations["before"][pair["name"]]["identity"], "The observed role or database identity changed.")
            m.require_role_isolated(pair)
            name, oid = pair["name"], pair["database_oid"]
            m.require(m.pg(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={oid})+"
                           f"(SELECT count(*) FROM pg_prepared_xacts WHERE database='{name}')+"
                           f"(SELECT count(*) FROM pg_replication_slots WHERE database='{name}');") == "0",
                      "The reviewed database has active or prepared work.")
            observed_public = runner.public_identity(name)
            casts = runner.cast_inventory(name)
            casts_sha256 = m.sha(json.dumps(casts, sort_keys=True).encode())
            m.require(observed_public == OBSERVED_PUBLIC[name] and casts == pair["casts"] and
                      casts_sha256 == observations["before"][name]["casts_sha256"],
                      "The reviewed public schema or cast inventory changed.")
            objects = inspect_retained(pair)
            expected = observations["before"][name]
            return {"identity": rows, "original_public": pair["public"], "observed_public": observed_public,
                    "catalog_objects": objects, "relations": expected["relations"], "table_data": expected["table_data"],
                    "sequences": expected["sequences"], "casts_sha256": casts_sha256,
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
                  "retained_database_evidence": {"directory": str(EVIDENCE), "directory_identity": evidence_identity,
                      "observations_path": str(OBSERVATIONS), "observations_sha256": OBSERVATIONS_SHA, "dumps": DUMPS},
                  "acl_exception": {"database": PAIRS[0]["name"], "schema_oid": 2200, "relation": "users",
                      "relation_oid": 7063563, "owner_oid": 6150502, "acl": USERS_ACL,
                      "scope": "Exact observed owner-default relation ACL; all other unsupported-object predicates retained"},
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
                      original_failure_evidence_preserved=True, retained_database_evidence_preserved=True,
                      population_checks=population_checks, force_or_backend_termination_used=False)
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
