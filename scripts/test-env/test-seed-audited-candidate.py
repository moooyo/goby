#!/usr/bin/env python3
"""Pure seed guards; no files, sockets, databases, or candidate processes are used."""
import copy
import importlib.util
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

SPEC = importlib.util.spec_from_file_location("candidate_seed", Path(__file__).with_name("seed-audited-candidate.py"))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


FRESH_ROOT = Path("/opt/goby-audited-candidate-20260914T010203Z-0123456789ab")


def catalog_fixture(root=FRESH_ROOT):
    """Build the complete observed public catalog without accessing resources."""
    identities = {name: "%032x" % number for number, name in enumerate(
        ("movie", "series", "season-1", "season-2", "episode-1-1", "episode-1-2", "episode-2-1", "album", "mp3", "flac"), 1)}
    libraries = {directory: {"Id": "%032x" % number, "Name": name, "CollectionType": collection,
                              "Paths": [str(root / "data/media" / directory)]}
                 for number, (directory, name, collection) in enumerate(M.LIBRARIES, 101)}

    def item(key, kind, name, parent, relative=None, **fields):
        return {"Id": identities[key], "Type": kind, "Name": name, "ParentId": parent,
                "ServerId": "f" * 32, "IsFolder": kind in ("Series", "Season", "MusicAlbum"),
                **({"Path": str(root / "data/media" / relative)} if relative is not None else {}), **fields}

    def playable(key, kind, name, parent, relative, ticks, container, streams, **fields):
        row = item(key, kind, name, parent, relative, RunTimeTicks=ticks, MediaStreams=streams,
                   UserData={"Played": False, "PlaybackPositionTicks": 0, "PlayCount": 0}, **fields)
        row["MediaSources"] = [{"Id": identities[key], "ItemId": identities[key], "Path": row["Path"],
                                "RunTimeTicks": ticks, "Container": container,
                                "SupportsDirectPlay": True, "SupportsDirectStream": True}]
        return row

    movie_streams = [{"Type": "Video", "Codec": "h264", "Index": 0}, {"Type": "Audio", "Codec": "aac", "Index": 1}]
    movie_streams += [{"Type": "Subtitle", "Codec": codec, "Index": index, "IsExternal": True, "Language": language,
                       "Path": str(root / "data/media/Movies" / ("M3e Client Movie.en." + codec))}
                      for codec, index, language in (("srt", 2, "en"), ("vtt", 3, "eng"))]
    details = [playable("movie", "Movie", "M3e Client Movie", libraries["Movies"]["Id"],
                        "Movies/M3e Client Movie.mp4", 6000000000, "mp4", movie_streams),
               item("series", "Series", "M3e Client Series", libraries["TV"]["Id"], "TV/M3e Client Series")]
    for season in (1, 2):
        details.append(item("season-%d" % season, "Season", "Season %02d" % season, identities["series"],
                            "TV/M3e Client Series/Season %02d" % season, IndexNumber=season,
                            SeriesId=identities["series"], SeriesName="M3e Client Series"))
    for season, episode in ((1, 1), (1, 2), (2, 1)):
        details.append(playable("episode-%d-%d" % (season, episode), "Episode", "Episode %d-%d" % (season, episode),
                                identities["season-%d" % season],
                                "TV/M3e Client Series/Season %02d/M3e Client Series S%02dE%02d.mp4" % (season, season, episode),
                                1200000000, "mp4", [{"Type": "Video", "Codec": "h264", "Index": 0},
                                                    {"Type": "Audio", "Codec": "aac", "Index": 1}],
                                ParentIndexNumber=season, IndexNumber=episode, SeriesId=identities["series"],
                                SeriesName="M3e Client Series", SeasonId=identities["season-%d" % season],
                                SeasonName="Season %02d" % season))
    details.append(item("album", "MusicAlbum", "M3e Synthetic Album", libraries["Music"]["Id"]))
    for codec in ("mp3", "flac"):
        details.append(playable(codec, "Audio", "M3e " + codec.upper(), identities["album"],
                                "Music/M3e Client Audio." + codec, 1800000000, codec,
                                [{"Type": "Audio", "Codec": codec, "Index": 0}]))
    return details, libraries


def source_state_fixture():
    """Pair the public fixture with exact stored rows and owned cleanup evidence."""
    details, libraries = catalog_fixture()
    roles = ("admin", *M.SCENARIOS, "control-q")
    actors = {role: {"id": "%032x" % number, "username": "Guard " + role}
              for number, role in enumerate(roles, 201)}
    resources = {"users": [actor["id"] for actor in actors.values()],
                 "libraries": [library["Id"] for library in libraries.values()],
                 "jobs": [{"id": "%032x" % number, "libraryId": library["Id"], "status": "completed"}
                          for number, library in enumerate(libraries.values(), 301)]}
    cleanup = [{"kind": kind, "tokenSha256": fingerprint, "logoutAcknowledged": True, "sameTokenRejected": True}
               for kind, fingerprint in (("native", "a" * 64), ("emby", "b" * 64))]
    snapshot = {"schema": 28, "historyRows": 0,
        "users": [{"id": actor["id"], "name": actor["username"], "admin": role == "admin", "disabled": False,
                   "policy": copy.deepcopy(M.STORED_Q_POLICY) if role == "control-q" else {}}
                  for role, actor in actors.items()],
        "libraries": [{"id": row["Id"], "name": row["Name"], "collectionType": row["CollectionType"]}
                      for row in libraries.values()],
        "roots": [{"id": "%032x" % number, "libraryId": row["Id"], "path": row["Paths"][0]}
                  for number, row in enumerate(libraries.values(), 401)],
        "jobs": [{"id": row["id"], "libraryId": row["libraryId"], "status": "Completed", "error": "", "forceProbe": False}
                 for row in resources["jobs"]],
        "items": [{"id": row["Id"], "name": row["Name"], "type": row["Type"], "parentId": row["ParentId"], "path": row.get("Path", "")}
                  for row in details] + [{"id": row["Id"], "name": row["Name"], "type": "CollectionFolder", "parentId": None, "path": ""}
                                         for row in libraries.values()],
        "sessions": [{"userId": actors["admin"]["id"], "kind": kind, "tokenSha256": fingerprint, "revoked": True}
                     for kind, fingerprint in (("admin", "a" * 64), ("emby", "b" * 64))]}
    catalog = M.map_catalog(details, libraries, root=FRESH_ROOT, observed_tv=True)
    return [snapshot, details, libraries, actors, resources, cleanup, catalog]


def inspection_prior_pins():
    """Return the fixed failed inspections and SQL diagnostic without reading them."""
    return {
        "priorMetadataFailure": {"path": str(M.E3 / "initial-runtime-inspection-01/report.json"),
                                 "sha256": "7622127b9f288bc274566f1f05af80883120258dc55641d5d71618a40a26605d"},
        "priorSqlFailure": {"path": str(M.E3 / "initial-runtime-inspection-02/report.json"),
                            "sha256": "1aaad33511366fd3b9eb54923460314f0bdd63c4160deb6fa3710ba4816f83db"},
        "sqlFailureDiagnostic": {"path": str(M.E3 / "private/first-sql-diagnostic/report.json"),
                                 "sha256": "65370c3558444a5e7503e8709ea30ceee4e1cf11c61a468699cc6d2ac882c47d"},
    }


