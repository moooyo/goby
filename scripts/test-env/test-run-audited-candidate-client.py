#!/usr/bin/env python3
"""Pure guards for the one-shot core-client controller; no runtime access."""
import copy
import importlib.util
import json
import hashlib
from pathlib import Path
import sys
import types
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("client_run", Path(__file__).with_name("run-audited-candidate-client.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
REPLAY_SAVED = "--replay-retained-snapshot" in sys.argv
if REPLAY_SAVED:
    sys.argv.remove("--replay-retained-snapshot")


def fixture():
    sources = {key: {"path": str(m.R / "av-tool-verification-04/sources" / filename), "sha256": m.FROZEN.get(key, "d" * 64)}
               for key, filename in m.SOURCE_FILES.items()}
    return {"kind": "audited-candidate-client-run-input", "version": 1, "runId": "movie-01", "scenario": "movie",
        "output": str(m.R / "candidate-core-client-movie-01"), "runtimeEpoch": copy.deepcopy(m.EPOCH), "seedBinding": copy.deepcopy(m.BINDING),
        "admission": copy.deepcopy(m.ADMISSION), "admissionCloseout": copy.deepcopy(m.ADMISSION_CLOSEOUT), "hosting": copy.deepcopy(m.HOSTING),
        "hostingInitialization": {"path": str(m.R / "client-host-startup-01/private/report.json"), "sha256": "a" * 64},
        "avVerification": copy.deepcopy(m.AV_VERIFICATION), "runtimeHelper": copy.deepcopy(m.RUNTIME), "node": copy.deepcopy(m.NODE),
        "compiledCatalog": {"path": str(m.R / "compiled-catalog.json"), "sha256": "c" * 64},
        "sources": sources, "budgets": copy.deepcopy(m.BUDGETS), "gatewayBudgets": copy.deepcopy(m.GATEWAY_BUDGETS)}


def make_job(value=None):
    return m.ClientRun(types.SimpleNamespace(), value or fixture(), {"path": str(m.R / "input.json"), "sha256": "e" * 64},
                       {"path": str(m.R / "run-audited-candidate-client.py"), "sha256": "f" * 64})


def retained_fixture():
    actor, item, control = "a" * 32, "c" * 32, "b" * 32
    binding = {"actors": {"movie": {"id": actor, "username": "synthetic-movie"}}, "catalog": {"movie": {"id": item, "runtimeTicks": 6000000000}}}
    tables = "activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users".split()
    before = {"capturedAt": "2026-09-13T11:21:00Z", "tables": {name: [] for name in tables}, "sequences": {"synthetic": {"lastValue": "1", "isCalled": True}}}
    before["tables"]["users"] = [{"id": actor, "name": "synthetic-movie", "is_administrator": False, "is_disabled": False, "policy": {}}]
    before["tables"]["sessions"] = [{"id": str(i), "user_id": control, "revoked_at": "2026-09-13T11:20:00Z"} for i in range(10)] + [
        {"id": m.RETAINED_AUTH, "user_id": actor, "device_id": "synthetic-device", "kind": "emby", "revoked_at": "2026-09-13T11:20:00Z"}]
    before["tables"]["play_sessions"] = [{"id": m.RETAINED_PLAY, "auth_session_id": m.RETAINED_AUTH, "user_id": actor, "item_id": item,
        "device_id": "synthetic-device", "media_source_id": "mediasource_" + item, "duration_ticks": 6000000000, "application_client_id": None,
        "state": "Prepared", "counted": False, "started_at": None, "stopped_at": None, "position_ticks": 0, "player_state": {},
        "created_at": "2026-09-13T11:15:00Z", "updated_at": "2026-09-13T11:15:00Z", "expires_at": "2026-09-13T11:45:00Z", "client_correlated": False}]
    before["tables"]["user_item_data"] = [{"user_id": actor, "item_id": item, "playback_position_ticks": 0, "play_count": 0,
        "is_favorite": False, "played": False, "last_played_at": None, "updated_at": "2026-09-13T11:15:00Z"}]
    closeout = {"kind": "audited-core-movie04-failure-closeout", "status": "closed_failed_attempt_with_retained_unstarted_preparation",
        "runtimeEpoch": copy.deepcopy(m.EPOCH), "sourceAfter": copy.deepcopy(m.RETAINED_SNAPSHOT), "scenario": "movie", "runId": "movie-04",
        "browserAndGatewayClosed": True, "allElevenSessionsRevoked": True, "clientAcceptance": False, "playbackStarted": False,
        "clientPlaybackReferences": 0, "encodingJobs": 0, "retainedPreparation": {"id": m.RETAINED_PLAY, "authSessionId": m.RETAINED_AUTH, "itemId": item, "ownerCredentialRevoked": True}}
    return before, binding, {"closeout": closeout, "snapshot": copy.deepcopy(before)}


def startup_fixture():
    """Saved-artifact fixtures; these callbacks never access runtime state."""
    value = fixture()
    pin = lambda name: {"path": str(m.R / "client-host-startup-01/private" / name), "sha256": "a" * 64}
    process = {"pid": 101, "startTicks": "100", "bootId": "boot", "uid": 0, "exe": "/frozen/host", "exeDevice": 1,
               "exeInode": 2, "cmdline": ["/frozen/host"], "networkNamespace": "net:[10]", "cgroup": "0::/system.slice/host.service\n"}
    hosting = {"process": process, "listener": {"host": "127.0.0.1", "port": 28497, "socketInode": "12"},
        "unitProperties": dict.fromkeys(m.HOST_UNIT_FIELDS, "fixture"), "packageSha256": "b" * 64, "executableSha256": "c" * 64,
        "serverId": "d" * 32, "version": "4.9.5.0", "serverName": "Goby Core AV Original Client Host 01"}
    candidate = {**process, "pid": 102, "exe": "/frozen/goby", "cmdline": ["/frozen/goby"], "cgroup": "0::/system.slice/candidate.service\n"}
    epoch = {"runtimeHelper": copy.deepcopy(m.RUNTIME), "candidateProcess": candidate,
        "candidate": {"listener": {"port": 28498, "socketInode": "13"}}, "postgresProcess": {**process, "pid": 103}, "lease": {"backendPid": 104}}
    anchor = {**candidate, "listener": {"host": "127.0.0.1", "port": 28498, "socketInode": "13"}}
    host_anchor = {key: copy.deepcopy(hosting[key]) for key in ("process", "listener", "packageSha256", "executableSha256")}
    host_anchor["unit"] = copy.deepcopy(hosting["unitProperties"])
    tables = set("activity_entries application_key_clients application_key_devices application_keys catalog_entities client_playback_references devices encoding_jobs extra_reserved_paths item_entities item_extra_resources item_images item_metadata_state item_subtitles item_theme_resources items libraries library_roots managed_settings play_sessions scan_jobs schema_migrations server_settings sessions task_definitions task_occurrences task_run_children task_run_requests task_runs task_triggers theme_owner_ids theme_reserved_paths user_item_data user_settings users".split())
    before = {"capturedAt": "2026-09-13T00:00:00Z", "tables": {name: [] for name in tables}, "sequences": {"fixture_sequence": 7}}
    after = copy.deepcopy(before)
    after["capturedAt"] = "2026-09-13T00:00:01Z"
    initialization = {"kind": "audited-original-client-host-startup-input", "version": 1, "output": str(m.R / "client-host-startup-01"),
        **{key: copy.deepcopy(value[key]) for key in ("hosting", "runtimeEpoch", "seedBinding", "runtimeHelper", "compiledCatalog")},
        "gateway": copy.deepcopy(value["sources"]["gateway"]), "proxy": copy.deepcopy(value["sources"]["proxy"]), "budgets": copy.deepcopy(m.HOST_STARTUP_BUDGETS)}
    raw = b"HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n"
    web = {"status": 200, "headers": [["Content-Type", "text/html"]], "headersComplete": True, "bodyRead": False,
           "bodyComplete": None, "rawHeaders": pin("index-headers.bin")}
    report = {"kind": "audited-original-client-host-startup", "version": 1, "status": "ready_for_core_client", "failure": None,
        "cleanupFailures": [], "wizardCompleted": True, "input": pin("input.json"), "helper": {"path": str(m.R / "host-startup-tool-verification-01/initialize-audited-client-host.py"), "sha256": "e" * 64},
        **{key: copy.deepcopy(value[key]) for key in ("hosting", "runtimeEpoch", "seedBinding")}, "credentials": pin("credentials.json"),
        "sourceBefore": pin("before.json"), "sourceAfter": pin("after.json"), "hostingBefore": copy.deepcopy(host_anchor), "hostingAfter": copy.deepcopy(host_anchor),
        "candidateBefore": copy.deepcopy(anchor), "candidateAfter": copy.deepcopy(anchor), "postgresBefore": copy.deepcopy(epoch["postgresProcess"]),
        "postgresAfter": copy.deepcopy(epoch["postgresProcess"]), "leaseBefore": copy.deepcopy(epoch["lease"]), "leaseAfter": copy.deepcopy(epoch["lease"]),
        "preservation": {"ownedTablesExact": 35, "sequencesExact": True, "candidateContinuous": True, "postgresContinuous": True, "leaseExact": True, "hostingContinuous": True},
        "publicIdentity": {"id": hosting["serverId"], "version": hosting["version"], "serverName": hosting["serverName"]},
        "networkConfiguration": copy.deepcopy(m.HOST_NETWORK_CONFIGURATION), "users": {"count": 1, "adminId": "f" * 32, "adminName": "goby-client-host-admin-01"},
        "libraries": {"count": 0}, "webIndex": {"status": 200, "location": None, "bodyRead": False, "response": pin("index-response.json")},
        "cleanup": [{"tokenSha256": "1" * 64, "logoutStatus": 204, "sameTokenStatus": 401, "logoutStatusResponse": pin("logout.json"), "sameTokenStatusResponse": pin("revoked.json")}],
        "requests": {"normal": 11, "cleanup": 2}, "elapsedMilliseconds": 1000, "budgets": copy.deepcopy(m.HOST_STARTUP_BUDGETS),
        "automaticWriteRetry": False, "servicesStartedOrStopped": 0, "gobyBusinessHttpRequests": 0}
    documents = {report["input"]["path"]: initialization, report["sourceBefore"]["path"]: before, report["sourceAfter"]["path"]: after,
        report["webIndex"]["response"]["path"]: web, pin("logout.json")["path"]: {"status": 204, "complete": True},
        pin("revoked.json")["path"]: {"status": 401, "complete": True}}
    raw_files = {web["rawHeaders"]["path"]: raw}
    check = lambda: m.hosting_initialized(report, value, hosting, epoch, lambda row: documents[row["path"]], lambda row: raw_files[row["path"]], tables)
    return report, initialization, documents, raw_files, check


class Guards(unittest.TestCase):
    def test_retained_version_is_movie_only_and_preserves_legacy_input(self):
        value = fixture()
        value.update(version=2, retainedBaseline=copy.deepcopy(m.RETAINED_BASELINE))
        self.assertIs(m.validate_input(value), value)
        for scenario in m.SCENARIOS - {"movie"}:
            with self.subTest(scenario=scenario), self.assertRaisesRegex(m.RunError, "retained_movie_input_authority"):
                m.validate_input({**value, "scenario": scenario})
        with self.assertRaises(m.RunError):
            m.validate_input({**value, "version": 1})
        changed = copy.deepcopy(value)
        changed["retainedBaseline"]["sha256"] = "0" * 64
        with self.assertRaises(m.RunError):
            m.validate_input(changed)

    def test_retained_exact_before_rejects_unknown_rows_and_stale_pruning_window(self):
        before, binding, retained = retained_fixture()
        m.verify_actor_before(before, binding, "movie", retained)
        with self.assertRaisesRegex(m.RunError, "scenario_actor_already_consumed"):
            m.verify_actor_before(before, binding, "movie")
        for mutation in (lambda row: row["tables"]["play_sessions"].append({"id": "unknown", "user_id": "a" * 32}),
                         lambda row: row["tables"]["play_sessions"][0].pop("player_state"),
                         lambda row: row["tables"]["user_item_data"][0].update(play_count=1),
                         lambda row: row["tables"]["sessions"][-1].update(revoked_at=None),
                         lambda row: row["sequences"]["synthetic"].update(lastValue="2")):
            changed = copy.deepcopy(before)
            mutation(changed)
            with self.assertRaisesRegex(m.RunError, "retained_movie_fresh_state_changed"):
                m.verify_actor_before(changed, binding, "movie", retained)
        stale = copy.deepcopy(before)
        stale["capturedAt"] = "2026-09-20T11:30:00Z"
        with self.assertRaisesRegex(m.RunError, "retained_movie_pruning_deadline"):
            m.verify_actor_before(stale, binding, "movie", retained)

    def test_retained_before_distinguishes_nested_json_booleans_from_numbers(self):
        for key, boolean, number in (("EnableMediaPlayback", False, 0), ("EnableAllFolders", True, 1)):
            before, binding, retained = retained_fixture()
            foreign = {"id": "c" * 32, "name": "Control", "policy": {key: boolean}}
            before["tables"]["users"].append(copy.deepcopy(foreign))
            retained["snapshot"]["tables"]["users"].append(copy.deepcopy(foreign))
            m.verify_actor_before(before, binding, "movie", retained)
            before["tables"]["users"][-1]["policy"][key] = number
            with self.subTest(key=key), self.assertRaisesRegex(m.RunError, "retained_movie_fresh_state_changed"):
                m.verify_actor_before(before, binding, "movie", retained)

    def test_retained_authority_rejects_nonzero_live_wrong_auth_and_extra_residue(self):
        for mutation in (lambda row: row["tables"]["sessions"][-1].update(revoked_at=None),
                         lambda row: row["tables"]["play_sessions"][0].update(auth_session_id="wrong"),
                         lambda row: row["tables"]["play_sessions"][0].update(counted=True),
                         lambda row: row["tables"]["user_item_data"][0].update(playback_position_ticks=1),
                         lambda row: row["tables"]["client_playback_references"].append({"user_id": "a" * 32}),
                         lambda row: row["tables"]["encoding_jobs"].append({"user_id": "a" * 32})):
            before, binding, retained = retained_fixture()
            mutation(retained["snapshot"])
            with self.assertRaises(m.RunError):
                m.validate_retained_movie_baseline(retained["closeout"], retained["snapshot"], binding)

    if REPLAY_SAVED:
        def test_saved_movie04_snapshot_offline_replay(self):
            def read(pin):
                raw = Path(pin["path"]).read_bytes()
                self.assertEqual(hashlib.sha256(raw).hexdigest(), pin["sha256"])
                return json.loads(raw)
            closeout, saved, binding = read(m.RETAINED_BASELINE), read(m.RETAINED_SNAPSHOT), read(m.BINDING)
            retained = {"closeout": closeout, "snapshot": saved}
            m.verify_actor_before(copy.deepcopy(saved), binding, "movie", retained)
            changed = copy.deepcopy(saved)
            changed["tables"]["play_sessions"][0]["counted"] = True
            with self.assertRaises(m.RunError):
                m.verify_actor_before(changed, binding, "movie", retained)

    def test_hosting_gate_rejects_incomplete_startup_redirect_and_live_token(self):
        startup_fixture()[-1]()
        for failure in ("unfinished", "redirect", "live-token"):
            report, initialization, documents, raw_files, check = startup_fixture()
            if failure == "unfinished":
                report["status"] = "startup_incomplete_resources_retained"
            elif failure == "redirect":
                # A claimed ready projection must not hide a recorded redirect.
                web = documents[report["webIndex"]["response"]["path"]]
                web["headers"].append(["Location", "index.html?start=wizard"])
                raw_files[web["rawHeaders"]["path"]] = b"HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nLocation: index.html?start=wizard\r\n\r\n"
            else:
                documents[report["cleanup"][0]["sameTokenStatusResponse"]["path"]]["status"] = 200
            with self.subTest(failure=failure), self.assertRaisesRegex(m.RunError, "hosting_initialization_"):
                check()

    def test_hosting_gate_rejects_mixed_authority_and_changed_goby_evidence(self):
        for failure in ("input-host", "epoch", "host-process", "table", "sequence"):
            report, initialization, documents, raw_files, check = startup_fixture()
            if failure == "input-host":
                initialization["hosting"]["sha256"] = "0" * 64
            elif failure == "epoch":
                report["runtimeEpoch"]["sha256"] = "0" * 64
            elif failure == "host-process":
                report["hostingAfter"]["process"]["pid"] += 1
            elif failure == "table":
                documents[report["sourceAfter"]["path"]]["tables"]["users"].append({"id": "unexpected"})
            else:
                documents[report["sourceAfter"]["path"]]["sequences"]["fixture_sequence"] += 1
            with self.subTest(failure=failure), self.assertRaisesRegex(m.RunError, "hosting_initialization_"):
                check()

    def test_exact_current_release_and_six_scenarios(self):
        for scenario in m.SCENARIOS:
            value = fixture()
            value["scenario"] = scenario
            self.assertIs(m.validate_input(value), value)
        for key in ("runtimeEpoch", "seedBinding", "admission", "admissionCloseout", "hosting", "avVerification", "runtimeHelper", "node"):
            value = fixture()
            value[key]["sha256"] = "0" * 64
            with self.subTest(key=key), self.assertRaises(m.RunError):
                m.validate_input(value)

    def test_sources_cannot_escape_or_mix_module_directories(self):
        for path in (str(m.R / "different/client-browser-playback.mjs"), "/tmp/client-browser-playback.mjs",
                     str(m.R / "candidate-core-client-movie-01/client-browser-playback.mjs")):
            value = fixture()
            value["sources"]["movie"]["path"] = path
            with self.subTest(path=path), self.assertRaises(m.RunError):
                m.validate_input(value)
        value = fixture()
        value["sources"]["adapter"]["sha256"] = "0" * 64
        with self.assertRaises(m.RunError):
            m.validate_input(value)

    def test_failed_admission_prevents_output_and_both_starts(self):
        job = make_job()
        epoch = {"helpers": {"seed": {}}, "runtimeHelper": m.RUNTIME, "currentSource": {"schema": 28}}
        report = {"kind": "audited-candidate-live-admission", "version": 2, "status": "admission_failed_resources_retained"}
        pins = {job.input_pin["path"]: job.value, m.BINDING["path"]: {}, m.ADMISSION["path"]: report, "original-seed": {}}
        seed = types.SimpleNamespace(descriptor=lambda pin: pins[pin["path"]])
        job.r = types.SimpleNamespace(read_bootstrap=lambda pin: json.dumps(epoch), load_helper=lambda key, pin: seed,
            validate_epoch=lambda value: value, validate_seed_runtime_binding=Mock(), SEED={"path": "original-seed"})
        job.open, job.start_worker = Mock(), Mock()
        with patch.object(m.os.path, "lexists", return_value=False), self.assertRaisesRegex(m.RunError, "successful_current_admission_required"):
            job.run()
        job.open.assert_not_called()
        job.start_worker.assert_not_called()

    def test_output_collision_precedes_any_reader_or_worker(self):
        job = make_job()
        job.r.read_bootstrap, job.open, job.start_worker = Mock(), Mock(), Mock()
        with patch.object(m.os.path, "lexists", return_value=True), self.assertRaisesRegex(m.RunError, "client_output_collision"):
            job.run()
        job.r.read_bootstrap.assert_not_called()
        job.open.assert_not_called()
        job.start_worker.assert_not_called()

    def test_unit_namespace_cache_and_exact_argv(self):
        job = make_job()
        for role in ("gateway", "browser"):
            command = ["/usr/bin/node", "/frozen/adapter.mjs"]
            argv, properties = m.unit_argv(job.units[role], "owned", command, job.output, role, "ssh-context")
            self.assertEqual(argv[argv.index("--") + 1:], command)
            self.assertEqual(properties["Type"], "exec")
            self.assertEqual(properties["PrivateNetwork"], "yes")
            self.assertEqual(properties["ProtectHome"], "read-only")
            self.assertEqual(properties["Restart"], "no")
            self.assertEqual(properties["ReadWritePaths"], str(job.output / role))
            self.assertIn("--setenv=HOME=/root", argv)
            self.assertIn("--setenv=PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright", argv)
            self.assertIn("--setenv=SSH_CONNECTION=ssh-context", argv)
            self.assertNotIn("CapabilityBoundingSet", properties)

    def test_actor_baseline_preserves_other_actors_but_rejects_consumed_actor(self):
        binding = {"actors": {"movie": {"id": "P"}}}
        snapshot = {"tables": {"users": [{"id": "P", "is_administrator": False, "is_disabled": False}],
            "play_sessions": [{"user_id": "another"}], "client_playback_references": []}}
        m.verify_actor_before(snapshot, binding, "movie")
        for table in ("play_sessions", "client_playback_references"):
            changed = copy.deepcopy(snapshot)
            changed["tables"][table].append({"user_id": "P"})
            with self.subTest(table=table), self.assertRaisesRegex(m.RunError, "scenario_actor_already_consumed"):
                m.verify_actor_before(changed, binding, "movie")

    def test_gateway_exit_must_be_observed_not_inferred_from_closed_pid(self):
        job = make_job()
        job.workers = {"gateway": {"terminal": None, "closure": {"workerPidAbsent": True}}}
        with self.assertRaisesRegex(m.RunError, "gateway_exit_status_not_observed"):
            job.gateway_exit()
        for terminal in ({"LoadState": "not-found"}, {"LoadState": "loaded", "Result": "signal", "ExecMainStatus": "15"}):
            job.workers["gateway"]["terminal"] = terminal
            with self.assertRaises(m.RunError):
                job.gateway_exit()
        job.workers["gateway"]["terminal"] = {"LoadState": "loaded", "Result": "success", "ExecMainStatus": "0"}
        self.assertEqual(job.gateway_exit(), 0)

    def test_terminal_must_match_original_worker_before_persistence(self):
        job = make_job()
        job.save = Mock()
        job.workers = {"browser": {"pid": 123, "terminal": None}}
        for row in ({"MainPID": "123", "ExecMainPID": "123"}, {"MainPID": "0", "ExecMainPID": "124"}):
            with self.assertRaisesRegex(m.RunError, "worker_terminal_identity_changed"):
                job.record_terminal("browser", row)
        job.save.assert_not_called()
        actual = {"MainPID": "0", "ExecMainPID": "123", "Result": "success", "ExecMainStatus": "0"}
        job.record_terminal("browser", actual)
        self.assertEqual(job.workers["browser"]["terminal"], actual)
        job.save.assert_called_once_with("browser-terminal.json", actual)

    def test_signal_rejects_reused_pid_without_attestation(self):
        state = {"unit": "owned.service", "pid": 123, "command": ["/usr/bin/python3", "-I", "-B", "/frozen/gateway.py"]}
        row = {"MainPID": "123", "ExecMainPID": "123"}
        process = {"pid": 123, "bootId": "boot", "uid": 0, "cmdline": state["command"], "cgroup": "0::/system.slice/owned.service\n", "startTicks": "1234"}
        m.verify_signal_target(state, row, process, "boot")
        for changes in ({"cgroup": "0::/system.slice/foreign.service\n"}, {"cmdline": ["/foreign"]}, {"uid": 1}):
            with self.subTest(changes=changes), self.assertRaisesRegex(m.RunError, "gateway_signal_identity_changed"):
                m.verify_signal_target(state, row, {**process, **changes}, "boot")
        with self.assertRaises(m.RunError):
            m.verify_signal_target(state, {**row, "MainPID": "0"}, process, "boot")
        state["process"] = process
        with self.assertRaises(m.RunError):
            m.verify_signal_target(state, row, {**process, "startTicks": "1235"}, "boot")


if __name__ == "__main__":
    unittest.main()
