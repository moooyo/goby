#!/usr/bin/env python3
"""Publish a read-only closeout of the committed seed after its mapper rejection."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
import sys
import types

R = Path("/opt/goby-test/resumed-delivery-20260913-4cd0f29a0c14")
OLD = R / "candidate-core-seed-01"
FAILURE = {"path": str(OLD / "private/failure.json"), "sha256": "5a9c5ff8698ef3746cda143913e7b7a69506d5bbb334b59ead670511114fac82"}
ORIGINAL_SHA = "365363509c9e9b71c2b5ae323ddc9c4a39605a5f6f8596a84a5693d933e1ced7"
PRIOR_RECONCILIATION_FAILURE = {"path": str(R / "candidate-core-seed-reconciliation-01/private/failure.json"), "sha256": "e089750ca279b2296d1017ee100cb4ff46c321dc9166ceaff43123b5e4757d48"}
STORED_Q_POLICY = {"EnableAllFolders": False, "EnabledFolders": [], "EnableMediaPlayback": True, "EnablePlaybackRemuxing": True,
                   "EnableAudioPlaybackTranscoding": True, "EnableVideoPlaybackTranscoding": True, "IsAdministrator": False, "IsDisabled": False}
QUERY = """BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT json_build_object(
 'schema',(SELECT max(version) FROM schema_migrations),
 'users',(SELECT json_agg(json_build_object('id',id,'name',name,'admin',is_administrator,'disabled',is_disabled,'policy',policy) ORDER BY id) FROM users),
 'libraries',(SELECT json_agg(json_build_object('id',id,'name',name,'collectionType',collection_type) ORDER BY id) FROM libraries),
 'roots',(SELECT json_agg(json_build_object('id',id,'libraryId',library_id,'path',path) ORDER BY id) FROM library_roots),
 'jobs',(SELECT json_agg(json_build_object('id',id,'libraryId',library_id,'status',status,'error',error,'forceProbe',force_probe) ORDER BY id) FROM scan_jobs),
 'items',(SELECT json_agg(json_build_object('id',id,'parentId',parent_id,'name',name,'type',type,'path',path) ORDER BY id) FROM items),
 'sessions',(SELECT json_agg(json_build_object('userId',user_id,'kind',kind,'tokenSha256',encode(token_hash,'hex'),'revoked',revoked_at IS NOT NULL) ORDER BY id) FROM sessions),
 'historyRows',(SELECT count(*) FROM user_item_data));
