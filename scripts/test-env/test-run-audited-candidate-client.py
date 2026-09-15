#!/usr/bin/env python3
"""Pure guards and explicitly pinned saved-state replay; no live work."""
import copy
import importlib.util
import json
import hashlib
import math
import os
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


def reviewed_replay_arguments(argv, flags=("--reviewed-baselines", "--reviewed-baselines-sha256")):
    m.need(not any(argument.startswith(flag + "=") for argument in argv for flag in flags), "reviewed_replay_arguments_invalid")
    counts = [argv.count(flag) for flag in flags]
    if counts == [0, 0]:
        return None
    m.need(counts == [1, 1], "reviewed_replay_arguments_invalid")
    positions = [argv.index(flag) for flag in flags]
    m.need(all(position + 1 < len(argv) and not argv[position + 1].startswith("-") for position in positions), "reviewed_replay_arguments_invalid")
    pin = {"path": argv[positions[0] + 1], "sha256": argv[positions[1] + 1]}
    m.descriptor(pin)
    for position in sorted(positions, reverse=True):
        del argv[position:position + 2]
    return pin


def reviewed_replay_json(raw):
    def pairs(rows):
        result = {}
        for key, value in rows:
            m.need(key not in result, "reviewed_replay_duplicate_json_key")
            result[key] = value
        return result
    def finite(token):
        value = float(token)
        m.need(math.isfinite(value), "reviewed_replay_nonfinite_json")
        return value
    def constant(unused):
        raise m.RunError("reviewed_replay_nonfinite_json")
    return json.loads(raw, object_pairs_hook=pairs, parse_float=finite, parse_constant=constant)


def read_reviewed_replay_descriptor(pin):
    m.need(sys.platform == "linux" and os.geteuid() == 0, "reviewed_replay_root_required")
    m.descriptor(pin)
    path = Path(pin["path"])
    m.need(not any(character in str(path) for character in "\r\n\x00"), "reviewed_replay_path_invalid")
    for node in (path, *path.parents):
        info = node.lstat()
        m.need(not stat.S_ISLNK(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022 and
               (stat.S_ISREG(info.st_mode) if node == path else stat.S_ISDIR(info.st_mode)), "reviewed_replay_path_authority")
    identity = lambda info: tuple(getattr(info, key) for key in
        ("st_dev", "st_ino", "st_mode", "st_uid", "st_gid", "st_nlink", "st_size", "st_mtime_ns", "st_ctime_ns"))
    before = path.lstat()
    limit = 256 << 20
    m.need(stat.S_ISREG(before.st_mode) and before.st_uid == 0 and stat.S_IMODE(before.st_mode) == 0o600 and
           before.st_nlink == 1 and 0 <= before.st_size <= limit, "reviewed_replay_file_authority")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW), "rb") as stream:
        m.need(identity(os.fstat(stream.fileno())) == identity(before), "reviewed_replay_file_changed_on_open")
        raw = stream.read(limit + 1)
        m.need(identity(os.fstat(stream.fileno())) == identity(before), "reviewed_replay_file_changed_on_read")
    m.need(identity(path.lstat()) == identity(before) and len(raw) == before.st_size and hashlib.sha256(raw).hexdigest() == pin["sha256"],
           "reviewed_replay_file_bytes_changed")
    return reviewed_replay_json(raw)


def load_reviewed_replay_cases(pin):
    value = read_reviewed_replay_descriptor(pin)
    m.need(isinstance(value, dict) and set(value) == {"kind", "version", "cases"} and
           value["kind"] == "audited-candidate-reviewed-baseline-replay-input" and type(value["version"]) is int and value["version"] == 1 and
           isinstance(value["cases"], list) and len(value["cases"]) == 3, "reviewed_replay_cases_schema")
    for row, name in zip(value["cases"], ("movie05", "movie06", "latest-subtitles")):
        m.need(isinstance(row, dict) and set(row) == {"name", "review", "seed", "inputBinding"} and row["name"] == name and
               isinstance(row["inputBinding"], dict) and set(row["inputBinding"]) == {"runtimeEpoch", "seedBinding"}, "reviewed_replay_case_schema")
        for descriptor in (row["review"], row["seed"], *row["inputBinding"].values()):
            m.descriptor(descriptor)
        m.need(m.canonical(row["seed"]) == m.canonical(row["inputBinding"]["seedBinding"]), "reviewed_replay_seed_binding")
    return value["cases"]


def load_reviewed_tv_replay_input(pin):
    value = read_reviewed_replay_descriptor(pin)
    m.need(isinstance(value, dict) and set(value) == {"kind", "version", "review", "seed", "inputBinding"} and
           value["kind"] == "audited-candidate-reviewed-tv-baseline-replay-input" and type(value["version"]) is int and value["version"] == 1 and
           isinstance(value["inputBinding"], dict) and set(value["inputBinding"]) == {"runtimeEpoch", "seedBinding"}, "reviewed_tv_replay_input_schema")
    for pin in (value["review"], value["seed"], *value["inputBinding"].values()):
        m.descriptor(pin)
    m.need(m.canonical(value["seed"]) == m.canonical(value["inputBinding"]["seedBinding"]), "reviewed_tv_replay_seed_binding")
    return value


def load_episode_provenance_replay_input(pin):
    value = read_reviewed_replay_descriptor(pin)
    m.need(isinstance(value, dict) and set(value) == {"kind", "version", "scenario", "seed", "provenance"} and
           value["kind"] == "audited-candidate-episode-provenance-replay-input" and type(value["version"]) is int and value["version"] == 1 and
           value["scenario"] == "episode" and isinstance(value["provenance"], dict) and set(value["provenance"]) == m.FINAL_PROVENANCE_KEYS,
           "episode_provenance_replay_input_schema")
    for selected in (value["seed"], *value["provenance"].values()):
        m.descriptor(selected)
    return value


REVIEWED_REPLAY_PIN = reviewed_replay_arguments(sys.argv)
REVIEWED_REPLAY_CASES = load_reviewed_replay_cases(REVIEWED_REPLAY_PIN) if REVIEWED_REPLAY_PIN is not None else None
REVIEWED_TV_REPLAY_FLAGS = ("--reviewed-tv-baseline", "--reviewed-tv-baseline-sha256")
REVIEWED_TV_REPLAY_PIN = reviewed_replay_arguments(sys.argv, REVIEWED_TV_REPLAY_FLAGS)
REVIEWED_TV_REPLAY_INPUT = load_reviewed_tv_replay_input(REVIEWED_TV_REPLAY_PIN) if REVIEWED_TV_REPLAY_PIN is not None else None
EPISODE_PROVENANCE_REPLAY_FLAGS = ("--episode-provenance", "--episode-provenance-sha256")
EPISODE_PROVENANCE_REPLAY_PIN = reviewed_replay_arguments(sys.argv, EPISODE_PROVENANCE_REPLAY_FLAGS)
EPISODE_PROVENANCE_REPLAY_INPUT = load_episode_provenance_replay_input(EPISODE_PROVENANCE_REPLAY_PIN) if EPISODE_PROVENANCE_REPLAY_PIN is not None else None


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


def native_verification_fixture():
    old = {"kind": "tv-parent-candidate-client-component-verification", "version": 1, "passed": True,
        "sourceUnchanged": True, "browserStarted": False, "businessHttp": False,
        "adapterCounts": {"tests": 55, "pass": 55, "fail": 0, "skipped": 0}, "adapterSavedReplayChecks": 7,
        "closerCounts": {"testCount": 32, "passed": 32, "failed": 0, "sourceUnchanged": True},
        "version3Counts": {"tests": 12, "pass": 12, "fail": 0, "skipped": 0},
        "savedMovie05ReplayCounts": {"testCount": 13, "passed": 13, "failed": 0, "sourceUnchanged": True},
        "movieCounts": {"tests": 19, "pass": 19, "fail": 0, "skipped": 0},
        "sourcePins": {m.SOURCE_FILES[key]: checksum for key, checksum in m.REUSED_AV_SOURCES.items()}}
    # A future current selection must not change the historical fixture.
    selected = {**m.REUSED_AV_SOURCES, "adapter": "a" * 64, "subtitles": "b" * 64}
    current = {"kind": "candidate-native-rejection-component-verification", "version": 1, "passed": True,
        "sourceUnchanged": True, "browserStarted": True, "businessHttp": False, "syntheticBrowserRuns": 1, "originalClientRuns": 0,
        "verificationWorker": {"closed": True}, "freshComponents": ["adapter", "subtitles"],
        "reusedComponents": copy.deepcopy(m.REUSED_AV_VERIFICATION), "reusedChecks": ["closer", "version3", "savedMovie05", "movie"],
        "adapterCounts": {"tests": 55, "pass": 55, "fail": 0, "skipped": 0}, "adapterSavedReplayChecks": 7,
        "nativeCounts": {"testCount": 6, "passed": 6, "failed": 0, "sourceUnchanged": True},
        "subtitleCounts": {"tests": 3, "pass": 3, "fail": 0, "skipped": 0},
        "sourcePins": {m.SOURCE_FILES[key]: checksum for key, checksum in selected.items()}}
    return current, old, selected


def make_job(value=None):
    return m.ClientRun(types.SimpleNamespace(), value or fixture(), {"path": str(m.R / "input.json"), "sha256": "e" * 64},
                       {"path": str(m.R / "run-audited-candidate-client.py"), "sha256": "f" * 64})


def successor_admission_fixture():
    pin = lambda name, digit: {"path": str(m.R / "synthetic-admission" / name), "sha256": digit * 64}
    old_source = {"archiveSha256": "1" * 64, "sourceManifest": pin("old-source.json", "2"),
        "binary": pin("old-goby", "3"), "fullReport": pin("old-full.json", "4"), "schema": 28}
    old = {"kind": "audited-candidate-live-admission", "version": 2, "status": "admitted_for_core_client",
        "candidateAdmissionComplete": True, "failure": None, "cleanupFailures": [],
        "runtimeEpoch": copy.deepcopy(m.HOST_STARTUP_HISTORY["runtimeEpoch"]),
        "seedRuntimeBinding": copy.deepcopy(m.HOST_STARTUP_HISTORY["seedBinding"]), "currentSource": old_source}
    value = fixture()
    value.update(runtimeEpoch=pin("new-epoch.json", "5"), seedBinding=pin("new-binding.json", "6"))
    source = {**copy.deepcopy(old_source), "binary": pin("new-goby", "7"), "fullReport": pin("new-full.json", "8")}
    epoch = {"version": 3, "operationKind": "binary_successor", "previousEpoch": copy.deepcopy(old["runtimeEpoch"]),
        "transitionInput": pin("transition-input.json", "9"), "productInput": pin("transition-input.json", "9"), "currentSource": source}
    product = {"kind": "audited-candidate-transition-input", "version": 2,
        "previousEpoch": copy.deepcopy(old["runtimeEpoch"]), "previousBinding": copy.deepcopy(old["seedRuntimeBinding"])}
    report = {**copy.deepcopy(old), "version": 3, "admissionKind": "affected_tv_parent", "runtimeEpoch": copy.deepcopy(value["runtimeEpoch"]),
        "seedRuntimeBinding": copy.deepcopy(value["seedBinding"]), "currentSource": copy.deepcopy(source),
        "transitionCloseout": copy.deepcopy(m.AFFECTED_TV_PARENT_CLOSEOUT),
        "freshChecks": dict.fromkeys(["runtimeIdentity", "tvDefaultParents", "tvDetailParents", "ordinaryAuthorization",
            "healthWindow60Seconds", "sourceAndInactivePreserved", "sessionCleanup"], True),
        "reusedAdmission04": {"report": copy.deepcopy(m.AFFECTED_TV_PARENT_ADMISSION04), "runtimeEpoch": copy.deepcopy(old["runtimeEpoch"]),
            "seedRuntimeBinding": copy.deepcopy(old["seedRuntimeBinding"]), "currentSource": copy.deepcopy(old_source),
            "contracts": ["native_authentication_and_query_carriers", "storage_and_library_access", "backup_create_download", "restore_ready_cancel_retained_stage"]}}
    return {"report": report, "value": value, "epoch": epoch, "old": old, "product": product}


def check_successor_admission(value):
    return m.admitted(value["report"], value["value"], value["epoch"], reused_admission04=value["old"], product_input=value["product"])


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
        "runtimeEpoch": copy.deepcopy(m.RETAINED_MOVIE_EPOCH), "sourceAfter": copy.deepcopy(m.RETAINED_SNAPSHOT), "scenario": "movie", "runId": "movie-04",
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
        "inputEvidence": {"after": copy.deepcopy(m.MOVIE05_SNAPSHOT), "epoch": copy.deepcopy(m.RETAINED_MOVIE_EPOCH)}, "browserOutcome": "failed", "browserExitCode": 1, "gatewayExitCode": 0,
        "checks": {"authentication": {"allSessionsRevoked": 13}, "ownedData": {"ownedTables": 35, "oldRowsDeleted": 0, "userData": copy.deepcopy(before["tables"]["user_item_data"][0])}}}
    return before, binding, {"closeout": closeout, "snapshot": copy.deepcopy(before)}


