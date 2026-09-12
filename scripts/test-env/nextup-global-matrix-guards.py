#!/usr/bin/env python3
"""Remote-only pure matrix guards; no service, HTTP, credentials, or fixture use.

Run this file and the explicitly supplied matrix source through ssh test-env.
These synthetic observations test the protocol planner, not the reference server.
"""

from __future__ import annotations

from copy import deepcopy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys
import unittest
from urllib.parse import parse_qs, urlsplit


sys.dont_write_bytecode = True
if sys.platform != "linux" or not os.environ.get("SSH_CONNECTION") or len(sys.argv) != 2:
    print(json.dumps({"suite": "nextup-global-matrix-guards", "result": "blocked",
                      "reason": "An authorized SSH session and one matrix source path are required."}))
    raise SystemExit(2)

SOURCE = Path(sys.argv[1]).resolve(strict=True)
SPEC = importlib.util.spec_from_file_location("nextup_global_matrix_under_test", SOURCE)
M = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = M
SPEC.loader.exec_module(M)
SHA = "a" * 64
STAMP = "2026-09-12T01:00:00Z"


def fixture_manifest():
    manifest = {"schemaVersion": 1, "runId": "synthetic-guard-only", "target": "reference",
        "preparationReceiptSha256": SHA, "coordinationReceiptSha256": SHA, "catalogReceiptSha256": SHA,
        "serverId": "reference-test", "lifecycleSeparationSeconds": 2,
        "binding": {"processIdentity": {"pid": 1, "startTicks": "2"}, "version": "test",
                    "endpoint": "http://127.0.0.1:1", "isolationIdentity": "test-only",
                    "evidenceRoot": "/not-opened-by-guards", "fixtureReleased": True, "freshEvidenceRoot": True},
        "cleanupContract": {"mode": "episode-delete-played-items", "zeroBaselineRequired": True,
                            "verifiedForBoundTarget": True, "receiptSha256": SHA},
        "actors": {}, "libraries": {}, "items": {}}
    for actor in M.ACTORS:
        manifest["actors"][actor] = {"userId": "user-" + actor, "username": "ordinary-" + actor,
            "deviceId": "device-" + actor, "credentialRef": "credential-" + actor,
            "newOwnedOrdinaryAccount": True, "allowedFolderIds": ["view-LA", "view-LB"],
            "policyReceiptSha256": SHA}
    for series in ("A", "B"):
        library = "L" + series
        manifest["libraries"][library] = {"libraryId": "library-" + library, "viewId": "view-" + library,
            "sourceRootId": "root-" + library, "scopeItemId": "view-" + library, "policyFolderId": "view-" + library,
            "owned": True, "collectionType": "tvshows", "mediaRootReceiptSha256": SHA}
        manifest["items"][series] = {"id": "series-" + series, "type": "Series", "name": "Series " + series,
            "library": library, "parentId": "root-" + library}
        for season in (1, 2):
            symbol = series + "S" + str(season)
            manifest["items"][symbol] = {"id": "season-" + symbol, "type": "Season", "library": library,
                "parentId": "series-" + series, "seriesId": "series-" + series, "indexNumber": season}
        for index, (season, episode) in enumerate(((1, 1), (1, 2), (2, 1)), 1):
            symbol = series + str(index)
            manifest["items"][symbol] = {"id": "episode-" + symbol, "type": "Episode", "library": library,
                "parentId": "season-" + series + "S" + str(season), "seriesId": "series-" + series,
                "parentIndexNumber": season, "indexNumber": episode, "runtimeTicks": M.RUNTIME_TICKS,
                "frameRate": 30, "mediaSha256": SHA}
    return manifest