def authority_fixture():
    """Build independent pinned documents for a fresh embedded candidate."""
    def pin(path, digit):
        return {"path": str(path), "sha256": digit * 64}

    run_id = FRESH_ROOT.name.removeprefix("goby-audited-candidate-")
    suffix = run_id.split("-")[-1]
    value = {"kind": "audited-candidate-seed-input", "version": 2, "runId": "embedded-seed-guard",
             "output": str(M.E3 / "seed-guard"), "candidateManifest": pin(FRESH_ROOT / "private/manifest.json", "a"),
             "runtimeInspection": pin(M.INITIAL_INSPECTION / "report.json", "b"),
             "runtimeHelper": pin(M.E3 / "prepare-audited-candidate.py", "c"),
             "inspectionHelper": pin(M.INSPECTION_HELPER, "d"),
             "provisionInput": pin(M.E3 / "provision-input.json", "e"),
             "mediaManifest": pin(M.E3 / "private/client-media-manifest.json", "f"), "budgets": copy.deepcopy(M.BUDGETS)}
    candidate = {"runId": run_id, "provisionVersion": 2, "status": "running_awaiting_live_acceptance",
                 "dataDirectory": str(FRESH_ROOT / "data"), "bootstrapExecuted": False, "recoveryRestoreExecuted": False,
                 "candidateAdmissionComplete": False, "clientAcceptance": False,
                 "binary": {"path": str(FRESH_ROOT / "install/goby"), "sha256": M.EMBEDDED_BINARY_SHA},
                 "runtime": pin(FRESH_ROOT / "private/runtime.env", "1"), "input": copy.deepcopy(value["provisionInput"]),
                 "productEvidence": copy.deepcopy(M.PRODUCT_EVIDENCE),
                 "ordinaryRegressionStatus": "passed_with_explicit_profile_gap", "ordinaryRegressionPhases": 2,
                 "taggedFullRegressionClaimed": False, "sourceState": {"users": 0, "schema": 28, "migrations": 28},
                 "dashboard": {"mode": "embedded", "buildManifest": copy.deepcopy(M.PRODUCT_EVIDENCE["embeddedBuildManifest"]),
                               "assetCount": 57, "externalDirectoryInstalled": False, "webDirectoryOverridePresent": False},
                 "inaccessiblePaths": list(M.FRESH_INACCESSIBLE),
                 "loadedInaccessiblePaths": {role: sorted(M.FRESH_INACCESSIBLE) for role in ("server", "postgres")},
                 "database": "goby_candidate_" + suffix, "recoveryDatabase": "goby_recovery_" + suffix,
                 "serverIdentity": {"uid": 55242, "pid": 7001, "startTicks": 100001, "bootId": "guard-boot",
                                    "exe": str(FRESH_ROOT / "install/goby"), "executableDevice": 17, "executableInode": 101},
                 "postgresIdentity": {"uid": 55242, "pid": 7002, "startTicks": 100002, "bootId": "guard-boot",
                                      "exe": "/usr/lib/postgresql/17/bin/postgres", "executableDevice": 17, "executableInode": 102},
                 "ports": {"http": 28518, "postgres": 28517}, "publicUrl": "http://127.0.0.1:28516",
                 "directUrl": "http://127.0.0.1:28518", "listener": {"socketInode": 9001}}
    provision_input = {"kind": "audited-candidate-provision-input", "version": 2, "runId": run_id,
                       "ports": copy.deepcopy(candidate["ports"]), "public_url": candidate["publicUrl"],
                       "dashboardProfile": "embedded-administrator-v1", **copy.deepcopy(M.PRODUCT_EVIDENCE)}
    inspection_input = {"kind": "audited-candidate-inspection-input", "version": 3,
                        "output": str(M.INITIAL_INSPECTION), "provisionManifest": copy.deepcopy(value["candidateManifest"]),
                        "provisionInput": copy.deepcopy(value["provisionInput"]), "provisionHelper": copy.deepcopy(value["runtimeHelper"]),
                        "sourceArchive": copy.deepcopy(M.PRODUCT_EVIDENCE["sourceArchive"]),
                        "sourceManifest": copy.deepcopy(M.PRODUCT_EVIDENCE["sourceManifest"]),
                        "budgets": {"maximumSeconds": 180, "maximumSqlSessions": 16,
                                    "maximumHttpRequests": 64, "maximumHttpBodyBytes": 4194304},
                        **inspection_prior_pins()}
    attestation = {"kind": "audited-candidate-runtime-inspection", "version": 3, "status": "ready_pending_seed",
                   "stage": "complete", "failure": None, "readProcessesClosed": True,
                   "input": pin(M.E3 / "inspection-input.json", "2"), "helper": copy.deepcopy(value["inspectionHelper"]),
                   "provisionManifest": copy.deepcopy(value["candidateManifest"]), "provisionInput": copy.deepcopy(value["provisionInput"]),
                   "provisionHelper": copy.deepcopy(value["runtimeHelper"]), "sourceEvidence": copy.deepcopy(candidate["productEvidence"]),
                   "binary": copy.deepcopy(candidate["binary"]), "databaseWrites": 0,
                   "bootstrapPerformed": False, "restorePerformed": False, "serviceChangesPerformed": False,
                   "candidateAdmissionComplete": False, "clientAcceptance": False,
                   "database": {"sourceSchemaVersion": 28, "sourceMigrationCount": 28, "sourceUsers": 0,
                                "recoveryTargetEmpty": True, "allSqlTransactionsReadOnly": True},
                   "processes": {role: copy.deepcopy(candidate[role + "Identity"]) for role in ("server", "postgres")},
                   "httpListener": copy.deepcopy(candidate["listener"]),
                   "dashboard": {"assetCount": 57, "allAssetsMatched": True, "entryReferencesMatched": 5},
                   **inspection_prior_pins(),
                   "metadataCorrectionReview": {"path": str(M.E3 / "empty-environment-files-review.json"),
                                                "sha256": "1b518abf9491027366936e7b88e4738d22c0a609d87c029e2f5d54b11c8c9ce8"},
                   "sqlCorrection": {"priorFailurePreserved": True, "priorSqlResultRecovered": False,
                                     "diagnosticReadProcessesClosed": True,
                                     "passfilePolicy": "owned_socket_path_required_absent", "stderrPolicy": "empty_required"}}
    return [value, candidate, attestation, inspection_input, provision_input]


class SeedGuards(unittest.TestCase):
    def value(self):
        return {"runId": "audited-seed-test", "output": str(M.R / "seed-pure-guard"), "candidateManifest": M.MANIFEST,
                "runtimeInspection": M.INSPECTION, "runtimeHelper": {}, "budgets": copy.deepcopy(M.BUDGETS)}

    def io(self):
        io = M.CandidateIO(self.value(), {}, {})
        io.pin = Mock()
        io.candidate = {"publicUrl": "http://127.0.0.1:28496"}
        io.port = 28498
        return io

    def test_existing_output_rejected_before_new_receipt_or_candidate_access(self):
        io = self.io()
        with patch.object(M.os.path, "lexists", return_value=True), patch.object(M, "descriptor") as read, \
             patch.object(M, "write_json_once") as write, patch.object(M.os, "mkdir") as mkdir:
            with self.assertRaises(ValueError):
                io.open()
            read.assert_not_called()
            write.assert_not_called()
            mkdir.assert_not_called()

    def test_private_failure_text_is_bounded_and_cannot_enter_the_public_summary(self):
        public = {"status": "seed_failed_resources_retained", "stage": "source_state", "errorType": "RuntimeError",
                  "receipt": {"path": "/private/failure.json", "sha256": "a" * 64}, "candidateAdmissionComplete": False}
        failure = {**public, "resources": {"users": ["synthetic-owned-user"]}}
        before = copy.deepcopy(failure)
        prefix = "synthetic-secret:"
        private = M.private_failure_receipt(failure, RuntimeError(prefix + "x" * 5000))
        self.assertIsNot(private, failure)
        self.assertIs(private["resources"], failure["resources"])
        self.assertEqual(private["stage"], "source_state")
        self.assertEqual(private["errorMessage"], prefix + "x" * (4096 - len(prefix)))
        self.assertEqual(len(private["errorMessage"]), 4096)
        self.assertEqual(failure, before)
        self.assertNotIn("errorMessage", failure)
        self.assertEqual(M.public_failure_summary(failure), public)
        projected = M.public_failure_summary(private)
        self.assertEqual(projected, public)
        self.assertNotIn("synthetic-secret", M.encoded(projected).decode())

    def test_failed_mutation_is_dispatched_once_and_retains_unknown_responsibility(self):
        io = self.io()
        connection = Mock()
        connection.request.side_effect = TimeoutError("synthetic transport timeout")
        with patch.object(M, "write_json_once", return_value={"path": "/private/intent", "sha256": "a" * 64}) as persist, \
             patch.object(M.http.client, "HTTPConnection", return_value=connection):
            with self.assertRaises(TimeoutError):
                io.request("create-user", "POST", "/admin/v1/users", {"Name": "synthetic"})
            self.assertEqual(connection.request.call_count, 1)
            self.assertEqual(persist.call_count, 1)
            self.assertEqual(io.requests, {"normal": 1, "cleanup": 0})
            self.assertEqual(io.request_states[0]["outcome"], "unknown")
            connection.close.assert_called_once()

    def test_normal_exhaustion_cannot_consume_cleanup_reserve(self):
        io = self.io()
        io.requests["normal"] = M.BUDGETS["maximumRequests"] - M.BUDGETS["cleanupRequests"]
        with patch.object(M.http.client, "HTTPConnection") as connection, patch.object(M, "write_json_once") as write:
            with self.assertRaises(ValueError):
                io.request("normal-after-cap", "GET", "/healthz")
            connection.assert_not_called()
            write.assert_not_called()
            self.assertEqual(io.requests["cleanup"], 0)

    def test_fixed_length_premature_eof_is_not_a_complete_response(self):
        response = Mock(status=200, length=8)
        response.isclosed.return_value = True
        self.assertFalse(M.response_complete(response, "GET", b"{}", 1024))
        response.length = 0
        self.assertTrue(M.response_complete(response, "GET", b"{}", 1024))
        response.isclosed.return_value = False
        self.assertFalse(M.response_complete(response, "GET", b"{}", 1024))
        self.assertFalse(M.response_complete(response, "HEAD", b"unexpected", 1024))

    def test_known_native_cookie_survives_incomplete_login_body_for_exact_cleanup(self):
        response = {"status": 200, "complete": False, "raw": b"{", "headers": [("Set-Cookie", "goby_session=" + "a" * 32 + "; Path=/admin; HttpOnly")]}
        auth = M.response_auth(response, "native")
        self.assertEqual(auth["token"], "a" * 32)
        self.assertEqual(auth["csrf"], M.sha(("goby:admin:csrf:" + "a" * 32).encode()))
        with self.assertRaises(ValueError):
            M.response_auth(response, "emby")

    def test_q_policy_requires_explicit_empty_folders_without_disabling_playback(self):
        user = {"IsAdministrator": False, "IsDisabled": False, "Policy": {**M.PLAYBACK, "EnableAllFolders": False, "EnabledFolders": []}}
        M.check_policy(user, False)
        for patch_policy in ({"EnableAllFolders": True}, {"EnabledFolders": ["a" * 32]}, {"EnableMediaPlayback": False}):
            changed = copy.deepcopy(user)
            changed["Policy"].update(patch_policy)
            with self.assertRaises(ValueError):
                M.check_policy(changed, False)

    def test_scan_completion_requires_native_case_exact_job_and_no_warning(self):
        job = {"Id": "a" * 32, "LibraryId": "b" * 32, "ForceProbe": False, "Status": "completed", "Error": ""}
        self.assertTrue(M.completed_job(job, "b" * 32, "a" * 32))
        for changed in ({"Status": "Completed"}, {"Status": "failed"}, {"Error": "probe incomplete"}, {"LibraryId": "c" * 32}):
            with self.assertRaises(ValueError):
                M.completed_job({**job, **changed}, "b" * 32, "a" * 32)

    def test_actual_english_subtitle_alias_is_preserved_and_other_languages_rejected(self):
        for codec, index in (("srt", 2), ("vtt", 3)):
            row = {"Codec": codec, "Index": index, "IsExternal": True, "Path": str(M.C / "data/media/Movies" / ("M3e Client Movie.en." + codec))}
            for language in ("en", "eng"):
                result = M.mapped_subtitle({**row, "Language": language}, codec)
                self.assertEqual(result["language"], language)
                self.assertEqual(result["index"], index)
            for language in ("", "English", "fr", "zh", None):
                with self.assertRaises(ValueError):
                    M.mapped_subtitle({**row, "Language": language}, codec)