def reviewed_fixture():
    before, binding, old = movie05_fixture()
    binding.update(runtimeEpoch=copy.deepcopy(m.EPOCH), serverId="d" * 32)
    before["tables"]["users"].append({"id": "b" * 32, "name": "synthetic-control", "policy": {"EnableMediaPlayback": False}})
    before["tables"]["activity_entries"].append({"id": "foreign-activity", "data": {"enabled": True}})
    before["tables"]["play_sessions"].append({**copy.deepcopy(before["tables"]["play_sessions"][0]),
        "id": "play_foreign", "user_id": "b" * 32, "auth_session_id": "0"})
    pin = lambda name, digest: {"path": str(m.R / name), "sha256": digest * 64}
    review_pin = pin("synthetic-reviewed-movie-baseline.json", "1")
    value = fixture()
    value.update(version=4, retainedBaseline=review_pin)
    actor, movie = binding["actors"]["movie"], binding["catalog"]["movie"]
    review = {"kind": "audited-candidate-reviewed-movie-baseline", "version": 1, "status": "reviewed_closed_state", "scenario": "movie",
        "closeout": pin("synthetic-movie06-owned-state-closeout.json", "2"), "snapshot": pin("synthetic-movie06-source-after.json", "3"),
        "sourceEpoch": copy.deepcopy(m.RETAINED_MOVIE_EPOCH), "runtimeEpoch": copy.deepcopy(value["runtimeEpoch"]),
        "seedBinding": copy.deepcopy(value["seedBinding"]), "manifest": pin("synthetic-movie06-manifest.json", "4"),
        "actor": {key: actor[key] for key in ("id", "username")},
        "item": {"id": movie["id"], "mediaSourceId": "mediasource_" + movie["id"], "runtimeTicks": movie["runtimeTicks"]},
        "preparedExpirations": [{"playSessionId": m.MOVIE05_PREPARED, "authSessionId": m.MOVIE05_AUTH}], "boundary": None}
    review["movieProvenance"] = {key: copy.deepcopy(review[key]) for key in ("closeout", "snapshot", "sourceEpoch", "manifest")}
    source = {"archiveSha256": "5" * 64, "sourceManifest": pin("synthetic-source-manifest.json", "6"),
        "binary": pin("synthetic-goby", "7"), "fullReport": pin("synthetic-full-report.json", "8"), "schema": 28}
    epoch = {"kind": "audited-candidate-runtime-epoch", "version": 2, "currentSource": source}
    manifest = {"kind": "audited-candidate-client-input", "version": 1, "scenario": "movie", "runId": "movie-06", "serverId": binding["serverId"],
        "actor": copy.deepcopy(review["actor"]), "catalog": {"movie": copy.deepcopy(movie)},
        "source": {"manifestSha256": source["sourceManifest"]["sha256"], "binarySha256": source["binary"]["sha256"], "schema": source["schema"]}}
    closeout = old["closeout"]
    closeout.update(kind="audited-movie06-owned-state-closeout", inputEvidence={"after": copy.deepcopy(review["snapshot"]),
        "epoch": copy.deepcopy(review["sourceEpoch"]), "manifest": copy.deepcopy(review["manifest"])})
    closeout["checks"]["ownedData"].update(clientPlaybackReferences=0, encodingJobs=0)
    closeout["checks"]["savedRuntimeAndWorkers"] = {"candidateContinuous": True, "postgresContinuous": True, "leaseExact": True,
        "workers": {role: {"pidAbsentAsRecorded": True, "recursiveCgroupEmptyAsRecorded": True} for role in ("browser", "gateway")}}
    retained = {"review": review, "closeout": closeout, "snapshot": copy.deepcopy(before), "sourceEpoch": epoch, "manifest": manifest,
        "inputBinding": {key: copy.deepcopy(value[key]) for key in ("runtimeEpoch", "seedBinding")}, "boundary": None}
    retained["movieProvenance"] = {key: copy.deepcopy(retained[key]) for key in ("closeout", "snapshot", "sourceEpoch", "manifest")}
    return value, before, binding, retained


def reviewed_subtitles_fixture():
    value, before, binding, retained = reviewed_fixture()
    pin = lambda name, digest: {"path": str(m.R / name), "sha256": digest * 64}
    binding["actors"]["subtitles"] = {"id": "e" * 32, "username": "synthetic-subtitles"}
    before["capturedAt"] = "2026-09-13T12:30:00Z"
    before["tables"]["users"].append({"id": "e" * 32, "name": "synthetic-subtitles", "is_administrator": False, "is_disabled": False, "policy": {}})
    before["tables"]["sessions"].append({"id": "subtitle-auth", "user_id": "e" * 32, "revoked_at": "2026-09-13T12:29:00Z"})
    before["tables"]["play_sessions"].append({"id": "play_subtitles", "user_id": "e" * 32, "auth_session_id": "subtitle-auth"})
    before["tables"]["user_item_data"].append({"user_id": "e" * 32, "item_id": "subtitle-item", "play_count": 1})
    before["tables"]["client_playback_references"] = [{"user_id": "b" * 32, "auth_session_id": str(index),
        "play_session_id": "play_foreign", "device_id": "synthetic-audio", "client_nonce": "nonce-" + str(index)} for index in (0, 1)]
    review = retained["review"]
    review.update(closeout=pin("synthetic-subtitles-closeout.json", "9"), snapshot=pin("synthetic-subtitles-after.json", "a"),
        sourceEpoch=copy.deepcopy(m.EPOCH), manifest=pin("synthetic-subtitles-manifest.json", "b"), boundary=pin("synthetic-subtitles-boundary.json", "c"))
    process = {"pid": 123, "startTicks": "1234", "bootId": "synthetic-boot", "uid": 1001}
    candidate = {**process, "listener": {"host": "127.0.0.1", "port": 19181, "socketInode": "4"}}
    epoch = copy.deepcopy(retained["sourceEpoch"])
    epoch.update(version=3, candidateProcess=process, postgresProcess={"pid": 456, "startTicks": "5678"},
        lease={"held": True, "owner": "synthetic-owner"}, candidate={"database": "synthetic-current"})
    epoch["currentSource"]["binary"] = pin("synthetic-current-goby", "d")
    manifest = copy.deepcopy(retained["manifest"])
    manifest.update(scenario="subtitles", runId="subtitles-01", actor=copy.deepcopy(binding["actors"]["subtitles"]), processes={"candidate": candidate})
    manifest["source"]["binarySha256"] = epoch["currentSource"]["binary"]["sha256"]
    evidence = {"kind": "audited-candidate-client-closeout-input", "version": 3, "manifest": copy.deepcopy(review["manifest"]),
        "runtimeEpoch": copy.deepcopy(review["sourceEpoch"]), "seedBinding": copy.deepcopy(review["seedBinding"]), "admission": copy.deepcopy(value["admission"]),
        "sourceBefore": pin("synthetic-subtitles-before.json", "e"), "sourceAfter": copy.deepcopy(review["snapshot"]), "boundary": copy.deepcopy(review["boundary"]),
        "sources": copy.deepcopy(value["sources"]), "output": str(m.R / "synthetic-subtitles-closeout"), "retainedBaseline": None}
    for key in ("observation", "summary", "gatewayAttestation", "gatewayIndex", "serverLog"):
        evidence[key] = pin("synthetic-subtitles-" + key + ".json", "f")
    counts = {"afterSessions": "sessions", "afterPlays": "play_sessions", "afterUserData": "user_item_data",
        "afterClientReferences": "client_playback_references", "afterEncodingJobs": "encoding_jobs"}
    closeout = {"kind": "audited-candidate-subtitles-owned-state-closeout", "status": "owned_state_closed_client_acceptance_pending", "clientAcceptance": False,
        "inputEvidence": evidence, "sourcePinsUnchanged": True, "originalUIRejection": {"originalObservationUnchanged": True},
        "sourceState": {"allSessionsRevoked": True, **{key: len(before["tables"][table]) for key, table in counts.items()}}}
    boundary = {"kind": "audited-candidate-client-boundary", "version": 1, "runId": "subtitles-01", "runtimeEpoch": copy.deepcopy(review["sourceEpoch"]),
        "sourceBefore": copy.deepcopy(evidence["sourceBefore"]), "sourceAfter": copy.deepcopy(review["snapshot"]),
        "candidateBefore": copy.deepcopy(candidate), "candidateAfter": copy.deepcopy(candidate), "postgresBefore": copy.deepcopy(epoch["postgresProcess"]),
        "postgresAfter": copy.deepcopy(epoch["postgresProcess"]), "leaseBefore": copy.deepcopy(epoch["lease"]), "leaseAfter": copy.deepcopy(epoch["lease"]),
        "database": epoch["candidate"]["database"], "beforeMonotonicNs": "100", "afterMonotonicNs": "200",
        "clientWorker": {"exitCode": 0, "mainPID": 0, "remainingBrowserPids": [], "workerPidAbsent": True},
        "gatewayWorker": {"exitCode": 0, "mainPID": 0, "workerPidAbsent": True, "index": copy.deepcopy(evidence["gatewayIndex"])}}
    retained.update(closeout=closeout, snapshot=copy.deepcopy(before), sourceEpoch=epoch, manifest=manifest, boundary=boundary)
    return value, before, binding, retained


def reviewed_tv_fixture():
    value, before, binding, movie_retained = reviewed_subtitles_fixture()
    pin = lambda name, digest: {"path": str(m.R / name), "sha256": digest * 64}
    actor = {"id": "f" * 32, "username": "synthetic-tv"}
    binding["actors"]["tv-browse"] = actor
    catalog = {"tvLibrary": {"id": "5" * 32, "name": "M3e Client Television"},
        "series": {"id": "6" * 32, "type": "Series", "name": "M3e Client Series"},
        "seasons": [{"id": str(index + 6) * 32, "type": "Season", "indexNumber": index} for index in (1, 2)],
        "episodes": [{"id": identity * 32, "type": "Episode", "name": "Episode " + str(season) + "-" + str(index),
            "indexNumber": index, "parentIndexNumber": season, "runtimeTicks": 6000000000} for identity, season, index in (("9", 1, 1), ("a", 1, 2), ("b", 2, 1))]}
    binding["catalog"].update(copy.deepcopy(catalog))
    item = catalog["episodes"][2]
    prior = copy.deepcopy(movie_retained["movieProvenance"]["snapshot"])
    play = {"id": "play_synthetic_tv", "auth_session_id": "synthetic-tv-auth", "user_id": actor["id"], "item_id": item["id"],
        "media_source_id": "mediasource_" + item["id"], "duration_ticks": item["runtimeTicks"], "device_id": "synthetic-tv-device",
        "application_client_id": None, "client_correlated": False, "state": "Prepared", "counted": False, "position_ticks": 0,
        "started_at": None, "stopped_at": None, "created_at": "2026-09-13T12:19:00Z", "updated_at": "2026-09-13T12:19:00Z",
        "expires_at": "2026-09-13T12:49:00Z", "player_state": {}}
    userdata = {"user_id": actor["id"], "item_id": item["id"], "play_count": 0, "playback_position_ticks": 0,
        "is_favorite": False, "played": False, "last_played_at": None, "updated_at": "2026-09-13T12:19:00Z"}
    for snapshot in (prior, before):
        snapshot["tables"]["users"].append({"id": actor["id"], "name": actor["username"], "is_administrator": False, "is_disabled": False, "policy": {}})
        snapshot["tables"]["sessions"].append({"id": play["auth_session_id"], "user_id": actor["id"], "kind": "emby", "device_id": play["device_id"],
            "revoked_at": "2026-09-13T12:19:59Z"})
        snapshot["tables"]["play_sessions"].append(copy.deepcopy(play))
        snapshot["tables"]["user_item_data"].append(copy.deepcopy(userdata))
    admission_auth = {"credentialId": "0f" * 16, "tokenSha256": "9" * 64, "sameTokenRejected": True}
    before["tables"]["sessions"].append({"id": admission_auth["credentialId"], "user_id": actor["id"], "kind": "emby",
        "device_id": "synthetic-admission-device", "token_hash": "\\x" + admission_auth["tokenSha256"],
        "revoked_at": "2026-09-13T12:25:00Z"})
    provenance_pins = {"closeout": pin("synthetic-tv-owned-closeout.json", "1"), "snapshot": pin("synthetic-tv-after.json", "2"),
        "sourceEpoch": copy.deepcopy(m.RETAINED_MOVIE_EPOCH), "manifest": pin("synthetic-tv-manifest.json", "3")}
    epoch = copy.deepcopy(movie_retained["movieProvenance"]["sourceEpoch"])
    manifest = copy.deepcopy(movie_retained["movieProvenance"]["manifest"])
    manifest.update(scenario="tv-browse", runId="tv-browse-01", actor=copy.deepcopy(actor), catalog=copy.deepcopy(catalog))
    closeout = {"kind": "audited-tv-browse01-owned-state-closeout", "status": "owned_state_closed_client_acceptance_pending", "clientAcceptance": False,
        "browserOutcome": "failed", "browserExitCode": 1, "gatewayExitCode": 0, "physicalMediaRequests": 0, "playingReports": 0,
        "inputEvidence": {"after": copy.deepcopy(provenance_pins["snapshot"]), "epoch": copy.deepcopy(provenance_pins["sourceEpoch"]),
            "manifest": copy.deepcopy(provenance_pins["manifest"])}, "checks": {
            "authentication": {"allSessionsRevoked": len(prior["tables"]["sessions"])},
            "ownedData": {"ownedTables": 35, "oldRowsDeleted": 0, "clientPlaybackReferences": len(prior["tables"]["client_playback_references"]),
                "encodingJobs": 0, "newCountedPlays": 0, "newUserData": copy.deepcopy(userdata), "unstartedPreparations": [{"id": play["id"],
                    "authSessionId": play["auth_session_id"], "itemId": item["id"], "state": "Prepared", "counted": False, "positionTicks": 0, "prepareOrdinals": [277]}]},
            "savedRuntimeAndWorkers": copy.deepcopy(movie_retained["movieProvenance"]["closeout"]["checks"]["savedRuntimeAndWorkers"])}}
    review = {key: copy.deepcopy(row) for key, row in movie_retained["review"].items() if key != "movieProvenance"}
    review.update(kind="audited-candidate-reviewed-tv-baseline", scenario="tv-browse", tvProvenance=provenance_pins,
        postBrowseAdmission=copy.deepcopy(m.ADMISSION), actor=copy.deepcopy(actor),
        item={"id": item["id"], "mediaSourceId": play["media_source_id"], "runtimeTicks": item["runtimeTicks"]},
        preparedExpirations=[{"playSessionId": play["id"], "authSessionId": play["auth_session_id"]}])
    retained = {key: copy.deepcopy(row) for key, row in movie_retained.items() if key not in ("review", "movieProvenance")}
    retained.update(review=review, snapshot=copy.deepcopy(before), tvProvenance={"closeout": closeout, "snapshot": prior, "sourceEpoch": epoch, "manifest": manifest},
        postBrowseAdmission={"kind": "audited-candidate-live-admission", "version": 3, "status": "admitted_for_core_client",
            "candidateAdmissionComplete": True, "failure": None, "cleanupFailures": [], "runtimeEpoch": copy.deepcopy(review["runtimeEpoch"]),
            "seedRuntimeBinding": copy.deepcopy(review["seedBinding"]), "controllerSessions": {"P": admission_auth}})
    retained["manifest"]["catalog"].update(copy.deepcopy(catalog))
    retained["closeout"]["sourceState"].update(afterSessions=len(before["tables"]["sessions"]), afterPlays=len(before["tables"]["play_sessions"]),
        afterUserData=len(before["tables"]["user_item_data"]))
    value.update(version=5, scenario="tv-browse", retainedBaseline=pin("synthetic-reviewed-tv-baseline.json", "4"),
        currentRuntime=pin("synthetic-current-runtime.json", "5"))
    return value, before, binding, retained


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


