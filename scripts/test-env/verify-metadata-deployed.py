#!/usr/bin/env python3
"""Read the deployed M5b metadata catalog once, only through SSH on Linux.

Only native administrator login/logout may mutate application state. No item,
library, scan, playback, or user-state write is issued. Source media/NFO files
and the independent browser runner's core/private directories are never opened.
Complete PostgreSQL JSON row texts remain in memory; reports retain counts and
aggregate hashes only. An exclusive attempt marker prevents retry/overwrite.
"""

from __future__ import annotations

from contextlib import redirect_stderr, redirect_stdout
from datetime import datetime, timezone
import hashlib
import importlib.util
import io
import json
import math
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
from urllib.parse import quote, urlencode, urlsplit


sys.dont_write_bytecode = True
RESULT = Path("/opt/goby-test/m5b-deployed-metadata-read.json")
ATTEMPT = Path("/opt/goby-test/m5b-deployed-metadata-read.attempt.json")
OWNER = "goby-metadata-deployed-read-m5b-v1"
SERVICE = "goby-foundation-test.service"
EXPECTED_PID = 3379452
EXPECTED_UID = 995
EXPECTED_BINARY_SHA256 = "210c6c075cd80db403c1a28f1a5672d9cdbdb93a81ef76dbba61dc4d83eea197"
MAX_REPORT_BYTES = 128 * 1024
ITEM_TYPES = {"Movie", "Video", "Series", "Season", "Episode", "Folder", "MusicArtist", "MusicAlbum", "Audio"}
VALUE_FIELDS = {"Name", "SortName", "Overview", "OriginalTitle", "OfficialRating", "ProductionYear", "PremiereDate",
                "CommunityRating", "ProviderIds", "Genres", "Tags", "Studios", "People", "IndexNumber", "ParentIndexNumber"}
DETAIL_FIELDS = {"Item", "Revision", "Automatic", "Effective", "Overrides", "LockedValues", "LockedFields",
                 "EditableFields", "InactiveFields", "LastEditedBy", "LastEditedAt"}
ITEM_FIELDS = {"Id", "LibraryId", "ParentId", "ParentName", "Name", "Type", "Path", "IsFolder"}
SUMMARY_FIELDS = ITEM_FIELDS | {"IndexNumber", "ParentIndexNumber", "ProductionYear", "HasOverrides", "LockedFieldCount"}
TABLE_NAME = re.compile(r"[a-z_][a-z0-9_]*")
IDENTIFIER = re.compile(r"[0-9a-f]{32}")


class Failure(Exception):
    """Carry only a fixed, non-secret assertion label."""


def check(condition, label):
    if not condition:
        raise Failure(label)


def canonical(value):
    return json.dumps(value, ensure_ascii=True, sort_keys=True, separators=(",", ":"), allow_nan=False).encode("utf-8")


