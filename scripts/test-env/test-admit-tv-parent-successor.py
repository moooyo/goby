#!/usr/bin/env python3
"""Pure affected-admission guards; optional saved-contract replay has no live IO."""
import copy
import importlib.util
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest


spec = importlib.util.spec_from_file_location("tv_admission", Path(__file__).with_name("admit-audited-candidate.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
SAVED = "--saved-contracts" in sys.argv
if SAVED:
    sys.argv.remove("--saved-contracts")


def pin(name):
    return {"path": "/opt/goby-test/" + name, "sha256": "a" * 64}


def input_value():
    return {"kind": "audited-candidate-admission-input", "version": 3, "admissionKind": "affected_tv_parent",
            "runId": "affected-tv-parent-01", "output": m.TV_ROOT + "candidate-live-admission-05",
            "runtimeHelper": pin("runtime"), "admissionHelper": pin("admission"), "compiledCatalog": pin("catalog"),
            "runtimeEpoch": copy.deepcopy(m.TV_EPOCH), "seedRuntimeBinding": copy.deepcopy(m.TV_BINDING),
            "reusedAdmission04": copy.deepcopy(m.TV_REUSED), "transitionCloseout": copy.deepcopy(m.TV_CLOSEOUT),
            "budgets": dict(m.TV_LIMITS)}


def authority():
    value = input_value()
    keys = ("serverId", "admin", "actors", "controlQ", "catalog", "catalogFile", "actualCatalogDtos", "libraries", "roots", "resources")
    seed = {key: {} for key in keys}
    seed.update(helper=pin("seed-helper"), input=pin("seed-input"), candidateManifest=pin("provision"), cleanup=[],
                source={"manifestSha256": "0" * 64})
    source = {"sourceManifest": pin("new-source"), "binary": {"path": "/opt/candidate/goby", "sha256": "b" * 64}}
    previous = {"version": 2, "operationKind": "environment_revision", "currentSource": {
        "binary": {"path": "/opt/candidate/goby", "sha256": "c" * 64}, "sourceManifest": pin("old-source")}}
    epoch = {"version": 3, "operationKind": "binary_successor", "runtimeHelper": value["runtimeHelper"],
             "seedProvenance": pin("seed"), "originalProvision": seed["candidateManifest"], "currentSource": source,
             "previousEpoch": pin("previous"), "before": pin("before"), "after": pin("after"), "preservation": pin("proof"),
             "candidateProcess": {"pid": 22}, "postgresProcess": {"pid": 11}, "calls": {"stop": 1, "replace": 1, "start": 1},
             "candidate": {"listener": {"port": 28498, "socketInode": "1234"}}}
    binding = {key: copy.deepcopy(seed[key]) for key in keys}
    binding.update(version=3, runtimeEpoch=value["runtimeEpoch"], originalSeed=epoch["seedProvenance"], seedExecutor=seed["helper"],
                   seedInput=seed["input"], seedCleanup=[], previousBinding=pin("previous-binding"))
    transition = {"compiledCatalog": value["compiledCatalog"], "previousEpoch": epoch["previousEpoch"], "previousBinding": binding["previousBinding"]}
    reused = {"kind": "audited-candidate-live-admission", "version": 2, "status": "admitted_for_core_client", "failure": None,
              "cleanupFailures": [], "candidateAdmissionComplete": True, "clientAcceptance": False, "runtimeEpoch": epoch["previousEpoch"],
              "seedRuntimeBinding": binding["previousBinding"], "currentSource": previous["currentSource"], "inactiveStageRetained": True,
              "activeGenerationChanged": False, "playbackRequests": 0, "applyRequests": 0, "rollbackRequests": 0,
              "controllerSessions": {key: {"sameTokenRejected": True} for key in ("admin", "P", "Q")}}
    closeout = {key: copy.deepcopy(epoch[key]) for key in ("currentSource", "previousEpoch", "before", "after", "preservation", "runtimeHelper", "candidateProcess", "postgresProcess", "calls")}
    closeout.update(kind="audited-candidate-tv-parent-transition-closeout", version=1,
                    status="successor_running_awaiting_affected_live_admission", runtimeEpoch=value["runtimeEpoch"], seedRuntimeBinding=value["seedRuntimeBinding"],
                    candidateAdmissionComplete=False, clientAcceptance=False, currentProcessPinned=True, oldProcessAbsent=True,
                    stagedBinaryAbsent=True, installedBinaryVerified=True,
                    preservationChecks={"ownedTablesExact": 35, "sequencesExact": True, "inactiveStageExact": True,
                        "allPriorPlayAndUserDataExact": True, "controlMediaAssetsExact": True, "configurationExact": True,
                        "postgresContinuous": True, "hostingContinuous": True})
    return tuple(copy.deepcopy(row) for row in (value, epoch, binding, seed, transition, previous, reused, closeout))


def tv_fixture():
    library, series_id = "a" * 32, "b" * 32
    series = {"id": series_id, "type": "Series", "path": "/media/series"}
    seasons = [{"id": digit * 32, "type": "Season", "parentId": series_id, "seriesId": series_id, "indexNumber": number}
               for number, digit in ((1, "c"), (2, "d"))]
    episodes = [{"id": digit * 32, "type": "Episode", "parentId": seasons[season - 1]["id"], "seriesId": series_id,
                 "parentIndexNumber": season, "indexNumber": episode, "path": "/media/episode-" + digit}
                for season, episode, digit in ((1, 1, "e"), (1, 2, "f"), (2, 1, "1"))]
    rows = []
    for item in [series, *seasons, *episodes]:
        rows.append({"id": item["id"], "type": item["type"], "name": "Stored " + item["id"][0],
                     "library_id": library, "parent_id": item.get("parentId", library), "is_folder": item["type"] != "Episode",
                     "index_number": item.get("indexNumber", 0), "parent_index_number": item.get("parentIndexNumber", 0),
                     "path": item.get("path", "/media/season-" + item["id"][0])})
    rows[2]["name"] = "Season 02"
    seed = {"catalog": {"series": series, "seasons": seasons, "episodes": episodes}, "libraries": {"TV": {"Id": library}}}
    snapshot = {"tables": {"items": rows}}
    return seed, snapshot, {row["id"]: row for row in rows}


def dto(row, rows, detail=False):
    value = {"Id": row["id"], "Type": row["type"], "Name": row["name"], "ParentId": row["parent_id"],
             "IsFolder": row["is_folder"], "ServerId": "server"}
    if detail:
        value["Path"] = row["path"]
    if row["type"] in ("Season", "Episode"):
        value["IndexNumber"] = row["index_number"]
        parent = rows[row["parent_id"]]
        series = parent if row["type"] == "Season" else rows[parent["parent_id"]]
        value.update(SeriesId=series["id"], SeriesName=series["name"])
        if row["type"] == "Episode":
            value.update(ParentIndexNumber=row["parent_index_number"], SeasonId=parent["id"], SeasonName=parent["name"])
    return value


def state_delta():
    before = {"capturedAt": "2026-09-13T15:00:00Z", "tables": {"table_%d" % number: [] for number in range(30)},
              "sequences": {"devices_id_seq": {"lastValue": "7", "isCalled": True},
                            "activity_entries_id_seq": {"lastValue": "58", "isCalled": True},
                            "users_seq": {"lastValue": "8", "isCalled": True}}}
    before["tables"].update(sessions=[{"id": "old", "revoked_at": "old"}], devices=[{"id": 7, "old": True}],
                            activity_entries=[{"id": 58, "old": True}], play_sessions=[{"id": "prepared", "state": "Prepared"}],
                            user_item_data=[{"id": "old-data", "position_ticks": 42}])
    after = copy.deepcopy(before)
    after["capturedAt"] = "2026-09-13T15:02:00Z"
    logins = {}
    for offset, role in enumerate(("P", "Q")):
        token, device_id = "token-" + role, 8 + offset
        session = {"id": role, "user_id": "actor-" + role, "kind": "emby", "token_hash": "\\x" + m.sha(token.encode()),
                   "device_registry_id": device_id, "created_at": "2026-09-13T15:00:01Z", "last_seen_at": "2026-09-13T15:00:01Z", "revoked_at": None}
        device = {"id": device_id, "reported_device_id": "fresh-" + role, "last_user_id": session["user_id"], "last_seen_at": session["last_seen_at"]}
        logins[role] = {"initialSession": copy.deepcopy(session), "initialDevice": copy.deepcopy(device), "deviceId": "fresh-" + role,
                        "auth": {"kind": "emby", "token": token}, "logout401": True}
        session.update(last_seen_at="2026-09-13T15:00:30Z", revoked_at="2026-09-13T15:01:01Z")
        after["tables"]["sessions"].append(session)
        after["tables"]["devices"].append(device)
        for count, action in enumerate(("session.login", "session.revoked")):
            after["tables"]["activity_entries"].append({"id": 59 + offset * 2 + count, "action": action, "source": "emby", "actor_kind": "user",
                "actor_id": session["user_id"], "actor_credential_id": role, "resource_kind": "session", "resource_id": role, "state": "",
                "severity": "Info", "request_id": "", "observation_fingerprint": "", "revision": 0, "previous_revision": 0,
                "affected_count": 1, "changed_fields": [], "created_at": "2026-09-13T15:01:02Z"})
    after["sequences"]["devices_id_seq"]["lastValue"] = "9"
    after["sequences"]["activity_entries_id_seq"]["lastValue"] = "62"
    return before, after, logins


class TVAdmissionGuards(unittest.TestCase):
    def test_exact_v3_input_preserves_separate_old_admission(self):
        self.assertEqual(m.validate_input(input_value())["budgets"], m.TV_LIMITS)
        self.assertEqual(set(m.TV_CHECKS), {"runtimeIdentity", "tvDefaultParents", "tvDetailParents", "ordinaryAuthorization",
                                          "healthWindow60Seconds", "sourceAndInactivePreserved", "sessionCleanup"})

    def test_input_rejects_old_scope_authority_or_expanded_budget(self):
        for key, replacement in (("runtimeEpoch", pin("old-epoch")), ("seedRuntimeBinding", pin("old-binding")),
                                 ("reusedAdmission04", pin("different-report")), ("transitionCloseout", pin("different-closeout")),
                                 ("admissionKind", "full"), ("budgets", dict(m.LIMITS))):
            value = input_value()
            value[key] = replacement
            with self.assertRaises(m.AdmissionError):
                m.validate_input(value)

    def test_successor_authority_retains_original_seed_and_reused_source(self):
        values = authority()
        original = copy.deepcopy(values)
        self.assertEqual(m.validate_tv_authority(*values)["pid"], 22)
        self.assertEqual(values, original)

    def test_authority_rejects_cross_candidate_and_seed_bindings(self):
        for mutate in (lambda v: v[2].update(runtimeEpoch=pin("wrong")), lambda v: v[2].update(seedExecutor=pin("wrong")),
                       lambda v: v[4].update(previousBinding=pin("wrong")), lambda v: v[6].update(currentSource=v[1]["currentSource"]),
                       lambda v: v[7].update(candidateProcess={"pid": 999})):
            values = authority()
            mutate(values)
            with self.assertRaises(m.AdmissionError):
                m.validate_tv_authority(*values)

    def test_reused_report_must_have_its_own_full_cleanup(self):
        for mutate in (lambda row: row.update(version=3), lambda row: row.update(candidateAdmissionComplete=False),
                       lambda row: row.update(inactiveStageRetained=False), lambda row: row.update(applyRequests=1),
                       lambda row: row["controllerSessions"]["admin"].update(sameTokenRejected=False)):
            values = authority()
            mutate(values[6])
            with self.assertRaises(m.AdmissionError):
                m.validate_tv_authority(*values)

    def test_tv_names_and_relationships_come_from_stored_rows(self):
        seed, snapshot, rows = tv_fixture()
        self.assertEqual(m.tv_rows(seed, snapshot), rows)
        for row in rows.values():
            m.validate_tv_dto(dto(row, rows), row, rows, "server")
        episode = rows[seed["catalog"]["episodes"][-1]["id"]]
        self.assertEqual(dto(episode, rows)["SeasonName"], "Season 02")

    def test_default_relationships_cannot_be_missing_or_synthesized(self):
        seed, _snapshot, rows = tv_fixture()
        row = rows[seed["catalog"]["episodes"][-1]["id"]]
        for mutation in (lambda value: value.pop("SeriesId"), lambda value: value.update(SeasonName="Season 2"),
                         lambda value: value.update(ParentId="wrong"), lambda value: value.update(ServerId="wrong"),
                         lambda value: value.update(Path=row["path"])):
            value = dto(row, rows)
            mutation(value)
            with self.assertRaises(m.AdmissionError):
                m.validate_tv_dto(value, row, rows, "server")

    def test_season_does_not_claim_itself_as_its_parent_season(self):
        seed, _snapshot, rows = tv_fixture()
        row = rows[seed["catalog"]["seasons"][1]["id"]]
        value = dto(row, rows)
        value.update(SeasonId=row["id"], SeasonName=row["name"])
        with self.assertRaises(m.AdmissionError):
            m.validate_tv_dto(value, row, rows, "server")

    def test_detail_requires_exact_path(self):
        seed, _snapshot, rows = tv_fixture()
        row = rows[seed["catalog"]["episodes"][-1]["id"]]
        value = dto(row, rows, detail=True)
        m.validate_tv_dto(value, row, rows, "server", detail=True)
        value["Path"] += ".different"
        with self.assertRaises(m.AdmissionError):
            m.validate_tv_dto(value, row, rows, "server", detail=True)

    def test_inventory_rejects_missing_duplicate_or_foreign_items(self):
        _seed, _snapshot, rows = tv_fixture()
        value = {"Items": [dto(row, rows) for row in rows.values()], "TotalRecordCount": 6}
        m.validate_tv_inventory(value, rows, rows, "server")
        for mutate in (lambda data: data["Items"].pop(), lambda data: data["Items"].__setitem__(0, data["Items"][1]),
                       lambda data: data["Items"][0].update(Id="foreign")):
            altered = copy.deepcopy(value)
            mutate(altered)
            with self.assertRaises(m.AdmissionError):
                m.validate_tv_inventory(altered, rows, rows, "server")

    def test_two_login_delta_retains_existing_prepared_and_user_data(self):
        proof = m.reconcile_tv_source(*state_delta())
        self.assertEqual((proof["newSessions"], proof["newDevices"], proof["newActivityRows"]), (2, 2, 4))

    def test_old_rows_and_inactive_stage_are_immutable(self):
        for table in ("sessions", "devices", "activity_entries", "play_sessions", "user_item_data"):
            before, after, logins = state_delta()
            after["tables"][table][0]["unexpected"] = True
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, after, logins)
        before, after, _ = state_delta()
        with self.assertRaises(m.AdmissionError):
            m.same_snapshot(before, after, "inactive_changed")

    def test_owned_credential_must_match_token_and_be_revoked(self):
        for mutate in (lambda after, logins: after["tables"]["sessions"][1].update(revoked_at=None),
                       lambda after, logins: after["tables"]["sessions"][1].update(token_hash="wrong"),
                       lambda after, logins: logins["Q"].update(logout401=False),
                       lambda after, logins: after["tables"]["devices"][1].update(last_user_id="wrong")):
            before, after, logins = state_delta()
            mutate(after, logins)
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, after, logins)

    def test_activity_requires_exact_actor_credential_and_action(self):
        for key, value in (("actor_credential_id", "other"), ("action", "backup.requested"), ("affected_count", 0)):
            before, after, logins = state_delta()
            after["tables"]["activity_entries"][1][key] = value
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, after, logins)

    def test_sequences_cannot_hide_unused_or_foreign_allocations(self):
        for name in ("devices_id_seq", "activity_entries_id_seq", "users_seq"):
            before, after, logins = state_delta()
            after["sequences"][name]["lastValue"] = str(int(after["sequences"][name]["lastValue"]) + 1)
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, after, logins)

    def test_json_boolean_and_integer_differences_are_not_equal(self):
        for table in ("sessions", "devices", "activity_entries"):
            before, after, logins = state_delta()
            before["tables"][table][0]["nested"] = {"value": True}
            after["tables"][table][0]["nested"] = {"value": 1}
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, after, logins)
        for table, initial in (("sessions", "initialSession"), ("devices", "initialDevice")):
            before, after, logins = state_delta()
            logins["P"][initial]["nested"] = {"value": True}
            after["tables"][table][1]["nested"] = {"value": 1}
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, after, logins)
        for name in ("devices_id_seq", "activity_entries_id_seq", "users_seq"):
            before, after, logins = state_delta()
            after["sequences"][name]["isCalled"] = 1
            with self.assertRaises(m.AdmissionError):
                m.reconcile_tv_source(before, after, logins)

    def test_episode_indices_must_be_integers_not_booleans(self):
        seed, _snapshot, rows = tv_fixture()
        row = rows[seed["catalog"]["episodes"][0]["id"]]
        for key in ("IndexNumber", "ParentIndexNumber"):
            value = dto(row, rows)
            value[key] = True
            with self.assertRaises(m.AdmissionError):
                m.validate_tv_dto(value, row, rows, "server")

    def test_control_comparison_rejects_changed_or_uncovered_facts(self):
        keys = ("trees", "controlDocuments", "fixedFiles", "loadedUnits", "protected", "hostingBefore", "hostingAfter")
        expected = {key: {"nested": {"value": True}} for key in keys}
        m.validate_tv_controls(expected, copy.deepcopy(expected))
        for key in keys:
            observed = copy.deepcopy(expected)
            observed[key]["nested"]["value"] = 1
            with self.assertRaises(m.AdmissionError):
                m.validate_tv_controls(expected, observed)
        observed = copy.deepcopy(expected)
        observed.pop("fixedFiles")
        with self.assertRaises(m.AdmissionError):
            m.validate_tv_controls(expected, observed)


