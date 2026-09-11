#!/usr/bin/env python3
"""Repair the one failed source31 disposal after a read-only JSON cast proof.

This one-run operator reuses the pinned backup runner's removal guards. It
never changes the failed report, original receipt, logs, credentials, or HBA.
Only the pinned failed attempt is accepted; any prior repair prevents a retry.
The one fixed server-test schema and two empty public Extras tables are pinned
by complete catalog, ownership, population, sequence, and dependency evidence.
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
RUN = "20260911_154145_d9df2c8f78f8"
WORK = Path("/opt/goby-test/exec-work-m3e")
SOURCE = WORK / "source-attempt-31"
OUTPUT = WORK / ("client-backup-run-" + RUN)
REPAIR_OUTPUT = WORK / "source31-disposal-repair-01"
CONTROL = Path("/opt/goby-test/postgres-workspace-v1")
RUNNER = WORK / "backup-schema27-tool-01/run-client-backup-tests.py"
RUNNER_SHA = "7cf91591efaeca841907cfe1950d0bfcb6c33ef091a17fe54c69db167a74c22a"
MANIFEST_SHA = "0468c7e557c82249a866b81ae7b22924c2dc456bf5cd333af45b9a6d1ad7aa92"
RECEIPT_SHA = "606da91c1a22e81d69a8b57d6bdf190e5aea0c2b48ee835f4ee7000de61c982a"
FAILED_OPERATOR = WORK / "source31-disposal-tool-01/dispose-source31-special-features-full-failed-pair.py"
FAILED_OPERATOR_SHA = "85d63c61b938a5cf014a77a34bee085753c425d206aa923580b7c21ac4fb4800"
FAILED_GUARD = WORK / "source31-disposal-tool-01/test-dispose-source31-special-features-full-failed-pair.py"
FAILED_GUARD_SHA = "c9c70ff4c73abfdc96ffd5d01cfa4f02effc0dc00cc8fbe8ff921f80695f7b16"
DIAGNOSIS = WORK / "source31-disposal-sql-diagnosis-01/report.json"
DIAGNOSIS_SHA = "784eb7e6444661f6aab497f3e2ade06ba165fb1278aada9f3a1b619ec3847fd3"
FIRST_ATTEMPT_FILES = {
    "disposal-before.json": "0dab837b1215d1a7f499e811d5df8d745579a32917e1d40e3d9447fcd8b8dc0f",
    "disposal-failed.json": "aa83628eed53fd88d802e46090f9ba09f9507f8bb0f9a48331e08421a4c085d8",
    "disposal-intent.json": "1aeb56df25f676ce4d4e870240821436c0e00e5d90498c326d1b298fe33c272e",
    "disposal-normalize-goby_backup_m3e_target-intent.json": "86ca6f1fc9fa620d1117ee37fec446f6001cdaef9d4e6152a365fe1ea25b0a89",
    "disposal-original-receipt.json": RECEIPT_SHA,
}
REPORT_SHA = "92177a7b3c3f1797ed24109aacd03c575caebac80faccd4d820df3e640853779"
LOG_SHA = "6788eec946f2a9939c32664627c504261e4b6addb6c17e4d699444d8beb8a12c"
CATALOG_BEFORE_SHA = "ce62d3df80d4eb2b1595d712eeb30df38a9521c285722b2ca335daf90bb5d376"
HBA_SHA = "6571fe86239d7bec6f5e9de9ebefbeda56f0e7df43cdbc58a3dc064e41e958fd"
OWNER_SHA = "610b3dcdafa8b6bb960a26946c398b98c02357d61c9c7fdf9b5a8b8504b9d5af"
CLUSTER = {"system_identifier": "7684040109719526738", "process": {
    "pid": 327173, "start_ticks": 289359, "boot_id": "6bdfc486-7bc8-412f-82b5-70095a09dde7"}}
PAIRS = [{"name": "goby_backup_m3e_source", "role_oid": 8334353, "database_oid": 8334354},
         {"name": "goby_backup_m3e_target", "role_oid": 8334355, "database_oid": 8334356}]
OUTPUT_IDENTITY = {"device": 2049, "inode": 2817763}
CATALOG_SHA = "1fc91c2e380805bff0f87867547d307bc7830ffeb49c3489da4e1713a5c0047d"
GUARD_SHA = {
    "guards.stdout": "9ecb9d07c61c61b7510c8ed16e895ad2c146d0244920914906663d1d40703ea9",
    "guards.stderr": "990ab0a20e53bd44e889ebd85482fb5bf15631275d8b187e056c324fd560f09c",
}
STOP_INTENT = WORK / "specialfeatures27-transfer-01/source31-stop-intent.json"
STOP_INTENT_SHA = "688ae5c9019faf9523f5263fb10f435dde78dbeb98146b72325bc918cdf58f6e"
SERVER_FIXTURE_SHA = "498bf263a1b4261417d55a2bd1d9e3dd398df39307c5acfcf1f853f1f2de0479"
RECOVERY_FIXTURE_SHA = "b71d66d70f19e4ee44d28e0b7e2df493de388c6c9b2002e3966bfca4958781a1"
EXTRA_SCHEMA = "goby_server_test_32ed586c92dbf6c66de0b89c"
TABLES = ("extra_reserved_paths", "item_extra_resources")
MISSING_FOREIGN = {"extra_reserved_paths.extra_reserved_paths_root_id_fkey",
    "item_extra_resources.item_extra_resources_owner_item_id_fkey",
    "item_extra_resources.item_extra_resources_resource_item_id_fkey"}
OBSERVED = {
    ("goby_backup_m3e_source", "public"): {"oid": 2200, "identities": "4db1fcbcc6077c4de1bb7a0f71efffbc2078755082359f61a8f04a82210eae25",
        "rows": "7257d5d267f9ff0e0032bc405e5f7fa2c74d248ec90d289a7998ead453dedb38", "sequences": "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", "addresses": 19, "toast": 2},
    ("goby_backup_m3e_target", "public"): {"oid": 8961085, "identities": "cc75de7c145d2a7b958e65a5f71a5ff95f4b206ab99359a8e9a97ff08f027cfb",
        "rows": "7257d5d267f9ff0e0032bc405e5f7fa2c74d248ec90d289a7998ead453dedb38", "sequences": "44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a", "addresses": 19, "toast": 2},
    ("goby_backup_m3e_source", EXTRA_SCHEMA): {"oid": 9014011, "identities": "a844954e08c59edfe5cde671a85e6207cb177a47a581c6460ffee1c8122d3709",
        "rows": "96bbecfe74e0bc335e3ef896ca1750751062df46096d80a59b89887570927b2c", "sequences": "6dbacf5ae9c44d4cf1068c6f3463455095519f173ea6c7651bf083af836200b9", "addresses": 942, "toast": 35},
}


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def verify_failed_attempt(m):
    # The old attempt is evidence only. No field is rewritten or adopted as a
    # successful normalization; fresh catalog capture must still match it.
    documents = {}
    for name, digest in FIRST_ATTEMPT_FILES.items():
        raw = m.private_read(OUTPUT / name)
        m.require(m.sha(raw) == digest, "The original failed disposal evidence changed.")
        documents[name] = m.decode(raw)
    for path, digest in ((FAILED_OPERATOR, FAILED_OPERATOR_SHA), (FAILED_GUARD, FAILED_GUARD_SHA)):
        m.require(m.sha(m.private_read(path, modes=(0o600, 0o644))) == digest, "The original executed disposal tools changed.")
    failed = documents["disposal-failed.json"]
    m.require(failed == {"run_id": RUN, "status": "failed", "error_type": "Failure",
        "source_receipt_sha256": RECEIPT_SHA, "pairs": [dict(pair, phase="owned") for pair in PAIRS]},
        "The original failure no longer proves both retained owned pairs.")
    intent = documents["disposal-intent.json"]
    m.require(intent["run_id"] == RUN and intent["status"] == "reviewed" and intent["removed"] == [] and
        intent["operator_sha256"] == FAILED_OPERATOR_SHA and intent["source_receipt_sha256"] == RECEIPT_SHA and
        intent["before"] == documents["disposal-before.json"], "The original disposal intent changed scope.")
    target = PAIRS[1]
    m.require(documents["disposal-normalize-goby_backup_m3e_target-intent.json"] == {
        "run_id": RUN, "pair": target, "before_sha256": fingerprint(documents["disposal-before.json"][target["name"]]),
        "exact_schema": None, "public_tables": list(TABLES), "schema_dependency_closure_verified": True,
        "public_cascade": False}, "The first attempt advanced beyond the reviewed first normalization intent.")
    raw = m.private_read(DIAGNOSIS)
    m.require(m.sha(raw) == DIAGNOSIS_SHA, "The independent read-only SQL diagnosis changed.")
    diagnosis = m.decode(raw)
    m.require(diagnosis["run_id"] == RUN and diagnosis["operator_sha256"] == FAILED_OPERATOR_SHA and
        diagnosis["database_read_only"] is True and diagnosis["lock_or_drop_executed"] is False and
        diagnosis["before_matches_failed_attempt"] is True and diagnosis["after_unchanged"] is True and
        diagnosis["original_receipt_unchanged"] is True and
        [(case["case"], case["exit_code"]) for case in diagnosis["cases"]] == [("original", 3), ("cast-jsonb", 0)],
        "The diagnosis does not prove the isolated cast correction and unchanged retained state.")


def expected_objects(m, baseline, schema):
    m.require(schema in ("public", EXTRA_SCHEMA), "An unreviewed schema cannot be normalized.")
    if schema == EXTRA_SCHEMA:
        return baseline["objects"]
    relations = {*TABLES, "extra_reserved_paths_pkey", "item_extra_resources_pkey", "item_extra_resources_owner_idx"}
    result = [row for row in baseline["objects"] if
              (row["kind"] in ("relation", "index") and row["name"] in relations) or
              (row["kind"] == "column" and row["name"].split(".")[0] in relations) or
              (row["kind"] == "constraint" and row["name"].split(".")[0] in TABLES and row["name"] not in MISSING_FOREIGN)]
    m.require(len(result) == 26, "The exact residual M27 catalog subset changed.")
    return result


def identity_sql(oid, owner):
    return ("SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY class_name,oid),'[]'::jsonb) FROM ("
        f"SELECT 'pg_class'::text class_name,c.oid::bigint,c.relname name,c.relkind::text kind,c.relowner::bigint owner,c.relacl::text acl FROM pg_class c WHERE c.relnamespace={oid}"
        f" UNION ALL SELECT 'pg_proc',p.oid::bigint,p.proname,p.prokind::text,p.proowner::bigint,p.proacl::text FROM pg_proc p WHERE p.pronamespace={oid}"
        f" UNION ALL SELECT 'pg_type',t.oid::bigint,t.typname,t.typtype::text,t.typowner::bigint,t.typacl::text FROM pg_type t WHERE t.typnamespace={oid}"
        f" UNION ALL SELECT 'pg_constraint',c.oid::bigint,c.conname,c.contype::text,{owner}::bigint,NULL::text FROM pg_constraint c WHERE c.connamespace={oid}"
        f" UNION ALL SELECT 'pg_trigger',t.oid::bigint,t.tgname,t.tgisinternal::text,{owner}::bigint,NULL::text FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace={oid}"
        f" UNION ALL SELECT 'pg_attrdef',a.oid::bigint,a.adnum::text,'default',{owner}::bigint,NULL::text FROM pg_attrdef a JOIN pg_class c ON c.oid=a.adrelid WHERE c.relnamespace={oid}) q")


def closure_sql(oid):
    # TOAST relations and their indexes are included only through the exact
    # reviewed base relations' reltoastrelid edges, never by a name prefix.
    return (f"WITH base AS (SELECT oid,reltoastrelid FROM pg_class WHERE relnamespace={oid}),"
        " rels AS (SELECT oid FROM base UNION SELECT reltoastrelid FROM base WHERE reltoastrelid<>0 UNION SELECT indexrelid FROM pg_index WHERE indrelid IN (SELECT reltoastrelid FROM base WHERE reltoastrelid<>0)),"
        f" owned(classid,objid) AS (SELECT 'pg_namespace'::regclass::oid,{oid}::oid UNION SELECT 'pg_class'::regclass::oid,oid FROM rels"
        f" UNION SELECT 'pg_proc'::regclass::oid,oid FROM pg_proc WHERE pronamespace={oid}"
        f" UNION SELECT 'pg_type'::regclass::oid,oid FROM pg_type WHERE typnamespace={oid} OR typrelid IN (SELECT oid FROM rels)"
        f" UNION SELECT 'pg_constraint'::regclass::oid,oid FROM pg_constraint WHERE connamespace={oid}"
        " UNION SELECT 'pg_trigger'::regclass::oid,oid FROM pg_trigger WHERE tgrelid IN (SELECT oid FROM rels)"
        " UNION SELECT 'pg_attrdef'::regclass::oid,oid FROM pg_attrdef WHERE adrelid IN (SELECT oid FROM rels))"
        " SELECT jsonb_build_object('unknown',(SELECT coalesce(jsonb_agg(jsonb_build_object('class',d.classid::regclass::text,'oid',d.objid,'refclass',d.refclassid::regclass::text,'ref',d.refobjid,'kind',d.deptype)),'[]'::jsonb)"
        " FROM pg_depend d WHERE (EXISTS(SELECT 1 FROM owned o WHERE o.classid=d.refclassid AND o.objid=d.refobjid) AND NOT EXISTS(SELECT 1 FROM owned o WHERE o.classid=d.classid AND o.objid=d.objid))"
        " OR (d.deptype IN ('i','e','P','S') AND EXISTS(SELECT 1 FROM owned o WHERE o.classid=d.classid AND o.objid=d.objid) AND NOT EXISTS(SELECT 1 FROM owned o WHERE o.classid=d.refclassid AND o.objid=d.refobjid))),"
        " 'toast_tables',(SELECT count(*) FROM base WHERE reltoastrelid<>0),'owned_addresses',(SELECT count(*) FROM owned))")


def preparation_unknown_sql(m, name, role):
    sql = m.unsupported_objects_sql(role)
    if name != PAIRS[0]["name"]:
        return sql
    original = "SELECT 1 FROM pg_namespace WHERE nspname NOT IN ('public','pg_catalog','pg_toast','information_schema')"
    m.require(sql.count(original) == 1, "The frozen runner's namespace predicate changed.")
    # This is a separate pre-normalization query, not a replacement for the
    # runner's inspector. Its object checks also cover the one extra schema.
    return sql.replace("n.nspname='public'", "n.nspname IN ('public','" + EXTRA_SCHEMA + "')").replace(original,
        original + " AND NOT(oid=9014011 AND nspname='" + EXTRA_SCHEMA + "' AND nspowner=8334353 AND nspacl IS NULL)")


def observe_namespace(m, pair, baseline, schema):
    name, role = pair["name"], pair["role_oid"]
    m.require((name, schema) in OBSERVED, "Only the exact reviewed namespace and database can be inspected.")
    expected = OBSERVED[(name, schema)]
    statements = []
    def capture(sql):
        raw = m.pg('BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SET LOCAL search_path=pg_catalog,"' + schema + '"; ' + sql + '; COMMIT;', name)
        value = m.decode(raw)
        statements.append((sql, raw))
        return value
    namespace_sql = "SELECT jsonb_build_object('oid',oid::bigint,'name',nspname,'owner',nspowner::bigint,'acl',nspacl,'comment',obj_description(oid,'pg_namespace')) FROM pg_namespace WHERE nspname='" + schema + "'"
    namespace = capture(namespace_sql)
    m.require(namespace == {"oid": expected["oid"], "name": schema, "owner": role if schema == EXTRA_SCHEMA else 6171,
        "acl": None if schema == EXTRA_SCHEMA else ["pg_database_owner=UC/pg_database_owner", "=U/pg_database_owner"],
        "comment": None if schema == EXTRA_SCHEMA else "standard public schema"}, "The exact namespace identity or grants changed.")
    matches = re.findall(r"(?m)^const catalogObjectsSQL = `([^`]+)`", (SOURCE / "internal/backuppg/catalog.go").read_text())
    m.require(len(matches) == 1 and matches[0].startswith("WITH namespace AS") and ";" not in matches[0], "The trusted catalog query changed.")
    objects = capture(matches[0].replace("$1", "'" + schema + "'"))
    m.require(objects == expected_objects(m, baseline, schema), "The retained objects are not the exact accepted schema or M27 subset.")
    identities = capture(identity_sql(expected["oid"], role))
    m.require(fingerprint(identities) == expected["identities"] and all(row["owner"] == role and row["acl"] is None for row in identities),
              "An object OID, owner, kind, or ACL changed.")
    rows, sequences = {}, {}
    for row in identities:
        if row["class_name"] != "pg_class" or row["kind"] not in ("r", "S"):
            continue
        table = row["name"]
        m.require(re.fullmatch(r"[a-z][a-z0-9_]*", table), "A retained relation name escaped its fixed SQL grammar.")
        if row["kind"] == "r":
            values = capture(f'SELECT coalesce(jsonb_agg(to_jsonb(v) ORDER BY to_jsonb(v)::text),\'[]\'::jsonb) FROM "{schema}"."{table}" v')
            rows[table] = {"rows": len(values), "sha256": m.sha(statements[-1][1].encode())}
        else:
            sequences[table] = capture(f'SELECT jsonb_build_object(\'last_value\',last_value,\'log_cnt\',log_cnt,\'is_called\',is_called) FROM "{schema}"."{table}"')
    m.require(fingerprint(rows) == expected["rows"] and fingerprint(sequences) == expected["sequences"], "A retained table population or sequence changed.")
    closure = capture(closure_sql(expected["oid"]))
    m.require(closure == {"unknown": [], "toast_tables": expected["toast"], "owned_addresses": expected["addresses"]},
              "The exact object dependency closure has an outside edge or unknown member.")
    return {"namespace": namespace, "catalog_objects": objects, "identities": identities, "table_data": rows,
            "sequences": sequences, "dependency_closure": closure}, statements


def observe_residual_pair(m, runner, pair, baseline):
    name, oid = pair["name"], pair["database_oid"]
    rows = m.pair_rows(name)
    m.validate_pair_rows(pair, rows, runner.tag)
    m.require_role_isolated(pair)
    m.require(m.pg(f"SELECT (SELECT count(*) FROM pg_stat_activity WHERE datid={oid})+"
                   f"(SELECT count(*) FROM pg_prepared_xacts WHERE database='{name}')+"
                   f"(SELECT count(*) FROM pg_replication_slots WHERE database='{name}');") == "0", "The pair has live or prepared work.")
    m.require(runner.cast_inventory(name) == pair["casts"] and m.pg(preparation_unknown_sql(m, name, pair["role_oid"]), name) == "f",
              "An unreviewed hook, cast, schema, object, or privilege prevents normalization.")
    snapshots, checks = {}, {}
    for schema in ([EXTRA_SCHEMA, "public"] if name == PAIRS[0]["name"] else ["public"]):
        snapshots[schema], checks[schema] = observe_namespace(m, pair, baseline, schema)
    return {"identity": rows, "namespaces": snapshots, "work_count": 0, "casts_unchanged": True}, checks


def normalize_reviewed_pair(m, runner, pair, before, checks, publish):
    name = pair["name"]
    namespaces = before["namespaces"]
    m.require({key: pair[key] for key in ("name", "role_oid", "database_oid")} in PAIRS and
              set(namespaces) == set(checks) == ({EXTRA_SCHEMA, "public"} if name == PAIRS[0]["name"] else {"public"}) and
              set(namespaces["public"]["table_data"]) == set(TABLES),
              "Normalization escaped the exact reviewed pair, schemas, or two public tables.")
    publish(REPAIR_OUTPUT / ("disposal-normalize-" + name + "-intent.json"), {"run_id": RUN, "pair": {key: pair[key] for key in ("name", "role_oid", "database_oid")},
        "before_sha256": fingerprint(before), "exact_schema": EXTRA_SCHEMA if EXTRA_SCHEMA in namespaces else None,
        "public_tables": list(TABLES), "schema_dependency_closure_verified": True, "public_cascade": False})
    locked = [f'"{schema}"."{table}"' for schema, value in namespaces.items() for table in sorted(value["table_data"])]
    sql = "BEGIN; SET LOCAL lock_timeout='3s'; SET LOCAL statement_timeout='60s'; LOCK TABLE " + ",".join(locked) + " IN ACCESS EXCLUSIVE MODE;\n"
    for schema, statements in checks.items():
        sql += 'SET LOCAL search_path=pg_catalog,"' + schema + '";\n'
        unknown = preparation_unknown_sql(m, name, pair["role_oid"])
        m.require(unknown.startswith("SELECT ") and unknown.endswith(";"), "The frozen unknown-object guard shape changed.")
        body = "BEGIN\nIF (" + unknown[7:-1] + ") THEN RAISE EXCEPTION 'Unknown residual object appeared'; END IF;\n"
        body += "\n".join("IF (" + query + ")::jsonb IS DISTINCT FROM E'" + raw.replace("\\", "\\\\").replace("'", "''") +
            "'::jsonb THEN RAISE EXCEPTION 'Reviewed residual state changed'; END IF;" for query, raw in statements) + "\nEND;"
        delimiter = "$source31_checked_state$"
        m.require(delimiter not in body, "A captured value conflicts with the bounded guard delimiter.")
        sql += "DO " + delimiter + body + delimiter + ";\n"
    if EXTRA_SCHEMA in namespaces:
        sql += 'DROP SCHEMA "' + EXTRA_SCHEMA + '" CASCADE;\n'
    sql += 'DROP TABLE public."item_extra_resources",public."extra_reserved_paths"; COMMIT;'
    runner.check_cluster(runner.hba_before)
    m.validate_pair_rows(pair, m.pair_rows(name), runner.tag)
    m.require_role_isolated(pair)
    m.pg(sql, name)
    # The frozen inspector is used unchanged only after real normalization.
    m.require(runner.inspect_objects(pair) == [], "The pair is not truly empty after the reviewed normalization.")
    publish(REPAIR_OUTPUT / ("disposal-normalize-" + name + "-completed.json"), {"run_id": RUN, "name": name, "objects": [], "status": "normalized"})


def load_runner():
    # Check the fixed executable source before importing any of its code.
    for path in reversed((RUNNER, *RUNNER.parents)):
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or info.st_uid != 0 or info.st_gid != 0 or info.st_mode & 0o022:
            raise RuntimeError("The pinned runner path is not privately controlled.")
    info = RUNNER.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or hashlib.sha256(RUNNER.read_bytes()).hexdigest() != RUNNER_SHA:
        raise RuntimeError("The pinned runner differs from the reviewed source31 input.")
    spec = importlib.util.spec_from_file_location("source31_disposal_runner", RUNNER)
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
    identity, files = m.verify_source(SOURCE, MANIFEST_SHA, 27)
    m.require(files.get(m.catalog_name(27)) == CATALOG_SHA and
              len(m.read_source_catalog(SOURCE, files, 27)["catalog"]["Tables"]) == 35,
              "The exact 35-table schema27 catalog changed.")
    for name, digest in GUARD_SHA.items():
        m.require(m.sha(m.private_read(RUNNER.parent / name)) == digest,
                  "The retained 108-guard verification evidence changed.")
    workspace = m.load_workspace(SOURCE)
    lock = workspace.acquire_lock()
    started = False
    runner = None
    repair_identity = None
    try:
        original = m.private_read(m.RECEIPT)
        m.require(m.sha(original) == RECEIPT_SHA, "The retained failure receipt changed.")
        receipt = m.decode(original)
        tag = m.MARKER + ":" + RUN
        unit = "goby-client-backup-" + RUN.replace("_", "-") + ".service"
        m.require(receipt["marker"] == m.MARKER and receipt["run_id"] == RUN and receipt["tag"] == tag and
                  receipt["unit"] == unit and receipt["source"] == str(SOURCE) and receipt["output"] == str(OUTPUT) and
                  receipt["source_manifest_sha256"] == MANIFEST_SHA and type(receipt["schema"]) is int and receipt["schema"] == 27 and
                  receipt.get("catalog_sha256") == CATALOG_SHA and receipt["mode"] == "full" and receipt["phase"] == "retained" and
                  receipt["cleanup_complete"] is False and receipt["cluster"] == CLUSTER and
                  receipt["cluster_owner_sha256"] == OWNER_SHA and receipt["output_identity"] == OUTPUT_IDENTITY,
                  "The failure receipt is outside this exact reviewed run.")
        m.require([{key: p[key] for key in ("name", "role_oid", "database_oid")} for p in receipt["pairs"]] == PAIRS and
                  all(p["phase"] == "owned" for p in receipt["pairs"]), "The reviewed pair identities changed.")
        m.require(m.directory(OUTPUT) == OUTPUT_IDENTITY, "The failed output directory was replaced.")
        disposal_path = CONTROL / ("client-backup-disposal-" + RUN + ".json")
        m.require(not m.present(disposal_path) and not m.present(REPAIR_OUTPUT) and
                  {p.name for p in OUTPUT.iterdir() if p.name.startswith("disposal-")} == set(FIRST_ATTEMPT_FILES) and
                  not any(m.present(OUTPUT / (p["name"] + "-objects.json")) for p in PAIRS),
                  "This exact repair is missing its failed attempt or already has repair evidence.")
        verify_failed_attempt(m)
        pinned = {"report.json": REPORT_SHA, "go.log": LOG_SHA, "catalog-before.json": CATALOG_BEFORE_SHA,
                  "hba-original": HBA_SHA}
        for name, digest in pinned.items():
            m.require(m.sha(m.private_read(OUTPUT / name)) == digest, "Original failure evidence changed.")
        failed = m.decode(m.private_read(OUTPUT / "report.json"))
        m.require(failed["status"] == "failed" and failed["run_id"] == RUN and failed["unit_exit"] == 0 and
                  failed.get("schema") == 27 and failed.get("mode") == "full" and
                  failed["pair_evidence_retained"] is True and failed["hba_after_sha256"] == HBA_SHA and
                  failed["cleanup"] == {"hba_restored_exactly": True, "receipt_saved": True, "unit_terminal": True},
                  "The failure report no longer proves a terminal run with restored HBA.")
        before_catalog = m.decode(m.private_read(OUTPUT / "catalog-before.json"))
        stop_raw = m.private_read(STOP_INTENT)
        m.require(m.sha(stop_raw) == STOP_INTENT_SHA, "The owned stop intent changed.")
        stop = m.decode(stop_raw)
        m.require(stop["run_id"] == RUN and stop["unit"] == unit and stop["main_pid"] == 700802 and
                  stop["state"]["Description"] == tag and stop["state"]["MainPID"] == "700802" and
                  failed["unit_process"]["pid"] == 700802 and failed["unit_process"]["start_ticks"] == 6417182,
                  "The terminal run is not the recorded explicitly stopped server-test lifetime.")
        m.require(files["internal/server/server_integration_test.go"] == SERVER_FIXTURE_SHA and
                  files["internal/recovery/manager_integration_test.go"] == RECOVERY_FIXTURE_SHA,
                  "The known schema creator or incomplete old cleanup list changed.")
        args = SimpleNamespace(source=SOURCE, manifest_sha256=MANIFEST_SHA, schema=27, mode="full", run="", package=[])
        runner = m.Runner(args)
        runner.run, runner.tag, runner.unit, runner.output = RUN, tag, unit, REPAIR_OUTPUT
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
            verify_failed_attempt(m)
            if repair_identity is not None:
                m.require(m.directory(REPAIR_OUTPUT) == repair_identity, "The independent repair evidence directory changed.")
            m.require(m.private_read(m.RECEIPT) == original and m.directory(OUTPUT) == OUTPUT_IDENTITY,
                      "The original receipt or output identity changed during disposal.")
            snapshot = REPAIR_OUTPUT / "disposal-original-receipt.json"
            if m.present(snapshot):
                m.require(m.private_read(snapshot) == original, "The exclusive original-receipt snapshot changed.")
            for name, digest in pinned.items():
                m.require(m.sha(m.private_read(OUTPUT / name)) == digest, "Original failure evidence changed during disposal.")
            m.require(m.sha(m.private_read(STOP_INTENT)) == STOP_INTENT_SHA, "The stop provenance changed during disposal.")
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
            public = runner.public_identity(name)
            m.require(public == dict(pair["public"], oid=OBSERVED[(name, "public")]["oid"]) and runner.cast_inventory(name) == pair["casts"],
                      "The reviewed empty public schema or cast inventory changed.")
            inspect_empty(pair)
            return {"identity": rows, "public": public, "original_public": pair["public"], "catalog_objects": [], "table_row_counts": {},
                    "connections_prepared_slots": 0, "external_role_dependencies": 0, "casts_unchanged": True}

        def catalog_without_reviewed_pair():
            current = m.catalog_state()
            for field in ("roles", "databases"):
                current[field] = [row for row in current[field] if row["name"] not in m.NAMES]
            return current

        check_cluster()
        baseline = m.read_source_catalog(SOURCE, files, 27)
        before_pairs = {pair["name"]: observe_residual_pair(m, runner, pair, baseline)[0] for pair in runner.pairs}
        m.require(before_pairs == m.decode(m.private_read(OUTPUT / "disposal-before.json")),
                  "The retained pair no longer equals the pre-failure snapshot.")
        m.require(catalog_without_reviewed_pair() == before_catalog, "Preexisting cluster metadata changed.")
        REPAIR_OUTPUT.mkdir(mode=0o700)
        m.sync_directory(REPAIR_OUTPUT.parent)
        repair_identity = m.directory(REPAIR_OUTPUT)
        runner.output_identity = repair_identity
        m.create_private(REPAIR_OUTPUT / "OWNER.json", json.dumps({"marker": "goby-source31-disposal-repair-v1",
            "run_id": RUN, "path": str(REPAIR_OUTPUT), "failed_operator_sha256": FAILED_OPERATOR_SHA,
            "first_attempt_files": FIRST_ATTEMPT_FILES}, sort_keys=True).encode() + b"\n")
        m.sync_directory(REPAIR_OUTPUT)
        report = {"marker": "goby-client-backup-disposal-m3e-v1", "run_id": RUN, "tag": tag,
                  "status": "reviewed", "source_receipt_sha256": RECEIPT_SHA, "failed_report_sha256": REPORT_SHA,
                  "failed_log_sha256": LOG_SHA, "source_manifest_sha256": MANIFEST_SHA,
                  "schema": 27, "catalog_sha256": CATALOG_SHA, "runner_sha256": RUNNER_SHA, "runner_guard_evidence": GUARD_SHA,
                  "system_identifier": CLUSTER["system_identifier"], "cluster": CLUSTER,
                  "hba_sha256": HBA_SHA, "pairs": PAIRS, "before": before_pairs,
                  "operator_sha256": m.sha(Path(__file__).read_bytes()), "removed": [],
                  "stop_intent_sha256": STOP_INTENT_SHA, "exact_interrupted_schema": EXTRA_SCHEMA,
                  "normalization": "Only the fixed schema with proven dependency closure uses CASCADE; public tables use no CASCADE."}

        def publish(path, value):
            raw = json.dumps(value, sort_keys=True, indent=2).encode() + b"\n"
            m.create_private(path, raw)
            m.sync_directory(path.parent)
            return m.sha(raw)

        # This is a new exclusive snapshot; the live failure receipt and all
        # historical receipt snapshots remain byte-for-byte unchanged.
        m.create_private(REPAIR_OUTPUT / "disposal-original-receipt.json", original)
        m.sync_directory(REPAIR_OUTPUT)
        publish(REPAIR_OUTPUT / "disposal-before.json", before_pairs)
        publish(REPAIR_OUTPUT / "disposal-intent.json", report)
        started = True
        sequence = 0

        def save_progress():
            # Retain the original failure receipt; only append disposal evidence.
            nonlocal sequence
            check_cluster()
            sequence += 1
            publish(REPAIR_OUTPUT / f"disposal-progress-{sequence:02d}.json",
                    {"run_id": RUN, "source_receipt_sha256": RECEIPT_SHA,
                     "pairs": [{k: p[k] for k in ("name", "role_oid", "database_oid", "phase")} for p in runner.pairs]})

        runner.save = save_progress
        for pair in reversed(runner.pairs):
            check_cluster()
            current, checks = observe_residual_pair(m, runner, pair, baseline)
            m.require(current == before_pairs[pair["name"]], "The exact retained residual state changed.")
            normalize_reviewed_pair(m, runner, pair, current, checks, publish)
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
        report.update(repair_directory=str(REPAIR_OUTPUT), first_attempt_files_preserved=True,
                      first_attempt_files=FIRST_ATTEMPT_FILES, diagnostic_report_sha256=DIAGNOSIS_SHA)
        repair_report = REPAIR_OUTPUT / "disposal-report.json"
        digest = publish(repair_report, report)
        # Publish only the previously absent canonical success receipt required
        # by the unchanged runner. First-attempt records remain immutable.
        report_path = OUTPUT / "disposal-report.json"
        m.create_private(report_path, m.private_read(repair_report))
        m.sync_directory(OUTPUT)
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
                m.create_private(REPAIR_OUTPUT / "disposal-failed.json", json.dumps(failure, sort_keys=True).encode() + b"\n")
                m.sync_directory(REPAIR_OUTPUT)
            except Exception:
                pass
        raise
    finally:
        fcntl.flock(lock, fcntl.LOCK_UN)
        os.close(lock)


if __name__ == "__main__":
    main()