class MediaManifestV2Guards(unittest.TestCase):
    def fixture(self):
        file_bytes = {name: 1 for name in M.FILES}
        file_bytes[next(reversed(file_bytes))] = 201156949 - (len(file_bytes) - 1)
        return {"kind": "audited-candidate-media-fixture-manifest", "version": 2, "sourceRoot": str(M.MEDIA),
                "marker": "goby-client-media-m3e-v1", "files": copy.deepcopy(M.FILES),
                "fileBytes": file_bytes, "totalBytes": 201156949}

    def test_fresh_media_manifest_has_the_exact_fourteen_file_and_byte_closure(self):
        manifest = self.fixture()
        self.assertEqual(len(manifest["files"]), 14)
        self.assertEqual(set(manifest["fileBytes"]), set(manifest["files"]))
        self.assertEqual(sum(manifest["fileBytes"].values()), 201156949)
        self.assertEqual(M.validate_media_manifest(manifest, fresh=True), manifest)

    def test_legacy_media_manifest_keeps_its_marker_and_files_contract(self):
        manifest = {"marker": "goby-client-media-m3e-v1", "files": copy.deepcopy(M.FILES), "historicalMetadata": "retained"}
        self.assertEqual(M.validate_media_manifest(manifest), manifest)
        for mutation in ({"marker": "foreign"}, {"files": {}}):
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_media_manifest({**manifest, **mutation})

    def test_fresh_media_manifest_requires_exact_keys_kind_version_marker_and_source(self):
        for mutation in ({"kind": "audited-candidate-seed-input"}, {"version": 1}, {"version": True}, {"version": 2.0},
                         {"marker": "foreign"}, {"sourceRoot": str(M.C / "data/media")},
                         {"sourceRoot": str(M.E3 / "media")}, {"unexpected": False}):
            manifest = self.fixture()
            manifest.update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_media_manifest(manifest, fresh=True)
        for field in self.fixture():
            manifest = self.fixture()
            del manifest[field]
            with self.subTest(field=field), self.assertRaises((ValueError, KeyError)):
                M.validate_media_manifest(manifest, fresh=True)

    def test_fresh_media_file_names_and_hashes_cannot_be_missing_added_or_substituted(self):
        for field in ("files", "fileBytes"):
            for operation in ("remove", "add"):
                manifest = self.fixture()
                if operation == "remove":
                    del manifest[field][next(iter(manifest[field]))]
                else:
                    manifest[field]["Movies/foreign.mp4"] = "a" * 64 if field == "files" else 1
                with self.subTest(field=field, operation=operation), self.assertRaises(ValueError):
                    M.validate_media_manifest(manifest, fresh=True)
        manifest = self.fixture()
        manifest["files"]["Movies/M3e Client Movie.mp4"] = "a" * 64
        with self.assertRaises(ValueError):
            M.validate_media_manifest(manifest, fresh=True)

    def test_each_media_file_size_is_a_positive_integer(self):
        for name in M.FILES:
            for size in (0, -1, True, False, 1.0, "1"):
                manifest = self.fixture()
                manifest["fileBytes"][name] = size
                with self.subTest(name=name, size=size), self.assertRaises(ValueError):
                    M.validate_media_manifest(manifest, fresh=True)

    def test_media_size_sum_and_the_fixed_total_are_both_required(self):
        for operation in ("sum-only", "total-only", "consistent-wrong-total"):
            manifest = self.fixture()
            if operation in ("sum-only", "consistent-wrong-total"):
                manifest["fileBytes"][next(iter(manifest["fileBytes"]))] += 1
            if operation in ("total-only", "consistent-wrong-total"):
                manifest["totalBytes"] += 1
            with self.subTest(operation=operation), self.assertRaises(ValueError):
                M.validate_media_manifest(manifest, fresh=True)


class FixtureHardlinkV2Guards(unittest.TestCase):
    def identities(self):
        return {name: (17, 700 if checksum == M.MOVIE_SHA else 1000 + index, 0o100644, 0, 0,
                       4 if checksum == M.MOVIE_SHA else 1, 48786888 if checksum == M.MOVIE_SHA else 1, 100, 100)
                for index, (name, checksum) in enumerate(M.FILES.items())}

    def test_complete_four_path_movie_inode_group_is_allowed(self):
        identities = self.identities()
        movies = [row for name, row in identities.items() if M.FILES[name] == M.MOVIE_SHA]
        self.assertEqual(len(movies), 4)
        self.assertEqual(len(identities) - len(movies), 10)
        self.assertEqual({row[:2] for row in movies}, {(17, 700)})
        M.validate_fixture_hardlinks(identities)

    def test_external_links_split_movie_inodes_and_other_multilinks_are_rejected(self):
        for operation in ("external-link", "split-inodes", "other-multilink"):
            identities = self.identities()
            movie_names = [name for name, checksum in M.FILES.items() if checksum == M.MOVIE_SHA]
            if operation == "external-link":
                for name in movie_names:
                    row = identities[name]
                    identities[name] = (*row[:5], 5, *row[6:])
            elif operation == "split-inodes":
                for name in movie_names[2:]:
                    row = identities[name]
                    identities[name] = (row[0], 701, *row[2:])
            else:
                name = next(name for name, checksum in M.FILES.items() if checksum != M.MOVIE_SHA)
                row = identities[name]
                identities[name] = (*row[:5], 2, *row[6:])
            with self.subTest(operation=operation), self.assertRaises(ValueError):
                M.validate_fixture_hardlinks(identities)


