#!/usr/bin/env python3
"""Pure guards for the one-shot core-client controller; no runtime access."""
import copy
import importlib.util
import json
import hashlib
from pathlib import Path
import stat
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
REPLAY_MOVIE05 = "--replay-movie05-snapshot" in sys.argv
if REPLAY_MOVIE05:
    sys.argv.remove("--replay-movie05-snapshot")


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


def movie05_fixture():
    before, binding, unused = retained_fixture()
    actor = binding["actors"]["movie"]["id"]
    before["capturedAt"] = "2026-09-13T12:20:00Z"
    for identity in ("85c836316077f2acd5e4ee2ed56d675d", m.MOVIE05_AUTH):
        before["tables"]["sessions"].append({"id": identity, "user_id": actor, "kind": "emby", "device_id": "movie05-device", "revoked_at": "2026-09-13T12:10:00Z"})
    template = before["tables"]["play_sessions"][0]
    plays = []
    for identity, (state, auth, counted, position) in m.MOVIE05_HISTORY.items():
        plays.append({**copy.deepcopy(template), "id": identity, "auth_session_id": auth, "state": state, "counted": counted, "position_ticks": position,
            "device_id": "synthetic-device" if auth == m.RETAINED_AUTH else "movie05-device",
            "started_at": "2026-09-13T12:01:00Z" if counted else None, "stopped_at": None if state == "Prepared" else "2026-09-13T12:02:00Z"})
    before["tables"]["play_sessions"] = plays
    before["tables"]["user_item_data"][0].update(play_count=2, playback_position_ticks=1217878390, last_played_at="2026-09-13T12:01:00Z")
    closeout = {"kind": "audited-movie05-owned-state-closeout", "status": "owned_state_closed_client_acceptance_pending", "clientAcceptance": False,
        "inputEvidence": {"after": copy.deepcopy(m.MOVIE05_SNAPSHOT), "epoch": copy.deepcopy(m.EPOCH)}, "browserOutcome": "failed", "browserExitCode": 1, "gatewayExitCode": 0,
        "checks": {"authentication": {"allSessionsRevoked": 13}, "ownedData": {"ownedTables": 35, "oldRowsDeleted": 0, "userData": copy.deepcopy(before["tables"]["user_item_data"][0])}}}
    return before, binding, {"closeout": closeout, "snapshot": copy.deepcopy(before)}


def log_fixture():
    process = {"pid": 123, "startTicks": "1234", "bootId": "synthetic-boot", "uid": 1001, "exe": "/fixed/goby", "exeDevice": 1,
        "exeInode": 3, "cmdline": ["/fixed/goby"], "networkNamespace": "net:[1]", "cgroup": "0::/system.slice/candidate.service\n"}
    info = types.SimpleNamespace(st_mode=stat.S_IFREG | 0o600, st_uid=0, st_nlink=1, st_dev=1, st_ino=2)
    facts = m.server_log_file_facts(m.SERVER_LOG, info, info)
    raw_before = b'{"msg":"before"}\n'
    raw_after = raw_before + b'{"msg":"after"}\n'
    def snapshot(raw, label, at):
        return {"capturedAt": at, "candidateBefore": copy.deepcopy(process), "candidateAfter": copy.deepcopy(process), "file": copy.deepcopy(facts),
            "length": len(raw), "content": {"path": str(m.R / ("synthetic-server-" + label + ".raw")), "sha256": hashlib.sha256(raw).hexdigest()}}
    return process, info, snapshot(raw_before, "before", "2026-09-13T12:20:00Z"), snapshot(raw_after, "after", "2026-09-13T12:20:01Z"), raw_before, raw_after


