#!/usr/bin/env python3
"""Remote-only synthetic wire and actual journal persistence guards."""

from copy import deepcopy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs, urlsplit


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


STAMP = "2026-09-13T01:00:00+00:00"
SOURCE = TRANSPORT = PREPARATION = MATRIX = None


class Fixture:
    def __init__(self, root):
        self.support, self.preparation, self.planner = TRANSPORT, PREPARATION, MATRIX
        self.checks, self.closed = 0, False
        self.descriptors, self.closed_windows = [], []
        self.original_policy = {"IsAdministrator": False, "EnableAllFolders": False, "IsDisabled": False,
            "EnabledFolders": ["101", "103"], "AuthenticationProviderId": "owned-provider", "ExtraUntouched": {"value": [1, 2]}}
        self.grant_policy = deepcopy(self.original_policy)
        self.grant_policy["EnabledFolders"] = ["b953914417ce42b5a5e07a3e8f6fd386", "38fbd09cb8af4731993dd77e1750457f"]
        self.credentials = {actor: {"username": actor, "credentialRef": actor + "-credential", "password": actor * 40}
                            for actor in ("admin", "P")}
        actors = {actor: {"userId": actor, "username": actor, "credentialRef": actor + "-credential", "deviceId": "new-" + actor}
                  for actor in ("admin", "P")}
        users = ["admin", "P", "Q", "u1", "u2", "u3", "u4", "u5"]
        self.items, self.catalogs, self.details, libraries = {}, {}, {}, {}
        self.prior = {"server": {"id": "synthetic-server", "version": "1"}, "actors": actors,
            "libraries": {symbol: {"name": "Library " + symbol, "seriesName": "Series " + symbol} for symbol in ("LA", "LB")},
            "media": {"roots": {symbol: "/synthetic/media/" + symbol for symbol in ("LA", "LB")}}}
        self.userdata = {"Played": False, "PlayCount": 0, "PlaybackPositionTicks": 0, "IsFavorite": False, "Key": "retained-userdata"}
        for symbol, library_id, source_id, series_id in (("LA", "101", "102", "105"), ("LB", "103", "104", "111")):
            prefix, name = symbol[1], self.prior["libraries"][symbol]["seriesName"]
            root_path = self.prior["media"]["roots"][symbol]
            libraries[symbol] = {"libraryId": library_id, "viewId": library_id, "sourceRootId": source_id}
            native = {"Id": source_id, "Type": "Folder", "ParentId": "1", "Path": root_path}
            series = {"Id": series_id, "Type": "Series", "ParentId": source_id, "Name": name, "Path": root_path + "/" + name}
            self.items[prefix] = {"id": series_id, "type": "Series", "library": symbol, "name": name, "parentId": source_id}
            rows = [native, series]
            for season in (1, 2):
                season_id = str(int(series_id) + season)
                row = {"Id": season_id, "Type": "Season", "ParentId": series_id, "SeriesId": series_id, "IndexNumber": season}
                rows.append(row)
                self.items[prefix + "S" + str(season)] = {"id": season_id, "type": "Season", "library": symbol,
                    "parentId": series_id, "seriesId": series_id, "indexNumber": season}
            episode_ids = ("109", "108", "110") if symbol == "LA" else ("114", "115", "116")
            for index, (season, number) in enumerate(((1, 1), (1, 2), (2, 1))):
                item = prefix + str(index + 1)
                row = {"Id": episode_ids[index], "Type": "Episode", "ParentId": str(int(series_id) + season), "SeriesId": series_id,
                    "SeasonId": str(int(series_id) + season),
                    "IndexNumber": number, "ParentIndexNumber": season, "RunTimeTicks": 6000000000,
                    "Path": root_path + "/" + name + "/Season %02d/" % season + "%s S%02dE%02d.mp4" % (name, season, number),
                    "MediaSources": [{"RunTimeTicks": 6000000000, "MediaStreams": [{"Type": "Video", "AverageFrameRate": 30, "RealFrameRate": 30}]}]}
                rows.append(row)
                self.items[item] = {"id": row["Id"], "type": "Episode", "library": symbol, "parentId": row["ParentId"],
                    "seriesId": series_id, "parentIndexNumber": season, "indexNumber": number, "runtimeTicks": 6000000000,
                    "frameRate": 30, "mediaSha256": "a" * 64}
            for row in rows:
                row["UserData"] = deepcopy(self.userdata)
                self.details[row["Id"]] = deepcopy(row)
            self.catalogs[library_id] = rows
        for index in range(8):
            self.catalogs["old-lib-" + str(index)] = []
        self.library_dtos = [{"ItemId": key, "Name": "Library " + key, "Locations": ["/synthetic/" + key]} for key in self.catalogs]
        routes = [{"group": "old", "userId": "u1", "itemId": self.items[item]["id"]} for item in SOURCE.EPISODES]
        routes += [{"group": "grant-P", "userId": "P", "itemId": self.items[item]["id"]} for item in SOURCE.EPISODES]
        self.value = {"runId": "synthetic-grant", "actors": actors, "readonlyRoots": [], "outputRoot": str(root / "run"),
            "endpoint": {"scheme": "http", "host": "127.0.0.1", "port": 18197},
            "publicBaseline": {"sha256": "b" * 64}, "folderAuthority": {"devicesResponse": {"sha256": "c" * 64}},
            "preservation": {"userIds": users, "libraryIds": list(self.catalogs), "detailRoutes": routes}}
        self.prior_state = {"items": self.items, "libraries": libraries}
        self.old_devices = {"old-device-" + str(index): {"Id": "old-device-" + str(index), "ReportedDeviceId": "reported-" + str(index),
            "DateLastActivity": "2026-09-12T01:00:00+00:00", "LastUserId": "u1"} for index in range(91)}
        self.baseline = {"marker": "synthetic-public", "roster": {}, "devices": deepcopy(self.old_devices),
            "libraries": {row["ItemId"]: deepcopy(row) for row in self.library_dtos}}
        self.folder_records = {"virtualFoldersResponse": (None, {"Items": deepcopy(self.library_dtos), "TotalRecordCount": 10})}
        self.raw = SOURCE.canonical(self.value).encode()

    def acquire(self):
        self.check()

    def check(self):
        self.checks += 1

    def close(self):
        self.closed = True