def diagnostic_job_fixture(exit_code="1"):
    value, source, binding, retained = reviewed_tv_fixture()
    job = make_job(value)
    job.binding, job.retained = binding, retained
    binding["actors"]["tv-browse"]["credentials"] = {"path": str(m.R / "synthetic-tv-credentials.json"), "sha256": "a" * 64}
    process, unused, before, after, raw_before, raw_after = log_fixture()
    candidate = {**process, "listener": {"host": "127.0.0.1", "port": 19181, "socketInode": "4"}}
    job.epoch = {"candidateProcess": process, "candidate": {"publicUrl": "http://127.0.0.1:19180", "directUrl": "http://127.0.0.1:19181", "database": "synthetic"},
        "currentSource": {"sourceManifest": {"sha256": "c" * 64}, "binary": {"sha256": "d" * 64}}}
    job.gateway = types.SimpleNamespace(PROCESS_FIELDS=set(process))
    job.hosting = {"process": process, "listener": candidate["listener"], "executableSha256": "e" * 64}
    job.io = types.SimpleNamespace(pin=lambda: candidate)
    # The test models the admitted worker boundary without performing live work.
    job.preflight, job.open, job.verify_hosting, job.wait_client = Mock(), Mock(), Mock(), Mock()
    events, saved = [], {}
    def save(name, content):
        events.append(name)
        saved[name] = copy.deepcopy(content)
        return {"path": str(job.private / name), "sha256": "f" * 64}
    def sample(label):
        events.append("source_" + label)
        return source, {"path": str(job.private / ("source-" + label + ".json")), "sha256": "f" * 64}, candidate, {"pid": 456}, {"owned": True}
    def capture(label):
        events.append("log_" + label)
        return (before, raw_before) if label == "before" else (after, raw_after)
    job.save, job.source_sample, job.capture_server_log = save, sample, capture
    job.start_worker = lambda role, argv: events.append("start_" + role)
    job.close_worker = lambda role: events.append("close_" + role)
    job.wait_gateway = lambda: ({"process": process, "listener": {"host": "127.0.0.1", "port": 19180, "socketInode": "5"}},
        {"path": str(job.private / "gateway-attestation.json"), "sha256": "f" * 64})
    closed = {"unit": {"MainPID": "0"}, "workerPidAbsent": True, "recursiveCgroupPids": []}
    job.workers = {"browser": {"closure": copy.deepcopy(closed), "terminal": {"LoadState": "loaded", "Result": "success" if exit_code == "0" else "exit-code", "ExecMainStatus": exit_code}},
        "gateway": {"closure": copy.deepcopy(closed), "terminal": {"LoadState": "loaded", "Result": "success", "ExecMainStatus": "0"}}}
    summary = {"status": "owned_state_closed_diagnostic_result", "clientAcceptance": False, "uiAccepted": exit_code == "0",
        "originalBrowserOutcome": "completed" if exit_code == "0" else "failed", "originalBrowserFailure": None if exit_code == "0" else "synthetic_ui_rejection",
        "diagnostic": {"status": "inconclusive" if exit_code == "0" else "response_identified"}}
    job.pin_file = lambda path: {"path": str(path), "sha256": "f" * 64}
    def read(pin):
        if pin["path"] == str(job.output / "gateway/index.json"):
            return {"complete": True, "webMediaAndWebSocketBodiesRetained": False}
        if pin["path"] == str(job.output / "closeout/summary.json"):
            return summary
        raise m.RunError("synthetic_unexpected_descriptor")
    job.s = types.SimpleNamespace(descriptor=read)
    job.p = types.SimpleNamespace(command=Mock())
    return job, events, saved, summary


def component_fixture(*, wrong_seed=False):
    """Finite v5 receipt fixture; existing tests own the unchanged native ancestor."""
    value, unused_before, unused_binding, unused_retained = reviewed_tv_fixture()
    raw = {}
    def pin(name, content):
        body = content if isinstance(content, bytes) else (m.canonical(content) + "\n").encode()
        selected = {"path": str(m.R / "synthetic-v5-components" / name), "sha256": hashlib.sha256(body).hexdigest()}
        raw[selected["path"]] = body
        return selected
    sources = {role: pin("sources/" + name, ("# selected " + role + "\n").encode()) for role, name in m.SOURCE_FILES.items()}
    frozen = {role: row["sha256"] for role, row in sources.items()}
    frozen["closer"] = hashlib.sha256(b"old closer").hexdigest()
    controller = pin("controller.py", b"# selected controller\n")
    value.update(sources=sources, runtimeHelper=pin("runtime.py", b'raise AssertionError("new_helper_imported")\n'))
    cases = {"kind": "audited-candidate-reviewed-tv-baseline-replay-input", "version": 1,
             "review": value["retainedBaseline"], "seed": copy.deepcopy(value["seedBinding"]),
             "inputBinding": {key: value[key] for key in ("runtimeEpoch", "seedBinding")}}
    if wrong_seed:
        cases["seed"]["sha256"] = "0" * 64
    evidence = {"cases": pin("cases.json", cases)}
    replay = {"review": value["retainedBaseline"], "tablesCompared": 35, "sequencesCompared": 5}
    javascript = {"kind": "audited-candidate-client-closeout-pure-guards", "source": sources["closer"],
                  "sourceUnchanged": True, "testCount": 1, "passed": 1, "failed": 0, "clientAcceptanceClaim": False,
                  "tests": [{"outcome": "passed"}], "reviewedTVBaselineReplay": replay}
    evidence["javascriptResult"] = pin("javascript.json", javascript)
    evidence["runtimeVerification"] = pin("runtime-verification.json", {"kind": "audited-candidate-current-runtime-verification",
        "status": "passed", "source": value["runtimeHelper"], "passed": 1, "failed": 0, "environmentDecoded": False,
        "sqlCalls": 0, "httpCalls": 0, "serviceActions": 0})
    outputs = {"python": ((m.canonical({"kind": "audited-candidate-reviewed-tv-baseline-python-replay",
        "reviewSha256": value["retainedBaseline"]["sha256"], "rejectedTableMutations": 35, "rejectedSequenceMutations": 5}) + "\n").encode(), b"Ran 1 tests in 0.001s\n\nOK\n"),
               "javascript": ((m.canonical({**evidence["javascriptResult"], **{key: javascript[key] for key in
                    ("testCount", "passed", "failed", "sourceUnchanged", "clientAcceptanceClaim")}}) + "\n").encode(), b""),
               "log-binding": (b"# tests 1\n# pass 1\n# fail 0\n# skipped 0\n# cancelled 0\n", b"")}
    executions, test_sources = [], []
    for name in ("python", "javascript", "log-binding"):
        test_source = pin("test-" + name + ".py", ("# " + name + "\n").encode())
        test_sources.append(test_source)
        argv = [test_source["path"]]
        if name != "log-binding":
            argv += ["--reviewed-tv-baseline", evidence["cases"]["path"], "--reviewed-tv-baseline-sha256", evidence["cases"]["sha256"]]
        executions.append({"name": name, "exitCode": 0, "timedOut": False, "childReaped": True, "argv": argv,
                           "testSource": test_source, "stdout": pin(name + ".stdout", outputs[name][0]),
                           "stderr": pin(name + ".stderr", outputs[name][1])})
    evidence["dispatch"] = pin("dispatch.json", {"kind": "tv-client-component-verification-dispatch", "version": 1,
        "scope": m.COMPONENT_SCOPE, "browserRuns": 0, "businessHttpCalls": 0, "sqlCalls": 0, "serviceActions": 0,
        "sources": [controller, value["runtimeHelper"], *sources.values(), *test_sources], "executions": executions})
    receipt = {"kind": "audited-candidate-client-component-admission", "version": 1, "scope": m.COMPONENT_SCOPE,
               "controller": controller, "sources": sources, "runtimeHelper": value["runtimeHelper"],
               "retainedBaseline": value["retainedBaseline"], "currentRuntime": value["currentRuntime"],
               "ancestor": copy.deepcopy(m.AV_VERIFICATION), "evidence": evidence}
    review = {"kind": "audited-candidate-tv-component-review", "version": 1, "status": "passed", "scope": m.COMPONENT_SCOPE,
              "boundInputs": {key: copy.deepcopy(receipt[key]) for key in ("controller", "sources", "runtimeHelper", "retainedBaseline", "currentRuntime", "ancestor", "evidence")},
              "testsReplayed": False, "businessHttpCalls": 0, "sqlCalls": 0, "serviceActions": 0,
              "counts": dict.fromkeys(("python", "javascript", "log-binding", "runtime"), 1)}
    receipt["independentReview"] = pin("review.json", review)
    value["avVerification"] = pin("component.json", receipt)
    return {"value": value, "receipt": receipt, "controller": controller, "raw": raw, "pin": pin,
            "frozen": frozen, "read": lambda selected: raw[selected["path"]]}


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


def final_fixture(scenario="movie"):
    """Synthetic v6 authority; it never supplies a production artifact or input."""
    value, before, binding, legacy = reviewed_fixture()
    pin = lambda name: {"path": str(m.R / ("synthetic-final/" + name)), "sha256": hashlib.sha256(name.encode()).hexdigest()}
    sequences = {name: {"lastValue": "1", "isCalled": True} for name in m.FINAL_SEQUENCES}
    movie_pins = {**copy.deepcopy(legacy["review"]["movieProvenance"]), "boundary": None}
    movie_history = {**copy.deepcopy(legacy["movieProvenance"]), "boundary": None}
    movie_history["snapshot"]["sequences"] = copy.deepcopy(sequences)
    before["sequences"] = copy.deepcopy(sequences)
    binding.update(version=4, runtimeEpoch=pin("runtime-epoch.json"))
    source = {**copy.deepcopy(legacy["sourceEpoch"]["currentSource"]),
              **{key: pin(key + ".json") for key in ("artifactReceipt", "buildManifest", "sourceBridge", "sourceManifest", "fullReport", "binary")}}
    epoch = {"kind": "audited-candidate-runtime-epoch", "version": 4, "operationKind": "programs_successor", "currentSource": source}
    actor_pins, actor_history = movie_pins, movie_history
    if scenario != "movie":
        actor = {"id": ("e" if scenario == "episode" else "f") * 32, "username": "synthetic-" + scenario}
        binding["actors"][scenario] = actor
        if scenario == "episode":
            binding["catalog"]["episodes"] = [{"id": "9" * 32, "type": "Episode", "parentIndexNumber": 2, "indexNumber": 1, "runtimeTicks": 6000000000}]
        item = m.final_item(binding, scenario)
        old_play = next(row for row in before["tables"]["play_sessions"] if row["state"] == "Prepared" and row["user_id"] == binding["actors"]["movie"]["id"])
        play = {**copy.deepcopy(old_play), "id": "play_" + scenario + "_retained", "auth_session_id": scenario + "_retained_auth", "user_id": actor["id"],
                "item_id": item["id"], "media_source_id": item["mediaSourceId"], "duration_ticks": item["runtimeTicks"], "device_id": scenario + "-device"}
        before["tables"]["users"].append({"id": actor["id"], "name": actor["username"], "is_administrator": False, "is_disabled": False, "policy": {}})
        before["tables"]["sessions"].append({"id": play["auth_session_id"], "user_id": actor["id"], "kind": "emby", "device_id": play["device_id"], "revoked_at": "2026-09-13T12:20:00Z"})
        before["tables"]["play_sessions"].append(play)
        before["tables"]["user_item_data"].append({"user_id": actor["id"], "item_id": item["id"], "play_count": 1, "playback_position_ticks": 123000000,
                                                  "is_favorite": False, "played": False, "last_played_at": "2026-09-13T12:10:00Z"})
        actor_pins = {key: pin(scenario + "-" + key + ".json") for key in m.FINAL_PROVENANCE_KEYS}
        process = {"pid": 100, "startTicks": "1000", "bootId": "synthetic-boot"}
        historic_epoch = {**copy.deepcopy(legacy["sourceEpoch"]), "version": 3, "candidateProcess": process, "postgresProcess": {"pid": 200}, "lease": {"owned": True}}
        candidate = {**process, "listener": {"port": 19181, "socketInode": "10"}}
        manifest = {**copy.deepcopy(legacy["manifest"]), "scenario": scenario, "runId": scenario + "-01", "actor": actor,
                    "catalog": copy.deepcopy(binding["catalog"]), "processes": {"candidate": candidate}}
        evidence = {"sourceAfter": actor_pins["snapshot"], "runtimeEpoch": actor_pins["sourceEpoch"], "manifest": actor_pins["manifest"],
                    "sourceBefore": pin(scenario + "-before.json"), "boundary": actor_pins["boundary"], "gatewayIndex": pin(scenario + "-gateway.json")}
        boundary = {"kind": "audited-candidate-client-boundary", "version": 1, "runId": manifest["runId"], "runtimeEpoch": actor_pins["sourceEpoch"],
                    "sourceBefore": evidence["sourceBefore"], "sourceAfter": actor_pins["snapshot"], "candidateBefore": candidate, "candidateAfter": copy.deepcopy(candidate),
                    "postgresBefore": historic_epoch["postgresProcess"], "postgresAfter": copy.deepcopy(historic_epoch["postgresProcess"]),
                    "leaseBefore": historic_epoch["lease"], "leaseAfter": copy.deepcopy(historic_epoch["lease"]),
                    "clientWorker": {"exitCode": 0, "mainPID": 0, "workerPidAbsent": True, "remainingBrowserPids": []},
                    "gatewayWorker": {"exitCode": 0, "mainPID": 0, "workerPidAbsent": True, "index": evidence["gatewayIndex"]}}
        actor_history = {"snapshot": copy.deepcopy(before), "sourceEpoch": historic_epoch, "manifest": manifest, "boundary": boundary,
                         "closeout": {"kind": "audited-candidate-" + scenario + "-owned-state-closeout", "status": "owned_state_closed_client_acceptance_pending",
                                      "clientAcceptance": False, "sourcePinsUnchanged": True, "inputEvidence": evidence}}
    actor = binding["actors"][scenario]
    before["capturedAt"] = "2026-09-13T13:00:00Z"
    before["tables"]["client_playback_references"].append({"user_id": "b" * 32, "auth_session_id": "foreign-auth", "play_session_id": "play_foreign", "client_nonce": "foreign-reference"})
    for role in ("P", "Q"):
        before["tables"]["sessions"].append({"id": "admission-" + role, "user_id": "b" * 32, "kind": "emby", "revoked_at": "2026-09-13T12:59:59Z"})
    value.update(version=6, runId=scenario + "-final-01", scenario=scenario, runtimeEpoch=binding["runtimeEpoch"], seedBinding=pin("seed-binding.json"),
                 admission=pin("admission.json"), admissionCloseout=pin("admission-closeout.json"), runtimeHelper=pin("runtime.py"),
                 avVerification=pin("components.json"), retainedBaseline=pin("baseline.json"))
    value["output"] = str(m.R / ("candidate-core-client-" + value["runId"]))
    for role in ("closer", "subtitles"):
        value["sources"][role]["sha256"] = pin(role)["sha256"]
    review = {"kind": "audited-candidate-final-client-baseline", "version": 1, "status": "reviewed_closed_state", "scenario": scenario,
              "actor": {key: actor[key] for key in ("id", "username")}, "item": m.final_item(binding, scenario), "runtimeEpoch": value["runtimeEpoch"],
              "seedBinding": value["seedBinding"], "currentSnapshot": pin("after-admission.json"),
              "currentStateEvidence": {"kind": "programs-admission", "receipt": value["admissionCloseout"]},
              "actorProvenance": actor_pins, "movieProvenance": movie_pins,
              "preparedExpirations": [{"playSessionId": row["id"], "authSessionId": row["auth_session_id"]} for row in before["tables"]["play_sessions"]
                                      if row["user_id"] == actor["id"] and row["state"] == "Prepared"]}
    state = {"kind": "audited-candidate-programs-admission-closeout", "version": 1, "status": "admitted_for_core_client", "admissionKind": "affected_programs",
             "runtimeEpoch": value["runtimeEpoch"], "seedRuntimeBinding": value["seedBinding"], "sourceAfter": review["currentSnapshot"], "currentSource": source}
    retained = {"review": review, "snapshot": copy.deepcopy(before), "currentStateEvidence": state, "actorProvenance": actor_history,
                "movieProvenance": movie_history, "inputBinding": {key: value[key] for key in ("runtimeEpoch", "seedBinding", "admission", "admissionCloseout")}, "epoch": epoch}
    return value, before, binding, retained, epoch