class FreshAuthorityV2Guards(unittest.TestCase):
    def pins(self):
        return ({"path": str(M.E3 / "seed-input.json"), "sha256": "3" * 64},
                {"path": str(M.E3 / "seed-audited-candidate.py"), "sha256": "4" * 64})

    def test_fresh_seed_documents_form_one_pure_authority_chain(self):
        args = authority_fixture()
        input_pin, source_pin = self.pins()
        with patch.object(M, "descriptor") as descriptor, patch.object(M, "read_checked") as read, \
             patch.object(M, "write_json_once") as write, patch.object(M.os, "open") as open_file, \
             patch.object(M.http.client, "HTTPConnection") as connection:
            self.assertEqual(M.E3, Path("/opt/goby-test/embedded-candidate-20260914"))
            self.assertEqual(M.INITIAL_INSPECTION, M.E3 / "initial-runtime-inspection-sql-corrected")
            self.assertEqual(M.INSPECTION_HELPER, M.E3 / "private/operators-sql-corrected/inspect-audited-candidate.py")
            self.assertEqual(M.INSPECTION_PRIORS, inspection_prior_pins())
            self.assertEqual((args[0]["version"], args[3]["version"]), (2, 3))
            self.assertEqual(M.validate_seed_input(args[0], input_pin, source_pin), args[0])
            self.assertEqual(M.validate_fresh_authority(*args), FRESH_ROOT)
            for operation in (descriptor, read, write, open_file, connection):
                operation.assert_not_called()

    def test_legacy_seed_input_contract_is_preserved(self):
        value = authority_fixture()[0]
        value.update(version=1, output=str(M.R / "seed-guard"), candidateManifest=copy.deepcopy(M.MANIFEST),
                     runtimeInspection=copy.deepcopy(M.INSPECTION), runtimeHelper={"path": str(M.R / "prepare.py"), "sha256": "a" * 64},
                     mediaManifest={"path": str(M.MEDIA / "manifest.json"), "sha256": "f" * 64})
        del value["provisionInput"], value["inspectionHelper"]
        self.assertEqual(M.validate_seed_input(value), value)

    def test_seed_input_version_kind_membership_run_and_frozen_budget_are_exact(self):
        mutations = ({"version": True}, {"version": 2.0}, {"version": 3}, {"kind": "audited-candidate-admission-input"},
                     {"runId": "../foreign"}, {"runId": ""}, {"unexpected": False},
                     {"budgets": {**M.BUDGETS, "maximumRequests": M.BUDGETS["maximumRequests"] - 1}})
        for mutation in mutations:
            value = authority_fixture()[0]
            value.update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_seed_input(value, *self.pins())
        for field in authority_fixture()[0]:
            value = authority_fixture()[0]
            del value[field]
            with self.subTest(field=field), self.assertRaises((ValueError, KeyError)):
                M.validate_seed_input(value, *self.pins())

    def test_v2_selectors_reject_wrong_scope_escaping_paths_suffix_and_hash(self):
        for field, suffix in (("runtimeInspection", ".json"), ("provisionInput", ".json"), ("mediaManifest", ".json"),
                              ("runtimeHelper", ".py"), ("inspectionHelper", ".py")):
            paths = (str(M.R / ("foreign" + suffix)), str(Path(str(M.E3) + "-other") / ("foreign" + suffix)),
                     str(M.E3 / "../" / ("foreign" + suffix)), "relative" + suffix,
                     str(M.E3 / "wrong.txt"), str(M.C / ("foreign" + suffix)))
            for path in paths:
                value = authority_fixture()[0]
                value[field]["path"] = path
                with self.subTest(field=field, path=path), self.assertRaises(ValueError):
                    M.validate_seed_input(value, *self.pins())
            for fingerprint in ("a" * 63, "A" * 64, "g" * 64, None):
                value = authority_fixture()[0]
                value[field]["sha256"] = fingerprint
                with self.subTest(field=field, fingerprint=fingerprint), self.assertRaises(ValueError):
                    M.validate_seed_input(value, *self.pins())

    def test_executor_and_input_selectors_must_stay_in_the_fresh_scope(self):
        for index, suffix in ((0, ".json"), (1, ".py")):
            for mutation in ({"path": str(M.R / ("foreign" + suffix))},
                             {"path": str(M.E3 / "../" / ("foreign" + suffix))},
                             {"path": str(M.E3 / "wrong.txt")}, {"sha256": "invalid"}, {"extra": False}):
                pins = list(self.pins())
                pins[index].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaises(ValueError):
                    M.validate_seed_input(authority_fixture()[0], *pins)

    def test_candidate_manifest_must_derive_a_fresh_canonical_root(self):
        for path in (M.MANIFEST["path"], str(FRESH_ROOT / "manifest.json"), str(FRESH_ROOT / "private/other.json"),
                     str(FRESH_ROOT / "private/../private/manifest.json"),
                     "/opt/arbitrary-candidate/private/manifest.json", str(M.E3 / "private/manifest.json")):
            value = authority_fixture()[0]
            value["candidateManifest"]["path"] = path
            with self.subTest(path=path), self.assertRaises(ValueError):
                M.validate_seed_input(value, *self.pins())

    def test_only_the_sql_corrected_initial_inspection_receipt_is_admitted(self):
        for directory in ("initial-runtime-inspection-01", "initial-runtime-inspection-02", "later-runtime-inspection"):
            args = authority_fixture()
            args[0]["runtimeInspection"]["path"] = str(M.E3 / directory / "report.json")
            with self.subTest(directory=directory, validator="input"), self.assertRaises(ValueError):
                M.validate_seed_input(args[0], *self.pins())
            with self.subTest(directory=directory, validator="authority"), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
            args = authority_fixture()
            args[3]["output"] = str(M.E3 / directory)
            with self.subTest(directory=directory, validator="inspection-output"), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_inspection_helper_cannot_be_replaced_even_when_report_binding_matches(self):
        for path in (M.E3 / "inspect-audited-candidate.py", M.E3 / "private/operators/inspect-audited-candidate.py",
                     M.E3 / "private/operators-sql-corrected/renamed-inspector.py"):
            args = authority_fixture()
            args[0]["inspectionHelper"]["path"] = args[2]["helper"]["path"] = str(path)
            with self.subTest(path=str(path), validator="input"), self.assertRaises(ValueError):
                M.validate_seed_input(args[0], *self.pins())
            with self.subTest(path=str(path), validator="authority"), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_inspection_input_requires_version_three_exact_membership_and_budgets(self):
        for version in (1, 2, 3.0, True):
            args = authority_fixture()
            args[3]["version"] = version
            with self.subTest(version=version), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
        for field in authority_fixture()[3]:
            args = authority_fixture()
            del args[3][field]
            with self.subTest(field=field), self.assertRaises((ValueError, KeyError)):
                M.validate_fresh_authority(*args)
        args = authority_fixture()
        args[3]["unexpected"] = False
        with self.assertRaises(ValueError):
            M.validate_fresh_authority(*args)
        for field in authority_fixture()[3]["budgets"]:
            args = authority_fixture()
            args[3]["budgets"][field] += 1
            with self.subTest(budget=field), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_every_failed_inspection_and_diagnostic_pin_is_required_and_exact(self):
        priors = inspection_prior_pins()
        for document in (2, 3):
            for field in priors:
                for operation in ("remove", "hash", "path", "cross-wire"):
                    args = authority_fixture()
                    if operation == "remove":
                        del args[document][field]
                    elif operation == "hash":
                        args[document][field]["sha256"] = "0" * 64
                    elif operation == "path":
                        args[document][field]["path"] = str(M.E3 / "foreign-report.json")
                    else:
                        other = next(name for name in priors if name != field)
                        args[document][field] = copy.deepcopy(priors[other])
                    with self.subTest(document=document, field=field, operation=operation), self.assertRaises((ValueError, KeyError)):
                        M.validate_fresh_authority(*args)

    def test_matching_but_forged_failure_provenance_is_not_an_authority(self):
        for field in inspection_prior_pins():
            args = authority_fixture()
            for document in (2, 3):
                args[document][field]["sha256"] = "0" * 64
            with self.subTest(field=field), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_metadata_correction_review_keeps_its_fixed_path_and_digest(self):
        for operation in ("remove", "hash", "path"):
            args = authority_fixture()
            if operation == "remove":
                del args[2]["metadataCorrectionReview"]
            elif operation == "hash":
                args[2]["metadataCorrectionReview"]["sha256"] = "0" * 64
            else:
                args[2]["metadataCorrectionReview"]["path"] = str(M.E3 / "foreign-review.json")
            with self.subTest(operation=operation), self.assertRaises((ValueError, KeyError)):
                M.validate_fresh_authority(*args)

    def test_sql_correction_requires_exact_fields_values_and_boolean_types(self):
        mutations = ({"priorFailurePreserved": False}, {"priorFailurePreserved": 1},
                     {"priorSqlResultRecovered": True}, {"priorSqlResultRecovered": 0},
                     {"diagnosticReadProcessesClosed": False}, {"diagnosticReadProcessesClosed": 1},
                     {"passfilePolicy": "inherited_environment"}, {"stderrPolicy": "capture_only"}, {"unexpected": False})
        for mutation in mutations:
            args = authority_fixture()
            args[2]["sqlCorrection"].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
        for field in authority_fixture()[2]["sqlCorrection"]:
            args = authority_fixture()
            del args[2]["sqlCorrection"][field]
            with self.subTest(field=field), self.assertRaises((ValueError, KeyError)):
                M.validate_fresh_authority(*args)
        args = authority_fixture()
        del args[2]["sqlCorrection"]
        with self.assertRaises((ValueError, KeyError)):
            M.validate_fresh_authority(*args)

    def test_corrected_inspection_requires_all_read_processes_closed(self):
        for replacement in (False, 1, None):
            args = authority_fixture()
            if replacement is None:
                del args[2]["readProcessesClosed"]
            else:
                args[2]["readProcessesClosed"] = replacement
            with self.subTest(replacement=replacement), self.assertRaises((ValueError, KeyError)):
                M.validate_fresh_authority(*args)

    def test_v2_media_manifest_uses_the_fresh_private_selector(self):
        value = authority_fixture()[0]
        self.assertEqual(value["mediaManifest"]["path"], str(M.E3 / "private/client-media-manifest.json"))
        value["mediaManifest"]["path"] = str(M.MEDIA / "manifest.json")
        with self.assertRaises(ValueError):
            M.validate_seed_input(value, *self.pins())

    def test_candidate_run_data_slots_and_embedded_binary_bind_to_the_new_manifest(self):
        mutations = ({"runId": "20260914T010203Z-abcdefabcdef"}, {"dataDirectory": str(M.C / "data")},
                     {"database": "goby_candidate_legacy"}, {"recoveryDatabase": "goby_recovery_legacy"},
                     {"binary": {"path": str(M.C / "install/goby"), "sha256": M.EMBEDDED_BINARY_SHA}},
                     {"binary": {"path": str(FRESH_ROOT / "install/goby"), "sha256": "a" * 64}},
                     {"runtime": {"path": str(M.C / "private/runtime.env"), "sha256": "a" * 64}},
                     {"provisionVersion": 1}, {"status": "stopped"})
        for mutation in mutations:
            args = authority_fixture()
            args[1].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_candidate_must_remain_unseeded_and_unclaimed(self):
        for field in ("bootstrapExecuted", "recoveryRestoreExecuted", "candidateAdmissionComplete", "clientAcceptance", "taggedFullRegressionClaimed"):
            for replacement in (True, 0, None):
                args = authority_fixture()
                if replacement is None:
                    del args[1][field]
                else:
                    args[1][field] = replacement
                with self.subTest(field=field, replacement=replacement), self.assertRaises((ValueError, KeyError)):
                    M.validate_fresh_authority(*args)
        for mutation in ({"sourceState": {"users": 1, "schema": 28, "migrations": 28}},
                         {"sourceState": {"users": 0, "schema": 27, "migrations": 27}},
                         {"ordinaryRegressionStatus": "passed"}, {"ordinaryRegressionPhases": 1}):
            args = authority_fixture()
            args[1].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_all_product_evidence_pins_bind_candidate_inspection_and_provision(self):
        for field in M.PRODUCT_EVIDENCE:
            for document, container in ((1, "productEvidence"), (2, "sourceEvidence"), (4, None)):
                args = authority_fixture()
                target = args[document] if container is None else args[document][container]
                target[field]["sha256"] = "0" * 64
                with self.subTest(field=field, document=document), self.assertRaises(ValueError):
                    M.validate_fresh_authority(*args)

    def test_embedded_dashboard_and_both_loaded_unit_masks_are_required(self):
        for mutation in ({"mode": "external"}, {"assetCount": 56}, {"externalDirectoryInstalled": True},
                         {"webDirectoryOverridePresent": True}, {"buildManifest": {"path": "/foreign/manifest.json", "sha256": "a" * 64}}):
            args = authority_fixture()
            args[1]["dashboard"].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
        for role in (None, "server", "postgres"):
            args = authority_fixture()
            paths = args[1]["inaccessiblePaths"] if role is None else args[1]["loadedInaccessiblePaths"][role]
            paths.remove(str(M.C))
            with self.subTest(role=role), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
        args = authority_fixture()
        args[1]["inaccessiblePaths"].append(str(M.C))
        with self.assertRaises(ValueError):
            M.validate_fresh_authority(*args)

    def test_non_root_processes_and_the_observed_invocation_and_listener_are_exact(self):
        for role in ("server", "postgres"):
            for uid in (0, -1, True, "55242"):
                args = authority_fixture()
                args[1][role + "Identity"]["uid"] = uid
                with self.subTest(role=role, uid=uid), self.assertRaises(ValueError):
                    M.validate_fresh_authority(*args)
            for field, replacement in (("pid", 8000), ("startTicks", 3000), ("bootId", "foreign-boot"),
                                        ("exe", "/foreign/goby"), ("executableInode", 9000)):
                args = authority_fixture()
                args[2]["processes"][role][field] = replacement
                with self.subTest(role=role, field=field), self.assertRaises(ValueError):
                    M.validate_fresh_authority(*args)
        args = authority_fixture()
        args[2]["httpListener"]["socketInode"] += 1
        with self.assertRaises(ValueError):
            M.validate_fresh_authority(*args)

    def test_initial_inspection_must_be_complete_success_without_mutations(self):
        for mutation in ({"version": 2}, {"version": 3.0}, {"status": "ready_pending_live_acceptance"},
                         {"status": "inspection_failed"}, {"stage": "runtime_configuration"},
                         {"failure": {"type": "InspectionError", "code": "inspection_read_command_unclosed"}},
                         {"databaseWrites": 1}, {"databaseWrites": False}):
            args = authority_fixture()
            args[2].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
        for field in ("failure", "bootstrapPerformed", "restorePerformed", "serviceChangesPerformed", "candidateAdmissionComplete", "clientAcceptance"):
            args = authority_fixture()
            del args[2][field]
            with self.subTest(field=field), self.assertRaises((ValueError, KeyError)):
                M.validate_fresh_authority(*args)
        for field in ("bootstrapPerformed", "restorePerformed", "serviceChangesPerformed", "candidateAdmissionComplete", "clientAcceptance"):
            for replacement in (True, 0):
                args = authority_fixture()
                args[2][field] = replacement
                with self.subTest(field=field, replacement=replacement), self.assertRaises(ValueError):
                    M.validate_fresh_authority(*args)

    def test_inspection_provision_manifest_helpers_and_inputs_cannot_be_cross_wired(self):
        for document, field in ((1, "input"), (2, "provisionManifest"), (2, "provisionInput"), (2, "provisionHelper"),
                                (2, "helper"), (3, "provisionManifest"), (3, "provisionInput"), (3, "provisionHelper"),
                                (3, "sourceArchive"), (3, "sourceManifest")):
            args = authority_fixture()
            args[document][field]["sha256"] = "0" * 64
            with self.subTest(document=document, field=field), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
        for document, mutation in ((3, {"output": str(M.E3 / "later-inspection")}), (3, {"version": 2}),
                                    (4, {"runId": "20260914T010203Z-abcdefabcdef"}), (4, {"version": 1}),
                                    (4, {"public_url": "http://127.0.0.1:18096"}), (4, {"dashboardProfile": "external"}),
                                    (4, {"ports": {"http": 18096, "postgres": 5432}})):
            args = authority_fixture()
            args[document].update(mutation)
            with self.subTest(document=document, mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_attested_inspection_input_cannot_select_the_old_resumed_scope(self):
        args = authority_fixture()
        args[2]["input"]["path"] = str(M.R / "inspection-input.json")
        with self.assertRaises(ValueError):
            M.validate_fresh_authority(*args)

    def test_inspected_empty_schema_and_embedded_asset_results_are_required(self):
        for mutation in ({"sourceSchemaVersion": 27}, {"sourceMigrationCount": 27}, {"sourceUsers": 1}, {"sourceUsers": False},
                         {"recoveryTargetEmpty": False}, {"recoveryTargetEmpty": 1}, {"allSqlTransactionsReadOnly": False}):
            args = authority_fixture()
            args[2]["database"].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)
        for mutation in ({"assetCount": 56}, {"allAssetsMatched": False}, {"allAssetsMatched": 1}, {"entryReferencesMatched": 4}):
            args = authority_fixture()
            args[2]["dashboard"].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.validate_fresh_authority(*args)

    def test_seed2_automatically_uses_fresh_io_without_opening_resources(self):
        value = authority_fixture()[0]
        with patch.object(M, "descriptor") as descriptor, patch.object(M, "read_checked") as read, \
             patch.object(M, "write_json_once") as write, patch.object(M.os, "mkdir") as mkdir, \
             patch.object(M.http.client, "HTTPConnection") as connection:
            io = M.CandidateIO(value, *self.pins())
            self.assertEqual(io.root, FRESH_ROOT)
            self.assertEqual(io.output, M.E3 / "seed-guard")
            self.assertEqual(io.requests, {"normal": 0, "cleanup": 0})
            self.assertFalse(io.created)
            for operation in (descriptor, read, write, mkdir, connection):
                operation.assert_not_called()

    def test_admission4_can_explicitly_use_fresh_io_with_bounded_non_seed_budgets(self):
        value = authority_fixture()[0]
        value.update(kind="audited-candidate-admission-input", version=4, output=str(M.E3 / "admission-guard"),
                     budgets={"maximumSeconds": 900, "cleanupSeconds": 100, "maximumRequests": 220, "cleanupRequests": 10})
        io = M.CandidateIO(value, *self.pins(), fresh=True)
        self.assertEqual(io.root, FRESH_ROOT)
        self.assertEqual(io.budgets, value["budgets"])
        self.assertNotEqual(io.budgets, M.BUDGETS)

    def test_fresh_io_budget_types_caps_and_cleanup_reserves_are_enforced(self):
        for mutation in ({"maximumSeconds": 1201}, {"maximumRequests": 241}, {"maximumSeconds": True},
                         {"maximumRequests": 240.0}, {"cleanupSeconds": 0}, {"cleanupRequests": -1},
                         {"cleanupSeconds": 1200}, {"cleanupRequests": 240}, {"unexpected": 1}):
            value = authority_fixture()[0]
            value["budgets"].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                M.CandidateIO(value, *self.pins())

    def test_fresh_io_output_cannot_escape_scope_or_enter_the_legacy_candidate(self):
        for output in (str(M.R / "seed-guard"), str(M.E3 / "nested/seed-guard"), str(M.E3 / "../seed-guard"),
                       str(FRESH_ROOT / "seed-guard"), str(M.C / "private/seed-guard"), str(M.E3 / "Uppercase")):
            value = authority_fixture()[0]
            value["output"] = output
            with self.subTest(output=output), self.assertRaises(ValueError):
                M.CandidateIO(value, *self.pins())

    def test_fresh_output_collision_precedes_authority_reads_or_candidate_access(self):
        io = M.CandidateIO(authority_fixture()[0], *self.pins())
        with patch.object(M.os.path, "lexists", return_value=True), patch.object(M, "descriptor") as descriptor, \
             patch.object(M, "read_checked") as read, patch.object(M, "write_json_once") as write, \
             patch.object(M.os, "mkdir") as mkdir, patch.object(M.http.client, "HTTPConnection") as connection:
            with self.assertRaises(ValueError):
                io.open()
            for operation in (descriptor, read, write, mkdir, connection):
                operation.assert_not_called()
            self.assertFalse(io.created)