class Wire:
    def __init__(self, fixture, overrides=None):
        self.fixture, self.overrides = fixture, overrides or {}
        self.requests, self.tokens, self.revoked = [], {}, set()
        self.policy = deepcopy(fixture.original_policy)

    def response(self, status, body, *, complete=True, raw=None, headers=None):
        raw = (SOURCE.canonical(body).encode() if body is not None else b"") if raw is None else raw
        return TRANSPORT.WireResponse(status, headers or [["Content-Type", "application/json"], ["Content-Length", str(len(raw))]],
            raw, complete, STAMP, None)

    def profile(self, user):
        policy = self.policy if user == "P" else {"IsAdministrator": user == "admin"}
        date = STAMP if user in self.tokens else "2026-09-12T01:00:00+00:00"
        return {"Id": user, "Name": user, "Policy": deepcopy(policy), "Configuration": {}, "LastLoginDate": date, "LastActivityDate": date}

    def send(self, request, headers, payload, **budgets):
        self.requests.append({"label": request.label, "actor": request.actor, "method": request.method, "route": request.route,
            "headers": deepcopy(headers), "payload": payload})
        if request.label in self.overrides:
            result = self.overrides[request.label]
            if isinstance(result, BaseException):
                raise result
            return result(self, request) if callable(result) else result
        route, actor = request.route, request.actor
        if request.label == "login-" + actor:
            token = "synthetic-secret-token-" + actor
            self.tokens[actor] = token
            return self.response(200, {"ServerId": "synthetic-server", "AccessToken": token, "User": self.profile(actor),
                "SessionInfo": {"Id": "session-" + actor, "UserId": actor, "DeviceId": "new-" + actor}})
        if request.method == "POST" and route.endswith("/Policy"):
            self.policy = SOURCE.strict_json(payload)
            return self.response(204, None)
        if request.label.startswith("logout-"):
            self.revoked.add(actor)
            return self.response(204, None)
        if request.label.startswith("rejection-"):
            return self.response(401, None)
        if route == "/emby/Library/SelectableMediaFolders":
            return self.response(403, {"Error": "Synthetic ordinary-account observation"})
        if route == "/emby/System/Info/Public":
            return self.response(200, {"Id": "synthetic-server", "Version": "1"})
        if route == "/emby/Users":
            return self.response(200, [self.profile(user) for user in self.fixture.value["preservation"]["userIds"]])
        if route == "/emby/Library/VirtualFolders/Query":
            return self.response(200, {"Items": self.fixture.library_dtos, "TotalRecordCount": 10})
        if route == "/emby/Devices":
            devices = list(deepcopy(self.fixture.old_devices).values())
            for user in self.tokens:
                devices.append({"Id": "device-" + user, "ReportedDeviceId": "new-" + user, "LastUserId": user, "LastUserName": user,
                    "AppName": SOURCE.CLIENT, "Name": SOURCE.DEVICE, "AppVersion": SOURCE.VERSION, "DateLastActivity": STAMP})
            return self.response(200, {"Items": devices, "TotalRecordCount": 0})
        if route == "/emby/System/Configuration" or route.startswith("/emby/UserSettings/"):
            return self.response(200, {})
        if route == "/emby/Users/P":
            return self.response(200, self.profile("P"))
        if route.endswith("/Views"):
            rows = [{"Id": key} for key in ("101", "103")] if self.policy == self.fixture.grant_policy else []
            return self.response(200, {"Items": rows, "TotalRecordCount": len(rows)})
        split = urlsplit(route)
        if "/Items?" in route:
            query = parse_qs(split.query)
            rows = self.fixture.catalogs[query["ParentId"][0]] if "ParentId" in query else []
            return self.response(200, {"Items": rows, "TotalRecordCount": len(rows)})
        if "/Items/" in route:
            return self.response(200, self.fixture.details[route.rsplit("/", 1)[1]])
        raise AssertionError("The synthetic wire received an unplanned route.")