COMMIT;"""


def need(value, message):
    if not value:
        raise ValueError(message)


def bootstrap_read(pin):
    path = Path(pin["path"])
    need(path.is_relative_to(R) and ".." not in path.parts, "Reconciliation authority escaped the resumed scope.")
    for node in (path, *path.parents):
        info = node.lstat()
        need(info.st_uid == 0 and not stat.S_ISLNK(info.st_mode) and not info.st_mode & 0o022, "Unsafe reconciliation authority.")
        need(stat.S_ISREG(info.st_mode) if node == path else stat.S_ISDIR(info.st_mode), "Unexpected authority type.")
    before = path.stat()
    signature = lambda info: (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid, info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
    need(before.st_nlink == 1 and before.st_size <= 4 << 20, "Reconciliation authority exceeded its bound.")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        raw = stream.read((4 << 20) + 1)
        need(signature(os.fstat(stream.fileno())) == signature(before), "Authority changed during its read.")
    need(signature(path.stat()) == signature(before) and len(raw) == before.st_size and hashlib.sha256(raw).hexdigest() == pin["sha256"], "Reconciliation authority digest differs.")
    return raw


class StateCheckError(ValueError):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


def validate_source_state(snapshot, details, libraries, actors, resources, cleanup, catalog):
    """Compare the complete stored snapshot to captured projections without IO."""
    def check(value, code):
        if not value:
            raise StateCheckError(code)
    try:
        check(snapshot["schema"] == 28 and snapshot["historyRows"] == 0 and len(snapshot["users"]) == 8 and
              len(resources["users"]) == 8 and {row["id"] for row in snapshot["users"]} == set(resources["users"]) == {row["id"] for row in actors.values()},
              "source_schema_or_user_membership_differs")
        by_id = {row["id"]: row for row in snapshot["users"]}
        for role, actor in actors.items():
            row = by_id[actor["id"]]
            check(row["name"] == actor["username"] and row["admin"] is (role == "admin") and row["disabled"] is False and isinstance(row["policy"], dict), "stored_account_identity_differs")
            if role == "control-q":
                check(set(row["policy"]) == set(STORED_Q_POLICY) and row["policy"]["EnabledFolders"] == [] and
                      all(row["policy"][key] is expected for key, expected in STORED_Q_POLICY.items() if key != "EnabledFolders"), "stored_q_policy_differs")
            else:
                check(row["policy"] == {}, "stored_default_policy_differs")
        library_ids = {row["Id"] for row in libraries.values()}
        check(len(libraries) == len(snapshot["libraries"]) == len(snapshot["roots"]) == len(library_ids) == 3 and
              len({row["id"] for row in snapshot["roots"]}) == 3, "library_root_count_differs")
        for library in libraries.values():
            check({"id": library["Id"], "name": library["Name"], "collectionType": library["CollectionType"]} in snapshot["libraries"] and
                  [row["path"] for row in snapshot["roots"] if row["libraryId"] == library["Id"]] == library["Paths"], "library_root_mapping_differs")
        check(len(snapshot["jobs"]) == len(resources["jobs"]) == 3 and
              {(row["id"], row["libraryId"]) for row in snapshot["jobs"]} == {(row["id"], row["libraryId"]) for row in resources["jobs"]} and
              all(row["status"] == "Completed" and row["error"] == "" and row["forceProbe"] is False for row in snapshot["jobs"]), "scan_completion_differs")
        detail_ids = {row["Id"] for row in details}
        check(len(details) == len(detail_ids) == 10 and not detail_ids & library_ids and len(snapshot["items"]) == 13 and
              {row["id"] for row in snapshot["items"]} == detail_ids | library_ids, "stored_item_membership_differs")
        stored = {row["id"]: row for row in snapshot["items"]}
        for library in libraries.values():
            check(stored[library["Id"]] == {"id": library["Id"], "name": library["Name"], "type": "CollectionFolder", "parentId": None, "path": ""}, "collection_folder_shape_differs")
        for dto in details:
            row = stored[dto["Id"]]
            if dto["Type"] == "MusicAlbum":
                check(dto["Id"] == catalog["album"]["id"] and "Path" not in dto and row["path"] == "" and
                      row["parentId"] == dto["ParentId"] == libraries["Music"]["Id"], "root_album_projection_differs")
                observed_path = ""
            else:
                observed_path = dto["Path"]
            check((row["name"], row["type"], row["path"], row["parentId"]) == (dto["Name"], dto["Type"], observed_path, dto["ParentId"]), "catalog_item_projection_differs")
        check(len(cleanup) == 2 and {row["kind"] for row in cleanup} == {"native", "emby"}, "helper_cleanup_kind_binding_differs")
        expected_sessions = {("admin" if row["kind"] == "native" else "emby", row["tokenSha256"]) for row in cleanup}
        check(len(snapshot["sessions"]) == 2 and {row["kind"] for row in snapshot["sessions"]} == {"admin", "emby"} and
              all(row["revoked"] is True and row["userId"] == actors["admin"]["id"] for row in snapshot["sessions"]) and
              {(row["kind"], row["tokenSha256"]) for row in snapshot["sessions"]} == expected_sessions, "helper_token_revocation_differs")
        return {"users": 8, "libraries": 3, "roots": 3, "completedScans": 3, "storedItems": 13, "publicItems": 10, "collectionRoots": 3, "revokedHelperSessions": 2, "historyRows": 0}
    except StateCheckError:
        raise
    except (KeyError, TypeError, ValueError, IndexError):
        raise StateCheckError("source_snapshot_shape_invalid") from None


def reconcile(module, value, input_pin, source_pin, check_snapshot=None):
    m = module
    need(set(value) == {"kind", "version", "output", "mappingHelper", "originalBinding", "failure", "capturedDtos", "credentials"} and
         value["kind"] == "audited-candidate-seed-reconciliation-input" and value["version"] == 1 and value["failure"] == FAILURE,
         "Unexpected read-only reconciliation contract.")
    need(value["originalBinding"]["path"] == str(OLD / "private/binding.json") and value["capturedDtos"]["path"] == str(OLD / "private/actual-catalog-dtos.json"), "Original receipt paths differ.")
    binding, failure, details = [m.descriptor(value[key]) for key in ("originalBinding", "failure", "capturedDtos")]
    prior_failure = m.descriptor(PRIOR_RECONCILIATION_FAILURE)
    need(prior_failure["status"] == "read_only_reconciliation_failed" and prior_failure["originalFailure"] == FAILURE and
         prior_failure["businessWrites"] == prior_failure["httpRequests"] == 0, "Prior read-only failure binding differs.")
    need(binding["source"]["sha256"] == ORIGINAL_SHA and Path(binding["source"]["path"]).is_relative_to(R), "Original executed source differs.")
    m.read_checked(binding["source"]["path"], ORIGINAL_SHA)
    original = m.descriptor(binding["input"])
    need(original["output"] == str(OLD) and binding["candidate"] == m.MANIFEST and binding["runtimeInspection"] == m.INSPECTION and
         failure["status"] == "seed_failed_resources_retained" and failure["stage"] == "map_actual_catalog" and failure["output"] == str(OLD) and
         failure["requests"] == {"normal": 46, "cleanup": 4} and len(failure["cleanup"]) == 2 and
         all(row["logoutAcknowledged"] and row["sameTokenRejected"] for row in failure["cleanup"]), "Committed seed or cleanup facts differ.")

    def old_descriptor(pin):
        need(Path(pin["path"]).parent == OLD / "private", "Captured artifact escaped the original private scope.")
        return m.descriptor(pin)

    def response_body(label, status):
        rows = [row for row in failure["requestStates"] if row["label"] == label]
        need(len(rows) == 1 and rows[0]["outcome"] == "response_received" and rows[0]["status"] == status, "Captured operation is missing or uncertain.")
        response = old_descriptor(rows[0]["receipt"])
        need(response["status"] == status and response["complete"] is True, "Captured HTTP completion differs.")
        return old_descriptor(response["body"])

    server_id = m.item_id(response_body("public-system-info", 200)["Id"])
    roles = ("admin", *m.SCENARIOS, "control-q")
    need(set(value["credentials"]) == set(roles), "Existing credential membership differs.")
    actors = {}
    for role in roles:
        pin = value["credentials"][role]
        need(pin["path"] == str(OLD / "private" / (role + "-credentials.json")), "Credential artifact escaped its original role.")
        credential = old_descriptor(pin)
        need(set(credential) == {"actorId", "serverId", "username", "password"} and credential["serverId"] == server_id and
             isinstance(credential["password"], str) and len(credential["password"]) == 64, "Existing credential descriptor differs.")
        user = response_body("bootstrap" if role == "admin" else "create-" + role, 201)["User"]
        need(user["Id"] == credential["actorId"] and user["Name"] == credential["username"] and user["IsAdministrator"] is (role == "admin") and user["IsDisabled"] is False, "Captured account identity differs.")
        actors[role] = {"id": m.item_id(credential["actorId"]), "username": credential["username"], "credentials": pin}
    resources = failure["resources"]
    need(len(resources["users"]) == 8 and set(resources["users"]) == {row["id"] for row in actors.values()}, "Original user resources differ.")
    libraries = {}
    for directory, name, collection in m.LIBRARIES:
        library = response_body("create-library-" + directory.lower(), 201)["Library"]
        need(library["Name"] == name and library["CollectionType"] == collection and library["Paths"] == [str(m.C / "data/media" / directory)], "Captured library binding differs.")
        libraries[directory] = library
    need(len(resources["libraries"]) == 3 and set(resources["libraries"]) == {row["Id"] for row in libraries.values()} and
         len(details) == 10 and len({row["Id"] for row in details}) == 10 and all(row["ServerId"] == server_id for row in details), "Captured library or item membership differs.")
    catalog = m.map_catalog(details, libraries)
    if check_snapshot:
        need(check_snapshot["path"] == str(R / "candidate-core-seed-reconciliation-01/private/002-reconcile-source-state.stdout"), "Only the saved failed-run snapshot may be replayed.")
        snapshot = m.descriptor(check_snapshot)
        validation = validate_source_state(snapshot, details, libraries, actors, resources, failure["cleanup"], catalog)
        return {"snapshot": check_snapshot, "capturedDtos": value["capturedDtos"], "validation": validation, "businessWrites": 0, "httpRequests": 0, "newSqlQueries": 0}
    io = m.CandidateIO({**original, "output": value["output"], "budgets": {"maximumSeconds": 300, "cleanupSeconds": 30, "maximumRequests": 2, "cleanupRequests": 1}}, input_pin, source_pin)
    io.open()
    stage = "pin_owned_cluster"
    try:
        # No HTTP method is called: SQL transactions inspect only the pinned owned cluster.
        pg = io.candidate["processes"]["postgres"]
        need(io.provision.cluster(pg) == io.candidate["clusterSystemIdentifier"], "Owned PostgreSQL cluster changed.")
        stage = "read_source_state"
        snapshot = m.parse(io.provision.psql("reconcile-source-state", QUERY, io.candidate["database"]))
        stage = "validate_source_state"
        validation = validate_source_state(snapshot, details, libraries, actors, resources, failure["cleanup"], catalog)
        stage = "read_recovery_state"
        io.provision.cluster(pg)
        recovery = m.parse(io.provision.psql("reconcile-recovery-empty", "BEGIN READ ONLY; SELECT json_build_object('relations',(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace),'functions',(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace),'types',(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace)); COMMIT;", io.candidate["recoveryDatabase"]))
        need(recovery == {"relations": 0, "functions": 0, "types": 0}, "Recovery target is no longer empty.")
        stage = "verify_copied_files"
        copied = [old_descriptor(pin) for pin in resources["copiedFiles"]]
        need(len(copied) == 14 and {row["path"] for row in copied} == {str(m.C / "data/media" / name) for name in m.FILES}, "Copied file membership differs.")
        entries = list((m.C / "data/media").rglob("*"))
        need(len(entries) <= 30 and all(not path.is_symlink() for path in entries) and
             {str(path.relative_to(m.C / "data/media")) for path in entries if not path.is_dir()} == set(m.FILES), "Current media membership differs.")
        owners = (0, m.pwd.getpwnam("goby").pw_uid)
        for row in copied:
            io.deadline()
            path = Path(row["path"])
            before = m.safe_path(path, owners)
            need(list(m.file_identity(before)) == row["identity"] and before.st_uid == 0 and before.st_nlink == 1 and stat.S_IMODE(before.st_mode) == 0o440 and
                 before.st_size == row["bytes"] and row["sha256"] == m.FILES[str(path.relative_to(m.C / "data/media"))], "Copied file authority differs.")
            with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
                need(m.file_identity(os.fstat(stream.fileno())) == m.file_identity(before) and hashlib.file_digest(stream, "sha256").hexdigest() == row["sha256"] and
                     m.file_identity(os.fstat(stream.fileno())) == m.file_identity(before), "Copied file bytes changed.")
            need(m.file_identity(path.stat()) == m.file_identity(before), "Copied file changed after rehash.")
        state_pin = m.write_json_once(io.private / "current-state.json", {"source": snapshot, "recovery": recovery,
            "validation": validation, "businessSnapshotTransactionsReadOnly": True, "clusterIdentityQueriesAreSelectOnly": True, "httpRequests": 0})
        stage = "publish_manifest"
        catalog_pin = m.write_json_once(io.private / "catalog.json", catalog)
        provision_input = m.descriptor(io.candidate["input"])
        process = io.pin()
        io.deadline()
        result = {"kind": "audited-candidate-seed-manifest", "version": 1, "status": "seeded_pending_live_acceptance", "input": binding["input"], "helper": binding["source"],
            "candidateManifest": m.MANIFEST, "runtimeInspection": m.INSPECTION, "serverId": server_id, "admin": actors["admin"], "actors": {role: actors[role] for role in m.SCENARIOS}, "controlQ": actors["control-q"],
            "catalog": catalog, "catalogFile": catalog_pin, "actualCatalogDtos": value["capturedDtos"], "libraries": libraries, "roots": {key: row["Paths"][0] for key, row in libraries.items()},
            "source": {"manifestSha256": provision_input["sourceManifest"]["sha256"], "binarySha256": io.candidate["binary"]["sha256"], "schema": 28}, "processes": {"candidate": process},
            "resources": resources, "cleanup": failure["cleanup"], "requests": failure["requests"], "budgets": original["budgets"], "playbackRequests": 0,
            "clientAcceptance": False, "candidateAdmissionComplete": False, "failure": FAILURE,
            "reconciliation": {"input": input_pin, "helper": source_pin, "mappingHelper": value["mappingHelper"], "originalBinding": value["originalBinding"], "state": state_pin,
                "priorFailure": PRIOR_RECONCILIATION_FAILURE, "httpRequests": 0, "businessWrites": 0, "originalFailurePreserved": True,
                "reason": "Preserve the observed English alias and explicitly compare stored collection roots and legacy policy fields to their public projections."}}
        return m.write_json_once(io.private / "manifest.json", result)
    except Exception as error:
        failure_pin = None
        try:
            failure_pin = m.write_json_once(io.private / "failure.json", {"status": "read_only_reconciliation_failed", "stage": stage, "code": getattr(error, "code", "reconciliation_" + stage + "_failed"),
                "errorType": type(error).__name__, "originalFailure": FAILURE, "priorFailure": PRIOR_RECONCILIATION_FAILURE, "businessWrites": 0, "httpRequests": 0})
        except Exception:
            pass
        print(json.dumps({"status": "read_only_reconciliation_failed", "stage": stage, "code": getattr(error, "code", "reconciliation_" + stage + "_failed"),
            "errorType": type(error).__name__, "receipt": failure_pin, "originalFailure": FAILURE, "businessWrites": 0}))
        return None


def main():
    need(sys.platform == "linux" and os.geteuid() == 0 and os.environ.get("SSH_CONNECTION") and sys.flags.isolated and sys.flags.dont_write_bytecode, "Use root SSH with /usr/bin/python3 -I -B.")
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("input", "input-sha256", "source-sha256"):
        parser.add_argument("--" + name, required=True)
    parser.add_argument("--check-snapshot")
    parser.add_argument("--check-snapshot-sha256")
    args = parser.parse_args()
    source_pin = {"path": str(Path(__file__).absolute()), "sha256": args.source_sha256}
    bootstrap_read(source_pin)
    input_pin = {"path": args.input, "sha256": args.input_sha256}
    provisional = json.loads(bootstrap_read(input_pin))
    module = types.ModuleType("reconciled_seed_mapping")
    module.__file__ = provisional["mappingHelper"]["path"]
    exec(compile(bootstrap_read(provisional["mappingHelper"]), module.__file__, "exec"), module.__dict__)
    need(bool(args.check_snapshot) == bool(args.check_snapshot_sha256), "Saved snapshot path and hash must be supplied together.")
    check_snapshot = {"path": args.check_snapshot, "sha256": args.check_snapshot_sha256} if args.check_snapshot else None
    try:
        result = reconcile(module, module.descriptor(input_pin), input_pin, source_pin, check_snapshot)
    except Exception as error:
        print(json.dumps({"status": "pure_state_replay_failed" if check_snapshot else "reconciliation_preflight_failed", "stage": "validate_source_state" if isinstance(error, StateCheckError) else "reconciliation_preflight",
            "code": getattr(error, "code", "reconciliation_preflight_failed"), "errorType": type(error).__name__, "businessWrites": 0, "httpRequests": 0}))
        return 2
    if result and check_snapshot:
        print(json.dumps({"status": "pure_state_replay_passed", **result}))
        return 0
    if result:
        print(json.dumps({"status": "seeded_pending_live_acceptance", "manifest": result, "businessWrites": 0, "httpRequests": 0, "candidateAdmissionComplete": False}))
    return 0 if result else 2


if __name__ == "__main__":
    raise SystemExit(main())