def aggregate(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def private_write(path, value):
    payload = json.dumps(value, ensure_ascii=True, indent=2, sort_keys=True, allow_nan=False).encode("utf-8") + b"\n"
    check(len(payload) <= MAX_REPORT_BYTES, "The sanitized report exceeds its size bound")
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(descriptor, "wb") as output:
        output.write(payload)
        output.flush()
        os.fsync(output.fileno())
    info = path.lstat()
    check(stat.S_ISREG(info.st_mode) and info.st_uid == 0 and stat.S_IMODE(info.st_mode) == 0o600 and info.st_nlink == 1,
          "The sanitized report has unsafe ownership or permissions")


def reserve_attempt():
    check(sys.platform == "linux" and os.geteuid() == 0, "Run only as root on the authorized Linux test host")
    os.umask(0o077)
    parent = RESULT.parent
    info = parent.lstat()
    check(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and info.st_mode & 0o022 == 0 and parent.resolve(strict=True) == parent,
          "The fixed report directory has unsafe ownership")
    check(not RESULT.exists() and not RESULT.is_symlink() and not ATTEMPT.exists() and not ATTEMPT.is_symlink(),
          "A prior deployed metadata attempt already exists; it must not be retried or overwritten")
    private_write(ATTEMPT, {"owner": OWNER, "status": "claimed", "started_at": datetime.now(timezone.utc).isoformat(),
                            "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()})
    return info.st_dev, info.st_ino


def load_helpers():
    modules = []
    # Suppress helper import diagnostics: even an unexpected import failure must
    # not print arbitrary exception text, credentials, HTTP bodies, or URLs.
    with redirect_stdout(io.StringIO()), redirect_stderr(io.StringIO()):
        for name in ("verify-direct-playback.py", "verify-audio.py"):
            specification = importlib.util.spec_from_file_location("metadata_read_" + name.replace("-", "_"), Path(__file__).with_name(name))
            check(specification is not None and specification.loader is not None, "A protected helper cannot be loaded")
            module = importlib.util.module_from_spec(specification)
            specification.loader.exec_module(module)
            modules.append(module)
    return modules


def deployment():
    result = subprocess.run(["systemctl", "show", SERVICE, "-p", "MainPID", "--value"], capture_output=True, timeout=5)
    check(result.returncode == 0 and len(result.stdout) < 64, "The deployment process observation failed")
    pid = int(result.stdout.strip())
    check(pid == EXPECTED_PID, "The deployed main process changed from the authorized binary")
    status = Path(f"/proc/{pid}/status").read_text(encoding="utf-8")
    line = next(value for value in status.splitlines() if value.startswith("Uid:"))
    check([int(value) for value in line.split()[1:]] == [EXPECTED_UID] * 4, "The deployed process user does not match the authorized service")
    digest = hashlib.sha256()
    with Path(f"/proc/{pid}/exe").open("rb") as executable:
        for block in iter(lambda: executable.read(1024 * 1024), b""):
            digest.update(block)
    check(digest.hexdigest() == EXPECTED_BINARY_SHA256, "The deployed executable digest does not match the accepted build")
    return {"main_pid": pid, "uid": EXPECTED_UID, "binary_sha256": digest.hexdigest()}


def table_snapshot(database):
    names = database.read("SELECT COALESCE(json_agg(tablename ORDER BY tablename),'[]'::json) FROM pg_tables WHERE schemaname='public';",
                          "Public table inventory")
    check(isinstance(names, list) and 0 < len(names) <= 64 and all(isinstance(name, str) and TABLE_NAME.fullmatch(name) for name in names),
          "The public table inventory is outside its bounded scope")
    entries = []
    for name in names:
        # JSON row TEXT preserves every numeric digit and omission/null value.
        # The Database helper decodes only the outer JSON, never those raw rows.
        entries.append("'" + name + "',COALESCE((SELECT json_agg(row_text ORDER BY row_text) FROM "
                       '(SELECT to_jsonb(t)::text AS row_text FROM public."' + name + '" t) snapshot_rows),\'[]\'::json)')
    query = "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY; SELECT json_build_object(" + ",".join(entries) + "); COMMIT;"
    snapshot = database.read(query, "Complete deployed table snapshot")
    check(isinstance(snapshot, dict) and set(snapshot) == set(names), "The complete table snapshot omitted a table")
    for rows in snapshot.values():
        check(isinstance(rows, list) and len(rows) <= 20000 and all(isinstance(row, str) for row in rows),
              "A table snapshot exceeds its complete-row bound")
        rows.sort()
    return snapshot


def snapshot_report(snapshot):
    return {name: {"count": len(rows), "sha256": aggregate(rows)} for name, rows in sorted(snapshot.items())}


def parsed_rows(snapshot, name):
    check(name in snapshot, "A required deployed table is missing")
    result = [json.loads(row) for row in snapshot[name]]
    check(all(isinstance(row, dict) for row in result), "A deployed table row is not an object")
    return result


def rows_by_id(snapshot, name):
    rows = parsed_rows(snapshot, name)
    check(all(isinstance(row.get("id"), str) for row in rows), "A required row identity is invalid")
    result = {row["id"]: raw for row, raw in zip(rows, snapshot[name])}
    check(len(result) == len(rows), "A row identity is unexpectedly duplicated")
    return result


def identify_login(before, after):
    check(set(before) == set(after), "Login changed the table inventory")
    for name in before:
        if name != "sessions":
            check(before[name] == after[name], "Login changed a table outside authentication sessions")
    old, current = rows_by_id(before, "sessions"), rows_by_id(after, "sessions")
    created = set(current) - set(old)
    check(len(created) == 1 and all(current.get(key) == value for key, value in old.items()),
          "Login changed preexisting authentication rows or created an unexpected number of sessions")
    identifier = next(iter(created))
    row = json.loads(current[identifier])
    check(row.get("kind") == "admin" and row.get("revoked_at") is None, "Login did not create one active native administrator session")
    return identifier


def check_final_preservation(before, after, owned_id, login_snapshot):
    check(set(before) == set(after), "The public table inventory changed during catalog reads")
    for name in before:
        if name != "sessions":
            check(before[name] == after[name], "A non-authentication table changed during deployed catalog verification")
    old, current = rows_by_id(before, "sessions"), rows_by_id(after, "sessions")
    check(all(current.get(key) == value for key, value in old.items()), "A preexisting authentication row changed")
    check(set(current) - set(old) == {owned_id}, "Authentication changes escaped the single owned login")
    original = json.loads(rows_by_id(login_snapshot, "sessions")[owned_id])
    revoked = json.loads(current[owned_id])
    check(original.pop("revoked_at") is None and isinstance(revoked.pop("revoked_at"), str) and original == revoked,
          "Logout changed more than the owned session revocation timestamp")


def safe_api_type(smoke):
    class CatalogAPI(smoke.API):
        def __init__(self):
            super().__init__()
            self.counts = {"GET": 0, "POST": 0, "DELETE": 0}
            self.logout_attempted = False

        def request(self, method, path, **options):
            parsed = urlsplit(path)
            route = parsed.path
            check(not parsed.scheme and not parsed.netloc and not parsed.fragment, "HTTP verification must remain on local native routes")
            if method == "POST":
                check(route == "/admin/v1/session" and not parsed.query and self.counts[method] == 0,
                      "The verifier cannot issue a write other than its single administrator login")
            elif method == "DELETE":
                check(route == "/admin/v1/session" and not parsed.query and not self.logout_attempted,
                      "The verifier cannot issue a write other than its single administrator logout")
                self.logout_attempted = True
            elif method == "GET":
                check(route in {"/admin/v1/libraries", "/admin/v1/capabilities"} or
                      re.fullmatch(r"/admin/v1/libraries/[0-9a-f]{32}/items", route) or
                      re.fullmatch(r"/admin/v1/items/[0-9a-f]{32}/metadata", route),
                      "The verifier cannot read a route outside native catalog metadata")
                check(self.counts[method] < 100, "The metadata HTTP request budget was exceeded")
            else:
                raise Failure("The verifier cannot perform this HTTP method")
            self.counts[method] += 1
            return super().request(method, path, **options)

    return CatalogAPI


def expected_item(row, item_index):
    parent = item_index.get(row.get("parent_id"), {})
    if parent.get("library_id") != row["library_id"]:
        parent = {}
    return {"Id": row["id"], "LibraryId": row["library_id"], "ParentId": row.get("parent_id") or "",
            "ParentName": parent.get("name", ""), "Name": row["name"], "Type": row["type"],
            "Path": row["path"], "IsFolder": row["is_folder"]}


def expected_summary(row, item_index, states):
    state = states[row["id"]]
    effective = state.get("effective")
    if effective is None:
        effective = row.get("local_metadata")
    year = effective.get("ProductionYear") if isinstance(effective, dict) else None
    if type(year) not in {int, float} or not 1 <= year <= 9999 or int(year) != year:
        year = None
    return {**expected_item(row, item_index), "IndexNumber": row["index_number"] if row["type"] in {"Season", "Episode"} else None,
            "ParentIndexNumber": row["parent_index_number"] if row["type"] == "Episode" else None,
            "ProductionYear": year, "HasOverrides": bool(state["overrides"]), "LockedFieldCount": len(state["locked_values"])}


def check_values(values):
    check(isinstance(values, dict) and set(values) == VALUE_FIELDS, "Complete metadata values have missing or additional properties")
    for field in ("Name", "SortName", "Overview", "OriginalTitle", "OfficialRating"):
        check(isinstance(values[field], str), "A metadata text value has the wrong JSON type")
    check(bool(values["Name"] and values["SortName"]), "An automatic name or sort name is empty")
    for field in ("ProductionYear", "IndexNumber", "ParentIndexNumber"):
        value = values[field]
        check(value is None or type(value) is int, "A nullable metadata integer lost its JSON type")
    value = values["CommunityRating"]
    check(value is None or type(value) in {int, float} and math.isfinite(value), "A nullable metadata rating is invalid")
    value = values["PremiereDate"]
    check(value is None or isinstance(value, str) and re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z", value),
          "A nullable premiere date is not a UTC timestamp")
    check(isinstance(values["ProviderIds"], dict) and all(isinstance(key, str) and isinstance(value, str) for key, value in values["ProviderIds"].items()),
          "Provider identifiers are not a non-null string map")
    for field in ("Genres", "Tags", "Studios"):
        check(isinstance(values[field], list) and all(isinstance(value, str) for value in values[field]),
              "A metadata name collection is not a non-null array")
    check(isinstance(values["People"], list), "Credits must be a non-null array")
    for person in values["People"]:
        check(isinstance(person, dict) and set(person) == {"Name", "Type", "Role", "SortOrder"} and
              all(isinstance(person[field], str) for field in ("Name", "Type", "Role")) and
              (person["SortOrder"] is None or type(person["SortOrder"]) is int), "A projected source credit has the wrong shape")


def check_detail(detail, row, item_index):
    check(isinstance(detail, dict) and set(detail) == DETAIL_FIELDS, "Metadata detail must have exactly eleven properties")
    check(isinstance(detail["Item"], dict) and set(detail["Item"]) == ITEM_FIELDS and detail["Item"] == expected_item(row, item_index),
          "Metadata detail changed the authorized item context")
    check(detail["Revision"] == "1", "An existing item has an unexpected metadata revision")
    check_values(detail["Automatic"])
    check_values(detail["Effective"])
    check(detail["Effective"] == detail["Automatic"], "Unedited effective metadata differs from its automatic values")
    check(detail["Overrides"] == {} and detail["LockedValues"] == {} and detail["LockedFields"] == [] and detail["InactiveFields"] == [],
          "An existing item unexpectedly has saved metadata controls")
    expected_editable = VALUE_FIELDS - {"ParentIndexNumber"}
    if row["type"] != "Episode":
        expected_editable -= {"IndexNumber"}
    fields = detail["EditableFields"]
    check(isinstance(fields, list) and len(fields) == len(set(fields)) and set(fields) == expected_editable,
          "Metadata detail has an invalid editable-field projection")
    check(detail["LastEditedBy"] == "" and detail["LastEditedAt"] is None, "Unedited metadata acquired administrator edit attribution")


def verify_catalog(api, snapshot):
    libraries = parsed_rows(snapshot, "libraries")
    items = parsed_rows(snapshot, "items")
    states = {row["item_id"]: row for row in parsed_rows(snapshot, "item_metadata_state")}
    item_index = {row["id"]: row for row in items}
    check(len(libraries) == 5 and all(IDENTIFIER.fullmatch(row["id"]) for row in libraries), "The deployed scope is not the five existing libraries")
    nonroot = [row for row in items if row["id"] != row["library_id"]]
    check(len(nonroot) == 16 and all(IDENTIFIER.fullmatch(row["id"]) and row["type"] in ITEM_TYPES for row in nonroot),
          "The deployed metadata scope is not the sixteen existing non-root items")
    check(all(row["id"] in states and states[row["id"]]["revision"] == 1 and states[row["id"]]["overrides"] == {} and
              states[row["id"]]["locked_values"] == {} for row in nonroot), "The deployed metadata baseline has unexpected controls or revisions")
    probed = [row["media"] for row in items if row.get("media") is not None]
    check(probed and all(isinstance(value, dict) and value.get("ProbeVersion") == 5 for value in probed),
          "The deployed media catalog is not on probe version five")
    inventory = api.request("GET", "/admin/v1/libraries", admin=True, label="Existing library inventory")
    check(isinstance(inventory, dict) and inventory.get("TotalRecordCount") == 5 and len(inventory.get("Items", [])) == 5 and
          {row["Id"] for row in inventory.get("Items", [])} == {row["id"] for row in libraries}, "The native library inventory differs from the catalog")
    checks = []
    for ordinal, library in enumerate(sorted(libraries, key=lambda value: value["id"]), 1):
        records = sorted((row for row in nonroot if row["library_id"] == library["id"]), key=lambda row: (row["sort_name"].lower(), row["id"]))
        check(records, "An expected existing library has no non-root metadata items")
        context = {"Id": library["id"], "Name": library["name"], "CollectionType": library["collection_type"]}
        base = "/admin/v1/libraries/" + quote(library["id"], safe="") + "/items"
        chosen_type = records[0]["type"]
        search = records[0]["name"]
        check(0 < len(search.encode("utf-8")) <= 1024 and not any(ord(character) < 32 for character in search),
              "An existing library search value is outside the native contract")
        variants = [({}, records, 0, 50),
                    ({"StartIndex": "1", "Limit": "2"}, records[1:3], 1, 2),
                    ({"Types": chosen_type}, [row for row in records if row["type"] == chosen_type], 0, 50),
                    ({"SearchTerm": search}, [row for row in records if search.strip().lower() in row["name"].lower()], 0, 50)]
        for parameters, expected, start, limit in variants:
            target = base + ("?" + urlencode(parameters) if parameters else "")
            response = api.request("GET", target, admin=True, label="Existing native metadata item query")
            total = len(records) if "StartIndex" in parameters else len(expected)
            check(isinstance(response, dict) and set(response) == {"Library", "Items", "TotalRecordCount", "StartIndex", "Limit"},
                  "Native metadata list has an unexpected envelope")
            check(response["Library"] == context and type(response["TotalRecordCount"]) is int and response["TotalRecordCount"] == total and
                  type(response["StartIndex"]) is int and response["StartIndex"] == start and type(response["Limit"]) is int and response["Limit"] == limit,
                  "Native metadata list changed its library, total, or pagination")
            check(isinstance(response["Items"], list) and len(response["Items"]) == len(expected), "Native metadata page has an unexpected length")
            for actual, row in zip(response["Items"], expected):
                check(isinstance(actual, dict) and set(actual) == SUMMARY_FIELDS and actual == expected_summary(row, item_index, states),
                      "Native metadata summary differs from complete catalog facts")
        checks.append({"library_ordinal": ordinal, "item_count": len(records), "item_identity_sha256": aggregate([row["id"] for row in records]),
                       "default_paging_type_search": "passed"})
    for row in nonroot:
        detail = api.request("GET", "/admin/v1/items/" + quote(row["id"], safe="") + "/metadata", admin=True, label="Existing metadata detail")
        check_detail(detail, row, item_index)
    return {"library_count": 5, "non_root_item_count": 16, "probed_media_count": len(probed), "probe_version": 5,
            "libraries": checks, "detail_envelope_fields": 11, "all_revision_one": True,
            "all_controls_empty": True, "all_effective_equal_automatic": True, "nullable_and_collection_shapes": "passed"}, nonroot[0]["id"]


def main():
    summary = {"owner": OWNER, "status": "failed", "scope": "Deployed native catalog reads only; writes/rescans were accepted separately by browser tests using the same binary",
               "http_retries": 0, "source_media_or_nfo_opened": False, "cleanup_errors": [], "snapshots": {}}
    stage, report_parent = "attempt reservation", None
    database = api = before = login_snapshot = final_snapshot = owned_id = None
    private_values = []
    try:
        report_parent = reserve_attempt()
        stage = "protected helper import"
        smoke, audio = load_helpers()
        stage = "deployment identity"
        summary["deployment"] = deployment()
        stage = "read-only database connection"
        database = audio.Database()
        check(database.environment["PGPORT"] == "5432", "The verifier must use only the deployed PostgreSQL port")
        stage = "complete beginning table snapshot"
        before = table_snapshot(database)
        summary["snapshots"]["before_login"] = snapshot_report(before)
        migrations = parsed_rows(before, "schema_migrations")
        check(len(migrations) == 14 and max(row["version"] for row in migrations) == 14, "The deployed schema is not exactly fourteen")
        summary["deployment"]["schema_version"] = 14
        private_values.extend(row.get("path", "") for row in parsed_rows(before, "items"))
        stage = "administrator login"
        api = safe_api_type(smoke)()
        credentials = smoke.credentials()
        private_values.extend(credentials.values())
        api.admin_login(credentials)
        credentials.clear()
        private_values.extend([api.cookie, api.cookie.partition("=")[2], api.csrf])
        stage = "complete post-login table snapshot"
        login_snapshot = table_snapshot(database)
        owned_id = identify_login(before, login_snapshot)
        summary["snapshots"]["after_login"] = snapshot_report(login_snapshot)
        stage = "metadata editing capability"
        capability = api.request("GET", "/admin/v1/capabilities", admin=True, label="Metadata editing capability")
        check(capability.get("Features", {}).get("MetadataEditing") is True, "The deployed metadata editing capability is not enabled")
        summary["metadata_editing_capability"] = True
        stage = "five-library catalog and sixteen metadata details"
        summary["catalog"], first_item = verify_catalog(api, login_snapshot)
        stage = "complete post-read table snapshot"
        after_reads = table_snapshot(database)
        summary["snapshots"]["after_catalog_reads"] = snapshot_report(after_reads)
        check(after_reads == login_snapshot, "Native metadata GET requests changed complete database rows")
        summary["get_requests_all_tables_unchanged"] = True
        stage = "administrator logout"
        api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False, label="Owned metadata administrator logout")
        stage = "revoked administrator metadata denial"
        denied = api.request("GET", "/admin/v1/items/" + quote(first_item, safe="") + "/metadata", admin=True, expected=(401,), label="Revoked administrator metadata denial")
        check(isinstance(denied, dict) and denied.get("Error", {}).get("Code") == "invalid_credentials" and not (DETAIL_FIELDS & set(denied)),
              "A revoked administrator cookie still exposed metadata")
        summary["revoked_cookie_metadata_status"] = 401
        stage = "complete ending table snapshot"
        final_snapshot = table_snapshot(database)
        check_final_preservation(before, final_snapshot, owned_id, login_snapshot)
        summary["all_preexisting_rows_unchanged"] = True
        summary["authorized_authentication_changes"] = {"created_admin_sessions": 1, "own_session_revoked": True, "other_sessions_unchanged": True}
        stage = "ending deployment identity"
        check(deployment() == {key: summary["deployment"][key] for key in ("main_pid", "uid", "binary_sha256")}, "The deployment changed during catalog verification")
        summary["status"] = "passed"
    except BaseException as error:
        summary["failed_stage"] = stage
        summary["error"] = str(error) if isinstance(error, Failure) else "A protected helper or verification operation failed"
        summary["error_type"] = type(error).__name__
    finally:
        if api is not None:
            if api.cookie and not api.logout_attempted:
                try:
                    api.request("DELETE", "/admin/v1/session", admin=True, expected=(204,), parse=False, label="Owned metadata failure cleanup logout")
                except BaseException:
                    summary["cleanup_errors"].append("The single owned administrator logout attempt failed")
            summary["http_method_counts"] = dict(api.counts)
        if database is not None and before is not None:
            try:
                if final_snapshot is None:
                    final_snapshot = table_snapshot(database)
                summary["snapshots"]["after_logout"] = snapshot_report(final_snapshot)
                if owned_id is not None and login_snapshot is not None:
                    check_final_preservation(before, final_snapshot, owned_id, login_snapshot)
                    summary["all_preexisting_rows_unchanged"] = True
            except BaseException:
                summary["cleanup_errors"].append("The final complete-table preservation observation failed")
        if summary["cleanup_errors"]:
            summary["status"] = "failed"
        if report_parent is not None:
            try:
                info = RESULT.parent.lstat()
                check((info.st_dev, info.st_ino) == report_parent, "The fixed report directory changed")
                encoded = canonical(summary).decode("ascii")
                check(all(not value or len(value) < 8 or value not in encoded for value in private_values), "The sanitized report contains a protected value")
                private_write(RESULT, summary)
            except BaseException:
                summary = {"owner": OWNER, "status": "failed", "failed_stage": "sanitized report persistence",
                           "error": "The first-attempt result could not be saved privately; no retry or overwrite was performed"}
        print(json.dumps(summary, ensure_ascii=True, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