class Guards(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="nextup-grant-guard-")
        self.addCleanup(self.temporary.cleanup)
        self.fixture = Fixture(Path(self.temporary.name))
        self.wire = Wire(self.fixture)
        self.runner = SOURCE.Runner(self.fixture, wire=self.wire, monotonic=lambda: 0.0, now=lambda: STAMP)
        self.socket = patch.object(socket, "socket", side_effect=AssertionError("No real socket is allowed."))
        self.socket.start()
        self.addCleanup(self.socket.stop)
        self.connection = patch.object(socket, "create_connection", side_effect=AssertionError("No real network is allowed."))
        self.connection.start()
        self.addCleanup(self.connection.stop)

    def labels(self):
        return [row["label"] for row in self.wire.requests]

    def state(self):
        return SOURCE.strict_json((Path(self.fixture.value["outputRoot"]) / "private/state.json").read_bytes())

    def test_complete_flow_is_59_plus_50_and_restores_the_whole_original_policy(self):
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "awaiting_independent_attestation")
        self.assertEqual((terminal["normalRequestCount"], terminal["cleanupRequestCount"], terminal["requestCount"]), (59, 50, 109))
        writes = [row for row in self.wire.requests if row["route"].endswith("/Policy")]
        self.assertEqual([SOURCE.strict_json(row["payload"]) for row in writes], [self.fixture.grant_policy, self.fixture.original_policy])
        self.assertEqual(self.wire.policy, self.fixture.original_policy)
        self.assertEqual(len([label for label in self.labels() if label.startswith("before-")]), 43)
        self.assertEqual(len([label for label in self.labels() if label.startswith("after-")]), 43)
        self.assertEqual(self.runner.events["own-P-selectable-folders"]["status"], 403)
        self.assertEqual(self.runner.revoked, {"P", "admin"})

    def test_each_logout_and_rejection_use_the_same_owned_token(self):
        self.runner.run()
        for actor in ("admin", "P"):
            selected = [row for row in self.wire.requests if row["label"] in ("logout-" + actor, "rejection-" + actor)]
            self.assertEqual([row["headers"]["X-Emby-Token"] for row in selected], [self.runner.tokens[actor]] * 2)

    def test_grant_timeout_retains_pending_and_never_restores_or_retries(self):
        self.wire.overrides["grant-policy"] = TimeoutError("Synthetic lost response.")
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(self.labels()[-1], "grant-policy")
        self.assertEqual(self.state()["pending"]["label"], "grant-policy")
        self.assertTrue(self.state()["uncertain"])

    def test_complete_unacknowledged_policy_status_never_authorizes_restore(self):
        self.wire.overrides["grant-policy"] = self.wire.response(500, {"error": "synthetic"})
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertNotIn("restore-policy", self.labels())
        self.assertIn("responseReceiptSha256", self.state()["pending"])

    def test_grant_readback_failure_keeps_acknowledged_write_unresolved(self):
        self.wire.overrides["granted-P-policy"] = self.wire.response(200, {"Id": "P", "Name": "P", "Policy": {}})
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(self.state()["policyState"], "grant-acknowledged")
        self.assertEqual(self.labels()[-1], "granted-P-policy")
        self.assertNotIn("restore-policy", self.labels())

    def test_incomplete_grant_response_never_authorizes_restore(self):
        self.wire.overrides["grant-policy"] = self.wire.response(204, None, complete=False)
        self.runner.run()
        self.assertEqual(self.labels()[-1], "grant-policy")
        self.assertTrue(self.state()["uncertain"])

    def test_declared_json_duplicates_keep_the_login_pending(self):
        self.wire.overrides["login-admin"] = self.wire.response(200, None, raw=b'{"AccessToken":"x","AccessToken":"y"}')
        self.runner.run()
        self.assertEqual(self.labels(), ["login-admin"])
        self.assertEqual(self.state()["pending"]["label"], "login-admin")

    def test_wrong_login_actor_does_not_register_or_close_an_unknown_token(self):
        self.wire.overrides["login-P"] = self.wire.response(200, {"ServerId": "synthetic-server", "AccessToken": "unknown-token",
            "User": {"Id": "Q", "Name": "Q", "Policy": {"IsAdministrator": False}},
            "SessionInfo": {"Id": "wrong-session", "UserId": "Q", "DeviceId": "new-P"}})
        self.runner.run()
        self.assertNotIn("P", self.runner.tokens)
        self.assertNotIn("logout-P", self.labels())
        self.assertNotIn("restore-policy", self.labels())

    def test_negative_known_views_still_restore_and_close_owned_tokens(self):
        self.wire.overrides["own-P-views"] = self.wire.response(200, {"Items": [], "TotalRecordCount": 0})
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertIn("restore-policy", self.labels())
        self.assertNotIn("own-P-selectable-folders", self.labels())
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_restore_timeout_keeps_pending_and_stops_all_cleanup_http(self):
        self.wire.overrides["restore-policy"] = TimeoutError("Synthetic unknown restore.")
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(self.labels()[-1], "restore-policy")
        self.assertEqual(self.state()["pending"]["label"], "restore-policy")

    def test_restore_readback_failure_does_not_claim_restoration_or_close_tokens(self):
        self.wire.overrides["restored-P-policy"] = self.wire.response(200, {"Id": "P", "Name": "P", "Policy": {}})
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertEqual(self.state()["policyState"], "restore-acknowledged")
        self.assertFalse(terminal["originalPolicyRestored"])
        self.assertNotIn("logout-P", self.labels())

    def test_known_restore_view_failure_still_closes_known_tokens(self):
        self.wire.overrides["restored-own-P-views"] = self.wire.response(200, {"Items": [{"Id": "101"}], "TotalRecordCount": 1})
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertTrue(terminal["allKnownTokensClosed"])
        self.assertTrue(terminal["originalPolicyRestored"])

    def test_non_200_empty_restore_views_do_not_count_as_verified_empty_views(self):
        self.wire.overrides["restored-own-P-views"] = self.wire.response(403, {"Items": [], "TotalRecordCount": 0})
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertIsNotNone(terminal["failure"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_non_200_snapshot_body_is_not_accepted_as_a_public_document(self):
        self.wire.overrides["before-configuration"] = self.wire.response(403, {})
        terminal = self.runner.run()
        self.assertNotIn("grant-policy", self.labels())
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_fresh_library_dto_drift_blocks_the_policy_write(self):
        changed = deepcopy(self.fixture.library_dtos)
        changed[0]["Locations"] = ["/synthetic/another-root"]
        self.wire.overrides["before-libraries"] = self.wire.response(200, {"Items": changed, "TotalRecordCount": 10})
        terminal = self.runner.run()
        self.assertNotIn("grant-policy", self.labels())
        self.assertFalse(terminal["grantSemanticsProven"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_wrong_before_witness_season_blocks_the_grant(self):
        altered = deepcopy(self.fixture.details["109"])
        altered["SeasonId"] = "another-season"
        self.wire.overrides["before-detail-6"] = self.wire.response(200, altered)
        terminal = self.runner.run()
        self.assertNotIn("grant-policy", self.labels())
        self.assertFalse(terminal["grantSemanticsProven"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_wrong_own_token_season_fails_proof_then_restores(self):
        altered = deepcopy(self.fixture.details["109"])
        altered["SeasonId"] = "another-season"
        self.wire.overrides["own-P-detail-A1"] = self.wire.response(200, altered)
        terminal = self.runner.run()
        self.assertFalse(terminal["grantSemanticsProven"])
        self.assertTrue(terminal["originalPolicyRestored"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_false_zero_primitive_type_cannot_pass_own_token_details(self):
        altered = deepcopy(self.fixture.details["109"])
        altered["UserData"]["PlayCount"] = False
        self.wire.overrides["own-P-detail-A1"] = self.wire.response(200, altered)
        terminal = self.runner.run()
        self.assertFalse(terminal["grantSemanticsProven"])
        self.assertTrue(terminal["originalPolicyRestored"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_extra_catalog_episode_cannot_match_the_frozen_twelve_items(self):
        rows = deepcopy(self.fixture.catalogs["101"])
        extra = deepcopy(self.fixture.details["109"])
        extra["Id"] = "foreign-episode"
        rows.append(extra)
        self.wire.overrides["own-P-catalog-LA"] = self.wire.response(200, {"Items": rows, "TotalRecordCount": len(rows)})
        terminal = self.runner.run()
        self.assertFalse(terminal["grantSemanticsProven"])
        self.assertTrue(terminal["originalPolicyRestored"])
        self.assertTrue(terminal["allKnownTokensClosed"])

    def test_failed_after_snapshot_semantics_still_close_known_tokens(self):
        self.wire.overrides["after-configuration"] = self.wire.response(200, {"changed": True})
        terminal = self.runner.run()
        self.assertFalse(terminal["publicPreserved"])
        self.assertTrue(terminal["allKnownTokensClosed"])
        self.assertEqual(terminal["status"], "stopped_with_known_cleanup")

    def test_missing_same_token_401_stops_the_remaining_cleanup(self):
        self.wire.overrides["rejection-P"] = self.wire.response(200, [])
        terminal = self.runner.run()
        self.assertEqual(terminal["status"], "recovery_required")
        self.assertFalse(terminal["allKnownTokensClosed"])
        self.assertNotIn("logout-admin", self.labels())

    def test_pending_commit_failure_sends_no_http(self):
        original = TRANSPORT.Journal.state
        def state(journal, value):
            if value["pending"] is not None:
                raise OSError("Synthetic pending commit failure.")
            return original(journal, value)
        with patch.object(TRANSPORT.Journal, "state", state):
            terminal = self.runner.run()
        self.assertIsNone(terminal)
        self.assertEqual(self.labels(), [])

    def test_raw_response_persistence_failure_cannot_start_cleanup(self):
        original = TRANSPORT.Journal.save
        def save(journal, name, value, *, export=False):
            if name.endswith("grant-policy-response.json") and not export:
                raise OSError("Synthetic raw response persistence failure.")
            return original(journal, name, value, export=export)
        with patch.object(TRANSPORT.Journal, "save", save):
            terminal = self.runner.run()
        self.assertIsNone(terminal)
        self.assertEqual(self.labels()[-1], "grant-policy")
        self.assertEqual(self.state()["pending"]["label"], "grant-policy")

    def test_policy_ack_commit_failure_cannot_start_cleanup(self):
        original = TRANSPORT.Journal.state
        def state(journal, value):
            if value["policyState"] == "grant-acknowledged":
                raise OSError("Synthetic acknowledged policy commit failure.")
            return original(journal, value)
        with patch.object(TRANSPORT.Journal, "state", state):
            terminal = self.runner.run()
        self.assertIsNone(terminal)
        self.assertEqual(self.labels()[-1], "grant-policy")
        self.assertEqual(self.state()["pending"]["label"], "grant-policy")

    def test_export_contains_no_raw_token_or_password(self):
        self.runner.run()
        root = Path(self.fixture.value["outputRoot"]) / "export"
        for path in root.iterdir():
            raw = path.read_text()
            for secret in list(self.runner.tokens.values()) + [row["password"] for row in self.fixture.credentials.values()]:
                self.assertNotIn(secret, raw)

    def test_runner_and_scope_cannot_resume(self):
        self.runner.run()
        with self.assertRaises(ValueError):
            self.runner.run()
        another = SOURCE.Runner(self.fixture, wire=self.wire, monotonic=lambda: 0.0, now=lambda: STAMP)
        with self.assertRaises(FileExistsError):
            another.run()

    def test_foreign_policy_field_cannot_be_written(self):
        self.runner.started = 0.0
        self.runner.tokens["admin"] = "synthetic-token"
        self.runner.journal = TRANSPORT.Journal(self.fixture.value["outputRoot"], uid=0)
        self.addCleanup(self.runner.journal.close)
        foreign = deepcopy(self.fixture.grant_policy)
        foreign["EnableAllFolders"] = True
        with self.assertRaises(ValueError):
            self.runner._dispatch("grant-policy", "admin", "POST", "/emby/Users/P/Policy", foreign)
        self.assertEqual(self.labels(), [])


def main():
    global SOURCE, TRANSPORT, PREPARATION, MATRIX
    if not (sys.platform == "linux" and os.geteuid() == 0 and sys.flags.isolated and sys.flags.dont_write_bytecode and os.environ.get("SSH_CONNECTION")):
        raise SystemExit("Only the remote Python -I -B environment is supported.")
    os.umask(0o077)
    import argparse
    parser = argparse.ArgumentParser()
    for key in ("source", "transport", "preparation", "matrix", "report"):
        parser.add_argument("--" + key, type=Path, required=True)
    args = parser.parse_args()
    SOURCE = load(args.source, "grant_worker_guard_subject")
    TRANSPORT = load(args.transport, "grant_worker_guard_transport")
    PREPARATION = load(args.preparation, "grant_worker_guard_preparation")
    MATRIX = load(args.matrix, "grant_worker_guard_matrix")
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Guards))
    report = {"kind": "nextup-folder-grant-synthetic-guards", "passed": result.wasSuccessful(), "testsRun": result.testsRun,
        "failures": len(result.failures), "errors": len(result.errors), "skips": len(result.skipped),
        "actualBusinessHttpRequests": 0, "actualProcessProbes": 0,
        "sourceSha256": {key: hashlib.sha256(getattr(args, key).read_bytes()).hexdigest() for key in ("source", "transport", "preparation", "matrix")},
        "guardSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    with args.report.open("x", encoding="utf-8") as handle:
        json.dump(report, handle, sort_keys=True, indent=2)
        handle.write("\n")
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