def startup_fixture(*, successor=False, epoch_version=2):
    """Saved-artifact fixtures; these callbacks never access runtime state."""
    value = fixture()
    if successor:
        value.update(copy.deepcopy(m.HOST_STARTUP_HISTORY))
        for key, digest in m.HOST_STARTUP_TRANSPORT.items():
            value["sources"][key]["sha256"] = digest
    pin = lambda name: {"path": str(m.R / "client-host-startup-01/private" / name), "sha256": "a" * 64}
    process = {"pid": 101, "startTicks": "100", "bootId": "boot", "uid": 0, "exe": "/frozen/host", "exeDevice": 1,
               "exeInode": 2, "cmdline": ["/frozen/host"], "networkNamespace": "net:[10]", "cgroup": "0::/system.slice/host.service\n"}
    hosting = {"process": process, "listener": {"host": "127.0.0.1", "port": 28497, "socketInode": "12"},
        "unitProperties": dict.fromkeys(m.HOST_UNIT_FIELDS, "fixture"), "packageSha256": "b" * 64, "executableSha256": "c" * 64,
        "serverId": "d" * 32, "version": "4.9.5.0", "serverName": "Goby Core AV Original Client Host 01"}
    candidate = {**process, "pid": 102, "exe": "/frozen/goby", "cmdline": ["/frozen/goby"], "cgroup": "0::/system.slice/candidate.service\n"}
    epoch = {"version": epoch_version, "runtimeHelper": copy.deepcopy(value["runtimeHelper"]), "candidateProcess": candidate,
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
    if successor:
        epoch.update(kind="audited-candidate-runtime-epoch", operationKind="environment_revision",
                     transitionInput=pin("environment-input.json"), previousEpoch=pin("prior-binary-epoch.json"))
        historical_binding = {"kind": "audited-candidate-seed-runtime-binding", "version": 2,
            "runtimeEpoch": copy.deepcopy(m.HOST_STARTUP_HISTORY["runtimeEpoch"]), "previousBinding": pin("prior-binding.json")}
        configuration = {"kind": "audited-candidate-environment-revision-input", "version": 1,
            "runtimeHelper": copy.deepcopy(m.HOST_STARTUP_HISTORY["runtimeHelper"]), "previousEpoch": copy.deepcopy(epoch["previousEpoch"]),
            "previousSeedBinding": copy.deepcopy(historical_binding["previousBinding"])}
        current = copy.deepcopy(epoch)
        current.update(version=3, operationKind="binary_successor", previousEpoch=copy.deepcopy(m.HOST_STARTUP_HISTORY["runtimeEpoch"]),
                       transitionInput=pin("successor-input.json"), productInput=pin("successor-input.json"),
                       configurationInput=copy.deepcopy(epoch["transitionInput"]), runtimeHelper=pin("successor-runtime.py"))
        current["candidateProcess"]["pid"] += 100
        current["candidateProcess"]["startTicks"] = "200"
        current["candidate"]["listener"]["socketInode"] = "113"
        product = {"kind": "audited-candidate-transition-input", "version": 2,
            "previousEpoch": copy.deepcopy(m.HOST_STARTUP_HISTORY["runtimeEpoch"]), "previousBinding": copy.deepcopy(m.HOST_STARTUP_HISTORY["seedBinding"])}
        historical_hosting = copy.deepcopy(hosting)
        documents.update({m.HOST_STARTUP_HISTORY["runtimeEpoch"]["path"]: epoch,
            m.HOST_STARTUP_HISTORY["seedBinding"]["path"]: historical_binding,
            m.HOST_STARTUP_HISTORY["hostingInitialization"]["path"]: report,
            epoch["transitionInput"]["path"]: configuration, current["transitionInput"]["path"]: product,
            report["hosting"]["path"]: historical_hosting})
        value.update(runtimeEpoch=pin("successor-epoch.json"), seedBinding=pin("successor-binding.json"), runtimeHelper=copy.deepcopy(current["runtimeHelper"]))
        context = {"report": report, "initialization": initialization, "documents": documents, "raw_files": raw_files,
            "epoch": current, "historical_epoch": epoch, "historical_binding": historical_binding,
            "hosting": hosting, "historical_hosting": historical_hosting, "value": value,
            "lineage": {"productEpoch": current, "productInput": product, "configurationInput": configuration}}
        context["check"] = lambda: m.hosting_initialized(context["report"], value, hosting, current,
            lambda row: documents[row["path"]], lambda row: raw_files[row["path"]], tables, lineage=context["lineage"])
        return context
    check = lambda: m.hosting_initialized(report, value, hosting, epoch, lambda row: documents[row["path"]], lambda row: raw_files[row["path"]], tables)
    return report, initialization, documents, raw_files, check


class Guards(unittest.TestCase):
    def test_version3_requires_movie05_or_null_without_changing_legacy_versions(self):
        for scenario in m.SCENARIOS:
            value = fixture()
            value.update(version=3, scenario=scenario, retainedBaseline=copy.deepcopy(m.MOVIE05_BASELINE) if scenario == "movie" else None)
            self.assertIs(m.validate_input(value), value)
            changed = copy.deepcopy(value)
            changed["retainedBaseline"] = None if scenario == "movie" else copy.deepcopy(m.MOVIE05_BASELINE)
            with self.subTest(scenario=scenario), self.assertRaisesRegex(m.RunError, "version3_retained_input_authority"):
                m.validate_input(changed)
        old = fixture()
        old.update(version=2, retainedBaseline=copy.deepcopy(m.RETAINED_BASELINE))
        m.validate_input(old)
        with self.assertRaises(m.RunError):
            m.validate_input({**old, "version": 3})

    def test_movie05_baseline_preserves_all_five_plays_and_checks_every_pruning_deadline(self):
        before, binding, retained = movie05_fixture()
        self.assertEqual(m.validate_movie05_baseline(retained["closeout"], before, binding)["id"], m.MOVIE05_PREPARED)
        m.verify_actor_before(before, binding, "movie", retained, 3)
        for mutate in (lambda row: row["tables"]["play_sessions"].pop(),
                       lambda row: row["tables"]["play_sessions"][0].update(state="Prepared"),
                       lambda row: row["tables"]["sessions"][-1].update(revoked_at=None),
                       lambda row: row["tables"]["user_item_data"][0].update(play_count=0),
                       lambda row: row["tables"]["client_playback_references"].append({"id": "unknown"})):
            changed = copy.deepcopy(before)
            mutate(changed)
            with self.assertRaises(m.RunError):
                m.validate_movie05_baseline(retained["closeout"], changed, binding)
        changed = copy.deepcopy(before)
        changed["tables"]["play_sessions"][-1]["player_state"]["CanSeek"] = True
        with self.assertRaisesRegex(m.RunError, "retained_movie_fresh_state_changed"):
            m.verify_actor_before(changed, binding, "movie", retained, 3)
        expired = copy.deepcopy(before)
        expired["tables"]["play_sessions"][-1]["expires_at"] = "2026-09-06T00:00:00Z"
        retained["snapshot"] = copy.deepcopy(expired)
        with self.assertRaisesRegex(m.RunError, "retained_movie_pruning_deadline"):
            m.verify_actor_before(expired, binding, "movie", retained, 3)

    def test_server_log_metadata_requires_fixed_root_file_and_matching_stdout(self):
        unused, info, before, unused_after, raw, unused_raw = log_fixture()
        self.assertEqual(before["file"]["device"], "1")
        self.assertEqual(before["file"]["mode"], 0o600)
        for fields in ({"st_ino": 3}, {"st_dev": 2}, {"st_uid": 1001}, {"st_nlink": 2}, {"st_mode": stat.S_IFREG | 0o640}, {"st_mode": stat.S_IFIFO | 0o600}):
            wrong = types.SimpleNamespace(**{**vars(info), **fields})
            with self.subTest(fields=fields), self.assertRaisesRegex(m.RunError, "server_log_file_authority"):
                m.server_log_file_facts(m.SERVER_LOG, info, wrong)
        with self.assertRaises(m.RunError):
            m.server_log_file_facts("/tmp/another.log", info, info)

    def test_server_log_prefix_guard_rejects_rotation_process_change_truncation_and_partial_lines(self):
        process, unused, before, after, raw_before, raw_after = log_fixture()
        m.validate_server_log_pair(process, before, after, raw_before, raw_after)
        for mutate in (lambda row: row["file"].update(inode="3"), lambda row: row["candidateAfter"].update(startTicks="other"),
                       lambda row: row.update(length=row["length"]-1), lambda row: row.update(capturedAt="2026-09-13T12:19:59Z"),
                       lambda row: row["file"].update(uid=False)):
            changed = copy.deepcopy(after)
            mutate(changed)
            with self.assertRaises(m.RunError):
                m.validate_server_log_pair(process, before, changed, raw_before, raw_after)
        for raw in (raw_before[:-1], b'X'+raw_after[1:]):
            changed = copy.deepcopy(after)
            changed["length"] = len(raw)
            changed["content"]["sha256"] = hashlib.sha256(raw).hexdigest()
            with self.assertRaises(m.RunError):
                m.validate_server_log_pair(process, before, changed, raw_before, raw)
        with self.assertRaisesRegex(m.RunError, "server_log_prefix_incomplete"):
            m.validate_server_log_read(process, process, process, before["file"], before["file"], raw_before, m.SERVER_LOG_LIMIT+1)

    def test_server_log_is_saved_after_source_after_even_when_browser_exit_is_one(self):
        value = fixture()
        value.update(version=3, retainedBaseline=copy.deepcopy(m.MOVIE05_BASELINE))
        job = make_job(value)
        source, job.binding, job.retained = movie05_fixture()
        actor = job.binding["actors"]["movie"]
        actor["credentials"] = {"path": str(m.R / "synthetic-credential.json"), "sha256": "a"*64}
        job.binding["serverId"] = "b"*32
        process, unused, before, after, raw_before, raw_after = log_fixture()
        candidate = {**process, "listener": {"host": "127.0.0.1", "port": 19181, "socketInode": "4"}}
        job.epoch = {"candidateProcess": process, "candidate": {"publicUrl": "http://127.0.0.1:19180", "directUrl": "http://127.0.0.1:19181", "database": "synthetic"},
            "currentSource": {"sourceManifest": {"sha256": "c"*64}, "binary": {"sha256": "d"*64}}}
        job.gateway = types.SimpleNamespace(PROCESS_FIELDS=set(process))
        job.hosting = {"process": process, "listener": candidate["listener"], "executableSha256": "e"*64}
        job.io = types.SimpleNamespace(pin=lambda: candidate)
        job.preflight, job.open, job.verify_hosting, job.wait_client = Mock(), Mock(), Mock(), Mock()
        events = []
        pin = {"path": str(m.R / "synthetic.json"), "sha256": "f"*64}
        def save(name, content):
            events.append(name)
            return {"path": str(job.private / name), "sha256": "f"*64}
        def sample(label):
            events.append("source_"+label)
            return source, pin, candidate, {"pid": 456}, {"owned": True}
        def capture(label):
            events.append("log_"+label)
            return (before, raw_before) if label == "before" else (after, raw_after)
        job.save, job.source_sample, job.capture_server_log = save, sample, capture
        job.start_worker = lambda role, argv: events.append("start_"+role)
        job.close_worker = lambda role: events.append("close_"+role)
        job.wait_gateway = lambda: ({"process": process, "listener": {"host": "127.0.0.1", "port": 19180, "socketInode": "5"}}, pin)
        closed = {"unit": {"MainPID": "0"}, "workerPidAbsent": True, "recursiveCgroupPids": []}
        job.workers = {"browser": {"closure": closed, "terminal": {"Result": "exit-code", "ExecMainStatus": "1"}},
                       "gateway": {"closure": closed, "terminal": {"Result": "success", "ExecMainStatus": "0"}}}
        with self.assertRaisesRegex(m.RunError, "browser_worker_failed"):
            job.run()
        self.assertLess(events.index("log_before"), events.index("start_gateway"))
        self.assertLess(events.index("close_gateway"), events.index("source_after"))
        self.assertLess(events.index("source_after"), events.index("log_after"))
        self.assertLess(events.index("log_after"), events.index("server-log.json"))
        self.assertIsNotNone(job.server_log_pin)

    if REPLAY_MOVIE05:
        def test_saved_movie05_snapshot_offline_replay(self):
            def read(pin):
                raw = Path(pin["path"]).read_bytes()
                self.assertEqual(hashlib.sha256(raw).hexdigest(), pin["sha256"])
                return json.loads(raw)
            closeout, saved, binding = read(m.MOVIE05_BASELINE), read(m.MOVIE05_SNAPSHOT), read(m.BINDING)
            retained = {"closeout": closeout, "snapshot": saved}
            m.verify_actor_before(copy.deepcopy(saved), binding, "movie", retained, 3)
            changed = copy.deepcopy(saved)
            changed["tables"]["play_sessions"][0]["counted"] = True
            with self.assertRaises(m.RunError):
                m.verify_actor_before(changed, binding, "movie", retained, 3)

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

    def test_successor_hosting_uses_fixed_history_with_a_different_current_candidate(self):
        for version in (1, 2):
            startup_fixture(epoch_version=version)[-1]()
        value = startup_fixture(successor=True)
        self.assertNotEqual(value["epoch"]["candidateProcess"], value["historical_epoch"]["candidateProcess"])
        self.assertNotEqual(value["value"]["runtimeHelper"], m.HOST_STARTUP_HISTORY["runtimeHelper"])
        before = copy.deepcopy(value["report"])
        with patch.dict(m.FROZEN, {"gateway": "0" * 64, "proxy": "1" * 64}):
            value["check"]()
        self.assertEqual(value["report"], before)

    def test_successor_hosting_rejects_missing_mixed_or_unanchored_history(self):
        for failure in ("parent", "lineage-missing", "lineage-extra", "lineage-duplicate", "lineage-current", "old-binding", "old-runtime", "config-runtime",
                        "config-version", "binding-epoch", "startup-pin", "current-runtime", "hosting", "input-binding", "transport"):
            value = startup_fixture(successor=True)
            if failure == "parent":
                value["epoch"]["previousEpoch"]["sha256"] = "0" * 64
            elif failure == "lineage-missing":
                value["lineage"] = None
            elif failure == "lineage-extra":
                value["lineage"]["anotherEpoch"] = value["historical_epoch"]
            elif failure == "lineage-duplicate":
                value["lineage"]["productEpoch"] = [value["epoch"], value["epoch"]]
            elif failure == "lineage-current":
                value["lineage"]["productEpoch"] = copy.deepcopy(value["epoch"])
                value["lineage"]["productEpoch"]["candidateProcess"]["pid"] += 1
            elif failure == "old-binding":
                value["lineage"]["productInput"]["previousBinding"]["sha256"] = "0" * 64
            elif failure == "old-runtime":
                value["historical_epoch"]["runtimeHelper"]["sha256"] = "0" * 64
            elif failure == "config-runtime":
                value["lineage"]["configurationInput"]["runtimeHelper"]["sha256"] = "0" * 64
            elif failure == "config-version":
                value["lineage"]["configurationInput"]["version"] = True
            elif failure == "binding-epoch":
                value["historical_binding"]["runtimeEpoch"]["sha256"] = "0" * 64
            elif failure == "startup-pin":
                value["value"]["hostingInitialization"]["sha256"] = "0" * 64
            elif failure == "current-runtime":
                value["value"]["runtimeHelper"]["sha256"] = "0" * 64
            elif failure == "hosting":
                value["hosting"]["process"]["pid"] += 1
            elif failure == "input-binding":
                value["initialization"]["seedBinding"]["sha256"] = "0" * 64
            else:
                value["initialization"]["gateway"]["sha256"] = "0" * 64
            with self.subTest(failure=failure), self.assertRaisesRegex(m.RunError, "hosting_initialization_"):
                value["check"]()

    def test_successor_hosting_still_checks_every_old_receipt_internally(self):
        for failure in ("candidate-before", "candidate-after", "postgres", "lease", "table", "sequence", "index-body", "redirect", "live-token", "retry"):
            value = startup_fixture(successor=True)
            report, documents = value["report"], value["documents"]
            if failure == "candidate-before":
                report["candidateBefore"]["pid"] = value["epoch"]["candidateProcess"]["pid"]
            elif failure == "candidate-after":
                report["candidateAfter"]["pid"] += 1
            elif failure == "postgres":
                report["postgresAfter"]["pid"] += 1
            elif failure == "lease":
                report["leaseAfter"]["backendPid"] += 1
            elif failure == "table":
                documents[report["sourceAfter"]["path"]]["tables"]["users"].append({"id": "unexpected"})
            elif failure == "sequence":
                documents[report["sourceAfter"]["path"]]["sequences"]["fixture_sequence"] += 1
            elif failure == "index-body":
                documents[report["webIndex"]["response"]["path"]]["bodyRead"] = True
            elif failure == "redirect":
                web = documents[report["webIndex"]["response"]["path"]]
                web["headers"].append(["Location", "index.html?start=wizard"])
                value["raw_files"][web["rawHeaders"]["path"]] = b"HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nLocation: index.html?start=wizard\r\n\r\n"
            elif failure == "live-token":
                documents[report["cleanup"][0]["sameTokenStatusResponse"]["path"]]["status"] = 200
            else:
                report["automaticWriteRetry"] = True
            with self.subTest(failure=failure), self.assertRaisesRegex(m.RunError, "hosting_initialization_"):
                value["check"]()

    def test_successor_hosting_rejects_nested_boolean_as_integer_in_the_passed_report(self):
        value = startup_fixture(successor=True)
        saved = value["documents"][m.HOST_STARTUP_HISTORY["hostingInitialization"]["path"]]
        value["report"] = copy.deepcopy(saved)
        value["report"]["preservation"]["sequencesExact"] = 1
        self.assertIs(saved["preservation"]["sequencesExact"], True)
        # Ordinary Python equality would erase this nested type change.
        self.assertEqual(value["report"], saved)
        with self.assertRaisesRegex(m.RunError, "hosting_initialization_history_changed"):
            value["check"]()

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
