#!/usr/bin/env python3
"""Pure stored/public projection guards; do not open candidate resources."""
import copy
import importlib.util
from pathlib import Path
import unittest

SPEC = importlib.util.spec_from_file_location("seed_reconciliation", Path(__file__).with_name("reconcile-audited-candidate-seed.py"))
M = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(M)


class ProjectionGuards(unittest.TestCase):
    def fixture(self):
        roles = ("admin", "movie", "episode", "mp3", "flac", "subtitles", "tv-browse", "control-q")
        actors = {role: {"id": "actor-" + role, "username": role} for role in roles}
        libraries = {key: {"Id": "library-" + key, "Name": key, "CollectionType": kind, "Paths": ["/owned/" + key]}
                     for key, kind in (("Movies", "movies"), ("TV", "tvshows"), ("Music", "music"))}
        shapes = (("Movie", "library-Movies"), ("Series", "library-TV"), ("Season", "item-1"), ("Season", "item-1"),
                  ("Episode", "item-2"), ("Episode", "item-2"), ("Episode", "item-3"), ("MusicAlbum", "library-Music"), ("Audio", "item-7"), ("Audio", "item-7"))
        details = [{"Id": "item-" + str(i), "Name": "Name " + str(i), "Type": kind, "ParentId": parent,
                    **({} if kind == "MusicAlbum" else {"Path": "/owned/item-" + str(i)})} for i, (kind, parent) in enumerate(shapes)]
        resources = {"users": [actor["id"] for actor in actors.values()], "jobs": [{"id": "job-" + key, "libraryId": library["Id"]} for key, library in libraries.items()]}
        cleanup = [{"kind": "native", "tokenSha256": "native-hash"}, {"kind": "emby", "tokenSha256": "emby-hash"}]
        snapshot = {"schema": 28, "historyRows": 0,
            "users": [{"id": actor["id"], "name": actor["username"], "admin": role == "admin", "disabled": False,
                       "policy": copy.deepcopy(M.STORED_Q_POLICY) if role == "control-q" else {}} for role, actor in actors.items()],
            "libraries": [{"id": row["Id"], "name": row["Name"], "collectionType": row["CollectionType"]} for row in libraries.values()],
            "roots": [{"id": "root-" + key, "libraryId": row["Id"], "path": row["Paths"][0]} for key, row in libraries.items()],
            "jobs": [{**row, "status": "Completed", "error": "", "forceProbe": False} for row in resources["jobs"]],
            "items": [{"id": row["Id"], "name": row["Name"], "type": row["Type"], "parentId": row["ParentId"], "path": row.get("Path", "")} for row in details] +
                     [{"id": row["Id"], "name": row["Name"], "type": "CollectionFolder", "parentId": None, "path": ""} for row in libraries.values()],
            "sessions": [{"userId": actors["admin"]["id"], "kind": kind, "tokenSha256": fingerprint, "revoked": True}
                         for kind, fingerprint in (("admin", "native-hash"), ("emby", "emby-hash"))]}
        return [snapshot, details, libraries, actors, resources, cleanup, {"album": {"id": "item-7"}}]

    def test_exact_thirteen_stored_rows_and_ten_public_items_pass(self):
        result = M.validate_source_state(*self.fixture())
        self.assertEqual((result["storedItems"], result["publicItems"], result["collectionRoots"]), (13, 10, 3))

    def test_q_legacy_keys_are_required_and_unknown_keys_are_rejected(self):
        mutations = ({"IsAdministrator": True}, {"IsDisabled": True}, {"IsAdministrator": 0}, {"UnexpectedPolicy": False}, {"EnabledFolders": ["library-Movies"]})
        for mutation in mutations:
            args = self.fixture()
            args[0]["users"][-1]["policy"].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaisesRegex(M.StateCheckError, "stored_q_policy_differs"):
                M.validate_source_state(*args)
        args = self.fixture()
        del args[0]["users"][-1]["policy"]["IsDisabled"]
        with self.assertRaisesRegex(M.StateCheckError, "stored_q_policy_differs"):
            M.validate_source_state(*args)

    def test_ordinary_stored_policy_cannot_be_replaced_by_its_public_projection(self):
        args = self.fixture()
        args[0]["users"][1]["policy"] = {"EnableAllFolders": True}
        with self.assertRaisesRegex(M.StateCheckError, "stored_default_policy_differs"):
            M.validate_source_state(*args)

    def test_extra_missing_or_replaced_root_rows_are_not_ignored(self):
        for operation in ("add", "remove", "replace"):
            args = self.fixture()
            if operation == "add":
                args[0]["items"].append({"id": "unknown"})
            elif operation == "remove":
                args[0]["items"].pop()
            else:
                args[0]["items"][-1]["id"] = "unknown"
            with self.subTest(operation=operation), self.assertRaisesRegex(M.StateCheckError, "stored_item_membership_differs"):
                M.validate_source_state(*args)

    def test_collection_root_name_parent_type_and_path_are_all_exact(self):
        for mutation in ({"name": "Other"}, {"parentId": "item-1"}, {"type": "Folder"}, {"path": "/owned"}, {"unknown": False}):
            args = self.fixture()
            args[0]["items"][-1].update(mutation)
            with self.subTest(mutation=mutation), self.assertRaisesRegex(M.StateCheckError, "collection_folder_shape_differs"):
                M.validate_source_state(*args)

    def test_only_root_album_may_omit_its_public_path(self):
        args = self.fixture()
        args[1][7]["Path"] = ""
        with self.assertRaisesRegex(M.StateCheckError, "root_album_projection_differs"):
            M.validate_source_state(*args)
        args = self.fixture()
        del args[1][0]["Path"]
        with self.assertRaisesRegex(M.StateCheckError, "source_snapshot_shape_invalid"):
            M.validate_source_state(*args)

    def test_revocation_and_every_public_parent_edge_remain_required(self):
        args = self.fixture()
        args[0]["sessions"][0]["revoked"] = False
        with self.assertRaisesRegex(M.StateCheckError, "helper_token_revocation_differs"):
            M.validate_source_state(*args)
        for index in range(10):
            args = self.fixture()
            args[0]["items"][index]["parentId"] = "foreign"
            with self.subTest(index=index), self.assertRaises(M.StateCheckError):
                M.validate_source_state(*args)

    def test_revoked_token_fingerprints_cannot_be_swapped_between_session_kinds(self):
        args = self.fixture()
        left, right = args[0]["sessions"]
        left["tokenSha256"], right["tokenSha256"] = right["tokenSha256"], left["tokenSha256"]
        with self.assertRaisesRegex(M.StateCheckError, "helper_token_revocation_differs"):
            M.validate_source_state(*args)
        args = self.fixture()
        args[5][0]["kind"] = "unknown"
        with self.assertRaisesRegex(M.StateCheckError, "helper_cleanup_kind_binding_differs"):
            M.validate_source_state(*args)


if __name__ == "__main__":
    unittest.main()