if SAVED:
    class SavedContractReplay(unittest.TestCase):
        def test_saved_successor_contract_and_entire_preflight_before_health(self):
            # Every source is already captured and pinned. No runtime module is loaded.
            read = lambda pin: m.parse(m.initial_read(pin["path"], pin["sha256"]))
            epoch, binding = read(m.TV_EPOCH), read(m.TV_BINDING)
            seed, baseline = read(binding["originalSeed"]), read(epoch["after"])
            transition = read(epoch["productInput"])
            value = input_value()
            value.update(runtimeHelper=epoch["runtimeHelper"], compiledCatalog=transition["compiledCatalog"])
            m.validate_input(value)
            expected = m.validate_tv_authority(value, epoch, binding, seed, transition, read(epoch["previousEpoch"]), read(m.TV_REUSED), read(m.TV_CLOSEOUT))
            catalog = read(value["compiledCatalog"])
            m.validate_catalog(catalog, read(epoch["currentSource"]["sourceManifest"]), value["compiledCatalog"])
            candidate = epoch["candidate"]
            def sql_json(database, query):
                self.assertEqual(query, m.snapshot_sql(catalog))
                self.assertIn(database, (candidate["database"], candidate["recoveryDatabase"]))
                return copy.deepcopy(baseline["source" if database == candidate["database"] else "inactiveStage"])
            inspector = SimpleNamespace(assert_target_cluster=lambda: None, database_facts=lambda slot: candidate["databases"][slot]["afterStart"],
                                        deployment_lease=lambda: epoch["lease"], sql_json=sql_json)
            io = SimpleNamespace(value=value, epoch=epoch, binding=binding, candidate=candidate, private=Path("/opt/not-written/private"),
                                 deadline=lambda cleanup=False: 100, pin=lambda: copy.deepcopy(expected), requests={"normal": 0, "cleanup": 0})
            helper = SimpleNamespace(read_checked=lambda path, sha256, limit=None: m.initial_read(path, sha256),
                                     write_json_once=lambda path, body: {"path": str(path), "sha256": m.sha(m.canonical(body))})
            job = m.TVParentAdmission(io, helper, inspector, seed, catalog, epoch=epoch, binding=binding,
                                      expected_process=expected, baseline=baseline)
            boundary = []
            job.fixture_files = lambda label: []
            job.resource_counters = lambda label: {}
            job.capture_controls = lambda label: {}
            job.health_due = lambda: boundary.append("health_boundary")
            job.preflight()
            self.assertEqual(boundary, ["health_boundary"])
            self.assertEqual(io.requests, {"normal": 0, "cleanup": 0})
            self.assertEqual(set(job.credentials), {"P", "Q"})
            self.assertEqual(job.credentials["P"]["actorId"], seed["actors"]["tv-browse"]["id"])
            self.assertEqual(len(job.tv), 6)
            self.assertEqual(job.tv[job.detail_ids[1]]["parent_id"], job.detail_ids[0])
            self.assertEqual(len(job.before["tables"]["play_sessions"]), 7)
            self.assertEqual(len(job.before["tables"]["user_item_data"]), 2)


if __name__ == "__main__":
    unittest.main()