class Guards(unittest.TestCase):
    def test_final_v6_is_separate_from_legacy_and_does_not_accept_recovery_runtime(self):
        frozen = copy.deepcopy(m.FROZEN)
        for scenario in ("movie", "episode", "subtitles"):
            value, _, _, _, _ = final_fixture(scenario)
            self.assertEqual(m.validate_input(value), value)
            for change in ({"scenario": "tv-browse"}, {"version": 5}, {"currentRuntime": copy.deepcopy(m.EPOCH)}, {"admission": copy.deepcopy(m.ADMISSION)}):
                with self.subTest(scenario=scenario, change=change), self.assertRaises(m.RunError):
                    m.validate_input({**copy.deepcopy(value), **change})
        self.assertEqual(m.FROZEN, frozen)

    def test_final_baselines_keep_each_consumed_actor_and_foreign_references(self):
        for scenario in ("movie", "episode", "subtitles"):
            _, before, seed, retained, epoch = final_fixture(scenario)
            original = copy.deepcopy(retained)
            self.assertEqual(len(m.validate_final_baseline(retained, seed, epoch)), 1)
            m.verify_actor_before(before, seed, scenario, retained, 6, epoch=epoch)
            self.assertEqual(retained, original)
            self.assertEqual(len(before["tables"]["client_playback_references"]), 1)

    def test_final_baseline_requires_independent_admission_after_state(self):
        for mutate in (
            lambda r: r["currentStateEvidence"].update(kind="audited-candidate-live-admission", version=5),
            lambda r: r["review"]["currentStateEvidence"].update(receipt=r["inputBinding"]["admission"]),
            lambda r: r["currentStateEvidence"].update(sourceAfter={"path": str(m.R / "transition-source-after.json"), "sha256": "0" * 64}),
            lambda r: r["currentStateEvidence"].update(status="pending_independent_review"),
        ):
            _, _, seed, retained, epoch = final_fixture()
            mutate(retained)
            with self.assertRaisesRegex(m.RunError, "final_admission_state_binding"):
                m.validate_final_baseline(retained, seed, epoch)

    def test_final_following_client_binds_same_epoch_admission_and_current_snapshot(self):
        _, before, seed, retained, epoch = final_fixture("episode")
        review = retained["review"]
        review["currentStateEvidence"]["kind"] = "final-client"
        retained["currentStateEvidence"] = {"kind": "audited-candidate-client-closeout", "status": "core_scenario_closed", "contractVersion": 6, "scenario": "movie",
            "source": {"binarySha256": epoch["currentSource"]["binary"]["sha256"], "manifestSha256": epoch["currentSource"]["sourceManifest"]["sha256"], "schema": 28},
            "evidence": {**{key: retained["inputBinding"][key] for key in ("runtimeEpoch", "seedBinding", "admission")}, "sourceAfter": review["currentSnapshot"]}}
        m.verify_actor_before(before, seed, "episode", retained, 6, epoch=epoch)
        for key in ("runtimeEpoch", "seedBinding", "admission", "sourceAfter"):
            changed = copy.deepcopy(retained)
            changed["currentStateEvidence"]["evidence"][key] = {**changed["currentStateEvidence"]["evidence"][key], "sha256": "0" * 64}
            with self.subTest(key=key), self.assertRaisesRegex(m.RunError, "final_preceding_client_state_binding"):
                m.validate_final_baseline(changed, seed, epoch)

    def test_final_current_snapshot_checks_all_35_tables_and_five_sequences(self):
        _, before, seed, retained, epoch = final_fixture()
        for table in sorted(m.REVIEWED_BASELINE_TABLES):
            changed = copy.deepcopy(before)
            changed["tables"][table].append({"id": "unexpected-row"})
            with self.subTest(table=table), self.assertRaises(m.RunError):
                m.verify_actor_before(changed, seed, "movie", retained, 6, epoch=epoch)
        for name in m.FINAL_SEQUENCES:
            changed = copy.deepcopy(before); changed["sequences"][name]["lastValue"] = "2"
            with self.subTest(sequence=name), self.assertRaisesRegex(m.RunError, "final_fresh_state_changed"):
                m.verify_actor_before(changed, seed, "movie", retained, 6, epoch=epoch)

    def test_final_rejects_wrong_actor_provenance_prepared_scope_and_owned_residue(self):
        mutations = [lambda r: r["review"].update(actor={"id": "b" * 32, "username": "synthetic-control"}),
                     lambda r: r["review"].update(preparedExpirations=[]),
                     lambda r: r["actorProvenance"]["manifest"].update(scenario="tv-browse"),
                     lambda r: r["snapshot"]["tables"]["sessions"].append({"id": "duplicate", "user_id": r["review"]["actor"]["id"], "revoked_at": None})]
        for mutation in mutations:
            _, _, seed, retained, epoch = final_fixture("episode"); mutation(retained)
            with self.assertRaises(m.RunError):
                m.validate_final_baseline(retained, seed, epoch)
        _, _, seed, retained, epoch = final_fixture("subtitles")
        retained["snapshot"]["tables"]["encoding_jobs"].append({"id": "owned-job", "play_session_id": retained["review"]["preparedExpirations"][0]["playSessionId"]})
        with self.assertRaisesRegex(m.RunError, "final_current_actor_residue"):
            m.validate_final_baseline(retained, seed, epoch)

    def test_final_loader_reads_real_state_and_both_provenances_without_mutation(self):
        value, _, seed, retained, epoch = final_fixture("subtitles")
        records = {value["retainedBaseline"]["path"]: retained["review"], retained["review"]["currentSnapshot"]["path"]: retained["snapshot"],
                   retained["review"]["currentStateEvidence"]["receipt"]["path"]: retained["currentStateEvidence"]}
        for role in ("actorProvenance", "movieProvenance"):
            for key, pin in retained["review"][role].items():
                if pin is not None:
                    records[pin["path"]] = retained[role][key]
        original = copy.deepcopy(records)
        loaded = m.load_final_baseline(value["retainedBaseline"], retained["inputBinding"], seed, epoch, lambda pin: copy.deepcopy(records[pin["path"]]))
        self.assertEqual(loaded, retained)
        self.assertEqual(records, original)

    def test_final_missing_components_fail_before_runtime_actor_selection(self):
        value, _, _, _, _ = final_fixture()
        with self.assertRaisesRegex(m.RunError, "final_component_schema"):
            m.validate_v6_component_evidence({}, value, {"path": str(m.R / "controller.py"), "sha256": "1" * 64}, lambda pin: b"")
    def test_v5_component_selection_binds_actual_source_bytes_and_input_pins(self):
        f = component_fixture()
        with patch.object(m, "FROZEN", f["frozen"]):
            m.validate_v5_component_selection(f["receipt"], f["value"], f["controller"], f["read"])
            for selected in (f["controller"], f["value"]["runtimeHelper"], f["value"]["sources"]["closer"]):
                original = f["raw"][selected["path"]]
                f["raw"][selected["path"]] = original + b"changed"
                with self.subTest(source=selected["path"]), self.assertRaisesRegex(m.RunError, "component_read_digest"):
                    m.validate_v5_component_selection(f["receipt"], f["value"], f["controller"], f["read"])
                f["raw"][selected["path"]] = original
            for name in ("controller", "runtimeHelper", "retainedBaseline", "currentRuntime"):
                changed = copy.deepcopy(f["receipt"])
                changed[name]["sha256"] = "0" * 64
                with self.subTest(binding=name), self.assertRaisesRegex(m.RunError, "component_selected_inputs_changed"):
                    m.validate_v5_component_selection(changed, f["value"], f["controller"], f["read"])

    def test_v5_component_evidence_rejects_changed_bytes_and_wrong_saved_seed(self):
        original_read = m.component_read
        def read_component(pin, read_pin):
            # Existing native-ancestor tests cover those fixed historical receipts.
            if pin in (m.AV_VERIFICATION, m.REUSED_AV_VERIFICATION):
                return b"{}"
            return original_read(pin, read_pin)
        for changed in (None, "dispatch", "javascriptResult", "independentReview", "seed"):
            f = component_fixture(wrong_seed=changed == "seed")
            if changed not in (None, "seed"):
                selected = f["receipt"][changed] if changed == "independentReview" else f["receipt"]["evidence"][changed]
                f["raw"][selected["path"]] += b"changed"
            with self.subTest(changed=changed), patch.object(m, "FROZEN", f["frozen"]), \
                    patch.object(m, "component_read", side_effect=read_component), patch.object(m, "validate_native_rejection_verification"):
                if changed is None:
                    m.validate_v5_component_evidence(f["receipt"], f["value"], f["controller"], f["read"])
                else:
                    reason = "component_tv_baseline_not_tested" if changed == "seed" else "component_read_digest"
                    with self.assertRaisesRegex(m.RunError, reason):
                        m.validate_v5_component_evidence(f["receipt"], f["value"], f["controller"], f["read"])

    def test_v5_component_refuses_old_receipt_helper_and_wrong_scenario_before_reads(self):
        for change in ("receipt", "helper", "scenario"):
            f = component_fixture()
            selected = copy.deepcopy(f["value"])
            if change == "receipt":
                selected["avVerification"] = copy.deepcopy(m.AV_VERIFICATION)
            elif change == "helper":
                selected["runtimeHelper"] = copy.deepcopy(m.RUNTIME)
            else:
                selected["scenario"] = "movie"
            read = Mock(side_effect=AssertionError("unexpected_component_read"))
            with self.subTest(change=change), patch.object(m, "FROZEN", f["frozen"]), self.assertRaises(m.RunError):
                m.validate_v5_component_selection(f["receipt"], selected, f["controller"], read)
            read.assert_not_called()
            if change == "scenario":
                with self.assertRaisesRegex(m.RunError, "version5_reviewed_tv_required"):
                    m.validate_input(selected)

    def test_v5_retained_actor_and_seed_cannot_be_substituted(self):
        value, unused, binding, retained = reviewed_tv_fixture()
        changed_binding = copy.deepcopy(binding)
        changed_binding["actors"]["tv-browse"]["id"] = "0" * 32
        with self.assertRaises(m.RunError):
            m.validate_reviewed_tv_baseline(retained, changed_binding)
        changed_input = copy.deepcopy(value)
        changed_input["seedBinding"]["sha256"] = "0" * 64
        with self.assertRaisesRegex(m.RunError, "current_admission_authority_changed"):
            m.validate_input(changed_input)

    def test_v5_bootstrap_rejection_never_imports_new_helper(self):
        f = component_fixture()
        receipt = copy.deepcopy(f["receipt"])
        receipt["controller"]["sha256"] = "0" * 64
        value = copy.deepcopy(f["value"])
        value["avVerification"] = f["pin"]("rejected-component.json", receipt)
        input_pin = f["pin"]("input.json", value)
        trusted_raw = b"# fixed protected-reader fixture\n"
        trusted_pin = f["pin"]("old-runtime.py", trusted_raw)
        imported = []
        def module(specification):
            imported.append(specification.origin)
            result = types.ModuleType(specification.name)
            result.read_bootstrap = f["read"]
            return result
        with patch.object(m, "FROZEN", f["frozen"]), patch.object(m, "RUNTIME", trusted_pin), \
                patch.object(Path, "read_bytes", return_value=trusted_raw), \
                patch.object(importlib.util, "module_from_spec", side_effect=module), \
                self.assertRaisesRegex(m.RunError, "component_selected_inputs_changed"):
            m.bootstrap_runtime(value["runtimeHelper"], f["controller"], input_pin)
        self.assertEqual(imported, [trusted_pin["path"]])

    def test_v5_epochio_receives_current_identity_without_rewriting_history(self):
        value, unused, unused_binding, unused_retained = reviewed_tv_fixture()
        job = make_job(value)
        job.epoch = {"candidateProcess": {"pid": 123}, "candidate": {"database": "historical"}}
        historical = copy.deepcopy(job.epoch)
        job.current_runtime = {"current": {"candidateProcess": {"pid": 456}}}
        current = copy.deepcopy(job.current_runtime)
        io = types.SimpleNamespace(created=False, open=Mock(side_effect=m.RunError("synthetic_open_stop")))
        job.r.EpochIO, job.modules = Mock(return_value=io), {}
        with self.assertRaisesRegex(m.RunError, "synthetic_open_stop"):
            job.open()
        self.assertEqual(job.r.EpochIO.call_args.kwargs, {"current_runtime": current, "current_runtime_pin": value["currentRuntime"]})
        self.assertEqual(job.current_identity(), current["current"])
        self.assertEqual(job.epoch, historical)
        self.assertFalse(job.created)

    def test_v5_log_and_boundary_bind_current_identity_and_preserve_epoch(self):
        job, unused_events, saved, unused_summary = diagnostic_job_fixture("0")
        historical = copy.deepcopy(job.epoch)
        current = {**copy.deepcopy(job.epoch["candidateProcess"]), "pid": 987, "startTicks": "9876"}
        job.current_runtime = {"current": {"candidateProcess": current}}
        original_sample, original_capture = job.source_sample, job.capture_server_log
        def sample(label):
            result = list(original_sample(label))
            result[2] = {**result[2], **current}
            return tuple(result)
        def capture(label):
            result, raw = original_capture(label)
            return {**result, "candidateBefore": copy.deepcopy(current), "candidateAfter": copy.deepcopy(current)}, raw
        job.source_sample, job.capture_server_log = sample, capture
        job.io.pin = lambda: sample("pin")[2]
        with patch.dict(m.os.environ, {"SSH_CONNECTION": "synthetic-ssh"}):
            job.run()
        for name in ("server-log.json", "boundary.json"):
            self.assertEqual(saved[name]["version"], 2)
            self.assertEqual(saved[name]["currentRuntime"], job.value["currentRuntime"])
            self.assertEqual(saved[name]["runtimeEpoch"], job.value["runtimeEpoch"])
        self.assertEqual(saved["boundary.json"]["candidateBefore"]["pid"], 987)
        self.assertEqual(saved["adapter-input.json"]["processes"]["candidate"]["pid"], 987)
        self.assertEqual(job.epoch, historical)

    def test_native_verification_separates_fresh_checks_from_reused_results(self):
        current, old, selected = native_verification_fixture()
        old_before = copy.deepcopy(old)
        current["sourcePins"]["test-native-rejections.mjs"] = "c" * 64
        self.assertNotIn("closerCounts", current)
        self.assertNotIn("movieCounts", current)
        with patch.object(m, "FROZEN", selected):
            self.assertIsNone(m.validate_native_rejection_verification(current, old))
            # The two new suites have actual positive counts, not guessed totals.
            current["nativeCounts"].update(testCount=9, passed=9)
            current["subtitleCounts"].update(tests=5, **{"pass": 5})
            self.assertIsNone(m.validate_native_rejection_verification(current, old))
        self.assertEqual(old, old_before)

    def test_native_verification_rejects_wrong_execution_scope_and_boolean_counts(self):
        changes = [("kind", "tv-parent-candidate-client-component-verification"), ("version", True),
            ("passed", 1), ("sourceUnchanged", 1), ("browserStarted", False), ("browserStarted", 1), ("businessHttp", 0),
            ("syntheticBrowserRuns", 0), ("syntheticBrowserRuns", True), ("syntheticBrowserRuns", 2),
            ("originalClientRuns", 1), ("originalClientRuns", False), ("verificationWorker", {"closed": False}),
            ("verificationWorker", {"closed": 1}), ("freshComponents", ["adapter"]),
            ("freshComponents", ["adapter", "subtitles", "movie"]), ("reusedChecks", ["closer", "movie"])]
        for key, value in changes:
            current, old, selected = native_verification_fixture()
            current[key] = value
            with self.subTest(key=key, value=value), patch.object(m, "FROZEN", selected), self.assertRaises(m.RunError):
                m.validate_native_rejection_verification(current, old)

    def test_native_verification_requires_each_fresh_suite_and_strict_counts(self):
        for group, key, value in [("adapterCounts", "fail", False), ("adapterCounts", "pass", 54),
                ("nativeCounts", "testCount", True), ("nativeCounts", "failed", False), ("nativeCounts", "sourceUnchanged", 1),
                ("nativeCounts", "passed", 5), ("subtitleCounts", "fail", False), ("subtitleCounts", "skipped", 1),
                ("subtitleCounts", "tests", 0)]:
            current, old, selected = native_verification_fixture()
            current[group][key] = value
            with self.subTest(group=group, key=key), patch.object(m, "FROZEN", selected), self.assertRaises(m.RunError):
                m.validate_native_rejection_verification(current, old)
        for missing in ("adapterCounts", "nativeCounts", "subtitleCounts", "adapterSavedReplayChecks"):
            current, old, selected = native_verification_fixture()
            del current[missing]
            with self.subTest(missing=missing), patch.object(m, "FROZEN", selected), self.assertRaises(m.RunError):
                m.validate_native_rejection_verification(current, old)

    def test_native_verification_checks_the_fixed_old_receipt_independently(self):
        for failure in ("descriptor-path", "descriptor-hash", "kind", "version", "passed", "source", "browser", "http",
                        "adapter", "saved-tv", "closer", "version3", "saved-movie", "movie", "old-source"):
            current, old, selected = native_verification_fixture()
            if failure == "descriptor-path":
                current["reusedComponents"]["path"] = str(m.R / "different/verification.json")
            elif failure == "descriptor-hash":
                current["reusedComponents"]["sha256"] = "0" * 64
            elif failure in ("kind", "version", "passed", "source", "browser", "http"):
                key, value = {"kind": ("kind", "wrong"), "version": ("version", True), "passed": ("passed", 1),
                    "source": ("sourceUnchanged", 1), "browser": ("browserStarted", True), "http": ("businessHttp", 0)}[failure]
                old[key] = value
            elif failure == "saved-tv":
                old["adapterSavedReplayChecks"] = 6
            elif failure == "old-source":
                old["sourcePins"][m.SOURCE_FILES["adapter"]] = selected["adapter"]
            else:
                group, key = {"adapter": ("adapterCounts", "fail"), "closer": ("closerCounts", "failed"),
                    "version3": ("version3Counts", "fail"), "saved-movie": ("savedMovie05ReplayCounts", "failed"),
                    "movie": ("movieCounts", "fail")}[failure]
                old[group][key] = False
            with self.subTest(failure=failure), patch.object(m, "FROZEN", selected), self.assertRaises(m.RunError):
                m.validate_native_rejection_verification(current, old)

    def test_native_verification_rejects_stale_fresh_sources_and_changed_reused_sources(self):
        for component in m.SOURCE_FILES:
            current, old, selected = native_verification_fixture()
            current["sourcePins"][m.SOURCE_FILES[component]] = "0" * 64
            with self.subTest(component=component), patch.object(m, "FROZEN", selected), self.assertRaises(m.RunError):
                m.validate_native_rejection_verification(current, old)
        for component in m.SOURCE_FILES:
            current, old, selected = native_verification_fixture()
            selected[component] = m.REUSED_AV_SOURCES[component] if component in ("adapter", "subtitles") else "0" * 64
            current["sourcePins"][m.SOURCE_FILES[component]] = selected[component]
            with self.subTest(selection=component), patch.object(m, "FROZEN", selected), self.assertRaises(m.RunError):
                m.validate_native_rejection_verification(current, old)

    def test_admission_version_tracks_the_runtime_epoch_and_requires_current_success(self):
        value = successor_admission_fixture()
        check_successor_admission(value)
        for version in (1, 2):
            legacy_value = {"runtimeEpoch": value["old"]["runtimeEpoch"], "seedBinding": value["old"]["seedRuntimeBinding"]}
            m.admitted(value["old"], legacy_value, {"version": version, "currentSource": value["old"]["currentSource"]})
            with self.subTest(version=version), self.assertRaises(m.RunError):
                m.admitted(value["report"], legacy_value, {"version": version, "currentSource": value["old"]["currentSource"]})
        for mutate in (lambda row: row["report"].update(version=2), lambda row: row["report"].update(version=3.0),
                       lambda row: row["report"].update(status="admission_failed_resources_retained"),
                       lambda row: row["report"].update(candidateAdmissionComplete=1), lambda row: row["report"].pop("failure"),
                       lambda row: row["report"].update(currentSource=copy.deepcopy(row["old"]["currentSource"]))):
            changed = successor_admission_fixture()
            mutate(changed)
            with self.assertRaises(m.RunError):
                check_successor_admission(changed)

    def test_affected_tv_admission_requires_exact_fresh_checks_and_fixed_transition_closeout(self):
        for failure in ("kind", "missing", "extra", "false", "integer", "closeout"):
            value = successor_admission_fixture()
            if failure == "kind":
                value["report"]["admissionKind"] = "full_admission"
            elif failure == "missing":
                value["report"]["freshChecks"].pop("tvDetailParents")
            elif failure == "extra":
                value["report"]["freshChecks"]["unreviewed"] = True
            elif failure in ("false", "integer"):
                value["report"]["freshChecks"]["sessionCleanup"] = False if failure == "false" else 1
            else:
                value["report"]["transitionCloseout"]["sha256"] = "0" * 64
            with self.subTest(failure=failure), self.assertRaises(m.RunError):
                check_successor_admission(value)

    def test_affected_tv_admission_rejects_untrusted_old04_or_mixed_reuse_metadata(self):
        for failure in ("old-missing", "old-version", "old-failed", "old-failure-missing", "parent", "binding", "product", "source-shape",
                        "report-pin", "epoch-pin", "binding-pin", "source", "contracts", "extra"):
            value = successor_admission_fixture()
            reuse = value["report"]["reusedAdmission04"]
            if failure == "old-missing":
                value["old"] = None
            elif failure == "old-version":
                value["old"]["version"] = 3
            elif failure == "old-failed":
                value["old"]["candidateAdmissionComplete"] = False
            elif failure == "old-failure-missing":
                value["old"].pop("failure")
            elif failure == "parent":
                value["epoch"]["previousEpoch"]["sha256"] = "0" * 64
            elif failure == "binding":
                value["product"]["previousBinding"]["sha256"] = "0" * 64
            elif failure == "product":
                value["product"] = None
            elif failure == "source-shape":
                value["old"]["currentSource"].pop("archiveSha256")
            elif failure in ("report-pin", "epoch-pin", "binding-pin"):
                key = {"report-pin": "report", "epoch-pin": "runtimeEpoch", "binding-pin": "seedRuntimeBinding"}[failure]
                reuse[key]["sha256"] = "0" * 64
            elif failure == "source":
                reuse["currentSource"]["binary"]["sha256"] = "0" * 64
            elif failure == "contracts":
                reuse["contracts"].append("new_backup_or_restore")
            else:
                reuse["unreviewed"] = True
            with self.subTest(failure=failure), self.assertRaises(m.RunError):
                check_successor_admission(value)

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

    def test_version4_requires_reviewed_movie_without_widening_legacy_inputs(self):
        value, unused, unused_binding, unused_retained = reviewed_fixture()
        self.assertIs(m.validate_input(value), value)
        for mutate in (lambda row: row.update(version=4.0), lambda row: row.update(version=True),
                       lambda row: row.update(retainedBaseline=None), lambda row: row.pop("retainedBaseline"),
                       lambda row: row.update(reviewedBaseline=copy.deepcopy(row["retainedBaseline"]))):
            changed = copy.deepcopy(value)
            mutate(changed)
            with self.assertRaises(m.RunError):
                m.validate_input(changed)
        for scenario in m.SCENARIOS - {"movie"}:
            with self.subTest(scenario=scenario), self.assertRaises(m.RunError):
                m.validate_input({**value, "scenario": scenario})
        for version in (1, 2, 3):
            with self.subTest(version=version), self.assertRaises(m.RunError):
                m.validate_input({**value, "version": version})
        replay_path, replay_hash = str(m.R / "synthetic-reviewed-cases.json"), "a" * 64
        arguments = ["tests.py", "-v", "--reviewed-baselines", replay_path, "--reviewed-baselines-sha256", replay_hash]
        self.assertEqual(reviewed_replay_arguments(arguments), {"path": replay_path, "sha256": replay_hash})
        self.assertEqual(arguments, ["tests.py", "-v"])
        for arguments in (["tests.py", "--reviewed-baselines", replay_path], ["tests.py", "--reviewed-baselines-sha256", replay_hash],
                          ["tests.py", "--reviewed-baselines", replay_path, "--reviewed-baselines-sha256"],
                          ["tests.py", "--reviewed-baselines", "--reviewed-baselines-sha256", replay_hash],
                          ["tests.py", "--reviewed-baselines", replay_path, "--reviewed-baselines", replay_path, "--reviewed-baselines-sha256", replay_hash],
                          ["tests.py", "--reviewed-baselines", replay_path, "--reviewed-baselines-sha256", replay_hash, "--reviewed-baselines-sha256", replay_hash],
                          ["tests.py", "--reviewed-baselines=" + replay_path, "--reviewed-baselines-sha256", replay_hash]):
            with self.assertRaises(m.RunError):
                reviewed_replay_arguments(arguments)
        for raw in (b'{"value":0,"value":1}', b'{"value":NaN}', b'{"value":Infinity}', b'{"value":1e999}'):
            with self.assertRaises(m.RunError):
                reviewed_replay_json(raw)
        self.assertEqual(reviewed_replay_json(b'{"value":9223372036854775807}')["value"], 9223372036854775807)

    def test_reviewed_current_state_keeps_movie_provenance_and_foreign_references(self):
        for factory in (reviewed_fixture, reviewed_subtitles_fixture):
            unused, before, binding, retained = factory()
            prepared = m.validate_reviewed_movie_baseline(retained, binding)
            self.assertEqual([row["id"] for row in prepared], [m.MOVIE05_PREPARED])
            original = copy.deepcopy(retained)
            m.verify_actor_before(before, binding, "movie", retained, 4)
            self.assertEqual(m.canonical(retained), m.canonical(original))
            with self.assertRaisesRegex(m.RunError, "reviewed_movie_baseline_required"):
                m.verify_actor_before(before, binding, "movie", None, 4)
        self.assertEqual(len(before["tables"]["client_playback_references"]), 2)
        self.assertNotEqual(retained["review"]["sourceEpoch"], retained["review"]["movieProvenance"]["sourceEpoch"])
        for count in (0, 2):
            unused, unused_before, binding, retained = reviewed_fixture()
            if count == 0:
                for snapshot in (retained["snapshot"], retained["movieProvenance"]["snapshot"]):
                    row = next(row for row in snapshot["tables"]["play_sessions"] if row["id"] == m.MOVIE05_PREPARED)
                    row.update(state="Expired", stopped_at="2026-09-13T12:19:00Z")
                retained["review"]["preparedExpirations"] = []
            else:
                for snapshot in (retained["snapshot"], retained["movieProvenance"]["snapshot"]):
                    row = next(row for row in snapshot["tables"]["play_sessions"] if row["id"] == m.MOVIE05_PREPARED)
                    snapshot["tables"]["play_sessions"].append({**copy.deepcopy(row), "id": "play_extra"})
                retained["review"]["preparedExpirations"].append({"playSessionId": "play_extra", "authSessionId": m.MOVIE05_AUTH})
            self.assertEqual(len(m.validate_reviewed_movie_baseline(retained, binding)), count)

    def test_reviewed_baseline_rejects_wrong_pins_source_actor_and_movie_provenance(self):
        mutations = [lambda row: row["review"].update(version=True), lambda row: row["review"].update(extra=True),
            lambda row: row["review"]["runtimeEpoch"].update(sha256="0" * 64),
            lambda row: row["inputBinding"]["seedBinding"].update(sha256="0" * 64),
            lambda row: row["review"]["actor"].update(username="other"),
            lambda row: row["review"]["item"].update(runtimeTicks=True),
            lambda row: row["movieProvenance"]["manifest"].update(serverId="0" * 32),
            lambda row: row["movieProvenance"]["manifest"].update(runId="movie-x"),
            lambda row: row["movieProvenance"]["closeout"]["inputEvidence"]["after"].update(sha256="0" * 64),
            lambda row: row["movieProvenance"]["sourceEpoch"]["currentSource"]["binary"].update(sha256="0" * 64),
            lambda row: row["sourceEpoch"]["currentSource"]["sourceManifest"].update(sha256="0" * 64),
            lambda row: row["manifest"].update(scenario="movie"),
            lambda row: row["review"]["movieProvenance"].pop("snapshot")]
        for index, mutate in enumerate(mutations):
            unused, unused_before, binding, retained = reviewed_subtitles_fixture()
            mutate(retained)
            with self.subTest(index=index), self.assertRaises(m.RunError):
                m.validate_reviewed_movie_baseline(retained, binding)

    def test_reviewed_movie_history_rejects_live_credentials_duplicates_and_unreviewed_prepared_rows(self):
        mutations = [lambda row: row["snapshot"]["tables"]["sessions"][0].update(revoked_at=None),
            lambda row: row["snapshot"]["tables"]["sessions"].append(copy.deepcopy(row["snapshot"]["tables"]["sessions"][0])),
            lambda row: row["snapshot"]["tables"]["play_sessions"].append(copy.deepcopy(row["snapshot"]["tables"]["play_sessions"][0])),
            lambda row: row["snapshot"]["tables"]["play_sessions"][0].update(counted=True),
            lambda row: row["snapshot"]["tables"]["play_sessions"][0].update(application_client_id="other"),
            lambda row: row["snapshot"]["tables"]["play_sessions"][0].update(position_ticks=True),
            lambda row: row["snapshot"]["tables"]["play_sessions"][0].update(position_ticks=9007199254740992),
            lambda row: row["snapshot"]["tables"]["play_sessions"][0].update(expires_at="invalid"),
            lambda row: row["closeout"].update(failure=None),
            lambda row: row["closeout"]["checks"]["ownedData"].update(oldRowsDeleted=False),
            lambda row: row["closeout"]["checks"]["savedRuntimeAndWorkers"].update(candidateContinuous=1)]
        for index, mutate in enumerate(mutations):
            unused, unused_before, binding, retained = reviewed_subtitles_fixture()
            mutate(retained["movieProvenance"])
            with self.subTest(index=index), self.assertRaises(m.RunError):
                m.validate_reviewed_movie_baseline(retained, binding)
        for mutate in (lambda row: row.update(preparedExpirations=[]),
                       lambda row: row["preparedExpirations"].append(copy.deepcopy(row["preparedExpirations"][0])),
                       lambda row: row["preparedExpirations"][0].update(authSessionId="other"),
                       lambda row: row["preparedExpirations"][0].update(playSessionId="play_foreign")):
            unused, unused_before, binding, retained = reviewed_subtitles_fixture()
            mutate(retained["review"])
            with self.assertRaises(m.RunError):
                m.validate_reviewed_movie_baseline(retained, binding)

    def test_reviewed_subtitles_closeout_requires_exact_counts_binding_and_worker_closure(self):
        mutations = [lambda row: row["closeout"].update(sourcePinsUnchanged=1),
            lambda row: row["closeout"].update(failure=None),
            lambda row: (row["closeout"].pop("originalUIRejection"), row["closeout"].update(originalObservationUnchanged=True)),
            lambda row: row["closeout"].update(originalObservationUnchanged=True, originalUIRejection={}),
            lambda row: row["closeout"].update(originalObservationUnchanged=True, originalUIRejection={"originalObservationUnchanged": False}),
            lambda row: row["closeout"].update(originalObservationUnchanged=True, originalUIRejection={"originalObservationUnchanged": None}),
            lambda row: row["closeout"].update(originalObservationUnchanged=True, originalUIRejection=None),
            lambda row: row["closeout"].update(originalObservationUnchanged=True, originalUIRejection=True),
            lambda row: row["closeout"]["originalUIRejection"].update(originalObservationUnchanged=1),
            lambda row: row["closeout"]["sourceState"].update(afterClientReferences=0),
            lambda row: row["closeout"]["sourceState"].update(allSessionsRevoked=1),
            lambda row: row["closeout"]["inputEvidence"].update(version=2),
            lambda row: row["closeout"]["inputEvidence"]["boundary"].update(sha256="0" * 64),
            lambda row: row.update(boundary=None), lambda row: row["review"].update(boundary=None),
            lambda row: row["boundary"]["candidateAfter"].update(startTicks="different"),
            lambda row: row["boundary"]["postgresAfter"].update(pid=789),
            lambda row: row["boundary"]["leaseAfter"].update(held=1),
            lambda row: row["boundary"]["clientWorker"].update(mainPID=False),
            lambda row: row["boundary"]["clientWorker"].update(remainingBrowserPids=[123]),
            lambda row: row["boundary"]["gatewayWorker"].update(exitCode=1),
            lambda row: row["boundary"]["gatewayWorker"]["index"].update(sha256="0" * 64),
            lambda row: row["boundary"].update(afterMonotonicNs="99")]
        for index, mutate in enumerate(mutations):
            unused, unused_before, binding, retained = reviewed_subtitles_fixture()
            mutate(retained)
            with self.subTest(index=index), self.assertRaises(m.RunError):
                m.validate_reviewed_movie_baseline(retained, binding)

    def test_reviewed_latest_snapshot_cannot_rewrite_movie_rows_or_hide_owned_residue(self):
        for table, field, value in (("users", "name", "renamed"), ("sessions", "device_id", "other-device"),
                                    ("play_sessions", "player_state", {"changed": True}), ("user_item_data", "play_count", 3)):
            unused, unused_before, binding, retained = reviewed_subtitles_fixture()
            actor = binding["actors"]["movie"]["id"]
            key = "id" if table == "users" else "user_id"
            row = next(row for row in retained["snapshot"]["tables"][table] if row[key] == actor)
            row[field] = value
            with self.subTest(table=table), self.assertRaisesRegex(m.RunError, "reviewed_movie_history_changed"):
                m.validate_reviewed_movie_baseline(retained, binding)
        for table in ("client_playback_references", "encoding_jobs"):
            for field, value in (("user_id", "a" * 32), ("auth_session_id", m.MOVIE05_AUTH), ("play_session_id", m.MOVIE05_PREPARED)):
                unused, unused_before, binding, retained = reviewed_subtitles_fixture()
                retained["snapshot"]["tables"][table].append({"user_id": "b" * 32, field: value})
                count = "afterClientReferences" if table == "client_playback_references" else "afterEncodingJobs"
                retained["closeout"]["sourceState"][count] += 1
                with self.subTest(table=table, field=field), self.assertRaisesRegex(m.RunError, "reviewed_movie_owned_residue"):
                    m.validate_reviewed_movie_baseline(retained, binding)

    def test_reviewed_before_preserves_all_foreign_rows_sequences_and_json_types(self):
        mutations = [lambda row: row["tables"]["users"][1]["policy"].update(EnableMediaPlayback=0),
            lambda row: row["tables"]["activity_entries"][0]["data"].update(enabled=1),
            lambda row: row["tables"]["client_playback_references"].pop(),
            lambda row: row["tables"]["sessions"][0].update(revoked_at="2026-09-13T12:31:00Z"),
            lambda row: row["tables"]["play_sessions"].pop(),
            lambda row: row["tables"].update(extra_table=row["tables"].pop("task_runs")),
            lambda row: row["sequences"]["synthetic"].update(isCalled=1),
            lambda row: row["sequences"]["synthetic"].update(lastValue="2")]
        for index, mutate in enumerate(mutations):
            unused, before, binding, retained = reviewed_subtitles_fixture()
            mutate(before)
            with self.subTest(index=index), self.assertRaisesRegex(m.RunError, "reviewed_movie_fresh_state_changed"):
                m.verify_actor_before(before, binding, "movie", retained, 4)

    def test_reviewed_pruning_checks_terminal_rows_and_nanosecond_deadline(self):
        unused, before, binding, retained = reviewed_subtitles_fixture()
        before["capturedAt"] = "2026-09-13T12:29:59.999999999Z"
        with self.assertRaisesRegex(m.RunError, "reviewed_movie_pruning_deadline"):
            m.verify_actor_before(before, binding, "movie", retained, 4)
        for fraction, passes in (("000000000", False), ("000000001", True)):
            unused, before, binding, retained = reviewed_subtitles_fixture()
            for snapshot in (before, retained["snapshot"], retained["movieProvenance"]["snapshot"]):
                snapshot["tables"]["play_sessions"][0]["expires_at"] = "2026-09-06T12:50:00." + fraction + "Z"
            if passes:
                m.verify_actor_before(before, binding, "movie", retained, 4)
            else:
                with self.assertRaisesRegex(m.RunError, "reviewed_movie_pruning_deadline"):
                    m.verify_actor_before(before, binding, "movie", retained, 4)

    def test_reviewed_preflight_reads_all_dependencies_and_fails_before_output_or_workers(self):
        value, unused_before, binding, retained = reviewed_subtitles_fixture()
        # Model a future component admission without changing the frozen pins.
        value["sources"]["closer"]["sha256"] = "0" * 64
        with patch.object(m, "FROZEN", {**m.FROZEN, "closer": "0" * 64}):
            job = make_job(value)
        epoch = {"helpers": {"seed": {}}, "runtimeHelper": m.RUNTIME, "currentSource": {"schema": 28}}
        report = {"kind": "audited-candidate-live-admission", "version": 2, "status": "admission_failed_resources_retained"}
        pins = {job.input_pin["path"]: value, m.BINDING["path"]: binding, m.ADMISSION["path"]: report, "original-seed": {},
            value["retainedBaseline"]["path"]: retained["review"]}
        for key in ("closeout", "snapshot", "sourceEpoch", "manifest"):
            pins[retained["review"][key]["path"]] = retained[key]
            pins[retained["review"]["movieProvenance"][key]["path"]] = retained["movieProvenance"][key]
        pins[retained["review"]["boundary"]["path"]] = retained["boundary"]
        read = Mock(side_effect=lambda pin: pins[pin["path"]])
        seed = types.SimpleNamespace(descriptor=read)
        job.r = types.SimpleNamespace(read_bootstrap=lambda pin: json.dumps(epoch), load_helper=lambda key, pin: seed,
            validate_epoch=lambda row: row, validate_seed_runtime_binding=Mock(), SEED={"path": "original-seed"})
        job.open, job.start_worker = Mock(), Mock()
        with patch.object(m.os.path, "lexists", return_value=False), self.assertRaisesRegex(m.RunError, "successful_current_admission_required"):
            job.run()
        requested = [call.args[0] for call in read.call_args_list]
        for pin in [value["retainedBaseline"], retained["review"]["boundary"], *retained["review"]["movieProvenance"].values(),
                    *[retained["review"][key] for key in ("closeout", "snapshot", "sourceEpoch", "manifest")]]:
            self.assertIn(pin, requested)
        job.open.assert_not_called()
        job.start_worker.assert_not_called()
        read.reset_mock()
        retained["review"]["actor"]["username"] = "wrong"
        with patch.object(m.os.path, "lexists", return_value=False), self.assertRaisesRegex(m.RunError, "reviewed_movie_actor_binding"):
            job.run()
        self.assertNotIn(m.ADMISSION, [call.args[0] for call in read.call_args_list])
        job.open.assert_not_called()
        job.start_worker.assert_not_called()

    def test_version4_old_closer_stops_before_any_dispatch_or_output(self):
        value, unused_before, unused_binding, unused_retained = reviewed_subtitles_fixture()
        self.assertIs(m.validate_input(value), value)
        job = make_job(value)
        job.r.read_bootstrap = Mock()
        job.open, job.start_worker, job.save = Mock(), Mock(), Mock()
        with patch.object(m.os.path, "lexists", return_value=False), self.assertRaisesRegex(m.RunError, "version4_closer_not_admitted"):
            job.run()
        job.r.read_bootstrap.assert_not_called()
        job.open.assert_not_called()
        job.start_worker.assert_not_called()
        job.save.assert_not_called()
        self.assertFalse(job.created)
        self.assertEqual(job.workers, {})

    def test_version5_is_tv_only_and_cannot_relabel_movie_contracts(self):
        value, before, binding, retained = reviewed_tv_fixture()
        self.assertIs(m.validate_input(value), value)
        for mutate in (lambda row: row.pop("currentRuntime"), lambda row: row.update(currentRuntime=None),
                       lambda row: row["currentRuntime"].update(sha256="invalid")):
            changed = copy.deepcopy(value)
            mutate(changed)
            with self.assertRaises(m.RunError):
                m.validate_input(changed)
        for version in (1, 2, 3, 4):
            legacy = fixture()
            if version == 2:
                legacy.update(version=2, retainedBaseline=copy.deepcopy(m.RETAINED_BASELINE))
            elif version == 3:
                legacy.update(version=3, retainedBaseline=copy.deepcopy(m.MOVIE05_BASELINE))
            elif version == 4:
                legacy = reviewed_fixture()[0]
            legacy["currentRuntime"] = copy.deepcopy(value["currentRuntime"])
            with self.subTest(legacy=version), self.assertRaises(m.RunError):
                m.validate_input(legacy)
        for scenario in m.SCENARIOS - {"tv-browse"}:
            with self.subTest(scenario=scenario), self.assertRaises(m.RunError):
                m.validate_input({**value, "scenario": scenario})
        for version in (1, 2, 3, 4, 5.0, True):
            with self.subTest(version=version), self.assertRaises(m.RunError):
                m.validate_input({**value, "version": version})
        with self.assertRaisesRegex(m.RunError, "reviewed_tv_baseline_required"):
            m.verify_actor_before(before, binding, "tv-browse", None, 5)
        with self.assertRaises(m.RunError):
            m.validate_reviewed_movie_baseline(retained, binding)
        unused, unused_before, movie_binding, movie = reviewed_subtitles_fixture()
        with self.assertRaises(m.RunError):
            m.validate_reviewed_tv_baseline(movie, movie_binding)
        for mutate in (lambda row: row["catalog"]["episodes"].append(copy.deepcopy(row["catalog"]["episodes"][2])),
                       lambda row: row["catalog"]["episodes"][2].update(indexNumber=True)):
            changed = copy.deepcopy(binding)
            mutate(changed)
            with self.assertRaisesRegex(m.RunError, "reviewed_tv_detail_target"):
                m.validate_reviewed_tv_baseline(retained, changed)
        replay_path, replay_hash = str(m.R / "synthetic-tv-replay-input.json"), "a" * 64
        arguments = ["tests.py", REVIEWED_TV_REPLAY_FLAGS[0], replay_path, REVIEWED_TV_REPLAY_FLAGS[1], replay_hash]
        self.assertEqual(reviewed_replay_arguments(arguments, REVIEWED_TV_REPLAY_FLAGS), {"path": replay_path, "sha256": replay_hash})
        self.assertEqual(arguments, ["tests.py"])
        for arguments in (["tests.py", REVIEWED_TV_REPLAY_FLAGS[0], replay_path], ["tests.py", REVIEWED_TV_REPLAY_FLAGS[1], replay_hash],
                          ["tests.py", REVIEWED_TV_REPLAY_FLAGS[0], replay_path, REVIEWED_TV_REPLAY_FLAGS[1]],
                          ["tests.py", REVIEWED_TV_REPLAY_FLAGS[0], replay_path, REVIEWED_TV_REPLAY_FLAGS[0], replay_path, REVIEWED_TV_REPLAY_FLAGS[1], replay_hash]):
            with self.assertRaises(m.RunError):
                reviewed_replay_arguments(arguments, REVIEWED_TV_REPLAY_FLAGS)

    def test_reviewed_tv_loader_uses_explicit_provenance_and_preserves_foreign_references(self):
        value, before, binding, retained = reviewed_tv_fixture()
        documents = {value["retainedBaseline"]["path"]: retained["review"], retained["review"]["boundary"]["path"]: retained["boundary"]}
        for key in ("closeout", "snapshot", "sourceEpoch", "manifest"):
            documents[retained["review"][key]["path"]] = retained[key]
            documents[retained["review"]["tvProvenance"][key]["path"]] = retained["tvProvenance"][key]
        documents[retained["review"]["postBrowseAdmission"]["path"]] = retained["postBrowseAdmission"]
        reader = Mock(side_effect=lambda pin: documents[pin["path"]])
        loaded = m.load_reviewed_tv_baseline(value["retainedBaseline"], retained["inputBinding"], binding, reader)
        self.assertEqual(m.canonical(loaded), m.canonical(retained))
        self.assertEqual(len(m.validate_reviewed_tv_baseline(loaded, binding)), 1)
        original = hashlib.sha256(m.canonical(loaded).encode()).hexdigest()
        m.verify_actor_before(before, binding, "tv-browse", loaded, 5)
        self.assertEqual(hashlib.sha256(m.canonical(loaded).encode()).hexdigest(), original)
        self.assertEqual(len(before["tables"]["client_playback_references"]), 2)
        requested = [call.args[0] for call in reader.call_args_list]
        for pin in [value["retainedBaseline"], retained["review"]["boundary"], retained["review"]["postBrowseAdmission"], *retained["review"]["tvProvenance"].values(),
                    *[retained["review"][key] for key in ("closeout", "snapshot", "sourceEpoch", "manifest")]]:
            self.assertIn(pin, requested)

    def test_reviewed_tv_post_browse_auth_exception_rejects_unproved_state_changes(self):
        unused, unused_before, binding, retained = reviewed_tv_fixture()
        self.assertEqual(len(m.validate_reviewed_tv_baseline(retained, binding)), 1)
        for change in ("unknown-extra", "unrevoked", "associated-play", "old-row"):
            unused, unused_before, binding, retained = reviewed_tv_fixture()
            tables = retained["snapshot"]["tables"]
            proof = retained["postBrowseAdmission"]["controllerSessions"]["P"]
            extra = next(row for row in tables["sessions"] if row["id"] == proof["credentialId"])
            if change == "unknown-extra":
                tables["sessions"].append({**copy.deepcopy(extra), "id": "ab" * 16})
                retained["closeout"]["sourceState"]["afterSessions"] += 1
            elif change == "unrevoked":
                extra["revoked_at"] = None
            elif change == "associated-play":
                play = next(row for row in tables["play_sessions"] if row.get("user_id") == binding["actors"]["tv-browse"]["id"])
                tables["play_sessions"].append({**copy.deepcopy(play), "id": "play_unproved_admission", "auth_session_id": proof["credentialId"]})
                retained["closeout"]["sourceState"]["afterPlays"] += 1
            else:
                old = next(row for row in tables["sessions"] if row["id"] == "synthetic-tv-auth")
                old["device_id"] = "changed-old-device"
            with self.subTest(change=change), self.assertRaises(m.RunError):
                m.validate_reviewed_tv_baseline(retained, binding)

    def test_reviewed_tv_provenance_rejects_playback_history_or_unreviewed_preparation(self):
        mutations = [lambda row: row["review"].update(kind="audited-candidate-reviewed-movie-baseline"),
            lambda row: row["review"].update(boundary=None), lambda row: row["review"].update(preparedExpirations=[]),
            lambda row: row["review"]["preparedExpirations"].append(copy.deepcopy(row["review"]["preparedExpirations"][0])),
            lambda row: row["review"]["preparedExpirations"][0].update(authSessionId="foreign"),
            lambda row: row["review"]["item"].update(id="9" * 32),
            lambda row: row["tvProvenance"]["closeout"].update(failure=None),
            lambda row: row["tvProvenance"]["closeout"].update(physicalMediaRequests=1),
            lambda row: row["tvProvenance"]["closeout"].update(playingReports=False),
            lambda row: row["tvProvenance"]["closeout"]["inputEvidence"]["after"].update(sha256="0" * 64),
            lambda row: row["tvProvenance"]["closeout"]["checks"]["ownedData"].update(newCountedPlays=1),
            lambda row: row["tvProvenance"]["closeout"]["checks"]["ownedData"]["unstartedPreparations"][0].update(prepareOrdinals=[277, 278]),
            lambda row: row["tvProvenance"]["closeout"]["checks"]["ownedData"]["unstartedPreparations"][0].update(prepareOrdinals=[True]),
            lambda row: row["tvProvenance"]["snapshot"]["tables"]["play_sessions"][-1].update(counted=True),
            lambda row: row["tvProvenance"]["snapshot"]["tables"]["play_sessions"][-1].update(started_at="2026-09-13T12:19:01Z"),
            lambda row: row["tvProvenance"]["snapshot"]["tables"]["play_sessions"][-1].update(position_ticks=1),
            lambda row: row["tvProvenance"]["snapshot"]["tables"]["user_item_data"][-1].update(play_count=1),
            lambda row: row["tvProvenance"]["snapshot"]["tables"]["sessions"][-1].update(revoked_at=None),
            lambda row: row["tvProvenance"]["snapshot"]["tables"]["sessions"].append(copy.deepcopy(row["tvProvenance"]["snapshot"]["tables"]["sessions"][-1])),
            lambda row: row["tvProvenance"]["sourceEpoch"]["currentSource"]["binary"].update(sha256="0" * 64)]
        for index, mutate in enumerate(mutations):
            unused, unused_before, binding, retained = reviewed_tv_fixture()
            mutate(retained)
            with self.subTest(index=index), self.assertRaises(m.RunError):
                m.validate_reviewed_tv_baseline(retained, binding)

    def test_reviewed_tv_latest_state_requires_exact_actor_history_catalog_and_no_owned_residue(self):
        for table, field, value in (("users", "name", "renamed"), ("sessions", "device_id", "other"),
                                    ("play_sessions", "player_state", {"changed": True}), ("user_item_data", "updated_at", "2026-09-13T12:30:00Z")):
            unused, unused_before, binding, retained = reviewed_tv_fixture()
            key = "id" if table == "users" else "user_id"
            row = next(row for row in retained["snapshot"]["tables"][table] if row.get(key) == binding["actors"]["tv-browse"]["id"])
            row[field] = value
            with self.subTest(table=table), self.assertRaisesRegex(m.RunError, "reviewed_tv_history_changed"):
                m.validate_reviewed_tv_baseline(retained, binding)
        for table in ("client_playback_references", "encoding_jobs"):
            for field, value in (("user_id", "f" * 32), ("auth_session_id", "synthetic-tv-auth"), ("play_session_id", "play_synthetic_tv")):
                unused, unused_before, binding, retained = reviewed_tv_fixture()
                retained["snapshot"]["tables"][table].append({"user_id": "b" * 32, field: value})
                count = "afterClientReferences" if table == "client_playback_references" else "afterEncodingJobs"
                retained["closeout"]["sourceState"][count] += 1
                with self.subTest(table=table, field=field), self.assertRaisesRegex(m.RunError, "reviewed_tv_owned_residue"):
                    m.validate_reviewed_tv_baseline(retained, binding)
        for mutate in (lambda row: row["manifest"]["catalog"]["episodes"][2].update(indexNumber=2),
                       lambda row: row["boundary"]["clientWorker"].update(mainPID=False),
                       lambda row: row["snapshot"].update(capturedAt="2026-09-13T12:19:59Z")):
            unused, unused_before, binding, retained = reviewed_tv_fixture()
            mutate(retained)
            with self.assertRaises(m.RunError):
                m.validate_reviewed_tv_baseline(retained, binding)

    def test_reviewed_tv_before_preserves_foreign_tables_sequences_and_nanosecond_pruning(self):
        mutations = [lambda row: row["tables"]["client_playback_references"][0].update(client_nonce="changed"),
            lambda row: row["tables"]["users"][1]["policy"].update(EnableMediaPlayback=0),
            lambda row: row["tables"]["play_sessions"][0]["player_state"].update(changed=True),
            lambda row: row["sequences"]["synthetic"].update(lastValue="2")]
        for index, mutate in enumerate(mutations):
            unused, before, binding, retained = reviewed_tv_fixture()
            mutate(before)
            with self.subTest(index=index), self.assertRaisesRegex(m.RunError, "reviewed_tv_fresh_state_changed"):
                m.verify_actor_before(before, binding, "tv-browse", retained, 5)
        for fraction, passes in (("000000000", False), ("000000001", True)):
            unused, before, binding, retained = reviewed_tv_fixture()
            for snapshot in (before, retained["snapshot"], retained["tvProvenance"]["snapshot"]):
                snapshot["tables"]["play_sessions"][-1]["expires_at"] = "2026-09-06T12:50:00." + fraction + "Z"
            if passes:
                m.verify_actor_before(before, binding, "tv-browse", retained, 5)
            else:
                with self.assertRaisesRegex(m.RunError, "reviewed_tv_pruning_deadline"):
                    m.verify_actor_before(before, binding, "tv-browse", retained, 5)

    def test_version5_rejects_old_closer_and_component_receipt_before_output_or_dispatch(self):
        for newer_closer, reason in ((False, "version5_closer_not_admitted"), (True, "version5_components_not_admitted")):
            value, unused_before, unused_binding, unused_retained = reviewed_tv_fixture()
            if newer_closer:
                value["sources"]["closer"]["sha256"] = "0" * 64
            with patch.object(m, "FROZEN", {**m.FROZEN, "closer": value["sources"]["closer"]["sha256"]}):
                job = make_job(value)
            job.r.read_bootstrap = Mock()
            job.open, job.start_worker, job.save = Mock(), Mock(), Mock()
            with patch.object(m.os.path, "lexists", return_value=False), self.subTest(newer_closer=newer_closer), self.assertRaisesRegex(m.RunError, reason):
                job.run()
            job.r.read_bootstrap.assert_not_called()
            job.open.assert_not_called()
            job.start_worker.assert_not_called()
            job.save.assert_not_called()
            self.assertFalse(job.created)
            self.assertEqual(job.workers, {})

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
        self.assert_failed_browser_log_is_saved(3)

    def test_version4_server_log_is_saved_when_browser_exit_is_one(self):
        self.assert_failed_browser_log_is_saved(4)

    def test_version5_diagnostic_closes_after_browser_exit_zero_or_one(self):
        for exit_code in ("0", "1"):
            job, events, saved, summary = diagnostic_job_fixture(exit_code)
            with self.subTest(exit_code=exit_code), patch.dict(m.os.environ, {"SSH_CONNECTION": "synthetic-ssh"}):
                result = job.run()
            self.assertEqual(result["status"], "owned_state_closed_diagnostic_result")
            self.assertIs(result["clientAcceptance"], False)
            for key in ("uiAccepted", "originalBrowserOutcome", "originalBrowserFailure", "diagnostic"):
                self.assertEqual(result[key], summary[key])
            self.assertEqual(saved["boundary.json"]["clientWorker"]["exitCode"], int(exit_code))
            self.assertEqual(saved["closeout-input.json"]["version"], 5)
            self.assertEqual(saved["closeout-input.json"]["currentRuntime"], job.value["currentRuntime"])
            self.assertEqual(saved["closeout-input.json"]["serverLog"], job.server_log_pin)
            self.assertLess(events.index("close_gateway"), events.index("source_after"))
            self.assertLess(events.index("source_after"), events.index("server-log.json"))
            self.assertLess(events.index("server-log.json"), events.index("boundary.json"))
            job.p.command.assert_called_once()
            self.assertEqual(job.p.command.call_args.args[0], "offline-client-closeout")

    def test_version5_diagnostic_rejects_worker_cleanup_terminal_and_closer_failures(self):
        cases = [("cleanup", "worker_responsibility_unclosed", False), ("worker", "worker_responsibility_unclosed", False),
            ("signal", "diagnostic_browser_worker_failed", False), ("timeout", "diagnostic_browser_worker_failed", False),
            ("exit-two", "diagnostic_browser_worker_failed", False), ("success-one", "diagnostic_browser_worker_failed", False),
            ("exit-code-zero", "diagnostic_browser_worker_failed", False), ("gateway", "gateway_exit_status_not_observed", False),
            ("closer-command", "synthetic_closer_failed", True), ("closer-status", "offline_diagnostic_closeout_incomplete", True),
            ("client-acceptance", "offline_diagnostic_closeout_incomplete", True), ("diagnostic-status", "offline_diagnostic_closeout_incomplete", True),
            ("missing-original", "offline_diagnostic_closeout_incomplete", True), ("ui-type", "offline_diagnostic_closeout_incomplete", True)]
        for failure, reason, dispatched in cases:
            job, events, saved, summary = diagnostic_job_fixture()
            if failure == "cleanup":
                def close(role):
                    events.append("close_" + role)
                    if role == "browser":
                        raise m.RunError("synthetic_cleanup_failed")
                job.close_worker = close
            elif failure == "worker":
                job.wait_client.side_effect = TimeoutError("synthetic_worker_timeout")
            elif failure in ("signal", "timeout", "exit-two", "success-one", "exit-code-zero"):
                outcome, status = {"signal": ("signal", "9"), "timeout": ("timeout", "1"), "exit-two": ("exit-code", "2"),
                    "success-one": ("success", "1"), "exit-code-zero": ("exit-code", "0")}[failure]
                job.workers["browser"]["terminal"].update(Result=outcome, ExecMainStatus=status)
            elif failure == "gateway":
                job.workers["gateway"]["terminal"].update(Result="signal", ExecMainStatus="9")
            elif failure == "closer-command":
                job.p.command.side_effect = m.RunError("synthetic_closer_failed")
            elif failure == "closer-status":
                summary["status"] = "core_scenario_closed"
            elif failure == "client-acceptance":
                summary["clientAcceptance"] = True
            elif failure == "diagnostic-status":
                summary["diagnostic"]["status"] = "accepted"
            elif failure == "missing-original":
                summary.pop("originalBrowserFailure")
            elif failure == "ui-type":
                summary["uiAccepted"] = 1
            with self.subTest(failure=failure), patch.dict(m.os.environ, {"SSH_CONNECTION": "synthetic-ssh"}), self.assertRaisesRegex(m.RunError, reason):
                job.run()
            self.assertIn("server-log.json", saved)
            if dispatched:
                job.p.command.assert_called_once()
                self.assertIn("boundary.json", saved)
            else:
                job.p.command.assert_not_called()
                self.assertNotIn("boundary.json", saved)

    def assert_failed_browser_log_is_saved(self, version):
        if version == 4:
            value, source, binding, retained = reviewed_subtitles_fixture()
        else:
            value = fixture()
            value.update(version=3, retainedBaseline=copy.deepcopy(m.MOVIE05_BASELINE))
            source, binding, retained = movie05_fixture()
        job = make_job(value)
        job.binding, job.retained = binding, retained
        actor = job.binding["actors"][value["scenario"]]
        actor["credentials"] = {"path": str(m.R / "synthetic-credential.json"), "sha256": "a"*64}
        job.binding.setdefault("serverId", "b"*32)
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

    def test_retained_movie_history_cannot_be_relabelled_as_the_current_successor(self):
        self.assertNotEqual(m.RETAINED_MOVIE_EPOCH, m.EPOCH)
        for factory, version in ((retained_fixture, 2), (movie05_fixture, 3)):
            before, binding, retained = factory()
            m.verify_actor_before(before, binding, "movie", retained, version)
            closeout = retained["closeout"]
            if version == 2:
                closeout["runtimeEpoch"] = copy.deepcopy(m.EPOCH)
            else:
                closeout["inputEvidence"]["epoch"] = copy.deepcopy(m.EPOCH)
            with self.subTest(version=version), self.assertRaises(m.RunError):
                m.verify_actor_before(before, binding, "movie", retained, version)

    def assert_reviewed_saved_case(self, position, expected):
        case = REVIEWED_REPLAY_CASES[position]
        seed = read_reviewed_replay_descriptor(case["seed"])
        retained = m.load_reviewed_movie_baseline(case["review"], case["inputBinding"], seed, read_reviewed_replay_descriptor)
        snapshot = retained["snapshot"]
        digest = lambda value: hashlib.sha256(m.canonical(value).encode()).hexdigest()
        retained_before = digest(retained)
        self.assertEqual(len(snapshot["tables"]), 35)
        self.assertEqual(len(snapshot["sequences"]), 5)
        self.assertEqual(tuple(len(snapshot["tables"][table]) for table in ("sessions", "play_sessions", "client_playback_references")), expected)
        self.assertEqual(len(retained["review"]["preparedExpirations"]), 1)
        self.assertEqual(len(m.validate_reviewed_movie_baseline(retained, seed)), 1)
        movie_pins = {**retained["review"]["movieProvenance"], "boundary": None}
        movie_records = {**retained["movieProvenance"], "boundary": None}
        m.validate_final_provenance(movie_records, movie_pins, "movie", seed)
        final_roles = ["movie"]
        if retained["manifest"]["scenario"] == "subtitles":
            m.validate_final_provenance({key: retained[key] for key in m.FINAL_PROVENANCE_KEYS},
                {key: retained["review"][key] for key in m.FINAL_PROVENANCE_KEYS}, "subtitles", seed)
            final_roles.append("subtitles")
        fresh = copy.deepcopy(snapshot)
        fresh_before, references_before = digest(fresh), digest(fresh["tables"]["client_playback_references"])
        m.verify_actor_before(fresh, seed, "movie", retained, 4)
        self.assertEqual(digest(fresh), fresh_before)
        for index, table in enumerate(sorted(m.REVIEWED_BASELINE_TABLES)):
            changed = copy.deepcopy(snapshot)
            if changed["tables"][table]:
                changed["tables"][table][0]["reviewedReplayMutation"] = True
            else:
                changed["tables"][table].append({"reviewedReplayMutation": True})
            with self.subTest(case=case["name"], tableIndex=index), self.assertRaisesRegex(m.RunError, "reviewed_movie_fresh_state_changed"):
                m.verify_actor_before(changed, seed, "movie", retained, 4)
        for index, sequence in enumerate(snapshot["sequences"]):
            changed = copy.deepcopy(snapshot)
            changed["sequences"][sequence]["lastValue"] = "1" if changed["sequences"][sequence]["lastValue"] == "0" else "0"
            with self.subTest(case=case["name"], sequenceIndex=index), self.assertRaisesRegex(m.RunError, "reviewed_movie_fresh_state_changed"):
                m.verify_actor_before(changed, seed, "movie", retained, 4)
        self.assertEqual(digest(retained), retained_before)
        self.assertEqual(digest(retained["snapshot"]["tables"]["client_playback_references"]), references_before)
        print(json.dumps({"kind": "audited-candidate-reviewed-baseline-python-replay", "case": case["name"],
            "reviewSha256": case["review"]["sha256"], "snapshotSha256": retained["review"]["snapshot"]["sha256"],
            "sessions": expected[0], "plays": expected[1], "foreignClientReferences": expected[2], "preparedExpirations": 1,
            "rejectedTableMutations": 35, "rejectedSequenceMutations": 5, "unchangedEvidenceSha256": retained_before,
            "foreignClientReferencesSha256": references_before, "finalClientProvenanceRoles": final_roles}))

    if REVIEWED_REPLAY_CASES is not None:
        def test_saved_reviewed_01_movie05_baseline(self):
            self.assert_reviewed_saved_case(0, (13, 5, 0))

        def test_saved_reviewed_02_movie06_baseline(self):
            self.assert_reviewed_saved_case(1, (14, 6, 0))

        def test_saved_reviewed_03_latest_subtitles_baseline(self):
            self.assert_reviewed_saved_case(2, (21, 13, 2))

    if EPISODE_PROVENANCE_REPLAY_INPUT is not None:
        def test_saved_episode_provenance(self):
            value = EPISODE_PROVENANCE_REPLAY_INPUT
            seed = read_reviewed_replay_descriptor(value["seed"])
            pins = value["provenance"]
            bundle = {key: read_reviewed_replay_descriptor(selected) for key, selected in pins.items()}
            digest = lambda content: hashlib.sha256(m.canonical(content).encode()).hexdigest()
            original = digest({"seed": seed, "bundle": bundle})
            self.assertEqual(seed["version"], 3)
            self.assertEqual(bundle["sourceEpoch"]["version"], 3)
            self.assertEqual(seed["runtimeEpoch"], pins["sourceEpoch"])
            rows = m.validate_final_provenance(bundle, pins, "episode", seed)
            snapshot = bundle["snapshot"]
            self.assertEqual(len(snapshot["tables"]), 35)
            self.assertEqual(len(snapshot["sequences"]), 5)
            self.assertEqual(tuple(len(snapshot["tables"][table]) for table in ("sessions", "play_sessions", "client_playback_references")), (20, 11, 2))
            self.assertEqual(len(rows["play_sessions"]), 2)
            self.assertEqual(sum(row["state"] == "Prepared" for row in rows["play_sessions"]), 1)
            self.assertEqual(digest({"seed": seed, "bundle": bundle}), original)
            print(json.dumps({"kind": "audited-candidate-episode-provenance-python-replay", "input": EPISODE_PROVENANCE_REPLAY_PIN,
                "provenance": pins, "seed": value["seed"], "productionValidator": "validate_final_provenance", "sessions": 20, "plays": 11,
                "foreignClientReferences": 2, "actorPlays": 2, "actorPrepared": 1, "tables": 35, "sequences": 5,
                "unchangedEvidenceSha256": original, "usedAsCurrentBaseline": False,
                "browserInputsCreated": 0, "httpCalls": 0, "sqlCalls": 0, "serviceCalls": 0, "clientAcceptance": False}))

    if REVIEWED_TV_REPLAY_INPUT is not None:
        def test_saved_reviewed_tv_baseline(self):
            value = REVIEWED_TV_REPLAY_INPUT
            seed = read_reviewed_replay_descriptor(value["seed"])
            retained = m.load_reviewed_tv_baseline(value["review"], value["inputBinding"], seed, read_reviewed_replay_descriptor)
            snapshot = retained["snapshot"]
            digest = lambda value: hashlib.sha256(m.canonical(value).encode()).hexdigest()
            original = digest(retained)
            self.assertEqual(retained["manifest"]["scenario"], "subtitles")
            self.assertEqual(retained["tvProvenance"]["manifest"]["scenario"], "tv-browse")
            self.assertEqual(len(snapshot["tables"]), 35)
            self.assertEqual(len(snapshot["sequences"]), 5)
            self.assertEqual(tuple(len(snapshot["tables"][table]) for table in ("sessions", "play_sessions", "client_playback_references")), (21, 13, 2))
            self.assertEqual(len(retained["review"]["preparedExpirations"]), 1)
            self.assertEqual(len(m.validate_reviewed_tv_baseline(retained, seed)), 1)
            fresh = copy.deepcopy(snapshot)
            fresh_before, references_before = digest(fresh), digest(fresh["tables"]["client_playback_references"])
            m.verify_actor_before(fresh, seed, "tv-browse", retained, 5)
            self.assertEqual(digest(fresh), fresh_before)
            for index, table in enumerate(sorted(m.REVIEWED_BASELINE_TABLES)):
                changed = copy.deepcopy(snapshot)
                if changed["tables"][table]:
                    changed["tables"][table][0]["reviewedReplayMutation"] = True
                else:
                    changed["tables"][table].append({"reviewedReplayMutation": True})
                with self.subTest(tableIndex=index), self.assertRaisesRegex(m.RunError, "reviewed_tv_fresh_state_changed"):
                    m.verify_actor_before(changed, seed, "tv-browse", retained, 5)
            for index, sequence in enumerate(snapshot["sequences"]):
                changed = copy.deepcopy(snapshot)
                row = changed["sequences"][sequence]
                m.need(isinstance(row, dict) and "lastValue" in row, "reviewed_tv_replay_sequence_shape")
                row["lastValue"] = "1" if row["lastValue"] == "0" else "0"
                with self.subTest(sequenceIndex=index), self.assertRaisesRegex(m.RunError, "reviewed_tv_fresh_state_changed"):
                    m.verify_actor_before(changed, seed, "tv-browse", retained, 5)
            self.assertEqual(digest(retained), original)
            self.assertEqual(digest(retained["snapshot"]["tables"]["client_playback_references"]), references_before)
            print(json.dumps({"kind": "audited-candidate-reviewed-tv-baseline-python-replay", "case": "latest-subtitles-tv",
                "reviewSha256": value["review"]["sha256"], "snapshotSha256": retained["review"]["snapshot"]["sha256"],
                "sessions": 21, "plays": 13, "foreignClientReferences": 2, "preparedExpirations": 1,
                "rejectedTableMutations": 35, "rejectedSequenceMutations": 5, "unchangedEvidenceSha256": original,
                "foreignClientReferencesSha256": references_before}))

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