class FreshSqlEnvironmentV2Guards(unittest.TestCase):
    def io(self, fresh=True):
        io = M.CandidateIO.__new__(M.CandidateIO)
        io.fresh, io.root = fresh, FRESH_ROOT
        io.candidate = {"postgresIdentity": {"uid": 55242}}
        environment = {"PATH": "/usr/bin", "LC_ALL": "C", "PGPASSFILE": "/dev/null", "PGSERVICEFILE": "/legacy/service.conf"}
        io.runtime_module = SimpleNamespace(ENV=environment)
        original = Mock(return_value=b"synthetic rows")
        io.provision = SimpleNamespace(psql=original)
        return io, environment, original

    def socket_info(self, **changes):
        return SimpleNamespace(**{"st_dev": 17, "st_ino": 800, "st_uid": 55242, "st_gid": 55242,
                                  "st_mode": 0o40700, **changes})

    def test_environment_copy_preserves_other_values_and_replaces_credential_defaults(self):
        for inherited in ({}, {"PGPASSFILE": "/legacy/.pgpass", "PGSERVICEFILE": "/legacy/.pg_service.conf"},
                          {"PGPASSFILE": "/dev/null", "PGSERVICEFILE": "/dev/null", "PGCONNECT_TIMEOUT": "30"}):
            environment = {"PATH": "/usr/bin", "LC_ALL": "C", **inherited}
            before = copy.deepcopy(environment)
            result = M.fresh_sql_environment(environment, FRESH_ROOT)
            self.assertIsNot(result, environment)
            self.assertEqual(environment, before)
            self.assertEqual(result, {**before,
                "PGPASSFILE": str(FRESH_ROOT / "postgres/socket/.goby-inspection-no-password"),
                "PGSERVICEFILE": str(FRESH_ROOT / "postgres/socket/.goby-inspection-no-service"), "PGCONNECT_TIMEOUT": "3"})

    def test_configure_keeps_independent_environment_copies_and_accepts_owned_absent_paths(self):
        io, environment, original = self.io()
        before = copy.deepcopy(environment)
        with patch.object(M, "safe_path", return_value=self.socket_info()) as safe, \
             patch.object(M.os.path, "lexists", return_value=False) as exists:
            io.configure_fresh_sql_environment()
            io.check_fresh_sql_environment()
            safe.assert_called_with(FRESH_ROOT / "postgres/socket", (0, 55242), True)
            self.assertEqual(io.sql_socket_identity, (17, 800, 55242, 55242, 0o40700))
            self.assertEqual(io.runtime_module.ENV, io.sql_environment)
            self.assertIsNot(io.runtime_module.ENV, io.sql_environment)
            self.assertIsNot(io.runtime_module.ENV, environment)
            self.assertEqual(environment, before)
            expected_paths = [io.sql_environment[key] for key in ("PGPASSFILE", "PGSERVICEFILE")]
            self.assertEqual([call.args[0] for call in exists.call_args_list], expected_paths * 2)
            original.assert_not_called()

    def test_wrong_owner_permissions_and_existing_or_dangling_credential_paths_are_rejected(self):
        for changes in ({"st_uid": 0}, {"st_uid": 55243}, {"st_mode": 0o40755}):
            io, _, original = self.io()
            with patch.object(M, "safe_path", return_value=self.socket_info(**changes)), \
                 patch.object(M.os.path, "lexists", return_value=False), \
                 self.subTest(changes=changes), self.assertRaises(ValueError):
                io.configure_fresh_sql_environment()
            original.assert_not_called()
        for field in ("PGPASSFILE", "PGSERVICEFILE"):
            io, environment, original = self.io()
            blocked = M.fresh_sql_environment(environment, FRESH_ROOT)[field]
            with patch.object(M, "safe_path", return_value=self.socket_info()), \
                 patch.object(M.os.path, "lexists", side_effect=lambda path: path == blocked), \
                 self.subTest(field=field), self.assertRaises(ValueError):
                io.configure_fresh_sql_environment()
            original.assert_not_called()

    def test_socket_identity_and_loaded_environment_drift_are_rejected(self):
        for changes in ({"st_dev": 18}, {"st_ino": 801}, {"st_uid": 55243}, {"st_gid": 55243}, {"st_mode": 0o40710}):
            io, _, original = self.io()
            with patch.object(M, "safe_path", return_value=self.socket_info()) as safe, \
                 patch.object(M.os.path, "lexists", return_value=False):
                io.configure_fresh_sql_environment()
                safe.return_value = self.socket_info(**changes)
                with self.subTest(changes=changes), self.assertRaises(ValueError):
                    io.check_fresh_sql_environment()
            original.assert_not_called()
        for mutation in ({"PGPASSFILE": "/dev/null"}, {"PGSERVICEFILE": "/dev/null"}, {"PATH": "/foreign/bin"}):
            io, _, original = self.io()
            with patch.object(M, "safe_path", return_value=self.socket_info()), \
                 patch.object(M.os.path, "lexists", return_value=False):
                io.configure_fresh_sql_environment()
                io.runtime_module.ENV.update(mutation)
                with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                    io.check_fresh_sql_environment()
            original.assert_not_called()

    def test_wrapped_sql_is_dispatched_once_and_rechecked_after_success_or_exception(self):
        for fails in (False, True):
            io, _, original = self.io()
            events = []
            io.check_fresh_sql_environment = Mock(side_effect=lambda: events.append("check"))

            def execute(*args):
                events.append("sql")
                if fails:
                    raise RuntimeError("synthetic SQL failure")
                return b"synthetic rows"

            original.side_effect = execute
            io.configure_fresh_sql_environment()
            events.clear()
            io.check_fresh_sql_environment.reset_mock()
            with self.subTest(fails=fails):
                if fails:
                    with self.assertRaisesRegex(RuntimeError, "synthetic SQL failure"):
                        io.provision.psql("guard-query", "SELECT 1;")
                    original.assert_called_once_with("guard-query", "SELECT 1;", "postgres")
                else:
                    self.assertEqual(io.provision.psql("guard-query", "SELECT 1;", "owned_database"), b"synthetic rows")
                    original.assert_called_once_with("guard-query", "SELECT 1;", "owned_database")
                self.assertEqual(events, ["check", "sql", "check"])
                self.assertEqual(io.check_fresh_sql_environment.call_count, 2)

    def test_sql_preflight_failure_prevents_dispatch(self):
        io, _, original = self.io()
        io.check_fresh_sql_environment = Mock()
        io.configure_fresh_sql_environment()
        io.check_fresh_sql_environment.reset_mock()
        io.check_fresh_sql_environment.side_effect = ValueError("synthetic preflight failure")
        with self.assertRaisesRegex(ValueError, "synthetic preflight failure"):
            io.provision.psql("guard-query", "SELECT 1;")
        original.assert_not_called()
        io.check_fresh_sql_environment.assert_called_once_with()

    def test_fresh_pin_rechecks_sql_authority_before_process_or_filesystem_access(self):
        io, _, original = self.io()
        io.check_fresh_sql_environment = Mock(side_effect=ValueError("synthetic pin preflight failure"))
        io.provision.show = Mock()
        with patch.object(M.Path, "read_text") as read, patch.object(M.os, "readlink") as readlink:
            with self.assertRaisesRegex(ValueError, "synthetic pin preflight failure"):
                io.pin()
            io.check_fresh_sql_environment.assert_called_once_with()
            io.provision.show.assert_not_called()
            original.assert_not_called()
            read.assert_not_called()
            readlink.assert_not_called()

    def test_legacy_configure_rejects_without_changing_module_environment_or_sql(self):
        io, environment, original = self.io(fresh=False)
        before = copy.deepcopy(environment)
        with patch.object(M, "fresh_sql_environment") as prepare, \
             patch.object(io, "check_fresh_sql_environment") as check:
            with self.assertRaises(ValueError):
                io.configure_fresh_sql_environment()
            self.assertIs(io.runtime_module.ENV, environment)
            self.assertEqual(environment, before)
            self.assertIs(io.provision.psql, original)
            prepare.assert_not_called()
            check.assert_not_called()
            original.assert_not_called()


