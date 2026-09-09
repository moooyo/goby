#!/usr/bin/env python3
"""Observe schema 13 -> 14 on the owned deployed database without writing to it.

Run through SSH before deployment and immediately after startup, before scans or
playback verification. Only aggregate hashes and counts leave the read-only DB
connection. Authentication and background job tables are deliberately excluded.
"""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys


sys.dont_write_bytecode = True
OWNER = "goby-m5b-migration-audit-v1"
DIRECTORY = Path("/opt/goby-test/exec-scratch")
TABLES = ("server_settings", "users", "libraries", "library_roots", "items",
          "catalog_entities", "item_entities", "item_images", "item_subtitles", "user_item_data")


def require(value, message):
    if not value:
        raise RuntimeError(message)


def read_report(path):
    info = path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o077 == 0 and
            info.st_nlink == 1 and 0 < info.st_size < 65536, "Unsafe audit report")
    report = json.loads(path.read_bytes())
    require(report.get("owner") == OWNER, "Unexpected audit ownership")
    return report


def main():
    require(os.geteuid() == 0 and len(sys.argv) == 2 and sys.argv[1] in {"before", "after"},
            "Run the owned audit as root with before or after")
    phase = sys.argv[1]
    info = DIRECTORY.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and
            DIRECTORY.resolve(strict=True) == DIRECTORY, "Unsafe audit directory")
    spec = importlib.util.spec_from_file_location("goby_m5b_audit_support", Path(__file__).with_name("verify-audio.py"))
    support = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(support)
    database = support.Database()
    fields = []
    for table in TABLES:
        expression = "to_jsonb(t)"
        fields.append("'" + table + "', jsonb_build_object('count',(SELECT count(*) FROM " + table +
                      "),'rows',COALESCE((SELECT jsonb_agg(v.row ORDER BY v.row::text COLLATE \"C\") " +
                      "FROM (SELECT " + expression + " AS row FROM " + table + " t LIMIT 4097) v),'[]'::jsonb))")
    query = "SELECT jsonb_build_object('schema',(SELECT max(version) FROM schema_migrations)," + ",".join(fields) + ");"
    snapshot = database.read(query, "Deployed migration snapshot")
    expected = 13 if phase == "before" else 14
    require(snapshot.get("schema") == expected, "Unexpected deployed schema version")
    tables = {}
    for table in TABLES:
        value = snapshot[table]
        require(type(value["count"]) is int and 0 <= value["count"] <= 4096 and
                len(value["rows"]) == value["count"], "Incomplete or excessive migration snapshot")
        payload = json.dumps(value["rows"], ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()
        tables[table] = {"count": value["count"], "sha256": hashlib.sha256(payload).hexdigest()}
    report = {"owner": OWNER, "phase": phase, "schema_version": expected, "application_database_port": 5432,
              "tables": tables, "status": "captured"}
    if phase == "after":
        before = read_report(DIRECTORY / "m5b-migration-before.json")
        require(before.get("phase") == "before" and before.get("schema_version") == 13 and
                before.get("tables") == tables, "Migration changed an existing recorded row")
        states = database.read("SELECT json_build_object('rows',count(*),'default_states',count(*) FILTER ("
                               "WHERE ms.revision=1 AND ms.overrides='{}'::jsonb AND ms.locked_values='{}'::jsonb "
                               "AND ms.last_edited_by IS NULL AND ms.last_edited_at IS NULL),"
                               "'source_projection_preserved',count(*) FILTER (WHERE ms.effective IS NOT DISTINCT FROM i.local_metadata),"
                               "'source_keys_match',count(*) FILTER (WHERE ms.source_key=catalog_metadata_source_key(i))) "
                               "FROM item_metadata_state ms JOIN items i ON i.id=ms.item_id;", "Initialized metadata states")
        require(all(value == tables["items"]["count"] for value in states.values()),
                "Migration did not initialize metadata state without changing source projections")
        report.update({"status": "passed", "existing_rows_preserved": True, "initialized_metadata_state": states})
    target = DIRECTORY / ("m5b-migration-" + phase + ".json")
    descriptor = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as output:
        json.dump(report, output, ensure_ascii=False, indent=2)
        output.write("\n")
    print(json.dumps({"status": report["status"], "phase": phase, "schema_version": expected,
                      "table_count": len(tables)}))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print(json.dumps({"status": "failed", "error": "The owned read-only migration audit did not complete"}))
        raise SystemExit(1) from None