class ObservedServer:
    """A synthetic independent response source, with deliberate failure controls."""

    def __init__(self, matrix, positive=False, date_tie=False, null_dates=False):
        self.matrix, self.positive, self.date_tie = matrix, positive, date_tie
        self.states = {actor: {item: {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0,
                                    "IsFavorite": False, "Key": item} for item in M.EPISODES}
                       for actor in M.ACTORS}
        if null_dates:
            for actor in self.states.values():
                for value in actor.values():
                    value["LastPlayedDate"] = None
        self.initial_states = deepcopy(self.states)
        self.date_index = 0

    def response(self, step):
        manifest = self.matrix.manifest
        actor = manifest["actors"][step.actor]
        if step.kind == "login":
            return 200, {"AccessToken": "private-test-token-" + step.actor, "ServerId": manifest["serverId"],
                "User": {"Id": actor["userId"], "Name": actor["username"], "Policy": {"IsAdministrator": False}},
                "SessionInfo": {"Id": "session-" + step.actor, "UserId": actor["userId"], "DeviceId": actor["deviceId"]}}
        if step.kind == "profile":
            return 200, {"Id": actor["userId"], "Policy": {"IsAdministrator": False,
                "EnableAllFolders": False, "EnabledFolders": ["view-LA", "view-LB"]}, "Configuration": {"Order": ["tv"]}}
        if step.kind == "preferences":
            return 200, {"home": "tv"}
        if step.kind == "playback-info":
            return 200, {"PlaySessionId": "play-" + step.lifecycle,
                         "MediaSources": [{"Id": "source-" + step.item, "RunTimeTicks": M.RUNTIME_TICKS}]}
        if step.kind in ("started", "progress", "stopped", "cleanup-stop"):
            if step.kind in ("stopped", "cleanup-stop"):
                self.date_index += 1
                value = self.states[step.actor][step.item]
                position = (self.matrix.plays[step.lifecycle]["lastReportedPosition"]
                            if step.kind == "cleanup-stop" else step.position)
                value.update(Played=position == M.RUNTIME_TICKS,
                    PlayCount=value["PlayCount"] + 1,
                    PlaybackPositionTicks=0 if position == M.RUNTIME_TICKS else position,
                    LastPlayedDate="2026-09-12T01:00:" + f"{1 if self.date_tie else self.date_index:02d}" + "Z")
            return 204, None
        if step.kind in ("reset", "cleanup-reset"):
            self.states[step.actor][step.item] = deepcopy(self.initial_states[step.actor][step.item])
            return 200, deepcopy(self.states[step.actor][step.item])
        if step.kind == "logout":
            return 204, None
        if step.kind == "token-invalid":
            return 401, {"error": "invalid token"}
        if step.kind == "nextup":
            symbols = list(step.expected_ids or ())
            if step.expected_ids is None and self.positive and step.stage != "R0":
                symbols = ["B3", "A3"]
            return 200, {"Items": [{"Id": manifest["items"][symbol]["id"], "Type": "Episode",
                                    "UserData": deepcopy(self.states[step.actor][symbol])} for symbol in symbols],
                         "TotalRecordCount": len(symbols)}
        mapped = manifest["items"][step.item]
        body = {"Id": mapped["id"], "Type": mapped["type"], "ParentId": mapped["parentId"]}
        if step.item not in M.EPISODES:
            body["UserData"] = {"Played": False, "PlayCount": 0, "UnplayedItemCount": 3}
        else:
            body.update(SeriesId=mapped["seriesId"], ParentIndexNumber=mapped["parentIndexNumber"],
                        IndexNumber=mapped["indexNumber"], RunTimeTicks=M.RUNTIME_TICKS,
                        UserData=deepcopy(self.states[step.actor][step.item]))
        return 200, body


def authorize(matrix, request, elapsed):
    token_sha = matrix.sessions.get(request.actor, {}).get("tokenSha256")
    return matrix.authorize(request, SHA, elapsed, actor_token_sha256=token_sha)


def consume(matrix, server, elapsed=1000):
    request = matrix.prepare_next(elapsed)
    if request is None:
        return None
    if isinstance(request, M.WaitRequired):
        elapsed += request.seconds
        request = matrix.prepare_next(elapsed)
    step = matrix.queue[0]
    authorize(matrix, request, elapsed)
    status, body = server.response(step)
    matrix.accept(status, body, STAMP, SHA, elapsed)
    return request