class CatalogV2Guards(unittest.TestCase):
    def mapped(self, details=None, libraries=None):
        if details is None:
            details, libraries = catalog_fixture()
        return M.map_catalog(details, libraries, root=FRESH_ROOT, observed_tv=True)

    def test_complete_catalog_uses_the_supplied_fresh_root_and_observed_tv_fields(self):
        details, libraries = catalog_fixture()
        result = self.mapped(details, libraries)
        self.assertNotEqual(FRESH_ROOT, M.C)
        self.assertEqual(len(details), 10)
        self.assertEqual(result["movie"]["path"], str(FRESH_ROOT / "data/media/Movies/M3e Client Movie.mp4"))
        self.assertEqual(result["movie"]["mediaSha256"], M.FILES["Movies/M3e Client Movie.mp4"])
        self.assertEqual(result["album"], {"id": details[7]["Id"], "name": "M3e Synthetic Album"})
        self.assertEqual(result["musicLibrary"]["id"], libraries["Music"]["Id"])
        self.assertEqual(result["tvLibrary"]["id"], libraries["TV"]["Id"])
        self.assertEqual([(row["index"], row["language"]) for row in result["subtitles"]], [(2, "en"), (3, "eng")])
        self.assertEqual([row["id"] for row in result["seasons"]], [row["Id"] for row in details[2:4]])
        self.assertEqual([row["id"] for row in result["episodes"]], [row["Id"] for row in details[4:7]])
        for observed, projected in zip(details[2:4], result["seasons"]):
            self.assertEqual(projected["id"], observed["Id"])
            self.assertEqual(projected["seriesId"], observed["SeriesId"])
            self.assertEqual(projected["parentId"], observed["ParentId"])
        for observed, projected in zip(details[4:7], result["episodes"]):
            self.assertEqual(projected["id"], observed["Id"])
            self.assertEqual(projected["seriesId"], observed["SeriesId"])
            self.assertEqual(projected["parentId"], observed["SeasonId"])

    def test_legacy_default_still_accepts_parent_edges_without_tv_projection(self):
        details, libraries = catalog_fixture(M.C)
        for row in details:
            for field in ("SeriesId", "SeriesName", "SeasonId", "SeasonName"):
                row.pop(field, None)
        result = M.map_catalog(details, libraries)
        self.assertEqual(result["series"]["id"], details[1]["Id"])
        self.assertEqual([row["seriesId"] for row in result["episodes"]], [details[1]["Id"]] * 3)

    def test_every_episode_tv_field_is_required_and_must_match_the_observed_chain(self):
        for index in (4, 5, 6):
            for field in ("SeriesId", "SeriesName", "SeasonId", "SeasonName"):
                for replacement in (None, "", "f" * 32, "Forged parent"):
                    details, libraries = catalog_fixture()
                    if replacement is None:
                        del details[index][field]
                    else:
                        details[index][field] = replacement
                    with self.subTest(index=index, field=field, replacement=replacement), self.assertRaises((ValueError, KeyError, TypeError)):
                        self.mapped(details, libraries)

    def test_every_season_series_field_is_required_and_must_match_its_parent(self):
        for index in (2, 3):
            for field in ("SeriesId", "SeriesName"):
                for replacement in (None, "", "f" * 32, "Forged series"):
                    details, libraries = catalog_fixture()
                    if replacement is None:
                        del details[index][field]
                    else:
                        details[index][field] = replacement
                    with self.subTest(index=index, field=field, replacement=replacement), self.assertRaises((ValueError, KeyError, TypeError)):
                        self.mapped(details, libraries)

    def test_parent_ids_and_tv_names_cannot_be_replaced_by_consistent_looking_references(self):
        for index, mutation in ((2, {"ParentId": "f" * 32, "SeriesId": "f" * 32}),
                                (4, {"ParentId": "f" * 32, "SeasonId": "f" * 32}),
                                (4, {"ParentIndexNumber": 2}), (4, {"IndexNumber": 2}),
                                (2, {"Name": "Forged season"})):
            details, libraries = catalog_fixture()
            details[index].update(mutation)
            with self.subTest(index=index, mutation=mutation), self.assertRaises((ValueError, KeyError, TypeError)):
                self.mapped(details, libraries)

    def test_metadata_titles_are_observed_and_not_replaced_by_seed_guesses(self):
        for index in (0, 1, 4, 5, 6, 7, 8, 9):
            details, libraries = catalog_fixture()
            details[index]["Name"] = "Forged metadata title"
            with self.subTest(index=index), self.assertRaises(ValueError):
                self.mapped(details, libraries)

    def test_supported_observed_album_and_audio_names_are_preserved(self):
        details, libraries = catalog_fixture()
        details[7]["Name"] = "Music"
        details[8]["Name"] = details[9]["Name"] = "M3e Client Audio"
        result = self.mapped(details, libraries)
        self.assertEqual(result["album"]["name"], "Music")
        self.assertEqual(result["mp3"]["name"], "M3e Client Audio")
        self.assertEqual(result["flac"]["name"], "M3e Client Audio")

    def test_old_candidate_sibling_and_escaping_media_paths_are_rejected(self):
        relative = "Movies/M3e Client Movie.mp4"
        for path in (str(M.C / "data/media" / relative), str(Path(str(FRESH_ROOT) + "-other") / "data/media" / relative),
                     str(FRESH_ROOT / "data/media/Movies/../../" / relative), "/tmp/" + relative):
            details, libraries = catalog_fixture()
            details[0]["Path"] = details[0]["MediaSources"][0]["Path"] = path
            with self.subTest(path=path), self.assertRaises(ValueError):
                self.mapped(details, libraries)

    def test_media_source_must_bind_exact_item_path_duration_and_readiness(self):
        mutations = ({"ItemId": "f" * 32}, {"Path": "/foreign/movie.mp4"}, {"RunTimeTicks": 1},
                     {"SupportsDirectPlay": False}, {"SupportsDirectPlay": 1},
                     {"SupportsDirectStream": False}, {"SupportsDirectStream": 1})
        for index in (0, 4, 8, 9):
            for mutation in mutations:
                details, libraries = catalog_fixture()
                details[index]["MediaSources"][0].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaises(ValueError):
                    self.mapped(details, libraries)
            for sources in ([], [copy.deepcopy(catalog_fixture()[0][index]["MediaSources"][0])] * 2):
                details, libraries = catalog_fixture()
                details[index]["MediaSources"] = sources
                with self.subTest(index=index, count=len(sources)), self.assertRaises((ValueError, IndexError)):
                    self.mapped(details, libraries)

    def test_duration_must_match_the_admitted_media_and_have_integer_type(self):
        for index, values in ((0, (0, -1, True, 5999999999, 6000000000.0)),
                               (4, (0, -1, True, 1199999999, 1200000000.0)),
                               (8, (0, -1, True, 1799999999, 1800000000.0)),
                               (9, (0, -1, True, 1799999999, 1800000000.0))):
            for value in values:
                details, libraries = catalog_fixture()
                details[index]["RunTimeTicks"] = details[index]["MediaSources"][0]["RunTimeTicks"] = value
                with self.subTest(index=index, value=value), self.assertRaises(ValueError):
                    self.mapped(details, libraries)

    def test_movie_and_audio_codec_or_container_substitutions_are_rejected(self):
        for index, stream_index, codec in ((0, 0, "hevc"), (0, 1, "mp3"), (8, 0, "aac"), (9, 0, "mp3")):
            details, libraries = catalog_fixture()
            details[index]["MediaStreams"][stream_index]["Codec"] = codec
            with self.subTest(index=index, stream=stream_index), self.assertRaises(ValueError):
                self.mapped(details, libraries)
        for index in (8, 9):
            details, libraries = catalog_fixture()
            details[index]["MediaSources"][0]["Container"] = "mp4"
            with self.subTest(index=index), self.assertRaises(ValueError):
                self.mapped(details, libraries)

    def test_every_playable_item_requires_zero_observer_userdata(self):
        for index in (0, 4, 5, 6, 8, 9):
            for mutation in ({"Played": True}, {"Played": 0}, {"PlaybackPositionTicks": 1}, {"PlayCount": 1}):
                details, libraries = catalog_fixture()
                details[index]["UserData"].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaises(ValueError):
                    self.mapped(details, libraries)

    def test_external_subtitles_bind_to_the_new_root_and_keep_real_english_aliases(self):
        details, _ = catalog_fixture()
        for row in details[0]["MediaStreams"][2:]:
            result = M.mapped_subtitle(row, row["Codec"], root=FRESH_ROOT)
            self.assertEqual(result["language"], row["Language"])
            self.assertEqual(result["index"], row["Index"])
            self.assertEqual(result["sha256"], M.FILES["Movies/M3e Client Movie.en." + row["Codec"]])
            for mutation in ({"Language": "English"}, {"Language": "fr"}, {"Index": True}, {"Index": -1},
                             {"IsExternal": False}, {"Codec": "ass"},
                             {"Path": str(M.C / "data/media/Movies" / ("M3e Client Movie.en." + row["Codec"]))},
                             {"Path": str(FRESH_ROOT / "data/media/Movies/../" / ("M3e Client Movie.en." + row["Codec"]))}):
                with self.subTest(codec=row["Codec"], mutation=mutation), self.assertRaises(ValueError):
                    M.mapped_subtitle({**row, **mutation}, row["Codec"], root=FRESH_ROOT)

    def test_subtitle_inventory_codec_uniqueness_and_index_uniqueness_are_required(self):
        for mutation in ("remove", "duplicate", "same-index", "same-codec"):
            details, libraries = catalog_fixture()
            streams = details[0]["MediaStreams"]
            if mutation == "remove":
                streams.pop()
            elif mutation == "duplicate":
                streams.append(copy.deepcopy(streams[-1]))
            elif mutation == "same-index":
                streams[-1]["Index"] = streams[-2]["Index"]
            else:
                streams[-1]["Codec"] = streams[-2]["Codec"]
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                self.mapped(details, libraries)


class StoredProjectionV2Guards(unittest.TestCase):
    def test_exact_thirteen_stored_rows_and_ten_public_items_pass(self):
        args = source_state_fixture()
        result = M.validate_source_state(*args)
        self.assertEqual((result["storedItems"], result["publicItems"], result["collectionRoots"]), (13, 10, 3))
        self.assertEqual(len(args[0]["items"]), 13)
        self.assertEqual(sum(row["type"] == "CollectionFolder" for row in args[0]["items"]), 3)

    def test_schema_history_and_owned_user_membership_are_required(self):
        for mutation in ({"schema": 27}, {"schema": 29}, {"historyRows": 1}):
            args = source_state_fixture()
            args[0].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)
        for operation in ("remove", "add", "replace", "duplicate"):
            args = source_state_fixture()
            users = args[0]["users"]
            if operation == "remove":
                users.pop()
            elif operation == "add":
                users.append({**users[0], "id": "foreign"})
            elif operation == "replace":
                users[0]["id"] = "foreign"
            else:
                users[-1] = copy.deepcopy(users[0])
            with self.subTest(operation=operation), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)

    def test_stored_user_names_admin_and_disabled_flags_match_owned_actors(self):
        for index in range(8):
            for mutation in ({"name": "Forged actor"}, {"disabled": True}, {"admin": index != 0}):
                args = source_state_fixture()
                args[0]["users"][index].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaises(M.StateCheckError):
                    M.validate_source_state(*args)

    def test_exact_owned_completed_jobs_cannot_be_replaced_or_softened(self):
        for index in range(3):
            for mutation in ({"id": "foreign"}, {"libraryId": "foreign"}, {"status": "completed"},
                             {"status": "Running"}, {"error": "retained warning"}, {"forceProbe": True}):
                args = source_state_fixture()
                args[0]["jobs"][index].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaises(M.StateCheckError):
                    M.validate_source_state(*args)

    def test_stored_q_has_eight_keys_while_public_policy_has_six(self):
        args = source_state_fixture()
        public_policy = {**M.PLAYBACK, "EnableAllFolders": False, "EnabledFolders": []}
        self.assertEqual(set(M.STORED_Q_POLICY), set(public_policy) | {"IsAdministrator", "IsDisabled"})
        self.assertEqual((len(M.STORED_Q_POLICY), len(public_policy)), (8, 6))
        self.assertIs(M.STORED_Q_POLICY["IsAdministrator"], False)
        self.assertIs(M.STORED_Q_POLICY["IsDisabled"], False)
        M.check_policy({"IsAdministrator": False, "IsDisabled": False, "Policy": public_policy}, False)
        args[0]["users"][-1]["policy"] = public_policy
        with self.assertRaisesRegex(M.StateCheckError, "stored_q_policy_differs"):
            M.validate_source_state(*args)

    def test_q_legacy_keys_bool_types_and_exact_policy_membership_are_required(self):
        mutations = ({"IsAdministrator": True}, {"IsDisabled": True}, {"IsAdministrator": 0}, {"IsDisabled": 0},
                     {"UnexpectedPolicy": False}, {"EnabledFolders": ["foreign-library"]},
                     {"EnableAllFolders": True}, {"EnableMediaPlayback": False})
        for mutation in mutations:
            args = source_state_fixture()
            args[0]["users"][-1]["policy"].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaisesRegex(M.StateCheckError, "stored_q_policy_differs"):
                M.validate_source_state(*args)
        for field in M.STORED_Q_POLICY:
            args = source_state_fixture()
            del args[0]["users"][-1]["policy"][field]
            with self.subTest(field=field), self.assertRaisesRegex(M.StateCheckError, "stored_q_policy_differs"):
                M.validate_source_state(*args)

    def test_admin_and_ordinary_stored_policies_remain_empty_objects(self):
        for index in range(7):
            args = source_state_fixture()
            self.assertEqual(args[0]["users"][index]["policy"], {})
            args[0]["users"][index]["policy"] = {**M.PLAYBACK, "EnableAllFolders": True, "EnabledFolders": []}
            with self.subTest(index=index), self.assertRaisesRegex(M.StateCheckError, "stored_default_policy_differs"):
                M.validate_source_state(*args)

    def test_extra_missing_duplicate_or_replaced_stored_roots_are_rejected(self):
        for operation in ("add", "remove", "replace", "duplicate"):
            args = source_state_fixture()
            if operation == "add":
                args[0]["items"].append({"id": "unknown"})
            elif operation == "remove":
                args[0]["items"].pop()
            elif operation == "replace":
                args[0]["items"][-1]["id"] = "unknown"
            else:
                args[0]["items"][-1] = copy.deepcopy(args[0]["items"][-2])
            with self.subTest(operation=operation), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)

    def test_public_collection_roots_cannot_expand_the_ten_item_dto_boundary(self):
        args = source_state_fixture()
        root = args[0]["items"][-1]
        args[1].append({"Id": root["id"], "Name": root["name"], "Type": root["type"], "ParentId": root["parentId"], "Path": root["path"]})
        with self.assertRaises(M.StateCheckError):
            M.validate_source_state(*args)

    def test_collection_root_name_parent_type_path_and_membership_are_exact(self):
        for index in (10, 11, 12):
            for mutation in ({"name": "Other"}, {"parentId": "foreign"}, {"type": "Folder"}, {"path": "/owned"}, {"unknown": False}):
                args = source_state_fixture()
                args[0]["items"][index].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaisesRegex(M.StateCheckError, "collection_folder_shape_differs"):
                    M.validate_source_state(*args)

    def test_only_the_root_music_album_may_omit_its_public_path(self):
        args = source_state_fixture()
        self.assertNotIn("Path", args[1][7])
        self.assertEqual(args[0]["items"][7]["path"], "")
        M.validate_source_state(*args)
        for replacement in ("", str(FRESH_ROOT / "data/media/Music")):
            args = source_state_fixture()
            args[1][7]["Path"] = replacement
            with self.subTest(replacement=replacement), self.assertRaisesRegex(M.StateCheckError, "root_album_projection_differs"):
                M.validate_source_state(*args)
        for index in (*range(7), 8, 9):
            args = source_state_fixture()
            del args[1][index]["Path"]
            with self.subTest(index=index), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)
        for mutation in ({"parentId": args[1][1]["Id"]}, {"type": "Folder"}, {"path": "/foreign/album"}):
            args = source_state_fixture()
            args[0]["items"][7].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)

    def test_public_names_paths_types_and_every_parent_edge_match_stored_rows(self):
        for index in range(10):
            mutations = ({"name": "Forged stored title"}, {"type": "Folder"}, {"parentId": "foreign"}, {"path": "/foreign/item"})
            for mutation in mutations:
                args = source_state_fixture()
                args[0]["items"][index].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaises(M.StateCheckError):
                    M.validate_source_state(*args)

    def test_library_and_root_paths_cannot_point_to_the_old_candidate(self):
        for index, directory in enumerate(("Movies", "TV", "Music")):
            args = source_state_fixture()
            args[0]["roots"][index]["path"] = str(M.C / "data/media" / directory)
            with self.subTest(directory=directory), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)

    def test_exactly_two_owned_revoked_sessions_match_cleanup_kind_and_fingerprint(self):
        for index in (0, 1):
            for mutation in ({"revoked": False}, {"revoked": 1}, {"tokenSha256": "c" * 64},
                             {"kind": "unknown"}, {"userId": "foreign"}):
                args = source_state_fixture()
                args[0]["sessions"][index].update(mutation)
                with self.subTest(index=index, mutation=mutation), self.assertRaises(M.StateCheckError):
                    M.validate_source_state(*args)
        for operation in ("remove", "add", "swap", "duplicate-kind"):
            args = source_state_fixture()
            sessions = args[0]["sessions"]
            if operation == "remove":
                sessions.pop()
            elif operation == "add":
                sessions.append(copy.deepcopy(sessions[0]))
            elif operation == "swap":
                sessions[0]["tokenSha256"], sessions[1]["tokenSha256"] = sessions[1]["tokenSha256"], sessions[0]["tokenSha256"]
            else:
                sessions[1]["kind"] = sessions[0]["kind"]
            with self.subTest(operation=operation), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)

    def test_cleanup_has_exact_native_and_emby_kind_to_token_bindings(self):
        for operation in ("unknown", "duplicate-kind", "swap", "remove", "add"):
            args = source_state_fixture()
            cleanup = args[5]
            if operation == "unknown":
                cleanup[0]["kind"] = "unknown"
            elif operation == "duplicate-kind":
                cleanup[1]["kind"] = "native"
            elif operation == "swap":
                cleanup[0]["tokenSha256"], cleanup[1]["tokenSha256"] = cleanup[1]["tokenSha256"], cleanup[0]["tokenSha256"]
            elif operation == "remove":
                cleanup.pop()
            else:
                cleanup.append(copy.deepcopy(cleanup[0]))
            with self.subTest(operation=operation), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)


if __name__ == "__main__":
    unittest.main()