def advance(matrix, server, predicate):
    start = matrix.last_elapsed + 10
    for index in range(300):
        if predicate(matrix):
            return
        if consume(matrix, server, start + index * 10) is None:
            break
    if not predicate(matrix):
        raise AssertionError("The synthetic guard did not reach its bounded target state.")


class MatrixGuards(unittest.TestCase):
    def matrix(self, positive=False, date_tie=False, null_dates=False):
        matrix = M.Matrix(fixture_manifest())
        return matrix, ObservedServer(matrix, positive, date_tie, null_dates)

    def test_01_exact_budget_counts_include_login_and_cleanup(self):
        matrix, _ = self.matrix()
        self.assertEqual(matrix.frozen_plan["requestCounts"], {"R0-R4": 116, "first-positive-extension": 14,
            "R5-R6-positive-only": 63, "cleanup-maximum": 49})
        self.assertEqual(matrix.frozen_plan["maximumNormalRequests"], 193)
        self.assertEqual(matrix.frozen_plan["maximumPlannedRequests"], 242)

    def test_02_mapping_rejects_reused_actor(self):
        manifest = fixture_manifest()
        manifest["actors"]["Q"]["userId"] = manifest["actors"]["P"]["userId"]
        with self.assertRaises(M.MatrixError):
            M.Matrix(manifest)

    def test_03_mapping_rejects_episode_order_inferred_from_ids(self):
        manifest = fixture_manifest()
        manifest["items"]["A2"]["parentIndexNumber"] = 2
        with self.assertRaises(M.MatrixError):
            M.Matrix(manifest)

    def test_04_mapping_requires_verified_cleanup_contract(self):
        manifest = fixture_manifest()
        manifest["cleanupContract"]["verifiedForBoundTarget"] = False
        with self.assertRaises(M.MatrixError):
            M.Matrix(manifest)

    def test_05_mapping_requires_current_fixture_release(self):
        manifest = fixture_manifest()
        manifest["binding"]["fixtureReleased"] = False
        with self.assertRaises(M.MatrixError):
            M.Matrix(manifest)

    def test_06_no_undeclared_flags_or_arbitrary_routes(self):
        matrix, _ = self.matrix()
        for rows in matrix.frozen_plan["branches"].values():
            for row in rows:
                request = row["wire"]
                self.assertTrue(request["route"].startswith("/emby/"))
                self.assertNotIn("EnableResumable", request["route"])
                self.assertNotIn("EnableRewatching", request["route"])
        expected = matrix.prepare_next(0)
        forged = M.Request(expected.label, "P", "POST", "https://foreign.invalid/", None, True, False)
        with self.assertRaises(M.MatrixError):
            matrix.authorize(forged, SHA, 0)

    def test_07_r0_baseline_precedes_first_mutation(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].kind == "playback-info")
        self.assertTrue(all(len(matrix.baseline[actor]) == 6 for actor in M.ACTORS))
        self.assertEqual(matrix.queue[0].stage, "R1")
        self.assertTrue(all(len(matrix.summaries[actor]) == 6 for actor in M.ACTORS))

    def test_08_exact_core_route_and_actor(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].label == "R0-Q-nextup-global")
        request = matrix.prepare_next(matrix.last_elapsed)
        self.assertEqual(parse_qs(urlsplit(request.route).query), {"UserId": ["user-Q"], "Limit": ["10"],
            "Fields": ["UserData,ParentId"], "EnableImages": ["false"]})

    def test_09_all_empty_stops_at_client_discovery(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.mode == "client-discovery-required")
        self.assertEqual(matrix.count, 116)
        self.assertIsNone(matrix.positive)
        self.assertFalse(matrix.r5_r6_started)
        self.assertIsNone(matrix.prepare_next(matrix.last_elapsed))
        self.assertEqual(matrix.client_discovery_plan()["maximumSeconds"], 1200)

    def test_10_positive_extension_precedes_next_mutation(self):
        matrix, server = self.matrix(positive=True)
        advance(matrix, server, lambda state: state.positive is not None)
        self.assertEqual(matrix.queue[0].stage, "EXT")
        self.assertTrue(all(step.kind == "nextup" for step in matrix.queue[:14]))
        self.assertEqual(matrix.positive["orderedSymbols"], ["B3", "A3"])

    def test_11_positive_does_not_assert_global_candidate_order(self):
        matrix, server = self.matrix(positive=True)
        advance(matrix, server, lambda state: state.mode == "api-observed")
        self.assertEqual(matrix.count, 193)
        self.assertTrue(matrix.r5_r6_started)
        self.assertEqual(matrix.failure, None)

    def test_12_positive_parent_ids_use_separate_view_mapping(self):
        matrix, server = self.matrix(positive=True)
        advance(matrix, server, lambda state: state.queue[0].variant == "parent-LA")
        request = matrix.prepare_next(matrix.last_elapsed)
        self.assertEqual(parse_qs(urlsplit(request.route).query)["ParentId"], ["view-LA"])
        self.assertNotIn("SeriesId", parse_qs(urlsplit(request.route).query))

    def test_13_combined_scope_is_not_parent_only(self):
        matrix, server = self.matrix(positive=True)
        advance(matrix, server, lambda state: state.queue[0].variant == "combined-A-AS2")
        params = parse_qs(urlsplit(matrix.prepare_next(matrix.last_elapsed).route).query)
        self.assertEqual(params["SeriesId"], ["series-A"])
        self.assertEqual(params["ParentId"], ["season-AS2"])

    def test_14_beyond_offset_uses_observed_count(self):
        matrix, server = self.matrix(positive=True)
        advance(matrix, server, lambda state: state.queue[0].variant == "beyond-count")
        params = parse_qs(urlsplit(matrix.prepare_next(matrix.last_elapsed).route).query)
        self.assertEqual(params["StartIndex"], ["3"])

    def test_15_partial_response_not_requested_position_establishes_state(self):
        matrix, server = self.matrix(positive=True)
        advance(matrix, server, lambda state: state.queue[0].stage == "R5" and state.queue[0].kind == "after-play")
        server.states["P"]["A2"]["PlaybackPositionTicks"] = 0
        with self.assertRaises(M.MatrixError):
            consume(matrix, server, matrix.last_elapsed + 10)
        self.assertEqual(matrix.mode, "recovery-required")

    def test_16_completed_report_requires_full_played_fact(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].kind == "after-play")
        server.states["P"]["A1"]["Played"] = False
        with self.assertRaises(M.MatrixError):
            consume(matrix, server, matrix.last_elapsed + 10)

    def test_17_r4_other_actor_drift_is_rejected(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].label == "R4-P-detail-A1")
        server.states["P"]["A1"]["PlayCount"] += 1
        with self.assertRaises(M.MatrixError):
            consume(matrix, server, matrix.last_elapsed + 10)

    def test_18_full_detail_field_presence_is_preserved(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].label == "R1-Q-detail-A1")
        server.states["Q"]["A1"]["LastPlayedDate"] = None
        with self.assertRaises(M.MatrixError):
            consume(matrix, server, matrix.last_elapsed + 10)

    def test_19_timestamp_tie_is_inconclusive_without_replay(self):
        matrix, server = self.matrix(date_tie=True)
        advance(matrix, server, lambda state: state.mode == "client-discovery-required")
        self.assertEqual(matrix.count, 116)
        self.assertTrue(all(row["status"] == "inconclusive-persisted-dates-not-increasing" for row in matrix.ranking))

    def test_20_real_time_wait_has_no_implicit_request(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].kind == "playback-info")
        matrix.last_stop_elapsed = matrix.last_elapsed
        observed = matrix.prepare_next(matrix.last_elapsed)
        self.assertIsInstance(observed, M.WaitRequired)
        self.assertEqual(observed.seconds, 2)

    def test_21_budget_refuses_normal_request_at_220(self):
        matrix, _ = self.matrix()
        matrix.count = 220
        with self.assertRaises(M.MatrixError):
            matrix.prepare_next(0)

    def test_22_attempt_requires_durable_intent_digest(self):
        matrix, _ = self.matrix()
        request = matrix.prepare_next(0)
        with self.assertRaises(M.MatrixError):
            matrix.authorize(request, "", 0)
        self.assertEqual(matrix.count, 0)

    def test_23_lost_response_blocks_replay_and_inferred_cleanup(self):
        matrix, _ = self.matrix()
        request = matrix.prepare_next(0)
        matrix.authorize(request, SHA, 0)
        matrix.lost_response("A login response was lost.")
        self.assertEqual(matrix.count, 1)
        with self.assertRaises(M.MatrixError):
            matrix.prepare_next(1)
        with self.assertRaises(M.MatrixError):
            matrix.begin_cleanup()

    def test_24_actor_token_swap_is_rejected(self):
        matrix, server = self.matrix()
        consume(matrix, server, 0)
        request = matrix.prepare_next(1)
        with self.assertRaises(M.MatrixError):
            matrix.authorize(request, SHA, 1, actor_token_sha256="b" * 64)

    def test_25_cleanup_restores_full_details_and_rejects_each_token(self):
        matrix, server = self.matrix(positive=True)
        advance(matrix, server, lambda state: state.mode == "api-observed")
        count = matrix.begin_cleanup()
        self.assertLessEqual(count, 80)
        start = matrix.last_elapsed
        for index in range(count):
            consume(matrix, server, start + 10 + index * 10)
        self.assertEqual(matrix.mode, "closed")
        self.assertEqual(matrix.revoked, set(M.ACTORS))
        self.assertEqual(matrix.cleanup_proofs, {(actor, item) for actor in M.ACTORS for item in M.EPISODES})
        self.assertEqual(matrix.current, matrix.baseline)

    def test_26_client_discovery_requires_closed_api_and_separate_evidence(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.mode == "client-discovery-required")
        receipt = {"manifestSha256": SHA, "responseReceiptSha256": SHA, "visibleStateReceiptSha256": SHA,
                   "playbackAttempts": 0, "elapsedSeconds": 20, "positiveGlobalResponse": False,
                   "actualClientRequest": True, "seriesIdOmitted": True, "injectedResponseOrManualReport": False}
        with self.assertRaises(M.MatrixError):
            matrix.record_client_discovery(receipt)
        count, start = matrix.begin_cleanup(), matrix.last_elapsed
        for index in range(count):
            consume(matrix, server, start + 10 + index * 10)
        matrix.record_client_discovery(receipt)
        self.assertEqual(matrix.outcome, "reference_global_positive_unresolved")
        self.assertFalse(matrix.r5_r6_started)

    def test_27_manifest_drift_cannot_change_an_authorized_route(self):
        matrix, _ = self.matrix()
        matrix.manifest["actors"]["P"]["userId"] = "foreign-user"
        with self.assertRaises(M.MatrixError):
            matrix.prepare_next(0)

    def test_28_queue_reordering_cannot_skip_frozen_requests(self):
        matrix, _ = self.matrix()
        matrix.queue[0], matrix.queue[1] = matrix.queue[1], matrix.queue[0]
        with self.assertRaises(M.MatrixError):
            matrix.prepare_next(0)

    def test_29_library_management_ids_must_be_distinct(self):
        manifest = fixture_manifest()
        manifest["libraries"]["LB"]["libraryId"] = manifest["libraries"]["LA"]["libraryId"]
        with self.assertRaises(M.MatrixError):
            M.Matrix(manifest)

    def test_31_cleanup_reconciles_a_new_stop_before_selecting_its_reset(self):
        matrix, server = self.matrix(null_dates=True)
        advance(matrix, server, lambda state: state.queue[0].kind == "progress")
        request = matrix.prepare_next(matrix.last_elapsed)
        authorize(matrix, request, matrix.last_elapsed)
        with self.assertRaises(M.MatrixError):
            matrix.accept(400, {"error": "synthetic rejected progress"}, STAMP, SHA, matrix.last_elapsed)
        self.assertEqual(matrix.current["P"]["A1"], matrix.baseline["P"]["A1"])
        matrix.begin_cleanup()
        self.assertTrue(any(row.kind == "cleanup-reset" and row.actor == "P" and row.item == "A1" for row in matrix.queue))
        requests = []
        while matrix.queue:
            requests.append(consume(matrix, server, matrix.last_elapsed + 10))
        self.assertEqual(matrix.mode, "closed-with-observation-failure")
        self.assertEqual(matrix.current, matrix.baseline)
        self.assertEqual(matrix.revoked, set(M.ACTORS))
        self.assertEqual(sum(row.method == "DELETE" for row in requests), 1)
        reconciled = next(row for row in matrix.facts if row["kind"] == "cleanup-reconcile")
        self.assertTrue(reconciled["ownedStopStateReconciled"])
        self.assertTrue(reconciled["cleanupResetRequired"])

    def test_32_cleanup_reconciles_an_acknowledged_stop_after_detail_failure(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].kind == "after-play")
        request = matrix.prepare_next(matrix.last_elapsed)
        authorize(matrix, request, matrix.last_elapsed)
        with self.assertRaises(M.MatrixError):
            matrix.accept(500, {"error": "synthetic detail failure"}, STAMP, SHA, matrix.last_elapsed)
        matrix.begin_cleanup()
        self.assertFalse(any(row.kind == "cleanup-stop" for row in matrix.queue))
        while matrix.queue:
            consume(matrix, server, matrix.last_elapsed + 10)
        self.assertEqual(matrix.current, matrix.baseline)
        self.assertEqual(matrix.mode, "closed-with-observation-failure")

    def test_33_cleanup_stop_does_not_authorize_unrelated_userdata_changes(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.queue[0].kind == "progress")
        request = matrix.prepare_next(matrix.last_elapsed)
        authorize(matrix, request, matrix.last_elapsed)
        with self.assertRaises(M.MatrixError):
            matrix.accept(400, None, STAMP, SHA, matrix.last_elapsed)
        matrix.begin_cleanup()
        consume(matrix, server, matrix.last_elapsed + 10)
        server.states["P"]["A1"]["IsFavorite"] = True
        with self.assertRaises(M.MatrixError):
            consume(matrix, server, matrix.last_elapsed + 10)
        self.assertFalse(any(row["kind"] == "cleanup-reset" for row in matrix.facts))
        with self.assertRaises(M.MatrixError):
            matrix.begin_cleanup()

    def test_30_summary_restore_compares_json_primitive_types(self):
        matrix, server = self.matrix()
        advance(matrix, server, lambda state: state.mode == "client-discovery-required")
        matrix.begin_cleanup()
        advance(matrix, server, lambda state: state.queue[0].kind == "cleanup-summary")
        request, step = matrix.prepare_next(matrix.last_elapsed), matrix.queue[0]
        authorize(matrix, request, matrix.last_elapsed)
        status, body = server.response(step)
        body["UserData"]["Played"] = 0
        with self.assertRaises(M.MatrixError):
            matrix.accept(status, body, STAMP, SHA, matrix.last_elapsed)
        with self.assertRaises(M.MatrixError):
            matrix.begin_cleanup()


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(MatrixGuards)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print(json.dumps({"suite": "nextup-global-matrix-guards", "classification": "synthetic planner guards; not reference evidence",
        "testsRun": result.testsRun, "failures": len(result.failures), "errors": len(result.errors),
        "skipped": len(result.skipped), "sourceSha256": hashlib.sha256(SOURCE.read_bytes()).hexdigest(),
        "guardSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "passed": result.wasSuccessful()}))
    raise SystemExit(0 if result.wasSuccessful() else 1)
